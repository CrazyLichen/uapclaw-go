package reasoning

import (
	"reflect"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// intentOf 构造显式 reasoning 意图。
func intentOf(v any) llmschema.ReasoningIntent {
	return llmschema.ReasoningIntent{Set: true, Value: v}
}

// reasoningOf 构造新式 ReasoningConfig map。
func reasoningOf(kv map[string]any) map[string]any { return kv }

// clientOf 构造最小客户端配置。
func clientOf(provider string) *llmschema.ModelClientConfig {
	return &llmschema.ModelClientConfig{ClientID: "cid", ClientProvider: provider}
}

// modelOf 构造最小请求配置。
func modelOf(name string) *llmschema.ModelRequestConfig {
	return &llmschema.ModelRequestConfig{ModelName: name}
}

// ──────────────────────────── 协议判定 ────────────────────────────

// TestResolveReasoningProtocol_三协议判定
// Python: reasoning.py:134-147
func TestResolveReasoningProtocol_三协议判定(t *testing.T) {
	cases := []struct {
		name string
		cfg  *llmschema.ModelClientConfig
		want string
	}{
		{"api_mode=anthropic_messages", &llmschema.ModelClientConfig{APIMode: protocolAnthropicMessages}, protocolAnthropicMessages},
		{"provider=Anthropic", &llmschema.ModelClientConfig{ClientProvider: "Anthropic"}, protocolAnthropicMessages},
		{"api_mode=responses", &llmschema.ModelClientConfig{APIMode: protocolResponses}, protocolResponses},
		{"auth=oauth", &llmschema.ModelClientConfig{AuthMode: llmschema.AuthModeOpenAIAccountOAuth}, protocolResponses},
		{"provider=OpenAIAccount", &llmschema.ModelClientConfig{ClientProvider: "OpenAIAccount"}, protocolResponses},
		{"默认 chat", &llmschema.ModelClientConfig{ClientProvider: "OpenAI"}, protocolChatCompletions},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveReasoningProtocol(c.cfg); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestResolveReasoningProtocol_nil 验证 nil 安全。
func TestResolveReasoningProtocol_nil(t *testing.T) {
	if got := ResolveReasoningProtocol(nil); got != protocolChatCompletions {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── 通用 OpenAI ────────────────────────────

// TestResolve_通用openai_effort直写sdk_params
// Python: test_reasoning_resolver.py:87 test_openai_chat_reasoning_effort_is_added_to_sdk_params
func TestResolve_通用openai_effort直写sdk_params(t *testing.T) {
	plan := ResolveReasoningPlan(clientOf("OpenAI"), modelOf("unknown-model"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	if got := plan.SDKParams["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v, want high", got)
	}
	if len(plan.ExtraBody) != 0 {
		t.Errorf("extra_body 应为空，got %v", plan.ExtraBody)
	}
}

// TestResolve_通用openai_关闭用none
// Python: test_reasoning_resolver.py:108 test_openai_chat_disabled_uses_current_none_effort_value
func TestResolve_通用openai_关闭用none(t *testing.T) {
	plan := ResolveReasoningPlan(clientOf("OpenAI"), modelOf("unknown-model"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if got := plan.SDKParams["reasoning_effort"]; got != "none" {
		t.Fatalf("reasoning_effort = %v, want none", got)
	}
}

// TestResolve_通用openai_auto无控制为空plan
func TestResolve_通用openai_auto无控制为空plan(t *testing.T) {
	plan := ResolveReasoningPlan(clientOf("OpenAI"), modelOf("unknown-model"), "",
		intentOf(reasoningOf(map[string]any{"mode": "auto"})))
	if len(plan.SDKParams) != 0 || len(plan.ExtraBody) != 0 {
		t.Fatalf("plan = %+v, want empty", plan)
	}
}

// ──────────────────────────── DashScope 测试分组 ────────────────────────────

// TestResolve_dashscope_qwen走extra_body
// Python: test_reasoning_resolver.py:128 test_dashscope_qwen_reasoning_uses_extra_body_not_sdk_kwargs
func TestResolve_dashscope_qwen走extra_body(t *testing.T) {
	cfg := clientOf("DashScope")
	cfg.EndpointProfile = "dashscope"
	plan := ResolveReasoningPlan(cfg, modelOf("qwen-max"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled"})))
	if got := plan.ExtraBody["enable_thinking"]; got != true {
		t.Fatalf("enable_thinking = %v, want true", got)
	}
	if _, exists := plan.SDKParams["reasoning_effort"]; exists {
		t.Fatal("dashscope 不应写 sdk_params.reasoning_effort")
	}
}

// TestResolve_dashscope_qwen38_enabled带effort
// Python: test_reasoning_resolver.py:189 test_dashscope_qwen38_maps_openai_high_effort_to_xhigh
func TestResolve_dashscope_qwen38_high映射xhigh(t *testing.T) {
	cfg := clientOf("DashScope")
	cfg.EndpointProfile = "dashscope"
	plan := ResolveReasoningPlan(cfg, modelOf("qwen3.8-max"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	if got := plan.ExtraBody["reasoning_effort"]; got != "xhigh" {
		t.Fatalf("reasoning_effort = %v, want xhigh", got)
	}
	if got := plan.ExtraBody["enable_thinking"]; got != true {
		t.Fatalf("enable_thinking = %v", got)
	}
}

// TestResolve_dashscope_qwen38_budget优先不并发effort
// Python: test_reasoning_resolver.py:159 test_dashscope_qwen38_does_not_send_effort_and_budget_together
func TestResolve_dashscope_qwen38_budget优先(t *testing.T) {
	cfg := clientOf("DashScope")
	cfg.EndpointProfile = "dashscope"
	plan := ResolveReasoningPlan(cfg, modelOf("qwen3.8-max"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high", "budget_tokens": float64(2048)})))
	if got := plan.ExtraBody["thinking_budget"]; got != 2048 {
		t.Fatalf("thinking_budget = %v, want 2048", got)
	}
	if _, exists := plan.ExtraBody["reasoning_effort"]; exists {
		t.Fatal("budget 与 effort 不得并存")
	}
}

// ──────────────────────────── DeepSeek 测试分组 ────────────────────────────

// TestResolve_deepseek_关闭用thinking_disabled
// Python: test_reasoning_resolver.py:530 test_deepseek_profile_disabled_sends_thinking_disabled_not_effort_off
func TestResolve_deepseek_关闭用thinking_disabled(t *testing.T) {
	cfg := clientOf("DeepSeek")
	cfg.EndpointProfile = "deepseek"
	plan := ResolveReasoningPlan(cfg, modelOf("deepseek-v4-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	thinking, ok := plan.ExtraBody["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", plan.ExtraBody["thinking"])
	}
	if _, exists := plan.SDKParams["reasoning_effort"]; exists {
		t.Fatal("关闭时不应发 reasoning_effort")
	}
}

// TestResolve_deepseek_启用带effort
// Python: test_reasoning_resolver.py:506 test_deepseek_profile_enabled_sends_thinking_type_and_effort
func TestResolve_deepseek_启用带effort(t *testing.T) {
	cfg := clientOf("DeepSeek")
	cfg.EndpointProfile = "deepseek"
	plan := ResolveReasoningPlan(cfg, modelOf("deepseek-v4-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	thinking, ok := plan.ExtraBody["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", plan.ExtraBody["thinking"])
	}
	if got := plan.SDKParams["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v", got)
	}
}

// TestResolve_deepseek_xhigh对pro映射max
// Python: test_reasoning_resolver.py:916 test_deepseek_v4_pro_maps_xhigh_to_max
func TestResolve_deepseek_xhigh对pro映射max(t *testing.T) {
	cfg := clientOf("DeepSeek")
	cfg.EndpointProfile = "deepseek"
	plan := ResolveReasoningPlan(cfg, modelOf("deepseek-v4-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "xhigh"})))
	if got := plan.SDKParams["reasoning_effort"]; got != "max" {
		t.Fatalf("reasoning_effort = %v, want max", got)
	}
}

// TestResolve_deepseek判定看api_base 验证聚合网关按 api_base 识别 deepseek。
// Python: reasoning.py:354-361 _is_deepseek
//
// 注意 _is_deepseek 只改 provider 名，profile 仍需模型名匹配：
// 模型名不是 deepseek-v4-* 时回落通用 reasoning_effort。
func TestResolve_deepseek判定看api_base(t *testing.T) {
	// 模型名匹配 deepseek-v4-* → 走 deepseek wire
	cfg := clientOf("OpenAI")
	cfg.APIBase = "https://api.deepseek.com/v1"
	plan := ResolveReasoningPlan(cfg, modelOf("deepseek-v4-pro"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	thinking, ok := plan.ExtraBody["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("应按 deepseek wire 编码，got %+v", plan)
	}

	// 模型名不匹配 → 回落通用 reasoning_effort
	plan2 := ResolveReasoningPlan(cfg, modelOf("some-model"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	if got := plan2.SDKParams["reasoning_effort"]; got != "none" {
		t.Fatalf("未匹配 profile 时应回落通用 effort，got %+v", plan2)
	}
}

// ──────────────────────────── Anthropic 测试分组 ────────────────────────────

// TestResolve_anthropic_启用自适应
// Python: test_reasoning_resolver.py:647 test_anthropic_enabled_maps_effort_to_output_config
func TestResolve_anthropic_启用带effort走output_config(t *testing.T) {
	cfg := clientOf("Anthropic")
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("claude-opus-4-1"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "adaptive" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
	oc, ok := plan.SDKParams["output_config"].(map[string]any)
	if !ok || oc["effort"] != "high" {
		t.Fatalf("output_config = %#v", plan.SDKParams["output_config"])
	}
}

// TestResolve_anthropic_带budget走enabled
// Python: test_reasoning_resolver.py:666 test_anthropic_enabled_without_budget_uses_adaptive_thinking
func TestResolve_anthropic_带budget走enabled(t *testing.T) {
	cfg := clientOf("Anthropic")
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("claude-opus-4-1"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "budget_tokens": float64(4096)})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" || thinking["budget_tokens"] != 4096 {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
}

// TestResolve_anthropic_auto无控制空plan
// Python: test_reasoning_resolver.py:685 test_anthropic_auto_without_controls_is_empty_plan
func TestResolve_anthropic_auto无控制空plan(t *testing.T) {
	cfg := clientOf("Anthropic")
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("claude-opus-4-1"), "",
		intentOf(reasoningOf(map[string]any{"mode": "auto"})))
	if len(plan.SDKParams) != 0 || len(plan.ExtraBody) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}

// TestResolve_anthropic_关闭
// Python: test_reasoning_resolver.py:630 test_anthropic_disabled_sends_thinking_type_disabled
func TestResolve_anthropic_关闭(t *testing.T) {
	cfg := clientOf("Anthropic")
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("claude-opus-4-1"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", plan.SDKParams["thinking"])
	}
}

// TestResolve_anthropicManualBudget_档位映射
// Python: test_reasoning_resolver.py:302 test_anthropic_manual_budget_maps_labeled_efforts
func TestResolve_anthropicManualBudget_档位映射(t *testing.T) {
	cfg := clientOf("Anthropic")
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("claude-sonnet-4-5"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["budget_tokens"] != 16384 {
		t.Fatalf("thinking = %#v，want budget 16384", plan.SDKParams["thinking"])
	}
}

// TestResolve_anthropic_plainEnabled用medium预算
// Python: test_reasoning_resolver.py:322 test_anthropic_manual_budget_plain_enabled_uses_medium_budget
func TestResolve_anthropic_plainEnabled用medium预算(t *testing.T) {
	cfg := clientOf("Anthropic")
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("claude-sonnet-4-5"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled"})))
	thinking, ok := plan.SDKParams["thinking"].(map[string]any)
	if !ok || thinking["budget_tokens"] != 8192 {
		t.Fatalf("thinking = %#v，want budget 8192", plan.SDKParams["thinking"])
	}
}

// ──────────────────────────── Responses 测试分组 ────────────────────────────

// TestResolve_responses_effort映射
// Python: test_reasoning_resolver.py:856 test_openai_account_responses_reasoning_effort_maps_to_reasoning_body
func TestResolve_responses_effort映射(t *testing.T) {
	cfg := clientOf("OpenAIAccount")
	cfg.APIMode = protocolResponses
	plan := ResolveReasoningPlan(cfg, modelOf("gpt-5"), "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "medium"})))
	payload, ok := plan.SDKParams["reasoning"].(map[string]any)
	if !ok || payload["effort"] != "medium" {
		t.Fatalf("reasoning = %#v", plan.SDKParams["reasoning"])
	}
}

// TestResolve_responses_关闭为nil
// Python: reasoning.py:402-408
func TestResolve_responses_关闭为nil(t *testing.T) {
	cfg := clientOf("OpenAIAccount")
	cfg.APIMode = protocolResponses
	plan := ResolveReasoningPlan(cfg, modelOf("gpt-5"), "",
		intentOf(reasoningOf(map[string]any{"mode": "disabled"})))
	v, exists := plan.SDKParams["reasoning"]
	if !exists || v != nil {
		t.Fatalf("reasoning = %#v, want 显式 nil", v)
	}
}

// ──────────────────────────── 遗留 dict 与冲突 ────────────────────────────

// TestResolve_遗留dict原样透传
// Python: test_reasoning_resolver.py:40 test_legacy_raw_reasoning_dict_remains_accepted
func TestResolve_遗留dict原样透传(t *testing.T) {
	raw := map[string]any{"effort": "high", "summary": "auto"}
	plan := ResolveReasoningPlan(clientOf("OpenAI"), modelOf("any"), "", intentOf(raw))
	got, ok := plan.SDKParams["reasoning"].(map[string]any)
	if !ok || !reflect.DeepEqual(got, raw) {
		t.Fatalf("reasoning = %#v", plan.SDKParams["reasoning"])
	}
}

// TestResolve_遗留dict_anthropic丢弃
// Python: reasoning.py:324-327
func TestResolve_遗留dict_anthropic丢弃(t *testing.T) {
	cfg := clientOf("Anthropic")
	cfg.APIMode = protocolAnthropicMessages
	plan := ResolveReasoningPlan(cfg, modelOf("any"), "",
		intentOf(map[string]any{"effort": "high", "summary": "auto"}))
	if len(plan.SDKParams) != 0 {
		t.Fatalf("anthropic 下遗留 dict 应丢弃，got %+v", plan.SDKParams)
	}
}

// TestResolve_遗留控制冲突给warning
// Python: test_reasoning_resolver.py:396 test_explicit_extra_body_reasoning_controls_are_not_overridden
func TestResolve_遗留控制冲突给warning(t *testing.T) {
	m := modelOf("any")
	m.Extra = map[string]any{"reasoning_effort": "low"}
	plan := ResolveReasoningPlan(clientOf("OpenAI"), m, "",
		intentOf(reasoningOf(map[string]any{"mode": "enabled", "effort": "high"})))
	if len(plan.Warnings) == 0 {
		t.Fatal("应产生 warning")
	}
	if len(plan.SDKParams) != 0 {
		t.Fatalf("冲突时不应写参数: %+v", plan.SDKParams)
	}
}

// TestResolve_未传reasoning回落modelConfig 验证取值优先级。
// Python: reasoning.py:300-306 _explicit_reasoning_value
func TestResolve_未传reasoning回落modelConfig(t *testing.T) {
	m := modelOf("unknown-model")
	m.Reasoning = reasoningOf(map[string]any{"mode": "enabled", "effort": "low"})
	plan := ResolveReasoningPlan(clientOf("OpenAI"), m, "", llmschema.ReasoningIntent{Set: false})
	if got := plan.SDKParams["reasoning_effort"]; got != "low" {
		t.Fatalf("reasoning_effort = %v, want low（应回落 modelConfig）", got)
	}
}

// TestResolve_显式nil不回落modelConfig 验证显式 nil 覆盖配置。
// Python: reasoning.py:321 has_explicit_reasoning = "reasoning" in explicit_kwargs
func TestResolve_显式nil不回落modelConfig(t *testing.T) {
	m := modelOf("unknown-model")
	m.Reasoning = reasoningOf(map[string]any{"mode": "enabled", "effort": "low"})
	plan := ResolveReasoningPlan(clientOf("OpenAI"), m, "",
		llmschema.ReasoningIntent{Set: true, Value: nil})
	if len(plan.SDKParams) != 0 || len(plan.ExtraBody) != 0 {
		t.Fatalf("显式 nil 应产生空 plan，got %+v", plan)
	}
}

// ──────────────────────────── apply / controls 测试分组 ────────────────────────────

// TestApplyReasoningPlan_不覆盖已有键
// Python: reasoning.py:935-962
func TestApplyReasoningPlan_不覆盖已有键(t *testing.T) {
	params := map[string]any{"reasoning_effort": "low"}
	plan := ReasoningPlan{SDKParams: map[string]any{"reasoning_effort": "high"}}
	ApplyReasoningPlan(params, plan, false)
	if params["reasoning_effort"] != "low" {
		t.Fatalf("被覆盖了: %v", params["reasoning_effort"])
	}
	ApplyReasoningPlan(params, plan, true)
	if params["reasoning_effort"] != "high" {
		t.Fatalf("override 未生效: %v", params["reasoning_effort"])
	}
}

// TestApplyReasoningPlan_extra_body非override保留已有
func TestApplyReasoningPlan_extra_body非override保留已有(t *testing.T) {
	params := map[string]any{"extra_body": map[string]any{"keep": 1, "thinking": "x"}}
	plan := ReasoningPlan{ExtraBody: map[string]any{"thinking": "plan", "add": 2}}
	ApplyReasoningPlan(params, plan, false)
	eb := params["extra_body"].(map[string]any)
	if eb["keep"] != 1 || eb["add"] != 2 {
		t.Fatalf("eb = %#v", eb)
	}
	if eb["thinking"] != "x" {
		t.Fatalf("非 override 时应保留已有值，got %v", eb["thinking"])
	}
}

// TestApplyReasoningPlan_extra_body非map时丢弃 验证日志分支不崩。
func TestApplyReasoningPlan_extra_body非map时丢弃(t *testing.T) {
	params := map[string]any{"extra_body": "not-a-map"}
	plan := ReasoningPlan{ExtraBody: map[string]any{"thinking": "plan"}}
	ApplyReasoningPlan(params, plan, false)
	if params["extra_body"] != "not-a-map" {
		t.Fatalf("非法 extra_body 应保留原值，got %v", params["extra_body"])
	}
}

// TestReasoningRequestControls_只暴露wire键
// Python: test_reasoning_resolver.py:46 test_reasoning_request_controls_only_exposes_wire_controls
func TestReasoningRequestControls_只暴露wire键(t *testing.T) {
	params := map[string]any{
		"model": "m", "temperature": 0.5, "reasoning_effort": "high",
		"extra_body": map[string]any{"thinking": map[string]any{"type": "enabled"}, "unrelated": 1},
	}
	got := ReasoningRequestControls(params)
	if _, exists := got["model"]; exists {
		t.Error("不应暴露 model")
	}
	if got["reasoning_effort"] != "high" {
		t.Errorf("缺 reasoning_effort")
	}
	eb, ok := got["extra_body"].(map[string]any)
	if !ok || len(eb) != 1 || eb["thinking"] == nil {
		t.Errorf("extra_body = %#v", got["extra_body"])
	}
}
