# 48h 逻辑审查 — 2026-09-21

> 审查范围：最近 48h 内提交（81a4233b fix(ci): 修复 AgentServer.Stop 竞态条件及 48h 审查问题）涉及的模块
> 及此前一周（9.12-9.19）完成的实现章节：9.24 P3/P4 SkillEvolutionRail、9.66a WorktreeManager、10.6.7 RuntimePromptRail/ProjectMemoryRail、memory any tightening

**审查方法**：逐文件对照 Python 参考实现，检查方法签名、执行步骤、错误处理、日志对齐；验证 ⤵️/TODO 标记的真实状态

---

## 问题汇总

| 级别 | 数量 |
|------|------|
| 严重 | 22 |
| 一般 | 39 |
| 提示 | 26 |

---

## 严重问题 (S)

### S-01: DeepAdapter.ProcessMessageImpl/ProcessMessageStreamImpl 未调用 updateRuntimeConfig

**Python 样例** (interface_deep.py L4475):
```python
# process_message_impl 步骤 17
await self._update_runtime_config(runtimeConfig)
# 内部：SetLanguage/SetChannel/SetMode/WriteRuntimeStateYAML/7个RuntimePromptRail setter
```

**Go 问题** (deep_adapter.go L781-792):
```go
// 步骤 16-17: update_runtime_config + CWD 种子
if requestCwd != "" {
    d.seedRuntimeCwd(ctx, requestCwd)  // 只做了 CWD 种子，未调用 d.updateRuntimeConfig()
}
```

`updateRuntimeConfig` 方法已存在于 `deep_adapter_config.go:82`，但 `ProcessMessageImpl` 和 `ProcessMessageStreamImpl` 都没有调用它。`CodeAdapter.updateRuntimeConfig` (code_adapter.go:535) 同样未被调用。

**影响**：每次请求不会触发 RuntimePromptRail 的 7 个 setter（SetLanguage/SetChannel/SetMode 等）、runtime_state.yaml 不会更新、语言/通道等运行时配置不会刷新。

**修复方案**：在 `ProcessMessageImpl` L781 和 `ProcessMessageStreamImpl` L929 处构造 `runtimeConfig` 并调用 `d.updateRuntimeConfig(ctx, config)`。

---

### S-02: DeepAdapter.ProcessMessageImpl/ProcessMessageStreamImpl 未调用 handleSlashCommand

**Python 样例** (interface_deep.py L4438):
```python
slash_result = await self._handle_slash_command(query, session_id, mode)
if slash_result:
    # 处理 approval_chunks 或返回内容
```

**Go 问题** (deep_adapter.go L747):
```go
// ⤵️ 10.6.3-10: slash_result = _handle_slash_command(query, sessionID, mode)
```

`handleSlashCommand` 方法已实现 (deep_adapter_slash.go L28-47)，但未被调用。`ProcessMessageStreamImpl` L898 同样跳过。

**影响**：用户无法使用 `/evolve`、`/evolve_simplify`、`/evolve_rebuild`、`/evolve_rollback`、`/evolve_list` 命令。

**修复方案**：在步骤 7 位置调用 `d.handleSlashCommand(ctx, query, sessionID, mode)`，处理返回结果。

---

### S-03: RuntimePromptRail.injectTimeSection 中文模式下 en key 填充英文

**Python 样例** (runtime_prompt_rail.py L129-146):
```python
if language == "cn":
    content = {"cn": time_content, "en": time_content}  # cn 和 en 都是中文
```

**Go 问题** (runtime_prompt_rail.go L293-303):
```go
if !forceEnglish && language == "cn" {
    // Go 生成了 timeContentCN 和 timeContentEN 两个版本
    content["cn"] = timeContentCN
    content["en"] = timeContentEN  // ← 错误：en key 填了英文
}
```

**影响**：与 Python 行为不一致。Python 的设计是 cn 模式下两个 key 都是中文内容。

**修复方案**：cn 模式下 `content["en"]` 也填 `timeContentCN`，与 Python 一致。

---

### S-04: RuntimePromptRail.existingDirs/existingDir 缺少 expanduser

**Python 样例** (runtime_prompt_rail.py L106):
```python
path = os.path.abspath(os.path.expanduser(item.strip()))
```

**Go 问题** (runtime_prompt_rail.go L645):
```go
path := filepath.Clean(item)  // 没有 expandHome / filepath.Abs
```

L667 同样缺少 expanduser。

**影响**：如果 `trusted_dirs` 包含 `~/project` 之类的路径，Go 不会展开 `~`，导致 `trusted_dirs_policy` section 不会被注入，安全策略失效。

**修复方案**：调用 `common.ExpandHome(path)` + `filepath.Abs(path)` 对齐 Python。

---

### S-05: RuntimePromptRail 缺少 Uninit 方法

**Python 样例** (runtime_prompt_rail.py L53-63):
```python
def uninit(self, agent):
    # remove_section: time/runtime/language_output/env/git_status/browser_tool_policy/trusted_dirs_policy
    self.system_prompt_builder = None
```

**Go 问题**：`RuntimePromptRail` 没有 `Init` 和 `Uninit` 方法。Go 版本从 `cbc.Agent().SystemPromptBuilder()` 获取 builder，不存储引用，无法在 Uninit 时清理注入的 7 个 section。

**影响**：Rail 卸载后，已注入的 section 残留在 SystemPromptBuilder 中，不会被清除。

**修复方案**：实现 `Uninit` 方法，调用 `builder.RemoveSection()` 清除所有 7 个 section。

---

### S-06: buildAgentRails 中 SkillEvolutionRail 在冷启动时被挂载

**Python 样例** (interface_deep.py L2158-2161):
```python
# SkillEvolutionRail 不在冷启动时挂载，由 _update_rails_for_mode 按 mode 按需注册/注销
# MemoryRail 不在冷启动时挂载，由 _update_rails_for_mode 按 mode 按需注册/注销
```

**Go 问题** (deep_adapter_rails.go L101-109):
```go
// 步骤 8: skillEvolutionRail
evolve := d.buildSkillEvolutionRail()
if evolve != nil {
    // 直接注册到 railsList — 冷启动就挂载了
}
```

**影响**：Python 设计为冷启动不挂载 SkillEvolutionRail/MemoryRail，由 `updateRailsForMode` 按 mode 动态决定。Go 在冷启动时直接挂载，与 Python 行为不一致。当 `evolution.enabled=true` 时，Go 在 agent.fast 模式也挂载了 SkillEvolutionRail，Python 不会。

**修复方案**：从 `buildAgentRails` 中移除 SkillEvolutionRail 的直接注册，改为在 `updatePlanModeRails` / `updateAgentModeRails` 中按需注册，对齐 Python。

---

### S-07: buildAgentRails 中 Rail 顺序与 Python 不一致

**Python 样例** (interface_deep.py L2130-2156) 顺序：
1. runtime_prompt_rail → 2. response_prompt_rail → 3. stream_event_rail → 4. task_planning_rail → 5. security_rail → 6. heartbeat_rail → 7. avatar_rail → 8. subagent_rail → 9. permission_rail → 10. context_processor_rail

**Go 问题** (deep_adapter_rails.go L44-217) 顺序：
heartbeat → taskPlanning → filesystem → agentMode → mcp → progressiveTool → skill → skillEvolution → skillCreate → streamEvent → subagent → security → memory → externalMemory → avatar → runtimePrompt → responsePrompt → contextAssemble → contextProcessor → permission → userHook

**影响**：Rail 顺序影响优先级和执行顺序。Python 中 runtime_prompt_rail 排最前面（优先级最高），Go 排在中间靠后。

**修复方案**：调整 `buildAgentRails` 中的 append 顺序，将 runtimePromptRail/responsePromptRail/streamEventRail/taskPlanningRail/securityRail/heartbeatRail/avatarRail/subagentRail/permissionRail/contextProcessorRail 的注册顺序对齐 Python。

---

### S-08: TeamBackend.CancelTask 缺少前置幂等校验

**Python 样例** (team.py L851-896):
```python
async def cancel_task(self, task_id: str) -> bool:
    task = await self.task_manager.get(task_id)
    if not task:
        return False
    if task.status == TaskStatus.CANCELLED.value:
        return True  # 幂等返回
    cancelled_task = await self.task_manager.cancel(task_id)
```

**Go 问题** (team_backend.go L807-833):
```go
func (tb *TeamBackend) CancelTask(ctx context.Context, taskID string) atschema.MemberOpResult {
    unblocked, err := tb.taskManager.Cancel(ctx, taskID)
    // 直接调用 Cancel，没有前置的 task 存在检查和已取消幂等检查
```

**影响**：对已取消的任务重复调用 `CancelTask`，Python 幂等返回 True，Go 会返回 error。

**修复方案**：在调用 `Cancel` 前先 `Get` 任务，检查是否存在且是否已取消。

---

### S-09: TeamBackend.SpawnMember 未使用 Allocation.ToDBRef()

**Python 样例** (team.py L280-281):
```python
model_ref_json: Optional[str] = _json.dumps(allocation.to_db_ref()) if allocation is not None else None
```

**Go 问题** (team_backend.go L399-410):
```go
if cfg.allocation != nil {
    refMap := map[string]any{"model_name": cfg.allocation.Entry.ModelName, "model_index": cfg.allocation.GroupIndex}
    // 手动构建 refMap，没有使用已有的 Allocation.ToDBRef() 方法
```

L407-410 对 `modelConfigAllocator` 分支也重复了同样代码。

**影响**：如果 `ToDBRef()` 的字段映射发生变化，手动构建的 refMap 不会同步更新。

**修复方案**：替换为 `json.Marshal(cfg.allocation.ToDBRef())`，对齐 Python。

---

### S-10: SessionManager.CancelSessionTask 在 waitTimeout==nil 时不等待任务完成

**Python 样例** (session_manager.py L52-65):
```python
task.cancel()
try:
    if wait_timeout is None:
        await task  # 无限期等待任务完成
    else:
        await asyncio.wait_for(task, timeout=wait_timeout)
```

**Go 问题** (session_manager.go L80-106):
```go
cancelFn()
if waitTimeout != nil {
    select {
    case <-time.After(*waitTimeout):
        // 超时
    case <-ctx.Done():
    }
}
// waitTimeout == nil 时完全不等待，直接清理
sm.mu.Lock()
sm.sessionTasks[sessionID] = nil
sm.mu.Unlock()
```

**影响**：Go 可能在任务 goroutine 尚未真正停止时就清理引用，导致 goroutine 泄漏或数据竞争。Python 在 `wait_timeout=None` 时会无限期等待。

**修复方案**：当 `waitTimeout == nil` 时，应等待任务完成信号（如通过 channel 或 context cancel 传播后的确认），而非直接跳过等待。

---

### S-11: TaskManager.ApprovePlan 缺少 assignee 校验

**Python 样例** (task_manager.py L1279-1280):
```python
if not existing.assignee:
    return TaskOpResult.fail(f"Task {task_id} has no assignee")
```

**Go 问题** (task_manager.go L843-916)：`ApprovePlan` 方法没有检查 `task.Assignee` 是否为空。

**影响**：无 assignee 的任务可以通过审批，Python 会拒绝。

**修复方案**：在 `ApprovePlan` 入口处增加 assignee 空值校验。

---

### S-12: SkillEvolutionRail.approvalRuntime 缺少延迟重建逻辑

**Python 样例** (skill_evolution_rail.py L266-279):
```python
@property
def approval_runtime(self):
    if self._manager is not self._approval_runtime._manager or ...:
        self._approval_runtime = ApprovalRuntime(self._manager, ...)
    return self._approval_runtime
```

**Go 问题** (skill_evolution_rail.go L386-388):
```go
func (r *SkillEvolutionRail) ApprovalRuntime() *ApprovalRuntime {
    return r.approvalRuntime  // 直接返回构造时创建的，没有重建检查
}
```

**影响**：如果 `manager` 或 `pendingApprovalSnapshots` 在运行时被替换，Go 版本不会重建 approvalRuntime，可能导致审批路由到错误的 manager。

**修复方案**：在 `ApprovalRuntime()` 中增加与 Python 等价的一致性检查，不一致时重建。

---

### S-13: SkillEvolutionRail.detectActiveRequestSignals 缺少异常保护

**Python 样例** (skill_evolution_rail.py L646-653):
```python
try:
    trajectory_signals = self.evolution_signal_detector.detect_trajectory_signals(...)
except Exception as exc:
    logger.warning("...")
    trajectory_signals = []
```

**Go 问题** (skill_evolution_rail.go L1776-1787)：`DetectTrajectorySignals` 无 defer/recover 保护，`DetectUserIntent` 错误被静默忽略（`_` 丢弃）。

**影响**：如果 `DetectTrajectorySignals` panic，整个 `RequestUserEvolution` 调用失败，而 Python 会优雅降级返回空信号。

**修复方案**：在 `detectActiveRequestSignals` 中为 `DetectTrajectorySignals` 增加 defer/recover 保护，panic 时返回空信号。

---

### S-14: ContextEvolutionRail 完全缺失

**Python 样例** (context_evolution_rail.py L28-268)：
```python
class ContextEvolutionRail(EvolutionRail):
    async def before_task_iteration(self, ctx):  # 检索记忆+注入系统提示
    async def after_task_iteration(self, ctx):   # 恢复提示+自动摘要
    def extract_trajectory(self, ...):            # 从 context_engine 提取轨迹
```

**Go 问题**：`internal/agentcore/harness/rails/evolution/` 目录中完全没有 `context_evolution_rail.go` 文件。

**影响**：上下文演进功能不可用。这是 IMPLEMENTATION_PLAN.md 9.24 P6 中标记为 ☐ 的内容，确认确实未实现。

**修复方案**：实现 `ContextEvolutionRail`，对齐 Python 的三个核心方法。

---

### S-15: ProjectMemoryRail.BeforeModelCall 缺少 panic recover 保护

**Python 样例** (project_memory_rail.py L155-171):
```python
try:
    files = discover_and_load_memory_files(...)
except (OSError, ValueError, TypeError) as exc:
    logger.exception(...)
    files = []
```

**Go 问题** (project_memory_rail.go L221-225)：
```go
files := project_memory.DiscoverAndLoadMemoryFiles(...)
// 无 defer/recover 保护
```

**影响**：如果 `DiscoverAndLoadMemoryFiles` 内部 panic，整个 BeforeModelCall 会崩溃，阻塞 model call。Python 保证 rail 崩溃不会阻塞。

**修复方案**：在 `BeforeModelCall` 中为 `DiscoverAndLoadMemoryFiles` 调用增加 defer/recover 保护。

---

### S-16: ProjectMemory files.go 中 JIUWENSWARM→UAPCLAWSWARM 缺少兼容标记

**Python 样例** (files.py)：使用 `JIUWENSWARM.md` 和 `.jiuwen/` 作为项目标记。

**Go 问题** (files.go)：使用 `UAPCLAWSWARM.md` 和 `.uapclaw/`，但 `projectRootMarkers` 中没有保留 `.jiuwen` 作为兼容标记。

**影响**：Go 无法发现旧版 Python 项目根目录（以 `.jiuwen` 标记的），迁移后项目记忆文件不会被加载。

**修复方案**：在 `projectRootMarkers` 中增加 `.jiuwen` 作为兼容回退标记，同时检查 `JIUWENSWARM.md` 文件。

---

### S-17: TeamBackend.CancelAllTasks 返回类型丢失取消数量

**Python 样例** (team.py L898-935):
```python
async def cancel_all_tasks(self, skip_assignees=None) -> int:
    return len(cancelled_tasks)  # 返回被取消的任务数
```

**Go 问题** (team_backend.go L837-849):
```go
func (tb *TeamBackend) CancelAllTasks(ctx context.Context, skipAssignees []string) atschema.MemberOpResult {
    // 返回 MemberOpResult，丢失了取消数量信息
```

**影响**：调用者无法知道有多少任务被取消，广播消息中 Go 硬编码了取消数量的格式化。

**修复方案**：在 `MemberOpResult` 中携带取消数量，或改为返回 `(int, error)` 对齐 Python。

---

### S-18: TeamBackend leader_member_name 初始化逻辑差异

**Python 样例** (team.py L136):
```python
self.leader_member_name = str(leader_member_name or (member_name if is_leader else "")).strip()
```

**Go 问题** (team_backend.go L229):
```go
leaderMemberName: memberName, // 无论 is_leader 是什么都默认为 memberName
```

**影响**：当 `is_leader=False` 且未传 `leader_member_name` 时，Go 将当前成员（非 leader）的名字设为 leaderMemberName，Python 留空。

**修复方案**：对齐 Python 逻辑：当 `leaderMemberName` 为空且 `isLeader=true` 时回退到 `memberName`，否则留空。

---

### S-19: WorktreeManager.fireRail 完全未实现

**Python 样例** (manager.py L694-714):
```python
def _fire_rail(self, method, *args, **kwargs):
    for rail in self._rails:
        result = getattr(rail, method)(*args, **kwargs)
        if result is not None:
            last_result = result
    return last_result
```

**Go 问题** (manager.go L443-447):
```go
func fireRail(rails []WorktreeLifecycleRail, method string, args ...any) any {
    return nil  // 完全没有实现 rail 调度逻辑
}
```

**影响**：WorktreeManager 的所有生命周期 hook（before_worktree_create, after_worktree_create, before_worktree_exit, after_worktree_exit）**完全不会被调用**。AutoSetupRail（依赖安装）和 DiffSummaryRail（diff 摘要）完全不工作。

**修复方案**：实现 fireRail，遍历 rails 切片，通过反射或方法接口调用对应 hook，返回最后一个非 nil 结果。

---

### S-20: WorktreeRail.Init 未注册工具到 Agent

**Python 样例** (rails.py L134-136):
```python
Runner.resource_mgr.add_tool(self._tools)
agent.ability_manager.add(tool.card)
```

**Go 问题** (rails.go L142-157)：
```go
r.tools = []tool.Tool{enterTool, exitTool}
// 只创建了工具列表，完全没有注册到 Agent
```

**影响**：`enter_worktree` 和 `exit_worktree` 工具对 Agent 不可见，Agent 无法调用它们。

**修复方案**：在 `Init` 中调用 `agent.AbilityManager().Add(tool.Card())` 和资源管理器注册。

---

### S-21: WorktreeRail.Init 中 WithWorktreeSessionState 返回的新 ctx 被丢弃

**Python 样例** (rails.py L127)：`init_session_state()` 在 `_state` ContextVar 中设置 holder，所有后续 tool 调用通过 `_state.get()` 自动共享。

**Go 问题** (rails.go L140)：
```go
state := InitWorktreeSessionState()
_ = WithWorktreeSessionState(ctx, state)  // 返回的新 context 被丢弃！
```

**影响**：后续 `GetCurrentSession(ctx)` 使用原始 ctx，找不到注入的 state，返回 nil。整个 session 传播链断裂——WorktreeSession 不可用。

**修复方案**：将 `WithWorktreeSessionState` 返回的新 ctx 传播出去，存入 Rail 或通过回调注入 Agent 的 context 链。

---

### S-22: Worktree tools.resolveOwner 硬编码空值

**Python 样例** (tools.py L64-72):
```python
def _resolve_owner(kwargs):
    owner_id = kwargs.get("owner_id") or kwargs.get("member_name", "")
    tag = kwargs.get("tag") or kwargs.get("team_name", "")
    return owner_id, tag
```

**Go 问题** (tools.go L286-290):
```go
func resolveOwner(opts []tool.ToolOption) (string, string) {
    return "", ""  // 硬编码空值
}
```

**影响**：所有 worktree 事件（WorktreeCreatedEvent/WorktreeRemovedEvent）的 OwnerID 和 Tag 始终为空。team 框架无法通过事件追踪哪个 member 拥有哪个 worktree。

**修复方案**：从 tool options 中提取 owner_id/tag 参数，对齐 Python。

---

## 一般问题 (M)

### M-01: handleStream 层 vs UapClaw 层的分工导致混淆

**Python 样例**：`_handle_stream` (interface.py) 中直接调 `append_history_record` + memory hook。

**Go 问题**：`handle_envelope.go` 的 `handleStream` 中没有 history 和 memory hook，但这些逻辑在 `uapclaw.go` 的 `ProcessMessageStreamImpl` 中实现了。

**影响**：逻辑正确但架构不同——Go 将 history/memory hook 下沉到 UapClaw 层。需确保 handleStream 的调用链一定经过 UapClaw。

**修复方案**：添加注释说明分工，或在 handleStream 中增加防御性检查确保 UapClaw 已处理 history。

---

### M-02: RuntimePromptRail.readRuntimeStateYAML 被调用 3 次

**Python 样例** (runtime_prompt_rail.py L148-155)：`runtime_state` 只读取一次，所有字段共用。

**Go 问题** (runtime_prompt_rail.go)：`readRuntimeStateYAML()` 在 `injectRuntimeSection`(L321)、`injectLanguageOutputSection`(L364)、`injectGitStatusSection`(L460) 各调用一次。

**修复方案**：在 `BeforeModelCall` 入口处读取一次 runtimeState，传给各 inject 方法。

---

### M-03: RuntimePromptRail.injectEnvSection 的 osVersion 不对齐

**Python**：`f"{platform.system()} {platform.release()}"` → "Linux 6.5.0"
**Go** (runtime_prompt_rail.go L393)：`fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)` → "linux amd64"

**修复方案**：使用 `syscall.Uname()` 获取 release 信息，对齐 Python 输出格式。

---

### M-04: TeamBackend.ShutdownMember 使用 TryTransitionMemberStatus (CAS) vs Python update_member_status

**Python** (team.py L566-568)：`update_member_status(member_name, team_name, SHUTDOWN_REQUESTED)` — 非 CAS。
**Go** (team_backend.go L534-535)：`TryTransitionMemberStatus(memberName, teamName, member.Status, SHUTDOWN_REQUESTED)` — CAS。

**影响**：并发场景下行为不同。Go 的 CAS 更安全但与 Python 不一致。如果为有意改进，需添加注释说明。

---

### M-05: TeamBackend.startup 对 startup_member 返回 false 的处理不一致

**Python** (team.py L356-358)：无条件 append started 列表。
**Go** (team_backend.go L458-466)：仅 `ok=true` 时 append。

**修复方案**：对齐 Python，即使 startup_member 返回 false 也继续并 append。

---

### M-06: TeamBackend.clean_team 日志级别不一致

**Python**：`team_logger.error(...)` — error 级别
**Go** (team_backend.go L742)：`logger.Warn(...)` — Warn 级别

**修复方案**：改为 Error 对齐 Python。

---

### M-07: TeamBackend.SpawnHumanAgent 缺少失败日志

**Python** (team.py L1147-1153)：`team_logger.warning("Failed to register human agent...")`
**Go** (team_backend.go L956-961)：直接返回 result，没有额外日志。

**修复方案**：在 SpawnHumanAgent 返回失败时增加 Warn 日志。

---

### M-08: TaskManager.submit_plan 路径验证不完整

**Python** (task_manager.py L905-1015)：使用 `_resolve_submitted_plan_path` 做 expanduser + 绝对路径 + 文件存在性检查，`shutil.copyfile` 复制文件。
**Go** (task_manager.go L756-839)：只做 `os.ReadFile`，丢失文件权限。

**修复方案**：增加 expanduser + 绝对路径校验，使用 `os.CopyFS` 或 `io.Copy` 保留文件权限。

---

### M-09: TaskManager.notifyLeaderOfPlan 不走 TeamMessageManager

**Python** (task_manager.py L833-851)：创建独立的 `TeamMessageManager` 实例发送 P2P 消息，返回 `leader_message_id`。
**Go** (task_manager.go L922-959)：直接用 `messager.Send`，不保存 message_id。

**修复方案**：使用 `TeamMessageManager.SendMessage` 对齐 Python，并将 message_id 写入 plan index。

---

### M-10: TaskManager.complete 事件缺少 MemberName

**Python** (task_manager.py L734-741)：`TaskCompletedEvent(member_name=completed_task.assignee)`
**Go** (task_manager.go L503-506)：`TaskCompletedEvent` 没有设置 MemberName。

**修复方案**：在 TaskCompletedEvent 中设置 `MemberName: task.Assignee`。

---

### M-11: SessionManager.EnsureSessionProcessor 不检查 processor 是否 done

**Python** (session_manager.py L78-125)：当 processor `done()` 时重建 queue 和 priority。
**Go** (session_manager.go L129-153)：只检查 sessionProcessors 和 sessionSignals 存在性，不检查 processor 是否已死。

**修复方案**：增加 processor 存活状态检查（如通过 channel 非阻塞探测）。

---

### M-12: SessionManager.processSessionQueue 不记录 task 错误

**Go** (session_manager.go L294-304)：`_, _ = item.task(taskCtx)` — 忽略 task 返回值。
**Python** (session_manager.py L96-103)：`except Exception as e` 有异常日志。

**修复方案**：记录 task 返回的 error。

---

### M-13: SessionManager.HasActiveTasks 语义偏差

**Python**：`any(t is not None and not t.done() for t in self._session_tasks.values())`
**Go** (session_manager.go L230-238)：检查 `cancelFn != nil`，但 cancelFn 不为 nil 不代表任务仍在执行。

**修复方案**：在 processSessionQueue 中 task 完成后尽快设 `sessionTasks[sessionID] = nil`，缩小误判窗口。

---

### M-14: SkillEvolutionRail.generate_and_emit_experience 缺失

**Python** (skill_evolution_rail.py L527-547)：`generate_and_emit_experience` 是向后兼容的公开 wrapper。
**Go**：完全缺失。

**修复方案**：添加 `GenerateAndEmitExperience` 方法，委托 `RunEvolution`。

---

### M-15: TeamSkillEvolutionRail.on_approve_record/on_reject_record 兼容别名缺失

**Python** (team_skill_evolution_rail.py L745-751)：`on_approve_record` / `on_reject_record` 是 `approve_record` / `reject_record` 的兼容别名。
**Go**：缺失。

**修复方案**：添加 `OnApproveRecord` / `OnRejectRecord` 方法，委托 `ApproveRecord` / `RejectRecord`。

---

### M-16: TeamSkillEvolutionRail.EvolutionConfig 缺少 max_concurrent_evolution

**Python** (team_skill_evolution_rail.py L340-351)：`evolution_config` 包含 `max_concurrent_evolution`。
**Go** (team_skill_evolution_rail.go L1005-1014)：缺少此字段。

**修复方案**：在 `EvolutionConfig` 结构体中增加 `MaxConcurrentEvolution` 字段。

---

### M-17: CodeAdapter 配置注释标记未实现但实际已实现

**Go** (code_adapter.go L925)：
```go
// ⤵️ 10.6.3-10: ConfirmInterruptRail 尚未实现
```
但 L928 调用了 `c.buildConfirmInterruptRail()`，该方法 (L1128-1132) 实际已实现。

**修复方案**：移除过时的 ⤵️ 标记和注释，标记为 ✅。

---

### M-18: CodeAdapter.ConfigureTeamMemberAgent 缺少 setCodingMemoryDirectory

**Python** (interface_code.py)：`_set_coding_memory_directory(agent, self._project_dir)`
**Go** (code_adapter.go L1427)：`// ⤵️ 待回填: setCodingMemoryDirectory`

**修复方案**：实现 `setCodingMemoryDirectory` 或调用已有的 CodingMemoryManager 设置方法。

---

### M-19: ProjectMemoryRail.SetAdditionalDirectories 用 Abs 而非 EvalSymlinks

**Python** (project_memory_rail.py L136)：`os.path.realpath(d)` — 解析符号链接。
**Go** (project_memory_rail.go L183)：`filepath.Abs(d)` — 不解析符号链接。

**修复方案**：改用 `filepath.EvalSymlinks` 对齐 Python。

---

### M-20: ProjectMemory/files.go fnmatchMatch 不支持 [seq] 语法

**Python** (files.py L703)：`fnmatchcase(candidate, normalized)` — 完整 fnmatch 支持。
**Go** (files.go L936)：`filepath.Match(pattern, name)` — 只支持 `*` 和 `?`。

**修复方案**：使用第三方 fnmatch 库（如 `github.com/minio/pkg/v2/fnmatch`）或自行实现 `[seq]` 支持。

---

### M-21: ProcessInterrupt 中 StreamEventRail 操作全部标记 ⤵️

**Python** (interface_deep.py L6-8)：pause → `streamEventRail.pause(session_id)`，resume → `streamEventRail.resume(session_id)`
**Go** (deep_adapter.go L1203-1233)：所有 pause/resume/supplement/cancel 分支中 StreamEventRail 操作标记为 `⤵️ 10.6.3-10`。

**影响**：StreamEventRail 的 pause/resume 功能完全无效，因为 `buildStreamEventRail` 本身也返回 nil。

**修复方案**：等 SkillCreateRail/StreamEventRail/MemoryRail 等 Rail 类型实现后回填此调用链。

---

### M-22: ProcessMessageStreamImpl 缺少 team 模式分流

**Python** (interface_deep.py L7)：`if mode in ("team", "team.plan", "code.team"): → team_helpers.process_team_message_stream`
**Go** (deep_adapter.go L891-892)：`// ⤵️ 9.55-9.65 TeamHelpers`

**影响**：team 模式请求无法正确分流处理。

---

### M-23: ProcessMessageStreamImpl 缺少 auto_harness 分流

**Python** (interface_deep.py L8)：`if mode == "auto_harness": → auto_harness 分流`
**Go** (deep_adapter.go L895)：`// ⤵️ 10.6.11-12`

---

### M-24: CreateInstance 中 load_user_rails 缺失

**Python** (interface_deep.py L2614+)：`await self.load_user_rails()`
**Go**：标记为 `⤵️ 10.6.3-10`，完全跳过。

**影响**：用户自定义的 Rail 扩展无法加载。

---

### M-25: CreateInstance 中 _jiuwenswarm_project_dir 属性设置缺失

**Python** (interface_deep.py L2609)：`setattr(self._instance, "_jiuwenswarm_project_dir", self._project_dir or self._workspace_dir)`
**Go**：缺失此属性设置。

**修复方案**：在 Go 的 CreateInstance 中设置等价属性（可能通过 DeepConfig 或直接字段赋值）。

---

### M-26: updatePlanModeRails 中 SkillCreateRail 处理标记 ⤵️

**Python** (interface_deep.py L2824-2845)：完整的 SkillCreateRail 注册/注销逻辑。
**Go** (deep_adapter_rails.go L712-714)：`// ⤵️ 待回填: SkillCreateRail 处理`

---

### M-27: updatePlanModeRails/updateAgentModeRails 中 MemoryRail 处理标记 ⤵️

**Python** (interface_deep.py L2775-2777, L2876-2878)：`_handle_memory_rail_by_config` 和 `_handle_external_memory_rail_by_config`
**Go** (deep_adapter_rails.go L633, L637, L775, L779)：`// ⤵️ 待回填: handleMemoryRailByConfig`

---

### M-28: AgentServer.Stop 中 agentManager 可能为 nil

**Go** (agent_server.go L136-142)：如果 Stop() 在 run() 初始化 agentManager 之前被调用，Cleanup 被跳过。
**Python**：agent_manager 在构造时创建，不存在此问题。

**修复方案**：在 run() 中提前创建 agentManager（在 goroutine 启动前），或 Stop 中增加等待。

---

### M-29: WorktreeManager.resolveTargetPath 静默降级而非报错

**Python** (manager.py L644-670)：`_resolve_target_path` 在 `get_workspace()` 返回 None 时直接 `raise RuntimeError`。
**Go** (manager.go L410-421)：返回 `""` 时降级到 `m.config.BaseDir` 再降级到 `cwd.GetCwd(ctx)`。

**影响**：Go 不会在 workspace 未设置时报错，而是静默降级，可能导致 worktree 被创建在错误位置，隐藏配置错误。

**修复方案**：在 workspace 未设置且 BaseDir 为空时返回 error，对齐 Python。

---

### M-30: WorktreeManager.copyIncludeFiles 中 simpleMatch 不等于 Python fnmatch

**Python** (manager.py L453)：`fnmatch.fnmatch(entry, p)` — 完整 Unix glob 模式匹配（支持 `*`, `?`, `[seq]` 等）。
**Go** (manager.go L560-569)：`simpleMatch` — 仅支持 `*` 前缀匹配和精确匹配，不支持 `?`、`[seq]`、`**` 等。

**影响**：`include_patterns: [".env.*"]` 在 Python 中匹配 `.env.local` 等，Go 的 simpleMatch 行为可能不同。

**修复方案**：使用 `filepath.Match` 或第三方 fnmatch 库替换 simpleMatch。

---

### M-31: WorktreeRail.Init 中 lang 和 agentID 硬编码

**Python** (rails.py L115-116)：`lang = agent.system_prompt_builder.language`，`agent_id = getattr(getattr(agent, "card", None), "id", None)` — 从 Agent 动态获取。
**Go** (rails.go L128-129)：`lang := "cn"`，`agentID := ""` — 硬编码。

**修复方案**：从 `agent.SystemPromptBuilder().Language()` 和 `agent.Card().ID` 动态获取。

---

### M-32: WorktreeRail.Uninit 未从 Agent 移除工具

**Python** (rails.py L138-148)：遍历工具从 `agent.ability_manager.remove(name)` 和 `Runner.resource_mgr.remove_tool(tool_id)` 移除。
**Go** (rails.go L161-165)：只清空 `r.tools` 和 `r.manager`，未从 Agent 移除工具。

**修复方案**：在 Uninit 中调用 `agent.AbilityManager().Remove(name)` 和资源管理器移除。

---

### M-33: AutoSetupRail 缺少自定义 commands 参数

**Python** (rails.py L404)：`def __init__(self, commands: list[str] | None = None)` — 支持传入自定义 setup 命令。
**Go** (rails.go L52-53)：`type AutoSetupRail struct{}` — 无 commands 字段。

**修复方案**：在 AutoSetupRail 中增加 `commands []string` 字段和 `WithCommands` Option。

---

### M-34: WorktreeLifecycleRail 接口缺少 4 个 hook

**Python** (rails.py L214-391)：定义了 7 个 hook：before_worktree_create, after_worktree_create, before_worktree_exit, after_worktree_exit, on_worktree_file_write, before_worktree_commit, after_worktree_commit, on_worktree_sync。
**Go** (backend.go L32-41)：只定义了 4 个 hook。

**缺失 hook**：OnWorktreeFileWrite, BeforeWorktreeCommit, AfterWorktreeCommit, OnWorktreeSync。

**修复方案**：补充 4 个 hook 方法定义到 WorktreeLifecycleRail 接口。

---

### M-35: ExitWorktreeTool 缺少 ValidationError 与 RuntimeError 区分

**Python** (tools.py L276-279)：`except ValidationError as e: return ToolOutput(success=False, error=e.message)` — 专门处理 ValidationError。
**Go** (tools.go L176-179)：所有 error 统一返回 `map[string]any{"error": err.Error()}`。

**影响**：Python 中 ExitWorktreeTool 区分 RuntimeError/GitError（失败）和 ValidationError（需要确认），Go 不区分，影响 LLM 的重试/确认逻辑。

---

### M-36: Worktree postCreationSetup 符号链接未指定 target_is_directory

**Python** (manager.py L404)：`os.symlink(src, dst, target_is_directory=True)` — 显式指定目录符号链接。
**Go** (manager.go L483)：`os.Symlink(src, dst)` — 未指定。

**影响**：在 Windows 上，如果目标路径不存在，Go 可能无法正确创建目录符号链接。

---

### M-37: manager.copyFile 不保留元数据

**Python** (manager.py L458)：`shutil.copy2(src, dst)` — 保留元数据（修改时间等）。
**Go** (manager.go L572-578)：`copyFile` 只读写内容，权限固定 0o644。

**修复方案**：使用 `os.Stat` 获取原始权限和修改时间，复制后 `os.Chmod` + `os.Chtimes` 恢复。

---

### M-38: Worktree ValidateSlug 错误消息使用中文

**Python** (slug.py L29)：`"Invalid worktree name: must be ..."` — 英文。
**Go** (slug.go L34)：`"无效的 worktree名称：长度不得超过 ..."` — 中文。

**影响**：Python 对外暴露英文错误消息，Go 用中文，对 LLM 调用方行为不一致。tools.go 中的硬编码消息又是英文，存在内部不一致。

---

### M-39: models.go 中 WorktreeSession.CreationDurationMs 无法区分 None/0

**Python** (models.py L122)：`creation_duration_ms: float | None = None` — 可为 None。
**Go** (models.go L52)：`CreationDurationMs float64` — 零值 0.0，无法区分"未设置"与"0ms"。

**修复方案**：改用 `*float64` 指针类型，nil 表示未设置。

---

## 提示问题 (T)

### T-01: RuntimePromptRail.SetRuntimePaths 空字符串不转 nil

**Python** L79：`self._cwd = cwd.strip() if isinstance(cwd, str) and cwd.strip() else None`
**Go** L134：只 TrimSpace，空字符串保留为空字符串。

---

### T-02: RuntimePromptRail 缺少 ProjectMemoryRail setter（CodeAdapter.updateRuntimeConfig 中 ⤵️）

Go code_adapter.go L534：`// ⤵️ 待后续章节回填: ProjectMemoryRail / _update_tools_for_mode / _update_session_tools`

---

### T-03: AgentServer.handleCancel stream 取消机制差异

**Python**：`task.cancel()` 直接注入 CancelledError。
**Go**：context cancel，依赖下游检查 `ctx.Done()`。
这是 Go/Python 惯用法差异，但需确保所有流式处理路径都检查 context。

---

### T-04: SessionState 缺少 Python ContextVar 的 reset 能力

Python `ContextVar.set()` 返回 Token 可回退，Go `SessionState.SetSessionID` 直接覆盖。
这是 Go 不支持 ContextVar 语义的已知限制。

---

### T-05: SkillEvolutionRail.RunEvolution 异常处理差异

Python：`except Exception as exc: logger.warning(...)` — 捕获所有异常仅 warning。
Go：返回 error，由基类 `safeRunEvolution` 统一处理。功能等价但日志格式不同。

---

### T-06: TeamSkillEvolutionRail.RunEvolution 异常处理差异

Python：完整 try/except 块，捕获后调用 `_emit_background_outcome_event`。
Go：`defer func() { if rec := recover() { ... } }()` 仅捕获 panic，不捕获 error 返回。
Go 的 error 由基类处理，但 Python 中 RunEvolution 内部的非致命错误允许继续执行，Go 可能提前退出。

---

### T-07: buildProgressiveToolRail 返回 nil 不影响功能

Go deep_adapter_rails.go L254-258：`buildProgressiveToolRail` 返回 nil，注释说"由 CreateDeepAgent 内部自动创建"。这是正确的。

---

### T-08: HookExecutor runPromptHook 中 LLM 超时后 goroutine 不会取消

Go executor.go L397-413：LLM 超时使用 goroutine + select + time.After，但超时后已发起的 LLM 调用不会被取消，可能泄漏 goroutine。

**修复方案**：为 LLM 调用传入 context.WithTimeout，使超时后 LLM 客户端也能收到取消信号。

---

### T-09: Slash 命令 /new_session 缺少 SessionStart Hook 触发

Go slash_cmd.go L114：`TODO(#11.7+#11.13): SessionMap 集成 + triggerSessionStartHook`
Python 中 `/new_session` 后触发 `HookEvent.SessionStart`。

---

### T-10: Gateway 层 HookEvent 事件无触发代码

Python 中 `UserPromptSubmit`/`SessionStart`/`SessionEnd`/`Notification`/`ConfigChange`/`InstructionsLoaded`/`Setup` 等 7 个 Gateway 事件虽有常量定义，但 Go 中无代码路径触发它们。

---

### T-11: UserHookRail 只实现了 4 个 Rail 事件回调

Python UserHookRail 的 4 个回调（PreToolUse/PostToolUse/PostToolUseFailure/Stop）Go 已完整实现。
但其他 6 个 Rail 事件（PermissionRequest/PermissionDenied/SubagentStart/SubagentStop/BeforeModelCall/AfterModelCall）没有在 UserHookRail 中拦截。

---

### T-12: embeddings.go 中 Go 增加了 EMBED_BASE_URL/EMBED_API_KEY 别名

Python 只读 `EMBEDDING_BASE_URL`，Go 增加了 `EMBED_BASE_URL` 别名。可能是为了兼容旧配置，但与 Python 行为不一致。

---

### T-13: MockEmbeddingProvider seed 差异

Python：用完整 32 位 hex md5 做 seed。
Go：只用 md5 前 8 字节做 seed。
仅影响测试，不影响生产。

---

### T-14: AgentServer.run 中 stopCh 关闭顺序修复验证

81a4233b 提交将 `running=false` 移入 run() 的 defer 中，在 `close(stopCh)` 之前执行。这是正确的修复——确保 Stop() 中 `<-s.stopCh` 返回时 running 已为 false。

---

### T-15: Worktree gitEnv 中 GIT_ASKPASS 清空而非移除

**Python** (git.py L78-82)：
```python
def _git_env() -> dict[str, str]:
    env = os.environ.copy()
    env["GIT_TERMINAL_PROMPT"] = "0"
    env["GIT_ASKPASS"] = ""  # 设为空字符串
    return env
```

**Go** (git.go)：
```go
func gitEnv() []string {
    env := os.Environ()
    env = append(env, "GIT_TERMINAL_PROMPT=0")
    env = append(env, "GIT_ASKPASS=")  // 同样清空
}
```

Go 实现与 Python 一致（都是设空字符串），但 Python 返回 `dict` 会覆盖同名 key，Go 用 `append` 会**追加**而非覆盖已有的 `GIT_ASKPASS`。如果宿主环境已有 `GIT_ASKPASS=/usr/bin/ssh-askpass`，Go 的环境切片中会同时存在新旧两个值，行为取决于 Git 读取最后一个还是第一个。

**修复方案**：先遍历 `os.Environ()` 移除已有的 `GIT_ASKPASS` 条目，再追加空值版本；或改用 `map[string]string` 构建后转为切片。

---

### T-16: Worktree copyFile 不保留文件权限和元数据

**Python** (manager.py)：`shutil.copy2(src, dst)` — 保留权限和修改时间。
**Go** (manager.go L572-578)：`copyFile` 只 `ReadFile` + `WriteFile` 权限固定 `0o644`。

**影响**：对提示级别归类是因为 include 文件通常是 `.env` 类，权限影响较小。但 `0o644` 使所有 include 文件可读，如果原始文件是 `0o600`（仅 owner 可读），安全属性降级。

**修复方案**：读取原始 `os.Stat` 权限并恢复，同 M-37 修复。

---

### T-17: CleanupStaleWorktrees 使用 local time 而非 UTC

**Python** (cleanup.py L89-90)：
```python
cutoff = datetime.now(tz=timezone.utc) - timedelta(days=config.cleanup_after_days)
cutoff_ts = cutoff.timestamp()
```

**Go** (cleanup.go L68)：
```go
cutoffTime := time.Now().Add(-time.Duration(config.CleanupAfterDays) * 24 * time.Hour)
```

**差异**：Python 使用 UTC 时间，Go 使用本地时间。`os.Stat.ModTime()` 在大多数系统上返回 UTC，但 Go 的 `time.Now()` 是本地时间。在 UTC+8 时区，Go 的 cutoff 会比 Python 晚 8 小时，导致清理更激进（提前 8 小时判定过期）。

**修复方案**：改为 `time.Now().UTC()` 对齐 Python。

---

### T-18: CleanupStaleWorktrees 未并行执行安全检查

**Python** (cleanup.py L111-114)：
```python
changes, unpushed = await asyncio.gather(
    status_porcelain(wt_path),
    has_unpushed_commits(wt_path),
)
```

**Go** (cleanup.go L94-103)：`StatusPorcelain` 和 `HasUnpushedCommits` 串行调用。

**影响**：清理效率较低（每个 worktree 两次 git 子进程串行），但功能等价。对提示级别归类是因为不影响正确性。

**修复方案**：使用 `errgroup` 或双 goroutine 并行执行安全检查。

---

### T-19: EnterWorktreeTool CWD 切换无日志

**Go** (tools.go)：`cwdState.SetCwd(session.WorktreePath)` 和 `cwdState.SetOriginalCwd(session.WorktreePath)` 执行后无日志记录 CWD 切换。

**Python** (tools.py)：`logger.info("CWD switched to worktree: %s", session.worktree_path)`。

**修复方案**：在 CWD 切换后添加 Info 日志，记录 `worktree_path`。

---

### T-20: WorktreeManager.Enter 中 fireRail 未调用 before/after worktree_create

**Python** (manager.py L694-714)：`_fire_rail("before_worktree_create", ...)` 和 `_fire_rail("after_worktree_create", ...)` 在创建前后被调用。
**Go** (manager.go L87-151)：`Enter` 方法中没有任何 `fireRail` 调用。

**影响**：与 S-19（fireRail 未实现）关联。即使 fireRail 实现了，调用点也缺失。此处归类为提示因为 S-19 已覆盖根因，此条目追踪调用点缺失。

**修复方案**：在 `Enter` 中 `backend.Create` 前后添加 `fireRail` 调用；在 `Exit` 中 `removeWorktreeInternal` 前后添加 `fireRail` 调用。

---

## ⤵️/TODO 标记状态确认

| 标记位置 | 标记内容 | 实际状态 |
|---------|---------|---------|
| deep_adapter_rails.go L404-407 | SkillCreateRail ⤵️ 未实现 | ✅ 确认未实现，Python 完整实现 |
| deep_adapter_rails.go L412-415 | StreamEventRail ⤵️ 未实现 | ✅ 确认未实现，Python 完整实现 |
| deep_adapter_rails.go L450-453 | MemoryRail ⤵️ 未实现 | ✅ 确认未实现，Python 完整实现 |
| deep_adapter_rails.go L458-461 | ExternalMemoryRail ⤵️ 未实现 | ✅ 确认未实现，Python 完整实现 |
| deep_adapter_rails.go L494-497 | ResponsePromptRail ⤵️ 未实现 | ✅ 确认未实现，Python 完整实现 |
| deep_adapter.go L747 | slash 命令 ⤵️ 未调用 | ⚠️ 方法已实现但未被调用（应修复） |
| deep_adapter.go L891-898 | Team/AutoHarness ⤵️ | ✅ 确认未实现 |
| deep_adapter.go L1203-1233 | StreamEventRail ⤵️ | ✅ 确认未实现（依赖 StreamEventRail 本身） |
| code_adapter.go L925 | ConfirmInterruptRail ⤵️ | ❌ 标记过时，实际已实现 |
| code_adapter.go L534 | ProjectMemoryRail ⤵️ | ✅ 确认未回填 |
| evolution/context_evolution_rail | 完全缺失 | ✅ 确认未实现（9.24 P6） |
| worktree/manager.go L443-447 | fireRail return nil | ⚠️ 方法存在但完全空实现（S-19） |
| worktree/rails.go L140 | WithWorktreeSessionState ctx 丢弃 | ⚠️ 调用存在但返回值被 `_ =` 丢弃（S-21） |
| worktree/rails.go L128-129 | lang/agentID 硬编码 | ⚠️ 应从 Agent 动态获取（M-31） |
| worktree/tools.go L286-290 | resolveOwner return "" | ⚠️ 应从 opts 提取（S-22） |

---

## 修复优先级建议

### P0（必须立即修复，影响核心功能）

1. **S-19**：WorktreeManager.fireRail 完全未实现 — 整个 Worktree 生命周期 hook 链断裂
2. **S-20**：WorktreeRail.Init 未注册工具到 Agent — enter/exit 工具对 Agent 不可见
3. **S-21**：WithWorktreeSessionState ctx 被丢弃 — WorktreeSession 传播链断裂
4. **S-22**：resolveOwner 硬编码空值 — team 框架无法追踪 worktree 归属
5. **S-01**：DeepAdapter 调用 updateRuntimeConfig — 影响所有请求的运行时配置
6. **S-02**：DeepAdapter 调用 handleSlashCommand — 影响 /evolve 系列命令
7. **S-03**：RuntimePromptRail injectTimeSection cn 模式 en key — 影响中文用户提示词
8. **S-04**：RuntimePromptRail expanduser — 影响 trusted_dirs 安全策略
9. **S-08**：CancelTask 前置幂等校验 — 影响任务取消可靠性
10. **S-10**：CancelSessionTask 等待逻辑 — 影响 goroutine 泄漏

### P1（近期修复，影响功能完整性）

11. **S-05**：RuntimePromptRail Uninit
12. **S-06**：SkillEvolutionRail 冷启动挂载
13. **S-07**：buildAgentRails 顺序
14. **S-09**：SpawnMember 使用 ToDBRef
15. **S-11**：ApprovePlan assignee 校验
16. **S-12**：approvalRuntime 延迟重建
17. **S-13**：detectActiveRequestSignals 异常保护
18. **S-15**：ProjectMemoryRail panic recover
19. **S-18**：leaderMemberName 初始化逻辑
20. **M-17**：移除过时 ⤵️ 标记
21. **M-29**：resolveTargetPath 静默降级应报错
22. **M-30**：simpleMatch 替换为 filepath.Match
23. **M-31**：WorktreeRail lang/agentID 应从 Agent 动态获取
24. **M-32**：WorktreeRail.Uninit 未从 Agent 移除工具
25. **M-34**：WorktreeLifecycleRail 补充 4 个缺失 hook
26. **M-35**：ExitWorktreeTool 缺少 ValidationError 区分

### P2（后续修复，涉及待实现章节或低影响）

27. **S-14**：ContextEvolutionRail 实现
28. **S-16**：ProjectMemory .jiuwen 兼容
29. **S-17**：CancelAllTasks 返回取消数量
30. **M-21~M-27**：StreamEventRail/MemoryRail/Team 等回填
31. **M-33**：AutoSetupRail 缺少 commands 字段
32. **M-36**：symlink target_is_directory
33. **M-37**：copyFile 保留元数据
34. **M-38**：ValidateSlug 错误消息语言不一致
35. **M-39**：CreationDurationMs None/0 语义区分
36. **T-15~T-20**：gitEnv 覆盖/UTC 时间/并行检查/CWD 日志/fireRail 调用点
