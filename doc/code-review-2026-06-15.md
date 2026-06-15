# 代码审查报告 — 2026-06-15

> 审查范围：最近 24 小时提交涉及的功能模块
> 审查时间：2026-06-15
> 修复时间：2026-06-15
> Python 参考项目：`/home/opensource/agent-core/openjiuwen/` + `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`
> 严重问题状态：✅ 全部已修复
> 一般问题状态：🟡 部分已修复（G-01/G-02/G-03/G-07/G-09/G-10/G-11/G-12/G-13/G-15/G-16/G-17/G-18/G-19/G-20/G-21/G-22 已修复；G-04/G-05 等待 5.12 SessionConfig；G-08 已文档化；G-14 等待 5.13+ Callback 框架）

---

## 一、审查范围

根据 git log，最近 24 小时主要实现了以下领域和章节：

| 领域 | 章节 | 内容 | Go 代码路径 |
|------|------|------|------------|
| 领域四 | 4.29 | StorageCodec 存储编解码协议 | `internal/agentcore/foundation/store/index/`, `internal/agentcore/memory/codec/` |
| 领域五 | 5.1 | State 体系 | `internal/agentcore/session/state/` |
| 领域五 | 5.2 | BaseSession 接口 | `internal/agentcore/session/session.go` |
| 领域五 | 5.3 | AgentSession | `internal/agentcore/session/agent.go`, `internal/agentcore/session/internal/` |
| 领域五 | 5.4 | WorkflowSession | `internal/agentcore/session/workflow.go` |
| 领域五 | 5.5 | SessionNode / NodeSessionFacade | `internal/agentcore/session/node.go` |
| 领域五 | 5.6 | SessionController | `internal/agentcore/session/controller/` |
| 领域五 | 5.7 | Interaction | `internal/agentcore/session/interaction/` |

---

## 二、问题汇总统计

| 严重程度 | 数量 | 说明 |
|---------|------|------|
| 🔴 严重 | 8 | 功能逻辑错误，与 Python 行为严重不一致，可能导致运行时 panic 或数据错误 |
| 🟡 一般 | 22 | 行为差异、设计不当、遗漏功能，影响可维护性或特定场景正确性 |
| 🔵 提示 | 18 | 代码规范、日志缺失、性能优化建议、测试覆盖不足 |

---

## 三、🔴 严重问题（8 项）

### S-01 `getBySchema` 缺少 `isRoot`/`isRefPath` 逻辑 — schema 查询结果错误

**文件**: `internal/agentcore/session/state/utils.go`，第 130-152 行、第 310-326 行

**问题**: Python 的 `get_by_schema` 有 `is_root` 参数和 `is_ref_path` 检查。在非根层，如果 schema 值是普通字符串（非 `${...}` 引用路径），Python 直接保留该字符串作为默认值。Go 缺少此逻辑，将所有字符串都当路径引用处理，导致包含默认值的 schema 查询返回 `nil` 而非原始字符串。

**Python 行为**:
```python
if is_ref_path(target_schema):
    result[target_key] = get_by_schema(target_schema, data, is_root=False)
else:
    result[target_key] = target_schema  # 普通字符串 → 原样保留
```

**Go 行为**:
```go
case string:
    result[targetKey] = getBySchema(StringKey(s), data)  // 始终当路径解析
```

**影响**: 所有包含非引用路径字符串默认值的 schema 查询都会返回错误结果。

---

### S-02 `rootToPath` 不支持 list 索引路径 — 数组索引更新全部失效

**文件**: `internal/agentcore/session/state/utils.go`，第 236-241 行

**问题**: Python 的 `root_to_path` 完整支持 list 索引路径导航（如 `items[0].name`），包括自动扩展列表。Go 对 `case int:` 直接返回 `nil, nil`，导致所有包含数组索引的嵌套路径更新操作静默失败。

```go
case int:
    // list 索引在 updateDict 场景下不常见，返回 nil
    return nil, nil
```

**影响**: `updateDict(map[string]any{"items[0].name": "new"}, source)` 等含数组索引的更新操作完全不生效。

---

### S-03 `getValueByNestedPath` 不支持负数索引 — 功能缺失

**文件**: `internal/agentcore/session/state/utils.go`，第 179-181 行

**问题**: `parseListIndexes` 能解析负数索引（`a[-1]` → `["a", -1]`），但 `getValueByNestedPath` 中 `p < 0` 被直接拒绝返回 `nil`。Python 支持负数索引返回倒数第 N 个元素。

```go
case int:
    if !ok || p < 0 || p >= len(list) {  // p < 0 直接返回 nil
        return nil
    }
```

**影响**: 所有使用负数索引的路径查询静默返回空值。

---

### S-04 所有状态结构体均无并发保护 — 可能导致运行时 panic

**文件**: 多个文件

**涉及结构体**: `InMemoryState`、`InMemoryCommitState`、`AgentStateCollection`、`WorkflowStateCollection`、`WorkflowCommitState`、`Session.preRunDone/postRunDone`

**问题**: 所有内部使用 `map[string]any` 存储状态的结构体都没有互斥锁保护。Python 因 GIL 和 async/await 单线程协作模型不存在并发安全问题。Go 中 map 的并发读写会导致 **panic**。

**影响**: 工作流场景中节点并行执行时，共享底层状态的并发读写将导致 data race 和 panic。

---

### S-05 `InMemoryCommitState.UpdateByID` 空字符串 nodeID 静默忽略

**文件**: `internal/agentcore/session/state/inmemory_commit_state.go`，第 71-76 行

**问题**: Python 中 `node_id is None` 时抛出异常 `build_error(StatusCode.ERROR)`，Go 中 `nodeID == ""` 时静默 `return`。静默忽略可能导致调用方不知道更新被丢弃，这是危险的。此外 Python 允许空字符串 `""`（仅禁止 `None`），Go 禁止空字符串，语义不一致。

```go
func (s *InMemoryCommitState) UpdateByID(nodeID string, data map[string]any) {
    if nodeID == "" {
        return  // 静默忽略
    }
```

---

### S-06 `SessionScope` 作为 map key 缺少可靠的相等语义 — 可能导致 MetaMap 查找失败

**文件**: `internal/agentcore/session/controller/scope.go`

**问题**: Python 的 `SessionScope` 是 `@dataclass(frozen=True)` 自带 `__eq__` 和 `__hash__`。Go 的 `SessionScope` 包含 `Scope` 和 `Subject` 两个接口字段，作为 map key 时依赖 Go 原生接口比较，可能因底层具体类型不同但 `String()` 相同而查找失败。`FlushScope` 中已经用 `String()` 比较说明开发者意识到直接比较不可靠，但 `MetaMap` 的 key 查找仍用 Go 原生比较。

**影响**: 从 JSON 反序列化恢复的 `SessionScope` 可能无法在 `MetaMap` 中匹配原始 key。

---

### S-07 `Interrupt.Value` 使用 `map[string]any` 而非结构体 — 类型安全丢失

**文件**: `internal/agentcore/session/interaction/interaction.go`，第 112-118 行、第 126-153 行

**问题**: Python 的 `Interrupt.Value` 是 `OutputSchema` 实例（Pydantic model，有 `type`/`index`/`payload` 字段），Go 使用 `map[string]any`。丢失类型安全性，下游消费者无法通过类型断言获取结构化数据。Python 写流 payload 是元组 `(node_id, value)`，Go 是 `InteractionOutput` 结构体指针，序列化格式不一致。

```go
PanicGraphInterrupt(Interrupt{
    Value: map[string]any{
        "type":    InteractionType,
        "index":   w.idx,
        "payload": payload,
    },
})
```

---

### S-08 `WithSessionID` 创建重复的内部 AgentSession 实例 — 资源浪费和潜在问题

**文件**: `internal/agentcore/session/agent.go`，第 72-77 行

**问题**: `NewSession()` 先在第 62 行创建了 `internal.NewAgentSession(sessionID)`，然后 `WithSessionID` 又创建了一个新的 `internal.NewAgentSession(id)` 覆盖前者。不仅造成不必要的对象创建，如果未来 AgentSession 构造中有副作用（如启动 goroutine、注册回调等），将导致泄漏。

```go
func WithSessionID(id string) SessionOption {
    return func(s *Session) {
        s.inner = internal.NewAgentSession(id)  // 覆盖前一个实例
    }
}
```

---

## 四、🟡 一般问题（22 项）

### G-01 `WorkflowCommitState.SetUpdates` JSON 序列化后类型断言失败

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`，第 191-215 行

**问题**: `SetUpdates` 使用 `gs.(map[string][]map[string]any)` 类型断言。经过 JSON 序列化/反序列化后，`[]map[string]any` 变为 `[]any`，导致断言永远失败。Python 不受此影响因为是动态类型。

---

### G-02 `AgentStateCollection` 缺少 `traceState` 和 `UpdateTrace` 方法

**文件**: `internal/agentcore/session/state/agent_state_collection.go`

**问题**: Python 的 `StateCollection` 有 `_trace_state` 字段、`update_trace` 方法和 `dump` 包含 `trace_state`。Go 完全缺失，`Dump` 结果与 Python 不兼容。

---

### G-03 `State` 接口方法缺失 — `GetGlobal`/`UpdateGlobal`/`UpdateTrace`/`Dump` 不在接口中

**文件**: `internal/agentcore/session/state/state.go`

**问题**: Python 的 `State(RecoverableStateLike)` 接口包含 `get_global`、`update_global`、`update_trace`、`dump` 方法。Go 的 `State` 接口只包含 `ReadableState` + `RecoverableState` + `Update` + `GetByTransformer`，这些方法直接在具体结构体上实现，无法多态调用。

---

### G-04 `WithWorkflowSessionParent` 忽略了 parent — envs 始终为空

**文件**: `internal/agentcore/session/workflow.go`，第 53-59 行

**问题**: `WithWorkflowSessionParent` 直接 `_ = parent` 忽略了 parent 参数，导致 `GetEnvs()` 始终返回空 map，无法继承父会话的环境变量。

```go
func WithWorkflowSessionParent(parent BaseSession) WorkflowSessionOption {
    return func(ws *WorkflowSession) {
        var envs map[string]any
        _ = parent  // 直接忽略了 parent！
```

---

### G-05 无 parent 时 `WorkflowSession` 的 config 为 nil

**文件**: `internal/agentcore/session/internal/workflow_session.go`，第 119-149 行

**问题**: Python 无 parent 时创建 `Config()` 默认实例，Go 无 parent 时 config 保持 nil。导致无 parent 的 WorkflowSession 的 `Config()` 返回 nil。

---

### G-06 `Interact()` 缺少 `trace_component_interactive_inputs` 调用点

**文件**: `internal/agentcore/session/node.go`，第 177-186 行

**问题**: Python 在 `interact()` 返回用户输入后调用 `TracerWorkflowUtils.trace_component_interactive_inputs`。Go 完全缺失此追踪逻辑，且没有预留调用点（即使当前是桩实现），后续容易遗忘。

---

### G-07 `GetState`/`GetGlobalState` 类型断言失败时返回 `(nil, nil)` 歧义

**文件**: `internal/agentcore/session/node.go`，第 111-116 行、第 128-133 行

**问题**: 当 `inner.State()` 不是 `*WorkflowCommitState` 时，返回 `(nil, nil)`，调用方无法区分 "值为 nil" 和 "类型不匹配"。同样问题出现在 `Session.GetState` 等方法中。

---

### G-08 `WorkflowCommitState.GetUpdates` 返回类型 `map[string]any` 与 `CommitState` 接口的 `map[string][]map[string]any` 不一致

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`，第 176-188 行

**问题**: 虽然不直接实现 `CommitState` 接口不产生编译错误，但类型不一致导致使用混淆。

---

### G-09 `WorkflowCommitState.GetState`/`SetState` 覆写通过嵌入实现的潜在问题

**文件**: `internal/agentcore/session/state/workflow_commit_state.go`，第 226-265 行

**问题**: 通过 `WorkflowStateCollection` 类型变量调用 `GetState`/`SetState` 会调用基类方法而非覆写版本，与 Python 继承自然覆写的行为不同。

---

### G-10 Python `_safe_extend_container` 和 `root_to_index` 未实现

**文件**: 对应 Python `/home/opensource/agent-core/openjiuwen/core/session/utils.py` 第 269-385 行

**问题**: Python 有完整的安全扩展容器、负数索引调整、嵌套深度限制等功能。Go 缺失，且 `rootToPath` 对 list 索引直接返回 `nil`。

---

### G-11 `SessionController.Flush()` 锁粒度问题 — 释放锁期间可能数据不一致

**文件**: `internal/agentcore/session/controller/session_controller.go`，第 66-92 行

**问题**: Python 整个 flush 过程持锁，Go 在中间释放了锁。释放锁期间其他操作（如 CreateIfNotExists）可能修改 SessionCache，导致 flush 数据与最终写入 meta 的数据不一致。

---

### G-12 `ChainSession.Load()` 数据容器恢复被跳过

**文件**: `internal/agentcore/session/controller/chain_session.go`，第 103 行

**问题**: Python 在 `state_data['data']` 存在时调用 `self.data_container.load()` 恢复数据容器内容。Go 完全跳过了这一步，从磁盘加载的会话无法恢复数据容器状态。

---

### G-13 `SessionController.loadSession()` 错误被吞掉

**文件**: `internal/agentcore/session/controller/session_controller.go`，第 675-717 行

**问题**: 失败时只记日志但不返回 error，调用者无法感知加载失败。`GetScopeActiveSession` 等方法在 `loadSession` 失败后可能返回不完整的 session 或 nil。

---

### G-14 缺少 P2P 和 PubSub 回调注册 — 跨 Agent 调用链不可见

**文件**: `internal/agentcore/session/controller/global_controller.go`，第 616-618 行

**问题**: Python 注册了 `AGENT_P2P_RECEIVED` 和 `AGENT_PUBSUB_RECEIVED` 回调，Go 只注册了 `AGENT_SESSION_CREATED`。缺失导致跨 Agent 调用链不可见。

---

### G-15 `ChainSession.Flush()` 写入 link 文件时忽略错误

**文件**: `internal/agentcore/session/controller/chain_session.go`，第 267-268 行

**问题**: 序列化和写入文件都忽略了错误。Python 在 flush 中写文件失败会返回 False，Go 忽略错误，行为不一致。

```go
linkBytes, _ := json.MarshalIndent(linkData, "", "  ")
_ = os.WriteFile(linkPath, linkBytes, 0o644)
```

---

### G-16 `RemoveScopeSessions` 中条件判断永远为 true — 逻辑冗余

**文件**: `internal/agentcore/session/controller/session_controller.go`，第 626-630 行

**问题**: 行 626 将 Sessions 置 nil，行 628 判断 `len(scopeMeta.Sessions) == 0` 永远为 true。与 Python 行为一致（总是删除 scope），但条件判断是多余的且令人困惑。

---

### G-17 `InitInteractiveInputs` 中从 state 读取输入的路径与 Python 不一致

**文件**: `internal/agentcore/session/interaction/base.go`，第 131-187 行

**问题**: Python 调用 `state().get()` 查询"组件级"状态，Go 优先使用 `GetGlobal()`。如果 `INTERACTIVE_INPUT` 存储在组件级状态而非全局状态中，Go 会读取到 nil。

---

### G-18 `SimpleMemoryIndex.SetStorageCodec` 并发不安全

**文件**: `internal/agentcore/foundation/store/index/simple.go`，第 85-87 行

**问题**: `s.codec` 字段无并发保护。`SetStorageCodec` 直接赋值，而 `AddMemories`/`Search`/`GetByID`/`ListMemories` 都在读 `s.codec`。Python 因 GIL 不存在此问题，Go 中可能导致读取半初始化的接口值。

---

### G-19 `SimpleMemoryIndex` Search 和 ListMemories 的 Decode 错误处理不一致

**文件**: `internal/agentcore/foundation/store/index/simple.go`，第 295-298 行、第 551-553 行

**问题**: Search 和 ListMemories 的 Decode 失败时 `continue` 静默丢弃且不记录日志，与 AddMemories/GetByID 返回 error 不一致。

---

### G-20 缺少 `WrappedSession`/`StateSession`/`RouterSession` 抽象层

**文件**: 缺失

**问题**: Python 有完整的 `WrappedSession` → `StateSession` → `RouterSession` 继承体系，提供 `update_state`、`get_state`、`write_stream` 等统一方法。Go 的 `NodeSessionFacade` 直接实现这些方法，未来添加新 Session 包装器时缺少可复用基础。

---

### G-21 `NodeSessionFacade.UpdateState()` 类型断言失败时静默吞掉错误

**文件**: `internal/agentcore/session/node.go`，第 97-107 行

**问题**: 当 `f.inner.State()` 不是 `*WorkflowCommitState` 时，UpdateState 什么也不做也不报错。同样问题存在于 GetState、UpdateGlobalState、GetGlobalState、DumpState 等方法。

---

### G-22 `InMemoryCommitState.Commit` 全量提交有多余操作

**文件**: `internal/agentcore/session/state/inmemory_commit_state.go`，第 81-95 行

**问题**: 先逐个设 nil 再用新 map 覆盖，是多余操作。Python 直接 `self._updates.clear()`。

---

## 五、🔵 提示问题（18 项）

### T-01 `AesStorageCodec` Warn 日志缺少 `event_type` 字段

**文件**: `internal/agentcore/memory/codec/aes_storage_codec.go`，第 70-72 行、第 88-90 行

**问题**: Python 日志包含 `event_type=LogEventType.MEMORY_PROCESS`，Go 缺少此字段，与日志规范第 8-9 条不符。

---

### T-02 `AesStorageCodec` 容错行为与接口签名语义矛盾

**文件**: `internal/agentcore/memory/codec/aes_storage_codec.go`，第 64-94 行

**问题**: 方法签名返回 `(string, error)` 暗示可能返回 error，但 `AesStorageCodec` 永远不返回非 nil error（容错模式）。导致调用方对 error 的处理策略因理解不同而不一致。

---

### T-03 `SqlMessageStore` 使用 `*codec.AesStorageCodec` 具体类型而非 `StorageCodec` 接口

**文件**: `internal/agentcore/memory/manage/model/sql_message_store.go`，第 27 行

**问题**: 无法注入其他 StorageCodec 实现（如测试时无法注入 mock），不利于单元测试。已知延后项。

---

### T-04 `AesStorageCodec` 缺少 Encode 每次产生不同输出的测试

**问题**: Python 有 `test_encode_produces_different_output` 测试（GCM 随机 nonce），Go 无对应测试。

---

### T-05 `AesStorageCodec` 缺少 Encode 加密异常容错测试

**问题**: Python 有 `test_encode_encrypt_exception_fallback`，Go 无对应测试。

---

### T-06 Python `AesStorageCodec` 中 CryptUtils 未注册时 passthrough 降级，Go 直接报错

**文件**: `internal/agentcore/memory/codec/aes_storage_codec.go`，第 42-58 行

**问题**: 虽然 `AesGcmCrypt` 在 init() 时自动注册，但如果全局注册表被清空，行为与 Python 不一致。

---

### T-07 `Transformer` 类型定义归类到枚举区块不当

**文件**: `internal/agentcore/session/state/state.go`，第 49-50 行

**问题**: 枚举区块用于 `type iota` 和类型别名。`Transformer` 是 `func` 类型定义，应归类到结构体区块。

---

### T-08 `key.go` 区块标题 "StateKey 方法" 不符合规范

**文件**: `internal/agentcore/session/state/key.go`，第 45 行

**问题**: 规范定义的区块为结构体/枚举/常量/全局变量/导出函数/非导出函数。"StateKey 方法"不在其中。

---

### T-09 `session.go` 接口和结构体分开两个分隔注释

**文件**: `internal/agentcore/session/session.go`，第 5-36 行

**问题**: 规范要求接口归类到结构体区块排在结构体之前，不应单独有"接口"分隔注释。

---

### T-10 `agent.go` 自定义方法分组注释不符合规范

**文件**: `internal/agentcore/session/agent.go`

**问题**: 使用了 "身份/配置方法"、"状态读写方法" 等非标准分隔注释，规范只允许 "导出函数" 和 "非导出函数" 两种。

---

### T-11 类型断言失败处缺少异常路径日志

**文件**: `internal/agentcore/session/agent.go`、`internal/agentcore/session/node.go` 多处

**问题**: 所有类型断言失败时静默忽略，根据日志规范第 8 条，异常路径应添加 Error 日志。

---

### T-12 `AgentStateCollection.UpdateGlobal` 忽略了 error 返回值

**文件**: `internal/agentcore/session/state/agent_state_collection.go`，第 42-44 行

**问题**: `_ = s.globalState.Update(data)` 用 `_ =` 忽略错误，应在注释中说明原因。

---

### T-13 `InMemoryState.SetState` 和 `InMemoryCommitState.SetUpdates` 未深拷贝传入数据

**文件**: `internal/agentcore/session/state/inmemory_state.go`，第 50-54 行；`inmemory_commit_state.go`，第 138-142 行

**问题**: 与 `GetState`（深拷贝返回）和 `Update`（深拷贝输入）的模式不对称。Python 也没有深拷贝，行为一致，但模式不统一。

---

### T-14 `NodeSessionFacade` 构造时记录 Info 级别日志

**文件**: `internal/agentcore/session/node.go`，第 41-45 行

**问题**: Python 的 `Session.__init__` 不记录日志。Go 在高频创建场景下会产生大量日志噪声，建议降为 Debug。

---

### T-15 `Interact()` 错误消息格式与 Python 不一致

**文件**: `internal/agentcore/session/node.go`，第 179 行

**问题**: Python 使用 `build_error(StatusCode.COMP_SESSION_INTERACT_ERROR, ...)` 构造结构化错误，Go 使用 `fmt.Errorf`。应使用 `exception.RaiseError` 对齐。

---

### T-16 `InteractiveInput.Update()` 中 `nodeID == ""` 与 Python `node_id is None` 语义不同

**文件**: `internal/agentcore/session/interaction/interactive_input.go`，第 56 行

**问题**: Python 允许空字符串 nodeID（仅禁止 None），Go 禁止空字符串。

---

### T-17 `SplitNestedPath`/`parseListIndexes` 不支持 `['key']` 语法

**文件**: `internal/agentcore/session/state/utils.go`，第 380-450 行

**问题**: Python 的正则 `r'\[(-?\d+)\]|\[\'([^\']+)\'\]'` 支持 `['key']` 语法，Go 只处理 `[数字]` 格式。

---

### T-18 时间戳精度不一致 — Go 毫秒 vs Python 微秒

**文件**: `internal/agentcore/session/controller/chain_session.go`，第 191 行

**问题**: Python `datetime.timestamp()` 精度为微秒，Go `float64(time.Now().UnixMilli()) / 1000.0` 为毫秒精度。

---

## 六、修复优先级建议

### 最高优先级（影响正确性）

| 问题 | 说明 |
|------|------|
| S-01 | `getBySchema` 补充 `isRoot`/`isRefPath` 逻辑 — 影响所有 schema 查询 |
| S-02 | `rootToPath` 支持 list 索引路径 — 影响所有含数组索引的更新 |
| S-03 | `getValueByNestedPath` 支持负数索引 — 功能缺失 |
| S-06 | `SessionScope` 作为 map key 改为 `string` 类型 — 防止查找失败 |

### 高优先级（运行时安全）

| 问题 | 说明 |
|------|------|
| S-04 | 所有状态结构体加并发保护（至少加 `sync.RWMutex`） |
| S-05 | `UpdateByID` 空字符串改为返回 error |
| S-07 | `Interrupt.Value` 定义 `OutputSchema` 结构体替代 `map[string]any` |
| S-08 | `WithSessionID` 应更新 sessionID 而非创建新实例 |

### 中优先级（功能完整性和一致性）

| 问题 | 说明 |
|------|------|
| G-01 | `SetUpdates` 类型断言需处理 JSON 反序列化后的类型 |
| G-02 | `AgentStateCollection` 补充 `traceState`/`UpdateTrace`/`Dump` |
| G-04 | `WithWorkflowSessionParent` 从 parent 获取 envs |
| G-06 | `Interact()` 预留追踪调用点 |
| G-07 | 类型断言失败时返回明确 error |
| G-11 | `Flush()` 保持锁一致性 |
| G-12 | `ChainSession.Load()` 实现数据容器恢复 |
| G-18 | `SetStorageCodec` 加并发保护 |
| G-19 | Search/ListMemories Decode 失败添加 Warn 日志 |

### 低优先级（规范和优化）

| 问题 | 说明 |
|------|------|
| T-01~T-18 | 日志规范、区块标题、测试覆盖、精度等 |

---

## 七、各模块总体评价

### 4.29 StorageCodec

**功能符合度**: 高。核心接口定义、AesStorageCodec 容错行为、密文格式、SimpleMemoryIndex 中的编解码集成均正确对齐 Python。主要设计决策（接口签名加 error、key 长度严格校验）都有文档说明且合理。

**主要风险**: 并发安全（G-18）和错误处理不一致（G-19）。

### 5.1 State 体系

**功能符合度**: 中。核心接口体系（ReadableState/RecoverableState/State/CommitState）已建立，但 `getBySchema` 的 `isRoot`/`isRefPath` 逻辑缺失（S-01）和 `rootToPath` 不支持 list 索引（S-02）是两个严重功能缺陷，影响所有 schema 驱动的状态查询和更新操作。

**主要风险**: Schema 查询逻辑不完整、并发安全、类型断言脆弱。

### 5.2-5.4 Session 体系

**功能符合度**: 中。BaseSession/AgentSession/WorkflowSession 的生命周期和状态管理基本对齐 Python。但 `WithSessionID` 实现有问题（S-08），`WithWorkflowSessionParent` 忽略了 parent（G-04），envs/config 继承不完整。

**主要风险**: 选项模式实现不当、类型断言错误处理不完善、缺少抽象层。

### 5.5 SessionNode

**功能符合度**: 中。NodeSessionFacade 的 18 个方法基本覆盖 Python 功能。但 Interact 缺少追踪调用点（G-06），类型断言失败静默处理（G-07/G-21），错误消息格式不对齐 Python（T-15）。

### 5.6 SessionController

**功能符合度**: 中。Scope/Subject 体系、DataContainer 工厂、ChainSession、GlobalSessionController 基本实现。但 SessionScope map key 不可靠（S-06），Flush 锁粒度问题（G-11），数据容器恢复跳过（G-12），加载错误被吞（G-13）。

### 5.7 Interaction

**功能符合度**: 中。BaseInteraction/WorkflowInteraction/SimpleAgentInteraction/AgentInteraction/InteractiveInput/InteractionOutput 已实现。但 `Interrupt.Value` 类型不安全（S-07），`initInteractiveInputs` 读取路径与 Python 不一致（G-17）。
