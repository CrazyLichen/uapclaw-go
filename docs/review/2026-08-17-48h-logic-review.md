# 48小时代码逻辑审查报告

> 审查时间：2026-08-17
> 审查范围：48-72小时内提交（2026-08-15 ~ 2026-08-17）
> 涉及章节：7.6 FragmentMemoryManager、7.9 Memory数据模型、10.6.3 StructuredAskUserRail、9.65a-4 TeamBackend、9.50 SkillToolkit回填、9.64 Team Memory、interrupt.ParseToolArgs导出

---

## 一、审查概览

| 章节 | 提交数 | 严重 | 一般 | 提示 |
|------|--------|------|------|------|
| 7.6 FragmentMemoryManager | 5 | 4 | 4 | 3 |
| 7.9 Memory数据模型 | 2 | 2 | 5 | 4 |
| 10.6.3 StructuredAskUserRail | 5 | 1 | 3 | 3 |
| 9.65a-4 TeamBackend | 3 | 6 | 4 | 3 |
| 9.50 SkillToolkit回填 | 2 | 0 | 3 | 4 |
| 9.64 Team Memory | 1 | 3 | 4 | 2 |
| interrupt.ParseToolArgs | 1 | 0 | 1 | 1 |
| **合计** | — | **16** | **24** | **20** |

---

## 二、严重问题（功能逻辑缺陷）

### S-01：TeamBackend.CancelMember 错误修改成员状态

**章节**：9.65a-4 TeamBackend
**文件**：`internal/agent_teams/tools/team_backend.go:426-455`

**Python 行为**：
```python
# team.py:600-663
# cancel_member 只对 BUSY 状态成员操作，不修改成员状态
# 只重置 CLAIMED 任务 + 发送 cancel_request 消息 + 发布事件
if current_status != MemberStatus.BUSY:
    return True  # 非 BUSY 直接返回成功

# 重置 CLAIMED 任务
self._task_manager.reset_tasks_by_assignee(member_name, [TaskStatus.CLAIMED])

# 发送 cancel_request 消息通知成员
self._message_manager.send_message(
    content=t("team.cancel_request_content"),
    to_member_name=member_name
)

# 发布事件
self._event_publisher.publish(MemberCanceledEvent(...))
```

**Go 问题**：
```go
// team_backend.go:426-455
// 错误1: CAS 将成员状态改为 SHUTDOWN_REQUESTED（Python 不修改状态！）
result, err := db.Member().TryTransitionMemberStatus(ctx, memberName, current, schema.MemberStatusShutdownRequested)
// 错误2: 对所有非终态成员执行，Python 只对 BUSY 成员执行
// 错误3: 缺失发送 cancel_request 消息
```

**影响**：CancelMember 语义完全偏离 Python，被取消的成员状态被改为 SHUTDOWN_REQUESTED，与 Python 行为不一致。后续逻辑可能依赖成员状态做判断，导致连锁错误。

**修复方案**：
1. 移除 CAS 状态转换逻辑，CancelMember 不应修改成员状态
2. 只对 BUSY 成员执行操作，非 BUSY 返回成功
3. 添加发送 cancel_request 消息步骤
4. 添加 MemberCanceledEvent 事件发布

---

### S-02：TeamBackend.ShutdownMember 幂等语义错误 + 缺失消息通知

**章节**：9.65a-4 TeamBackend
**文件**：`internal/agent_teams/tools/team_backend.go:387-414`

**Python 行为**：
```python
# team.py:514-598
# SHUTDOWN_REQUESTED 状态是幂等成功
if current_status in (MemberStatus.SHUTDOWN, MemberStatus.SHUTDOWN_REQUESTED):
    return MemberOpResult(success=True)  # 幂等成功

# 发送 shutdown_request 消息通知成员
self._message_manager.send_message(
    content=t("team.shutdown_request_content"),
    to_member_name=member_name
)

# 发布事件
self._event_publisher.publish(MemberShutdownEvent(...))
```

**Go 问题**：
```go
// team_backend.go:387-414
// 错误1: SHUTDOWN_REQUESTED 状态返回 fail（Python 返回 success 幂等）
if current == schema.MemberStatusShutdown || current == schema.MemberStatusShutdownRequested {
    return failResult(...)  // 应为 success
}
// 错误2: 缺失发送 shutdown_request 消息
// 错误3: 额外取消任务 CancelAllTasks（Python shutdown_member 不取消任务）
```

**影响**：
1. 重复调用 ShutdownMember 会失败，与 Python 幂等行为不同
2. 被关闭的成员收不到通知消息，可能继续执行任务
3. 不应取消任务（cancel 和 shutdown 是不同语义）

**修复方案**：
1. SHUTDOWN_REQUESTED 状态返回成功（幂等）
2. 移除 CancelAllTasks 调用
3. 添加发送 shutdown_request 消息步骤

---

### S-03：TeamBackend.Startup 缺失 on_created 回调和 MemberSpawnedEvent

**章节**：9.65a-4 TeamBackend
**文件**：`internal/agent_teams/tools/team_backend.go:344-375`

**Python 行为**：
```python
# team.py:336-396
def startup(self, on_created=None):
    for member in unstarted_members:
        self.startup_member(member.member_name, on_created=on_created)

def startup_member(self, member_name, on_created=None):
    # CAS UNSTARTED→STARTING
    # 调用 on_created 回调实际启动 agent 进程
    if on_created:
        on_created(member_name)
    # 发布 MemberSpawnedEvent
    self._event_publisher.publish(MemberSpawnedEvent(...))

def _spawn_and_publish(self, member_name, on_created):
    try:
        on_created(member_name)
    except Exception:
        # 启动失败回滚 STARTING→UNSTARTED
        self.update_member_status(member_name, MemberStatus.UNSTARTED)
```

**Go 问题**：
```go
// team_backend.go:344-375
// Startup(): 查询 UNSTARTED 成员，CAS 转换 UNSTARTED→STARTING，返回启动列表
// StartupMember(): 仅做 CAS 转换
// 完全缺失 on_created 回调、MemberSpawnedEvent 事件发布、启动失败回滚
```

**影响**：Startup 是团队创建后实际启动 agent 进程的关键入口。缺失 on_created 回调意味着 Go 版本的 Startup 仅仅修改了状态，没有真正启动任何 agent。

**修复方案**：
1. Startup/StartupMember 增加 `onCreated func(memberName string) error` 参数
2. CAS 成功后调用 onCreated 回调
3. 回调失败时回滚状态 STARTING→UNSTARTED
4. 发布 MemberSpawnedEvent

---

### S-04：TeamBackend.CleanTeam 不排除自身 + 判定条件差异

**章节**：9.65a-4 TeamBackend
**文件**：`internal/agent_teams/tools/team_backend.go:553-586`

**Python 行为**：
```python
# team.py:665-727
def is_team_cleaned(self):
    for member_data in self._member_dao.list_members(...):
        if member_data.member_name == self.member_name:
            continue  # 排除自身（Leader）
        if member_data.status != MemberStatus.SHUTDOWN:
            return False
    return True
```

**Go 问题**：
```go
// team_backend.go:553-586
// 不排除自身，且额外允许 ERROR 状态
for _, member := range members {
    if member.Status != schema.MemberStatusShutdown && member.Status != schema.MemberStatusError {
        return false, nil
    }
}
```

**影响**：Leader 自身状态是 BUSY/RUNNING，Go 版本因为不排除自身，CleanTeam 永远返回 false（Leader 不是 SHUTDOWN/ERROR）。Python 版本排除自身后才能在 Leader 仍活跃时判断其他成员是否都已关闭。

**修复方案**：
1. 遍历时排除自身成员（`member.MemberName == tb.memberName`）
2. 判定条件改为只检查 SHUTDOWN（与 Python 对齐，移除 ERROR 检查）

---

### S-05：TeamBackend.approve_plan 使用 taskID 而非 planID

**章节**：9.65a-4 TeamBackend
**文件**：`internal/agent_teams/tools/team_backend.go:655-673`

**Python 行为**：
```python
# team.py:398-462
def approve_plan(self, plan_id, approved=True, feedback=None):
    plan_record = self._task_manager.get_plan_record(plan_id)
    member_name = plan_record.member_name
    task_id = plan_record.task_id
    # 校验 member 存在
    self._task_manager.approve_plan(plan_id, approved, feedback, leader_name)
```

**Go 问题**：
```go
// team_backend.go:655-673
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) MemberOpResult {
    // 直接传 taskID，没有 planID 概念
    result, err := tb.taskManager.ApprovePlan(ctx, taskID, true, "")
    // 硬编码 approved=true，不支持拒绝和 feedback
    // 缺失 plan_record 查找和 member 校验
}
```

**影响**：planID 和 taskID 是不同概念。Python 中一个 task 可以有多个 plan（审批记录），Go 混淆了这两个概念，且不支持拒绝（approved=false）和反馈。

**修复方案**：
1. 签名改为 `ApprovePlan(ctx, planID string, approved bool, feedback string)`
2. 先查 plan_record 获取 member_name 和 task_id
3. 校验 member 存在后调用 taskManager.ApprovePlan

---

### S-06：TeamBackend.is_team_completed 空任务/空成员判定缺失

**章节**：9.65a-4 TeamBackend
**文件**：`internal/agent_teams/tools/team_backend.go:240-276`

**Python 行为**：
```python
# team.py:791-828
tasks = self._task_manager.list_tasks(...)
if not tasks:
    return None  # 无任务时无法判定，返回 None

members = self._member_dao.list_members(...)
if not members:
    return None  # 无成员时无法判定，返回 None
```

**Go 问题**：
```go
// Go 不检查空任务/空成员，空集合的 all() 为 true
// 导致无任务时误判为 completed
```

**影响**：团队刚创建、尚无任务时，is_team_completed 返回 completed，与 Python 行为不同（Python 返回 None 表示无法判定）。

**修复方案**：添加空任务和空成员检查，返回 nil 表示无法判定。

---

### S-07：TeamMemoryManager.init_toolkit 缺少 read_only_source → Workspace 覆盖逻辑

**章节**：9.64 Team Memory
**文件**：`internal/agent_teams/memory/manager.go:91-117`

**Python 行为**：
```python
# manager.py:74-77
if self._read_only_source:
    self._workspace = Workspace(root_path=self._read_only_source)
else:
    self._workspace = params.workspace
```

**Go 问题**：
```go
// manager.go:91-117
// 直接使用 params.Workspace，完全没有 read_only_source 覆盖逻辑
```

**影响**：当 ReadOnlySourceWorkspace 非空时，Go 端成员可能读不到正确的只读来源工作空间内容，导致共享记忆来源错误。

**修复方案**：
```go
if m.readOnlySource != "" {
    m.workspace = workspace.NewWorkspace(workspace.WithRootPath(m.readOnlySource))
} else {
    m.workspace = params.Workspace
}
```

---

### S-08：FragmentMemoryManager.AddMemories 缺少 llm 参数

**章节**：7.6 FragmentMemoryManager
**文件**：`internal/agentcore/memory/manage/base_manager.go`, `fragment_manager.go`

**Python 行为**：
```python
# base_memory_manager.py
def add_memories(self, user_id, scope_id, memories, llm=None, **kwargs):
    # llm 参数传给 MemUpdateChecker.Check() 的 base_chat_model
    checker.check(new_memories=..., old_memories=..., base_chat_model=llm)
```

**Go 问题**：
```go
// base_manager.go
AddMemories(ctx context.Context, userID string, scopeID string,
    memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error)
// 无 llm 参数
```

**影响**：即使 7.8 回填完成 MemUpdateChecker LLM 逻辑，Go 的 AddMemories 也无法传入 LLM model，冲突检查永远走 stub 路径（全部返回 ADD），导致记忆冲突无法被检测。

**修复方案**：
1. BaseMemoryManager 接口 AddMemories 增加 `model *llm.Model` 可选参数
2. FragmentMemoryManager 实现同步增加
3. MemUpdateChecker.Check 也需增加 `baseChatModel *llm.Model` 参数

---

### S-09：BaseMemoryManager.AddMemories 签名硬编码 FragmentMemoryUnit

**章节**：7.6 FragmentMemoryManager
**文件**：`internal/agentcore/memory/manage/base_manager.go`

**Python 行为**：
```python
# base_memory_manager.py
def add_memories(self, user_id, scope_id, memories: dict[str, list[BaseMemoryUnit]]):
    # memories value 是 BaseMemoryUnit 基类列表
    # FragmentMemoryManager 传入 FragmentMemoryUnit（继承 BaseMemoryUnit）
    # SummaryManager 传入 SummaryUnit
    # VariableManager 传入 VariableUnit
```

**Go 问题**：
```go
// base_manager.go
AddMemories(ctx context.Context, userID string, scopeID string,
    memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error)
// 硬编码为 FragmentMemoryUnit，SummaryManager/VariableManager 无法使用同一接口
```

**影响**：SummaryManager（7.7）和 VariableManager（7.7）实现时，无法使用同一 BaseMemoryManager 接口，必须修改接口签名或定义新接口。

**修复方案**：将 AddMemories 的 value 类型改为 `[]*mem_model.BaseMemoryUnit` 或定义 `MemoryUnit` 接口。

---

### S-10：MemUpdateChecker.Check 缺少 model 参数，回填时连锁修改

**章节**：7.6 FragmentMemoryManager
**文件**：`internal/agentcore/memory/manage/update/update_checker.go:79`

**Python 行为**：
```python
# mem_update_checker.py:126-132
def check(self, new_memories, old_memories, base_chat_model, retries=3):
    if base_chat_model is None:
        return [MemoryActionItem(mem_id=k, action="ADD") for k in new_memories]
    # 有 model 时，使用 LLM 做冲突检查
```

**Go 问题**：
```go
// update_checker.go:79
func (c *MemUpdateChecker) Check(newMemories, oldMemories) ([]*MemoryActionItem, error)
// 无 model 参数，7.8 回填时必须修改签名
```

**影响**：当前 stub 签名已固化，FragmentMemoryManager 已依赖。7.8 回填时修改 Check 签名会引发连锁修改。

**修复方案**：现在就预留 `baseChatModel *llm.Model` 参数（传 nil 走 stub），避免回填时破坏接口。

---

### S-11：FragmentMemoryManager.Search 缺少排序和截断

**章节**：7.6 FragmentMemoryManager
**文件**：`internal/agentcore/memory/manage/fragment_manager.go`

**Python 行为**：
```python
# fragment_memory_manager.py:230-240
result.sort(key=lambda x: x["score"], reverse=True)
return result[:top_k]
```

**Go 问题**：
```go
// Go 的 Search 直接返回 index 层结果，无排序无截断
return results, nil
```

**影响**：Go 的 Search 结果可能未按 score 排序，也未截断到 topK，返回结果数量和顺序与 Python 不一致。

**修复方案**：
```go
sort.Slice(results, func(i, j int) bool {
    return results[i].Score > results[j].Score
})
if topK > 0 && len(results) > topK {
    results = results[:topK]
}
```

---

### S-12：FragmentMemoryManager.ListFragmentMemories 缺少校验和排序

**章节**：7.6 FragmentMemoryManager
**文件**：`internal/agentcore/memory/manage/fragment_manager.go`

**Python 行为**：
```python
# fragment_memory_manager.py:300-334
# 1. 校验 mem_type 是否合法
if mem_type and mem_type not in FRAGMENT_MEMORY_TYPE:
    logger.error(f"Invalid mem_type: {mem_type}")
    return []
# 2. 按 (mem, timestamp) 降序排序
result.sort(key=lambda x: (x['mem'], str(x.get('timestamp') or '')), reverse=True)
```

**Go 问题**：
```go
// Go 无 memType 合法性校验，无排序，直接返回 docs
```

**影响**：任意 memType 字符串都直接传给 ListMemories，可能导致查询到非预期的记忆类型。返回结果无排序，与 Python 行为不一致。

**修复方案**：
1. 添加 memType 合法性校验（不在 FragmentMemoryTypes 中则返回空）
2. 添加按 Text+Timestamp 降序排序

---

### S-13：SupportMemoryType 枚举缺失

**章节**：7.9 Memory数据模型
**文件**：`internal/agentcore/memory/manage/mem_model/memory_unit.go`

**Python 行为**：
```python
# mem_model/memory_unit.py
class SupportMemoryType(str, Enum):
    USER_PROFILE = "user_profile"
    SUMMARY = "summary"
```

**Go 问题**：`SupportMemoryType` 枚举完全缺失。该枚举在 Python 的 `vector_migrator.py` 中被使用，用于确定哪些 MemoryType 支持向量存储迁移。

**修复方案**：添加 `SupportMemoryType` 枚举及对应的值常量。

---

### S-14：sql_message_store message_id 生成算法跨语言不兼容

**章节**：7.9 Memory数据模型
**文件**：`internal/agentcore/memory/manage/mem_model/sql_message_store.go`

**Python 行为**：
```python
# Python datetime.__str__() 返回 ISO 格式：2024-01-15T10:30:00+08:00（T 分隔符）
content_str = json.dumps(message.content, ensure_ascii=False)
hashlib.sha256(f"{content_str}{timestamp}".encode()).hexdigest()[:16]
```

**Go 问题**：
```go
// Go 使用空格分隔：2006-01-02 15:04:05-07:00
// 且 json.Marshal 会转义 HTML 特殊字符、不保证 key 顺序
sha256.Sum256([]byte(fmt.Sprintf("%s%s", content, timestamp.Format("2006-01-02 15:04:05-07:00"))))
```

**影响**：Go 和 Python 对同一消息生成不同的 message_id。如果不需要跨语言数据兼容可接受，但应在注释中明确说明。如果存在共享数据库场景，将导致消息重复。

**修复方案**：如果需要跨语言兼容，对齐 Python 的时间格式为 ISO（T 分隔符）和 json 序列化行为。否则添加注释说明此差异。

---

### S-15：StructuredAskUserTool 缺少 query 参数

**章节**：10.6.3 StructuredAskUserRail
**文件**：`internal/swarm/server/rails/structured_ask_user_tool.go`

**Python 行为**：
```python
# ask_user_rail.py:85-123
EXTENDED_INPUT_PARAMS = {
    "type": "object",
    "properties": {
        "query": {"type": "string", "description": "The question to ask the user"},
        "questions": {"type": "array", ...}
    },
    "required": ["query"]  # query 是必填的
}
```

**Go 问题**：
```go
// Go 复用基础 AskUserMetadataProvider 的 schema
// 只有 questions 参数，缺少 query 参数
// required: ["questions"]（Python: required: ["query"]）
```

**影响**：LLM 看到的工具 schema 与 Python 不一致。Go 版本让 LLM 以为 questions 是必填参数而非 query，可能导致工具调用行为偏差（LLM 可能不传 query 而只传 questions，或反之）。

**修复方案**：`NewStructuredAskUserTool` 应使用自定义 schema（包含 `query` + `questions` 两个参数，`required: ["query"]`），而非复用基础 AskUserMetadataProvider 的 schema。

---

### S-16：FragmentMemoryManager._process_conflict_info 方法缺失

**章节**：7.6 FragmentMemoryManager
**文件**：`internal/agentcore/memory/manage/fragment_manager.go`

**Python 行为**：
```python
# fragment_memory_manager.py:82-101
@staticmethod
def _process_conflict_info(conflict_info, input_memory_ids_map):
    """将 LLM 返回的数字 ID 映射回真实 mem_id"""
    for item in conflict_info:
        if "id" in item and isinstance(item["id"], int):
            item["id"] = input_memory_ids_map.get(item["id"], item["id"])
    return conflict_info
```

**Go 问题**：完全缺失。MemUpdateChecker 回填完成后，LLM 返回的冲突信息中的 ID 是整数索引，需要映射回真实的 mem_id 字符串，否则冲突处理结果将无法正确关联。

**修复方案**：添加 `processConflictInfo` 方法，7.8 回填时必须补上。

---

## 三、一般问题

### G-01：TeamBackend.cancel_task 缺失消息通知

**章节**：9.65a-4
**Python 发消息通知 assignee，Go 发事件。** Python 通过 `message_manager.send_message` 向被取消任务的 assignee 发送通知消息，Go 只发布 TaskUnblockedEvent。建议添加消息通知。

### G-02：TeamBackend.cancel_all_tasks 缺失广播消息

**章节**：9.65a-4
**Python 广播消息通知全员，Go 只调用 CancelAllTasks。** Python 取消后调用 `message_manager.broadcast_message`，Go 缺失此步骤。

### G-03：TeamBackend.BuildTeam/SpawnHumanAgent 缺失 AgentCard 和 i18n

**章节**：9.65a-4
- BuildTeam 中 Leader 的 agentCard 传空字符串，Python 传完整 AgentCard JSON
- SpawnHumanAgent 缺失 i18n 默认值（`t("hitt.human_agent_display_name")` 等）

### G-04：TeamBackend.ForceCleanTeam 不排除自身

**章节**：9.65a-4
Go 直接设所有非 SHUTDOWN 成员为 SHUTDOWN，不排除自身。Python 排除自身后对其他成员调用 `shutdown_member(force=True)` 走正规流程。

### G-05：VariableUnit/SummaryUnit 缺少 mem_type 强制约束

**章节**：7.9
Python 通过 `field(default=MemoryType.VARIABLE, init=False)` 强制约束 mem_type，Go 没有此约束，调用者可传入任意 MemoryType。

### G-06：OperationType 零值与 Python None 语义不同

**章节**：7.9
Go 的 OperationType 零值是 OperationTypeAdd(0)，Python 默认 None。Go 中无法区分"未设置操作类型"和"操作类型为 ADD"，可能导致逻辑错误。建议使用指针 `*OperationType` 或增加 `OperationTypeUnknown` 哨兵值。

### G-07：CreateTables 缺少旧表迁移检测和 schema 版本初始化

**章节**：7.9
Python 的 `create_tables()` 检测旧表 group_id 列、记录新建表、初始化 schema 版本号。Go 的 `CreateTables()` 仅调 GORM AutoMigrate。

### G-08：StructuredAskUserRail questions 序列化路径差异

**章节**：10.6.3
Python 的 `ToolCallInterruptRequest` 使用 `model_config = {"extra": "allow"}`，questions 展平到顶层。Go 的 questions 嵌套在 `request` 字段内。前端 `extractQuestionsFromValue` 需要增加从 `value.request.questions` 路径提取的逻辑。

### G-09：StructuredAskUserTool required 字段差异

**章节**：10.6.3
Python `required: ["query"]`（query 必填），Go `required: ["questions"]`（questions 必填）。options item 中 Python `required: ["label"]`，Go `required: ["label", "description"]`。

### G-10：buildInteractiveInputFromAnswers 缺少 free_text 处理

**章节**：10.6.3
用户选择"Other"输入自定义文本时，前端不发送 `selected_options` 而发 `free_text`。Go 跳过此输入导致 answersDict 为空。

### G-11：SkillToolkit install_skill 中 SkillNet name 回退逻辑不一致

**章节**：9.50
**文件**：`internal/swarm/agents/harness/tools/skill_toolkit.go:378-379`

**Python**：`name = Path(target).name if resolved_source == "skillnet" else target`
**Go**：`name = identifier`（不区分来源）

SkillNet 安装时如果 skill.name 为空，Go 不会从 URL 路径推断名称。

**修复方案**：
```go
if resolvedSource == "skillnet" {
    name = filepath.Base(identifier)
} else {
    name = identifier
}
```

### G-12：SkillToolkit _list_installed_skills 缺少异常兜底

**章节**：9.50
**文件**：`internal/swarm/agents/harness/tools/skill_toolkit.go:726`

Go 忽略了 `HandleSkillsInstalled` 返回的 error（`payload, _ := tk.manager.HandleSkillsInstalled(...)`），Python 返回明确的失败信息。建议添加 error 处理和回退。

### G-13：SkillToolkit uninstall_skill 缺少回退消息

**章节**：9.50
**文件**：`skill_toolkit.go:438, 463`

- `list_installed_skills` 失败时缺少回退消息 "failed to inspect installed skills"
- `HandleSkillsUninstall` 失败时缺少回退消息 "skill uninstall failed"

### G-14：TeamMemoryManager.init_toolkit SharedMemoryManager 创建时机差异

**章节**：9.64
Python 在 init_toolkit 成功后创建 SharedMemoryManager，Go 在构造函数中创建。init_toolkit 失败时 Python 不会创建，Go 会。

### G-15：GetMemoryIndexManager 使用 context.Background() 丢失超时控制

**章节**：9.64
**文件**：`internal/agentcore/memory/lite/manager_impl.go:600`

Go 使用 `context.Background()` 而非传入的 ctx，不支持超时和取消。

### G-16：MemoryIndexManager 接口缺少 IsClosed() 方法

**章节**：9.64
Go 的 `MemberMemoryToolkit.Initialize` 通过类型断言访问 `IsClosed()`，但接口定义中没有此方法。应在接口中添加 `IsClosed() bool`。

### G-17：WriteTeamSummary 缺少 prepend_newline=False 参数

**章节**：9.64
Python 的 sysOperation write 调用 `write_file(..., prepend_newline=False)`，Go 只有 `WithFsCreateIfNotExist(true)`。需确认 Go sysOperation 默认行为。

### G-18：FragmentMemoryManager._add_memory_to_store 方法缺失

**章节**：7.6
Python 中有独立的带参数校验的原子写入方法 `_add_memory_to_store`，Go 缺失。当前 AddMemories 内联了写入逻辑，但如果其他路径需要单独调用原子写入，则缺少入口。

### G-19：data_id_manager hash 算法差异

**章节**：7.9
Python 用 `hash()`（非确定性，跨进程 salt 不同），Go 用 `fnv.New32a()`（确定性）。功能上不影响（同进程内唯一），但需注释说明不可跨语言兼容。

---

## 四、提示问题

### T-01：StructuredAskUserTool 描述文本换行丢失

**章节**：10.6.3
Python 的 `_EXTENDED_DESCRIPTION` 中模式 1 和模式 2 之间有 `\n` 换行，Go 用字符串拼接无换行。影响 LLM 可读性。

### T-02：StructuredAskUserTool options 缺少 preview 字段

**章节**：10.6.3
Python 的 options item schema 有 `preview` 字段（可选预览内容），Go 缺失。前端无法展示视觉比较内容。

### T-03：StructuredAskUserTool AskUserPayload answer 字段兼容

**章节**：10.6.3
Python `StructuredAskUserRail` 处理 `AskUserPayload` 时检查 `answer` 字段做兼容，Go 的 `AskUserPayload` 没有 `answer` 字段。当前不影响功能。

### T-04：StructuredAskUserRail Init 重复注册风险

**章节**：10.6.3
如果 `RegisterRail` 机制触发了嵌入的 `AskUserRail.Init()`，可能注册两套 `ask_user` 工具。需确认框架不自动调用父类 Init。

### T-05：ParseToolArgs 与 AgentModeRail.parseToolArgs 代码重复

**章节**：interrupt.ParseToolArgs
`interrupt.ParseToolArgs(*ToolCall)` 和 `AgentModeRail.parseToolArgs(string)` 逻辑相同但签名不同，无法直接替换。建议 `interrupt` 包提供 `ParseToolArgsJSON(string)` 版本。

### T-06：SkillToolkit search_skill auto 来源搜索顺序硬编码

**章节**：9.50
Python 用 `sorted(_SUPPORTED_SOURCES)` 动态排序，Go 硬编码 `["clawhub", "skillnet", "teamskillshub"]`。当前顺序一致，但添加新来源时行为可能不同。

### T-07：SkillToolkit install_skill 中 teamskillshub 多传了 market_url

**章节**：9.50
Go 端额外传递 `market_url` 参数，Python 端没有此参数。如果 SkillManager 不处理该参数则无实际作用。

### T-08：SkillToolkit Deep 模式注册缺少错误处理日志

**章节**：9.50
`deep_adapter_tools.go:500` 中 `AddTool` 返回的 error 被忽略，没有警告日志。Code 模式的注册反而有 warn 日志。

### T-09：encryptMemoryIfNeeded / decryptMemoryIfNeeded 变为包级函数

**章节**：7.6
Python 中是 `BaseMemoryManager` 的 `@staticmethod`，Go 中改为包级函数。功能不受影响，但若后续 SummaryManager 需要加密，调用方式不同。

### T-10：FragmentMemoryTypes 应为只读

**章节**：7.6
Python 是模块级常量列表，Go 声明为 `var`（可被意外修改）。建议改为函数返回或确保只读。

### T-11：sql_message_store timestamp 存储格式差异

**章节**：7.9
Go 使用 RFC3339（`T` 分隔），Python 由 SQLAlchemy+驱动决定。读取 Python 写入的旧数据时可能格式不兼容。

### T-12：sql_message_store update_message 加密输入差异

**章节**：7.9
Go 先 JSON marshal 再 codec.Encode，Python 直接 codec.Encode 原始对象。两语言加密输入不同，解密结果不兼容。

### T-13：allocator.groupIndexOf 值比较 vs Python 引用比较

**章节**：9.64
Python 用 `is`（引用身份），Go 用 `entrySignature` + `ModelID` 值比较。同一 model_name 下有完全相同条目时可能返回错误索引，但 UUID 正确时等价。

### T-14：allocator 未知策略时 Go 静默回退 vs Python 报错

**章节**：9.64
Python `raise ValueError(...)`，Go 记录日志并返回 nil。Go 更宽容但可能掩盖配置错误。

### T-15：MemberMemoryToolkit scenario 未做标准化

**章节**：9.64
Python 做 `.strip().lower()`，Go 不做。非标准化输入（如 "CODING"）Go 不识别。

### T-16：CodingMemoryToolContext 缺少 NodeName 字段

**章节**：9.64
Python 传 `node_name="coding_memory"`，Go 没有 `WithNodeName` 选项。

### T-17：semantic_store.py 和 user_mem_store.py 未移植

**章节**：7.9
Python 的 mem_model 目录下有 `semantic_store.py`（语义存储）和 `user_mem_store.py`（用户记忆 KV 存储），Go 完全缺失。需确认是否计划移植。

### T-18：RouterAllocator.LoadStateDict 过于简化

**章节**：9.64
Python 有清晰的 digest 匹配检查和注释，Go 仅 `_ = state["pool_digest"]` 丢弃值。

### T-19：doc.go 未反映 ParseToolArgs 新导出

**章节**：interrupt.ParseToolArgs
`internal/agentcore/harness/rails/interrupt/doc.go` 未列出 `ParseToolArgs` 为包级导出函数。

### T-20：FragmentMemoryUnit 字段默认值/零值语义差异

**章节**：7.9
Python 的 `message_mem_id` 默认 None（可区分"未设置"和"空字符串"），Go 零值为 `""`（无法区分）。

---

## 五、⤵️ 待回填代码状态确认

| 标记 | 位置 | 状态 | 备注 |
|------|------|------|------|
| ⤵️ 7.8 WriteManager | `manage/write_manager.go` 不存在 | ✅ 确认未实现 | Python 有完整实现 |
| ⤵️ 7.8 SearchManager | `manage/search/` 目录不存在 | ✅ 确认未实现 | Python 有完整实现 |
| ⤵️ 7.8 MemUpdateChecker LLM | `manage/update/update_checker.go:79` | ✅ 确认 stub | 签名需预留 model 参数 |
| ⤵️ 7.7 SummaryManager | 不存在 | ✅ 确认未实现 | Python 有完整实现 |
| ⤵️ 7.7 VariableManager | 不存在 | ✅ 确认未实现 | Python 有完整实现 |
| ⤵️ 7.8 _process_conflict_info | fragment_manager.go 缺失 | ✅ 确认未实现 | 回填时必须补上 |
| ⤵️ 7.2 register_tools | team_memory/manager.go:125 | ✅ 确认空实现 | |
| ⤵️ 7.2 load_and_inject | team_memory/manager.go | ✅ 确认空实现 | |
| ⤵️ 7.2 extract_team_memories | team_memory/extractor.go:83 | ✅ 确认空实现 | |
| ⤵️ 7.2+9.65a BuildExtractionContext | team_memory/extractor.go:75 | ✅ 确认空实现 | |

---

## 六、修复优先级建议

### P0 — 必须立即修复（影响核心功能正确性）

| 编号 | 问题 | 修复难度 |
|------|------|---------|
| S-01 | CancelMember 错误修改成员状态 | 中 |
| S-02 | ShutdownMember 幂等语义 + 缺失消息 | 中 |
| S-03 | Startup 缺失 on_created 回调 | 高 |
| S-04 | CleanTeam 不排除自身 | 低 |
| S-08 | AddMemories 缺少 llm 参数 | 中 |
| S-15 | StructuredAskUserTool 缺少 query 参数 | 低 |

### P1 — 回填前必须修复（否则回填时连锁修改）

| 编号 | 问题 | 修复难度 |
|------|------|---------|
| S-09 | AddMemories 签名硬编码 FragmentMemoryUnit | 中 |
| S-10 | MemUpdateChecker.Check 缺少 model 参数 | 低 |
| S-16 | _process_conflict_info 缺失 | 低 |
| G-06 | OperationType 零值语义 | 低 |

### P2 — 后续版本修复

| 编号 | 问题 |
|------|------|
| S-05~S-07, S-11~S-14 | TeamBackend 其他问题、Search排序、数据模型差异 |
| G-01~G-19 | 消息通知缺失、i18n、序列化差异等 |
| T-01~T-20 | 日志、文档、代码风格等 |
