//go:build llm

package embedding

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenAIEmbedding_真实调用_EmbedQuery 测试 OpenAI Embedding 真实调用
// 运行: go test -tags=llm ./internal/agentcore/retrieval/embedding/...
func TestOpenAIEmbedding_真实调用_EmbedQuery(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY 环境变量未设置")
	}

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   baseURL,
		APIKey:    apiKey,
	})

	vec, err := client.EmbedQuery(context.Background(), "Hello, world!")
	require.NoError(t, err)
	assert.NotEmpty(t, vec)
	t.Logf("嵌入维度: %d", len(vec))
}

// TestOpenAIEmbedding_真实调用_EmbedDocuments 测试 OpenAI Embedding 批量真实调用
// 运行: go test -tags=llm ./internal/agentcore/retrieval/embedding/...
func TestOpenAIEmbedding_真实调用_EmbedDocuments(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY 环境变量未设置")
	}

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   baseURL,
		APIKey:    apiKey,
	})

	vecs, err := client.EmbedDocuments(context.Background(), []string{"Hello", "World", "Test"})
	require.NoError(t, err)
	assert.Len(t, vecs, 3)
	t.Logf("嵌入维度: %d", len(vecs[0]))
}
