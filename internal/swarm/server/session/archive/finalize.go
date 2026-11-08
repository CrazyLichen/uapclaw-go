package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// spawnFinalizer 启动（或复用）某会话的后台收尾。
// Python: session_archive.py _spawn_finalizer（:411）
//
// 复用条件：同会话已有在飞任务且操作标识相同。操作标识不同说明上一个
// 操作已被撤销而新操作在等名额，此时旧任务必须取消——否则它会以旧意图
// 去动新操作的数据。
func (s *Service) spawnFinalizer(
	sessionID, action, channelID, projectID string, generation *int,
) {
	operation, _ := lifecycle.State("session", sessionID)["operation"].(map[string]any)
	operationID := stringOf(operation, "operation_id")

	s.mu.Lock()
	if _, exists := s.finalizing[sessionID]; exists {
		if s.finalizingOperations[sessionID] == operationID {
			s.mu.Unlock()
			return
		}
		if cancel := s.finalizing[sessionID]; cancel != nil {
			cancel()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.finalizing[sessionID] = cancel
	s.finalizingOperations[sessionID] = operationID
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.finalizing, sessionID)
			delete(s.finalizingOperations, sessionID)
			s.mu.Unlock()
			cancel()
		}()
		select {
		case s.finalizeSem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-s.finalizeSem }()
		s.finalizeSession(ctx, sessionID, action, channelID, projectID, generation, operationID)
	}()
}

// finalizeSession 带占用重试预算的收尾入口。
// Python: session_archive.py _finalize_session（:468）
//
// 占用错误（OPERATION_IN_PROGRESS）重试 40 次 × 50ms：接受请求可能还在
// 释放自己的锁，短暂等待比直接放弃更合适；重试预算耗尽也算一次失败，
// 让它走退避与次数上限，而不是每 10 秒被恢复循环无限重拉。
func (s *Service) finalizeSession(
	ctx context.Context, sessionID, action, channelID, projectID string,
	generation *int, operationID string,
) {
	var busyErr error
	for attempt := 0; attempt < maxBusyRetries; attempt++ {
		err := s.finalizeSessionOnce(ctx, sessionID, action, channelID, projectID,
			generation, operationID)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			// 关闭或调用方取消：留可重试标记，交给下一个进程或恢复循环
			s.saveCleanupFailure(sessionID, operationID, "operation cancelled", nil, 0, false)
			s.pokeRecovery()
			return
		}
		if isOperationInProgress(err) {
			busyErr = err
			time.Sleep(busyRetrySleep)
			continue
		}
		attempts := currentCleanupAttempts(sessionID) + 1
		s.recordFinalizeFailure(sessionID, action, attempts, err, operationID)
		return
	}
	logger.Warn(logComponent).Str("session_id", sessionID).
		Msg("session finalizer deferred by a live operation")
	attempts := currentCleanupAttempts(sessionID) + 1
	if busyErr == nil {
		busyErr = fmt.Errorf("session operation still in progress")
	}
	s.recordFinalizeFailure(sessionID, action, attempts, busyErr, operationID)
	s.pokeRecovery()
}

// finalizeSessionOnce 一次收尾尝试（加会话锁）。
// Python: session_archive.py _finalize_session_locked（:552）
func (s *Service) finalizeSessionOnce(
	ctx context.Context, sessionID, action, channelID, projectID string,
	generation *int, operationID string,
) error {
	// 取消即中止：上游在 CancelledError 分支留可重试标记，
	// 让下一个进程（或恢复循环）接着做完物理清理。
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	release, err := s.lockWait("session", sessionID)
	if err != nil {
		return err
	}
	defer release()

	value := lifecycle.State("session", sessionID)
	operation, _ := value["operation"].(map[string]any)
	if operation == nil {
		return nil
	}
	// 操作已被替换或已终结：本次尝试作废（不报错，因为不是失败）
	if stringOf(operation, "operation_id") != operationID {
		return nil
	}
	status := operationStatus(operation)
	if status == lifecycle.OperationStatusCompleted || status == lifecycle.OperationStatusAbandoned {
		return nil
	}
	if operationKind(operation) != action {
		return nil
	}
	// 动手之前先记次数：反复在清理中途崩溃的进程也能耗尽预算，
	// 而不是跨重启永远循环。
	attempts := asIntValue(operation["cleanup_attempts"]) + 1
	if attempts > MaxCleanupAttempts {
		s.abandonCleanup(sessionID, action, attempts, "cleanup attempt limit reached", operationID)
		return nil
	}
	cleanupOnly := action == operationKindDelete && readBoolField(operation, "runtime_deleted")
	phase := "stop_sessions"
	if cleanupOnly {
		phase = "cleanup_mailbox"
	}
	if _, updateErr := lifecycle.Update("session", sessionID, map[string]any{
		"status":           lifecycle.OperationStatusRunning,
		"phase":            phase,
		"stop_pending":     !cleanupOnly,
		"cleanup_attempts": attempts,
		"cleanup_next_at":  0,
	}); updateErr != nil {
		return updateErr
	}
	if bodyErr := s.finalizeSessionBody(ctx, sessionID, action,
		firstNonEmpty(stringOf(operation, "channel_id"), channelID),
		firstNonEmpty(stringOf(operation, "project_id"), projectID),
		generationFromOperationOrDefault(operation, generation),
		operation); bodyErr != nil {
		s.recordFinalizeFailure(sessionID, action, attempts, bodyErr, operationID)
	}
	return nil
}

// finalizeSessionBody 收尾主体：停运行时 → 刷写 → 加围栏 → 删目录/移动。
// Python: session_archive.py _finalize_session_body（:607）
func (s *Service) finalizeSessionBody(
	ctx context.Context, sessionID, action, channelID, projectID string,
	generation *int, operation map[string]any,
) error {
	// 上次已删掉运行时：直接补完邮箱与完成标记，不重复破坏性动作
	if action == operationKindDelete && readBoolField(operation, "runtime_deleted") {
		return s.completeSessionDelete(ctx, sessionID, projectID)
	}
	active, archived, pathErr := lifecycle.SessionPaths(sessionID, "")
	if pathErr != nil {
		return pathErr
	}
	meta, metaErr := lifecycle.RawMetadata(sessionID)
	if metaErr != nil {
		meta = map[string]any{}
	}
	ownerChannel := firstNonEmpty(stringOf(meta, "channel_id"), channelID)

	if action == operationKindDelete {
		// 接受阶段只挡新工作；等一个不理会取消的消费者属于后台，不占请求路径
		if mailbox := s.mailboxOrNil(); mailbox != nil {
			if beginErr := mailbox.BeginTargetDelete(ctx, sessionID); beginErr != nil {
				logger.Warn(logComponent).Err(beginErr).Str("session_id", sessionID).
					Msg("session cleanup could not settle mailbox")
			}
		}
	}
	if pathExists(active) {
		s.stopHeartbeat(ctx, sessionID)
		s.stopSubagents(ctx, sessionID, action, ownerChannel)
		running := true
		s.mu.Lock()
		probe := s.runningProbe
		s.mu.Unlock()
		if probe != nil {
			running = probe.IsSessionRunning(sessionID)
		}
		// 删除始终排空写入路径：即便没有执行被标为 RUNNING，
		// 孤儿团队流与常驻工作仍占着必须在 rmtree 前释放的资源。
		if running || generation != nil || action == operationKindDelete {
			if stopErr := s.stopSessionForLifecycle(ctx, ownerChannel, sessionID,
				generation, action == operationKindArchive); stopErr != nil {
				return stopErr
			}
		}
	}

	if _, updateErr := lifecycle.Update("session", sessionID,
		map[string]any{"phase": "flush_writes"}); updateErr != nil {
		return updateErr
	}
	if !s.flushWrites() {
		// 已接受的写入现在只能尽力而为；目录操作仍要尝试，
		// 否则一个卡住的写者能永久阻塞用户。
		logger.Warn(logComponent).Str("session_id", sessionID).Str("action", action).
			Msg("session cleanup flushed with pending writes")
	}
	if _, updateErr := lifecycle.Update("session", sessionID,
		map[string]any{"phase": "fence_writes"}); updateErr != nil {
		return updateErr
	}
	if fenceErr := lifecycle.FenceWrites("session", sessionID); fenceErr != nil {
		return fenceErr
	}

	if action == operationKindDelete {
		if _, updateErr := lifecycle.Update("session", sessionID,
			map[string]any{"phase": "delete_directory"}); updateErr != nil {
			return updateErr
		}
		if s.runtime == nil {
			return &lifecycle.LifecycleError{
				Code: "DELETE_FAILED", Message: "session runtime is unavailable"}
		}
		if deleteErr := s.runtime.DeleteSession(ctx, ownerChannel, sessionID); deleteErr != nil {
			return deleteErr
		}
		// 先落这个边界再清邮箱：删除后的重试不该重复破坏性 Runtime 动作
		if _, updateErr := lifecycle.Update("session", sessionID, map[string]any{
			"phase":           "cleanup_mailbox",
			"runtime_deleted": true,
			"stop_pending":    false,
		}); updateErr != nil {
			return updateErr
		}
		return s.completeSessionDelete(ctx, sessionID, projectID)
	}

	// 归档：移动目录 + 清缓存 + 可选重排置顶
	if _, updateErr := lifecycle.Update("session", sessionID,
		map[string]any{"phase": "move_directory"}); updateErr != nil {
		return updateErr
	}
	archivedAt := asFloatValue(operation["archived_at"])
	if archivedAt <= 0 {
		archivedAt = float64(time.Now().UnixNano()) / 1e9
	}
	if moveErr := s.MoveSessionDirectory(sessionID, active, archived, archivedAt); moveErr != nil {
		return moveErr
	}
	s.mu.Lock()
	removeCache := s.removeMetadataCache
	reindex := s.reindexPins
	s.mu.Unlock()
	if removeCache != nil {
		removeCache(sessionID)
	}
	if readBoolField(operation, "pin_reindex_required") && reindex != nil {
		if reindexErr := reindex(); reindexErr != nil {
			logger.Warn(logComponent).Err(reindexErr).Str("session_id", sessionID).
				Msg("session archive could not reindex pins")
		}
	}
	payload := map[string]any{
		"session_id":   sessionID,
		"ok":           true,
		"archived":     true,
		"archived_at":  archivedAt,
		"stop_pending": false,
		"project_id":   projectID,
	}
	return lifecycle.Complete("session", sessionID, true, false, payload)
}

// completeSessionDelete 完成删除：清邮箱 → 落完成标记 → 通知推荐系统。
// Python: session_archive.py _complete_session_delete（:728）
func (s *Service) completeSessionDelete(
	ctx context.Context, sessionID, projectID string,
) error {
	if mailbox := s.mailboxOrNil(); mailbox != nil {
		if onDeletedErr := mailbox.OnTargetDeleted(ctx, sessionID); onDeletedErr != nil {
			logger.Warn(logComponent).Err(onDeletedErr).Str("session_id", sessionID).
				Msg("session cleanup could not finalize mailbox")
		}
	}
	payload := map[string]any{
		"session_id": sessionID,
		"ok":         true,
		"project_id": projectID,
	}
	if completeErr := lifecycle.Complete("session", sessionID, false, true, payload); completeErr != nil {
		return completeErr
	}
	s.mu.Lock()
	hook := s.sessionHasLeftProject
	s.mu.Unlock()
	if hook != nil {
		if hookErr := hook(ctx, projectID, sessionID); hookErr != nil {
			logger.Warn(logComponent).Err(hookErr).Str("session_id", sessionID).
				Msg("session delete could not notify recommendation host")
		}
	}
	return nil
}

// stopHeartbeat 停止会话后台心跳。
// Python: session_archive.py _stop_heartbeat（:322）
//
// 活的心跳轮不能把归档/删除变成 busy 拒绝：先停轮，再读已静默的会话。
// 失败只记日志，让拒绝取消的轮次仍走普通 busy 检查。
func (s *Service) stopHeartbeat(ctx context.Context, sessionID string) {
	s.mu.Lock()
	stopper := s.heartbeatStopper
	s.mu.Unlock()
	if stopper == nil {
		return
	}
	if err := stopper.StopHeartbeatRuns(ctx, sessionID); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("session lifecycle could not stop heartbeat runs")
	}
}

// stopSubagents 释放会话常驻子代理。
// Python: session_archive.py _stop_subagents（:340）
//
// 子代理会一直常驻直到被显式释放，于是能活过用户自己的停止动作、
// 让会话看起来仍然 busy——用户被要求去停一个已经停过的会话。
func (s *Service) stopSubagents(ctx context.Context, sessionID, action, channelID string) {
	s.mu.Lock()
	stopper := s.subagentStopper
	s.mu.Unlock()
	if stopper == nil {
		return
	}
	reason := "session_deleted"
	if action == operationKindArchive {
		reason = "session_archived"
	}
	if err := stopper.StopSubagentRuntimes(ctx, sessionID, channelID, reason); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("session lifecycle could not release subagents")
	}
}

// stopSessionForLifecycle 按代次停止会话执行。
// Python: session_archive.py（runtime.stop_session_for_lifecycle）
func (s *Service) stopSessionForLifecycle(
	ctx context.Context, channelID, sessionID string, generation *int, stopRunner bool,
) error {
	s.mu.Lock()
	stopper := s.sessionStopper
	s.mu.Unlock()
	if stopper == nil {
		// 未注入停止器：不阻塞收尾，让后续 rmtree 尽力完成
		logger.Debug(logComponent).Str("session_id", sessionID).
			Msg("session stopper not configured, skipping runtime stop")
		return nil
	}
	return stopper.StopSessionForLifecycle(ctx, channelID, sessionID, generation, stopRunner)
}

// flushWrites 刷写元数据与历史队列。
// Python: session_archive.py asyncio.gather(flush_pending_writes(10), ...)
func (s *Service) flushWrites() bool {
	s.mu.Lock()
	metadata := s.metadataFlusher
	history := s.historyFlusher
	s.mu.Unlock()
	ok := true
	if metadata != nil && !metadata.FlushPendingWrites(flushTimeoutSeconds) {
		ok = false
	}
	if history != nil && !history.FlushPendingWrites(flushTimeoutSeconds) {
		ok = false
	}
	return ok
}

// MoveSessionDirectory 在活动区与归档区之间移动会话目录。
// Python: session_archive.py _move_session_directory
func (s *Service) MoveSessionDirectory(
	sessionID, source, target string, archivedAt float64,
) error {
	if !pathExists(source) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if pathExists(target) {
		if removeErr := os.RemoveAll(target); removeErr != nil {
			return removeErr
		}
	}
	if err := os.Rename(source, target); err != nil {
		return err
	}
	// 归档时间写到元数据，供列表排序
	metaPath := filepath.Join(target, "metadata.json")
	if pathExists(metaPath) {
		meta, readErr := lifecycle.ReadJSON(metaPath)
		if readErr == nil {
			meta["archived_at"] = archivedAt
			_ = lifecycle.AtomicJSON(metaPath, meta)
		}
	}
	return nil
}

// archiveTime 归档时间：优先取元数据里已记录的，否则用当前时刻。
// Python: session_archive.py archive_time
func (s *Service) archiveTime(sessionID, archivedPath string, meta map[string]any) float64 {
	if stored := asFloatValue(meta["archived_at"]); stored > 0 {
		return stored
	}
	metaPath := filepath.Join(archivedPath, "metadata.json")
	if pathExists(metaPath) {
		if value, err := lifecycle.ReadJSON(metaPath); err == nil {
			if stored := asFloatValue(value["archived_at"]); stored > 0 {
				return stored
			}
		}
	}
	return float64(time.Now().UnixNano()) / 1e9
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// isOperationInProgress 错误是否为"操作占用"。
func isOperationInProgress(err error) bool {
	lifecycleErr, ok := err.(*lifecycle.LifecycleError)
	return ok && lifecycleErr.Code == "OPERATION_IN_PROGRESS"
}

// currentCleanupAttempts 读当前已记的收尾次数。
func currentCleanupAttempts(sessionID string) int {
	operation, _ := lifecycle.State("session", sessionID)["operation"].(map[string]any)
	if operation == nil {
		return 0
	}
	return asIntValue(operation["cleanup_attempts"])
}

// generationFromOperationOrDefault 取操作的停止代次，缺失时用调用方传入的。
func generationFromOperationOrDefault(operation map[string]any, fallback *int) *int {
	if generation := generationFromOperation(operation); generation != nil {
		return generation
	}
	return fallback
}

// readBoolField 读布尔字段。
func readBoolField(value map[string]any, key string) bool {
	boolean, _ := value[key].(bool)
	return boolean
}

// asIntValue 把 JSON 数值转 int。
func asIntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

// asFloatValue 把 JSON 数值转 float64。
func asFloatValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}
