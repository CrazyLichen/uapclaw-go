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

// DeleteMemByID 按 ID 删除记忆。
//
// ① emit_before MEMORY_DELETED → ② 分布式锁 → writeManager.DeleteMemByID
// 对齐 Python: @_fw.emit_before(MemoryEvents.MEMORY_DELETED) + delete_mem_by_id
func (m *LongTermMemory) DeleteMemByID(
	ctx context.Context,
	memID string,
	opts ...UserScopeOption,
) error {
	p := newUserScopeParams(opts...)
	// ① 触发 MEMORY_DELETED 回调
	triggerMemoryBefore(ctx, callback.MemoryDeleted, &callback.MemoryEventData{
		Event:      callback.MemoryDeleted,
		UserID:     p.UserID,
		ScopeID:    p.ScopeID,
		MemoryID:   memID,
		MemoryType: "all",
	})

	// ② 执行删除
	return m.deleteMemByIDImpl(ctx, memID, p)
}

// DeleteMemByUserID 按用户 ID 删除记忆。
//
// ① emit_before MEMORY_DELETED → ② 分布式锁 → writeManager.DeleteMemByUserID
// 对齐 Python: @_fw.emit_before(MemoryEvents.MEMORY_DELETED) + delete_mem_by_user_id
func (m *LongTermMemory) DeleteMemByUserID(
	ctx context.Context,
	opts ...UserScopeOption,
) error {
	p := newUserScopeParams(opts...)
	// ① 触发 MEMORY_DELETED 回调
	triggerMemoryBefore(ctx, callback.MemoryDeleted, &callback.MemoryEventData{
		Event:      callback.MemoryDeleted,
		UserID:     p.UserID,
		ScopeID:    p.ScopeID,
		MemoryType: "all",
	})

	// ② 执行删除
	return m.deleteMemByUserIDImpl(ctx, p)
}

// DeleteMemByScope 按 scope 删除所有记忆。
// 遍历 scope_user_mapping → 逐用户分布式锁 → writeManager.DeleteMemByUserID → 删除 mapping。
//
// 对齐 Python: LongTermMemory.delete_mem_by_scope(scope_id)
func (m *LongTermMemory) DeleteMemByScope(ctx context.Context, scopeID string) error {
	if !validateID("MEMORY_DELETE", scopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_DELETE").
			Str("scope_id", scopeID).Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	scopeUserData, err := m.scopeUserMappingManager.GetByScopeID(ctx, scopeID)
	if err != nil {
		return err
	}
	userIDs := make([]string, 0, len(scopeUserData))
	for _, data := range scopeUserData {
		if uid, ok := data["user_id"]; ok {
			// 安全类型断言：user_id 字段由 ScopeUserMappingManager.Add 写入，类型为 string
			if s, ok := uid.(string); ok {
				userIDs = append(userIDs, s)
			} else {
				// M-05: 类型断言失败时记录 Warn 日志
				logger.Warn(logComponent).Str("event_type", "MEMORY_DELETE").
					Str("scope_id", scopeID).
					Interface("user_id", uid).
					Msg("DeleteMemByScope: user_id 类型断言失败，跳过该条目")
			}
		}
	}

	if m.writeManager != nil {
		for _, userID := range userIDs {
			lockErr := acquireUserLock(ctx, m.kvStore, userID, func(ctx context.Context) error {
				return m.writeManager.DeleteMemByUserID(ctx, userID, scopeID)
			})
			if lockErr != nil {
				return lockErr
			}
		}
	}

	if m.scopeUserMappingManager != nil {
		if delErr := m.scopeUserMappingManager.DeleteByScopeID(ctx, scopeID); delErr != nil {
			return delErr
		}
	}

	logger.Debug(logComponent).Str("event_type", "MEMORY_DELETE").
		Str("scope_id", scopeID).Msg("Successfully deleted memories.")
	return nil
}

// DeleteMessagesByUserAndScope 按用户和 scope 删除消息。
//
// 对齐 Python: LongTermMemory.delete_messages_by_user_and_scope(user_id, scope_id)
func (m *LongTermMemory) DeleteMessagesByUserAndScope(
	ctx context.Context,
	opts ...UserScopeOption,
) error {
	p := newUserScopeParams(opts...)

	if !validateID("MEMORY_RETRIEVE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_RETRIEVE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Str("memory_type", "message").
			Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
			exception.WithParam("memory_type", "message"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	if m.messageManager != nil {
		_, err := m.messageManager.DeleteByUserAndScope(ctx, p.UserID, p.ScopeID)
		return err
	}
	return nil
}

// DeleteVariables 删除用户变量。
//
// 对齐 Python: LongTermMemory.delete_variables(names, user_id, scope_id)
func (m *LongTermMemory) DeleteVariables(
	ctx context.Context,
	names []string,
	opts ...UserScopeOption,
) error {
	p := newUserScopeParams(opts...)

	if !validateID("MEMORY_DELETE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_DELETE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Str("memory_type", "variable").
			Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
			exception.WithParam("memory_type", "variable"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	return acquireUserLock(ctx, m.kvStore, p.UserID, func(ctx context.Context) error {
		if m.variableManager == nil {
			return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
				exception.WithParam("memory_type", "variable"),
				exception.WithMsg("variable manager is not initialized"),
			)
		}
		for _, name := range names {
			if err := m.variableManager.DeleteUserVariable(ctx, p.UserID, p.ScopeID, name); err != nil {
				return err
			}
		}
		return nil
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deleteMemByIDImpl DeleteMemByID 的核心实现。
func (m *LongTermMemory) deleteMemByIDImpl(ctx context.Context, memID string, p *userScopeParams) error {
	if !validateID("MEMORY_DELETE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_DELETE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Str("memory_id", memID).
			Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	return acquireUserLock(ctx, m.kvStore, p.UserID, func(ctx context.Context) error {
		if m.writeManager == nil {
			return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
				exception.WithParam("memory_type", "all"),
				exception.WithMsg("write manager is not initialized"),
			)
		}
		return m.writeManager.DeleteMemByID(ctx, p.UserID, p.ScopeID, memID)
	})
}

// deleteMemByUserIDImpl DeleteMemByUserID 的核心实现。
func (m *LongTermMemory) deleteMemByUserIDImpl(ctx context.Context, p *userScopeParams) error {
	if !validateID("MEMORY_DELETE", p.ScopeID) {
		logger.Error(logComponent).Str("event_type", "MEMORY_DELETE").
			Str("user_id", p.UserID).Str("scope_id", p.ScopeID).
			Msg("Invalid scope_id format.")
		return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
			exception.WithParam("memory_type", "all"),
			exception.WithMsg("invalid scope_id format"),
		)
	}

	return acquireUserLock(ctx, m.kvStore, p.UserID, func(ctx context.Context) error {
		if m.writeManager == nil {
			return exception.BuildError(exception.StatusMemoryDeleteMemoryExecutionError,
				exception.WithParam("memory_type", "all"),
				exception.WithMsg("write manager is not initialized"),
			)
		}
		return m.writeManager.DeleteMemByUserID(ctx, p.UserID, p.ScopeID)
	})
}
