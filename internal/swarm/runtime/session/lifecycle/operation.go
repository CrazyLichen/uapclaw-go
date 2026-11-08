package lifecycle

import (
	"fmt"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// OperationStatusRunning 操作进行中
	OperationStatusRunning = "running"
	// OperationStatusCompleted 操作已完成
	OperationStatusCompleted = "completed"
	// OperationStatusFailed 操作失败（可重试）
	OperationStatusFailed = "failed"
	// OperationStatusAbandoned 操作放弃（不再重试）
	OperationStatusAbandoned = "abandoned"
	// OperationKindDelete 删除
	OperationKindDelete = "delete"
	// OperationKindArchive 归档
	OperationKindArchive = "archive"
	// phaseBlock 阻断执行阶段
	phaseBlock = "block"
	// phaseCheckSessions 检查会话阶段
	phaseCheckSessions = "check_sessions"
	// phaseFinalizing 收尾阶段
	phaseFinalizing = "finalizing"
	// leaseDurationSeconds 操作租约时长（秒）
	// Python: lifecycle.py claim_operation / renew_operation 的 30
	leaseDurationSeconds = 30
)

// ──────────────────────────── 导出函数 ────────────────────────────

// State 读资源状态（resourceID 为空返回空 map）。
// Python: lifecycle.py state
func State(kind, resourceID string) map[string]any {
	if resourceID == "" {
		return map[string]any{}
	}
	path, err := ResourcePath(kind, resourceID)
	if err != nil {
		return map[string]any{}
	}
	value, readErr := ReadJSON(path)
	if readErr != nil {
		return map[string]any{}
	}
	return value
}

// SessionDeletePending 是否有未完成的删除操作占用该会话的执行屏障。
// Python: lifecycle.py session_delete_pending
func SessionDeletePending(sessionID string) bool {
	value := State("session", sessionID)
	blocked, _ := value["blocked"].(bool)
	operation, _ := value["operation"].(map[string]any)
	if !blocked || operation == nil {
		return false
	}
	kind, _ := operation["kind"].(string)
	status, _ := operation["status"].(string)
	return kind == OperationKindDelete && status != OperationStatusCompleted
}

// SaveLocked 写资源文件（调用方须持锁）。
// Python: lifecycle.py save_locked
//
// 双写：revision 自增后先写 operations/<operation_id>.json（事件流消费方
// 只读这个目录），再写 resources/<kind>_<id>.json（准入判定读这个）。
// 顺序不可颠倒：先写操作日志再写资源文件，崩溃后事件流不会缺条目。
func SaveLocked(kind, resourceID string, value map[string]any) {
	revision := asInt(value["revision"]) + 1
	value["revision"] = revision
	if operation, ok := value["operation"].(map[string]any); ok && operation != nil {
		operation["updated_at"] = nowSeconds()
		operation["revision"] = revision
		if operationID, _ := operation["operation_id"].(string); operationID != "" {
			operationPath := operationLogPath(operationID)
			_ = AtomicJSON(operationPath, operation)
		}
	}
	path, err := ResourcePath(kind, resourceID)
	if err != nil {
		return
	}
	_ = AtomicJSON(path, value)
}

// Begin 开始一次生命周期操作。
// Python: lifecycle.py begin
//
// 行为逐条对齐：
//  1. 已有未完成操作且 kind 不同 → OPERATION_IN_PROGRESS
//  2. 已有未完成操作且 kind 相同 → 直接返回既有操作（幂等重入）
//  3. 资源已永久删除 → delete 返回既有操作，其他动作 NOT_FOUND
//  4. 否则新建操作，generation 累加 blockExecution
func Begin(kind, resourceID, action string, blockExecution bool) (map[string]any, error) {
	release, err := ResourceLock(kind, resourceID)
	if err != nil {
		return nil, err
	}
	defer release()

	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		return nil, readErr
	}
	existing, _ := value["operation"].(map[string]any)
	if existing != nil {
		status, _ := existing["status"].(string)
		if status != OperationStatusCompleted {
			existingKind, _ := existing["kind"].(string)
			if existingKind != action {
				return nil, &LifecycleError{
					Code:    "OPERATION_IN_PROGRESS",
					Message: "another lifecycle operation is pending"}
			}
			return existing, nil
		}
	}
	if deleted, _ := value["deleted"].(bool); deleted {
		if action == OperationKindDelete {
			return existing, nil
		}
		return nil, &LifecycleError{Code: "NOT_FOUND", Message: "resource was permanently deleted"}
	}
	generation := asInt(value["generation"])
	if blockExecution {
		generation++
	}
	now := nowSeconds()
	operation := map[string]any{
		"operation_id":     newOperationID(),
		"resource_type":    kind,
		"resource_id":      resourceID,
		"kind":             action,
		"status":           OperationStatusRunning,
		"phase":            phaseCheckSessions,
		"generation":       generation,
		"created_at":       now,
		"updated_at":       now,
		"archived_at":      now,
		"completed_items":  map[string]any{},
		"errors":           []any{},
		"retryable":        true,
		"stop_pending":     false,
		"owner_id":         "",
		"lease_expires_at": 0,
	}
	if blockExecution {
		operation["phase"] = phaseBlock
	}
	value["operation"] = operation
	value["generation"] = generation
	blocked := blockExecution
	if stored, ok := value["blocked"].(bool); ok && stored {
		blocked = true
	}
	value["blocked"] = blocked
	if blockExecution {
		value["write_blocked"] = action == "unarchive"
	} else if _, ok := value["write_blocked"]; !ok {
		value["write_blocked"] = false
	}
	value["drain_generation"] = generation - 1
	SaveLocked(kind, resourceID, value)
	return operation, nil
}

// ClaimOperation 认领操作（仅由持有独立执行归属锁的进程调用）。
// Python: lifecycle.py claim_operation
//
// 换主时递增 generation：旧主的在飞写入用旧代次，被 write_guard 拒绝。
func ClaimOperation(kind, resourceID, ownerID string) (map[string]any, error) {
	release, err := ResourceLock(kind, resourceID)
	if err != nil {
		return nil, err
	}
	defer release()

	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		return nil, readErr
	}
	operation, _ := value["operation"].(map[string]any)
	if operation == nil {
		return map[string]any{}, nil
	}
	if status, _ := operation["status"].(string); status == OperationStatusCompleted {
		return operation, nil
	}
	if currentOwner, _ := operation["owner_id"].(string); currentOwner != "" && currentOwner != ownerID {
		generation := asInt(value["generation"]) + 1
		value["generation"] = generation
		operation["generation"] = generation
	}
	operation["owner_id"] = ownerID
	operation["lease_expires_at"] = nowSeconds() + leaseDurationSeconds
	SaveLocked(kind, resourceID, value)
	return operation, nil
}

// RenewOperation 续租或释放操作。
// Python: lifecycle.py renew_operation
//
// 租约心跳不代表业务状态迁移，因此不走 save_locked（不自增 revision），
// 直接写两份文件——避免事件流被心跳刷屏。
func RenewOperation(kind, resourceID, ownerID string, release bool) error {
	lockRelease, err := ResourceLock(kind, resourceID)
	if err != nil {
		return err
	}
	defer lockRelease()

	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		return readErr
	}
	operation, _ := value["operation"].(map[string]any)
	if operation == nil {
		return nil
	}
	if currentOwner, _ := operation["owner_id"].(string); currentOwner != ownerID {
		return nil
	}
	if status, _ := operation["status"].(string); status == OperationStatusCompleted {
		return nil
	}
	if release {
		operation["lease_expires_at"] = 0
	} else {
		operation["lease_expires_at"] = nowSeconds() + leaseDurationSeconds
	}
	if operationID, _ := operation["operation_id"].(string); operationID != "" {
		_ = AtomicJSON(operationLogPath(operationID), operation)
	}
	path, pathErr := ResourcePath(kind, resourceID)
	if pathErr != nil {
		return pathErr
	}
	return AtomicJSON(path, value)
}

// Update 更新操作的若干字段。
// Python: lifecycle.py update(kind, resource_id, **changes)
func Update(kind, resourceID string, changes map[string]any) (map[string]any, error) {
	release, err := ResourceLock(kind, resourceID)
	if err != nil {
		return nil, err
	}
	defer release()

	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		return nil, readErr
	}
	operation, _ := value["operation"].(map[string]any)
	if operation == nil {
		return map[string]any{}, nil
	}
	for key, item := range changes {
		operation[key] = item
	}
	SaveLocked(kind, resourceID, value)
	return operation, nil
}

// Complete 标记操作完成并落最终可见性。
// Python: lifecycle.py complete(kind, resource_id, *, archived, deleted, result)
//
// 完成即把 blocked/write_blocked 对齐到最终状态：归档与删除都不可见，
// 未归档未删除则解除围栏。
func Complete(
	kind, resourceID string, archived, deleted bool, result map[string]any,
) error {
	release, err := ResourceLock(kind, resourceID)
	if err != nil {
		return err
	}
	defer release()

	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		return readErr
	}
	finalBlocked := archived || deleted
	value["blocked"] = finalBlocked
	value["write_blocked"] = finalBlocked
	value["deleted"] = deleted
	operation, _ := value["operation"].(map[string]any)
	if operation == nil {
		operation = map[string]any{}
		value["operation"] = operation
	}
	operation["status"] = OperationStatusCompleted
	operation["stop_pending"] = false
	if result == nil {
		result = map[string]any{}
	}
	operation["result"] = result
	if deleted {
		delete(operation, "delete_metadata")
	}
	SaveLocked(kind, resourceID, value)
	return nil
}

// LogicallyDeleted 会话是否已接受删除、只待物理清理。
// Python: lifecycle.py logically_deleted
func LogicallyDeleted(value map[string]any) bool {
	deleted, _ := value["deleted"].(bool)
	return deleted
}

// LogicallyArchived 会话是否已逻辑归档（目录可能还在活动区）。
// Python: lifecycle.py logically_archived
//
// 归档是"同步接受 + 后台收尾"，因此列表必须在目录还没搬走时就不显示它。
// 失败与放弃的归档保持同一逻辑结果：用户要求归档，清理只决定字节放哪。
func LogicallyArchived(value map[string]any) bool {
	operation, _ := value["operation"].(map[string]any)
	if operation == nil {
		return false
	}
	kind, _ := operation["kind"].(string)
	status, _ := operation["status"].(string)
	return kind == OperationKindArchive && status != OperationStatusCompleted
}

// MarkFinalizing 记录逻辑结果并把物理清理交给后台任务。
// Python: lifecycle.py mark_finalizing
//
// write_blocked 刻意不动：已被元数据/历史队列接受的写入仍需排空，
// 后台 finalizer 在排空之后才加写围栏。
func MarkFinalizing(
	kind, resourceID string, deleted bool, changes map[string]any,
) (map[string]any, error) {
	release, err := ResourceLock(kind, resourceID)
	if err != nil {
		return nil, err
	}
	defer release()

	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		return nil, readErr
	}
	operation, _ := value["operation"].(map[string]any)
	if operation == nil {
		return map[string]any{}, nil
	}
	if status, _ := operation["status"].(string); status == OperationStatusCompleted {
		return operation, nil
	}
	if deleted {
		value["deleted"] = true
	}
	value["blocked"] = true
	operation["status"] = OperationStatusRunning
	operation["phase"] = phaseFinalizing
	operation["stop_pending"] = true
	operation["retryable"] = true
	operation["cleanup_attempts"] = 0
	operation["cleanup_next_at"] = 0
	operation["cleanup_exhausted"] = false
	for key, item := range changes {
		operation[key] = item
	}
	SaveLocked(kind, resourceID, value)
	return operation, nil
}

// AbandonFailedOperation 关闭失败/放弃的操作，让反向动作接管。
// Python: lifecycle.py abandon_failed_operation
//
// 失败的归档会留下"逻辑已归档、目录还在活动区"的状态，撤销归档必须
// 能从该状态恢复，而不是被残留操作永远挡住。
func AbandonFailedOperation(kind, resourceID, expectedKind string) (bool, error) {
	release, err := ResourceLock(kind, resourceID)
	if err != nil {
		return false, err
	}
	defer release()

	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		return false, readErr
	}
	operation, _ := value["operation"].(map[string]any)
	if operation == nil {
		return false, nil
	}
	if status, _ := operation["status"].(string); status == OperationStatusCompleted {
		return false, nil
	}
	operationKind, _ := operation["kind"].(string)
	operationStatus, _ := operation["status"].(string)
	if operationKind != expectedKind ||
		(operationStatus != OperationStatusFailed && operationStatus != OperationStatusAbandoned) {
		return false, nil
	}
	archived := false
	if kind == "session" || resourceID != "" {
		_, archivedPath, pathErr := SessionPaths(resourceID, "")
		if pathErr == nil {
			archived = pathExists(archivedPath)
		}
	}
	value["blocked"] = archived
	value["write_blocked"] = archived
	value["deleted"] = false
	operation["status"] = OperationStatusCompleted
	operation["stop_pending"] = false
	operation["result"] = map[string]any{}
	SaveLocked(kind, resourceID, value)
	return true, nil
}

// SetVisibility 改写逻辑可见性，不动操作记录。
// Python: lifecycle.py set_visibility
//
// 恢复路径在物理搬移成功或失败后调用：只要目录回到活动区，会话就必须
// 可见，即便后续某个清理步骤失败了。
func SetVisibility(
	kind, resourceID string, blocked bool, writeBlocked *bool, deleted bool,
) error {
	release, err := ResourceLock(kind, resourceID)
	if err != nil {
		return err
	}
	defer release()

	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		return readErr
	}
	value["blocked"] = blocked
	if writeBlocked == nil {
		value["write_blocked"] = blocked
	} else {
		value["write_blocked"] = *writeBlocked
	}
	if !blocked {
		value["deleted"] = deleted
	}
	SaveLocked(kind, resourceID, value)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stateLocked 读资源状态（调用方须持锁）。
func stateLocked(kind, resourceID string) (map[string]any, error) {
	path, err := ResourcePath(kind, resourceID)
	if err != nil {
		return nil, err
	}
	return ReadJSON(path)
}

// operationLogPath 操作日志路径（事件流消费方只读这个目录）。
func operationLogPath(operationID string) string {
	return fmt.Sprintf("%s/lifecycle/operations/%s.json", GetAgentRootDir(), operationID)
}

// newOperationID 生成操作标识（32 位十六进制，与 uuid4().hex 同形）。
func newOperationID() string {
	return fmt.Sprintf("%016x%016x", time.Now().UnixNano(), time.Now().UnixMicro())
}

// nowSeconds 当前 Unix 秒（含小数）。
func nowSeconds() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
