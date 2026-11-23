package http

import (
	components "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HttpAuthConfig 认证配置（Python HttpAuthConfig :54-68 一比一）
type HttpAuthConfig struct {
	// Type 认证类型（默认 HttpAuthTypeNone）
	Type HttpAuthType
	// Username / Password Basic/Digest 认证
	Username *string
	Password *string
	// Token Bearer 令牌
	Token *string
	// APIKey API Key
	APIKey *string
	// InLocation API Key 位置：header|query|body（默认 header）
	InLocation string
	// Name API Key 头名（默认 "Authorization"）
	Name string
	// AccessKey / SecretKey / Region AWS 签名（Python 未实现，字段照搬）
	AccessKey *string
	SecretKey *string
	Region    *string
}

// HttpRequestBodyConfig 请求体配置（Python :71-77 一比一）
type HttpRequestBodyConfig struct {
	// ContentType 内容类型（默认 json）
	ContentType HttpContentType
	// JSONData dict|string|nil 三态（Python Union）
	JSONData any
	// FormData / MultipartForm / BinaryData / TextData 见 Python 字段注释
	FormData      map[string]any
	MultipartForm map[string]any
	// BinaryData base64 编码引用
	BinaryData *string
	// TextData 文本体
	TextData *string
}

// HttpResponseHandlingConfig 响应处理配置（Python :80-87 一比一）
type HttpResponseHandlingConfig struct {
	// ResponseFormat 响应格式（默认 autodetect）
	ResponseFormat HttpResponseFormat
	// ResponseCodeSuccessCodes 成功码（默认 [200,201,202,204]）
	ResponseCodeSuccessCodes []int
	// ResponseCodeFailureCodes 失败码（默认空）
	ResponseCodeFailureCodes []int
	// ResponseMode full|on-success|on-error（默认 full）
	ResponseMode string
	// ResponseDataProperty 顶层属性抽取（Python Optional）
	ResponseDataProperty *string
	// MaxRedirects 死字段：Python 定义未消费（spec §4）
	MaxRedirects int
	// ThrowOnHTTPError 死字段：Python 定义未消费
	ThrowOnHTTPError bool
}

// HttpAdvancedOptionsConfig 高级选项（Python :90-99 一比一；仅 IgnoreSSLIssues
// 与 Proxy 被消费，其余死字段照搬，spec §4）
type HttpAdvancedOptionsConfig struct {
	// FollowRedirect 死字段（Python 默认 True 但请求恒禁跟随）
	FollowRedirect bool
	// IgnoreSSLIssues 忽略 SSL 校验（消费项）
	IgnoreSSLIssues bool
	// Proxy 代理地址（消费项）
	Proxy string
	// Timeout 死字段（毫秒，默认 10000；消费的是顶层 timeout 秒）
	Timeout int
	// DisableCompression 死字段
	DisableCompression bool
	// DisableFollowTrackRedirect 死字段
	DisableFollowTrackRedirect bool
	// MaxBodyLength 死字段（默认 1MB）
	MaxBodyLength int
	// UseStream 死字段
	UseStream bool
	// ProxyHeader 死字段
	ProxyHeader map[string]string
}

// HttpRetryConfig 重试配置（Python :102-107 一比一）
type HttpRetryConfig struct {
	// Enabled 重试开关
	Enabled bool
	// MaxRetries 最大重试次数
	MaxRetries int
	// RetryOnStatusCodes 触发重试的状态码
	RetryOnStatusCodes []int
	// RetryDelay 基础延迟（毫秒）
	RetryDelay int
	// BackoffType fixed|linear|exponential
	BackoffType string
}

// HttpRateLimitConfig 限流配置（Python :110-113 一比一；零消费死配置，spec §4）
type HttpRateLimitConfig struct {
	// Enabled 限流开关
	Enabled bool
	// RequestsPerUnit 单位请求数
	RequestsPerUnit int
	// Unit second|minute|hour
	Unit string
}

// HttpRequestParamConfig 请求参数配置（Python :116-128 一比一）
type HttpRequestParamConfig struct {
	// URL 请求地址（必填，Python Field(...)）
	URL string
	// Method HTTP 方法（默认 GET）
	Method string
	// Headers dict|string 双形态（Python Union）
	Headers any
	// QueryParameters 查询参数
	QueryParameters map[string]any
	// Body 请求体配置
	Body *HttpRequestBodyConfig
	// Authentication 认证配置
	Authentication *HttpAuthConfig
	// ResponseHandling / AdvancedOptions / RetryConfig / RateLimitConfig 子配置
	ResponseHandling *HttpResponseHandlingConfig
	AdvancedOptions  *HttpAdvancedOptionsConfig
	RetryConfig      *HttpRetryConfig
	RateLimitConfig  *HttpRateLimitConfig
	// Timeout 顶层超时（秒，Python 1.0-300.0；消费项）
	Timeout float64
	// MaxResponseByteSize 响应最大字节数（默认 10MB；消费项）
	MaxResponseByteSize int
}

// HTTPRequestExecutable 执行体（Python :136- 一比一）
type HTTPRequestExecutable struct {
	// WorkflowComponent 8.17 组件基座（四槽 + 组合面，槽优先装配）
	components.WorkflowComponent
	// cfg 组件配置（Python self.config）
	cfg *HttpComponentConfig
	// requestParams 快捷引用（Python self.request_params）
	requestParams *HttpRequestParamConfig
}

// HTTPRequestComponent 组件组合面（Python :494- 一比一）
type HTTPRequestComponent struct {
	// BaseComponentComposable 组合面基座（Python ComponentComposable mixin 嵌入）
	components.BaseComponentComposable
	// config 组件配置（Python self.__config 私有属性怪癖 → Go 非导出字段 + Config() 访问器）
	config *HttpComponentConfig
	// executable 懒构造缓存（Python self._executable）
	executable *HTTPRequestExecutable
}

// HttpComponentConfig 组件配置（Python :131-133 一比一；Metadata 照搬 Optional 字段）
type HttpComponentConfig struct {
	// RequestParams 请求参数
	RequestParams *HttpRequestParamConfig
	// Metadata 组件元数据（Python Optional[WorkflowComponentMetadata]）
	Metadata any
}

// ──────────────────────────── 枚举 ────────────────────────────

// HttpAuthType 认证类型（Python str Enum :28-34 六成员全量，规则 9.2.4 不裁剪）
type HttpAuthType string

const (
	// HttpAuthTypeNone 无认证
	HttpAuthTypeNone HttpAuthType = "none"
	// HttpAuthTypeBasic Basic 认证
	HttpAuthTypeBasic HttpAuthType = "basic"
	// HttpAuthTypeBearer Bearer 令牌
	HttpAuthTypeBearer HttpAuthType = "bearer"
	// HttpAuthTypeAPIKey API Key
	HttpAuthTypeAPIKey HttpAuthType = "api_key"
	// HttpAuthTypeDigest Digest 认证（Python 未实现，成员照搬）
	HttpAuthTypeDigest HttpAuthType = "digest"
	// HttpAuthTypeAWS AWS 签名（Python 未实现，成员照搬）
	HttpAuthTypeAWS HttpAuthType = "aws"
)

// HttpContentType 请求体内容类型（Python :37-43 六成员全量）
type HttpContentType string

const (
	// HttpContentTypeJSON JSON
	HttpContentTypeJSON HttpContentType = "json"
	// HttpContentTypeForm 表单
	HttpContentTypeForm HttpContentType = "form"
	// HttpContentTypeMultipartForm 多部分表单
	HttpContentTypeMultipartForm HttpContentType = "multipart_form"
	// HttpContentTypeBinary 二进制
	HttpContentTypeBinary HttpContentType = "binary"
	// HttpContentTypeText 文本
	HttpContentTypeText HttpContentType = "text"
	// HttpContentTypeAuto 自动（Python 定义未消费，成员照搬）
	HttpContentTypeAuto HttpContentType = "auto"
)

// HttpResponseFormat 响应格式（Python :46-51 五成员全量）
type HttpResponseFormat string

const (
	// HttpResponseFormatAutodetect 自动判型
	HttpResponseFormatAutodetect HttpResponseFormat = "autodetect"
	// HttpResponseFormatJSON JSON
	HttpResponseFormatJSON HttpResponseFormat = "json"
	// HttpResponseFormatText 文本
	HttpResponseFormatText HttpResponseFormat = "text"
	// HttpResponseFormatBinary 二进制
	HttpResponseFormatBinary HttpResponseFormat = "binary"
	// HttpResponseFormatBuffer 缓冲（Python 定义未消费，成员照搬）
	HttpResponseFormatBuffer HttpResponseFormat = "buffer"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sslVerifyEnv / sslCertEnv / sslTriggerValue SSL 环境开关
	// （Python SslUtils.get_ssl_config("HTTP_SSL_VERIFY", "HTTP_SSL_CERT", ["false"], …) :273）
	sslVerifyEnv    = "HTTP_SSL_VERIFY"
	sslCertEnv      = "HTTP_SSL_CERT"
	sslTriggerValue = "false"

	// chunkSize 响应分块读大小（Python iter_chunked(8192) :294）
	chunkSize = 8192
)

// ──────────────────────────── 全局变量 ────────────────────────────

// textContentTypes 文本类 content-type 白名单（Python 类常量 TEXT_CONTENT_TYPES :137-138）
var textContentTypes = []string{
	"text/", "application/json", "application/xml",
	"application/javascript", "application/xhtml+xml",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHttpAuthConfig 默认认证配置（pydantic 默认值语义）
func NewHttpAuthConfig() *HttpAuthConfig {
	return &HttpAuthConfig{Type: HttpAuthTypeNone, InLocation: "header", Name: "Authorization"}
}

// NewHttpRequestBodyConfig 默认请求体配置
func NewHttpRequestBodyConfig() *HttpRequestBodyConfig {
	return &HttpRequestBodyConfig{ContentType: HttpContentTypeJSON}
}

// NewHttpResponseHandlingConfig 默认响应处理配置
func NewHttpResponseHandlingConfig() *HttpResponseHandlingConfig {
	return &HttpResponseHandlingConfig{
		ResponseFormat:           HttpResponseFormatAutodetect,
		ResponseCodeSuccessCodes: []int{200, 201, 202, 204},
		ResponseCodeFailureCodes: []int{},
		ResponseMode:             "full",
		MaxRedirects:             21,
		ThrowOnHTTPError:         true,
	}
}

// NewHttpAdvancedOptionsConfig 默认高级选项
func NewHttpAdvancedOptionsConfig() *HttpAdvancedOptionsConfig {
	return &HttpAdvancedOptionsConfig{FollowRedirect: true, Timeout: 10000, MaxBodyLength: 1048576}
}

// NewHttpRetryConfig 默认重试配置
func NewHttpRetryConfig() *HttpRetryConfig {
	return &HttpRetryConfig{
		MaxRetries:         3,
		RetryOnStatusCodes: []int{429, 500, 502, 503, 504},
		RetryDelay:         1000,
		BackoffType:        "exponential",
	}
}

// NewHttpRateLimitConfig 默认限流配置
func NewHttpRateLimitConfig() *HttpRateLimitConfig {
	return &HttpRateLimitConfig{RequestsPerUnit: 1, Unit: "second"}
}

// NewHttpRequestParamConfig 默认请求参数配置
func NewHttpRequestParamConfig() *HttpRequestParamConfig {
	return &HttpRequestParamConfig{
		Method:              "GET",
		Headers:             map[string]string{},
		QueryParameters:     map[string]any{},
		ResponseHandling:    NewHttpResponseHandlingConfig(),
		AdvancedOptions:     NewHttpAdvancedOptionsConfig(),
		RetryConfig:         NewHttpRetryConfig(),
		RateLimitConfig:     NewHttpRateLimitConfig(),
		Timeout:             60.0,
		MaxResponseByteSize: 10 * 1024 * 1024,
	}
}

// NewHttpComponentConfig 默认组件配置
func NewHttpComponentConfig() *HttpComponentConfig {
	return &HttpComponentConfig{RequestParams: NewHttpRequestParamConfig()}
}
