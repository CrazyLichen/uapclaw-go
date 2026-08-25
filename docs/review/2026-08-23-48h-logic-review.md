# 48h 逻辑审查报告 (2026-08-23)

> 审查范围：最近 48 小时提交（11 个 commit, 456 文件, +7308/-3005 行）  
> 涉及章节：9.65a-4 TeamBackend、9.51-53 Harness 资源/Schema/Prompts、9.64 Team Memory、9.70-9.80 Evolving 全链路、10.3-10.6 Server/Skill/Extensions、7.x Memory、5.x Context Engine/Session、6.x Callback/Rails/Runner

## 审查统计

| 严重程度 | 数量 | 说明 |
|---------|------|------|
| 严重 | 25 | 功能缺失/逻辑错误，影响正确性 |
| 一般 | 32 | 行为偏差/数据丢失，不影响核心流程但与 Python 不一致 |
| 提示 | 26 | 日志缺失/命名不规范/文档标记过期 |

---

## 一、严重问题（25 个）

### S-01: TeamBackend — ShutdownMember/CancelMember 丢弃 SendMessage 返回值

**模块**: `internal/agent_teams/tools/team_backend.go`  
**章节**: 9.65a-4

**Python 样例** (`openjiuwen/agent_teams/tools/team.py` L574-579):
```python
msg_id = await self.message_manager.send_message(
    content=t("team.shutdown_request_content"),
    to_member_name=member_name,
)
if not msg_id:
    team_logger.warning(f"Failed to send shutdown request message to member {member_name}")
```

**Go 问题** (`team_backend.go` L436, L480):
```go
_, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.shutdown_request_content"), memberName, tb.memberName)
_, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
```

**问题**: Go 用 `_, _` 完全丢弃返回值，消息发送失败被静默吞掉。全文件共有 5 处 `_, _ =` 丢弃返回值（L436, L438, L480, L675, L703）。Python 在 `shutdown_member`、`cancel_member`、`cancel_all_tasks` 中均检查返回值并记录 warning/error。

**修复方案**: 检查 SendMessage/BroadcastMessage 返回的 error，失败时至少记录 warning 日志，对齐 Python。CancelMember 中消息发送失败时应返回 `MemberOpResultFail`，对齐 Python 的 `return False` 行为。

---

### S-02: TeamBackend — ApprovePlan 签名缺少 approved/feedback 参数

**模块**: `internal/agent_teams/tools/team_backend.go`  
**章节**: 9.65a-4

**Python 样例** (`team.py` L398-403):
```python
async def approve_plan(self, plan_id: str, approved: bool = True, feedback: Optional[str] = None) -> bool:
```

**Go 问题** (`team_backend.go` L711):
```go
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) atschema.MemberOpResult {
    err := tb.taskManager.ApprovePlan(ctx, taskID, true, "")  // 硬编码 approved=true, feedback=""
```

**问题**: Go 签名只有 `taskID`，缺少 `approved` 和 `feedback` 参数，硬编码 `approved=true`。**无法拒绝计划**，功能缺失。Python 中 `plan_id` 语义与 Go 的 `taskID` 也不一致。

**修复方案**: 签名改为 `ApprovePlan(ctx context.Context, planID string, approved bool, feedback string) atschema.MemberOpResult`。

---

### S-03: TeamBackend — CancelAllTasks 返回类型与 Python 不一致

**模块**: `internal/agent_teams/tools/team_backend.go`  
**章节**: 9.65a-4

**Python 样例** (`team.py` L898-935):
```python
async def cancel_all_tasks(self, skip_assignees=None) -> int:
    ...
    return len(cancelled_tasks)
```

**Go 问题** (`team_backend.go` L695):
```go
func (tb *TeamBackend) CancelAllTasks(ctx context.Context, skipAssignees []string) atschema.MemberOpResult {
```

**问题**: Python 返回 `int`（取消的任务数），Go 返回 `MemberOpResult`。调用方无法获知取消了多少任务。

**修复方案**: 改为返回 `(int, error)` 或在 `MemberOpResult.Reason` 中记录取消数量。

---

### S-04: TeamBackend — CancelMember 消息发送失败仍返回 Success

**模块**: `internal/agent_teams/tools/team_backend.go`  
**章节**: 9.65a-4

**Python 样例** (`team.py` L645-650):
```python
success = await self.message_manager.send_message(
    content=t("team.cancel_request_content"), to_member_name=member_name
)
if not success:
    team_logger.error(f"Failed to send cancel request message to member {member_name}")
    return False
```

**Go 问题** (`team_backend.go` L479-487):
```go
_, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
// ... 即使 SendMessage 失败，也返回 Success
return atschema.NewMemberOpResultSuccess()
```

**问题**: Python 在消息发送失败时返回 `False`，Go 无论消息是否发送成功都返回 `Success`。

**修复方案**: 检查 SendMessage 返回值，失败时返回 `MemberOpResultFail`。

---

### S-05: TeamBackend — CleanTeam 对 ERROR 状态处理与 Python 不一致

**模块**: `internal/agent_teams/tools/team_backend.go`  
**章节**: 9.65a-4

**Python 样例** (`team.py` L680-688):
```python
if member_data.status != MemberStatus.SHUTDOWN.value:
    all_shutdown = False
    break
```

**Go 问题** (`team_backend.go` L599-605):
```go
if m.Status != string(atschema.MemberStatusShutdown) &&
    m.Status != string(atschema.MemberStatusError) {
    // 无法清理
}
```

**问题**: Python 只允许 SHUTDOWN 状态通过清理检查，ERROR 状态也算"未关闭"。Go 额外允许了 ERROR 状态，行为更宽松。

**修复方案**: 去掉对 `MemberStatusError` 的豁免，只允许 SHUTDOWN，对齐 Python。

---

### S-06: TeamBackend — ForceCleanTeam 多余的 onTeamCleaned 回调 + 忽略 DB 返回值

**模块**: `internal/agent_teams/tools/team_backend.go`  
**章节**: 9.65a-4

**Python 样例** (`team.py` L729-761):
```python
async def force_clean_team(self, shutdown_members=True):
    success = await self.db.force_delete_team_session(self.team_name)
    # on_team_cleaned 不在此路径调用
    if success: team_logger.info(...)
    return success
```

**Go 问题** (`team_backend.go` L647-658):
```go
tb.db.ForceDeleteTeamSession(ctx, tb.teamName)  // 返回值被忽略
if tb.onTeamCleaned != nil {                     // Python 不调用此回调
    if err := tb.onTeamCleaned(ctx); err != nil { ... }
}
return true, nil  // 无条件返回 true
```

**问题**: (1) Python 的 `force_clean_team` **不调用** `on_team_cleaned` 回调（只在 `clean_team` 正常路径触发），Go 调用了，可能导致回调被重复触发。(2) `ForceDeleteTeamSession` 返回值被忽略，Python 根据结果返回 bool。(3) Go 无条件返回 `true`。

**修复方案**: 移除 ForceCleanTeam 中的 onTeamCleaned 回调调用；检查 ForceDeleteTeamSession 返回值；根据结果返回相应值。

---

### S-07: TeamAgent — ShutdownSelf 缺少 CloseStream 调用

**模块**: `internal/agent_teams/agent/team_agent.go`  
**章节**: 9.55

**Python 样例** (`team_agent.py` L613-626):
```python
async def shutdown_self(self) -> None:
    await self._stream_controller.cooperative_cancel()
    if self._state.team_member is not None:
        try:
            await self._state.team_member.update_status(MemberStatus.SHUTDOWN)
        except Exception as e: ...
    self._close_stream()  # ← 关键：向 streamQueue 写入 nil sentinel
```

**Go 问题** (`team_agent.go` L636-646):
```go
func (a *TeamAgent) ShutdownSelf(ctx context.Context) error {
    if a.streamController != nil {
        _ = a.streamController.CooperativeCancel(ctx)
    }
    _ = a.UpdateStatus(ctx, atschema.MemberStatusShutdown)
    return nil  // ← 缺少 CloseStream() 调用
}
```

**问题**: Go 缺少 `CloseStream()` 调用。Python 中 `_close_stream` 向 streamQueue 写入 nil sentinel 使外层循环退出。没有这个调用，stream 会永远阻塞，teammate 无法优雅退出。

**修复方案**: 在 `UpdateStatus` 后添加 `a.streamController.CloseStream()`。

---

### S-08: TeamAgent — LookupHumanAgentRuntime 缺少 is_human_agent 检查

**模块**: `internal/agent_teams/agent/team_agent.go`  
**章节**: 9.55

**Python 样例** (`team_agent.py` L268-280):
```python
def lookup_human_agent_runtime(self, member_name: str) -> Optional["TeamAgent"]:
    backend = self._configurator.team_backend
    if backend is None or not backend.is_human_agent(member_name):
        return None
    return self._spawn_manager.lookup_inprocess_agent(member_name)
```

**Go 问题** (`team_agent.go` L401-417):
```go
func (a *TeamAgent) LookupHumanAgentRuntime(memberName string) *TeamAgent {
    if a.spawnManager == nil { return nil }
    agent := a.spawnManager.LookupInprocessAgent(memberName)
    // ← 缺少 backend.IsHumanAgent(memberName) 检查
```

**问题**: Go 直接查找 spawnManager，可能返回非 human-agent 的 agent。

**修复方案**: 在 `LookupInprocessAgent` 前增加 `backend.IsHumanAgent(memberName)` 检查。

---

### S-09: RuntimeManager — handleInteractiveInput 为 stub，始终返回 success

**模块**: `internal/agent_teams/runtime/manager.go`  
**章节**: 9.59

**Python 样例** (`manager.py` L386-390):
```python
if isinstance(payload, InteractiveInput):
    if entry.agent.has_pending_interrupt():
        await entry.agent.resume_interrupt(payload)
        return DeliverResult.success(None)
    return DeliverResult.failure("unsupported_interactive_input")
```

**Go 问题** (`manager.go` L232-237):
```go
func (m *TeamRuntimeManager) handleInteractiveInput(entry *ActiveTeam, input *sessioninteraction.InteractiveInput) (*interaction.DeliverResult, error) {
    // ⤵️ 待 9.55 回填: 完整实现
    return interaction.NewDeliverResultSuccess(nil), nil  // 始终返回 success
}
```

**问题**: Python 在 `has_pending_interrupt()` 为 False 时返回 `failure("unsupported_interactive_input")`，Go 始终返回 success。InteractiveInput 被静默丢弃，调用方误以为投递成功。

**修复方案**: 实现 `entry.Agent.HasPendingInterrupt()` 检查，不存在时返回 `DeliverResultFailure`。

---

### S-10: RuntimeManager — agentLookup 和 AutoStart 均为 stub

**模块**: `internal/agent_teams/runtime/manager.go`  
**章节**: 9.59

**Python 样例** (`manager.py` L453-473):
```python
if isinstance(payload, OperatorMessage):
    inbox = UserInbox(backend.message_manager)
    if payload.target is None:
        await agent.auto_start_all()
        result = await inbox.broadcast(payload.body)
        return result
    await agent.auto_start_member(payload.target)
    result = await inbox.direct(payload.target, payload.body)
    return result
```

**Go 问题** (`manager.go` L308-328):
```go
case *interaction.OperatorMessage:
    if p.Target() == nil {
        // ⤵️ 待 9.55 回填: agent.AutoStartAll()
        return inbox.Broadcast(p.Body())
    }
    // ⤵️ 待 9.55 回填: agent.AutoStartMember(*p.Target())
    return inbox.Direct(*p.Target(), p.Body())
```

**问题**: (1) 广播/点对点发送前未启动目标成员，消息可能被已停止的成员错过。(2) `agentLookup` 传入 nil，导致 `HumanAgentInbox.driveAgent()` 永远返回 `agent_unavailable`，HITT LLM 驱动路径完全不可用。

**修复方案**: 回填 `agent.AutoStartAll()`/`agent.AutoStartMember()`；传入 `entry.Agent.LookupHumanAgentRuntime` 作为 agentLookup。

---

### S-11: HumanAgentInbox — driveAgent 缺少 DeliverInput 调用

**模块**: `internal/agent_teams/interaction/human_agent_inbox.go`  
**章节**: 9.59

**Python 样例** (`human_agent_inbox.py` L217-229):
```python
async def _drive_agent(self, body: str, *, sender: str) -> DeliverResult:
    if self._agent_lookup is None:
        return DeliverResult.failure("agent_unavailable")
    agent = self._agent_lookup(sender)
    if agent is None:
        return DeliverResult.failure("agent_unavailable")
    await agent.deliver_input(body)
    return DeliverResult.success(None)
```

**Go 问题** (`human_agent_inbox.go` L195-217):
```go
func (h *HumanAgentInbox) driveAgent(body string, sender string) (*DeliverResult, error) {
    ...
    // ⤵️ 待 9.55 回填: agent.DeliverInput(ctx, body)
    _ = agent
    return NewDeliverResultSuccess(nil), nil
}
```

**问题**: `agent.DeliverInput(ctx, body)` 未被调用，HumanAgent 的 LLM 驱动完全无效。

**修复方案**: 回填 `agent.DeliverInput(ctx, body)` 调用。

---

### S-12: TeamMember — Status/UpdateStatus/UpdateExecutionStatus 全部为 stub

**模块**: `internal/agent_teams/agent/member.go`  
**章节**: 9.65

**Python 样例** (`member.py` L76-141): 完整实现了读取旧状态 → None 检查短路 → 等值短路 → DB 写入 → 事件发布。

**Go 问题** (`member.go` L49-71):
```go
func (m *TeamMember) Status(ctx context.Context) (atschema.MemberStatus, error) {
    return atschema.MemberStatusReady, nil  // 始终返回 Ready
}
func (m *TeamMember) UpdateStatus(ctx context.Context, newStatus atschema.MemberStatus) (bool, error) {
    logger.Info(logComponent).Str("new_status", string(newStatus)).Msg("TeamMember 状态更新")
    return true, nil  // 不做任何 DB 操作
}
```

**问题**: Python 中 `status()` 当成员行不存在时返回 `None`，Go 始终返回 `Ready`。UpdateStatus 不做 DB 写入和事件发布。影响：(1) `StreamController.runOneRound` 中 `SHUTDOWN_REQUESTED` 分支永远不触发（S-13 关联）(2) 成员状态变更事件不发布，其他进程无法感知状态变更。

**修复方案**: TODO 已标注 #9.65，回填时必须实现完整的三段逻辑（读旧值→短路→写DB→发事件），并调整 Status 返回值语义（支持 nil/不存在）。

---

### S-13: StreamController — TeamMember.Status() stub 导致 SHUTDOWN_REQUESTED 分支永远不触发

**模块**: `internal/agent_teams/agent/stream_controller.go`  
**章节**: 9.60

**Go 问题** (`stream_controller.go` L557-569):
```go
// ⤵️ 待 #9.65 TeamMember.Status() 实现后替换为真实状态检查
memberStatus := atschema.MemberStatusReady  // ← 因为 Status() 是 stub
if sc.state != nil && sc.state.TeamMember != nil {
    status, _ := sc.state.TeamMember.Status(ctx)
    memberStatus = status  // 永远是 Ready
}
if memberStatus == atschema.MemberStatusShutdownRequested {
    sc.CloseStream()  // ← 永远不会触发
}
```

**问题**: Python 中这是关键路径：当 teammate 被请求关闭时，轮次结束后检查状态并关闭流。当前 Go 的 teammate 无法通过此路径优雅关闭。

**修复方案**: 需在 S-12 回填 TeamMember.Status() 后验证此分支能正常触发。

---

### S-14: AgentConfigurator — UpdateModelPool 缺少 inherit_pool_ids 合并逻辑

**模块**: `internal/agent_teams/agent/agent_configurator.go`  
**章节**: 9.64

**Python 样例** (`agent_configurator.py` L572-579):
```python
def update_model_pool(self, new_pool: list) -> None:
    merged = inherit_pool_ids(self.ctx.team_spec.model_pool, list(new_pool))
    self.ctx.team_spec.model_pool = merged
    self.model_allocator = build_model_allocator(self.spec, self.ctx.team_spec)
```

**Go 问题** (`agent_configurator.go` L409-423):
```go
func (c *AgentConfigurator) UpdateModelPool(newPool []models.ModelPoolEntry) {
    allocator := models.BuildModelAllocatorForPool(newPool, strategy, teamName)
    // ← 直接用新池创建分配器，丢失旧池 allocation_id
```

**问题**: Python 先用 `inherit_pool_ids` 合并旧池和新池（保留旧池中带 allocation_id 的条目），再更新 spec 和分配器。Go 直接用新池创建分配器，**丢失了旧池中已分配条目的 allocation_id**。模型池动态更新后，已分配的模型引用会断裂。

**修复方案**: 实现 `InheritPoolIDs` 函数，合并旧池和新池，更新 `TeamSpec.ModelPool`，再用合并后的池构建分配器。

---

### S-15: AgentConfigurator — BuildMemoryManager 缺少 spec.memory.enabled 检查

**模块**: `internal/agent_teams/agent/agent_configurator.go`  
**章节**: 9.64

**Python 样例** (`agent_configurator.py` L458-459):
```python
if not (spec.memory and spec.memory.enabled):
    return None
```

**Go 问题** (`agent_configurator.go` L386-403):
```go
func (c *AgentConfigurator) BuildMemoryManager(...) *memory.TeamMemoryManager {
    // 无条件创建
    return memory.NewTeamMemoryManager(params)
}
```

**问题**: Python 在 `spec.memory.enabled` 为 False 时返回 None，不创建 TeamMemoryManager。Go 无条件创建，导致非记忆团队也创建无用管理器，且缺少 lifecycle 条件的字段设置（temporary/persistent 差异）。

**修复方案**: 添加 `spec.Memory != nil && spec.Memory.Enabled` 检查，不满足时返回 nil。

---

### S-16: TrajectoryExtractor — buildToolDetail 中 toolName 始终为空字符串

**模块**: `internal/evolving/trajectory/extractor.go`  
**章节**: 9.77

**Python 样例** (`extractor.py` L145-175):
```python
def _build_tool_detail(self, span: Any) -> ToolCallDetail:
    tool_name = getattr(span, "name", "") or ""
```

**Go 问题** (`extractor.go` L257-283):
```go
func (e *TracerTrajectoryExtractor) buildToolDetail(span *tracer.Span) *ToolCallDetail {
    toolName := ""  // ← 始终为空！
```

**问题**: Go 没有从 span 获取 `Name` 字段。导致所有 tool call 的 `ToolName` 为空，后续 `isCollaborativeStep` 和 `isTeamSkillFileAccess` 全部失效。轨迹分析完全无法识别工具调用。

**修复方案**: 将 `buildDetail` 签名改为接收 `*tracer.TraceAgentSpan`，或额外传入 tool name 参数。

---

### S-17: TrajectoryExtractor — buildMeta 缺少 agent_id 注入

**模块**: `internal/evolving/trajectory/extractor.go`  
**章节**: 9.77

**Python 样例** (`extractor.py` L186-202):
```python
agent_id = getattr(span, "agent_id", None) or base_meta.get("agent_id")
if agent_id:
    meta["agent_id"] = agent_id
```

**Go 问题** (`extractor.go` L288-318): 缺少 `agent_id` 的注入步骤。

**修复方案**: 在 `buildMeta` 中添加 `span.AgentID` → `meta["agent_id"]` 注入逻辑。

---

### S-18: TrajectoryExtractor — parseLLMResponse 不处理结构体类型

**模块**: `internal/evolving/trajectory/extractor.go`  
**章节**: 9.77

**Python 样例** (`extractor.py` L204-213):
```python
@staticmethod
def _parse_llm_response(outputs: Any) -> Optional[Dict[str, Any]]:
    if isinstance(outputs, dict): return outputs
    if hasattr(outputs, "model_dump"): return outputs.model_dump()
    if hasattr(outputs, "__dict__"): return outputs.__dict__
    return None
```

**Go 问题** (`extractor.go` L386-396):
```go
func parseLLMResponse(outputs any) map[string]any {
    switch v := outputs.(type) {
    case map[string]any: return v
    default: return nil  // ← 结构体类型直接丢弃
    }
}
```

**问题**: 只处理 `map[string]any`，Python 还会尝试 `model_dump()` 和 `__dict__`。如果 LLM 响应是结构体类型（如 `AssistantMessage`），Go 直接返回 nil。

**修复方案**: 添加 JSON marshal/unmarshal 兜底（等价于 Python 的 model_dump）。

---

### S-19: TeamSkillExperienceOptimizer — GenerateUserPatch/GenerateTrajectoryPatch 不加载真实技能数据

**模块**: `internal/evolving/optimizer/skill_call/team_optimizer.go`  
**章节**: 9.72d

**Python 样例** (`team_skill_experience_optimizer.py` L371-409):
```python
skill_content = await self._load_skill_content(skill_name)
existing_evolutions = await self._load_existing_evolutions_summary(skill_name)
```

**Go 问题** (`team_optimizer.go` L343-453):
```go
skillContent := summarizeSkillContentTeamFallback(skillName, o.language)  // 永远返回 "无"
existingEvolutions := langDefault("无已有演进经验", ...)  // 永远返回占位文本
```

**问题**: Go 构造函数接收 `evolutionStore`，但 `generateUserPatch` 和 `generateTrajectoryPatch` 都没调用它。LLM 优化器使用不完整的上下文（硬编码占位文本），导致生成的演进记录质量极低。

**修复方案**: 当 `o.evolutionStore` 非空时，调用 `ReadSkillContent()` 和 `LoadFullEvolutionLog()` 获取真实数据。

---

### S-20: spawn/child.go — RunSpawnedProcess 中 SetConfig 错误只记日志不中止

**模块**: `internal/agentcore/runner/spawn/child.go`  
**章节**: 6.28

**Python 样例** (`child_process.py` L456-458):
```python
Runner.set_config(deserialize_runner_config(spawn_agent_config.runner_config))
await Runner.start()  # set_config 抛异常时不会执行
```

**Go 问题** (`child.go` L87-93):
```go
if err := childRunner.SetConfig(spawnAgentConfig.RunnerConfig); err != nil {
    logger.Error(...).Msg("设置 Runner 配置失败")
    // ← 继续执行 Start()！
}
```

**问题**: SetConfig 失败后继续执行 `Start()`，子进程将以错误配置运行，行为不可预测。

**修复方案**: SetConfig 失败时应 `return fmt.Errorf("设置 Runner 配置失败: %w", err)`。

---

### S-21: spawn/child.go — DONE/ERROR 消息未传播输入消息的 message_id

**模块**: `internal/agentcore/runner/spawn/child.go`  
**章节**: 6.28

**Python 样例** (`child_process.py` L216-230):
```python
done_message = Message(type=MessageType.DONE, payload={"result": result}, message_id=message_id)
error_message = Message(type=MessageType.ERROR, payload={"error": str(e)}, message_id=message_id)
```

**Go 问题** (`child.go` L385-388):
```go
doneMsg := NewMessage(MessageTypeDone, map[string]any{"result": result})
// NewMessage() 生成新的 message_id，而非传播 msg.MessageID
```

**问题**: 请求-响应关联关系丢失，父进程无法匹配响应。`HandleHealthCheck`、`HandleShutdown` 同样缺失。

**修复方案**: 响应消息应设置 `MessageID` 为输入消息的 `MessageID`，或增加 `NewMessageWithID()` 工厂函数。

---

### S-22: callback/chain.go — error_handler 返回 ChainActionContinue 时追加错误的 result

**模块**: `internal/agentcore/runner/callback/chain.go`  
**章节**: 6.24

**Python 样例** (`chain.py` L171-176):
```python
error_result = await self.error_handlers[callback](e, context)
if error_result:
    context.results.append(error_result)  # 使用 error_handler 的返回值
```

**Go 问题** (`chain.go` L176-181):
```go
case ChainActionContinue:
    cctx.Results = append(cctx.Results, result)  // result 是失败回调的返回值，可能为 nil
```

**问题**: Go 追加的是 `result`（失败回调的返回值，可能为 nil），而 Python 追加的是 `error_result`（error_handler 的返回值）。链式上下文中的结果数据不正确。

**修复方案**: error_handler 在 ChainActionContinue 时应使用 error_handler 提供的返回值，而非失败的回调结果。

---

---

### S-23: VerificationRail — BeforeModelCall 缺少 enable_task_loop 和 plan_mode 守卫

**模块**: `internal/agentcore/harness/rails/subagent/verification_rail.go`
**章节**: 9.29

**Python 样例** (`verification_rail.py` L125-153):
```python
async def before_model_call(self, ctx: AgentCallbackContext) -> None:
    deep_config = getattr(self._agent, "_deep_config", None)
    if deep_config is None or not deep_config.enable_task_loop:
        return  # 仅在 task loop 激活时注入
    if ctx.session is not None:
        state = self._agent.load_state(ctx.session)
        if getattr(state.plan_mode, "mode", None) == "plan":
            return  # plan mode 下跳过注入
```

**Go 问题** (`verification_rail.go` L170-195):
```go
func (r *VerificationRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    if r.promptBuilder == nil { return nil }
    // 直接构建 section 并注入，没有 enable_task_loop / plan_mode 守卫
    section := saprompt.PromptSection{...}
    r.promptBuilder.AddSection(section)
    return nil
}
```

**问题**: Go 无条件注入验证约束提醒，在非 task loop 场景和 plan mode 下也会注入，增加 LLM 噪音。Go 的 VerificationRail 结构体缺少 `_agent` 字段，无法获取 deepConfig 和 agent state。

**修复方案**: 在 VerificationRail 中增加 agent 字段，BeforeModelCall 中增加 `deepConfig.EnableTaskLoop` 和 `state.PlanMode.Mode` 两个守卫。

---

### S-24: SubagentRail — 缺少 Uninit 方法

**模块**: `internal/agentcore/harness/rails/subagent/subagent_rail.go`
**章节**: 9.29

**Python 样例** (`subagent_rail.py` L83-102):
```python
def uninit(self, agent) -> None:
    if self.tools and hasattr(agent, "ability_manager"):
        for tool in self.tools:
            name = getattr(tool.card, "name", None)
            if name: agent.ability_manager.remove(name)
            tool_id = tool.card.id
            if tool_id: Runner.resource_mgr.remove_tool(tool_id)
    if self.enable_async_subagent:
        agent.set_session_toolkit(None)
```

**Go 问题**: SubagentRail 完全缺少 `Uninit` 方法。Python 中会从 ability_manager 移除工具、从 resource_mgr 移除工具、清空 session_toolkit。Go 注册的工具在 rail 卸载后不会被清理，造成资源泄漏和工具残留。

**修复方案**: 实现 `Uninit(agent)` 方法，包含工具注销逻辑和 `SetSessionToolkit(nil)` 清理。

---

### S-25: TodoItem — ActiveForm JSON tag 不一致导致序列化不兼容

**模块**: `internal/agentcore/harness/schema/task.go`
**章节**: 9.51-53

**Python 样例** (`task.py` L56): Pydantic 序列化输出 `"activeForm"` (camelCase)

**Go 问题** (`task.go` L22):
```go
ActiveForm string json:"active_form"  // JSON marshal 输出 "active_form" (snake_case)
```

**问题**: `ToDict()` 方法使用 `"activeForm"`（与 Python 一致），但 `json.Marshal`/`json.Unmarshal` 使用 `"active_form"`。通过 JSON 路径序列化/反序列化的 TodoItem 与 Python 端数据不兼容。

**修复方案**: 将 JSON tag 改为 `json:"activeForm"` 以与 Python 保持一致。

---

## 二、一般问题（32 个）

（以下为补充发现，S-01~S-22 对应的 G-01~G-24 已在上方列出）

### G-25: VerificationContractRail — Init 缺少保存 agent 引用

**模块**: `internal/agentcore/harness/rails/subagent/verification_contract_rail.go`

Python `init` 中 `self._agent = agent`，Go 没有保存 agent 引用。当前不影响功能，但与 Python 对齐声明不一致，未来扩展可能需要。

---

### G-26: HeartbeatRail — BeforeModelCall 缺少 RemoveSection 先行调用

**模块**: `internal/agentcore/harness/rails/heartbeat.go`

Go 总是 `AddSection` 但从不先 `RemoveSection`。如果多次触发 `BeforeModelCall`（心跳是周期性调用），会导致同名 section 重复累积。Python 和其他 Rail（VerificationRail、VerificationContractRail）都使用 `remove` + `add` 模式。

**修复方案**: 在 `AddSection` 之前增加 `sb.RemoveSection(sections.SectionHeartbeat)`。

---

### G-27: SubagentRail — 工具注册委托 factory.go 与 Python 不一致

**模块**: `internal/agentcore/harness/rails/subagent/subagent_rail.go`

Python 的 Rail 自行注册工具（`Runner.resource_mgr.add_tool` + `agent.ability_manager.add`），Go 的 Rail 只持有引用，由 factory.go 统一处理。改变了注册时机和职责归属。

**修复方案**: 确认 factory.go 确实统一处理了注册，或让 SubagentRail 自行注册。

---

### G-28: AskUserRail/McpRail — 工具注册顺序与 Python 相反

**模块**: `internal/agentcore/harness/rails/interrupt/ask_user_rail.go`, `internal/agentcore/harness/rails/mcp_rail.go`

Python 先 `Runner.resource_mgr.add_tool`，后 `agent.ability_manager.add`。Go 先 `am.Add`，后 `resourceMgr.AddTool`。如果 ResourceMgr.add_tool 有副作用，注册顺序差异可能导致问题。

**修复方案**: 将 Go 代码中的注册顺序改为先 AddTool 后 am.Add。

---

### G-29: TaskPlan.ToMarkdown 格式与 Python 不一致

**模块**: `internal/agentcore/harness/schema/task.go`

Python: `## Goal: {goal}` + `- [>] content`。Go: `# Task Plan\n\n**Goal:** {goal}` + `- [→] content`。使用了不同的 markdown 标题级别、Goal 格式和进度图标。

**修复方案**: 对齐 Python 格式，使用 `## Goal:` 二级标题、`[>]` 替代 `[→]`。

---

### G-30: FromDictEvolutionPatch 对无效 target 静默回退

**模块**: `internal/evolving/checkpointing/types.go`

Python `EvolutionTarget(raw_target)` 无效值会抛 `ValueError`，Go 对无效 target 静默回退不返回 error。注释说"对齐 Python: 不验证"，但 Python 实际会验证并报错。

**修复方案**: 将无效 target 也返回 error，或修正注释说明为何有意回退。

---

### G-31: ConversationSignalDetector.Detect 不支持 Trajectory 统一入口

**模块**: `internal/evolving/signal/`

Python `detect()` 接受 `Union[Trajectory, List[dict]]`，自动判断类型。Go `Detect()` 只接受 `[]map[string]any`，调用者需手动调用 `ConvertTrajectoryToMessages` + `DetectTrajectorySignals`。

**修复方案**: 增加 `DetectFromTrajectory(traj *trajectory.Trajectory)` 便捷方法。

---

### G-32: Evolving sharing 子包完全缺失

**模块**: `internal/evolving/sharing/` — 目录不存在

Python 有完整的 `openjiuwen/agent_evolving/sharing/` 子包（共享后端、经验共享器、Hub 客户端、关键词提取等）。Go 端完全没有。需确认是否已在计划中标记。

---

## 三、提示问题（26 个）

（以下为补充发现）

### T-19: McpRail.Uninit 使用 recover 代替前置 nil 检查

**模块**: `internal/agentcore/harness/rails/mcp_rail.go`

Python 用 `if name and hasattr(agent, "ability_manager")` 条件检查，Go 用 `defer+recover`。Go 的 recover 可能掩盖真正的 bug（如 Card() 返回 nil 导致 nil pointer panic）。

**修复方案**: 在 `am.Remove` 和 `resourceMgr.RemoveTool` 前增加 `t.Card() != nil` 检查。

---

### T-20: TaskLoopEventHandler 中 UUID 格式差异

**模块**: `internal/agentcore/harness/task_loop/handler.go`

Python `uuid.uuid4().hex` 返回 32 字符无连字符，Go `uuid.New().String()` 返回 36 字符带连字符。task_id 格式不同，可能导致与 Python 系统交互时的兼容性问题。

**修复方案**: 使用 `strings.ReplaceAll(uuid.New().String(), "-", "")` 或直接用 hex 编码。

---

### T-21: TaskLoopController.DrainFollowUp 返回 nil vs 空列表

**模块**: `internal/agentcore/harness/task_loop/controller.go`

Python `return []`，Go `return nil`。JSON 序列化时 `nil` → `null`，`[]` → `[]`。

**修复方案**: 改为 `return []string{}`。

---

### T-22: AskUserRail.parseUserInput 空字符串路径与 Python 不一致

**模块**: `internal/agentcore/harness/rails/interrupt/ask_user_rail.go`

Python 空字符串直接 interrupt，Go 空字符串走 `&AskUserPayload{}, true` 间接路径。功能结果相同，但路径不同。

---

### T-23: ModelUsageRecord.String() 格式差异

**模块**: `internal/agentcore/harness/schema/task.go`

Python: `model_id={model_id} input={input} output={output}`，Go: `{model_id}: input={input}, output={output}`。

---

### T-24: FromDictEvolutionLog 跳过失败记录的注释不准确

**模块**: `internal/evolving/checkpointing/types.go`

注释说"对齐 Python: 不因单条记录解析失败中断"，但 Python 实际会让整个 from_dict 失败。Go 的容错行为更安全，但注释与实际行为不一致。

**修复方案**: 修正注释为"Go 增强：单条记录解析失败时跳过而非整体失败"，并添加 Debug 日志。

---

### T-25: EvolutionPatch keywords/summary 未参与序列化

**模块**: `internal/evolving/checkpointing/types.go`

Python 和 Go 一致地遗漏了 `keywords` 和 `summary` 的序列化/反序列化。两端都需补充。

---

### T-26: BrowserAgent 多处 ⤵️ 回填项

**模块**: `internal/agentcore/harness/tools/browser_move/runtime.go`

- 缺少 `ensure_browser_runtime_client_patch` 调用
- 缺少 controller 初始化
- 缺少 code executor 绑定和 `register_builtin_actions`
- 缺少 `_register_runtime_tool` 和 `ability_manager.add`
- 缺少 `site_profiles`、`selector_cache` 和 `record_card_probe_result`

均标记为 `⤵️ 9.38-49 回填`，属于计划中的未完成项。

### G-01: TeamBackend — ShutdownMember CAS 错误信息丢失诊断数据

**模块**: `internal/agent_teams/tools/team_backend.go`  
**章节**: 9.65a-4

Python 在校验失败时返回带当前状态信息的 fail reason（`cannot shut down from status 'xxx'`），Go 的 fail reason 是通用的 `CAS transition failed`，丢失了诊断信息。

**修复方案**: CAS 失败时记录当前状态和目标状态，如 `CAS transition failed for member X: current=READY, target=SHUTDOWN_REQUESTED`。

---

### G-02: TeamBackend — Startup 跳过自身与 Python 不一致

**模块**: `internal/agent_teams/tools/team_backend.go` L362

Python 的 `startup()` 不跳过自身，Go 额外跳过了 `tb.memberName`。虽然 leader 在 BuildTeam 时已设为 BUSY（不会被查到），但如果未来 leader 状态变为 UNSTARTED（如恢复场景），Go 会跳过而 Python 不会。

**修复方案**: 去掉 `if m.MemberName == tb.memberName { continue }` 或添加注释说明这是有意偏差。

---

### G-03: TeamBackend — BuildTeam 注册 Leader 绕过 SpawnMember 去重检查

**模块**: `internal/agent_teams/tools/team_backend.go` L535

Go 直接调用 `CreateMember`（绕过 `SpawnMember`），缺少"成员已存在"的幂等检查。Python 的 `spawn_member` 有去重检查。

**修复方案**: 在直接调用 CreateMember 前加上去重检查，或给 SpawnMember 添加 status/execution_status 可选参数。

---

### G-04: TeamBackend — SpawnHumanAgent 传空字符串作为 agentCard

**模块**: `internal/agent_teams/tools/team_backend.go` L766

Python 构造了完整的 AgentCard 对象传给 spawn_member，Go 传了空字符串 `""`。DB 中 human-agent 成员的 agent_card 列为空，Python 中则存储了完整 JSON。

**修复方案**: SpawnHumanAgent 应构造 agentCard JSON，如 `{"id":"team_member","name":"xxx","description":"yyy"}`。

---

### G-05: TeamBackend — CancelTask 缺少"已取消"幂等检查

**模块**: `internal/agent_teams/tools/team_backend.go` L665

Python 在 cancel_task 入口做了"已取消"幂等短路返回 True，Go 依赖底层 FSM 报错返回 fail。幂等语义不同。

**修复方案**: 在 CancelTask 入口增加"已取消"幂等检查，返回 success。

---

### G-06: TeamAgent — UpdateStatus 绕过 TeamMember 验证逻辑

**模块**: `internal/agent_teams/agent/team_agent.go` L365-376

Python 委托给 `team_member.update_status()`（含 None 检查 + 等值短路 + 事件发布），Go 直接调用 `backend.DB().Member().UpdateMemberStatus()`，绕过了验证和事件发布，且返回值未检查。

**修复方案**: 应委托 `a.state.TeamMember.UpdateStatus(ctx, status)`。

---

### G-07: HumanAgentInbox — BroadcastMessage 空消息 ID 语义差异

**模块**: `internal/agent_teams/interaction/human_agent_inbox.go` L112-118

Python `broadcast_message` 返回 None 表示失败，Go 中 `err == nil && msgID == ""` 时返回 success(nil)，Python 返回 failure。

**修复方案**: 检查 `msgID == ""` 时返回 `DeliverResultFailure("broadcast_failed")`。

---

### G-08: SkillToolkit — skillnet score 默认值 Go nil vs Python 0

**模块**: `internal/swarm/agents/harness/tools/skill_toolkit.go` L679

Python `item.get("stars", 0)` 缺失时默认 0，Go `toIntPtr(item["stars"])` 缺失时返回 nil。

**修复方案**: skillnet 来源的 stars 字段，缺失时默认设为 `*int(0)`。

---

### G-09: DiffService — agentID 命名不一致

**模块**: `internal/swarm/server/utils/diff_service.go` L139

Adapter 层已用 `"uapclaw"`，DiffService 仍用旧名 `"jiuwenswarm"`。SkillManager 临时目录前缀也用旧名。

**修复方案**: 统一为 `"uapclaw"`，或提取为常量。

---

### G-10: CodeAdapter — PermissionInterruptRail 未实现

**模块**: `internal/swarm/server/adapter/code_adapter.go` L796

Python 的 code 模式下 PermissionInterruptRail 是固定 Rail，Go 返回 nil（标注 ⤵️ 10.6.3-10），code 模式下权限检查缺失。

**修复方案**: 实现 PermissionInterruptRail 或标注明确延后。

---

### G-11: UapClaw — Team 模式后续请求判断缺失 + processTeamInterrupt 硬编码 false

**模块**: `internal/swarm/server/runtime/uapclaw.go` L508-512, L777, L797

Python 通过 `active_session_id/pending_session_id` 判断首次/后续请求，Go 所有请求都走同一队列。`processTeamInterrupt` 中 `paused`/`cancelled` 硬编码为 false，Team 暂停/取消功能完全无效。

**修复方案**: 回填 Team 请求分发逻辑；processTeamInterrupt 应读取实际中断状态。

---

### G-12: GlobalSessionController — FlushScope/FlushAll 串行执行

**模块**: `internal/agentcore/session/controller/global_controller.go` L157-180

Python 使用 `asyncio.gather` 并发执行，Go 是串行且遇到第一个错误就返回。`FlushSession` 用了 errgroup 但 FlushScope/FlushAll 没有。

**修复方案**: 使用 `errgroup` 并发执行，错误处理收集所有结果。

---

### G-13: MessageOffloader — offloadMessage 未传递 extra_fields

**模块**: `internal/agentcore/context_engine/processor/offloader/message_offloader.go` L285-310

Python 将原始消息的额外字段（`tool_call_id`, `name`, `metadata` 等）通过 `**extra_fields` 传递，Go 只传 `role`/`content`/`offload_handle`/`offload_path`。卸载后的 ToolMessage 可能丢失关键元数据。

**修复方案**: 提取原始消息的 `ToolCallID`/`Name`/`Metadata` 等字段，通过选项传递给 OffloadMessages。

---

### G-14: ToolResultBudgetProcessor — offload 占位符缺少 path 字段

**模块**: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go` L520

Python: `[[OFFLOAD: handle=..., type=..., path=...]]`  
Go: `[[OFFLOAD: handle=..., type=...]]`（缺少 path）

**修复方案**: 在 offload handle 字符串中加入 `path=%s` 字段。

---

### G-15: coding_memory_tool_ops — Create 模式 SKIP 逻辑缺失

**模块**: `internal/agentcore/memory/lite/coding_memory_tool_ops.go` L216-219

Python 在 Create 模式下调用 `_run_checker` 做冗余判断，如果新记忆不在 actions 中则返回 `WriteMode.SKIP`。Go 标注为 ⤵️ 回填 7.8，当前无法判断冗余，可能导致写入冗余记忆。

**修复方案**: 当 runChecker 实现后（7.8），在 Create 模式中加入 REDUNDANT 判断和 SKIP 返回。

---

### G-16: coding_memory_tool_ops — Append 模式 SKIP 判断缺失

同上，`prepareAppendMode` 不支持返回 SKIP 结果，Python 中 `_prepare_append_mode` 如果判定 REDUNDANT 会直接返回 `WriteResult(mode=SKIP)`。

**修复方案**: `prepareAppendMode` 需要支持返回 SKIP 结果。

---

### G-17: EvolutionStore — readFileText 错误处理行为差异

**模块**: `internal/evolving/checkpointing/evolution_store.go` L182-210

Python: sys_operation code != 0 时返回空串（不 fallback 到本地读）。Go: sys_operation 失败时 fallback 到本地 `os.ReadFile`。行为不一致。

**修复方案**: 对齐 Python：sys_operation code != 0 时直接返回 `("", nil)`，不 fallback。

---

### G-18: EvolutionStore — normalizeBaseDirs 不展开 ~ 路径

**模块**: `internal/evolving/checkpointing/evolution_store.go` L642-658

Python 使用 `Path.expanduser().resolve()` 展开 `~`，Go 使用 `filepath.Abs()` 不展开。如果配置中包含 `~/skills` 这样的路径，Go 版本会失败。

**修复方案**: 在 `normalizeBaseDirs` 中先调用 `expandHomeDir` 展开 `~`。

---

### G-19: SingleDimUpdater/MultiDimUpdater — score_threshold 类型转换不完整

**模块**: `internal/evolving/updater/single_dim/single_dim.go` L107, `multi_dim/multi_dim.go` L129

只处理了 `float64` 类型，Python 的 `config.get("score_threshold")` 可以是任意类型。如果传入 `int`（如 `0`），Go 会忽略。

**修复方案**: 增加 `int` 类型分支。

---

### G-20: callback/filters.go — ValidationFilter/ConditionalFilter/ParamModifyFilter 缺少 panic 保护

**模块**: `internal/agentcore/runner/callback/filters.go` L285-414

Python 用 try/except 捕获 validator/condition 抛出的异常返回 SKIP，Go 没有 recover/panic 保护。如果 Validator panic，会导致整个回调框架崩溃。

**修复方案**: 在 Filter 方法中加 `defer func() { if r := recover(); r != nil { ... } }()` 保护。

---

### G-21: callback/chain.go — ChainActionRetry 重试耗尽时 fall through

**模块**: `internal/agentcore/runner/callback/chain.go` L189-193

重试次数已耗尽时，Go 没有 break 也没有 rollback，会 fall through 正常完成循环，将失败的回调视为成功。

**修复方案**: 重试耗尽时触发 rollback：`c.rollback(execCtx, cctx, executedInfos)`。

---

### G-22: StreamController — CancelledError 处理中外部取消可能导致 shouldContinue 误判

**模块**: `internal/agent_teams/agent/stream_controller.go` L605-608

Python 在 CancelledError 时 re-raise，上层 `cancelled = True`。Go 中 `ctx.Err()` 检测取消后直接 return，如果是外部取消（非 cooperative_cancel），`cancelRequested` 不为 true，`shouldContinue` 会误判为 true。

**修复方案**: 在 `executeRound` 中当 `ctx.Err() != nil` 时设置标志让 `runOneRound` 知道轮次被取消。

---

### G-23: Pipeline.Resume — GetNextStage 需确认支持函数式 next_stage

**模块**: `internal/swarm/server/runtime/skill/skilldev/pipeline.go` L200

Python 支持 `next_stage` 为函数（callable），动态计算下一阶段。Go 通过 `GetNextStage(data)` 方法统一处理，需确认内部确实支持函数式计算（如 REVIEW 阶段根据 action 决定下一阶段）。

**修复方案**: 确认 `SuspensionPoint.GetNextStage` 实现；如果是简单值类型，需修改为 `func(data map[string]any) SkillDevStage`。

---

### G-24: ExtensionRegistry — 缺少 list_extensions 方法

**模块**: `internal/swarm/extensions/registry.go`

Python 有 `list_extensions()` 方法返回已注册扩展列表，Go 仅有 `Register`/`Unregister`/`Trigger`。

**修复方案**: 补充 `ListExtensions() []*ExtensionMetadata` 方法。

---

## 三、提示问题（18 个）

### T-01: TeamBackend — ApproveTool 返回值差异（bool vs MemberOpResult）
Python 返回 bool，Go 返回 MemberOpResult。风格差异，功能等价。

### T-02: TeamBackend — IsTeamCompleted 缺少"无任务"/"无成员"检查
Python `if not tasks: return None` / `if not members: return None`，Go 没有这两步检查。

### T-03: TeamBackend — BuildTeam 预定义成员 `_ = agentCard` 多余代码
`agentCard := memberCardID; _ = agentCard` 是遗留代码，应删除 `_ = agentCard` 行。

### T-04: TeamBackend — human_agent_names 返回类型差异
Python 返回 frozenset（不可变集合），Go 返回排序后的字符串切片。合理适配。

### T-05: TeamAgent — deliver_input 缺少排队日志
Python 记录 `[member] queueing input for next round (transition window): preview`，Go 无日志。

### T-06: TeamAgent — resume_interrupt 缺少日志
Python 两个分支（stale/queueing）都有日志，Go 均无。

### T-07: StreamController — startRound 对非 string 内容显示 "non-string" 而非类型名
Python 用 `type(content).__name__`，Go 固定为 "non-string"。应使用 `fmt.Sprintf("%T", content)`。

### T-08: StreamController — pendingInterruptResumes 类型为 []any
Python 为 `list[InteractiveInput]`，Go 为 `[]any`，丢失类型安全。

### T-09: AgentConfigurator — SetupInfra 缺少 messager_config 节点 ID 调整和 model allocator 构建
标注 TODO #9.65 和 #9.64，确认待回填。

### T-10: SkillManager 临时目录前缀用旧名
`os.MkdirTemp("", "jiuwenswarm_clawhub_")` 应改为 `"uapclaw_clawhub_"`。

### T-11: TrajectoryAggregator — 冗余 nil check
`if iv == 0 { iv = 0 }` 完全无效，`StartTimeMs` 已是 int 零值就是 0。

### T-12: ExperienceScorer — Evaluate 返回 nil vs []
Python 空记录返回 `[]`，Go 返回 `nil, nil`。nil slice 的 `len()` 为 0 行为等价，但 `== nil` 检查不一致。

### T-13: FragmentMemoryManager 完全缺失
`internal/agentcore/memory/manage/` 包没有 `fragment_manager.go`，`manage_test.go` 仅为 `// TODO: 补充单元测试`。ListFragmentMemories 排序逻辑不存在。

### T-14: ListFragmentMemories 排序逻辑缺失
Python 使用 `(mem, timestamp)` 二元组降序排序，Go 无实现。

### T-15: spawn/child.go — 未向父进程发送顶层 ERROR 消息
Python 在 except 块中向 stdout 发送 ERROR 消息，Go 只返回 error 给调用方。

### T-16: DiffService 算法差异
Go 使用 LCS，Python 使用 difflib.SequenceMatcher（Ratcliff/Obershelp 算法）。极端边界情况下 hunk 划分可能不同。

### T-17: callback/FilterResult 缺少 ModifiedArgs/ModifiedKwargs 分离
Python 分离 `modified_args` 和 `modified_kwargs`，Go 只有 `ModifiedData any`。合理的语言差异简化。

### T-18: StreamController — executeRound 超时使用硬编码 3600s
应从 DeepAgentConfig.CompletionTimeout 读取，暂用默认值。

---

## 四、⤵️ 占位代码审计结果

### 已实现但文档标记过期（应更新为 ✅）

| 占位点 | 实际状态 | 证据 |
|--------|---------|------|
| 5.2 Config 返回类型 | ✅ 已实现 | `SessionConfig` 接口已存在，`Config()` 返回类型已改为 `config.SessionConfig` |
| 5.5 Config 回填 | ✅ 已实现 | NodeSessionFacade.Config() 已返回 `config.SessionConfig` |
| 6.8 ConsumeRetryRequest/HasForceFinishRequest | ✅ 已实现 | 已有真实实现和测试 |

### 高优先级未实现

| 占位点 | 当前状态 | 影响 |
|--------|---------|------|
| 9.59 SessionManager TeamAgent 类型依赖 | `any` 占位 | 交互核心逻辑为 stub |
| 9.61 RecoveryManager | 文件不存在 | 恢复管理完全缺失 |
| 9.55 DeliverInput/AutoStart | stub | Team Agent 无法启动和投递 |
| 9.85 InProcessSpawn 核心 Runner | stub | teammate 不会执行任何工作 |

### 中优先级未实现

| 占位点 | 当前状态 |
|--------|---------|
| 9.26 BrowserAgent Playwright MCP | 大量 TODO/占位 |
| 9.28 PlanAgent team.plan 特化 | TeamPlanMode any 占位 |
| 7.22/7.23 Migration Operations/UpdateSchema | 全部返回 "not yet implemented" |
| 7.8 MemUpdateChecker 冲突检查 | 直接返回 ADD（stub） |

### 低优先级未实现（明确延后）

| 占位点 | 当前状态 |
|--------|---------|
| 10.5.7 ExtensionLoader | 返回错误 |
| 10.5.8 ExtensionManager | 返回错误 |
| 10.5.10 CryptoUtility | 空实现 |
| 9.71 evaluator_pipeline | 仅 doc.go |

### 代码库中 ⤵️ 占位总数

约 120+ 个，主要集中在：
- `swarm/server/adapter/` (50+) — Rails/工具集集成
- `agent_teams/` (30+) — TeamAgent/Coordination 依赖
- `context_engine/` (10+) — Workspace/Session 集成
- `session/` (15+) — ActorManager/GraphStore

---

## 五、修复优先级建议

### P0 — 阻塞级（影响核心正确性，需立即修复）

1. **S-07** TeamAgent.ShutdownSelf 缺少 CloseStream — teammate 无法优雅退出
2. **S-02** ApprovePlan 签名缺失 — 无法拒绝计划
3. **S-01** SendMessage 返回值丢弃 — 消息失败被静默吞掉
4. **S-09/S-10/S-11** Team 交互核心为 stub — HITT/InteractiveInput 功能不可用
5. **S-16** TrajectoryExtractor toolName 为空 — 轨迹分析完全失效
6. **S-24** SubagentRail 缺少 Uninit — 资源泄漏/工具残留
7. **S-23** VerificationRail 缺少守卫 — 非预期场景注入噪音

### P1 — 重要级（影响数据完整性，应尽快修复）

8. **S-14** UpdateModelPool 缺少 inherit_pool_ids — 池更新丢失 allocation_id
9. **S-19** GenerateUserPatch 不加载真实数据 — LLM 优化器使用占位上下文
10. **S-17/S-18** TrajectoryExtractor agent_id/parseLLMResponse 缺失
11. **S-20** spawn/child SetConfig 错误不传播
12. **S-21** spawn/child message_id 不传播
13. **S-22** callback chain Continue 追加错误 result
14. **S-25** TodoItem JSON tag 不兼容 — 序列化兼容性

### P2 — 改进级（行为偏差，应计划修复）

15. **S-03/S-04** CancelAllTasks/CancelMember 返回类型/语义
16. **S-05/S-06** CleanTeam/ForceCleanTeam 行为偏差
17. **S-12/S-13** TeamMember stub 导致 SHUTDOWN_REQUESTED 不触发
18. **S-15** BuildMemoryManager 无条件创建
19. **G-01~G-32** 各一般问题（重点关注 G-26 HeartbeatRail 重复累积、G-28 注册顺序）

### P3 — 文档级

20. **T-01~T-26** 各提示问题
21. 3 个 ⤵️ 标记应更新为 ✅
22. 约 120 个 ⤵️ 占位的跟踪管理
