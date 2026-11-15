# 8.3b TaskExecutorPool / NodeTask 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 一比一落地 Python `openjiuwen/core/graph/pregel/task.py`（174 行）为 Go `internal/agentcore/graph/pregel/task.go`：单 step 任务池 + 节点任务执行体（FIRST_EXCEPTION 等待、中断降级、ns 拼接注入、EDGE_TRAVERSED 回调）。

**Architecture:** 方案 B（已拍板）——每节点一个 goroutine，结果经单容量 done channel 交付；`WaitAll` 用 `reflect.Select` 复刻 `asyncio.wait(FIRST_EXCEPTION)`，首个错误触发取消路径（per-task cancel ctx），被取消任务以 `context.Canceled` 记账，按「普通异常 > 中断」优先级上抛。图级中断按 8.3 既有纪律以 panic（`*GraphInterrupt`）传播，`nodeTask.execute` 用 recover 转成返回值。

**Tech Stack:** Go 标准库（context/reflect/sync/atomic/errors/strconv）+ 既有 `store`/`callback`/`logger` 包。

**Spec:** docs/superpowers/specs/2026-11-19-task-executor-pool-8.3b-design.md
**前置件（已核对全齐）：** Message/GraphInterrupt(8.6/8.7)、Router(8.5)、PregelNode(8.3，`name/fn/routers` 非导出)、CreateInnerConfig/常量(8.3d)、PendingNode(8.9a)、callback.Trigger + callback.EdgeTraversed。本节零回填欠账，不换章节。

**已拍板的循环依赖破环（用户方案①）：** Python task.py 用函数内延迟导入 `store.base.PendingNode` 破环（task.py:23/37）；Go 无延迟导入，把 `PendingNode`（纯数据，自身零 pregel 依赖）的定义**整体迁入 pregel 包**（新文件 `pending.go`，含其 MarshalJSON/UnmarshalJSON 与 pendingNodeJSON），store 侧删除定义、`GraphState.PendingNode` 字段与 `CreateState` 参数改为 `pregel.PendingNode`（不留别名）。登记偏差 D6。

**偏差总表（登记入 spec §7）：**

| 编号 | 偏差 | 理由 |
|------|------|------|
| D1 | 无条件注入 config（Python 按形参反射注入） | 承 8.3 D1 固定签名拍板 |
| D2 | `map[*nodeTask]*PregelNode` 替代 `Dict[asyncio.Task, PregelNode]`；goroutine+done channel 替代 asyncio.Task | Go 无 asyncio |
| D3 | `context.Canceled` 复刻 `asyncio.CancelledError` | Go 取消机制等价物 |
| D4 | Python sync/coroutine 双路调用消解为单路直调 | Go 静态签名 |
| D5 | `Submit(ctx, node, version)`/`CancelAll(ctx)` 增加 ctx 形参 | per-task 取消需要句柄（对应 asyncio Task.cancel） |
| D6 | `PendingNode` 定义迁入 pregel（store 不留别名） | 对应 Python 延迟导入破环意图；Go 无延迟导入 |

**关键语义锚点（实施前必读）：**

1. Python `NodeTask.run` 的 `graph_id=self.config.get(NS)` 读的是 **pool 的外部 PregelConfig 的 ns 键**（task.py:168），不是拼过 ns 的 inner_config —— Go 用 `t.config.Ns`（外部配置），**不是** `inner.Ns`。
2. 中断以 **panic** 传播：`NodeFunc` 纪律（node.go:19-23）明确「图级中断是 panic（`*GraphInterrupt`），由 8.3b 用 recover 转成返回值」。execute 的 recover 只认 `*GraphInterrupt`，其他 panic 原样重抛。
3. `_commit_failure`（task.py:32-38）同名节点**首记不覆盖**。
4. 取消记账（task.py:70-84 / 97-114）：被取消任务若结果带异常先打 warning（`event_type=graph_vertex_call_error`），然后无论结果如何都记 `CancelledError`（→ Go `context.Canceled`）入 failed；wait_all 的取消收尾在优先级上抛**之前**。
5. 优先级上抛（task.py:86-90）：`firstErr` 优先于 `interrupt`；cancel_all **不上抛**。
6. 日志（规则 3）：`logComponent = logger.ComponentAgentCore`（与 store/base.go 先例一致），Msg 用英文（"task cancelled with exception" / "running task with exception"）。

**构建/测试命令：** 单包 `go test ./internal/agentcore/graph/pregel/...`；全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（记忆 testing-build-tags）。

---

### Task 1: PendingNode 迁移破环（pregel/pending.go）

**Files:**
- Create: `internal/agentcore/graph/pregel/pending.go`
- Modify: `internal/agentcore/graph/store/base.go`（删 PendingNode 定义与两个 JSON 方法、改 GraphState/CreateState 类型引用）
- Modify: `internal/agentcore/graph/store/base_test.go`、`internal/agentcore/session/checkpointer/persistence_test.go`（引用改 pregel.PendingNode）
- Test: 随迁测试（store/base_test.go 中 PendingNode 相关用例改为经 pregel 包验证）

- [ ] **Step 1: 新建 pregel/pending.go（结构体区块 + JSON 形态）**

```go
package pregel

import (
	"encoding/json"
	"errors"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingNode 未完成节点的持久化记录。
// 物理归属偏差 D6：Python 定义于 store/base.py:26-29，task.py 以函数内延迟导入
// 破环（task.py:23/37）；Go 无延迟导入，本类型定义迁入 pregel 包、store 直接引用
// （不留别名）。本类型自身零 pregel 依赖，迁移安全。
type PendingNode struct {
	// NodeName 节点名
	NodeName string `json:"node_name"`
	// Status 任务状态（取值 TaskStatusInterrupt / TaskStatusError）
	Status string `json:"status"`
	// Exception 累积的异常列表（Python 默认 None，Go nil 等价；JSON 往返经 string 适配，偏差 D-8'）
	Exception []error `json:"exception,omitempty"`
}

// pendingNodeJSON PendingNode 的 JSON 形态（Exception 经 string 适配，偏差 D-8'）
type pendingNodeJSON struct {
	NodeName  string   `json:"node_name"`
	Status    string   `json:"status"`
	Exception []string `json:"exception"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MarshalJSON Exception []error → []string（Go error 接口 JSON 序列化静默丢内容，D-8）
func (p PendingNode) MarshalJSON() ([]byte, error) {
	aux := pendingNodeJSON{NodeName: p.NodeName, Status: p.Status}
	for _, e := range p.Exception {
		if e != nil {
			aux.Exception = append(aux.Exception, e.Error())
		}
	}
	return json.Marshal(aux)
}

// UnmarshalJSON []string → errors.New 重建；null → nil（对齐 Python exception 默认 None）
func (p *PendingNode) UnmarshalJSON(data []byte) error {
	var aux pendingNodeJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.NodeName, p.Status = aux.NodeName, aux.Status
	if aux.Exception == nil {
		p.Exception = nil
		return nil
	}
	p.Exception = make([]error, len(aux.Exception))
	for i, s := range aux.Exception {
		p.Exception[i] = errors.New(s)
	}
	return nil
}
```

- [ ] **Step 2: store/base.go 删除旧定义并改引用**

删除 base.go:14-30（PendingNode 结构体 + pendingNodeJSON）、base.go:125-152（两个 JSON 方法）。改三处引用为 `pregel.PendingNode`：
- `GraphState.PendingNode map[string]pregel.PendingNode`
- `Clone` 内 `c.PendingNode = make(map[string]pregel.PendingNode, ...)`
- `CreateState(..., pendingNode map[string]pregel.PendingNode, ...)` 与其内 `pendingNode = map[string]pregel.PendingNode{}`

- [ ] **Step 3: 改两个测试文件的引用**

- `store/base_test.go`：`map[string]PendingNode{...}` → `map[string]pregel.PendingNode{...}`（4 处）；PendingNode 自身 Marshal/Unmarshal 往返用例（base_test.go:179 附近）**移到** pregel 包新测试文件 `pending_test.go`（类型已不在 store）。
- `session/checkpointer/persistence_test.go:1473`：`store.PendingNode` → `pregel.PendingNode`。

- [ ] **Step 4: 验证**

Run: `go build ./... && go vet ./... && go test ./internal/agentcore/graph/pregel/... ./internal/agentcore/graph/store/... ./internal/agentcore/session/checkpointer/...`
Expected: 全部 PASS（pending_test.go 往返用例随包运行）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/graph/pregel/pending.go internal/agentcore/graph/pregel/pending_test.go internal/agentcore/graph/store/base.go internal/agentcore/graph/store/base_test.go internal/agentcore/session/checkpointer/persistence_test.go
git commit -m "refactor(pregel): PendingNode 迁入 pregel 破环（8.3b 前置，偏差 D6）"
```

---

### Task 2: nodeTask 执行体（TDD）

**Files:**
- Create: `internal/agentcore/graph/pregel/task.go`
- Test: `internal/agentcore/graph/pregel/task_test.go`

- [ ] **Step 1: 写失败测试（execute：成功/异常/中断三态 + ns 拼接 + EDGE_TRAVERSED）**

```go
package pregel

import (
	"context"
	"errors"
	"testing"
)

func TestNodeTask_成功返回消息(t *testing.T) {
	node := NewPregelNode("a", NoopNodeFunc, NewStaticRouter("b", "c"))
	cfg := &PregelConfig{Ns: "top"}
	task := &nodeTask{node: node, config: cfg, version: 1,
		ctx: context.Background(), done: make(chan nodeResult, 1)}
	msgs, err := task.execute()
	if err != nil {
		t.Fatalf("execute 报错: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Sender != "a" || msgs[0].Target != "b" {
		t.Fatalf("消息不符: %+v", msgs)
	}
	if msgs[0].Kind() != KindTrigger {
		t.Fatalf("消息种类不符: %v", msgs[0].Kind())
	}
}

func TestNodeTask_异常上抛(t *testing.T) {
	want := errors.New("boom")
	node := NewPregelNode("a", func(context.Context, NodeInput) error { return want })
	task := &nodeTask{node: node, config: &PregelConfig{}, version: 1,
		ctx: context.Background(), done: make(chan nodeResult, 1)}
	_, err := task.execute()
	if !errors.Is(err, want) {
		t.Fatalf("期望 boom，得 %v", err)
	}
}

func TestNodeTask_中断经recover降级为返回值(t *testing.T) {
	node := NewPregelNode("a", func(context.Context, NodeInput) error {
		PanicGraphInterrupt(Interrupt{Value: "v"})
		return nil
	})
	task := &nodeTask{node: node, config: &PregelConfig{}, version: 1,
		ctx: context.Background(), done: make(chan nodeResult, 1)}
	_, err := task.execute()
	var gi *GraphInterrupt
	if !errors.As(err, &gi) {
		t.Fatalf("期望 *GraphInterrupt，得 %v", err)
	}
}

func TestNodeTask_ns拼接双写(t *testing.T) {
	var got NodeInput
	node := NewPregelNode("s1", func(_ context.Context, in NodeInput) error { got = in; return nil })
	task := &nodeTask{node: node, version: 2,
		config: &PregelConfig{Ns: "main", RecursionLimit: 0}, // RecursionLimit 0 → CreateInnerConfig 补 10000
		ctx:    context.Background(), done: make(chan nodeResult, 1)}
	task.config.ParentNs = "main:Sub:3" // 内嵌结构体字段需在赋值后设置，见下方写法
	// 注意：上面两行合成一个复合字面量更稳，这里分两步仅为可读
	_, _ = task.execute()
	want := "main:Sub:3:s1:2"
	if got.Config.Ns != want || got.Config.ParentNs != want {
		t.Fatalf("ns 双写不符: ns=%q parent=%q", got.Config.Ns, got.Config.ParentNs)
	}
	if got.Config.RecursionLimit != MaxRecursiveLimit {
		t.Fatalf("递归上限未补默认: %d", got.Config.RecursionLimit)
	}
	if got.State != nil {
		t.Fatalf("State 恒 nil，得 %v", got.State)
	}
}

func TestNodeTask_顶层无ParentNs不拼(t *testing.T) {
	var got NodeInput
	node := NewPregelNode("s1", func(_ context.Context, in NodeInput) error { got = in; return nil })
	task := &nodeTask{node: node, version: 1,
		config: &PregelConfig{Ns: "main"}, ctx: context.Background(), done: make(chan nodeResult, 1)}
	_, _ = task.execute()
	if got.Config.Ns != "main" || got.Config.ParentNs != "" {
		t.Fatalf("顶层不应改写 ns: %+v", got.Config)
	}
}
```

⚠️ 注意 `nodeTask.config` 是 `*PregelConfig`：`&PregelConfig{...}.ParentNs = ...` 写法非法，测试里 ns 用例改为：

```go
	cfg := &PregelConfig{Ns: "main"}
	cfg.ParentNs = "main:Sub:3"
	task := &nodeTask{node: node, version: 2, config: cfg, ...}
```

（`ParentNs` 属嵌入的 `InnerPregelConfig` 而非 `PregelConfig`——所以 config 字段类型用 `*InnerPregelConfig` 更对：Python NodeTask 收的是 PregelConfig，run 里 create_inner_config 后改写。**拍板：`nodeTask.config` 类型为 `*InnerPregelConfig`**，pool 构造时经 `CreateInnerConfig(p.config)` 得到（补递归上限语义与 Python create_inner_config 时机一致）；ns 拼接用例相应为 `cfg.ParentNs = "main:Sub:3"`。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestNodeTask`
Expected: 编译失败 `undefined: nodeTask`

- [ ] **Step 3: 实现 nodeTask（task.go 前半）**

```go
package pregel

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// nodeResult 单个节点任务的交付物。
// err 为 *GraphInterrupt 时等价 Python「中断降级为返回值」（task.py:172-174）。
type nodeResult struct {
	// msgs 成功任务的路由消息
	msgs []Message
	// err 失败任务的上抛错误（含中断）
	err error
}

// nodeTask 单节点执行任务，对应 Python NodeTask（task.py:117-174）。
// 字段与 Python __init__ 一一对应（node/config/messages/version），
// ctx/cancel/done/finished 为方案 B 并发模型的承载件（偏差 D2/D5）。
type nodeTask struct {
	// node 执行目标节点
	node *PregelNode
	// config 外部配置经 CreateInnerConfig 归一化的内部配置（Python task.py:29 create 时机）
	config *InnerPregelConfig
	// messages 本次执行路由产出的消息（Python task.py:122）
	messages []Message
	// version 节点版本号，参与 ns 拼接
	version int
	// ctx 本任务的可取消执行上下文（对应 asyncio Task.cancel 的注入点）
	ctx context.Context
	// cancel 取消函数，由 pool 在 FIRST_EXCEPTION / CancelAll 路径调用
	cancel context.CancelFunc
	// done 结果交付通道（容量 1）
	done chan nodeResult
	// finished 标记 execute 已完成（CancelAll 判未完成任务用）
	finished atomic.Bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newNodeTask 构造任务（对应 Python NodeTask.__init__）。
func newNodeTask(node *PregelNode, config *InnerPregelConfig, version int, base context.Context) *nodeTask {
	ctx, cancel := context.WithCancel(base)
	return &nodeTask{
		node:    node,
		config:  config,
		version: version,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan nodeResult, 1),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// run 执行任务并交付结果（对应 Python NodeTask.run）。
// 返回值 Union[List[Message], GraphInterrupt] 由 nodeResult{msgs, err} 承载：
// 中断时 msgs=nil、err=*GraphInterrupt（经 execute 的 recover 转换）。
func (t *nodeTask) run() {
	msgs, err := t.execute()
	t.finished.Store(true)
	t.done <- nodeResult{msgs: msgs, err: err}
}

// execute 执行节点函数并收集路由消息（按 Python task.py:125-174 逐行）。
// 中断经 recover 降级：NodeFunc 纪律（node.go:19-23）——图级中断是 panic，
// 本处是唯一 recover 点，只认 *GraphInterrupt，其余 panic 原样重抛。
func (t *nodeTask) execute() (msgs []Message, err error) {
	defer func() {
		if r := recover(); r != nil {
			gi, ok := r.(*GraphInterrupt)
			if !ok {
				panic(r)
			}
			msgs, err = nil, gi
		}
	}()

	// Python task.py:139-147：形参含 config 时 create_inner_config 并按需拼 ns。
	// D1：Go 固定签名无条件注入；ParentNs 非空才拼接、双写（结构体化后直接改字段）。
	inner := *t.config
	if inner.ParentNs != "" {
		newFullNs := inner.ParentNs + ":" + t.node.name + ":" + strconv.Itoa(t.version)
		inner.Ns = newFullNs
		inner.ParentNs = newFullNs
	}

	// Python task.py:151-154 sync/coroutine 双路 → D4 单路直调；state 恒 nil（task.py:149）。
	if err := t.node.fn(t.ctx, NodeInput{Config: inner}); err != nil {
		return nil, err
	}

	// Python task.py:157-160：依次 dispatch 收集消息。
	t.messages = make([]Message, 0, len(t.node.routers))
	for _, r := range t.node.routers {
		out, derr := r.Dispatch(t.ctx, t.node.name)
		if derr != nil {
			return nil, derr
		}
		t.messages = append(t.messages, out...)
	}
	msgs = t.messages

	// Python task.py:162-168：逐条 EDGE_TRAVERSED 回调。
	// 锚点：graph_id 读外部配置的 Ns（self.config.get(NS)），不是拼过的 inner.Ns。
	if len(msgs) > 0 {
		for _, m := range msgs {
			callback.Trigger(t.ctx, string(callback.EdgeTraversed), map[string]any{
				"source_node": m.Sender,
				"target_node": m.Target,
				"graph_id":    t.config.Ns,
			})
		}
	}
	return msgs, nil
}
```

⚠️ `t.config.Ns`：`InnerPregelConfig` 嵌入 `PregelConfig`，`Ns` 提升字段可直接访问——但 config 是指针共享，pool 构造的 InnerPregelConfig 与 pool.config 对应；Python 读的也是同一个外部 config。保持一致。

- [ ] **Step 4: 补 EDGE_TRAVERSED 断言测试**

```go
func TestNodeTask_EdgeTraversed回调字段(t *testing.T) {
	calls := []map[string]any{}
	orig := callback.GetCallbackFramework()
	fw := callback.NewCallbackFramework()
	fw.RegisterCustom(string(callback.EdgeTraversed),
		func(ctx context.Context, data map[string]any) (any, error) {
			calls = append(calls, data)
			return nil, nil
		})
	callback.SetCallbackFramework(fw)
	defer callback.SetCallbackFramework(orig)
	// framework 的注册/替换 API 名以 internal/agentcore/runner/callback 实际导出为准，
	// 若无 SetCallbackFramework 则用 fw := GetCallbackFramework() 直接挂 handler 后摘除。

	node := NewPregelNode("a", NoopNodeFunc, NewStaticRouter("b"))
	task := &nodeTask{node: node, config: &InnerPregelConfig{PregelConfig: PregelConfig{Ns: "top"}},
		version: 1, ctx: context.Background(), done: make(chan nodeResult, 1)}
	_, err := task.execute()
	if err != nil {
		t.Fatalf("execute 报错: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("回调次数 %d != 1", len(calls))
	}
	if calls[0]["source_node"] != "a" || calls[0]["target_node"] != "b" || calls[0]["graph_id"] != "top" {
		t.Fatalf("回调字段不符: %+v", calls[0])
	}
}
```

（回调框架的挂接 API 实施时按 `callback` 包实际导出调整——`RegisterCustom`/全局替换的方式以包内既有测试的写法为准，grep `callback.` 的 `_test.go` 用例照抄其惯用法。）

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestNodeTask -v`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/graph/pregel/task.go internal/agentcore/graph/pregel/task_test.go
git commit -m "feat(pregel): 8.3b nodeTask 执行体（ns 拼接/recover 中断降级/EDGE_TRAVERSED）"
```

---

### Task 3: TaskExecutorPool 基础（Submit/Clear/commitFailure）

**Files:**
- Modify: `internal/agentcore/graph/pregel/task.go`
- Test: `internal/agentcore/graph/pregel/task_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestPool_Submit与Clear(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{Ns: "top"})
	pool.Submit(context.Background(), NewPregelNode("a", NoopNodeFunc), 1)
	if len(pool.runningTasks) != 1 {
		t.Fatalf("runningTasks 应有 1 项")
	}
	// 清空前必须先收割结果，否则 goroutine 永久阻塞在 done 发送
	if err := pool.WaitAll(); err != nil {
		t.Fatalf("WaitAll 报错: %v", err)
	}
	if len(pool.succeedMessages) != 0 {
		t.Fatalf("无路由应零消息，得 %d", len(pool.succeedMessages))
	}
	pool.Clear()
	if len(pool.runningTasks) != 0 || len(pool.failed) != 0 || len(pool.succeedMessages) != 0 {
		t.Fatalf("Clear 后应全空")
	}
}

func TestPool_commitFailure首记不覆盖(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	node := NewPregelNode("a", NoopNodeFunc)
	pool.commitFailure(node, errors.New("first"))
	pool.commitFailure(node, errors.New("second"))
	pn := pool.failed["a"]
	if len(pn.Exception) != 1 || pn.Exception[0].Error() != "first" {
		t.Fatalf("应首记不覆盖: %+v", pn)
	}
	if pn.Status != TaskStatusError {
		t.Fatalf("状态应为 __error__: %q", pn.Status)
	}
}

func TestPool_commitFailure中断状态(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	node := NewPregelNode("a", NoopNodeFunc)
	pool.commitFailure(node, &GraphInterrupt{Interrupts: []Interrupt{{Value: "v"}}})
	if pool.failed["a"].Status != TaskStatusInterrupt {
		t.Fatalf("状态应为 __interrupt__: %q", pool.failed["a"].Status)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestPool`
Expected: `undefined: NewTaskExecutorPool`

- [ ] **Step 3: 实现 pool 结构体与三方法（task.go 后半）**

```go
// ──────────────────────────── 结构体 ────────────────────────────
// （续 task.go，位于 nodeTask 之前，符合规范 2 结构体区块聚合）

// TaskExecutorPool 单 step 的节点任务池（Python TaskExecutorPool，task.py:19-114）。
// 非并发安全约定：engine 单线程逐 step 调用 Submit→WaitAll（对齐 Python 事件循环
// 串行语义）；runningTasks 的 Submit/WaitAll/CancelAll 交叉访问用 mu 兜底，
// succeedMessages/failed 由 Python 同样仅 wait_all/cancel_all/clear 触碰，不加锁。
type TaskExecutorPool struct {
	// config 外部配置（Python task.py:21）
	config *PregelConfig
	// succeedMessages 成功任务的路由消息（Python succeed_messages）
	succeedMessages []Message
	// failed 失败节点记账（Python failed）
	failed map[string]PendingNode
	// runningTasks 在途任务→节点反查表（Python running_tasks，偏差 D2）
	runningTasks map[*nodeTask]*PregelNode
	// mu 保护 runningTasks 的 Submit/WaitAll/CancelAll 交叉访问
	mu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskExecutorPool 构造任务池（Python task.py:20-25）。
func NewTaskExecutorPool(config *PregelConfig) *TaskExecutorPool {
	return &TaskExecutorPool{
		config:       config,
		failed:       make(map[string]PendingNode),
		runningTasks: make(map[*nodeTask]*PregelNode),
	}
}

// Submit 提交节点执行任务（Python task.py:27-30）。
// D5：ctx 形参承载 asyncio create_task 的取消句柄；每任务派生可取消子 ctx。
func (p *TaskExecutorPool) Submit(ctx context.Context, node *PregelNode, version int) {
	inner := CreateInnerConfig(*p.config) // 对齐 Python task.py:29 的 create 时机
	t := newNodeTask(node, &inner, version, ctx)
	p.mu.Lock()
	p.runningTasks[t] = node
	p.mu.Unlock()
	go t.run()
}

// Clear 清空三容器（Python task.py:92-95）。
func (p *TaskExecutorPool) Clear() {
	p.succeedMessages = nil
	p.failed = make(map[string]PendingNode)
	p.mu.Lock()
	p.runningTasks = make(map[*nodeTask]*PregelNode)
	p.mu.Unlock()
}

// commitFailure 失败记账，同名首记不覆盖（Python task.py:32-38）。
func (p *TaskExecutorPool) commitFailure(node *PregelNode, err error) {
	name := node.name
	if _, ok := p.failed[name]; ok {
		return
	}
	status := TaskStatusError
	var gi *GraphInterrupt
	if errors.As(err, &gi) {
		status = TaskStatusInterrupt
	}
	p.failed[name] = PendingNode{NodeName: name, Status: status, Exception: []error{err}}
}
```

⚠️ `nodeTask.config` 类型定为 `*InnerPregelConfig` 后，Task 2 代码的 `newNodeTask(node, config, version, base)` 签名一致；Task 2 测试里 `config: &InnerPregelConfig{PregelConfig: PregelConfig{Ns: "main"}}`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestPool -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/graph/pregel/task.go internal/agentcore/graph/pregel/task_test.go
git commit -m "feat(pregel): 8.3b TaskExecutorPool Submit/Clear/commitFailure"
```

---

### Task 4: WaitAll（FIRST_EXCEPTION + 取消路径 + 优先级上抛）

**Files:**
- Modify: `internal/agentcore/graph/pregel/task.go`
- Test: `internal/agentcore/graph/pregel/task_test.go`

- [ ] **Step 1: 写失败测试（四态）**

```go
func TestWaitAll_单节点异常上抛(t *testing.T) {
	want := errors.New("boom")
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("a",
		func(context.Context, NodeInput) error { return want }), 1)
	err := pool.WaitAll()
	if !errors.Is(err, want) {
		t.Fatalf("应上抛 boom，得 %v", err)
	}
	if pool.failed["a"].Status != TaskStatusError {
		t.Fatalf("失败记账缺失")
	}
}

func TestWaitAll_中断上抛(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("a",
		func(context.Context, NodeInput) error { PanicGraphInterrupt(Interrupt{Value: "v"}); return nil }), 1)
	err := pool.WaitAll()
	var gi *GraphInterrupt
	if !errors.As(err, &gi) {
		t.Fatalf("应上抛中断，得 %v", err)
	}
	if pool.failed["a"].Status != TaskStatusInterrupt {
		t.Fatalf("中断记账缺失")
	}
}

func TestWaitAll_兄弟并发异常取消成功任务(t *testing.T) {
	// b 先异常 → a 被取消（fn 响应 ctx 取消）→ a 记 context.Canceled
	pool := NewTaskExecutorPool(&PregelConfig{})
	slowDone := make(chan struct{})
	pool.Submit(context.Background(), NewPregelNode("a",
		func(ctx context.Context, NodeInput) error {
			<-ctx.Done() // 协作取消：对齐 asyncio 在 await 点注入 CancelledError
			close(slowDone)
			return ctx.Err()
		}), 1)
	pool.Submit(context.Background(), NewPregelNode("b",
		func(context.Context, NodeInput) error { return errors.New("boom") }), 1)
	err := pool.WaitAll()
	if err == nil || err.Error() != "boom" {
		t.Fatalf("应上抛 boom，得 %v", err)
	}
	<-slowDone
	pn := pool.failed["a"]
	if pn.Status != TaskStatusError || pn.Exception[0] != context.Canceled {
		t.Fatalf("被取消任务应记 context.Canceled: %+v", pn)
	}
}

func TestWaitAll_同轮异常优先于中断(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("a",
		func(context.Context, NodeInput) error { PanicGraphInterrupt(Interrupt{Value: "i"}); return nil }), 1)
	pool.Submit(context.Background(), NewPregelNode("b",
		func(context.Context, NodeInput) error { return errors.New("boom") }), 1)
	err := pool.WaitAll()
	if err == nil || err.Error() != "boom" {
		t.Fatalf("普通异常应优先于中断，得 %v", err)
	}
}

func TestWaitAll_成功消息收集(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("a", NoopNodeFunc, NewStaticRouter("b", "c")), 1)
	if err := pool.WaitAll(); err != nil {
		t.Fatalf("WaitAll 报错: %v", err)
	}
	if len(pool.succeedMessages) != 2 {
		t.Fatalf("成功消息应 2 条，得 %d", len(pool.succeedMessages))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestWaitAll`
Expected: `pool.WaitAll undefined`

- [ ] **Step 3: 实现 WaitAll**

```go
// WaitAll 等待全部在途任务收场，FIRST_EXCEPTION 语义（Python task.py:40-90）。
// 1) 任一任务出错（含中断）即取消其余在途任务（对应 asyncio.wait FIRST_EXCEPTION
//    提前返回 + pending.cancel()）；2) 被取消任务记 context.Canceled（D3），
//    结果带异常先打 warning（task.py:74-80）；3) 普通异常 > 中断 优先上抛（task.py:86-90）。
// 取消是协作式的：NodeFunc 必须响应 ctx（对齐 Python 协程在 await 点注入
// CancelledError；无 await 的 fn 忽略取消则本轮等待至其自然完成——Python 同性质）。
func (p *TaskExecutorPool) WaitAll() error {
	p.mu.Lock()
	total := len(p.runningTasks)
	p.mu.Unlock()
	if total == 0 {
		return nil
	}

	var (
		firstErr        error
		interruptErr    *GraphInterrupt
		cancelInitiated bool
	)
	received := make(map[*nodeTask]nodeResult, total)
	cancelled := make(map[*nodeTask]bool, total)

	for len(received) < total {
		// 就绪通道快照：每轮重建 select 集（对应 asyncio.wait 的 tasks 列表）
		p.mu.Lock()
		pending := make([]*nodeTask, 0, len(p.runningTasks))
		for t := range p.runningTasks {
			if _, ok := received[t]; !ok {
				pending = append(pending, t)
			}
		}
		p.mu.Unlock()

		cases := make([]reflect.SelectCase, len(pending))
		for i, t := range pending {
			cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(t.done)}
		}
		chosen, val, _ := reflect.Select(cases)
		t := pending[chosen]
		res := val.Interface().(nodeResult)
		received[t] = res
		p.mu.Lock()
		node := p.runningTasks[t]
		p.mu.Unlock()

		if cancelInitiated {
			// 取消收尾分支（Python task.py:74-84 的 gather 段）
			cancelled[t] = true
			if res.err != nil {
				logger.Warn(logComponent).
					Str("event_type", "graph_vertex_call_error").
					Str("error", res.err.Error()).
					Msg("task cancelled with exception")
			}
			continue
		}

		if res.err != nil {
			p.commitFailure(node, res.err)
			var gi *GraphInterrupt
			if errors.As(res.err, &gi) {
				if interruptErr == nil {
					interruptErr = gi
				}
			} else if firstErr == nil {
				firstErr = res.err
			}
			// FIRST_EXCEPTION：尚有在途 → 进入取消路径（task.py:70-72）
			if len(received) < total {
				cancelInitiated = true
				for _, ot := range pending {
					if ot != t {
						ot.cancel()
						cancelled[ot] = true
					}
				}
			}
		} else {
			p.succeedMessages = append(p.succeedMessages, res.msgs...)
		}
	}

	// 取消记账（Python task.py:81-84）：无论结果如何都记 CancelledError 等价物
	for t := range cancelled {
		p.commitFailure(t.node, context.Canceled)
	}

	if firstErr != nil {
		return firstErr
	}
	if interruptErr != nil {
		return interruptErr
	}
	return nil
}
```

新增 import：`reflect`、`sync`、`github.com/uapclaw/uapclaw-go/internal/common/logger`。task.go 内 `const logComponent = logger.ComponentAgentCore`（常量区块）。

- [ ] **Step 4: 跑测试 + race**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestWaitAll -v -race && go test ./internal/agentcore/graph/pregel/ -race`
Expected: 全部 PASS，race 干净

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/graph/pregel/task.go internal/agentcore/graph/pregel/task_test.go
git commit -m "feat(pregel): 8.3b WaitAll FIRST_EXCEPTION 语义（取消路径+优先级上抛）"
```

---

### Task 5: CancelAll

**Files:**
- Modify: `internal/agentcore/graph/pregel/task.go`
- Test: `internal/agentcore/graph/pregel/task_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestCancelAll_取消未完成并记账(t *testing.T) {
	fin := make(chan struct{})
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("slow",
		func(ctx context.Context, NodeInput) error {
			<-ctx.Done()
			close(fin)
			return ctx.Err()
		}), 1)
	pool.Submit(context.Background(), NewPregelNode("fast", NoopNodeFunc), 1)
	pool.CancelAll()
	<-fin
	if len(pool.runningTasks) != 0 {
		t.Fatalf("runningTasks 应清空")
	}
	pn := pool.failed["slow"]
	if pn.Status != TaskStatusError || pn.Exception[0] != context.Canceled {
		t.Fatalf("被取消任务应记 context.Canceled: %+v", pn)
	}
	if _, ok := pool.failed["fast"]; ok {
		t.Fatalf("已完成的 fast 不应记账（Python cancel_all 只处理 to_cancel）")
	}
}

func TestCancelAll_空池安全(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.CancelAll() // 不 panic 即通过
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestCancelAll`
Expected: `pool.CancelAll undefined`

- [ ] **Step 3: 实现 CancelAll**

```go
// CancelAll 取消全部在途任务并清空 runningTasks（Python task.py:97-114）。
// 不上抛：被取消任务结果带异常仅打 warning（task.py:104-109）；
// 已完成任务（fast）不入 to_cancel、不记账（Python 只 pop 被取消的）。
func (p *TaskExecutorPool) CancelAll() {
	p.mu.Lock()
	toCancel := make([]*nodeTask, 0, len(p.runningTasks))
	for t := range p.runningTasks {
		if !t.finished.Load() {
			t.cancel()
			toCancel = append(toCancel, t)
		}
	}
	p.mu.Unlock()

	for _, t := range toCancel {
		res := <-t.done // 对齐 asyncio.gather(return_exceptions=True) 的等待
		if res.err != nil {
			logger.Warn(logComponent).
				Str("event_type", "graph_vertex_call_error").
				Str("error", res.err.Error()).
				Msg("running task with exception")
		}
		p.commitFailure(t.node, context.Canceled)
	}
	p.mu.Lock()
	p.runningTasks = make(map[*nodeTask]*PregelNode)
	p.mu.Unlock()
}
```

⚠️ 已完成但结果未收割的任务（fast）：其 goroutine 的 done 发送已成功（WaitAll 之外独立），`finished=true` 不入 toCancel；其 done 通道无人读也不泄漏（容量 1，发送方不阻塞）。CancelAll 清空 runningTasks 后这些结果与 Python 一样被丢弃（不进 succeed_messages）。

- [ ] **Step 4: 跑测试 + race**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestCancelAll -v -race && go test ./internal/agentcore/graph/pregel/ -race`
Expected: PASS，race 干净

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/graph/pregel/task.go internal/agentcore/graph/pregel/task_test.go
git commit -m "feat(pregel): 8.3b CancelAll（取消记账+清空在途表）"
```

---

### Task 6: 文档同步 + 全量回归收尾

**Files:**
- Modify: `internal/agentcore/graph/pregel/doc.go`（文件树加 pending.go / task.go，两行一职责）
- Modify: `docs/superpowers/specs/2026-11-19-task-executor-pool-8.3b-design.md`（§2 前置件表 PendingNode 行改「8.9a 落地 + 本节 D6 迁入 pregel」；§3 `runningTasks` 类型改 `map[*nodeTask]*PregelNode`；§7 偏差表补 D6、D5 修为 Submit(ctx)/CancelAll；§6 改为「中断以 panic 传递，execute recover 转返回值（承 node.go NodeFunc 纪律），pool 侧 errors.As 判型」）
- Modify: `IMPLEMENTATION_PLAN.md` 8.3b 行 ☐→✅ 并写 Go 事实（含 D1-D6、pending.go 迁移、覆盖率数字）

- [ ] **Step 1: 更新 doc.go 文件树**：`pending.go # PendingNode（自 store 迁入，破环偏差 D6）`、`task.go # TaskExecutorPool/NodeTask 单 step 任务池与执行体`

- [ ] **Step 2: spec 按上方清单同步**（保持与实现一致，三处均为事实性修正）

- [ ] **Step 3: 覆盖率确认**

Run: `go test -cover ./internal/agentcore/graph/pregel/`
Expected: task.go/pending.go ≥85%（包整体维持 ≥90%）

- [ ] **Step 4: 全量回归**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 零 FAIL

- [ ] **Step 5: Commit 并推送**

```bash
git add internal/agentcore/graph/pregel/doc.go docs/superpowers/specs/2026-11-19-task-executor-pool-8.3b-design.md IMPLEMENTATION_PLAN.md
git commit -m "docs(pregel): 8.3b 完成——spec 偏差同步/IMPLEMENTATION_PLAN 状态回写/doc.go 文件树"
git push
```

---

## Self-Review 结论

- **Spec 覆盖**：§3 类型对照（Task 2/3）、§4 方案 B 并发（Task 4/5）、§5 run 步骤（Task 2）、§6 中断判定（Task 2 recover + spec 修正入 Task 6）、§8 测试 11 用例分布于 Task 2-5、§9 完成标准（Task 6）。循环依赖阻塞经用户拍板方案①入 Task 1。
- **占位符**：Task 2 Step 4 的回调挂接 API 标注了「以 callback 包实际导出为准」——这是事实核查项而非空白（包内已有测试惯用法可照抄），实施时落实。
- **类型一致**：`nodeTask.config *InnerPregelConfig` 已在 Task 2/3 两处统一；`nodeResult{msgs, err}` 各任务一致；`Submit(ctx, node, version)` 与测试一致。
