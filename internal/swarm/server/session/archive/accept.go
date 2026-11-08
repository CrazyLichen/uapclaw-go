package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Session 处理一次会话生命周期动作（archive / delete / unarchive）。
// Python: session_archive.py SessionArchiveService.session（:292）
//
// 步骤与上游一致：
//  1. 校验 ID、读元数据、推断项目
//  2. 无 parentOperation 时先拿**项目锁**（顺序不可颠倒：项目级联已持项目锁，
//     会话锁在内层），并检查项目上是否有未完成操作
//  3. 进入 acceptSession
func (s *Service) Session(
	ctx context.Context, sessionID, action, channelID, parentOperation string,
) (map[string]any, error) {
	if _, err := lifecycle.ValidateID(sessionID); err != nil {
		return nil, err
	}
	if action != operationKindDelete && action != operationKindArchive && action != operationKindUnarchive {
		return nil, &lifecycle.LifecycleError{
			Code: "BAD_REQUEST", Message: "unknown session lifecycle action"}
	}

	meta, metaErr := lifecycle.RawMetadata(sessionID)
	if metaErr != nil {
		meta = map[string]any{}
	}
	projectID := lifecycle.ProjectIDFor(meta, nil)

	if parentOperation == "" {
		release, lockErr := s.lockWait("project", projectID)
		if lockErr != nil {
			return nil, lockErr
		}
		parent, _ := lifecycle.State("project", projectID)["operation"].(map[string]any)
		if parent != nil {
			if status, _ := parent["status"].(string); status != lifecycle.OperationStatusCompleted {
				release()
				return nil, &lifecycle.LifecycleError{
					Code: "OPERATION_IN_PROGRESS", Message: "project operation is pending"}
			}
		}
		result, acceptErr := s.acceptSession(ctx, sessionID, action, channelID, projectID)
		release()
		return result, acceptErr
	}
	return s.acceptSession(ctx, sessionID, action, channelID, projectID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// acceptSession 会话锁内的接受阶段。
// Python: session_archive.py _session（:864）
func (s *Service) acceptSession(
	ctx context.Context, sessionID, action, channelID, projectID string,
) (map[string]any, error) {
	release, err := s.lockWait("session", sessionID)
	if err != nil {
		return nil, err
	}
	defer release()

	active, archived, pathErr := lifecycle.SessionPaths(sessionID, "")
	if pathErr != nil {
		return nil, pathErr
	}
	previous, _ := lifecycle.State("session", sessionID)["operation"].(map[string]any)
	pending := previous != nil &&
		operationStatus(previous) != lifecycle.OperationStatusCompleted

	activeExists := pathExists(active)
	archivedExists := pathExists(archived)
	if !activeExists && !archivedExists && !(pending && action == operationKindDelete) {
		return nil, &lifecycle.LifecycleError{Code: "NOT_FOUND", Message: "session not found"}
	}

	meta, metaErr := lifecycle.RawMetadata(sessionID)
	if metaErr != nil {
		meta = map[string]any{}
	}
	isCronSession := hasCronID(meta) || strings.HasPrefix(sessionID, "cron_") ||
		strings.HasPrefix(sessionID, "heartbeat_")
	// 子代理控制与会话适配器按 Agent 持有，Agent 又按渠道缓存：
	// 必须走会话自己的渠道，而不是恰好服务本次请求的那个渠道。
	ownerChannelID := firstNonEmpty(stringOf(meta, "channel_id"), channelID)
	pinned, _ := meta["pinned"].(bool)
	pinReindexRequired := (action == operationKindArchive || action == operationKindUnarchive) && pinned

	if action != operationKindDelete && isCronSession {
		return nil, &lifecycle.LifecycleError{
			Code: "FORBIDDEN", Message: "cron and heartbeat sessions cannot be archived separately"}
	}
	if action == operationKindUnarchive {
		return s.unarchiveSession(sessionID, projectID, active, archived,
			previous, pending, pinReindexRequired)
	}
	return s.acceptSessionCleanup(sessionID, action, projectID, meta, active, archived,
		ownerChannelID, pinReindexRequired, previous, pending)
}

// acceptSessionCleanup 落逻辑结果后立即返回，不等物理清理。
// Python: session_archive.py _accept_session_cleanup（:934）
func (s *Service) acceptSessionCleanup(
	sessionID, action, projectID string, meta map[string]any,
	active, archived, ownerChannelID string, pinReindexRequired bool,
	previous map[string]any, pending bool,
) (map[string]any, error) {
	// 重试时沿用首次记录的渠道与项目：这两个值决定了要向哪个 agent 停东西
	if action == operationKindDelete && previous != nil {
		if runtimeDeleted, _ := previous["runtime_deleted"].(bool); runtimeDeleted {
			projectID = firstNonEmpty(stringOf(previous, "project_id"), projectID)
			ownerChannelID = firstNonEmpty(stringOf(previous, "channel_id"), ownerChannelID)
		}
	}
	if pending && previous != nil && operationKind(previous) != action {
		canTakeOverArchive := action == operationKindDelete &&
			operationKind(previous) == operationKindArchive &&
			isFailedOrAbandoned(previous)
		if !canTakeOverArchive {
			return nil, &lifecycle.LifecycleError{
				Code: "OPERATION_IN_PROGRESS", Message: "another lifecycle operation is pending"}
		}
		// 失败的归档仍占着"逻辑已归档"的结果。删除它必须落新意图，
		// 而不是被残留操作弹回；物理清理保持尽力而为。
		if _, abandonErr := lifecycle.AbandonFailedOperation(
			"session", sessionID, operationKindArchive); abandonErr != nil {
			return nil, abandonErr
		}
		previous, _ = lifecycle.State("session", sessionID)["operation"].(map[string]any)
		pending = previous != nil && operationStatus(previous) != lifecycle.OperationStatusCompleted
	}

	var mailbox Mailbox
	if action == operationKindDelete {
		mailbox = s.mailboxOrNil()
	}
	// 重复点击加入在飞清理：同步重申邮箱屏障，覆盖"重启后重试"场景
	if pending && previous != nil && operationStatus(previous) == lifecycle.OperationStatusRunning {
		if mailbox != nil {
			mailbox.BlockTargetDelete(sessionID)
		}
		s.spawnFinalizer(sessionID, action, ownerChannelID, projectID,
			generationFromOperation(previous))
		return acceptedPayload(sessionID, action, projectID, previous), nil
	}
	// 已归档且无待办：幂等返回，不再起一次收尾
	if !pending && action == operationKindArchive && pathExists(archived) {
		return map[string]any{
			"session_id":   sessionID,
			"ok":           true,
			"archived":     true,
			"archived_at":  s.archiveTime(sessionID, archived, meta),
			"accepted":     true,
			"finalizing":   false,
			"stop_pending": false,
			"project_id":   projectID,
		}, nil
	}

	operation, beginErr := lifecycle.Begin("session", sessionID, action, true)
	if beginErr != nil {
		return nil, beginErr
	}
	operation, claimErr := lifecycle.ClaimOperation("session", sessionID, s.ownerID)
	if claimErr != nil {
		return nil, claimErr
	}
	generation := s.captureGeneration(sessionID)
	// 失败的置顶重排不能让"需要重排"这个要求随元数据一起丢掉
	if stored, ok := operation["pin_reindex_required"].(bool); ok && stored {
		pinReindexRequired = true
	}
	if _, updateErr := lifecycle.Update("session", sessionID, map[string]any{
		"project_id":           projectID,
		"channel_id":           ownerChannelID,
		"stop_generation":      generation,
		"pin_reindex_required": pinReindexRequired,
	}); updateErr != nil {
		return nil, updateErr
	}
	operation, finalizingErr := lifecycle.MarkFinalizing(
		"session", sessionID, action == operationKindDelete, nil)
	if finalizingErr != nil {
		return nil, finalizingErr
	}
	if mailbox != nil {
		// 快速非阻塞屏障；等消费者退出属于后台 finalizer，不能拖慢响应
		mailbox.BlockTargetDelete(sessionID)
	}
	s.spawnFinalizer(sessionID, action, ownerChannelID, projectID, generation)
	return acceptedPayload(sessionID, action, projectID, operation), nil
}

// acceptedPayload 构造接受阶段的返回体（Gateway 完成契约）。
// Python: session_archive.py _accepted_payload（:388）
//
// accepted / finalizing / stop_pending 三个字段让调用方能区分
// "已接受"与"已删完"。
func acceptedPayload(sessionID, action, projectID string, operation map[string]any) map[string]any {
	if action == operationKindArchive {
		return map[string]any{
			"session_id":   sessionID,
			"ok":           true,
			"archived":     true,
			"archived_at":  operation["archived_at"],
			"accepted":     true,
			"finalizing":   true,
			"stop_pending": true,
			"project_id":   projectID,
		}
	}
	return map[string]any{
		"session_id":   sessionID,
		"ok":           true,
		"accepted":     true,
		"finalizing":   true,
		"stop_pending": true,
		"project_id":   projectID,
	}
}

// captureGeneration 快照后台停止要瞄准的运行时代次。
// Python: session_archive.py _capture_generation（:371）
//
// 清理期间会话可能被重开（或取消归档），没有快照的迟到 finalizer
// 会把新运行时关掉。
func (s *Service) captureGeneration(sessionID string) *int {
	s.mu.Lock()
	probe := s.generationProbe
	s.mu.Unlock()
	if probe == nil {
		return nil
	}
	return probe.SessionGeneration(sessionID)
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// pathExists 路径是否存在。
func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// sessionRoots 返回活动区与归档区根目录。
func sessionRoots() (string, string) {
	active := lifecycle.GetAgentSessionsDir()
	return active, filepath.Join(filepath.Dir(active), "sessions_archived")
}

// operationStatus 读操作状态。
func operationStatus(operation map[string]any) string {
	status, _ := operation["status"].(string)
	return status
}

// operationKind 读操作类型。
func operationKind(operation map[string]any) string {
	kind, _ := operation["kind"].(string)
	return kind
}

// isFailedOrAbandoned 操作是否已失败或放弃。
func isFailedOrAbandoned(operation map[string]any) bool {
	status := operationStatus(operation)
	return status == lifecycle.OperationStatusFailed || status == lifecycle.OperationStatusAbandoned
}

// generationFromOperation 从操作记录取停止代次。
func generationFromOperation(operation map[string]any) *int {
	raw, exists := operation["stop_generation"]
	if !exists || raw == nil {
		return nil
	}
	generation, ok := asIntPointer(raw)
	if !ok {
		return nil
	}
	return &generation
}

// asIntPointer 把 JSON 数值转 int 指针。
func asIntPointer(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

// stringOf 读字符串字段。
func stringOf(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}

// firstNonEmpty 取第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// hasCronID 元数据是否带 cron_id。
func hasCronID(meta map[string]any) bool {
	value, exists := meta["cron_id"]
	if !exists || value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

// mailboxOrNil 取会话邮箱（未注入返回 nil）。
func (s *Service) mailboxOrNil() Mailbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mailbox
}
