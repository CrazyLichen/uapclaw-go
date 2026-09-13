# ProjectMemoryRail (10.6.7) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ProjectMemoryRail，在每次 LLM 调用前动态注入项目记忆 PromptSection，完整对齐 Python `project_memory/` + `project_memory_rail.py` 的所有功能。

**Architecture:** 新建 `project_memory/` 子包（files.go + section.go + doc.go），新建 `project_memory_rail.go`，回填 `code_adapter.go` 的 `buildProjectMemoryRail` stub。文件发现使用内存缓存+文件系统快照失效策略，@include 递归展开，frontmatter paths 作用域限定，git worktree 检测。

**Tech Stack:** Go, filepath.Glob, os/exec（git 命令）, DeepAgentRail 基类, SystemPromptBuilderInterface

---

## File Structure

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/swarm/agents/harness/common/rails/project_memory/doc.go` | 新建 | 包文档 |
| `internal/swarm/agents/harness/common/rails/project_memory/files.go` | 新建 | 文件发现/加载/缓存/合并 |
| `internal/swarm/agents/harness/common/rails/project_memory/section.go` | 新建 | PromptSection 工厂 |
| `internal/swarm/agents/harness/common/rails/project_memory/files_test.go` | 新建 | files 单元测试 |
| `internal/swarm/agents/harness/common/rails/project_memory/section_test.go` | 新建 | section 单元测试 |
| `internal/swarm/agents/harness/common/rails/project_memory_rail.go` | 新建 | ProjectMemoryRail 结构体 |
| `internal/swarm/agents/harness/common/rails/project_memory_rail_test.go` | 新建 | Rail 单元测试 |
| `internal/swarm/agents/harness/common/rails/doc.go` | 修改 | 添加 project_memory/ 子包 |
| `internal/swarm/server/adapter/code_adapter.go` | 修改 | 回填 buildProjectMemoryRail |

---

### Task 1: project_memory 子包骨架（doc.go + 常量 + 结构体）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/project_memory/doc.go`
- Create: `internal/swarm/agents/harness/common/rails/project_memory/files.go`（仅常量+结构体+缓存声明，函数体暂用 panic 占位）
- Create: `internal/swarm/agents/harness/common/rails/project_memory/section.go`（仅常量，函数体暂用 panic 占位）

- [ ] **Step 1: 创建 project_memory 目录**

```bash
mkdir -p internal/swarm/agents/harness/common/rails/project_memory
```

- [ ] **Step 2: 创建 doc.go**

```go
// Package project_memory 提供项目记忆文件发现、加载、缓存和合并功能。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/project_memory/，
// 在 ProjectMemoryRail 的 before_model_call 钩子中使用。
//
// 功能包括：
//   - 多层级发现：managed → user → project (root → cwd) → local
//   - 固定文件名 + glob 扫描（+ 可选额外目录）
//   - git worktree 处理
//   - symlink 安全去重
//   - @include 展开
//   - frontmatter 剥离 + paths: 作用域限定
//   - 内存缓存 + 文件系统快照失效
//   - 软字符上限截断
//
// 文件目录：
//
//	project_memory/
//	├── doc.go           # 包文档
//	├── files.go         # 文件发现/加载/缓存/合并
//	└── section.go       # PromptSection 工厂
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/project_memory/
package project_memory
```

- [ ] **Step 3: 创建 files.go — 常量 + 结构体 + 缓存声明**

完整内容（对齐 Python files.py L48-134）。注意所有命名映射到 uapclaw：

```go
package project_memory

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
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
	// entries 路径 → (exists, isDir, mtime_ns, size)
	entries map[string]snapshotEntry
}

type snapshotEntry struct {
	exists  bool
	isDir   bool
	mtimeNs int64  // 0 表示无
	size    int64  // 0 表示无（目录也用 0）
}

// cacheEntry 缓存条目。
// Python: _DiscoveryCacheEntry (files.py L127-130)
type cacheEntry struct {
	files    []LoadedMemoryFile
	snapshot watchSnapshot
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ProjectRootMarkers 项目根目录标记文件。
	// Python: PROJECT_ROOT_MARKERS (files.py L48-57)
	ProjectRootMarkers = ".git:.uapclaw:.claude:pyproject.toml:package.json:Cargo.toml:go.mod:pom.xml"

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
	priorityManaged = 10
	// priorityUser user 层优先级
	priorityUser = 20
	// priorityProject project 层优先级
	priorityProject = 30
	// priorityLocal local 层优先级
	priorityLocal = 40
)

var (
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
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// discoveryCache 缓存：cacheKey → cacheEntry
	// Python: _DISCOVERY_CACHE (files.py L133)
	discoveryCache = make(map[cacheKey]*cacheEntry)

	// cacheMu 缓存互斥锁
	// Python: _CACHE_LOCK (files.py L134)
	cacheMu sync.RWMutex

	// pmLogComponent 日志组件
	pmLogComponent = logger.ComponentAgentServer

	// projectRootMarkersSlice 预切分的 markers 列表
	projectRootMarkersSlice []string
)

// cacheKey 缓存键
type cacheKey struct {
	workspace   string
	targetPath  string
	additionalDirs string // 用 joined string 做可比较 key
}

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
	// 实现在 Task 2
	panic("TODO: Task 2")
}

// DiscoverAndLoadMemoryFiles 发现并加载所有适用的 memory 文件。
// Python: discover_and_load_memory_files (files.py L174-333)
func DiscoverAndLoadMemoryFiles(workspace string, targetPath string, additionalDirectories []string) []LoadedMemoryFile {
	// 实现在 Task 4
	panic("TODO: Task 4")
}

// MergeMemoryContent 将文件合并为单个文本块，按优先级排列，超过 maxChars 时截断。
// Python: merge_memory_content (files.py L336-367)
func MergeMemoryContent(files []LoadedMemoryFile, maxChars int) string {
	// 实现在 Task 5
	panic("TODO: Task 5")
}

// GetLargeMemoryFiles 返回超过单文件字符阈值的警告列表。
// Python: get_large_memory_files (files.py L370-387)
func GetLargeMemoryFiles(files []LoadedMemoryFile, threshold int) []map[string]any {
	// 实现在 Task 5
	panic("TODO: Task 5")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 初始化 projectRootMarkersSlice
func init() {
	projectRootMarkersSlice = splitProjectRootMarkers()
}

func splitProjectRootMarkers() []string {
	markers := make([]string, 0, 8)
	for _, m := range filepath.SplitList(ProjectRootMarkers) {
		// ProjectRootMarkers 用 : 分隔（跨平台统一）
		m = strings.TrimSpace(m)
		if m != "" {
			markers = append(markers, m)
		}
	}
	return markers
}
```

注意：`ProjectRootMarkers` 用冒号分隔的字符串，在 `init()` 中切分为 `[]string`。这与 Go 的惯用法更一致，同时保证常量可比较。

实际上，Go 更好的做法是直接声明 `[]string`，因为 Go 的 `const` 不支持切片。改为 `var`：

```go
var (
	// projectRootMarkers 项目根目录标记文件。
	// Python: PROJECT_ROOT_MARKERS (files.py L48-57)
	projectRootMarkers = []string{
		".git", ".uapclaw", ".claude",
		"pyproject.toml", "package.json", "Cargo.toml", "go.mod", "pom.xml",
	}
)
```

删掉 `ProjectRootMarkers` 常量和 `splitProjectRootMarkers` / `init()` 函数，直接用 `projectRootMarkers` 变量。

- [ ] **Step 4: 创建 section.go — 常量 + 函数签名**

```go
package project_memory

import saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SectionName project_memory section 名称。
	// Python: SECTION_NAME = "project_memory" (section.py L13)
	SectionName = "project_memory"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildProjectMemorySection 构建 project_memory PromptSection。
// 内容为空时返回 nil。始终生成中英双语内容。
// Python: build_project_memory_section (section.py L28-51)
func BuildProjectMemorySection(content string, priority int) *saprompt.PromptSection {
	// 实现在 Task 6
	panic("TODO: Task 6")
}
```

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/agents/harness/common/rails/project_memory/...
```

Expected: 编译失败（panic 占位函数签名中引用了未 import 的 `strings` 等），修复后应通过

- [ ] **Step 6: 修复编译问题后提交**

```bash
git add internal/swarm/agents/harness/common/rails/project_memory/
git commit -m "feat(project_memory): add skeleton with constants, structs, and cache declarations"
```

---

### Task 2: files.go — 工具函数 + FindProjectRoot

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files.go`

实现所有底层工具函数 + `FindProjectRoot`。

- [ ] **Step 1: 写 failing test — FindProjectRoot**

在 `files_test.go` 中：

```go
package project_memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectRoot(t *testing.T) {
	// 在临时目录创建 .git marker
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	root := FindProjectRoot(subDir)
	if root == "" {
		t.Fatal("expected non-empty root")
	}
	// FindProjectRoot 返回 resolve 后的路径，比较前需 resolve
	resolved, _ := filepath.EvalSymlinks(tmpDir)
	got, _ := filepath.EvalSymlinks(root)
	if got != resolved {
		t.Fatalf("expected %s, got %s", resolved, got)
	}
}

func TestFindProjectRoot_无标记时返回空(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "a", "b")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 不创建任何 marker
	root := FindProjectRoot(subDir)
	if root != "" {
		t.Fatalf("expected empty, got %s", root)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run TestFindProjectRoot -v
```

Expected: panic 或 FAIL

- [ ] **Step 3: 实现 FindProjectRoot + 工具函数**

在 files.go 的非导出函数区块添加：

```go
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

// safeResolve 安全 resolve（不抛异常）。
// Python: _safe_resolve (files.py L926-930)
func safeResolve(p string) string {
	resolved, err := filepath.Abs(p)
	if err != nil {
		return p
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
	home := path.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// expandHome 展开路径中的 ~。
// Python: os.path.expanduser
func expandHome(p string) string {
	return path.ExpandHome(p)
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run TestFindProjectRoot -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/project_memory/
git commit -m "feat(project_memory): implement FindProjectRoot + utility functions"
```

---

### Task 3: files.go — Frontmatter 解析 + @include

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files.go`
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files_test.go`

实现 frontmatter 解析、@include 展开、paths 作用域匹配。

- [ ] **Step 1: 写 failing test — parseFrontmatter**

```go
func TestParseFrontmatter_基本(t *testing.T) {
	raw := "---\npaths: [\"src/**\", \"test/**\"]\n---\nBody content\n"
	fm, body := parseFrontmatter(raw)
	if len(fm) == 0 {
		t.Fatal("expected non-empty frontmatter")
	}
	paths, ok := fm["paths"]
	if !ok {
		t.Fatal("expected paths key")
	}
	list, ok := paths.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", paths)
	}
	if len(list) != 2 || list[0] != "src/**" || list[1] != "test/**" {
		t.Fatalf("unexpected paths: %v", list)
	}
	if body != "Body content\n" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestParseFrontmatter_无frontmatter(t *testing.T) {
	raw := "Just body\nNo frontmatter\n"
	fm, body := parseFrontmatter(raw)
	if len(fm) != 0 {
		t.Fatalf("expected empty frontmatter, got %v", fm)
	}
	if body != raw {
		t.Fatalf("expected body unchanged, got %q", body)
	}
}

func TestExtractIncludeSpec(t *testing.T) {
	tests := []struct {
		line     string
		expected string
	}{
		{"@include path/to/file.md", "path/to/file.md"},
		{"@path/to/file.md", "path/to/file.md"},
		{"@@not-an-include", ""},
		{"normal text", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractIncludeSpec(tt.line)
		if got != tt.expected {
			if tt.expected == "" && got == "" {
				continue
			}
			if got != tt.expected {
				t.Errorf("extractIncludeSpec(%q) = %q, want %q", tt.line, got, tt.expected)
			}
		}
	}
}

func TestParseFrontmatterValue_布尔(t *testing.T) {
	if parseFrontmatterValue("true") != true {
		t.Error("expected true")
	}
	if parseFrontmatterValue("false") != false {
		t.Error("expected false")
	}
}

func TestParseFrontmatterValue_内联列表(t *testing.T) {
	got := parseFrontmatterValue("[a, b, c]")
	list, ok := got.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", got)
	}
	if len(list) != 3 || list[0] != "a" || list[1] != "b" || list[2] != "c" {
		t.Fatalf("unexpected list: %v", list)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run "TestParseFrontmatter|TestExtractIncludeSpec" -v
```

- [ ] **Step 3: 实现 frontmatter 解析 + @include 函数**

在 files.go 非导出函数区块添加（全部对齐 Python files.py L100-820）：

```go
// frontmatterRe 匹配 YAML frontmatter 块。
// Python: _FRONTMATTER_RE (files.py L101)
var frontmatterRe = regexp.MustCompile(`(?s)^---\s*\n(.*?\n)---\s*(?:\n|\z)`)

// fenceRe 匹配代码围栏行。
// Python: _FENCE_RE (files.py L102)
var fenceRe = regexp.MustCompile(`^\s*(` + "`" + "`" + "`|~~~)`)

// parseFrontmatter 轻量 frontmatter 解析。
// Python: _parse_frontmatter (files.py L708-746)
func parseFrontmatter(raw string) (map[string]any, string) {
	match := frontmatterRe.FindStringSubmatchIndex(raw)
	if match == nil {
		return map[string]any{}, raw
	}
	fmText := raw[match[2]:match[3]]
	lines := strings.Split(fmText, "\n")
	result := make(map[string]any, 4)
	idx := 0
	for idx < len(lines) {
		line := lines[idx]
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") || !strings.Contains(line, ":") {
			idx++
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value != "" {
			result[key] = parseFrontmatterValue(value)
			idx++
			continue
		}
		// 块值
		var block []string
		idx++
		for idx < len(lines) {
			nxt := lines[idx]
			nxtStripped := strings.TrimSpace(nxt)
			if nxtStripped == "" {
				idx++
				continue
			}
			if !strings.HasPrefix(nxt, " ") && !strings.HasPrefix(nxt, "\t") &&
				strings.Contains(nxt, ":") && !strings.HasPrefix(nxtStripped, "-") {
				break
			}
			block = append(block, nxtStripped)
			idx++
		}
		result[key] = parseFrontmatterBlock(block)
	}
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
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		if parsed := parseInlineFrontmatterList(value); parsed != nil {
			return parsed
		}
	}
	lowered := strings.ToLower(value)
	switch lowered {
	case "true":
		return true
	case "false":
		return false
	case "null", "none":
		return nil
	}
	return strings.Trim(strings.Trim(value, `"`), `'`)
}

// parseInlineFrontmatterList 解析行内列表 [a, b, c]。
// Python: _parse_inline_frontmatter_list (files.py L765-807)
func parseInlineFrontmatterList(value string) []string {
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return []string{}
	}
	var items []string
	var current []rune
	var quote rune
	escaped := false
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
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
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
	tail := strings.TrimSpace(string(current))
	if tail != "" {
		items = append(items, strings.Trim(strings.Trim(tail, `"`), `'`))
	}
	if quote != 0 {
		return nil // 未闭合引号
	}
	return items
}

// parseFrontmatterBlock 解析缩进块。
// Python: _parse_frontmatter_block (files.py L810-819)
func parseFrontmatterBlock(lines []string) any {
	if len(lines) == 0 {
		return []string{}
	}
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

// extractIncludeSpec 从行中提取 include 路径。
// Python: _extract_include_spec (files.py L615-623)
func extractIncludeSpec(line string) string {
	stripped := strings.TrimSpace(line)
	if stripped == "" {
		return ""
	}
	if strings.HasPrefix(stripped, "@include ") {
		return strings.TrimSpace(stripped[len("@include "):])
	}
	if strings.HasPrefix(stripped, "@") && !strings.HasPrefix(stripped, "@@") {
		return strings.TrimSpace(stripped[1:])
	}
	return ""
}

// resolveIncludePath 解析 include 路径。
// Python: _resolve_include_path (files.py L626-636)
func resolveIncludePath(spec string, currentPath string) string {
	cleaned := strings.TrimSpace(spec)
	cleaned = strings.Trim(strings.Trim(cleaned, `"`), `'`)
	if cleaned == "" {
		return ""
	}
	if strings.HasPrefix(cleaned, "~/") {
		return expandHome(cleaned)
	}
	if filepath.IsAbs(cleaned) {
		return cleaned
	}
	dir := filepath.Dir(currentPath)
	resolved := filepath.Join(dir, cleaned)
	return safeResolve(resolved)
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

// frontmatterPathsMatch 检查 frontmatter 的 paths: 规则是否匹配当前目标路径。
// Python: _frontmatter_paths_match (files.py L639-648)
func frontmatterPathsMatch(frontmatter map[string]any, filePath string, targetPath string) bool {
	globs := extractPathsGlobs(frontmatter)
	if len(globs) == 0 {
		return true
	}
	return pathsMatchTarget(globs, filePath, targetPath)
}

// pathsMatchTarget 将 paths globs 与目标路径匹配。
// Python: _paths_match_target (files.py L663-705)
func pathsMatchTarget(globs []string, rulePath string, targetPath string) bool {
	projectRoot := FindProjectRoot(targetPath)
	var ruleBase string
	if projectRoot != "" && !isRelativeTo(rulePath, projectRoot) {
		ruleBase = projectRoot
	} else {
		ruleBase = filepath.Dir(rulePath)
		// 向上查找 .uapclaw 或 .claude 目录
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

	rel, err := filepath.Rel(ruleBase, targetPath)
	if err != nil {
		return false
	}
	// 转为 posix 风格路径（对齐 Python as_posix）
	relPosix := filepath.ToSlash(rel)
	relPosix = strings.Trim(relPosix, "/")

	var candidates []string
	if relPosix != "" {
		trail := pathNormalize(relPosix) + "/"
		dirMagic := relPosix + "/__dir__"
		candidates = []string{relPosix, trail, dirMagic}
	} else {
		candidates = []string{"", "/", "/__dir__", ".", "__dir__"}
	}

	for _, pattern := range globs {
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
func fnmatchMatch(name, pattern string) bool {
	// 用 filepath.Match 实现基本 glob 匹配
	matched, _ := filepath.Match(pattern, name)
	return matched
}

// pathNormalize 规范化 posix 路径（对齐 Python posixpath.normpath）
func pathNormalize(p string) string {
	p = filepath.ToSlash(p)
	// 简单的 normpath：去除多余的 ./ 和 //
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
```

需要在文件头部 import 添加 `"regexp"`。

- [ ] **Step 4: 实现 @include 展开函数**

```go
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

	for _, line := range strings.Split(body, "\n") {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			keptLines = append(keptLines, line)
			continue
		}
		if inFence {
			keptLines = append(keptLines, line)
			continue
		}
		includeSpec := extractIncludeSpec(line)
		if includeSpec == "" {
			keptLines = append(keptLines, line)
			continue
		}
		includePath := resolveIncludePath(includeSpec, currentPath)
		if includePath == "" {
			continue
		}
		watchPaths[safeResolve(filepath.Dir(includePath))] = true
		watchPaths[safeResolve(includePath)] = true
		info, err := os.Stat(includePath)
		if err != nil || info.IsDir() {
			continue
		}
		loadPath(includePath, kind, out, seen, targetPath, watchPaths)
	}

	return strings.Join(keptLines, "\n")
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run "TestParseFrontmatter|TestExtractIncludeSpec|TestParseFrontmatterValue" -v
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/project_memory/
git commit -m "feat(project_memory): implement frontmatter parsing, @include expansion, paths scoping"
```

---

### Task 4: files.go — Git Worktree + loadPath + 扫描函数 + DiscoverAndLoad

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files.go`
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files_test.go`

实现 git worktree 检测、loadPath、扫描函数、完整 DiscoverAndLoad。

- [ ] **Step 1: 写 failing test — DiscoverAndLoadMemoryFiles 基本场景**

```go
func TestDiscoverAndLoadMemoryFiles_项目根加载(t *testing.T) {
	tmpDir := t.TempDir()
	// 创建 .git marker
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	// 创建 UAPCLAWSWARM.md
	mdContent := "# Project Memory\nThis is project memory."
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte(mdContent), 0o644)

	files := DiscoverAndLoadMemoryFiles(tmpDir, tmpDir, nil)
	if len(files) == 0 {
		t.Fatal("expected at least one file")
	}
	if files[0].Kind != "project" {
		t.Fatalf("expected kind=project, got %s", files[0].Kind)
	}
	if files[0].Content != mdContent {
		t.Fatalf("content mismatch: %q", files[0].Content)
	}
}

func TestDiscoverAndLoadMemoryFiles_空目录不生成结果(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	// 不创建任何 .md 文件

	files := DiscoverAndLoadMemoryFiles(tmpDir, tmpDir, nil)
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run TestDiscoverAndLoadMemoryFiles -v
```

- [ ] **Step 3: 实现 git worktree + loadPath + 扫描函数 + DiscoverAndLoad**

在 files.go 非导出函数区块添加：

```go
// detectGitWorktree 检测 git worktree 信息。
// Python: _detect_git_worktree (files.py L822-834)
func detectGitWorktree(cwd string) *GitWorktreeInfo {
	gitExe, err := exec.LookPath("git")
	if err != nil {
		return nil
	}

	worktreeRoot := gitPath(cwd, gitExe, "rev-parse", "--show-toplevel")
	commonDir := gitPath(cwd, gitExe, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if worktreeRoot == "" || commonDir == "" {
		return nil
	}

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
	base := filepath.Base(commonDir)
	if base == ".git" {
		return filepath.Dir(commonDir)
	}
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
	if worktreeInfo == nil {
		return false
	}
	if worktreeInfo.CanonicalRoot == worktreeInfo.WorktreeRoot {
		return false
	}
	return isRelativeTo(directory, worktreeInfo.CanonicalRoot) &&
		!isRelativeTo(directory, worktreeInfo.WorktreeRoot)
}

// normalizeAdditionalDirectories 规范化额外扫描目录。
// Python: _normalize_additional_directories (files.py L395-418)
func normalizeAdditionalDirectories(dirs []string, workspace string) string {
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
	var result []string
	seen := make(map[string]bool)
	for _, raw := range dirs {
		candidate := expandHome(raw)
		if !filepath.IsAbs(candidate) && workspace != "" {
			candidate = filepath.Join(workspace, candidate)
		}
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
	for _, entry := range entries {
		rel := entry[0]
		kind := entry[1]
		watchPaths[safeResolve(baseDir)] = true
		absPath := filepath.Join(baseDir, rel)
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
	for _, pattern := range patterns {
		globBase := filepath.Join(baseDir, filepath.Dir(pattern))
		watchPaths[safeResolve(globBase)] = true
		matches, err := filepath.Glob(filepath.Join(baseDir, pattern))
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
	for _, rawPattern := range patterns {
		expanded := expandHome(rawPattern)
		watchPaths[safeResolve(filepath.Dir(expanded))] = true
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
	expanded := expandHome(raw)
	watchPaths[safeResolve(filepath.Dir(expanded))] = true
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
	resolved := safeResolve(filePath)
	watchPaths[resolved] = true
	if seen[resolved] {
		return
	}
	seen[resolved] = true

	rawBytes, err := os.ReadFile(filePath)
	if err != nil {
		logger.Warn(pmLogComponent).
			Str("path", resolved).
			Err(err).
			Msg("[project_memory] 读取文件失败")
		return
	}
	raw := string(rawBytes)

	frontmatter, body := parseFrontmatter(raw)
	if !frontmatterPathsMatch(frontmatter, resolved, targetPath) {
		return
	}

	bodyWithoutIncludes := expandIncludes(body, resolved, kind, out, seen, targetPath, watchPaths)
	bodyStripped := strings.TrimSpace(bodyWithoutIncludes)
	if bodyStripped == "" {
		return
	}

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
			entries[raw] = snapshotEntry{exists: false}
			continue
		}
		mtimeNs := info.ModTime().UnixNano()
		var size int64
		if !info.IsDir() {
			size = info.Size()
		}
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
```

然后实现 `DiscoverAndLoadMemoryFiles`（替换 panic 占位）：

```go
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
	for _, raw := range ManagedMemoryFiles {
		tryLoadSingle(raw, "managed", &files, seen, targetKey, watchPaths)
	}
	scanAbsoluteGlobs(ManagedMemoryGlobs, "managed", &files, seen, targetKey, watchPaths)

	// user 层
	for _, raw := range UserMemoryFiles {
		tryLoadSingle(raw, "user", &files, seen, targetKey, watchPaths)
	}
	scanAbsoluteGlobs(UserMemoryGlobs, "user", &files, seen, targetKey, watchPaths)

	// project 层（root → cwd 遍历）
	projectRoot := FindProjectRoot(workspaceKey)
	if projectRoot != "" {
		cwd := workspaceKey
		worktreeInfo := detectGitWorktree(cwd)
		scanRoot := projectRoot
		if worktreeInfo != nil && worktreeInfo.CanonicalRoot != worktreeInfo.WorktreeRoot &&
			isRelativeTo(projectRoot, worktreeInfo.CanonicalRoot) {
			scanRoot = worktreeInfo.CanonicalRoot
		}

		var walkDirs []string
		current := cwd
		for {
			walkDirs = append(walkDirs, current)
			if current == scanRoot || filepath.Dir(current) == current {
				break
			}
			current = filepath.Dir(current)
		}
		// 反转：从 root → cwd
		for i, j := 0, len(walkDirs)-1; i < j; i, j = i+1, j-1 {
			walkDirs[i], walkDirs[j] = walkDirs[j], walkDirs[i]
		}

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
	if normalizedAdditional != "" {
		for _, rawDir := range filepath.SplitList(normalizedAdditional) {
			watchPaths[safeResolve(rawDir)] = true
			scanRelativeFiles(rawDir, ProjectMemoryFiles, &files, seen, targetKey, watchPaths)
			scanRelativeGlobs(rawDir, ProjectMemoryGlobs, "project", &files, seen, targetKey, watchPaths)
			scanRelativeFiles(rawDir, LocalMemoryFiles, &files, seen, targetKey, watchPaths)
		}
	}

	// 按优先级排序
	sort.Slice(files, func(i, j int) bool {
		return files[i].Priority < files[j].Priority
	})

	snapshot := buildWatchSnapshot(watchPaths)
	cacheMu.Lock()
	discoveryCache[ck] = &cacheEntry{files: files, snapshot: snapshot}
	cacheMu.Unlock()

	return append([]LoadedMemoryFile(nil), files...)
}
```

需要在 import 中添加 `"context"`, `"os/exec"`, `"sort"`, `"time"`。

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run TestDiscoverAndLoadMemoryFiles -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/project_memory/
git commit -m "feat(project_memory): implement DiscoverAndLoadMemoryFiles with git worktree, scanning, caching"
```

---

### Task 5: files.go — MergeMemoryContent + GetLargeMemoryFiles + 缓存失效

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files.go`
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files_test.go`

- [ ] **Step 1: 写 failing test — MergeMemoryContent**

```go
func TestMergeMemoryContent_基本合并(t *testing.T) {
	files := []LoadedMemoryFile{
		{Path: "/a.md", Kind: "project", Content: "AAA", Priority: 30},
		{Path: "/b.md", Kind: "local", Content: "BBB", Priority: 40},
	}
	merged := MergeMemoryContent(files, DefaultMaxChars)
	if !strings.Contains(merged, "AAA") || !strings.Contains(merged, "BBB") {
		t.Fatalf("merged should contain both contents, got: %s", merged)
	}
	if !strings.Contains(merged, "project memory") || !strings.Contains(merged, "local memory") {
		t.Fatalf("merged should contain kind headers, got: %s", merged)
	}
}

func TestMergeMemoryContent_超限截断(t *testing.T) {
	files := []LoadedMemoryFile{
		{Path: "/a.md", Kind: "project", Content: strings.Repeat("A", 100), Priority: 30},
		{Path: "/b.md", Kind: "local", Content: strings.Repeat("B", 100), Priority: 40},
	}
	merged := MergeMemoryContent(files, 50)
	if !strings.Contains(merged, "truncated") {
		t.Fatal("expected truncation marker")
	}
}

func TestMergeMemoryContent_空列表(t *testing.T) {
	merged := MergeMemoryContent(nil, DefaultMaxChars)
	if merged != "" {
		t.Fatalf("expected empty, got %q", merged)
	}
}

func TestClearProjectMemoryCache(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("test"), 0o644)

	// 首次加载，应写入缓存
	DiscoverAndLoadMemoryFiles(tmpDir, tmpDir, nil)
	// 清除缓存
	ClearProjectMemoryCache(tmpDir)
	// 清除后重新加载应成功
	files := DiscoverAndLoadMemoryFiles(tmpDir, tmpDir, nil)
	if len(files) == 0 {
		t.Fatal("expected files after cache clear")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run "TestMergeMemoryContent|TestClearProjectMemoryCache" -v
```

- [ ] **Step 3: 实现 MergeMemoryContent + GetLargeMemoryFiles（替换 panic 占位）**

```go
func MergeMemoryContent(files []LoadedMemoryFile, maxChars int) string {
	var parts []string
	total := 0
	var truncatedAt string
	for _, f := range files {
		chunk := "### " + f.Kind + " memory -- " + short(f.Path) + "\n" + f.Content + "\n"
		if total+len(chunk) > maxChars {
			remaining := maxChars - total
			if remaining > 200 {
				parts = append(parts, chunk[:remaining])
			}
			truncatedAt = f.Path
			break
		}
		parts = append(parts, chunk)
		total += len(chunk)
	}
	merged := strings.TrimSpace(strings.Join(parts, "\n"))
	if truncatedAt != "" {
		logger.Warn(pmLogComponent).
			Int("max_chars", maxChars).
			Str("truncated_at", truncatedAt).
			Msg("[project_memory] 合并内容超出字符上限，已截断")
		merged += "\n\n<!-- project memory truncated (> max_chars) -->"
	}
	return merged
}

func GetLargeMemoryFiles(files []LoadedMemoryFile, threshold int) []map[string]any {
	var warnings []map[string]any
	for _, f := range files {
		charCount := len(f.Content)
		if charCount > threshold {
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
```

需要在 import 中添加 `"fmt"`。

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run "TestMergeMemoryContent|TestClearProjectMemoryCache" -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/project_memory/
git commit -m "feat(project_memory): implement MergeMemoryContent, GetLargeMemoryFiles, cache invalidation"
```

---

### Task 6: section.go — BuildProjectMemorySection

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/section.go`
- Create: `internal/swarm/agents/harness/common/rails/project_memory/section_test.go`

- [ ] **Step 1: 写 failing test**

```go
package project_memory

import (
	"strings"
	"testing"
)

func TestBuildProjectMemorySection_非空内容(t *testing.T) {
	section := BuildProjectMemorySection("Hello memory", 120)
	if section == nil {
		t.Fatal("expected non-nil section")
	}
	if section.Name != SectionName {
		t.Fatalf("expected name=%s, got %s", SectionName, section.Name)
	}
	if section.Priority != 120 {
		t.Fatalf("expected priority=120, got %d", section.Priority)
	}
	cn, ok := section.Content["cn"]
	if !ok || !strings.Contains(cn, "项目记忆") {
		t.Fatalf("cn content should contain 项目记忆, got %q", cn)
	}
	en, ok := section.Content["en"]
	if !ok || !strings.Contains(en, "Project Memory") {
		t.Fatalf("en content should contain Project Memory, got %q", en)
	}
}

func TestBuildProjectMemorySection_空内容(t *testing.T) {
	section := BuildProjectMemorySection("", 120)
	if section != nil {
		t.Fatal("expected nil for empty content")
	}
	section = BuildProjectMemorySection("   ", 120)
	if section != nil {
		t.Fatal("expected nil for whitespace content")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run TestBuildProjectMemorySection -v
```

- [ ] **Step 3: 实现 BuildProjectMemorySection（替换 panic 占位）**

```go
func BuildProjectMemorySection(content string, priority int) *saprompt.PromptSection {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	body := strings.TrimSpace(content)
	cn := "## 项目记忆（ProjectMemoryRail 自动加载）\n\n以下内容来自项目根、用户目录、本地私有文件的合并。修改磁盘文件即可在下一轮对话生效。\n\n" + body + "\n"
	en := "## Project Memory (auto-loaded by ProjectMemoryRail)\n\nThe following is merged from project root, user home, and local private files. Edits take effect on the next turn.\n\n" + body + "\n"
	section := saprompt.PromptSection{
		Name:     SectionName,
		Content:  map[string]string{"cn": cn, "en": en},
		Priority: priority,
	}
	return &section
}
```

需要在 section.go import 中添加 `"strings"` 和 saprompt。

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run TestBuildProjectMemorySection -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/project_memory/
git commit -m "feat(project_memory): implement BuildProjectMemorySection with bilingual content"
```

---

### Task 7: project_memory_rail.go — ProjectMemoryRail 结构体

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/project_memory_rail.go`
- Create: `internal/swarm/agents/harness/common/rails/project_memory_rail_test.go`

- [ ] **Step 1: 创建 project_memory_rail.go**

```go
package rails

import (
	"context"
	"os"
	"sort"
	"strings"

	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pm "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/project_memory"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProjectMemoryRail 项目记忆护栏 — 每次 before_model_call 刷新 project_memory section。
//
// 自动加载项目记忆文件（UAPCLAWSWARM.md / .uapclaw/rules/*.md 等），
// 合并后注入 system prompt。write-like 工具后显式失效缓存。
//
// Python: ProjectMemoryRail(DeepAgentRail) — project_memory_rail.py (222 行)
type ProjectMemoryRail struct {
	harnessrails.DeepAgentRail
	// workspacePath 构造时传入的 workspace 路径（私有属性，避免被 SetWorkspace 覆盖）
	workspacePath string
	// language 语言（cn/en）
	language string
	// maxChars 合并字符上限
	maxChars int
	// additionalDirectories 额外扫描目录
	additionalDirectories []string
	// systemPromptBuilder 注入的 builder
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// projectMemoryRailPriority ProjectMemoryRail 优先级
	// Python: ProjectMemoryRail.priority = 5 (同 RuntimePromptRail)
	projectMemoryRailPriority = 5

	// projectMemorySectionPriority project_memory section 优先级
	// Python: ProjectMemoryRail.SECTION_PRIORITY = 120
	projectMemorySectionPriority = 120
)

// ──────────────────────────── 全局变量 ────────────────────────────

// writeLikeTools write-like 工具集合，执行后需清除缓存。
// Python: ProjectMemoryRail.WRITE_LIKE_TOOLS (project_memory_rail.py L51-60)
var writeLikeTools = map[string]bool{
	"write_file":      true,
	"edit_file":       true,
	"write_text_file": true,
	"write":           true,
	"delete_file":     true,
	"delete":          true,
	"move_file":       true,
	"rename_file":     true,
}

// pmRailLogComponent 日志组件
var pmRailLogComponent = logger.ComponentAgentServer

// 编译时验证 ProjectMemoryRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ProjectMemoryRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProjectMemoryRail 创建 ProjectMemoryRail 实例。
// Python: ProjectMemoryRail.__init__ (project_memory_rail.py L67-83)
func NewProjectMemoryRail(workspaceDir string, language string, maxChars int, additionalDirs []string) *ProjectMemoryRail {
	r := &ProjectMemoryRail{
		workspacePath:         workspaceDir,
		language:              language,
		maxChars:              maxChars,
		additionalDirectories: additionalDirs,
	}
	if r.language == "" {
		r.language = "cn"
	}
	if r.maxChars == 0 {
		r.maxChars = pm.DefaultMaxChars
	}
	r.WithPriority(projectMemoryRailPriority)
	return r
}

// Init 初始化时获取 systemPromptBuilder。
// Python: ProjectMemoryRail.init (project_memory_rail.py L89-100)
func (r *ProjectMemoryRail) Init(agent agentinterfaces.BaseAgent) {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	if r.systemPromptBuilder == nil {
		logger.Warn(pmRailLogComponent).
			Str("rail", "ProjectMemoryRail").
			Msg("agent 没有 system_prompt_builder，ProjectMemoryRail 已禁用")
		return
	}
	logger.Info(pmRailLogComponent).
		Str("rail", "ProjectMemoryRail").
		Str("workspace", r.ResolveWorkspacePath()).
		Str("language", r.language).
		Msg("ProjectMemoryRail 已初始化")
}

// Uninit 清除缓存并移除注入的 section。
// Python: ProjectMemoryRail.uninit (project_memory_rail.py L102-111)
func (r *ProjectMemoryRail) Uninit(_ agentinterfaces.BaseAgent) {
	pm.ClearProjectMemoryCache(r.ResolveWorkspacePath())
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(pm.SectionName)
	}
}

// SetLanguage per-request 更新语言（值未变时 no-op）。
// Python: ProjectMemoryRail.set_language (project_memory_rail.py L117-119)
func (r *ProjectMemoryRail) SetLanguage(language string) {
	if language != "" && language != r.language {
		r.language = language
	}
}

// GetLanguage 返回当前语言设置。
// Python: ProjectMemoryRail.get_language (project_memory_rail.py L121-123)
func (r *ProjectMemoryRail) GetLanguage() string {
	return r.language
}

// SetAdditionalDirectories per-request 热更新额外扫描目录（realpath 去重）。
// Python: ProjectMemoryRail.set_additional_directories (project_memory_rail.py L126-142)
func (r *ProjectMemoryRail) SetAdditionalDirectories(dirs []string) {
	extra := dirs
	if extra == nil {
		extra = []string{}
	}
	// 合并构造时和运行时目录，按 realpath 去重
	baseResolved := make(map[string]bool, len(r.additionalDirectories))
	for _, d := range r.additionalDirectories {
		baseResolved[safeResolveDir(d)] = true
	}
	merged := make([]string, len(r.additionalDirectories))
	copy(merged, r.additionalDirectories)
	for _, d := range extra {
		resolved := safeResolveDir(d)
		if !baseResolved[resolved] {
			merged = append(merged, d)
			baseResolved[resolved] = true
		}
	}
	r.additionalDirectories = merged
}

// BeforeModelCall 模型调用前刷新 project_memory section。
// Python: ProjectMemoryRail.before_model_call (project_memory_rail.py L148-192)
func (r *ProjectMemoryRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	workspacePath := r.ResolveWorkspacePath()

	var files []pm.LoadedMemoryFile
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Warn(pmRailLogComponent).
					Str("rail", "ProjectMemoryRail").
					Str("workspace", workspacePath).
					Any("error", rec).
					Msg("[ProjectMemoryRail] discovery 发生异常")
				files = nil
			}
		}()
		files = pm.DiscoverAndLoadMemoryFiles(workspacePath, workspacePath, r.additionalDirectories)
	}()

	merged := pm.MergeMemoryContent(files, r.maxChars)

	// 移除旧 section
	r.systemPromptBuilder.RemoveSection(pm.SectionName)

	if strings.TrimSpace(merged) == "" {
		return nil
	}

	section := pm.BuildProjectMemorySection(merged, projectMemorySectionPriority)
	if section != nil {
		r.systemPromptBuilder.AddSection(*section)
	}

	return nil
}

// AfterToolCall write-like 工具后清除缓存。
// Python: ProjectMemoryRail.after_tool_call (project_memory_rail.py L194-199)
func (r *ProjectMemoryRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolName := ""
	if cbc != nil && cbc.Inputs() != nil {
		// 从 Inputs 中获取 tool_name
		if m, ok := cbc.Inputs().(map[string]any); ok {
			if tn, ok := m["tool_name"].(string); ok {
				toolName = strings.TrimSpace(tn)
			}
		}
	}
	if toolName == "" {
		return nil
	}
	if writeLikeTools[toolName] {
		pm.ClearProjectMemoryCache(r.ResolveWorkspacePath())
	}
	return nil
}

// ResolveWorkspacePath 解析 workspace 路径。
// Python: ProjectMemoryRail.resolve_workspace_path (project_memory_rail.py L205-218)
func (r *ProjectMemoryRail) ResolveWorkspacePath() string {
	ws := r.Workspace()
	if ws != nil && ws.RootPath() != "" {
		return ws.RootPath()
	}
	return r.workspacePath
}

// GetCallbacks 合并回调。
func (r *ProjectMemoryRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	if _, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]; !ok {
		callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
			return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}
	}
	if _, ok := callbacks[agentinterfaces.CallbackAfterToolCall]; !ok {
		callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
			return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeResolveDir 安全解析目录路径
func safeResolveDir(dir string) string {
	resolved, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	// 尝试 filepath.EvalSymlinks 获取真实路径
	if evaled, err := filepath.EvalSymlinks(resolved); err == nil {
		return evaled
	}
	return resolved
}
```

需要 import 添加 `"path/filepath"`。

- [ ] **Step 2: 写测试 — project_memory_rail_test.go**

```go
package rails

import (
	"os"
	"path/filepath"
	"testing"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	pm "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/project_memory"
)

// mockBuilder 用于测试的 mock SystemPromptBuilder
type mockBuilder struct {
	sections map[string]saprompt.PromptSection
	lang     string
}

func newMockBuilder() *mockBuilder {
	return &mockBuilder{sections: make(map[string]saprompt.PromptSection)}
}

func (m *mockBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	m.sections[section.Name] = section
	return nil
}
func (m *mockBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(m.sections, name)
	return nil
}
func (m *mockBuilder) Language() string          { return m.lang }
func (m *mockBuilder) SetLanguage(lang string)    { m.lang = lang }
func (m *mockBuilder) GetSection(name string) *saprompt.PromptSection {
	s, ok := m.sections[name]
	if !ok {
		return nil
	}
	return &s
}
func (m *mockBuilder) HasSection(name string) bool { _, ok := m.sections[name]; return ok }

func TestNewProjectMemoryRail(t *testing.T) {
	rail := NewProjectMemoryRail("/tmp/workspace", "cn", 0, nil)
	if rail.language != "cn" {
		t.Fatalf("expected cn, got %s", rail.language)
	}
	if rail.maxChars != pm.DefaultMaxChars {
		t.Fatalf("expected %d, got %d", pm.DefaultMaxChars, rail.maxChars)
	}
}

func TestProjectMemoryRail_SetLanguage(t *testing.T) {
	rail := NewProjectMemoryRail("/tmp/workspace", "cn", 0, nil)
	rail.SetLanguage("en")
	if rail.language != "en" {
		t.Fatalf("expected en, got %s", rail.language)
	}
	// 值未变时 no-op
	rail.SetLanguage("en")
	if rail.language != "en" {
		t.Fatalf("expected en (unchanged), got %s", rail.language)
	}
}

func TestProjectMemoryRail_BeforeModelCall_注入section(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("Test memory content"), 0o644)

	rail := NewProjectMemoryRail(tmpDir, "cn", 0, nil)
	builder := newMockBuilder()
	rail.systemPromptBuilder = builder

	ctx := context.Background()
	err := rail.BeforeModelCall(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	section, ok := builder.sections[pm.SectionName]
	if !ok {
		t.Fatal("expected project_memory section to be added")
	}
	cn, ok := section.Content["cn"]
	if !ok || cn == "" {
		t.Fatal("expected cn content in section")
	}
}

func TestProjectMemoryRail_BeforeModelCall_空目录(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	// 不创建 .md 文件

	rail := NewProjectMemoryRail(tmpDir, "cn", 0, nil)
	builder := newMockBuilder()
	rail.systemPromptBuilder = builder

	err := rail.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := builder.sections[pm.SectionName]; ok {
		t.Fatal("expected no section for empty workspace")
	}
}

func TestProjectMemoryRail_ResolveWorkspacePath(t *testing.T) {
	rail := NewProjectMemoryRail("/tmp/ws", "cn", 0, nil)
	if rail.ResolveWorkspacePath() != "/tmp/ws" {
		t.Fatalf("expected /tmp/ws, got %s", rail.ResolveWorkspacePath())
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/ -run TestProjectMemoryRail -v
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/project_memory_rail.go internal/swarm/agents/harness/common/rails/project_memory_rail_test.go
git commit -m "feat(rails): implement ProjectMemoryRail with BeforeModelCall/AfterToolCall"
```

---

### Task 8: 回填 code_adapter.go + 更新 doc.go

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go`
- Modify: `internal/swarm/agents/harness/common/rails/doc.go`

- [ ] **Step 1: 修改 code_adapter.go — projectMemoryRail 字段类型**

将第 68 行：
```go
projectMemoryRail sainterfaces.AgentRail
```
改为：
```go
projectMemoryRail *rails.ProjectMemoryRail
```

- [ ] **Step 2: 修改 code_adapter.go — buildProjectMemoryRail 方法体**

将第 1028-1031 行：
```go
func (c *CodeAdapter) buildProjectMemoryRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 ProjectMemoryRail
	return nil
}
```
改为：
```go
func (c *CodeAdapter) buildProjectMemoryRail() *rails.ProjectMemoryRail {
	// Python: JiuwenClawCodeAdapter._build_project_memory_rail() (interface_code.py)
	workspaceDir := c.deep.agentWorkspaceDir
	if c.deep.projectDir != "" {
		workspaceDir = c.deep.projectDir
	}
	return rails.NewProjectMemoryRail(
		workspaceDir,
		c.deep.runtimeLanguage,
		pm.DefaultMaxChars,
		nil, // additionalDirectories 由 SetAdditionalDirectories 热更新
	)
}
```

需要在 code_adapter.go import 中添加 `pm "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/project_memory"`（如果 rails 已导入则不需要单独导入 pm）。

- [ ] **Step 3: 回填 ⤵️ 标记 — trusted_dirs 注入**

在第 588 行附近（`updateRuntimeConfig` 方法中），将：
```go
	// ⤵️ 待后续章节回填:
	// - ProjectMemoryRail 语言同步 + trusted_dirs 注入
```
改为：
```go
	// ✅ ProjectMemoryRail 语言同步 + trusted_dirs 注入
	if c.projectMemoryRail != nil {
		c.projectMemoryRail.SetLanguage(resolvedLanguage)
		if len(config.TrustedDirs) > 0 {
			c.projectMemoryRail.SetAdditionalDirectories(config.TrustedDirs)
		}
	}
```

- [ ] **Step 4: 更新 rails/doc.go**

添加 project_memory/ 子包和 project_memory_rail.go 条目。

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/...
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/adapter/code_adapter.go internal/swarm/agents/harness/common/rails/doc.go
git commit -m "feat(adapter): backfill buildProjectMemoryRail + trusted_dirs injection + update doc.go"
```

---

### Task 9: 补充完整测试覆盖 + 全量编译 + 覆盖率

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files_test.go`
- Modify: `internal/swarm/agents/harness/common/rails/project_memory_rail_test.go`

补充更多测试场景对齐 Python 测试文件。

- [ ] **Step 1: 补充 files_test.go 更多场景**

补充以下测试（对齐 Python test_project_memory_rail.py + test_project_memory.py）：
- `TestDiscoverAndLoadMemoryFiles_Glob扫描` — .uapclaw/rules/*.md
- `TestDiscoverAndLoadMemoryFiles_Local优先级最高` — UAPCLAWSWARM.local.md 覆盖
- `TestDiscoverAndLoadMemoryFiles_Include展开` — @include 指令
- `TestDiscoverAndLoadMemoryFiles_FrontmatterPaths作用域` — paths 限定
- `TestDiscoverAndLoadMemoryFiles_缓存快照失效` — 文件修改后重载
- `TestDetectGitWorktree_非Git目录` — 返回 nil
- `TestGetLargeMemoryFiles_超限文件`

- [ ] **Step 2: 补充 project_memory_rail_test.go 更多场景**

- `TestProjectMemoryRail_AfterToolCall_清除缓存` — write-like 工具后缓存被清除
- `TestProjectMemoryRail_Uninit_移除section` — uninit 后 section 被移除
- `TestProjectMemoryRail_SetAdditionalDirectories_去重` — realpath 去重

- [ ] **Step 3: 运行全量测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/... -v -cover
```

Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 4: 运行 adapter 包编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/
git commit -m "test(project_memory): add comprehensive test coverage for ProjectMemoryRail"
```

---

### Task 10: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.6.3-10 行**

将 `ProjectMemory` 从未完成标记为已完成：

```
| 10.6.3-10 | 🔄 | Swarm Rails | AskUser✅/Avatar✅/Permissions✅/Interrupt✅/ProjectMemory✅/ResponsePrompt/RuntimePrompt✅/StreamEvent |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: mark ProjectMemory (10.6.7) as completed in IMPLEMENTATION_PLAN"
```
