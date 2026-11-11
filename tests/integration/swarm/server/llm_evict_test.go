//go:build integration

// LLM 连接池热重载驱逐端到端集成测试。
// 跨子系统：swarm/server/runtime（AgentManager diff 与 ReloadOptions）→
// llm/model_clients（共享 Transport 池与精确驱逐）→ llm/schema（配置归一）→
// common/config（GetDefaultModels 含 agentos）。
//
// Python: jiuwenswarm/server/runtime/agent_manager.py:1882-1960
// _evict_stale_llm_clients 的链路级对拍。
package server_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	rt "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMEvictSuite 热重载驱逐集成测试套件。
type LLMEvictSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLLMEvictSuite 运行套件。
func TestLLMEvictSuite(t *testing.T) {
	suite.Run(t, new(LLMEvictSuite))
}

// poolInstance 取回（或建立）指定身份的共享 Transport。
func poolInstance(cfg *llmschema.ModelClientConfig) *transportInstance {
	return model_clients.GetSharedTransport(cfg.AuthMode, cfg.APIKey, cfg.APIBase, cfg.VerifySSL, cfg.SSLCert)
}

// transportInstance 类型别名（断言层不直接引 net/http）。
type transportInstance = model_clients.SharedTransport

// IT-E1 reload 移除模型 → 旧 key 连接被驱逐、新 key 不受影响。
func (s *LLMEvictSuite) TestReload驱逐_移除模型() {
	model_clients.ResetSharedTransports()
	defer model_clients.ResetSharedTransports()

	am := rt.NewAgentManager()
	cfgEntryA := map[string]any{
		"model_client_config": map[string]any{
			"client_provider": "Anthropic", "model_name": "mA",
			"api_key": "kA", "api_base": "https://evict-a.test",
		},
	}
	cfgEntryB := map[string]any{
		"model_client_config": map[string]any{
			"client_provider": "OpenAI", "model_name": "mB",
			"api_key": "kB", "api_base": "https://evict-b.test",
		},
	}
	// VerifySSL=true 对齐 evict 链路：NewModelClientConfig 默认 true（schema/config.go:460），
	// 池 key 不一致会导致驱逐永远 miss。
	cfgA := &llmschema.ModelClientConfig{ClientID: "tA", ClientProvider: "Anthropic",
		AuthMode: llmschema.AuthModeAPIKey, APIKey: "kA", APIBase: "https://evict-a.test", VerifySSL: true}
	cfgB := &llmschema.ModelClientConfig{ClientID: "tB", ClientProvider: "OpenAI",
		AuthMode: llmschema.AuthModeAPIKey, APIKey: "kB", APIBase: "https://evict-b.test", VerifySSL: true}

	// 两个 key 各建一条池连接
	ta := poolInstance(cfgA)
	tb := poolInstance(cfgB)

	// reload 1：登记 A（首次变更只登记不关闭——Python :1893-1896）
	am.EvictStaleLLMClients(map[string]any{"models": map[string]any{"defaults": []any{cfgEntryA}}})
	// reload 2：B 替换 A → A 的连接被驱逐
	am.EvictStaleLLMClients(map[string]any{"models": map[string]any{"defaults": []any{cfgEntryB}}})

	// A 的 transport 实例更换（驱逐后重建）
	ta2 := poolInstance(cfgA)
	s.Require().NotSame(ta, ta2, "A 的连接应被驱逐并重建")
	// B 不受影响
	s.Require().Same(tb, poolInstance(cfgB), "B 的连接不应受影响")
}

// IT-E2 reload 移除模型（换凭证同形态）→ 旧 key 连接被驱逐。
func (s *LLMEvictSuite) TestReload驱逐_换凭证() {
	model_clients.ResetSharedTransports()
	defer model_clients.ResetSharedTransports()

	am := rt.NewAgentManager()
	cfgOld := map[string]any{
		"model_client_config": map[string]any{
			"client_provider": "Anthropic", "model_name": "m",
			"api_key": "k-old", "api_base": "https://rotate.test",
		},
	}
	cfgNew := map[string]any{
		"model_client_config": map[string]any{
			"client_provider": "Anthropic", "model_name": "m",
			"api_key": "k-new", "api_base": "https://rotate.test",
		},
	}
	cfgPool := &llmschema.ModelClientConfig{ClientID: "t", ClientProvider: "Anthropic",
		AuthMode: llmschema.AuthModeAPIKey, APIKey: "k-old", APIBase: "https://rotate.test", VerifySSL: true}
	tOld := poolInstance(cfgPool)

	am.EvictStaleLLMClients(map[string]any{"models": map[string]any{"defaults": []any{cfgOld}}})
	am.EvictStaleLLMClients(map[string]any{"models": map[string]any{"defaults": []any{cfgNew}}})

	tNew := poolInstance(cfgPool)
	s.Require().NotSame(tOld, tNew, "换凭证后旧连接应被驱逐")
}

// IT-E3 ReloadOptions scopes 链路：model 域 reload 端到端触发驱逐。
func (s *LLMEvictSuite) TestReloadOptions_scopes链路() {
	model_clients.ResetSharedTransports()
	defer model_clients.ResetSharedTransports()

	am := rt.NewAgentManager()
	cfgEntry := map[string]any{
		"model_client_config": map[string]any{
			"client_provider": "Anthropic", "model_name": "m",
			"api_key": "k-scope", "api_base": "https://scope.test",
		},
	}
	effective := map[string]any{"models": map[string]any{"defaults": []any{cfgEntry}}}
	cfgPool := &llmschema.ModelClientConfig{ClientID: "t", ClientProvider: "Anthropic",
		AuthMode: llmschema.AuthModeAPIKey, APIKey: "k-scope", APIBase: "https://scope.test", VerifySSL: true}
	tBefore := poolInstance(cfgPool)

	// reload 1（model 域）：登记快照（首次只登记不关闭）
	s.Require().NoError(am.ReloadAgentsConfig(nil, effective, nil,
		rt.ReloadOptions{ReloadScopes: []string{"model"}}))
	s.Require().Same(tBefore, poolInstance(cfgPool), "首次 reload 只登记、不驱逐")

	// reload 2：配置删除模型 → diff 出 removed → 驱逐
	s.Require().NoError(am.ReloadAgentsConfig(nil, map[string]any{"models": map[string]any{}}, nil,
		rt.ReloadOptions{ReloadScopes: []string{"model"}}))
	tAfter := poolInstance(cfgPool)
	s.Require().NotSame(tBefore, tAfter, "删除模型后旧 key 连接应被驱逐")
}
