//go:build integration

package memory_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/ltm"
)

// ──────────────────────────── 测试套件 ────────────────────────────

// LongTermMemoryE2ESuite LongTermMemory E2E 集成测试套件
type LongTermMemoryE2ESuite struct {
	suite.Suite
	// ctx 测试上下文
	ctx context.Context
}

func TestLongTermMemoryE2ESuite(t *testing.T) {
	suite.Run(t, new(LongTermMemoryE2ESuite))
}

func (s *LongTermMemoryE2ESuite) SetupTest() {
	s.ctx = context.Background()
	// 每次测试前重置单例
	ltm.ResetLongTermMemory()
}

// ──────────────────────────── 创建和初始化 ────────────────────────────

// TestLTM_创建 测试零值创建
func (s *LongTermMemoryE2ESuite) TestLTM_创建() {
	m := ltm.NewLongTermMemory()
	s.NotNil(m)
	s.False(m.IsInitialized(), "零值创建后不应已初始化")
}

// TestLTM_单例 测试单例模式
func (s *LongTermMemoryE2ESuite) TestLTM_单例() {
	m1 := ltm.GetLongTermMemory()
	m2 := ltm.GetLongTermMemory()
	s.Equal(m1, m2, "GetLongTermMemory 应返回同一实例")
}

// TestLTM_RegisterStore_KVOnly 测试仅注册 KV 存储需要 dbStore
func (s *LongTermMemoryE2ESuite) TestLTM_RegisterStore_KVOnly() {
	m := ltm.NewLongTermMemory()
	kvStore := kv.NewInMemoryKVStore()

	err := m.RegisterStore(s.ctx, kvStore)
	s.Error(err, "仅注册 KV 应报错：需要 dbStore")
	s.Contains(err.Error(), "db store", "错误信息应提及 db store")
}

// TestLTM_RegisterStore_nilKV 测试 nil KV 报错
func (s *LongTermMemoryE2ESuite) TestLTM_RegisterStore_nilKV() {
	m := ltm.NewLongTermMemory()

	err := m.RegisterStore(s.ctx, nil)
	s.Error(err, "nil KV 应报错")
}

// TestLTM_KVStore 测试获取 KV 存储（未注册时为 nil）
func (s *LongTermMemoryE2ESuite) TestLTM_KVStore() {
	m := ltm.NewLongTermMemory()
	s.Nil(m.KVStore(), "未注册时 KVStore 应为 nil")
}

// ──────────────────────────── 未初始化时操作报错 ────────────────────────────

// TestLTM_SearchUserMem_未初始化 测试未初始化时搜索报错
func (s *LongTermMemoryE2ESuite) TestLTM_SearchUserMem_未初始化() {
	m := ltm.NewLongTermMemory()

	_, err := m.SearchUserMem(s.ctx, "query", 5)
	s.Error(err, "未初始化时搜索应报错")
}

// ──────────────────────────── 选项构造 ────────────────────────────

// TestLTM_AddMessagesOption 测试 AddMessages 选项
func (s *LongTermMemoryE2ESuite) TestLTM_AddMessagesOption() {
	opts := []ltm.AddMessagesOption{
		ltm.WithUserID("user-1"),
		ltm.WithScopeID("scope-1"),
		ltm.WithSessionID("sess-1"),
	}
	s.Len(opts, 3, "应成功创建 3 个选项")
}

// TestLTM_SearchOption 测试 Search 选项
func (s *LongTermMemoryE2ESuite) TestLTM_SearchOption() {
	opts := []ltm.SearchOption{
		ltm.SearchWithUserID("user-1"),
		ltm.SearchWithScopeID("scope-1"),
		ltm.SearchWithThreshold(0.5),
	}
	s.Len(opts, 3, "应成功创建 3 个搜索选项")
}

// TestLTM_UserScopeOption 测试 UserScope 选项
func (s *LongTermMemoryE2ESuite) TestLTM_UserScopeOption() {
	opts := []ltm.UserScopeOption{
		ltm.Uid("user-1"),
		ltm.Sid("scope-1"),
	}
	s.Len(opts, 2, "应成功创建 2 个 UserScope 选项")
}

// TestLTM_RegisterStoreOption 测试 RegisterStore 选项
func (s *LongTermMemoryE2ESuite) TestLTM_RegisterStoreOption() {
	opts := []ltm.RegisterStoreOption{
		ltm.WithVectorStore(nil),
		ltm.WithDbStore(nil),
		ltm.WithEmbeddingModel(nil),
		ltm.WithMessageStore(nil),
	}
	s.Len(opts, 4, "应成功创建 4 个 RegisterStore 选项")
}

// ──────────────────────────── InMemoryKVStore 验证 ────────────────────────────

// TestLTM_KVStoreSetGet 测试 KV 存储基本读写
func (s *LongTermMemoryE2ESuite) TestLTM_KVStoreSetGet() {
	store := kv.NewInMemoryKVStore()

	err := store.Set(s.ctx, "test_key", []byte("test_value"))
	s.NoError(err)

	val, err := store.Get(s.ctx, "test_key")
	s.NoError(err)
	s.Equal([]byte("test_value"), val)

	exists, err := store.Exists(s.ctx, "test_key")
	s.NoError(err)
	s.True(exists)
}

// TestLTM_KVStoreDelete 测试 KV 存储删除
func (s *LongTermMemoryE2ESuite) TestLTM_KVStoreDelete() {
	store := kv.NewInMemoryKVStore()

	store.Set(s.ctx, "key1", []byte("val1"))
	err := store.Delete(s.ctx, "key1")
	s.NoError(err)

	val, err := store.Get(s.ctx, "key1")
	s.NoError(err)
	s.Nil(val, "删除后获取应为 nil")
}

// TestLTM_KVStoreExclusiveSet 测试 KV 存储排他写入
func (s *LongTermMemoryE2ESuite) TestLTM_KVStoreExclusiveSet() {
	store := kv.NewInMemoryKVStore()

	ok, err := store.ExclusiveSet(s.ctx, "lock_key", []byte("lock_val"), 0)
	s.NoError(err)
	s.True(ok, "新 key 排他写入应成功")

	ok, err = store.ExclusiveSet(s.ctx, "lock_key", []byte("lock_val2"), 0)
	s.NoError(err)
	s.False(ok, "已存在 key 排他写入应失败")
}
