package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
)

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
		"headers":          map[string]any{"X-Extra": "e"},
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
	if gb, ok := got["body"].(map[string]any); !ok || gb["a"] != 1 {
		t.Fatalf("inputs 回落 body 漂移: %v", got["body"])
	}
	if ga, ok := got["authentication"].(map[string]any); !ok || ga["type"] != "basic" {
		t.Fatalf("inputs 回落 authentication 漂移: %v", got["authentication"])
	}
}

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
	a.Type, a.Username, a.Password = HttpAuthTypeBasic, &u, &p
	h = e.ApplyAuthentication(map[string]string{}, a)
	// base64("user:pass") = dXNlcjpwYXNz
	if h["Authorization"] != "Basic dXNlcjpwYXNz" {
		t.Fatalf("basic 漂移: %v", h)
	}
	// basic 用户名缺失 → 不注入（Python :428 if username and password）
	a2 := NewHttpAuthConfig()
	a2.Type, a2.Password = HttpAuthTypeBasic, &p
	h = e.ApplyAuthentication(map[string]string{}, a2)
	if len(h) != 0 {
		t.Fatalf("basic 缺 username 应不注入: %v", h)
	}
	// bearer
	a3 := NewHttpAuthConfig()
	a3.Type, a3.Token = HttpAuthTypeBearer, func() *string { s := "tk"; return &s }()
	if h := e.ApplyAuthentication(map[string]string{}, a3); h["Authorization"] != "Bearer tk" {
		t.Fatalf("bearer 漂移: %v", h)
	}
	// api_key header 位置
	a4 := NewHttpAuthConfig()
	a4.Type, a4.APIKey, a4.InLocation, a4.Name = HttpAuthTypeAPIKey, func() *string { s := "key"; return &s }(), "header", "X-Key"
	if h := e.ApplyAuthentication(map[string]string{}, a4); h["X-Key"] != "key" {
		t.Fatalf("api_key header 漂移: %v", h)
	}
	// api_key query 位置 → 不注入（Python :439-441 仅 header 分支）
	a5 := NewHttpAuthConfig()
	a5.Type, a5.APIKey, a5.InLocation = HttpAuthTypeAPIKey, func() *string { s := "key"; return &s }(), "query"
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
	bc.JSONData = map[string]any{"a": 1}
	b, ct := e.PrepareRequestBody(bc)
	if ct != "application/json" || string(b.([]byte)) != `{"a":1}` {
		t.Fatalf("JSON 编码漂移: %v %q", b, ct)
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

// makeRequestParams 组装 makeRequest 的 params 形状（ProcessInputs 产物契约）
func makeRequestParams(targetURL, method string, body any) map[string]any {
	return map[string]any{
		"url": targetURL, "method": method, "headers": map[string]string{},
		"query_parameters": map[string]any{}, "body": body,
		"authentication": map[string]any{},
	}
}

// TestMakeRequest_GET基本链 GET + 查询透传 + 结果 dict 形状
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
	rp.Timeout = 5
	e := &HTTPRequestExecutable{requestParams: rp}
	params := makeRequestParams(srv.URL, "GET", map[string]any{})
	params["query_parameters"] = map[string]any{"q": "v"}
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
	bc := NewHttpRequestBodyConfig()
	bc.JSONData = map[string]any{"a": 1}
	if _, err := e.MakeRequest(makeRequestParams(srv.URL, "POST", bc)); err != nil {
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
	_, err := e.MakeRequest(makeRequestParams(srv.URL, "GET", map[string]any{}))
	if err == nil {
		t.Fatal("超限应报错")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应 BaseError: %v", err)
	}
	if err.(*exception.BaseError).Code() != 102000 {
		t.Fatalf("应 102000: %v", err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("超限应触发 1 次重试（共 2 次请求）: %d", hits)
	}
}

// TestMakeRequest_状态码重试 429 命中重试表 → 重试后成功
func TestMakeRequest_状态码重试(t *testing.T) {
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
	res, err := e.MakeRequest(makeRequestParams(srv.URL, "GET", map[string]any{}))
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
	_, err := e.MakeRequest(makeRequestParams(rp.URL, "GET", map[string]any{}))
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
	res, err := e.MakeRequest(makeRequestParams(srv.URL, "GET", map[string]any{}))
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
	res, err := e.MakeRequest(makeRequestParams(srv.URL, "GET", map[string]any{}))
	if err != nil || res["status_code"] != 302 {
		t.Fatalf("302 应原样返回不跟随: %v %v", res, err)
	}
}

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
	if out["statusCode"] != 200 || out["ok"] != true || out["url"] != "u" {
		t.Fatalf("json autodetect 信封漂移: %v", out)
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
	// 其余 → BINARY pass（bytes 原样）
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
	// failure_codes 非空 → 抑制 2xx 兜底子句 + success_codes 收窄为 [200] → 204 失败
	//（Python :368 is_success = in success_codes or (not failure_codes and 2xx)）
	rh.ResponseCodeFailureCodes = []int{500}
	rh.ResponseCodeSuccessCodes = []int{200}
	if out := e.ProcessResponse(mkResp(204)); out["ok"] != false {
		t.Fatal("failure_codes 非空时 204 出收窄 success_codes 应失败")
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

// TestComponent_懒构造缓存 executable 属性单例 + Config 访问器 + 槽优先
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
	// ToExecutable 走 ToExecutableFn 槽返回同一单例（8.20 槽优先）
	if c.ToExecutable() != graphexe.GeneralExecutor(ex1) {
		t.Fatal("ToExecutable 应经槽返回单例")
	}
}
