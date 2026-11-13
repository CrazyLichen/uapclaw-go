package router

import (
	"fmt"
	"math"
	"strings"
	"time"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// depError (deployment_id, error_message) 二元组。← py:931 errors.append((selected.id, str(e)))
type depError struct {
	id  string
	msg string
}

// ──────────────────────────── 非导出函数 ────────────────────────────
// reliable_router.go 引用的小工具（Python 内联表达式 → Go 助手）。

// nowSec 当前秒级时间戳（对齐 time.time()）。
func nowSec() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// minInt min(a, b)。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// intOfAny usage 字段取值（py:496 `usage.get("x") or 0`：None/缺省 → 0）。
func intOfAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	default:
		return 0
	}
}

// strOfAny any → string（None/缺省 → 空串）。
func strOfAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// truthy Python truthiness（非空判定）。
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != ""
	case bool:
		return t
	case int:
		return t != 0
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return true
	}
}

// isBool Python isinstance(v, bool) 判定。
func isBool(v any) bool {
	_, ok := v.(bool)
	return ok
}

// toFloat64 Python float(v) 等价（int/float64/数字字符串）。
func toFloat64(v any) (float64, error) {
	switch t := v.(type) {
	case int:
		return float64(t), nil
	case float64:
		return t, nil
	case string:
		var f float64
		_, err := fmt.Sscanf(strings.TrimSpace(t), "%g", &f)
		return f, err
	default:
		return 0, fmt.Errorf("not a number")
	}
}

// toFloatMust 宽松转 float（失败回 0；weights 场景）。
func toFloatMust(v any) float64 {
	f, _ := toFloat64(v)
	return f
}

// mapOf any → map（非 dict 返回 nil，对齐 py isinstance(usage, dict) 判定）。
func mapOf(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// orAny `a or b` 语义（a 为 nil/零值取 b）。
func orAny(a, b any) any {
	if truthy(a) {
		return a
	}
	return b
}

// orDefault 空串取默认（py:1611 tc.get("type", "function")）。
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// orDefaultAny 非 nil 取 a，否则 b（py:1626 raw.get(k, raw)）。
func orDefaultAny(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

// strPtrOrNil 字符串指针（空串仍保留——调用方决定 None 语义）。
func strPtrOrNil(s string) *string {
	return &s
}

// ptrFloat *float64 构造。
func ptrFloat(f float64) *float64 { return &f }

// firstNonNil 取第一个非 nil map。
func firstNonNil(maps ...map[string]any) map[string]any {
	for _, m := range maps {
		if m != nil {
			return m
		}
	}
	return map[string]any{}
}

// contextErrMsg str(context.error) if context.error else None。
func contextErrMsg(c *intelliroutercore.RoutingContext) *string {
	if c.Error != nil {
		return ptrStr(c.Error.Error())
	}
	return nil
}

// round4 四舍五入 4 位小数（py:1715 round(raw_latency, 4) 浮点噪声控制）。
func round4(f float64) float64 {
	return math.Round(f*10000) / 10000
}

// floatInf float('inf')。
func floatInf() float64 {
	return math.Inf(1)
}

// nilToNil 值包装为 any（指针 nil → nil）。
func nilToNil(v any) any { return v }

// ptrStr 字符串指针。
func ptrStr(s string) *string { return &s }

// floatPtrOf float → 指针。
func floatPtrOf(f float64) *float64 { return &f }
