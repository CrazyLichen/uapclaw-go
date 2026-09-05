// Package utils 提供通用工具函数。
//
// deepcopy.go 实现 map[string]any 的深拷贝，替代各包中重复的 deepCopyMap 实现。
package utils

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DeepCopyMap 深拷贝 map[string]any。
// 递归处理嵌套 map 和 slice，其余类型浅拷贝。
// 统一替代各包中重复定义的 deepCopyMap 函数。
func DeepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = DeepCopyMap(val)
		case []any:
			result[k] = DeepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// DeepCopySlice 深拷贝 []any。
// 递归处理嵌套 map 和 slice，其余类型浅拷贝。
func DeepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = DeepCopyMap(val)
		case []any:
			result[i] = DeepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────
