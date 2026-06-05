package openai

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestClientConfig 创建测试用的客户端配置（关闭 SSL 验证以避免证书要求）
func newTestClientConfig(provider, apiKey, apiBase string) *llmschema.ModelClientConfig {
	return llmschema.NewModelClientConfig(provider, apiKey, apiBase, llmschema.WithVerifySSL(false))
}

// newTestModelConfig 创建测试用的模型请求配置
func newTestModelConfig() *llmschema.ModelRequestConfig {
	return llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-4"))
}

// ──────────────────────────── NewOpenAIModelClient 测试 ────────────────────────────

func TestNewOpenAIModelClient_ValidConfig(t *testing.T) {
	// 有效配置创建客户端成功
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
	if err != nil {
		t.Fatalf("NewOpenAIModelClient 返回错误: %v", err)
	}
	if client == nil {
		t.Fatal("client 不应为 nil")
	}
}

func TestNewOpenAIModelClient_NoAPIKey(t *testing.T) {
	// 缺少 API Key 应失败
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "", "https://api.openai.com/v1"))
	if err == nil {
		t.Error("缺少 API Key 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Key 时 client 应为 nil")
	}
}

func TestNewOpenAIModelClient_NoAPIBase(t *testing.T) {
	// 缺少 API Base 应失败
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", ""))
	if err == nil {
		t.Error("缺少 API Base 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Base 时 client 应为 nil")
	}
}

// ──────────────────────────── 接口合规性测试 ────────────────────────────

func TestOpenAIModelClient_ImplementsBaseModelClient(t *testing.T) {
	// 验证 OpenAIModelClient 实现了 BaseModelClient 接口
	var _ model_clients.BaseModelClient = (*OpenAIModelClient)(nil)
}

// ──────────────────────────── 不支持的方法测试 ────────────────────────────

func TestOpenAIModelClient_GenerateImageReturnsError(t *testing.T) {
	// GenerateImage 应返回错误
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result, err := client.GenerateImage(context.Background(), nil)
	if err == nil {
		t.Error("GenerateImage 应返回错误")
	}
	if result != nil {
		t.Error("GenerateImage 结果应为 nil")
	}
}

func TestOpenAIModelClient_GenerateSpeechReturnsError(t *testing.T) {
	// GenerateSpeech 应返回错误
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result, err := client.GenerateSpeech(context.Background(), nil)
	if err == nil {
		t.Error("GenerateSpeech 应返回错误")
	}
	if result != nil {
		t.Error("GenerateSpeech 结果应为 nil")
	}
}

func TestOpenAIModelClient_GenerateVideoReturnsError(t *testing.T) {
	// GenerateVideo 应返回错误
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result, err := client.GenerateVideo(context.Background(), nil)
	if err == nil {
		t.Error("GenerateVideo 应返回错误")
	}
	if result != nil {
		t.Error("GenerateVideo 结果应为 nil")
	}
}
