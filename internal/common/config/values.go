package config

import (
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// FloatValue 把配置中的单个值转成浮点数。
//
// 认三类形态，缺一不可：
//   - 整数：yaml.v3 把 `timeout: 1800` 解成 int（超界为 int64/uint64）
//   - 浮点：encoding/json 恒为 float64，带 UseNumber 时为 json.Number
//   - 纯数字字符串：配置支持 ${ENV:-default} 占位符，替换后拿到的是字符串
//     （`timeout: ${TIMEOUT:-1800}` → "1800"），不认就会静默回落默认值
//
// 注意与 toFloat64 的分工：toFloat64 只认数值形态，专供 mapsEqual 用——
// 那里若把 "1800" 和 1800 判等，配置合并会把「字符串→数字」的类型改动悄悄
// 抹平。语义不同的两件事不共用一个"宽松"实现。
func FloatValue(v any) (float64, bool) {
	if f, ok := toFloat64(v); ok {
		return f, true
	}
	if s, ok := v.(string); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

// IntValue 把配置中的单个值转成整数（小数截断），非数值返回 false。
func IntValue(v any) (int, bool) {
	f, ok := FloatValue(v)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// Float 从配置 map 中按 key 读取浮点数值（FloatValue 的取键版）。
func Float(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	return FloatValue(m[key])
}

// Int 从配置 map 中按 key 读取整数值（IntValue 的取键版）。
func Int(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	return IntValue(m[key])
}
