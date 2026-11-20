package vector_store

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestChromaMapWhereFilter 验证 map 过滤器的 String / Validate / JSON 往返。
func TestChromaMapWhereFilter(t *testing.T) {
	f := &chromaMapWhereFilter{m: map[string]any{"key": "value"}}

	t.Run("Validate 恒通过", func(t *testing.T) {
		if err := f.Validate(); err != nil {
			t.Fatalf("map 过滤器应恒通过，实际 %v", err)
		}
	})

	t.Run("String 输出 JSON", func(t *testing.T) {
		got := f.String()
		if got == "" {
			t.Fatal("String 不应为空")
		}
		var back map[string]any
		if err := json.Unmarshal([]byte(got), &back); err != nil {
			t.Fatalf("应输出合法 JSON，实际 %q: %v", got, err)
		}
		if back["key"] != "value" {
			t.Fatalf("往返后内容不符: %v", back)
		}
	})

	t.Run("MarshalJSON 与 UnmarshalJSON 往返", func(t *testing.T) {
		b, err := f.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON 失败: %v", err)
		}
		var reloaded chromaMapWhereFilter
		if err := reloaded.UnmarshalJSON(b); err != nil {
			t.Fatalf("UnmarshalJSON 失败: %v", err)
		}
		if reloaded.m["key"] != "value" {
			t.Fatalf("往返后内容不符: %v", reloaded.m)
		}
	})

	t.Run("UnmarshalJSON 非法输入报错", func(t *testing.T) {
		var reloaded chromaMapWhereFilter
		if err := reloaded.UnmarshalJSON([]byte("not-json")); err == nil {
			t.Fatal("非法 JSON 应报错")
		}
	})
}

// TestChromaMapWhereDocumentFilter 验证 where_document 过滤器的各方法。
func TestChromaMapWhereDocumentFilter(t *testing.T) {
	f := &chromaMapWhereDocumentFilter{m: map[string]any{"$contains": "x"}}

	t.Run("Operator 返回空（map 自带操作符）", func(t *testing.T) {
		if got := f.Operator(); got != "" {
			t.Fatalf("Operator 应为空串，实际 %q", got)
		}
	})

	t.Run("Operand 返回原 map", func(t *testing.T) {
		got, ok := f.Operand().(map[string]any)
		if !ok || got["$contains"] != "x" {
			t.Fatalf("Operand = %v", f.Operand())
		}
	})

	t.Run("Validate 恒通过", func(t *testing.T) {
		if err := f.Validate(); err != nil {
			t.Fatalf("应恒通过，实际 %v", err)
		}
	})

	t.Run("String 输出 JSON", func(t *testing.T) {
		if got := f.String(); got == "" {
			t.Fatal("String 不应为空")
		}
	})

	t.Run("JSON 往返", func(t *testing.T) {
		b, err := f.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON 失败: %v", err)
		}
		var reloaded chromaMapWhereDocumentFilter
		if err := reloaded.UnmarshalJSON(b); err != nil {
			t.Fatalf("UnmarshalJSON 失败: %v", err)
		}
		if reloaded.m["$contains"] != "x" {
			t.Fatalf("往返后内容不符: %v", reloaded.m)
		}
	})
}

// TestChromaWhereFilter_NilClause 验证 clause 为 nil 时各方法的降级行为。
func TestChromaWhereFilter_NilClause(t *testing.T) {
	f := &chromaWhereFilter{}

	t.Run("String 返回空串", func(t *testing.T) {
		if got := f.String(); got != "" {
			t.Fatalf("应返回空串，实际 %q", got)
		}
	})

	t.Run("Validate 返回 nil", func(t *testing.T) {
		if err := f.Validate(); err != nil {
			t.Fatalf("应返回 nil，实际 %v", err)
		}
	})

	t.Run("MarshalJSON 返回 null", func(t *testing.T) {
		b, err := f.MarshalJSON()
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if string(b) != "null" {
			t.Fatalf("应返回 null，实际 %q", string(b))
		}
	})

	t.Run("UnmarshalJSON 空转", func(t *testing.T) {
		if err := f.UnmarshalJSON([]byte(`{"a":1}`)); err != nil {
			t.Fatalf("clause 为 nil 时应空转，实际 %v", err)
		}
	})
}
