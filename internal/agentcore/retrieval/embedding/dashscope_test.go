package embedding

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

func TestDashscopeEmbedding_EmbedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"status_code": 200,
			"output": {
				"embeddings": [{"embedding": [0.1, 0.2, 0.3], "index": 0}]
			}
		}`)
	}))
	defer server.Close()

	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	vec, err := client.EmbedQuery(context.Background(), "你好世界")
	require.NoError(t, err)
	assert.Len(t, vec, 3)
	assert.InDelta(t, 0.1, vec[0], 0.001)
}

func TestDashscopeEmbedding_EmbedQuery_空文本(t *testing.T) {
	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})

	_, err := client.EmbedQuery(context.Background(), "")
	assert.Error(t, err)
}

func TestDashscopeEmbedding_EmbedDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"status_code": 200,
			"output": {
				"embeddings": [{"embedding": [0.1, 0.2], "index": 0}, {"embedding": [0.3, 0.4], "index": 1}]
			}
		}`)
	}))
	defer server.Close()

	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	vecs, err := client.EmbedDocuments(context.Background(), []string{"你好", "世界"})
	require.NoError(t, err)
	assert.Len(t, vecs, 2)
}

func TestDashscopeEmbedding_EmbedDocuments_回调(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"status_code": 200,
			"output": {
				"embeddings": [{"embedding": [0.1, 0.2], "index": 0}]
			}
		}`)
	}))
	defer server.Close()

	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	}, WithDashscopeMaxBatchSize(1))

	cb := NewNoopCallback()
	vecs, err := client.EmbedDocuments(context.Background(), []string{"a"}, embedding.WithCallback(cb))
	require.NoError(t, err)
	assert.Len(t, vecs, 1)
	assert.Equal(t, 1, cb.CallCounter())
}

func TestDashscopeEmbedding_EmbedMultimodal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"status_code": 200,
			"output": {
				"embeddings": [{"embedding": [0.1, 0.2, 0.3], "index": 0}]
			}
		}`)
	}))
	defer server.Close()

	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "multimodal-embedding-v1",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	doc, addErr := common.NewMultimodalDocument().AddField(common.ModalityText, "描述")
	require.NoError(t, addErr)
	doc, addErr = doc.AddField(common.ModalityImage, "https://example.com/img.png")
	require.NoError(t, addErr)
	vec, err := client.EmbedMultimodal(context.Background(), doc)
	require.NoError(t, err)
	assert.Len(t, vec, 3)
}

func TestDashscopeEmbedding_EmbedMultimodal_nil文档(t *testing.T) {
	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})

	_, err := client.EmbedMultimodal(context.Background(), nil)
	assert.Error(t, err)
}

func TestDashscopeEmbedding_Dimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"status_code": 200,
			"output": {
				"embeddings": [{"embedding": [0.1, 0.2, 0.3, 0.4], "index": 0}]
			}
		}`)
	}))
	defer server.Close()

	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	dim := client.Dimension()
	assert.Equal(t, 4, dim)
}

func TestDashscopeEmbedding_服务端错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"status_code": 400,
			"code": "InvalidParameter",
			"message": "model not found"
		}`)
	}))
	defer server.Close()

	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	}, WithDashscopeMaxRetries(1))

	_, err := client.EmbedQuery(context.Background(), "hello")
	assert.Error(t, err)
}

func TestDashscopeEmbedding_接口约束(t *testing.T) {
	// 验证 DashscopeEmbedding 满足 BaseEmbedding 接口
	var _ embedding.BaseEmbedding = &DashscopeEmbedding{}
	// 验证 DashscopeEmbedding 满足 MultimodalEmbedder 接口
	var _ MultimodalEmbedder = &DashscopeEmbedding{}
}

// TestDashscopeEmbedding_Option函数 验证各 Option 函数正常工作
func TestDashscopeEmbedding_Option函数(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"status_code": 200,
			"output": {
				"embeddings": [{"embedding": [0.1], "index": 0}]
			}
		}`)
	}))
	defer server.Close()

	customClient := &http.Client{Timeout: 5 * time.Second}
	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	},
		WithDashscopeTimeout(10*time.Second),
		WithDashscopeMaxRetries(1),
		WithDashscopeMaxBatchSize(2),
		WithDashscopeMaxConcurrent(5),
		WithDashscopeDimension(256),
		WithDashscopeHTTPClient(customClient),
		WithDashscopeExtraHeaders(map[string]string{"X-Custom": "test"}),
	)

	assert.Equal(t, 10*time.Second, client.timeout)
	assert.Equal(t, 1, client.maxRetries)
	assert.Equal(t, 2, client.maxBatchSize)
	assert.True(t, client.matryoshkaDimension)
	assert.Equal(t, 256, client.dimension)
	assert.Equal(t, customClient, client.httpClient)
	assert.NotNil(t, client.extraHeaders)
	assert.Equal(t, "test", client.extraHeaders["X-Custom"])

	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, vec)
}

// TestDashscopeEmbedding_DimensionWithContext 验证 DimensionWithContext 正常工作
func TestDashscopeEmbedding_DimensionWithContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"status_code": 200,
			"output": {
				"embeddings": [{"embedding": [0.1, 0.2, 0.3, 0.4], "index": 0}]
			}
		}`)
	}))
	defer server.Close()

	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	dim, err := client.DimensionWithContext(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 4, dim)
}

// TestDashscopeEmbedding_DimensionWithContext_已有维度 验证维度缓存
func TestDashscopeEmbedding_DimensionWithContext_已有维度(t *testing.T) {
	client := NewDashscopeEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
		APIKey:    "sk-test",
	}, WithDashscopeDimension(128))

	dim, err := client.DimensionWithContext(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 128, dim)
}
