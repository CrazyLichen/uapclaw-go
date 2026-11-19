// progressive.go：渐进式工具暴露开关读取。
//
// 对应 Python 代码：jiuwenswarm/common/config.py:433-449
// （DEFAULT_PROGRESSIVE_TOOL_ENABLED / get_progressive_tool_enabled）
package config

// ──────────────────────────── 常量 ────────────────────────────

// DefaultProgressiveToolEnabled 缺省关闭（历史行为：缺键 = 关）。
// Python: DEFAULT_PROGRESSIVE_TOOL_ENABLED (config.py:64)
const DefaultProgressiveToolEnabled = false

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
