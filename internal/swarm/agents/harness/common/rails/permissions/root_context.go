// 单次权限决策的紧凑 root-only 权威上下文。
// 本文件定义"用户到底想干什么"这一背景的结构化载体：意图轮次（RootIntentTurn）、
// 决策上下文（RootDecisionContext）、以及从宿主消息里提取用户原文的辅助函数。
// 该上下文有两个用途：
//  1. 冻结在权限卡里，用户回答后随卡解冻，保证 rail 看到的是"用户看到那张卡时"的背景；
//  2. 投影成意图视图交给审查模型，作为判断"这个操作是否违反用户意图"的依据。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/root_context.py（528 行）
package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RootAskUserOption 反问澄清里的一个选项。
// Python: @dataclass(frozen=True, slots=True) RootAskUserOption
type RootAskUserOption struct {
	// Label 选项标签，必填
	Label string
	// Description 选项说明
	Description string
	// Preview 选项预览
	Preview string
}

// RootAskUserClarification 一次反问及其答案。
// Python: @dataclass(frozen=True, slots=True) RootAskUserClarification
type RootAskUserClarification struct {
	// Question 反问的问题
	Question string
	// Answers 用户给出的答案（非空，最多 8 条）
	Answers []string
	// Options 问题提供的选项（0 个或 2-4 个）
	Options []RootAskUserOption
}

// RootIntentTurn 一轮用户意图。
// kind 区分来源：HISTORY（历史消息）、FRESH（本次请求）、STEER（运行中插入）、
// ASK_USER_CLARIFICATION（反问澄清）。前三者只带 Text，最后一种只带 Clarifications。
// Python: @dataclass(frozen=True, slots=True) RootIntentTurn
type RootIntentTurn struct {
	// RequestID 归属的宿主请求 ID
	RequestID string
	// Kind 意图类型，见 RootIntentTurnKind
	Kind string
	// Text 用户原文
	Text string
	// Clarifications 反问澄清记录
	Clarifications []RootAskUserClarification
}

// RootDecisionContext 一次权限决策的完整背景。
// Python: @dataclass(frozen=True, slots=True) RootDecisionContext
type RootDecisionContext struct {
	// SessionID 会话标识
	SessionID string
	// RequestID 宿主请求标识
	RequestID string
	// ChannelID 渠道标识
	ChannelID string
	// TrustedTurns 可信意图轮次，只收来自真实用户的消息
	TrustedTurns []RootIntentTurn
	// AutoReviewBlockReason 自动审查被阻断的原因，空串表示未阻断
	AutoReviewBlockReason string
}

// RootIntentProjection 从消息列表投影出的意图轮次与被阻断原因。
// Python: @dataclass(frozen=True, slots=True) RootIntentProjection
type RootIntentProjection struct {
	// Turns 投影出的意图轮次
	Turns []RootIntentTurn
	// AutoReviewBlockReason 投影过程中出现的阻断原因
	AutoReviewBlockReason string
}

// OriginalUserIntentEvidence 原始用户意图证据。
// Python: @dataclass(frozen=True, slots=True) OriginalUserIntentEvidence
type OriginalUserIntentEvidence struct {
	// Source 意图来源，见 UserIntentSource
	Source string
	// Text 意图文本
	Text string
	// Context 决策上下文，可为 nil
	Context *RootDecisionContext
}

// rootContextKey 决策上下文的 context key
type rootContextKey struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// RootIntentTurnKind 意图轮次类型取值。
// Python: class RootIntentTurnKind
const (
	// RootIntentTurnKindHistory 会话历史中的用户消息
	RootIntentTurnKindHistory = "history"
	// RootIntentTurnKindFresh 本次请求的用户消息
	RootIntentTurnKindFresh = "fresh"
	// RootIntentTurnKindSteer 任务运行中插入的指令
	RootIntentTurnKindSteer = "steer"
	// RootIntentTurnKindAskUserClarification 反问澄清
	RootIntentTurnKindAskUserClarification = "ask_user_clarification"
)

// UserIntentSource 用户意图来源取值。
// Python: class UserIntentSource
const (
	// UserIntentSourceHostUserMessage 来自宿主转发的用户消息
	UserIntentSourceHostUserMessage = HostUserMessageSource
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// RootContextKey 决策上下文在 extra 中的键名。
	// Python: ROOT_CONTEXT_KEY
	RootContextKey = "jiuwenswarm.root_permission_context.v1"

	// HostUserMessageSource 宿主用户消息的来源标记。
	// Python: HOST_USER_MESSAGE_SOURCE
	HostUserMessageSource = "host_user_message"

	// RootIntentMaxTurns 意图窗口最大轮次。
	// Python: ROOT_INTENT_MAX_TURNS
	RootIntentMaxTurns = 20

	// RootIntentMaxTurnChars 单轮意图最大字符数。
	// Python: ROOT_INTENT_MAX_TURN_CHARS
	RootIntentMaxTurnChars = 8000

	// RootIntentMaxTotalChars 意图窗口最大总字符数。
	// Python: ROOT_INTENT_MAX_TOTAL_CHARS
	RootIntentMaxTotalChars = 48000

	// AutoReviewBlockIntentInputTooLarge 输入过大导致自动审查阻断。
	// Python: AUTO_REVIEW_BLOCK_INTENT_INPUT_TOO_LARGE
	AutoReviewBlockIntentInputTooLarge = "intent_input_too_large"

	// AutoReviewBlockHistoryWindowTruncated 历史窗口被截断导致自动审查阻断。
	// Python: AUTO_REVIEW_BLOCK_HISTORY_WINDOW_TRUNCATED
	AutoReviewBlockHistoryWindowTruncated = "intent_history_window_truncated"

	// HostUserPromptPrefixZh 中文宿主消息前缀。
	// Python: HOST_USER_PROMPT_PREFIX_ZH
	HostUserPromptPrefixZh = "你收到一条消息：\n"

	// HostUserPromptPrefixEn 英文宿主消息前缀。
	// Python: HOST_USER_PROMPT_PREFIX_EN
	HostUserPromptPrefixEn = "You receive a new message:\n"

	// HostUserOriginExternal 外部用户撰写
	// Python: HOST_USER_ORIGIN_EXTERNAL
	HostUserOriginExternal = "external_user_authored"

	// HostUserOriginInternal 内部派发
	// Python: HOST_USER_ORIGIN_INTERNAL
	HostUserOriginInternal = "internal_dispatch"

	// rootAskUserOptionMinOptions 选项数量下限（有选项时必须 >= 2）
	rootAskUserOptionMinOptions = 2

	// rootAskUserOptionMaxOptions 选项数量上限
	rootAskUserOptionMaxOptions = 4

	// rootAskUserMaxAnswers 单次澄清最多答案数
	rootAskUserMaxAnswers = 8
)

// ──────────────────────────── 全局变量 ────────────────────────────

// rootIntentTurnKinds 合法的意图轮次类型集合。
// Python: _TURN_KINDS
var rootIntentTurnKinds = map[string]struct{}{
	RootIntentTurnKindHistory:              {},
	RootIntentTurnKindFresh:                {},
	RootIntentTurnKindSteer:                {},
	RootIntentTurnKindAskUserClarification: {},
}

// rootBlockReasons 合法的自动审查阻断原因集合。
// Python: _BLOCK_REASONS
var rootBlockReasons = map[string]struct{}{
	"":                                    {},
	AutoReviewBlockIntentInputTooLarge:    {},
	AutoReviewBlockHistoryWindowTruncated: {},
}

// requiredEnvelopeKeys 用户消息信封的必填键。
// Python: _REQUIRED_ENVELOPE_KEYS
var requiredEnvelopeKeys = []string{
	"source",
	"timestamp",
	"preferred_response_language",
	"content",
	"files_updated_by_user",
	"type",
	"origin_kind",
}

// optionalEnvelopeKeys 用户消息信封的可选键。
// Python: _OPTIONAL_ENVELOPE_KEYS
var optionalEnvelopeKeys = []string{"skills_to_use", "trusted_dirs", "timezone"}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRootAskUserOption 构造并校验一个反问选项。
// Python: RootAskUserOption(...) 的 __post_init__
func NewRootAskUserOption(label, description, preview string) (RootAskUserOption, error) {
	if strings.TrimSpace(label) == "" {
		return RootAskUserOption{}, errors.New("invalid ask-user option")
	}
	return RootAskUserOption{Label: label, Description: description, Preview: preview}, nil
}

// ToMapping 序列化为映射。
// Python: RootAskUserOption.to_mapping()
func (o RootAskUserOption) ToMapping() map[string]any {
	return map[string]any{
		"label":       o.Label,
		"description": o.Description,
		"preview":     o.Preview,
	}
}

// RootAskUserOptionFromMapping 从映射还原，字段集合必须完全一致。
// Python: RootAskUserOption.from_mapping(value)
func RootAskUserOptionFromMapping(value map[string]any) (RootAskUserOption, error) {
	if value == nil || len(value) != 3 {
		return RootAskUserOption{}, errors.New("invalid ask-user option")
	}
	label, err1 := strictString(value, "label")
	description, err2 := strictString(value, "description")
	preview, err3 := strictString(value, "preview")
	if err := firstErr(err1, err2, err3); err != nil {
		return RootAskUserOption{}, errors.New("invalid ask-user option")
	}
	return NewRootAskUserOption(label, description, preview)
}

// NewRootAskUserClarification 构造并校验一条反问澄清。
// 校验顺序对齐 Python __post_init__：问题非空 → options 类型 → 选项数量
// → 标签唯一且不含 "Other" → 答案非空且不超上限 → 逐条 trim。
// Python: RootAskUserClarification(...) 的 __post_init__
func NewRootAskUserClarification(
	question string,
	answers []string,
	options []RootAskUserOption,
) (RootAskUserClarification, error) {
	if strings.TrimSpace(question) == "" {
		return RootAskUserClarification{}, errors.New("invalid ask-user question")
	}
	if len(options) > 0 {
		if len(options) < rootAskUserOptionMinOptions || len(options) > rootAskUserOptionMaxOptions {
			return RootAskUserClarification{}, errors.New("ambiguous ask-user options")
		}
	}
	labels := make(map[string]struct{}, len(options))
	for _, option := range options {
		if _, dup := labels[option.Label]; dup {
			return RootAskUserClarification{}, errors.New("ambiguous ask-user options")
		}
		labels[option.Label] = struct{}{}
	}
	if _, hasOther := labels["Other"]; hasOther {
		return RootAskUserClarification{}, errors.New("ambiguous ask-user options")
	}
	if len(answers) == 0 {
		return RootAskUserClarification{}, errors.New("invalid ask-user answers")
	}
	if len(answers) > rootAskUserMaxAnswers {
		return RootAskUserClarification{}, errors.New("invalid ask-user answers")
	}
	trimmed := make([]string, 0, len(answers))
	for _, answer := range answers {
		text := strings.TrimSpace(answer)
		if text == "" {
			return RootAskUserClarification{}, errors.New("invalid ask-user answers")
		}
		trimmed = append(trimmed, text)
	}
	return RootAskUserClarification{
		Question: question,
		Answers:  trimmed,
		Options:  options,
	}, nil
}

// ToMapping 序列化为映射。
// Python: RootAskUserClarification.to_mapping()
func (c RootAskUserClarification) ToMapping() map[string]any {
	options := make([]any, 0, len(c.Options))
	for _, option := range c.Options {
		options = append(options, option.ToMapping())
	}
	answers := make([]any, 0, len(c.Answers))
	for _, answer := range c.Answers {
		answers = append(answers, answer)
	}
	return map[string]any{
		"question": c.Question,
		"options":  options,
		"answers":  answers,
	}
}

// RootAskUserClarificationFromMapping 从映射还原，字段集合必须完全一致。
// Python: RootAskUserClarification.from_mapping(value)
func RootAskUserClarificationFromMapping(value map[string]any) (RootAskUserClarification, error) {
	if value == nil || len(value) != 3 {
		return RootAskUserClarification{}, errors.New("invalid ask-user clarification")
	}
	rawOptions, ok1 := value["options"].([]any)
	rawAnswers, ok2 := value["answers"].([]any)
	question, errQ := strictString(value, "question")
	if !ok1 || !ok2 || errQ != nil {
		return RootAskUserClarification{}, errors.New("invalid ask-user clarification")
	}

	options := make([]RootAskUserOption, 0, len(rawOptions))
	for _, item := range rawOptions {
		asMap, ok := item.(map[string]any)
		if !ok {
			return RootAskUserClarification{}, errors.New("invalid ask-user clarification")
		}
		option, err := RootAskUserOptionFromMapping(asMap)
		if err != nil {
			return RootAskUserClarification{}, errors.New("invalid ask-user clarification")
		}
		options = append(options, option)
	}

	answers := make([]string, 0, len(rawAnswers))
	for _, item := range rawAnswers {
		text, ok := item.(string)
		if !ok {
			return RootAskUserClarification{}, errors.New("invalid ask-user clarification")
		}
		answers = append(answers, text)
	}

	clarification, err := NewRootAskUserClarification(question, answers, options)
	if err != nil {
		return RootAskUserClarification{}, errors.New("invalid ask-user clarification")
	}
	return clarification, nil
}

// NewRootIntentTurn 构造并校验一轮意图。
// Python: RootIntentTurn(...) 的 __post_init__
func NewRootIntentTurn(
	requestID string,
	kind string,
	text string,
	clarifications []RootAskUserClarification,
) (RootIntentTurn, error) {
	if len(requestID) > 512 {
		return RootIntentTurn{}, errors.New("invalid intent request id")
	}
	if _, ok := rootIntentTurnKinds[kind]; !ok {
		return RootIntentTurn{}, errors.New("invalid intent turn kind")
	}
	if kind != RootIntentTurnKindHistory && strings.TrimSpace(requestID) == "" {
		return RootIntentTurn{}, errors.New("current intent request id is required")
	}

	turn := RootIntentTurn{
		RequestID:      requestID,
		Kind:           kind,
		Text:           text,
		Clarifications: clarifications,
	}
	if kind == RootIntentTurnKindAskUserClarification {
		if text != "" || len(clarifications) < 1 ||
			len(clarifications) > rails.MaxStructuredQuestions {
			return RootIntentTurn{}, errors.New("invalid ask-user intent turn")
		}
	} else if strings.TrimSpace(text) == "" || len(clarifications) > 0 {
		return RootIntentTurn{}, errors.New("invalid user intent turn")
	}

	if len(RootIntentTurnText(turn, true)) > RootIntentMaxTurnChars {
		return RootIntentTurn{}, errors.New("intent turn exceeds capacity")
	}
	return turn, nil
}

// ToMapping 序列化为映射。
// Python: RootIntentTurn.to_mapping()
func (t RootIntentTurn) ToMapping() map[string]any {
	clarifications := make([]any, 0, len(t.Clarifications))
	for _, item := range t.Clarifications {
		clarifications = append(clarifications, item.ToMapping())
	}
	return map[string]any{
		"request_id":     t.RequestID,
		"kind":           t.Kind,
		"text":           t.Text,
		"clarifications": clarifications,
	}
}

// RootIntentTurnFromMapping 从映射还原，字段集合必须完全一致。
// Python: RootIntentTurn.from_mapping(value)
func RootIntentTurnFromMapping(value map[string]any) (RootIntentTurn, error) {
	if value == nil || len(value) != 4 {
		return RootIntentTurn{}, errors.New("invalid intent turn")
	}
	rawClarifications, ok := value["clarifications"].([]any)
	if !ok {
		return RootIntentTurn{}, errors.New("invalid intent clarifications")
	}
	requestID, err1 := strictString(value, "request_id")
	kind, err2 := strictString(value, "kind")
	text, err3 := strictString(value, "text")
	if err := firstErr(err1, err2, err3); err != nil {
		return RootIntentTurn{}, errors.New("invalid intent turn")
	}

	clarifications := make([]RootAskUserClarification, 0, len(rawClarifications))
	for _, item := range rawClarifications {
		asMap, ok := item.(map[string]any)
		if !ok {
			return RootIntentTurn{}, errors.New("invalid intent clarifications")
		}
		clarification, err := RootAskUserClarificationFromMapping(asMap)
		if err != nil {
			return RootIntentTurn{}, errors.New("invalid intent clarifications")
		}
		clarifications = append(clarifications, clarification)
	}

	return NewRootIntentTurn(requestID, kind, text, clarifications)
}

// RootIntentTurnText 返回一轮意图的文本表示。
// 非澄清轮次直接返回原文；澄清轮次序列化成 JSON，
// includeQuestion 控制是否带上问题与选项（容量计算时需要带上）。
// Python: root_intent_turn_text(turn, *, include_question=False)
func RootIntentTurnText(turn RootIntentTurn, includeQuestion bool) string {
	if turn.Kind != RootIntentTurnKindAskUserClarification {
		return turn.Text
	}

	clarifications := make([]any, 0, len(turn.Clarifications))
	for _, item := range turn.Clarifications {
		answers := make([]any, 0, len(item.Answers))
		for _, answer := range item.Answers {
			answers = append(answers, answer)
		}
		projected := map[string]any{"answers": answers}
		if includeQuestion {
			options := make([]any, 0, len(item.Options))
			for _, option := range item.Options {
				options = append(options, option.ToMapping())
			}
			projected["question"] = item.Question
			projected["options"] = options
		}
		clarifications = append(clarifications, projected)
	}

	payload, err := json.Marshal(map[string]any{"clarifications": clarifications})
	if err != nil {
		return ""
	}
	return string(payload)
}

// NewRootDecisionContext 构造并校验决策上下文。
// Python: RootDecisionContext(...) 的 __post_init__
func NewRootDecisionContext(
	sessionID string,
	requestID string,
	channelID string,
	trustedTurns []RootIntentTurn,
	autoReviewBlockReason string,
) (*RootDecisionContext, error) {
	for name, value := range map[string]string{"session_id": sessionID, "request_id": requestID} {
		if strings.TrimSpace(value) == "" || len(value) > 512 {
			return nil, fmt.Errorf("invalid root %s", name)
		}
	}
	if len(channelID) > 512 {
		return nil, errors.New("invalid root channel")
	}
	if len(trustedTurns) > RootIntentMaxTurns {
		return nil, errors.New("trusted intent exceeds capacity")
	}
	total := 0
	for _, turn := range trustedTurns {
		total += len(RootIntentTurnText(turn, true))
	}
	if total > RootIntentMaxTotalChars {
		return nil, errors.New("trusted intent exceeds capacity")
	}
	if _, ok := rootBlockReasons[autoReviewBlockReason]; !ok {
		return nil, errors.New("invalid auto-review block reason")
	}

	turns := make([]RootIntentTurn, len(trustedTurns))
	copy(turns, trustedTurns)
	return &RootDecisionContext{
		SessionID:             sessionID,
		RequestID:             requestID,
		ChannelID:             channelID,
		TrustedTurns:          turns,
		AutoReviewBlockReason: autoReviewBlockReason,
	}, nil
}

// ObjectiveText 返回最近一条非澄清轮次的原文，用于作为任务目标。
// Python: RootDecisionContext.objective_text（property）
func (c *RootDecisionContext) ObjectiveText() string {
	if c == nil {
		return ""
	}
	for i := len(c.TrustedTurns) - 1; i >= 0; i-- {
		if c.TrustedTurns[i].Kind != RootIntentTurnKindAskUserClarification {
			return c.TrustedTurns[i].Text
		}
	}
	return ""
}

// ToMapping 序列化为映射。
// Python: RootDecisionContext.to_mapping()
func (c *RootDecisionContext) ToMapping() map[string]any {
	turns := make([]any, 0, len(c.TrustedTurns))
	for _, turn := range c.TrustedTurns {
		turns = append(turns, turn.ToMapping())
	}
	return map[string]any{
		"session_id":               c.SessionID,
		"request_id":               c.RequestID,
		"channel_id":               c.ChannelID,
		"trusted_turns":            turns,
		"auto_review_block_reason": c.AutoReviewBlockReason,
	}
}

// TaskObjective 返回任务目标文本，无 context 时回退到 Text。
// Python: OriginalUserIntentEvidence.task_objective（property）
func (e OriginalUserIntentEvidence) TaskObjective() string {
	if e.Context != nil {
		return e.Context.ObjectiveText()
	}
	return e.Text
}

// CurrentUserMessage 返回当前用户消息，无 context 或无轮次时回退到 Text。
// Python: OriginalUserIntentEvidence.current_user_message（property）
func (e OriginalUserIntentEvidence) CurrentUserMessage() string {
	if e.Context == nil || len(e.Context.TrustedTurns) == 0 {
		return e.Text
	}
	return RootIntentTurnText(e.Context.TrustedTurns[len(e.Context.TrustedTurns)-1], false)
}

// RootDecisionContextFromMapping 从映射还原决策上下文，字段集合必须完全一致。
// Python: RootDecisionContext.from_mapping(value)
func RootDecisionContextFromMapping(value map[string]any) (*RootDecisionContext, error) {
	if value == nil || len(value) != 5 {
		return nil, errors.New("invalid root decision context")
	}
	rawTurns, ok := value["trusted_turns"].([]any)
	if !ok {
		return nil, errors.New("invalid trusted turns")
	}
	sessionID, err1 := strictString(value, "session_id")
	requestID, err2 := strictString(value, "request_id")
	channelID, err3 := strictString(value, "channel_id")
	blockReason, err4 := strictString(value, "auto_review_block_reason")
	if err := firstErr(err1, err2, err3, err4); err != nil {
		return nil, errors.New("invalid root decision context")
	}

	turns := make([]RootIntentTurn, 0, len(rawTurns))
	for _, item := range rawTurns {
		asMap, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("invalid trusted turns")
		}
		turn, err := RootIntentTurnFromMapping(asMap)
		if err != nil {
			return nil, errors.New("invalid trusted turns")
		}
		turns = append(turns, turn)
	}

	return NewRootDecisionContext(sessionID, requestID, channelID, turns, blockReason)
}

// CurrentRootDecisionContext 返回当前绑定的决策上下文，未绑定时为 nil。
// Python: current_root_decision_context()
func CurrentRootDecisionContext(ctx context.Context) *RootDecisionContext {
	if v, ok := ctx.Value(rootContextKey{}).(*RootDecisionContext); ok {
		return v
	}
	return nil
}

// BindRootDecisionContext 把决策上下文绑到当前任务树。
// 返回的清理函数等价于 Python 的 reset(token)：Go 的 context 不可变，
// 丢弃派生 ctx 即恢复上一层绑定。context 可为 nil（显式清空）。
// Python: bind_root_decision_context(context)
func BindRootDecisionContext(ctx context.Context, decisionContext *RootDecisionContext) (context.Context, func()) {
	return context.WithValue(ctx, rootContextKey{}, decisionContext), func() {}
}

// RootDecisionContextFromExtra 从 extra 映射里取出决策上下文。
// 值可以是 *RootDecisionContext 或 map[string]any（跨序列化边界后）。
// Python: root_decision_context_from_extra(extra)
func RootDecisionContextFromExtra(extra map[string]any) (*RootDecisionContext, error) {
	if extra == nil {
		return nil, errors.New("root decision context missing")
	}
	raw, ok := extra[RootContextKey]
	if !ok {
		return nil, errors.New("root decision context missing")
	}
	if typed, ok := raw.(*RootDecisionContext); ok {
		return typed, nil
	}
	asMap, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("invalid root decision context")
	}
	return RootDecisionContextFromMapping(asMap)
}

// PutRootDecisionContextInInputs 把决策上下文写入 inputs 的 run.context.extra。
// 分层路径与 Python 一致：inputs["run"]["context"]["extra"][ROOT_CONTEXT_KEY]。
// Python: put_root_decision_context_in_inputs(inputs, context)
func PutRootDecisionContextInInputs(inputs map[string]any, decisionContext *RootDecisionContext) map[string]any {
	result := copyMap(inputs)
	run := nestedCopyMap(result, "run")
	ctxMap := nestedCopyMap(run, "context")
	extra := nestedCopyMap(ctxMap, "extra")
	extra[RootContextKey] = decisionContext.ToMapping()
	ctxMap["extra"] = extra
	run["context"] = ctxMap
	result["run"] = run
	return result
}

// AppendRootClarification 把一批澄清作为新轮次追加到意图窗口末尾。
// 轮次上限 ROOT_INTENT_MAX_TURNS，总字符上限 ROOT_INTENT_MAX_TOTAL_CHARS；
// 超限时从最老的轮次开始丢弃，只剩一轮仍超限则清空窗口并标记阻断原因。
// Python: append_root_clarification(context, clarifications)
func AppendRootClarification(
	decisionContext *RootDecisionContext,
	clarifications []RootAskUserClarification,
) *RootDecisionContext {
	answer, err := NewRootIntentTurn(
		decisionContext.RequestID,
		RootIntentTurnKindAskUserClarification,
		"",
		clarifications,
	)
	if err != nil {
		// Python 侧由 dataclass 构造抛异常，Go 侧降级为不追加
		return decisionContext
	}

	// 只保留最近 ROOT_INTENT_MAX_TURNS-1 轮，再追加本轮
	start := 0
	if len(decisionContext.TrustedTurns) > RootIntentMaxTurns-1 {
		start = len(decisionContext.TrustedTurns) - (RootIntentMaxTurns - 1)
	}
	// 精确分配容量，避免后续 append 复用底层数组污染上游切片
	kept := len(decisionContext.TrustedTurns) - start
	turns := make([]RootIntentTurn, 0, kept+1)
	turns = append(turns, decisionContext.TrustedTurns[start:]...)
	turns = append(turns, answer)

	truncated := len(decisionContext.TrustedTurns) >= RootIntentMaxTurns
	for totalChars(turns) > RootIntentMaxTotalChars {
		if len(turns) == 1 {
			replaced, err := NewRootDecisionContext(
				decisionContext.SessionID,
				decisionContext.RequestID,
				decisionContext.ChannelID,
				[]RootIntentTurn{},
				AutoReviewBlockHistoryWindowTruncated,
			)
			if err != nil {
				return decisionContext
			}
			return replaced
		}
		// Python 的 turns.pop(0) 从队首丢弃。显式复制而非 turns[1:]，
		// 避免与上游切片共用底层数组导致后续写入污染原 context。
		next := make([]RootIntentTurn, len(turns)-1)
		copy(next, turns[1:])
		turns = next
		truncated = true
	}

	blockReason := decisionContext.AutoReviewBlockReason
	if truncated {
		blockReason = AutoReviewBlockHistoryWindowTruncated
	}
	replaced, err := NewRootDecisionContext(
		decisionContext.SessionID,
		decisionContext.RequestID,
		decisionContext.ChannelID,
		turns,
		blockReason,
	)
	if err != nil {
		return decisionContext
	}
	return replaced
}

// HasValidAskUserClarification 判断证据里是否存在任一澄清轮次。
// ⤵️ 消费方待 9.90d（_auto_permission）批次回填：Python 侧由 url_safety.py 调用。
// Python: has_valid_ask_user_clarification(evidence)
func HasValidAskUserClarification(evidence *OriginalUserIntentEvidence) bool {
	if evidence == nil || evidence.Context == nil {
		return false
	}
	for _, turn := range evidence.Context.TrustedTurns {
		if len(turn.Clarifications) > 0 {
			return true
		}
	}
	return false
}

// OriginalUserIntent 从当前上下文构造原始用户意图证据。
// ⤵️ 消费方待 9.90d（_auto_permission/before_tool.py）批次回填。
// Python: original_user_intent()
func OriginalUserIntent(ctx context.Context) *OriginalUserIntentEvidence {
	decisionContext := CurrentRootDecisionContext(ctx)
	if decisionContext == nil || len(decisionContext.TrustedTurns) == 0 {
		return nil
	}
	return &OriginalUserIntentEvidence{
		Source: UserIntentSourceHostUserMessage,
		Text: RootIntentTurnText(
			decisionContext.TrustedTurns[len(decisionContext.TrustedTurns)-1], false,
		),
		Context: decisionContext,
	}
}

// FileDeliveryConstraintText 把意图窗口拼成一段文字，供文件投递约束判断。
// ⤵️ 消费方待 9.90d（_auto_permission/before_tool.py）批次回填。
// Python: file_delivery_constraint_text(evidence)
func FileDeliveryConstraintText(evidence *OriginalUserIntentEvidence) string {
	if evidence == nil {
		return ""
	}
	if evidence.Context == nil {
		return evidence.Text
	}
	parts := make([]string, 0, len(evidence.Context.TrustedTurns))
	for _, turn := range evidence.Context.TrustedTurns {
		if text := RootIntentTurnText(turn, false); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// ExtractPermissionUserContent 从渲染后的宿主提示词里提取用户原文。
// 只接受指定前缀 + 合法信封 JSON 的形态，其余一律返回 nil（fail-closed）：
// 信封键集合、type、origin_kind、source 黑名单、时间戳与语言字段、
// files_updated_by_user 的 JSON 对象字符串均需校验通过。
// Python: extract_permission_user_content(rendered_prompt)
func ExtractPermissionUserContent(renderedPrompt any) *string {
	prompt, ok := renderedPrompt.(string)
	if !ok {
		return nil
	}
	var raw string
	matched := false
	for _, prefix := range []string{HostUserPromptPrefixZh, HostUserPromptPrefixEn} {
		if strings.HasPrefix(prompt, prefix) {
			raw = prompt[len(prefix):]
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}

	envelope, end, err := decodeStrictObject(raw)
	if err != nil {
		return nil
	}
	if strings.TrimSpace(raw[end:]) != "" {
		return nil
	}

	keys := make(map[string]struct{}, len(envelope))
	for key := range envelope {
		keys[key] = struct{}{}
	}
	for _, key := range requiredEnvelopeKeys {
		if _, ok := keys[key]; !ok {
			return nil
		}
	}
	for key := range keys {
		if !containsString(requiredEnvelopeKeys, key) && !containsString(optionalEnvelopeKeys, key) {
			return nil
		}
	}

	if envelopeString(envelope, "type") != "user input" {
		return nil
	}
	if envelopeString(envelope, "origin_kind") != HostUserOriginExternal {
		return nil
	}
	source := envelopeString(envelope, "source")
	if strings.TrimSpace(source) == "" {
		return nil
	}
	lowerSource := strings.ToLower(strings.TrimSpace(source))
	if lowerSource == "system" || lowerSource == "cron" || lowerSource == "heartbeat" {
		return nil
	}
	for _, name := range []string{"timestamp", "preferred_response_language"} {
		if strings.TrimSpace(envelopeString(envelope, name)) == "" {
			return nil
		}
	}
	if !isJSONObjectString(envelope["files_updated_by_user"]) {
		return nil
	}

	content, ok := envelope["content"].(string)
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// BuildRootIntentProjection 从消息列表投影意图轮次。
// 逆序遍历（由新到旧）按轮次与总字符上限挑选，最后反转回时间顺序。
// 阻断原因按“先到先记”原则（block = block or 新原因）。
// Python: build_root_intent_projection(messages, *, context_available, current_text,
// current_request_id, current_kind)
func BuildRootIntentProjection(
	messages []any,
	contextAvailable bool,
	currentText string,
	currentRequestID string,
	currentKind string,
) RootIntentProjection {
	block := ""

	// turns 中 nil 表示"该轮超长被丢弃"，用指针区分
	type slot struct {
		turn *RootIntentTurn
	}
	slots := make([]slot, 0)

	if contextAvailable {
		for _, message := range messages {
			role := messageRole(message)
			if strings.ToLower(strings.TrimSpace(role)) != "user" {
				continue
			}
			content := ExtractPermissionUserContent(messageContent(message))
			if content == nil {
				continue
			}
			if len(*content) > RootIntentMaxTurnChars {
				slots = append(slots, slot{turn: nil})
				continue
			}
			turn, err := NewRootIntentTurn("", RootIntentTurnKindHistory, *content, nil)
			if err != nil {
				slots = append(slots, slot{turn: nil})
				continue
			}
			slots = append(slots, slot{turn: &turn})
		}
	}

	current := strings.TrimSpace(currentText)
	if current != "" {
		if len(current) > RootIntentMaxTurnChars {
			block = AutoReviewBlockIntentInputTooLarge
		} else {
			turn, err := NewRootIntentTurn(currentRequestID, currentKind, current, nil)
			if err == nil {
				slots = append(slots, slot{turn: &turn})
			}
		}
	}

	selected := make([]RootIntentTurn, 0)
	total := 0
	for i := len(slots) - 1; i >= 0; i-- {
		if slots[i].turn == nil || len(selected) >= RootIntentMaxTurns {
			if block == "" {
				block = AutoReviewBlockHistoryWindowTruncated
			}
			break
		}
		length := len(RootIntentTurnText(*slots[i].turn, true))
		if total+length > RootIntentMaxTotalChars {
			if block == "" {
				block = AutoReviewBlockHistoryWindowTruncated
			}
			break
		}
		selected = append(selected, *slots[i].turn)
		total += length
	}
	// 反转回时间顺序
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}

	return RootIntentProjection{Turns: selected, AutoReviewBlockReason: block}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strictString 取映射里的字符串字段，缺失或非字符串均返回错误。
func strictString(value map[string]any, key string) (string, error) {
	raw, ok := value[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("invalid %s", key)
	}
	return text, nil
}

// firstErr 返回第一个非 nil 错误。
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// copyMap 浅拷贝映射，nil 返回空映射。
func copyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

// nestedCopyMap 浅拷贝 source[key]（若为映射），否则返回新的空映射。
func nestedCopyMap(source map[string]any, key string) map[string]any {
	if nested, ok := source[key].(map[string]any); ok {
		return copyMap(nested)
	}
	return make(map[string]any)
}

// totalChars 累计一组轮次的容量占用（含问题与选项）。
func totalChars(turns []RootIntentTurn) int {
	total := 0
	for _, turn := range turns {
		total += len(RootIntentTurnText(turn, true))
	}
	return total
}

// containsString 判断字符串是否在切片中。
func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// envelopeString 取信封里的字符串字段，非字符串返回空串。
func envelopeString(envelope map[string]any, key string) string {
	if text, ok := envelope[key].(string); ok {
		return text
	}
	return ""
}

// decodeStrictObject 解析 JSON 对象，重复键视为错误，返回已消费的字节数。
// Python: json.JSONDecoder(object_pairs_hook=_strict_object).raw_decode(raw)
func decodeStrictObject(raw string) (map[string]any, int, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	token, err := decoder.Token()
	if err != nil {
		return nil, 0, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, 0, errors.New("not an object")
	}

	result := make(map[string]any)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, 0, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, 0, errors.New("invalid key")
		}
		if _, dup := result[key]; dup {
			return nil, 0, errors.New("duplicate JSON object key")
		}
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, 0, err
		}
		result[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, 0, err
	}

	consumed := int(decoder.InputOffset())
	return result, consumed, nil
}

// isJSONObjectString 判断值是否为 JSON 对象字符串。
// Python: _is_json_object_string(value)
func isJSONObjectString(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return false
	}
	_, ok = parsed.(map[string]any)
	return ok
}

// messageRole 取消息的 role，兼容字符串与带 value 字段的枚举形态。
// Python: getattr(getattr(message, "role", ""), "value", getattr(message, "role", ""))
func messageRole(message any) string {
	switch typed := message.(type) {
	case map[string]any:
		if role, ok := typed["role"].(string); ok {
			return role
		}
		if nested, ok := typed["role"].(map[string]any); ok {
			if value, ok := nested["value"].(string); ok {
				return value
			}
		}
	}
	return ""
}

// messageContent 取消息的 content 字段。
// Python: getattr(message, "content", None)
func messageContent(message any) any {
	if typed, ok := message.(map[string]any); ok {
		return typed["content"]
	}
	return nil
}
