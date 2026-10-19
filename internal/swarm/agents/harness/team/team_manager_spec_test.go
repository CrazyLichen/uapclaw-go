package team

import (
	"testing"
)

// TestLoadTeamSpec_分布式normalize 缺依赖时回退 local（Go 恒无缺依赖，仅验证 normalize 路径）。
func TestLoadTeamSpec_分布式normalize(t *testing.T) {
	cfg := map[string]any{
		"team": map[string]any{
			"runtime":   map[string]any{"mode": "distributed", "role": "leader"},
			"transport": map[string]any{"type": "pyzmq"},
		},
		"modes":  map[string]any{"team": map[string]any{}},
		"models": map[string]any{},
	}
	spec := loadTeamSpec("sess-1", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
}

// TestLoadTeamSpec_modelPool注入 多默认模型时填充 spec.ModelPool。
func TestLoadTeamSpec_modelPool注入(t *testing.T) {
	cfg := map[string]any{
		"team":  map[string]any{},
		"modes": map[string]any{"team": map[string]any{}},
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m1", "api_key": "k1", "api_base": "https://1",
						"client_provider": "OpenAI", "timeout": 60,
					},
					"model_config_obj": map[string]any{"temperature": 0.9},
				},
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m2", "api_key": "k2", "api_base": "https://2",
						"client_provider": "OpenAI",
					},
				},
			},
		},
	}
	spec := loadTeamSpec("sess-2", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
	if len(spec.ModelPool) != 2 {
		t.Fatalf("ModelPool = %d, want 2", len(spec.ModelPool))
	}
	if spec.ModelPoolStrategy != "by_model_name" {
		t.Fatalf("ModelPoolStrategy = %q", spec.ModelPoolStrategy)
	}
	first := spec.ModelPool[0]
	if first.ModelName != "m1" || first.APIKey != "k1" || first.APIBaseURL != "https://1" || first.APIProvider != "OpenAI" {
		t.Fatalf("first pool entry mismatch: %+v", first)
	}
	clientMeta, _ := first.Metadata["client"].(map[string]any)
	if clientMeta == nil || clientMeta["timeout"] != 60.0 {
		t.Fatalf("metadata.client timeout missing: %v", first.Metadata)
	}
	if _, hasModelName := clientMeta["model_name"]; hasModelName {
		t.Fatalf("metadata.client should exclude model_name")
	}
	requestMeta, _ := first.Metadata["request"].(map[string]any)
	if requestMeta == nil || requestMeta["temperature"] != 0.9 {
		t.Fatalf("metadata.request temperature missing: %v", first.Metadata)
	}
}

// TestLoadTeamSpec_单模型不注入pool。
func TestLoadTeamSpec_单模型不注入pool(t *testing.T) {
	cfg := map[string]any{
		"team":  map[string]any{},
		"modes": map[string]any{"team": map[string]any{}},
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{"model_name": "only"},
			},
		},
	}
	spec := loadTeamSpec("sess-3", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
	if len(spec.ModelPool) != 0 {
		t.Fatalf("single model should not populate pool, got %d", len(spec.ModelPool))
	}
}

// TestNormalizeDistributedTransportFields_方法包装 委托纯函数。
func TestNormalizeDistributedTransportFields_方法包装(t *testing.T) {
	mgr := NewTeamManager()
	configBase := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed", "role": "leader"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	teamCfg := map[string]any{"transport": map[string]any{"type": "pyzmq", "params": map[string]any{}}}
	result := mgr.NormalizeDistributedTransportFields(configBase, teamCfg)
	params := result["transport"].(map[string]any)["params"].(map[string]any)
	if params["direct_addr"] != "tcp://0.0.0.0:18555" {
		t.Fatalf("method wrapper should delegate to pure function")
	}
}

// TestParsePort_方法包装。
func TestParsePort_方法包装(t *testing.T) {
	mgr := NewTeamManager()
	if got := mgr.ParsePort("18600", 1, "f"); got != 18600 {
		t.Fatalf("ParsePort method = %d", got)
	}
}
