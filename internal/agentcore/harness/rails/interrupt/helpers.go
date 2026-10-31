package interrupt

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// ConvertInteractionsToAskUserQuestion 将 __interaction__ payload 转换为前端
// chat.ask_user_question 格式。
//
// Python: convert_interactions_to_ask_user_question(state_outputs)
// (interrupt_helpers.py line 285-339)
//
// AskUserRail 中断：value 有 questions 字段 → source="ask_user_interrupt"
// PermissionRail 中断：value 无 questions 字段 → source="permission_interrupt"
//
// interactions 的元素已在调用边界由 utils.PayloadToMap 收敛为 map——
// Python 的 `state_outputs` 元素既可能是 InteractionOutput 对象也可能是 dict
// （interrupt_helpers.py L351-362 的 hasattr/dict 双分支），Go 用「桥接成 map」
// 等价实现；入参收窄为 []map[string]any，避免传单对象时静默返回 nil。
func ConvertInteractionsToAskUserQuestion(interactions []map[string]any) map[string]any {
	if len(interactions) == 0 {
		return nil
	}

	// Python: controller output 可以包含 permission interrupt shell 和
	// real ask_user interrupt。优先匹配 AskUserRail 中断（有 questions 字段），
	// 否则前端可能收到空的 permission prompt。
	for _, interaction := range interactions {
		requestID, valueObj := extractInteractionParts(interaction)
		if requestID == "" {
			continue
		}

		questionsRaw := extractQuestionsFromValue(valueObj)
		if questionsRaw == nil {
			continue
		}

		questions := buildMultiQuestions(questionsRaw)
		return map[string]any{
			"event_type": "chat.ask_user_question",
			"request_id": requestID,
			"questions":  questions,
			"source":     "ask_user_interrupt",
		}
	}

	// 第二轮：遍历所有 interaction，尝试提取 permission_interrupt
	for _, interaction := range interactions {
		requestID, _ := extractInteractionParts(interaction)
		if requestID == "" {
			continue
		}

		questionData := extractQuestionFromInteraction(interaction)
		if questionData == nil {
			continue
		}

		return map[string]any{
			"event_type": "chat.ask_user_question",
			"request_id": requestID,
			"questions":  []map[string]any{questionData},
			"source":     "permission_interrupt",
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractQuestionFromInteraction 从单个 interaction payload 中提取问题信息。
// Python: extract_question_from_interaction(payload) (line 426-475)
//
// payload 已由调用边界桥接为 map（对齐 Python 的 dict 分支；
// Python 的对象分支由 utils.PayloadToMap 的 JSON 往返等价覆盖）。
func extractQuestionFromInteraction(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}

	toolName := ""
	message := ""
	var uiOptions any

	valueObj, hasValue := payload["value"]
	if hasValue {
		if valueMap, ok := valueObj.(map[string]any); ok {
			message = utils.StrValFromMap(valueMap, "message")
			if message == "" {
				message = utils.StrValFromMap(valueMap, "question")
			}
			toolName = utils.StrValFromMap(valueMap, "tool_name")
			uiOptions = valueMap["ui_options"]
		} else {
			message = utils.StrValFromMap(payload, "message")
			if message == "" {
				message = utils.StrValFromMap(payload, "question")
			}
		}
	} else {
		message = utils.StrValFromMap(payload, "message")
		if message == "" {
			message = utils.StrValFromMap(payload, "question")
		}
	}

	if uiOptions != nil {
		if optionsList, ok := toSlice(uiOptions); ok && len(optionsList) > 0 {
			return map[string]any{
				"question":     messageOrDefault(message, toolName),
				"header":       headerFromToolName(toolName),
				"options":      optionsList,
				"multi_select": false,
			}
		}
	}

	// 默认权限审批选项
	return map[string]any{
		"question": messageOrDefault(message, toolName),
		"header":   headerFromToolName(toolName),
		"options": []map[string]any{
			{"label": "本次允许", "description": "仅本次授权执行"},
			{"label": "总是允许", "description": "记住该规则，以后自动放行"},
			{"label": "拒绝", "description": "拒绝执行此工具"},
		},
		"multi_select": false,
	}
}

// extractInteractionParts 从 interaction 中提取 request_id 和 value。
// Python: _extract_interaction_parts(interaction) (line 351-362)
//
// interaction 已由调用边界桥接为 map，等价覆盖 Python 的 hasattr(interaction, "id")
// 对象分支与 isinstance(interaction, dict) 分支。
func extractInteractionParts(interaction map[string]any) (string, any) {
	if interaction == nil {
		return "", nil
	}
	requestID := utils.StrValFromMap(interaction, "id")
	valueObj := interaction["value"]
	return strings.TrimSpace(requestID), valueObj
}

// extractQuestionsFromValue 从 value 对象中提取 questions 列表。
// Python: _extract_questions_from_value(value_obj) (line 365-399)
//
// AskUserRail 的 value 有 questions 属性 → 返回列表
// 如果 questions 不存在或为空 → 返回 nil 表示不是 AskUserRail 中断
func extractQuestionsFromValue(valueObj any) []any {
	if valueObj == nil {
		return nil
	}

	// 1. 直接从 value dict 中取 questions
	if m, ok := valueObj.(map[string]any); ok {
		qs, ok := m["questions"].([]any)
		if ok && len(qs) > 0 {
			return qs
		}
	}

	// 2. questions 嵌入在 tool_args 中（StructuredAskUserRail 路径）
	if m, ok := valueObj.(map[string]any); ok {
		toolArgsRaw := m["tool_args"]
		if toolArgsRaw != nil {
			// tool_args 可能是 JSON string
			if s, ok := toolArgsRaw.(string); ok && s != "" {
				var args map[string]any
				if err := json.Unmarshal([]byte(s), &args); err == nil {
					qs, ok := args["questions"].([]any)
					if ok && len(qs) > 0 {
						return qs
					}
				}
			}
			// tool_args 可能直接是 dict
			if argsMap, ok := toolArgsRaw.(map[string]any); ok {
				qs, ok := argsMap["questions"].([]any)
				if ok && len(qs) > 0 {
					return qs
				}
			}
		}
	}

	return nil
}

// buildMultiQuestions 从 questions 数据构建前端 PendingQuestionItem 列表。
// Python: _build_multi_questions(questions_data) (line 402-423)
//
// 有选项的问题：保留原始选项 + 追加 Other（自定义输入）
// 无选项的问题：不追加 Other，前端应直接进入自由输入模式
func buildMultiQuestions(questionsData []any) []map[string]any {
	questions := make([]map[string]any, 0, len(questionsData))
	for _, q := range questionsData {
		qMap, ok := q.(map[string]any)
		if !ok {
			continue
		}

		rawOptions, _ := qMap["options"].([]any)
		var options []map[string]any
		if len(rawOptions) > 0 {
			options = make([]map[string]any, 0, len(rawOptions)+1)
			for _, opt := range rawOptions {
				optMap, ok := opt.(map[string]any)
				if !ok {
					continue
				}
				options = append(options, map[string]any{
					"label":       utils.StrValFromMap(optMap, "label"),
					"description": utils.StrValFromMap(optMap, "description"),
				})
			}
			// 追加 Other 选项
			options = append(options, map[string]any{
				"label":       "Other",
				"description": "Custom input",
			})
		}

		multiSelect := false
		if ms, ok := qMap["multi_select"].(bool); ok {
			multiSelect = ms
		}

		questions = append(questions, map[string]any{
			"question":     utils.StrValFromMap(qMap, "question"),
			"header":       utils.StrValFromMap(qMap, "header"),
			"options":      options,
			"multi_select": multiSelect,
		})
	}
	return questions
}

// toSlice 将 any 转换为 []any。
// 支持 []any、[]map[string]any、nil 三种情况。
func toSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	switch s := v.(type) {
	case []any:
		return s, true
	case []map[string]any:
		result := make([]any, len(s))
		for i, item := range s {
			result[i] = item
		}
		return result, true
	default:
		return nil, false
	}
}

// messageOrDefault 如果 message 为空，返回默认提示文本。
// Python: message or f"工具 `{tool_name}` 需要授权才能执行"
func messageOrDefault(message string, toolName string) string {
	if message != "" {
		return message
	}
	return fmt.Sprintf("工具 `%s` 需要授权才能执行", toolName)
}

// headerFromToolName 根据 toolName 生成 header 文本。
// Python: f"权限审批: {tool_name}" if tool_name else "权限审批"
func headerFromToolName(toolName string) string {
	if toolName != "" {
		return fmt.Sprintf("权限审批: %s", toolName)
	}
	return "权限审批"
}
