// 任务级权限模式判定，供 Gateway 配置层与 agent 运行时共用。
//
// 对应 Python 代码：jiuwenswarm/common/permission_mode.py（47 行）
//
// 本包只读 `permissions.mode` 字符串，不做任何归一化改写——
// 运行时的配置归一化在 permissions 包的 auto_config 里完成。

package permission_mode

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ManualPermissionMode 手动权限模式（默认）
	ManualPermissionMode = "manual"
	// AutoPermissionMode 自动审查权限模式
	AutoPermissionMode = "auto"
	// logComponent 日志组件
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// validRuntimeModes 合法运行时模式集合
var validRuntimeModes = map[string]struct{}{
	ManualPermissionMode: {},
	AutoPermissionMode:   {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolvePermissionRuntimeMode 返回任务级权限运行时模式。
//
// Python: resolve_permission_runtime_mode(permission_config)
// 未知取值回落 manual 并记警告。
func ResolvePermissionRuntimeMode(permissionConfig map[string]any) string {
	rawMode := ManualPermissionMode
	if permissionConfig != nil {
		if raw, exists := permissionConfig["mode"]; exists && raw != nil {
			if text, ok := raw.(string); ok {
				rawMode = trimLower(text)
			}
		}
	}
	if _, valid := validRuntimeModes[rawMode]; valid {
		return rawMode
	}
	logger.Warn(logComponent).
		Str("raw_mode", rawMode).
		Msg("unknown permissions.mode; falling back to manual")
	return ManualPermissionMode
}

// IsAutoPermissionMode 是否选择了任务级自动权限模式。
//
// Python: is_auto_permission_mode(permission_config)
func IsAutoPermissionMode(permissionConfig map[string]any) bool {
	return ResolvePermissionRuntimeMode(permissionConfig) == AutoPermissionMode
}

// IsPermissionBoundaryEnabled 权限边界是否被显式启用。
//
// Python: is_permission_boundary_enabled(permission_config)
// 严格判等 true（Python 的 `is True`），非布尔真值不算。
func IsPermissionBoundaryEnabled(permissionConfig map[string]any) bool {
	if permissionConfig == nil {
		return false
	}
	raw, exists := permissionConfig["enabled"]
	if !exists {
		return false
	}
	enabled, ok := raw.(bool)
	return ok && enabled
}

// IsAutoPermissionEnabled 任务级自动权限是否具备激活条件。
//
// Python: is_auto_permission_enabled(permission_config)
func IsAutoPermissionEnabled(permissionConfig map[string]any) bool {
	return IsPermissionBoundaryEnabled(permissionConfig) &&
		IsAutoPermissionMode(permissionConfig)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// trimLower 去首尾空白并转小写（Python 的 str().strip().lower()）。
func trimLower(text string) string {
	start := 0
	end := len(text)
	for start < end && isSpace(text[start]) {
		start++
	}
	for end > start && isSpace(text[end-1]) {
		end--
	}
	trimmed := text[start:end]
	lowered := make([]byte, len(trimmed))
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		lowered[i] = c
	}
	return string(lowered)
}

// isSpace 是否 ASCII 空白。
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}
