package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewContextEngineConfig 测试 NewContextEngineConfig 构造函数
func TestNewContextEngineConfig(t *testing.T) {
	config := NewContextEngineConfig()

	// int 字段默认为 0（不限）
	if config.MaxContextMessageNum != 0 {
		t.Errorf("MaxContextMessageNum 应为 0，实际 %d", config.MaxContextMessageNum)
	}
	if config.DefaultWindowMessageNum != 0 {
		t.Errorf("DefaultWindowMessageNum 应为 0，实际 %d", config.DefaultWindowMessageNum)
	}
	if config.DefaultWindowRoundNum != 0 {
		t.Errorf("DefaultWindowRoundNum 应为 0，实际 %d", config.DefaultWindowRoundNum)
	}
	if config.ContextWindowTokens != 0 {
		t.Errorf("ContextWindowTokens 应为 0，实际 %d", config.ContextWindowTokens)
	}

	// bool 字段默认为 false
	if config.EnableKVCacheRelease {
		t.Error("EnableKVCacheRelease 应为 false")
	}
	if config.EnableReload {
		t.Error("EnableReload 应为 false")
	}
	if config.EnableTiktokenCounter {
		t.Error("EnableTiktokenCounter 应为 false")
	}

	// string 字段默认为空串
	if config.ModelName != "" {
		t.Errorf("ModelName 应为空串，实际 %q", config.ModelName)
	}

	// map 字段应为空 map（非 nil）
	if config.ModelContextWindowTokens == nil {
		t.Error("ModelContextWindowTokens 应为空 map，不应为 nil")
	}
	if len(config.ModelContextWindowTokens) != 0 {
		t.Errorf("ModelContextWindowTokens 长度应为 0，实际 %d", len(config.ModelContextWindowTokens))
	}
}

// TestNewContextEngineConfig_JSON序列化 测试 JSON 序列化和反序列化
func TestNewContextEngineConfig_JSON序列化(t *testing.T) {
	config := NewContextEngineConfig()

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	// model_context_window_tokens 应为空对象而非 null
	mct, ok := parsed["model_context_window_tokens"]
	if !ok {
		t.Error("JSON 中应包含 model_context_window_tokens 字段")
	}
	mctMap, ok := mct.(map[string]any)
	if !ok {
		t.Errorf("model_context_window_tokens 应为对象，实际类型 %T", mct)
	}
	if len(mctMap) != 0 {
		t.Errorf("model_context_window_tokens 应为空对象，实际 %v", mctMap)
	}
}

// TestNewContextEngineConfig_JSON反序列化 测试从 JSON 反序列化配置
func TestNewContextEngineConfig_JSON反序列化(t *testing.T) {
	jsonStr := `{
		"max_context_message_num": 200,
		"default_window_message_num": 50,
		"default_window_round_num": 10,
		"enable_kv_cache_release": true,
		"enable_reload": true,
		"enable_tiktoken_counter": false,
		"context_window_tokens": 128000,
		"model_name": "qwen-max",
		"model_context_window_tokens": {"qwen-max": 32768, "qwen-plus": 131072}
	}`

	var config ContextEngineConfig
	if err := json.Unmarshal([]byte(jsonStr), &config); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	if config.MaxContextMessageNum != 200 {
		t.Errorf("MaxContextMessageNum 应为 200，实际 %d", config.MaxContextMessageNum)
	}
	if config.DefaultWindowMessageNum != 50 {
		t.Errorf("DefaultWindowMessageNum 应为 50，实际 %d", config.DefaultWindowMessageNum)
	}
	if config.DefaultWindowRoundNum != 10 {
		t.Errorf("DefaultWindowRoundNum 应为 10，实际 %d", config.DefaultWindowRoundNum)
	}
	if !config.EnableKVCacheRelease {
		t.Error("EnableKVCacheRelease 应为 true")
	}
	if !config.EnableReload {
		t.Error("EnableReload 应为 true")
	}
	if config.EnableTiktokenCounter {
		t.Error("EnableTiktokenCounter 应为 false")
	}
	if config.ContextWindowTokens != 128000 {
		t.Errorf("ContextWindowTokens 应为 128000，实际 %d", config.ContextWindowTokens)
	}
	if config.ModelName != "qwen-max" {
		t.Errorf("ModelName 应为 qwen-max，实际 %q", config.ModelName)
	}
	if len(config.ModelContextWindowTokens) != 2 {
		t.Errorf("ModelContextWindowTokens 长度应为 2，实际 %d", len(config.ModelContextWindowTokens))
	}
	if config.ModelContextWindowTokens["qwen-max"] != 32768 {
		t.Errorf("ModelContextWindowTokens[qwen-max] 应为 32768，实际 %d", config.ModelContextWindowTokens["qwen-max"])
	}
}

// TestContextEngineConfig_Validate_默认值通过 测试默认配置通过校验
func TestContextEngineConfig_Validate_默认值通过(t *testing.T) {
	config := NewContextEngineConfig()
	if err := config.Validate(); err != nil {
		t.Errorf("默认配置应通过校验，实际错误: %v", err)
	}
}

// TestContextEngineConfig_Validate_合法值通过 测试合法配置通过校验
func TestContextEngineConfig_Validate_合法值通过(t *testing.T) {
	config := ContextEngineConfig{
		MaxContextMessageNum:     200,
		DefaultWindowMessageNum:  50,
		DefaultWindowRoundNum:    10,
		EnableKVCacheRelease:     true,
		EnableReload:             true,
		EnableTiktokenCounter:    true,
		ContextWindowTokens:      128000,
		ModelName:                "qwen-max",
		ModelContextWindowTokens: map[string]int{"qwen-max": 32768},
	}
	if err := config.Validate(); err != nil {
		t.Errorf("合法配置应通过校验，实际错误: %v", err)
	}
}

// TestContextEngineConfig_Validate_负数失败 测试 int 字段为负数时校验失败
func TestContextEngineConfig_Validate_负数失败(t *testing.T) {
	tests := []struct {
		name   string
		config ContextEngineConfig
		field  string
	}{
		{
			name:   "MaxContextMessageNum 为负",
			config: ContextEngineConfig{MaxContextMessageNum: -1, ModelContextWindowTokens: map[string]int{}},
			field:  "max_context_message_num",
		},
		{
			name:   "DefaultWindowMessageNum 为负",
			config: ContextEngineConfig{DefaultWindowMessageNum: -1, ModelContextWindowTokens: map[string]int{}},
			field:  "default_window_message_num",
		},
		{
			name:   "DefaultWindowRoundNum 为负",
			config: ContextEngineConfig{DefaultWindowRoundNum: -1, ModelContextWindowTokens: map[string]int{}},
			field:  "default_window_round_num",
		},
		{
			name:   "ContextWindowTokens 为负",
			config: ContextEngineConfig{ContextWindowTokens: -1, ModelContextWindowTokens: map[string]int{}},
			field:  "context_window_tokens",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err == nil {
				t.Errorf("%s 为负数时应返回错误", tt.field)
			}
		})
	}
}

// TestContextEngineConfig_Validate_mapValue非正失败 测试 ModelContextWindowTokens value <= 0 时校验失败
func TestContextEngineConfig_Validate_mapValue非正失败(t *testing.T) {
	tests := []struct {
		name   string
		config ContextEngineConfig
	}{
		{
			name: "value 为 0",
			config: ContextEngineConfig{
				ModelContextWindowTokens: map[string]int{"qwen-max": 0},
			},
		},
		{
			name: "value 为负数",
			config: ContextEngineConfig{
				ModelContextWindowTokens: map[string]int{"qwen-max": -1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err == nil {
				t.Error("ModelContextWindowTokens value <= 0 时应返回错误")
			}
		})
	}
}

// TestContextEngineConfig_Validate_mapValue正数通过 测试 ModelContextWindowTokens value > 0 时校验通过
func TestContextEngineConfig_Validate_mapValue正数通过(t *testing.T) {
	config := ContextEngineConfig{
		ModelContextWindowTokens: map[string]int{"qwen-max": 32768, "qwen-plus": 131072},
	}
	if err := config.Validate(); err != nil {
		t.Errorf("ModelContextWindowTokens value > 0 时应通过校验，实际错误: %v", err)
	}
}

// TestContextEngineConfig_值类型字段 测试结构体为值类型，赋值时复制而非共享
func TestContextEngineConfig_值类型字段(t *testing.T) {
	config1 := ContextEngineConfig{
		MaxContextMessageNum:     200,
		ModelContextWindowTokens: map[string]int{"qwen-max": 32768},
	}
	config2 := config1

	// 修改 config2 不影响 config1（值类型复制）
	config2.MaxContextMessageNum = 100
	if config1.MaxContextMessageNum != 200 {
		t.Errorf("值类型赋值应为复制，config1.MaxContextMessageNum 应仍为 200，实际 %d", config1.MaxContextMessageNum)
	}

	// 注意：map 是引用类型，config2.ModelContextWindowTokens 与 config1 共享底层数据
	// 这是 Go 的标准行为，与 Python 的 Pydantic model_copy(update) 语义不同
}
