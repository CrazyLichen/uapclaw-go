// 结构化 ask_user 的 questions 严格校验。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
// 的 `resolve_interrupt` 里 questions 校验段（L363-480）
//
// ## 为什么每条拒绝都写明「该改成什么」
//
// 上游注释说得很清楚：这里的拒绝**永远不会到达人**——它作为工具结果回给模型，
// 模型唯一的动作是再次调用工具。只说「哪里错了」而不说「该发什么」，
// 模型重试时会带一模一样的参数；而**重复的相同工具调用正是循环检测器终止
// 运行的触发条件**，用户看到的会是终止文案而不是答案。
//
// 因此每条拒绝信息都必须引用正确的形状，这套文案一字不改照抄上游。
//
// ## strict_initial_request 的作用
//
// 首次请求（无 user_input 且不是稀疏重放）才做「标签唯一性 / 保留标签 Other /
// description 与 preview 必须是字符串」这三项额外检查。续接请求放宽，
// 因为此时 args 可能来自更早的版本。

package rails

import (
	"fmt"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// reservedOtherOptionLabel 结构化选项的保留标签
	//
	// Python: canonical_label == "Other"
	reservedOtherOptionLabel = "Other"

	// minStructuredOptions 有选项时的最小数量
	minStructuredOptions = 2
	// maxStructuredOptions 有选项时的最大数量
	maxStructuredOptions = 4
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IsSavedPendingToolCall 判断本次是否为 Core 持久化中断态的稀疏重放。
//
// Python: _is_saved_pending_tool_call(ctx, tool_call)
// 读会话里持久化的中断状态，若其待决调用与本次 tool_call 同 ID 则为重放。
func IsSavedPendingToolCall(cbc any, toolCall *llmschema.ToolCall) bool {
	if cbc == nil || toolCall == nil {
		return false
	}
	session := callbackSession(cbc)
	if session == nil {
		return false
	}
	raw, err := session.GetState(sessionstate.StringKey(saschema.InterruptionKey))
	if err != nil || raw == nil {
		return false
	}
	state, ok := raw.(*saschema.ToolInterruptionState)
	if !ok || state == nil || len(state.InterruptedTools) == 0 {
		return false
	}
	// Python: any(entry.tool_call.id == tool_call.id for entry in state.interrupted_tools)
	for _, entry := range state.InterruptedTools {
		if entry != nil && entry.ToolCall != nil && entry.ToolCall.ID == toolCall.ID {
			return true
		}
	}
	return false
}

// ValidateStructuredQuestions 校验 questions 参数，返回拒绝文案（空串表示通过）。
//
// Python: ask_user_rail.resolve_interrupt 的 questions 校验段
// strictInitialRequest 为 true 时额外检查标签唯一性与类型。
func ValidateStructuredQuestions(
	toolCall *llmschema.ToolCall,
	strictInitialRequest bool,
) string {
	if toolCall == nil {
		return ""
	}
	args := interrupt.ParseToolArgs(toolCall)
	// 没有传 questions 的调用是单纯提问，直接放过。
	rawQuestions, hasQuestions := args["questions"]
	if !hasQuestions {
		return ""
	}
	// 第一层：整个 questions 必须是数组，且条数不超过上限。
	questionsList, isList := rawQuestions.([]any)
	if !isList {
		return "[INVALID_ARGUMENT] questions must be an array of question " +
			"objects. Send it as a JSON array, e.g. questions: " +
			`[{"question": "Which one?", "options": [{"label": "A"}, ` +
			`{"label": "B"}]}]. To ask a single free-text question ` +
			"instead, omit questions entirely and send only query."
	}
	// 条数超上限时让模型分批问，避免一次弹太多问题。
	if len(questionsList) > MaxStructuredQuestions {
		return fmt.Sprintf("[INVALID_ARGUMENT] ask_user accepts at most %d questions "+
			"per call; received %d. Call ask_user again with the first %d questions, "+
			"and ask the remaining ones in a further call once these are answered.",
			MaxStructuredQuestions, len(questionsList), MaxStructuredQuestions)
	}
	// 第二层：逐个问题检查形状 —— 问题体、题干、可选标题。
	for questionIndex, rawQuestion := range questionsList {
		question, isMapping := rawQuestion.(map[string]any)
		if !isMapping {
			return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d] must be an object. "+
				"Replace it with an object carrying the question text, plus 2-4 options "+
				"when the answer is a choice, e.g. {\"question\": \"Which one?\", "+
				"\"options\": [{\"label\": \"A\"}, {\"label\": \"B\"}]}.", questionIndex)
		}
		questionText, isString := question["question"].(string)
		if !isString || strings.TrimSpace(questionText) == "" {
			return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d].question is required "+
				"and must be a non-empty string. Add the sentence to put to the user, "+
				"e.g. \"question\": \"Which branch should the fix go on?\".",
				questionIndex)
		}
		if header, hasHeader := question["header"]; hasHeader {
			if _, isHeaderString := header.(string); !isHeaderString {
				return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d].header must be "+
					"a string when provided. Send a short label of a few words, "+
					"e.g. header: \"Deployment\", or drop the field.", questionIndex)
			}
		}
		// 第三层：选项列表的形状 —— 数组、每项有标签、标签不重复也不占用保留名。
		rawOptions, hasOptions := question["options"]
		if !hasOptions {
			continue
		}
		optionsList, isOptionsList := rawOptions.([]any)
		if !isOptionsList {
			return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d].options must be an "+
				"array of option objects. Send it as a JSON array, e.g. options: "+
				"[{\"label\": \"Yes\"}, {\"label\": \"No\"}]. To let the user answer "+
				"in their own words instead, omit options entirely.", questionIndex)
		}
		canonicalLabels := make(map[string]struct{}, len(optionsList))
		for optionIndex, rawOption := range optionsList {
			var label string
			var isLabelString bool
			if option, isOptionMapping := rawOption.(map[string]any); isOptionMapping {
				label, isLabelString = option["label"].(string)
			}
			if !isLabelString || strings.TrimSpace(label) == "" {
				return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d].options[%d].label "+
					"is required and must be a non-empty string. Give the option the "+
					"1-5 words the user should see, e.g. {\"label\": \"Apply update\"}.",
					questionIndex, optionIndex)
			}
			if strictInitialRequest {
				option, _ := rawOption.(map[string]any)
				detail := ""
				for _, field := range []string{"description", "preview"} {
					value, hasField := option[field]
					if !hasField {
						continue
					}
					if _, isStringField := value.(string); !isStringField {
						return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d].options[%d] "+
							"description and preview must be strings when provided.",
							questionIndex, optionIndex)
					}
					_ = detail
				}
				canonicalLabel := strings.TrimSpace(label)
				if canonicalLabel == reservedOtherOptionLabel {
					return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d].options must use "+
						"unique labels and must not use the reserved label 'Other'.",
						questionIndex)
				}
				if _, duplicate := canonicalLabels[canonicalLabel]; duplicate {
					return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d].options must use "+
						"unique labels and must not use the reserved label 'Other'.",
						questionIndex)
				}
				canonicalLabels[canonicalLabel] = struct{}{}
			}
		}
		// 第四层：选项个数只允许 0 或 2-4 个（1 个选项不构成选择）。
		if len(optionsList) > 0 &&
			(len(optionsList) < minStructuredOptions || len(optionsList) > maxStructuredOptions) {
			return fmt.Sprintf("[INVALID_ARGUMENT] questions[%d].options must contain "+
				"either 0 or 2-4 items; received %d. Merge or drop choices until at "+
				"most 4 remain, ask the rest in a further ask_user call, or omit "+
				"options entirely to let the user answer in their own words.",
				questionIndex, len(optionsList))
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// callbackSession 从回调上下文取出会话门面（nil 安全）。
func callbackSession(cbc any) sessioninterfaces.SessionFacade {
	carrier, ok := cbc.(interface {
		Session() sessioninterfaces.SessionFacade
	})
	if !ok {
		return nil
	}
	return carrier.Session()
}
