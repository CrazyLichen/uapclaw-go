package lite

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"strings"

	baseEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	apiEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EmbeddingProvider 嵌入向量提供者接口。对齐 Python EmbeddingProvider
type EmbeddingProvider interface {
	// EmbedQuery 嵌入单个查询文本
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	// EmbedDocuments 嵌入多个文档文本
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
	// ID 返回提供者标识。对齐 Python: self.provider.id
	ID() string
	// Model 返回提供者模型名。对齐 Python: self.provider.model
	Model() string
	// Dims 返回嵌入向量维度。对齐 Python: self.provider.dims
	Dims() int
}

// MockEmbeddingProvider 模拟嵌入提供者。对齐 Python MockEmbeddingProvider
type MockEmbeddingProvider struct {
	// id 提供者标识
	id string
	// model 提供者模型名
	model string
	// dims 嵌入向量维度。对齐 Python MockEmbeddingProvider.dims = 128
	dims int
}

// baseEmbeddingAdapter 将 foundation/store/embedding.BaseEmbedding 适配为 lite.EmbeddingProvider
type baseEmbeddingAdapter struct {
	base  baseEmbedding.BaseEmbedding
	prov  string // 提供者标识
	model string // 提供者模型名
	dims  int    // 嵌入向量维度
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockEmbeddingProvider 创建模拟嵌入提供者
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{id: "mock", model: "mock", dims: 128}
}

// EmbedQuery 返回基于文本 hash 的 128 维确定性随机向量。
//
// 对齐 Python: MockEmbeddingProvider.embed_query — random.seed(md5(text).hexdigest()), [random.uniform(-1,1) for _ in range(128)]
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	h := md5.Sum([]byte(text))
	seed := int64(binary.BigEndian.Uint64(h[:8]))
	r := rand.New(rand.NewSource(seed))
	vec := make([]float64, 128)
	for i := range vec {
		vec[i] = r.Float64()*2 - 1 // uniform(-1, 1)
	}
	return vec, nil
}

// EmbedDocuments 批量嵌入文档。
//
// 对齐 Python: MockEmbeddingProvider.embed_documents
func (m *MockEmbeddingProvider) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := m.EmbedQuery(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = vec
	}
	return result, nil
}

// ID 返回提供者标识
func (m *MockEmbeddingProvider) ID() string { return m.id }

// Model 返回提供者模型名
func (m *MockEmbeddingProvider) Model() string { return m.model }

// Dims 返回嵌入向量维度
func (m *MockEmbeddingProvider) Dims() int { return m.dims }

// EmbedQuery baseEmbeddingAdapter 的 EmbedQuery 实现，委托给 base
func (a *baseEmbeddingAdapter) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	return a.base.EmbedQuery(ctx, text)
}

// EmbedDocuments baseEmbeddingAdapter 的 EmbedDocuments 实现，委托给 base
func (a *baseEmbeddingAdapter) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	return a.base.EmbedDocuments(ctx, texts)
}

// ID 返回提供者标识
func (a *baseEmbeddingAdapter) ID() string { return a.prov }

// Model 返回提供者模型名
func (a *baseEmbeddingAdapter) Model() string { return a.model }

// Dims 返回嵌入向量维度
func (a *baseEmbeddingAdapter) Dims() int { return a.dims }

// ResolveEmbeddingConfigFromEnv 从环境变量构建 EmbeddingConfig。对齐 Python resolve_embedding_config_from_env
func ResolveEmbeddingConfigFromEnv(modelName, fallbackBaseURL, fallbackAPIKey string) *apiEmbedding.EmbeddingConfig {
	envModelName := os.Getenv("EMBEDDING_MODEL_NAME")
	if envModelName == "" {
		envModelName = modelName
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
	// 对齐 Python: if model_name and base_url and api_key → 返回配置，否则返回 None
	if envModelName == "" || baseURL == "" || apiKey == "" {
		return nil
	}
	return &apiEmbedding.EmbeddingConfig{
		ModelName: envModelName,
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
		// 对齐 Python: base_url 以 /embeddings 结尾时裁剪
		embeddingConfig.BaseURL = strings.TrimSuffix(embeddingConfig.BaseURL, "/embeddings")
		base := apiEmbedding.NewAPIEmbedding(*embeddingConfig)
		return &baseEmbeddingAdapter{base: base, prov: provider, model: model, dims: 1024}, nil
	}

	// fallback 到 mock
	// 对齐 Python: logger.warning("Embedding API key not found, using mock provider")
	if fallback == "mock" || fallback == "" {
		logger.Warn(logComponent).Msg("Embedding API key not found, using mock provider")
		return NewMockEmbeddingProvider(), nil
	}

	return nil, fmt.Errorf("嵌入提供者未配置: provider=%s, model=%s", provider, model)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
