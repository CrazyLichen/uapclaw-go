package routing

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/reasoning"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 常量 ────────────────────────────

// clientConfigFields ModelClientConfig 的字段名集合（对齐 Python
// compiler.py:40 _CLIENT_CONFIG_FIELDS = set(ModelClientConfig.model_fields)）。
var clientConfigFields = map[string]struct{}{
	"client_id": {}, "client_provider": {}, "api_key": {}, "api_base": {},
	"timeout": {}, "stream_first_chunk_timeout": {}, "stream_idle_timeout": {},
	"max_retries": {}, "use_shared_llm_http_client": {}, "verify_ssl": {},
	"ssl_cert": {}, "custom_headers": {}, "api_mode": {}, "auth_mode": {},
	"endpoint_profile": {}, "extensions": {}, "legacy_client_provider": {},
	"intelli_router": {},
}

// requestModelKeys 请求配置中与模型名冲突的键（compiler.py:41）。
var requestModelKeys = map[string]struct{}{"model_name": {}, "model": {}}

// internalRequestKeys 内部键，不得进入请求配置（compiler.py:42-50）。
var internalRequestKeys = map[string]struct{}{
	"api_key": {}, "api_base": {}, "client_provider": {}, "deployment": {},
	"messages": {}, "provider": {}, "stream": {},
}

// coreOnlyRequestKeys 仅核心层消费的键，不下发给 deployment（compiler.py:51）。
var coreOnlyRequestKeys = map[string]struct{}{"context_window": {}}

// ──────────────────────────── 导出函数 ────────────────────────────

// CompileModelSelection 把解析后的模型/模型组编译为运行时 LLM 配置。
// 权限、归属与持久化仍由上层负责；本函数只校验载荷并转换为
// ModelClientConfig / ModelRequestConfig 对。
// Python: compiler.py:91-113 compile_model_selection
func CompileModelSelection(selection any) (*CompiledModelSelection, error) {
	resolved, err := coerceSelection(selection)
	if err != nil {
		return nil, err
	}
	switch r := resolved.(type) {
	case *ResolvedModel:
		clientConfig, requestConfig, err := compileModel(r)
		if err != nil {
			return nil, err
		}
		return &CompiledModelSelection{
			ModelClientConfig:  clientConfig,
			ModelRequestConfig: requestConfig,
			SelectedType:       "model",
			SelectedID:         r.ModelID,
		}, nil
	case *ResolvedModelGroup:
		clientConfig, requestConfig, err := compileModelGroup(r)
		if err != nil {
			return nil, err
		}
		return &CompiledModelSelection{
			ModelClientConfig:  clientConfig,
			ModelRequestConfig: requestConfig,
			SelectedType:       "model_group",
			SelectedID:         r.ModelGroupID,
		}, nil
	}
	// Python 侧无对应分支（coerceSelection 只可能返回两种类型）
	return nil, exception.BuildError(exception.StatusModelRequestConfigInvalid,
		exception.WithMsg("model selection payload must be a mapping or resolved model object"))
}

// GetModelGroupModels 从模型组的启用路由构建可调用模型候选。
// Python: compiler.py:116-142 get_model_group_models
func GetModelGroupModels(modelGroup *ResolvedModelGroup) ([]*ModelGroupModelCandidate, error) {
	if modelGroup == nil {
		// Python: compiler.py:118-119 TypeError 守卫
		return nil, fmt.Errorf("model_group must be a ResolvedModelGroup")
	}
	enabledRoutes, err := enabledRoutes(modelGroup)
	if err != nil {
		return nil, err
	}
	var candidates []*ModelGroupModelCandidate
	for _, route := range enabledRoutes {
		clientConfig, requestConfig, err := compileRouteAsModel(route, modelGroup.RequestConfig)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, &ModelGroupModelCandidate{
			// Python: compiler.py:129-140（Model(model_client_config=..., model_config=...)；
			// Go 侧拆两字段持有，见 schema.go ModelGroupModelCandidate 注释）
			Model:            requestConfig,
			ClientConfig:     clientConfig,
			RouteID:          route.RouteID,
			ModelID:          route.Model.ModelID,
			ModelName:        route.Model.ModelName,
			ClientProvider:   route.Model.Provider,
			ProviderKey:      reasoning.ProviderKeyOf(clientConfig),
			FallbackTag:      route.Model.FallbackTag,
			ModelDescription: route.Model.ModelDescription,
		})
	}
	return candidates, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// coerceSelection 归一选择载荷：DTO 直传，map 走 JSON round-trip 校验。
// Python: compiler.py:145-162 _coerce_selection（pydantic model_validate
// 由 JSON round-trip + 枚举校验模拟）。
func coerceSelection(selection any) (any, error) {
	switch s := selection.(type) {
	case *ResolvedModel:
		return s, nil
	case *ResolvedModelGroup:
		return s, nil
	case map[string]any:
		if _, hasRoutes := s["routes"]; hasRoutes {
			return coerceGroup(s)
		}
		return coerceModel(s)
	}
	return nil, exception.BuildError(exception.StatusModelRequestConfigInvalid,
		exception.WithMsg("model selection payload must be a mapping or resolved model object"))
}

// coerceModel map → ResolvedModel（JSON round-trip 模拟 pydantic 校验）。
func coerceModel(data map[string]any) (*ResolvedModel, error) {
	model, err := jsonRoundTripPtr[ResolvedModel](data)
	if err != nil {
		return nil, exception.BuildError(exception.StatusModelRequestConfigInvalid,
			exception.WithMsg(err.Error()))
	}
	if model.ClientOptions == nil {
		model.ClientOptions = map[string]any{}
	}
	if model.RequestDefaults == nil {
		model.RequestDefaults = map[string]any{}
	}
	return model, nil
}

// coerceGroup map → ResolvedModelGroup（JSON round-trip 模拟 pydantic 校验）。
func coerceGroup(data map[string]any) (*ResolvedModelGroup, error) {
	group, err := jsonRoundTripPtr[ResolvedModelGroup](data)
	if err != nil {
		return nil, exception.BuildError(exception.StatusModelRequestConfigInvalid,
			exception.WithMsg(err.Error()))
	}
	if group.Routing == nil {
		group.Routing = &ResolvedRoutingConfig{StrategyKwargs: map[string]any{}}
	}
	if group.RequestConfig == nil {
		group.RequestConfig = map[string]any{}
	}
	return group, nil
}

// compileModel 编译单模型（compiler.py:165-178 _compile_model）。
func compileModel(model *ResolvedModel) (*schema.ModelClientConfig, *schema.ModelRequestConfig, error) {
	if err := validateModel(model); err != nil {
		return nil, nil, err
	}
	clientData := clientConfigData(model)
	requestDefaults := requestConfigData(model.RequestDefaults)
	clientConfig, err := buildClientConfig(clientData)
	if err != nil {
		return nil, nil, configInvalidError(err)
	}
	requestData := deepCopyMap(requestDefaults)
	requestData["model"] = model.ModelName
	requestConfig, err := buildRequestConfig(requestData)
	if err != nil {
		return nil, nil, configInvalidError(err)
	}
	return clientConfig, requestConfig, nil
}

// compileModelGroup 编译模型组（compiler.py:181-220 _compile_model_group）。
func compileModelGroup(modelGroup *ResolvedModelGroup) (*schema.ModelClientConfig, *schema.ModelRequestConfig, error) {
	enabledRoutes, err := enabledRoutes(modelGroup)
	if err != nil {
		return nil, nil, err
	}
	strategy, numRetries, strategyKwargs, routerOptions, noRouting, err := routingConfigData(modelGroup.Routing)
	if err != nil {
		return nil, nil, err
	}
	if noRouting {
		clientConfig, requestConfig, err := compileRouteAsModel(enabledRoutes[0], modelGroup.RequestConfig)
		if err != nil {
			return nil, nil, err
		}
		return clientConfig, requestConfig, nil
	}
	var deployments []schema.IntelliRouterDeploymentConfig
	for _, route := range enabledRoutes {
		data, err := deploymentData(route, modelGroup.RequestConfig)
		if err != nil {
			return nil, nil, err
		}
		deployment, err := jsonRoundTripPtr[schema.IntelliRouterDeploymentConfig](data)
		if err != nil {
			return nil, nil, err
		}
		if deployment.RequestDefaults == nil {
			deployment.RequestDefaults = map[string]any{}
		}
		deployments = append(deployments, *deployment)
	}
	if numRetries == nil {
		// Python: compiler.py:192-193 num_retries = max(len(deployments) - 1, 0)
		n := len(deployments) - 1
		if n < 0 {
			n = 0
		}
		numRetries = &n
	}
	routerConfig := schema.NewDefaultIntelliRouterConfig()
	routerConfigPtr := &routerConfig
	if modelGroup.ModelGroupID != "" {
		id := modelGroup.ModelGroupID
		routerConfigPtr.ModelGroupID = &id
	}
	routerConfigPtr.Deployments = deployments
	routerConfigPtr.Strategy = strategy
	routerConfigPtr.NumRetries = numRetries
	routerConfigPtr.StrategyKwargs = strategyKwargs
	applyRouterOptions(routerConfigPtr, routerOptions)
	clientConfig := &schema.ModelClientConfig{
		ClientProvider: "intelli_router",
		IntelliRouter:  routerConfigPtr,
	}
	requestData := map[string]any{}
	if contextWindow := minContextWindow(enabledRoutes, modelGroup.RequestConfig); contextWindow != nil {
		requestData["context_window"] = *contextWindow
	}
	requestConfig, err := buildRequestConfig(requestData)
	if err != nil {
		return nil, nil, groupInvalidError(err)
	}
	return clientConfig, requestConfig, nil
}

// enabledRoutes 过滤启用路由，空则报 MODEL_GROUP_NO_AVAILABLE_ROUTE
// （compiler.py:223-230 _enabled_routes）。
func enabledRoutes(modelGroup *ResolvedModelGroup) ([]*ResolvedRoute, error) {
	var enabled []*ResolvedRoute
	for _, route := range modelGroup.Routes {
		if route.Enabled {
			enabled = append(enabled, route)
		}
	}
	if len(enabled) == 0 {
		return nil, exception.BuildError(exception.StatusModelGroupNoAvailableRoute,
			exception.WithMsg(fmt.Sprintf("model_group_id=%s has no enabled routes", modelGroup.ModelGroupID)))
	}
	return enabled, nil
}

// compileRouteAsModel 把一条路由当普通模型编译
// （compiler.py:233-245 _compile_route_as_model）。
func compileRouteAsModel(route *ResolvedRoute, groupRequestConfig map[string]any) (*schema.ModelClientConfig, *schema.ModelRequestConfig, error) {
	requestDefaults := mergeRequestDefaults(route.Model.RequestDefaults, groupRequestConfig, route.RequestOverrides)
	selectedModel := *route.Model
	selectedModel.RequestDefaults = requestDefaults
	return compileModel(&selectedModel)
}

// routingConfigData 解析路由配置（compiler.py:248-274 _routing_config_data）。
func routingConfigData(routingConfig *ResolvedRoutingConfig) (string, *int, map[string]any, map[string]any, bool, error) {
	routing := routingConfigMapping(routingConfig)
	// Python: compiler.py:252-253 raw_strategy = routing.pop("strategy"); no_routing = raw_strategy is None
	rawStrategy, hasStrategy := routing["strategy"]
	noRouting := !hasStrategy || rawStrategy == nil
	strategy := ""
	if !noRouting {
		var err error
		strategy, err = strategyValue(rawStrategy)
		if err != nil {
			return "", nil, nil, nil, false, err
		}
	}
	// routingConfigMapping 产物为 int/float64 值；pydantic 路径（JSON round-trip
	// 后 ResolvedRoutingConfig.NumRetries 已是 *int，经 mapping 转为值）。
	var numRetries *int
	switch v := routing["num_retries"].(type) {
	case *int:
		numRetries = v
	case int:
		numRetries = &v
	case float64:
		n := int(v)
		numRetries = &n
	}
	nestedKwargs, ok := routing["strategy_kwargs"].(map[string]any)
	if routing["strategy_kwargs"] == nil {
		nestedKwargs = map[string]any{}
		ok = true
	}
	if !ok {
		return "", nil, nil, nil, false, exception.BuildError(exception.StatusModelGroupInvalid,
			exception.WithMsg("routing.strategy_kwargs must be a mapping"))
	}
	strategyKwargs := deepCopyMap(nestedKwargs)
	if strategy == "tag-filtered" {
		// Python: compiler.py:266-272 fallback_tag 必须是 string
		if tag, exists := strategyKwargs["fallback_tag"]; exists && tag != nil {
			if _, isString := tag.(string); !isString {
				return "", nil, nil, nil, false, exception.BuildError(exception.StatusModelGroupInvalid,
					exception.WithMsg("routing.strategy_kwargs.fallback_tag must be a string"))
			}
		}
	}
	routerOptions := routerOptionsData(routingConfig)
	return strategy, numRetries, strategyKwargs, routerOptions, noRouting, nil
}

// routerOptionsData 提取路由器选项（compiler.py:277-291 _router_options_data）。
func routerOptionsData(routingConfig *ResolvedRoutingConfig) map[string]any {
	routing := routingConfigMapping(routingConfig)
	fieldMap := map[string]string{
		"timeout_seconds":               "timeout",
		"enable_health_check":           "enable_health_check",
		"health_check_interval_seconds": "health_check_interval",
		"enable_observability":          "enable_observability",
	}
	options := map[string]any{}
	for sourceKey, targetKey := range fieldMap {
		if v, exists := routing[sourceKey]; exists && v != nil {
			options[targetKey] = v
		}
	}
	return options
}

// applyRouterOptions 把 routerOptionsData 产物写入 router config（类型分派）。
func applyRouterOptions(routerConfig *schema.IntelliRouterConfig, options map[string]any) {
	if v, ok := options["timeout"].(float64); ok {
		routerConfig.Timeout = v
	}
	if v, ok := options["enable_health_check"].(bool); ok {
		routerConfig.EnableHealthCheck = v
	}
	if v, ok := options["health_check_interval"].(float64); ok {
		routerConfig.HealthCheckInterval = v
	}
	if v, ok := options["enable_observability"].(bool); ok {
		routerConfig.EnableObservability = v
	}
}

// routingConfigMapping 归一为 map（compiler.py:294-301 _routing_config_mapping）。
func routingConfigMapping(routingConfig *ResolvedRoutingConfig) map[string]any {
	if routingConfig == nil {
		return map[string]any{}
	}
	merged := map[string]any{}
	if routingConfig.Strategy != nil {
		merged["strategy"] = string(*routingConfig.Strategy)
	}
	if routingConfig.NumRetries != nil {
		merged["num_retries"] = *routingConfig.NumRetries
	}
	if routingConfig.StrategyKwargs != nil {
		merged["strategy_kwargs"] = routingConfig.StrategyKwargs
	}
	if routingConfig.TimeoutSeconds != nil {
		merged["timeout_seconds"] = *routingConfig.TimeoutSeconds
	}
	if routingConfig.EnableHealthCheck != nil {
		merged["enable_health_check"] = *routingConfig.EnableHealthCheck
	}
	if routingConfig.HealthCheckIntervalSeconds != nil {
		merged["health_check_interval_seconds"] = *routingConfig.HealthCheckIntervalSeconds
	}
	if routingConfig.EnableObservability != nil {
		merged["enable_observability"] = *routingConfig.EnableObservability
	}
	for k, v := range routingConfig.Extra {
		merged[k] = v
	}
	return merged
}

// strategyValue 策略取值校验（compiler.py:304-305 _strategy_value +
// pydantic RoutingStrategyName 枚举校验）。
// Python 侧非法值错误消息为 pydantic ValidationError 文本，无法逐字复刻；
// Go 侧使用等义消息，状态码对齐 MODEL_REQUEST_CONFIG_INVALID（compiler.py:158-162）。
func strategyValue(strategy any) (string, error) {
	s, ok := strategy.(string)
	if !ok {
		return "", exception.BuildError(exception.StatusModelRequestConfigInvalid,
			exception.WithMsg("routing.strategy must be one of [ordered-failover, tag-filtered]"))
	}
	switch RoutingStrategyName(s) {
	case RoutingStrategyOrderedFailover, RoutingStrategyTagFiltered:
		return s, nil
	}
	return "", exception.BuildError(exception.StatusModelRequestConfigInvalid,
		exception.WithMsg("routing.strategy must be one of [ordered-failover, tag-filtered]"))
}

// validateModel 模型名/provider 非空校验（compiler.py:308-318 _validate_model）。
func validateModel(model *ResolvedModel) error {
	if strings.TrimSpace(model.ModelName) == "" {
		return exception.BuildError(exception.StatusModelRequestConfigInvalid,
			exception.WithMsg(fmt.Sprintf("model_id=%s has empty model_name", model.ModelID)))
	}
	if strings.TrimSpace(model.Provider) == "" {
		return exception.BuildError(exception.StatusModelRequestConfigInvalid,
			exception.WithMsg(fmt.Sprintf("model_id=%s has empty provider", model.ModelID)))
	}
	return nil
}

// deploymentData 组装单条 deployment 配置（compiler.py:321-349 _deployment_data）。
func deploymentData(route *ResolvedRoute, groupRequestConfig map[string]any) (map[string]any, error) {
	if err := validateModel(route.Model); err != nil {
		return nil, err
	}
	requestDefaults, err := providerRequestDefaults(route, groupRequestConfig)
	if err != nil {
		return nil, err
	}
	clientOptions := deepCopyMap(route.Model.ClientOptions)
	data := map[string]any{
		"route_id":         route.RouteID,
		"model_id":         route.Model.ModelID,
		"model_name":       route.Model.ModelName,
		"api_key":          route.Model.APIKey,
		"api_base":         route.Model.APIBase,
		"provider":         routerProvider(route.Model),
		"request_defaults": requestDefaults,
	}
	if route.Model.FallbackTag != nil {
		data["fallback_tag"] = *route.Model.FallbackTag
	}
	if route.Model.ModelDescription != nil {
		data["model_description"] = *route.Model.ModelDescription
	}
	// Python: compiler.py:338-341 tpm/rpm/timeout 非空才写入
	if route.TPM != nil {
		data["tpm"] = *route.TPM
	}
	if route.RPM != nil {
		data["rpm"] = *route.RPM
	}
	if route.Timeout != nil {
		data["timeout"] = *route.Timeout
	}
	if route.Model.EndpointProfile != nil {
		data["endpoint_profile"] = *route.Model.EndpointProfile
	}
	// Python: compiler.py:344-348 endpoint_profile/custom_headers/verify_ssl
	// 从 client_options 透传（endpoint_profile 已在 data 中则跳过）
	for _, key := range []string{"endpoint_profile", "custom_headers", "verify_ssl"} {
		if key == "endpoint_profile" {
			if _, exists := data["endpoint_profile"]; exists {
				continue
			}
		}
		if v, exists := clientOptions[key]; exists && v != nil {
			data[key] = v
		}
	}
	return data, nil
}

// clientConfigData 组装单模型客户端配置（compiler.py:352-366 _client_config_data）。
func clientConfigData(model *ResolvedModel) map[string]any {
	data := map[string]any{
		"client_provider": model.Provider,
		"api_key":         model.APIKey,
		"api_base":        model.APIBase,
	}
	if model.EndpointProfile != nil {
		data["endpoint_profile"] = *model.EndpointProfile
	}
	for key, value := range model.ClientOptions {
		if key == "client_provider" || key == "api_key" || key == "api_base" {
			continue
		}
		if key == "endpoint_profile" {
			if _, exists := data["endpoint_profile"]; exists {
				continue
			}
		}
		data[key] = value
	}
	return data
}

// requestConfigData 过滤请求默认参数（compiler.py:369-378 _request_config_data）。
func requestConfigData(values map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range values {
		normalizedKey := key
		if key == "model_name" {
			normalizedKey = "model"
		}
		if value == nil || inSet(key, internalRequestKeys) || inSet(normalizedKey, requestModelKeys) {
			continue
		}
		if inSet(normalizedKey, clientConfigFields) {
			continue
		}
		result[normalizedKey] = value
	}
	return result
}

// mergeRequestDefaults 多源深合并（compiler.py:381-389 _merge_request_defaults）。
func mergeRequestDefaults(sources ...map[string]any) map[string]any {
	merged := map[string]any{}
	for _, source := range sources {
		for key, value := range requestConfigData(source) {
			if valueMap, isMap := value.(map[string]any); isMap {
				if existing, exists := merged[key]; exists {
					if existingMap, ok := existing.(map[string]any); ok {
						merged[key] = deepMergeDicts(existingMap, valueMap)
						continue
					}
				}
			}
			merged[key] = value
		}
	}
	return merged
}

// providerRequestDefaults 组装下发 deployment 的请求默认参数
// （compiler.py:392-402 _provider_request_defaults）。
func providerRequestDefaults(route *ResolvedRoute, groupRequestConfig map[string]any) (map[string]any, error) {
	requestDefaults := mergeRequestDefaults(
		route.Model.RequestDefaults, groupRequestConfig, route.RequestOverrides)
	for key := range coreOnlyRequestKeys {
		delete(requestDefaults, key)
	}
	if err := applyReasoningControls(route.Model, requestDefaults); err != nil {
		return nil, err
	}
	flattenExtraBody(requestDefaults)
	return requestDefaults, nil
}

// applyReasoningControls 编译期展开 reasoning（compiler.py:405-421
// _apply_reasoning_controls）。
// Python 的 explicit_kwargs 携带全部显式键；Go 的 ReasoningIntent 只承载
// reasoning 一个键（reasoning 包已登记偏差），此处传 reasoning 值。
func applyReasoningControls(model *ResolvedModel, requestDefaults map[string]any) error {
	reasoningValue, exists := requestDefaults["reasoning"]
	if !exists {
		return nil
	}
	delete(requestDefaults, "reasoning")
	clientConfig, err := buildClientConfig(reasoningClientConfigData(model))
	if err != nil {
		return err
	}
	modelConfig := &schema.ModelRequestConfig{ModelName: model.ModelName}
	modelConfig.Reasoning = reasoningValue
	plan := reasoning.ResolveReasoningPlan(
		clientConfig, modelConfig, model.ModelName,
		schema.ReasoningIntent{Set: true, Value: reasoningValue})
	reasoning.ApplyReasoningPlan(requestDefaults, plan, false)
	return nil
}

// reasoningClientConfigData reasoning 展开用的临时客户端配置，anthropic
// 强制补 api_mode（compiler.py:424-428 _reasoning_client_config_data）。
func reasoningClientConfigData(model *ResolvedModel) map[string]any {
	data := clientConfigData(model)
	if routerProvider(model) == "anthropic" {
		if _, exists := data["api_mode"]; !exists {
			data["api_mode"] = "anthropic-messages"
		}
	}
	return data
}

// flattenExtraBody 拍平 extra_body（compiler.py:431-437 _flatten_extra_body）。
func flattenExtraBody(params map[string]any) {
	extraBody, exists := params["extra_body"]
	if !exists {
		return
	}
	delete(params, "extra_body")
	switch v := extraBody.(type) {
	case map[string]any:
		for key, value := range v {
			params[key] = value
		}
	default:
		if extraBody != nil {
			params["extra_body"] = extraBody
		}
	}
}

// minContextWindow 启用路由的最小正 context_window（compiler.py:440-456
// _min_context_window；跳 bool、取正 int 最小、空返回 nil）。
func minContextWindow(routes []*ResolvedRoute, groupRequestConfig map[string]any) *int {
	var values []int
	for _, route := range routes {
		requestDefaults := mergeRequestDefaults(
			route.Model.RequestDefaults, groupRequestConfig, route.RequestOverrides)
		value, exists := requestDefaults["context_window"]
		if !exists {
			continue
		}
		// JSON round-trip 后 int 即 float64；bool 显式跳过
		if _, isBool := value.(bool); isBool {
			continue
		}
		switch n := value.(type) {
		case int:
			if n > 0 {
				values = append(values, n)
			}
		case float64:
			if n > 0 && n == float64(int(n)) {
				values = append(values, int(n))
			}
		}
	}
	if len(values) == 0 {
		return nil
	}
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return &min
}

// deepMergeDicts 嵌套 map 深合并（compiler.py:459-466 _deep_merge_dicts）。
func deepMergeDicts(left, right map[string]any) map[string]any {
	merged := deepCopyMap(left)
	for key, value := range right {
		if valueMap, isMap := value.(map[string]any); isMap {
			if existing, exists := merged[key]; exists {
				if existingMap, ok := existing.(map[string]any); ok {
					merged[key] = deepMergeDicts(existingMap, valueMap)
					continue
				}
			}
		}
		if value != nil {
			merged[key] = value
		}
	}
	return merged
}

// buildClientConfig map → ModelClientConfig（已知键进字段、未知键进 Extra，
// 对齐 pydantic extra="allow"；借助 llmschema 的 UnmarshalJSON 拆分）。
func buildClientConfig(data map[string]any) (*schema.ModelClientConfig, error) {
	return jsonRoundTripPtr[schema.ModelClientConfig](data)
}

// buildRequestConfig map → ModelRequestConfig。
func buildRequestConfig(data map[string]any) (*schema.ModelRequestConfig, error) {
	return jsonRoundTripPtr[schema.ModelRequestConfig](data)
}

// configInvalidError 包装 MODEL_REQUEST_CONFIG_INVALID（compiler.py:174-178）。
func configInvalidError(err error) error {
	return exception.BuildError(exception.StatusModelRequestConfigInvalid,
		exception.WithMsg(err.Error()), exception.WithCause(err))
}

// groupInvalidError 包装 MODEL_GROUP_INVALID（compiler.py:216-220）。
func groupInvalidError(err error) error {
	return exception.BuildError(exception.StatusModelGroupInvalid,
		exception.WithMsg(err.Error()), exception.WithCause(err))
}

// jsonRoundTrip 深拷贝式转换：map/结构体经 JSON 序列化往返。
// Python 侧无对应独立函数（pydantic 校验/隐式转换语义的 Go 等价物）。
func jsonRoundTripPtr[T any](data any) (*T, error) {
	out, err := jsonRoundTrip[T](data)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func jsonRoundTrip[T any](data any) (T, error) {
	var out T
	b, err := json.Marshal(data)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	return out, nil
}

// deepCopyMap 深拷贝 map（对齐 Python deepcopy 的别名隔离语义）。
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out, err := jsonRoundTrip[map[string]any](m)
	if err != nil {
		return map[string]any{}
	}
	return out
}

// inSet 集合成员判断。
func inSet(key string, set map[string]struct{}) bool {
	_, ok := set[key]
	return ok
}
