# 48小时逻辑审查报告

> **审查日期**: 2026-08-17 ~ 2026-08-20
> **审查范围**: 8月15-17日提交记录覆盖的实现计划章节
> **审查方法**: 逐方法对照 Python 参考项目，检查方法签名、步骤完整性、⤵️标记验证

---

## 一、审查范围

8月15-17日共 50+ 个提交，涉及以下章节的实现/修复：

| 章节 | 内容 | 状态 |
|------|------|------|
| 7.6 | FragmentMemoryManager | ✅ 新实现 |
| 7.8 | MemUpdateChecker stub | ⤵️ 回填标记 |
| 7.9 | 记忆数据模型 (MemoryType/OperationType/FragmentMemoryUnit 等) | ✅ 新实现 |
| 9.65a-4 | TeamBackend 门面 (30+ 方法) | ✅ 新实现+修复 |
| 10.3.7-11 | 适配器辅助修复 (CodeAgentRail/SkillToolkit/ModelCache) | ✅ 回填修复 |
| 10.6.1-2 | Prompt Builder (Agent/Code 模式提示词) | ✅ 新实现 |
| 10.6.3 | StructuredAskUserRail + StructuredAskUserTool | ✅ 新实现 |

---

## 二、问题汇总

| 严重程度 | 数量 |
|---------|------|
| 严重 | 8 |
| 一般 | 22 |
| 提示 | 15 |

---

## 三、严重问题

### S-01: TeamBackend.ShutdownMember 缺少 force 参数和 send_message 步骤

**Go文件**: `internal/agent_teams/tools/team_backend.go:387-414`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:514-598`

**问题描述**: Go 的 `ShutdownMember` 缺少 Python 的 `force` 参数，且缺少关键的消息发送步骤（`message_manager.send_message`）。Python 版本在状态更新后发送关闭请求消息给目标成员，这是跨进程通知的关键机制。此外 Go 错误地调用了 `CancelAllTasks`，Python 的 `shutdown_member` 不取消任务。

**Python样例**:
```python
async def shutdown_member(self, member_name: str, force: bool = False) -> MemberOpResult:
    # ... status update ...
    msg_id = await self.message_manager.send_message(
        content=t("team.shutdown_request_content"),
        to_member_name=member_name,
    )
    if not msg_id:
        team_logger.warning(...)
    await self.messager.publish(
        ... MemberShutdownEvent(..., force=force)
    )
```

**Go问题**:
```go
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) atschema.MemberOpResult {
    // ... CAS transition ...
    _, _ = tb.taskManager.CancelAllTasks(ctx, []string{memberName})  // ← Python 不做这个
    tb.publishEvent(ctx, atschema.MemberShutdownEvent{..., Force: false})  // ← 缺少 send_message
}
```

**修复方案**:
1. 添加 `force bool` 参数
2. CAS 转换后调用 `tb.messageManager.SendMessage(ctx, shutdownContent, memberName, "")`
3. `MemberShutdownEvent.Force` 使用传入的 `force` 参数
4. 移除 `CancelAllTasks` 调用

---

### S-02: TeamBackend.CancelMember 语义完全偏离 Python

**Go文件**: `internal/agent_teams/tools/team_backend.go:426-455`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:600-663`

**问题描述**: Go 的 `CancelMember` 使用 CAS 转换 `→SHUTDOWN_REQUESTED`，但 Python 的 `cancel_member` **不修改成员状态**。Python 仅在成员 BUSY 时重置其 CLAIMED 任务并发送取消消息+事件。Go 版本多做了状态转换，且缺少 `send_message` 步骤。

**Python样例**:
```python
async def cancel_member(self, member_name: str) -> bool:
    if current_status != MemberStatus.BUSY:
        return True  # 不忙，无需取消
    # 重置 CLAIMED 任务
    claimed_tasks = await self.task_manager.get_tasks_by_assignee(...)
    for task in claimed_tasks:
        await self.task_manager.reset(task.task_id)
    # 发送消息
    await self.message_manager.send_message(
        content=t("team.cancel_request_content"), to_member_name=member_name
    )
    # 发布事件
    await self.messager.publish(... MemberCanceledEvent(...))
```

**Go问题**:
```go
func (tb *TeamBackend) CancelMember(ctx context.Context, memberName string) atschema.MemberOpResult {
    // 步骤 3: CAS 转换 → SHUTDOWN_REQUESTED  ← Python 不做这个！
    ok := tb.db.Member().TryTransitionMemberStatus(...)
    // 步骤 4: 重置 CLAIMED 任务  ← 通过 db.Task 而非 taskManager
    // 步骤 5: 发布事件  ← 缺少 send_message！
}
```

**修复方案**:
1. 移除 CAS 状态转换（cancel_member 不修改成员状态）
2. 添加 BUSY 状态检查，仅对 BUSY 成员执行
3. 使用 `taskManager` 代替直接 DAO 调用
4. 在事件发布前调用 `messageManager.SendMessage`
5. 返回类型改为 `bool` 对齐 Python

---

### S-03: TeamBackend.ApprovePlan 签名不一致——Go 用 taskID，Python 用 planID

**Go文件**: `internal/agent_teams/tools/team_backend.go:655-673`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:398-463`

**问题描述**: Go 的 `ApprovePlan` 接收 `taskID`，但 Python 接收 `plan_id`。Python 的完整流程是：先查找 plan record 获取 task_id 和 member_name，再调用 `task_manager.approve_plan(plan_id=plan_id, ...)`。Go 直接传 taskID 绕过了 plan record 查找逻辑和多重校验。

**Python样例**:
```python
async def approve_plan(self, plan_id: str, approved: bool = True, feedback: Optional[str] = None) -> bool:
    plan_record = self.task_manager.get_plan_record(plan_id)
    if not plan_record:
        return False
    member_name = str(plan_record.get("member_name") or "")
    task_id = str(plan_record.get("task_id") or "")
    # 多重校验（stale plan、decision 重复检查等）...
    result = await self.task_manager.approve_plan(plan_id=plan_id, approved=approved, feedback=feedback, leader_name=self.member_name)
```

**Go问题**:
```go
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) atschema.MemberOpResult {
    err := tb.taskManager.ApprovePlan(ctx, taskID, true, "")  // 用 taskID 当 planID
}
```

**修复方案**: 签名改为 `ApprovePlan(ctx, planID string, approved bool, feedback string)`，先查找 plan record 获取 taskID 和 memberName，再做校验。

---

### S-04: StructuredAskUserTool schema 缺少 query 字段

**Go文件**: `internal/swarm/server/rails/structured_ask_user_tool.go:34`
**Python参考**: `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py:85-104`

**问题描述**: Python 的 `StructuredAskUserTool` 使用 `EXTENDED_INPUT_PARAMS`（包含 `query` + `questions` 两个字段），而 Go 使用 `BuildToolCard("ask_user", ...)` 从 `AskUserMetadataProvider` 获取 schema，**该 schema 只有 `questions` 字段，没有 `query` 字段**。这意味着 LLM 在纯文本查询模式下无法正确传递 `query` 参数。

**Python样例**:
```python
EXTENDED_INPUT_PARAMS_EN = {
    "type": "object",
    "properties": {
        "query": {"type": "string", "description": "The question to present to the user (required)."},
        "questions": {"type": "array", "description": "Structured questions...", "items": _QUESTIONS_ITEM_SCHEMA},
    },
    "required": ["query"],
}
```

**Go问题**:
```go
card, err := hprompts.BuildToolCard("ask_user", "ask_user", language, nil, agentID)
// AskUserMetadataProvider.GetInputParams() 只有 questions 字段
card.Description = getStructuredDescription(language)  // 只覆盖了描述，没有覆盖 schema
```

**修复方案**: 在 `NewStructuredAskUserTool` 中合并 `query` 字段到 `input_params`，或者像 Python 一样直接构建完整的 `EXTENDED_INPUT_PARAMS` schema。具体做法：在 `GetAskUserMetadataProviderInputParams` 中添加 `query` 属性，或在 `NewStructuredAskUserTool` 中手动将 `query` 属性追加到 card 的 `input_params`。

---

### S-05: AgentTool 中 modelCache 类型断言永远失败

**Go文件**: `internal/swarm/server/adapter/agent_tool.go:233-237`
**Python参考**: `jiuwenswarm/agents/harness/tools/code_agent_rail.py:198`

**问题描述**: `AgentTool.createSubAgent()` 通过 `t.parentAgent.(interface{ ModelCache() map[string]*llm.Model })` 获取 modelCache，但 `parentAgent` 类型是 `sainterfaces.BaseAgent`，`DeepAdapter` 未实现 `BaseAgent` 接口。此类型断言**永远返回 false**，modelCache 始终为 nil。当 agent 定义中指定了 model 名时，子 Agent 无法从缓存中查找对应模型实例。

**Go问题**:
```go
var modelCache map[string]*llm.Model
if t.parentAgent != nil {
    if v, ok := t.parentAgent.(interface{ ModelCache() map[string]*llm.Model }); ok {
        modelCache = v.ModelCache()  // 永远不会执行
    }
}
```

**修复方案**: 推荐：在 `AgentTool` 构造时直接注入 `modelCache`，不依赖运行时类型断言。修改 `NewAgentTool` 签名，增加 `modelCache map[string]*llm.Model` 参数，在 `CodeAgentRail.Init()` 中从 `DeepAdapter.ModelCache()` 获取并传入。

---

### S-06: TeamBackend.CleanTeam 不跳过自身检查

**Go文件**: `internal/agent_teams/tools/team_backend.go:553-586`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:665-727`

**问题描述**: Go 的 `CleanTeam` 检查所有非终态成员（包含自身），但 Python 的 `clean_team` 排除自身。Leader 自身通常不是 SHUTDOWN 状态，Go 的实现会导致 `clean_team` 永远无法成功（Leader 自己不是 SHUTDOWN 就返回 false）。

**Python样例**:
```python
for member_data in members:
    if member_data.member_name == self.member_name:
        continue  # 跳过自身
    if member_data.status != MemberStatus.SHUTDOWN.value:
        all_shutdown = False
        break
```

**Go问题**:
```go
for _, m := range members {
    // 没有跳过自身的逻辑
    if m.Status != string(atschema.MemberStatusShutdown) && ... {
        return false, nil  // Leader 未 SHUTDOWN → 永远返回 false
    }
}
```

**修复方案**: 在循环中添加 `if m.MemberName == tb.memberName { continue }`。

---

### S-07: DataIdManager 哈希算法不一致（hash vs fnv）

**Go文件**: `internal/agentcore/memory/manage/mem_model/data_id_manager.go:51-53`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/data_id_manager.py:13`

**问题描述**: Python 使用内置 `hash(user_id)`，Go 使用 `fnv.New32a()`。两者的哈希结果完全不同，意味着相同 user_id 在 Python 和 Go 会生成不同的 ID。如果存在跨语言数据交互，ID 不匹配将导致数据查找失败。

**Python样例**:
```python
h = hash(user_id) & 0xFFFFFF
```

**Go问题**:
```go
h := fnv.New32a()
_, _ = h.Write([]byte(userID))
hashVal := h.Sum32() & 0xFFFFFF
```

**修复方案**: 注意 Python 的 `hash()` 在不同进程间结果也不一致（PYTHONHASHSEED 随机化），所以 fnv 实际上更稳定。如果不需要跨语言 ID 兼容，当前实现可接受，但需要在注释中说明此差异。如果需要兼容，需统一使用确定性哈希（如 sha256 取前缀）。

---

### S-08: SqlMessageStore.generateMessageID 时间戳格式可能不一致

**Go文件**: `internal/agentcore/memory/manage/mem_model/sql_message_store.go:366-368`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/sql_message_store.py:43-46`

**问题描述**: Python 用 `f"{content_str}{timestamp}"` 拼接，其中 `timestamp` 是 Python datetime 对象的 `__str__()` 输出。Go 用 `timestamp.Format("2006-01-02 15:04:05-07:00")`。如果两端时间格式有任何微小差异（如 UTC 时间 Python 输出 `+00:00` 而 Go 输出 `Z`），将导致相同数据生成不同的 message_id，造成跨语言数据不可达。

**Python样例**:
```python
content_str = json.dumps(message.content, ensure_ascii=False)
message_hash = hashlib.sha256(f"{content_str}{timestamp}".encode()).hexdigest()
return f"msg_{message_hash[:16]}_{int(timestamp.timestamp()*1000)}"
```

**Go问题**:
```go
func generateMessageID(content string, timestamp time.Time) string {
    messageHash := sha256.Sum256([]byte(fmt.Sprintf("%s%s", content, timestamp.Format("2006-01-02 15:04:05-07:00"))))
    return fmt.Sprintf("msg_%x_%d", messageHash[:8], timestamp.UnixMilli())
}
```

**修复方案**: 统一使用 ISO 8601 / RFC3339 格式（两端一致），或在注释中标注此风险。注意 Python 的 `%x` 格式化与小写 hex 对应，Go 的 `%x` 也是小写，格式一致。

---

## 四、一般问题

### G-01: FragmentMemoryManager.AddMemories 缺少 llm 参数

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:67-68`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:125-126`

**问题描述**: Python 的 `add_memories` 签名包含 `llm: Model | None = None` 参数，传递给 `MemUpdateChecker.check(base_chat_model=llm)`。Go 的 `AddMemories` 方法签名中没有 `llm` 参数，导致 MemUpdateChecker 永远无法获取 LLM 实例。7.8 回填时必须添加此参数。

**修复方案**: 在 `AddMemories` 签名中增加 `llm` 参数（或等效的 Model 接口），并修改 `BaseMemoryManager` 接口。

---

### G-02: FragmentMemoryUnit 缺少 OperationType 的 Optional 语义

**Go文件**: `internal/agentcore/memory/manage/mem_model/memory_unit.go:28`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/memory_unit.py:37-41`

**问题描述**: Python `FragmentMemoryUnit.operation_type` 是 `Optional[OperationType] = None`，可以为空。Go 用 `OperationType` 值类型（零值为 `OperationTypeAdd=0`），无法区分"未设置"和"add"。

**修复方案**: 将 `OperationType` 改为 `*OperationType`（指针类型），nil 表示未设置。

---

### G-03: VariableUnit/SummaryUnit 缺少 mem_type 默认值约束

**Go文件**: `internal/agentcore/memory/manage/mem_model/memory_unit.go:34-55`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/memory_unit.py:44-57`

**问题描述**: Python 的 `VariableUnit` 强制 `mem_type=MemoryType.VARIABLE`，`SummaryUnit` 强制 `mem_type=MemoryType.SUMMARY`。Go 没有强制约束。

**修复方案**: 提供 `NewVariableUnit` / `NewSummaryUnit` 构造函数，内部设置默认值。

---

### G-04: AskUserMetadataProvider input_params required 字段过严

**Go文件**: `internal/agentcore/harness/prompts/tools/ask_user.go:72-77`
**Python参考**: `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py:47-83`

**问题描述**: Python 的 `_QUESTIONS_ITEM_SCHEMA` 中 `required: ["question"]`（只有 question 必填），options item 的 `required: ["label"]`。Go 中 questions 的 required 是 `["header", "question", "options"]`，options item 的 required 是 `["label", "description"]`。header/options/description 必填限制过严，LLM 调用可能失败。

**修复方案**: 修改 `GetAskUserMetadataProviderInputParams` 中的 required 字段，对齐 Python：questions item 的 required 改为 `["question"]`，options item 的 required 改为 `["label"]`。

---

### G-05: FragmentMemoryManager.Update 中 time.Now() 未使用 UTC 时区

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:191`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:211`

**问题描述**: Python 使用 `datetime.now(timezone.utc).astimezone()` 确保时间戳带 UTC 时区。Go 使用 `time.Now()` 返回本地时区时间。`parseTimestamp` 的 fallback 也有同样问题。

**修复方案**: 将 `time.Now()` 改为 `time.Now().UTC()` 对齐 Python 的 UTC 行为。

---

### G-06: ListFragmentMemories 缺少排序逻辑

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:293`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:329`

**问题描述**: Python 的 `list_fragment_memories` 在返回前按 `(mem, timestamp)` 降序排序。Go 直接返回底层结果，没有排序。

**修复方案**: 在返回前添加排序逻辑，对齐 Python。

---

### G-07: ListFragmentMemories 缺少 mem_type 合法性校验

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:286-291`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:310-318`

**问题描述**: Python 校验 `mem_type.value` 是否在 `FRAGMENT_MEMORY_TYPE` 中，不合法时记录 Error 日志并返回空列表。Go 没有合法性校验。

**修复方案**: 在 `memType != ""` 分支中添加 `isFragmentMemoryType` 校验。

---

### G-08: ShutdownMember 幂等路径缺失

**Go文件**: `internal/agent_teams/tools/team_backend.go:394-397`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:543-549`

**问题描述**: Python 对 `SHUTDOWN` 和 `SHUTDOWN_REQUESTED` 状态返回成功（幂等），Go 对这些状态返回失败。

**修复方案**: `SHUTDOWN` 和 `SHUTDOWN_REQUESTED` 应返回 `MemberOpResultSuccess()`。

---

### G-09: CancelTask 缺少消息通知步骤

**Go文件**: `internal/agent_teams/tools/team_backend.go:618-640`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:851-896`

**问题描述**: Python 的 `cancel_task` 在事件之外还通过 `message_manager.send_message` 发送取消通知消息给 assignee。Go 只有事件通知。

**修复方案**: 在事件发布前调用 `tb.messageManager.SendMessage`。

---

### G-10: CancelAllTasks 缺少广播消息步骤

**Go文件**: `internal/agent_teams/tools/team_backend.go:644-651`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:898-936`

**问题描述**: Python 在取消后发送广播消息通知所有成员。Go 只有事件。

**修复方案**: 在取消后调用 `tb.messageManager.BroadcastMessage`。

---

### G-11: IsTeamCompleted 缺少"至少一个任务/成员"的检查

**Go文件**: `internal/agent_teams/tools/team_backend.go:252-276`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:791-830`

**问题描述**: Go 的 `IsTeamCompleted` 在没有任务或没有成员时返回 `nil, nil`（隐式完成），但 Python 明确检查 `if not tasks: return None` / `if not members: return None`，即没有任务或成员时不认为团队完成。

**修复方案**: 在步骤 3 后添加 `if len(tasks) == 0 { return nil, nil }`，步骤 2 后添加 `if len(members) == 0 { return nil, nil }`。

---

### G-12: ForceCleanTeam 使用 UpdateMemberStatus 而非 ShutdownMember

**Go文件**: `internal/agent_teams/tools/team_backend.go:590-612`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:729-763`

**问题描述**: Python 的 `force_clean_team` 对每个非自身成员调用 `shutdown_member(force=True)`，这会触发完整的关闭流程（消息发送+事件发布）。Go 直接 `UpdateMemberStatus` 跳过了消息和事件，也不跳过自身。

**修复方案**: 改为调用 `ShutdownMember(ctx, memberName, true)` 并跳过自身。

---

### G-13: SkillToolkit.InstallSkill 中 skillnet 名称推断缺失 Path(target).name 逻辑

**Go文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go:377-379`
**Python参考**: `jiuwenswarm/agents/harness/tools/skill_toolkits.py:409-412`

**问题描述**: 当 skill.name 为空时，Python 使用 `Path(target).name` 从 URL 中提取最后一段作为名称，Go 直接使用完整 identifier。

**修复方案**:
```go
if name == "" {
    if normalizedSource == "skillnet" {
        name = filepath.Base(identifier)
    } else {
        name = identifier
    }
}
```

---

### G-14: SkillToolkit 三个公开方法缺少顶层异常兜底

**Go文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go`
**Python参考**: `jiuwenswarm/agents/harness/tools/skill_toolkits.py:279-281`

**问题描述**: Python 的 `search_skill`/`install_skill`/`uninstall_skill` 整体包裹在 `try/except Exception` 中。Go 版本没有 `defer/recover` 兜底，未预期的 panic 会导致工具调用失败。

**修复方案**: 在三个方法中添加 `defer func() { if r := recover(); r != nil { ... } }()` 兜底。

---

### G-15: CodeAgentRail.Reload 依赖类型断言获取 *CodeAgentRail

**Go文件**: `internal/swarm/server/adapter/code_adapter.go:391`

**问题描述**: `c.codeAgentRail.(*CodeAgentRail)` 类型断言依赖运行时类型匹配。如果 `buildCodeAgentRail()` 被修改返回其他实现，断言就会静默失败。

**修复方案**: 将 `c.codeAgentRail` 字段类型从 `sainterfaces.AgentRail` 改为 `*CodeAgentRail`。

---

### G-16: StructuredAskUserRail 空字符串在非结构化路径行为不一致

**Go文件**: `internal/swarm/server/rails/structured_ask_user_rail.go:264-268`
**Python参考**: `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py:327-328`

**问题描述**: Python 非结构化路径中空字符串通过 `self.reject(tool_result=user_input)` 直接 reject（返回空字符串结果）。Go 的非结构化路径只处理非空字符串，空字符串会落入 `parentResolve`。

**修复方案**: 在非结构化路径中，空字符串也应走 `Reject`：`if strInput, ok := userInput.(string); ok { return r.AskUserRail.BaseInterruptRail.Reject(strInput) }`。

---

### G-17: StructuredAskUserRail.parseStructuredInput 处理 AskUserPayload 时缺少 answer 字段兼容

**Go文件**: `internal/swarm/server/rails/structured_ask_user_rail.go:280-283`
**Python参考**: `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py:282-292`

**问题描述**: Python 先检查 `free_text = getattr(user_input, "answer", None)` 兼容旧格式。Go 直接透传 `input.Answers`，没有兼容旧格式 `answer` 字段。

**修复方案**: 在 `AskUserPayload` 结构体中增加 `Answer string` 字段（`json:"answer,omitempty"`），或在 `parseStructuredInput` 中对 `map[string]any` 输入额外检查 `answer` 键。

---

### G-18: PromptPriority 常量不完整

**Go文件**: `internal/swarm/agents/harness/common/prompt/prompt_builder.go:20-21`
**Python参考**: `jiuwenswarm/agents/harness/common/prompt/prompt_builder.py:22-30`

**问题描述**: Python 定义了 IDENTITY=10, SKILLS=40, MEMORY=55, RESPONSE=60, WORKSPACE=70, TODO=85 共6个优先级常量。Go 仅定义了 `responsePriority=60` 一个。

**修复方案**: 补充完整的 PromptPriority 常量集。

---

### G-19: SupportMemoryType 枚举缺失

**Go文件**: `internal/agentcore/memory/manage/mem_model/memory_unit.go`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/memory_unit.py:24-27`

**问题描述**: Python 定义了 `SupportMemoryType` 枚举（仅含 USER_PROFILE 和 SUMMARY），Go 完全缺失。

**修复方案**: 在 `memory_unit.go` 中补充 `SupportMemoryType` 枚举定义。

---

### G-20: SqlDbStore applyConditions 不支持具体切片类型的 IN 子句

**Go文件**: `internal/agentcore/memory/manage/mem_model/sql_db_store.go:611-614`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/sql_db_store.py:160-161`

**问题描述**: Python `update()`/`delete()` 中条件值为 list 时使用 IN 子句。Go 的 `applyConditions` 只处理了 `[]any`（IN）和默认（等值），`[]string` 等具体切片类型会走到等值匹配分支，导致 SQL 错误。

**修复方案**: `applyConditions` 中增加 `[]string, []int, []int64, []float64` 的 case。

---

### G-21: RefreshHumanAgentRoster 缺少 db.Initialize() 调用

**Go文件**: `internal/agent_teams/tools/team_backend.go:708-721`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:1156-1190`

**问题描述**: Python 的 `refresh_human_agent_roster` 先调用 `db.initialize()` 确保 DAO 就绪，Go 版本跳过了此步骤。

**修复方案**: 在调用 `ListHumanAgentNames` 之前先调用 `tb.db.Initialize(ctx)`。

---

### G-22: AgentTool.createSubAgent 调用 CreateDeepAgent 时使用 context.Background()

**Go文件**: `internal/swarm/server/adapter/agent_tool.go:366`

**问题描述**: 使用了新的 Background context，而非从 Invoke 传入的 ctx。如果父请求被 cancel（如超时），子 Agent 创建不会被取消。

**修复方案**: 将 `ctx` 传入 `createSubAgent` 方法签名，使用 `harness.CreateDeepAgent(ctx, params)`。

---

## 五、提示问题

### T-01: FragmentMemoryManager.search 可能缺少排序截断

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:211`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:239`

**问题描述**: Python 的 `search` 方法在返回前按 score 降序排序并截断 top_k。Go 直接返回底层 `memoryIndex.Search()` 的结果。需确认底层是否已排序。

---

### T-02: FragmentMemoryManager.delete 日志 memory_id 类型差异

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:249`

**问题描述**: Python 传 `[mem_id]`（列表），Go 传 `memID`（字符串）。功能等价但格式不一致。

---

### T-03: FragmentMemoryManager.parseTimestamp fallback 未 UTC

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:413,427`

**问题描述**: Python fallback 使用 `datetime.now(timezone.utc).astimezone()`，Go 使用 `time.Now()`。

**修复方案**: 改为 `time.Now().UTC()`。

---

### T-04: SemanticStore 和 UserMemStore 完全缺失

**Go文件**: 无
**Python参考**: `openjiuwen/core/memory/manage/mem_model/semantic_store.py`, `user_mem_store.py`

**问题描述**: Python 中 `SemanticStore` 和 `UserMemStore` 在 Go 端完全没有对应实现。需确认是否计划移植。

---

### T-05: SqlDbStore.Get 硬编码 `id` 列名

**Go文件**: `internal/agentcore/memory/manage/mem_model/sql_db_store.go:517`

**问题描述**: 主键列名硬编码为 `id`，但某些表主键列名是 `message_id` 而非 `id`。

---

### T-06: SqlDbStore.ConditionGet 类型校验过严

**Go文件**: `internal/agentcore/memory/manage/mem_model/sql_db_store.go:100-107`

**问题描述**: 枚举了 5 种切片类型但遗漏了 `[]byte` 等。建议用 reflect 判断。

---

### T-07: CodeAdapter section 添加顺序与 Python 不一致

**Go文件**: `internal/swarm/agents/harness/code/prompt/code_prompt_builder.go:55-62`
**Python参考**: `jiuwenswarm/agents/harness/code/prompt/code_prompt_builder.py:600-609`

**问题描述**: `session_guidance` 在 Go 中放在最后，Python 中在第 3 位。由于 Builder 按优先级排序，最终输出一致，但添加顺序不匹配。

---

### T-08: 品牌名 JiuwenSwarm → UapClaw

**Go文件**: `internal/swarm/agents/harness/common/prompt/identity.go:44,47`

**问题描述**: 项目规则要求"提示词一比一复刻 Python 原文"，品牌名从 JiuwenSwarm 改为 UapClaw。如是有意改名需在注释中标注。

---

### T-09: readWorkspaceFile 缺少 .strip() 对齐

**Go文件**: `internal/swarm/agents/harness/common/prompt/prompt_builder.go:139-143`
**Python参考**: `jiuwenswarm/agents/harness/common/prompt/prompt_builder.py:270-273`

**问题描述**: Python 对读取内容调用 `.strip()`，Go 直接返回原始内容。

**修复方案**: `content := strings.TrimSpace(string(data))`

---

### T-10: readWorkspaceFile FileNotFoundError 时缺少 debug 日志

**Go文件**: `internal/swarm/agents/harness/common/prompt/prompt_builder.go:130-131`

**问题描述**: Python 记录 `logger.debug`，Go 静默跳过。

---

### T-11: Python SectionName 缺少 RESPONSE，Go 额外添加

**问题描述**: Go 的合理改进，无需修改。

---

### T-12: SkillToolkit.InstallSkill teamskillshub 安装额外 market_url 参数

**Go文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go:354-358`

**问题描述**: Go 额外添加了 Python 不存在的 `market_url` 参数。需确认是否有意为之。

---

### T-13: AgentTool.Invoke 缺少 Python 中对 inputs 非 dict 类型的兼容

**Go文件**: `internal/swarm/server/adapter/agent_tool.go:94-100`

**问题描述**: Python 同时处理 dict 和 object 两种 inputs 类型。Go 只处理 `map[string]any`。

---

### T-14: StructuredAskUserRail._build_ask_request 未被覆盖

**问题描述**: Python 显式覆盖但只是调用 super()，Go 直接调用。功能等价。

---

### T-15: ScopeUserMappingManager.Add 空 string 处理

**问题描述**: Python 用 `or ''` 将 None 转空字符串，Go 的 string 不会是 nil。行为实际一致，无 bug。

---

## 六、⤵️ 占位代码真实状态验证

| 位置 | 占位内容 | 真实状态 |
|------|---------|---------|
| `manage/update/update_checker.go:33` | MemUpdateChecker 7.8 LLM 冲突检查 | ✅ 确认 stub，直接返回 ADD |
| `manage/mem_model/data_id_manager.go` | 无占位 | 哈希算法差异（S-07） |
| `tools/database/team_dao.go` | 9.65a DAO | ✅ 确认占位文件 |
| `tools/database/engine.go:45-59` | 9.65a-5 SQL 实现 | ✅ 确认 stub |
| `adapter/code_adapter.go:56-65` | LspRail/ProjectMemoryRail/WorktreeRail | ✅ 确认返回 nil |
| `adapter/deep_adapter_rails.go:248-316` | 10 个未实现 Rail | ✅ 确认返回 nil |
| `adapter/deep_adapter_tools.go:525-710` | wiki/image_gen/小艺/acp_chat 工具 | ✅ 确认未实现 |

所有 ⤵️ 标记与实际代码一致，没有遗漏或误标。

---

## 七、修复优先级建议

### P0（立即修复 — 功能性 bug）

| 编号 | 问题 | 影响范围 |
|------|------|---------|
| S-04 | StructuredAskUserTool 缺少 query 字段 | ask_user 工具纯文本模式失效 |
| S-06 | CleanTeam 不跳过自身 | clean_team 永远无法成功 |
| S-02 | CancelMember 语义偏离 | cancel 误修改成员状态+缺少消息通知 |
| S-01 | ShutdownMember 缺少 send_message | 跨进程关闭通知缺失 |
| S-05 | modelCache 类型断言永远失败 | 子 Agent 无法使用指定模型 |

### P1（近期修复 — 逻辑偏差）

| 编号 | 问题 |
|------|------|
| S-03 | ApprovePlan 参数类型错误 |
| G-04 | input_params required 过严 |
| G-08 | ShutdownMember 幂等路径 |
| G-11 | IsTeamCompleted 缺少空检查 |
| G-12 | ForceCleanTeam 跳过消息和自身 |
| G-09/10 | CancelTask/CancelAllTasks 缺少消息通知 |
| G-01 | AddMemories 缺少 llm 参数 |

### P2（后续修复 — 规范对齐）

| 编号 | 问题 |
|------|------|
| S-07/S-08 | 哈希/ID 生成跨语言兼容 |
| G-02/03 | OperationType Optional / 默认值约束 |
| G-05/06/07 | FragmentMemoryManager UTC/排序/校验 |
| G-16/17 | StructuredAskUserRail 兼容性 |
| G-13/14/15 | SkillToolkit/CodeAgentRail 对齐 |
| G-19/20 | SupportMemoryType / IN 子句 |
