package ability

import (
	"os"
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultToolCallTimeout 卡片未声明预算时的默认外层调用预算（秒）。
	// 作为"工具自身无超时"时的兜底，防止卡死的 tool.invoke 永久阻塞整轮 task_loop。
	// 工具可在 ToolCard.Properties["resilience"]["timeout_s"] 声明覆盖。
	// Python: DEFAULT_TOOL_CALL_TIMEOUT (ability_manager.py:241)
	defaultToolCallTimeout = 300.0

	// maxToolCallTimeoutHardLimit 全局绝对硬上限（秒）。
	// 即使工具显式豁免外层超时（timeout_s=None），也不能超过此上限，
	// 防止"合法挂起"占满整轮。
	// Python: MAX_TOOL_CALL_TIMEOUT_HARD_LIMIT (ability_manager.py:245-247)
	maxToolCallTimeoutHardLimit = 3600.0
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveCallTimeout 解析卡片级外层调用预算。
// 解析顺序（逐条对齐 Python）：
//  1. ToolCard.Properties["resilience"]["timeout_s"] 显式声明：
//     nil 或非正数 → 返回 nil（豁免，由工具内部超时管辖）；正数 → 该值
//  2. 无 resilience 块 / 无 timeout_s 键 / 类型非法 → DEFAULT_TOOL_CALL_TIMEOUT
//
// 返回 nil 表示豁免，调用方必须用 effectiveCallTimeout 提升为硬上限。
// Python: AbilityManager._resolve_call_timeout (ability_manager.py:679-713)
func resolveCallTimeout(card *tool.ToolCard) *float64 {
	fallback := envFloat("DEFAULT_TOOL_CALL_TIMEOUT", defaultToolCallTimeout)

	if card == nil || card.Properties == nil {
		return &fallback
	}
	resilience, ok := card.Properties["resilience"].(map[string]any)
	if !ok {
		return &fallback
	}
	declared, hasKey := resilience["timeout_s"]
	if !hasKey {
		return &fallback
	}
	if declared == nil {
		// 显式声明 null：豁免外层超时
		return nil
	}
	value, ok := toFloat(declared)
	if !ok {
		return &fallback
	}
	if value <= 0 {
		// 非正数同样视为豁免（上游：``None`` or non-positive → None）
		return nil
	}
	return &value
}

// effectiveCallTimeout 把豁免（nil）提升为硬上限。
// 豁免不等于无限：没有这一层，声明 timeout_s=None 的工具可以永久挂住整轮。
func effectiveCallTimeout(budget *float64) float64 {
	if budget == nil {
		return envFloat("MAX_TOOL_CALL_TIMEOUT_HARD_LIMIT", maxToolCallTimeoutHardLimit)
	}
	return *budget
}

// resolveInvocationTimeout 应用工具的输入感知预算钩子。
// 卡片预算只是起点；有内部执行期限的工具（bash/powershell）会把
// 执行预算加上清理宽限后返回，使外层超时晚于内层超时。
// Python: AbilityManager._resolve_invocation_timeout (ability_manager.py:715-729)
func resolveInvocationTimeout(tl tool.Tool, inputs map[string]any, defaultTimeout float64) float64 {
	if tl == nil {
		return defaultTimeout
	}
	return tl.GetCallTimeout(inputs, defaultTimeout)
}

// envFloat 读取浮点型环境变量，缺失或非法时返回默认值。
func envFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

// toFloat 把任意数值转成 float64。
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
