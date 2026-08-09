package server

import (
	"context"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleExtensionsList 处理 extensions.list 请求。stub：返回空列表。
func (s *AgentServer) handleExtensionsList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"extensions": []any{},
		}),
	), nil
}

// handleExtensionsImport 处理 extensions.import 请求。stub：返回 ok=true。
func (s *AgentServer) handleExtensionsImport(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleExtensionsDelete 处理 extensions.delete 请求。stub：返回 ok=true。
func (s *AgentServer) handleExtensionsDelete(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleExtensionsToggle 处理 extensions.toggle 请求。stub：返回 ok=true。
func (s *AgentServer) handleExtensionsToggle(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleHooksList 处理 hooks.list 请求。返回 hooks 配置摘要。
// 对齐 Python: _handle_hooks_list — payload 包含 events、disable_all_hooks、source
// 对齐 Python: try/except 包裹，失败时返回 ok=False
func (s *AgentServer) handleHooksList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 对齐 Python: try/except 包裹，失败时返回 ok=False
	configBase, _ := s.config.Load()
	hooksCfg := hookscfg.LoadHooksConfig(configBase)
	summary := hooksCfg.GetEventSummary()
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"events":            summary,
			"disable_all_hooks": hooksCfg.DisableAllHooks,
			"source":            "config.yaml",
		}),
	), nil
}
