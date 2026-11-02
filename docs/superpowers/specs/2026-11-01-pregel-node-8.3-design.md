# 8.3 PregelNode（节点描述体）设计文档

> 对应实现计划：领域八 8.3（IMPLEMENTATION_PLAN.md L499）
> Python 源码：`openjiuwen/core/graph/pregel/base.py:46-50`（`PregelNode`，5 行）；调用协议见 `pregel/task.py:125-174`
> 前置依赖：8.5（`Router` 接口及三实现，已 ✅）、8.3d（`InnerPregelConfig`，已 ✅）、8.6（`Message`，已 ✅）
> 后续消费方：8.3a（`PregelBuilder` 是唯一生产者，构图期对 `routers` 原地追加）、8.3b（`NodeTask` 是唯一调用者，负责 `NodeInput` 的唯一构造点）、8.3c（`Pregel`/`PregelLoop` 按节点名持有并取用）
> 本节范围：**只做 `PregelNode`**。`Channel` ABC 已按方案 B 移入 8.4（`channels.go`），不再属本节。

## 1. 流程位置与作用

### 1.1 在整体会话流中的位置

Agent 的一次执行本身就是一个工作流图；工作流被编译成 Pregel 图后，由「超步」循环驱动。`PregelNode` 是这个循环里**唯一把「做什么」和「往哪走」绑在一起的结构**。

```
Agent 会话（single_agent / ReAct 等）
 └─ Workflow.invoke                                      workflow.py:317
     └─ BaseWorkflow.compile                             _workflow.py:306
         └─ PregelGraph.compile                          graph.py:216
             └─ PregelGraph._compile                     graph.py:242-245 ← 构图期（只跑一次）
                 ├─ nodes{id: Vertex} → builder.add_node → ★PregelNode(name, func, routers)
                 ├─ edges            → builder.add_edge  → ★nodes[src].routers.append(Router)
                 └─ builder.build    → Pregel(nodes, channels, ...)   builder.py:71-77
                     └─ Pregel.run                     engine.py:222  ← 执行期
                         └─ PregelLoop.run_step        engine.py:88
                             ├─ get_ready_nodes() → active_nodes（节点名字符串）  engine.py:111
                             ├─ tasks_to_run.append(self.graph.nodes[name])      engine.py:135
                             ├─ executor.submit(node, node_version[name])        engine.py:139
                             │    └─ NodeTask.run                                task.py:125
                             │         ├─ ① node.func(**kwargs)      ← 执行体  task.py:151-154
                             │         ├─ ② node.routers[i].dispatch() ← 出边  task.py:156-160
                             │         └─ ③ trigger(EDGE_TRAVERSED, ...)        task.py:162-168
                             └─ succeed_messages → buffer → flush → 下一超步     engine.py:150-152
```

### 1.2 作用（5 条）

1. **本结构的三个字段恰好是图引擎的三个必需品**：`name`（调度键 = 通道键）、`func`（执行体）、`routers`（出边）。缺 `name` 则引擎按名字取不到节点（`engine.py:135`）；缺 `routers` 则节点跑完图就停住；缺 `func` 则没有执行体。
2. **构图期与执行期之间唯一的交接物**：构图期由 8.3a 生产 → 执行期被 8.3b 调用一次/超步 → 被 8.3c 按名字持有。本包剩余三节都围着它转。
3. **中断旁路的物理位置**：`func` 抛 `GraphInterrupt` 后，`task.py:172-174` 直接把异常当返回值交出去，**第 ② 步整段不执行**。即「中断不产生任何出边消息」这条硬约束，落点就在本结构的两段之间。
4. **不携带运行时状态**：版本号（`node_version`）不在节点里，由引擎在 `submit(node, version)` 时传入；`state` 也不在。节点是纯描述体，不随超步变化，因此天然可并发共享（只读）。
5. **它是 `Vertex` 之上的薄壳**：真实执行体是 8.11 的 `Vertex` 实例（`vertex.py:323` 的 `__call__(state, config)`），`PregelNode` 只负责把它和出边绑在一起，不做任何封装/装饰。

### 1.3 Python 事实基线

```python
# pregel/base.py:46-50
class PregelNode:
    def __init__(self, name: str, func: Callable[[Any], Any], routers: list[IRouter]):
        self.name = name
        self.func = func
        self.routers = routers        # 存引用，不拷贝
```

| 事实 | 证据 |
|---|---|
| 零方法、零校验、零日志 | `base.py` 全文无 `logger.` 调用，类体只有三行赋值 |
| `routers` 构造后必须可原地追加 | `builder.py:49/52/56/60/68` 全是 `nodes[item].routers.append(...)` |
| `func` 的调用协议由 8.3b 用**形参名嗅探**决定 | `task.py:137-149`：`inspect.signature(func)` → 名字含 `config` 则注入派生 `inner_config`（`ns = parent:node:version`），名字含 `state` 则注入字面量 `None`；随后 `target_func(**kwargs)` |
| 实际只有两种函数形态 | `builder.py:18-19` 的 `lambda: None`（零参）；`vertex.py:323` 的 `__call__(self, state, config)`（两名全命中 ⇒ 每次注入） |
| 类型注解自相矛盾 | 注解是 `Callable[[Any], Any]`（1 个位置参数），真实调用是纯 kwargs ⇒ 注解不可信 |
| 调用返回值被丢弃 | `task.py:151-154` 只 `await target_func(**kwargs)`，不用返回值；出边完全由 `routers` 决定 |
| `state` 恒为 `None` | `task.py:149` 是唯一赋值点；`vertex.py:341` 把它塞进事件的 `inputs`，值永远为 None |

## 2. 文件结构

```
internal/agentcore/graph/pregel/
├── doc.go          # 已有：更新文件目录树 + 概述句（「节点与通道抽象（8.3）」→「节点描述体（8.3）」）
├── node.go         # 新增：NodeInput / NodeFunc / PregelNode + NewPregelNode / addRouter / NoopNodeFunc
└── node_test.go    # 新增：§7 用例
```

不并入 `base.go`：那里已有 `Message`/`Interrupt`/`GraphInterrupt` 三类型 111 行，且本包 `router.go`/`channels.go`/`config.go` 均为一类型一文件。

## 3. 类型设计

### 3.1 `NodeInput` + `NodeFunc`

```go
// NodeInput 节点函数入参：把 Python 的「按形参名条件注入」换成显式结构体传递。
// 纪律：唯一构造点是 8.3b 的 NodeTask，别处不应构造。
// Python: task.py:137-149
type NodeInput struct {
	// Config 本次执行的内部配置，Ns/ParentNs 由 8.3b 改写为 parent:node:version
	Config InnerPregelConfig
	// State 恒 nil，对齐 task.py:149 的字面量 None。类型只能是 any（§3.2）
	State any
}

// NodeFunc 节点执行体。
//
// 纪律：只返回普通错误。图级中断是 panic（*GraphInterrupt），由 8.3b 用 recover
// 转成返回值；NodeFunc 内不得 recover 或吞掉它（对齐 task.py:172-174 的 except 顺序）。
// 出边完全由 Routers 决定，NodeFunc 的返回值不参与路由（Python 同）。
type NodeFunc func(ctx context.Context, in NodeInput) error
```

`error` 是唯一出通道：Python 节点没有「返回值」语义（`task.py:151-154` 丢弃调用结果），所以 Go 不需要 `(any, error)`。

### 3.2 `State` 只能是 `any`（终态，不是占位）

1. **值恒 nil**：`task.py:149` 是唯一赋值点，字面量 `None`。
2. **类型本尊是死类型**：`graph_state.GraphState`（8.8，`TypedDict{source_node_id: Annotated[list, operator.add]}`）在全仓**只被 import 一次**（`vertex.py:18`）、**零实例化**（唯一的 `GraphState(` 命中在 `store/base.py:65`，那是 8.9 的同名另一个类）、`operator.add` 无人执行。
3. **结构性硬约束**：`graph_state.py` 属 `graph` 包；Go 侧 `graph` 必须 import `pregel`（8.1 的 `PregelGraph.compile` 要调 `PregelBuilder`）。若 `pregel` 反向引用 `graph.GraphState` 即构成编译期环。

结论：`State any` 为终态，8.3 落地时不改、也不设回填点。将来若要接通图状态，唯一合法形态是「`State any` + 在 graph 层断言」（与 session 层断言 `Interrupt.Value` 同款纪律），而非在 pregel 内换具体类型。

**接入状态（2026-11-01 实施后追记，非回填点）**：本节交付时 `NodeInput` 的两个字段**零读者、零写者**——`NewPregelNode`／`NodeInput{...}`／`NodeFunc(...)` 在全仓非测试代码中零调用，`node_test.go` 是唯一构造点，且只断言 `State == nil`。它此刻的价值只有两条：① 把 `NodeFunc` 的签名一次定死，避免 8.3b/8.11 返工；② 对齐 Python 的数据形状。真正的写入方是 8.3b 的 `NodeTask`（`Config` 由 `CreateInnerConfig` 派生并改写 `Ns`/`ParentNs`），真正的读取方是 8.11 的 `Vertex`。这类「字段先于消费者存在」的状态属于本节的有意设计，**不是缺陷、也不设 ⤴️ 回填标记**。

### 3.3 `PregelNode`

```go
// PregelNode 图节点的描述体：调度键 + 执行体 + 出边。
//
// 字段全部非导出：8.3a/8.3b/8.3c 与本类型同在 pregel 包，非导出不阻碍任何消费方，
// 却能让「构造 / 追加」成为唯一入口（nil 路由只能在这两处被拦下，代价最低）。
// 使用方式为指针：builder 构造后还要追加路由，值类型会丢 mutation。
// Python: pregel/base.py:46-50
type PregelNode struct {
	// name 调度键，同时是 TriggerChannel 的通道键
	name string
	// fn 执行体，非 nil（构造期保证）
	fn NodeFunc
	// routers 出边，构造后由 8.3a 追加，8.3b 逐条 Dispatch；元素非 nil
	routers []Router
}
```

字段全非导出是 8.5 spec §9 已拍板的结论（2026-11-01）；本设计只执行，不重议。

### 3.4 三个函数

```go
// NewPregelNode 构造节点。
//
// routers 为变长参数并按构造期复制（Python base.py:50 存引用；无消费者依赖可变性，
// 同 8.5 D5）。fn 为 nil 或任一 router 为 nil 一律 panic。
// Python: PregelNode(name, func, routers)
func NewPregelNode(name string, fn NodeFunc, routers ...Router) *PregelNode

// addRouter 追加一条出边路由，供 8.3a 的 builder 在构图期使用。
//
// 对应 Python 的 nodes[src].routers.append(...)（builder.py:49-68）；字段非导出后
// 必须留此方法，否则 8.3a 无法落地。r 为 nil 时 panic（与构造期同口径）。
func (n *PregelNode) addRouter(r Router)

// NoopNodeFunc 空节点函数，对齐 builder.py:18-19 的 lambda: None（START/END 占位节点）。
func NoopNodeFunc(context.Context, NodeInput) error
```

`addRouter` 未导出：全包内唯一调用方是 8.3a 的 builder。

### 3.5 nil 校验的两个已知边界（写进注释）

1. **typed-nil 挡不住**：`var r *StaticRouter; var i Router = r` 时 `i != nil` 为 true，构造期 `r == nil` 判定通过，但 8.3b 调 `Dispatch` 会解引用 `r.targets` panic。这是 Go 接口的固有性质，运行期无法防御，故 8.3b 侧**零判空**（8.5 spec §9 已述）。
2. **`nil fn` panic 是 Go 独有的严格化**：Python 对 `func=None` 是在执行期 `inspect.signature(None)` 抛 TypeError；Go 若不在构造期拦，nil 函数调用会崩在 8.3b 的调用栈里、trace 指向框架而非构图错误。已确认（2026-11-01）采用构造期 panic。

## 4. 偏差归档

| 编号 | Python | Go | 理由 |
|---|---|---|---|
| D1 | `Callable[[Any], Any]` + `inspect.signature` 按形参名条件注入 kwargs | `NodeFunc func(ctx, NodeInput) error`，无条件结构化传入 | Go `reflect.Type` 只有类型与元数、**无函数形参名**，且调用参数编译期定死；与 8.5 D2 同源（能静态表达就静态表达，不做反射嗅探）。节点忽略 `Config`/`State` 的代价为零（值类型、无副作用） |
| D2 | 节点调用返回值直接丢弃（`task.py:151-154`） | 唯一出通道是 `error` | Python 节点无「返回值」语义，出边只由 Routers 决定；返回 `(any, error)` 会造出一个无人消费的载荷 |
| D3 | `self.routers = routers` 存引用，builder 直接 `.append` | 构造期复制入参切片 + 新增 `addRouter` 承接追加 | 无消费者依赖可变性（同 8.5 D5）；字段非导出后原地追加必须走方法，nil 校验也因此覆盖到两条路径 |
| D4 | 零校验；`func=None` 执行期 TypeError、`routers` 含 `None` 执行期 AttributeError | 构造期与追加期 panic | 把「必然崩」从执行期挪到构图期，不引入 Python 没有的运行期分支；typed-nil 仍挡不住（§3.5） |
| D5 | `state: GraphState` 注解 | `State any` 恒 nil | 值恒 None；`graph_state.GraphState` 全仓零实例化零读取；`pregel → graph` 引用成编译期环（§3.2） |
| D6 | 类实例（引用语义） | `*PregelNode` | builder 构造后仍要追加路由，值类型会丢 mutation |
| — | 零 `logger.` 调用 | 零日志 | `base.py` 全文无日志，本节不引入 |

## 5. 章节顺序（本轮不动，事实留档）

**决策（2026-11-01，用户）**：8.3a 与 8.8 的章节位置问题**本轮不处理**，继续保持计划表现行顺序（`8.3 → 8.8 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b`）。8.3a/8.8 两行原文不动、顺序不改；仅在计划表顺序注记区追加一行记录本轮决策（见 §6 第 2 条）。

留档两条事实，供后续节次自行消化（本节不做任何动作）：

1. **8.3a 的 `Build` 依赖 8.3c**：`builder.build()` 返回 `Pregel(nodes, channels, store, after_step)`（`builder.py:71-77`），`*Pregel` 属 8.3c。按现行顺序，8.3a 的 `Build` 若先做会缺类型；但 `AddNode`/`AddEdge`/`AddBranch` 只依赖已完成的 8.3/8.4/8.5，可先行。
2. **8.8 的 `GraphState` 是零消费死类型**：全仓零实例化、零读取、`operator.add` 无人执行；唯一引用是 `vertex.py:18` 的类型注解，且 `internal/agentcore/graph` 包当前尚不存在（8.1 才建）。

## 6. 回填清单（不删旧内容）

1. `IMPLEMENTATION_PLAN.md` L499（8.3 行）：`☐ → ✅`，**保留**「⤴️ `Channel` ABC 已移入 8.4（2026-11-01 方案 B…），本节只留 `PregelNode`」原文，追加 Go 事实（`node.go`、`NodeInput`/`NodeFunc`/`PregelNode`、`addRouter`、D1-D6、覆盖率）。
2. `IMPLEMENTATION_PLAN.md` L542/L544 两行顺序注记：**原文保留**，追加第三行「8.3 已落地（2026-11-01），下一节仍为 8.8；8.3a 与 8.8 的位置问题本轮明确不处理，事实留档见 8.3 spec §5」。
3. `docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md` §9：
   - 首句「**8.3**：`PregelNode` 直接引用本节接口…」→ 追加「已兑现：`pregel/node.go`」；
   - 「**已拍板（2026-11-01）**：字段全部非导出 + `NewPregelNode(name, fn, routers ...Router)`…构造时对 `nil` 元素直接 panic」→ **原文一字不改**，追加「已兑现 + `addRouter` 承接 builder 的原地追加（Python 无此方法）」；
   - 「**理由**」段后追加一条：`fn` 形态与 `NodeInput` 的对应关系（承接本节 D1「不做签名嗅探」）。
4. `docs/superpowers/specs/2026-11-01-pregel-channels-8.4-design.md` §5「计划表 8.3 只保留 `PregelNode`」→ 追加「已兑现（`node.go`）」。
5. `docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md` 首段与 R2 行中涉及 8.3 的表述 → 追加「`Channel.Accept` 已由 8.4 落地；8.3 只留 `PregelNode` 并已落地，`Kind()` 的消费方全部兑现」。
6. `internal/agentcore/graph/pregel/doc.go`：目录树加 `node.go`；概述句「节点与通道抽象（8.3）」→「节点描述体（8.3）」；首段补充 `PregelNode` 到类型清单。
7. `internal/agentcore/graph/pregel/channels.go:16-19` 归属说明：**保留原文**（记录的是「原归 8.3」的历史，不因本节落地而改写）。
8. memory：新建 `pregel-node-8.3-complete.md`；更新 `pregel-router-8.5-complete.md` 中「下一节 8.3」的指向与「8.3 已拍板」段的完成态。

## 7. 测试设计

| 用例 | 断言 |
|---|---|
| `TestNewPregelNode_字段与顺序` | `name` 一致；`routers` 长度与顺序与入参一致；`fn` 可调用 |
| `TestNewPregelNode_复制入参切片` | 外部切片改元素后，节点内 `routers` 不受影响 |
| `TestNewPregelNode_nilFnPanics` | `recover()` 捕到 panic |
| `TestNewPregelNode_nilRouterPanics` | 含「多个 router 中间夹一个 nil」 |
| `TestAddRouter_追加保序` | 连续追加三条，顺序为追加顺序；`fn` 与 `name` 不受影响 |
| `TestAddRouter_nilPanics` | `recover()` 捕到 panic |
| `TestNoopNodeFunc_返nil` | 返回 nil；入参 `NodeInput` 值原样可取（`Config.Ns` 读回一致） |
| `TestNodeFunc_接收NodeInput` | 用闭包捕获入参，验证 `ctx` 与 `NodeInput{Config, State}` 原样透传、`State` 为 nil |

覆盖率目标：`node.go` 100%（本节只有三个函数，逐函数全覆盖可达）。

## 8. 实施顺序建议（供 writing-plans 展开）

1. `node.go`：`NodeInput`/`NodeFunc`/`PregelNode` 三类型 → `NewPregelNode` → `NoopNodeFunc` → `addRouter`（按仓库声明顺序规范：结构体 → 导出函数 → 非导出函数）
2. `node_test.go`：§7 用例
3. `pgrep -f 'go (build|test)'` 确认无残留进程后单包跑：`go test ./internal/agentcore/graph/pregel/ -cover`，确认 `node.go` 100%
4. `doc.go` 更新（目录树 + 概述句）
5. `IMPLEMENTATION_PLAN.md` 8.3 → ✅、追加顺序注记第三行
6. 三份 spec（8.5 / 8.4 / 8.6）追加兑现说明
7. `gofmt` 走 `$(go env GOROOT)/bin/gofmt -l -w`
8. 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./...`（零 FAIL）

## 9. 后续衔接提示（本轮不实施）

- **8.3a**：`add_node(name, fn, routers...)` → `NewPregelNode` + `NewTriggerChannel`；`add_edge` 的 barrier 分支对 `start` 中每个源节点调 `node.addRouter(NewBarrierRouter(barrier.Key()))`，静态分支调 `NewStaticRouter(...)`；`add_branch` → `NewConditionalRouter`。START/END 用 `NoopNodeFunc`。
- **8.3b**：`NodeTask` 是 `NodeInput` 的唯一构造点——`CreateInnerConfig(config)` 后若 `ParentNs` 非空则把 `Ns`/`ParentNs` 同时改写为 `parent:node:version`，`State` 恒 nil；调用 `node.fn(ctx, in)`；`error` 计入 `PendingNode`；`GraphInterrupt` 由 recover 判定为中断并非错误（`errors.As` 判 `*pregel.GraphInterrupt`，禁止 `r.(error)` 兜底，8.7 D3）；中断时**整段路由不执行**。
- **8.11**：`Vertex` 的执行体要包成 `NodeFunc`（`func(ctx, NodeInput) error`），放弃 Python 的 `__call__(state, config)` 签名。
