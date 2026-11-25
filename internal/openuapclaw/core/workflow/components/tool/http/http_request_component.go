package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/pythonsem"
	security "github.com/uapclaw/uapclaw-go/internal/common/security"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	components "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	"gopkg.in/yaml.v3"
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

// HttpComponentConfig 组件配置（Python :131-133 一比一；metadata 字段经
// components.ComponentConfig 基座承载——8.18 LLMComponentConfig 同款，审查收口②）
type HttpComponentConfig struct {
	// components.ComponentConfig 组件基座配置（承载 metadata: Optional[WorkflowComponentMetadata]）
	components.ComponentConfig
	// RequestParams 请求参数
	RequestParams *HttpRequestParamConfig
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

// NewHTTPRequestExecutable 构造执行体（Python __init__ :140-143 一比一）
func NewHTTPRequestExecutable(componentConfig *HttpComponentConfig) *HTTPRequestExecutable {
	e := &HTTPRequestExecutable{cfg: componentConfig, requestParams: componentConfig.RequestParams}
	// 四槽装配（8.20 槽优先铁律）：invoke 必装；stream/collect/transform
	// Python 为 yield/return invoke 委托单帧（:155-168）
	e.InvokeFn = e.invoke
	e.StreamFn = e.stream
	e.CollectFn = e.collect
	e.TransformFn = e.transform
	return e
}

// ProcessInputs 输入组装与合并（Python process_inputs :190-238 一比一）。
// 步骤序不可调换（规则 9.3）：url → method → headers → query → body → authentication。
func (e *HTTPRequestExecutable) ProcessInputs(inputs map[string]any) (map[string]any, error) {
	processed := map[string]any{}
	// url：占位符解析（:199）
	processed["url"] = resolvePlaceholders(e.requestParams.URL, inputs)
	// method：inputs 可覆盖 + 大写化（:202-203）
	method := e.requestParams.Method
	if m, ok := inputs["method"].(string); ok {
		method = m
	}
	processed["method"] = strings.ToUpper(method)
	// headers：str 模板 dict 化 / dict 拷贝，inputs.headers 覆盖（:206-212）
	var headers map[string]string
	switch raw := e.requestParams.Headers.(type) {
	case string:
		headers = toStringStringMap(resolveTemplateToDict(raw, inputs))
	case map[string]string:
		headers = make(map[string]string, len(raw))
		for k, v := range raw {
			headers[k] = v
		}
	case map[string]any:
		headers = toStringStringMap(raw)
	default:
		headers = map[string]string{}
	}
	if extra, ok := inputs["headers"].(map[string]any); ok {
		for k, v := range extra {
			headers[k] = pythonsem.PyStr(v)
		}
	}
	processed["headers"] = headers
	// query：str 值逐个占位符解析，inputs.query_parameters 覆盖（:215-220）
	query := map[string]any{}
	for k, v := range e.requestParams.QueryParameters {
		query[k] = v
	}
	for k, v := range query {
		if s, ok := v.(string); ok {
			query[k] = resolvePlaceholders(s, inputs)
		}
	}
	if extra, ok := inputs["query_parameters"].(map[string]any); ok {
		for k, v := range extra {
			query[k] = v
		}
	}
	processed["query_parameters"] = query
	// body：config 有 → json_data str 时解析【原地写回】；无 → inputs.body 默认 {}（:223-229）
	bodyConfig := e.requestParams.Body
	if bodyConfig != nil {
		if s, ok := bodyConfig.JSONData.(string); ok {
			// D-22-3 忠实复刻：写回 config 内嵌对象（组件级状态跨 invoke 持续）
			bodyConfig.JSONData = resolveTemplateToDict(s, inputs)
		}
		processed["body"] = bodyConfig
	} else {
		if b, ok := inputs["body"]; ok {
			processed["body"] = b
		} else {
			processed["body"] = map[string]any{}
		}
	}
	// authentication：config 有则透传；无 → inputs.authentication 默认 {}（:232-236）
	if e.requestParams.Authentication != nil {
		processed["authentication"] = e.requestParams.Authentication
	} else {
		if a, ok := inputs["authentication"]; ok {
			processed["authentication"] = a
		} else {
			processed["authentication"] = map[string]any{}
		}
	}
	return processed, nil
}

// ApplyAuthentication 认证注入请求头（Python apply_authentication :420-446 一比一）。
// DIGEST/AWS 与 Python 注释同款不实现；API_KEY 仅 header 位置生效。
func (e *HTTPRequestExecutable) ApplyAuthentication(headers map[string]string, authConfig any) map[string]string {
	auth, ok := authConfig.(*HttpAuthConfig)
	if !ok || auth == nil || auth.Type == HttpAuthTypeNone {
		return headers
	}
	switch auth.Type {
	case HttpAuthTypeBasic:
		// username/password 均非空才注入（Python :428 and 语义：nil 视为 falsy）
		if auth.Username != nil && *auth.Username != "" && auth.Password != nil && *auth.Password != "" {
			credentials := *auth.Username + ":" + *auth.Password
			headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(credentials))
		}
	case HttpAuthTypeBearer:
		if auth.Token != nil && *auth.Token != "" {
			headers["Authorization"] = "Bearer " + *auth.Token
		}
	case HttpAuthTypeAPIKey:
		if auth.APIKey != nil && *auth.APIKey != "" && auth.InLocation == "header" {
			headers[auth.Name] = *auth.APIKey
		}
	}
	// DIGEST/AWS：需额外实现，Python 注释保留（:443-444）
	return headers
}

// PrepareRequestBody 请求体编码（Python prepare_request_body :448-476 一比一）。
// 返回 (载荷, contentType)；FORM 分支为偏差 D-22-2（urlencoded 修正）。
func (e *HTTPRequestExecutable) PrepareRequestBody(bodyConfig any) (any, string) {
	bc, ok := bodyConfig.(*HttpRequestBodyConfig)
	if !ok || bc == nil {
		// Python `if not body_config` 对空 dict 同样短路返回（:452-453）
		return nil, ""
	}
	switch bc.ContentType {
	case HttpContentTypeJSON:
		if bc.JSONData != nil {
			// Python json.dumps（无 indent）；Go Marshal 键序排序为已知微差
			raw, err := json.Marshal(bc.JSONData)
			if err != nil {
				return nil, ""
			}
			return raw, "application/json"
		}
	case HttpContentTypeForm:
		if len(bc.FormData) > 0 {
			// D-22-2：Python aiohttp.FormData 出 multipart 载荷 + urlencoded 头不一致；
			// Go 修正为 urlencoded 载荷 + urlencoded 头（spec 拍板 #4）
			vals := url.Values{}
			for k, v := range bc.FormData {
				vals.Set(k, pythonsem.PyStr(v))
			}
			return vals.Encode(), "application/x-www-form-urlencoded"
		}
	case HttpContentTypeMultipartForm:
		if len(bc.MultipartForm) > 0 {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			for k, v := range bc.MultipartForm {
				_ = w.WriteField(k, pythonsem.PyStr(v))
			}
			_ = w.Close()
			// Python 头写死 'multipart/form-data'（不带 boundary，:466）——逐字复刻
			return buf.String(), "multipart/form-data"
		}
	case HttpContentTypeText:
		if bc.TextData != nil {
			return *bc.TextData, "text/plain"
		}
	case HttpContentTypeBinary:
		if bc.BinaryData != nil {
			// Python base64.b64decode 默认 std encoding（:473）
			raw, err := base64.StdEncoding.DecodeString(*bc.BinaryData)
			if err != nil {
				return nil, ""
			}
			return raw, "application/octet-stream"
		}
	}
	return nil, ""
}

// CalculateRetryDelay 重试延迟（秒）（Python calculate_retry_delay :478-491 一比一）
func (e *HTTPRequestExecutable) CalculateRetryDelay(retryCount int) float64 {
	baseDelay := float64(e.requestParams.RetryConfig.RetryDelay) / 1000.0
	switch e.requestParams.RetryConfig.BackoffType {
	case "fixed":
		return baseDelay
	case "linear":
		return baseDelay * float64(retryCount+1)
	case "exponential":
		return baseDelay * math.Pow(2, float64(retryCount))
	}
	return baseDelay
}

// MakeRequest 执行 HTTP 请求（Python make_request :240-354 一比一）。
// 步骤序（规则 9.3）：认证注入 → 请求体编码 + Content-Type → 代理 →
// 重试循环（每轮新建 transport + SSL env 每轮重求，spec 拍板 #3）→
// 分块读 + 超限 → 文本/二进制分型 → 结果 dict → 状态码重试判定 → 异常包装。
func (e *HTTPRequestExecutable) MakeRequest(params map[string]any) (map[string]any, error) {
	targetURL := params["url"].(string)
	method := params["method"].(string)
	headers := params["headers"].(map[string]string)
	queryParams := params["query_parameters"].(map[string]any)
	bodyConfig := params["body"]
	authConfig := params["authentication"]

	// 认证注入（:252）
	headers = e.ApplyAuthentication(headers, authConfig)

	// 请求体编码（:255-257）
	requestBody, contentType := e.PrepareRequestBody(bodyConfig)
	if contentType != "" {
		headers["Content-Type"] = contentType
	}

	// 代理（:260）
	proxy := security.GetProxyURL(targetURL, e.requestParams.AdvancedOptions.Proxy)

	// 超时（秒，:263）
	timeoutSeconds := e.requestParams.Timeout

	// 重试上限（:266-267）
	maxRetries := 0
	if e.requestParams.RetryConfig.Enabled {
		maxRetries = e.requestParams.RetryConfig.MaxRetries
	}
	retryCount := 0

	var result map[string]any
	for retryCount <= maxRetries {
		// 每轮新建 transport + SSL 策略每轮重求（:269-278，spec 拍板 #3）
		urlIsHTTPS := strings.HasPrefix(strings.ToLower(targetURL), "https://")
		sslVerify, sslCert, err := security.GetSSLConfig(sslVerifyEnv, sslCertEnv,
			[]string{sslTriggerValue}, urlIsHTTPS)
		if err != nil {
			return nil, err
		}
		tlsCfg := &tls.Config{InsecureSkipVerify: true} // ≙ aiohttp.TCPConnector(ssl=False)
		if sslVerify && !e.requestParams.AdvancedOptions.IgnoreSSLIssues {
			strict, serr := security.CreateStrictTLSConfig(sslCert)
			if serr != nil {
				return nil, serr
			}
			tlsCfg = strict
		}
		transport := &http.Transport{TLSClientConfig: tlsCfg}
		if proxy != "" {
			pu, perr := url.Parse(proxy)
			if perr != nil {
				return nil, exception.BuildError(exception.StatusComponentToolExecutionError,
					exception.WithParam("error_msg", fmt.Sprintf("HTTP request failed: %v", perr)))
			}
			transport.Proxy = http.ProxyURL(pu)
		}
		client := &http.Client{
			Transport: transport,
			Timeout:   time.Duration(timeoutSeconds * float64(time.Second)),
			// allow_redirects=False（:288；follow_redirect 死字段不生效，spec §4）
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		// 构造请求（:282-291）：仅 POST/PUT/PATCH 携带 body（:286 条件逐字）
		var bodyReader io.Reader
		if method == "POST" || method == "PUT" || method == "PATCH" {
			bodyReader = toBodyReader(requestBody)
		}
		q := url.Values{}
		for k, v := range queryParams {
			q.Set(k, pythonsem.PyStr(v))
		}
		fullURL := targetURL
		if len(q) > 0 {
			sep := "?"
			if strings.Contains(fullURL, "?") {
				sep = "&"
			}
			fullURL = fullURL + sep + q.Encode()
		}
		req, rerr := http.NewRequest(method, fullURL, bodyReader)
		if rerr != nil {
			return nil, exception.BuildError(exception.StatusComponentToolExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("HTTP request failed: %v", rerr)))
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, derr := client.Do(req)
		if derr != nil {
			// 异常路径：重试判定（:337-352）
			if e.requestParams.RetryConfig.Enabled && retryCount < maxRetries {
				time.Sleep(time.Duration(e.CalculateRetryDelay(retryCount) * float64(time.Second)))
				retryCount++
				continue
			}
			return nil, exception.BuildError(exception.StatusComponentToolExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("HTTP request failed: %v", derr)))
		}

		// 分块读 + 超限（:293-303）。Python raise 在 try 内 → 走 except
		// 重试路径再包装，此处同序（超限 → retry/包装，而非直接抛）
		content := []byte{}
		buf := make([]byte, chunkSize)
		readErr := error(nil)
	readLoop:
		for {
			n, rerr := resp.Body.Read(buf)
			content = append(content, buf[:n]...)
			if int64(len(content)) > int64(e.requestParams.MaxResponseByteSize) {
				readErr = exception.BuildError(exception.StatusComponentToolExecutionError,
					exception.WithParam("error_msg", fmt.Sprintf(
						"Response size (%d bytes) exceeds maximum allowed size \n                                        (%d bytes)",
						len(content), e.requestParams.MaxResponseByteSize)))
				break readLoop
			}
			if rerr == io.EOF {
				break readLoop
			}
			if rerr != nil {
				readErr = rerr
				break readLoop
			}
		}
		_ = resp.Body.Close()

		if readErr != nil {
			if e.requestParams.RetryConfig.Enabled && retryCount < maxRetries {
				time.Sleep(time.Duration(e.CalculateRetryDelay(retryCount) * float64(time.Second)))
				retryCount++
				continue
			}
			return nil, exception.BuildError(exception.StatusComponentToolExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("HTTP request failed: %v", readErr)))
		}

		// 文本/二进制分型（:306-313）
		respContentType := strings.ToLower(resp.Header.Get("Content-Type"))
		var contentResult any
		if containsAny(respContentType, textContentTypes) {
			contentResult = string(content)
		} else {
			contentResult = content // D-22-1：[]byte 直装
		}

		// 响应头 dict 化（:317）。Python dict(CIMultiDict) 对重复键后值覆盖前值，
		// 审查收口④：取切片末值（后值覆盖）对齐，不用 Join 多值拼接
		respHeaders := map[string]string{}
		for k, vs := range resp.Header {
			respHeaders[strings.ToLower(k)] = vs[len(vs)-1]
		}

		result = map[string]any{
			"status_code": resp.StatusCode,
			"headers":     respHeaders,
			"content":     contentResult,
			"url":         resp.Request.URL.String(),
			"reason":      resp.Status,
		}

		// 状态码重试判定（:324-335）
		if e.requestParams.RetryConfig.Enabled &&
			intInSlice(resp.StatusCode, e.requestParams.RetryConfig.RetryOnStatusCodes) &&
			retryCount < maxRetries {
			time.Sleep(time.Duration(e.CalculateRetryDelay(retryCount) * float64(time.Second)))
			retryCount++
			continue
		}
		break
	}
	return result, nil
}

// ProcessResponse 响应处理（Python process_response :356-418 一比一）。
// 步骤序：码判定 → autodetect 判型 → 按 format 解析 → property 抽取 →
// 结果 dict{statusCode,headers,body,url,ok} → response_mode 三分支。
func (e *HTTPRequestExecutable) ProcessResponse(response map[string]any) map[string]any {
	statusCode := response["status_code"].(int)
	content := response["content"]
	headers := response["headers"].(map[string]string)

	// 码判定（:365-369）
	successCodes := e.requestParams.ResponseHandling.ResponseCodeSuccessCodes
	failureCodes := e.requestParams.ResponseHandling.ResponseCodeFailureCodes
	isSuccess := intInSlice(statusCode, successCodes) ||
		(len(failureCodes) == 0 && statusCode >= 200 && statusCode < 300)
	isFailure := intInSlice(statusCode, failureCodes) || statusCode >= 400

	// autodetect 判型（:372-380）
	responseFormat := e.requestParams.ResponseHandling.ResponseFormat
	if responseFormat == HttpResponseFormatAutodetect {
		ct := strings.ToLower(headers["content-type"])
		if strings.Contains(ct, "application/json") {
			responseFormat = HttpResponseFormatJSON
		} else if strings.Contains(ct, "text/") {
			responseFormat = HttpResponseFormatText
		} else {
			responseFormat = HttpResponseFormatBinary
		}
	}

	// 按 format 解析（:383-394）
	parsedContent := content
	switch responseFormat {
	case HttpResponseFormatJSON:
		contentStr := ""
		if s, ok := content.(string); ok {
			contentStr = s
		} else if b, isB := content.([]byte); isB {
			contentStr = string(b)
		}
		var parsed any
		if err := json.Unmarshal([]byte(contentStr), &parsed); err == nil {
			parsedContent = parsed
		}
		// JSONDecodeError → parsed_content = content（原文透传，:388-389）
	case HttpResponseFormatText:
		if s, ok := content.(string); ok {
			parsedContent = s
		} else if b, isB := content.([]byte); isB {
			parsedContent = string(b)
		}
	case HttpResponseFormatBinary:
		// Python pass（:392-394）
	}

	// property 抽取（:397-401，仅顶层 key，miss 回原值）
	if prop := e.requestParams.ResponseHandling.ResponseDataProperty; prop != nil {
		if m, ok := parsedContent.(map[string]any); ok {
			if v, exists := m[*prop]; exists {
				parsedContent = v
			}
		}
	}

	// 结果 dict（:405-411）
	result := map[string]any{
		"statusCode": statusCode,
		"headers":    headers,
		"body":       parsedContent,
		"url":        response["url"],
		"ok":         isSuccess && !isFailure,
	}

	// response_mode 三分支（:404-417）
	responseMode := e.requestParams.ResponseHandling.ResponseMode
	if responseMode == "on-success" && !isSuccess {
		result = map[string]any{}
	} else if responseMode == "on-error" && isSuccess {
		result = map[string]any{}
	}
	return result
}

// NewHTTPRequestComponent 构造组件（Python __init__ :495-498 一比一）
func NewHTTPRequestComponent(componentConfig *HttpComponentConfig) *HTTPRequestComponent {
	c := &HTTPRequestComponent{config: componentConfig}
	// 槽优先（8.20 铁律）：ToExecutableFn 注入构造产物
	c.ToExecutableFn = func() graphexe.GeneralExecutor { return c.Executable() }
	return c
}

// Config 配置访问器（Python config property :500-503）
func (c *HTTPRequestComponent) Config() *HttpComponentConfig { return c.config }

// Executable 懒构造单例（Python executable property :505-509）
func (c *HTTPRequestComponent) Executable() *HTTPRequestExecutable {
	if c.executable == nil {
		c.executable = c.toExecutable()
	}
	return c.executable
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invoke 三步链（Python invoke :145-153 一比一；sess 形参为槽位签名要求，
// Python 亦收 session 不使用）
func (e *HTTPRequestExecutable) invoke(ctx context.Context, inputs map[string]any,
	sess *session.NodeSessionFacade, modelContext iface.ModelContext) (map[string]any, error) {
	processed, err := e.ProcessInputs(inputs)
	if err != nil {
		return nil, err
	}
	response, err := e.MakeRequest(processed)
	if err != nil {
		return nil, err
	}
	return e.ProcessResponse(response), nil
}

// stream 单帧委托（Python stream :155-158 yield invoke）
func (e *HTTPRequestExecutable) stream(ctx context.Context, inputs map[string]any,
	sess *session.NodeSessionFacade, modelContext iface.ModelContext) (<-chan graphexe.StreamChunk[any], error) {
	out, err := e.invoke(ctx, inputs, sess, modelContext)
	if err != nil {
		return nil, err
	}
	ch := make(chan graphexe.StreamChunk[any], 1)
	ch <- graphexe.StreamChunk[any]{Val: out}
	close(ch)
	return ch, nil
}

// collect 批出委托（Python collect :160-163 透传 inputs 给 invoke）。偏差 D-22-7：
// Python 流式 inputs 为「schema 重建 dict，叶子值 async generator」（stream_actor
// base.py:255-266 rebuild_dict），透传后占位符解析产出语义无意义结果（可跑通但结果坏，
// 零消费方未暴露）；Go 收帧取最后帧传 invoke——结构对齐"透传"、语义合理化。
func (e *HTTPRequestExecutable) collect(ctx context.Context, inputs <-chan map[string]any,
	sess *session.NodeSessionFacade, modelContext iface.ModelContext) (map[string]any, error) {
	last := drainLastFrame(inputs)
	return e.invoke(ctx, last, sess, modelContext)
}

// transform 单帧委托（Python transform :165-168 yield invoke；inputs 透传同 collect，D-22-7）
func (e *HTTPRequestExecutable) transform(ctx context.Context, inputs <-chan map[string]any,
	sess *session.NodeSessionFacade, modelContext iface.ModelContext) (<-chan graphexe.StreamChunk[any], error) {
	last := drainLastFrame(inputs)
	out, err := e.invoke(ctx, last, sess, modelContext)
	if err != nil {
		return nil, err
	}
	ch := make(chan graphexe.StreamChunk[any], 1)
	ch <- graphexe.StreamChunk[any]{Val: out}
	close(ch)
	return ch, nil
}

// drainLastFrame 消费流式 inputs 至关闭，返回最后帧（nil ≙ 无帧）。nil 通道直通。
func drainLastFrame(inputs <-chan map[string]any) map[string]any {
	if inputs == nil {
		return nil
	}
	var last map[string]any
	for frame := range inputs {
		last = frame
	}
	return last
}

// toExecutable 产出可执行体（Python to_executable :511-512）
func (c *HTTPRequestComponent) toExecutable() *HTTPRequestExecutable {
	return NewHTTPRequestExecutable(c.config)
}

// containsAny ct 是否命中白名单任一子串（Python any(x in ct for …) :308）
func containsAny(ct string, list []string) bool {
	for _, s := range list {
		if strings.Contains(ct, s) {
			return true
		}
	}
	return false
}

// intInSlice int 切片包含判定（Python `in` 列表）
func intInSlice(v int, list []int) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// toBodyReader 载荷 → io.Reader（aiohttp data= 参数对应物）
func toBodyReader(requestBody any) io.Reader {
	switch b := requestBody.(type) {
	case nil:
		return nil
	case []byte:
		return bytes.NewReader(b)
	case string:
		return strings.NewReader(b)
	default:
		return strings.NewReader(pythonsem.PyStr(b))
	}
}

// resolvePlaceholders {{key}} 字面替换（Python _resolve_placeholders :170-176；
// 值经 PyRepr 对齐 Python str(value)）
func resolvePlaceholders(template string, inputs map[string]any) string {
	for key, value := range inputs {
		placeholder := "{{" + key + "}}"
		template = strings.ReplaceAll(template, placeholder, pythonsem.PyStr(value))
	}
	return template
}

// resolveTemplateToDict 占位符解析后 dict 化（Python _resolve_template_to_dict
// :178-188；ast.literal_eval → Go yaml/json 宽解析，失败回 {}，偏差 D-22-4）
func resolveTemplateToDict(template string, inputs map[string]any) map[string]any {
	resolved := resolvePlaceholders(template, inputs)
	var parsed map[string]any
	// 先 json 后 yaml 宽解析：ast.literal_eval 接受 Python 字面量（单引号等），
	// yaml 超集覆盖；解析失败或非 dict → 空 map（except 分支逐字对齐）
	if err := json.Unmarshal([]byte(resolved), &parsed); err != nil {
		if err := yaml.Unmarshal([]byte(resolved), &parsed); err != nil {
			return map[string]any{}
		}
	}
	return parsed
}

// toStringStringMap map[string]any → map[string]string（headers 内部承载形态）
func toStringStringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = pythonsem.PyStr(v)
	}
	return out
}
