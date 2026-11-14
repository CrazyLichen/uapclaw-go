package common

import (
	"errors"
	"strings"
	"testing"
)

// ModelProbeOutput 三源判输出。
// 对齐 Python: model_config_validation.py:41-81。
func TestModelProbeOutput_三源判输出(t *testing.T) {
	// 正常 content
	_, ok := ModelProbeOutput(map[string]any{"content": "hello"})
	if !ok {
		t.Errorf("非空 content 应有输出")
	}
	// reasoning 模型：content 空、reasoning_content 非空
	_, ok = ModelProbeOutput(map[string]any{"content": "", "reasoning_content": "thinking..."})
	if !ok {
		t.Errorf("reasoning_content 非空应有输出")
	}
	// Ollama 场景：content 空 + usage.output_tokens > 0
	_, ok = ModelProbeOutput(map[string]any{"content": "", "usage_metadata": map[string]any{"output_tokens": 3}})
	if !ok {
		t.Errorf("output_tokens>0 应有输出")
	}
	// 全空
	_, ok = ModelProbeOutput(map[string]any{"content": ""})
	if ok {
		t.Errorf("全空应无输出")
	}
	// content 返回给调用方
	content, _ := ModelProbeOutput(map[string]any{"content": "hi"})
	if content != "hi" {
		t.Errorf("content 应透传: %q", content)
	}
}

// ProbeModelConnection：预算降级重试与最终失败上抛。
// 对齐 Python: model_config_validation.py:87-129。
func TestProbeModelConnection_预算降级(t *testing.T) {
	// 第一次失败、第二次成功
	attempts := 0
	invoker := func(maxTokens int, _ map[string]any) (any, error) {
		attempts++
		if maxTokens == 3 {
			return nil, errors.New("boom")
		}
		return map[string]any{"content": "ok"}, nil
	}
	result, err := ProbeModelConnection(invoker, []int{3, 16}, nil)
	if err != nil || result.Content != "ok" || attempts != 2 {
		t.Errorf("降级重试应成功: %v %v %d", result, err, attempts)
	}
	// 全部失败 → 最后错误上抛
	invoker = func(int, map[string]any) (any, error) { return nil, errors.New("always") }
	_, err = ProbeModelConnection(invoker, []int{3, 16}, nil)
	if err == nil || err.Error() != "always" {
		t.Errorf("最终失败应上抛: %v", err)
	}
	// 响应无输出也算失败尝试（Python: :110-111）
	invoker = func(maxTokens int, _ map[string]any) (any, error) {
		if maxTokens == 3 {
			return map[string]any{"content": ""}, nil
		}
		return map[string]any{"content": "ok"}, nil
	}
	result, err = ProbeModelConnection(invoker, []int{3, 16}, nil)
	if err != nil || result == nil {
		t.Errorf("空响应后降级应成功: %v", err)
	}
	// 空 token_limits 拒绝（Python: :96-98）
	_, err = ProbeModelConnection(invoker, nil, nil)
	if err == nil || err.Error() != "token_limits must contain at least one value" {
		t.Errorf("空预算应拒绝: %v", err)
	}
}

// IsPlaceholderAPIBase：固定占位串 + 示例域含子域名。
// 对齐 Python: model_config_validation.py:121-131。
func TestIsPlaceholderAPIBase_占位与示例域(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.com/compatible-mode/v1", true},
		{"https://example.com/v1", true},
		{"https://api.example.com/v1", true},
		{"https://example.org", true},
		{"https://real.example.cn/v1", false},
		{"", false},
		{"https://api.real.com/v1", false},
	}
	for _, c := range cases {
		if got := IsPlaceholderAPIBase(c.in); got != c.want {
			t.Errorf("IsPlaceholderAPIBase(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// IsPlaceholderModelEntry：三占位任一命中。
// 对齐 Python: model_config_validation.py:133-141。
func TestIsPlaceholderModelEntry_三占位(t *testing.T) {
	if !IsPlaceholderModelEntry(map[string]any{"model_name": "your-model-name"}) {
		t.Errorf("占位模型名应命中")
	}
	if !IsPlaceholderModelEntry(map[string]any{"api_key": "sk-xxxxxxxxx"}) {
		t.Errorf("占位密钥应命中")
	}
	if !IsPlaceholderModelEntry(map[string]any{"api_base": "https://example.com/v1"}) {
		t.Errorf("示例域地址应命中")
	}
	if IsPlaceholderModelEntry(map[string]any{"model_name": "gpt-x", "api_key": "sk-real", "api_base": "https://real.com"}) {
		t.Errorf("正常条目不应命中")
	}
	if IsPlaceholderModelEntry(nil) == false && false {
		t.Error("unreachable")
	}
}

// ModelClientConfigView 三键视图。
// 对齐 Python: model_config_validation.py:139-143。
func TestModelClientConfigView_三键(t *testing.T) {
	view := ModelClientConfigView(map[string]any{"api_base": "https://a", "model_name": "m", "extra": "x"})
	if len(view) != 3 || view["api_base"] != "https://a" || view["model_name"] != "m" {
		t.Errorf("视图应只含三键: %v", view)
	}
}

// ValidateModelsConfig：逐类错误文案。
// 对齐 Python: model_config_validation.py:196-292。
func TestValidateModelsConfig_错误文案(t *testing.T) {
	// 合法配置零错误
	good := map[string]any{
		"defaults": []any{map[string]any{"model_id": "mdl_a", "model_client_config": map[string]any{"model_name": "m"}}},
		"agentos":  []any{},
		"groups": []any{map[string]any{
			"model_group_id": "mgp_a", "is_default": true, "enabled": true,
			"routes": []any{map[string]any{"route_id": "rte_1", "model_id": "mdl_a", "enabled": true}},
		}},
	}
	if errs := ValidateModelsConfig(good); len(errs) != 0 {
		t.Fatalf("合法配置不应有错误: %v", errs)
	}
	bad := map[string]any{
		"defaults": []any{
			map[string]any{},                                        // model_id required
			map[string]any{"model_id": "mdl_a"},                     // ok
			map[string]any{"model_id": "mdl_a"},                     // duplicate
			map[string]any{"model_id": "mdl_c", "model_client_config": "not-map"}, // must be object
		},
		"agentos": []any{map[string]any{"model_id": "mdl_a", "is_default": true}}, // cannot be default（+duplicate）
		"groups": []any{
			map[string]any{"model_group_id": "mgp_a", "is_default": true, "enabled": false, "routes": []any{}}, // disabled default + no routes
			map[string]any{"model_group_id": "mgp_a"}, // duplicate
		},
	}
	errs := ValidateModelsConfig(bad)
	joined := strings.Join(errs, "; ")
	for _, want := range []string{
		"defaults[0].model_id is required",
		"duplicate model_id: mdl_a",
		"defaults[3].model_client_config must be an object",
		"agentos[0] cannot be default",
		"default model group \"mgp_a\" is disabled",
		"model group \"mgp_a\" must contain at least one route",
		"duplicate model_group_id: mgp_a",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("缺少错误文案 %q，实际: %s", want, joined)
		}
	}
}

// 路由引用缺失模型与补丁禁用键。
// 对齐 Python: model_config_validation.py:270-274/:154-162。
func TestValidateModelsConfig_路由引用与禁用键(t *testing.T) {
	bad := map[string]any{
		"defaults": []any{map[string]any{"model_id": "mdl_a"}},
		"groups": []any{map[string]any{
			"model_group_id": "mgp_a",
			"routes": []any{map[string]any{
				"route_id": "rte_1", "model_id": "mdl_missing",
				"request_overrides": map[string]any{"model": "hijack", "stream": true},
			}},
			"request_config": map[string]any{"api_key": "x"},
			"routing":        map[string]any{"strategy": "bogus", "num_retries": -1, "timeout_seconds": -5},
		}},
	}
	errs := ValidateModelsConfig(bad)
	joined := strings.Join(errs, "; ")
	for _, want := range []string{
		"group \"mgp_a\" references missing model_id \"mdl_missing\"",
		"group \"mgp_a\" route \"rte_1\" contains forbidden fields: model, stream",
		"group \"mgp_a\".request_config contains forbidden fields: api_key",
		"group \"mgp_a\".routing.strategy is invalid",
		"group \"mgp_a\".routing.num_retries must be a non-negative integer",
		"group \"mgp_a\".routing.timeout_seconds must be a positive finite number",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("缺少 %q，实际: %s", want, joined)
		}
	}
}

// RaiseIfInvalid：错误合并与 details。
// 对齐 Python: model_config_validation.py:294-299。
func TestRaiseIfInvalid_合并与details(t *testing.T) {
	if err := RaiseIfInvalid(map[string]any{}); err != nil {
		// 空对象 = 三源缺省空列表 = 合法（Python: models.get("defaults") or []）
		t.Errorf("空 models 不应有错误: %v", err)
	}
	bad := map[string]any{"defaults": "not-a-list"}
	err := RaiseIfInvalid(bad)
	se, ok := err.(*ModelSelectionError)
	if !ok || se.Code != ModelGroupInvalid {
		t.Fatalf("应抛 MODEL_GROUP_INVALID: %v", err)
	}
	if se.Message != "models.defaults, models.agentos and models.groups must be arrays" {
		t.Errorf("文案不符: %s", se.Message)
	}
	if _, ok := se.Details["errors"]; !ok {
		t.Errorf("details 应含 errors 列表")
	}
}

// validateRouting 的超时键合法值形态。
// 对齐 Python: model_config_validation.py:199-213。
func TestValidateRouting_超时键(t *testing.T) {
	base := map[string]any{"model_group_id": "mgp_a", "routes": []any{map[string]any{"route_id": "rte_1", "model_id": "m"}}}
	defaults := []any{map[string]any{"model_id": "m"}}
	cases := []struct {
		value any
		valid bool
	}{
		{30, true}, {30.5, true}, {"30", true}, {"  ", false},
		{true, false}, {0, false}, {-1, false}, {"abc", false},
	}
	for _, c := range cases {
		models := map[string]any{
			"defaults": defaults,
			"groups": []any{map[string]any{
				"model_group_id": "mgp_a",
				"routes":         base["routes"].([]any),
				"routing":        map[string]any{"timeout_seconds": c.value},
			}},
		}
		errs := ValidateModelsConfig(models)
		if c.valid && len(errs) != 0 {
			t.Errorf("值 %v 应合法，错误: %v", c.value, errs)
		}
		if !c.valid && len(errs) == 0 {
			t.Errorf("值 %v 应非法", c.value)
		}
	}
}
