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
