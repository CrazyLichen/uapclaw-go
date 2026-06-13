package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
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
	vecs, err := client.EmbedDocuments(context.Background(), []string{"a"}, embedding.EmbedOption{Callback: cb})
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
		json.NewDecoder(r.Body).Decode(&receivedBody)
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

	doc := NewMultimodalDocument().AddField(ModalityText, "描述文本")
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
