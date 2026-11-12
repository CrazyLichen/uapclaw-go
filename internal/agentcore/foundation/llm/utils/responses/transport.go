package responses

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/utils/providererror"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent responses 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// Transport Responses API 纯 HTTP 传输层（SSE 逐行）。
// 只负责 HTTP I/O：发送请求与迭代 SSE 事件；请求体构建与响应解析在 utils.go。
// 已知偏差（spec §7）：Python httpx → Go net/http；SSE 用 bufio 按行读、空行结算；
// httpx 的 is_closed 重建语义由 http.Client 连接池等价承担。
// Python: responses_transport.py:32 OpenAIAccountResponsesTransport
type Transport struct {
	baseURL        string
	timeoutSeconds float64
	verify         any // bool 或证书路径（Python Union[bool, str, Any]）
	proxy          string
	httpClient     *http.Client // 懒建（对齐 _get_client）
}

// TransportOptions 构造选项。
// BaseURL 默认 defaultOpenAIAccountBaseURL（23.6 前置件）；
// TimeoutSeconds 默认 60；Verify 默认 true；MaxRetries 语义在 Go 为不启用传输层重试。
// Python: __init__（responses_transport.py:40-56）
type TransportOptions struct {
	// BaseURL 服务根（自动去掉尾部 "/"）
	BaseURL string
	// TimeoutSeconds 请求超时（秒）
	TimeoutSeconds float64
	// Verify true=系统校验 / false=跳过校验 / 字符串=CA 证书路径
	Verify any
	// Proxy 全局代理 URL（空则不设代理）
	Proxy string
}

// StreamRequest 单次流式请求参数。
// Python: stream_response/create_response 关键字参数
type StreamRequest struct {
	// Body 请求体（transport 内部置 stream=true 的浅拷贝）
	Body map[string]any
	// AccessToken Bearer 凭据（api_key 或 OAuth token）
	AccessToken string
	// ModelName 模型名（回调与解析用；空则取 body["model"]）
	ModelName string
	// SessionID 会话 ID（session_id / x-client-request-id 双头）
	SessionID string
	// ExtraHeaders 额外请求头
	ExtraHeaders map[string]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTransport 创建 Transport。
func NewTransport(opts TransportOptions) *Transport {
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenAIAccountBaseURL
	}
	timeout := opts.TimeoutSeconds
	if timeout <= 0 {
		timeout = 60.0
	}
	verify := opts.Verify
	if verify == nil {
		verify = true
	}
	return &Transport{
		baseURL:        strings.TrimRight(baseURL, "/"),
		timeoutSeconds: timeout,
		verify:         verify,
		proxy:          opts.Proxy,
	}
}

// CreateResponse 发送请求并把流式 chunk 累积为单条 AssistantMessage。
// Python: responses_transport.py:58 create_response
func (t *Transport) CreateResponse(ctx context.Context, req StreamRequest) (*llmschema.AssistantMessage, error) {
	var finalChunk *llmschema.AssistantMessageChunk
	ch, err := t.StreamResponse(ctx, req)
	if err != nil {
		return nil, err
	}
	for chunk := range ch {
		if chunk == nil {
			continue
		}
		if chunk.StreamError != nil {
			return nil, chunk.StreamError
		}
		if finalChunk == nil {
			finalChunk = chunk
		} else {
			finalChunk = finalChunk.Merge(chunk)
		}
	}
	return MessageFromStreamChunk(finalChunk), nil
}

// StreamResponse 流式请求，chunk 经 channel 交付。
// 建立连接/HTTP 状态错误经 error 返回；流内错误以 StreamError chunk 交付后关闭
// （对齐既有 openai 流通道语义）；事件解析错误（response.failed/error）同样经
// StreamError chunk 交付（Python 在生成器内 raise 的等价物）。
// Python: responses_transport.py:79 stream_response
func (t *Transport) StreamResponse(ctx context.Context, req StreamRequest) (<-chan *llmschema.AssistantMessageChunk, error) {
	// body 浅拷贝并置 stream=true（Python :89-90）
	streamBody := make(map[string]any, len(req.Body)+1)
	for k, v := range req.Body {
		streamBody[k] = v
	}
	streamBody["stream"] = true

	effectiveModel := req.ModelName
	if effectiveModel == "" {
		effectiveModel, _ = req.Body["model"].(string)
	}

	headers, err := BuildHeaders(req.AccessToken, req.SessionID, req.ExtraHeaders)
	if err != nil {
		return nil, err
	}

	client, err := t.getHTTPClient()
	if err != nil {
		return nil, &ResponsesError{Message: fmt.Sprintf("build http client failed: %v", err)}
	}

	payload, err := json.Marshal(streamBody)
	if err != nil {
		return nil, &ResponsesError{Message: fmt.Sprintf("encode request body failed: %v", err)}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.responsesURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, &ResponsesError{Message: fmt.Sprintf("build request failed: %v", err)}
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &ResponsesError{Message: fmt.Sprintf("send request failed: %v", err)}
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return nil, raiseForHTTPError(resp.StatusCode, body)
	}

	// trigger(LLM_RESPONSE_STARTED)：响应头到达即触发，retry_count 未知恒 nil
	// Python: responses_transport.py:108-112
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:      callback.LLMResponseStarted,
		ModelName:  effectiveModel,
		RetryCount: nil,
	})

	ch := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()
		parser := NewStreamParser(effectiveModel)
		var buffer []string
		scanner := bufio.NewScanner(resp.Body)
		// SSE 行可能很长（大 arguments delta），放宽行上限
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		emitEvent := func(lines []string) bool {
			event, err := ParseSSEBlock(lines)
			if err != nil {
				ch <- llmschema.NewAssistantMessageChunk("", llmschema.WithChunkStreamError(err))
				return false
			}
			if event == nil {
				return true
			}
			chunk, err := parser.Parse(event)
			if err != nil {
				ch <- llmschema.NewAssistantMessageChunk("", llmschema.WithChunkStreamError(err))
				return false
			}
			if chunk != nil {
				ch <- chunk
			}
			return true
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if !emitEvent(buffer) {
					return
				}
				buffer = nil
			} else {
				buffer = append(buffer, line)
			}
		}
		if scanner.Err() != nil {
			ch <- llmschema.NewAssistantMessageChunk("",
				llmschema.WithChunkStreamError(&ResponsesError{Message: fmt.Sprintf("stream read failed: %v", scanner.Err())}))
			return
		}
		if len(buffer) > 0 {
			emitEvent(buffer) // 收尾块
		}
	}()
	return ch, nil
}

// Close 关闭空闲连接。
// Python: responses_transport.py:130 aclose
func (t *Transport) Close() error {
	if t.httpClient != nil {
		t.httpClient.CloseIdleConnections()
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// responsesURL 拼接 /responses 端点。
// Python: responses_transport.py:136 _responses_url
func (t *Transport) responsesURL() string {
	return t.baseURL + "/responses"
}

// getHTTPClient 懒建 http.Client（连接复用；等价 Python _get_client/_make_client）。
func (t *Transport) getHTTPClient() (*http.Client, error) {
	if t.httpClient != nil {
		return t.httpClient, nil
	}
	transport := &http.Transport{
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
		// Python max_retries=0：Go 默认即无传输层重试
	}
	tlsConfig := &tls.Config{}
	switch v := t.verify.(type) {
	case bool:
		tlsConfig.InsecureSkipVerify = !v
	case string:
		if v != "" {
			pem, err := os.ReadFile(v)
			if err != nil {
				return nil, err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("no valid certs in %s", v)
			}
			tlsConfig.RootCAs = pool
		}
	}
	transport.TLSClientConfig = tlsConfig
	if t.proxy != "" {
		proxyURL, err := url.Parse(t.proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	t.httpClient = &http.Client{
		Transport: transport,
		// Python: httpx.Timeout(max(5.0, timeout_seconds))
		Timeout: time.Duration(maxSeconds(t.timeoutSeconds)) * time.Second,
	}
	return t.httpClient, nil
}

// raiseForHTTPError 非成功状态转 ResponsesError。
// 文案链：error.message → detail → SummarizeProviderErrorText → 固定文案。
// Python: responses_utils.py:388 raise_for_http_error + _http_error_message
func raiseForHTTPError(statusCode int, body []byte) *ResponsesError {
	message := ""
	if msg, ok := httpErrorMessage(body); ok {
		message = msg
	}
	if message == "" {
		text := strings.TrimSpace(string(body))
		if text != "" {
			message = providererror.SummarizeProviderErrorText(text, statusCode)
		}
	}
	if message == "" {
		message = fmt.Sprintf("OpenAI account Responses request failed with status %d.", statusCode)
	}
	return &ResponsesError{Message: message, StatusCode: statusCode}
}

// maxSeconds 超时下限 5 秒。
// Python: max(5.0, float(timeout_seconds))
func maxSeconds(timeout float64) float64 {
	if timeout < 5.0 {
		return 5.0
	}
	return timeout
}
