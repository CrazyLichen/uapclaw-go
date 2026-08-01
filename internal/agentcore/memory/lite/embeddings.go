package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EmbeddingProvider 嵌入向量提供者接口。⤵️ 回填: 7.4
type EmbeddingProvider interface {
	// EmbedQuery 嵌入单个查询文本
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	// EmbedDocuments 嵌入多个文档文本
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
}

// MockEmbeddingProvider 模拟嵌入提供者。真实实现，返回零向量
type MockEmbeddingProvider struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockEmbeddingProvider 创建模拟嵌入提供者
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{}
}

// EmbedQuery 真实实现，返回空向量
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, _ string) ([]float64, error) {
	return make([]float64, 0), nil
}

// EmbedDocuments 真实实现，返回空切片
func (m *MockEmbeddingProvider) EmbedDocuments(_ context.Context, _ []string) ([][]float64, error) {
	return nil, nil
}

// ResolveEmbeddingConfigFromEnv 从环境变量构建 EmbeddingConfig。
// ⤵️ 回填: 7.4 — 当前返回 nil
func ResolveEmbeddingConfigFromEnv(modelName, fallbackBaseURL, fallbackAPIKey string) *embedding.EmbeddingConfig {
	return nil
}

// CreateEmbeddingProvider 根据配置创建嵌入提供者。
// ⤵️ 回填: 7.4 — 当前返回 MockEmbeddingProvider
func CreateEmbeddingProvider(provider, model, fallback string, embeddingConfig *embedding.EmbeddingConfig) (EmbeddingProvider, error) {
	return NewMockEmbeddingProvider(), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
