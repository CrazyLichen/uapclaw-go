package support

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// CompressionErrorKind 压缩模型调用失败的分类枚举
// Python: compression_executor.py:10 (CompressionErrorKind)
type CompressionErrorKind string

const (
	// CompressionErrorKindContextOverflow 上下文溢出
	CompressionErrorKindContextOverflow CompressionErrorKind = "context_overflow"
	// CompressionErrorKindRateLimit 限流
	CompressionErrorKindRateLimit CompressionErrorKind = "rate_limit"
	// CompressionErrorKindAuthentication 认证失败
	CompressionErrorKindAuthentication CompressionErrorKind = "authentication"
	// CompressionErrorKindTimeout 超时
	CompressionErrorKindTimeout CompressionErrorKind = "timeout"
	// CompressionErrorKindServerUnstable 服务端不稳定
	CompressionErrorKindServerUnstable CompressionErrorKind = "server_unstable"
	// CompressionErrorKindAPIRequestError API 请求错误
	CompressionErrorKindAPIRequestError CompressionErrorKind = "api_request_error"
	// CompressionErrorKindUnknown 未知错误
	CompressionErrorKindUnknown CompressionErrorKind = "unknown"
)

// ──────────────────────────── 常量 ────────────────────────────

// compressionOverflowMarkers 上下文溢出错误标记
// Python: compression_executor.py:44 (_CONTEXT_OVERFLOW_MARKERS)
var compressionOverflowMarkers = []string{
	"context length",
	"context_length",
	"maximum context",
	"max context",
	"token limit",
	"too many tokens",
	"prompt is too long",
	"input is too long",
	"request too large",
	"413",
}

// compressionRateLimitMarkers 限流错误标记
// Python: compression_executor.py:57 (_RATE_LIMIT_MARKERS)
var compressionRateLimitMarkers = []string{"rate_limit", "rate limit", "too many requests", "429"}

// compressionAuthMarkers 认证错误标记
// Python: compression_executor.py:58 (_AUTHENTICATION_MARKERS)
var compressionAuthMarkers = []string{"authentication", "unauthorized", "invalid api key", "api key invalid", "401", "403"}

// compressionTimeoutMarkers 超时错误标记
// Python: compression_executor.py:59 (_TIMEOUT_MARKERS)
var compressionTimeoutMarkers = []string{"timeout", "timed out", "read timeout", "connect timeout"}

// compressionServerUnstableMarkers 服务端不稳定错误标记
// Python: compression_executor.py:60 (_SERVER_UNSTABLE_MARKERS)
var compressionServerUnstableMarkers = []string{
	"internal server error",
	"service unavailable",
	"bad gateway",
	"gateway timeout",
	"overloaded",
	"temporarily unavailable",
	"500",
	"502",
	"503",
	"504",
}

// compressionAPIRequestMarkers 请求参数错误标记
// Python: compression_executor.py:66 (_API_REQUEST_MARKERS)
var compressionAPIRequestMarkers = []string{"bad request", "invalid request", "400"}

// ──────────────────────────── 结构体 ────────────────────────────

// CompressionError 压缩模型调用失败的结构化错误
// Python: compression_executor.py:14 (CompressionError)
type CompressionError struct {
	// Kind 错误分类
	Kind CompressionErrorKind
	// Message 错误描述
	Message string
	// OriginalError 原始错误
	OriginalError error
}

// Error 实现 error 接口
func (e *CompressionError) Error() string { return e.Message }

// Unwrap 返回原始错误
func (e *CompressionError) Unwrap() error { return e.OriginalError }

// IsContextOverflow 是否为上下文溢出
// Python: compression_executor.py:29 (is_context_overflow)
func (e *CompressionError) IsContextOverflow() bool {
	return e.Kind == CompressionErrorKindContextOverflow
}

// CompressionRequest 压缩请求载体
// Python: compression_executor.py:68 (CompressionRequest)
type CompressionRequest struct {
	// Prompt 压缩提示词
	Prompt string
	// ContextMessages 上下文消息
	ContextMessages []llm_schema.BaseMessage
	// SystemMessages 系统消息
	SystemMessages []llm_schema.BaseMessage
	// Tools 工具定义（可为空）
	Tools []commonschema.ToolInfoInterface
	// ExcludeRecentMessages 从尾部排除的消息条数（保护尾巴）
	ExcludeRecentMessages int
	// OutputParser 输出解析器（可选）
	OutputParser model_clients.BaseOutputParser
}

// NewCompressionRequestFromContextWindow 从上下文窗口构造压缩请求
// Python: compression_executor.py:82 (CompressionRequest.from_context_window)
func NewCompressionRequestFromContextWindow(
	prompt string,
	contextWindow iface.ContextWindow,
	excludeRecentMessages int,
	outputParser model_clients.BaseOutputParser,
) *CompressionRequest {
	return &CompressionRequest{
		Prompt:                prompt,
		SystemMessages:        append([]llm_schema.BaseMessage(nil), contextWindow.SystemMessages...),
		ContextMessages:       append([]llm_schema.BaseMessage(nil), contextWindow.ContextMessages...),
		Tools:                 append([]commonschema.ToolInfoInterface(nil), contextWindow.Tools...),
		ExcludeRecentMessages: excludeRecentMessages,
		OutputParser:          outputParser,
	}
}

// CompressionResult 归一化的压缩响应，保留原始模型响应访问
// Python: compression_executor.py:101 (CompressionResult)
type CompressionResult struct {
	// Response 原始 AssistantMessage
	Response *llm_schema.AssistantMessage
	// Usage 用量元数据（可为 nil）
	Usage any
	// Err 错误（Python 侧 dataclass 字段，Go 保留字段形态）
	Err error
}

// Content 取响应文本
// Python: compression_executor.py:107 (CompressionResult.content)
func (r *CompressionResult) Content() string {
	if r.Response == nil {
		return ""
	}
	return r.Response.Content.String()
}

// CompressionExecutor 压缩模型调用包装器（复用主 agent 前缀上下文）
// Python: compression_executor.py:114 (CompressionExecutor)
type CompressionExecutor struct {
	model        *llm.Model
	lastResponse *llm_schema.AssistantMessage
}

// NewCompressionExecutor 构造压缩执行器
// Python: compression_executor.py:118 (__init__)
func NewCompressionExecutor(model *llm.Model) *CompressionExecutor {
	return &CompressionExecutor{model: model}
}

// LastResponse 最近一次响应
// Python: compression_executor.py:121 (last_response)
func (e *CompressionExecutor) LastResponse() *llm_schema.AssistantMessage { return e.lastResponse }

// Invoke 执行压缩调用；模型调用失败时经 classify_compression_error 包装
// Python: compression_executor.py:127 (invoke)
func (e *CompressionExecutor) Invoke(
	ctx context.Context,
	request *CompressionRequest,
	invokeKwargs map[string]any,
) (*CompressionResult, error) {
	messages := model_clients.NewMessagesParam(BuildMessages(request)...)
	opts := []model_clients.InvokeOption{}
	if len(request.Tools) > 0 {
		opts = append(opts, model_clients.WithTools(request.Tools...))
	}
	if request.OutputParser != nil {
		opts = append(opts, model_clients.WithInvokeOutputParser(request.OutputParser))
	}
	if len(invokeKwargs) > 0 {
		// Python: **model_invoke_kwargs 透传（kv cache affinity 等）
		opts = append(opts, model_clients.WithInvokeExtra(invokeKwargs))
	}
	response, err := e.model.Invoke(ctx, messages, opts...)
	if err != nil {
		return nil, classifyCompressionError(err)
	}
	e.lastResponse = response
	var usage any
	if response.UsageMetadata != nil {
		usage = response.UsageMetadata
	}
	return &CompressionResult{Response: response, Usage: usage}, nil
}

// BuildContextMessages 构建种子上下文（供 forked-agent 执行体使用）：
// build_messages 会在尾部追加压缩提示词，适用于直连模型调用；
// ReAct agent 经 agent.invoke 收到提示词，因此执行体只需系统/上下文前缀，不重复提示词。
// Python: compression_executor.py:151 (build_context_messages)
func BuildContextMessages(request *CompressionRequest) []llm_schema.BaseMessage {
	contextMessages := append([]llm_schema.BaseMessage(nil), request.ContextMessages...)
	if request.ExcludeRecentMessages > 0 {
		keepCount := len(contextMessages) - request.ExcludeRecentMessages
		if keepCount < 0 {
			keepCount = 0
		}
		contextMessages = contextMessages[:keepCount]
	}
	result := make([]llm_schema.BaseMessage, 0, len(request.SystemMessages)+len(contextMessages))
	result = append(result, request.SystemMessages...)
	result = append(result, contextMessages...)
	return result
}

// BuildMessages 构建直连模型调用的完整消息：种子上下文 + 压缩提示词用户消息
// Python: compression_executor.py:169 (build_messages)
func BuildMessages(request *CompressionRequest) []llm_schema.BaseMessage {
	result := BuildContextMessages(request)
	result = append(result, llm_schema.NewUserMessage(request.Prompt))
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// classifyCompressionError 把任意模型调用错误包装为 CompressionError
// Python: compression_executor.py:176 (classify_compression_error)
func classifyCompressionError(err error) *CompressionError {
	message := errorText(err)
	return &CompressionError{
		Kind:          classifyCompressionErrorKind(err, message),
		Message:       firstNonEmpty(message, errType(err)),
		OriginalError: err,
	}
}

// classifyCompressionErrorKind 错误文本 + 状态码 → 分类
// Python: compression_executor.py:184 (_classify_compression_error_kind)
func classifyCompressionErrorKind(err error, message string) CompressionErrorKind {
	normalized := strings.ToLower(message)
	statusCode, hasCode := extractStatusCode(err)
	if !hasCode {
		statusCode = 0
	}
	if containsAny(normalized, compressionOverflowMarkers) {
		return CompressionErrorKindContextOverflow
	}
	if statusCode == 413 {
		return CompressionErrorKindContextOverflow
	}
	if containsAny(normalized, compressionRateLimitMarkers) || statusCode == 429 {
		return CompressionErrorKindRateLimit
	}
	if containsAny(normalized, compressionAuthMarkers) || statusCode == 401 || statusCode == 403 {
		return CompressionErrorKindAuthentication
	}
	if containsAny(normalized, compressionTimeoutMarkers) {
		return CompressionErrorKindTimeout
	}
	if containsAny(normalized, compressionServerUnstableMarkers) ||
		statusCode == 500 || statusCode == 502 || statusCode == 503 || statusCode == 504 {
		return CompressionErrorKindServerUnstable
	}
	if containsAny(normalized, compressionAPIRequestMarkers) || statusCode == 400 {
		return CompressionErrorKindAPIRequestError
	}
	return CompressionErrorKindUnknown
}

// containsAny 文本是否包含任一标记
// Python: compression_executor.py:217 (_contains_any)
func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// errorText 拼接错误链上各层文本与状态信息
// Python: compression_executor.py:222 (_error_text)
func errorText(err error) string {
	var parts []string
	current := err
	seen := map[error]bool{}
	for current != nil && !seen[current] {
		seen[current] = true
		parts = append(parts, current.Error())
		status := errorsStatus(current)
		if status != nil {
			parts = append(parts, fmt.Sprint(status))
		}
		for _, attr := range []string{"code", "status_code", "request_id", "type"} {
			if value, ok := errorAttr(current, attr); ok {
				parts = append(parts, fmt.Sprint(value))
			}
		}
		if c, ok := current.(interface{ Unwrap() error }); ok {
			current = c.Unwrap()
		} else {
			current = nil
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// errType 取错误的类型名（Python error.__class__.__name__ 等价）
func errType(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}

// errorsStatus 若错误实现 Status() 接口则取状态值
// Python: _error_text 中 getattr(current, "status") 段
func errorsStatus(err error) any {
	if s, ok := err.(interface{ Status() any }); ok {
		return s.Status()
	}
	return nil
}

// errorAttr 若错误实现 AttrXxx 方法之一则取值（Go 侧以窄接口近似 Python getattr）
// Python: _error_text 中 code/status_code/request_id/type 段
func errorAttr(err error, attr string) (any, bool) {
	switch attr {
	case "code":
		if v, ok := err.(interface{ Code() any }); ok {
			return v.Code(), true
		}
	case "status_code":
		if v, ok := err.(interface{ StatusCode() any }); ok {
			return v.StatusCode(), true
		}
	case "request_id":
		if v, ok := err.(interface{ RequestID() any }); ok {
			return v.RequestID(), true
		}
	case "type":
		if v, ok := err.(interface{ ErrorType() any }); ok {
			return v.ErrorType(), true
		}
	}
	return nil, false
}

// extractStatusCode 沿错误链提取 HTTP 状态码
// Python: compression_executor.py:254 (_extract_status_code)
// 返回值：(状态码, 是否取到)。Python 返回 Optional[int]，Go 侧以 (int,bool) 等价。
func extractStatusCode(err error) (int, bool) {
	current := err
	seen := map[error]bool{}
	for current != nil && !seen[current] {
		seen[current] = true
		for _, attr := range []string{"status_code", "status"} {
			if getter, ok := attrGetter(current, attr); ok {
				if code, ok := coerceStatusCode(getter()); ok {
					return code, true
				}
			}
		}
		if c, ok := current.(interface{ Unwrap() error }); ok {
			current = c.Unwrap()
		} else {
			current = nil
		}
	}
	return 0, false
}

// attrGetter 以窄接口近似 Python getattr(current, attr)
func attrGetter(err error, attr string) (func() any, bool) {
	switch attr {
	case "status_code":
		if v, ok := err.(interface{ StatusCode() int }); ok {
			return func() any { return v.StatusCode() }, true
		}
		if v, ok := err.(interface{ StatusCode() any }); ok {
			return func() any { return v.StatusCode() }, true
		}
	case "status":
		if v, ok := err.(interface{ Status() int }); ok {
			return func() any { return v.Status() }, true
		}
		if v, ok := err.(interface{ Status() any }); ok {
			return func() any { return v.Status() }, true
		}
	}
	return nil, false
}

// coerceStatusCode 把状态值收敛为 int
// Python: compression_executor.py:284 (_coerce_status_code)
func coerceStatusCode(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n, true
		}
	}
	return 0, false
}

// firstNonEmpty 返回第一个非空字符串
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
