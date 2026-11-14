package recommendation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// topic_search.go topic 推荐的搜索适配器：OpenJiuwen 原生自由搜索与
// Ark Responses Web Search。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_search.py
// （678 行，一比一移植；aiohttp/openai SDK → net/http 等价转换）。
// 既有切片：常量 OpenjiuwenNativeSearchProviderId / OpenJiuwenNativeSearchContractError /
// SelectedTopicSearchProviderID 已随 31b 落在 topic_pipeline_d1.go（⤴️31c 归位仅收口）。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// OpenjiuwenNativeSearchMaxConcurrency 原生搜索并发上限（topic_search.py:47）。
	OpenjiuwenNativeSearchMaxConcurrency = 3
	// openjiuwenWebSearchEngineErrorCode 引擎错误码（:48）。
	openjiuwenWebSearchEngineErrorCode = 182600
	// openjiuwenWebSearchAllEnginesFailedCode 全引擎失败码（:49）。
	openjiuwenWebSearchAllEnginesFailedCode = 182601
	// ArkWebSearchEntitlementReceiptVersion 权益拒绝小票版本（:354-356）。
	ArkWebSearchEntitlementReceiptVersion = "topic-starter-v6-ark-responses-web-search-entitlement-v1"
	// SearchInstructions 搜索指令（:365-368）。
	SearchInstructions = "Call web_search exactly once. Use the user input verbatim as the search " +
		"action query. Return a concise answer with URL citations."
)

// non200StatusPattern non-200 状态尾缀（:50）。
var non200StatusPattern = regexp.MustCompile(`non-200 status: ([1-5][0-9]{2})$`)

// entitlementReceiptFields 权益小票字段集（:357-367）。
var entitlementReceiptFields = []string{
	"receipt_version", "provider_id", "actual_model", "operation", "tool_type",
	"provider_error_code", "http_status", "provider_request_id", "observed_at",
}

// ──────────────────────────── 结构体 ────────────────────────────

// OpenJiuwenNativeSearchResult 原生搜索结果（:73-78）。
type OpenJiuwenNativeSearchResult struct {
	// Engine 实际引擎
	Engine string
	// Rows 结果行
	Rows []map[string]string
	// Receipt 成功小票
	Receipt map[string]any
}

// ArkWebSearchResult Ark web search 结果（:365-370）。
type ArkWebSearchResult struct {
	// Engine 引擎
	Engine string
	// Rows URL citation 行
	Rows []map[string]string
	// Receipt 小票
	Receipt map[string]any
}

// ArkWebSearchContractError 宿主响应解析器的稳定 fail-closed 原因（:371-375）。
type ArkWebSearchContractError struct {
	// Reason 契约失败码
	Reason string
}

// Error 实现 error 接口。
func (e *ArkWebSearchContractError) Error() string { return e.Reason }

// ArkWebSearchEntitlementError 厂商拒绝 Web Search 工具的 redacted 证据
// （:377-384）。
type ArkWebSearchEntitlementError struct {
	// Receipt 权益小票
	Receipt map[string]any
}

// Error 实现 error 接口。
func (e *ArkWebSearchEntitlementError) Error() string { return "ark_web_search_entitlement_blocked" }

// providerStatusError 等价 openai.APIStatusError 的消费面。
type providerStatusError struct {
	// StatusCode HTTP 状态码
	StatusCode int
	// RequestID 厂商请求 ID
	RequestID string
	// Body 响应体
	Body map[string]any
}

// Error 实现 error 接口。
func (e *providerStatusError) Error() string {
	return fmt.Sprintf("non-200 status: %d", e.StatusCode)
}

// ResponsesClient Responses API 客户端消费面（等价 AsyncOpenAI.responses.create）。
type ResponsesClient interface {
	// ResponsesCreate 创建一次 responses 调用
	ResponsesCreate(ctx context.Context, request map[string]any) (map[string]any, error)
	// Close 关闭底层资源
	Close() error
}

// ResponsesClientFactory 构造客户端（对齐 client_factory）。
type ResponsesClientFactory func(config *ArkBackgroundConfig) (ResponsesClient, error)

// SearchRunner 结构化搜索执行器（:63-66）。
type SearchRunner = func(query string, maxResults, timeoutSeconds int) (string, []map[string]string, error)

// ──────────────────────────── 全局变量 ────────────────────────────

// defaultResponsesClientFactory 默认 Responses 客户端（对齐 :376-379
// _default_client_factory：AsyncOpenAI(api_key, base_url) 等价转换）。
var defaultResponsesClientFactory ResponsesClientFactory = func(config *ArkBackgroundConfig) (ResponsesClient, error) {
	return newArkResponsesHTTPClient(config), nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildOpenjiuwenNativeSearch 构造原生自由搜索门面（:317-330）。
func BuildOpenjiuwenNativeSearch(environ map[string]string, searchRunner SearchRunner,
	requestIDFactory func() string) (*OpenJiuwenNativeSearch, error) {
	values := environ
	if values == nil {
		values = osEnvironMap()
	}
	if err := RequireNativeFreeSearch(values); err != nil {
		return nil, err
	}
	return NewOpenJiuwenNativeSearch(searchRunner, requestIDFactory), nil
}

// RequireNativeFreeSearch 原生 lane 需要两个自由搜索引擎都启用（:332-343）。
func RequireNativeFreeSearch(values map[string]string) error {
	enabled := func(key string) bool {
		return strings.ToLower(strings.TrimSpace(values[key])) == "true"
	}
	if !enabled("FREE_SEARCH_DDG_ENABLED") || !enabled("FREE_SEARCH_BING_ENABLED") {
		return &OpenJiuwenNativeSearchContractError{
			Reason: "openjiuwen_native_search_requires_free_engines"}
	}
	return nil
}

// BuildTopicSearchProvider 构造进程启动时选定的搜索 lane（:365-374）。
func BuildTopicSearchProvider(_ *ArkBackgroundConfig, environ map[string]string,
	nativeFactory func(map[string]string) (*OpenJiuwenNativeSearch, error)) (any, error) {
	values := environ
	if values == nil {
		values = osEnvironMap()
	}
	if _, err := SelectedTopicSearchProviderID(values); err != nil {
		return nil, err
	}
	if nativeFactory == nil {
		nativeFactory = func(values map[string]string) (*OpenJiuwenNativeSearch, error) {
			return BuildOpenjiuwenNativeSearch(values, nil, nil)
		}
	}
	return nativeFactory(values)
}

// NewArkResponsesWebSearch 构造 Ark Responses Web Search 门面（:519-528）。
func NewArkResponsesWebSearch(config *ArkBackgroundConfig, clientFactory ResponsesClientFactory) (*ArkResponsesWebSearch, error) {
	if clientFactory == nil {
		clientFactory = defaultResponsesClientFactory
	}
	client, err := clientFactory(config)
	if err != nil {
		return nil, err
	}
	return &ArkResponsesWebSearch{
		config:             config,
		client:             client,
		InvocationReceipts: []map[string]any{},
		sem:                make(chan struct{}, 5),
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// osEnvironMap 环境变量映射。
func osEnvironMap() map[string]string {
	env := map[string]string{}
	for _, entry := range os.Environ() {
		if idx := strings.Index(entry, "="); idx >= 0 {
			env[entry[:idx]] = entry[idx+1:]
		}
	}
	return env
}

// versionOrDateTokenRE 版本号/日期 token（topic_news.py:44-48；
// 三支 OR 手写等价，RE2 无回溯）。
var versionOrDateTokenRE = regexp.MustCompile(`(?i)^(?:v(?:er(?:sion)?)?\.?\s*)?\d+(?:\.\d+)+$|^v?\d+(?:\.\d+)*[a-z]?$|^(?:19|20)\d{2}(?:年|[-/]\d{1,2}(?:[-/]\d{1,2})?)?$`)

// newNativeRequestID 请求 ID（:70-72）。
func newNativeRequestID() string {
	return fmt.Sprintf("openjiuwen-native-search-%s", newUUIDHex())
}

// providerFailureKind 厂商失败分类（:96-133 _provider_failure_kind）。
// Python 靠异常类型判定；Go 侧以自有错误类型 + 约定等价：
// *nativeProviderAPIError 携带 code/params（182600/182601 语义）、
// context.DeadlineExceeded → timeout、*providerStatusError → http_status、
// 其余 → transport。
func providerFailureKind(err error) (string, any) {
	var apiErr *nativeProviderAPIError
	switch {
	case nativeAs(err, &apiErr) && apiErr.Code == openjiuwenWebSearchAllEnginesFailedCode:
		return "all_engines_failed", nil
	case nativeAs(err, &apiErr) && apiErr.Code == openjiuwenWebSearchEngineErrorCode &&
		apiErr.Params["engine"] == "duckduckgo":
		providerReason, _ := apiErr.Params["reason"].(string)
		if providerReason == "anti-bot challenge page returned" {
			return "challenge", nil
		}
		if m := non200StatusPattern.FindStringSubmatch(providerReason); m != nil {
			status := 0
			for _, ch := range m[1] {
				status = status*10 + int(ch-'0')
			}
			return "http_status", status
		}
		return "transport", nil
	case err == context.DeadlineExceeded:
		return "timeout", nil
	case strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "Client.Timeout"):
		return "timeout", nil
	}
	var statusErr *providerStatusError
	if nativeAs(err, &statusErr) && statusErr.StatusCode >= 100 && statusErr.StatusCode <= 599 {
		return "http_status", statusErr.StatusCode
	}
	if strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "connect") ||
		strings.Contains(err.Error(), "proxy") {
		if strings.Contains(err.Error(), "proxy") {
			return "proxy", nil
		}
		return "transport", nil
	}
	return "unknown", nil
}

// nativeAs 简化 errors.As。
func nativeAs(err error, target any) bool {
	if err == nil {
		return false
	}
	switch t := target.(type) {
	case **nativeProviderAPIError:
		if e, ok := err.(*nativeProviderAPIError); ok {
			*t = e
			return true
		}
	case **providerStatusError:
		if e, ok := err.(*providerStatusError); ok {
			*t = e
			return true
		}
	}
	return false
}

// nativeProviderAPIError 等价厂商 API 错误对象（code + params）。
type nativeProviderAPIError struct {
	// Code 错误码
	Code int
	// Params 附加参数
	Params map[string]any
}

// Error 实现 error 接口。
func (e *nativeProviderAPIError) Error() string {
	return fmt.Sprintf("provider api error: code=%d", e.Code)
}

// providerFailureReceipt 失败小票（:135-147）。
func providerFailureReceipt(err error, hostRequestID string) map[string]any {
	kind, httpStatus := providerFailureKind(err)
	receipt := map[string]any{
		"receipt_version": OpenjiuwenNativeSearchFailureReceiptVersion,
		"provider_id":     OpenjiuwenNativeSearchProviderId,
		"host_request_id": hostRequestID,
		"engine":          "openjiuwen-free-search",
		"failure_kind":    kind,
	}
	if httpStatus != nil {
		receipt["http_status"] = httpStatus
	}
	return receipt
}

// defaultSearchRunner 默认结构化搜索执行器（:75-94 _default_search_runner；
// 跨边界调用 openjiuwen 自由搜索结构化入口，与 Python 同 necessity）。
func defaultSearchRunner(query string, maxResults, timeoutSeconds int) (string, []map[string]string, error) {
	return tools.RunFreeSearchStructured(query, maxResults, timeoutSeconds)
}

// newArkResponsesHTTPClient 构造默认 Responses HTTP 客户端。
func newArkResponsesHTTPClient(config *ArkBackgroundConfig) ResponsesClient {
	return &arkResponsesHTTPClient{config: config}
}

// ──────────────────────────── 结构体 ────────────────────────────

// OpenJiuwenNativeSearch OpenJiuwen 原生自由搜索的结构化门面（:174-196）。
type OpenJiuwenNativeSearch struct {
	// ProviderId 厂商 ID
	ProviderId string
	// InvocationReceipts 调用小票（对齐 invocation_receipts 列表）
	InvocationReceipts []map[string]any

	searchRunner       SearchRunner
	requestIDFactory   func() string
	searchSemaphore    chan struct{}
	searchSemaphoreMu  sync.Mutex
	invocationReceipts []map[string]any
}

// NewOpenJiuwenNativeSearch 构造（对齐 __init__：默认 runner/request_id 工厂）。
func NewOpenJiuwenNativeSearch(searchRunner SearchRunner, requestIDFactory func() string) *OpenJiuwenNativeSearch {
	if searchRunner == nil {
		searchRunner = defaultSearchRunner
	}
	if requestIDFactory == nil {
		requestIDFactory = newNativeRequestID
	}
	return &OpenJiuwenNativeSearch{
		ProviderId:         OpenjiuwenNativeSearchProviderId,
		searchRunner:       searchRunner,
		requestIDFactory:   requestIDFactory,
		searchSemaphore:    make(chan struct{}, OpenjiuwenNativeSearchMaxConcurrency),
		invocationReceipts: []map[string]any{},
	}
}

// Search 执行结构化搜索（对齐 :192-289 __call__）。
func (s *OpenJiuwenNativeSearch) Search(_ context.Context, query string, maxResults, timeoutSeconds int) (*OpenJiuwenNativeSearchResult, error) {
	hostRequestID := strings.TrimSpace(s.requestIDFactory())
	if hostRequestID == "" || strings.TrimSpace(query) == "" || maxResults < 1 || maxResults > 20 || timeoutSeconds <= 0 {
		return nil, &OpenJiuwenNativeSearchContractError{
			Reason: "search_request_contract_failed", HostRequestID: hostRequestID}
	}
	// 信号量并发上限（asyncio.Semaphore(3) 等价）
	s.searchSemaphore <- struct{}{}
	engine, rawRows, runErr := func() (string, []map[string]string, error) {
		defer func() { <-s.searchSemaphore }()
		return s.searchRunner(query, maxResults, timeoutSeconds)
	}()
	if runErr != nil {
		return nil, &OpenJiuwenNativeSearchProviderError{
			Receipt:       providerFailureReceipt(runErr, hostRequestID),
			hostRequestID: hostRequestID,
		}
	}
	if !OpenjiuwenNativeSearchEngines[engine] || rawRows == nil {
		return nil, &OpenJiuwenNativeSearchContractError{
			Reason: "search_result_contract_failed", HostRequestID: hostRequestID}
	}
	rows := make([]map[string]string, 0, len(rawRows))
	urls := make([]string, 0, len(rawRows))
	for _, rawRow := range rawRows {
		if len(rows) >= maxResults {
			break
		}
		if rawRow == nil {
			return nil, &OpenJiuwenNativeSearchContractError{
				Reason: "search_result_contract_failed", HostRequestID: hostRequestID}
		}
		rowURL := strings.TrimSpace(rawRow["url"])
		parsed, _ := url.Parse(rowURL)
		if parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return nil, &OpenJiuwenNativeSearchContractError{
				Reason: "search_result_contract_failed", HostRequestID: hostRequestID}
		}
		rows = append(rows, rawRow)
		urls = append(urls, rowURL)
	}
	if len(rows) == 0 || hasDuplicate(urls) {
		return nil, &OpenJiuwenNativeSearchContractError{
			Reason: "search_result_contract_failed", HostRequestID: hostRequestID}
	}
	receipt := map[string]any{
		"receipt_version": OpenjiuwenNativeSearchReceiptVersion,
		"provider_id":     OpenjiuwenNativeSearchProviderId,
		"host_request_id": hostRequestID,
		"requested_query": query,
		"engine":          engine,
		"result_urls":     urls,
		"result_count":    len(rows),
	}
	result := &OpenJiuwenNativeSearchResult{Engine: engine, Rows: rows, Receipt: receipt}
	s.invocationReceipts = append(s.invocationReceipts, receipt)
	return result, nil
}

// OpenJiuwenNativeSearchProviderError redacted 厂商失败（:88-95）。
type OpenJiuwenNativeSearchProviderError struct {
	// Receipt 失败小票
	Receipt map[string]any

	hostRequestID string
}

// Error 实现 error 接口（对齐 :99-103：reason 由失败类别映射）。
func (e *OpenJiuwenNativeSearchProviderError) Error() string {
	if kind, ok := e.Receipt["failure_kind"].(string); ok {
		return OpenjiuwenNativeSearchFailureReasonByKind[kind]
	}
	return "provider_unknown"
}

// HostRequestID 宿主请求 ID（对齐 host_request_id 属性）。
func (e *OpenJiuwenNativeSearchProviderError) HostRequestID() string { return e.hostRequestID }

// ArkResponsesWebSearch 配置绑定的 Responses Web Search 门面（:517-568）。
type ArkResponsesWebSearch struct {
	// ProviderId 厂商 ID
	ProviderId string
	// InvocationReceipts 调用小票
	InvocationReceipts []map[string]any

	config *ArkBackgroundConfig
	client ResponsesClient
	sem    chan struct{}
	semMu  sync.Mutex
}

// Close 关闭底层客户端（对齐 aclose）。
func (s *ArkResponsesWebSearch) Close() error { return s.client.Close() }

// Search 执行 Ark web search（对齐 :533-568 __call__）。
func (s *ArkResponsesWebSearch) Search(ctx context.Context, query string, maxResults, timeoutSeconds int) (*ArkWebSearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, &ArkWebSearchContractError{Reason: "search_request_contract_failed"}
	}
	if maxResults < 1 || maxResults > 50 || timeoutSeconds <= 0 {
		return nil, &ArkWebSearchContractError{Reason: "search_request_contract_failed"}
	}
	request := map[string]any{
		"model":          s.config.Model,
		"input":          query,
		"instructions":   SearchInstructions,
		"tools":          []map[string]any{{"type": "web_search", "max_keyword": 1, "limit": maxResults}},
		"tool_choice":    "required",
		"max_tool_calls": 1,
		"store":          false,
		"temperature":    s.config.EffectiveTemperature(),
		"top_p":          s.config.EffectiveTopP(),
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	s.sem <- struct{}{}
	payload, err := func() (map[string]any, error) {
		defer func() { <-s.sem }()
		return s.client.ResponsesCreate(callCtx, request)
	}()
	if err != nil {
		var statusErr *providerStatusError
		if nativeAs(err, &statusErr) {
			receipt := toolNotOpenReceipt(statusErr, s.config.Model)
			if receipt != nil {
				return nil, &ArkWebSearchEntitlementError{Receipt: receipt}
			}
		}
		return nil, err
	}
	result, parseErr := parseArkWebSearchResponse(payload, arkExpectedModel(s.config), query, maxResults)
	if parseErr != nil {
		return nil, parseErr
	}
	s.InvocationReceipts = append(s.InvocationReceipts, result.Receipt)
	return result, nil
}

// arkExpectedModel 期望模型（expected_model or model）。
func arkExpectedModel(config *ArkBackgroundConfig) string {
	if config.ExpectedModel != "" {
		return config.ExpectedModel
	}
	return config.Model
}

// hasDuplicate 判断是否含重复项。
func hasDuplicate(values []string) bool {
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}

// providerErrorCode 厂商错误码（:386-395 _provider_error_code）。
func providerErrorCode(statusErr *providerStatusError) string {
	if statusErr.Body == nil {
		return ""
	}
	nested, _ := statusErr.Body["error"].(map[string]any)
	errorObj := statusErr.Body
	if nested != nil {
		errorObj = nested
	}
	code, _ := errorObj["code"].(string)
	return code
}

// toolNotOpenReceipt ToolNotOpen 权益拒绝小票（:397-419）。
func toolNotOpenReceipt(statusErr *providerStatusError, actualModel string) map[string]any {
	if providerErrorCode(statusErr) != "ToolNotOpen" {
		return nil
	}
	if statusErr.StatusCode < 400 || statusErr.StatusCode >= 500 {
		return nil
	}
	requestID := strings.TrimSpace(statusErr.RequestID)
	if requestID == "" {
		return nil
	}
	return map[string]any{
		"receipt_version":     ArkWebSearchEntitlementReceiptVersion,
		"provider_id":         ArkWebSearchProviderID,
		"actual_model":        actualModel,
		"operation":           "responses.create",
		"tool_type":           "web_search",
		"provider_error_code": "ToolNotOpen",
		"http_status":         statusErr.StatusCode,
		"provider_request_id": requestID,
		"observed_at":         time.Now().UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
	}
}

// nonnegativeInteger 非负整数判定（:431-440 _nonnegative_integer）。
func nonnegativeInteger(value any) (int, bool) {
	if v, ok := value.(int); ok && v >= 0 {
		return v, true
	}
	if v, ok := value.(float64); ok && v == float64(int(v)) && v >= 0 {
		return int(v), true
	}
	return 0, false
}

// requiredText 必填文本（:442-447）。
func requiredText(value any, reason string) (string, error) {
	s, ok := value.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", &ArkWebSearchContractError{Reason: reason}
	}
	return strings.TrimSpace(s), nil
}

// parseArkSearchUsage usage 解析（:449-475 _parse_usage）。
func parseArkSearchUsage(payload map[string]any) (map[string]any, error) {
	usage, _ := payload["usage"].(map[string]any)
	if usage == nil {
		return nil, &ArkWebSearchContractError{Reason: "response_metadata_mismatch"}
	}
	toolUsage, _ := usage["tool_usage"].(map[string]any)
	if toolUsage == nil {
		return nil, &ArkWebSearchContractError{Reason: "response_metadata_mismatch"}
	}
	webSearchCount, ok := nonnegativeInteger(toolUsage["web_search"])
	if !ok || webSearchCount < 1 {
		return nil, &ArkWebSearchContractError{Reason: "response_metadata_mismatch"}
	}
	receipt := map[string]any{
		"tool_usage": map[string]any{"web_search": webSearchCount},
	}
	for _, field := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		if count, ok := nonnegativeInteger(usage[field]); ok {
			receipt[field] = count
		}
	}
	ordered := map[string]any{}
	for _, field := range []string{"input_tokens", "output_tokens", "total_tokens", "tool_usage"} {
		if v, exists := receipt[field]; exists {
			ordered[field] = v
		}
	}
	return ordered, nil
}

// parseURLCitations URL citation 解析（:477-517 _parse_url_citations）。
func parseURLCitations(output []any, maxResults int) ([]map[string]string, error) {
	rows := make([]map[string]string, 0)
	seenURLs := map[string]bool{}
	for _, item := range output {
		itemMap, _ := item.(map[string]any)
		if itemMap == nil || itemMap["type"] != "message" {
			continue
		}
		if itemMap["status"] != "completed" {
			return nil, &ArkWebSearchContractError{Reason: "response_metadata_mismatch"}
		}
		content, _ := itemMap["content"].([]any)
		if content == nil {
			return nil, &ArkWebSearchContractError{Reason: "missing_url_citation"}
		}
		for _, contentItem := range content {
			contentMap, _ := contentItem.(map[string]any)
			if contentMap == nil {
				return nil, &ArkWebSearchContractError{Reason: "missing_url_citation"}
			}
			annotationsRaw, exists := contentMap["annotations"]
			if !exists || annotationsRaw == nil {
				continue
			}
			annotations, _ := annotationsRaw.([]any)
			if annotations == nil {
				return nil, &ArkWebSearchContractError{Reason: "url_citation_contract_failed"}
			}
			for _, annotationRaw := range annotations {
				annotation, _ := annotationRaw.(map[string]any)
				if annotation == nil {
					return nil, &ArkWebSearchContractError{Reason: "url_citation_contract_failed"}
				}
				if annotation["type"] != "url_citation" {
					continue
				}
				rowURL, err := requiredText(annotation["url"], "url_citation_contract_failed")
				if err != nil {
					return nil, err
				}
				parsed, _ := url.Parse(rowURL)
				if parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
					return nil, &ArkWebSearchContractError{Reason: "url_citation_contract_failed"}
				}
				row := map[string]string{"url": rowURL}
				for _, field := range []string{"title", "summary", "site_name", "publish_time", "freshness_info"} {
					if _, exists := annotation[field]; !exists {
						continue
					}
					text, err := requiredText(annotation[field], "url_citation_contract_failed")
					if err != nil {
						return nil, err
					}
					row[field] = text
				}
				if !seenURLs[rowURL] {
					seenURLs[rowURL] = true
					rows = append(rows, row)
				}
			}
		}
	}
	if len(rows) == 0 {
		return nil, &ArkWebSearchContractError{Reason: "missing_url_citation"}
	}
	if len(rows) > maxResults {
		rows = rows[:maxResults]
	}
	return rows, nil
}

// parseArkWebSearchResponse 响应解析（:519-600 _parse_response）。
func parseArkWebSearchResponse(response map[string]any, expectedModel, query string, maxResults int) (*ArkWebSearchResult, error) {
	responseID, _ := response["id"].(string)
	if responseID == "" || response["object"] != "response" || response["status"] != "completed" ||
		response["model"] != expectedModel {
		return nil, &ArkWebSearchContractError{Reason: "response_metadata_mismatch"}
	}
	output, _ := response["output"].([]any)
	if output == nil {
		return nil, &ArkWebSearchContractError{Reason: "response_metadata_mismatch"}
	}
	var searchCalls []map[string]any
	for _, item := range output {
		itemMap, _ := item.(map[string]any)
		if itemMap != nil && itemMap["type"] == "web_search_call" {
			searchCalls = append(searchCalls, itemMap)
		}
	}
	if len(searchCalls) == 0 {
		return nil, &ArkWebSearchContractError{Reason: "missing_web_search_call"}
	}
	if len(searchCalls) != 1 {
		return nil, &ArkWebSearchContractError{Reason: "multiple_web_search_calls"}
	}
	searchCall := searchCalls[0]
	callID, _ := searchCall["id"].(string)
	action, _ := searchCall["action"].(map[string]any)
	if callID == "" || searchCall["status"] != "completed" || action == nil || action["type"] != "search" {
		return nil, &ArkWebSearchContractError{Reason: "web_search_call_metadata_mismatch"}
	}
	actionQuery, _ := action["query"].(string)
	if actionQuery != query {
		return nil, &ArkWebSearchContractError{Reason: "search_query_mismatch"}
	}
	usage, err := parseArkSearchUsage(response)
	if err != nil {
		return nil, err
	}
	rows, err := parseURLCitations(output, maxResults)
	if err != nil {
		return nil, err
	}
	receipt := map[string]any{
		"receipt_version":      ArkWebSearchReceiptVersion,
		"provider_id":          ArkWebSearchProviderID,
		"provider_response_id": responseID,
		"response_object":      "response",
		"response_status":      "completed",
		"actual_model":         expectedModel,
		"web_search_call_id":   callID,
		"action":               map[string]any{"type": "search", "query": query},
		"usage":                usage,
		"result_count":         len(rows),
	}
	return &ArkWebSearchResult{Engine: ArkWebSearchProviderID, Rows: rows, Receipt: receipt}, nil
}

// arkResponsesHTTPClient 默认 Responses 客户端（等价 AsyncOpenAI 的被使用面）。
type arkResponsesHTTPClient struct {
	config *ArkBackgroundConfig
}

// ResponsesCreate POST {base_url}/responses。
func (c *arkResponsesHTTPClient) ResponsesCreate(ctx context.Context, request map[string]any) (map[string]any, error) {
	endpoint := strings.TrimRight(c.config.BaseURL, "/") + "/responses"
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw := make([]byte, 0, 1<<16)
	buf := make([]byte, 16*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if readErr != nil {
			break
		}
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		requestID := resp.Header.Get("x-request-id")
		return nil, &providerStatusError{
			StatusCode: resp.StatusCode,
			RequestID:  requestID,
			Body:       payload,
		}
	}
	if payload == nil {
		return nil, &ArkWebSearchContractError{Reason: "response_metadata_mismatch"}
	}
	return payload, nil
}

// Close 关闭客户端。
func (c *arkResponsesHTTPClient) Close() error { return nil }
