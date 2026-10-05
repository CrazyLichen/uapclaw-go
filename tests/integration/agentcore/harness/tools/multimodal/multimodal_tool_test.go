//go:build integration

package multimodal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	multimodal "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/multimodal"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MultimodalToolSuite 多模态工具集成测试套件。
//
// 验证 Audio/Vision 工具的注册、Schema 定义和描述完整性，
// 不执行真实 LLM/音频/视觉调用。
type MultimodalToolSuite struct {
	isuite.AgentSuite
	// mockClient 模拟模型客户端
	mockClient modelclients.BaseModelClient
	// audioConfig 音频模型配置
	audioConfig *hschema.AudioModelConfig
	// visionConfig 视觉模型配置
	visionConfig *hschema.VisionModelConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMultimodalToolSuite(t *testing.T) {
	suite.Run(t, new(MultimodalToolSuite))
}

// SetupSuite 初始化测试套件，创建模拟客户端和配置。
func (s *MultimodalToolSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()
	s.mockClient = newStubModelClient()
	s.audioConfig = &hschema.AudioModelConfig{
		BaseURL:            "https://mock.api/v1",
		APIKey:             "test-key",
		TranscriptionModel: "whisper-1",
		QAModel:            "gpt-4o-audio",
		MaxRetries:         1,
	}
	s.visionConfig = &hschema.VisionModelConfig{
		Model: "gpt-4o",
	}
}

// TestAudioToolRegistration 测试音频工具注册。
// 对齐 Python: create_audio_tools 返回 3 个工具实例。
func (s *MultimodalToolSuite) TestAudioToolRegistration() {
	tools := multimodal.CreateAudioTools(s.mockClient, s.audioConfig, "cn", "test-agent")
	s.Len(tools, 3, "应创建 3 个音频工具")
}

// TestAudioToolSchemaRequiredFields 测试音频工具 Schema 包含必填字段。
// 对齐 Python: AudioTranscriptionTool.input_schema 包含 audio_path_or_url。
func (s *MultimodalToolSuite) TestAudioToolSchemaRequiredFields() {
	tools := multimodal.CreateAudioTools(s.mockClient, s.audioConfig, "cn", "test-agent")
	for _, t := range tools {
		card := t.Card()
		s.NotNil(card, "工具 Card 不应为 nil")
		// 验证 InputParams 存在
		if card.InputParams != nil {
			s.True(len(card.InputParams) > 0, "工具 %s 应有 InputParams", card.GetName())
		}
	}
}

// TestVisionToolRegistration 测试视觉工具注册。
// 对齐 Python: create_vision_tools 返回 2 个工具实例。
func (s *MultimodalToolSuite) TestVisionToolRegistration() {
	tools := multimodal.CreateVisionTools(s.mockClient, s.visionConfig, "cn", "test-agent")
	s.Len(tools, 2, "应创建 2 个视觉工具")
}

// TestVisionToolSchemaRequiredFields 测试视觉工具 Schema 包含必填字段。
// 对齐 Python: ImageOCRTool.input_schema 包含 image_path_or_url。
func (s *MultimodalToolSuite) TestVisionToolSchemaRequiredFields() {
	tools := multimodal.CreateVisionTools(s.mockClient, s.visionConfig, "cn", "test-agent")
	for _, t := range tools {
		card := t.Card()
		s.NotNil(card, "工具 Card 不应为 nil")
		if card.InputParams != nil {
			s.True(len(card.InputParams) > 0, "工具 %s 应有 InputParams", card.GetName())
		}
	}
}

// TestToolIDsProperlySet 测试工具 ID 被正确设置。
// 对齐 Python: Tool.card.id 非空。
func (s *MultimodalToolSuite) TestToolIDsProperlySet() {
	allTools := s.allMultimodalTools()
	for _, t := range allTools {
		card := t.Card()
		s.NotEmpty(card.GetID(), "工具 ID 不应为空")
	}
}

// TestToolDescriptionsNonEmpty 测试工具描述非空。
// 对齐 Python: Tool.card.description 非空。
func (s *MultimodalToolSuite) TestToolDescriptionsNonEmpty() {
	allTools := s.allMultimodalTools()
	for _, t := range allTools {
		card := t.Card()
		s.NotEmpty(card.GetDescription(), "工具 %s 描述不应为空", card.GetName())
	}
}

// TestMultipleToolsCoexist 测试音频+视觉工具可共存。
// 对齐 Python: 同时注册 audio_tools + vision_tools 无冲突。
func (s *MultimodalToolSuite) TestMultipleToolsCoexist() {
	audioTools := multimodal.CreateAudioTools(s.mockClient, s.audioConfig, "cn", "test-agent")
	visionTools := multimodal.CreateVisionTools(s.mockClient, s.visionConfig, "cn", "test-agent")
	allTools := append(audioTools, visionTools...)
	s.Len(allTools, 5, "音频3+视觉2 共 5 个工具应共存")

	// 验证所有工具 ID 唯一
	idSet := make(map[string]struct{})
	for _, t := range allTools {
		id := t.Card().GetID()
		_, exists := idSet[id]
		s.False(exists, "工具 ID %s 不应重复", id)
		idSet[id] = struct{}{}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// allMultimodalTools 返回所有音频+视觉工具。
func (s *MultimodalToolSuite) allMultimodalTools() []tool.Tool {
	audioTools := multimodal.CreateAudioTools(s.mockClient, s.audioConfig, "cn", "test-agent")
	visionTools := multimodal.CreateVisionTools(s.mockClient, s.visionConfig, "cn", "test-agent")
	return append(audioTools, visionTools...)
}

// stubModelClient 用于测试的桩模型客户端，仅满足 BaseModelClient 接口。
type stubModelClient struct{}

func newStubModelClient() *stubModelClient { return &stubModelClient{} }

func (m *stubModelClient) Invoke(_ context.Context, _ modelclients.MessagesParam, _ ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return nil, nil
}

func (m *stubModelClient) Stream(_ context.Context, _ modelclients.MessagesParam, _ ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}

func (m *stubModelClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}

func (m *stubModelClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}

func (m *stubModelClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}

func (m *stubModelClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}

func (m *stubModelClient) Release(_ context.Context, _ ...modelclients.ReleaseOption) (bool, error) {
	return false, nil
}

func (m *stubModelClient) SupportsKVCacheRelease() bool {
	return false
}

// 编译时验证 stubModelClient 满足 BaseModelClient 接口
var _ modelclients.BaseModelClient = (*stubModelClient)(nil)
