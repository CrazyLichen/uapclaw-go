package common

import "testing"

// newResolverConfig 构造五级回退测试目录：默认组 + 默认模型 + alias。
func newResolverConfig() map[string]any {
	return map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_id": "mdl_a", "is_default": true, "alias": "alias-a",
					"model_client_config": map[string]any{"model_name": "gpt-a", "client_provider": "openai", "api_base": "https://a", "api_key": "sk"},
					"model_config_obj":    map[string]any{"reasoning_level": "high"},
				},
				map[string]any{
					"model_id": "mdl_b",
					"model_client_config": map[string]any{"model_name": "gpt-b", "client_provider": "openai"},
				},
			},
			"groups": []any{
				map[string]any{
					"model_group_id": "mgp_a", "is_default": true, "enabled": true,
					"request_config": map[string]any{"temperature": 0.5},
					"routing":        map[string]any{"timeout_seconds": 30},
					"routes": []any{
						map[string]any{"route_id": "rte_1", "model_id": "mdl_a", "enabled": true, "tpm": 100, "request_overrides": map[string]any{"temperature": 0.1}},
						map[string]any{"route_id": "rte_2", "model_id": "mdl_b", "enabled": false},
					},
				},
			},
		},
	}
}

// Choose 五级回退顺序。
// 对齐 Python: model_routing_registry.py:45-74。
func TestChoose_五级回退顺序(t *testing.T) {
	r := NewModelSelectionResolver(NewModelCatalog(newResolverConfig()))
	// ① explicit
	got, err := r.Choose(&ModelSelection{Type: "model", ID: "mdl_b"}, &ModelExecutionContext{})
	if err != nil || got.ID != "mdl_b" {
		t.Errorf("explicit 应优先: %v %v", got, err)
	}
	// ② session
	got, err = r.Choose(nil, &ModelExecutionContext{SessionSelection: &ModelSelection{Type: "model", ID: "mdl_a"}})
	if err != nil || got.ID != "mdl_a" {
		t.Errorf("session 应次之: %v %v", got, err)
	}
	// ③ spec
	got, err = r.Choose(nil, &ModelExecutionContext{SpecSelection: &ModelSelection{Type: "model_group", ID: "mgp_a"}})
	if err != nil || got.ID != "mgp_a" {
		t.Errorf("spec 应次之: %v %v", got, err)
	}
	// ④ 默认组
	got, err = r.Choose(nil, &ModelExecutionContext{})
	if err != nil || got.Type != "model_group" || got.ID != "mgp_a" {
		t.Errorf("应回落默认组: %v %v", got, err)
	}
	// ⑤ legacy 唯一匹配（alias-a 只命中 mdl_a）——须先移除默认组（组级回退挡在 legacy 之前）
	rNoGroup := NewModelSelectionResolver(NewModelCatalog(noGroupConfig()))
	got, err = rNoGroup.Choose(nil, &ModelExecutionContext{LegacyModelName: "alias-a"})
	if err != nil || got.ID != "mdl_a" {
		t.Errorf("legacy 唯一匹配应生效: %v %v", got, err)
	}
	// 全空目录 → MODEL_GROUP_INVALID "no model is configured"
	empty := NewModelSelectionResolver(NewModelCatalog(map[string]any{"models": map[string]any{}}))
	_, err = empty.Choose(nil, nil)
	if err == nil || err.Error() != "no model is configured" {
		t.Errorf("全空应报错: %v", err)
	}
}

// noGroupConfig 无默认组的目录（legacy 回退可达）。
func noGroupConfig() map[string]any {
	cfg := newResolverConfig()
	groups := cfg["models"].(map[string]any)["groups"].([]any)
	groups[0].(map[string]any)["is_default"] = false
	return cfg
}

// Choose legacy 多命中跳过 → 回落首个默认模型。
// 对齐 Python: model_routing_registry.py:59-68（len(matches) == 1 才用）。
func TestChoose_Legacy多命中跳过(t *testing.T) {
	cfg := noGroupConfig()
	models := cfg["models"].(map[string]any)
	// 两个模型同 alias → 多命中
	models["defaults"].([]any)[1].(map[string]any)["alias"] = "alias-a"
	r := NewModelSelectionResolver(NewModelCatalog(cfg))
	got, err := r.Choose(nil, &ModelExecutionContext{LegacyModelName: "alias-a"})
	if err != nil || got.ID != "mdl_a" {
		t.Errorf("多命中应跳过 legacy、回落首个默认模型 mdl_a: %v %v", got, err)
	}
}

// Resolve model 直取 + request_defaults 组装。
// 对齐 Python: model_routing_registry.py:107-115/:87-92。
func TestResolve_单模型与requestDefaults(t *testing.T) {
	r := NewModelSelectionResolver(NewModelCatalog(newResolverConfig()))
	got, err := r.Resolve(&ModelSelection{Type: "model", ID: "mdl_a"}, nil)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	model, ok := got.(ResolvedModel)
	if !ok {
		t.Fatalf("应返回 ResolvedModel")
	}
	if model.ModelID != "mdl_a" || model.Source != "defaults" || model.Provider != "openai" || model.APIKey != "sk" {
		t.Errorf("字段不符: %+v", model)
	}
	if _, exists := model.RequestDefaults["model"]; exists {
		t.Errorf("request_defaults 应 pop model")
	}
	if _, exists := model.ClientOptions["model_name"]; exists {
		t.Errorf("client_options 应剔除 model_name 保留键")
	}
	if _, exists := model.ClientOptions["api_key"]; exists {
		t.Errorf("client_options 应剔除 api_key 保留键")
	}
}

// Resolve 模型组：路由收窄、enabled 三闸、组装。
// 对齐 Python: model_routing_registry.py:116-156。
func TestResolve_模型组与路由闸(t *testing.T) {
	r := NewModelSelectionResolver(NewModelCatalog(newResolverConfig()))
	// 全组
	got, err := r.Resolve(&ModelSelection{Type: "model_group", ID: "mgp_a"}, nil)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	group := got.(ResolvedModelGroup)
	if group.ModelGroupID != "mgp_a" || len(group.Routes) != 2 {
		t.Fatalf("全组应含 2 条路由: %+v", group)
	}
	if group.RequestConfig["temperature"] != 0.5 || group.Routing["timeout_seconds"] != float64(30) {
		t.Errorf("request_config/routing 应透传: %v %v", group.RequestConfig, group.Routing)
	}
	// 收窄到 rte_1
	got, _ = r.Resolve(&ModelSelection{Type: "model_group", ID: "mgp_a", RouteID: "rte_1"}, nil)
	group = got.(ResolvedModelGroup)
	if len(group.Routes) != 1 || group.Routes[0].RouteID != "rte_1" {
		t.Errorf("收窄应只剩 rte_1: %+v", group.Routes)
	}
	// unknown route_id → NOT_FOUND
	_, err = r.Resolve(&ModelSelection{Type: "model_group", ID: "mgp_a", RouteID: "rte_x"}, nil)
	if err == nil || err.Error() != "unknown route_id \"rte_x\" in model group \"mgp_a\"" {
		t.Errorf("未知路由文案不符: %v", err)
	}
	// disabled route → DISABLED
	_, err = r.Resolve(&ModelSelection{Type: "model_group", ID: "mgp_a", RouteID: "rte_2"}, nil)
	if err == nil || err.Error() != "route \"rte_2\" in model group \"mgp_a\" is disabled" {
		t.Errorf("禁用路由文案不符: %v", err)
	}
}

// Resolve 组级禁用与全禁用路由。
func TestResolve_组级闸(t *testing.T) {
	cfg := newResolverConfig()
	groups := cfg["models"].(map[string]any)["groups"].([]any)
	// 组禁用
	groups[0].(map[string]any)["enabled"] = false
	r := NewModelSelectionResolver(NewModelCatalog(cfg))
	_, err := r.Resolve(&ModelSelection{Type: "model_group", ID: "mgp_a"}, nil)
	if err == nil || err.Error() != "model group \"mgp_a\" is disabled" {
		t.Errorf("组禁用文案不符: %v", err)
	}
	// 全路由禁用（组 enabled）——快照在构造时固化，重造目录
	groups[0].(map[string]any)["enabled"] = true
	routes := groups[0].(map[string]any)["routes"].([]any)
	routes[0].(map[string]any)["enabled"] = false
	r = NewModelSelectionResolver(NewModelCatalog(cfg))
	_, err = r.Resolve(&ModelSelection{Type: "model_group", ID: "mgp_a"}, nil)
	if err == nil || err.Error() != "model group \"mgp_a\" has no enabled routes" {
		t.Errorf("全禁用路由文案不符: %v", err)
	}
	// 无模型名 = DISABLED
	cfg = newResolverConfig()
	defaults := cfg["models"].(map[string]any)["defaults"].([]any)
	defaults[1].(map[string]any)["model_client_config"].(map[string]any)["model_name"] = ""
	r = NewModelSelectionResolver(NewModelCatalog(cfg))
	_, err = r.Resolve(&ModelSelection{Type: "model", ID: "mdl_b"}, nil)
	if err == nil || err.Error() != "model \"mdl_b\" is disabled" {
		t.Errorf("无模型名禁用文案不符: %v", err)
	}
}

// Restore：存在原样返回；已删除回落默认。
// 对齐 Python: model_routing_registry.py:39-43 与 session_provisioner.py:788-790 消费语义。
func TestRestore_存在与回落(t *testing.T) {
	r := NewModelSelectionResolver(NewModelCatalog(newResolverConfig()))
	// 存在
	got, err := r.Restore(&ModelSelection{Type: "model", ID: "mdl_a"})
	if err != nil || got.ID != "mdl_a" {
		t.Errorf("存在的引用应原样: %v %v", got, err)
	}
	// 已删除（引用未知组）→ 回落默认组
	got, err = r.Restore(&ModelSelection{Type: "model_group", ID: "mgp_deleted"})
	if err != nil || got.ID != "mgp_a" {
		t.Errorf("已删除应回落默认组: %v %v", got, err)
	}
}

// 权限闸：model 与 model_group 两面。
// 对齐 Python: model_routing_registry.py:78-79/:116-117。
func TestResolve_权限闸(t *testing.T) {
	r := NewModelSelectionResolver(NewModelCatalog(newResolverConfig()))
	denied := func(kind, id string) bool { return false }
	_, err := r.Resolve(&ModelSelection{Type: "model", ID: "mdl_a"}, &ModelExecutionContext{CanAccess: denied})
	if err == nil || err.Error() != "model \"mdl_a\" is forbidden" {
		t.Errorf("模型权限闸文案不符: %v", err)
	}
	_, err = r.Resolve(&ModelSelection{Type: "model_group", ID: "mgp_a"}, &ModelExecutionContext{CanAccess: denied})
	if err == nil || err.Error() != "model group \"mgp_a\" is forbidden" {
		t.Errorf("组权限闸文案不符: %v", err)
	}
}
