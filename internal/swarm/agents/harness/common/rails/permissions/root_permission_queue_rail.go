// Root 工具身份 rail 与缺失兄弟卡的权限闩锁。
// 该 rail 站在工具执行入口，负责：
//  1. 给本次工具调用发号码牌（ToolInvocationKeyV1）并绑定到回调上下文；
//  2. 首次执行时把卡置为 active；若引擎随后抛权限中断，则冻结成 pending；
//  3. 恢复时认领答案（resuming → active），把答案与冻结上下文交给后续 rail；
//  4. 缺失兄弟卡时闩锁：待决卡还在队列里却没被 Core 一并抛出，就报错而非放行。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/root_permission_queue_rail.py（536 行）
package permissions

import (
	"context"
	"errors"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RootPermissionRequestBinding 由 Host 适配器边界选定的可信 root 身份。
// Python: @dataclass(frozen=True, slots=True) RootPermissionRequestBinding
type RootPermissionRequestBinding struct {
	// RootSessionID 根会话标识
	RootSessionID string
	// RequestID 宿主请求标识
	RequestID string
	// Queue 本次请求使用的队列，nil 表示明确禁用
	Queue *RootPermissionQueue
}

// RootPermissionResume 在 Auto/Base 权限消费输入之前绑定的精确待决卡状态。
// Python: @dataclass(frozen=True, slots=True) RootPermissionResume
type RootPermissionResume struct {
	// Card 待决卡
	Card RootPermissionCard
	// UserInput 本次恢复的用户输入
	UserInput any
}

// RootNonPermissionResume 在权限生命周期之外被准入的精确 Core 续接。
// Python: @dataclass(frozen=True, slots=True) RootNonPermissionResume
type RootNonPermissionResume struct {
	// RootSessionID 根会话标识
	RootSessionID string
	// ToolCallID 工具调用标识
	ToolCallID string
	// ToolName 工具名
	ToolName string
}

// RootPermissionWrapperResume 指向真实 Core 中断项的请求级一次性引用。
// Python: @dataclass(slots=True) RootPermissionWrapperResume
type RootPermissionWrapperResume struct {
	// ToolCall 对应的工具调用
	ToolCall any
	// Card 待决卡
	Card RootPermissionCard
	// Consumed 是否已消费
	Consumed bool
}

// rootPermissionRequestBindingHolder 承载绑定三态。
// Python 用单个哨兵 _UNBOUND 表示"从未绑定"，与"绑定 None"区分。
// Go 无单值多态，用 bound 标志 + value 双字段表达同一语义。
type rootPermissionRequestBindingHolder struct {
	// bound 是否已绑定（true 时 value 可为 nil，表示明确禁用）
	bound bool
	// value 绑定值
	value *RootPermissionRequestBinding
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// RootPermissionQueuePriority 权限队列 rail 优先级
	// Python: ROOT_PERMISSION_QUEUE_PRIORITY
	RootPermissionQueuePriority = 100

	// RootPermissionCompletionPriority 权限完成 rail 优先级
	// Python: ROOT_PERMISSION_COMPLETION_PRIORITY
	RootPermissionCompletionPriority = 89

	// RootPermissionQueueUserInputKey 恢复输入在 extra 中的键
	// Python: ROOT_PERMISSION_QUEUE_USER_INPUT_KEY
	RootPermissionQueueUserInputKey = "_jiuwenswarm_root_permission_resume_v1"

	// RootNonPermissionResumeDTOKey 非权限续接 DTO 在 extra 中的键
	// Python: ROOT_NON_PERMISSION_RESUME_DTO_KEY
	RootNonPermissionResumeDTOKey = "jiuwenswarm.root_non_permission_resume.v1"

	// ToolInvocationContextAttribute 号码牌在回调上下文属性上的键
	// Python: TOOL_INVOCATION_CONTEXT_ATTRIBUTE
	ToolInvocationContextAttribute = "_jiuwenswarm_tool_invocation_key_v1"

	// RootPermissionResumeAttribute 权限续接在回调上下文属性上的键
	// Python: ROOT_PERMISSION_RESUME_ATTRIBUTE
	RootPermissionResumeAttribute = "_jiuwenswarm_root_permission_resume_v1"

	// RootNonPermissionResumeAttribute 非权限续接在回调上下文属性上的键
	// Python: ROOT_NON_PERMISSION_RESUME_ATTRIBUTE
	RootNonPermissionResumeAttribute = "_jiuwenswarm_root_non_permission_resume_v1"

	// RootPermissionWrappersKey 权限包装续接列表在 extra 中的键
	// Python: ROOT_PERMISSION_WRAPPERS_KEY
	RootPermissionWrappersKey = "jiuwenswarm.root_permission_wrappers.v1"

	// RootPermissionWrapperAttribute 包装续接标记在回调上下文属性上的键
	// Python: ROOT_PERMISSION_WRAPPER_ATTRIBUTE
	RootPermissionWrapperAttribute = "_jiuwenswarm_permission_wrapper_resume"

	// PermissionInterruptRequestAttribute 权限中断请求在回调上下文属性上的键
	// Python: PERMISSION_INTERRUPT_REQUEST_ATTRIBUTE
	PermissionInterruptRequestAttribute = "_jiuwenswarm_permission_interrupt_request_v1"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// rootPermissionBindingKey 权限请求绑定的 context key
type rootPermissionBindingKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// BindRootPermissionRequest 绑定一个 root 权限请求，返回派生 context 与清理函数。
// enabled 为 false 时绑定"明确禁用"（value 为 nil 但 bound 为 true），
// 与"从未绑定"区分开（对齐 Python 的 set(None) vs _UNBOUND）。
// Python: bind_root_permission_request(*, root_session_id, request_id, enabled, queue=None)
func BindRootPermissionRequest(
	ctx context.Context,
	rootSessionID string,
	requestID string,
	enabled bool,
	queue *RootPermissionQueue,
) (context.Context, func(), error) {
	holder := &rootPermissionRequestBindingHolder{bound: true}
	if enabled {
		normalizedRoot, err := NormalizeToolInvocationText(
			"root_session_id", rootSessionID, defaultToolInvocationTextMaximum,
		)
		if err != nil {
			return ctx, func() {}, err
		}
		normalizedRequest, err := NormalizeToolInvocationText(
			"request_id", requestID, defaultToolInvocationTextMaximum,
		)
		if err != nil {
			return ctx, func() {}, err
		}
		holder.value = &RootPermissionRequestBinding{
			RootSessionID: normalizedRoot,
			RequestID:     normalizedRequest,
			Queue:         queue,
		}
	}
	return context.WithValue(ctx, rootPermissionBindingKey{}, holder), func() {}, nil
}

// ResetRootPermissionRequest 恢复绑定前的状态。
// Go 的 context 不可变，丢弃派生 ctx 即等价于 Python 的 reset(token)。
// Python: reset_root_permission_request(token)
func ResetRootPermissionRequest(context.Context) {}

// CurrentRootPermissionQueue 返回当前绑定的队列。
// 未绑定时报错 root_permission_request_unbound；绑定明确禁用时返回 nil。
// Python: current_root_permission_queue()
func CurrentRootPermissionQueue(ctx context.Context) (*RootPermissionQueue, error) {
	holder, ok := ctx.Value(rootPermissionBindingKey{}).(*rootPermissionRequestBindingHolder)
	if !ok || !holder.bound {
		return nil, &RootPermissionQueueError{"root_permission_request_unbound"}
	}
	if holder.value == nil {
		return nil, nil
	}
	return holder.value.Queue, nil
}

// OptionalRootPermissionQueue 只在当前请求安装了队列时返回它，否则返回 nil。
// Python: optional_root_permission_queue()
func OptionalRootPermissionQueue(ctx context.Context) *RootPermissionQueue {
	holder, ok := ctx.Value(rootPermissionBindingKey{}).(*rootPermissionRequestBindingHolder)
	if !ok || !holder.bound || holder.value == nil {
		return nil
	}
	return holder.value.Queue
}

// ToolInvocationKeyFromContext 从回调上下文取得本次调用的号码牌。
// Python: tool_invocation_key_from_context(ctx)
func ToolInvocationKeyFromContext(cbc *agentinterfaces.AgentCallbackContext) *ToolInvocationKeyV1 {
	if cbc == nil {
		return nil
	}
	raw, ok := cbc.GetExtra(ToolInvocationContextAttribute)
	if !ok {
		return nil
	}
	key, ok := raw.(*ToolInvocationKeyV1)
	if !ok {
		return nil
	}
	return key
}

// RootPermissionResumeFromContext 从回调上下文取得权限续接状态。
// Python: root_permission_resume_from_context(ctx)
func RootPermissionResumeFromContext(cbc *agentinterfaces.AgentCallbackContext) *RootPermissionResume {
	if cbc == nil {
		return nil
	}
	raw, ok := cbc.GetExtra(RootPermissionResumeAttribute)
	if !ok {
		return nil
	}
	resume, ok := raw.(*RootPermissionResume)
	if !ok {
		return nil
	}
	return resume
}

// RootNonPermissionResumeFromContext 从回调上下文取得非权限续接状态。
// Python: root_nonpermission_resume_from_context(ctx)
func RootNonPermissionResumeFromContext(cbc *agentinterfaces.AgentCallbackContext) *RootNonPermissionResume {
	if cbc == nil {
		return nil
	}
	raw, ok := cbc.GetExtra(RootNonPermissionResumeAttribute)
	if !ok {
		return nil
	}
	resume, ok := raw.(*RootNonPermissionResume)
	if !ok {
		return nil
	}
	return resume
}

// PutRootNonPermissionResumeInInputs 把一个 Host 校验过的回调标记写入共享 RunContext 状态。
// 路径与 Python 一致：inputs["run"]["context"]["extra"]。
// Python: put_root_nonpermission_resume_in_inputs(inputs, prepared, *, wrappers=())
func PutRootNonPermissionResumeInInputs(
	inputs map[string]any,
	prepared *RootNonPermissionResume,
	wrappers []*RootPermissionWrapperResume,
) map[string]any {
	result := copyMap(inputs)
	run := nestedCopyMap(result, "run")
	ctxMap := nestedCopyMap(run, "context")
	extra := nestedCopyMap(ctxMap, "extra")

	delete(extra, RootNonPermissionResumeDTOKey)
	if prepared != nil {
		extra[RootNonPermissionResumeDTOKey] = prepared
	}
	delete(extra, RootPermissionWrappersKey)
	if len(wrappers) > 0 {
		extra[RootPermissionWrappersKey] = wrappers
	}

	ctxMap["extra"] = extra
	run["context"] = ctxMap
	result["run"] = run
	return result
}

// ConsumePermissionWrapperResume 只消费本次回调上的标记，不消费继承来的 extra 标志。
// Python: consume_permission_wrapper_resume(ctx)
func ConsumePermissionWrapperResume(cbc *agentinterfaces.AgentCallbackContext) bool {
	if cbc == nil {
		return false
	}
	raw, ok := cbc.GetExtra(RootPermissionWrapperAttribute)
	if !ok {
		return false
	}
	cbc.DeleteExtra(RootPermissionWrapperAttribute)

	marker, ok := raw.(*RootPermissionWrapperResume)
	if !ok {
		return false
	}
	return marker.Consumed
}

// MarkPermissionInterruptRequest 在回调上下文上标记本次权限中断请求。
// 已标记过不同的请求时报错（一个回调只能对应一个权限中断）。
// Python: mark_permission_interrupt_request(ctx, request)
func MarkPermissionInterruptRequest(
	cbc *agentinterfaces.AgentCallbackContext,
	request *saschema.InterruptRequest,
) error {
	if cbc == nil {
		return &RootPermissionQueueError{"permission_interrupt_request_mismatch"}
	}
	if raw, ok := cbc.GetExtra(PermissionInterruptRequestAttribute); ok {
		if existing, isRequest := raw.(*saschema.InterruptRequest); !isRequest || existing != request {
			return &RootPermissionQueueError{"permission_interrupt_request_mismatch"}
		}
	}
	cbc.SetExtra(PermissionInterruptRequestAttribute, request)
	return nil
}

// IsMarkedPermissionInterrupt 判断异常携带的中断是否就是本回调标记过的权限中断。
// Python: is_marked_permission_interrupt(ctx, exc)
func IsMarkedPermissionInterrupt(cbc *agentinterfaces.AgentCallbackContext, exc error) bool {
	interrupt := toolInterruptOf(exc)
	if interrupt == nil || cbc == nil {
		return false
	}
	raw, ok := cbc.GetExtra(PermissionInterruptRequestAttribute)
	if !ok {
		return false
	}
	marked, isRequest := raw.(*saschema.InterruptRequest)
	if !isRequest {
		return false
	}
	return marked == interrupt.Request
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolInterruptOf 从异常链里取出 ToolInterruptException。
// 直接是 ToolInterruptException 时返回它；AbortError 时看 cause。
// Python: _tool_interrupt(exc)
func toolInterruptOf(exc error) *saschema.ToolInterruptException {
	if exc == nil {
		return nil
	}
	var interrupt *saschema.ToolInterruptException
	if errors.As(exc, &interrupt) {
		return interrupt
	}
	return nil
}

// attachKey 把号码牌写进中断请求的 metadata。
// Python: _attach_key(request, key)
func attachKey(request *saschema.InterruptRequest, key *ToolInvocationKeyV1) error {
	if request == nil || key == nil {
		return &RootPermissionQueueError{"permission_interrupt_metadata_invalid"}
	}
	if request.Metadata == nil {
		request.Metadata = make(map[string]any)
	}
	request.Metadata["tool_invocation_key"] = key.ToWire()
	return nil
}

// sessionIDOf 取回调上下文里的执行会话 ID。
// Python: _session_id(ctx)
func sessionIDOf(cbc *agentinterfaces.AgentCallbackContext) (string, error) {
	if cbc == nil || cbc.Session() == nil {
		return "", &RootPermissionQueueError{"permission_queue_execution_session_id_invalid"}
	}
	return NormalizeToolInvocationText(
		"execution_session_id", cbc.Session().GetSessionID(), defaultToolInvocationTextMaximum,
	)
}

// toolCallIDOf 取工具调用 ID。
// Python: _tool_call_id(inputs)
func toolCallIDOf(inputs *agentinterfaces.ToolCallInputs) (string, error) {
	if inputs == nil || inputs.ToolCall == nil {
		return "", &RootPermissionQueueError{"permission_queue_tool_call_id_invalid"}
	}
	return NormalizeToolInvocationText(
		"tool_call_id", inputs.ToolCall.ID, defaultToolInvocationTextMaximum,
	)
}

// toolNameOf 取工具名，优先 inputs.ToolName，回退 ToolCall.Name。
// Python: _tool_name(inputs)
func toolNameOf(inputs *agentinterfaces.ToolCallInputs) (string, error) {
	if inputs == nil {
		return "", &RootPermissionQueueError{"permission_queue_tool_name_invalid"}
	}
	name := inputs.ToolName
	if name == "" && inputs.ToolCall != nil {
		name = inputs.ToolCall.Name
	}
	return NormalizeToolInvocationText("tool_name", name, defaultToolInvocationTextMaximum)
}

// resumeInputOf 返回本次恢复给该调用的答案。
// 优先取本次新鲜归属的答案（跳过 carried），其次读原始恢复输入的两种键。
// 未找到时返回 (nil, false)。
// Python: _resume_input(ctx, tool_call_id)
func resumeInputOf(cbc *agentinterfaces.AgentCallbackContext, toolCallID string) (any, bool) {
	if cbc == nil {
		return nil, false
	}

	// 1. 优先读 RESUME_INTERRUPT_ANSWERS_KEY 下的新鲜答案
	if raw, ok := cbc.GetExtra(saschema.ResumeInterruptAnswersKey); ok {
		if byCall, ok := raw.(map[string]map[string]*saschema.InterruptAnswer); ok {
			if answers, ok := byCall[toolCallID]; ok {
				for _, answer := range answers {
					if answer != nil && !answer.Carried {
						return answer.Value, true
					}
				}
			}
		}
	}

	// 2. 回退读原始恢复输入
	for _, name := range []string{RootPermissionQueueUserInputKey, saschema.ResumeUserInputKey} {
		raw, ok := cbc.GetExtra(name)
		if !ok {
			continue
		}
		if value, found := userInputValueOf(raw, toolCallID); found {
			return value, true
		}
		return raw, true
	}
	return nil, false
}

// userInputValueOf 从恢复输入里按 tool_call_id 取值。
// 两种形态：InteractiveInput（按 tool_call_id 取 UserInputs）与普通映射。
// Python: isinstance(user_inputs, Mapping) / isinstance(value, Mapping) 分支
func userInputValueOf(raw any, toolCallID string) (any, bool) {
	if interactive, ok := raw.(*sessioninteraction.InteractiveInput); ok {
		value, found := interactive.UserInputs[toolCallID]
		return value, found
	}
	if asMap, ok := raw.(map[string]any); ok {
		value, found := asMap[toolCallID]
		return value, found
	}
	return nil, false
}

// runExtraOf 取 RunContext 的 extra 映射。
// 优先从 inputs.RunContext 读，回退 ctx.extra["run_context"]。
// Python: _run_extra(ctx)
func runExtraOf(cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	if cbc == nil {
		return nil
	}
	if raw, ok := cbc.GetExtra("run_context"); ok {
		if asMap, ok := raw.(map[string]any); ok {
			if extra, ok := asMap["extra"].(map[string]any); ok {
				return extra
			}
		}
	}
	return nil
}

// popNonPermissionResume 从 run extra 里取出并删除非权限续接标记。
// Python: _pop_nonpermission_resume(ctx)
func popNonPermissionResume(cbc *agentinterfaces.AgentCallbackContext) (bool, any) {
	extra := runExtraOf(cbc)
	if extra == nil {
		return false, nil
	}
	raw, ok := extra[RootNonPermissionResumeDTOKey]
	if !ok {
		return false, nil
	}
	delete(extra, RootNonPermissionResumeDTOKey)
	return true, raw
}

// abortWithInterrupt 构造中断错误，语义等价 Python 的
// raise AbortError(reason=..., cause=ToolInterruptException(...))。
// Go 侧用 callback.AbortError 承载，由工具执行层识别为非错误的中止。
func abortWithInterrupt(
	toolName string,
	request *saschema.InterruptRequest,
	toolCall *llmschema.ToolCall,
) error {
	exc := &saschema.ToolInterruptException{
		Request:  request,
		ToolCall: toolCall,
	}
	return cb.NewAbortError(
		fmt.Sprintf("Tool execution interrupted: %s", toolName), exc,
	)
}

// ──────────────────────────── 结构体（rail） ────────────────────────────

// RootPermissionQueueRail 绑定一次 root 调用，并在路由前闩锁缺失的待决兄弟卡。
// 优先级 100（在权限 rail 之前执行）：先发号码牌、认领答案，权限 rail 才能
// 带着正确的上下文做判定。
// Python: class RootPermissionQueueRail(AgentRail)
type RootPermissionQueueRail struct {
	// queue 本 rail 服务的队列
	queue *RootPermissionQueue
	// answerClaimed 答案被认领时的回调，可为 nil
	answerClaimed func(*ToolInvocationKeyV1)
}

// RootPermissionCompletionRail 在权限边界放行后立即结束队列所有权。
// 优先级 89（紧跟在权限 rail 之后）：权限判定一旦放行，卡就该从队列移除，
// 不应继续占用待决状态。
// Python: class RootPermissionCompletionRail(AgentRail)
type RootPermissionCompletionRail struct {
	// queue 本 rail 服务的队列
	queue *RootPermissionQueue
}

// ──────────────────────────── 导出函数（rail） ────────────────────────────

// NewRootPermissionQueueRail 创建权限队列 rail。
// answerClaimed 可为 nil（对齐 Python 的 answer_claimed=None）。
// Python: RootPermissionQueueRail.__init__(queue, *, answer_claimed=None)
func NewRootPermissionQueueRail(
	queue *RootPermissionQueue,
	answerClaimed func(*ToolInvocationKeyV1),
) *RootPermissionQueueRail {
	return &RootPermissionQueueRail{queue: queue, answerClaimed: answerClaimed}
}

// NewRootPermissionCompletionRail 创建权限完成 rail。
// Python: RootPermissionCompletionRail.__init__(queue)
func NewRootPermissionCompletionRail(queue *RootPermissionQueue) *RootPermissionCompletionRail {
	return &RootPermissionCompletionRail{queue: queue}
}

// ──────────────────────────── 非导出函数（rail 辅助） ────────────────────────────

// finishOnToolException 工具异常时结束该卡的队列所有权。
// Python: RootPermissionQueueRail.on_tool_exception(ctx)
func finishOnToolException(r *RootPermissionQueueRail, cbc *agentinterfaces.AgentCallbackContext) {
	key := ToolInvocationKeyFromContext(cbc)
	if key != nil {
		r.queue.Finish(key)
	}
}

// finishOnCompletion 权限边界放行后结束该卡的队列所有权。
// Python: RootPermissionCompletionRail.before_tool_call(ctx)
func finishOnCompletion(r *RootPermissionCompletionRail, cbc *agentinterfaces.AgentCallbackContext) {
	key := ToolInvocationKeyFromContext(cbc)
	if key != nil {
		r.queue.Finish(key)
	}
}

// Priority 返回权限队列 rail 优先级 100。
// Python: RootPermissionQueueRail.priority = ROOT_PERMISSION_QUEUE_PRIORITY
func (r *RootPermissionQueueRail) Priority() int { return RootPermissionQueuePriority }

// Priority 返回权限完成 rail 优先级 89。
// Python: RootPermissionCompletionRail.priority = ROOT_PERMISSION_COMPLETION_PRIORITY
func (r *RootPermissionCompletionRail) Priority() int { return RootPermissionCompletionPriority }
