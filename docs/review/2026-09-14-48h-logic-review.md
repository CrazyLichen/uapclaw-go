# 48 小时逻辑审查 — 2026-09-14

> 审查范围：最近 48 小时内提交的代码，覆盖实现计划中 9.66a / 9.24 P3-P4 / 9.80a / 7.10 / 10.6.6-10.6.7 / Memory any 收紧 章节。
> 对比 Python 参考项目：`/home/opensource/agent-core/openjiuwen/`（agentcore）+ `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`（swarm）

---

## 审查章节概览

| 章节 | Python 参考 | Go 代码 | 严重 | 一般 | 提示 |
|------|------------|---------|------|------|------|
| 9.66a WorktreeManager | `openjiuwen/harness/tools/worktree/` | `internal/agentcore/harness/tools/worktree/` | 6 | 5 | 4 |
| 9.24 P3/P4 SkillEvolutionRail | `openjiuwen/harness/rails/evolution/` | `internal/agentcore/harness/rails/evolution/` | 3 | 8 | 2 |
| 9.80a ExperienceSharing | `openjiuwen/agent_evolving/sharing/` | `internal/evolving/sharing/` | 1 | 5 | 4 |
| 10.6.6-7 ProjectMemoryRail | `jiuwenswarm/.../rails/project_memory_rail.py` | `internal/swarm/.../rails/project_memory_rail.go` | 1 | 1 | 1 |
| 10.6.7 RuntimePromptRail | `jiuwenswarm/.../rails/runtime_prompt_rail.py` | `internal/swarm/.../rails/runtime_prompt_rail.go` | 2 | 1 | 2 |
| 10.6.3-10 待实现 Rails | `jiuwenswarm/.../rails/` | `internal/swarm/server/adapter/deep_adapter_rails.go` | 3 | 3 | 0 |
| 7.10 Memory Index | `openjiuwen/core/memory/manage/index/` | `internal/agentcore/memory/manage/index/` | 0 | 4 | 4 |
| Memory any 收紧 | — | `internal/agentcore/memory/` | 2 | 1 | 3 |
| **合计** | | | **18** | **28** | **20** |

---

## 一、9.66a WorktreeManager

### S-01 🔴 严重：`WorktreeLifecycleRail` 接口缺少 4 个 hook 方法

**Python 样例：**
```python
# rails.py L310-391
class WorktreeLifecycleRail:
    async def on_worktree_file_write(self, ctx, session, file_path) -> bool:
        return True
    async def before_worktree_commit(self, ctx, session, message, files) -> str | None:
        return None
    async def after_worktree_commit(self, ctx, session, commit_sha) -> None:
        pass
    async def on_worktree_sync(self, ctx, session, direction, files) -> list[str]:
        return files
```

**Go 问题代码：**
```go
// backend.go L32-41
type WorktreeLifecycleRail interface {
    BeforeWorktreeCreate(ctx context.Context, slug, repoRoot string) (string, error)
    AfterWorktreeCreate(ctx context.Context, session *WorktreeSession) error
    BeforeWorktreeExit(ctx context.Context, session *WorktreeSession, action string) (string, error)
    AfterWorktreeExit(ctx context.Context, session *WorktreeSession, action string) error
}
```

**问题分析：** Python 定义了 8 个 hook 方法，Go 只实现了 4 个。缺少 `OnWorktreeFileWrite`、`BeforeWorktreeCommit`、`AfterWorktreeCommit`、`OnWorktreeSync`。这些 hook 在文件写入、提交、同步等操作时被调用，缺失意味着这些生命周期事件无法被拦截或增强。

**修复方案：** 在 `WorktreeLifecycleRail` 接口中添加这 4 个方法，并给 `AutoSetupRail` 和 `DiffSummaryRail` 添加对应的空实现。

---

### S-02 🔴 严重：`fireRail` 是空实现，`lifecycleRails` 从未被调用

**Python 样例：**
```python
# manager.py L694-714
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

**Go 问题代码：**
```go
// manager.go L443-447
func (m *WorktreeManager) fireRail(method string, args ...any) any {
    // 当前实现只支持 BeforeWorktreeCreate 和 BeforeWorktreeExit
    // 完整的 rail 调度在 rails.go 的 WorktreeRail 中处理
    return nil
}
```

**问题分析：** Python 的 `_fire_rail` 是 Manager 与 Rail 的核心桥梁，在 `Enter`/`Exit` 等关键位置被调用。Go 版本硬编码返回 nil，`lifecycleRails` 列表虽已初始化但从未被遍历。`Enter`/`Exit` 方法中没有调用 `fireRail`。

**影响：** `AutoSetupRail.AfterWorktreeCreate` 和 `DiffSummaryRail.BeforeWorktreeExit` 实现了接口但永远不会被触发，lifecycle hook 机制完全失效。

**修复方案：** 实现 `fireRail`：遍历 `m.lifecycleRails`，通过方法名映射调用对应接口方法。在 `Enter` 创建成功后调用 `AfterWorktreeCreate`，在 `Exit` 移除前调用 `BeforeWorktreeExit`，与 Python 对齐。

---

### S-03 🔴 严重：`WorktreeRail.Init` 中 `WithWorktreeSessionState` 返回的 context 被丢弃

**Python 样例：**
```python
# rails.py L127
init_session_state()  # ContextVar 设置，后续所有 async 任务共享
```

**Go 问题代码：**
```go
// rails.go L131-132
state := InitWorktreeSessionState()
_ = WithWorktreeSessionState(ctx, state)  // BUG: 返回的新 ctx 被丢弃
```

**问题分析：** `WithWorktreeSessionState` 返回携带 `WorktreeSessionState` 的新 context，但赋给 `_` 丢弃了。后续工具调用使用原始 ctx，无法通过 `ctx` 访问 `WorktreeSessionState`，导致 `GetCurrentSession`/`SetCurrentSession` 等基于 context 的操作失效。

**影响：** worktree session 状态管理完全失效——进入/退出 worktree 后无法在后续调用中获取当前 session。

**修复方案：** 必须将 `WithWorktreeSessionState` 返回的 context 传播到 agent 的上下文机制中，确保后续 `Enter`/`Exit` 等方法能通过 ctx 访问 session state。

---

### S-04 🔴 严重：`WorktreeRail.Init` 缺少工具注册到 Agent 的逻辑

**Python 样例：**
```python
# rails.py L134-136
def init(self, agent):
    Runner.resource_mgr.add_tool(self._tools)
    for tool in self._tools:
        agent.ability_manager.add(tool.card)
```

**Go 问题代码：**
```go
// rails.go L145
r.tools = []tool.Tool{enterTool, exitTool}
// 缺少: agent.ability_manager.add / resource_mgr.add_tool 等价逻辑
```

**问题分析：** Python 的 `init()` 将工具注册到 `Runner.resource_mgr` 和 `agent.ability_manager`。Go 只创建了工具对象放入内部切片，没有注册到任何 agent 管理器，LLM 无法发现和调用 `enter_worktree`/`exit_worktree` 工具。

**影响：** worktree 工具对 LLM 不可见，Agent 无法通过工具调用来进入/退出 worktree。

**修复方案：** 在 `Init` 中将工具注册到 agent 的 ability manager 和对应的 resource manager，对齐 Python 的注册逻辑。

---

### S-05 🔴 严重：`WorktreeRail.Uninit` 缺少从 Agent 注销工具的逻辑

**Python 样例：**
```python
# rails.py L138-148
def uninit(self, agent):
    for tool in self._tools:
        name = getattr(tool.card, "name", None)
        if name and hasattr(agent, "ability_manager"):
            agent.ability_manager.remove(name)
        tool_id = getattr(tool.card, "id", None)
        if tool_id:
            Runner.resource_mgr.remove_tool(tool_id)
```

**Go 问题代码：**
```go
// rails.go L153-157
func (r *WorktreeRail) Uninit(agent interfaces.BaseAgent) error {
    r.tools = nil
    r.manager = nil
    return nil
}
```

**问题分析：** Python 的 `uninit()` 从 `agent.ability_manager` 移除工具并从 `Runner.resource_mgr` 注销。Go 只清空内部引用，agent 侧仍保留已失效的工具注册，可能导致 LLM 尝试调用已销毁的工具。

**修复方案：** 在 `Uninit` 中遍历 `r.tools`，从 agent 的 ability manager 和 resource manager 注销工具。

---

### S-06 🔴 严重：`simpleMatch` 不是 `fnmatch` 等价实现

**Python 样例：**
```python
import fnmatch
if any(fnmatch.fnmatch(entry, p) for p in patterns):
```

**Go 问题代码：**
```go
// manager.go L560-569
func simpleMatch(name, pattern string) bool {
    if pattern == "*" { return true }
    if strings.Contains(pattern, "*") {
        prefix := strings.TrimSuffix(pattern, "*")
        return strings.HasPrefix(name, prefix)
    }
    return name == pattern
}
```

**问题分析：** Python `fnmatch.fnmatch()` 支持 `*`、`?`、`[seq]`、`[!seq]` 等 glob 模式，`*` 可出现在任意位置。Go 的 `simpleMatch` 只实现了 `*` 作为尾部前缀通配符和精确匹配。不支持中间通配符（如 `config/*.env`）、`?`、`[seq]` 等。

**修复方案：** 使用 Go 标准库 `path.Match()` 或 `filepath.Match()` 替换 `simpleMatch`，支持 `*`、`?`、`[seq]`。

---

### M-01 🟡 一般：`resolveOwner` 始终返回空字符串，owner 信息丢失

**Python 样例：**
```python
# tools.py L64-72
def _resolve_owner(kwargs):
    owner_id = kwargs.get("owner_id") or kwargs.get("member_name")
    tag = kwargs.get("tag") or kwargs.get("team_name")
    return owner_id, tag
```

**Go 问题代码：**
```go
// tools.go L286-290
func resolveOwner(opts []tool.ToolOption) (string, string) {
    return "", ""
}
```

**问题分析：** Python 从调用方 kwargs 提取 `owner_id`/`tag`，Go 硬编码返回空。团队模式下 owner 信息用于事件传播和工作空间管理，缺失会导致事件中无法追溯 worktree 归属。

**修复方案：** 从 `tool.ToolOption` 或 context 中提取 owner 信息，或增加携带 `owner_id`/`tag` 的 `ToolOption`。

---

### M-02 🟡 一般：`WorktreeRail.Init` 硬编码 `lang="cn"` 和 `agentID=""`

**Python 样例：**
```python
# rails.py L115-116
lang = agent.system_prompt_builder.language
agent_id = getattr(getattr(agent, "card", None), "id", None)
```

**Go 问题代码：**
```go
// rails.go L120-121
lang := "cn"
agentID := ""
```

**问题分析：** Python 从 agent 属性动态获取语言和 ID，Go 硬编码。非中文环境下工具描述会不正确。

**修复方案：** 从 `interfaces.BaseAgent` 获取 language 和 agentID。

---

### M-03 🟡 一般：`copyFile` 不保留文件元数据，不等价于 Python 的 `shutil.copy2`

**Python 样例：**
```python
# manager.py L458
shutil.copy2(src, dst)  # 保留权限位、atime、mtime
```

**Go 问题代码：**
```go
// manager.go L572-578
func copyFile(src, dst string) error {
    data, err := os.ReadFile(src)
    if err != nil { return err }
    return os.WriteFile(dst, data, 0o644)  // 权限固定 0o644，不保留原始权限和 mtime
}
```

**修复方案：** 使用 `os.Stat(src)` 获取权限后 `os.WriteFile(dst, data, mode)`，并调用 `os.Chtimes` 保留 mtime/atime。

---

### M-04 🟡 一般：`WorktreeBackend.Remove` 返回 `bool` 而非 `error`

**Go 问题代码：**
```go
// backend.go L51
Remove(ctx context.Context, worktreePath, repoRoot string) bool
```

**问题分析：** Python 的 `worktree_remove` 可以通过 `GitError` 抛出异常传递失败原因。Go 返回 `bool` 无法区分 "worktree 不存在" 和 "git 命令失败"。

**修复方案：** 将 `Remove` 返回类型改为 `error`，所有调用方相应调整。

---

### M-05 🟡 一般：`NewWorktreeConfig` 未显式设置 `Enabled: false`

**修复方案：** 显式设置 `Enabled: false` 保持与 Python 对齐和代码清晰。

---

### T-01 💡 提示：`os.Symlink` 在 Windows 上缺少 `target_is_directory` 等价

**修复方案：** 添加注释说明差异，Linux 环境下无影响。

---

### T-02 💡 提示：`EnterWorktreeTool.Invoke` 返回格式与 Python `ToolOutput` 不对齐

**修复方案：** 成功返回时加 `"success": true` 字段与 Python 的 `ToolOutput.success` 对齐。

---

### T-03 💡 提示：`CleanupStaleWorktrees` 中 mtime 比较使用 `time.Now()` 而非 UTC

**Python 样例：** `datetime.now(tz=timezone.utc)`

**修复方案：** 改为 `time.Now().UTC()` 确保跨时区一致性。

---

### T-04 💡 提示：`CleanupStaleWorktrees` 未使用并行安全检查

**Python 样例：** `asyncio.gather(status_porcelain, has_unpushed_commits)`

**修复方案：** 可用 goroutine + `errgroup` 并行执行，性能优化建议。

---

## 二、9.24 P3/P4 SkillEvolutionRail

### S-07 🔴 严重：`handleEvolutionFromSignals` 绕过 `finalize_staged_evolution_request`，审批快照不注册

**Python 样例：**
```python
# skill_evolution_rail.py L931-940
return await self.approval_runtime.finalize_staged_evolution_request(
    request,
    requires_approval=requires_approval,
    emit_approval_request=(
        (lambda staged_request: self._emit_generated_records(ctx, skill_name, staged_request))
        if emit_host_events
        else (lambda staged_request: None)
    ),
    on_auto_approved=_on_auto_approved,
)
```

**Go 问题代码：**
```go
// skill_evolution_rail.go L1686-1701
if !requiresApproval {
    // ...
    r.sharingAfterAutoApproved(ctx, skillName, request)
    return request, nil
}
if emitHostEvents {
    r.emitGeneratedRecords(cbc, skillName, request)
}
return request, nil
```

**问题分析：** Python 将审批逻辑完全委托给 `approval_runtime.finalize_staged_evolution_request()`，该方法内部处理快照注册、emit_approval_request 回调和 on_auto_approved 回调。Go 直接内联了分支逻辑，绕过了 `FinalizeStagedEvolutionRequest`，导致 `_pending_approval_snapshots` 不被注册。

**影响：** 后续 `ApproveRecord` 在 `_pending_approval_snapshots` 中找不到 `requestID`，审批流程可能完全失效。

**修复方案：** 将审批逻辑委托给 `approvalRuntime.FinalizeStagedEvolutionRequest()`，至少在 `requiresApproval=true` 路径调用该方法注册快照。

---

### S-08 🔴 严重：`RunEvolution` 缺少顶层异常捕获

**Python 样例：**
```python
# skill_evolution_rail.py L383-525
try:
    # ... 整个 evolution 逻辑 ...
except Exception as exc:
    logger.warning("[SkillEvolutionRail] auto evolution failed: %s", exc)
```

**Go 问题代码：** `RunEvolution` 整个方法体没有 `defer recover()` 保护。

**修复方案：** 入口添加 `defer func() { if rec := recover(); rec != nil { logger.Warn(...) } }()`。

---

### S-19 🔴 严重：`_upload_approved_records_for_sharing` 方法缺失

**Python 样例：**
```python
# skill_evolution_sharing.py L340-372
async def _upload_approved_records_for_sharing(
    self, pending, request_id,
) -> None:
    if not self.is_sharing_enabled or self._share_stager is None:
        return
    records = pending.get("records", [])
    if not records:
        return
    await self._share_stager.stage_and_upload(...)
```

**Go 问题：** Go 的 `sharingAfterAutoApproved` 仅覆盖自动审批路径，手动审批路径（`ApproveRecord`）没有调用共享上传。Python 在两种审批路径都调用 `_upload_approved_records_for_sharing`。

**影响：** 手动审批通过的进化记录不会上传到共享平台，团队间经验共享功能断裂。

**修复方案：** 在 `ApproveRecord` 成功路径中添加 `uploadApprovedRecordsForSharing` 调用，与 Python 对齐。

**流程示例：**
```
Python: 信号检测 → 暂存请求 → 审批 → FinalizeStagedEvolutionRequest → on_auto_approved → _upload_approved_records_for_sharing → HubClient
Go:    信号检测 → 暂存请求 → 审批(无 Finalize) → ❌ 手动审批路径缺少上传步骤
```

---

### M-06 🟡 一般：`GenerateAndEmitExperience` 方法缺失

**修复方案：** 确认 slash 命令处理是否直接调用 `RequestUserEvolution`。如果是，无需添加。

---

### M-16 🟡 一般：`_legacy_user_intent` 方法缺失

**Python 样例：**
```python
# skill_evolution_rail.py L776-808
@staticmethod
def _legacy_user_intent(*, user_query, signals, messages):
    if user_query:
        return user_query
    for sig in signals:
        if sig.get("signal_type") == "user_request":
            return sig.get("excerpt", "")
    return None
```

**Go 问题：** Go 版本中 `_legacy_user_intent` 缺失。Python 使用该方法作为信号检测的 fallback 机制，当 LLM 信号检测不可用时通过关键词匹配获取用户意图。

**修复方案：** 实现 `legacyUserIntent` 方法，复用 Python 的关键词匹配逻辑。

---

### M-17 🟡 一般：`buildExperienceSharer` 缺少 `backend_name` 验证

**Python 样例：**
```python
# skill_evolution_sharing.py L200-220
def _build_experience_sharer(self, config: dict) -> ExperienceSharer | None:
    backend_name = config.get("backend_name", "")
    if backend_name not in ("hub", "local"):
        logger.warning(f"Unknown sharing backend: {backend_name}")
        return None
```

**Go 问题：** Go 的 `buildExperienceSharer` 没有对 `backend_name` 进行验证，可能传入无效的 backend 名称导致运行时错误。

**修复方案：** 在 `buildExperienceSharer` 中添加 `backend_name` 的白名单验证。

---

### M-18 🟡 一般：`download_top_k` 配置项未从配置中解析

**Python 样例：**
```python
# skill_evolution_sharing.py L225
download_top_k = config.get("download_top_k", 5)
```

**Go 问题：** Go 版本中 `download_top_k` 使用硬编码值而非从配置解析，用户无法调整下载条数。

**修复方案：** 从配置中读取 `download_top_k`，默认值 5。

---

### M-19 🟡 一般：`approval_runtime`/`team_signal_detector` 每次 Init 都重建

**Python 样例：**
```python
# skill_evolution_rail.py — approval_runtime 在 __init__ 中创建一次
self.approval_runtime = ApprovalRuntime(...)
```

**Go 问题：** Go 版本在 `Init` 方法中重建 `approvalRuntime` 和 `teamSignalDetector`，如果 Rail 被多次 Init/Uninit，会导致状态丢失。

**修复方案：** 将 `approvalRuntime`/`teamSignalDetector` 的创建移到构造函数，或增加 nil 检查避免重复创建。

---

### M-20 🟡 一般：`record_llm_policy` 与 Python 的 `record_llm_usage_policy` 不同步

**Python 样例：**
```python
# 在 evolution 完成后记录 LLM 使用策略
self._record_llm_usage_policy(skill_name, policy_type, tokens_used)
```

**Go 问题：** Go 的 `recordLLMPolicy` 方法签名和行为与 Python 不完全对齐，缺少 `tokens_used` 参数和策略类型分类。

**修复方案：** 对齐 Python 的 `record_llm_usage_policy` 签名和行为。

---

### M-24 🟡 一般：`TeamSkillEvolutionRail.EvolutionConfig()` 缺少 `max_concurrent_evolution`

**Python 样例：**
```python
# team_skill_evolution_rail.py L312
@property
def evolution_config(self) -> dict[str, Any]:
    return {
        ...
        "max_concurrent_evolution": self._max_concurrent_evolution,
    }
```

**Go 问题代码：**
```go
// team_skill_evolution_rail.go
func (r *TeamSkillEvolutionRail) EvolutionConfig() map[string]any {
    return map[string]any{
        "user_request_llm_policy":      r.userRequestLLMPolicy,
        "trajectory_issue_llm_policy":  r.trajectoryIssueLLMPolicy,
        "record_llm_policy":            r.recordLLMPolicy,
        "evaluate_llm_policy":          r.evaluateLLMPolicy,
        "simplify_llm_policy":          r.simplifyLLMPolicy,
        "eval_interval":                r.evalInterval,
        "evolution_total_timeout_secs": r.evolutionTotalTimeoutSec,
        // 缺少: "max_concurrent_evolution"
    }
}
```

**问题分析：** Go 的 `EvolutionRail` 内部已有 `evolutionSem` 信号量实现并发控制，功能不缺失，但 `EvolutionConfig()` 返回的 map 未暴露该配置项，与 Python 不一致。下游依赖该配置值的代码可能取到零值。

**修复方案：** 在 `EvolutionConfig()` 返回 map 中添加 `"max_concurrent_evolution": r.maxConcurrentEvolution`。同时在 `TeamSkillEvolutionRail` 结构体中添加该字段。

---

### T-05 💡 提示：`inferSkillFromTexts` 注释"暂未实现"已过时

**修复方案：** 删除过时注释。

---

### T-16 💡 提示：`DetectActiveRequestSignals` 缺少异常隔离

**Python 样例：**
```python
try:
    signals = await self._detect_active_request_signals(...)
except Exception as exc:
    logger.warning("Signal detection failed: %s", exc)
    signals = []
```

**Go 问题：** Go 的 `DetectActiveRequestSignals` 没有 try/except 等价的 recover 保护，信号检测失败可能导致整个 evolution 流程中断。

**修复方案：** 在调用方增加 recover 保护或 error 容错处理。

---

## 三、9.80a ExperienceSharing

### S-09 🔴 严重：`HasSkillPackage` 缺少 error 返回值

**Python 样例：**
```python
# experience_sharer.py — has_skill_package 可能抛出异常
def has_skill_package(self, skill_name: str) -> bool:
    try:
        return self._check_skill_package(skill_name)
    except Exception:
        raise  # 调用方可捕获
```

**Go 问题代码：**
```go
// interface.go L25
HasSkillPackage(ctx context.Context, skillID string) bool
```

**问题分析：** Python 的 `has_skill_package` 在检查过程出错时抛异常，调用方可以区分"包不存在"和"检查过程出错"。Go 只返回 `bool`，调用方无法获知检查是否异常，错误被静默吞掉。

**影响：** 当 skill package 检查过程出错时（如网络问题、文件 I/O 错误），调用方无法感知，可能误判为"包不存在"而触发不必要的下载。

**修复方案：** 将 `HasSkillPackage` 返回类型改为 `(bool, error)`，所有实现方（`LocalFileBackend`、`fakeHubBackend`、`mockBackend`）和调用方相应调整。

---

### M-07 🟡 一般：`ExperienceSharer` 中 `defer recover()` 冗余

**Go 问题代码：**
```go
// experience_sharer.go L369-376
func (es *ExperienceSharer) DownloadSkillPackage(...) []byte {
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).
                Str("skill_id", resolvedID).
                Any("panic", r).
                Msg("[ExperienceSharer] download_skill_package failed")
        }
    }()
    data, err := es.backend.DownloadSkillPackage(ctx, resolvedID)
    if err != nil {  // 已有 error 返回检查
        logger.Warn(logComponent).Err(err).Msg(...)
        return nil
    }
    return data
}
```

**问题分析：** Python 在关键位置用 try/except 捕获异常，Go 已通过 error 返回值处理了大部分错误路径。额外的 `defer recover()` 是冗余的——`DownloadSkillPackage` 和 `GetSkillPackageMeta` 的 backend 调用都返回 error，不存在 panic 路径。

**修复方案：** 移除 `DownloadSkillPackage` 和 `GetSkillPackageMeta` 中的冗余 `defer recover()` 块。如果需要防御性保护，应仅在可能产生 panic 的边界（如类型断言、map 并发访问）保留。

---

### M-21 🟡 一般：`ListCachedBundles` 缺少排序

**Python 样例：**
```python
# experience_sharer.py
def list_cached_bundles(self, skill_id) -> list[dict]:
    bundles = self._list_all_bundles(skill_id)
    bundles.sort(key=lambda b: b.get("updated_at", ""), reverse=True)
    return bundles
```

**Go 问题代码：**
```go
// experience_sharer.go L425-462
func (es *ExperienceSharer) ListCachedBundles(skillID string) []SharedSkillBundle {
    // ... 遍历目录读取 bundle ...
    return bundles  // 缺少排序
}
```

**问题分析：** Python 对缓存 bundle 列表按 `updated_at` 降序排序，Go 缺少此排序。下游逻辑（如选择最新 bundle）可能获取到过时的数据。

**修复方案：** 在返回前按 `SharedSkillBundle.UpdatedAt` 降序排序：`sort.Slice(bundles, func(i, j int) bool { return bundles[i].UpdatedAt > bundles[j].UpdatedAt })`。

---

### M-22 🟡 一般：`HubClient.InstallSkill` 返回 error 而 Python 静默返回 None

**Python 样例：**
```python
# hub_client.py
async def install_skill(self, skill_id, skill_name=None):
    try:
        await self._do_install(skill_id, skill_name)
    except Exception:
        pass  # 静默忽略安装失败
```

**Go 问题代码：**
```go
// hub_client.go L76-108
func (c *ExperienceHubClient) InstallSkill(ctx context.Context, skillID string, skillName string) (string, error) {
    // 空包、空 ID 等场景返回 error
}
```

**问题分析：** Python 的 `install_skill` 安装失败时静默返回 None，Go 返回 error。行为差异不大，但需确认调用方是否正确处理了 error（不应因安装失败中断主流程）。

**修复方案：** 调用方在非关键路径应忽略 `InstallSkill` 返回的 error，与 Python 的静默行为对齐。

---

### M-25 🟡 一般：`MatchFailureKeywords` 缺少 `error = None` 排除逻辑

**Python 样例：**
```python
# signal/from_conv.py
_FAILURE_KEYWORDS = re.compile(
    r"error(?!\s*=\s*None)|exception|traceback|failed|failure|timeout|timed out"
    r"|errno|connectionerror|oserror|valueerror|typeerror"
    r"|错误|异常|失败|超时"
    r"|no such file|permission denied|access denied"
    r"|command not found|not recognized",
    re.IGNORECASE,
)
# 负向前瞻 (?!\s*=\s*None) 仅对 "error" 生效
```

**Go 问题代码：**
```go
// signal/from_conv.go
var failureKeywords = regexp.MustCompile(
    `(?i)error|exception|traceback|failed|failure|timeout|timed out` +
        `|errno|connectionerror|oserror|valueerror|typeerror` +
        `|错误|异常|失败|超时` +
        `|no such file|permission denied|access denied` +
        `|command not found|not recognized`,
)
// MatchFailureKeywords 直接调用 failureKeywords.MatchString(content)
// findFailureKeywordIndex 则有 error=None 排除逻辑
```

**问题分析：** Go 不支持 `(?!\s*=\s*None)` 负向前瞻语法，在 `findFailureKeywordIndex` 中通过后置检查实现了等价逻辑。但导出的 `MatchFailureKeywords`（被 `ShareStager` 使用）直接调用 `failureKeywords.MatchString()`，**没有** `error = None` 排除逻辑。

**影响：** `ShareStager.messagesHasSuccessfulTool()` 中调用 `MatchFailureKeywords` 检查工具结果是否含失败关键词时，`error = None` 会被误匹配为失败，导致有效的进化记录被跳过不上传。

**修复方案：** 将 `MatchFailureKeywords` 改为使用 `findFailureKeywordIndex` 的逻辑（或新增一个 `MatchFailureKeywordsFiltered` 函数），确保 `error = None` 不被误判为失败。

---

### T-06 💡 提示：`hubPath` 环境变量读取逻辑等价，仅代码风格差异

---

### T-17 💡 提示：`keyword_extractor` 的 `skillHint` 使用 variadic `...string` 而非显式参数

**Python 样例：**
```python
# keyword_extractor.py
def extract_query_keywords(self, feedback_excerpt, skill_hint="") -> QueryKeywords:
```

**Go 问题代码：**
```go
// keyword_extractor.go L207
func (e *KeywordExtractor) ExtractQueryKeywords(ctx context.Context, feedbackExcerpt string, skillHint ...string) QueryKeywords
```

**问题分析：** Python 使用显式的 `skill_hint=""` 默认参数，Go 用 variadic。功能等价但 API 风格不一致。

**修复方案：** 可保持 variadic 风格（Go 惯用法），但需在文档中说明差异。

---

### T-18 💡 提示：`HubClient.Search` 缺少分页参数

**Python 样例：** `hub_client.py` 的 `search` 方法接受 `limit` 参数控制返回数量。

**Go 问题：** Go 版本可能缺少分页控制，但在当前使用场景下影响有限。

---

### T-19 💡 提示：`ShareStager` 的筛选阈值硬编码

**Python 样例：** Python 的 `ShareStager` 从配置读取筛选阈值。

**Go 问题：** Go 版本阈值硬编码，缺少可配置性。

**修复方案：** 后续可从配置中读取阈值。

---

## 四、10.6.6-7 ProjectMemoryRail + RuntimePromptRail

### S-10 🔴 严重：`RuntimePromptRail` 缺少 `Uninit` 方法，7 个 section 残留

**Python 样例：**
```python
# runtime_prompt_rail.py L53-63
def uninit(self, agent):
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

**Go 问题代码：** `RuntimePromptRail` 没有 `Init`/`Uninit` 方法。

**问题分析：** Rail 被替换/移除后，time/runtime/language_output/env/git_status/browser_tool_policy/trusted_dirs_policy 这 7 个 section 会残留在系统提示词中。

**修复方案：** 实现 `Uninit` 方法，依次 `RemoveSection` 7 个 section 名称。

---

### S-11 🔴 严重：`injectTimeSection`/`injectRuntimeSection` 未先 `RemoveSection`

**Python 样例：** Python 在 `before_model_call` 中对 `language_output`/`git_status`/`browser_tool_policy`/`trusted_dirs_policy` 先 `remove_section` 再 `add_section`。`time`/`runtime` section 依赖 builder 内部同名替换逻辑。

**Go 问题：** Go 的 `injectTimeSection` 和 `injectRuntimeSection` 没有先调用 `RemoveSection`。如果 `AddSection` 对同名 section 不做替换，会导致重复 section。

**修复方案：** 确认 `SystemPromptBuilder.AddSection` 对同名 section 的行为。如果不做替换，在 `injectTimeSection`/`injectRuntimeSection` 开头加 `RemoveSection`。

---

### S-12→提示 💡 `BuildProjectMemorySection` 省略 language 参数

**Python 样例：**
```python
section = build_project_memory_section(merged, language=self._language, priority=self.SECTION_PRIORITY)
```

**Go 问题代码：**
```go
section := project_memory.BuildProjectMemorySection(merged, sectionPriority)
```

**问题分析：** 经核实，Python 的 `build_project_memory_section` 实际内部 `del language` 不使用该参数（仅 API 兼容），Go 省略该参数**行为一致**。

---

### M-08 🟡 一般：`injectTimeSection` 中文模式下 en key 内容与 Python 不一致

**Python 样例：** 中文模式下 `content={"cn": time_content, "en": time_content}` 两个 key 都指向同一中文内容。

**Go 问题代码：** 中文模式下 `Content: map[string]string{"cn": timeContentCN, "en": timeContentEN}` 分放不同内容。

**修复方案：** 与 Python 对齐，中文模式下 `Content: map[string]string{"cn": timeContentCN, "en": timeContentCN}`。此问题同时影响 `injectRuntimeSection`/`injectEnvSection`/`injectTrustedDirsPolicySection`。

---

### T-07 💡 提示：`BuildProjectMemorySection` 签名省略 language 参数

Python 实际内部 `del language` 不使用，Go 省略行为一致。

---

### T-08 💡 提示：`RuntimePromptRail` 省略 `timezone_offset` 参数

Python 中该参数当前未被使用，Go 可暂不添加，记录差异以备后续。

---

### T-09 💡 提示：Windows 上 SHELL 环境变量 fallback 缺失

**修复方案：** 可在 Windows 上增加 `COMSPEC` 环境变量检测。

---

## 五、10.6.3-10 待实现 Rails（⤵️ 标记确认）

### S-13 🔴 严重：`ResponsePromptRail` 未实现（返回 nil）

**Go 问题代码：**
```go
// deep_adapter_rails.go L494-497
func (d *DeepAdapter) buildResponsePromptRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 ResponsePromptRail
    return nil
}
```

**Python 参考：** `jiuwenswarm/.../rails/response_prompt_rail.py` L12-36，包含 init/uninit/before_model_call 逻辑。

**修复方案：** 创建 `response_prompt_rail.go`，移植 Python 的 `_response_prompt` 函数（位于 `prompt_builder.py` L36-105）。

---

### S-14 🔴 严重：`JiuClawStreamEventRail` 未实现（返回 nil，~740 行）

**Go 问题代码：**
```go
// deep_adapter_rails.go L412-415
func (d *DeepAdapter) buildStreamEventRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 JiuClawStreamEventRail
    return nil
}
```

**Python 参考：** `stream_event_rail.py` L178-914，包含 pause/resume/abort、tool_call/tool_result 事件发射、todo.updated/context.usage 事件、_fix_incomplete_tool_context 等。

**修复方案：** 完整移植，这是最复杂的未实现 rail。需同时移植 `interrupt_helpers.py` 中的 `convert_interactions_to_ask_user_question` 等转换函数。

---

### S-15 🔴 严重：`MemoryRail`/`ExternalMemoryRail` 未实现（返回 nil）

**Go 问题代码：**
```go
func (d *DeepAdapter) buildMemoryRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 MemoryRail
    return nil
}
func (d *DeepAdapter) buildExternalMemoryRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 ExternalMemoryRail
    return nil
}
```

**修复方案：** 对应 Python `agents/harness/common/rails/` 下的 memory_rail 实现。注意 ProjectMemoryRail 已替代 MemoryRail 核心功能，但 Deep 模式仍使用 MemoryRail。

---

### M-09 🟡 一般：ACP 通道权限确认降级为 interrupt（⤵️ 待回填）

**Go 问题代码：**
```go
// deep_adapter_rails.go L886-913
if channelID != "acp" {
    return &harnesssecurity.PermissionConfirmResponse{Action: harnesssecurity.ConfirmActionInterrupt}, nil
}
// ⤵️ ACP: ACP output manager 尚未实现，降级为 ConfirmActionInterrupt
return &harnesssecurity.PermissionConfirmResponse{Action: harnesssecurity.ConfirmActionInterrupt}, nil
```

**修复方案：** 等 ACP output manager 实现后回填完整逻辑。

---

### M-10 🟡 一般：Rail 模式切换缺少 MemoryRail 处理（⤵️ 待回填）

**Go 问题代码：**
```go
// ⤵️ 待回填: handleMemoryRailByConfig
// ⤵️ 待回填: handleExternalMemoryRailByConfig
```

**修复方案：** 实现 handleMemoryRailByConfig/handleExternalMemoryRailByConfig，对齐 Python 的模式切换逻辑。

---

### M-11 🟡 一般：`LspRail` 未实现（Code 模式专有，返回 nil）

**修复方案：** 移植 LspRail，参考 Python 的 LSP 相关代码。

---

## 六、7.10 Memory Index

### M-12 🟡 一般：`VariableManager.AddMemories` 返回 nil 切片而非空切片

**Python 样例：** `return []`（空列表，JSON 序列化为 `[]`）

**Go 问题代码：** `return nil, nil`（nil 切片，JSON 序列化为 `null`）

**修复方案：** 返回 `[]mem_model.MemoryUnit{}` 空切片，确保 JSON 序列化与 Python 对齐。

---

### M-13 🟡 一般：`AddMemories` 缺少顶层 try/except BaseError 传播模式

**Python 样例：**
```python
try:
    # 整个方法体
except BaseError:
    raise
except Exception as e:
    self._wrap_exception(e, StatusCode.MEMORY_ADD_MEMORY_EXECUTION_ERROR, self.mem_type)
```

**Go 问题：** Go 有 `wrapException` 但仅在内部调用点使用，不如 Python 的 try/except 模式防御性强。

**修复方案：** 增加统一的顶层错误包装，确保非 BaseError 异常被包装为 BaseError。

---

### M-14 🟡 一般：`SummaryManager.Get` 返回值结构与 Python dict 字段映射差异

**问题分析：** Python 返回 dict（含 `mem`/`mem_type`/`source_id`/`metadata` key），Go 返回 `*index.MemoryDoc`。Go 的 typed struct 设计是合理的，但需确认上层调用方正确处理 `Doc.Text` 而非 Python 的 `"mem"` 键、`Doc.Fields["source_id"]` 而非 `"source_id"` 键。

**修复方案：** 添加注释或文档说明字段映射差异。

---

### M-15 🟡 一般：`WriteManager.getMemTypeFromIndex` 错误处理差异

**问题分析：** Python 在 `GetByID` 失败时统一返回 `None`（不区分"查询出错"和"记忆不存在"），Go **区分**了两种情况。Go 的行为更严格合理，但需确保上层调用方能正确处理 error 返回。

**修复方案：** Go 行为更合理，无需修改。确认上层调用方适配即可。

---

### T-10 💡 提示：`_process_conflict_info` 方法缺失

**修复方案：** 确认 Go 的 `MemUpdateChecker.Check` 返回的 ID 是否已经是原始 mem_id。如是，可安全忽略。

---

### T-11 💡 提示：`_add_memory_to_store` 逻辑已内联

Go 的内联方式在功能上等价，无需修改。

---

### T-12 💡 提示：VariableManager 日志 metadata/context 字段结构差异

Python 用 `metadata={"context": context}`，Go 用 `Str("context", context)`。

**修复方案：** 对齐 Python 日志格式，改为 `Any("metadata", map[string]any{"context": context})`。

---

### T-13 💡 提示：`VariableManager.DeleteByUserID` kvStore==nil 返回 `(false, nil)`

**修复方案：** 建议返回 error 或添加注释说明行为差异。

---

### T-14 💡 提示：doc.go 中 Python 路径标注需更新

---

## 七、Memory any 类型收紧

### S-21 🔴 严重：`CreateMemorySettings` sync 整数字段缺少 float64 类型断言

**文件：** `/home/opensource/uap-claw-go/internal/agentcore/memory/lite/config.go` L262-273

**问题：** `sync` 配置的 `watchDebounceMs` 和 `intervalMinutes` 只做了 `.(int)` 断言，没有 `.(float64)` 回退。当 overrides 来自 JSON 反序列化（如 YAML 配置文件或 HTTP API），Go 的 `encoding/json` 将所有数字解码为 `float64`，此时 `.(int)` 永远不会成功，这两个字段将被静默忽略。

相比之下，同文件中 `chunking.tokens`/`chunking.overlap` 和 `cache.maxEntries` 都同时处理了 `float64` 和 `int` 两种情况。

**Python 样例：** Python 用 `setattr(settings, key, value)` 直接赋值，动态类型无此问题。

**Go 当前问题代码：**
```go
case "sync":
    if v, ok := value.(map[string]any); ok {
        // ...
        if v2, ok := v["watchDebounceMs"].(int); ok {  // 只断言 int，JSON 解码的 float64 会失败
            s.Sync.WatchDebounceMs = v2
        }
        // ...
        if v2, ok := v["intervalMinutes"].(int); ok {  // 同样问题
            s.Sync.IntervalMinutes = v2
        }
    }
```

**修复方案：** 在 `.(int)` 之前先尝试 `.(float64)`，与 chunking/cache 字段保持一致：
```go
if v2, ok := v["watchDebounceMs"].(float64); ok {
    s.Sync.WatchDebounceMs = int(v2)
} else if v2, ok := v["watchDebounceMs"].(int); ok {
    s.Sync.WatchDebounceMs = v2
}
// intervalMinutes 同理
```

---

### S-22 🔴 严重：`UserMemoryRecord.Metadata` 类型为 `string`，与 Python `dict` 不一致

**文件：** `/home/opensource/uap-claw-go/internal/agentcore/memory/manage/mem_model/user_mem_store.go` L32-33

**问题：** Go 端 `Metadata` 字段类型为 `string`，但 Python 端 `metadata` 是一个 `dict`（`fields.get("metadata")` 返回 `{}` 等字典对象）。当 summary 类型记忆通过 MemoryIndex 存入（Fields 包含 `metadata: map[string]any{}`），如果后续通过 `UserMemStore.Get()` 反序列化为 `UserMemoryRecord`，JSON 中的 `{"metadata": {}}` 无法反序列化到 `string` 类型字段——`json.Unmarshal` 会将 JSON 对象静默跳过，`Metadata` 字段将丢失。

**Python 样例** (SummaryManager.get, L156)：
```python
"metadata": memory_doc.fields.get("metadata")  # 返回 dict，如 {} 或 {"key": "value"}
```

**Go 当前问题代码：**
```go
type UserMemoryRecord struct {
    // ...
    Metadata string `json:"metadata,omitempty"`  // 应为 map[string]any
}
```

**修复方案：** 将 `Metadata` 改为 `map[string]any` 以对齐 Python dict 类型：
```go
Metadata map[string]any `json:"metadata,omitempty"`
```

**影响范围：** `UserMemStore` 和 `UserMemoryRecord` 当前仅在 `mem_model` 包内部和测试中使用，未外部引用，重构不会导致编译破坏。

---

### M-23 🟡 一般：`stubMemoryIndexManager.Search` 签名与接口不匹配

**文件：** `/home/opensource/uap-claw-go/internal/agentcore/memory/lite/coding_memory_tool_ops_test.go` L268

**问题：** 测试 stub 的 `Search` 方法参数类型仍为 `map[string]any`，但 `MemoryIndexManager` 接口已改为 `SearchOpts`。当前该 stub 从未实际赋值给接口变量（死代码），编译不会报错，但如果后续有人使用此 stub 会立刻编译失败。

**Go 当前问题代码：**
```go
func (s *stubMemoryIndexManager) Search(_ context.Context, _ string, _ map[string]any) ([]SearchResult, error) {
    return nil, nil
}
```

**修复方案：** 将参数改为 `SearchOpts`：
```go
func (s *stubMemoryIndexManager) Search(_ context.Context, _ string, _ SearchOpts) ([]SearchResult, error) {
    return nil, nil
}
```

---

### T-15 💡 提示：`sharingConfig` 字段仍使用 `map[string]any`

有意为之的设计决策（sharing config key 集合不固定），暂不修改。

---

### T-20 💡 提示：`CacheConfig.MaxEntries` 和 `QueryConfig.MaxResults` 使用 `float64` 语义不当

**文件：** `/home/opensource/uap-claw-go/internal/agentcore/memory/lite/config.go` L33, L80

**问题：** `MaxResults`（最大结果数）和 `MaxEntries`（最大缓存条目数）语义上都是整数计数，但定义为 `float64`。虽然提交说明解释为"JSON 反序列化兼容"，但这只影响 `CreateMemorySettings` 的 overrides 解析。已定义的结构体字段完全可以用 `int`，因为 JSON 反序列化到 struct 时 `encoding/json` 会自动将 `float64` 转为 `int`（如果是整数值）。

**Python 样例：** Python 默认值分别是 `10` 和 `10000`，都是 int。

**修复方案：** 改为 `int` 类型，在 `CreateMemorySettings` 的 overrides 解析中用 `float64`→`int` 转换（与 SyncConfig 整数字段一致）。

---

### T-21 💡 提示：`shouldFullReindex` 中 `map[string]any` 与 `float64` 的比较可能不稳定

**文件：** `/home/opensource/uap-claw-go/internal/agentcore/memory/lite/manager_impl.go` L726

**问题：** `meta["chunkTokens"]` 从 JSON 反序列化为 `float64`，与 `m.settings.Chunking.Tokens`（也是 `float64`）直接用 `!=` 比较。对于整数数值（如 256.0 == 256.0），浮点直接比较通常是安全的。但如果未来有人通过 Python `setattr` 写入带小数的值（如 256.5），可能导致精度问题。当前实现与 Python 原文行为一致（Python 也用 `!=` 比较），因此只是提示级。

**修复方案：** 如果想更稳健，可转为 int 比较或使用阈值比较，但当前行为已对齐 Python，优先级低。

---

### T-22 💡 提示：Update 后的 JSON 可能包含 `UserMemoryRecord` 未定义的字段

**文件：** `/home/opensource/uap-claw-go/internal/agentcore/memory/manage/mem_model/user_mem_store.go` L182-229

**问题：** `Update` 方法用 `map[string]any` 做字段级合并后写回 JSON。如果 Update 传入的 `data` 包含 `UserMemoryRecord` 未定义的 key（如上游代码传入任意 dict key），这些 key 会保留在 JSON 中。后续 `Get()` 反序列化为 `UserMemoryRecord` 时，未定义的 key 被 `json.Unmarshal` 静默忽略，造成数据丢失。这是从 map[string]any 迁移到 typed struct 的固有权衡，Python 原文不存在此问题（因为始终用 dict 操作）。

**修复方案：** 设计上已接受（Update 保持 map[string]any 参数即为此原因）。如需完全兼容，可添加 `Extra map[string]any `json:"-"`` 字段来捕获未知 key，但当前不紧急。

---

### 附：合理保留的 `map[string]any`（不属于遗漏）

以下 `map[string]any` 用法经审查认为合理，不属于遗漏：
1. `CreateMemorySettings` 的 `overrides` 参数 — 对齐 Python `**overrides` 动态参数，无法改为 typed struct
2. `SqlDbStore` 全系列方法 — 通用 SQL 存储接口，表结构动态，不适合 typed struct
3. `MemoryDoc.Fields` — 对齐 Python `fields=dict`，key 不固定
4. `MemUpdateChecker.parseCheckItems` — 解析 LLM 动态 JSON 输出
5. `SemanticStore.AddDocs` 参数 — 对齐向量存储接口
6. `PromptApplier.Apply` 参数 — 模板变量，key 不固定
7. `coding_memory_tool_ops.go` 的 `Write` 返回值 — 对齐 Python 返回 dict

---

## 问题汇总

| 编号 | 级别 | 章节 | 问题 | 状态 |
|------|------|------|------|------|
| S-01 | 严重 | 9.66a | `WorktreeLifecycleRail` 接口缺少 4 个 hook 方法 | ☐ |
| S-02 | 严重 | 9.66a | `fireRail` 空实现，`lifecycleRails` 从未被调用 | ☐ |
| S-03 | 严重 | 9.66a | `WithWorktreeSessionState` 返回的 context 被丢弃 | ☐ |
| S-04 | 严重 | 9.66a | `WorktreeRail.Init` 缺少工具注册到 Agent | ☐ |
| S-05 | 严重 | 9.66a | `WorktreeRail.Uninit` 缺少从 Agent 注销工具 | ☐ |
| S-06 | 严重 | 9.66a | `simpleMatch` 不是 `fnmatch` 等价实现 | ☐ |
| S-07 | 严重 | 9.24 | `handleEvolutionFromSignals` 绕过 `finalize_staged_evolution_request` | ☐ |
| S-08 | 严重 | 9.24 | `RunEvolution` 缺少顶层异常捕获 | ☐ |
| S-09 | 严重 | 9.80a | `HasSkillPackage` 缺少 error 返回值 | ☐ |
| S-10 | 严重 | 10.6.7 | `RuntimePromptRail` 缺少 `Uninit`，7 个 section 残留 | ☐ |
| S-11 | 严重 | 10.6.7 | `injectTimeSection`/`injectRuntimeSection` 未先 `RemoveSection` | ☐ |
| S-12→提示 | 10.6.7 | `BuildProjectMemorySection` 省略 language 参数（Python 也不使用） | ☐ |
| S-13 | 严重 | 10.6.3-10 | `ResponsePromptRail` 未实现（返回 nil） | ☐ |
| S-14 | 严重 | 10.6.3-10 | `JiuClawStreamEventRail` 未实现（返回 nil，~740 行） | ☐ |
| S-15 | 严重 | 10.6.3-10 | `MemoryRail`/`ExternalMemoryRail` 未实现（返回 nil） | ☐ |
| S-19 | 严重 | 9.24 | `_upload_approved_records_for_sharing` 手动审批路径缺失 | ☐ |
| S-21 | 严重 | Memory | `CreateMemorySettings` sync 整数字段缺少 float64 断言 | ☐ |
| S-22 | 严重 | Memory | `UserMemoryRecord.Metadata` 类型 string vs Python dict | ☐ |
| M-01 | 一般 | 9.66a | `resolveOwner` 始终返回空，owner 信息丢失 | ☐ |
| M-02 | 一般 | 9.66a | `Init` 硬编码 `lang="cn"` 和 `agentID=""` | ☐ |
| M-03 | 一般 | 9.66a | `copyFile` 不保留文件元数据，不等价 `shutil.copy2` | ☐ |
| M-04 | 一般 | 9.66a | `WorktreeBackend.Remove` 返回 `bool` 而非 `error` | ☐ |
| M-05 | 一般 | 9.66a | `NewWorktreeConfig` 未显式设置 `Enabled: false` | ☐ |
| M-06 | 一般 | 9.24 | `GenerateAndEmitExperience` 方法缺失 | ☐ |
| M-07 | 一般 | 9.80a | `ExperienceSharer` 中 `defer recover()` 冗余 | ☐ |
| M-08 | 一般 | 10.6.7 | `injectTimeSection` 中文模式下 en key 内容不一致 | ☐ |
| M-09 | 一般 | 10.6.3-10 | ACP 通道权限确认降级为 interrupt | ☐ |
| M-10 | 一般 | 10.6.3-10 | Rail 模式切换缺少 MemoryRail 处理 | ☐ |
| M-11 | 一般 | 10.6.3-10 | `LspRail` 未实现 | ☐ |
| M-12 | 一般 | 7.10 | `VariableManager.AddMemories` 返回 nil 切片而非空切片 | ☐ |
| M-13 | 一般 | 7.10 | `AddMemories` 缺少顶层 BaseError 传播模式 | ☐ |
| M-14 | 一般 | 7.10 | `SummaryManager.Get` 返回值字段映射差异 | ☐ |
| M-15 | 一般 | 7.10 | `WriteManager.getMemTypeFromIndex` 错误处理差异 | ☐ |
| M-16 | 一般 | 9.24 | `_legacy_user_intent` 方法缺失 | ☐ |
| M-17 | 一般 | 9.24 | `buildExperienceSharer` 缺少 `backend_name` 验证 | ☐ |
| M-18 | 一般 | 9.24 | `download_top_k` 未从配置中解析 | ☐ |
| M-19 | 一般 | 9.24 | `approval_runtime`/`team_signal_detector` 每次 Init 重建 | ☐ |
| M-20 | 一般 | 9.24 | `record_llm_policy` 与 Python 的 `record_llm_usage_policy` 不同步 | ☐ |
| M-21 | 一般 | 9.80a | `ListCachedBundles` 缺少排序 | ☐ |
| M-22 | 一般 | 9.80a | `HubClient.InstallSkill` 返回 error 而 Python 静默返回 None | ☐ |
| M-23 | 一般 | Memory | `stubMemoryIndexManager.Search` 签名与接口不匹配 | ☐ |
| M-24 | 一般 | 9.24 | `TeamSkillEvolutionRail.EvolutionConfig()` 缺少 `max_concurrent_evolution` | ☐ |
| M-25 | 一般 | 9.80a | `MatchFailureKeywords` 缺少 `error = None` 排除逻辑 | ☐ |
| T-01 | 提示 | 9.66a | `os.Symlink` Windows 差异 | ☐ |
| T-02 | 提示 | 9.66a | `Invoke` 返回格式与 Python `ToolOutput` 不对齐 | ☐ |
| T-03 | 提示 | 9.66a | `CleanupStaleWorktrees` mtime 非 UTC | ☐ |
| T-04 | 提示 | 9.66a | `CleanupStaleWorktrees` 未并行安全检查 | ☐ |
| T-05 | 提示 | 9.24 | `inferSkillFromTexts` 注释过时 | ☐ |
| T-06 | 提示 | 9.80a | `hubPath` 环境变量读取差异（等价） | ☐ |
| T-07 | 提示 | 10.6.7 | `BuildProjectMemorySection` 省略 language（Python 也不使用） | ☐ |
| T-08 | 提示 | 10.6.7 | `RuntimePromptRail` 省略 `timezone_offset` | ☐ |
| T-09 | 提示 | 10.6.7 | Windows SHELL 环境变量 fallback 缺失 | ☐ |
| T-10 | 提示 | 7.10 | `_process_conflict_info` 缺失（需确认 checker 实现） | ☐ |
| T-11 | 提示 | 7.10 | `_add_memory_to_store` 已内联 | ☐ |
| T-12 | 提示 | 7.10 | VariableManager 日志 metadata/context 字段差异 | ☐ |
| T-13 | 提示 | 7.10 | `DeleteByUserID` kvStore==nil 返回 `(false, nil)` | ☐ |
| T-14 | 提示 | 7.10 | doc.go Python 路径标注需更新 | ☐ |
| T-15 | 提示 | Memory | `sharingConfig` 仍为 `map[string]any`（有意为之） | ☐ |
| T-16 | 提示 | 9.24 | `DetectActiveRequestSignals` 缺少异常隔离 | ☐ |
| T-17 | 提示 | 9.80a | `skillHint` variadic vs 显式参数风格差异 | ☐ |
| T-18 | 提示 | 9.80a | `HubClient.Search` 缺少分页参数 | ☐ |
| T-19 | 提示 | 9.80a | `ShareStager` 筛选阈值硬编码 | ☐ |
| T-20 | 提示 | Memory | `CacheConfig`/`QueryConfig` 整数字段用 float64 | ☐ |
| T-21 | 提示 | Memory | `shouldFullReindex` float64 比较可能不稳定 | ☐ |
| T-22 | 提示 | Memory | Update JSON 含 `UserMemoryRecord` 未定义字段 | ☐ |

---

## 修复优先级建议

### P0 — 功能完全失效（立即修复）

1. **S-03**（context 丢弃）— worktree session 状态管理完全失效
2. **S-04**（工具未注册）— LLM 无法发现 worktree 工具
3. **S-07**（审批快照不注册）— 审批流程完全失效
4. **S-19**（手动审批路径缺少上传）— 团队经验共享断裂
5. **S-21**（sync float64 断言缺失）— JSON 配置场景字段静默丢失
6. **S-22**（Metadata string vs dict）— summary 类型记忆元数据反序列化丢失

### P1 — 核心功能缺失（本周修复）

7. **S-02**（fireRail 空实现）— lifecycle hook 机制失效
8. **S-05**（工具未注销）— rail 替换后工具残留
9. **S-08**（缺少 panic 保护）— agent 崩溃风险
10. **S-10**（RuntimePromptRail 缺 Uninit）— section 残留
11. **S-09**（HasSkillPackage 缺 error）— 检查出错被静默吞掉

### P2 — 行为偏差（本周修复）

12. **S-01**（接口缺少 4 个 hook）— 生命周期事件拦截缺失
13. **S-06**（simpleMatch 不等价）— 复杂 glob 失效
14. **S-11**（未先 RemoveSection）— 可能重复 section
15. **M-08**（time section en key 不一致）— 行为偏差
16. **M-12**（nil 切片 vs 空切片）— JSON 序列化差异
17. **M-25**（MatchFailureKeywords 缺排除）— ShareStager 误判失败

### P3 — 待实现标记确认（按计划推进）

18. **S-13**（ResponsePromptRail）— ⤵️ 10.6.3-10
19. **S-14**（StreamEventRail）— ⤵️ 10.6.3-10
20. **S-15**（MemoryRail）— ⤵️ 10.6.3-10

---

## 已排除的误报

| 原编号 | 误报内容 | 排除原因 |
|--------|---------|---------|
| 原 S-16 | `_parse_messages` 方法缺失 | Python 的 `_parse_messages` 是消息格式归一化函数（dict/to_dict/model_dump），Go 直接接收 `[]map[string]any`，无需此转换 |
| 原 S-17 | `parseTopLevelFrontmatter` 返回 `map[string]string` | Python 的 `parse_top_level_frontmatter` 也返回 `dict[str, str]`（只解析标量字段），Go 行为一致 |

---

## 审查方法说明

本次审查采用以下方法：
1. **Git 历史**：提取 48 小时内 60+ 提交，按章节归类
2. **Python 对照**：逐方法比较 Python 与 Go 的参数、步骤、异常处理
3. **TODO/Stub 扫描**：grep 所有 TODO、stub、placeholder、⤵️ 关键词
4. **模型字段对齐**：对比 Pydantic BaseModel 和 Go struct 的字段完整性
5. **回调/事件链路**：确认事件处理链路是否完整
6. **Context 传播**：确认 context.Value 链路是否正确传递
7. **工具注册/注销**：确认工具是否正确注册到 Agent 能力管理器
8. **类型断言路径**：检查 `map[string]any` 场景下 int/float64 断言完整性
