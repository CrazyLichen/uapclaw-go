package context

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBuildModelContextWindowTokens_用户值覆盖拉取值 验证 OpenRouter 表与用户表的合并优先级。
func TestBuildModelContextWindowTokens_用户值覆盖拉取值(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/qwen3-max","context_length":262144},{"id":"other/qwen3-max","context_length":1}]}`))
	}))
	defer srv.Close()
	resetOpenRouterCache()
	overrideOpenRouterURL(srv.URL)
	t.Cleanup(func() {
		overrideOpenRouterURL("https://openrouter.ai/api/v1/models")
		resetOpenRouterCache()
	})

	got := BuildModelContextWindowTokens(map[string]int{"qwen3-max": 1000}, true, 3)
	if got["qwen3-max"] != 1000 {
		t.Fatalf("用户显式值应覆盖拉取值，got %d", got["qwen3-max"])
	}
	if got["vendor/qwen3-max"] != 262144 {
		t.Fatalf("带前缀 id 应保留，got %v", got)
	}
	if _, exists := got["other/qwen3-max"]; !exists {
		t.Fatalf("带前缀 id 应保留完整键")
	}
}

// TestBuildModelContextWindowTokens_关闭拉取仅用用户表 验证开关为假时不发生网络访问。
func TestBuildModelContextWindowTokens_关闭拉取仅用用户表(t *testing.T) {
	resetOpenRouterCache()
	got := BuildModelContextWindowTokens(map[string]int{"m": 1234}, false, 3)
	if len(got) != 1 || got["m"] != 1234 {
		t.Fatalf("got %v", got)
	}
}

// TestFetchOpenRouterContextWindows_失败沿用缓存 验证拉取失败不清空已有缓存。
func TestFetchOpenRouterContextWindows_失败沿用缓存(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"a/b","context_length":777}]}`))
	}))
	resetOpenRouterCache()
	overrideOpenRouterURL(srv.URL)
	t.Cleanup(func() {
		overrideOpenRouterURL("https://openrouter.ai/api/v1/models")
		resetOpenRouterCache()
	})

	if got := fetchOpenRouterContextWindows(3); got["b"] != 777 {
		t.Fatalf("首次拉取应命中，got %v", got)
	}
	srv.Close() // 后续请求失败
	expireOpenRouterCacheTTL()
	got := fetchOpenRouterContextWindows(3)
	if got["b"] != 777 {
		t.Fatalf("拉取失败应沿用缓存，got %v", got)
	}
}

// TestResolveContextMax_优先级 验证 fallback > 用户映射 > 内置表 > 默认值。
func TestResolveContextMax_优先级(t *testing.T) {
	if got := ResolveContextMax("qwen-max", 131072, map[string]int{"qwen-max": 32000}); got != 131072 {
		t.Fatalf("fallback 应最高优先，got %d", got)
	}
	if got := ResolveContextMax("qwen-max", 0, map[string]int{"qwen-max": 32000}); got != 32000 {
		t.Fatalf("用户映射应次优先，got %d", got)
	}
	if got := ResolveContextMax("qwen-max", 0, nil); got != 262144 {
		t.Fatalf("内置表应命中 262144，got %d", got)
	}
	if got := ResolveContextMax("unknown-model-x", 0, nil); got != DefaultContextMaxTokens {
		t.Fatalf("未知模型应回落默认值，got %d", got)
	}
}
