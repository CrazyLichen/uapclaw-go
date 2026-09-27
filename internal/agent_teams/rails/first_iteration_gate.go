package rails

import (
	"context"
	"sync"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FirstIterationGate 首次迭代信号门。
// Python: FirstIterationGate(AgentRail) (rails/first_iteration_gate.py)
//
// Go 用 channel 替代 Python asyncio.Event。
// 外部代码调用 Wait() 阻塞直到 Agent 进入首次迭代。
// 嵌入 BaseRail 实现 AgentRail 接口。
type FirstIterationGate struct {
	agentinterfaces.BaseRail
	// ch 信号通道，close(ch) 表示门已打开
	ch chan struct{}
	// once 确保 ch 只关闭一次
	once sync.Once
	// mu 保护 Reset 操作
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFirstIterationGate 创建首次迭代门控。
// Python: FirstIterationGate()
func NewFirstIterationGate() *FirstIterationGate {
	return &FirstIterationGate{
		ch: make(chan struct{}),
	}
}

// Wait 阻塞直到首次迭代开始。
// Python: async wait()
//
// 支持 context 取消。
func (g *FirstIterationGate) Wait(ctx context.Context) error {
	select {
	case <-g.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsReady 返回门是否已打开。
// Python: is_ready -> bool
func (g *FirstIterationGate) IsReady() bool {
	select {
	case <-g.ch:
		return true
	default:
		return false
	}
}

// BeforeTaskIteration 首次迭代时打开门。
// Python: async before_task_iteration(ctx)
func (g *FirstIterationGate) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	g.once.Do(func() { close(g.ch) })
	return nil
}

// Reset 重置门用于新一轮。
// Python: reset()
func (g *FirstIterationGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.once = sync.Once{}
	g.ch = make(chan struct{})
}

// ──────────────────────────── 非导出函数 ────────────────────────────
