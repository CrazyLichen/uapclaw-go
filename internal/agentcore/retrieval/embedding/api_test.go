package embedding

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
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

func TestAPIEmbedding_Option函数(t *testing.T) {
	server := newTestAPIServer(`{"data": [{"embedding": [0.1], "index": 0}]}`)
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	},
		WithAPITimeout(10*time.Second),
		WithAPIMaxRetries(1),
		WithAPIMaxBatchSize(2),
		WithAPIMaxConcurrent(5),
		WithAPIExtraHeaders(map[string]string{"X-Custom": "test"}),
	)

	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, vec)
}

func TestAPIEmbedding_EmbedDocuments_批处理(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data": [{"embedding": [0.1, 0.2], "index": 0}]}`)
	}))
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	}, WithAPIMaxBatchSize(1))

	vecs, err := client.EmbedDocuments(context.Background(), []string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Len(t, vecs, 3)
	assert.Equal(t, 3, callCount) // 每个文本一个批次
}

func TestAPIEmbedding_EmbedDocuments_含空文本(t *testing.T) {
	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})

	_, err := client.EmbedDocuments(context.Background(), []string{"hello", ""})
	assert.Error(t, err)
}

// TestAPIEmbedding_ExtraParams 验证额外参数透传到 API payload
func TestAPIEmbedding_ExtraParams(t *testing.T) {
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
	}, WithAPIExtraParams(map[string]any{
		"encoding_format": "base64",
		"dimensions":      512,
	}))

	_, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)

	assert.Equal(t, "base64", receivedBody["encoding_format"])
	assert.Equal(t, float64(512), receivedBody["dimensions"]) // JSON 数字反序列化为 float64
}

// TestAPIEmbedding_客户端错误不重试 验证 4xx 错误不会被重试
func TestAPIEmbedding_客户端错误不重试(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusUnauthorized) // 401
		_, _ = fmt.Fprint(w, `{"error": "unauthorized"}`)
	}))
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	}, WithAPIMaxRetries(3))

	_, err := client.EmbedQuery(context.Background(), "hello")
	assert.Error(t, err)
	// 4xx 错误不应重试，应只调用 1 次
	assert.Equal(t, 1, callCount, "4xx 错误不应重试，应只调用 1 次")
}

// TestAPIEmbedding_服务端错误可重试 验证 5xx 错误会被重试
func TestAPIEmbedding_服务端错误可重试(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError) // 500
		_, _ = fmt.Fprint(w, `{"error": "internal server error"}`)
	}))
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	}, WithAPIMaxRetries(3))

	_, err := client.EmbedQuery(context.Background(), "hello")
	assert.Error(t, err)
	// 5xx 错误应重试，总共调用 3 次（初始 + 2 次重试）
	assert.Equal(t, 3, callCount, "5xx 错误应重试 3 次")
}

// TestAPIEmbedding_WithAPIHTTPClient 验证自定义 HTTP 客户端 Option
func TestAPIEmbedding_WithAPIHTTPClient(t *testing.T) {
	server := newTestAPIServer(`{"data": [{"embedding": [0.1], "index": 0}]}`)
	defer server.Close()

	customClient := &http.Client{Timeout: 5 * time.Second}
	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	}, WithAPIHTTPClient(customClient))

	assert.Equal(t, customClient, client.httpClient)
	vec, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, vec)
}

// TestAPIEmbedding_响应格式_data_base64 验证 data[] 中 base64 编码的 embedding 解码
func TestAPIEmbedding_响应格式_data_base64(t *testing.T) {
	// 构造 base64 编码的 float32 向量 [0.1, 0.2]
	vec := []float32{0.1, 0.2}
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}
	b64Str := base64.StdEncoding.EncodeToString(buf)

	server := newTestAPIServer(fmt.Sprintf(
		`{"data": [{"embedding": %q, "index": 0}]}`, b64Str,
	))
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	})

	vecs, err := client.EmbedDocuments(context.Background(), []string{"hello"})
	require.NoError(t, err)
	assert.Len(t, vecs, 1)
	assert.Len(t, vecs[0], 2)
	assert.InDelta(t, 0.1, vecs[0][0], 0.01)
	assert.InDelta(t, 0.2, vecs[0][1], 0.01)
}

// TestAPIEmbedding_ExtraParams覆盖 确保多次调用 WithAPIExtraParams 合并参数
func TestAPIEmbedding_ExtraParams覆盖(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data": [{"embedding": [0.1], "index": 0}]}`)
	}))
	defer server.Close()

	client := NewAPIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   server.URL,
	},
		WithAPIExtraParams(map[string]any{"encoding_format": "float"}),
		WithAPIExtraParams(map[string]any{"user": "test-user"}), // 合并
	)

	_, err := client.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "float", receivedBody["encoding_format"])
	assert.Equal(t, "test-user", receivedBody["user"])
}

// TestAPIEmbedding_4xx不同状态码 验证各种 4xx 错误均不重试
func TestAPIEmbedding_4xx不同状态码(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 429} {
		t.Run(fmt.Sprintf("HTTP_%d", code), func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				w.WriteHeader(code)
				_, _ = fmt.Fprint(w, `{"error": "client error"}`)
			}))
			defer server.Close()

			client := NewAPIEmbedding(EmbeddingConfig{
				ModelName: "test-model",
				BaseURL:   server.URL,
			}, WithAPIMaxRetries(3))

			_, err := client.EmbedQuery(context.Background(), "hello")
			assert.Error(t, err)
			assert.Equal(t, 1, callCount, "HTTP %d 不应重试", code)
		})
	}
}
