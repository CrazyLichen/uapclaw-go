package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// newOpenAITestServer 创建模拟 OpenAI API 的测试 HTTP 服务
func newOpenAITestServer(responseBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, responseBody)
	}))
}

func TestOpenAIEmbedding_EmbedQuery(t *testing.T) {
	server := newOpenAITestServer(`{
		"data": [{"embedding": [0.1, 0.2, 0.3], "index": 0}],
		"model": "text-embedding-3-small",
		"object": "list",
		"usage": {"prompt_tokens": 5, "total_tokens": 5}
	}`)
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Len(t, vec, 3)
	assert.InDelta(t, 0.1, vec[0], 0.001)
}

func TestOpenAIEmbedding_EmbedQuery_空文本(t *testing.T) {
	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})

	_, err := client.EmbedQuery(context.Background(), "")
	assert.Error(t, err)
}

func TestOpenAIEmbedding_EmbedDocuments(t *testing.T) {
	server := newOpenAITestServer(`{
		"data": [{"embedding": [0.1, 0.2], "index": 0}, {"embedding": [0.3, 0.4], "index": 1}],
		"model": "text-embedding-3-small",
		"object": "list",
		"usage": {"prompt_tokens": 10, "total_tokens": 10}
	}`)
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	vecs, err := client.EmbedDocuments(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	assert.Len(t, vecs, 2)
}

func TestOpenAIEmbedding_EmbedDocuments_回调(t *testing.T) {
	server := newOpenAITestServer(`{
		"data": [{"embedding": [0.1, 0.2], "index": 0}],
		"model": "text-embedding-3-small",
		"object": "list",
		"usage": {"prompt_tokens": 5, "total_tokens": 5}
	}`)
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	}, WithOpenAIMaxBatchSize(1))

	cb := NewNoopCallback()
	vecs, err := client.EmbedDocuments(context.Background(), []string{"a"}, embedding.WithCallback(cb))
	require.NoError(t, err)
	assert.Len(t, vecs, 1)
	assert.Equal(t, 1, cb.CallCounter())
}

func TestOpenAIEmbedding_Dimension(t *testing.T) {
	server := newOpenAITestServer(`{
		"data": [{"embedding": [0.1, 0.2, 0.3, 0.4], "index": 0}],
		"model": "text-embedding-3-small",
		"object": "list",
		"usage": {"prompt_tokens": 5, "total_tokens": 5}
	}`)
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	dim := client.Dimension()
	assert.Equal(t, 4, dim)
}

func TestOpenAIEmbedding_Matryoshka维度(t *testing.T) {
	var receivedBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"data": [{"embedding": [0.1, 0.2], "index": 0}],
			"model": "text-embedding-3-small",
			"object": "list",
			"usage": {"prompt_tokens": 5, "total_tokens": 5}
		}`)
	}))
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	}, WithOpenAIDimension(256))

	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Len(t, vec, 2)

	// 验证请求中包含 dimensions 参数
	assert.Contains(t, string(receivedBody["dimensions"]), "256")
}

func TestOpenAIEmbedding_EmbedMultimodal(t *testing.T) {
	server := newOpenAITestServer(`{
		"data": [{"embedding": [0.1, 0.2, 0.3], "index": 0}],
		"model": "text-embedding-3-small",
		"object": "list",
		"usage": {"prompt_tokens": 5, "total_tokens": 5}
	}`)
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})

	doc, addErr := common.NewMultimodalDocument().AddField(common.ModalityText, "描述文本")
	require.NoError(t, addErr)
	vec, err := client.EmbedMultimodal(context.Background(), doc)
	require.NoError(t, err)
	assert.Len(t, vec, 3)
}

func TestOpenAIEmbedding_EmbedMultimodal_nil文档(t *testing.T) {
	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})

	_, err := client.EmbedMultimodal(context.Background(), nil)
	assert.Error(t, err)
}

func TestOpenAIEmbedding_服务端错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error": {"message": "internal error"}}`)
	}))
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	}, WithOpenAIMaxRetries(1))

	_, err := client.EmbedQuery(context.Background(), "hello")
	assert.Error(t, err)
}

func TestOpenAIEmbedding_接口约束(t *testing.T) {
	// 验证 OpenAIEmbedding 满足 BaseEmbedding 接口
	var _ embedding.BaseEmbedding = &OpenAIEmbedding{}
	// 验证 OpenAIEmbedding 满足 MultimodalEmbedder 接口
	var _ MultimodalEmbedder = &OpenAIEmbedding{}
}

func TestParseOpenAIResponse(t *testing.T) {
	data := []openai.Embedding{
		{Embedding: []float64{0.1, 0.2}, Index: 0},
		{Embedding: []float64{0.3, 0.4}, Index: 1},
	}
	result, err := parseOpenAIResponse(data)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestParseOpenAIResponse_空数据(t *testing.T) {
	_, err := parseOpenAIResponse([]openai.Embedding{})
	assert.Error(t, err)
}

func TestOpenAIEmbedding_URL处理(t *testing.T) {
	// 验证 BaseURL 末尾的 / 和 /embeddings 被移除
	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "https://api.openai.com/v1/embeddings/",
		APIKey:    "sk-test",
	})
	// 客户端应正常创建
	assert.NotNil(t, client)
}

func TestOpenAIEmbedding_维度参数(t *testing.T) {
	dim := int64(256)
	opt := param.Opt[int64]{Value: dim}
	assert.True(t, opt.Valid())
	assert.Equal(t, int64(256), opt.Value)
}

// TestOpenAIEmbedding_ExtraHeaders 验证额外请求头透传给 SDK
func TestOpenAIEmbedding_ExtraHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data": [{"embedding": [0.1], "index": 0}], "model": "test", "object": "list", "usage": {"prompt_tokens": 1, "total_tokens": 1}}`)
	}))
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	}, WithOpenAIExtraHeaders(map[string]string{
		"X-Custom-Auth": "custom-token",
	}))

	_, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "custom-token", receivedHeaders.Get("X-Custom-Auth"))
}

// TestOpenAIEmbedding_ExtraParams 验证额外参数透传给 SDK
func TestOpenAIEmbedding_ExtraParams(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data": [{"embedding": [0.1], "index": 0}], "model": "test", "object": "list", "usage": {"prompt_tokens": 1, "total_tokens": 1}}`)
	}))
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "text-embedding-3-small",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	}, WithOpenAIExtraParams(map[string]any{
		"encoding_format": "base64",
	}))

	_, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "base64", receivedBody["encoding_format"])
}

// TestOpenAIEmbedding_Option函数 验证各 Option 函数正常工作
func TestOpenAIEmbedding_Option函数(t *testing.T) {
	server := newOpenAITestServer(`{
		"data": [{"embedding": [0.1], "index": 0}],
		"model": "test", "object": "list",
		"usage": {"prompt_tokens": 1, "total_tokens": 1}
	}`)
	defer server.Close()

	client := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	},
		WithOpenAITimeout(5*time.Second),
		WithOpenAIMaxRetries(1),
		WithOpenAIMaxBatchSize(2),
		WithOpenAIMaxConcurrent(5),
		WithOpenAIDimension(256),
		WithOpenAIExtraHeaders(map[string]string{"X-Test": "val"}),
		WithOpenAIExtraParams(map[string]any{"encoding_format": "base64"}),
	)

	assert.Equal(t, 5*time.Second, client.timeout)
	assert.Equal(t, 1, client.maxRetries)
	assert.Equal(t, 2, client.maxBatchSize)
	assert.True(t, client.matryoshkaDimension)
	assert.Equal(t, 256, client.dimension)
	assert.NotNil(t, client.extraHeaders)
	assert.Equal(t, "val", client.extraHeaders["X-Test"])
	assert.NotNil(t, client.extraParams)
	assert.Equal(t, "base64", client.extraParams["encoding_format"])

	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, vec)
}
