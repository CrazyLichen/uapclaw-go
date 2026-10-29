package server

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
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

// handleInitialize 处理 initialize 请求。
//
// Python: AgentWsServer._handle_initialize(ws, request, send_lock)
//
// 步骤：
//  1. 构建 extra_config（含 protocol_version、client_capabilities）
//  2. 调用 AgentManager.Initialize() 获取 capabilities
//  3. 非 ACP 通道返回 nil → fallback 到默认 capabilities
//  4. ACP 通道返回 capabilities（AgentManager ACP 分支已回填，10.6.24d Task 9）
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

	// 4. ACP 通道返回 capabilities（AgentManager ACP 分支已回填，10.6.24d Task 9）
	caps["protocol_version"] = protocolVersion
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(caps),
	), nil
}

// handleACPToolResponse 处理 acp.tool_response 请求：完成 pending 的 JSON-RPC 请求。
// Python: _handle_acp_tool_response (agent_ws_server.py L4659-4698)
//
// params: {jsonrpc_id, response}；完成成功回 accepted=true，
// 未知/迟到回 accepted=false + ignored=true（回填点④）。
func (s *AgentServer) handleACPToolResponse(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := map[string]any{}
	if len(request.Params) > 0 {
		_ = json.Unmarshal(request.Params, &params)
	}
	// rawID 原样回传/记日志（Python payload 回传 params 原值，可为 None/int/str）；
	// jsonrpcID 归一后仅用于 pending 查找。
	rawID := params["jsonrpc_id"]
	jsonrpcID := swarmtools.NormalizeJsonrpcID(rawID)
	responsePayload, _ := params["response"].(map[string]any)
	if responsePayload == nil {
		responsePayload = map[string]any{}
	}

	if swarmtools.GetAcpOutputManager().CompleteJsonrpcResponse(jsonrpcID, responsePayload) {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"accepted": true,
			}),
		), nil
	}
	// Python L4679-4683: 未知/迟到响应仅 Info 不上抛
	logger.Info(logComponent).
		Str("jsonrpc_id", jsonToStr(rawID)).
		Str("request_id", request.RequestID).
		Msg("[AgentServer] ignore unknown/late acp tool response")
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"accepted":   false,
			"ignored":    true,
			"reason":     "unknown_or_late_response",
			"jsonrpc_id": rawID,
		}),
	), nil
}

// jsonToStr 任意 ID 值安全字符串化（nil→"<nil>" 对齐 Python %s）。
func jsonToStr(v any) string {
	if v == nil {
		return "<nil>"
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "<nil>"
	}
	return string(b)
}
