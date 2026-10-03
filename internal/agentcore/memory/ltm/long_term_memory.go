package ltm

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	db "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	storeindex "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	vector "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/codec"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/search"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/process/extract"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LongTermMemory 长期记忆编排器，记忆系统的顶层入口。
//
// 不实现任何存储逻辑，而是组装和协调已有的各子模块，
// 对外提供统一的记忆写入、检索、删除、更新 API。
// 使用 sync.Once 全局单例模式。
//
// Python: openjiuwen/core/memory/long_term_memory.py (LongTermMemory)
type LongTermMemory struct {
	// sysMemConfig 系统记忆引擎配置
	sysMemConfig *config.MemoryEngineConfig
	// scopeConfig 内存中的 scope 配置缓存（已加密）
	scopeConfig map[string]*config.MemoryScopeConfig
	// scopeMu 保护 scopeConfig 和 scopeEmbedding 的读写
	scopeMu sync.RWMutex
	// dbStore 后端存储
	kvStore      kv.BaseKVStore
	vectorStore  vector.BaseVectorStore
	dbStore      db.BaseDbStore
	sqlDbStore   *mem_model.SqlDbStore // 缓存的 SqlDbStore 实例，对齐 Python: self._sql_db_store
	messageStore db.BaseMessageStore
	// memoryIndex 记忆索引
	memoryIndex storeindex.BaseMemoryIndex
	// storageCodec AES 编解码器
	storageCodec *codec.AesStorageCodec
	// managers 各子管理器
	scopeUserMappingManager *mem_model.ScopeUserMappingManager
	messageManager          *mem_model.MessageManager
	fragmentMemoryManager   *index.FragmentMemoryManager
	variableManager         *index.VariableManager
	writeManager            *index.WriteManager
	summaryManager          *index.SummaryManager
	searchManager           *search.SearchManager
	generator               *extract.Generator
	// fragmentType 碎片记忆类型列表
	fragmentType []string
	// llm
	baseLLM *llm.Model
	// embedding
	baseEmbed embedding.BaseEmbedding
	// scopeEmbedding scope 级 embedding 模型缓存
	scopeEmbedding map[string]embedding.BaseEmbedding
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ltmOnce 控制 GetLongTermMemory 的单例初始化
	ltmOnce sync.Once
	// ltmInstance 全局 LongTermMemory 实例
	ltmInstance *LongTermMemory
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetLongTermMemory 返回全局 LongTermMemory 单例。
// 首次调用时自动初始化，后续调用返回同一实例。
//
// Python: LongTermMemory() — Singleton metaclass
func GetLongTermMemory() *LongTermMemory {
	ltmOnce.Do(func() {
		ltmInstance = NewLongTermMemory()
	})
	return ltmInstance
}

// NewLongTermMemory 创建新的 LongTermMemory 实例（零值初始化）。
//
// Python: LongTermMemory.__init__()
func NewLongTermMemory() *LongTermMemory {
	return &LongTermMemory{
		scopeConfig:    make(map[string]*config.MemoryScopeConfig),
		scopeEmbedding: make(map[string]embedding.BaseEmbedding),
	}
}

// ResetLongTermMemory 重置全局单例（仅用于测试）。
func ResetLongTermMemory() {
	ltmOnce = sync.Once{}
	ltmInstance = nil
}

// KVStore 返回已注册的 KV 存储（nil 表示未注册）。
// 供 OpenJiuwenProvider 判断是否需要 RegisterStore。
func (m *LongTermMemory) KVStore() kv.BaseKVStore {
	return m.kvStore
}

// IsInitialized 返回 LTM 是否已初始化（kvStore/dbStore 已注册且 SetConfig 已调用）。
func (m *LongTermMemory) IsInitialized() bool {
	return m.kvStore != nil && m.dbStore != nil && m.searchManager != nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getLLMFromConfig 从配置创建 LLM 实例。
// 对齐 Python: LongTermMemory._get_llm_from_config(model_config, model_client_config)
func getLLMFromConfig(modelConfig *llmschema.ModelRequestConfig, modelClientConfig *llmschema.ModelClientConfig) (*llm.Model, error) {
	return llm.NewModel(modelClientConfig, modelConfig)
}

// triggerMemoryBefore 触发记忆事件回调（emit_before 模式，在操作前调用）。
// 对齐 Python: @_fw.emit_before(MemoryEvents.MEMORY_ADDED) 等
func triggerMemoryBefore(ctx context.Context, event callback.MemoryEventType, data *callback.MemoryEventData) {
	fw := callback.GetCallbackFramework()
	if fw != nil {
		fw.TriggerMemory(ctx, data)
	}
}

// triggerMemoryAfter 触发记忆事件回调（emit_after 模式，在操作后调用）。
// 对齐 Python: trigger(MemoryEvents.MEMORY_SEARCH_FINISHED, ...)
func triggerMemoryAfter(ctx context.Context, event callback.MemoryEventType, data *callback.MemoryEventData) {
	fw := callback.GetCallbackFramework()
	if fw != nil {
		fw.TriggerMemory(ctx, data)
	}
}

// validateID 校验 scopeID 格式。
// 对齐 Python: LongTermMemory._validate_id(event_type, scope_id)
func validateID(eventType string, scopeID string) bool {
	if scopeID == "" {
		logger.Error(logComponent).Str("event_type", eventType).
			Str("scope_id", scopeID).
			Msg("Scope_id is invalid.")
		return false
	}
	if strings.Contains(scopeID, "/") {
		logger.Error(logComponent).Str("event_type", eventType).
			Str("scope_id", scopeID).
			Msg("Scope_id cannot contain separator '/'.")
		return false
	}
	if len(scopeID) > 128 {
		logger.Error(logComponent).Str("event_type", eventType).
			Str("scope_id", scopeID).
			Msg("Scope_id length exceeds limit (128).")
		return false
	}
	return true
}

// checkMessages 检查消息列表中是否包含 human 消息，并截断非 human 消息。
// 对齐 Python: LongTermMemory._check_messages(messages)
func (m *LongTermMemory) checkMessages(messages []llmschema.BaseMessage) (bool, []llmschema.BaseMessage) {
	outMessages := make([]llmschema.BaseMessage, 0, len(messages))
	hasHumanMsg := false
	inputMsgMaxLen := config.DefaultMemoryEngineConfig().InputMsgMaxLen
	if m.sysMemConfig != nil && m.sysMemConfig.InputMsgMaxLen > 0 {
		inputMsgMaxLen = m.sysMemConfig.InputMsgMaxLen
	}

	for _, msg := range messages {
		if msg.GetRole() == llmschema.RoleTypeUser {
			outMessages = append(outMessages, msg)
			hasHumanMsg = true
			continue
		}
		// 截断非 human 消息内容
		content := msg.GetContent()
		text := content.Text()
		if utf8.RuneCountInString(text) > inputMsgMaxLen {
			runes := []rune(text)
			msg.SetContent(llmschema.NewTextContent(string(runes[:inputMsgMaxLen])))
		}
		outMessages = append(outMessages, msg)
	}
	return hasHumanMsg, outMessages
}

// getHistoryMessages 获取历史消息。
// 对齐 Python: LongTermMemory._get_history_messages(user_id, scope_id, session_id, history_window_size)
func (m *LongTermMemory) getHistoryMessages(ctx context.Context, userID string, scopeID string, sessionID string, historyWindowSize int) ([]llmschema.BaseMessage, error) {
	if m.messageManager == nil {
		return []llmschema.BaseMessage{}, nil
	}
	msgAndMetas, err := m.messageManager.Get(ctx, userID, scopeID, sessionID, historyWindowSize)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("user_id", userID).
			Str("scope_id", scopeID).Msg("获取历史消息失败")
		return nil, err
	}
	inputMsgMaxLen := config.DefaultMemoryEngineConfig().InputMsgMaxLen
	if m.sysMemConfig != nil && m.sysMemConfig.InputMsgMaxLen > 0 {
		inputMsgMaxLen = m.sysMemConfig.InputMsgMaxLen
	}

	var historyMessages []llmschema.BaseMessage
	for _, mm := range msgAndMetas {
		msg := mm.Message
		if msg.GetRole() == llmschema.RoleTypeUser {
			historyMessages = append(historyMessages, msg)
			continue
		}
		// 截断非 human 消息内容
		text := msg.GetContent().Text()
		if utf8.RuneCountInString(text) > inputMsgMaxLen {
			runes := []rune(text)
			msg.SetContent(llmschema.NewTextContent(string(runes[:inputMsgMaxLen])))
		}
		historyMessages = append(historyMessages, msg)
	}
	return historyMessages, nil
}

// runMigration 执行单个迁移函数，统一日志和错误处理。
// 对齐 Python: LongTermMemory._run_migration(migrate_func, store, store_type)
func runMigration(ctx context.Context, migrateFunc func(ctx context.Context) error, storeType string) error {
	logger.Info(logComponent).Str("event_type", "MEMORY_INIT").Str("store_type", storeType).
		Msg("Starting migration")
	err := migrateFunc(ctx)
	if err != nil {
		logger.Error(logComponent).Str("event_type", "MEMORY_INIT").Err(err).Str("store_type", storeType).
			Msg("Migration failed")
		return err
	}
	logger.Info(logComponent).Str("event_type", "MEMORY_INIT").Str("store_type", storeType).
		Msg("Migration completed successfully")
	return nil
}
