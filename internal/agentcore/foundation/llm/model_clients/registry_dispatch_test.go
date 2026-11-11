package model_clients

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestImplementationProvider_四分支 对齐上游四分支分派。
// Python: model_clients/__init__.py:24-38 _implementation_provider
func TestImplementationProvider_四分支(t *testing.T) {
	cases := []struct {
		name string
		cfg  *llmschema.ModelClientConfig
		want string
	}{
		{"anthropic 协议无视原 provider", &llmschema.ModelClientConfig{ClientProvider: "DashScope", APIMode: "anthropic_messages"}, "Anthropic"},
		{"非 OpenAI 原样返回", &llmschema.ModelClientConfig{ClientProvider: "intelli_router"}, "intelli_router"},
		{"OpenAI + oauth → OpenAIAccount", &llmschema.ModelClientConfig{ClientProvider: "OpenAI", AuthMode: llmschema.AuthModeOpenAIAccountOAuth}, "OpenAIAccount"},
		{"OpenAI 默认", &llmschema.ModelClientConfig{ClientProvider: "OpenAI"}, "OpenAI"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ImplementationProvider(c.cfg); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestImplementationProvider_nil 验证 nil 安全。
func TestImplementationProvider_nil(t *testing.T) {
	if got := ImplementationProvider(nil); got != "" {
		t.Errorf("got %q", got)
	}
}

// 说明：别名 provider 归一后由 openai 包承接，该链路需要已注册的注册表。
// model_clients 包不能 import openai（会成环），故该用例放在 openai 包内：
// 见 openai/dispatch_test.go 的 TestCreateModelClient_别名provider落到OpenAI。

// TestCreateModelClient_归一失败上抛 验证校准错误不静默。
// InferenceAffinity 的 kv_cache.mode=release 被上游 Literal 拒绝（偏差 D-10）。
func TestCreateModelClient_归一失败上抛(t *testing.T) {
	_, err := CreateModelClient(&llmschema.ModelClientConfig{
		ClientID:       "cid",
		ClientProvider: "InferenceAffinity",
		APIKey:         "sk-test",
		APIBase:        "https://example.com/v1",
	}, &llmschema.ModelRequestConfig{ModelName: "m"})
	if err == nil {
		t.Fatal("InferenceAffinity 应因 kv_cache.mode=release 归一失败而上抛错误")
	}
}
