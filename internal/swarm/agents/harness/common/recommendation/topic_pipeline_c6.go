package recommendation

// topic_pipeline_c6.go 一比一对齐上游 topic_pipeline.py:2889-2952 的
// _attach_interest_sources、:3292-3383 的 _rid_decision_context 与
// :4378-4479 的 wanted/证据/组件调用小票助手。

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AttachInterestSources 给全局卡补上兴趣背后的用户原话
// （topic_pipeline.py:2889-2952）。Memory 单元来源的卡携带该单元的陈述；
// 会话来源的卡携带会话的提问与（当单元持有该会话时）单元的摘要与陈述。
// 只携带本 scope 自己会话里的陈述；卡的单元归属与否定覆盖面不变。
func AttachInterestSources(plans []map[string]any, snapshot *V6ContextSnapshot,
	summaries []*SessionSummaryArtifact, feedback *InterestFeedback) {
	memory, _ := snapshot.State["memory"].(map[string]any)
	units, _ := memory["units"].([]any)
	unitLocators := map[string][][]any{}
	for _, raw := range units {
		unit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		companion, ok := unit["companion"].(map[string]any)
		if !ok {
			continue
		}
		locators := [][]any{}
		for _, rawLocator := range pyListAny(companion["canonical_source_locators"]) {
			if locator, ok := rawLocator.([]any); ok {
				locators = append(locators, append([]any(nil), locator...))
			}
		}
		unitLocators[pyStr(companion["unit_id"])] = locators
	}
	profileText := map[string]string{}
	for _, interest := range snapshot.LongTermProfileInterests {
		if len(interest.SourceProfileUnitIDs) > 0 {
			profileText[strings.Join(interest.SourceProfileUnitIDs, "\x00")] = interest.SummaryText
		}
	}
	summaryByID := map[string]*SessionSummaryArtifact{}
	for _, summary := range summaries {
		summaryByID[summary.SessionSummaryID] = summary
	}
	scopeSessions := map[string]bool{}
	sourceMessages := map[string]string{}
	locatorKeys := map[string]bool{}
	for _, message := range snapshot.Messages {
		scopeSessions[message.SessionID] = true
		if message.Natural() && PipelineScopeMatches(message.ProjectID, snapshot.ContextScope) {
			sourceMessages[locatorKey(message.SessionID, message.Ordinal)] = message.Content
		}
	}
	for _, plan := range plans {
		unitIDs := pyListStrings(plan["source_memory_unit_ids"])
		var locators [][]any
		summary := summaryByID[pyStr(plan["interest_summary_id"])]
		if summary != nil {
			for _, locator := range summary.SourceLocators {
				locators = append(locators, []any{locator.SessionID, locator.Ordinal})
			}
			if len(unitIDs) == 0 {
				events := make([]string, 0, len(summary.SourceEventIDs))
				for _, eventID := range summary.SourceEventIDs {
					events = append(events, MemoryEvidenceID(summary.SessionID, eventID))
				}
				candidates := make([][]string, 0, len(profileText))
				for joined := range profileText {
					candidates = append(candidates, strings.Split(joined, "\x00"))
				}
				unitIDs = feedback.HoldingUnits(events, candidates)
				if len(unitIDs) > 0 {
					plan["persona_context"] = pyStr(plan["persona_context"]) +
						"\n所属长期兴趣：" + profileText[strings.Join(unitIDs, "\x00")]
				}
			}
		}
		for _, unitID := range unitIDs {
			for _, locator := range unitLocators[unitID] {
				sessionID := pyStr(locator[0])
				if !scopeSessions[sessionID] {
					continue
				}
				duplicate := false
				for _, existing := range locators {
					if pyStr(existing[0]) == sessionID && pyIntOf(existing[1]) == pyIntOf(locator[1]) {
						duplicate = true
						break
					}
				}
				if !duplicate {
					locators = append(locators, locator)
				}
			}
		}
		plan["source_locators"] = locators
		seen := map[string]bool{}
		var statements []string
		for _, locator := range locators {
			key := locatorKey(pyStr(locator[0]), pyIntOf(locator[1]))
			if seen[key] {
				continue
			}
			seen[key] = true
			if content, present := sourceMessages[key]; present {
				statements = append(statements, content)
			}
		}
		if len(statements) > 0 {
			plan["persona_context"] = pyStr(plan["persona_context"]) +
				"\n用户原话：\n" + strings.Join(statements, "\n")
		}
		_ = locatorKeys
	}
}

// RidDecisionContext 冻结提供给 RID 的项目适配的当前时点证据
// （topic_pipeline.py:3292-3383）。
func RidDecisionContext(snapshot *V6ContextSnapshot, summaries []*SessionSummaryArtifact,
	feedback *InterestFeedback, decisionTime float64) map[string]any {
	naturalMessages := []*V6SourceMessage{}
	for _, item := range snapshot.Messages {
		if item.Natural() {
			naturalMessages = append(naturalMessages, item)
		}
	}
	sort.SliceStable(naturalMessages, func(i, j int) bool {
		if naturalMessages[i].ObservedAt != naturalMessages[j].ObservedAt {
			return naturalMessages[i].ObservedAt < naturalMessages[j].ObservedAt
		}
		if naturalMessages[i].SessionID != naturalMessages[j].SessionID {
			return naturalMessages[i].SessionID < naturalMessages[j].SessionID
		}
		return naturalMessages[i].Ordinal < naturalMessages[j].Ordinal
	})
	var recentNatural []*V6SourceMessage
	if len(naturalMessages) > 20 {
		recentNatural = naturalMessages[len(naturalMessages)-20:]
	} else {
		recentNatural = naturalMessages
	}
	recent30DayCount := 0.0
	for _, item := range naturalMessages {
		if 0 <= decisionTime-item.ObservedAt && decisionTime-item.ObservedAt <= 30*24*60*60 {
			recent30DayCount++
		}
	}
	activityGroup := "steady"
	if recent30DayCount >= 10 {
		activityGroup = "active"
	}
	requiredShortDeltaInterestSummaryIDs := ShortDeltaSummaryIDs(snapshot, summaries)

	profileUpdatedAt := map[string]float64{}
	memory, _ := snapshot.State["memory"].(map[string]any)
	units, _ := memory["units"].([]any)
	for _, raw := range units {
		unit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		core, _ := unit["core"].(map[string]any)
		companion, _ := unit["companion"].(map[string]any)
		if core == nil || companion == nil {
			continue
		}
		unitID := strings.TrimSpace(pyStr(companion["unit_id"]))
		timestamp := core["timestamp"]
		switch timestamp.(type) {
		case float64, int, int64:
			if unitID != "" {
				profileUpdatedAt[unitID] = toFloatOr(timestamp, 0)
			}
		}
	}

	profileInterestAges := []map[string]any{}
	for _, interest := range WantedProfileInterests(snapshot, feedback) {
		var updatedAt any
		for _, unitID := range interest.SourceProfileUnitIDs {
			if value, present := profileUpdatedAt[unitID]; present {
				if updatedAt == nil || value > updatedAt.(float64) {
					updatedAt = value
				}
			}
		}
		var ageSeconds any
		if updatedAt != nil {
			age := decisionTime - updatedAt.(float64)
			if age < 0 {
				age = 0
			}
			ageSeconds = age
		}
		profileInterestAges = append(profileInterestAges, map[string]any{
			"interest_summary_id": interest.InterestSummaryID,
			"updated_at":          updatedAt,
			"age_seconds":         ageSeconds,
		})
	}

	summaryAges := make([]map[string]any, 0, len(summaries))
	for _, summary := range summaries {
		age := decisionTime - summary.CompletedAt
		if age < 0 {
			age = 0
		}
		summaryAges = append(summaryAges, map[string]any{
			"session_summary_id": summary.SessionSummaryID,
			"completed_at":       summary.CompletedAt,
			"age_seconds":        age,
		})
	}

	recentNaturalFeedback := make([]map[string]any, 0, len(recentNatural))
	for _, item := range recentNatural {
		age := decisionTime - item.ObservedAt
		if age < 0 {
			age = 0
		}
		content := ClipSummaryText(item.Content, RIDFeedbackMessageChars)
		if content == "" {
			content = ""
		}
		recentNaturalFeedback = append(recentNaturalFeedback, map[string]any{
			"event_id":     item.EventID,
			"observed_at":  item.ObservedAt,
			"age_seconds":  age,
			"intent_cluster": item.IntentCluster,
			"action_type":  item.ActionType,
			"content":      content,
		})
	}

	return map[string]any{
		"decision_time":         decisionTime,
		"activity_group":        activityGroup,
		"activity_group_method": "natural_events_last_30_days_ge_10_v1",
		// RID 未读的会话里问出的最新问题让它无从纳入；触发随后等待一个
		// 能回答它的池（spec D7 rev 25）。
		"short_delta_lineage_required":            len(requiredShortDeltaInterestSummaryIDs) > 0,
		"required_short_delta_interest_summary_ids": requiredShortDeltaInterestSummaryIDs,
		"natural_event_count":                     len(naturalMessages),
		"natural_event_count_last_30_days":        recent30DayCount,
		"session_summary_ages":                    summaryAges,
		"profile_interest_ages":                   profileInterestAges,
		"recent_natural_feedback":                 recentNaturalFeedback,
	}
}

// WantedProfileInterests RID 仍可选的长期兴趣：被否定单元之外
// （topic_pipeline.py:4378-4390）。读取方拒绝被否定单元的每张卡，建立在
// 其上的兴趣只会产出页面永不展示的卡。
func WantedProfileInterests(snapshot *V6ContextSnapshot,
	feedback *InterestFeedback) []*LongTermProfileInterestInput {
	negatedUnits := feedback.DismissedUnits()
	var result []*LongTermProfileInterestInput
	for _, interest := range snapshot.LongTermProfileInterests {
		negated := false
		for _, unitID := range interest.SourceProfileUnitIDs {
			if negatedUnits[unitID] {
				negated = true
				break
			}
		}
		if !negated {
			result = append(result, interest)
		}
	}
	return result
}

// WantedSessionSummaries RID 仍可选的会话摘要：全局页上否定扣住的除外
// （topic_pipeline.py:4392-4411）。全局生成丢弃完全由否定内容构成的会话
// 兴趣；每张项目卡只延续项目自己的会话。
func WantedSessionSummaries(snapshot *V6ContextSnapshot,
	summaries []*SessionSummaryArtifact, feedback *InterestFeedback) []*SessionSummaryArtifact {
	if snapshot.ContextScope != GlobalNewChatScope {
		return summaries
	}
	eventsByInterest := map[string][]string{}
	for _, summary := range summaries {
		events := make([]string, 0, len(summary.SourceLocators))
		for _, locator := range summary.SourceLocators {
			events = append(events, MemoryEvidenceID(locator.SessionID, locator.EventID))
		}
		eventsByInterest[summary.SessionSummaryID] = events
	}
	withheld := feedback.Withheld(eventsByInterest)
	var result []*SessionSummaryArtifact
	for _, summary := range summaries {
		if !withheld[summary.SessionSummaryID] {
			result = append(result, summary)
		}
	}
	return result
}

// InterestsBehindDismissals 被否定扣住的兴趣 ID（topic_pipeline.py:4413-4424）。
func InterestsBehindDismissals(feedback *InterestFeedback,
	interests []*RIDInterestDecision) map[string]bool {
	eventsByInterest := map[string][]string{}
	for _, interest := range interests {
		events := make([]string, 0, len(interest.SourceLocators))
		for _, locator := range interest.SourceLocators {
			events = append(events, MemoryEvidenceID(locator.SessionID, locator.EventID))
		}
		eventsByInterest[interest.InterestSummaryID] = events
	}
	return feedback.Withheld(eventsByInterest)
}

// ProviderMatchesTrigger 触发键与组件小票的厂商/契约一致
// （topic_pipeline.py:4426-4437）。
func ProviderMatchesTrigger(triggerKey map[string]any, componentID string,
	receipt *LearnedComponentReceipt) bool {
	contracts, _ := triggerKey["component_contract_versions"].(map[string]any)
	return contracts != nil &&
		pyStr(contracts[componentID]) == receipt.Binding.ContractVersion &&
		pyStr(triggerKey["model"]) == receipt.Binding.ProviderVersion
}

// ComponentInvocationReceipt 组件调用小票（topic_pipeline.py:4439-4454）。
func ComponentInvocationReceipt(receipt *LearnedComponentReceipt, bindingScope string,
	candidateIDs, starterIDs []string) map[string]any {
	if candidateIDs == nil {
		candidateIDs = []string{}
	}
	if starterIDs == nil {
		starterIDs = []string{}
	}
	return map[string]any{
		"schema_version":    ComponentInvocationReceiptVersion,
		"component_id":      receipt.Binding.ComponentID,
		"request_id":        receipt.RequestID,
		"input_snapshot_id": receipt.InputSnapshotID,
		"binding_scope":     bindingScope,
		"candidate_ids":     candidateIDs,
		"starter_ids":       starterIDs,
	}
}

// ComponentInvocationAppliesToCard 组件调用是否作用到该卡
// （topic_pipeline.py:4456-4478）。
func ComponentInvocationAppliesToCard(invocation, card map[string]any) bool {
	switch pyStr(invocation["binding_scope"]) {
	case "all_cards":
		return true
	case "candidate":
		for _, candidateID := range pyListStrings(invocation["candidate_ids"]) {
			if pyStr(card["card_id"]) == candidateID {
				return true
			}
		}
		return false
	case "starter":
		for _, starterID := range pyListStrings(invocation["starter_ids"]) {
			if pyStr(card["source_starter_id"]) == starterID {
				return true
			}
		}
		return false
	}
	return false
}

// FlattenEvidence 证据束的载荷形态（topic_pipeline.py:4334-4367）。
func FlattenEvidence(bundle *EvidenceBundle) map[string]any {
	primary := bundle.Spans[0]
	result := map[string]any{
		"candidate_id":         bundle.CandidateID,
		"fetch_status":         "success",
		"source_url":           primary.SourceURL,
		"source_kind":          primary.SourceKind,
		"publisher":            primary.Publisher,
		"document_revision_id": primary.DocumentRevisionID,
		"retrieved_at": time.Unix(int64(primary.RetrievedAt), 0).UTC().
			Format("2006-01-02T15:04:05Z07:00"),
		"snapshot_date":     bundle.SnapshotDate,
		"snapshot_version":  bundle.SnapshotVersion,
		"source_gate_mode":  bundle.SourceGateMode,
		"content_boundary": map[string]any{
			"start": primary.Start,
			"end":   primary.End,
			"text":  primary.ExactText,
		},
		"exact_spans":       spanPayloads(bundle.Spans),
		"component_receipt": bundle.ComponentReceipt.AsPayload(),
	}
	if primary.PublishedAt != "" {
		result["published_at"] = primary.PublishedAt
		provenance := primary.PublicationDateProvenance
		if provenance != nil {
			result["published_at_source"] = fmt.Sprintf("%s:%s",
				provenance.SourceType, provenance.SourceField)
			result["publication_date_provenance"] = provenance.AsPayload()
		} else {
			result["published_at_source"] = "frozen_document_metadata"
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// locatorKey 会话+序号定位键。
func locatorKey(sessionID string, ordinal int) string {
	return fmt.Sprintf("%s\x00%d", sessionID, ordinal)
}

// pyListAny 元素列表（非列表返回 nil）。
func pyListAny(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	return items
}

// spanPayloads 片段的载荷列表。
func spanPayloads(spans []ExactEvidenceSpan) []map[string]any {
	out := make([]map[string]any, 0, len(spans))
	for index := range spans {
		out = append(out, spans[index].AsPayload())
	}
	return out
}
