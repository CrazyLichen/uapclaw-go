package reasoning

import (
	"testing"
)

// TestReasoningProfiles_条数与上游一致 对齐上游表规模。
// Python: reasoning_profiles.py:97-298 REASONING_PROFILES（42 条）
func TestReasoningProfiles_条数与上游一致(t *testing.T) {
	if len(reasoningProfiles) != 42 {
		t.Fatalf("reasoningProfiles = %d 条, want 42", len(reasoningProfiles))
	}
}

// TestModelReasoningFallbacks_条数与上游一致 对齐上游表规模。
// Python: reasoning_profiles.py:305-356 MODEL_REASONING_FALLBACKS（23 条）
func TestModelReasoningFallbacks_条数与上游一致(t *testing.T) {
	if len(modelReasoningFallbacks) != 23 {
		t.Fatalf("modelReasoningFallbacks = %d 条, want 23", len(modelReasoningFallbacks))
	}
}

// TestProviderCaps_十五个能力常量 对齐上游模块级能力定义。
// Python: reasoning_profiles.py:41-91（TOGGLE/ALWAYS_ON/UNSUPPORTED/... 共 15 个）
func TestProviderCaps_十五个能力常量(t *testing.T) {
	cases := []struct {
		name      string
		cap       ReasoningCapability
		options   []string
		recommend string
	}{
		{"toggle", toggleCapability, []string{"off", "on"}, "on"},
		{"alwaysOn", alwaysOnCapability, nil, ""},
		{"unsupported", unsupportedCapability, nil, ""},
		{"anthropicManualBudget", anthropicManualBudgetCapability, []string{"off", "low", "medium", "high"}, "medium"},
		{"deepseekV4", deepseekV4Capability, []string{"off", "low", "high", "max"}, "high"},
		{"glm52", glm52Capability, []string{"off", "high", "max"}, "max"},
		{"qwen38", qwen38Capability, []string{"off", "low", "medium", "xhigh"}, "xhigh"},
		{"kimiK3", kimiK3Capability, []string{"low", "high", "max"}, "high"},
		{"volcengineSeed20", volcengineSeed20Capability, []string{"off", "low", "medium", "high"}, "medium"},
		{"openaiGPT56", openaiGPT56Capability, []string{"off", "low", "medium", "high", "xhigh", "max"}, "medium"},
		{"openaiGPT55", openaiGPT55Capability, []string{"off", "low", "medium", "high", "xhigh"}, "medium"},
		{"openaiGPT51To54", openaiGPT51To54Capability, []string{"off", "low", "medium", "high", "xhigh"}, "off"},
		{"openaiGPT5", openaiGPT5Capability, []string{"minimal", "low", "medium", "high"}, "medium"},
		{"standardOpenAI", standardOpenAICapability, []string{"off", "low", "medium", "high"}, ""},
		{"standardAnthropic", standardAnthropicCapability, []string{"off", "low", "medium", "high", "max"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.cap.Options) != len(c.options) {
				t.Fatalf("Options = %v, want %v", c.cap.Options, c.options)
			}
			for i := range c.options {
				if c.cap.Options[i] != c.options[i] {
					t.Errorf("Options[%d] = %q, want %q", i, c.cap.Options[i], c.options[i])
				}
			}
			if c.recommend == "" {
				if c.cap.Recommended != nil {
					t.Errorf("Recommended = %q, want nil", *c.cap.Recommended)
				}
			} else if c.cap.Recommended == nil || *c.cap.Recommended != c.recommend {
				t.Errorf("Recommended = %v, want %q", c.cap.Recommended, c.recommend)
			}
		})
	}
}

// TestFindReasoningProfile_provider作用域优先 同模型名在不同 provider 下命不同条目。
// Python: reasoning_profiles.py:367-383 find_reasoning_profile
func TestFindReasoningProfile_provider作用域优先(t *testing.T) {
	// glm-5.2 在 dashscope 走 dashscope_glm_anthropic，在 zhipu 走 glm_anthropic
	ds := FindReasoningProfile("dashscope", "glm-5.2")
	if ds == nil || ds.AnthropicWire != "dashscope_glm_anthropic" {
		t.Fatalf("dashscope glm-5.2 = %+v", ds)
	}
	zp := FindReasoningProfile("zhipu", "glm-5.2")
	if zp == nil || zp.AnthropicWire != "glm_anthropic" {
		t.Fatalf("zhipu glm-5.2 = %+v", zp)
	}
}

// TestFindReasoningProfile_通配符 验证 fnmatch 语义（* 跨 /）。
// Python: reasoning_profiles.py:373 fnmatchcase
func TestFindReasoningProfile_通配符(t *testing.T) {
	// pattern "openai/gpt-5.6-*" 含斜杠，model_key 也含斜杠
	if p := FindReasoningProfile("openrouter", "openai/gpt-5.6-mini"); p == nil {
		t.Fatal("openrouter openai/gpt-5.6-mini 未命中")
	}
	// "qwen3-vl-*-thinking*" 中间通配
	if p := FindReasoningProfile("dashscope", "qwen3-vl-plus-thinking"); p == nil {
		t.Fatal("qwen3-vl-plus-thinking 未命中")
	}
}

// TestFindReasoningProfile_模型名先转小写 验证大小写不敏感来自 model_key 归一，
// 而非 fnmatch 折叠：上游 model_key = str(model).strip().lower() 之后才 fnmatchcase。
// Python: reasoning_profiles.py:369
func TestFindReasoningProfile_模型名先转小写(t *testing.T) {
	if p := FindReasoningProfile("openai", "GPT-5.6"); p == nil {
		t.Fatal("大写 GPT-5.6 应命中（上游先 lower 再匹配）")
	}
}

// TestFindReasoningProfile_fallback按裸模型名 验证 fallback 表用 rsplit("/") 后的裸名再试。
// Python: reasoning_profiles.py:375-382（fallback_model_keys = {model_key, 裸名}）
func TestFindReasoningProfile_fallback按裸模型名(t *testing.T) {
	// 未知 provider + 带 vendor 前缀的模型名 → 用裸名命中 fallback
	p := FindReasoningProfile("unknown-vendor", "openai/gpt-5.6-mini")
	if p == nil {
		t.Fatal("应通过裸名 gpt-5.6-mini 命中 fallback")
	}
	if p.Provider != "*" {
		t.Errorf("应命中 fallback 表（Provider=\"*\"），got %q", p.Provider)
	}
}

// TestFindReasoningProfile_未知名返回nil 验证无匹配不误命中。
func TestFindReasoningProfile_未知名返回nil(t *testing.T) {
	if p := FindReasoningProfile("no-such-provider", "no-such-model-zzz"); p != nil {
		t.Fatalf("got %+v, want nil", p)
	}
}

// TestFindReasoningProfile_provider名归一 验证下划线转横线、大小写不敏感。
// Python: reasoning_profiles.py:368 provider_key = provider.lower().replace("_","-")
func TestFindReasoningProfile_provider名归一(t *testing.T) {
	for _, name := range []string{"zhipu", "Zhipu", "ZHIPU"} {
		if p := FindReasoningProfile(name, "glm-5.2"); p == nil {
			t.Errorf("provider=%q 未命中", name)
		}
	}
}

// TestStandardReasoningCapability_按协议分流 验证协议默认能力。
// Python: reasoning_profiles.py:386-389
func TestStandardReasoningCapability_按协议分流(t *testing.T) {
	if c := StandardReasoningCapability("anthropic_messages"); len(c.Options) != 5 {
		t.Errorf("anthropic options = %v, want 5 项", c.Options)
	}
	if c := StandardReasoningCapability("chat_completions"); len(c.Options) != 4 {
		t.Errorf("openai options = %v, want 4 项", c.Options)
	}
}

// TestProfileReasoningCapability_anthropic独立能力 验证 AnthropicCapability 优先。
// Python: reasoning_profiles.py:391-395
func TestProfileReasoningCapability_anthropic独立能力(t *testing.T) {
	p := &ReasoningProfile{
		Capability:          ReasoningCapability{Options: []string{"off"}},
		AnthropicCapability: &ReasoningCapability{Options: []string{"off", "max"}},
	}
	if c := ProfileReasoningCapability(p, "anthropic_messages"); len(c.Options) != 2 {
		t.Errorf("anthropic = %v, want 2 项", c.Options)
	}
	if c := ProfileReasoningCapability(p, "chat_completions"); len(c.Options) != 1 {
		t.Errorf("openai = %v, want 1 项", c.Options)
	}
	if c := ProfileReasoningCapability(nil, "chat_completions"); len(c.Options) != 4 {
		t.Errorf("nil profile 应回落协议默认，got %v", c.Options)
	}
}
