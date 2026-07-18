package context_engineer

import (
	"reflect"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// reflectPtrElem 获取指针指向的结构体 reflect.Value
func reflectPtrElem(ptr any) reflect.Value {
	v := reflect.ValueOf(ptr)
	if v.Kind() == reflect.Pointer {
		return v.Elem()
	}
	return v
}

// testConfig 测试用 ProcessorConfig 实现
type testConfig struct {
	TokensThreshold int                           `json:"tokens_threshold"`
	MessagesToKeep  int                           `json:"messages_to_keep"`
	KeepLastRound   bool                          `json:"keep_last_round"`
	Model           *llmschema.ModelRequestConfig `json:"model"`
	ModelClient     *llmschema.ModelClientConfig  `json:"model_client"`
}

func (c *testConfig) Validate() error { return nil }

func (c *testConfig) SetModelDefaults(model *llmschema.ModelRequestConfig, modelClient *llmschema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}

func (c *testConfig) GetModel() *llmschema.ModelRequestConfig {
	return c.Model
}

// ──────────────────────────── snakeToPascal 测试 ────────────────────────────

func TestSnakeToPascal(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"tokens_threshold", "TokensThreshold"},
		{"messages_to_keep", "MessagesToKeep"},
		{"keep_last_round", "KeepLastRound"},
		{"model", "Model"},
		{"model_client", "ModelClient"},
		{"compression_target_tokens", "CompressionTargetTokens"},
		{"large_message_threshold", "LargeMessageThreshold"},
		{"offload_message_type", "OffloadMessageType"},
		{"protected_tool_names", "ProtectedToolNames"},
		{"", ""},
	}

	for _, tt := range tests {
		result := snakeToPascal(tt.input)
		if result != tt.expected {
			t.Errorf("snakeToPascal(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// ──────────────────────────── MergeConfigWithOverrides 测试 ────────────────────────────

func TestMergeConfigWithOverrides_空覆盖(t *testing.T) {
	base := &testConfig{
		TokensThreshold: 100000,
		MessagesToKeep:  10,
		KeepLastRound:   false,
	}

	result := MergeConfigWithOverrides(base, nil)
	if result != base {
		t.Error("空覆盖应返回原始 base")
	}

	result = MergeConfigWithOverrides(base, map[string]any{})
	if result != base {
		t.Error("空 map 覆盖应返回原始 base")
	}
}

func TestMergeConfigWithOverrides_部分字段覆盖(t *testing.T) {
	base := &testConfig{
		TokensThreshold: 100000,
		MessagesToKeep:  10,
		KeepLastRound:   false,
	}

	overrides := map[string]any{
		"tokens_threshold": 50000,
		"keep_last_round":  true,
	}

	result := MergeConfigWithOverrides(base, overrides)
	resultCfg, ok := result.(*testConfig)
	if !ok {
		t.Fatal("结果类型断言失败")
	}

	// 覆盖的字段
	if resultCfg.TokensThreshold != 50000 {
		t.Errorf("TokensThreshold = %d, want 50000", resultCfg.TokensThreshold)
	}
	if resultCfg.KeepLastRound != true {
		t.Error("KeepLastRound = false, want true")
	}

	// 未覆盖的字段保持原值
	if resultCfg.MessagesToKeep != 10 {
		t.Errorf("MessagesToKeep = %d, want 10", resultCfg.MessagesToKeep)
	}

	// base 不应被修改
	if base.TokensThreshold != 100000 {
		t.Error("原始 base 被修改了")
	}
}

func TestMergeConfigWithOverrides_不存在字段(t *testing.T) {
	base := &testConfig{
		TokensThreshold: 100000,
	}

	overrides := map[string]any{
		"nonexistent_field": 42,
	}

	// 不存在的字段应跳过，不报错
	result := MergeConfigWithOverrides(base, overrides)
	resultCfg, ok := result.(*testConfig)
	if !ok {
		t.Fatal("结果类型断言失败")
	}
	if resultCfg.TokensThreshold != 100000 {
		t.Errorf("TokensThreshold = %d, want 100000", resultCfg.TokensThreshold)
	}
}

func TestMergeConfigWithOverrides_Model字段回填(t *testing.T) {
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}

	base := &testConfig{
		TokensThreshold: 100000,
		Model:           nil,
	}

	overrides := map[string]any{
		"model": modelCfg,
	}

	result := MergeConfigWithOverrides(base, overrides)
	resultCfg, ok := result.(*testConfig)
	if !ok {
		t.Fatal("结果类型断言失败")
	}
	if resultCfg.Model == nil || resultCfg.Model.ModelName != "test-model" {
		t.Error("Model 字段未正确设置")
	}
}

func TestMergeConfigWithOverrides_float64转int(t *testing.T) {
	base := &testConfig{
		TokensThreshold: 100000,
	}

	overrides := map[string]any{
		"tokens_threshold": float64(50000),
	}

	result := MergeConfigWithOverrides(base, overrides)
	resultCfg, ok := result.(*testConfig)
	if !ok {
		t.Fatal("结果类型断言失败")
	}
	if resultCfg.TokensThreshold != 50000 {
		t.Errorf("TokensThreshold = %d, want 50000", resultCfg.TokensThreshold)
	}
}

// ──────────────────────────── MergeProcessors 测试 ────────────────────────────

func TestMergeProcessors_基础合并(t *testing.T) {
	base := []iface.ProcessorSpec{
		{Type: "DialogueCompressor", Config: &testConfig{TokensThreshold: 100000}},
		{Type: "MessageSummaryOffloader", Config: &testConfig{TokensThreshold: 20000}},
	}

	overrides := []iface.ProcessorSpec{
		{
			Type:            "DialogueCompressor",
			ConfigOverrides: map[string]any{"tokens_threshold": 50000},
		},
	}

	result := MergeProcessors(base, overrides, nil, nil)
	if len(result) != 2 {
		t.Fatalf("结果长度 = %d, want 2", len(result))
	}

	// 第一个应被合并
	if result[0].Type != "DialogueCompressor" {
		t.Errorf("Type = %q, want DialogueCompressor", result[0].Type)
	}
	cfg, ok := result[0].Config.(*testConfig)
	if !ok {
		t.Fatal("Config 类型断言失败")
	}
	if cfg.TokensThreshold != 50000 {
		t.Errorf("TokensThreshold = %d, want 50000", cfg.TokensThreshold)
	}

	// 第二个应保留原值
	cfg2, ok := result[1].Config.(*testConfig)
	if !ok {
		t.Fatal("Config2 类型断言失败")
	}
	if cfg2.TokensThreshold != 20000 {
		t.Errorf("TokensThreshold = %d, want 20000", cfg2.TokensThreshold)
	}
}

func TestMergeProcessors_追加新processor(t *testing.T) {
	base := []iface.ProcessorSpec{
		{Type: "DialogueCompressor", Config: &testConfig{TokensThreshold: 100000}},
	}

	overrides := []iface.ProcessorSpec{
		{Type: "NewProcessor", Config: &testConfig{TokensThreshold: 5000}},
	}

	result := MergeProcessors(base, overrides, nil, nil)
	if len(result) != 2 {
		t.Fatalf("结果长度 = %d, want 2", len(result))
	}
	if result[1].Type != "NewProcessor" {
		t.Errorf("Type = %q, want NewProcessor", result[1].Type)
	}
}

func TestMergeProcessors_无base时dict覆盖应panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("无 base 时 dict 覆盖应 panic")
		}
	}()

	overrides := []iface.ProcessorSpec{
		{
			Type:            "NewProcessor",
			ConfigOverrides: map[string]any{"tokens_threshold": 5000},
		},
	}

	MergeProcessors(nil, overrides, nil, nil)
}

func TestMergeProcessors_完整配置替换(t *testing.T) {
	base := []iface.ProcessorSpec{
		{Type: "DialogueCompressor", Config: &testConfig{TokensThreshold: 100000, MessagesToKeep: 10}},
	}

	replacementCfg := &testConfig{TokensThreshold: 50000, MessagesToKeep: 5, KeepLastRound: true}
	overrides := []iface.ProcessorSpec{
		{Type: "DialogueCompressor", Config: replacementCfg},
	}

	result := MergeProcessors(base, overrides, nil, nil)
	cfg, ok := result[0].Config.(*testConfig)
	if !ok {
		t.Fatal("Config 类型断言失败")
	}
	if cfg.TokensThreshold != 50000 || cfg.MessagesToKeep != 5 || !cfg.KeepLastRound {
		t.Error("完整配置替换未生效")
	}
}

// ──────────────────────────── deepCopyConfig 测试 ────────────────────────────

func TestDeepCopyConfig_深拷贝不影响原始(t *testing.T) {
	original := &testConfig{
		TokensThreshold: 100000,
		MessagesToKeep:  10,
	}

	copied := deepCopyConfig(original)
	copiedCfg := copied.(*testConfig)
	copiedCfg.TokensThreshold = 50000

	if original.TokensThreshold != 100000 {
		t.Error("深拷贝后修改不应影响原始对象")
	}
}

func TestDeepCopyConfig_nil输入(t *testing.T) {
	result := deepCopyConfig(nil)
	if result != nil {
		t.Error("nil 输入应返回 nil")
	}
}

// ──────────────────────────── fillModelDefaults 测试 ────────────────────────────

func TestFillModelDefaults_回填Model字段(t *testing.T) {
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "qwen-max"}
	clientCfg := &llmschema.ModelClientConfig{APIKey: "test-key"}

	cfg := &testConfig{
		TokensThreshold: 100000,
		Model:           nil,
		ModelClient:     nil,
	}

	fillModelDefaults(cfg, modelCfg, clientCfg)

	if cfg.Model == nil || cfg.Model.ModelName != "qwen-max" {
		t.Error("Model 字段未回填")
	}
	if cfg.ModelClient == nil || cfg.ModelClient.APIKey != "test-key" {
		t.Error("ModelClient 字段未回填")
	}
}

func TestFillModelDefaults_已有Model不覆盖(t *testing.T) {
	existingModel := &llmschema.ModelRequestConfig{ModelName: "existing-model"}
	newModel := &llmschema.ModelRequestConfig{ModelName: "new-model"}

	cfg := &testConfig{
		Model: existingModel,
	}

	fillModelDefaults(cfg, newModel, nil)

	if cfg.Model.ModelName != "existing-model" {
		t.Error("已有的 Model 字段不应被覆盖")
	}
}

func TestFillModelDefaults_nilModelConfig不回填(t *testing.T) {
	cfg := &testConfig{
		Model: nil,
	}

	fillModelDefaults(cfg, nil, nil)

	if cfg.Model != nil {
		t.Error("nil modelConfig 不应回填 Model 字段")
	}
}

// ──────────────────────────── setFieldValue 测试 ────────────────────────────

func TestSetFieldValue_各种类型(t *testing.T) {
	cfg := &testConfig{}

	v := reflectPtrElem(cfg)

	// int 字段
	setFieldValue(v.FieldByName("TokensThreshold"), 50000)
	if cfg.TokensThreshold != 50000 {
		t.Errorf("int 字段设置失败: %d", cfg.TokensThreshold)
	}

	// bool 字段
	setFieldValue(v.FieldByName("KeepLastRound"), true)
	if !cfg.KeepLastRound {
		t.Error("bool 字段设置失败")
	}

	// 指针字段（传值）
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test"}
	setFieldValue(v.FieldByName("Model"), modelCfg)
	if cfg.Model == nil || cfg.Model.ModelName != "test" {
		t.Error("指针字段设置失败")
	}
}
