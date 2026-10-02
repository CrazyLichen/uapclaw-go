package migrator

import (
	"context"
	"testing"

	vector "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
)

// ──────────────────────────── 辅助结构体 ────────────────────────────

// fakeVectorStoreForMigrator 测试用假向量存储
type fakeVectorStoreForMigrator struct {
	// collections 集合名列表
	collections []string
	// metadata 集合名 → 元数据
	metadata map[string]map[string]any
	// updateSchemaCalls UpdateSchema 调用记录
	updateSchemaCalls []updateSchemaCall
}

type updateSchemaCall struct {
	collectionName string
	operations     []operation.Operation
}

func newFakeVectorStoreForMigrator() *fakeVectorStoreForMigrator {
	return &fakeVectorStoreForMigrator{
		metadata: make(map[string]map[string]any),
	}
}

func (s *fakeVectorStoreForMigrator) CreateCollection(_ context.Context, _ string, _ *vector.CollectionSchema, _ ...vector.Option) error {
	return nil
}

func (s *fakeVectorStoreForMigrator) DeleteCollection(_ context.Context, _ string, _ ...vector.Option) error {
	return nil
}

func (s *fakeVectorStoreForMigrator) GetSchema(_ context.Context, _ string, _ ...vector.Option) (*vector.CollectionSchema, error) {
	return nil, nil
}

func (s *fakeVectorStoreForMigrator) CollectionExists(_ context.Context, _ string, _ ...vector.Option) (bool, error) {
	return true, nil
}

func (s *fakeVectorStoreForMigrator) Search(_ context.Context, _ string, _ []float64, _ string, _ int, _ map[string]any, _ ...vector.Option) ([]vector.VectorSearchResult, error) {
	return nil, nil
}

func (s *fakeVectorStoreForMigrator) DeleteDocsByIDs(_ context.Context, _ string, _ []string, _ ...vector.Option) error {
	return nil
}

func (s *fakeVectorStoreForMigrator) GetDocsByFilters(_ context.Context, _ string, _ map[string]any, _ int, _ ...vector.Option) ([]map[string]any, error) {
	return nil, nil
}

func (s *fakeVectorStoreForMigrator) AddDocs(_ context.Context, _ string, _ []map[string]any, _ ...vector.Option) error {
	return nil
}

func (s *fakeVectorStoreForMigrator) UpdateDocsByFilters(_ context.Context, _ string, _ map[string]any, _ map[string]any, _ ...vector.Option) error {
	return nil
}

func (s *fakeVectorStoreForMigrator) DeleteDocsByFilters(_ context.Context, _ string, _ map[string]any, _ ...vector.Option) error {
	return nil
}

func (s *fakeVectorStoreForMigrator) ListCollectionNames(_ context.Context) ([]string, error) {
	return s.collections, nil
}

func (s *fakeVectorStoreForMigrator) UpdateSchema(_ context.Context, collectionName string, operations []operation.Operation, _ ...vector.Option) error {
	s.updateSchemaCalls = append(s.updateSchemaCalls, updateSchemaCall{
		collectionName: collectionName,
		operations:     operations,
	})
	return nil
}

func (s *fakeVectorStoreForMigrator) GetCollectionMetadata(_ context.Context, collectionName string, _ ...vector.Option) (map[string]any, error) {
	if m, ok := s.metadata[collectionName]; ok {
		return m, nil
	}
	return map[string]any{}, nil
}

func (s *fakeVectorStoreForMigrator) UpdateCollectionMetadata(_ context.Context, collectionName string, metadata map[string]any, _ ...vector.Option) error {
	s.metadata[collectionName] = metadata
	return nil
}

func (s *fakeVectorStoreForMigrator) RenameCollection(_ context.Context, _, _ string, _ ...vector.Option) error {
	return nil
}

// ──────────────────────────── VectorMigrator.TryMigrate 测试 ────────────────────────────

// TestVectorMigrator_TryMigrate_发现集合并迁移 测试发现集合并执行迁移
func TestVectorMigrator_TryMigrate_发现集合并迁移(t *testing.T) {
	store := newFakeVectorStoreForMigrator()
	store.collections = []string{"user1_scope1_summary", "user2_scope2_summary", "user1_scope1_user_profile"}
	store.metadata["user1_scope1_summary"] = map[string]any{"schema_version": 0}
	store.metadata["user2_scope2_summary"] = map[string]any{"schema_version": 0}

	supportedTypes := []string{"user_profile", "summary"}
	m := NewVectorMigrator(store, supportedTypes)

	ops := []operation.Operation{
		&operation.AddScalarFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			FieldName:     "new_field",
			FieldType:     "string",
		},
	}

	err := m.TryMigrate(context.Background(), "vector_summary", ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 验证只迁移了 _summary 后缀的集合
	if len(store.updateSchemaCalls) != 2 {
		t.Errorf("应有 2 次 UpdateSchema 调用, 实际 %d", len(store.updateSchemaCalls))
	}

	// 验证 metadata 已更新
	if m, ok := store.metadata["user1_scope1_summary"]["schema_version"].(int); !ok || m != 1 {
		t.Errorf("user1_scope1_summary schema_version 应为 1, 实际 %v", store.metadata["user1_scope1_summary"]["schema_version"])
	}
}

// TestVectorMigrator_TryMigrate_不支持的MemoryType 测试不支持的 memory type
func TestVectorMigrator_TryMigrate_不支持的MemoryType(t *testing.T) {
	store := newFakeVectorStoreForMigrator()
	supportedTypes := []string{"user_profile", "summary"}
	m := NewVectorMigrator(store, supportedTypes)

	ops := []operation.Operation{
		&operation.AddScalarFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		},
	}

	err := m.TryMigrate(context.Background(), "vector_unknown", ops)
	if err == nil {
		t.Error("不支持的 memory type 应返回错误")
	}
}

// TestVectorMigrator_TryMigrate_部分操作待执行 测试只执行高版本操作
func TestVectorMigrator_TryMigrate_部分操作待执行(t *testing.T) {
	store := newFakeVectorStoreForMigrator()
	store.collections = []string{"user1_scope1_summary"}
	store.metadata["user1_scope1_summary"] = map[string]any{"schema_version": 1}

	supportedTypes := []string{"summary"}
	m := NewVectorMigrator(store, supportedTypes)

	ops := []operation.Operation{
		&operation.AddScalarFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			FieldName:     "field_v1",
		},
		&operation.AddScalarFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			FieldName:     "field_v2",
		},
	}

	err := m.TryMigrate(context.Background(), "vector_summary", ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 验证只应用了 v2 操作
	if len(store.updateSchemaCalls) != 1 {
		t.Fatalf("应有 1 次 UpdateSchema 调用, 实际 %d", len(store.updateSchemaCalls))
	}
	if len(store.updateSchemaCalls[0].operations) != 1 {
		t.Errorf("应只应用 1 个操作, 实际 %d", len(store.updateSchemaCalls[0].operations))
	}
	if store.updateSchemaCalls[0].operations[0].SchemaVersion() != 2 {
		t.Errorf("应应用 v2 操作, 实际 v%d", store.updateSchemaCalls[0].operations[0].SchemaVersion())
	}
}

// TestVectorMigrator_TryMigrate_版本已是最新 测试集合版本已是最新的情况
func TestVectorMigrator_TryMigrate_版本已是最新(t *testing.T) {
	store := newFakeVectorStoreForMigrator()
	store.collections = []string{"user1_scope1_summary"}
	store.metadata["user1_scope1_summary"] = map[string]any{"schema_version": 5}

	supportedTypes := []string{"summary"}
	m := NewVectorMigrator(store, supportedTypes)

	ops := []operation.Operation{
		&operation.AddScalarFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}},
			FieldName:     "field_v3",
		},
	}

	err := m.TryMigrate(context.Background(), "vector_summary", ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 不应有 UpdateSchema 调用
	if len(store.updateSchemaCalls) != 0 {
		t.Errorf("版本已是最新时不应调用 UpdateSchema, 实际调用 %d 次", len(store.updateSchemaCalls))
	}
}

// TestVectorMigrator_TryMigrate_无匹配集合 测试没有匹配的集合
func TestVectorMigrator_TryMigrate_无匹配集合(t *testing.T) {
	store := newFakeVectorStoreForMigrator()
	store.collections = []string{"user1_scope1_user_profile"}

	supportedTypes := []string{"summary"}
	m := NewVectorMigrator(store, supportedTypes)

	ops := []operation.Operation{
		&operation.AddScalarFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		},
	}

	err := m.TryMigrate(context.Background(), "vector_summary", ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 不应有 UpdateSchema 调用
	if len(store.updateSchemaCalls) != 0 {
		t.Errorf("无匹配集合时不应调用 UpdateSchema, 实际调用 %d 次", len(store.updateSchemaCalls))
	}
}

// TestVectorMigrator_FindCollections_去掉前缀 测试 entity_key 去掉 vector_ 前缀
func TestVectorMigrator_FindCollections_去掉前缀(t *testing.T) {
	store := newFakeVectorStoreForMigrator()
	store.collections = []string{"user1_scope1_summary"}

	supportedTypes := []string{"summary"}
	m := NewVectorMigrator(store, supportedTypes)

	collections, err := m.findCollections(context.Background(), "vector_summary")
	if err != nil {
		t.Fatalf("findCollections 失败: %v", err)
	}

	if len(collections) != 1 || collections[0] != "user1_scope1_summary" {
		t.Errorf("期望 [user1_scope1_summary], 实际 %v", collections)
	}
}
