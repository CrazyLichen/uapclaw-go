package recommendation

// topic_pipeline_c5.go 一比一对齐上游 topic_pipeline.py:3409-3783 的
// 摘要批构造助手，以及 session_history.py:145-243 的完结前缀投影
// （project_completed_session_prefix，⤴️31d 归位会话域）。

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 常量 ────────────────────────────

// SummaryInputPolicyVersion 摘要输入策略版本（topic_pipeline.py:242）
const SummaryInputPolicyVersion = "topic-starter-v6-session-summary-input-v1"

// SummaryMaxTurnsPerSession 每会话摘要轮数上限（:243）
const SummaryMaxTurnsPerSession = 20

// SummaryMaxUserChars 摘要用户正文截断长度（:244）
const SummaryMaxUserChars = 1500

// SummaryMaxAnswerChars 摘要助手回答截断长度（:245）
const SummaryMaxAnswerChars = 1500

// SummaryMaxSessionChars 每会话摘要总字符预算（:246）
const SummaryMaxSessionChars = 8000

// SummaryTruncationMarker 截断标记（:247）
const SummaryTruncationMarker = "…"

// SummaryReservedOutputTokens 每会话预留输出 token（:248）
const SummaryReservedOutputTokens = 256

// SummaryBaseReservedOutputTokens 批基础预留输出 token（:249）
const SummaryBaseReservedOutputTokens = 512

// SummarySessionWindowSeconds 近期会话窗口（:256，90 天）
const SummarySessionWindowSeconds = 90 * 24 * 60 * 60

// SummarySessionLimit 近期会话摘要上限（:257）
const SummarySessionLimit = 50

// RIDSessionSummaryLimit RID 会话摘要上限（:259）
const RIDSessionSummaryLimit = 30

// SessionRequestCompletedEventType 会话请求完结事件（common/schema/message.py
// 契约，字面值实测上游测试断言 "chat.request_completed"）
const SessionRequestCompletedEventType = "chat.request_completed"

// ──────────────────────────── 结构体 ────────────────────────────

// SummaryTurn 一条摘要候选轮（payload, content, answer 三元组
// 对齐 _select_summary_turns 的入参形状；answer 为 nil 表示无回答）。
type SummaryTurn struct {
	// Message 来源消息
	Message *V6SourceMessage
	// Content 截断后的正文
	Content string
	// Answer 截断后的回答（可空）
	Answer any
}

// PipelineStageTokenCounter 阶段 token 预算的估算缝（⤴️31d：topic_history.py
// :553 StageTokenCounter 真实类型落地后删除本接口，规则 8 锚点 0 命中验收）。
type PipelineStageTokenCounter interface {
	// EstimateStageTokens 估算 framed 系统与用户载荷加预留输出的 token 数
	EstimateStageTokens(systemPrompt, userPayload string, reservedOutput int) int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SessionRequestTerminalStatusSet 请求终态白名单
// （session_history.py:40-47）。
func SessionRequestTerminalStatusSet() map[string]bool {
	return map[string]bool{
		"success": true, "error": true, "cancelled": true,
		"failed": true, "waiting_user": true, "unknown": true,
	}
}

// IsTerminalCompletionRow 完结行只在它是组内最后一条且形状完整时闭包请求
// （session_history.py:145-165 _is_terminal_completion_row）。
func IsTerminalCompletionRow(requestID string, completionOrdinal, lastOrdinal int,
	completion map[string]any, eventID, status string, completedAt any) bool {
	completed, isNumeric := toFloat(completedAt)
	return completionOrdinal == lastOrdinal &&
		completion["role"] == "assistant" &&
		completion["feedback_only"] == true &&
		eventID == requestID+":request_completed" &&
		SessionRequestTerminalStatusSet()[status] &&
		isNumeric && completed >= 0
}

// ProjectCompletedSessionPrefix 精确的已完结请求前缀，不推断静默
// （session_history.py:167-243）。请求只在其组以合法完结行结尾时完结；
// 重复完结由写序最后一条裁决；无 gaps 时首个未闭组截断前缀。
func ProjectCompletedSessionPrefix(sessionID string, records []map[string]any,
	allowGaps bool) *CompletedSessionProjection {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil
	}
	type requestGroup struct {
		lastOrdinal    int
		userOrdinals   []int
		completionRows []struct {
			ordinal int
			record  map[string]any
		}
	}
	groups := map[string]*requestGroup{}
	// Python dict 保持插入序：按首次出现顺序记 key。
	order := []string{}
	for ordinal, record := range records {
		if record == nil {
			continue
		}
		requestID := strings.TrimSpace(pyStr(record["request_id"]))
		role := strings.ToLower(strings.TrimSpace(pyStr(record["role"])))
		if requestID == "" {
			if role != "user" && role != "human" {
				continue
			}
			requestID = fmt.Sprintf("__missing_request_id__:%d", ordinal)
		}
		group := groups[requestID]
		if group == nil {
			group = &requestGroup{}
			groups[requestID] = group
			order = append(order, requestID)
		}
		group.lastOrdinal = ordinal
		if (role == "user" || role == "human") && !pythonTruthy(record["is_goal_objective_message"]) {
			group.userOrdinals = append(group.userOrdinals, ordinal)
		}
		if pyStr(record["event_type"]) == SessionRequestCompletedEventType {
			group.completionRows = append(group.completionRows, struct {
				ordinal int
				record  map[string]any
			}{ordinal, record})
		}
	}
	var completedRequestIDs []string
	var completedUserOrdinals []int
	var boundaries []*SessionCompletionBoundary
	for _, requestID := range order {
		group := groups[requestID]
		if len(group.userOrdinals) == 0 {
			continue
		}
		if len(group.completionRows) == 0 {
			if allowGaps {
				continue
			}
			break
		}
		last := group.completionRows[len(group.completionRows)-1]
		status := strings.ToLower(strings.TrimSpace(pyStr(last.record["status"])))
		eventID := strings.TrimSpace(pyStr(last.record["id"]))
		completedAt := last.record["timestamp"]
		closes := IsTerminalCompletionRow(requestID, last.ordinal, group.lastOrdinal,
			last.record, eventID, status, completedAt)
		if !closes {
			if allowGaps {
				continue
			}
			break
		}
		completedRequestIDs = append(completedRequestIDs, requestID)
		completedUserOrdinals = append(completedUserOrdinals, group.userOrdinals...)
		boundary, err := NewSessionCompletionBoundary(sid, eventID, requestID,
			last.ordinal, toFloatOr(completedAt, 0), status)
		if err != nil {
			continue
		}
		boundaries = append(boundaries, boundary)
	}
	if len(boundaries) == 0 {
		return nil
	}
	boundary := boundaries[0]
	for _, candidate := range boundaries[1:] {
		if candidate.Ordinal > boundary.Ordinal {
			boundary = candidate
		}
	}
	return &CompletedSessionProjection{
		Boundary:              boundary,
		CompletedRequestIDs:   completedRequestIDs,
		CompletedUserOrdinals: completedUserOrdinals,
	}
}

// SummaryOrigins 摘要的输入/协助来源（topic_pipeline.py:3409-3419）：
// 唯一取值或 "mixed"。
func SummaryOrigins(messages []*V6SourceMessage) (string, string) {
	inputOrigins := []string{}
	seenInput := map[string]bool{}
	assistanceOrigins := []string{}
	seenAssistance := map[string]bool{}
	for _, item := range messages {
		if !seenInput[item.InputOrigin] {
			seenInput[item.InputOrigin] = true
			inputOrigins = append(inputOrigins, item.InputOrigin)
		}
		if !seenAssistance[item.AssistanceOrigin] {
			seenAssistance[item.AssistanceOrigin] = true
			assistanceOrigins = append(assistanceOrigins, item.AssistanceOrigin)
		}
	}
	inputOrigin := "mixed"
	if len(inputOrigins) == 1 {
		inputOrigin = inputOrigins[0]
	}
	assistanceOrigin := "mixed"
	if len(assistanceOrigins) == 1 {
		assistanceOrigin = assistanceOrigins[0]
	}
	return inputOrigin, assistanceOrigin
}

// SummaryAbstractContext 摘要的抽象上下文簇（topic_pipeline.py:3422-3432）。
func SummaryAbstractContext(messages []*V6SourceMessage) []string {
	clusters := []string{}
	seen := map[string]bool{}
	for _, item := range messages {
		if item.IntentCluster != "" && item.IntentCluster != "unmapped" && !seen[item.IntentCluster] {
			seen[item.IntentCluster] = true
			clusters = append(clusters, item.IntentCluster)
		}
	}
	if len(clusters) == 0 {
		return []string{"general_interest"}
	}
	return clusters
}

// SelectSummaryTurns 预算内的最新轮，保持原顺序（topic_pipeline.py:3435-3459）。
// 输入按最旧在前；第一条保留轮即使单独超预算也保留；后续轮在轮数或预算处
// 停止。文本按模型请求的同一方式截断。
func SelectSummaryTurns(turns []SummaryTurn) []SummaryTurn {
	var kept []SummaryTurn
	used := 0
	for index := len(turns) - 1; index >= 0; index-- {
		turn := turns[index]
		clippedContent := pyStrOrNilToEmpty(ClipSummaryText(turn.Content, SummaryMaxUserChars))
		clippedAnswer := ClipSummaryTextAny(turn.Answer, SummaryMaxAnswerChars)
		cost := utf8.RuneCountInString(clippedContent) + utf8.RuneCountInString(pyStrOrNilToEmpty(clippedAnswer))
		if len(kept) > 0 &&
			(len(kept) >= SummaryMaxTurnsPerSession || used+cost > SummaryMaxSessionChars) {
			break
		}
		kept = append(kept, SummaryTurn{Message: turn.Message, Content: clippedContent, Answer: clippedAnswer})
		used += cost
	}
	// 反转回原顺序
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept
}

// SummaryCoverageIDs 摘要需要负责的轮（topic_pipeline.py:3462-3469）。
func SummaryCoverageIDs(messages []*V6SourceMessage) map[string]bool {
	ordered := append([]*V6SourceMessage(nil), messages...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	turns := make([]SummaryTurn, 0, len(ordered))
	for _, message := range ordered {
		turns = append(turns, SummaryTurn{Message: message, Content: message.Content, Answer: message.AssistantAnswer})
	}
	selected := SelectSummaryTurns(turns)
	covered := map[string]bool{}
	for _, turn := range selected {
		covered[turn.Message.EventID] = true
	}
	return covered
}

// SessionIdentityPayload 摘要覆盖的有界轮（topic_pipeline.py:3472-3502）。
// 窗口内新轮会改变 digest。
func SessionIdentityPayload(sessionID string, messages []*V6SourceMessage) map[string]any {
	ordered := append([]*V6SourceMessage(nil), messages...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	turns := make([]SummaryTurn, 0, len(ordered))
	for _, message := range ordered {
		turns = append(turns, SummaryTurn{Message: message, Content: message.Content, Answer: message.AssistantAnswer})
	}
	selected := SelectSummaryTurns(turns)
	covered := make([]*V6SourceMessage, 0, len(selected))
	for _, turn := range selected {
		covered = append(covered, turn.Message)
	}
	inputOrigin, assistanceOrigin := SummaryOrigins(messages)
	abstractContext := SummaryAbstractContext(covered)
	interactionPattern := make([]any, 0, len(covered))
	for _, message := range covered {
		interactionPattern = append(interactionPattern, message.ActionType)
	}
	turnPayloads := make([]map[string]any, 0, len(selected))
	for _, turn := range selected {
		turnPayloads = append(turnPayloads, turnPayload(turn))
	}
	return map[string]any{
		"session_id":          sessionID,
		"abstract_context":    abstractContext,
		"interaction_pattern": interactionPattern,
		"input_origin":        inputOrigin,
		"assistance_origin":   assistanceOrigin,
		"messages":            turnPayloads,
	}
}

// ClipSummaryText 按字符数截断并加省略标记（topic_pipeline.py:3505-3511）。
func ClipSummaryText(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	cut := limit - utf8.RuneCountInString(SummaryTruncationMarker)
	if cut < 0 {
		cut = 0
	}
	return string(runes[:cut]) + SummaryTruncationMarker
}

// ClipSummaryTextAny 空值透传的截断（None → None）。
func ClipSummaryTextAny(value any, limit int) any {
	if value == nil {
		return nil
	}
	return ClipSummaryText(pyStr(value), limit)
}

// BoundedSummaryTurns 摘要输入与其负责的轮（topic_pipeline.py:3514-3522）。
func BoundedSummaryTurns(messages []*V6SourceMessage) []SummaryTurn {
	ordered := append([]*V6SourceMessage(nil), messages...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	turns := make([]SummaryTurn, 0, len(ordered))
	for _, message := range ordered {
		turns = append(turns, SummaryTurn{Message: message, Content: message.Content, Answer: message.AssistantAnswer})
	}
	return SelectSummaryTurns(turns)
}

// SessionModelPayload 摘要器实际读到的最新轮（topic_pipeline.py:3525-3559）。
func SessionModelPayload(sessionID string, messages []*V6SourceMessage) map[string]any {
	inputOrigin, assistanceOrigin := SummaryOrigins(messages)
	bounded := BoundedSummaryTurns(messages)
	boundedMessages := make([]*V6SourceMessage, 0, len(bounded))
	for _, turn := range bounded {
		boundedMessages = append(boundedMessages, turn.Message)
	}
	interactionPattern := make([]any, 0, len(bounded))
	for _, turn := range bounded {
		interactionPattern = append(interactionPattern, turn.Message.ActionType)
	}
	turnPayloads := make([]map[string]any, 0, len(bounded))
	for _, turn := range bounded {
		turnPayloads = append(turnPayloads, turnPayload(turn))
	}
	return map[string]any{
		"session_id": sessionID,
		// 计数告诉审计该会话共有多少轮、摘要取自多少轮。
		"source_turn_count":   len(messages),
		"supplied_turn_count": len(bounded),
		"abstract_context":    SummaryAbstractContext(boundedMessages),
		"interaction_pattern": interactionPattern,
		"input_origin":        inputOrigin,
		"assistance_origin":   assistanceOrigin,
		"messages":            turnPayloads,
	}
}

// SummaryRequestPayload 摘要请求载荷串（topic_pipeline.py:3562-3578）。
func SummaryRequestPayload(inputSnapshotID, contextScope string,
	sessions []map[string]any, requestID string) string {
	if requestID == "" {
		requestID = "v6-session-summary-" + strings.Repeat("0", 32)
	}
	payload := map[string]any{
		"input_snapshot_id":    inputSnapshotID,
		"request_id":           requestID,
		"context_scope":        contextScope,
		"summary_input_policy": SummaryInputPolicyVersion,
		"sessions":             sessions,
	}
	encoded, err := utils.MarshalNoEscape(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// SummaryBatchEstimate 摘要批 token 估算（topic_pipeline.py:3581-3596）。
func SummaryBatchEstimate(counter PipelineStageTokenCounter, snapshot *V6ContextSnapshot,
	sessions []map[string]any, reservedOutput int) int {
	return counter.EstimateStageTokens(pipelineSessionSummaryPrompt,
		SummaryRequestPayload(snapshot.InputSnapshotID, snapshot.ContextScope, sessions, ""),
		reservedOutput)
}

// FitSummarySession 丢弃最旧轮直到单个会话装进上下文窗口
// （topic_pipeline.py:3599-3639）。单轮仍装不下时逐半收缩，而不是让一次
// 只需要更短摘录的准备失败。
func FitSummarySession(payload map[string]any, counter PipelineStageTokenCounter,
	snapshot *V6ContextSnapshot, limit int) map[string]any {
	for {
		estimate := SummaryBatchEstimate(counter, snapshot, []map[string]any{payload},
			SummaryBaseReservedOutputTokens+SummaryReservedOutputTokens)
		if estimate <= limit {
			return payload
		}
		messages, _ := payload["messages"].([]map[string]any)
		if len(messages) > 1 {
			rest := append([]map[string]any(nil), messages[1:]...)
			pattern, _ := payload["interaction_pattern"].([]any)
			restPattern := append([]any(nil), pattern[1:]...)
			updated := map[string]any{}
			for key, value := range payload {
				updated[key] = value
			}
			updated["messages"] = rest
			updated["interaction_pattern"] = restPattern
			payload = updated
			continue
		}
		// 单轮在小上下文窗口仍可能过大：逐半收缩而非失败。
		if len(messages) == 0 {
			return nil
		}
		message := map[string]any{}
		for key, value := range messages[0] {
			message[key] = value
		}
		content := pyStr(message["content"])
		answer := pyStr(message["assistant_answer"])
		if utf8.RuneCountInString(content) <= 1 && utf8.RuneCountInString(answer) <= 1 {
			return nil
		}
		runes := []rune(content)
		half := utf8.RuneCountInString(content) / 2
		if half < 1 {
			half = 1
		}
		message["content"] = string(runes[:half])
		if answer != "" {
			answerRunes := []rune(answer)
			answerHalf := utf8.RuneCountInString(answer) / 2
			if answerHalf < 1 {
				answerHalf = 1
			}
			message["assistant_answer"] = string(answerRunes[:answerHalf])
		}
		updated := map[string]any{}
		for key, value := range payload {
			updated[key] = value
		}
		updated["messages"] = []map[string]any{message}
		payload = updated
	}
}

// PlanSummaryBatches 摘要器上下文预算内的贪心整会话批
// （topic_pipeline.py:3642-3673）。单会话装不下返回 nil。
func PlanSummaryBatches(payloads []map[string]any, counter PipelineStageTokenCounter,
	snapshot *V6ContextSnapshot, limit int) [][]int {
	var batches [][]int
	var current []int
	for position := range payloads {
		candidate := append(append([]int(nil), current...), position)
		candidatePayloads := make([]map[string]any, 0, len(candidate))
		for _, index := range candidate {
			candidatePayloads = append(candidatePayloads, payloads[index])
		}
		estimate := SummaryBatchEstimate(counter, snapshot, candidatePayloads,
			SummaryBaseReservedOutputTokens+SummaryReservedOutputTokens*len(candidate))
		if estimate <= limit {
			current = candidate
			continue
		}
		if len(current) == 0 {
			return nil
		}
		batches = append(batches, current)
		current = []int{position}
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// RecentSummarySessions 只摘要近期会话，但携带本次增量的会话必入
// （topic_pipeline.py:3676-3703）。
func RecentSummarySessions(grouped map[string][]*V6SourceMessage,
	prefixesBySession map[string]*CompletedSessionProjection,
	deltaEventIDs []string, now float64) []string {
	deltaIDs := map[string]bool{}
	for _, eventID := range deltaEventIDs {
		deltaIDs[eventID] = true
	}
	mandatory := map[string]bool{}
	for sessionID, messages := range grouped {
		for _, message := range messages {
			if deltaIDs[message.EventID] {
				mandatory[sessionID] = true
				break
			}
		}
	}
	cutoff := now - SummarySessionWindowSeconds
	type recentEntry struct {
		sessionID string
		completed float64
	}
	var recent []recentEntry
	for sessionID := range grouped {
		prefix := prefixesBySession[sessionID]
		completed := 0.0
		if prefix != nil && prefix.Boundary != nil {
			completed = prefix.Boundary.CompletedAt
		}
		if completed >= cutoff {
			recent = append(recent, recentEntry{sessionID, completed})
		}
	}
	sort.Slice(recent, func(i, j int) bool {
		if recent[i].completed != recent[j].completed {
			return recent[i].completed > recent[j].completed
		}
		return recent[i].sessionID < recent[j].sessionID
	})
	selected := map[string]bool{}
	for index, entry := range recent {
		if index >= SummarySessionLimit {
			break
		}
		selected[entry.sessionID] = true
	}
	for sessionID := range mandatory {
		selected[sessionID] = true
	}
	result := make([]string, 0, len(selected))
	for sessionID := range selected {
		result = append(result, sessionID)
	}
	sort.Strings(result)
	return result
}

// RecentRIDSummaries RID 的最新摘要，保留短增量要求的每一条
// （topic_pipeline.py:3706-3722）。
func RecentRIDSummaries(summaries []*SessionSummaryArtifact,
	requiredIDs []string) []*SessionSummaryArtifact {
	if len(summaries) <= RIDSessionSummaryLimit {
		return summaries
	}
	required := map[string]bool{}
	for _, item := range requiredIDs {
		required[item] = true
	}
	var kept []*SessionSummaryArtifact
	var rest []*SessionSummaryArtifact
	for _, item := range summaries {
		if required[item.SessionSummaryID] {
			kept = append(kept, item)
		} else {
			rest = append(rest, item)
		}
	}
	room := RIDSessionSummaryLimit - len(kept)
	if room < 0 {
		room = 0
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].CompletedAt != rest[j].CompletedAt {
			return rest[i].CompletedAt > rest[j].CompletedAt
		}
		return rest[i].SessionSummaryID < rest[j].SessionSummaryID
	})
	if len(rest) > room {
		rest = rest[:room]
	}
	return append(kept, rest...)
}

// RIDRequestEstimatePayload RID 请求的序列化串，用于上下文窗口预检
// （topic_pipeline.py:3725-3750）。
func RIDRequestEstimatePayload(ridInputs map[string]any) string {
	binding, _ := ridInputs["binding"].(*LearnedComponentBinding)
	var bindingProvenance any
	if binding != nil {
		bindingProvenance = binding.AsProvenance()
	}
	interests, _ := ridInputs["long_term_profile_interests"].([]*LongTermProfileInterestInput)
	interestPayloads := make([]map[string]any, 0, len(interests))
	for _, interest := range interests {
		interestPayloads = append(interestPayloads, interest.AsPayload())
	}
	summaries, _ := ridInputs["session_summaries"].([]*SessionSummaryArtifact)
	summaryPayloads := make([]map[string]any, 0, len(summaries))
	for _, summary := range summaries {
		summaryPayloads = append(summaryPayloads, summary.AsPayload())
	}
	payload := map[string]any{
		"binding":                     bindingProvenance,
		"input_snapshot_id":           ridInputs["input_snapshot_id"],
		"user_profile":                ridInputs["user_profile"],
		"long_term_profile_interests": interestPayloads,
		"session_summaries":           summaryPayloads,
		"activity_group":              ridInputs["activity_group"],
		"threshold_version":           ridInputs["threshold_version"],
		"feedback_context":            ridInputs["feedback_context"],
		"prompt_version":              RID_API_PROMPT_VERSION,
		"delta":                       ridInputs["delta"],
		"decision_context":            ridInputs["decision_context"],
		"request_id":                  "v6-rid-" + strings.Repeat("0", 32),
	}
	encoded, err := utils.MarshalNoEscape(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// CompletedAnswers 从成功完结的请求收集可见回答段
// （topic_pipeline.py:3753-3783）。
func CompletedAnswers(sessionID string, records []map[string]any) map[string]string {
	prefix := ProjectCompletedSessionPrefix(sessionID, records, true)
	if prefix == nil {
		return map[string]string{}
	}
	completionStatuses := map[string]string{}
	for _, record := range records {
		if pyStr(record["event_type"]) == SessionRequestCompletedEventType {
			completionStatuses[pyStr(record["request_id"])] =
				strings.ToLower(strings.TrimSpace(pyStr(record["status"])))
		}
	}
	successful := map[string]bool{}
	for _, requestID := range prefix.CompletedRequestIDs {
		if completionStatuses[requestID] == "success" {
			successful[requestID] = true
		}
	}
	answers := map[string][]string{}
	answerOrder := []string{}
	for _, record := range records {
		requestID := pyStr(record["request_id"])
		content, isStr := record["content"].(string)
		if successful[requestID] && pyStr(record["role"]) == "assistant" &&
			pyStr(record["event_type"]) == "chat.final" && isStr && strings.TrimSpace(content) != "" {
			if _, seen := answers[requestID]; !seen {
				answerOrder = append(answerOrder, requestID)
			}
			answers[requestID] = append(answers[requestID], strings.TrimSpace(content))
		}
	}
	result := map[string]string{}
	for _, requestID := range answerOrder {
		result[requestID] = strings.Join(answers[requestID], "\n\n")
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// turnPayload 摘要轮的载荷形状（:3490-3501/:3547-3557 共用）。
func turnPayload(turn SummaryTurn) map[string]any {
	return map[string]any{
		"event_id":          turn.Message.EventID,
		"ordinal":           turn.Message.Ordinal,
		"request_id":        turn.Message.RequestID,
		"role":              "user",
		"content":           turn.Content,
		"assistant_answer":  turn.Answer,
		"assistance_origin": turn.Message.AssistanceOrigin,
	}
}

// pyStrOrNilToEmpty nil → 空串。
func pyStrOrNilToEmpty(value any) string {
	if value == nil {
		return ""
	}
	return pyStr(value)
}
