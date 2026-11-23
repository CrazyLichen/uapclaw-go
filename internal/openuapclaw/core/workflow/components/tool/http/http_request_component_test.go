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
