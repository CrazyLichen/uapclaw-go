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

// ──────────────────────────── 覆盖率补充（spec §9 ≥85%）────────────────────────────

// coerceSelection typed DTO 直传路径（compiler.py:146-148）。
func TestCompileModelSelection_TypedDTO直传(t *testing.T) {
	model := NewResolvedModel()
	model.ModelID = "m1"
	model.ModelName = "qwen-plus"
	model.Provider = "openai"
	compiled, err := CompileModelSelection(model)
	if err != nil {
		t.Fatalf("typed model: %v", err)
	}
	if compiled.SelectedID != "m1" || compiled.SelectedType != "model" {
		t.Fatalf("selected 错误：%+v", compiled)
	}
	group := NewResolvedModelGroup()
	group.ModelGroupID = "g1"
	route := NewResolvedRoute()
	route.Model = NewResolvedModel()
	route.Model.ModelID = "m1"
	route.Model.ModelName = "qwen-plus"
	route.Model.Provider = "openai"
	group.Routes = []*ResolvedRoute{route}
	compiled, err = CompileModelSelection(group)
	if err != nil {
		t.Fatalf("typed group: %v", err)
	}
	if compiled.SelectedID != "g1" || compiled.SelectedType != "model_group" {
		t.Fatalf("selected 错误：%+v", compiled)
	}
	// 非 map 非 DTO → MODEL_REQUEST_CONFIG_INVALID
	if _, err := CompileModelSelection(42); err == nil {
		t.Fatal("非法载荷应报错")
	}
}

// GetModelGroupModels 错误路径（nil / 空 enabled）。
func TestGetModelGroupModels_错误路径(t *testing.T) {
	if _, err := GetModelGroupModels(nil); err == nil {
		t.Fatal("nil group 应报错")
	}
	group := NewResolvedModelGroup()
	group.ModelGroupID = "g1"
	route := NewResolvedRoute()
	route.Enabled = false
	route.Model = NewResolvedModel()
	route.Model.ModelID = "m1"
	group.Routes = []*ResolvedRoute{route}
	if _, err := GetModelGroupModels(group); err == nil {
		t.Fatal("无启用路由应报错")
	}
}

// applyRouterOptions / routingConfigMapping 全字段（compiler.py:277-301）。
func TestRouterOptions_全字段(t *testing.T) {
	strategy := RoutingStrategyOrderedFailover
	retries := 2
	timeoutSec := 12.0
	health := true
	interval := 60.0
	obs := true
	cfg := &ResolvedRoutingConfig{
		Strategy:                   &strategy,
		NumRetries:                 &retries,
		StrategyKwargs:             map[string]any{"fallback_tag": "fast"},
		TimeoutSeconds:             &timeoutSec,
		EnableHealthCheck:          &health,
		HealthCheckIntervalSeconds: &interval,
		EnableObservability:        &obs,
		Extra:                      map[string]any{"custom": 1},
	}
	// routingConfigMapping：DTO → map 逐键
	m := routingConfigMapping(cfg)
	if m["strategy"] != "ordered-failover" || m["custom"] != 1 {
		t.Fatalf("mapping 错误：%v", m)
	}
	// routerOptionsData → applyRouterOptions：全部四键生效
	_, _, _, options, _, err := routingConfigData(cfg)
	if err != nil {
		t.Fatalf("routingConfigData: %v", err)
	}
	if options["timeout"].(float64) != 12.0 || options["enable_health_check"] != true ||
		options["health_check_interval"].(float64) != 60.0 || options["enable_observability"] != true {
		t.Fatalf("options 错误：%v", options)
	}
	group := NewResolvedModelGroup()
	group.ModelGroupID = "g1"
	group.Routing = cfg
	route := NewResolvedRoute()
	route.Model = NewResolvedModel()
	route.Model.ModelID = "m1"
	route.Model.ModelName = "qwen-plus"
	route.Model.Provider = "openai"
	group.Routes = []*ResolvedRoute{route}
	compiled, err := CompileModelSelection(group)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	routerCfg := compiled.ModelClientConfig.IntelliRouter
	if routerCfg.Timeout != 12.0 || !routerCfg.EnableHealthCheck ||
		routerCfg.HealthCheckInterval != 60.0 || !routerCfg.EnableObservability {
		t.Fatalf("router options 未生效：%+v", routerCfg)
	}
	if routerCfg.NumRetries == nil || *routerCfg.NumRetries != 2 {
		t.Fatal("num_retries 应透传")
	}
	if routerCfg.StrategyKwargs["fallback_tag"] != "fast" {
		t.Fatal("strategy_kwargs 应透传")
	}
}

// validateModel 错误分支（compiler.py:308-318）。
func TestValidateModel_错误分支(t *testing.T) {
	model := NewResolvedModel()
	model.ModelID = "m1"
	model.ModelName = "qwen-plus"
	model.Provider = "openai"
	if err := validateModel(model); err != nil {
		t.Fatalf("合法模型不应报错：%v", err)
	}
	model.ModelName = "  "
	if err := validateModel(model); err == nil ||
		!containsSubstring(err.Error(), "has empty model_name") {
		t.Fatalf("空 model_name 应报错：%v", err)
	}
	model.ModelName = "qwen-plus"
	model.Provider = ""
	if err := validateModel(model); err == nil ||
		!containsSubstring(err.Error(), "has empty provider") {
		t.Fatalf("空 provider 应报错：%v", err)
	}
}

// compileModel 构造错误包装（compiler.py:174-178 configInvalidError）。
func TestCompileModel_构造错误包装(t *testing.T) {
	model := NewResolvedModel()
	model.ModelID = "m1"
	model.ModelName = "qwen-plus"
	model.Provider = "openai"
	// 非法 client_options（client_provider 类型错误）→ JSON 转换失败
	model.ClientOptions = map[string]any{"timeout": make(chan int)}
	_, _, err := compileModel(model)
	if err == nil {
		t.Fatal("非法 client_options 应报错")
	}
}

// strategy_kwargs 非 map 报错（compiler.py:259-263，经 JSON round-trip
// 进 ResolvedRoutingConfig.Extra 的非法形状）。
func TestRoutingConfigData_StrategyKwargs非Map(t *testing.T) {
	if _, err := coerceGroup(map[string]any{
		"model_group_id":  "g1",
		"strategy_kwargs": 42,
		"routes":          []any{},
	}); err != nil {
		t.Fatalf("coerceGroup: %v", err)
	}
	// 直接构造非法形状：Extra 携带非 map strategy_kwargs
	bad := &ResolvedRoutingConfig{StrategyKwargs: nil, Extra: map[string]any{"strategy_kwargs": 42}}
	_, _, _, _, _, err := routingConfigData(bad)
	if err == nil || !containsSubstring(err.Error(), "routing.strategy_kwargs must be a mapping") {
		t.Fatalf("非 map strategy_kwargs 应报错：%v", err)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// flattenExtraBody 非 map 分支 + deepMergeDicts 嵌套合并（compiler.py:431-466）。
func TestFlattenExtraBody_与深度合并(t *testing.T) {
	// 非 map extra_body 原样保留
	params := map[string]any{"extra_body": "raw-string"}
	flattenExtraBody(params)
	if params["extra_body"] != "raw-string" {
		t.Fatalf("非 map extra_body 应原样保留：%v", params)
	}
	// nil extra_body 删除
	params2 := map[string]any{"extra_body": nil}
	flattenExtraBody(params2)
	if _, exists := params2["extra_body"]; exists {
		t.Fatal("nil extra_body 应删除")
	}
	// deepMergeDicts：嵌套 map 递归 + None 跳过
	merged := deepMergeDicts(
		map[string]any{"a": map[string]any{"x": 1, "y": nil}, "keep": 1},
		map[string]any{"a": map[string]any{"y": 2}, "z": nil},
	)
	inner := merged["a"].(map[string]any)
	// deepCopyMap 经 JSON round-trip，整型归一为 float64（Python deepcopy
	// 保留 int；数值语义等价，登记为已知归一差异）
	if inner["x"] != float64(1) || inner["y"] != 2 {
		t.Fatalf("嵌套合并错误：%v", inner)
	}
	if merged["keep"] != float64(1) {
		t.Fatal("左侧非嵌套键保留")
	}
	if _, exists := merged["z"]; exists {
		t.Fatal("右侧 None 不覆盖")
	}
	// mergeRequestDefaults：嵌套 map 深合并路径
	out := mergeRequestDefaults(
		map[string]any{"opts": map[string]any{"temperature": 0.1}},
		map[string]any{"opts": map[string]any{"top_p": 0.9}},
	)
	opts := out["opts"].(map[string]any)
	if opts["temperature"] != 0.1 || opts["top_p"] != 0.9 {
		t.Fatalf("深合并错误：%v", opts)
	}
}

// deploymentData tpm/rpm/timeout + client_options 三键透传（compiler.py:338-348）。
func TestDeploymentData_路由级字段与三键(t *testing.T) {
	groupCfg := map[string]any{}
	route := NewResolvedRoute()
	route.RouteID = "r1"
	route.TPM = ptrInt(1000)
	route.RPM = ptrInt(10)
	route.Timeout = ptrFloat(15)
	route.Model = NewResolvedModel()
	route.Model.ModelID = "m1"
	route.Model.ModelName = "m"
	route.Model.Provider = "openai"
	route.Model.ClientOptions = map[string]any{
		"endpoint_profile": "siliconflow",
		"custom_headers":   map[string]any{"X-A": "b"},
		"verify_ssl":       false,
		"api_key":          "ignored",
	}
	data, err := deploymentData(route, groupCfg)
	if err != nil {
		t.Fatalf("deploymentData: %v", err)
	}
	if data["tpm"] != 1000 || data["rpm"] != 10 || data["timeout"] != float64(15) {
		t.Fatalf("路由级字段错误：%v", data)
	}
	if data["endpoint_profile"] != "siliconflow" || data["custom_headers"] == nil || data["verify_ssl"] != false {
		t.Fatalf("client_options 三键错误：%v", data)
	}
	// api_key 属于 client_options 过滤键，不得透传到 deployment 顶层覆盖
	if data["api_key"] != "" {
		// route.model.api_key 空串 → data["api_key"] 为 ""
		t.Fatalf("api_key 应取 model 字段：%v", data["api_key"])
	}
}

func ptrInt(i int) *int           { return &i }
func ptrFloat(f float64) *float64 { return &f }
