package runtime

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/endpointprofiles"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// connDiffKey 驱逐判定八元组，比 core connection_key 宽是刻意保守。
// Python: jiuwenswarm/server/runtime/agent_manager.py:1906-1926 _ConnDiffKey
// 「新声明下同一 api_base 可能对应不同 endpoint_profile / auth_mode / api_mode，
// 这些会影响连接身份（如 affinity 走 custom_headers 不带 Authorization）。
// 纳入 diff key 避免误关/漏关连接池。」
type connDiffKey struct {
	clientProvider  string
	endpointProfile string
	authMode        string
	apiMode         string
	apiKey          string
	apiBase         string
	verifySSL       bool
	sslCert         string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EvictStaleLLMClients 模型热更新后，关闭"已从 models.defaults 删除/改掉凭证"的 LLM 连接。
// agent-core 的 HTTP client 池是进程级共享的，这里只做**增量关闭**（上次默认集 -
// 本次默认集），绝不碰其它组件仍在使用的连接。被删除/更新的模型即使有在途调用也会
// 立即断开——用户既然不再使用它，就不该让它继续偷偷消耗 token。被误伤的连接会在
// 下次调用时自愈重建。进程启动后的第一次模型变更，因无历史快照，只登记不关闭。
// Python: jiuwenswarm/server/runtime/agent_manager.py:1882-1960 _evict_stale_llm_clients
func (am *AgentManager) EvictStaleLLMClients(effectiveConfig map[string]any) {
	newConfigs := map[connDiffKey]*llmschema.ModelClientConfig{}

	// 步骤 1：从生效配置枚举全部默认模型条目（含 agentos，S0-5 已补）
	entries := config.GetDefaultModels(effectiveConfig)
	for _, entry := range entries {
		mcc, ok := entry["model_client_config"].(map[string]any)
		if !ok {
			continue
		}
		// 步骤 2：构造归一化 ModelClientConfig；无效条目 skip + warning（Python :1935-1939）
		clientProvider, _ := mcc["client_provider"].(string)
		if clientProvider == "" {
			clientProvider = "OpenAI"
		}
		cfg := buildModelClientConfig(clientProvider, mcc)
		if cfg == nil {
			logger.Warn(logComponent).
				Str("model_name", stringOf(mcc["model_name"])).
				Msg("[AgentManager] skip invalid model config during evict")
			continue
		}
		normalized, err := endpointprofiles.NormalizeModelClientConfig(cfg)
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("model_name", stringOf(mcc["model_name"])).
				Msg("[AgentManager] skip invalid model config during evict")
			continue
		}
		key := connDiffKey{
			clientProvider:  normalized.ClientProvider,
			endpointProfile: normalized.EndpointProfile,
			authMode:        string(normalized.AuthMode),
			apiMode:         string(normalized.APIMode),
			apiKey:          normalized.APIKey,
			apiBase:         normalized.APIBase,
			verifySSL:       normalized.VerifySSL,
			sslCert:         normalized.SSLCert,
		}
		newConfigs[key] = normalized
	}

	// 步骤 3：diff——removed = 上次快照 - 本次
	am.mu.Lock()
	removed := make([]*llmschema.ModelClientConfig, 0, len(am.lastModelConnConfigs))
	for key, cfg := range am.lastModelConnConfigs {
		if _, exists := newConfigs[key]; !exists {
			removed = append(removed, cfg)
		}
	}
	am.lastModelConnConfigs = newConfigs
	am.mu.Unlock()

	// 步骤 4：空 diff 早退
	if len(removed) == 0 {
		return
	}

	// 步骤 5：core 侧精确驱逐（23.1-D2）
	model_clients.CloseConnections(removed)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildModelClientConfig 从配置 dict 构造 ModelClientConfig（容忍字段缺失）。
// Python: ModelClientConfig(**mcc_fields) 的宽松对应（失败返回 nil 由调用方 warning）。
func buildModelClientConfig(provider string, mcc map[string]any) *llmschema.ModelClientConfig {
	cfg, err := llmschema.NewModelClientConfig(
		provider,
		stringOf(mcc["api_key"]),
		stringOf(mcc["api_base"]),
	)
	if err != nil {
		return nil
	}
	return cfg
}

// stringOf 安全转字符串。
func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
