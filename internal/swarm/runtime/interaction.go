// 回答一次 Runtime 交互的传输无关输入。
//
// 对应 Python 代码：jiuwenswarm/runtime/interaction.py（237 行）
//
// 作用：把"用户回答了某个提问/审批"这件事，从各渠道的原始 request 形态里剥出来，
// 变成类型化的 InteractionAnswerInput，再由 to_agent_request 还原成 Runtime 消费的
// AgentRequest。这样渠道层不必知道 chat.send 与 chat.answer 的区别。
//
// 关键产品语义（上游注释原文）：interrupt 来源的回答"接着被打断的轮次跑"
// （chat.send + is_stream），普通交互回答则是独立一轮（chat.answer）。

package runtime

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InteractionAnswerInput 回答一次交互，不暴露传输层 request 对象。
//
// Python: interaction.py:106-130 @dataclass(frozen=True, slots=True, kw_only=True)
type InteractionAnswerInput struct {
	// RequestID 请求标识（必填）
	RequestID string
	// ChannelID 渠道标识（必填）
	ChannelID string
	// SessionID 会话标识（必填）
	SessionID string
	// InteractionID 被回答的交互标识（必填）
	InteractionID string
	// Answers 回答对象列表（深拷贝，不允许为空）
	Answers []map[string]any
	// Source 来源（决定是否续跑被打断的轮次）
	Source string
	// InterruptID 中断标识
	InterruptID string
	// ApprovalSchema 审批 schema 标识
	ApprovalSchema string
	// EvolutionMeta 技能演进审批元数据
	EvolutionMeta map[string]any
	// Mode 运行模式
	Mode string
	// WorkMode 工作模式（work / code）
	WorkMode string
	// ProjectDir 工程目录
	ProjectDir string
	// Cwd 当前工作目录
	Cwd string
	// TrustedDirs 受信目录（不得重复）
	TrustedDirs []string
	// RunPermissions 请求级权限覆盖
	RunPermissions map[string]any
}

// InteractionAnswerError 交互边界的稳定校验失败。
//
// Python: interaction.py:47-51 class InteractionAnswerError(ValueError)
type InteractionAnswerError struct {
	// Message 失败原因
	Message string
}

// Error 实现 error 接口。
func (e *InteractionAnswerError) Error() string { return e.Message }

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InteractionBadRequestCode 交互校验失败的稳定错误码
	// Python: interaction.py:49 code = "INTERACTION_BAD_REQUEST"
	InteractionBadRequestCode = "INTERACTION_BAD_REQUEST"

	// SkillEvolutionApprovalSchema 技能演进审批 schema
	// Python: evolution.py:7 SKILL_EVOLUTION_APPROVAL_SCHEMA
	SkillEvolutionApprovalSchema = "openjiuwen.skill_evolution_approval.v1"

	// SkillEvolutionApprovalSource 技能演进审批来源
	// Python: evolution.py:8 SKILL_EVOLUTION_APPROVAL_SOURCE
	SkillEvolutionApprovalSource = "skill_evolution_approval"

	// evolutionInterruptSource 演进中断来源
	// Python: evolution.py:46 payload.get("source") == "evolution_interrupt"
	evolutionInterruptSource = "evolution_interrupt"

	// interactionIDKeyRequestID 按 request_id 索引交互的事件键
	// Python: interaction.py:33 "request_id"
	interactionIDKeyRequestID = "request_id"

	// interactionIDKeyInteractionID 按 interaction_id 索引交互的事件键
	// Python: interaction.py:35 "interaction_id"
	interactionIDKeyInteractionID = "interaction_id"

	// eventTypeHarnessActivateInteraction 激活确认交互事件
	// Python: interaction.py:35 "harness.activate_interaction"
	eventTypeHarnessActivateInteraction = "harness.activate_interaction"

	// eventTypeChatAskUserQuestion 向用户提问事件
	// Python: interaction.py:29 "chat.ask_user_question"
	eventTypeChatAskUserQuestion = "chat.ask_user_question"

	// workModeWork 工作模式
	// Python: interaction.py:26 _WORK_MODES = frozenset({"work", "code"})
	workModeWork = "work"

	// workModeCode 代码模式
	workModeCode = "code"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// UserInteractionEvents 会向用户（或其客户端）索取回答、且轮次会等待的事件类型。
// Python: interaction.py:29 USER_INTERACTION_EVENTS
var UserInteractionEvents = map[string]struct{}{
	eventTypeChatAskUserQuestion: {},
	"chat.client_tool_request":   {},
}

// interactionIDKeys 每种事件类型里承载交互标识的 payload 键。
// Python: interaction.py:32-36 _INTERACTION_ID_KEYS
var interactionIDKeys = map[string]string{
	eventTypeChatAskUserQuestion:        interactionIDKeyRequestID,
	"chat.client_tool_request":          interactionIDKeyRequestID,
	eventTypeHarnessActivateInteraction: interactionIDKeyInteractionID,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// InteractionIDs 返回一个事件所要回答的交互标识（不索取则为空集）。
// Python: interaction.py:39-45 interaction_ids
func InteractionIDs(event *RuntimeEvent) map[string]struct{} {
	if event == nil {
		return map[string]struct{}{}
	}
	rawEventType, _ := event.Payload["event_type"].(string)
	eventType := strings.TrimSpace(rawEventType)
	key, ok := interactionIDKeys[eventType]
	if !ok {
		return map[string]struct{}{}
	}
	raw := ""
	if text, okString := event.Payload[key].(string); okString {
		raw = text
	}
	interactionID := strings.TrimSpace(raw)
	if interactionID == "" {
		return map[string]struct{}{}
	}
	return map[string]struct{}{interactionID: {}}
}

// AskedInteractionIDs 返回一串事件所要回答的全部交互标识。
// Python: interaction.py:47-49 asked_interaction_ids
func AskedInteractionIDs(events []*RuntimeEvent) map[string]struct{} {
	asked := map[string]struct{}{}
	for _, event := range events {
		for id := range InteractionIDs(event) {
			asked[id] = struct{}{}
		}
	}
	return asked
}

// ValidateAnswerItems 校验并深拷贝回答对象。
// Python: interaction.py:69-81 validate_answer_items
//
// 拒绝字符串/字节这类"长得像序列但不是对象序列"的输入（上游显式排除，
// 否则 "abc" 会被逐字符当成对象处理，报出的错误毫无意义）。
func ValidateAnswerItems(value []any) ([]map[string]any, error) {
	if value == nil {
		return nil, &InteractionAnswerError{Message: "answers must be a sequence of objects"}
	}
	result := make([]map[string]any, 0, len(value))
	for index, item := range value {
		mapping, ok := item.(map[string]any)
		if !ok {
			return nil, &InteractionAnswerError{
				Message: "answers[" + strconv.Itoa(index) + "] must be an object",
			}
		}
		result = append(result, deepCopyMapping(mapping))
	}
	if len(result) == 0 {
		return nil, &InteractionAnswerError{Message: "answers must not be empty"}
	}
	return result, nil
}

// ResumesInterruptedTurn 本次回答是否针对一个被挂起的执行。
// Python: interaction.py:170-186 InteractionAnswerInput.resumes_interrupted_turn
//
// 两条判定：
//  1. 来源属于中断恢复来源集合 → 是
//  2. 退化到演进审批 payload 判定（request_id 前缀 call_ / 来源 evolution_interrupt /
//     evolution_meta.approval_transport == "interrupt"）
func (a *InteractionAnswerInput) ResumesInterruptedTurn() bool {
	if _, ok := InterruptResumeSources[a.Source]; ok {
		return true
	}
	payload := map[string]any{
		"request_id": a.InteractionID,
		"source":     a.Source,
	}
	if a.ApprovalSchema != "" {
		payload["approval_schema"] = a.ApprovalSchema
	}
	if a.EvolutionMeta != nil {
		payload["evolution_meta"] = deepCopyMapping(a.EvolutionMeta)
	}
	return isInterruptEvolutionApprovalAnswerPayload(payload)
}

// ToAgentRequest 构造 Runtime 消费的内部请求。
// Python: interaction.py:188-232 InteractionAnswerInput.to_agent_request
//
// 关键点：interrupt 来源用 chat.send（续跑被打断的轮次，is_stream=true），
// 普通交互用 chat.answer；query 在续跑时为空串、否则为 nil（Python None）。
func (a *InteractionAnswerInput) ToAgentRequest() (*schema.AgentRequest, error) {
	resumes := a.ResumesInterruptedTurn()

	answers := make([]any, 0, len(a.Answers))
	for _, item := range a.Answers {
		answers = append(answers, deepCopyMapping(item))
	}

	params := map[string]any{
		"session_id":                a.SessionID,
		"request_id":                a.InteractionID,
		"answers":                   answers,
		"source":                    a.Source,
		"supports_user_interaction": true,
	}
	// Python:196 "query": "" if resumes else None
	if resumes {
		params["query"] = ""
	} else {
		params["query"] = nil
	}

	// Python:198-205 仅在非空时写入的可选项
	optional := map[string]string{
		"interrupt_id":    a.InterruptID,
		"mode":            a.Mode,
		"work_mode":       a.WorkMode,
		"project_dir":     a.ProjectDir,
		"cwd":             a.Cwd,
		"approval_schema": a.ApprovalSchema,
	}
	for key, value := range optional {
		if value != "" {
			params[key] = value
		}
	}
	if a.EvolutionMeta != nil {
		params["evolution_meta"] = deepCopyMapping(a.EvolutionMeta)
	}
	if len(a.TrustedDirs) > 0 {
		dirs := make([]any, 0, len(a.TrustedDirs))
		for _, dir := range a.TrustedDirs {
			dirs = append(dirs, dir)
		}
		params["trusted_dirs"] = dirs
	}
	if a.RunPermissions != nil {
		params["run_permissions"] = deepCopyMapping(a.RunPermissions)
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	reqMethod := schema.ReqMethodChatAnswer
	if resumes {
		reqMethod = schema.ReqMethodChatSend
	}
	sessionID := a.SessionID
	return &schema.AgentRequest{
		RequestID: a.RequestID,
		ChannelID: a.ChannelID,
		SessionID: &sessionID,
		ReqMethod: reqMethod,
		Params:    encoded,
		IsStream:  resumes,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requiredText 取必填文本（去空白后为空即报错）。
// Python: interaction.py:54-59 _required_text
func interactionRequiredText(name, value string) (string, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return "", &InteractionAnswerError{Message: name + " is required"}
	}
	return text, nil
}

// optionalText 取可选文本（去空白）。
// Python: interaction.py:61-67 _optional_text
func interactionOptionalText(name, value string) (string, error) {
	_ = name
	return strings.TrimSpace(value), nil
}

// trustedDirs 校验受信目录（不得重复）。
// Python: interaction.py:83-90 _trusted_dirs
func trustedDirs(value []string) ([]string, error) {
	result := make([]string, 0, len(value))
	seen := make(map[string]struct{}, len(value))
	for _, item := range value {
		text, err := interactionRequiredText("trusted_dirs item", item)
		if err != nil {
			return nil, err
		}
		if _, duplicated := seen[text]; duplicated {
			return nil, &InteractionAnswerError{
				Message: "trusted_dirs must not contain duplicates",
			}
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result, nil
}

// optionalMapping 取可选映射（键必须是字符串）。
// Python: interaction.py:92-104 _optional_mapping
func optionalMapping(name string, value map[string]any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	return deepCopyMapping(value), nil
}

// NewInteractionAnswerInput 构造并校验一次交互回答。
// Python: interaction.py:132-168 InteractionAnswerInput.__post_init__
//
// 校验顺序与上游一致：四个必填 → answers → 各可选文本 → work_mode 取值 → trusted_dirs。
func NewInteractionAnswerInput(input InteractionAnswerInput) (*InteractionAnswerInput, error) {
	out := input

	requestID, err := interactionRequiredText("request_id", input.RequestID)
	if err != nil {
		return nil, err
	}
	out.RequestID = requestID

	channelID, err := interactionRequiredText("channel_id", input.ChannelID)
	if err != nil {
		return nil, err
	}
	out.ChannelID = channelID

	sessionID, err := interactionRequiredText("session_id", input.SessionID)
	if err != nil {
		return nil, err
	}
	out.SessionID = sessionID

	interactionID, err := interactionRequiredText("interaction_id", input.InteractionID)
	if err != nil {
		return nil, err
	}
	out.InteractionID = interactionID

	answers, err := ValidateAnswerItems(rawAnswersFrom(input.Answers))
	if err != nil {
		return nil, err
	}
	out.Answers = answers

	if out.Source, err = interactionOptionalText("source", input.Source); err != nil {
		return nil, err
	}
	if out.InterruptID, err = interactionOptionalText("interrupt_id", input.InterruptID); err != nil {
		return nil, err
	}
	if out.ApprovalSchema, err = interactionOptionalText("approval_schema", input.ApprovalSchema); err != nil {
		return nil, err
	}
	if out.EvolutionMeta, err = optionalMapping("evolution_meta", input.EvolutionMeta); err != nil {
		return nil, err
	}
	if out.Mode, err = interactionOptionalText("mode", input.Mode); err != nil {
		return nil, err
	}

	workMode, err := interactionOptionalText("work_mode", input.WorkMode)
	if err != nil {
		return nil, err
	}
	workMode = strings.ToLower(workMode)
	// Python:157-159 非空时必须是 work / code
	if workMode != "" && workMode != workModeWork && workMode != workModeCode {
		return nil, &InteractionAnswerError{Message: "work_mode must be 'work' or 'code'"}
	}
	out.WorkMode = workMode

	if out.ProjectDir, err = interactionOptionalText("project_dir", input.ProjectDir); err != nil {
		return nil, err
	}
	if out.Cwd, err = interactionOptionalText("cwd", input.Cwd); err != nil {
		return nil, err
	}
	if out.TrustedDirs, err = trustedDirs(input.TrustedDirs); err != nil {
		return nil, err
	}
	if out.RunPermissions, err = optionalMapping("run_permissions", input.RunPermissions); err != nil {
		return nil, err
	}
	return &out, nil
}

// isEvolutionApprovalRequestID 是否为演进审批的 request_id 前缀。
// Python: evolution.py:11-15 is_evolution_approval_request_id
func isEvolutionApprovalRequestID(requestID any) bool {
	text, ok := requestID.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(text, "skill_evolve_") ||
		strings.HasPrefix(text, "team_skill_evolve_")
}

// isEvolutionApprovalPayload 是否为演进审批 payload。
// Python: evolution.py:18-38 is_evolution_approval_payload
//
// 非字符串的 source 一律不匹配：payload 来自渠道边界，可能是畸形 JSON，
// 用集合查找会因 list/dict 不可哈希而抛错，故显式做类型判断。
func isEvolutionApprovalPayload(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if isEvolutionApprovalRequestID(payload["request_id"]) {
		return true
	}
	if source, ok := payload["source"].(string); ok {
		if source == evolutionInterruptSource || source == SkillEvolutionApprovalSource {
			return true
		}
	}
	if schemaValue, ok := payload["approval_schema"].(string); ok {
		if schemaValue == SkillEvolutionApprovalSchema {
			return true
		}
	}
	if meta, ok := payload["evolution_meta"].(map[string]any); ok {
		if kind, okKind := meta["event_kind"].(string); okKind && kind == "approval" {
			return true
		}
	}
	return false
}

// isInterruptEvolutionApprovalAnswerPayload 是否为"经中断通道"的演进审批回答。
// Python: evolution.py:41-52 is_interrupt_evolution_approval_answer_payload
func isInterruptEvolutionApprovalAnswerPayload(payload map[string]any) bool {
	if !isEvolutionApprovalPayload(payload) {
		return false
	}
	if requestID, ok := payload["request_id"].(string); ok {
		if strings.HasPrefix(requestID, "call_") {
			return true
		}
	}
	if source, ok := payload["source"].(string); ok && source == evolutionInterruptSource {
		return true
	}
	meta, ok := payload["evolution_meta"].(map[string]any)
	if !ok {
		return false
	}
	transport, ok := meta["approval_transport"].(string)
	return ok && transport == "interrupt"
}

// rawAnswersFrom 把已类型化的回答列表转回 []any 以便复用 ValidateAnswerItems。
func rawAnswersFrom(answers []map[string]any) []any {
	if answers == nil {
		return nil
	}
	out := make([]any, 0, len(answers))
	for _, item := range answers {
		out = append(out, item)
	}
	return out
}

// deepCopyMapping 深拷贝一层映射（值里的 map/slice 递归复制）。
// Python: copy.deepcopy(dict(...))
//
// 上游用 deepcopy 是为了让调用方之后改原对象不影响已构造的输入；
// Go 侧同样必须复制，否则 answers / evolution_meta 会被外部改动。
func deepCopyMapping(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out[key] = deepCopyValue(source[key])
	}
	return out
}

// deepCopyValue 递归复制任意值。
func deepCopyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCopyMapping(typed)
	case []any:
		copied := make([]any, 0, len(typed))
		for _, item := range typed {
			copied = append(copied, deepCopyValue(item))
		}
		return copied
	case []string:
		return append([]string{}, typed...)
	default:
		return value
	}
}
