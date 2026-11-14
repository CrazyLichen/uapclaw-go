package common

import (
	"encoding/json"
	"testing"
)

// BuildReasoningModelRequestKwargs：agentos 旧 max_tokens → context_window 迁移。
// 对齐 Python: reasoning_injector.py:98-105。
func TestBuildReasoningModelRequestKwargs_AgentOS旧MaxTokens迁移(t *testing.T) {
	got := BuildReasoningModelRequestKwargs(
		map[string]any{"client_provider": "openai"},
		map[string]any{"_source": "agentos", "max_tokens": 8192},
		"",
	)
	if got["context_window"] != 8192 {
		t.Errorf("旧 max_tokens 应迁移到 context_window: %v", got["context_window"])
	}
	if _, ok := got["max_tokens"]; ok {
		t.Errorf("旧 max_tokens 键应已移除")
	}
	// 新旧并存以新字段为准（Python: reasoning_injector.py:100 以新字段为准）
	got = BuildReasoningModelRequestKwargs(
		map[string]any{},
		map[string]any{"_source": "agentos", "max_tokens": 4096, "context_window": 262144},
		"",
	)
	if got["context_window"] != 262144 {
		t.Errorf("新旧并存应以新字段为准: %v", got["context_window"])
	}
}

// BuildReasoningModelRequestKwargs：内部字段清理四键 + model 回填。
// 对齐 Python: reasoning_injector.py:106-114/:141。
func TestBuildReasoningModelRequestKwargs_内部字段清理(t *testing.T) {
	got := BuildReasoningModelRequestKwargs(
		map[string]any{},
		map[string]any{"model": "legacy", "model_name": "legacy2", "reasoning_level": "high", "_source": "x", "temperature": 0.7},
		"",
	)
	for _, key := range []string{"reasoning_level", "_source"} {
		if _, ok := got[key]; ok {
			t.Errorf("内部键 %q 应已清理", key)
		}
	}
	if got["model"] != "legacy" {
		t.Errorf("model 应回填 model_config_obj 的 model: %v", got["model"])
	}
	if got["temperature"] != 0.7 {
		t.Errorf("业务参数应保留: %v", got["temperature"])
	}
}

// context_window 归一：负值/bool/字符串/零 → 删除；正常值保留转 int。
// 对齐 Python: reasoning_injector.py:127-140。
func TestBuildReasoningModelRequestKwargs_ContextWindow归一(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		keep bool
	}{
		{"正常值", 262144, true},
		{"负值", -1, false},
		{"零", 0, false},
		{"布尔", true, false},
		{"字符串", "big", false},
		{"JSON数字", json.Number("131072"), true},
	}
	for _, c := range cases {
		got := BuildReasoningModelRequestKwargs(map[string]any{}, map[string]any{"context_window": c.raw}, "m")
		if c.keep {
			if _, ok := got["context_window"]; !ok {
				t.Errorf("%s: 应保留 context_window", c.name)
			}
		} else if _, ok := got["context_window"]; ok {
			t.Errorf("%s: 应删除 context_window", c.name)
		}
	}
}

// InjectReasoningParams：档位注入 + 采样覆盖 + reasoning_level 不外发。
// 对齐 Python: reasoning_injector.py:71-88。
func TestInjectReasoningParams_档位注入与采样覆盖(t *testing.T) {
	// level 注入
	got := InjectReasoningParams(
		map[string]any{},
		map[string]any{"reasoning_level": "high"},
	)
	reasoning, ok := got["reasoning"].(map[string]string)
	if !ok || reasoning["mode"] != "enabled" || reasoning["effort"] != "high" {
		t.Errorf("high 档位应注入 reasoning: %v", got["reasoning"])
	}
	if _, ok := got["reasoning_level"]; ok {
		t.Errorf("内部提示 reasoning_level 不得外发")
	}
	// 无档位短路（Python: reasoning_injector.py:85-86）
	got = InjectReasoningParams(map[string]any{}, map[string]any{})
	if _, ok := got["reasoning"]; ok {
		t.Errorf("无档位不应注入 reasoning: %v", got)
	}
	// Kimi 采样覆盖（api_base 命中）
	got = InjectReasoningParams(
		map[string]any{"api_base": "https://api.moonshot.cn/v1"},
		map[string]any{},
	)
	if got["temperature"] != 1.0 || got["top_p"] != 0.95 {
		t.Errorf("Kimi 端点应强制采样覆盖: %v", got)
	}
}

// ResolveModelName：显式名优先，否则回 model/model_name。
// 对齐 Python: reasoning_injector.py:55-61。
func TestResolveModelName_回退链(t *testing.T) {
	if got := ResolveModelName("  m  ", nil); got != "m" {
		t.Errorf("显式名去空白优先: %q", got)
	}
	if got := ResolveModelName("", map[string]any{"model": "cfg-model"}); got != "cfg-model" {
		t.Errorf("应回退 model 键: %q", got)
	}
	if got := ResolveModelName("", map[string]any{"model_name": "cfg-name"}); got != "cfg-name" {
		t.Errorf("应回退 model_name 键: %q", got)
	}
	if got := ResolveModelName("", map[string]any{}); got != "" {
		t.Errorf("全空返回空串: %q", got)
	}
}
