package iface

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── ProcessorConfig 测试 ────────────────────────────

// TestProcessorConfig fakeConfig 实现 ProcessorConfig 接口
func TestProcessorConfig(t *testing.T) {
	cfg := &fakeProcessorConfig{valid: true}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() 返回错误: %v, 期望 nil", err)
	}

	cfg2 := &fakeProcessorConfig{valid: false}
	if err := cfg2.Validate(); err == nil {
		t.Error("Validate() 应返回错误，但返回 nil")
	}
}

// ──────────────────────────── ProcessorFactory 测试 ────────────────────────────

// TestProcessorFactory 工厂函数类型
func TestProcessorFactory(t *testing.T) {
	factory := ProcessorFactory(func(config ProcessorConfig) (ContextProcessor, error) {
		return nil, nil
	})
	if factory == nil {
		t.Fatal("ProcessorFactory 应为非 nil")
	}
}

// ──────────────────────────── fakeProcessorConfig 测试辅助 ────────────────────────────

// fakeProcessorConfig 用于测试的模拟处理器配置
type fakeProcessorConfig struct {
	valid bool
}

// errFakeValidate 模拟校验错误
var errFakeValidate = errors.New("配置校验失败")

func (f *fakeProcessorConfig) Validate() error {
	if !f.valid {
		return errFakeValidate
	}
	return nil
}

func (f *fakeProcessorConfig) SetModelDefaults(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) {
}

func (f *fakeProcessorConfig) GetModel() *llmschema.ModelRequestConfig {
	return nil
}

// TestWithSysOperation 验证 WithSysOperation 选项函数
func TestWithSysOperation(t *testing.T) {
	var op sys_operation.SysOperation = nil
	opt := WithSysOperation(op)
	o := &ProcessorOption{}
	opt(o)
	assert.Nil(t, o.SysOperation)
}

// TestWithModel 验证 WithModel 选项函数
func TestWithModel(t *testing.T) {
	// 注册 mock provider，使 NewModel 能成功创建
	registry := model_clients.GetClientRegistry()
	registry.Register("llm_OpenAI", "llm", func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return &mockBaseModelClient{}, nil
	})
	defer func() { _ = registry.Unregister("llm_OpenAI", "llm") }()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: "OpenAI", ClientID: "llm_OpenAI", APIKey: "test-key", APIBase: "http://localhost", VerifySSL: false}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "gpt-4"}
	m, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)

	opt := WithModel(m)
	o := &ProcessorOption{}
	opt(o)
	assert.Equal(t, m, o.Model)
}

// ──────────────────────────── mockBaseModelClient 测试辅助 ────────────────────────────

// mockBaseModelClient 用于测试的模拟模型客户端
type mockBaseModelClient struct{}

func (m *mockBaseModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return nil, nil
}
func (m *mockBaseModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}
func (m *mockBaseModelClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}
func (m *mockBaseModelClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}
func (m *mockBaseModelClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}
func (m *mockBaseModelClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}
func (m *mockBaseModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *mockBaseModelClient) SupportsKVCacheRelease() bool {
	return false
}
