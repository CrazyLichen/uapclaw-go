package adapter

import (
	"testing"
)

// TestBuildModelFromEntry_未知字段落Extra 对齐 pydantic extra="allow"。
func TestBuildModelFromEntry_未知字段落Extra(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{"model_name": "gpt-4", "api_key": "sk-test", "api_base": "https://api.openai.com/v1"},
		map[string]any{"frequency_penalty": 0.5, "seed": 42},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	extra := model.ModelConfig.Extra
	if extra["frequency_penalty"] != 0.5 {
		t.Errorf("frequency_penalty 未落 Extra：%v", extra)
	}
	if extra["seed"] != 42 {
		t.Errorf("seed 未落 Extra：%v", extra)
	}
}

// TestBuildModelFromEntry_stop生效 此前从不读 mco["stop"]。
func TestBuildModelFromEntry_stop生效(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{"model_name": "gpt-4", "api_key": "sk-test", "api_base": "https://api.openai.com/v1"},
		map[string]any{"stop": "\n\n"},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	stop := model.ModelConfig.Stop
	if stop == nil || *stop != "\n\n" {
		t.Errorf("Stop = %v, 期望 \\n\\n", stop)
	}
}

// TestBuildModelFromEntry_maxTokens为零不丢 上游直传，0 是合法值。
func TestBuildModelFromEntry_maxTokens为零不丢(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{"model_name": "gpt-4", "api_key": "sk-test", "api_base": "https://api.openai.com/v1"},
		map[string]any{"max_tokens": 0},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	maxTokens := model.ModelConfig.MaxTokens
	if maxTokens == nil {
		t.Fatal("max_tokens=0 被丢弃")
	}
	if *maxTokens != 0 {
		t.Errorf("MaxTokens = %d, 期望 0", *maxTokens)
	}
}

// TestBuildModelFromEntry_maxTokens字符串形态 走 config.IntValue，
// 与 pydantic Optional[int] 的 lax 强制转换一致（"128000" → 128000）。
func TestBuildModelFromEntry_maxTokens字符串形态(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{"model_name": "gpt-4", "api_key": "sk-test", "api_base": "https://api.openai.com/v1"},
		map[string]any{"max_tokens": "128000"},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	maxTokens := model.ModelConfig.MaxTokens
	if maxTokens == nil {
		t.Fatal("字符串形态的 max_tokens 被丢弃（config.IntValue 应接受纯数字字符串）")
	}
	if *maxTokens != 128000 {
		t.Errorf("MaxTokens = %d, 期望 128000", *maxTokens)
	}
}

// TestBuildModelFromEntry_reasoning注入 reasoning_level 生效。
func TestBuildModelFromEntry_reasoning注入(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{"model_name": "gpt-4", "api_key": "sk-test", "api_base": "https://api.openai.com/v1"},
		map[string]any{"reasoning_level": "high"},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	reasoning, ok := model.ModelConfig.Reasoning.(map[string]string)
	if !ok {
		t.Fatalf("Reasoning 类型 = %T", model.ModelConfig.Reasoning)
	}
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning = %v", reasoning)
	}
}

// TestBuildModelFromEntry_endpointProfile落位 显式值必须进 EndpointProfile 字段。
func TestBuildModelFromEntry_endpointProfile落位(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{
			"model_name": "m", "api_key": "sk-test",
			"api_base":         "https://gw.example.com/v1",
			"endpoint_profile": "dashscope",
		},
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if got := model.ClientConfig.EndpointProfile; got != "dashscope" {
		t.Errorf("EndpointProfile = %q, 期望 dashscope", got)
	}
	if _, leaked := model.ClientConfig.Extra["endpoint_profile"]; leaked {
		t.Error("endpoint_profile 不应落 Extra（ProviderKey 读不到）")
	}
}

// TestBuildModelFromEntry_agentos迁移 _source=agentos 时 max_tokens 迁到 context_window。
func TestBuildModelFromEntry_agentos迁移(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{"model_name": "m", "api_key": "sk-test", "api_base": "https://api.openai.com/v1"},
		map[string]any{"_source": "agentos", "max_tokens": 128000},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	cfg := model.ModelConfig
	if cfg.ContextWindow == nil || *cfg.ContextWindow != 128000 {
		t.Errorf("ContextWindow = %v, 期望 128000", cfg.ContextWindow)
	}
	if cfg.MaxTokens != nil {
		t.Errorf("MaxTokens 应为 nil（已迁走），实际 %d", *cfg.MaxTokens)
	}
}

// TestBuildModelFromEntry_clientProvider默认 缺失时回填 OpenAI。
func TestBuildModelFromEntry_clientProvider默认(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{"model_name": "gpt-4", "api_key": "sk-test", "api_base": "https://api.openai.com/v1"},
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if got := model.ClientConfig.ClientProvider; got != "OpenAI" {
		t.Errorf("Provider = %q, 期望 OpenAI", got)
	}
}

// TestBuildModelFromEntry_Kimi采样强制 Kimi 端点必须 temperature=1.0/top_p=0.95。
func TestBuildModelFromEntry_Kimi采样强制(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{
			"model_name": "kimi-k2",
			"api_base":   "https://api.moonshot.cn/v1",
			"api_key":    "sk-test",
		},
		map[string]any{"temperature": 0.3, "top_p": 0.5},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	cfg := model.ModelConfig
	if cfg.Temperature == nil || *cfg.Temperature != 1.0 {
		t.Errorf("Temperature = %v, 期望 1.0（Kimi 强制）", cfg.Temperature)
	}
	if cfg.TopP == nil || *cfg.TopP != 0.95 {
		t.Errorf("TopP = %v, 期望 0.95", cfg.TopP)
	}
}

// TestBuildModelFromEntry_默认温度 未配置时用 0.95。
func TestBuildModelFromEntry_默认温度(t *testing.T) {
	model, err := BuildModelFromEntry(
		map[string]any{"model_name": "gpt-4", "api_key": "sk-test", "api_base": "https://api.openai.com/v1"},
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if model.ModelConfig.Temperature == nil || *model.ModelConfig.Temperature != 0.95 {
		t.Errorf("Temperature = %v, 期望默认 0.95", model.ModelConfig.Temperature)
	}
}
