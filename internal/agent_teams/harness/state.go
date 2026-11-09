package harness

import (
	"sync"
	"sync/atomic"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SafeStateSnapshot 迭代/轮边界处捕获的可回滚快照。
// Python: SafeStateSnapshot (state.py L42-59)
type SafeStateSnapshot struct {
	// ContextMessages 当前轮上下文消息（with_history=false 段）的独立副本
	ContextMessages []llmschema.BaseMessage
	// DeepAgentState DeepAgentState.ToSessionDict() 的结果
	DeepAgentState map[string]any
	// IterationIndex 轮前基线为 0；其余为刚完成那一轮的 1-based 序号
	IterationIndex int
}

// ActiveRound 一次外圈执行（一轮）。
// Python: ActiveRound (agent_teams/harness/state.py L82-165)
type ActiveRound struct {
	// RoundID 轮次序号（自 1 递增）
	RoundID int
	// TaskID 提交给任务循环内核的任务标识
	TaskID string
	// OriginalQuery 本轮原始查询（string / []string / *interaction.InteractiveInput）
	OriginalQuery any
	// RunContext 本轮请求上下文（可为 nil）
	RunContext *agentinterfaces.RunContext
	// Turn 本轮所属的成员 turn
	Turn MemberTurn
	// TurnOpened 本轮是否新开 turn（决定是否 commit）
	TurnOpened bool
	// Cancel 取消本轮的上下文取消函数
	Cancel func()
	// Done 本轮结束信号
	Done chan struct{}
	// PreRoundSnapshot 轮前基线（index=0）
	PreRoundSnapshot *SafeStateSnapshot
	// LastSafeSnapshot 最近一次轮边界快照
	LastSafeSnapshot *SafeStateSnapshot
	// LastIterSnapshot 最近一次迭代边界快照（主回滚目标）
	LastIterSnapshot *SafeStateSnapshot
	// IterPhase 当前子相位（atomic.Value 存 RoundPhase）
	IterPhase atomic.Value
	// ModelCallInFlight 模型调用在飞（唯一可安全硬取消的窗口）
	ModelCallInFlight atomic.Bool
	// ToolStarted 当前迭代已开始执行工具
	ToolStarted atomic.Bool
	// PauseRequested 协作停止已武装（正确性权威）
	PauseRequested atomic.Bool
	// GracefulAbort 优雅中止：跑完本轮后回 IDLE
	GracefulAbort atomic.Bool
	// FailureRetry 本轮是否为崩溃后的一次性重试
	FailureRetry bool
	// PauseAck 工具相位暂停的延迟应答
	PauseAck chan error
	// mode 本轮启动方式（同包可见）
	mode startMode
}

// Phase 返回当前子相位。
func (r *ActiveRound) Phase() RoundPhase {
	if v, ok := r.IterPhase.Load().(RoundPhase); ok {
		return v
	}
	return RoundPhaseBoundary
}

// SetPhase 设置当前子相位。
func (r *ActiveRound) SetPhase(p RoundPhase) { r.IterPhase.Store(p) }

// InboxMessage 排队等待驱动的输入。
// Python: InboxMessage (state.py)
//
// 与上游的差异：上游带单调递增 seq 并把它作为 send() 的返回值；Go 的
// Send 只返回 error（HarnessProtocol 契约），seq 没有消费者，故不保留。
type InboxMessage struct {
	// Content 输入内容
	Content any
	// Immediate true 表示转向当前轮，false 表示等本轮结束后再跑
	Immediate bool
}

// HarnessInternalState 全部可变状态容器；supervisor 是唯一写者。
// Python: HarnessInternalState (state.py)。
// 相位用原子量保存，因为 State() 会被外部协程并发读取
// （Python 靠单线程 asyncio 串行，Go 必须显式同步）。
type HarnessInternalState struct {
	// phase 当前生命周期相位
	phase atomic.Value
	// mu 保护 active、queue、roundSeq 与暂停轮查询
	mu sync.RWMutex
	// active 当前活动轮次，无则为 nil
	active *ActiveRound
	// queue 待驱动输入队列
	queue []InboxMessage
	// roundSeq 轮次序号自增源
	roundSeq int
	// pausedQuery 暂停轮的原始查询
	pausedQuery string
	// hasPausedQuery 区分“空查询”与“没有暂停轮”
	hasPausedQuery bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// HarnessState 生命周期相位。
// Python: HarnessState (harness_protocol/state.py)，5 态。
// 注意：DeepAgent 内部的 InteractionPhase 只有 idle/running/terminated 三态，
// 暂停是 L2 独有的概念。
type HarnessState string

const (
	// HarnessStateIdle 空闲（无在飞轮次）
	HarnessStateIdle HarnessState = "idle"
	// HarnessStateRunning 有轮次在执行
	HarnessStateRunning HarnessState = "running"
	// HarnessStatePausing 已接受暂停、尚未抵达可恢复边界
	HarnessStatePausing HarnessState = "pausing"
	// HarnessStatePaused 已暂停在可恢复边界
	HarnessStatePaused HarnessState = "paused"
	// HarnessStateTerminated 已终止（不可逆）
	HarnessStateTerminated HarnessState = "terminated"
)

// RoundPhase 内层 ReAct 循环在当前轮内的子相位。
// Python: RoundPhase (state.py L27-39)
type RoundPhase string

const (
	// RoundPhaseBoundary 迭代之间：无模型调用、无工具在跑
	RoundPhaseBoundary RoundPhase = "boundary"
	// RoundPhaseModel 模型调用中，尚未提交 tool_calls
	RoundPhaseModel RoundPhase = "model"
	// RoundPhaseTool 正在执行本轮工具
	RoundPhaseTool RoundPhase = "tool"
)

// startMode 本轮启动方式（互斥）。
// Python: _StartMode (native_harness.py L108-117)
type startMode int

const (
	// startModeDefault 空闲起轮
	startModeDefault startMode = iota
	// startModeFollowUp 由排队输入驱动
	startModeFollowUp
	// startModeFailureRetry 崩溃后的一次性重试
	startModeFailureRetry
	// startModeResumeContinuation 暂停轮续跑
	startModeResumeContinuation
	// startModeContinuesTurn 任务计划续跑
	startModeContinuesTurn
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
// Python: HarnessInternalState.next_round_id
func (s *HarnessInternalState) NextRoundID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roundSeq++
	return s.roundSeq
}

// roundSeqSnapshot 返回当前轮次序号（测试/诊断用）。
func (s *HarnessInternalState) roundSeqSnapshot() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.roundSeq
}

// popInboxBatch 取队首批输入：连续字符串合批，遇非字符串停止。
// Python: _drain_pending_messages (native_harness.py L1239-1259)
// Go 的 InboxMessage 无 run_context（偏差 D-5），故不按上下文切批。
func (s *HarnessInternalState) popInboxBatch() (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return nil, false
	}
	first := s.queue[0]
	if _, isStr := first.Content.(string); !isStr {
		s.queue = s.queue[1:]
		return first.Content, true
	}
	batch := []string{first.Content.(string)}
	s.queue = s.queue[1:]
	for len(s.queue) > 0 {
		next := s.queue[0]
		text, isStr := next.Content.(string)
		if !isStr {
			break
		}
		batch = append(batch, text)
		s.queue = s.queue[1:]
	}
	if len(batch) == 1 {
		return batch[0], true
	}
	return batch, true
}

// clearInbox 清空待驱动队列。
func (s *HarnessInternalState) clearInbox() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = nil
}

// SetPausedQuery 记录暂停轮查询。
func (s *HarnessInternalState) SetPausedQuery(q string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pausedQuery = q
	s.hasPausedQuery = true
}

// ClearPausedQuery 清除暂停轮查询。
func (s *HarnessInternalState) ClearPausedQuery() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pausedQuery = ""
	s.hasPausedQuery = false
}

// PausedQuery 返回暂停轮查询；第二个返回值表示是否存在暂停轮。
func (s *HarnessInternalState) PausedQuery() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pausedQuery, s.hasPausedQuery
}
