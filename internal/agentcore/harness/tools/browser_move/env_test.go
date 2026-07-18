package browser_move

import (
	"os"
	"testing"
)

func TestFirstNonEmptyEnv(t *testing.T) {
	os.Setenv("TEST_BM_KEY1", "")
	os.Setenv("TEST_BM_KEY2", "value2")
	defer os.Unsetenv("TEST_BM_KEY1")
	defer os.Unsetenv("TEST_BM_KEY2")
	result := FirstNonEmptyEnv("TEST_BM_KEY1", "TEST_BM_KEY2")
	if result != "value2" {
		t.Errorf("期望 value2, 得到 %s", result)
	}
}

func TestFirstNonEmptyEnv_全部为空(t *testing.T) {
	result := FirstNonEmptyEnv("TEST_BM_NONEXISTENT1", "TEST_BM_NONEXISTENT2")
	if result != "" {
		t.Errorf("期望空字符串, 得到 %s", result)
	}
}

func TestNormalizeProvider(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"openai", "openai"},
		{"OpenAI", "openai"},
		{"alibaba", "dashscope"},
		{"aliyun", "dashscope"},
		{"silicon-flow", "siliconflow"},
		{"silicon_flow", "siliconflow"},
		{"unknown_provider", "unknown_provider"},
	}
	for _, tt := range tests {
		result := NormalizeProvider(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeProvider(%q) = %q, 期望 %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsTruthyEnv(t *testing.T) {
	if !IsTruthyEnv("1") || !IsTruthyEnv("true") || !IsTruthyEnv("yes") || !IsTruthyEnv("on") {
		t.Error("期望真值判断正确")
	}
	if IsTruthyEnv("0") || IsTruthyEnv("false") || IsTruthyEnv("") || IsTruthyEnv("no") {
		t.Error("期望假值判断正确")
	}
}

func TestIsFalsyEnv(t *testing.T) {
	if !IsFalsyEnv("0") || !IsFalsyEnv("false") || !IsFalsyEnv("no") || !IsFalsyEnv("off") {
		t.Error("期望假值判断正确")
	}
	if IsFalsyEnv("1") || IsFalsyEnv("true") || IsFalsyEnv("") {
		t.Error("期望非假值判断正确")
	}
}

func TestResolveIntEnv(t *testing.T) {
	os.Setenv("TEST_BM_INT", "42")
	defer os.Unsetenv("TEST_BM_INT")
	min := 1
	result := ResolveIntEnv([]string{"TEST_BM_INT"}, 10, &min)
	if result != 42 {
		t.Errorf("期望 42, 得到 %d", result)
	}
}

func TestResolveIntEnv_低于最小值(t *testing.T) {
	os.Setenv("TEST_BM_INT_LOW", "0")
	defer os.Unsetenv("TEST_BM_INT_LOW")
	min := 1
	result := ResolveIntEnv([]string{"TEST_BM_INT_LOW"}, 10, &min)
	if result != 10 {
		t.Errorf("期望回退默认值 10, 得到 %d", result)
	}
}

func TestResolveIntEnv_无最小值限制(t *testing.T) {
	os.Setenv("TEST_BM_INT_NOMIN", "0")
	defer os.Unsetenv("TEST_BM_INT_NOMIN")
	result := ResolveIntEnv([]string{"TEST_BM_INT_NOMIN"}, 10, nil)
	if result != 0 {
		t.Errorf("期望 0, 得到 %d", result)
	}
}

func TestResolveBoolEnv(t *testing.T) {
	os.Setenv("TEST_BM_BOOL", "true")
	defer os.Unsetenv("TEST_BM_BOOL")
	result := ResolveBoolEnv([]string{"TEST_BM_BOOL"}, false)
	if result != true {
		t.Errorf("期望 true, 得到 %v", result)
	}
}

func TestResolveBoolEnv_默认值(t *testing.T) {
	result := ResolveBoolEnv([]string{"TEST_BM_BOOL_NONEXISTENT"}, true)
	if result != true {
		t.Errorf("期望 true, 得到 %v", result)
	}
}

func TestResolveModelName(t *testing.T) {
	os.Setenv("MODEL_NAME", "test-model")
	defer os.Unsetenv("MODEL_NAME")
	result := ResolveModelName()
	if result != "test-model" {
		t.Errorf("期望 test-model, 得到 %s", result)
	}
}

func TestResolveModelName_默认值(t *testing.T) {
	os.Unsetenv("MODEL_NAME")
	result := ResolveModelName()
	if result != DefaultModelName {
		t.Errorf("期望 %s, 得到 %s", DefaultModelName, result)
	}
}

func TestResolveBrowserTimeoutS(t *testing.T) {
	os.Setenv("BROWSER_TIMEOUT_S", "300")
	defer os.Unsetenv("BROWSER_TIMEOUT_S")
	result := ResolveBrowserTimeoutS()
	if result != 300 {
		t.Errorf("期望 300, 得到 %d", result)
	}
}

func TestParseCommandArgs(t *testing.T) {
	result := ParseCommandArgs("-y @playwright/mcp@latest")
	if len(result) != 2 || result[0] != "-y" || result[1] != "@playwright/mcp@latest" {
		t.Errorf("ParseCommandArgs 结果不正确: %v", result)
	}
}

func TestParseCommandArgs_JSON数组(t *testing.T) {
	result := ParseCommandArgs(`["-y", "@playwright/mcp@latest"]`)
	if len(result) != 2 || result[0] != "-y" || result[1] != "@playwright/mcp@latest" {
		t.Errorf("ParseCommandArgs JSON 结果不正确: %v", result)
	}
}

func TestParseCommandArgs_空字符串(t *testing.T) {
	result := ParseCommandArgs("")
	if result != nil {
		t.Errorf("期望 nil, 得到 %v", result)
	}
}

func TestInferProviderFromAPIBase(t *testing.T) {
	tests := []struct {
		apiBase  string
		expected string
	}{
		{"https://openrouter.ai/api/v1", "openrouter"},
		{"https://api.siliconflow.cn/v1", "siliconflow"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "dashscope"},
		{"https://api.openai.com/v1", "openai"},
		{"", ""},
	}
	for _, tt := range tests {
		result := InferProviderFromAPIBase(tt.apiBase)
		if result != tt.expected {
			t.Errorf("InferProviderFromAPIBase(%q) = %q, 期望 %q", tt.apiBase, result, tt.expected)
		}
	}
}

func TestResolveModelSettings_OpenRouter(t *testing.T) {
	os.Setenv("MODEL_PROVIDER", "openrouter")
	os.Setenv("OPENROUTER_API_KEY", "or-key")
	os.Setenv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
	defer os.Unsetenv("MODEL_PROVIDER")
	defer os.Unsetenv("OPENROUTER_API_KEY")
	defer os.Unsetenv("OPENROUTER_BASE_URL")

	provider, apiKey, apiBase := ResolveModelSettings()
	if provider != "openrouter" {
		t.Errorf("期望 openrouter, 得到 %s", provider)
	}
	if apiKey != "or-key" {
		t.Errorf("期望 or-key, 得到 %s", apiKey)
	}
	if apiBase != "https://openrouter.ai/api/v1" {
		t.Errorf("期望 https://openrouter.ai/api/v1, 得到 %s", apiBase)
	}
}

func TestResolveModelSettings_SiliconFlow(t *testing.T) {
	os.Setenv("MODEL_PROVIDER", "siliconflow")
	os.Setenv("SILICONFLOW_API_KEY", "sf-key")
	defer os.Unsetenv("MODEL_PROVIDER")
	defer os.Unsetenv("SILICONFLOW_API_KEY")

	provider, _, _ := ResolveModelSettings()
	if provider != "siliconflow" {
		t.Errorf("期望 siliconflow, 得到 %s", provider)
	}
}

func TestResolveModelSettings_DashScope(t *testing.T) {
	os.Setenv("MODEL_PROVIDER", "dashscope")
	os.Setenv("DASHSCOPE_API_KEY", "ds-key")
	defer os.Unsetenv("MODEL_PROVIDER")
	defer os.Unsetenv("DASHSCOPE_API_KEY")

	provider, _, _ := ResolveModelSettings()
	if provider != "dashscope" {
		t.Errorf("期望 dashscope, 得到 %s", provider)
	}
}

func TestResolveModelSettings_默认OpenAI(t *testing.T) {
	// 清除可能干扰的环境变量
	os.Unsetenv("MODEL_PROVIDER")
	os.Unsetenv("MODEL_CLIENT_PROVIDER")
	os.Unsetenv("API_KEY")
	os.Unsetenv("MODEL_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("OPENROUTER_API_KEY")

	provider, _, _ := ResolveModelSettings()
	if provider != "openai" {
		t.Errorf("期望默认 openai, 得到 %s", provider)
	}
}
