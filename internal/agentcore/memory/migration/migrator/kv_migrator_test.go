package migrator

import (
	"context"
	"fmt"
	"testing"

	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
)

// ──────────────────────────── 辅助结构体 ────────────────────────────

// fakeKVStore 内存 KV 存储实现，用于测试
type fakeKVStore struct {
	data map[string][]byte
}

func newFakeKVStore() *fakeKVStore {
	return &fakeKVStore{data: make(map[string][]byte)}
}

func (s *fakeKVStore) Set(_ context.Context, key string, value []byte) error {
	s.data[key] = value
	return nil
}

func (s *fakeKVStore) ExclusiveSet(_ context.Context, key string, value []byte, _ int) (bool, error) {
	if _, exists := s.data[key]; exists {
		return false, nil
	}
	s.data[key] = value
	return true, nil
}

func (s *fakeKVStore) Get(_ context.Context, key string) ([]byte, error) {
	return s.data[key], nil
}

func (s *fakeKVStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := s.data[key]
	return ok, nil
}

func (s *fakeKVStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func (s *fakeKVStore) GetByPrefix(_ context.Context, prefix string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for k, v := range s.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			result[k] = v
		}
	}
	return result, nil
}

func (s *fakeKVStore) DeleteByPrefix(_ context.Context, prefix string, _ int) error {
	for k := range s.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(s.data, k)
		}
	}
	return nil
}

func (s *fakeKVStore) MGet(_ context.Context, keys []string) ([][]byte, error) {
	result := make([][]byte, len(keys))
	for i, key := range keys {
		result[i] = s.data[key]
	}
	return result, nil
}

func (s *fakeKVStore) BatchDelete(_ context.Context, keys []string, _ int) (int, error) {
	count := 0
	for _, key := range keys {
		if _, exists := s.data[key]; exists {
			delete(s.data, key)
			count++
		}
	}
	return count, nil
}

func (s *fakeKVStore) Pipeline(_ context.Context) kv.KVPipeline {
	return nil
}

// ──────────────────────────── validateOperationsOrder 测试 ────────────────────────────

// TestValidateOperationsOrder_升序 测试升序操作列表
func TestValidateOperationsOrder_升序(t *testing.T) {
	ops := []operation.Operation{
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}}},
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}}},
	}
	if !validateOperationsOrder(ops) {
		t.Error("升序操作列表应通过校验")
	}
}

// TestValidateOperationsOrder_非升序 测试非升序操作列表
func TestValidateOperationsOrder_非升序(t *testing.T) {
	ops := []operation.Operation{
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}}},
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
	}
	if validateOperationsOrder(ops) {
		t.Error("非升序操作列表应校验失败")
	}
}

// TestValidateOperationsOrder_相同版本 测试相同版本
func TestValidateOperationsOrder_相同版本(t *testing.T) {
	ops := []operation.Operation{
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
	}
	if validateOperationsOrder(ops) {
		t.Error("相同版本操作应校验失败")
	}
}

// TestValidateOperationsOrder_单操作 测试单操作列表
func TestValidateOperationsOrder_单操作(t *testing.T) {
	ops := []operation.Operation{
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
	}
	if !validateOperationsOrder(ops) {
		t.Error("单操作列表应通过校验")
	}
}

// ──────────────────────────── KVMigrator.TryMigrate 测试 ────────────────────────────

// TestKVMigrator_TryMigrate_空操作 测试空操作列表直接返回
func TestKVMigrator_TryMigrate_空操作(t *testing.T) {
	store := newFakeKVStore()
	registry := operation.NewOperationRegistry()
	m := NewKVMigrator(store, registry)

	err := m.TryMigrate(context.Background(), KVEntityKey, nil)
	if err != nil {
		t.Errorf("空操作列表应返回 nil, got %v", err)
	}
}

// TestKVMigrator_TryMigrate_错误EntityKey 测试错误的 entityKey
func TestKVMigrator_TryMigrate_错误EntityKey(t *testing.T) {
	store := newFakeKVStore()
	registry := operation.NewOperationRegistry()
	m := NewKVMigrator(store, registry)

	ops := []operation.Operation{
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
	}
	err := m.TryMigrate(context.Background(), "wrong_key", ops)
	if err == nil {
		t.Error("错误的 entityKey 应返回错误")
	}
}

// TestKVMigrator_TryMigrate_新KVStore初始化版本 测试新 KV Store 自动设置初始版本
func TestKVMigrator_TryMigrate_新KVStore初始化版本(t *testing.T) {
	store := newFakeKVStore()
	registry := operation.NewOperationRegistry()

	// 注册一个操作，使得 registry 有 current_version=1
	registry.Register(KVEntityKey, &operation.UpdateKVOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
	})

	// 传入与 registry 当前版本相同的操作，使当前版本 >= 最新操作版本
	ops := []operation.Operation{
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		},
	}

	m := NewKVMigrator(store, registry)
	err := m.TryMigrate(context.Background(), KVEntityKey, ops)
	if err != nil {
		t.Errorf("应返回 nil, got %v", err)
	}

	// 验证版本已写入（getCurrentVersion 中检测到空 store + 无 memory data → 设置初始版本）
	versionBytes, _ := store.Get(context.Background(), KVSchemaVersionKey)
	if string(versionBytes) != "1" {
		t.Errorf("初始版本应为 1, got %s", string(versionBytes))
	}
}

// TestKVMigrator_TryMigrate_执行操作 测试执行 KV 迁移操作
func TestKVMigrator_TryMigrate_执行操作(t *testing.T) {
	store := newFakeKVStore()
	registry := operation.NewOperationRegistry()

	// 添加一些数据
	store.Set(context.Background(), "prefix_key1", []byte("value1"))

	// 注册 prefix
	common.KVPrefixRegistry.RegisterCurrent("prefix_")

	ops := []operation.Operation{
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			UpdateFunc: func(ctx context.Context, kvStore kv.BaseKVStore) error {
				return kvStore.Set(ctx, "prefix_key1", []byte("migrated_value"))
			},
		},
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			UpdateFunc: func(ctx context.Context, kvStore kv.BaseKVStore) error {
				return kvStore.Set(ctx, "prefix_key2", []byte("new_key"))
			},
		},
	}

	m := NewKVMigrator(store, registry)
	err := m.TryMigrate(context.Background(), KVEntityKey, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 验证数据已更新
	val, _ := store.Get(context.Background(), "prefix_key1")
	if string(val) != "migrated_value" {
		t.Errorf("prefix_key1 = %s, want migrated_value", string(val))
	}

	val, _ = store.Get(context.Background(), "prefix_key2")
	if string(val) != "new_key" {
		t.Errorf("prefix_key2 = %s, want new_key", string(val))
	}

	// 验证版本已更新
	versionBytes, _ := store.Get(context.Background(), KVSchemaVersionKey)
	if string(versionBytes) != "2" {
		t.Errorf("版本应为 2, got %s", string(versionBytes))
	}

	// 清理
	common.KVPrefixRegistry.Unregister("prefix_")
}

// TestKVMigrator_TryMigrate_操作失败恢复备份 测试操作失败时从备份恢复
func TestKVMigrator_TryMigrate_操作失败恢复备份(t *testing.T) {
	store := newFakeKVStore()
	registry := operation.NewOperationRegistry()

	// 添加初始数据
	store.Set(context.Background(), "prefix_original", []byte("original_value"))
	common.KVPrefixRegistry.RegisterCurrent("prefix_")

	ops := []operation.Operation{
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			UpdateFunc: func(ctx context.Context, kvStore kv.BaseKVStore) error {
				return fmt.Errorf("模拟操作失败")
			},
		},
	}

	m := NewKVMigrator(store, registry)
	err := m.TryMigrate(context.Background(), KVEntityKey, ops)
	if err == nil {
		t.Error("操作失败应返回错误")
	}

	// 验证数据已恢复
	val, _ := store.Get(context.Background(), "prefix_original")
	if string(val) != "original_value" {
		t.Errorf("恢复后 prefix_original = %s, want original_value", string(val))
	}

	// 清理
	common.KVPrefixRegistry.Unregister("prefix_")
}

// TestKVMigrator_TryMigrate_版本已是最新 测试当前版本已是最新的情况
func TestKVMigrator_TryMigrate_版本已是最新(t *testing.T) {
	store := newFakeKVStore()
	registry := operation.NewOperationRegistry()

	// 设置当前版本为 5
	store.Set(context.Background(), KVSchemaVersionKey, []byte("5"))

	ops := []operation.Operation{
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}},
		},
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 4}},
		},
	}

	m := NewKVMigrator(store, registry)
	err := m.TryMigrate(context.Background(), KVEntityKey, ops)
	if err != nil {
		t.Errorf("版本已是最新时应返回 nil, got %v", err)
	}
}

// TestKVMigrator_TryMigrate_部分操作待执行 测试只执行版本号大于当前版本的操作
func TestKVMigrator_TryMigrate_部分操作待执行(t *testing.T) {
	store := newFakeKVStore()
	registry := operation.NewOperationRegistry()

	// 设置当前版本为 2
	store.Set(context.Background(), KVSchemaVersionKey, []byte("2"))

	executed := make(map[int]bool)

	ops := []operation.Operation{
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			UpdateFunc: func(ctx context.Context, kvStore kv.BaseKVStore) error {
				executed[1] = true
				return nil
			},
		},
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			UpdateFunc: func(ctx context.Context, kvStore kv.BaseKVStore) error {
				executed[2] = true
				return nil
			},
		},
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}},
			UpdateFunc: func(ctx context.Context, kvStore kv.BaseKVStore) error {
				executed[3] = true
				return nil
			},
		},
	}

	m := NewKVMigrator(store, registry)
	err := m.TryMigrate(context.Background(), KVEntityKey, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 只有 schema_version > 2 的操作才应执行
	if executed[1] {
		t.Error("schema_version=1 的操作不应执行")
	}
	if executed[2] {
		t.Error("schema_version=2 的操作不应执行")
	}
	if !executed[3] {
		t.Error("schema_version=3 的操作应执行")
	}

	// 验证版本已更新为 3
	versionBytes, _ := store.Get(context.Background(), KVSchemaVersionKey)
	if string(versionBytes) != "3" {
		t.Errorf("版本应为 3, got %s", string(versionBytes))
	}
}

// TestKVMigrator_TryMigrate_不支持的操作类型 测试不支持的操作类型
func TestKVMigrator_TryMigrate_不支持的操作类型(t *testing.T) {
	store := newFakeKVStore()
	registry := operation.NewOperationRegistry()

	// 添加数据使有 memory module data
	store.Set(context.Background(), "test_key", []byte("test_value"))
	common.KVPrefixRegistry.RegisterCurrent("test_")

	ops := []operation.Operation{
		&operation.AddColumnOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		},
	}

	m := NewKVMigrator(store, registry)
	err := m.TryMigrate(context.Background(), KVEntityKey, ops)
	if err == nil {
		t.Error("不支持的操作类型应返回错误")
	}

	// 清理
	common.KVPrefixRegistry.Unregister("test_")
}

// ──────────────────────────── filterPendingOperations 测试 ────────────────────────────

// TestFilterPendingOperations_NilCurrent 测试 currentVersion 为 nil 时返回全部操作
func TestFilterPendingOperations_NilCurrent(t *testing.T) {
	ops := []operation.Operation{
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}}},
	}
	result := filterPendingOperations(ops, nil)
	if len(result) != 2 {
		t.Errorf("currentVersion 为 nil 时应返回全部操作, got %d", len(result))
	}
}

// TestFilterPendingOperations_过滤低版本 测试过滤掉低版本操作
func TestFilterPendingOperations_过滤低版本(t *testing.T) {
	v := 2
	ops := []operation.Operation{
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}}},
		&operation.UpdateKVOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}}},
	}
	result := filterPendingOperations(ops, &v)
	if len(result) != 1 {
		t.Errorf("应只有 1 个待执行操作, got %d", len(result))
	}
	if result[0].SchemaVersion() != 3 {
		t.Errorf("待执行操作版本应为 3, got %d", result[0].SchemaVersion())
	}
}
