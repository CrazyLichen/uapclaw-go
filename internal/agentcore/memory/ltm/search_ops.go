package ltm

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/search"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// SearchUserMem 搜索用户记忆。
//
// ① emit_before MEMORY_SEARCH_STARTED → ② searchUserMemImpl → ③ trigger SEARCH_FINISHED
// 对齐 Python: @_fw.emit_before(MemoryEvents.MEMORY_SEARCH_STARTED) + search_user_mem
func (m *LongTermMemory) SearchUserMem(
	ctx context.Context,
	query string,
	num int,
	opts ...SearchOption,
) ([]*MemResult, error) {
	p := newSearchParams(query, num, opts...)
	// ① 触发 MEMORY_SEARCH_STARTED 回调
	triggerMemoryBefore(ctx, callback.MemorySearchStarted, &callback.MemoryEventData{
		Event:      callback.MemorySearchStarted,
		UserID:     p.UserID,
		ScopeID:    p.ScopeID,
		Query:      p.Query,
		SearchType: "user_mem",
	})

	// ② 执行搜索逻辑
	results, err := m.searchUserMemImpl(ctx, p, m.fragmentType)
	if err != nil {
		return nil, err
	}

	// ③ 触发 MEMORY_SEARCH_FINISHED 回调
	triggerMemoryAfter(ctx, callback.MemorySearchFinished, &callback.MemoryEventData{
		Event:       callback.MemorySearchFinished,
		UserID:      p.UserID,
		ScopeID:     p.ScopeID,
		Query:       p.Query,
		SearchType:  "user_mem",
		ResultCount: len(results),
	})

	return results, nil
}

// SearchUserHistorySummary 搜索用户历史摘要。
//
// ① emit_before MEMORY_SEARCH_STARTED → ② searchUserMemImpl → ③ trigger SEARCH_FINISHED
// 对齐 Python: @_fw.emit_before(MemoryEvents.MEMORY_SEARCH_STARTED) + search_user_history_summary
func (m *LongTermMemory) SearchUserHistorySummary(
	ctx context.Context,
	query string,
	num int,
	opts ...SearchOption,
) ([]*MemResult, error) {
	p := newSearchParams(query, num, opts...)
	// ① 触发 MEMORY_SEARCH_STARTED 回调
	triggerMemoryBefore(ctx, callback.MemorySearchStarted, &callback.MemoryEventData{
		Event:      callback.MemorySearchStarted,
		UserID:     p.UserID,
		ScopeID:    p.ScopeID,
		Query:      p.Query,
		SearchType: "history_summary",
	})

	// ② 执行搜索逻辑（搜索类型限 SUMMARY）
	results, err := m.searchUserMemImpl(ctx, p, []string{mem_model.MemoryTypeSummary.String()})
	if err != nil {
		return nil, err
	}

	// ③ 触发 MEMORY_SEARCH_FINISHED 回调
	triggerMemoryAfter(ctx, callback.MemorySearchFinished, &callback.MemoryEventData{
		Event:       callback.MemorySearchFinished,
		UserID:      p.UserID,
		ScopeID:     p.ScopeID,
		Query:       p.Query,
		SearchType:  "history_summary",
		ResultCount: len(results),
	})

	return results, nil
}

// GetVariables 获取用户变量。
//
// names 为变量名列表：
//   - nil / 空切片: 返回所有变量
//   - 单元素: 返回单个变量
//   - 多元素: 返回多个变量
//
// 返回值为 map[变量名]变量值。
//
// Python: LongTermMemory.get_variables(names, user_id, scope_id)
// Python names 类型为 Union[str, list[str], None]，Go 用 []string 统一表达三种语义。
func (m *LongTermMemory) GetVariables(
	ctx context.Context,
	names []string,
	opts ...UserScopeOption,
) (map[string]string, error) {
	// 解析默认参数
	p := newUserScopeParams(opts...)

	if !validateID("MEMORY_RETRIEVE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_RETRIEVE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Str("memory_type", mem_model.MemoryTypeVariable.String()).
			Msg("Invalid scope_id format.")
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", mem_model.MemoryTypeVariable.String()),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	if m.searchManager == nil {
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("search manager is not initialized"),
		)
	}

	// nil / 空切片 → 返回所有变量
	if len(names) == 0 {
		return m.searchManager.GetAllUserVariable(ctx, p.UserID, p.ScopeID)
	}

	ret := make(map[string]string, len(names))
	for _, name := range names {
		value, err := m.searchManager.GetUserVariable(ctx, p.UserID, p.ScopeID, name)
		if err != nil {
			return nil, err
		}
		ret[name] = value
	}
	return ret, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// searchUserMemImpl 搜索用户记忆的核心实现。
// 对齐 Python: search_user_mem / search_user_history_summary 的核心逻辑
func (m *LongTermMemory) searchUserMemImpl(ctx context.Context, p *searchParams, searchTypes []string) ([]*MemResult, error) {
	if !validateID("MEMORY_RETRIEVE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_RETRIEVE").
			Str("query", p.Query).Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Msg("Invalid scope_id format.")
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "user_mem"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	if m.searchManager == nil {
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("search manager is not initialized"),
		)
	}

	m.applyScopeEmbedding(ctx, p.ScopeID)

	params := &search.SearchParams{
		Query:      p.Query,
		ScopeID:    p.ScopeID,
		TopK:       p.Num,
		UserID:     p.UserID,
		Threshold:  p.Threshold,
		SearchType: searchTypes,
	}

	searchData, err := m.searchManager.Search(ctx, params)
	if err != nil {
		// S-04: 对齐 Python，按错误类型分类日志级别
		var baseErr *exception.BaseError
		if errors.As(err, &baseErr) && baseErr.Category() == exception.ErrorCategoryValidation {
			// Python ValueError → Warning 级别
			logger.Warn(logComponent).Err(err).Str("event_type", "LLM_CALL_ERROR").
				Str("method", "SearchUserMem").Str("user_id", p.UserID).
				Str("scope_id", p.ScopeID).Str("query", p.Query).
				Msg("Search user mem has ValueError-like exception.")
		} else {
			// Python Exception → Error 级别
			logger.Error(logComponent).Err(err).Str("event_type", "LLM_CALL_ERROR").
				Str("method", "SearchUserMem").Str("user_id", p.UserID).
				Str("scope_id", p.ScopeID).Str("query", p.Query).
				Msg("Search user mem has exception.")
		}
		return nil, exception.BuildError(exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "user_mem"),
			exception.WithMsg(fmt.Sprintf("%v", err)),
			exception.WithCause(err),
		)
	}

	// 排序截断
	sort.Slice(searchData, func(i, j int) bool {
		return searchData[i].Score > searchData[j].Score
	})
	if len(searchData) > p.Num {
		searchData = searchData[:p.Num]
	}

	// 构建 MemResult 列表
	memResults := make([]*MemResult, 0, len(searchData))
	for _, item := range searchData {
		memType := mem_model.MemoryTypeUnknown
		if item.Doc != nil {
			memType = mem_model.ParseMemoryType(item.Doc.Type)
		}
		memResults = append(memResults, &MemResult{
			MemInfo: &MemInfo{
				MemID:     item.Doc.ID,
				Content:   item.Doc.Text,
				Type:      memType,
				Timestamp: &item.Doc.Timestamp,
			},
			Score: item.Score,
		})
	}

	return memResults, nil
}
