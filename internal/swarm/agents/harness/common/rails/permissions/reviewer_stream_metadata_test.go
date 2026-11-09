package permissions

import (
	"testing"
)

// ──────────────────────────── 常量 ────────────────────────────

func TestReviewerToolResultMetadataExtraKey(t *testing.T) {
	want := "permission_reviewer_metadata_by_tool_call_id"
	if ReviewerToolResultMetadataExtraKey != want {
		t.Errorf("got %q, want %q", ReviewerToolResultMetadataExtraKey, want)
	}
}

// ──────────────────────────── Record ────────────────────────────

// TestRecordReviewerToolResultMetadata_写入 验证建桶并写入。
func TestRecordReviewerToolResultMetadata_写入(t *testing.T) {
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"route": "auto"})

	bucket, ok := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	if !ok {
		t.Fatalf("bucket type = %T", extra[ReviewerToolResultMetadataExtraKey])
	}
	got, ok := bucket["call-1"].(map[string]any)
	if !ok || got["route"] != "auto" {
		t.Errorf("got %v", bucket["call-1"])
	}
}

// TestRecordReviewerToolResultMetadata_深拷贝 验证写入后修改原 map 不影响桶。
func TestRecordReviewerToolResultMetadata_深拷贝(t *testing.T) {
	extra := map[string]any{}
	metadata := map[string]any{"nested": map[string]any{"k": "v"}}
	RecordReviewerToolResultMetadata(extra, "call-1", metadata)

	metadata["nested"].(map[string]any)["k"] = "changed"

	bucket, _ := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	got, _ := bucket["call-1"].(map[string]any)
	if got["nested"].(map[string]any)["k"] != "v" {
		t.Error("metadata should be deep-copied")
	}
}

// TestRecordReviewerToolResultMetadata_各提前返回 验证 nil/空 id/空 metadata。
func TestRecordReviewerToolResultMetadata_各提前返回(t *testing.T) {
	// nil extra 不 panic
	RecordReviewerToolResultMetadata(nil, "call-1", map[string]any{"k": "v"})

	// 空 tool_call_id
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "", map[string]any{"k": "v"})
	if _, exists := extra[ReviewerToolResultMetadataExtraKey]; exists {
		t.Error("empty tool_call_id should not create bucket")
	}

	// 空 metadata
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{})
	if _, exists := extra[ReviewerToolResultMetadataExtraKey]; exists {
		t.Error("empty metadata should not create bucket")
	}
}

// TestRecordReviewerToolResultMetadata_覆盖非法桶 验证桶类型不对时重建。
func TestRecordReviewerToolResultMetadata_覆盖非法桶(t *testing.T) {
	extra := map[string]any{ReviewerToolResultMetadataExtraKey: "not-a-map"}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"k": "v"})

	bucket, ok := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	if !ok {
		t.Fatalf("bucket should be rebuilt, got %T", extra[ReviewerToolResultMetadataExtraKey])
	}
	if _, exists := bucket["call-1"]; !exists {
		t.Error("should write into rebuilt bucket")
	}
}

// TestRecordReviewerToolResultMetadata_多条目 验证同一桶多个 id。
func TestRecordReviewerToolResultMetadata_多条目(t *testing.T) {
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"a": 1})
	RecordReviewerToolResultMetadata(extra, "call-2", map[string]any{"b": 2})

	bucket, _ := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	if len(bucket) != 2 {
		t.Errorf("bucket size = %d, want 2", len(bucket))
	}
}

// ──────────────────────────── Consume ────────────────────────────

// TestConsumeReviewerToolResultMetadata_取出并删除 验证消费语义。
func TestConsumeReviewerToolResultMetadata_取出并删除(t *testing.T) {
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"route": "auto"})

	got := ConsumeReviewerToolResultMetadata(extra, "call-1")
	if got == nil || got["route"] != "auto" {
		t.Errorf("got %v", got)
	}
	// 桶已空 → 连桶一起删除
	if _, exists := extra[ReviewerToolResultMetadataExtraKey]; exists {
		t.Error("empty bucket should be removed")
	}
	// 再消费一次返回 nil
	if again := ConsumeReviewerToolResultMetadata(extra, "call-1"); again != nil {
		t.Errorf("second consume should be nil, got %v", again)
	}
}

// TestConsumeReviewerToolResultMetadata_保留其他条目 验证桶未空时保留。
func TestConsumeReviewerToolResultMetadata_保留其他条目(t *testing.T) {
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"a": 1})
	RecordReviewerToolResultMetadata(extra, "call-2", map[string]any{"b": 2})

	_ = ConsumeReviewerToolResultMetadata(extra, "call-1")

	bucket, ok := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	if !ok {
		t.Fatal("bucket should still exist")
	}
	if len(bucket) != 1 {
		t.Errorf("bucket size = %d, want 1", len(bucket))
	}
	if _, exists := bucket["call-2"]; !exists {
		t.Error("call-2 should remain")
	}
}

// TestConsumeReviewerToolResultMetadata_各提前返回 验证 nil/空 id/非法桶。
func TestConsumeReviewerToolResultMetadata_各提前返回(t *testing.T) {
	if got := ConsumeReviewerToolResultMetadata(nil, "call-1"); got != nil {
		t.Errorf("nil extra -> %v", got)
	}
	extra := map[string]any{}
	if got := ConsumeReviewerToolResultMetadata(extra, ""); got != nil {
		t.Errorf("empty id -> %v", got)
	}
	// 非法桶 → 删除键并返回 nil
	extra[ReviewerToolResultMetadataExtraKey] = "not-a-map"
	if got := ConsumeReviewerToolResultMetadata(extra, "call-1"); got != nil {
		t.Errorf("invalid bucket -> %v", got)
	}
	if _, exists := extra[ReviewerToolResultMetadataExtraKey]; exists {
		t.Error("invalid bucket should be removed")
	}
}

// TestConsumeReviewerToolResultMetadata_深拷贝 验证返回值是副本。
func TestConsumeReviewerToolResultMetadata_深拷贝(t *testing.T) {
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"k": "v"})

	got := ConsumeReviewerToolResultMetadata(extra, "call-1")
	got["k"] = "changed"

	// 再写一次确认桶里是原值（桶已被删，重新写入验证不影响）
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"k": "v2"})
	again := ConsumeReviewerToolResultMetadata(extra, "call-1")
	if again["k"] != "v2" {
		t.Errorf("got %v", again)
	}
}

// ──────────────────────────── Peek ────────────────────────────

// TestPeekReviewerToolResultMetadata_只读 验证不删除条目。
func TestPeekReviewerToolResultMetadata_只读(t *testing.T) {
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"route": "auto"})

	got := PeekReviewerToolResultMetadata(extra, "call-1")
	if got == nil || got["route"] != "auto" {
		t.Errorf("got %v", got)
	}
	// 条目仍在
	bucket, ok := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	if !ok {
		t.Fatal("bucket should still exist")
	}
	if _, exists := bucket["call-1"]; !exists {
		t.Error("peek should not remove entry")
	}
}

// TestPeekReviewerToolResultMetadata_各提前返回 验证 nil/空 id/非法桶。
func TestPeekReviewerToolResultMetadata_各提前返回(t *testing.T) {
	if got := PeekReviewerToolResultMetadata(nil, "call-1"); got != nil {
		t.Errorf("nil extra -> %v", got)
	}
	extra := map[string]any{}
	if got := PeekReviewerToolResultMetadata(extra, ""); got != nil {
		t.Errorf("empty id -> %v", got)
	}
	if got := PeekReviewerToolResultMetadata(extra, "missing"); got != nil {
		t.Errorf("missing bucket -> %v", got)
	}
	extra[ReviewerToolResultMetadataExtraKey] = "not-a-map"
	if got := PeekReviewerToolResultMetadata(extra, "call-1"); got != nil {
		t.Errorf("invalid bucket -> %v", got)
	}
	// peek 不清空非法桶（对齐 Python 只 return None）
	if _, exists := extra[ReviewerToolResultMetadataExtraKey]; !exists {
		t.Error("peek should not clean invalid bucket")
	}
}

// TestPeekReviewerToolResultMetadata_深拷贝 验证返回值是副本。
func TestPeekReviewerToolResultMetadata_深拷贝(t *testing.T) {
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"nested": map[string]any{"k": "v"}})

	got := PeekReviewerToolResultMetadata(extra, "call-1")
	got["nested"].(map[string]any)["k"] = "changed"

	again := PeekReviewerToolResultMetadata(extra, "call-1")
	if again["nested"].(map[string]any)["k"] != "v" {
		t.Error("peek should return a deep copy")
	}
}

// TestPeekReviewerToolResultMetadata_缺id返回nil 验证桶在但 id 不在。
func TestPeekReviewerToolResultMetadata_缺id返回nil(t *testing.T) {
	extra := map[string]any{}
	RecordReviewerToolResultMetadata(extra, "call-1", map[string]any{"a": 1})
	if got := PeekReviewerToolResultMetadata(extra, "call-2"); got != nil {
		t.Errorf("missing id -> %v", got)
	}
}
