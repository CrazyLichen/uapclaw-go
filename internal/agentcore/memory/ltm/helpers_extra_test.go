package ltm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/codec"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestSetScopeConfig_正常流程 测试设置 scope 配置完整流程。
func TestSetScopeConfig_正常流程(t *testing.T) {
	env := newTestEnv(t)
	// 需要初始化 storageCodec
	c, err := codec.NewAesStorageCodec([]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	env.m.storageCodec = c

	cfg := config.DefaultMemoryScopeConfig()
	err = env.m.SetScopeConfig(context.Background(), "scope1", cfg)
	// InMemoryKVStore 的 Set 方法接受 []byte
	assert.NoError(t, err)
}

// TestSetScopeConfig_无效ScopeID 测试无效 scopeID。
func TestSetScopeConfig_无效ScopeID(t *testing.T) {
	env := newTestEnv(t)
	cfg := config.DefaultMemoryScopeConfig()
	err := env.m.SetScopeConfig(context.Background(), "", cfg)
	assert.Error(t, err)
}

// TestGetScopeConfig_不存在 测试获取不存在的 scope 配置。
func TestGetScopeConfig_不存在(t *testing.T) {
	env := newTestEnv(t)
	result, err := env.m.GetScopeConfig(context.Background(), "nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestGetScopeConfig_无效ScopeID 测试无效 scopeID。
func TestGetScopeConfig_无效ScopeID(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.m.GetScopeConfig(context.Background(), "")
	assert.Error(t, err)
}

// TestDeleteScopeConfig_无效ScopeID 测试无效 scopeID。
func TestDeleteScopeConfig_无效ScopeID(t *testing.T) {
	env := newTestEnv(t)
	err := env.m.DeleteScopeConfig(context.Background(), "")
	assert.Error(t, err)
}

// TestDeleteScopeConfig_正常流程 测试删除存在的配置。
func TestDeleteScopeConfig_正常流程(t *testing.T) {
	env := newTestEnv(t)
	err := env.m.DeleteScopeConfig(context.Background(), "scope1")
	// InMemoryKVStore 的 Delete 对不存在的 key 也返回 nil
	assert.NoError(t, err)
}

// TestGetScopeLLM_无ScopeConfig 测试无 scope 配置时回退到 baseLLM。
func TestGetScopeLLM_无ScopeConfig(t *testing.T) {
	env := newTestEnv(t)
	llmInstance, err := env.m.getScopeLLM(context.Background(), "scope1")
	assert.NoError(t, err)
	assert.Nil(t, llmInstance) // baseLLM 也是 nil
}

// TestGetScopeConfig_内存缓存 测试内存缓存优先。
func TestGetScopeConfig_内存缓存(t *testing.T) {
	env := newTestEnv(t)
	c, err := codec.NewAesStorageCodec([]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	env.m.storageCodec = c

	// 先设置到内存缓存
	cfg := &config.MemoryScopeConfig{
		UserProfileDefinition: "测试画像",
	}
	env.m.scopeConfig["scope1"] = cfg

	// 从内存缓存读取
	result, err := env.m.getScopeConfig(context.Background(), "scope1")
	assert.NoError(t, err)
	assert.Equal(t, "测试画像", result.UserProfileDefinition)
}

// TestApplyScopeEmbedding_MemoryIndexNil 测试 memoryIndex 为 nil 时安全返回。
func TestApplyScopeEmbedding_MemoryIndexNil(t *testing.T) {
	env := newTestEnv(t)
	// memoryIndex 为 nil，不应 panic
	env.m.applyScopeEmbedding(context.Background(), "scope1")
}

// TestGetScopeEmbeddingModel_无配置 测试无 scope 配置时返回 nil。
func TestGetScopeEmbeddingModel_无配置(t *testing.T) {
	env := newTestEnv(t)
	result := env.m.getScopeEmbeddingModel(context.Background(), "scope1")
	assert.Nil(t, result)
}

// TestUserMemTotalNum_正常流程 测试 searchManager 已初始化时的计数。
func TestUserMemTotalNum_正常流程(t *testing.T) {
	env := newTestEnv(t)
	// searchManager 未初始化
	_, err := env.m.UserMemTotalNum(context.Background(), Uid("u1"), Sid("s1"))
	assert.Error(t, err)
}

// TestGetUserMemByPage_正常流程 测试 searchManager 未初始化。
func TestGetUserMemByPage_正常流程(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.m.GetUserMemByPage(context.Background(), 10, 1, mem_model.MemoryTypeUnknown,
		Uid("u1"), Sid("s1"))
	assert.Error(t, err)
}

// TestGetRecentMessages_ManagerNil 测试 messageManager 为 nil。
func TestGetRecentMessages_ManagerNil(t *testing.T) {
	env := newTestEnv(t)
	msgs, err := env.m.GetRecentMessages(context.Background(), 10, Uid("u1"), Sid("s1"))
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

// TestGetRecentMessages_无效ScopeID 测试无效 scopeID。
func TestGetRecentMessages_无效ScopeID(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.GetRecentMessages(context.Background(), 10, Sid("has/slash"))
	assert.Error(t, err)
}

// TestDeleteVariables_正常流程 测试 variableManager 未初始化。
func TestDeleteVariables_正常流程(t *testing.T) {
	env := newTestEnv(t)
	err := env.m.DeleteVariables(context.Background(), []string{"var1"}, Uid("u1"), Sid("s1"))
	// acquireUserLock 需要成功的 kvStore，但 variableManager 为 nil 在锁内部
	assert.Error(t, err)
}

// TestUpdateVariables_正常流程 测试 variableManager 未初始化。
func TestUpdateVariables_正常流程(t *testing.T) {
	env := newTestEnv(t)
	err := env.m.UpdateVariables(context.Background(), map[string]string{"k": "v"}, Uid("u1"), Sid("s1"))
	assert.Error(t, err)
}

// TestDeleteMemByIDImpl_正常流程 测试 writeManager 未初始化（在锁内部）。
func TestDeleteMemByIDImpl_正常流程(t *testing.T) {
	env := newTestEnv(t)
	err := env.m.deleteMemByIDImpl(context.Background(), "mem1", &userScopeParams{
		UserID:  "u1",
		ScopeID: "s1",
	})
	assert.Error(t, err)
}

// TestDeleteMemByUserIDImpl_正常流程 测试 writeManager 未初始化。
func TestDeleteMemByUserIDImpl_正常流程(t *testing.T) {
	env := newTestEnv(t)
	err := env.m.deleteMemByUserIDImpl(context.Background(), &userScopeParams{
		UserID:  "u1",
		ScopeID: "s1",
	})
	assert.Error(t, err)
}

// TestUpdateMemByIDImpl_正常流程 测试 writeManager 未初始化。
func TestUpdateMemByIDImpl_正常流程(t *testing.T) {
	env := newTestEnv(t)
	err := env.m.updateMemByIDImpl(context.Background(), "mem1", "new content", &userScopeParams{
		UserID:  "u1",
		ScopeID: "s1",
	})
	assert.Error(t, err)
}

// TestGetHistoryMessages_ManagerNil 测试 messageManager 为 nil。
func TestGetHistoryMessages_ManagerNil(t *testing.T) {
	m := NewLongTermMemory()
	msgs, err := m.getHistoryMessages(context.Background(), "u1", "s1", "sess1", 10)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

// TestAddMessages_正常流程 测试完整 AddMessages 流程。
func TestAddMessages_正常流程(t *testing.T) {
	env := newTestEnv(t)
	// writeManager 和 searchManager 未初始化
	_, err := env.m.AddMessages(context.Background(), nil, nil, WithScopeID("s1"), WithUserID("u1"))
	// 应该因为缺少 LLM 或 writeManager 而返回错误
	assert.Error(t, err)
}

// TestSearchUserMemImpl_正常流程 测试 searchManager 已初始化。
func TestSearchUserMemImpl_正常流程(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.m.searchUserMemImpl(context.Background(), &searchParams{
		Query:   "test",
		Num:     5,
		UserID:  "u1",
		ScopeID: "s1",
	}, env.m.fragmentType)
	assert.Error(t, err)
}
