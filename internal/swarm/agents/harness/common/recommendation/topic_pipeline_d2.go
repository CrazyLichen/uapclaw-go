package recommendation

// topic_pipeline_d2.go 一比一对齐上游 topic_pipeline.py:4623-4907 的
// scope_input_revision 与 refresh_short_intent。
// 已知偏差（§5 并发映射）：async def → 同步方法（调用方在 D 段主循环里
// 经 goroutine 调度）；hashlib.sha256/ensure_ascii 序列化用
// utils.MarshalNoEscape+sha256 等价。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"

	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ScopeInputRevision 检测自然兴趣与 scope 讨论覆盖的变化
// （topic_pipeline.py:4623-4699 scope_input_revision）。条目为
// (session_id, "completed_answers", items) / (session_id, witnesses) /
// ("deprojected-project-history", digests) 三形态。
func (p *TopicPipeline) ScopeInputRevision(contextScope string) ([]any, error) {
	if _, err := ProjectIDForScope(contextScope); err != nil {
		return nil, err
	}
	records := p.CompletedRecordsLoader()
	metadata, _ := swarmsession.GetAllSessionsMetadata(1_000_000, 0)
	projectBySession := map[string]string{}
	for _, item := range metadata {
		sessionID := pyStr(item["session_id"])
		if sessionID != "" {
			projectBySession[sessionID] = pyStr(item["project_id"])
		}
	}
	var revision []any
	var deprojectedProjectRevisions []string
	sessionIDs := make([]string, 0, len(records))
	for sessionID := range records {
		sessionIDs = append(sessionIDs, sessionID)
	}
	sort.Strings(sessionIDs)
	for _, sessionID := range sessionIDs {
		sessionProjectID, known := projectBySession[sessionID]
		if !known {
			sessionProjectID = ""
		}
		scopeMatches := PipelineScopeMatches(sessionProjectID, contextScope)
		contributesToGlobalProfile := contextScope == GlobalNewChatScope &&
			!scopeMatches && !GlobalProjectBuckets[sessionProjectID]
		if !scopeMatches && !contributesToGlobalProfile {
			continue
		}
		if scopeMatches {
			answers := CompletedAnswers(sessionID, records[sessionID])
			if len(answers) > 0 {
				// Python tuple(answers.items())：按答案键序固定形态
				keys := make([]string, 0, len(answers))
				for key := range answers {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				items := make([]any, 0, len(keys))
				for _, key := range keys {
					items = append(items, []any{key, answers[key]})
				}
				revision = append(revision, []any{sessionID, "completed_answers", items})
			}
		}
		type witnessItem struct {
			eventID   string
			timestamp float64
			content   string
			input     string
			assist    string
		}
		var witnessItems []witnessItem
		for _, record := range records[sessionID] {
			if record == nil {
				continue
			}
			role := strings.ToLower(pyStr(record["role"]))
			if role != "user" && role != "human" {
				continue
			}
			inputOrigin := pyStrOr(record["input_origin"], "pasted_or_unknown")
			if inputOrigin != ShortIntentNaturalInputOrigin {
				continue
			}
			assistanceOrigin := pyStrOr(record["assistance_origin"], "unknown")
			if assistanceOrigin != ShortIntentNaturalAssistanceOrigin {
				continue
			}
			witnessItems = append(witnessItems, witnessItem{
				eventID:   pyStrOr(record["id"], pyStr(record["request_id"])),
				timestamp: toFloatOr(record["timestamp"], 0),
				content:   pyStr(record["content"]),
				input:     inputOrigin,
				assist:    assistanceOrigin,
			})
		}
		if contributesToGlobalProfile && len(witnessItems) > 0 {
			// 该小票只决定昂贵的状态/模型工作能否跳过。这里做哈希使具名
			// 项目的文本与 locator 不进全局 scope 见证，同时仍检测改写。
			serialized := pyWitnessJSON(witnessItems)
			digest := sha256.Sum256([]byte(serialized))
			deprojectedProjectRevisions = append(deprojectedProjectRevisions,
				hex.EncodeToString(digest[:]))
		} else if len(witnessItems) > 0 {
			items := make([]any, 0, len(witnessItems))
			for _, witness := range witnessItems {
				items = append(items, []any{witness.eventID, witness.timestamp,
					witness.content, witness.input, witness.assist})
			}
			revision = append(revision, []any{sessionID, items})
		}
	}
	if len(deprojectedProjectRevisions) > 0 {
		sort.Strings(deprojectedProjectRevisions)
		digests := make([]any, 0, len(deprojectedProjectRevisions))
		for _, digest := range deprojectedProjectRevisions {
			digests = append(digests, digest)
		}
		revision = append(revision, []any{"deprojected-project-history", digests})
	}
	return revision, nil
}

// RefreshShortIntent 每个 scope 历史快照求值一次 RecGPT-Mobile OR 触发
// （topic_pipeline.py:4701-4906 refresh_short_intent）。
// 返回空串表示成功写入或无需写入；否则为失败原因码。
func (p *TopicPipeline) RefreshShortIntent(contextScope string) string {
	if _, err := ProjectIDForScope(contextScope); err != nil {
		return "invalid_context_scope"
	}
	now := p.Now()
	records := p.CompletedRecordsLoader()
	inputs := LoadGenerationInputs(p.Store, now, func() map[string][]map[string]any {
		return records
	})
	metadata, _ := swarmsession.GetAllSessionsMetadata(1_000_000, 0)
	projectBySession := map[string]string{}
	for _, item := range metadata {
		projectBySession[pyStr(item["session_id"])] = pyStr(item["project_id"])
	}
	mapping := HistoryStateMappingEntries(inputs.State)
	var scoped []*CanonicalBehavior
	for index := range inputs.ActiveBehaviors {
		behavior := &inputs.ActiveBehaviors[index]
		sessionProjectID, known := projectBySession[behavior.SessionID]
		if !known {
			sessionProjectID = ""
		}
		if PipelineScopeMatches(sessionProjectID, contextScope) {
			scoped = append(scoped, behavior)
		}
	}
	adapt := func(behavior *CanonicalBehavior) *ShortIntentBehavior {
		mapped := mapping[behavior.BehaviorIndex]
		if mapped == nil {
			mapped = map[string]any{}
		}
		sessionRecords := records[behavior.SessionID]
		record := map[string]any{}
		if 0 <= behavior.Ordinal && behavior.Ordinal < len(sessionRecords) &&
			sessionRecords[behavior.Ordinal] != nil {
			record = sessionRecords[behavior.Ordinal]
		}
		return &ShortIntentBehavior{
			EventID: pyStrOr(behavior.RecordID, fmt.Sprintf("behavior:%d",
				behavior.BehaviorIndex)),
			IntentCluster:    pyStrOr(mapped["tag"], "unmapped"),
			ObservedAt:       behavior.TimestampOr0,
			InputOrigin:      pyStrOr(record["input_origin"], "pasted_or_unknown"),
			AssistanceOrigin: pyStrOr(record["assistance_origin"], "unknown"),
			HighIntentKind:   PipelineHighIntentKind(pyStr(mapped["action_type"])),
		}
	}
	type adaptedPair struct {
		behavior *CanonicalBehavior
		v6       *ShortIntentBehavior
	}
	var natural []adaptedPair
	for _, behavior := range scoped {
		v6 := adapt(behavior)
		if v6.IsNatural() {
			natural = append(natural, adaptedPair{behavior, v6})
		}
	}
	boundary := 0
	for _, item := range natural {
		if item.behavior.BehaviorIndex > boundary {
			boundary = item.behavior.BehaviorIndex
		}
	}
	revisionPayload := make([]any, 0, len(natural))
	for _, item := range natural {
		revisionPayload = append(revisionPayload, []any{
			item.behavior.BehaviorIndex, item.v6.EventID, item.v6.IntentCluster,
			item.v6.ObservedAt, nilIfEmpty(item.v6.HighIntentKind, item.v6.HighIntentKind != ""),
		})
	}
	revisionDigest := sha256.Sum256([]byte(pyWitnessJSON(revisionPayload)))
	snapshotID := fmt.Sprintf("%s:%s:%s:%d", SHORT_INTENT_SNAPSHOT_PREFIX,
		contextScope, hex.EncodeToString(revisionDigest[:]), boundary)
	shortStore, err := NewShortIntentStateStore(p.Store)
	if err != nil {
		return "short_intent_state_invalid"
	}
	existing, err := shortStore.LoadForSnapshot(snapshotID)
	if err != nil || existing != nil {
		return ""
	}
	latest, err := shortStore.LoadLatestForScope(contextScope)
	if err != nil {
		return "short_intent_state_invalid"
	}
	if latest == nil {
		window := 20
		if len(natural) < window {
			window = len(natural)
		}
		var current []*ShortIntentBehavior
		for _, item := range natural[len(natural)-window:] {
			current = append(current, item.v6)
		}
		anchorReady := len(current) >= MinDriftSamples
		var anchor []*ShortIntentBehavior
		if anchorReady {
			anchor = current
		}
		initial, err := EvaluateShortIntent(snapshotID, anchor, current, current, true)
		if err != nil {
			return "short_intent_state_invalid"
		}
		initial.DriftAnchorSnapshotID = nilIfEmptyToStr(anchorReady, snapshotID)
		initial.DriftAnchorEventIDs = behaviorEventIDs(anchor)
		initial.DriftAnchorIntentClusters = behaviorClusters(anchor)
		if _, err := shortStore.Persist(initial, now); err != nil {
			return "short_intent_state_invalid"
		}
		return ""
	}
	parts := strings.Split(latest.InputSnapshotID, ":")
	previousBoundary := 0
	if len(parts) < 2 {
		return "short_intent_state_invalid"
	}
	parsed, parseErr := strconv.Atoi(parts[len(parts)-1])
	if parseErr != nil {
		return "short_intent_state_invalid"
	}
	previousBoundary = parsed

	if len(latest.DriftAnchorEventIDs) != len(latest.DriftAnchorIntentClusters) {
		return "short_intent_state_invalid"
	}
	var legacyAnchorState *ShortIntentState
	var reference []*ShortIntentBehavior
	if len(latest.DriftAnchorEventIDs) >= MinDriftSamples {
		for index, eventID := range latest.DriftAnchorEventIDs {
			reference = append(reference, &ShortIntentBehavior{
				EventID: eventID, IntentCluster: latest.DriftAnchorIntentClusters[index],
				InputOrigin:      ShortIntentNaturalInputOrigin,
				AssistanceOrigin: ShortIntentNaturalAssistanceOrigin,
			})
		}
	} else {
		legacyCandidate, err := shortStore.LoadLegacyAnchorForScope(contextScope)
		if err != nil {
			return "short_intent_state_invalid"
		}
		if legacyCandidate != nil {
			legacyParts := strings.Split(legacyCandidate.InputSnapshotID, ":")
			if len(legacyParts) < 2 {
				return "short_intent_state_invalid"
			}
			anchorBoundary, anchorErr := strconv.Atoi(legacyParts[len(legacyParts)-1])
			if anchorErr != nil {
				return "short_intent_state_invalid"
			}
			var legacyReference []*ShortIntentBehavior
			for _, item := range natural {
				if item.behavior.BehaviorIndex <= anchorBoundary {
					legacyReference = append(legacyReference, item.v6)
				}
			}
			if len(legacyReference) > 20 {
				legacyReference = legacyReference[len(legacyReference)-20:]
			}
			if len(legacyReference) >= MinDriftSamples {
				legacyAnchorState = legacyCandidate
				reference = legacyReference
			}
		}
	}
	var newlyEvaluated []*ShortIntentBehavior
	for _, item := range natural {
		if item.behavior.BehaviorIndex > previousBoundary {
			newlyEvaluated = append(newlyEvaluated, item.v6)
		}
	}
	var decision *ShortIntentDecision
	var anchorSnapshotID string
	var anchorEventIDs []string
	var anchorClusters []string
	if len(reference) > 0 {
		window := len(reference)
		if window > len(natural) {
			window = len(natural)
		}
		var current []*ShortIntentBehavior
		for _, item := range natural[len(natural)-window:] {
			current = append(current, item.v6)
		}
		decision, err = EvaluateShortIntent(snapshotID, reference, current, newlyEvaluated, true)
		if err != nil {
			return "short_intent_state_invalid"
		}
		if len(decision.TriggerReasons) > 0 {
			anchorSnapshotID = snapshotID
			anchorEventIDs = behaviorEventIDs(current)
			anchorClusters = behaviorClusters(current)
		} else if legacyAnchorState != nil {
			anchorSnapshotID = legacyAnchorState.InputSnapshotID
			anchorEventIDs = behaviorEventIDs(reference)
			anchorClusters = behaviorClusters(reference)
		} else {
			anchorSnapshotID = latest.DriftAnchorSnapshotID
			anchorEventIDs = latest.DriftAnchorEventIDs
			anchorClusters = latest.DriftAnchorIntentClusters
		}
	} else {
		window := 20
		if window > len(natural) {
			window = len(natural)
		}
		var current []*ShortIntentBehavior
		for _, item := range natural[len(natural)-window:] {
			current = append(current, item.v6)
		}
		baselineIsReady := len(current) >= MinDriftSamples
		var baseline []*ShortIntentBehavior
		if baselineIsReady {
			baseline = current
		}
		decision, err = EvaluateShortIntent(snapshotID, baseline, current, newlyEvaluated, true)
		if err != nil {
			return "short_intent_state_invalid"
		}
		if baselineIsReady {
			anchorSnapshotID = snapshotID
			anchorEventIDs = behaviorEventIDs(current)
			anchorClusters = behaviorClusters(current)
		}
	}
	decision.DriftAnchorSnapshotID = anchorSnapshotID
	decision.DriftAnchorEventIDs = anchorEventIDs
	decision.DriftAnchorIntentClusters = anchorClusters
	if _, err := shortStore.Persist(decision, now); err != nil {
		return "short_intent_state_invalid"
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pyWitnessJSON Python json.dumps(..., ensure_ascii=False, separators=(",", ":"))
// 等价的紧凑序列化。
func pyWitnessJSON(value any) string {
	encoded, err := utils.MarshalNoEscape(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// nilIfEmptyToStr 条件透传快照 ID（否则 None）。
func nilIfEmptyToStr(condition bool, value string) string {
	if condition {
		return value
	}
	return ""
}

// behaviorClusters 行为簇列表。
func behaviorClusters(behaviors []*ShortIntentBehavior) []string {
	out := make([]string, 0, len(behaviors))
	for _, behavior := range behaviors {
		out = append(out, behavior.IntentCluster)
	}
	return out
}
