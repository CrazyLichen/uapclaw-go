package config

import (
	"encoding/json"
	"log"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeConfig 配置后处理，将需要结构化的字段解析为原生类型。
//
// Python: _normalize_config(config)
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

	// 3. channels.web.send_file_allowed 缺省 true（Python L100-103）
	// Python: if channels.get("web", {}).get("send_file_allowed") is None:
	//             channels["web"] = {"send_file_allowed": True}
	// 严格对齐 Python 替换语义：web 缺键或 send_file_allowed 显式 null（YAML ~）
	// 时整个替换 channels.web（web 下其他键随之丢失，与 Python 一致）。
	// channels 节不存在时不操作（Python .get("channels", {}) 返回脱离
	// config 的临时 dict，修改不影响原 config）。web 为非 map 时 Python
	// 抛 AttributeError，Go 防御性替换（吞异常路径对齐）。
	if channels, ok := data["channels"].(map[string]any); ok {
		needReplace := true
		if web, ok := channels["web"].(map[string]any); ok {
			if v, exists := web["send_file_allowed"]; exists && v != nil {
				needReplace = false
			}
		}
		if needReplace {
			channels["web"] = map[string]any{"send_file_allowed": true}
		}
	}
}

// ParseCustomHeaders 解析 custom_headers 配置，支持 JSON 字符串格式。
//
// 如果输入已经是 map 类型则原样返回；如果是 JSON 字符串则解析为 map；
// 其他类型或解析失败返回 nil。
// Python: _parse_custom_headers(value)
func ParseCustomHeaders(value any) map[string]any {
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
		// Python: logger.warning(f"custom_headers JSON parse failed: {e}")
		// config 包因循环依赖（logger → config）不能导入 logger，改用标准库 log.Printf
		log.Printf("[config] custom_headers JSON 解析失败: value=%s, err=%v", truncateString(s, 100), err)
		return nil
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateString 截断字符串到指定长度。
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
