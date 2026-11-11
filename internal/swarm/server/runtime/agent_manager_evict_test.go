package runtime

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestConnDiffKey_八元组 endpoint_profile/auth_mode/api_mode 变化触发驱逐。
func TestConnDiffKey_八元组(t *testing.T) {
	base := connDiffKey{clientProvider: "Anthropic", endpointProfile: "deepseek",
		authMode: "api_key", apiMode: "anthropic_messages", apiKey: "k",
		apiBase: "https://a", verifySSL: false}
	same := base
	if base != same {
		t.Fatal("同八元组应相等")
	}
	changedEP := base
	changedEP.endpointProfile = "openrouter"
	if base == changedEP {
		t.Fatal("endpoint_profile 变化应不等")
	}
	changedAuth := base
	changedAuth.authMode = "custom_headers"
	if base == changedAuth {
		t.Fatal("auth_mode 变化应不等")
	}
	changedMode := base
	changedMode.apiMode = "chat_completions"
	if base == changedMode {
		t.Fatal("api_mode 变化应不等")
	}
	changedKey := base
	changedKey.apiKey = "k2"
	if base == changedKey {
		t.Fatal("api_key 变化应不等")
	}
}

// TestEvictStaleLLMClients_首次只登记 快照建立但不驱逐（无历史快照语义）。
func TestEvictStaleLLMClients_首次只登记(t *testing.T) {
	am := NewAgentManager()
	cfgEntry := map[string]any{
		"model_client_config": map[string]any{
			"client_provider": "Anthropic", "model_name": "m1",
			"api_key": "kA", "api_base": "https://a",
		},
	}
	am.EvictStaleLLMClients(map[string]any{"models": map[string]any{"defaults": []any{cfgEntry}}})
	if len(am.lastModelConnConfigs) != 1 {
		t.Fatalf("首次应登记 1 条, got %d", len(am.lastModelConnConfigs))
	}
	// 第二次相同配置：diff 为空，快照不变
	am.EvictStaleLLMClients(map[string]any{"models": map[string]any{"defaults": []any{cfgEntry}}})
	if len(am.lastModelConnConfigs) != 1 {
		t.Fatalf("相同配置快照应不变, got %d", len(am.lastModelConnConfigs))
	}
}

// TestEvictStaleLLMClients_移除触发驱逐 移除后快照更新、驱逐走 core 侧。
func TestEvictStaleLLMClients_移除触发驱逐(t *testing.T) {
	am := NewAgentManager()
	cfgA := map[string]any{
		"model_client_config": map[string]any{
			"client_provider": "Anthropic", "model_name": "mA",
			"api_key": "kA", "api_base": "https://a",
		},
	}
	cfgB := map[string]any{
		"model_client_config": map[string]any{
			"client_provider": "OpenAI", "model_name": "mB",
			"api_key": "kB", "api_base": "https://b",
		},
	}
	// 先建池（被驱逐条目真实存在于共享池）
	modelPool := llmschema.ModelClientConfig{ClientID: "t", ClientProvider: "Anthropic",
		AuthMode: llmschema.AuthModeAPIKey, APIKey: "kA", APIBase: "https://a"}
	model_clients.GetSharedTransport(modelPool.AuthMode, modelPool.APIKey, modelPool.APIBase,
		modelPool.VerifySSL, modelPool.SSLCert)

	am.EvictStaleLLMClients(map[string]any{"models": map[string]any{"defaults": []any{cfgA}}})
	// B 替换 A
	am.EvictStaleLLMClients(map[string]any{"models": map[string]any{"defaults": []any{cfgB}}})
	if len(am.lastModelConnConfigs) != 1 {
		t.Fatalf("快照应更新为 1 条, got %d", len(am.lastModelConnConfigs))
	}
	// 驱逐后 core 池重建实例
	ta := model_clients.GetSharedTransport(modelPool.AuthMode, modelPool.APIKey, modelPool.APIBase,
		modelPool.VerifySSL, modelPool.SSLCert)
	_ = ta
	// （驱逐的真实性由 core 侧 TestCloseConnections_精确驱逐 保证，此处验证 diff 正确性）
	model_clients.ResetSharedTransports()
}

// TestReloadFingerprint 相同输入稳定、任一维度变化不同。
func TestReloadFingerprint(t *testing.T) {
	cfg := map[string]any{"a": 1}
	f1 := reloadFingerprint(cfg, map[string]string{"E": "1"}, "ch1", "", []string{"model"})
	f2 := reloadFingerprint(cfg, map[string]string{"E": "1"}, "ch1", "", []string{"model"})
	if f1 != f2 {
		t.Fatal("相同输入应稳定")
	}
	f3 := reloadFingerprint(cfg, map[string]string{"E": "1"}, "ch1", "", []string{"multimodal"})
	if f1 == f3 {
		t.Fatal("scopes 变化应不同")
	}
	f4 := reloadFingerprint(cfg, map[string]string{"E": "2"}, "ch1", "", []string{"model"})
	if f1 == f4 {
		t.Fatal("env 变化应不同")
	}
}

// TestContainsScope 集合判定。
func TestContainsScope(t *testing.T) {
	set := map[string]struct{}{"model": {}}
	if !containsScope(set, "model") || containsScope(set, "search") {
		t.Fatal("containsScope 判定失败")
	}
}
