package config

import (
	"context"
	"os"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
)

// testCryptoKey 32 字节测试密钥（AES-256）
var testCryptoKey = []byte("0123456789abcdef0123456789abcdef")

// TestGetDefaultModels_新格式defaults列表 优先读取 models.defaults 列表并附加 is_default。
func TestGetDefaultModels_新格式defaults列表(t *testing.T) {
	cfg := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{"model_name": "m1", "api_key": "k"},
					"model_config_obj":    map[string]any{"temperature": 0.7},
				},
			},
		},
	}
	entries := GetDefaultModels(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if b, _ := entries[0]["is_default"].(bool); !b {
		t.Fatalf("single-entry group should be is_default=true, got %v", entries[0]["is_default"])
	}
}

// TestGetDefaultModels_旧格式default单对象 包装为列表。
func TestGetDefaultModels_旧格式default单对象(t *testing.T) {
	cfg := map[string]any{
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{"model_name": "m-old"},
			},
		},
	}
	entries := GetDefaultModels(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	mcc := entries[0]["model_client_config"].(map[string]any)
	if mcc["model_name"] != "m-old" {
		t.Fatalf("unexpected model_name: %v", mcc["model_name"])
	}
}

// TestGetDefaultModels_环境变量回退 无 models 段时从 env 构造。
func TestGetDefaultModels_环境变量回退(t *testing.T) {
	t.Setenv("MODEL_ALIAS", "my-alias")
	t.Setenv("API_BASE", "https://api.example.com")
	t.Setenv("API_KEY", "sk-test")
	t.Setenv("MODEL_NAME", "env-model")
	t.Setenv("MODEL_PROVIDER", "OpenAI")

	entries := GetDefaultModels(map[string]any{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 env entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry["alias"] != "my-alias" {
		t.Fatalf("expected alias, got %v", entry["alias"])
	}
	mcc := entry["model_client_config"].(map[string]any)
	if mcc["model_name"] != "env-model" || mcc["api_base"] != "https://api.example.com" {
		t.Fatalf("unexpected mcc: %v", mcc)
	}
	if mcc["timeout"] != 1800 {
		t.Fatalf("expected timeout=1800, got %v", mcc["timeout"])
	}
	if mcc["verify_ssl"] != false {
		t.Fatalf("expected verify_ssl=false, got %v", mcc["verify_ssl"])
	}
	mco := entry["model_config_obj"].(map[string]any)
	if mco["temperature"] != 0.95 {
		t.Fatalf("expected temperature=0.95, got %v", mco["temperature"])
	}
}

// TestGetDefaultModels_环境变量回退alias为空不附加。
func TestGetDefaultModels_环境变量回退alias为空不附加(t *testing.T) {
	os.Unsetenv("MODEL_ALIAS") //nolint:errcheck // 测试清理
	entries := GetDefaultModels(nil)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if _, exists := entries[0]["alias"]; exists {
		t.Fatalf("empty alias should not be attached")
	}
}

// TestInferIsDefault_分组语义 同名多条首 true、显式 true 保留。
func TestInferIsDefault_分组语义(t *testing.T) {
	entries := []map[string]any{
		{"model_client_config": map[string]any{"model_name": "same"}},
		{"model_client_config": map[string]any{"model_name": "same"}},
		{"model_client_config": map[string]any{"model_name": "other"}},
	}
	result := InferIsDefault(entries)
	if result[0]["is_default"] != true {
		t.Fatalf("group multi: first should be true")
	}
	if result[1]["is_default"] != false {
		t.Fatalf("group multi: second should be false")
	}
	if result[2]["is_default"] != true {
		t.Fatalf("single group should be true")
	}
}

// TestInferIsDefault_显式true保留 首个显式 true 生效其余置 false。
func TestInferIsDefault_显式true保留(t *testing.T) {
	entries := []map[string]any{
		{"model_client_config": map[string]any{"model_name": "a"}, "is_default": false},
		{"model_client_config": map[string]any{"model_name": "a"}, "is_default": true},
		{"model_client_config": map[string]any{"model_name": "a"}, "is_default": true},
	}
	result := InferIsDefault(entries)
	if result[0]["is_default"] != false || result[1]["is_default"] != true || result[2]["is_default"] != false {
		t.Fatalf("explicit true keep-first failed: %v %v %v",
			result[0]["is_default"], result[1]["is_default"], result[2]["is_default"])
	}
}

// TestDecryptModelEntries_customHeaders解析 JSON 字符串转 map，且不修改原始数据。
func TestDecryptModelEntries_customHeaders解析(t *testing.T) {
	entries := []map[string]any{
		{"model_client_config": map[string]any{
			"model_name":     "m",
			"custom_headers": `{"X-Trace":"1"}`,
		}},
	}
	result := decryptModelEntries(entries)
	mcc := result[0]["model_client_config"].(map[string]any)
	headers, ok := mcc["custom_headers"].(map[string]any)
	if !ok || headers["X-Trace"] != "1" {
		t.Fatalf("custom_headers not parsed: %v", mcc["custom_headers"])
	}
	// 原始数据不被修改（深拷贝）
	orig := entries[0]["model_client_config"].(map[string]any)
	if _, isMap := orig["custom_headers"].(map[string]any); isMap {
		t.Fatalf("original entry should stay unmodified")
	}
}

// TestDecryptModelEntries_解密api_key 全局 provider 就绪时 api_key 解密返回。
func TestDecryptModelEntries_解密api_key(t *testing.T) {
	original := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(original)

	provider, err := crypto.NewAesGcmProvider(testCryptoKey)
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	crypto.SetCryptoProvider(provider)

	ciphertext, err := provider.Encrypt(context.Background(), "sk-secret")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	entries := []map[string]any{{
		"model_client_config": map[string]any{"api_key": ciphertext, "model_name": "m"},
	}}
	result := decryptModelEntries(entries)

	got := result[0]["model_client_config"].(map[string]any)["api_key"]
	if got != "sk-secret" {
		t.Errorf("api_key = %v, 期望解密为 sk-secret", got)
	}
	// 深拷贝：原 entries 不被修改
	orig := entries[0]["model_client_config"].(map[string]any)["api_key"]
	if orig != ciphertext {
		t.Errorf("原始条目被修改: %v", orig)
	}
}

// TestDecryptModelEntries_无provider 全局 nil 时 api_key 保持原值。
func TestDecryptModelEntries_无provider(t *testing.T) {
	original := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(original)
	crypto.SetCryptoProvider(nil)

	entries := []map[string]any{{
		"model_client_config": map[string]any{"api_key": "plain", "model_name": "m"},
	}}
	result := decryptModelEntries(entries)

	got := result[0]["model_client_config"].(map[string]any)["api_key"]
	if got != "plain" {
		t.Errorf("api_key = %v, 期望无 provider 时保持 plain", got)
	}
}

// TestDecryptModelEntries_解密失败保留原值 密文无效时保留原值（对齐 Python except: pass）。
func TestDecryptModelEntries_解密失败保留原值(t *testing.T) {
	original := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(original)

	provider, err := crypto.NewAesGcmProvider(testCryptoKey)
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	crypto.SetCryptoProvider(provider)

	entries := []map[string]any{{
		"model_client_config": map[string]any{"api_key": "not-a-ciphertext", "model_name": "m"},
	}}
	result := decryptModelEntries(entries)

	got := result[0]["model_client_config"].(map[string]any)["api_key"]
	if got != "not-a-ciphertext" {
		t.Errorf("api_key = %v, 期望解密失败时保留原值", got)
	}
}
