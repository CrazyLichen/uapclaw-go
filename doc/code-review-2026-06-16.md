# 代码审查报告 — 2026-06-16

> 审查范围：最近 24 小时提交涉及的功能模块（State 双层接口体系重构 + 会话系统代码审查问题修复）
> 审查时间：2026-06-16
> 审查人：Code Review Agent
> Python 参考项目：`/home/opensource/agent-core/openjiuwen/` + `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`

---

## 一、审查范围

根据 git log，最近 24 小时（共 17 个提交）主要实现了以下领域和章节：

| 领域 | 章节 | 内容 | Go 代码路径 |
|------|------|------|------------|
| 领域五 | 5.1 | State 双层接口体系重构（SessionState + StateLike + 向后兼容别名） | `internal/agentcore/session/state/` |
| 领域五 | 5.2-5.4 | Session 体系消费方重构（消除类型断言 + internal 层 SessionState） | `internal/agentcore/session/agent.go`, `workflow.go`, `session.go` |
| 领域五 | 5.5 | NodeSessionFacade 委托到 SessionState | `internal/agentcore/session/node.go` |
| 领域五 | 5.6 | SessionController 多项修复（G-01/G-10/G-11/G-12/G-13/G-15） | `internal/agentcore/session/controller/` |
| 领域五 | 5.7 | Interaction 修复（G-17） | `internal/agentcore/session/interaction/` |
| 领域四 | 4.18 | SimpleMemoryIndex 修复（G-18/G-19） | `internal/agentcore/foundation/store/index/simple.go` |
| 领域五 | — | 新增 RouterSessionFacade（G-20） | `internal/agentcore/session/wrapper.go` |
| — | — | Go 编码规范合规性修复 | 多个文件 |

---

## 二、问题汇总统计

| 严重程度 | 数量 | 说明 |
|---------|------|------|
| 🔴 严重 | 10 | 功能逻辑与 Python 不一致、运行时行为差异、数据正确性风险 |
| 🟡 一般 | 16 | 行为差异、设计缺陷、遗漏功能，影响可维护性或特定场景正确性 |
| 🔵 提示 | 10 | 代码规范、向后兼容、设计建议 |

---

## 三、🔴 严重问题（10 项）

### S-01 `GetByTransformer` 传给 transformer 的参数类型与 Python 不一致

**文件**: `internal/agentcore/session/state/inmemory_state.go`，第 50-54 行

**Python 行为**: `InMemoryStateLike.get_by_transformer(transformer)` 调用 `transformer(self._state)`，传给 transformer 的是底层原始 `dict`（`self._state`）

**Go 行为**: `InMemoryStateLike.GetByTransformer(transformer)` 调用 `transformer(s)`，传给 transformer 的是 `*InMemoryStateLike` 自身（`ReadableStateLike` 接口）

**影响**: Python 的 transformer 接收 raw dict，可直接用 `dict[key]` 访问、`dict.update()` 修改。Go 的 transformer 只能通过 `Get/GetByPrefix` 等方法访问。更严重的是 Python 的 transformer 收到未深拷贝的原始引用，可直接修改内部状态；Go 的 transformer 通过接口访问会触发深拷贝。如果上游代码依赖 transformer 的写入能力，Go 版本将无法正确工作。

---

### S-02 `WorkflowCommitState.Rollback` 签名与 Python 不一致 — Python 无参使用内部 nodeID

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`，第 149-156 行

**Python 行为**: `CommitState.rollback()` 无参数，内部使用 `self._node_id` 回滚当前节点的四个子状态

**Go 行为**: `WorkflowCommitState.Rollback(nodeID string)` 需要调用方显式传入 nodeID

**影响**: 调用方必须知道当前 nodeID 并正确传递。如果调用方传错 nodeID，会回滚错误节点的状态。Python 的设计更安全（封装了 nodeID），破坏了封装性。

---

### S-03 `WorkflowCommitState.Commit` 签名与 Python 不一致 — Python 始终提交全部

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`，第 139-146 行

**Python 行为**: `CommitState.commit()` 无参数，始终调用各子状态的 `commit()`（无参），提交所有节点的暂存

**Go 行为**: `WorkflowCommitState.Commit(nodeID ...string)` 支持按节点提交，将 nodeID 透传给子状态

**影响**: Python `commit()` 语义为"提交全部"，Go `Commit("node1")` 语义为"只提交 node1"。当外部调用按节点提交时，行为与 Python 不同。

---

### S-04 `InMemoryCommitState.Commit/Rollback` 对指定节点设置 nil 而非空 slice

**文件**: `internal/agentcore/session/state/inmemory_commit_state.go`，第 128、137 行

**Python 行为**: `commit` 指定节点后 `self._updates[node_id] = []`（空列表）；`rollback` 后 `self._updates[node_id] = []`（空列表）。`get_updates()` 返回包含空列表条目的完整 dict

**Go 行为**: `commit` 指定节点后 `s.updates[id] = nil`；`rollback` 后 `s.updates[nodeID] = nil`。`GetUpdates()` 过滤了 `len(updates) == 0` 的条目，导致已 commit/rollback 的节点从 GetUpdates 结果中消失

**影响**: (1) `GetUpdates` 返回的 key 集合不同：Python 包含已操作但清空的节点，Go 不包含；(2) 序列化/反序列化场景中可能丢失已提交节点的信息；(3) Rollback 后 `GetUpdates` 不再包含该节点，可能影响依赖 updates 判断节点是否"活跃"的逻辑。

---

### S-05 `CommitStateLike.Commit` variadic 签名与 Python 单可选参数语义不同

**文件**: `internal/agentcore/session/state/state.go`，第 42-43 行

**Python 行为**: `CommitStateLike.commit(node_id: str = None)`，node_id 是单个可选字符串参数，None 表示提交全部

**Go 行为**: `Commit(nodeID ...string)`，使用 variadic 参数，不传表示提交全部，传多个表示提交多个节点

**影响**: Go 的 `Commit("node1", "node2")` 可以同时提交多个节点，Python 的 `commit()` 只能传一个 `node_id` 或不传。Go 的多节点提交是额外功能，可能导致语义混乱。此外 Go 无法表达"提交 None"（空字符串在 `UpdateByID` 中被禁止）。

---

### S-06 类型断言消除不完整 — 消费方仍有 5 处类型断言

**文件**:
- `internal/agentcore/session/agent.go`（第 266 行）— `s.inner.State().(*state.AgentStateCollection)`
- `internal/agentcore/session/internal/workflow_session.go`（第 193、232 行）— `parent.State().(*state.WorkflowCommitState)`
- `internal/agentcore/session/interaction/interaction.go`（第 66 行）— `session.State().(*state.WorkflowCommitState)`
- `internal/agentcore/session/interaction/base.go`（第 235 行）— `st.(*state.WorkflowCommitState)`

**Python 行为**: Python 通过继承链隐式支持 `commit_cmp`/`get_workflow_state`/`create_node_state` 等方法，无需类型断言

**Go 行为**: 消费方代码被迫在需要调用 `CommitCmp`、`GetWorkflowState`、`UpdateAndCommitWorkflowState`、`CreateNodeState` 时做类型断言

**影响**: 这是所有类型断言问题的根因——`SessionState` 接口不够宽泛，无法覆盖 `WorkflowCommitState` 的所有功能。类型断言失败时 Go 静默降级（Python 会抛异常），可能掩盖配置错误。**严重场景**：`agent.go` 中 `AgentStateCollection` 断言失败时 globalState 不共享，WorkflowSession 和 AgentSession 之间全局状态不互通。

---

### S-07 `GlobalSessionController.FlushAgent/FlushSession` 释放全局锁后再调用子操作 — 竞态风险

**文件**: `internal/agentcore/session/controller/global_controller.go`，第 114-127、130-140 行

**Python 行为**: Python 在全局锁内完成所有操作（asyncio 协作式调度，不存在竞态）

**Go 行为**: Go 先在 `g.mu.Lock()` 内查找，然后 `g.mu.Unlock()` 释放全局锁后才调用 `controller.Flush()`/`controller.FlushSession()`

**影响**: 释放全局锁后、调用 Flush 前，其他 goroutine 可能通过 `RemoveSession` 删除了目标 session，导致 Flush 目标已不存在。虽然 `FlushSession` 内部有"不存在则返回 nil"的保护，但可能导致数据不一致（session 被删除后未 flush）。

---

### S-08 `WithWorkflowSessionParent` 忽略了 parent — envs 始终为空

**文件**: `internal/agentcore/session/workflow.go`，第 53-59 行

**Python 行为**: Python `WorkflowSession.__init__()` 中，当 parent 不为 None 时，envs 直接从 `parent.config().get_envs()` 获取

**Go 行为**: `WithWorkflowSessionParent` 直接 `_ = parent` 忽略了 parent 参数，导致 `GetEnvs()` 始终返回空 map，无法继承父会话的环境变量

**影响**: WorkflowSession 的 `GetEnvs()` 始终返回空 map，组件开发者通过 WorkflowSession 获取不到任何配置信息。此问题在上一期审查报告（G-04）中已标记，但本次仍未修复。

---

### S-09 无 parent 时 WorkflowSession 的 config 为 nil

**文件**: `internal/agentcore/session/internal/workflow_session.go`，第 119-149 行

**Python 行为**: Python 无 parent 时创建 `Config()` 默认实例

**Go 行为**: Go 无 parent 时 config 保持 nil

**影响**: 无 parent 的 WorkflowSession 的 `Config()` 返回 nil。调用方如果尝试在无 parent 的 WorkflowSession 上调用 config 方法会 panic。此问题在上一期审查报告（G-05）中已标记，但本次仍未修复。

---

### S-10 link 文件中 `field_scopes` 序列化格式不一致

**文件**: `internal/agentcore/session/controller/chain_session.go`，第 276-280 行

**Python 行为**: Python `ChainSession.flush()` 写入 `'field_scopes': list(policy.field_scopes) if policy.field_scopes else None`。当 `field_scopes` 为 None（默认值）时写入 JSON `null`

**Go 行为**: Go `ChainSession.Flush()` 总是创建非 nil 切片 `fieldScopes := make([]string, 0, len(policy.FieldScopes))`，如果 `policy.FieldScopes` 为 nil，写入空数组 `[]` 而非 `null`

**影响**: 序列化格式差异（`null` vs `[]`），可能导致跨语言互操作问题。虽然 Go 侧数据往返一致（读回时 `len==0` 跳过，FieldScopes 为 nil），但与 Python 的磁盘格式不兼容。

---

## 四、🟡 一般问题（16 项）

### G-01 `SetUpdates` 对 global_updates 的 workflowOnly 检查比 Python 更严格

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`

**Python 行为**: `CommitState.set_updates()` 中 `global_updates` 不做 `workflow_only` 检查，只要 `global_updates is not None` 就设置

**Go 行为**: `SetUpdates()` 额外加了 `s.workflowOnly` 条件判断，只有 `workflowOnly=true` 时才设置 `globalState`

**影响**: Python 的 `CommitState.set_updates` 无论 `workflow_only` 都会设置 `global_updates`，Go 侧更严格，可能阻止恢复检查点时 `global_updates` 数据写入。

---

### G-02 `AgentStateCollection.GetGlobal` 零值 key 语义与 Python `key=None` 不同

**文件**: `internal/agentcore/session/state/agent_state_collection.go`，第 41-48 行

**Python 行为**: `StateCollection.get_global(key=None)` 用 `key is None` 判断是否返回完整全局状态快照

**Go 行为**: `AgentStateCollection.GetGlobal(key)` 用 `key.IsZero()` 判断

**影响**: `StateKey` 的零值包括 `StringKey("")` 和未初始化的 `StateKey{}`。如果调用方传入 `StringKey("")` 表示"空字符串路径"而非"获取全部"，Go 会错误地返回完整快照。Python 中 `key=""` 不是 `None`，不会触发获取全部的逻辑。

---

### G-03 `WorkflowCommitState.SetState` 对 nil 子状态值处理与 Python 不同

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`，第 288-302 行

**Python 行为**: 无条件调用 `self._io_state.set_state(state.get(IO_STATE_KEY))`，即使 `state.get()` 返回 None，底层 `InMemoryStateLike.set_state` 有 `if state:` 保护

**Go 行为**: 使用 `if io, ok := st[IOStateKey]; ok { if m, ok := io.(map[string]any); ok { ... } }`，只当键存在且类型为 `map[string]any` 时才调用 SetState

**影响**: 如果 `st[IOStateKey]` 的值不是 `map[string]any` 类型（例如 nil 或其他类型），Python 仍会调用 `set_state`（底层保护生效），Go 会跳过。对于 JSON 反序列化场景影响较小，但手动构造的 map 可能存在差异。

---

### G-04 `WorkflowCommitState.SetState` 未检查 workflowOnly 条件就设置 globalState

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`，第 283-287 行

**Python 行为**: `CommitState.set_state(state)` 中 global_state 不检查 `workflow_only`，只要存在就设置

**Go 行为**: `SetState` 对 globalState 也不检查 `workflowOnly`，与 Python 一致

**影响**: 虽然 Python 也不检查，但从语义上当 `workflowOnly=false` 时 globalState 由外部管理，SetState 不应覆盖它。`GetState` 方法中 `workflowOnly=false` 时返回 `GlobalStateKey: nil`，但 `SetState` 仍然会设置 `globalState`，可能造成数据不一致。

---

### G-05 `InMemoryCommitState.GetUpdates` 返回深拷贝，Python 返回原始引用

**文件**: `internal/agentcore/session/state/inmemory_commit_state.go`，第 141-155 行

**Python 行为**: `InMemoryCommitState.get_updates()` 返回 `self._updates`（原始引用，无深拷贝）

**Go 行为**: `GetUpdates()` 对每个 update 做了深拷贝

**影响**: Go 版本更安全但性能略低。如果 Python 上游代码有通过 `get_updates()` 返回值直接修改 updates 的模式，Go 版本不会反映这些修改。对于序列化/反序列化场景无影响。

---

### G-06 `InMemoryCommitState.SetUpdates` 未深拷贝传入数据

**文件**: `internal/agentcore/session/state/inmemory_commit_state.go`，第 158-164 行

**Python 行为**: `set_updates(updates)` 中 `if updates: self._updates = updates`，直接引用赋值

**Go 行为**: `SetUpdates(updates)` 中 `if updates != nil { s.updates = updates }`，也是直接引用赋值

**影响**: 与 Python 行为一致（都不深拷贝），但与 `GetUpdates`（返回深拷贝）的防御性策略不对称。调用方保留对内部 updates 的引用可能造成数据竞争。

---

### G-07 `ChainSession.Load` 中 data 为 null 时行为与 Python 不同

**文件**: `internal/agentcore/session/controller/chain_session.go`，第 107-119 行

**Python 行为**: `'data' in state_data` 时就调用 `self.data_container.load(agent_id, session_id, None)`

**Go 行为**: `if dataRaw, ok := stateData["data"]; ok && dataRaw != nil`，data 为 null 时跳过

**影响**: 如果磁盘上 data 字段为 null，Python 会传入 None 给 load（由底层保护），Go 则保留原容器不替换。

---

### G-08 `LoadAgentSessionContainer` 中 PreRun 失败不阻止容器创建

**文件**: `internal/agentcore/session/controller/data_container.go`，第 135-151 行

**Python 行为**: `await agent_session.pre_run()` 失败会抛异常导致 load 失败

**Go 行为**: `sa.PreRun(context.Background())` 失败只记录错误，不阻止容器创建

**影响**: 可能导致后续使用时数据不一致（session 没有完整初始化）。

---

### G-09 Flush 持锁模式与 Python 不同 — Go 串行 Flush

**文件**: `internal/agentcore/session/controller/session_controller.go`

**Python 行为**: Python 通过 `asyncio.gather` 并发执行所有 flush_tasks

**Go 行为**: Go 在 `SessionController.Flush()` 中遍历所有 session 串行执行 Flush

**影响**: 如果某个 session 的 Flush 耗时很长（如磁盘 IO 慢），所有 session 的 Flush 和控制器的其他操作都会被阻塞。Go 应考虑使用 goroutine 并发 Flush。

---

### G-10 `Search/ListMemories Decode` 失败时 Go 跳过而非中断 — 与 Python 行为不一致

**文件**: `internal/agentcore/foundation/store/index/simple.go`，第 306-316 行

**Python 行为**: decode 失败异常会传播导致整个 search 方法中断

**Go 行为**: codec.Decode 失败时记录 Warn 日志并 continue 跳过该条记录

**影响**: Go 的行为比 Python 更健壮（跳过而非中断），但是是有意偏差，应明确文档化。

---

### G-11 `RouterSessionFacade` 缺少 Python RouterSession 的 `base()` 方法

**文件**: `internal/agentcore/session/wrapper.go`

**Python 行为**: `RouterSession` 覆写了 `base()` 方法返回 None，是一种安全壳设计

**Go 行为**: `RouterSessionFacade` 没有提供 `Base()` 方法或等效功能

**影响**: Python `RouterSession.base()` 返回 None 阻止路由函数通过 `base()` 访问底层 session。Go 天然阻止了这种访问（方法不存在），功能等价但接口不完整。

---

### G-12 `WorkflowInteraction` 构造时 nodeID 延迟获取 — 与 Python 不同

**文件**: `internal/agentcore/session/interaction/interaction.go`，第 63-79 行

**Python 行为**: `WorkflowInteraction.__init__()` 中直接 `self._node_id = session.executable_id()`，构造时缓存 nodeID

**Go 行为**: `NewWorkflowInteraction()` 不缓存 nodeID，每次调用 `WaitUserInputs` 时通过 `getExecutableID` 延迟获取

**影响**: 功能等价但设计风格不同。延迟绑定更灵活，但增加了每次调用的类型断言开销。

---

### G-13 `NodeSessionFacade.Interact()` 缺少 `trace_component_interactive_inputs` 追踪调用点

**文件**: `internal/agentcore/session/node.go`，第 176-187 行

**Python 行为**: Python 在 `interact()` 返回用户输入后调用 `TracerWorkflowUtils.trace_component_interactive_inputs`

**Go 行为**: 完全缺失此追踪逻辑，且没有预留调用点

**影响**: 此问题在上一期审查报告（G-06）中已标记，但本次仍未修复。后续容易遗忘。

---

### G-14 `SubWorkflowSession` 字段名带 "2" 后缀不够优雅

**文件**: `internal/agentcore/session/internal/workflow_session.go`，第 219-256 行

**Python 行为**: `SubWorkflowSession` 继承 `NodeSession`，字段自然覆盖

**Go 行为**: 使用 `workflowNestingDepth2/workflowID2/mainWorkflowID2` 字段名（带2后缀）来"覆盖"嵌入的 NodeSession 的字段

**影响**: 方法覆写可以工作，但字段名容易混淆，增加维护成本。

---

### G-15 `InMemoryCommitState` 不应满足 SessionState 接口

**文件**: `internal/agentcore/session/state/inmemory_commit_state.go`，第 167-177 行

**Python 行为**: `InMemoryCommitState` 继承 `CommitStateLike`，不实现 State 接口

**Go 行为**: `InMemoryCommitState` 实现了 `GetGlobal/UpdateGlobal/UpdateTrace/Dump` 方法，使其也满足 `SessionState` 接口

**影响**: 语义不清晰——`InMemoryCommitState` 作为单存储单元，不应是 `SessionState` 的实现。

---

### G-16 `rootToIndex` 对 nil 中间节点的处理比 Python 更宽松

**文件**: `internal/agentcore/session/state/utils.go`

**Python 行为**: `root_to_index` 在中间索引处理完成后，对当前元素做类型检查 `if current is not None and not isinstance(current, (list, tuple)): return None, None`，确保中间节点必须是 list/tuple

**Go 行为**: `rootToIndex` 在 nil 情况下在 `createIfAbsent` 时自动创建空 `[]any`

**影响**: Go 的行为更宽松——Python 对 nil 中间节点不做 `createIfAbsent`（只有超过 len 时才扩展），Go 对 nil 中间节点在 `createIfAbsent=true` 时自动创建。差异可能导致 Go 允许某些 Python 拒绝的数据结构。

---

## 五、🔵 提示问题（10 项）

### T-01 `InMemoryStateLike` 的向后兼容别名可能导致混淆

**文件**: `internal/agentcore/session/state/inmemory_state.go`，第 19、24 行

**说明**: `type InMemoryState = InMemoryStateLike` 和 `var NewInMemoryState = NewInMemoryStateLike` 保持向后兼容。别名增加了维护负担，建议设定移除时间线。

---

### T-02 向后兼容别名 `State/CommitState/ReadableState/RecoverableState` 可能导致新代码继续使用旧名称

**文件**: `internal/agentcore/session/state/state.go`，第 77-80 行

**说明**: 别名让旧代码无需修改，但新代码可能误用旧名称。建议添加 lint 规则或在注释中更明确地标注废弃。

---

### T-03 `AgentStateCollection.Dump` 返回硬编码 `"trace_state"` 字符串键

**文件**: `internal/agentcore/session/state/agent_state_collection.go`，第 80-85 行

**说明**: 其他字段使用 `GlobalStateKey/AgentStateKey` 常量，但 `trace_state` 使用硬编码字符串。建议定义 `TraceStateKey` 常量。

---

### T-04 `WorkflowStateCollection.Dump` 同样使用硬编码 `"trace_state"` 键

**文件**: `internal/agentcore/session/state/workflow_state_collection.go`，第 110-121 行

**说明**: 同 T-03，建议定义 `TraceStateKey` 常量。

---

### T-05 `AgentStateCollection.UpdateTrace` 空实现但 `Dump` 输出 traceState

**文件**: `internal/agentcore/session/state/agent_state_collection.go`，第 58-59 行

**说明**: 与 Python 行为一致（Python 的 `update_trace` 也是 `pass`），但 `Dump` 中输出的 `trace_state` 始终为空 map。

---

### T-06 `CreateNodeState` 硬编码 `workflowOnly=true`

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`，第 167-185 行

**说明**: 与 Python 行为一致（Python 默认也是 True），但如果未来需要创建非 workflowOnly 的状态，需要修改硬编码。

---

### T-07 `sessionCreator` 包级变量无锁保护

**文件**: `internal/agentcore/session/controller/data_container.go`，第 102-126 行

**说明**: `sessionCreator` 是包级变量，没有锁保护。由于 `RegisterSessionCreator` 只在 `init()` 中调用一次，实际风险很低。

---

### T-08 `FlushScope` 无快速返回检查

**文件**: `internal/agentcore/session/controller/session_controller.go`，第 111-125 行

**说明**: Python 在 `flush_scope()` 中先检查 `if session_scope not in self.meta_map: return True`，不存在时快速返回。Go 遍历所有 SessionCache 即使该 scope 不存在，总是执行 `writeMetaFile()`。

---

### T-09 `WorkflowSession.Close`/`SubWorkflowSession.Close` 为空实现

**文件**: `internal/agentcore/session/internal/workflow_session.go`，第 301-303、470-472 行

**说明**: Python 在 `close()` 时调用 `actor_manager.shutdown()`，Go 为空实现。已知待回填项。

---

### T-10 `AgentSpan` 在 Go 侧始终为 nil

**文件**: `internal/agentcore/session/internal/agent_session.go`

**说明**: Python 在 `AgentSession.__init__()` 中自动创建 Tracer 和 AgentSpan，Go 需要外部手动注入（通过 `WithAgentSpan` 选项）。追踪功能不完整。

---

## 六、修复优先级建议

### 最高优先级（影响正确性）

| 问题 | 说明 | 建议 |
|------|------|------|
| S-06 | 类型断言消除不完整 | 扩展 `SessionState` 接口，增加 `CommitCmp/GetWorkflowState/UpdateAndCommitWorkflowState/CreateNodeState` 等方法（AgentStateCollection 返回空实现），彻底消除类型断言 |
| S-04 | Commit/Rollback 设置 nil vs 空 slice | 改为 `s.updates[id] = make([]map[string]any, 0)` 对齐 Python `[]`，并调整 `GetUpdates` 不过滤空 slice |
| S-02 | Rollback 签名与 Python 不一致 | 考虑增加无参 `RollbackAll()` 方法，或让 `Rollback()` 无参使用内部 nodeID |
| S-03 | Commit 签名与 Python 不一致 | 同 S-02，提供对齐 Python 的无参全量提交方法 |

### 高优先级（数据一致性和竞态安全）

| 问题 | 说明 | 建议 |
|------|------|------|
| S-07 | GlobalSessionController 竞态风险 | 在全局锁内完成 Flush 调用，或使用引用计数保护 session 生命周期 |
| S-08 | WithWorkflowSessionParent 忽略 parent | 从 parent 获取 envs |
| S-10 | field_scopes 序列化格式不一致 | nil 时写入 null 而非 `[]`，对齐 Python |
| S-01 | GetByTransformer 参数类型差异 | 定义 `TransformerFunc` 传 raw map，或提供两种 transformer 接口 |

### 中优先级（功能完整性和一致性）

| 问题 | 说明 |
|------|------|
| G-01 | SetUpdates 对 global_updates 的 workflowOnly 检查 |
| G-02 | GetGlobal 零值 key 语义差异 |
| G-03 | SetState 对 nil 子状态值处理 |
| G-07 | Load 中 data 为 null 处理 |
| G-08 | PreRun 失败不阻止容器创建 |
| G-09 | Flush 串行化性能问题 |
| G-13 | Interact 缺少追踪调用点 |

### 低优先级（规范和优化）

| 问题 | 说明 |
|------|------|
| T-01~T-10 | 向后兼容别名、硬编码字符串、空实现、设计建议等 |

---

## 七、各模块总体评价

### 5.1 State 双层接口体系

**功能符合度**: 中。核心接口体系（SessionState/StateLike/CommitStateLike + 向后兼容别名）已建立，但 `SessionState` 接口不够宽泛导致消费方仍有类型断言。`GetByTransformer` 参数类型差异（S-01）、`Commit/Rollback` 签名差异（S-02/S-03/S-05）、nil vs 空 slice（S-04）是主要不符合项。

**主要风险**: SessionState 接口设计不完整、Commit/Rollback 语义与 Python 不一致、GetByTransformer 行为差异。

### 5.2-5.4 Session 体系消费方重构

**功能符合度**: 中。类型断言部分消除，但核心功能点（WorkflowCommitState 的特有方法）仍需断言。`WithWorkflowSessionParent` 忽略 parent（S-08）、无 parent 时 config 为 nil（S-09）仍未修复。

**主要风险**: 类型断言失败静默降级可能掩盖错误、parent 继承链不完整。

### 5.6 SessionController

**功能符合度**: 中偏高。G-11（Flush 持锁全程）、G-12（数据容器恢复）、G-13（loadSession 返回 error）、G-15（link 文件错误处理）的修复基本对齐 Python。但 GlobalController 竞态风险（S-07）、Flush 串行化（G-09）是新的问题。

**主要风险**: GlobalController 竞态条件、Flush 性能。

### 5.7 Interaction

**功能符合度**: 中偏高。G-17 的修复（从 GetGlobal/UpdateGlobal 改为 Get/Update）对齐了 Python 的 `state().get()` 路径。但类型断言（S-06 的 interaction 部分）和缺少追踪调用点（G-13）仍未解决。

### 新增 RouterSessionFacade（G-20）

**功能符合度**: 高。核心设计对齐 Python 的 RouterSession 安全壳——禁用写操作保留读操作。缺少 `base()` 方法（G-11）不影响功能。WriteStream/WriteCustomStream/GetEnv/GetNodeConfig 返回 nil 对齐 Python 的 `pass` 行为。

---

## 八、与上期审查报告（2026-06-15）对比

| 上期问题 | 上期等级 | 本期状态 |
|---------|---------|---------|
| S-01 getBySchema 缺少 isRoot/isRefPath | 🔴 | ✅ 已修复（通过 safeExtendContainer/rootToIndex） |
| S-02 rootToPath 不支持 list 索引 | 🔴 | ✅ 已修复 |
| S-03 getValueByNestedPath 不支持负数索引 | 🔴 | ✅ 已修复 |
| S-04 状态结构体无并发保护 | 🔴 | ✅ 已修复（各结构体已加 sync.RWMutex） |
| S-05 UpdateByID 空字符串静默忽略 | 🔴 | ✅ 已修复（改为返回 error） |
| S-06 SessionScope map key 不可靠 | 🔴 | ⚠️ 待确认 |
| S-07 Interrupt.Value 类型不安全 | 🔴 | ⚠️ 待确认 |
| S-08 WithSessionID 重复创建实例 | 🔴 | ⚠️ 待确认 |
| G-04 WithWorkflowSessionParent 忽略 parent | 🟡 | ❌ 未修复（本期 S-08 重新标记为严重） |
| G-06 Interact 缺少追踪调用点 | 🟡 | ❌ 未修复（本期 G-13） |
| G-07 类型断言失败返回 (nil, nil) | 🟡 | ✅ 部分修复（SessionState 接口消除部分断言，但核心断言仍存在） |

**结论**: 上期 8 个严重问题中 4 个已确认修复，3 个待确认，1 个（G-04）未修复且升级为严重。本期新增 6 个严重问题（S-01~S-06），主要集中在 State 双层接口体系的设计差异和类型断言消除不完整。
