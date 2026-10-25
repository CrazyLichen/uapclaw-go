package extractor

import (
	"context"
	"errors"
	"strings"
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

// ──────────────────────────── buildPrompt 测试 ────────────────────────────

// TestBuildPrompt_标题为空 测试空标题回落 Untitled（Go 增强 §7.2-4）
func TestBuildPrompt_标题为空(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("Alice knows Bob", "")
	if !strings.Contains(prompt, "Title:\nUntitled\n") {
		t.Error("空标题应渲染为 Untitled")
	}
	if !strings.Contains(prompt, "Passage:\nAlice knows Bob") {
		t.Error("passage 应原样渲染")
	}
}

// TestBuildPrompt_非空标题 测试非空标题渲染
func TestBuildPrompt_非空标题(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("body text", "My Title")
	if !strings.Contains(prompt, "Title:\nMy Title\n") {
		t.Error("非空标题应原样渲染")
	}
}

// TestBuildPrompt_花括号安全 测试 passage 含 { 特殊字符不破坏 Replacer（Go 增强 §7.2-4）
func TestBuildPrompt_花括号安全(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("var x = {a: 1}; func() { return 2; }", "T")
	// 花括号原样保留（NewReplacer 只替换 {title}/{passage} 字面量，
	// passage 内部花括号替换后注入不参与二次解析）
	if !strings.Contains(prompt, "var x = {a: 1}") {
		t.Error("passage 花括号应原样保留")
	}
	if !strings.Contains(prompt, "Title:\nT\n") {
		t.Error("标题替换应不受 passage 花括号影响")
	}
}

// TestBuildPrompt_模板完整性 测试模板关键段落齐备（Go 增强：防逐字复制走样）
func TestBuildPrompt_模板完整性(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("p", "t")
	for _, want := range []string{
		"# Instruction",
		"# Demonstration 1",
		"# Demonstration 2",
		"Magic Johnson",
		"Elden Ring",
		`"named_entities"`,
		`"triples"`,
		`If no triples are found, return {"named_entities": [...], "triples": []}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("模板应包含 %q", want)
		}
	}
}
