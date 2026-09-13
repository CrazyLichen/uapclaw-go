# ProjectMemoryRail (10.6.7) 设计文档

## 概述

实现 ProjectMemoryRail，在每次 LLM 调用前动态注入项目记忆 PromptSection，完整对齐 Python `jiuwenswarm/agents/harness/common/rails/project_memory/` + `project_memory_rail.py` 的所有功能。

## 命名映射（Python → Go）

| Python | Go |
|--------|-----|
| `jiuwenswarm` | `uapclawswarm` |
| `JIUWENSWARM` | `UAPCLAWSWARM` |
| `.jiuwen` | `.uapclaw` |
| `~/.jiuwen` | `~/.uapclaw` |
| `/etc/jiuwen` | `/etc/uapclaw` |
| `JIUWENSWARM.md` | `UAPCLAWSWARM.md` |
| `JIUWENSWARM.local.md` | `UAPCLAWSWARM.local.md` |
| `JIUWENSWARM_ADDITIONAL_DIRECTORIES` | `UAPCLAWSWARM_ADDITIONAL_DIRECTORIES` |

## 在 Agent 会话中的流程位置与作用

### 流程位置

```
用户消息 → Gateway → AgentServer → Adapter.ProcessMessage
                                    ↓
                            DeepAgent.Run()
                                    ↓
                    ┌─── before_model_call (回调链) ───┐
                    │                                  │
                    │  RuntimePromptRail (priority=5)  │  ← 注入时间/环境/语言等运行时 section
                    │  ProjectMemoryRail (priority=5)  │  ← 注入项目记忆 section
                    │  UserHookRail                    │  ← 用户自定义 hook
                    │  ...                             │
                    └──────────────────────────────────┘
                                    ↓
                        LLM 模型调用
                                    ↓
                    ┌─── after_tool_call (回调链) ─────┐
                    │  ProjectMemoryRail               │  ← write-like 工具后清除缓存
                    │  UserHookRail                    │
                    │  ...                             │
                    └──────────────────────────────────┘
                                    ↓
                        工具执行 / 模型响应
```

### 核心作用

| 作用 | 说明 |
|------|------|
| **自动加载项目记忆** | 扫描项目根目录的 `UAPCLAWSWARM.md` / `.uapclaw/rules/*.md` 等文件，合并后注入 system prompt |
| **多层级优先级合并** | managed(10) < user(20) < project(30) < local(40)，低优先级内容被高优先级覆盖 |
| **缓存 + 自动失效** | 文件发现结果缓存（避免每轮 IO），write-like 工具后显式失效，保证磁盘修改在下一轮对话生效 |
| **支持 `@include` 展开** | UAPCLAWSWARM.md 中可写 `@include path/to/other.md` 引入其他文件 |
| **支持 frontmatter paths 作用域** | YAML frontmatter 的 `paths:` 字段限定该规则只对特定路径生效 |
| **Git worktree 处理** | 检测 git worktree，避免 canonical 仓库和 worktree 重复加载 |

## 文件结构

| 文件 | 操作 | 职责 | 对齐 Python |
|------|------|------|-------------|
| `internal/swarm/agents/harness/common/rails/project_memory/doc.go` | 新建 | 包文档 | — |
| `internal/swarm/agents/harness/common/rails/project_memory/files.go` | 新建 | 文件发现/加载/缓存/合并 | `project_memory/files.py` (963行) |
| `internal/swarm/agents/harness/common/rails/project_memory/section.go` | 新建 | PromptSection 工厂 | `project_memory/section.py` (55行) |
| `internal/swarm/agents/harness/common/rails/project_memory/files_test.go` | 新建 | files 单元测试 | `test_project_memory_rail.py` + `test_project_memory.py` |
| `internal/swarm/agents/harness/common/rails/project_memory/section_test.go` | 新建 | section 单元测试 | — |
| `internal/swarm/agents/harness/common/rails/project_memory_rail.go` | 新建 | ProjectMemoryRail 结构体 | `project_memory_rail.py` (222行) |
| `internal/swarm/agents/harness/common/rails/project_memory_rail_test.go` | 新建 | Rail 单元测试 | `test_project_memory_rail.py` |
| `internal/swarm/agents/harness/common/rails/doc.go` | 修改 | 添加 project_memory/ 子包条目 | — |
| `internal/swarm/server/adapter/code_adapter.go` | 修改 | 回填 buildProjectMemoryRail + trusted_dirs | `interface_code.py` |

## 子包 `project_memory/` 详细设计

### files.go — 文件发现/加载/缓存/合并

#### 常量

| Go 常量 | Python 对应 | 值 |
|---------|------------|-----|
| `ProjectRootMarkers` | `PROJECT_ROOT_MARKERS` | `[]string{".git", ".uapclaw", ".claude", "pyproject.toml", "package.json", "Cargo.toml", "go.mod", "pom.xml"}` |
| `ProjectMemoryFiles` | `PROJECT_MEMORY_FILES` | `[][2]string{{"UAPCLAWSWARM.md", "project"}, {".uapclaw/UAPCLAWSWARM.md", "project"}}`（对齐 Python 两个条目） |
| `LocalMemoryFiles` | `LOCAL_MEMORY_FILES` | `[][2]string{{"UAPCLAWSWARM.local.md", "local"}}` |
| `ProjectMemoryGlobs` | `PROJECT_MEMORY_GLOBS` | `[]string{".uapclaw/rules/*.md"}` |
| `UserMemoryFiles` | `USER_MEMORY_FILES` | `[]string{"~/.uapclaw/UAPCLAWSWARM.md"}` |
| `UserMemoryGlobs` | `USER_MEMORY_GLOBS` | `[]string{"~/.uapclaw/rules/*.md"}` |
| `ManagedMemoryFiles` | `MANAGED_MEMORY_FILES` | `[]string{"/etc/uapclaw/UAPCLAWSWARM.md"}` |
| `ManagedMemoryGlobs` | `MANAGED_MEMORY_GLOBS` | `[]string{"/etc/uapclaw/rules/*.md"}` |
| `AdditionalDirectoriesEnv` | `ADDITIONAL_DIRECTORIES_ENV` | `"UAPCLAWSWARM_ADDITIONAL_DIRECTORIES"` |
| `DefaultMaxChars` | `DEFAULT_MAX_CHARS` | `60000` |
| `MaxMemoryCharCount` | `MAX_MEMORY_CHARACTER_COUNT` | `40000` |
| `priorityManaged` | `PRIORITY["managed"]` | `10` |
| `priorityUser` | `PRIORITY["user"]` | `20` |
| `priorityProject` | `PRIORITY["project"]` | `30` |
| `priorityLocal` | `PRIORITY["local"]` | `40` |

#### 结构体

```go
// LoadedMemoryFile 已加载的记忆文件
type LoadedMemoryFile struct {
    Path        string         // 文件绝对路径
    Kind        string         // 层级：managed/user/project/local
    Content     string         // 文件内容（frontmatter 剥离后）
    Frontmatter map[string]any // 解析后的 frontmatter
    Priority    int            // 优先级数值
}

// GitWorktreeInfo Git worktree 信息
type GitWorktreeInfo struct {
    WorktreeRoot  string // worktree 根目录
    CanonicalRoot string // canonical 仓库根目录
}
```

#### 缓存结构

```go
type cacheEntry struct {
    files    []LoadedMemoryFile
    snapshot watchSnapshot
}

type watchSnapshot struct {
    mtimes map[string]time.Time
    sizes  map[string]int64
}

var (
    cache   = make(map[string]*cacheEntry)
    cacheMu sync.RWMutex
)
```

缓存逻辑（对齐 Python `_cache` + `_watch_snapshot`）：
1. 首次调用 → 扫描磁盘，存入 `cache[workspace]`，同时记录 `watchSnapshot`（每个文件的 mtime+size）
2. 后续调用 → 先比对当前磁盘快照与缓存快照，无变化则直接返回缓存
3. `ClearProjectMemoryCache(workspace)` → 删除缓存+快照
4. `AfterToolCall` → 显式调用 `ClearProjectMemoryCache`

```go
// buildWatchSnapshot 对齐 Python _build_watch_snapshot
func buildWatchSnapshot(paths []string) watchSnapshot

// snapshotUnchanged 对比两个快照是否相同
func snapshotUnchanged(a, b watchSnapshot) bool
```

#### 导出函数

| Go 函数 | Python 对应 | 作用 |
|---------|------------|------|
| `ClearProjectMemoryCache(workspace string)` | `clear_project_memory_cache` | 清除缓存 |
| `FindProjectRoot(cwd string) string` | `find_project_root` | 向上遍历查找项目根 |
| `DiscoverAndLoadMemoryFiles(workspace string, targetPath string, additionalDirectories []string) []LoadedMemoryFile` | `discover_and_load_memory_files` | 发现并加载所有 memory 文件 |
| `MergeMemoryContent(files []LoadedMemoryFile, maxChars int) string` | `merge_memory_content` | 按优先级合并，超限截断 |
| `GetLargeMemoryFiles(files []LoadedMemoryFile, threshold int) []map[string]any` | `get_large_memory_files` | 返回超限文件警告 |

#### 非导出函数（全部对齐 Python 内部函数）

| Go 函数 | Python 对应 |
|---------|------------|
| `normalizeAdditionalDirectories(cwd string, dirs []string) []string` | `_normalize_additional_directories` |
| `scanRelativeFiles(base string, entries [][2]string, kind string, out *[]LoadedMemoryFile, seen map[string]bool)` | `_scan_relative_files` |
| `scanRelativeGlobs(base string, patterns []string, kind string, out *[]LoadedMemoryFile, seen map[string]bool)` | `_scan_relative_globs` |
| `scanAbsoluteGlobs(patterns []string, kind string, out *[]LoadedMemoryFile, seen map[string]bool)` | `_scan_absolute_globs` |
| `tryLoadSingle(raw string, kind string, out *[]LoadedMemoryFile, seen map[string]bool)` | `_try_load_single` |
| `loadPath(path string, kind string, out *[]LoadedMemoryFile, seen map[string]bool)` | `_load_path` |
| `expandIncludes(body string, currentPath string, kind string, out *[]LoadedMemoryFile, seen map[string]bool, depth int) string` | `_expand_includes` |
| `extractIncludeSpec(line string) string` | `_extract_include_spec` |
| `resolveIncludePath(spec string, currentPath string) string` | `_resolve_include_path` |
| `frontmatterPathsMatch(frontmatter map[string]any, path string, targetPath string) bool` | `_frontmatter_paths_match` |
| `extractPathsGlobs(frontmatter map[string]any) []string` | `_extract_paths_globs` |
| `pathsMatchTarget(globs []string, rulePath string, targetPath string) bool` | `_paths_match_target` |
| `parseFrontmatter(raw string) (map[string]any, string)` | `_parse_frontmatter` |
| `parseFrontmatterValue(value string) any` | `_parse_frontmatter_value` |
| `parseInlineFrontmatterList(value string) []string` | `_parse_inline_frontmatter_list` |
| `parseFrontmatterBlock(lines []string) []string` | `_parse_frontmatter_block` |
| `detectGitWorktree(cwd string) *GitWorktreeInfo` | `_detect_git_worktree` |
| `shouldSkipProjectDir(dir string, worktreeInfo *GitWorktreeInfo) bool` | `_should_skip_project_dir` |
| `buildWatchSnapshot(paths []string) watchSnapshot` | `_build_watch_snapshot` |
| `isRelativeTo(path string, root string) bool` | `_is_relative_to` |
| `safeResolve(path string) string` | `_safe_resolve` |
| `short(path string) string` | `_short` |

### section.go — PromptSection 工厂

```go
const SectionName = "project_memory"

// BuildProjectMemorySection 构建 project_memory PromptSection。
// 内容为空时返回 nil。始终生成中英双语内容。
func BuildProjectMemorySection(content string, priority int) *saprompt.PromptSection
```

双语内容（对齐 Python）：
- 中文头：`## 项目记忆（ProjectMemoryRail 自动加载）`
- 中文注：`以下内容来自项目根、用户目录、本地私有文件的合并。修改磁盘文件即可在下一轮对话生效。`
- 英文头：`## Project Memory (auto-loaded by ProjectMemoryRail)`
- 英文注：`The following is merged from project root, user home, and local private files. Edits take effect on the next turn.`

### @include 展开机制

Python 支持 `@include path/to/file.md` 和 `@path/to/file.md` 语法，递归展开，防止循环引用。

```go
func expandIncludes(body string, currentPath string, kind string,
    out *[]LoadedMemoryFile, seen map[string]bool, depth int) string
```

解析规则（对齐 Python）：
- `@include path` — 显式 include 语法
- `@path` — 简写语法（行首 `@` + 路径）
- 路径解析优先级：`~/` → `$HOME/`，`/` → 绝对路径，其余相对于 currentPath
- 最大递归深度 10 层
- `seen` map 按 `safeResolve` 后的路径去重
- 目标文件不存在时 warn 日志，保留原始 `@include` 行

### Frontmatter 解析

Python 用轻量自定义解析器（不用 PyYAML），Go 同样不引入 YAML 库：

```go
// parseFrontmatter 轻量 frontmatter 解析
// 输入：原始文件内容
// 输出：(frontmatter map, 去除 frontmatter 后的 body)
// 格式：--- 开头和结尾的 YAML 块
func parseFrontmatter(raw string) (map[string]any, string)

// parseFrontmatterValue 解析单个 frontmatter 值
// 支持：true/false → bool, [a, b, c] → []string, 其余 → string
func parseFrontmatterValue(value string) any

// extractPathsGlobs 从 frontmatter 提取 paths glob 列表
// 支持：paths: [a, b] 和 paths: a 两种格式
func extractPathsGlobs(frontmatter map[string]any) []string

// pathsMatchTarget 用 filepath.Match 匹配 target_path
// 注意：Go 的 filepath.Match 仅支持 * 和 ?，不支持 [seq] 语法，
// 与 Python fnmatch 有差异。但 Python 实际 paths 规则中未使用 [seq]，
// 因此 filepath.Match 足以满足对齐需求。
func pathsMatchTarget(globs []string, rulePath string, targetPath string) bool
```

### Git Worktree 检测

```go
// detectGitWorktree 检测 git worktree 信息
// 读取 .git 文件内容：若包含 "gitdir:" 前缀则为 worktree
// 从 gitdir 路径反推 canonical 仓库根目录
func detectGitWorktree(cwd string) *GitWorktreeInfo

// shouldSkipProjectDir 判断是否跳过 canonical 仓库的 project memory
// 避免在 worktree 和 canonical 仓库中重复加载
func shouldSkipProjectDir(dir string, worktreeInfo *GitWorktreeInfo) bool
```

### MergeMemoryContent 截断逻辑

1. 按 priority 升序排列文件（managed 10 < user 20 < project 30 < local 40）
2. 同 priority 内按路径字母序
3. 逐文件拼接，每个文件用 `---\n# Source: {short_path}\n---\n{content}\n\n` 格式
4. 总字符数超过 maxChars → 在截断点插入 `... (truncated, {remaining} more files)`
5. 完全空 → 返回空字符串

## project_memory_rail.go — Rail 结构体

```go
type ProjectMemoryRail struct {
    harnessrails.DeepAgentRail

    workspacePath         string
    language              string
    maxChars              int
    additionalDirectories []string
    systemPromptBuilder   *saprompt.SystemPromptBuilder
}
```

类常量：
```go
var WriteLikeTools = map[string]bool{
    "write_file": true, "edit_file": true, "write_text_file": true,
    "write": true, "delete_file": true, "delete": true,
    "move_file": true, "rename_file": true,
}
const SectionPriority = 120
```

方法：

| Go 方法 | Python 对应 | 作用 |
|---------|------------|------|
| `NewProjectMemoryRail(workspace, language, maxChars, additionalDirs)` | `__init__` | 构造 |
| `Init(agent)` | `init` | 获取 systemPromptBuilder |
| `Uninit(agent)` | `uninit` | 清缓存 + 移除 section |
| `SetLanguage(lang)` | `set_language` | 按请求切换语言（值未变时 no-op） |
| `GetLanguage() string` | `get_language` | 返回当前语言 |
| `SetAdditionalDirectories(dirs)` | `set_additional_directories` | 热更新额外目录（realpath 去重） |
| `BeforeModelCall(ctx)` | `before_model_call` | 发现→合并→remove→add section |
| `AfterToolCall(ctx)` | `after_tool_call` | write-like 工具后清除缓存 |
| `ResolveWorkspacePath() string` | `resolve_workspace_path` | workspace.root_path 优先 |

BeforeModelCall 核心流程：
1. 检查 `systemPromptBuilder` 是否存在，无则直接返回
2. 调用 `DiscoverAndLoadMemoryFiles`（含缓存+快照比对）
3. 调用 `MergeMemoryContent` 合并
4. `RemoveSection(SectionName)` 移除旧 section
5. 内容非空时 `BuildProjectMemorySection` + `AddSection`

## 回填点

实现完成后需回填以下已有 stub：

1. **`code_adapter.go:68`** — `projectMemoryRail` 字段类型从 `sainterfaces.AgentRail` 改为 `*rails.ProjectMemoryRail`
2. **`code_adapter.go:874-878`** — `buildProjectMemoryRail()` 从 `return nil` 改为真实构建
3. **`code_adapter.go:1028-1031`** — 同上，方法体实现
4. **`code_adapter.go:588`** — ⤵️ 回填：`ProjectMemoryRail 语言同步 + trusted_dirs 注入`
5. **`rails/doc.go`** — 添加 `project_memory/` 子包和 `project_memory_rail.go` 条目

## 错误处理策略

对齐 Python 的防御性策略：

| 场景 | 处理 |
|------|------|
| `DiscoverAndLoadMemoryFiles` 中单文件 IO 失败 | 跳过该文件，warn 日志，不中断整体发现 |
| `BeforeModelCall` 中 discovery 抛异常 | catch 后 warn 日志，files 置空，不中断 model call |
| `RemoveSection` / `AddSection` 失败 | catch 后 warn 日志，不中断 |
| `@include` 目标文件不存在 | warn 日志，保留原始 `@include` 行 |
| frontmatter 解析失败 | 忽略 frontmatter，保留全部内容 |
| `FindProjectRoot` 遍历到根目录无 marker | 返回空字符串，由调用方处理 |

## 日志设计

- 组件：`logger.ComponentAgentServer`（属于 server 层 Rails）
- Python `[ProjectMemoryRail]` 前缀 → Go `logger.Info(logComponent).Str("rail", "ProjectMemoryRail")`
- 对齐 Python 所有 `logger.warning` / `logger.info` / `logger.exception` 调用点

## 测试策略

| 文件 | 覆盖范围 | 对齐 Python 测试 |
|------|---------|-----------------|
| `project_memory/files_test.go` | FindProjectRoot / DiscoverAndLoad / MergeContent / @include / frontmatter paths / git worktree / 缓存快照 / 额外目录 | `test_project_memory_rail.py` 590行 + `test_project_memory.py` 723行 |
| `project_memory/section_test.go` | BuildProjectMemorySection 空内容/非空/双语/priority | section.py 逻辑简单，3-5 个测试 |
| `project_memory_rail_test.go` | Init/Uninit/SetLanguage/SetAdditionalDirectories/BeforeModelCall/AfterToolCall/ResolveWorkspacePath | 对齐 `test_project_memory_rail.py` |

测试使用 `t.TempDir()` 创建临时目录，不依赖外部环境。

## 与 Python 源码对应关系

| Python 文件 | 行数 | Go 对应 |
|------------|------|---------|
| `project_memory_rail.py` | 222 行 | `project_memory_rail.go` |
| `project_memory/__init__.py` | 48 行 | `project_memory/doc.go` |
| `project_memory/files.py` | 963 行 | `project_memory/files.go` |
| `project_memory/section.py` | 55 行 | `project_memory/section.go` |
| `memory_rpc.py` | 474 行 | 不在本次范围（10.6.13-18） |
| `interface_code.py` | 引用 | `code_adapter.go` 回填 |

## 前置依赖状态

全部已就绪，无需额外实现：

| 依赖 | 状态 |
|------|------|
| `DeepAgentRail` 基类 | ✅ 已实现 |
| `SystemPromptBuilder`（AddSection/RemoveSection） | ✅ 已实现 |
| `PromptSection`（多语言 content） | ✅ 已实现 |
| `workspace.Workspace`（root_path） | ✅ 已实现 |
| `callback.AgentCallbackContext` | ✅ 已实现 |
| `code_adapter.go` 的 `buildProjectMemoryRail()` stub | ✅ 占位已就绪 |
