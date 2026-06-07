package service_api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/form_handler"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RestfulApiCard RESTful API 工具配置卡片，扩展 ToolCard 增加 HTTP 请求相关配置。
//
// 与 Python 对齐：input_params 使用原始 JSON Schema map（InputSchema），
// 因为 properties 中的每个参数可带 location 扩展属性（path/query/header/body/form），
// 这在 Go 的 []*Param 结构化列表中无法表达。
//
// 对应 Python: openjiuwen/core/foundation/tool/service_api/restful_api.py (RestfulApiCard)
type RestfulApiCard struct {
	tool.ToolCard
	// URL RESTful API 路径，如 /api/v1/users 或 https://api.example.com/users/{id}
	URL string
	// Method HTTP 方法，默认 POST
	Method string
	// Headers 默认请求头
	Headers map[string]any
	// Queries 默认查询参数
	Queries map[string]any
	// Paths 默认路径参数
	Paths map[string]any
	// Timeout 超时秒数，默认 60
	Timeout float64
	// MaxResponseByteSize 响应体大小限制（字节），默认 10MB
	MaxResponseByteSize int
	// InputSchema 原始 JSON Schema map，含 location 扩展属性
	// 替代 ToolCard.InputParams（[]*Param 无法表达 location 字段）
	InputSchema map[string]any
}

// RestfulApi HTTP REST 工具，将参数映射到 HTTP 请求的各位置并发送请求。
//
// 对应 Python: openjiuwen/core/foundation/tool/service_api/restful_api.py (RestfulApi)
type RestfulApi struct {
	card           *RestfulApiCard
	apiParamMapper *APIParamMapper
}

// ──────────────────────────── 枚举 ────────────────────────────

// RestfulApiCardOption RestfulApiCard 构造选项函数。
type RestfulApiCardOption func(*RestfulApiCard)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultTimeout 默认超时秒数
	defaultTimeout float64 = 60
	// defaultMaxResponseByteSize 默认响应体大小限制 10MB
	defaultMaxResponseByteSize int = 10 * 1024 * 1024
	// restfulSSLVerifyEnv SSL 验证开关环境变量名
	restfulSSLVerifyEnv = "RESTFUL_SSL_VERIFY"
	// restfulSSLCertEnv SSL 证书路径环境变量名
	restfulSSLCertEnv = "RESTFUL_SSL_CERT"
)

// supportedMethods 支持的 HTTP 方法集合
var supportedMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

// pathParamPattern 匹配 URL 中的路径参数占位符，如 {id}、{userId}
var pathParamPattern = regexp.MustCompile(`\{(\w+)\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithRestfulApiCardHeaders 设置默认请求头。
func WithRestfulApiCardHeaders(headers map[string]any) RestfulApiCardOption {
	return func(c *RestfulApiCard) { c.Headers = headers }
}

// WithRestfulApiCardQueries 设置默认查询参数。
func WithRestfulApiCardQueries(queries map[string]any) RestfulApiCardOption {
	return func(c *RestfulApiCard) { c.Queries = queries }
}

// WithRestfulApiCardPaths 设置默认路径参数。
func WithRestfulApiCardPaths(paths map[string]any) RestfulApiCardOption {
	return func(c *RestfulApiCard) { c.Paths = paths }
}

// WithRestfulApiCardTimeout 设置超时秒数。
func WithRestfulApiCardTimeout(timeout float64) RestfulApiCardOption {
	return func(c *RestfulApiCard) { c.Timeout = timeout }
}

// WithRestfulApiCardMaxResponseByteSize 设置响应体大小限制。
func WithRestfulApiCardMaxResponseByteSize(size int) RestfulApiCardOption {
	return func(c *RestfulApiCard) { c.MaxResponseByteSize = size }
}

// NewRestfulApiCard 创建 RestfulApiCard 实例。
//
// 校验逻辑：
//   - method 必须在支持的方法集合中
//   - URL 有效性校验（简化版，SSRF 防护 ⤵️ 预留回填点）
//   - URL 中的 {param} 路径占位符必须在 InputSchema 中有 location:path 定义
//
// 对应 Python: RestfulApiCard(url=..., method=..., ...)
func NewRestfulApiCard(
	name, description, apiURL, method string,
	inputSchema map[string]any,
	opts ...RestfulApiCardOption,
) (*RestfulApiCard, error) {
	// 校验 method
	method = strings.ToUpper(method)
	if method == "" {
		method = "POST"
	}
	if !supportedMethods[method] {
		return nil, exception.BuildError(
			exception.StatusToolRestfulApiCardConfigInvalid,
			exception.WithParam("reason", fmt.Sprintf("不支持的方法 %s，仅支持: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS", method)),
		)
	}

	// 校验 URL
	if err := validateURL(apiURL); err != nil {
		return nil, err
	}

	card := &RestfulApiCard{
		ToolCard:            *tool.NewToolCard(name, description, nil, nil),
		URL:                 apiURL,
		Method:              method,
		Headers:             make(map[string]any),
		Queries:             make(map[string]any),
		Paths:               make(map[string]any),
		Timeout:             defaultTimeout,
		MaxResponseByteSize: defaultMaxResponseByteSize,
		InputSchema:         inputSchema,
	}

	for _, opt := range opts {
		opt(card)
	}

	// 校验 URL 路径参数与 InputSchema 的匹配
	if err := validatePathParams(card.URL, card.InputSchema); err != nil {
		return nil, err
	}

	return card, nil
}

// ToolInfo 覆写 ToolCard.ToolInfo()，用 InputSchema 作为 parameters。
//
// 与 Python 对齐：RestfulApiCard 的 input_params 是原始 JSON Schema map，
// 直接作为 ToolInfo.parameters 传给 LLM（location 等扩展属性也包含在内）。
func (c *RestfulApiCard) ToolInfo() *schema.ToolInfo {
	return schema.NewToolInfo(c.Name, c.Description, c.InputSchema)
}

// NewRestfulApi 创建 RestfulApi 工具实例。
//
// 对应 Python: RestfulApi(card)
func NewRestfulApi(card *RestfulApiCard) (*RestfulApi, error) {
	if err := tool.ValidateToolCard(&card.ToolCard); err != nil {
		return nil, err
	}
	return &RestfulApi{
		card: card,
		apiParamMapper: NewAPIParamMapper(
			card.InputSchema,
			card.Queries,
			card.Headers,
			card.Paths,
		),
	}, nil
}

// Card 返回工具配置卡片。
func (r *RestfulApi) Card() *tool.ToolCard {
	return &r.card.ToolCard
}

// Invoke 一次性执行 RESTful API 请求，返回完整结果。
//
// 流程（严格对齐 Python RestfulApi.invoke）：
//  1. 如果 card.InputSchema 不为 nil，用 FormatWithSchemaMap 格式化输入
//  2. APIParamMapper.Map 映射参数到各位置
//  3. 构建 HTTP 请求（path 替换 → query 拼接 → header 合并 → body 设置）
//  4. 发送请求（net/http + timeout + 代理）
//  5. 检查响应体大小
//  6. ParserRegistry 解析响应
//  7. 返回 {code, data, url, headers, reason, message} 结构
//
// 对应 Python: RestfulApi.invoke()
func (r *RestfulApi) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	callOpts := tool.NewToolCallOptions(opts...)
	finalTimeout := r.card.Timeout

	// 1. 格式化输入
	if r.card.InputSchema != nil {
		formatted, err := tool.SchemaUtils{}.FormatWithSchemaMap(
			inputs, r.card.InputSchema,
			tool.WithFormatSkipNoneValue(callOpts.SkipNoneValue),
			tool.WithFormatSkipValidate(callOpts.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		inputs = formatted
	}

	// 2. 映射参数
	defaultLocation := APIParamLocationBody
	if r.card.Method == "GET" || r.card.Method == "HEAD" ||
		r.card.Method == "OPTIONS" || r.card.Method == "DELETE" {
		defaultLocation = APIParamLocationQuery
	}
	mapResults := r.apiParamMapper.Map(inputs, defaultLocation)

	// 3. 确定超时
	finalTimeout = callOpts.Timeout
	if finalTimeout <= 0 {
		finalTimeout = r.card.Timeout
	}

	// 4. 发送请求
	maxResponseBytes := callOpts.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = r.card.MaxResponseByteSize
	}
	raiseForStatus := callOpts.RaiseForStatus

	return r.doRequest(ctx, mapResults, finalTimeout, maxResponseBytes, raiseForStatus)
}

// Stream RestfulApi 不支持流式调用，返回 ErrStreamNotSupported。
//
// 对应 Python: RestfulApi.stream() → raise TOOL_STREAM_NOT_SUPPORTED
func (r *RestfulApi) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(r.card.String())
}

// GetParametersByLocation 按 location 分组展示参数，供 GUI 使用。
//
// 对应 Python: RestfulApi.get_parameters_by_location()
func GetParametersByLocation(card *RestfulApiCard) map[string][]map[string]any {
	result := map[string][]map[string]any{
		"path":   {},
		"query":  {},
		"header": {},
		"body":   {},
		"form":   {},
	}

	if card.InputSchema == nil {
		return result
	}

	properties, _ := card.InputSchema["properties"].(map[string]any)
	requiredFields, _ := card.InputSchema["required"].([]any)

	requiredSet := make(map[string]bool)
	for _, r := range requiredFields {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}

	for paramName, paramDef := range properties {
		paramMap, _ := paramDef.(map[string]any)
		location := "body" // 默认 body
		if loc, ok := paramMap["location"].(string); ok {
			location = strings.ToLower(loc)
		}

		paramInfo := map[string]any{
			"name":        paramName,
			"type":        "string",
			"description": "",
			"required":    requiredSet[paramName],
		}
		if t, ok := paramMap["type"].(string); ok {
			paramInfo["type"] = t
		}
		if d, ok := paramMap["description"].(string); ok {
			paramInfo["description"] = d
		}
		if d, ok := paramMap["default"]; ok {
			paramInfo["default"] = d
		}

		result[location] = append(result[location], paramInfo)
	}

	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// doRequest 构建并发送 HTTP 请求。
//
// 对应 Python: RestfulApi._async_request()
func (r *RestfulApi) doRequest(
	ctx context.Context,
	mapResults map[APIParamLocation]map[string]any,
	timeout float64,
	maxResponseBytes int,
	raiseForStatus bool,
) (map[string]any, error) {
	// 构建 URL
	reqURL := r.card.URL

	// 替换路径参数 {id} → 实际值
	pathParams := mapResults[APIParamLocationPath]
	for k, v := range pathParams {
		reqURL = strings.ReplaceAll(reqURL, "{"+k+"}", fmt.Sprintf("%v", v))
	}

	// 拼接 query 参数
	queryParams := mapResults[APIParamLocationQuery]
	if len(queryParams) > 0 {
		values := url.Values{}
		for k, v := range queryParams {
			switch tv := v.(type) {
			case []any:
				// 数组展开：ids=[1,2,3] → ids=1&ids=2&ids=3
				for _, item := range tv {
					values.Add(k, fmt.Sprintf("%v", item))
				}
			default:
				values.Add(k, fmt.Sprintf("%v", tv))
			}
		}
		if strings.Contains(reqURL, "?") {
			reqURL = reqURL + "&" + values.Encode()
		} else {
			reqURL = reqURL + "?" + values.Encode()
		}
	}

	// 合并 headers
	headers := mapResults[APIParamLocationHeader]
	headerMap := make(map[string]string)
	for k, v := range headers {
		headerMap[k] = fmt.Sprintf("%v", v)
	}

	// 构建请求体
	formParams := mapResults[APIParamLocationForm]
	bodyParams := mapResults[APIParamLocationBody]
	var bodyReader io.Reader

	if len(formParams) > 0 {
		// 使用 FormHandlerManager 处理 form 参数，构建 multipart/form-data 请求体
		formBody, formContentType, formErr := r.processFormData(ctx, formParams, bodyParams)
		if formErr != nil {
			return nil, formErr
		}
		bodyReader = bytes.NewReader(formBody)
		headerMap = prepareHeadersForFormData(headerMap)
		headerMap["Content-Type"] = formContentType
	} else if r.card.Method == "GET" || r.card.Method == "HEAD" ||
		r.card.Method == "OPTIONS" || r.card.Method == "DELETE" {
		// GET/HEAD/OPTIONS/DELETE：body_params 已作为 query params 处理
		// 如果 bodyParams 不为空，追加到 query（与 Python 对齐）
		if len(bodyParams) > 0 {
			values := url.Values{}
			for k, v := range bodyParams {
				values.Add(k, fmt.Sprintf("%v", v))
			}
			if strings.Contains(reqURL, "?") {
				reqURL = reqURL + "&" + values.Encode()
			} else {
				reqURL = reqURL + "?" + values.Encode()
			}
		}
	} else {
		// POST/PUT/PATCH：body_params 作为 JSON body
		if len(bodyParams) > 0 {
			bodyBytes, _ := json.Marshal(bodyParams)
			bodyReader = bytes.NewReader(bodyBytes)
			if _, ok := headerMap["Content-Type"]; !ok {
				headerMap["Content-Type"] = "application/json"
			}
		}
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, r.card.Method, reqURL, bodyReader)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolRestfulApiExecutionError,
			exception.WithParam("method", r.card.Method),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", r.card.String()),
			exception.WithCause(err),
		)
	}

	// 设置 headers
	for k, v := range headerMap {
		req.Header.Set(k, v)
	}

	// SSL 配置（⤵️ 3.11 回填回调触发，当前直接用环境变量）
	// verifySwitch := os.Getenv(restfulSSLVerifyEnv)
	// sslCert := os.Getenv(restfulSSLCertEnv)

	// 创建 HTTP 客户端（支持代理）
	client := &http.Client{
		Timeout: time.Duration(timeout * float64(time.Second)),
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		// 超时检测
		if ctx.Err() == context.DeadlineExceeded {
			return nil, exception.BuildError(
				exception.StatusToolRestfulApiExecutionTimeout,
				exception.WithParam("method", r.card.Method),
				exception.WithParam("timeout", fmt.Sprintf("%.0f", timeout)),
				exception.WithParam("card", r.card.String()),
				exception.WithCause(err),
			)
		}
		return nil, exception.BuildError(
			exception.StatusToolRestfulApiExecutionError,
			exception.WithParam("method", r.card.Method),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", r.card.String()),
			exception.WithCause(err),
		)
	}
	defer resp.Body.Close()

	// 检查 HTTP 状态码
	if raiseForStatus && resp.StatusCode >= 400 {
		return nil, exception.BuildError(
			exception.StatusToolRestfulApiResponseError,
			exception.WithParam("method", r.card.Method),
			exception.WithParam("code", fmt.Sprintf("%d", resp.StatusCode)),
			exception.WithParam("reason", resp.Status),
		)
	}

	// 格式化响应
	return r.formatResponse(resp, maxResponseBytes)
}

// formatResponse 格式化 HTTP 响应。
//
// 对应 Python: RestfulApi._format_response()
func (r *RestfulApi) formatResponse(resp *http.Response, maxResponseBytes int) (map[string]any, error) {
	// 读取响应体（限制大小）
	limitedReader := io.LimitReader(resp.Body, int64(maxResponseBytes)+1)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolRestfulApiResponseProcessError,
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", r.card.String()),
			exception.WithCause(err),
		)
	}

	// 检查是否超出大小限制
	if len(bodyBytes) > maxResponseBytes {
		return nil, exception.BuildError(
			exception.StatusToolRestfulApiResponseSizeExceedLimit,
			exception.WithParam("method", r.card.Method),
			exception.WithParam("max_length", fmt.Sprintf("%d", maxResponseBytes)),
			exception.WithParam("actual_length", fmt.Sprintf("%d", len(bodyBytes))),
			exception.WithParam("card", r.card.String()),
		)
	}

	// 收集响应头
	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	// 使用 ParserRegistry 解析响应
	parsedResponse, err := GetParserRegistry().Parse(respHeaders, bodyBytes, resp.StatusCode)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolRestfulApiResponseProcessError,
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", r.card.String()),
			exception.WithCause(err),
		)
	}

	// 构建返回结果（对齐 Python）
	result := map[string]any{
		"code":    resp.StatusCode,
		"data":    parsedResponse,
		"url":     resp.Request.URL.String(),
		"headers": respHeaders,
		"reason":  resp.Status,
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result["message"] = "success"
	} else {
		result["message"] = resp.Status
	}

	return result, nil
}

// validateURL 校验 URL 有效性。
//
// 简化版：使用 net/url 解析。SSRF 防护等安全特性 ⤵️ 预留回填点
// （等 common/security/url_utils.go 迁移后回填）。
func validateURL(rawURL string) error {
	if rawURL == "" {
		return exception.BuildError(
			exception.StatusToolRestfulApiCardConfigInvalid,
			exception.WithParam("reason", "URL 不能为空"),
		)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return exception.BuildError(
			exception.StatusToolRestfulApiCardConfigInvalid,
			exception.WithParam("reason", fmt.Sprintf("URL 解析失败: %s", err.Error())),
		)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return exception.BuildError(
			exception.StatusToolRestfulApiCardConfigInvalid,
			exception.WithParam("reason", fmt.Sprintf("不支持的 URL 协议: %s，仅支持 http/https", parsed.Scheme)),
		)
	}
	return nil
}

// validatePathParams 校验 URL 中的路径参数占位符与 InputSchema 中 location:path 定义的匹配。
//
// 对应 Python: RestfulApiCard.model_post_init()
func validatePathParams(apiURL string, inputSchema map[string]any) error {
	// 提取 URL 中的路径参数名
	urlPathParams := pathParamPattern.FindAllStringSubmatch(apiURL, -1)
	if len(urlPathParams) == 0 {
		return nil
	}

	urlPathParamSet := make(map[string]bool)
	for _, match := range urlPathParams {
		urlPathParamSet[match[1]] = true
	}

	// 检查 InputSchema 是否定义
	if inputSchema == nil {
		paramNames := make([]string, 0, len(urlPathParamSet))
		for k := range urlPathParamSet {
			paramNames = append(paramNames, k)
		}
		return exception.BuildError(
			exception.StatusToolRestfulApiCardConfigInvalid,
			exception.WithParam("reason", fmt.Sprintf(
				"URL 包含路径参数 %v 但未定义 input_params schema，必须为每个路径参数定义 location:path",
				paramNames)),
		)
	}

	properties, _ := inputSchema["properties"].(map[string]any)

	// 收集 schema 中标记为 path 的参数
	schemaPathParams := make(map[string]bool)
	for paramName, paramDef := range properties {
		if paramMap, ok := paramDef.(map[string]any); ok {
			if loc, ok := paramMap["location"].(string); ok && strings.ToLower(loc) == "path" {
				schemaPathParams[paramName] = true
			}
		}
	}

	// 检查 URL 中的路径参数是否都在 schema 中定义
	missingInSchema := make([]string, 0)
	for k := range urlPathParamSet {
		if !schemaPathParams[k] {
			missingInSchema = append(missingInSchema, k)
		}
	}
	if len(missingInSchema) > 0 {
		return exception.BuildError(
			exception.StatusToolRestfulApiCardConfigInvalid,
			exception.WithParam("reason", fmt.Sprintf(
				"URL 包含路径参数 %v 但未在 input_params schema 中定义 location:path",
				missingInSchema)),
		)
	}

	return nil
}

// processFormData 使用 FormHandlerManager 处理表单参数，构建 multipart/form-data 请求体。
//
// 流程（对齐 Python RestfulApi._process_form_data）：
//  1. 创建 bytes.Buffer + multipart.Writer
//  2. 遍历 formParams，每个参数根据 form_handler_type 获取对应处理器
//  3. 调用 handler.Handle() 将字段写入 multipart Writer
//  4. 遍历 bodyParams，非 nil 值以 application/json content_type 写入
//  5. 关闭 Writer，返回 buffer 字节和 multipart content-type（含 boundary）
//
// 对应 Python: RestfulApi._process_form_data()
func (r *RestfulApi) processFormData(
	ctx context.Context,
	formParams map[string]any,
	bodyParams map[string]any,
) (bodyBytes []byte, contentType string, err error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	mgr := form_handler.GetFormHandlerManager()

	// 处理 form_params
	for paramName, paramInfo := range formParams {
		info, ok := paramInfo.(map[string]any)
		if !ok {
			continue
		}
		handlerType := "default"
		if ht, ok := info["form_handler_type"].(string); ok && ht != "" {
			handlerType = ht
		}
		value := info["value"]

		handler := mgr.GetHandler(handlerType)
		if handleErr := handler.Handle(ctx, writer, paramName, value); handleErr != nil {
			return nil, "", exception.BuildError(
				exception.StatusToolRestfulApiExecutionError,
				exception.WithParam("method", r.card.Method),
				exception.WithParam("reason", fmt.Sprintf("表单字段 %q 处理失败: %s", paramName, handleErr.Error())),
				exception.WithCause(handleErr),
			)
		}
	}

	// 处理 body_params：以 application/json content-type 追加到 form
	for paramName, paramValue := range bodyParams {
		if paramValue == nil {
			continue
		}
		jsonBytes, marshalErr := json.Marshal(paramValue)
		if marshalErr != nil {
			continue
		}
		part, createErr := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {fmt.Sprintf(`form-data; name="%s"`, paramName)},
			"Content-Type":        {"application/json"},
		})
		if createErr != nil {
			continue
		}
		if _, writeErr := part.Write(jsonBytes); writeErr != nil {
			continue
		}
	}

	// 关闭 Writer（必须，写入 terminating boundary）
	writer.Close()

	return buf.Bytes(), writer.FormDataContentType(), nil
}

// prepareHeadersForFormData 为 multipart/form-data 请求准备请求头。
//
// 移除手动设置的 Content-Type，因为 multipart.Writer 会自动生成
// 包含 boundary 的正确 Content-Type。手动设置会导致请求失败。
//
// 对应 Python: RestfulApi._prepare_headers_for_form_data()
func prepareHeadersForFormData(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return make(map[string]string)
	}
	processed := make(map[string]string, len(headers))
	for key, value := range headers {
		if strings.ToLower(key) == "content-type" {
			logger.Debug(logger.ComponentAgentCore).
				Str("content_type", value).
				Msg("multipart/form-data 请求移除手动设置的 Content-Type，将自动设置含 boundary 的正确值")
			continue
		}
		processed[key] = value
	}
	return processed
}
