# 48h 逻辑审查报告 — 2026-09-19

> 审查范围：9.66a WorktreeManager / 10.6.3-10 Swarm Rails / 9.24 P4 Evolution Rails / 10.3.7-11 适配器辅助
> 审查日期：2026-09-19
> 审查方法：逐方法对照 Python 参考项目，检查签名/步骤/占位代码

## 审查章节

| 章节 | 状态 | 描述 |
|------|------|------|
| 9.66a | ✅ | WorktreeManager 完整实现 |
| 10.6.3-10 | 🔄 | Swarm Rails（AskUser/Avatar/Permissions/Interrupt/ProjectMemory/RuntimePrompt/StreamEvent/ResponsePrompt） |
| 9.24 P4 | ✅ | TeamSkillEvolutionRail + SkillEvolutionRail |
| 10.3.7-11 | 🔄 | 适配器辅助（SysOpBuilder/Recap/EvolutionHelpers/CodeAgentRail/ListAvailableTools/Context连接） |

## 问题统计

| 严重级别 | 数量 | 问题ID |
|---------|------|--------|
| **严重** | 18 | WT-01~03, SR-01~06, EV-01~05, AD-01~04 |
| **一般** | 30 | WT-04~12, SR-07~16, EV-06~15, AD-05~14 |
| **提示** | 22 | WT-13~17, SR-17~20, EV-16~21, AD-15~18 |
| **总计** | **70** | |

---

## 一、WorktreeManager (9.66a)

### WT-01 [严重] `WorktreeRail.Init` 中 `WithWorktreeSessionState` 返回值被丢弃，session 机制失效

**描述**：`WithWorktreeSessionState(ctx, state)` 返回新 context，但 `Init` 中用 `_ = WithWorktreeSessionState(ctx, state)` 丢弃了返回值。这意味着 session state 实际上没有被注入到 context 中，后续通过 `WorktreeSessionStateFromCtx` 获取时将返回 nil，所有依赖 context 传播 session 的代码（Enter/Exit 工具、BeforeInvoke/AfterInvoke）都将失效。

**Python 样例**：
```python
# rails.py — Python 使用 ContextVar 全局绑定，无需 ctx
def init(self, agent) -> None:
    init_session_state()  # ContextVar.set() 全局生效
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/rails.go` L139-140
```go
state := InitWorktreeSessionState()
_ = WithWorktreeSessionState(ctx, state)  // ← 返回的新 ctx 被丢弃！
```

**修复方案**：Go 使用 context.Value 传播 session，而 Python 使用 ContextVar 全局绑定。需要在 `WorktreeRail` 内持有 `*WorktreeSessionState` 引用，在 `BeforeInvoke` 时注入到请求 context：
```go
func (r *WorktreeRail) Init(ctx context.Context, agent interfaces.BaseAgent) error {
    r.sessionState = InitWorktreeSessionState()  // 保存引用
    // ...
}
// 在 BeforeInvoke 中注入
func (r *WorktreeRail) BeforeInvoke(ctx context.Context, cbc *AgentCallbackContext) error {
    ctx = WithWorktreeSessionState(ctx, r.sessionState)
    // ...
}
```

---

### WT-02 [严重] `WorktreeRail.Init` 未向 Agent 注册工具，Uninit 未注销工具

**描述**：Python 的 `WorktreeRail.init()` 调用 `Runner.resource_mgr.add_tool()` 和 `agent.ability_manager.add()` 将 enter/exit 工具注册到 Agent。Go 的 `Init` 只创建了工具列表但未调用任何注册 API，工具不会被 Agent 识别和使用。同样，`Uninit` 未调用任何注销 API，工具残留。

**Python 样例**：
```python
def init(self, agent) -> None:
    self._tools = [EnterWorktreeTool(...), ExitWorktreeTool(...)]
    Runner.resource_mgr.add_tool(self._tools)
    for tool in self._tools:
        agent.ability_manager.add(tool.card)

def uninit(self, agent) -> None:
    for tool in self._tools:
        agent.ability_manager.remove(name)
    self._tools = []
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/rails.go` L127-165
```go
func (r *WorktreeRail) Init(ctx context.Context, agent interfaces.BaseAgent) error {
    // ...
    r.tools = []tool.Tool{enterTool, exitTool}
    // 缺少: agent.AbilityManager().Add(...) 和 Runner.resourceMgr.AddTool(...)
    return nil
}
func (r *WorktreeRail) Uninit(agent interfaces.BaseAgent) error {
    r.tools = nil
    r.manager = nil
    // 缺少: agent.AbilityManager().Remove(...)
    return nil
}
```

**修复方案**：
```go
func (r *WorktreeRail) Init(ctx context.Context, agent interfaces.BaseAgent) error {
    // ...
    r.tools = []tool.Tool{enterTool, exitTool}
    for _, t := range r.tools {
        agent.AbilityManager().Add(t.Card())
    }
    if rm := runner.GetResourceMgr(); rm != nil {
        for _, t := range r.tools {
            rm.AddTool(t)
        }
    }
    return nil
}

func (r *WorktreeRail) Uninit(agent interfaces.BaseAgent) error {
    for _, t := range r.tools {
        agent.AbilityManager().Remove(t.Card().Name)
    }
    if rm := runner.GetResourceMgr(); rm != nil {
        for _, t := range r.tools {
            rm.RemoveTool(t.Card().ID)
        }
    }
    r.tools = nil
    r.manager = nil
    return nil
}
```

---

### WT-03 [严重] `WorktreeLifecycleRail` 接口缺失 3 个 hook 方法

**描述**：Python 的 `WorktreeLifecycleRail` 定义了 7 个 hook 方法，Go 只实现了 4 个（create 前/后、exit 前/后），缺少 3 个关键 hook：`on_worktree_file_write`、`before_worktree_commit`/`after_worktree_commit`。这些 hook 是安全控制和 CI 触发的核心入口。

**Python 样例**：
```python
class WorktreeLifecycleRail(DeepAgentRail):
    async def on_worktree_file_write(self, ctx, session, file_path) -> bool:
        return True  # True = 允许写入
    async def before_worktree_commit(self, ctx, session, message, files) -> str | None:
        return None  # None = 不干预
    async def after_worktree_commit(self, ctx, session, commit_sha) -> None:
        pass
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/backend.go` L32-41

**修复方案**：在 `WorktreeLifecycleRail` 接口中补充缺失的 hook 方法签名：
```go
type WorktreeLifecycleRail interface {
    BeforeWorktreeCreate(ctx context.Context, slug, repoRoot string) (string, error)
    AfterWorktreeCreate(ctx context.Context, session *WorktreeSession) error
    BeforeWorktreeExit(ctx context.Context, session *WorktreeSession, action string) (string, error)
    AfterWorktreeExit(ctx context.Context, session *WorktreeSession, action string) error
    // 新增
    OnWorktreeFileWrite(ctx context.Context, session *WorktreeSession, filePath string) (bool, error)
    BeforeWorktreeCommit(ctx context.Context, session *WorktreeSession, message string, files []string) (string, error)
    AfterWorktreeCommit(ctx context.Context, session *WorktreeSession, commitSHA string) error
}
```
并在 `BaseLifecycleRail` 中提供默认空实现。

---

### WT-04 [一般] `WorktreeManager.fireRail` 是空实现，生命周期 hook 永不执行

**描述**：Python 的 `_fire_rail` 遍历 `self._rails`，用 `getattr` 动态调用指定方法名。Go 的 `fireRail` 直接返回 `nil`，没有任何调度逻辑。

**Python 样例**：
```python
async def _fire_rail(self, method: str, *args, **kwargs) -> Any:
    result = None
    for rail in self._rails:
        handler = getattr(rail, method, None)
        if handler:
            r = await handler(*args, **kwargs)
            if r is not None:
                result = r
    return result
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/manager.go` L443-447

**修复方案**：实现完整的 fireRail 逻辑，在 Enter/Exit 中显式调用各 rail 的 Before/After 方法（Go 风格，避免反射）：
```go
func (m *WorktreeManager) fireBeforeCreate(ctx context.Context, slug, repoRoot string) (string, error) {
    for _, rail := range m.lifecycleRails {
        override, err := rail.BeforeWorktreeCreate(ctx, slug, repoRoot)
        if err != nil { return "", err }
        if override != "" { return override, nil }
    }
    return "", nil
}
```

---

### WT-05 [一般] `simpleMatch` 不等价于 Python `fnmatch`，模式匹配大幅缩水

**描述**：Python 使用 `fnmatch.fnmatch`（支持 `*`、`?`、`[seq]`），Go 只支持 `*` 尾通配和精确匹配。对 `include_patterns` 如 `.env.*`、`config/*.yaml` 的匹配行为与 Python 不一致。

**Python 样例**：
```python
if any(fnmatch.fnmatch(entry, p) for p in patterns):
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/manager.go` L560-569

**修复方案**：使用 `filepath.Match` 替代 `simpleMatch`（支持 `*`、`?`、`[seq]`）。

---

### WT-06 [一般] `copyFile` 未保留文件元数据，不等价于 Python `shutil.copy2`

**描述**：Python 使用 `shutil.copy2` 保留 mtime/atime，Go 用 `os.ReadFile` + `os.WriteFile` 丢失时间戳和权限。

**Python 样例**：
```python
shutil.copy2(src, dst)
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/manager.go` L572-578

**修复方案**：使用 `os.Open` + `io.Copy` + `os.Chtimes` 或保留源文件权限：
```go
func copyFile(src, dst string) error {
    info, err := os.Stat(src)
    if err != nil { return err }
    data, err := os.ReadFile(src)
    if err != nil { return err }
    if err := os.WriteFile(dst, data, info.Mode()); err != nil { return err }
    return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
```

---

### WT-07 [一般] `GitError.Command` 是 string 而非 `[]string`，丢失参数信息

**描述**：Python `GitError.command` 是 `list[str]`（如 `["worktree", "add", "-B", "branch"]`），Go 的 `Command` 是 `string`（仅 `"worktree add"`），丢失了具体参数信息，不利于调试。

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/git.go` L18-25

**修复方案**：将 `Command string` 改为 `Args []string`。

---

### WT-08 [一般] `GitBackend.Create` 稀疏检出失败时返回 `fmt.Errorf` 而非 `*GitError`

**描述**：Python 在稀疏检出失败时重新构造 `GitError(["sparse-checkout"], ...)` 并链式附加原始异常。Go 用 `fmt.Errorf` 包装，调用方无法通过类型断言区分错误类型。

**Python 样例**：
```python
raise GitError(["sparse-checkout"], e.returncode, f"Failed sparse checkout: {e.stderr}") from e
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/backend.go` L153-156

**修复方案**：返回 `*GitError` 类型：
```go
if err := SparseCheckoutSet(ctx, targetPath, sparse); err != nil {
    _ = WorktreeRemove(ctx, targetPath, repoRoot, true)
    if ge, ok := err.(*GitError); ok {
        return nil, &GitError{Args: []string{"sparse-checkout"}, ReturnCode: ge.ReturnCode, Stderr: fmt.Sprintf("稀疏检出失败，worktree 已清理: %s", ge.Stderr)}
    }
    return nil, &GitError{Args: []string{"sparse-checkout"}, ReturnCode: -1, Stderr: err.Error()}
}
```

---

### WT-09 [一般] `resolveOwner` 永远返回空字符串，owner 信息丢失

**描述**：Python 的 `_resolve_owner` 从 kwargs 中读取 `owner_id`/`tag` 或 `member_name`/`team_name`。Go 的 `resolveOwner` 始终返回 `("", "")`，导致事件中 `owner_id` 和 `tag` 字段永远缺失。

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/tools.go` L286-290

**修复方案**：从 Agent 的 context 或 session 中获取 owner 信息，或从 ToolOption 中提取。标注为待回填点。

---

### WT-10 [一般] `WorktreeRail.BeforeInvoke` 对 `stored` 类型判断不完整

**描述**：Python 的 `before_invoke` 处理 `dict` 和 `WorktreeSession` 两种类型（防御性回退），Go 只处理 `map[string]any`，缺少对 `*WorktreeSession` 直接存储的防御性回退。

**Python 样例**：
```python
if isinstance(stored, dict):
    stored = WorktreeSession.model_validate(stored)
set_current_session(stored)  # stored 可能已经是 WorktreeSession
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/rails.go` L192-209

**修复方案**：添加 `case *WorktreeSession:` 分支。

---

### WT-11 [一般] `CleanupStaleWorktrees` 未并行执行安全检查

**描述**：Python 使用 `asyncio.gather` 并行执行 `status_porcelain` 和 `has_unpushed_commits`，Go 串行执行，性能较差。

**Python 样例**：
```python
changes, unpushed = await asyncio.gather(
    status_porcelain(wt_path),
    has_unpushed_commits(wt_path),
)
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/cleanup.go` L93-103

**修复方案**：使用 `errgroup.Group` 并行执行两个检查。

---

### WT-12 [一般] `WorktreeRail.Init` 中获取 `lang` 和 `agentID` 硬编码

**描述**：Python 从 `agent.system_prompt_builder.language` 和 `agent.card.id` 获取。Go 硬编码 `lang := "cn"` 和 `agentID := ""`。

**Python 样例**：
```python
lang = agent.system_prompt_builder.language
agent_id = getattr(getattr(agent, "card", None), "id", None)
```

**Go 问题代码位置**：`internal/agentcore/harness/tools/worktree/rails.go` L128-129

**修复方案**：从 `interfaces.BaseAgent` 接口中获取语言和 ID。

---

### WT-13 [提示] `AutoSetupRail.BeforeWorktreeCreate` 返回空字符串而非 nil 语义

**描述**：Python 返回 `None` 表示不干预，Go 返回 `""`。需约定空字符串表示"不干预"。

**修复方案**：接口文档约定空字符串 = 不干预，或改接口为返回 `*string`（nil = 不干预）。

---

### WT-14 [提示] `WorktreePrune` 忽略错误返回

**修复方案**：与 Python 一致，prune 失败不影响主流程，可保持返回 nil 或改为无返回值函数。

---

### WT-15 [提示] `gitEnv` 可能导致环境变量重复

**修复方案**：使用 `map` 过滤重复 key 后再转 `[]string`，或添加注释说明 git 取最后一个值。

---

### WT-16 [提示] `EnterWorktreeTool.slugExists` 在 workspace 为空时返回 false，Python 抛异常

**修复方案**：可保持当前更宽容的行为，但添加日志说明 workspace 未设置。

---

### WT-17 [提示] `DiffSummaryRail.BeforeWorktreeExit` 增加了 Python 没有的安全检查

**描述**：Go 增加了 `session.OriginalHeadCommit == ""` 的安全检查。这是改进而非 bug。

**修复方案**：无需修改，Go 的防御性检查更安全。

---

## 二、Swarm Rails (10.6.3-10)

### SR-01 [严重] `RuntimePromptRail` 缺少 `Uninit` 方法，7 个 section 在 rail 卸载时残留

**描述**：Python `RuntimePromptRail` 覆写了 `uninit()`，清理 7 个注入的 section（time/runtime/language_output/env/git_status/browser_tool_policy/trusted_dirs_policy）。Go 没有 `Uninit` 方法，rail 卸载后 section 残留。

**Python 样例**：
```python
def uninit(self, agent) -> None:
    if self.system_prompt_builder is not None:
        self.system_prompt_builder.remove_section("time")
        self.system_prompt_builder.remove_section("runtime")
        self.system_prompt_builder.remove_section("language_output")
        self.system_prompt_builder.remove_section("env")
        self.system_prompt_builder.remove_section("git_status")
        self.system_prompt_builder.remove_section("browser_tool_policy")
        self.system_prompt_builder.remove_section("trusted_dirs_policy")
    self.system_prompt_builder = None
```

**Go 问题代码位置**：`internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go`

**修复方案**：
```go
func (r *RuntimePromptRail) Uninit(agent interfaces.BaseAgent) error {
    if spb := agent.SystemPromptBuilder(); spb != nil {
        for _, name := range []string{"time", "runtime", "language_output",
            "env", "git_status", "browser_tool_policy", "trusted_dirs_policy"} {
            spb.RemoveSection(name)
        }
    }
    r.systemPromptBuilder = nil
    return nil
}
```

---

### SR-02 [严重] `AvatarPromptRail` 缺少 `Uninit` 方法，section 残留

**描述**：当 AvatarPromptRail 被卸载时，`forbidden_memory`/`avatar_identity`/`group_chat_memory_notice`/`memory_fully_disabled`/`interaction_guidance` 这些 section 会残留在 prompt 中。对比 `ProjectMemoryRail` 的 Go 实现已正确实现了 `Uninit()`。

**Go 问题代码位置**：`internal/swarm/agents/harness/common/rails/avatar_rail.go`

**修复方案**：添加 `Uninit` 方法，遍历 `injectedSections` 调用 `builder.RemoveSection(name)`。

---

### SR-03 [严重] `StreamEventRail` 完全未实现

**描述**：Python `JiuClawStreamEventRail` 负责流事件发射（tool_call/tool_result/todo.updated/context.usage）、暂停/中止检查、中断工具上下文修复、JSON 参数修复。Go 端 `buildStreamEventRail()` 返回 nil，整个流事件系统缺失。

**Python 样例**：
```python
class JiuClawStreamEventRail(DeepAgentRail):
    async def before_invoke(self, ctx): ...    # 捕获 conversation_id
    async def before_model_call(self, ctx): ...  # pause check + context fix
    async def after_model_call(self, ctx): ...   # context_usage emit
    async def before_tool_call(self, ctx): ...   # pause check + tool_call emit
    async def after_tool_call(self, ctx): ...    # tool_result + ask_user_question + todo.updated
    async def on_model_exception(self, ctx): ... # context repair
```

**Go 问题代码位置**：`internal/swarm/server/adapter/deep_adapter_rails.go` L412-415
```go
func (d *DeepAdapter) buildStreamEventRail() sainterfaces.AgentRail {
    return nil  // ⤵️ 10.6.3-10
}
```

**修复方案**：实现完整的 `JiuClawStreamEventRail`，6 个回调点全部对齐 Python。

---

### SR-04 [严重] `ResponsePromptRail` 完全未实现

**描述**：Python `ResponsePromptRail` 在 `before_model_call` 中注入 response/message-format section。Go 端 `buildResponsePromptRail()` 返回 nil。

**Python 样例**：
```python
class ResponsePromptRail(DeepAgentRail):
    def init(self, agent):
        self.system_prompt_builder = getattr(agent, "system_prompt_builder", None)
    def uninit(self, agent):
        if self.system_prompt_builder is not None:
            self.system_prompt_builder.remove_section("response")
    async def before_model_call(self, ctx):
        section = _response_prompt(self.system_prompt_builder.language or "cn")
        self.system_prompt_builder.add_section(section)
```

**Go 问题代码位置**：`internal/swarm/server/adapter/deep_adapter_rails.go` L494-497

**修复方案**：实现 `ResponsePromptRail`，包含 `Init`/`Uninit`/`BeforeModelCall`。

---

### SR-05 [严重] `extractQuestionsFromValue` 只处理 `map[string]any`，不处理结构体类型

**描述**：Python 的 `_extract_questions_from_value` 支持 `hasattr(value_obj, 'questions')` 和 `isinstance(value_obj, dict)` 两种路径。Go 只处理 `map[string]any`，如果 `valueObj` 是结构体（如 `InterruptRequest`），Go 无法提取 `questions`，导致 AskUserRail 的结构化问题在前端不可见。

**Python 样例**：
```python
if hasattr(value_obj, 'questions'):
    qs = value_obj.questions
    if qs and len(qs) > 0: return qs
elif isinstance(value_obj, dict):
    qs = value_obj.get("questions", [])
```

**Go 问题代码位置**：`internal/agentcore/harness/rails/interrupt/helpers.go` L190-228

**修复方案**：添加对 `saschema.InterruptRequester` 接口的类型分支处理。

---

### SR-06 [严重] `AvatarPromptRail.before_tool_call` 中 `shouldDisableMemory` 分支缺少 `return`

**描述**：Python 在 `should_disable_memory` 为 true 时无论是否匹配记忆工具都 `return`（不检查写入拦截），Go 在不匹配时 fall through 到群聊写入拦截分支，行为不一致。

**Python 样例**：
```python
if should_disable_memory:
    if tool_name in all_memory_tools:
        self._reject_tool(ctx, "[PERMISSION_DENIED] 记忆系统已禁用")
    return  # ← 无论是否匹配都 return
```

**Go 问题代码位置**：`internal/swarm/agents/harness/common/rails/avatar_rail.go` L195-212
```go
if shouldDisableMemory {
    if _, exists := memoryAllTools[toolInputs.ToolName]; exists {
        r.rejectTool(cbc, toolInputs, "[PERMISSION_DENIED] 记忆系统已禁用")
        return nil
    }
    // ← 缺少 return nil，fall through 到场景1
}
```

**修复方案**：在 `shouldDisableMemory` 为 true 的分支末尾（无论是否匹配）添加 `return nil`。

---

### SR-07 [一般] `RuntimePromptRail` 缺少 `Init` 方法

**描述**：Python 在 `init()` 中保存 `system_prompt_builder` 引用。Go 在 `BeforeModelCall` 中获取。虽然功能等价，但缺少 init 意味着 Go 无法在 init 阶段做前置校验。

**修复方案**：添加 `Init` 方法，保存 `systemPromptBuilder` 引用，与 Python 生命周期管理对齐。

---

### SR-08 [一般] `RuntimePromptRail` injectLanguageOutputSection 缺少 `RemoveSection` 先行清除

**描述**：Python 在注入 language_output section 前先 `remove_section("language_output")`，Go 直接 `add_section`。虽然 `AddSection` 内部可能已有去重逻辑，但 Python 的模式更安全。

**Python 样例**：
```python
self.system_prompt_builder.remove_section("language_output")
# ...
self.system_prompt_builder.add_section(...)
```

**Go 问题代码位置**：`internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go` L361-382

**修复方案**：在 `injectLanguageOutputSection` 开头添加 `builder.RemoveSection("language_output")`。

---

### SR-09 [一般] `RuntimePromptRail` OS 版本信息与 Python 不对齐

**描述**：Python 使用 `platform.system()` + `platform.release()`（如 "Linux 5.15.0"），Go 使用 `runtime.GOOS` + `runtime.GOARCH`（如 "linux amd64"）。

**Python 样例**：
```python
os_version = f"{plat.system()} {plat.release()}"  # "Linux 5.15.0-91-generic"
```

**Go 问题代码**：
```go
osVersion := fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)  // "linux amd64"
```

**修复方案**：使用 `syscall.Uname` 或执行 `uname -r` 获取内核版本。

---

### SR-10 [一般] `RuntimePromptRail` existingDirs 缺少 `ExpandHome`（`~` 展开）

**描述**：Python 使用 `os.path.expanduser(item.strip())` 展开 `~`，Go 只调 `filepath.Abs` + `filepath.Clean`。上次审查（RP-02）已标记但未修复。

**修复方案**：使用 `expandUserPath`（参考 permissions_persist.go）展开 `~` 前缀。

---

### SR-11 [一般] `ProjectMemoryRail.BeforeModelCall` 缺少异常保护

**描述**：Python 的 `before_model_call` 中 `discover_and_load_memory_files` 调用被 try/except 包裹。Go 版本没有显式的防御性 recover。

**Python 样例**：
```python
try:
    files = discover_and_load_memory_files(...)
except (OSError, ValueError, TypeError) as exc:
    logger.exception("[ProjectMemoryRail] discovery failed ...")
    files = []
```

**修复方案**：添加 `defer recover` 保护，panic 时降级为空文件列表。

---

### SR-12 [一般] `AvatarPromptRail` 缺少 `buildMemoryDisabledPrompt`（写入禁用但允许读取）

**描述**：Python 有两个记忆禁用提示词函数（写入禁用/完全禁用），Go 只实现了完全禁用版本。

**修复方案**：添加 `buildMemoryDisabledPrompt(language string) string`，一比一复刻 Python。

---

### SR-13 [一般] `permissions_persist.go` 缺少 `PersistCliTrustedDirectory`（无 overrides 版本）

**描述**：Python 有两个 CLI 信任目录函数（含/不含 approval_overrides），Go 只实现了含 overrides 版本。

**修复方案**：添加 `PersistCliTrustedDirectory(rawPath string) map[string]any`。

---

### SR-14 [一般] `interrupt/helpers.go` 中 `extractInteractionParts` 不处理结构体类型

**描述**：与 SR-05 同类问题，Python 支持 `hasattr(interaction, 'id')` 对象路径，Go 只处理 `map[string]any`。

**修复方案**：添加对 `saschema.InterruptRequester` 接口的类型分支处理。

---

### SR-15 [一般] `ProjectMemoryRail.BeforeModelCall` 缺少 logger.exception 级别日志

**修复方案**：在 `DiscoverAndLoadMemoryFiles` 调用前后添加防御性日志。

---

### SR-16 [一般] `readRuntimeStateYAML` 在 BeforeModelCall 中被调用 3 次，冗余文件 I/O

**描述**：上次审查（RP-04）已标记但未修复。

**修复方案**：在 `BeforeModelCall` 中读一次 `runtimeState`，作为参数传给各 inject 方法。

---

### SR-17 [提示] `RuntimePromptRail` injectTimeSection 中文模式 content map 键名

**描述**：中文模式下 `time` section content 有 `cn`/`en` 两个键。与 Python 一致，无需修改。

---

### SR-18 [提示] `permissions_config_rpc.go` 的 dispatch 与 Python 的 try/except 分支

**描述**：Go 的 error 返回 + panic recover 模式已等价覆盖 Python 的 ValueError + Exception 两层 except。无需修改。

---

### SR-19 [提示] `owner_scopes.go` 使用 `context.Context` 而非 `ContextVar`

**描述**：Go 惯用模式，功能等价。无需修改。

---

### SR-20 [提示] `RuntimePromptRail` 缺少 `timezone_offset` 参数

**描述**：上次审查（RP-09）已标记。低优先级。

---

## 三、Evolution Rails (9.24)

### EV-01 [严重] `SkillEvolutionRail.RunEvolution` 缺少顶层异常捕获

**描述**：Python `run_evolution` 在方法体最外层有 `try/except Exception` 捕获全局异常。Go 版本没有全局异常保护，如果内部步骤 panic，整个后台任务崩溃。

**Python 样例**：
```python
try:
    # ... 全部 run_evolution 逻辑 ...
except Exception as exc:
    logger.warning("[SkillEvolutionRail] auto evolution failed: %s", exc)
```

**Go 问题代码位置**：`internal/agentcore/harness/rails/evolution/skill_evolution_rail.go` L560-694

**修复方案**：
```go
func (r *SkillEvolutionRail) RunEvolution(...) error {
    defer func() {
        if rec := recover(); rec != nil {
            logger.Error(logComponent).Any("panic", rec).Msg("[SkillEvolutionRail] run_evolution panic")
            r.emitProgress("failed", fmt.Sprintf("技能演进因意外错误失败: %v", rec))
        }
    }()
    // ...
}
```

---

### EV-02 [严重] `SkillEvolutionRail.RunEvolution` 同步路径缺少 `presented_entries` 消费

**描述**：Python 同步路径中通过 `self._experience_tracker.consume_eval_state(session)` 获取 `presented_entries`。Go 版本同步路径中未获取，导致同步模式下已展示的经验不会被评估。

**Python 样例**：
```python
elif ctx is not None:
    messages = self._collect_messages_from_trajectory(trajectory)
    session = ctx.session if hasattr(ctx, "session") else None
    presented_entries = self._experience_tracker.consume_eval_state(session)
```

**Go 问题代码位置**：`internal/agentcore/harness/rails/evolution/skill_evolution_rail.go` L577-583

**修复方案**：在同步路径中补充 `presentedEntries` 获取：
```go
} else if traj != nil {
    messages = collectMessagesFromTrajectory(traj)
    // 同步路径：消费评估状态
    sessionID := ""
    if cbc != nil && cbc.Session() != nil {
        sessionID = cbc.Session().GetSessionID()
    }
    presentedEntries = r.experienceTracker.ConsumeEvalState(sessionID)
}
```

---

### EV-03 [严重] `TeamSkillEvolutionRail.RunEvolution` 同步路径同样缺少 `presented_entries`

**描述**：与 EV-02 同类问题。

**Go 问题代码位置**：`internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go` L609-617

**修复方案**：同 EV-02。

---

### EV-04 [严重] `TeamSkillEvolutionRail.RunEvolution` 信号统计逻辑与 Python 不一致

**描述**：Python 对 `trajectory_issue_signals` 展开计算 `issue_count`，Go 只统计了信号数量。

**Python 样例**：
```python
issue_count = sum(len(get_team_trajectory_issues(signal)) for signal in trajectory_issue_signals)
self._emit_progress("detecting_signals",
    f"evolution signals detected: {issue_count} trajectory issues, "
    f"{len(signals) - len(trajectory_issue_signals)} user intents")
```

**Go 问题代码位置**：`internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go` L670-677

**修复方案**：展开统计 `issueCount`。

---

### EV-05 [严重] `SkillEvolutionRail.RunEvolution` 中 `detectUserIntent` 错误被静默丢弃

**描述**：Python 中 `detect_user_intent` 的异常被外层 try/except 捕获，Go 中返回的错误被 `_` 忽略。

**Python 样例**：
```python
user_intent_signals = await detector.detect_user_intent(messages)
# 异常被外层 except Exception 捕获并记录
```

**Go 问题代码位置**：`internal/agentcore/harness/rails/evolution/skill_evolution_rail.go` L627
```go
userIntentSignals, _ := detector.DetectUserIntent(ctx, messages)
```

**修复方案**：
```go
userIntentSignals, err := detector.DetectUserIntent(ctx, messages)
if err != nil {
    logger.Warn(logComponent).Err(err).Msg("[SkillEvolutionRail] 用户意图检测失败")
}
```

---

### EV-06 [一般] `EVOLUTION_SHARING_ENABLED` 优先级逻辑与 Python 不一致

**描述**：Python 先检查环境变量再 fallback 到 config，Go 先检查 config 再被环境变量覆盖。两者优先级不同。

**Python 样例**：
```python
env_enabled = os.getenv("EVOLUTION_SHARING_ENABLED")
enabled = cls._resolve_bool(env_enabled) if env_enabled is not None else cls._resolve_bool(config.get("enabled", False))
```

**修复方案**：调整优先级，使环境变量检查优先于 config。

---

### EV-07 [一般] `initSharing` 缺少 `_resolve_download_top_k` 逻辑

**描述**：Go 始终使用硬编码的 `defaultSharingDownloadTopK=3`，Python 从 config 中提取。

**修复方案**：在 `initSharing` 中从 `r.sharingConfig` 解析 `download_top_k`。

---

### EV-08 [一般] `buildExperienceSharer` 缺少 backend 类型检查和回退

**描述**：Python 检查 `config["backend"]` 是否为 `"local_file"`，不是则警告并回退。Go 直接创建 `LocalFileBackend`。

**修复方案**：添加 backend 类型检查和警告日志。

---

### EV-09 [一般] `SkillEvolutionRail.ApproveRecord` 缺少 `delete(pendingApprovalSnapshots, requestID)` 内存泄漏

**描述**：`TeamSkillEvolutionRail.ApproveRecord` 有 delete 清理，但 `SkillEvolutionRail.ApproveRecord` 缺少。

**修复方案**：在审批完成后添加 `delete(r.pendingApprovalSnapshots, requestID)`。

---

### EV-10 [一般] `_extract_tool_content` 缺少 `data` 中间层处理

**描述**：Python 先通过 `getattr(result, "data", None)` 获取 data 字典，Go 直接将 result 断言为 `map[string]any`。

**Python 样例**：
```python
data = getattr(result, "data", None)
if isinstance(data, dict):
    content = data.get("skill_content") or data.get("content") or ""
```

**修复方案**：添加 `data` 中间层处理逻辑。

---

### EV-11 [一般] `TeamSkillEvolutionRail.EvolutionConfig` 缺少 `max_concurrent_evolution` 字段

**修复方案**：添加 `"max_concurrent_evolution": cap(r.EvolutionRail.evolutionSem)` 到返回的 map 中。

---

### EV-12 [一般] `SkillEvolutionRail.OnAfterToolCall` 未将 session 传递给 ExperienceTracker

**描述**：Python 传递 session 对象，Go 只传 sessionID 字符串。需确认 Go 接口兼容性。

---

### EV-13 [一般] `teamExtractToolContent` 和 `extractToolContent` 代码重复

**修复方案**：提取到 `helpers.go` 作为包级共享函数。

---

### EV-14 [一般] `EvolutionSnapshot` Go 版本扩展字段需标注

**描述**：Go 的 `EvolutionSnapshot` 包含了 Python 不存在的 `PresentedEntries`/`SessionID`/`IncrementalMessages` 字段。这是合理的 Go 设计（强类型无法动态注入 dict key），但应在注释中标注这是 Go 对 Python dict 动态注入的替代方案。

---

### EV-15 [一般] `EvolutionRail.AfterModelCall` 中 `detail.Tools` 空时赋值 nil 切片而非 nil

**修复方案**：确保空时设置 `detail.Tools = nil`（与 Python `None` 对齐）。

---

### EV-16 [提示] 缺少 `ContextEvolutionRail` 的 Go 实现

**描述**：doc.go 中已标注"P6（ContextEvolutionRail）提供基础类型和基类"，为已知的待实现项。

---

### EV-17 [提示] 缺少旧版兼容方法

**描述**：`generate_and_emit_experience`/`_legacy_user_intent`/`_detect_signals`/`_parse_messages` 是旧版 API，Go 可选择跳过。

---

### EV-18 [提示] `sharing/` 子目录为空，共享逻辑内联到 `skill_evolution_rail.go`

**描述**：Go 无 mixin 机制，但文件较长（2159 行）。建议拆分到 `skill_evolution_sharing.go`。

---

### EV-19 [提示] `inferSkillFromTexts` 是简化实现

**描述**：Go 版本只搜索 `skill_tool_payloads` 和 `SKILL.md` 路径。确认 `internal/evolving` 包中是否有完整版本。

---

### EV-20 [提示] `_SHARED_RECORD_CONTEXT_MARKER` 常量未定义，直接内联字符串

**修复方案**：定义常量 `const sharedRecordContextMarker = "[shared origin="`。

---

### EV-21 [提示] `EvolutionEventKind` Go 中是 `type alias = string`，Python 中是 `Literal`

**描述**：Go 语言限制，已通过常量定义弥补。无需修改。

---

## 四、适配器辅助 (10.3.7-11)

### AD-01 [严重] `getCodeModeTools` 从 `configCache`（仅 react 段）读取 `modes`，始终返回 nil

**描述**：`configCache` 仅缓存 `configBase["react"]` 配置段，不包含 `modes` 字段。`getCodeModeTools` 从 `configCache` 读取 `modes.code.tools` 始终返回 nil，导致 Code 模式工具列表回退到 Deep 模式默认集，用户配置的 Code 模式工具不生效。

**Python 样例**：
```python
mode_config = config_base.get("modes", {}).get("code", {})
configured_tools = mode_config.get("tools") or []
```

**Go 问题代码位置**：`internal/swarm/server/adapter/code_adapter.go` L785-808
```go
func (c *CodeAdapter) getCodeModeTools() []string {
    if c.deep.configCache == nil { return nil }
    modes, ok := c.deep.configCache["modes"].(map[string]any)  // ← configCache 不含 modes
    if !ok { return nil }
    // ...
}
```

**修复方案**：`getCodeModeTools` 应从完整配置 `configBase` 读取。建议在 CodeAdapter 上缓存 `configBase`，或在 `CreateInstance` 时缓存 `modes` 段：
```go
func (c *CodeAdapter) getCodeModeTools() []string {
    if c.configBase == nil { return nil }
    modes, ok := c.configBase["modes"].(map[string]any)
    if !ok { return nil }
    // ...
}
```

---

### AD-02 [严重] slash 命令已实现但未接入 ProcessMessage 流程

**描述**：`handleSlashCommand` 方法已完整实现（evolve/simplify/rebuild/rollback/list），但 `ProcessMessageImpl` 和 `ProcessMessageStreamImpl` 中对应调用处标记为 `⤵️ 10.6.3-10` 未调用。用户无法通过 `/evolve` 等命令触发操作。

**Python 样例**：
```python
slash_result = await self._handle_slash_command(query, session_id, mode)
if slash_result is not None:
    return slash_result
```

**Go 问题代码位置**：`internal/swarm/server/adapter/deep_adapter.go` L747, L898

**修复方案**：将 `⤵️` 注释替换为实际调用：
```go
slashResult := d.handleSlashCommand(query, sessionID, mode)
if slashResult != nil {
    return slashResult
}
```

---

### AD-03 [严重] `ProcessMessageImpl/StreamImpl` 未调用 `updateRuntimeConfig`

**描述**：Python 中步骤 17 是 `await self._update_runtime_config(runtime_config)`，Go 在步骤 16-17 仅做了 `seedRuntimeCwd` 但未构造完整 `runtimeConfig` 并调用 `updateRuntimeConfig`。运行时提示词/语言/频道等配置不会在每次请求时更新。

**Python 样例**：
```python
runtime_config = self._build_runtime_config(channel=channel_id, language=language, ...)
await self._update_runtime_config(runtime_config)
```

**Go 问题代码位置**：`internal/swarm/server/adapter/deep_adapter.go` L782-792

**修复方案**：构造完整 runtimeConfig 并调用 `updateRuntimeConfig`。

---

### AD-04 [严重] `agent_history` 路径修正标记为 `⤵️` 未实现

**描述**：Python 中该逻辑遍历 instance 的 registered_rails 修改工具的 `_workspace_path`。Go 中注释说"Go 中工具没有 _workspace_path 属性"。这意味着 agent 历史文件可能写入用户项目目录而非 agent 系统 workspace。

**Go 问题代码位置**：`internal/swarm/server/adapter/code_adapter.go` L388

**修复方案**：设计 Go 等价方案，确保 `.agent_history` 写入路径为 agent 系统 workspace。

---

### AD-05 [一般] `evolutionEventKindLocal` 默认返回 `"progress"` 而非 `"stream"`

**描述**：公共 helpers 的 `EvolutionEventKind` 默认返回 `"stream"`，adapter 层内联版本默认返回 `"progress"`。两者不一致。

**修复方案**：统一内联版本默认返回 `"stream"`，或使用公共 `EvolutionEventKind`。

---

### AD-06 [一般] `SysOpBuilder` 缺少 3 个显示/查询函数

**描述**：Python 暴露了 `list_auto_managed_sandbox_paths`/`list_effective_sandbox_files`/`find_auto_managed_match`，用于 `/sandbox status` UI 展示。Go 未实现。

**修复方案**：在 `sysop_builder` 包中添加对应函数。

---

### AD-07 [一般] `CodeAdapter.getCodeModeTools` 中 `TOOL_GROUPS` 完整性需确认

**描述**：Python `TOOL_GROUPS` 包含 5 个分组（核心/搜索/代码智能/高级/可视化），Go 引用 `types.ToolGroups`。需确认是否包含完整 5 个分组。

---

### AD-08 [一般] `CodeAdapter.getCodeModeTools` 中 `DISALLOWED_FOR_SUBAGENTS` 完整性需确认

**描述**：Python 包含 7 个工具名，Go 引用 `types.DisallowedForSubagents`。需确认是否包含完整 7 个工具名。

---

### AD-09 [一般] `handleMemoryRailByConfig` 和 `handleExternalMemoryRailByConfig` 标记为 `⤵️`

**描述**：Python 中这两个方法负责按配置切换 memory rail（plan 模式下注册、fast 模式下注销）。

**Go 问题代码位置**：`internal/swarm/server/adapter/deep_adapter_rails.go` L633-637, L775-779

**修复方案**：待实现 memory rail 模式切换逻辑。

---

### AD-10 [一般] `ReloadAgentConfig` 中 `SkillCreateRail` 处理标记为 `⤵️`

**Go 问题代码位置**：`internal/swarm/server/adapter/deep_adapter_rails.go` L713-714

**修复方案**：待实现。

---

### AD-11 [一般] `getCurrentAgentRails` 缺少动态 rails 获取逻辑

**描述**：Python `_get_current_agent_rails` 根据配置状态决定是否将某些 rail 纳入重载列表。Go 缺少完整实现。

**修复方案**：待补充。

---

### AD-12 [一般] `ConfigureTeamMemberAgent` 缺少 Python 中的 `_jiuwenswarm_code_team_member` 标记

**修复方案**：添加 team member 标记属性。

---

### AD-13 [一般] `setCodingMemoryDirectory` 标记为 `⤵️`

**Go 问题代码位置**：`internal/swarm/server/adapter/code_adapter.go` L1425

**修复方案**：待实现。

---

### AD-14 [一般] `loadUserRails` 标记为 `⤵️`

**Go 问题代码位置**：`internal/swarm/server/adapter/code_adapter.go` L400, `deep_adapter.go` L541

**修复方案**：待实现。

---

### AD-15 [提示] `CodeAdapter.codeFixedRailNames` 缺少 `TeamSkillEvolutionRail`

**描述**：需确认 Code 模式是否需要支持 TeamSkillEvolutionRail。

---

### AD-16 [提示] `AgentTool` 中 `model` 参数未解析

**描述**：Python 支持子 agent model override，Go 当前使用父 agent model。

---

### AD-17 [提示] `CompressContext` 中 `"default_context"` 是否与 Python 默认 context 名称一致

**修复方案**：确认 Go 和 Python 的 ContextEngine 默认 context 名称是否一致。

---

### AD-18 [提示] `CreateSysOperationFromCard` 的隔离键复用+并发重试逻辑是 Go 增强

**描述**：Python 中这部分逻辑分散在 `Runner.resource_mgr.add_sys_operation`。Go 实现更健壮，无需修改。

---

## 修复优先级

### P0 — 必须立即修复（运行时 bug 或功能完全缺失）

| 编号 | 问题 | 影响 |
|------|------|------|
| WT-01 | `WithWorktreeSessionState` 返回值被丢弃 | Worktree 整个 session 机制失效 |
| WT-02 | `Init/Uninit` 未注册/注销工具 | WorktreeRail 完全不起作用 |
| AD-01 | `getCodeModeTools` 从 configCache 读取 modes | Code 模式工具列表始终回退到默认集 |
| AD-02 | slash 命令已实现但未接入 | 用户无法使用 /evolve 等命令 |
| AD-03 | 未调用 `updateRuntimeConfig` | 运行时提示词/语言/频道不随请求更新 |

### P1 — 应尽快修复（功能缺失或行为不一致）

| 编号 | 问题 | 影响 |
|------|------|------|
| SR-01 | RuntimePromptRail 缺少 Uninit | rail 卸载后 section 残留 |
| SR-02 | AvatarPromptRail 缺少 Uninit | section 残留 |
| SR-06 | AvatarPromptRail before_tool_call fall through | 记忆禁用检查行为不一致 |
| EV-01 | RunEvolution 缺少顶层异常捕获 | panic 导致后台任务崩溃 |
| EV-02/03 | RunEvolution 同步路径缺少 presented_entries | 已展示经验不被评估 |
| EV-05 | detectUserIntent 错误静默丢弃 | 诊断信息丢失 |
| WT-03 | WorktreeLifecycleRail 缺少 3 个 hook | 安全控制不可用 |

### P2 — 计划中修复（完整性补齐）

| 编号 | 问题 |
|------|------|
| SR-03 | StreamEventRail 未实现（⤵️标记正确） |
| SR-04 | ResponsePromptRail 未实现（⤵️标记正确） |
| SR-05/14 | extractQuestionsFromValue/extractInteractionParts 结构体类型支持 |
| AD-04 | agent_history 路径修正 |
| EV-04 | TeamSkillEvolutionRail 信号统计不一致 |
| WT-04 | fireRail 空实现 |
