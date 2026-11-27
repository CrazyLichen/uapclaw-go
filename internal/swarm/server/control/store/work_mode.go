// Package store Front Control 的项目磁盘查询层（只读）。
//
// 对齐 Python jiuwenswarm/server/control/store/project_queries.py（491 行）
// 及其依赖 project_store.py 的读路径子集、gateway/common/work_mode.py、
// jiuwenswarm/common/cron_session.py、server/runtime/session/session_info.py。
// Phase 1 Control 只拥有轻量项目读（list/info/sessions）；
// create/rename/pin 与全部 git 操作留在执行平面（Python project_adapter.py，⤴️ 挂账）。
package store

import "strings"

// ──────────────────────────── 常量 ────────────────────────────

const (
	// WorkModeWeb 工作模式：work（对齐 DEFAULT_WEB_WORK_MODE）
	WorkModeWeb = "work"
	// WorkModeCode 工作模式：code（对齐 DEFAULT_TUI_WORK_MODE）
	WorkModeCode = "code"
	// WorkModeDesign 工作模式：design（对齐 DESIGN_WORK_MODE）
	WorkModeDesign = "design"
	// DefaultProjectIDWork work 模式默认项目标识（DEFAULT_PROJECT_ID_WORK）
	DefaultProjectIDWork = "default"
	// DefaultProjectIDCode code 模式默认项目标识（DEFAULT_PROJECT_ID_CODE）
	DefaultProjectIDCode = "default_code"
)

// supportedWorkModes 合法工作模式集合（对齐 SUPPORTED_WORK_MODES）
var supportedWorkModes = map[string]bool{"code": true, "design": true, "work": true}

// executionWorkModes 支持定时任务的工作模式（对齐 EXECUTION_WORK_MODES）
var executionWorkModes = map[string]bool{"code": true, "work": true}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsDefaultProjectID 是否为虚拟默认项目 ID（含空串，归默认项目）。
// Python: gateway/common/work_mode.py is_default_project_id。
func IsDefaultProjectID(projectID string) bool {
	if projectID == "" {
		return true
	}
	return projectID == DefaultProjectIDWork || projectID == DefaultProjectIDCode
}

// ResolveDefaultProjectID 按 work_mode 返回默认项目 ID。
// Python: gateway/common/work_mode.py resolve_default_project_id。
func ResolveDefaultProjectID(workMode string) string {
	if workMode == WorkModeCode {
		return DefaultProjectIDCode
	}
	return DefaultProjectIDWork
}

// NormalizeWorkMode 归一工作模式，非法回退 default（对齐 normalize_work_mode）。
func NormalizeWorkMode(raw any, defaultMode string) string {
	if s, ok := raw.(string); ok {
		trimmed := trimLower(s)
		if supportedWorkModes[trimmed] {
			return trimmed
		}
	}
	if supportedWorkModes[trimLower(defaultMode)] {
		return trimLower(defaultMode)
	}
	return WorkModeWeb
}

// DefaultWorkModeForChannel 按通道推断默认 work_mode：tui→code，其他→work。
// Python: work_mode.py default_work_mode_for_channel。
func DefaultWorkModeForChannel(channelID string) string {
	if trimLower(channelID) == "tui" {
		return WorkModeCode
	}
	return WorkModeWeb
}

// ResolveRequestWorkMode 从请求参数解析 work_mode，严格校验：
// 未传/空 → 按通道默认；非法 → BAD_REQUEST。
// Python: work_mode.py resolve_request_work_mode。返回 (mode, errCode)。
func ResolveRequestWorkMode(params map[string]any, channelID string) (string, string) {
	if params == nil {
		return DefaultWorkModeForChannel(channelID), ""
	}
	raw, present := params["work_mode"]
	if !present {
		return DefaultWorkModeForChannel(channelID), ""
	}
	if raw == nil {
		return DefaultWorkModeForChannel(channelID), ""
	}
	s, isStr := raw.(string)
	if isStr && trimLower(s) == "" {
		return DefaultWorkModeForChannel(channelID), ""
	}
	if isStr && supportedWorkModes[trimLower(s)] {
		return trimLower(s), ""
	}
	return "", "BAD_REQUEST"
}

// trimLower 空格裁剪 + 小写（ASCII 语义，对齐 Python str.strip().lower() 的常用面）。
func trimLower(s string) string {
	return asciiLower(strings.TrimSpace(s))
}

// asciiLower 仅小写化 ASCII 字母（work_mode 取值均为 ASCII）。
func asciiLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
