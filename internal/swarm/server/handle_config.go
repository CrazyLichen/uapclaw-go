package server

import (
	"context"
	"encoding/json"
	"fmt"
	rt "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

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
// 从 request.Params 提取 config / env / target_channel_id / target_session_id /
// reload_scopes，调用 AgentManager.ReloadAgentsConfig。
// Python: agent_ws_server.py:12860-12889 _handle_agent_reload_config
// （reload_scopes 只接受字符串列表；合法域校验在 AgentManager 侧按交集判断）。
func (s *AgentServer) handleAgentReloadConfig(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var configPayload map[string]any
	var envOverrides map[string]string
	opts := rt.ReloadOptions{}

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
					envOverrides = anyMapToStringMap(m)
				}
			}
			// Python: agent_ws_server.py:12866-12878 target/scope 解析
			//（scope 合法域 {model, multimodal, search, team, permissions, agent_runtime}）
			if tc, ok := params["target_channel_id"]; ok {
				opts.TargetChannelID = strings.TrimSpace(fmt.Sprintf("%v", tc))
				if opts.TargetChannelID == "<nil>" {
					opts.TargetChannelID = ""
				}
			}
			if ts, ok := params["target_session_id"]; ok {
				opts.TargetSessionID = strings.TrimSpace(fmt.Sprintf("%v", ts))
				if opts.TargetSessionID == "<nil>" {
					opts.TargetSessionID = ""
				}
			}
			if rawScopes, ok := params["reload_scopes"].([]any); ok {
				for _, scope := range rawScopes {
					if str, isStr := scope.(string); isStr && str != "" {
						opts.ReloadScopes = append(opts.ReloadScopes, str)
					}
				}
			}
		}
	}

	if err := s.agentManager.ReloadAgentsConfig(ctx, configPayload, envOverrides, opts); err != nil {
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

// anyMapToStringMap 将 map[string]any 转为 map[string]string，
// 用于从 JSON 反序列化的 env 参数适配 AgentManager.ReloadAgentsConfig 的 string 类型签名。
func anyMapToStringMap(m map[string]any) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		if v == nil {
			result[k] = ""
			continue
		}
		if s, ok := v.(string); ok {
			result[k] = s
		} else {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}
