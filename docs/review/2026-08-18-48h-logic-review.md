# 48小时逻辑审查报告

> **审查日期**: 2026-08-17 ~ 2026-08-18
> **审查范围**: 48小时内提交记录覆盖的实现计划章节
> **审查方法**: 逐方法对照 Python 参考项目，检查方法签名、步骤完整性、⤵️标记验证

---

## 一、审查范围

48小时内共 31 个提交，涉及以下章节的实现/修复：

| 章节 | 内容 | 状态 |
|------|------|------|
| 7.6 | FragmentMemoryManager | ✅ 新实现 |
| 7.8 | MemUpdateChecker stub | ⤵️ 回填标记 |
| 7.9 | 记忆数据模型 | ✅ 新实现 |
| 9.50 | SkillToolkit 回填（Code模式注册+SkillNet安装轮询） | ✅ 回填 |
| 9.65a-4 | TeamBackend 门面 | ✅ 修复 |
| 10.3.7-11 | 适配器辅助修复 | ✅ 修复 |
| 10.6.3 | StructuredAskUserRail | ✅ 新实现 |

---

## 二、问题汇总

| 严重程度 | 数量 |
|---------|------|
| 严重 | 14 |
| 一般 | 19 |
| 提示 | 8 |

---

## 三、严重问题

### S-01: FragmentMemoryManager.AddMemories 缺少 llm 参数

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:67-68`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:125-126`

**问题描述**: Python 的 `add_memories` 签名包含 `llm: Model | None = None` 参数，传递给 `MemUpdateChecker.check(base_chat_model=llm)`。Go 的 `AddMemories` 方法签名中没有 `llm` 参数，导致 MemUpdateChecker 永远无法获取 LLM 实例进行冲突检查。7.8 回填时必须添加此参数。

**Python样例**:
```python
async def add_memories(self, user_id, scope_id, memories, llm=None, **kwargs):
    ...
    checker = MemUpdateChecker()
    action_items = await checker.check(new_memories, old_memories, base_chat_model=llm)
```

**Go问题**:
```go
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID string, scopeID string,
    memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error) {
    // 缺少 llm 参数
    checker := &update.MemUpdateChecker{}
    actionItems, err := checker.Check(newMemContent, oldMemories)  // 无法传递 LLM
```

**修复方案**: 在 `AddMemories` 签名中增加 `llm` 参数（或等效的 Model 接口），并传递给 `MemUpdateChecker.Check()`。同时修改 `BaseMemoryManager` 接口。

---

### S-02: MemUpdateChecker.Check 缺少 base_chat_model 参数

**Go文件**: `internal/agentcore/memory/manage/update/update_checker.go:79`
**Python参考**: `openjiuwen/core/memory/manage/update/mem_update_checker.py:126-132`

**问题描述**: Go 的 `Check()` 签名中缺少 `base_chat_model` 和 `retries` 参数，7.8 实现时无法传入 LLM。

**Python样例**:
```python
async def check(self, new_memories, old_memories, base_chat_model, retries=3):
    if base_chat_model is None:
        # 降级：直接返回全部 ADD
        return [MemoryActionItem(id=k, content=v, status=MemoryStatus.ADD) for k, v in new_memories.items()]
    # LLM 驱动的冲突检查
```

**Go问题**:
```go
func (c *MemUpdateChecker) Check(newMemories map[string]string, oldMemories map[string]string) ([]*MemoryActionItem, error) {
    // 缺少 baseChatModel 和 retries 参数
```

**修复方案**: 7.8 回填时修改 `Check` 签名，添加 `llm` 参数和 `retries` 参数。

---

### S-03: FragmentMemoryManager 缺失 _process_conflict_info 方法

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:82-101`

**问题描述**: Python 有 `_process_conflict_info` 静态方法，用于将 LLM 返回的冲突检查结果中的数字 ID 映射回实际记忆 ID。该方法处理 `conf_id == 0` 的特殊情况（映射为 `"-1"`），并通过 `input_memory_ids_map` 将数字 ID 映射为字符串 ID。Go 完全缺失此方法。

**Python样例**:
```python
@staticmethod
def _process_conflict_info(conflict_info, input_memory_ids_map):
    result = []
    for item in conflict_info:
        conf_id = item.get("conf_id", -1)
        if conf_id == 0:
            result.append({"id": "-1", "status": "DELETE"})
        else:
            result.append({"id": input_memory_ids_map.get(conf_id, str(conf_id)), "status": "ADD"})
    return result
```

**修复方案**: 7.8 回填时实现 `processConflictInfo` 方法，并标记为 ⤵️ 回填点。

---

### S-04: StructuredAskUserTool 的 input schema 缺少 query 字段

**Go文件**: `internal/swarm/server/rails/structured_ask_user_tool.go:34`
**Python参考**: `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py:86-103`

**问题描述**: Python 的 `StructuredAskUserTool` 使用 `EXTENDED_INPUT_PARAMS_EN/CN`，包含 `query` 和 `questions` 两个参数。Go 的 `NewStructuredAskUserTool` 使用 `BuildToolCard("ask_user", ...)` 从 `AskUserMetadataProvider` 获取 schema，但 `AskUserMetadataProvider.GetInputParams()` 只返回 `questions` 字段（没有 `query` 字段）。这导致 LLM 调用 `ask_user` 时无法传递 `query` 参数，功能缺失。

**Python样例**:
```python
EXTENDED_INPUT_PARAMS_EN = {
    "type": "object",
    "properties": {
        "query": {"type": "string", "description": "The question to present to the user (required)."},
        "questions": {"type": "array", "description": "...", "items": _QUESTIONS_ITEM_SCHEMA},
    },
    "required": ["query"],
}
```

**Go问题**:
```go
// AskUserMetadataProvider.GetInputParams 只返回 questions 字段，没有 query
func (p *AskUserMetadataProvider) GetInputParams(language string) map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "questions": map[string]any{...},  // 只有 questions，没有 query
        },
        "required": []any{"questions"},
    }
}
```

**修复方案**: 在 `AskUserMetadataProvider.GetInputParams()` 中添加 `query` 字段，对齐 Python 的 `EXTENDED_INPUT_PARAMS`。或者让 `StructuredAskUserTool` 不使用 `BuildToolCard`，而是直接构建包含 `query` + `questions` 的 schema。

---

### S-05: TeamBackend.shutdown_member 逻辑差异 — 缺少幂等成功 + 发送消息

**Go文件**: `internal/agent_teams/tools/team_backend.go:387-414`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:514-598`

**问题描述**: Go 的 `ShutdownMember` 对 SHUTDOWN/ERROR 返回 fail，Python 对 SHUTDOWN/SHUTDOWN_REQUESTED 返回 success（幂等）。Go 缺少发送消息步骤（Python 通过 `message_manager.send_message` 发送 `shutdown_request_content`）。

**Python样例**:
```python
# 幂等成功
if member.status in (MemberStatus.SHUTDOWN, MemberStatus.SHUTDOWN_REQUESTED):
    return MemberOpResult.success()
# 发送消息
await self.message_manager.send_message(
    content=t("team.shutdown_request_content"), to_member_name=member_name
)
```

**Go问题**:
```go
// SHUTDOWN/ERROR 返回 fail
if member.Status == string(atschema.MemberStatusShutdown) || member.Status == string(atschema.MemberStatusError) {
    return NewMemberOpResultFail("member already in terminal state"), nil
}
// 缺少发送消息步骤
```

**修复方案**: 1) SHUTDOWN/SHUTDOWN_REQUESTED 时返回 success（幂等）；2) 增加发送消息步骤。

---

### S-06: TeamBackend.cancel_member 逻辑完全不同

**Go文件**: `internal/agent_teams/tools/team_backend.go:426-455`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:600-663`

**问题描述**: Go 的 `CancelMember` 做了 CAS 状态转换到 SHUTDOWN_REQUESTED，Python 不做状态转换。Python 只在成员 BUSY 时执行取消，非 BUSY 返回 True。Go 缺少发送消息步骤。

**Python样例**:
```python
if member.status != MemberStatus.BUSY:
    return True  # 非 BUSY 不需要取消
# 重置 CLAIMED 任务
# 发送消息
await self.message_manager.send_message(content=t("team.cancel_request_content"), to_member_name=member_name)
```

**Go问题**:
```go
// Go 做 CAS 转换到 SHUTDOWN_REQUESTED
ok, err := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, ...)
```

**修复方案**: cancel_member 只在 BUSY 时执行取消，非 BUSY 返回 success；增加发送消息步骤；不做 CAS 到 SHUTDOWN_REQUESTED 的转换。

---

### S-07: TeamBackend.force_clean_team 直接 UpdateMemberStatus 而非调用 ShutdownMember

**Go文件**: `internal/agent_teams/tools/team_backend.go:590-612`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:729-761`

**问题描述**: Go 的 `ForceCleanTeam` 直接 `UpdateMemberStatus` 到 SHUTDOWN（跳过 FSM 校验、消息和事件），Python 对每个非自身成员调用 `shutdown_member`（含消息和事件）。

**Python样例**:
```python
if shutdown_members:
    for member_name in member_names:
        if member_name != self.leader_member_name:
            await self.shutdown_member(member_name, force=True)
```

**Go问题**:
```go
tb.db.Member().UpdateMemberStatus(ctx, memberName, string(atschema.MemberStatusShutdown))
// 跳过消息和事件
```

**修复方案**: Go 应调用 `ShutdownMember`（含消息和事件发布），而非直接 `UpdateMemberStatus`。

---

### S-08: TaskManager.add_with_priority 缺少 dependent_task_ids 参数

**Go文件**: `internal/agent_teams/tools/task_manager.go:453-471`
**Python参考**: `openjiuwen/agent_teams/tools/task_manager.py:285-369`

**问题描述**: Go 只接收 `dependsOnIDs`（新任务依赖的上游），缺少 `dependentTaskIDs`（依赖新任务的下游）参数。

**Python样例**:
```python
async def add_with_priority(self, ..., dependencies=None, dependent_task_ids=None, ...):
    await self.add_task_with_bidirectional_dependencies(
        dependencies=dependencies, dependent_task_ids=dependent_task_ids
    )
```

**Go问题**:
```go
func (tm *TeamTaskManager) AddWithPriority(ctx context.Context, ..., dependsOnIDs []string, ...) {
    tm.AddTaskWithBidirectionalDependencies(ctx, ..., dependsOnIDs)  // 缺少 dependentTaskIDs
}
```

**修复方案**: 添加 `dependentTaskIDs` 参数，并在 `AddTaskWithBidirectionalDependencies` 调用中传递。

---

### S-09: TaskManager.add_as_top_priority 使用两步操作而非原子操作

**Go文件**: `internal/agent_teams/tools/task_manager.go:474-514`
**Python参考**: `openjiuwen/agent_teams/tools/task_manager.py:371-451`

**问题描述**: Python 调用 `add_task_with_bidirectional_dependencies`（原子操作，含环路检测和状态刷新），Go 先 `CreateTask` 再通过 `MutateDependencyGraph` 添加边（两步操作，非原子）。

**Python样例**:
```python
dependent_task_ids = [t.task_id for t in all_pending if t.task_id != spec.task_id]
await self.add_task_with_bidirectional_dependencies(
    dependencies=None, dependent_task_ids=dependent_task_ids
)
```

**Go问题**:
```go
// 先创建任务
task, err := tm.CreateTask(ctx, ...)
// 再添加边
tm.db.Task().MutateDependencyGraph(ctx, taskID, mutationCtx)
```

**修复方案**: 使用 `AddTaskWithBidirectionalDependencies` 并传入 `dependentTaskIDs`，与 Python 保持一致。

---

### S-10: InMemoryDAO.loadEndpointsAndValidate 验证方向反了

**Go文件**: `internal/agent_teams/tools/database/memory_impl.go:964-992`
**Python参考**: `openjiuwen/agent_teams/tools/database/task_dao.py:209-244`

**问题描述**: Python 检查**下游任务（edge source / tid）**的状态是否在拒绝列表中，Go 检查的是**上游任务（upstream / depends_on_id）**。方向反了。

**Python样例**:
```python
for tid, dep_id in add_edges:
    src_status = endpoint_tasks[tid].status  # tid 是下游任务
    if src_status in TASK_DEPENDENCY_REJECT_STATUSES:
        raise _MutationFailure(...)
```

**Go问题**:
```go
if upstream.Status == fsm.TaskStatusCompleted || upstream.Status == fsm.TaskStatusCancelled {
    mc.failReason = "cannot add dependency on terminal task: " + edge.DependsOnID
    // 检查的是 upstream（DependsOnID），方向反了
```

**修复方案**: 修正验证逻辑，检查**下游任务（edge.TaskID）**的状态是否在拒绝列表中。同时，`TASK_DEPENDENCY_REJECT_STATUSES` 应包含 COMPLETED、CANCELLED、CLAIMED、PLAN_APPROVED、EXECUTING 等状态。

---

### S-11: InMemoryDAO.checkCycleAndComputeNewEdges 缺少边去重

**Go文件**: `internal/agent_teams/tools/database/memory_impl.go:996-1033`
**Python参考**: `openjiuwen/agent_teams/tools/database/task_dao.py:247-286`

**问题描述**: Python 在构建新边集时跳过已存在的边和重复的新边，Go 没有做边去重，可能导致重复边插入和唯一约束冲突。

**Python样例**:
```python
for tid, dep_id in add_edges:
    edge = (tid, dep_id)
    if edge in existing_edge_set or edge in new_edge_set:
        continue
    new_edge_set.add(edge)
```

**Go问题**:
```go
// 直接添加所有 addEdges，没有去重
for _, edge := range addEdges {
    adjacency[edge.TaskID] = append(adjacency[edge.TaskID], edge.DependsOnID)
    newEdgeRows = append(newEdgeRows, ...)
}
```

**修复方案**: 在构建邻接表之前，先检查边是否已存在于 `db.deps` 中，并去重 `addEdges` 中的重复边。

---

### S-12: CodeAdapter.buildConfiguredSubagents 中 code_agent 缺少 CodingMemoryRail 注入

**Go文件**: `internal/swarm/server/adapter/code_adapter.go:543-561`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:711-728`

**问题描述**: Python 中 code_agent 的构建逻辑包含 `CodingMemoryRail` 和 `SysOperationRail` 作为 rails，Go 的 code_agent 没有注入这些 rails。

**Python样例**:
```python
code_agent_rails = None
coding_memory_rail = self._coding_memory_rail
if coding_memory_rail is not None:
    code_agent_rails = [SysOperationRail(), coding_memory_rail]
code_spec = build_code_agent_config(model, workspace=workspace, language=resolved_language,
    rails=code_agent_rails, max_iterations=...)
```

**Go问题**:
```go
codeCfg := subagents.BuildCodeAgentConfig(c.deep.model, codeParams)
if codeCfg != nil {
    // ⤵️ 7.8: CodingMemoryRail 条件注入（当前 nil 占位）
    specs = append(specs, codeCfg)
}
```

**修复方案**: 在 `codeCfg` 创建后，检查 `c.codingMemoryRail` 是否已构建，如果是，将 `CodingMemoryRail` 注入到 `codeCfg` 的 rails 列表中。

---

### S-13: CodeAdapter._resolve_output_language 未对齐 Python

**Go文件**: `internal/swarm/server/adapter/code_adapter.go:488-490`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:204-218`

**问题描述**: Python 从 `get_config().get("preferred_language", "zh")` 读取用户偏好语言，然后调用 `resolve_language()` 做归一化。Go 直接委托给 `deep.resolveRuntimeLanguage()`，在 code 模式下会返回 "en"（因为 code 模式强制英文运行时），而不是 Python 那样独立读取用户偏好语言。

**Python样例**:
```python
def _resolve_output_language(self):
    lang = self._config_base.get("preferred_language", "zh")
    return resolve_language(lang)
```

**Go问题**:
```go
func (c *CodeAdapter) resolveOutputLanguage() string {
    return c.deep.resolveRuntimeLanguage()  // 返回 "en"，而非从 config 读取
}
```

**修复方案**: 在 `resolveOutputLanguage` 中读取 `configBase["preferred_language"]`，归一化后返回。

---

### S-14: SkillToolkit.install_skill 中 name 回退逻辑缺失

**Go文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go:357-361`
**Python参考**: `jiuwenswarm/agents/harness/common/tools/skill_toolkits.py:409-412`

**问题描述**: Go 缺少对 `skillnet` 来源的 `Path(target).name` 提取逻辑。当 skillnet 安装返回的 `skill` 对象不含 `name` 时，Go 直接用 `identifier`（一个 URL），而 Python 用 `Path(target).name` 从 URL 提取文件名部分。

**Python样例**:
```python
name = Path(target).name if resolved_source == "skillnet" else target
```

**Go问题**:
```go
name := strings.TrimSpace(toString(skill["name"]))
if name == "" {
    name = identifier  // 对 skillnet 来说 identifier 是 URL，应该提取文件名
}
```

**修复方案**: 在 `name == ""` 分支中，当 `normalizedSource == "skillnet"` 时，用 `filepath.Base(identifier)` 提取名称。

---

## 四、一般问题

### G-01: FragmentMemoryManager.ListFragmentMemories 缺少 mem_type 有效性校验和结果排序

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:280-298`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:300-334`

Python 检查 `mem_type.value not in FRAGMENT_MEMORY_TYPE` 时记录 error 并返回空列表，且对结果做 `result.sort(key=lambda x: (x['mem'], str(x.get('timestamp') or '')), reverse=True)` 排序。Go 缺少校验和排序。

**修复方案**: 添加 mem_type 有效性校验和结果排序逻辑。

---

### G-02: FragmentMemoryManager.Search/Get 返回类型与 Python 不一致

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:202-232`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:221-260`

Python 的 `search()` 返回 `list[dict]`（通过 `_doc_to_dict` 转换，含 id/mem/mem_type/timestamp/score/source_id），Go 返回 `[]*index.MemorySearchResult` / `*index.MemoryDoc`。Go 缺少 `_doc_to_dict` 转换方法。

**修复方案**: 如果上层调用方需要 dict 格式，应补充 `docToDict` 方法；如果 Go 的类型系统下直接使用结构体更合理，可忽略。

---

### G-03: SupportMemoryType 枚举缺失

**Go文件**: `internal/agentcore/memory/manage/mem_model/memory_unit.go`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/memory_unit.py:24-27`

Python 定义了 `SupportMemoryType` 枚举（USER_PROFILE + SUMMARY），Go 中缺失。

**修复方案**: 确认是否有使用场景，如有则补充。

---

### G-04: DataIdManager 哈希算法与 Python 不一致

**Go文件**: `internal/agentcore/memory/manage/mem_model/data_id_manager.go:51-53`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/data_id_manager.py:13`

Python 使用 `hash(user_id)`（非确定性），Go 使用 `fnv.New32a()`（确定性）。相同 user_id 在 Python 和 Go 中生成的 ID 不同。

**修复方案**: 如果不需要跨语言 ID 兼容性，当前实现可接受，但注释应修正。

---

### G-05: create_tables 缺少 group_id 迁移检测和版本初始化

**Go文件**: `internal/agentcore/memory/manage/mem_model/db_model.go:70-76`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/db_model.py:61-110`

Python 的 `create_tables` 有完整逻辑：检测旧表是否有 `group_id` 列、记录新建表名、初始化 schema 版本号。Go 的 `CreateTables` 只做 `AutoMigrate`。

**修复方案**: 按需实现 group_id 迁移检测和版本初始化。

---

### G-06: TeamBackend.clean_team 对 ERROR 状态的成员允许清理

**Go文件**: `internal/agent_teams/tools/team_backend.go:553-586`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:665-727`

Python 只接受 SHUTDOWN 状态，Go 多检查了 ERROR 状态。

**修复方案**: CleanTeam 应只接受 SHUTDOWN 状态（与 Python 一致）。

---

### G-07: TeamBackend.startup/startup_member 缺少 on_created 回调和 MemberSpawnedEvent

**Go文件**: `internal/agent_teams/tools/team_backend.go:344-375`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:336-396`

Go 缺少 `on_created` 回调、`MemberSpawnedEvent` 事件发布和回滚机制。

**修复方案**: 添加 `on_created` 回调参数、`MemberSpawnedEvent` 发布和回滚机制。

---

### G-08: TeamBackend.BuildTeam 缺少 CreateTeam 返回值检查

**Go文件**: `internal/agent_teams/tools/team_backend.go:490`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:984-992`

Go 没有检查 `CreateTeam` 返回值。

**修复方案**: 检查 CreateTeam 返回值，失败时返回 error。

---

### G-09: InMemoryDAO.DeleteTeam 级联删除不完整

**Go文件**: `internal/agent_teams/tools/database/memory_impl.go:221-236`
**Python参考**: Python 的 CASCADE 删除应删除所有关联表数据

Go `DeleteTeam` 只级联删除成员，缺少任务、依赖、消息和已读水位。

**修复方案**: 在 DeleteTeam 中也级联删除任务、依赖、消息和已读水位。

---

### G-10: TaskManager.complete 缺少 MemberName 在 TaskCompletedEvent

**Go文件**: `internal/agent_teams/tools/task_manager.go:378-384`
**Python参考**: `openjiuwen/agent_teams/tools/task_manager.py:734-741`

Go 的 `TaskCompletedEvent` 缺少 `MemberName` 字段（Python 从 task 的 assignee 获取）。

**修复方案**: 在 TaskCompletedEvent 中添加 MemberName。

---

### G-11: InMemoryDAO.GetMessages 缺少排序

**Go文件**: `internal/agent_teams/tools/database/memory_impl.go:758-807`
**Python参考**: `openjiuwen/agent_teams/tools/database/message_dao.py:104-167`

Python 使用 `query.order_by(message_model.timestamp)` 排序，Go 的 InMemory 实现没有排序。

**修复方案**: 对结果按 Timestamp 排序。

---

### G-12: SkillToolkit 搜索/安装/卸载方法缺少顶层异常捕获

**Go文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go:153-453`
**Python参考**: `jiuwenswarm/agents/harness/common/tools/skill_toolkits.py:209-499`

Python 的 `search_skill`/`install_skill`/`uninstall_skill` 都有 `try/except Exception` 包裹，异常时返回友好错误。Go 缺少此保护。

**修复方案**: 在各方法入口添加 `defer func() { if r := recover(); r != nil { ... } }()` 保护。

---

### G-13: CodeAdapter.buildConfiguredSubagents 中 explore/plan agent 的 max_iterations 未从配置读取

**Go文件**: `internal/swarm/server/adapter/code_adapter.go:505-538`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:678-705`

Python 从 `explore_agent_cfg.get("max_iterations")` 或 `react_cfg.get("max_iterations", 15)` 读取，Go 硬编码了 `MaxIterations: 15`。

**修复方案**: 从 `subagentsCfg` 和 `configCache` 读取 max_iterations。

---

### G-14: CodeAdapter 缺少 _update_rails_for_mode 方法

**Go文件**: `internal/swarm/server/adapter/code_adapter.go`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:770-824`

Python 的 `_update_rails_for_mode` 处理 code 模式下的 rail 生命周期（卸载 TaskPlanningRail/SkillEvolutionRail，保留 SubagentRail/ProjectMemoryRail/CodingMemoryRail）。Go 没有。

**修复方案**: 在 CodeAdapter 中实现 `_update_rails_for_mode`。

---

### G-15: CodeAdapter.CreateInstance 中缺少 dotenv.Load

**Go文件**: `internal/swarm/server/adapter/code_adapter.go:164-377`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:221-342`

Python 的 `create_instance` 通过 `super().create_instance()` 调用父类，父类会执行 `load_dotenv()`。Go 的 CodeAdapter.CreateInstance 完全重写了 CreateInstance，没有调用 `dotenv.Load()`。

**修复方案**: 在步骤 3 之前添加 `dotenv.Load(workspace.EnvFile())` 调用。

---

### G-16: HumanAgentInbox.driveAgent 缺少 agent.deliver_input 调用

**Go文件**: `internal/agent_teams/interaction/human_agent_inbox.go:195-216`
**Python参考**: `openjiuwen/agent_teams/interaction/human_agent_inbox.py:217-229`

Python 调用 `agent.deliver_input(body)` 驱动 avatar，Go 只返回 success 但未实际驱动。

**修复方案**: 实现 `agent.DeliverInput(ctx, body)` 调用。

---

### G-17: CodeAdapter.buildConfiguredSubagents 中 research_agent 不应在 code 模式下出现

**Go文件**: `internal/swarm/server/adapter/code_adapter.go:571-577`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:659-766`

Python 的 CodeAdapter 没有包含 `research_agent`，Go 的 CodeAdapter 包含了。

**修复方案**: 移除 `research_agent` 的处理，或确认 Python 是否也支持。

---

### G-18: StreamController 多个 ⤵️ 标记已实现但注释未更新

**Go文件**: `internal/agent_teams/agent/stream_controller.go:44-47,61-62,558-559,585-586`

`wakeMailboxCb`、`requestCompletionPollCb`、`pendingInterruptResumes` 已通过选项注入实现，但注释仍标记为 ⤵️。

**修复方案**: 移除已实现的 ⤵️ 注释。

---

### G-19: InMemoryDAO.update_task_status 对 CANCELLED 的处理与 Python 不一致

**Go文件**: `internal/agent_teams/tools/database/memory_impl.go:583-604`
**Python参考**: `openjiuwen/agent_teams/tools/database/task_dao.py:497-542`

Python 的 `update_task_status` 只在 `status == COMPLETED` 时执行终态传播，Go 在 `status == COMPLETED || status == CANCELLED` 时都执行。

**修复方案**: 确认 Go 的设计意图。如果要对齐 Python，则 `UpdateTaskStatus` 只在 COMPLETED 时执行终态传播。

---

## 五、提示问题

### T-01: FragmentMemoryManager._add_memory_to_store 独立方法缺失

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:415-448`

Python 有独立的 `_add_memory_to_store` 方法，Go 中逻辑已内联到 `AddMemories`。如果不需要单独调用，当前实现可接受。

---

### T-02: FragmentMemoryManager.add_memories 异常处理模式与 Go 不同

**Go文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:67-167`
**Python参考**: `openjiuwen/core/memory/manage/index/fragment_memory_manager.py:125-196`

Python 使用 try/except + BaseError re-raise，Go 的 `wrapException` 已实现等价逻辑。

---

### T-03: Python 的 SemanticStore 和 UserMemStore 在 Go 中无对应

**Go文件**: N/A
**Python参考**: `openjiuwen/core/memory/manage/index/semantic_store.py`, `user_mem_store.py`

可能属于其他章节（7.7/7.10）的实现范围。

---

### T-04: SqlMessageStore.generateMessageID 时间格式可能与 Python 不一致

**Go文件**: `internal/agentcore/memory/manage/mem_model/sql_message_store.go:367`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/sql_message_store.py:43-46`

Go 使用 `"2006-01-02 15:04:05-07:00"`，Python 使用 `str(datetime)`。如果格式不一致，可能导致相同数据生成不同的 message_id。

**修复方案**: 验证 Python `str(datetime)` 的输出格式与 Go 是否一致。

---

### T-05: CodeAdapter.buildFilesystemRail(false) 参数语义待确认

**Go文件**: `internal/swarm/server/adapter/code_adapter.go:779`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:374`

Python 的 `_build_filesystem_rail()` 不接受参数，Go 传了 `false`。需确认 code 模式下是否应传 `true`。

---

### T-06: CodeAdapter.buildAgentModeRail(nil) 参数语义待确认

**Go文件**: `internal/swarm/server/adapter/code_adapter.go:792`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:376`

传了 `nil` 参数，需确认是否正确。

---

### T-07: InMemoryDAO.mutateDependencyGraph 步骤4/5 失败时没有回滚逻辑

**Go文件**: `internal/agent_teams/tools/database/memory_impl.go:964-992`
**Python参考**: `openjiuwen/agent_teams/tools/database/task_dao.py:584-641`

Python 使用 `session.rollback()` 一次性回滚整个事务。Go 的步骤4/5 失败时没有回滚逻辑，可能导致数据不一致。

---

### T-08: SqlDbStore/SqlMessageStore 的错误处理差异（Go 用 error，Python 用 bool）

**Go文件**: `internal/agentcore/memory/manage/mem_model/sql_db_store.go`, `sql_message_store.go`
**Python参考**: `openjiuwen/core/memory/manage/mem_model/sql_db_store.py`, `sql_message_store.py`

Go 的 error 返回模式更符合 Go 习惯，也更利于调用方正确处理错误。这是合理的设计差异。

---

## 六、⤵️ 标记验证汇总

| 标记 | 文件 | 状态 | 说明 |
|------|------|------|------|
| ⤵️ 7.8 MemUpdateChecker | `update/update_checker.go` | 真实未实现 | stub 实现，7.8 回填时需完整实现 LLM 驱动冲突检查 |
| ⤵️ 7.8 CodingMemoryRail | `code_adapter.go:543` | 真实未实现 | code_agent 缺少 CodingMemoryRail 注入 |
| ⤵️ 10.6.3-10 LspRail | `code_adapter.go:759` | 真实未实现 | 返回 nil |
| ⤵️ 10.6.3-10 ProjectMemoryRail | `code_adapter.go:766` | 真实未实现 | 返回 nil |
| ⤵️ 10.6.3-10 PermissionInterruptRail | `code_adapter.go:985` | 真实未实现 | 参数传 nil |
| ⤵️ 10.6.3-10 WorktreeRail | `code_adapter.go:773` | 真实未实现 | 返回 nil |
| ⤵️ 9.55 TeamAgent | `human_agent_inbox.go:195` | 真实未实现 | driveAgent 缺少 deliver_input 调用 |
| ⤵️ 9.55 pendingInterruptResumes | `stream_controller.go:61` | **已实现** | 类型为 `[]any`，注释应更新 |
| ⤵️ 9.62 wakeMailboxCb | `stream_controller.go:44` | **已实现** | 通过 WithWakeMailbox 注入，注释应更新 |
| ⤵️ 9.62 requestCompletionPollCb | `stream_controller.go:47` | **已实现** | 通过 WithRequestCompletionPoll 注入，注释应更新 |

---

## 七、优先修复建议

### 第一优先级（功能 bug，影响正确性）

| # | 问题 | 修复复杂度 |
|---|------|-----------|
| S-10 | loadEndpointsAndValidate 验证方向反了 | 中 |
| S-11 | checkCycleAndComputeNewEdges 缺少边去重 | 低 |
| S-04 | StructuredAskUserTool 缺少 query 字段 | 低 |
| S-05 | shutdown_member 缺少幂等成功+发送消息 | 中 |
| S-06 | cancel_member 逻辑完全不同 | 中 |
| S-07 | force_clean_team 应调用 ShutdownMember | 低 |

### 第二优先级（7.8 回填时必须修复）

| # | 问题 | 修复复杂度 |
|---|------|-----------|
| S-01 | AddMemories 缺少 llm 参数 | 中 |
| S-02 | Check 缺少 base_chat_model 参数 | 低 |
| S-03 | 缺失 _process_conflict_info 方法 | 低 |
| S-08 | add_with_priority 缺少 dependent_task_ids | 低 |
| S-09 | add_as_top_priority 非原子操作 | 中 |

### 第三优先级（功能缺失，影响行为一致性）

| # | 问题 | 修复复杂度 |
|---|------|-----------|
| S-12 | code_agent 缺少 CodingMemoryRail | 低 |
| S-13 | resolveOutputLanguage 未对齐 | 低 |
| S-14 | install_skill name 回退逻辑缺失 | 低 |
| G-01 | ListFragmentMemories 缺少校验和排序 | 低 |
| G-06 | clean_team ERROR 状态处理 | 低 |
| G-09 | DeleteTeam 级联删除不完整 | 中 |
