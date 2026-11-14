// 把稳定业务选择解析为 Foundation 编译器输入 DTO。
// 对应 Python 代码：jiuwenswarm/common/model_routing_registry.py:1-155（完整版基线，裁决 D；
// gateway/common/model_routing_registry.py 为裁剪副本）。
package common

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ModelExecutionContext 解析时的上下文。
// Python: model_routing_registry.py:27 ModelExecutionContext（frozen dataclass）。
type ModelExecutionContext struct {
	// SessionSelection 会话级选择
	SessionSelection *ModelSelection
	// SpecSelection 规格级选择
	SpecSelection *ModelSelection
	// LegacyModelName 旧式模型名（兼容路径）
	LegacyModelName string
	// CanAccess 权限闸（Python Callable[[str, str], bool]，kind ∈ model/model_group；nil = 不过闸）
	CanAccess func(kind, id string) bool
}

// ModelSelectionResolver 选择解析器。
// Python: model_routing_registry.py:35 ModelSelectionResolver。
type ModelSelectionResolver struct {
	// catalog 目录（Python :36-37 构造注入，nil 时取全局目录）
	catalog *ModelCatalog
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelSelectionResolver 构造解析器。
// Python: model_routing_registry.py:36-37 __init__(catalog=None)。
func NewModelSelectionResolver(catalog *ModelCatalog) *ModelSelectionResolver {
	if catalog == nil {
		catalog = NewModelCatalog(nil)
	}
	return &ModelSelectionResolver{catalog: catalog}
}

// Restore 恢复一个保存的引用，仅在已被删除时回落。
// Python: model_routing_registry.py:39-43 restore。
func (r *ModelSelectionResolver) Restore(selection *ModelSelection) (*ModelSelection, error) {
	if r.catalog.HasSelection(selection) {
		return selection, nil
	}
	return r.Choose(nil, &ModelExecutionContext{})
}

// Choose 五级回退选择。
// Python: model_routing_registry.py:45-74 choose。
// 顺序：explicit → session → spec → 默认组 → legacy 唯一匹配 → 首个默认模型。
func (r *ModelSelectionResolver) Choose(explicit *ModelSelection, context *ModelExecutionContext) (*ModelSelection, error) {
	if context == nil {
		context = &ModelExecutionContext{}
	}
	// Python: :46-47 explicit 优先
	if explicit != nil {
		return explicit, nil
	}
	// Python: :48-49 session
	if context.SessionSelection != nil {
		return context.SessionSelection, nil
	}
	// Python: :50-51 spec
	if context.SpecSelection != nil {
		return context.SpecSelection, nil
	}
	// Python: :52-58 首个 enabled 且 is_default 的组
	for _, group := range r.catalog.Snapshot.Groups {
		// Python: :53 group.get("enabled", True) 默认真
		enabled := true
		if v, exists := group["enabled"]; exists {
			enabled = v == true
		}
		if enabled && group["is_default"] == true {
			return &ModelSelection{Type: "model_group", ID: asString(group["model_group_id"])}, nil
		}
	}
	// Python: :59-68 legacy_model_name 按模型名/alias 匹配，恰好一个命中才用
	if context.LegacyModelName != "" {
		matches := []*ModelSelection{}
		for _, model := range r.catalog.ListPublicModels() {
			if asString(model["model_name"]) == context.LegacyModelName || asString(model["alias"]) == context.LegacyModelName {
				matches = append(matches, &ModelSelection{Type: "model", ID: asString(model["model_id"])})
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	// Python: :69-74 默认模型，无默认取列表首个；全空抛 MODEL_GROUP_INVALID
	models := []*ModelSelection{}
	for _, model := range r.catalog.ListPublicModels() {
		if model["is_default"] == true {
			models = append(models, &ModelSelection{Type: "model", ID: asString(model["model_id"])})
		}
	}
	if len(models) == 0 {
		for _, model := range r.catalog.ListPublicModels() {
			models = append(models, &ModelSelection{Type: "model", ID: asString(model["model_id"])})
		}
	}
	if len(models) == 0 {
		return nil, NewModelSelectionError(ModelGroupInvalid, "no model is configured", nil)
	}
	return models[0], nil
}

// Resolve 解析选择为单模型或模型组。
// Python: model_routing_registry.py:107-156 resolve。
func (r *ModelSelectionResolver) Resolve(selection *ModelSelection, context *ModelExecutionContext) (ResolvedSelection, error) {
	if context == nil {
		context = &ModelExecutionContext{}
	}
	selected, err := r.Choose(selection, context)
	if err != nil {
		return nil, err
	}
	// Python: :113-115 model 直取
	if selected.Type == "model" {
		return r.resolveModel(selected.ID, context)
	}
	// Python: :116-117 组级权限闸
	if context.CanAccess != nil && !context.CanAccess("model_group", selected.ID) {
		return nil, NewModelSelectionError(ModelSelectionForbidden, fmt.Sprintf("model group %q is forbidden", selected.ID), nil)
	}
	group, err := r.catalog.GetGroup(selected.ID)
	if err != nil {
		return nil, err
	}
	// Python: :119-120 组级 enabled 闸
	enabled := true
	if v, exists := group["enabled"]; exists {
		enabled = v == true
	}
	if !enabled {
		return nil, NewModelSelectionError(ModelSelectionDisabled, fmt.Sprintf("model group %q is disabled", selected.ID), nil)
	}
	rawRoutes, _ := group["routes"].([]any)
	groupRoutes := rawRoutes
	// Python: :122-134 route_id 收窄
	if selected.RouteID != "" {
		var selectedRoutes []any
		for _, route := range groupRoutes {
			if rm, ok := route.(map[string]any); ok && asString(rm["route_id"]) == selected.RouteID {
				selectedRoutes = append(selectedRoutes, route)
			}
		}
		if len(selectedRoutes) == 0 {
			return nil, NewModelSelectionError(ModelSelectionNotFound, fmt.Sprintf("unknown route_id %q in model group %q", selected.RouteID, selected.ID), nil)
		}
		// Python: :129-133 收窄后第一条的 enabled 闸
		first := selectedRoutes[0].(map[string]any)
		routeEnabled := true
		if v, exists := first["enabled"]; exists {
			routeEnabled = v == true
		}
		if !routeEnabled {
			return nil, NewModelSelectionError(ModelSelectionDisabled, fmt.Sprintf("route %q in model group %q is disabled", selected.RouteID, selected.ID), nil)
		}
		groupRoutes = selectedRoutes
	}
	// Python: :135-136 至少一条 enabled 路由
	hasEnabled := false
	for _, route := range groupRoutes {
		if rm, ok := route.(map[string]any); ok {
			routeEnabled := true
			if v, exists := rm["enabled"]; exists {
				routeEnabled = v == true
			}
			if routeEnabled {
				hasEnabled = true
				break
			}
		}
	}
	if !hasEnabled {
		return nil, NewModelSelectionError(ModelGroupInvalid, fmt.Sprintf("model group %q has no enabled routes", selected.ID), nil)
	}
	// Python: :137-148 逐路由组装
	routes := []ResolvedRoute{}
	for _, route := range groupRoutes {
		rm, ok := route.(map[string]any)
		if !ok {
			continue
		}
		routeEnabled := true
		if v, exists := rm["enabled"]; exists {
			routeEnabled = v == true
		}
		model, err := r.resolveModel(asString(rm["model_id"]), context)
		if err != nil {
			return nil, err
		}
		routes = append(routes, ResolvedRoute{
			RouteID:          asString(rm["route_id"]),
			Model:            model,
			Enabled:          routeEnabled,
			RequestOverrides: orEmptyMap(rm["request_overrides"]),
			TPM:              asInt(rm["tpm"]),
			RPM:              asInt(rm["rpm"]),
			Timeout:          asFloat(rm["timeout"]),
		})
	}
	// Python: :149-150 空 routes 兜底
	if len(routes) == 0 {
		return nil, NewModelSelectionError(ModelGroupInvalid, fmt.Sprintf("model group %q has no routes", selected.ID), nil)
	}
	return ResolvedModelGroup{
		ModelGroupID:  selected.ID,
		Routes:        routes,
		RequestConfig: orEmptyMap(group["request_config"]),
		Routing:       orEmptyMap(group["routing"]),
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveModel 单模型解析。
// Python: model_routing_registry.py:76-105 _model。
func (r *ModelSelectionResolver) resolveModel(modelID string, context *ModelExecutionContext) (ResolvedModel, error) {
	hit, err := r.catalog.GetModel(modelID)
	if err != nil {
		return ResolvedModel{}, err
	}
	// Python: :78-79 模型级权限闸
	if context.CanAccess != nil && !context.CanAccess("model", modelID) {
		return ResolvedModel{}, NewModelSelectionError(ModelSelectionForbidden, fmt.Sprintf("model %q is forbidden", modelID), nil)
	}
	entry, source := hit.Entry, hit.Source
	mcc, _ := entry["model_client_config"].(map[string]any)
	if mcc == nil {
		mcc = map[string]any{}
	}
	mco, _ := entry["model_config_obj"].(map[string]any)
	if mco == nil {
		mco = map[string]any{}
	}
	modelDetail, _ := entry["model_detail"].(map[string]any)
	if modelDetail == nil {
		modelDetail = map[string]any{}
	}
	// Python: :83-84 无 model_name = 禁用
	modelName := asString(mcc["model_name"])
	if modelName == "" {
		return ResolvedModel{}, NewModelSelectionError(ModelSelectionDisabled, fmt.Sprintf("model %q is disabled", modelID), nil)
	}
	// Python: :85-86 client_options 剔除 4 保留键
	excluded := map[string]bool{"model_name": true, "client_provider": true, "api_base": true, "api_key": true}
	options := map[string]any{}
	for key, value := range mcc {
		if !excluded[key] {
			options[key] = value
		}
	}
	// Python: :87-92 request_defaults = build_reasoning_model_request_kwargs(...) 后 pop "model"
	defaults := BuildReasoningModelRequestKwargs(mcc, mco, modelName)
	delete(defaults, "model")
	// Python: :93-105 组装 ResolvedModel
	endpointProfile := firstNonEmpty(asString(entry["endpoint_profile"]), asString(mcc["endpoint_profile"]))
	return ResolvedModel{
		ModelID:          modelID,
		Source:           source,
		ModelName:        modelName,
		Provider:         asString(mcc["client_provider"]),
		APIBase:          asString(mcc["api_base"]),
		APIKey:           asString(mcc["api_key"]),
		EndpointProfile:  endpointProfile,
		FallbackTag:      firstNonEmpty(asString(modelDetail["fallback_tag"])),
		ModelDescription: firstNonEmpty(asString(modelDetail["model_description"])),
		ClientOptions:    options,
		RequestDefaults:  defaults,
	}, nil
}

// orEmptyMap 值转 map（nil → 空 map）。
func orEmptyMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// asInt 数值转 int（Python None → 0）。
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// asFloat 数值转 float64（Python None → 0）。
func asFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	default:
		return 0
	}
}
