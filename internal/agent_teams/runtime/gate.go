package runtime

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AdmissionTicket admit 成功后的不透明票据。
// 对齐 Python: AdmissionTicket (openjiuwen/agent_teams/runtime/gate.py)
//
// 调用方必须在 Agent 实际消费载荷后将票据传回 ConsumeDone，
// 以便门控正确排空。票据仅在 InteractGate.Admit 内部构造；
// 外部调用方应将 gate 字段视为不透明。
type AdmissionTicket struct {
	// gate 所属门控
	gate *InteractGate
}

// InteractGate Run/Interact 并发门控。
// 对齐 Python: InteractGate (openjiuwen/agent_teams/runtime/gate.py)
//
// 当 run_agent_team_streaming 调用进行中时，interact_team 可通过
// 门控准入新载荷。Run 即将退出时关闭门控（拒绝后续 interact），
// 并等待飞行中载荷消费完毕后 stream 才真正结束。
//
// 状态转换：
//
//	OPEN    --Admit()-------->      OPEN, inflight++
//	OPEN    --CloseAndDrain()--> CLOSING --(inflight==0)--> DRAINED
//	CLOSING --Admit()-------->      nil (rejected)
//	*       --ConsumeDone()-->      inflight--; signal drained when zero
//
// Python 使用 asyncio.Lock 保护状态（单线程协程）。
// Go 使用 sync.Mutex（多 goroutine 并发）。
type InteractGate struct {
	// closed 门控是否已关闭
	closed bool
	// inflight 飞行中载荷计数
	inflight int
	// drained inflight==0 信号通道（关闭表示已排空）
	drained chan struct{}
	// mu 互斥锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// gateLogComponent 日志组件
var gateLogComponent = logger.ComponentChannel

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInteractGate 创建新门控。
// 对齐 Python: InteractGate.__init__()
//
// Python 初始状态：_closed=Event(), _inflight=0, _drained=Event(set), _lock=Lock()
// Go 初始状态：closed=false, inflight=0, drained=已关闭通道
func NewInteractGate() *InteractGate {
	g := &InteractGate{
		drained: make(chan struct{}),
	}
	// 初始状态：inflight=0 → drained 信号已发出
	close(g.drained)
	return g
}

// Closed 门控是否已关闭。
// 对齐 Python: InteractGate.closed (property)
func (g *InteractGate) Closed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

// Inflight 当前飞行中的载荷数。
// 对齐 Python: InteractGate.inflight (property)
func (g *InteractGate) Inflight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inflight
}

// Admit 尝试准入一个新载荷。
// 对齐 Python: InteractGate.admit()
//
// Python 执行步骤：
//  1. async with self._lock:
//  2. if self._closed.is_set(): return None
//  3. self._inflight += 1
//  4. self._drained.clear()
//  5. return AdmissionTicket(gate=self)
//
// 门控已关闭时返回 nil；否则 inflight++ 并返回绑定到本门控的票据。
func (g *InteractGate) Admit() *AdmissionTicket {
	g.mu.Lock()
	defer g.mu.Unlock()
	// 对齐 Python 步骤 2
	if g.closed {
		return nil
	}
	// 对齐 Python 步骤 3-4
	g.inflight++
	g.drained = make(chan struct{})
	// 对齐 Python 步骤 5
	return &AdmissionTicket{gate: g}
}

// ConsumeDone 标记载荷已消费。
// 对齐 Python: InteractGate.consume_done(ticket)
//
// Python 执行步骤：
//  1. if ticket.gate is not self: return
//  2. async with self._lock:
//  3. if self._inflight <= 0: return
//  4. self._inflight -= 1
//  5. if self._inflight == 0: self._drained.set()
//
// 来自不同门控的票据被静默忽略。
// inflight 递减到 0 时关闭 drained 通道发出排空信号。
func (g *InteractGate) ConsumeDone(ticket *AdmissionTicket) {
	// 对齐 Python 步骤 1
	if ticket == nil || ticket.gate != g {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// 对齐 Python 步骤 3
	if g.inflight <= 0 {
		return
	}
	// 对齐 Python 步骤 4
	g.inflight--
	// 对齐 Python 步骤 5: if self._inflight == 0: self._drained.set()
	if g.inflight == 0 {
		close(g.drained)
	}
}

// CloseAndDrain 关闭门控并等待飞行中载荷排空。
// 对齐 Python: InteractGate.close_and_drain()
//
// Python 执行步骤：
//  1. async with self._lock:
//  2. self._closed.set()
//  3. if self._inflight == 0:
//  4. self._drained.set(); return
//  5. await self._drained.wait()
//
// Go 增加 ctx 参数支持超时/取消（Python 的 asyncio.wait 无超时保护）。
func (g *InteractGate) CloseAndDrain(ctx context.Context) error {
	g.mu.Lock()
	// 对齐 Python 步骤 2
	g.closed = true
	// 对齐 Python 步骤 3-4
	if g.inflight == 0 {
		select {
		case <-g.drained:
			// 已关闭
		default:
			close(g.drained)
		}
		g.mu.Unlock()
		return nil
	}
	drainedCh := g.drained
	g.mu.Unlock()

	// 对齐 Python 步骤 5: await self._drained.wait()
	select {
	case <-drainedCh:
		return nil
	case <-ctx.Done():
		logger.Warn(gateLogComponent).Err(ctx.Err()).
			Msg("CloseAndDrain 在等待飞行中载荷排空时被取消")
		return ctx.Err()
	}
}

// Reset 重置门控供新一轮 Run 使用。
// 对齐 Python: InteractGate.reset()
//
// Python 执行步骤：
//  1. async with self._lock:
//  2. self._closed.clear()
//  3. self._inflight = 0
//  4. self._drained.set()
func (g *InteractGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	// 对齐 Python 步骤 2
	g.closed = false
	// 对齐 Python 步骤 3
	g.inflight = 0
	// 对齐 Python 步骤 4
	select {
	case <-g.drained:
		// 已关闭，重新创建
		g.drained = make(chan struct{})
		close(g.drained)
	default:
		// 未关闭，直接关闭
		close(g.drained)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
