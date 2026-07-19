package tool_call

import "encoding/json"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractSchema 从 JSON Schema 字典提取结构骨架，去除类型信息。
// 递归处理嵌套字典，保留列表原样，将原始值替换为空字符串。
//
// 对齐 Python: extract_schema(schema_dict)
//
//	if not isinstance(schema_dict, dict):
//	    try: schema_dict = json.loads(schema_dict)
//	    except: return {}
//	result = {}
//	for key, value in schema_dict.items():
//	    if isinstance(value, dict):      result[key] = extract_schema(value)
//	    elif isinstance(value, list):    result[key] = value
//	    else:                            result[key] = ""
//	return result
func ExtractSchema(schemaDict map[string]any) map[string]any {
	result := make(map[string]any, len(schemaDict))
	for key, value := range schemaDict {
		switch v := value.(type) {
		case map[string]any:
			// 对齐 Python: result[key] = extract_schema(value)
			result[key] = ExtractSchema(v)
		case []any:
			// 对齐 Python: result[key] = value（保留列表原样，如 required 数组）
			result[key] = v
		default:
			// 对齐 Python: result[key] = ""（原始值替换为空字符串）
			result[key] = ""
		}
	}
	return result
}

// ExtractSchemaFromJSON 从 JSON 字符串提取结构骨架。
// 如果输入不是 dict，尝试 json.Unmarshal。
//
// 对齐 Python: extract_schema(schema_dict) 中 isinstance(schema_dict, dict) 的 else 分支
//
//	try: schema_dict = json.loads(schema_dict)
//	except: return {}
func ExtractSchemaFromJSON(jsonStr string) map[string]any {
	var schemaDict map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &schemaDict); err != nil {
		return map[string]any{}
	}
	return ExtractSchema(schemaDict)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
