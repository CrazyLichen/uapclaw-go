package reme

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	logger "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// jsonBlockPattern 匹配 ```json ... ``` 代码块
var jsonBlockPattern = regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```")

// numberPattern 匹配独立的数字
var numberPattern = regexp.MustCompile(`\b\d+\b`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseJSONListResponse 从 LLM 响应中解析整数列表。
// 对齐 Python parse_json_list_response(response, key)。
// 优先从 ```json``` 代码块解析，其次从纯 JSON 解析，最后回退到数字提取。
func ParseJSONListResponse(response string, key string) []int {
	// 尝试从 ```json``` 代码块解析
	jsonBlocks := jsonBlockPattern.FindAllStringSubmatch(response, -1)
	if len(jsonBlocks) > 0 {
		var parsed any
		if err := json.Unmarshal([]byte(jsonBlocks[0][1]), &parsed); err == nil {
			switch v := parsed.(type) {
			case map[string]any:
				if indices, ok := v[key]; ok {
					if list, ok := indices.([]any); ok {
						return toIntList(list)
					}
				}
			case []any:
				return toIntList(v)
			}
		}
	}

	// 回退：提取所有独立数字（小于 100）
	numbers := numberPattern.FindAllString(response, -1)
	var result []int
	for _, num := range numbers {
		n, err := strconv.Atoi(num)
		if err == nil && n < 100 {
			result = append(result, n)
		}
	}
	return result
}

// ParseJSONField 从 LLM 响应中解析指定 JSON 字段。
// 对齐 Python parse_json_field(response, key)。
// 优先从 ```json``` 代码块解析，其次从纯 JSON 解析。
// 未找到时返回空字符串。
func ParseJSONField(response string, key string) string {
	// 尝试从 ```json``` 代码块解析
	jsonBlocks := jsonBlockPattern.FindAllStringSubmatch(response, -1)
	if len(jsonBlocks) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(jsonBlocks[0][1]), &parsed); err == nil {
			if val, ok := parsed[key]; ok {
				if s, ok := val.(string); ok {
					return s
				}
				// 非字符串类型，转为字符串
				return fmt.Sprintf("%v", val)
			}
		}
	}

	// 尝试将整个响应作为 JSON 解析
	var parsed map[string]any
	if err := json.Unmarshal([]byte(response), &parsed); err == nil {
		if val, ok := parsed[key]; ok {
			if s, ok := val.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", val)
		}
	}

	// 解析失败
	logger.Warn(logComponent).
		Str("key", key).
		Msg("Failed to parse JSON response field")
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toIntList 将 []any 转换为 []int，跳过非数字元素
func toIntList(list []any) []int {
	result := make([]int, 0, len(list))
	for _, item := range list {
		switch v := item.(type) {
		case float64:
			result = append(result, int(v))
		case int:
			result = append(result, v)
		}
	}
	return result
}
