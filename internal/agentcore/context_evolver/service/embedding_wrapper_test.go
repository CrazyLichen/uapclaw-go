package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockEmbeddingClient 模拟 BaseEmbedding，记录调用参数。
type mockEmbeddingClient struct {
	// embedQueryFn 自定义 EmbedQuery 行为
	embedQueryFn func(ctx context.Context, text string, opts ...embedding.EmbedOption) ([]float64, error)
	// embedDocumentsFn 自定义 EmbedDocuments 行为
	embedDocumentsFn func(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error)
	// embedQueryCalls EmbedQuery 调用次数
	embedQueryCalls int
	// embedDocsCalls EmbedDocuments 调用次数
	embedDocsCalls int
	// lastQueryText 最近一次 EmbedQuery 的文本
	lastQueryText string
	// lastDocsTexts 最近一次 EmbedDocuments 的文本列表
	lastDocsTexts []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// EmbedQuery 实现 BaseEmbedding.EmbedQuery。
func (m *mockEmbeddingClient) EmbedQuery(ctx context.Context, text string, opts ...embedding.EmbedOption) ([]float64, error) {
	m.embedQueryCalls++
	m.lastQueryText = text
	if m.embedQueryFn != nil {
		return m.embedQueryFn(ctx, text, opts...)
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

// EmbedDocuments 实现 BaseEmbedding.EmbedDocuments。
func (m *mockEmbeddingClient) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	m.embedDocsCalls++
	m.lastDocsTexts = texts
	if m.embedDocumentsFn != nil {
		return m.embedDocumentsFn(ctx, texts, opts...)
	}
	result := make([][]float64, len(texts))
	for i := range texts {
		result[i] = []float64{0.1, 0.2, 0.3}
	}
	return result, nil
}

// Dimension 实现 BaseEmbedding.Dimension。
func (m *mockEmbeddingClient) Dimension() int {
	return 3
}

// DimensionWithContext 实现 BaseEmbedding.DimensionWithContext。
func (m *mockEmbeddingClient) DimensionWithContext(_ context.Context) (int, error) {
	return 3, nil
}

// TestNewOpenAIEmbeddingWrapper_APIKey缺失 验证 API key 为空时返回错误。
func TestNewOpenAIEmbeddingWrapper_APIKey缺失(t *testing.T) {
	_, err := NewOpenAIEmbeddingWrapper("text-embedding-3-small", "", "https://api.openai.com/v1")
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestOpenAIEmbeddingWrapper_Embed_基本调用 验证 Embed 正确委托到 EmbedQuery。
func TestOpenAIEmbeddingWrapper_Embed_基本调用(t *testing.T) {
	mock := &mockEmbeddingClient{}
	wrapper := NewOpenAIEmbeddingWrapperWithClient("text-embedding-3-small", mock)

	vec, err := wrapper.Embed(context.Background(), "hello world")
	require.NoError(t, err)
	assert.Equal(t, []float64{0.1, 0.2, 0.3}, vec)
	assert.Equal(t, 1, mock.embedQueryCalls)
	assert.Equal(t, "hello world", mock.lastQueryText)
}

// TestOpenAIEmbeddingWrapper_Embed_调用失败 验证错误处理。
func TestOpenAIEmbeddingWrapper_Embed_调用失败(t *testing.T) {
	mock := &mockEmbeddingClient{
		embedQueryFn: func(_ context.Context, _ string, _ ...embedding.EmbedOption) ([]float64, error) {
			return nil, fmt.Errorf("network error")
		},
	}
	wrapper := NewOpenAIEmbeddingWrapperWithClient("text-embedding-3-small", mock)

	_, err := wrapper.Embed(context.Background(), "hello")
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestOpenAIEmbeddingWrapper_Embed_BaseError透传 验证 BaseError 直接透传。
func TestOpenAIEmbeddingWrapper_Embed_BaseError透传(t *testing.T) {
	originalErr := exception.NewBaseError(
		exception.StatusToolchainEvolvingMemoryConfigInvalid,
		exception.WithMsg("test base error"),
	)
	mock := &mockEmbeddingClient{
		embedQueryFn: func(_ context.Context, _ string, _ ...embedding.EmbedOption) ([]float64, error) {
			return nil, originalErr
		},
	}
	wrapper := NewOpenAIEmbeddingWrapperWithClient("text-embedding-3-small", mock)

	_, err := wrapper.Embed(context.Background(), "hello")
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestOpenAIEmbeddingWrapper_EmbedBatch_基本调用 验证 EmbedBatch 正确委托到 EmbedDocuments。
func TestOpenAIEmbeddingWrapper_EmbedBatch_基本调用(t *testing.T) {
	mock := &mockEmbeddingClient{}
	wrapper := NewOpenAIEmbeddingWrapperWithClient("text-embedding-3-small", mock)

	vecs, err := wrapper.EmbedBatch(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	assert.Equal(t, 2, len(vecs))
	assert.Equal(t, 1, mock.embedDocsCalls)
	assert.Equal(t, []string{"hello", "world"}, mock.lastDocsTexts)
}

// TestOpenAIEmbeddingWrapper_EmbedBatch_空列表 验证空输入返回空列表。
func TestOpenAIEmbeddingWrapper_EmbedBatch_空列表(t *testing.T) {
	mock := &mockEmbeddingClient{}
	wrapper := NewOpenAIEmbeddingWrapperWithClient("text-embedding-3-small", mock)

	vecs, err := wrapper.EmbedBatch(context.Background(), []string{})
	require.NoError(t, err)
	assert.Equal(t, 0, len(vecs))
	// 空列表不应调用 EmbedDocuments
	assert.Equal(t, 0, mock.embedDocsCalls)
}

// TestOpenAIEmbeddingWrapper_EmbedBatch_调用失败 验证错误处理。
func TestOpenAIEmbeddingWrapper_EmbedBatch_调用失败(t *testing.T) {
	mock := &mockEmbeddingClient{
		embedDocumentsFn: func(_ context.Context, _ []string, _ ...embedding.EmbedOption) ([][]float64, error) {
			return nil, fmt.Errorf("batch error")
		},
	}
	wrapper := NewOpenAIEmbeddingWrapperWithClient("text-embedding-3-small", mock)

	_, err := wrapper.EmbedBatch(context.Background(), []string{"hello"})
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestOpenAIEmbeddingWrapper_EmbedBatch_BaseError透传 验证 BaseError 直接透传。
func TestOpenAIEmbeddingWrapper_EmbedBatch_BaseError透传(t *testing.T) {
	originalErr := exception.NewBaseError(
		exception.StatusToolchainEvolvingMemoryEmbeddingExecutionError,
		exception.WithMsg("embedding error"),
	)
	mock := &mockEmbeddingClient{
		embedDocumentsFn: func(_ context.Context, _ []string, _ ...embedding.EmbedOption) ([][]float64, error) {
			return nil, originalErr
		},
	}
	wrapper := NewOpenAIEmbeddingWrapperWithClient("text-embedding-3-small", mock)

	_, err := wrapper.EmbedBatch(context.Background(), []string{"hello"})
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestOpenAIEmbeddingWrapper_String 验证 String 方法。
func TestOpenAIEmbeddingWrapper_String(t *testing.T) {
	wrapper := &OpenAIEmbeddingWrapper{modelName: "text-embedding-3-small"}
	result := wrapper.String()
	assert.Contains(t, result, "text-embedding-3-small")
}

// TestOpenAIEmbeddingWrapper_实现EmbeddingService接口 验证接口实现。
func TestOpenAIEmbeddingWrapper_实现EmbeddingService接口(t *testing.T) {
	// 编译期接口断言
	var _ cecontext.EmbeddingService = (*OpenAIEmbeddingWrapper)(nil)
}
