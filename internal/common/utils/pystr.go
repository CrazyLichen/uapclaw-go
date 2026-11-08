package utils

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// PyText 工具结果的展示文本（前端卡片 / 日志 / 会话历史 / ACP）。
// 只用于「看」，不参与任何机器解析：想判断成功与否请读 raw_output 结构化字段，
// 模型侧走的是 BuildToolMessageContent 的 JSON 通道，与本函数无关。因此这里
// 选最省事且前端友好的形态，而不是复刻 Python 的字面量风格：
//   - nil → ""（对齐 Python 的 `"" if result is not None else ""`）
//   - string → 原样（不加引号）
//   - 其余（map/struct/slice…）→ 紧凑 JSON，如 {"error":"...","success":false}
//   - JSON 序列化失败 → fmt.Sprint 兜底
//
// 历史缺陷：这里曾是 fmt.Sprintf("%v")，Go 对 map 打印成 `map[k:v ...]`，
// 工具卡片与日志里全是 `map[...]` 前缀。
func PyText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}

// PyStr 对齐 Python str() 的**标量**形态（检索串、提示词等参与语义的文本）。
//   - None（nil）→ "None"
//   - str → 原样；bool → "True"/"False"
//   - 数字 → Python 记法（整数值浮点补 ".0"；json.Number 走字面量还原）
//   - 容器与其他类型 → fmt.Sprint 兜底（调用方只传标量；展示请用 PyText）
//
// int/float 的分野必须精确：str(1)="1" 与 str(1.0)="1.0" 在检索词表、抽取结果里
// 是不同 token；json.Number 的超界整数也不能被 Float64 化成 1.2e+19。
func PyStr(v any) string {
	switch val := v.(type) {
	case nil:
		return "None"
	case string:
		return val
	case bool:
		return pyBoolStr(val)
	case json.Number:
		return pyJSONNumberStr(val)
	default:
		if s, ok := pyNumberStr(v); ok {
			return s
		}
		return fmt.Sprint(v)
	}
}

// PyFloatStr 还原 Python str(float) 记法：整数值补 ".0"，nan/inf 用 Python 字面量。
func PyFloatStr(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	abs := math.Abs(f)
	// CPython float_repr 的 decpt 判定：abs ∈ [1e-4, 1e16) 用 f 记法
	// （整数值补 ".0"），其余用 e 记法（1e+16 / 1e-05，指数两位）。
	if abs == 0 || (abs >= 1e-4 && abs < 1e16) {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	return strconv.FormatFloat(f, 'e', -1, 64)
}

// IsPureIntegerLiteral 判断是否为纯整数字面量（可选符号 + 全数字，无小数点/指数）。
// 用于 json.Number 的超界整数还原：ParseInt 溢出后不能落到 Float64（会把
// 12345678901234567890 变成 1.2345678901234567e+19），必须原样透传。
func IsPureIntegerLiteral(s string) bool {
	if s == "" {
		return false
	}
	body := s
	if body[0] == '+' || body[0] == '-' {
		body = body[1:]
	}
	if body == "" {
		return false
	}
	for _, r := range body {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pyBoolStr 对齐 Python str(bool)。
func pyBoolStr(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

// pyNumberStr 把 Go 数字类型转为 Python 记法文本；非数字类型返回 false。
func pyNumberStr(v any) (string, bool) {
	switch n := v.(type) {
	case int:
		return strconv.Itoa(n), true
	case int8:
		return strconv.FormatInt(int64(n), 10), true
	case int16:
		return strconv.FormatInt(int64(n), 10), true
	case int32:
		return strconv.FormatInt(int64(n), 10), true
	case int64:
		return strconv.FormatInt(n, 10), true
	case uint:
		return strconv.FormatUint(uint64(n), 10), true
	case uint8:
		return strconv.FormatUint(uint64(n), 10), true
	case uint16:
		return strconv.FormatUint(uint64(n), 10), true
	case uint32:
		return strconv.FormatUint(uint64(n), 10), true
	case uint64:
		return strconv.FormatUint(n, 10), true
	case float32:
		return PyFloatStr(float64(n)), true
	case float64:
		return PyFloatStr(n), true
	default:
		return "", false
	}
}

// pyJSONNumberStr 还原 json.Number 的 Python str() 形态。
// 整数字面原样输出（Python int 无界，含超出 int64 的超界字面），
// 仅小数/指数字面走 PyFloatStr。
func pyJSONNumberStr(n json.Number) string {
	lit := n.String()
	if _, err := strconv.ParseInt(lit, 10, 64); err == nil {
		return lit
	}
	if IsPureIntegerLiteral(lit) {
		return lit
	}
	if f, err := n.Float64(); err == nil {
		return PyFloatStr(f)
	}
	return lit
}
