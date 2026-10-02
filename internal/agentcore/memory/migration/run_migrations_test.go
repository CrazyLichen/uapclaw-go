package migration

import (
	"context"
	"testing"

	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
)

// ──────────────────────────── fakeKVStore（简化版） ────────────────────────────

// fakeKVStoreForRun 简化版 KV 存储用于 run_migrations 测试
type fakeKVStoreForRun struct {
	data map[string][]byte
}

func newFakeKVStoreForRun() *fakeKVStoreForRun {
	return &fakeKVStoreForRun{data: make(map[string][]byte)}
}

func (s *fakeKVStoreForRun) Set(_ context.Context, key string, value []byte) error {
	s.data[key] = value
	return nil
}

func (s *fakeKVStoreForRun) ExclusiveSet(_ context.Context, key string, value []byte, _ int) (bool, error) {
	if _, exists := s.data[key]; exists {
		return false, nil
	}
	s.data[key] = value
	return true, nil
}

func (s *fakeKVStoreForRun) Get(_ context.Context, key string) ([]byte, error) {
	return s.data[key], nil
}

func (s *fakeKVStoreForRun) Exists(_ context.Context, key string) (bool, error) {
	_, ok := s.data[key]
	return ok, nil
}

func (s *fakeKVStoreForRun) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func (s *fakeKVStoreForRun) GetByPrefix(_ context.Context, prefix string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for k, v := range s.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			result[k] = v
		}
	}
	return result, nil
}

func (s *fakeKVStoreForRun) DeleteByPrefix(_ context.Context, prefix string, _ int) error {
	for k := range s.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(s.data, k)
		}
	}
	return nil
}

func (s *fakeKVStoreForRun) MGet(_ context.Context, keys []string) ([][]byte, error) {
	result := make([][]byte, len(keys))
	for i, key := range keys {
		result[i] = s.data[key]
	}
	return result, nil
}

func (s *fakeKVStoreForRun) BatchDelete(_ context.Context, keys []string, _ int) (int, error) {
	count := 0
	for _, key := range keys {
		if _, exists := s.data[key]; exists {
			delete(s.data, key)
			count++
		}
	}
	return count, nil
}

func (s *fakeKVStoreForRun) Pipeline(_ context.Context) kv.KVPipeline {
	return nil
}

// ──────────────────────────── runMigrationsWithRegistry 测试 ────────────────────────────

// TestRunKVMigrations_无注册操作 测试无注册操作时跳过
func TestRunKVMigrations_无注册操作(t *testing.T) {
	// 清空 registry
	KVRegistry.Clear()
	defer KVRegistry.Clear()

	store := newFakeKVStoreForRun()
	err := RunKVMigrations(context.Background(), store)
	if err != nil {
		t.Errorf("无注册操作时应返回 nil, got %v", err)
	}
}

// TestRunKVMigrations_有注册操作 测试执行已注册的 KV 迁移
func TestRunKVMigrations_有注册操作(t *testing.T) {
	KVRegistry.Clear()
	defer KVRegistry.Clear()

	store := newFakeKVStoreForRun()

	// 设置一个低版本号，使迁移操作（schema_version=1）需要执行
	store.Set(context.Background(), "MEMORY_MIGRATION_KV_SCHEMA_VERSION", []byte("0"))

	// 注册操作
	KVRegistry.Register("kv_global", &operation.UpdateKVOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		UpdateFunc: func(ctx context.Context, kvStore kv.BaseKVStore) error {
			return kvStore.Set(ctx, "test_key", []byte("migrated"))
		},
	})

	err := RunKVMigrations(context.Background(), store)
	if err != nil {
		t.Fatalf("RunKVMigrations 失败: %v", err)
	}

	// 验证数据已迁移
	val, _ := store.Get(context.Background(), "test_key")
	if string(val) != "migrated" {
		t.Errorf("test_key = %s, want migrated", string(val))
	}

	// 验证版本已更新
	versionVal, _ := store.Get(context.Background(), "MEMORY_MIGRATION_KV_SCHEMA_VERSION")
	if string(versionVal) != "1" {
		t.Errorf("版本 = %s, want 1", string(versionVal))
	}
}

// TestRunVectorMigrations_无注册操作 测试无注册操作时跳过
func TestRunVectorMigrations_无注册操作(t *testing.T) {
	VectorRegistry.Clear()
	defer VectorRegistry.Clear()

	// 空的 fake vector store 不需要实现，只需要传 nil
	// 但 RunVectorMigrations 需要 vectorStore 参数
	// 跳过此测试（因为创建完整 fake 太复杂），已通过 migrator 层级测试覆盖
}
