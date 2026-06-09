package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSSLAuthStrategy_Authenticate_非HTTPS(t *testing.T) {
	strategy := &SSLAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "SSL_VERIFY",
			"ssl_cert_env":      "SSL_CERT",
			"url":               "http://example.com",
		},
		ToolType: "restful_api",
	})
	if err != nil {
		t.Fatalf("非 HTTPS 不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	tlsConfig, ok := result.AuthData["tls_config"].(*tls.Config)
	if !ok {
		t.Fatal("期望 auth_data 中有 tls_config")
	}
	if !tlsConfig.InsecureSkipVerify {
		t.Error("非 HTTPS 期望 InsecureSkipVerify=true")
	}
}

func TestSSLAuthStrategy_Authenticate_验证关闭(t *testing.T) {
	_ = os.Setenv("TEST_AUTH_SSL_VERIFY_OFF", "false")
	defer func() { _ = os.Unsetenv("TEST_AUTH_SSL_VERIFY_OFF") }()

	strategy := &SSLAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "TEST_AUTH_SSL_VERIFY_OFF",
			"ssl_cert_env":      "SSL_CERT",
			"url":               "https://example.com",
		},
		ToolType: "restful_api",
	})
	if err != nil {
		t.Fatalf("verify off 不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	tlsConfig, ok := result.AuthData["tls_config"].(*tls.Config)
	if !ok {
		t.Fatal("期望 auth_data 中有 tls_config")
	}
	if !tlsConfig.InsecureSkipVerify {
		t.Error("verify off 期望 InsecureSkipVerify=true")
	}
}

func TestSSLAuthStrategy_Authenticate_空URL(t *testing.T) {
	_ = os.Setenv("TEST_AUTH_SSL_VERIFY_EMPTY", "false")
	defer func() { _ = os.Unsetenv("TEST_AUTH_SSL_VERIFY_EMPTY") }()

	strategy := &SSLAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "TEST_AUTH_SSL_VERIFY_EMPTY",
			"ssl_cert_env":      "SSL_CERT",
			"url":               "",
		},
		ToolType: "restful_api",
	})
	if err != nil {
		t.Fatalf("空 URL 默认 HTTPS，verify off 不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
}

func TestHeaderQueryAuthStrategy_Authenticate_基本(t *testing.T) {
	strategy := &HeaderQueryAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeHeaderAndQuery,
		Config: map[string]any{
			"auth_headers":      map[string]string{"Authorization": "Bearer token123"},
			"auth_query_params": map[string]string{"api_key": "key123"},
		},
		ToolType: "mcp",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	provider, ok := result.AuthData["auth_provider"].(*HeaderQueryProvider)
	if !ok {
		t.Fatal("期望 auth_data 中有 auth_provider")
	}
	if provider.Headers["Authorization"] != "Bearer token123" {
		t.Errorf("Headers[Authorization] = %q, want Bearer token123", provider.Headers["Authorization"])
	}
	if provider.QueryParams["api_key"] != "key123" {
		t.Errorf("QueryParams[api_key] = %q, want key123", provider.QueryParams["api_key"])
	}
}

func TestHeaderQueryAuthStrategy_Authenticate_空配置(t *testing.T) {
	strategy := &HeaderQueryAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeHeaderAndQuery,
		Config:   map[string]any{},
		ToolType: "mcp",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	provider, ok := result.AuthData["auth_provider"].(*HeaderQueryProvider)
	if ok && provider != nil {
		t.Error("无 headers/query 时期望 provider 为 nil")
	}
}

func TestAuthStrategyRegistry_ExecuteAuth_基本(t *testing.T) {
	registry := &AuthStrategyRegistry{
		strategies: map[string]AuthStrategy{
			AuthTypeSSL: &SSLAuthStrategy{},
		},
	}

	result, err := registry.ExecuteAuth(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "SSL_VERIFY",
			"ssl_cert_env":      "SSL_CERT",
			"url":               "http://example.com",
		},
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
}

func TestAuthStrategyRegistry_ExecuteAuth_不支持的类型(t *testing.T) {
	registry := &AuthStrategyRegistry{
		strategies: map[string]AuthStrategy{},
	}

	result, err := registry.ExecuteAuth(context.Background(), &ToolAuthConfig{
		AuthType: "unknown",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result.Success {
		t.Error("不支持的类型期望 Success=false")
	}
	if result.Message != "Unsupported auth type: unknown" {
		t.Errorf("Message = %q, want Unsupported auth type: unknown", result.Message)
	}
}

func TestRegisterAuthCallback_基本(t *testing.T) {
	fw := callback.NewCallbackFramework()
	RegisterAuthCallback(fw)

	results := fw.TriggerTool(context.Background(), &callback.ToolCallEventData{
		Event: callback.ToolAuth,
		Extra: map[string]any{
			"auth_config": &ToolAuthConfig{
				AuthType: AuthTypeSSL,
				Config: map[string]any{
					"verify_switch_env": "SSL_VERIFY",
					"ssl_cert_env":      "SSL_CERT",
					"url":               "http://example.com",
				},
				ToolType: "restful_api",
			},
		},
	})

	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	authResult, ok := results[0].(*ToolAuthResult)
	if !ok {
		t.Fatal("期望 *ToolAuthResult 类型")
	}
	if !authResult.Success {
		t.Error("期望 Success=true")
	}
}

func TestRegisterAuthCallback_无效配置(t *testing.T) {
	fw := callback.NewCallbackFramework()
	RegisterAuthCallback(fw)

	results := fw.TriggerTool(context.Background(), &callback.ToolCallEventData{
		Event: callback.ToolAuth,
		Extra: map[string]any{},
	})

	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	authResult, ok := results[0].(*ToolAuthResult)
	if !ok {
		t.Fatal("期望 *ToolAuthResult 类型")
	}
	if authResult.Success {
		t.Error("无效 config 期望 Success=false")
	}
}

func TestHeaderQueryProvider(t *testing.T) {
	provider := NewHeaderQueryProvider(
		map[string]string{"X-API-Key": "abc"},
		map[string]string{"token": "xyz"},
	)
	if provider.Headers["X-API-Key"] != "abc" {
		t.Errorf("Headers[X-API-Key] = %q, want abc", provider.Headers["X-API-Key"])
	}
	if provider.QueryParams["token"] != "xyz" {
		t.Errorf("QueryParams[token] = %q, want xyz", provider.QueryParams["token"])
	}
}

func TestAuthType常量(t *testing.T) {
	if AuthTypeSSL != "ssl" {
		t.Errorf("AuthTypeSSL = %q, want ssl", AuthTypeSSL)
	}
	if AuthTypeHeaderAndQuery != "header_and_query" {
		t.Errorf("AuthTypeHeaderAndQuery = %q, want header_and_query", AuthTypeHeaderAndQuery)
	}
}

func TestToolAuthConfig_字段赋值(t *testing.T) {
	config := &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "SSL_VERIFY",
			"ssl_cert_env":      "SSL_CERT",
			"url":               "https://example.com",
		},
		ToolType: "restful_api",
		ToolID:   "tool-123",
	}

	if config.AuthType != AuthTypeSSL {
		t.Errorf("AuthType = %q, want %q", config.AuthType, AuthTypeSSL)
	}
	if config.ToolType != "restful_api" {
		t.Errorf("ToolType = %q, want restful_api", config.ToolType)
	}
	if config.ToolID != "tool-123" {
		t.Errorf("ToolID = %q, want tool-123", config.ToolID)
	}
}

func TestToolAuthResult_成功(t *testing.T) {
	result := &ToolAuthResult{
		Success:  true,
		AuthData: map[string]any{"tls_config": "mock"},
		Message:  "SSL authentication configured",
	}

	if !result.Success {
		t.Error("期望 Success=true")
	}
	if result.Message != "SSL authentication configured" {
		t.Errorf("Message = %q, want SSL authentication configured", result.Message)
	}
}

func TestToolAuthResult_失败(t *testing.T) {
	result := &ToolAuthResult{
		Success:  false,
		AuthData: map[string]any{},
		Message:  "Unsupported auth type: unknown",
	}

	if result.Success {
		t.Error("期望 Success=false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestToStringMap_nil输入(t *testing.T) {
	result := toStringMap(nil)
	if result != nil {
		t.Errorf("nil 输入期望返回 nil，实际 %v", result)
	}
}

func TestToStringMap_mapStringString(t *testing.T) {
	input := map[string]string{"key": "value", "foo": "bar"}
	result := toStringMap(input)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["key"] != "value" {
		t.Errorf("result[key] = %q, want value", result["key"])
	}
	if result["foo"] != "bar" {
		t.Errorf("result[foo] = %q, want bar", result["foo"])
	}
}

func TestToStringMap_mapStringAny_字符串值(t *testing.T) {
	input := map[string]any{"key": "value", "num": "123"}
	result := toStringMap(input)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["key"] != "value" {
		t.Errorf("result[key] = %q, want value", result["key"])
	}
	if result["num"] != "123" {
		t.Errorf("result[num] = %q, want 123", result["num"])
	}
}

func TestToStringMap_mapStringAny_非字符串值(t *testing.T) {
	input := map[string]any{"count": 42, "flag": true}
	result := toStringMap(input)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["count"] != "42" {
		t.Errorf("result[count] = %q, want 42", result["count"])
	}
	if result["flag"] != "true" {
		t.Errorf("result[flag] = %q, want true", result["flag"])
	}
}

func TestToStringMap_不支持的类型(t *testing.T) {
	result := toStringMap(12345)
	if result != nil {
		t.Errorf("不支持的类型期望返回 nil，实际 %v", result)
	}
}

func TestToStringMap_mapStringAny_nil值(t *testing.T) {
	input := map[string]any{"empty": nil}
	result := toStringMap(input)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	// nil 值通过 fmt.Sprintf("%v", nil) 转换为 "<nil>"
	if result["empty"] != "<nil>" {
		t.Errorf("result[empty] = %q, want <nil>", result["empty"])
	}
}

func TestSSLAuthStrategy_Authenticate_HTTPS验证开启_无证书环境变量(t *testing.T) {
	// 确保证书环境变量未设置
	_ = os.Unsetenv("TEST_AUTH_SSL_CERT_MISSING")
	// 确保验证开关未命中 triggerValue（不设置 "false"）
	_ = os.Setenv("TEST_AUTH_SSL_VERIFY_ON", "true")
	defer func() { _ = os.Unsetenv("TEST_AUTH_SSL_VERIFY_ON") }()

	strategy := &SSLAuthStrategy{}
	_, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "TEST_AUTH_SSL_VERIFY_ON",
			"ssl_cert_env":      "TEST_AUTH_SSL_CERT_MISSING",
			"url":               "https://example.com",
		},
		ToolType: "restful_api",
	})
	if err == nil {
		t.Fatal("HTTPS + verify=true + 无证书路径时期望报错")
	}
}

func TestSSLAuthStrategy_Authenticate_HTTPS验证开启_空证书路径(t *testing.T) {
	// 验证开关未命中 triggerValue
	_ = os.Setenv("TEST_AUTH_SSL_VERIFY_ON2", "true")
	_ = os.Setenv("TEST_AUTH_SSL_CERT_EMPTY", "")
	defer func() {
		_ = os.Unsetenv("TEST_AUTH_SSL_VERIFY_ON2")
		_ = os.Unsetenv("TEST_AUTH_SSL_CERT_EMPTY")
	}()

	strategy := &SSLAuthStrategy{}
	_, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "TEST_AUTH_SSL_VERIFY_ON2",
			"ssl_cert_env":      "TEST_AUTH_SSL_CERT_EMPTY",
			"url":               "https://example.com",
		},
		ToolType: "restful_api",
	})
	if err == nil {
		t.Fatal("HTTPS + verify=true + 证书路径为空时期望报错")
	}
}

func TestSSLAuthStrategy_Authenticate_HTTPS验证开启_无效证书路径(t *testing.T) {
	// 验证开关未命中 triggerValue
	_ = os.Setenv("TEST_AUTH_SSL_VERIFY_ON3", "true")
	// 指向一个不存在的证书文件
	_ = os.Setenv("TEST_AUTH_SSL_CERT_INVALID", "/nonexistent/cert.pem")
	defer func() {
		_ = os.Unsetenv("TEST_AUTH_SSL_VERIFY_ON3")
		_ = os.Unsetenv("TEST_AUTH_SSL_CERT_INVALID")
	}()

	strategy := &SSLAuthStrategy{}
	_, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "TEST_AUTH_SSL_VERIFY_ON3",
			"ssl_cert_env":      "TEST_AUTH_SSL_CERT_INVALID",
			"url":               "https://example.com",
		},
		ToolType: "restful_api",
	})
	if err == nil {
		t.Fatal("HTTPS + verify=true + 无效证书路径时期望报错")
	}
}

func TestSSLAuthStrategy_Authenticate_HTTPS验证开启_无证书路径(t *testing.T) {
	// verify=true 但不设置 sslCertEnv，走 CreateStrictTLSConfig("") 路径
	// 不设置任何 verify_switch_env，也不命中 triggerValue，urlIsHTTPS=true
	// 设置证书环境变量为空，这样 GetSSLConfig 会返回错误
	_ = os.Setenv("TEST_AUTH_SSL_VERIFY_ON4", "true")
	_ = os.Unsetenv("TEST_AUTH_SSL_CERT_NONE")
	defer func() {
		_ = os.Unsetenv("TEST_AUTH_SSL_VERIFY_ON4")
		_ = os.Unsetenv("TEST_AUTH_SSL_CERT_NONE")
	}()

	strategy := &SSLAuthStrategy{}
	_, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "TEST_AUTH_SSL_VERIFY_ON4",
			"ssl_cert_env":      "TEST_AUTH_SSL_CERT_NONE",
			"url":               "https://example.com",
		},
		ToolType: "restful_api",
	})
	// 因为 sslCertEnv 指向的环境变量为空，GetSSLConfig 会返回错误
	if err == nil {
		t.Fatal("verify=true 但无证书路径时期望报错")
	}
}

func TestSSLAuthStrategy_Authenticate_默认环境变量(t *testing.T) {
	// 不设置 verify_switch_env 和 ssl_cert_env，使用默认值
	strategy := &SSLAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"url": "http://example.com",
		},
		ToolType: "restful_api",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
}

func TestSSLAuthStrategy_Authenticate_URL大小写(t *testing.T) {
	// 测试 HTTPS 大写前缀，设置 verify off 使其走 InsecureSkipVerify 路径
	_ = os.Setenv("TEST_AUTH_SSL_VERIFY_CASE", "false")
	defer func() { _ = os.Unsetenv("TEST_AUTH_SSL_VERIFY_CASE") }()

	strategy := &SSLAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeSSL,
		Config: map[string]any{
			"verify_switch_env": "TEST_AUTH_SSL_VERIFY_CASE",
			"ssl_cert_env":      "SSL_CERT",
			"url":               "HTTPS://example.com",
		},
		ToolType: "restful_api",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	tlsConfig, ok := result.AuthData["tls_config"].(*tls.Config)
	if !ok {
		t.Fatal("期望 auth_data 中有 tls_config")
	}
	if !tlsConfig.InsecureSkipVerify {
		t.Error("verify off 期望 InsecureSkipVerify=true")
	}
}

func TestHeaderQueryAuthStrategy_Authenticate_mapStringAny(t *testing.T) {
	strategy := &HeaderQueryAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeHeaderAndQuery,
		Config: map[string]any{
			"auth_headers":      map[string]any{"Authorization": "Bearer abc"},
			"auth_query_params": map[string]any{"api_key": "key456"},
		},
		ToolType: "mcp",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	provider, ok := result.AuthData["auth_provider"].(*HeaderQueryProvider)
	if !ok {
		t.Fatal("期望 auth_data 中有 auth_provider")
	}
	if provider.Headers["Authorization"] != "Bearer abc" {
		t.Errorf("Headers[Authorization] = %q, want Bearer abc", provider.Headers["Authorization"])
	}
	if provider.QueryParams["api_key"] != "key456" {
		t.Errorf("QueryParams[api_key] = %q, want key456", provider.QueryParams["api_key"])
	}
}

func TestHeaderQueryAuthStrategy_Authenticate_仅Headers(t *testing.T) {
	strategy := &HeaderQueryAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeHeaderAndQuery,
		Config: map[string]any{
			"auth_headers": map[string]string{"X-Token": "tok"},
		},
		ToolType: "mcp",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	provider, ok := result.AuthData["auth_provider"].(*HeaderQueryProvider)
	if !ok {
		t.Fatal("期望 auth_data 中有 auth_provider")
	}
	if provider.Headers["X-Token"] != "tok" {
		t.Errorf("Headers[X-Token] = %q, want tok", provider.Headers["X-Token"])
	}
}

func TestHeaderQueryAuthStrategy_Authenticate_仅QueryParams(t *testing.T) {
	strategy := &HeaderQueryAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeHeaderAndQuery,
		Config: map[string]any{
			"auth_query_params": map[string]string{"token": "val"},
		},
		ToolType: "mcp",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	provider, ok := result.AuthData["auth_provider"].(*HeaderQueryProvider)
	if !ok {
		t.Fatal("期望 auth_data 中有 auth_provider")
	}
	if provider.QueryParams["token"] != "val" {
		t.Errorf("QueryParams[token] = %q, want val", provider.QueryParams["token"])
	}
}

func TestHeaderQueryAuthStrategy_Authenticate_mapStringAny_非字符串值(t *testing.T) {
	strategy := &HeaderQueryAuthStrategy{}
	result, err := strategy.Authenticate(context.Background(), &ToolAuthConfig{
		AuthType: AuthTypeHeaderAndQuery,
		Config: map[string]any{
			"auth_headers": map[string]any{"X-Count": 42, "X-Flag": true},
		},
		ToolType: "mcp",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success {
		t.Error("期望 Success=true")
	}
	provider, ok := result.AuthData["auth_provider"].(*HeaderQueryProvider)
	if !ok {
		t.Fatal("期望 auth_data 中有 auth_provider")
	}
	if provider.Headers["X-Count"] != "42" {
		t.Errorf("Headers[X-Count] = %q, want 42", provider.Headers["X-Count"])
	}
	if provider.Headers["X-Flag"] != "true" {
		t.Errorf("Headers[X-Flag] = %q, want true", provider.Headers["X-Flag"])
	}
}

// fakeErrorStrategy 用于测试的策略，总是返回错误
type fakeErrorStrategy struct{}

func (f *fakeErrorStrategy) Authenticate(_ context.Context, _ *ToolAuthConfig) (*ToolAuthResult, error) {
	return nil, errors.New("模拟认证失败")
}

func TestAuthStrategyRegistry_ExecuteAuth_策略返回错误(t *testing.T) {
	registry := &AuthStrategyRegistry{
		strategies: map[string]AuthStrategy{
			"fake": &fakeErrorStrategy{},
		},
	}

	result, err := registry.ExecuteAuth(context.Background(), &ToolAuthConfig{
		AuthType: "fake",
	})
	if err == nil {
		t.Fatal("策略返回错误时期望有 error")
	}
	if err.Error() != "模拟认证失败" {
		t.Errorf("错误信息 = %q, want 模拟认证失败", err.Error())
	}
	_ = result // 策略返回 error 时 result 为 nil
}

func TestAuthStrategyRegistry_Register(t *testing.T) {
	registry := &AuthStrategyRegistry{
		strategies: make(map[string]AuthStrategy),
	}
	strategy := &HeaderQueryAuthStrategy{}
	registry.Register("custom", strategy)

	if registry.strategies["custom"] != strategy {
		t.Error("注册后策略应存在于注册表")
	}
}

func TestRegisterAuthCallback_认证错误(t *testing.T) {
	fw := callback.NewCallbackFramework()
	RegisterAuthCallback(fw)

	results := fw.TriggerTool(context.Background(), &callback.ToolCallEventData{
		Event: callback.ToolAuth,
		Extra: map[string]any{
			"auth_config": &ToolAuthConfig{
				AuthType: "unknown_type",
			},
		},
	})

	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	authResult, ok := results[0].(*ToolAuthResult)
	if !ok {
		t.Fatal("期望 *ToolAuthResult 类型")
	}
	if authResult.Success {
		t.Error("不支持的认证类型期望 Success=false")
	}
	if authResult.Error != nil {
		t.Error("不支持的认证类型不应有 error 字段（是正常返回）")
	}
}

func TestRegisterAuthCallback_错误类型断言(t *testing.T) {
	fw := callback.NewCallbackFramework()
	RegisterAuthCallback(fw)

	// auth_config 不是 *ToolAuthConfig 类型
	results := fw.TriggerTool(context.Background(), &callback.ToolCallEventData{
		Event: callback.ToolAuth,
		Extra: map[string]any{
			"auth_config": "not_a_config",
		},
	})

	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	authResult, ok := results[0].(*ToolAuthResult)
	if !ok {
		t.Fatal("期望 *ToolAuthResult 类型")
	}
	if authResult.Success {
		t.Error("错误类型断言期望 Success=false")
	}
	if authResult.Message != "auth_config not found or invalid type in Extra" {
		t.Errorf("Message = %q, want auth_config not found or invalid type in Extra", authResult.Message)
	}
}

func TestRegisterAuthCallback_策略执行错误(t *testing.T) {
	fw := callback.NewCallbackFramework()
	// 注册一个会返回错误的策略到全局注册表
	globalRegistry.Register("test_error", &fakeErrorStrategy{})
	RegisterAuthCallback(fw)

	results := fw.TriggerTool(context.Background(), &callback.ToolCallEventData{
		Event: callback.ToolAuth,
		Extra: map[string]any{
			"auth_config": &ToolAuthConfig{
				AuthType: "test_error",
			},
		},
	})

	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	authResult, ok := results[0].(*ToolAuthResult)
	if !ok {
		t.Fatal("期望 *ToolAuthResult 类型")
	}
	if authResult.Success {
		t.Error("策略执行错误期望 Success=false")
	}
	if authResult.Error == nil {
		t.Error("策略执行错误期望 Error 非空")
	}
}
