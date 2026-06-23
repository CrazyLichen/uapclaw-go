package ability

import (
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RepairToolArgumentsJSON 尝试修复畸形的 JSON 字符串，通过补全缺失的闭合括号。
// 返回修复后的字符串指针；无法修复时返回 nil。
//
// 对应 Python: AbilityManager._repair_tool_arguments_json
func RepairToolArgumentsJSON(arguments string) *string {
	text := arguments
	// 去除首尾空白
	for len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n' || text[0] == '\r') {
		text = text[1:]
	}
	for len(text) > 0 && (text[len(text)-1] == ' ' || text[len(text)-1] == '\t' || text[len(text)-1] == '\n' || text[len(text)-1] == '\r') {
		text = text[:len(text)-1]
	}
	if text == "" {
		return nil
	}

	var stack []byte
	inString := false
	escape := false

	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escape {
				escape = false
			} else if ch == '\\' {
				escape = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' || ch == '[' {
			stack = append(stack, ch)
			continue
		}
		if ch == '}' {
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return nil
			}
			stack = stack[:len(stack)-1]
			continue
		}
		if ch == ']' {
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return nil
			}
			stack = stack[:len(stack)-1]
		}
	}

	if inString {
		return nil
	}
	if len(stack) == 0 {
		return &text
	}

	// 按栈逆序补全闭合符号
	suffix := make([]byte, len(stack))
	for i := 0; i < len(stack); i++ {
		opener := stack[len(stack)-1-i]
		if opener == '{' {
			suffix[i] = '}'
		} else {
			suffix[i] = ']'
		}
	}
	repaired := text + string(suffix)
	return &repaired
}

// ParseToolArguments 将工具调用参数解析为 map[string]any。
// 先尝试 json.Unmarshal；失败后尝试 Repair + 再次 Unmarshal；
// 仍失败则返回错误，包含原始 JSON 和错误信息。
//
// 对应 Python: AbilityManager._parse_tool_arguments
func ParseToolArguments(arguments string) (map[string]any, error) {
	if arguments == "" {
		return nil, nil
	}

	// 先尝试直接解析
	var result map[string]any
	if err := json.Unmarshal([]byte(arguments), &result); err == nil {
		return result, nil
	}

	// 尝试修复
	repaired := RepairToolArgumentsJSON(arguments)
	if repaired != nil && *repaired != arguments {
		if err := json.Unmarshal([]byte(*repaired), &result); err == nil {
			logger.Warn(logger.ComponentAgentCore).
				Msg("通过补全闭合括号修复了畸形的工具参数")
			return result, nil
		}
	}

	return nil, fmt.Errorf("invalid tool arguments JSON: raw arguments: %q", arguments)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
