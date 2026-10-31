# 8.7 GraphInterrupt 设计文档

> 对应实现计划：领域八 8.7（IMPLEMENTATION_PLAN.md L507）
> Python 源码：`openjiuwen/core/graph/pregel/base.py` 第 17-20 行（`GraphInterrupt`）
> 前置依赖：8.6（`Interrupt` 值对象 + `Message`，已 ✅）、8.3d（`TaskStatusInterrupt = "__interrupt__"` 常量，已 ✅）
> 后续消费方：8.3b（`NodeTask` recover 转返回值、`_commit_failure` 记 `PendingNode`）、8.3c（`Pregel.Run` 顶层转 `{"__interrupt__": ...}`）、8.11（`Vertex` 放行）、8.14（`_workflow` 取 `Interrupts[0].Value`）、tracer（`span.Status = INTERRUPTED`）
> 章节范围决策：**本节独立成批（方案 A）**，不并入 8.8（`GraphState`）或 8.9a（store 基元）。理由：本节是跨包迁移 + 死代码修复，改动面集中，独立审查闭环口径最清晰

## 1. 流程位置与作用

### 1.1 在 Agent 会话中的位置

`GraphInterrupt` 是**唯一一条从最深层组件穿透到引擎顶层的中断链路**，Python 侧逐跳（行号已逐条核实）：

```
WorkflowInteraction.wait_user_inputs                              interaction.py:40-50
WorkflowInteraction.user_latest_input                             interaction.py:52-64
  └─ raise GraphInterrupt((Interrupt(value=OutputSchema(
         type="__interaction__", index=idx, payload=InteractionOutput(...))),))
     ↑ 全仓唯一生产者：等不到用户输入时，把「暂停」变成控制流跳转
        │
        ▼
Vertex._run_executable                                           vertex.py:207-212   ← 8.11
  └─ except GraphInterrupt: log info "Interrupt to call node" → raise
     显式排在 BaseError / Exception 之前 ⇒ 不被错误包装、不重试
Vertex._run_executable_with_retry                                vertex.py:255-256   ← 8.11
  └─ except GraphInterrupt: raise（不套 timeout、不转 WORKFLOW_EXECUTION_TIMEOUT）
        │
        ▼
NodeTask.run                                                     task.py:172-174     ← 8.3b
  └─ except GraphInterrupt as e: return e        ← 异常转「返回值」
        │
        ▼
TaskExecutorPool.wait_all                                        task.py:40-90       ← 8.3b
  ├─ isinstance(res, GraphInterrupt) → _commit_failure          task.py:63-66
  ├─ _commit_failure: status = TASK_STATUS_INTERRUPT            task.py:36-38
  └─ 优先级：if first_err_exc: raise → elif interrupt_exc: raise task.py:87-90
        │
        ▼
PregelLoop.run_step                                              engine.py:62-79     ← 8.3c
  ├─ isinstance(e, GraphInterrupt) ⇒ 跳过 GRAPH_SUPER_STEP_ERROR 日志
  └─ 仍执行 _save_state_on_error（snapshot + pending_buffer + pending_node）engine.py:185-196
        │
        ▼
Pregel.run                                                       engine.py:261-280   ← 8.3c
  ├─ trigger(LOOP_FINISHED) + GRAPH_END(interrupted) 日志
  ├─ is_top_level → return {TASK_STATUS_INTERRUPT: e.value}      engine.py:275
  └─ 子图        → raise（继续向上传）
        │
        ▼
_workflow._execute_component                                     _workflow.py:119-133 ← 8.14
  └─ except GraphInterrupt as e: exception = e; return e.value[0].value   ← 取 [0].value
     finally: state.commit() + post_workflow_execute(..., exception=e)
        │
        ├─ Checkpointer.post_workflow_execute → isWorkflowInterrupted（Go 5.8/8.3d 已回填）
        ├─ Tracer TraceWorkflowHandler.on_invoke → span.status = INTERRUPTED  handler.py:315
        └─ LoopComp.on_invoke → except GraphInterrupt: raise（直通）           loop_comp.py:208
```

### 1.2 作用（4 条）

1. **控制流跳转手段，而非错误**：把「等用户输入」这一合法的非错误暂停，从节点函数深处跳到引擎顶层并落检查点。所有中间层都必须在 `except BaseError / Exception` **之前**先放行它（`vertex.py:207`、`loop_comp.py:208` 都如此），否则会被错误包装或重试逻辑吞掉。
2. **携带中断值**：`value` 是 `Interrupt` 的 tuple；`Interrupt.value` 装 `OutputSchema{type:"__interaction__", index, payload}`。顶层包成 `{"__interrupt__": value}` 供 checkpointer 与上层识别，工作流层再用 `[0].value` 取回。
3. **与错误互斥且优先级更低**：`wait_all` 中普通异常优先抛出，只在无普通异常时才上抛中断；`_commit_failure` 用 `PendingNode.status` 把 `__interrupt__` 与 `__error__` 分开——这直接决定恢复时是「重跑待恢复节点」还是「报错」。
4. **类型契约**：Python 声明 `Interrupt | tuple[Interrupt, ...]`（`base.py:18`），但唯二生产点都传一元 tuple、消费点都按 `[0]` 取用 ⇒ Go 用切片即可，无需 union。

### 1.3 Go 侧现状与缺口

| 项 | 位置 | 状态 |
|---|---|---|
| `GraphInterrupt{Interrupts []pregel.Interrupt}` | `session/interaction/base.go:17-20` | 临时寄居（注释写明本节迁出） |
| `PanicGraphInterrupt` | `session/interaction/base.go:82-84` | 与结构体同居；生产点 `interaction.go:138`、`:170` |
| `Error()` | 无 | Python 是 `Exception`（`super().__init__(str(value))`，`base.py:20`），Go 缺 |
| `isGraphInterrupt()` 标记方法 | 无 | `tracer/handler.go:19-21` 的接口要求它，`case graphInterrupter`（`:300`）**是死代码**；且按 §4.2 的 Go 规则**补了也不生效** |
| `interaction.InteractionOutputSchema` | `interaction.go:24-36` | 与 `stream.OutputSchema` 字段重复（§5） |
| 真实消费者 | 仅 tracer（经接口）+ 3 处测试 | 8.3b/8.3c/8.11/8.14 均为未来消费者 |

## 2. 文件结构与改动面

```
internal/agentcore/graph/pregel/
├── base.go           # 修改：追加 GraphInterrupt 结构体 + Error() + PanicGraphInterrupt；新增 import "fmt"
├── base_test.go      # 修改：新增本节用例（Error 格式、panic 值类型、参数展开）
├── doc.go            # 修改：目录树职责、包概述、分层纪律两条
└── (constants.go / config.go 不改)

internal/agentcore/session/interaction/
├── base.go           # 修改：删 GraphInterrupt + PanicGraphInterrupt，删 pregel import
├── base_test.go      # 修改：删 TestPanicGraphInterrupt（用例移交 pregel），该文件唯一的 pregel import 随之删除
├── interaction.go    # 修改：删 InteractionOutputSchema；两处 panic 改 pregel.PanicGraphInterrupt + stream.OutputSchema；新增 stream import
├── interaction_test.go # 修改：断言改 *pregel.GraphInterrupt，新增 pregel import，新增 Value 形状断言
└── doc.go            # 修改：目录树与包注释

internal/agentcore/session/tracer/
├── handler.go        # 修改：删 graphInterrupter 接口，case 改 *pregel.GraphInterrupt，新增 pregel import
└── handler_test.go   # 修改：删 testGraphInterrupt，用例改用真实 *pregel.GraphInterrupt

internal/agentcore/session/
├── node_test.go      # 修改：断言包名 interaction → pregel（新增 pregel import）
└── doc.go            # 修改：目录树与类型索引

IMPLEMENTATION_PLAN.md  # 修改：8.7 行 → ✅
```

## 3. 类型设计

声明顺序遵循规范 2（结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数）。`Error()` 是导出方法，与 `PanicGraphInterrupt` 同处「导出函数」区块（与 8.6 把 `Kind()` 放在构造函数之后一致），不单开「非导出函数」区块；本节无枚举与常量。

```go
package pregel

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// GraphInterrupt 图级中断，控制流跳转手段（Python 侧是 Exception）。
//
// 生命周期与 Interrupt 不同：Interrupt 是随检查点持久化的值对象，
// 本类型是一次执行内的控制流跳转——由交互层 panic 抛出，被节点任务恢复成
// 返回值，最终在引擎顶层转成 {"__interrupt__": Interrupts} 交给上层。
// Python: pregel/base.py:17-20
type GraphInterrupt struct {
	// Interrupts 中断信号列表。Python 类型注解为 Interrupt | tuple[Interrupt, ...]，
	// 但唯二生产点（interaction.py:49/61）均传一元 tuple、消费点按 [0] 取用，
	// 故 Go 用切片表达；空切片语义与 Python 空 tuple 一致（8.14 取 [0] 时须自防越界）。
	Interrupts []Interrupt
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PanicGraphInterrupt 触发图级中断 panic。
// panic 值恒为 *GraphInterrupt，消费者按指针断言。
// Python: raise GraphInterrupt((Interrupt(...),))
func PanicGraphInterrupt(interrupts ...Interrupt) {
	panic(&GraphInterrupt{Interrupts: interrupts})
}

// Error 实现 error 接口，对齐 Python Exception.__init__(str(value))（base.py:20）。
//
// 纪律：本方法**仅供日志与格式化**。任何层禁止用 r.(error) 判定中断——
// Python 的 except 顺序（GraphInterrupt 先于 BaseError/Exception）在 Go 侧
// 只能靠具体类型断言还原，一旦按 error 兜底就会把中断误判成普通错误并吞掉。
func (e *GraphInterrupt) Error() string {
	return fmt.Sprintf("%v", e.Interrupts)
}
```

### 3.1 关键取舍

| 取舍 | 决策 | 理由 |
|---|---|---|
| 是否实现 `Error()` | 实现，格式 `fmt.Sprintf("%v", e.Interrupts)` | Python 侧它就是 `Exception`，`str(e)` 恒可用；无 `Error()` 时日志打出 `&{[{...} true node]}` 结构体字面量，排障成本高。格式刻意**不做得更友好**，以保留「Python 的 `str(value)` 对人类无意义」这一事实（`Interrupt` 无 `__str__`，`str(tuple)` 是 `<... object at 0x...>`） |
| 指针还是值 | panic 值恒为 `*GraphInterrupt` | 与现状一致；`Error()` 定义在指针接收者上，值类型不满足 `error`，可少一类误判面 |
| 是否加 `isGraphInterrupt()` | **不加** | tracer 改直连后无接口需要它；且跨包未导出方法名不构成同一方法（§4.2），补了也无效 |
| 是否加 `FirstValue()` 之类的便捷访问器 | **不加**（YAGNI） | Python 侧就是裸索引 `e.value[0].value`（`_workflow.py:124`），唯一消费方 8.14 自己索引即可；空切片防护由 8.14 自行决定，避免在 8.7 凭空造一个语义承诺 |
| `Interrupts` 还是 `Value` | `Interrupts []Interrupt` | 对齐 8.6 已落地的字段名（消费者零改动）；`Value` 已被 `Interrupt.Value` 占用 |
| `Resumable` / `NS` 是否保留 | **一个不删** | 8.6 §R1 的书面承诺「8.7 迁 `GraphInterrupt` 时不得删」；本设计无删减理由 |
| pregel 内是否 recover / 打日志 | **不** | Python `base.py` 内零日志零捕获，捕获点全在上层（`task.py`/`engine.py`，分属 8.3b/8.3c） |

## 4. 调用侧改造

### 4.1 `session/interaction` 去重

| 改动 | 文件:行 | 说明 |
|---|---|---|
| 删 `GraphInterrupt` 结构体 | `base.go:14-20` | 连带删除 `pregel` import（该文件此后不再引用 pregel） |
| 删 `PanicGraphInterrupt` | `base.go:80-84` | 迁往 `pregel`；`AgentInterrupt` / `PanicAgentInterrupt` 不动（不属本节） |
| 两处 panic 加包名前缀 | `interaction.go:138-144`、`:170-178` | 改 `pregel.PanicGraphInterrupt(pregel.Interrupt{...})`；`Resumable: true` / `NS: nodeID` 原样保留 |
| 断言包名变更 | `node_test.go:388-389` | `*interaction.GraphInterrupt` → `*pregel.GraphInterrupt` |

**不留类型别名**（`type GraphInterrupt = pregel.GraphInterrupt`）：与 8.6 对 `Interrupt` 的处置一致（未在 interaction 重导出），仓库亦有「不重导出」先例（`common` 不重导出 `EmbeddingConfig`）。

依赖方向：`session/interaction → graph/pregel` 本已存在（8.6），本节只扩大使用面，不新增依赖。

### 4.2 tracer 改为直连（并撤销 8.6 交棒的标记方法方案）

**结论**：删除 `graphInterrupter` 接口（`handler.go:17-21`），`case graphInterrupter`（`:300`）改为 `case *pregel.GraphInterrupt`，新增 `graph/pregel` import。

**为什么 8.6 交棒的方案不可行**（Go 语言规则）：

```go
// package tracer
type graphInterrupter interface { isGraphInterrupt() }   // 等价于要求 tracer.isGraphInterrupt
// package pregel
func (e *GraphInterrupt) isGraphInterrupt() {}           // 提供的是 pregel.isGraphInterrupt
```

未导出方法名是**包限定**的，两个名字在类型系统里完全不同，因此 `*pregel.GraphInterrupt` **永远不满足** `tracer.graphInterrupter`。当前之所以「看起来能跑」，是因为假实现 `testGraphInterrupt`（`handler_test.go:24-27`）**恰好定义在 tracer 包内**——即该分支从未被真实业务类型触发过。

**为何直连安全**：

- `tracer → pregel` 无环：`pregel` 的依赖闭包只有标准库（8.3d spec §2、8.6 spec §4 均已核实），`pregel` 不 import `session/**`。
- 与 Python 完全一致：`handler.py:20` 就是直接 `from openjiuwen.core.graph.pregel import GraphInterrupt`。
- 语义更收敛：Python 只对 `GraphInterrupt` 特判，没有「任意中断类」概念；接口抽象在此处没有对应物。

**保留接口的替代方案（未采纳）**：把接口迁到 pregel（`pregel.Interrupter`）由本包类型实现。功能等价，但多一层无消费者的间接，且与 Python 不对齐。

## 5. 同批回填：`InteractionOutputSchema` → `stream.OutputSchema`

### 5.1 事实核对

| 层 | Python | Go（改造前） | Go（改造后） |
|---|---|---|---|
| 通用流信封 | `OutputSchema{type, index, payload: Any}`（`stream/base.py:30`） | `stream.OutputSchema{Type, Index, Payload any, IsLastSchema bool}`（`stream/base.go:32`） | 复用 |
| 交互业务负载 | `InteractionOutput{id, value}`（`interaction.py:27`） | `interaction.InteractionOutput{ID, Value any}` | 不变 |
| 中断值信封 | **复用** `OutputSchema` | `interaction.InteractionOutputSchema{Type, Index, Payload InteractionOutput}`（`interaction.go:29`） | **删除**，复用 `stream.OutputSchema` |

Python 侧本就是一个信封复用（`interaction.py:49` 直接构造 `OutputSchema`），Go 的专用结构体是 5.x 的有意偏差（换取 `Payload` 类型安全）。本节按「完全对齐 Python」口径回填。

### 5.2 安全性证据

- **无循环依赖**：`session/stream` 的全部内部依赖只有 `common/exception`、`common/logger`；`interaction` 当前不 import `stream`，新增后方向为 `interaction → stream`，与 `stream` 无反向边。
- **`IsLastSchema` 零副作用**：该字段是 Go 为 controller chunk 扩的（对齐 `ControllerOutputChunk.last_chunk`），而 `ToMap()` 明确只输出 `type/index/payload`（`stream/base.go:100-109`），`Validate()` 只看 `type` 非空与 `index >= 0`（`:134-146`）。作为中断值复用时恒为零值 `false`。
- **当前无消费方**：`InteractionOutputSchema` 全仓只被构造两次（`interaction.go:139`、`:171`），**零拆箱**——唯一消费方 8.14 尚未实现。

### 5.3 改造后形态

```go
pregel.PanicGraphInterrupt(pregel.Interrupt{
	Value: stream.OutputSchema{
		Type:    InteractionType,
		Index:   w.idx,
		Payload: payload, // InteractionOutput，装进 any
	},
})
```

8.14 消费时多一次断言：`gi.Interrupts[0].Value.(stream.OutputSchema).Payload.(interaction.InteractionOutput)`。与 Python `e.value[0].value` 的取值路径同构。

## 6. 回填清单（逐条，不丢旧记录）

| # | 文件:行 | 动作 |
|---|---|---|
| 1 | `IMPLEMENTATION_PLAN.md:507` | 8.7 ☐ → ✅；**保留**两个 ⤴️ 注记并改写为「已回填」；新增偏差编号 D1/D2/D3 与设计文档路径 |
| 2 | `interaction/base.go:15` | 「本类型本体 8.7 迁入该包」→ 迁移完成说明（本体已删除） |
| 3 | `interaction/doc.go:4,9,22` | 包注释两处 `GraphInterrupt` 改指 `pregel.GraphInterrupt`；目录树 `base.go` 行去掉 `GraphInterrupt`，`interaction.go` 行去掉 `InteractionOutputSchema` 并补 `InteractionOutput` |
| 4 | `session/doc.go:53,118,122` | 目录树与类型索引两处描述更新（`GraphInterrupt` 标注「定义于 graph/pregel」） |
| 5 | `pregel/base.go:27` | `Interrupt` 注释「（控制流跳转手段，8.7 迁入本包）」→ 完成时态，指向本包 `GraphInterrupt` |
| 6 | `pregel/doc.go` | 目录树 `base.go` 职责补 `GraphInterrupt`；包概述补「中断控制流」；分层纪律补两条（禁止 `r.(error)` 判中断、本包含 panic 类但仍是叶子包） |
| 7 | `tracer/handler.go:288` | 注释「支持 `*interaction.GraphInterrupt`」→ `*pregel.GraphInterrupt` |

**不改旧 spec**：`docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md` §L200（交棒指令）与 §R1（字段不删）保持原样，作为历史记录；本 spec §4.2 与 §7 D1 显式引用并给出结论。

## 7. 偏差归档

| 编号 | 偏差 | 缘由 | 审查口径 |
|---|---|---|---|
| D1 | 撤销 8.6 交棒的「为 `pregel.GraphInterrupt` 补 `isGraphInterrupt()`」方案，tracer 改直连 `*pregel.GraphInterrupt` | 未导出方法名包限定，跨包不构成同一方法，按原方案回填后分支仍是死代码（§4.2） | 计划表 8.7 行须写明「原方案不可行」，不得静默删掉 8.6 的注记 |
| D2 | 删除 `interaction.InteractionOutputSchema`，中断值改用 `stream.OutputSchema`（`Payload` 回到 `any`） | Python 复用同一个 `OutputSchema`；5.x 的专用结构体属有意偏差，本节按完全对齐回填 | 8.14 消费时多一次 `.Payload` 断言；`IsLastSchema` 恒 false 且不入 `ToMap` |
| D3 | `GraphInterrupt` 实现 `Error()` 但禁止被当作 `error` 处理 | Python 是 `Exception`，但 Go 侧它只经 panic 传播；提供 `Error()` 只为日志可读 | 任何 recover 点必须用 `case *pregel.GraphInterrupt`，spec 与 `doc.go` 双处写死 |

## 8. 风险与观察点

- **JSON 键大小写**：被删的 `InteractionOutputSchema` 带 `json:"type"` 等标签，`stream.OutputSchema` **无 json tag**。当前无直序列化路径（checkpointer 只判键存在，`ToMap()` 输出三键），故不改。观察点：若 8.14 需要把中断值直接 `json.Marshal` 到 E2A，届时给 `stream.OutputSchema` 补标签，不在本节扩大改动面。
- **`Error()` 诱发误判**：已列入 D3 纪律，8.3b/8.11 落地时逐点核对。
- **空 `Interrupts`**：`PanicGraphInterrupt()` 允许零参调用（Python 侧同样允许 `GraphInterrupt()`，但 `_workflow.py:124` 会 IndexError）。Go 不在 8.7 引入防护，由 8.14 自行决定；测试中保留一条「零参 panic 不崩」的用例，锁住「不在 panic 路径上多做校验」的决策。

## 9. 测试设计

`pregel/base_test.go`（该文件覆盖率目标 100%，与 8.6 口径一致，`-race` 干净）：

| 用例 | 断言 |
|---|---|
| `TestGraphInterrupt_Error` | 精确字符串断言（防格式漂移）：零元素 → `[]`；单元素 `Value:"test_value"` → `[{test_value false }]`；双元素 → 两元素按序以空格分隔，`Resumable`/`NS` 位置不变 |
| `TestPanicGraphInterrupt` | recover 值类型为 `*pregel.GraphInterrupt`；`len(Interrupts) == 1`；`Interrupts[0].Value` 原样落地（该用例从 `interaction/base_test.go:140` 迁入） |
| `TestPanicGraphInterrupt_多中断` | 变参展开为切片，顺序与实参一致 |
| `TestPanicGraphInterrupt_零参数` | panic 值 `Interrupts` 为空切片，`Error()` 不崩（§8 第 3 条的锁定用例） |
| `TestGraphInterrupt_Resumable与NS不丢` | 构造含 `Resumable: true` / `NS: "node_x"` 的中断，panic→recover 后两字段原值（守住 8.6 §R1 承诺） |

`interaction`：

| 用例 | 断言 |
|---|---|
| `base_test.go` | 删除 `TestPanicGraphInterrupt`（移交 pregel） |
| `interaction_test.go:106,142,160` | 断言改 `*pregel.GraphInterrupt` |
| `interaction_test.go`（新增） | `Interrupts[0].Value` 可断言为 `stream.OutputSchema`，且其 `Payload` 是 `InteractionOutput`、`Type == "__interaction__"`、`Index == w.idx` |
| `interaction_test.go:168` | 保留 `Resumable` 断言，取值路径改经 `pregel.GraphInterrupt` |

`tracer`：

| 用例 | 断言 |
|---|---|
| `handler_test.go:471-473` | 删 `testGraphInterrupt`，用例改用真实 `&pregel.GraphInterrupt{...}`；仍断言 `span.Status == NodeStatusInterrupted` 且 `span.Error == nil` |
| `handler_test.go`（已有，保留不动） | 反证：表驱动中「普通错误」用例（`fmt.Errorf("普通错误")`）仍走 `case error` 分支——直连改造后必须继续通过，防止把兜底分支吃掉 |

全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`，包覆盖率 ≥ 85%。

## 10. 实施顺序建议（供 writing-plans 展开）

1. `pregel/base.go`：`GraphInterrupt` + `Error()` + `PanicGraphInterrupt`（新增 `import "fmt"`）
2. `pregel/base_test.go`：§9 表 5 条用例，跑 `go test ./internal/agentcore/graph/pregel/ -cover` 确认 100%
3. `interaction`：删结构体与 helper、两处 panic 改造、`InteractionOutputSchema` → `stream.OutputSchema`；跑 `go test ./internal/agentcore/session/interaction/`
4. `tracer`：删接口 + 直连 + 测试改造；跑 `go test ./internal/agentcore/session/tracer/`
5. 文档：`pregel/doc.go`、`interaction/doc.go`、`session/doc.go`、`handler.go` 注释（§6 第 2-7 项）
6. `IMPLEMENTATION_PLAN.md` 8.7 → ✅（§6 第 1 项）
7. 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./...`（零 FAIL）

## 11. 后续路线（本节不实施，仅记录依赖结论）

```
8.7 → 8.3(PregelNode/Channel ABC) → 8.8(GraphState) → 8.4(channels) → 8.5(routers)
    → 8.3a(builder) → 8.9a(Store 接口/PendingNode/GraphState/CreateState，从 8.9 拆出提前)
    → 8.3b(task) → 8.3c(engine) → 8.9b(InMemoryStore/serde + 5.8 的 4 处 Delete 回填)
```

依据：`task.py:36-38` 的 `PendingNode` 与 `engine.py:191-198` 的 `create_state` 都落在 8.9 名下，但 8.3b/8.3c 无它们无法编译；8.6 spec §「后续依赖」已提出同一结论，本节只做确认（不改计划表其他行）。
