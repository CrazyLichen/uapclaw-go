package project_memory

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	utilpath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoadedMemoryFile 已加载的记忆文件。
// Python: LoadedMemoryFile (files.py L110-118)
type LoadedMemoryFile struct {
	// Path 文件绝对路径
	Path string
	// Kind 层级：managed/user/project/local
	Kind string
	// Content 文件内容（frontmatter 剥离后）
	Content string
	// Frontmatter 解析后的 frontmatter
	Frontmatter map[string]any
	// Priority 优先级数值
	Priority int
}

// GitWorktreeInfo Git worktree 信息。
// Python: GitWorktreeInfo (files.py L121-124)
type GitWorktreeInfo struct {
	// WorktreeRoot worktree 根目录
	WorktreeRoot string
	// CanonicalRoot canonical 仓库根目录
	CanonicalRoot string
}

// watchSnapshot 文件系统快照，用于缓存失效检测。
// Python: _build_watch_snapshot → tuple[(path, exists, is_dir, mtime_ns, size), ...]
type watchSnapshot struct {
	// entries 路径 → 快照条目
	entries map[string]snapshotEntry
}

// snapshotEntry 快照条目
type snapshotEntry struct {
	// exists 路径是否存在
	exists bool
	// isDir 是否为目录
	isDir bool
	// mtimeNs 修改时间纳秒（0 表示无）
	mtimeNs int64
	// size 文件大小（目录为 0）
	size int64
}

// cacheEntry 缓存条目。
// Python: _DiscoveryCacheEntry (files.py L127-130)
type cacheEntry struct {
	files    []LoadedMemoryFile
	snapshot watchSnapshot
}

// cacheKey 缓存键
type cacheKey struct {
	workspace      string
	targetPath     string
	additionalDirs string // 用 joined string 做可比较 key
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AdditionalDirectoriesEnv 额外目录环境变量。
	// Python: ADDITIONAL_DIRECTORIES_ENV = "JIUWENSWARM_ADDITIONAL_DIRECTORIES"
	AdditionalDirectoriesEnv = "UAPCLAWSWARM_ADDITIONAL_DIRECTORIES"

	// DefaultMaxChars 合并内容默认字符上限。
	// Python: DEFAULT_MAX_CHARS = 60_000
	DefaultMaxChars = 60000

	// MaxMemoryCharCount 单文件字符阈值。
	// Python: MAX_MEMORY_CHARACTER_COUNT = 40_000
	MaxMemoryCharCount = 40000

	// priorityManaged managed 层优先级
	// Python: PRIORITY["managed"] = 10
	priorityManaged = 10
	// priorityUser user 层优先级
	// Python: PRIORITY["user"] = 20
	priorityUser = 20
	// priorityProject project 层优先级
	// Python: PRIORITY["project"] = 30
	priorityProject = 30
	// priorityLocal local 层优先级
	// Python: PRIORITY["local"] = 40
	priorityLocal = 40
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// projectRootMarkers 项目根目录标记文件。
	// Python: PROJECT_ROOT_MARKERS (files.py L48-57)
	projectRootMarkers = []string{
		".git", ".uapclaw", ".claude",
		"pyproject.toml", "package.json", "Cargo.toml", "go.mod", "pom.xml",
	}

	// ProjectMemoryFiles 项目级固定文件名。
	// Python: PROJECT_MEMORY_FILES (files.py L59-62)
	// 每对：[0]=相对路径, [1]=kind
	ProjectMemoryFiles = [][2]string{
		{"UAPCLAWSWARM.md", "project"},
		{".uapclaw/UAPCLAWSWARM.md", "project"},
	}

	// LocalMemoryFiles 本地私有文件名。
	// Python: LOCAL_MEMORY_FILES (files.py L64-66)
	LocalMemoryFiles = [][2]string{
		{"UAPCLAWSWARM.local.md", "local"},
	}

	// ProjectMemoryGlobs 项目级 glob 模式。
	// Python: PROJECT_MEMORY_GLOBS (files.py L68-70)
	ProjectMemoryGlobs = []string{".uapclaw/rules/*.md"}

	// UserMemoryFiles 用户级固定文件名。
	// Python: USER_MEMORY_FILES (files.py L72-74)
	UserMemoryFiles = []string{"~/.uapclaw/UAPCLAWSWARM.md"}

	// UserMemoryGlobs 用户级 glob 模式。
	// Python: USER_MEMORY_GLOBS (files.py L76-78)
	UserMemoryGlobs = []string{"~/.uapclaw/rules/*.md"}

	// ManagedMemoryFiles 托管级固定文件名。
	// Python: MANAGED_MEMORY_FILES (files.py L80-82)
	ManagedMemoryFiles = []string{"/etc/uapclaw/UAPCLAWSWARM.md"}

	// ManagedMemoryGlobs 托管级 glob 模式。
	// Python: MANAGED_MEMORY_GLOBS (files.py L84-86)
	ManagedMemoryGlobs = []string{"/etc/uapclaw/rules/*.md"}

	// kindPriority kind → 优先级映射。
	// Python: PRIORITY (files.py L92-98)
	kindPriority = map[string]int{
		"managed": priorityManaged,
		"user":    priorityUser,
		"project": priorityProject,
		"local":   priorityLocal,
	}

	// discoveryCache 缓存：cacheKey → cacheEntry
	// Python: _DISCOVERY_CACHE (files.py L133)
	discoveryCache = make(map[cacheKey]*cacheEntry)

	// cacheMu 缓存互斥锁
	// Python: _CACHE_LOCK (files.py L134)
	cacheMu sync.RWMutex

	// pmLogComponent 日志组件
	pmLogComponent = logger.ComponentAgentServer

	// frontmatterRe 匹配 YAML frontmatter 块。
	// Python: _FRONTMATTER_RE (files.py L101)
	frontmatterRe = regexp.MustCompile(`(?s)^---\s*\n(.*?\n)---\s*(?:\n|\z)`)

	// fenceRe 匹配代码围栏行。
	// Python: _FENCE_RE (files.py L102)
	fenceRe = regexp.MustCompile(`^\s*(~~~|` + "`" + "`" + "`)")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ClearProjectMemoryCache 清除缓存的发现结果。
// Python: clear_project_memory_cache (files.py L142-155)
func ClearProjectMemoryCache(workspace string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if workspace == "" {
		discoveryCache = make(map[cacheKey]*cacheEntry)
		return
	}
	wsKey := safeResolve(workspace)
	for key := range discoveryCache {
		if key.workspace == wsKey {
			delete(discoveryCache, key)
		}
	}
}

// FindProjectRoot 从 cwd 向上遍历查找项目根目录。
// Python: find_project_root (files.py L158-171)
func FindProjectRoot(cwd string) string {
	current, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}
	current = safeResolve(current)

	for dir := current; dir != ""; dir = filepath.Dir(dir) {
		for _, marker := range projectRootMarkers {
			p := filepath.Join(dir, marker)
			if _, err := os.Stat(p); err == nil {
				return dir
			}
		}
		if dir == filepath.Dir(dir) {
			// 到达文件系统根
			break
		}
	}
	return ""
}

// DiscoverAndLoadMemoryFiles 发现并加载所有适用的 memory 文件。
// Python: discover_and_load_memory_files (files.py L174-333)
func DiscoverAndLoadMemoryFiles(workspace string, targetPath string, additionalDirectories []string) []LoadedMemoryFile {
	workspaceKey := safeResolve(workspace)
	targetKey := safeResolve(targetPath)
	if targetKey == "" {
		targetKey = workspaceKey
	}
	normalizedAdditional := normalizeAdditionalDirectories(additionalDirectories, workspaceKey)
	ck := cacheKey{workspace: workspaceKey, targetPath: targetKey, additionalDirs: normalizedAdditional}

	cacheMu.RLock()
	cached, ok := discoveryCache[ck]
	cacheMu.RUnlock()
	if ok {
		// 构建当前快照并比对
		watchPaths := make(map[string]bool)
		for path := range cached.snapshot.entries {
			watchPaths[path] = true
		}
		currentSnapshot := buildWatchSnapshot(watchPaths)
		if snapshotUnchanged(cached.snapshot, currentSnapshot) {
			return append([]LoadedMemoryFile(nil), cached.files...)
		}
	}

	var files []LoadedMemoryFile
	seen := make(map[string]bool)
	watchPaths := make(map[string]bool)

	// managed 层
	// Python: for raw in MANAGED_MEMORY_FILES: _try_load_single(...)
	for _, raw := range ManagedMemoryFiles {
		tryLoadSingle(raw, "managed", &files, seen, targetKey, watchPaths)
	}
	// Python: _scan_absolute_globs(MANAGED_MEMORY_GLOBS, kind="managed", ...)
	scanAbsoluteGlobs(ManagedMemoryGlobs, "managed", &files, seen, targetKey, watchPaths)

	// user 层
	// Python: for raw in USER_MEMORY_FILES: _try_load_single(...)
	for _, raw := range UserMemoryFiles {
		tryLoadSingle(raw, "user", &files, seen, targetKey, watchPaths)
	}
	// Python: _scan_absolute_globs(USER_MEMORY_GLOBS, kind="user", ...)
	scanAbsoluteGlobs(UserMemoryGlobs, "user", &files, seen, targetKey, watchPaths)

	// project 层（root → cwd 遍历）
	// Python: project_root = find_project_root(workspace_key)
	projectRoot := FindProjectRoot(workspaceKey)
	if projectRoot != "" {
		cwd := workspaceKey
		// Python: worktree_info = _detect_git_worktree(cwd)
		worktreeInfo := detectGitWorktree(cwd)
		scanRoot := projectRoot
		// Python: if worktree_info is not None and worktree_info.canonical_root != worktree_info.worktree_root
		//           and _is_relative_to(project_root, worktree_info.canonical_root):
		//             scan_root = worktree_info.canonical_root
		if worktreeInfo != nil && worktreeInfo.CanonicalRoot != worktreeInfo.WorktreeRoot &&
			isRelativeTo(projectRoot, worktreeInfo.CanonicalRoot) {
			scanRoot = worktreeInfo.CanonicalRoot
		}

		// Python: walk_dirs = []  current = cwd  while True: walk_dirs.append(current) ...
		var walkDirs []string
		current := cwd
		for {
			walkDirs = append(walkDirs, current)
			if current == scanRoot || filepath.Dir(current) == current {
				break
			}
			current = filepath.Dir(current)
		}
		// Python: walk_dirs.reverse()
		for i, j := 0, len(walkDirs)-1; i < j; i, j = i+1, j-1 {
			walkDirs[i], walkDirs[j] = walkDirs[j], walkDirs[i]
		}

		// Python: for d in walk_dirs:
		for _, d := range walkDirs {
			watchPaths[safeResolve(d)] = true
			skipProject := shouldSkipProjectDir(d, worktreeInfo)
			if !skipProject {
				scanRelativeFiles(d, ProjectMemoryFiles, &files, seen, targetKey, watchPaths)
				scanRelativeGlobs(d, ProjectMemoryGlobs, "project", &files, seen, targetKey, watchPaths)
			}
			scanRelativeFiles(d, LocalMemoryFiles, &files, seen, targetKey, watchPaths)
		}
	}

	// 额外目录
	// Python: for raw_dir in normalized_additional_dirs:
	if normalizedAdditional != "" {
		for _, rawDir := range filepath.SplitList(normalizedAdditional) {
			watchPaths[safeResolve(rawDir)] = true
			scanRelativeFiles(rawDir, ProjectMemoryFiles, &files, seen, targetKey, watchPaths)
			scanRelativeGlobs(rawDir, ProjectMemoryGlobs, "project", &files, seen, targetKey, watchPaths)
			scanRelativeFiles(rawDir, LocalMemoryFiles, &files, seen, targetKey, watchPaths)
		}
	}

	// Python: files.sort(key=lambda f: f.priority)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Priority < files[j].Priority
	})

	// Python: snapshot = _build_watch_snapshot(watch_paths)
	snapshot := buildWatchSnapshot(watchPaths)
	cacheMu.Lock()
	discoveryCache[ck] = &cacheEntry{files: files, snapshot: snapshot}
	cacheMu.Unlock()

	return append([]LoadedMemoryFile(nil), files...)
}

// MergeMemoryContent 将文件合并为单个文本块，按优先级排列，超过 maxChars 时截断。
// Python: merge_memory_content (files.py L336-367)
func MergeMemoryContent(files []LoadedMemoryFile, maxChars int) string {
	var parts []string
	total := 0
	var truncatedAt string
	// Python: for f in files:
	for _, f := range files {
		// Python: chunk = f"### {f.kind} memory -- {_short(f.path)}\n{f.content}\n"
		chunk := "### " + f.Kind + " memory -- " + short(f.Path) + "\n" + f.Content + "\n"
		// Python: if total + len(chunk) > max_chars:
		if total+len(chunk) > maxChars {
			remaining := maxChars - total
			// Python: if remaining > 200: parts.append(chunk[:remaining])
			if remaining > 200 {
				parts = append(parts, chunk[:remaining])
			}
			truncatedAt = f.Path
			break
		}
		parts = append(parts, chunk)
		total += len(chunk)
	}
	// Python: merged = "\n".join(parts).strip()
	merged := strings.TrimSpace(strings.Join(parts, "\n"))
	if truncatedAt != "" {
		// Python: logger.warning("[project_memory] merged content exceeded max_chars=%d; truncated at %s", ...)
		logger.Warn(pmLogComponent).
			Int("max_chars", maxChars).
			Str("truncated_at", truncatedAt).
			Msg("[project_memory] 合并内容超出字符上限，已截断")
		// Python: merged += "\n\n<!-- project memory truncated (> max_chars) -->"
		merged += "\n\n<!-- project memory truncated (> max_chars) -->"
	}
	return merged
}

// GetLargeMemoryFiles 返回超过单文件字符阈值的警告列表。
// Python: get_large_memory_files (files.py L370-387)
func GetLargeMemoryFiles(files []LoadedMemoryFile, threshold int) []map[string]any {
	var warnings []map[string]any
	for _, f := range files {
		charCount := len(f.Content)
		if charCount > threshold {
			// Python: warnings.append({"path": f.path, "kind": f.kind, "char_count": ..., ...})
			warnings = append(warnings, map[string]any{
				"path":       f.Path,
				"kind":       f.Kind,
				"char_count": charCount,
				"threshold":  threshold,
				"message":    "Large " + short(f.Path) + " (" + fmt.Sprintf("%d", charCount) + " chars > " + fmt.Sprintf("%d", threshold) + ") will impact performance",
			})
		}
	}
	return warnings
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeResolve 安全 resolve（不抛异常）。
// Python: _safe_resolve (files.py L926-930)
func safeResolve(p string) string {
	resolved, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	// 尝试 EvalSymlinks 获取真实路径（对齐 Python Path.resolve()）
	if evaled, err := filepath.EvalSymlinks(resolved); err == nil {
		return evaled
	}
	return resolved
}

// isRelativeTo 判断 path 是否在 root 下。
// Python: _is_relative_to (files.py L918-923)
func isRelativeTo(p string, root string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// short 将路径中 home 目录替换为 ~。
// Python: _short (files.py L933-940)
func short(p string) string {
	home := utilpath.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// expandHome 展开路径中的 ~。
// Python: os.path.expanduser
func expandHome(p string) string {
	return utilpath.ExpandHome(p)
}

// detectGitWorktree 检测 git worktree 信息。
// Python: _detect_git_worktree (files.py L822-834)
func detectGitWorktree(cwd string) *GitWorktreeInfo {
	// Python: git_exe = _git_executable()
	gitExe, err := exec.LookPath("git")
	if err != nil {
		return nil
	}

	// Python: worktree_root = _git_path(cwd, "rev-parse", "--show-toplevel")
	worktreeRoot := gitPath(cwd, gitExe, "rev-parse", "--show-toplevel")
	// Python: common_dir = _git_path(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	commonDir := gitPath(cwd, gitExe, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if worktreeRoot == "" || commonDir == "" {
		return nil
	}

	// Python: canonical_root = _canonical_root_from_common_dir(common_dir)
	canonicalRoot := canonicalRootFromCommonDir(commonDir)
	if canonicalRoot == "" {
		return nil
	}

	return &GitWorktreeInfo{
		WorktreeRoot:  worktreeRoot,
		CanonicalRoot: canonicalRoot,
	}
}

// gitPath 执行 git 命令返回路径。
// Python: _git_path (files.py L848-869)
func gitPath(cwd string, gitExe string, args ...string) string {
	// Python: completed = subprocess.run([git_exe, "-C", str(cwd), *args], check=True, stdout=PIPE, stderr=DEVNULL, text=True, timeout=2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitExe, append([]string{"-C", cwd}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return ""
	}
	return safeResolve(result)
}

// canonicalRootFromCommonDir 从 git common dir 推断 canonical 仓库根。
// Python: _canonical_root_from_common_dir (files.py L872-878)
func canonicalRootFromCommonDir(commonDir string) string {
	// Python: if common_dir.name == ".git": return common_dir.parent
	base := filepath.Base(commonDir)
	if base == ".git" {
		return filepath.Dir(commonDir)
	}
	// Python: parent = common_dir.parent; if parent.name == "worktrees" and parent.parent.name == ".git": return parent.parent.parent
	parent := filepath.Dir(commonDir)
	grandparent := filepath.Dir(parent)
	if filepath.Base(parent) == "worktrees" && filepath.Base(grandparent) == ".git" {
		return filepath.Dir(grandparent)
	}
	return ""
}

// shouldSkipProjectDir 判断是否跳过 canonical 仓库的 project memory。
// Python: _should_skip_project_dir (files.py L881-892)
func shouldSkipProjectDir(directory string, worktreeInfo *GitWorktreeInfo) bool {
	// Python: if worktree_info is None: return False
	if worktreeInfo == nil {
		return false
	}
	// Python: if worktree_info.canonical_root == worktree_info.worktree_root: return False
	if worktreeInfo.CanonicalRoot == worktreeInfo.WorktreeRoot {
		return false
	}
	// Python: return _is_relative_to(directory, worktree_info.canonical_root) and not _is_relative_to(directory, worktree_info.worktree_root)
	return isRelativeTo(directory, worktreeInfo.CanonicalRoot) &&
		!isRelativeTo(directory, worktreeInfo.WorktreeRoot)
}

// normalizeAdditionalDirectories 规范化额外扫描目录。
// Python: _normalize_additional_directories (files.py L395-418)
func normalizeAdditionalDirectories(dirs []string, workspace string) string {
	// Python: if additional_directories is None: env_value = os.getenv(...)
	if dirs == nil {
		envValue := os.Getenv(AdditionalDirectoriesEnv)
		if envValue == "" {
			return ""
		}
		var envDirs []string
		for _, item := range filepath.SplitList(envValue) {
			item = strings.TrimSpace(item)
			if item != "" {
				envDirs = append(envDirs, item)
			}
		}
		dirs = envDirs
	}
	// Python: result = []; seen = set()
	var result []string
	seen := make(map[string]bool)
	for _, raw := range dirs {
		// Python: candidate = Path(os.path.expanduser(str(raw)))
		candidate := expandHome(raw)
		// Python: if not candidate.is_absolute() and workspace is not None: candidate = workspace / candidate
		if !filepath.IsAbs(candidate) && workspace != "" {
			candidate = filepath.Join(workspace, candidate)
		}
		// Python: normalized = _safe_resolve(candidate)
		normalized := safeResolve(candidate)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return strings.Join(result, string(os.PathListSeparator))
}

// scanRelativeFiles 扫描相对路径的固定文件名。
// Python: _scan_relative_files (files.py L421-441)
func scanRelativeFiles(
	baseDir string,
	entries [][2]string,
	out *[]LoadedMemoryFile,
	seen map[string]bool,
	targetPath string,
	watchPaths map[string]bool,
) {
	// Python: for rel, kind in entries:
	for _, entry := range entries {
		rel := entry[0]
		kind := entry[1]
		// Python: watch_paths.add(_safe_resolve(base_dir))
		watchPaths[safeResolve(baseDir)] = true
		// Python: abs_path = base_dir / rel
		absPath := filepath.Join(baseDir, rel)
		// Python: if abs_path.is_file():
		info, err := os.Stat(absPath)
		if err == nil && !info.IsDir() {
			loadPath(absPath, kind, out, seen, targetPath, watchPaths)
		}
	}
}

// scanRelativeGlobs 扫描相对路径的 glob 模式。
// Python: _scan_relative_globs (files.py L444-466)
func scanRelativeGlobs(
	baseDir string,
	patterns []string,
	kind string,
	out *[]LoadedMemoryFile,
	seen map[string]bool,
	targetPath string,
	watchPaths map[string]bool,
) {
	// Python: for pattern in patterns:
	for _, pattern := range patterns {
		// Python: watch_paths.add(_safe_resolve(base_dir / Path(pattern).parent))
		globBase := filepath.Join(baseDir, filepath.Dir(pattern))
		watchPaths[safeResolve(globBase)] = true
		// Python: for matched in sorted(_glob.glob(str(base_dir / pattern))):
		matches, err := filepath.Glob(filepath.Join(baseDir, pattern))
		if err != nil {
			continue
		}
		sort.Strings(matches)
		for _, matched := range matches {
			// Python: if path.is_file():
			info, err := os.Stat(matched)
			if err == nil && !info.IsDir() {
				loadPath(matched, kind, out, seen, targetPath, watchPaths)
			}
		}
	}
}

// scanAbsoluteGlobs 扫描绝对路径的 glob 模式。
// Python: _scan_absolute_globs (files.py L469-491)
func scanAbsoluteGlobs(
	patterns []string,
	kind string,
	out *[]LoadedMemoryFile,
	seen map[string]bool,
	targetPath string,
	watchPaths map[string]bool,
) {
	// Python: for raw_pattern in patterns:
	for _, rawPattern := range patterns {
		// Python: expanded = os.path.expanduser(raw_pattern)
		expanded := expandHome(rawPattern)
		// Python: watch_paths.add(_safe_resolve(Path(expanded).parent))
		watchPaths[safeResolve(filepath.Dir(expanded))] = true
		// Python: for matched in sorted(_glob.glob(expanded)):
		matches, err := filepath.Glob(expanded)
		if err != nil {
			continue
		}
		sort.Strings(matches)
		for _, matched := range matches {
			info, err := os.Stat(matched)
			if err == nil && !info.IsDir() {
				loadPath(matched, kind, out, seen, targetPath, watchPaths)
			}
		}
	}
}

// tryLoadSingle 加载单个绝对路径文件。
// Python: _try_load_single (files.py L494-514)
func tryLoadSingle(
	raw string,
	kind string,
	out *[]LoadedMemoryFile,
	seen map[string]bool,
	targetPath string,
	watchPaths map[string]bool,
) {
	// Python: expanded = os.path.expanduser(raw)
	expanded := expandHome(raw)
	// Python: watch_paths.add(_safe_resolve(path.parent))
	watchPaths[safeResolve(filepath.Dir(expanded))] = true
	// Python: if path.is_file():
	info, err := os.Stat(expanded)
	if err == nil && !info.IsDir() {
		loadPath(expanded, kind, out, seen, targetPath, watchPaths)
	}
}

// loadPath 加载一个文件：读取、解析 frontmatter、展开 include、去重。
// Python: _load_path (files.py L517-564)
func loadPath(
	filePath string,
	kind string,
	out *[]LoadedMemoryFile,
	seen map[string]bool,
	targetPath string,
	watchPaths map[string]bool,
) {
	// Python: resolved = str(path.resolve())
	resolved := safeResolve(filePath)
	// Python: watch_paths.add(resolved)
	watchPaths[resolved] = true
	// Python: if resolved in seen: return
	if seen[resolved] {
		return
	}
	seen[resolved] = true

	// Python: raw = path.read_text(encoding="utf-8", errors="replace")
	rawBytes, err := os.ReadFile(filePath)
	if err != nil {
		// Python: logger.warning("[project_memory] failed to read %s: %s", resolved, exc)
		logger.Warn(pmLogComponent).
			Str("path", resolved).
			Err(err).
			Msg("[project_memory] 读取文件失败")
		return
	}
	raw := string(rawBytes)

	// Python: frontmatter, body = _parse_frontmatter(raw)
	frontmatter, body := parseFrontmatter(raw)
	// Python: if not _frontmatter_paths_match(frontmatter, path=path, target_path=target_path): return
	if !frontmatterPathsMatch(frontmatter, resolved, targetPath) {
		return
	}

	// Python: body_without_includes = _expand_includes(body, current_path=path, kind=kind, out=out, seen=seen, target_path=target_path, watch_paths=watch_paths)
	bodyWithoutIncludes := expandIncludes(body, resolved, kind, out, seen, targetPath, watchPaths)
	// Python: body_stripped = body_without_includes.strip()
	bodyStripped := strings.TrimSpace(bodyWithoutIncludes)
	if bodyStripped == "" {
		return
	}

	// Python: out.append(LoadedMemoryFile(path=resolved, kind=kind, content=body_stripped, frontmatter=frontmatter, priority=PRIORITY.get(kind, 30)))
	priority := priorityProject
	if p, ok := kindPriority[kind]; ok {
		priority = p
	}

	*out = append(*out, LoadedMemoryFile{
		Path:        resolved,
		Kind:        kind,
		Content:     bodyStripped,
		Frontmatter: frontmatter,
		Priority:    priority,
	})
}

// expandIncludes 展开 @include 指令。
// Python: _expand_includes (files.py L567-612)
func expandIncludes(
	body string,
	currentPath string,
	kind string,
	out *[]LoadedMemoryFile,
	seen map[string]bool,
	targetPath string,
	watchPaths map[string]bool,
) string {
	var keptLines []string
	inFence := false

	// Python: for line in body.splitlines():
	for _, line := range strings.Split(body, "\n") {
		// Python: if _FENCE_RE.match(line): in_fence = not in_fence
		if fenceRe.MatchString(line) {
			inFence = !inFence
			keptLines = append(keptLines, line)
			continue
		}
		// Python: if in_fence: kept_lines.append(line); continue
		if inFence {
			keptLines = append(keptLines, line)
			continue
		}

		// Python: include_spec = _extract_include_spec(line)
		includeSpec := extractIncludeSpec(line)
		if includeSpec == "" {
			keptLines = append(keptLines, line)
			continue
		}

		// Python: include_path = _resolve_include_path(include_spec, current_path=current_path)
		includePath := resolveIncludePath(includeSpec, currentPath)
		if includePath == "" {
			continue
		}
		// Python: watch_paths.add(_safe_resolve(include_path.parent))
		watchPaths[safeResolve(filepath.Dir(includePath))] = true
		// Python: watch_paths.add(_safe_resolve(include_path))
		watchPaths[safeResolve(includePath)] = true
		// Python: if not include_path.is_file(): continue
		info, err := os.Stat(includePath)
		if err != nil || info.IsDir() {
			continue
		}

		// Python: _load_path(include_path, kind, out, seen, target_path=target_path, watch_paths=watch_paths)
		loadPath(includePath, kind, out, seen, targetPath, watchPaths)
	}

	return strings.Join(keptLines, "\n")
}

// extractIncludeSpec 从行中提取 include 路径。
// Python: _extract_include_spec (files.py L615-623)
func extractIncludeSpec(line string) string {
	stripped := strings.TrimSpace(line)
	if stripped == "" {
		return ""
	}
	// Python: if stripped.startswith("@include "): return stripped[len("@include "):].strip()
	if strings.HasPrefix(stripped, "@include ") {
		return strings.TrimSpace(stripped[len("@include "):])
	}
	// Python: if stripped.startswith("@") and not stripped.startswith("@@"): return stripped[1:].strip()
	if strings.HasPrefix(stripped, "@") && !strings.HasPrefix(stripped, "@@") {
		return strings.TrimSpace(stripped[1:])
	}
	return ""
}

// resolveIncludePath 解析 include 路径。
// Python: _resolve_include_path (files.py L626-636)
func resolveIncludePath(spec string, currentPath string) string {
	// Python: cleaned = spec.strip().strip('"').strip("'")
	cleaned := strings.TrimSpace(spec)
	cleaned = strings.Trim(strings.Trim(cleaned, `"`), `'`)
	if cleaned == "" {
		return ""
	}
	// Python: if cleaned.startswith("~/"): return Path(os.path.expanduser(cleaned))
	if strings.HasPrefix(cleaned, "~/") {
		return expandHome(cleaned)
	}
	// Python: if cleaned.startswith("/"): return Path(cleaned)
	if filepath.IsAbs(cleaned) {
		return cleaned
	}
	// Python: return (current_path.parent / cleaned).resolve()
	dir := filepath.Dir(currentPath)
	resolved := filepath.Join(dir, cleaned)
	return safeResolve(resolved)
}

// frontmatterPathsMatch 检查 frontmatter 的 paths: 规则是否匹配当前目标路径。
// Python: _frontmatter_paths_match (files.py L639-648)
func frontmatterPathsMatch(frontmatter map[string]any, filePath string, targetPath string) bool {
	globs := extractPathsGlobs(frontmatter)
	if len(globs) == 0 {
		return true
	}
	return pathsMatchTarget(globs, filePath, targetPath)
}

// extractPathsGlobs 从 frontmatter 提取 paths glob 列表。
// Python: _extract_paths_globs (files.py L651-660)
func extractPathsGlobs(frontmatter map[string]any) []string {
	raw, ok := frontmatter["paths"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v != "" {
			return []string{v}
		}
		return nil
	case []string:
		result := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(item)
			if s != "" {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// pathsMatchTarget 将 paths globs 与目标路径匹配。
// Python: _paths_match_target (files.py L663-705)
func pathsMatchTarget(globs []string, rulePath string, targetPath string) bool {
	// Python: project_root = find_project_root(str(target))
	projectRoot := FindProjectRoot(targetPath)
	var ruleBase string
	// Python: if project_root is not None and not _is_relative_to(rule_path, project_root): rule_base = project_root
	if projectRoot != "" && !isRelativeTo(rulePath, projectRoot) {
		ruleBase = projectRoot
	} else {
		// Python: else: rule_base = rule_path.parent; for parent in rule_path.parents: if parent.name in {".jiuwen", ".claude"}: rule_base = parent.parent; break
		ruleBase = filepath.Dir(rulePath)
		dir := rulePath
		for {
			base := filepath.Base(dir)
			if base == ".uapclaw" || base == ".claude" {
				ruleBase = filepath.Dir(dir)
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Python: relative = target.relative_to(rule_base.resolve())
	rel, err := filepath.Rel(ruleBase, targetPath)
	if err != nil {
		return false
	}
	// Python: relative_posix = relative.as_posix().strip("/")
	relPosix := filepath.ToSlash(rel)
	relPosix = strings.Trim(relPosix, "/")

	// Python: candidates
	var candidates []string
	if relPosix != "" {
		// Python: trail = posixpath.normpath(posixpath.join(relative_posix, ".")) + "/"
		trail := pathNormalize(relPosix) + "/"
		// Python: with_dir_magic = posixpath.join(relative_posix, "__dir__")
		dirMagic := relPosix + "/__dir__"
		candidates = []string{relPosix, trail, dirMagic}
	} else {
		candidates = []string{"", "/", "/__dir__", ".", "__dir__"}
	}

	// Python: for glob_pattern in globs:
	for _, pattern := range globs {
		// Python: normalized = glob_pattern.strip().lstrip("./").replace("\\", "/")
		normalized := strings.TrimSpace(pattern)
		normalized = strings.TrimLeft(normalized, "./")
		normalized = strings.ReplaceAll(normalized, `\`, "/")
		if normalized == "" {
			continue
		}
		for _, candidate := range candidates {
			if fnmatchMatch(candidate, normalized) {
				return true
			}
		}
	}
	return false
}

// fnmatchMatch 简单的 fnmatch 风格匹配（支持 *, ?）。
// Python: fnmatch.fnmatchcase
// 注意：Go 的 filepath.Match 仅支持 * 和 ?，不支持 [seq] 语法，
// 与 Python fnmatch 有差异。但 Python 实际 paths 规则中未使用 [seq]，
// 因此 filepath.Match 足以满足对齐需求。
func fnmatchMatch(name, pattern string) bool {
	matched, _ := filepath.Match(pattern, name)
	return matched
}

// pathNormalize 规范化 posix 路径（对齐 Python posixpath.normpath）
func pathNormalize(p string) string {
	p = filepath.ToSlash(p)
	parts := strings.Split(p, "/")
	var out []string
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "/")
}

// parseFrontmatter 轻量 frontmatter 解析。
// Python: _parse_frontmatter (files.py L708-746)
func parseFrontmatter(raw string) (map[string]any, string) {
	// Python: match = _FRONTMATTER_RE.match(raw)
	match := frontmatterRe.FindStringSubmatchIndex(raw)
	if match == nil {
		return map[string]any{}, raw
	}
	// Python: lines = match.group(1).splitlines()
	fmText := raw[match[2]:match[3]]
	lines := strings.Split(fmText, "\n")
	result := make(map[string]any, 4)
	idx := 0
	for idx < len(lines) {
		line := lines[idx]
		stripped := strings.TrimSpace(line)
		// Python: if not stripped or stripped.startswith("#") or ":" not in line: idx += 1; continue
		if stripped == "" || strings.HasPrefix(stripped, "#") || !strings.Contains(line, ":") {
			idx++
			continue
		}
		// Python: key, _, value = line.partition(":")
		key, value, _ := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value != "" {
			// Python: result[key] = _parse_frontmatter_value(value)
			result[key] = parseFrontmatterValue(value)
			idx++
			continue
		}
		// 块值
		// Python: block = []; idx += 1; while idx < len(lines): ...
		var block []string
		idx++
		for idx < len(lines) {
			nxt := lines[idx]
			nxtStripped := strings.TrimSpace(nxt)
			if nxtStripped == "" {
				idx++
				continue
			}
			// Python: if not nxt.startswith((" ", "\t")) and ":" in nxt and not nxt_stripped.startswith("-"): break
			if !strings.HasPrefix(nxt, " ") && !strings.HasPrefix(nxt, "\t") &&
				strings.Contains(nxt, ":") && !strings.HasPrefix(nxtStripped, "-") {
				break
			}
			block = append(block, nxtStripped)
			idx++
		}
		// Python: result[key] = _parse_frontmatter_block(block)
		result[key] = parseFrontmatterBlock(block)
	}
	// Python: return result, raw[match.end():]
	bodyStart := match[1]
	return result, raw[bodyStart:]
}

// parseFrontmatterValue 解析单个 frontmatter 值。
// Python: _parse_frontmatter_value (files.py L749-762)
func parseFrontmatterValue(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Python: if value.startswith("[") and value.endswith("]"): parsed = _parse_inline_frontmatter_list(value)
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		if parsed := parseInlineFrontmatterList(value); parsed != nil {
			return parsed
		}
	}
	// Python: lowered = value.lower()
	lowered := strings.ToLower(value)
	switch lowered {
	case "true":
		return true
	case "false":
		return false
	case "null", "none":
		return nil
	}
	// Python: return value.strip('"').strip("'")
	return strings.Trim(strings.Trim(value, `"`), `'`)
}

// parseInlineFrontmatterList 解析行内列表 [a, b, c]。
// Python: _parse_inline_frontmatter_list (files.py L765-807)
func parseInlineFrontmatterList(value string) []string {
	// Python: inner = value[1:-1].strip()
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return []string{}
	}

	var items []string
	var current []rune
	var quote rune
	escaped := false

	// Python: for ch in inner:
	for _, ch := range inner {
		if quote != 0 {
			if escaped {
				current = append(current, ch)
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
				continue
			}
			current = append(current, ch)
			continue
		}
		// Python: if ch in {'"', "'"}: quote = ch; continue
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		// Python: if ch == ",": item = "".join(current).strip(); items.append(item.strip('"').strip("'")); current = []; continue
		if ch == ',' {
			item := strings.TrimSpace(string(current))
			if item != "" {
				items = append(items, strings.Trim(strings.Trim(item, `"`), `'`))
			}
			current = current[:0]
			continue
		}
		current = append(current, ch)
	}

	// Python: tail = "".join(current).strip()
	tail := strings.TrimSpace(string(current))
	if tail != "" {
		items = append(items, strings.Trim(strings.Trim(tail, `"`), `'`))
	}

	// Python: if quote is not None: return None
	if quote != 0 {
		return nil
	}
	return items
}

// parseFrontmatterBlock 解析缩进块。
// Python: _parse_frontmatter_block (files.py L810-819)
func parseFrontmatterBlock(lines []string) any {
	if len(lines) == 0 {
		return []string{}
	}
	// Python: if all(line.startswith("-") for line in lines):
	allDash := true
	for _, l := range lines {
		if !strings.HasPrefix(l, "-") {
			allDash = false
			break
		}
	}
	if allDash {
		result := make([]string, 0, len(lines))
		for _, l := range lines {
			// Python: line[1:].strip().strip('"').strip("'")
			item := strings.TrimSpace(l[1:])
			item = strings.Trim(strings.Trim(item, `"`), `'`)
			if item != "" {
				result = append(result, item)
			}
		}
		return result
	}
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		item := strings.TrimSpace(l)
		item = strings.Trim(strings.Trim(item, `"`), `'`)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

// buildWatchSnapshot 构建文件系统快照。
// Python: _build_watch_snapshot (files.py L895-915)
func buildWatchSnapshot(paths map[string]bool) watchSnapshot {
	// 收集并排序路径
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		if p = strings.TrimSpace(p); p != "" {
			sorted = append(sorted, p)
		}
	}
	sort.Strings(sorted)

	entries := make(map[string]snapshotEntry, len(sorted))
	for _, raw := range sorted {
		info, err := os.Stat(raw)
		if err != nil {
			// Python: snapshot.append((raw, False, False, None, None))
			entries[raw] = snapshotEntry{exists: false}
			continue
		}
		// Python: mtime_ns = getattr(stat_result, "st_mtime_ns", None)
		mtimeNs := info.ModTime().UnixNano()
		// Python: size = None if path.is_dir() else stat_result.st_size
		var size int64
		if !info.IsDir() {
			size = info.Size()
		}
		// Python: snapshot.append((raw, True, path.is_dir(), mtime_ns, size))
		entries[raw] = snapshotEntry{
			exists:  true,
			isDir:   info.IsDir(),
			mtimeNs: mtimeNs,
			size:    size,
		}
	}
	return watchSnapshot{entries: entries}
}

// snapshotUnchanged 对比两个快照是否相同。
func snapshotUnchanged(a, b watchSnapshot) bool {
	if len(a.entries) != len(b.entries) {
		return false
	}
	for k, v := range a.entries {
		bv, ok := b.entries[k]
		if !ok {
			return false
		}
		if v != bv {
			return false
		}
	}
	return true
}
