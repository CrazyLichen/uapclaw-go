package server

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

const (
	// protocolVersion 协议版本
	protocolVersion = "1.0"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleInitialize 处理 initialize 请求。
//
// 对齐 Python: AgentWsServer._handle_initialize(ws, request, send_lock)
//
// 步骤：
//  1. 构建 extra_config（含 protocol_version、client_capabilities）
//  2. 调用 AgentManager.Initialize() 获取 capabilities
//  3. 非 ACP 通道返回 nil → fallback 到默认 capabilities
//  4. ACP 通道返回 capabilities（⤵️ 等 ACP 实现）
func (s *AgentServer) handleInitialize(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 1. 构建 extra_config（对齐 Python agent_ws_server.py L563-572）
	extraConfig := make(map[string]any)
	extraConfig["protocol_version"] = protocolVersion

	// 从 params 解析 clientCapabilities
	if len(request.Params) > 0 {
		var params map[string]any
		if err := json.Unmarshal(request.Params, &params); err == nil {
			if caps, ok := params["clientCapabilities"]; ok {
				extraConfig["client_capabilities"] = caps
			}
		}
	}

	// 2. 调用 AgentManager.Initialize
	caps, err := s.agentManager.Initialize(ctx, request.ChannelID, extraConfig)
	if err != nil {
		// 初始化失败 → fallback 到默认 capabilities
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"capabilities":     map[string]any{},
				"protocol_version": protocolVersion,
			}),
		), nil
	}

	// 3. 非 ACP 通道返回 nil → fallback 到默认 capabilities
	if caps == nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"capabilities":     map[string]any{},
				"protocol_version": protocolVersion,
			}),
		), nil
	}

	// 4. ACP 通道返回 capabilities（⤵️ 等 ACP 实现后 Initialize 会返回非 nil）
	caps["protocol_version"] = protocolVersion
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(caps),
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
