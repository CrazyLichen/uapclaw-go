package recommendation

// topic_outcomes.go 一比一对齐上游 topic_outcomes.py 的自足工件层
// （topic_outcomes.py:44-970 + :1918-2022）。
//
// 交互记录半区（:955-1916 _shown_card/record_terminal_interaction/dismissal/
// migrate/join_starter_outcomes/TopicStarterStoreOutcomeSink、
// :1457-1528 record_terminal_session_start）⤴️ 31c Task 6-9：
// 依赖 topic_product.py 的 find_exact_terminal_card（:5182，其
// _project_sources_still_belong :5273 依赖 server 会话域 get_session_metadata）、
// card_uses_host_evidence（:3667，corpus/hot_list/profile_interest 三种 host
// receipt）、valid_search_receipt（:811，四种 search receipt schema）、
// card_is_news/is_cold_start_card/card_search_receipt/card_provider_response_ids、
// memory_sessions_by_unit/interest_evidence_ids/interest_session_ids，以及
// topic_news.py news_card_event（:300，load_hot_list_pull 链）。上述符号落地后
// 本文件按上游结构补齐半区并删除交点注释（规则 8）。

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// StarterOutcomeSchemaVersion 起始结果 schema 版本（topic_outcomes.py:44）
	StarterOutcomeSchemaVersion = "topic-starter-v6-starter-outcome-v3"
	// UtilityTrainingSchemaVersion 效用训练样本 schema 版本（:45）
	UtilityTrainingSchemaVersion = "topic-starter-v6-utility-training-v2"
	// RevisitPairSchemaVersion 回访对 schema 版本（:46）
	RevisitPairSchemaVersion = "topic-starter-v6-revisit-pair-v1"
	// RIDTrainingSchemaVersion RID 训练元组 schema 版本（:47）
	RIDTrainingSchemaVersion = "topic-starter-v6-rid-training-tuple-v3"
	// RIDUserContextSchemaVersion RID 用户上下文快照 schema 版本（:48）
	RIDUserContextSchemaVersion = "topic-starter-v6-rid-user-context-v1"
	// PreferencePairSchemaVersion 偏好对 schema 版本（:49）
	PreferencePairSchemaVersion = "topic-starter-v6-preference-pair-v1"
	// TrainingSliceSchemaVersion 训练切片清单 schema 版本（:50）
	TrainingSliceSchemaVersion = "topic-starter-v6-training-slice-v2"
	// OutcomeSignalSchemaVersion 新页结果信号 schema 版本（:1918）
	OutcomeSignalSchemaVersion = "topic-starter-v6-new-page-outcome-signal-v1"

	// GlobalNewChatScope 全局新会话 scope（topic_product.py:81 ⤴️31c 归位）
	GlobalNewChatScope = "global_new_chat"
)

// GlobalProjectBuckets 全局桶集合（topic_product.py:3245 ⤴️31c 归位）。
var GlobalProjectBuckets = map[string]bool{"": true, "default": true, "default_code": true}

// ──────────────────────────── 导出函数 ────────────────────────────

// ProjectIDForScope scope → 项目 ID；非法 scope 报错
// （topic_product.py:3254-3266 ⤴️31c 归位）。
func ProjectIDForScope(contextScope string) (string, error) {
	if contextScope == GlobalNewChatScope {
		return "", nil
	}
	if strings.HasPrefix(contextScope, "project:") {
		projectID := strings.TrimPrefix(contextScope, "project:")
		if projectID != "" && projectID == strings.TrimSpace(projectID) {
			return projectID, nil
		}
	}
	return "", fmt.Errorf("invalid context_scope")
}

// BuildRevisitPair 构建回访对（topic_outcomes.py:162-200）。
func BuildRevisitPair(earlier, later *SessionSummaryArtifact, matchLabel bool,
	matcherID, matcherVersion string) (*RevisitPair, error) {
	var reason string
	hasReason := false
	if earlier.SessionID == later.SessionID {
		reason, hasReason = "same_session_not_revisit", true
	} else if earlier.ContextScope != later.ContextScope {
		reason, hasReason = "context_scope_mismatch", true
	} else if later.CompletedAt <= earlier.CompletedAt {
		reason, hasReason = "later_summary_not_later", true
	} else if !earlier.NaturalRevisitEligible() {
		reason, hasReason = "earlier_summary_ineligible", true
	} else if later.InputOrigin != "manual_free_text" {
		reason, hasReason = "ineligible_input_origin", true
	} else if later.AssistanceOrigin != "none" {
		reason, hasReason = "assisted_input_excluded", true
	}
	pair := &RevisitPair{
		RevisitPairID: fmt.Sprintf("revisit:%s:%s:%s",
			earlier.SessionSummaryID, later.SessionSummaryID, matcherVersion),
		EarlierSummaryID: earlier.SessionSummaryID,
		LaterSummaryID:   later.SessionSummaryID,
		EarlierSessionID: earlier.SessionID,
		LaterSessionID:   later.SessionID,
		ContextScope:     earlier.ContextScope,
		MatchLabel:       matchLabel,
		MatcherID:        matcherID,
		MatcherVersion:   matcherVersion,
		NaturalEligible:  !hasReason,
		ExclusionReason:  nilIfEmpty(reason, hasReason),
		ObservedAt:       later.CompletedAt,
		SchemaVersion:    RevisitPairSchemaVersion,
	}
	if err := pair.validate(); err != nil {
		return nil, err
	}
	return pair, nil
}

// BuildRIDTrainingTuple 构建 RID 训练元组（topic_outcomes.py:342-386）。
func BuildRIDTrainingTuple(ridTrainingTupleID, corpusBatchID, userHistorySnapshotID,
	userID string, positivePair *RevisitPair,
	intraInterestNegatives, interInterestNegatives []string) (*RIDTrainingTuple, error) {
	if !positivePair.NaturalEligible || !positivePair.MatchLabel {
		return nil, fmt.Errorf("RID training requires a natural positive revisit pair")
	}
	for _, value := range []string{ridTrainingTupleID, corpusBatchID, userHistorySnapshotID, userID} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("RID training tuple contract failed")
		}
	}
	if _, err := ProjectIDForScope(positivePair.ContextScope); err != nil {
		return nil, fmt.Errorf("RID training tuple contract failed")
	}
	if len(intraInterestNegatives) == 0 || len(interInterestNegatives) == 0 {
		return nil, fmt.Errorf("RID training tuple contract failed")
	}
	tuple := &RIDTrainingTuple{
		RIDTrainingTupleID:              ridTrainingTupleID,
		CorpusBatchID:                   corpusBatchID,
		UserHistorySnapshotID:           userHistorySnapshotID,
		UserID:                          userID,
		ContextScope:                    positivePair.ContextScope,
		PositiveRevisitPairID:           positivePair.RevisitPairID,
		PositiveSummaryID:               positivePair.EarlierSummaryID,
		PositiveWitnessSummaryID:        positivePair.LaterSummaryID,
		IntraInterestNegativeSummaryIDs: intraInterestNegatives,
		InterInterestNegativeSummaryIDs: interInterestNegatives,
		MatcherID:                       positivePair.MatcherID,
		MatcherVersion:                  positivePair.MatcherVersion,
		SchemaVersion:                   RIDTrainingSchemaVersion,
	}
	if err := tuple.validate(); err != nil {
		return nil, err
	}
	return tuple, nil
}

// BuildTrainingSliceManifest 构建训练切片清单（topic_outcomes.py:526-570）。
func BuildTrainingSliceManifest(trainingSliceManifestID string, windowStart, windowEnd,
	labelHorizon float64, filters map[string]any, contextScope string,
	inputArtifactVersions map[string]string, utilityExamples []*UtilityTrainingExample,
	offlineMetrics map[string]float64, inputArtifactIDs []string,
	inputExcludedArtifactReasons map[string]string) (*TrainingSliceManifest, error) {
	included := dedupeStrings(inputArtifactIDs)
	excluded := map[string]string{}
	for key, value := range inputExcludedArtifactReasons {
		excluded[key] = value
	}
	for _, example := range utilityExamples {
		reason := ""
		hasReason := false
		if example.TrainingExclusionReason != nil {
			reason, hasReason = *example.TrainingExclusionReason, true
		}
		if example.ContextScope != contextScope {
			reason, hasReason = "context_scope_mismatch", true
		} else if !(windowStart <= example.LabelHorizon && example.LabelHorizon <= labelHorizon) {
			reason, hasReason = "example_after_label_horizon", true
		} else if example.FeatureSnapshot == nil {
			if !hasReason {
				reason, hasReason = "missing_feature_snapshot", true
			}
		} else if toFloatOr(example.FeatureSnapshot["observed_at"], 0) >
			math.Min(example.LabelHorizon, labelHorizon) {
			reason, hasReason = "feature_snapshot_after_label_horizon", true
		}
		if hasReason {
			excluded[example.UtilityExampleID] = reason
		} else {
			included = append(included, example.UtilityExampleID)
		}
	}
	manifest := &TrainingSliceManifest{
		TrainingSliceManifestID: trainingSliceManifestID,
		WindowStart:             windowStart,
		WindowEnd:               windowEnd,
		LabelHorizon:            labelHorizon,
		Filters:                 filters,
		ContextScope:            contextScope,
		InputArtifactVersions:   inputArtifactVersions,
		IncludedArtifactIDs:     included,
		ExcludedArtifactReasons: excluded,
		OfflineMetrics:          offlineMetrics,
		SchemaVersion:           TrainingSliceSchemaVersion,
	}
	if err := manifest.validate(); err != nil {
		return nil, err
	}
	return manifest, nil
}

// BuildUtilityTrainingExample 保留产品标签并排除泄漏或错配特征
// （topic_outcomes.py:784-836）。
func BuildUtilityTrainingExample(outcome *StarterOutcome, featureSnapshot map[string]any) (*UtilityTrainingExample, error) {
	var admitted map[string]any
	var exclusionReason string
	hasExclusion := false
	if featureSnapshot == nil {
		exclusionReason, hasExclusion = "missing_feature_snapshot", true
	} else if pyStr(featureSnapshot["snapshot_id"]) != outcome.UserStateSnapshotID {
		exclusionReason, hasExclusion = "feature_snapshot_id_mismatch", true
	} else if !finiteTimestamp(featureSnapshot["observed_at"]) {
		exclusionReason, hasExclusion = "feature_snapshot_time_invalid", true
	} else if toFloatOr(featureSnapshot["observed_at"], 0) > outcome.LabelHorizon {
		exclusionReason, hasExclusion = "feature_snapshot_after_label_horizon", true
	} else if strings.TrimSpace(pyStr(featureSnapshot["version"])) == "" ||
		!isMapValue(featureSnapshot["features"]) {
		exclusionReason, hasExclusion = "feature_snapshot_contract_failed", true
	} else {
		admitted = map[string]any{
			"snapshot_id": pyStr(featureSnapshot["snapshot_id"]),
			"observed_at": toFloatOr(featureSnapshot["observed_at"], 0),
			"version":     pyStr(featureSnapshot["version"]),
			"features":    featureSnapshot["features"],
		}
	}
	labels := map[string]any{
		"shown":                     outcome.Shown,
		"clicked":                   outcome.Clicked,
		"not_interested":            outcome.NotInterested,
		"started":                   outcome.Started,
		"conversation_depth":        outcome.ConversationDepth,
		"future_activity_observed":  outcome.FutureActivityObserved,
		"future_natural_activity":   outcome.FutureNaturalActivity,
		"future_pa_active_activity": outcome.FuturePAActiveActivity,
		"outcome_label":             outcome.OutcomeLabel,
	}
	var reasonPtr *string
	if hasExclusion {
		reasonPtr = &exclusionReason
	}
	example := &UtilityTrainingExample{
		UtilityExampleID:            fmt.Sprintf("utility:%s", outcome.OutcomeID),
		OutcomeID:                   outcome.OutcomeID,
		UserStateSnapshotID:         outcome.UserStateSnapshotID,
		ContextScope:                outcome.ContextScope,
		LabelHorizon:                outcome.LabelHorizon,
		Labels:                      labels,
		NaturalLaneEligible:         outcome.FutureNaturalActivity,
		PAActiveEligible:            outcome.FuturePAActiveActivity,
		NaturalLaneExclusionReasons: outcome.FutureActivityExclusionReasons,
		TrainingExclusionReason:     reasonPtr,
		FeatureSnapshot:             admitted,
		VersionRefs:                 outcome.VersionRefs,
		SchemaVersion:               UtilityTrainingSchemaVersion,
	}
	if err := example.validate(); err != nil {
		return nil, err
	}
	return example, nil
}

// ArtifactFromPayload 严格按 schema_version 分发（topic_outcomes.py:839-861）。
func ArtifactFromPayload(payload any) (any, error) {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("artifact payload must be an object")
	}
	switch pyStr(root["schema_version"]) {
	case SESSION_SUMMARY_SCHEMA_VERSION:
		return SessionSummaryArtifactFromPayload(payload)
	case RevisitPairSchemaVersion:
		return RevisitPairFromPayload(payload)
	case RIDTrainingSchemaVersion:
		return RIDTrainingTupleFromPayload(payload)
	case RIDUserContextSchemaVersion:
		return RIDUserContextSnapshotFromPayload(payload)
	case StarterOutcomeSchemaVersion:
		return StarterOutcomeFromPayload(payload)
	case UtilityTrainingSchemaVersion:
		return UtilityTrainingExampleFromPayload(payload)
	case PreferencePairSchemaVersion:
		return PreferencePairFromPayload(payload)
	case TrainingSliceSchemaVersion:
		return TrainingSliceManifestFromPayload(payload)
	}
	return nil, fmt.Errorf("unsupported artifact schema_version")
}

// PersistOutcomeArtifact 向既有有界事件账本追加一个带版本的工件
// （topic_outcomes.py:929-952）。
func PersistOutcomeArtifact(store *TopicStarterStore, artifact any, createdAt float64) bool {
	if !finiteTimestamp(createdAt) {
		return false
	}
	eventType, artifactID, sessionID, slateID, payload, ok := outcomeStorageFields(artifact)
	if !ok {
		return false
	}
	recorded, _ := store.RecordEvent(map[string]any{
		"session_id":        sessionID,
		"slate_id":          slateID,
		"event_type":        eventType,
		"topic_id":          artifactID,
		"policy_id":         POLICY_ID,
		"selection_mode":    "training_data_plane",
		"propensity_reason": "not_applicable",
		"payload":           payload,
		"created_at":        createdAt,
	})
	return recorded
}

// RecordTerminalConversationDepth 记录一次已启动会话的最终观察深度
// （topic_outcomes.py:1531-1580）。
func RecordTerminalConversationDepth(store *TopicStarterStore, manifestID, exposureID,
	cardRevisionID, startedSessionID string, conversationDepth int, createdAt float64) bool {
	if strings.TrimSpace(startedSessionID) == "" {
		return false
	}
	if conversationDepth < 0 {
		return false
	}
	if !finiteTimestamp(createdAt) {
		return false
	}
	start := exactOutcomeEvent(store, "session_bound", manifestID, exposureID, cardRevisionID)
	if start == nil {
		return false
	}
	if pyStr(start["session_id"]) != startedSessionID {
		return false
	}
	if createdAt < toFloatOr(start["created_at"], 0) {
		return false
	}
	startPayload := mapAnyOf(start["payload"])
	payload := map[string]any{}
	for key, value := range startPayload {
		payload[key] = value
	}
	payload["conversation_depth"] = conversationDepth
	recorded, _ := store.RecordEvent(map[string]any{
		"session_id":        startedSessionID,
		"slate_id":          exposureID,
		"event_type":        "conversation_depth",
		"topic_id":          cardRevisionID,
		"policy_id":         POLICY_ID,
		"selection_mode":    "unknown",
		"propensity_reason": "not_estimated",
		"payload":           payload,
		"created_at":        createdAt,
	})
	return recorded
}

// RecordTerminalFutureActivity 记录后续活动并显式标注 natural/PA 资格
// （topic_outcomes.py:1599-1673）。
func RecordTerminalFutureActivity(store *TopicStarterStore, manifestID, exposureID,
	cardRevisionID, startedSessionID, laterSessionID, sourceEventID, sourceLocator,
	inputOrigin, assistanceOrigin, contextScope string, createdAt float64) bool {
	for _, value := range []string{startedSessionID, laterSessionID, sourceEventID,
		sourceLocator, inputOrigin, assistanceOrigin, contextScope} {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	if !finiteTimestamp(createdAt) {
		return false
	}
	start := exactOutcomeEvent(store, "session_bound", manifestID, exposureID, cardRevisionID)
	if start == nil {
		return false
	}
	if pyStr(start["session_id"]) != startedSessionID {
		return false
	}
	if pyStr(mapAnyOf(start["payload"])["context_scope"]) != contextScope {
		return false
	}
	if createdAt < toFloatOr(start["created_at"], 0) {
		return false
	}
	reason := naturalExclusionReason(inputOrigin, assistanceOrigin, startedSessionID, laterSessionID)
	startPayload := mapAnyOf(start["payload"])
	payload := map[string]any{}
	for key, value := range startPayload {
		payload[key] = value
	}
	payload["later_session_id"] = laterSessionID
	payload["source_event_id"] = sourceEventID
	payload["source_locator"] = sourceLocator
	payload["input_origin"] = inputOrigin
	payload["assistance_origin"] = assistanceOrigin
	payload["natural_revisit_eligible"] = reason == ""
	payload["pa_active_eligible"] = reason == ""
	payload["exclusion_reason"] = nilIfEmpty(reason, reason != "")
	recorded, _ := store.RecordEvent(map[string]any{
		"session_id":        laterSessionID,
		"slate_id":          exposureID,
		"event_type":        "future_activity",
		"topic_id":          cardRevisionID,
		"policy_id":         POLICY_ID,
		"selection_mode":    "unknown",
		"propensity_reason": "not_estimated",
		"payload":           payload,
		"created_at":        createdAt,
	})
	return recorded
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// finiteTimestamp 有限非负时间戳（topic_outcomes.py:62-63）。
func finiteTimestamp(value any) bool {
	switch v := value.(type) {
	case int:
		return float64(v) >= 0
	case float64:
		return !math.IsInf(v, 0) && !math.IsNaN(v) && v >= 0
	default:
		return false
	}
}

// validOutcomeScope scope 校验（topic_outcomes.py:66-71）。
func validOutcomeScope(value any) bool {
	scope := pyStr(value)
	if scope == GlobalNewChatScope {
		return true
	}
	if strings.HasPrefix(scope, "project:") && strings.TrimSpace(strings.TrimPrefix(scope, "project:")) != "" {
		return true
	}
	return false
}

// nonEmptyStr 非空字符串判定（:58-59）。
func nonEmptyStr(value any) bool {
	s, ok := value.(string)
	return ok && strings.TrimSpace(s) != ""
}

// naturalExclusionReason natural/PA 资格排除原因（:1583-1596）。
func naturalExclusionReason(inputOrigin, assistanceOrigin, startedSessionID, laterSessionID string) string {
	if inputOrigin != "manual_free_text" {
		return "ineligible_input_origin"
	}
	if assistanceOrigin != "none" {
		return "assisted_input_excluded"
	}
	if laterSessionID == startedSessionID {
		return "same_session_not_revisit"
	}
	return ""
}

// nilIfEmpty 空串转 nil（Python None 语义）。
func nilIfEmpty(value string, hasValue bool) any {
	if !hasValue {
		return nil
	}
	return value
}

// isMapValue map 值判定。
func isMapValue(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

// dedupeStrings 保序去重（dict.fromkeys 语义）。
func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// exactOutcomeEvent 精确匹配一条事件（topic_outcomes.py:1425-1454 _exact_event）。
func exactOutcomeEvent(store *TopicStarterStore, eventType, manifestID, exposureID, cardRevisionID string) map[string]any {
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: eventType,
		SlateID:   &exposureID,
		TopicID:   &cardRevisionID,
		PolicyID:  &poolPolicyIDPtr,
		Limit:     8,
	})
	if err != nil {
		return nil
	}
	var matches []map[string]any
	for _, row := range rows {
		payload, ok := row["payload"].(map[string]any)
		if !ok {
			continue
		}
		if pyStr(payload["manifest_id"]) != manifestID {
			continue
		}
		if pyStr(payload["exposure_id"]) != exposureID {
			continue
		}
		if pyStr(payload["card_revision_id"]) != cardRevisionID {
			continue
		}
		matches = append(matches, row)
	}
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// outcomeStorageFields 工件的四列存储字段（:864-921 _artifact_storage_fields）。
func outcomeStorageFields(artifact any) (eventType, artifactID, sessionID, slateID string, payload map[string]any, ok bool) {
	switch value := artifact.(type) {
	case *SessionSummaryArtifact:
		if value == nil {
			return "", "", "", "", nil, false
		}
		return "session_summary", value.SessionSummaryID, value.SessionID, value.ContextScope, value.AsPayload(), true
	case *RevisitPair:
		if value == nil {
			return "", "", "", "", nil, false
		}
		return "revisit_pair", value.RevisitPairID, value.LaterSessionID, value.ContextScope, value.AsPayload(), true
	case *RIDTrainingTuple:
		if value == nil {
			return "", "", "", "", nil, false
		}
		return "rid_training_tuple", value.RIDTrainingTupleID, value.UserHistorySnapshotID, value.ContextScope, value.AsPayload(), true
	case *RIDUserContextSnapshot:
		if value == nil {
			return "", "", "", "", nil, false
		}
		return "rid_user_context_snapshot", value.UserHistorySnapshotID, value.UserHistorySnapshotID, value.ContextScope, value.AsPayload(), true
	case *StarterOutcome:
		if value == nil {
			return "", "", "", "", nil, false
		}
		session := value.ExposureID
		if sessionID, isString := value.StartedSessionID.(string); isString && sessionID != "" {
			session = sessionID
		}
		return "starter_outcome", value.OutcomeID, session, value.ExposureID, value.AsPayload(), true
	case *UtilityTrainingExample:
		if value == nil {
			return "", "", "", "", nil, false
		}
		return "utility_training_example", value.UtilityExampleID, value.UserStateSnapshotID, value.ContextScope, value.AsPayload(), true
	case *PreferencePair:
		if value == nil {
			return "", "", "", "", nil, false
		}
		return "preference_pair", value.PreferencePairID, "", value.ExposureID, value.AsPayload(), true
	case *TrainingSliceManifest:
		if value == nil {
			return "", "", "", "", nil, false
		}
		return "training_slice_manifest", value.TrainingSliceManifestID, "", value.ContextScope, value.AsPayload(), true
	}
	return "", "", "", "", nil, false
}

// OutcomeArtifactIdentity 工件的 (event_type, artifact_id)（:924-926）。
func OutcomeArtifactIdentity(artifact any) (string, string) {
	eventType, artifactID, _, _, _, ok := outcomeStorageFields(artifact)
	if !ok {
		return "", ""
	}
	return eventType, artifactID
}

// versionRefsOf 卡片版本引用（:981-996 _version_refs）。
func versionRefsOf(card map[string]any) map[string]string {
	refFields := []string{"profile_semantic_version", "short_intent_version",
		"generation_config_version", "corpus_version", "document_snapshot_version",
		"retrieval_config_version", "evidence_extractor_version", "list_search_version"}
	result := map[string]string{}
	for _, field := range refFields {
		value, present := card[field]
		if !present || value == nil {
			continue
		}
		text := pyStr(value)
		if strings.TrimSpace(text) != "" {
			result[field] = text
		}
	}
	return result
}

// componentRequestIDsOf 组件调用血缘（:1003-1016 _component_request_ids）。
func componentRequestIDsOf(card map[string]any) [][2]string {
	invocations, ok := card["component_invocation_receipts"].([]any)
	if !ok {
		return nil
	}
	var result [][2]string
	for _, invocationValue := range invocations {
		invocation, ok := invocationValue.(map[string]any)
		if !ok {
			return nil
		}
		componentID, cOK := invocation["component_id"].(string)
		requestID, rOK := invocation["request_id"].(string)
		if !cOK || strings.TrimSpace(componentID) == "" || !rOK || strings.TrimSpace(requestID) == "" {
			return nil
		}
		result = append(result, [2]string{componentID, requestID})
	}
	seen := map[string]bool{}
	for _, pair := range result {
		if seen[pair[0]] {
			return nil
		}
		seen[pair[0]] = true
	}
	return result
}

// sortScopedItems 排序 (completed_at, summary_id, user_id)。
func sortScopedItems(items []scopedSummary) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].summary.CompletedAt != items[j].summary.CompletedAt {
			return items[i].summary.CompletedAt < items[j].summary.CompletedAt
		}
		if items[i].summary.SessionSummaryID != items[j].summary.SessionSummaryID {
			return items[i].summary.SessionSummaryID < items[j].summary.SessionSummaryID
		}
		return items[i].userID < items[j].userID
	})
}

// scopedSummary 工件-用户配对（collector 内部用）。
type scopedSummary struct {
	summary *SessionSummaryArtifact
	userID  string
}

// ──────────────────────────── ⤴️ 31c 交点前工件类型（topic_outcomes.py:106-836） ────────────────────────────

// RevisitPair 回访对（topic_outcomes.py:106-148）。
type RevisitPair struct {
	RevisitPairID    string  `json:"revisit_pair_id"`
	EarlierSummaryID string  `json:"earlier_summary_id"`
	LaterSummaryID   string  `json:"later_summary_id"`
	EarlierSessionID string  `json:"earlier_session_id"`
	LaterSessionID   string  `json:"later_session_id"`
	ContextScope     string  `json:"context_scope"`
	MatchLabel       bool    `json:"match_label"`
	MatcherID        string  `json:"matcher_id"`
	MatcherVersion   string  `json:"matcher_version"`
	NaturalEligible  bool    `json:"natural_eligible"`
	ExclusionReason  any     `json:"exclusion_reason"`
	ObservedAt       float64 `json:"observed_at"`
	SchemaVersion    string  `json:"schema_version"`
}

// validate 契约校验（对齐 __post_init__ :122-148，条件顺序逐条对应）。
func (p *RevisitPair) validate() error {
	for _, value := range []string{p.RevisitPairID, p.EarlierSummaryID, p.LaterSummaryID,
		p.EarlierSessionID, p.LaterSessionID, p.MatcherID, p.MatcherVersion, p.SchemaVersion} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("revisit pair contract failed")
		}
	}
	if !validOutcomeScope(p.ContextScope) {
		return fmt.Errorf("revisit pair contract failed")
	}
	if _, ok := p.ExclusionReason.(bool); ok {
		return fmt.Errorf("revisit pair contract failed")
	}
	if !finiteTimestamp(p.ObservedAt) {
		return fmt.Errorf("revisit pair contract failed")
	}
	if p.NaturalEligible && p.ExclusionReason != nil {
		return fmt.Errorf("revisit pair contract failed")
	}
	if !p.NaturalEligible && !nonEmptyStr(p.ExclusionReason) {
		return fmt.Errorf("revisit pair contract failed")
	}
	return nil
}

// AsPayload 载荷（:150-151）。
func (p *RevisitPair) AsPayload() map[string]any {
	return map[string]any{
		"revisit_pair_id": p.RevisitPairID, "earlier_summary_id": p.EarlierSummaryID,
		"later_summary_id": p.LaterSummaryID, "earlier_session_id": p.EarlierSessionID,
		"later_session_id": p.LaterSessionID, "context_scope": p.ContextScope,
		"match_label": p.MatchLabel, "matcher_id": p.MatcherID,
		"matcher_version": p.MatcherVersion, "natural_eligible": p.NaturalEligible,
		"exclusion_reason": p.ExclusionReason, "observed_at": p.ObservedAt,
		"schema_version": p.SchemaVersion,
	}
}

// RevisitPairFromPayload 严格回读（:153-159）。
func RevisitPairFromPayload(payload any) (*RevisitPair, error) {
	root, ok := payload.(map[string]any)
	if !ok || !mapKeysEqual(root, []string{"revisit_pair_id", "earlier_summary_id",
		"later_summary_id", "earlier_session_id", "later_session_id", "context_scope",
		"match_label", "matcher_id", "matcher_version", "natural_eligible",
		"exclusion_reason", "observed_at", "schema_version"}) ||
		pyStr(root["schema_version"]) != RevisitPairSchemaVersion {
		return nil, fmt.Errorf("artifact payload contract failed")
	}
	pair := &RevisitPair{
		RevisitPairID: pyStr(root["revisit_pair_id"]), EarlierSummaryID: pyStr(root["earlier_summary_id"]),
		LaterSummaryID: pyStr(root["later_summary_id"]), EarlierSessionID: pyStr(root["earlier_session_id"]),
		LaterSessionID: pyStr(root["later_session_id"]), ContextScope: pyStr(root["context_scope"]),
		MatchLabel: truthy(root["match_label"]), MatcherID: pyStr(root["matcher_id"]),
		MatcherVersion: pyStr(root["matcher_version"]), NaturalEligible: truthy(root["natural_eligible"]),
		ExclusionReason: root["exclusion_reason"], ObservedAt: toFloatOr(root["observed_at"], -1),
		SchemaVersion: pyStr(root["schema_version"]),
	}
	if err := pair.validate(); err != nil {
		return nil, err
	}
	return pair, nil
}

// RIDUserContextSnapshot RID 请求的原始上下文（topic_outcomes.py:203-249）。
type RIDUserContextSnapshot struct {
	UserHistorySnapshotID string         `json:"user_history_snapshot_id"`
	ContextScope          string         `json:"context_scope"`
	CutoffAt              float64        `json:"cutoff_at"`
	RequestPayload        map[string]any `json:"request_payload"`
	SchemaVersion         string         `json:"schema_version"`
}

// validate 契约校验（对齐 __post_init__ :213-242）。
func (s *RIDUserContextSnapshot) validate() error {
	request := s.RequestPayload
	if strings.TrimSpace(s.UserHistorySnapshotID) == "" ||
		!validOutcomeScope(s.ContextScope) || !finiteTimestamp(s.CutoffAt) ||
		s.SchemaVersion != RIDUserContextSchemaVersion || request == nil ||
		!nonEmptyStr(request["input_snapshot_id"]) || !isMapValue(request["user_profile"]) ||
		!isMapValue(request["binding"]) || !nonEmptyStr(request["activity_group"]) ||
		!nonEmptyStr(request["threshold_version"]) || anyListOf(request["session_summaries"]) == nil ||
		!isMapValue(request["decision_context"]) {
		return fmt.Errorf("RID user context contract failed")
	}
	decisionContext := mapAnyOf(request["decision_context"])
	if toFloatOr(decisionContext["decision_time"], -1) != s.CutoffAt {
		return fmt.Errorf("RID user context contract failed")
	}
	if !nonEmptyStr(decisionContext["activity_group_method"]) {
		return fmt.Errorf("RID user context contract failed")
	}
	for _, payloadValue := range anyListOf(request["session_summaries"]) {
		summary, err := SessionSummaryArtifactFromPayload(payloadValue)
		if err != nil {
			return fmt.Errorf("RID user context history after cutoff or ineligible")
		}
		if summary.ContextScope != s.ContextScope || !summary.NaturalRevisitEligible() ||
			summary.CompletedAt > s.CutoffAt {
			return fmt.Errorf("RID user context history after cutoff or ineligible")
		}
	}
	return nil
}

// AsPayload 载荷（:244-245）。
func (s *RIDUserContextSnapshot) AsPayload() map[string]any {
	return map[string]any{
		"user_history_snapshot_id": s.UserHistorySnapshotID,
		"context_scope":            s.ContextScope,
		"cutoff_at":                s.CutoffAt,
		"request_payload":          s.RequestPayload,
		"schema_version":           s.SchemaVersion,
	}
}

// RIDUserContextSnapshotFromPayload 严格回读（:247-249）。
func RIDUserContextSnapshotFromPayload(payload any) (*RIDUserContextSnapshot, error) {
	root, ok := payload.(map[string]any)
	if !ok || !mapKeysEqual(root, []string{"user_history_snapshot_id", "context_scope",
		"cutoff_at", "request_payload", "schema_version"}) ||
		pyStr(root["schema_version"]) != RIDUserContextSchemaVersion {
		return nil, fmt.Errorf("artifact payload contract failed")
	}
	snapshot := &RIDUserContextSnapshot{
		UserHistorySnapshotID: pyStr(root["user_history_snapshot_id"]),
		ContextScope:          pyStr(root["context_scope"]),
		CutoffAt:              toFloatOr(root["cutoff_at"], -1),
		RequestPayload:        mapAnyOf(root["request_payload"]),
		SchemaVersion:         pyStr(root["schema_version"]),
	}
	if err := snapshot.validate(); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// RIDTrainingTuple RID 训练元组（topic_outcomes.py:252-306）。
type RIDTrainingTuple struct {
	RIDTrainingTupleID              string   `json:"rid_training_tuple_id"`
	CorpusBatchID                   string   `json:"corpus_batch_id"`
	UserHistorySnapshotID           string   `json:"user_history_snapshot_id"`
	UserID                          string   `json:"user_id"`
	ContextScope                    string   `json:"context_scope"`
	PositiveRevisitPairID           string   `json:"positive_revisit_pair_id"`
	PositiveSummaryID               string   `json:"positive_summary_id"`
	PositiveWitnessSummaryID        string   `json:"positive_witness_summary_id"`
	IntraInterestNegativeSummaryIDs []string `json:"intra_interest_negative_summary_ids"`
	InterInterestNegativeSummaryIDs []string `json:"inter_interest_negative_summary_ids"`
	MatcherID                       string   `json:"matcher_id"`
	MatcherVersion                  string   `json:"matcher_version"`
	SchemaVersion                   string   `json:"schema_version"`
}

// validate 契约校验（对齐 __post_init__ :268-306）。
func (t *RIDTrainingTuple) validate() error {
	for _, value := range []string{t.RIDTrainingTupleID, t.CorpusBatchID,
		t.UserHistorySnapshotID, t.UserID, t.PositiveRevisitPairID, t.PositiveSummaryID,
		t.PositiveWitnessSummaryID, t.MatcherID, t.MatcherVersion, t.SchemaVersion} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("RID training tuple contract failed")
		}
	}
	if !validOutcomeScope(t.ContextScope) || len(t.IntraInterestNegativeSummaryIDs) == 0 ||
		len(t.InterInterestNegativeSummaryIDs) == 0 ||
		t.PositiveSummaryID == t.PositiveWitnessSummaryID {
		return fmt.Errorf("RID training tuple contract failed")
	}
	intraSet := map[string]bool{}
	for _, id := range t.IntraInterestNegativeSummaryIDs {
		if strings.TrimSpace(id) == "" || intraSet[id] || id == t.PositiveSummaryID {
			return fmt.Errorf("RID training tuple contract failed")
		}
		intraSet[id] = true
	}
	interSet := map[string]bool{}
	for _, id := range t.InterInterestNegativeSummaryIDs {
		if strings.TrimSpace(id) == "" || interSet[id] || id == t.PositiveSummaryID {
			return fmt.Errorf("RID training tuple contract failed")
		}
		interSet[id] = true
	}
	for id := range intraSet {
		if interSet[id] {
			return fmt.Errorf("RID training tuple contract failed")
		}
	}
	return nil
}

// AsPayload 载荷（:308-327）。
func (t *RIDTrainingTuple) AsPayload() map[string]any {
	return map[string]any{
		"rid_training_tuple_id": t.RIDTrainingTupleID, "corpus_batch_id": t.CorpusBatchID,
		"user_history_snapshot_id": t.UserHistorySnapshotID, "user_id": t.UserID,
		"context_scope": t.ContextScope, "positive_revisit_pair_id": t.PositiveRevisitPairID,
		"positive_summary_id":                 t.PositiveSummaryID,
		"positive_witness_summary_id":         t.PositiveWitnessSummaryID,
		"intra_interest_negative_summary_ids": stringsToAny(t.IntraInterestNegativeSummaryIDs),
		"inter_interest_negative_summary_ids": stringsToAny(t.InterInterestNegativeSummaryIDs),
		"matcher_id":                          t.MatcherID, "matcher_version": t.MatcherVersion,
		"schema_version": t.SchemaVersion,
	}
}

// RIDTrainingTupleFromPayload 严格回读（:329-339）。
func RIDTrainingTupleFromPayload(payload any) (*RIDTrainingTuple, error) {
	root, ok := payload.(map[string]any)
	if !ok || !mapKeysEqual(root, []string{"rid_training_tuple_id", "corpus_batch_id",
		"user_history_snapshot_id", "user_id", "context_scope", "positive_revisit_pair_id",
		"positive_summary_id", "positive_witness_summary_id",
		"intra_interest_negative_summary_ids", "inter_interest_negative_summary_ids",
		"matcher_id", "matcher_version", "schema_version"}) ||
		pyStr(root["schema_version"]) != RIDTrainingSchemaVersion {
		return nil, fmt.Errorf("artifact payload contract failed")
	}
	tuple := &RIDTrainingTuple{
		RIDTrainingTupleID: pyStr(root["rid_training_tuple_id"]), CorpusBatchID: pyStr(root["corpus_batch_id"]),
		UserHistorySnapshotID: pyStr(root["user_history_snapshot_id"]), UserID: pyStr(root["user_id"]),
		ContextScope: pyStr(root["context_scope"]), PositiveRevisitPairID: pyStr(root["positive_revisit_pair_id"]),
		PositiveSummaryID:               pyStr(root["positive_summary_id"]),
		PositiveWitnessSummaryID:        pyStr(root["positive_witness_summary_id"]),
		IntraInterestNegativeSummaryIDs: anyListToStrings(root["intra_interest_negative_summary_ids"]),
		InterInterestNegativeSummaryIDs: anyListToStrings(root["inter_interest_negative_summary_ids"]),
		MatcherID:                       pyStr(root["matcher_id"]),
		MatcherVersion:                  pyStr(root["matcher_version"]),
		SchemaVersion:                   pyStr(root["schema_version"]),
	}
	if err := tuple.validate(); err != nil {
		return nil, err
	}
	return tuple, nil
}

// PreferencePair 偏好对（topic_outcomes.py:389-439）。
type PreferencePair struct {
	PreferencePairID         string   `json:"preference_pair_id"`
	ContextScope             string   `json:"context_scope"`
	ManifestID               string   `json:"manifest_id"`
	ExposureID               string   `json:"exposure_id"`
	PreferredCardRevisionIDs []string `json:"preferred_card_revision_ids"`
	NegativeCardRevisionID   string   `json:"negative_card_revision_id"`
	NegativeType             string   `json:"negative_type"`
	RankerVersion            string   `json:"ranker_version"`
	DiversityVersion         string   `json:"diversity_version"`
	GeneratorVersion         string   `json:"generator_version"`
	CreatedAt                float64  `json:"created_at"`
	SchemaVersion            string   `json:"schema_version"`
}

// validate 契约校验（对齐 __post_init__ :404-439）。
func (p *PreferencePair) validate() error {
	for _, value := range []string{p.PreferencePairID, p.ManifestID, p.ExposureID,
		p.NegativeCardRevisionID, p.RankerVersion, p.DiversityVersion,
		p.GeneratorVersion, p.SchemaVersion} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("preference pair contract failed")
		}
	}
	if !validOutcomeScope(p.ContextScope) || len(p.PreferredCardRevisionIDs) == 0 {
		return fmt.Errorf("preference pair contract failed")
	}
	for _, revisionID := range p.PreferredCardRevisionIDs {
		if strings.TrimSpace(revisionID) == "" || revisionID == p.NegativeCardRevisionID {
			return fmt.Errorf("preference pair contract failed")
		}
	}
	switch p.NegativeType {
	case "utility_neg", "diversity_neg", "joint_failure_neg", "not_interested":
	default:
		return fmt.Errorf("preference pair contract failed")
	}
	if !finiteTimestamp(p.CreatedAt) {
		return fmt.Errorf("preference pair contract failed")
	}
	return nil
}

// AsPayload 载荷（:441-455）。
func (p *PreferencePair) AsPayload() map[string]any {
	return map[string]any{
		"preference_pair_id": p.PreferencePairID, "context_scope": p.ContextScope,
		"manifest_id": p.ManifestID, "exposure_id": p.ExposureID,
		"preferred_card_revision_ids": stringsToAny(p.PreferredCardRevisionIDs),
		"negative_card_revision_id":   p.NegativeCardRevisionID, "negative_type": p.NegativeType,
		"ranker_version": p.RankerVersion, "diversity_version": p.DiversityVersion,
		"generator_version": p.GeneratorVersion, "created_at": p.CreatedAt,
		"schema_version": p.SchemaVersion,
	}
}

// PreferencePairFromPayload 严格回读（:457-464）。
func PreferencePairFromPayload(payload any) (*PreferencePair, error) {
	root, ok := payload.(map[string]any)
	if !ok || !mapKeysEqual(root, []string{"preference_pair_id", "context_scope",
		"manifest_id", "exposure_id", "preferred_card_revision_ids",
		"negative_card_revision_id", "negative_type", "ranker_version",
		"diversity_version", "generator_version", "created_at", "schema_version"}) ||
		pyStr(root["schema_version"]) != PreferencePairSchemaVersion {
		return nil, fmt.Errorf("artifact payload contract failed")
	}
	pair := &PreferencePair{
		PreferencePairID: pyStr(root["preference_pair_id"]), ContextScope: pyStr(root["context_scope"]),
		ManifestID: pyStr(root["manifest_id"]), ExposureID: pyStr(root["exposure_id"]),
		PreferredCardRevisionIDs: anyListToStrings(root["preferred_card_revision_ids"]),
		NegativeCardRevisionID:   pyStr(root["negative_card_revision_id"]),
		NegativeType:             pyStr(root["negative_type"]),
		RankerVersion:            pyStr(root["ranker_version"]), DiversityVersion: pyStr(root["diversity_version"]),
		GeneratorVersion: pyStr(root["generator_version"]),
		CreatedAt:        toFloatOr(root["created_at"], -1),
		SchemaVersion:    pyStr(root["schema_version"]),
	}
	if err := pair.validate(); err != nil {
		return nil, err
	}
	return pair, nil
}

// TrainingSliceManifest 训练切片清单（topic_outcomes.py:467-499）。
type TrainingSliceManifest struct {
	TrainingSliceManifestID string             `json:"training_slice_manifest_id"`
	WindowStart             float64            `json:"window_start"`
	WindowEnd               float64            `json:"window_end"`
	LabelHorizon            float64            `json:"label_horizon"`
	Filters                 map[string]any     `json:"filters"`
	ContextScope            string             `json:"context_scope"`
	InputArtifactVersions   map[string]string  `json:"input_artifact_versions"`
	IncludedArtifactIDs     []string           `json:"included_artifact_ids"`
	ExcludedArtifactReasons map[string]string  `json:"excluded_artifact_reasons"`
	OfflineMetrics          map[string]float64 `json:"offline_metrics"`
	SchemaVersion           string             `json:"schema_version"`
}

// validate 契约校验（对齐 __post_init__ :481-499）。
func (m *TrainingSliceManifest) validate() error {
	if strings.TrimSpace(m.TrainingSliceManifestID) == "" || !validOutcomeScope(m.ContextScope) ||
		!finiteTimestamp(m.WindowStart) || !finiteTimestamp(m.WindowEnd) ||
		!finiteTimestamp(m.LabelHorizon) || m.WindowEnd <= m.WindowStart ||
		!(m.WindowStart <= m.LabelHorizon && m.LabelHorizon <= m.WindowEnd) ||
		len(m.InputArtifactVersions) == 0 {
		return fmt.Errorf("training slice contract failed")
	}
	for key, value := range m.InputArtifactVersions {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return fmt.Errorf("training slice contract failed")
		}
	}
	return nil
}

// AsPayload 载荷（:501-514）。
func (m *TrainingSliceManifest) AsPayload() map[string]any {
	filters := map[string]any{}
	for key, value := range m.Filters {
		filters[key] = value
	}
	versions := map[string]any{}
	for key, value := range m.InputArtifactVersions {
		versions[key] = value
	}
	metrics := map[string]any{}
	for key, value := range m.OfflineMetrics {
		metrics[key] = value
	}
	reasons := map[string]any{}
	for key, value := range m.ExcludedArtifactReasons {
		reasons[key] = value
	}
	return map[string]any{
		"training_slice_manifest_id": m.TrainingSliceManifestID,
		"window_start":               m.WindowStart, "window_end": m.WindowEnd,
		"label_horizon": m.LabelHorizon, "filters": filters,
		"context_scope": m.ContextScope, "input_artifact_versions": versions,
		"included_artifact_ids":     stringsToAny(m.IncludedArtifactIDs),
		"excluded_artifact_reasons": reasons, "offline_metrics": metrics,
		"schema_version": m.SchemaVersion,
	}
}

// TrainingSliceManifestFromPayload 严格回读（:516-523）。
func TrainingSliceManifestFromPayload(payload any) (*TrainingSliceManifest, error) {
	root, ok := payload.(map[string]any)
	if !ok || !mapKeysEqual(root, []string{"training_slice_manifest_id", "window_start",
		"window_end", "label_horizon", "filters", "context_scope",
		"input_artifact_versions", "included_artifact_ids",
		"excluded_artifact_reasons", "offline_metrics", "schema_version"}) ||
		pyStr(root["schema_version"]) != TrainingSliceSchemaVersion {
		return nil, fmt.Errorf("artifact payload contract failed")
	}
	versions := map[string]string{}
	for key, value := range mapAnyOf(root["input_artifact_versions"]) {
		versions[key] = pyStr(value)
	}
	reasons := map[string]string{}
	for key, value := range mapAnyOf(root["excluded_artifact_reasons"]) {
		reasons[key] = pyStr(value)
	}
	metrics := map[string]float64{}
	for key, value := range mapAnyOf(root["offline_metrics"]) {
		f, ok := toFloat(value)
		if !ok {
			return nil, fmt.Errorf("artifact payload contract failed")
		}
		metrics[key] = f
	}
	manifest := &TrainingSliceManifest{
		TrainingSliceManifestID: pyStr(root["training_slice_manifest_id"]),
		WindowStart:             toFloatOr(root["window_start"], -1),
		WindowEnd:               toFloatOr(root["window_end"], -1),
		LabelHorizon:            toFloatOr(root["label_horizon"], -1),
		Filters:                 mapAnyOf(root["filters"]),
		ContextScope:            pyStr(root["context_scope"]),
		InputArtifactVersions:   versions,
		IncludedArtifactIDs:     anyListToStrings(root["included_artifact_ids"]),
		ExcludedArtifactReasons: reasons,
		OfflineMetrics:          metrics,
		SchemaVersion:           pyStr(root["schema_version"]),
	}
	if err := manifest.validate(); err != nil {
		return nil, err
	}
	return manifest, nil
}

// StarterOutcome 起始结果（topic_outcomes.py:573-657）。
type StarterOutcome struct {
	OutcomeID                      string            `json:"outcome_id"`
	UserStateSnapshotID            string            `json:"user_state_snapshot_id"`
	ManifestID                     string            `json:"manifest_id"`
	GenerationBatchID              string            `json:"generation_batch_id"`
	ExposureID                     string            `json:"exposure_id"`
	ExposurePosition               int               `json:"exposure_position"`
	CardRevisionID                 string            `json:"card_revision_id"`
	ContextScope                   string            `json:"context_scope"`
	ProjectID                      any               `json:"project_id"`
	Shown                          bool              `json:"shown"`
	Clicked                        bool              `json:"clicked"`
	NotInterested                  bool              `json:"not_interested"`
	Started                        bool              `json:"started"`
	StartedSessionID               any               `json:"started_session_id"`
	ConversationDepth              int               `json:"conversation_depth"`
	FutureActivityObserved         bool              `json:"future_activity_observed"`
	FutureNaturalActivity          bool              `json:"future_natural_activity"`
	FuturePAActiveActivity         bool              `json:"future_pa_active_activity"`
	FutureActivityExclusionReasons []string          `json:"future_activity_exclusion_reasons"`
	OutcomeLabel                   string            `json:"outcome_label"`
	ExposedAt                      float64           `json:"exposed_at"`
	ClickedAt                      any               `json:"clicked_at"`
	NotInterestedAt                any               `json:"not_interested_at"`
	StartedAt                      any               `json:"started_at"`
	LabelHorizon                   float64           `json:"label_horizon"`
	VersionRefs                    map[string]string `json:"version_refs"`
	ComponentRequestIDs            [][2]string       `json:"component_request_ids"`
	ProviderResponseIDs            []string          `json:"provider_response_ids"`
	SearchReceipt                  any               `json:"search_receipt"`
	SchemaVersion                  string            `json:"schema_version"`
}

// validate 契约校验（对齐 __post_init__ :606-657）。
func (o *StarterOutcome) validate() error {
	for _, value := range []string{o.OutcomeID, o.UserStateSnapshotID, o.ManifestID,
		o.GenerationBatchID, o.ExposureID, o.CardRevisionID, o.OutcomeLabel, o.SchemaVersion} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("starter outcome contract failed")
		}
	}
	if !validOutcomeScope(o.ContextScope) {
		return fmt.Errorf("starter outcome contract failed")
	}
	if len(o.ProviderResponseIDs) == 0 {
		return fmt.Errorf("starter outcome contract failed")
	}
	providerSet := map[string]bool{}
	for _, id := range o.ProviderResponseIDs {
		if strings.TrimSpace(id) == "" || providerSet[id] {
			return fmt.Errorf("starter outcome contract failed")
		}
		providerSet[id] = true
	}
	// ⤴️ 31c：search_receipt 校验随 valid_search_receipt（topic_product.py:811）落地接入
	if len(o.ComponentRequestIDs) == 0 {
		return fmt.Errorf("starter outcome contract failed")
	}
	seen := map[string]bool{}
	for _, pair := range o.ComponentRequestIDs {
		if strings.TrimSpace(pair[0]) == "" || strings.TrimSpace(pair[1]) == "" || seen[pair[0]] {
			return fmt.Errorf("starter outcome contract failed")
		}
		seen[pair[0]] = true
	}
	if o.ExposurePosition < 1 || o.ConversationDepth < 0 {
		return fmt.Errorf("starter outcome contract failed")
	}
	if !finiteTimestamp(o.ExposedAt) || !finiteTimestamp(o.LabelHorizon) ||
		o.LabelHorizon < o.ExposedAt {
		return fmt.Errorf("starter outcome contract failed")
	}
	for _, value := range []any{o.ClickedAt, o.NotInterestedAt, o.StartedAt} {
		if value != nil && !finiteTimestamp(value) {
			return fmt.Errorf("starter outcome contract failed")
		}
	}
	return nil
}

// AsPayload 载荷（:659-695）。
func (o *StarterOutcome) AsPayload() map[string]any {
	componentPairs := make([]any, 0, len(o.ComponentRequestIDs))
	for _, pair := range o.ComponentRequestIDs {
		componentPairs = append(componentPairs, []any{pair[0], pair[1]})
	}
	return map[string]any{
		"outcome_id": o.OutcomeID, "user_state_snapshot_id": o.UserStateSnapshotID,
		"manifest_id": o.ManifestID, "generation_batch_id": o.GenerationBatchID,
		"exposure_id": o.ExposureID, "exposure_position": o.ExposurePosition,
		"card_revision_id": o.CardRevisionID, "context_scope": o.ContextScope,
		"project_id": o.ProjectID, "shown": o.Shown, "clicked": o.Clicked,
		"not_interested": o.NotInterested, "started": o.Started,
		"started_session_id":                o.StartedSessionID,
		"conversation_depth":                o.ConversationDepth,
		"future_activity_observed":          o.FutureActivityObserved,
		"future_natural_activity":           o.FutureNaturalActivity,
		"future_pa_active_activity":         o.FuturePAActiveActivity,
		"future_activity_exclusion_reasons": stringsToAny(o.FutureActivityExclusionReasons),
		"outcome_label":                     o.OutcomeLabel, "exposed_at": o.ExposedAt,
		"clicked_at": o.ClickedAt, "not_interested_at": o.NotInterestedAt,
		"started_at": o.StartedAt, "label_horizon": o.LabelHorizon,
		"version_refs":          mapOfStringsToAny(o.VersionRefs),
		"component_request_ids": componentPairs,
		"provider_response_ids": stringsToAny(o.ProviderResponseIDs),
		"search_receipt":        o.SearchReceipt,
		"schema_version":        o.SchemaVersion,
	}
}

// StarterOutcomeFromPayload 严格回读（:697-705）。
func StarterOutcomeFromPayload(payload any) (*StarterOutcome, error) {
	root, ok := payload.(map[string]any)
	expectedKeys := []string{"outcome_id", "user_state_snapshot_id", "manifest_id",
		"generation_batch_id", "exposure_id", "exposure_position", "card_revision_id",
		"context_scope", "project_id", "shown", "clicked", "not_interested", "started",
		"started_session_id", "conversation_depth", "future_activity_observed",
		"future_natural_activity", "future_pa_active_activity",
		"future_activity_exclusion_reasons", "outcome_label", "exposed_at",
		"clicked_at", "not_interested_at", "started_at", "label_horizon",
		"version_refs", "component_request_ids", "provider_response_ids",
		"search_receipt", "schema_version"}
	if !ok || !mapKeysEqual(root, expectedKeys) ||
		pyStr(root["schema_version"]) != StarterOutcomeSchemaVersion {
		return nil, fmt.Errorf("artifact payload contract failed")
	}
	outcome := &StarterOutcome{
		OutcomeID:           pyStr(root["outcome_id"]),
		UserStateSnapshotID: pyStr(root["user_state_snapshot_id"]),
		ManifestID:          pyStr(root["manifest_id"]),
		GenerationBatchID:   pyStr(root["generation_batch_id"]),
		ExposureID:          pyStr(root["exposure_id"]),
		ExposurePosition:    intOf(root["exposure_position"]),
		CardRevisionID:      pyStr(root["card_revision_id"]),
		ContextScope:        pyStr(root["context_scope"]),
		ProjectID:           root["project_id"],
		Shown:               truthy(root["shown"]), Clicked: truthy(root["clicked"]),
		NotInterested: truthy(root["not_interested"]), Started: truthy(root["started"]),
		StartedSessionID:               root["started_session_id"],
		ConversationDepth:              intOf(root["conversation_depth"]),
		FutureActivityObserved:         truthy(root["future_activity_observed"]),
		FutureNaturalActivity:          truthy(root["future_natural_activity"]),
		FuturePAActiveActivity:         truthy(root["future_pa_active_activity"]),
		FutureActivityExclusionReasons: anyListToStrings(root["future_activity_exclusion_reasons"]),
		OutcomeLabel:                   pyStr(root["outcome_label"]),
		ExposedAt:                      toFloatOr(root["exposed_at"], -1),
		ClickedAt:                      root["clicked_at"], NotInterestedAt: root["not_interested_at"],
		StartedAt:           root["started_at"],
		LabelHorizon:        toFloatOr(root["label_horizon"], -1),
		ComponentRequestIDs: componentPairsFromAny(root["component_request_ids"]),
		ProviderResponseIDs: anyListToStrings(root["provider_response_ids"]),
		SearchReceipt:       root["search_receipt"],
		SchemaVersion:       pyStr(root["schema_version"]),
	}
	versionRefs := map[string]string{}
	for key, value := range mapAnyOf(root["version_refs"]) {
		versionRefs[key] = pyStr(value)
	}
	outcome.VersionRefs = versionRefs
	if err := outcome.validate(); err != nil {
		return nil, err
	}
	return outcome, nil
}

// UtilityTrainingExample 效用训练样本（topic_outcomes.py:708-751）。
type UtilityTrainingExample struct {
	UtilityExampleID            string            `json:"utility_example_id"`
	OutcomeID                   string            `json:"outcome_id"`
	UserStateSnapshotID         string            `json:"user_state_snapshot_id"`
	ContextScope                string            `json:"context_scope"`
	LabelHorizon                float64           `json:"label_horizon"`
	Labels                      map[string]any    `json:"labels"`
	NaturalLaneEligible         bool              `json:"natural_lane_eligible"`
	PAActiveEligible            bool              `json:"pa_active_eligible"`
	NaturalLaneExclusionReasons []string          `json:"natural_lane_exclusion_reasons"`
	TrainingExclusionReason     *string           `json:"training_exclusion_reason"`
	FeatureSnapshot             map[string]any    `json:"feature_snapshot"`
	VersionRefs                 map[string]string `json:"version_refs"`
	SchemaVersion               string            `json:"schema_version"`
}

// validate 契约校验（对齐 __post_init__ :724-751）。
func (e *UtilityTrainingExample) validate() error {
	for _, value := range []string{e.UtilityExampleID, e.OutcomeID,
		e.UserStateSnapshotID, e.SchemaVersion} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("utility training example contract failed")
		}
	}
	if !validOutcomeScope(e.ContextScope) || !finiteTimestamp(e.LabelHorizon) ||
		e.Labels == nil {
		return fmt.Errorf("utility training example contract failed")
	}
	if e.FeatureSnapshot == nil && (e.TrainingExclusionReason == nil || strings.TrimSpace(*e.TrainingExclusionReason) == "") {
		return fmt.Errorf("utility training example contract failed")
	}
	if e.FeatureSnapshot != nil && e.TrainingExclusionReason != nil {
		return fmt.Errorf("utility training example contract failed")
	}
	return nil
}

// AsPayload 载荷（:753-772）。
func (e *UtilityTrainingExample) AsPayload() map[string]any {
	labels := map[string]any{}
	for key, value := range e.Labels {
		labels[key] = value
	}
	reasons := stringsToAny(e.NaturalLaneExclusionReasons)
	versionRefs := mapOfStringsToAny(e.VersionRefs)
	var reasonValue any
	if e.TrainingExclusionReason != nil {
		reasonValue = *e.TrainingExclusionReason
	}
	var featureValue any
	if e.FeatureSnapshot != nil {
		featureCopy := map[string]any{}
		for key, value := range e.FeatureSnapshot {
			featureCopy[key] = value
		}
		featureValue = featureCopy
	}
	return map[string]any{
		"utility_example_id": e.UtilityExampleID, "outcome_id": e.OutcomeID,
		"user_state_snapshot_id": e.UserStateSnapshotID, "context_scope": e.ContextScope,
		"label_horizon": e.LabelHorizon, "labels": labels,
		"natural_lane_eligible": e.NaturalLaneEligible, "pa_active_eligible": e.PAActiveEligible,
		"natural_lane_exclusion_reasons": reasons,
		"training_exclusion_reason":      reasonValue,
		"feature_snapshot":               featureValue, "version_refs": versionRefs,
		"schema_version": e.SchemaVersion,
	}
}

// UtilityTrainingExampleFromPayload 严格回读（:774-781）。
func UtilityTrainingExampleFromPayload(payload any) (*UtilityTrainingExample, error) {
	root, ok := payload.(map[string]any)
	if !ok || !mapKeysEqual(root, []string{"utility_example_id", "outcome_id",
		"user_state_snapshot_id", "context_scope", "label_horizon", "labels",
		"natural_lane_eligible", "pa_active_eligible", "natural_lane_exclusion_reasons",
		"training_exclusion_reason", "feature_snapshot", "version_refs", "schema_version"}) ||
		pyStr(root["schema_version"]) != UtilityTrainingSchemaVersion {
		return nil, fmt.Errorf("artifact payload contract failed")
	}
	versionRefs := map[string]string{}
	for key, value := range mapAnyOf(root["version_refs"]) {
		versionRefs[key] = pyStr(value)
	}
	var reasonPtr *string
	if value, isString := root["training_exclusion_reason"].(string); isString {
		reasonPtr = &value
	}
	var feature map[string]any
	if value, isMap := root["feature_snapshot"].(map[string]any); isMap {
		feature = value
	}
	example := &UtilityTrainingExample{
		UtilityExampleID: pyStr(root["utility_example_id"]), OutcomeID: pyStr(root["outcome_id"]),
		UserStateSnapshotID:         pyStr(root["user_state_snapshot_id"]),
		ContextScope:                pyStr(root["context_scope"]),
		LabelHorizon:                toFloatOr(root["label_horizon"], -1),
		Labels:                      mapAnyOf(root["labels"]),
		NaturalLaneEligible:         truthy(root["natural_lane_eligible"]),
		PAActiveEligible:            truthy(root["pa_active_eligible"]),
		NaturalLaneExclusionReasons: anyListToStrings(root["natural_lane_exclusion_reasons"]),
		TrainingExclusionReason:     reasonPtr, FeatureSnapshot: feature,
		VersionRefs: versionRefs, SchemaVersion: pyStr(root["schema_version"]),
	}
	if err := example.validate(); err != nil {
		return nil, err
	}
	return example, nil
}

// stringsToAny []string → []any 载荷。
func stringsToAny(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

// anyListToStrings []any → []string。
func anyListToStrings(value any) []string {
	var result []string
	for _, item := range anyListOf(value) {
		result = append(result, pyStr(item))
	}
	return result
}

// mapOfStringsToAny map[string]string → map[string]any 载荷。
func mapOfStringsToAny(source map[string]string) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// componentPairsFromAny 载荷 → 组件请求对。
func componentPairsFromAny(value any) [][2]string {
	var result [][2]string
	for _, item := range anyListOf(value) {
		pair, ok := item.([]any)
		if !ok || len(pair) != 2 {
			return nil
		}
		result = append(result, [2]string{pyStr(pair[0]), pyStr(pair[1])})
	}
	return result
}

// ──────────────────────────── ⤴️ 31c 交点前：新页结果信号与队列（:1918-2022） ────────────────────────────

// NewPageOutcomeSignal 与展示修订绑定的接受/否定信号（topic_outcomes.py:1924-1990）。
type NewPageOutcomeSignal struct {
	Outcome             string      `json:"outcome"`
	ManifestID          string      `json:"manifest_id"`
	GenerationBatchID   string      `json:"generation_batch_id"`
	ExposureID          string      `json:"exposure_id"`
	CardRevisionID      string      `json:"card_revision_id"`
	ContextScope        string      `json:"context_scope"`
	ProjectID           any         `json:"project_id"`
	ComponentRequestIDs [][2]string `json:"component_request_ids"`
	ProviderResponseIDs []string    `json:"provider_response_ids"`
	SearchReceipt       any         `json:"search_receipt"`
	OccurredAt          float64     `json:"occurred_at"`
	SchemaVersion       string      `json:"schema_version"`
	HostWritten         bool        `json:"host_written"`
}

// NewNewPageOutcomeSignal 构造并校验（对齐 __post_init__ :1942-1990）。
func NewNewPageOutcomeSignal(outcome, manifestID, generationBatchID, exposureID,
	cardRevisionID, contextScope string, projectID any, componentRequestIDs [][2]string,
	providerResponseIDs []string, searchReceipt any, occurredAt float64,
	hostWritten bool) (*NewPageOutcomeSignal, error) {
	signal := &NewPageOutcomeSignal{
		Outcome: outcome, ManifestID: manifestID, GenerationBatchID: generationBatchID,
		ExposureID: exposureID, CardRevisionID: cardRevisionID,
		ContextScope: contextScope, ProjectID: projectID,
		ComponentRequestIDs: componentRequestIDs, ProviderResponseIDs: providerResponseIDs,
		SearchReceipt: searchReceipt, OccurredAt: occurredAt,
		SchemaVersion: OutcomeSignalSchemaVersion, HostWritten: hostWritten,
	}
	if signal.SchemaVersion != OutcomeSignalSchemaVersion {
		return nil, fmt.Errorf("unsupported outcome signal schema_version")
	}
	if outcome != "accepted" && outcome != "not_interested" {
		return nil, fmt.Errorf("outcome must be accepted or not_interested")
	}
	for _, value := range []string{manifestID, generationBatchID, exposureID, cardRevisionID, contextScope} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("outcome lineage IDs must be non-empty")
		}
	}
	expectedProjectID, err := ProjectIDForScope(contextScope)
	if err != nil {
		return nil, fmt.Errorf("invalid outcome context scope")
	}
	var expectedValue any
	if expectedProjectID != "" {
		expectedValue = expectedProjectID
	}
	if !anyEqual(signal.ProjectID, expectedValue) {
		return nil, fmt.Errorf("outcome project does not match context scope")
	}
	if math.IsInf(signal.OccurredAt, 0) || math.IsNaN(signal.OccurredAt) {
		return nil, fmt.Errorf("occurred_at must be finite")
	}
	if hostWritten {
		if len(signal.ComponentRequestIDs) != 0 || len(signal.ProviderResponseIDs) != 0 {
			return nil, fmt.Errorf("host-written outcome must not claim model provenance")
		}
		if signal.SearchReceipt != nil {
			return nil, fmt.Errorf("host-written outcome must not claim a search receipt")
		}
		return signal, nil
	}
	if len(signal.ComponentRequestIDs) == 0 {
		return nil, fmt.Errorf("component request provenance must be non-empty")
	}
	componentSet := map[string]bool{}
	for _, pair := range signal.ComponentRequestIDs {
		if strings.TrimSpace(pair[0]) == "" || strings.TrimSpace(pair[1]) == "" {
			return nil, fmt.Errorf("component request provenance must be non-empty")
		}
		if componentSet[pair[0]] {
			return nil, fmt.Errorf("component request provenance contains duplicates")
		}
		componentSet[pair[0]] = true
	}
	if len(signal.ProviderResponseIDs) == 0 {
		return nil, fmt.Errorf("provider response provenance must be non-empty")
	}
	providerSet := map[string]bool{}
	for _, id := range signal.ProviderResponseIDs {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("provider response provenance must be non-empty")
		}
		if providerSet[id] {
			return nil, fmt.Errorf("provider response provenance contains duplicates")
		}
		providerSet[id] = true
	}
	if signal.SearchReceipt != nil && !isMapValue(signal.SearchReceipt) {
		return nil, fmt.Errorf("search receipt provenance must be an object or null")
	}
	return signal, nil
}

// NewPageOutcomeQueue 在唯一生产 sink 挂接前排队的信号队列
// （topic_outcomes.py:1993-2021）。
type NewPageOutcomeQueue struct {
	mu      sync.Mutex
	pending []*NewPageOutcomeSignal
	sink    func(*NewPageOutcomeSignal) bool
}

// PendingCount 待投递数（:2000-2002）。
func (q *NewPageOutcomeQueue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Publish 投递一个信号（:2004-2008）。
func (q *NewPageOutcomeQueue) Publish(signal *NewPageOutcomeSignal) string {
	q.mu.Lock()
	if q.sink == nil {
		q.pending = append(q.pending, signal)
		q.mu.Unlock()
		return "queued"
	}
	sink := q.sink
	q.mu.Unlock()
	if sink(signal) {
		return "recorded"
	}
	return "rejected"
}

// AttachSink 仅挂接一次并按序同步排空挂接前快照（:2010-2021）。
// 已有 sink 返回 nil（Python None）。
func (q *NewPageOutcomeQueue) AttachSink(sink func(*NewPageOutcomeSignal) bool) []bool {
	q.mu.Lock()
	if q.sink != nil {
		q.mu.Unlock()
		return nil
	}
	q.sink = sink
	pending := q.pending
	q.pending = nil
	q.mu.Unlock()
	results := make([]bool, 0, len(pending))
	for _, signal := range pending {
		results = append(results, sink(signal))
	}
	return results
}
