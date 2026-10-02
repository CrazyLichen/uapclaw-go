package ltm

import (
	"context"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetRecentMessages 获取最近消息。
//
// 对齐 Python: LongTermMemory.get_recent_messages(user_id, scope_id, session_id, num)
func (m *LongTermMemory) GetRecentMessages(
	ctx context.Context,
	opts ...SearchOption,
) ([]llmschema.BaseMessage, error) {
	p := newSearchParams("", 10, opts...)

	if !validateID("MEMORY_RETRIEVE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_RETRIEVE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Str("memory_type", "message").
			Msg("Invalid scope_id format.")
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "message"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	if m.messageManager == nil {
		return nil, nil
	}

	msgAndMetas, err := m.messageManager.Get(ctx, p.UserID, p.ScopeID, "", p.Num)
	if err != nil {
		return nil, err
	}

	var recentMessages []llmschema.BaseMessage
	for _, mm := range msgAndMetas {
		recentMessages = append(recentMessages, mm.Message)
	}
	return recentMessages, nil
}

// GetMessageByID 按 ID 获取消息。
//
// 对齐 Python: LongTermMemory.get_message_by_id(msg_id)
func (m *LongTermMemory) GetMessageByID(ctx context.Context, msgID string) (*llmschema.BaseMessage, *time.Time, error) {
	if m.messageManager == nil {
		logger.Warn(logComponent).Str("event_type", "MEMORY_RETRIEVE").
			Str("memory_type", "message").Str("memory_id", msgID).
			Msg("Message manager is not initialized.")
		return nil, nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "message"),
			exception.WithMsg("message manager is not initialized"),
		)
	}

	msgAndMeta, err := m.messageManager.GetByID(ctx, msgID)
	if err != nil {
		return nil, nil, err
	}
	if msgAndMeta == nil {
		return nil, nil, nil
	}

	msg := msgAndMeta.Message
	var ts *time.Time
	if msgAndMeta.Metadata != nil {
		ts = &msgAndMeta.Metadata.Timestamp
	}

	return &msg, ts, nil
}

// UserMemTotalNum 返回用户记忆总数。
//
// 对齐 Python: LongTermMemory.user_mem_total_num(user_id, scope_id)
func (m *LongTermMemory) UserMemTotalNum(
	ctx context.Context,
	opts ...SearchOption,
) (int, error) {
	p := newSearchParams("", 0, opts...)

	if !validateID("MEMORY_RETRIEVE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_RETRIEVE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Msg("Invalid scope_id format.")
		return 0, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	if m.searchManager == nil {
		return 0, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("search manager is not initialized"),
		)
	}

	searchData, err := m.searchManager.ListUserProfile(ctx, p.UserID, p.ScopeID)
	if err != nil {
		return 0, err
	}
	return len(searchData), nil
}

// GetUserMemByPage 分页获取用户记忆。
//
// 对齐 Python: LongTermMemory.get_user_mem_by_page(user_id, scope_id, page_size, page_idx, memory_type)
func (m *LongTermMemory) GetUserMemByPage(
	ctx context.Context,
	pageSize int,
	pageIdx int,
	memoryType mem_model.MemoryType,
	opts ...SearchOption,
) ([]*MemInfo, error) {
	p := newSearchParams("", 0, opts...)

	if !validateID("MEMORY_RETRIEVE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_RETRIEVE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Msg("Invalid scope_id format.")
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	if m.searchManager == nil {
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("search manager is not initialized"),
		)
	}

	var searchMemoryType string
	if memoryType != mem_model.MemoryTypeUnknown {
		searchMemoryType = memoryType.String()
	}

	searchData, err := m.searchManager.ListUserMem(ctx, p.UserID, p.ScopeID, pageSize, pageIdx, searchMemoryType)
	if err != nil {
		return nil, err
	}

	if len(searchData) == 0 {
		return nil, nil
	}

	memResults := make([]*MemInfo, 0, len(searchData))
	for _, item := range searchData {
		memType := mem_model.MemoryTypeUnknown
		if item.Doc != nil {
			memType = mem_model.ParseMemoryType(item.Doc.Type)
		}
		memResults = append(memResults, &MemInfo{
			MemID:   item.Doc.ID,
			Content: item.Doc.Text,
			Type:    memType,
		})
	}
	return memResults, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
