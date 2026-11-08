package common

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InvalidProtocolId 外部输入的协议标识不合格。
// Python: gateway/common/protocol_ids.py InvalidProtocolId
//
// Message 刻意不含原始值：调用方可以安全把消息返回或写日志，
// 即使输入极大（对齐上游 value-safe 约定）。
type InvalidProtocolId struct {
	// Message 不含原值的错误说明
	Message string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SessionIDMaxLen 会话 ID 最大长度（与持久会话契约一致）
	// Python: protocol_ids.py SESSION_ID_MAX_LEN
	SessionIDMaxLen = 80
	// WorkflowRunIDMaxLen workflow run ID 最大长度
	// Python: protocol_ids.py WORKFLOW_RUN_ID_MAX_LEN
	WorkflowRunIDMaxLen = 256
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sessionIDPattern 会话 ID 白名单：ASCII 字母数字下划线开头结尾，
	// 点与横线只允许出现在中间，总长上限由 SessionIDMaxLen 单独判定。
	// Python: protocol_ids.py _SESSION_ID_PATTERN
	sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_](?:[A-Za-z0-9_.-]{0,78}[A-Za-z0-9_])?$`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *InvalidProtocolId) Error() string { return e.Message }

// ValidateSessionID 校验外部传入的会话标识。
// Python: protocol_ids.py validate_session_id
//
// 四条判据，顺序与上游一致：
//  1. 非空且无首尾空白
//  2. 长度不超过 SessionIDMaxLen
//  3. 命中白名单正则（全串匹配）
//
// 校验必须在标识被写入路由表或日志之前完成。
func ValidateSessionID(value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return &InvalidProtocolId{Message: "invalid session_id"}
	}
	if len(value) > SessionIDMaxLen {
		return &InvalidProtocolId{
			Message: fmt.Sprintf("session_id exceeds maximum length %d", SessionIDMaxLen),
		}
	}
	if !sessionIDPattern.MatchString(value) {
		return &InvalidProtocolId{Message: "invalid session_id"}
	}
	return nil
}

// IsValidSessionID 判断会话标识是否满足共享契约。
// Python: protocol_ids.py is_valid_session_id
func IsValidSessionID(value string) bool {
	return ValidateSessionID(value) == nil
}

// ValidateWorkflowRunID 校验外部传入的 workflow run 标识。
// Python: protocol_ids.py validate_workflow_run_id
//
// 判据：非空且无首尾空白 / 长度上限 / 不含控制字符（<32 或 ==127）。
func ValidateWorkflowRunID(value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return &InvalidProtocolId{Message: "invalid run_id"}
	}
	if len(value) > WorkflowRunIDMaxLen {
		return &InvalidProtocolId{
			Message: fmt.Sprintf("run_id exceeds maximum length %d", WorkflowRunIDMaxLen),
		}
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return &InvalidProtocolId{Message: "invalid run_id"}
		}
	}
	return nil
}

// ResolveSessionDir 安全解析会话目录，返回目录绝对路径与是否存在。
// Python: server/runtime/session/session_history.py resolve_session_dir
//
// 两步防御：
//  1. 白名单校验——不合格时根本不拼路径
//  2. Clean 后确认结果仍在 sessions 根内（白名单被绕过的兜底）
//
// 活动目录不存在时回退到 sessions_archived/<id>（Python 侧同一语义）。
// 非法标识返回错误，调用方应转成 BAD_REQUEST。
func ResolveSessionDir(sessionID, sessionsRoot string) (string, bool, error) {
	if err := ValidateSessionID(sessionID); err != nil {
		return "", false, err
	}
	cleanedRoot := filepath.Clean(sessionsRoot)
	active := filepath.Clean(filepath.Join(cleanedRoot, sessionID))
	if active != cleanedRoot && !strings.HasPrefix(active, cleanedRoot+string(os.PathSeparator)) {
		return "", false, &InvalidProtocolId{Message: "invalid session_id"}
	}
	if info, statErr := os.Stat(active); statErr == nil && info.IsDir() {
		return active, true, nil
	}
	// 归档目录与 sessions 同级：<root>/../sessions_archived/<id>
	archived := filepath.Clean(filepath.Join(filepath.Dir(cleanedRoot), "sessions_archived", sessionID))
	if info, statErr := os.Stat(archived); statErr == nil && info.IsDir() {
		return archived, true, nil
	}
	return active, false, nil
}
