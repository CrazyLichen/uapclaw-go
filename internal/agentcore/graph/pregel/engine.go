package pregel

import (
	"context"
	"fmt"
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

// AfterStepFunc 每超步回调（engine.py:216-221 同步/协程双形态 → B1 偏差同步单形态）
type AfterStepFunc func(ctx context.Context, loop *PregelLoop)

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

// ──────────────────────────── 非导出函数 ────────────────────────────

// isResume 恢复判定（engine.py:94-96 staticmethod；Go 作包级函数）
func isResume(state *GraphState) bool {
	return state != nil && state.IsResumable()
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

// ──────────────────────────── 导出函数 ────────────────────────────

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
