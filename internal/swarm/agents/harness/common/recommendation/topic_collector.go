package recommendation

// topic_collector.go 一比一对齐上游 topic_collector.py（2151 行）：
// 从持久化会话历史回收终局话题起始结果。
// ⤴️31c/31d 接线标注：_default_sessions_loader（get_all_sessions_metadata）
// 与 history_loader（load_history_records）属 server 会话域，Go 以注入 loader
// 承载，默认空表（偏差登记于 spec §5，会话域落地后接线替换默认值）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultDepthQuiescenceSeconds 深度静默期（topic_collector.py:78）
	DefaultDepthQuiescenceSeconds = 24 * 60 * 60
	// DefaultLabelHorizonSeconds 标注视界（:79）
	DefaultLabelHorizonSeconds = 7 * 24 * 60 * 60
	// DefaultMaxBoundEvents 绑定事件页上限（:80）
	DefaultMaxBoundEvents = 2048
	// DefaultMaxExposures 曝光页上限（:81）
	DefaultMaxExposures = 2048
	// DefaultMaxSessions 会话上限（:82）
	DefaultMaxSessions = 10000
	// LocalUserID 本地用户 ID（:83）
	LocalUserID = "local"
	// FeatureSnapshotVersion 预标注特征快照版本（:84）
	FeatureSnapshotVersion = "topic-starter-v6-prelabel-feature-v1"
	// TrainingExclusionSchemaVersion 训练排除 schema 版本（:85）
	TrainingExclusionSchemaVersion = "topic-starter-v6-training-exclusion-v1"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OutcomeCollectionReport 一次收集的报告（topic_collector.py:100-116）。
type OutcomeCollectionReport struct {
	BoundSessionsScanned                         int
	DepthEventsRecorded                          int
	FutureActivityEventsRecorded                 int
	SessionSummaryArtifactsRecorded              int
	RevisitPairArtifactsRecorded                 int
	RIDTrainingTupleArtifactsRecorded            int
	ActiveExpressionObservationArtifactsRecorded int
	ActiveExpressionExampleArtifactsRecorded     int
	PassiveStarterExampleArtifactsRecorded       int
	PATrainingExampleArtifactsRecorded           int
	TrainingExclusionArtifactsRecorded           int
	StarterOutcomeArtifactsRecorded              int
	UtilityExampleArtifactsRecorded              int
	PreferencePairArtifactsRecorded              int
	TrainingSliceArtifactsRecorded               int
}

// TrainingExclusionRecord 一个真实产品产物不可训练的带版本原因
// （topic_collector.py:119-194）。
type TrainingExclusionRecord struct {
	ExclusionID         string  `json:"exclusion_id"`
	TrainingDomain      string  `json:"training_domain"`
	SourceArtifactID    string  `json:"source_artifact_id"`
	Reason              string  `json:"reason"`
	UserStateSnapshotID string  `json:"user_state_snapshot_id"`
	ContextScope        string  `json:"context_scope"`
	ManifestID          string  `json:"manifest_id"`
	ExposureID          string  `json:"exposure_id"`
	CreatedAt           float64 `json:"created_at"`
	SchemaVersion       string  `json:"schema_version"`
}

// NewTrainingExclusionRecord 构造并校验（对齐 __post_init__ :134-159）。
func NewTrainingExclusionRecord(exclusionID, trainingDomain, sourceArtifactID, reason,
	userStateSnapshotID, contextScope, manifestID, exposureID string, createdAt float64) (*TrainingExclusionRecord, error) {
	record := &TrainingExclusionRecord{
		ExclusionID: exclusionID, TrainingDomain: trainingDomain,
		SourceArtifactID: sourceArtifactID, Reason: reason,
		UserStateSnapshotID: userStateSnapshotID, ContextScope: contextScope,
		ManifestID: manifestID, ExposureID: exposureID,
		CreatedAt: createdAt, SchemaVersion: TrainingExclusionSchemaVersion,
	}
	if _, ok := map[string]bool{"rid": true, "pa_active": true, "pa_passive": true}[trainingDomain]; !ok {
		return nil, fmt.Errorf("training exclusion contract failed")
	}
	for _, value := range []string{exclusionID, sourceArtifactID, reason,
		userStateSnapshotID, manifestID, exposureID} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("training exclusion contract failed")
		}
	}
	if record.SchemaVersion != TrainingExclusionSchemaVersion ||
		finiteTimestamp(createdAt) == false || createdAt < 0 {
		return nil, fmt.Errorf("training exclusion contract failed")
	}
	if _, err := ProjectIDForScope(contextScope); err != nil {
		return nil, fmt.Errorf("training exclusion contract failed")
	}
	return record, nil
}

// ArtifactID 产物 ID（:161-163）。
func (r *TrainingExclusionRecord) ArtifactID() string { return r.ExclusionID }

// AsPayload 载荷（:165-177）。
func (r *TrainingExclusionRecord) AsPayload() map[string]any {
	return map[string]any{
		"exclusion_id": r.ExclusionID, "training_domain": r.TrainingDomain,
		"source_artifact_id": r.SourceArtifactID, "reason": r.Reason,
		"user_state_snapshot_id": r.UserStateSnapshotID,
		"context_scope":          r.ContextScope, "manifest_id": r.ManifestID,
		"exposure_id": r.ExposureID, "created_at": r.CreatedAt,
		"schema_version": r.SchemaVersion,
	}
}

// TrainingExclusionRecordFromPayload 严格回读（:179-194）。
func TrainingExclusionRecordFromPayload(payload any) (*TrainingExclusionRecord, error) {
	root, ok := payload.(map[string]any)
	if !ok || !mapKeysEqual(root, []string{"exclusion_id", "training_domain",
		"source_artifact_id", "reason", "user_state_snapshot_id", "context_scope",
		"manifest_id", "exposure_id", "created_at", "schema_version"}) {
		return nil, fmt.Errorf("training exclusion contract failed")
	}
	return NewTrainingExclusionRecord(
		pyStr(root["exclusion_id"]), pyStr(root["training_domain"]),
		pyStr(root["source_artifact_id"]), pyStr(root["reason"]),
		pyStr(root["user_state_snapshot_id"]), pyStr(root["context_scope"]),
		pyStr(root["manifest_id"]), pyStr(root["exposure_id"]),
		toFloatOr(root["created_at"], -1))
}

// ridCorpusMaterialization RID 语料物化结果（topic_collector.py:197-203）。
type ridCorpusMaterialization struct {
	summaryRecorded   int
	pairRecorded      int
	tupleRecorded     int
	exclusionRecorded int
	sliceRecorded     int
	labels            map[[2]string]*bool
}

// trainingPlaneMaterialization 训练面物化结果（:206-213）。
type trainingPlaneMaterialization struct {
	starterOutcomesRecorded        int
	utilityExamplesRecorded        int
	preferencePairsRecorded        int
	trainingSlicesRecorded         int
	passiveStarterExamplesRecorded int
	passivePAExamplesRecorded      int
	passiveExclusionsRecorded      int
}

// historySignal 单条历史信号（topic_collector.py:216-226）。
type historySignal struct {
	sessionID        string
	ordinal          int
	sourceEventID    string
	sourceLocator    string
	observedAt       float64
	inputOrigin      string
	assistanceOrigin string
	intentCluster    string
	hasIntentCluster bool
}

// TopicStarterOutcomeCollector 通过稳定血缘落实深度与后续活动标注
// （topic_collector.py:707-768）。
type TopicStarterOutcomeCollector struct {
	cancelCtx                 context.Context
	store                     *TopicStarterStore
	historyLoader             func(sessionID string) []map[string]any
	sessionsLoader            func() []map[string]any
	now                       func() float64
	depthQuiescenceSeconds    float64
	labelHorizonSeconds       float64
	maxBoundEvents            int
	maxExposures              int
	summaryPairMatcher        func(earlier, later *SessionSummaryArtifact) (bool, error)
	summaryPairMatcherID      string
	summaryPairMatcherVersion string
	summaryPairMatcherRefused bool
	// projectCompletedPrefix / projectCompletedPrefixes 会话完结前缀投影
	// （Python session_history.project_completed_session_prefix(es)，属会话域）
	// ⤴️31d 接线：nil 默认投影为空（无完结边界 → 无信号）。
	projectCompletedPrefix   func(sessionID string, records []map[string]any, allowGaps bool) *CompletedSessionProjection
	projectCompletedPrefixes func(sessionID string, records []map[string]any, allowGaps bool) []*CompletedSessionProjection
}

// CompletedSessionProjection 会话完结前缀投影最小形状
// （Python session_history.CompletedSessionPrefix；⤴️31d 归位会话域类型）。
type CompletedSessionProjection struct {
	// Boundary 完结边界
	Boundary *SessionCompletionBoundary
	// CompletedUserOrdinals 已完结用户请求序号
	CompletedUserOrdinals []int
}

// Equal 边界等值（Python CompletionBoundary __eq__ 语义）。
func (p *CompletedSessionProjection) Equal(other *SessionCompletionBoundary) bool {
	if p == nil || p.Boundary == nil || other == nil {
		return false
	}
	return anyEqual(p.Boundary.AsPayload(), other.AsPayload())
}

// NewTopicStarterOutcomeCollector 构造（对齐 __init__ :710-768）。
// historyLoader/sessionsLoader 为空时回退空表（⤴️31c/31d 会话域接线）。
func NewTopicStarterOutcomeCollector(ctx context.Context, store *TopicStarterStore,
	historyLoader func(string) []map[string]any,
	sessionsLoader func() []map[string]any,
	now func() float64,
	depthQuiescenceSeconds, labelHorizonSeconds float64,
	maxBoundEvents, maxExposures int,
	summaryPairMatcher func(earlier, later *SessionSummaryArtifact) (bool, error),
	summaryPairMatcherID, summaryPairMatcherVersion string) (*TopicStarterOutcomeCollector, error) {
	if depthQuiescenceSeconds < 0 || labelHorizonSeconds < 0 ||
		maxBoundEvents < 1 || maxExposures < 1 {
		return nil, fmt.Errorf("collector bounds must be non-negative")
	}
	matcherBound := summaryPairMatcher != nil
	idBound := strings.TrimSpace(summaryPairMatcherID) != ""
	versionBound := strings.TrimSpace(summaryPairMatcherVersion) != ""
	if !matcherBound {
		if idBound || versionBound {
			return nil, fmt.Errorf("summary pair matcher binding must be complete")
		}
	} else if !idBound || !versionBound {
		return nil, fmt.Errorf("summary pair matcher binding must be complete")
	}
	if now == nil {
		now = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	if historyLoader == nil {
		historyLoader = func(string) []map[string]any { return nil }
	}
	if sessionsLoader == nil {
		sessionsLoader = func() []map[string]any { return nil }
	}
	return &TopicStarterOutcomeCollector{
		cancelCtx: ctx, store: store,
		historyLoader: historyLoader, sessionsLoader: sessionsLoader, now: now,
		depthQuiescenceSeconds: depthQuiescenceSeconds,
		labelHorizonSeconds:    labelHorizonSeconds,
		maxBoundEvents:         maxBoundEvents, maxExposures: maxExposures,
		summaryPairMatcher:        summaryPairMatcher,
		summaryPairMatcherID:      strings.TrimSpace(summaryPairMatcherID),
		summaryPairMatcherVersion: strings.TrimSpace(summaryPairMatcherVersion),
	}, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// checkCancelled 取消检查（:770-772）。
func (c *TopicStarterOutcomeCollector) checkCancelled() error {
	if c.cancelCtx != nil && c.cancelCtx.Err() != nil {
		return c.cancelCtx.Err()
	}
	return nil
}

// RecordExclusion 记录一条训练排除（:774-810）。
func (c *TopicStarterOutcomeCollector) RecordExclusion(trainingDomain, sourceArtifactID, reason,
	userStateSnapshotID, contextScope, manifestID, exposureID, sessionID string, createdAt float64) (bool, error) {
	if err := c.checkCancelled(); err != nil {
		return false, err
	}
	record, err := NewTrainingExclusionRecord(
		fmt.Sprintf("training-exclusion:%s:%s:%s:%s",
			manifestID, exposureID, trainingDomain, sourceArtifactID),
		trainingDomain, sourceArtifactID, reason,
		userStateSnapshotID, contextScope, manifestID, exposureID, createdAt)
	if err != nil {
		return false, err
	}
	return persistTrainingEvent(c.store, "training_exclusion", record.ArtifactID(),
		record.AsPayload(), sessionID, exposureID, createdAt), nil
}

// matchSummaryPair 匹配一对摘要（:812-832）。
func (c *TopicStarterOutcomeCollector) matchSummaryPair(earlier, later *SessionSummaryArtifact) (*bool, string, error) {
	if err := c.checkCancelled(); err != nil {
		return nil, "", err
	}
	if c.summaryPairMatcher == nil || c.summaryPairMatcherRefused {
		return nil, "summary_pair_matcher_unavailable", nil
	}
	result, err := c.summaryPairMatcher(earlier, later)
	if err != nil {
		// 拒绝类失败拒绝本轮剩余配对
		c.summaryPairMatcherRefused = PROVIDER_REFUSALS[ProviderFailureReason(err)]
		return nil, "summary_pair_matcher_unavailable", nil
	}
	if err := c.checkCancelled(); err != nil {
		return nil, "", err
	}
	return &result, "", nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectorTimestamp 时间戳安全读取（:236-240）。
func collectorTimestamp(value any) *float64 {
	switch v := value.(type) {
	case int:
		f := float64(v)
		if math.IsInf(f, 0) || math.IsNaN(f) || f < 0 {
			return nil
		}
		return &f
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) || v < 0 {
			return nil
		}
		return &v
	default:
		return nil
	}
}

// sessionUserID 会话归属用户（:243-248）。
func sessionUserID(metadata any) string {
	root, isMap := metadata.(map[string]any)
	if !isMap {
		return ""
	}
	// Web chat 留空 user_id；本数据目录的会话属于其唯一本地用户
	if value, isString := root["user_id"].(string); isString && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return LocalUserID
}

// persistTrainingEvent 训练事件落库（:251-275）。
func persistTrainingEvent(store *TopicStarterStore, eventType, artifactID string,
	payload map[string]any, sessionID, exposureID string, createdAt float64) bool {
	if collectorTimestamp(createdAt) == nil {
		return false
	}
	recorded, _ := store.RecordEvent(map[string]any{
		"session_id": sessionID, "slate_id": exposureID, "event_type": eventType,
		"topic_id": artifactID, "policy_id": ProductPolicyID,
		"selection_mode": "training_data_plane", "propensity_reason": "not_applicable",
		"payload": payload, "created_at": createdAt,
	})
	return recorded
}

// originOf 来源回退（:278-279）。
func originOf(value any, fallback string) string {
	if text, isString := value.(string); isString && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	return fallback
}

// userRecords 用户记录枚举（:282-287）。
func userRecords(records []map[string]any) []struct {
	Ordinal int
	Record  map[string]any
} {
	result := []struct {
		Ordinal int
		Record  map[string]any
	}{}
	for ordinal, record := range records {
		if pyStr(record["role"]) == "user" {
			result = append(result, struct {
				Ordinal int
				Record  map[string]any
			}{ordinal, record})
		}
	}
	return result
}

// scopeMatches 元数据 scope 匹配（:290-295）。
func scopeMatches(metadata map[string]any, contextScope string) bool {
	expectedProjectID, err := ProjectIDForScope(contextScope)
	if err != nil {
		return false
	}
	actualProjectID := strings.TrimSpace(pyStr(metadata["project_id"]))
	if expectedProjectID == "" {
		return GlobalProjectBuckets[actualProjectID]
	}
	return actualProjectID == expectedProjectID
}

// exactManifest 精确清单载荷（:298-313）。
func exactManifest(store *TopicStarterStore, manifestID string) map[string]any {
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_slate_manifest", SlateID: &manifestID,
		PolicyID: &productPolicyIDPtr, Limit: 2, NewestFirst: true,
	})
	if err != nil || len(rows) != 1 {
		return nil
	}
	payload, isMap := rows[0]["payload"].(map[string]any)
	if !isMap || pyStr(payload["manifest_id"]) != manifestID {
		return nil
	}
	return payload
}

// manifestSessionSummaries 清单摘要工件（:316-329）。
func manifestSessionSummaries(manifest map[string]any) []*SessionSummaryArtifact {
	raw, isList := manifest["session_summary_artifacts"].([]any)
	if !isList {
		return nil
	}
	var summaries []*SessionSummaryArtifact
	seen := map[string]bool{}
	for _, item := range raw {
		summary, err := SessionSummaryArtifactFromPayload(item)
		if err != nil {
			return nil
		}
		if seen[summary.SessionSummaryID] {
			return nil
		}
		seen[summary.SessionSummaryID] = true
		summaries = append(summaries, summary)
	}
	return summaries
}

// exactExposedAt 精确曝光时间（:332-355）。
func exactExposedAt(store *TopicStarterStore, manifestID, exposureID string) *float64 {
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "dynamic_exposure", SlateID: &exposureID, TopicID: &manifestID,
		PolicyID: &productPolicyIDPtr, Limit: 2,
	})
	if err != nil {
		return nil
	}
	var matches []map[string]any
	for _, row := range rows {
		payload, isMap := row["payload"].(map[string]any)
		if !isMap {
			continue
		}
		if pyStr(payload["manifest_id"]) != manifestID {
			continue
		}
		// 对齐 Python payload.get("exposure_id", exposure_id)：缺键按 exposure_id 处理
		if value, present := payload["exposure_id"]; present && !anyEqual(value, exposureID) {
			continue
		}
		matches = append(matches, row)
	}
	if len(matches) != 1 {
		return nil
	}
	return collectorTimestamp(matches[0]["created_at"])
}

// mappedIntents 行为映射兴趣标签（:358-384）。
func mappedIntents(store *TopicStarterStore) map[[2]any]string {
	state, ok := HistoryLoadLatestV4(store)
	if !ok {
		state = map[string]any{}
	}
	mapping := HistoryStateMappingEntries(state)
	byPosition := map[[2]any]string{}
	ledger, ledgerIsMap := state["history_ledger"].(map[string]any)
	if !ledgerIsMap {
		return byPosition
	}
	sessions, sessionsIsMap := ledger["sessions"].(map[string]any)
	if !sessionsIsMap {
		return byPosition
	}
	for sessionID, sessionValue := range sessions {
		session, sessionIsMap := sessionValue.(map[string]any)
		if !sessionIsMap {
			continue
		}
		for _, entryValue := range anyListOf(session["entries"]) {
			entry, entryIsMap := entryValue.(map[string]any)
			if !entryIsMap || truthy(entry["retracted"]) {
				continue
			}
			behaviorIndex, behaviorOK := entry["behavior_index"].(int)
			ordinal, ordinalOK := entry["ordinal"].(int)
			if !behaviorOK || !ordinalOK {
				continue
			}
			mapped := mapping[behaviorIndex]
			tag := strings.TrimSpace(pyStr(mapped["tag"]))
			if tag != "" {
				byPosition[[2]any{sessionID, ordinal}] = tag
			}
		}
	}
	return byPosition
}

// signalOf 单条记录信号（:387-423）。
func signalOf(sessionID string, ordinal int, record map[string]any, mappedIntent string, hasMappedIntent bool) *historySignal {
	observedAt := collectorTimestamp(record["timestamp"])
	if observedAt == nil {
		return nil
	}
	fallbackLocator := fmt.Sprintf("session:%s:history:%d", sessionID, ordinal)
	sourceLocator := originOf(record["source_locator"], fallbackLocator)
	sourceEventID := strings.TrimSpace(pyStr(orValue(record["id"], record["request_id"])))
	if sourceEventID == "" {
		sourceEventID = sourceLocator
	}
	explicitIntent, intentIsString := record["intent_cluster"].(string)
	var intentCluster string
	hasIntentCluster := false
	if intentIsString && strings.TrimSpace(explicitIntent) != "" {
		intentCluster, hasIntentCluster = strings.TrimSpace(explicitIntent), true
	} else if hasMappedIntent {
		intentCluster, hasIntentCluster = mappedIntent, true
	}
	return &historySignal{
		sessionID: sessionID, ordinal: ordinal,
		sourceEventID: sourceEventID, sourceLocator: sourceLocator,
		observedAt:       *observedAt,
		inputOrigin:      originOf(record["input_origin"], "pasted_or_unknown"),
		assistanceOrigin: originOf(record["assistance_origin"], "unknown"),
		intentCluster:    intentCluster, hasIntentCluster: hasIntentCluster,
	}
}

// eventPages 有界分页稳定快照（:426-446）。
func eventPages(store *TopicStarterStore, eventType string, pageSize int) []map[string]any {
	var result []map[string]any
	offset := 0
	for {
		page, err := store.QueryEvents(QueryEventsOptions{
			EventType: eventType, PolicyID: &productPolicyIDPtr,
			Limit: pageSize, Offset: offset,
		})
		if err != nil {
			return result
		}
		result = append(result, page...)
		if len(page) < pageSize {
			return result
		}
		offset += len(page)
	}
}

// summaryCoverageTimes 各摘要 ID 最近被覆盖携带的时间（:449-473）。
func summaryCoverageTimes(store *TopicStarterStore, pageSize int) map[string]float64 {
	times := map[string]float64{}
	for _, row := range eventPages(store, SessionSummaryCoverageEventType, pageSize) {
		payload, isMap := row["payload"].(map[string]any)
		if !isMap {
			continue
		}
		sessions, isList := payload["sessions"].([]any)
		createdAt := collectorTimestamp(row["created_at"])
		if !isList || createdAt == nil {
			continue
		}
		for _, itemValue := range sessions {
			item, isMap := itemValue.(map[string]any)
			if !isMap {
				continue
			}
			if summaryID, isString := item["session_summary_id"].(string); isString {
				if *createdAt > times[summaryID] {
					times[summaryID] = *createdAt
				}
			}
		}
	}
	return times
}

// coveredSessionItemFields 覆盖条目字段（:476-481）。
var coveredSessionItemFields = []string{"session_id", "session_summary_id", "input_origin", "assistance_origin"}

// isCoveredSessionItem 覆盖条目形状恰如写出才可信（:484-512）。
func isCoveredSessionItem(item any) bool {
	root, isMap := item.(map[string]any)
	if !isMap {
		return false
	}
	if !mapKeysEqual(root, []string{"session_id", "session_summary_id", "input_origin",
		"assistance_origin", "rid_eligible", "rid_exclusion_reason"}) {
		return false
	}
	for _, field := range coveredSessionItemFields {
		if !nonEmptyStr(root[field]) {
			return false
		}
	}
	if _, isBool := root["rid_eligible"].(bool); !isBool {
		return false
	}
	exclusionReason := root["rid_exclusion_reason"]
	if exclusionReason != nil && pyStr(exclusionReason) != "non_natural_or_assisted_input" {
		return false
	}
	eligible := root["rid_eligible"].(bool)
	return eligible == (exclusionReason == nil)
}

// coveredStandaloneSessionSummaries 仅经精确覆盖装载独立持久化摘要
// （:515-656）。
func coveredStandaloneSessionSummaries(store *TopicStarterStore, pageSize int) []*SessionSummaryArtifact {
	type coverageEntry struct {
		contextScope         string
		summaryConfigVersion string
		item                 map[string]any
	}
	coverageBySummaryID := map[string]coverageEntry{}
	ambiguousCoverageIDs := map[string]bool{}
	for _, row := range eventPages(store, SessionSummaryCoverageEventType, pageSize) {
		payload, isMap := row["payload"].(map[string]any)
		if !isMap {
			continue
		}
		if !mapKeysEqual(payload, []string{"schema_version", "input_snapshot_id",
			"context_scope", "summary_config_version", "sessions"}) {
			continue
		}
		if pyStr(payload["schema_version"]) != SessionSummaryCoverageReceiptVersion {
			continue
		}
		inputSnapshotID := pyStr(payload["input_snapshot_id"])
		contextScope := pyStr(payload["context_scope"])
		summaryConfigVersion := pyStr(payload["summary_config_version"])
		sessions, isList := payload["sessions"].([]any)
		if inputSnapshotID == "" || contextScope == "" || summaryConfigVersion == "" || !isList {
			continue
		}
		if pyStr(row["session_id"]) != "topic-starter-session-summary" {
			continue
		}
		if pyStr(row["slate_id"]) != contextScope {
			continue
		}
		// 一条畸形条目使整批失效，恰如小票写出时的方式
		var validItems []map[string]any
		for _, item := range sessions {
			if isCoveredSessionItem(item) {
				validItems = append(validItems, item.(map[string]any))
			}
		}
		if len(validItems) != len(sessions) {
			continue
		}
		sessionIDSet := map[string]bool{}
		summaryIDSet := map[string]bool{}
		for _, item := range validItems {
			sessionIDSet[pyStr(item["session_id"])] = true
			summaryIDSet[pyStr(item["session_summary_id"])] = true
		}
		if len(sessionIDSet) != len(validItems) || len(summaryIDSet) != len(validItems) {
			continue
		}
		// 既有数据库仅以自然状态为覆盖键
		topicID := pyStr(row["topic_id"])
		if topicID != inputSnapshotID+"|"+summaryConfigVersion &&
			topicID != SessionSummaryCoverageTopicID(payload) {
			continue
		}
		for _, item := range validItems {
			summaryID := pyStr(item["session_summary_id"])
			coverage := coverageEntry{contextScope, summaryConfigVersion, item}
			if existing, exists := coverageBySummaryID[summaryID]; exists {
				if existing.contextScope != coverage.contextScope ||
					existing.summaryConfigVersion != coverage.summaryConfigVersion ||
					!anyEqual(existing.item, coverage.item) {
					ambiguousCoverageIDs[summaryID] = true
					delete(coverageBySummaryID, summaryID)
					continue
				}
			} else if !ambiguousCoverageIDs[summaryID] {
				coverageBySummaryID[summaryID] = coverage
			}
		}
	}
	var summaries []*SessionSummaryArtifact
	for _, row := range eventPages(store, "session_summary", pageSize) {
		payload, isMap := row["payload"].(map[string]any)
		if !isMap {
			continue
		}
		summary, err := SessionSummaryArtifactFromPayload(payload)
		if err != nil {
			continue
		}
		matchedCoverage, covered := coverageBySummaryID[summary.SessionSummaryID]
		if !covered {
			continue
		}
		prefix := fmt.Sprintf("summary:%s:", summary.SessionID)
		semanticDigest := strings.TrimPrefix(summary.SessionSummaryID, prefix)
		if pyStr(row["topic_id"]) != summary.SessionSummaryID {
			continue
		}
		if pyStr(row["session_id"]) != summary.SessionID {
			continue
		}
		if pyStr(row["slate_id"]) != summary.ContextScope {
			continue
		}
		if summary.ContextScope != matchedCoverage.contextScope {
			continue
		}
		if summary.SummaryConfigVersion != matchedCoverage.summaryConfigVersion {
			continue
		}
		if !strings.HasPrefix(summary.SessionSummaryID, prefix) {
			continue
		}
		if len([]rune(semanticDigest)) != 64 {
			continue
		}
		hexOK := true
		for _, r := range semanticDigest {
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
			if !isHex {
				hexOK = false
			}
		}
		if !hexOK {
			continue
		}
		var exclusionReason any
		if !summary.NaturalRevisitEligible() {
			exclusionReason = "non_natural_or_assisted_input"
		}
		expectedItem := map[string]any{
			"session_id":           summary.SessionID,
			"session_summary_id":   summary.SessionSummaryID,
			"input_origin":         summary.InputOrigin,
			"assistance_origin":    summary.AssistanceOrigin,
			"rid_eligible":         summary.NaturalRevisitEligible(),
			"rid_exclusion_reason": exclusionReason,
		}
		if !anyEqual(matchedCoverage.item, expectedItem) {
			continue
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

// startedDepthsFinalized 起始深度已全部落实（:659-704）。
func startedDepthsFinalized(store *TopicStarterStore, manifestID, exposureID string,
	exposedAt, labelHorizon float64) bool {
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "session_bound", SlateID: &exposureID,
		PolicyID: &productPolicyIDPtr, Limit: 8,
	})
	if err != nil {
		return false
	}
	var starts []map[string]any
	for _, row := range rows {
		createdAt := toFloatOr(row["created_at"], 0)
		if !(exposedAt <= createdAt && createdAt <= labelHorizon) {
			continue
		}
		payload, isMap := row["payload"].(map[string]any)
		if !isMap {
			continue
		}
		if pyStr(payload["manifest_id"]) != manifestID {
			continue
		}
		if pyStr(payload["exposure_id"]) != exposureID {
			continue
		}
		starts = append(starts, row)
	}
	for _, start := range starts {
		revisionID := pyStr(start["topic_id"])
		startedSessionID := pyStr(start["session_id"])
		depths, err := store.QueryEvents(QueryEventsOptions{
			EventType: "conversation_depth", SlateID: &exposureID, TopicID: &revisionID,
			PolicyID: &productPolicyIDPtr, Limit: 8,
		})
		if err != nil {
			return false
		}
		found := false
		for _, row := range depths {
			if toFloatOr(row["created_at"], 0) <= labelHorizon &&
				pyStr(row["session_id"]) == startedSessionID {
				if payload, isMap := row["payload"].(map[string]any); isMap &&
					pyStr(payload["manifest_id"]) == manifestID {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ──────────────────────────── ⤴️ 核心方法族（topic_collector.py:834-2151） ────────────────────────────

// recordActiveSignal 记录主动信号（:1187-1259）。
func (c *TopicStarterOutcomeCollector) recordActiveSignal(signal *historySignal,
	userStateSnapshotID, contextScope, manifestID, exposureID string) (int, int, int, int, error) {
	observation := &ActiveExpressionObservation{
		UserStateSnapshotID: userStateSnapshotID, SourceLocator: signal.sourceLocator,
		ObservedAt: signal.observedAt, InputOrigin: signal.inputOrigin,
		AssistanceOrigin: signal.assistanceOrigin, ContextScope: contextScope,
		SchemaVersion: ActiveExpressionObservationSchemaVersion,
	}
	observationRecorded := 0
	if persistTrainingEvent(c.store, "active_expression_observation",
		observation.ArtifactID(), observation.AsPayload(),
		signal.sessionID, exposureID, signal.observedAt) {
		observationRecorded = 1
	}
	admission, err := AdmitActiveExpression(observation)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if admission.Example == nil {
		exclusionRecorded := 0
		recorded, recErr := c.RecordExclusion("pa_active", observation.ArtifactID(),
			admission.ExclusionReason, userStateSnapshotID, contextScope,
			manifestID, exposureID, signal.sessionID, signal.observedAt)
		if recErr != nil {
			return 0, 0, 0, 0, recErr
		}
		if recorded {
			exclusionRecorded = 1
		}
		return observationRecorded, 0, 0, exclusionRecorded, nil
	}
	example := admission.Example
	exampleRecorded := 0
	if persistTrainingEvent(c.store, "active_expression_example",
		example.ArtifactID(), example.AsPayload(),
		signal.sessionID, exposureID, signal.observedAt) {
		exampleRecorded = 1
	}
	trainingExample, err := example.ToTrainingExample(true, signal.observedAt)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	paRecorded := 0
	if persistTrainingEvent(c.store, "pa_training_example",
		trainingExample.ArtifactID(), trainingExample.AsPayload(),
		signal.sessionID, exposureID, signal.observedAt) {
		paRecorded = 1
	}
	return observationRecorded, exampleRecorded, paRecorded, 0, nil
}

// materializeRIDCorpus 从冻结历史构建 IceBreaker 标注，独立于曝光
// （topic_collector.py:834-1185）。
func (c *TopicStarterOutcomeCollector) materializeRIDCorpus(summaries []*SessionSummaryArtifact,
	sessionMetadata map[string]map[string]any,
	summarySignals func(*SessionSummaryArtifact) []*historySignal,
) (*ridCorpusMaterialization, error) {
	var contextSnapshots []*RIDUserContextSnapshot
	for _, row := range eventPages(c.store, "rid_user_context_snapshot",
		maxInt2(1, c.maxExposures)) {
		snapshot, err := RIDUserContextSnapshotFromPayload(row["payload"])
		if err != nil {
			continue
		}
		contextSnapshots = append(contextSnapshots, snapshot)
	}
	summaryRecorded := 0
	var valid []struct {
		summary *SessionSummaryArtifact
		userID  string
	}
	// 摘要按最近被池或覆盖携带到达；同一完结边界在多个摘要配置下
	// 经第一个被比较一次
	comparedBoundaries := map[string]bool{}
	for _, summary := range summaries {
		metadata, exists := sessionMetadata[summary.SessionID]
		if !exists {
			continue
		}
		if !scopeMatches(metadata, summary.ContextScope) {
			continue
		}
		if len(summarySignals(summary)) == 0 {
			continue
		}
		if PersistOutcomeArtifact(c.store, summary, summary.CompletedAt) {
			summaryRecorded++
		}
		userID := sessionUserID(metadata)
		boundaryKey := fmt.Sprintf("%v", summary.CompletionBoundary)
		if userID != "" && summary.NaturalRevisitEligible() && !comparedBoundaries[boundaryKey] {
			comparedBoundaries[boundaryKey] = true
			valid = append(valid, struct {
				summary *SessionSummaryArtifact
				userID  string
			}{summary, userID})
		}
	}
	pairRecorded := 0
	tupleRecorded := 0
	exclusionRecorded := 0
	sliceRecorded := 0
	labels := map[[2]string]*bool{}
	byScope := map[string][]struct {
		summary *SessionSummaryArtifact
		userID  string
	}{}
	for _, item := range valid {
		byScope[item.summary.ContextScope] = append(byScope[item.summary.ContextScope], item)
	}
	scopes := make([]string, 0, len(byScope))
	for scope := range byScope {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	for _, contextScope := range scopes {
		if err := c.checkCancelled(); err != nil {
			return nil, err
		}
		scopedItems := byScope[contextScope]
		sort.SliceStable(scopedItems, func(i, j int) bool {
			a, b := scopedItems[i], scopedItems[j]
			if a.summary.CompletedAt != b.summary.CompletedAt {
				return a.summary.CompletedAt < b.summary.CompletedAt
			}
			if a.summary.SessionSummaryID != b.summary.SessionSummaryID {
				return a.summary.SessionSummaryID < b.summary.SessionSummaryID
			}
			return a.userID < b.userID
		})
		digestInput := make([]any, 0, len(scopedItems))
		for _, item := range scopedItems {
			digestInput = append(digestInput, []any{item.userID,
				item.summary.SessionSummaryID, item.summary.CompletedAt})
		}
		raw, _ := marshalCompactPayload(map[string]any{"rows": digestInput}), true
		_ = raw
		batchDigest := stableDigest(scopedItems)
		batchID := fmt.Sprintf("rid-corpus-batch:%s:%s", contextScope, batchDigest)
		var pairIDs, tupleIDs []string
		contextIDs := map[string]bool{}
		excluded := map[string]string{}
		byUser := map[string][]*SessionSummaryArtifact{}
		for _, item := range scopedItems {
			byUser[item.userID] = append(byUser[item.userID], item.summary)
		}
		if c.summaryPairMatcher == nil {
			hasComparison := false
			for _, items := range byUser {
				sessionSet := map[string]bool{}
				for _, item := range items {
					sessionSet[item.SessionID] = true
				}
				if len(sessionSet) > 1 {
					hasComparison = true
				}
			}
			if hasComparison {
				sourceID := fmt.Sprintf("rid-matcher-binding:%s", batchID)
				exclusionReason := "summary_pair_matcher_unavailable"
				var latestCompletedAt float64
				for _, item := range scopedItems {
					if item.summary.CompletedAt > latestCompletedAt {
						latestCompletedAt = item.summary.CompletedAt
					}
				}
				recorded, recErr := c.RecordExclusion("rid", sourceID, exclusionReason,
					batchID, contextScope, batchID, batchID, "rid-corpus", latestCompletedAt)
				if recErr != nil {
					return nil, recErr
				}
				if recorded {
					exclusionRecorded++
				}
				excluded[sourceID] = exclusionReason
			}
		} else {
			comparisonsComplete := map[string]bool{}
			comparisonCount := map[string]int{}
			matchesByCandidate := map[string][]*RevisitPair{}
			summaryByID := map[string]*SessionSummaryArtifact{}
			userBySummaryID := map[string]string{}
			for _, item := range scopedItems {
				summaryByID[item.summary.SessionSummaryID] = item.summary
				userBySummaryID[item.summary.SessionSummaryID] = item.userID
			}
			userIDs := make([]string, 0, len(byUser))
			for userID := range byUser {
				userIDs = append(userIDs, userID)
			}
			sort.Strings(userIDs)
			for _, userID := range userIDs {
				userSummaries := byUser[userID]
				sort.SliceStable(userSummaries, func(i, j int) bool {
					a, b := userSummaries[i], userSummaries[j]
					if a.CompletedAt != b.CompletedAt {
						return a.CompletedAt < b.CompletedAt
					}
					return a.SessionSummaryID < b.SessionSummaryID
				})
				for index := 0; index+1 < len(userSummaries); index++ {
					earlier := userSummaries[index]
					comparisonsComplete[earlier.SessionSummaryID] = true
					comparisonCount[earlier.SessionSummaryID] = 0
					for _, later := range userSummaries[index+1:] {
						if earlier.SessionID == later.SessionID {
							continue
						}
						comparisonCount[earlier.SessionSummaryID]++
						matchLabel, matchReason, matchErr := c.matchSummaryPair(earlier, later)
						if matchErr != nil {
							return nil, matchErr
						}
						if matchLabel == nil {
							comparisonsComplete[earlier.SessionSummaryID] = false
							sourceID := fmt.Sprintf("rid-match:%s:%s:%s",
								earlier.SessionSummaryID, later.SessionSummaryID,
								c.summaryPairMatcherVersion)
							recorded, recErr := c.RecordExclusion("rid", sourceID,
								matchReason, batchID, contextScope, batchID, batchID,
								later.SessionID, later.CompletedAt)
							if recErr != nil {
								return nil, recErr
							}
							if recorded {
								exclusionRecorded++
							}
							excluded[sourceID] = matchReason
							continue
						}
						pair, pairErr := BuildRevisitPair(earlier, later,
							*matchLabel, c.summaryPairMatcherID, c.summaryPairMatcherVersion)
						if pairErr != nil {
							return nil, pairErr
						}
						labelValue := pair.MatchLabel
						labels[[2]string{earlier.SessionSummaryID, later.SessionSummaryID}] = &labelValue
						pairIDs = append(pairIDs, pair.RevisitPairID)
						if PersistOutcomeArtifact(c.store, pair, pair.ObservedAt) {
							pairRecorded++
						}
						if pair.MatchLabel {
							matchesByCandidate[earlier.SessionSummaryID] =
								append(matchesByCandidate[earlier.SessionSummaryID], pair)
						}
					}
				}
			}
			negativeIDsByUser := map[string][]string{}
			for userID, userSummaries := range byUser {
				var negativeIDs []string
				for _, summary := range userSummaries {
					summaryID := summary.SessionSummaryID
					if comparisonCount[summaryID] <= 0 {
						continue
					}
					if !comparisonsComplete[summaryID] {
						continue
					}
					if len(matchesByCandidate[summaryID]) > 0 {
						continue
					}
					negativeIDs = append(negativeIDs, summaryID)
				}
				negativeIDsByUser[userID] = negativeIDs
			}
			positiveSummaryIDs := make([]string, 0, len(matchesByCandidate))
			for positiveSummaryID := range matchesByCandidate {
				positiveSummaryIDs = append(positiveSummaryIDs, positiveSummaryID)
			}
			sort.Strings(positiveSummaryIDs)
			for _, positiveSummaryID := range positiveSummaryIDs {
				positivePairs := matchesByCandidate[positiveSummaryID]
				positivePair := positivePairs[0]
				for _, candidate := range positivePairs[1:] {
					if candidate.ObservedAt < positivePair.ObservedAt ||
						(candidate.ObservedAt == positivePair.ObservedAt &&
							candidate.LaterSummaryID < positivePair.LaterSummaryID) {
						positivePair = candidate
					}
				}
				positiveUserID := userBySummaryID[positiveSummaryID]
				witness := summaryByID[positivePair.LaterSummaryID]
				witnessSignals := summarySignals(witness)
				if len(witnessSignals) == 0 {
					continue
				}
				witnessStartedAt := witnessSignals[0].observedAt
				for _, signal := range witnessSignals[1:] {
					if signal.observedAt < witnessStartedAt {
						witnessStartedAt = signal.observedAt
					}
				}
				var eligibleContexts []*RIDUserContextSnapshot
				for _, snapshot := range contextSnapshots {
					members := anyListOf(mapAnyOf(snapshot.RequestPayload)["session_summaries"])
					memberIDs := map[string]bool{}
					for _, member := range members {
						memberIDs[pyStr(mapAnyOf(member)["session_summary_id"])] = true
					}
					allSameUser := true
					for memberID := range memberIDs {
						if userBySummaryID[memberID] != positiveUserID {
							allSameUser = false
						}
					}
					if snapshot.ContextScope == contextScope &&
						snapshot.CutoffAt < witnessStartedAt &&
						memberIDs[positiveSummaryID] && allSameUser {
						eligibleContexts = append(eligibleContexts, snapshot)
					}
				}
				var context *RIDUserContextSnapshot
				for _, snapshot := range eligibleContexts {
					if context == nil ||
						snapshot.CutoffAt > context.CutoffAt ||
						(snapshot.CutoffAt == context.CutoffAt &&
							snapshot.UserHistorySnapshotID > context.UserHistorySnapshotID) {
						context = snapshot
					}
				}
				historyIDs := map[string]bool{}
				if context != nil {
					for _, member := range anyListOf(mapAnyOf(context.RequestPayload)["session_summaries"]) {
						historyIDs[pyStr(mapAnyOf(member)["session_summary_id"])] = true
					}
				}
				var intraIDs []string
				for _, summaryID := range negativeIDsByUser[positiveUserID] {
					if historyIDs[summaryID] {
						intraIDs = append(intraIDs, summaryID)
					}
				}
				var interIDs []string
				if context != nil {
					for _, item := range scopedItems {
						if item.userID == positiveUserID {
							continue
						}
						if item.summary.CompletedAt >= context.CutoffAt {
							continue
						}
						interIDs = append(interIDs, item.summary.SessionSummaryID)
					}
				}
				var exclusionReason string
				hasExclusion := false
				if context == nil {
					exclusionReason, hasExclusion = "missing_pre_revisit_user_context", true
				} else if len(intraIDs) == 0 {
					exclusionReason, hasExclusion = "missing_intra_user_negative", true
				} else if len(interIDs) == 0 {
					exclusionReason, hasExclusion = "missing_inter_user_negative", true
				}
				if hasExclusion {
					recorded, recErr := c.RecordExclusion("rid",
						positivePair.RevisitPairID, exclusionReason, batchID,
						contextScope, batchID, batchID,
						positivePair.LaterSessionID, positivePair.ObservedAt)
					if recErr != nil {
						return nil, recErr
					}
					if recorded {
						exclusionRecorded++
					}
					excluded[positivePair.RevisitPairID] = exclusionReason
					continue
				}
				if context == nil {
					return nil, fmt.Errorf("rid training tuple requires a resolved context")
				}
				contextIDs[context.UserHistorySnapshotID] = true
				tupleArtifact, tupleErr := BuildRIDTrainingTuple(
					fmt.Sprintf("rid-training-v3:%s:%s:%s:%s", batchID,
						positiveSummaryID, context.UserHistorySnapshotID,
						c.summaryPairMatcherVersion),
					batchID, context.UserHistorySnapshotID, positiveUserID,
					positivePair, intraIDs, interIDs)
				if tupleErr != nil {
					return nil, tupleErr
				}
				tupleIDs = append(tupleIDs, tupleArtifact.RIDTrainingTupleID)
				if PersistOutcomeArtifact(c.store, tupleArtifact, positivePair.ObservedAt) {
					tupleRecorded++
				}
			}
		}
		if len(pairIDs) > 0 || len(tupleIDs) > 0 || len(excluded) > 0 {
			windowStart := scopedItems[0].summary.CompletedAt
			labelHorizon := scopedItems[0].summary.CompletedAt
			for _, item := range scopedItems {
				if item.summary.CompletedAt < windowStart {
					windowStart = item.summary.CompletedAt
				}
				if item.summary.CompletedAt > labelHorizon {
					labelHorizon = item.summary.CompletedAt
				}
			}
			matcherID := c.summaryPairMatcherID
			if matcherID == "" {
				matcherID = "unbound"
			}
			matcherVersion := c.summaryPairMatcherVersion
			if matcherVersion == "" {
				matcherVersion = "unbound"
			}
			var inputArtifactIDs []string
			inputArtifactIDs = append(inputArtifactIDs, pairIDs...)
			inputArtifactIDs = append(inputArtifactIDs, tupleIDs...)
			contextIDList := make([]string, 0, len(contextIDs))
			for id := range contextIDs {
				contextIDList = append(contextIDList, id)
			}
			sort.Strings(contextIDList)
			inputArtifactIDs = append(inputArtifactIDs, contextIDList...)
			trainingSlice, sliceErr := BuildTrainingSliceManifest(
				fmt.Sprintf("rid-training-slice-v3:%s", batchID),
				windowStart, labelHorizon+1.0, labelHorizon,
				map[string]any{
					"training_domain": "rid", "corpus_batch_id": batchID,
					"matcher_id": matcherID, "matcher_version": matcherVersion,
				},
				contextScope,
				map[string]string{
					"revisit_pair":              RevisitPairSchemaVersion,
					"rid_training_tuple":        RIDTrainingSchemaVersion,
					"rid_user_context_snapshot": RIDUserContextSchemaVersion,
					"training_exclusion":        TrainingExclusionSchemaVersion,
				},
				nil,
				map[string]float64{
					"pair_count":           float64(len(dedupeStrings(pairIDs))),
					"training_tuple_count": float64(len(dedupeStrings(tupleIDs))),
				},
				inputArtifactIDs, excluded)
			if sliceErr != nil {
				return nil, sliceErr
			}
			if PersistOutcomeArtifact(c.store, trainingSlice, labelHorizon) {
				sliceRecorded++
			}
		}
	}
	return &ridCorpusMaterialization{
		summaryRecorded: summaryRecorded, pairRecorded: pairRecorded,
		tupleRecorded: tupleRecorded, exclusionRecorded: exclusionRecorded,
		sliceRecorded: sliceRecorded, labels: labels,
	}, nil
}

// stableDigest scoped 摘要批次的确定性摘要（对齐 hashlib.sha256[:24]）。
func stableDigest(scopedItems []struct {
	summary *SessionSummaryArtifact
	userID  string
}) string {
	payload := make([]any, 0, len(scopedItems))
	for _, item := range scopedItems {
		payload = append(payload, []any{item.userID,
			item.summary.SessionSummaryID, item.summary.CompletedAt})
	}
	raw := marshalCompactPayload(map[string]any{"rows": payload})
	// 对齐 json.dumps(..., separators=(",", ":")) 的紧凑数组形态
	compact := strings.TrimSuffix(strings.TrimPrefix(string(raw), `{"rows":`), `}`)
	compact = strings.TrimSuffix(compact, `}`)
	digest := sha256Hex(compact)
	return digest[:24]
}

// maxInt2 整数最大值。
func maxInt2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// sha256Hex 十六进制 SHA-256。
func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Collect 一次幂等有界维护回收（topic_collector.py:1261-1731）。
func (c *TopicStarterOutcomeCollector) Collect(exposureWindow *[2]float64) (*OutcomeCollectionReport, error) {
	if err := c.checkCancelled(); err != nil {
		return nil, err
	}
	now := c.now()
	if math.IsInf(now, 0) || math.IsNaN(now) || now < 0 {
		return nil, fmt.Errorf("collector clock must be a finite timestamp")
	}
	boundEvents := eventPages(c.store, "session_bound", c.maxBoundEvents)
	if exposureWindow != nil {
		start, end := exposureWindow[0], exposureWindow[1]
		if !(math.IsInf(start, 0) == false && math.IsInf(end, 0) == false &&
			math.IsNaN(start) == false && math.IsNaN(end) == false &&
			0 <= start && start < end) {
			return nil, fmt.Errorf("invalid exposure collection window")
		}
		exposureRows := eventPages(c.store, "dynamic_exposure", c.maxExposures)
		exposureIDs := map[string]bool{}
		for _, row := range exposureRows {
			createdAt := collectorTimestamp(row["created_at"])
			if createdAt == nil {
				continue
			}
			if !(*createdAt >= start && *createdAt < end) {
				continue
			}
			exposureIDs[pyStr(row["slate_id"])] = true
		}
		var filtered []map[string]any
		for _, row := range boundEvents {
			payload := mapAnyOf(row["payload"])
			if exposureIDs[pyStr(payload["exposure_id"])] {
				filtered = append(filtered, row)
			}
		}
		boundEvents = filtered
	}
	sessionMetadata := map[string]map[string]any{}
	for _, item := range c.sessionsLoader() {
		if sessionID := strings.TrimSpace(pyStr(item["session_id"])); sessionID != "" {
			sessionMetadata[sessionID] = item
		}
	}
	historyCache := map[string][]map[string]any{}
	loadHistory := func(sessionID string) []map[string]any {
		if _, cached := historyCache[sessionID]; !cached {
			historyCache[sessionID] = c.historyLoader(sessionID)
		}
		return historyCache[sessionID]
	}
	mapped := mappedIntents(c.store)
	signalsBySession := map[string][]*historySignal{}
	signals := func(sessionID string) []*historySignal {
		if _, cached := signalsBySession[sessionID]; !cached {
			var values []*historySignal
			var completedPrefix *CompletedSessionProjection
			if c.projectCompletedPrefix != nil {
				completedPrefix = c.projectCompletedPrefix(sessionID, loadHistory(sessionID), true)
			}
			completedOrdinals := map[int]bool{}
			if completedPrefix != nil {
				for _, ordinal := range completedPrefix.CompletedUserOrdinals {
					completedOrdinals[ordinal] = true
				}
			}
			for _, pair := range userRecords(loadHistory(sessionID)) {
				if !completedOrdinals[pair.Ordinal] {
					continue
				}
				intent, hasIntent := mapped[[2]any{sessionID, pair.Ordinal}]
				value := signalOf(sessionID, pair.Ordinal, pair.Record, intent, hasIntent)
				if value != nil {
					values = append(values, value)
				}
			}
			signalsBySession[sessionID] = values
		}
		return signalsBySession[sessionID]
	}
	summarySignals := func(summary *SessionSummaryArtifact) []*historySignal {
		var revisions []*CompletedSessionProjection
		if c.projectCompletedPrefixes != nil {
			revisions = c.projectCompletedPrefixes(summary.SessionID, loadHistory(summary.SessionID), true)
		}
		var revision *CompletedSessionProjection
		for _, item := range revisions {
			if item.Equal(summary.CompletionBoundary) {
				revision = item
			}
		}
		if revision == nil {
			return nil
		}
		completedOrdinals := map[int]bool{}
		for _, ordinal := range revision.CompletedUserOrdinals {
			completedOrdinals[ordinal] = true
		}
		sessionSignalIndex := map[[2]any]*historySignal{}
		for _, item := range signals(summary.SessionID) {
			sessionSignalIndex[[2]any{item.ordinal, item.sourceEventID}] = item
		}
		var values []*historySignal
		for _, locator := range summary.SourceLocators {
			if !completedOrdinals[locator.Ordinal] {
				return nil
			}
			signal := sessionSignalIndex[[2]any{locator.Ordinal, locator.EventID}]
			if signal == nil {
				return nil
			}
			values = append(values, signal)
		}
		return values
	}
	servingSummaries := map[string]*SessionSummaryArtifact{}
	ambiguousSummaryIDs := map[string]bool{}
	pageSize := maxInt2(1, c.maxExposures)
	carriedAt := summaryCoverageTimes(c.store, pageSize)
	rememberSummary := func(summary *SessionSummaryArtifact) {
		if ambiguousSummaryIDs[summary.SessionSummaryID] {
			return
		}
		existing, exists := servingSummaries[summary.SessionSummaryID]
		if exists && !anyEqual(existing.AsPayload(), summary.AsPayload()) {
			ambiguousSummaryIDs[summary.SessionSummaryID] = true
			delete(servingSummaries, summary.SessionSummaryID)
			return
		}
		servingSummaries[summary.SessionSummaryID] = summary
	}
	manifestRows, _ := c.store.QueryEvents(QueryEventsOptions{
		EventType: "starter_slate_manifest", PolicyID: &productPolicyIDPtr,
		Limit: c.maxExposures, NewestFirst: true,
	})
	for _, row := range manifestRows {
		manifestPayload, isMap := row["payload"].(map[string]any)
		if !isMap {
			continue
		}
		for _, summary := range manifestSessionSummaries(manifestPayload) {
			rememberSummary(summary)
			if createdAt := collectorTimestamp(row["created_at"]); createdAt != nil {
				if *createdAt > carriedAt[summary.SessionSummaryID] {
					carriedAt[summary.SessionSummaryID] = *createdAt
				}
			}
		}
	}
	for _, summary := range coveredStandaloneSessionSummaries(c.store, pageSize) {
		rememberSummary(summary)
	}
	for summaryID := range ambiguousSummaryIDs {
		delete(servingSummaries, summaryID)
	}
	// 按携带时间倒序排列
	orderedSummaries := make([]*SessionSummaryArtifact, 0, len(servingSummaries))
	for _, summary := range servingSummaries {
		orderedSummaries = append(orderedSummaries, summary)
	}
	sort.SliceStable(orderedSummaries, func(i, j int) bool {
		return carriedAt[orderedSummaries[i].SessionSummaryID] > carriedAt[orderedSummaries[j].SessionSummaryID]
	})
	ridResult, ridErr := c.materializeRIDCorpus(orderedSummaries, sessionMetadata, summarySignals)
	if ridErr != nil {
		return nil, ridErr
	}
	summaryRecorded := ridResult.summaryRecorded
	revisitPairRecorded := ridResult.pairRecorded
	ridTupleRecorded := ridResult.tupleRecorded
	exclusionRecorded := ridResult.exclusionRecorded
	ridSliceRecorded := ridResult.sliceRecorded
	depthRecorded := 0
	futureRecorded := 0
	activeObservationRecorded := 0
	activeExampleRecorded := 0
	activePARecorded := 0
	for _, bound := range boundEvents {
		if err := c.checkCancelled(); err != nil {
			return nil, err
		}
		payload, isMap := bound["payload"].(map[string]any)
		if !isMap {
			continue
		}
		manifestID := strings.TrimSpace(pyStr(payload["manifest_id"]))
		exposureID := strings.TrimSpace(pyStr(payload["exposure_id"]))
		cardRevisionID := strings.TrimSpace(pyStr(orValue(payload["card_revision_id"], bound["topic_id"])))
		startedSessionID := strings.TrimSpace(pyStr(orValue(payload["started_session_id"], bound["session_id"])))
		contextScope := strings.TrimSpace(pyStr(payload["context_scope"]))
		startedAt := collectorTimestamp(bound["created_at"])
		if manifestID == "" || exposureID == "" || cardRevisionID == "" ||
			startedSessionID == "" || contextScope == "" || startedAt == nil {
			continue
		}
		expectedProjectID, scopeErr := ProjectIDForScope(contextScope)
		if scopeErr != nil {
			continue
		}
		var expectedValue any
		if expectedProjectID != "" {
			expectedValue = expectedProjectID
		}
		if !anyEqual(payload["project_id"], expectedValue) {
			continue
		}
		if pyStr(bound["session_id"]) != startedSessionID ||
			pyStr(bound["slate_id"]) != exposureID ||
			pyStr(bound["topic_id"]) != cardRevisionID {
			continue
		}
		startedUsers := userRecords(loadHistory(startedSessionID))
		var lastUserAt float64
		hasLastUser := false
		for _, pair := range startedUsers {
			if stamp := collectorTimestamp(pair.Record["timestamp"]); stamp != nil {
				if !hasLastUser || *stamp > lastUserAt {
					lastUserAt = *stamp
					hasLastUser = true
				}
			}
		}
		if !hasLastUser {
			lastUserAt = *startedAt
		}
		metadataLastUserAt := collectorTimestamp(mapAnyOf(sessionMetadata[startedSessionID])["last_user_message_at"])
		if metadataLastUserAt != nil && *metadataLastUserAt > lastUserAt {
			lastUserAt = *metadataLastUserAt
		}
		exposedAt := exactExposedAt(c.store, manifestID, exposureID)
		var depthBase float64
		if exposedAt != nil {
			depthBase = *exposedAt
		} else {
			depthBase = *startedAt
		}
		depthLabelHorizon := depthBase + c.labelHorizonSeconds
		depthUsers := []map[string]any{}
		for _, pair := range startedUsers {
			if stamp := collectorTimestamp(pair.Record["timestamp"]); stamp != nil && *stamp <= depthLabelHorizon {
				depthUsers = append(depthUsers, pair.Record)
			}
		}
		existingDepth, _ := c.store.QueryEvents(QueryEventsOptions{
			EventType: "conversation_depth", SlateID: &exposureID, TopicID: &cardRevisionID,
			PolicyID: &productPolicyIDPtr, Limit: 1,
		})
		if len(existingDepth) == 0 && now >= depthLabelHorizon &&
			now-lastUserAt >= c.depthQuiescenceSeconds &&
			RecordTerminalConversationDepth(c.store, manifestID, exposureID,
				cardRevisionID, startedSessionID, len(depthUsers), depthLabelHorizon) {
			depthRecorded++
		}
		manifest := exactManifest(c.store, manifestID)
		batchID := strings.TrimSpace(pyStr(payload["generation_batch_id"]))
		projectID := payload["project_id"]
		var card map[string]any
		if manifest != nil && batchID != "" {
			card = FindExactTerminalCard(c.store, manifestID, cardRevisionID,
				batchID, contextScope, projectID)
		}
		userStateSnapshotID := ""
		if card != nil {
			userStateSnapshotID = strings.TrimSpace(pyStr(card["generation_input_snapshot_id"]))
		}
		if userStateSnapshotID == "" {
			continue
		}
		for _, startedSignal := range signals(startedSessionID) {
			if !(*startedAt <= startedSignal.observedAt && startedSignal.observedAt <= depthLabelHorizon) {
				continue
			}
			obs, ex, pa, excl, sigErr := c.recordActiveSignal(startedSignal,
				userStateSnapshotID, contextScope, manifestID, exposureID)
			if sigErr != nil {
				return nil, sigErr
			}
			activeObservationRecorded += obs
			activeExampleRecorded += ex
			activePARecorded += pa
			exclusionRecorded += excl
		}
		interestSummaryID := ""
		if card != nil {
			interestSummaryID = strings.TrimSpace(pyStr(card["interest_summary_id"]))
		}
		rid, ridIsMap := manifest["rid_result"].(map[string]any)
		var decisions []map[string]any
		if ridIsMap {
			for _, field := range []string{"trigger_interests", "rejected_interests"} {
				for _, item := range anyListOf(rid[field]) {
					if m, isMap := item.(map[string]any); isMap {
						decisions = append(decisions, m)
					}
				}
			}
		}
		var matchedDecisions []map[string]any
		for _, item := range decisions {
			if pyStr(item["interest_summary_id"]) == interestSummaryID {
				matchedDecisions = append(matchedDecisions, item)
			}
		}
		var earlier *SessionSummaryArtifact
		if len(matchedDecisions) == 1 && interestSummaryID != "" {
			decision := matchedDecisions[0]
			summaryIDs := decision["source_session_summary_ids"]
			sourceEventIDs := decision["source_event_ids"]
			manifestSummaries := map[string]*SessionSummaryArtifact{}
			for _, summary := range manifestSessionSummaries(manifest) {
				manifestSummaries[summary.SessionSummaryID] = summary
			}
			candidateSummary := manifestSummaries[interestSummaryID]
			var eventIDList []any
			if candidateSummary != nil {
				for _, eventID := range candidateSummary.SourceEventIDs {
					eventIDList = append(eventIDList, eventID)
				}
			}
			matchedSummary := candidateSummary != nil &&
				anyEqual(summaryIDs, []any{interestSummaryID}) &&
				anyEqual(sourceEventIDs, eventIDList) &&
				candidateSummary.ContextScope == contextScope
			if matchedSummary {
				uniqueSources := summarySignals(candidateSummary)
				if len(uniqueSources) > 0 {
					earlier = candidateSummary
					if PersistOutcomeArtifact(c.store, earlier, earlier.CompletedAt) {
						summaryRecorded++
					}
				}
			}
		}
		if earlier == nil {
			continue
		}
		subjectUserID := sessionUserID(sessionMetadata[earlier.SessionID])
		type laterEntry struct {
			summary *SessionSummaryArtifact
			signals []*historySignal
		}
		laterBySession := map[string]laterEntry{}
		for _, candidateSummary := range servingSummaries {
			laterSessionID := candidateSummary.SessionID
			metadata, exists := sessionMetadata[laterSessionID]
			if laterSessionID == startedSessionID || laterSessionID == earlier.SessionID {
				continue
			}
			if !exists {
				continue
			}
			if !scopeMatches(metadata, contextScope) {
				continue
			}
			if candidateSummary.ContextScope != contextScope {
				continue
			}
			if !(startedAt != nil &&
				*startedAt < candidateSummary.CompletedAt &&
				candidateSummary.CompletedAt <= depthLabelHorizon) {
				continue
			}
			var candidateSignals []*historySignal
			for _, item := range summarySignals(candidateSummary) {
				if *startedAt < item.observedAt && item.observedAt <= depthLabelHorizon {
					candidateSignals = append(candidateSignals, item)
				}
			}
			if len(candidateSignals) == 0 {
				continue
			}
			existing, exists := laterBySession[laterSessionID]
			if !exists || candidateSummary.CompletedAt < existing.summary.CompletedAt {
				laterBySession[laterSessionID] = laterEntry{candidateSummary, candidateSignals}
			}
		}
		var laterSessionIDs []string
		for sessionID := range laterBySession {
			laterSessionIDs = append(laterSessionIDs, sessionID)
		}
		sort.Strings(laterSessionIDs)
		for _, laterSessionID := range laterSessionIDs {
			if err := c.checkCancelled(); err != nil {
				return nil, err
			}
			later := laterBySession[laterSessionID]
			metadata := sessionMetadata[laterSessionID]
			laterSignal := later.signals[0]
			if PersistOutcomeArtifact(c.store, later.summary, later.summary.CompletedAt) {
				summaryRecorded++
			}
			laterUserID := sessionUserID(metadata)
			sameUser := subjectUserID != "" && laterUserID != "" &&
				subjectUserID == laterUserID
			if !sameUser {
				continue
			}
			obs, ex, pa, excl, sigErr := c.recordActiveSignal(laterSignal,
				userStateSnapshotID, contextScope, manifestID, exposureID)
			if sigErr != nil {
				return nil, sigErr
			}
			activeObservationRecorded += obs
			activeExampleRecorded += ex
			activePARecorded += pa
			exclusionRecorded += excl
			matchLabel := ridResult.labels[[2]string{earlier.SessionSummaryID, later.summary.SessionSummaryID}]
			if matchLabel == nil {
				label, _, labelErr := c.matchSummaryPair(earlier, later.summary)
				if labelErr != nil {
					return nil, labelErr
				}
				matchLabel = label
			}
			if matchLabel == nil {
				continue
			}
			pair, pairErr := BuildRevisitPair(earlier, later.summary,
				*matchLabel, c.summaryPairMatcherID, c.summaryPairMatcherVersion)
			if pairErr != nil {
				return nil, pairErr
			}
			if PersistOutcomeArtifact(c.store, pair, pair.ObservedAt) {
				revisitPairRecorded++
			}
			if pair.MatchLabel && (pair.NaturalEligible ||
				pyStr(pair.ExclusionReason) == "ineligible_input_origin" ||
				pyStr(pair.ExclusionReason) == "assisted_input_excluded") {
				if RecordTerminalFutureActivity(c.store, manifestID, exposureID,
					cardRevisionID, startedSessionID, laterSessionID,
					laterSignal.sourceEventID, laterSignal.sourceLocator,
					laterSignal.inputOrigin, laterSignal.assistanceOrigin,
					contextScope, laterSignal.observedAt) {
					futureRecorded++
				}
			}
		}
	}
	trainingPlane, planeErr := c.materializeTrainingPlane(now, exposureWindow)
	if planeErr != nil {
		return nil, planeErr
	}
	return &OutcomeCollectionReport{
		BoundSessionsScanned:                         len(boundEvents),
		DepthEventsRecorded:                          depthRecorded,
		FutureActivityEventsRecorded:                 futureRecorded,
		SessionSummaryArtifactsRecorded:              summaryRecorded,
		RevisitPairArtifactsRecorded:                 revisitPairRecorded,
		RIDTrainingTupleArtifactsRecorded:            ridTupleRecorded,
		ActiveExpressionObservationArtifactsRecorded: activeObservationRecorded,
		ActiveExpressionExampleArtifactsRecorded:     activeExampleRecorded,
		PassiveStarterExampleArtifactsRecorded:       trainingPlane.passiveStarterExamplesRecorded,
		PATrainingExampleArtifactsRecorded:           activePARecorded + trainingPlane.passivePAExamplesRecorded,
		TrainingExclusionArtifactsRecorded:           exclusionRecorded + trainingPlane.passiveExclusionsRecorded,
		StarterOutcomeArtifactsRecorded:              trainingPlane.starterOutcomesRecorded,
		UtilityExampleArtifactsRecorded:              trainingPlane.utilityExamplesRecorded,
		PreferencePairArtifactsRecorded:              trainingPlane.preferencePairsRecorded,
		TrainingSliceArtifactsRecorded:               ridSliceRecorded + trainingPlane.trainingSlicesRecorded,
	}, nil
}

// materializeTrainingPlane 训练面物化（topic_collector.py:1733-2151）。
func (c *TopicStarterOutcomeCollector) materializeTrainingPlane(now float64, exposureWindow *[2]float64) (*trainingPlaneMaterialization, error) {
	result := &trainingPlaneMaterialization{}
	exposures := eventPages(c.store, "dynamic_exposure", c.maxExposures)
	for _, exposure := range exposures {
		if err := c.checkCancelled(); err != nil {
			return nil, err
		}
		payload, isMap := exposure["payload"].(map[string]any)
		exposedAt := collectorTimestamp(exposure["created_at"])
		if !isMap || exposedAt == nil {
			continue
		}
		if exposureWindow != nil &&
			!(exposureWindow[0] <= *exposedAt && *exposedAt < exposureWindow[1]) {
			continue
		}
		manifestID := strings.TrimSpace(pyStr(payload["manifest_id"]))
		exposureID := pyStr(exposure["slate_id"])
		if manifestID == "" || strings.TrimSpace(exposureID) == "" {
			continue
		}
		labelHorizon := *exposedAt + c.labelHorizonSeconds
		if now < labelHorizon {
			continue
		}
		if !startedDepthsFinalized(c.store, manifestID, exposureID, *exposedAt, labelHorizon) {
			continue
		}
		joined, joinErr := JoinStarterOutcomes(c.store, manifestID, exposureID, labelHorizon, 4)
		if joinErr != nil {
			return nil, joinErr
		}
		if len(joined) == 0 {
			continue
		}
		var utilities []*UtilityTrainingExample
		var inputArtifactIDs []string
		excludedArtifacts := map[string]string{}
		for _, outcome := range joined {
			if PersistOutcomeArtifact(c.store, outcome, labelHorizon) {
				result.starterOutcomesRecorded++
			}
			featureSnapshot := map[string]any{
				"snapshot_id": outcome.UserStateSnapshotID,
				"observed_at": outcome.ExposedAt,
				"version":     FeatureSnapshotVersion,
				"features": map[string]any{
					"exposure_position": outcome.ExposurePosition,
					"version_refs":      mapOfStringsToAny(outcome.VersionRefs),
				},
			}
			utility, utilityErr := BuildUtilityTrainingExample(outcome, featureSnapshot)
			if utilityErr != nil {
				return nil, utilityErr
			}
			utilities = append(utilities, utility)
			if PersistOutcomeArtifact(c.store, utility, labelHorizon) {
				result.utilityExamplesRecorded++
			}
		}
		contextScope := joined[0].ContextScope
		userStateSnapshotID := joined[0].UserStateSnapshotID
		exclusionRows, _ := c.store.QueryEvents(QueryEventsOptions{
			EventType: "training_exclusion", SlateID: &exposureID,
			PolicyID: &productPolicyIDPtr, Limit: c.maxBoundEvents,
		})
		var exclusionRecords []*TrainingExclusionRecord
		for _, row := range exclusionRows {
			rowPayload, isMap := row["payload"].(map[string]any)
			if !isMap {
				continue
			}
			if pyStr(rowPayload["manifest_id"]) != manifestID {
				continue
			}
			createdAt := collectorTimestamp(row["created_at"])
			if createdAt == nil || *createdAt > labelHorizon {
				continue
			}
			record, err := TrainingExclusionRecordFromPayload(rowPayload)
			if err != nil {
				return nil, err
			}
			exclusionRecords = append(exclusionRecords, record)
		}
		excludedSources := map[string]string{}
		for _, record := range exclusionRecords {
			excludedSources[record.SourceArtifactID] = record.Reason
		}
		for key, value := range excludedSources {
			excludedArtifacts[key] = value
		}
		activeObservationRows, _ := c.store.QueryEvents(QueryEventsOptions{
			EventType: "active_expression_observation", SlateID: &exposureID,
			PolicyID: &productPolicyIDPtr, Limit: c.maxBoundEvents,
		})
		for _, row := range activeObservationRows {
			observation, err := ActiveExpressionObservationFromPayload(row["payload"])
			if err != nil {
				return nil, err
			}
			if observation.UserStateSnapshotID == userStateSnapshotID &&
				observation.ContextScope == contextScope &&
				observation.ObservedAt <= labelHorizon &&
				excludedSources[observation.ArtifactID()] == "" {
				inputArtifactIDs = append(inputArtifactIDs, observation.ArtifactID())
			}
		}
		for _, pair := range []struct {
			eventType string
			kind      string // "example" / "pa_active"
		}{
			{"active_expression_example", "example"},
			{"pa_training_example", "pa_active"},
		} {
			rows, _ := c.store.QueryEvents(QueryEventsOptions{
				EventType: pair.eventType, SlateID: &exposureID,
				PolicyID: &productPolicyIDPtr, Limit: c.maxBoundEvents,
			})
			for _, row := range rows {
				var snapshotID, artifactScope, artifactID string
				var artifactTime float64
				domainOK := true
				if pair.kind == "example" {
					artifact, err := ActiveExpressionExampleFromPayload(row["payload"])
					if err != nil {
						return nil, err
					}
					snapshotID = artifact.UserStateSnapshotID
					artifactScope = artifact.ContextScope
					artifactTime = artifact.ObservedAt
					artifactID = artifact.ArtifactID()
				} else {
					artifact, err := PATrainingExampleFromPayload(row["payload"])
					if err != nil {
						return nil, err
					}
					snapshotID = artifact.UserStateSnapshotID
					artifactScope = artifact.ContextScope
					artifactTime = artifact.EventTime
					artifactID = artifact.ArtifactID()
					domainOK = artifact.Domain == "active"
				}
				if snapshotID == userStateSnapshotID && artifactScope == contextScope &&
					artifactTime <= labelHorizon && domainOK {
					inputArtifactIDs = append(inputArtifactIDs, artifactID)
				}
			}
		}
		manifest := exactManifest(c.store, manifestID)
		var trace map[string]any
		if manifest != nil {
			if listSearch, isMap := manifest["list_search"].(map[string]any); isMap {
				if value, isMap := listSearch["bounded_beam_trace"].(map[string]any); isMap {
					trace = value
				}
			}
		}
		batchID := strings.TrimSpace(pyStr(payload["generation_batch_id"]))
		projectID := payload["project_id"]
		for _, outcome := range joined {
			var card map[string]any
			if manifest != nil && batchID != "" {
				card = FindExactTerminalCard(c.store, manifestID,
					outcome.CardRevisionID, batchID, contextScope, projectID)
			}
			sourceStarterID := ""
			corpusVersion := ""
			if card != nil {
				sourceStarterID = strings.TrimSpace(pyStr(card["source_starter_id"]))
				corpusVersion = strings.TrimSpace(pyStr(card["corpus_version"]))
			}
			if sourceStarterID == "" {
				continue
			}
			sessionForRecord := exposureID
			if outcome.StartedSessionID != nil {
				sessionForRecord = pyStr(outcome.StartedSessionID)
			}
			if corpusVersion == "" || len(outcome.VersionRefs) == 0 {
				recorded, recErr := c.RecordExclusion("pa_passive", outcome.OutcomeID,
					"missing_passive_feature_lineage", userStateSnapshotID,
					contextScope, manifestID, exposureID, sessionForRecord, labelHorizon)
				if recErr != nil {
					return nil, recErr
				}
				if recorded {
					result.passiveExclusionsRecorded++
				}
				excludedArtifacts[outcome.OutcomeID] = "missing_passive_feature_lineage"
				continue
			}
			passive := &PassiveStarterExample{
				UserStateSnapshotID: userStateSnapshotID, ContextScope: contextScope,
				CorpusVersion: corpusVersion, StarterID: sourceStarterID,
				ExposureID: exposureID, ManifestID: manifestID,
				CardRevisionID: outcome.CardRevisionID, ExposedAt: outcome.ExposedAt,
				FeatureVersionRefs: outcome.VersionRefs,
				SchemaVersion:      PassiveStarterExampleSchemaVersion,
			}
			if outcome.ClickedAt != nil {
				if clicked, isFloat := outcome.ClickedAt.(float64); isFloat {
					passive.ClickedAt = &clicked
				}
			}
			if outcome.NotInterestedAt != nil {
				if notInterested, isFloat := outcome.NotInterestedAt.(float64); isFloat {
					passive.NotInterestedAt = &notInterested
				}
			}
			if persistTrainingEvent(c.store, "passive_starter_example",
				passive.ArtifactID(), passive.AsPayload(), sessionForRecord,
				exposureID, labelHorizon) {
				result.passiveStarterExamplesRecorded++
			}
			if outcome.Clicked == outcome.NotInterested {
				reason := "missing_passive_label"
				if outcome.Clicked {
					reason = "conflicting_passive_labels"
				}
				recorded, recErr := c.RecordExclusion("pa_passive", passive.ArtifactID(),
					reason, userStateSnapshotID, contextScope, manifestID,
					exposureID, sessionForRecord, labelHorizon)
				if recErr != nil {
					return nil, recErr
				}
				if recorded {
					result.passiveExclusionsRecorded++
				}
				excludedArtifacts[passive.ArtifactID()] = reason
				continue
			}
			positiveLabel := outcome.Clicked
			var eventTime float64
			if positiveLabel {
				eventTime = toFloatOr(outcome.ClickedAt, 0)
			} else {
				eventTime = toFloatOr(outcome.NotInterestedAt, 0)
			}
			if outcome.ClickedAt == nil && outcome.NotInterestedAt == nil {
				continue
			}
			passiveTraining, trainErr := passive.ToTrainingExample(positiveLabel, eventTime)
			if trainErr != nil {
				return nil, trainErr
			}
			if persistTrainingEvent(c.store, "pa_training_example",
				passiveTraining.ArtifactID(), passiveTraining.AsPayload(),
				sessionForRecord, exposureID, eventTime) {
				result.passivePAExamplesRecorded++
			}
			inputArtifactIDs = append(inputArtifactIDs, passive.ArtifactID(), passiveTraining.ArtifactID())
		}
		var positiveIDs []string
		for _, outcome := range joined {
			if outcome.NotInterested {
				continue
			}
			engaged := outcome.Clicked || outcome.Started || outcome.FutureNaturalActivity
			if !engaged {
				continue
			}
			positiveIDs = append(positiveIDs, outcome.CardRevisionID)
		}
		rankerVersion := strings.TrimSpace(pyStr(mapAnyOf(trace)["utility_scorer_version"]))
		diversityVersion := strings.TrimSpace(pyStr(mapAnyOf(trace)["diversity_scorer_version"]))
		generatorVersion := ""
		if manifest != nil {
			generatorVersion = strings.TrimSpace(pyStr(manifest["generation_config_version"]))
		}
		if len(positiveIDs) > 0 && rankerVersion != "" && diversityVersion != "" && generatorVersion != "" {
			for _, negative := range joined {
				if !negative.NotInterested {
					continue
				}
				preference := &PreferencePair{
					PreferencePairID: fmt.Sprintf("preference:%s:%s:h%.6f",
						exposureID, negative.CardRevisionID, labelHorizon),
					ContextScope: negative.ContextScope, ManifestID: manifestID,
					ExposureID: exposureID, PreferredCardRevisionIDs: positiveIDs,
					NegativeCardRevisionID: negative.CardRevisionID,
					NegativeType:           "not_interested", RankerVersion: rankerVersion,
					DiversityVersion: diversityVersion, GeneratorVersion: generatorVersion,
					CreatedAt: labelHorizon, SchemaVersion: PreferencePairSchemaVersion,
				}
				if err := preference.validate(); err != nil {
					return nil, err
				}
				inputArtifactIDs = append(inputArtifactIDs, preference.PreferencePairID)
				if PersistOutcomeArtifact(c.store, preference, labelHorizon) {
					result.preferencePairsRecorded++
				}
			}
		}
		versions := map[string]string{
			"starter_outcome":               StarterOutcomeSchemaVersion,
			"utility_training":              UtilityTrainingSchemaVersion,
			"preference_pair":               PreferencePairSchemaVersion,
			"rid_training_tuple":            RIDTrainingSchemaVersion,
			"active_expression_observation": ActiveExpressionObservationSchemaVersion,
			"active_expression_example":     ActiveExpressionExampleSchemaVersion,
			"passive_starter_example":       PassiveStarterExampleSchemaVersion,
			"pa_training_example":           PATrainingExampleSchemaVersion,
			"training_exclusion":            TrainingExclusionSchemaVersion,
		}
		trainingSlice, sliceErr := BuildTrainingSliceManifest(
			fmt.Sprintf("training-slice:%s:h%.6f", exposureID, labelHorizon),
			*exposedAt, labelHorizon+1.0, labelHorizon,
			map[string]any{
				"manifest_id": manifestID, "exposure_id": exposureID,
				"label_horizon_seconds": c.labelHorizonSeconds,
			},
			contextScope, versions, utilities,
			map[string]float64{"example_count": float64(len(utilities))},
			inputArtifactIDs, excludedArtifacts)
		if sliceErr != nil {
			return nil, sliceErr
		}
		if PersistOutcomeArtifact(c.store, trainingSlice, labelHorizon) {
			result.trainingSlicesRecorded++
		}
	}
	return result, nil
}
