package index

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ──────────────────────────── MemoryDoc 测试 ────────────────────────────

func TestMemoryDoc_JSON序列化(t *testing.T) {
	ts := time.Date(2025, 7, 25, 10, 30, 0, 0, time.UTC)
	doc := &MemoryDoc{
		ID:        "mem-001",
		Text:      "用户偏好深色模式",
		Type:      "user_profile",
		Timestamp: ts,
		Fields:    map[string]any{"priority": "high"},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored MemoryDoc
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ID != doc.ID {
		t.Errorf("ID: 期望 %q, 实际 %q", doc.ID, restored.ID)
	}
	if restored.Text != doc.Text {
		t.Errorf("Text: 期望 %q, 实际 %q", doc.Text, restored.Text)
	}
	if restored.Type != doc.Type {
		t.Errorf("Type: 期望 %q, 实际 %q", doc.Type, restored.Type)
	}
	if !restored.Timestamp.Equal(doc.Timestamp) {
		t.Errorf("Timestamp: 期望 %v, 实际 %v", doc.Timestamp, restored.Timestamp)
	}
}

func TestMemoryDoc_Fields为nil时omitempty(t *testing.T) {
	doc := &MemoryDoc{
		ID:   "mem-002",
		Text: "测试",
		Type: "test",
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// Fields 为 nil 时，omitempty 应省略 fields 字段
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("反序列化到 map 失败: %v", err)
	}
	if _, ok := raw["fields"]; ok {
		t.Error("Fields 为 nil 时 JSON 中不应包含 fields 字段")
	}
}

func TestMemoryDoc_零值序列化(t *testing.T) {
	doc := &MemoryDoc{}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored MemoryDoc
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ID != "" {
		t.Errorf("ID: 期望空字符串, 实际 %q", restored.ID)
	}
	if restored.Text != "" {
		t.Errorf("Text: 期望空字符串, 实际 %q", restored.Text)
	}
	if !restored.Timestamp.IsZero() {
		t.Errorf("Timestamp: 期望零值, 实际 %v", restored.Timestamp)
	}
}

// ──────────────────────────── MemorySearchResult 测试 ────────────────────────────

func TestMemorySearchResult_字段访问(t *testing.T) {
	doc := &MemoryDoc{ID: "mem-001", Text: "测试", Type: "test"}
	result := &MemorySearchResult{Doc: doc, Score: 0.95}

	if result.Doc.ID != "mem-001" {
		t.Errorf("Doc.ID: 期望 mem-001, 实际 %q", result.Doc.ID)
	}
	if result.Score != 0.95 {
		t.Errorf("Score: 期望 0.95, 实际 %f", result.Score)
	}
}

// ──────────────────────────── UserScope 测试 ────────────────────────────

func TestUserScope_字段访问(t *testing.T) {
	us := UserScope{UserID: "user-1", ScopeID: "scope-1"}

	if us.UserID != "user-1" {
		t.Errorf("UserID: 期望 user-1, 实际 %q", us.UserID)
	}
	if us.ScopeID != "scope-1" {
		t.Errorf("ScopeID: 期望 scope-1, 实际 %q", us.ScopeID)
	}
}

// ──────────────────────────── StorageCodec 测试 ────────────────────────────

// fakeCodec 用于测试的模拟编解码器
type fakeCodec struct {
	encoded map[string]string
	decoded map[string]string
}

func newFakeCodec() *fakeCodec {
	return &fakeCodec{
		encoded: make(map[string]string),
		decoded: make(map[string]string),
	}
}

func (f *fakeCodec) Encode(text string) string {
	encoded := "enc:" + text
	f.encoded[encoded] = text
	return encoded
}

func (f *fakeCodec) Decode(data string) string {
	decoded := strings.TrimPrefix(data, "enc:")
	f.decoded[data] = decoded
	return decoded
}

func TestStorageCodec_接口约束(t *testing.T) {
	// 验证 fakeCodec 满足 StorageCodec 接口
	var _ StorageCodec = &fakeCodec{}
}

func TestStorageCodec_EncodeDecode(t *testing.T) {
	codec := newFakeCodec()
	original := "敏感数据"
	encoded := codec.Encode(original)
	decoded := codec.Decode(encoded)

	if decoded != original {
		t.Errorf("编解码往返失败: 期望 %q, 实际 %q", original, decoded)
	}
}

// ──────────────────────────── MemoryIndexBase 测试 ────────────────────────────

func TestNewMemoryIndexBase(t *testing.T) {
	base := NewMemoryIndexBase()

	if base == nil {
		t.Fatal("NewMemoryIndexBase 返回 nil")
	}
	if base.backups == nil {
		t.Error("backups map 未初始化")
	}
	if len(base.backups) != 0 {
		t.Errorf("backups map 应为空, 实际长度 %d", len(base.backups))
	}
}

func TestGetSchemaVersion_默认值(t *testing.T) {
	base := NewMemoryIndexBase()

	if v := base.GetSchemaVersion(); v != 0 {
		t.Errorf("默认 schema 版本应为 0, 实际 %d", v)
	}
}

func TestUpdateSchemaVersion(t *testing.T) {
	base := NewMemoryIndexBase()

	base.UpdateSchemaVersion(3)
	if v := base.GetSchemaVersion(); v != 3 {
		t.Errorf("更新后 schema 版本应为 3, 实际 %d", v)
	}

	base.UpdateSchemaVersion(7)
	if v := base.GetSchemaVersion(); v != 7 {
		t.Errorf("再次更新后 schema 版本应为 7, 实际 %d", v)
	}
}

func TestCreateBackup(t *testing.T) {
	base := NewMemoryIndexBase()
	base.UpdateSchemaVersion(5)

	ctx := context.Background()
	bid, err := base.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("CreateBackup 失败: %v", err)
	}
	if bid == "" {
		t.Error("备份 ID 不应为空")
	}
	if len(base.backups) != 1 {
		t.Errorf("backups map 应有 1 条记录, 实际 %d", len(base.backups))
	}
	if data, ok := base.backups[bid]; !ok {
		t.Error("备份 ID 在 backups map 中不存在")
	} else if data.SchemaVersion != 5 {
		t.Errorf("备份中 schema 版本应为 5, 实际 %d", data.SchemaVersion)
	}
}

func TestRestoreBackup_存在(t *testing.T) {
	base := NewMemoryIndexBase()
	base.UpdateSchemaVersion(5)

	ctx := context.Background()
	bid, _ := base.CreateBackup(ctx)

	// 修改版本号后恢复
	base.UpdateSchemaVersion(10)
	if v := base.GetSchemaVersion(); v != 10 {
		t.Fatalf("恢复前 schema 版本应为 10, 实际 %d", v)
	}

	err := base.RestoreBackup(ctx, bid)
	if err != nil {
		t.Fatalf("RestoreBackup 失败: %v", err)
	}
	if v := base.GetSchemaVersion(); v != 5 {
		t.Errorf("恢复后 schema 版本应为 5, 实际 %d", v)
	}
}

func TestRestoreBackup_不存在(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	err := base.RestoreBackup(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("恢复不存在的备份应返回错误")
	}
}

func TestCleanupBackup(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	bid, _ := base.CreateBackup(ctx)

	if len(base.backups) != 1 {
		t.Fatalf("清理前应有 1 条备份, 实际 %d", len(base.backups))
	}

	err := base.CleanupBackup(ctx, bid)
	if err != nil {
		t.Fatalf("CleanupBackup 失败: %v", err)
	}
	if len(base.backups) != 0 {
		t.Errorf("清理后应有 0 条备份, 实际 %d", len(base.backups))
	}
}

func TestCleanupBackup_不存在时不报错(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	err := base.CleanupBackup(ctx, "nonexistent-id")
	if err != nil {
		t.Errorf("清理不存在的备份应返回 nil, 实际 %v", err)
	}
}

func TestListMemories_默认(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	docs, err := base.ListMemories(ctx, "user-1", "scope-1", 0, 100, nil)
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if docs != nil {
		t.Errorf("默认 ListMemories 应返回 nil, 实际 %v", docs)
	}
}

func TestListUserScopes_默认(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	scopes, err := base.ListUserScopes(ctx)
	if err != nil {
		t.Fatalf("ListUserScopes 失败: %v", err)
	}
	if scopes != nil {
		t.Errorf("默认 ListUserScopes 应返回 nil, 实际 %v", scopes)
	}
}
