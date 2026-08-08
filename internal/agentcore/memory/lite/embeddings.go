package lite

import (
	"context"
	"fmt"
	"os"

	baseEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	apiEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 接口 ────────────────────────────

// EmbeddingProvider 嵌入向量提供者接口。对齐 Python EmbeddingProvider
type EmbeddingProvider interface {
	// EmbedQuery 嵌入单个查询文本
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	// EmbedDocuments 嵌入多个文档文本
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// MockEmbeddingProvider 模拟嵌入提供者。对齐 Python MockEmbeddingProvider
type MockEmbeddingProvider struct{}

// baseEmbeddingAdapter 将 foundation/store/embedding.BaseEmbedding 适配为 lite.EmbeddingProvider
type baseEmbeddingAdapter struct {
	base baseEmbedding.BaseEmbedding
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockEmbeddingProvider 创建模拟嵌入提供者
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{}
}

// EmbedQuery MockEmbeddingProvider 的 EmbedQuery 实现，返回空向量
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, _ string) ([]float64, error) {
	return make([]float64, 0), nil
}

// EmbedDocuments MockEmbeddingProvider 的 EmbedDocuments 实现，返回空切片
func (m *MockEmbeddingProvider) EmbedDocuments(_ context.Context, _ []string) ([][]float64, error) {
	return nil, nil
}

// EmbedQuery baseEmbeddingAdapter 的 EmbedQuery 实现，委托给 base
func (a *baseEmbeddingAdapter) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	return a.base.EmbedQuery(ctx, text)
}

// EmbedDocuments baseEmbeddingAdapter 的 EmbedDocuments 实现，委托给 base
func (a *baseEmbeddingAdapter) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	return a.base.EmbedDocuments(ctx, texts)
}

// ResolveEmbeddingConfigFromEnv 从环境变量构建 EmbeddingConfig。对齐 Python resolve_embedding_config_from_env
func ResolveEmbeddingConfigFromEnv(modelName, fallbackBaseURL, fallbackAPIKey string) *apiEmbedding.EmbeddingConfig {
	modelName = os.Getenv("EMBEDDING_MODEL_NAME")
	if modelName == "" {
		modelName = os.Getenv("EMBED_MODEL")
	}
	baseURL := os.Getenv("EMBEDDING_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("EMBED_BASE_URL")
	}
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
	apiKey := os.Getenv("EMBEDDING_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("EMBED_API_KEY")
	}
	if apiKey == "" {
		apiKey = fallbackAPIKey
	}
	if modelName == "" {
		modelName = "default"
	}
	if baseURL == "" || apiKey == "" {
		return nil
	}
	return &apiEmbedding.EmbeddingConfig{
		ModelName: modelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}
}

// CreateEmbeddingProvider 根据配置创建嵌入提供者。对齐 Python create_embedding_provider
func CreateEmbeddingProvider(provider, model, fallback string, embeddingConfig *apiEmbedding.EmbeddingConfig) (EmbeddingProvider, error) {
	if provider == "mock" {
		return NewMockEmbeddingProvider(), nil
	}

	// 优先使用 embeddingConfig
	if embeddingConfig != nil && embeddingConfig.APIKey != "" {
		base := apiEmbedding.NewAPIEmbedding(*embeddingConfig)
		return &baseEmbeddingAdapter{base: base}, nil
	}

	// fallback 到 mock
	if fallback == "mock" || fallback == "" {
		return NewMockEmbeddingProvider(), nil
	}

	return nil, fmt.Errorf("嵌入提供者未配置: provider=%s, model=%s", provider, model)
}
