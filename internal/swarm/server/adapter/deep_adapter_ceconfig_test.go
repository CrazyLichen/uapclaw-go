package adapter

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDeepAgentContextEngineConfig_窗口override 验证 model_config_obj.context_window 收集成 override 并恢复 200/10 默认值
func TestDeepAgentContextEngineConfig_窗口映射(t *testing.T) {
	d := &DeepAdapter{}
	reactCfg := map[string]any{
		"context_engine_config": map[string]any{
			"enable_kv_cache_release": true,
		},
	}
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name":      "qwen3-coder",
						"api_base":        "https://example.com/v1",
						"api_key":         "sk-test",
						"client_provider": "OpenAI",
					},
					"model_config_obj": map[string]any{
						"context_window": "32k",
						"max_tokens":     8192,
					},
					"is_default": true,
				},
			},
		},
	}

	got := d.deepAgentContextEngineConfig(reactCfg, configBase)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	if got.ModelContextWindowTokensOverride != 32768 {
		t.Errorf("override 应为 32768（32k），实际 %d", got.ModelContextWindowTokensOverride)
	}
	if got.ModelName != "qwen3-coder" {
		t.Errorf("ModelName 应为 qwen3-coder，实际 %q", got.ModelName)
	}
	if len(got.ModelContextWindowTokens) != 0 {
		t.Errorf("不再从模型条目填 name→tokens 映射，实际 %v", got.ModelContextWindowTokens)
	}
	// §4.5：恢复 Python ReActAgentConfig 默认值
	if got.MaxContextMessageNum != 200 {
		t.Errorf("MaxContextMessageNum 应为 200，实际 %d", got.MaxContextMessageNum)
	}
	if got.DefaultWindowRoundNum != 10 {
		t.Errorf("DefaultWindowRoundNum 应为 10，实际 %d", got.DefaultWindowRoundNum)
	}
	if got.ContextWindowTokens != 0 {
		t.Errorf("ContextWindowTokens（优先级1）应保持 0，实际 %d", got.ContextWindowTokens)
	}
	if !got.EnableKVCacheRelease {
		t.Error("EnableKVCacheRelease 应为 true")
	}
}

// TestDeepAgentContextEngineConfig_无cec段返回nil 保持旧行为
func TestDeepAgentContextEngineConfig_无cec段返回nil(t *testing.T) {
	d := &DeepAdapter{}
	if got := d.deepAgentContextEngineConfig(map[string]any{}, map[string]any{}); got != nil {
		t.Errorf("无 context_engine_config 段时应返回 nil，实际 %+v", got)
	}
}

// TestDeepAgentContextEngineConfig_同名模型冲突 默认条目优先
func TestDeepAgentContextEngineConfig_同名模型冲突(t *testing.T) {
	d := &DeepAdapter{}
	reactCfg := map[string]any{"context_engine_config": map[string]any{}}
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m1", "api_base": "https://a/v1",
						"api_key": "k1", "client_provider": "OpenAI",
					},
					"model_config_obj": map[string]any{"context_window": 8000},
					"is_default":       false,
				},
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m1", "api_base": "https://b/v1",
						"api_key": "k2", "client_provider": "OpenAI",
					},
					"model_config_obj": map[string]any{"context_window": 32000},
					"is_default":       true,
				},
			},
		},
	}
	got := d.deepAgentContextEngineConfig(reactCfg, configBase)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	if got.ModelContextWindowTokensOverride != 32000 {
		t.Errorf("默认条目优先应取 32000，实际 %d", got.ModelContextWindowTokensOverride)
	}
}

// TestDeepAgentContextEngineConfig_非默认条目被忽略 默认条目先出现时，
// 后出现的非默认条目应被忽略（日志 kept/ignored 分支）。
func TestDeepAgentContextEngineConfig_非默认条目被忽略(t *testing.T) {
	d := &DeepAdapter{}
	reactCfg := map[string]any{"context_engine_config": map[string]any{}}
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m2", "api_base": "https://a/v1",
						"api_key": "k1", "client_provider": "OpenAI",
					},
					"model_config_obj": map[string]any{"context_window": 32000},
					"is_default":       true,
				},
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m2", "api_base": "https://b/v1",
						"api_key": "k2", "client_provider": "OpenAI",
					},
					"model_config_obj": map[string]any{"context_window": 8000},
					"is_default":       false,
				},
			},
		},
	}
	got := d.deepAgentContextEngineConfig(reactCfg, configBase)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	if got.ModelContextWindowTokensOverride != 32000 {
		t.Errorf("非默认条目应被忽略，窗口应保持 32000，实际 %d", got.ModelContextWindowTokensOverride)
	}
}

// TestToPositiveInt_各类型分支 覆盖 int/int32/int64/float64/string/非法值
func TestToPositiveInt_各类型分支(t *testing.T) {
	cases := []struct {
		name   string
		in     any
		want   int
		wantOK bool
	}{
		{"int", 32000, 32000, true},
		{"int32", int32(8192), 8192, true},
		{"int64", int64(4096), 4096, true},
		{"float64", float64(2048), 2048, true},
		{"string", "16000", 16000, true},
		{"string带空格", " 1024 ", 1024, true},
		{"字符串非法", "abc", 0, false},
		{"零", 0, 0, false},
		{"负数", -1, 0, false},
		{"nil", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toPositiveInt(tc.in)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("toPositiveInt(%v) = (%d,%v)，期望 (%d,%v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
