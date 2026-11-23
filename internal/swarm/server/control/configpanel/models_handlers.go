// 模型读写处理器。
// 对应 Python 代码：jiuwenswarm/common/config_panel/models_handlers.py（1074 行）。
// 本文件先落展示助手（reasoning_level_display）与上下文窗口解析
// （common/context_window.py 移植）；validate/list/replace_all 主处理器随后续任务。
package configpanel

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// DefaultContextWindowTokens 默认上下文窗口（Python: context_window.py:11）。
const DefaultContextWindowTokens = 256 * 1024

// MaxContextWindowTokens 上限（Python: context_window.py:12）。
const MaxContextWindowTokens = int64(1)<<53 - 1

// contextWindowPattern 数量 + 可选单位（Python: context_window.py:13-16）。
var contextWindowPattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(tokens?|k(?:i?b)?|m(?:i?b)?)?$`)

// contextWindowUnitMultipliers 单位倍率（Python: context_window.py:17）。
var contextWindowUnitMultipliers = map[string]float64{
	"tokens": 1,
	"token":  1,
	"k":      1024,
	"kb":     1024,
	"kib":    1024,
	"m":      1024 * 1024,
	"mb":     1024 * 1024,
	"mib":    1024 * 1024,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ReasoningLevelDisplay 存储的 reasoning_level 规范化为展示串
// （Python: models_handlers.py:110 reasoning_level_display）。
// 旧 YAML 裸 on/off 会被 YAML 1.1 解析成布尔——映射回字符串，
// 前端与 replace_all 变更检测都不见裸布尔。
func ReasoningLevelDisplay(value any) string {
	if b, ok := value.(bool); ok {
		if b {
			return "on"
		}
		return "off"
	}
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

// ParsePositiveInt 带大小写无关 K/M 单位的正整数解析
// （Python: context_window.py:31 parse_positive_int）。
// 支持 256k / 256 K / 1m / 纯数字；非法回 0（调用方以 0 判缺失）。
func ParsePositiveInt(value any) int {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
		return 0
	case int64:
		if v > 0 {
			return int(v)
		}
		return 0
	case float64:
		if v > 0 {
			return int(v)
		}
		return 0
	case string:
		m := contextWindowPattern.FindStringSubmatch(strings.TrimSpace(v))
		if m == nil {
			return 0
		}
		num, err := strconv.ParseFloat(m[1], 64)
		if err != nil || num <= 0 {
			return 0
		}
		multiplier := 1.0
		if unit := strings.ToLower(m[2]); unit != "" {
			mult, ok := contextWindowUnitMultipliers[unit]
			if !ok {
				return 0
			}
			multiplier = mult
		}
		result := num * multiplier
		if result > float64(MaxContextWindowTokens) {
			return int(MaxContextWindowTokens)
		}
		return int(result)
	}
	return 0
}

// parsePositiveIntOr 便捷：解析失败回 fallback。
func parsePositiveIntOr(value any, fallback int) int {
	if n := ParsePositiveInt(value); n > 0 {
		return n
	}
	return fallback
}
