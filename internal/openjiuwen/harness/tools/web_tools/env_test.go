package web_tools

import (
	"os"
	"testing"
)

// TestEnvFlag 空值返回默认值
func TestEnvFlag(t *testing.T) {
	tests := []struct {
		name       string
		envVal     string
		defaultVal bool
		want       bool
	}{
		{"空值_默认true", "", true, true},
		{"空值_默认false", "", false, false},
		{"1", "1", false, true},
		{"true", "true", false, true},
		{"yes", "yes", false, true},
		{"on", "on", false, true},
		{"enabled", "enabled", false, true},
		{"0", "0", true, false},
		{"false", "false", true, false},
		{"大写TRUE", "TRUE", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_ENV_FLAG_" + tt.name
			os.Setenv(key, tt.envVal)
			defer os.Unsetenv(key)
			if got := envFlag(key, tt.defaultVal); got != tt.want {
				t.Errorf("envFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsFreeSearchEnabled DDG/Bing 开关组合
func TestIsFreeSearchEnabled(t *testing.T) {
	// 两个都关闭
	os.Unsetenv(freeSearchDDGEnabledEnv)
	os.Unsetenv(freeSearchBingEnabledEnv)
	if IsFreeSearchEnabled() {
		t.Error("两个都关闭时应返回 false")
	}

	// DDG 开启
	os.Setenv(freeSearchDDGEnabledEnv, "true")
	defer os.Unsetenv(freeSearchDDGEnabledEnv)
	if !IsFreeSearchEnabled() {
		t.Error("DDG 开启时应返回 true")
	}
}

// TestIsPaidSearchEnabled API Key 配置检测
func TestIsPaidSearchEnabled(t *testing.T) {
	// 清理所有 key
	for _, key := range paidSearchAPIKeyEnvs() {
		os.Unsetenv(key)
	}
	if IsPaidSearchEnabled() {
		t.Error("没有 API Key 时应返回 false")
	}

	// 设置一个 key
	os.Setenv("BOCHA_API_KEY", "test-key")
	defer os.Unsetenv("BOCHA_API_KEY")
	if !IsPaidSearchEnabled() {
		t.Error("有 API Key 时应返回 true")
	}
}

// TestConfiguredPaidSearchProviders 降级顺序
func TestConfiguredPaidSearchProviders(t *testing.T) {
	// 清理
	for _, key := range paidSearchAPIKeyEnvs() {
		os.Unsetenv(key)
	}

	// 设置两个 key
	os.Setenv("BOCHA_API_KEY", "test-key")
	os.Setenv("SERPER_API_KEY", "test-key")
	defer os.Unsetenv("BOCHA_API_KEY")
	defer os.Unsetenv("SERPER_API_KEY")

	providers := configuredPaidSearchProviders()
	// 降级顺序应为 bocha, serper（按 paidSearchProviderOrder 顺序）
	if len(providers) != 2 {
		t.Fatalf("应有 2 个 provider, got %d", len(providers))
	}
	if providers[0] != "bocha" {
		t.Errorf("第一个应为 bocha, got %s", providers[0])
	}
	if providers[1] != "serper" {
		t.Errorf("第二个应为 serper, got %s", providers[1])
	}
}

// TestSafeEnvChoice 允许列表过滤
func TestSafeEnvChoice(t *testing.T) {
	allowed := map[string]bool{"a": true, "b": true}

	// 空值返回默认
	os.Unsetenv("TEST_SAFE_ENV")
	if got := safeEnvChoice("TEST_SAFE_ENV", "a", allowed); got != "a" {
		t.Errorf("空值应返回默认, got %s", got)
	}

	// 在允许列表中
	os.Setenv("TEST_SAFE_ENV", "b")
	defer os.Unsetenv("TEST_SAFE_ENV")
	if got := safeEnvChoice("TEST_SAFE_ENV", "a", allowed); got != "b" {
		t.Errorf("应在允许列表中, got %s", got)
	}

	// 不在允许列表中返回默认
	os.Setenv("TEST_SAFE_ENV", "c")
	if got := safeEnvChoice("TEST_SAFE_ENV", "a", allowed); got != "a" {
		t.Errorf("不在允许列表中应返回默认, got %s", got)
	}
}

// TestNoProxyEntries NO_PROXY 配置列表
func TestNoProxyEntries(t *testing.T) {
	os.Unsetenv("NO_PROXY")
	os.Unsetenv("no_proxy")
	entries := noProxyEntries()
	if len(entries) == 0 {
		t.Error("应返回默认 NO_PROXY 列表")
	}
	// 验证默认值中包含 localhost
	found := false
	for _, e := range entries {
		if e == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Error("默认列表应包含 localhost")
	}
}
