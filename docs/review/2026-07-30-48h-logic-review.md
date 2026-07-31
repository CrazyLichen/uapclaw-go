# 14天代码逻辑审查报告（2026-07-16 ~ 2026-07-30）

> 审查范围：对比 Python 参考项目（openjiuwen / jiuwenswarm），审查 Go 移植过程中的逻辑 bug、步骤缺失和行为不一致。
> 审查方法：逐方法对比 Python 和 Go 的方法签名、执行步骤、默认参数和分支逻辑。

---

## 严重问题（共 12 个）

### S01. TruncateHistoryRecords 签名与 Python 不一致 — 按 request_id 截断 vs 按 cut_index 截断

**严重程度**：严重 — rewind 功能行为完全不同

**Python 样例**：
```python
# jiuwenswarm/server/runtime/session/session_history.py L255
def truncate_history_records(*, session_id: str, cut_index: int) -> dict[str, Any]:
    """截断会话历史到指定位置（线程安全）。"""
    # ...
    truncated = history[:cut_index]  # 按数值索引位置截断
    return {"remaining_records": len(truncated), "removed_records": total - len(truncated)}
```

**Go 问题代码**：
```go
// internal/swarm/server/runtime/session_history.go L172
func TruncateHistoryRecords(sessionID string, requestID string) error {
    // 按 request_id 遍历查找再截断，语义完全不同
    truncateIdx := -1
    for i, r := range records {
        if rid, ok := r["request_id"].(string); ok && rid == requestID {
            truncateIdx = i
        }
    }
    // ...
}
```

**修复方案**：修改签名为 `TruncateHistoryRecords(sessionID string, cutIndex int) (map[string]any, error)`，按索引位置截断，对齐 Python。同时返回 remaining/removed 计数。调用方需要从 request_id 映射到 cut_index 后再调用。

**流程示例**：
```
Python: rewind → 计算 cut_index → truncate_history_records(session_id, cut_index=5) → 保留前5条
Go(当前): rewind → TruncateHistoryRecords(session_id, request_id) → 找到该 request_id 的最后出现位置 → 保留到该位置
差异场景: 同一 request_id 下有 user+assistant 两条记录（索引4、5），Python 按 cut_index=4 只保留前4条，
         Go 按 request_id 找到最后索引5，保留了6条（包含不应保留的 assistant 回复）
```

---

### S02. AppendHistoryRecord 缺少 session_metadata 联动

**严重程度**：严重 — 消息计数不更新、标题不自动生成、delivery context 不刷新

**Python 样例**：
```python
# jiuwenswarm/server/runtime/session/session_history.py L176-200
def append_history_record(...):
    # ... 写入 history.json 后 ...
    from jiuwenswarm.server.runtime.session.session_metadata import (
        set_session_delivery_context,
        update_session_metadata,
    )
    update_session_metadata(
        session_id=sid, channel_id=cid,
        increment_message_count=True,
        user_content=content_text if role_norm == "user" else None,
        channel_metadata=channel_metadata, mode=mode,
    )
    if role_norm == "user":
        set_session_delivery_context(
            session_id=sid, channel_id=cid,
            source_request_id=rid,
            route_metadata=channel_metadata,
        )
```

**Go 问题代码**：
```go
// internal/swarm/server/runtime/session_history.go L51-101
func AppendHistoryRecord(sessionID, requestID, channelID, role, content string, ...) {
    // ... 只写入 history.json，完全没有 update_session_metadata 和 set_session_delivery_context 调用
    ensureHistoryWorker()
    select {
    case historyWriteQueue <- historyWriteItem{sessionID: sid, record: item}:
    default:
        writeHistoryItem(sid, item)
    }
}
```

**修复方案**：在 `AppendHistoryRecord` 入队后，添加 `UpdateSessionMetadata` 和 `SetSessionDeliveryContext` 调用。需要先实现 `session_metadata.go` 模块。

---

### S03. read_team_history_records 完全缺失

**严重程度**：严重 — team 模式历史记录无法读取

**Python 样例**：
```python
# jiuwenswarm/server/runtime/session/session_history.py L76
def read_team_history_records(session_id: str) -> list[dict[str, Any]]:
    """读取 team 模式相关的历史记录。"""
    # 含 5 次递增间隔重试逻辑
    # 含 _is_team_relevant 事件过滤
```

**Go 问题代码**：文件不存在，无等价实现。

**修复方案**：在 `session_history.go` 中新增 `ReadTeamHistoryRecords(sessionID string) ([]map[string]any, error)` 函数，包含重试和过滤逻辑。

---

### S04. session_metadata 独立模块缺失

**严重程度**：严重 — 功能碎片化，缺少关键功能

**Python 样例**：
```python
# jiuwenswarm/server/runtime/session/session_metadata.py 包含：
# - init_session_metadata()
# - update_session_metadata() — 增量更新 message_count、自动生成标题、channel_metadata 首次写入
# - get_session_metadata()
# - increment_session_round_count()
# - set_session_delivery_context() / get_session_delivery_context()
# - build_server_push_message() — 构造 evolution watcher 的 server_push 消息
# - remove_team_mode_session_dirs_at_startup() — 启动时清理 team 模式会话目录
# - get_all_sessions_metadata()
# - _METADATA_CACHE + _CACHE_LOCK 缓存机制
```

**Go 问题代码**：无独立 `session_metadata.go`，相关逻辑散落在 `handle_session.go` 中，且缺少 `remove_team_mode_session_dirs_at_startup` 和 `build_server_push_message`。

**修复方案**：创建独立的 `session_metadata.go`，一比一对齐 Python `session_metadata.py` 的所有函数。

---

### S05. BrowserAgent Rails 注入逻辑错误 — 用户传 Rails 时丢失 BrowserRuntimeRail

**严重程度**：严重 — 浏览器代理核心功能失效

**Python 样例**：
```python
# openjiuwen/harness/subagents/browser_agent.py L240-243
injected_rails = [BrowserRuntimeRail(browser_backend)]
final_rails = list(rails or []) + injected_rails  # 合并语义：总是追加
```

**Go 问题代码**：
```go
// internal/agentcore/harness/browser_agent_factory.go L62-65
finalRails := params.Rails
if finalRails == nil {
    finalRails = []sainterfaces.AgentRail{bm.NewBrowserRuntimeRail(runtime)}
}
// 覆盖语义：用户传了 rails 就完全替代，BrowserRuntimeRail 丢失
```

**修复方案**：改为合并语义，总是追加 BrowserRuntimeRail：
```go
finalRails := make([]sainterfaces.AgentRail, 0)
finalRails = append(finalRails, params.Rails...)
finalRails = append(finalRails, bm.NewBrowserRuntimeRail(runtime))
```

---

### S06. ToolOptimizerBase.Bind() 始终返回 0，未实现绑定逻辑

**严重程度**：严重 — tool 域优化完全不可用

**Python 样例**：
```python
# openjiuwen/agent_evolving/optimizer/tool_call/base.py
class ToolOptimizerBase(BaseOptimizer):
    def bind(self, agent, operators, targets=None):
        bound = super().bind(agent, operators, targets)  # 调用 BaseOptimizer.bind → filter_operators
        # ... 完整绑定逻辑
```

**Go 问题代码**：
```go
// internal/evolving/optimizer/tool_call/base.go:264
func (b *ToolOptimizerBase) Bind(agent any, operators []any, targets []string) int {
    // ⤵️ 9.70: 等待 Trainer 实现后回填 Operator 类型转换
    return 0
}
```

**修复方案**：实现 `filter_operators` 逻辑，遍历 operators 过滤出 `ToolCallOperator` 类型，设置到 ToolOptimizerBase 的 operators 字段，返回绑定数量。

---

### S07. Trainer.Train 未传递 trajectories 给 Updater.Update

**严重程度**：严重 — 优化器无法获取轨迹，InstructionOptimizer 等依赖轨迹的优化器无法工作

**Python 样例**：
```python
# openjiuwen/agent_evolving/trainer/trainer.py
updated = await self._updater.update(trajectories, evaluated, config=kwargs)
# trajectories 从 forward 中提取的执行轨迹
```

**Go 问题代码**：
```go
// internal/evolving/trainer/trainer.go:206
t.updater.Update(ctx, nil, evaluated, config)  // trajectories 参数始终传 nil
```

**修复方案**：在 `Forward()` 中实现轨迹提取，将提取结果传递给 `Train()` 中的 `updater.Update()`。

---

### S08. Trainer.Train 未实现候选列表多方案评估

**严重程度**：严重 — beam_search 返回多个候选时无法选最优

**Python 样例**：
```python
# openjiuwen/agent_evolving/trainer/trainer.py
if isinstance(updated, list):
    val_score, val_evaluated = self._select_best_candidate_on_val(
        agent, operators, candidates, val_cases
    )
```

**Go 问题代码**：
```go
// internal/evolving/trainer/trainer.go:221-222
// ⤵️ 待 9.72 Optimizer 回填：候选列表多方案评估，当前仅处理单方案
```

**修复方案**：在 `Train()` 中检查 `update` 返回值是否为列表，若是则实现 `_select_best_candidate_on_val` 逻辑：对每个候选在验证集上评估，选最优。

---

### S09. AgentConfigurator.SetupInfra()/SetupAgent() 几乎全是 TODO 占位

**严重程度**：严重 — 团队 Agent 无法正常初始化

**Python 样例**：
```python
# openjiuwen/agent_teams/agent/agent_configurator.py
class AgentConfigurator:
    async def setup_infra(self):
        # 完整实现：messager 创建、workspace manager、model allocator、
        # team backend 设置、worktree manager 创建

    async def setup_agent(self):
        # 完整实现：workspace 路径解析+symlink、buildSpec 深拷贝+覆盖、
        # 六种 Rail 构造、TeamHarness.build()、memory manager、agent_customizer
```

**Go 问题代码**：
```go
// internal/agent_teams/agent/agent_configurator.go
func (c *AgentConfigurator) SetupInfra(...) {
    // 只有 Blueprint 和 SpawnPayloadBuilder 构建，步骤 5-9 全部 TODO
}
func (c *AgentConfigurator) SetupAgent(...) {
    // 只有空的 harness 构建调用，步骤 1-17 全部 TODO
}
```

**修复方案**：逐步骤对齐 Python 实现。这是大工程，但 9.57 标记为 ✅ 时应实际完成。

---

### S10. runtime/dispatch.py 完全未移植

**严重程度**：严重 — activate 无法决策创建/恢复/拒绝

**Python 样例**：
```python
# openjiuwen/agent_teams/runtime/dispatch.py
class RunActionKind(str, Enum):
    CREATE = "create"
    NEW_TEAM_IN_SESSION = "new_team_in_session"
    COLD_RECOVER = "cold_recover"
    RESUME_FROM_PAUSE = "resume_from_pause"
    REJECT_RUNNING = "reject_running"
    REJECT_ORPHANED = "reject_orphaned"
    REJECT_INCONSISTENT = "reject_inconsistent"

def decide_run_action(*, team_in_db, team_in_session, pool_entry, ...) -> RunAction:
    # 完整的调度真值表
```

**Go 问题代码**：文件不存在。

**修复方案**：创建 `internal/agent_teams/runtime/dispatch.go`，一比一对齐 `RunActionKind` 枚举和 `decide_run_action` 函数。

---

### S11. runtime/metadata.py 完全未移植

**严重程度**：严重 — session checkpoint 中的团队数据无法持久化和恢复

**Python 样例**：
```python
# openjiuwen/agent_teams/runtime/metadata.py
TEAMS_KEY = "teams"
TEAM_DB_STATE_PENDING_CREATE = "pending_create"
TEAM_DB_STATE_CREATED = "created"
TEAM_DB_STATE_CLEANED = "cleaned"

def read_teams_bucket(session) -> dict[str, dict[str, Any]]: ...
def read_team_namespace(session, team_name) -> dict | None: ...
def write_team_namespace(session, team_name, payload) -> None: ...
def merge_team_namespace(session, team_name, partial) -> None: ...
def read_team_db_state(session, team_name) -> str | None: ...
def merge_team_db_state(session, team_name, state) -> None: ...
def remove_team_namespace(session, team_name) -> bool: ...
```

**Go 问题代码**：文件不存在。

**修复方案**：创建 `internal/agent_teams/runtime/metadata.go`，实现所有元数据读写函数。

---

### S12. TeamRuntimeManager 大部分方法是 stub

**严重程度**：严重 — 运行时管理器无法管理团队生命周期

**Python 样例**：
```python
# openjiuwen/agent_teams/runtime/manager.py
class TeamRuntimeManager:
    async def activate(self, ...):    # 完整调度+副作用流程
    async def finalize(self, ...):    # pause-vs-stop 决策
    async def finalize_member(self, ...):  # 非 leader 终结逻辑
    async def delete_team(self, ...): # 完整删除流程
    async def release_session(self, ...): # 会话释放
    # 共 12 个完整方法
```

**Go 问题代码**：
```go
// internal/agent_teams/runtime/manager.go
// Activate(), Finalize(), Pause(), StopTeam(), DeleteTeam() 全部只是日志+返回空
```

**修复方案**：逐方法对齐 Python 实现，优先实现 `Activate`（依赖 dispatch.go）和 `Finalize`。

---

## 一般问题（共 18 个）

### G01. BrowserAgent 工具注入顺序反转

**Python**：用户工具在前，runtime 工具在后（`final_tools = list(tools or []) + injected_tools`）
**Go**：runtime 工具在前，用户工具追加（`allToolInstances = runtimeTools` 然后 `append(params.ToolInstances...)`）
**修复**：改为 `allToolInstances = append(params.ToolInstances, runtimeTools...)`

---

### G02. BuildBrowserRuntimeTools 缺少 language 参数

**Python**：`build_browser_runtime_tools(runtime, language="cn")` — 根据语言选择工具描述
**Go**：`BuildBrowserRuntimeTools(runtime)` — 无 language 参数
**修复**：添加 `language string` 参数

---

### G03. CreateExploreAgent 的 SysOperationRail 默认值不一致

**Python**：`create_explore_agent` 用 `SysOperationRail()`（不传 read_only），允许写操作
**Go**：统一用 `WithReadOnly(true)`，双重约束（提示词 + Rail）
**说明**：Go 的做法更安全，但与 Python 行为不一致。如果严格对齐 Python，`CreateExploreAgent` 应使用 `NewSysOperationRail()`（不传 read_only）。

---

### G04. CreateVerificationAgent 工厂函数缺失

**Python**：有 `create_verification_agent` 函数
**Go**：只有 `BuildVerificationAgentConfig`，无 `CreateVerificationAgent`
**修复**：在 `harness` 包中新增 `CreateVerificationAgent` 工厂函数，对齐其他 agent 模式。

---

### G05. AgentManager.Initialize() ACP 逻辑未实现

**Python**：`initialize()` 对 "acp" 通道有完整实现
**Go**：`Initialize()` 返回 nil, nil，仅有 ⤵️ 注释
**说明**：随 ACP 实现时一并补充。

---

### G06. AgentManager.CreateSession() ACP 分支未实现

**Python**：ACP 通道创建 `acp_{uuid[:8]}` 格式 session_id
**Go**：ACP 分支只有 ⤵️ 注释，始终返回 "default"
**说明**：随 ACP 实现时一并补充。

---

### G07. AgentManager.ReloadAgentsConfig() 缺少 team evolution config 更新

**Python**：reload 后调用 `get_team_manager(channel_id).update_evolution_config(team_config)`
**Go**：只有 TODO 注释
**修复**：补充 team evolution config 热更新逻辑。

---

### G08. CodeAdapter.CreateInstance() 大量步骤仅有 ⤵️ 占位

步骤 4（多模态配置刷新）、13-19（agentCard/toolCards/rails/subagents/instance 创建）、21（coding_memory workspace）、24（load_user_rails）均标记为 ⤵️ 延后实现。
**说明**：Code 模式无法真正创建 agent 实例，需逐步回填。

---

### G09. UapClaw.CreateInstance() 缺少 dreaming 启动

**Python**：创建 agent 后 `asyncio.create_task(adapter.try_start_dreaming(...))`
**Go**：未实现
**修复**：在 CreateInstance 末尾添加 dreaming 启动逻辑。

---

### G10. UapClaw.ReloadAgentConfig() 缺少 dreaming 停止/重启

**Python**：reload 时先 `try_stop_dreaming()`，reload 后 `try_start_dreaming()`
**Go**：未实现
**修复**：补充 dreaming 生命周期管理。

---

### G11. AgentConfigService.ListAvailableTools() InternalName 硬编码不一致

**Python**：使用 `_TOOL_DISPLAY_NAMES` 反向映射，LSP 的 internal_name 是 "lsp"（小写）
**Go**：硬编码中 LSP 的 InternalName 为 "LSP"（大写），Read 的 InternalName 为 "Read"（Python 中应为 "read_file"）
**修复**：对齐 Python 的 `_TOOL_DISPLAY_NAMES` 映射。

---

### G12. ToolOptimizerBase.OptimizeTool 索引差异

**Python**：`result_descs[-1][-1][0]["description"]` — 取最后一个 batch 的最后一个 node 的**第一个** step
**Go**：取最后一个 batch 的最后一个 node 的**最后一个** step
**修复**：改为取 `[0]`（第一个 step），对齐 Python。

---

### G13. Trainer.Forward 中轨迹提取未实现

**Go**：`var trajectories any = nil`，注释 `⤵️ 待 9.77 Trajectory Extractor 回填`
**修复**：实现轨迹提取逻辑，对齐 Python。

---

### G14. UserInbox.Direct/Broadcast 是 stub

**Go**：返回硬编码 `"stub-direct-msg-id"`，未调用 TeamMessageManager
**修复**：接入实际的 MessageManager 发送逻辑。

---

### G15. HumanAgentInbox.driveAgent 未调用 agent.DeliverInput

**Go**：找到 agent 后直接返回成功，未调用 `DeliverInput`
**Python**：`await agent.deliver_input(body)`
**修复**：补充 `agent.DeliverInput(ctx, body)` 调用。

---

### G16. TeamRuntimeManager.dispatchPayload 中 UserInbox 传入 nil messageManager

**Go**：`interaction.NewUserInbox(nil)` + 缺少 `auto_start_all()/auto_start_member()` 调用
**Python**：传入 `backend.message_manager` + 调用 `auto_start_all()/auto_start_member()`
**修复**：传入实际的 messageManager，补充 auto_start 调用。

---

### G17. SpawnManager.BuildContextFromDB 是空实现

**Go**：只返回空的 `TeamRuntimeContext{}`
**修复**：从数据库读取 teammate 行并构建上下文。

---

### G18. SpawnManager.RestartTeammate 缺少 initialMessage 和 sessionID

**Go**：`initialMessage := ""` 和 `sessionID := ""`，应从 DB 和 session context 获取
**Python**：`initial_message = teammate.prompt` / `session=get_session_id()`
**修复**：从 teammate 记录和 session context 获取实际值。

---

## 提示问题（共 14 个）

### T01. VerificationAgent 描述的 fallback 语言不一致

**Python**：`VERIFICATION_AGENT_DESC.get(resolved_language, VERIFICATION_AGENT_DESC["en"])` — fallback 到 "en"
**Go**：`defaultVerificationAgentDescription["cn"]` — fallback 到 "cn"
**修复**：改为 fallback 到 "en"，对齐 Python。

---

### T02. search_via_bash 参数未实现

**Python**：`build_explore_agent_config` 和 `create_explore_agent` 有 `search_via_bash: bool = False`
**Go**：完全缺失此参数
**说明**：当前不影响功能，未来需要时补充。

---

### T03. CodingMemoryRail 仍为占位代码

**Go**：`code_agent_factory.go` 中 CodingMemoryRail 条件注入逻辑被注释掉
**Python**：完整实现
**说明**：标注为 ⤵️ 9.19-23 回填。

---

### T04. BuildBrowserAgentConfig 忽略用户传入的 settings

**Python**：`resolved_settings = _resolve_runtime_settings(model, settings)`
**Go**：`bm.ResolveRuntimeSettings(model, nil)` — 总是传 nil
**修复**：支持用户传入自定义 RuntimeSettings。

---

### T05. StreamController 日志组件应使用 ComponentAgentCore

**Go**：`scLogComponent = logger.ComponentCommon`
**规范**：agentcore 下的包应使用 `ComponentAgentCore`
**修复**：改为 `logger.ComponentAgentCore`。

---

### T06. StreamController.PendingInterruptResumes 类型为 []any

**Python**：`list[InteractiveInput]`
**Go**：`[]any`，待 9.55 回填具化
**说明**：功能不受影响，类型安全性较低。

---

### T07. MultiDimUpdater.Bind/Process 为空实现

**Go**：返回 0/空 map
**说明**：标注为 ⤵️ 待后续回填。

---

### T08. BaseOptimizerMixin.Bind 中 targets 为空时不使用 DefaultTargets

**Python**：`self._targets = list(targets or self.default_targets())`
**Go**：`m.targets = targets` — 不检查空值
**修复**：添加空值判断，targets 为空时使用 `DefaultTargets()`。

---

### T09. TextualParameter.Gradients 类型差异

**Python**：`Dict[str, Any]` — 支持 str 和 list
**Go**：`map[string]string` — 只支持字符串
**说明**：当前 InstructionOptimizer 只用字符串，不影响功能，但未来扩展受限。

---

### T10. ConversationSignalDetector.Detect 不接受 Trajectory

**Python**：统一入口接受 Trajectory 或消息列表
**Go**：只接受消息列表，需另行调用 `DetectTrajectorySignals()`
**说明**：API 不统一但功能完整。

---

### T11. Trajectory.ToMessages 不包含 Response 中的 tool_calls

**Python**：对 response 处理时包含 `tool_calls` 字段
**Go**：只检查 Response 中的 `role` 和 `content`
**修复**：补充 `tool_calls` 字段处理。

---

### T12. Trainer 检查点保存/恢复为空实现

**Go**：`ResumeIfNeeded()` 和 `SaveCheckpointIfNeeded()` 均为空方法
**说明**：标注为 ⤵️ 待 9.78 回填。

---

### T13. Trainer.Evaluate 忽略错误

**Go**：`valScore, valEvaluated, _ = t.Evaluate(ctx, agent, valCases)` — 忽略 error
**修复**：检查 error，评估失败时跳过 best_score 更新。

---

### T14. PlanAgent/ExploreAgent 的 subagents 参数未传递

**Python**：`create_plan_agent` 和 `create_explore_agent` 都接受 `subagents` 参数
**Go**：`CreatePlanAgent` 和 `CreateExploreAgent` 不接受也不传递 subagents
**说明**：当前 PlanAgent/ExploreAgent 通常不需要子代理，但签名应保持一致。

---

## ⤵️ 占位代码审计

以下 ⤵️ 标记的代码经确认**确实尚未实现**（非标记遗漏）：

| 标记位置 | 对齐章节 | 状态 |
|---------|---------|------|
| `deep_agent.go`: load_harness_config/unload_harness_config | 9.3 | ❌ 返回 error |
| `deep_agent.go`: browser/code/research/mobile_gui 工厂 | 9.3/9.25-9.31 | ❌ 返回 error（browser/code/research 已在 harness 包实现但未接入） |
| `deep_agent.go`: build_permission_interrupt_rail | 9.11 | ❌ 仅日志 |
| `deep_agent.go`: WebFreeSearchTool/WebPaidSearchTool | 9.1 | ❌ 返回 error |
| `factory.go`: SecurityRail/SkillUseRail/SubagentRail/TaskPlanningRail | 9.8-9.24 | ❌ 仅占位 |
| `harness_config/builder.go`: 工具实例化 | 9.38 | ❌ 返回 error |
| `code_agent_factory.go`: CodingMemoryRail | 9.19-9.23 | ❌ 被注释 |
| `tool_call/base.go`: Bind | 9.70 | ❌ 返回 0 |
| `trainer/trainer.go`: 轨迹提取/检查点 | 9.77/9.78 | ❌ nil/空方法 |
| `agent_configurator.go`: SetupInfra/SetupAgent | 9.57 | ❌ 大量 TODO |
| `runtime/manager.go`: Activate/Finalize/... | 9.59b | ❌ stub |
| `session_manager.go`: BindSession 步骤4-5 | 9.61 | ❌ TODO |
| `uapclaw.go`: dreaming/cloud memory hooks | 10.3.2 | ❌ ⤵️ |

---

## 问题统计

| 严重程度 | 数量 | 关键领域 |
|---------|------|---------|
| 严重 | 12 | session_history 签名/缺失功能、metadata 联动、BrowserAgent rails、ToolOptimizer bind、Trainer 轨迹/候选、agent_teams 核心 |
| 一般 | 18 | 工具顺序/参数缺失、ACP 占位、CodeAdapter ⤵️、dreaming、消息 stub、DB 空实现 |
| 提示 | 14 | fallback 语言、占位参数、类型安全、日志组件、空实现标注 |

---

## 优先修复建议

### P0（立即修复，影响核心功能正确性）

1. **S01** TruncateHistoryRecords 签名 — rewind 行为错误
2. **S02** AppendHistoryRecord 缺少 metadata 联动 — 消息计数/标题/delivery context 失效
3. **S05** BrowserAgent Rails 注入逻辑 — 用户传 rails 时核心 Rail 丢失

### P1（尽快修复，影响模块可用性）

4. **S03+S04** session_history + session_metadata 缺失功能
5. **S06+S07+S08** ToolOptimizer Bind/Trainer trajectories/候选评估 — evolving 模块不可用
6. **S09+S10+S11+S12** agent_teams 核心缺失 — 团队模式无法运行

### P2（按计划回填）

7. G01-G18 一般问题随功能模块逐步完善
8. T01-T14 提示问题在相关模块迭代时修复
