package ltm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/codec"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/search"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/process/extract"
	apiembedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// SetConfig 设置系统配置，初始化所有子管理器。
//
// 完整流程对齐 Python set_config：
// 1. kv_store 和 db_store 必须已注册
// 2. memory_index 必须已提供
// 3. 创建 AesStorageCodec 并设置到 memoryIndex
// 4. 初始化各子管理器
// 5. 设置初始 LLM（如果配置中有默认模型）
//
// Python: LongTermMemory.set_config(config)
func (m *LongTermMemory) SetConfig(cfg *config.MemoryEngineConfig) error {
	if m.kvStore == nil || m.dbStore == nil {
		return exception.BuildError(exception.StatusMemorySetConfigExecutionError,
			exception.WithParam("config_type", "system"),
			exception.WithMsg("kv store and db store must be registered before setting config"),
		)
	}
	if m.memoryIndex == nil {
		return exception.BuildError(exception.StatusMemorySetConfigExecutionError,
			exception.WithParam("config_type", "system"),
			exception.WithMsg("memory_index must be provided (via register_plugin or register_store)"),
		)
	}
	m.sysMemConfig = cfg

	// 创建编解码器
	c, err := codec.NewAesStorageCodec(cfg.CryptoKey)
	if err != nil {
		return exception.BuildError(exception.StatusMemorySetConfigExecutionError,
			exception.WithParam("config_type", "system"),
			exception.WithMsg(fmt.Sprintf("创建 AesStorageCodec 失败: %v", err)),
			exception.WithCause(err),
		)
	}
	if m.memoryIndex != nil {
		m.memoryIndex.SetStorageCodec(c)
	}
	m.storageCodec = c

	// 初始化 DataIdManager
	dataIdGenerator := mem_model.NewDataIdManager()

	// 初始化 ScopeUserMappingManager
	// M-02: 缓存 sqlDbStore，对齐 Python self._sql_db_store 复用
	if m.sqlDbStore == nil {
		m.sqlDbStore = mem_model.NewSqlDbStore(m.dbStore)
	}
	m.scopeUserMappingManager = mem_model.NewScopeUserMappingManager(m.sqlDbStore)

	// 初始化 MessageManager
	if m.messageStore != nil {
		// S-01: 对齐 Python set_config，为 SqlMessageStore 回填 crypto_key
		if sqlMsg, ok := m.messageStore.(*mem_model.SqlMessageStore); ok {
			sqlMsg.SetStorageCodec(c)
		}
		m.messageManager = mem_model.NewMessageManager(m.messageStore)
	}

	// 初始化 FragmentMemoryManager
	m.fragmentMemoryManager = index.NewFragmentMemoryManager(m.memoryIndex, cfg.CryptoKey)

	// 初始化 SummaryManager
	m.summaryManager = index.NewSummaryManager(m.memoryIndex, cfg.CryptoKey)

	// 初始化 VariableManager
	varMgr, err := index.NewVariableManager(m.kvStore, cfg.CryptoKey)
	if err != nil {
		return exception.BuildError(exception.StatusMemorySetConfigExecutionError,
			exception.WithParam("config_type", "system"),
			exception.WithMsg(fmt.Sprintf("创建 VariableManager 失败: %v", err)),
			exception.WithCause(err),
		)
	}
	m.variableManager = varMgr

	// 初始化 WriteManager
	managers := map[string]index.BaseMemoryManager{
		mem_model.MemoryTypeUserProfile.String():    m.fragmentMemoryManager,
		mem_model.MemoryTypeEpisodicMemory.String(): m.fragmentMemoryManager,
		mem_model.MemoryTypeSemanticMemory.String(): m.fragmentMemoryManager,
		mem_model.MemoryTypeVariable.String():       m.variableManager,
		mem_model.MemoryTypeSummary.String():        m.summaryManager,
	}
	m.fragmentType = []string{
		mem_model.MemoryTypeUserProfile.String(),
		mem_model.MemoryTypeEpisodicMemory.String(),
		mem_model.MemoryTypeSemanticMemory.String(),
	}
	m.writeManager = index.NewWriteManager(managers, m.memoryIndex)

	// 初始化 SearchManager
	m.searchManager = search.NewSearchManager(managers, cfg.CryptoKey, m.memoryIndex)

	// 初始化 Generator
	m.generator = extract.NewGenerator(dataIdGenerator, m.searchManager)

	// 设置初始 LLM
	if cfg.DefaultModelCfg != nil && cfg.DefaultModelClientCfg != nil {
		llmInstance, llmErr := getLLMFromConfig(cfg.DefaultModelCfg, cfg.DefaultModelClientCfg)
		if llmErr != nil {
			logger.Error(logComponent).Err(llmErr).Msg("初始化默认 LLM 失败")
		} else {
			m.baseLLM = llmInstance
		}
	}

	return nil
}

// SetScopeConfig 设置 scope 级配置，深拷贝并加密 API Key 后存入 KVStore。
//
// 对齐 Python: LongTermMemory.set_scope_config(scope_id, memory_scope_config)
func (m *LongTermMemory) SetScopeConfig(ctx context.Context, scopeID string, scopeCfg *config.MemoryScopeConfig) error {
	if !validateID("MEMORY_STORE", scopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_STORE").
			Str("scope_id", scopeID).Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemorySetConfigExecutionError,
			exception.WithParam("config_type", "scope"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	// 深拷贝配置（避免修改原始）
	encryptedConfig := deepCopyScopeConfig(scopeCfg)

	// 加密 API Key
	if encryptedConfig.ModelClientCfg != nil && encryptedConfig.ModelClientCfg.APIKey != "" {
		encryptedConfig.ModelClientCfg.APIKey = m.storageCodec.Encode(encryptedConfig.ModelClientCfg.APIKey)
	}
	if encryptedConfig.EmbeddingCfg != nil && encryptedConfig.EmbeddingCfg.APIKey != "" {
		encryptedConfig.EmbeddingCfg.APIKey = m.storageCodec.Encode(encryptedConfig.EmbeddingCfg.APIKey)
	}

	m.scopeMu.Lock()
	m.scopeConfig[scopeID] = encryptedConfig
	m.scopeMu.Unlock()

	configKey := fmt.Sprintf("%s/%s", ScopeConfigKey, scopeID)
	configJSON, err := encryptedConfig.ToJSON()
	if err != nil {
		return exception.BuildError(exception.StatusMemorySetConfigExecutionError,
			exception.WithParam("config_type", "scope"),
			exception.WithMsg(fmt.Sprintf("序列化 scope 配置失败: %v", err)),
		)
	}
	if err := m.kvStore.Set(ctx, configKey, []byte(configJSON)); err != nil {
		return exception.BuildError(exception.StatusMemorySetConfigExecutionError,
			exception.WithParam("config_type", "scope"),
			exception.WithMsg(fmt.Sprintf("写入 KV 存储失败: %v", err)),
			exception.WithCause(err),
		)
	}

	// 清除 scope embedding 缓存
	m.scopeMu.Lock()
	delete(m.scopeEmbedding, scopeID)
	m.scopeMu.Unlock()

	return nil
}

// GetScopeConfig 从 KVStore 读取 scope 配置并解密 API Key。
//
// 对齐 Python: LongTermMemory.get_scope_config(scope_id)
func (m *LongTermMemory) GetScopeConfig(ctx context.Context, scopeID string) (*config.MemoryScopeConfig, error) {
	if !validateID("MEMORY_RETRIEVE", scopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_RETRIEVE").
			Str("scope_id", scopeID).Msg("Invalid scope_id format.")
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "scope_config"),
			exception.WithMsg("invalid scope_id format"),
		)
	}
	configKey := fmt.Sprintf("%s/%s", ScopeConfigKey, scopeID)
	configJSON, err := m.kvStore.Get(ctx, configKey)
	if err != nil {
		return nil, err
	}
	if configJSON == nil {
		return nil, nil
	}

	// 反序列化
	encryptedConfig, err := config.MemoryScopeConfigFromJSON(string(configJSON))
	if err != nil {
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "scope_config"),
			exception.WithMsg(fmt.Sprintf("反序列化 scope 配置失败: %v", err)),
		)
	}

	// 解密 API Key
	if encryptedConfig.ModelClientCfg != nil && encryptedConfig.ModelClientCfg.APIKey != "" {
		encryptedConfig.ModelClientCfg.APIKey = m.storageCodec.Decode(encryptedConfig.ModelClientCfg.APIKey)
	}
	if encryptedConfig.EmbeddingCfg != nil && encryptedConfig.EmbeddingCfg.APIKey != "" {
		encryptedConfig.EmbeddingCfg.APIKey = m.storageCodec.Decode(encryptedConfig.EmbeddingCfg.APIKey)
	}

	return encryptedConfig, nil
}

// DeleteScopeConfig 删除 scope 配置。
//
// 对齐 Python: LongTermMemory.delete_scope_config(scope_id)
func (m *LongTermMemory) DeleteScopeConfig(ctx context.Context, scopeID string) error {
	if !validateID("MEMORY_DELETE", scopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_DELETE").
			Str("scope_id", scopeID).Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
			exception.WithParam("memory_type", "scope_config"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	configKey := fmt.Sprintf("%s/%s", ScopeConfigKey, scopeID)
	if err := m.kvStore.Delete(ctx, configKey); err != nil {
		logger.Error(logComponent).Err(err).Str("event_type", "MEMORY_DELETE").
			Str("scope_id", scopeID).Msg("Failed to delete configuration.")
		return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
			exception.WithParam("memory_type", "scope_config"),
			exception.WithMsg(fmt.Sprintf("failed to delete scope config: %v", err)),
			exception.WithCause(err),
		)
	}

	m.scopeMu.Lock()
	delete(m.scopeConfig, scopeID)
	delete(m.scopeEmbedding, scopeID)
	m.scopeMu.Unlock()

	logger.Debug(logComponent).Str("event_type", "MEMORY_DELETE").
		Str("scope_id", scopeID).Msg("Successfully deleted configuration.")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopyScopeConfig 深拷贝 MemoryScopeConfig（对齐 Python copy.deepcopy）。
func deepCopyScopeConfig(cfg *config.MemoryScopeConfig) *config.MemoryScopeConfig {
	if cfg == nil {
		return nil
	}
	// 通过 JSON 序列化/反序列化实现深拷贝
	data, err := json.Marshal(cfg)
	if err != nil {
		// M-01: fallback 补充所有字段 + Warn 日志
		logger.Warn(logComponent).Err(err).Msg("deepCopyScopeConfig JSON 序列化失败，使用浅拷贝 fallback")
		return &config.MemoryScopeConfig{
			UserProfileDefinition:    cfg.UserProfileDefinition,
			SemanticMemoryDefinition: cfg.SemanticMemoryDefinition,
			EpisodicMemoryDefinition: cfg.EpisodicMemoryDefinition,
			ModelCfg:                 cfg.ModelCfg,
			ModelClientCfg:           cfg.ModelClientCfg,
			EmbeddingCfg:             cfg.EmbeddingCfg,
		}
	}
	copyCfg := &config.MemoryScopeConfig{}
	if err := json.Unmarshal(data, copyCfg); err != nil {
		logger.Warn(logComponent).Err(err).Msg("deepCopyScopeConfig JSON 反序列化失败，使用浅拷贝 fallback")
		return &config.MemoryScopeConfig{
			UserProfileDefinition:    cfg.UserProfileDefinition,
			SemanticMemoryDefinition: cfg.SemanticMemoryDefinition,
			EpisodicMemoryDefinition: cfg.EpisodicMemoryDefinition,
			ModelCfg:                 cfg.ModelCfg,
			ModelClientCfg:           cfg.ModelClientCfg,
			EmbeddingCfg:             cfg.EmbeddingCfg,
		}
	}
	return copyCfg
}

// getScopeLLM 获取 scope 级 LLM，scope 配置优先 → 系统默认 → baseLLM。
// 对齐 Python: LongTermMemory._get_scope_llm(scope_id)
func (m *LongTermMemory) getScopeLLM(ctx context.Context, scopeID string) (*llm.Model, error) {
	scopeCfg, err := m.getScopeConfig(ctx, scopeID)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("scope_id", scopeID).
			Msg("Failed to get scope LLM.")
		return m.baseLLM, nil
	}

	if scopeCfg != nil && scopeCfg.ModelCfg != nil && scopeCfg.ModelClientCfg != nil {
		llmInstance, llmErr := getLLMFromConfig(scopeCfg.ModelCfg, scopeCfg.ModelClientCfg)
		if llmErr != nil {
			logger.Error(logComponent).Err(llmErr).Str("scope_id", scopeID).
				Msg("Failed to create LLM from scope config.")
			return m.baseLLM, nil
		}
		return llmInstance, nil
	}

	// 系统默认配置
	if m.sysMemConfig != nil {
		if m.sysMemConfig.DefaultModelClientCfg == nil {
			logger.Debug(logComponent).Str("event_type", "MEMORY_RETRIEVE").
				Str("scope_id", scopeID).
				Msg("Default model client config is missing, cannot instantiate LLM.")
		} else if m.sysMemConfig.DefaultModelCfg == nil {
			logger.Debug(logComponent).Str("event_type", "MEMORY_RETRIEVE").
				Str("scope_id", scopeID).
				Msg("Default model config is missing, cannot instantiate LLM.")
		} else {
			llmInstance, llmErr := getLLMFromConfig(m.sysMemConfig.DefaultModelCfg, m.sysMemConfig.DefaultModelClientCfg)
			if llmErr != nil {
				logger.Error(logComponent).Err(llmErr).Str("scope_id", scopeID).
					Msg("Failed to create LLM from system default config.")
				return m.baseLLM, nil
			}
			return llmInstance, nil
		}
	}

	return m.baseLLM, nil
}

// getScopeConfig 获取 scope 配置，内存缓存优先 → KVStore 读取。
// 对齐 Python: LongTermMemory._get_scope_config(scope_id)
func (m *LongTermMemory) getScopeConfig(ctx context.Context, scopeID string) (*config.MemoryScopeConfig, error) {
	// 先查内存缓存
	m.scopeMu.RLock()
	cfg, ok := m.scopeConfig[scopeID]
	m.scopeMu.RUnlock()
	if ok {
		// 深拷贝避免修改缓存中的加密配置
		decryptedConfig := deepCopyScopeConfig(cfg)
		// 解密 API Key
		if decryptedConfig.ModelClientCfg != nil && decryptedConfig.ModelClientCfg.APIKey != "" {
			decryptedConfig.ModelClientCfg.APIKey = m.storageCodec.Decode(decryptedConfig.ModelClientCfg.APIKey)
		}
		if decryptedConfig.EmbeddingCfg != nil && decryptedConfig.EmbeddingCfg.APIKey != "" {
			decryptedConfig.EmbeddingCfg.APIKey = m.storageCodec.Decode(decryptedConfig.EmbeddingCfg.APIKey)
		}
		return decryptedConfig, nil
	}

	// 内存未命中，从 KVStore 读取
	return m.GetScopeConfig(ctx, scopeID)
}

// applyScopeEmbedding 将 scope 级嵌入模型应用到 memoryIndex。
// 对齐 Python: LongTermMemory._apply_scope_embedding(scope_id)
func (m *LongTermMemory) applyScopeEmbedding(ctx context.Context, scopeID string) {
	if m.memoryIndex == nil {
		return
	}
	scopeEmbed := m.getScopeEmbeddingModel(ctx, scopeID)
	if scopeEmbed != nil {
		m.memoryIndex.SetEmbeddingModel(scopeEmbed)
	} else {
		m.memoryIndex.SetEmbeddingModel(m.baseEmbed)
	}
}

// getScopeEmbeddingModel 获取 scope 级嵌入模型，缓存优先 → 配置实例化。
// 对齐 Python: LongTermMemory._get_scope_embedding_model(scope_id)
func (m *LongTermMemory) getScopeEmbeddingModel(ctx context.Context, scopeID string) embedding.BaseEmbedding {
	// 检查缓存
	m.scopeMu.RLock()
	emb, ok := m.scopeEmbedding[scopeID]
	m.scopeMu.RUnlock()
	if ok {
		return emb
	}

	// 从 scope 配置获取
	scopeCfg, err := m.getScopeConfig(ctx, scopeID)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("event_type", "MEMORY_RETRIEVE").
			Str("scope_id", scopeID).Msg("Failed to get scope config for embedding.")
		return nil
	}

	if scopeCfg != nil && scopeCfg.EmbeddingCfg != nil {
		// 使用 APIEmbedding 实例化
		emb := apiembedding.NewAPIEmbedding(*scopeCfg.EmbeddingCfg)
		m.scopeMu.Lock()
		m.scopeEmbedding[scopeID] = emb
		m.scopeMu.Unlock()
		return emb
	}

	logger.Error(logComponent).Str("event_type", "MEMORY_RETRIEVE").
		Str("scope_id", scopeID).Msg("No embedding model available.")
	return nil
}
