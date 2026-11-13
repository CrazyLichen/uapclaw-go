package recommendation

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// TERMINAL_CONTRACT_VERSION 终局契约版本（对齐 topic_terminal.py:12）
const TERMINAL_CONTRACT_VERSION = "topic-starter-v6-terminal-v2"

// COMPONENT_RECEIPT_SCHEMA_VERSION 组件小票版本（对齐 :13）
const COMPONENT_RECEIPT_SCHEMA_VERSION = "topic-starter-v6-component-receipt-v2"

// SESSION_SUMMARY_SCHEMA_VERSION 会话摘要版本（对齐 :14）
const SESSION_SUMMARY_SCHEMA_VERSION = "topic-starter-v6-session-summary-v2"

// LONG_TERM_PROFILE_INTEREST_SCHEMA_VERSION 长期画像兴趣版本（对齐 :15-17）
const LONG_TERM_PROFILE_INTEREST_SCHEMA_VERSION = "topic-starter-v6-long-term-profile-interest-v1"

// RID_RESULT_SCHEMA_VERSION RID 结果版本（对齐 :18）
const RID_RESULT_SCHEMA_VERSION = "topic-starter-v6-rid-result-v2"

// RID_API_PROMPT_VERSION RID API 代理提示词版本（对齐 :19）
const RID_API_PROMPT_VERSION = "topic-starter-v6-rid-api-proxy-v20"

// RID_COMPONENT_ID RID 组件 ID（对齐 :20）
const RID_COMPONENT_ID = "icebreaker_rid"

// RID_STAGE_NAME RID 蒸馏阶段名（对齐 ArkRIDDistillationAdapter.__call__ :165-171）
const RID_STAGE_NAME = "rid_interest_distiller"

// terminalComponentIDs 支持的终局组件集合（对齐 :175-182 _COMPONENT_IDS）
var terminalComponentIDs = map[string]bool{
	RID_COMPONENT_ID:             true,
	"pa_bridge_fixed_retriever":  true,
	"verbatim_web_evidence":      true,
	"interaction_utility_scorer": true,
	"diversity_scorer":           true,
}

// terminalScoreSemantics 评分语义集合（对齐 :183-185 _SCORE_SEMANTICS）
var terminalScoreSemantics = map[string]bool{"uncalibrated_api_proxy": true, "not_scored": true}

// terminalOutputStatuses 输出状态集合（对齐 :186-187 _OUTPUT_STATUSES）
var terminalOutputStatuses = map[string]bool{"success": true, "fallback": true, "failed": true}

// ──────────────────────────── 全局变量 ────────────────────────────

// ridAPISystemPrompt RID API 代理提示词（对齐 :22-174 _RID_API_SYSTEM_PROMPT，
// f-string 插值 RID_API_PROMPT_VERSION，其余逐字复刻）。
const ridAPISystemPrompt = `You are the Topic Starter IceBreaker-adapted RID API proxy
(` + RID_API_PROMPT_VERSION + `).

Task. Distill this user's trigger interests: the subjects this person would
willingly pick up again in a fresh conversation today. Judge every supplied
long-term profile interest and completed-session summary, then return the
subset that qualifies, ranked. The host records every omitted input as rejected.

Done when every live subject the activity_group rule keeps is selected, every
context-only item is left unselected, and selected ranks run from 1 with the
strongest revisit evidence first. A history with several live subjects yields
several trigger interests. Select zero to N of the N inputs; return an empty
decisions array only when no live subject remains.

What qualifies. A subject is an activity, study thread, project or curiosity of
the user's own. Revisit evidence makes it a trigger interest: the user came back
to it across sessions, is in the middle of it, or names it a lasting interest. A
subject expressed once still qualifies while the user is clearly inside it. An
errand run for someone else, a one-off lookup with no follow-up, or a question
asked on another person's behalf is not the user's subject.

Context items shape how a subject continues and are never selected on their
own: format preferences, operating constraints, schedules, pauses, completions
and status notes. Read each onto the subject it belongs to. When a chosen
summary only resumes something or names a generic action (continue, check,
set up), also select the supplied summary that identifies what is resumed,
its system, tool, data or constraints, so downstream generators can cite it;
this carries context for that subject, not the other tasks of that session.

Currency. The newest evidence about a subject decides. A finished task closes
the task and leaves its subject live: a finished tube change closes the repair,
tyre care remains a subject. A constraint narrows a subject ("no lessons this
month", "only songs I already know") and keeps it live under that limit. A pause
that covers the whole subject and is still in force makes it not-now; a pause
with its own end ("nothing tonight", "resume tomorrow", "paused this week, still
a long-term interest") leaves the subject live, ranked below current threads. A
request not to recommend or discuss a subject until a time or condition is met
makes it not-now until then, even if it remains a long-term interest. A
wish that holds only once a fact is established keeps the subject live with the
condition intact; checking the condition becomes a subject only when the user
explicitly asks for that. Explicit cancellation, "no longer needed" and handing
the matter to someone else close the subject unless newer evidence reopens it.
Assistant-covered explanations tell downstream what to avoid repeating; they are
neither user preferences nor proof that the user completed anything, and a user
who still finds something hard has a live subject in that difficulty.

Activity group. activity_group steady: select every live subject. active: the
user carries many subjects, so keep those with the strongest revisit evidence,
still several when several exist. Use the host-supplied activity_group and
threshold_version as given.

Lineage. When decision_context.short_delta_lineage_required is true, its
required_short_delta_interest_summary_ids name the summaries of the newest
change. This marks its source, not whether it is a live subject. Apply the
context and currency rules above: include the listed summary that best
identifies a qualifying live subject, then the sustained subjects it connects
to. Leave a context-only or not-now change unselected and select the other live
subjects as usual. When decision_context is absent, this rule does not apply.

Feedback. A negative preference with direction_anchor withholds that named user
subject, not every subject sharing its source_id or source_event_ids; those
fields record lineage. Keep a multi-subject summary selected when it still
identifies another live, unblocked subject.

Ranking. Long-term profile interests and session summaries are ranked together
in one order: the selected items take 1, 2, 3 and so on across both lists, with
no ties and no restart for the second list. Strongest revisit evidence first;
among equals the newer subject, then supplied order. The selection is compact:
when several summaries cover one subject, select the newest one plus any that
add a distinct facet, not every session.

Examples, demonstration only; this user's facts are in the payload:
- Guitar practice across five sessions; newest summary: rehearsal cancelled, no
  music tonight -> guitar practice selected; the pause covers one evening.
- "Resumed chess endgame study this week" plus "stopped a recurring blunder,
  wrote a note on checking loose pieces" -> both selected: the study thread and
  the checking habit the note captures; the blunder itself is closed.
- "Asked how a friend's child could start learning Japanese" -> unselected,
  asked for someone else.
- "Prefers one worked example per reply"; "will not drill into the rented
  flat's walls"; "listed three openings to look at later"; "only studies with
  free materials" -> unselected context: a reply format, a constraint, a status
  note and a scope limit; apply them to the selected subjects.
- "No Japanese practice this week, but fine to discuss it" -> selected,
  ranked below the current threads.
- "Do not recommend Japanese topics this week; still a long-term interest" ->
  unselected until the week ends.
- "Plan the wall shelf only once the landlord agrees" -> selected with the
  condition intact; nothing is selected for checking the landlord's answer.

Output. Return exactly {"decisions":[{"interest_summary_id":"...",
"selected":true,"rank":1,"resonance_score_nullable":null}]} with no extra
fields or explanation. Every interest_summary_id comes from the supplied inputs
and appears at most once; unselected items use rank null; resonance_score_nullable
must be null in this API-proxy lane.
`

// RIDAPISystemPrompt 导出提示词供 golden 断言使用。
func RIDAPISystemPrompt() string { return ridAPISystemPrompt }

// ──────────────────────────── 结构体 ────────────────────────────

// SessionCompletionBoundary 关闭一个已完成会话前缀的确切持久历史行。
// Python: server/runtime/session/session_history.py:68-116。
// ⤴️ 31d：Go 侧收敛至 internal/swarm/server/session 后替换为真实类型并删除本定义
// （HistoryStateStore 同款过渡决策，spec D-c-5）。
type SessionCompletionBoundary struct {
	SessionID   string  `json:"session_id"`
	EventID     string  `json:"event_id"`
	RequestID   string  `json:"request_id"`
	Ordinal     int     `json:"ordinal"`
	CompletedAt float64 `json:"completed_at"`
	Status      string  `json:"status"`
}

// sessionRequestTerminalStatuses 请求终态白名单（对齐 session_history.py:40-47）。
var sessionRequestTerminalStatuses = map[string]bool{
	"success": true, "error": true, "cancelled": true,
	"failed": true, "waiting_user": true, "unknown": true,
}

// NewSessionCompletionBoundary 构造并校验（对齐 :78-93 __post_init__）。
func NewSessionCompletionBoundary(sessionID, eventID, requestID string, ordinal int, completedAt float64, status string) (*SessionCompletionBoundary, error) {
	b := &SessionCompletionBoundary{
		SessionID: sessionID, EventID: eventID, RequestID: requestID,
		Ordinal: ordinal, CompletedAt: completedAt, Status: status,
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b, nil
}

// validate 校验（对齐 :78-93）。
func (b *SessionCompletionBoundary) validate() error {
	rejected := b.SessionID == "" || b.EventID == "" || b.RequestID == "" ||
		b.Ordinal < 0 || !isFiniteNonNegative(b.CompletedAt) ||
		!sessionRequestTerminalStatuses[b.Status]
	if rejected {
		return fmt.Errorf("invalid session completion boundary")
	}
	return nil
}

// AsPayload 序列化（对齐 :95-103 as_payload）。
func (b *SessionCompletionBoundary) AsPayload() map[string]any {
	return map[string]any{
		"session_id": b.SessionID, "event_id": b.EventID, "request_id": b.RequestID,
		"ordinal": b.Ordinal, "completed_at": b.CompletedAt, "status": b.Status,
	}
}

// BoundaryFromPayload 反序列化（对齐 :105-116 from_payload）。
func BoundaryFromPayload(payload any) (*SessionCompletionBoundary, error) {
	m, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid session completion boundary payload")
	}
	if !payloadKeySetEqual(m, "session_id", "event_id", "request_id", "ordinal", "completed_at", "status") {
		return nil, fmt.Errorf("invalid session completion boundary payload")
	}
	b := &SessionCompletionBoundary{
		SessionID:   terminalString(m["session_id"]),
		EventID:     terminalString(m["event_id"]),
		RequestID:   terminalString(m["request_id"]),
		Ordinal:     int(terminalFloat(m["ordinal"])),
		CompletedAt: terminalFloat(m["completed_at"]),
		Status:      terminalString(m["status"]),
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b, nil
}

// ArkRIDDistillationAdapter 共享版本化 stage invoker 之上的 RID API 代理提示词。
// 对齐 Python :161-171 ArkRIDDistillationAdapter。
type ArkRIDDistillationAdapter struct {
	invokeStage StageInvoker
}

// StageInvoker 后台阶段调用器（对齐 :158 StageInvoker = Callable[...]）。
type StageInvoker func(ctx any, stage, systemPrompt, payload string) (any, error)

// NewArkRIDDistillationAdapter 构造适配器。
func NewArkRIDDistillationAdapter(invokeStage StageInvoker) *ArkRIDDistillationAdapter {
	return &ArkRIDDistillationAdapter{invokeStage: invokeStage}
}

// Call 对齐 Python __call__（:166-171）：以固定阶段名/提示词调用。
func (a *ArkRIDDistillationAdapter) Call(ctx any, payload map[string]any) (any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return a.invokeStage(ctx, RID_STAGE_NAME, ridAPISystemPrompt, string(raw))
}

// LearnedComponentBinding 顶替一个学习型组件的版本化 API 提示词。
// 对齐 Python :173-211 LearnedComponentBinding。
type LearnedComponentBinding struct {
	// ComponentID 组件 ID（必须在 terminalComponentIDs 中）
	ComponentID string `json:"component_id"`
	// ProviderID 厂商 ID
	ProviderID string `json:"provider_id"`
	// ProviderVersion 厂商版本
	ProviderVersion string `json:"provider_version"`
	// ConfigVersion 配置版本
	ConfigVersion string `json:"config_version"`
	// ContractVersion 契约版本（默认 TERMINAL_CONTRACT_VERSION）
	ContractVersion string `json:"contract_version"`
}

// NewLearnedComponentBinding 构造并校验（对齐 :187-206 __post_init__）。
func NewLearnedComponentBinding(componentID, providerID, providerVersion, configVersion string) (*LearnedComponentBinding, error) {
	b := &LearnedComponentBinding{
		ComponentID: componentID, ProviderID: providerID,
		ProviderVersion: providerVersion, ConfigVersion: configVersion,
		ContractVersion: TERMINAL_CONTRACT_VERSION,
	}
	if !terminalComponentIDs[b.ComponentID] {
		return nil, fmt.Errorf("unsupported terminal component_id")
	}
	for _, v := range []string{b.ProviderID, b.ProviderVersion, b.ConfigVersion, b.ContractVersion} {
		if !terminalNonempty(v) {
			return nil, fmt.Errorf("component binding requires a versioned provider")
		}
	}
	return b, nil
}

// AsProvenance 血缘快照（对齐 :203-211 as_provenance）。
func (b *LearnedComponentBinding) AsProvenance() map[string]any {
	return map[string]any{
		"component_id":     b.ComponentID,
		"provider_id":      b.ProviderID,
		"provider_version": b.ProviderVersion,
		"config_version":   b.ConfigVersion,
		"contract_version": b.ContractVersion,
	}
}

// LearnedComponentReceipt 一次版本化学习型组件调用及其输出状态。
// 对齐 Python :213-268 LearnedComponentReceipt。
type LearnedComponentReceipt struct {
	// Binding 组件绑定
	Binding *LearnedComponentBinding `json:"binding"`
	// InputSnapshotID 输入快照 ID
	InputSnapshotID string `json:"input_snapshot_id"`
	// RequestID 请求 ID
	RequestID string `json:"request_id"`
	// ScoreSemantics 评分语义（success 必为 uncalibrated_api_proxy，否则 not_scored）
	ScoreSemantics string `json:"score_semantics"`
	// OutputStatus 输出状态
	OutputStatus string `json:"output_status"`
	// FallbackReason 降级原因（success 必空，非 success 必填）
	FallbackReason string `json:"fallback_reason"`
	// ReceiptVersion 小票版本
	ReceiptVersion string `json:"receipt_version"`
}

// NewLearnedComponentReceipt 构造并校验（对齐 :229-257 __post_init__）。
func NewLearnedComponentReceipt(binding *LearnedComponentBinding, inputSnapshotID, requestID, scoreSemantics, outputStatus, fallbackReason string) (*LearnedComponentReceipt, error) {
	r := &LearnedComponentReceipt{
		Binding: binding, InputSnapshotID: inputSnapshotID, RequestID: requestID,
		ScoreSemantics: scoreSemantics, OutputStatus: outputStatus,
		FallbackReason: fallbackReason, ReceiptVersion: COMPONENT_RECEIPT_SCHEMA_VERSION,
	}
	for _, v := range []string{r.InputSnapshotID, r.RequestID, r.ReceiptVersion} {
		if !terminalNonempty(v) {
			return nil, fmt.Errorf("receipt requires stable invocation identity")
		}
	}
	if !terminalScoreSemantics[r.ScoreSemantics] {
		return nil, fmt.Errorf("unsupported score_semantics")
	}
	if !terminalOutputStatuses[r.OutputStatus] {
		return nil, fmt.Errorf("unsupported output_status")
	}
	expectedSemantics := "not_scored"
	if r.OutputStatus == "success" {
		if r.FallbackReason != "" {
			return nil, fmt.Errorf("successful receipt forbids fallback_reason")
		}
		expectedSemantics = "uncalibrated_api_proxy"
	} else if !terminalNonempty(r.FallbackReason) {
		return nil, fmt.Errorf("non-success receipt requires fallback_reason")
	}
	if r.ScoreSemantics != expectedSemantics {
		return nil, fmt.Errorf("api_proxy score_semantics must be uncalibrated")
	}
	return r, nil
}

// AsPayload 序列化（对齐 :259-268 as_payload）。
func (r *LearnedComponentReceipt) AsPayload() map[string]any {
	return map[string]any{
		"receipt_version":   r.ReceiptVersion,
		"binding":           r.Binding.AsProvenance(),
		"input_snapshot_id": r.InputSnapshotID,
		"request_id":        r.RequestID,
		"score_semantics":   r.ScoreSemantics,
		"output_status":     r.OutputStatus,
		"fallback_reason":   r.FallbackReason,
	}
}

// SessionSourceLocator 一条被摘要的用户事件的主机侧精确源位置。
// 对齐 Python :270-305 SessionSourceLocator。
type SessionSourceLocator struct {
	SessionID string `json:"session_id"`
	Ordinal   int    `json:"ordinal"`
	EventID   string `json:"event_id"`
}

// NewSessionSourceLocator 构造并校验（对齐 :281-290 __post_init__）。
func NewSessionSourceLocator(sessionID string, ordinal int, eventID string) (*SessionSourceLocator, error) {
	l := &SessionSourceLocator{SessionID: sessionID, Ordinal: ordinal, EventID: eventID}
	if !terminalNonempty(l.SessionID) || l.Ordinal < 0 || !terminalNonempty(l.EventID) {
		return nil, fmt.Errorf("invalid session source locator")
	}
	return l, nil
}

// AsPayload 序列化（对齐 :292-300）。
func (l *SessionSourceLocator) AsPayload() map[string]any {
	return map[string]any{"session_id": l.SessionID, "ordinal": l.Ordinal, "event_id": l.EventID}
}

// SessionSourceLocatorFromPayload 反序列化（对齐 :302-305 from_payload）。
func SessionSourceLocatorFromPayload(payload any) (*SessionSourceLocator, error) {
	m, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid session source locator payload")
	}
	if !payloadKeySetEqual(m, "session_id", "ordinal", "event_id") {
		return nil, fmt.Errorf("invalid session source locator payload")
	}
	return NewSessionSourceLocator(terminalString(m["session_id"]), int(terminalFloat(m["ordinal"])), terminalString(m["event_id"]))
}

// SessionSummaryArtifact 带干预血缘的版本化完结会话摘要。
// 对齐 Python :307-459 SessionSummaryArtifact。
type SessionSummaryArtifact struct {
	SessionSummaryID       string                     `json:"session_summary_id"`
	SessionID              string                     `json:"session_id"`
	ContextScope           string                     `json:"context_scope"`
	SummaryText            string                     `json:"summary_text"`
	InputOrigin            string                     `json:"input_origin"`
	AssistanceOrigin       string                     `json:"assistance_origin"`
	SourceEventIDs         []string                   `json:"source_event_ids"`
	SourceLocators         []*SessionSourceLocator    `json:"source_locators"`
	CompletionBoundary     *SessionCompletionBoundary `json:"completion_boundary"`
	CompletedAt            float64                    `json:"completed_at"`
	SummaryProviderID      string                     `json:"summary_provider_id"`
	SummaryProviderVersion string                     `json:"summary_provider_version"`
	SummaryConfigVersion   string                     `json:"summary_config_version"`
	SchemaVersion          string                     `json:"schema_version"`
}

// NewSessionSummaryArtifact 构造并校验（对齐 :337-424 __post_init__，校验顺序逐段对应）。
func NewSessionSummaryArtifact(sessionSummaryID, sessionID, contextScope, summaryText, inputOrigin, assistanceOrigin string,
	sourceEventIDs []string, sourceLocators []*SessionSourceLocator, completionBoundary *SessionCompletionBoundary,
	completedAt float64, summaryProviderID, summaryProviderVersion, summaryConfigVersion string) (*SessionSummaryArtifact, error) {
	a := &SessionSummaryArtifact{
		SessionSummaryID: sessionSummaryID, SessionID: sessionID, ContextScope: contextScope,
		SummaryText: summaryText, InputOrigin: inputOrigin, AssistanceOrigin: assistanceOrigin,
		SourceEventIDs: sourceEventIDs, SourceLocators: sourceLocators,
		CompletionBoundary: completionBoundary, CompletedAt: completedAt,
		SummaryProviderID: summaryProviderID, SummaryProviderVersion: summaryProviderVersion,
		SummaryConfigVersion: summaryConfigVersion, SchemaVersion: SESSION_SUMMARY_SCHEMA_VERSION,
	}
	// 对齐 :339-350 身份与内容非空
	for _, v := range []string{a.SessionSummaryID, a.SessionID, a.SummaryText, a.InputOrigin, a.AssistanceOrigin, a.SummaryProviderID, a.SummaryProviderVersion, a.SummaryConfigVersion, a.SchemaVersion} {
		if !terminalNonempty(v) {
			return nil, fmt.Errorf("session summary requires stable identity and content")
		}
	}
	// 对齐 :351-356 scope
	if a.ContextScope != "global_new_chat" &&
		(!strings.HasPrefix(a.ContextScope, "project:") || strings.TrimSpace(strings.TrimPrefix(a.ContextScope, "project:")) == "") {
		return nil, fmt.Errorf("context_scope must be global_new_chat or project:<id>")
	}
	// 对齐 :357-363 source_event_ids 非空且唯一
	if len(a.SourceEventIDs) == 0 || hasDuplicates(a.SourceEventIDs) || anyEmpty(a.SourceEventIDs) {
		return nil, fmt.Errorf("source_event_ids must be non-empty and unique")
	}
	// 对齐 :364-386 locators 与事件一一对应且归属同一会话
	if len(a.SourceLocators) == 0 || len(a.SourceLocators) != len(a.SourceEventIDs) {
		return nil, fmt.Errorf("source locators must match unique source events")
	}
	eventIDs := make([]string, 0, len(a.SourceLocators))
	for _, locator := range a.SourceLocators {
		if locator.SessionID != a.SessionID {
			return nil, fmt.Errorf("source locators must match unique source events")
		}
		eventIDs = append(eventIDs, locator.EventID)
	}
	if hasDuplicates(eventIDs) {
		return nil, fmt.Errorf("source locators must match unique source events")
	}
	for i, eventID := range eventIDs {
		if eventID != a.SourceEventIDs[i] {
			return nil, fmt.Errorf("source locators must match unique source events")
		}
	}
	// 对齐 :387-404 完结边界闭合每个 locator
	if a.CompletionBoundary == nil ||
		a.CompletionBoundary.SessionID != a.SessionID ||
		a.CompletionBoundary.CompletedAt != a.CompletedAt {
		return nil, fmt.Errorf("completion boundary must close every source locator")
	}
	for _, locator := range a.SourceLocators {
		if locator.Ordinal >= a.CompletionBoundary.Ordinal {
			return nil, fmt.Errorf("completion boundary must close every source locator")
		}
	}
	// 对齐 :405-411 completed_at 有限非负
	if !isFiniteNonNegative(a.CompletedAt) {
		return nil, fmt.Errorf("completed_at must be a finite timestamp")
	}
	return a, nil
}

// NaturalRevisitEligible 自然回访资格（对齐 :426-430 natural_revisit_eligible）。
func (a *SessionSummaryArtifact) NaturalRevisitEligible() bool {
	return a.InputOrigin == "manual_free_text" && a.AssistanceOrigin == "none"
}

// AsPayload 序列化（对齐 :432-455 as_payload）。
func (a *SessionSummaryArtifact) AsPayload() map[string]any {
	locators := make([]map[string]any, 0, len(a.SourceLocators))
	for _, locator := range a.SourceLocators {
		locators = append(locators, locator.AsPayload())
	}
	eventIDs := append([]string(nil), a.SourceEventIDs...)
	return map[string]any{
		"session_summary_id":       a.SessionSummaryID,
		"session_id":               a.SessionID,
		"context_scope":            a.ContextScope,
		"summary_text":             a.SummaryText,
		"input_origin":             a.InputOrigin,
		"assistance_origin":        a.AssistanceOrigin,
		"source_event_ids":         eventIDs,
		"source_locators":          locators,
		"completion_boundary":      a.CompletionBoundary.AsPayload(),
		"completed_at":             a.CompletedAt,
		"summary_provider_id":      a.SummaryProviderID,
		"summary_provider_version": a.SummaryProviderVersion,
		"summary_config_version":   a.SummaryConfigVersion,
		"schema_version":           a.SchemaVersion,
		"natural_revisit_eligible": a.NaturalRevisitEligible(),
	}
}

// SessionSummaryArtifactFromPayload 反序列化（对齐 :457-503 from_payload，
// 15 键全等 + schema 版本 + eligibility 投影复核）。
func SessionSummaryArtifactFromPayload(payload any) (*SessionSummaryArtifact, error) {
	m, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("session summary payload contract failed")
	}
	if !payloadKeySetEqual(m,
		"session_summary_id", "session_id", "context_scope", "summary_text",
		"input_origin", "assistance_origin", "source_event_ids", "source_locators",
		"completion_boundary", "completed_at", "summary_provider_id",
		"summary_provider_version", "summary_config_version", "schema_version",
		"natural_revisit_eligible") {
		return nil, fmt.Errorf("session summary payload contract failed")
	}
	if terminalString(m["schema_version"]) != SESSION_SUMMARY_SCHEMA_VERSION {
		return nil, fmt.Errorf("session summary payload contract failed")
	}
	var eventIDs []string
	switch ids := m["source_event_ids"].(type) {
	case []any:
		eventIDs = make([]string, 0, len(ids))
		for _, id := range ids {
			eventIDs = append(eventIDs, terminalString(id))
		}
	case []string:
		eventIDs = ids
	default:
		return nil, fmt.Errorf("session summary payload contract failed")
	}
	var locators []*SessionSourceLocator
	switch raws := m["source_locators"].(type) {
	case []any:
		locators = make([]*SessionSourceLocator, 0, len(raws))
		for _, raw := range raws {
			locator, err := SessionSourceLocatorFromPayload(raw)
			if err != nil {
				return nil, err
			}
			locators = append(locators, locator)
		}
	case []*SessionSourceLocator:
		locators = raws
	case []map[string]any:
		locators = make([]*SessionSourceLocator, 0, len(raws))
		for _, raw := range raws {
			locator, err := SessionSourceLocatorFromPayload(raw)
			if err != nil {
				return nil, err
			}
			locators = append(locators, locator)
		}
	default:
		return nil, fmt.Errorf("session summary payload contract failed")
	}
	boundary, err := BoundaryFromPayload(m["completion_boundary"])
	if err != nil {
		return nil, err
	}
	artifact, err := NewSessionSummaryArtifact(
		terminalString(m["session_summary_id"]), terminalString(m["session_id"]),
		terminalString(m["context_scope"]), terminalString(m["summary_text"]),
		terminalString(m["input_origin"]), terminalString(m["assistance_origin"]),
		eventIDs, locators, boundary, terminalFloat(m["completed_at"]),
		terminalString(m["summary_provider_id"]), terminalString(m["summary_provider_version"]),
		terminalString(m["summary_config_version"]),
	)
	if err != nil {
		return nil, err
	}
	// 对齐 :499-502 eligibility 投影复核
	eligible, _ := m["natural_revisit_eligible"].(bool)
	if eligible != artifact.NaturalRevisitEligible() {
		return nil, fmt.Errorf("session summary eligibility projection mismatch")
	}
	return artifact, nil
}

// LongTermProfileInterestInput 提供给 RID 的画像数据的无 locator RecGPT-v3 投影。
// 对齐 Python :505-587 LongTermProfileInterestInput。
type LongTermProfileInterestInput struct {
	InterestSummaryID      string   `json:"interest_summary_id"`
	ContextScope           string   `json:"context_scope"`
	ProfileSemanticVersion int      `json:"profile_semantic_version"`
	InterestCluster        string   `json:"interest_cluster"`
	SummaryText            string   `json:"summary_text"`
	SourceProfileUnitIDs   []string `json:"source_profile_unit_ids"`
	SchemaVersion          string   `json:"schema_version"`
}

// NewLongTermProfileInterestInput 构造并校验（对齐 :522-553 __post_init__）。
func NewLongTermProfileInterestInput(interestSummaryID, contextScope string, profileSemanticVersion int, interestCluster, summaryText string, sourceProfileUnitIDs []string) (*LongTermProfileInterestInput, error) {
	i := &LongTermProfileInterestInput{
		InterestSummaryID: interestSummaryID, ContextScope: contextScope,
		ProfileSemanticVersion: profileSemanticVersion, InterestCluster: interestCluster,
		SummaryText: summaryText, SourceProfileUnitIDs: sourceProfileUnitIDs,
		SchemaVersion: LONG_TERM_PROFILE_INTEREST_SCHEMA_VERSION,
	}
	for _, v := range []string{i.InterestSummaryID, i.InterestCluster, i.SummaryText, i.SchemaVersion} {
		if !terminalNonempty(v) {
			return nil, fmt.Errorf("long-term profile interest requires identity and content")
		}
	}
	if i.ContextScope != "global_new_chat" {
		return nil, fmt.Errorf("long-term profile interest is global_new_chat only")
	}
	if i.ProfileSemanticVersion < 1 {
		return nil, fmt.Errorf("long-term profile interest requires semantic version")
	}
	if len(i.SourceProfileUnitIDs) == 0 || hasDuplicates(i.SourceProfileUnitIDs) || anyEmpty(i.SourceProfileUnitIDs) {
		return nil, fmt.Errorf("long-term profile interest requires source profile units")
	}
	return i, nil
}

// AsPayload 序列化（对齐 :555-570 as_payload）。
func (i *LongTermProfileInterestInput) AsPayload() map[string]any {
	unitIDs := append([]string(nil), i.SourceProfileUnitIDs...)
	return map[string]any{
		"interest_summary_id":      i.InterestSummaryID,
		"context_scope":            i.ContextScope,
		"profile_semantic_version": i.ProfileSemanticVersion,
		"interest_cluster":         i.InterestCluster,
		"summary_text":             i.SummaryText,
		"source_profile_unit_ids":  unitIDs,
		"schema_version":           i.SchemaVersion,
		"source_kind":              "long_term_profile",
	}
}

// EligibleNaturalRevisitSummaries 返回请求服务 scope 内自然无辅助的摘要。
// 对齐 Python :589-597 eligible_natural_revisit_summaries。
func EligibleNaturalRevisitSummaries(summaries []*SessionSummaryArtifact, contextScope string) []*SessionSummaryArtifact {
	var out []*SessionSummaryArtifact
	for _, summary := range summaries {
		if summary.ContextScope == contextScope && summary.NaturalRevisitEligible() {
			out = append(out, summary)
		}
	}
	return out
}

// RIDInterestDecision 一条带精确源血缘的 RID 选中/拒绝决策。
// 对齐 Python :599-653 RIDInterestDecision。
type RIDInterestDecision struct {
	InterestSummaryID        string                  `json:"interest_summary_id"`
	SourceKind               string                  `json:"source_kind"`
	SourceSessionSummaryIDs  []string                `json:"source_session_summary_ids"`
	SourceProfileInterestIDs []string                `json:"source_profile_interest_ids"`
	SourceProfileUnitIDs     []string                `json:"source_profile_unit_ids"`
	SourceEventIDs           []string                `json:"source_event_ids"`
	SourceLocators           []*SessionSourceLocator `json:"source_locators"`
	Selected                 bool                    `json:"selected"`
	Rank                     int                     `json:"rank"`
	HasRank                  bool                    `json:"-"`
	ResonanceScoreNullable   float64                 `json:"resonance_score_nullable"`
	HasScore                 bool                    `json:"-"`
	ScoreSemantics           string                  `json:"score_semantics"`
}

// AsPayload 序列化（对齐 :628-653 as_payload）。
func (d *RIDInterestDecision) AsPayload() map[string]any {
	locators := make([]map[string]any, 0, len(d.SourceLocators))
	for _, locator := range d.SourceLocators {
		locators = append(locators, locator.AsPayload())
	}
	var rank any
	if d.HasRank {
		rank = d.Rank
	}
	var score any
	if d.HasScore {
		score = d.ResonanceScoreNullable
	}
	return map[string]any{
		"interest_summary_id":         d.InterestSummaryID,
		"source_kind":                 d.SourceKind,
		"source_session_summary_ids":  append([]string(nil), d.SourceSessionSummaryIDs...),
		"source_profile_interest_ids": append([]string(nil), d.SourceProfileInterestIDs...),
		"source_profile_unit_ids":     append([]string(nil), d.SourceProfileUnitIDs...),
		"source_event_ids":            append([]string(nil), d.SourceEventIDs...),
		"source_locators":             locators,
		"selected":                    d.Selected,
		"rank":                        rank,
		"resonance_score_nullable":    score,
		"score_semantics":             d.ScoreSemantics,
	}
}

// RIDResult 被检索与生成阶段消费的已校验 RID 结果。
// 对齐 Python :655-701 RIDResult。
type RIDResult struct {
	ContextScope     string                   `json:"context_scope"`
	ActivityGroup    string                   `json:"activity_group"`
	ThresholdVersion string                   `json:"threshold_version"`
	Receipt          *LearnedComponentReceipt `json:"receipt"`
	Decisions        []*RIDInterestDecision   `json:"decisions"`
	RIDResultVersion string                   `json:"rid_result_version"`
}

// TriggerInterests 按 rank 升序的选中项（对齐 :663-669 trigger_interests）。
func (r *RIDResult) TriggerInterests() []*RIDInterestDecision {
	var selected []*RIDInterestDecision
	for _, item := range r.Decisions {
		if item.Selected {
			selected = append(selected, item)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].Rank < selected[j].Rank
	})
	return selected
}

// RejectedInterests 未选中项（对齐 :671-673 rejected_interests）。
func (r *RIDResult) RejectedInterests() []*RIDInterestDecision {
	var rejected []*RIDInterestDecision
	for _, item := range r.Decisions {
		if !item.Selected {
			rejected = append(rejected, item)
		}
	}
	return rejected
}

// AsDownstreamPayload 下游消费形态（对齐 :675-693 as_downstream_payload）。
func (r *RIDResult) AsDownstreamPayload() map[string]any {
	triggers := r.TriggerInterests()
	triggerPayloads := make([]map[string]any, 0, len(triggers))
	for _, item := range triggers {
		triggerPayloads = append(triggerPayloads, item.AsPayload())
	}
	rejected := r.RejectedInterests()
	rejectedPayloads := make([]map[string]any, 0, len(rejected))
	for _, item := range rejected {
		rejectedPayloads = append(rejectedPayloads, item.AsPayload())
	}
	return map[string]any{
		"rid_result_version": r.RIDResultVersion,
		"context_scope":      r.ContextScope,
		"activity_group":     r.ActivityGroup,
		"threshold_version":  r.ThresholdVersion,
		"component_receipt":  r.Receipt.AsPayload(),
		"trigger_interests":  triggerPayloads,
		"rejected_interests": rejectedPayloads,
	}
}

// RIDDistiller RID 蒸馏调用器（对齐 :695 RIDDistiller）。
type RIDDistiller func(ctx any, payload map[string]any) (any, error)

// DistillTriggerInterestsInput 蒸馏入参集合（Python 关键字参数形态）。
type DistillTriggerInterestsInput struct {
	UserProfile              map[string]any
	LongTermProfileInterests []*LongTermProfileInterestInput
	SessionSummaries         []*SessionSummaryArtifact
	InputSnapshotID          string
	RequestID                string
	ActivityGroup            string
	ThresholdVersion         string
	Delta                    []map[string]any
	FeedbackContext          map[string]any
	Binding                  *LearnedComponentBinding
	InvokeDistiller          RIDDistiller
	DecisionContext          map[string]any
}

// DistillTriggerInterests 运行 RID 提示词并校验完整决策集。
// 对齐 Python :697-904 distill_trigger_interests（校验顺序逐段对应，
// 失败返回 (nil, 原因码)，原因码逐字照抄）。
func DistillTriggerInterests(ctx any, in DistillTriggerInterestsInput) (*RIDResult, string) {
	profileInterests := in.LongTermProfileInterests
	summaries := in.SessionSummaries
	// 对齐 :711-714
	if in.Binding == nil || in.Binding.ComponentID != RID_COMPONENT_ID {
		return nil, "rid_input_contract_failed"
	}
	if len(profileInterests) == 0 && len(summaries) == 0 {
		return nil, "rid_input_contract_failed"
	}
	// Go 静态类型已保证元素类型（对齐 :715-723 的 isinstance 检查无需运行时判断）
	if !terminalNonempty(in.InputSnapshotID) || !terminalNonempty(in.RequestID) ||
		!terminalNonempty(in.ActivityGroup) || !terminalNonempty(in.ThresholdVersion) {
		return nil, "rid_input_contract_failed"
	}
	// 对齐 :724-727 ID 唯一性
	profileInterestsByID := make(map[string]*LongTermProfileInterestInput, len(profileInterests))
	for _, interest := range profileInterests {
		profileInterestsByID[interest.InterestSummaryID] = interest
	}
	summariesByID := make(map[string]*SessionSummaryArtifact, len(summaries))
	for _, summary := range summaries {
		summariesByID[summary.SessionSummaryID] = summary
	}
	if len(profileInterestsByID) != len(profileInterests) || len(summariesByID) != len(summaries) {
		return nil, "rid_input_contract_failed"
	}
	// 对齐 :728-729 两类 ID 不相交
	for id := range profileInterestsByID {
		if _, clash := summariesByID[id]; clash {
			return nil, "rid_input_contract_failed"
		}
	}
	// 对齐 :730-737 scope 单一且摘要全部自然回访合格
	scopes := map[string]bool{}
	for _, interest := range profileInterests {
		scopes[interest.ContextScope] = true
	}
	for _, summary := range summaries {
		scopes[summary.ContextScope] = true
		if !summary.NaturalRevisitEligible() {
			return nil, "rid_input_contract_failed"
		}
	}
	if len(scopes) != 1 {
		return nil, "rid_input_contract_failed"
	}

	// input_by_id：id → (source_kind, 源对象)
	type sourceEntry struct {
		kind    string
		profile *LongTermProfileInterestInput
		summary *SessionSummaryArtifact
	}
	inputByID := make(map[string]sourceEntry)
	for _, interest := range profileInterests {
		inputByID[interest.InterestSummaryID] = sourceEntry{kind: "long_term_profile", profile: interest}
	}
	for _, summary := range summaries {
		inputByID[summary.SessionSummaryID] = sourceEntry{kind: "session_summary", summary: summary}
	}

	// 对齐 :745-759 每次调用变化的内容放最后，连续请求共享最长缓存前缀
	requestPayload := map[string]any{
		"binding":                     in.Binding.AsProvenance(),
		"input_snapshot_id":           in.InputSnapshotID,
		"user_profile":                in.UserProfile,
		"long_term_profile_interests": payloadList(func(i int) map[string]any { return profileInterests[i].AsPayload() }, len(profileInterests)),
		"session_summaries":           payloadList(func(i int) map[string]any { return summaries[i].AsPayload() }, len(summaries)),
		"activity_group":              in.ActivityGroup,
		"threshold_version":           in.ThresholdVersion,
		"feedback_context":            in.FeedbackContext,
		"prompt_version":              RID_API_PROMPT_VERSION,
		"delta":                       deltaPayload(in.Delta),
	}
	if in.DecisionContext != nil {
		requestPayload["decision_context"] = in.DecisionContext
	}
	requestPayload["request_id"] = in.RequestID
	// 对齐 :760-763 provider 失败是关闭性结果
	response, err := in.InvokeDistiller(ctx, requestPayload)
	if err != nil {
		return nil, "rid_distillation_failed"
	}
	// 对齐 :765-770 输出契约
	resp, ok := response.(map[string]any)
	if !ok {
		return nil, "rid_output_contract_failed"
	}
	if !payloadKeySetEqual(resp, "decisions") {
		return nil, "rid_output_contract_failed"
	}
	decisionsRaw, ok := resp["decisions"].([]any)
	if !ok {
		return nil, "rid_output_contract_failed"
	}
	scoreSemantics := "uncalibrated_api_proxy"
	decisionsByID := make(map[string]*RIDInterestDecision)
	seenIDs := make(map[string]bool)
	var selectedRanks []int
	for _, rawDecision := range decisionsRaw {
		raw, ok := rawDecision.(map[string]any)
		if !ok || !payloadKeySetEqual(raw, "interest_summary_id", "selected", "rank", "resonance_score_nullable") {
			return nil, "rid_output_contract_failed"
		}
		summaryID := terminalString(raw["interest_summary_id"])
		selected, _ := raw["selected"].(bool)
		rank := raw["rank"]
		scoreValid, score := finiteNullable(raw["resonance_score_nullable"])
		if summaryID == "" {
			return nil, "rid_output_contract_failed"
		}
		source, known := inputByID[summaryID]
		if !known {
			return nil, "rid_output_contract_failed"
		}
		if seenIDs[summaryID] {
			return nil, "rid_output_contract_failed"
		}
		if _, isBool := raw["selected"].(bool); !isBool {
			return nil, "rid_output_contract_failed"
		}
		if !scoreValid {
			return nil, "rid_output_contract_failed"
		}
		if score != nil {
			// 本通道 resonance 必须为 null（对齐 :802-804）
			return nil, "rid_output_contract_failed"
		}
		decision := &RIDInterestDecision{
			InterestSummaryID: summaryID,
			Selected:          selected,
			ScoreSemantics:    scoreSemantics,
		}
		if source.summary != nil {
			decision.SourceKind = "session_summary"
			decision.SourceSessionSummaryIDs = []string{source.summary.SessionSummaryID}
			decision.SourceProfileInterestIDs = []string{}
			decision.SourceProfileUnitIDs = []string{}
			decision.SourceEventIDs = append([]string(nil), source.summary.SourceEventIDs...)
			decision.SourceLocators = source.summary.SourceLocators
		} else {
			decision.SourceKind = "long_term_profile"
			decision.SourceSessionSummaryIDs = []string{}
			decision.SourceProfileInterestIDs = []string{source.profile.InterestSummaryID}
			decision.SourceProfileUnitIDs = append([]string(nil), source.profile.SourceProfileUnitIDs...)
			decision.SourceEventIDs = []string{}
			decision.SourceLocators = nil
		}
		if selected {
			rankInt, ok := auditPlainInt(rank)
			// 对齐 :805-808 选中必须有 ≥1 的整数 rank
			if !ok || rankInt < 1 {
				return nil, "rid_output_contract_failed"
			}
			decision.HasRank = true
			decision.Rank = rankInt
			selectedRanks = append(selectedRanks, rankInt)
		} else if rank != nil {
			// 对齐 :809-810 未选中 rank 必须为 null
			return nil, "rid_output_contract_failed"
		}
		seenIDs[summaryID] = true
		decisionsByID[summaryID] = decision
	}
	// 对齐 :839-840 选中 rank 必须是 1..n 连续
	sort.Ints(selectedRanks)
	for i, rank := range selectedRanks {
		if rank != i+1 {
			return nil, "rid_output_contract_failed"
		}
	}
	// 对齐 :842-866 未出现在响应中的输入一律视为拒绝（host 记为 rejected）
	decisions := make([]*RIDInterestDecision, 0, len(inputByID))
	for interestID, source := range inputByID {
		if decision, ok := decisionsByID[interestID]; ok {
			decisions = append(decisions, decision)
			continue
		}
		decision := &RIDInterestDecision{
			InterestSummaryID: interestID,
			Selected:          false,
			ScoreSemantics:    scoreSemantics,
		}
		if source.kind == "session_summary" {
			decision.SourceKind = "session_summary"
			decision.SourceSessionSummaryIDs = []string{interestID}
			decision.SourceProfileInterestIDs = []string{}
			decision.SourceProfileUnitIDs = []string{}
			decision.SourceEventIDs = append([]string(nil), source.summary.SourceEventIDs...)
			decision.SourceLocators = source.summary.SourceLocators
		} else {
			decision.SourceKind = "long_term_profile"
			decision.SourceSessionSummaryIDs = []string{}
			decision.SourceProfileInterestIDs = []string{interestID}
			decision.SourceProfileUnitIDs = append([]string(nil), source.profile.SourceProfileUnitIDs...)
			decision.SourceEventIDs = []string{}
			decision.SourceLocators = nil
		}
		decisions = append(decisions, decision)
	}
	// 对齐 :868-886 RIDResult 装配（scope 取输入首个条目）
	var firstScope string
	for _, entry := range inputByID {
		if entry.summary != nil {
			firstScope = entry.summary.ContextScope
		} else {
			firstScope = entry.profile.ContextScope
		}
		break
	}
	receipt, err := NewLearnedComponentReceipt(in.Binding, in.InputSnapshotID, in.RequestID, scoreSemantics, "success", "")
	if err != nil {
		return nil, "rid_output_contract_failed"
	}
	return &RIDResult{
		ContextScope:     firstScope,
		ActivityGroup:    in.ActivityGroup,
		ThresholdVersion: in.ThresholdVersion,
		Receipt:          receipt,
		Decisions:        decisions,
		RIDResultVersion: RID_RESULT_SCHEMA_VERSION,
	}, ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// terminalNonempty 非空白字符串（对齐 :138-140 _is_nonempty）。
func terminalNonempty(value string) bool {
	return strings.TrimSpace(value) != ""
}

// isFiniteNonNegative 有限非负数值（对齐 :405-411 等处的时间戳校验）。
func isFiniteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

// hasDuplicates 重复检测。
func hasDuplicates(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}

// anyEmpty 存在空白项。
func anyEmpty(values []string) bool {
	for _, v := range values {
		if !terminalNonempty(v) {
			return true
		}
	}
	return false
}

// payloadKeySetEqual 键集合全等（对齐 Python set(payload) == {...}）。
func payloadKeySetEqual(m map[string]any, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// terminalString 安全取字符串。
func terminalString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// terminalFloat 数值转 float64（bool 不是数值）。
func terminalFloat(v any) float64 {
	if b, ok := v.(bool); ok {
		_ = b
		return 0
	}
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

// finiteNullable 有限可空数值（对齐 :691-697 _finite_nullable）。
func finiteNullable(value any) (bool, any) {
	if value == nil {
		return true, nil
	}
	switch n := value.(type) {
	case int:
		return true, float64(n)
	case int64:
		return true, float64(n)
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return false, nil
		}
		return true, n
	}
	return false, nil
}

// payloadList 按序构造 payload 列表。
func payloadList(fn func(int) map[string]any, n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fn(i))
	}
	return out
}

// deltaPayload delta 列表直传（保持条目键序由 json 序列化决定）。
func deltaPayload(delta []map[string]any) []map[string]any {
	if delta == nil {
		return []map[string]any{}
	}
	return delta
}
