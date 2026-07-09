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

// handleAgentsList 处理 agents.list 请求。stub：返回空列表。
func (s *AgentServer) handleAgentsList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"agents": []any{},
		}),
	), nil
}

// handleAgentsGet 处理 agents.get 请求。stub：返回 NOT_FOUND。
func (s *AgentServer) handleAgentsGet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{
			"error": map[string]any{
				"code":    "NOT_FOUND",
				"message": "Agent 不存在",
			},
		}),
	), nil
}

// handleAgentsCreate 处理 agents.create 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleAgentsCreate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleAgentsUpdate 处理 agents.update 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleAgentsUpdate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleAgentsDelete 处理 agents.delete 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleAgentsDelete(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleAgentsEnable 处理 agents.enable 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleAgentsEnable(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleAgentsDisable 处理 agents.disable 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleAgentsDisable(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleAgentsToolsList 处理 agents.tools_list 请求。stub：返回空列表。
func (s *AgentServer) handleAgentsToolsList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"tools": []any{},
		}),
	), nil
}
