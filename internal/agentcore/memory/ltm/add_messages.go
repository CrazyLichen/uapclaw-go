package ltm

import (
	"context"
	"fmt"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/process/extract"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AddMessages 添加消息并生成记忆。
//
// ① emit_before MEMORY_ADDED 回调 → ② addMessagesImpl 执行逻辑
// 对齐 Python: @_fw.emit_before(MemoryEvents.MEMORY_ADDED) + add_messages
func (m *LongTermMemory) AddMessages(
	ctx context.Context,
	messages []llmschema.BaseMessage,
	agentConfig *config.AgentMemoryConfig,
	opts ...AddMessagesOption,
) (*AddMemResult, error) {
	// ① 触发 MEMORY_ADDED 回调
	p := newAddMessagesParams(messages, agentConfig, opts...)
	triggerMemoryBefore(ctx, callback.MemoryAdded, &callback.MemoryEventData{
		Event:   callback.MemoryAdded,
		UserID:  p.UserID,
		ScopeID: p.ScopeID,
	})

	// ② 执行核心逻辑
	return m.addMessagesImpl(ctx, p)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// addMessagesImpl AddMessages 的核心实现。
//
// 完整流程对齐 Python add_messages：
// 1. validateID(scopeID)
// 2. getScopeLLM(scopeID)
// 3. getScopeConfig(scopeID)
// 4. applyScopeEmbedding(scopeID)
// 5. DistributedLock(kvStore, "user/{userID}")
// 6. LLM 为空返回错误
// 7. getHistoryMessages(userID, scopeID, sessionID, historyWindowSize)
// 8. scopeUserMappingManager.Add(userID, scopeID)
// 9. timestamp 为 nil 时 time.Now()
// 10. 遍历 messages 调 messageManager.Add()
// 11. genMem=false → 返回空 AddMemResult
// 12. checkMessages → 无 human 消息返回空 AddMemResult
// 13. generator.GenAllMemory(ctx, params)
// 14. writeManager.AddMemories(ctx, userID, scopeID, allMemory, llm)
// 15. 按 MemoryType 分类填充 AddMemResult
func (m *LongTermMemory) addMessagesImpl(ctx context.Context, p *addMessagesParams) (*AddMemResult, error) {
	// Step 1: validateID
	if !validateID("MEMORY_STORE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_STORE").
			Str("scope_id", p.ScopeID).Str("user_id", p.UserID).
			Msg("Invalid scope_id format.")
		return nil, exception.BuildError(exception.StatusMemoryAddMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	// Step 2: getScopeLLM
	llmInstance, err := m.getScopeLLM(ctx, p.ScopeID)
	if err != nil {
		return nil, err
	}

	// Step 3: getScopeConfig
	scopeConfig, err := m.getScopeConfig(ctx, p.ScopeID)
	if err != nil {
		return nil, err
	}

	// Step 4: applyScopeEmbedding
	m.applyScopeEmbedding(ctx, p.ScopeID)

	// Step 5-15: 分布式锁内执行
	var result *AddMemResult
	lockErr := acquireUserLock(ctx, m.kvStore, p.UserID, func(ctx context.Context) error {
		// Step 6: LLM 未初始化返回错误
		if llmInstance == nil {
			logger.Error(logComponent).Str("event_type", "MEMORY_STORE").
				Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
				Msg("LLM is not initialized.")
			return exception.BuildError(exception.StatusMemoryAddMemoryExecutionError,
				exception.WithParam("memory_type", "all"),
				exception.WithMsg("LLM is not initialized"),
			)
		}

		// Step 7: getHistoryMessages
		historyMessages, histErr := m.getHistoryMessages(ctx, p.UserID, p.ScopeID, p.SessionID, p.GenMemWithHistoryMsgNum)
		if histErr != nil {
			return histErr
		}

		// Step 8: scopeUserMappingManager.Add
		if m.scopeUserMappingManager != nil {
			if addErr := m.scopeUserMappingManager.Add(ctx, p.UserID, p.ScopeID); addErr != nil {
				logger.Error(logComponent).Err(addErr).Str("user_id", p.UserID).
					Str("scope_id", p.ScopeID).Msg("添加 scope_user_mapping 失败")
			}
		}

		// Step 9: timestamp 为 nil 时 time.Now()
		timestamp := p.Timestamp
		if timestamp == nil {
			now := time.Now()
			timestamp = &now
		}
		timestampStr := timestamp.Format("2006-01-02 15:04:05")

		// Step 10: 遍历 messages 调 messageManager.Add()
		msgID := "-1"
		for i, msg := range p.Messages {
			msgTimestamp := timestamp.Add(time.Millisecond * time.Duration(i))
			addReq := &mem_model.MessageAddRequest{
				UserID:    p.UserID,
				ScopeID:   p.ScopeID,
				Content:   msg.GetContent().Text(),
				Role:      msg.GetRole().String(),
				SessionID: p.SessionID,
				Timestamp: msgTimestamp,
			}
			if m.messageManager != nil {
				id, addErr := m.messageManager.Add(ctx, addReq)
				if addErr != nil {
					logger.Error(logComponent).Err(addErr).Str("user_id", p.UserID).
						Str("scope_id", p.ScopeID).Msg("添加消息失败")
					continue
				}
				msgID = id
			}
		}

		// Step 11: genMem=false → 返回空 AddMemResult
		if !p.GenMem {
			result = &AddMemResult{}
			return nil
		}

		// Step 12: checkMessages → 无 human 消息返回空 AddMemResult
		hasHumanMsg, checkedMessages := m.checkMessages(p.Messages)
		if !hasHumanMsg {
			logger.Debug(logComponent).Str("event_type", "MEMORY_STORE").
				Str("memory_type", "message").Int("memory_count", len(checkedMessages)).
				Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
				Msg("Memory engine no need to process messages.")
			result = &AddMemResult{}
			return nil
		}

		// Step 13: generator.GenAllMemory
		forbiddenVariables := ""
		summaryMaxToken := 0
		if m.sysMemConfig != nil {
			forbiddenVariables = m.sysMemConfig.ForbiddenVariables
			summaryMaxToken = m.sysMemConfig.SingleTurnHistorySummaryMaxToken
		}
		genParams := &extract.GenAllMemoryParams{
			Messages:          checkedMessages,
			HistoryMessages:   historyMessages,
			BaseModel:         llmInstance,
			MemoryConfig:      p.AgentConfig,
			EngineConfig:      m.sysMemConfig,
			ScopeConfig:       scopeConfig,
			UserID:            p.UserID,
			ScopeID:           p.ScopeID,
			ForbiddenVariables: forbiddenVariables,
			MessageMemID:      msgID,
			Timestamp:         timestampStr,
			SummaryMaxToken:   summaryMaxToken,
		}
		allMemory, genErr := m.generator.GenAllMemory(ctx, genParams)
		if genErr != nil {
			return genErr
		}

		// Step 14: writeManager.AddMemories（对齐 Python: write_manager.add_memories(memories, llm=llm)）
		writeResult, writeErr := m.writeManager.AddMemories(ctx, p.UserID, p.ScopeID, allMemory, index.WithLLMModel(llmInstance))
		if writeErr != nil {
			logger.Error(logComponent).Err(writeErr).Str("memory_type", "unknown").
				Str("event_type", "MEMORY_STORE").Str("user_id", p.UserID).
				Str("scope_id", p.ScopeID).Msg("Failed to add mem.")
			return exception.BuildError(exception.StatusMemoryAddMemoryExecutionError,
				exception.WithParam("memory_type", "unknown"),
				exception.WithMsg(fmt.Sprintf("%v", writeErr)),
				exception.WithCause(writeErr),
			)
		}

		logger.Debug(logComponent).Str("event_type", "MEMORY_STORE").
			Int("memory_count", len(allMemory)).Str("memory_type", "all type").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Msg("Successfully added memory units.")

		// Step 15: 按 MemoryType 分类填充 AddMemResult
		result = classifyWriteResult(writeResult)
		return nil
	})

	if lockErr != nil {
		return nil, lockErr
	}
	return result, nil
}

// classifyWriteResult 按 MemoryType 分类写入结果。
// 对齐 Python: AddMemResult(variables=[...], user_profile=[...], ...)
func classifyWriteResult(writeResult []mem_model.MemoryUnit) *AddMemResult {
	r := &AddMemResult{}
	for _, unit := range writeResult {
		switch unit.GetMemType() {
		case mem_model.MemoryTypeVariable:
			if v, ok := unit.(*mem_model.VariableUnit); ok {
				r.Variables = append(r.Variables, v)
			}
		case mem_model.MemoryTypeUserProfile:
			if v, ok := unit.(*mem_model.FragmentMemoryUnit); ok {
				r.UserProfile = append(r.UserProfile, v)
			}
		case mem_model.MemoryTypeSemanticMemory:
			if v, ok := unit.(*mem_model.FragmentMemoryUnit); ok {
				r.SemanticMemory = append(r.SemanticMemory, v)
			}
		case mem_model.MemoryTypeEpisodicMemory:
			if v, ok := unit.(*mem_model.FragmentMemoryUnit); ok {
				r.EpisodicMemory = append(r.EpisodicMemory, v)
			}
		case mem_model.MemoryTypeSummary:
			if v, ok := unit.(*mem_model.SummaryUnit); ok {
				r.Summary = append(r.Summary, v)
			}
		}
	}
	return r
}
