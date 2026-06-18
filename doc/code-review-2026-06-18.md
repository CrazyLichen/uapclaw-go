# Code Review — 2026-06-18

> 审查范围：24小时内提交（5.8 Checkpointer 接口+工厂、5.9 PersistenceCheckpointer、5.12 card 具体化回填、session 集成）
> 对照参考：`openjiuwen/core/session/checkpointer/` + `openjiuwen/core/session/agent.py`
> 审查日期：2026-06-18

---

## 审查概要

| 严重级别 | 数量 | 说明 |
|---------|------|------|
| 🔴 严重 | 4 | 功能缺失或逻辑错误，影响核心功能正确性 |
| 🟡 一般 | 8 | 行为与 Python 不一致或设计缺陷，需关注 |
| 🔵 提示 | 10 | 改进建议、日志缺失、代码风格等 |

---

## 🔴 严重问题

### S-01：NewSession 未将 card/checkpointer 注入 inner AgentSession，检查点机制完全失效

**Go 文件**：`session/agent.go:80`
**Python 参考**：`session/internal/agent.py:43`

**Python 行为**：
```python
self._checkpointer = CheckpointerFactory.get_checkpointer() if checkpointer is None else checkpointer
```
Python 的 `AgentSession.__init__` 默认从工厂获取 checkpointer，确保检查点机制始终生效。

**Go 实现**：
```go
s.inner = internal.NewAgentSession(s.sessionID)  // 只传 sessionID，其余全为零值
```

**影响**：
1. `s.inner.Checkpointer()` 始终返回 `nil` → PreRun/PostRun/Commit 中 `cp != nil` 检查失败，所有检查点操作被跳过
2. `s.inner.Card()` 返回 `nil` → `inner.AgentID()` 返回空字符串
3. `s.inner.Config()` 返回 `nil` → agentCheckpointerSession.Config() 返回 nil

**修复方向**：在 `NewSession` 中通过 option 注入关键字段：
```go
s.inner = internal.NewAgentSession(s.sessionID,
    internal.WithCard(s.card),
    internal.WithCheckpointer(checkpointer.GetCheckpointer()),
)
```

---

### S-02：agentCheckpointerSession.AgentID() 因 inner.card 为 nil 返回空字符串，导致状态存储键冲突

**Go 文件**：`session/agent.go:368-370`
**Python 参考**：`checkpointer/inmemory.py:166`

**Python 行为**：
```python
agent_id = session.agent_id() if hasattr(session, "agent_id") else 'Na'
```
Python 在 `agent_id` 不可用时返回 `'Na'` 作为哨兵值。

**Go 实现**：
```go
func (a *agentCheckpointerSession) AgentID() string {
    return a.inner.AgentID()  // inner.card == nil → 返回 ""
}
```

**影响**：所有没有 card 的 Agent 共享空字符串键，检查点数据互相覆盖。Python 中 `AgentStorage._get_entity_id` 使用 `agent_id` 构建 KV key（如 `agent::agent_id::session_id`），空字符串键会导致 `agent::::session_id` 畸形 key。

**修复方向**：
1. S-01 修复后 inner 有 card，此问题自然解决
2. 同时 `GetAgentID`/`GetTeamID` 在断言失败时应返回 `"Na"` 而非空字符串，对齐 Python

---

### S-03：interruptAgentExecute 的 CheckpointerSession 类型断言在当前所有 session 类型下都会失败

**Go 文件**：`interaction/base.go:199-221`

**问题描述**：`interruptAgentExecute` 将 `baseSession` 断言为 `checkpointer.CheckpointerSession`，但当前所有 session 类型（`*internal.AgentSession`、`*internal.WorkflowSession`、`*internal.NodeSession`）都不满足 `CheckpointerSession` 接口（缺少 `WorkflowID()`、`Parent()` 等方法）。断言失败后走 `!ok` 分支，静默跳过中断检查点保存。

**影响**：中断场景下的检查点保存完全失效，中断恢复后状态丢失。

**修复方向**：为交互层的 `baseSession` 提供类似 `agentCheckpointerSession` 的适配器，或扩展 `baseSession` 接口使其包含 `CheckpointerSession` 的方法。

---

### S-04：Release 方法缺失 agentID 可选参数，无法单独清除特定 Agent 的检查点

**Go 文件**：`checkpointer/base.go:34`
**Python 参考**：`checkpointer/inmemory.py:311`、`checkpointer/persistence.py:914`

**Python 行为**：
```python
# InMemory
async def release(self, session_id: str, agent_id: str = None):
    if agent_id is not None:
        agent_store = self._agent_stores.get(session_id)
        await agent_store.clear(agent_id)

# Persistence
async def release(self, session_id: str, agent_id: Optional[str] = None):
    if agent_id is not None:
        await self._agent_storage.clear(agent_id, session_id)
```

**Go 实现**：
```go
Release(ctx context.Context, sessionID string) error  // 无 agentID 参数
```

**影响**：无法精细化释放单个 Agent 的检查点资源，只能全量释放整个 session。

**修复方向**：`Release` 增加可选参数 `agentID ...string`（可变参数模拟可选），或拆为 `Release(ctx, sessionID)` 和 `ReleaseAgent(ctx, sessionID, agentID)` 两个方法。

---

## 🟡 一般问题

### G-01：GetAgentID/GetTeamID 断言失败返回空字符串而非 Python 的 "Na"

**Go 文件**：`checkpointer/base.go:141,149`
**Python 参考**：`checkpointer/inmemory.py:166,191`

Python 返回 `'Na'` 作为哨兵值（用在 KV key 构建中），Go 返回空字符串 `""`。空字符串在 KV key 中导致 `agent::::session_id` 畸形 key。

**修复**：返回 `"Na"` 对齐 Python，或在 `BuildKeyWithNamespace` 中对空值做防护。

---

### G-02：WorkflowStorage.Recover 的 inputs 处理缺失 user_inputs 逐节点恢复分支

**Go 文件**：`checkpointer/persistence.go:636-661`、`checkpointer/inmemory.go` WorkflowStorage.Recover
**Python 参考**：`checkpointer/persistence.py:333-350`

Python 的 `recover()` 有两条分支：
1. `raw_inputs is not None` → 直接 `update_and_commit_workflow_state`
2. `user_inputs` 不为空 → 逐节点创建 NodeSession，追加交互输入，最后 `commit()`

Go 只实现了第一种（map 直接更新），完全缺失 `user_inputs` 逐节点恢复逻辑。多节点交互恢复场景下数据会丢失。

---

### G-03：PreAgentExecute 使用 Update（合并）而非 Python 的 set_state（覆盖）

**Go 文件**：`checkpointer/inmemory.go:368`（PreAgentExecute）
**Python 参考**：`checkpointer/inmemory.py:188`

Python 使用 `set_state({INTERACTIVE_INPUT: [inputs]})` 完全替换状态，Go 使用 `Update()` 合并更新。两者语义不同：Python 会覆盖之前恢复的所有状态字段，Go 保留已有字段。

> 注：Python 此行为看起来像是 bug（恢复后再覆盖），Go 的 Update 可能反而更合理，但与 Python 行为不一致。

---

### G-04：agentTeamEntityHooks.GetStateToSave 逻辑与 Python 不一致

**Go 文件**：`checkpointer/persistence.go:232-240`
**Python 参考**：`checkpointer/persistence.py:318-319`

Python 的 `AgentTeamStorage._get_state_to_save` 直接调用 `session.state().get_global(None)` 获取全局状态。Go 先断言 `*state.AgentStateCollection` 调用 `asc.GetState()`，失败才回退到 `GetGlobal(AllStateKey)`。语义不同，且断言具体类型降低了灵活性。

---

### G-05：PersistenceCheckpointerProvider 严重简化，缺少 db_type/db_client/db_timeout/db_enable_wal 配置

**Go 文件**：`checkpointer/persistence.go:991-1013`
**Python 参考**：`checkpointer/persistence.py:976-1035`

Python 的 Provider 支持：
- `db_type` 切换（sqlite / shelve）
- `db_client` 注入（预配置的 AsyncEngine）
- `db_timeout` / `db_enable_wal` 配置
- shelve 后端
- SQLite WAL 模式自动启用

Go 只支持 SQLite（GORM），硬编码 `.db` 后缀，无任何配置灵活性。

---

### G-06：WorkflowStorage.Save 对 nil state/updates 的处理与 Python 不一致

**Go 文件**：`checkpointer/persistence.go:446-463`
**Python 参考**：`checkpointer/persistence.py:361-380`

Python 总是尝试序列化 state（包括 None/空 dict），仅序列化结果为 falsy 时跳过。Go 增加了 `if mainState != nil` 判断，nil 时直接跳过。这可能导致空状态不写入 dumpType/blob key，影响后续 recover 的判断。

---

### G-07：agentCheckpointerSession.Parent() 返回 nil 导致 workflow store 提前移除

**Go 文件**：`session/agent.go:356`
**Python 参考**：`checkpointer/inmemory.py:92-106`

Python 判断 `isinstance(session.parent(), AgentSession)` 来决定是否移除 workflow store。Go 用 `session.Parent() != nil && session.Parent().(AgentIDProvider)` 断言，但 `agentCheckpointerSession.Parent()` 返回 `nil`，导致永远判断为非 AgentSession，workflow store 被提前移除。

---

### G-08：PostRun 吞掉 Commit 错误

**Go 文件**：`session/agent.go:247-265`
**Python 参考**：`session/agent.py:134-136`

Go 中 `_ = s.Commit(ctx)` 忽略了 Commit 的错误。Python 中如果 `commit()` 内部 `post_agent_execute` 抛异常，异常会向上传播。

---

## 🔵 提示问题

### T-01：GetThreadID 同时在接口和包级别定义，语义矛盾

**Go 文件**：`checkpointer/base.go:14-16`（接口方法）、`base.go:117-119`（包级函数）

Python 的 `get_thread_id` 是 `@staticmethod`，不依赖实例。Go 同时在接口内和包级别定义，导致实现者必须提供转发实现，调用者不确定该用哪个。

**建议**：从 `Checkpointer` 接口中移除 `GetThreadID`，仅保留包级函数。

---

### T-02：默认序列化器从 pickle 改为 JSON，跨语言检查点不兼容

**Go 文件**：`checkpointer/serializer.go`、`inmemory.go:124`、`persistence.go:144`
**Python 参考**：`checkpointer/serde.py:33-42`

Python 默认使用 `PickleSerializer`，Go 使用 `JSONSerializer`。Python 写入的检查点数据（pickle 格式）Go 无法读取，反之亦然。

> 注：Go 不支持 pickle 是合理的架构选择，但应在文档中明确标注此兼容性限制。当前 Go 项目内 JSON↔JSON 是自洽的。

---

### T-03：LoadsTyped 对 data == nil 未处理

**Go 文件**：`checkpointer/serializer.go:53-61`
**Python 参考**：`checkpointer/serde.py:26-27`

Python 中 `if data is None: return None`，Go 无 nil 输入检查，`json.Unmarshal(nil, &result)` 返回 EOF 错误。

---

### T-04：CheckpointerFactoryConfig 缺少默认值

**Go 文件**：`checkpointer/factory.go:21-26`
**Python 参考**：`checkpointer/checkpointer.py:41-43`

Python 的 `type` 默认 `"in_memory"`，`conf` 默认空 `dict`。Go 的零值分别为 `""` 和 `nil`，传入空 struct 会导致 `"未知的检查点器类型: "` 错误。

---

### T-05：CheckpointerFactoryConfig 缺少 URL 密码脱敏的 String() 方法

**Go 文件**：`checkpointer/factory.go:21-26`
**Python 参考**：`checkpointer/checkpointer.py:45-51`

Python 的 `__repr__`/`__str__` 会调用 `_redact_url_in_value` 脱敏 conf 中的 URL 密码。Go 无 `String()` 方法，日志/调试输出可能泄露数据库连接字符串中的密码。

---

### T-06：PreWorkflowExecute 缺失 workflowID 为空的防御性检查

**Go 文件**：`checkpointer/persistence.go:852-868`
**Python 参考**：`checkpointer/persistence.py:834-841`

Python 在 force_delete 分支中检查 `workflow_id is None`，为空时记录 warning 并 return。Go 直接调用 Clear，空 workflowID 会构建无效 key。

---

### T-07：Clear 日志缺失 deleted_keys 字段

**Go 文件**：`checkpointer/persistence.go:355-361`
**Python 参考**：`checkpointer/persistence.py:267`

Python 的 clear() 日志包含 `metadata={"deleted_keys": deleted, "storage_type": "persistence"}`，Go 缺少此诊断信息。

---

### T-08：AgentStorage.Save 无条件写入空状态

**Go 文件**：`checkpointer/inmemory.go:790-805`
**Python 参考**：`checkpointer/inmemory.py:414`

Python 在 `dumps_typed` 返回 None/空值时不写入 blob，Go 无论 data 是否为空都调用 `setBlob`。

---

### T-09：innerClearWorkflowSession 中 isSucceed 硬编码为 true，导致错误日志遗漏

**Go 文件**：`checkpointer/inmemory.go:713-751`

因 graph store 未实现，`isSucceed` 硬编码为 `true`，`!isSucceed` 永远为 false，workflow store clear 失败时的 error 日志永远不会记录。8.7 回填时需修正此逻辑。

---

### T-10：缺少 CreateSerializer 工厂函数

**Go 文件**：`checkpointer/serializer.go`
**Python 参考**：`checkpointer/serde.py:45-51`

Python 有 `create_serializer(type_name)` 工厂函数，Go 各处硬编码 `NewJSONSerializer()`。如果未来要支持多种序列化器需要重构。

---

## 问题与领域章节映射

| 问题编号 | 领域章节 | 影响文件 |
|---------|---------|---------|
| S-01 | 5.8/5.12 | session/agent.go |
| S-02 | 5.8/5.12 | session/agent.go, checkpointer/base.go |
| S-03 | 5.7/5.8 | session/interaction/base.go |
| S-04 | 5.8/5.9 | checkpointer/base.go, inmemory.go, persistence.go |
| G-01 | 5.8 | checkpointer/base.go |
| G-02 | 5.8/5.9 | checkpointer/persistence.go, inmemory.go |
| G-03 | 5.8 | checkpointer/inmemory.go |
| G-04 | 5.9 | checkpointer/persistence.go |
| G-05 | 5.9 | checkpointer/persistence.go |
| G-06 | 5.9 | checkpointer/persistence.go |
| G-07 | 5.8/5.12 | session/agent.go |
| G-08 | 5.3 | session/agent.go |
| T-01~T-10 | 5.8/5.9 | checkpointer/ |

---

## 修复优先级建议

1. **立即修复**（P0）：S-01 + S-02 — NewSession 未注入 card/checkpointer 导致检查点完全失效
2. **尽快修复**（P1）：S-03 + S-04 — 中断保存失效 + Release 缺少 agentID
3. **版本内修复**（P2）：G-01~G-08 — 行为对齐问题
4. **持续改进**（P3）：T-01~T-10 — 改进建议
