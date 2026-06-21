package processor

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextEvent_字段默认值 验证 ContextEvent 零值
func TestContextEvent_字段默认值(t *testing.T) {
	var e ContextEvent
	if e.EventType != "" {
		t.Errorf("EventType 零值应为空串，实际 %q", e.EventType)
	}
	if e.MessagesToModify != nil {
		t.Errorf("MessagesToModify 零值应为 nil，实际 %v", e.MessagesToModify)
	}
	if e.CompactSummary != "" {
		t.Errorf("CompactSummary 零值应为空串，实际 %q", e.CompactSummary)
	}
	if e.CompressionUsage != nil {
		t.Errorf("CompressionUsage 零值应为 nil，实际 %v", e.CompressionUsage)
	}
}

// TestContextEvent_构造 验证结构体字面量构造
func TestContextEvent_构造(t *testing.T) {
	e := &ContextEvent{
		EventType:        "DialogueCompressor",
		MessagesToModify: []int{0, 1, 2},
		CompactSummary:   "压缩了3条消息",
		CompressionUsage: map[string]any{
			"calls":        1,
			"total_tokens": 500,
		},
	}
	if e.EventType != "DialogueCompressor" {
		t.Errorf("EventType = %q, want DialogueCompressor", e.EventType)
	}
	if len(e.MessagesToModify) != 3 {
		t.Errorf("MessagesToModify 长度 = %d, want 3", len(e.MessagesToModify))
	}
	if e.CompactSummary != "压缩了3条消息" {
		t.Errorf("CompactSummary = %q, want 压缩了3条消息", e.CompactSummary)
	}
	if e.CompressionUsage["calls"] != 1 {
		t.Errorf("CompressionUsage[calls] = %v, want 1", e.CompressionUsage["calls"])
	}
}

// TestContextEvent_JSON序列化 验证 JSON 序列化/反序列化
func TestContextEvent_JSON序列化(t *testing.T) {
	original := &ContextEvent{
		EventType:        "MessageOffloader",
		MessagesToModify: []int{5},
		CompactSummary:   "卸载了1条消息",
		CompressionUsage: map[string]any{"input_tokens": float64(100)},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored ContextEvent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.EventType != original.EventType {
		t.Errorf("EventType = %q, want %q", restored.EventType, original.EventType)
	}
	if len(restored.MessagesToModify) != 1 || restored.MessagesToModify[0] != 5 {
		t.Errorf("MessagesToModify = %v, want [5]", restored.MessagesToModify)
	}
	if restored.CompactSummary != original.CompactSummary {
		t.Errorf("CompactSummary = %q, want %q", restored.CompactSummary, original.CompactSummary)
	}
}

// TestContextEvent_JSON省略空字段 验证 omitempty 行为
func TestContextEvent_JSON省略空字段(t *testing.T) {
	e := ContextEvent{EventType: "test"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	// CompressionUsage 为 nil 时应省略
	if _, ok := m["compression_usage"]; ok {
		t.Error("compression_usage 应被 omitempty 省略")
	}
	// EventType 应保留
	if _, ok := m["event_type"]; !ok {
		t.Error("event_type 不应被省略")
	}
}
