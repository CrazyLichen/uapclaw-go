# 8.3 PregelNode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/agentcore/graph/pregel` 包落地 Pregel 节点描述体——`NodeInput`／`NodeFunc`／`PregelNode` 三类型与 `NewPregelNode`／`addRouter`／`NoopNodeFunc` 三函数，把「一个节点的执行体」与「它的出边路由」绑成 8.3a/8.3b/8.3c 共用的唯一交接物。

**Architecture:** Python 的 `PregelNode` 只有三行赋值（`name`/`func`/`routers`），复杂度全在**调用协议**上：`NodeTask` 用 `inspect.signature` 按形参名嗅探后注入 `config`/`state` 两个 kwargs。Go 的 reflect 取不到函数形参名、调用参数编译期定死，故改为单个静态类型 `NodeFunc func(ctx, NodeInput) error` + 显式结构体 `NodeInput{Config, State}`：所有节点同一签名，需要什么读什么，8.3b 侧单条调用路径、零判断（与 8.5 D2「不做签名嗅探」同源）。字段全非导出、构造期与追加期统一 nil 校验、指针使用（builder 构造后仍要追加路由）。

**Tech Stack:** Go 1.x（标准库 `context`／`testing`），包 `internal/agentcore/graph/pregel`，对齐 Python `openjiuwen/core/graph/pregel/base.py:46-50`（5 行）+ `pregel/task.py:125-174`（调用协议）。

**设计文档：** `docs/superpowers/specs/2026-11-01-pregel-node-8.3-design.md`（已提交 `d81c7bc0`）

---

## 背景：本任务在全局中的位置

Agent 的一次执行本身是工作流图。构图期 `PregelGraph._compile`（`graph.py:242-245`）把 `nodes{id: Vertex}` 交给 `PregelBuilder.add_node` → 产出 `PregelNode(name, func, routers)`，边则由 `builder.add_edge` 在**已有节点上原地 `routers.append(...)`**（`builder.py:49-68`）。执行期 `PregelLoop.run_step` 拿到就绪节点名 → `self.graph.nodes[name]` 取到本节的结构 → `executor.submit(node, node_version[name])`（`engine.py:135-139`）→ `NodeTask.run` 依次做三件事：`node.func(**kwargs)`（`task.py:151-154`）→ 逐条 `node.routers[i].dispatch()`（`task.py:156-160`）→ 发 `EDGE_TRAVERSED` 事件（`task.py:162-168`）。

**硬约束（8.3b 落地时必须保持）**：`func` 抛 `GraphInterrupt` 后 `task.py:172-174` 直接把异常当返回值交出，**路由整段不执行**。这个「中断不产生任何出边消息」的分界线，物理位置就是本结构的两段之间——所以 `NodeFunc` 只返回 `error`，中断走 panic，两者不能混。

**本节范围**：只做 `PregelNode`。`Channel` ABC 已按方案 B 移入 8.4（`channels.go`），**不在本节**。

**章节顺序**：本轮**不改**计划表顺序（`8.3 → 8.8 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b`），也不处理 8.3a / 8.8 的位置问题（已拍板「都不处理」）。两条事实留档于 8.3 spec §5：① 8.3a 的 `Build` 返回 `*Pregel`（属 8.3c）；② 8.8 的 `graph_state.GraphState` 是全仓零实例化的死类型。本节**不得**为这两件事改动任何计划表行。

---

## 文件结构

| 文件 | 职责 | 动作 |
|------|------|------|
| `internal/agentcore/graph/pregel/node.go` | `NodeInput`/`NodeFunc`/`PregelNode` + `NewPregelNode`/`NoopNodeFunc`/`addRouter` | 新建 |
| `internal/agentcore/graph/pregel/node_test.go` | 上者单元测试，目标 `node.go` 覆盖率 100% | 新建 |
| `internal/agentcore/graph/pregel/doc.go` | 目录树 + 包概述 + 类型清单 | 修改 |
| `IMPLEMENTATION_PLAN.md` | 8.3 行 `☐→✅` + 顺序注记追加第三行 | 修改 |
| `docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md` | §9 三处追加（原文一字不改） | 修改 |
| `docs/superpowers/specs/2026-11-01-pregel-channels-8.4-design.md` | §5.4/§6 追加「已兑现」 | 修改 |
| `docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md` | 首段追加「8.3 已落地」 | 修改 |

**最终 `node.go` 的声明顺序**（规范 2；每个 Task 只往对应区块追加，不做后续搬家）：

```
结构体：NodeInput → NodeFunc → PregelNode
        （NodeFunc 是具名函数类型，规范未单列，按类型声明归此块，排在结构体之后）
导出函数：NewPregelNode → NoopNodeFunc
非导出函数：(*PregelNode).addRouter
```

**`node.go` 的 import 只有 `context`**（panic 文案用字符串拼接，不引 `fmt`）。

---

### Task 1: `NodeInput` + `NodeFunc`

**Files:**
- Create: `internal/agentcore/graph/pregel/node.go`
- Test: `internal/agentcore/graph/pregel/node_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/agentcore/graph/pregel/node_test.go`：

```go
package pregel

import (
	"context"
	"testing"
)

// TestNodeFunc_接收NodeInput 节点函数按 NodeFunc 签名被调用时，ctx 与 NodeInput 原样透传，State 为 nil。
// Python: task.py:137-154（按形参名嗅探后拼 kwargs 调用 func，state 恒 None）
func TestNodeFunc_接收NodeInput(t *testing.T) {
	type ctxKey string
	key := ctxKey("trace")
	ctx := context.WithValue(context.Background(), key, "abc")

	var gotCtx context.Context
	var gotIn NodeInput
	fn := NodeFunc(func(c context.Context, in NodeInput) error {
		gotCtx = c
		gotIn = in
		return nil
	})

	in := NodeInput{Config: InnerPregelConfig{PregelConfig: PregelConfig{Ns: "wf", SessionID: "s1"}}}
	if err := fn(ctx, in); err != nil {
		t.Fatalf("节点函数返回错误：%v", err)
	}
	if gotCtx.Value(key) != "abc" {
		t.Errorf("ctx 未透传：期望 %q", "abc")
	}
	if gotIn.Config.Ns != "wf" || gotIn.Config.SessionID != "s1" {
		t.Errorf("Config 未透传：Ns=%q SessionID=%q", gotIn.Config.Ns, gotIn.Config.SessionID)
	}
	if gotIn.State != nil {
		t.Errorf("State 应为 nil，实际 %v", gotIn.State)
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败**

```bash
pgrep -f 'go (build|test)' || echo "无残留编译进程"
go test ./internal/agentcore/graph/pregel/ -run 'TestNodeFunc' -v
```

预期：`FAIL` + `undefined: NodeInput` / `undefined: NodeFunc`（编译错误）。

- [ ] **Step 3: 写最小实现**

新建 `internal/agentcore/graph/pregel/node.go`：

```go
package pregel

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// NodeInput 节点函数入参：把 Python 的「按形参名条件注入」换成显式结构体传递。
//
// 纪律：唯一构造点是 8.3b 的 NodeTask；包内别处不应构造。
// Python: pregel/task.py:137-149
type NodeInput struct {
	// Config 本次执行的内部配置，Ns/ParentNs 由 8.3b 改写为 parent:node:version
	Config InnerPregelConfig
	// State 恒 nil，对齐 task.py:149 的字面量 None。
	//
	// 只能是 any：Python 注解的 graph_state.GraphState 全仓零实例化、零读取，
	// 且该类型属 graph 包，而 graph 要 import 本包，收窄即成编译期环（8.3 spec §3.2）。
	State any
}

// NodeFunc 节点执行体。
//
// 纪律：只返回普通错误。图级中断是 panic（*GraphInterrupt），由 8.3b 用 recover
// 转成返回值；本类型内不得 recover 或吞掉它（对齐 task.py:172-174 的 except 顺序）。
// 出边完全由 PregelNode 的 routers 决定，返回值不参与路由（Python 同）。
type NodeFunc func(ctx context.Context, in NodeInput) error
```

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/graph/pregel/ -run 'TestNodeFunc' -v
```

预期：`--- PASS: TestNodeFunc_接收NodeInput`、`ok internal/agentcore/graph/pregel`。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/pregel/node.go internal/agentcore/graph/pregel/node_test.go
git commit -m "feat(pregel): 8.3 新增 NodeInput/NodeFunc 节点执行体协议"
```

---

### Task 2: `PregelNode` + `NewPregelNode`

**Files:**
- Modify: `internal/agentcore/graph/pregel/node.go`（结构体区块追加 `PregelNode`；导出函数区块追加 `NewPregelNode`）
- Test: `internal/agentcore/graph/pregel/node_test.go`（结构体区块追加 `stubRouter`；末尾追加 4 个用例）

- [ ] **Step 1: 写失败测试**

在 `node_test.go` 的 `package pregel` 与 `import` 之后、第一个测试函数之前，插入测试替身（测试文件同样遵守声明顺序：结构体在函数之前）：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// stubRouter 测试用路由：只带一个标识，供断言挂载顺序与身份；Dispatch 恒返回空。
type stubRouter struct {
	id string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Dispatch 恒返回空消息，本类型只用于断言节点持有的路由身份。
func (s *stubRouter) Dispatch(context.Context, string) ([]Message, error) {
	return nil, nil
}
```

在文件末尾追加四个用例：

```go
// TestNewPregelNode_字段与顺序 构造出的节点保住 name、fn 与路由顺序。
// Python: base.py:46-50（三行赋值，routers 保序）
func TestNewPregelNode_字段与顺序(t *testing.T) {
	r1 := &stubRouter{id: "r1"}
	r2 := &stubRouter{id: "r2"}
	n := NewPregelNode("A", NoopNodeFunc, r1, r2)

	if n.name != "A" {
		t.Errorf("name = %q，期望 \"A\"", n.name)
	}
	if n.fn == nil {
		t.Fatal("fn 不应为 nil")
	}
	if len(n.routers) != 2 {
		t.Fatalf("routers 长度 = %d，期望 2", len(n.routers))
	}
	if n.routers[0] != Router(r1) || n.routers[1] != Router(r2) {
		t.Errorf("routers 顺序或身份不符：%v", n.routers)
	}
}

// TestNewPregelNode_复制入参切片 外部改动用于展开的切片，不影响节点内部的路由列表。
// 偏差 D3：Python base.py:50 存引用，Go 构造期复制（同 8.5 D5）。
func TestNewPregelNode_复制入参切片(t *testing.T) {
	r1 := &stubRouter{id: "r1"}
	r2 := &stubRouter{id: "r2"}
	list := []Router{r1, r2}
	n := NewPregelNode("A", NoopNodeFunc, list...)

	list[0] = &stubRouter{id: "changed"}

	if n.routers[0] != Router(r1) {
		t.Errorf("节点内部切片被外部改动污染：%v", n.routers[0])
	}
}

// TestNewPregelNode_nilFnPanics 节点函数为 nil 时构造期 panic。
// 偏差 D4：Python 要等到执行期 inspect.signature(None) 才抛 TypeError。
func TestNewPregelNode_nilFnPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("fn 为 nil 时期望 panic，未发生")
		}
	}()
	NewPregelNode("A", nil)
}

// TestNewPregelNode_nilRouterPanics nil 路由夹在合法路由中间也必须被拦下。
// 偏差 D4：Python 对 None router 是调用期 AttributeError。
func TestNewPregelNode_nilRouterPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("router 为 nil 时期望 panic，未发生")
		}
	}()
	NewPregelNode("A", NoopNodeFunc, &stubRouter{id: "r1"}, nil)
}
```

> 注意：`TestNewPregelNode_字段与顺序` 用了 `NoopNodeFunc`（Task 4 才落地）。本 Task 先写测试会编译失败于 `undefined: NoopNodeFunc` 与 `undefined: NewPregelNode` 两处——这是**预期的**，Step 3 一并补齐。

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/agentcore/graph/pregel/ -run 'TestNewPregelNode' -v
```

预期：`FAIL` + `undefined: PregelNode` / `undefined: NewPregelNode` / `undefined: NoopNodeFunc`。

- [ ] **Step 3: 写最小实现**

在 `node.go` 结构体区块的 `NodeFunc` 之后追加：

```go
// PregelNode 图节点的描述体：调度键 + 执行体 + 出边。
//
// 字段全部非导出：8.3a/8.3b/8.3c 与本类型同在 pregel 包，非导出不阻碍任何消费方，
// 却能让「构造 / 追加」成为唯一入口（nil 路由只能在这两处被拦下）。
// 使用方式为指针：builder 构造后还要追加路由，值类型会丢 mutation。
// Python: pregel/base.py:46-50
type PregelNode struct {
	// name 调度键，同时是 TriggerChannel 的通道键
	name string
	// fn 执行体，非 nil（构造期保证）
	fn NodeFunc
	// routers 出边，构造后由 8.3a 追加、8.3b 逐条 Dispatch；元素非 nil
	routers []Router
}
```

在导出函数区块（三个类型之后、非导出函数区块之前）追加：

```go
// NewPregelNode 构造节点。
//
// fn 为 nil 或任一 router 为 nil 一律 panic：Python 对 None 是在执行期才抛
// （func=None → inspect.signature 报错；router=None → AttributeError），
// 本处把「必然崩」提前到构图期。typed-nil 挡不住——var r *StaticRouter;
// var i Router = r 时 i != nil 为真，调用 Dispatch 仍会解引用 r.targets panic，
// 故 8.3b 的 NodeTask 循环零判空（8.5 spec §9）。
//
// routers 复制入参切片（Python base.py:50 存引用；无消费者依赖可变性，同 8.5 D5）。
// Python: PregelNode(name, func, routers)
func NewPregelNode(name string, fn NodeFunc, routers ...Router) *PregelNode {
	if fn == nil {
		panic("pregel: 节点函数不能为 nil，节点名=" + name)
	}
	n := &PregelNode{
		name:    name,
		fn:      fn,
		routers: make([]Router, 0, len(routers)),
	}
	for _, r := range routers {
		n.addRouter(r)
	}
	return n
}
```

同时在同一区块内补上 `NoopNodeFunc` 的正式实现（本 Task 的 `TestNewPregelNode_字段与顺序` 要用它，故必须就地落地，不留占位；Task 4 只补它的专项测试）：

```go
// NoopNodeFunc 空节点函数，对齐 builder.py:18-19 的 lambda: None（START/END 占位节点）。
func NoopNodeFunc(context.Context, NodeInput) error { return nil }
```

并在非导出函数区块追加（本 Task 即为正式版本，Task 3 只补测试）：

```go
// addRouter 追加一条出边路由，供 8.3a 的 builder 在构图期使用。
//
// router 为 nil 时 panic：与构造期同口径，覆盖 builder 的追加路径
// （Python builder.py:49-68 直接 nodes[src].routers.append(...)）。
// 对应 Python 无独立方法（字段公开、直接 append）。
func (n *PregelNode) addRouter(r Router) {
	if r == nil {
		panic("pregel: 路由不能为 nil，节点名=" + n.name)
	}
	n.routers = append(n.routers, r)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/graph/pregel/ -run 'TestNewPregelNode' -v
```

预期：4 个用例全 `PASS`，`ok internal/agentcore/graph/pregel`。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/pregel/node.go internal/agentcore/graph/pregel/node_test.go
git commit -m "feat(pregel): 8.3 新增 PregelNode 与 NewPregelNode（nil panic + 复制路由）"
```

---

### Task 3: `addRouter` 的专项测试

实现已在 Task 2 落地（`NewPregelNode` 依赖它）。本 Task 只补两条直接针对它的用例，把「构造期之外的第二条追加路径」也锁住。

**Files:**
- Test: `internal/agentcore/graph/pregel/node_test.go`（末尾追加 2 个用例）

- [ ] **Step 1: 写测试**

```go
// TestAddRouter_追加保序 builder 在构图期按边声明顺序追加路由，节点侧必须保序。
// Python: builder.py:49-68（nodes[src].routers.append(...) 保序）
func TestAddRouter_追加保序(t *testing.T) {
	n := NewPregelNode("A", NoopNodeFunc)
	r1 := &stubRouter{id: "r1"}
	r2 := &stubRouter{id: "r2"}
	r3 := &stubRouter{id: "r3"}

	n.addRouter(r1)
	n.addRouter(r2)
	n.addRouter(r3)

	if len(n.routers) != 3 {
		t.Fatalf("routers 长度 = %d，期望 3", len(n.routers))
	}
	if n.routers[0] != Router(r1) || n.routers[1] != Router(r2) || n.routers[2] != Router(r3) {
		t.Errorf("追加顺序不符：%v", n.routers)
	}
	if n.name != "A" || n.fn == nil {
		t.Errorf("追加路由影响了 name/fn：name=%q", n.name)
	}
}

// TestAddRouter_nilPanics 追加 nil 路由与构造期同口径 panic。
// 偏差 D4：Python 无此方法，直接 append(None) 要到 dispatch 才炸。
func TestAddRouter_nilPanics(t *testing.T) {
	n := NewPregelNode("A", NoopNodeFunc)
	defer func() {
		if recover() == nil {
			t.Fatal("追加 nil 路由时期望 panic，未发生")
		}
	}()
	n.addRouter(nil)
}
```

- [ ] **Step 2: 跑测试确认通过（非 TDD 红灯阶段，实现已存在）**

```bash
go test ./internal/agentcore/graph/pregel/ -run 'TestAddRouter' -v
```

预期：2 个用例 `PASS`。若 `TestAddRouter_nilPanics` 未 panic，说明 Task 2 的 nil 校验被漏写，立即回到 `node.go` 补齐。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/graph/pregel/node_test.go
git commit -m "test(pregel): 8.3 覆盖 addRouter 的追加保序与 nil panic"
```

---

### Task 4: `NoopNodeFunc` 专项测试

实现已在 Task 2 临时补齐。本 Task 补测试并确认注释与 Python 出处对齐。

**Files:**
- Test: `internal/agentcore/graph/pregel/node_test.go`（末尾追加 1 个用例）

- [ ] **Step 1: 写测试**

```go
// TestNoopNodeFunc_返nil 空节点函数恒返回 nil，且不因入参取值而改变；用于 START/END 占位。
// Python: builder.py:18-19（add_node(START, lambda: None, [])）
func TestNoopNodeFunc_返nil(t *testing.T) {
	cases := []NodeInput{
		{},
		{Config: InnerPregelConfig{PregelConfig: PregelConfig{Ns: "wf", SessionID: "s1"}}},
	}
	for _, in := range cases {
		if err := NoopNodeFunc(context.Background(), in); err != nil {
			t.Errorf("NoopNodeFunc 返回错误：%v", err)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认通过**

```bash
go test ./internal/agentcore/graph/pregel/ -run 'TestNoopNodeFunc' -v
```

预期：`PASS`。

- [ ] **Step 3: 确认 `node.go` 覆盖率 100%**

```bash
go test ./internal/agentcore/graph/pregel/ -coverprofile=/tmp/node83.out >/dev/null &&
  go tool cover -func=/tmp/node83.out | grep 'pregel/node.go'
```

预期：`node.go` 内 `NodeFunc`/`NewPregelNode`/`NoopNodeFunc`/`addRouter` 及二者所在行均为 `100.0%`。

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/graph/pregel/node_test.go
git commit -m "test(pregel): 8.3 覆盖 NoopNodeFunc 并确认 node.go 覆盖率 100%"
```

---

### Task 5: 更新 `pregel/doc.go`

**Files:**
- Modify: `internal/agentcore/graph/pregel/doc.go:3-8`（概述）、`:29-39`（目录树与 Python 路径）

- [ ] **Step 1: 改概述句**

把 `doc.go` 第 3-8 行：

```go
// 本包是领域八的编译期最底层：构图（8.1）、节点与通道抽象（8.3）、
// 调度引擎（8.3b/8.3c）、通道管理（8.4）、顶点（8.11）、工作流（8.14）
// 均依赖本包提供的保留节点名、终止态键、递归预算、配置归一化能力，
// 以及超步之间传递调度消息的 Message、承载中断值的 Interrupt、承载中断跳转的
// GraphInterrupt、产出调度消息的 Router，与落地这些消息并决定下一超步激活哪些
// 节点的 Channel/ChannelManager（就绪判定 + CNF 汇聚 + 检查点快照）。
```

替换为：

```go
// 本包是领域八的编译期最底层：构图（8.1）、节点描述体（8.3）、
// 调度引擎（8.3b/8.3c）、通道管理（8.4）、顶点（8.11）、工作流（8.14）
// 均依赖本包提供的保留节点名、终止态键、递归预算、配置归一化能力，
// 以及超步之间传递调度消息的 Message、承载中断值的 Interrupt、承载中断跳转的
// GraphInterrupt、产出调度消息的 Router、把执行体与出边绑在一起的 PregelNode，
// 与落地这些消息并决定下一超步激活哪些节点的 Channel/ChannelManager
// （就绪判定 + CNF 汇聚 + 检查点快照）。
```

- [ ] **Step 2: 改目录树**

把目录树区块：

```go
//	pregel/
//	├── doc.go         # 包文档
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象、GraphInterrupt 中断跳转
//	├── channels.go    # Channel 接口 + ChannelSnapshot + Trigger/Barrier 通道 + ChannelManager
//	├── router.go      # Router 接口 + Selector/SelectorFunc + Static/Barrier/Conditional 三个路由实现
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
```

替换为（新增 `node.go` 一行，位置在 `base.go` 之后——类型文件先于功能文件）：

```go
//	pregel/
//	├── doc.go         # 包文档
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象、GraphInterrupt 中断跳转
//	├── node.go        # NodeInput/NodeFunc 与节点描述体 PregelNode（含 addRouter/NoopNodeFunc）
//	├── channels.go    # Channel 接口 + ChannelSnapshot + Trigger/Barrier 通道 + ChannelManager
//	├── router.go      # Router 接口 + Selector/SelectorFunc + Static/Barrier/Conditional 三个路由实现
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
```

- [ ] **Step 3: 确认编译与全包测试仍通过**

```bash
go build ./internal/agentcore/graph/pregel/ && go test ./internal/agentcore/graph/pregel/ -cover
```

预期：`ok internal/agentcore/graph/pregel`，包覆盖率不低于改动前（8.4/8.5 收尾时为 100% 的 `channels.go`/`router.go` 不被拉低）。

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/graph/pregel/doc.go
git commit -m "docs(pregel): 8.3 doc.go 补 node.go 目录项与节点描述体概述"
```

---

### Task 6: 计划表与三份 spec 回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:499`（8.3 行）、`:542-544` 之后的顺序注记区
- Modify: `docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md` §9（三处追加）
- Modify: `docs/superpowers/specs/2026-11-01-pregel-channels-8.4-design.md` §5.4 / §6 第 2 条
- Modify: `docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md` 首段

**铁律：旧内容一字不删，只追加。**

- [ ] **Step 1: 改 8.3 行**

把 `IMPLEMENTATION_PLAN.md:499` 的状态 `☐` 改为 `✅`，并在该行「内容」列**行首**插入 Go 事实，保留原有「⤴️ `Channel` ABC 已移入 8.4…」整段原文不动：

```
✅ Go 事实：`node.go` 提供 `NodeInput{Config InnerPregelConfig; State any}`（State 恒 nil 对齐 task.py:149；只能是 any——8.8 的 GraphState 全仓零实例化且 graph↔pregel 成编译期环）+ `NodeFunc func(ctx, NodeInput) error`（唯一出通道是 error，返回值不参与路由）+ `PregelNode{name/fn/routers}` 字段全非导出、指针使用 + `NewPregelNode(name, fn, routers...)`（fn/任一 router 为 nil 即 panic、复制入参切片）+ `addRouter`（承接 8.3a 的原地追加，Python 无此方法）+ `NoopNodeFunc`（对齐 lambda: None）；偏差 D1-D6 归档 spec §4（D1 不做形参名嗅探=8.5 D2 同源、D5 state 恒 any 属终态、D6 指针）；零日志对齐 base.py；`node.go` 覆盖率 100%；设计：docs/superpowers/specs/2026-11-01-pregel-node-8.3-design.md
```

- [ ] **Step 2: 追加顺序注记第三行**

在 L544「二次调整」注记**之后**新增一行（前两行原文不动）：

```
> **8.x 实施顺序（2026-11-01 三次记录）**：8.3 已落地；9 节顺序仍为 8.5 → 8.4 → 8.3 → 8.8 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b。**本轮明确不处理** 8.3a 与 8.8 的位置问题（8.3a 的 `Build` 返回属 8.3c 的 `*Pregel`；8.8 的 `graph_state.GraphState` 为零实例化死类型且 `graph` 包尚未建立），事实留档见 8.3 spec §5。原注记保留。
```

- [ ] **Step 3: 8.5 spec §9 三处追加**

3a. 首句后追加：

```
已兑现（2026-11-01）：`pregel/node.go` 的 `PregelNode` 直接持有 `routers []Router`。
```

3b. 「**已拍板（2026-11-01）**：`PregelNode` 字段全部**非导出** + `NewPregelNode(name, fn, routers ...Router)` 构造函数收口，构造时对 `nil` 元素**直接 panic**；8.3b 的 `NodeTask` 循环**零判空**。」——该句**原文一字不改**，紧随其后追加：

```
已兑现（2026-11-01）。补充一条 Python 没有的成员：`(*PregelNode).addRouter(r Router)` 未导出方法——`builder.py:49-68` 是 `nodes[src].routers.append(...)` 直接改 list，Go 字段非导出后必须留方法；nil 校验收在该方法内，使「构造」与「builder 追加」两条路径同口径（typed-nil 仍挡不住，见下文理由段）。
```

3c. §9 末尾追加一条：

```
- **8.3 的 `fn` 形态（2026-11-01 已拍板）**：`NodeFunc func(ctx context.Context, in NodeInput) error`，`NodeInput{Config InnerPregelConfig; State any}`。即把 `task.py:137-149` 的「按形参名条件注入 kwargs」换成「无条件结构化传入」——与本节 D2「不做签名嗅探」同源。代价：零参节点（START/END）改写为收下入参但忽略的 `NoopNodeFunc`。
```

- [ ] **Step 4: 8.4 spec 追加**

4a. §5.4 中「计划表 8.3 行的「`PregelNode` / `Channel ABC`」需追加归属说明（不删原文）」——句末追加「；已兑现（2026-11-01）：8.3 只落 `PregelNode`（`pregel/node.go`），`Channel` 接口由本节提供」。

4b. §6 第 2 条末尾追加：「**已兑现（2026-11-01）**：8.3 行已追加该说明并置 ✅。」

- [ ] **Step 5: 8.6 spec 首段追加**

在首段已有的「（2026-11-01 更新：…）」括号后追加：

```
（2026-11-01 再更新：8.3 已落地 `pregel/node.go`，仅含 `NodeInput`/`NodeFunc`/`PregelNode`；`Channel` 接口最终归属 8.4，`Kind()` 的过滤消费方已全部兑现）
```

- [ ] **Step 6: 校验只增不减**

```bash
git diff --stat
git diff --numstat | awk '{ if ($2 != "0") print "删行：" $0 }'
```

预期：每条 diff 的输出**第二列为 0**（无删除行）。若有非 0 行，说明改坏了原文，立即 `git checkout -- <该文件>` 重做。

- [ ] **Step 7: 提交**

```bash
git add IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md docs/superpowers/specs/2026-11-01-pregel-channels-8.4-design.md docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md
git commit -m "docs: 8.3 落地回填（计划表 ✅ + 8.5/8.4/8.6 spec 追加兑现说明）"
```

---

### Task 7: 全量回归与收尾

**Files:** 无新增（仅执行与 memory 更新）

- [ ] **Step 1: gofmt**

```bash
$(go env GOROOT)/bin/gofmt -l -w internal/agentcore/graph/pregel/node.go internal/agentcore/graph/pregel/node_test.go internal/agentcore/graph/pregel/doc.go
```

预期：无输出（已格式化）。若 `gofmt` 不在 GOROOT 下，用 `command -v gofmt` 兜底。

- [ ] **Step 2: 单包覆盖率再确认**

```bash
go test ./internal/agentcore/graph/pregel/ -coverprofile=/tmp/pregel83.out >/dev/null &&
  go tool cover -func=/tmp/pregel83.out | grep -E 'node.go|total'
```

预期：`node.go` 逐函数 100%，`total` 不低于改动前。

- [ ] **Step 3: 全量回归（必须带 CI 同款标签）**

```bash
pgrep -f 'go (build|test)' || echo "无残留编译进程"
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./... 2>&1 | tail -30
```

预期：末行 `exit=0` 语义对应「无 FAIL」；日志中无 `--- FAIL`、无 `FAIL\t` 包行。

- [ ] **Step 4: 写 memory 并修正旧指向**

新建 `pregel-node-8.3-complete.md`（`/root/.codebuddy/projects/home-opensource-uapclaw-gateway/memory/`），内容要点：

- 6 commits、`node.go` 覆盖率 100%、全量零 FAIL
- 已拍板：`NodeFunc(ctx, NodeInput) error`（不做形参名嗅探，= 8.5 D2 同源）；`State any` 是终态（值恒 None + `graph_state.GraphState` 零实例化 + pregel→graph 成环）
- `addRouter` 是 Python 没有的成员（承接 builder 原地 append，nil 校验收敛于此）
- 本轮明确不处理 8.3a/8.8 位置问题，事实：8.3a 的 Build 依赖 8.3c 的 `*Pregel`；8.8 是死类型且 `graph` 包未建
- 下一节：8.8（按现行顺序不动）

同时更新 `pregel-router-8.5-complete.md` 中「下一节 8.3」→「8.3 已完成，见 pregel-node-8.3-complete.md；下一节 8.8」，并在 `MEMORY.md` 索引加一行。

- [ ] **Step 5: 提交（仅当 memory 在仓库外则跳过此步，直接汇报）**

```bash
git status --porcelain
```

预期：为空（memory 目录在仓库外）。若有遗留改动，逐文件确认属于本节后再提交。

---

## 审查口径（供实施后自查）

1. **注释语言**：全部中文（规范 1）。
2. **声明顺序**：结构体（`NodeInput` → `NodeFunc` → `PregelNode`）→ 导出函数（`NewPregelNode` → `NoopNodeFunc`）→ 非导出函数（`addRouter`）（规范 2）。
3. **零日志**：`node.go` 不得出现 `logger.` 调用（对齐 `base.py` 全文无日志）。
4. **零新增依赖**：`node.go` 只 import `context`。
5. **不漏校验**：构造与 `addRouter` 两条路径都拦 nil（`TestNewPregelNode_nilRouterPanics` / `TestAddRouter_nilPanics` 同时通过）。
6. **不留回填点**：`State any` 注释里不得出现「待 8.8」之类字样——它是终态。
7. **不动计划表其它行**：8.3a / 8.8 两行原文与顺序必须与本计划落笔前完全一致（`git diff IMPLEMENTATION_PLAN.md` 逐行核对）。
8. **回填不减行**：Task 6 Step 6 的 `git diff --numstat` 检查必须全部第二列为 0。
9. **`channels.go:16-19` 的归属说明保持原文**：那段记录的是「`Channel` 接口原归 8.3」的历史，本节落地后**不改写**（`git diff internal/agentcore/graph/pregel/channels.go` 应为空）。
10. **`base.go` 未被触碰**：`git diff --stat` 中不应出现 `base.go`（`Message`/`Interrupt`/`GraphInterrupt` 属 8.6/8.7，本节不碰）。
