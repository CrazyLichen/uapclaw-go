# 9.66a WorktreeManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整移植 Python `openjiuwen/harness/tools/worktree/` 包，实现 Git Worktree 隔离系统（12个新文件 + 6处回填）。

**Architecture:** WorktreeManager 是核心管理器（生命周期），GitBackend 是可插拔后端（默认用 git worktree 命令），WorktreeRail 注入 Enter/ExitWorktreeTool 并桥接 Session 状态持久化，WorktreeLifecycleRail 提供 hook 扩展点。WorktreeSessionState 通过 context.Value 指针传播（对齐 CwdState/SessionState 模式）。

**Tech Stack:** Go 1.22+, os/exec (git), os/symlink, sync.RWMutex, context.Value, DeepAgentRail, Tool interface

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/agentcore/harness/tools/worktree/slug.go` | Slug 校验 + 分支名/路径生成 |
| 创建 | `internal/agentcore/harness/tools/worktree/slug_test.go` | Slug 测试 |
| 创建 | `internal/agentcore/harness/tools/worktree/events.go` | harness 层事件类型 + WorktreeEventHandler |
| 创建 | `internal/agentcore/harness/tools/worktree/session.go` | WorktreeSessionState（context.Value 指针模式） |
| 创建 | `internal/agentcore/harness/tools/worktree/session_test.go` | Session 测试 |
| 创建 | `internal/agentcore/harness/tools/worktree/git.go` | Git 命令同步封装 |
| 创建 | `internal/agentcore/harness/tools/worktree/git_test.go` | Git 测试 |
| 创建 | `internal/agentcore/harness/tools/worktree/backend.go` | WorktreeBackend 接口 + GitBackend + 注册表 |
| 创建 | `internal/agentcore/harness/tools/worktree/backend_test.go` | Backend 测试 |
| 创建 | `internal/agentcore/harness/tools/worktree/manager.go` | WorktreeManager 核心逻辑 |
| 创建 | `internal/agentcore/harness/tools/worktree/manager_test.go` | Manager 测试 |
| 创建 | `internal/agentcore/harness/tools/worktree/notice.go` | BuildWorktreeNotice |
| 创建 | `internal/agentcore/harness/tools/worktree/notice_test.go` | Notice 测试 |
| 创建 | `internal/agentcore/harness/tools/worktree/tools.go` | worktreeToolBase + Enter/ExitWorktreeTool |
| 创建 | `internal/agentcore/harness/tools/worktree/tools_test.go` | Tools 测试 |
| 创建 | `internal/agentcore/harness/tools/worktree/rails.go` | WorktreeRail + WorktreeLifecycleRail + AutoSetupRail + DiffSummaryRail |
| 创建 | `internal/agentcore/harness/tools/worktree/rails_test.go` | Rails 测试 |
| 创建 | `internal/agentcore/harness/tools/worktree/cleanup.go` | CleanupStaleWorktrees |
| 创建 | `internal/agentcore/harness/tools/worktree/cleanup_test.go` | Cleanup 测试 |
| 修改 | `internal/agentcore/harness/tools/worktree/doc.go` | 更新文件列表 |
| 修改 | `internal/agent_teams/agent/resources.go` | WorktreeManager any → *worktree.WorktreeManager |
| 修改 | `internal/agent_teams/agent/agent_configurator.go` | CreateWorktreeManager 实现 + 类型回填 |
| 修改 | `internal/swarm/server/adapter/code_adapter.go` | buildWorktreeRail 实现 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 9.66a 状态 ✅ |

---

### Task 1: slug.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/slug.go`
- Create: `internal/agentcore/harness/tools/worktree/slug_test.go`

- [ ] **Step 1: 创建 slug.go**

创建 `internal/agentcore/harness/tools/worktree/slug.go`，对齐 Python `slug.py`：

```go
package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// MaxSlugLength worktree 名称最大长度。
// Python: MAX_SLUG_LENGTH = 64
const MaxSlugLength = 64

// ──────────────────────────── 全局变量 ────────────────────────────

// validSlugSegment 合法 slug 段正则。
// Python: VALID_SLUG_SEGMENT = re.compile(r"^[a-zA-Z0-9._-]+$")
var validSlugSegment = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateSlug 校验 worktree slug 安全性。
// Python: validate_slug(slug)
//
// 拒绝路径遍历、绝对路径、shell 元字符和超长名称。
func ValidateSlug(slug string) error {
	if len(slug) > MaxSlugLength {
		return fmt.Errorf("invalid worktree name: must be %d characters or fewer (got %d)", MaxSlugLength, len(slug))
	}
	for _, segment := range splitPath(slug) {
		if segment == "." || segment == ".." {
			return fmt.Errorf("invalid worktree name %q: must not contain \".\" or \"..\" path segments", slug)
		}
		if !validSlugSegment.MatchString(segment) {
			return fmt.Errorf("invalid worktree name %q: each segment must be non-empty and contain only letters, digits, dots, underscores, and dashes", slug)
		}
	}
	return nil
}

// WorktreeBranchName 将 slug 转为 git 分支名。
// Python: worktree_branch_name(slug)
//
// 将 "/" 替换为 "+"，前缀加 "worktree-"。
// 例：feature-auth → worktree-feature-auth，user/feature-login → worktree-user+feature-login
func WorktreeBranchName(slug string) string {
	result := slug
	for _, pair := range [][2]string{{"/", "+"}} {
		result = replaceAll(result, pair[0], pair[1])
	}
	return "worktree-" + result
}

// WorktreePathFor 计算 worktree 目录路径。
// Python: worktree_path_for(base_dir, slug)
//
// 路径格式：{baseDir}/.worktrees/{slug}
func WorktreePathFor(baseDir, slug string) string {
	return filepath.Join(baseDir, ".worktrees", slug)
}

// WorktreesDir 返回所有 worktree 的父目录。
// Python: worktrees_dir(base_dir)
func WorktreesDir(baseDir string) string {
	return filepath.Join(baseDir, ".worktrees")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitPath 按 "/" 分割路径（对齐 Python slug.split("/")）。
func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	var parts []string
	for _, s := range splitString(p, "/") {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}

// splitString 按 sep 分割字符串。
func splitString(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// replaceAll 替换所有出现的子串。
func replaceAll(s, old, new string) string {
	var result string
	start := 0
	for i := 0; i <= len(s)-len(old); i++ {
		if s[i:i+len(old)] == old {
			result += s[start:i] + new
			start = i + len(old)
			i += len(old) - 1
		}
	}
	result += s[start:]
	return result
}
```

**注意**：实际实现应使用 `strings.SplitN`/`strings.ReplaceAll` 等标准库函数代替手写的 `splitString`/`replaceAll`。上面为展示逻辑用伪实现，正式代码用标准库。

- [ ] **Step 2: 创建 slug_test.go**

创建 `internal/agentcore/harness/tools/worktree/slug_test.go`：

```go
package worktree

import (
	"path/filepath"
	"testing"
)

// TestValidateSlug_合法 测试合法 slug
func TestValidateSlug_合法(t *testing.T) {
	slugs := []string{"feature-auth", "my-feature", "user/feature-login", "a.b_c-d", "x"}
	for _, slug := range slugs {
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("ValidateSlug(%q) 不应返回错误: %v", slug, err)
		}
	}
}

// TestValidateSlug_路径遍历 测试路径遍历拒绝
func TestValidateSlug_路径遍历(t *testing.T) {
	if err := ValidateSlug(".."); err == nil {
		t.Error("ValidateSlug('..') 应返回错误")
	}
	if err := ValidateSlug("."); err == nil {
		t.Error("ValidateSlug('.') 应返回错误")
	}
	if err := ValidateSlug("../etc"); err == nil {
		t.Error("ValidateSlug('../etc') 应返回错误")
	}
}

// TestValidateSlug_超长 测试超长 slug 拒绝
func TestValidateSlug_超长(t *testing.T) {
	longSlug := makeLongString('a', MaxSlugLength+1)
	if err := ValidateSlug(longSlug); err == nil {
		t.Error("超长 slug 应返回错误")
	}
}

// TestValidateSlug_非法字符 测试非法字符拒绝
func TestValidateSlug_非法字符(t *testing.T) {
	illegal := []string{"feature auth", "feature@auth", "feature#auth", ""}
	for _, slug := range illegal {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("ValidateSlug(%q) 应返回错误", slug)
		}
	}
}

// TestWorktreeBranchName 测试分支名生成
func TestWorktreeBranchName(t *testing.T) {
	tests := []struct {
		slug     string
		expected string
	}{
		{"feature-auth", "worktree-feature-auth"},
		{"user/feature-login", "worktree-user+feature-login"},
		{"a/b/c", "worktree-a+b+c"},
	}
	for _, tt := range tests {
		got := WorktreeBranchName(tt.slug)
		if got != tt.expected {
			t.Errorf("WorktreeBranchName(%q) = %q, want %q", tt.slug, got, tt.expected)
		}
	}
}

// TestWorktreePathFor 测试路径计算
func TestWorktreePathFor(t *testing.T) {
	got := WorktreePathFor("/tmp/workspace", "feature-auth")
	expected := filepath.Join("/tmp/workspace", ".worktrees", "feature-auth")
	if got != expected {
		t.Errorf("WorktreePathFor = %q, want %q", got, expected)
	}
}

// TestWorktreesDir 测试父目录
func TestWorktreesDir(t *testing.T) {
	got := WorktreesDir("/tmp/workspace")
	expected := filepath.Join("/tmp/workspace", ".worktrees")
	if got != expected {
		t.Errorf("WorktreesDir = %q, want %q", got, expected)
	}
}

// makeLongString 生成长度为 n 的重复字符字符串
func makeLongString(c rune, n int) string {
	result := make([]rune, n)
	for i := range result {
		result[i] = c
	}
	return string(result)
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -v -count=1 -run TestValidate`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/slug.go internal/agentcore/harness/tools/worktree/slug_test.go
git commit -m "feat(worktree): 添加 slug 校验 + 分支名/路径生成

- ValidateSlug: 路径遍历防护、64字符限制、段内只允许 alphanumeric/.-_
- WorktreeBranchName: / → + 转换
- WorktreePathFor / WorktreesDir: .worktrees 子目录
- 6 个单元测试"
```

---

### Task 2: events.go（harness 层事件类型）

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/events.go`

- [ ] **Step 1: 创建 events.go**

创建 `internal/agentcore/harness/tools/worktree/events.go`：

```go
package worktree

import (
	"context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeCreatedEvent worktree 创建/恢复事件。
// Python: WorktreeCreatedEvent
type WorktreeCreatedEvent struct {
	// WorktreeName worktree 名称
	WorktreeName string `json:"worktree_name"`
	// WorktreePath worktree 绝对路径
	WorktreePath string `json:"worktree_path"`
	// OwnerID 持有者标识（如 team member name）
	OwnerID string `json:"owner_id,omitempty"`
	// Tag 分组标签（如 team name）
	Tag string `json:"tag,omitempty"`
	// Existed 是否从已有 worktree 恢复
	Existed bool `json:"existed"`
}

// WorktreeRemovedEvent worktree 移除事件。
// Python: WorktreeRemovedEvent
type WorktreeRemovedEvent struct {
	// WorktreeName worktree 名称
	WorktreeName string `json:"worktree_name"`
	// WorktreePath worktree 绝对路径
	WorktreePath string `json:"worktree_path"`
	// OwnerID 持有者标识
	OwnerID string `json:"owner_id,omitempty"`
	// Tag 分组标签
	Tag string `json:"tag,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// isWorktreeEvent 实现 WorktreeEvent 接口
func (*WorktreeCreatedEvent) isWorktreeEvent() {}
func (*WorktreeRemovedEvent) isWorktreeEvent() {}
```

同时在此文件中定义接口和回调类型（对齐 Python `WorktreeEvent` union + `WorktreeEventHandler`）：

在结构体区块之后补充：

```go
// WorktreeEvent worktree 事件联合类型。
// Python: WorktreeEvent = Union[WorktreeCreatedEvent, WorktreeRemovedEvent]
type WorktreeEvent interface {
	isWorktreeEvent()
}

// WorktreeEventHandler worktree 生命周期事件回调。
// Python: WorktreeEventHandler = Callable[[WorktreeEvent], Awaitable[None]]
type WorktreeEventHandler func(ctx context.Context, event WorktreeEvent) error
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/tools/worktree/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/events.go
git commit -m "feat(worktree): 添加 harness 层事件类型

- WorktreeCreatedEvent / WorktreeRemovedEvent
- WorktreeEvent 接口（联合类型）
- WorktreeEventHandler 回调类型"
```

---

### Task 3: session.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/session.go`
- Create: `internal/agentcore/harness/tools/worktree/session_test.go`

- [ ] **Step 1: 创建 session.go**

对齐 Python `session.py` + Go 项目 CwdState/SessionState 模式。完整代码参考设计文档 §1。核心：

- `WorktreeSessionState` 结构体（`sync.RWMutex` + `session *WorktreeSession` + `defaultWorktreeName string`）
- `worktreeSessionStateKeyType` context key 类型
- `InitWorktreeSessionState()` / `WithWorktreeSessionState()` / `WorktreeSessionStateFromCtx()`
- `GetCurrentSession()` / `SetCurrentSession()` / `GetDefaultWorktreeName()` / `SetDefaultWorktreeName()` / `RequireCurrentSession()`

~80 行，对齐 `internal/agent_teams/sessionctx/session_state.go` 风格。

- [ ] **Step 2: 创建 session_test.go**

6 个测试用例（对齐设计文档 §14 session_test.go）：
- `TestInitWorktreeSessionState`
- `TestWithWorktreeSessionState`
- `TestSetCurrentSession`
- `TestRequireCurrentSession_存在`
- `TestRequireCurrentSession_不存在`
- `TestGetSetDefaultWorktreeName`

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -v -count=1 -run TestWorktreeSession`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/session.go internal/agentcore/harness/tools/worktree/session_test.go
git commit -m "feat(worktree): 添加 WorktreeSessionState（context.Value 指针模式）

- 对齐 Python session.py + Go CwdState/SessionState 模式
- GetCurrentSession/SetCurrentSession/RequireCurrentSession
- 6 个单元测试"
```

---

### Task 4: git.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/git.go`
- Create: `internal/agentcore/harness/tools/worktree/git_test.go`

这是最大的独立文件（~300行），对齐 Python `git.py`。

- [ ] **Step 1: 创建 git.go**

核心类型和函数（对齐设计文档 §3）：

```go
package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GitError Git 命令执行错误。
// Python: GitError(Exception)
type GitError struct {
	// Command 失败的命令
	Command string
	// ReturnCode 退出码
	ReturnCode int
	// Stderr 标准错误输出
	Stderr string
}

// GitResult Git 命令执行结果。
// Python: GitResult(frozen dataclass)
type GitResult struct {
	// ReturnCode 退出码
	ReturnCode int
	// Stdout 标准输出
	Stdout string
	// Stderr 标准错误
	Stderr string
}
```

函数清单（全部同步 + `context.Context`）：

- `runGit(ctx, args, cwd) GitResult` — 核心执行器，`os/exec.CommandContext` + 30s 超时，设置 `GIT_TERMINAL_PROMPT=0`
- `findGitRoot(ctx, cwd) (string, error)` — `git rev-parse --show-toplevel`
- `findCanonicalGitRoot(ctx, cwd) (string, error)` — 从 worktree 找主仓库（读 .git 文件 → commondir → 向上找）
- `getCurrentBranch(ctx, cwd) (string, error)` — `git rev-parse --abbrev-ref HEAD`
- `getDefaultBranch(ctx, cwd) (string, error)` — `git remote show origin` → HEAD branch
- `revParse(ctx, ref, cwd) (string, error)` — `git rev-parse ref`
- `worktreeAdd(ctx, repoRoot, wtPath, branch, baseRef string, noCheckout bool) error` — `git worktree add -B branch wtPath baseRef [--no-checkout]`
- `worktreeRemove(ctx, wtPath string, repoRoot string, force bool) (bool, error)` — `git worktree remove [--force] wtPath`
- `worktreePrune(ctx, repoRoot) error` — `git worktree prune`
- `branchDelete(ctx, branch, repoRoot) error` — `git branch -D branch`
- `fetchRef(ctx, repoRoot, ref string, remote ...string) (bool, error)` — `git fetch remote ref`
- `sparseCheckoutSet(ctx, wtPath string, paths []string) error` — `git sparse-checkout set paths`
- `statusPorcelain(ctx, cwd) ([]string, error)` — `git status --porcelain`
- `countCommitsSince(ctx, baseCommit, cwd string) (*int, error)` — `git rev-list --count base..HEAD`，nil = 无法确定
- `hasUnpushedCommits(ctx, cwd) (*bool, error)` — `git rev-list @{upstream}..HEAD --count`
- `readWorktreeHeadSHA(wtPath string) (string, error)` — 快速路径：直接读 `.git` 文件 → commondir → HEAD → ref，不调 git 子进程
- `_gitEnv() []string` — 返回 `GIT_TERMINAL_PROMPT=0` 等环境变量

~300 行。

- [ ] **Step 2: 创建 git_test.go**

对齐设计文档 §14 git_test.go：
- `TestRunGit_正常命令` — `git --version`
- `TestRunGit_错误命令` — `git nonexistent-command`
- `TestFindCanonicalGitRoot` — 在 `t.TempDir()` 创建真实 git 仓库
- `TestReadWorktreeHeadSHA` — 快速路径验证
- `TestStatusPorcelain` — 在临时 git 仓库中验证
- `TestGetCurrentBranch` — 分支名读取

真实 git worktree 操作的测试标记 `//go:build integration`：
- `TestWorktreeAddRemove_真实调用`

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -v -count=1 -run TestRunGit`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/git.go internal/agentcore/harness/tools/worktree/git_test.go
git commit -m "feat(worktree): 添加 Git 命令同步封装

- 16 个 git 辅助函数（worktree add/remove/prune/sparse-checkout/status 等）
- GitError/GitResult 类型
- readWorktreeHeadSHA 快速路径（不调 git 子进程）
- os/exec.CommandContext + 30s 超时
- 7 个单元测试 + integration 标签的真实 git 测试"
```

---

### Task 5: backend.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/backend.go`
- Create: `internal/agentcore/harness/tools/worktree/backend_test.go`

- [ ] **Step 1: 创建 backend.go**

对齐 Python `backend.py`（273行）。核心：

- `WorktreeBackend` 接口（Create/Remove/Exists 3个方法）
- `GitBackend` 结构体 + 四阶段 Create 实现
- `_resolveBase` 方法（智能基准分支解析）
- `_BACKEND_REGISTRY` 注册表 + `registerWorktreeBackend` / `createBackend`
- `ManagerOption` / `WithEventHandler` / `WithLifecycleRails` 函数式选项也放这里

~200 行。

- [ ] **Step 2: 创建 backend_test.go**

对齐设计文档 §14 backend_test.go：
- `TestGitBackend_Create_新建` — 需要 `//go:build integration` 标签
- `TestGitBackend_Create_恢复已有` — Phase 1 快速恢复
- `TestGitBackend_Remove`
- `TestGitBackend_Exists`
- `TestCreateBackend_默认Git`
- `TestCreateBackend_未知名称`

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -v -count=1 -run TestCreateBackend`
Expected: PASS（单元测试部分）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/backend.go internal/agentcore/harness/tools/worktree/backend_test.go
git commit -m "feat(worktree): 添加 WorktreeBackend 接口 + GitBackend 实现

- WorktreeBackend 接口（Create/Remove/Exists）
- GitBackend 四阶段创建（快速恢复/基准解析/worktree add/稀疏检出）
- 后端注册表 + createBackend 工厂
- ManagerOption 函数式选项
- 6 个单元测试"
```

---

### Task 6: manager.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/manager.go`
- Create: `internal/agentcore/harness/tools/worktree/manager_test.go`

最大的任务。manager.go ~400 行，manager_test.go ~500 行。

- [ ] **Step 1: 创建 manager.go**

对齐 Python `manager.py`（727行）。核心（对齐设计文档 §4）：

- `WorktreeManager` 结构体
- `NewWorktreeManager` 构造函数
- `Enter(ctx, slug, memberName, teamName)` — 完整 enter 流程
- `Exit(ctx, action, discardChanges)` — 完整 exit 流程
- `CreateOwnerWorktree` / `CreateAgentWorktree`
- `RecoverWorktreeForOwner` / `RecoverWorktreeForMember`
- `CountChanges`
- `CleanupWorktreesByPrefix` / `CleanupTeamWorktrees` / `RemoveWorktree`
- 内部辅助方法：`resolveTargetPath`/`ownerSlug`/`resolvePolicy`/`fireRail`/`checkChanges`/`removeWorktreeInternal`/`postCreationSetup`/`copyIncludeFiles`/`configureHooksPath`

所有方法同步签名 + `context.Context`。`sync.Mutex` 不需要（Manager 本身无 mutable 共享状态，锁在 session 级别的 `WorktreeSessionState` 中）。

- [ ] **Step 2: 创建 manager_test.go**

对齐设计文档 §14 manager_test.go：
- `TestNewWorktreeManager`
- `TestEnter_创建Worktree`
- `TestEnter_恢复已有`
- `TestExit_Keep`
- `TestExit_Remove无变更`
- `TestExit_Remove有变更拒绝`
- `TestExit_Remove有变更确认`
- `TestCreateOwnerWorktree`
- `TestCountChanges`
- `TestRecoverWorktreeForOwner`
- `TestCleanupWorktreesByPrefix`
- `TestPostCreationSetup_Symlink`
- `TestPostCreationSetup_IncludeFiles`
- `TestPostCreationSetup_HooksPath`

使用 `t.TempDir()` + 真实 git 操作，标记 `//go:build integration`。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -v -count=1 -run TestNewWorktreeManager`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/manager.go internal/agentcore/harness/tools/worktree/manager_test.go
git commit -m "feat(worktree): 实现 WorktreeManager

- Enter/Exit 生命周期（session 级）
- CreateOwnerWorktree（owner 级，不改 ContextVar）
- RecoverWorktreeForOwner
- CountChanges（fail-closed）
- CleanupWorktreesByPrefix
- postCreationSetup（symlink/includeFiles/hooksPath）
- 14 个单元测试"
```

---

### Task 7: notice.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/notice.go`
- Create: `internal/agentcore/harness/tools/worktree/notice_test.go`

- [ ] **Step 1: 创建 notice.go**

对齐 Python `notice.py`（36行）。一比一复刻提示词内容。

```go
package worktree

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildWorktreeNotice 构建 worktree 上下文提示。
// Python: build_worktree_notice(parent_cwd, worktree_cwd)
//
// 注入到 Agent 系统提示中，告知 Agent 当前处于隔离 worktree。
func BuildWorktreeNotice(parentCwd, worktreeCwd string) string {
	return fmt.Sprintf(
		"You are operating in an isolated git worktree at %s. "+
			"The parent context lives in %s — same repository, "+
			"same relative file structure, separate working copy.\n\n"+
			"Important:\n"+
			"- Paths from the parent context refer to %s\n"+
			"- Translate them to your worktree root before use\n"+
			"- Re-read files before editing if the parent may have modified them\n"+
			"- Your changes stay in this worktree and will not affect the parent",
		worktreeCwd, parentCwd, parentCwd)
}
```

- [ ] **Step 2: 创建 notice_test.go**

- `TestBuildWorktreeNotice` — 验证输出包含两个路径

- [ ] **Step 3: 运行测试 + 提交**

```bash
git add internal/agentcore/harness/tools/worktree/notice.go internal/agentcore/harness/tools/worktree/notice_test.go
git commit -m "feat(worktree): 添加 BuildWorktreeNotice

- 1:1 复刻 Python build_worktree_notice
- 1 个单元测试"
```

---

### Task 8: cleanup.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/cleanup.go`
- Create: `internal/agentcore/harness/tools/worktree/cleanup_test.go`

- [ ] **Step 1: 创建 cleanup.go**

对齐 Python `cleanup.py`（129行）。Fail-closed 安全策略。

核心：
- `ephemeralPatterns` — 正则切片（`^teammate-[0-9a-f]{8}$` + `^agent-[0-9a-f]{7}$`）
- `IsEphemeralSlug(slug) bool`
- `CleanupStaleWorktrees(ctx, config, backend, currentWorktreePath) (int, error)` — 五步安全检查

~90 行。

- [ ] **Step 2: 创建 cleanup_test.go**

- `TestIsEphemeralSlug` — 匹配/不匹配
- `TestCleanupStaleWorktrees_清理过期`
- `TestCleanupStaleWorktrees_跳过当前`
- `TestCleanupStaleWorktrees_跳过有变更`

需要 mock `WorktreeBackend`。

- [ ] **Step 3: 运行测试 + 提交**

```bash
git add internal/agentcore/harness/tools/worktree/cleanup.go internal/agentcore/harness/tools/worktree/cleanup_test.go
git commit -m "feat(worktree): 添加 CleanupStaleWorktrees

- IsEphemeralSlug + ephemeral pattern 匹配
- Fail-closed 安全策略（5步检查）
- 4 个单元测试"
```

---

### Task 9: tools.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/tools.go`
- Create: `internal/agentcore/harness/tools/worktree/tools_test.go`

- [ ] **Step 1: 创建 tools.go**

对齐 Python `tools.py`（314行）。核心：

- `worktreeToolBase`（包内不导出）：嵌入 `*tool.ToolCard` + `*WorktreeManager`，实现 `Card()` 和 `Stream` 返回 `ErrStreamNotSupported`
- `EnterWorktreeTool`：嵌入 `worktreeToolBase` + `language string` + `agentID string`
  - `Invoke(ctx, inputs, opts)` — 完整 enter 流程（对齐设计文档 §7）
  - `resolveSlug` — 无 name → 生成随机 `<adj>-<noun>-<4hex>`
  - `slugExists` — 通过 backend.Exists 检查
- `ExitWorktreeTool`：嵌入 `worktreeToolBase` + `language string` + `agentID string`
  - `Invoke(ctx, inputs, opts)` — 完整 exit 流程（对齐设计文档 §7）
- 辅助函数：`generateRandomSlug` / `resolveOwner` / `resolveWorkspaceWorktreePath`

ToolCard 构造使用已有的 `harness/prompts/tools` 包中的 `EnterWorktreeMetadataProvider` / `ExitWorktreeMetadataProvider` 获取 description 和 inputParams。

~250 行。

- [ ] **Step 2: 创建 tools_test.go**

9 个测试用例。需要 mock `WorktreeManager` + `WorktreeBackend`。

- [ ] **Step 3: 运行测试 + 提交**

```bash
git add internal/agentcore/harness/tools/worktree/tools.go internal/agentcore/harness/tools/worktree/tools_test.go
git commit -m "feat(worktree): 实现 EnterWorktreeTool + ExitWorktreeTool

- worktreeToolBase 包内基类（Card + Stream not supported）
- EnterWorktreeTool: resolveSlug/generateRandomSlug/enter/CWD switch
- ExitWorktreeTool: validate action/count changes/exit/CWD restore
- 9 个单元测试"
```

---

### Task 10: rails.go + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/worktree/rails.go`
- Create: `internal/agentcore/harness/tools/worktree/rails_test.go`

- [ ] **Step 1: 创建 rails.go**

对齐 Python `rails.py`（480行）。核心：

**WorktreeLifecycleRail**（hook 基类）：
- 嵌入 `rails.DeepAgentRail`
- 8 个 hook 方法（默认 no-op）：`BeforeWorktreeCreate`/`AfterWorktreeCreate`/`BeforeWorktreeExit`/`AfterWorktreeExit`/`OnWorktreeFileWrite`/`BeforeWorktreeCommit`/`AfterWorktreeCommit`/`OnWorktreeSync`

**WorktreeRail**（注入 rail）：
- 嵌入 `rails.DeepAgentRail`
- `Priority() int` — 返回 100（与 SysOperationRail 同级）
- `Init(ctx, agent)` — 创建 Manager + InitWorktreeSessionState + 注册工具
- `Uninit(agent)` — 移除工具 + 清空 Manager
- `BeforeInvoke(ctx, cbc)` — 从 Session.state 恢复 → SetCurrentSession + SetCwd + SetOriginalCwd
- `AfterInvoke(ctx, cbc)` — 将 GetCurrentSession → 持久化到 Session.state
- Session state key 常量：`sessionStateKey = "_worktree_session"` / `defaultWorktreeNameKey = "_worktree_default_name"`

**AutoSetupRail**：
- 嵌入 `WorktreeLifecycleRail`
- `AfterWorktreeCreate` — 检测 pyproject.toml → `uv sync --quiet`，package.json → `npm install --silent`

**DiffSummaryRail**：
- 嵌入 `WorktreeLifecycleRail`
- `BeforeWorktreeExit` — action="keep" 时记录 `git diff --stat`

~350 行。

- [ ] **Step 2: 创建 rails_test.go**

10 个测试用例。需要构造 mock AgentCallbackContext。

- [ ] **Step 3: 运行测试 + 提交**

```bash
git add internal/agentcore/harness/tools/worktree/rails.go internal/agentcore/harness/tools/worktree/rails_test.go
git commit -m "feat(worktree): 实现 WorktreeRail + WorktreeLifecycleRail + AutoSetupRail + DiffSummaryRail

- WorktreeLifecycleRail: 8 个 hook 方法
- WorktreeRail: Init/Uninit/BeforeInvoke/AfterInvoke
- AutoSetupRail: 检测项目类型并运行 setup
- DiffSummaryRail: action=keep 时记录 git diff --stat
- 10 个单元测试"
```

---

### Task 11: 更新 doc.go

**Files:**
- Modify: `internal/agentcore/harness/tools/worktree/doc.go`

- [ ] **Step 1: 更新文件目录树**

将 doc.go 更新为包含所有新文件：

```go
// Package worktree 提供 Git Worktree 隔离能力，让每个 Agent 在独立的工作树中执行任务，
// 避免并发操作对主仓库的干扰。
//
// 本包实现完整的 Worktree 生命周期管理：创建/恢复、进入/退出、变更检测、批量清理、
// session 状态持久化、事件分发和 lifecycle hook 体系。
// 对齐 Python openjiuwen/harness/tools/worktree/。
//
// 文件目录：
//
//	worktree/
//	├── doc.go           # 包文档
//	├── models.go        # 核心数据结构与枚举定义
//	├── slug.go          # Slug 校验 + 分支名/路径生成
//	├── events.go        # harness 层事件类型 + WorktreeEventHandler
//	├── session.go       # WorktreeSessionState（context.Value 指针模式）
//	├── git.go           # Git 命令同步封装
//	├── backend.go       # WorktreeBackend 接口 + GitBackend + 注册表
//	├── manager.go       # WorktreeManager 核心逻辑
//	├── notice.go        # BuildWorktreeNotice 上下文提示
//	├── cleanup.go       # CleanupStaleWorktrees 过期清理
//	├── tools.go         # worktreeToolBase + Enter/ExitWorktreeTool
//	└── rails.go         # WorktreeRail + WorktreeLifecycleRail + AutoSetupRail + DiffSummaryRail
//
// 对应 Python 代码：openjiuwen/harness/tools/worktree/
package worktree
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/doc.go
git commit -m "docs(worktree): 更新 doc.go 文件目录"
```

---

### Task 12: 回填 agent/resources.go

**Files:**
- Modify: `internal/agent_teams/agent/resources.go`

- [ ] **Step 1: 修改 WorktreeManager 类型**

1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"`
2. 将 `WorktreeManager any` 改为 `WorktreeManager *worktree.WorktreeManager`
3. 删除 `// TODO(#9.66a): WorktreeManager 实现后替换为具体类型` 注释

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/...`
Expected: 编译失败（因为 agent_configurator.go 中还有 `any` 类型引用）——这是预期的，Task 13 一起修复

先跳过编译验证，与 Task 13 合并。

- [ ] **暂不提交，等 Task 13 一起提交**

---

### Task 13: 回填 agent/agent_configurator.go

**Files:**
- Modify: `internal/agent_teams/agent/agent_configurator.go`

- [ ] **Step 1: 修改 WorktreeManager 相关方法**

1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"`
2. `WorktreeManager()` 返回类型 `any` → `*worktree.WorktreeManager`
3. `SetWorktreeManager(v any)` → `SetWorktreeManager(v *worktree.WorktreeManager)`
4. 删除 3 处 `// TODO(#9.66a)` 注释

- [ ] **Step 2: 实现 CreateWorktreeManager**

替换空方法体为完整实现（对齐 Python `AgentConfigurator.create_worktree_manager`）：

```go
func (c *AgentConfigurator) CreateWorktreeManager(spec atschema.TeamAgentSpec) *worktree.WorktreeManager {
	cfg := worktree.NewWorktreeConfig()
	if spec.Worktree != nil {
		cfg = *spec.Worktree
	}
	cfg.Enabled = true

	// 事件镜像回调：harness 层 WorktreeEvent → agent_teams 层 schema.TypedEvent
	var eventHandler worktree.WorktreeEventHandler
	if c.WorkspaceManager() != nil && c.TeamBackend() != nil {
		wsMgr := c.WorkspaceManager()
		tb := c.TeamBackend()
		eventHandler = func(ctx context.Context, event worktree.WorktreeEvent) error {
			switch e := event.(type) {
			case *worktree.WorktreeCreatedEvent:
				// 挂载 worktree symlink
				_ = wsMgr.MountWorktree(e.WorktreeName, e.WorktreePath)
				// 发布 schema 层事件
				tb.PublishEvent(ctx, atevents.TeamEventWorktreeCreated, atevents.WorktreeCreatedEvent{
					WorktreeName: e.WorktreeName,
					WorktreePath: e.WorktreePath,
					Existed:      e.Existed,
				})
			case *worktree.WorktreeRemovedEvent:
				// 卸载 worktree symlink
				_ = wsMgr.UnmountWorktree(e.WorktreeName)
				// 发布 schema 层事件
				tb.PublishEvent(ctx, atevents.TeamEventWorktreeRemoved, atevents.WorktreeRemovedEvent{
					WorktreeName: e.WorktreeName,
					WorktreePath: e.WorktreePath,
				})
			}
			return nil
		}
	}

	return worktree.NewWorktreeManager(cfg, nil, worktree.WithEventHandler(eventHandler))
}
```

- [ ] **Step 3: 修改 SetupInfra 步骤 9**

将 `c.CreateWorktreeManager(spec)` 改为 `c.SetWorktreeManager(c.CreateWorktreeManager(spec))`，删除 `// TODO(#9.66a)` 注释。

- [ ] **Step 4: 运行编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 5: 运行现有测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交（与 Task 12 一起）**

```bash
git add internal/agent_teams/agent/resources.go internal/agent_teams/agent/agent_configurator.go
git commit -m "refactor(agent): 回填 WorktreeManager 具体类型，清零 TODO(#9.66a)

- resources.go: WorktreeManager any → *worktree.WorktreeManager
- agent_configurator.go: CreateWorktreeManager 完整实现
- 事件镜像回调：harness WorktreeEvent → schema TypedEvent
- SetupInfra 步骤 9 调用 SetWorktreeManager
- WorktreeManager()/SetWorktreeManager() 类型具体化"
```

---

### Task 14: 回填 code_adapter.go

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 实现 buildWorktreeRail**

将 stub 替换为实际构造（需确认 code_adapter 中 `buildWorktreeRail` 方法的当前签名和上下文，参考 `buildWorkspaceRail` 等已有方法模式）：

```go
func (c *CodeAdapter) buildWorktreeRail() sinterfaces.AgentRail {
	// 从配置中获取 WorktreeConfig
	cfg := worktree.NewWorktreeConfig()
	cfg.Enabled = true
	rail := worktree.NewWorktreeRail(worktree.WithConfig(cfg))
	return rail
}
```

- [ ] **Step 2: 运行编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/adapter/code_adapter.go
git commit -m "feat(adapter): 实现 buildWorktreeRail，替换 nil stub

- 从配置构造 WorktreeRail
- 清零 TODO(10.6.3-10)"
```

---

### Task 15: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.66a 状态**

将 `| 9.66a | ☐ | WorktreeManager |` 改为 `| 9.66a | ✅ | WorktreeManager |` 并补充实现摘要。

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.66a 状态为 ✅"
```
