package migrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
)

// ──────────────────────────── 辅助结构体 ────────────────────────────

// fakeMemoryIndex 测试用假内存索引
type fakeMemoryIndex struct {
	// docs 文档存储: userID_scopeID → docs
	docs map[string][]*index.MemoryDoc
	// schemaVersion 当前 schema 版本
	schemaVersion int
	// backupData 备份数据
	backupData map[string]map[string][]*index.MemoryDoc
	// backupIDCounter 备份 ID 计数器
	backupIDCounter int
}

func newFakeMemoryIndex() *fakeMemoryIndex {
	return &fakeMemoryIndex{
		docs:       make(map[string][]*index.MemoryDoc),
		backupData: make(map[string]map[string][]*index.MemoryDoc),
	}
}

func (f *fakeMemoryIndex) key(userID, scopeID string) string {
	return userID + "_" + scopeID
}

func (f *fakeMemoryIndex) AddMemories(_ context.Context, userID, scopeID string, memories []*index.MemoryDoc) error {
	key := f.key(userID, scopeID)
	f.docs[key] = append(f.docs[key], memories...)
	return nil
}

func (f *fakeMemoryIndex) DeleteMemories(_ context.Context, userID, scopeID string, ids []string) error {
	key := f.key(userID, scopeID)
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	var filtered []*index.MemoryDoc
	for _, doc := range f.docs[key] {
		if !idSet[doc.ID] {
			filtered = append(filtered, doc)
		}
	}
	f.docs[key] = filtered
	return nil
}

func (f *fakeMemoryIndex) ListMemories(_ context.Context, userID, scopeID string, offset, limit int, _ []string) ([]*index.MemoryDoc, error) {
	key := f.key(userID, scopeID)
	docs := f.docs[key]
	if offset >= len(docs) {
		return nil, nil
	}
	end := offset + limit
	if end > len(docs) {
		end = len(docs)
	}
	result := make([]*index.MemoryDoc, end-offset)
	copy(result, docs[offset:end])
	return result, nil
}

func (f *fakeMemoryIndex) GetSchemaVersion() int {
	return f.schemaVersion
}

func (f *fakeMemoryIndex) UpdateSchemaVersion(version int) {
	f.schemaVersion = version
}

func (f *fakeMemoryIndex) CreateBackup(_ context.Context) (string, error) {
	f.backupIDCounter++
	backupID := fmt.Sprintf("backup_%d", f.backupIDCounter)
	backup := make(map[string][]*index.MemoryDoc)
	for k, v := range f.docs {
		copied := make([]*index.MemoryDoc, len(v))
		copy(copied, v)
		backup[k] = copied
	}
	f.backupData[backupID] = backup
	return backupID, nil
}

func (f *fakeMemoryIndex) RestoreBackup(_ context.Context, backupID string) error {
	backup, ok := f.backupData[backupID]
	if !ok {
		return fmt.Errorf("备份不存在: %s", backupID)
	}
	f.docs = make(map[string][]*index.MemoryDoc)
	for k, v := range backup {
		copied := make([]*index.MemoryDoc, len(v))
		copy(copied, v)
		f.docs[k] = copied
	}
	return nil
}

func (f *fakeMemoryIndex) CleanupBackup(_ context.Context, backupID string) error {
	delete(f.backupData, backupID)
	return nil
}

func (f *fakeMemoryIndex) DeleteByUser(_ context.Context, _ string) error {
	return nil
}

func (f *fakeMemoryIndex) DeleteByUserAndScope(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeMemoryIndex) DeleteByScope(_ context.Context, _ string) error {
	return nil
}

func (f *fakeMemoryIndex) UpdateMemories(_ context.Context, _, _ string, _ []*index.MemoryDoc) error {
	return nil
}

func (f *fakeMemoryIndex) SetStorageCodec(_ index.StorageCodec) {}

func (f *fakeMemoryIndex) GetByID(_ context.Context, _, _, _ string) (*index.MemoryDoc, error) {
	return nil, nil
}

func (f *fakeMemoryIndex) Search(_ context.Context, _, _ string, _ string, _ []string, _ int) ([]*index.MemorySearchResult, error) {
	return nil, nil
}

func (f *fakeMemoryIndex) SearchMemories(_ context.Context, _, _ string, _ []float64, _ string, _ int, _ map[string]any, _ []string) ([]*index.MemoryDoc, error) {
	return nil, nil
}

func (f *fakeMemoryIndex) ListUserScopes(_ context.Context) ([]index.UserScope, error) {
	var scopes []index.UserScope
	for k := range f.docs {
		// 解析 userID_scopeID
		var userID, scopeID string
		for i := 0; i < len(k); i++ {
			if k[i] == '_' {
				userID = k[:i]
				scopeID = k[i+1:]
				break
			}
		}
		if userID != "" && scopeID != "" {
			scopes = append(scopes, index.UserScope{UserID: userID, ScopeID: scopeID})
		}
	}
	return scopes, nil
}

// ──────────────────────────── IndexVersionMigrator 测试 ────────────────────────────

// TestIndexVersionMigrator_TryMigrate_空操作 测试空操作列表直接返回
func TestIndexVersionMigrator_TryMigrate_空操作(t *testing.T) {
	idx := newFakeMemoryIndex()
	m := NewIndexVersionMigrator()

	err := m.TryMigrate(context.Background(), idx, nil)
	if err != nil {
		t.Errorf("空操作列表应返回 nil, got %v", err)
	}
}

// TestIndexVersionMigrator_TryMigrate_重命名字段 测试字段重命名操作
func TestIndexVersionMigrator_TryMigrate_重命名字段(t *testing.T) {
	idx := newFakeMemoryIndex()
	idx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "doc1", Fields: map[string]any{"old_name": "value1", "other": "value2"}},
	})

	m := NewIndexVersionMigrator()
	ops := []operation.Operation{
		&operation.RenameMemoryDocFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			OldFieldName:  "old_name",
			NewFieldName:  "new_name",
		},
	}

	err := m.TryMigrate(context.Background(), idx, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	if idx.schemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", idx.schemaVersion)
	}

	docs, _ := idx.ListMemories(context.Background(), "user1", "scope1", 0, 100, nil)
	if len(docs) != 1 {
		t.Fatalf("应有 1 个文档, 实际 %d", len(docs))
	}

	if _, exists := docs[0].Fields["old_name"]; exists {
		t.Error("old_name 不应存在")
	}
	if docs[0].Fields["new_name"] != "value1" {
		t.Errorf("new_name = %v, want value1", docs[0].Fields["new_name"])
	}
}

// TestIndexVersionMigrator_TryMigrate_变换字段 测试字段值变换操作
func TestIndexVersionMigrator_TryMigrate_变换字段(t *testing.T) {
	idx := newFakeMemoryIndex()
	idx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "doc1", Fields: map[string]any{"count": 5}},
	})

	m := NewIndexVersionMigrator()
	ops := []operation.Operation{
		&operation.TransformMemoryDocFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			FieldName:     "count",
			TransformFunc: func(v any) any {
				if i, ok := v.(int); ok {
					return i * 2
				}
				return v
			},
		},
	}

	err := m.TryMigrate(context.Background(), idx, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	docs, _ := idx.ListMemories(context.Background(), "user1", "scope1", 0, 100, nil)
	if docs[0].Fields["count"] != 10 {
		t.Errorf("count = %v, want 10", docs[0].Fields["count"])
	}
}

// TestIndexVersionMigrator_TryMigrate_添加字段 测试添加字段操作
func TestIndexVersionMigrator_TryMigrate_添加字段(t *testing.T) {
	idx := newFakeMemoryIndex()
	idx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "doc1", Fields: map[string]any{"existing": "val"}},
	})

	m := NewIndexVersionMigrator()
	ops := []operation.Operation{
		&operation.AddMemoryDocFieldOperation{
			BaseOperation:      operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			FieldName:          "new_field",
			DefaultValueOrFunc: "default_val",
		},
	}

	err := m.TryMigrate(context.Background(), idx, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	docs, _ := idx.ListMemories(context.Background(), "user1", "scope1", 0, 100, nil)
	if docs[0].Fields["new_field"] != "default_val" {
		t.Errorf("new_field = %v, want default_val", docs[0].Fields["new_field"])
	}
}

// TestIndexVersionMigrator_TryMigrate_添加字段_函数默认值 测试添加字段使用函数默认值
func TestIndexVersionMigrator_TryMigrate_添加字段_函数默认值(t *testing.T) {
	idx := newFakeMemoryIndex()
	idx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "doc1", Fields: map[string]any{}},
	})

	m := NewIndexVersionMigrator()
	ops := []operation.Operation{
		&operation.AddMemoryDocFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			FieldName:     "computed",
			DefaultValueOrFunc: func() any { return 42 },
		},
	}

	err := m.TryMigrate(context.Background(), idx, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	docs, _ := idx.ListMemories(context.Background(), "user1", "scope1", 0, 100, nil)
	if docs[0].Fields["computed"] != 42 {
		t.Errorf("computed = %v, want 42", docs[0].Fields["computed"])
	}
}

// TestIndexVersionMigrator_TryMigrate_删除字段 测试删除字段操作
func TestIndexVersionMigrator_TryMigrate_删除字段(t *testing.T) {
	idx := newFakeMemoryIndex()
	idx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "doc1", Fields: map[string]any{"to_remove": "val", "keep": "val2"}},
	})

	m := NewIndexVersionMigrator()
	ops := []operation.Operation{
		&operation.RemoveMemoryDocFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			FieldName:     "to_remove",
		},
	}

	err := m.TryMigrate(context.Background(), idx, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	docs, _ := idx.ListMemories(context.Background(), "user1", "scope1", 0, 100, nil)
	if _, exists := docs[0].Fields["to_remove"]; exists {
		t.Error("to_remove 应已被删除")
	}
	if docs[0].Fields["keep"] != "val2" {
		t.Errorf("keep = %v, want val2", docs[0].Fields["keep"])
	}
}

// TestIndexVersionMigrator_TryMigrate_版本已是最新 测试当前版本已是最新的情况
func TestIndexVersionMigrator_TryMigrate_版本已是最新(t *testing.T) {
	idx := newFakeMemoryIndex()
	idx.schemaVersion = 5

	m := NewIndexVersionMigrator()
	ops := []operation.Operation{
		&operation.RenameMemoryDocFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}},
			OldFieldName:  "old",
			NewFieldName:  "new",
		},
	}

	err := m.TryMigrate(context.Background(), idx, ops)
	if err != nil {
		t.Errorf("版本已是最新时应返回 nil, got %v", err)
	}
}

// TestIndexVersionMigrator_TryMigrate_不支持的操作类型 测试不支持的操作类型
func TestIndexVersionMigrator_TryMigrate_不支持的操作类型(t *testing.T) {
	idx := newFakeMemoryIndex()

	m := NewIndexVersionMigrator()
	ops := []operation.Operation{
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		},
	}

	err := m.TryMigrate(context.Background(), idx, ops)
	if err == nil {
		t.Error("不支持的操作类型应返回错误")
	}
}
