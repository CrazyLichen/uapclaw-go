package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── ProviderType 测试 ────────────────────────────

// TestProviderType_String 验证 ProviderType 枚举值转字符串。
func TestProviderType_String(t *testing.T) {
	tests := []struct {
		pt   ProviderType
		want string
	}{
		{ProviderTypeOpenAI, "OpenAI"},
		{ProviderTypeOpenRouter, "OpenRouter"},
		{ProviderTypeSiliconFlow, "SiliconFlow"},
		{ProviderTypeDashScope, "DashScope"},
		{ProviderTypeDeepSeek, "DeepSeek"},
		{ProviderTypeInferenceAffinity, "InferenceAffinity"},
		{ProviderTypeIntelliRouter, "intelli_router"},
	}
	for _, tt := range tests {
		if got := tt.pt.String(); got != tt.want {
			t.Errorf("ProviderType(%d).String() = %q, want %q", int(tt.pt), got, tt.want)
		}
	}
}

// TestProviderType_String_Invalid 验证无效枚举值的字符串表示。
func TestProviderType_String_Invalid(t *testing.T) {
	pt := ProviderType(999)
	want := "ProviderType(999)"
	if got := pt.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestProviderType_MarshalJSON 验证 ProviderType 序列化为字符串。
func TestProviderType_MarshalJSON(t *testing.T) {
	pt := ProviderTypeOpenAI
	data, err := json.Marshal(pt)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(data) != `"OpenAI"` {
		t.Errorf("got %s, want %q", data, `"OpenAI"`)
	}
}

// TestProviderType_UnmarshalJSON 验证字符串反序列化为 ProviderType。
func TestProviderType_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		json string
		want ProviderType
	}{
		{`"OpenAI"`, ProviderTypeOpenAI},
		{`"DashScope"`, ProviderTypeDashScope},
		{`"intelli_router"`, ProviderTypeIntelliRouter},
	}
	for _, tt := range tests {
		var pt ProviderType
		if err := json.Unmarshal([]byte(tt.json), &pt); err != nil {
			t.Fatalf("反序列化 %s 失败: %v", tt.json, err)
		}
		if pt != tt.want {
			t.Errorf("got %v, want %v", pt, tt.want)
		}
	}
}

// TestProviderType_UnmarshalJSON_CaseInsensitive 验证大小写不敏感反序列化。
func TestProviderType_UnmarshalJSON_CaseInsensitive(t *testing.T) {
	tests := []struct {
		json string
		want ProviderType
	}{
		{`"openai"`, ProviderTypeOpenAI},
		{`"dashscope"`, ProviderTypeDashScope},
		{`"deepseek"`, ProviderTypeDeepSeek},
		{`"siliconflow"`, ProviderTypeSiliconFlow},
	}
	for _, tt := range tests {
		var pt ProviderType
		if err := json.Unmarshal([]byte(tt.json), &pt); err != nil {
			t.Fatalf("反序列化 %s 失败: %v", tt.json, err)
		}
		if pt != tt.want {
			t.Errorf("got %v, want %v", pt, tt.want)
		}
	}
}

// TestProviderType_UnmarshalJSON_Unknown 验证未知字符串反序列化报错。
func TestProviderType_UnmarshalJSON_Unknown(t *testing.T) {
	var pt ProviderType
	err := json.Unmarshal([]byte(`"UnknownProvider"`), &pt)
	if err == nil {
		t.Error("期望反序列化未知 provider 报错，但未报错")
	}
}

// ──────────────────────────── ParseProviderType 测试 ────────────────────────────

// TestParseProviderType 验证精确匹配解析。
func TestParseProviderType(t *testing.T) {
	pt, ok := ParseProviderType("OpenAI")
	if !ok {
		t.Error("期望精确匹配 OpenAI 成功")
	}
	if pt != ProviderTypeOpenAI {
		t.Errorf("got %v, want %v", pt, ProviderTypeOpenAI)
	}
}

// TestParseProviderType_CaseInsensitive 验证大小写不敏感解析。
func TestParseProviderType_CaseInsensitive(t *testing.T) {
	pt, ok := ParseProviderType("openai")
	if !ok {
		t.Error("期望大小写不敏感匹配 openai 成功")
	}
	if pt != ProviderTypeOpenAI {
		t.Errorf("got %v, want %v", pt, ProviderTypeOpenAI)
	}
}

// TestParseProviderType_Unknown 验证未知 provider 返回 false。
func TestParseProviderType_Unknown(t *testing.T) {
	_, ok := ParseProviderType("UnknownProvider")
	if ok {
		t.Error("期望未知 provider 返回 false")
	}
}

// ──────────────────────────── ValidateAndNormalizeProvider 测试 ────────────────────────────

// TestValidateAndNormalizeProvider_ExactMatch 验证精确匹配返回规范值。
func TestValidateAndNormalizeProvider_ExactMatch(t *testing.T) {
	got := ValidateAndNormalizeProvider("OpenAI")
	if got != "OpenAI" {
		t.Errorf("got %q, want %q", got, "OpenAI")
	}
}

// TestValidateAndNormalizeProvider_CaseInsensitive 验证大小写不敏感匹配返回规范值。
func TestValidateAndNormalizeProvider_CaseInsensitive(t *testing.T) {
	got := ValidateAndNormalizeProvider("dashscope")
	if got != "DashScope" {
		t.Errorf("got %q, want %q", got, "DashScope")
	}
}

// TestValidateAndNormalizeProvider_UnknownPreserved 验证未知 provider 保留原值（宽松策略）。
func TestValidateAndNormalizeProvider_UnknownPreserved(t *testing.T) {
	got := ValidateAndNormalizeProvider("MyCustomProvider")
	if got != "MyCustomProvider" {
		t.Errorf("got %q, want %q", got, "MyCustomProvider")
	}
}

// TestValidateAndNormalizeProvider_WithValidator 验证注入 ProviderValidator 后的行为。
func TestValidateAndNormalizeProvider_WithValidator(t *testing.T) {
	// 保存原始验证器
	original := globalProviderValidator
	defer func() { globalProviderValidator = original }()

	// 注入自定义验证器
	SetProviderValidator(&testProviderValidator{
		providers: map[string]string{
			"llm_custom": "Custom",
		},
	})

	// 已注册的自定义 provider 应返回规范化名称
	got := ValidateAndNormalizeProvider("llm_custom")
	if got != "Custom" {
		t.Errorf("got %q, want %q", got, "Custom")
	}

	// 未注册的 provider 仍保留原值
	got = ValidateAndNormalizeProvider("NotRegistered")
	if got != "NotRegistered" {
		t.Errorf("got %q, want %q", got, "NotRegistered")
	}
}

// TestValidateAndNormalizeProvider_TrimSpace 验证前后空格被裁剪。
func TestValidateAndNormalizeProvider_TrimSpace(t *testing.T) {
	got := ValidateAndNormalizeProvider("  OpenAI  ")
	if got != "OpenAI" {
		t.Errorf("got %q, want %q", got, "OpenAI")
	}
}

// TestSetProviderValidator 验证设置和重置全局验证器。
func TestSetProviderValidator(t *testing.T) {
	original := globalProviderValidator
	defer func() { globalProviderValidator = original }()

	// 设置验证器
	v := &testProviderValidator{providers: map[string]string{}}
	SetProviderValidator(v)
	if globalProviderValidator != v {
		t.Error("设置验证器后全局验证器应更新")
	}

	// 重置为 nil
	SetProviderValidator(nil)
	if globalProviderValidator != nil {
		t.Error("重置后全局验证器应为 nil")
	}
}

// testProviderValidator 测试用 ProviderValidator 实现。
type testProviderValidator struct {
	providers map[string]string
}

func (v *testProviderValidator) ValidateProvider(provider string) string {
	if normalized, ok := v.providers[provider]; ok {
		return normalized
	}
	return ""
}

// ──────────────────────────── ModelClientConfig 测试 ────────────────────────────

// TestNewModelClientConfig 验证构造函数默认值。
func TestNewModelClientConfig(t *testing.T) {
	cfg := NewModelClientConfig("OpenAI", "sk-xxx", "https://api.openai.com")
	if cfg.ClientID == "" {
		t.Error("ClientID 应自动生成 UUID")
	}
	if cfg.ClientProvider != "OpenAI" {
		t.Errorf("ClientProvider = %q, want %q", cfg.ClientProvider, "OpenAI")
	}
	if cfg.APIKey != "sk-xxx" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "sk-xxx")
	}
	if cfg.APIBase != "https://api.openai.com" {
		t.Errorf("APIBase = %q, want %q", cfg.APIBase, "https://api.openai.com")
	}
	if cfg.Timeout != 60.0 {
		t.Errorf("Timeout = %f, want 60.0", cfg.Timeout)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if !cfg.VerifySSL {
		t.Error("VerifySSL 默认应为 true")
	}
	if cfg.SSLCert != "" {
		t.Errorf("SSLCert = %q, want 空", cfg.SSLCert)
	}
	if cfg.CustomHeaders != nil {
		t.Error("CustomHeaders 默认应为 nil")
	}
	if cfg.Extra != nil {
		t.Error("Extra 默认应为 nil")
	}
}

// TestNewModelClientConfig_WithOptions 验证选项函数生效。
func TestNewModelClientConfig_WithOptions(t *testing.T) {
	cfg := NewModelClientConfig("OpenAI", "sk-xxx", "https://api.openai.com",
		WithClientID("custom-id"),
		WithTimeout(30.0),
		WithMaxRetries(5),
		WithVerifySSL(false),
		WithSSLCert("/path/to/cert.pem"),
		WithCustomHeaders(map[string]string{"X-Custom": "value"}),
		WithConfigExtra(map[string]any{"extra_field": "extra_value"}),
	)
	if cfg.ClientID != "custom-id" {
		t.Errorf("ClientID = %q, want %q", cfg.ClientID, "custom-id")
	}
	if cfg.Timeout != 30.0 {
		t.Errorf("Timeout = %f, want 30.0", cfg.Timeout)
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", cfg.MaxRetries)
	}
	if cfg.VerifySSL {
		t.Error("VerifySSL 应为 false")
	}
	if cfg.SSLCert != "/path/to/cert.pem" {
		t.Errorf("SSLCert = %q, want %q", cfg.SSLCert, "/path/to/cert.pem")
	}
	if cfg.CustomHeaders["X-Custom"] != "value" {
		t.Error("CustomHeaders 未正确设置")
	}
	if cfg.Extra["extra_field"] != "extra_value" {
		t.Error("Extra 未正确设置")
	}
}

// TestModelClientConfig_Validate_MissingRequired 验证必填字段校验。
func TestModelClientConfig_Validate_MissingRequired(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *ModelClientConfig
		wantErr bool
	}{
		{
			name:    "空 provider",
			cfg:     NewModelClientConfig("", "sk-xxx", "https://api.openai.com"),
			wantErr: true,
		},
		{
			name:    "空 api_key",
			cfg:     NewModelClientConfig("OpenAI", "", "https://api.openai.com"),
			wantErr: true,
		},
		{
			name:    "空 api_base",
			cfg:     NewModelClientConfig("OpenAI", "sk-xxx", ""),
			wantErr: true,
		},
		{
			name:    "timeout 为 0",
			cfg:     NewModelClientConfig("OpenAI", "sk-xxx", "https://api.openai.com", WithTimeout(0)),
			wantErr: true,
		},
		{
			name:    "timeout 为负数",
			cfg:     NewModelClientConfig("OpenAI", "sk-xxx", "https://api.openai.com", WithTimeout(-1)),
			wantErr: true,
		},
		{
			name:    "全部有效",
			cfg:     NewModelClientConfig("OpenAI", "sk-xxx", "https://api.openai.com"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestModelClientConfig_Validate_ProviderNormalization 验证 Validate 规范化 provider。
func TestModelClientConfig_Validate_ProviderNormalization(t *testing.T) {
	cfg := NewModelClientConfig("openai", "sk-xxx", "https://api.openai.com")
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate 失败: %v", err)
	}
	if cfg.ClientProvider != "OpenAI" {
		t.Errorf("规范化后 ClientProvider = %q, want %q", cfg.ClientProvider, "OpenAI")
	}
}

// TestModelClientConfig_MarshalJSON_WithExtra 验证 Extra 字段合并输出。
func TestModelClientConfig_MarshalJSON_WithExtra(t *testing.T) {
	cfg := NewModelClientConfig("OpenAI", "sk-xxx", "https://api.openai.com",
		WithConfigExtra(map[string]any{
			"custom_field":  "custom_value",
			"another_field": 42,
		}),
	)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	// 验证标准字段存在
	if result["client_provider"] != "OpenAI" {
		t.Errorf("client_provider = %v, want %q", result["client_provider"], "OpenAI")
	}
	// 验证 Extra 字段合并到顶层
	if result["custom_field"] != "custom_value" {
		t.Errorf("custom_field = %v, want %q", result["custom_field"], "custom_value")
	}
	if result["another_field"] != float64(42) {
		t.Errorf("another_field = %v, want 42", result["another_field"])
	}
}

// TestModelClientConfig_UnmarshalJSON_WithExtra 验证未知 key 存入 Extra。
func TestModelClientConfig_UnmarshalJSON_WithExtra(t *testing.T) {
	jsonStr := `{
		"client_id": "test-id",
		"client_provider": "OpenAI",
		"api_key": "sk-xxx",
		"api_base": "https://api.openai.com",
		"timeout": 60,
		"max_retries": 3,
		"verify_ssl": true,
		"custom_field": "custom_value",
		"another_field": 42
	}`
	var cfg ModelClientConfig
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if cfg.ClientProvider != "OpenAI" {
		t.Errorf("ClientProvider = %q, want %q", cfg.ClientProvider, "OpenAI")
	}
	if cfg.Extra == nil {
		t.Fatal("Extra 不应为 nil")
	}
	if cfg.Extra["custom_field"] != "custom_value" {
		t.Errorf("Extra[custom_field] = %v, want %q", cfg.Extra["custom_field"], "custom_value")
	}
}

// TestModelClientConfig_RoundTrip 验证序列化→反序列化一致性。
func TestModelClientConfig_RoundTrip(t *testing.T) {
	original := NewModelClientConfig("OpenAI", "sk-xxx", "https://api.openai.com",
		WithTimeout(30.0),
		WithMaxRetries(5),
		WithVerifySSL(false),
		WithSSLCert("/path/to/cert.pem"),
		WithConfigExtra(map[string]any{"extra_key": "extra_val"}),
	)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored ModelClientConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ClientProvider != original.ClientProvider {
		t.Errorf("ClientProvider: got %q, want %q", restored.ClientProvider, original.ClientProvider)
	}
	if restored.APIKey != original.APIKey {
		t.Errorf("APIKey: got %q, want %q", restored.APIKey, original.APIKey)
	}
	if restored.APIBase != original.APIBase {
		t.Errorf("APIBase: got %q, want %q", restored.APIBase, original.APIBase)
	}
	if restored.Timeout != original.Timeout {
		t.Errorf("Timeout: got %f, want %f", restored.Timeout, original.Timeout)
	}
	if restored.MaxRetries != original.MaxRetries {
		t.Errorf("MaxRetries: got %d, want %d", restored.MaxRetries, original.MaxRetries)
	}
	if restored.VerifySSL != original.VerifySSL {
		t.Errorf("VerifySSL: got %v, want %v", restored.VerifySSL, original.VerifySSL)
	}
	if restored.SSLCert != original.SSLCert {
		t.Errorf("SSLCert: got %q, want %q", restored.SSLCert, original.SSLCert)
	}
	if restored.Extra["extra_key"] != "extra_val" {
		t.Errorf("Extra[extra_key]: got %v, want %q", restored.Extra["extra_key"], "extra_val")
	}
}

// ──────────────────────────── ModelRequestConfig 测试 ────────────────────────────

// TestNewModelRequestConfig 验证构造函数默认值。
func TestNewModelRequestConfig(t *testing.T) {
	cfg := NewModelRequestConfig()
	if cfg.ModelName != "" {
		t.Errorf("ModelName = %q, want 空", cfg.ModelName)
	}
	if cfg.Temperature != 0.95 {
		t.Errorf("Temperature = %f, want 0.95", cfg.Temperature)
	}
	if cfg.TopP != 0.1 {
		t.Errorf("TopP = %f, want 0.1", cfg.TopP)
	}
	if cfg.MaxTokens != nil {
		t.Error("MaxTokens 默认应为 nil")
	}
	if cfg.Stop != nil {
		t.Error("Stop 默认应为 nil")
	}
	if cfg.Extra != nil {
		t.Error("Extra 默认应为 nil")
	}
}

// TestNewModelRequestConfig_WithOptions 验证选项函数生效。
func TestNewModelRequestConfig_WithOptions(t *testing.T) {
	cfg := NewModelRequestConfig(
		WithModelName("gpt-4"),
		WithTemperature(0.5),
		WithTopP(0.9),
		WithMaxTokens(1000),
		WithStop("END"),
		WithRequestExtra(map[string]any{"custom": "val"}),
	)
	if cfg.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", cfg.ModelName, "gpt-4")
	}
	if cfg.Temperature != 0.5 {
		t.Errorf("Temperature = %f, want 0.5", cfg.Temperature)
	}
	if cfg.TopP != 0.9 {
		t.Errorf("TopP = %f, want 0.9", cfg.TopP)
	}
	if cfg.MaxTokens == nil || *cfg.MaxTokens != 1000 {
		t.Errorf("MaxTokens = %v, want 1000", cfg.MaxTokens)
	}
	if cfg.Stop == nil || *cfg.Stop != "END" {
		t.Errorf("Stop = %v, want END", cfg.Stop)
	}
	if cfg.Extra["custom"] != "val" {
		t.Error("Extra 未正确设置")
	}
}

// TestModelRequestConfig_MarshalJSON_ModelAlias 验证 model 别名序列化。
func TestModelRequestConfig_MarshalJSON_ModelAlias(t *testing.T) {
	cfg := NewModelRequestConfig(WithModelName("gpt-4"))
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	// JSON 中应使用 "model" 键名，不是 "model_name"
	if result["model"] != "gpt-4" {
		t.Errorf("model = %v, want %q", result["model"], "gpt-4")
	}
	if _, ok := result["model_name"]; ok {
		t.Error("不应存在 model_name 键，应使用 model")
	}
}

// TestModelRequestConfig_UnmarshalJSON_ModelAlias 验证 model 别名反序列化。
func TestModelRequestConfig_UnmarshalJSON_ModelAlias(t *testing.T) {
	jsonStr := `{"model":"gpt-4","temperature":0.7,"top_p":0.5}`
	var cfg ModelRequestConfig
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if cfg.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", cfg.ModelName, "gpt-4")
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", cfg.Temperature)
	}
	if cfg.TopP != 0.5 {
		t.Errorf("TopP = %f, want 0.5", cfg.TopP)
	}
}

// TestModelRequestConfig_Extra_RoundTrip 验证 Extra 字段 RoundTrip。
func TestModelRequestConfig_Extra_RoundTrip(t *testing.T) {
	cfg := NewModelRequestConfig(
		WithModelName("gpt-4"),
		WithRequestExtra(map[string]any{"custom_param": 123}),
	)

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored ModelRequestConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", restored.ModelName, "gpt-4")
	}
	if restored.Extra["custom_param"] != float64(123) {
		t.Errorf("Extra[custom_param] = %v, want 123", restored.Extra["custom_param"])
	}
}
