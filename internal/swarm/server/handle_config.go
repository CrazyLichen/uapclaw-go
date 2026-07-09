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

// handleAgentReloadConfig 处理 agent.reload_config 请求。stub：返回 ok=true。
func (s *AgentServer) handleAgentReloadConfig(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// TODO: 实现配置热重载逻辑
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}
