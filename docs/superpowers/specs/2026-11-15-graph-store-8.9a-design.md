# 8.9a Graph Store 设计（方案 A：独立 store 包 / JSON serde / Clone 深拷贝）

> 日期：2026-11-15
> 章节：领域八 8.9a（`openjiuwen/core/graph/store/` 的 base.py + inmemory.py + serde.py 部分）
> Python 参考：`openjiuwen/core/graph/store/base.py`(159 行) · `inmemory.py`(47 行) · `serde.py`(51 行) · `__init__.py`(15 行)
> Go 落点：`internal/agentcore/graph/store/`（新包）+ `internal/agentcore/graph/pregel/message.go`（追加 `Copy`）
> 前置依赖：8.6 ✅（Message/Interrupt）、8.4 ✅（ChannelSnapshot）、8.3d ✅（常量）
> 后续消费方：8.3c（`PregelLoop.Init` resume / `RunStep` save / `_save_state_on_error` / `Build(store=…)`）、8.9b（channel_values 序列化）、5.8 回填（checkpointer `GraphStore() any` + 4 处 Delete）

## 1. 流程位置与作用

8.9a 是 Pregel 引擎的**持久化地基**。构图期 `PregelBuilder.Build(store=…)`（8.3c 回填）把 `Store` 注入 `*Pregel`；执行期每个 super-step 由 8.3c 的 `PregelLoop` 写入快照（`GraphState` 六字段：ns/step/channel_values/pending_buffer/pending_node/node_version），`Init` 时读回并按 `is_resumable()` 判定走恢复分支而非全新起跑。会话删除链路（5.8 checkpointer 的 4 处 Delete）也经 `GraphStore` 壳清理图状态。

```
构图期                        执行期（每个 super-step）
PregelBuilder.Build(store=…) ──▶ PregelLoop.Init：Store.get(session,ns)
                                 → is_resumable 判定 → 恢复 channel/node_version/step/pending
Checkpointer(5.8).GraphStore()─▶ 每步结束 / 中断出错：Store.save(session, ns, state)
                                 会话删除：Store.Delete(session, ns?)（4 处回填）
```

## 2. 已拍板决策（2026-11-15，用户）

1. **方案 A（包落点）**：新建 `internal/agentcore/graph/store/` 包，与 `pregel` 平级，一比一映射 Python 三文件（`base.go`/`inmemory.go`/`serde.go` + `doc.go`）。依赖方向 store→pregel 单向（对齐 Python TYPE_CHECKING 的 Message 引用）。
2. **方案 A（serde）**：`Serializer` 接口 + `JSONSerializer` 实现；`CreateSerializer("json")` 仍返回 error `"json is not yet supported"`（复刻 Python raise 语义——Python 侧类完整实现但工厂刻意拒绝，属预留未验收的死代码通道，我们照抄不自作主张解锁）；`"pickle"` 无 Go 对应物，以 JSON 作为功能等价物、登记永久偏差。
3. **方案 A（深拷贝，用户修正版）**：`pregel.Message` 提供 `Copy()` 方法（在 pregel 包内定义，方法体可直接读非导出 `kind` 并原样保留），`store.GraphState.Clone()` 对 `channel_values` 逐条调 `msg.Copy()`。不做 JSON 往返拷贝。
4. **方案 A（回填时点）**：5.8 checkpointer 的 `GraphStore() any` 收窄为具体类型（`*store.GraphStore`）并在本节回填，连锁改 5 个 fake；4 处 `⤵️ 8.9 回填` Delete 调用同步补齐。

## 3. Go 设计

### 3.1 文件映射（规则 9：一文件对一文件）

| Python | Go | 内容 |
|---|---|---|
| `base.py` | `base.go` | `PendingNode` / `GraphState` / `Store` 接口 / `CreateState` / `GraphStore` 日志壳 |
| `inmemory.py` | `inmemory.go` | `InMemoryStore` |
| `serde.py` | `serde.go` | `Serializer` 接口 / `JSONSerializer` / `CreateSerializer` |
| `__init__.py` | `doc.go` | 导出索引（Go 无 `__all__`，以 doc.go 核心类型索引承担） |

### 3.2 base.go

```go
// PendingNode 未完成节点的持久化记录
type PendingNode struct {
    // NodeName 节点名
    NodeName string
    // Status 任务状态（对齐 Python str，取值 TaskStatusInterrupt/TaskStatusError，8.3d 常量）
    Status string
    // Exception 累积的异常列表（Python 默认 None，Go 语义等价为 nil）
    Exception []error
}

// GraphState 引擎检查点状态（与 8.8 graph_state.GraphState 同名不同义，Go 靠包隔离）
type GraphState struct {
    // Ns 命名空间
    Ns string
    // Step super-step 序号
    Step int
    // ChannelValues 通道快照（Python Dict[str, Any]，按计划行收窄为 map[string][]pregel.ChannelSnapshot）
    ChannelValues map[string][]pregel.ChannelSnapshot
    // PendingBuffer 待发消息
    PendingBuffer []pregel.Message
    // PendingNode 未完成节点表
    PendingNode map[string]PendingNode
    // NodeVersion 节点版本
    NodeVersion map[string]int
}

// IsResumable 有未完成节点/待发消息或 channel 值时引擎 init 才走恢复分支。
// 恢复判定必须与 8.3c PregelLoop 同口径，两处经本方法共享，避免口径漂移。
func (s *GraphState) IsResumable() bool {
    return len(s.PendingNode) > 0 || len(s.PendingBuffer) > 0 || len(s.ChannelValues) > 0
}

// Store 图状态存储抽象（Python async 三方法 → Go 同步签名，async 由调用方 goroutine 承担）
type Store interface {
    Get(sessionID, ns string) (*GraphState, error)
    Save(sessionID, ns string, state *GraphState) error
    // Delete ns 为空串时删整个 session（Python Optional[str]=None → Go 空串哨兵，见偏差 D-3）
    Delete(sessionID, ns string) error
}

// CreateState 工厂：None → 空容器兜底（pending_buffer/pending_node/node_version）
func CreateState(ns string, step int, channelSnapshot map[string][]pregel.ChannelSnapshot,
    pendingBuffer []pregel.Message, pendingNode map[string]PendingNode, nodeVersion map[string]int) *GraphState
```

`GraphStore` 日志壳逐条复刻：构造收 `saver Store`；`Get` 未命中 debug 日志（`event_type=GRAPH_STORE_GET`、`session_id`、`graph_id=ns`）；get/save 异常 error 日志后**原样返回 error**（Python re-raise）；`Save` 前后两条 debug（Begin/Succeed，含 `step`）；`Delete` ns 为空打 `all`、前后 debug、异常走 **debug 级**（Python 原文即 `logger.debug` + re-raise，照抄）后返回 error。组件常量 `ComponentAgentCore`，Msg 文案英文（规则 3 + logs-english 纪律）。

### 3.3 inmemory.go

`InMemoryStore`：`map[string]map[string]*GraphState`（Python defaultdict → Go 惰性建二级 map）。三方法：

- `Get`：未命中返回 `(nil, nil)`（对齐 Python `Optional=None`）；命中返回**深拷贝**（`state.Clone()`）
- `Save`：存入 `state.Clone()`（对齐 Python 双向 deepcopy——存副本防外部篡改）
- `Delete`：session 不存在静默返回；ns 空串删整个 session；否则 `_delete_ns_by_prefix`（**前缀匹配** `strings.HasPrefix`，收集后逐个删）；删空后连带删 session 外层键

### 3.4 Clone / Message.Copy（深拷贝路径）

```go
// 在 internal/agentcore/graph/pregel/message.go 追加：
// Copy 返回 Message 的深拷贝（供 8.9a GraphState.Clone 使用；Python 侧由 copy.deepcopy 承担）
func (m Message) Copy() Message  // 逐字段复制 Sender/Target/Payload，kind 经包内路径原样保留
```

```go
// 在 store 包：
// Clone 返回 GraphState 的深拷贝
func (s *GraphState) Clone() *GraphState
//   - ChannelValues：外层 map + 内层切片 + 每个 Message 调 .Copy()
//   - PendingBuffer：逐条 .Copy()
//   - PendingNode / NodeVersion：一层 map 复制（值类型可浅拷贝）
//   - Exception：一层切片复制（error 值本身视为不可变）
```

### 3.5 serde.go

```go
// Serializer 序列化抽象（dumps/loads 返回 (类型名, 字节) 元组 → Go (string, []byte)）
type Serializer interface {
    DumpsTyped(obj any) (string, []byte, error)
    LoadsTyped(dataType string, data []byte) (any, error)
}

// JSONSerializer JSON 实现（功能上承担 Python PickleSerializer 的默认可用角色）
// CreateSerializer(typeName string) (Serializer, error)
//   - "json" → error "json is not yet supported"（复刻 Python raise，见 §2.2）
//   - "pickle" → 返回 JSONSerializer（偏差 D-1：pickle 无 Go 对应物）
//   - 其余 → error "Unknown serializer type: <name>"
// LoadsTyped：data 为 nil → (nil, nil)；类型不匹配 → (nil, nil)（对齐 Python 返回 None）
```

### 3.6 已知偏差登记

| 编号 | Python | Go | 理由 | 回收条件 |
|---|---|---|---|---|
| D-1 | `create_serializer("pickle")` 返回 `PickleSerializer` | `"pickle"` 返回 `JSONSerializer` | Go 无 pickle；json 是 Python 侧预留的替代通道 | 上游解锁 json 或提供别的序列化通道时跟随 |
| D-2 | `copy.deepcopy`（语言内建） | `Message.Copy()` + `GraphState.Clone()` | Go 无原生 deepcopy；非导出 kind 决定重建必须发生在 pregel 包内 | 无（永久语言层偏差） |
| D-3 | `delete(ns: Optional[str] = None)` | `Delete(sessionID, ns string)`，空串哨兵表示删全部 | Go 无 Optional | 无 |
| D-4 | async 三方法 | 同步签名 | Go 异步由调用方 goroutine 承担 | 无 |
| D-5 | `GraphState.channel_values: Dict[str, Any]` | `map[string][]pregel.ChannelSnapshot` | 计划行已拍板的类型收窄；Message.kind 非导出决定快照形态 | 随 8.9b 若需调整同步 |
| D-6 | `create_state` 关键字仅参 + `node_version: Dict = None` 默认 | 平铺位置参数 + nil 兜底 | Go 无关键字参数/可变默认值陷阱 | 无 |

## 4. 回填清单（同改动内完成，规则 8/13）

1. `session/checkpointer/inmemory.go`：`graphStore any` → `*store.GraphStore`（`GraphStore()` 接口面 `interfaces.go:83` 同步收窄）；4 处 `⤵️ 8.9 回填` 注释处补 `graphStore.Delete(sessionID, wid)`（实施时以 grep `⤵️ 8.9 回填` 逐处核实，当前定位 2 处：L181、L568）
2. `checkpointer/inmemory_test.go:383-390`「8.9 回填前应返回 nil」断言改写为返回非 nil
3. 连锁 fake：`GraphStore() any` 签名变更波及的 5 个测试替身（interaction_test / agent_test / agent_team_test / session_test / agent_session_test）同改动内改完
4. `IMPLEMENTATION_PLAN.md`：8.9 行拆分登记 8.9a（✅ 后回填 Go 事实段）与 8.9b（⤵️ 保留），顺序注记区追加一行记录本轮无换序、依赖核验结论（8.3b 与本节无依赖、维持编号序；8.8 尊重 2026-11-15 拍板维持后移）

## 5. 测试设计

- `IsResumable` 三条件各自成立/全空
- `CreateState` nil 兜底三容器
- `GraphStore` 壳：未命中 debug、异常透传（fake saver 抛错）、Delete 空 ns 打 `all`
- `InMemoryStore`：Get 命中返回副本（改副本不影响内部）、Save 存副本（改原对象不影响内部）、Delete 三分支（无 session / 全删 / 前缀删 + 删空连带）、前缀匹配只删前缀命中的 ns
- `Clone`：深拷贝独立性（改 Clone 后的 ChannelValues/PendingBuffer 不影响原）、`Message.Copy()` kind 保留（trigger/barrier 各验一次）
- `CreateSerializer`：`"json"` error 原文案、`"pickle"` 返回非 nil、未知类型 error；`LoadsTyped` nil → nil、类型不匹配 → nil
- 目标覆盖率 ≥85%（包整体）

## 6. 计划表与注记变更（不丢失清单）

1. 顺序注记区追加第五行（原四行注记逐字保留，追加制）：
   `> **8.x 实施顺序（2026-11-15 五次记录）**：8.9a 按方案 A 落地（store 独立包 + serde JSON 承担 pickle + Message.Copy 深拷贝 + 5.8 GraphStore 本节回填）；依赖核验：8.3b 与本节无依赖维持编号序、8.8 维持 2026-11-15 后移拍板（依据见 8.9a spec §2）。原注记保留。`
2. 8.9 行：原描述逐字保留，追加「8.9a ✅ 见本行 Go 事实段；8.9b ⤵️ 维持悬挂（Message.kind 重建已由 8.9a `Message.Copy` 提前兑现 D-10 的运行时深拷贝面，序列化面的 kind 重建仍留 8.9b）」——D-10 拆两半：Clone/深拷贝面本节兑现，JSON 序列化面（serde 产出字节流时的 kind 保真）留 8.9b。
3. 5.8 行（checkpointer）如有 `GraphStore any` 描述，回填时同步更正。
