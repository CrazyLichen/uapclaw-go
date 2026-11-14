package models

import (
	"strings"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewModelPoolEntry 测试构造函数 + ModelID 自动生成
func TestNewModelPoolEntry(t *testing.T) {
	entry := NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI")
	if entry.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", entry.ModelName, "gpt-4")
	}
	if entry.APIKey != "key-1" {
		t.Errorf("APIKey = %q, want %q", entry.APIKey, "key-1")
	}
	if entry.APIBaseURL != "https://api.openai.com/v1" {
		t.Errorf("APIBaseURL = %q, want %q", entry.APIBaseURL, "https://api.openai.com/v1")
	}
	if entry.APIProvider != "OpenAI" {
		t.Errorf("APIProvider = %q, want %q", entry.APIProvider, "OpenAI")
	}
	if entry.ModelID == "" {
		t.Error("ModelID 不应为空，应自动生成 UUID")
	}
	if len(entry.ModelID) != 36 { // UUID 标准长度
		t.Errorf("ModelID 长度 = %d, want 36", len(entry.ModelID))
	}

	// 测试选项函数
	entry2 := NewModelPoolEntry("qwen", "key-2", "https://dashscope.aliyuncs.com", "DashScope",
		WithDescription("测试描述"),
		WithMetadata(map[string]any{"weight": 1.0}),
	)
	if entry2.Description != "测试描述" {
		t.Errorf("Description = %q, want %q", entry2.Description, "测试描述")
	}
	if entry2.Metadata == nil {
		t.Error("Metadata 不应为 nil")
	}

	// 两个实例的 ModelID 应不同
	if entry.ModelID == entry2.ModelID {
		t.Error("两个实例的 ModelID 不应相同")
	}
}

// TestModelPoolEntry_ToTeamModelConfig 测试基本字段映射
func TestModelPoolEntry_ToTeamModelConfig(t *testing.T) {
	entry := NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI")
	cfg := entry.ToTeamModelConfig()

	// 验证 ModelClientConfig
	clientCfg := cfg.ModelClientConfig
	if clientCfg.ClientID != entry.ModelID {
		t.Errorf("ClientID = %q, want %q", clientCfg.ClientID, entry.ModelID)
	}
	if clientCfg.ClientProvider != "OpenAI" {
		t.Errorf("ClientProvider = %q, want %q", clientCfg.ClientProvider, "OpenAI")
	}
	if clientCfg.APIKey != "key-1" {
		t.Errorf("APIKey = %q, want %q", clientCfg.APIKey, "key-1")
	}
	if clientCfg.APIBase != "https://api.openai.com/v1" {
		t.Errorf("APIBase = %q, want %q", clientCfg.APIBase, "https://api.openai.com/v1")
	}

	// 验证 ModelRequestConfig
	if cfg.ModelRequestConfig == nil {
		t.Fatal("ModelRequestConfig 不应为 nil")
	}
	if cfg.ModelRequestConfig.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", cfg.ModelRequestConfig.ModelName, "gpt-4")
	}
}

// TestModelPoolEntry_ToTeamModelConfig_MetadataMerge 测试 client/request 子字典合并
func TestModelPoolEntry_ToTeamModelConfig_MetadataMerge(t *testing.T) {
	entry := NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI",
		WithMetadata(map[string]any{
			"client": map[string]any{
				"timeout":     30.0,
				"max_retries": 5,
				"verify_ssl":  false,
				"ssl_cert":    "/path/to/cert.pem",
				"custom_headers": map[string]any{
					"X-Custom": "value",
				},
			},
			"request": map[string]any{
				"temperature": 0.5,
				"top_p":       0.9,
				"max_tokens":  4096,
				"stop":        "END",
			},
		}),
	)
	cfg := entry.ToTeamModelConfig()

	// 验证 client 合并
	clientCfg := cfg.ModelClientConfig
	if clientCfg.Timeout != 30.0 {
		t.Errorf("Timeout = %f, want 30.0", clientCfg.Timeout)
	}
	if clientCfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", clientCfg.MaxRetries)
	}
	if clientCfg.VerifySSL != false {
		t.Errorf("VerifySSL = %v, want false", clientCfg.VerifySSL)
	}
	if clientCfg.SSLCert != "/path/to/cert.pem" {
		t.Errorf("SSLCert = %q, want %q", clientCfg.SSLCert, "/path/to/cert.pem")
	}
	if clientCfg.CustomHeaders == nil || clientCfg.CustomHeaders["X-Custom"] != "value" {
		t.Errorf("CustomHeaders[X-Custom] = %q, want %q", clientCfg.CustomHeaders["X-Custom"], "value")
	}

	// 验证池条目显式字段覆盖 metadata 中的同名键
	if clientCfg.ClientID != entry.ModelID {
		t.Errorf("ClientID 应从池条目继承 = %q, got %q", entry.ModelID, clientCfg.ClientID)
	}
	if clientCfg.APIKey != "key-1" {
		t.Errorf("APIKey 应从池条目继承 = %q, got %q", "key-1", clientCfg.APIKey)
	}

	// 验证 request 合并
	if cfg.ModelRequestConfig == nil {
		t.Fatal("ModelRequestConfig 不应为 nil")
	}
	reqCfg := cfg.ModelRequestConfig
	if reqCfg.Temperature == nil || *reqCfg.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", reqCfg.Temperature)
	}
	if reqCfg.TopP == nil || *reqCfg.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", reqCfg.TopP)
	}
	if reqCfg.MaxTokens == nil || *reqCfg.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %v, want 4096", reqCfg.MaxTokens)
	}
	if reqCfg.Stop == nil || *reqCfg.Stop != "END" {
		t.Errorf("Stop = %v, want END", reqCfg.Stop)
	}
	if reqCfg.ModelName != "gpt-4" {
		t.Errorf("ModelName 应从池条目继承 = %q, got %q", "gpt-4", reqCfg.ModelName)
	}
}

// TestModelPoolEntry_ToTeamModelConfig_MetadataMerge_额外字段 测试 Extra 字段处理
func TestModelPoolEntry_ToTeamModelConfig_MetadataMerge_额外字段(t *testing.T) {
	entry := NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI",
		WithMetadata(map[string]any{
			"client": map[string]any{
				"custom_field": "hello",
			},
			"request": map[string]any{
				"extra_param": 42,
			},
		}),
	)
	cfg := entry.ToTeamModelConfig()

	// 验证 client Extra 包含自定义字段
	if cfg.ModelClientConfig.Extra == nil {
		t.Error("ClientConfig.Extra 不应为 nil")
	} else if v, ok := cfg.ModelClientConfig.Extra["custom_field"]; !ok || v != "hello" {
		t.Errorf("ClientConfig.Extra[custom_field] = %v, want hello", v)
	}

	// 验证 request Extra 包含自定义字段
	if cfg.ModelRequestConfig == nil {
		t.Fatal("ModelRequestConfig 不应为 nil")
	}
	if cfg.ModelRequestConfig.Extra == nil {
		t.Error("RequestConfig.Extra 不应为 nil")
	} else if v, ok := cfg.ModelRequestConfig.Extra["extra_param"]; !ok {
		t.Errorf("RequestConfig.Extra[extra_param] 不存在")
	} else {
		_ = v // 使用 v 避免编译错误
	}
}

// TestModelPoolEntry_ToTeamModelConfig_空Metadata 测试无 metadata 时的默认行为
func TestModelPoolEntry_ToTeamModelConfig_空Metadata(t *testing.T) {
	entry := NewModelPoolEntry("qwen", "key-2", "https://dashscope.aliyuncs.com", "DashScope")
	cfg := entry.ToTeamModelConfig()

	// 默认值应来自 NewModelClientConfig
	if cfg.ModelClientConfig.Timeout != 360.0 {
		t.Errorf("Timeout = %f, want 360.0（默认，Python: schema/config.py:145）", cfg.ModelClientConfig.Timeout)
	}
	if cfg.ModelClientConfig.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3（默认）", cfg.ModelClientConfig.MaxRetries)
	}
	if cfg.ModelClientConfig.VerifySSL != true {
		t.Errorf("VerifySSL = %v, want true（默认）", cfg.ModelClientConfig.VerifySSL)
	}

	// 默认值应来自 NewModelRequestConfig
	if cfg.ModelRequestConfig == nil {
		t.Fatal("ModelRequestConfig 不应为 nil")
	}
	// C-D3 后采样参数无默认值（nil = 未设置，不下发；Python schema/config.py:262-266）
	if cfg.ModelRequestConfig.Temperature != nil {
		t.Errorf("Temperature 默认应为 nil，实际 %v", *cfg.ModelRequestConfig.Temperature)
	}
	if cfg.ModelRequestConfig.TopP != nil {
		t.Errorf("TopP 默认应为 nil，实际 %v", *cfg.ModelRequestConfig.TopP)
	}
}

// TestModelRouterConfig_Validate_正常 测试合法配置
func TestModelRouterConfig_Validate_正常(t *testing.T) {
	r := &ModelRouterConfig{
		APIBaseURL:  "https://openrouter.ai/v1",
		APIKey:      "key-1",
		APIProvider: "OpenRouter",
		ModelNames:  []string{"gpt-4", "claude-3"},
	}
	if err := r.Validate(); err != nil {
		t.Errorf("Validate() 不应报错, got: %v", err)
	}
}

// TestModelRouterConfig_Validate_空白 测试空白 model_name 错误
func TestModelRouterConfig_Validate_空白(t *testing.T) {
	r := &ModelRouterConfig{
		APIBaseURL:  "https://openrouter.ai/v1",
		APIKey:      "key-1",
		APIProvider: "OpenRouter",
		ModelNames:  []string{"gpt-4", "", "  "},
	}
	err := r.Validate()
	if err == nil {
		t.Fatal("Validate() 应报错，空白 model_name")
	}
	if !strings.Contains(err.Error(), "空白") {
		t.Errorf("错误信息应包含'空白', got: %v", err)
	}
}

// TestModelRouterConfig_Validate_重复 测试重复 model_name 错误
func TestModelRouterConfig_Validate_重复(t *testing.T) {
	r := &ModelRouterConfig{
		APIBaseURL:  "https://openrouter.ai/v1",
		APIKey:      "key-1",
		APIProvider: "OpenRouter",
		ModelNames:  []string{"gpt-4", "claude-3", "gpt-4"},
	}
	err := r.Validate()
	if err == nil {
		t.Fatal("Validate() 应报错，重复 model_name")
	}
	if !strings.Contains(err.Error(), "重复") {
		t.Errorf("错误信息应包含'重复', got: %v", err)
	}
}

// TestModelRouterConfig_Validate_空列表 测试空 model_names 错误
func TestModelRouterConfig_Validate_空列表(t *testing.T) {
	r := &ModelRouterConfig{
		APIBaseURL:  "https://openrouter.ai/v1",
		APIKey:      "key-1",
		APIProvider: "OpenRouter",
		ModelNames:  []string{},
	}
	err := r.Validate()
	if err == nil {
		t.Fatal("Validate() 应报错，空 model_names")
	}
}

// TestModelRouterConfig_ToPoolEntries 测试展开验证
func TestModelRouterConfig_ToPoolEntries(t *testing.T) {
	r := &ModelRouterConfig{
		APIBaseURL:  "https://openrouter.ai/v1",
		APIKey:      "key-1",
		APIProvider: "OpenRouter",
		ModelNames:  []string{"gpt-4", "claude-3"},
		Metadata:    map[string]any{"weight": 1.0},
	}
	entries := r.ToPoolEntries()

	if len(entries) != 2 {
		t.Fatalf("ToPoolEntries 长度 = %d, want 2", len(entries))
	}

	// 验证第一个条目
	if entries[0].ModelName != "gpt-4" {
		t.Errorf("entries[0].ModelName = %q, want %q", entries[0].ModelName, "gpt-4")
	}
	if entries[0].APIKey != "key-1" {
		t.Errorf("entries[0].APIKey = %q, want %q", entries[0].APIKey, "key-1")
	}
	if entries[0].APIBaseURL != "https://openrouter.ai/v1" {
		t.Errorf("entries[0].APIBaseURL = %q, want %q", entries[0].APIBaseURL, "https://openrouter.ai/v1")
	}
	if entries[0].APIProvider != "OpenRouter" {
		t.Errorf("entries[0].APIProvider = %q, want %q", entries[0].APIProvider, "OpenRouter")
	}
	if entries[0].ModelID == "" {
		t.Error("entries[0].ModelID 不应为空")
	}

	// 验证第二个条目
	if entries[1].ModelName != "claude-3" {
		t.Errorf("entries[1].ModelName = %q, want %q", entries[1].ModelName, "claude-3")
	}

	// 验证 ModelID 各自独立
	if entries[0].ModelID == entries[1].ModelID {
		t.Error("两个条目的 ModelID 不应相同")
	}

	// 验证 metadata 深拷贝（修改展开条目的 metadata 不影响原始）
	entries[0].Metadata["weight"] = 2.0
	if r.Metadata["weight"] == 2.0 {
		t.Error("修改展开条目的 Metadata 不应影响原始路由器的 Metadata")
	}
}

// TestInheritPoolIDs_完全匹配 测试完全匹配场景
func TestInheritPoolIDs_完全匹配(t *testing.T) {
	current := []ModelPoolEntry{
		*NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI", WithModelID("id-1")),
		*NewModelPoolEntry("claude-3", "key-2", "https://api.anthropic.com", "Anthropic", WithModelID("id-2")),
	}
	// 新池条目与旧池完全相同（除 model_id）
	newPool := []ModelPoolEntry{
		*NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI", WithModelID("new-id-1")),
		*NewModelPoolEntry("claude-3", "key-2", "https://api.anthropic.com", "Anthropic", WithModelID("new-id-2")),
	}

	result := InheritPoolIDs(current, newPool)
	if len(result) != 2 {
		t.Fatalf("结果长度 = %d, want 2", len(result))
	}
	if result[0].ModelID != "id-1" {
		t.Errorf("result[0].ModelID = %q, want %q（从 current 继承）", result[0].ModelID, "id-1")
	}
	if result[1].ModelID != "id-2" {
		t.Errorf("result[1].ModelID = %q, want %q（从 current 继承）", result[1].ModelID, "id-2")
	}
}

// TestInheritPoolIDs_无匹配 测试无匹配场景
func TestInheritPoolIDs_无匹配(t *testing.T) {
	current := []ModelPoolEntry{
		*NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI", WithModelID("id-1")),
	}
	newPool := []ModelPoolEntry{
		*NewModelPoolEntry("claude-3", "key-2", "https://api.anthropic.com", "Anthropic", WithModelID("new-id-1")),
	}

	result := InheritPoolIDs(current, newPool)
	if len(result) != 1 {
		t.Fatalf("结果长度 = %d, want 1", len(result))
	}
	// 无匹配时保留新条目自己的 model_id
	if result[0].ModelID != "new-id-1" {
		t.Errorf("result[0].ModelID = %q, want %q（保留新条目自己的 ID）", result[0].ModelID, "new-id-1")
	}
}

// TestInheritPoolIDs_部分匹配 测试部分匹配场景
func TestInheritPoolIDs_部分匹配(t *testing.T) {
	current := []ModelPoolEntry{
		*NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI", WithModelID("id-1")),
		*NewModelPoolEntry("claude-3", "key-2", "https://api.anthropic.com", "Anthropic", WithModelID("id-2")),
	}
	newPool := []ModelPoolEntry{
		*NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI", WithModelID("new-id-1")),        // 匹配
		*NewModelPoolEntry("qwen", "key-3", "https://dashscope.aliyuncs.com", "DashScope", WithModelID("new-id-2")), // 不匹配
	}

	result := InheritPoolIDs(current, newPool)
	if len(result) != 2 {
		t.Fatalf("结果长度 = %d, want 2", len(result))
	}
	if result[0].ModelID != "id-1" {
		t.Errorf("result[0].ModelID = %q, want %q（从 current 继承）", result[0].ModelID, "id-1")
	}
	if result[1].ModelID != "new-id-2" {
		t.Errorf("result[1].ModelID = %q, want %q（保留新条目自己的 ID）", result[1].ModelID, "new-id-2")
	}
}

// TestInheritPoolIDs_顺序无关 测试顺序无关场景
func TestInheritPoolIDs_顺序无关(t *testing.T) {
	current := []ModelPoolEntry{
		*NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI", WithModelID("id-1")),
		*NewModelPoolEntry("claude-3", "key-2", "https://api.anthropic.com", "Anthropic", WithModelID("id-2")),
	}
	// 新池顺序不同，但内容相同
	newPool := []ModelPoolEntry{
		*NewModelPoolEntry("claude-3", "key-2", "https://api.anthropic.com", "Anthropic", WithModelID("new-id-2")),
		*NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI", WithModelID("new-id-1")),
	}

	result := InheritPoolIDs(current, newPool)
	if len(result) != 2 {
		t.Fatalf("结果长度 = %d, want 2", len(result))
	}
	if result[0].ModelID != "id-2" {
		t.Errorf("result[0].ModelID = %q, want %q（claude-3 从 current 继承）", result[0].ModelID, "id-2")
	}
	if result[1].ModelID != "id-1" {
		t.Errorf("result[1].ModelID = %q, want %q（gpt-4 从 current 继承）", result[1].ModelID, "id-1")
	}
}

// TestEntrySignature_排除ModelID 测试签名不包含 model_id
func TestEntrySignature_排除ModelID(t *testing.T) {
	entry1 := ModelPoolEntry{
		ModelName: "gpt-4", APIKey: "key-1", APIBaseURL: "https://api.openai.com/v1",
		APIProvider: "OpenAI", ModelID: "id-1",
	}
	entry2 := ModelPoolEntry{
		ModelName: "gpt-4", APIKey: "key-1", APIBaseURL: "https://api.openai.com/v1",
		APIProvider: "OpenAI", ModelID: "id-2", // 不同 model_id
	}
	if entrySignature(entry1) != entrySignature(entry2) {
		t.Error("签名应忽略 model_id，相同配置应有相同签名")
	}
}

// TestEntrySignature_配置变更 测试配置变更导致不同签名
func TestEntrySignature_配置变更(t *testing.T) {
	entry1 := ModelPoolEntry{
		ModelName: "gpt-4", APIKey: "key-1", APIBaseURL: "https://api.openai.com/v1",
		APIProvider: "OpenAI", ModelID: "id-1",
	}
	entry2 := ModelPoolEntry{
		ModelName: "gpt-4", APIKey: "key-2", APIBaseURL: "https://api.openai.com/v1", // 不同 api_key
		APIProvider: "OpenAI", ModelID: "id-1",
	}
	if entrySignature(entry1) == entrySignature(entry2) {
		t.Error("不同配置应有不同签名")
	}
}

// TestInheritPoolIDs_凭证轮换 测试凭证轮换产生新 ID
func TestInheritPoolIDs_凭证轮换(t *testing.T) {
	current := []ModelPoolEntry{
		*NewModelPoolEntry("gpt-4", "old-key", "https://api.openai.com/v1", "OpenAI", WithModelID("id-1")),
	}
	newPool := []ModelPoolEntry{
		*NewModelPoolEntry("gpt-4", "new-key", "https://api.openai.com/v1", "OpenAI", WithModelID("new-id-1")),
	}

	result := InheritPoolIDs(current, newPool)
	// 凭证轮换后签名不匹配，应保留新条目的 ID
	if result[0].ModelID != "new-id-1" {
		t.Errorf("凭证轮换后应保留新 ID = %q, got %q", "new-id-1", result[0].ModelID)
	}
}

// TestModelPoolEntry_ToTeamModelConfig_Metadata中同名键被覆盖 测试池条目字段覆盖 metadata 同名键
func TestModelPoolEntry_ToTeamModelConfig_Metadata中同名键被覆盖(t *testing.T) {
	entry := NewModelPoolEntry("gpt-4", "key-1", "https://api.openai.com/v1", "OpenAI",
		WithMetadata(map[string]any{
			"client": map[string]any{
				"api_key": "should-be-overridden",
				"timeout": 30.0,
			},
			"request": map[string]any{
				"model": "should-be-overridden",
			},
		}),
	)
	cfg := entry.ToTeamModelConfig()

	// api_key 应被池条目覆盖
	if cfg.ModelClientConfig.APIKey != "key-1" {
		t.Errorf("APIKey = %q, want %q（池条目覆盖）", cfg.ModelClientConfig.APIKey, "key-1")
	}
	// timeout 应从 metadata 合并
	if cfg.ModelClientConfig.Timeout != 30.0 {
		t.Errorf("Timeout = %f, want 30.0（从 metadata 合并）", cfg.ModelClientConfig.Timeout)
	}
	// model 应被池条目覆盖
	if cfg.ModelRequestConfig.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q（池条目覆盖）", cfg.ModelRequestConfig.ModelName, "gpt-4")
	}
}

// TestModelRouterConfig_ToPoolEntries_无Metadata 测试无 metadata 展开
func TestModelRouterConfig_ToPoolEntries_无Metadata(t *testing.T) {
	r := &ModelRouterConfig{
		APIBaseURL:  "https://openrouter.ai/v1",
		APIKey:      "key-1",
		APIProvider: "OpenRouter",
		ModelNames:  []string{"gpt-4"},
	}
	entries := r.ToPoolEntries()
	if len(entries) != 1 {
		t.Fatalf("ToPoolEntries 长度 = %d, want 1", len(entries))
	}
	if entries[0].Metadata != nil {
		t.Errorf("Metadata 应为 nil，got %v", entries[0].Metadata)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestToFloat64 测试 any→float64 类型转换
func TestToFloat64(t *testing.T) {
	tests := []struct {
		input    any
		expected float64
		ok       bool
	}{
		{float64(3.14), 3.14, true},
		{float32(2.5), 2.5, true},
		{int(42), 42.0, true},
		{int64(100), 100.0, true},
		{int32(7), 7.0, true},
		{"not a number", 0, false},
	}
	for _, tt := range tests {
		got, ok := toFloat64(tt.input)
		if ok != tt.ok {
			t.Errorf("toFloat64(%v) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if ok && got != tt.expected {
			t.Errorf("toFloat64(%v) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestToInt 测试 any→int 类型转换
func TestToInt(t *testing.T) {
	tests := []struct {
		input    any
		expected int
		ok       bool
	}{
		{int(42), 42, true},
		{int64(100), 100, true},
		{int32(7), 7, true},
		{float64(3.0), 3, true},
		{float32(2.0), 2, true},
		{"not a number", 0, false},
	}
	for _, tt := range tests {
		got, ok := toInt(tt.input)
		if ok != tt.ok {
			t.Errorf("toInt(%v) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if ok && got != tt.expected {
			t.Errorf("toInt(%v) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestEntrySignature 测试 entrySignature 分支
func TestEntrySignature(t *testing.T) {
	// 无 Metadata
	e1 := ModelPoolEntry{ModelName: "gpt-4", APIKey: "key", APIBaseURL: "base", APIProvider: "p"}
	sig1 := entrySignature(e1)
	if sig1 == "" {
		t.Error("entrySignature 不应为空")
	}
	// 有 Metadata
	e2 := ModelPoolEntry{
		ModelName: "gpt-4", APIKey: "key", APIBaseURL: "base", APIProvider: "p",
		Metadata: map[string]any{"temperature": 0.7},
	}
	sig2 := entrySignature(e2)
	if sig2 == "" {
		t.Error("有 Metadata 时 entrySignature 不应为空")
	}
}

// TestIntelliRouterDeployment_ToDeploymentDict_可选字段省略 可选字段 omit 非 null、tags[0] 填 fallback_tag。
// Python: pool.py:297-326 to_deployment_dict
func TestIntelliRouterDeployment_ToDeploymentDict_可选字段省略(t *testing.T) {
	id := "d1"
	dep := &IntelliRouterDeployment{
		ModelName: "deepseek-chat", APIKey: "sk-1", APIBase: "https://api.deepseek.com",
		Provider: "deepseek", ID: id, Tags: []string{"deepseek"},
	}
	got := dep.ToDeploymentDict()
	// 必填四键恒在
	if got["model_name"] != "deepseek-chat" || got["provider"] != "deepseek" ||
		got["api_key"] != "sk-1" || got["api_base"] != "https://api.deepseek.com" {
		t.Fatalf("必填键缺失: %v", got)
	}
	// tags 非空 → fallback_tag 用 tags[0]；id 保留
	if got["fallback_tag"] != "deepseek" || got["id"] != "d1" {
		t.Fatalf("tags/id 处理错误: %v", got)
	}
	// 未设置的可选键不得出现（verify_ssl 缺省时由客户端层回退路由器级）
	for _, k := range []string{"endpoint_profile", "custom_headers", "model_description", "tpm", "rpm", "timeout", "verify_ssl", "model_id", "request_defaults"} {
		if _, ok := got[k]; ok {
			t.Fatalf("可选键 %s 不应出现", k)
		}
	}
	// 空 tags 时保留显式 fallback_tag
	dep2 := &IntelliRouterDeployment{ModelName: "m", APIKey: "k", APIBase: "b", Provider: "p", FallbackTag: "ft"}
	if dep2.ToDeploymentDict()["fallback_tag"] != "ft" {
		t.Fatal("空 tags 时应保留显式 fallback_tag")
	}
	// 指针可选字段：显式设置才下发
	ssl := false
	dep3 := &IntelliRouterDeployment{ModelName: "m", APIKey: "k", APIBase: "b", Provider: "p", VerifySSL: &ssl, TPM: intPtrOf(1000)}
	got3 := dep3.ToDeploymentDict()
	if got3["verify_ssl"] != false || got3["tpm"] != 1000 {
		t.Fatalf("指针字段下发错误: %v", got3)
	}
}

// TestIntelliRouterDeployment_APIBase注释锚点 api_base 不带 /v1 的坑以字段注释承载（pool.py:262-276 原文），此处仅防回退改语义。
func TestIntelliRouterDeployment_必填Provider(t *testing.T) {
	dep := &IntelliRouterDeployment{ModelName: "m", APIKey: "k", APIBase: "b"}
	got := dep.ToDeploymentDict()
	if got["provider"] != "" {
		t.Fatal("provider 空值应原样下发（校验由 Config/Allocator 构造期做）")
	}
}

func intPtrOf(v int) *int { return &v }

// TestIntelliRouterConfig_ToPoolEntries 单条 "*" entry、client 快照、遗留键清理、缺省值。
// Python: pool.py:329-455 IntelliRouterConfig + to_pool_entries + _client_extra
func TestIntelliRouterConfig_ToPoolEntries(t *testing.T) {
	cfg := NewIntelliRouterConfig([]*IntelliRouterDeployment{{
		ModelName: "m1", APIKey: "k1", APIBase: "https://a.com", Provider: "deepseek",
	}})
	entries := cfg.ToPoolEntries()
	if len(entries) != 1 {
		t.Fatalf("必须展开为单条 entry，got %d", len(entries))
	}
	e := entries[0]
	if e.ModelName != INTELLI_ROUTER_UNIFIED_MODEL || e.APIProvider != INTELLI_ROUTER_PROVIDER {
		t.Fatalf("形状错误: %s/%s", e.ModelName, e.APIProvider)
	}
	if e.APIKey != "" || e.APIBaseURL != "" {
		t.Fatal("顶层凭证必须为空（凭证在 deployment 内）")
	}
	if e.Description != "IntelliRouter unified routing across 1 deployment(s)" {
		t.Fatalf("description 文案不匹配: %q", e.Description)
	}
	client := e.Metadata["client"].(map[string]any)
	router := client["intelli_router"].(map[string]any)
	// 路由器级缺省值（pool.py:359-375 Field defaults）
	if router["strategy"] != "simple-shuffle" {
		t.Fatalf("strategy 缺省: %v", router["strategy"])
	}
	if router["num_retries"] != 3 {
		t.Fatalf("num_retries 缺省: %v", router["num_retries"])
	}
	if router["timeout"] != float64(30.0) {
		t.Fatalf("timeout 缺省: %v", router["timeout"])
	}
	if client["verify_ssl"] != true {
		t.Fatalf("verify_ssl 缺省 true: %v", client["verify_ssl"])
	}
	deps := router["deployments"].([]any)
	d0 := deps[0].(map[string]any)
	if d0["route_id"] != "route_0" {
		t.Fatalf("route_id 缺省应为 route_{index}: %v", d0)
	}
	if d0["model_name"] != "m1" {
		t.Fatalf("deployment 键错误: %v", d0)
	}
	// 遗留 intelli_router_* 键被清理（pool.py:444-448）
	cfg2 := NewIntelliRouterConfig([]*IntelliRouterDeployment{cfg.Deployments[0]})
	cfg2.Metadata = map[string]any{
		"client": map[string]any{"intelli_router_api_key": "legacy", "timeout": 99},
	}
	c2 := cfg2.ToPoolEntries()[0].Metadata["client"].(map[string]any)
	if _, ok := c2["intelli_router_api_key"]; ok {
		t.Fatal("遗留 intelli_router_* 键应被丢弃")
	}
	if c2["timeout"] != 99 {
		t.Fatal("非遗留的 client 键应保留")
	}
	// 显式 Strategy 覆盖缺省
	cfg3 := NewIntelliRouterConfig([]*IntelliRouterDeployment{cfg.Deployments[0]})
	cfg3.Strategy = "ordered-failover"
	r3 := cfg3.ToPoolEntries()[0].Metadata["client"].(map[string]any)["intelli_router"].(map[string]any)
	if r3["strategy"] != "ordered-failover" {
		t.Fatalf("显式 strategy 应生效: %v", r3["strategy"])
	}
}

// TestIntelliRouterConfig_ValidateModelNames model_names 只能是 ["*"]。
// Python: _validate_model_names (pool.py:387-393)
func TestIntelliRouterConfig_ValidateModelNames(t *testing.T) {
	cfg := NewIntelliRouterConfig([]*IntelliRouterDeployment{{ModelName: "m", APIKey: "k", APIBase: "b", Provider: "p"}})
	if err := cfg.Validate(); err != nil {
		t.Fatalf("model_names 缺省应通过: %v", err)
	}
	cfg.ModelNames = []string{"x"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "exactly one logical") {
		t.Fatalf("非 [\"*\"] 应报错: %v", err)
	}
	cfg.ModelNames = []string{INTELLI_ROUTER_UNIFIED_MODEL}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("[\"*\"] 应通过: %v", err)
	}
	// ResolvedModelNames 恒 ["*"]（pool.py:395-398）
	if got := cfg.ResolvedModelNames(); len(got) != 1 || got[0] != "*" {
		t.Fatalf("ResolvedModelNames: %v", got)
	}
}
