package config

import (
	"encoding/json"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeConfig 配置后处理，将需要结构化的字段解析为原生类型。
//
// 对应 Python: _normalize_config(config)
//  1. models.*.model_client_config.custom_headers — JSON 字符串 → map
//  2. react.model_client_config.custom_headers — JSON 字符串 → map
//  3. channels.web.send_file_allowed — 默认值 true
func NormalizeConfig(data map[string]any) {
	if data == nil {
		return
	}

	// 1. 解析 models 中各条目的 custom_headers
	models, ok := data["models"].(map[string]any)
	if ok {
		for _, entry := range models {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			mcc, ok := entryMap["model_client_config"].(map[string]any)
			if !ok {
				continue
			}
			if raw, exists := mcc["custom_headers"]; exists {
				mcc["custom_headers"] = ParseCustomHeaders(raw)
			}
		}
	}

	// 2. 解析 react.model_client_config.custom_headers
	react, ok := data["react"].(map[string]any)
	if ok {
		if mcc, ok := react["model_client_config"].(map[string]any); ok {
			if raw, exists := mcc["custom_headers"]; exists {
				mcc["custom_headers"] = ParseCustomHeaders(raw)
			}
		}
	}

	// 3. 设置 channels.web.send_file_allowed 默认值
	channels, ok := data["channels"].(map[string]any)
	if ok {
		web, ok := channels["web"].(map[string]any)
		if ok {
			if _, exists := web["send_file_allowed"]; !exists {
				web["send_file_allowed"] = true
			}
		} else {
			channels["web"] = map[string]any{"send_file_allowed": true}
		}
	}
}

// ParseCustomHeaders 解析 custom_headers 配置，支持 JSON 字符串格式。
//
// 如果输入已经是 map 类型则原样返回；如果是 JSON 字符串则解析为 map；
// 其他类型或解析失败返回 nil。
// 对应 Python: _parse_custom_headers(value)
func ParseCustomHeaders(value any) any {
	if value == nil {
		return nil
	}

	// 已经是 map，直接返回
	if m, ok := value.(map[string]any); ok {
		return m
	}

	// 字符串类型，尝试 JSON 解析
	s, ok := value.(string)
	if !ok {
		return nil
	}
	if s == "" {
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil
	}
	return result
}
