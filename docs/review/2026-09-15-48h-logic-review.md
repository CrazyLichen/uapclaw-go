# 48小时逻辑审查报告（2026-09-15）

> 审查范围：48小时内提交记录对应的 4 个实现章节
> 审查方法：对照 Python 参考项目逐方法比对签名与步骤

## 审查范围

| 章节 | 状态 | Go 路径 | Python 参考路径 |
|------|------|---------|----------------|
| 9.66a WorktreeManager | ✅ | `internal/agentcore/harness/tools/worktree/` | `openjiuwen/harness/tools/worktree/` |
| 9.24 P3/P4 SkillEvolutionRail | ✅ | `internal/agentcore/harness/rails/evolution/` | `openjiuwen/harness/rails/evolution/` |
| 10.6.7 RuntimePromptRail | ✅ | `internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go` | `jiwenswarm/agents/harness/common/rails/runtime_prompt_rail.py` |
| 10.6.7 ProjectMemoryRail | ✅ | `internal/swarm/agents/harness/common/rails/project_memory_rail.go` | `jiwenswarm/agents/harness/common/rails/project_memory_rail.py` |

---

## 问题总览

| 分类 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| W — WorktreeManager (9.66a) | 6 | 9 | 5 | 20 |
| E — SkillEvolutionRail (9.24) | 7 | 9 | 6 | 22 |
| R — RuntimePromptRail (10.6.7) | 4 | 7 | 4 | 15 |
| P — ProjectMemoryRail (10.6.7) | 3 | 7 | 4 | 14 |
| **合计** | **20** | **32** | **19** | **71** |

---

## 一、WorktreeManager (9.66a) — 20 个问题

### 严重问题（6 个）

#### W-01 [严重] `fireRail` 是空实现，Python 的 `_fire_rail` 实际调用 rails

**Python 样例** (`manager.py:694-714`):
```python
async def _fire_rail(self, method: str, *args: Any, **kwargs: Any) -> Any:
    result = None
    for rail in self._rails:
        handler = getattr(rail, method, None)
        if handler:
            r = await handler(*args, **kwargs)
            if r is not None:
                result = r
    return result
```

**Go 问题** (`manager.go:443-447`):
```go
func (m *WorktreeManager) fireRail(method string, args ...any) any {
    // 当前实现只支持 BeforeWorktreeCreate 和 BeforeWorktreeExit
    // 完整的 rail 调度在 rails.go 的 WorktreeRail 中处理
    return nil
}
```

**问题描述**：Go 的 `fireRail` 永远返回 nil，不做任何实际调用。Python 通过反射调用注册的 rail 上对应方法名。Go 端的 WorktreeManager 完全无法通过 `fireRail` 触发 lifecycle hooks。

**修复方案**：实现正确的 `fireRail`，遍历 `m.lifecycleRails`，按接口方法名调用。更 Go-idiomatically，在 `Enter`/`Exit` 中直接调用 `WorktreeLifecycleRail` 接口方法，不需要通过字符串方法名反射。

---

#### W-02 [严重] `WorktreeLifecycleRail` 接口缺失 Python 的全部 hooks

**Python 样例** (`rails.py:214-391`)：
WorktreeLifecycleRail 定义了 7+ 个 hook 方法：`before_worktree_create`、`after_worktree_create`、`before_worktree_exit`、`after_worktree_exit`、`on_worktree_file_write`、`before_worktree_commit`、`after_worktree_commit`、`on_worktree_sync`

**Go 问题** (`backend.go:32-41`)：
`WorktreeLifecycleRail` 接口只定义了 4 个：`BeforeWorktreeCreate`、`AfterWorktreeCreate`、`BeforeWorktreeExit`、`AfterWorktreeExit`

**问题描述**：缺失 4 个 hook 方法：`OnWorktreeFileWrite`、`BeforeWorktreeCommit`、`AfterWorktreeCommit`、`OnWorktreeSync`。Go 端无法实现文件写入拦截、提交拦截和同步拦截。

**修复方案**：在 `WorktreeLifecycleRail` 接口中补充缺失的 4 个方法，并为 `AutoSetupRail` 和 `DiffSummaryRail` 补充空实现。

---

#### W-03 [严重] `simpleMatch` 不是 `fnmatch` 的等价实现

**Python 样例** (`manager.py:453`)：
```python
fnmatch.fnmatch(entry, p)  # 支持 *, ?, [seq] 标准 glob 模式
```

**Go 问题** (`manager.go:560-569`):
```go
func simpleMatch(name, pattern string) bool {
    if pattern == "*" { return true }
    if strings.Contains(pattern, "*") {
        prefix := strings.TrimSuffix(pattern, "*")
        return strings.HasPrefix(name, prefix)
    }
    return name == pattern
}
```

**问题描述**：`simpleMatch` 只处理最简单的 `prefix*` 和完全匹配两种情况。不支持 `?`、`[seq]`、`*` 在中间（如 `config/*/secrets`）。例如 pattern `.env.*` 无法正确匹配 `.env.local`。

**修复方案**：使用 `path.Match`（Go 标准库，支持 `*`/`?`/`[seq]`）替代当前手写实现。

---

#### W-04 [严重] `copyFile` 不保留文件权限，Python 用 `shutil.copy2`

**Python 样例** (`manager.py:458`)：
```python
shutil.copy2(src, dst)  # 保留元数据（权限、atime/mtime）
```

**Go 问题** (`manager.go:572-578`):
```go
func copyFile(src, dst string) error {
    data, err := os.ReadFile(src)
    if err != nil { return err }
    return os.WriteFile(dst, data, 0o644)  // 硬编码 0o644
}
```

**问题描述**：硬编码 `0o644` 覆盖了原文件权限（可执行脚本变成不可执行）；不保留 mtime/atime。对于 `include_patterns` 拷贝的配置文件和脚本，权限丢失会导致 worktree 中文件不可执行。

**修复方案**：使用 `os.Stat(src)` 获取原文件权限，然后用 `os.WriteFile(dst, data, mode)` 保留权限。并用 `os.Chtimes` 保留 mtime。

---

#### W-05 [严重] `gitEnv()` 会产生重复环境变量

**Python 样例** (`git.py:43-53`)：
```python
env = os.environ.copy()
env["GIT_TERMINAL_PROMPT"] = "0"  # dict key 唯一性保证覆盖
env["GIT_ASKPASS"] = ""
```

**Go 问题** (`git.go:353-358`):
```go
func gitEnv() []string {
    env := os.Environ()
    env = append(env, "GIT_TERMINAL_PROMPT=0")  // append 可能重复
    env = append(env, "GIT_ASKPASS=")
    return env
}
```

**问题描述**：`os.Environ()` 返回 `[]string`，如果系统已有 `GIT_TERMINAL_PROMPT=1`，`append` 会导致两个条目。Python 的 dict 赋值天然保证 key 唯一性。

**修复方案**：构建 `map[string]string` 从 `os.Environ()`，覆盖新值，再转回 `[]string`。

---

#### W-06 [严重] `WorktreeRail.Init` 中 context 注入失败，session 管理完全失效

**Python 样例** (`rails.py:115-116`)：
```python
# Python 使用 asyncio ContextVar，init_session_state() 在当前 task 设置 holder
```

**Go 问题** (`rails.go:139-140`):
```go
state := InitWorktreeSessionState()
_ = WithWorktreeSessionState(ctx, state)  // 丢弃了返回的新 context！
```

**问题描述**：`WithWorktreeSessionState` 返回一个新的 `context.Context`，但用 `_` 丢弃了返回值。session state 实际上没有被注入到任何可传播的 context 中。后续 `GetCurrentSession(ctx)` / `SetCurrentSession(ctx, ...)` 都将找不到 state holder，导致 worktree session 管理完全失效。

**修复方案**：需要让 `Init` 返回新的 `context.Context`，或者让调用方（如 CodeAdapter.buildWorktreeRails）将 state 注入到 agent 的 context 中。具体流程示例：
```
1. WorktreeRail.Init() 创建 state 并返回新 ctx
2. CodeAdapter.buildWorktreeRails() 将新 ctx 传给 agent
3. 后续 EnterTool/ExitTool 通过 agent 的 ctx 找到 session state
```

---

### 一般问题（9 个）

#### W-07 [一般] `AutoSetupRail` 丢失 `commands` 参数

**Python 样例** (`rails.py:404`)：
```python
AutoSetupRail(commands: list[str] | None = None)
```

**Go 问题** (`rails.go:53`)：
```go
AutoSetupRail struct{}  // 无任何字段
```

**修复方案**：给 `AutoSetupRail` 添加 `commands []string` 字段和构造函数。

---

#### W-08 [一般] `WorktreeRail.Init` 硬编码 `lang="cn"` 和 `agentID=""`，未从 agent 获取

**Python 样例** (`rails.py:115-116`)：
```python
lang = agent.system_prompt_builder.language
agent_id = getattr(getattr(agent, "card", None), "id", None)
```

**Go 问题** (`rails.go:128-129`):
```go
lang := "cn"
agentID := ""
```

**修复方案**：从 `interfaces.BaseAgent` 参数中获取 language 和 agent_id。

---

#### W-09 [一般] `WorktreeRail.Uninit` 未移除 agent 上的工具

**Python 样例** (`rails.py:138-148`)：
```python
def uninit(self, agent) -> None:
    for tool in self._tools:
        name = getattr(tool.card, "name", None)
        if name and hasattr(agent, "ability_manager"):
            agent.ability_manager.remove(name)
        tool_id = getattr(tool.card, "id", None)
        if tool_id:
            Runner.resource_mgr.remove_tool(tool_id)
    self._tools = []
```

**Go 问题** (`rails.go:161-164`):
```go
func (r *WorktreeRail) Uninit(agent interfaces.BaseAgent) error {
    r.tools = nil
    r.manager = nil
    return nil
}
```

**修复方案**：在 `Uninit` 中遍历 `r.tools`，调用 agent 的 ability manager 清理逻辑。

---

#### W-10 [一般] `cleanup.go` 中 `status_porcelain` 错误处理与 Python 行为不一致

**Python 样例**：`status_porcelain` 失败返回空列表，`if changes:` 为 False 继续执行。
**Go 问题**：`if err != nil || len(changes) > 0 { continue }`，失败时跳过（fail-closed）。

**修复方案**：Go 的 fail-closed 更安全，建议保持当前行为并标注为有意偏差。

---

#### W-11 [一般] `DiffSummaryRail.BeforeWorktreeExit` 返回空字符串而非 nil

**Python 样例** (`rails.py:479`)：`return None` 表示不覆盖 action。
**Go 问题** (`rails.go:297`)：`return "", nil`。空字符串和 nil 语义不同。

**修复方案**：接口改为返回 `*string`（nil = 不干预），或在文档中明确空字符串 = 不干预。

---

#### W-12 [一般] `WorktreeSession.CreationDurationMs` 类型应为 `*float64`

**Python 样例** (`models.py:122`)：`creation_duration_ms: float | None = None`
**Go 问题** (`models.go:52`)：`CreationDurationMs float64`（零值无法区分"未测量"和"0ms"）

**修复方案**：改为 `*float64`，nil 表示未测量。

---

#### W-13 [一般] `copyIncludeFiles` 返回 `([]string, error)` 但 Python 不抛异常

**修复方案**：移除 error 返回值简化签名，或保留但标注当前实现不会返回 error。

---

#### W-14 [一般] `cleanup.go` 中并行检查变成串行

**Python 样例** (`cleanup.py:111-113`)：
```python
changes, unpushed = await asyncio.gather(
    status_porcelain(wt_path),
    has_unpushed_commits(wt_path),
)
```

**Go 问题**：串行执行 `StatusPorcelain` 和 `HasUnpushedCommits`。

**修复方案**：使用 goroutine 并行执行两个检查。

---

#### W-15 [一般] `WorktreeRail.Init` 未向 agent 的 ability_manager 注册工具

**Python 样例** (`rails.py:134-136`)：
```python
Runner.resource_mgr.add_tool(self._tools)
for tool in self._tools:
    agent.ability_manager.add(tool.card)
```

**Go 问题** (`rails.go:153`)：
```go
r.tools = []tool.Tool{enterTool, exitTool}  // 只创建，未注册
```

**问题描述**：工具创建了但没有注册到 agent，agent 不知道有 `enter_worktree`/`exit_worktree` 可用。

**修复方案**：在 `Init` 中调用 `agent.AbilityManager()` 注册工具卡片。

---

### 提示问题（5 个）

#### W-16 [提示] `GitError.Command` 类型不一致

Python `command: list[str]`（完整参数列表），Go `Command string`（只有命令名）。信息量少于 Python。

---

#### W-17 [提示] `WorktreeConfig.Enabled` 默认值

Python 显式 `enabled: bool = False`，Go 零值隐式 false。功能正确但意图不显式。

---

#### W-18 [提示] `StatusPorcelain` Go 返回 `error`，Python 返回空列表

Go 接口更符合惯用做法，调用方需处理两种情况。

---

#### W-19 [提示] `WorktreeSession.LifecyclePolicy` 在 Enter 中总是从 config 推断

Go 在 session 创建时就 resolve 了 policy，Python 保持 AUTO。行为差异不大。

---

#### W-20 [提示] `buildWorktreeRail` 未传 eventHandler 和 lifecycleRails

在 team 模式下，缺少 event handler 意味着其他成员无法感知 worktree 变化。

---

## 二、SkillEvolutionRail (9.24) — 22 个问题

### 严重问题（7 个）

#### E-01 [严重] `disabledSkills` 类型不一致——基类 map vs 子类 slice

**Python 样例** (`skill_evolution_rail.py:413-414`):
```python
if self._disabled_skills:
    skill_names = [name for name in skill_names if name not in self._disabled_skills]
```
Python `_disabled_skills` 是 `set[str]`。

**Go 问题** (`skill_evolution_rail.go:71`):
```go
disabledSkills []string  // 子类用 slice
```
基类 `EvolutionRail.disabledSkills` 是 `map[string]bool`（`evolution_rail.go`）。`filterRegularSkills` 调用基类的 `isSkillDisabled`（用 map），子类的 `[]string` 完全冗余。`WithDisabledSkillsSet` 同时设置两个字段，存在不一致风险。

**修复方案**：删除 SkillEvolutionRail 上的 `disabledSkills []string` 字段，统一使用基类的 `disabledSkills map[string]bool`。

---

#### E-02 [严重] `ShouldHintSimplifyOrRebuild` 实现方式与 Python 不一致

**Python 样例** (`skill_evolution_rail.py:1310-1324`):
```python
def should_hint_simplify_or_rebuild(self, skill_name: str) -> bool:
    store = self._evolution_store
    skill_dir = store.resolve_skill_dir(skill_name)
    if skill_dir is None:
        return False
    evo_path = skill_dir / "evolutions.json"
    if not evo_path.is_file():
        return False
    try:
        data = json.loads(evo_path.read_text(encoding="utf-8"))
        entries = data.get("entries", [])
        return len(entries) >= 10
    except Exception:
        return False
```

**Go 问题** (`skill_evolution_rail.go:1018-1025`):
```go
func (r *SkillEvolutionRail) ShouldHintSimplifyOrRebuild(skillName string) bool {
    evoLog, err := r.evolutionStore.LoadFullEvolutionLog(context.Background(), skillName)
    if err != nil || evoLog == nil {
        return false
    }
    return len(evoLog.Entries) >= 10
}
```

**问题描述**：Python 直接读取 `evolutions.json` 文件，Go 使用 `LoadFullEvolutionLog` 方法。如果该方法行为与直接读文件不一致（如合并其他来源），结果会不同。

**修复方案**：验证 `LoadFullEvolutionLog` 确实等价于读 `evolutions.json` 解析 `entries`；如有差异改为与 Python 一致的文件直读方式。

---

#### E-03 [严重] `handleEvolutionFromSignals` 中 orchestrator.Evolve 缺少 `source` 参数

**Python 样例** (`skill_evolution_rail.py:865-873`):
```python
return await self._online_orchestrator.evolve(
    skill_name=skill_name,
    ...
    source="experience_updater",
)
```

**Go 问题** (`skill_evolution_rail.go:1656-1666`):
```go
result, err := r.orchestrator.Evolve(
    ctx, skillName, convertSignalsToValues(signals), messages, userQuery,
    nil,       // trajectory
    requiresApproval,
    map[string]any{},
    nil,       // source ← 传 nil！
)
```

**修复方案**：传入 `&source` 而非 `nil`，`source := "experience_updater"`。

---

#### E-04 [严重] TeamSkillEvolutionRail `RunEvolution` 异常处理不完整

**Python 样例** (`team_skill_evolution_rail.py:667-670`):
```python
except Exception as exc:
    logger.warning("[TeamSkillEvolutionRail] run_evolution failed: %s", exc, exc_info=True)
    self._emit_background_outcome_event({"status": "failed", ...})
    self._emit_progress("failed", f"evolution analysis failed: {exc}")
```

**Go 问题** (`team_skill_evolution_rail.go:590-597`):
```go
defer func() {
    if rec := recover(); rec != nil {  // 只捕获 panic
        logger.Error(logComponent).Any("panic", rec).Msg("[TeamSkillEvolutionRail] run_evolution 全局异常捕获")
        r.emitProgress("failed", ...)
    }
}()
```

**问题描述**：Go 只捕获 panic，不捕获 error。Python 的 except 块同时做 `emit_background_outcome_event` 和 `emit_progress("failed")`，Go 只做了后者且仅在 panic 时。

**修复方案**：RunEvolution 末尾增加 error 聚合逻辑——如任一步骤返回 error，执行 `emitBackgroundOutcomeEvent` + `emitProgress("failed")`。

---

#### E-05 [严重] `emitGeneratedRecords` 中缺少 `request_id`

**Python 样例** (`skill_evolution_rail.py:744-749`):
```python
self._emit_progress(
    "approval_required",
    f"experience records for '{skill_name}' ready, awaiting approval",
    skill_name=skill_name,
    request_id=approval_request.request_id,
)
```

**Go 问题** (`skill_evolution_rail.go:1727`):
```go
r.emitProgress("approval_required", fmt.Sprintf("..."), WithSkillName(skillName))
// ← 缺少 WithRequestID
```

**修复方案**：添加 `WithRequestID(approvalRequest.RequestID)` 参数。

---

#### E-06 [严重] TeamSkillEvolutionRail `EvolutionConfig` 缺少 `max_concurrent_evolution`

**Python 样例** (`team_skill_evolution_rail.py:340-351`):
```python
"max_concurrent_evolution": self._max_concurrent_evolution,
```

**Go 问题** (`team_skill_evolution_rail.go:1005-1015`)：返回值中缺少此字段。

**修复方案**：添加 `"max_concurrent_evolution": cap(r.EvolutionRail.evolutionSem)` 或从基类字段获取。

---

#### E-07 [严重] SkillEvolutionRail 绕过了 `FinalizeStagedEvolutionRequest`

**Python 样例** (`skill_evolution_rail.py:931-940`):
```python
return await self.approval_runtime.finalize_staged_evolution_request(
    request,
    requires_approval=requires_approval,
    emit_approval_request=...,
    on_auto_approved=_on_auto_approved,
)
```

**Go 问题** (`skill_evolution_rail.go:1687-1701`)：
自行实现了 if/else 路由逻辑，完全绕过了 `FinalizeStagedEvolutionRequest`。

**问题描述**：1) `approval_runtime` 实例上的 `FinalizeStagedEvolutionRequest` 未被调用；2) 逻辑分散，后续统一逻辑变更会漏掉；3) 与 TeamSkillEvolutionRail 实现不一致。

**修复方案**：SkillEvolutionRail 的 `handleEvolutionFromSignals` 应改为调用 `r.approvalRuntime.FinalizeStagedEvolutionRequest`，与 Python 和 TeamSkillEvolutionRail 保持一致。

---

### 一般问题（9 个）

#### E-08 [一般] `SnapshotForEvolution` 中 Go 自己收集消息而非调用基类

Python 调用 `super()._snapshot_for_evolution`，Go 直接内联收集。行为等价但代码风格不同。

---

#### E-09 [一般] `detectActiveRequestSignals` 缺少对 `DetectTrajectorySignals` 的异常捕获

Python 对 trajectory 检测和 user intent 检测分别有 try/except。Go 对 `DetectUserIntent` 错误静默丢弃（`_`），对 `DetectTrajectorySignals` 无保护。

**修复方案**：对 `DetectUserIntent` 错误应记录日志而非静默丢弃。

---

#### E-10 [一般] `inferTeamSkillFromTrajectory` 多收集了 LLM 步骤文本

**Python 样例** (`team_skill_evolution_rail.py:115-130`)：只遍历 `kind == "tool"` 的步骤。
**Go 问题** (`team_skill_evolution_rail.go:1731-1751`)：额外收集了 LLM 步骤的文本，导致 `texts` 列表更长，`inferSkillFromTexts` 结果可能不同。

**修复方案**：删除 LLM 步骤的文本收集，只保留 tool 步骤。

---

#### E-11 [一般] `evolveSkillWithSharing` 中 auto_save=True 时 `generating_updates` 重复 emit

auto_save=True 路径会再次 emit `generating_updates`，造成重复。

**修复方案**：auto_save=True 路径传 `emitHostEvents=false` 或在 `evolveSkillWithSharing` 开头去掉 `generating_updates` emit。

---

#### E-13 [一般] `emitSharedRecordsApproval` 中使用 `context.Background()` 而非调用方的 ctx

**Go 问题** (`skill_evolution_rail.go:1341`):
```go
request, _ := r.manager.StageRecords(context.Background(), skillName, records, ...)
```

**修复方案**：将调用方的 ctx 传入 `StageRecords`。

---

#### E-14 [一般] `RequestUserEvolution` 中 `records` 来源差异

Python 从 `request.proposal.records` 获取 records，Go 从 `request.PendingChange.Payload` 获取。

**修复方案**：对齐 Python，从 `request.Proposal.Records` 获取 records。

---

#### E-16 [一般] `buildDuplicateCheckPrompt` 中条件判断与 Python 不一致

Python：`if (self._language or "cn") == "cn"`
Go：`if (r.language != "" && r.language != "en") || r.language == ""`

**修复方案**：改为 `if r.language == "" || r.language == "cn"` 更明确对齐 Python。

---

#### E-17 [一般] `attachEvolutionMeta` 中元数据类型不一致

`_evolution_meta` 在 `approval_events.go` 中类型断言为 `map[string]string`，但上游可能存储为 `map[string]any`，导致断言失败。

**修复方案**：统一 `_evolution_meta` 类型为 `map[string]any`，或同时尝试两种类型断言。

---

#### E-15 [一般] (重复 E-06) TeamSkillEvolutionRail `EvolutionConfig` 缺少 `max_concurrent_evolution`

已在 E-06 中列出。

---

### 提示问题（6 个）

#### E-18 [提示] `_detect_signals` 方法在 Python 中存在但 Go 未实现

可能是遗留代码，当前 `run_evolution` 使用 `detect_trajectory_signals` + `detect_user_intent`。

---

#### E-19 [提示] `_parse_messages` 静态方法未移植

Go 中消息已经是 `[]map[string]any` 格式，不需要此转换。无需移植。

---

#### E-20 [提示] `team_trajectory_store` 参数已废弃，Go 未保留

正确，无需保留废弃参数。

---

#### E-21 [提示] `generate_and_emit_experience` 方法未移植

Python 的 backward-compatible wrapper，如果 adapter 层有调用则需要移植。

---

#### E-22 [提示] `dumpTrajectoryDebug` 未被调用

仅用于手动调试，保持现状。

---

#### E-23 [提示] `isCompletedTeamTaskView` 格式化差异

Python 用 `str(result)` 而 Go 用 `fmt.Sprintf("%v", result)`，结构化数据的字符串表示可能不同。

---

## 三、RuntimePromptRail (10.6.7) — 15 个问题

### 严重问题（4 个）

#### R-01 [严重] CodeAdapter setter 顺序与 Python 不一致

**Python 样例** (`interface_code.py:866-876`):
```python
self._runtime_prompt_rail.set_language(resolved_language)
self._runtime_prompt_rail.set_force_english(self._force_english_runtime_prompt)
self._runtime_prompt_rail.set_channel(resolved_channel)
self._runtime_prompt_rail.set_model_name(self._resolve_model_name())
self._runtime_prompt_rail.set_mode(runtime_config.mode)
self._runtime_prompt_rail.set_trusted_dirs(runtime_config.trusted_dirs)
self._runtime_prompt_rail.set_runtime_paths(cwd=..., project_dir=...)
```

**Go 问题** (`code_adapter.go:571-576`):
```go
SetLanguage → SetForceEnglish → SetChannel → SetTrustedDirs → SetRuntimePaths → SetModelName → SetMode
```

**问题描述**：`trusted_dirs` 和 `runtime_paths` 在 Python 中排在 `model_name`/`mode` 之后，Go 中排在之前。setter 本身无状态赋值，顺序不影响运行结果，但与 Python 不一致造成维护困难。

**修复方案**：调整 Go setter 调用顺序对齐 Python：`SetLanguage → SetForceEnglish → SetChannel → SetModelName → SetMode → SetTrustedDirs → SetRuntimePaths`。

---

#### R-02 [严重] `SetRuntimePaths` 缺少 `project_dir` fallback

**Python 样例** (`interface_deep.py:3145-3148`):
```python
self._runtime_prompt_rail.set_runtime_paths(
    cwd=runtime_config.cwd,
    project_dir=runtime_config.project_dir or self._project_dir,
)
```

**Go 问题** (`deep_adapter_config.go:121`):
```go
d.runtimePromptRail.SetRuntimePaths(config.CWD, config.ProjectDir)
```

**问题描述**：Python 中 `project_dir` 参数有 `or self._project_dir` 的 fallback，Go 缺少。CodeAdapter 同样缺少（`code_adapter.go:575`）。

**修复方案**：
```go
projectDir := config.ProjectDir
if projectDir == "" {
    projectDir = d.projectDir
}
d.runtimePromptRail.SetRuntimePaths(config.CWD, projectDir)
```

---

#### R-03 [严重] `updatePromptForMode` 调用位置错误

**Python 样例** (`interface_code.py:898-911`):
```
_update_rails_for_mode → _update_tools_for_mode → _update_session_tools → _refresh_acp_runtime_tools → _update_prompt_for_mode  # 最后
```

**Go 问题** (`code_adapter.go:580-586`):
```
updatePromptForMode → updateRailsForMode  # updatePromptForMode 在前！
```

**问题描述**：Go 中 `updatePromptForMode` 在 `updateRailsForMode` 之前调用，Python 中在最后。某些 rail（如 AgentModeRail）可能依赖 prompt 语言的旧值。

**修复方案**：将 `updatePromptForMode` 移到 `updateRailsForMode` 之后。

---

#### R-04 [严重] `injectTimeSection` 双语 content 与 Python 不一致

**Python 样例** (`runtime_prompt_rail.py:142-146`):
```python
self.system_prompt_builder.add_section(PromptSection(
    name="time",
    content={"cn": time_content, "en": time_content},  # 两个 key 都用同一值
    priority=92,
))
```

**Go 问题** (`runtime_prompt_rail.go:299-303`):
```go
builder.AddSection(saprompt.PromptSection{
    Name:     "time",
    Content:  map[string]string{"cn": timeContentCN, "en": timeContentEN},  // cn=中文, en=英文
    Priority: sectionTimePriority,
})
```

**问题描述**：Python 在 `language="cn"` 时，`content={"cn": time_content, "en": time_content}` 两个 key 都指向中文内容。Go 在 `language="cn"` 时，cn key 指向中文，en key 指向英文。后续如果 builder language 切换到 "en"，Python 仍显示中文，Go 会显示英文——行为差异。

**修复方案**：对齐 Python，在 `language="cn"` 分支中将 `en` key 也设为中文内容：
```go
Content: map[string]string{"cn": timeContentCN, "en": timeContentCN},
```

---

### 一般问题（7 个）

#### R-05 [一般] RuntimePromptRail 缺少 `Uninit` 生命周期清理

Python `uninit` 清理 7 个注入的 section（time/runtime/language_output/env/git_status/browser_tool_policy/trusted_dirs_policy），Go 没有对应清理逻辑。如果 RuntimePromptRail 被动态卸载，注入的 section 会残留。

**修复方案**：实现 `Uninit` 方法，在 rail 卸载时调用 builder 的 `RemoveSection` 清理所有注入的 section。

---

#### R-06 [一般] `NewRuntimePromptRail` 缺少 `timezone_offset` 参数

Python 构造函数接受 `timezone_offset: int = 8`，Go 不接受。当前不影响功能，但接口不一致。

---

#### R-07 [一般] `existingDirs`/`existingDir` 缺少 `filepath.Abs` + expanduser

Python 使用 `os.path.abspath(os.path.expanduser(...))`，Go 只用 `filepath.Clean()`。`filepath.Clean` 不做绝对路径转换也不展开 `~`。

**修复方案**：使用 `filepath.Abs()` 替代 `filepath.Clean()`，添加 `expandUser` 辅助函数。

---

#### R-08 [一般] CodeAdapter 缺少 `_update_tools_for_mode` / `_update_session_tools` / `_refresh_acp_runtime_tools`（已标记 ⤵️）

已标记为待回填，确认正确。

---

#### R-09 [一般] DeepAdapter 缺少上述三步且未标记 ⤵️

**修复方案**：补充 ⤵️ 标记。

---

#### R-10 [一般] `writeRuntimeStateYAML` 调用顺序与 Python 不一致

Python 先设 setter 再写 YAML，Go 先写 YAML 再设 setter。

**修复方案**：将 `writeRuntimeStateYAML` 调用移到 RuntimePromptRail setter 之后。

---

#### R-11 [一般] `writeRuntimeStateYAML` 缺少 `project_dir` fallback chain

**Python 样例** (`interface_code.py:881-884`):
```python
project_dir=runtime_config.project_dir or runtime_config.cwd or self._project_dir or self._workspace_dir,
```

**Go 问题**：只传了 `config.ProjectDir`，缺少完整 fallback chain。

**修复方案**：
```go
projectDir := firstNonEmpty(config.ProjectDir, config.CWD, c.deep.projectDir, c.deep.workspaceDir)
```

---

### 提示问题（4 个）

#### R-14 [提示] `_existing_dir` None vs 空字符串语义差异

Go 用空字符串代替 None，惯用做法，无需修改。

---

#### R-15 [提示] platform 字段格式差异（合理）

Go 用 `runtime.GOOS + runtime.GOARCH`，Python 用 `sys.platform + platform.release()`。平台差异合理。

---

#### R-17 [提示] `readRuntimeStateYAML` 多次调用

Go 在 `BeforeModelCall` 中 3 次读取同一文件，Python 只读取一次。建议优化为读取一次传递给各 inject 方法。

---

#### R-18 [提示] `SystemPromptBuilderInterface` 缺少 `GetAllSections`

Python 有 `get_all_sections()` 方法，Go 接口缺少。当前 RuntimePromptRail 不需要，无需修改。

---

## 四、ProjectMemoryRail (10.6.7) — 14 个问题

### 严重问题（3 个）

#### P-01 [严重] `buildProjectMemoryRail` 缺少 instance_overrides / config_cache 读取额外目录

**Python 样例** (`interface_code.py:553-558`):
```python
raw_additional_dirs = self._instance_overrides.get(
    "project_memory_additional_directories",
    self._config_cache.get("project_memory", {}).get("additional_directories"),
)
if raw_additional_dirs is None:
    raw_additional_dirs = os.getenv("JIUWENSWARM_ADDITIONAL_DIRECTORIES", "")
```

**Go 问题** (`code_adapter.go:1053-1064`)：只从环境变量读取，完全跳过了 Python 的优先级链：instance_overrides → config_cache → 环境变量。

**修复方案**：在 `buildProjectMemoryRail` 中实现完整的优先级链读取，需新增 `toStringList(v any) []string` 辅助函数处理 string/list/tuple/set 类型。

---

#### P-02 [严重] `buildProjectMemoryRail` 缺少 try/except 错误保护

**Python 样例** (`interface_code.py:550,588-592`):
```python
try:
    # ... build rail ...
    return rail
except Exception as exc:
    logger.warning("[JiuwenClawCodeAdapter] ProjectMemoryRail create failed: %s", exc)
    return None
```

**Go 问题**：整个函数没有 recover/try-catch，任何 panic 会导致整个 createInstance 崩溃。

**修复方案**：在 `buildProjectMemoryRail` 中加 `defer func() { if r := recover(); r != nil { ... } }()`。

---

#### P-03 [严重] `SetAdditionalDirectories` 使用 `filepath.Abs` 而非 `EvalSymlinks` 去重

**Python 样例** (`project_memory_rail.py:136-141`):
```python
base_resolved = {os.path.realpath(d) for d in self._additional_directories}
```
Python 使用 `os.path.realpath()`（解析符号链接后返回真实路径）做去重。

**Go 问题** (`project_memory_rail.go:183-184`):
```go
if resolved, err := filepath.Abs(d); err == nil {  // 不解析符号链接
    baseResolved[resolved] = struct{}{}
}
```

**问题描述**：如果 `/a` 是 `/b` 的符号链接，Python 会认为它们相同而 Go 不会，导致同一目录被重复扫描。

**修复方案**：使用 `filepath.EvalSymlinks` + `filepath.Abs` 组合（与 `safeResolve` 一致）。

---

### 一般问题（7 个）

#### P-04 [一般] `updateRailsForMode` 缺少 ProjectMemoryRail 补充注册逻辑

Python CodeAdapter 在 code 模式下检查 ProjectMemoryRail 是否缺失，缺失则补充注册。Go 缺少此逻辑。

**修复方案**：CodeAdapter 应覆写 `updateRailsForMode` 或在 `updateAgentModeRails` 中增加 ProjectMemoryRail 的补充注册逻辑。

---

#### P-05 [一般] `normalizeAdditionalDirectories` 返回 `string` 而非 `[]string`

Python 返回 `tuple[str, ...]`，Go 用 `os.PathListSeparator` 拼接为 string。路径包含分隔符时可能产生歧义。

**修复方案**：改为返回 `[]string`，`cacheKey.additionalDirs` 用排序后的 joined string 做比较键。

---

#### P-06 [一般] `DiscoverAndLoadMemoryFiles` 缓存比对存在 TOCTOU

Python 在 `_CACHE_LOCK` 内完成缓存读取和比对，Go 在 RLock 释放后再构建快照比对，存在 Time-of-Check-Time-of-Use 问题。

**修复方案**：将快照比对放在同一把锁内（注意 `buildWatchSnapshot` 内含 IO 操作，可能影响性能，但 Python 同样在锁内做）。

---

#### P-07 [一般] `fnmatchMatch` 不支持完整 fnmatch 语法

Go 的 `filepath.Match` 只支持 `*`/`?`/`[range]`，Python `fnmatchcase` 支持更丰富的模式。代码注释已标注此差异。

**修复方案**：记录为已知限制，如需完整对齐可用第三方 fnmatch 库。

---

#### P-08 [一般] `ClearProjectMemoryCache` 用空字符串代替 Python 的 None

Go 惯用做法，功能等价。建议在函数注释中明确空字符串=清除全部。

---

#### P-09 [一般] `BeforeModelCall` 缺少 recover 异常保护

Python 用 try/except 捕获 `discover_and_load_memory_files` 的异常并降级为空列表。Go 没有 recover，未预期 panic 会导致 model call 失败。

**修复方案**：在 `BeforeModelCall` 中加 `defer func() { if rec := recover(); rec != nil { ... err = nil } }()`。

---

#### P-10 [一般] `BuildProjectMemorySection` 移除了 language 参数

Python 保留 `language` 参数用于 API 兼容性（`del language` 不使用），Go 直接移除。功能正确但 API 不一致。

---

### 提示问题（4 个）

#### P-11 [提示] `watchSnapshot` 使用 map 而非有序 tuple

Python 的 watch_snapshot 是有序 tuple 可直接 `==` 比较，Go 使用 map 遍历比较。功能等价但实现差异。

---

#### P-12 [提示] projectRootMarkers `.jiuwen` → `.uapclaw` 重命名

品牌重命名，属设计决策。如需保持与旧版 Python 的兼容性，可同时包含 `.jiuwen` 和 `.uapclaw`。

---

#### P-13 [提示] server 层缺少 `clear_project_memory_cache` 调用

Python 的 `agent_ws_server.py` 在 workspace 切换/初始化场景中主动调用 `clear_project_memory_cache`。Go 的 server 层缺少此调用，可能导致 workspace 切换后缓存未及时失效。

---

#### P-14 [提示] `safeResolveDir` 死代码

`project_memory_rail.go:319-333` 中定义但未在项目中被调用。

---

## 修复优先级建议

### P0 — 必须立即修复（功能完全失效或严重数据丢失风险）

| 编号 | 章节 | 问题 | 影响 |
|------|------|------|------|
| W-06 | 9.66a | WorktreeRail.Init 中 context 注入失败 | worktree session 管理完全失效 |
| W-15 | 9.66a | 工具未注册到 agent ability_manager | agent 看不到 enter/exit_worktree |
| W-05 | 9.66a | gitEnv() 环境变量重复 | 可能导致 git 命令行为异常 |

### P1 — 应尽快修复（功能偏差或重要逻辑缺失）

| 编号 | 章节 | 问题 | 影响 |
|------|------|------|------|
| E-07 | 9.24 | SkillEvolutionRail 绕过 FinalizeStagedEvolutionRequest | 审批逻辑分散，与 Team 版不一致 |
| E-01 | 9.24 | disabledSkills 类型不一致 | 过滤逻辑可能不一致 |
| E-04 | 9.24 | RunEvolution 异常处理不完整 | 失败时缺少 emitBackgroundOutcomeEvent |
| R-02 | 10.6.7 | SetRuntimePaths 缺少 project_dir fallback | project_dir 为空时行为不一致 |
| R-03 | 10.6.7 | updatePromptForMode 位置错误 | 可能影响 rail 模式切换逻辑 |
| R-04 | 10.6.7 | injectTimeSection 双语 content 不一致 | 语言切换时行为偏差 |
| P-01 | 10.6.7 | buildProjectMemoryRail 缺少额外目录读取 | config.yaml 配置被忽略 |
| P-02 | 10.6.7 | buildProjectMemoryRail 缺少错误保护 | panic 导致实例创建失败 |
| W-01 | 9.66a | fireRail 空实现 | lifecycle hooks 无法触发 |
| W-04 | 9.66a | copyFile 不保留权限 | 可执行文件权限丢失 |

### P2 — 后续修复（一致性或防御性改进）

其余一般问题均归此类，建议按模块分批修复。

---

## 待回填代码确认

以下标记为 ⤵️ 的代码经确认确实尚未实现（非遗漏）：

| 位置 | 标记 | 状态 |
|------|------|------|
| `code_adapter.go` | ⤵️ `_update_tools_for_mode` | 未实现，待后续章节 |
| `code_adapter.go` | ⤵️ `_update_session_tools` | 未实现，待后续章节 |
| `code_adapter.go` | ⤵️ `setCodingMemoryDirectory` | 未实现 |
| `code_adapter.go` | ⤵️ LspRail 尚未实现 | 未实现，待 10.6.3-10 |
| `code_adapter.go` | ⤵️ ConfirmInterruptRail | 未实现 |
| `deep_adapter_team.go` | ⤵️ Rail 实例化/注入 | 部分实现，TeamSkillEvolutionRail 已注入 |
| `deep_adapter_a2x.go` | ⤵️ A2X 客户端 | 全部未实现，待 11.10 |
| `deep_adapter_team.go` | ⤵️ processTeamMessageStream | 未实现，待 10.3.7-11 |
| `deep_adapter_config.go` | ⤵️ browser_agent | 未实现，待 browser 功能 |

⚠️ **注意**：`deep_adapter_config.go` 中缺少 `_update_tools_for_mode` / `_update_session_tools` / `_refresh_acp_runtime_tools` 的 ⤵️ 标记（R-09），CodeAdapter 中已有标记。建议在 DeepAdapter 中补充标记。
