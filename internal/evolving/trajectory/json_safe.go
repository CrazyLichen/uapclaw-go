package trajectory

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// JSONSafe 递归转换常见消息/工具调用对象为可 JSON 序列化的值。
//
// 处理规则：
//   - nil → nil
//   - string/int/float64/bool → 原值
//   - []any → 递归每个元素
//   - map[string]any → 递归 value
//   - 其他类型 → json.Marshal→json.Unmarshal 到 any（兜底，利用 Go JSON 序列化链）
//   - Marshal 失败 → fmt.Sprint(value) 转字符串
//
// 对齐 Python:
//
//	if value is None or isinstance(value, (str, int, float, bool)):
//	    return value
//	if isinstance(value, (list, tuple)):
//	    return [_json_safe(item) for item in value]
//	if isinstance(value, dict):
//	    return {str(key): _json_safe(item) for key, item in value.items()}
//	model_dump = getattr(value, "model_dump", None)
//	if callable(model_dump):
//	    dumped = model_dump()
//	    if isinstance(dumped, dict):
//	        return _json_safe(dumped)
//	return str(value)
//
// 对应 Python: _json_safe(value)
func JSONSafe(value any) any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string, int, float64, bool:
		return v
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = JSONSafe(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = JSONSafe(item)
		}
		return result
	default:
		// 兜底：json.Marshal→json.Unmarshal，利用 Go 的 json.Marshaler 接口
		// 等价于 Python 的 getattr(value, "model_dump", None)
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		var unmarshalled any
		if err := json.Unmarshal(b, &unmarshalled); err != nil {
			return fmt.Sprint(value)
		}
		return JSONSafe(unmarshalled)
	}
}

// MessageToDict 将运行时消息对象标准化为消息类字典。
//
// 处理规则：
//  1. 已经是 map[string]any → 直接 JSONSafe
//  2. 尝试 json.Marshal→json.Unmarshal 到 map[string]any → JSONSafe
//  3. 兜底 → {"role": "unknown", "content": fmt.Sprint(msg)}
//
// 对齐 Python:
//
//	if isinstance(message, dict):
//	    return _json_safe(message)
//	role = getattr(message, "role", None)
//	if role is not None:
//	    item = {"role": role, "content": _json_safe(getattr(message, "content", ""))}
//	    ...
//	    return item
//	model_dump = getattr(message, "model_dump", None)
//	if callable(model_dump):
//	    dumped = model_dump()
//	    if isinstance(dumped, dict):
//	        return _json_safe(dumped)
//	return {"role": "unknown", "content": str(message)}
//
// 对应 Python: Trajectory._message_to_dict(message)
func MessageToDict(msg any) map[string]any {
	if msg == nil {
		return map[string]any{"role": "unknown", "content": ""}
	}
	// 分支1: 已经是 map
	if m, ok := msg.(map[string]any); ok {
		return JSONSafe(m).(map[string]any)
	}
	// 分支2+3: 尝试 JSON 序列化（等价于 Python getattr + model_dump）
	b, err := json.Marshal(msg)
	if err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			return JSONSafe(m).(map[string]any)
		}
	}
	// 兜底
	return map[string]any{"role": "unknown", "content": fmt.Sprint(msg)}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// responseToText 从 LLM 响应中提取文本内容。
//
// 处理规则：
//  1. 有 Content 字段（断言为有 Content 方法的接口）→ 返回 Content
//  2. map[string]any → 取 "content" 或 "text" 键
//  3. 兜底 → fmt.Sprint(response)
//
// 对齐 Python:
//
//	if hasattr(response, "content"):
//	    return str(response.content or "")
//	if isinstance(response, dict):
//	    return str(response.get("content", "") or response.get("text", "") or "")
//	return str(response or "")
//
// 对应 Python: _response_to_text(response)
func responseToText(response any) string {
	if response == nil {
		return ""
	}
	// 分支1: 有 Content 方法（对齐 Python hasattr(response, "content")）
	type contenter interface {
		Content() string
	}
	if c, ok := response.(contenter); ok {
		return c.Content()
	}
	// 分支2: map[string]any（对齐 Python isinstance(response, dict)）
	if m, ok := response.(map[string]any); ok {
		if content, _ := m["content"].(string); content != "" {
			return content
		}
		if text, _ := m["text"].(string); text != "" {
			return text
		}
	}
	// 分支3: 兜底（对齐 Python return str(response or "")）
	return fmt.Sprint(response)
}
