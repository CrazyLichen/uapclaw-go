package extractor

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// stubTripleClient 惰性 mock：所有方法零行为（captioner_test.go stubCaptionClient 先例）
type stubTripleClient struct{}

func (s *stubTripleClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubTripleClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubTripleClient) SupportsKVCacheRelease() bool { return false }

// newTripleTestModel 构造带 stub 客户端的 Model（WithClient 注入，走真实 Invoke 链）
func newTripleTestModel(t *testing.T) *llm.Model {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(&stubTripleClient{}))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// TestTripleExtractor_构造 测试初始化（对齐 test_init）
func TestTripleExtractor_构造(t *testing.T) {
	model := newTripleTestModel(t)
	ex := NewTripleExtractor(model, "test-model",
		WithTripleTemperature(0.0), WithTripleMaxConcurrent(10))
	if ex.llmClient != model {
		t.Error("llmClient 应为构造传入的 model")
	}
	if ex.modelName != "test-model" {
		t.Errorf("modelName 应为 test-model，实际 %s", ex.modelName)
	}
	if ex.temperature != 0.0 {
		t.Errorf("temperature 应为 0.0，实际 %v", ex.temperature)
	}
	if cap(ex.limiter) != 10 {
		t.Errorf("limiter 容量应为 10，实际 %d", cap(ex.limiter))
	}
}

// TestTripleExtractor_默认值构造 测试默认构造（对齐 test_init_with_defaults）
func TestTripleExtractor_默认值构造(t *testing.T) {
	ex := NewTripleExtractor(newTripleTestModel(t), "test-model")
	if ex.temperature != 0.0 {
		t.Errorf("默认 temperature 应为 0.0，实际 %v", ex.temperature)
	}
	if cap(ex.limiter) != 50 {
		t.Errorf("默认 limiter 容量应为 50，实际 %d", cap(ex.limiter))
	}
}
