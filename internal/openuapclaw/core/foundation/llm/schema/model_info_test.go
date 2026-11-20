package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── BaseModelInfo 测试 ────────────────────────────

// TestNewBaseModelInfo 验证构造函数默认值。
func TestNewBaseModelInfo(t *testing.T) {
	info := NewBaseModelInfo("https://api.openai.com")
	if info.APIKey != "" {
		t.Errorf("APIKey = %q, want 空", info.APIKey)
	}
	if info.APIBase != "https://api.openai.com" {
		t.Errorf("APIBase = %q, want %q", info.APIBase, "https://api.openai.com")
	}
	if info.ModelName != "" {
		t.Errorf("ModelName = %q, want 空", info.ModelName)
	}
	if info.Temperature != 0.95 {
		t.Errorf("Temperature = %f, want 0.95", info.Temperature)
	}
	if info.TopP != 0.1 {
		t.Errorf("TopP = %f, want 0.1", info.TopP)
	}
	if info.Streaming {
		t.Error("Streaming 默认应为 false")
	}
	if info.Timeout != 60 {
		t.Errorf("Timeout = %d, want 60", info.Timeout)
	}
	if info.CustomHeaders != nil {
		t.Error("CustomHeaders 默认应为 nil")
	}
	if info.Extra != nil {
		t.Error("Extra 默认应为 nil")
	}
}

// TestNewBaseModelInfo_WithOptions 验证选项函数生效。
func TestNewBaseModelInfo_WithOptions(t *testing.T) {
	info := NewBaseModelInfo("https://api.openai.com",
		WithAPIKey("sk-xxx"),
		WithModelNameForInfo("gpt-4"),
		WithTemperatureForInfo(0.5),
		WithTopPForInfo(0.9),
		WithStreaming(true),
		WithTimeoutForInfo(30),
		WithCustomHeadersForInfo(map[string]string{"X-Custom": "val"}),
		WithInfoExtra(map[string]any{"extra_field": "extra_val"}),
	)
	if info.APIKey != "sk-xxx" {
		t.Errorf("APIKey = %q, want %q", info.APIKey, "sk-xxx")
	}
	if info.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", info.ModelName, "gpt-4")
	}
	if info.Temperature != 0.5 {
		t.Errorf("Temperature = %f, want 0.5", info.Temperature)
	}
	if info.TopP != 0.9 {
		t.Errorf("TopP = %f, want 0.9", info.TopP)
	}
	if !info.Streaming {
		t.Error("Streaming 应为 true")
	}
	if info.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", info.Timeout)
	}
	if info.CustomHeaders["X-Custom"] != "val" {
		t.Error("CustomHeaders 未正确设置")
	}
	if info.Extra["extra_field"] != "extra_val" {
		t.Error("Extra 未正确设置")
	}
}

// TestBaseModelInfo_Validate_EmptyAPIBase 验证 api_base 为空报错。
func TestBaseModelInfo_Validate_EmptyAPIBase(t *testing.T) {
	info := &BaseModelInfo{APIBase: ""}
	err := info.Validate()
	if err == nil {
		t.Error("期望 api_base 为空时报错")
	}
}

// TestBaseModelInfo_Validate_InvalidTimeout 验证 timeout ≤ 0 报错。
func TestBaseModelInfo_Validate_InvalidTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout int
		wantErr bool
	}{
		{"timeout 为 0", 0, true},
		{"timeout 为负数", -1, true},
		{"timeout 有效", 60, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &BaseModelInfo{APIBase: "https://api.openai.com", Timeout: tt.timeout}
			err := info.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestBaseModelInfo_MarshalJSON_ModelAlias 验证 model 别名序列化。
func TestBaseModelInfo_MarshalJSON_ModelAlias(t *testing.T) {
	info := NewBaseModelInfo("https://api.openai.com", WithModelNameForInfo("gpt-4"))
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	// JSON 中应使用 "model" 键名
	if result["model"] != "gpt-4" {
		t.Errorf("model = %v, want %q", result["model"], "gpt-4")
	}
	if _, ok := result["model_name"]; ok {
		t.Error("不应存在 model_name 键，应使用 model")
	}
}

// TestBaseModelInfo_UnmarshalJSON_ModelAlias 验证 model 别名反序列化。
func TestBaseModelInfo_UnmarshalJSON_ModelAlias(t *testing.T) {
	jsonStr := `{"api_base":"https://api.openai.com","model":"gpt-4","temperature":0.8}`
	var info BaseModelInfo
	if err := json.Unmarshal([]byte(jsonStr), &info); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if info.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", info.ModelName, "gpt-4")
	}
	if info.APIBase != "https://api.openai.com" {
		t.Errorf("APIBase = %q, want %q", info.APIBase, "https://api.openai.com")
	}
	if info.Temperature != 0.8 {
		t.Errorf("Temperature = %f, want 0.8", info.Temperature)
	}
}

// TestBaseModelInfo_MarshalJSON_StreamAlias 验证 stream 别名序列化。
func TestBaseModelInfo_MarshalJSON_StreamAlias(t *testing.T) {
	info := NewBaseModelInfo("https://api.openai.com", WithStreaming(true))
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	// JSON 中应使用 "stream" 键名
	if result["stream"] != true {
		t.Errorf("stream = %v, want true", result["stream"])
	}
	if _, ok := result["streaming"]; ok {
		t.Error("不应存在 streaming 键，应使用 stream")
	}
}

// TestBaseModelInfo_UnmarshalJSON_StreamAlias 验证 stream 别名反序列化。
func TestBaseModelInfo_UnmarshalJSON_StreamAlias(t *testing.T) {
	jsonStr := `{"api_base":"https://api.openai.com","stream":true}`
	var info BaseModelInfo
	if err := json.Unmarshal([]byte(jsonStr), &info); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if !info.Streaming {
		t.Error("Streaming 应为 true")
	}
}

// TestBaseModelInfo_Extra_RoundTrip 验证 Extra 字段 RoundTrip。
func TestBaseModelInfo_Extra_RoundTrip(t *testing.T) {
	info := NewBaseModelInfo("https://api.openai.com",
		WithModelNameForInfo("gpt-4"),
		WithInfoExtra(map[string]any{"custom_param": 123}),
	)

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored BaseModelInfo
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.APIBase != "https://api.openai.com" {
		t.Errorf("APIBase = %q, want %q", restored.APIBase, "https://api.openai.com")
	}
	if restored.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", restored.ModelName, "gpt-4")
	}
	if restored.Extra["custom_param"] != float64(123) {
		t.Errorf("Extra[custom_param] = %v, want 123", restored.Extra["custom_param"])
	}
}

// TestBaseModelInfo_MarshalJSON_WithExtra 验证 Extra 字段合并输出。
func TestBaseModelInfo_MarshalJSON_WithExtra(t *testing.T) {
	info := NewBaseModelInfo("https://api.openai.com",
		WithInfoExtra(map[string]any{
			"custom_field":  "custom_value",
			"another_field": 42,
		}),
	)
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	// 验证标准字段存在
	if result["api_base"] != "https://api.openai.com" {
		t.Errorf("api_base = %v, want %q", result["api_base"], "https://api.openai.com")
	}
	// 验证 Extra 字段合并到顶层
	if result["custom_field"] != "custom_value" {
		t.Errorf("custom_field = %v, want %q", result["custom_field"], "custom_value")
	}
	if result["another_field"] != float64(42) {
		t.Errorf("another_field = %v, want 42", result["another_field"])
	}
}

// TestBaseModelInfo_UnmarshalJSON_WithExtra 验证未知 key 存入 Extra。
func TestBaseModelInfo_UnmarshalJSON_WithExtra(t *testing.T) {
	jsonStr := `{
		"api_base": "https://api.openai.com",
		"model": "gpt-4",
		"temperature": 0.8,
		"custom_field": "custom_value",
		"another_field": 42
	}`
	var info BaseModelInfo
	if err := json.Unmarshal([]byte(jsonStr), &info); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if info.APIBase != "https://api.openai.com" {
		t.Errorf("APIBase = %q, want %q", info.APIBase, "https://api.openai.com")
	}
	if info.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", info.ModelName, "gpt-4")
	}
	if info.Extra == nil {
		t.Fatal("Extra 不应为 nil")
	}
	if info.Extra["custom_field"] != "custom_value" {
		t.Errorf("Extra[custom_field] = %v, want %q", info.Extra["custom_field"], "custom_value")
	}
}

// ──────────────────────────── ModelConfig 测试 ────────────────────────────

// TestNewModelConfig 验证 ModelConfig 构造函数。
func TestNewModelConfig(t *testing.T) {
	info := NewBaseModelInfo("https://api.openai.com", WithModelNameForInfo("gpt-4"))
	cfg := NewModelConfig("OpenAI", *info)
	if cfg.ModelProvider != "OpenAI" {
		t.Errorf("ModelProvider = %q, want %q", cfg.ModelProvider, "OpenAI")
	}
	if cfg.ModelInfo.APIBase != "https://api.openai.com" {
		t.Errorf("ModelInfo.APIBase = %q, want %q", cfg.ModelInfo.APIBase, "https://api.openai.com")
	}
	if cfg.ModelInfo.ModelName != "gpt-4" {
		t.Errorf("ModelInfo.ModelName = %q, want %q", cfg.ModelInfo.ModelName, "gpt-4")
	}
}

// TestModelConfig_RoundTrip 验证 ModelConfig 序列化→反序列化一致性。
func TestModelConfig_RoundTrip(t *testing.T) {
	info := NewBaseModelInfo("https://api.openai.com",
		WithAPIKey("sk-xxx"),
		WithModelNameForInfo("gpt-4"),
		WithTemperatureForInfo(0.7),
		WithTopPForInfo(0.9),
		WithStreaming(true),
		WithTimeoutForInfo(30),
	)
	cfg := NewModelConfig("OpenAI", *info)

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored ModelConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ModelProvider != "OpenAI" {
		t.Errorf("ModelProvider = %q, want %q", restored.ModelProvider, "OpenAI")
	}
	if restored.ModelInfo.APIBase != "https://api.openai.com" {
		t.Errorf("APIBase = %q, want %q", restored.ModelInfo.APIBase, "https://api.openai.com")
	}
	if restored.ModelInfo.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", restored.ModelInfo.ModelName, "gpt-4")
	}
	if restored.ModelInfo.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", restored.ModelInfo.Temperature)
	}
	if !restored.ModelInfo.Streaming {
		t.Error("Streaming 应为 true")
	}
	if restored.ModelInfo.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", restored.ModelInfo.Timeout)
	}
}
