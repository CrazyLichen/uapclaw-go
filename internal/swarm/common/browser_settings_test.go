package common

import (
	"os"
	"testing"
)

// TestResolveJevConnection typesafe 供应商完整往返
func TestResolveJevConnection(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "sk-jev")
	cfg := map[string]any{
		"browser": map[string]any{
			"decision": map[string]any{
				"provider": "typesafe",
				"api_base": "https://api.typesafe.ai/v1/",
			},
		},
	}
	got, err := ResolveJevConnection(cfg, nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got["api_base"] != "https://api.typesafe.ai/v1" {
		t.Errorf("api_base = %q, want 尾斜杠剥离", got["api_base"])
	}
	if got["model_name"] != "jev-1.13.0" {
		t.Errorf("model_name = %q", got["model_name"])
	}
	if got["endpoint_path"] != "/systemone" {
		t.Errorf("endpoint_path = %q, want /systemone", got["endpoint_path"])
	}
}

// TestResolveJevConnection_BrowserNotMapping 非映射报错
func TestResolveJevConnection_BrowserNotMapping(t *testing.T) {
	_, err := ResolveJevConnection(map[string]any{"browser": "x"}, nil)
	if err == nil || err.Error() != "browser must be a mapping" {
		t.Errorf("err = %v, want browser must be a mapping", err)
	}
	_, err = ResolveJevConnection(map[string]any{"browser": map[string]any{"decision": "x"}}, nil)
	if err == nil || err.Error() != "browser.decision must be a mapping" {
		t.Errorf("err = %v, want browser.decision must be a mapping", err)
	}
}

// TestResolveJevConnection_ProviderInvalid 非法 provider
func TestResolveJevConnection_ProviderInvalid(t *testing.T) {
	cfg := map[string]any{"browser": map[string]any{"decision": map[string]any{"provider": "bogus"}}}
	_, err := ResolveJevConnection(cfg, nil)
	if err == nil || err.Error() != "Jev provider must be openrouter, typesafe or custom" {
		t.Errorf("err = %v", err)
	}
}

// TestResolveJevConnection_HTTPSValidation URL 与密钥校验
func TestResolveJevConnection_HTTPSValidation(t *testing.T) {
	t.Setenv("JEV_CUSTOM_API_KEY", "sk")
	cfg := func(apiBase string) map[string]any {
		return map[string]any{"browser": map[string]any{
			"decision": map[string]any{"provider": "custom", "api_base": apiBase},
		}}
	}
	for _, bad := range []string{"http://api.typesafe.ai/v1", "https://u:p@x.ai/v1", "https://x.ai/v1?q=1", "not a url"} {
		if _, err := ResolveJevConnection(cfg(bad), nil); err == nil ||
			err.Error() != "Jev API base must be an HTTPS URL without credentials, query or fragment" {
			t.Errorf("api_base %q: err = %v", bad, err)
		}
	}
	if _, err := ResolveJevConnection(cfg(""), nil); err == nil ||
		err.Error() != "Jev api_base is required in Models > Jev settings" {
		t.Errorf("空 api_base: err = %v", err)
	}
	// 密钥缺失
	t.Setenv("JEV_CUSTOM_API_KEY", "")
	os.Unsetenv("JEV_CUSTOM_API_KEY")
	if _, err := ResolveJevConnection(cfg("https://api.typesafe.ai/v1"), nil); err == nil ||
		err.Error() != "Jev API key is required in Models > Jev settings" {
		t.Errorf("无密钥: err = %v", err)
	}
}

// TestResolveJevConnection_OpenRouterEndpoint openrouter 决策端点
func TestResolveJevConnection_OpenRouterEndpoint(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-o")
	got, err := ResolveJevConnection(map[string]any{"browser": map[string]any{
		"decision": map[string]any{"provider": "openrouter"},
	}}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got["endpoint_path"] != "/decisions" || got["model_name"] != "typesafe/jev-1.13" {
		t.Errorf("got = %v", got)
	}
}
