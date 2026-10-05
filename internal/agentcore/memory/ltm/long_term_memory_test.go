package ltm

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"gorm.io/gorm"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestIsInitialized_未初始化 所有依赖为 nil 时返回 false
func TestIsInitialized_未初始化(t *testing.T) {
	m := NewLongTermMemory()
	if m.IsInitialized() {
		t.Error("未初始化的 LTM IsInitialized 应返回 false")
	}
}

// TestIsInitialized_部分初始化 只有 kvStore 时返回 false
func TestIsInitialized_部分初始化(t *testing.T) {
	m := NewLongTermMemory()
	m.kvStore = &stubKVStore{}
	if m.IsInitialized() {
		t.Error("只有 kvStore 时 IsInitialized 应返回 false")
	}
}

// TestIsInitialized_kvStore和dbStore 设置 kvStore 和 dbStore 但缺少 searchManager 时返回 false
func TestIsInitialized_kvStore和dbStore(t *testing.T) {
	m := NewLongTermMemory()
	m.kvStore = &stubKVStore{}
	m.dbStore = &stubDbStore{}
	if m.IsInitialized() {
		t.Error("缺少 searchManager 时 IsInitialized 应返回 false")
	}
}

// TestKVStore_未注册 返回 nil
func TestKVStore_未注册(t *testing.T) {
	m := NewLongTermMemory()
	if m.KVStore() != nil {
		t.Error("未注册时 KVStore 应返回 nil")
	}
}

// TestKVStore_已注册 返回已注册的实例
func TestKVStore_已注册(t *testing.T) {
	m := NewLongTermMemory()
	var store kv.BaseKVStore = &stubKVStore{}
	m.kvStore = store
	got := m.KVStore()
	if got != store {
		t.Error("已注册时 KVStore 应返回已注册的实例")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stubKVStore 用于测试的最小 stub KV 存储
type stubKVStore struct{}

func (s *stubKVStore) Set(_ context.Context, _ string, _ []byte) error { return nil }
func (s *stubKVStore) ExclusiveSet(_ context.Context, _ string, _ []byte, _ int) (bool, error) {
	return true, nil
}
func (s *stubKVStore) Get(_ context.Context, _ string) ([]byte, error)  { return nil, nil }
func (s *stubKVStore) Exists(_ context.Context, _ string) (bool, error) { return false, nil }
func (s *stubKVStore) Delete(_ context.Context, _ string) error         { return nil }
func (s *stubKVStore) GetByPrefix(_ context.Context, _ string) (map[string][]byte, error) {
	return nil, nil
}
func (s *stubKVStore) DeleteByPrefix(_ context.Context, _ string, _ int) error       { return nil }
func (s *stubKVStore) MGet(_ context.Context, _ []string) ([][]byte, error)          { return nil, nil }
func (s *stubKVStore) BatchDelete(_ context.Context, _ []string, _ int) (int, error) { return 0, nil }
func (s *stubKVStore) Pipeline(_ context.Context) kv.KVPipeline                      { return nil }

// stubDbStore 用于测试的最小 stub DB 存储
type stubDbStore struct{}

func (s *stubDbStore) GetDB(_ context.Context) *gorm.DB { return nil }
