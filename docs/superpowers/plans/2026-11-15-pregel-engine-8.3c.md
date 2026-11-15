# 8.3c Pregel 调度引擎 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans（用户已选 inline execution）。Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `openjiuwen/core/graph/pregel/engine.py`，落地 PregelLoop 超步调度引擎 + Pregel.Run + 回填 builder.Build()。

**Architecture:** 同包 `internal/agentcore/graph/pregel` 新增 `engine.go`（PregelLoop/Pregel/RecursionError）与 `state.go`（B5 类型迁移：GraphState/Store/CreateState 自 store 包迁入）；channels.go 加锁（D1）；builder.go 回填 Build()。步骤与 Python 一比一（规则 9/12）。

**Tech Stack:** Go 1.x、`internal/common/exception`、`internal/common/logger`、`runner/callback`。

**Spec:** docs/superpowers/specs/2026-11-15-pregel-engine-8.3c-design.md（偏差 B1-B6）

**Python 源:** `/home/opensource/agent-core/openjiuwen/core/graph/pregel/engine.py`（285 行，逐行对照）

---

### Task 1: exception 补 112104 状态码

**Files:**
- Modify: `internal/common/exception/codes_graph.go`

- [ ] **Step 1: 在 Pregel 段（112103 之后）追加状态码**

```go
	// StatusPregelGraphSuperStepExecutionError Pregel 图超步执行失败
	StatusPregelGraphSuperStepExecutionError = NewStatusCode(
		"PREGEL_GRAPH_SUPER_STEP_EXECUTION_ERROR", 112104,
		"graph super-step failed, step={step}, error='{reason}'")
```

- [ ] **Step 2: 验证** `go build ./internal/common/exception/ && go vet ./internal/common/exception/`
- [ ] **Step 3: Commit** `git add internal/common/exception/codes_graph.go && git commit -m "feat(exception): 补 112104 PREGEL_GRAPH_SUPER_STEP_EXECUTION_ERROR"`

### Task 2: RecursionError 类型

**Files:**
- Create: `internal/agentcore/graph/pregel/engine.go`（骨架，仅 RecursionError）
- Test: `internal/agentcore/graph/pregel/engine_test.go`

- [ ] **Step 1: 失败测试**

```go
package pregel

import (
	"errors"
	"testing"
)

// TestRecursionError_Error 文案复刻 Python engine.py:139-141
func TestRecursionError_Error(t *testing.T) {
	e := &RecursionError{Limit: 25, Step: 26}
	assert.Equal(t, "Recursion limit of 25 reached at step 26", e.Error())
	var target *RecursionError
	ok := errors.As(error(e), &target)
	assert.True(t, ok)
}
```

（`assert` 用项目测试惯例：`github.com/stretchr/testify/assert`，与包内既有测试一致。）

- [ ] **Step 2: 运行确认失败** `go test ./internal/agentcore/graph/pregel/ -run TestRecursionError`，期望 undefined: RecursionError
- [ ] **Step 3: 实现**

```go
// RecursionError 递归上限超限（Python 内置 RecursionError 的 Go 等价物，spec D3）。
// engine.py:85 按类型名分类上抛，Go 以 errors.As 类型断言还原该契约。
type RecursionError struct {
	// Limit 递归上限（max_step）
	Limit int
	// Step 触发时的步号
	Step int
}

// Error 文案复刻 engine.py:139-141
func (e *RecursionError) Error() string {
	return fmt.Sprintf("Recursion limit of %d reached at step %d", e.Limit, e.Step)
}
```

- [ ] **Step 4: 测试通过** `-run TestRecursionError` PASS
- [ ] **Step 5: Commit**

### Task 3: B5 类型迁移（GraphState/Store/CreateState → pregel）

**Files:**
- Create: `internal/agentcore/graph/pregel/state.go`
- Modify: `internal/agentcore/graph/store/base.go`（删迁出符号，改引用 pregel）
- Modify: `internal/agentcore/graph/store/inmemory.go`、`serde.go`、`doc.go`（限定名替换）
- Modify: `internal/agentcore/session/checkpointer/persistence.go`、`inmemory.go`、`serializer.go`、`internal/agentcore/session/interfaces/interfaces.go`（`store.Store`→`pregel.Store`、`store.GraphState`→`pregel.GraphState`，含各 `_test.go`）

- [ ] **Step 1: 把 store/base.go 中 `GraphState`（含 `IsResumable`/`Clone` 两方法）、`Store` 接口、`CreateState`、`logComponent` 常量**整段迁入新文件 `pregel/state.go`（包名改 `pregel`，import 去掉 pregel 自引用，字段类型 `ChannelSnapshot/Message/PendingNode` 去限定名）。`IsResumable` 注释「与 8.3c PregelLoop._is_resume 同口径」保留。
- [ ] **Step 2: store/base.go** 删除上述符号，`GraphStore` 的 saver 类型改 `pregel.Store`，Get/Save/Delete 签名中的 `*GraphState` 改 `*pregel.GraphState`；文件头注释补一行「GraphState/Store/CreateState 因 8.3c import 环迁入 pregel（spec B5，与 8.3b D6 同源）」。
- [ ] **Step 3: store 包其余文件与全部消费方** `sed` 级替换：`store.Store`→`pregel.Store`、`store.GraphState`→`pregel.GraphState`，按需补 import。
- [ ] **Step 4: 验证** `go build ./... && go vet ./...`
- [ ] **Step 5: 包测试** `go test ./internal/agentcore/graph/... ./internal/agentcore/session/checkpointer/...`
- [ ] **Step 6: Commit**

### Task 4: ChannelManager 加锁（D1）

**Files:**
- Modify: `internal/agentcore/graph/pregel/channels.go`
- Test: `internal/agentcore/graph/pregel/channels_test.go`（追加并发用例）

- [ ] **Step 1: ChannelManager 增加字段 `mu sync.Mutex`；`BufferMessage/IsEmpty/Flush/GetReadyNodes/Consume/Buffer/Snapshot/Restore` 八方法开头 `m.mu.Lock(); defer m.mu.Unlock()`（NewChannelManager 构造期不加锁）；并发纪律注释（偏差 D-8 段）改为：「8.3c 拍板方案 A：各公开方法加互斥锁（Python asyncio 单线程无需锁，Go 防御性加锁，spec B2），-race 并发用例锁定」**
- [ ] **Step 2: 并发测试**

```go
// TestChannelManager_并发安全 多 goroutine 读写与主循环并发无竞态（-race 门禁）
func TestChannelManager_并发安全(t *testing.T) {
	m := NewChannelManager([]Channel{NewTriggerChannel("a"), NewTriggerChannel(End)})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				m.BufferMessage(NewTriggerMessage("a", "a"))
				_ = m.Snapshot()
				_ = m.GetReadyNodes()
				_ = m.Buffer()
				_ = m.IsEmpty()
				_ = m.Flush()
				m.Consume("a")
				m.Restore(map[string][]ChannelSnapshot{"a": {{Messages: []Message{NewTriggerMessage("a", "a")}}}})
			}
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 3: `-race` 验证** `go test -race ./internal/agentcore/graph/pregel/ -run TestChannelManager_并发安全` PASS
- [ ] **Step 4: Commit**

### Task 5: PregelLoop 结构体 + Init

**Files:**
- Modify: `internal/agentcore/graph/pregel/engine.go`
- Test: `internal/agentcore/graph/pregel/engine_test.go`

- [ ] **Step 1: 失败测试**（fake saver，复用 Task 3 迁移后的 `pregel.Store`）

```go
type fakeSaver struct {
	state *GraphState
	getErr error
	saved *GraphState
}

func (f *fakeSaver) Get(ctx context.Context, sessionID, ns string) (*GraphState, error) {
	if f.getErr != nil { return nil, f.getErr }
	return f.state, nil
}
func (f *fakeSaver) Save(ctx context.Context, sessionID, ns string, s *GraphState) error {
	f.saved = s
	return nil
}
func (f *fakeSaver) Delete(ctx context.Context, sessionID, ns string) error { return nil }

// TestPregelLoop_Init_首跑 触发 START 消息并 flush（engine.py:59-62）
func TestPregelLoop_Init_首跑(t *testing.T) {
	pg := testGraph() // START→A→END 三节点图，见 Task 7 前置 helper
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{RecursionLimit: 25}})
	err := l.Init(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, l.Executor)
	assert.Equal(t, 25, l.MaxStep)
	assert.Equal(t, []string{"__start__"}, readyTargetsOf(l.Manager)) // A 通道就绪
}

// TestPregelLoop_Init_恢复 state 可恢复则逐项回填（engine.py:45-58）
func TestPregelLoop_Init_恢复(t *testing.T) {
	sv := &fakeSaver{state: &GraphState{
		Step: 3, ChannelValues: map[string][]ChannelSnapshot{
			"a": {{Messages: []Message{NewTriggerMessage("x", "a")}}}},
		NodeVersion: map[string]int{"a": 2},
		PendingBuffer: []Message{NewTriggerMessage("a", "b")},
		PendingNode: map[string]PendingNode{"z": {NodeName: "z", Status: TaskStatusError}},
	}}
	pg := testGraph()
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{SessionID: "s", Ns: "n", RecursionLimit: 25}, ParentNs: ""})
	l.Saver = sv
	assert.NoError(t, l.Init(context.Background()))
	assert.Equal(t, 3, l.Step)
	assert.Equal(t, 3+25, l.MaxStep)
	assert.Equal(t, map[string]int{"a": 2}, l.NodeVersion)
	assert.Equal(t, map[string]PendingNode{"z": {NodeName: "z", Status: TaskStatusError}}, l.retryPendingNodes)
}
```

`testGraph()` helper：`NewPregelBuilder().AddNode("a", NoopNodeFunc).AddEdge(Start, "a").AddEdge("a", End).Build(nil, nil)`（Task 8 之前 Build 未回填——本 helper 延后到 Task 8 落地后再补，或本 Task 先手工构造 `NewPregel(map, channels, Start, nil, nil)`）。**决定：Task 5 用手工构造**，避免任务间依赖倒挂。

- [ ] **Step 2: 实现 PregelLoop 骨架 + Init（engine.py:25-62 一比一）**

```go
// PregelLoop 单次图执行的超步循环（Python engine.py:25-205）。
// 字段对齐 Python 公开属性（spec D6）：回调方跨包可读。
type PregelLoop struct {
	// Graph 执行中的图（engine.py:27 self.graph）
	Graph *Pregel
	// Manager 通道管理器（engine.py:28 self.manager）
	Manager *ChannelManager
	// Step 当前步号（engine.py:29 self.step）
	Step int
	// MaxStep 本轮步数上限（engine.py:30 self.max_step）
	MaxStep int
	// Config 内部配置（engine.py:31 self.config；实收 inner_config，见 engine.py:248 与 8.3b 审查修正）
	Config *InnerPregelConfig
	// Saver 检查点存储（engine.py:32 self.saver）
	Saver Store
	// ActiveNodes 本超步活跃节点（engine.py:33 self.active_nodes）
	ActiveNodes []string
	// Executor 任务池（engine.py:34 self.executor）
	Executor *TaskExecutorPool
	// NodeVersion 节点版本表（engine.py:37 defaultdict(int)；B4：Go map 零值即 0）
	NodeVersion map[string]int
	// retryPendingNodes 待重试节点（engine.py:35 self._retry_pending_nodes，非导出）
	retryPendingNodes map[string]PendingNode
}

// NewPregelLoop 构造循环（engine.py:26-37）
func NewPregelLoop(graph *Pregel, config *InnerPregelConfig) *PregelLoop {
	return &PregelLoop{
		Graph:       graph,
		Manager:     NewChannelManager(graph.Channels),
		Config:      config,
		Saver:       graph.Store,
		NodeVersion: make(map[string]int),
	}
}

// Init 初始化：构造任务池、恢复检查点或触发 START（engine.py:39-62）
func (l *PregelLoop) Init(ctx context.Context) error {
	// engine.py:40-41
	l.Executor = NewTaskExecutorPool(l.Config)
	l.MaxStep = l.Config.RecursionLimit
	// engine.py:42-44
	var state *GraphState
	if l.Config.SessionID != "" && l.Config.Ns != "" && l.Saver != nil {
		var err error
		state, err = l.Saver.Get(ctx, l.Config.SessionID, l.Config.Ns)
		if err != nil {
			return err
		}
	}
	// engine.py:45-58
	if isResume(state) {
		// Restore barrier channel
		l.Manager.Restore(state.ChannelValues)
		// restore loop node version
		l.NodeVersion = state.NodeVersion
		// Restore step
		l.Step = state.Step
		l.MaxStep = state.Step + l.Config.RecursionLimit
		// Restore task result message
		for _, msg := range state.PendingBuffer {
			l.Manager.BufferMessage(msg)
		}
		// Pending node
		if state.PendingNode != nil {
			l.retryPendingNodes = state.PendingNode
		}
		return nil
	}
	// engine.py:59-62 Trigger start node
	l.Manager.BufferMessage(NewTriggerMessage(l.Graph.Initial, l.Graph.Initial))
	return l.Manager.Flush()
}

// isResume 恢复判定（engine.py:94-96 staticmethod；归属为类型级语义，Go 作包级函数）
func isResume(state *GraphState) bool {
	return state != nil && state.IsResumable()
}
```

- [ ] **Step 3: 测试通过 + Commit**

### Task 6: RunStep / runStep / saveStateOnError

**Files:** 同 Task 5

- [ ] **Step 1: 失败测试**（核心四例：happy path 走到 END 返回 false；仅 flush 步；递归上限；错误保存后 resume 重试 pending_node）

```go
// TestPregelLoop_RunStep_递归上限（engine.py:138-141）
func TestPregelLoop_RunStep_递归上限(t *testing.T) {
	pg := handGraph() // 手工构造单节点图（同 Task 5 helper）
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{RecursionLimit: 0}}) // 0→CreateInnerConfig 补 10000；此处直接 inner.MaxStep 覆盖
	l.MaxStep = 2 // 直接置小，规避 helper 复杂度
	l.Step = 3
	_, err := l.RunStep(context.Background())
	var re *RecursionError
	assert.True(t, errors.As(err, &re))
	assert.Equal(t, 2, re.Limit)
}

// TestPregelLoop_RunStep_错误保存 主循环错误时状态落盘（engine.py:82/173-205）
func TestPregelLoop_RunStep_错误保存(t *testing.T) {
	sv := &fakeSaver{}
	pg := failingGraph() // 节点 a 的 NodeFunc 返回 errors.New("boom")
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{SessionID: "s", Ns: "n"}})
	l.Saver = sv
	_ = l.Init(context.Background())
	_, err := l.RunStep(context.Background())
	assert.Error(t, err)
	var be *exception.BaseError
	assert.True(t, errors.As(err, &be)) // 非 GraphInterrupt/BaseError/RecursionError → 包 112104
	assert.Equal(t, 112104, be.Code())
	assert.NotNil(t, sv.saved)                 // _save_state_on_error 落盘
	assert.NotEmpty(t, sv.saved.PendingNode)   // failed 记账进 pending_node
}

// TestPregelLoop_RunStep_中断保存不包装 GraphInterrupt 原样上抛（engine.py:85-86）
func TestPregelLoop_RunStep_中断保存不包装(t *testing.T) {
	pg := interruptingGraph() // NodeFunc 内 PanicGraphInterrupt(Interrupt{Value: "x"})
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{SessionID: "s", Ns: "n"}})
	l.Saver = &fakeSaver{}
	_ = l.Init(context.Background())
	_, err := l.RunStep(context.Background())
	var gi *GraphInterrupt
	assert.True(t, errors.As(err, &gi))
	assert.Equal(t, "x", gi.Interrupts[0].Value)
}
```

- [ ] **Step 2: 实现 RunStep（engine.py:64-92 一比一两层结构）**

```go
// RunStep 执行一个超步，false 表示图结束（engine.py:64-92）。
// 两层结构一比一：外层错误处理边界，内层 runStep 步骤。
func (l *PregelLoop) RunStep(ctx context.Context) (bool, error) {
	err := l.runStep(ctx)
	if err == nil {
		return true, nil
	}
	// engine.py:68-81 非 GraphInterrupt 才打 GRAPH_SUPER_STEP_ERROR
	var gi *GraphInterrupt
	if !errors.As(err, &gi) {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_SUPER_STEP_ERROR").
			Err(err).
			Str("graph_id", l.Config.Ns).
			Str("session_id", l.Config.SessionID).
			Str("ns", l.Config.Ns).
			Int("step", l.Step).
			Str("active_nodes", strings.Join(l.ActiveNodes, ",")).
			Msg("Failed to run graph super-step")
	}
	// engine.py:82
	if serr := l.saveStateOnError(ctx, err); serr != nil {
		return false, serr
	}
	// engine.py:83-92 分类上抛
	var re *RecursionError
	var be *exception.BaseError
	if errors.As(err, &gi) || errors.As(err, &re) || errors.As(err, &be) {
		return false, err
	}
	return false, exception.BuildError(exception.StatusPregelGraphSuperStepExecutionError,
		exception.WithParam("step", l.Step),
		exception.WithParam("reason", err.Error()),
		exception.WithCause(err))
}
```

- [ ] **Step 3: 实现 runStep（engine.py:98-171 步骤逐条）**

```go
// runStep 内层超步步骤（engine.py:98-171，编号对齐）
func (l *PregelLoop) runStep(ctx context.Context) error {
	// engine.py:99-110 debug 日志
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_SUPER_STEP_START").
		Str("graph_id", l.Config.Ns).
		Str("session_id", l.Config.SessionID).
		Str("ns", l.Config.Ns).
		Int("step", l.Step).
		Str("active_nodes", strings.Join(l.ActiveNodes, ",")).
		Msg("Start to run graph super-step")
	// engine.py:111-127 Determine tasks for this round
	// Retry tasks from graph state
	if len(l.retryPendingNodes) > 0 {
		names := make([]string, 0, len(l.retryPendingNodes))
		for name := range l.retryPendingNodes {
			names = append(names, name)
		}
		sort.Strings(names) // Python dict.keys() 顺序无契约；对齐 GetReadyNodes D-4 稳定序
		l.ActiveNodes = names
		// Clear retry queue (scheduled this round)
		l.retryPendingNodes = nil
	} else {
		// First in
		readyNodes := l.Manager.GetReadyNodes()
		// Active nodes
		l.ActiveNodes = nil
		for _, n := range readyNodes {
			if _, ok := l.Graph.Nodes[n]; ok && n != End {
				l.ActiveNodes = append(l.ActiveNodes, n)
				l.NodeVersion[n]++
			}
		}
	}
	// engine.py:129-136
	if len(l.ActiveNodes) == 0 {
		if l.Manager.IsEmpty() {
			return errGraphEnd // 内部哨兵：见下方说明
		}
		// Only flush buffer
		if err := l.Manager.Flush(); err != nil {
			return err
		}
		l.Step++
		return errGraphEnd
	}
	// engine.py:138-141
	if l.Step > l.MaxStep {
		return &RecursionError{Limit: l.MaxStep, Step: l.Step}
	}
	// engine.py:143-145
	tasksToRun := make([]*PregelNode, 0, len(l.ActiveNodes))
	for _, name := range l.ActiveNodes {
		l.Manager.Consume(name)
		tasksToRun = append(tasksToRun, l.Graph.Nodes[name])
	}
	// engine.py:147-149 Execute tasks
	for _, node := range tasksToRun {
		l.Executor.Submit(ctx, node, l.NodeVersion[node.Name()])
	}
	// engine.py:151-155
	if err := l.Executor.WaitAll(); err != nil {
		if errors.Is(err, context.Canceled) { // 偏差 B3：asyncio.CancelledError
			l.Executor.CancelAll()
			return err
		}
		return err
	}
	// engine.py:157-161 Summarize results
	for _, msg := range l.Executor.succeedMessages {
		l.Manager.BufferMessage(msg)
	}
	if err := l.Manager.Flush(); err != nil {
		return err
	}
	l.Executor.Clear()
	// engine.py:163-169 Hook
	if l.Graph.AfterStep != nil {
		l.Graph.AfterStep(ctx, l)
	}
	l.Step++
	return nil
}
```

**结束信号说明（计划内定型）：** Python 用 `return False/True` 区分；Go 内层 runStep 返回 error，需携带"结束"信息。定义包内哨兵 `var errGraphEnd = errors.New("graph end")`，RunStep 外层最先判 `errors.Is(err, errGraphEnd) → return false, nil`（放在日志判断之前，哨兵不打错误日志、不落盘）。这是 Python bool 返回值的 error 通道等价物，属 B6 同源表达。

- [ ] **Step 4: 实现 saveStateOnError（engine.py:173-205）**

```go
// saveStateOnError 错误状态落盘（engine.py:173-205）
func (l *PregelLoop) saveStateOnError(ctx context.Context, cause error) error {
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_SUPER_STEP_ERROR").
		Err(cause).
		Str("graph_id", l.Config.Ns).
		Str("session_id", l.Config.SessionID).
		Str("ns", l.Config.Ns).
		Int("step", l.Step).
		Str("active_nodes", strings.Join(l.ActiveNodes, ",")).
		Msg("Failed to run graph super-step, caused by save state")
	// engine.py:186-187
	if l.Config.SessionID == "" || l.Config.Ns == "" || l.Saver == nil {
		return nil
	}
	// engine.py:188-192（manager.buffer 经 Buffer() 副本，偏差 D-9；executor 判空对齐 `if self.executor:`）
	pendingBuffer := l.Manager.Buffer()
	pendingNode := map[string]PendingNode{}
	if l.Executor != nil {
		pendingBuffer = append(pendingBuffer, l.Executor.succeedMessages...)
		pendingNode = l.Executor.failed
	}
	// engine.py:193-200
	state := CreateState(l.Config.Ns, l.Step, l.Manager.Snapshot(),
		pendingBuffer, pendingNode, l.NodeVersion)
	// engine.py:201-205
	if err := l.Saver.Save(ctx, l.Config.SessionID, l.Config.Ns, state); err != nil {
		logger.Error(logComponent).Err(err).
			Str("graph_id", l.Config.Ns).Msg("save state on error failed")
		return err
	}
	return nil
}
```

- [ ] **Step 5: 测试通过 + Commit**

### Task 7: Pregel + Run

**Files:** 同 Task 5

- [ ] **Step 1: 失败测试**（手工构造图：START→a→END）

```go
// handGraph 手工构造 START→a→END 图（Build 回填前的构造方式）
func handGraph() *Pregel {
	nodes := map[string]*PregelNode{}
	nodes[Start] = NewPregelNode(Start, NoopNodeFunc)
	a := NewPregelNode("a", NoopNodeFunc)
	a.addRouter(NewStaticRouter(End))
	nodes["a"] = a
	nodes[End] = NewPregelNode(End, NoopNodeFunc)
	channels := []Channel{NewTriggerChannel(Start), NewTriggerChannel("a"), NewTriggerChannel(End)}
	return NewPregel(nodes, channels, Start, nil, nil)
}

// TestPregel_Run_happyPath 正常跑完返回空 map（engine.py:254-268）
func TestPregel_Run_happyPath(t *testing.T) {
	pg := handGraph()
	res, err := pg.Run(context.Background(), nil)
	assert.NoError(t, err)
	assert.Empty(t, res)
}

// TestPregel_Run_顶层中断 转换 __interrupt__（engine.py:269-284）
func TestPregel_Run_顶层中断(t *testing.T) {
	pg := interruptingGraph()
	res, err := pg.Run(context.Background(), nil)
	assert.NoError(t, err)
	v, ok := res[TaskStatusInterrupt]
	assert.True(t, ok)
	gi := v.(*GraphInterrupt)
	assert.Equal(t, "x", gi.Interrupts[0].Value)
}

// TestPregel_Run_子图中断非顶层 不转换直接上抛（engine.py:284-285）
func TestPregel_Run_子图中断非顶层(t *testing.T) {
	pg := interruptingGraph()
	_, err := pg.Run(context.Background(), &PregelConfig{ParentNs? ...})
```

注意：子图判定用 `InnerPregelConfig.ParentNs`，而 Run 入参是 `*PregelConfig`（无 ParentNs）——**修正**：Run 入参对齐 Python `config: Optional[PregelConfig]`，is_top_level 判定在 CreateInnerConfig 之后看 `inner.ParentNs`。`*PregelConfig` 永远无 ParentNs ⇒ 顶层恒真。Python 里子图调用方会直接构造含 parent_ns 的 TypedDict 传入——Go 侧等价物是传 `*InnerPregelConfig`？**决定：Run 入参 `*InnerPregelConfig`**（承载 parent_ns 的唯一 Go 形状），`CreateInnerConfig` 的 falsy 补默认在 Run 内仍执行（engine.py:232 对含 parent_ns 的 TypedDict 同样调 create_inner_config）。偏差登记于 Task 9 汇总表：Run 形参类型 `*InnerPregelConfig`（Python 注解 PregelConfig 是 TypedDict 可动态携带 parent_ns，8.3b D1 同源已确立该形状）。

```go
	inner := CreateInnerConfig(PregelConfig{})
	inner.ParentNs = "parent:ns"
	_, err := pg.Run(context.Background(), &inner)
	var gi *GraphInterrupt
	assert.True(t, errors.As(err, &gi))
```

- [ ] **Step 2: 实现 Pregel + Run（engine.py:208-285）**

```go
// Pregel 可执行图对象（Python engine.py:208-228）
type Pregel struct {
	// Nodes 节点表（engine.py:223 self.nodes）
	Nodes map[string]*PregelNode
	// Store 检查点存储（engine.py:224 self.store）
	Store Store
	// Channels 通道列表（engine.py:226 self.channels）
	Channels []Channel
	// Initial 起始节点名（engine.py:227 self.initial）
	Initial string
	// AfterStep 每步回调（engine.py:228 self.after_step；B1 单形态）
	AfterStep AfterStepFunc
}

// AfterStepFunc 每超步回调（engine.py:216-221 双形态 → B1 偏差同步单形态）
type AfterStepFunc func(ctx context.Context, loop *PregelLoop)

// NewPregel 构造（engine.py:210-228）
func NewPregel(nodes map[string]*PregelNode, channels []Channel, initial string,
	store Store, afterStep AfterStepFunc) *Pregel {
	return &Pregel{Nodes: nodes, Store: store, Channels: channels, Initial: initial, AfterStep: afterStep}
}

// Run 执行图（engine.py:230-285）。返回值：正常 ({}, nil)；顶层中断 ({__interrupt__: gi}, nil)；
// 其余 (nil, err)（B6：Go 以 error 承载 Python raise）。
func (g *Pregel) Run(ctx context.Context, config *InnerPregelConfig) (map[string]any, error) {
	// engine.py:232-237
	cfg := DefaultPregelConfig()
	if config != nil {
		cfg = config.PregelConfig
	}
	inner := CreateInnerConfig(cfg)
	if config != nil && config.ParentNs != "" {
		inner.ParentNs = config.ParentNs
	}
	isTopLevel := inner.ParentNs == ""
	if isTopLevel && inner.Ns != "" {
		inner.ParentNs = inner.Ns
	}
	// engine.py:239-246 GRAPH_START log
	logger.Info(logComponent).
		Str("event_type", "GRAPH_START").
		Str("graph_id", inner.Ns).
		Str("session_id", inner.SessionID).
		Bool("is_top_level", isTopLevel).
		Msg("Pregel graph engine execution started")
	loop := NewPregelLoop(g, &inner)
	// engine.py:249-268 try 块
	if err := loop.Init(ctx); err != nil {
		return nil, err // init 异常不经 except GraphInterrupt 转换（Python 同：raw 传播）
	}
	callback.Trigger(ctx, string(callback.LoopStarted), map[string]any{"graph_id": inner.Ns})
	runErr := func() error {
		for {
			more, err := loop.RunStep(ctx)
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		}
	}()
	if runErr == nil {
		// engine.py:256-268
		callback.Trigger(ctx, string(callback.LoopFinished),
			map[string]any{"graph_id": inner.Ns, "total_steps": loop.Step})
		logger.Info(logComponent).
			Str("event_type", "GRAPH_END").
			Str("graph_id", inner.Ns).
			Str("session_id", inner.SessionID).
			Int("total_steps", loop.Step).
			Msg("Pregel graph engine execution completed")
		return map[string]any{}, nil
	}
	// engine.py:269-285 except GraphInterrupt
	var gi *GraphInterrupt
	if errors.As(runErr, &gi) {
		callback.Trigger(ctx, string(callback.LoopFinished),
			map[string]any{"graph_id": inner.Ns, "total_steps": loop.Step})
		logger.Info(logComponent).
			Str("event_type", "GRAPH_END").
			Str("graph_id", inner.Ns).
			Str("session_id", inner.SessionID).
			Int("total_steps", loop.Step).
			Msg("Pregel graph engine execution interrupted")
		if isTopLevel {
			return map[string]any{TaskStatusInterrupt: gi}, nil
		}
		return nil, runErr
	}
	return nil, runErr
}
```

- [ ] **Step 3: 测试通过 + Commit**

### Task 8: builder.Build() 回填

**Files:**
- Modify: `internal/agentcore/graph/pregel/builder.go`（删除 ⤴️ 注记，追加 Build）
- Test: `internal/agentcore/graph/pregel/builder_test.go`

- [ ] **Step 1: 失败测试**

```go
// TestPregelBuilder_Build Build 交付调度引擎（Python builder.py:71-77；8.3a 方案 A 回填）
func TestPregelBuilder_Build(t *testing.T) {
	b := NewPregelBuilder()
	assert.NoError(t, b.AddEdge(Start, "a"))
	assert.NoError(t, b.AddEdge("a", End))
	sv := &fakeSaver{}
	pg := b.Build(sv, func(ctx context.Context, loop *PregelLoop) {
		loop.Step++ // 回调可读写 loop（D6 字段导出的消费面）
	})
	assert.Same(t, sv, pg.Store)
	assert.Equal(t, Start, pg.Initial)
	assert.Len(t, pg.Nodes, 4) // START/END/a + builder 预注册
	res, err := pg.Run(context.Background(), nil)
	assert.NoError(t, err)
	assert.Empty(t, res)
}
```

- [ ] **Step 2: 实现**

```go
// Build 将节点表与通道交付给调度引擎（Python builder.py:71-77）。
// store 可为 nil（PregelLoop 对 saver 三重判空，engine.py:43/186）；
// afterStep 同步单形态（偏差 B1）。8.3a 方案 A 承诺的本节回填兑现点。
func (b *PregelBuilder) Build(store Store, afterStep AfterStepFunc) *Pregel {
	return NewPregel(b.nodes, b.channels, Start, store, afterStep)
}
```

同时把 builder.go 头注释中「Build()（⤴️ 8.3c 回填）」改为「Build()（8.3c 已回填）」。

- [ ] **Step 3: 测试通过（含 Task 5/7 的手工构造改用 Build 的 helper 复核）+ Commit**

### Task 9: doc.go + 计划表 + 全量回归 + 推送

**Files:**
- Modify: `internal/agentcore/graph/pregel/doc.go`、`internal/agentcore/graph/store/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: doc.go** 两个包文件树各补/改条目（pregel 增 engine.go/state.go；store 标注类型迁移说明）
- [ ] **Step 2: IMPLEMENTATION_PLAN.md** 8.3c 行打 ✅ 并按先例格式写 Go 事实；8.3a 行 ⤴️ 注记后追加「✅ 已回填（8.3c，commit XXX）」；L553 实施序注记追加「8.3c 已落地」行
- [ ] **Step 3: 偏差汇总**（engine.go 头注释或 spec 均可，spec §6 增 B7）
  - B7：Run 形参 `*InnerPregelConfig`（Python TypedDict 动态键 parent_ns 的 Go 承载，8.3b D1 同源）
  - B8：runStep 结束信号 `errGraphEnd` 哨兵（Python bool 返回的 error 通道等价物）
- [ ] **Step 4: 全量回归** `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（零 FAIL）+ `go test -race ./internal/agentcore/graph/...`
- [ ] **Step 5: 回填验证** grep `⤴️ 8.3c` 仅计划表历史注记；`go build ./... && go vet ./...`
- [ ] **Step 6: Commit + push**

---

## Self-Review 结论

- **Spec 覆盖**：D1→Task4、D2/D3→Task2/6、D4→Task8、D5/D6→Task7/5、B5→Task3、112104→Task1、serde 核对已记录于 spec §3（无需任务）✅
- **占位符**：无 TBD；`testGraph` 命名改为 `handGraph` 统一 ✅
- **类型一致性**：`AfterStepFunc`/`Store`/`GraphState`/`errGraphEnd` 各任务引用一致（Task5 helper 在 Task8 后可改用 Build，行为不变）✅
