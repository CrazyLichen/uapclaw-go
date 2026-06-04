package config

import (
	"os"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestResolveEnvVars_字符串无环境变量(t *testing.T) {
	result := ResolveEnvVars("hello world", nil)
	if result != "hello world" {
		t.Errorf("期望 hello world，实际 %v", result)
	}
}

func TestResolveEnvVars_字符串含环境变量(t *testing.T) {
	os.Setenv("TEST_HOST", "localhost")
	defer os.Unsetenv("TEST_HOST")

	result := ResolveEnvVars("${TEST_HOST}", nil)
	if result != "localhost" {
		t.Errorf("期望 localhost，实际 %v", result)
	}
}

func TestResolveEnvVars_环境变量带默认值(t *testing.T) {
	// 环境变量不存在时使用默认值
	result := ResolveEnvVars("${UNSET_VAR:-fallback}", nil)
	if result != "fallback" {
		t.Errorf("期望 fallback，实际 %v", result)
	}

	// 环境变量存在时使用环境变量
	os.Setenv("SET_VAR", "actual")
	defer os.Unsetenv("SET_VAR")

	result = ResolveEnvVars("${SET_VAR:-fallback}", nil)
	if result != "actual" {
		t.Errorf("期望 actual，实际 %v", result)
	}
}

func TestResolveEnvVars_环境变量无默认值不存在(t *testing.T) {
	result := ResolveEnvVars("${COMPLETELY_UNSET_VAR}", nil)
	if result != "" {
		t.Errorf("期望空字符串，实际 %v", result)
	}
}

func TestResolveEnvVars_环境变量存在但为空(t *testing.T) {
	os.Setenv("EMPTY_VAR", "")
	defer os.Unsetenv("EMPTY_VAR")

	// 空环境变量 + 有默认值 → 使用默认值
	result := ResolveEnvVars("${EMPTY_VAR:-fallback}", nil)
	if result != "fallback" {
		t.Errorf("期望 fallback，实际 %v", result)
	}

	// 空环境变量 + 无默认值 → 空字符串
	result = ResolveEnvVars("${EMPTY_VAR}", nil)
	if result != "" {
		t.Errorf("期望空字符串，实际 %v", result)
	}
}

func TestResolveEnvVars_混合字符串(t *testing.T) {
	os.Setenv("TEST_HOST", "localhost")
	defer os.Unsetenv("TEST_HOST")
	os.Setenv("TEST_PORT", "8080")
	defer os.Unsetenv("TEST_PORT")

	result := ResolveEnvVars("http://${TEST_HOST}:${TEST_PORT}/api", nil)
	if result != "http://localhost:8080/api" {
		t.Errorf("期望 http://localhost:8080/api，实际 %v", result)
	}
}

func TestResolveEnvVars_字典递归解析(t *testing.T) {
	os.Setenv("TEST_API_KEY", "sk-123")
	defer os.Unsetenv("TEST_API_KEY")
	os.Setenv("TEST_URL", "http://example.com")
	defer os.Unsetenv("TEST_URL")

	input := map[string]any{
		"url":     "${TEST_URL}",
		"api_key": "${TEST_API_KEY}",
		"nested": map[string]any{
			"deep_url": "${TEST_URL}/v2",
		},
	}

	result := ResolveEnvVars(input, nil)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("期望 map[string]any 类型")
	}
	if m["url"] != "http://example.com" {
		t.Errorf("期望 http://example.com，实际 %v", m["url"])
	}
	if m["api_key"] != "sk-123" {
		t.Errorf("期望 sk-123，实际 %v", m["api_key"])
	}
	nested := m["nested"].(map[string]any)
	if nested["deep_url"] != "http://example.com/v2" {
		t.Errorf("期望 http://example.com/v2，实际 %v", nested["deep_url"])
	}
}

func TestResolveEnvVars_切片递归解析(t *testing.T) {
	os.Setenv("TEST_ITEM", "value")
	defer os.Unsetenv("TEST_ITEM")

	input := []any{"${TEST_ITEM}", "static", "${UNSET:-default}"}
	result := ResolveEnvVars(input, nil)
	arr, ok := result.([]any)
	if !ok {
		t.Fatal("期望 []any 类型")
	}
	if arr[0] != "value" {
		t.Errorf("期望 value，实际 %v", arr[0])
	}
	if arr[1] != "static" {
		t.Errorf("期望 static，实际 %v", arr[1])
	}
	if arr[2] != "default" {
		t.Errorf("期望 default，实际 %v", arr[2])
	}
}

func TestResolveEnvVars_非字符串类型原样返回(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"整数", 42},
		{"布尔值", true},
		{"浮点数", 3.14},
		{"nil", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveEnvVars(tt.input, nil)
			if result != tt.input {
				t.Errorf("期望 %v，实际 %v", tt.input, result)
			}
		})
	}
}

func TestResolveEnvVars_DecryptFunc调用(t *testing.T) {
	os.Setenv("MY_API_KEY", "encrypted_value")
	defer os.Unsetenv("MY_API_KEY")
	os.Setenv("NORMAL_VAR", "plain_value")
	defer os.Unsetenv("NORMAL_VAR")

	// 模拟解密函数：api_key 类变量返回解密值
	decryptFn := func(envName, value string) (string, bool) {
		if isSensitiveVar(envName) {
			return "decrypted_" + value, true
		}
		return value, false
	}

	input := map[string]any{
		"api_key":  "${MY_API_KEY}",
		"normal":   "${NORMAL_VAR}",
	}

	result := ResolveEnvVars(input, decryptFn)
	m := result.(map[string]any)

	// api_key 应被解密
	if m["api_key"] != "decrypted_encrypted_value" {
		t.Errorf("期望 decrypted_encrypted_value，实际 %v", m["api_key"])
	}
	// 普通变量不应被解密
	if m["normal"] != "plain_value" {
		t.Errorf("期望 plain_value，实际 %v", m["normal"])
	}
}

func TestResolveEnvVars_DecryptFunc为nil不解密(t *testing.T) {
	os.Setenv("MY_TOKEN", "raw_token")
	defer os.Unsetenv("MY_TOKEN")

	result := ResolveEnvVars("${MY_TOKEN}", nil)
	if result != "raw_token" {
		t.Errorf("期望 raw_token，实际 %v", result)
	}
}

func TestIsSensitiveVar(t *testing.T) {
	tests := []struct {
		name    string
		varName string
		want    bool
	}{
		{"包含api_key", "MY_API_KEY", true},
		{"包含token", "ACCESS_TOKEN", true},
		{"包含api-key", "MY_API-KEY", true},
		{"普通变量", "HOST_NAME", false},
		{"大小写不敏感", "MY_Api_Key", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSensitiveVar(tt.varName); got != tt.want {
				t.Errorf("isSensitiveVar(%q) = %v，期望 %v", tt.varName, got, tt.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"空字符串", "", ""},
		{"波浪线展开", "~/projects", home + "/projects"},
		{"绝对路径", "/etc/config", "/etc/config"},
		{"相对路径", "config.yaml", "config.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePath(tt.input)
			if got != tt.want {
				t.Errorf("ResolvePath(%q) = %q，期望 %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveEnvVarString(t *testing.T) {
	os.Setenv("TEST_HOST", "127.0.0.1")
	defer os.Unsetenv("TEST_HOST")

	result := ResolveEnvVarString("host=${TEST_HOST}", nil)
	if result != "host=127.0.0.1" {
		t.Errorf("期望 host=127.0.0.1，实际 %s", result)
	}
}
