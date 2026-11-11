package interrupt

import "testing"

// TestIterInteractions_扁平列表 测试普通列表原样返回。
func TestIterInteractions_扁平列表(t *testing.T) {
	inputs := []any{
		map[string]any{"id": "r1"},
		map[string]any{"id": "r2"},
	}
	got := iterInteractions(inputs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

// TestIterInteractions_嵌套列表递归 测试 list 元素被展开。
func TestIterInteractions_嵌套列表递归(t *testing.T) {
	inputs := []any{
		[]any{
			map[string]any{"id": "r1"},
			map[string]any{"id": "r2"},
		},
	}
	got := iterInteractions(inputs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2（嵌套 list 应被递归展开）", len(got))
	}
}

// TestIterInteractions_拆__interaction__包装 测试 SDK 状态 schema 解包。
//
// Python: getattr(interaction, "type", None) == "__interaction__"
//
//	→ yield from _iter_interactions([interaction.payload])
func TestIterInteractions_拆interaction包装(t *testing.T) {
	inputs := []any{
		map[string]any{
			"type":    "__interaction__",
			"payload": map[string]any{"id": "r1", "value": map[string]any{}},
		},
	}
	got := iterInteractions(inputs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	first, _ := got[0].(map[string]any)
	if first == nil || first["id"] != "r1" {
		t.Errorf("id = %v, want r1（包装应被拆开）", first["id"])
	}
}

// TestIterInteractions_嵌套包装 测试多层包装递归。
func TestIterInteractions_嵌套包装(t *testing.T) {
	inputs := []any{
		map[string]any{
			"type": "__interaction__",
			"payload": map[string]any{
				"type":    "__interaction__",
				"payload": map[string]any{"id": "deep"},
			},
		},
	}
	got := iterInteractions(inputs)
	first, _ := got[0].(map[string]any)
	if len(got) != 1 || first == nil || first["id"] != "deep" {
		t.Errorf("got %v, want 单条 id=deep", got)
	}
}

// TestIterInteractions_空输入 测试空列表。
func TestIterInteractions_空输入(t *testing.T) {
	if got := iterInteractions(nil); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestIterInteractions_非map元素保留 测试标量元素不被丢弃。
func TestIterInteractions_非map元素保留(t *testing.T) {
	got := iterInteractions([]any{"plain", 42})
	if len(got) != 2 {
		t.Errorf("len = %d, want 2（非 map 元素应原样保留）", len(got))
	}
}

// TestExtractInteractionParts_取id与value 测试提取。
func TestExtractInteractionParts_取id与value(t *testing.T) {
	id, value := extractInteractionParts(map[string]any{
		"id":    " r1 ",
		"value": map[string]any{"interrupt_kind": "permission"},
	})
	if id != "r1" {
		t.Errorf("id = %q, want r1（应 trim）", id)
	}
	if value == nil {
		t.Error("value 为 nil")
	}
}

// TestExtractInteractionParts_value缺省为空map 测试默认值。
//
// Python: value_obj = interaction.get("value", {})
func TestExtractInteractionParts_value缺省为空map(t *testing.T) {
	id, value := extractInteractionParts(map[string]any{"id": "r1"})
	if id != "r1" {
		t.Errorf("id = %q", id)
	}
	if _, ok := value.(map[string]any); !ok {
		t.Errorf("value = %T, want map[string]any（缺省应为空 map）", value)
	}
}

// TestExtractInteractionParts_nil输入 测试 nil。
func TestExtractInteractionParts_nil输入(t *testing.T) {
	id, value := extractInteractionParts(nil)
	if id != "" || value != nil {
		t.Errorf("got (%q, %v), want (\"\", nil)", id, value)
	}
}
