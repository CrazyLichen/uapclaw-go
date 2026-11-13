package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestIntelliRouterDeploymentConfig_字段序列化 校验 14 字段的 json tag 对齐 Python
// schema/config.py:68-87 IntelliRouterDeploymentConfig（model_dump 键名）。
func TestIntelliRouterDeploymentConfig_字段序列化(t *testing.T) {
	cfg := IntelliRouterDeploymentConfig{
		RouteID:          "r1",
		ModelID:          "m1",
		ModelName:        "qwen-max",
		Provider:         "openai",
		APIKey:           "k",
		APIBase:          "http://x",
		FallbackTag:      irStrPtr("tag-a"),
		ModelDescription: irStrPtr("desc"),
		RequestDefaults:  map[string]any{"temperature": 0.7},
		TPM:              irIntPtr(100000),
		RPM:              irIntPtr(1000),
		Timeout:          irFloat64Ptr(15.0),
		EndpointProfile:  "siliconflow",
		CustomHeaders:    map[string]string{"X-A": "b"},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"route_id":"r1"`, `"model_id":"m1"`, `"model_name":"qwen-max"`, `"provider":"openai"`,
		`"api_key":"k"`, `"api_base":"http://x"`, `"endpoint_profile":"siliconflow"`,
		`"custom_headers":{"X-A":"b"}`, `"verify_ssl":null`, `"fallback_tag":"tag-a"`,
		`"model_description":"desc"`, `"request_defaults":{"temperature":0.7}`,
		`"tpm":100000`, `"rpm":1000`, `"timeout":15`,
	} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("缺少键 %s：%s", key, b)
		}
	}
}

// TestIntelliRouterConfig_缺省值 校验缺省值对齐 Python schema/config.py:90-113
// IntelliRouterConfig（strategy 默认 ordered-failover、timeout 30、健康检查间隔 300）。
func TestIntelliRouterConfig_缺省值(t *testing.T) {
	cfg := NewDefaultIntelliRouterConfig()
	if cfg.Strategy != "ordered-failover" {
		t.Fatalf("strategy 缺省应为 ordered-failover，得 %q", cfg.Strategy)
	}
	if cfg.Timeout != 30.0 {
		t.Fatalf("timeout 缺省应为 30.0，得 %v", cfg.Timeout)
	}
	if cfg.EnableHealthCheck {
		t.Fatal("enable_health_check 缺省应为 false")
	}
	if cfg.HealthCheckInterval != 300.0 {
		t.Fatalf("health_check_interval 缺省应为 300.0，得 %v", cfg.HealthCheckInterval)
	}
	if cfg.NumRetries != nil {
		t.Fatalf("num_retries 缺省应为 nil（由编译器/客户端兜底 max(len-1,0)），得 %v", *cfg.NumRetries)
	}
	if cfg.Deployments == nil || len(cfg.Deployments) != 0 {
		t.Fatal("deployments 缺省应为空列表而非 nil")
	}
}

// TestModelClientConfig_挂载IntelliRouter 校验 ModelClientConfig.intelli_router 字段
// 挂载与 JSON 往返（Python schema/config.py intelli_router: Optional[IntelliRouterConfig]）。
func TestModelClientConfig_挂载IntelliRouter(t *testing.T) {
	cfg := ModelClientConfig{
		ClientProvider: "intelli_router",
		IntelliRouter: &IntelliRouterConfig{
			ModelGroupID: irStrPtr("g1"),
			Deployments: []IntelliRouterDeploymentConfig{{
				RouteID: "r1", ModelName: "qwen-max", Provider: "openai",
			}},
		},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"intelli_router":{"model_group_id":"g1"`) {
		t.Fatalf("intelli_router 未挂载：%s", b)
	}
	var back ModelClientConfig
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.IntelliRouter == nil || len(back.IntelliRouter.Deployments) != 1 {
		t.Fatalf("往返丢失 intelli_router：%+v", back.IntelliRouter)
	}
	if back.IntelliRouter.Deployments[0].ModelName != "qwen-max" {
		t.Fatalf("deployment 字段丢失：%+v", back.IntelliRouter.Deployments[0])
	}
}

func irStrPtr(s string) *string       { return &s }
func irIntPtr(i int) *int             { return &i }
func irFloat64Ptr(f float64) *float64 { return &f }
