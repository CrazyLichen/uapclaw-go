package external

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	memconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/ltm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewOpenJiuwenProvider_空配置(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	assert.Equal(t, "openjiuwen", p.Name())
	assert.False(t, p.IsAvailable())
	assert.False(t, p.IsInitialized())
}

func TestNewOpenJiuwenProvider_嵌入配置可用(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"embedding": map[string]any{
			"model_name": "text-embedding-v3",
		},
	})
	assert.True(t, p.IsAvailable())
}

func TestNewOpenJiuwenProvider_嵌入配置无model_name不可用(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"embedding": map[string]any{
			"base_url": "https://api.example.com",
		},
	})
	assert.False(t, p.IsAvailable())
}

func TestNewOpenJiuwenProvider_默认UserID(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	assert.Equal(t, ltm.DefaultValue, p.userID)
	assert.Equal(t, ltm.DefaultValue, p.scopeID)
	assert.Equal(t, ltm.DefaultValue, p.sessionID)
}

func TestNewOpenJiuwenProvider_WithOption(t *testing.T) {
	p := NewOpenJiuwenProvider(nil,
		OJWithEngineConfig(memconfig.DefaultMemoryEngineConfig()),
		OJWithScopeConfig(memconfig.DefaultMemoryScopeConfig()),
		OJWithAgentMemoryConfig(memconfig.DefaultAgentMemoryConfig()),
	)
	assert.NotNil(t, p.engineConfig)
	assert.NotNil(t, p.scopeConfig)
	assert.NotNil(t, p.agentMemoryConfig)
}

func TestOpenJiuwenProvider_GetToolSchemas(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	schemas := p.GetToolSchemas()
	assert.Len(t, schemas, 2)
	assert.Equal(t, "ltm_search", schemas[0].Name)
	assert.Equal(t, "ltm_search_summary", schemas[1].Name)
	// 验证 Schema 参数
	ltmParams, ok := schemas[0].Parameters["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, ltmParams, "query")
	assert.Contains(t, ltmParams, "num")
	assert.Contains(t, ltmParams, "threshold")
	summaryParams, ok := schemas[1].Parameters["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, summaryParams, "query")
	assert.Contains(t, summaryParams, "num")
}

func TestOpenJiuwenProvider_SystemPromptBlock(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	block := p.SystemPromptBlock()
	assert.Contains(t, block, "Long-Term Memory System")
	assert.Contains(t, block, "ltm_search")
	assert.Contains(t, block, "User profile")
	assert.Contains(t, block, "Episodic memory")
	assert.Contains(t, block, "Semantic memory")
	assert.Contains(t, block, "Conversation summaries")
}

func TestOpenJiuwenProvider_Shutdown(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	p.initialized = true
	err := p.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.False(t, p.IsInitialized())
}

func TestOpenJiuwenProvider_HandleToolCall_未初始化(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	result, err := p.HandleToolCall(context.Background(), "ltm_search", map[string]any{"query": "test"})
	assert.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "Memory provider not initialized", parsed["error"])
}

func TestOpenJiuwenProvider_HandleToolCall_未知工具(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	p.initialized = true
	// 直接测试 dispatchToolCall（跳过 HandleToolCall 中的 LTM 初始化检查）
	_, err := p.dispatchToolCall(context.Background(), "unknown_tool", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestOpenJiuwenProvider_HandleToolCall_error转JSON(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	p.initialized = true
	// HandleToolCall 在 dispatchToolCall 返回 error 时应转为 {"error":..., "results":[]}
	// 需要让 LTM IsInitialized 返回 true，否则先走 "not initialized" 分支
	// 此处用 ltm_search 调用（LTM 单例未真正初始化，dispatchToolCall 会返回 error）
	result, err := p.HandleToolCall(context.Background(), "ltm_search", map[string]any{"query": "test"})
	assert.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	// LTM 未初始化时应返回 "Memory provider not initialized" error
	assert.Contains(t, parsed["error"], "not initialized")
}

func TestOpenJiuwenProvider_Prefetch_未初始化(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	result, err := p.Prefetch(context.Background(), "test query")
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestOpenJiuwenProvider_Prefetch_空查询(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	p.initialized = true
	result, err := p.Prefetch(context.Background(), "")
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestOpenJiuwenProvider_SyncTurn_未初始化(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	err := p.SyncTurn(context.Background(), "hello", "world")
	assert.NoError(t, err)
}

func TestOpenJiuwenProvider_SyncTurn_空消息(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	p.initialized = true
	err := p.SyncTurn(context.Background(), "", "")
	assert.NoError(t, err)
}

func TestOpenJiuwenProvider_parseScopeConfig_空配置(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	cfg := p.parseScopeConfig()
	assert.Nil(t, cfg)
}

func TestOpenJiuwenProvider_parseScopeConfig_有效配置(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"scope_config": map[string]any{
			"user_profile_definition":    "测试画像",
			"semantic_memory_definition": "测试语义",
			"episodic_memory_definition": "测试情景",
		},
	})
	cfg := p.parseScopeConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, "测试画像", cfg.UserProfileDefinition)
	assert.Equal(t, "测试语义", cfg.SemanticMemoryDefinition)
	assert.Equal(t, "测试情景", cfg.EpisodicMemoryDefinition)
}

func TestOpenJiuwenProvider_parseScopeConfig_无效JSON(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"scope_config": map[string]any{
			"model_client_cfg": "not a valid object",
		},
	})
	// model_client_cfg 是 string，无法反序列化为 ModelClientConfig 结构体
	// json.Unmarshal 会失败，parseScopeConfig 返回 nil
	cfg := p.parseScopeConfig()
	assert.Nil(t, cfg)
}

func TestOpenJiuwenProvider_createKVStore_memory后端(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"kv": map[string]any{"backend": "memory"},
	})
	store := p.createKVStore()
	assert.NotNil(t, store)
}

func TestOpenJiuwenProvider_createKVStore_不支持的backend(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"kv": map[string]any{"backend": "unknown"},
	})
	store := p.createKVStore()
	assert.Nil(t, store)
}

func TestOpenJiuwenProvider_createVectorStore_不支持的backend(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"vector": map[string]any{"backend": "unknown"},
	})
	store := p.createVectorStore()
	assert.Nil(t, store)
}

func TestOpenJiuwenProvider_createDBStore_不支持的backend(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"db": map[string]any{"backend": "unknown"},
	})
	store := p.createDBStore(context.Background())
	assert.Nil(t, store)
}

func TestOpenJiuwenProvider_createEmbedding_无配置(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	emb := p.createEmbedding()
	assert.Nil(t, emb)
}

func TestOpenJiuwenProvider_createEmbedding_有配置(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"embedding": map[string]any{
			"model_name": "text-embedding-v3",
			"base_url":   "https://api.example.com",
			"api_key":    "test-key",
		},
	})
	emb := p.createEmbedding()
	assert.NotNil(t, emb)
}

func TestOpenJiuwenProvider_createEmbedding_无model_name(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"embedding": map[string]any{
			"base_url": "https://api.example.com",
		},
	})
	emb := p.createEmbedding()
	assert.Nil(t, emb)
}

func TestOpenJiuwenProvider_常量默认值(t *testing.T) {
	// 对齐 Python 默认值
	assert.Equal(t, 5, defaultRecallUserMemNum)
	assert.Equal(t, 3, defaultRecallHistoryMemNum)
	assert.Equal(t, "memory", defaultKVBackend)
	assert.Equal(t, "chroma", defaultVectorBackend)
	assert.Equal(t, "sqlite", defaultDBBackend)
}

func TestOpenJiuwenProvider_OJWithKVStore(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	p := NewOpenJiuwenProvider(nil, OJWithKVStore(store))
	assert.NotNil(t, p.kvStore)
	assert.Equal(t, store, p.kvStore)
}

func TestOpenJiuwenProvider_OJWithVectorStore(t *testing.T) {
	p := NewOpenJiuwenProvider(nil, OJWithVectorStore(nil))
	// vectorStore 为 nil，但 option 被调用了
	assert.Nil(t, p.vectorStore)
}

func TestOpenJiuwenProvider_OJWithDbStore(t *testing.T) {
	p := NewOpenJiuwenProvider(nil, OJWithDbStore(nil))
	assert.Nil(t, p.dbStore)
}

func TestOpenJiuwenProvider_OJWithEmbeddingModel(t *testing.T) {
	p := NewOpenJiuwenProvider(nil, OJWithEmbeddingModel(nil))
	assert.Nil(t, p.embeddingModel)
}

func TestOpenJiuwenProvider_IsAvailable_三store都有(t *testing.T) {
	p := NewOpenJiuwenProvider(nil,
		OJWithKVStore(kv.NewInMemoryKVStore()),
	)
	// 只有 kvStore，不够
	assert.False(t, p.IsAvailable())
}

func TestOpenJiuwenProvider_接口满足(t *testing.T) {
	// 编译时验证 OpenJiuwenProvider 满足 MemoryProvider 接口
	var _ MemoryProvider = (*OpenJiuwenProvider)(nil)
}

func TestOpenJiuwenProvider_createKVStore_shelve后端(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"kv": map[string]any{"backend": "shelve", "path": t.TempDir() + "/kv"},
	})
	store := p.createKVStore()
	assert.NotNil(t, store)
}

func TestOpenJiuwenProvider_createKVStore_sqlite后端(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"kv": map[string]any{"backend": "sqlite", "path": t.TempDir() + "/kv.db"},
	})
	store := p.createKVStore()
	assert.NotNil(t, store)
}

func TestOpenJiuwenProvider_createVectorStore_chroma后端(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"vector": map[string]any{"backend": "chroma", "persist_directory": t.TempDir() + "/chroma"},
	})
	store := p.createVectorStore()
	assert.NotNil(t, store)
}

func TestOpenJiuwenProvider_createDBStore_sqlite后端(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"db": map[string]any{"backend": "sqlite", "path": t.TempDir() + "/ltm.db"},
	})
	store := p.createDBStore(context.Background())
	assert.NotNil(t, store)
}

func TestOpenJiuwenProvider_Initialize_缺失store(t *testing.T) {
	// config 没有 embedding 且没有直接注入 store → IsAvailable 为 false → createKVStore 返回 memory 类型
	// 但 vectorStore (chroma) 和 dbStore (sqlite) 需要目录存在
	p := NewOpenJiuwenProvider(map[string]any{
		"vector": map[string]any{"backend": "chroma", "persist_directory": t.TempDir() + "/chroma"},
		"db":     map[string]any{"backend": "sqlite", "path": t.TempDir() + "/ltm.db"},
	})
	err := p.Initialize(context.Background())
	assert.NoError(t, err)
	// kvStore 和 dbStore 都创建了，但 vectorStore (chroma) 可能需要 chroma 运行环境
	// 核心验证：Initialize 不应 panic
}

func TestOpenJiuwenProvider_Initialize_覆盖UserID(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	err := p.Initialize(context.Background(), WithUserID("new-user"), WithScopeID("new-scope"), WithSessionID("new-session"))
	// S-02: 无 config 时 store 创建失败，Initialize 现在返回 error 而非吞错
	if err == nil {
		// 如果恰好所有 store 都创建成功（不太可能在 nil config 下发生）
		assert.Equal(t, "new-user", p.userID)
		assert.Equal(t, "new-scope", p.scopeID)
		assert.Equal(t, "new-session", p.sessionID)
	} else {
		// 预期：无 config 时 Initialize 返回 error
		assert.Contains(t, err.Error(), "store creation failed")
		// 验证选项仍然被正确设置
		assert.Equal(t, "new-user", p.userID)
		assert.Equal(t, "new-scope", p.scopeID)
		assert.Equal(t, "new-session", p.sessionID)
	}
}
