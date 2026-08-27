# 48小时逻辑审查报告 — 2026-08-26

> 审查范围：48小时内提交 + 近期功能实现章节与 Python 参考项目的一致性
> 提交范围：80f87475 (i18n) 为48小时内唯一提交；扩展至近14天功能提交覆盖的章节
> 审查章节：9.65a-4 TeamBackend、7.6/7.9 Memory、10.6.1-3 Prompt/Rails、9.51-9.53 工具修复

---

## 一、提交概览

| 提交 | 日期 | 章节 |
|------|------|------|
| 80f87475 fix(i18n) | 08-26 | 全局 |
| a7324ead fix(harness): 对齐 Python 9.51-9.53 | 08-24 | 9.51-9.53 |
| 5df0dc84 feat(9.65a-4): spawnAndPublish + Startup 回调 | 08-23 | 9.65a-4 |
| fe77bdfc fix(9.65a-4): any→具体类型回填 | 08-23 | 9.65a-4 |
| 373521a7 feat: FragmentMemoryManager (7.6) | 08-20 | 7.6 |
| c0d43bea feat: StructuredAskUserRail (10.6.3) | 08-19 | 10.6.3 |
| 04f041ab feat: code/prompt 包 (10.6.1-2) | 08-18 | 10.6.1-2 |

---

## 二、严重问题（功能缺失或逻辑错误）

### S-01：StructuredAskUserTool schema 缺少 `query` 参数，LLM 无法使用纯文本查询模式

**章节**：10.6.3 StructuredAskUserRail

**问题描述**：Python 的 `EXTENDED_INPUT_PARAMS` schema 包含 `query`（string，必填）和 `questions`（array，可选），Go 的 schema 仅有 `questions`（必填），完全缺失 `query` 参数。`required` 也不匹配：Python `["query"]`，Go `["questions"]`。同时 Go 的 `getStructuredDescription` 描述文本中提到了 `query`（"只传 `query`"、"传 `query` + `questions`"），但 schema 中没有声明，LLM 无法知道可传 `query`。

**Python 参考代码**：
- 文件：`jiuwenswarm/agents/harness/common/rails/ask_user_rail.py`，行 85-104
```python
EXTENDED_INPUT_PARAMS_EN: dict[str, Any] = {
    "type": "object",
    "properties": {
        "query": {
            "type": "string",
            "description": "The question to present to the user (required).",
        },
        "questions": {
            "type": "array",
            "description": "Structured questions with selectable options. ...",
            "items": _QUESTIONS_ITEM_SCHEMA,
        },
    },
    "required": ["query"],
}
```

**Go 问题代码**：
- 文件：`internal/agentcore/harness/prompts/tools/ask_user.go`，行 51-83
```go
return map[string]any{
    "type": "object",
    "properties": map[string]any{
        "questions": map[string]any{  // ← 只有 questions，缺少 query
            // ...
        },
    },
    "required": []any{"questions"},  // ← questions 必填，query 缺失
}
```

**修复方案**：为 `StructuredAskUserTool` 创建独立的 `EXTENDED_INPUT_PARAMS` schema（包含 `query` + `questions`），与 Python 对齐。修改 `NewStructuredAskUserTool` 直接构建 ToolCard 使用扩展 schema，不再复用 `AskUserMetadataProvider` 的 schema。

**影响范围**：StructuredAskUserTool 的所有调用方（CodeAdapter、DeepAdapter 中 ask_user 工具注册）。

---

### S-02：TeamBackend.ShutdownMember 缺少 FSM 状态转换校验

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 先做 FSM 校验 `is_valid_transition(current_status, SHUTDOWN_REQUESTED)`，只有校验通过才调用 `update_member_status`。Go 使用 `TryTransitionMemberStatus`（CAS 方式），但跳过了 `is_valid_transition` 校验。CAS 失败可能是并发竞争导致的暂时失败，也可能是非法状态转换，两者的错误原因完全不同，Go 返回笼统的 "CAS transition failed"。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 551-570
```python
if not is_valid_transition(current_status, MemberStatus.SHUTDOWN_REQUESTED, MEMBER_TRANSITIONS):
    return MemberOpResult.fail(f"Member {member_name} cannot shut down from status '{current_status.value}'")
success = await self.db.member.update_member_status(member_name, self.team_name, MemberStatus.SHUTDOWN_REQUESTED.value)
if not success:
    return MemberOpResult.fail(f"Database rejected status update for member {member_name}")
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 429-434
```go
ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
    member.Status, string(atschema.MemberStatusShutdownRequested))
if !ok {
    return atschema.NewMemberOpResultFail("CAS transition failed for: " + memberName)
}
```

**修复方案**：在 CAS 之前添加 `fsm.IsValidMemberTransition(currentStatus, MemberStatusShutdownRequested)` 校验，校验失败返回明确错误信息。CAS 仍保留作为并发安全保护。

---

### S-03：TeamBackend.BuildTeam 中 Leader 注册绕过 spawn_member

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 的 `build_team` 通过 `self.spawn_member()` 注册 Leader，享受已有成员检查、DB 写入失败处理、日志记录等保障。Go 直接调用底层 `CreateMember`，跳过了这些保障。Go 未检查 Leader 成员是否已存在，未处理 `CreateMember` 返回值（忽略 `ok`），且 `agentCard` 参数传空字符串，Python 构造了完整 AgentCard。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 1001-1010
```python
await self.spawn_member(
    member_name=leader_member_name,
    display_name=leader_display_name,
    agent_card=leader_card,
    desc=leader_desc,
    status=MemberStatus.BUSY,
    execution_status=ExecutionStatus.RUNNING,
    mode=MemberMode.BUILD_MODE,
    allocation=self.leader_allocation,
)
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 535-537
```go
tb.db.Member().CreateMember(ctx, tb.memberName, tb.teamName, leaderDisplayName, "",
    string(atschema.MemberStatusBusy), string(atschema.TeamRoleLeader), leaderDesc,
    string(atschema.ExecutionStatusRunning), string(atschema.MemberModeBuildMode), "", leaderModelRefJSON)
// 返回值被忽略！
```

**修复方案**：将 Leader 注册改为调用 `tb.SpawnMember()`（需先修复 S-07 扩展 SpawnMember 签名），或至少补充：存在性检查、返回值检查、AgentCard 构造。

---

### S-04：TeamBackend.CancelMember 消息发送失败后忽略错误

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 在 `cancel_member` 中消息发送失败时返回 `False`，阻止后续事件发布。Go 忽略 `SendMessage` 返回值，继续发布事件，即使消息未送达。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 645-650
```python
success = await self.message_manager.send_message(
    content=t("team.cancel_request_content"), to_member_name=member_name
)
if not success:
    team_logger.error(f"Failed to send cancel request message to member {member_name}")
    return False
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 480
```go
_, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
// 错误被忽略，继续执行后续逻辑
```

**修复方案**：检查 `SendMessage` 返回值，失败时返回 `MemberOpResultFail`。

---

### S-05：TeamBackend.IsTeamCompleted 缺少"至少一个任务"前置条件

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 明确要求"至少一个任务"才返回完成；当 `tasks` 为空列表时返回 `None`。Go 没有此检查，零任务时空 for 循环自然跳过，可能返回 `TaskCount=0` 的完成快照——没有任务的团队不应被认为是"完成"的。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 813-815
```python
tasks = await self.task_manager.list_tasks()
if not tasks:
    return None
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 252-262
```go
tasks, err := tb.db.Task().GetTeamTasks(ctx, tb.teamName, "")
if err != nil {
    return nil, err
}
// ← 缺少 if len(tasks) == 0 { return nil, nil }
for _, t := range tasks {
    if !fsm.IsTaskTerminal(t.Status) {
        return nil, nil
    }
}
```

**修复方案**：添加 `if len(tasks) == 0 { return nil, nil }`。

---

### S-06：TaskPlan.ToMarkdown() 格式与 Python 不一致，缺少 result_summary

**章节**：9.51 工具相关

**问题描述**：Go 的 `TaskPlan.ToMarkdown()` 与 Python 有三处差异：(1) `IN_PROGRESS` 图标 Go 用 `[→]`，Python 用 `[>]`；(2) Go 完全缺少 `result_summary` 后缀显示；(3) 标题格式 Go 用 `# Task Plan\n**Goal:**`，Python 用 `## Goal:`。其中 `result_summary` 缺失影响最大——已完成任务的上下文无法展示。

**Python 参考代码**：
- 文件：`openjiuwen/harness/schema/task.py`，行 177-195
```python
def to_markdown(self) -> str:
    lines = [f"## Goal: {self.goal}", ""]
    for t in self.tasks:
        if t.status == TodoStatus.COMPLETED: mark = "√"
        elif t.status == TodoStatus.IN_PROGRESS: mark = ">"
        elif t.status == TodoStatus.CANCELLED: mark = "×"
        else: mark = " "
        suffix = ""
        if t.result_summary:
            suffix = f" — {t.result_summary}"
        lines.append(f"- [{mark}] {t.content}{suffix}")
    return "\n".join(lines)
```

**Go 问题代码**：
- 文件：`internal/agentcore/harness/schema/task.go`，行 251-268
```go
sb.WriteString("# Task Plan\n\n**Goal:** ")
sb.WriteString(tp.Goal)
sb.WriteString("\n\n")
for _, task := range tp.Tasks {
    icon, ok := StatusIcons[task.Status]  // IN_PROGRESS = "[→]"，非 "[>]"
    if !ok { icon = "[?]" }
    sb.WriteString("- ")
    sb.WriteString(icon)
    sb.WriteString(" ")
    sb.WriteString(task.Content)
    // ← 缺少 result_summary 后缀
    sb.WriteString("\n")
}
```

**修复方案**：
1. `IN_PROGRESS` 图标从 `[→]` 改为 `[>]`
2. 添加 `task.ResultSummary` 后缀显示：`if task.ResultSummary != "" { sb.WriteString(" — "); sb.WriteString(task.ResultSummary) }`
3. 标题格式改为 `## Goal:` 对齐 Python

---

### S-07：TeamBackend.ForceCleanTeam 多余调用 onTeamCleaned 回调

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 的 `force_clean_team` **不调用** `_on_team_cleaned` 回调，只有 `clean_team` 才调用。Go 的 `ForceCleanTeam` 额外调用了 `onTeamCleaned`，违反 Python 的设计意图——`_on_team_cleaned` 应 "exactly once on the clean_team SUCCESS path"，强制清理路径不应触发。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 729-761
```python
async def force_clean_team(self, skip_assignees: set[str] | None = None) -> bool:
    # ... 只调用 _remove_cleanup_paths，不调用 _on_team_cleaned
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 648-654
```go
// 步骤 3: 回调触发
if tb.onTeamCleaned != nil {
    if err := tb.onTeamCleaned(ctx); err != nil {
        logger.Warn(tbLogComponent).Err(err).Msg("ForceCleanTeam: onTeamCleaned 回调失败")
    }
}
```

**修复方案**：从 `ForceCleanTeam` 中移除 `onTeamCleaned` 回调调用。

---

### S-08：TeamBackend.CleanTeam 允许 ERROR 状态成员，Python 仅允许 SHUTDOWN

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 的 `clean_team` 只允许 `SHUTDOWN` 状态的成员通过，ERROR 状态的成员被视为活跃成员。Go 额外允许了 `ERROR` 状态，有 ERROR 成员时 Go 会继续清理，Python 会拒绝。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 684
```python
if member_data.status != MemberStatus.SHUTDOWN.value:
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 599-600
```go
if m.Status != string(atschema.MemberStatusShutdown) &&
    m.Status != string(atschema.MemberStatusError) {
```

**修复方案**：移除对 `MemberStatusError` 的豁免，只允许 `SHUTDOWN` 状态。

---

## 三、一般问题（逻辑不一致但不影响核心流程）

### G-01：TeamBackend.ShutdownMember 缺少 `force` 参数

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 的 `shutdown_member` 接收 `force: bool = False`，在 `MemberShutdownEvent` 中传递。Go 硬编码 `Force: false`。`ForceCleanTeam` 内部调用 `ShutdownMember` 时，Python 传递 `force=True`，Go 无法传递。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 514, 743
```python
async def shutdown_member(self, member_name: str, force: bool = False) -> MemberOpResult:
    ...
# ForceCleanTeam 中调用:
await self.shutdown_member(member_data.member_name, force=True)
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 418, 638
```go
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) atschema.MemberOpResult {
    // force 硬编码为 false
...
result := tb.ShutdownMember(ctx, m.MemberName)  // ForceCleanTeam 中调用，无法传 force=true
```

**修复方案**：给 `ShutdownMember` 添加 `force bool` 参数，在 `ForceCleanTeam` 中传入 `force=true`。

---

### G-02：TeamBackend.ApprovePlan 参数语义不一致

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 接收 `plan_id`（计划记录 ID），Go 接收 `taskID`（任务 ID），语义不同。Python 还接收 `approved` 和 `feedback` 参数，Go 硬编码 `approved=true, feedback=""`，无法拒绝计划。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 398-403
```python
async def approve_plan(self, plan_id: str, approved: bool = True, feedback: Optional[str] = None) -> bool:
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 711
```go
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) atschema.MemberOpResult {
```

**修复方案**：将参数改为 `(ctx context.Context, planID string, approved bool, feedback string)`，对齐 Python。

---

### G-03：TeamBackend.CancelTask 缺少幂等检查 + 多余的事件发布

**章节**：9.65a-4 TeamBackend

**问题描述**：(1) Python 有 "already cancelled" 幂等检查，Go 缺少；(2) Python 不发布 `TaskCancelledEvent`/`TaskUnblockedEvent`，Go 额外发布了这两种事件。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 866-896
```python
if task.status == TaskStatus.CANCELLED.value:
    team_logger.info(f"Task {task_id} is already cancelled")
    return True
# 无 TaskCancelledEvent/TaskUnblockedEvent 发布
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 665-691
```go
// ← 缺少 already cancelled 幂等检查
tb.publishEvent(ctx, atschema.TaskCancelledEvent{...})  // ← Python 不存在此事件
for _, uid := range unblocked {
    tb.publishEvent(ctx, atschema.TaskUnblockedEvent{...})  // ← Python 不存在此事件
}
```

**修复方案**：添加 "already cancelled" 幂等检查。评估 `TaskCancelledEvent`/`TaskUnblockedEvent` 是否为 Go 扩展设计，如不需要则移除。

---

### G-04：TeamBackend.SpawnMember 缺少 status/execution_status/mode/allocation 参数

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 的 `spawn_member` 接收 `status`、`execution_status`、`mode`、`allocation` 参数，允许自定义初始状态。Go 硬编码为 `MemberStatusUnstarted`/`ExecutionStatusIdle`/`teammateMode`，不可自定义。这直接导致 S-03 中 Leader 注册无法通过 `SpawnMember` 调用（Leader 需要 `BUSY`/`RUNNING`/`BUILD_MODE` 状态）。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 232-244
```python
async def spawn_member(self, member_name: str, display_name: str, agent_card: AgentCard, *,
    status: MemberStatus = MemberStatus.UNSTARTED,
    execution_status: ExecutionStatus = ExecutionStatus.IDLE,
    mode: MemberMode = MemberMode.BUILD_MODE,
    allocation: Optional["Allocation"] = None,
    role: TeamRole = TeamRole.TEAMMATE,
) -> MemberOpResult:
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 304
```go
func (tb *TeamBackend) SpawnMember(ctx context.Context, memberName, displayName, agentCard, role, desc, prompt, modelName string) atschema.MemberOpResult {
```

**修复方案**：扩展 `SpawnMember` 签名，添加 `status`、`executionStatus`、`mode` 参数（可使用 Functional Options）。

---

### G-05：TeamBackend.CancelAllTasks 返回类型不一致

**章节**：9.65a-4 TeamBackend

**问题描述**：Python 返回 `int`（取消的任务数），Go 返回 `MemberOpResult`。调用方无法从 Go 返回值获取取消数量。

**Python 参考代码**：
- 文件：`openjiuwen/agent_teams/tools/team.py`，行 898-901
```python
async def cancel_all_tasks(self, skip_assignees: Optional[set[str]] = None) -> int:
```

**Go 问题代码**：
- 文件：`internal/agent_teams/tools/team_backend.go`，行 695
```go
func (tb *TeamBackend) CancelAllTasks(ctx context.Context, skipAssignees []string) atschema.MemberOpResult {
```

**修复方案**：修改返回值为 `(int, error)` 或在 `MemberOpResult` 中增加计数字段。

---

### G-06：ListFragmentMemories 缺少 memType 参数校验

**章节**：7.6 FragmentMemoryManager

**问题描述**：Python 校验 `mem_type.value not in FRAGMENT_MEMORY_TYPE` 时返回空列表+记录 error 日志。Go 接受 `string`，只做非空判断，不校验是否为合法碎片记忆类型。传入非法类型如 `"invalid_type"` 时 Go 直接传给底层，Python 返回空列表。

**Python 参考代码**：
- 文件：`openjiuwen/core/memory/manage/index/fragment_memory_manager.py`，行 310-318
```python
if mem_type:
    if mem_type.value not in FRAGMENT_MEMORY_TYPE:
        memory_logger.error("%s is not a valid memory type", mem_type.value, ...)
        return []
```

**Go 问题代码**：
- 文件：`internal/agentcore/memory/manage/index/fragment_manager.go`，行 283
```go
func (m *FragmentMemoryManager) ListFragmentMemories(ctx context.Context, userID string, scopeID string,
    offset int, batchSize int, memType string) ([]*index.MemoryDoc, error) {
    // ← memType 非空时不校验合法性
```

**修复方案**：添加 `memType` 非空时校验是否在 `FragmentMemoryTypes` 中，非法时记录 error 并返回空切片。

---

### G-07：EXTENDED_INPUT_PARAMS 中 options item 的 `required` 与 Python 不一致

**章节**：10.6.3 StructuredAskUserRail

**问题描述**：Go 的 options item `required` 为 `["label", "description"]`，Python 只有 `["label"]`（description 可选）。Go 还有 Python 中没有的 `preview` 字段。

**Python 参考代码**：
- 文件：`jiuwenswarm/agents/harness/common/rails/ask_user_rail.py`，行 60-74
```python
"required": ["label"],  # description 可选
```

**Go 问题代码**：
- 文件：`internal/agentcore/harness/prompts/tools/ask_user.go`，行 66-73
```go
"required": []any{"label", "description"},  // ← description 是必填
"preview": map[string]any{...},  // ← Python 没有
```

**修复方案**：修复 S-01 时一并修正 options item 的 `required` 和字段定义。

---

### G-08：codeRailBuildNames 缺少 LspRail/ProjectMemoryRail/CodingMemoryRail 映射

**章节**：10.3.7-11 适配器辅助

**问题描述**：Python 的 `_RAIL_BUILD_NAMES` 包含 `LspRail`/`ProjectMemoryRail`/`CodingMemoryRail` 条目，Go 的 `codeRailBuildNames` 缺少这三个。当 config.yaml 引用这些名称时，Go 输出 "未知的动态 Rail 名称" 警告而非 "已在固定集合中，跳过" 信息。

**修复方案**：在 `codeRailBuildNames` 中添加三个缺失条目。

---

### G-09：FragmentMemoryManager.AddMemories + BaseMemoryManager 接口缺少 llm 参数

**章节**：7.6/7.9 Memory

**问题描述**：Python 的 `add_memories` 和 `BaseMemoryManager` 接口都接受 `llm: Model | None = None` 参数，传给 `MemUpdateChecker.check()`。Go 缺少此参数，7.8 回填 LLM 驱动冲突检查时无法传入 `llm`。

**Python 参考代码**：
- 文件：`openjiuwen/core/memory/manage/index/fragment_memory_manager.py`，行 125-126
```python
async def add_memories(self, ..., llm: Model | None = None, **kwargs):
    ...
    action_items = await checker.check(new_memories, old_memories, base_chat_model=llm)
```

**Go 问题代码**：
- 文件：`internal/agentcore/memory/manage/index/fragment_manager.go`，行 68
```go
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID string, scopeID string,
    memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error) {
    // ← 无 llm 参数
```

**修复方案**：在 `AddMemories` 和 `BaseMemoryManager` 接口中预留 `llm Model` 参数（当前传 nil），7.8 回填时使用。

---

## 四、提示问题（日志/格式等不影响流程）

### T-01：RegisterCleanupPath 路径展开方式不一致

**章节**：9.65a-4 TeamBackend

Python 使用 `Path(path).expanduser()` 展开 `~`，Go 使用 `os.ExpandEnv(path)`，不展开 `~`。Linux 上 `~` 不会自动展开。

**Python**：`self._cleanup_paths.add(str(Path(path).expanduser()))`
**Go**：`expanded := filepath.Clean(os.ExpandEnv(path))`

**修复方案**：添加 `~` 展开逻辑（`os.UserHomeDir()` 替换）。

---

### T-02：IsHumanAgent 未处理空字符串

**章节**：9.65a-4 TeamBackend

Python 处理 `None` 返回 `False`，Go 不处理空字符串。

**修复方案**：添加 `if memberName == "" { return false }` 前置检查。

---

### T-03：SpawnHumanAgent 缺少 AgentCard 构造和失败日志

**章节**：9.65a-4 TeamBackend

Go 传空字符串 `""` 作为 `agentCard`，Python 构造完整 AgentCard。Python 在 `spawn_member` 失败时记录 warning 日志，Go 没有。

**修复方案**：构造 AgentCard，添加 spawn 失败 warning 日志。

---

### T-04：RefreshHumanAgentRoster 缺少 db.initialize() 调用

**章节**：9.65a-4 TeamBackend

Python 在查询前调用 `db.initialize()`（如果可用），Go 直接查询。

**修复方案**：根据 Go 的数据库层设计决定是否需要初始化检查。

---

### T-05：buildCodeAgentRails 中 ConfirmInterruptRail ⤵️ 标注与实际不符

**章节**：10.3.7-11 适配器辅助

Go 代码注释写着 `⤵️ 10.6.3-10: ConfirmInterruptRail 尚未实现`，但 `buildConfirmInterruptRail()` 方法实际上已实现。

**修复方案**：更新注释为 `✅ ConfirmInterruptRail 已实现`。

---

### T-06：BuildCodeSessionGuidanceSection 添加顺序与 Python 不一致

**章节**：10.6.1-2 Prompt Builder

Go 代码中 `BuildCodeSessionGuidanceSection()` 放在第 8 位，Python `_CODE_SECTION_GENERATORS` 中在第 3 位。由于 `Build()` 按 priority 排序，实际输出顺序正确（priority 55 > 25），但代码可读性不一致。

**修复方案**：调整 `AddSection` 调用顺序与 Python 列表顺序一致。

---

### T-07：Search() score 排序截断防御层缺失

**章节**：7.6 FragmentMemoryManager

Go 的 `FragmentMemoryManager.Search()` 直接委托给底层 `SimpleMemoryIndex.Search()`（已实现排序截断），但 Python 在上层做了二次保险。切换到其他 index 实现时可能缺失。

**修复方案**：在 `FragmentMemoryManager.Search()` 返回前添加按 Score 降序排序+截断至 topK 的防御逻辑。

---

### T-08：agent_manager.go 额外追加 `: %w` 错误链

**章节**：48小时 i18n 提交修改的文件

Python 不包含原始错误，Go 追加了 `%w` 错误链。如上游代码做字符串匹配可能失败。

**修复方案**：根据项目错误处理策略决定是否保留 `%w`。

---

### T-09：chroma_query_func 全部错误消息中文化与 Python 原版不一致

**章节**：9.51 工具修复 / 48小时 i18n 提交

i18n 提交将英文错误消息统一改为中文，但 Python 原版仍为英文。如果上游代码做字符串匹配会失败。

**修复方案**：根据项目 i18n 策略决定。如需与 Python 完全一致则保持英文错误消息。

---

### T-10：Bash/PowerShell 注入检测消息中英混杂

**章节**：9.51 工具修复

同一模块内部分消息已中文化，部分仍为英文，风格不统一。

**修复方案**：统一中英文消息风格。

---

## 五、⤵️ 待回填代码状态确认

| 标记位置 | 内容 | 确认状态 |
|----------|------|---------|
| `agent_teams/tools/database/` | Team DAO 实现 | ✅ 确认未实现 |
| `agent_teams/tools/database/engine.go` | SQL Engine 函数 | ✅ 确认未实现 |
| `memory/manage/update/update_checker.go` | MemUpdateChecker 完整 LLM 检查 | ✅ 确认未实现（仅 stub） |
| `memory/manage/mem_model/db_model.go` | CreateTables 迁移逻辑 | ✅ 确认未实现 |
| `adapter/code_adapter.go:57` | LspRail | ✅ 确认未实现 |
| `adapter/code_adapter.go:60` | ProjectMemoryRail | ✅ 确认未实现 |
| `adapter/code_adapter.go:65` | WorktreeRail | ✅ 确认未实现 |
| `adapter/code_adapter.go:796` | PermissionInterruptRail | ✅ 确认未实现 |
| `adapter/code_adapter.go:828` | ConfirmInterruptRail | ❌ **标注与实际不符** — 已实现 |
| `adapter/code_adapter.go:389` | load_user_rails | ✅ 确认未实现 |

---

## 六、问题统计

| 严重级别 | 数量 | 需立即修复 |
|---------|------|-----------|
| 严重 | 8 | S-01, S-02, S-03, S-04, S-05, S-06, S-07, S-08 |
| 一般 | 9 | G-01 ~ G-09 |
| 提示 | 10 | T-01 ~ T-10 |

**优先修复建议**：
1. **S-01** StructuredAskUserTool schema 缺少 query — 影响 LLM function calling 正确性
2. **S-02 + G-01** ShutdownMember FSM 校验 + force 参数 — 影响状态机正确性
3. **S-03 + G-04** BuildTeam Leader 注册 + SpawnMember 签名扩展 — 影响团队构建正确性
4. **S-06** TaskPlan.ToMarkdown() — 影响任务计划展示和 LLM 交互
5. **G-09** AddMemories 接口预留 llm 参数 — 为 7.8 回填做准备
