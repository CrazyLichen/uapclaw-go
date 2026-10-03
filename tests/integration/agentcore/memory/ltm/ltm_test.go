//go:build integration

package ltm_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/ltm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LongTermMemorySuite 测试 LongTermMemory 核心 API。
//
// 覆盖：
//   - NewLongTermMemory 零值创建
//   - IsInitialized 状态检查
//   - RegisterStore 仅 KVStore（dbStore 缺失导致 SetConfig 失败）
//   - SetConfig 前置条件校验
//   - AddMessages 未初始化时返回错误
//   - GetVariables 未初始化时返回错误
//
// 对应 Python: openjiuwen/core/memory/long_term_memory.py
type LongTermMemorySuite struct {
	isuite.MemorySuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestLongTermMemorySuite(t *testing.T) {
	suite.Run(t, new(LongTermMemorySuite))
}

// TestLTM_NewLongTermMemory_零值创建 测试零值创建不 panic。
func (s *LongTermMemorySuite) TestLTM_NewLongTermMemory_零值创建() {
	m := ltm.NewLongTermMemory()
	s.NotNil(m, "NewLongTermMemory 不应返回 nil")
	s.False(m.IsInitialized(), "零值实例不应标记为已初始化")
	s.Nil(m.KVStore(), "零值实例 KVStore 应为 nil")
}

// TestLTM_IsInitialized_未注册存储 测试未注册存储时 IsInitialized 返回 false。
func (s *LongTermMemorySuite) TestLTM_IsInitialized_未注册存储() {
	m := ltm.NewLongTermMemory()
	s.False(m.IsInitialized(), "未注册存储时不应标记为已初始化")
}

// TestLTM_RegisterStore_KVStore成功_无DbStore 测试仅注册 KVStore 时 RegisterStore 返回错误
// （因为 SetConfig 需要 dbStore 和 memoryIndex）。
func (s *LongTermMemorySuite) TestLTM_RegisterStore_KVStore成功_无DbStore() {
	ctx := s.Ctx
	m := ltm.NewLongTermMemory()
	kvStore := kv.NewInMemoryKVStore()

	// 仅注册 KVStore，不提供 dbStore 和 vectorStore
	err := m.RegisterStore(ctx, kvStore)
	s.Error(err, "仅 KVStore 时 SetConfig 应失败（缺少 dbStore 和 memoryIndex）")

	// 但 KVStore 应已被赋值
	s.Equal(kvStore, m.KVStore(), "KVStore 应已被赋值")
}

// TestLTM_SetConfig_前置条件校验 测试 SetConfig 在未注册必需存储时返回错误。
func (s *LongTermMemorySuite) TestLTM_SetConfig_前置条件校验() {
	m := ltm.NewLongTermMemory()

	// 未注册 kvStore/dbStore
	err := m.SetConfig(config.DefaultMemoryEngineConfig())
	s.Error(err, "未注册存储时 SetConfig 应返回错误")

	// 仅注册 kvStore（无 dbStore）
	m2 := ltm.NewLongTermMemory()
	m2.RegisterStore(s.Ctx, kv.NewInMemoryKVStore()) // 会报错但不影响后续赋值
	// 手动赋值 kvStore 模拟注册后缺少 dbStore
	m2.RegisterStore(s.Ctx, kv.NewInMemoryKVStore())
	err = m2.SetConfig(config.DefaultMemoryEngineConfig())
	s.Error(err, "仅有 kvStore 无 dbStore 时 SetConfig 应返回错误")
}

// TestLTM_AddMessages_未初始化 测试未初始化 writeManager 时 AddMessages 返回错误或 panic。
// 注意：当前 AddMessages 在未初始化的 LTM 上会 panic（缺少 storageCodec 守卫），
// 此测试用 recover 捕获并验证 panic 发生。
func (s *LongTermMemorySuite) TestLTM_AddMessages_未初始化() {
	m := ltm.NewLongTermMemory()

	// 未初始化任何存储 — AddMessages 内部访问 nil storageCodec 会 panic
	defer func() {
		r := recover()
		s.NotNil(r, "未初始化时 AddMessages 应 panic 或返回错误")
	}()

	_, _ = m.AddMessages(s.Ctx, nil, nil, ltm.WithScopeID("s1"), ltm.WithUserID("u1"))
}

// TestLTM_GetVariables_未初始化 测试未初始化 searchManager 时 GetVariables 返回错误。
func (s *LongTermMemorySuite) TestLTM_GetVariables_未初始化() {
	ctx := s.Ctx
	m := ltm.NewLongTermMemory()

	// 未初始化任何存储
	_, err := m.GetVariables(ctx, []string{"var1"}, ltm.Uid("u1"), ltm.Sid("s1"))
	s.Error(err, "未初始化时 GetVariables 应返回错误")
}
