// 基于 Core sparse resume 的 root-only 逐卡权限队列。
// 队列负责三件事：
//  1. 串行化：一批并行工具调用产生的多个权限中断，只把队首一张卡暴露给前端；
//  2. 身份校验：用户答案必须精确对应队首卡，否则报错，绝不猜测；
//  3. fail-closed：与 Core 返回的中断集合对不齐时，隔离整个会话（quarantine），
//     之后该会话任何操作都报错，宁可让用户重来也不放行错配的答案。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/root_permission_queue.py（635 行）
package permissions

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RootPermissionQueueError 权限队列的 fail-closed 错误。
// Code 为错误码（对齐 Python 的 RootPermissionQueueError("xxx") 文本）。
// Python: class RootPermissionQueueError(ValueError)
type RootPermissionQueueError struct {
	// Code 错误码
	Code string
}

// RootPermissionCard 一次 root 工具调用的不可变身份与中断请求。
// Python: @dataclass(frozen=True, slots=True) RootPermissionCard
type RootPermissionCard struct {
	// Key 本次调用的号码牌
	Key *ToolInvocationKeyV1
	// ToolName 工具名
	ToolName string
	// State 卡片状态，见 RootPermissionCardState
	State string
	// Request 冻结的中断请求，仅 pending 及之后有值
	Request *InterruptRequestSnapshot
	// AutoManual 是否为自动转人工
	AutoManual bool
	// RootContext 冻结的决策上下文
	RootContext *RootDecisionContext
}

// RootPermissionSnapshot Core 的一次中断快照，按可见的 FIFO 顺序排列。
// Python: @dataclass(frozen=True, slots=True) RootPermissionSnapshot
type RootPermissionSnapshot struct {
	// Cards 队首起的卡片序列
	Cards []RootPermissionCard
	// Interactions 对应的交互项
	Interactions []map[string]any
}

// RootPermissionAnswer 为立即稀疏派发预留的一张队首卡答案。
// Python: @dataclass(frozen=True, slots=True) RootPermissionAnswer
type RootPermissionAnswer struct {
	// Card 被预留的卡片（状态 resuming）
	Card RootPermissionCard
	// InteractiveInput 稀疏输入，只含本卡的 tool_call_id
	InteractiveInput map[string]any
	// Payload 归一化后的答案
	Payload map[string]any
}

// RootPermissionClaim 一次已准入的稀疏恢复回调的唯一 CAS 赢家。
// Python: @dataclass(frozen=True, slots=True) RootPermissionClaim
type RootPermissionClaim struct {
	// Card 认领成功的卡片（状态 active）
	Card RootPermissionCard
	// Quarantined 该会话是否已被隔离
	Quarantined bool
}

// InterruptRequestSnapshot 中断请求的深拷贝快照。
// Python 用 copy.deepcopy(request) 冻结；Go 无内建 deepcopy，用独立结构承载
// 需要冻结的字段，保证 mark_pending 之后外部修改不影响已冻结的卡。
type InterruptRequestSnapshot struct {
	// Message 向用户展示的消息
	Message string
	// PayloadSchema 用户输入的数据结构定义
	PayloadSchema map[string]any
	// AutoConfirmKey 自动确认键
	AutoConfirmKey string
	// UIOptions UI 选项
	UIOptions []map[string]any
	// Metadata 中断元数据
	Metadata map[string]any
	// InterruptID 中断唯一标识
	InterruptID string
	// InterruptKind 中断类型
	InterruptKind string
	// InterruptOwner 抛出该中断的 rail 键
	InterruptOwner string
}

// RootPermissionQueue 持有待决卡顺序与每个 root 会话唯一在途的队首答案。
// Python: class RootPermissionQueue
type RootPermissionQueue struct {
	// idFactory 号码牌生成器，nil 时用默认实现
	idFactory func() string
	// cards invocation_id → 卡片
	cards map[string]RootPermissionCard
	// active (root, request, exec, call) → invocation_id
	active map[activeKey]string
	// pending (root, exec, call) → invocation_id
	pending map[pendingCallKey]string
	// order root_session_id → 有序 invocation_id 列表
	order map[string][]string
	// quarantined 已隔离的 root 会话
	quarantined map[string]struct{}
	// mu 保护以上全部字段；RLock 语义（可重入由调用方保证不递归调用公开方法）
	mu sync.Mutex
}

// activeKey 活跃卡的四元组键。
// Python: _active_key 返回的 tuple
type activeKey struct {
	rootSessionID      string
	requestID          string
	executionSessionID string
	toolCallID         string
}

// pendingCallKey 待决卡的三元组键。
// Python: pending_key = (root_session_id, execution_session_id, tool_call_id)
type pendingCallKey struct {
	rootSessionID      string
	executionSessionID string
	toolCallID         string
}

// InteractionItem 表示一个 Core 中断项，可给出身份与请求值。
// 该接口用于解耦：permissions 包需要读取中断项，但不希望依赖具体的中断项类型。
// 宿主把 schema 层的中断项适配成该接口即可。
type InteractionItem interface {
	// InteractionID 中断项的内部 ID（对应 Core 的 tool_call_id）
	InteractionID() string
	// InteractionValue 中断请求值
	InteractionValue() any
}

// InteractionRequestProvider 表示一个中断请求，可给出类型与元数据。
// 实现方通常是 *saschema.InterruptRequest（或其适配器）。队列只用它读取
// interrupt_kind 与 metadata，不解析别的字段。
type InteractionRequestProvider interface {
	// RequestInterruptKind 中断类型
	RequestInterruptKind() string
	// RequestMetadata 中断元数据
	RequestMetadata() map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// RootPermissionCardState 卡片状态取值。
// Python: RootPermissionCardState = Literal["active", "pending", "resuming"]
const (
	// RootPermissionCardStateActive 工具正在执行，尚未卡住
	RootPermissionCardStateActive = "active"
	// RootPermissionCardStatePending 已卡住，排队等待用户回答
	RootPermissionCardStatePending = "pending"
	// RootPermissionCardStateResuming 已预留答案，等待回调认领
	RootPermissionCardStateResuming = "resuming"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxRootPermissionCards 单个快照允许的最大卡片数。
	// Python: MAX_ROOT_PERMISSION_CARDS
	MaxRootPermissionCards = 64

	// rootPermissionAnswerFeedbackMaxLength 反馈文本最大长度。
	// Python: len(feedback) > 4000
	rootPermissionAnswerFeedbackMaxLength = 4000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// rootPermissionQueueAllowedAnswerKeys reserve_answer 接受的答案键集合。
// Python: _normalize_answer 的 allowed
var rootPermissionQueueAllowedAnswerKeys = map[string]struct{}{
	"approved":      {},
	"auto_confirm":  {},
	"persist_allow": {},
	"feedback":      {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *RootPermissionQueueError) Error() string {
	return e.Code
}

// NewRootPermissionQueue 创建权限队列。
// idFactory 为 nil 时使用默认的随机号码牌生成器（对齐 Python secrets.token_urlsafe(18)）。
// Python: RootPermissionQueue.__init__(*, id_factory=None)
// IsRootPermissionQueue 标识该值为根权限队列。
//
// 供 common/rails 的窄接口 RootPermissionQueueAccessor 判定，
// 避免 rails ↔ rails/permissions 循环依赖。
func (q *RootPermissionQueue) IsRootPermissionQueue() {}

// NewRootPermissionQueue 创建根权限队列。
//
// Python: RootPermissionQueue(id_factory)
func NewRootPermissionQueue(idFactory func() string) *RootPermissionQueue {
	if idFactory == nil {
		idFactory = defaultInvocationIDFactory
	}
	return &RootPermissionQueue{
		idFactory:   idFactory,
		cards:       make(map[string]RootPermissionCard),
		active:      make(map[activeKey]string),
		pending:     make(map[pendingCallKey]string),
		order:       make(map[string][]string),
		quarantined: make(map[string]struct{}),
	}
}

// Begin 绑定一次新调用；若 Core 恢复时命中同一调用则返回其待决卡。
// Python: RootPermissionQueue.begin(*, root_session_id, request_id,
// execution_session_id, tool_call_id, tool_name)
func (q *RootPermissionQueue) Begin(
	rootSessionID string,
	requestID string,
	executionSessionID string,
	toolCallID string,
	toolName string,
) (RootPermissionCard, error) {
	// 先把几个必填标识补齐并做长度校验，任一不合法就拒绝绑定。
	rootSessionID, err := rootQueueText("root_session_id", rootSessionID, 512)
	if err != nil {
		return RootPermissionCard{}, err
	}
	executionSessionID, err = rootQueueText("execution_session_id", executionSessionID, 512)
	if err != nil {
		return RootPermissionCard{}, err
	}
	toolCallID, err = rootQueueText("tool_call_id", toolCallID, 512)
	if err != nil {
		return RootPermissionCard{}, err
	}
	toolName, err = rootQueueText("tool_name", toolName, 512)
	if err != nil {
		return RootPermissionCard{}, err
	}

	// 同一「会话 + 执行 + 工具调用」若已有待决卡，说明是 Core 恢复后的重放，
	// 直接把原卡还回去，不再新建。
	pendingKey := pendingCallKey{rootSessionID, executionSessionID, toolCallID}

	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.raiseIfQuarantinedLocked(rootSessionID); err != nil {
		return RootPermissionCard{}, err
	}

	if pendingID, ok := q.pending[pendingKey]; ok {
		card, exists := q.cards[pendingID]
		if !exists || card.ToolName != toolName {
			return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_pending_mismatch"}
		}
		return card, nil
	}

	// 走到这里说明是新调用：request_id 变成必填，且四条标识组合不能重复。
	requestID, err = rootQueueText("request_id", requestID, 512)
	if err != nil {
		return RootPermissionCard{}, err
	}

	key := activeKey{rootSessionID, requestID, executionSessionID, toolCallID}
	if _, exists := q.active[key]; exists {
		return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_duplicate_call"}
	}

	// 生成新的调用标识，并保证不与已有卡片撞号。
	invocationID, err := rootQueueText("invocation_id", q.idFactory(), MaxToolInvocationIDLength)
	if err != nil {
		return RootPermissionCard{}, err
	}
	if _, exists := q.cards[invocationID]; exists {
		return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_id_collision"}
	}

	toolKey, err := NewToolInvocationKeyV1(
		invocationID, rootSessionID, requestID, ExecutorKindAgent,
		executionSessionID, toolCallID, ToolInvocationKeyVersion,
	)
	if err != nil {
		return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_invocation_id_invalid"}
	}

	// 卡片落库并登记为活跃调用，后续中断请求按 active 索引找回来。
	card := RootPermissionCard{
		Key:      toolKey,
		ToolName: toolName,
		State:    RootPermissionCardStateActive,
	}
	q.cards[invocationID] = card
	q.active[key] = invocationID
	return card, nil
}

// MarkPending 冻结一次活跃调用发出的中断请求，把卡片转为 pending。
// Python: RootPermissionQueue.mark_pending(key, *, request, auto_manual, root_context)
func (q *RootPermissionQueue) MarkPending(
	key *ToolInvocationKeyV1,
	request *InterruptRequestSnapshot,
	autoManual bool,
	rootContext *RootDecisionContext,
) (RootPermissionCard, error) {
	if request == nil {
		return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_request_invalid"}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	card, err := q.exactCardLocked(key)
	if err != nil {
		return RootPermissionCard{}, err
	}
	if card.State == RootPermissionCardStatePending {
		if !snapshotEqual(card.Request, request) {
			return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_request_changed"}
		}
		return card, nil
	}
	if card.State != RootPermissionCardStateActive {
		return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_pending_transition"}
	}

	pendingKey := pendingCallKey{
		key.RootSessionID, key.ExecutionSessionID, key.ToolCallID,
	}
	if existingID, exists := q.pending[pendingKey]; exists && existingID != key.InvocationID {
		return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_pending_collision"}
	}

	frozen := card
	frozen.State = RootPermissionCardStatePending
	frozen.Request = cloneSnapshot(request)
	frozen.AutoManual = autoManual
	frozen.RootContext = rootContext

	q.cards[key.InvocationID] = frozen
	delete(q.active, activeKeyOf(card.Key))
	q.pending[pendingKey] = key.InvocationID
	return frozen, nil
}

// PendingForCall 返回精确匹配的待决兄弟卡，不改变队列顺序。
// Python: RootPermissionQueue.pending_for_call(*, root_session_id,
// execution_session_id, tool_call_id)
func (q *RootPermissionQueue) PendingForCall(
	rootSessionID string,
	executionSessionID string,
	toolCallID string,
) (RootPermissionCard, bool) {
	lookup := pendingCallKey{rootSessionID, executionSessionID, toolCallID}

	q.mu.Lock()
	defer q.mu.Unlock()

	invocationID, ok := q.pending[lookup]
	if !ok {
		return RootPermissionCard{}, false
	}
	card, exists := q.cards[invocationID]
	if !exists || card.State != RootPermissionCardStatePending {
		return RootPermissionCard{}, false
	}
	return card, true
}

// ReserveAnswer 校验并预留唯一一张队首卡的答案。
// Python: RootPermissionQueue.reserve_answer(root_session_id, incoming)
func (q *RootPermissionQueue) ReserveAnswer(
	rootSessionID string,
	rawInputs map[string]any,
	userInputs map[string]any,
) (RootPermissionAnswer, error) {
	// ① 校验入参：会话 ID 有界、不接受原始输入、必须恰好一张卡片一条回答
	rootSessionID, err := rootQueueText("root_session_id", rootSessionID, 512)
	if err != nil {
		return RootPermissionAnswer{}, err
	}
	if rawInputs != nil {
		return RootPermissionAnswer{}, &RootPermissionQueueError{"permission_queue_raw_input_invalid"}
	}
	if len(userInputs) != 1 {
		return RootPermissionAnswer{}, &RootPermissionQueueError{"permission_queue_one_answer_required"}
	}

	// ② 从唯一的回答里取出卡片 ID 与回答内容，并校验结构
	var rawCardID string
	var rawPayload any
	for id, payload := range userInputs {
		rawCardID, rawPayload = id, payload
	}
	cardID, err := rootQueueText("card_id", rawCardID, 128)
	if err != nil {
		return RootPermissionAnswer{}, err
	}
	payloadMap, ok := rawPayload.(map[string]any)
	if !ok {
		return RootPermissionAnswer{}, &RootPermissionQueueError{"permission_queue_payload_invalid"}
	}

	// ③ 入队锁：整段判定与状态改写必须原子，避免与出队/隔离并发
	q.mu.Lock()
	defer q.mu.Unlock()

	// ④ 隔离态的会话不得再接受回答
	if err := q.raiseIfQuarantinedLocked(rootSessionID); err != nil {
		return RootPermissionAnswer{}, err
	}

	// ⑤ 只允许回答队首那张仍在等待的卡片（不能插队、不能重复回答）
	order := q.order[rootSessionID]
	if len(order) == 0 {
		return RootPermissionAnswer{}, &RootPermissionQueueError{"permission_queue_empty"}
	}
	card, exists := q.cards[order[0]]
	if !exists || card.State != RootPermissionCardStatePending {
		return RootPermissionAnswer{}, &RootPermissionQueueError{"permission_queue_head_invalid"}
	}
	if cardID != card.Key.InvocationID {
		return RootPermissionAnswer{}, &RootPermissionQueueError{"permission_queue_non_head_answer"}
	}

	// ⑥ 归一化回答内容
	payload, err := normalizeRootPermissionAnswer(payloadMap)
	if err != nil {
		return RootPermissionAnswer{}, err
	}

	// ⑦ 占位：队首卡片由待回答改为恢复中，防止同一张卡被再次认领
	reserved := card
	reserved.State = RootPermissionCardStateResuming
	q.cards[card.Key.InvocationID] = reserved

	// ⑧ 组装返回：卡片副本 + 单条交互输入 + 归一化后的回答
	sparse := map[string]any{card.Key.ToolCallID: cloneMap(payload)}

	return RootPermissionAnswer{
		Card:             reserved,
		InteractiveInput: sparse,
		Payload:          payload,
	}, nil
}

// ReleaseAnswer 把未派发的队首预留退回 pending。
// Python: RootPermissionQueue.release_answer(answer)
func (q *RootPermissionQueue) ReleaseAnswer(answer RootPermissionAnswer) {
	q.mu.Lock()
	defer q.mu.Unlock()

	current, exists := q.cards[answer.Card.Key.InvocationID]
	if !exists || !cardEqual(current, answer.Card) ||
		current.State != RootPermissionCardStateResuming {
		return
	}
	restored := current
	restored.State = RootPermissionCardStatePending
	q.cards[current.Key.InvocationID] = restored
}

// ClaimAnswerForCall 把一个已准入的答案由 resuming CAS 到回调持有的 active。
// Python: RootPermissionQueue.claim_answer_for_call(*, root_session_id,
// execution_session_id, tool_call_id, tool_name)
func (q *RootPermissionQueue) ClaimAnswerForCall(
	rootSessionID string,
	executionSessionID string,
	toolCallID string,
	toolName string,
) (RootPermissionClaim, error) {
	rootID, err := rootQueueText("root_session_id", rootSessionID, 512)
	if err != nil {
		return RootPermissionClaim{}, err
	}
	execID, err := rootQueueText("execution_session_id", executionSessionID, 512)
	if err != nil {
		return RootPermissionClaim{}, err
	}
	callID, err := rootQueueText("tool_call_id", toolCallID, 512)
	if err != nil {
		return RootPermissionClaim{}, err
	}
	name, err := rootQueueText("tool_name", toolName, 512)
	if err != nil {
		return RootPermissionClaim{}, err
	}

	lookup := pendingCallKey{rootID, execID, callID}

	q.mu.Lock()
	defer q.mu.Unlock()

	invocationID, ok := q.pending[lookup]
	card, exists := q.cards[invocationID]
	if !ok || !exists || card.State != RootPermissionCardStateResuming {
		return RootPermissionClaim{}, &RootPermissionQueueError{"permission_queue_answer_not_reserved"}
	}
	if card.ToolName != name {
		return RootPermissionClaim{}, &RootPermissionQueueError{"permission_queue_pending_mismatch"}
	}

	key := activeKeyOf(card.Key)
	if _, dup := q.active[key]; dup {
		return RootPermissionClaim{}, &RootPermissionQueueError{"permission_queue_duplicate_call"}
	}

	claimed := card
	claimed.State = RootPermissionCardStateActive
	q.cards[card.Key.InvocationID] = claimed
	delete(q.pending, lookup)
	q.active[key] = card.Key.InvocationID

	_, quarantined := q.quarantined[card.Key.RootSessionID]
	return RootPermissionClaim{Card: claimed, Quarantined: quarantined}, nil
}

// ConsumeAcceptedAnswerIfQuarantined 让请求清理消费一个未被回调认领的已准入答案。
// Python: RootPermissionQueue.consume_accepted_answer_if_quarantined(answer)
func (q *RootPermissionQueue) ConsumeAcceptedAnswerIfQuarantined(answer RootPermissionAnswer) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	current, exists := q.cards[answer.Card.Key.InvocationID]
	if !exists || !cardEqual(current, answer.Card) {
		return false
	}
	if current.State != RootPermissionCardStateResuming {
		return false
	}
	if _, quarantined := q.quarantined[current.Key.RootSessionID]; !quarantined {
		return false
	}
	q.removeLocked(current)
	return true
}

// Reconcile 对齐 Core 返回的中断集合，只暴露其队首。
// 任何对不齐的情况（快照非法、数量不符、身份不符、混入其它中断、
// 重复调用、卡状态不对、兄弟卡缺失）都隔离会话并报错。
// Python: RootPermissionQueue.reconcile(result, *, root_session_id)
func (q *RootPermissionQueue) Reconcile(
	resultType string,
	state []any,
	interruptIDs []any,
	rootSessionID string,
) (*RootPermissionSnapshot, error) {
	rootSessionID, err := rootQueueText("root_session_id", rootSessionID, 512)
	if err != nil {
		return nil, err
	}
	if resultType != "interrupt" {
		return nil, nil
	}

	// 第一段：先否决快照本身——两份列表必须同时存在、长度一致、条数在合理范围。
	// 这一段的任何不合规都说明 Core 与本队列已经不同步，直接隔离该会话。
	if state == nil || interruptIDs == nil || len(state) != len(interruptIDs) {
		q.Quarantine(rootSessionID)
		return nil, &RootPermissionQueueError{"permission_queue_snapshot_invalid"}
	}
	if len(state) == 0 {
		q.Quarantine(rootSessionID)
		return nil, &RootPermissionQueueError{"permission_queue_snapshot_invalid"}
	}
	if len(state) > MaxRootPermissionCards {
		q.Quarantine(rootSessionID)
		return nil, &RootPermissionQueueError{"permission_queue_snapshot_invalid"}
	}

	// 第二段：逐条核对身份并取出调用键。客户端工具类中断不归本队列管，跳过；
	// 取不到键的中断记为「其它中断」，只要混入就整批拒绝。
	interactions := make([]map[string]any, 0, len(state))
	keys := make([]*ToolInvocationKeyV1, 0, len(state))
	sawOther := false

	for index, rawID := range interruptIDs {
		innerID, err := rootQueueText("tool_call_id", rawID, 512)
		if err != nil {
			q.Quarantine(rootSessionID)
			return nil, &RootPermissionQueueError{"permission_queue_snapshot_invalid"}
		}
		interaction := interactionOf(state[index])
		if interaction == nil || interaction["id"] != innerID {
			q.Quarantine(rootSessionID)
			return nil, &RootPermissionQueueError{"permission_queue_snapshot_mismatch"}
		}

		request, ok := toInteractionRequest(interaction["value"])
		if !ok {
			q.Quarantine(rootSessionID)
			return nil, &RootPermissionQueueError{"permission_queue_snapshot_mismatch"}
		}
		if request.Kind == saschema.InterruptKindClientTool {
			continue
		}
		wireKey := requestKeyOf(request)
		if wireKey == nil {
			sawOther = true
			interactions = append(interactions, interaction)
			continue
		}

		key, err := ToolInvocationKeyV1FromWire(wireKey)
		if err != nil {
			q.Quarantine(rootSessionID)
			return nil, &RootPermissionQueueError{"permission_queue_locator_invalid"}
		}
		if key.RootSessionID != rootSessionID || key.ToolCallID != innerID {
			q.Quarantine(rootSessionID)
			return nil, &RootPermissionQueueError{"permission_queue_locator_mismatch"}
		}
		keys = append(keys, key)
		interactions = append(interactions, interaction)
	}

	// 第三段：全是其它中断等于本队列无事可做；混着来则判为混合中断。
	if len(keys) == 0 {
		return nil, nil
	}
	if sawOther || len(keys) != len(interactions) {
		q.Quarantine(rootSessionID)
		return nil, &RootPermissionQueueError{"permission_queue_mixed_interrupt"}
	}

	// 第四段：同一次调用不能出现两张卡。
	seenInvocation := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, dup := seenInvocation[key.InvocationID]; dup {
			q.Quarantine(rootSessionID)
			return nil, &RootPermissionQueueError{"permission_queue_duplicate"}
		}
		seenInvocation[key.InvocationID] = struct{}{}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// 第五段：按调用键取卡，卡不在或不是 pending 都算对不齐。
	cards := make([]RootPermissionCard, 0, len(keys))
	for _, key := range keys {
		card, err := q.exactCardLocked(key)
		if err != nil {
			q.quarantined[rootSessionID] = struct{}{}
			return nil, err
		}
		if card.State != RootPermissionCardStatePending {
			q.quarantined[rootSessionID] = struct{}{}
			return nil, &RootPermissionQueueError{"permission_queue_card_not_pending"}
		}
		cards = append(cards, card)
	}

	// 第六段：本会话其它卡不能有「还在排队」或「恢复了一半」的，否则只能隔离。
	liveIDs := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		liveIDs[key.InvocationID] = struct{}{}
	}
	for _, card := range q.cards {
		if card.Key.RootSessionID != rootSessionID {
			continue
		}
		if _, live := liveIDs[card.Key.InvocationID]; live {
			continue
		}
		if card.State == RootPermissionCardStateResuming {
			q.quarantined[rootSessionID] = struct{}{}
			return nil, &RootPermissionQueueError{"permission_queue_unclaimed_resume_missing"}
		}
		if card.State == RootPermissionCardStatePending {
			q.quarantined[rootSessionID] = struct{}{}
			return nil, &RootPermissionQueueError{"permission_queue_sibling_missing"}
		}
	}

	// 最后：按 Core 给的顺序输出队首，后面交给调用方只暴露第一张。
	order := make([]string, 0, len(keys))
	for _, key := range keys {
		order = append(order, key.InvocationID)
	}
	q.order[rootSessionID] = order

	return &RootPermissionSnapshot{Cards: cards, Interactions: interactions}, nil
}

// Get 按 key 精确取卡，key 不完全一致时返回 nil。
// Python: RootPermissionQueue.get(key)
func (q *RootPermissionQueue) Get(key *ToolInvocationKeyV1) (RootPermissionCard, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	card, exists := q.cards[key.InvocationID]
	if !exists || !keyEqual(card.Key, key) {
		return RootPermissionCard{}, false
	}
	return card, true
}

// Finish 删除精确匹配的卡，返回是否删除。
// Python: RootPermissionQueue.finish(key)
func (q *RootPermissionQueue) Finish(key *ToolInvocationKeyV1) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	card, exists := q.cards[key.InvocationID]
	if !exists || !keyEqual(card.Key, key) {
		return false
	}
	q.removeLocked(card)
	return true
}

// FinishActive 只删除仍由回调持有的精确权限评估卡（必须处于 active）。
// Python: RootPermissionQueue.finish_active(key)
func (q *RootPermissionQueue) FinishActive(key *ToolInvocationKeyV1) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	card, exists := q.cards[key.InvocationID]
	if !exists || !keyEqual(card.Key, key) || card.State != RootPermissionCardStateActive {
		return false
	}
	q.removeLocked(card)
	return true
}

// HasLive 返回队列是否仍持有任何权限准入状态；rootSessionID 为空表示不限会话。
// Python: RootPermissionQueue.has_live(*, root_session_id=None)
func (q *RootPermissionQueue) HasLive(rootSessionID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, card := range q.cards {
		if rootSessionID == "" || card.Key.RootSessionID == rootSessionID {
			return true
		}
	}
	return false
}

// SnapshotScope 返回指定会话（可再限定请求）下的全部卡片 key。
// Python: RootPermissionQueue.snapshot_scope(*, root_session_id, request_id=None)
func (q *RootPermissionQueue) SnapshotScope(rootSessionID string, requestID *string) []*ToolInvocationKeyV1 {
	q.mu.Lock()
	defer q.mu.Unlock()

	keys := make([]*ToolInvocationKeyV1, 0)
	for _, card := range q.cards {
		if card.Key.RootSessionID != rootSessionID {
			continue
		}
		if requestID != nil && card.Key.RequestID != *requestID {
			continue
		}
		keys = append(keys, card.Key)
	}
	return keys
}

// BeginCutover 在 Core 取消之前隔离一个非空的 root scope。
// Python: RootPermissionQueue.begin_cutover(*, root_session_id)
func (q *RootPermissionQueue) BeginCutover(rootSessionID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, already := q.quarantined[rootSessionID]; already {
		return true
	}
	hasCard := false
	for _, card := range q.cards {
		if card.Key.RootSessionID == rootSessionID {
			hasCard = true
			break
		}
	}
	if !hasCard {
		return false
	}
	q.quarantined[rootSessionID] = struct{}{}
	return true
}

// CutoverContinuationScope 在活跃回调清理后冻结 Core 续接卡。
// Python: RootPermissionQueue.cutover_continuation_scope(*, root_session_id)
func (q *RootPermissionQueue) CutoverContinuationScope(rootSessionID string) ([]*ToolInvocationKeyV1, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, started := q.quarantined[rootSessionID]; !started {
		return nil, &RootPermissionQueueError{"permission_queue_cutover_not_started"}
	}

	cards := make([]RootPermissionCard, 0)
	for _, card := range q.cards {
		if card.Key.RootSessionID == rootSessionID {
			cards = append(cards, card)
		}
	}
	for _, card := range cards {
		if card.State == RootPermissionCardStateActive {
			return nil, &RootPermissionQueueError{"permission_queue_active_cleanup_pending"}
		}
	}
	for _, card := range cards {
		if card.State == RootPermissionCardStateResuming {
			return nil, &RootPermissionQueueError{"permission_queue_unowned_resume"}
		}
	}

	keys := make([]*ToolInvocationKeyV1, 0, len(cards))
	for _, card := range cards {
		keys = append(keys, card.Key)
	}
	return keys, nil
}

// CancelSnapshot 取消一批快照卡，返回实际删除数。
// Python: RootPermissionQueue.cancel_snapshot(keys)
func (q *RootPermissionQueue) CancelSnapshot(keys []*ToolInvocationKeyV1) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	removed := 0
	for _, key := range keys {
		card, exists := q.cards[key.InvocationID]
		if exists && keyEqual(card.Key, key) {
			q.removeLocked(card)
			removed++
		}
	}
	return removed
}

// DiscardContinuation 删除一批已确认丢弃的 Core 续接卡并恢复队列。
// Python: RootPermissionQueue.discard_continuation(keys, *, root_session_id)
func (q *RootPermissionQueue) DiscardContinuation(
	keys []*ToolInvocationKeyV1,
	rootSessionID string,
) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, started := q.quarantined[rootSessionID]; !started {
		return 0, &RootPermissionQueueError{"permission_queue_cutover_not_started"}
	}

	expected := make(map[string]struct{})
	for _, key := range keys {
		if key.RootSessionID == rootSessionID {
			expected[key.InvocationID] = struct{}{}
		}
	}
	live := make(map[string]struct{})
	for _, card := range q.cards {
		if card.Key.RootSessionID == rootSessionID {
			live[card.Key.InvocationID] = struct{}{}
		}
	}
	if !stringSetEqual(live, expected) {
		return 0, &RootPermissionQueueError{"permission_queue_discard_scope_changed"}
	}

	removed := 0
	for _, key := range keys {
		if key.RootSessionID != rootSessionID {
			continue
		}
		card, exists := q.cards[key.InvocationID]
		if exists && keyEqual(card.Key, key) {
			q.removeLocked(card)
			removed++
		}
	}
	delete(q.quarantined, rootSessionID)
	return removed, nil
}

// Quarantine 隔离指定 root 会话。
// Python: RootPermissionQueue.quarantine(root_session_id)
func (q *RootPermissionQueue) Quarantine(rootSessionID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.quarantined[rootSessionID] = struct{}{}
}

// RaiseIfQuarantined 会话已隔离时报错。
// Python: RootPermissionQueue.raise_if_quarantined(root_session_id)
func (q *RootPermissionQueue) RaiseIfQuarantined(rootSessionID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.raiseIfQuarantinedLocked(rootSessionID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// interactionRequest reconcile 从交互项里解析出的中断请求视图。
type interactionRequest struct {
	// Kind 中断类型
	Kind string
	// Metadata 中断元数据
	Metadata map[string]any
	// Value 结构化值（Python 的 request.value 退化形态）
	Value map[string]any
}

// raiseIfQuarantinedLocked 已持锁的隔离检查。
// Python: _raise_if_quarantined_locked
func (q *RootPermissionQueue) raiseIfQuarantinedLocked(rootSessionID string) error {
	if _, quarantined := q.quarantined[rootSessionID]; quarantined {
		return &RootPermissionQueueError{"permission_queue_quarantined"}
	}
	return nil
}

// exactCardLocked 已持锁的精确取卡，不存在或 key 不符时报错。
// Python: _exact_card_locked
func (q *RootPermissionQueue) exactCardLocked(key *ToolInvocationKeyV1) (RootPermissionCard, error) {
	card, exists := q.cards[key.InvocationID]
	if !exists || !keyEqual(card.Key, key) {
		return RootPermissionCard{}, &RootPermissionQueueError{"permission_queue_card_mismatch"}
	}
	return card, nil
}

// removeLocked 已持锁的删除，同时清理 active / pending / order 三张索引表。
// Python: _remove_locked
func (q *RootPermissionQueue) removeLocked(card RootPermissionCard) {
	delete(q.cards, card.Key.InvocationID)
	delete(q.active, activeKeyOf(card.Key))
	delete(q.pending, pendingCallKey{
		card.Key.RootSessionID, card.Key.ExecutionSessionID, card.Key.ToolCallID,
	})

	remaining := make([]string, 0, len(q.order[card.Key.RootSessionID]))
	for _, item := range q.order[card.Key.RootSessionID] {
		if item != card.Key.InvocationID {
			remaining = append(remaining, item)
		}
	}
	if len(remaining) > 0 {
		q.order[card.Key.RootSessionID] = remaining
	} else {
		delete(q.order, card.Key.RootSessionID)
	}
}

// activeKeyOf 由 key 构造四元组。
// Python: _active_key(key)
func activeKeyOf(key *ToolInvocationKeyV1) activeKey {
	return activeKey{
		key.RootSessionID, key.RequestID, key.ExecutionSessionID, key.ToolCallID,
	}
}

// rootQueueText 归一化身份字段，失败时转成队列错误码。
// Python: _text(name, value, *, maximum=512)
func rootQueueText(name string, value any, maximum int) (string, error) {
	normalized, err := NormalizeToolInvocationText(name, value, maximum)
	if err != nil {
		return "", &RootPermissionQueueError{fmt.Sprintf("permission_queue_%s_invalid", name)}
	}
	return normalized, nil
}

// normalizeRootPermissionAnswer 归一化用户答案。
// 只允许 approved/auto_confirm/persist_allow/feedback 四个键；
// persist_allow 为真时必须 approved 且 auto_confirm 都为真。
// Python: _normalize_answer(value)
func normalizeRootPermissionAnswer(value map[string]any) (map[string]any, error) {
	for key := range value {
		if _, allowed := rootPermissionQueueAllowedAnswerKeys[key]; !allowed {
			return nil, &RootPermissionQueueError{"permission_queue_payload_invalid"}
		}
	}

	approved, okApproved := value["approved"].(bool)
	autoConfirm, okConfirm := value["auto_confirm"].(bool)
	if !okApproved || !okConfirm {
		return nil, &RootPermissionQueueError{"permission_queue_decision_invalid"}
	}

	persistAllow := false
	if raw, exists := value["persist_allow"]; exists {
		asBool, ok := raw.(bool)
		if !ok {
			return nil, &RootPermissionQueueError{"permission_queue_decision_invalid"}
		}
		persistAllow = asBool
	}

	feedback := ""
	if raw, exists := value["feedback"]; exists {
		asString, ok := raw.(string)
		if !ok {
			return nil, &RootPermissionQueueError{"permission_queue_decision_invalid"}
		}
		feedback = asString
	}

	if len(feedback) > rootPermissionAnswerFeedbackMaxLength {
		return nil, &RootPermissionQueueError{"permission_queue_feedback_too_large"}
	}
	if persistAllow && (!approved || !autoConfirm) {
		return nil, &RootPermissionQueueError{"permission_queue_scope_invalid"}
	}

	result := map[string]any{
		"approved":     approved,
		"auto_confirm": autoConfirm,
		"feedback":     feedback,
	}
	if _, exists := value["persist_allow"]; exists {
		result["persist_allow"] = persistAllow
	}
	return result, nil
}

// interactionOf 从 Core 的中断项里取出 {id, value} 形态。
// 接受三种输入形态（对齐 Python 的 getattr(item, "payload", item) 与 Mapping 分支）：
//  1. 实现 InteractionItem 接口的对象；
//  2. {"id":..., "value":...} 映射；
//  3. {"payload": {上述两种形态}} 包装。
//
// Python: _interaction(item)
func interactionOf(item any) map[string]any {
	if provider, ok := item.(InteractionItem); ok {
		return map[string]any{
			"id":    trimSpace(provider.InteractionID()),
			"value": provider.InteractionValue(),
		}
	}
	payload, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	if nested, hasPayload := payload["payload"]; hasPayload {
		return interactionOf(nested)
	}
	if _, hasID := payload["id"]; hasID {
		if _, hasValue := payload["value"]; hasValue {
			id, _ := payload["id"].(string)
			return map[string]any{
				"id":    trimSpace(id),
				"value": payload["value"],
			}
		}
	}
	return nil
}

// toInteractionRequest 把中断请求值转成 reconcile 用的视图。
// 优先使用实现了 InteractionRequestProvider 的具体类型（例如
// *saschema.InterruptRequest），否则回退到 map 形态读取 kind/metadata/value。
func toInteractionRequest(value any) (*interactionRequest, bool) {
	if provider, ok := value.(InteractionRequestProvider); ok {
		request := &interactionRequest{
			Kind:     provider.RequestInterruptKind(),
			Metadata: provider.RequestMetadata(),
		}
		if raw, ok := value.(map[string]any); ok {
			request.Value = raw
		}
		return request, true
	}
	asMap, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	request := &interactionRequest{Value: asMap}
	if kind, ok := asMap["interrupt_kind"].(string); ok {
		request.Kind = kind
	}
	if metadata, ok := asMap["metadata"].(map[string]any); ok {
		request.Metadata = metadata
	}
	return request, true
}

// requestKeyOf 从中断请求里取出 tool_invocation_key。
// 优先读 metadata，回退读 value（对齐 Python 两处查找）。
// Python: _request_key(request)
func requestKeyOf(request *interactionRequest) map[string]any {
	if request.Metadata != nil {
		if raw, ok := request.Metadata["tool_invocation_key"].(map[string]any); ok {
			return raw
		}
	}
	if request.Value != nil {
		if raw, ok := request.Value["tool_invocation_key"].(map[string]any); ok {
			return raw
		}
	}
	return nil
}

// defaultInvocationIDFactory 生成默认号码牌。
// Python: lambda: f"tiv_{secrets.token_urlsafe(18)}"
func defaultInvocationIDFactory() string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "tiv_fallback"
	}
	return "tiv_" + base64.RawURLEncoding.EncodeToString(buf)
}

// cloneSnapshot 深拷贝中断请求快照。
// Python: deepcopy(request)
func cloneSnapshot(source *InterruptRequestSnapshot) *InterruptRequestSnapshot {
	if source == nil {
		return nil
	}
	clone := *source
	clone.PayloadSchema = cloneMap(source.PayloadSchema)
	clone.Metadata = cloneMap(source.Metadata)
	if source.UIOptions != nil {
		clone.UIOptions = make([]map[string]any, len(source.UIOptions))
		for i, item := range source.UIOptions {
			clone.UIOptions[i] = cloneMap(item)
		}
	}
	return &clone
}

// snapshotEqual 判断两个快照是否相等（Python dataclass 的 __eq__）。
func snapshotEqual(a, b *InterruptRequestSnapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Message == b.Message &&
		a.AutoConfirmKey == b.AutoConfirmKey &&
		a.InterruptID == b.InterruptID &&
		a.InterruptKind == b.InterruptKind &&
		a.InterruptOwner == b.InterruptOwner &&
		mapEqual(a.PayloadSchema, b.PayloadSchema) &&
		mapEqual(a.Metadata, b.Metadata) &&
		uiOptionsEqual(a.UIOptions, b.UIOptions)
}

// mapEqual 浅比较两个 map（值为标量或可比较结构）。
func mapEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for key, left := range a {
		right, ok := b[key]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", left) != fmt.Sprintf("%v", right) {
			return false
		}
	}
	return true
}

// uiOptionsEqual 比较 UI 选项列表。
func uiOptionsEqual(a, b []map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !mapEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// cardEqual 判断两卡是否相等（Python dataclass 的 __eq__）。
func cardEqual(a, b RootPermissionCard) bool {
	return keyEqual(a.Key, b.Key) &&
		a.ToolName == b.ToolName &&
		a.State == b.State &&
		a.AutoManual == b.AutoManual &&
		snapshotEqual(a.Request, b.Request) &&
		a.RootContext == b.RootContext
}

// keyEqual 判断两个号码牌是否相等（7 字段全部一致）。
func keyEqual(a, b *ToolInvocationKeyV1) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// cloneMap 浅拷贝映射。
func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

// stringSetEqual 比较两个字符串集合。
func stringSetEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

// errIsRootQueue 判断错误是否为队列错误（供上层分类）。
func errIsRootQueue(err error, code string) bool {
	var queueErr *RootPermissionQueueError
	if !errors.As(err, &queueErr) {
		return false
	}
	return queueErr.Code == code
}
