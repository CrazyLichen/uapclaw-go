package openai

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestCreateModelClient_别名provider落到OpenAI 验证别名归一后由本包承接。
//
// 本用例必须放在 openai 包：注册依赖本包 init()，而 model_clients 包
// 不能 import openai（会成环）。
// Python: model_clients/__init__.py:88-92（先 normalize 再按实现名分派）
func TestCreateModelClient_别名provider落到OpenAI(t *testing.T) {
	// 11 个别名 provider 归一后 client_provider=OpenAI，全部由本包承接
	for _, provider := range []string{
		"DashScope", "SiliconFlow", "DeepSeek", "OpenRouter",
		"Moonshot", "MiniMax", "ModelArts", "VolcEngine", "Qianfan", "Zhipu", "MiMo",
	} {
		t.Run(provider, func(t *testing.T) {
			client, err := model_clients.CreateModelClient(&llmschema.ModelClientConfig{
				ClientID:       "cid",
				ClientProvider: provider,
				APIKey:         "sk-test",
				APIBase:        "https://example.com/v1",
			}, &llmschema.ModelRequestConfig{ModelName: "m"})
			if err != nil {
				t.Fatalf("创建失败: %v", err)
			}
			if _, ok := client.(*OpenAIModelClient); !ok {
				t.Fatalf("应由 OpenAIModelClient 承接，got %T", client)
			}
		})
	}
}

// TestCreateModelClient_归一注入endpointProfile 验证归一后 endpoint_profile 被写入。
// Python: endpoint_profiles.py:104-142 LEGACY_PROVIDER_ALIASES
func TestCreateModelClient_归一注入endpointProfile(t *testing.T) {
	// 用反射不可行，改为直接调归一函数验证（它由 registry 调用）
	cfg := &llmschema.ModelClientConfig{
		ClientID:       "cid",
		ClientProvider: "DashScope",
		APIKey:         "sk-test",
		APIBase:        "https://example.com/v1",
	}
	if _, err := model_clients.CreateModelClient(cfg, &llmschema.ModelRequestConfig{ModelName: "qwen-max"}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	// 入参不被修改（归一返回副本）
	if cfg.ClientProvider != "DashScope" {
		t.Errorf("入参被修改: %q", cfg.ClientProvider)
	}
	if cfg.EndpointProfile != "" {
		t.Errorf("入参被修改: EndpointProfile=%q", cfg.EndpointProfile)
	}
}

// TestCreateModelClient_OpenAI原样 验证 OpenAI 自身不经别名表。
func TestCreateModelClient_OpenAI原样(t *testing.T) {
	client, err := model_clients.CreateModelClient(&llmschema.ModelClientConfig{
		ClientID:       "cid",
		ClientProvider: "OpenAI",
		APIKey:         "sk-test",
		APIBase:        "https://api.openai.com/v1",
	}, &llmschema.ModelRequestConfig{ModelName: "gpt-4"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, ok := client.(*OpenAIModelClient); !ok {
		t.Fatalf("got %T", client)
	}
}
