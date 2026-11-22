// Package tools —— SessionMessagingRouteRail：把「本次工具调用专属」的路由
// 上下文绑定进调用级载体，并在每轮模型调用前刷新跨会话队列快照提示词。
// Python: jiuwenswarm/agents/harness/common/tools/session_messaging_toolkit.py:177-326
// SessionMessagingRouteRail（rail 与工具族同文件归属，Go 按包内一文件一职责拆分，
// 仍在 tools 包内，对应关系在 doc.go 核对）。
//
// Go 等价载体说明（Python ContextVar → Go 调用级 extra）：
// Python before_tool_call 以 ContextVar token 绑定 route/tool_call_id、after 复位；
// Go 侧 rail 把 route wire + tool_call_id 写入 ForkForToolCall 的
// AgentCallbackContext.extra（每次工具调用独立 fork，作用域等价 token），
// 工具侧经 ToolCallOptions.CallbackContext.Extra() 读取后注入 context.Context
// （见 session_messaging_toolkit.go 的 invokeFn 包装）。该等价实现已登记 spec §5。
package tools

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
)

// sessionMessagingRouteRailPriority rail 优先级。
// Python: session_messaging_toolkit.py:180 priority = 97
const sessionMessagingRouteRailPriority = 97

// sessionInputMessageRouteExtraKey 请求入口写入 cbc.extra 的路由键
// （Python _route 直读键）。Python: session_messaging_toolkit.py:287
const sessionInputMessageRouteExtraKey = "session_input_message_route"

// sessionMessagingToolCallIDExtraKey 工具调用标识经调用级 extra 载运的键。
// Python: :181-182 _CALL_TOKEN_ATTR（ContextVar token）；Go 等价载体（spec §5 登记）。
const sessionMessagingToolCallIDExtraKey = "jiuwenswarm.session_messaging_tool_call_id.v1"

// sessionMessagingRouteTokenExtraKey 路由经调用级 extra 载运的键（token 等价）。
// Python: :181 _ROUTE_TOKEN_ATTR。
const sessionMessagingRouteTokenExtraKey = "jiuwenswarm.session_messaging_route_token.v1"

// sessionToolsPromptBeforeModelCall 每轮模型调用前刷新的跨会话队列快照提示词。
// Python: session_messaging_toolkit.py:214-229（一比一复刻原文，不做翻译）。
const sessionToolsPromptBeforeModelCall = "Cross-session tasks: use session_list to find the user's previous sessions; " +
	"session_read reads a target's persisted history/results without executing it. " +
	"session_message_list(target_session_id) reads its mailbox and queue state. " +
	"When the user asks to continue pending cross-session work, identify the intended " +
	"target from the conversation and current queue state, then call " +
	"session_continue_queued(target_session_id). If the target is ambiguous, ask. " +
	"Never resend queued messages or infer that an unknown task succeeded. " +
	"A restart or a query alone does not authorize resuming work. " +
	"Continuation acceptance is not completion; query status and history afterwards. " +
	"If continuation returns finish_current_turn=true, acknowledge and finish this " +
	"turn so ordinary queued tasks can start; never poll or wait on your own session. " +
	"The following host snapshot covers outstanding messages sent or received by " +
	"this session; has_more requires further discovery with session_list. " +
	"It is refreshed before each model call.\n"

// SessionMessagingRouteRail 绑定「本次工具调用任务」所属的路由。
// Python: session_messaging_toolkit.py:177-182
type SessionMessagingRouteRail struct {
	rails.DeepAgentRail
	// service 服务面（AgentServer 装配后经 SetService 注入；nil=提示词不注入）
	// Python: :186-187 self._service
	service SessionMessagingServicePort
	// promptBuilder 模型调用前的系统提示词构建器（Init 缓存，Uninit 清理）
	// Python: :187 self._prompt_builder
	promptBuilder saprompt.SystemPromptBuilderInterface
}

// 编译时验证满足 AgentRail 接口。
var _ agentinterfaces.AgentRail = (*SessionMessagingRouteRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionMessagingRouteRail 创建路由 rail。
// Python: :184-188 __init__
func NewSessionMessagingRouteRail() *SessionMessagingRouteRail {
	r := &SessionMessagingRouteRail{}
	r.WithPriority(sessionMessagingRouteRailPriority)
	return r
}

// Init 缓存系统提示词构建器。
// Python: :189-190 init：prompt_builder = getattr(agent, "system_prompt_builder", None)
func (r *SessionMessagingRouteRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	if agent == nil {
		r.promptBuilder = nil
		return nil
	}
	r.promptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 移除提示词节并释放服务引用。
// Python: :192-196 uninit
func (r *SessionMessagingRouteRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.promptBuilder != nil {
		r.promptBuilder.RemoveSection("session_tools")
	}
	r.promptBuilder = nil
	r.service = nil
	return nil
}

// SetService 注入服务面（AgentServer Runtime 重建后刷新）。
// Python: :198-199 set_service
func (r *SessionMessagingRouteRail) SetService(service SessionMessagingServicePort) {
	r.service = service
}

// BeforeModelCall 移除旧节 → 取路由 → 拉队列快照 → 注入 session_tools 节。
// Python: :201-234 before_model_call
func (r *SessionMessagingRouteRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	builder := r.promptBuilder
	if builder == nil {
		return nil
	}
	// Python: :204 remove_section("session_tools")
	builder.RemoveSection("session_tools")
	route := routeFromCBC(cbc)
	if r.service == nil || route == nil {
		return nil
	}
	// Python: :209 pending_context(route.source_for_list())
	state, err := r.service.PendingContext(ctx, route.SourceForList())
	if err != nil {
		// Python: :210-212 except (SessionMessagingError, OSError, sqlite3.Error) → warning + 占位
		logger.Warn(logger.ComponentAgentServer).
			Err(err).
			Msg("Could not refresh cross-Session queue context")
		state = map[string]any{"state_unavailable": true}
	}
	// Python: :213-229 内容 = 固定提示词 + json.dumps(state, ensure_ascii=False)
	stateJSON, marshalErr := json.Marshal(state)
	if marshalErr != nil {
		stateJSON = []byte("{}")
	}
	content := sessionToolsPromptBeforeModelCall + string(stateJSON)
	// Python: :230-234 add_section（cn/en 同文，priority=SESSION_TOOLS=82）
	builder.AddSection(saprompt.PromptSection{
		Name:     "session_tools",
		Content:  map[string]string{"cn": content, "en": content},
		Priority: sessionMessagingToolsPriority,
	})
	return nil
}

// BeforeToolCall 工具名命中集合时把 route + tool_call_id 写入调用级 extra。
// Python: :314-330 before_tool_call（ContextVar set → Go 调用级 extra 写入，spec §5）
func (r *SessionMessagingRouteRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	if _, hit := sessionMessagingToolNames[inputs.ToolName]; !hit {
		return nil
	}
	route := routeFromCBC(cbc)
	toolCallID := toolCallIDOfInputs(inputs)
	if route == nil || route.SessionID == "" || route.RequestID == "" {
		return nil
	}
	// Python: :329 _SESSION_MESSAGING_ROUTE.set(route) → Go 等价：调用级 extra 写 wire
	cbc.SetExtra(sessionMessagingRouteTokenExtraKey, route.ToWire())
	// Python: :331-333 _SESSION_MESSAGING_TOOL_CALL_ID.set(tool_call_id)
	cbc.SetExtra(sessionMessagingToolCallIDExtraKey, toolCallID)
	return nil
}

// AfterToolCall 复位调用级 extra（等价 Python token reset）。
// Python: :332-341 after_tool_call
func (r *SessionMessagingRouteRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil {
		return nil
	}
	cbc.DeleteExtra(sessionMessagingToolCallIDExtraKey)
	cbc.DeleteExtra(sessionMessagingRouteTokenExtraKey)
	return nil
}

// OnToolException 异常路径同样复位（Python finally 语义）。
// Python: :343-345 on_tool_exception
func (r *SessionMessagingRouteRail) OnToolException(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return r.AfterToolCall(ctx, cbc)
}

// GetCallbacks 覆写基类回调映射，注册四个钩子。
func (r *SessionMessagingRouteRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackOnToolException] = func(ctx context.Context, railCtx any) error {
		return r.OnToolException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// routeFromCBC 从回调上下文解析路由。
// Python: :276-313 _route（ctx.extra["session_input_message_route"] 或
// run_context.extra[SESSION_MESSAGING_ROUTE_EXTRA_KEY]）
func routeFromCBC(cbc *agentinterfaces.AgentCallbackContext) *SessionMessagingRoute {
	if cbc == nil {
		return nil
	}
	// session_input_message_route 直读键
	if v, ok := cbc.GetExtra(sessionInputMessageRouteExtraKey); ok {
		if raw, isMap := v.(map[string]any); isMap {
			return routeFromWire(raw)
		}
	}
	// run.context.extra[EXTRA_KEY]：run_context 由请求入口经 extra["run_context"] 载运
	if runRaw, ok := cbc.GetExtra("run_context"); ok {
		runMap, isMap := runRaw.(map[string]any)
		if !isMap {
			return nil
		}
		contextMap, _ := runMap["context"].(map[string]any)
		extra, _ := contextMap["extra"].(map[string]any)
		if extra != nil {
			if raw, ok := extra[SessionMessagingRouteExtraKey].(map[string]any); ok {
				return routeFromWire(raw)
			}
		}
	}
	return nil
}

// toolCallIDOfInputs 取工具调用标识。
// Python: :236-251 _tool_call_id（tool_call.id）
func toolCallIDOfInputs(inputs *agentinterfaces.ToolCallInputs) string {
	if inputs == nil || inputs.ToolCall == nil {
		return ""
	}
	return inputs.ToolCall.ID
}
