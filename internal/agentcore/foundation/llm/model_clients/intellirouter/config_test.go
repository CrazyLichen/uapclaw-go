package intellirouter

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──── FromModelClientConfig 测试 ────

// TestFromModelClientConfig_DefaultValues 测试默认值。
func TestFromModelClientConfig_DefaultValues(t *testing.T) {
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
	)

	config := FromModelClientConfig(cc)
	if config.Strategy != "simple-shuffle" {
		t.Errorf("Strategy = %q, 期望 %q", config.Strategy, "simple-shuffle")
	}
	if config.NumRetries != 3 {
		t.Errorf("NumRetries = %d, 期望 3", config.NumRetries)
	}
	if config.Timeout != 30.0 {
		t.Errorf("Timeout = %f, 期望 30.0", config.Timeout)
	}
	if config.EnableHealthCheck {
		t.Error("EnableHealthCheck 应为 false")
	}
	if config.HealthCheckInterval != 300.0 {
		t.Errorf("HealthCheckInterval = %f, 期望 300.0", config.HealthCheckInterval)
	}
	if len(config.Deployments) != 0 {
		t.Errorf("Deployments 长度 = %d, 期望 0", len(config.Deployments))
	}
}

// TestFromModelClientConfig_WithDeployments 测试带 deployments 的配置提取。
func TestFromModelClientConfig_WithDeployments(t *testing.T) {
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "deepseek-dep1",
					"model_name": "deepseek-v4-flash",
					"api_key":    "sk-test-123",
					"api_base":   "https://api.deepseek.com",
					"tpm":        200000,
					"rpm":        120,
					"tags":       []any{"primary", "high-throughput"},
					"timeout":    60.0,
				},
			},
			"intelli_router_strategy":    "adaptive",
			"intelli_router_num_retries": 5,
			"intelli_router_timeout":     45.0,
			"intelli_router_strategy_kwargs": map[string]any{
				"exploration_ratio": 0.15,
				"w_health":         1.0,
				"w_latency":        0.3,
			},
			"intelli_router_enable_health_check":    true,
			"intelli_router_health_check_interval": 120.0,
		}),
	)

	config := FromModelClientConfig(cc)

	// 基础字段
	if config.Strategy != "adaptive" {
		t.Errorf("Strategy = %q, 期望 %q", config.Strategy, "adaptive")
	}
	if config.NumRetries != 5 {
		t.Errorf("NumRetries = %d, 期望 5", config.NumRetries)
	}
	if config.Timeout != 45.0 {
		t.Errorf("Timeout = %f, 期望 45.0", config.Timeout)
	}
	if !config.EnableHealthCheck {
		t.Error("EnableHealthCheck 应为 true")
	}
	if config.HealthCheckInterval != 120.0 {
		t.Errorf("HealthCheckInterval = %f, 期望 120.0", config.HealthCheckInterval)
	}

	// Deployments
	if len(config.Deployments) != 1 {
		t.Fatalf("Deployments 长度 = %d, 期望 1", len(config.Deployments))
	}
	dep := config.Deployments[0]
	if dep.ID != "deepseek-dep1" {
		t.Errorf("ID = %q, 期望 %q", dep.ID, "deepseek-dep1")
	}
	if dep.ModelName != "deepseek-v4-flash" {
		t.Errorf("ModelName = %q, 期望 %q", dep.ModelName, "deepseek-v4-flash")
	}
	if dep.APIKey != "sk-test-123" {
		t.Errorf("APIKey = %q, 期望 %q", dep.APIKey, "sk-test-123")
	}
	if dep.APIBase != "https://api.deepseek.com" {
		t.Errorf("APIBase = %q, 期望 %q", dep.APIBase, "https://api.deepseek.com")
	}
	if dep.TPM != 200000 {
		t.Errorf("TPM = %d, 期望 200000", dep.TPM)
	}
	if dep.RPM != 120 {
		t.Errorf("RPM = %d, 期望 120", dep.RPM)
	}
	if len(dep.Tags) != 2 || dep.Tags[0] != "primary" || dep.Tags[1] != "high-throughput" {
		t.Errorf("Tags = %v, 期望 [primary high-throughput]", dep.Tags)
	}
	if dep.Timeout != 60.0 {
		t.Errorf("Timeout = %f, 期望 60.0", dep.Timeout)
	}

	// StrategyKwargs
	if config.StrategyKwargs["exploration_ratio"] != 0.15 {
		t.Errorf("StrategyKwargs.exploration_ratio = %v, 期望 0.15", config.StrategyKwargs["exploration_ratio"])
	}
}

// TestFromModelClientConfig_DeploymentDefaults 测试 deployment 字段的默认值。
func TestFromModelClientConfig_DeploymentDefaults(t *testing.T) {
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "test-model",
					"api_key":    "key1",
					"api_base":   "https://api.test.com",
				},
			},
		}),
	)

	config := FromModelClientConfig(cc)
	dep := config.Deployments[0]

	if dep.TPM != 100000 {
		t.Errorf("TPM 默认 = %d, 期望 100000", dep.TPM)
	}
	if dep.RPM != 60 {
		t.Errorf("RPM 默认 = %d, 期望 60", dep.RPM)
	}
	if dep.Timeout != 30.0 {
		t.Errorf("Timeout 默认 = %f, 期望 30.0", dep.Timeout)
	}
	if len(dep.Tags) != 0 {
		t.Errorf("Tags 默认应为空, 实际 = %v", dep.Tags)
	}
}

// TestFromModelClientConfig_NilExtra 测试 Extra 为 nil 的情况。
func TestFromModelClientConfig_NilExtra(t *testing.T) {
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(true),
	)
	// Extra 为 nil

	config := FromModelClientConfig(cc)
	if config.Strategy != "simple-shuffle" {
		t.Errorf("Strategy = %q, 期望 %q", config.Strategy, "simple-shuffle")
	}
	if config.VerifySSL != true {
		t.Error("VerifySSL 应为 true（从 ModelClientConfig 继承）")
	}
}

// TestFromModelClientConfig_DeploymentsAsAnySlice 测试 deployments 为 []any 格式。
func TestFromModelClientConfig_DeploymentsAsAnySlice(t *testing.T) {
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []any{
				map[string]any{
					"id":         "dep1",
					"model_name": "model1",
					"api_key":    "key1",
					"api_base":   "https://api1.test.com",
				},
			},
		}),
	)

	config := FromModelClientConfig(cc)
	if len(config.Deployments) != 1 {
		t.Fatalf("Deployments 长度 = %d, 期望 1", len(config.Deployments))
	}
	if config.Deployments[0].ID != "dep1" {
		t.Errorf("ID = %q, 期望 %q", config.Deployments[0].ID, "dep1")
	}
}

// TestFromModelClientConfig_DeploymentVerifySSL 测试 deployment 级别的 verify_ssl。
func TestFromModelClientConfig_DeploymentVerifySSL(t *testing.T) {
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(true),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "model1",
					"api_key":    "key1",
					"api_base":   "https://api1.test.com",
				},
				{
					"id":         "dep2",
					"model_name": "model1",
					"api_key":    "key2",
					"api_base":   "https://api2.test.com",
					"verify_ssl": false,
				},
			},
		}),
	)

	config := FromModelClientConfig(cc)

	// dep1 未指定 verify_ssl，应继承全局默认值 true
	if config.Deployments[0].VerifySSL != true {
		t.Error("dep1 VerifySSL 应继承全局默认值 true")
	}
	// dep2 显式指定 verify_ssl=false
	if config.Deployments[1].VerifySSL != false {
		t.Error("dep2 VerifySSL 应为 false")
	}
}
