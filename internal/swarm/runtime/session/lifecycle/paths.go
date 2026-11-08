package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LifecycleError 生命周期操作的已知失败。
// Python: lifecycle.py LifecycleError（带 code 与 details）
type LifecycleError struct {
	// Code 稳定错误码（BAD_REQUEST / NOT_FOUND / CONFLICT /
	// OPERATION_IN_PROGRESS / SESSION_ARCHIVED / SESSION_ID_CONFLICT）
	Code string
	// Message 错误说明
	Message string
	// Details 附加信息
	Details map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// reservedIDPrefix Windows 保留设备名（大小写不敏感，按第一个点前判）。
	// Python: lifecycle.py validate_id 的 CON/PRN/AUX/NUL/COM1-9/LPT1-9
	reservedIDPrefix = buildReservedIDs()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *LifecycleError) Error() string { return e.Message }

// GetAgentSessionsDir 会话根目录。
// Python: lifecycle.py get_agent_sessions_dir → utils.get_agent_sessions_dir()
func GetAgentSessionsDir() string {
	return filepath.Join(GetAgentRootDir(), "sessions")
}

// GetAgentRootDir agent 数据根目录（会话根目录的父级）。
// Python: lifecycle.py get_agent_root_dir（get_agent_sessions_dir().parent）
func GetAgentRootDir() string {
	if dir := strings.TrimSpace(os.Getenv("UAPCLAW_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "agent")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "agent")
	}
	return filepath.Join(home, ".uapclaw", "agent")
}

// ValidateID 校验资源标识，返回规范化后的值。
// Python: lifecycle.py validate_id
//
// 判据与上游逐条一致：
//  1. 必须是字符串（Go 侧由签名保证）
//  2. 非空且无首尾空白
//  3. 不含 ".."，不以 "." 或 " " 结尾
//  4. 不含 / \ : NUL < > " | ? *，不含控制字符（<32）
//  5. 第一个点前的部分不是 Windows 保留设备名
//
// 违反时返回 BAD_REQUEST；保留名返回 reserved resource ID 文案。
func ValidateID(value string) (string, error) {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
		return "", &LifecycleError{Code: "BAD_REQUEST", Message: "invalid resource ID"}
	}
	if strings.Contains(value, "..") ||
		strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") {
		return "", &LifecycleError{Code: "BAD_REQUEST", Message: "invalid resource ID"}
	}
	for _, char := range value {
		switch char {
		case '/', '\\', ':', '\x00', '<', '>', '"', '|', '?', '*':
			return "", &LifecycleError{Code: "BAD_REQUEST", Message: "invalid resource ID"}
		}
		if char < 32 {
			return "", &LifecycleError{Code: "BAD_REQUEST", Message: "invalid resource ID"}
		}
	}
	head := strings.ToUpper(strings.SplitN(value, ".", 2)[0])
	if _, reserved := reservedIDPrefix[head]; reserved {
		return "", &LifecycleError{Code: "BAD_REQUEST", Message: "reserved resource ID"}
	}
	return value, nil
}

// ResourcePath 资源生命周期文件路径。
// Python: lifecycle.py resource_path
//
// kind 只接受 session / project（其他值属编程错误，返回错误而非 panic）。
func ResourcePath(kind, resourceID string) (string, error) {
	if kind != "session" && kind != "project" {
		return "", fmt.Errorf("unsupported lifecycle resource kind: %s", kind)
	}
	validated, err := ValidateID(resourceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(GetAgentRootDir(), "lifecycle", "resources",
		kind+"_"+validated+".json"), nil
}

// SessionPaths 返回会话的活动目录与归档目录。
// Python: lifecycle.py session_paths
//
// 两条安全约束与上游一致：
//  1. 符号链接 / junction 不得扩大物理删除范围（解析后父目录必须与字面父目录一致）
//  2. 同一 ID 不得同时存在于两个存储区
func SessionPaths(sessionID string, sessionsRoot string) (string, string, error) {
	if _, err := ValidateID(sessionID); err != nil {
		return "", "", err
	}
	root := sessionsRoot
	if strings.TrimSpace(root) == "" {
		root = GetAgentSessionsDir()
	}
	active := filepath.Join(root, sessionID)
	archived := filepath.Join(filepath.Dir(root), "sessions_archived", sessionID)
	for _, path := range []string{active, archived} {
		info, statErr := os.Lstat(path)
		isSymlink := statErr == nil && info.Mode()&os.ModeSymlink != 0
		if isSymlink {
			return "", "", &LifecycleError{
				Code: "BAD_REQUEST", Message: "session path escapes managed storage"}
		}
		if statErr == nil {
			resolved, resolveErr := filepath.EvalSymlinks(path)
			if resolveErr != nil {
				return "", "", &LifecycleError{
					Code: "BAD_REQUEST", Message: "session path escapes managed storage"}
			}
			parentResolved, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
			if parentErr != nil {
				parentResolved = filepath.Dir(path)
			}
			if resolved != path || filepath.Dir(resolved) != parentResolved {
				return "", "", &LifecycleError{
					Code: "BAD_REQUEST", Message: "session path escapes managed storage"}
			}
		}
	}
	activeExists := pathExists(active)
	archivedExists := pathExists(archived)
	if activeExists && archivedExists {
		return "", "", &LifecycleError{
			Code: "SESSION_ID_CONFLICT", Message: "session exists in both storage areas"}
	}
	return active, archived, nil
}

// ResolveSession 解析会话目录（归档优先）。
// Python: lifecycle.py resolve_session
func ResolveSession(sessionID string, mustExist bool, sessionsRoot string) (string, error) {
	active, archived, err := SessionPaths(sessionID, sessionsRoot)
	if err != nil {
		return "", err
	}
	path := active
	if pathExists(archived) {
		path = archived
	}
	if mustExist && !isDirectory(path) {
		return "", &LifecycleError{Code: "NOT_FOUND", Message: "session not found"}
	}
	return path, nil
}

// RawMetadata 读会话原始元数据。
// Python: lifecycle.py raw_metadata
//
// 与元数据写入方共用同一把跨进程生命周期锁：未加锁的读在原子替换窗口内
// 可能收到共享冲突，即便会话本身健康。读锁使这里与写入方互斥。
func RawMetadata(sessionID string) (map[string]any, error) {
	release, err := ResourceLock("session", sessionID)
	if err != nil {
		return nil, err
	}
	defer release()
	path, resolveErr := ResolveSession(sessionID, false, "")
	if resolveErr != nil {
		return nil, resolveErr
	}
	return ReadJSON(filepath.Join(path, "metadata.json"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildReservedIDs 构造 Windows 保留设备名集合。
func buildReservedIDs() map[string]struct{} {
	reserved := map[string]struct{}{
		"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	}
	for i := 1; i <= 9; i++ {
		reserved[fmt.Sprintf("COM%d", i)] = struct{}{}
		reserved[fmt.Sprintf("LPT%d", i)] = struct{}{}
	}
	return reserved
}

// pathExists 路径是否存在（不跟随符号链接失败即视为不存在）。
func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// isDirectory 路径是否为目录。
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
