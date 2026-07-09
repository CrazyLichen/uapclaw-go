package server

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// protocolVersion 协议版本
	protocolVersion = "1.0"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleInitialize 处理 initialize 请求。返回默认 capabilities。
func (s *AgentServer) handleInitialize(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"capabilities":     map[string]any{},
			"protocol_version": protocolVersion,
		}),
	), nil
}

// handleACPToolResponse 处理 acp.tool_response 请求。stub：返回 ok=true。
func (s *AgentServer) handleACPToolResponse(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// TODO: 实现 ACP 工具响应处理逻辑
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}
