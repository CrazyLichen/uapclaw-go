// Package cua 提供 cua（computer-use）桌面子代理的能力目录与任务级解析。
//
// 工具名为 desktop_tools.CUA_DESKTOP_TOOL_NAMES 中的裸名（对齐上游
// harness/tools/cua/cua_capabilities.py）。本包只承载受信任的能力目录与
// 解析器；桌面工具链本体（desktop_tools/backend/rails）⤴️ 领域十八。
//
// 对应 Python 代码：openjiuwen/harness/tools/cua/cua_capabilities.py
package cua

import (
	"errors"
	"fmt"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CoreCuaCapabilityName 核心只读观察能力名。
	// Python: CORE_CUA_CAPABILITY_NAME (cua_capabilities.py:14)
	CoreCuaCapabilityName = "core"
)

// 核心只读工具名（显式列出，禁用前缀匹配——避免新 cua 工具未经能力策略
// 审查就被意外暴露）。
// Python: CORE_CUA_TOOL_NAMES (cua_capabilities.py:17-24)
var CoreCuaToolNames = []string{
	"get_cursor_position",
	"get_screen_size",
	"get_window_state",
	"list_apps",
	"list_windows",
	"zoom",
}

// 鼠标键盘输入工具名。
// Python: INPUT_CUA_TOOL_NAMES (cua_capabilities.py:26-36)
var InputCuaToolNames = []string{
	"click",
	"double_click",
	"drag",
	"hotkey",
	"move_cursor",
	"press_key",
	"right_click",
	"scroll",
	"type_text",
}

// 应用生命周期工具名。
// Python: APP_LIFECYCLE_CUA_TOOL_NAMES (cua_capabilities.py:38-41)
var AppLifecycleCuaToolNames = []string{
	"bring_to_front",
	"launch_app",
}

// 强制终止工具名（默认授权之外：cua 权限规则 deny kill_app，
// 仅运营显式要求时提供）。
// Python: APP_TERMINATION_CUA_TOOL_NAMES (cua_capabilities.py:43-44)
var AppTerminationCuaToolNames = []string{"kill_app"}

// ──────────────────────────── 结构体 ────────────────────────────

// CuaCapability 一个批准暴露给主代理的 cua 能力。
// Python: CuaCapability (cua_capabilities.py:47-51)
type CuaCapability struct {
	// Name 能力名
	Name string
	// Description 描述（主代理据此选择能力）
	Description string
	// ToolNames 该能力授予的工具名
	ToolNames []string
}

// ResolvedCuaCapabilities 校验后的任务级能力与工具选择。
// Python: ResolvedCuaCapabilities (cua_capabilities.py:54-59)
type ResolvedCuaCapabilities struct {
	// RequestedNames 请求的能力名（稳定去重后）
	RequestedNames []string
	// SelectedNames 最终选中的能力名（core 恒在）
	SelectedNames []string
	// RejectedNames 未知名
	RejectedNames []string
	// AllowedToolNames 展开后的任务级工具允许清单
	AllowedToolNames []string
}

// DefaultCuaCapabilities 受信任能力目录。
// Python: DEFAULT_CUA_CAPABILITIES (cua_capabilities.py:62-89)
var DefaultCuaCapabilities = []CuaCapability{
	{
		Name: CoreCuaCapabilityName,
		Description: "Observe the desktop read-only: list apps and windows, " +
			"and capture or zoom into a window's screenshot.",
		ToolNames: CoreCuaToolNames,
	},
	{
		Name:        "input",
		Description: "Send mouse and keyboard input to desktop apps: click, drag, scroll, type, and press keys.",
		ToolNames:   InputCuaToolNames,
	},
	{
		Name:        "app_lifecycle",
		Description: "Launch or foreground desktop applications.",
		ToolNames:   AppLifecycleCuaToolNames,
	},
	{
		Name:        "app_termination",
		Description: "Force-terminate desktop applications; unsaved state is lost.",
		ToolNames:   AppTerminationCuaToolNames,
	},
}

// DefaultCuaAgentCapabilityNames 调用方未指定选择时 cua 工厂默认授予的能力
// （core 由解析器恒定补入，不在此列）。
// Python: DEFAULT_CUA_AGENT_CAPABILITY_NAMES (cua_capabilities.py:93-94)
var DefaultCuaAgentCapabilityNames = []string{"input", "app_lifecycle"}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stableUnique 规范化名字并保持首现顺序。
// Python: _stable_unique (cua_capabilities.py:97-106)
func stableUnique(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		item := trimOrEmpty(value)
		if item == "" {
			continue
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

// trimOrEmpty 对齐 Python str(value or "").strip()：零值字符串归空。
func trimOrEmpty(value string) string {
	// Python 侧 or "" 只把 None/空串归空，Go string 零值即空串
	return strings.TrimSpace(value)
}

// buildCapabilityIndex 构建并校验受信任能力目录。
// Python: _build_capability_index (cua_capabilities.py:109-121)
func buildCapabilityIndex(available []CuaCapability) (map[string]CuaCapability, error) {
	catalog := make(map[string]CuaCapability, len(available))
	for _, capability := range available {
		name := strings.TrimSpace(capability.Name)
		if name == "" {
			return nil, errors.New("Cua capability name cannot be empty.")
		}
		if _, dup := catalog[name]; dup {
			return nil, fmt.Errorf("Duplicate cua capability name: %s", name)
		}
		catalog[name] = capability
	}
	if _, ok := catalog[CoreCuaCapabilityName]; !ok {
		return nil, fmt.Errorf("Cua capability catalog must include %q.", CoreCuaCapabilityName)
	}
	return catalog, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveCuaCapabilities 校验主代理的能力选择并展开为工具允许清单。
//
// 主代理负责按描述选择能力；本解析器不做任务解释：恒补 core 能力、
// 拒绝未知名、把批准的能力名展开为确定的任务级工具允许清单。
//
// Python: resolve_cua_capabilities (cua_capabilities.py:124-155)
func ResolveCuaCapabilities(requestedNames []string, available []CuaCapability) (*ResolvedCuaCapabilities, error) {
	if available == nil {
		available = DefaultCuaCapabilities
	}
	catalog, err := buildCapabilityIndex(available)
	if err != nil {
		return nil, err
	}
	requested := stableUnique(requestedNames)

	selected := []string{CoreCuaCapabilityName}
	rejected := make([]string, 0)
	for _, name := range requested {
		if _, ok := catalog[name]; !ok {
			rejected = append(rejected, name)
			continue
		}
		if !containsStr(selected, name) {
			selected = append(selected, name)
		}
	}

	allowed := make([]string, 0)
	for _, capabilityName := range selected {
		allowed = append(allowed, catalog[capabilityName].ToolNames...)
	}
	allowed = stableUnique(allowed)

	return &ResolvedCuaCapabilities{
		RequestedNames:   requested,
		SelectedNames:    selected,
		RejectedNames:    rejected,
		AllowedToolNames: allowed,
	}, nil
}

// containsStr 判断列表是否含目标串。
func containsStr(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
