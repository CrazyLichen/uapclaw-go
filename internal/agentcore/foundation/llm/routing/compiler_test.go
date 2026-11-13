package routing

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// 以下 12 个用例逐用例对齐上游
// tests/unit_tests/core/foundation/llm/test_model_group_routing_compiler.py

// 对齐 test_compile_single_model_preserves_explicit_request_defaults_and_none
// （compiler.py:91 compile_model_selection；None top_p 不得发明缺省 0.95）。
func TestCompileSingleModel_PreservesExplicitRequestDefaultsAndNone(t *testing.T) {
	compiled, err := CompileModelSelection(map[string]any{
		"model_id": "m1", "model_name": "qwen-plus", "provider": "dashscope",
		"api_key": "sk", "api_base": "https://dashscope.aliyuncs.com/compatible-mode",
		"endpoint_profile": "dashscope",
		"request_defaults": map[string]any{"temperature": 0.4, "top_p": nil},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if compiled.SelectedType != "model" || compiled.SelectedID != "m1" {
		t.Fatalf("selected 错误：%s/%s", compiled.SelectedType, compiled.SelectedID)
	}
	cc := compiled.ModelClientConfig
	if cc.ClientProvider != "dashscope" || cc.EndpointProfile != "dashscope" {
		t.Fatalf("client config 错误：%+v", cc)
	}
	rc := compiled.ModelRequestConfig
	if rc.ModelName != "qwen-plus" {
		t.Fatalf("model_name 错误：%s", rc.ModelName)
	}
	if rc.Temperature == nil || *rc.Temperature != 0.4 {
		t.Fatalf("temperature 错误：%v", rc.Temperature)
	}
	if rc.TopP != nil {
		t.Fatalf("top_p 应保持未设置（不得发明 0.95），得 %v", *rc.TopP)
	}
}

// 对齐 test_compile_single_model_preserves_explicit_top_p_value。
func TestCompileSingleModel_PreservesExplicitTopP(t *testing.T) {
	compiled, err := CompileModelSelection(map[string]any{
		"model_id": "m1", "model_name": "qwen-plus", "provider": "dashscope",
		"api_key": "sk", "api_base": "https://x",
		"request_defaults": map[string]any{"top_p": 0.95},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if compiled.ModelRequestConfig.TopP == nil || *compiled.ModelRequestConfig.TopP != 0.95 {
		t.Fatalf("top_p 应为 0.95，得 %v", compiled.ModelRequestConfig.TopP)
	}
}

// 对齐 test_compile_single_model_ignores_model_request_default_aliases
// （model/model_name 别名键不得进请求配置，compiler.py:41 _REQUEST_MODEL_KEYS）。
func TestCompileSingleModel_IgnoresModelRequestDefaultAliases(t *testing.T) {
	compiled, err := CompileModelSelection(map[string]any{
		"model_id": "m1", "model_name": "qwen-plus", "provider": "dashscope",
		"api_key": "sk", "api_base": "https://x", "endpoint_profile": "dashscope",
		"request_defaults": map[string]any{
			"model": "wrong-model", "model_name": "also-wrong", "temperature": 0.4,
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if compiled.ModelRequestConfig.ModelName != "qwen-plus" {
		t.Fatalf("别名键污染 model_name：%s", compiled.ModelRequestConfig.ModelName)
	}
	if compiled.ModelRequestConfig.Temperature == nil || *compiled.ModelRequestConfig.Temperature != 0.4 {
		t.Fatal("temperature 丢失")
	}
}

// 对齐 test_compile_model_group_without_strategy_uses_first_enabled_route_as_single_model
// （no-routing 短路，compiler.py:188-189）。
func TestCompileModelGroup_WithoutStrategy_UsesFirstEnabledRoute(t *testing.T) {
	compiled, err := CompileModelSelection(map[string]any{
		"model_group_id": "group-1",
		"request_config": map[string]any{"max_tokens": 1024},
		"routes":         groupTwoRoutesFixture(),
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if compiled.SelectedType != "model_group" || compiled.SelectedID != "group-1" {
		t.Fatalf("selected 错误：%s/%s", compiled.SelectedType, compiled.SelectedID)
	}
	cc := compiled.ModelClientConfig
	if cc.ClientProvider != "openai" || cc.IntelliRouter != nil {
		t.Fatalf("应为普通模型客户端：%+v", cc)
	}
	if cc.APIKey != "sk-a" || cc.APIBase != "https://api.openai.com" {
		t.Fatalf("取错路由：%+v", cc)
	}
	rc := compiled.ModelRequestConfig
	if rc.ModelName != "qwen-plus" || rc.ContextWindow == nil || *rc.ContextWindow != 100000 {
		t.Fatalf("request config 错误：%+v", rc)
	}
	if rc.Temperature == nil || *rc.Temperature != 0.4 {
		t.Fatal("temperature 丢失")
	}
	if rc.MaxTokens == nil || *rc.MaxTokens != 1024 {
		t.Fatal("组级 max_tokens 未合并")
	}
}

// 对齐 test_compile_model_group_none_strategy_preserves_reasoning_for_single_model_client
// （strategy=None 也走 no-routing，reasoning 原样透传）。
func TestCompileModelGroup_NoneStrategy_PreservesReasoning(t *testing.T) {
	compiled, err := CompileModelSelection(map[string]any{
		"model_group_id": "group-1",
		"routing":        map[string]any{"strategy": nil},
		"routes": []any{map[string]any{
			"route_id": "route-a",
			"model": map[string]any{
				"model_id": "m1", "model_name": "claude-sonnet-4-5", "provider": "anthropic",
				"api_key": "sk", "api_base": "https://api.anthropic.com",
				"request_defaults": map[string]any{
					"reasoning": map[string]any{"mode": "enabled", "budget_tokens": 2048},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	rc := compiled.ModelRequestConfig
	if rc.ModelName != "claude-sonnet-4-5" {
		t.Fatalf("model_name 错误：%s", rc.ModelName)
	}
	m, ok := rc.Reasoning.(map[string]any)
	if !ok {
		t.Fatalf("reasoning 应为 map，得 %T", rc.Reasoning)
	}
	if m["mode"] != "enabled" {
		t.Fatalf("reasoning.mode 错误：%v", m["mode"])
	}
	if v, ok := m["budget_tokens"].(float64); !ok || v != 2048 {
		t.Fatalf("reasoning.budget_tokens 错误：%v", m["budget_tokens"])
	}
}

// 对齐 test_compile_model_group_with_strategy_uses_intelli_router_and_min_context_window
// （组→IntelliRouterConfig；num_retries 缺省 len-1；context_window 取最小且不下发 deployment）。
func TestCompileModelGroup_WithStrategy_UsesIntelliRouterAndMinContextWindow(t *testing.T) {
	compiled, err := CompileModelSelection(map[string]any{
		"model_group_id": "group-1",
		"routing":        map[string]any{"strategy": "ordered-failover"},
		"request_config": map[string]any{"context_window": 64000, "max_tokens": 1024},
		"routes": []any{
			map[string]any{
				"route_id": "route-a",
				"model": map[string]any{
					"model_id": "m1", "model_name": "qwen-plus", "provider": "openai",
					"api_key": "sk-a", "api_base": "https://api.openai.com",
					"request_defaults": map[string]any{"context_window": 128000, "temperature": 0.4},
				},
			},
			map[string]any{
				"route_id":          "route-b",
				"request_overrides": map[string]any{"context_window": 16000},
				"model": map[string]any{
					"model_id": "m2", "model_name": "deepseek-chat", "provider": "deepseek",
					"api_key": "sk-b", "api_base": "https://api.deepseek.com",
					"request_defaults": map[string]any{"context_window": 32000},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cc := compiled.ModelClientConfig
	if cc.ClientProvider != "intelli_router" {
		t.Fatalf("client_provider 错误：%s", cc.ClientProvider)
	}
	rc := compiled.ModelRequestConfig
	if rc.ModelName != "" {
		t.Fatalf("路由组 request_config 不应带 model_name：%s", rc.ModelName)
	}
	if rc.ContextWindow == nil || *rc.ContextWindow != 16000 {
		t.Fatalf("context_window 应取最小 16000，得 %v", rc.ContextWindow)
	}
	router := cc.IntelliRouter
	if router == nil {
		t.Fatal("intelli_router 缺失")
	}
	if router.Strategy != "ordered-failover" {
		t.Fatalf("strategy 错误：%s", router.Strategy)
	}
	if router.NumRetries == nil || *router.NumRetries != 1 {
		t.Fatalf("num_retries 缺省应为 len-1=1，得 %v", router.NumRetries)
	}
	if len(router.Deployments) != 2 ||
		router.Deployments[0].RouteID != "route-a" || router.Deployments[1].RouteID != "route-b" {
		t.Fatalf("deployments 错误：%+v", router.Deployments)
	}
	d0 := router.Deployments[0].RequestDefaults
	if len(d0) != 2 || d0["temperature"] != 0.4 || d0["max_tokens"] != float64(1024) {
		t.Fatalf("deployment[0] request_defaults 错误：%v", d0)
	}
	if _, exists := router.Deployments[1].RequestDefaults["context_window"]; exists {
		t.Fatal("context_window 不得下发给 deployment")
	}
}

// 对齐 test_compile_anthropic_group_with_dashscope_profile_uses_anthropic_router_adapter
// （anthropic 强制补 api_mode 后 reasoning 展开为 thinking）。
func TestCompileAnthropicGroup_DashscopeProfile_UsesAnthropicRouterAdapter(t *testing.T) {
	compiled, err := CompileModelSelection(map[string]any{
		"model_group_id": "group-1",
		"routing":        map[string]any{"strategy": "ordered-failover"},
		"routes": []any{map[string]any{
			"route_id": "route-a",
			"model": map[string]any{
				"model_id": "m1", "model_name": "glm-5.2", "provider": "anthropic",
				"api_key": "sk", "api_base": "https://dashscope.aliyuncs.com/apps/anthropic",
				"endpoint_profile": "dashscope",
				"request_defaults": map[string]any{
					"reasoning": map[string]any{"mode": "enabled", "budget_tokens": 2048},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	router := compiled.ModelClientConfig.IntelliRouter
	if router == nil || len(router.Deployments) != 1 {
		t.Fatalf("router config 错误：%+v", router)
	}
	d := router.Deployments[0]
	if d.Provider != "anthropic" {
		t.Fatalf("deployment.provider 应为 anthropic，得 %s", d.Provider)
	}
	if d.EndpointProfile != "dashscope" {
		t.Fatalf("endpoint_profile 错误：%s", d.EndpointProfile)
	}
	thinking, ok := d.RequestDefaults["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking 缺失：%v", d.RequestDefaults)
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking.type 错误：%v", thinking["type"])
	}
	if v, ok := thinking["budget_tokens"].(float64); !ok || v != 2048 {
		t.Fatalf("thinking.budget_tokens 错误：%v", thinking["budget_tokens"])
	}
}

// 对齐 test_compile_tag_filtered_strategy_kwargs。
func TestCompileTagFiltered_StrategyKwargs(t *testing.T) {
	compiled, err := CompileModelSelection(map[string]any{
		"model_group_id": "group-1",
		"routing": map[string]any{
			"strategy":        "tag-filtered",
			"strategy_kwargs": map[string]any{"fallback_tag": "fast-model"},
		},
		"routes": []any{map[string]any{
			"route_id": "route-a",
			"model": map[string]any{
				"model_id": "m1", "model_name": "gpt-4o-mini", "provider": "openai",
				"api_key": "sk", "api_base": "https://api.openai.com",
				"fallback_tag": "fast-model",
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	router := compiled.ModelClientConfig.IntelliRouter
	if router == nil || router.Strategy != "tag-filtered" {
		t.Fatalf("strategy 错误：%+v", router)
	}
	if v, ok := router.StrategyKwargs["fallback_tag"].(string); !ok || v != "fast-model" {
		t.Fatalf("strategy_kwargs 错误：%v", router.StrategyKwargs)
	}
}

// 对齐 test_compile_tag_filtered_rejects_non_string_fallback_tag。
func TestCompileTagFiltered_RejectsNonStringFallbackTag(t *testing.T) {
	_, err := CompileModelSelection(map[string]any{
		"model_group_id": "group-1",
		"routing": map[string]any{
			"strategy":        "tag-filtered",
			"strategy_kwargs": map[string]any{"fallback_tag": []any{"fast-model"}},
		},
		"routes": []any{map[string]any{
			"route_id": "route-a",
			"model": map[string]any{
				"model_id": "m1", "model_name": "gpt-4o-mini", "provider": "openai",
				"api_key": "sk", "api_base": "https://api.openai.com",
			},
		}},
	})
	assertBaseError(t, err, exception.StatusModelGroupInvalid)
}

// 对齐 test_compile_model_group_rejects_invalid_no_routing_strategy_strings
// （strategy="" / "none" 非法；pydantic 枚举校验，无法复刻其消息文本，
// Go 侧消息见 strategyValue 注释偏差）。
func TestCompileModelGroup_RejectsInvalidNoRoutingStrategyStrings(t *testing.T) {
	for _, strategy := range []string{"", "none"} {
		_, err := CompileModelSelection(map[string]any{
			"model_group_id": "group-1",
			"routing":        map[string]any{"strategy": strategy},
			"routes":         groupOneRouteFixture(),
		})
		assertBaseError(t, err, exception.StatusModelRequestConfigInvalid)
	}
}

// 对齐 test_compile_model_group_rejects_when_no_enabled_routes
// （compiler.py:223-230 _enabled_routes）。
func TestCompileModelGroup_RejectsWhenNoEnabledRoutes(t *testing.T) {
	routes := groupOneRouteFixture()
	routes[0].(map[string]any)["enabled"] = false
	_, err := CompileModelSelection(map[string]any{
		"model_group_id": "group-1", "routes": routes,
	})
	assertBaseError(t, err, exception.StatusModelGroupNoAvailableRoute)
	if err != nil && !strings.Contains(err.Error(), "model_group_id=group-1 has no enabled routes") {
		t.Fatalf("错误消息漂移：%v", err)
	}
}

// 对齐 test_get_model_group_models_builds_callable_candidates_from_enabled_routes
// （compiler.py:116-142 get_model_group_models）。
func TestGetModelGroupModels_BuildsCallableCandidates(t *testing.T) {
	group := mustParseGroup(t, map[string]any{
		"model_group_id": "group-1",
		"request_config": map[string]any{"max_tokens": 1024},
		"routes": []any{
			map[string]any{
				"route_id":          "route-a",
				"request_overrides": map[string]any{"top_p": 0.8},
				"model": map[string]any{
					"model_id": "m1", "model_name": "deepseek-chat", "provider": "openai",
					"api_key": "sk-a", "api_base": "https://api.deepseek.com",
					"endpoint_profile":  "deepseek",
					"fallback_tag":      "general",
					"model_description": "general model",
					"request_defaults": map[string]any{
						"temperature": 0.2,
						"reasoning":   map[string]any{"mode": "enabled", "effort": "high"},
					},
				},
			},
			map[string]any{
				"route_id": "route-b", "enabled": false,
				"model": map[string]any{
					"model_id": "m2", "model_name": "deepseek-chat", "provider": "deepseek",
					"api_key": "sk-b", "api_base": "https://api.deepseek.com",
				},
			},
			map[string]any{
				"route_id": "route-c",
				"model": map[string]any{
					"model_id": "m3", "model_name": "claude-sonnet-4-5", "provider": "anthropic",
					"api_key": "sk-c", "api_base": "https://api.anthropic.com",
				},
			},
		},
	})

	candidates, err := GetModelGroupModels(group)
	if err != nil {
		t.Fatalf("get models: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("应只有 route-a/route-c 两个候选，得 %d", len(candidates))
	}
	c0 := candidates[0]
	if c0.RouteID != "route-a" || c0.ModelID != "m1" || c0.ModelName != "deepseek-chat" {
		t.Fatalf("候选 0 基础字段错误：%+v", c0)
	}
	if c0.ClientProvider != "openai" {
		t.Fatalf("client_provider 错误：%s", c0.ClientProvider)
	}
	if c0.ProviderKey != "deepseek" {
		t.Fatalf("provider_key 应为 deepseek，得 %s", c0.ProviderKey)
	}
	if c0.FallbackTag == nil || *c0.FallbackTag != "general" {
		t.Fatal("fallback_tag 丢失")
	}
	if c0.ModelDescription == nil || *c0.ModelDescription != "general model" {
		t.Fatal("model_description 丢失")
	}
	if c0.ClientConfig.APIKey != "sk-a" || c0.ClientConfig.EndpointProfile != "deepseek" {
		t.Fatalf("client config 错误：%+v", c0.ClientConfig)
	}
	rc := c0.Model
	if rc.ModelName != "deepseek-chat" || rc.Temperature == nil || *rc.Temperature != 0.2 {
		t.Fatalf("request config 错误：%+v", rc)
	}
	if rc.TopP == nil || *rc.TopP != 0.8 {
		t.Fatal("top_p 覆盖丢失")
	}
	if rc.MaxTokens == nil || *rc.MaxTokens != 1024 {
		t.Fatal("组级 max_tokens 丢失")
	}
	m, ok := rc.Reasoning.(map[string]any)
	if !ok || m["effort"] != "high" {
		t.Fatalf("reasoning.effort 丢失：%v", rc.Reasoning)
	}
	if c1 := candidates[1]; c1.RouteID != "route-c" || c1.ModelID != "m3" {
		t.Fatalf("候选 1 错误：%+v", c1)
	}
}

// ──────────────────────────── 非导出函数（fixture）────────────────────────────

func groupOneRouteFixture() []any {
	return []any{map[string]any{
		"route_id": "route-a",
		"model": map[string]any{
			"model_id": "m1", "model_name": "qwen-plus", "provider": "openai",
			"api_key": "sk-a", "api_base": "https://api.openai.com",
		},
	}}
}

func groupTwoRoutesFixture() []any {
	return []any{
		map[string]any{
			"route_id": "route-a",
			"model": map[string]any{
				"model_id": "m1", "model_name": "qwen-plus", "provider": "openai",
				"api_key": "sk-a", "api_base": "https://api.openai.com",
				"request_defaults": map[string]any{"context_window": 100000, "temperature": 0.4},
			},
		},
		map[string]any{
			"route_id": "route-b",
			"model": map[string]any{
				"model_id": "m2", "model_name": "deepseek-chat", "provider": "deepseek",
				"api_key": "sk-b", "api_base": "https://api.deepseek.com",
				"request_defaults": map[string]any{"context_window": 10000},
			},
		},
	}
}

func mustParseGroup(t *testing.T, data map[string]any) *ResolvedModelGroup {
	t.Helper()
	group, err := coerceGroup(data)
	if err != nil {
		t.Fatalf("coerceGroup: %v", err)
	}
	return group
}

func assertBaseError(t *testing.T, err error, status exception.StatusCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("应返回错误（%s）", status.Name())
	}
	base, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("应为 BaseError，得 %T", err)
	}
	if base.Status().Code() != status.Code() {
		t.Fatalf("状态码应为 %s(%d)，得 %s(%d)",
			status.Name(), status.Code(), base.Status().Name(), base.Status().Code())
	}
}
