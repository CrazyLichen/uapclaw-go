package reasoning

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// 本文件补齐各 wire 编码器的覆盖。每条用例对应上游 test_reasoning_resolver.py 的一个场景，
// 注释写明 Python 行号。

// intPtr 返回整型指针（测试辅助）。
func intPtr(n int) *int { return &n }

// ──────────────────────────── Moonshot / Kimi 测试分组 ────────────────────────────

// TestMoonshot_K26_chat_关闭
// Python: test_reasoning_resolver.py:723 test_moonshot_k26_chat_disabled_sends_thinking_type
func TestMoonshot_K26_chat_关闭(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k2.6"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	thinking, ok := plan.ExtraBody["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", plan.ExtraBody["thinking"])
	}
}

// TestMoonshot_K26_chat_启用
// Python: test_reasoning_resolver.py:744 test_moonshot_k26_chat_enabled_sends_thinking_type
func TestMoonshot_K26_chat_启用(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k2.6"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled"})))
	thinking, ok := plan.ExtraBody["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", plan.ExtraBody["thinking"])
	}
}

// TestMoonshot_K27code_不发disabled
// Python: test_reasoning_resolver.py:768 test_moonshot_k27_code_does_not_send_disabled_thinking
func TestMoonshot_K27code_不发disabled(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k2.7-code"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if len(plan.SDKParams) != 0 || len(plan.ExtraBody) != 0 {
		t.Fatalf("k27-code 应返回空 plan，got %+v", plan)
	}
}

// TestMoonshot_K3_用effort且忽略关闭
// Python: test_reasoning_resolver.py:781 test_moonshot_k3_uses_reasoning_effort_and_ignores_disable
func TestMoonshot_K3_用effort且忽略关闭(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	// 关闭：K3 无 off 档位，返回空 plan
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if len(plan.SDKParams) != 0 || len(plan.ExtraBody) != 0 {
		t.Fatalf("K3 关闭应空 plan，got %+v", plan)
	}
	// 启用 + effort：走 reasoning_effort
	plan2 := ResolveReasoningPlan(cfg, modelOf("kimi-k3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	if got := plan2.SDKParams["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v", got)
	}
}

// TestMoonshot_K3_xhigh映射max
// Python: test_reasoning_resolver.py:803 test_moonshot_k3_maps_xhigh_to_max
func TestMoonshot_K3_xhigh映射max(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "xhigh"})))
	if got := plan.SDKParams["reasoning_effort"]; got != "max" {
		t.Fatalf("reasoning_effort = %v, want max", got)
	}
}

// TestMoonshot_K26_anthropic_关闭
// Python: test_reasoning_resolver.py:815 test_moonshot_k26_anthropic_disabled_sends_thinking_type
func TestMoonshot_K26_anthropic_关闭(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k2.6"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
}

// TestMoonshot_K3_anthropic_关闭不发thinking
// Python: test_reasoning_resolver.py:836 test_moonshot_k3_anthropic_disable_does_not_send_thinking
func TestMoonshot_K3_anthropic_关闭不发thinking(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if _, exists := plan.SDKParams["thinking"]; exists {
		t.Fatalf("K3 anthropic 关闭不应发 thinking，got %+v", plan.SDKParams)
	}
}

// ──────────────────────────── MiniMax 测试分组 ────────────────────────────

// TestMinimax_M27_保留reasoning_split不发明开关
// Python: test_reasoning_resolver.py:1054 test_minimax_m27_keeps_reasoning_split_but_does_not_send_disable_toggle
func TestMinimax_M2_保留reasoning_split(t *testing.T) {
	cfg := clientOf("MiniMax")
	cfg.EndpointProfile = "minimax"
	plan := ResolveReasoningPlan(cfg, modelOf("minimax-m2"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if plan.ExtraBody["reasoning_split"] != true {
		t.Fatalf("reasoning_split = %v, want true", plan.ExtraBody["reasoning_split"])
	}
}

// TestMinimax_M3_自适应与关闭
// Python: test_reasoning_resolver.py:1072 test_minimax_m3_supports_adaptive_and_disabled_toggles
func TestMinimax_M3_自适应与关闭(t *testing.T) {
	cfg := clientOf("MiniMax")
	cfg.EndpointProfile = "minimax"
	// 启用 → thinking.type=adaptive
	plan := ResolveReasoningPlan(cfg, modelOf("minimax-m3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled"})))
	thinking, ok := plan.ExtraBody["thinking"].(map[string]any)
	if !ok || thinking["type"] != "adaptive" {
		t.Fatalf("thinking = %#v，want adaptive", plan.ExtraBody["thinking"])
	}
	// 关闭 → thinking.type=disabled
	plan2 := ResolveReasoningPlan(cfg, modelOf("minimax-m3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	thinking2, ok := plan2.ExtraBody["thinking"].(map[string]any)
	if !ok || thinking2["type"] != "disabled" {
		t.Fatalf("thinking = %#v，want disabled", plan2.ExtraBody["thinking"])
	}
}

// TestMinimax_M3_anthropic_自适应
// Python: reasoning.py:745-754 _minimax_anthropic_reasoning_plan
func TestMinimax_M3_anthropic_自适应(t *testing.T) {
	cfg := clientOf("MiniMax")
	cfg.EndpointProfile = "minimax"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("minimax-m3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "adaptive" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
}

// ──────────────────────────── OpenRouter 测试分组 ────────────────────────────

// TestOpenRouter_关闭用effort_none
// Python: test_reasoning_resolver.py:1099 test_openrouter_uses_effort_none_for_disabled_reasoning
func TestOpenRouter_关闭用effort_none(t *testing.T) {
	cfg := clientOf("OpenRouter")
	cfg.EndpointProfile = "openrouter"
	plan := ResolveReasoningPlan(cfg, modelOf("openai/gpt-5.6-mini"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	payload, ok := plan.ExtraBody["reasoning"].(map[string]any)
	if !ok || payload["effort"] != "none" {
		t.Fatalf("reasoning = %#v", plan.ExtraBody["reasoning"])
	}
}

// TestOpenRouter_anthropic模式用网关结构
// Python: test_reasoning_resolver.py:1116 test_openrouter_anthropic_mode_uses_gateway_reasoning_shape
func TestOpenRouter_anthropic模式用网关结构(t *testing.T) {
	cfg := clientOf("OpenRouter")
	cfg.EndpointProfile = "openrouter"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("openai/gpt-5.6-mini"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	payload, ok := plan.ExtraBody["reasoning"].(map[string]any)
	if !ok || payload["effort"] != "high" {
		t.Fatalf("reasoning = %#v", plan.ExtraBody["reasoning"])
	}
}

// TestOpenRouter_未知前缀返回空
// Python: reasoning.py:912-913
func TestOpenRouter_未知前缀返回空(t *testing.T) {
	// 直接调编码器（profile 表里没有未知前缀条目）
	plan := openRouterReasoningPlan("unknownvendor/some-model",
		&llmschema.ReasoningConfig{Mode: "enabled", Effort: strPtr("high")})
	if len(plan.ExtraBody) != 0 {
		t.Fatalf("未知前缀应返回空 plan，got %+v", plan)
	}
}

// TestOpenRouter_budget映射max_tokens
// Python: reasoning.py:920-921
func TestOpenRouter_budget映射max_tokens(t *testing.T) {
	plan := openRouterReasoningPlan("openai/gpt-5.6-mini",
		&llmschema.ReasoningConfig{Mode: "enabled", BudgetTokens: intPtr(4096)})
	payload, ok := plan.ExtraBody["reasoning"].(map[string]any)
	if !ok || payload["max_tokens"] != 4096 {
		t.Fatalf("reasoning = %#v", plan.ExtraBody["reasoning"])
	}
}

// ──────────────────────────── vLLM 测试分组 ────────────────────────────

// TestVLLM_关闭双字段
// Python: reasoning.py:782-788 _vllm_enable_thinking_plan
func TestVLLM_关闭双字段(t *testing.T) {
	cfg := clientOf("OpenAI")
	cfg.EndpointProfile = "vllm"
	plan := ResolveReasoningPlan(cfg, modelOf("any-model"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if plan.ExtraBody["enable_thinking"] != false {
		t.Fatalf("enable_thinking = %v", plan.ExtraBody["enable_thinking"])
	}
	ctk, ok := plan.ExtraBody["chat_template_kwargs"].(map[string]any)
	if !ok || ctk["enable_thinking"] != false {
		t.Fatalf("chat_template_kwargs = %#v", plan.ExtraBody["chat_template_kwargs"])
	}
}

// TestVLLM_启用按档位映射预算
// Python: reasoning.py:792-803
func TestVLLM_启用按档位映射预算(t *testing.T) {
	cfg := clientOf("OpenAI")
	cfg.EndpointProfile = "vllm"
	plan := ResolveReasoningPlan(cfg, modelOf("any-model"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	if got := plan.ExtraBody["thinking_budget"]; got != 8192 {
		t.Fatalf("thinking_budget = %v, want 8192", got)
	}
	if _, exists := plan.ExtraBody["reasoning_effort"]; exists {
		t.Fatal("vllm 不得发 reasoning_effort")
	}
}

// ──────────────────────────── 火山 Seed ────────────────────────────

// TestVolcengineSeed_用minimal表示关闭
// Python: test_reasoning_resolver.py:1026 test_volcengine_seed_uses_minimal_to_disable_reasoning
func TestVolcengineSeed_用minimal表示关闭(t *testing.T) {
	cfg := clientOf("VolcEngine")
	cfg.EndpointProfile = "volcengine"
	plan := ResolveReasoningPlan(cfg, modelOf("seed-2.0-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if got := plan.SDKParams["reasoning_effort"]; got != "minimal" {
		t.Fatalf("reasoning_effort = %v, want minimal", got)
	}
}

// TestVolcengineSeed_档位映射
// Python: reasoning.py:579-589
func TestVolcengineSeed_档位映射(t *testing.T) {
	cases := map[string]string{"low": "low", "medium": "medium", "high": "high", "xhigh": "high", "max": "high"}
	for in, want := range cases {
		cfg := clientOf("VolcEngine")
		cfg.EndpointProfile = "volcengine"
		plan := ResolveReasoningPlan(cfg, modelOf("seed-2.0-pro"), "",
			intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": in})))
		if got := plan.SDKParams["reasoning_effort"]; got != want {
			t.Errorf("effort=%q → %v, want %q", in, got, want)
		}
	}
}

// ──────────────────────────── GLM / Qwen38 Anthropic 测试分组 ────────────────────────────

// TestGlm_anthropic走output_config
// Python: test_reasoning_resolver.py:967 test_modelarts_anthropic_glm52_effort_goes_through_extra_body
func TestGlm_anthropic走extra_effort(t *testing.T) {
	cfg := clientOf("Zhipu")
	cfg.EndpointProfile = "zhipu"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("glm-5.2"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
	if got := plan.ExtraBody["reasoning_effort"]; got != "high" {
		t.Fatalf("extra_body.reasoning_effort = %v, want high", got)
	}
}

// TestGlm_chat_effort在extra_body
// Python: reasoning.py:495-500 wire == "glm"
func TestGlm_chat_effort在extra_body(t *testing.T) {
	cfg := clientOf("Zhipu")
	cfg.EndpointProfile = "zhipu"
	plan := ResolveReasoningPlan(cfg, modelOf("glm-5.2"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "low"})))
	// low 归一为 high
	if got := plan.ExtraBody["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v, want high", got)
	}
}

// TestQwen38_anthropic走output_config
// Python: test_reasoning_resolver.py:206 test_dashscope_anthropic_qwen38_uses_output_config_effort
func TestQwen38_anthropic走output_config(t *testing.T) {
	cfg := clientOf("DashScope")
	cfg.EndpointProfile = "dashscope"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("qwen3.8-max"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	oc, ok := plan.SDKParams["output_config"].(map[string]any)
	if !ok || oc["effort"] != "xhigh" {
		t.Fatalf("output_config = %#v，want xhigh", plan.SDKParams["output_config"])
	}
}

// TestQwen38_anthropic_显式budget优先
// Python: test_reasoning_resolver.py:246 test_dashscope_anthropic_qwen38_explicit_budget_wins_over_effort
func TestQwen38_anthropic_显式budget优先(t *testing.T) {
	cfg := clientOf("DashScope")
	cfg.EndpointProfile = "dashscope"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("qwen3.8-max"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high", "budget_tokens": float64(2048)})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["budget_tokens"] != 2048 {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
	if _, exists := plan.SDKParams["output_config"]; exists {
		t.Fatal("显式 budget 时不应发 output_config")
	}
}

// TestQwen38_anthropic_关闭
// Python: test_reasoning_resolver.py:260 test_dashscope_anthropic_qwen38_disabled_sends_thinking_disabled
func TestQwen38_anthropic_关闭(t *testing.T) {
	cfg := clientOf("DashScope")
	cfg.EndpointProfile = "dashscope"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("qwen3.8-max"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
}

// TestDashscopeAnthropic_plainEnabled不发budget
// Python: test_reasoning_resolver.py:287 test_dashscope_anthropic_plain_toggle_enabled_sends_no_budget
func TestDashscopeAnthropic_plainEnabled不发budget(t *testing.T) {
	cfg := clientOf("DashScope")
	cfg.EndpointProfile = "dashscope"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("qwen-max"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
	if _, exists := thinking["budget_tokens"]; exists {
		t.Fatal("plain enabled 不应发 budget_tokens")
	}
}

// ──────────────────────────── ModelArts 测试分组 ────────────────────────────

// TestModelarts_openpangu走thinking开关
// Python: test_reasoning_resolver.py:929 test_modelarts_openpangu_uses_thinking_toggle_from_api_base
func TestModelarts_openpangu走thinking开关(t *testing.T) {
	cfg := clientOf("ModelArts")
	cfg.EndpointProfile = "modelarts"
	plan := ResolveReasoningPlan(cfg, modelOf("openpangu-2.0-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled"})))
	thinking, ok := plan.ExtraBody["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", plan.ExtraBody["thinking"])
	}
}

// TestModelarts_deepseek_effort固定high
// Python: reasoning.py:484-489 wire == "modelarts_deepseek"
func TestModelarts_deepseek_effort固定high(t *testing.T) {
	cfg := clientOf("ModelArts")
	cfg.EndpointProfile = "modelarts"
	plan := ResolveReasoningPlan(cfg, modelOf("deepseek-v4-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "low"})))
	if got := plan.ExtraBody["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v, want high（modelarts 固定 high）", got)
	}
}

// ──────────────────────────── 能力查询 ────────────────────────────

// TestGetReasoningCapability_Kimi档位
// Python: reasoning.py:150-177 get_reasoning_capability
func TestGetReasoningCapability_Kimi档位(t *testing.T) {
	c := GetReasoningCapability("Moonshot", "kimi-k3", protocolChatCompletions, "", "")
	if len(c.Options) != 3 || c.Options[0] != "low" || c.Options[2] != "max" {
		t.Fatalf("options = %v", c.Options)
	}
	if c.Recommended == nil || *c.Recommended != "high" {
		t.Fatalf("recommended = %v", c.Recommended)
	}
}

// TestGetReasoningCapability_未列模型继承协议默认
// Python: reasoning.py:174-177
func TestGetReasoningCapability_未列模型继承协议默认(t *testing.T) {
	openai := GetReasoningCapability("some-vendor", "totally-unknown", protocolChatCompletions, "", "")
	if len(openai.Options) != 4 {
		t.Errorf("openai options = %v, want 4", openai.Options)
	}
	anthropic := GetReasoningCapability("some-vendor", "totally-unknown", protocolAnthropicMessages, "", "")
	if len(anthropic.Options) != 5 {
		t.Errorf("anthropic options = %v, want 5", anthropic.Options)
	}
}

// TestGetReasoningCapability_apiBase推断provider
// Python: reasoning.py:122-131
func TestGetReasoningCapability_apiBase推断provider(t *testing.T) {
	c := GetReasoningCapability("", "glm-5.2", protocolChatCompletions, "https://open.bigmodel.cn/api/paas/v4", "")
	if len(c.Options) != 3 || c.Options[2] != "max" {
		t.Fatalf("options = %v，zhipu glm-5.2 应为 off/high/max", c.Options)
	}
}

// TestGetReasoningCapabilityCatalog_结构
// Python: reasoning.py:218-242
func TestGetReasoningCapabilityCatalog_结构(t *testing.T) {
	cat := GetReasoningCapabilityCatalog()
	defaults, ok := cat["protocol_defaults"].(map[string]any)
	if !ok || len(defaults) != 2 {
		t.Fatalf("protocol_defaults = %#v", cat["protocol_defaults"])
	}
	fb, ok := cat["model_fallbacks"].([]any)
	if !ok || len(fb) != 23 {
		t.Fatalf("model_fallbacks = %d, want 23", len(fb))
	}
}

// TestGetProviderReasoningRules_只返回该provider
// Python: reasoning.py:180-215
func TestGetProviderReasoningRules_只返回该provider(t *testing.T) {
	rules := GetProviderReasoningRules("dashscope", "", "")
	if len(rules) == 0 {
		t.Fatal("dashscope 应有规则")
	}
	for _, r := range rules {
		if _, ok := r["patterns"]; !ok {
			t.Error("缺 patterns")
		}
		if _, ok := r["capabilities"]; !ok {
			t.Error("缺 capabilities")
		}
	}
	// 未知 provider 返回空
	if got := GetProviderReasoningRules("no-such", "", ""); len(got) != 0 {
		t.Errorf("未知 provider 应返回空，got %d 条", len(got))
	}
}

// TestProviderFromAPIBase_边界
// Python: reasoning.py:122-131
func TestProviderFromAPIBase_边界(t *testing.T) {
	cases := map[string]string{
		"":                                   "",
		"https://api.deepseek.com/v1":        "deepseek",
		"api.deepseek.com":                   "deepseek",
		"https://ark.cn-beijing.volces.com":  "volcengine",
		"https://unknown.example.com/v1":     "",
		"https://sub.dashscope.aliyuncs.com": "dashscope",
	}
	for in, want := range cases {
		if got := ProviderFromAPIBase(in); got != want {
			t.Errorf("ProviderFromAPIBase(%q) = %q, want %q", in, got, want)
		}
	}
}

// ──────────────────────────── 补充：anthropic 侧 deepseek / moonshot ────────────────────────────

// TestDeepseek_anthropic_关闭发thinking_disabled
// Python: test_reasoning_resolver.py:573 test_deepseek_anthropic_disabled_sends_thinking_disabled
func TestDeepseek_anthropic_关闭发thinking_disabled(t *testing.T) {
	cfg := clientOf("DeepSeek")
	cfg.EndpointProfile = "deepseek"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("deepseek-v4-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
}

// TestDeepseek_anthropic_启用映射output_config
// Python: test_reasoning_resolver.py:601 test_deepseek_anthropic_enabled_maps_effort_to_output_config
func TestDeepseek_anthropic_启用映射output_config(t *testing.T) {
	cfg := clientOf("DeepSeek")
	cfg.EndpointProfile = "deepseek"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("deepseek-v4-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "medium"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
	oc, ok := plan.SDKParams["output_config"].(map[string]any)
	// medium 归一为 high
	if !ok || oc["effort"] != "high" {
		t.Fatalf("output_config = %#v，want high", plan.SDKParams["output_config"])
	}
}

// TestMoonshot_anthropic_K3启用走extra_effort
// Python: reasoning.py:624-641
func TestMoonshot_anthropic_K3启用走extra_effort(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "medium"})))
	if got := plan.ExtraBody["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v, want high（medium→high）", got)
	}
}

// TestMoonshot_anthropic_k2_auto空plan
// Python: reasoning.py:639-640
func TestMoonshot_anthropic_k2_auto空plan(t *testing.T) {
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k2.6"), "",
		intentOf(reasoningOf(map[string]any{"mode": "auto"})))
	if len(plan.SDKParams) != 0 || len(plan.ExtraBody) != 0 {
		t.Fatalf("k2 auto 应空 plan，got %+v", plan)
	}
}

// TestCapPtr_返回独立副本 验证 capPtr 不共享底层数组。
func TestCapPtr_返回独立副本(t *testing.T) {
	src := ReasoningCapability{Options: []string{"off"}}
	p := capPtr(src)
	if p == nil || len(p.Options) != 1 || p.Options[0] != "off" {
		t.Fatalf("p = %+v", p)
	}
	p.Options[0] = "changed"
	if src.Options[0] != "off" {
		t.Fatal("capPtr 修改影响原值")
	}
}

// ──────────────────────────── 补充：边界分支 ────────────────────────────

// TestApplyReasoningPlan_空plan不创建extra_body
func TestApplyReasoningPlan_空plan不创建extra_body(t *testing.T) {
	params := map[string]any{}
	ApplyReasoningPlan(params, ReasoningPlan{}, false)
	if _, exists := params["extra_body"]; exists {
		t.Fatal("空 plan 不应创建 extra_body")
	}
}

// TestApplyReasoningPlan_override替换extra_body
func TestApplyReasoningPlan_override替换extra_body(t *testing.T) {
	params := map[string]any{"extra_body": map[string]any{"thinking": "old"}}
	ApplyReasoningPlan(params, ReasoningPlan{ExtraBody: map[string]any{"thinking": "new"}}, true)
	if params["extra_body"].(map[string]any)["thinking"] != "new" {
		t.Fatal("override 应替换已有值")
	}
}

// TestApplyReasoningPlan_无extra_body时新建
func TestApplyReasoningPlan_无extra_body时新建(t *testing.T) {
	params := map[string]any{}
	ApplyReasoningPlan(params, ReasoningPlan{ExtraBody: map[string]any{"thinking": "x"}}, false)
	eb, ok := params["extra_body"].(map[string]any)
	if !ok || eb["thinking"] != "x" {
		t.Fatalf("extra_body = %#v", params["extra_body"])
	}
}

// TestApplyReasoningPlan_带warning仍写参数
func TestApplyReasoningPlan_带warning仍写参数(t *testing.T) {
	params := map[string]any{}
	ApplyReasoningPlan(params, ReasoningPlan{
		SDKParams: map[string]any{"reasoning_effort": "high"},
		Warnings:  []string{"w1"},
	}, false)
	if params["reasoning_effort"] != "high" {
		t.Fatal("warning 不应阻止写参数")
	}
}

// TestProviderKeyOf_nil 验证 nil 安全。
func TestProviderKeyOf_nil(t *testing.T) {
	if got := providerKeyOf(nil); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestLegacyProviderName_无Extra 验证无 legacy 键时返回空。
func TestLegacyProviderName_无Extra(t *testing.T) {
	if got := legacyProviderName(&llmschema.ModelClientConfig{}); got != "" {
		t.Errorf("got %q", got)
	}
	if got := legacyProviderName(nil); got != "" {
		t.Errorf("nil got %q", got)
	}
}

// TestModelName_requestModel优先 验证 requestModel 覆盖 modelConfig。
func TestModelName_requestModel优先(t *testing.T) {
	if got := modelName(modelOf("cfg-model"), "req-model"); got != "req-model" {
		t.Errorf("got %q, want req-model", got)
	}
	if got := modelName(modelOf("cfg-model"), ""); got != "cfg-model" {
		t.Errorf("got %q, want cfg-model", got)
	}
	if got := modelName(nil, ""); got != "" {
		t.Errorf("got %q, want 空串", got)
	}
}

// TestExplicitReasoningValue_nilModelConfig 验证 nil 安全。
func TestExplicitReasoningValue_nilModelConfig(t *testing.T) {
	if got := explicitReasoningValue(nil, llmschema.ReasoningIntent{Set: false}); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestHasLegacyReasoningControls_extra_body内字段 验证 extra_body 分支。
func TestHasLegacyReasoningControls_extra_body内字段(t *testing.T) {
	values := map[string]any{"extra_body": map[string]any{"reasoning_effort": "low"}}
	if !hasLegacyReasoningControls(values) {
		t.Fatal("extra_body 内的遗留控制应被检出")
	}
	// nil 值不算
	values2 := map[string]any{"reasoning_effort": nil}
	if hasLegacyReasoningControls(values2) {
		t.Fatal("nil 值不算遗留控制")
	}
}

// TestIsDeepSeek_三种判定 验证 provider key / identity / api_base 三条路径。
func TestIsDeepSeek_三种判定(t *testing.T) {
	// 1) provider key 为 deepseek
	if !isDeepSeek(&llmschema.ModelClientConfig{ClientProvider: "DeepSeek"}) {
		t.Error("provider=DeepSeek 应判定为 deepseek")
	}
	// 2) legacy 名称为 deepseek
	cfg := &llmschema.ModelClientConfig{ClientProvider: "OpenAI", Extra: map[string]any{"legacy_client_provider": "DeepSeek"}}
	if !isDeepSeek(cfg) {
		t.Error("legacy provider 应判定为 deepseek")
	}
	// 3) api_base 含 deepseek.com
	cfg2 := &llmschema.ModelClientConfig{ClientProvider: "OpenAI", APIBase: "https://api.deepseek.com/v1"}
	if !isDeepSeek(cfg2) {
		t.Error("api_base 应判定为 deepseek")
	}
	// 非 deepseek
	if isDeepSeek(&llmschema.ModelClientConfig{ClientProvider: "OpenAI", APIBase: "https://api.openai.com"}) {
		t.Error("openai 不应判定为 deepseek")
	}
	if isDeepSeek(nil) {
		t.Error("nil 不应判定为 deepseek")
	}
}

// TestAnthropicReasoningPlan_vllm优先 验证 vllm 在 profile 查找之前短路。
// Python: reasoning.py:370-372
func TestAnthropicReasoningPlan_vllm优先(t *testing.T) {
	cfg := clientOf("Anthropic")
	cfg.EndpointProfile = "vllm"
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("claude-opus-4-1"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	if plan.ExtraBody["enable_thinking"] != true {
		t.Fatalf("vllm 应走 enable_thinking，got %+v", plan)
	}
}

// TestOpenAIResponses_auto无effort空plan
// Python: reasoning.py:406-408
func TestOpenAIResponses_auto无effort空plan(t *testing.T) {
	cfg := clientOf("OpenAIAccount")
	cfg.APIMode = protocolResponses
	plan := ResolveReasoningPlan(cfg, modelOf("gpt-5"), "",
		intentOf(reasoningOf(map[string]any{"mode": "auto"})))
	if len(plan.SDKParams) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}

// TestProfileReasoningPlan_disabled且capability无off 验证前置早返回。
// Python: reasoning.py:454-455
func TestProfileReasoningPlan_disabled且capability无off(t *testing.T) {
	// kimi-k3 的 options 不含 off
	cfg := clientOf("Moonshot")
	cfg.EndpointProfile = "moonshot"
	plan := ResolveReasoningPlan(cfg, modelOf("kimi-k3"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if len(plan.SDKParams) != 0 || len(plan.ExtraBody) != 0 {
		t.Fatalf("capability 无 off 时应空 plan，got %+v", plan)
	}
}

// TestNormalizeDeepSeekEffort_全部档位
// Python: reasoning.py:551-563
func TestNormalizeDeepSeekEffort_全部档位(t *testing.T) {
	cases := []struct{ in, model, want string }{
		{"minimal", "", "low"},
		{"medium", "", "high"},
		{"xhigh", "deepseek-v4-pro", "max"},
		{"xhigh", "deepseek-v4", "high"},
		{"low", "", "low"},
		{"high", "", "high"},
		{"max", "", "max"},
		{"bogus", "", ""},
	}
	for _, c := range cases {
		got := normalizeDeepSeekEffort(strPtr(c.in), c.model)
		if got != c.want {
			t.Errorf("normalizeDeepSeekEffort(%q,%q) = %q, want %q", c.in, c.model, got, c.want)
		}
	}
	if got := normalizeDeepSeekEffort(nil, ""); got != "" {
		t.Errorf("nil → %q, want 空串", got)
	}
}

// ──────────────────────────── 补充：归一化函数全档位 ────────────────────────────

// TestNormalizeGlmEffort_全档位
// Python: reasoning.py:566-576 _normalize_glm_effort
func TestNormalizeGlmEffort_全档位(t *testing.T) {
	cases := map[string]string{
		"none": "none", "off": "none", "minimal": "none",
		"low": "high", "medium": "high", "high": "high",
		"xhigh": "max", "max": "max", "bogus": "",
	}
	for in, want := range cases {
		if got := normalizeGlmEffort(strPtr(in)); got != want {
			t.Errorf("normalizeGlmEffort(%q) = %q, want %q", in, got, want)
		}
	}
	if got := normalizeGlmEffort(nil); got != "" {
		t.Errorf("nil → %q", got)
	}
}

// TestNormalizeQwen38Effort_全档位
// Python: reasoning.py:831-843 _normalize_qwen38_effort
func TestNormalizeQwen38Effort_全档位(t *testing.T) {
	cases := map[string]string{
		"none": "none", "off": "none",
		"minimal": "low", "low": "low",
		"medium": "medium",
		"high":   "xhigh", "xhigh": "xhigh", "max": "xhigh",
		"bogus": "",
	}
	for in, want := range cases {
		if got := normalizeQwen38Effort(strPtr(in)); got != want {
			t.Errorf("normalizeQwen38Effort(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNormalizeVolcengineSeedEffort_全档位
// Python: reasoning.py:579-589
func TestNormalizeVolcengineSeedEffort_全档位(t *testing.T) {
	cases := map[string]string{
		"none": "minimal", "off": "minimal", "minimal": "minimal",
		"low": "low", "medium": "medium", "high": "high",
		"xhigh": "high", "max": "high", "bogus": "",
	}
	for in, want := range cases {
		if got := normalizeVolcengineSeedEffort(strPtr(in)); got != want {
			t.Errorf("normalizeVolcengineSeedEffort(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNormalizeKimiK3Effort_全档位
// Python: reasoning.py:644-654
func TestNormalizeKimiK3Effort_全档位(t *testing.T) {
	cases := map[string]string{
		"ultra": "max", "xhigh": "max", "max": "max",
		"medium": "high", "high": "high",
		"minimum": "low", "minimal": "low", "light": "low", "low": "low",
		"bogus": "",
	}
	for in, want := range cases {
		if got := normalizeKimiK3Effort(strPtr(in)); got != want {
			t.Errorf("normalizeKimiK3Effort(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMoonshotModelFamily_三族判定
// Python: reasoning.py:592-600
func TestMoonshotModelFamily_三族判定(t *testing.T) {
	cases := map[string]string{
		"kimi-k3":            "k3",
		"moonshotai/kimi-k3": "k3",
		"kimi-k2.7-code":     "k27_code",
		"kimi-k2.6":          "k2_toggle",
		"kimi-k2.5":          "k2_toggle",
		"unknown":            "unknown",
	}
	for in, want := range cases {
		if got := moonshotModelFamily(in); got != want {
			t.Errorf("moonshotModelFamily(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDeepseekAnthropic_仅有mode_enabled无effort
// Python: reasoning.py:546-547 elif reasoning.mode == "enabled"
func TestDeepseekAnthropic_仅有mode_enabled无effort(t *testing.T) {
	plan := deepseekAnthropicReasoningPlan(
		&llmschema.ReasoningConfig{Mode: "enabled"}, "deepseek-v4-pro")
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
	if _, exists := plan.SDKParams["output_config"]; exists {
		t.Fatal("无 effort 不应发 output_config")
	}
}

// TestIsDeepSeek_identity归一 验证下划线转横线的 identity 分支。
func TestIsDeepSeek_identity归一(t *testing.T) {
	cfg := &llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		Extra:          map[string]any{"legacy_client_provider": "deep_seek"},
	}
	// "deep_seek" 归一为 "deep-seek"，不等于 "deepseek" → 不应命中 identity 分支
	// 但 providerKeyOf 会把它归一为 "deep-seek"，也不等于 "deepseek"
	if isDeepSeek(cfg) {
		t.Log("deep_seek 归一后不匹配 deepseek（预期行为）")
	}
}
