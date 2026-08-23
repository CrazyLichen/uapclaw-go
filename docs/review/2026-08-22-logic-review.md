# Go 移植逻辑审查报告

> 审查日期：2026-08-22
> 审查范围：8月15-17日提交记录中完成的功能章节
> 审查方法：逐章对照 Python 参考项目，检查方法签名、逻辑步骤、回填标记准确性
> Python 参考项目根目录：`/home/opensource/agent-core/openjiuwen/` 和 `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`

---

## 审查范围

最近一次提交日期为 2026-08-17，48小时内无新提交。本次审查覆盖 8月15-17日开发活动中完成的以下章节：

| 章节 | 功能 | 状态 | Python 参考 |
|------|------|------|-------------|
| 7.6 | FragmentMemoryManager | ✅ | `openjiuwen/core/memory/manage/` |
| 7.9 | 记忆数据模型 | ✅ | `openjiuwen/core/memory/manage/mem_model/` |
| 9.65a-4 | TeamBackend 门面 | ✅ | `openjiuwen/agent_teams/tools/team.py` |
| 10.6.3 | StructuredAskUserRail | ✅ | `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py` |
| 10.3.19-20 | 技能管理(SkillManager/SkillToolkit) | ✅ | `jiuwenswarm/server/runtime/skill/` |
| 10.6.1-2 | Prompt Builder | ✅ | `jiuwenswarm/agents/harness/common/prompt/` · `code/prompt/` |

---

## 问题汇总统计

| 严重程度 | 数量 | 说明 |
|---------|------|------|
| 🔴 严重 | 25 | 功能缺失/逻辑错误，影响核心流程 |
| 🟡 一般 | 18 | 逻辑偏差，不影响主流程但行为与 Python 不一致 |
| 🔵 提示 | 10 | 日志缺失、风格差异等 |

---

## 一、7.6 FragmentMemoryManager

### 🔴 S7.1：AddMemories / MemUpdateChecker.Check 缺少 llm 参数

**Python 样例：**
```python
# openjiuwen/core/memory/manage/fragment_memory_manager.py
async def add_memories(self, user_id, scope_id, memories, llm=None, **kwargs):
    # ...
    action_items = await self.checker.check(
        new_memories, old_memories, base_chat_model=llm
    )
```

**Go 问题：**
```go
// internal/agentcore/memory/manage/fragment_manager.go
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID, scopeID string, memories map[string][]*mem_model.FragmentMemoryUnit) error {
    // 缺少 llm 参数
    actionItems, err := m.checker.Check(newMemContent, oldMemories)
    // Check 也没有 llm 和 retries 参数
}
```

**影响：** 7.8 回填时 LLM 驱动的冲突检查需要此参数，当前接口无法接收 LLM 客户端。

**修复方案：**
```go
// 1. 修改 AddMemories 签名
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID, scopeID string, memories map[string][]*mem_model.FragmentMemoryUnit, llm ...*llm.Model) error

// 2. 修改 MemUpdateChecker.Check 签名
func (c *MemUpdateChecker) Check(newMemories, oldMemories []*mem_model.FragmentMemoryUnit, opts ...CheckOption) ([]*UpdateActionItem, error)
// 其中 CheckOption 可包含 WithModel(llm) 和 WithRetries(n)

// 3. 添加 ⤵️ 回填标记指向 7.8
```

---

### 🔴 S7.2：BaseMemoryManager 接口 AddMemories 使用具体类型导致不可通用

**Python 样例：**
```python
# Python 接口接受基类型
async def add_memories(self, user_id, scope_id, memories: dict[str, list[BaseMemoryUnit]], ...)
```

**Go 问题：**
```go
// 使用具体类型 FragmentMemoryUnit，SummaryManager/VariableManager 无法实现同一接口
type BaseMemoryManager interface {
    AddMemories(ctx context.Context, userID, scopeID string, memories map[string][]*mem_model.FragmentMemoryUnit) error
}
```

**修复方案：**
```go
// 改用基类型 BaseMemoryUnit（需确认 mem_model 包已定义此基类型）
type BaseMemoryManager interface {
    AddMemories(ctx context.Context, userID, scopeID string, memories map[string][]*mem_model.BaseMemoryUnit) error
}
```

---

### 🟡 M7.1：Search 缺少 manager 层 score 降序排序和 top_k 截断

**Python 样例：**
```python
result.sort(key=lambda x: x["score"], reverse=True)
return result[:top_k]
```

**Go 问题：** 直接返回 `m.memoryIndex.Search()` 结果，无防御性排序和截断。

**修复方案：**
```go
func (m *FragmentMemoryManager) Search(ctx context.Context, ...) ([]*index.MemorySearchResult, error) {
    results, err := m.memoryIndex.Search(...)
    // 添加防御性排序
    sort.Slice(results, func(i, j int) bool {
        return results[i].Score > results[j].Score
    })
    // 截断
    if topK > 0 && len(results) > topK {
        results = results[:topK]
    }
    return results, nil
}
```

---

### 🟡 M7.2：ListFragmentMemories 缺少排序和非法 mem_type 校验

**Python 样例：**
```python
# 非法类型校验
if mem_type.value not in FRAGMENT_MEMORY_TYPE:
    logger.error(...)
    return []
# 排序
result.sort(key=lambda x: (x['mem'], str(x.get('timestamp') or '')), reverse=True)
```

**Go 问题：** 无校验（任何 memType 字符串直接透传）、无排序。

**修复方案：** 添加 `isFragmentMemoryType` 校验 + 降序排序。

---

### 🟡 M7.3：缺失 _process_conflict_info 方法（未标记 ⤵️）

**Python 样例：**
```python
@staticmethod
def _process_conflict_info(conflict_info, input_memory_ids_map):
    """将 LLM 返回的数字 ID 映射回实际记忆 ID"""
    # conf_id==0 映射为 "-1"
```

**Go 问题：** 完全缺失，且无 ⤵️ 回填标记。

**修复方案：** 在 `fragment_manager.go` 中添加 `processConflictInfo` 方法 + ⤵️ 回填标记指向 7.8。

---

### 🔵 T7.1：缺失 _add_memory_to_store 独立方法（功能已内联）

**说明：** Go 的内联实现功能等价，但缺少 Python 中的独立原子写入入口。不阻塞流程。

---

## 二、9.65a-4 TeamBackend

### 🔴 S9.1：ShutdownMember 幂等逻辑与 Python 相反

**Python 样例：**
```python
def shutdown_member(self, member_name, force=False):
    member_data = self.db.member.get_member(member_name, self.team_name)
    # SHUTDOWN/SHUTDOWN_REQUESTED -> 返回 success（幂等）
    if member_data.status in (MemberStatus.SHUTDOWN, MemberStatus.SHUTDOWN_REQUESTED):
        return MemberOpResult(success=True, member_name=member_name)
```

**Go 问题：**
```go
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) MemberOpResult {
    // 终态 -> 返回 fail
    if member.Status == MemberStatusShutdown || member.Status == MemberStatusError {
        return MemberOpResult{Success: false, ...}
    }
}
```

**影响：** Python 认为重复关闭是正常操作（幂等），Go 视为错误。下游代码若依赖幂等性（如 force_clean_team 中多次调用），行为将不一致。

**修复方案：** 改为 Python 语义——终态返回 `Success: true`：
```go
if member.Status == MemberStatusShutdown || member.Status == MemberStatusShutdownRequested {
    return MemberOpResult{Success: true, MemberName: memberName}
}
```

---

### 🔴 S9.2：ShutdownMember 缺少 force 参数

**Python 样例：**
```python
def shutdown_member(self, member_name, force=False):
    # force 影响 MemberShutdownEvent 中的 Force 字段
    self.event_bus.publish(MemberShutdownEvent(force=force))
```

**Go 问题：** `ShutdownMember(ctx, memberName)` 无 force 参数，硬编码 `Force: false`。

**影响：** `ForceCleanTeam` 依赖 `shutdown_member(force=True)`，Go 无法传递此语义。

**修复方案：** 添加 force 参数：
```go
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string, force ...bool) MemberOpResult {
    isForce := false
    if len(force) > 0 {
        isForce = force[0]
    }
    // ...
    tb.publishEvent(MemberShutdownEvent{Force: isForce})
}
```

---

### 🔴 S9.3：ShutdownMember 缺少发送 shutdown 消息步骤

**Python 样例：**
```python
# 步骤5: 向成员发送关闭请求消息
self.message_manager.send_message(
    content=t("team.shutdown_request_content"),
    to_member_name=member_name,
)
```

**Go 问题：** 完全缺少 `messageManager.SendMessage` 调用，成员进程可能收不到关闭通知。

**修复方案：** 在 CAS 状态转换后、事件发布前添加消息发送：
```go
tb.messageManager.SendMessage(ctx, &schema.TypedEvent{
    Content:      "shutdown_request",
    ToMemberName: memberName,
})
```

---

### 🔴 S9.4：ShutdownMember 不应调用 CancelAllTasks

**Python 样例：** Python 的 `shutdown_member` **不取消任何任务**，只改状态+发消息+发事件。

**Go 问题：**
```go
// Go 步骤4调用了 CancelAllTasks（Python 中不存在的额外行为）
tb.taskManager.CancelAllTasks(ctx, []string{memberName})
```

**影响：** shutdown 和 cancel 是两个不同语义。shutdown 是"请求关闭"，cancel 是"取消当前执行"。Go 混淆了两者。

**修复方案：** 移除 `CancelAllTasks` 调用。

---

### 🔴 S9.5：CancelMember 语义完全错误——不应改成员状态

**Python 样例：**
```python
def cancel_member(self, member_name):
    member_data = self.db.member.get_member(member_name, self.team_name)
    if member_data.status != MemberStatus.BUSY:
        return True  # 非 BUSY 无需取消
    # 不改成员状态！只重置 CLAIMED 任务 + 发消息 + 发事件
    self.task_manager.reset_tasks_by_assignee(...)
    self.message_manager.send_message(content=t("team.cancel_request_content"), ...)
    self.event_bus.publish(MemberCanceledEvent(...))
```

**Go 问题：**
```go
func (tb *TeamBackend) CancelMember(ctx context.Context, memberName string) MemberOpResult {
    // 错误：先 CAS 改状态为 SHUTDOWN_REQUESTED
    result := tb.tryTransitionMemberStatus(memberName, MemberStatusShutdownRequested)
    // 非 BUSY 也走终态检查 -> 返回 fail
}
```

**影响：** cancel 的语义是"取消当前执行"而非"关闭成员"。Go 把 cancel 和 shutdown 混为一谈。

**修复方案：** 重写 CancelMember 对齐 Python 逻辑：
```go
func (tb *TeamBackend) CancelMember(ctx context.Context, memberName string) MemberOpResult {
    member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
    if err != nil { return MemberOpResult{Success: false} }
    if member.Status != MemberStatusBusy {
        return MemberOpResult{Success: true, MemberName: memberName}
    }
    // 重置 CLAIMED 任务
    tb.taskManager.ResetTasksByAssignee(ctx, memberName)
    // 发送 cancel 消息
    tb.messageManager.SendMessage(ctx, &schema.TypedEvent{Content: "cancel_request", ToMemberName: memberName})
    // 发布事件
    tb.publishEvent(MemberCanceledEvent{MemberName: memberName})
    return MemberOpResult{Success: true, MemberName: memberName}
}
```

---

### 🔴 S9.6：Startup/StartupMember 缺少 on_created 回调 + 事件发布 + 回滚

**Python 样例：**
```python
async def startup_member(self, member_name, on_created):
    # CAS: UNSTARTED -> STARTING
    success = self.db.member.try_transition_member_status(member_name, MemberStatus.STARTING)
    if not success:
        return False
    try:
        await self._spawn_and_publish(member_name, on_created)  # on_created + MemberSpawnedEvent
    except Exception:
        # rollback: STARTING -> UNSTARTED
        self.db.member.update_member_status(member_name, MemberStatus.UNSTARTED)
        raise
    return True
```

**Go 问题：** `StartupMember` 只做 CAS 状态转换，没有回调调用、事件发布、回滚逻辑。

**修复方案：** 添加回调参数 + MemberSpawnedEvent 发布 + 异常回滚。

---

### 🔴 S9.7：ApprovePlan 参数完全缺失

**Python 样例：**
```python
def approve_plan(self, plan_id, approved=True, feedback=None):
    plan_record = self.task_manager.get_plan_record(plan_id)
    member_name = plan_record["member_name"]
    task_id = plan_record["task_id"]
    self.db.member.get_member(member_name, self.team_name)  # 验证成员
    self.task_manager.approve_plan(plan_id, approved, feedback, leader_name)
```

**Go 问题：**
```go
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) MemberOpResult {
    // 传入 taskID 而非 planID
    // 缺少 approved/feedback 参数
    // 不验证 plan_record 和 member_name
}
```

**影响：** 无法区分通过/拒绝计划，无法附带反馈意见。

**修复方案：**
```go
func (tb *TeamBackend) ApprovePlan(ctx context.Context, planID string, approved bool, feedback ...string) MemberOpResult {
    planRecord := tb.taskManager.GetPlanRecord(ctx, planID)
    // 验证成员
    _, err := tb.db.Member().GetMember(ctx, planRecord.MemberName, tb.teamName)
    if err != nil { return MemberOpResult{Success: false} }
    return tb.taskManager.ApprovePlan(ctx, planID, approved, feedback, tb.memberName)
}
```

---

### 🔴 S9.8：CleanTeam 活跃成员检查不跳过自身

**Python 样例：**
```python
for member_data in all_members:
    if member_data.member_name == self.member_name:
        continue  # 跳过自身
    if member_data.status != MemberStatus.SHUTDOWN:
        raise RuntimeError(...)
```

**Go 问题：** `CleanTeam` 不跳过自身，要求所有成员都是终态。

**修复方案：** 遍历成员时跳过 `tb.memberName`。

---

### 🔴 S9.9：ForceCleanTeam 不应触发 on_team_cleaned 回调

**Python 样例：** `force_clean_team` **不触发** `on_team_cleaned` 回调。

**Go 问题：** 步骤3调用了 `tb.onTeamCleaned(ctx)`。

**修复方案：** 移除 `ForceCleanTeam` 中的 `onTeamCleaned` 调用。

---

### 🔴 S9.10：ForceCleanTeam shutdown 逻辑简化过度 + 不跳过自身

**Python 样例：**
```python
if shutdown_members:
    for member_data in all_members:
        if member_data.member_name == self.member_name:
            continue  # 跳过自身
        self.shutdown_member(member_data.member_name, force=True)
```

**Go 问题：** 直接 `UpdateMemberStatus` 到 SHUTDOWN（包括自身），跳过了完整 shutdown 流程。

**修复方案：** 改为调用 `ShutdownMember(force=True)` 并跳过自身。

---

### 🔴 S9.11：CancelAllTasks 缺少广播消息

**Python 样例：**
```python
self.message_manager.broadcast_message(
    content=f"All tasks ({len(cancelled_tasks)}) have been cancelled by team leader."
)
```

**Go 问题：** 无广播消息。

**修复方案：** 在 `CancelAllTasks` 后添加广播。

---

### 🔴 S9.12：SpawnMember 缺少 status/execution_status/mode/allocation 参数

**Python 样例：**
```python
def spawn_member(self, member_name, display_name, agent_card, *,
                 desc, prompt, status=UNSTARTED, execution_status=IDLE,
                 mode=BUILD_MODE, allocation=None, role=TEAMMATE):
```

**Go 问题：** 硬编码 `status=Unstarted, execution_status=Idle, mode=teammateMode`，不支持自定义。BuildTeam 中创建 Leader 时需传 `status=BUSY, execution_status=RUNNING`，Go 不得不绕过 SpawnMember 直接调 `db.CreateMember`。

**修复方案：** 添加 Functional Options：
```go
type SpawnMemberOption func(*spawnMemberConfig)
func WithStatus(s MemberStatus) SpawnMemberOption { ... }
func WithExecutionStatus(s ExecutionStatus) SpawnMemberOption { ... }
func WithMode(m TeamMode) SpawnMemberOption { ... }
```

---

### 🟡 M9.1：ApproveTool 缺少成员验证步骤

**Python 样例：** 先 `db.member.get_member` 验证成员存在后才发布事件。

**Go 问题：** 直接发布事件，不做验证。

**修复方案：** 添加成员验证。

---

### 🟡 M9.2：BuildTeam 缺少 create_team 返回值检查

**Python 样例：**
```python
success = self.db.team.create_team(...)
if not success:
    raise RuntimeError("Failed to create team")
```

**Go 问题：** 不检查返回值。

---

### 🟡 M9.3：BuildTeam Leader 注册绕过 SpawnMember

**Python 样例：** Leader 也走 `self.spawn_member(...)` 统一路径。

**Go 问题：** 直接调 `db.Member().CreateMember(...)`，绕过了去重检查、HITT 缓存写透等逻辑。

**修复方案：** 在 SpawnMember 支持自定义 status 后（S9.12），Leader 也走 SpawnMember 路径。

---

### 🟡 M9.4：SpawnHumanAgent 缺少 i18n fallback

**Python 样例：**
```python
display_name = display_name or t("hitt.human_agent_display_name")
desc = desc or t("hitt.human_agent_default_persona")
```

**Go 问题：** 空字符串直接传入，无 i18n 回退。

---

### 🟡 M9.5：HumanAgentNames 返回类型差异

**Python：** 返回 `frozenset[str]`（不可变集合）。**Go：** 返回 `[]string`（排序切片）。

---

### 🔵 T9.1：ShutdownMember 日志字段缺失

**Python：** 日志包含状态转换（`current -> SHUTDOWN_REQUESTED`）和 force 参数。**Go：** 只记录 "成员已请求关闭"。

---

## 三、10.6.3 StructuredAskUserRail

### 🔴 S10.1：StructuredAskUserTool input_params schema 缺少 query 顶层属性

**Python 样例：**
```python
# jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
EXTENDED_INPUT_PARAMS_EN = {
    "type": "object",
    "properties": {
        "query": {"type": "string", "description": "向用户展示的问题（必填）。"},
        "questions": {"type": "array", "description": "带选项的结构化问题...", "items": {...}}
    },
    "required": ["query"]  # query 必填，questions 可选
}
```

**Go 问题：**
```go
// internal/swarm/server/rails/structured_ask_user_tool.go
// 使用 BuildToolCard 获取 AskUserMetadataProvider 的 schema
// 该 schema 只有 questions（required），没有 query 字段
```

**影响：** LLM 无法以"纯文本查询"模式调用 ask_user（传入 query 不传 questions），因为 Go schema 中根本没有 query 字段。Python 的设计允许 LLM 自由选择两种模式，Go 只支持结构化模式。

**修复方案：** 在 `NewStructuredAskUserTool` 中自行构建与 Python `EXTENDED_INPUT_PARAMS_EN/CN` 一致的 schema，包含 `query`（required）和 `questions`（optional）两个顶层属性：

```go
func NewStructuredAskUserTool(language, agentID string) (tool.Tool, error) {
    card := &schema.ToolCard{
        Name:        "ask_user",
        Description: getStructuredDescription(language),
        InputParams: buildExtendedInputParams(language), // 新方法
    }
    // ...
}

func buildExtendedInputParams(language string) []*schema.Param {
    return []*schema.Param{
        {
            Name:        "query",
            Type:        "string",
            Required:    true,
            Description: "向用户展示的问题（必填）。",
        },
        {
            Name:        "questions",
            Type:        "array",
            Required:    false,
            Description: "带选项的结构化问题列表（可选）...",
            // items 定义...
        },
    }
}
```

---

### 🔴 S10.2：tool description 声称支持 query 模式但 schema 不匹配

**Go 问题：** `getStructuredDescription` 的 CN 版本描述"1. 普通提问（自由文本）：仅传入 `query`"，但 schema 中没有 `query` 字段。LLM 可能尝试传入 `query` 参数但 schema 不会允许。

**修复方案：** 与 S10.1 一起修复——在 schema 中添加 query 字段后，description 和 schema 将一致。

---

### 🟡 M10.1：questions item 中 header/options 的 required 约束过严

**Python 样例：**
```python
_QUESTIONS_ITEM_SCHEMA = {
    "required": ["question"]  # 仅 question 必填，header/options/multi_select 可选
}
```

**Go 问题：** `required: ["header", "question", "options"]`，强制 LLM 每次必须传 header 和 options。

**影响：** LLM 不想传 options 时（纯文本模式），Go 的 schema 会拒绝。

**修复方案：** 修改 `AskUserMetadataProvider` 中 questions item 的 required 为 `["question"]`。

---

### 🟡 M10.2：options item 中 description 必填

**Python：** `required: ["label"]`。**Go：** `required: ["label", "description"]`。

**修复方案：** 修改 options item 的 required 为 `["label"]`。

---

### 🔵 T10.1：异常处理方式差异

**Python：** `try/except Exception` 包裹整个 is_structured 分支。**Go：** `defer/recover` 处理 panic，对 error 不生效。

**说明：** Go 代码中的 type switch + map 操作不会产生 panic，实际场景差异不大。

---

## 四、10.3.19-20 技能管理

### 🔴 S10.3.1：SkillNet 搜索/安装/评估均为 stub

**Python 样例：**
```python
# handle_skills_skillnet_search 完整实现：调用 skillnet-ai API
# handle_skills_skillnet_install 完整实现：后台 asyncio task 下载文件
# handle_skills_skillnet_evaluate 完整实现
```

**Go 问题：**
- `HandleSkillsSkillnetSearch` → 返回 `errNotImplemented`
- `HandleSkillsSkillnetInstall` → 创建 job 后无后台 goroutine，**job 永远 pending**
- `HandleSkillsSkillnetEvaluate` → 返回 `errNotImplemented`

**修复方案：**
1. SkillNet 搜索：实现 HTTP 调用 skillnet-ai API
2. SkillNet 安装：在 `HandleSkillsSkillnetInstall` 中启动 goroutine 执行下载：
```go
go func() {
    defer close(job.Done)
    // 下载文件 → 写入状态 → 调用 hook
    job.Status = "completed"
}()
```
3. SkillNet 评估：实现完整逻辑或标注 ⤵️

---

### 🔴 S10.3.2：mirror 目录机制完全缺失

**Python 样例：**
```python
def _get_mirror_skills_dirs(self):
    return [
        os.path.join(self.workspace_dir, "skills"),
        os.path.join(self.agent_data_dir, "skills"),
    ]
# 安装时写入 mirror，卸载时清理 mirror
```

**Go 问题：** 没有 mirror 目录概念。ClawHub/TeamSkillsHub 安装时不写 mirror，卸载时不清理 mirror。

**影响：** Agent 可能在 mirror 目录找不到已安装的技能。

**修复方案：** 实现 `getMirrorSkillsDirs()` 并在安装/卸载时同步 mirror。

---

### 🔴 S10.3.3：uninstall 内置技能保护缺失

**Python 样例：**
```python
def handle_skills_uninstall(self, ...):
    # 遍历 builtin 目录通过 SKILL.md 解析匹配技能名
    for builtin_skill_dir in self._builtin_skills_dirs:
        if skill_name.lower() == builtin_skill_name.lower():
            raise ValueError("Cannot uninstall built-in skill")
```

**Go 问题：** `HandleSkillsUninstall` 仅简单删除目录，没有检查 builtin 技能目录。

**修复方案：** 在卸载前遍历 builtin 目录，通过 SKILL.md 解析匹配技能名，检测到则拒绝删除。

---

### 🔴 S10.3.4：import_local 远程 URL 下载缺失

**Python 样例：**
```python
def handle_skills_import_local(self, ...):
    if url.startswith(("http://", "https://")):
        # 下载 + SHA256 校验 + ZIP/tar.gz 解压 + 导入
        self._import_skill_from_remote_archive(url, sha256, ...)
```

**Go 问题：** `HandleSkillsImportLocal` 仅处理本地目录路径，不支持远程 URL 下载。

**修复方案：** 添加远程 URL 分支，实现 HTTP 下载 + SHA256 校验 + ZIP 解压。

---

### 🔴 S10.3.5：TeamSkillsHub Publish 缺少 plugin.yaml 规范化

**Python 样例：**
```python
def _prepare_teamskills_publish_zip(self, skill_dir):
    # 生成 plugin.yaml（name/version/display_name/description/runtime/metadata）
    # 从 SKILL.md 解析 meta
    # 将文件放入 {skill_name}/{skill_name}/ 子目录结构
```

**Go 问题：** 直接上传原始 ZIP，没有 plugin.yaml 规范化。

**修复方案：** 在发布前生成 plugin.yaml + 规范化目录结构。

---

### 🔴 S10.3.6：gitClone/gitPull/gitGetCommit 均为 stub

**Python 样例：** 真实调用 git 命令。

**Go 问题：** 3 个方法均返回 `errNotImplemented` 或空操作。marketplace_toggle 无法正常工作。

**修复方案：** 使用 `os/exec` 调用 git 命令，或标注 ⤵️ 回填。

---

### 🟡 M10.3.1：marketplace_add 默认 enabled 状态不一致

**Python：** 新增源默认 `enabled=False`。**Go：** 默认 `enabled=True`。

**修复方案：** 改为 `enabled: false`。

---

### 🟡 M10.3.2：marketplace_remove/toggle 缓存清理缺失

**Python：** 删除源时清理本地 git clone 缓存。**Go：** 仅修改 state。

**修复方案：** 在 marketplace_remove/toggle 中添加本地缓存清理逻辑。

---

### 🟡 M10.3.3：TeamSkillsHub Validate 团队技能校验简化

**Python：** 校验 `roles` 数组至少 2 个有效 id。**Go：** 仅校验 name/description 非空。

---

### 🟡 M10.3.4：YAML frontmatter 解析简化

**Python：** 使用 `yaml.safe_load` 支持列表、嵌套、日期。**Go：** 仅逐行解析 `key: value`。

**修复方案：** 使用 `gopkg.in/yaml.v3` 进行完整 YAML 解析。

---

### 🟡 M10.3.5：matchHost 缺少后缀匹配

**Python：** `_assert_team_skills_hub_download_url_allowed` 支持 `.example.com` 后缀匹配。**Go：** 缺少此分支。

---

### 🟡 M10.3.6：代理环境变量上下文管理缺失

**Python：** `_skillnet_network_context` 在调用期间临时设置 HTTP_PROXY 等。**Go：** 无此机制。

---

### 🟡 M10.3.7：ClawHub Download 缺少 version 字段

**Python：** `_add_installed_plugin` 包含 `version` 字段。**Go：** `AddInstalledPlugin` 缺少。

---

### 🔵 T10.3.1：_safe_rmtree 无 Windows 重试

**Python：** 3 次重试 + Windows 只读属性修改。**Go：** 直接 `os.RemoveAll`。

---

### 🔵 T10.3.2：refreshAgentDataIndexes 为空操作

**Go：** 仅打印日志，不实际重建 agent-data.json。

---

## 五、10.6.1-2 Prompt Builder

### 🟡 M10.6.1：identity 节品牌名替换不统一

**Python：** 统一使用 "JiuwenSwarm"。

**Go 问题：** code prompt 使用 "JiuwenSwarm"，identity 节使用 "UapClaw"。同一个 prompt 中品牌名不统一。

**修复方案：** 统一品牌名策略——要么全部替换为 "UapClaw"，要么全部保持 "JiuwenSwarm"。当前混合使用会导致 LLM 认知混乱。

---

### 🟡 M10.6.2：BuildAgentIdentityPrompt 使用 PromptModeNone

**Python：** 使用默认 mode（无 mode 参数）。**Go：** 使用 `PromptModeNone`。

**影响：** 当前结果等价（都只注册了 identity），但未来扩展行为不同——Python 会渲染所有注册 section，Go 仅渲染 identity。

**修复方案：** 如未来不需要多 mode，当前可保持；如需对齐 Python 扩展行为，应改为默认 mode。

---

### 🔵 T10.6.1：readWorkspaceFile 缺少 FileNotFoundError 的 Debug 日志

**Python：** `logger.debug(f"File not found: {file_path}")`。**Go：** 静默跳过。

**修复方案：** 添加 Debug 日志：
```go
if os.IsNotExist(err) {
    logger.Debug(logComponent).Str("file_path", filePath).Msg("文件不存在")
    return "", nil
}
```

---

### 🔵 T10.6.2：readWorkspaceFile 缺少 .strip()

**Python：** 对文件内容做 `.strip()`。**Go：** 无 strip，保留首尾空白。

**修复方案：** 添加 `strings.TrimSpace(content)`。

---

### 🔵 T10.6.3：Code section 添加顺序与 Python 列表顺序不同

**说明：** session_guidance 在 Python 列表中排第 3 位，在 Go 中排第 8 位。运行时按 Priority 排序输出一致，仅代码阅读顺序不同。无功能影响。

---

## 六、⤵️ 回填标记准确性汇总

| 位置 | 当前状态 | 应有状态 | 说明 |
|------|---------|---------|------|
| 7.6 `MemUpdateChecker.Check` | ✅ 标记 ⤵️ 7.8 | ✅ 准确 | stub 实现，确实需要回填 |
| 7.6 `AddMemories` llm 参数 | ❌ 无标记 | ⚠️ 应标记 ⤵️ | 接口签名不完整，7.8 回填时需补 |
| 7.6 `_process_conflict_info` | ❌ 无方法无标记 | ⚠️ 应标记 ⤵️ | 方法完全缺失 |
| 9.65a-4 `ShutdownMember` force 参数 | ❌ 无标记 | ⚠️ 应标记 ⤵️ | 缺少 force 参数 |
| 9.65a-4 `CancelMember` 语义错误 | ❌ 无标记 | ⚠️ 需修复 | 不是回填问题而是逻辑 bug |
| 9.65a-4 `ApprovePlan` 参数缺失 | ❌ 无标记 | ⚠️ 需修复 | 接口签名不完整 |
| 10.3.19-20 SkillNet 搜索 | ❌ 无标记（标记为 ✅） | ⚠️ 应标记 ⤵️ | 返回 errNotImplemented |
| 10.3.19-20 SkillNet 安装 | ❌ 无标记（标记为 ✅） | ⚠️ 应标记 ⤵️ | job 永远 pending |
| 10.3.19-20 SkillNet 评估 | ❌ 无标记（标记为 ✅） | ⚠️ 应标记 ⤵️ | 返回 errNotImplemented |
| 10.3.19-20 git 操作 | ❌ 无标记（标记为 ✅） | ⚠️ 应标记 ⤵️ | 返回 errNotImplemented |
| 10.3.19-20 mirror 目录 | ❌ 无标记 | ⚠️ 应标记 ⤵️ | 完全缺失 |

---

## 七、优先修复建议

### P0 — 核心功能错误（立即修复）

| # | 章节 | 问题 | 修复复杂度 |
|---|------|------|-----------|
| 1 | 9.65a-4 | `CancelMember` 语义完全错误——不应改成员状态 | 中 |
| 2 | 9.65a-4 | `ShutdownMember` 幂等逻辑相反 + 缺 force 参数 + 缺发送消息 | 中 |
| 3 | 9.65a-4 | `ApprovePlan` 参数完全缺失 | 低 |
| 4 | 9.65a-4 | `Startup/StartupMember` 缺少回调+事件+回滚 | 中 |
| 5 | 9.65a-4 | `ForceCleanTeam` 不应触发 on_team_cleaned + 不跳过自身 | 低 |
| 6 | 10.6.3 | StructuredAskUserTool schema 缺少 query 字段 | 中 |

### P1 — 功能缺失（尽快修复）

| # | 章节 | 问题 | 修复复杂度 |
|---|------|------|-----------|
| 7 | 10.3.19-20 | SkillNet 安装缺少后台 goroutine（job 永远 pending） | 中 |
| 10.3.19-20 | mirror 目录机制完全缺失 | 高 |
| 9 | 10.3.19-20 | uninstall 内置技能保护缺失 | 低 |
| 10 | 10.3.19-20 | import_local 远程 URL 下载缺失 | 中 |
| 11 | 10.3.19-20 | TeamSkillsHub Publish 缺少 plugin.yaml | 中 |
| 12 | 7.6 | BaseMemoryManager 接口使用具体类型导致不可通用 | 低 |

### P2 — 逻辑偏差（计划修复）

| # | 章节 | 问题 | 修复复杂度 |
|---|------|------|-----------|
| 13 | 10.3.19-20 | SkillNet 搜索/评估为 stub | 中 |
| 14 | 10.3.19-20 | git 操作为 stub | 中 |
| 15 | 10.3.19-20 | marketplace 默认 enabled 状态不一致 | 低 |
| 16 | 10.3.19-20 | marketplace_remove/toggle 缓存清理缺失 | 中 |
| 17 | 10.6.3 | questions item required 约束过严 | 低 |
| 18 | 7.6 | Search/ListFragmentMemories 缺少排序和校验 | 低 |
| 19 | 10.6.1-2 | identity 节品牌名替换不统一 | 低 |
