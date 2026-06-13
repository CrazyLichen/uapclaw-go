//go:build llm

package embedding

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDashscopeEmbedding_真实调用_EmbedQuery 测试 DashScope Embedding 真实调用
// 运行: go test -tags=llm ./internal/agentcore/retrieval/embedding/...
func TestDashscopeEmbedding_真实调用_EmbedQuery(t *testing.T) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY 环境变量未设置")
	}

	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   "https://dashscope.aliyuncs.com/api/v1/services/embeddings/text-embedding/text-embedding",
		APIKey:    apiKey,
	})

	vec, err := client.EmbedQuery(context.Background(), "你好，世界！")
	require.NoError(t, err)
	assert.NotEmpty(t, vec)
	t.Logf("嵌入维度: %d", len(vec))
}
