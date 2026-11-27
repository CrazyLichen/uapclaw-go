package repositories

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 常量 ────────────────────────────

// controlSessionMethods 控制面会话方法集合。
// Python: server/control/methods.py CONTROL_SESSION_METHODS（:14-20）：
// list / get_metadata / pin / color_set / preview。
// Go 侧 list 与 get_metadata 走网关本地实现（session.list/session.get_metadata
// 既有链路），此处仅承载 pin；color_set/preview ⤴️ 挂账。
var controlSessionMethods = map[string]bool{
	"session.pin": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleSessionRequest 控制面会话请求统一入口。
// Python: session_repository.py handle_session_request（:202-228）——
// 非列表方法先过 lifecycle guard，再分派；本批仅承载 SESSION_PIN。
func HandleSessionRequest(request *schema.AgentRequest) *schema.AgentResponse {
	method := string(request.ReqMethod)
	if !controlSessionMethods[method] {
		return buildErrorResponse(request,
			"unsupported control session method: "+method, "BAD_REQUEST")
	}
	// 非列表方法先过 lifecycle guard（Python :207-217）
	params := requestParamsMap(request)
	sessionID := strings.TrimSpace(stringFromParams(params, "session_id"))
	if request.SessionID != nil && sessionID == "" {
		sessionID = strings.TrimSpace(*request.SessionID)
	}
	if err := lifecycle.Guard(sessionID, ""); err != nil {
		code := "BAD_REQUEST"
		if le, ok := err.(*lifecycle.LifecycleError); ok && le.Code != "" {
			code = le.Code
		}
		return buildErrorResponse(request, err.Error(), code)
	}
	return HandleSessionPin(request)
}

// HandleSessionPin 置顶/取消置顶会话。
// Python: session_repository.py handle_pin（:213-231）。
func HandleSessionPin(request *schema.AgentRequest) *schema.AgentResponse {
	params := requestParamsMap(request)
	sid := strings.TrimSpace(stringFromParams(params, "session_id"))
	if sid == "" {
		return buildErrorResponse(request, "session_id is required", "BAD_REQUEST")
	}
	rawPinned, ok := params["pinned"].(bool)
	if !ok {
		return buildErrorResponse(request, "pinned must be boolean", "BAD_REQUEST")
	}
	newPinned, newOrder, found := swarmsession.SetSessionPinned(sid, rawPinned)
	if !found {
		return buildErrorResponse(request, "session not found", "NOT_FOUND")
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(map[string]any{"pinned": newPinned, "pin_order": float64(newOrder)}),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringFromParams 取参数字符串字段。
func stringFromParams(params map[string]any, key string) string {
	s, _ := params[key].(string)
	return s
}
