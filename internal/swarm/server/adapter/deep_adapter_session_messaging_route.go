// Package adapter —— session messaging 请求入口路由绑定。
// Python: interface_deep.py:18319-18324（非流式）/ :19570-19575（流式）
// bind_session_messaging_route + :18363-18364 / :19617-19618
// with_session_messaging_route(inputs, current_session_messaging_route())。
package adapter

import (
	"context"
	"strings"

	sessionmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// sessionMessageInternalKey 宿主注入的跨会话信任标记键。
// Python: gateway/common/session_message.py:7 SESSION_MESSAGE_INTERNAL_KEY

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionMessagingRouteContext 解析宿主认证的路由上下文（不改变提示词来源）。
// Python: session_messaging_toolkit.py:142-153 session_messaging_route_context
// （params[_jiuwenswarm_cross_session] 优先；trusted_session_message_route 字段
// Go schema.AgentRequest 无对应物，IPC 面随领域十五后续批次——偏差登记 spec §5）。
func sessionMessagingRouteContext(req *schema.AgentRequest) map[string]any {
	if req == nil {
		return nil
	}
	params := parseParams(req.Params)
	if crossSession, ok := params[sessionMessageInternalKey].(map[string]any); ok {
		return crossSession
	}
	return nil
}

// bindSessionMessagingRouteForRequest 请求入口绑定路由并注入 inputs。
// 返回新 ctx（含路由）与复位函数；inputs 原地更新（wire 载运）。
// Python: :18321-18324 bind + :18363-18364 with_session_messaging_route
func bindSessionMessagingRouteForRequest(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (context.Context, func()) {
	if req == nil {
		return ctx, func() {}
	}
	userID := paramString(paramsOf(req), "user_id", "")
	bound, reset := sessionmtools.BindSessionMessagingRoute(
		ctx,
		derefString(req.SessionID),
		req.RequestID,
		userID,
		sessionMessagingRouteContext(req),
	)
	// Python: :18363 inputs = with_session_messaging_route(inputs, current_session_messaging_route())
	if inputs != nil {
		updated := sessionmtools.WithSessionMessagingRoute(inputs, sessionmtools.CurrentSessionMessagingRoute(bound))
		for k, v := range updated {
			inputs[k] = v
		}
	}
	return bound, reset
}

// derefString *string nil 安全解引用（空回退 ""）。
func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

// paramsOf 取请求参数（nil 安全）。
func paramsOf(req *schema.AgentRequest) map[string]any {
	if req == nil {
		return nil
	}
	return parseParams(req.Params)
}
