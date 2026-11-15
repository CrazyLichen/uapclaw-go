// 本文件对齐 openjiuwen/harness_providers/skills.py。
// 把 portable 技能包整目录复制进各 CLI 的发现目录。
package harness_providers

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillSource 一个 manifest 技能目录或含技能目录的技能库。
// Python: SkillSource (skills.py:23-40, frozen dataclass)
type SkillSource struct {
	// Dir 技能包或技能库根目录
	Dir string
	// Mode 取值 all 或 auto_list
	Mode string
	// EnabledSkills auto_list 模式下的技能名白名单（nil 表示全选）
	EnabledSkills []string
}

// NewSkillSource 构造并校验 SkillSource。
// Python: __post_init__（skills.py:31-40）
func NewSkillSource(dir string, mode string, enabledSkills []string) (SkillSource, error) {
	if dir == "" {
		return SkillSource{}, fmt.Errorf("skill dir must be a non-empty path")
	}
	if mode != "all" && mode != "auto_list" {
		return SkillSource{}, fmt.Errorf("skill mode must be all or auto_list")
	}
	// Python: enabled_skills 为 None 表示全选；传数组时元素必须全是字符串
	// （Go 侧 []string 类型系统已保证元素为字符串，保留空参校验语义）
	return SkillSource{Dir: dir, Mode: mode, EnabledSkills: enabledSkills}, nil
}

// ──────────────────────────── 常量 ────────────────────────────

// scanDirs provider 名 → 发现目录（项目相对路径）。
// Python: _SCAN_DIRS (skills.py:19)
var scanDirs = map[string]string{
	"claudecode": ".claude/skills",
	"codex":      ".agents/skills",
	"dsh":        ".dsh/skills",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// copyLock 进程内复制互斥锁。
// Python: _COPY_LOCK (skills.py:20)
var copyLock sync.Mutex

var (
	// copyFn 目录复制注入点（测试替换对齐 Python monkeypatch shutil.copytree）
	copyFn = copyTree
	// renameFn 重命名注入点（测试替换对齐 Python monkeypatch Path.rename）
	renameFn = os.Rename
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeSkills 冻结 provider 配置，构造期不访问源文件。
// Python: normalize_skills (skills.py:43-59)
func NormalizeSkills(values []any, conflict string) ([]SkillSource, error) {
	if conflict != "skip" && conflict != "replace" && conflict != "append" {
		return nil, fmt.Errorf("skill_conflict must be skip, replace or append")
	}
	result := make([]SkillSource, 0, len(values))
	for _, value := range values {
		switch v := value.(type) {
		case SkillSource:
			result = append(result, v)
		case string:
			result = append(result, SkillSource{Dir: v, Mode: "all"})
		case map[string]any:
			// Python: SkillSource(**dict(value))——走 __post_init__ 校验
			dir, _ := v["dir"].(string)
			mode, _ := v["mode"].(string)
			if mode == "" {
				mode = "all"
			}
			var enabled []string
			if raw, ok := v["enabled_skills"].([]any); ok {
				for _, item := range raw {
					s, _ := item.(string)
					enabled = append(enabled, s)
				}
			}
			src, err := NewSkillSource(dir, mode, enabled)
			if err != nil {
				return nil, err
			}
			result = append(result, src)
		default:
			return nil, fmt.Errorf("skill source must be a directory or manifest skill declaration")
		}
	}
	return result, nil
}

// InstallSkills 运行时启动前把完整技能包复制进发现目录，保留既有项目技能。
// replace 在发现目录外暂存，rename 失败回滚；复制产物在 harness 停止后留存。
// 内部软链物化；逃逸/环软链复制前拒绝。
// Python: install_skills (skills.py:146-221)——步骤逐行对齐。
func InstallSkills(sources []SkillSource, provider string, cwd string, conflict string, scanDir string) ([]string, error) {
	// Python: if not sources: return ()
	if len(sources) == 0 {
		return nil, nil
	}
	if conflict != "skip" && conflict != "replace" && conflict != "append" {
		return nil, fmt.Errorf("skill_conflict must be skip, replace or append")
	}
	relative := scanDir
	if relative == "" {
		relative = scanDirs[provider]
	}
	if relative == "" {
		return nil, fmt.Errorf("provider %q declares no skill discovery directory", provider)
	}
	projectRaw := cwd
	if projectRaw == "" {
		projectRaw, _ = os.Getwd()
	}
	// Python: Path(cwd or os.getcwd()).expanduser().resolve(strict=True)
	project, err := resolvePath(projectRaw)
	if err != nil {
		return nil, err
	}
	scan := filepath.Join(project, relative)
	var planned []struct {
		name   string
		source string
	}
	for _, source := range sources {
		root, err := resolvePath(source.Dir)
		if err != nil {
			return nil, err
		}
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("skill source is not a directory: %s", root)
		}
		bundles, err := discoverBundles(root)
		if err != nil {
			return nil, err
		}
		if len(bundles) == 0 {
			return nil, fmt.Errorf("no SKILL.md bundles found in %s", root)
		}
		for _, bundle := range bundles {
			name, err := parseSkillName(bundle)
			if err != nil {
				return nil, err
			}
			if len(source.EnabledSkills) == 0 || containsString(source.EnabledSkills, name) {
				planned = append(planned, struct{ name, source string }{name, bundle})
			}
		}
	}
	var installed []string
	// Python: with _COPY_LOCK —— 多成员同项目启动互斥
	copyLock.Lock()
	defer copyLock.Unlock()
	// Python: scan.resolve().is_relative_to(project)（非 strict resolve）
	scanResolved, err := resolvePath(scan)
	if err == nil && !isRelative(project, scanResolved) {
		return nil, fmt.Errorf("skill discovery directory escapes the project")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(scan, 0o755); err != nil { // parents=True, exist_ok=True
		return nil, err
	}
	for _, item := range planned {
		matches := matchSkills(scan, item.name)
		if len(matches) > 0 && conflict == "skip" {
			continue
		}
		if len(matches) > 1 && conflict == "replace" {
			return nil, fmt.Errorf("multiple existing directories declare skill %q", item.name)
		}
		copyName := item.name
		if len(matches) > 0 && conflict == "append" {
			copyName = nextCopyName(scan, item.name)
		}
		var destination string
		if len(matches) > 0 && conflict == "replace" {
			destination = matches[0]
		} else {
			destination = filepath.Join(scan, copyName)
		}
		// Python: target_location = destination.parent.resolve() / destination.name
		parentResolved, err := resolvePath(filepath.Dir(destination))
		if err != nil {
			return nil, err
		}
		targetLocation := filepath.Join(parentResolved, filepath.Base(destination))
		sourceResolved, err := resolvePath(item.source)
		if err != nil {
			return nil, err
		}
		if sourceResolved == targetLocation {
			continue // 自复制 no-op
		}
		if isRelative(sourceResolved, targetLocation) || isRelative(targetLocation, sourceResolved) {
			return nil, fmt.Errorf("skill source and destination must not contain each other")
		}
		if err := validateTree(item.source, item.source, nil); err != nil {
			return nil, err
		}
		// Python: TemporaryDirectory(prefix=".openjiuwen-skill-", dir=project)
		// D-15.12-c：os.MkdirTemp + 手动清理（committed 后保留产物由 rename 迁走，残留即清理）
		stage, err := os.MkdirTemp(project, ".openjiuwen-skill-*")
		if err != nil {
			return nil, err
		}
		var installErr error
		func() {
			// Python: TemporaryDirectory 上下文退出即删（含提交后的 backup 残留）
			defer os.RemoveAll(stage)
			copied := filepath.Join(stage, "new")
			backup := filepath.Join(stage, "previous")
			if err := copyFn(item.source, copied); err != nil {
				installErr = err
				return
			}
			if copyName != item.name {
				if err := renameCopiedSkill(copied, copyName); err != nil {
					installErr = err
					return
				}
			}
			// Python: destination.exists() or destination.is_symlink()
			existed := lstatExists(destination)
			if existed && conflict == "append" {
				installErr = fmt.Errorf("skill destination appeared during copy: %s", destination)
				return
			}
			if existed {
				if err := renameFn(destination, backup); err != nil {
					installErr = err
					return
				}
			}
			if err := renameFn(copied, destination); err != nil {
				if existed {
					_ = renameFn(backup, destination) // Python: backup.rename(destination) 回滚
				}
				installErr = err
				return
			}
			installed = append(installed, destination)
		}()
		if installErr != nil {
			return nil, installErr
		}
	}
	return installed, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolvePath EvalSymlinks 组合解析（D-15.12-a：替代 Python resolve(strict=True)）。
// 不存在 → error（等价 strict 抛 FileNotFoundError）。
func resolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// isRelative 判断 child 是否位于 parent 之下（均须已解析）。
// Python: Path.is_relative_to
func isRelative(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// validateTree 校验技能树：软链逃逸/环/非常规文件三类拒绝。
// Python: _validate_tree (skills.py:93-103)
func validateTree(path, root string, ancestors map[string]bool) error {
	resolved, err := resolvePath(path)
	if err != nil {
		return err
	}
	if !isRelative(root, resolved) {
		return fmt.Errorf("skill symlink escapes its bundle: %s", path)
	}
	fi, err := os.Stat(resolved) // Stat 跟随软链：Python resolved.is_dir() 同义
	if err != nil {
		return err
	}
	if fi.IsDir() {
		if ancestors[resolved] {
			return fmt.Errorf("cyclic skill directory: %s", path)
		}
		next := make(map[string]bool, len(ancestors)+1)
		for k, v := range ancestors {
			next[k] = v
		}
		next[resolved] = true
		entries, err := os.ReadDir(resolved)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := validateTree(filepath.Join(resolved, entry.Name()), root, next); err != nil {
				return err
			}
		}
		return nil
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("skill contains a non-regular file: %s", path)
	}
	return nil
}

// casefoldName Go 侧简化折叠（D-15.12-b：strings.EqualFold 语义以小写化表达，
// 技能名允许集 [A-Za-z0-9._-] 下与 str.casefold 等价）。
func casefoldName(s string) string { return strings.ToLower(s) }

// matchSkills 在 scan 目录中找与 name 匹配的既有技能目录（可能多个）。
// 目录名与 front-matter 名双轨比对；front-matter 解析失败静默跳过（用目录名）。
// Python: _matches (skills.py:106-117)
func matchSkills(scan, name string) []string {
	entries, err := os.ReadDir(scan)
	if err != nil {
		return nil
	}
	var matches []string
	for _, entry := range entries {
		existing := entry.Name()
		if entry.Type()&os.ModeSymlink == 0 && entry.IsDir() {
			if fi, err := os.Stat(filepath.Join(scan, entry.Name(), "SKILL.md")); err == nil && fi.Mode().IsRegular() {
				if parsed, err := parseSkillName(filepath.Join(scan, entry.Name())); err == nil {
					existing = parsed
				}
			}
		}
		if casefoldName(name) == casefoldName(entry.Name()) || casefoldName(name) == casefoldName(existing) {
			matches = append(matches, filepath.Join(scan, entry.Name()))
		}
	}
	return matches
}

// nextCopyName append 策略下的下一个可用名：-2 起步，总长截到 128。
// Python: _next_copy_name (skills.py:120-127)
func nextCopyName(scan, name string) string {
	for index := 2; ; index++ {
		suffix := fmt.Sprintf("-%d", index)
		candidate := name
		if len(candidate) > 128-len(suffix) {
			// Python 按字符截断；技能名 ASCII 限集下字节==字符
			candidate = candidate[:128-len(suffix)]
		}
		candidate += suffix
		if len(matchSkills(scan, candidate)) == 0 {
			return candidate
		}
	}
}

// skillFrontMatter 解析 SKILL.md 的 YAML front matter，返回元数据。
// 无 front matter 返回空 map；utf-8-sig BOM 剥离对齐（D-15.12-d）。
// Python: _name 内嵌逻辑（skills.py:63-77）
func skillFrontMatter(dir string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(raw), "\uFEFF")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]any{}, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unterminated skill front matter: %s", dir)
	}
	var metadata map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &metadata); err != nil {
		return nil, err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return metadata, nil
}

// parseSkillName 解析技能名并校验格式。
// Python: _name (skills.py:62-80)
func parseSkillName(dir string) (string, error) {
	// Python: document.resolve().is_relative_to(directory.resolve())——SKILL.md 须在包内
	docResolved, err := resolvePath(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return "", err
	}
	dirResolved, err := resolvePath(dir)
	if err != nil {
		return "", err
	}
	if !isRelative(dirResolved, docResolved) {
		return "", fmt.Errorf("SKILL.md must stay inside its bundle")
	}
	metadata, err := skillFrontMatter(dir)
	if err != nil {
		return "", err
	}
	name, _ := metadata["name"].(string)
	if name == "" {
		name = filepath.Base(dir)
	}
	// Python: re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", name)
	re := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	if !re.MatchString(name) {
		return "", fmt.Errorf("invalid portable skill name: %q", name)
	}
	return name, nil
}

// discoverBundles 发现 root 下的技能包。
// root 自含 SKILL.md → 单包；否则排序递归子目录（跳过软链目录）。
// Python: _discover (skills.py:83-90)
func discoverBundles(root string) ([]string, error) {
	if fi, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil && fi.Mode().IsRegular() {
		return []string{root}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, entry := range entries {
		// Python: sorted(root.iterdir())——ReadDir 已按文件名序
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			sub, err := discoverBundles(filepath.Join(root, entry.Name()))
			if err != nil {
				return nil, err
			}
			result = append(result, sub...)
		}
	}
	return result, nil
}

// renameCopiedSkill 把复制出的包 SKILL.md front matter 的 name 改为 name。
// 无 front matter 直接返回（目录名即技能名）。
// Python: _rename_copied_skill (skills.py:130-143)
func renameCopiedSkill(bundleDir string, name string) error {
	docPath := filepath.Join(bundleDir, "SKILL.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		return err
	}
	text := strings.TrimPrefix(string(raw), "\uFEFF")
	// Python: splitlines(keepends=True) —— 保留行尾
	lines := splitLinesKeepEnds(text)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil // Python 的 next(...) 在此不可达（_name 已校验）；防御返回
	}
	var metadata map[string]any
	body := strings.Join(stripEnds(lines[1:end]), "\n")
	if err := yaml.Unmarshal([]byte(body), &metadata); err != nil {
		return err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["name"] = name
	var buf strings.Builder
	buf.WriteString("---\n")
	out, err := yaml.Marshal(metadata) // yaml.v3 默认不转义 Unicode、保插入序（D-15.12-d）
	if err != nil {
		return err
	}
	buf.Write(out)
	buf.WriteString("---\n")
	for _, line := range lines[end+1:] {
		buf.WriteString(line)
	}
	return os.WriteFile(docPath, []byte(buf.String()), 0o644)
}

// splitLinesKeepEnds 保留行尾的按行切分（Python splitlines(keepends=True) 语义，13.18 惯例）。
func splitLinesKeepEnds(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// stripEnds 去掉各行行尾换行。
func stripEnds(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.TrimRight(line, "\r\n")
	}
	return out
}

// containsString 判含。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// lstatExists Lstat 判存在（软链也算存在，对齐 destination.exists() or is_symlink()）。
func lstatExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// copyTree 目录复制（shutil.copytree(symlinks=False) 语义：软链物化、保留 mode）。
func copyTree(src, dst string) error {
	return copyTreeWalk(src, dst)
}

// copyTreeWalk copyTree 的递归实现。
func copyTreeWalk(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		// 内部软链物化：读真实目标内容复制（symlinks=False）
		real, err := resolvePath(src)
		if err != nil {
			return err
		}
		return copyTreeWalk(real, dst)
	}
	if !fi.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, fi.Mode().Perm()); err != nil {
			return err
		}
		return os.Chmod(dst, fi.Mode().Perm())
	}
	if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyTreeWalk(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
