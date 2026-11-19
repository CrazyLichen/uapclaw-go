package config

// ──────────────────────────── 导出函数 ────────────────────────────

// String 取 any 的字符串值：string 原样；其余得空串。
// 对齐全仓既有 strVal/asString/stringValue 等逐字同义助手的统一口径；
// Python 侧对应 dict 取值后 str() 断言失败的默认空串语义。
func String(v any) string {
	s, _ := v.(string)
	return s
}

// StringList 取 any 的字符串列表：[]string 原样透传；[]any 过滤取字符串元素；
// 其余得 nil（JSON 序列化为 null）。
func StringList(v any) []string {
	switch arr := v.(type) {
	case []string:
		return arr
	case []any:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// StringListOrEmpty 同 StringList，但非列表输入得空切片（JSON 序列化为 []）。
// 用于落盘 JSON 形状敏感的场景（如 external-cli-models.json 的 efforts 字段）。
func StringListOrEmpty(v any) []string {
	if list := StringList(v); list != nil {
		return list
	}
	return []string{}
}

// DictList 取 any 的字典列表：仅保留 map[string]any 元素，其余滤除。
func DictList(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if dict, ok := item.(map[string]any); ok {
			out = append(out, dict)
		}
	}
	return out
}

// IsEmptyList 判断 any 是否为空列表或非列表（对齐 Python `not isinstance(list) or not list`）。
func IsEmptyList(v any) bool {
	list, ok := v.([]any)
	return !ok || len(list) == 0
}
