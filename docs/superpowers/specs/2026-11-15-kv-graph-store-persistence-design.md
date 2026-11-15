# KV 版 GraphStore（Persistence 侧回填）设计——8.9b 兑现 + 8.8/8.9b 盘点

> 日期：2026-11-15
> 章节：领域八 8.9b（提前兑现）+ 5.8/5.9 Persistence 侧回填
> Python 参考：`openjiuwen/core/session/checkpointer/persistence.py:554-720`（局部类 `GraphStore(Store)`）
> Go 落点：`internal/agentcore/session/checkpointer/persistence.go`（追加 `GraphStore` 类型）+ `internal/agentcore/graph/pregel/base.go`（Message 定制序列化）+ `internal/agentcore/graph/store/base.go`（PendingNode 定制序列化）
> 前置依赖：8.9a ✅（store.Store 接口/JSONSerializer）、5.9 ✅（kv.BaseKVStore/BuildKeyWithNamespace/WorkflowNamespaceGraph）

## 1. 流程位置与作用

Persistence 检查点器（GORM+SQLite 后端 `kv.NewDbBasedKVStore`）的图状态持久化通道。8.9a 落地时 persistence 侧 `graphStore=nil`（nil 守卫跳过）；本节补上 KV 版实现并注入，使 Persistence 检查点器的 2 处 Delete 与未来 8.3c 的 save/get 全链路真实生效。

## 2. 已拍板决策（2026-11-15，用户）

1. **落点与接口（方案 A）**：`GraphStore` 类型追加在 `persistence.go`（规则 9：Python 局部类属该模块），实现 `store.Store` 接口，内部持有 `kv.BaseKVStore` + `store.Serializer`（`CreateSerializer("pickle")` → JSONSerializer，偏差 D-1）。`PersistenceCheckpointer.graphStore` 字段与 `interfaces.Checkpointer.GraphStore()` 从 `*store.GraphStore` 改为 `store.Store`（对齐 Python checkpointer 持有 raw Store 的事实；InMemoryCheckpointer 内部仍构造 `NewGraphStore(NewInMemoryStore())` 但以接口承载）。
2. **D-10 kind 重建（方案 A）**：以 `Message` 定制 `MarshalJSON/UnmarshalJSON` 实现——序列化带 kind 槽（`"kind":"trigger"/"barrier"`），反序列化按 kind 精确重建，未知/缺失 kind 返回 error（守住「零值 kind 被通道拒收」的 8.6 不变式）。**不采用** barrier 前缀猜测。8.9b 悬挂的序列化面 D-10 就此兑现注销。
3. **D-8 error 适配（方案 A）**：`PendingNode` 定制序列化，`Exception []error ↔ []string`（双向 `err.Error()`/`errors.New`），null→nil 对齐 Python exception 默认 None。不做则 KV 往返直接断链（error 接口 Unmarshal 报错）。
4. **8.8/8.9b 盘点**：本节完成后 8.9b 剩余仅 kind 序列化面（已由本节消化）→ 8.9b 行状态登记为 ✅（内容并入本节）；8.8（graph_state.GraphState，零实例化死类型）维持 2026-11-15 后移拍板，仍 ⤵️ 待实施序走到。

## 3. Go 设计

### 3.1 persistence.go 内 GraphStore（对齐 Python :554-720）

```go
// GraphStore KV 版图状态存储（键形 session:workflow-graph:<ns>:<suffix>，
// 与 workflow 自身状态隔离）。Python: persistence.py:554-720 局部类
type GraphStore struct {
    // kvStore KV 存储后端（工厂路径为 GORM+SQLite 的 DbBasedKVStore）
    kvStore kv.BaseKVStore
    // serde 序列化器（CreateSerializer("pickle") → JSONSerializer，偏差 D-1）
    serde store.Serializer
}

// NewPersistenceGraphStore 构造（Python __init__ :571-576）
func NewPersistenceGraphStore(kvStore kv.BaseKVStore) *GraphStore {
    serde, _ := store.CreateSerializer("pickle") // "pickle" 分支恒成功，err 恒 nil
    return &GraphStore{kvStore: kvStore, serde: serde}
}
```

常量：`graphStoreDataType = "checkpoint_data_type"`、`graphStoreDataValue = "checkpoint_data_value"`（Python `_DATA_TYPE/_DATA_VALUE`）、键数 2。

### 3.2 Get（对齐 :603-653）

1. `Pipeline(ctx)` 两次 `Get`：`BuildKeyWithNamespace(sessionID, WorkflowNamespaceGraph, ns, graphStoreDataType/DataValue)`
2. `Execute` 结果数 ≠2 → error 日志后 `(nil, nil)`
3. type 或 value 任一空 → debug 日志（CHECKPOINT_RESTORE）后 `(nil, nil)`
4. 反序列化（`serde.LoadsTyped(string(type), value)`）失败或得 nil → error/debug 日志后 `(nil, nil)`
5. JSON 往返即副本，无需 Clone（对齐 Python pickle 语义）

### 3.3 Save（对齐 :656-700）

1. `serde.DumpsTyped(state)` 失败 → warn 日志后 `return nil`（Python 不 raise，照抄）
2. Pipeline 两次 `Set` 两键 → `Execute` 失败 → error 日志后**返回 error**（Python re-raise）
3. 成功 debug 日志（CHECKPOINT_SAVE，`storage_type=graph`）

### 3.4 Delete（对齐 :702-737）

- ns 空：`kvStore.DeleteByPrefix(ctx, BuildKey(sessionID, WorkflowNamespaceGraph), 0)`
- 否则：`DeleteByPrefix(ctx, BuildKeyWithNamespace(sessionID, WorkflowNamespaceGraph, ns), 0)`（batchSize=0 对齐 Python batch_size=None 默认，实施时核对 Go 侧 0 语义）
- debug 日志两分支（CHECKPOINT_CLEAR）

### 3.5 Message 定制序列化（pregel/base.go 追加，用户拍板形态）

`MarshalJSON`：`{"sender","target","payload","kind"}` 四槽；`UnmarshalJSON`：switch kind（`KindTrigger`/`KindBarrier`）精确赋 `m.kind`，default 返回 `unknown message kind: %q` error。已核对枚举值 `KindTrigger="trigger"`、`KindBarrier="barrier"`。

### 3.6 PendingNode 定制序列化（store/base.go 追加）

`MarshalJSON`：Exception → `[]string`（nil→null）；`UnmarshalJSON`：`[]string`→`errors.New`，null→nil。`GraphState` 本身不定制——json 对嵌套 Message/PendingNode 自动递归调用各自定制方法。

### 3.7 注入点（nil 守卫的两处保持不动）

`NewPersistenceCheckpointer` 构造：`graphStore: NewPersistenceGraphStore(kvStore)`；字段类型改 `store.Store`；`GraphStore()` 返回类型同步。两处 Delete 的 nil 守卫**保留**（防御性，Python 无此守卫但 Go 侧零成本；或按结构忠实删守卫——实施时确认：Python 侧 `_graph_state` 构造期必有，Go 构造期也必注入，故**删除 nil 守卫**更忠实，登记微偏差说明即可）。

### 3.8 已知偏差登记

| 编号 | 内容 |
|---|---|
| D-10' | Python pickle 无 JSON 形态；Message/PendingNode 的 Marshal/UnmarshalJSON 为 Go 侧发明，字节格式自定（含 `"kind"` 槽），不追求跨语言互操作 |
| D-8' | error 保真度收窄：仅保留 `err.Error()` 文本，类型/栈信息不持久化（pickle 全保真的近似） |
| D-11 | Python `_graph_state` 构造期必有故无空判；Go 构造期注入后同理删除 nil 守卫（等价忠实） |

## 4. 测试设计

- `Message` JSON 往返：kind 双值保真；缺 kind / 未知 kind 拒绝（error）；与 `GraphState` 组合往返（自动递归验证）
- `PendingNode` JSON 往返：error 文本保真、null→nil、非 nil 列表
- KV GraphStore 三方法（用 `kv.NewInMemoryKVStore` 或临时 sqlite 真库，按 5.9 既有测试惯例选）：Get 未命中 (nil,nil)、Save/Get 往返字段全等（含 kind、Exception）、Delete 单 ns / 全 session 前缀、Save 序列化失败 warn 静默（注入坏 serde 或不可序列化 payload）、pipeline Execute 失败透传 error
- 注入链路：`NewPersistenceCheckpointer` 后 `GraphStore() != nil` 且 Get/Save/Delete 可用；两处 Delete 路径实测清键
- 13 处 fake 返回类型连锁改 `store.Store` 后锚点 `GraphStore() *store.GraphStore` = 0 命中
- 覆盖率 ≥85%；全量回归 CI 同款标签零 FAIL

## 5. 计划表与注记变更（不丢失清单）

1. 8.9b 内容并入本节：8.9b 行（若计划表无独立行则维持 8.9 行内 ⤵️ 记录）改 ✅，注明「序列化面 kind 重建由 Persistence KV GraphStore 节兑现（Message/PendingNode 定制 Marshal/Unmarshal）」
2. 8.9 行 Go 事实段追加：「persistence 侧 KV GraphStore 已落地并注入，nil 守卫已删（D-11）；8.9b ⤵️ 注销」
3. 5.9 行若为 ✅，追加回填记录「graphStore 注入于 8.9b 兑现节完成」
4. 8.8 行不动（维持后移拍板）
5. 顺序注记区追加第六行记录本轮盘点结论
