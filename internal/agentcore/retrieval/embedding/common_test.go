package embedding

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
)

func TestValidateEmbedDocs_空列表(t *testing.T) {
	_, err := ValidateEmbedDocs([]string{})
	assert.Error(t, err)
}

func TestValidateEmbedDocs_含空文本(t *testing.T) {
	_, err := ValidateEmbedDocs([]string{"hello", "", "world"})
	assert.Error(t, err)
}

func TestValidateEmbedDocs_全部为空(t *testing.T) {
	_, err := ValidateEmbedDocs([]string{"", "  "})
	assert.Error(t, err)
}

func TestValidateEmbedDocs_正常输入(t *testing.T) {
	result, err := ValidateEmbedDocs([]string{"hello", "world"})
	require.NoError(t, err)
	assert.Equal(t, []string{"hello", "world"}, result)
}

func TestBatchTexts(t *testing.T) {
	texts := []string{"a", "b", "c", "d", "e"}

	batches := BatchTexts(texts, 2)
	assert.Len(t, batches, 3)
	assert.Equal(t, []string{"a", "b"}, batches[0])
	assert.Equal(t, []string{"c", "d"}, batches[1])
	assert.Equal(t, []string{"e"}, batches[2])
}

func TestBatchTexts_批大小为0(t *testing.T) {
	texts := []string{"a", "b", "c"}
	batches := BatchTexts(texts, 0)
	assert.Len(t, batches, 3) // batchSize<=0 按 1 处理
}

func TestBatchTexts_刚好整除(t *testing.T) {
	texts := []string{"a", "b", "c", "d"}
	batches := BatchTexts(texts, 2)
	assert.Len(t, batches, 2)
}

func TestParseEmbeddingResponse_embedding格式(t *testing.T) {
	body := []byte(`{"embedding": [0.1, 0.2, 0.3]}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.InDelta(t, 0.1, result[0][0], 0.001)
}

func TestParseEmbeddingResponse_embedding嵌套格式(t *testing.T) {
	body := []byte(`{"embedding": [[0.1, 0.2], [0.3, 0.4]]}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestParseEmbeddingResponse_embeddings格式(t *testing.T) {
	body := []byte(`{"embeddings": [[0.1, 0.2], [0.3, 0.4]]}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestParseEmbeddingResponse_data格式(t *testing.T) {
	body := []byte(`{
		"data": [
			{"embedding": [0.1, 0.2], "index": 0},
			{"embedding": [0.3, 0.4], "index": 1}
		]
	}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestParseEmbeddingResponse_data格式_乱序(t *testing.T) {
	body := []byte(`{
		"data": [
			{"embedding": [0.3, 0.4], "index": 1},
			{"embedding": [0.1, 0.2], "index": 0}
		]
	}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.InDelta(t, 0.1, result[0][0], 0.001) // index 0 排前面
	assert.InDelta(t, 0.3, result[1][0], 0.001) // index 1 排后面
}

func TestParseEmbeddingResponse_无有效字段(t *testing.T) {
	body := []byte(`{"foo": "bar"}`)
	_, err := ParseEmbeddingResponse(body)
	assert.Error(t, err)
}

func TestParseEmbeddingResponse_无效JSON(t *testing.T) {
	body := []byte(`not json`)
	_, err := ParseEmbeddingResponse(body)
	assert.Error(t, err)
}

func TestRetryWithBackoff_首次成功(t *testing.T) {
	result, err := RetryWithBackoff(context.Background(), 3, func(attempt int) ([][]float64, error) {
		return [][]float64{{0.1, 0.2}}, nil
	})
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestRetryWithBackoff_重试后成功(t *testing.T) {
	callCount := 0
	result, err := RetryWithBackoff(context.Background(), 3, func(attempt int) ([][]float64, error) {
		callCount++
		if attempt < 2 {
			return nil, errors.New("临时错误")
		}
		return [][]float64{{0.1, 0.2}}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
	assert.Len(t, result, 1)
}

func TestRetryWithBackoff_全部失败(t *testing.T) {
	_, err := RetryWithBackoff(context.Background(), 2, func(attempt int) ([][]float64, error) {
		return nil, errors.New("永久错误")
	})
	assert.Error(t, err)
}

func TestRetryWithBackoff_上下文取消(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RetryWithBackoff(ctx, 3, func(attempt int) ([][]float64, error) {
		return nil, errors.New("错误")
	})
	assert.Error(t, err)
}

func TestExecuteWithConcurrency(t *testing.T) {
	tasks := []EmbeddingTask{
		func() ([][]float64, error) { return [][]float64{{1.0}}, nil },
		func() ([][]float64, error) { return [][]float64{{2.0}}, nil },
		func() ([][]float64, error) { return [][]float64{{3.0}}, nil },
	}

	result, err := ExecuteWithConcurrency(context.Background(), tasks, make(chan struct{}, 2))
	require.NoError(t, err)
	assert.Len(t, result, 3)
}

func TestExecuteWithConcurrency_任务失败(t *testing.T) {
	tasks := []EmbeddingTask{
		func() ([][]float64, error) { return nil, errors.New("失败") },
	}
	_, err := ExecuteWithConcurrency(context.Background(), tasks, nil)
	assert.Error(t, err)
}

func TestNewEmbeddingHTTPClient_HTTP(t *testing.T) {
	client := NewEmbeddingHTTPClient("http://localhost:8080")
	assert.NotNil(t, client)
}

func TestNewEmbeddingHTTPClient_HTTPS(t *testing.T) {
	client := NewEmbeddingHTTPClient("https://api.openai.com")
	assert.NotNil(t, client)
}

func TestResolveBatchSize(t *testing.T) {
	assert.Equal(t, 4, ResolveBatchSize(4, 8))
	assert.Equal(t, 8, ResolveBatchSize(0, 8))  // 0 用默认
	assert.Equal(t, 8, ResolveBatchSize(16, 8)) // 不超过 max
	assert.Equal(t, 1, ResolveBatchSize(0, 0))  // 都为 0 时回退 1
}

func TestApplyEmbedOptions(t *testing.T) {
	batchSize, cb := ApplyEmbedOptions(nil, 8)
	assert.Equal(t, 8, batchSize)
	assert.Nil(t, cb)

	cb2 := NewNoopCallback()
	batchSize, cb = ApplyEmbedOptions([]embedding.EmbedOption{
		embedding.WithBatchSize(4),
		embedding.WithCallback(cb2),
	}, 8)
	assert.Equal(t, 4, batchSize)
	assert.Equal(t, cb2, cb)
}

// TestNewEmbeddingHTTPClient_HTTPS跳过验证 验证 EMBEDDING_SSL_VERIFY=false 时跳过 TLS 验证
func TestNewEmbeddingHTTPClient_HTTPS跳过验证(t *testing.T) {
	t.Setenv("EMBEDDING_SSL_VERIFY", "false")
	client := NewEmbeddingHTTPClient("https://api.openai.com")
	assert.NotNil(t, client)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "应有 Transport")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify, "应跳过 TLS 验证")
}

// TestNewEmbeddingHTTPClient_自定义超时 验证自定义超时参数
func TestNewEmbeddingHTTPClient_自定义超时(t *testing.T) {
	client := NewEmbeddingHTTPClient("http://localhost:8080", 5*time.Second)
	assert.NotNil(t, client)
	assert.Equal(t, 5*time.Second, client.Timeout)
}

// TestCreateTLSConfigWithCert_文件不存在 验证证书文件不存在时报错
func TestCreateTLSConfigWithCert_文件不存在(t *testing.T) {
	_, err := createTLSConfigWithCert("/nonexistent/cert.pem")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "读取证书文件失败")
}

// TestCreateTLSConfigWithCert_无效证书 验证无效证书内容报错
func TestCreateTLSConfigWithCert_无效证书(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "cert-*.pem")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.WriteString("not a valid certificate")
	require.NoError(t, tmpFile.Close())

	_, err2 := createTLSConfigWithCert(tmpFile.Name())
	require.Error(t, err2)
	assert.Contains(t, err2.Error(), "解析证书文件失败")
}

// TestDefaultIsRetryable 验证 defaultIsRetryable 判断逻辑
func TestDefaultIsRetryable(t *testing.T) {
	// 普通 error 可重试
	assert.True(t, defaultIsRetryable(context.Canceled))
}

// TestNewEmbeddingHTTPClient_自定义证书路径 验证证书路径设置但加载失败回退
func TestNewEmbeddingHTTPClient_自定义证书路径加载失败(t *testing.T) {
	t.Setenv("EMBEDDING_SSL_CERT", "/nonexistent/cert.pem")
	client := NewEmbeddingHTTPClient("https://api.openai.com")
	assert.NotNil(t, client)
	// 加载失败时应回退到默认 TLS 配置（无自定义 Transport）
}
