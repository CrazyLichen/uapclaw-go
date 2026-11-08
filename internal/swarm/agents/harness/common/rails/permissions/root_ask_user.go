// root Host 拥有的紧凑普通 ask_user 续接。
// 当用户对权限卡不是直接"同意/拒绝"，而是反问澄清时，走 ask_user 工具自身的
// 中断通道。本文件负责把那次反问的上下文（当时问了什么、有哪些选项）冻结进
// 中断请求的 metadata，并在用户答案回来后把问答对还原成澄清记录，
// 供 root_context.AppendRootClarification 追加到意图窗口。
// 关键点：澄清不是"发给 LLM 的新消息"，而是"给权限判断看的备忘录"。
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/root_ask_user.py（274 行）
package permissions

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserQuestionReference 一次反问的问题快照。
// Python: @dataclass(frozen=True, slots=True) AskUserQuestionReference
type AskUserQuestionReference struct {
	// Question 问题文本
	Question string
	// Options 选项列表
	Options []RootAskUserOption
	// MultiSelect 是否多选
	MultiSelect bool
}

// RootAskUserContinuation 一次 ask_user 中断冻结的续接上下文。
// Python: @dataclass(frozen=True, slots=True) RootAskUserContinuation
type RootAskUserContinuation struct {
	// ToolCallID 对应的工具调用 ID
	ToolCallID string
	// Context 中断发生时的决策上下文
	Context *RootDecisionContext
	// Questions 问题列表
	Questions []AskUserQuestionReference
}

// RootAskUserResume 用户答案还原后的续接结果。
// Python: @dataclass(frozen=True, slots=True) RootAskUserResume
type RootAskUserResume struct {
	// Continuation 原始续接上下文
	Continuation *RootAskUserContinuation
	// Clarifications 澄清记录；容量不足时为 nil
	Clarifications []RootAskUserClarification
	// CapacityReason 容量原因，非空表示未能生成澄清
	CapacityReason string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AskUserToolName ask_user 工具名
	// Python: ASK_USER_TOOL_NAME
	AskUserToolName = "ask_user"

	// AskUserContinuationMetadataKey 续接元数据在中断请求 metadata 中的键
	// Python: ASK_USER_CONTINUATION_METADATA_KEY
	AskUserContinuationMetadataKey = "jiuwenswarm_root_ask_user_v1"

	// AskUserResumeDTOKey 续接 DTO 在 extra 中的键
	// Python: ASK_USER_RESUME_DTO_KEY
	AskUserResumeDTOKey = "jiuwenswarm.root_ask_user_resume.v1"

	// maxAskUserAnswers 单次澄清最多答案条数
	// Python: _MAX_ANSWERS
	maxAskUserAnswers = 8
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAskUserQuestionReference 构造并校验一个问题快照。
// Python: AskUserQuestionReference(...) 由 from_mapping 校验
func NewAskUserQuestionReference(
	question string,
	options []RootAskUserOption,
	multiSelect bool,
) (AskUserQuestionReference, error) {
	if strings.TrimSpace(question) == "" {
		return AskUserQuestionReference{}, errors.New("invalid ask-user question")
	}
	return AskUserQuestionReference{
		Question:    question,
		Options:     options,
		MultiSelect: multiSelect,
	}, nil
}

// ToMapping 序列化为映射。
// Python: AskUserQuestionReference.to_mapping()
func (r AskUserQuestionReference) ToMapping() map[string]any {
	options := make([]any, 0, len(r.Options))
	for _, option := range r.Options {
		options = append(options, option.ToMapping())
	}
	return map[string]any{
		"question":     r.Question,
		"options":      options,
		"multi_select": r.MultiSelect,
	}
}

// AskUserQuestionReferenceFromMapping 从映射还原，字段集合必须完全一致。
// Python: AskUserQuestionReference.from_mapping(value)
func AskUserQuestionReferenceFromMapping(value map[string]any) (AskUserQuestionReference, error) {
	if value == nil || len(value) != 3 {
		return AskUserQuestionReference{}, errors.New("invalid ask-user question")
	}
	question, err := strictString(value, "question")
	if err != nil || strings.TrimSpace(question) == "" {
		return AskUserQuestionReference{}, errors.New("invalid ask-user question")
	}
	rawOptions, okOptions := value["options"].([]any)
	multiSelect, okMulti := value["multi_select"].(bool)
	if !okOptions || !okMulti {
		return AskUserQuestionReference{}, errors.New("invalid ask-user question")
	}

	options := make([]RootAskUserOption, 0, len(rawOptions))
	for _, item := range rawOptions {
		asMap, ok := item.(map[string]any)
		if !ok {
			return AskUserQuestionReference{}, errors.New("invalid ask-user question")
		}
		option, err := RootAskUserOptionFromMapping(asMap)
		if err != nil {
			return AskUserQuestionReference{}, errors.New("invalid ask-user question")
		}
		options = append(options, option)
	}
	return AskUserQuestionReference{Question: question, Options: options, MultiSelect: multiSelect}, nil
}

// PutAskUserResumeInInputs 把续接 DTO 写入 inputs 的 run.context.extra。
// prepared 为 nil 时只移除旧值（对齐 Python 先 pop 再按需写入）。
// Python: put_ask_user_resume_in_inputs(inputs, prepared)
func PutAskUserResumeInInputs(inputs map[string]any, prepared *RootAskUserResume) map[string]any {
	result := copyMap(inputs)
	run := nestedCopyMap(result, "run")
	contextMap := nestedCopyMap(run, "context")
	extra := nestedCopyMap(contextMap, "extra")
	delete(extra, AskUserResumeDTOKey)
	if prepared != nil {
		extra[AskUserResumeDTOKey] = prepared
	}
	contextMap["extra"] = extra
	run["context"] = contextMap
	result["run"] = run
	return result
}

// BuildAskUserMetadata 构造要写进中断请求 metadata 的续接快照。
// 只有工具名是 ask_user 且能解析出问题时才返回非 nil，否则返回 nil。
// Python: build_ask_user_metadata(*, context, tool_name, tool_call_id, tool_args)
func BuildAskUserMetadata(
	decisionContext *RootDecisionContext,
	toolName any,
	toolCallID any,
	toolArgs any,
) map[string]any {
	callID, _ := toolCallID.(string)
	callID = strings.TrimSpace(callID)
	name, _ := toolName.(string)

	questions, err := questionsOf(toolArgs)
	if err != nil {
		return nil
	}
	if name != AskUserToolName || callID == "" {
		return nil
	}
	if len(callID) > 512 || len(questions) == 0 {
		return nil
	}

	questionMappings := make([]any, 0, len(questions))
	for _, question := range questions {
		questionMappings = append(questionMappings, question.ToMapping())
	}
	return map[string]any{
		"tool_call_id": callID,
		"context":      decisionContext.ToMapping(),
		"questions":    questionMappings,
	}
}

// AskUserContinuation 从 metadata 还原续接上下文。
// expectedToolCallID 必须与快照里的一致，问题数量必须在 1..MaxStructuredQuestions，
// 且问题文本不得重复；任何不符都返回 nil（fail-closed）。
// Python: ask_user_continuation(metadata, *, expected_tool_call_id)
func AskUserContinuation(metadata any, expectedToolCallID string) *RootAskUserContinuation {
	asMap, ok := metadata.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := asMap[AskUserContinuationMetadataKey].(map[string]any)
	if !ok || len(raw) != 3 {
		return nil
	}

	callID, okCall := raw["tool_call_id"].(string)
	rawQuestions, okQuestions := raw["questions"].([]any)
	if !okCall || !okQuestions || callID != expectedToolCallID {
		return nil
	}

	contextMap, okContext := raw["context"].(map[string]any)
	if !okContext {
		return nil
	}
	decisionContext, err := RootDecisionContextFromMapping(contextMap)
	if err != nil {
		return nil
	}

	questions := make([]AskUserQuestionReference, 0, len(rawQuestions))
	for _, item := range rawQuestions {
		asMapItem, ok := item.(map[string]any)
		if !ok {
			return nil
		}
		question, err := AskUserQuestionReferenceFromMapping(asMapItem)
		if err != nil {
			return nil
		}
		questions = append(questions, question)
	}

	if len(questions) < 1 || len(questions) > rails.MaxStructuredQuestions {
		return nil
	}
	seen := make(map[string]struct{}, len(questions))
	for _, question := range questions {
		if _, dup := seen[question.Question]; dup {
			return nil
		}
		seen[question.Question] = struct{}{}
	}

	return &RootAskUserContinuation{
		ToolCallID: callID,
		Context:    decisionContext,
		Questions:  questions,
	}
}

// PrepareAskUserResume 把用户对该次 ask_user 的回答转成澄清记录。
// 答案必须是 {"answers": {问题文本: 答案}}，问题集合必须与快照完全一致；
// 单条澄清超容量时返回带 CapacityReason 的结果。
// Python: prepare_ask_user_resume(*, continuation, answer)
func PrepareAskUserResume(
	continuation *RootAskUserContinuation,
	answer any,
) *RootAskUserResume {
	asMap, ok := answer.(map[string]any)
	if !ok || len(asMap) != 1 {
		return nil
	}
	answers, ok := asMap["answers"].(map[string]any)
	if !ok {
		return nil
	}
	if len(answers) != len(continuation.Questions) {
		return nil
	}
	for _, question := range continuation.Questions {
		if _, exists := answers[question.Question]; !exists {
			return nil
		}
	}

	clarifications := make([]RootAskUserClarification, 0, len(continuation.Questions))
	for _, question := range continuation.Questions {
		values, ok := answerValues(answers[question.Question], question.MultiSelect)
		if !ok {
			return nil
		}
		clarifications = append(clarifications, RootAskUserClarification{
			Question: question.Question,
			Answers:  values,
			Options:  question.Options,
		})
	}

	turn, err := NewRootIntentTurn(
		continuation.Context.RequestID,
		RootIntentTurnKindAskUserClarification,
		"",
		clarifications,
	)
	if err != nil {
		// Python 在此处直接构造 dataclass，容量超限由 __post_init__ 抛错
		return &RootAskUserResume{
			Continuation:   continuation,
			Clarifications: nil,
			CapacityReason: AutoReviewBlockIntentInputTooLarge,
		}
	}
	if len(RootIntentTurnText(turn, true)) > RootIntentMaxTurnChars {
		return &RootAskUserResume{
			Continuation:   continuation,
			Clarifications: nil,
			CapacityReason: AutoReviewBlockIntentInputTooLarge,
		}
	}

	return &RootAskUserResume{
		Continuation:   continuation,
		Clarifications: clarifications,
	}
}

// ApplyAskUserResume 把续接结果应用到决策上下文。
// 容量原因非空时只标记阻断原因；否则把澄清追加到意图窗口。
// Python: apply_ask_user_resume(prepared)
func ApplyAskUserResume(prepared *RootAskUserResume) *RootDecisionContext {
	if prepared.CapacityReason != "" {
		replaced, err := NewRootDecisionContext(
			prepared.Continuation.Context.SessionID,
			prepared.Continuation.Context.RequestID,
			prepared.Continuation.Context.ChannelID,
			prepared.Continuation.Context.TrustedTurns,
			prepared.CapacityReason,
		)
		if err != nil {
			return prepared.Continuation.Context
		}
		return replaced
	}
	if prepared.Clarifications == nil {
		return prepared.Continuation.Context
	}
	return AppendRootClarification(prepared.Continuation.Context, prepared.Clarifications)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// questionsOf 从工具参数里解析问题列表。
// 支持字符串 JSON 与映射两种形态；无 questions 时回退到单个 query 字符串。
// 任一问题非法、数量越界、文本重复都返回空（fail-closed）。
// Python: _questions(tool_args)
func questionsOf(toolArgs any) ([]AskUserQuestionReference, error) {
	switch typed := toolArgs.(type) {
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(typed), &parsed); err != nil {
			return nil, nil
		}
		return questionsOf(parsed)
	case map[string]any:
		rawQuestions, exists := typed["questions"]
		if !exists || rawQuestions == nil {
			query, ok := typed["query"].(string)
			if ok && strings.TrimSpace(query) != "" {
				return []AskUserQuestionReference{{Question: query}}, nil
			}
			return nil, nil
		}

		list, ok := rawQuestions.([]any)
		if !ok || len(list) < 1 || len(list) > rails.MaxStructuredQuestions {
			return nil, nil
		}

		result := make([]AskUserQuestionReference, 0, len(list))
		for _, item := range list {
			asMap, ok := item.(map[string]any)
			if !ok {
				return nil, nil
			}
			question, ok := asMap["question"].(string)
			if !ok || strings.TrimSpace(question) == "" {
				return nil, nil
			}
			multiSelect := false
			if raw, exists := asMap["multi_select"]; exists {
				asBool, ok := raw.(bool)
				if !ok {
					return nil, nil
				}
				multiSelect = asBool
			}
			options, ok := optionsOf(asMap["options"])
			if !ok {
				return nil, nil
			}
			result = append(result, AskUserQuestionReference{
				Question:    question,
				Options:     options,
				MultiSelect: multiSelect,
			})
		}

		seen := make(map[string]struct{}, len(result))
		for _, item := range result {
			if _, dup := seen[item.Question]; dup {
				return nil, nil
			}
			seen[item.Question] = struct{}{}
		}
		return result, nil
	}
	return nil, nil
}

// optionsOf 解析选项列表。
// 空列表合法；非空时必须是 2-4 个、标签唯一且不含 "Other"。
// Python: _options(value)
func optionsOf(value any) ([]RootAskUserOption, bool) {
	if value == nil {
		return nil, true
	}
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	if len(list) > 0 {
		if len(list) < rootAskUserOptionMinOptions || len(list) > rootAskUserOptionMaxOptions {
			return nil, false
		}
	}

	result := make([]RootAskUserOption, 0, len(list))
	for _, item := range list {
		asMap, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		label, err := strictString(asMap, "label")
		if err != nil {
			return nil, false
		}
		description, okDesc := asMap["description"].(string)
		if _, exists := asMap["description"]; exists && !okDesc {
			return nil, false
		}
		preview, okPreview := asMap["preview"].(string)
		if _, exists := asMap["preview"]; exists && !okPreview {
			return nil, false
		}
		result = append(result, RootAskUserOption{
			Label: label, Description: description, Preview: preview,
		})
	}

	labels := make(map[string]struct{}, len(result))
	for _, option := range result {
		if _, dup := labels[option.Label]; dup {
			return nil, false
		}
		labels[option.Label] = struct{}{}
	}
	if _, hasOther := labels["Other"]; hasOther {
		return nil, false
	}
	return result, true
}

// answerValues 归一化单个问题的答案。
// 单值或列表均可；空、超过 8 条、单选给了多个、含空白字符串都判非法；
// 返回值逐条 trim。
// Python: _answer_values(value, *, multi_select)
func answerValues(value any, multiSelect bool) ([]string, bool) {
	var raw []any
	if list, ok := value.([]any); ok {
		raw = list
	} else {
		raw = []any{value}
	}

	if len(raw) == 0 || len(raw) > maxAskUserAnswers {
		return nil, false
	}
	if !multiSelect && len(raw) != 1 {
		return nil, false
	}

	values := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, false
		}
		values = append(values, strings.TrimSpace(text))
	}
	return values, true
}
