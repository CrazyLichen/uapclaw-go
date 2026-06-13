package embedding

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EmbeddingConfig 嵌入模型配置。
//
// 对应 Python: EmbeddingConfig
type EmbeddingConfig struct {
	// ModelName 模型名称
	ModelName string
	// BaseURL API 地址
	BaseURL string
	// APIKey API 密钥
	APIKey string
}

// MultimodalOption 多模态嵌入的可选参数。
type MultimodalOption struct {
	// Instruction 多模态嵌入指令（VLLM 使用）
	Instruction string
}

// MultimodalEmbedder 多模态嵌入接口，支持文本+图片+音频+视频。
type MultimodalEmbedder interface {
	embedding.BaseEmbedding
	// EmbedMultimodal 将多模态文档转换为向量。
	EmbedMultimodal(ctx context.Context, doc *common.MultimodalDocument, opts ...MultimodalOption) ([]float64, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// envEmbeddingSSLVerify SSL 验证开关环境变量
	envEmbeddingSSLVerify = "EMBEDDING_SSL_VERIFY"
	// envEmbeddingSSLCert SSL 证书路径环境变量
	envEmbeddingSSLCert = "EMBEDDING_SSL_CERT"

	// defaultTimeout 默认请求超时
	defaultTimeout = 60 * time.Second
	// defaultMaxRetries 默认最大重试次数
	defaultMaxRetries = 3
	// defaultMaxBatchSize 默认每批最大文档数
	defaultMaxBatchSize = 8
	// defaultMaxConcurrent 默认最大并发数
	defaultMaxConcurrent = 50

	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateEmbedDocs 校验输入文本列表，返回非空文档。
//
// 对齐 Python: APIEmbedding.validate_embed_docs
func ValidateEmbedDocs(texts []string) ([]string, error) {
	if len(texts) == 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "文本列表为空"),
		)
	}

	var nonEmpty []string
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}

	emptyCount := len(texts) - len(nonEmpty)
	if emptyCount > 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("%d 个文本为空", emptyCount)),
		)
	}

	if len(nonEmpty) == 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "所有文本为空"),
		)
	}

	return nonEmpty, nil
}

// BatchTexts 按 batchSize 将文本列表分片。
func BatchTexts(texts []string, batchSize int) [][]string {
	if batchSize <= 0 {
		batchSize = 1
	}

	var batches [][]string
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batches = append(batches, texts[i:end])
	}
	return batches
}

// EmbeddingTask 嵌入任务函数类型。
type EmbeddingTask func() ([][]float64, error)

// ExecuteWithConcurrency 通用并发执行框架。
//
// limiter 为并发信号量（buffered channel），nil 表示不限制并发。
func ExecuteWithConcurrency(
	ctx context.Context,
	tasks []EmbeddingTask,
	limiter chan struct{},
) ([][]float64, error) {
	type taskResult struct {
		index int
		data  [][]float64
		err   error
	}

	resultCh := make(chan taskResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, fn EmbeddingTask) {
			defer wg.Done()
			if limiter != nil {
				select {
				case limiter <- struct{}{}:
					defer func() { <-limiter }()
				case <-ctx.Done():
					resultCh <- taskResult{index: idx, err: ctx.Err()}
					return
				}
			}
			data, err := fn()
			resultCh <- taskResult{index: idx, data: data, err: err}
		}(i, task)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	results := make([][][]float64, len(tasks))
	for res := range resultCh {
		if res.err != nil {
			return nil, res.err
		}
		results[res.index] = res.data
	}

	// 展平结果
	var all [][]float64
	for _, batch := range results {
		all = append(all, batch...)
	}
	return all, nil
}

// ParseEmbeddingResponse 通用 HTTP 响应解析。
//
// 支持三种格式：
//   - {"embedding": [...]} / {"embedding": [[...], [...]]}
//   - {"embeddings": [[...], [...]]}
//   - {"data": [{"embedding": [...]}, ...]}
func ParseEmbeddingResponse(body []byte) ([][]float64, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("解析响应 JSON 失败: %s", err)),
		)
	}

	// 格式 1: {"embedding": [...]}
	if embRaw, ok := raw["embedding"]; ok {
		// 尝试解析为 [][]float64
		var nested [][]float64
		if err := json.Unmarshal(embRaw, &nested); err == nil {
			return nested, nil
		}
		// 尝试解析为 []float64（单条）
		var flat []float64
		if err := json.Unmarshal(embRaw, &flat); err == nil {
			return [][]float64{flat}, nil
		}
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", "embedding 字段格式无效"),
		)
	}

	// 格式 2: {"embeddings": [[...], [...]]}
	if embsRaw, ok := raw["embeddings"]; ok {
		var embeddings [][]float64
		if err := json.Unmarshal(embsRaw, &embeddings); err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("embeddings 字段格式无效: %s", err)),
			)
		}
		return embeddings, nil
	}

	// 格式 3: {"data": [{"embedding": [...]}, ...]}
	if dataRaw, ok := raw["data"]; ok {
		var dataItems []struct {
			Embedding json.RawMessage `json:"embedding"`
			Index     int             `json:"index"`
		}
		if err := json.Unmarshal(dataRaw, &dataItems); err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("data 字段格式无效: %s", err)),
			)
		}

		// 按 index 排序
		for i := 1; i < len(dataItems); i++ {
			for j := i; j > 0 && dataItems[j].Index < dataItems[j-1].Index; j-- {
				dataItems[j], dataItems[j-1] = dataItems[j-1], dataItems[j]
			}
		}

		var embeddings [][]float64
		for _, item := range dataItems {
			// 尝试解析为 float64 数组
			var flat []float64
			if err := json.Unmarshal(item.Embedding, &flat); err == nil {
				embeddings = append(embeddings, flat)
				continue
			}
			// 尝试解析为 base64 字符串
			var b64Str string
			if err := json.Unmarshal(item.Embedding, &b64Str); err == nil {
				vec, err := ParseBase64Embedding(b64Str)
				if err != nil {
					return nil, exception.BuildError(
						exception.StatusRetrievalEmbeddingResponseInvalid,
						exception.WithParam("error_msg", fmt.Sprintf("base64 解码失败: %s", err)),
					)
				}
				embeddings = append(embeddings, vec)
				continue
			}
		}

		if len(embeddings) == 0 {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", "data 项中无有效 embedding 字段"),
			)
		}

		return embeddings, nil
	}

	return nil, exception.BuildError(
		exception.StatusRetrievalEmbeddingResponseInvalid,
		exception.WithParam("error_msg", "响应中无 embedding/embeddings/data 字段"),
	)
}

// RetryWithBackoff 通用重试 + 指数退避。
//
// fn 参数 attempt 从 0 开始。maxRetries 为最大重试次数（即最多调用 fn maxRetries 次）。
// 如果 fn 返回的 error 是 *exception.BaseError 且 IsRecoverable() == false，则立即退出不重试。
// 对齐 Python 行为：只重试可恢复错误（网络错误/5xx），不重试客户端错误（4xx/输入错误）。
func RetryWithBackoff(
	ctx context.Context,
	maxRetries int,
	fn func(attempt int) ([][]float64, error),
) ([][]float64, error) {
	return retryWithBackoffGeneric(ctx, maxRetries, fn, defaultIsRetryable)
}

// defaultIsRetryable 默认的可重试判断逻辑。
// *exception.BaseError 且 IsRecoverable()==false 的错误不重试，其他都重试。
func defaultIsRetryable(err error) bool {
	if baseErr, ok := err.(*exception.BaseError); ok && !baseErr.IsRecoverable() {
		return false
	}
	return true
}

// retryWithBackoffGeneric 通用重试 + 指数退避（带自定义可重试判断）。
func retryWithBackoffGeneric(
	ctx context.Context,
	maxRetries int,
	fn func(attempt int) ([][]float64, error),
	isRetryable func(error) bool,
) ([][]float64, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, err := fn(attempt)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// 检查是否可重试
		if !isRetryable(err) {
			return nil, err
		}

		if attempt < maxRetries-1 {
			logger.Warn(logComponent).
				Str("event_type", "embedding_retry").
				Int("attempt", attempt+1).
				Int("max_retries", maxRetries).
				Err(err).
				Msg("嵌入请求失败，准备重试")

			// 指数退避：100ms, 200ms, 400ms...
			backoff := time.Duration(math.Pow(2, float64(attempt))) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return nil, exception.BuildError(
		exception.StatusRetrievalEmbeddingRequestCallFailed,
		exception.WithParam("error_msg", fmt.Sprintf("重试 %d 次后仍失败: %s", maxRetries, lastErr)),
		exception.WithCause(lastErr),
	)
}

// NewEmbeddingHTTPClient 创建嵌入客户端的 HTTP Client。
//
// 根据 EMBEDDING_SSL_VERIFY / EMBEDDING_SSL_CERT 环境变量配置 TLS。
func NewEmbeddingHTTPClient(apiURL string) *http.Client {
	isHTTPS := strings.HasPrefix(apiURL, "https://")

	if !isHTTPS {
		return &http.Client{Timeout: defaultTimeout}
	}

	// 检查是否跳过验证
	verifySwitch := strings.ToLower(strings.TrimSpace(os.Getenv(envEmbeddingSSLVerify)))
	if verifySwitch == "false" {
		return &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}

	// 检查自定义证书
	certPath := os.Getenv(envEmbeddingSSLCert)
	if certPath != "" {
		tlsCfg, err := createTLSConfigWithCert(certPath)
		if err != nil {
			logger.Warn(logComponent).
				Str("cert_path", certPath).
				Err(err).
				Msg("加载 SSL 证书失败，使用默认 TLS 配置")
			return &http.Client{Timeout: defaultTimeout}
		}
		return &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
		}
	}

	return &http.Client{Timeout: defaultTimeout}
}

// ResolveBatchSize 解析批大小，尊重调用方传入值但不超过 maxBatchSize。
func ResolveBatchSize(callerBatchSize, maxBatchSize int) int {
	bsz := callerBatchSize
	if bsz <= 0 {
		bsz = maxBatchSize
	}
	if maxBatchSize > 0 && bsz > maxBatchSize {
		bsz = maxBatchSize
	}
	if bsz <= 0 {
		bsz = 1
	}
	return bsz
}

// ApplyEmbedOptions 解析 EmbedOption 列表，返回批大小和回调。
func ApplyEmbedOptions(opts []embedding.EmbedOption, defaultBatchSize int) (int, embedding.Callback) {
	batchSize := defaultBatchSize
	var cb embedding.Callback
	for _, opt := range opts {
		if opt.BatchSize > 0 {
			batchSize = opt.BatchSize
		}
		if opt.Callback != nil {
			cb = opt.Callback
		}
	}
	return batchSize, cb
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createTLSConfigWithCert 使用自定义证书创建 TLS 配置。
func createTLSConfigWithCert(certPath string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	caPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("读取证书文件失败: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("解析证书文件失败")
	}
	cfg.RootCAs = certPool

	return cfg, nil
}
