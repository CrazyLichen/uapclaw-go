# 8.5 IRouter 三实现 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/agentcore/graph/pregel` 包落地 Pregel 路由层——`Router` 接口与 `StaticRouter`／`BarrierRouter`／`ConditionalRouter` 三个实现，把「节点执行完成后激活哪些下游节点」翻译成 8.6 已完成的 `Message` 列表。

**Architecture:** 三个 Python 类各自零状态、纯函数式（入参只有 `source_node`），Go 侧保持三个独立类型而不合并（与 8.6 合并 `Message` 的判据不同：那里是零行为纯数据，这里是构造签名相同但产出 `kind` 不同）。`ConditionalRouter` 的 selector 用 `Selector` 接口 + `SelectorFunc` 适配器静态建模，**不复刻** Python 的「按形参名嗅探 `state`」——Go reflect 取不到形参名，且该值恒 `nil` 且全链零读取，两条分支行为等价。

**Tech Stack:** Go 1.x（标准库 `context`／`fmt`／`testing`），包 `internal/agentcore/graph/pregel`，对齐 Python `openjiuwen/core/graph/pregel/router.py`（56 行）+ `pregel/base.py:23-26`。

**设计文档：** `docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md`

---

## 背景：本任务在全局中的位置

Pregel 是超步调度模型。`PregelLoop.run_step` 每轮取就绪节点 → `NodeTask.run` 执行节点函数 → **节点函数返回后遍历 `node.routers` 调 `dispatch(source_node)`**（`task.py:156-160`）→ 汇总消息 → `ChannelManager.flush` 按 `msg.Target` 找通道、`accept` 按 `Kind()` 过滤 → 决定下一超步的 `active_nodes`。

因此 Router 是「节点跑完 → 下一批节点被激活」之间唯一的决策点，也是 `Message` 的唯一生产者（`Sender=source_node`、`Target=目标路由键`），8.3b 的 `EDGE_TRAVERSED` 事件直接读这两个字段。

**中断旁路（硬约束，8.3b 落地时必须保持）**：`NodeTask.run` 捕到 `GraphInterrupt` 就把异常当返回值交出（`task.py:172-174`），路由整段不执行，中断不产生任何消息。

**章节顺序**：本节**前移到 8.3 之前**（已拍板）。8.3 的 `PregelNode` 字段是 `func + routers []IRouter`，必须先有 Router 接口；而本节只依赖已完成的 8.6 `Message` + 标准库。新顺序：`8.5 → 8.3 → 8.8 → 8.4 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b`。

---

## 文件结构

| 文件 | 职责 | 动作 |
|------|------|------|
| `internal/agentcore/graph/pregel/router.go` | `Router` 接口 + `Selector`/`SelectorFunc` + 三实现 + `buildTriggerMessages` | 新建 |
| `internal/agentcore/graph/pregel/router_test.go` | 上者单元测试，目标 `router.go` 覆盖率 100% | 新建 |
| `internal/agentcore/graph/pregel/doc.go` | 目录树 + 包概述 + 对应 Python 路径 | 修改 |
| `IMPLEMENTATION_PLAN.md` | 8.5 行 ✅ + 领域八表格后的顺序注记行 | 修改 |
| `docs/superpowers/specs/2026-10-31-graph-interrupt-8.7-design.md` | §11 追加顺序调整行（原文不动） | 修改 |

**最终 `router.go` 的声明顺序**（规范 2，实施中每个 Task 只往对应区块插入，不做后续搬家）：

```
结构体：Router → Selector → SelectorFunc → StaticRouter → BarrierRouter → ConditionalRouter
全局变量：4 条编译期接口断言
导出函数：NewStaticRouter → NewBarrierRouter → NewConditionalRouter
          → SelectorFunc.Select → StaticRouter.Dispatch → BarrierRouter.Dispatch → ConditionalRouter.Dispatch
非导出函数：buildTriggerMessages
```

---

### Task 1: `Router` 接口 + `Selector` / `SelectorFunc`

**Files:**
- Create: `internal/agentcore/graph/pregel/router.go`
- Create: `internal/agentcore/graph/pregel/router_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/graph/pregel/router_test.go`：

```go
package pregel

import (
	"context"
	"testing"
)

// TestSelectorFunc 函数适配器把 ctx 与 state 原样转发给被适配的函数。
// Python: 无对应物（Python 直接收 callable，由 router.py:39-43 判断是否协程）
func TestSelectorFunc(t *testing.T) {
	type ctxKey string
	key := ctxKey("trace")
	ctx := context.WithValue(context.Background(), key, "abc")

	var gotState any = "sentinel"
	sel := SelectorFunc(func(gotCtx context.Context, state any) ([]string, error) {
		if gotCtx.Value(key) != "abc" {
			t.Errorf("ctx 未透传：期望 \"abc\"")
		}
		gotState = state
		return []string{"D"}, nil
	})

	targets, err := sel.Select(ctx, nil)
	if err != nil {
		t.Fatalf("Select 返回错误：%v", err)
	}
	if len(targets) != 1 || targets[0] != "D" {
		t.Errorf("targets = %v，期望 [D]", targets)
	}
	if gotState != nil {
		t.Errorf("state = %v，期望 nil", gotState)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run TestSelectorFunc -v 2>&1 | tail -5
```

预期：编译失败，含 `undefined: SelectorFunc`（`Router`／`Selector` 也尚未定义）。

- [ ] **Step 3: 写最小实现**

创建 `internal/agentcore/graph/pregel/router.go`：

```go
package pregel

import "context"

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

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：SelectorFunc 必须满足 Selector。
var _ Selector = SelectorFunc(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// Select 实现 Selector：调用被适配的函数。
func (f SelectorFunc) Select(ctx context.Context, state any) ([]string, error) {
	return f(ctx, state)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run TestSelectorFunc -v 2>&1 | tail -5
```

预期：`--- PASS: TestSelectorFunc`、`PASS`。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && $(go env GOROOT)/bin/gofmt -l -w internal/agentcore/graph/pregel/router.go internal/agentcore/graph/pregel/router_test.go
git add internal/agentcore/graph/pregel/router.go internal/agentcore/graph/pregel/router_test.go
git commit -m "feat(8.5): pregel Router/Selector 接口与 SelectorFunc 适配器"
```

预期：`gofmt -l` 无输出（无待格式化文件）。

---

### Task 2: `StaticRouter` + `buildTriggerMessages`

**Files:**
- Modify: `internal/agentcore/graph/pregel/router.go`
- Modify: `internal/agentcore/graph/pregel/router_test.go`

- [ ] **Step 1: 写失败测试**

在 `router_test.go` **末尾追加**：

```go
// TestStaticRouter_零目标 空 targets 产出非 nil 空切片，等价「边终止」。
// Python: StaticRouter([]) 用于 END 节点（tests/unit_tests/core/graph/test_pregel.py:75）
func TestStaticRouter_零目标(t *testing.T) {
	msgs, err := NewStaticRouter().Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if msgs == nil {
		t.Fatalf("msgs = nil，期望非 nil 空切片（对齐 Python 的 []）")
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d，期望 0", len(msgs))
	}
}

// TestStaticRouter_单目标 1→1 的字段填法。
// Python: router.py:17-18
func TestStaticRouter_单目标(t *testing.T) {
	msgs, err := NewStaticRouter("B").Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d，期望 1", len(msgs))
	}
	if msgs[0].Sender != "A" {
		t.Errorf("Sender = %q，期望 \"A\"", msgs[0].Sender)
	}
	if msgs[0].Target != "B" {
		t.Errorf("Target = %q，期望 \"B\"", msgs[0].Target)
	}
	if msgs[0].Kind() != KindTrigger {
		t.Errorf("Kind() = %q，期望 %q", msgs[0].Kind(), KindTrigger)
	}
}

// TestStaticRouter_多目标保序 1→N 的消息顺序与入参顺序逐位一致。
// Python: 列表推导保序（router.py:18）
func TestStaticRouter_多目标保序(t *testing.T) {
	targets := []string{"a", "b", "c", "d", "e"}
	msgs, err := NewStaticRouter(targets...).Dispatch(context.Background(), "src")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != len(targets) {
		t.Fatalf("len(msgs) = %d，期望 %d", len(msgs), len(targets))
	}
	for i, want := range targets {
		if msgs[i].Target != want {
			t.Errorf("msgs[%d].Target = %q，期望 %q", i, msgs[i].Target, want)
		}
		if msgs[i].Sender != "src" {
			t.Errorf("msgs[%d].Sender = %q，期望 \"src\"", i, msgs[i].Sender)
		}
	}
}

// TestNewStaticRouter_复制入参 构造后调用方改原切片不影响实例（偏差 D5）。
// Python: self.targets = targets 存引用（router.py:15），Go 侧收紧
func TestNewStaticRouter_复制入参(t *testing.T) {
	targets := []string{"A", "B"}
	r := NewStaticRouter(targets...)
	targets[0] = "X"
	targets[1] = "Y"

	msgs, err := r.Dispatch(context.Background(), "src")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d，期望 2", len(msgs))
	}
	if msgs[0].Target != "A" || msgs[1].Target != "B" {
		t.Errorf("targets 被外部改写：%q, %q，期望 \"A\", \"B\"", msgs[0].Target, msgs[1].Target)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run TestStaticRouter -v 2>&1 | tail -5
```

预期：编译失败，含 `undefined: NewStaticRouter`。

- [ ] **Step 3: 写最小实现**

在 `router.go` 的 `// ──────────────────────────── 全局变量 ────────────────────────────` 之前**插入**结构体（即 `SelectorFunc` 定义之后）：

```go
// StaticRouter 静态扇出（1→N）。
//
// targets 为空时产出零条消息，等价「边终止」（Python 测试用 StaticRouter([])
// 表示 END 节点）。构造时复制入参切片，调用方后续改切片不影响本实例。
// Python: pregel/router.py:11-18
type StaticRouter struct {
	// targets 目标路由键列表（顺序即消息产出顺序）
	targets []string
}
```

在全局变量区块的断言下方**追加**：

```go
// 编译期接口断言：StaticRouter 必须满足 Router。
var _ Router = (*StaticRouter)(nil)
```

在 `// Select 实现 Selector` 之前**插入**构造函数：

```go
// NewStaticRouter 构造静态扇出路由。
// Python: StaticRouter(targets)（router.py:14-15）
func NewStaticRouter(targets ...string) *StaticRouter {
	return &StaticRouter{targets: append([]string(nil), targets...)}
}
```

在文件**末尾追加**（新建「非导出函数」区块）：

```go
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

在 `Selector` 的 `Select` 方法之后、文件末尾的「非导出函数」区块之前**插入** `Dispatch`：

```go
// Dispatch 产出 1→N 触发消息。
// Python: router.py:17-18
func (r *StaticRouter) Dispatch(_ context.Context, sourceNode string) ([]Message, error) {
	return buildTriggerMessages(sourceNode, r.targets), nil
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run TestStaticRouter -v 2>&1 | tail -8
```

预期：4 个 `--- PASS`（`TestStaticRouter_零目标`、`_单目标`、`_多目标保序`、`TestNewStaticRouter_复制入参`）。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && $(go env GOROOT)/bin/gofmt -l -w internal/agentcore/graph/pregel/router.go internal/agentcore/graph/pregel/router_test.go
git add internal/agentcore/graph/pregel/router.go internal/agentcore/graph/pregel/router_test.go
git commit -m "feat(8.5): StaticRouter 1→N 扇出与 buildTriggerMessages"
```

---

### Task 3: `BarrierRouter`

**Files:**
- Modify: `internal/agentcore/graph/pregel/router.go`
- Modify: `internal/agentcore/graph/pregel/router_test.go`

- [ ] **Step 1: 写失败测试**

在 `router_test.go` **末尾追加**：

```go
// TestBarrierRouter_产出汇聚消息 target 是 barrier 通道的合成路由键。
// Python: BarrierRouter([barrier.key])（builder.py:49），键格式见 channels.py:155-164
func TestBarrierRouter_产出汇聚消息(t *testing.T) {
	const key = "barrier:(a|b)&c->d"
	msgs, err := NewBarrierRouter(key).Dispatch(context.Background(), "a")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d，期望 1", len(msgs))
	}
	if msgs[0].Kind() != KindBarrier {
		t.Errorf("Kind() = %q，期望 %q", msgs[0].Kind(), KindBarrier)
	}
	if msgs[0].Sender != "a" {
		t.Errorf("Sender = %q，期望 \"a\"（BarrierChannel 用它判 OR 组）", msgs[0].Sender)
	}
	if msgs[0].Target != key {
		t.Errorf("Target = %q，期望 %q", msgs[0].Target, key)
	}
}

// TestBarrierRouter_零目标 空 targets 产出非 nil 空切片。
func TestBarrierRouter_零目标(t *testing.T) {
	msgs, err := NewBarrierRouter().Dispatch(context.Background(), "a")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if msgs == nil {
		t.Fatalf("msgs = nil，期望非 nil 空切片")
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d，期望 0", len(msgs))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run TestBarrierRouter -v 2>&1 | tail -5
```

预期：编译失败，含 `undefined: NewBarrierRouter`。

- [ ] **Step 3: 写最小实现**

在 `StaticRouter` 结构体之后**插入**：

```go
// BarrierRouter 汇聚路由（N→1），产出 BarrierMessage。
//
// target 不是节点名，而是 barrier 通道的合成路由键 "barrier:a&b->c"
// （channels.py:155-164）；sender 由 BarrierChannel 收下作为 OR 组判据。
// Python: pregel/router.py:49-56
type BarrierRouter struct {
	// targets 目标 barrier 通道路由键列表
	targets []string
}
```

在全局变量区块**追加**：

```go
// 编译期接口断言：BarrierRouter 必须满足 Router。
var _ Router = (*BarrierRouter)(nil)
```

在 `NewStaticRouter` 之后**插入**：

```go
// NewBarrierRouter 构造汇聚路由。
// Python: BarrierRouter(targets)（router.py:52-53）
func NewBarrierRouter(targets ...string) *BarrierRouter {
	return &BarrierRouter{targets: append([]string(nil), targets...)}
}
```

在 `StaticRouter.Dispatch` 之后**插入**：

```go
// Dispatch 产出 N→1 汇聚消息。
// Python: router.py:55-56
func (r *BarrierRouter) Dispatch(_ context.Context, sourceNode string) ([]Message, error) {
	msgs := make([]Message, 0, len(r.targets))
	for _, target := range r.targets {
		msgs = append(msgs, NewBarrierMessage(sourceNode, target))
	}
	return msgs, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run TestBarrierRouter -v 2>&1 | tail -6
```

预期：2 个 `--- PASS`。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && $(go env GOROOT)/bin/gofmt -l -w internal/agentcore/graph/pregel/router.go internal/agentcore/graph/pregel/router_test.go
git add internal/agentcore/graph/pregel/router.go internal/agentcore/graph/pregel/router_test.go
git commit -m "feat(8.5): BarrierRouter N→1 汇聚路由"
```

---

### Task 4: `ConditionalRouter`

**Files:**
- Modify: `internal/agentcore/graph/pregel/router.go`
- Modify: `internal/agentcore/graph/pregel/router_test.go`

- [ ] **Step 1: 写失败测试**

在 `router_test.go` 的 import 块补 `errors`（改为 `"context"` / `"errors"` / `"testing"` 三行），并在**末尾追加**：

```go
// TestConditionalRouter_单目标 selector 返回单目标 → 1 条 Trigger 消息。
// Python: router.py:44-46（isinstance(targets, str) 的归一化在 Go 侧上移到调用方，偏差 D3）
func TestConditionalRouter_单目标(t *testing.T) {
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return []string{"D"}, nil
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d，期望 1", len(msgs))
	}
	if msgs[0].Sender != "A" || msgs[0].Target != "D" {
		t.Errorf("msg = %+v，期望 Sender=\"A\" Target=\"D\"", msgs[0])
	}
	if msgs[0].Kind() != KindTrigger {
		t.Errorf("Kind() = %q，期望 %q", msgs[0].Kind(), KindTrigger)
	}
}

// TestConditionalRouter_多目标 多目标保序。
func TestConditionalRouter_多目标(t *testing.T) {
	targets := []string{"E", "F", "G"}
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return targets, nil
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != len(targets) {
		t.Fatalf("len(msgs) = %d，期望 %d", len(msgs), len(targets))
	}
	for i, want := range targets {
		if msgs[i].Target != want {
			t.Errorf("msgs[%d].Target = %q，期望 %q", i, msgs[i].Target, want)
		}
	}
}

// TestConditionalRouter_零目标 空目标产出非 nil 空切片。
func TestConditionalRouter_零目标(t *testing.T) {
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return nil, nil
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if msgs == nil {
		t.Fatalf("msgs = nil，期望非 nil 空切片")
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d，期望 0", len(msgs))
	}
}

// TestConditionalRouter_selector收到nilState 记录 Python 事实基线：
// state 恒为 None（router.py:37），「注入/不注入」两分支行为等价（偏差 D2）。
func TestConditionalRouter_selector收到nilState(t *testing.T) {
	var gotState any = "sentinel"
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, state any) ([]string, error) {
		gotState = state
		return []string{"D"}, nil
	}))
	if _, err := r.Dispatch(context.Background(), "A"); err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if gotState != nil {
		t.Errorf("state = %v，期望 nil", gotState)
	}
}

// TestConditionalRouter_ctx透传 ctx 原样传给 selector。
func TestConditionalRouter_ctx透传(t *testing.T) {
	type ctxKey string
	key := ctxKey("trace")
	ctx := context.WithValue(context.Background(), key, "abc")

	var got any
	r := NewConditionalRouter(SelectorFunc(func(gotCtx context.Context, _ any) ([]string, error) {
		got = gotCtx.Value(key)
		return nil, nil
	}))
	if _, err := r.Dispatch(ctx, "A"); err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if got != "abc" {
		t.Errorf("ctx 值 = %v，期望 \"abc\"", got)
	}
}

// TestConditionalRouter_selector返错原样上抛 错误不得包装（偏差 D6）：
// 8.3b/8.3c 需要靠 errors.As 识别 *pregel.GraphInterrupt。
func TestConditionalRouter_selector返错原样上抛(t *testing.T) {
	errSentinel := errors.New("selector 失败")
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return nil, errSentinel
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
	if err != errSentinel {
		t.Errorf("err = %v，期望原样返回哨兵错误（不得包装）", err)
	}
	if msgs != nil {
		t.Errorf("msgs = %v，期望 nil", msgs)
	}
}

// TestConditionalRouter_selector为nil 在 Dispatch 阶段报错（对齐 Python
// 到 dispatch 才 inspect.signature(None) 抛 TypeError 的时机）。
func TestConditionalRouter_selector为nil(t *testing.T) {
	msgs, err := NewConditionalRouter(nil).Dispatch(context.Background(), "A")
	if err == nil {
		t.Fatalf("err = nil，期望非 nil")
	}
	if msgs != nil {
		t.Errorf("msgs = %v，期望 nil", msgs)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run TestConditionalRouter -v 2>&1 | tail -5
```

预期：编译失败，含 `undefined: NewConditionalRouter`。

- [ ] **Step 3: 写最小实现**

在 `router.go` 顶部把 import 改为：

```go
import (
	"context"
	"fmt"
)
```

在 `BarrierRouter` 结构体之后**插入**：

```go
// ConditionalRouter 条件路由：由 selector 决定目标，产出 TriggerMessage。
// Python: pregel/router.py:26-46
type ConditionalRouter struct {
	// selector 选择器；nil 时 Dispatch 返回 error（对齐 Python 在 dispatch
	// 阶段才 inspect.signature(None) 抛 TypeError 的时机）
	selector Selector
}
```

在全局变量区块**追加**：

```go
// 编译期接口断言：ConditionalRouter 必须满足 Router。
var _ Router = (*ConditionalRouter)(nil)
```

在 `NewBarrierRouter` 之后**插入**：

```go
// NewConditionalRouter 构造条件路由。
// Python: ConditionalRouter(selector)（router.py:29-30）
func NewConditionalRouter(selector Selector) *ConditionalRouter {
	return &ConditionalRouter{selector: selector}
}
```

在 `BarrierRouter.Dispatch` 之后**插入**：

```go
// Dispatch 由 selector 决定目标并产出触发消息。
//
// 纪律：state 恒传 nil（spec §1.3）；selector 的 error 原样上抛，不包装——
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
```

- [ ] **Step 4: 跑测试确认通过并核对覆盖率**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -cover -v 2>&1 | tail -25
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -coverprofile=/tmp/pregel85.out >/dev/null && go tool cover -func=/tmp/pregel85.out | grep router.go
```

预期：全部 `--- PASS`；`go tool cover -func` 输出中 `router.go` 的每个函数均为 `100.0%`，无低于 100% 的行。若 `ConditionalRouter.Dispatch` 或 `buildTriggerMessages` 未达 100%，补齐对应分支用例后再继续。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && $(go env GOROOT)/bin/gofmt -l -w internal/agentcore/graph/pregel/router.go internal/agentcore/graph/pregel/router_test.go
git add internal/agentcore/graph/pregel/router.go internal/agentcore/graph/pregel/router_test.go
git commit -m "feat(8.5): ConditionalRouter 条件路由与 Selector 静态建模"
```

---

### Task 5: 更新 `pregel/doc.go`

**Files:**
- Modify: `internal/agentcore/graph/pregel/doc.go`

- [ ] **Step 1: 包概述补一句**

把这一段：

```go
// 以及超步之间传递调度消息的 Message、承载中断值的 Interrupt 与承载中断跳转的
// GraphInterrupt。
```

替换为：

```go
// 以及超步之间传递调度消息的 Message、承载中断值的 Interrupt、承载中断跳转的
// GraphInterrupt，与决定下一超步激活哪些节点并产出调度消息的 Router。
```

- [ ] **Step 2: 目录树补 `router.go`**

把：

```go
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象、GraphInterrupt 中断跳转
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
```

替换为：

```go
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象、GraphInterrupt 中断跳转
//	├── router.go      # Router 接口 + Selector/SelectorFunc + Static/Barrier/Conditional 三个路由实现
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
```

- [ ] **Step 3: 对应 Python 路径补 `router.py`**

把：

```go
// 对应 Python 代码：openjiuwen/core/graph/pregel/base.py · config.py · constants.py
```

替换为：

```go
// 对应 Python 代码：openjiuwen/core/graph/pregel/base.py · config.py · constants.py · router.py
```

- [ ] **Step 4: 校验包文档格式与编译**

```bash
cd /home/opensource/uapclaw-gateway && $(go env GOROOT)/bin/gofmt -l internal/agentcore/graph/pregel/doc.go && go doc ./internal/agentcore/graph/pregel 2>&1 | head -20
```

预期：`gofmt -l` 无输出；`go doc` 输出的包注释含 `router.go` 一行与 `pregel/router.py`。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/graph/pregel/doc.go
git commit -m "docs(8.5): pregel 包文档补 router.go 与职责说明"
```

---

### Task 6: 计划表与历史 spec 的顺序注记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:505`（8.5 行）+ 领域八表格后的注记行
- Modify: `docs/superpowers/specs/2026-10-31-graph-interrupt-8.7-design.md:299` 之后

- [ ] **Step 1: 替换 8.5 行**

把第 505 行整行替换为（覆盖率以 Task 4 Step 4 实测值为准）：

```markdown
| 8.5 | ✅ | IRouter 三实现 | ✅ `Router` 接口(`Dispatch(ctx, sourceNode) ([]Message, error)`，对应 Python `IRouter.dispatch`) + `Selector`/`SelectorFunc`(`Select(ctx, state any)`，state 恒 nil，见偏差 D2 与 spec §3.2) + `StaticRouter`(1→N→TriggerMessage，空 targets≡边终止)/`BarrierRouter`(N→1→BarrierMessage，target 为 `barrier:a&b->c` 合成键)/`ConditionalRouter`(selector 决定目标；Python 的 str→[]string 归一化上移到调用方，偏差 D3)；三类型不合并(D4)、targets 构造复制(D5)、selector error 原样上抛不包装(D6)；偏差 D1-D6 归档 spec §4；`router.go` 覆盖率 100%；⤴️ 本节前移至 8.3 之前（8.3 的 `PregelNode.routers` 需先有 Router 接口）。设计：docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md | `openjiuwen/core/graph/pregel/router.py` + `pregel/base.py`(23-26) |
```

- [ ] **Step 2: 领域八表格后插入顺序注记行**

在该表之后、`---` 分隔线之前的 `**验证点**：✅ 可定义和执行工作流 DAG，多 Agent 协作完成任务` 下一行，插入空行 + 下面这行（**不改动 8.6/8.7 两行原文**）：

```markdown
> **8.x 实施顺序（2026-11-01 调整）**：8.5 → 8.3 → 8.8 → 8.4 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b（8.5 自原路线前移：8.3 的 `PregelNode` 必须先有 Router 接口，而 8.5 只依赖已完成的 `Message`。依据见 8.5 spec §5）。
```

- [ ] **Step 3: 8.7 spec §11 追加调整行**

在 `docs/superpowers/specs/2026-10-31-graph-interrupt-8.7-design.md` 的 §11 路线代码块**下方**（`依据：…` 段落之后）追加一行，**不改动原代码块与原文**：

```markdown
> 2026-11-01 调整：8.5 提前到 8.3 之前（8.5 只依赖已完成的 `Message`，而 8.3 的 `PregelNode` 必须先有 Router 接口）。原文保留。
```

- [ ] **Step 4: 核对未误伤其他行并提交**

```bash
cd /home/opensource/uapclaw-gateway && git diff --stat && git diff IMPLEMENTATION_PLAN.md | head -30
```

预期：`IMPLEMENTATION_PLAN.md` 只有 8.5 一行变更 + 1 行新增；无其他行的删除。确认后：

```bash
cd /home/opensource/uapclaw-gateway && git add IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-10-31-graph-interrupt-8.7-design.md
git commit -m "docs(8.5): 计划表标记完成并记录 8.5 前移决策"
```

---

### Task 7: 全量回归与收尾

**Files:** 无（仅验证）

- [ ] **Step 1: 检查无残留 go 进程**

```bash
pgrep -f 'go (build|test)' || echo "无残留进程"
```

预期：输出 `无残留进程`；若有输出，先 `pkill -f 'go test'` 再继续（同规则 5：单进程互斥）。

- [ ] **Step 2: 全量回归（必须带 CI 同款构建标签）**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./... 2>&1 | grep -v "^ok" | tail -40
```

预期：无 `FAIL` 行（`ok` 行已被过滤）。漏掉 `-tags "sqlite_fts5 test"` 会让 memory 包 FTS 用例假失败。

- [ ] **Step 3: 确认依赖方向未变（pregel 仍是叶子包）**

```bash
cd /home/opensource/uapclaw-gateway && go list -f '{{join .Imports "\n"}}' ./internal/agentcore/graph/pregel/ | sort
```

预期：仅 `context` / `fmt` 两条标准库 import，**不含** `internal/agentcore/session` 或 `internal/agentcore/graph`（`go list .Imports` 不含测试专用 import，`testing` 不会出现）。

- [ ] **Step 4: 确认提交序列与工作区**

```bash
cd /home/opensource/uapclaw-gateway && git log --oneline -8 && git status --short
```

预期：本批共 7 笔提交（`24911906` 的 spec + 本 plan + Task1-4 的 4 笔代码 + Task5 的 doc.go + Task6 的计划表）。工作区干净；若有残留改动，逐一核对是否属于本任务，**非本任务的改动不要顺手提交**（尤其禁止 `git commit -am`）。

---

## 审查口径（供实施后自查）

| 检查项 | 期望 |
|--------|------|
| 声明顺序 | `router.go` 内：结构体（`Router`/`Selector`/`SelectorFunc`/三实现）→ 全局变量（4 条断言）→ 导出函数（3 构造 + 4 方法）→ 非导出函数（`buildTriggerMessages`），段间有分隔注释 |
| 注释语言 | 全中文；每个类型/方法带 `Python:` 前缀标注源行号 |
| 日志 | 本节零日志（Python `router.py` 全文无 `logger.` 调用，规则 3 无对应点） |
| 覆盖率 | `router.go` 每个函数 100.0%；`pregel` 包整体不下降（8.7 后为 100%） |
| 偏差 | D1 `async→ctx+error`、D2 不复刻签名嗅探、D3 去 str 归一化、D4 三类型不合并、D5 targets 复制、D6 error 不包装——六条在 spec §4 有档 |
| state 纪律 | `Selector.Select` 的 `state` 恒传 `nil`；`pregel` 内不得出现对 `state` 的类型断言 |
| 依赖方向 | `pregel` 不 import `session/**` 与 `graph`（编译期断言见 Task 7 Step 3 命令） |
| 空目标语义 | 三实现均返回**非 nil 空切片**（Python `[]`），不得返回 `nil` |
| 中断纪律 | 本节不 recover、不判中断；路由跳过中断的逻辑属于 8.3b |
