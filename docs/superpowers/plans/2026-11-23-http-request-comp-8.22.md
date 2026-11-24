# 8.22 HTTPRequestComponent 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `core/workflow/components/tool/http/http_request_component.py`（512 行）为 Go 工作流 HTTP 请求节点组件，一比一结构忠实，偏差按 spec D-22-1..6 登记。

**Architecture:** 新包 `internal/openuapclaw/core/workflow/components/tool/http/`，两文件（doc.go + http_request_component.go）。`HTTPRequestComponent`（组合面，ToExecutableFn 槽注入）+ `HTTPRequestExecutable`（8.17 WorkflowComponent 四槽基座，invoke 三步链 processInputs→makeRequest→processResponse）。HTTP 层用 net/http，每轮重试新建 transport（spec 拍板 #3）。

**Tech Stack:** Go net/http + net/url + mime/multipart；`common/security`（SslUtils/UrlUtils）；`common/exception`（StatusComponentToolExecutionError=102000）；httptest 真链测试。

**Spec:** `docs/superpowers/specs/2026-11-23-http-request-comp-8.22-design.md`

---

## 全局纪律（每个 Task 都适用）

1. **注释全中文**（规则 1）；**声明排序**：结构体→枚举→常量→全局变量→导出函数→非导出函数，用分隔注释块（规则 2）——最终文件必须按此序组装（本计划按实施顺序给代码，组装时按声明序归位）
2. **共享 checkout 提交纪律**（2026-11-23 事故教训）：提交前 `git status --porcelain` 检查非本任务 staged 项；**一律用 path-limited 提交** `git commit <paths> -m "..."`，禁 `-a`/`-A`/裸 `git commit`
3. Python 死代码**照搬不优化**：死字段进结构体不消费；`process_inputs` 原地写回怪癖忠实复刻（spec D-22-3）；DIGEST/AWS 认证不实现
4. 全量回归命令：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/openuapclaw/core/workflow/...`（最终 Task 跑全仓）

---

### Task 1: 包地基——doc.go + 枚举 + 配置结构体

**Files:**
- Create: `internal/openuapclaw/core/workflow/components/tool/http/doc.go`
- Create: `internal/openuapclaw/core/workflow/components/tool/http/http_request_component.go`（本 Task 只落枚举与配置段）
- Test: `internal/openuapclaw/core/workflow/components/tool/http/http_request_component_test.go`

- [ ] **Step 1: 写 doc.go**

```go
// Package http 提供工作流 HTTP 请求节点组件（领域八 8.22）。
//
// 一比一对齐 Python openjiuwen/core/workflow/components/tool/http/
// http_request_component.py：HTTPRequestComponent 组合面 + HTTPRequestExecutable
// 执行体（invoke 三步链 processInputs→makeRequest→processResponse）。
// 支持 {{key}} 占位符模板、六类认证枚举（实际实现 basic/bearer/api_key）、
// 五类请求体、响应格式化、三退避重试、响应大小限制、SSL/代理安全工具。
// 已知偏差见 spec docs/superpowers/specs/2026-11-23-http-request-comp-8.22-design.md §5。
//
// 文件目录：
//
//	http/
//	├── doc.go                       # 包文档
//	└── http_request_component.go   # 枚举/配置/执行体/组件
//
// 对应 Python 代码：openjiuwen/core/workflow/components/tool/http/
package http
```

- [ ] **Step 2: 写枚举与配置结构体（含默认构造器）**

按声明序落入 `http_request_component.go`（结构体段→枚举段→常量段→导出函数段；执行体/组件结构体本 Task 先占位声明）：

```go
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

// NewHttpAuthConfig 默认认证配置（pydantic 默认值语义）
func NewHttpAuthConfig() *HttpAuthConfig {
	return &HttpAuthConfig{Type: HttpAuthTypeNone, InLocation: "header", Name: "Authorization"}
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
	BinaryData    *string // base64 编码引用
	TextData      *string
}

// NewHttpRequestBodyConfig 默认请求体配置
func NewHttpRequestBodyConfig() *HttpRequestBodyConfig {
	return &HttpRequestBodyConfig{ContentType: HttpContentTypeJSON}
}

// HttpResponseHandlingConfig 响应处理配置（Python :80-87 一比一）
type HttpResponseHandlingConfig struct {
	ResponseFormat          HttpResponseFormat
	ResponseCodeSuccessCodes []int
	ResponseCodeFailureCodes []int
	ResponseMode            string // full|on-success|on-error
	ResponseDataProperty    *string
	MaxRedirects            int    // 死字段：Python 定义未消费（spec §4）
	ThrowOnHTTPError        bool   // 死字段
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

// HttpAdvancedOptionsConfig 高级选项（Python :90-99 一比一；仅 IgnoreSSLIssues
// 与 Proxy 被消费，其余死字段照搬，spec §4）
type HttpAdvancedOptionsConfig struct {
	FollowRedirect             bool // 死字段
	IgnoreSSLIssues            bool
	Proxy                      string
	Timeout                    int               // 死字段（毫秒，默认 10000；消费的是顶层 timeout 秒）
	DisableCompression         bool              // 死字段
	DisableFollowTrackRedirect bool              // 死字段
	MaxBodyLength              int               // 死字段（默认 1MB）
	UseStream                  bool              // 死字段
	ProxyHeader                map[string]string // 死字段
}

// NewHttpAdvancedOptionsConfig 默认高级选项
func NewHttpAdvancedOptionsConfig() *HttpAdvancedOptionsConfig {
	return &HttpAdvancedOptionsConfig{FollowRedirect: true, Timeout: 10000, MaxBodyLength: 1048576}
}

// HttpRetryConfig 重试配置（Python :102-107 一比一）
type HttpRetryConfig struct {
	Enabled           bool
	MaxRetries        int
	RetryOnStatusCodes []int
	RetryDelay        int    // 毫秒
	BackoffType       string // fixed|linear|exponential
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

// HttpRateLimitConfig 限流配置（Python :110-113 一比一；零消费死配置，spec §4）
type HttpRateLimitConfig struct {
	Enabled        bool
	RequestsPerUnit int
	Unit           string // second|minute|hour
}

// NewHttpRateLimitConfig 默认限流配置
func NewHttpRateLimitConfig() *HttpRateLimitConfig {
	return &HttpRateLimitConfig{RequestsPerUnit: 1, Unit: "second"}
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

// HTTPRequestExecutable 执行体（Python :136- 一比一；Task 6 补方法）
type HTTPRequestExecutable struct {
	components.WorkflowComponent
	// cfg 组件配置（Python self.config）
	cfg *HttpComponentConfig
	// requestParams 快捷引用（Python self.request_params）
	requestParams *HttpRequestParamConfig
}

// HTTPRequestComponent 组件组合面（Python :494- 一比一；Task 6 补方法）
type HTTPRequestComponent struct {
	components.BaseComponentComposable
	// config 组件配置（Python self.__config 私有属性怪癖 → Go 非导出字段）
	config *HttpComponentConfig
	// executable 懒构造缓存（Python self._executable）
	executable *HTTPRequestExecutable
}

// HttpComponentConfig 组件配置（Python :131-133 一比一）
// 注：Python 嵌 WorkflowComponentMetadata —— Go 用 components 包同款（8.18 先例：
// LLMComponentConfig 嵌 ComponentConfig）；Metadata 字段照搬。
type HttpComponentConfig struct {
	// RequestParams 请求参数
	RequestParams *HttpRequestParamConfig
	// Metadata 组件元数据（Python Optional[WorkflowComponentMetadata]）
	Metadata any
}

// NewHttpComponentConfig 默认组件配置
func NewHttpComponentConfig() *HttpComponentConfig {
	return &HttpComponentConfig{RequestParams: NewHttpRequestParamConfig()}
}
```

（import 段：`components "…/core/workflow/components"`，实施时补全模块前缀。）

- [ ] **Step 3: 写枚举与常量段（声明序在结构体后）**

```go
// ──────────────────────────── 枚举 ────────────────────────────

// HttpAuthType 认证类型（Python str Enum :28-34 六成员全量）
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
	// HttpContentTypeAuto 自动（Python 定义未消费）
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
	// HttpResponseFormatBuffer 缓冲（Python 定义未消费）
	HttpResponseFormatBuffer HttpResponseFormat = "buffer"
)
```

```go
// ──────────────────────────── 常量 ────────────────────────────

// sslVerifyEnv / sslCertEnv / sslTriggerValues SSL 环境开关
// （Python SslUtils.get_ssl_config("HTTP_SSL_VERIFY", "HTTP_SSL_CERT", ["false"], …) :273）
const (
	sslVerifyEnv    = "HTTP_SSL_VERIFY"
	sslCertEnv      = "HTTP_SSL_CERT"
	sslTriggerValue = "false"
)

// chunkSize 响应分块读大小（Python iter_chunked(8192) :294）
const chunkSize = 8192
```

```go
// ──────────────────────────── 全局变量 ────────────────────────────

// textContentTypes 文本类 content-type 白名单（Python 类常量 TEXT_CONTENT_TYPES :137-138）
var textContentTypes = []string{
	"text/", "application/json", "application/xml",
	"application/javascript", "application/xhtml+xml",
}
```

- [ ] **Step 4: 写测试（枚举值 + 默认值断言）**

```go
package http

import "testing"

// TestEnums_全量映射 枚举成员与 Python 逐字对齐（规则 9.2.4 不裁剪）
func TestEnums_全量映射(t *testing.T) {
	if HttpAuthTypeNone != "none" || HttpAuthTypeBasic != "basic" ||
		HttpAuthTypeBearer != "bearer" || HttpAuthTypeAPIKey != "api_key" ||
		HttpAuthTypeDigest != "digest" || HttpAuthTypeAWS != "aws" {
		t.Fatal("HttpAuthType 成员值漂移")
	}
	if HttpContentTypeAuto != "auto" || HttpResponseFormatBuffer != "buffer" {
		t.Fatal("HttpContentType/HttpResponseFormat 成员值漂移")
	}
}

// TestDefaults_pydantic对齐 默认值逐项对齐 Python pydantic default
func TestDefaults_pydantic对齐(t *testing.T) {
	c := NewHttpComponentConfig()
	rp := c.RequestParams
	if rp.Method != "GET" || rp.Timeout != 60.0 || rp.MaxResponseByteSize != 10*1024*1024 {
		t.Fatalf("顶层默认漂移: %+v", rp)
	}
	rh := rp.ResponseHandling
	if rh.ResponseFormat != HttpResponseFormatAutodetect || rh.ResponseMode != "full" ||
		len(rh.ResponseCodeSuccessCodes) != 4 || rh.ResponseCodeSuccessCodes[0] != 200 ||
		rh.MaxRedirects != 21 || !rh.ThrowOnHTTPError {
		t.Fatalf("response_handling 默认漂移: %+v", rh)
	}
	ao := rp.AdvancedOptions
	if !ao.FollowRedirect || ao.IgnoreSSLIssues || ao.Timeout != 10000 || ao.MaxBodyLength != 1048576 {
		t.Fatalf("advanced_options 默认漂移: %+v", ao)
	}
	rc := rp.RetryConfig
	if rc.Enabled || rc.MaxRetries != 3 || rc.RetryDelay != 1000 || rc.BackoffType != "exponential" ||
		len(rc.RetryOnStatusCodes) != 5 {
		t.Fatalf("retry_config 默认漂移: %+v", rc)
	}
	rl := rp.RateLimitConfig
	if rl.Enabled || rl.RequestsPerUnit != 1 || rl.Unit != "second" {
		t.Fatalf("rate_limit_config 默认漂移: %+v", rl)
	}
	auth := NewHttpAuthConfig()
	if auth.Type != HttpAuthTypeNone || auth.InLocation != "header" || auth.Name != "Authorization" {
		t.Fatalf("auth 默认漂移: %+v", auth)
	}
}
```

- [ ] **Step 5: 编译 + 跑测试**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: PASS（2 例）

- [ ] **Step 6: 提交（path-limited）**

```bash
git commit internal/openuapclaw/core/workflow/components/tool/http/doc.go \
  internal/openuapclaw/core/workflow/components/tool/http/http_request_component.go \
  internal/openuapclaw/core/workflow/components/tool/http/http_request_component_test.go \
  -m "feat(http): 8.22 HTTP 组件包地基——枚举/配置结构体/默认值（Task1）"
```

---

### Task 2: 占位符助手 + processInputs

**Files:**
- Modify: `internal/openuapclaw/core/workflow/components/tool/http/http_request_component.go`（追加导出段）
- Test: 同文件 `_test.go` 追加

- [ ] **Step 1: 写失败测试**

```go
// TestResolvePlaceholders_字面替换 {{key}} 替换 + str(value) 语义
func TestResolvePlaceholders_字面替换(t *testing.T) {
	inputs := map[string]any{"host": "example.com", "port": 8080}
	got := resolvePlaceholders("http://{{host}}:{{port}}/x", inputs)
	if got != "http://example.com:8080/x" {
		t.Fatalf("占位符替换失败: %q", got)
	}
	// 未知 key 保留原样（Python replace 无匹配不动）
	if resolvePlaceholders("{{missing}}", inputs) != "{{missing}}" {
		t.Fatal("未知占位符应保留原样")
	}
}

// TestResolveTemplateToDict_解析失败回空 dict（Python ast.literal_eval except 分支）
func TestResolveTemplateToDict_解析失败回空(t *testing.T) {
	inputs := map[string]any{"k": "v"}
	if got := resolveTemplateToDict(`{"a": "{{k}}"}`, inputs); len(got) != 1 || got["a"] != "v" {
		t.Fatalf("模板 dict 解析失败: %v", got)
	}
	// 非 dict / 语法错误 → 空 map
	if got := resolveTemplateToDict("not-a-dict {", inputs); len(got) != 0 {
		t.Fatalf("解析失败应回空: %v", got)
	}
	if got := resolveTemplateToDict(`["list"]`, inputs); len(got) != 0 {
		t.Fatalf("非 dict 应回空: %v", got)
	}
}

// TestProcessInputs_合并语义 五段合并逐项断言（Python :190-238 步骤序）
func TestProcessInputs_合并语义(t *testing.T) {
	cfgJSON := `{"q": "{{name}}"}`
	body := NewHttpRequestBodyConfig()
	body.JSONData = cfgJSON
	auth := NewHttpAuthConfig()
	rp := NewHttpRequestParamConfig()
	rp.URL = "http://{{host}}/api"
	rp.Headers = map[string]string{"X-Base": "b"}
	rp.QueryParameters = map[string]any{"page": "{{p}}", "n": 1}
	rp.Body = body
	rp.Authentication = auth
	e := NewHTTPRequestExecutable(&HttpComponentConfig{RequestParams: rp})

	inputs := map[string]any{
		"host": "h1", "name": "n1", "p": "2",
		"headers":         map[string]any{"X-Extra": "e"},
		"query_parameters": map[string]any{"page": "9"},
	}
	got, err := e.ProcessInputs(inputs)
	if err != nil {
		t.Fatalf("ProcessInputs 失败: %v", err)
	}
	if got["url"] != "http://h1/api" || got["method"] != "GET" {
		t.Fatalf("url/method 漂移: %v", got)
	}
	h := got["headers"].(map[string]string)
	if h["X-Base"] != "b" || h["X-Extra"] != "e" {
		t.Fatalf("headers 合并漂移: %v", h) // config 先、inputs 覆盖后
	}
	q := got["query_parameters"].(map[string]any)
	if q["page"] != "9" || q["n"] != 1 {
		t.Fatalf("query 合并漂移: %v", q)
	}
	// 原地写回断言（D-22-3）：config 的 json_data 已从 str 变 dict
	if _, isStr := body.JSONData.(string); isStr {
		t.Fatal("json_data 应被原地写回为 dict（Python 怪癖 D-22-3）")
	}
	if m, ok := body.JSONData.(map[string]any); !ok || m["q"] != "n1" {
		t.Fatalf("json_data 解析内容漂移: %v", body.JSONData)
	}
	if got["authentication"] != auth {
		t.Fatal("config 有 authentication 应直接透传引用")
	}
}

// TestProcessInputs_无配置回落inputs body/authentication 缺 config 时取 inputs
func TestProcessInputs_无配置回落inputs(t *testing.T) {
	rp := NewHttpRequestParamConfig()
	rp.URL = "http://h"
	rp.Method = "post"
	e := NewHTTPRequestExecutable(&HttpComponentConfig{RequestParams: rp})
	body := map[string]any{"a": 1}
	authD := map[string]any{"type": "basic"}
	got, err := e.ProcessInputs(map[string]any{"body": body, "authentication": authD})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	// method 大写化（Python :202-203）
	if got["method"] != "POST" {
		t.Fatalf("method 应大写: %v", got["method"])
	}
	if got["body"] != body || got["authentication"] != authD {
		t.Fatalf("inputs 回落漂移: %v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败（符号未定义）**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: FAIL undefined: resolvePlaceholders

- [ ] **Step 3: 实现（导出函数段）**

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// NewHTTPRequestExecutable 构造执行体（Python __init__ :140-143 一比一）
func NewHTTPRequestExecutable(componentConfig *HttpComponentConfig) *HTTPRequestExecutable {
	e := &HTTPRequestExecutable{cfg: componentConfig, requestParams: componentConfig.RequestParams}
	// 四槽装配（8.20 槽优先铁律）：invoke 槽必装；stream/collect/transform
	// Python 为 yield/return invoke 委托单帧（:155-168），Task 6 装配
	e.InvokeFn = e.invoke
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
	if m, ok := inputs["method"]; ok && m != nil {
		method = m.(string)
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
			headers[k] = pythonsem.PyRepr(v)
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
```

非导出助手段：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// resolvePlaceholders {{key}} 字面替换（Python _resolve_placeholders :170-176；
// 值经 PyRepr 对齐 Python str(value)）
func resolvePlaceholders(template string, inputs map[string]any) string {
	for key, value := range inputs {
		placeholder := "{{" + key + "}}"
		template = strings.ReplaceAll(template, placeholder, pythonsem.PyRepr(value))
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
		out[k] = pythonsem.PyRepr(v)
	}
	return out
}
```

import 补：`encoding/json`、`strings`、`gopkg.in/yaml.v3`（或仓内既有 yaml 包——实施时 `grep yaml` 用既有依赖）、`pythonsem`。

- [ ] **Step 4: 跑测试通过**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: PASS（6 例）

- [ ] **Step 5: 提交（path-limited）**

```bash
git commit internal/openuapclaw/core/workflow/components/tool/http/http_request_component.go \
  internal/openuapclaw/core/workflow/components/tool/http/http_request_component_test.go \
  -m "feat(http): 8.22 占位符助手+ProcessInputs 五段合并（Task2，含 D-22-3 原地写回）"
```

---

### Task 3: applyAuthentication + prepareRequestBody + calculateRetryDelay

**Files:**
- Modify: 同上两文件

- [ ] **Step 1: 写失败测试**

```go
// TestApplyAuthentication_四类认证 basic/bearer/api_key/none 逐类断言
func TestApplyAuthentication_四类认证(t *testing.T) {
	e := &HTTPRequestExecutable{}
	// none / nil → 原头返回
	h := e.ApplyAuthentication(map[string]string{"A": "1"}, nil)
	if h["A"] != "1" || len(h) != 1 {
		t.Fatal("nil auth 应原头返回")
	}
	// basic
	a := NewHttpAuthConfig()
	u, p := "user", "pass"
	a.Type, a.Username, a.Password = HttpAuthTypeBasic, u, p
	h = e.ApplyAuthentication(map[string]string{}, a)
	// base64("user:pass") = dXNlcjpwYXNz
	if h["Authorization"] != "Basic dXNlcjpwYXNz" {
		t.Fatalf("basic 漂移: %v", h)
	}
	// basic 用户名缺失 → 不注入（Python :428 if username and password）
	a2 := NewHttpAuthConfig()
	a2.Type, a2.Password = HttpAuthTypeBasic, p
	h = e.ApplyAuthentication(map[string]string{}, a2)
	if len(h) != 0 {
		t.Fatalf("basic 缺 username 应不注入: %v", h)
	}
	// bearer
	a3 := NewHttpAuthConfig()
	a3.Type, a3.Token = HttpAuthTypeBearer, "tk"
	if h := e.ApplyAuthentication(map[string]string{}, a3); h["Authorization"] != "Bearer tk" {
		t.Fatalf("bearer 漂移: %v", h)
	}
	// api_key header 位置
	a4 := NewHttpAuthConfig()
	a4.Type, a4.APIKey, a4.InLocation, a4.Name = HttpAuthTypeAPIKey, "key", "header", "X-Key"
	if h := e.ApplyAuthentication(map[string]string{}, a4); h["X-Key"] != "key" {
		t.Fatalf("api_key header 漂移: %v", h)
	}
	// api_key query 位置 → 不注入（Python :439-441 仅 header 分支）
	a5 := NewHttpAuthConfig()
	a5.Type, a5.APIKey, a5.InLocation = HttpAuthTypeAPIKey, "key", "query"
	if h := e.ApplyAuthentication(map[string]string{}, a5); len(h) != 0 {
		t.Fatalf("api_key query 应不注入: %v", h)
	}
	// digest → 不注入（Python 未实现）
	a6 := NewHttpAuthConfig()
	a6.Type = HttpAuthTypeDigest
	if h := e.ApplyAuthentication(map[string]string{}, a6); len(h) != 0 {
		t.Fatalf("digest 应不注入: %v", h)
	}
}

// TestPrepareRequestBody_五类编码 各 ContentType 分支
func TestPrepareRequestBody_五类编码(t *testing.T) {
	e := &HTTPRequestExecutable{}
	// nil / 空 → (nil, "")
	if b, ct := e.PrepareRequestBody(nil); b != nil || ct != "" {
		t.Fatalf("nil body 应空: %v %q", b, ct)
	}
	// JSON
	bc := NewHttpRequestBodyConfig()
	bc.ContentType = HttpContentTypeJSON
	bc.JSONData = map[string]any{"a": 1}
	b, ct := e.PrepareRequestBody(bc)
	if ct != "application/json" || string(b.([]byte)) != `{"a":1}` {
		t.Fatalf("JSON 编码漂移: %q %q", b, ct)
	}
	// FORM：urlencoded 载荷 + urlencoded 头（D-22-2 修正偏差，契约钉住）
	bc2 := NewHttpRequestBodyConfig()
	bc2.ContentType = HttpContentTypeForm
	bc2.FormData = map[string]any{"x": "1", "y": "a b"}
	b2, ct2 := e.PrepareRequestBody(bc2)
	if ct2 != "application/x-www-form-urlencoded" {
		t.Fatalf("FORM 头漂移: %q", ct2)
	}
	if b2.(string) != "x=1&y=a+b" && b2.(string) != "y=a+b&x=1" {
		t.Fatalf("FORM urlencoded 载荷漂移: %q", b2)
	}
	// MULTIPART_FORM
	bc3 := NewHttpRequestBodyConfig()
	bc3.ContentType = HttpContentTypeMultipartForm
	bc3.MultipartForm = map[string]any{"f": "v"}
	b3, ct3 := e.PrepareRequestBody(bc3)
	if ct3 != "multipart/form-data" {
		t.Fatalf("MULTIPART 头漂移: %q", ct3)
	}
	if !strings.Contains(b3.(string), `name="f"`) {
		t.Fatalf("MULTIPART 载荷缺字段: %q", b3)
	}
	// TEXT
	td := "hi"
	bc4 := NewHttpRequestBodyConfig()
	bc4.ContentType = HttpContentTypeText
	bc4.TextData = &td
	if b4, ct4 := e.PrepareRequestBody(bc4); ct4 != "text/plain" || b4.(string) != "hi" {
		t.Fatalf("TEXT 漂移: %v %q", b4, ct4)
	}
	// BINARY：base64 解码
	enc := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	bc5 := NewHttpRequestBodyConfig()
	bc5.ContentType = HttpContentTypeBinary
	bc5.BinaryData = &enc
	b5, ct5 := e.PrepareRequestBody(bc5)
	if ct5 != "application/octet-stream" || !bytes.Equal(b5.([]byte), []byte{1, 2, 3}) {
		t.Fatalf("BINARY 漂移: %v %q", b5, ct5)
	}
	// 有 config 但数据字段空 → (nil, "")（Python 各 elif 条件带 xxx_data 非空判断）
	bc6 := NewHttpRequestBodyConfig()
	bc6.ContentType = HttpContentTypeJSON
	if b6, ct6 := e.PrepareRequestBody(bc6); b6 != nil || ct6 != "" {
		t.Fatalf("JSON 无数据应空: %v %q", b6, ct6)
	}
}

// TestCalculateRetryDelay_三退避 fixed/linear/exponential
func TestCalculateRetryDelay_三退避(t *testing.T) {
	e := &HTTPRequestExecutable{requestParams: NewHttpRequestParamConfig()}
	rc := e.requestParams.RetryConfig
	rc.RetryDelay = 1000
	rc.BackoffType = "fixed"
	if d := e.CalculateRetryDelay(2); d != 1.0 {
		t.Fatalf("fixed 漂移: %v", d)
	}
	rc.BackoffType = "linear"
	if d := e.CalculateRetryDelay(2); d != 3.0 {
		t.Fatalf("linear 漂移: %v", d)
	}
	rc.BackoffType = "exponential"
	if d := e.CalculateRetryDelay(2); d != 4.0 {
		t.Fatalf("exponential 漂移: %v", d)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: FAIL undefined 方法

- [ ] **Step 3: 实现**

```go
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
// inputs 回落的 map 形态 body 见 MakeRequest 的 falsy 判定说明。
func (e *HTTPRequestExecutable) PrepareRequestBody(bodyConfig any) (any, string) {
	bc, ok := bodyConfig.(*HttpRequestBodyConfig)
	if !ok || bc == nil {
		// Python `if not body_config` 对空 dict 同样短路返回（:452-453）
		return nil, ""
	}
	switch bc.ContentType {
	case HttpContentTypeJSON:
		if bc.JSONData != nil {
			return []byte(pythonsem.PyJSONDumps(bc.JSONData)), "application/json"
		}
	case HttpContentTypeForm:
		if len(bc.FormData) > 0 {
			// D-22-2：Python aiohttp.FormData 出 multipart 载荷 + urlencoded 头不一致；
			// Go 修正为 urlencoded 载荷 + urlencoded 头（spec 拍板 #4）
			vals := url.Values{}
			for k, v := range bc.FormData {
				vals.Set(k, pythonsem.PyRepr(v))
			}
			return vals.Encode(), "application/x-www-form-urlencoded"
		}
	case HttpContentTypeMultipartForm:
		if len(bc.MultipartForm) > 0 {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			for k, v := range bc.MultipartForm {
				_ = w.WriteField(k, pythonsem.PyRepr(v))
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
```

注：`pythonsem.PyJSONDumps` 若不存在（实施时 grep 确认），用 `json.Marshal` 等价（Python json.dumps 与 Go Marshal 对 map 键序处理一致——Go 排序键，Python 不排序，登记进 spec §5 备查即可，不新增助手）；已存在则用助手。

- [ ] **Step 4: 跑测试通过**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: PASS

- [ ] **Step 5: 提交（path-limited）**

```bash
git commit internal/openuapclaw/core/workflow/components/tool/http/http_request_component.go \
  internal/openuapclaw/core/workflow/components/tool/http/http_request_component_test.go \
  -m "feat(http): 8.22 认证/请求体编码/重试退避（Task3，D-22-2 FORM urlencoded 修正）"
```

---

### Task 4: makeRequest（重试循环 + 分块读 + 大小限制 + SSL/代理）

**Files:**
- Modify: 同上两文件

- [ ] **Step 1: 写失败测试（httptest 真链）**

```go
// TestMakeRequest_GET基本链 GET + 头/查询透传 + 结果 dict 形状
func TestMakeRequest_GET基本链(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "v" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL
	rp.QueryParameters = map[string]any{"q": "v"}
	rp.Timeout = 5
	e := &HTTPRequestExecutable{requestParams: rp}
	params := map[string]any{
		"url": srv.URL, "method": "GET", "headers": map[string]string{},
		"query_parameters": map[string]any{"q": "v"},
		"body":             map[string]any{}, "authentication": map[string]any{},
	}
	res, err := e.MakeRequest(params)
	if err != nil {
		t.Fatalf("MakeRequest 失败: %v", err)
	}
	if res["status_code"] != 200 || res["reason"] != "200 OK" {
		t.Fatalf("status/reason 漂移: %v", res)
	}
	// 文本型 content-type → content 为 string（str 形态）
	if _, ok := res["content"].(string); !ok {
		t.Fatalf("文本响应 content 应为 string: %T", res["content"])
	}
	if res["url"] == "" || res["headers"] == nil {
		t.Fatal("url/headers 缺失")
	}
}

// TestMakeRequest_POST写体 POST + JSON 体 + Content-Type 头
func TestMakeRequest_POST写体(t *testing.T) {
	var gotBody, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody, gotCT = string(raw), r.Header.Get("Content-Type")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL
	rp.Timeout = 5
	e := &HTTPRequestExecutable{requestParams: rp}
	params := map[string]any{
		"url": srv.URL, "method": "POST", "headers": map[string]string{},
		"query_parameters": map[string]any{}, "authentication": map[string]any{},
		"body": func() any {
			bc := NewHttpRequestBodyConfig()
			bc.JSONData = map[string]any{"a": 1}
			return bc
		}(),
	}
	if _, err := e.MakeRequest(params); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if gotBody != `{"a":1}` {
		t.Fatalf("请求体漂移: %q", gotBody)
	}
	if gotCT != "application/json" {
		t.Fatalf("Content-Type 漂移: %q", gotCT)
	}
}

// TestMakeRequest_响应超限重试后102000 超限异常走重试路径再包装（Python :298-303
// raise 在 try 内 → except → 重试 → 耗尽后 102000 包装）
func TestMakeRequest_响应超限重试后102000(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write(make([]byte, 100))
	}))
	defer srv.Close()
	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL
	rp.Timeout = 5
	rp.MaxResponseByteSize = 50
	rp.RetryConfig.Enabled = true
	rp.RetryConfig.MaxRetries = 1
	rp.RetryConfig.RetryDelay = 1
	rp.RetryConfig.BackoffType = "fixed"
	e := &HTTPRequestExecutable{requestParams: rp}
	params := map[string]any{
		"url": srv.URL, "method": "GET", "headers": map[string]string{},
		"query_parameters": map[string]any{}, "body": map[string]any{},
		"authentication": map[string]any{},
	}
	_, err := e.MakeRequest(params)
	if err == nil {
		t.Fatal("超限应报错")
	}
	if !errors.As(err, new(*exception.BaseError)) || err.(*exception.BaseError).Code() != 102000 {
		t.Fatalf("应 102000: %v", err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("超限应触发 1 次重试（共 2 次请求）: %d", hits)
	}
}

// TestMakeRequest_状态码重试耗尽 429 命中重试表 → 重试后成功
func TestMakeRequest_状态码重试耗尽(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL
	rp.Timeout = 5
	rp.RetryConfig.Enabled = true
	rp.RetryConfig.MaxRetries = 2
	rp.RetryConfig.RetryDelay = 1
	rp.RetryConfig.BackoffType = "fixed"
	e := &HTTPRequestExecutable{requestParams: rp}
	params := map[string]any{
		"url": srv.URL, "method": "GET", "headers": map[string]string{},
		"query_parameters": map[string]any{}, "body": map[string]any{},
		"authentication": map[string]any{},
	}
	res, err := e.MakeRequest(params)
	if err != nil || res["status_code"] != 200 {
		t.Fatalf("重试后应成功: %v %v", res, err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("应恰好重试一次: %d", hits)
	}
}

// TestMakeRequest_网络错误重试耗尽102000 连接拒绝 → 重试耗尽 → 102000
func TestMakeRequest_网络错误重试耗尽102000(t *testing.T) {
	rp := NewHttpRequestParamConfig()
	rp.URL = "http://127.0.0.1:1/nope" // 保留端口必拒绝
	rp.Timeout = 5
	rp.RetryConfig.Enabled = true
	rp.RetryConfig.MaxRetries = 1
	rp.RetryConfig.RetryDelay = 1
	rp.RetryConfig.BackoffType = "fixed"
	e := &HTTPRequestExecutable{requestParams: rp}
	params := map[string]any{
		"url": rp.URL, "method": "GET", "headers": map[string]string{},
		"query_parameters": map[string]any{}, "body": map[string]any{},
		"authentication": map[string]any{},
	}
	_, err := e.MakeRequest(params)
	if err == nil || err.(*exception.BaseError).Code() != 102000 {
		t.Fatalf("网络错误耗尽应 102000: %v", err)
	}
}

// TestMakeRequest_二进制分型 非文本 content-type → content 为 []byte（D-22-1 钉住）
func TestMakeRequest_二进制分型(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte{0, 1, 2})
	}))
	defer srv.Close()
	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL
	rp.Timeout = 5
	e := &HTTPRequestExecutable{requestParams: rp}
	params := map[string]any{
		"url": srv.URL, "method": "GET", "headers": map[string]string{},
		"query_parameters": map[string]any{}, "body": map[string]any{},
		"authentication": map[string]any{},
	}
	res, err := e.MakeRequest(params)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if _, ok := res["content"].([]byte); !ok {
		t.Fatalf("二进制 content 应为 []byte: %T", res["content"])
	}
}

// TestMakeRequest_从不跟随重定向 allow_redirects=False 语义钉住
func TestMakeRequest_从不跟随重定向(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/next")
		w.WriteHeader(302)
	}))
	defer srv.Close()
	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL
	rp.Timeout = 5
	e := &HTTPRequestExecutable{requestParams: rp}
	params := map[string]any{
		"url": srv.URL, "method": "GET", "headers": map[string]string{},
		"query_parameters": map[string]any{}, "body": map[string]any{},
		"authentication": map[string]any{},
	}
	res, err := e.MakeRequest(params)
	if err != nil || res["status_code"] != 302 {
		t.Fatalf("302 应原样返回不跟随: %v %v", res, err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: FAIL undefined: MakeRequest

- [ ] **Step 3: 实现 MakeRequest**

```go
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
	var requestBody any
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
		transport := &http.Transport{
			TLSClientConfig: tlsCfg,
			Proxy:           nil,
		}
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
			q.Set(k, pythonsem.PyRepr(v))
		}
		req, rerr := http.NewRequest(method, targetURL, bodyReader)
		if rerr != nil {
			return nil, exception.BuildError(exception.StatusComponentToolExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("HTTP request failed: %v", rerr)))
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, rerr := client.Do(req)
		if rerr != nil {
			// 异常路径：重试判定（:337-352）
			if e.requestParams.RetryConfig.Enabled && retryCount < maxRetries {
				time.Sleep(time.Duration(e.CalculateRetryDelay(retryCount) * float64(time.Second)))
				retryCount++
				continue
			}
			return nil, exception.BuildError(exception.StatusComponentToolExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("HTTP request failed: %v", rerr)))
		}

		// 分块读 + 超限（:293-303）。注意 Python raise 在 try 内 → 走 except
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
						"Response size (%d bytes) exceeds maximum allowed size \\n                                        (%d bytes)",
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

		// 响应头 dict 化（:317）
		respHeaders := map[string]string{}
		for k, vs := range resp.Header {
			respHeaders[strings.ToLower(k)] = strings.Join(vs, ",")
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
```

非导出助手（追加非导出段）：

```go
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
		return strings.NewReader(pythonsem.PyRepr(b))
	}
}
```

import 补：`bytes`、`crypto/tls`、`errors`、`fmt`、`io`、`net/http`、`net/http/httptest`（仅测试）、`net/url`、`time`、`math`、`security "…/internal/common/security"`、`exception "…/internal/common/exception"`。

- [ ] **Step 4: 跑测试通过**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/ -count=1`
Expected: PASS（全部用例）

- [ ] **Step 5: 提交（path-limited）**

```bash
git commit internal/openuapclaw/core/workflow/components/tool/http/http_request_component.go \
  internal/openuapclaw/core/workflow/components/tool/http/http_request_component_test.go \
  -m "feat(http): 8.22 MakeRequest 重试循环/分块读/SSL 代理/重定向禁跟随（Task4）"
```

---

### Task 5: processResponse

**Files:**
- Modify: 同上两文件

- [ ] **Step 1: 写失败测试**

```go
// TestProcessResponse_autodetect三分支 json/text/其余判型
func TestProcessResponse_autodetect三分支(t *testing.T) {
	e := &HTTPRequestExecutable{requestParams: NewHttpRequestParamConfig()}
	rh := e.requestParams.ResponseHandling
	// json
	rh.ResponseFormat = HttpResponseFormatAutodetect
	out := e.ProcessResponse(map[string]any{
		"status_code": 200, "headers": map[string]string{"content-type": "application/json"},
		"content": `{"a":1}`, "url": "u",
	})
	if out["body"] == nil || out["statusCode"] != 200 || out["ok"] != true || out["url"] != "u" {
		t.Fatalf("json autodetect 漂移: %v", out)
	}
	if m, ok := out["body"].(map[string]any); !ok || m["a"] != float64(1) {
		t.Fatalf("json body 应解析为 map: %v", out["body"])
	}
	// text
	out = e.ProcessResponse(map[string]any{
		"status_code": 200, "headers": map[string]string{"content-type": "text/plain"},
		"content": "hi", "url": "u",
	})
	if out["body"] != "hi" {
		t.Fatalf("text body 漂移: %v", out)
	}
	// 其余 → BINARY pass（bytes/string 原样）
	out = e.ProcessResponse(map[string]any{
		"status_code": 200, "headers": map[string]string{"content-type": "application/octet-stream"},
		"content": []byte{1}, "url": "u",
	})
	if b, ok := out["body"].([]byte); !ok || len(b) != 1 {
		t.Fatalf("binary body 应原样: %v", out)
	}
}

// TestProcessResponse_码判定矩阵 success/failure/ok 组合
func TestProcessResponse_码判定矩阵(t *testing.T) {
	e := &HTTPRequestExecutable{requestParams: NewHttpRequestParamConfig()}
	rh := e.requestParams.ResponseHandling
	mkResp := func(code int) map[string]any {
		return map[string]any{"status_code": code,
			"headers": map[string]string{"content-type": "text/plain"}, "content": "x", "url": "u"}
	}
	// 2xx + 默认 success_codes → ok=true
	if out := e.ProcessResponse(mkResp(204)); out["ok"] != true {
		t.Fatal("204 应成功")
	}
	// 4xx → ok=false
	if out := e.ProcessResponse(mkResp(404)); out["ok"] != false {
		t.Fatal("404 应失败")
	}
	// failure_codes 显式含 302 → 302 判失败
	rh.ResponseCodeFailureCodes = []int{302}
	if out := e.ProcessResponse(mkResp(302)); out["ok"] != false {
		t.Fatal("302 命中 failure_codes 应失败")
	}
	rh.ResponseCodeFailureCodes = []int{}
	// success_codes 收窄为 [200] → 204 非 success
	rh.ResponseCodeSuccessCodes = []int{200}
	if out := e.ProcessResponse(mkResp(204)); out["ok"] != false {
		t.Fatal("204 出收窄 success_codes 应失败")
	}
}

// TestProcessResponse_JSON失败透传 json.loads 失败 → 原文（Python :388-389）
func TestProcessResponse_JSON失败透传(t *testing.T) {
	e := &HTTPRequestExecutable{requestParams: NewHttpRequestParamConfig()}
	rh := e.requestParams.ResponseHandling
	rh.ResponseFormat = HttpResponseFormatJSON
	out := e.ProcessResponse(map[string]any{
		"status_code": 200, "headers": map[string]string{}, "content": "not-json{", "url": "u",
	})
	if out["body"] != "not-json{" {
		t.Fatalf("JSON 解析失败应原文透传: %v", out)
	}
}

// TestProcessResponse_property抽取与mode三分支 data_property + response_mode
func TestProcessResponse_property抽取与mode三分支(t *testing.T) {
	e := &HTTPRequestExecutable{requestParams: NewHttpRequestParamConfig()}
	rh := e.requestParams.ResponseHandling
	rh.ResponseFormat = HttpResponseFormatJSON
	prop := "data"
	rh.ResponseDataProperty = &prop
	out := e.ProcessResponse(map[string]any{
		"status_code": 200, "headers": map[string]string{"content-type": "application/json"},
		"content": `{"data":{"x":1}}`, "url": "u",
	})
	if m, ok := out["body"].(map[string]any); !ok || m["x"] != float64(1) {
		t.Fatalf("property 抽取漂移: %v", out)
	}
	// miss 回原值（Python .get(prop_path, parsed_content)）
	rh.ResponseDataProperty = &prop
	out = e.ProcessResponse(map[string]any{
		"status_code": 200, "headers": map[string]string{"content-type": "application/json"},
		"content": `{"other":2}`, "url": "u",
	})
	if _, ok := out["body"].(map[string]any); !ok {
		t.Fatalf("miss 应回原 dict: %v", out)
	}
	// on-success 非成功 → 空 dict
	rh.ResponseDataProperty = nil
	rh.ResponseMode = "on-success"
	out = e.ProcessResponse(map[string]any{
		"status_code": 500, "headers": map[string]string{"content-type": "text/plain"},
		"content": "x", "url": "u",
	})
	if len(out) != 0 {
		t.Fatalf("on-success 非成功应清空: %v", out)
	}
	// on-error 成功 → 空 dict
	rh.ResponseMode = "on-error"
	out = e.ProcessResponse(map[string]any{
		"status_code": 200, "headers": map[string]string{"content-type": "text/plain"},
		"content": "x", "url": "u",
	})
	if len(out) != 0 {
		t.Fatalf("on-error 成功应清空: %v", out)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: FAIL undefined: ProcessResponse

- [ ] **Step 3: 实现**

```go
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
		contentStr, ok := content.(string)
		if !ok {
			if b, isB := content.([]byte); isB {
				contentStr = string(b)
			}
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
```

- [ ] **Step 4: 跑测试通过**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/ -count=1`
Expected: PASS

- [ ] **Step 5: 提交（path-limited）**

```bash
git commit internal/openuapclaw/core/workflow/components/tool/http/http_request_component.go \
  internal/openuapclaw/core/workflow/components/tool/http/http_request_component_test.go \
  -m "feat(http): 8.22 ProcessResponse 码判定/autodetect/mode 三分支（Task5）"
```

---

### Task 6: Executable 四槽装配 + Component 组合面

**Files:**
- Modify: 同上两文件

- [ ] **Step 1: 写失败测试**

```go
// TestExecutable_四槽装配 invoke 真链 + stream/collect/transform 单帧委托
func TestExecutable_四槽装配(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"r":1}`))
	}))
	defer srv.Close()
	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL
	rp.Timeout = 5
	e := NewHTTPRequestExecutable(&HttpComponentConfig{RequestParams: rp})
	out, err := e.InvokeFn(context.Background(), map[string]any{}, nil, nil)
	if err != nil {
		t.Fatalf("invoke 失败: %v", err)
	}
	if out["statusCode"] != 200 {
		t.Fatalf("invoke 三步链漂移: %v", out)
	}
	// stream：单帧
	ch, err := e.StreamFn(context.Background(), map[string]any{}, nil, nil)
	if err != nil {
		t.Fatalf("stream 失败: %v", err)
	}
	frames := 0
	for f := range ch {
		frames++
		if f.Err != nil {
			t.Fatalf("stream 帧错误: %v", f.Err)
		}
	}
	if frames != 1 {
		t.Fatalf("stream 应单帧: %d", frames)
	}
	// collect：批出
	cOut, err := e.CollectFn(context.Background(), nil, nil, nil)
	if err != nil || cOut["statusCode"] != 200 {
		t.Fatalf("collect 漂移: %v %v", cOut, err)
	}
	// transform：单帧
	tch, err := e.TransformFn(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("transform 失败: %v", err)
	}
	tFrames := 0
	for range tch {
		tFrames++
	}
	if tFrames != 1 {
		t.Fatalf("transform 应单帧: %d", tFrames)
	}
}

// TestComponent_懒构造缓存 executable 属性单例 + Config 访问器
func TestComponent_懒构造缓存(t *testing.T) {
	cfg := NewHttpComponentConfig()
	c := NewHTTPRequestComponent(cfg)
	if c.Config() != cfg {
		t.Fatal("Config 访问器应返回原引用")
	}
	ex1 := c.Executable()
	ex2 := c.Executable()
	if ex1 != ex2 {
		t.Fatal("executable 应懒构造单例缓存（Python _executable 缓存）")
	}
	if _, ok := any(ex1).(*HTTPRequestExecutable); !ok {
		t.Fatalf("产物类型漂移: %T", ex1)
	}
	// ToExecutable 走 ToExecutableFn 槽返回同一单例
	if c.ToExecutable() != any(ex1).(graphexe.GeneralExecutor) == false {
		t.Fatal("ToExecutable 应经槽返回单例")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: FAIL undefined: invoke/StreamFn/NewHTTPRequestComponent 等

- [ ] **Step 3: 实现（非导出 invoke + 四槽装配补全 + Component 方法）**

`NewHTTPRequestExecutable` 中补三槽（Task 2 已装 InvokeFn）：

```go
	// stream/collect/transform 单帧委托（Python :155-168 yield/return invoke）
	e.StreamFn = func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade,
		modelContext iface.ModelContext) (<-chan graphexe.StreamChunk[any], error) {
		out, err := e.invoke(ctx, inputs, sess, modelContext)
		if err != nil {
			return nil, err
		}
		ch := make(chan graphexe.StreamChunk[any], 1)
		ch <- graphexe.StreamChunk[any]{Val: out}
		close(ch)
		return ch, nil
	}
	e.CollectFn = func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade,
		modelContext iface.ModelContext) (map[string]any, error) {
		return e.invoke(ctx, inputs, sess, modelContext)
	}
	e.TransformFn = func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade,
		modelContext iface.ModelContext) (<-chan graphexe.StreamChunk[any], error) {
		out, err := e.invoke(ctx, inputs, sess, modelContext)
		if err != nil {
			return nil, err
		}
		ch := make(chan graphexe.StreamChunk[any], 1)
		ch <- graphexe.StreamChunk[any]{Val: out}
		close(ch)
		return ch, nil
	}
```

非导出 invoke（非导出段）：

```go
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
```

Component 组合面（导出函数段）：

```go
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

// ToExecutable 产出可执行体（Python to_executable :511-512）
func (c *HTTPRequestComponent) toExecutable() *HTTPRequestExecutable {
	return NewHTTPRequestExecutable(c.config)
}
```

（注：测试里 `c.ToExecutable() != any(ex1).(graphexe.GeneralExecutor)` 的比较写法实施时以编译通过为准——GeneralExecutor 为接口，直接 `c.ToExecutable() == graphexe.GeneralExecutor(ex1)`。）

- [ ] **Step 4: 跑测试通过 + 覆盖率检查**

Run: `go test -cover ./internal/openuapclaw/core/workflow/components/tool/http/`
Expected: PASS；覆盖率 ≥ 85%

- [ ] **Step 5: 编译全仓 + vet**

Run: `go build ./... && go vet ./...`
Expected: 无错误

- [ ] **Step 6: 提交（path-limited）**

```bash
git commit internal/openuapclaw/core/workflow/components/tool/http/http_request_component.go \
  internal/openuapclaw/core/workflow/components/tool/http/http_request_component_test.go \
  -m "feat(http): 8.22 Executable 四槽装配+Component 组合面（Task6）"
```

---

### Task 7: 集成测试 + 全量回归

**Files:**
- Create: `internal/openuapclaw/core/workflow/components/tool/http/integration_test.go`

（归属说明：本测试用 `session.NewSession` + `workflow.ExecuteSingleComponent` 真链，跨 workflow/session/components/graph 多子系统，但按 8.18 惯例落组件包内集成用例——若审计工具判定须入 `tests/integration/`，移目录并补 `//go:build integration` + `suite.Run(`，先例见 workflow_llm_comp 套件。）

- [ ] **Step 1: 写集成用例（ExecuteSingleComponent 真链 + httptest 端点 + inputs 占位符注入）**

```go
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	workflow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
)

// TestIntegration_HTTPRequestComponent_真链 ExecuteSingleComponent 十步全链：
// inputs 占位符注入 URL → 三步链 → 输出信封贯通（对齐 8.18 llm 集成套件惯例）
func TestIntegration_HTTPRequestComponent_真链(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"echo": r.URL.Path})
	}))
	defer srv.Close()

	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL + "/{{path}}"
	rp.Timeout = 5
	comp := NewHTTPRequestComponent(&HttpComponentConfig{RequestParams: rp})

	sess := session.NewSession()
	out, err := workflow.ExecuteSingleComponent(context.Background(), "http-comp", *sess, comp,
		map[string]any{"path": "hello"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("单组件直调失败: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["statusCode"] != 200 || m["ok"] != true {
		t.Fatalf("输出信封漂移: %v", out)
	}
	body := m["body"].(map[string]any)
	if body["echo"] != "/hello" {
		t.Fatalf("占位符 URL 未贯通: %v", body)
	}
}
```

- [ ] **Step 2: 跑集成用例**

Run: `go test ./internal/openuapclaw/core/workflow/components/tool/http/ -run TestIntegration -count=1`
Expected: PASS

- [ ] **Step 3: 全量回归**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 零 FAIL（既有抖动用例对照 main 基线判断）

- [ ] **Step 4: 审计自检（若移入 tests/integration）**

Run: `go run ./cmd/integration-audit -verify`
Expected: 无新增违规

- [ ] **Step 5: 提交（path-limited）**

```bash
git commit internal/openuapclaw/core/workflow/components/tool/http/integration_test.go \
  -m "test(http): 8.22 集成——ExecuteSingleComponent 真链（Task7）"
```

---

### Task 8: IMPLEMENTATION_PLAN 回填 + 推送

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（8.22 行 ☐→✅ + Go 事实回填；实施顺序注记新增"二十一次记录"行，**历史注记全保留**）

- [ ] **Step 1: 回填 8.22 行**

将 `| 8.22 | ☐ | HTTPRequestComponent | HTTP 请求组件 | ... |` 改为 ✅ 行，按 8.21 行格式回填：包路径、一比一对照要点（三步链/五类 body/四类认证/三退避）、偏差 D-22-1..6（二进制 []byte+base64、FORM urlencoded 修正、原地写回复刻、ast→宽解析、asyncio→time.Sleep、inputs body 非 map 形态 102000 合理化）、覆盖率、spec/plan 路径。

- [ ] **Step 2: 实施顺序注记新增一行（保留全部历史注记）**

```markdown
> **8.x 实施顺序（2026-11-23 二十一次记录）**：8.22 一次收口（http 包 512 行一比一；D-22-1..6；FORM 怪癖方案 B 修正拍板；错误码勘误 182012→102000；每轮新建 transport 拍板）；下一节 8.23-8.26 按编号（8.23/8.24 属 Python llm 包），8.13 drawable 缝继续压后。原注记保留。
```

- [ ] **Step 3: 提交 + 推送**

```bash
git commit IMPLEMENTATION_PLAN.md -m "docs(plan): 8.22 HTTPRequestComponent 完成 ✅（D-22-1..6，回填 Go 事实）"
git push origin HEAD
```

- [ ] **Step 4: memory 登记**

更新 `rsi-8-22-complete` 记忆文件 + MEMORY.md 索引行（含 FORM 修正拍板、错误码勘误、共享 checkout path-limited 提交教训）。

---

## 自审记录

- **Spec 覆盖**：§3 对照表 19 符号 → Task1(配置/枚举 13 符号)+Task2(processInputs+2 助手)+Task3(3 方法)+Task4(MakeRequest+3 助手)+Task5+Task6(装配+Component 4 符号)全覆盖；§4 死字段 → Task1 结构体照搬；§5 偏差 D-22-1(Task4 测试钉住)/D-22-2(Task3 测试钉住)/D-22-3(Task2 测试钉住)/D-22-4(resolveTemplateToDict)/D-22-5(time.Sleep)/D-22-6(inputs body 非配置形态 falsy 短路，MakeRequest 经 PrepareRequestBody(nil/非配置) 承载) → 新登记 D-22-6 于 spec 待实施时补行；§6 测试 14 项 → Task2-7 覆盖
- **占位符扫描**：无 TBD/TODO；pythonsem.PyJSONDumps 存在性留实施时 grep 确认（有 fallback 指引，非占位）
- **类型一致性**：ProcessInputs 返回 map（headers 为 map[string]string、body 为 *HttpRequestBodyConfig|map[string]any）→ MakeRequest 取用一致；MakeRequest 返回 map（content any）→ ProcessResponse 取用一致
