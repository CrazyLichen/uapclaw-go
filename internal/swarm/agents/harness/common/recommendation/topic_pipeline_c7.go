package recommendation

// topic_pipeline_c7.go 一比一对齐上游 topic_pipeline.py:3786-4130 的
// _default_context_snapshot：流水线默认上下文快照构建。
// 会话/项目域直接 import（situation_report.go 已有 server/session 先例）；
// 已知偏差：Go ProjectRecord 无 name 字段，project_identifiers 缺项目名
// 一项（Python getattr(project, "name", "")）⤴️项目域补字段后回填。

import (
	"fmt"
	"sort"
	"strings"

	lifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultContextSnapshot 流水线默认上下文快照
// （topic_pipeline.py:3786-4130 _default_context_snapshot）。
func DefaultContextSnapshot(store *TopicStarterStore, contextScope string,
	now float64, recordsLoader func() map[string][]map[string]any) (*V6ContextSnapshot, error) {
	projectID, err := ProjectIDForScope(contextScope)
	if err != nil {
		return nil, err
	}
	records := recordsLoader()
	completedPrefixBySession := map[string]*CompletedSessionProjection{}
	for sessionID, sessionRecords := range records {
		sessionPrefix := ProjectCompletedSessionPrefix(sessionID, sessionRecords, true)
		if sessionPrefix != nil {
			completedPrefixBySession[sessionID] = sessionPrefix
		}
	}
	inputs := LoadGenerationInputs(store, now, func() map[string][]map[string]any {
		return records
	})
	rawStateReceipt, isMap := inputs.GenerationInputSnapshot["state_maintenance_receipt"].(map[string]any)
	if !isMap {
		return nil, &StateUnavailableError{Reason: "state_unavailable"}
	}
	sidecar := map[string]any{}
	if sidecarValue, ok := inputs.State["memory_sidecar"].(map[string]any); ok {
		for key, value := range sidecarValue {
			sidecar[key] = value
		}
	}
	feedback := InterestFeedbackFromState(inputs.State, contextScope, projectID, now)
	negativePreferences := feedback.Payloads()
	negativePreferenceVersion := feedback.Version()
	sidecar["negative_preferences"] = negativePreferences
	scopedState := map[string]any{}
	for key, value := range inputs.State {
		scopedState[key] = value
	}
	scopedState["memory_sidecar"] = sidecar
	stateReceipt := map[string]any{}
	for key, value := range rawStateReceipt {
		stateReceipt[key] = value
	}
	stateReceipt["negative_preference_version"] = negativePreferenceVersion

	metadata, _ := swarmsession.GetAllSessionsMetadata(1_000_000, 0)
	projectBySession := map[string]string{}
	for _, item := range metadata {
		sessionID := pyStr(item["session_id"])
		if sessionID != "" {
			projectBySession[sessionID] = pyStr(item["project_id"])
		}
	}
	projectsByID := map[string]lifecycle.ProjectRecord{}
	for _, project := range lifecycle.ListProjects() {
		projectsByID[project.ProjectID] = project
	}
	projectIdentifiersBySession := map[string][]string{}
	for _, item := range metadata {
		sessionID := pyStr(item["session_id"])
		if sessionID == "" {
			continue
		}
		projectID := pyStr(item["project_id"])
		project := projectsByID[projectID]
		identifiers := []string{sessionID, strings.TrimSpace(projectID),
			strings.TrimSpace(pyStr(item["project_dir"])),
			// 已知偏差：Go ProjectRecord 无 name 字段（Python project.name）
			"", strings.TrimSpace(project.ProjectDir)}
		seen := map[string]bool{}
		var unique []string
		for _, identifier := range identifiers {
			if identifier != "" && !seen[identifier] {
				seen[identifier] = true
				unique = append(unique, identifier)
			}
		}
		projectIdentifiersBySession[sessionID] = unique
	}

	mapping := HistoryStateMappingEntries(scopedState)
	var messages []*V6SourceMessage
	clusterOrder := []string{}
	clusterCounts := map[string]int{}
	for _, behavior := range inputs.ActiveBehaviors {
		mapped := mapping[behavior.BehaviorIndex]
		sessionProjectID, known := projectBySession[behavior.SessionID]
		if !known {
			sessionProjectID = ""
		}
		if !PipelineScopeMatches(sessionProjectID, contextScope) {
			continue
		}
		completedPrefix := completedPrefixBySession[behavior.SessionID]
		if completedPrefix == nil || !containsInt(completedPrefix.CompletedUserOrdinals, behavior.Ordinal) {
			continue
		}
		cluster := strings.TrimSpace(pyStr(mapped["tag"]))
		if cluster != "" {
			if _, seen := clusterCounts[cluster]; !seen {
				clusterOrder = append(clusterOrder, cluster)
			}
			clusterCounts[cluster]++
		}
		sessionRecords := records[behavior.SessionID]
		record := map[string]any{}
		if 0 <= behavior.Ordinal && behavior.Ordinal < len(sessionRecords) &&
			sessionRecords[behavior.Ordinal] != nil {
			record = sessionRecords[behavior.Ordinal]
		}
		messages = append(messages, MessageFromBehavior(behavior, record, mapped, sessionProjectID))
	}

	representedLocators := map[string]bool{}
	for _, message := range messages {
		representedLocators[locatorKey(message.SessionID, message.Ordinal)] = true
	}
	sessionIDs := make([]string, 0, len(completedPrefixBySession))
	for sessionID := range completedPrefixBySession {
		sessionIDs = append(sessionIDs, sessionID)
	}
	sort.Strings(sessionIDs)
	for _, sessionID := range sessionIDs {
		completedPrefix := completedPrefixBySession[sessionID]
		sessionProjectID, known := projectBySession[sessionID]
		if !known {
			sessionProjectID = ""
		}
		if !PipelineScopeMatches(sessionProjectID, contextScope) {
			continue
		}
		sessionRecords := records[sessionID]
		for _, ordinal := range completedPrefix.CompletedUserOrdinals {
			if representedLocators[locatorKey(sessionID, ordinal)] ||
				!(0 <= ordinal && ordinal < len(sessionRecords)) {
				continue
			}
			record := sessionRecords[ordinal]
			if record == nil {
				continue
			}
			role := strings.ToLower(strings.TrimSpace(pyStr(record["role"])))
			if role != "user" && role != "human" {
				continue
			}
			content, isStr := record["content"].(string)
			eventID := strings.TrimSpace(pyStr(record["id"]))
			inputOrigin := strings.TrimSpace(pyStr(record["input_origin"]))
			assistanceOrigin := strings.TrimSpace(pyStr(record["assistance_origin"]))
			if !isStr || strings.TrimSpace(content) == "" || eventID == "" || inputOrigin == "" {
				continue
			}
			switch assistanceOrigin {
			case "", "none", "unknown":
				continue
			}
			messages = append(messages, &V6SourceMessage{
				EventID: eventID, SessionID: sessionID, Ordinal: ordinal,
				Content: content, ObservedAt: HistoryTimestampOrZero(record),
				InputOrigin: inputOrigin, AssistanceOrigin: assistanceOrigin,
				IntentCluster: "unmapped", ActionType: "assisted_input",
				ProjectID: sessionProjectID,
			})
			representedLocators[locatorKey(sessionID, ordinal)] = true
		}
	}

	answersBySession := map[string]map[string]string{}
	for _, message := range messages {
		if _, present := answersBySession[message.SessionID]; !present {
			answersBySession[message.SessionID] = CompletedAnswers(message.SessionID, records[message.SessionID])
		}
	}
	for _, message := range messages {
		sessionRecords := records[message.SessionID]
		requestID := ""
		if 0 <= message.Ordinal && message.Ordinal < len(sessionRecords) &&
			sessionRecords[message.Ordinal] != nil {
			requestID = pyStr(sessionRecords[message.Ordinal]["request_id"])
		}
		message.RequestID = requestID
		message.AssistantAnswer = answersBySession[message.SessionID][requestID]
	}

	shortStore, err := NewShortIntentStateStore(store)
	if err != nil {
		return nil, err
	}
	shortState, err := shortStore.LoadLatestForScope(contextScope)
	if err != nil {
		return nil, err
	}
	if shortState == nil {
		shortState = &ShortIntentState{
			ShortIntentVersion: 0,
			InputSnapshotID: fmt.Sprintf("%s:%s:0", SHORT_INTENT_SNAPSHOT_PREFIX,
				contextScope),
			EvaluatedAt: now,
		}
	}
	triggerState, err := shortStore.LoadTriggerForVersion(contextScope,
		shortState.ShortIntentVersion)
	if err != nil {
		return nil, err
	}
	var pendingLineage []map[string]any
	if triggerState != nil {
		pendingLineage = triggerState.ReasonEvidenceLineage
	}
	naturalMessageEvents := map[string]bool{}
	for _, message := range messages {
		if message.Natural() {
			naturalMessageEvents[message.EventID] = true
		}
	}
	shortDeltaEventIDs := map[string]bool{}
	for eventID := range ShortTriggerEvidenceEventIDs(pendingLineage) {
		if naturalMessageEvents[eventID] {
			shortDeltaEventIDs[eventID] = true
		}
	}
	offset := 0
	for len(shortDeltaEventIDs) > 0 {
		manifests, err := store.QueryEvents(QueryEventsOptions{
			EventType: "starter_slate_manifest",
			PolicyID:  stringPtr(POLICY_ID),
			PayloadEquals: map[string]any{
				"context_scope":        contextScope,
				"short_intent_version": shortState.ShortIntentVersion,
			},
			Limit:  32,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range manifests {
			payload, _, ok := payloadMap(row["payload"])
			if !ok {
				payload = map[string]any{}
			}
			selection, _, _ := payloadMap(payload["rid_interest_selection"])
			if selection == nil {
				selection = map[string]any{}
			}
			consumedEventIDs := map[string]bool{}
			for _, eventID := range pyListStrings(selection["short_trigger_delta_event_ids"]) {
				consumedEventIDs[eventID] = true
			}
			triggerConsumed := false
			if subsetEvents(shortDeltaEventIDs, consumedEventIDs) &&
				pyStr(selection["delta_linked_interest_summary_id"]) != "" {
				triggerConsumed = true
			}
			if contextScope != GlobalNewChatScope {
				interests := map[string]map[string]any{}
				ridResult, _, _ := payloadMap(payload["rid_result"])
				if ridResult == nil {
					ridResult = map[string]any{}
				}
				for _, raw := range pyListMaps(ridResult["trigger_interests"]) {
					interests[pyStr(raw["interest_summary_id"])] = raw
				}
				consumedEventIDs = map[string]bool{}
				cardRows, err := store.QueryEvents(QueryEventsOptions{
					EventType: "starter_card",
					SlateID:   stringPtr(pyStr(payload["manifest_id"])),
					PolicyID:  stringPtr(POLICY_ID),
					Limit:     4,
				})
				if err != nil {
					return nil, err
				}
				for _, cardRow := range cardRows {
					card, _, ok := payloadMap(cardRow["payload"])
					if !ok {
						continue
					}
					revisionIDs := pyListStrings(payload["card_revision_ids"])
					if !containsString(revisionIDs, pyStr(card["card_revision_id"])) ||
						pyStr(card["generation_batch_id"]) != pyStr(payload["generation_batch_id"]) {
						continue
					}
					if pyStr(card["category"]) == "project_continuation" {
						for _, locator := range pyListMaps(card["project_source_locators"]) {
							consumedEventIDs[pyStr(locator["event_id"])] = true
						}
					} else {
						interest := interests[pyStr(card["interest_summary_id"])]
						if interest != nil {
							for _, eventID := range pyListStrings(interest["source_event_ids"]) {
								consumedEventIDs[eventID] = true
							}
						}
					}
				}
				triggerConsumed = intersectEvents(shortDeltaEventIDs, consumedEventIDs)
			}
			// 已带增量血缘的成功全局池已过写入方的兴趣/来源与首窗闸门；
			// 非触发求值抹掉该血缘的旧池没有。
			if triggerConsumed {
				pendingLineage = nil
				shortDeltaEventIDs = map[string]bool{}
				break
			}
		}
		if len(manifests) < 32 {
			break
		}
		offset += len(manifests)
	}

	deltaIndices := map[string]bool{}
	for _, item := range inputs.DeltaBehaviors {
		deltaIndices[fmt.Sprint(item.BehaviorIndex)] = true
	}
	longDeltaEventIDs := map[string]bool{}
	for _, item := range inputs.ActiveBehaviors {
		if !deltaIndices[fmt.Sprint(item.BehaviorIndex)] {
			continue
		}
		sessionProjectID, known := projectBySession[item.SessionID]
		if !known {
			sessionProjectID = ""
		}
		if !PipelineScopeMatches(sessionProjectID, contextScope) {
			continue
		}
		eventID := item.RecordID
		if eventID == "" {
			eventID = fmt.Sprintf("behavior:%d", item.BehaviorIndex)
		}
		longDeltaEventIDs[eventID] = true
	}
	var deltaEventIDs []string
	for _, message := range messages {
		if longDeltaEventIDs[message.EventID] || shortDeltaEventIDs[message.EventID] {
			deltaEventIDs = append(deltaEventIDs, message.EventID)
		}
	}

	activeVersion := pyIntOf(inputs.GenerationInputSnapshot["active_delta_version"])
	memory, _ := inputs.State["memory"].(map[string]any)
	profileSemanticVersion := pyIntOf(memory["semantic_version"])
	var longProfile []*LongTermProfileInterestInput
	if contextScope == GlobalNewChatScope {
		longProfile, err = DeprojectedLongProfile(scopedState, profileSemanticVersion,
			projectBySession, projectIdentifiersBySession)
		if err != nil {
			return nil, err
		}
	}

	abstractInterestClusters := mostCommonClusters(clusterOrder, clusterCounts, 16)
	longProfileClusters := make([]string, 0, len(longProfile))
	longProfileIDs := make([]string, 0, len(longProfile))
	for _, item := range longProfile {
		longProfileClusters = append(longProfileClusters, item.InterestCluster)
		longProfileIDs = append(longProfileIDs, item.InterestSummaryID)
	}
	var shortIntentClusters []string
	if triggerState != nil {
		shortIntentClusters = append([]string(nil), triggerState.DominantIntentClusters...)
	}

	var otherScopeDiscussion []map[string]any
	if contextScope == GlobalNewChatScope {
		otherScopeDiscussion, err = ProfileProjectDiscussion(store, scopedState,
			projectBySession, projectIdentifiersBySession)
		if err != nil {
			return nil, err
		}
	} else {
		otherScopeDiscussion, err = ProjectUnitGlobalDiscussion(store, scopedState,
			projectID, projectBySession)
		if err != nil {
			return nil, err
		}
	}

	var completedPrefixes []*CompletedSessionProjection
	for _, sessionID := range sessionIDs {
		sessionProjectID, known := projectBySession[sessionID]
		if !known {
			sessionProjectID = ""
		}
		if !PipelineScopeMatches(sessionProjectID, contextScope) {
			continue
		}
		completedPrefixes = append(completedPrefixes, completedPrefixBySession[sessionID])
	}

	return &V6ContextSnapshot{
		State:         scopedState,
		ContextScope:  contextScope,
		ProjectID:     nilAny(projectID),
		Messages:      messages,
		DeltaEventIDs: deltaEventIDs,
		ShortIntentState: &ShortIntentState{
			ShortIntentVersion:     shortState.ShortIntentVersion,
			InputSnapshotID:        shortState.InputSnapshotID,
			EvaluatedAt:            shortState.EvaluatedAt,
			TriggerReasons:         shortState.TriggerReasons,
			ReasonEvidenceLineage:  shortState.ReasonEvidenceLineage,
			DominantIntentClusters: shortState.DominantIntentClusters,
		},
		PendingShortIntentTriggerLineage: pendingLineage,
		InputSnapshotID: fmt.Sprintf("v6-input:%s:%d:%d:%d", contextScope,
			profileSemanticVersion, shortState.ShortIntentVersion, activeVersion),
		ProfileProjection: map[string]any{
			"profile_semantic_version":       profileSemanticVersion,
			"abstract_interest_clusters":     abstractInterestClusters,
			"abstract_long_profile_clusters": longProfileClusters,
			"long_term_profile_interest_ids": longProfileIDs,
			// 评分读取触发当前版本的求值。晚于它的无触发求值没有自己的意图，
			// 不会退役仍在锚定该版本的意图。
			"short_intent_clusters": shortIntentClusters,
		},
		LongTermProfileInterests:     longProfile,
		OtherScopeDiscussionCoverage: otherScopeDiscussion,
		CompletedSessionPrefixes:     completedPrefixes,
		StateMaintenanceReceipt:      stateReceipt,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsInt 整数列表包含。
func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// subsetEvents 子集判定。
func subsetEvents(events, candidate map[string]bool) bool {
	for event := range events {
		if !candidate[event] {
			return false
		}
	}
	return true
}

// intersectEvents 交集非空判定。
func intersectEvents(events, candidate map[string]bool) bool {
	for event := range events {
		if candidate[event] {
			return true
		}
	}
	return false
}

// mostCommonClusters Counter.most_common(n)：计数降序，并列保持首见序。
func mostCommonClusters(order []string, counts map[string]int, limit int) []string {
	sorted := append([]string(nil), order...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return counts[sorted[i]] > counts[sorted[j]]
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

// nilAny 非空项目 ID 透传，空串对齐 Python None。
func nilAny(projectID string) any {
	if projectID == "" {
		return nil
	}
	return projectID
}
