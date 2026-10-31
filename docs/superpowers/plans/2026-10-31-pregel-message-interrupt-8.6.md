# 8.6 Graph Message / Interrupt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/agentcore/graph/pregel` 包落地 Pregel 超步调度信号 `Message`（含 `TriggerMessage`/`BarrierMessage` 语义）与中断值对象 `Interrupt`，并把 `session/interaction` 里的同名 `Interrupt` 去重为引用 pregel 的单一定义。

**Architecture:** `Message` 用单结构体 + 非导出 `kind` 判别式建模（Python 的三层继承在此零行为，`isinstance` 判别在 Go 侧由 `Kind()` 取代），构造函数收口保证包外无法构造非法组合；`Interrupt` 用值结构体保留 Python 未声明但被 `interaction.py:61-63` 实际传入的 `Resumable`/`NS` 两字段（有意偏差 R1）。

**Tech Stack:** Go 1.x（标准库 `testing` 即可），包 `internal/agentcore/graph/pregel`，对齐 Python `openjiuwen/core/graph/pregel/base.py:10-43`。

**设计文档：** `docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md`

---

## 背景：本任务在全局中的位置

Pregel 图引擎的超步循环里，节点执行完由 Router 产出消息列表，`ChannelManager.flush()` 按 `msg.Target` 找到通道、调 `ch.accept(msg)`，通道用消息类型过滤后决定节点是否就绪。`Message` 是该控制平面唯一的载体，因此 8.3（`Channel.Accept`）、8.4（通道过滤）、8.5（Router 产消息）、8.3b（`NodeTask` 返回值）、8.3c（`ChannelManager.buffer`）、8.9（`GraphState.pending_buffer` 快照）全部依赖它，本节必须先落地。

`Interrupt` 是中断的**值对象**（随检查点持久化、由 `Pregel.Run` 顶层返回 `{"__interrupt__": value}`），它与 `GraphInterrupt`（控制流跳转手段，8.7 迁入本包）职责不同，故只做前者。

---

## 文件结构

| 文件 | 职责 | 动作 |
|------|------|------|
| `internal/agentcore/graph/pregel/base.go` | `Message` + `MessageKind` + 两个构造函数 + `Kind()` + `Interrupt` | 新建 |
| `internal/agentcore/graph/pregel/base_test.go` | 上者的单元测试，目标 100% 覆盖 | 新建 |
| `internal/agentcore/graph/pregel/doc.go` | 包文档：目录树 + 职责 + 对应 Python 路径 | 修改 |
| `internal/agentcore/session/interaction/base.go` | 删除本地 `Interrupt`；`GraphInterrupt.Interrupts` 指向 `pregel.Interrupt` | 修改 |
| `internal/agentcore/session/interaction/interaction.go` | 两处 `Interrupt{` 字面量加包名 | 修改 |
| `internal/agentcore/session/interaction/base_test.go` | 一处 `Interrupt{` 字面量加包名 | 修改 |
| `internal/agentcore/session/interaction/doc.go` | 目录树说明同步 | 修改 |
| `internal/agentcore/session/node_test.go` | `%T` 断言文本与类型断言随包名变更 | 修改 |
| `IMPLEMENTATION_PLAN.md` | 8.6 行 ✅ + 8.7 行补注 | 修改 |

---

### Task 1: `Message` + `MessageKind` + 构造函数 + `Kind()`

**Files:**
- Create: `internal/agentcore/graph/pregel/base.go`
- Create: `internal/agentcore/graph/pregel/base_test.go`

- [ ] **Step 1: 写失败测试（Message 全部用例）**

创建 `internal/agentcore/graph/pregel/base_test.go`：

```go
package pregel

import "testing"

// TestNewTriggerMessage 激活消息的字段与种类落地。
// Python: TriggerMessage(sender, target)（pregel/base.py:36-38）
func TestNewTriggerMessage(t *testing.T) {
	msg := NewTriggerMessage("nodeA", "nodeB")
	if msg.Sender != "nodeA" {
		t.Errorf("Sender = %q，期望 \"nodeA\"", msg.Sender)
	}
	if msg.Target != "nodeB" {
		t.Errorf("Target = %q，期望 \"nodeB\"", msg.Target)
	}
	if msg.Payload != nil {
		t.Errorf("Payload = %v，期望 nil（Python 调用点均不传 payload）", msg.Payload)
	}
	if msg.Kind() != KindTrigger {
		t.Errorf("Kind() = %q，期望 %q", msg.Kind(), KindTrigger)
	}
}

// TestNewBarrierMessage 汇聚消息的字段与种类落地。
// Python: BarrierMessage(sender, target)（pregel/base.py:41-43）
func TestNewBarrierMessage(t *testing.T) {
	msg := NewBarrierMessage("nodeA", "barrier:nodeA&nodeB->nodeC")
	if msg.Sender != "nodeA" {
		t.Errorf("Sender = %q，期望 \"nodeA\"", msg.Sender)
	}
	if msg.Target != "barrier:nodeA&nodeB->nodeC" {
		t.Errorf("Target = %q，期望 \"barrier:nodeA&nodeB->nodeC\"", msg.Target)
	}
	if msg.Payload != nil {
		t.Errorf("Payload = %v，期望 nil", msg.Payload)
	}
	if msg.Kind() != KindBarrier {
		t.Errorf("Kind() = %q，期望 %q", msg.Kind(), KindBarrier)
	}
}

// TestMessageKind_字符串值 判别式底层值逐值断言，防字符串漂移。
func TestMessageKind_字符串值(t *testing.T) {
	tests := []struct {
		name string
		kind MessageKind
		want string
	}{
		{"KindTrigger", KindTrigger, "trigger"},
		{"KindBarrier", KindBarrier, "barrier"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.kind) != tt.want {
				t.Errorf("%s = %q，期望 %q", tt.name, string(tt.kind), tt.want)
			}
		})
	}
}

// TestMessage_Kind 表驱动三态：trigger / barrier / 零值空串。
func TestMessage_Kind(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want MessageKind
	}{
		{"激活消息", NewTriggerMessage("a", "b"), KindTrigger},
		{"汇聚消息", NewBarrierMessage("a", "b"), KindBarrier},
		{"零值消息", Message{}, MessageKind("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.Kind(); got != tt.want {
				t.Errorf("Kind() = %q，期望 %q", got, tt.want)
			}
		})
	}
}

// TestMessage_零值不被任何通道接受
// 零值 Message 的 kind 为空串，既不等于 KindTrigger 也不等于 KindBarrier，
// 等价于 Python 基类 Message 实例不被任一 isinstance 命中（base.py 内无直接构造点）。
func TestMessage_零值不被任何通道接受(t *testing.T) {
	var msg Message
	if msg.Kind() != "" {
		t.Errorf("零值 Kind() = %q，期望空串", msg.Kind())
	}
	// 8.4 通道的过滤条件形式：Kind() == KindTrigger / Kind() == KindBarrier
	if msg.Kind() == KindTrigger {
		t.Error("零值不得被判为激活消息")
	}
	if msg.Kind() == KindBarrier {
		t.Error("零值不得被判为汇聚消息")
	}
}

// TestMessage_值拷贝独立 值类型语义：拷贝后修改副本不影响原件，
// 这是 GraphState.pending_buffer 快照可直接用 slice 拷贝的前提。
func TestMessage_值拷贝独立(t *testing.T) {
	origin := NewTriggerMessage("nodeA", "nodeB")
	copied := origin
	copied.Target = "nodeC"
	copied.Payload = "mutated"
	if origin.Target != "nodeB" {
		t.Errorf("原件 Target 被副本修改：%q", origin.Target)
	}
	if origin.Payload != nil {
		t.Errorf("原件 Payload 被副本修改：%v", origin.Payload)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' || true
go test ./internal/agentcore/graph/pregel/ -run 'TestNewTriggerMessage|TestNewBarrierMessage|TestMessageKind_字符串值|TestMessage_Kind|TestMessage_零值不被任何通道接受|TestMessage_值拷贝独立' -v
```

预期：编译失败，`undefined: NewTriggerMessage`（以及 `undefined: Message`、`undefined: KindTrigger` 等）。

- [ ] **Step 3: 写最小实现**

创建 `internal/agentcore/graph/pregel/base.go`：

```go
package pregel

// ──────────────────────────── 结构体 ────────────────────────────

// Message 超步之间传递的调度信号。
//
// Python 用三层继承表达（Message → TriggerMessage / BarrierMessage），
// 三者均零行为纯数据，仅靠 isinstance 判别，故 Go 侧合并为单结构体 + 判别式，
// 由 NewTriggerMessage / NewBarrierMessage 收口构造。
// Python: pregel/base.py:29-43
type Message struct {
	// Sender 发送方节点名（Barrier 通道据此判断 OR 组是否已满足）
	Sender string
	// Target 目标通道的路由键（普通通道即节点名；Barrier 通道为 "barrier:a&b->c" 合成键）
	Target string
	// Payload 载荷。Python 声明了该形参（默认 None）但全部调用点均不传，实际恒为 nil；
	// 保留字段以对齐数据形状，当前无消费者
	Payload any
	// kind 消息种类，只能经 NewTriggerMessage / NewBarrierMessage 设置，
	// 包外无法构造非法组合
	kind MessageKind
}

// ──────────────────────────── 枚举 ────────────────────────────

// MessageKind 消息种类判别式。
//
// 零值（空串）表示未设置种类：此时任何通道都拒绝接收，等价于 Python 基类
// Message 实例不被任一 isinstance 命中。Python 无此概念（靠 isinstance）。
type MessageKind string

const (
	// KindTrigger 1→N 激活消息
	KindTrigger MessageKind = "trigger"
	// KindBarrier N→1 汇聚消息
	KindBarrier MessageKind = "barrier"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTriggerMessage 构造激活消息（1→N）。
// Python: TriggerMessage(sender, target)
func NewTriggerMessage(sender, target string) Message {
	return Message{Sender: sender, Target: target, kind: KindTrigger}
}

// NewBarrierMessage 构造汇聚消息（N→1）。
// Python: BarrierMessage(sender, target)
func NewBarrierMessage(sender, target string) Message {
	return Message{Sender: sender, Target: target, kind: KindBarrier}
}

// Kind 返回消息种类。
func (m Message) Kind() MessageKind {
	return m.kind
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestNewTriggerMessage|TestNewBarrierMessage|TestMessageKind_字符串值|TestMessage_Kind|TestMessage_零值不被任何通道接受|TestMessage_值拷贝独立' -v
```

预期：`PASS`，5 个测试（含 2 个子测试）全绿。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && gofmt -l internal/agentcore/graph/pregel/base.go internal/agentcore/graph/pregel/base_test.go
git add internal/agentcore/graph/pregel/base.go internal/agentcore/graph/pregel/base_test.go
git commit -m "feat(8.6): 新增 Pregel Message 调度信号与 MessageKind 判别式"
```

预期：`gofmt -l` 无输出；提交后 `git show --stat --oneline HEAD` 只含这 2 个文件。

---

### Task 2: `Interrupt` 值对象

**Files:**
- Modify: `internal/agentcore/graph/pregel/base.go`
- Modify: `internal/agentcore/graph/pregel/base_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/agentcore/graph/pregel/base_test.go` 末尾追加：

```go
// TestInterrupt_字段 三字段读写往返。
// Python 声明仅有 value（pregel/base.py:10-14），Resumable/NS 为有意偏差 R1：
// interaction.py:61-63 实际以关键字传入了这两个 Python 不接受的参数。
func TestInterrupt_字段(t *testing.T) {
	it := Interrupt{
		Value:     "output_schema_stub",
		Resumable: true,
		NS:        "nodeA",
	}
	if it.Value != "output_schema_stub" {
		t.Errorf("Value = %v，期望 \"output_schema_stub\"", it.Value)
	}
	if !it.Resumable {
		t.Error("Resumable 应为 true")
	}
	if it.NS != "nodeA" {
		t.Errorf("NS = %q，期望 \"nodeA\"", it.NS)
	}
}

// TestInterrupt_零值 零值 Interrupt 各字段为 nil/false/空串，便于直接构造占位。
func TestInterrupt_零值(t *testing.T) {
	var it Interrupt
	if it.Value != nil {
		t.Errorf("Value = %v，期望 nil", it.Value)
	}
	if it.Resumable {
		t.Error("Resumable 零值应为 false")
	}
	if it.NS != "" {
		t.Errorf("NS = %q，期望空串", it.NS)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestInterrupt_' -v
```

预期：编译失败，`undefined: Interrupt`。

- [ ] **Step 3: 写最小实现**

在 `internal/agentcore/graph/pregel/base.go` 的结构体区块内、`Message` 之后追加（枚举声明之后仍需保持在 `MessageKind` 之前，因此插在 `Message` 的右花括号之后）：

```go
// Interrupt 图中断信号，值对象：随检查点持久化后由 Pregel.Run 返回给上层
// （顶层返回 {"__interrupt__": value}）。
//
// 与 GraphInterrupt（控制流跳转手段，8.7 迁入本包）职责不同：本类型只承载中断值。
// Python: pregel/base.py:10-14
type Interrupt struct {
	// Value 中断携带的值（Go 侧实际装 InteractionOutputSchema）
	Value any
	// Resumable 是否可恢复中断。Python 类型声明的 __init__ 不接受该形参，
	// 但 interaction.py:62 以关键字传入（运行时 TypeError），保留以承载作者意图
	Resumable bool
	// NS 命名空间（节点 ID），同上，interaction.py:63 以关键字传入
	NS string
}
```

- [ ] **Step 4: 跑测试确认通过并检查覆盖率**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -cover -v
```

预期：`PASS`，`coverage: 100.0% of statements`（现有 `config.go`/`constants.go` 已 100%）。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && gofmt -l internal/agentcore/graph/pregel/base.go internal/agentcore/graph/pregel/base_test.go
git add internal/agentcore/graph/pregel/base.go internal/agentcore/graph/pregel/base_test.go
git commit -m "feat(8.6): 新增 Pregel Interrupt 中断值对象（保留 Resumable/NS 有意偏差 R1）"
```

---

### Task 3: `doc.go` 同步

**Files:**
- Modify: `internal/agentcore/graph/pregel/doc.go`

- [ ] **Step 1: 更新包文档**

把 `doc.go` 首段第二句与目录树改为下列内容（保留原有 package 行与注释格式）：

```go
// Package pregel 提供 Pregel 图引擎的配置、常量、超步调度信号与中断值。
//
// 本包是领域八的编译期最底层：构图（8.1）、节点与通道抽象（8.3）、
// 调度引擎（8.3b/8.3c）、通道管理（8.4）、顶点（8.11）、工作流（8.14）
// 均依赖本包提供的保留节点名、终止态键、递归预算、配置归一化能力，
// 以及超步间传递调度消息的 Message 与承载中断值的 Interrupt。
//
// 配置以值类型结构体表达：字段零值等价于 Python TypedDict(total=False)
// 的「键缺失/None」，因此不存在指针字段，调用方也无须判空。
//
// 文件目录：
//
//	pregel/
//	├── doc.go         # 包文档
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
//
// 对应 Python 代码：openjiuwen/core/graph/pregel/base.py · config.py · constants.py
package pregel
```

- [ ] **Step 2: 编译并跑包测试**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/graph/pregel/ && go test ./internal/agentcore/graph/pregel/ -cover
```

预期：`ok`，覆盖率 100.0%。

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uapclaw-gateway && gofmt -l internal/agentcore/graph/pregel/doc.go
git add internal/agentcore/graph/pregel/doc.go
git commit -m "docs(8.6): pregel 包文档补 base.go 与调度信号职责"
```

---

### Task 4: `session/interaction` 的 `Interrupt` 去重

**Files:**
- Modify: `internal/agentcore/session/interaction/base.go:1-33`
- Modify: `internal/agentcore/session/interaction/interaction.go:137`、`:169`
- Modify: `internal/agentcore/session/interaction/base_test.go:156`
- Modify: `internal/agentcore/session/interaction/doc.go`
- Modify: `internal/agentcore/session/node_test.go:388-389`

- [ ] **Step 1: 先跑一次基线测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/session/... 2>&1 | tail -20
```

预期：全部 `ok`（记录基线，便于改动后对比）。

- [ ] **Step 2: `base.go` 删除本地 `Interrupt` 并改指向**

删除 `internal/agentcore/session/interaction/base.go` 第 13-22 行的整个 `Interrupt` 结构体（含 doc 注释），import 区块追加：

```go
	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
```

`GraphInterrupt` 结构体改为：

```go
// GraphInterrupt 图级中断异常，通过 panic 传播。
// Interrupt 类型来自 graph/pregel（8.6 起单一来源），本类型本体 8.7 迁入该包。
// Python: openjiuwen/core/graph/pregel/base.py (GraphInterrupt)
type GraphInterrupt struct {
	// Interrupts 中断信号列表
	Interrupts []pregel.Interrupt
}
```

`PanicGraphInterrupt` 签名改为：

```go
// PanicGraphInterrupt 触发图级中断 panic。
// Python: raise GraphInterrupt(...)
func PanicGraphInterrupt(interrupts ...pregel.Interrupt) {
	panic(&GraphInterrupt{Interrupts: interrupts})
}
```

- [ ] **Step 3: `interaction.go` 两处字面量加包名**

`internal/agentcore/session/interaction/interaction.go`：import 区块追加同一行 `pregel` 包引用，然后把 137 行与 169 行的 `PanicGraphInterrupt(Interrupt{` 改成 `PanicGraphInterrupt(pregel.Interrupt{`。其余（含 169 行附近的 `Resumable: true`、`NS: nodeID`）一律不动。

- [ ] **Step 4: 测试文件同步**

`internal/agentcore/session/interaction/base_test.go`：import 区块追加 `pregel` 包引用，156 行改为：

```go
	PanicGraphInterrupt(pregel.Interrupt{Value: "test_value"})
```

`internal/agentcore/session/node_test.go`：388-389 行改为

```go
		if _, ok := r.(*interaction.GraphInterrupt); !ok {
			t.Fatalf("期望 *interaction.GraphInterrupt，得到 %T", r)
```

（第二行只是断言文本，若已如此则不变。）

- [ ] **Step 5: `doc.go` 同步目录树说明**

`internal/agentcore/session/interaction/doc.go` 目录树中 `base.go` 一行改为：

```go
//	├── base.go               # ExecutableIDProvider 类型别名（→ interfaces 包）+ BaseInteraction + GraphInterrupt（Interrupt 引用 graph/pregel）+ AgentInterrupt + 常量 + 依赖接口
```

- [ ] **Step 6: 跑测试确认无回归**

```bash
cd /home/opensource/uapclaw-gateway && gofmt -l internal/agentcore/session/interaction/ internal/agentcore/session/node_test.go
go build ./... && go test ./internal/agentcore/session/... ./internal/agentcore/graph/... 2>&1 | tail -20
```

预期：`gofmt -l` 无输出；两族包全部 `ok`。若出现 `import cycle not allowed`，说明 pregel 反向引用了 session —— 立即停下检查，本设计不允许多加任何 import。

- [ ] **Step 7: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/session/interaction/base.go internal/agentcore/session/interaction/interaction.go internal/agentcore/session/interaction/base_test.go internal/agentcore/session/interaction/doc.go internal/agentcore/session/node_test.go
git commit -m "refactor(8.6): session/interaction 的 Interrupt 改用 graph/pregel 单一来源"
```

- [ ] **Step 8: 确认只有预期文件进入这一笔提交**

```bash
cd /home/opensource/uapclaw-gateway && git show --stat --oneline HEAD
```

预期：`5 files changed`，无其他包文件被卷入（若发现多余文件，`git reset --soft HEAD~1` 后重新显式 add）。

---

### Task 5: 计划表回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:506`（8.6 行）
- Modify: `IMPLEMENTATION_PLAN.md:507`（8.7 行）

- [ ] **Step 1: 替换 8.6 行**

把第 506 行整行替换为（覆盖率数字以 Step 3 实测值填写）：

```markdown
| 8.6 | ✅ | Graph Message / Interrupt | ✅ `Message`(单结构体替代 Python 三层继承，`Sender`/`Target`/`Payload`/非导出 `kind`) + `MessageKind`(string 枚举 trigger/barrier，零值空串≡Python 基类被两种 isinstance 拒绝) + `NewTriggerMessage`/`NewBarrierMessage`(构造函数收口防非法组合) + `Kind()`(供 8.4 通道过滤/日志/8.9 打标签) + `Interrupt`(Value/Resumable/NS 三字段，R1 有意偏差：Python 仅声明 value 但 interaction.py:62 实际传入另两参且必抛 TypeError)；✅ 同批去重 `session/interaction` 本地 `Interrupt`（4 文件纯类型来源变更，`GraphInterrupt.Interrupts` 字段名不变故消费者零改动）；R1-R4 偏差已归档 spec §6；base.go 覆盖率 100%。设计：docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md | `openjiuwen/core/graph/pregel/base.py`(10-43) |
```

- [ ] **Step 2: 8.7 行补注**

在第 507 行（8.7 GraphInterrupt）的"内容"列开头插入：

```markdown
⤴️ 8.6 已前移 `Interrupt` 定义与 `GraphInterrupt.Interrupts` 的类型指向（本行只剩 `GraphInterrupt` 本体迁移 + tracer 接线；迁移时须为 `pregel.GraphInterrupt` 补 `isGraphInterrupt()` 标记方法，否则 `session/tracer/handler.go:300` 的中断分支是死代码）；
```

- [ ] **Step 3: 核对实测覆盖率后提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -cover
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(8.6): 计划表标记完成并交代 8.7 交棒点"
```

预期：覆盖率为 100.0%，与 Step 1 写入的数字一致；若不一致，先改数字再提交。

---

### Task 6: 全量回归与收尾

**Files:** 无（仅验证）

- [ ] **Step 1: 检查无残留 go 进程**

```bash
pgrep -f 'go (build|test)' || echo "无残留进程"
```

预期：输出 `无残留进程`；若有输出，先 `pkill -f 'go test'` 再继续。

- [ ] **Step 2: 全量回归（必须带 CI 同款构建标签）**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./... 2>&1 | grep -v "^ok" | tail -40
```

预期：无 `FAIL` 行（`ok` 行已被过滤）。注意不要漏 `-tags "sqlite_fts5 test"`，否则 memory 包 FTS 用例会假失败。

- [ ] **Step 3: 确认提交序列**

```bash
cd /home/opensource/uapclaw-gateway && git log --oneline -6 && git status --short
```

预期：7 笔提交（spec + 本 plan + Task1-5），工作区干净。若 `git status` 有残留改动，逐一核对是否属于本任务，非本任务的改动**不要**顺手提交。

---

## 审查口径（供实施后自查）

| 检查项 | 期望 |
|--------|------|
| 声明顺序 | `base.go` 内：结构体（`Message`、`Interrupt`）→ 枚举（`MessageKind` + 常量）→ 导出函数 → 非导出函数，段间有分隔注释 |
| 注释语言 | 全中文，`Python:` 前缀标注对应源 |
| 日志 | 本节零日志（Python `base.py` 无任何 logger 调用，对齐） |
| 覆盖率 | `pregel` 包 100.0% |
| 依赖方向 | `session/interaction → graph/pregel` 单向；`pregel` import 仅标准库 |
| 断言位置 | `Interrupt.Value` 的类型断言只允许发生在 session 层；`pregel` 内零断言、零 `session/**` import（验收命令：`go list -f '{{join .Imports "\n"}}' ./internal/agentcore/graph/pregel/ \| grep session` 应无输出；不要用 `grep session/ *.go`，注释里提到该路径是正常的） |
| 偏差 | R1（Interrupt 三字段）、R2（Kind() 新增）、R3（单结构体）、R4（构造函数不设 payload 形参）四项在 spec §6 有档；`Value` 保持 `any` 的分层依据见 spec §3.2 |
