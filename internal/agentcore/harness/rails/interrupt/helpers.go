package interrupt

import (
	"encoding/json"
	"fmt"
	"strings"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// ConvertInteractionsToAskUserQuestion 将 __interaction__ payload 转换为前端
// chat.ask_user_question 格式。
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
		if isLatestInputPlaceholder(interaction) {
			continue
		}
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

	// 第二轮：无结构化 questions 的 ask_user（plain query）→ 自由文本问题卡。
	// Python: _build_plain_ask_user_question（interrupt_helpers.py:1029-1060）+
	// convert_interactions_to_ask_user_question 的 plain 分支（:1233-1256）——
	// 缺失时 plain query 的 ask_user 会落入第三轮被标成 permission_interrupt，
	// 前端按审批语义渲染、答案也无法归属回 ask_user 工具（2026-11-14 问答闭环实锤）。
	for _, interaction := range interactions {
		if isLatestInputPlaceholder(interaction) {
			continue
		}
		requestID, valueObj := extractInteractionParts(interaction)
		if requestID == "" {
			continue
		}

		plainQuestion := buildPlainAskUserQuestion(valueObj)
		if plainQuestion == nil {
			continue
		}

		return map[string]any{
			"event_type": "chat.ask_user_question",
			"request_id": requestID,
			"questions":  []map[string]any{plainQuestion},
			"source":     "ask_user_interrupt",
		}
	}

	// 第三轮：遍历所有 interaction，尝试提取 permission_interrupt
	for _, interaction := range interactions {
		if isLatestInputPlaceholder(interaction) {
			continue
		}
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

// isLatestInputPlaceholder 判断交互是否为 user_latest_input 占位交互（M-11）。
// 该交互的 kind 为 "latest_input"，语义是「图挂起等输入」而非权限审批；
// 不做判别会让前端凭空弹一个空问题的三选项审批框（对齐 Python：元组形状导致
// 转换器直接跳过）。
func isLatestInputPlaceholder(interaction map[string]any) bool {
	if interaction == nil {
		return false
	}
	kind, _ := interaction["kind"].(string)
	return kind == "latest_input"
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
	if valueObj == nil {
		// Python: interaction.get("value", {}) —— 缺省是空 dict 而非 None，
		// 下游（如 question_interactions）会直接对 value 取字段。
		valueObj = map[string]any{}
	}
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
	// 类型化对象桥接成 map（Python getattr 分支的 JSON 往返等价）
	if _, ok := valueObj.(map[string]any); !ok {
		if m, bridged := valueAsMap(valueObj); bridged {
			valueObj = m
		}
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// valueAsMap 把非 map 的中断值（如 *ToolCallInterruptRequest 等类型化对象）
// 桥接成 map。Python: 对象分支由 getattr 直读字段（interrupt_helpers.py 各处）；
// Go 等价实现是 JSON 往返（utils.PayloadToMap 同款约定——类型化对象的 JSON
// 标签已覆盖 tool_name/tool_args/questions 等字段名）。
func valueAsMap(valueObj any) (map[string]any, bool) {
	if valueObj == nil {
		return nil, false
	}
	if m, ok := valueObj.(map[string]any); ok {
		return m, true
	}
	b, err := json.Marshal(valueObj)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// isAskUserInterruptValue 判断中断值是否为 ask_user 中断。
//
// Python: _is_ask_user_interrupt_value (interrupt_helpers.py:997-1027)
//
// 判定顺序：①声明的 interrupt_kind == ASK_USER；②tool_name == "ask_user"
// （优先用显式工具身份——很多工具只有 query 参数，不能把一切 query 形状
// 的中断都当 ask_user）；③遗留 payload 的结构回退（payload_schema +
// questions 同时在场）。
func isAskUserInterruptValue(valueObj any) bool {
	// 类型化对象（*ToolCallInterruptRequest）先桥接成 map，再走统一读取
	if m, ok := valueAsMap(valueObj); ok {
		if raw, ok := m["interrupt_kind"].(string); ok && raw != "" {
			return raw == saschema.InterruptKindAskUser
		}
		if raw, ok := m["tool_name"].(string); ok && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw) == "ask_user"
		}
		_, hasPayloadSchema := m["payload_schema"]
		_, hasQuestions := m["questions"]
		return hasPayloadSchema && hasQuestions
	}
	if kind := interruptKindOfValue(valueObj); kind != "" {
		return kind == saschema.InterruptKindAskUser
	}
	if toolName := readValueField(valueObj, "tool_name"); toolName != "" {
		return toolName == "ask_user"
	}
	// 遗留 ask_user 中断 payload 可能不带 tool_name，保留结构回退。
	mapping, ok := valueObj.(map[string]any)
	if !ok {
		return false
	}
	_, hasPayloadSchema := mapping["payload_schema"]
	_, hasQuestions := mapping["questions"]
	return hasPayloadSchema && hasQuestions
}

// buildPlainAskUserQuestion 无结构化选项时构建自由文本 ask_user 问题卡。
//
// Python: _build_plain_ask_user_question (interrupt_helpers.py:1029-1060)
//
// 文案回退顺序：tool_args.query → message → question；全空返回 nil
// （调用方继续走 permission 投影）。
func buildPlainAskUserQuestion(valueObj any) map[string]any {
	if !isAskUserInterruptValue(valueObj) {
		return nil
	}
	if extractQuestionsFromValue(valueObj) != nil {
		return nil
	}

	toolName, message, toolArgs := readInterruptFields(valueObj)
	_ = toolName
	query := ""
	if toolArgs == nil {
		// 类型化对象：从桥接 map 读 tool_args（JSON 字符串）再解出 query
		if m, ok := valueAsMap(valueObj); ok {
			if argsRaw, ok := m["tool_args"].(string); ok && argsRaw != "" {
				var args map[string]any
				if err := json.Unmarshal([]byte(argsRaw), &args); err == nil {
					query = strings.TrimSpace(anyToString(args["query"]))
				}
			} else if argsMap, ok := m["tool_args"].(map[string]any); ok {
				query = strings.TrimSpace(anyToString(argsMap["query"]))
			}
			if query == "" {
				query = strings.TrimSpace(anyToString(m["message"]))
			}
			if query == "" {
				query = strings.TrimSpace(anyToString(m["question"]))
			}
		}
	}
	if toolArgs != nil {
		query = strings.TrimSpace(anyToString(toolArgs["query"]))
	}
	if query == "" {
		query = strings.TrimSpace(message)
	}
	if query == "" {
		query = strings.TrimSpace(readValueField(valueObj, "question"))
	}
	if query == "" {
		return nil
	}

	// Python: {"question": query, "header": "Question", "options": [], "multi_select": False}
	return map[string]any{
		"question":     query,
		"header":       "Question",
		"options":      []any{},
		"multi_select": false,
	}
}
