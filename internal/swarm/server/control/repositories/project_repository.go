// Package repositories Front Control 的项目磁盘仓库（只读控制面）。
//
// 对齐 Python jiuwenswarm/server/control/repositories/project_repository.py（84 行）。
// Phase 1 Control 只拥有轻量项目读（list/info/sessions）；
// create/rename/pin 与全部 git 操作留在执行平面。
package repositories

import (
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/store"
)

// ──────────────────────────── 常量 ────────────────────────────

// controlProjectMethods 控制面项目方法集合。
// Python: server/control/methods.py CONTROL_PROJECT_METHODS（:24-32）：
// info / pinned_sessions / get_sessions / get_cron_sessions /
// cron_resolve_binding / list。create/rename/pin 与 git 留在执行平面。
var controlProjectMethods = map[string]bool{
	"project.list":                 true,
	"project.info":                 true,
	"project.pinned_sessions":      true,
	"project.get_sessions":         true,
	"project.get_cron_sessions":    true,
	"project.cron.resolve_binding": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleProjectRequest 项目请求统一入口。
// Python: project_repository.py handle_project_request（:49-84）。
func HandleProjectRequest(request *schema.AgentRequest) *schema.AgentResponse {
	method := string(request.ReqMethod)
	if !controlProjectMethods[method] {
		return buildErrorResponse(request,
			"unsupported control project method: "+method, "BAD_REQUEST")
	}
	params := requestParamsMap(request)
	var triple store.Triple
	switch request.ReqMethod {
	case schema.ReqMethodProjectList:
		triple = store.LoadProjectList(params)
	case schema.ReqMethodProjectInfo:
		triple = store.LoadProjectInfo(params)
	case schema.ReqMethodProjectPinnedSessions:
		triple = store.LoadPinnedSessions()
	case schema.ReqMethodProjectGetSessions:
		triple = store.LoadProjectSessions(params, userIDValue(request.UserID))
	case schema.ReqMethodProjectGetCronSessions:
		triple = store.LoadProjectCronSessions(params, userIDValue(request.UserID))
	case schema.ReqMethodProjectCronResolveBinding:
		triple = store.ResolveCronBindingControl(params, request.ChannelID)
	default:
		return buildErrorResponse(request,
			"unsupported control project method: "+method, "BAD_REQUEST")
	}
	if triple.Error != "" {
		return buildErrorResponse(request, triple.Error, orCode(triple.Code, "INTERNAL_ERROR"))
	}
	payload := triple.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(payload),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildErrorResponse 失败响应。
// Python: server/control/responses.py build_error_response。
func buildErrorResponse(request *schema.AgentRequest, message, code string) *schema.AgentResponse {
	logger.Warn(logger.ComponentAgentServer).Str("method", string(request.ReqMethod)).Msg(message)
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{"error": message, "message": message, "code": code}),
	)
}

// requestParamsMap 解析请求参数（非法/缺省回空表）。
func requestParamsMap(request *schema.AgentRequest) map[string]any {
	var params map[string]any
	if len(request.Params) == 0 {
		return map[string]any{}
	}
	if err := json.Unmarshal(request.Params, &params); err != nil || params == nil {
		return map[string]any{}
	}
	return params
}

// userIDValue *string → string。
func userIDValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// orCode 空码回退。
func orCode(code, fallback string) string {
	if code == "" {
		return fallback
	}
	return code
}
