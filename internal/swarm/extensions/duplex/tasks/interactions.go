package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// NATIVEApprovalSources 原生审批来源集合。
// 对齐 tasks/interactions.py:32 NATIVE_APPROVAL_SOURCES。
var NATIVEApprovalSources = map[string]bool{
	"permission_interrupt": true,
	"confirm_interrupt":    true,
}

// InformationQuestion 校验观察到的信息提问（不授予审批权限）。
// 对齐 tasks/interactions.py:6-30 information_question。
func InformationQuestion(payload map[string]any) (map[string]any, error) {
	source, _ := payload["source"].(string)
	questions, hasQuestions := payload["questions"].([]any)
	requestID := payload["request_id"]
	const errMsg = "This interaction requires the existing non-voice interaction channel"
	if _, ok := map[string]bool{"": true, "ask_user": true, "ask_user_interrupt": true}[source]; !ok ||
		payload["approval_schema"] != nil || payload["evolution_meta"] != nil {
		return nil, errors.New(errMsg)
	}
	requestIDStr, isStr := requestID.(string)
	if !isStr || requestIDStr == "" {
		return nil, errors.New(errMsg)
	}
	if !hasQuestions || len(questions) == 0 {
		return nil, errors.New(errMsg)
	}
	for _, question := range questions {
		q, ok := question.(map[string]any)
		if !ok {
			return nil, errors.New(errMsg)
		}
		text, isStr := q["question"].(string)
		if !isStr || strings.TrimSpace(text) == "" {
			return nil, errors.New(errMsg)
		}
	}
	return map[string]any{
		"request_id":   requestID,
		"interrupt_id": fmt.Sprintf("%v", orEmpty(payload["interrupt_id"])),
		"source":       source,
		"questions":    deepCopyJSON(questions),
		"state":        "pending",
	}, nil
}

// NativeApprovalQuestion 校验原生审批提问。
// 对齐 tasks/interactions.py:34-59 native_approval_question。
func NativeApprovalQuestion(payload map[string]any) (map[string]any, error) {
	source, _ := payload["source"].(string)
	if !NATIVEApprovalSources[source] {
		return nil, errors.New("Unsupported native approval source")
	}
	requestID, isStr := payload["request_id"].(string)
	if !isStr || requestID == "" {
		return nil, errors.New("Approval request ID is required")
	}
	questions, ok := payload["questions"].([]any)
	if !ok || len(questions) == 0 || len(questions) > 32 {
		return nil, errors.New("Invalid approval questions")
	}
	for _, q := range questions {
		qm, isMap := q.(map[string]any)
		if !isMap {
			return nil, errors.New("Invalid approval questions")
		}
		text, isStr := qm["question"].(string)
		if !isStr || strings.TrimSpace(text) == "" {
			return nil, errors.New("Invalid approval questions")
		}
	}
	interruptID := ""
	if payload["interrupt_id"] != nil {
		interruptID = fmt.Sprintf("%v", payload["interrupt_id"])
	}
	return map[string]any{
		"request_id":      requestID,
		"interrupt_id":    interruptID,
		"source":          source,
		"questions":       deepCopyJSON(questions),
		"approval_schema": payload["approval_schema"],
		"state":           "pending",
	}, nil
}

// ValidateNativeApprovalAnswers 校验原生审批答案序列。
// 对齐 tasks/interactions.py:61-95 validate_native_approval_answers。
func ValidateNativeApprovalAnswers(interaction map[string]any, answers []any) ([]map[string]any, error) {
	if _, err := NativeApprovalQuestion(interaction); err != nil {
		return nil, err
	}
	questions, _ := interaction["questions"].([]any)
	if len(answers) != len(questions) {
		return nil, errors.New("Answer every approval question exactly once")
	}
	result := []map[string]any{}
	for i, question := range questions {
		q, _ := question.(map[string]any)
		answer, isMap := answers[i].(map[string]any)
		if !isMap {
			return nil, errors.New("Approval answer does not match the pending question")
		}
		answerQuestion, hasAnswerQuestion := answer["question"]
		if !hasAnswerQuestion || answerQuestion == nil {
			answerQuestion = q["question"]
		} else if fmt.Sprintf("%v", answerQuestion) != fmt.Sprintf("%v", q["question"]) {
			return nil, errors.New("Approval answer does not match the pending question")
		}
		text := ""
		if v, ok := answer["custom_input"]; ok {
			text, _ = v.(string)
		} else if v, ok := answer["answer"]; ok {
			text, _ = v.(string)
		}
		options, _ := answer["selected_options"].([]any)
		if len([]rune(text)) > 4000 {
			return nil, errors.New("Invalid approval answer")
		}
		if len(options) > 32 {
			return nil, errors.New("Invalid approval answer")
		}
		for _, option := range options {
			o, isStr := option.(string)
			if !isStr || strings.TrimSpace(o) == "" || len(o) > 1000 {
				return nil, errors.New("Invalid approval answer")
			}
		}
		if strings.TrimSpace(text) == "" && len(options) == 0 {
			return nil, errors.New("Approval answer is empty")
		}
		item := map[string]any{
			"question":        answerQuestion,
			"answer":          text,
			"selected_options": options,
		}
		if cardID, ok := answer["card_id"].(string); ok {
			cardID = strings.TrimSpace(cardID)
			if cardID != "" && len(cardID) <= 128 {
				item["card_id"] = cardID
			}
		}
		result = append(result, item)
	}
	return result, nil
}

// ValidateAnswers 把有序语音答案绑定到 Core 原始观察提问。
// 对齐 tasks/interactions.py:97-122 validate_answers。
func ValidateAnswers(interaction map[string]any, answers []any) ([]map[string]any, error) {
	if _, err := InformationQuestion(interaction); err != nil {
		return nil, err
	}
	questions, _ := interaction["questions"].([]any)
	if len(answers) != len(questions) {
		return nil, errors.New("Answer every observed question exactly once")
	}
	resolved := []map[string]any{}
	for i, question := range questions {
		q, _ := question.(map[string]any)
		answer := answers[i]
		if s, isStr := answer.(string); isStr {
			answer = map[string]any{"question": q["question"], "answer": s}
		}
		am, isMap := answer.(map[string]any)
		if !isMap {
			return nil, errors.New("Answer does not match the observed question")
		}
		for key := range am {
			if key != "question" && key != "answer" && key != "selected_options" {
				return nil, errors.New("Answer does not match the observed question")
			}
		}
		if fmt.Sprintf("%v", am["question"]) != fmt.Sprintf("%v", q["question"]) {
			return nil, errors.New("Answer does not match the observed question")
		}
		text := ""
		if v, ok := am["answer"]; ok {
			text, _ = v.(string)
		}
		options, _ := am["selected_options"].([]any)
		if len([]rune(text)) > 4000 {
			return nil, errors.New("Invalid information answer")
		}
		if len(options) > 32 {
			return nil, errors.New("Invalid information answer")
		}
		for _, option := range options {
			o, isStr := option.(string)
			if !isStr || strings.TrimSpace(o) == "" || len(o) > 1000 {
				return nil, errors.New("Invalid information answer")
			}
		}
		if strings.TrimSpace(text) == "" && len(options) == 0 {
			return nil, errors.New("Invalid information answer")
		}
		resolved = append(resolved, deepCopyMap(am))
	}
	return resolved, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// orEmpty 空安全取值。
// 对齐 tasks/interactions.py:23 str(payload.get("interrupt_id") or "")。
func orEmpty(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

// deepCopyJSON 深拷贝 JSON 值。
// 对齐 tasks/interactions.py:2 from copy import deepcopy。
func deepCopyJSON(value any) any {
	data, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

// deepCopyMap 深拷贝 map。
func deepCopyMap(value map[string]any) map[string]any {
	out, _ := deepCopyJSON(value).(map[string]any)
	return out
}
