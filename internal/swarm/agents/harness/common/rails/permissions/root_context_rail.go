// 在 OpenJiuwen 回调内绑定紧凑的 root 权限上下文。
//
// 该 rail 是"把背景摆到桌上给引擎看"的那一层：每次工具调用前，从回调 extra
// 里还原 RootDecisionContext 与 PermissionContext，绑定到各自的 context key；
// 工具调用结束后成对清理。恢复时优先采用卡里冻结的上下文（那才是用户看到
// 卡片时的背景），而不是重新推导。
//
// 同时它也是 ask_user 反问续接的接线点：
//   - 工具卡住时，把续接快照写进中断请求的 metadata（after_tool_call）；
//   - 用户答案回来后，把问答对还原成澄清并追加到意图窗口（before_tool_call）。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/root_context_rail.py（403 行）
package permissions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sysoperation "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RootContextRail 绑定/重置一个 root 上下文；续接身份由 Core 持有。
//
// 优先级 99（在 queue rail 之后、权限 rail 之前）：上下文必须先就位，
// 权限判定才有背景可读。
//
// Python: class RootContextRail(DeepAgentRail)
type RootContextRail struct {
	// rootPermissionQueue 关联的权限队列
	rootPermissionQueue *RootPermissionQueue
	// sysOperation 构造期注入的系统操作，可为 nil
	sysOperation any
	// commandSysOperation 命令执行用的系统操作，可为 nil
	commandSysOperation any
	// sandboxed 沙箱标志；nil 表示按 sys operation 的 mode 推导
	sandboxed *bool
}

// rootContextTokens 一次绑定产生的全部 token，用于成对清理。
//
// Go 的 context 不可变，token 用派生 context 承载，清理即丢弃。
//
// Python: @dataclass(frozen=True, slots=True) _Tokens
type rootContextTokens struct {
	// context 外层 context
	context context.Context
	// failure 失败原因
	failure string
	// channelID 渠道 ID
	channelID string
	// requestID 宿主请求 ID
	requestID string
	// owner 权限上下文
	owner *sschema.PermissionContext
	// rootSessionID 根会话 ID
	rootSessionID string
	// rootRequestID 根请求 ID
	rootRequestID string
	// commandBound 是否绑定了命令执行
	commandBound bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// RootContextRailPriority 上下文 rail 优先级
	// Python: ROOT_CONTEXT_RAIL_PRIORITY
	RootContextRailPriority = 99

	// RootContextFailureAttribute 上下文失败原因在回调上下文属性上的键
	// Python: ROOT_CONTEXT_FAILURE_ATTRIBUTE
	RootContextFailureAttribute = "_jiuwenswarm_root_context_failure_v1"

	// RootPermissionOwnerKey 权限 owner 在 extra 中的键
	// Python: ROOT_PERMISSION_OWNER_KEY
	RootPermissionOwnerKey = "jiuwenswarm.root_permission_owner.v1"

	// rootContextTokensAttribute token 集合在回调上下文属性上的键
	// Python: _TOKENS_ATTRIBUTE
	rootContextTokensAttribute = "_jiuwenswarm_root_context_tokens_v1"

	// rootContextFailureMissing 缺上下文
	// Python: _FAILURE_REASONS 的 root_context_missing
	rootContextFailureMissing = "root_context_missing"

	// rootContextFailureInvalid 上下文非法
	rootContextFailureInvalid = "root_context_invalid"

	// rootContextFailureSessionMismatch 会话不匹配
	rootContextFailureSessionMismatch = "root_context_session_mismatch"

	// rootContextFailureCommandUnavailable 命令执行不可用
	rootContextFailureCommandUnavailable = "root_context_command_unavailable"

	// rootContextFailureResumeInvalid 续接非法
	rootContextFailureResumeInvalid = "root_context_resume_invalid"

	// rootContextFailureAskAnswerInvalid 反问答案非法
	rootContextFailureAskAnswerInvalid = "root_context_ask_answer_invalid"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// rootContextFailureReasons 合法的失败原因集合。
// Python: _FAILURE_REASONS
var rootContextFailureReasons = map[string]struct{}{
	rootContextFailureInvalid:            {},
	rootContextFailureMissing:            {},
	rootContextFailureSessionMismatch:    {},
	rootContextFailureCommandUnavailable: {},
	rootContextFailureResumeInvalid:      {},
	rootContextFailureAskAnswerInvalid:   {},
}

// rootPermissionOwnerKeys 权限 owner 映射的键集合。
// Python: _owner_from_extra 的 expected
var rootPermissionOwnerKeys = map[string]struct{}{
	"principal_user_id":     {},
	"triggering_user_id":    {},
	"group_digital_avatar":  {},
	"enable_memory":         {},
	"avatar_principal_name": {},
	"avatar_mode":           {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRootContextRail 创建上下文 rail。
//
// sysOperation 与 sandboxed 对应 Python 的构造注入参数：
// sysOperation 可为 nil（回退到 commandSysOperation）；sandboxed 为 nil 时
// 按 sys operation 的 mode 是否为沙箱推导。
//
// Python: RootContextRail(*, root_permission_queue, sys_operation, sandboxed)
func NewRootContextRail(
	rootPermissionQueue *RootPermissionQueue,
	sysOperation any,
	commandSysOperation any,
	sandboxed *bool,
) (*RootContextRail, error) {
	if rootPermissionQueue == nil {
		return nil, errors.New("root permission queue is required")
	}
	return &RootContextRail{
		rootPermissionQueue: rootPermissionQueue,
		sysOperation:        sysOperation,
		commandSysOperation: commandSysOperation,
		sandboxed:           sandboxed,
	}, nil
}

// PutPermissionOwnerInInputs 把权限 owner 写入 inputs 的 run.context.extra。
//
// owner 为 nil 时只移除旧值。
//
// Python: put_permission_owner_in_inputs(inputs, owner)
func PutPermissionOwnerInInputs(
	inputs map[string]any,
	owner *sschema.PermissionContext,
) map[string]any {
	result := copyMap(inputs)
	run := nestedCopyMap(result, "run")
	contextMap := nestedCopyMap(run, "context")
	extra := nestedCopyMap(contextMap, "extra")

	delete(extra, RootPermissionOwnerKey)
	if owner != nil {
		extra[RootPermissionOwnerKey] = map[string]any{
			"principal_user_id":     owner.PrincipalUserID,
			"triggering_user_id":    owner.TriggeringUserID,
			"group_digital_avatar":  owner.GroupDigitalAvatar,
			"enable_memory":         owner.EnableMemory,
			"avatar_principal_name": owner.AvatarPrincipalName,
			"avatar_mode":           owner.AvatarMode,
		}
	}

	contextMap["extra"] = extra
	run["context"] = contextMap
	result["run"] = run
	return result
}

// RootContextFailureFromContext 取上下文失败原因，非合法值返回空串。
//
// Python: root_context_failure_from_context(ctx)
func RootContextFailureFromContext(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil {
		return ""
	}
	raw, ok := cbc.GetExtra(RootContextFailureAttribute)
	if !ok {
		return ""
	}
	reason, ok := raw.(string)
	if !ok {
		return ""
	}
	if _, valid := rootContextFailureReasons[reason]; !valid {
		return ""
	}
	return reason
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fromCallback 从回调 extra 里还原上下文与 owner。
//
// 返回 (上下文, owner, 失败原因)。失败原因非空时前两项均为 nil。
//
// Python: RootContextRail._from_callback(ctx)
func (r *RootContextRail) fromCallback(
	cbc *agentinterfaces.AgentCallbackContext,
) (*RootDecisionContext, *sschema.PermissionContext, string) {
	extra := callbackExtra(cbc)

	decisionContext, err := RootDecisionContextFromExtra(extra)
	if err != nil {
		if extra == nil {
			return nil, nil, rootContextFailureMissing
		}
		return nil, nil, rootContextFailureInvalid
	}

	owner, err := ownerFromExtra(extra, decisionContext.ChannelID)
	if err != nil {
		return nil, nil, rootContextFailureInvalid
	}

	sessionID := callbackSessionID(cbc)
	if decisionContext.SessionID != sessionID {
		return nil, nil, rootContextFailureSessionMismatch
	}
	if r.resolvedSysOperation() == nil {
		return nil, nil, rootContextFailureCommandUnavailable
	}
	return decisionContext, owner, ""
}

// resolvedSysOperation 返回实际可用的系统操作。
// Python: RootContextRail._resolved_sys_operation()
func (r *RootContextRail) resolvedSysOperation() any {
	if r.sysOperation != nil {
		return r.sysOperation
	}
	return r.commandSysOperation
}

// bind 绑定全部 context key，返回 token 集合。
//
// Python: RootContextRail._bind(context, owner, *, reason)
func (r *RootContextRail) bind(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	decisionContext *RootDecisionContext,
	owner *sschema.PermissionContext,
	reason string,
) (context.Context, *rootContextTokens) {
	tokens := &rootContextTokens{failure: reason}

	// 1. 决策上下文
	ctx, _ = BindRootDecisionContext(ctx, decisionContext)
	if decisionContext != nil {
		tokens.channelID = decisionContext.ChannelID
		tokens.requestID = decisionContext.RequestID
		tokens.rootSessionID = decisionContext.SessionID
		tokens.rootRequestID = decisionContext.RequestID
	}

	// 2. 失败原因写在回调上下文（属性级，不跨请求继承）
	if cbc != nil {
		if reason != "" {
			cbc.SetExtra(RootContextFailureAttribute, reason)
		} else {
			cbc.DeleteExtra(RootContextFailureAttribute)
		}
	}

	// 3. 渠道 / 请求 ID / owner
	ctx = sschema.WithToolPermissionChannelID(ctx, tokens.channelID)
	ctx = sschema.WithToolPermissionRequestID(ctx, tokens.requestID)
	if decisionContext != nil {
		ctx = sschema.WithPermissionContextValue(ctx, owner)
	} else {
		ctx = sschema.WithPermissionContextValue(ctx, nil)
	}

	// 4. root 权限请求绑定（三态：未绑定 / 明确禁用 / 已绑定）
	enabled := decisionContext != nil
	if decisionContext == nil {
		ctx, _, _ = BindRootPermissionRequest(ctx, "", "", false, nil)
	} else {
		ctx, _, _ = BindRootPermissionRequest(
			ctx, decisionContext.SessionID, decisionContext.RequestID, true, r.rootPermissionQueue,
		)
	}
	tokens.context = ctx

	// 5. 命令执行绑定
	operation := r.resolvedSysOperation()
	if decisionContext != nil && operation != nil {
		ctx, _, _ = tools.BindCommandExecution(ctx, operation, r.resolveSandboxed(operation))
		tokens.commandBound = true
	} else {
		ctx, _ = tools.BindNoCommandExecution(ctx)
	}
	tokens.context = ctx
	_ = enabled
	return ctx, tokens
}

// replace 只更新 context key，不产生 token（供 before_tool_call 的中途替换）。
//
// Python: RootContextRail._replace(context, owner, *, reason=None)
func (r *RootContextRail) replace(
	ctx context.Context,
	decisionContext *RootDecisionContext,
	owner *sschema.PermissionContext,
	reason string,
) context.Context {
	ctx, _ = BindRootDecisionContext(ctx, decisionContext)

	channelID, requestID := "", ""
	if decisionContext != nil {
		channelID = decisionContext.ChannelID
		requestID = decisionContext.RequestID
	}
	ctx = sschema.WithToolPermissionChannelID(ctx, channelID)
	ctx = sschema.WithToolPermissionRequestID(ctx, requestID)
	if decisionContext != nil {
		ctx = sschema.WithPermissionContextValue(ctx, owner)
		ctx, _, _ = BindRootPermissionRequest(
			ctx, decisionContext.SessionID, decisionContext.RequestID, true, r.rootPermissionQueue,
		)
	} else {
		ctx = sschema.WithPermissionContextValue(ctx, nil)
		ctx, _, _ = BindRootPermissionRequest(ctx, "", "", false, nil)
	}
	_ = reason
	return ctx
}

// resolveSandboxed 决定 sandboxed 标志。
//
// 构造期显式给出时以其为准；否则看 sys operation 的 mode 是否为沙箱。
//
// Python: self._sandboxed if self._sandboxed is not None else getattr(operation, "mode", None) == OperationMode.SANDBOX
//
// Go 侧 sys operation 的 mode 在 Card().Mode 上（OperationMode 是 int 枚举），
// 不是 SysOperation 自身的方法，故走 Card() 取。operation 为 any 是因为
// 本 rail 位于 swarm 层，sys operation 实现可能在 agentcore 或沙箱 provider 中
// （Python 侧同为 Any）。
func (r *RootContextRail) resolveSandboxed(operation any) bool {
	if r.sandboxed != nil {
		return *r.sandboxed
	}
	type cardProvider interface {
		Card() *sysoperation.SysOperationCard
	}
	if provider, ok := operation.(cardProvider); ok {
		card := provider.Card()
		if card != nil {
			return card.Mode == sysoperation.OperationModeSandbox
		}
	}
	return false
}

// popAskResume 从回调 extra 取出并删除反问续接 DTO。
//
// Python: RootContextRail._pop_ask_resume(ctx)
func popAskResume(cbc *agentinterfaces.AgentCallbackContext) *RootAskUserResume {
	extra := callbackExtra(cbc)
	if extra == nil {
		return nil
	}
	raw, ok := extra[AskUserResumeDTOKey]
	if !ok {
		return nil
	}
	delete(extra, AskUserResumeDTOKey)
	prepared, ok := raw.(*RootAskUserResume)
	if !ok {
		return nil
	}
	return prepared
}

// discardAskResume 丢弃回调 extra 里的反问续接 DTO。
//
// Python: RootContextRail._discard_ask_resume(ctx)
func discardAskResume(cbc *agentinterfaces.AgentCallbackContext) {
	extra := callbackExtra(cbc)
	if extra == nil {
		return
	}
	delete(extra, AskUserResumeDTOKey)
}

// ownerFromExtra 从 extra 还原权限上下文。
//
// 键集合必须完全一致，布尔与文本字段类型必须正确，否则视为非法。
//
// Python: _owner_from_extra(extra, channel_id)
func ownerFromExtra(extra map[string]any, channelID string) (*sschema.PermissionContext, error) {
	if extra == nil {
		return nil, nil
	}
	raw, exists := extra[RootPermissionOwnerKey]
	if !exists || raw == nil {
		return nil, nil
	}
	asMap, ok := raw.(map[string]any)
	if !ok || len(asMap) != len(rootPermissionOwnerKeys) {
		return nil, errors.New("invalid permission owner")
	}
	for key := range asMap {
		if _, valid := rootPermissionOwnerKeys[key]; !valid {
			return nil, errors.New("invalid permission owner")
		}
	}

	for _, name := range []string{"group_digital_avatar", "enable_memory", "avatar_mode"} {
		if _, ok := asMap[name].(bool); !ok {
			return nil, errors.New("invalid permission owner")
		}
	}
	for _, name := range []string{"principal_user_id", "triggering_user_id", "avatar_principal_name"} {
		if _, ok := asMap[name].(string); !ok {
			return nil, errors.New("invalid permission owner")
		}
	}

	return &sschema.PermissionContext{
		ChannelID:           channelID,
		PrincipalUserID:     asMap["principal_user_id"].(string),
		TriggeringUserID:    asMap["triggering_user_id"].(string),
		GroupDigitalAvatar:  asMap["group_digital_avatar"].(bool),
		EnableMemory:        asMap["enable_memory"].(bool),
		AvatarPrincipalName: asMap["avatar_principal_name"].(string),
		AvatarMode:          asMap["avatar_mode"].(bool),
	}, nil
}

// ownerFromCallback 从回调 extra 还原权限上下文（渠道取当前上下文）。
//
// Python: _owner_from_callback(ctx)
func ownerFromCallback(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) (*sschema.PermissionContext, error) {
	extra := callbackExtra(cbc)
	channelID := ""
	if decisionContext := CurrentRootDecisionContext(ctx); decisionContext != nil {
		channelID = decisionContext.ChannelID
	}
	return ownerFromExtra(extra, channelID)
}

// callbackExtra 取回调的 run extra 映射。
//
// Python: _callback_extra(ctx)
func callbackExtra(cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	return runExtraOf(cbc)
}

// callbackSessionID 取回调的会话 ID。
//
// Python: _session_id(ctx)
func callbackSessionID(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil || cbc.Session() == nil {
		return ""
	}
	return strings.TrimSpace(cbc.Session().GetSessionID())
}

// skipTool 把工具标记为跳过并写入失败结果。
//
// Python: _skip_tool(ctx, reason)
func skipTool(cbc *agentinterfaces.AgentCallbackContext, reason string) {
	if cbc == nil {
		return
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return
	}

	callID := ""
	if inputs.ToolCall != nil {
		callID = inputs.ToolCall.ID
	}
	message := fmt.Sprintf("[PERMISSION_BLOCKED] %s", reason)

	inputs.ToolResult = map[string]any{
		"success":           false,
		"status":            "blocked",
		"is_error":          true,
		"error":             message,
		"permission_status": "blocked",
		"blocked_reason":    reason,
	}
	inputs.ToolMsg = llmschema.NewToolMessage(callID, message)
	cbc.SetExtra("_skip_tool", true)
	cbc.SetExtra(RootContextFailureAttribute, reason)
}

// ──────────────────────────── 导出函数（rail 钩子） ────────────────────────────

// Priority 返回优先级 99。
// Python: RootContextRail.priority = ROOT_CONTEXT_RAIL_PRIORITY
func (r *RootContextRail) Priority() int { return RootContextRailPriority }

// BeforeInvoke invoke 开始前绑定生命周期上下文。
// Python: RootContextRail.before_invoke(ctx)
func (r *RootContextRail) BeforeInvoke(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	return r.bindLifecycle(ctx, cbc)
}

// AfterInvoke invoke 完成后丢弃反问续接并重置生命周期。
// Python: RootContextRail.after_invoke(ctx)
func (r *RootContextRail) AfterInvoke(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	discardAskResume(cbc)
	r.resetLifecycle(cbc)
	return nil
}

// BeforeTaskIteration 迭代开始前绑定生命周期上下文。
// Python: RootContextRail.before_task_iteration(ctx)
func (r *RootContextRail) BeforeTaskIteration(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	return r.bindLifecycle(ctx, cbc)
}

// AfterTaskIteration 迭代完成后重置生命周期。
// Python: RootContextRail.after_task_iteration(ctx)
func (r *RootContextRail) AfterTaskIteration(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	r.resetLifecycle(cbc)
	return nil
}

// BeforeToolCall 工具调用前绑定/校正上下文。
//
// 三分支（对齐 Python）：
//  1. 有权限续接 → 采用卡里冻结的上下文（用户看到卡片时的背景）；
//  2. 是 ask_user 且有反问续接 DTO → 还原澄清并追加意图窗口；
//  3. 其余 → 从回调 extra 还原上下文。
//
// 任一分支失败都置失败原因并跳过该工具（fail-closed，不放行）。
//
// Python: RootContextRail.before_tool_call(ctx)
func (r *RootContextRail) BeforeToolCall(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return nil
	}

	decisionContext, owner, reason := r.fromCallback(cbc)
	ctx = r.replace(ctx, nilIfReason(decisionContext, reason), nilIfReasonOwner(owner, reason), reason)

	if resume := RootPermissionResumeFromContext(cbc); resume != nil {
		retained := resume.Card.RootContext
		switch {
		case retained == nil:
			reason = rootContextFailureResumeInvalid
		case retained.SessionID != callbackSessionID(cbc):
			reason = rootContextFailureSessionMismatch
		default:
			decisionContext = retained
			reason = ""
			ctx = r.replace(ctx, decisionContext, owner, "")
		}
	} else if toolNameOfInputs(inputs) == AskUserToolName {
		if prepared := popAskResume(cbc); prepared != nil {
			if prepared.Continuation.ToolCallID != toolCallIDOfInputs(inputs) ||
				prepared.Continuation.Context.SessionID != callbackSessionID(cbc) {
				reason = rootContextFailureAskAnswerInvalid
			} else {
				decisionContext = ApplyAskUserResume(prepared)
				extra := callbackExtra(cbc)
				if extra != nil {
					extra[RootContextKey] = decisionContext.ToMapping()
				}
				reason = ""
				ctx = r.replace(ctx, decisionContext, owner, "")
			}
		}
	} else {
		discardAskResume(cbc)
	}

	if reason != "" || decisionContext == nil {
		if reason == "" {
			reason = rootContextFailureMissing
		}
		ctx = r.replace(ctx, nil, nil, reason)
		skipTool(cbc, reason)
	}
	return nil
}

// AfterToolCall 工具调用后：ask_user 卡住时把续接快照写进中断请求 metadata。
//
// Python: RootContextRail.after_tool_call(ctx)
func (r *RootContextRail) AfterToolCall(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	interrupt := toolInterruptOf(cbc.Exception())
	if interrupt == nil || interrupt.Request == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return nil
	}
	decisionContext := CurrentRootDecisionContext(ctx)
	if decisionContext == nil {
		return nil
	}

	var toolArgs any = inputs.ToolArgs
	snapshot := BuildAskUserMetadata(
		decisionContext,
		toolNameOfInputs(inputs),
		toolCallIDOfInputs(inputs),
		toolArgs,
	)
	if snapshot == nil {
		return nil
	}
	metadata := interrupt.Request.GetMetadata()
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[AskUserContinuationMetadataKey] = snapshot
	interrupt.Request.SetMetadata(metadata)
	return nil
}

// ──────────────────────────── 非导出函数（钩子辅助） ────────────────────────────

// bindLifecycle 幂等绑定生命周期上下文（已有 token 时不重复绑定）。
//
// Python: RootContextRail._bind_lifecycle(ctx)
func (r *RootContextRail) bindLifecycle(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	if cbc == nil {
		return nil
	}
	if _, bound := cbc.GetExtra(rootContextTokensAttribute); bound {
		return nil
	}

	decisionContext, owner, reason := r.fromCallback(cbc)
	_, tokens := r.bind(ctx, cbc, nilIfReason(decisionContext, reason), nilIfReasonOwner(owner, reason), reason)
	cbc.SetExtra(rootContextTokensAttribute, tokens)
	return nil
}

// resetLifecycle 成对清理生命周期上下文。
//
// Go 的 context 不可变，此处只需清除回调上的 token 记录与失败原因。
//
// Python: RootContextRail._reset_lifecycle(ctx)
func (r *RootContextRail) resetLifecycle(cbc *agentinterfaces.AgentCallbackContext) {
	if cbc == nil {
		return
	}
	if _, bound := cbc.GetExtra(rootContextTokensAttribute); !bound {
		return
	}
	cbc.DeleteExtra(rootContextTokensAttribute)
	cbc.DeleteExtra(RootContextFailureAttribute)
}

// nilIfReason 失败时返回 nil 上下文。
func nilIfReason(decisionContext *RootDecisionContext, reason string) *RootDecisionContext {
	if reason != "" {
		return nil
	}
	return decisionContext
}

// nilIfReasonOwner 失败时返回 nil owner。
func nilIfReasonOwner(owner *sschema.PermissionContext, reason string) *sschema.PermissionContext {
	if reason != "" {
		return nil
	}
	return owner
}

// toolNameOfInputs 取工具名（优先 ToolCall.Name）。
func toolNameOfInputs(inputs *agentinterfaces.ToolCallInputs) string {
	if inputs == nil {
		return ""
	}
	if inputs.ToolCall != nil && inputs.ToolCall.Name != "" {
		return strings.TrimSpace(inputs.ToolCall.Name)
	}
	return strings.TrimSpace(inputs.ToolName)
}

// toolCallIDOfInputs 取工具调用 ID。
func toolCallIDOfInputs(inputs *agentinterfaces.ToolCallInputs) string {
	if inputs == nil || inputs.ToolCall == nil {
		return ""
	}
	return strings.TrimSpace(inputs.ToolCall.ID)
}
