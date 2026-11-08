package lifecycle

import (
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeOwner 会话运行时的归属进程。
// Python: lifecycle.py runtime_owner（pid + started_at）
type RuntimeOwner struct {
	// PID 进程标识
	PID int
	// StartedAt 进程启动时刻（秒）
	StartedAt float64
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Guard 准入判定：该会话（及其项目）当前是否允许执行。
// Python: lifecycle.py guard
//
// 判定顺序与上游一致，先会话后项目。错误码区分三种"不可执行"：
//   - OPERATION_IN_PROGRESS 有未完成的生命周期操作
//   - NOT_FOUND             已被永久删除
//   - SESSION_ARCHIVED      已归档
func Guard(sessionID, projectID string) error {
	if sessionID != "" {
		value := State("session", sessionID)
		if blocked, _ := value["blocked"].(bool); blocked {
			operation, _ := value["operation"].(map[string]any)
			if operation != nil {
				if status, _ := operation["status"].(string); status != OperationStatusCompleted {
					return &LifecycleError{
						Code:    "OPERATION_IN_PROGRESS",
						Message: "session lifecycle operation in progress"}
				}
			}
			if deleted, _ := value["deleted"].(bool); deleted {
				return &LifecycleError{Code: "NOT_FOUND", Message: "session was permanently deleted"}
			}
			return &LifecycleError{Code: "SESSION_ARCHIVED", Message: "session is archived"}
		}
		_, archivedPath, pathErr := SessionPaths(sessionID, "")
		if pathErr != nil {
			return pathErr
		}
		if pathExists(archivedPath) {
			return &LifecycleError{Code: "SESSION_ARCHIVED", Message: "session is archived"}
		}
		if projectID == "" {
			meta, metaErr := RawMetadata(sessionID)
			if metaErr == nil {
				projectID = ProjectIDFor(meta, nil)
			}
		}
	}
	if projectID != "" {
		value := State("project", projectID)
		if blocked, _ := value["blocked"].(bool); blocked {
			operation, _ := value["operation"].(map[string]any)
			if operation != nil {
				if status, _ := operation["status"].(string); status != OperationStatusCompleted {
					return &LifecycleError{
						Code:    "OPERATION_IN_PROGRESS",
						Message: "project lifecycle operation in progress"}
				}
			}
			return &LifecycleError{Code: "NOT_FOUND", Message: "project was permanently deleted"}
		}
	}
	return nil
}

// WriteGuard 写准入：会话（及其项目）当前是否允许落盘。
// Python: lifecycle.py write_guard
//
// 与 Guard 的差别在于它会放行"已接受但仍在排空"的写入：blocked 为真而
// write_blocked 为假时，drain_generation 与当前代次都被接受——这是关闭
// 路径上已入队写入的最后窗口。
func WriteGuard(sessionID string, generation *int) error {
	value := State("session", sessionID)
	writeBlocked := readBoolWithFallback(value, "write_blocked", "blocked")
	_, archivedPath, pathErr := SessionPaths(sessionID, "")
	if pathErr != nil {
		return pathErr
	}
	if writeBlocked || pathExists(archivedPath) {
		return &LifecycleError{
			Code: "OPERATION_IN_PROGRESS", Message: "session writes are isolated"}
	}
	current := asInt(value["generation"])
	allowed := map[int]struct{}{current: {}}
	if blocked, _ := value["blocked"].(bool); blocked && !writeBlocked {
		drainGeneration := asInt(value["drain_generation"])
		allowed[drainGeneration] = struct{}{}
	}
	if generation != nil {
		if _, ok := allowed[*generation]; !ok {
			return &LifecycleError{
				Code: "OPERATION_IN_PROGRESS", Message: "stale session writer generation"}
		}
	}
	meta, metaErr := RawMetadata(sessionID)
	if metaErr == nil {
		projectID := ProjectIDFor(meta, nil)
		parent := State("project", projectID)
		if readBoolWithFallback(parent, "write_blocked", "blocked") {
			return &LifecycleError{
				Code: "OPERATION_IN_PROGRESS", Message: "project writes are isolated"}
		}
	}
	return nil
}

// AssertRuntimeOwner 确认没有别的活进程持有该会话运行时。
// Python: lifecycle.py assert_runtime_owner
//
// 只在能证明"归属进程已死"时放行；无法判断（无权限）一律按占用处理，
// 因为搬文件期间有第二个进程在写会造成不可恢复的损坏。
func AssertRuntimeOwner(sessionID string) error {
	ownerValue, _ := State("session", sessionID)["runtime_owner"].(map[string]any)
	if ownerValue == nil {
		return nil
	}
	ownerPID := asInt(ownerValue["pid"])
	if ownerPID == 0 || ownerPID == os.Getpid() {
		return nil
	}
	alive, startedAt, err := inspectProcess(ownerPID)
	if err != nil {
		return &LifecycleError{
			Code: "OPERATION_IN_PROGRESS", Message: "cannot verify the previous runtime owner"}
	}
	if !alive {
		return nil
	}
	ownerStarted := asFloat(ownerValue["started_at"])
	if startedAt > 0 && absFloat(startedAt-ownerStarted) < 0.01 {
		return &LifecycleError{
			Code: "OPERATION_IN_PROGRESS", Message: "another live AgentServer owns the session runtime"}
	}
	return nil
}

// ClaimRuntime 登记本进程为该会话的运行时归属。
// Python: lifecycle.py claim_runtime（guard → assert_runtime_owner → 写 pid）
func ClaimRuntime(sessionID string) error {
	release, err := ResourceLock("session", sessionID)
	if err != nil {
		return err
	}
	defer release()

	if guardErr := Guard(sessionID, ""); guardErr != nil {
		return guardErr
	}
	if ownerErr := AssertRuntimeOwner(sessionID); ownerErr != nil {
		return ownerErr
	}
	value, readErr := stateLocked("session", sessionID)
	if readErr != nil {
		return readErr
	}
	startedAt, _, _ := inspectProcess(os.Getpid())
	value["runtime_owner"] = map[string]any{
		"pid":        os.Getpid(),
		"started_at": startedAt,
	}
	SaveLocked("session", sessionID, value)
	return nil
}

// ReleaseRuntime 释放本进程的运行时归属。
// Python: lifecycle.py release_runtime
func ReleaseRuntime(sessionID string) error {
	release, err := ResourceLock("session", sessionID)
	if err != nil {
		return err
	}
	defer release()

	value, readErr := stateLocked("session", sessionID)
	if readErr != nil {
		return readErr
	}
	ownerValue, _ := value["runtime_owner"].(map[string]any)
	if ownerValue == nil {
		return nil
	}
	if asInt(ownerValue["pid"]) != os.Getpid() {
		return nil
	}
	delete(value, "runtime_owner")
	SaveLocked("session", sessionID, value)
	return nil
}

// Visible 会话是否应出现在活动列表中。
// Python: lifecycle.py visible
func Visible(meta map[string]any) bool {
	sessionID, _ := meta["session_id"].(string)
	if strings.TrimSpace(sessionID) == "" {
		return true
	}
	_, archivedPath, err := SessionPaths(sessionID, "")
	if err != nil {
		return true
	}
	return !pathExists(archivedPath)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readBoolWithFallback 读首选布尔键，缺失时回退到次选键。
func readBoolWithFallback(value map[string]any, primary, fallback string) bool {
	if raw, ok := value[primary]; ok {
		if boolean, isBool := raw.(bool); isBool {
			return boolean
		}
	}
	boolean, _ := value[fallback].(bool)
	return boolean
}

// asFloat 把 JSON 解出的数值转 float64。
func asFloat(value any) float64 {
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

// absFloat 取绝对值。
func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
