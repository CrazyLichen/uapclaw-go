package recommendation

// topic_short_intent.go 一比一对齐上游 topic_short_intent.py（504 行）的
// RecGPT-Mobile 适配的短意图刷新：ShortIntentBehavior/ShortIntentDecision/
// ShortIntentState/ShortIntentStateStore/evaluate_short_intent/
// build_generation_state_projection。
// ⤴️31c 归位：随 topic_short_intent 所属领域迁移时移入对应文件。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 常量 ────────────────────────────

// shortIntentBusyTimeoutSeconds 与同库其他存储相同的写等待
// （topic_short_intent.py:21）
const shortIntentBusyTimeoutSeconds = 5.0

// ShortIntentNaturalInputOrigin 自然输入来源（:22）
const ShortIntentNaturalInputOrigin = "manual_free_text"

// ShortIntentNaturalAssistanceOrigin 自然协助来源（:23）
const ShortIntentNaturalAssistanceOrigin = "none"

// ShortIntentHighIntentKinds 高意图类型集合（:24）
var ShortIntentHighIntentKinds = map[string]bool{
	"new_goal": true, "correction": true, "recent_action": true,
}

// MinDriftSamples 漂移判定的最小样本数（:25）
const MinDriftSamples = 3

// ShortIntentPipelineReceiptVersion 流水线读取小票版本（:26）
const ShortIntentPipelineReceiptVersion = "topic-starter-v6-short-intent-read-v1"

// SHORT_INTENT_SNAPSHOT_PREFIX 短意图快照 ID 前缀（topic_pipeline.py:205）
const SHORT_INTENT_SNAPSHOT_PREFIX = "v6-short"

// ──────────────────────────── 结构体 ────────────────────────────

// ShortIntentBehavior 冻结输入快照里的一条上游已分类行为
// （topic_short_intent.py:35-50 ShortIntentBehavior）。
type ShortIntentBehavior struct {
	// EventID 事件 ID
	EventID string
	// IntentCluster 意图簇
	IntentCluster string
	// ObservedAt 观察时间
	ObservedAt float64
	// InputOrigin 输入来源
	InputOrigin string
	// AssistanceOrigin 协助来源
	AssistanceOrigin string
	// HighIntentKind 高意图类型（无则为空串，对齐 None）
	HighIntentKind string
}

// IsNatural 自然行为判定（对齐 :45-50 is_natural）。
func (b *ShortIntentBehavior) IsNatural() bool {
	return b.InputOrigin == ShortIntentNaturalInputOrigin &&
		b.AssistanceOrigin == ShortIntentNaturalAssistanceOrigin
}

// ShortIntentDecision 纯 OR 触发结果，持久化前的形态
// （topic_short_intent.py:53-64 ShortIntentDecision）。
type ShortIntentDecision struct {
	// InputSnapshotID 输入快照 ID
	InputSnapshotID string
	// TriggerReasons 触发原因
	TriggerReasons []string
	// ReasonEvidenceLineage 原因证据血缘
	ReasonEvidenceLineage []map[string]any
	// DominantIntentClusters 当前主导意图簇
	DominantIntentClusters []string
	// MobileDrift 移动漂移（可空）
	MobileDrift *InterestDrift
	// DriftAnchorSnapshotId 漂移锚点快照 ID（可空）
	DriftAnchorSnapshotID string
	// DriftAnchorEventIds 漂移锚点事件
	DriftAnchorEventIDs []string
	// DriftAnchorIntentClusters 漂移锚点意图簇
	DriftAnchorIntentClusters []string
}

// ShortIntentState 一次不可变输入快照求值后的持久化状态
// （topic_short_intent.py:67-123 ShortIntentState）。
type ShortIntentState struct {
	// ShortIntentVersion 短意图版本
	ShortIntentVersion int
	// InputSnapshotID 输入快照 ID
	InputSnapshotID string
	// EvaluatedAt 求值时间
	EvaluatedAt float64
	// TriggerReasons 触发原因
	TriggerReasons []string
	// ReasonEvidenceLineage 原因证据血缘
	ReasonEvidenceLineage []map[string]any
	// DominantIntentClusters 主导意图簇
	DominantIntentClusters []string
	// DriftAnchorSnapshotId 漂移锚点快照 ID（可空）
	DriftAnchorSnapshotID string
	// DriftAnchorEventIds 漂移锚点事件
	DriftAnchorEventIDs []string
	// DriftAnchorIntentClusters 漂移锚点意图簇
	DriftAnchorIntentClusters []string
}

// AsPayload 序列化（对齐 :81-96 as_payload）。
func (s *ShortIntentState) AsPayload() map[string]any {
	lineage := make([]map[string]any, 0, len(s.ReasonEvidenceLineage))
	for _, item := range s.ReasonEvidenceLineage {
		lineage = append(lineage, item)
	}
	var anchor any
	if s.DriftAnchorSnapshotID != "" {
		anchor = s.DriftAnchorSnapshotID
	}
	return map[string]any{
		"short_intent_version":         s.ShortIntentVersion,
		"input_snapshot_id":            s.InputSnapshotID,
		"evaluated_at":                 s.EvaluatedAt,
		"trigger_reasons":              append([]string(nil), s.TriggerReasons...),
		"reason_evidence_lineage":      lineage,
		"dominant_intent_clusters":     append([]string(nil), s.DominantIntentClusters...),
		"drift_anchor_snapshot_id":     anchor,
		"drift_anchor_event_ids":       append([]string(nil), s.DriftAnchorEventIDs...),
		"drift_anchor_intent_clusters": append([]string(nil), s.DriftAnchorIntentClusters...),
	}
}

// ShortIntentStateFromPayload 反序列化（对齐 :98-123 from_payload）。
func ShortIntentStateFromPayload(payload map[string]any) *ShortIntentState {
	var anchor string
	if pyStr(payload["drift_anchor_snapshot_id"]) != "" {
		anchor = pyStr(payload["drift_anchor_snapshot_id"])
	}
	return &ShortIntentState{
		ShortIntentVersion:        pyIntOf(payload["short_intent_version"]),
		InputSnapshotID:           pyStr(payload["input_snapshot_id"]),
		EvaluatedAt:               toFloatOr(payload["evaluated_at"], 0),
		TriggerReasons:            pyListStrings(payload["trigger_reasons"]),
		ReasonEvidenceLineage:     pyListMaps(payload["reason_evidence_lineage"]),
		DominantIntentClusters:    pyListStrings(payload["dominant_intent_clusters"]),
		DriftAnchorSnapshotID:     anchor,
		DriftAnchorEventIDs:       pyListStrings(payload["drift_anchor_event_ids"]),
		DriftAnchorIntentClusters: pyListStrings(payload["drift_anchor_intent_clusters"]),
	}
}

// ShortIntentPersistResult 一次持久化结果
// （topic_short_intent.py:126-131 ShortIntentPersistResult）。
type ShortIntentPersistResult struct {
	// State 落库后的状态
	State *ShortIntentState
	// Created 是否新写
	Created bool
	// VersionChanged 版本是否递增
	VersionChanged bool
}

// ShortIntentStateStore 与长期 Memory 并列、互相独立的短状态持久层
// （topic_short_intent.py:133-380 ShortIntentStateStore）。
type ShortIntentStateStore struct {
	// Path 推荐库文件路径
	Path string
}

// NewShortIntentStateStore 构造并建表（对齐 :136-149 __init__）。
func NewShortIntentStateStore(topicStore *TopicStarterStore) (*ShortIntentStateStore, error) {
	store := &ShortIntentStateStore{Path: topicStore.Path()}
	db, err := store.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS short_intent_state (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			input_snapshot_id TEXT NOT NULL UNIQUE,
			short_intent_version INTEGER NOT NULL,
			evaluated_at REAL NOT NULL,
			state_json TEXT NOT NULL
		)`); err != nil {
		return nil, fmt.Errorf("创建短意图状态表失败: %w", err)
	}
	return store, nil
}

// connect 打开库连接（对齐 :151-155 _connect）。
func (s *ShortIntentStateStore) connect() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s?_busy_timeout=%d", s.Path,
		int(shortIntentBusyTimeoutSeconds*1000))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(0)
	return db, nil
}

// decode 反序列化行（对齐 :157-166 _decode）。
func (s *ShortIntentStateStore) decode(row *sql.Row) (*ShortIntentState, error) {
	if row == nil {
		return nil, nil
	}
	var stateJSON string
	if err := row.Scan(&stateJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stateJSON), &payload); err != nil {
		return nil, nil
	}
	return ShortIntentStateFromPayload(payload), nil
}

// LoadLatest 最新一条状态（对齐 :168-178 load_latest）。
func (s *ShortIntentStateStore) LoadLatest() (*ShortIntentState, error) {
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	row := db.QueryRow(`
		SELECT state_json
		FROM short_intent_state
		ORDER BY sequence DESC
		LIMIT 1`)
	return s.decode(row)
}

// shortIntentScopePrefix 快照 ID 的 scope 前缀
// （对齐 :180-198 _scope_prefix）：v6-short:<scope>[:<64位hex digest>]:<boundary>。
func shortIntentScopePrefix(inputSnapshotID string) string {
	if !strings.HasPrefix(inputSnapshotID, "v6-short:") {
		return ""
	}
	body := strings.TrimPrefix(inputSnapshotID, "v6-short:")
	// rpartition(":")：取最后一个冒号前后的内容
	lastIndex := strings.LastIndex(body, ":")
	if lastIndex <= 0 || lastIndex == len(body)-1 {
		return ""
	}
	head := body[:lastIndex]
	boundary := body[lastIndex+1:]
	if !allDigits(boundary) {
		return ""
	}
	scope := head
	if digestIndex := strings.LastIndex(head, ":"); digestIndex >= 0 {
		possibleScope := head[:digestIndex]
		semanticDigest := head[digestIndex+1:]
		if possibleScope != "" && len(semanticDigest) == 64 && allHex(semanticDigest) {
			scope = possibleScope
		}
	}
	if scope == "" {
		return ""
	}
	return "v6-short:" + scope + ":"
}

// LoadLatestForScope 一个 scope 的最新状态（对齐 :200-213）。
func (s *ShortIntentStateStore) LoadLatestForScope(contextScope string) (*ShortIntentState, error) {
	prefix := "v6-short:" + contextScope + ":"
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	row := db.QueryRow(`
		SELECT state_json
		FROM short_intent_state
		WHERE substr(input_snapshot_id, 1, ?) = ?
		ORDER BY sequence DESC
		LIMIT 1`, len(prefix), prefix)
	return s.decode(row)
}

// LoadLegacyAnchorForScope 从无锚点行恢复最后触发或首个基线
// （对齐 :215-250 load_legacy_anchor_for_scope）。
func (s *ShortIntentStateStore) LoadLegacyAnchorForScope(contextScope string) (*ShortIntentState, error) {
	prefix := "v6-short:" + contextScope + ":"
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	row := db.QueryRow(`
		SELECT state_json
		FROM short_intent_state
		WHERE substr(input_snapshot_id, 1, ?) = ?
		  AND COALESCE(
		        json_array_length(
		            json_extract(state_json, '$.trigger_reasons')
		        ),
		        0
		      ) > 0
		ORDER BY sequence DESC
		LIMIT 1`, len(prefix), prefix)
	state, err := s.decode(row)
	if err != nil {
		return nil, err
	}
	if state == nil {
		row = db.QueryRow(`
			SELECT state_json
			FROM short_intent_state
			WHERE substr(input_snapshot_id, 1, ?) = ?
			ORDER BY sequence ASC
			LIMIT 1`, len(prefix), prefix)
		state, err = s.decode(row)
		if err != nil {
			return nil, err
		}
	}
	return state, nil
}

// LoadTriggerForVersion 读取触发该版本的求值，不受后续读取影响
// （对齐 :252-275 load_trigger_for_version）。
func (s *ShortIntentStateStore) LoadTriggerForVersion(contextScope string,
	shortIntentVersion int) (*ShortIntentState, error) {
	prefix := "v6-short:" + contextScope + ":"
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	row := db.QueryRow(`
		SELECT state_json
		FROM short_intent_state
		WHERE substr(input_snapshot_id, 1, ?) = ?
		  AND short_intent_version = ?
		  AND json_array_length(
		        json_extract(state_json, '$.trigger_reasons')
		      ) > 0
		ORDER BY sequence DESC
		LIMIT 1`, len(prefix), prefix, shortIntentVersion)
	return s.decode(row)
}

// LoadForSnapshot 按快照 ID 读取（对齐 :277-288 load_for_snapshot）。
func (s *ShortIntentStateStore) LoadForSnapshot(inputSnapshotID string) (*ShortIntentState, error) {
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	row := db.QueryRow(`
		SELECT state_json
		FROM short_intent_state
		WHERE input_snapshot_id = ?
		LIMIT 1`, inputSnapshotID)
	return s.decode(row)
}

// Persist 追加一次求值；当且仅当任一 OR 原因触发时版本递增一次
// （对齐 :290-380 persist）。
func (s *ShortIntentStateStore) Persist(decision *ShortIntentDecision,
	evaluatedAt float64) (*ShortIntentPersistResult, error) {
	if evaluatedAt <= 0 {
		return nil, fmt.Errorf("evaluated_at must be positive")
	}
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	// Python BEGIN IMMEDIATE：写锁在事务开始即取得；go-sqlite3 以
	// Serializable 隔离级映射 BEGIN IMMEDIATE。
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	rollback := func(result *ShortIntentPersistResult, txErr error) (*ShortIntentPersistResult, error) {
		_ = tx.Rollback()
		return result, txErr
	}
	var existingJSON string
	existingErr := tx.QueryRow(`
		SELECT state_json
		FROM short_intent_state
		WHERE input_snapshot_id = ?
		LIMIT 1`, decision.InputSnapshotID).Scan(&existingJSON)
	if existingErr == nil {
		var payload map[string]any
		if json.Unmarshal([]byte(existingJSON), &payload) != nil || payload == nil {
			return rollback(nil, fmt.Errorf("stored short-intent state is invalid"))
		}
		state := ShortIntentStateFromPayload(payload)
		return &ShortIntentPersistResult{State: state, Created: false, VersionChanged: false}, nil
	}
	scopePrefix := shortIntentScopePrefix(decision.InputSnapshotID)
	previousVersion := 0
	if scopePrefix == "" {
		row := tx.QueryRow(`
			SELECT short_intent_version
			FROM short_intent_state
			ORDER BY sequence DESC
			LIMIT 1`)
		var version int
		if err := row.Scan(&version); err == nil {
			previousVersion = version
		}
	} else {
		row := tx.QueryRow(`
			SELECT short_intent_version
			FROM short_intent_state
			WHERE substr(input_snapshot_id, 1, ?) = ?
			ORDER BY sequence DESC
			LIMIT 1`, len(scopePrefix), scopePrefix)
		var version int
		if err := row.Scan(&version); err == nil {
			previousVersion = version
		}
	}
	versionChanged := len(decision.TriggerReasons) > 0
	version := previousVersion
	if versionChanged {
		version++
	}
	state := &ShortIntentState{
		ShortIntentVersion:        version,
		InputSnapshotID:           decision.InputSnapshotID,
		EvaluatedAt:               evaluatedAt,
		TriggerReasons:            append([]string(nil), decision.TriggerReasons...),
		ReasonEvidenceLineage:     append([]map[string]any(nil), decision.ReasonEvidenceLineage...),
		DominantIntentClusters:    append([]string(nil), decision.DominantIntentClusters...),
		DriftAnchorSnapshotID:     decision.DriftAnchorSnapshotID,
		DriftAnchorEventIDs:       append([]string(nil), decision.DriftAnchorEventIDs...),
		DriftAnchorIntentClusters: append([]string(nil), decision.DriftAnchorIntentClusters...),
	}
	encoded, err := utils.MarshalNoEscape(state.AsPayload())
	if err != nil {
		return rollback(nil, err)
	}
	if _, err := tx.Exec(`
		INSERT INTO short_intent_state(
			input_snapshot_id,
			short_intent_version,
			evaluated_at,
			state_json
		) VALUES (?, ?, ?, ?)`,
		state.InputSnapshotID, state.ShortIntentVersion, state.EvaluatedAt,
		string(encoded)); err != nil {
		return rollback(nil, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ShortIntentPersistResult{State: state, Created: true, VersionChanged: versionChanged}, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EvaluateShortIntent 只对自然行为求值全部独立短意图原因
// （topic_short_intent.py:405-485 evaluate_short_intent）。
func EvaluateShortIntent(inputSnapshotID string, referenceBehaviors,
	currentBehaviors []*ShortIntentBehavior,
	highIntentBehaviors []*ShortIntentBehavior,
	highIntentProvided bool) (*ShortIntentDecision, error) {
	if strings.TrimSpace(inputSnapshotID) == "" {
		return nil, fmt.Errorf("input_snapshot_id must be non-empty")
	}
	var reference, current []*ShortIntentBehavior
	for _, behavior := range referenceBehaviors {
		if behavior.IsNatural() {
			reference = append(reference, behavior)
		}
	}
	for _, behavior := range currentBehaviors {
		if behavior.IsNatural() {
			current = append(current, behavior)
		}
	}
	var reasons []string
	var lineage []map[string]any

	var drift *InterestDrift
	if len(reference) == len(current) && len(current) >= MinDriftSamples {
		referenceLabels := make([]string, 0, len(reference))
		for _, behavior := range reference {
			referenceLabels = append(referenceLabels, behavior.IntentCluster)
		}
		currentLabels := make([]string, 0, len(current))
		for _, behavior := range current {
			currentLabels = append(currentLabels, behavior.IntentCluster)
		}
		computed := ComputeInterestDrift(referenceLabels, currentLabels)
		drift = &computed
		if drift.ShouldUpdate {
			reasons = append(reasons, "mobile_drift")
			lineage = append(lineage, map[string]any{
				"reason":              "mobile_drift",
				"reference_event_ids": behaviorEventIDs(reference),
				"current_event_ids":   behaviorEventIDs(current),
				"window_length":       len(current),
				"entropy_delta":       drift.EntropyDelta,
				"jaccard_distance":    drift.JaccardDistance,
				"js_divergence":       drift.JSDivergence,
				"score":               drift.Score,
				"threshold":           0.8,
			})
		}
	}

	highIntentSource := current
	if highIntentProvided {
		highIntentSource = nil
		for _, behavior := range highIntentBehaviors {
			if behavior.IsNatural() {
				highIntentSource = append(highIntentSource, behavior)
			}
		}
	}
	var highIntent []*ShortIntentBehavior
	for _, behavior := range highIntentSource {
		if ShortIntentHighIntentKinds[behavior.HighIntentKind] {
			highIntent = append(highIntent, behavior)
		}
	}
	if len(highIntent) > 0 {
		reasons = append(reasons, "high_intent_event")
		kinds := make([]string, 0, len(highIntent))
		for _, item := range highIntent {
			kinds = append(kinds, item.HighIntentKind)
		}
		lineage = append(lineage, map[string]any{
			"reason":       "high_intent_event",
			"event_ids":    behaviorEventIDs(highIntent),
			"intent_kinds": kinds,
		})
	}

	referenceDominant := dominantClusters(reference)
	currentDominant := dominantClusters(current)
	if len(referenceDominant) > 0 && len(currentDominant) > 0 &&
		strings.Join(referenceDominant, "\x00") != strings.Join(currentDominant, "\x00") {
		reasons = append(reasons, "semantic_turnover")
		lineage = append(lineage, map[string]any{
			"reason":                             "semantic_turnover",
			"reference_dominant_intent_clusters": referenceDominant,
			"current_dominant_intent_clusters":   currentDominant,
			"reference_event_ids":                behaviorEventIDs(reference),
			"current_event_ids":                  behaviorEventIDs(current),
		})
	}

	return &ShortIntentDecision{
		InputSnapshotID:        inputSnapshotID,
		TriggerReasons:         reasons,
		ReasonEvidenceLineage:  lineage,
		DominantIntentClusters: currentDominant,
		MobileDrift:            drift,
	}, nil
}

// BuildGenerationStateProjection 把两个独立状态版本投影进候选生成
// （topic_short_intent.py:488-502 build_generation_state_projection）。
func BuildGenerationStateProjection(state *ShortIntentState,
	profileSemanticVersion int) (map[string]any, error) {
	if profileSemanticVersion < 0 {
		return nil, fmt.Errorf("profile_semantic_version must be a non-negative integer")
	}
	return map[string]any{
		"input_snapshot_id":            state.InputSnapshotID,
		"profile_semantic_version":     profileSemanticVersion,
		"short_intent_version":         state.ShortIntentVersion,
		"short_intent_trigger_reasons": append([]string(nil), state.TriggerReasons...),
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// behaviorEventIDs 行为事件列表。
func behaviorEventIDs(behaviors []*ShortIntentBehavior) []string {
	out := make([]string, 0, len(behaviors))
	for _, behavior := range behaviors {
		out = append(out, behavior.EventID)
	}
	return out
}

// dominantClusters 主导意图簇：计数并列最高者按字典序
// （对齐 :389-402 _dominant_clusters）。
func dominantClusters(behaviors []*ShortIntentBehavior) []string {
	counts := map[string]int{}
	for _, behavior := range behaviors {
		cluster := strings.TrimSpace(behavior.IntentCluster)
		if cluster != "" {
			counts[cluster]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	highest := 0
	for _, count := range counts {
		if count > highest {
			highest = count
		}
	}
	clusters := []string{}
	for cluster, count := range counts {
		if count == highest {
			clusters = append(clusters, cluster)
		}
	}
	sort.Strings(clusters)
	return clusters
}

// allDigits 全数字。
func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// allHex 全 16 进制小写字符。
func allHex(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') &&
			!(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
