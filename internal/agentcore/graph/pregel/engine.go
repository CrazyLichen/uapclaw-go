package pregel

import (
	"context"

	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 对齐 Python openjiuwen/core/graph/pregel/engine.py（285 行）：
// PregelLoop 单次图执行的超步循环 + Pregel 可执行图对象 + Run 顶层入口。

// ──────────────────────────── 结构体 ────────────────────────────

// PregelLoop 单次图执行的超步循环（Python engine.py:25-205）。
// 字段对齐 Python 公开属性（spec 8.3c D6）：afterStep 回调方跨包可读。
type PregelLoop struct {
	// Graph 执行中的图（engine.py:27 self.graph）
	Graph *Pregel
	// Manager 通道管理器（engine.py:28 self.manager）
	Manager *ChannelManager
	// Step 当前步号（engine.py:29 self.step）
	Step int
	// MaxStep 本轮步数上限（engine.py:30 self.max_step）
	MaxStep int
	// Config 内部配置（engine.py:31 self.config；实收 inner_config，engine.py:248）
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

// Pregel 可执行图对象（Python engine.py:208-228）。字段同 D6 导出纪律。
type Pregel struct {
	// Nodes 节点表（engine.py:223 self.nodes）
	Nodes map[string]*PregelNode
	// Store 检查点存储（engine.py:224 self.store）
	Store Store
	// Channels 通道列表（engine.py:226 self.channels）
	Channels []Channel
	// Initial 起始节点名（engine.py:227 self.initial）
	Initial string
	// AfterStep 每步回调（engine.py:228 self.after_step；B1 同步单形态）
	AfterStep AfterStepFunc
}

// RecursionError 递归上限超限（Python 内置 RecursionError 的 Go 等价物，spec D3）。
// engine.py:85 按类型名分类上抛（"RecursionError is caught by name at the workflow
// layer"），Go 以 errors.As 类型断言还原该契约。
type RecursionError struct {
	// Limit 递归上限（max_step）
	Limit int
	// Step 触发时的步号
	Step int
}

// AfterStepFunc 每超步回调（engine.py:216-221 同步/协程双形态 → B1 偏差同步单形态）
type AfterStepFunc func(ctx context.Context, loop *PregelLoop)

// ──────────────────────────── 全局变量 ────────────────────────────

// errGraphEnd 图结束信号（engine.py:129-131 return False 的 error 通道等价物，B8）。
// RunStep 外层最先识别并转为 (false, nil)，不打错误日志、不落盘。
var errGraphEnd = errors.New("pregel: graph end")

// ──────────────────────────── 导出函数 ────────────────────────────

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

// NewPregel 构造（engine.py:210-228）
func NewPregel(nodes map[string]*PregelNode, channels []Channel, initial string,
	store Store, afterStep AfterStepFunc) *Pregel {
	return &Pregel{Nodes: nodes, Store: store, Channels: channels, Initial: initial, AfterStep: afterStep}
}

// Error 文案复刻 engine.py:139-141
func (e *RecursionError) Error() string {
	return fmt.Sprintf("Recursion limit of %d reached at step %d", e.Limit, e.Step)
}

// Init 初始化：构造任务池、恢复检查点或触发 START（engine.py:39-62）
func (l *PregelLoop) Init(ctx context.Context) error {
	// engine.py:40-41
	l.Executor = NewTaskExecutorPool(l.Config)
	l.MaxStep = l.Config.RecursionLimit
	// engine.py:42-44：sessionID/ns/saver 三者齐才读存储
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

// RunStep 执行一个超步，false 表示图结束（engine.py:64-92）。
// 两层结构一比一：外层错误处理边界，内层 runStep 步骤（spec D2：同步保存后分类上抛）。
func (l *PregelLoop) RunStep(ctx context.Context) (bool, error) {
	err := l.runStep(ctx)
	if err == nil {
		return true, nil
	}
	// B8：图结束信号 → (false, nil)
	if errors.Is(err, errGraphEnd) {
		return false, nil
	}
	// B3：外层取消（asyncio.CancelledError）绕过整个错误处理块原样上抛——
	// Python 的 CancelledError 继承 BaseException 而非 Exception，
	// engine.py:67 的 `except Exception` 不捕获它，故不打日志、不落盘、不包装
	if errors.Is(err, context.Canceled) {
		return false, err
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
	// engine.py:82（D2：同步 await，保存完成前不向调用方暴露异常）
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

// Run 执行图（engine.py:230-285）。
// 返回值（B6）：正常 ({}, nil)；顶层中断 ({__interrupt__: gi}, nil)；其余 (nil, err)。
// config 形参类型 *InnerPregelConfig（偏差 B7）：Python TypedDict 可动态携带
// parent_ns，8.3b D1 已确立该 Go 承载形状。
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
	// engine.py:249-250：init 异常不经 except GraphInterrupt 转换（Python 同：raw 传播）
	if err := loop.Init(ctx); err != nil {
		return nil, err
	}
	// engine.py:251-253
	callback.Trigger(ctx, string(callback.LoopStarted), map[string]any{"graph_id": inner.Ns})
	// engine.py:254-255 while await loop.run_step()
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// isResume 恢复判定（engine.py:94-96 staticmethod；Go 作包级函数）
func isResume(state *GraphState) bool {
	return state != nil && state.IsResumable()
}

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
	// engine.py:201-205：保存失败上抛并取代原异常（Python except 块内新异常同语义）
	if err := l.Saver.Save(ctx, l.Config.SessionID, l.Config.Ns, state); err != nil {
		logger.Error(logComponent).Err(err).
			Str("graph_id", l.Config.Ns).Msg("save state on error failed")
		return err
	}
	return nil
}

// runStep 内层超步步骤（engine.py:98-171，步骤编号逐条对应）
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
		sort.Strings(names) // Python dict.keys() 序无契约；对齐 GetReadyNodes D-4 稳定序
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
			return errGraphEnd // End
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
		l.Executor.Submit(ctx, node, l.NodeVersion[node.name])
	}
	// engine.py:151-152
	if err := l.Executor.WaitAll(); err != nil {
		return err
	}
	// engine.py:153-155 except asyncio.CancelledError: cancel_all + raise
	// 偏差 B3（2026-11-15 审查收窄）：外层取消以 ctx.Err() 判定，而非 WaitAll 返回
	// context.Canceled——后者可能是节点自身返回的取消错误，属 FIRST_EXCEPTION 常规
	// 失败路径，Python 不会走 cancel_all 分支。ctx 取消经任务子 ctx 传播，WaitAll
	// 收割后在此统一 cancel_all 并上抛（CancelledError 语义，见 RunStep 透传分支）。
	if ctx.Err() != nil {
		l.Executor.CancelAll()
		return ctx.Err()
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
