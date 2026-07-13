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
//
// 对齐 Python: AgentWsServer._handle_initialize(ws, request, send_lock)
//
// Python 完整步骤：
//  1. 解析 params.clientCapabilities
//  2. 构建 extra_config（含 protocol_version、client_capabilities）
//  3. ACP channel 特殊处理（_set_ws_acp_client_capabilities）
//  4. 调用 agent_manager.initialize() 获取 capabilities
//  5. Fallback 到 ACP_DEFAULT_CAPABILITIES
//  6. 返回 capabilities
//
// 当前仅返回空 capabilities + protocol_version，待后续补齐：
//
//	⤵️ ACP 章节：解析 clientCapabilities、ACP channel 处理
//	⤵️ AgentManager 章节：agentManager.initialize() 调用、ACP_DEFAULT_CAPABILITIES fallback
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
