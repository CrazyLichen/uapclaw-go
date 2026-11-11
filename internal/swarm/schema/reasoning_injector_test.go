package schema

import (
	"reflect"
	"testing"
)

// TestInjectReasoningParams_无level不注入 没有 reasoning_level 时不写 reasoning 键。
func TestInjectReasoningParams_无level不注入(t *testing.T) {
	got := InjectReasoningParams(
		map[string]any{"model_name": "gpt-4"},
		map[string]any{"temperature": 0.7},
	)
	if _, ok := got["reasoning"]; ok {
		t.Errorf("无 reasoning_level 不应注入 reasoning：%v", got)
	}
	if _, ok := got["reasoning_level"]; ok {
		t.Error("reasoning_level 是内部提示，必须 pop 掉")
	}
	if got["temperature"] != 0.7 {
		t.Errorf("temperature 未保留：%v", got)
	}
}

// TestInjectReasoningParams_有level注入 high → enabled/effort=high。
func TestInjectReasoningParams_有level注入(t *testing.T) {
	got := InjectReasoningParams(
		map[string]any{"model_name": "gpt-4"},
		map[string]any{"reasoning_level": "high"},
	)
	reasoning, ok := got["reasoning"].(map[string]string)
	if !ok {
		t.Fatalf("reasoning 类型 = %T, 期望 map[string]string", got["reasoning"])
	}
	if reasoning["mode"] != "enabled" || reasoning["effort"] != "high" {
		t.Errorf("reasoning = %v", reasoning)
	}
	if _, exists := got["reasoning_level"]; exists {
		t.Error("reasoning_level 必须被 pop")
	}
}

// TestInjectReasoningParams_off档 off → mode=disabled 且无 effort。
func TestInjectReasoningParams_off档(t *testing.T) {
	got := InjectReasoningParams(
		map[string]any{"model_name": "m"},
		map[string]any{"reasoning_level": "off"},
	)
	reasoning, _ := got["reasoning"].(map[string]string)
	if reasoning["mode"] != "disabled" {
		t.Errorf("reasoning = %v", reasoning)
	}
	if _, hasEffort := reasoning["effort"]; hasEffort {
		t.Errorf("off 档不应有 effort：%v", reasoning)
	}
}

// TestInjectReasoningParams_samplingOverride覆盖 Kimi 端点覆盖调用方给的 temperature/top_p。
func TestInjectReasoningParams_samplingOverride覆盖(t *testing.T) {
	got := InjectReasoningParams(
		map[string]any{"api_base": "https://api.moonshot.cn/v1", "model_name": "kimi-k2"},
		map[string]any{"temperature": 0.3, "top_p": 0.5},
	)
	if got["temperature"] != 1.0 {
		t.Errorf("temperature = %v, 期望 1.0（Kimi 强制）", got["temperature"])
	}
	if got["top_p"] != 0.95 {
		t.Errorf("top_p = %v, 期望 0.95", got["top_p"])
	}
}

// TestBuildReasoningModelRequestKwargs_清理内部键。
func TestBuildReasoningModelRequestKwargs_清理内部键(t *testing.T) {
	got := BuildReasoningModelRequestKwargs(
		map[string]any{"model_name": "gpt-4", "api_base": "https://api.openai.com/v1"},
		map[string]any{
			"model": "should-be-popped", "model_name": "should-be-popped",
			"reasoning_level": "low", "_source": "agentos",
			"temperature": 0.5,
		},
		"gpt-4",
	)
	for _, key := range []string{"model_name", "reasoning_level", "_source"} {
		if _, ok := got[key]; ok {
			t.Errorf("%q 应被清理：%v", key, got)
		}
	}
	if got["model"] != "gpt-4" {
		t.Errorf("model = %v, 期望 gpt-4（由 model_name 参数回填）", got["model"])
	}
	if got["temperature"] != 0.5 {
		t.Errorf("temperature = %v", got["temperature"])
	}
	if _, ok := got["reasoning"]; !ok {
		t.Error("reasoning_level=low 应产生 reasoning 键")
	}
}

// TestBuildReasoningModelRequestKwargs_agentos迁移。
func TestBuildReasoningModelRequestKwargs_agentos迁移(t *testing.T) {
	got := BuildReasoningModelRequestKwargs(
		map[string]any{"model_name": "m"},
		map[string]any{"_source": "agentos", "max_tokens": 128000},
		"m",
	)
	if got["context_window"] != 128000 {
		t.Errorf("context_window = %v, 期望 128000", got["context_window"])
	}
	if _, ok := got["max_tokens"]; ok {
		t.Errorf("max_tokens 应被迁走：%v", got)
	}
}

// TestBuildReasoningModelRequestKwargs_contextWindow新字段优先。
func TestBuildReasoningModelRequestKwargs_contextWindow新字段优先(t *testing.T) {
	got := BuildReasoningModelRequestKwargs(
		map[string]any{"model_name": "m"},
		map[string]any{"_source": "agentos", "max_tokens": 999, "context_window": 128000},
		"m",
	)
	if got["context_window"] != 128000 {
		t.Errorf("context_window = %v, 期望 128000（新字段优先）", got["context_window"])
	}
}

// TestBuildReasoningModelRequestKwargs_contextWindow归一 bool/非正数丢弃。
func TestBuildReasoningModelRequestKwargs_contextWindow归一(t *testing.T) {
	for _, bad := range []any{true, false, 0, -5, "abc"} {
		got := BuildReasoningModelRequestKwargs(
			map[string]any{"model_name": "m"},
			map[string]any{"context_window": bad},
			"m",
		)
		if _, ok := got["context_window"]; ok {
			t.Errorf("context_window=%v 应被丢弃：%v", bad, got)
		}
	}
	got := BuildReasoningModelRequestKwargs(
		map[string]any{"model_name": "m"},
		map[string]any{"context_window": "128k"},
		"m",
	)
	// config.ParsePositiveTokens 的 k/m 单位按 1024 进制（既有约定）：128k = 131072
	if got["context_window"] != 131072 {
		t.Errorf("128k 应归一为 131072，得 %v", got["context_window"])
	}
}

// TestBuildReasoningModelRequestKwargs_未知字段保留 对齐 pydantic extra="allow"。
func TestBuildReasoningModelRequestKwargs_未知字段保留(t *testing.T) {
	got := BuildReasoningModelRequestKwargs(
		map[string]any{"model_name": "m"},
		map[string]any{"frequency_penalty": 0.5, "seed": 42},
		"m",
	)
	if got["frequency_penalty"] != 0.5 {
		t.Errorf("frequency_penalty 应保留：%v", got)
	}
	if got["seed"] != 42 {
		t.Errorf("seed 应保留：%v", got)
	}
}

// TestBuildReasoningModelRequestKwargs_不修改入参。
func TestBuildReasoningModelRequestKwargs_不修改入参(t *testing.T) {
	mco := map[string]any{"reasoning_level": "low", "temperature": 0.5}
	snapshot := make(map[string]any, len(mco))
	for k, v := range mco {
		snapshot[k] = v
	}
	_ = BuildReasoningModelRequestKwargs(map[string]any{"model_name": "m"}, mco, "m")
	if !reflect.DeepEqual(mco, snapshot) {
		t.Errorf("入参被修改：前 %v 后 %v", snapshot, mco)
	}
}
