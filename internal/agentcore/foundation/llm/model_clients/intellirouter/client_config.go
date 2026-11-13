package intellirouter

import (
	"encoding/json"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/router"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// 提取自 ModelClientConfig 的 IntelliRouter 类型化配置。
// Python: intelli_router_model_client.py:223-281 IntelliRouterClientConfig
type IntelliRouterClientConfig struct {
	// ModelGroupID 业务模型组 ID（Python: :224 model_group_id）
	ModelGroupID *string
	// Deployments deployment 配置列表（Python: :225 deployments）
	Deployments []map[string]any
	// Strategy 策略名，缺省 simple-shuffle（Python: :226）
	Strategy string
	// NumRetries 重试预算，缺省 3（Python: :227）
	NumRetries int
	// Timeout 请求超时秒，缺省 30.0（Python: :228）
	Timeout float64
	// StrategyKwargs 策略参数（Python: :229）
	StrategyKwargs map[string]any
	// EnableHealthCheck 启用健康检查，缺省 false（Python: :230）
	EnableHealthCheck bool
	// HealthCheckInterval 健康检查间隔秒，缺省 300.0（Python: :231）
	HealthCheckInterval float64
	// VerifySSL TLS 校验（从 ModelClientConfig.verify_ssl 透传，Python: :232）
	VerifySSL bool
	// EnableObservability 启用可观测钩子，缺省 false（Python: :233）
	EnableObservability bool
	// WebDashboardPort 指标面板端口（Go 侧不移植面板，偏差 D-3；Python: :234）
	WebDashboardPort int
	// RouterScope 路由器缓存作用域（Python: :235）
	RouterScope *string
}

// FromModelClientConfig 从 ModelClientConfig 提取 IntelliRouter 配置。
// Python: intelli_router_model_client.py:238-281 from_model_client_config
// 两条路径：intelli_router 结构化字段优先（:240-263）；否则读 extra 兜底键
// （:265-280 intelli_router_* 前缀键）。
func FromModelClientConfig(config *llmschema.ModelClientConfig) *IntelliRouterClientConfig {
	if config.IntelliRouter != nil {
		routerConfig := config.IntelliRouter
		numRetries := 3
		if routerConfig.NumRetries != nil {
			numRetries = *routerConfig.NumRetries
		} else if n := len(routerConfig.Deployments) - 1; n >= 0 && n < numRetries {
			// Python: :254-257 num_retries 缺省 max(len(deployments) - 1, 0)
			numRetries = n
		}
		// Python: :245-246 model_dump(exclude_none=True) → dict 列表
		deployments := []map[string]any{}
		for _, dep := range routerConfig.Deployments {
			deployments = append(deployments, deploymentToMap(dep))
		}
		return &IntelliRouterClientConfig{
			ModelGroupID:        routerConfig.ModelGroupID,
			Deployments:         deployments,
			Strategy:            routerConfig.Strategy,
			NumRetries:          numRetries,
			Timeout:             routerConfig.Timeout,
			StrategyKwargs:      routerConfig.StrategyKwargs,
			EnableHealthCheck:   routerConfig.EnableHealthCheck,
			HealthCheckInterval: routerConfig.HealthCheckInterval,
			VerifySSL:           config.VerifySSL,
			EnableObservability: routerConfig.EnableObservability,
			WebDashboardPort:    routerConfig.WebDashboardPort,
			RouterScope:         routerConfig.RouterScope,
		}
	}

	// Extra 兜底路径（Python: :265-280；ModelClientConfig extra="allow"）
	extra := config.Extra
	cfg := &IntelliRouterClientConfig{
		Strategy:            "simple-shuffle",
		NumRetries:          3,
		Timeout:             30.0,
		StrategyKwargs:      map[string]any{},
		HealthCheckInterval: 300.0,
		VerifySSL:           config.VerifySSL,
	}
	if v, ok := extra["intelli_router_model_group_id"].(string); ok && v != "" {
		cfg.ModelGroupID = &v
	}
	if v, ok := extra["intelli_router_deployments"].([]any); ok {
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				cfg.Deployments = append(cfg.Deployments, m)
			}
		}
	}
	// Python 无对应分支：Go 调用方可能直传 []map[string]any（非 JSON 往返产物）
	if v, ok := extra["intelli_router_deployments"].([]map[string]any); ok {
		cfg.Deployments = append(cfg.Deployments, v...)
	}
	if v, ok := extra["intelli_router_strategy"].(string); ok && v != "" {
		cfg.Strategy = v
	}
	switch v := extra["intelli_router_num_retries"].(type) {
	case float64:
		cfg.NumRetries = int(v)
	case int:
		cfg.NumRetries = v
	}
	if v, ok := extra["intelli_router_timeout"].(float64); ok {
		cfg.Timeout = v
	}
	if v, ok := extra["intelli_router_strategy_kwargs"].(map[string]any); ok {
		cfg.StrategyKwargs = v
	}
	if v, ok := extra["intelli_router_enable_health_check"].(bool); ok {
		cfg.EnableHealthCheck = v
	}
	if v, ok := extra["intelli_router_health_check_interval"].(float64); ok {
		cfg.HealthCheckInterval = v
	}
	if v, ok := extra["intelli_router_enable_observability"].(bool); ok {
		cfg.EnableObservability = v
	}
	switch v := extra["intelli_router_web_dashboard_port"].(type) {
	case float64:
		cfg.WebDashboardPort = int(v)
	case int:
		cfg.WebDashboardPort = v
	}
	return cfg
}

// deploymentToMap 结构化 deployment → map（对齐 Python
// model_dump(exclude_none=True) 的键名与 nil 剔除语义）。
func deploymentToMap(dep llmschema.IntelliRouterDeploymentConfig) map[string]any {
	data := map[string]any{"route_id": dep.RouteID}
	if dep.ModelID != "" {
		data["model_id"] = dep.ModelID
	}
	data["model_name"] = dep.ModelName
	data["provider"] = dep.Provider
	data["api_key"] = dep.APIKey
	data["api_base"] = dep.APIBase
	if dep.EndpointProfile != "" {
		data["endpoint_profile"] = dep.EndpointProfile
	}
	if dep.CustomHeaders != nil {
		data["custom_headers"] = dep.CustomHeaders
	}
	if dep.VerifySSL != nil {
		data["verify_ssl"] = *dep.VerifySSL
	}
	if dep.FallbackTag != nil {
		data["fallback_tag"] = *dep.FallbackTag
	}
	if dep.ModelDescription != nil {
		data["model_description"] = *dep.ModelDescription
	}
	if dep.RequestDefaults != nil && len(dep.RequestDefaults) > 0 {
		data["request_defaults"] = dep.RequestDefaults
	}
	if dep.TPM != nil {
		data["tpm"] = *dep.TPM
	}
	if dep.RPM != nil {
		data["rpm"] = *dep.RPM
	}
	if dep.Timeout != nil {
		data["timeout"] = *dep.Timeout
	}
	return data
}

// buildCoreDeployment IntelliRouterClientConfig 的 deployment dict →
// core.Deployment（Python: :497-560 _create_router 的 dep_kwargs 组装；
// Go 侧 23.4a Deployment 字段全量支持，required_new_fields 版本检查
// 不适用——单一内部实现，登记为 Python 无对应省略）。
func buildCoreDeployment(depCfg map[string]any, defaultVerifySSL bool) *intelliroutercore.Deployment {
	dep := &intelliroutercore.Deployment{}
	if v, ok := depCfg["route_id"].(string); ok && v != "" {
		dep.ID = v // Python: dep_cfg.get("route_id") or dep_cfg.get("id")
	} else if v, ok := depCfg["id"].(string); ok {
		dep.ID = v
	}
	if v, ok := depCfg["model_id"].(string); ok {
		dep.ModelID = v
	}
	if v, ok := depCfg["model_name"].(string); ok {
		dep.ModelName = v
	}
	if v, ok := depCfg["api_key"].(string); ok {
		dep.APIKey = v
	}
	if v, ok := depCfg["api_base"].(string); ok {
		dep.APIBase = v
	}
	dep.Provider = "openai" // Python: dep_cfg.get("provider", "openai")
	if v, ok := depCfg["provider"].(string); ok && v != "" {
		dep.Provider = v
	}
	if v, ok := depCfg["tpm"].(float64); ok {
		n := int(v)
		dep.TPM = &n
	}
	if v, ok := depCfg["rpm"].(float64); ok {
		n := int(v)
		dep.RPM = &n
	}
	if v, ok := depCfg["timeout"].(float64); ok {
		dep.Timeout = &v
	}
	// Python: dep_cfg.get("verify_ssl", config.verify_ssl)
	dep.VerifySSL = defaultVerifySSL
	if v, ok := depCfg["verify_ssl"].(bool); ok {
		dep.VerifySSL = v
	}
	if v, ok := depCfg["request_defaults"].(map[string]any); ok {
		dep.RequestDefaults = v
	}
	if v, ok := depCfg["endpoint_profile"].(string); ok && v != "" {
		dep.EndpointProfile = &v
	}
	if v, ok := depCfg["custom_headers"].(map[string]any); ok {
		headers := map[string]string{}
		for k, hv := range v {
			if hs, ok := hv.(string); ok {
				headers[k] = hs
			}
		}
		dep.CustomHeaders = headers
	}
	if v, ok := depCfg["fallback_tag"].(string); ok && v != "" {
		dep.FallbackTag = &v
	}
	if v, ok := depCfg["model_description"].(string); ok && v != "" {
		dep.ModelDescription = &v
	}
	if v, ok := depCfg["tags"].([]any); ok {
		for _, t := range v {
			if s, ok := t.(string); ok {
				dep.Tags = append(dep.Tags, s)
			}
		}
	}
	return dep
}

// deploymentsJSON deterministic JSON（键排序由 encoding/json map 序列化保证）。
func deploymentsJSON(deployments []map[string]any) string {
	b, _ := json.Marshal(deployments)
	return string(b)
}

// kwargsJSON strategy_kwargs deterministic JSON。
func kwargsJSON(kwargs map[string]any) string {
	b, _ := json.Marshal(kwargs)
	return string(b)
}

// routerParamsFor 组装 RouterParams（Python: :563-583 router 构造参数；
// web_dashboard_port 面板不移植，偏差 D-3——Python :551-557 的告警在 Go
// 侧保留日志）。
func (cfg *IntelliRouterClientConfig) routerParamsFor(eventBus *observability.EventBus) router.RouterParams {
	params := router.RouterParams{
		Deployments:         make([]*intelliroutercore.Deployment, 0, len(cfg.Deployments)),
		StrategyType:        cfg.Strategy,
		NumRetries:          cfg.NumRetries,
		Timeout:             cfg.Timeout,
		EnableHealthCheck:   cfg.EnableHealthCheck,
		HealthCheckInterval: cfg.HealthCheckInterval,
		HTTPClient:          newHTTPClient(cfg.VerifySSL),
	}
	for _, depCfg := range cfg.Deployments {
		params.Deployments = append(params.Deployments, buildCoreDeployment(depCfg, cfg.VerifySSL))
	}
	// Python: :582 **config.strategy_kwargs（weights/default_weight/fallback_tag
	// 作为构造参数透传，23.4a RouterParams 以 StrategyKwargs 承载）
	params.StrategyKwargs = cfg.StrategyKwargs
	if cfg.ModelGroupID != nil {
		params.ModelGroupID = *cfg.ModelGroupID
	}
	params.EventBus = eventBus
	return params
}
