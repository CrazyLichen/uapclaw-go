package auth

import (
	"context"
	"crypto/tls"
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
	os.Setenv("TEST_AUTH_SSL_VERIFY_OFF", "false")
	defer os.Unsetenv("TEST_AUTH_SSL_VERIFY_OFF")

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
	os.Setenv("TEST_AUTH_SSL_VERIFY_EMPTY", "false")
	defer os.Unsetenv("TEST_AUTH_SSL_VERIFY_EMPTY")

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
