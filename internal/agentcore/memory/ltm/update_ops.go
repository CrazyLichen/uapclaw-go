package ltm

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// UpdateMemByID 按 ID 更新记忆内容。
//
// ① emit_before MEMORY_UPDATED → ② 分布式锁 → applyScopeEmbedding → writeManager.UpdateMemByID
// 对齐 Python: @_fw.emit_before(MemoryEvents.MEMORY_UPDATED) + update_mem_by_id
func (m *LongTermMemory) UpdateMemByID(
	ctx context.Context,
	memID string,
	memory string,
	opts ...UserScopeOption,
) error {
	p := newUserScopeParams(opts...)
	// ① 触发 MEMORY_UPDATED 回调
	triggerMemoryBefore(ctx, callback.MemoryUpdated, &callback.MemoryEventData{
		Event:    callback.MemoryUpdated,
		UserID:   p.UserID,
		ScopeID:  p.ScopeID,
		MemoryID: memID,
	})

	// ② 执行更新
	return m.updateMemByIDImpl(ctx, memID, memory, p)
}

// UpdateVariables 更新用户变量。
//
// 对齐 Python: LongTermMemory.update_variables(variables, user_id, scope_id)
func (m *LongTermMemory) UpdateVariables(
	ctx context.Context,
	variables map[string]string,
	opts ...UserScopeOption,
) error {
	p := newUserScopeParams(opts...)

	if !validateID("MEMORY_UPDATE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_UPDATE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Str("memory_type", "variable").
			Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemoryUpdateMemoryExecutionError,
			exception.WithParam("memory_type", "variable"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	return acquireUserLock(ctx, m.kvStore, p.UserID, func(ctx context.Context) error {
		if m.variableManager == nil {
			return exception.BuildError(exception.StatusMemoryUpdateMemoryExecutionError,
				exception.WithParam("memory_type", "variable"),
				exception.WithMsg("variable manager is not initialized"),
			)
		}
		for name, value := range variables {
			if err := m.variableManager.UpdateUserVariable(ctx, p.UserID, p.ScopeID, name, value); err != nil {
				logger.Error(logComponent).Err(err).Str("name", name).
					Msg("更新变量失败")
			}
		}
		return nil
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// updateMemByIDImpl UpdateMemByID 的核心实现。
func (m *LongTermMemory) updateMemByIDImpl(ctx context.Context, memID string, memory string, p *userScopeParams) error {
	if !validateID("MEMORY_UPDATE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_UPDATE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Str("memory_id", memID).
			Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemoryUpdateMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	return acquireUserLock(ctx, m.kvStore, p.UserID, func(ctx context.Context) error {
		if m.writeManager == nil {
			return exception.BuildError(exception.StatusMemoryUpdateMemoryExecutionError,
				exception.WithParam("memory_type", "all"),
				exception.WithMsg("write manager is not initialized"),
			)
		}
		m.applyScopeEmbedding(ctx, p.ScopeID)
		return m.writeManager.UpdateMemByID(ctx, p.UserID, p.ScopeID, memID, memory)
	})
}
