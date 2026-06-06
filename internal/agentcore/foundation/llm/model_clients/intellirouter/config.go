package intellirouter

import (
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IntelliRouterClientConfig 从 ModelClientConfig.Extra 中提取的 IntelliRouter 路由配置。
//
// 对应 Python: IntelliRouterClientConfig（intelli_router_model_client.py 中的 dataclass）
//
// 配置字段通过 intelli_router_ 前缀从 ModelClientConfig.Extra 中提取，
// 对齐 Python 的 __pydantic_extra__ 读取逻辑。
type IntelliRouterClientConfig struct {
	// Deployments 部署端点配置列表
	Deployments []DeploymentConfig
	// Strategy 路由策略名称，默认 "simple-shuffle"
	Strategy string
	// NumRetries 重试次数，默认 3
	NumRetries int
	// Timeout 超时时间（秒），默认 30.0
	Timeout float64
	// StrategyKwargs 策略参数（adaptive 策略的多因子权重等）
	StrategyKwargs map[string]any
	// EnableHealthCheck 是否启用健康检查，默认 false
	EnableHealthCheck bool
	// HealthCheckInterval 健康检查间隔（秒），默认 300.0
	HealthCheckInterval float64
	// VerifySSL 是否验证 SSL 证书
	VerifySSL bool
}

// DeploymentConfig 单个部署端点配置。
//
// 对应 Python: intelli_router.Deployment 构造参数
type DeploymentConfig struct {
	// ID 部署端点唯一标识，如 "deepseek-v4-flash-dep1"
	ID string
	// ModelName 模型名称
	ModelName string
	// APIKey API 密钥
	APIKey string
	// APIBase API 基础 URL（OpenAI 兼容协议）
	APIBase string
	// TPM 每分钟 token 限制，默认 100000
	TPM int
	// RPM 每分钟请求限制，默认 60
	RPM int
	// Tags 标签列表（用于 tag-based 路由策略）
	Tags []string
	// Timeout 请求超时时间（秒），默认 30.0
	Timeout float64
	// VerifySSL 是否验证 SSL 证书
	VerifySSL bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// FromModelClientConfig 从 ModelClientConfig.Extra 中提取 IntelliRouter 路由配置。
//
// 对应 Python: IntelliRouterClientConfig.from_model_client_config(config)
//
// Extra 字段命名约定：intelli_router_{field_name}
//   - intelli_router_deployments → Deployments
//   - intelli_router_strategy → Strategy
//   - intelli_router_num_retries → NumRetries
//   - intelli_router_timeout → Timeout
//   - intelli_router_strategy_kwargs → StrategyKwargs
//   - intelli_router_enable_health_check → EnableHealthCheck
//   - intelli_router_health_check_interval → HealthCheckInterval
func FromModelClientConfig(config *llmschema.ModelClientConfig) *IntelliRouterClientConfig {
	extra := config.Extra
	if extra == nil {
		extra = make(map[string]any)
	}

	// 提取 deployments 列表
	var deployments []DeploymentConfig
	if rawDeps, ok := extra["intelli_router_deployments"]; ok {
		deployments = parseDeployments(rawDeps, config.VerifySSL)
	}

	// 提取 strategy_kwargs
	strategyKwargs := make(map[string]any)
	if raw, ok := extra["intelli_router_strategy_kwargs"]; ok {
		if m, ok := raw.(map[string]any); ok {
			strategyKwargs = m
		}
	}

	return &IntelliRouterClientConfig{
		Deployments:         deployments,
		Strategy:            getStringDefault(extra, "intelli_router_strategy", "simple-shuffle"),
		NumRetries:          getIntDefault(extra, "intelli_router_num_retries", 3),
		Timeout:             getFloatDefault(extra, "intelli_router_timeout", 30.0),
		StrategyKwargs:      strategyKwargs,
		EnableHealthCheck:   getBoolDefault(extra, "intelli_router_enable_health_check", false),
		HealthCheckInterval: getFloatDefault(extra, "intelli_router_health_check_interval", 300.0),
		VerifySSL:           config.VerifySSL,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseDeployments 从 Extra 中的 deployments 列表解析为 []DeploymentConfig。
//
// 输入格式：[]map[string]any（从 YAML/JSON 反序列化得到）
// 对应 Python: for dep_cfg in config.deployments: Deployment(...)
func parseDeployments(raw any, defaultVerifySSL bool) []DeploymentConfig {
	// 支持两种类型：[]map[string]any 和 []any（JSON 反序列化可能产生 []any）
	var depMaps []map[string]any

	switch v := raw.(type) {
	case []map[string]any:
		depMaps = v
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				depMaps = append(depMaps, m)
			}
		}
	default:
		return nil
	}

	deployments := make([]DeploymentConfig, 0, len(depMaps))
	for _, dep := range depMaps {
		// 解析 tags
		var tags []string
		if rawTags, ok := dep["tags"]; ok {
			switch v := rawTags.(type) {
			case []string:
				tags = v
			case []any:
				for _, t := range v {
					if s, ok := t.(string); ok {
						tags = append(tags, s)
					}
				}
			}
		}

		// 解析 verify_ssl：优先使用 deployment 级别的配置，不存在则用全局默认值
		verifySSL := defaultVerifySSL
		if v, ok := dep["verify_ssl"]; ok {
			if b, ok := v.(bool); ok {
				verifySSL = b
			}
		}

		dc := DeploymentConfig{
			ID:        getStringDefault(dep, "id", ""),
			ModelName: getStringDefault(dep, "model_name", ""),
			APIKey:    getStringDefault(dep, "api_key", ""),
			APIBase:   getStringDefault(dep, "api_base", ""),
			TPM:       getIntDefault(dep, "tpm", 100000),
			RPM:       getIntDefault(dep, "rpm", 60),
			Tags:      tags,
			Timeout:   getFloatDefault(dep, "timeout", 30.0),
			VerifySSL: verifySSL,
		}
		deployments = append(deployments, dc)
	}
	return deployments
}

// getStringDefault 从 map 中提取字符串值，不存在则返回默认值。
func getStringDefault(m map[string]any, key, defaultVal string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		// 尝试 fmt.Sprint 转换（处理数字等类型）
		s := fmt.Sprint(v)
		if s != "" {
			return s
		}
	}
	return defaultVal
}

// getIntDefault 从 map 中提取整数值，不存在则返回默认值。
func getIntDefault(m map[string]any, key string, defaultVal int) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return defaultVal
}

// getFloatDefault 从 map 中提取浮点数值，不存在则返回默认值。
func getFloatDefault(m map[string]any, key string, defaultVal float64) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return defaultVal
}

// getBoolDefault 从 map 中提取布尔值，不存在则返回默认值。
func getBoolDefault(m map[string]any, key string, defaultVal bool) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}
