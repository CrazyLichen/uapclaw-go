package server

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleConfigCacheClear 处理 config.cache_clear 请求。清除配置缓存，返回 ok=true。
func (s *AgentServer) handleConfigCacheClear(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	if s.config != nil {
		// 重新加载配置，等效于清除缓存
		_ = s.config.Reload()
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleAgentReloadConfig 处理 agent.reload_config 请求。
// 从 request.Params 提取 config 和 env，调用 AgentManager.ReloadAgentsConfig。
// 对齐 Python: _handle_agent_reload_config (agent_ws_server.py L4147-4171)
func (s *AgentServer) handleAgentReloadConfig(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var configPayload map[string]any
	var envOverrides map[string]any

	if len(request.Params) > 0 {
		var params map[string]any
		if err := json.Unmarshal(request.Params, &params); err == nil {
			if c, ok := params["config"]; ok {
				if m, ok := c.(map[string]any); ok {
					configPayload = m
				}
			}
			if e, ok := params["env"]; ok {
				if m, ok := e.(map[string]any); ok {
					envOverrides = m
				}
			}
		}
	}

	if err := s.agentManager.ReloadAgentsConfig(configPayload, envOverrides); err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": err.Error(),
			}),
		), nil
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"reloaded": true,
		}),
	), nil
}
