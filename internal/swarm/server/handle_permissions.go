package server

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handlePermissionsConfig 处理 permissions.* 请求。统一入口，按 req_method 二次分发。
//
// 对齐 Python AgentWebSocketServer 中 permissions 相关处理函数。当前全部为 stub。
func (s *AgentServer) handlePermissionsConfig(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	switch request.ReqMethod {
	case schema.ReqMethodPermissionsToolsGet:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"tools": []any{},
			}),
		), nil
	case schema.ReqMethodPermissionsToolsSet:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"ok": true,
			}),
		), nil
	case schema.ReqMethodPermissionsToolsUpdate:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"ok": true,
			}),
		), nil
	case schema.ReqMethodPermissionsToolsDelete:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"ok": true,
			}),
		), nil
	case schema.ReqMethodPermissionsRulesGet:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"rules": []any{},
			}),
		), nil
	case schema.ReqMethodPermissionsRulesCreate:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"ok": true,
			}),
		), nil
	case schema.ReqMethodPermissionsRulesUpdate:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"ok": true,
			}),
		), nil
	case schema.ReqMethodPermissionsRulesDelete:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"ok": true,
			}),
		), nil
	case schema.ReqMethodPermissionsApprovalOverridesGet:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"overrides": []any{},
			}),
		), nil
	case schema.ReqMethodPermissionsApprovalOverridesDelete:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"ok": true,
			}),
		), nil
	default:
		return notImplementedResponse(request)
	}
}
