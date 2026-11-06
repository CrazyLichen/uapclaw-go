package harness

import (
	"sync"
	"sync/atomic"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ActiveRound 一次外圈执行（一轮）。
//
// Python: ActiveRound (agent_teams/harness/state.py)
// 本阶段只含"驱动一轮"所需字段；快照（pre_round_snapshot/last_safe_snapshot/
// last_iter_snapshot）、相位跟踪（iter_phase/model_call_in_flight/tool_started）、
// pause（pause_requested/pause_ack）、turn 由 25.1c / 25.1e 追加。
type ActiveRound struct {
	// RoundID 轮次序号（自 1 递增）
	RoundID int
	// TaskID 提交给任务循环内核的任务标识（立即中断取消它）
	TaskID string
	// Query 本轮原始查询
	Query any
	// Cancel 取消本轮的上下文取消函数
	Cancel func()
	// Done 本轮结束信号
	Done chan struct{}
}

// InboxMessage 排队等待驱动的输入。
//
// Python: InboxMessage (state.py)
type InboxMessage struct {
	// Seq 单调递增序号，定义全局 FIFO
	Seq int
	// Content 输入内容
	Content any
	// Immediate true 表示转向当前轮，false 表示等本轮结束后再跑
	Immediate bool
}

// HarnessInternalState 全部可变状态容器；supervisor 是唯一写者。
//
// Python: HarnessInternalState (state.py)。
// 相位用原子量保存，因为 State() 会被外部协程并发读取
// （Python 靠单线程 asyncio 串行，Go 必须显式同步）。
type HarnessInternalState struct {
	// phase 当前生命周期相位
	phase atomic.Value
	// mu 保护 active、queue、seq 与 roundSeq（仅 supervisor 写入，
	// 但 State 之外的读取路径也在锁内）
	mu sync.RWMutex
	// active 当前活动轮次，无则为 nil
	active *ActiveRound
	// queue 待驱动输入队列
	queue []InboxMessage
	// seq 输入序号自增源
	seq int
	// roundSeq 轮次序号自增源
	roundSeq int
}

// ──────────────────────────── 枚举 ────────────────────────────

// HarnessState 生命周期相位。
//
// Python: HarnessState (harness_protocol/state.py)，5 态。
// 注意：DeepAgent 内部的 InteractionPhase 只有 idle/running/terminated 三态，
// 暂停是 L2 独有的概念。
type HarnessState string

const (
	// HarnessStateIdle 空闲（无在飞轮次）
	HarnessStateIdle HarnessState = "idle"
	// HarnessStateRunning 有轮次在执行
	HarnessStateRunning HarnessState = "running"
	// HarnessStatePausing 已接受暂停、尚未抵达可恢复边界（25.1c 启用）
	HarnessStatePausing HarnessState = "pausing"
	// HarnessStatePaused 已暂停在可恢复边界（25.1c 启用）
	HarnessStatePaused HarnessState = "paused"
	// HarnessStateTerminated 已终止（不可逆）
	HarnessStateTerminated HarnessState = "terminated"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newInternalState 创建内部状态，相位初始化为 idle。
func newInternalState() *HarnessInternalState {
	st := &HarnessInternalState{queue: make([]InboxMessage, 0, 4)}
	st.phase.Store(HarnessStateIdle)
	return st
}

// Phase 返回当前相位。
func (s *HarnessInternalState) Phase() HarnessState {
	if v, ok := s.phase.Load().(HarnessState); ok {
		return v
	}
	return HarnessStateIdle
}

// setPhase 更新相位（仅 supervisor 调用）。
func (s *HarnessInternalState) setPhase(p HarnessState) {
	s.phase.Store(p)
}

// Active 返回当前活动轮次。
func (s *HarnessInternalState) Active() *ActiveRound {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// SetActive 设置当前活动轮次。
func (s *HarnessInternalState) SetActive(r *ActiveRound) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = r
}

// NextRoundID 分配下一个轮次序号。
//
// Python: HarnessInternalState.next_round_id
func (s *HarnessInternalState) NextRoundID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roundSeq++
	return s.roundSeq
}
