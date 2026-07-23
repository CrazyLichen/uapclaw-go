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

// handleTeamDelete 处理 team.delete 请求。stub：返回 ok=true。
func (s *AgentServer) handleTeamDelete(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleTeamSnapshot 处理 team.snapshot 请求。stub：返回空快照。
func (s *AgentServer) handleTeamSnapshot(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"snapshot": map[string]any{},
		}),
	), nil
}

// handleTeamHistoryGet 处理 team.history.get 请求。读 team history 记录（纯文件系统），返回空列表。
func (s *AgentServer) handleTeamHistoryGet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// TODO(#team-history): 从文件系统读取 team history 记录
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"history": []any{},
		}),
	), nil
}
