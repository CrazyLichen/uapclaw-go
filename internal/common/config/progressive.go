// progressive.go：渐进式工具暴露开关读取。
//
// 对应 Python 代码：jiuwenswarm/common/config.py:433-449
// （DEFAULT_PROGRESSIVE_TOOL_ENABLED / get_progressive_tool_enabled）
package config

import "strings"

// ──────────────────────────── 常量 ────────────────────────────

// DefaultProgressiveToolEnabled 缺省关闭（历史行为：缺键 = 关）。
// Python: DEFAULT_PROGRESSIVE_TOOL_ENABLED (config.py:64)
const DefaultProgressiveToolEnabled = false

// DefaultProgressiveToolOptimize 检索层开关缺省关闭。
// Python: DEFAULT_PROGRESSIVE_TOOL_OPTIMIZE (config.py:68)
const DefaultProgressiveToolOptimize = false

// ──────────────────────────── 导出函数 ────────────────────────────

// ProgressiveToolEnabled 返回 ProgressiveToolRail 是否启用。
// 开关是 config.yaml 顶层 progressive_tool_enabled 键；缺键保持历史缺省。
// Python: get_progressive_tool_enabled (config.py:433-449)
func ProgressiveToolEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return DefaultProgressiveToolEnabled
	}
	value, ok := cfg["progressive_tool_enabled"]
	if !ok {
		return DefaultProgressiveToolEnabled
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch lower := toLowerFold(v); lower {
		case "1", "true", "yes", "on", "enabled":
			return true
		}
		return false
	}
	// Python: bool(value)——非空容器/数字非零为真。
	return value != nil && value != false && value != ""
}

// toLowerFold ASCII 小写（Python str.lower() 的保守对应）。
func toLowerFold(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

// ProgressiveToolOptimize 返回 tool_search 检索层开关。
// Python: get_progressive_tool_optimize (config.py:451-465)——progressive_tool_enabled
// 的伴随开关：两者同时为真才挂载检索层（AbilityManager deferred 分配保持武装时
// 检索层可单独关闭）。
func ProgressiveToolOptimize(cfg map[string]any) bool {
	if cfg == nil {
		return DefaultProgressiveToolOptimize
	}
	value, ok := cfg["progressive_tool_optimize"]
	if !ok {
		return DefaultProgressiveToolOptimize
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch toLowerFold(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on", "enabled":
			return true
		}
		return false
	}
	// Python: bool(value)。
	return value != nil && value != false && value != ""
}

// ToolRetrievalEnabled 返回 tool_search 渐进披露是否启用。
// Python: is_tool_retrieval_enabled (config.py:469-479)——双顶层开关同时为真：
// progressive_tool_enabled（AbilityManager deferred 分配）与
// progressive_tool_optimize（检索层本身）。
func ToolRetrievalEnabled(cfg map[string]any) bool {
	return ProgressiveToolEnabled(cfg) && ProgressiveToolOptimize(cfg)
}
