package schema

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNormalizeReasoningLevel_八档与别名 逐条对齐 reasoning_config.py:92-118。
func TestNormalizeReasoningLevel_八档与别名(t *testing.T) {
	cases := map[string]ReasoningLevel{
		"off": ReasoningLevelOff, "none": ReasoningLevelOff,
		"false": ReasoningLevelOff, "disable": ReasoningLevelOff,
		"disabled": ReasoningLevelOff, "OFF": ReasoningLevelOff,
		"on": ReasoningLevelOn, "true": ReasoningLevelOn,
		"enable": ReasoningLevelOn, "enabled": ReasoningLevelOn,
		"minimal": ReasoningLevelMinimal, "minimum": ReasoningLevelMinimal,
		"low":    ReasoningLevelLow,
		"medium": ReasoningLevelMedium, "med": ReasoningLevelMedium, "mid": ReasoningLevelMedium,
		"high":  ReasoningLevelHigh,
		"xhigh": ReasoningLevelXHigh, "extra_high": ReasoningLevelXHigh,
		"max": ReasoningLevelMax, "maximum": ReasoningLevelMax, "ultra": ReasoningLevelMax,
		"": "", "bogus": "",
	}
	for in, want := range cases {
		if got := NormalizeReasoningLevel(in); got != want {
			t.Errorf("NormalizeReasoningLevel(%q) = %q, 期望 %q", in, got, want)
		}
	}
	if got := NormalizeReasoningLevel(nil); got != "" {
		t.Errorf("nil = %q, 期望空串", got)
	}
	if got := NormalizeReasoningLevel(true); got != "" {
		t.Errorf("bool 应返回空串，得 %q", got)
	}
}

// TestReasoningConfigForLevel 三档映射。
// Python: reasoning_config.py:120-126
func TestReasoningConfigForLevel(t *testing.T) {
	if got := ReasoningConfigForLevel(ReasoningLevelOff); got["mode"] != "disabled" {
		t.Errorf("off → %v", got)
	}
	if got := ReasoningConfigForLevel(ReasoningLevelOn); got["mode"] != "enabled" {
		t.Errorf("on → %v", got)
	}
	got := ReasoningConfigForLevel(ReasoningLevelHigh)
	if got["mode"] != "enabled" || got["effort"] != "high" {
		t.Errorf("high → %v", got)
	}
}

// TestResolveSamplingOverride_Kimi Kimi 端点与模型必须强制采样参数。
// Python: reasoning_config.py:67-76
func TestResolveSamplingOverride_Kimi(t *testing.T) {
	for _, apiBase := range []string{
		"https://api.moonshot.cn/v1",
		"https://api.kimi.com/v1",
		"http://api.moonshot.cn",
	} {
		got := ResolveSamplingOverride(apiBase, "any-model")
		if got == nil || got["temperature"] != 1.0 || got["top_p"] != 0.95 {
			t.Errorf("%q → %v", apiBase, got)
		}
	}
	for _, model := range []string{"kimi-k2-0905", "moonshot/kimi-k2-0905", "KIMI-K2"} {
		if got := ResolveSamplingOverride("https://other.example/v1", model); got == nil {
			t.Errorf("模型 %q 应命中 Kimi 规则", model)
		}
	}
	if got := ResolveSamplingOverride("https://api.openai.com/v1", "gpt-4"); got != nil {
		t.Errorf("非 Kimi 应返回 nil，得 %v", got)
	}
	if got := ResolveSamplingOverride("", ""); got != nil {
		t.Errorf("空输入应返回 nil，得 %v", got)
	}
}

// TestEffectiveEndpointProfile_显式优先。
// Python: reasoning_config.py:59-64
func TestEffectiveEndpointProfile_显式优先(t *testing.T) {
	if got := EffectiveEndpointProfile("https://api.moonshot.cn", "dashscope"); got != "dashscope" {
		t.Errorf("显式值应优先，得 %q", got)
	}
	// 无显式值且无配置映射 → 空串（源码不内置 host）
	if got := EffectiveEndpointProfile("https://unknown.example", nil); got != "" {
		t.Errorf("未配置 host 应返回空串，得 %q", got)
	}
	// 空白显式值等同缺失
	if got := EffectiveEndpointProfile("https://unknown.example", "   "); got != "" {
		t.Errorf("空白显式值应视为缺失，得 %q", got)
	}
}

// TestResolveEndpointProfileOverride_从配置推断 用户配置的 host→profile 生效。
// Python: reasoning_config.py:43-56
func TestResolveEndpointProfileOverride_从配置推断(t *testing.T) {
	t.Setenv("UAPCLAW_CONFIG_YAML", writeEndpointProfileConfig(t,
		"endpoint_profile_overrides:\n  my-gw.internal: dashscope\n"))

	if got := ResolveEndpointProfileOverride("https://my-gw.internal/v1"); got != "dashscope" {
		t.Errorf("配置推断失败，得 %q", got)
	}
	if got := ResolveEndpointProfileOverride(""); got != "" {
		t.Errorf("空 api_base 应返回空串，得 %q", got)
	}
}

// writeEndpointProfileConfig 写一份临时 config.yaml 并返回路径。
func writeEndpointProfileConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	return path
}
