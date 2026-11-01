# 8.5 IRouter 三实现（Pregel 路由）设计文档

> 对应实现计划：领域八 8.5（IMPLEMENTATION_PLAN.md L505）
> Python 源码：`openjiuwen/core/graph/pregel/router.py` 全文 56 行（`StaticRouter` / `SelectorProtocol` / `ConditionalRouter` / `BarrierRouter`）+ `pregel/base.py:23-26`（`IRouter`）
> 前置依赖：8.6（`Message` / `NewTriggerMessage` / `NewBarrierMessage`，已 ✅）、8.3d（常量，已 ✅）
> 后续消费方：8.3（`PregelNode.routers []Router`）、8.3a（`PregelBuilder` 是 Router 的唯一生产者）、8.3b（`NodeTask` 调 `Dispatch` 并据消息发 `EDGE_TRAVERSED` 事件）、8.4（`ChannelManager.flush` 按 `Kind()` 过滤）
> 章节顺序决策：**8.5 提前于 8.3/8.8/8.4**（已拍板）。理由见 §5

## 1. 流程位置与作用

### 1.1 在整体会话流中的位置

Pregel 是「超步（superstep）」调度模型。Router 是**一个节点跑完 → 下一批节点被激活**之间唯一的决策点；没有 Router 就没有边，图停在该节点。

```
Gateway / IM ──E2A──▶ AgentServer ──▶ AgentSession / Workflow（8.14）
  └─ CompiledGraph._invoke ──▶ Pregel.run(config)                        engine.py:222
       └─ PregelLoop.run_step()                                          engine.py:88
            ├─ ready = manager.get_ready_nodes()                         engine.py:111
            ├─ manager.consume(node)                                     engine.py:134
            ├─ executor.submit(node) → NodeTask.run()                    task.py:29 / :125
            │    ├─ 执行 node.func（内部可能 panic GraphInterrupt ← 8.7） task.py:151-154
            │    ├─ ★ for r in node.routers: await r.dispatch(...)        task.py:156-160   ← 本节
            │    │     ├─ StaticRouter      → [TriggerMessage] * N        router.py:17-18
            │    │     ├─ ConditionalRouter → [TriggerMessage] * N        router.py:32-46
            │    │     └─ BarrierRouter     → [BarrierMessage] * N        router.py:55-56
            │    └─ 逐条 trigger(EDGE_TRAVERSED, sender, target)          task.py:162-168
            ├─ succeed_messages → manager.buffer_message(msg)            engine.py:150-151
            └─ manager.flush()                                          engine.py:152
                 ├─ ch = map_key_to_channel[msg.target]                  channels.py:43
                 ├─ changed = ch.accept(msg)                            channels.py:47
                 │    ├─ TriggerChannel: msg.Kind()==trigger             channels.py:112-116
                 │    └─ BarrierChannel: msg.Kind()==barrier              channels.py:174-179
                 └─ any channel ready → 节点入就绪集（下一超步）           channels.py:57
```

**中断旁路（重要）**：`NodeTask.run` 在第 ④ 步捕到 `GraphInterrupt` 就直接把它当返回值交出去（`task.py:172-174`），**第 ⑤ 步整段不执行**，因此中断不产生任何消息。8.3b 落地时必须保持这个顺序，否则中断后仍会激活下游节点。

### 1.2 作用（4 条）

1. **编译期 DSL → 运行期消息的翻译器**：`PregelGraph.compile` 把用户声明的边交给 `PregelBuilder`（`graph.py:275`），builder 按边形状选 Router 并挂到源节点的 `routers` 列表（`builder.py:46-60`、`:67-69`）。Router 是**纯函数式**的：入参只有 `source_node`，出参只有消息列表，不读图、不读通道、不读检查点。
2. **三种拓扑语义**：
   - `StaticRouter`：1→N 静态扇出（含 1→1 与 **0 目标**，后者等价「边终止」，Python 测试用 `StaticRouter([])` 表示 END 节点）；
   - `ConditionalRouter`：由 selector 回调动态选目标，支持单目标与多目标；
   - `BarrierRouter`：N→1，产出的是**汇聚信号**而非节点激活，`target` 是 barrier 通道的**合成路由键** `barrier:a&b->c`（`channels.py:155-164`），`sender` 会被 `BarrierChannel.received` 收下作为 OR 组判据。
3. **消息字段的唯一生产者**：`Sender = source_node`、`Target = 目标路由键`。8.3b 的 `EDGE_TRAVERSED` 事件（`task.py:162-168`）直接读这两个字段，8.4 的 `accept` 也依赖它们，故本节必须保证字段填法精确。
4. **错误来源之一**：`ConditionalRouter` 的 selector 可失败（最典型是 `BranchRouter` 无分支命中时 `build_error`，`branch_router.py:104-107`），Python 靠异常上抛终止本超步；Go 侧走 `error` 返回值，由 8.3b 决定如何计入 `PendingNode.exception`。

### 1.3 selector 与 state 的关系（Python 事实基线）

`ConditionalRouter.dispatch` 的唯一"智能"是**按形参名嗅探**（`router.py:33-43`）：

```python
sig = inspect.signature(self.selector)
kwargs = {}
if 'state' in sig.parameters:      # 只看形参「名字」
    kwargs['state'] = None         # 值恒为 None，且全链无人读取
targets = await self.selector(**kwargs) if 协程 else self.selector(**kwargs)
```

实测结论（逐条核实，勿重复查）：

| 事实 | 证据 |
|------|------|
| `state` 的唯一取值是字面量 `None` | `router.py:37`；全仓 `.dispatch(` 只有一个调用点 `task.py:159` |
| 唯一会带 `state` 形参的 selector 是 `_workflow.py` 的包装器 | `_workflow.py:275-285` 生成 `def new_router(state): ...`，函数体**收下就扔**，真正作用是把 `session` 闭包进去 |
| `BranchRouter` 走的是「不注入」分支 | `branch_router.py:91` `async def __call__(self, *args, **kwargs)`，签名里没有 `state` |
| 「注入」与「不注入」两条路径**行为完全等价** | 两条分支的差异仅是把 `None` 传给一个不读它的形参 |
| `state` 想装的东西（图状态）从未接通 | 8.8 的 `graph_state.py` 只有 `source_node_id: Annotated[list, operator.add]`，与 `ConditionalRouter` 无任何数据通路 |
| 测试基线全部是无参 selector | `test_pregel.py:131/161/203/308`、`test_task.py:63-65` |

因此 Go 侧不需要复刻签名嗅探（reflect 也拿不到形参名），见 §4 D2。

## 2. 文件结构

```
internal/agentcore/graph/pregel/
├── doc.go             # 更新：目录树补 router.go，包概述补「路由」
├── base.go            # 8.6/8.7 已有（Message / Interrupt / GraphInterrupt）
├── base_test.go       # 8.6/8.7 已有
├── config.go          # 8.3d 已有
├── config_test.go     # 8.3d 已有
├── constants.go       # 8.3d 已有
├── constants_test.go  # 8.3d 已有
├── router.go          # 新增：Router 接口 + Selector + 三实现
└── router_test.go     # 新增
```

与 Python 一致：`IRouter` 在 Python 侧定义于 `base.py`，Go 侧落到 `router.go` 单文件（`base.go` 只放调度信号与中断类型，避免文件职责膨胀）。本节**不新增任何第三方 import**（仅 `context` / `fmt`）。

## 3. 类型设计

声明顺序遵循规范 2（结构体 → 导出函数 → 非导出函数）；三个实现按 Python 类名一一对应。

```go
// ──────────────────────────── 结构体 ────────────────────────────

// Router 路由接口：节点执行完成后，据此产出下一超步的调度消息。
//
// 实现方约定：纯函数式——只读 sourceNode，不访问图、通道或检查点。
// Python: pregel/base.py:23-26 IRouter（唯一方法 dispatch）
type Router interface {
	Dispatch(ctx context.Context, sourceNode string) ([]Message, error)
}

// Selector 条件路由的选择器：返回目标节点名列表。
//
// state 恒传 nil：Python 侧该形参的唯一取值就是字面量 None（router.py:37），
// 且全链无人读取（唯一带该形参的 selector 是 _workflow.py:275 的包装器，
// 它收下即丢）。保留形参只为对齐数据形状，将来若接通图状态也只能是 any
// —— pregel 不能 import graph（8.1 的 graph.py 反向 import pregel，成环）。
type Selector interface {
	Select(ctx context.Context, state any) ([]string, error)
}

// SelectorFunc 把普通函数适配为 Selector，对齐仓库 XxxFunc func(ctx ...) 惯例。
type SelectorFunc func(ctx context.Context, state any) ([]string, error)

// StaticRouter 静态扇出（1→N）。
//
// targets 为空时产出零条消息，等价「边终止」（Python 测试用 StaticRouter([])
// 表示 END 节点）。构造时复制入参切片，调用方后续改切片不影响本实例。
// Python: pregel/router.py:11-18
type StaticRouter struct {
	// targets 目标路由键列表（顺序即消息产出顺序）
	targets []string
}

// BarrierRouter 汇聚路由（N→1），产出 BarrierMessage。
//
// target 不是节点名，而是 barrier 通道的合成路由键 "barrier:a&b->c"
// （channels.py:155-164）；sender 由 BarrierChannel 收下作为 OR 组判据。
// Python: pregel/router.py:49-56
type BarrierRouter struct {
	// targets 目标 barrier 通道路由键列表
	targets []string
}

// ConditionalRouter 条件路由：由 selector 决定目标，产出 TriggerMessage。
// Python: pregel/router.py:26-46
type ConditionalRouter struct {
	// selector 选择器；nil 时 Dispatch 返回 error（对齐 Python 在 dispatch
	// 阶段才 inspect.signature(None) 抛 TypeError 的时机）
	selector Selector
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：三个实现必须满足 Router，SelectorFunc 必须满足 Selector。
var (
	_ Router   = (*StaticRouter)(nil)
	_ Router   = (*BarrierRouter)(nil)
	_ Router   = (*ConditionalRouter)(nil)
	_ Selector = SelectorFunc(nil)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStaticRouter 构造静态扇出路由。
// Python: StaticRouter(targets)（router.py:14-15）
func NewStaticRouter(targets ...string) *StaticRouter {
	return &StaticRouter{targets: append([]string(nil), targets...)}
}

// NewBarrierRouter 构造汇聚路由。
// Python: BarrierRouter(targets)（router.py:52-53）
func NewBarrierRouter(targets ...string) *BarrierRouter {
	return &BarrierRouter{targets: append([]string(nil), targets...)}
}

// NewConditionalRouter 构造条件路由。
// Python: ConditionalRouter(selector)（router.py:29-30）
func NewConditionalRouter(selector Selector) *ConditionalRouter {
	return &ConditionalRouter{selector: selector}
}

// Select 实现 Selector：调用被适配的函数。
func (f SelectorFunc) Select(ctx context.Context, state any) ([]string, error) {
	return f(ctx, state)
}

// Dispatch 产出 1→N 触发消息。
// Python: router.py:17-18
func (r *StaticRouter) Dispatch(_ context.Context, sourceNode string) ([]Message, error) {
	return buildTriggerMessages(sourceNode, r.targets), nil
}

// Dispatch 产出 N→1 汇聚消息。
// Python: router.py:55-56
func (r *BarrierRouter) Dispatch(_ context.Context, sourceNode string) ([]Message, error) {
	msgs := make([]Message, 0, len(r.targets))
	for _, target := range r.targets {
		msgs = append(msgs, NewBarrierMessage(sourceNode, target))
	}
	return msgs, nil
}

// Dispatch 由 selector 决定目标并产出触发消息。
//
// 纪律：state 恒传 nil（§1.3）；selector 的 error 原样上抛，不包装——
// 8.3b 需要区分 GraphInterrupt 与普通 error，包装会破坏 errors.As。
// Python: router.py:32-46
func (r *ConditionalRouter) Dispatch(ctx context.Context, sourceNode string) ([]Message, error) {
	if r.selector == nil {
		return nil, fmt.Errorf("条件路由未设置 selector，节点=%s", sourceNode)
	}
	targets, err := r.selector.Select(ctx, nil)
	if err != nil {
		return nil, err
	}
	return buildTriggerMessages(sourceNode, targets), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTriggerMessages 按目标顺序构造触发消息。
//
// 目标为空时返回非 nil 空切片（对齐 Python 的 []），便于 8.3b 直接 extend
// 且 len 判断语义一致。
func buildTriggerMessages(sourceNode string, targets []string) []Message {
	msgs := make([]Message, 0, len(targets))
	for _, target := range targets {
		msgs = append(msgs, NewTriggerMessage(sourceNode, target))
	}
	return msgs
}
```

### 3.1 关键取舍

| 取舍 | 决策 | 理由 |
|------|------|------|
| 接口命名 | `Router`（非 `IRouter`） | Go 惯例不加 `I` 前缀；8.1 的 `graph.Router`（`Callable` 类型别名）在另一个包，未来不冲突。Python 名 `IRouter` 写入函数注释备查 |
| `async dispatch` | `Dispatch(ctx, sourceNode) ([]Message, error)` | Go 无协程区分，`ctx` 是唯一可传取消的等价物；错误走返回值 |
| `state` 形参 | `Selector.Select(ctx, state any)`，Dispatch 恒传 `nil` | 已拍板方案 A；`any` 是终态（见 §3.2） |
| 三类型是否合并 | **不合并** | 与 8.6 合并 `Message` 的判据不同：那里是「零行为纯数据、仅按 isinstance 判别」；这里是「构造签名相同但产出 kind 不同」，合并只增加一个 `kind` 字段与分支，可读性下降 |
| 字段可见性 | `targets` / `selector` 非导出 + 构造函数 | 沿用 8.6「包外不可伪造」的做法；同时保证 §4 D5 的切片复制不可绕过 |
| 返回值切片 | 空目标返回**非 nil 空切片** | 对齐 Python `[]`；`make([]Message, 0, 0)` 非 nil |
| 错误文案语言 | 中文（仓库 975/1364 条 `fmt.Errorf` 为中文） | 与仓库主流一致 |
| 日志 | **不补** | Python `router.py` 全文零 `logger.` 调用，编码规范规则 3 无对应点 |

### 3.2 `state any` 为何是终态

1. Python 侧**没有类型可搬**：`dispatch` 里是字面量 `kwargs['state'] = None`（`router.py:37`），`SelectorProtocol.__call__(self, output: Any = None)`（`router.py:22`）连 `state` 形参都没声明。
2. 8.8 的 `GraphState` **不是它**：`graph_state.py` 的 `TypedDict{source_node_id: Annotated[list, operator.add]}` 语义是「节点返回值聚合」，与「路由时传入的图状态」只是字面同名。Python 从未把前者喂给后者。
3. **结构性约束**：`graph_state.py` 属于 `graph` 包，而 8.1 的 `graph.py` 会 import `pregel`（`Pregel` / `PregelBuilder`）。若 `pregel` 反向 import `graph` 即成编译期环。这与 8.6 定死的 `Interrupt.Value any` 是同一条纪律：`pregel` 是叶子包，跨层值只能装箱透传。

## 4. 偏差归档

| 编号 | 偏差 | 缘由 | 审查口径 |
|------|------|------|---------|
| D1 | `async def dispatch` → `Dispatch(ctx, …) (…, error)` | Go 无协程/同步区分；错误必须走返回值 | 8.3b 调用点统一 `msgs, err := r.Dispatch(ctx, name)` |
| D2 | 不复刻「形参名含 `state`」嗅探，改为恒传 `nil` | Go reflect 无法取形参名（只能取类型与元数，仓库 `NumIn()` 零命中）；且该值恒 nil、零读取，两分支行为等价 | 若将来真接通图状态，须先解决 `pregel ↛ graph` 的环，届时另开偏差 |
| D3 | selector 返回 `[]string`，不做 `str → [str]` 归一化 | 静态类型下归一化是净损失；Python 的 `isinstance(targets, str)` 容错分支无测试覆盖 | 8.14 的包装器负责把单目标包成切片；若出现调用方遗漏，表现为编译期错误而非运行期静默 |
| D4 | `Dispatch` 的 `ctx` 在 Static/Barrier 中未使用 | 两者无阻塞点；保留形参以满足统一接口 | 用 `_ context.Context` 形参名显式标注 |
| D5 | `targets` 构造时复制 | Python 存引用（`self.targets = targets`），可被外部改写；无消费者依赖可变性 | 测试用例 `TestNewStaticRouter_复制入参` 锁死 |
| D6 | selector 返回 error 原样上抛，不 `fmt.Errorf("...: %w")` | 8.3b/8.3c 需要 `errors.As` 识别 `*pregel.GraphInterrupt`；加包装层会让中断判定失配 | 任意一层包装后必须用 `%w`，且 8.3b 侧用 `errors.As` 而非 `==` |

## 5. 章节顺序调整（8.5 前移，已拍板）

8.6 spec 与 8.7 spec §11 记录的路线是：

```
8.7 → 8.3(PregelNode/Channel ABC) → 8.8(GraphState) → 8.4(channels) → 8.5(routers) → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b
```

本节调整为（**只把 8.5 提到最前，其余相对次序不变**）：

```
8.5(routers) → 8.3(PregelNode/Channel ABC) → 8.8(GraphState) → 8.4(channels) → 8.3a(builder) → 8.9a → 8.3b → 8.3c → 8.9b
```

**依据**：

1. 8.3 的 `PregelNode` 字段是 `func + routers []IRouter`（`base.py:46-50`），**必须**引用 Router 接口；而计划表 8.3 行只列了 `PregelNode` + `Channel` ABC，`IRouter` 归在 8.5 名下。按旧顺序做 8.3 时接口尚未存在，只能临时把接口塞进 `base.go`，等于拆掉 8.5 的一半。
2. 8.5 的依赖面是全表最干净的：只依赖 8.6 的 `Message`（已 ✅）+ 标准库 `context`/`fmt`。
3. 8.8（`graph_state.py` 7 行）位置无关紧要，保持原相对次序。
4. 8.9 拆 a/b 的结论不变（`PendingNode` 与 `create_state` 必须先于 8.3b/8.3c），本轮不动。

## 6. 回填清单（不删旧内容）

1. **`IMPLEMENTATION_PLAN.md` L505（8.5 行）**：`☐` → `✅`。内容列保留原文的 `StaticRouter(1→N)/ConditionalRouter(…)/BarrierRouter` 描述，追加 Go 事实：包内 `router.go`、接口名 `Router`、`Selector`/`SelectorFunc`、D1-D6 偏差编号、`router.go` 覆盖率、设计文档路径。
2. **`IMPLEMENTATION_PLAN.md` 领域八表格之后的独立注记行**（新增一行，不动 8.6/8.7 两行原文）：`> **8.x 实施顺序（2026-11-01 调整）**：8.5 → 8.3 → 8.8 → 8.4 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b（8.5 自原路线前移，依据见 8.5 spec §5）。`
3. **`docs/superpowers/specs/2026-10-31-graph-interrupt-8.7-design.md` §11**：在路线代码块**下方**追加一行 `> 2026-11-01 调整：8.5 提前到 8.3 之前（8.5 只依赖已完成的 Message，而 8.3 的 PregelNode 必须先有 Router 接口）。原文保留。`，不改动原代码块。
4. **`pregel/doc.go`**：文件目录补 `├── router.go       # Router 接口 + Selector/SelectorFunc + 三个路由实现`；包概述段补一句「路由（节点执行后产出下一超步调度消息）」；`对应 Python 代码` 行补 `pregel/router.py`。
5. **本节无 ⤵️ 待回填点**：全仓 `grep -rn "8\.5" --include=*.go internal/` 仅命中无关字符串（`deep_adapter.go:746` 的步骤注释、测试里的时间戳），无占位类型或 TODO。

## 7. 测试设计

`router_test.go`（目标 `router.go` 覆盖率 **100%**，`-race` 干净）：

| 用例 | 断言 |
|------|------|
| `TestStaticRouter_零目标` | 返回非 nil 空切片、`len==0`、`err==nil` |
| `TestStaticRouter_单目标` | 1 条消息，`Kind()==KindTrigger`，`Sender`/`Target` 精确 |
| `TestStaticRouter_多目标保序` | 5 目标，消息顺序与入参顺序逐位一致 |
| `TestNewStaticRouter_复制入参` | 构造后调用方改原切片，实例行为不变（锁死 D5） |
| `TestBarrierRouter_产出汇聚消息` | `Kind()==KindBarrier`；`Target` 用真实合成键样例 `barrier:(a\|b)&c->d`（对齐 `channels.py:155-164` 格式） |
| `TestBarrierRouter_零目标` | 非 nil 空切片 |
| `TestConditionalRouter_单目标` | selector 返回 `[]string{"D"}` → 1 条 Trigger 消息 |
| `TestConditionalRouter_多目标` | 顺序保持 |
| `TestConditionalRouter_零目标` | 非 nil 空切片 |
| `TestConditionalRouter_selector收到nilState` | selector 内断言 `state == nil`（记录 Python 事实基线） |
| `TestConditionalRouter_selector返错原样上抛` | 用哨兵 error 断言 `err == sentinel`（不是包装后），锁死 D6 |
| `TestConditionalRouter_selector为nil` | 返回 error 且 `len(msgs)==0` |
| `TestConditionalRouter_ctx透传` | selector 内取 `ctx.Value(...)` 校验同一个 ctx |
| `TestRouter_接口断言` | `var _ Router = (*XxxRouter)(nil)` 三行（编译期）+ `SelectorFunc` 满足 `Selector` |
| `TestSelectorFunc` | 函数适配后调用转发正确 |

## 8. 实施顺序建议（供 writing-plans 展开）

1. `router.go`：`Router`/`Selector`/`SelectorFunc` + 三实现 + `buildTriggerMessages` + 编译期断言
2. `router_test.go`：上表用例；`go test ./internal/agentcore/graph/pregel/ -cover` 确认 `router.go` 100%
3. `doc.go` 更新（目录树 + 概述 + Python 路径）
4. `IMPLEMENTATION_PLAN.md` 8.5 → ✅、8.6/8.7 追加前移注记
5. 8.7 spec §11 追加调整行
6. `gofmt` 走 GOROOT 下的二进制（`$(go env GOROOT)/bin/gofmt -l -w`），不要用 PATH 里可能缺失的版本
7. 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./...`（293 包零 FAIL）

## 9. 后续衔接提示（本轮不实施）

- **8.3**：`PregelNode{ Name string; Func NodeFunc; Routers []Router }` 直接引用本节接口；`Channel` ABC 的 `Accept(msg Message) bool` 依赖 8.6 的 `Kind()`。
- **8.3a**：`PregelBuilder.addEdge` 三形状 → `NewBarrierRouter(barrierKey)` / `NewStaticRouter(end...)` / `NewStaticRouter(end)`；`addBranch` → `NewConditionalRouter(selector)`。`barrier:...` 合成键由 8.4 的 `BarrierChannel` 生成，builder 只做转手。
- **8.3b**：`NodeTask` 在节点函数正常返回后遍历 `node.Routers`；**中断时必须跳过整段路由**（对齐 `task.py:172-174`）。
- **8.14**：用户 callable 包装成语义等价 `SelectorFunc`（吞掉 `state`、闭包 `session`），对应 `_workflow.py:275-285`。
