package archive

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// StartRecovery 启动恢复循环（幂等）。
// Python: session_archive.py start_recovery / _recover
//
// 每轮扫生命周期资源，凡未完成、可重试且退避时刻已到的会话重新拉起收尾。
// 进程重启后残留的删除/归档都靠它做完。
func (s *Service) StartRecovery(ctx context.Context) {
	s.mu.Lock()
	if s.recoveryActive {
		s.mu.Unlock()
		return
	}
	s.recoveryActive = true
	s.mu.Unlock()

	// 启动时先做一次 archived_at 回填：列表只读取值，历史会话需要这一次补写
	// （Python: session_archive.py start_recovery 里 asyncio.to_thread(self._backfill_archive_times)）
	s.BackfillArchiveTimes()

	go func() {
		ticker := time.NewTicker(recoveryIdleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.recoveryPoke:
			case <-ticker.C:
			}
			s.mu.Lock()
			active := s.recoveryActive
			s.mu.Unlock()
			if !active {
				return
			}
			s.recoverOnce(ctx)
		}
	}()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recoverOnce 执行一轮恢复扫描。
// Python: session_archive.py _recover
func (s *Service) recoverOnce(ctx context.Context) {
	snapshots, err := lifecycle.EventSnapshots()
	if err != nil {
		return
	}
	now := float64(time.Now().UnixNano()) / 1e9
	for _, snapshot := range snapshots {
		event, _ := snapshot["event"].(string)
		if event != "session.lifecycle.updated" {
			continue
		}
		payload, _ := snapshot["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		sessionID, _ := payload["session_id"].(string)
		if sessionID == "" {
			continue
		}
		// 已终结的不再拉起
		if completed, _ := snapshot["completed"].(bool); completed {
			continue
		}
		operation, _ := lifecycle.State("session", sessionID)["operation"].(map[string]any)
		if operation == nil {
			continue
		}
		if status := operationStatus(operation); status == lifecycle.OperationStatusAbandoned {
			continue
		}
		// 不可重试的失败不自动重跑
		if retryable, exists := operation["retryable"]; exists {
			if flag, isBool := retryable.(bool); isBool && !flag {
				continue
			}
		}
		if nextAt := asFloatValue(operation["cleanup_next_at"]); nextAt > 0 && now < nextAt {
			continue
		}
		s.mu.Lock()
		_, running := s.finalizing[sessionID]
		s.mu.Unlock()
		if running {
			continue
		}
		action := operationKind(operation)
		if action != operationKindDelete && action != operationKindArchive {
			continue
		}
		logger.Info(logComponent).Str("session_id", sessionID).Str("action", action).
			Msg("session lifecycle recovery resumed")
		s.spawnFinalizer(sessionID, action,
			stringOf(operation, "channel_id"),
			stringOf(operation, "project_id"),
			generationFromOperation(operation))
	}
}

// recordFinalizeFailure 记录一次收尾失败（决定重试或放弃）。
// Python: session_archive.py _record_finalize_failure（:784）
func (s *Service) recordFinalizeFailure(
	sessionID, action string, attempts int, err error, operationID string,
) {
	code := ""
	if lifecycleErr, ok := err.(*lifecycle.LifecycleError); ok {
		code = lifecycleErr.Code
	}
	if _, nonRetryable := nonRetryableCodes[code]; nonRetryable || attempts >= MaxCleanupAttempts {
		s.abandonCleanup(sessionID, action, attempts, err.Error(), operationID)
		return
	}
	nextAt := float64(time.Now().UnixNano())/1e9 + CleanupBackoff(attempts)
	if s.saveCleanupFailure(sessionID, operationID, err.Error(), &attempts, nextAt, false) == nil {
		return
	}
	logger.Warn(logComponent).Err(err).
		Str("session_id", sessionID).Str("action", action).
		Int("attempt", attempts).Float64("retry_at", nextAt).
		Msg("session cleanup failed")
	s.pokeRecovery()
}

// saveCleanupFailure 在同一把锁内比对并落盘一次失败。
// Python: session_archive.py _save_cleanup_failure（:746）
//
// attempts 必须随失败一起落盘，不能只留局部变量：占用路径（拿不到跨进程锁）
// 永远进不到 finalizeSessionOnce，而那是唯一写 cleanup_attempts 的地方。
// 不落盘时 attempts 恒为 1，MaxCleanupAttempts 永不达成、abandonCleanup
// 不可达，收尾退化成无限重试。
//
// 返回落盘后的操作；操作已被替换或已终结时返回 nil。
func (s *Service) saveCleanupFailure(
	sessionID, operationID, reason string, attempts *int, nextAt float64, exhausted bool,
) map[string]any {
	release, err := s.lockWait("session", sessionID)
	if err != nil {
		return nil
	}
	defer release()

	operation, _ := lifecycle.State("session", sessionID)["operation"].(map[string]any)
	if operation == nil || stringOf(operation, "operation_id") != operationID {
		return nil
	}
	status := operationStatus(operation)
	if status == lifecycle.OperationStatusCompleted || status == lifecycle.OperationStatusAbandoned {
		return nil
	}
	newStatus := lifecycle.OperationStatusFailed
	if exhausted {
		newStatus = lifecycle.OperationStatusAbandoned
	}
	changes := map[string]any{
		"status":            newStatus,
		"stop_pending":      false,
		"retryable":         !exhausted,
		"cleanup_next_at":   nextAt,
		"cleanup_exhausted": exhausted,
		"errors":            []any{reason},
	}
	if attempts != nil {
		// 单调不减：并发/重入时不要把已记下的次数写回更小的值
		stored := asIntValue(operation["cleanup_attempts"])
		if *attempts > stored {
			changes["cleanup_attempts"] = *attempts
		} else {
			changes["cleanup_attempts"] = stored
		}
	}
	updated, updateErr := lifecycle.Update("session", sessionID, changes)
	if updateErr != nil {
		return nil
	}
	return updated
}

// abandonCleanup 放弃收尾（达到次数上限或遇不可重试错误）。
// Python: session_archive.py _abandon_cleanup（:825）
func (s *Service) abandonCleanup(
	sessionID, action string, attempts int, reason, operationID string,
) {
	updated := s.saveCleanupFailure(sessionID, operationID, reason, &attempts, 0, true)
	if updated == nil {
		return
	}
	logger.Warn(logComponent).Str("session_id", sessionID).Str("action", action).
		Int("attempts", attempts).Str("reason", reason).
		Msg("session cleanup abandoned after attempt limit")
}
