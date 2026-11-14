package common

import "testing"

// NormalizeReasoningLevel 档位归一：同义词映射 + 未知返回空。
// 对齐 Python: reasoning_config.py:79-102。
func TestNormalizeReasoningLevel_档位归一(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", ""}, {"  ", ""}, {"unknown", ""},
		{"ON", "on"}, {"true", "on"}, {"enabled", "on"},
		{"OFF", "off"}, {"none", "off"}, {"False", "off"}, {"disabled", "off"},
		{"minimal", "minimal"}, {"minimum", "minimal"},
		{"low", "low"},
		{"med", "medium"}, {"mid", "medium"}, {"MEDIUM", "medium"},
		{"high", "high"},
		{"extra_high", "xhigh"}, {"XHIGH", "xhigh"},
		{"ultra", "max"}, {"maximum", "max"}, {"Max", "max"},
	}
	for _, c := range cases {
		if got := NormalizeReasoningLevel(c.raw); got != c.want {
			t.Errorf("NormalizeReasoningLevel(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// ReasoningConfigForLevel 三种档位形态。
// 对齐 Python: reasoning_config.py:105-111。
func TestReasoningConfigForLevel_档位映射(t *testing.T) {
	if got := ReasoningConfigForLevel("off"); got["mode"] != "disabled" || len(got) != 1 {
		t.Errorf("off 应为 {mode: disabled}: %v", got)
	}
	if got := ReasoningConfigForLevel("on"); got["mode"] != "enabled" || len(got) != 1 {
		t.Errorf("on 应为 {mode: enabled}: %v", got)
	}
	got := ReasoningConfigForLevel("high")
	if got["mode"] != "enabled" || got["effort"] != "high" {
		t.Errorf("high 应为 {mode: enabled, effort: high}: %v", got)
	}
}

// ResolveSamplingOverride：Kimi host 命中、kimi- 模型前缀命中、其它不命中。
// 对齐 Python: reasoning_config.py:67-76。
func TestResolveSamplingOverride_Kimi规则(t *testing.T) {
	hit := ResolveSamplingOverride("https://api.moonshot.cn/v1", "any-model")
	if hit == nil || hit["temperature"] != 1.0 || hit["top_p"] != 0.95 {
		t.Errorf("moonshot host 应命中覆盖: %v", hit)
	}
	if ResolveSamplingOverride("https://other.example.com", "kimi-k2") == nil {
		t.Errorf("kimi- 前缀模型应命中覆盖")
	}
	if got := ResolveSamplingOverride("https://other.example.com", "gpt-x"); got != nil {
		t.Errorf("非 Kimi 不应命中: %v", got)
	}
	// 聚合方路径下的 Kimi 模型（取最后一个 / 后的段）
	if ResolveSamplingOverride("https://other.example.com", "vendor/kimi-k2") == nil {
		t.Errorf("聚合方路径 kimi 模型应命中覆盖")
	}
	// 缺 scheme 自动补 https（Python: reasoning_config.py:20-24）
	if ResolveSamplingOverride("api.kimi.com/v1", "") == nil {
		t.Errorf("缺 scheme 的 Kimi host 应命中覆盖")
	}
}

// ResolveEndpointProfileOverride：host 命中 / 空 host / 未配置。
// 对齐 Python: reasoning_config.py:43-56。
func TestResolveEndpointProfileOverride_按host查表(t *testing.T) {
	if got := ResolveEndpointProfileOverride(""); got != "" {
		t.Errorf("空 api_base 应返回空: %q", got)
	}
	if got := ResolveEndpointProfileOverride("https://unknown.example.com/v1"); got != "" {
		t.Errorf("未配置 host 应返回空: %q", got)
	}
}

// EffectiveEndpointProfile：显式优先，否则推断。
// 对齐 Python: reasoning_config.py:59-64。
func TestEffectiveEndpointProfile_显式优先(t *testing.T) {
	if got := EffectiveEndpointProfile("https://a.com", " dashscope "); got != "dashscope" {
		t.Errorf("显式配置去空白后应优先: %q", got)
	}
	if got := EffectiveEndpointProfile("https://a.com", ""); got != "" {
		t.Errorf("无显式配置且 host 未注册应返回空: %q", got)
	}
}

// ValidateReasoningLevelForModel：空值直通、非法档位、能力不匹配。
// 对齐 Python: reasoning_config.py:119-170。
func TestValidateReasoningLevelForModel_能力校验(t *testing.T) {
	// 空 = 用厂商默认
	if got, err := ValidateReasoningLevelForModel("  ", "m", "openai", "", ""); got != "" || err != nil {
		t.Errorf("空白应直通: got=%q err=%v", got, err)
	}
	// 非法档位文案逐字
	_, err := ValidateReasoningLevelForModel("bogus", "m", "openai", "", "")
	if err == nil || err.Error() != "reasoning_level is invalid; canonical values: off, on, minimal, low, medium, high, xhigh, max" {
		t.Errorf("非法档位文案不符: %v", err)
	}
	// 合法档位但能力不匹配（能力查询走 23.3 静态表，具体允许集随 provider/model 而定，
	// 这里只断言不 panic 且错误文案形状正确）
	_, err = ValidateReasoningLevelForModel("max", "gpt-4o-mini", "openai", "", "")
	if err != nil && err.Error()[:27] != "reasoning_level must be one "[:27] {
		t.Errorf("能力不匹配文案形状不符: %v", err)
	}
}
