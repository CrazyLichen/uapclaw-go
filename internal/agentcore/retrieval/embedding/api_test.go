package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
)

// newTestAPIServer 创建返回指定嵌入响应的测试 HTTP 服务
func newTestAPIServer(responseBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法和内容类型
		assert.Equal(nil, http.MethodPost, r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, responseBody)
	}))
}

func TestAPIEmbedding_EmbedQuery(t *testing.T) {
	server := newTestAPIServer(`{"data": [{"embedding": [0.1, 0.2, 0.3], "index": 0}]}`)
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
		APIKey:    "test-key",
	})

	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Len(t, vec, 3)
	assert.InDelta(t, 0.1, vec[0], 0.001)
}

func TestAPIEmbedding_EmbedQuery_空文本(t *testing.T) {
	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})

	_, err := client.EmbedQuery(context.Background(), "")
	assert.Error(t, err)

	_, err = client.EmbedQuery(context.Background(), "   ")
	assert.Error(t, err)
}

func TestAPIEmbedding_EmbedDocuments(t *testing.T) {
	server := newTestAPIServer(`{"data": [{"embedding": [0.1, 0.2], "index": 0}, {"embedding": [0.3, 0.4], "index": 1}]}`)
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	})

	vecs, err := client.EmbedDocuments(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	assert.Len(t, vecs, 2)
}

func TestAPIEmbedding_EmbedDocuments_空列表(t *testing.T) {
	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})

	_, err := client.EmbedDocuments(context.Background(), []string{})
	assert.Error(t, err)
}

func TestAPIEmbedding_EmbedDocuments_回调(t *testing.T) {
	server := newTestAPIServer(`{"data": [{"embedding": [0.1, 0.2], "index": 0}]}`)
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	}, WithAPIMaxBatchSize(1))

	cb := NewNoopCallback()
	vecs, err := client.EmbedDocuments(context.Background(), []string{"a"}, embedding.EmbedOption{Callback: cb})
	require.NoError(t, err)
	assert.Len(t, vecs, 1)
	assert.Equal(t, 1, cb.CallCounter())
}

func TestAPIEmbedding_Dimension_自动探测(t *testing.T) {
	server := newTestAPIServer(`{"data": [{"embedding": [0.1, 0.2, 0.3, 0.4], "index": 0}]}`)
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	})

	dim := client.Dimension()
	assert.Equal(t, 4, dim)

	// 第二次调用应使用缓存，不再请求
	dim2 := client.Dimension()
	assert.Equal(t, 4, dim2)
}

func TestAPIEmbedding_响应格式_embedding(t *testing.T) {
	server := newTestAPIServer(`{"embedding": [0.1, 0.2, 0.3]}`)
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	})

	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Len(t, vec, 3)
}

func TestAPIEmbedding_响应格式_embeddings(t *testing.T) {
	server := newTestAPIServer(`{"embeddings": [[0.1, 0.2], [0.3, 0.4]]}`)
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	})

	vecs, err := client.EmbedDocuments(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	assert.Len(t, vecs, 2)
}

func TestAPIEmbedding_响应格式_data(t *testing.T) {
	server := newTestAPIServer(`{"data": [{"embedding": [0.1, 0.2], "index": 0}]}`)
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	})

	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Len(t, vec, 2)
}

func TestAPIEmbedding_请求头(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data": [{"embedding": [0.1], "index": 0}]}`)
	}))
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
		APIKey:    "sk-test-key",
	})

	_, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)

	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "Bearer sk-test-key", receivedHeaders.Get("Authorization"))
}

func TestAPIEmbedding_请求Payload(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data": [{"embedding": [0.1], "index": 0}]}`)
	}))
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   server.URL,
	})

	_, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)

	assert.Equal(t, "text-embedding-3-small", receivedBody["model"])
	assert.Equal(t, "hello", receivedBody["input"])
}

func TestAPIEmbedding_服务端错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error": "internal server error"}`)
	}))
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	}, WithAPIMaxRetries(1))

	_, err := client.EmbedQuery(context.Background(), "hello")
	assert.Error(t, err)
}

func TestAPIEmbedding_接口约束(t *testing.T) {
	// 验证 APIEmbedding 满足 BaseEmbedding 接口
	var _ embedding.BaseEmbedding = &APIEmbedding{}
}
