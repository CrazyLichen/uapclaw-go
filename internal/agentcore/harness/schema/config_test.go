package schema

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	sap_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewDeepAgentConfig 测试 NewDeepAgentConfig 默认值
func TestNewDeepAgentConfig(t *testing.T) {
	cfg := NewDeepAgentConfig()
	if !cfg.AutoCreateWorkspace {
		t.Error("AutoCreateWorkspace 应为 true")
	}
	if !cfg.EnableReadImageMultimodal {
		t.Error("EnableReadImageMultimodal 应为 true")
	}
	if cfg.Model != nil {
		t.Error("Model 应为 nil")
	}
	if cfg.Card != nil {
		t.Error("Card 应为 nil")
	}
	if cfg.SystemPrompt != "" {
		t.Error("SystemPrompt 应为空")
	}
	if cfg.ContextEngineConfig != nil {
		t.Error("ContextEngineConfig 应为 nil")
	}
	if cfg.EnableTaskLoop {
		t.Error("EnableTaskLoop 应为 false")
	}
	if cfg.EnableAsyncSubagent {
		t.Error("EnableAsyncSubagent 应为 false")
	}
	if cfg.AddGeneralPurposeAgent {
		t.Error("AddGeneralPurposeAgent 应为 false")
	}
	if cfg.MaxIterations != 0 {
		t.Errorf("MaxIterations 应为 0（未配置=不限），实际为 %d", cfg.MaxIterations)
	}
	if cfg.CompletionTimeout != DefaultCompletionTimeout {
		t.Errorf("CompletionTimeout 应为 %v（默认值），实际为 %v", DefaultCompletionTimeout, cfg.CompletionTimeout)
	}
	if cfg.Language != DefaultLanguage {
		t.Errorf("Language 应为 %q（默认值），实际为 %q", DefaultLanguage, cfg.Language)
	}
	if cfg.PromptMode != PromptModeFull {
		t.Errorf("PromptMode 应为 PromptModeFull，实际为 %d", cfg.PromptMode)
	}
	if cfg.VisionModelConfig != nil {
		t.Error("VisionModelConfig 应为 nil")
	}
	if cfg.AudioModelConfig != nil {
		t.Error("AudioModelConfig 应为 nil")
	}
	if cfg.EnablePlanMode {
		t.Error("EnablePlanMode 应为 false")
	}
	if cfg.ProgressiveToolEnabled {
		t.Error("ProgressiveToolEnabled 应为 false")
	}
	if cfg.ToolSearchLimit != DefaultToolSearchLimit {
		t.Errorf("ToolSearchLimit 应为 %d（默认值），实际为 %d", DefaultToolSearchLimit, cfg.ToolSearchLimit)
	}
	if cfg.DefaultMode != AgentModeNormal {
		t.Errorf("DefaultMode 应为 AgentModeNormal，实际为 %d", cfg.DefaultMode)
	}
	if cfg.Permissions != nil {
		t.Error("Permissions 应为 nil")
	}
	if cfg.PermissionHost != nil {
		t.Error("PermissionHost 应为 nil")
	}
}

// TestResolveInnerReactMaxIterations 测试内层 ReAct 迭代上限解析
func TestResolveInnerReactMaxIterations(t *testing.T) {
	cfg := NewDeepAgentConfig()
	if got := cfg.ResolveInnerReactMaxIterations(); got != math.MaxInt {
		t.Errorf("ResolveInnerReactMaxIterations() = %d，期望 math.MaxInt", got)
	}

	cfg.MaxIterations = 20
	if got := cfg.ResolveInnerReactMaxIterations(); got != 20 {
		t.Errorf("ResolveInnerReactMaxIterations() = %d，期望 20", got)
	}

	cfg.MaxIterations = 1
	if got := cfg.ResolveInnerReactMaxIterations(); got != 1 {
		t.Errorf("ResolveInnerReactMaxIterations() = %d，期望 1", got)
	}
}

// TestEffectiveCompletionTimeout 测试 EffectiveCompletionTimeout
func TestEffectiveCompletionTimeout(t *testing.T) {
	cfg := NewDeepAgentConfig()
	if got := cfg.EffectiveCompletionTimeout(); got != DefaultCompletionTimeout {
		t.Errorf("EffectiveCompletionTimeout() = %f，期望 %f", got, DefaultCompletionTimeout)
	}

	cfg.CompletionTimeout = 300.0
	if got := cfg.EffectiveCompletionTimeout(); got != 300.0 {
		t.Errorf("EffectiveCompletionTimeout() = %f，期望 300.0", got)
	}
}

// TestEffectiveLanguage 测试 EffectiveLanguage
func TestEffectiveLanguage(t *testing.T) {
	cfg := NewDeepAgentConfig()
	if got := cfg.EffectiveLanguage(); got != DefaultLanguage {
		t.Errorf("EffectiveLanguage() = %q，期望 %q", got, DefaultLanguage)
	}

	cfg.Language = "en"
	if got := cfg.EffectiveLanguage(); got != "en" {
		t.Errorf("EffectiveLanguage() = %q，期望 %q", got, "en")
	}
}

// TestToolSearchLimit 测试 tool_search 默认条数与旧配置键忽略。
func TestToolSearchLimit(t *testing.T) {
	cfg := NewDeepAgentConfig()
	if cfg.ToolSearchLimit != DefaultToolSearchLimit {
		t.Errorf("ToolSearchLimit = %d，期望 %d", cfg.ToolSearchLimit, DefaultToolSearchLimit)
	}

	payload := []byte(`{"progressive_tool_enabled":true,"progressive_tool_max_loaded_tools":99}`)
	var decoded DeepAgentConfig
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("旧配置键应被静默忽略: %v", err)
	}
	if !decoded.ProgressiveToolEnabled {
		t.Error("progressive_tool_enabled 应生效")
	}
	if decoded.ToolSearchLimit != 0 {
		t.Errorf("未提供 tool_search_limit 时应保持零值，got %d", decoded.ToolSearchLimit)
	}
}

// TestNewVisionModelConfig 测试 NewVisionModelConfig 默认值
func TestNewVisionModelConfig(t *testing.T) {
	cfg := NewVisionModelConfig()
	if cfg.APIKey != "" {
		t.Error("APIKey 应为空")
	}
	if cfg.BaseURL != DefaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q，期望 %q", cfg.BaseURL, DefaultOpenAIBaseURL)
	}
	if cfg.Model != DefaultOpenAIVisionModel {
		t.Errorf("Model = %q，期望 %q", cfg.Model, DefaultOpenAIVisionModel)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d，期望 3", cfg.MaxRetries)
	}
}

// TestVisionModelConfigFromEnv 测试 VisionModelConfig.FromEnv
func TestVisionModelConfigFromEnv(t *testing.T) {
	// 无环境变量时应返回默认值
	_ = os.Unsetenv("VISION_API_KEY")
	_ = os.Unsetenv("OPENROUTER_API_KEY")
	_ = os.Unsetenv("OPENAI_API_KEY")
	_ = os.Unsetenv("VISION_BASE_URL")
	_ = os.Unsetenv("VISION_API_BASE")
	_ = os.Unsetenv("OPENROUTER_BASE_URL")
	_ = os.Unsetenv("OPENAI_BASE_URL")
	_ = os.Unsetenv("VISION_MODEL")
	_ = os.Unsetenv("VISION_MODEL_NAME")
	_ = os.Unsetenv("VISION_MAX_RETRIES")

	cfg := VisionModelConfig{}.FromEnv()
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q，期望空", cfg.APIKey)
	}
	if cfg.BaseURL != DefaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q，期望 %q", cfg.BaseURL, DefaultOpenAIBaseURL)
	}
	if cfg.Model != DefaultOpenAIVisionModel {
		t.Errorf("Model = %q，期望 %q", cfg.Model, DefaultOpenAIVisionModel)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d，期望 3", cfg.MaxRetries)
	}

	// 设置环境变量
	_ = os.Setenv("VISION_API_KEY", "test-key")
	defer func() { _ = os.Unsetenv("VISION_API_KEY") }()
	_ = os.Setenv("VISION_MODEL", "custom-model")
	defer func() { _ = os.Unsetenv("VISION_MODEL") }()

	cfg = VisionModelConfig{}.FromEnv()
	if cfg.APIKey != "test-key" {
		t.Errorf("APIKey = %q，期望 %q", cfg.APIKey, "test-key")
	}
	if cfg.Model != "custom-model" {
		t.Errorf("Model = %q，期望 %q", cfg.Model, "custom-model")
	}

	// 测试 OpenRouter URL 自动选择模型
	_ = os.Unsetenv("VISION_MODEL")
	_ = os.Setenv("VISION_BASE_URL", "https://openrouter.ai/api/v1")
	defer func() { _ = os.Unsetenv("VISION_BASE_URL") }()

	cfg = VisionModelConfig{}.FromEnv()
	if cfg.Model != DefaultOpenRouterVisionModel {
		t.Errorf("Model = %q，期望 %q", cfg.Model, DefaultOpenRouterVisionModel)
	}

	// 测试 MaxRetries 环境变量
	_ = os.Setenv("VISION_MAX_RETRIES", "5")
	defer func() { _ = os.Unsetenv("VISION_MAX_RETRIES") }()
	cfg = VisionModelConfig{}.FromEnv()
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d，期望 5", cfg.MaxRetries)
	}

	// 无效 MaxRetries 应回退默认值
	_ = os.Setenv("VISION_MAX_RETRIES", "invalid")
	cfg = VisionModelConfig{}.FromEnv()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d，期望 3（无效值回退）", cfg.MaxRetries)
	}
}

// TestNewAudioModelConfig 测试 NewAudioModelConfig 默认值
func TestNewAudioModelConfig(t *testing.T) {
	cfg := NewAudioModelConfig()
	if cfg.APIKey != "" {
		t.Error("APIKey 应为空")
	}
	if cfg.BaseURL != DefaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q，期望 %q", cfg.BaseURL, DefaultOpenAIBaseURL)
	}
	if cfg.TranscriptionModel != DefaultOpenAIAudioTranscriptionModel {
		t.Errorf("TranscriptionModel = %q，期望 %q", cfg.TranscriptionModel, DefaultOpenAIAudioTranscriptionModel)
	}
	if cfg.QAModel != DefaultOpenAIAudioQAModel {
		t.Errorf("QAModel = %q，期望 %q", cfg.QAModel, DefaultOpenAIAudioQAModel)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d，期望 3", cfg.MaxRetries)
	}
	if cfg.HTTPTimeout != DefaultAudioHTTPTimeout {
		t.Errorf("HTTPTimeout = %d，期望 %d", cfg.HTTPTimeout, DefaultAudioHTTPTimeout)
	}
	if cfg.MaxAudioBytes != DefaultMaxAudioBytes {
		t.Errorf("MaxAudioBytes = %d，期望 %d", cfg.MaxAudioBytes, DefaultMaxAudioBytes)
	}
	if cfg.ACRBaseURL != DefaultACRBaseURL {
		t.Errorf("ACRBaseURL = %q，期望 %q", cfg.ACRBaseURL, DefaultACRBaseURL)
	}
}

// TestAudioModelConfigFromEnv 测试 AudioModelConfig.FromEnv
func TestAudioModelConfigFromEnv(t *testing.T) {
	// 清理环境变量
	audioEnvVars := []string{
		"AUDIO_API_KEY", "OPENAI_API_KEY",
		"AUDIO_BASE_URL", "AUDIO_API_BASE", "OPENAI_BASE_URL",
		"AUDIO_TRANSCRIPTION_MODEL", "AUDIO_MODEL_NAME",
		"AUDIO_QUESTION_ANSWERING_MODEL",
		"AUDIO_MAX_RETRIES", "AUDIO_HTTP_TIMEOUT", "AUDIO_MAX_AUDIO_BYTES",
		"ACR_ACCESS_KEY", "ACR_ACCESS_SECRET", "ACR_BASE_URL",
	}
	for _, v := range audioEnvVars {
		_ = os.Unsetenv(v)
	}

	cfg := AudioModelConfig{}.FromEnv()
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q，期望空", cfg.APIKey)
	}
	if cfg.BaseURL != DefaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q，期望 %q", cfg.BaseURL, DefaultOpenAIBaseURL)
	}
	if cfg.TranscriptionModel != DefaultOpenAIAudioTranscriptionModel {
		t.Errorf("TranscriptionModel = %q，期望 %q", cfg.TranscriptionModel, DefaultOpenAIAudioTranscriptionModel)
	}
	if cfg.QAModel != DefaultOpenAIAudioQAModel {
		t.Errorf("QAModel = %q，期望 %q", cfg.QAModel, DefaultOpenAIAudioQAModel)
	}
	if cfg.ACRAccessKey != "" {
		t.Errorf("ACRAccessKey = %q，期望空", cfg.ACRAccessKey)
	}
	if cfg.ACRBaseURL != DefaultACRBaseURL {
		t.Errorf("ACRBaseURL = %q，期望 %q", cfg.ACRBaseURL, DefaultACRBaseURL)
	}

	// 设置环境变量
	_ = os.Setenv("AUDIO_API_KEY", "audio-test-key")
	defer func() { _ = os.Unsetenv("AUDIO_API_KEY") }()
	_ = os.Setenv("ACR_ACCESS_KEY", "acr-key")
	defer func() { _ = os.Unsetenv("ACR_ACCESS_KEY") }()
	_ = os.Setenv("ACR_ACCESS_SECRET", "acr-secret")
	defer func() { _ = os.Unsetenv("ACR_ACCESS_SECRET") }()
	_ = os.Setenv("ACR_BASE_URL", "https://custom-acr.example.com")
	defer func() { _ = os.Unsetenv("ACR_BASE_URL") }()

	cfg = AudioModelConfig{}.FromEnv()
	if cfg.APIKey != "audio-test-key" {
		t.Errorf("APIKey = %q，期望 %q", cfg.APIKey, "audio-test-key")
	}
	if cfg.ACRAccessKey != "acr-key" {
		t.Errorf("ACRAccessKey = %q，期望 %q", cfg.ACRAccessKey, "acr-key")
	}
	if cfg.ACRAccessSecret != "acr-secret" {
		t.Errorf("ACRAccessSecret = %q，期望 %q", cfg.ACRAccessSecret, "acr-secret")
	}
	if cfg.ACRBaseURL != "https://custom-acr.example.com" {
		t.Errorf("ACRBaseURL = %q，期望自定义 URL", cfg.ACRBaseURL)
	}
}

// TestSubAgentConfig 测试 SubAgentConfig 字段
func TestSubAgentConfig(t *testing.T) {
	scfg := SubAgentConfig{
		SystemPrompt:      "测试提示词",
		Language:          "en",
		PromptMode:        PromptModeMinimal,
		EnableTaskLoop:    true,
		MaxIterations:     10,
		FactoryName:       "test_factory",
		EnablePlanMode:    true,
		RestrictToWorkDir: true,
	}

	if scfg.SystemPrompt != "测试提示词" {
		t.Errorf("SystemPrompt = %q，期望 %q", scfg.SystemPrompt, "测试提示词")
	}
	if scfg.Language != "en" {
		t.Errorf("Language = %q，期望 %q", scfg.Language, "en")
	}
	if scfg.PromptMode != PromptModeMinimal {
		t.Errorf("PromptMode = %d，期望 PromptModeMinimal", scfg.PromptMode)
	}
	if !scfg.EnableTaskLoop {
		t.Error("EnableTaskLoop 应为 true")
	}
	if scfg.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d，期望 10", scfg.MaxIterations)
	}
	if scfg.FactoryName != "test_factory" {
		t.Errorf("FactoryName = %q，期望 %q", scfg.FactoryName, "test_factory")
	}
	if !scfg.EnablePlanMode {
		t.Error("EnablePlanMode 应为 true")
	}
	if !scfg.RestrictToWorkDir {
		t.Error("RestrictToWorkDir 应为 true")
	}
}

// TestDeepAgentConfigJSON 测试 DeepAgentConfig JSON 序列化
func TestDeepAgentConfigJSON(t *testing.T) {
	cfg := NewDeepAgentConfig()
	cfg.SystemPrompt = "测试提示词"
	cfg.Language = "cn"
	cfg.MaxIterations = 10
	cfg.CompletionTimeout = 300.0
	cfg.PromptMode = PromptModeMinimal
	cfg.DefaultMode = AgentModePlan
	cfg.Skills = []string{"skill1", "skill2"}
	cfg.ProgressiveToolEnabled = true
	cfg.ToolSearchLimit = 8

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var decoded DeepAgentConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	if decoded.SystemPrompt != cfg.SystemPrompt {
		t.Errorf("SystemPrompt = %q，期望 %q", decoded.SystemPrompt, cfg.SystemPrompt)
	}
	if decoded.Language != cfg.Language {
		t.Errorf("Language = %q，期望 %q", decoded.Language, cfg.Language)
	}
	if decoded.MaxIterations != cfg.MaxIterations {
		t.Errorf("MaxIterations = %d，期望 %d", decoded.MaxIterations, cfg.MaxIterations)
	}
	if decoded.CompletionTimeout != cfg.CompletionTimeout {
		t.Errorf("CompletionTimeout = %f，期望 %f", decoded.CompletionTimeout, cfg.CompletionTimeout)
	}
	if decoded.PromptMode != cfg.PromptMode {
		t.Errorf("PromptMode = %d，期望 %d", decoded.PromptMode, cfg.PromptMode)
	}
	if decoded.DefaultMode != cfg.DefaultMode {
		t.Errorf("DefaultMode = %d，期望 %d", decoded.DefaultMode, cfg.DefaultMode)
	}
	if !decoded.AutoCreateWorkspace {
		t.Error("AutoCreateWorkspace 应为 true")
	}
	if !decoded.EnableReadImageMultimodal {
		t.Error("EnableReadImageMultimodal 应为 true")
	}
	if decoded.ProgressiveToolEnabled != cfg.ProgressiveToolEnabled {
		t.Errorf("ProgressiveToolEnabled = %v，期望 %v", decoded.ProgressiveToolEnabled, cfg.ProgressiveToolEnabled)
	}
	if decoded.ToolSearchLimit != cfg.ToolSearchLimit {
		t.Errorf("ToolSearchLimit = %d，期望 %d", decoded.ToolSearchLimit, cfg.ToolSearchLimit)
	}
	if len(decoded.Skills) != 2 {
		t.Errorf("Skills 长度 = %d，期望 2", len(decoded.Skills))
	}
}

// TestVisionModelConfigJSON 测试 VisionModelConfig JSON 序列化
func TestVisionModelConfigJSON(t *testing.T) {
	cfg := NewVisionModelConfig()
	cfg.APIKey = "test-key"

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var decoded VisionModelConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	if decoded.APIKey != "test-key" {
		t.Errorf("APIKey = %q，期望 %q", decoded.APIKey, "test-key")
	}
	if decoded.BaseURL != DefaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q，期望 %q", decoded.BaseURL, DefaultOpenAIBaseURL)
	}
	if decoded.Model != DefaultOpenAIVisionModel {
		t.Errorf("Model = %q，期望 %q", decoded.Model, DefaultOpenAIVisionModel)
	}
}

// TestAudioModelConfigJSON 测试 AudioModelConfig JSON 序列化
func TestAudioModelConfigJSON(t *testing.T) {
	cfg := NewAudioModelConfig()

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var decoded AudioModelConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	if decoded.BaseURL != DefaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q，期望 %q", decoded.BaseURL, DefaultOpenAIBaseURL)
	}
	if decoded.TranscriptionModel != DefaultOpenAIAudioTranscriptionModel {
		t.Errorf("TranscriptionModel = %q，期望 %q", decoded.TranscriptionModel, DefaultOpenAIAudioTranscriptionModel)
	}
	if decoded.QAModel != DefaultOpenAIAudioQAModel {
		t.Errorf("QAModel = %q，期望 %q", decoded.QAModel, DefaultOpenAIAudioQAModel)
	}
	if decoded.HTTPTimeout != DefaultAudioHTTPTimeout {
		t.Errorf("HTTPTimeout = %d，期望 %d", decoded.HTTPTimeout, DefaultAudioHTTPTimeout)
	}
	if decoded.MaxAudioBytes != DefaultMaxAudioBytes {
		t.Errorf("MaxAudioBytes = %d，期望 %d", decoded.MaxAudioBytes, DefaultMaxAudioBytes)
	}
}

// TestConstants 测试常量值
func TestConstants(t *testing.T) {
	if DefaultCompletionTimeout != 600.0 {
		t.Errorf("DefaultCompletionTimeout = %f，期望 600.0", DefaultCompletionTimeout)
	}
	if DefaultToolSearchLimit != 5 {
		t.Errorf("DefaultToolSearchLimit = %d，期望 5", DefaultToolSearchLimit)
	}
	if DefaultLanguage != "cn" {
		t.Errorf("DefaultLanguage = %q，期望 %q", DefaultLanguage, "cn")
	}
	if DefaultMaxAudioBytes != 25*1024*1024 {
		t.Errorf("DefaultMaxAudioBytes = %d，期望 %d", DefaultMaxAudioBytes, 25*1024*1024)
	}
}

// TestEffectiveRestrictToWorkDir 测试 EffectiveRestrictToWorkDir
func TestEffectiveRestrictToWorkDir(t *testing.T) {
	// NewSubAgentConfig 默认值为 true（对齐 Python 默认值）
	scfg := NewSubAgentConfig()
	if !scfg.EffectiveRestrictToWorkDir() {
		t.Error("EffectiveRestrictToWorkDir() 应为 true（对齐 Python 默认值）")
	}

	// 显式设置 true
	scfg.RestrictToWorkDir = true
	if !scfg.EffectiveRestrictToWorkDir() {
		t.Error("EffectiveRestrictToWorkDir() 显式 true 时应为 true")
	}

	// 显式设置 false
	scfg.RestrictToWorkDir = false
	if scfg.EffectiveRestrictToWorkDir() {
		t.Error("EffectiveRestrictToWorkDir() 显式 false 时应为 false")
	}
}

// TestNewSubAgentConfig 测试 NewSubAgentConfig 默认值
func TestNewSubAgentConfig(t *testing.T) {
	scfg := NewSubAgentConfig()
	if !scfg.RestrictToWorkDir {
		t.Error("NewSubAgentConfig() 的 RestrictToWorkDir 应为 true")
	}
}

// TestSubAgentConfig_SpecName_NilCard 验证 AgentCard 为 nil 时 SpecName 返回空串
func TestSubAgentConfig_SpecName_NilCard(t *testing.T) {
	scfg := NewSubAgentConfig()
	if scfg.SpecName() != "" {
		t.Errorf("AgentCard 为 nil 时 SpecName 应为空串，实际 %q", scfg.SpecName())
	}
}

// TestSubAgentConfig_SpecName_有Name 验证 AgentCard 有名称时 SpecName 返回名称
func TestSubAgentConfig_SpecName_有Name(t *testing.T) {
	scfg := NewSubAgentConfig()
	scfg.AgentCard = sap_schema.NewAgentCard(sap_schema.WithAgentName("test-agent"))
	if scfg.SpecName() != "test-agent" {
		t.Errorf("SpecName 期望 %q，实际 %q", "test-agent", scfg.SpecName())
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewVideoModelConfig 测试 NewVideoModelConfig 默认值
func TestNewVideoModelConfig(t *testing.T) {
	cfg := NewVideoModelConfig()
	if cfg.APIKey != "" {
		t.Error("APIKey 应为空")
	}
	if cfg.BaseURL != "" {
		t.Error("BaseURL 应为空（不像 VisionModelConfig 默认填充）")
	}
	if cfg.Model != DefaultVideoModel {
		t.Errorf("Model = %q，期望 %q", cfg.Model, DefaultVideoModel)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d，期望 3", cfg.MaxRetries)
	}
	if cfg.ThinkingEnabled {
		t.Error("ThinkingEnabled 默认应为 false")
	}
}

// TestVideoModelConfig_FromEnv 测试 VideoModelConfig.FromEnv
func TestVideoModelConfig_FromEnv(t *testing.T) {
	// 清理视频相关环境变量
	videoEnvVars := []string{
		"VIDEO_API_KEY", "VIDEO_API_BASE", "ZHIPU_API_URL", "API_BASE",
		"VIDEO_MODEL_NAME", "MODEL_NAME", "VIDEO_THINKING_ENABLED", "VIDEO_MAX_RETRIES",
	}
	for _, v := range videoEnvVars {
		_ = os.Unsetenv(v)
	}

	// 无环境变量时应返回默认值
	cfg := VideoModelConfig{}.FromEnv()
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q，期望空", cfg.APIKey)
	}
	if cfg.BaseURL != DefaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q，期望 %q", cfg.BaseURL, DefaultOpenAIBaseURL)
	}
	if cfg.Model != DefaultVideoModel {
		t.Errorf("Model = %q，期望 %q", cfg.Model, DefaultVideoModel)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d，期望 3", cfg.MaxRetries)
	}
	if cfg.ThinkingEnabled {
		t.Error("ThinkingEnabled 应为 false")
	}

	// 设置环境变量
	_ = os.Setenv("VIDEO_API_KEY", "test-video-key")
	defer func() { _ = os.Unsetenv("VIDEO_API_KEY") }()
	_ = os.Setenv("VIDEO_API_BASE", "https://video-api.example.com/v1")
	defer func() { _ = os.Unsetenv("VIDEO_API_BASE") }()
	_ = os.Setenv("VIDEO_MODEL_NAME", "custom-video-model")
	defer func() { _ = os.Unsetenv("VIDEO_MODEL_NAME") }()
	_ = os.Setenv("VIDEO_THINKING_ENABLED", "true")
	defer func() { _ = os.Unsetenv("VIDEO_THINKING_ENABLED") }()
	_ = os.Setenv("VIDEO_MAX_RETRIES", "5")
	defer func() { _ = os.Unsetenv("VIDEO_MAX_RETRIES") }()

	cfg = VideoModelConfig{}.FromEnv()
	if cfg.APIKey != "test-video-key" {
		t.Errorf("APIKey = %q，期望 %q", cfg.APIKey, "test-video-key")
	}
	if cfg.BaseURL != "https://video-api.example.com/v1" {
		t.Errorf("BaseURL = %q，期望 %q", cfg.BaseURL, "https://video-api.example.com/v1")
	}
	if cfg.Model != "custom-video-model" {
		t.Errorf("Model = %q，期望 %q", cfg.Model, "custom-video-model")
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d，期望 5", cfg.MaxRetries)
	}
	if !cfg.ThinkingEnabled {
		t.Error("ThinkingEnabled 应为 true")
	}

	// 测试 ZHIPU_API_URL 回退
	_ = os.Unsetenv("VIDEO_API_BASE")
	_ = os.Setenv("ZHIPU_API_URL", "https://zhipu.example.com/v1")
	defer func() { _ = os.Unsetenv("ZHIPU_API_URL") }()
	cfg = VideoModelConfig{}.FromEnv()
	if cfg.BaseURL != "https://zhipu.example.com/v1" {
		t.Errorf("BaseURL = %q，期望 ZHIPU_API_URL 回退", cfg.BaseURL)
	}

	// 测试 MODEL_NAME 回退
	_ = os.Unsetenv("VIDEO_MODEL_NAME")
	_ = os.Setenv("MODEL_NAME", "fallback-model")
	defer func() { _ = os.Unsetenv("MODEL_NAME") }()
	cfg = VideoModelConfig{}.FromEnv()
	if cfg.Model != "fallback-model" {
		t.Errorf("Model = %q，期望 MODEL_NAME 回退", cfg.Model)
	}

	// 无效 MaxRetries 应回退默认值
	_ = os.Setenv("VIDEO_MAX_RETRIES", "invalid")
	cfg = VideoModelConfig{}.FromEnv()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d，期望 3（无效值回退）", cfg.MaxRetries)
	}
}

// TestVideoModelConfig_DefaultConstants 测试 VideoModelConfig 相关常量
func TestVideoModelConfig_DefaultConstants(t *testing.T) {
	if DefaultVideoModel != "glm-4.6v" {
		t.Errorf("DefaultVideoModel = %q，期望 glm-4.6v", DefaultVideoModel)
	}
}

// TestVideoModelConfigJSON 测试 VideoModelConfig JSON 序列化
func TestVideoModelConfigJSON(t *testing.T) {
	cfg := NewVideoModelConfig()
	cfg.APIKey = "test-video-key"
	cfg.ThinkingEnabled = true

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var decoded VideoModelConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	if decoded.APIKey != "test-video-key" {
		t.Errorf("APIKey = %q，期望 %q", decoded.APIKey, "test-video-key")
	}
	if decoded.Model != DefaultVideoModel {
		t.Errorf("Model = %q，期望 %q", decoded.Model, DefaultVideoModel)
	}
	if decoded.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d，期望 3", decoded.MaxRetries)
	}
	if !decoded.ThinkingEnabled {
		t.Error("ThinkingEnabled 应为 true")
	}
}

// ──────────────────────────── A-05: 内层 ReAct 迭代上限语义 ────────────────────────────

// TestDeepAgentConfig_ResolveInnerReactMaxIterations 验证 0 → 不限、正数原样、负数按不限
func TestDeepAgentConfig_ResolveInnerReactMaxIterations(t *testing.T) {
	// 0 = 未配置 = 不限（对应上游 None → sys.maxsize）
	assert.Equal(t, math.MaxInt, (&DeepAgentConfig{}).ResolveInnerReactMaxIterations())
	// 显式值原样透传，与 enable_task_loop 无关
	assert.Equal(t, 7, (&DeepAgentConfig{MaxIterations: 7}).ResolveInnerReactMaxIterations())
	// 负数同样透传：上游只判 None，不做降级（不去猜"配置笔误"）
	assert.Equal(t, -3, (&DeepAgentConfig{MaxIterations: -3}).ResolveInnerReactMaxIterations())
}

// TestNewDeepAgentConfig_MaxIterations未配置 验证默认不再预置 15
func TestNewDeepAgentConfig_MaxIterations未配置(t *testing.T) {
	cfg := NewDeepAgentConfig()
	assert.Equal(t, 0, cfg.MaxIterations) // 0 = 未配置 = 不限
}
