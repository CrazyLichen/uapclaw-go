package openai

import (
	"strings"
	"testing"
)

// ──────────────────────────── ModelParamRule 测试分组 ────────────────────────────

// TestApplyModelSpecificParams_MiniMax注入reasoning_split
// Python: test_openai_model_client.py:211 test_minimax_m_injects_reasoning_split
func TestApplyModelSpecificParams_MiniMax注入reasoning_split(t *testing.T) {
	params := map[string]any{"model": "MiniMax-M3"}
	ApplyModelSpecificParams(defaultModelParamRules, "MiniMax-M3", params)
	eb, ok := params["extra_body"].(map[string]any)
	if !ok || eb["reasoning_split"] != true {
		t.Fatalf("extra_body = %#v", params["extra_body"])
	}
}

// TestApplyModelSpecificParams_非MiniMax不动
// Python: test_openai_model_client.py:218 test_non_minimax_model_leaves_params_untouched
func TestApplyModelSpecificParams_非MiniMax不动(t *testing.T) {
	params := map[string]any{"model": "gpt-4"}
	before := len(params)
	ApplyModelSpecificParams(defaultModelParamRules, "gpt-4", params)
	if len(params) != before {
		t.Fatalf("params 被改动: %#v", params)
	}
	if _, exists := params["extra_body"]; exists {
		t.Fatal("不应创建 extra_body")
	}
}

// TestApplyModelSpecificParams_已有extra_body保留并合并
// Python: test_openai_model_client.py:225 test_existing_extra_body_preserved_and_merged
func TestApplyModelSpecificParams_已有extra_body保留并合并(t *testing.T) {
	params := map[string]any{"extra_body": map[string]any{"keep": 1}}
	ApplyModelSpecificParams(defaultModelParamRules, "MiniMax-M2", params)
	eb := params["extra_body"].(map[string]any)
	if eb["keep"] != 1 || eb["reasoning_split"] != true {
		t.Fatalf("eb = %#v", eb)
	}
}

// TestApplyModelSpecificParams_空model早返回
// Python: test_openai_model_client.py:234 test_none_model_is_noop
func TestApplyModelSpecificParams_空model早返回(t *testing.T) {
	params := map[string]any{}
	ApplyModelSpecificParams(defaultModelParamRules, "", params)
	if len(params) != 0 {
		t.Fatalf("params = %#v", params)
	}
}

// TestDefaultModelParamRules_大小写敏感 关键陷阱：小写 minimax 不匹配。
// Python: test_openai_model_client.py:427 test_default_minimax_predicate_is_case_sensitive
//
// 上游 predicate 是 m.startswith("MiniMax-M")，大写 M。
func TestDefaultModelParamRules_大小写敏感(t *testing.T) {
	for _, model := range []string{"minimax-m3", "MINIMAX-M3", "MiniMax-m3"} {
		params := map[string]any{}
		ApplyModelSpecificParams(defaultModelParamRules, model, params)
		if _, exists := params["extra_body"]; exists {
			t.Errorf("model=%q 不应匹配（predicate 要求前缀 MiniMax-M）", model)
		}
	}
	// 正确的大小写应命中
	params := map[string]any{}
	ApplyModelSpecificParams(defaultModelParamRules, "MiniMax-M3", params)
	if _, exists := params["extra_body"]; !exists {
		t.Error("MiniMax-M3 应命中")
	}
}

// TestDefaultModelParamRules_规模 验证默认规则集只有 1 条。
// Python: openai_model_client.py:87-93 _DEFAULT_MODEL_PARAM_RULES
func TestDefaultModelParamRules_规模(t *testing.T) {
	if len(defaultModelParamRules) != 1 {
		t.Fatalf("默认规则集 = %d 条, want 1", len(defaultModelParamRules))
	}
	if defaultModelParamRules[0].Name != "minimax_reasoning_split" {
		t.Errorf("规则名 = %q", defaultModelParamRules[0].Name)
	}
}

// TestApplyModelSpecificParams_规则集可替换 对齐上游子类扩展语义（Go 用传不同规则集表达）。
// Python: test_openai_model_client.py:242/267 test_subclass_*
func TestApplyModelSpecificParams_规则集可替换(t *testing.T) {
	custom := []ModelParamRule{{
		Name:            "x_marker",
		Predicate:       func(m string) bool { return strings.HasPrefix(m, "x-") },
		ExtraBodyFields: map[string]any{"marker": true},
	}}
	params := map[string]any{}
	ApplyModelSpecificParams(custom, "x-1", params)
	if params["extra_body"].(map[string]any)["marker"] != true {
		t.Fatalf("params = %#v", params)
	}
	// 默认规则集不受污染
	if len(defaultModelParamRules) != 1 {
		t.Fatalf("默认规则集被污染: %d 条", len(defaultModelParamRules))
	}
	// 自定义规则不泄漏到默认规则集
	params2 := map[string]any{}
	ApplyModelSpecificParams(defaultModelParamRules, "x-1", params2)
	if _, exists := params2["extra_body"]; exists {
		t.Fatal("自定义规则泄漏到默认规则集")
	}
}

// TestApplyModelSpecificParams_多规则合并
// Python: test_openai_model_client.py:316 test_multiple_matching_rules_merge_extra_body
func TestApplyModelSpecificParams_多规则合并(t *testing.T) {
	rules := []ModelParamRule{
		{Name: "r1", Predicate: func(m string) bool { return true }, ExtraBodyFields: map[string]any{"a": 1}},
		{Name: "r2", Predicate: func(m string) bool { return true }, ExtraBodyFields: map[string]any{"b": 2}},
	}
	params := map[string]any{}
	ApplyModelSpecificParams(rules, "any", params)
	eb := params["extra_body"].(map[string]any)
	if eb["a"] != 1 || eb["b"] != 2 {
		t.Fatalf("eb = %#v", eb)
	}
}

// TestApplyModelSpecificParams_后规则覆盖同名字段
// Python: test_openai_model_client.py:346 test_later_rule_overrides_earlier_same_field
func TestApplyModelSpecificParams_后规则覆盖同名字段(t *testing.T) {
	rules := []ModelParamRule{
		{Name: "r1", Predicate: func(m string) bool { return true }, ExtraBodyFields: map[string]any{"k": "first"}},
		{Name: "r2", Predicate: func(m string) bool { return true }, ExtraBodyFields: map[string]any{"k": "second"}},
	}
	params := map[string]any{}
	ApplyModelSpecificParams(rules, "any", params)
	if got := params["extra_body"].(map[string]any)["k"]; got != "second" {
		t.Fatalf("k = %v, want second", got)
	}
}

// TestApplyModelSpecificParams_空fields跳过
// Python: test_openai_model_client.py:375 test_empty_extra_body_fields_skipped
func TestApplyModelSpecificParams_空fields跳过(t *testing.T) {
	rules := []ModelParamRule{
		{Name: "empty", Predicate: func(m string) bool { return true }, ExtraBodyFields: nil},
	}
	params := map[string]any{}
	ApplyModelSpecificParams(rules, "any", params)
	if _, exists := params["extra_body"]; exists {
		t.Fatal("空 fields 不应创建 extra_body")
	}
}

// TestApplyModelSpecificParams_nilPredicate跳过 验证 nil 判定不 panic。
func TestApplyModelSpecificParams_nilPredicate跳过(t *testing.T) {
	rules := []ModelParamRule{{Name: "nil", ExtraBodyFields: map[string]any{"k": 1}}}
	params := map[string]any{}
	ApplyModelSpecificParams(rules, "any", params)
	if _, exists := params["extra_body"]; exists {
		t.Fatal("nil predicate 应跳过")
	}
}

// ──────────────────────────── HandleExtraBody（7 键）────────────────────────────

// TestHandleExtraBody_七个键全搬 对齐上游 _OPENAI_EXTRA_BODY_EXTENSION_FIELDS。
// Python: openai_model_client.py:120-131（7 个元素）
func TestHandleExtraBody_七个键全搬(t *testing.T) {
	params := map[string]any{
		"agent_hint":           "h",
		"cache_salt":           "s",
		"cache_sharing":        true,
		"return_token_ids":     true,
		"enable_thinking":      false,
		"thinking":             map[string]any{"type": "disabled"},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"model":                "m",
	}
	HandleExtraBody(params)
	if _, exists := params["model"]; !exists {
		t.Error("model 被误搬")
	}
	eb, ok := params["extra_body"].(map[string]any)
	if !ok {
		t.Fatalf("extra_body = %#v", params["extra_body"])
	}
	if len(eb) != 7 {
		t.Fatalf("extra_body 键数 = %d, want 7: %#v", len(eb), eb)
	}
	for _, k := range []string{
		"agent_hint", "cache_salt", "cache_sharing", "return_token_ids",
		"enable_thinking", "thinking", "chat_template_kwargs",
	} {
		if _, still := params[k]; still {
			t.Errorf("顶层仍残留 %q", k)
		}
		if _, moved := eb[k]; !moved {
			t.Errorf("extra_body 缺 %q", k)
		}
	}
}

// TestHandleExtraBody_扩展到7键 验证不再只搬 return_token_ids。
// Python: openai_model_client.py:1149-1157 循环 _OPENAI_EXTRA_BODY_EXTENSION_FIELDS 全量
func TestHandleExtraBody_扩展到7键(t *testing.T) {
	params := map[string]any{"thinking": map[string]any{"type": "disabled"}}
	HandleExtraBody(params)
	if _, still := params["thinking"]; still {
		t.Fatal("thinking 应被搬入 extra_body")
	}
	eb, ok := params["extra_body"].(map[string]any)
	if !ok || eb["thinking"] == nil {
		t.Fatalf("extra_body = %#v", params["extra_body"])
	}
}

// TestHandleExtraBody_无扩展键不创建
func TestHandleExtraBody_无扩展键不创建(t *testing.T) {
	params := map[string]any{"model": "m"}
	HandleExtraBody(params)
	if _, exists := params["extra_body"]; exists {
		t.Fatal("无扩展键时不应创建 extra_body")
	}
}

// ──────────────────────────── 孤儿 tool_call 裁剪 ────────────────────────────

// TestDropOrphanToolCalls_删除无id无名条目
// Python: llm_provider_compat_patch.py:142-167 _drop_orphan_tool_calls
func TestDropOrphanToolCalls_删除无id无名条目(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "c1", "function": map[string]any{"name": "f1", "arguments": "{}"}},
			map[string]any{"function": map[string]any{"arguments": "{}"}},
		}},
	}
	out := DropOrphanToolCalls(msgs)
	calls := out[0]["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	kept := calls[0].(map[string]any)
	if kept["id"] != "c1" {
		t.Errorf("保留的条目错: %#v", kept)
	}
}

// TestDropOrphanToolCalls_删空则删键
// Python: llm_provider_compat_patch.py:162-163 if not kept: repaired.pop("tool_calls")
func TestDropOrphanToolCalls_删空则删键(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"function": map[string]any{"arguments": "{}"}},
		}},
	}
	out := DropOrphanToolCalls(msgs)
	if _, exists := out[0]["tool_calls"]; exists {
		t.Fatal("删空后应移除 tool_calls 键")
	}
	if out[0]["role"] != "assistant" {
		t.Error("其他字段应保留")
	}
}

// TestDropOrphanToolCalls_无孤儿不动
func TestDropOrphanToolCalls_无孤儿不动(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "c1", "function": map[string]any{"name": "f1"}},
		}},
	}
	out := DropOrphanToolCalls(msgs)
	if len(out[0]["tool_calls"].([]any)) != 1 {
		t.Fatal("不应删除合法条目")
	}
}

// TestDropOrphanToolCalls_无tool_calls键跳过
func TestDropOrphanToolCalls_无tool_calls键跳过(t *testing.T) {
	msgs := []map[string]any{{"role": "user", "content": "hi"}}
	out := DropOrphanToolCalls(msgs)
	if len(out) != 1 || out[0]["content"] != "hi" {
		t.Fatalf("out = %#v", out)
	}
}

// TestDropOrphanToolCalls_非切片tool_calls跳过 验证畸形输入不 panic。
func TestDropOrphanToolCalls_非切片tool_calls跳过(t *testing.T) {
	msgs := []map[string]any{{"role": "assistant", "tool_calls": "not-a-slice"}}
	out := DropOrphanToolCalls(msgs)
	if out[0]["tool_calls"] != "not-a-slice" {
		t.Fatal("畸形输入应原样保留")
	}
}

// TestDropOrphanToolCalls_有id无name保留 验证只有 id 缺失且 name 缺失才删。
// Python: llm_provider_compat_patch.py:142-145 not tool_call.get("id") and not func.get("name")
func TestDropOrphanToolCalls_有id无name保留(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "c1", "function": map[string]any{}},
		}},
	}
	out := DropOrphanToolCalls(msgs)
	if len(out[0]["tool_calls"].([]any)) != 1 {
		t.Fatal("有 id 应保留（即使 name 为空）")
	}
}

// TestDropOrphanToolCalls_有name无id保留
func TestDropOrphanToolCalls_有name无id保留(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"function": map[string]any{"name": "f1"}},
		}},
	}
	out := DropOrphanToolCalls(msgs)
	if len(out[0]["tool_calls"].([]any)) != 1 {
		t.Fatal("有 name 应保留（即使 id 为空）")
	}
}

// TestDropOrphanToolCalls_不改原切片 验证返回新切片。
func TestDropOrphanToolCalls_不改原切片(t *testing.T) {
	original := map[string]any{"role": "assistant", "tool_calls": []any{
		map[string]any{"function": map[string]any{"arguments": "{}"}},
	}}
	msgs := []map[string]any{original}
	out := DropOrphanToolCalls(msgs)
	if _, still := original["tool_calls"]; !still {
		t.Fatal("原始消息 map 不应被就地修改（上游是 list(messages) 后替换元素）")
	}
	if _, exists := out[0]["tool_calls"]; exists {
		t.Fatal("出参应已删除 tool_calls")
	}
}
