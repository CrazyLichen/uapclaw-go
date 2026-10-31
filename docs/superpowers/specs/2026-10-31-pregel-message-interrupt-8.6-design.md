# 8.6 Graph Message / Interrupt 设计文档

> 对应实现计划：领域八 8.6（IMPLEMENTATION_PLAN.md L506）
> Python 源码：`openjiuwen/core/graph/pregel/base.py` 第 10-43 行（`Interrupt` / `Message` / `TriggerMessage` / `BarrierMessage`）
> 前置依赖：8.3d（`config.go` / `constants.go`，已 ✅）
> 后续消费方：8.3（`Channel.Accept(msg Message)`）、8.4（`TriggerChannel`/`BarrierChannel` 的 `Kind()` 过滤）、8.5（三个 Router 构造消息）、8.3b（`NodeTask` 返回值、`PendingNode`）、8.3c（`ChannelManager.buffer` 重放）、8.9（`GraphState.pending_buffer` 快照）
> 章节顺序决策：**8.6 提前于 8.3/8.4/8.5**（已拍板方案 A）。理由：`Message` 是上述各节的共同类型，严格按编号先做 8.3 会因类型未定义无法编译

## 1. 流程位置与作用

Python 侧完整调用链（行号逐条核实）：

```
Gateway / IM ──E2A──▶ AgentServer ──▶ AgentSession / Workflow（8.14）
  └─ CompiledGraph._invoke ──▶ Pregel.run(config)                     engine.py:222
       │
       ├─ PregelLoop.init()                                           engine.py:33
       │    └─ 首步自触发：buffer_message(TriggerMessage(
       │           sender=graph.initial, target=graph.initial))       engine.py:59   ← 本节类型
       │       manager.flush()
       │
       └─ while run_step():                                           engine.py:86
            ├─ ready = manager.get_ready_nodes()                      engine.py:107
            ├─ manager.consume(node)  → ch.consume() 清空消息         engine.py:131
            ├─ executor.submit(node) → NodeTask.run()                 task.py:125
            │     ├─ 执行节点 func（内部可能 raise GraphInterrupt ← 8.7）
            │     ├─ for r in node.routers: msgs = await r.dispatch() task.py:152
            │     │     ├─ StaticRouter      → [TriggerMessage]*N     router.py:18
            │     │     ├─ ConditionalRouter → [TriggerMessage]*N     router.py:46
            │     │     └─ BarrierRouter     → [BarrierMessage]*N     router.py:56
            │     ├─ 逐条 trigger(EDGE_TRAVERSED, sender, target)     task.py:156-161
            │     └─ return self.messages                             task.py:163
            ├─ succeed_messages → manager.buffer_message(msg)         engine.py:154-156
            └─ manager.flush()                                        engine.py:157
                 ├─ ch = map_key_to_channel[msg.target]               channels.py:43
                 ├─ changed = ch.accept(msg)                          channels.py:47
                 │    ├─ TriggerChannel: isinstance(msg, TriggerMessage)  channels.py:113
                 │    └─ BarrierChannel: isinstance(msg, BarrierMessage)  channels.py:175
                 │         └─ received.add(msg.sender)  ← sender 是 OR 组判据
                 └─ any channel ready → 节点入就绪集（下一超步）      channels.py:57

中断侧（本节 Interrupt 的值语义）：
  WorkflowInteraction.wait_user_inputs ─▶ raise GraphInterrupt((Interrupt(
        value=OutputSchema(type=INTERACTION, ...)),))                 interaction.py:49
  WorkflowInteraction.user_latest_input ─▶ raise GraphInterrupt((Interrupt(
        value=..., resumable=True, ns=self._node_id),))               interaction.py:61-63 ← TypeError
       └─ NodeTask 捕获 GraphInterrupt → 当返回值（不抛出）            task.py:158-159
       └─ TaskExecutorPool._commit_failure → status=__interrupt__     task.py:36
       └─ PregelLoop._save_state_on_error 落检查点                    engine.py:186-197
       └─ Pregel.run 顶层 → return {"__interrupt__": e.value}         engine.py:275
       └─ Checkpointer（Go 侧 5.8 已实现 isWorkflowInterrupted）      ← 8.3d 已回填该字面量
```

**作用（5 条）**：

1. **超步之间唯一的调度信号载体**：Pregel 的控制平面完全由 `Message` 列表驱动；通道与节点函数都不传业务数据（`payload` 全链零读写，`grep` 确认 `openjiuwen/core/graph/` 内除 `base.py:33` 的赋值外无消费者，`vertex.py:607` 的 `.payload` 属于 `OutputSchema`，与本类无关）。
2. **类型即语义，决定通道收不收**：`TriggerMessage` = 1→N 激活，`BarrierMessage` = N→1 汇聚；`TriggerChannel` 只收前者、`BarrierChannel` 只收后者，靠 `isinstance` 双向过滤（`channels.py:113` / `channels.py:175`）。
3. **字段语义不对称**：`target` 是 **channel 路由键**（普通通道即节点名；Barrier 通道是 `barrier:a&b->c` 合成键，见 `channels.py:155-164`），`sender` 是 **Barrier OR 组成员标识**（`BarrierChannel.received` 存 sender，绝不存 target）。
4. **检查点可恢复的语义前提**：`engine.py:190` 把 `manager.buffer`（`List[Message]`）塞进 `GraphState.pending_buffer`（`store/base.py:36`），恢复时 `engine.py:55-56` 逐条 `buffer_message` 重放；`InMemoryStore` 用 `copy.deepcopy`（`inmemory.py:19` / `inmemory.py:23`）。因此 `Message` 必须能被整体复制，且 Python 侧从无就地修改（已 grep 确认无字段赋值）。
5. **`Interrupt` 与 `GraphInterrupt` 分工**：`Interrupt` 是**值对象**（随检查点持久化、由 `Pregel.run` 返回给上层），`GraphInterrupt` 是**控制流跳转手段**（异常/Go 侧 panic）。两者同在 `base.py` 但生命周期不同，故 `Interrupt` 归本节、`GraphInterrupt` 归 8.7。

## 2. 文件结构

```
internal/agentcore/graph/pregel/
├── doc.go             # 更新：目录树补 base.go，职责段补“调度信号与中断值”
├── base.go            # 新增：Message + MessageKind + 两个构造函数 + Kind() + Interrupt
├── base_test.go       # 新增
├── constants.go       # 8.3d 已有
├── constants_test.go  # 8.3d 已有
├── config.go          # 8.3d 已有
└── config_test.go     # 8.3d 已有
```

`base.go` 与 Python 单文件同构，后续由 **8.3**（`PregelNode` / `Channel` ABC）、**8.5**（`IRouter`）、**8.7**（`GraphInterrupt`）继续追加，不拆文件。

## 3. 类型设计

声明顺序遵循规范 2（结构体 → 枚举 → 常量 → 导出函数 → 非导出函数）：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// Message 超步之间传递的调度信号。
// Python: pregel/base.py:29-33 Message
type Message struct {
	// Sender 发送方节点名（Barrier 通道据此判断 OR 组是否已满足）
	Sender string
	// Target 目标通道的路由键（普通通道即节点名；Barrier 通道为 "barrier:a&b->c" 合成键）
	Target string
	// Payload 载荷。Python 声明了该形参（默认 None）但全部调用点均不传，实际恒为 nil；
	// 保留字段以对齐数据形状，当前无消费者
	Payload any
	// kind 消息种类，只能经 NewTriggerMessage / NewBarrierMessage 设置
	kind MessageKind
}

// Interrupt 图中断信号，值对象：随检查点持久化后由 Pregel.Run 返回给上层。
// Python: pregel/base.py:10-14 Interrupt
type Interrupt struct {
	// Value 中断携带的值（Go 侧实际装 InteractionOutputSchema）
	Value any
	// Resumable 是否可恢复中断。Python 仅在 interaction.py:62 以关键字传入，
	// 而其 __init__ 不接受该形参（运行时 TypeError），见 §7 R1
	Resumable bool
	// NS 命名空间（节点 ID），同上，仅 interaction.py:63 传入
	NS string
}

// ──────────────────────────── 枚举 ────────────────────────────

// MessageKind 消息种类判别式
type MessageKind string

const (
	// KindTrigger 1→N 激活消息
	KindTrigger MessageKind = "trigger"
	// KindBarrier N→1 汇聚消息
	KindBarrier MessageKind = "barrier"
)
```

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// NewTriggerMessage 构造激活消息。
// Python: TriggerMessage(sender, target)（base.py:36-38）
func NewTriggerMessage(sender, target string) Message {
	return Message{Sender: sender, Target: target, kind: KindTrigger}
}

// NewBarrierMessage 构造汇聚消息。
// Python: BarrierMessage(sender, target)（base.py:41-43）
func NewBarrierMessage(sender, target string) Message {
	return Message{Sender: sender, Target: target, kind: KindBarrier}
}

// Kind 返回消息种类。零值 Message 返回空串，此时 8.4 的两种通道都拒绝接收，
// 与 Python 基类 Message 不被任一 isinstance 命中完全等价。
func (m Message) Kind() MessageKind {
	return m.kind
}
```

### 3.1 关键取舍

| 取舍 | 决策 | 理由 |
|------|------|------|
| 继承 → 单结构体 | `Message` 一个结构体 + 判别式，`TriggerMessage`/`BarrierMessage` 降级为构造函数 | 三个 Python 类零行为纯数据；已拍板方案 A |
| 判别式取值 | 非导出字段 `kind` + 导出方法 `Kind()` | 包外无法构造“kind=trigger 却走 barrier 路径”的非法组合；8.4 通道经 `Kind()` 过滤，日志可直接打印 |
| 枚举底层类型 | `string`（`"trigger"` / `"barrier"`）而非 iota | 仓库已有 `ModalityKind`/`IndexTypeKind`/`StepKind` 等字符串枚举先例，无 iota 判别式先例；字符串在日志与 8.9 序列化打标签时可读 |
| 值类型而非指针 | 构造函数返回 `Message` 值 | `[]Message` 的 slice 拷贝即等价 `copy.deepcopy`（Python 侧无就地修改）；无 nil-interface / nil-pointer 陷阱 |
| 零值语义 | 零值 `Message` 的 `Kind()` 为空串，任何通道都不接受 | 无需 Python 里不存在的 `Unknown` 分支；等价于“基类 `Message` 实例传入时两种 isinstance 都失败”（`base.py` 内无任何 `Message(sender=...)` 直接构造点） |
| `payload` | 保留字段、恒 nil、构造函数不设该形参 | Python 全部调用点均不传；Go 无可选参数，调用方可构造后按需赋值；对齐数据形状优先 |

## 4. 同批必做：`session/interaction` 去重

`Interrupt` 迁入 pregel 后必须同步处理 `session/interaction` 的本地同名结构体，否则两份类型语义漂移：

| 改动 | 文件:行 | 说明 |
|------|---------|------|
| 删除本地 `Interrupt` 结构体定义 | `session/interaction/base.go:14-22` | 改为 import `graph/pregel` |
| `GraphInterrupt.Interrupts []Interrupt` 指向 `pregel.Interrupt` | `session/interaction/base.go:26-30`（字段在 `:28`） | **字段名与字段数不变**，`PanicGraphInterrupt(interrupts ...Interrupt)` 签名不变，消费者零改动 |
| 两处 `Interrupt{...}` 字面量加包名前缀 | `session/interaction/interaction.go:137`、`:169` | 内容不变：`:169` 的 `Resumable: true` / `NS: nodeID` 按 §7 R1 保留 |
| 测试字面量加包名 | `session/interaction/base_test.go:156` | 仅类型名称变更 |
| 断言包名变更 | `session/node_test.go:388-389` | 仅 `%T` 断言文本与类型断言 |

依赖方向：`session/interaction → graph/pregel`。`pregel` 当前只 import 标准库，不成环（8.3d spec §2 已核实 pregel 依赖闭包不含 session）。

**不采用类型别名**（`type Interrupt = pregel.Interrupt`）：仓库有“不重导出”先例（`common` 不重导出 `EmbeddingConfig`），别名会留下第二个可写类型名。

**8.7 交棒**：本节完成后，8.7 只剩两件事 —— `GraphInterrupt` 本体迁入 pregel、tracer 接线。顺带记录一处既有缺口：`session/tracer/handler.go:20` 的 `graphInterrupter` 接口要求 `isGraphInterrupt()` 方法，但 `interaction.GraphInterrupt` 从未实现它，导致 `handler.go:300` 的 `case graphInterrupter` 分支是死代码；8.7 迁移时应在 `pregel.GraphInterrupt` 上补该标记方法。

## 5. 回填清单

1. **`IMPLEMENTATION_PLAN.md` L506（8.6 行）**：☐ → ✅，产出列补 Go 事实（单结构体 + 非导出 `kind` + `Kind()`、`Interrupt` 三字段、interaction 去重、R1-R4 偏差编号、覆盖率数字），并附设计文档路径。
2. **`IMPLEMENTATION_PLAN.md` L507（8.7 行）**：补注 `⤴️ 8.6 已前移 Interrupt 与 GraphInterrupt.Interrupts 类型指向`。
3. **`doc.go`**：目录树新增 `base.go` 一行（测试文件按项目规则不入目录树），职责段补“超步调度信号与中断值对象”，`对应 Python 代码` 行补 `pregel/base.py`。
4. **8.3d spec §6 R4「后续消费清单」**：无需修改（其中 8.7 的“checkpointer 判中断”已在该节完成）。

## 6. 偏差归档

| 编号 | 偏差 | 缘由 | 审查口径 |
|------|------|------|---------|
| R1 | `Interrupt` 保留 `Value`/`Resumable`/`NS` 三字段，Python 仅 `value` | `interaction.py:61-63` 传了 `resumable`/`ns` 两个 Python 类型不接受的 kwarg（运行必抛 `TypeError`，且无测试覆盖）；作者意图明确，保留成本为零、不改任何执行流程。已拍板方案 A | 两字段当前零消费者，8.7 迁 `GraphInterrupt` 时不得删 |
| R2 | 新增 `MessageKind` 与 `Kind()` 方法（Python 靠 `isinstance`） | 单结构体建模（已拍板方案 A）必须有可读判别式；供 8.4 通道过滤、日志排障、8.9 序列化打标签 | 属 Go 新增导出 API，不计入对齐缺失 |
| R3 | `Message`/`TriggerMessage`/`BarrierMessage` 三类型合并为单结构体 | 三者零行为纯数据，`isinstance` 判别在 Go 侧无对应物；已拍板方案 A | 构造函数名保留 Python 类名，调用点可读性不降低 |
| R4 | 构造函数不设 `payload` 形参，字段恒 nil | Python 全部调用点均不传 payload；Go 无可选参数 | 若后续 Python 出现传 payload 的调用点，补 `SetPayload` 或在构造后赋值 |

## 7. 测试设计

`base_test.go`（目标覆盖率 100%，`-race` 干净）：

| 用例 | 断言 |
|------|------|
| `TestNewTriggerMessage` | `Sender`/`Target` 落地、`Kind() == KindTrigger`、`Payload == nil` |
| `TestNewBarrierMessage` | 同上，`Kind() == KindBarrier` |
| `TestMessage_Kind` | 表驱动三态：trigger / barrier / 零值空串 |
| `TestMessage_零值不被任何通道接受` | 零值 `Message` 的 `Kind()` 为空串，且既不等于 `KindTrigger` 也不等于 `KindBarrier`（用 8.4 的过滤条件形式表达，不依赖 8.4 代码） |
| `TestMessage_值拷贝独立` | `b := a` 后改 `b.Target` 不影响 `a`（锁死“值类型 ⇒ 快照安全”的设计前提） |
| `TestInterrupt_字段` | `Value`/`Resumable`/`NS` 三字段读写往返 |
| `TestMessageKind_字符串值` | 逐值断言 `"trigger"`/`"barrier"`，防字符串漂移（对齐 8.3d 常量测试风格） |
| Python 交叉验证（注释形式） | `BarrierChannel._make_router_key`（`channels.py:155-164`）产出的 `barrier:a&b->c` 作为 `Target` 的合法样例，写入测试 fixture 注释 |

## 8. 实施顺序建议（供 writing-plans 展开）

1. `base.go`：`Message` + `MessageKind` + 两个构造函数 + `Kind()` + `Interrupt`
2. `base_test.go`：上表用例，跑 `go test ./internal/agentcore/graph/pregel/ -cover`，确认 100%
3. `doc.go` 更新目录树与职责段
4. `session/interaction` 去重（4 个文件，纯类型来源变更）+ 跑 `go test ./internal/agentcore/session/...`
5. `IMPLEMENTATION_PLAN.md` 8.6 ✅ / 8.7 补注
6. 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./...`（零 FAIL）

## 9. 后续顺序提示（本轮不实施）

`task.py:38` 的 `PendingNode` 与 `engine.py:_save_state_on_error` 的 `create_state` 都落在 **8.9** 名下，但 8.3b（`NodeTask`/`TaskExecutorPool`）与 8.3c（`PregelLoop`）必须先有它们才能编译。建议届时把 8.9 拆成两批：

- **8.9a（提前到 8.3b 之前）**：`Store` 接口 + `PendingNode` + `GraphState` + `CreateState`
- **8.9b（留在原位）**：`InMemoryStore`（深拷贝 + ns 前缀删除）+ `serde` + 5.8 checkpointer 的 4 处 Delete 回填

此处仅登记，待 8.6 落地后单独讨论。
