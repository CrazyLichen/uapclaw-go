// 本文件对应 Python openjiuwen/harness/schema/interaction.py，
// 提供会话级交互监督器使用的领域对象。
//
// 这些对象不包含任何 JiuwenSwarm 协议类型：会话要么收到完整的用户 inputs，
// 要么收到"继续执行当前持续目标"的指令；只有拿到唯一输出租约的调用方，
// 才会得到输出消费者。
package schema

import (
	"context"
	"iter"
	"sync"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InteractionOutputDriver 交互输出驱动，由 DeepAgent 实现。
//
// Python 用 TYPE_CHECKING 前向引用 DeepAgent（interaction.py L23）；
// Go 不允许 schema 反向依赖 harness，故收窄为两个方法的接口。
type InteractionOutputDriver interface {
	// NextOutput 取一条输出（票作废或读到结束哨兵时返回 false）
	NextOutput(ctx context.Context, lease *OutputLease) (stream.Schema, bool)
	// DetachOutput 释放指定票号的租约
	DetachOutput(ctx context.Context, token string, abortActiveRound bool)
}

// InteractionPhase 会话级交互循环的内部生命周期相位。
// Python: InteractionPhase (interaction.py L28)
type InteractionPhase string

// InputDispatchMode 轮次活跃期间的用户输入路由策略。
// Python: InputDispatchMode (interaction.py L36)
type InputDispatchMode string

// RoundWorkKind work item 类型。
// Python: RoundWorkItem.kind (Literal["user", "goal"])
type RoundWorkKind string

// InteractionEventType 经输出流传输的交互事件类型。
// Python: InteractionEventType (interaction.py L143)
type InteractionEventType string

// SendInputRequest 宿主投递输入的请求。
//
// inputs["query"] 是唯一的用户文本来源；整个 mapping 原样保留，
// 使既有 DeepAgent 调用元数据（会话 id、可信目录等）随 work item 传递。
// Python: SendInputRequest (interaction.py L43)
type SendInputRequest struct {
	// RequestID 请求标识
	RequestID string
	// Inputs 完整调用载荷（含 query）
	Inputs map[string]any
	// Mode 分发模式（仅对用户文本生效；InteractiveInput 恢复请求忽略）
	Mode InputDispatchMode
}

// RoundContext 单个 work item 的执行上下文。
// Python: RoundWorkItem.context (Dict[str, object])
//
// ⚠️ 必须用 NewUserWorkItem / NewGoalWorkItem 构造：ResetLoop 的默认值
// 由工厂按 Python 语义（默认 True）显式赋值，零值 false 不代表"未设置"。
type RoundContext struct {
	// GoalID 目标标识（仅 kind=goal 有值）
	GoalID string
	// Revision 目标修订号（上游默认 0，0 是合法值而非"未设置"）
	Revision int
	// SessionID 会话标识（仅 kind=goal 有值）
	SessionID string
	// ResetLoop 执行前是否重置任务循环（迭代计数 + 停止条件状态）
	ResetLoop bool
}

// RoundWorkItem 交互监督器消费的一个工作单元。
// Python: RoundWorkItem (interaction.py L59)
type RoundWorkItem struct {
	// Kind 类型（user / goal）
	Kind RoundWorkKind
	// RequestID 宿主请求标识（goal 轮为空）
	RequestID string
	// Inputs 原样传给 invoke 的载荷
	Inputs map[string]any
	// Context 执行上下文
	Context RoundContext
	// IsFollowUp 是否为同一轮的续跑输入
	IsFollowUp bool
}

// ActiveInteractionRound 当前由监督器执行的 work item 记账。
// Python: ActiveInteractionRound (interaction.py L119)
type ActiveInteractionRound struct {
	// Work 当前 work item
	Work *RoundWorkItem
	// TaskID 轮次任务标识
	TaskID string
}

// InteractionEvent 由交互输出生产者发出的结构化事件。
// Python: InteractionEvent (interaction.py L150)
type InteractionEvent struct {
	// Type 事件类型
	Type InteractionEventType
	// Payload 事件载荷
	Payload map[string]any
}

// RoundOutcome 一个由 Agent 执行的交互轮次的结果。
//
// NextWork 由 Agent 决定，DeepAgent 负责入队。
// Python: RoundOutcome (interaction.py L181)
type RoundOutcome struct {
	// NextWork 下一轮待排队的 work item
	NextWork *RoundWorkItem
	// ErrorCode 错误码
	ErrorCode string
	// ErrorMessage 错误信息
	ErrorMessage string
}

// OutputLease 当前唯一被允许消费交互输出的宿主凭据。
// Python: OutputLease (interaction.py L194)
type OutputLease struct {
	// Token 票号
	Token string
	// closed 关闭即作废
	closed chan struct{}
	// closeOnce 保证只关闭一次
	closeOnce sync.Once
	// Finishing 是否已进入收尾（不再接收新数据）
	Finishing atomic.Bool
}

// InteractionOutputStream 交给租约持有者的可关闭输出句柄。
// Python: InteractionOutputStream (interaction.py L203)
type InteractionOutputStream struct {
	// driver 输出驱动（DeepAgent）
	driver InteractionOutputDriver
	// lease 持有的租约
	lease *OutputLease
	// closed 句柄是否已关闭
	closed atomic.Bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InteractionPhaseIdle 空闲（无输出消费者或无待办）
	InteractionPhaseIdle InteractionPhase = "idle"
	// InteractionPhaseRunning 有轮次在执行
	InteractionPhaseRunning InteractionPhase = "running"
	// InteractionPhaseTerminated 已终止（不可逆）
	InteractionPhaseTerminated InteractionPhase = "terminated"
)

const (
	// InputDispatchModeFollowUp 作为后续输入排队
	InputDispatchModeFollowUp InputDispatchMode = "follow_up"
	// InputDispatchModeSteer 直接塞入当前活跃轮的转向队列
	InputDispatchModeSteer InputDispatchMode = "steer"
)

const (
	// RoundWorkKindUser 宿主投递的用户轮
	RoundWorkKindUser RoundWorkKind = "user"
	// RoundWorkKindGoal 由 openjiuwen 全权持有的目标轮
	RoundWorkKindGoal RoundWorkKind = "goal"
)

const (
	// InteractionEventTypeGoalUpdated 目标记录变更
	InteractionEventTypeGoalUpdated InteractionEventType = "goal.updated"
	// InteractionEventTypeExecutionError 轮次执行失败
	InteractionEventTypeExecutionError InteractionEventType = "execution.error"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUserWorkItem 由宿主输入构造隔离的 user work item。
// Python: RoundWorkItem.user (interaction.py L69)
func NewUserWorkItem(requestID string, inputs map[string]any, isFollowUp bool, resetLoop bool) *RoundWorkItem {
	return &RoundWorkItem{
		Kind:       RoundWorkKindUser,
		RequestID:  requestID,
		Inputs:     utils.DeepCopyMap(inputs),
		Context:    RoundContext{ResetLoop: resetLoop},
		IsFollowUp: isFollowUp,
	}
}

// NewGoalWorkItem 构造由 openjiuwen 全权持有的 goal work item。
// Python: RoundWorkItem.goal (interaction.py L87)
func NewGoalWorkItem(inputs map[string]any, goalID string, revision int, sessionID string) *RoundWorkItem {
	return &RoundWorkItem{
		Kind:      RoundWorkKindGoal,
		RequestID: "",
		Inputs:    utils.DeepCopyMap(inputs),
		Context: RoundContext{
			GoalID:    goalID,
			Revision:  revision,
			SessionID: sessionID,
			ResetLoop: true,
		},
	}
}

// Query 返回任务循环使用的查询，刻意只从 Inputs 派生。
// Python: RoundWorkItem.query (interaction.py L109)
func (w *RoundWorkItem) Query() any {
	if w == nil || w.Inputs == nil {
		return nil
	}
	return w.Inputs["query"]
}

// ResetLoop 返回执行前是否重置任务循环。
// Python: RoundWorkItem.reset_loop (interaction.py L114)
func (w *RoundWorkItem) ResetLoop() bool {
	if w == nil {
		return true
	}
	return w.Context.ResetLoop
}

// RunKind 返回运行类型。
// Python: ActiveInteractionRound.run_kind (L127)
func (r *ActiveInteractionRound) RunKind() RoundWorkKind { return r.Work.Kind }

// RunContext 返回运行上下文。
// Python: ActiveInteractionRound.run_context (L131)
func (r *ActiveInteractionRound) RunContext() RoundContext { return r.Work.Context }

// GoalID 返回目标标识（仅 goal 轮有值）。
// Python: ActiveInteractionRound.goal_id (L135)
func (r *ActiveInteractionRound) GoalID() string { return r.Work.Context.GoalID }

// Revision 返回目标修订号（仅 goal 轮有值）。
// Python: ActiveInteractionRound.revision (L139)
func (r *ActiveInteractionRound) Revision() int { return r.Work.Context.Revision }

// NewGoalUpdatedEvent 构造目标变更事件。
// Python: InteractionEvent.goal_updated (interaction.py L160)
func NewGoalUpdatedEvent(goal map[string]any) InteractionEvent {
	return InteractionEvent{
		Type:    InteractionEventTypeGoalUpdated,
		Payload: map[string]any{"goal": utils.DeepCopyMap(goal)},
	}
}

// NewExecutionErrorEvent 构造执行失败事件。
// Python: InteractionEvent.execution_error (interaction.py L167)
func NewExecutionErrorEvent(code string, message string, goal map[string]any) InteractionEvent {
	payload := map[string]any{"code": code, "message": message}
	if goal != nil {
		payload["goal"] = utils.DeepCopyMap(goal)
	}
	return InteractionEvent{Type: InteractionEventTypeExecutionError, Payload: payload}
}

// ToOutputSchema 转换为标准输出流帧。
// Python: InteractionEvent.to_output_schema (interaction.py L157)
func (e InteractionEvent) ToOutputSchema() *stream.OutputSchema {
	return &stream.OutputSchema{
		Type:    string(e.Type),
		Index:   0,
		Payload: utils.DeepCopyMap(e.Payload),
	}
}

// NewOutputLease 创建一张新租约。
// Python: OutputLease(token=uuid4().hex, closed=asyncio.Event()) (L263)
func NewOutputLease(token string) *OutputLease {
	return &OutputLease{Token: token, closed: make(chan struct{})}
}

// IsClosed 返回租约是否已作废。
// Python: lease.closed.is_set()
func (l *OutputLease) IsClosed() bool {
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

// Close 作废租约（幂等）。
// Python: lease.closed.set()
func (l *OutputLease) Close() {
	l.closeOnce.Do(func() { close(l.closed) })
}

// NewInteractionOutputStream 创建输出句柄。
// Python: InteractionOutputStream(agent, lease) (L206)
func NewInteractionOutputStream(driver InteractionOutputDriver, lease *OutputLease) *InteractionOutputStream {
	return &InteractionOutputStream{driver: driver, lease: lease}
}

// Lease 返回句柄持有的租约。
func (s *InteractionOutputStream) Lease() *OutputLease { return s.lease }

// Next 取一条输出；句柄或租约失效时返回 false。
// Python: InteractionOutputStream.__anext__ (interaction.py L214)
func (s *InteractionOutputStream) Next(ctx context.Context) (stream.Schema, bool) {
	if s.closed.Load() {
		return nil, false
	}
	item, ok := s.driver.NextOutput(ctx, s.lease)
	if !ok {
		s.closed.Store(true)
		return nil, false
	}
	return item, true
}

// All 以 iter.Seq 形式遍历输出，对齐 Python 的 __aiter__。
func (s *InteractionOutputStream) All(ctx context.Context) iter.Seq[stream.Schema] {
	return func(yield func(stream.Schema) bool) {
		for {
			item, ok := s.Next(ctx)
			if !ok {
				return
			}
			if !yield(item) {
				return
			}
		}
	}
}

// Close 释放本句柄对应的租约；陈旧句柄无法释放更新的租约。
// Python: InteractionOutputStream.close (interaction.py L226)
func (s *InteractionOutputStream) Close(ctx context.Context, abortActiveRound bool) {
	if s.closed.Swap(true) {
		return
	}
	s.driver.DetachOutput(ctx, s.lease.Token, abortActiveRound)
}
