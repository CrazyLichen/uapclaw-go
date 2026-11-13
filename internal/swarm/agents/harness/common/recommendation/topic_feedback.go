package recommendation

import (
	"encoding/json"
	"strings"
	"time"
)

// 本文件对齐 topic_feedback.py（329 行）：可审计的上下文反馈投影——
// 点击/否定反馈的精确不可变卡关联（record_v5_interaction）、动态曝光记账
// （record_v5_exposure）、槽位级信号（record_v5_slate_event）、
// 提示词证据投影（build_feedback_context）。
// Python 符号:行号逐条标注。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_feedback.py

// ──────────────────────────── 结构体 ────────────────────────────

// RecordV5InteractionOptions record_v5_interaction 的命名参数包
// （Python 关键字参数 :42-52）
type RecordV5InteractionOptions struct {
	// EventType 事件类型（仅 topic_click / not_interested）
	EventType string
	// SessionID 会话 ID
	SessionID string
	// SlateID 槽位 ID
	SlateID string
	// ManifestID 清单 ID
	ManifestID string
	// CardRevisionID 卡修订 ID
	CardRevisionID string
	// GenerationBatchID 生成批次 ID
	GenerationBatchID string
	// CreatedAt 事件时间戳
	CreatedAt float64
	// RequestID 请求 ID（可选）
	RequestID string
	// ClientPayload 客户端载荷（永不作为归因来源，立即丢弃）
	ClientPayload map[string]any
}

// RecordV5ExposureOptions record_v5_exposure 的命名参数包（Python :111-117）
type RecordV5ExposureOptions struct {
	// SessionID 会话 ID
	SessionID string
	// SlateID 槽位 ID
	SlateID string
	// Served 服务端持有的曝光内容
	Served map[string]any
	// CreatedAt 事件时间戳
	CreatedAt float64
}

// RecordV5SlateEventOptions record_v5_slate_event 的命名参数包（Python :160-168）
type RecordV5SlateEventOptions struct {
	// EventType 事件类型（post_exposure_direct / page_left / session_bound）
	EventType string
	// SessionID 会话 ID
	SessionID string
	// SlateID 槽位 ID
	SlateID string
	// ManifestID 清单 ID
	ManifestID string
	// GenerationBatchID 生成批次 ID
	GenerationBatchID string
	// CreatedAt 事件时间戳
	CreatedAt float64
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deduplicated 对齐 Python _deduplicated（:23-30）：去空、保序、去重
func deduplicated(values []any) []string {
	result := []string{}
	for _, value := range values {
		text := strings.TrimSpace(pyStrOr(value, ""))
		if text != "" && !containsString(result, text) {
			result = append(result, text)
		}
	}
	return result
}

// exactInteractionPayload 对齐 Python _exact_interaction_payload（:33-49）
func exactInteractionPayload(card map[string]any, manifestID, generationBatchID, requestID string) map[string]any {
	return map[string]any{
		"manifest_id":            manifestID,
		"card_revision_id":       card["card_revision_id"],
		"generation_batch_id":    generationBatchID,
		"request_id":             requestID,
		"interest_summary_id":    card["interest_summary_id"],
		"source_memory_unit_ids": anySliceCopy(card["source_memory_unit_ids"]),
		"source_locators":        anySliceCopy(card["source_locators"]),
		"identity_anchor":        pyStr(card["identity_anchor"]),
		"context_anchors":        anySliceCopy(card["context_anchors"]),
		"intent":                 pyStr(card["intent"]),
		"title":                  pyStr(card["title"]),
		"prompt":                 pyStr(card["prompt"]),
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RecordV5Interaction 在精确不可变卡关联之后记录点击/否定反馈
// （Python record_v5_interaction :42-108，步骤顺序逐条对应）
func RecordV5Interaction(store *TopicStarterStore, opts RecordV5InteractionOptions) bool {
	// 步骤 1：客户端文本永不作为归因来源（:53 del client_payload → Go 无需丢弃动作）

	// 步骤 2：事件类型白名单（:54-56）
	if opts.EventType != "topic_click" && opts.EventType != "not_interested" {
		return false
	}

	// 步骤 3：精确卡关联（:57-64）
	card := FindExactPoolCard(store, opts.ManifestID, opts.CardRevisionID, opts.GenerationBatchID)
	if card == nil {
		return false
	}

	// 步骤 4：构造事件载荷（:65-70）
	payload := exactInteractionPayload(card, opts.ManifestID, opts.GenerationBatchID, opts.RequestID)
	event := map[string]any{
		"session_id":        opts.SessionID,
		"slate_id":          opts.SlateID,
		"event_type":        opts.EventType,
		"topic_id":          opts.CardRevisionID,
		"policy_id":         POLICY_ID,
		"selection_mode":    "unknown",
		"propensity_reason": "not_estimated",
		"payload":           payload,
		"created_at":        opts.CreatedAt,
	}

	// 步骤 5：事件键幂等（:71-84）
	eventKey := store.EventKeyOf(event)
	existingItems, err := store.QueryEvents(QueryEventsOptions{
		EventType: opts.EventType,
		SlateID:   stringPtr(opts.SlateID),
		PolicyID:  stringPtr(POLICY_ID),
		Limit:     8,
	})
	if err != nil {
		return false
	}
	for _, existing := range existingItems {
		if pyStr(existing["event_key"]) == eventKey {
			return false
		}
	}

	// 步骤 6：点击直接落事件（:85-87）
	if opts.EventType == "topic_click" {
		recorded, err := store.RecordEvent(event)
		if err != nil {
			return false
		}
		return recorded
	}

	// 步骤 7：否定反馈写入负偏好（:89-107）
	previous, found := HistoryLoadLatestV4(store)
	var state map[string]any
	if found && previous != nil {
		state = deepCopyMap(previous)
	} else {
		state = HistoryEmptyV4State()
	}
	sidecar, _ := state["memory_sidecar"].(map[string]any)
	if sidecar == nil {
		sidecar = map[string]any{}
		state["memory_sidecar"] = sidecar
	}
	var preferences []any
	if raw, ok := sidecar["negative_preferences"].([]any); ok {
		preferences = raw
	} else {
		preferences = []any{}
	}
	sourceIDs := anySliceCopy(card["source_memory_unit_ids"])
	sourceID := ""
	if len(sourceIDs) > 0 {
		sourceID = pyStr(sourceIDs[0])
	}
	preference := map[string]any{
		"direction": pyStr(card["intent"]),
		"source_id": sourceID,
		"aliases":   deduplicated([]any{card["title"], card["prompt"]}),
		"anchors": deduplicated(append([]any{card["identity_anchor"]},
			anySliceCopy(card["context_anchors"])...)),
	}
	if !containsPreference(preferences, preference) {
		preferences = append(preferences, preference)
	}
	sidecar["negative_preferences"] = preferences
	if _, ok := sidecar["explicit_corrections"]; !ok {
		sidecar["explicit_corrections"] = []any{}
	}
	sidecar["negative_preference_version"] = toFloatOr(sidecar["negative_preference_version"], 0) + 1
	if _, err := HistorySaveV4State(store, state, []map[string]any{event}, opts.CreatedAt); err != nil {
		return false
	}
	return true
}

// RecordV5Exposure 记录动态曝光事件（Python record_v5_exposure :111-157）
func RecordV5Exposure(store *TopicStarterStore, opts RecordV5ExposureOptions) bool {
	manifestID := pyStr(opts.Served["manifest_id"])
	batchID := pyStr(opts.Served["generation_batch_id"])
	cards, cardsOK := opts.Served["cards"].([]any)
	if manifestID == "" {
		return false
	}
	if batchID == "" {
		return false
	}
	if !cardsOK {
		return false
	}
	if len(cards) == 0 {
		return false
	}
	exactCards := []map[string]any{}
	seenRevisions := map[string]struct{}{}
	for _, shown := range cards {
		shownMap, ok := shown.(map[string]any)
		if !ok {
			return false
		}
		revisionID := pyStr(shownMap["card_revision_id"])
		if revisionID == "" {
			return false
		}
		if _, dup := seenRevisions[revisionID]; dup {
			return false
		}
		exact := FindExactPoolCard(store, manifestID, revisionID, batchID)
		if exact == nil {
			return false
		}
		seenRevisions[revisionID] = struct{}{}
		exactCards = append(exactCards, exact)
	}
	selection := ClassifyStarterSlate(exactCards)
	recorded, err := store.RecordEvent(map[string]any{
		"session_id":        opts.SessionID,
		"slate_id":          opts.SlateID,
		"event_type":        "dynamic_exposure",
		"topic_id":          manifestID,
		"policy_id":         POLICY_ID,
		"selection_mode":    selection["selection_mode"],
		"propensity_reason": "not_estimated",
		"payload": map[string]any{
			"manifest_id":         manifestID,
			"generation_batch_id": batchID,
			"topics":              exposureTopics(exactCards),
		},
		"created_at": opts.CreatedAt,
	})
	if err != nil {
		return false
	}
	return recorded
}

// RecordV5SlateEvent 仅以服务端语义与 ID 记录 v5 槽位级信号
// （Python record_v5_slate_event :160-193，步骤顺序逐条对应）
func RecordV5SlateEvent(store *TopicStarterStore, opts RecordV5SlateEventOptions) bool {
	// 步骤 1：事件类型白名单（:169-171）
	if opts.EventType != "post_exposure_direct" && opts.EventType != "page_left" &&
		opts.EventType != "session_bound" {
		return false
	}
	// 步骤 2：清单存在性校验（:172-185）
	eventItems, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_slate_manifest",
		SlateID:   stringPtr(opts.ManifestID),
		PolicyID:  stringPtr(POLICY_ID),
		Limit:     1,
	})
	if err != nil {
		return false
	}
	manifestExists := false
	for _, event := range eventItems {
		payload, ok := event["payload"].(map[string]any)
		if !ok {
			continue
		}
		if pyStr(payload["manifest_id"]) == opts.ManifestID &&
			pyStr(payload["generation_batch_id"]) == opts.GenerationBatchID {
			manifestExists = true
		}
	}
	if !manifestExists {
		return false
	}
	// 步骤 3：落事件（:186-193）
	recorded, err := store.RecordEvent(map[string]any{
		"session_id":        opts.SessionID,
		"slate_id":          opts.SlateID,
		"event_type":        opts.EventType,
		"topic_id":          nil,
		"policy_id":         POLICY_ID,
		"selection_mode":    "unknown",
		"propensity_reason": "not_estimated",
		"payload": map[string]any{
			"manifest_id":         opts.ManifestID,
			"generation_batch_id": opts.GenerationBatchID,
		},
		"created_at": opts.CreatedAt,
	})
	if err != nil {
		return false
	}
	return recorded
}

// BuildFeedbackContext 投影提示词证据：计数加显式示例，永不出分
// （Python build_feedback_context :196-290，步骤顺序逐条对应）
func BuildFeedbackContext(store *TopicStarterStore, interestSummaryIDs []any) map[string]any {
	// 步骤 1：去重标识（:200）
	identifiers := deduplicated(interestSummaryIDs)

	// 步骤 2：计数表与桶初始化（:201-208）
	counts := map[string]map[string]any{}
	exposureDays := map[string]map[string]struct{}{}
	for _, identifier := range identifiers {
		counts[identifier] = map[string]any{"exposure_days": 0, "clicks": 0, "not_interested": 0}
		exposureDays[identifier] = map[string]struct{}{}
	}

	// 步骤 3：拉取四类事件（:209-227）
	events := []map[string]any{}
	for _, eventType := range []string{
		"dynamic_exposure", "topic_click", "not_interested", "post_exposure_direct",
	} {
		rows, err := store.QueryEvents(QueryEventsOptions{
			EventType:   eventType,
			PolicyID:    stringPtr(POLICY_ID),
			Limit:       256,
			NewestFirst: true,
		})
		if err != nil {
			continue
		}
		events = append(events, rows...)
	}

	// 步骤 4：事件投影（:228-279）
	positive := []any{}
	negative := []any{}
	slateSignals := []any{}
	for _, event := range events {
		eventType := pyStr(event["event_type"])
		payload, _ := event["payload"].(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		if eventType == "dynamic_exposure" {
			localDay := localDayString(toFloatOr(event["created_at"], 0))
			for _, topic := range anySliceCopy(payload["topics"]) {
				topicMap, ok := topic.(map[string]any)
				if !ok {
					continue
				}
				identifier := pyStr(topicMap["interest_summary_id"])
				if identifier == "" {
					identifier = pyStr(topicMap["id"])
				}
				if _, known := exposureDays[identifier]; known {
					exposureDays[identifier][localDay] = struct{}{}
				}
			}
			continue
		}
		if eventType == "topic_click" || eventType == "not_interested" {
			identifier := pyStr(payload["interest_summary_id"])
			exemplar := map[string]any{
				"interest_summary_id": nilValueIfEmpty(identifier),
				"card_revision_id":    payload["card_revision_id"],
				"title":               payload["title"],
				"prompt":              payload["prompt"],
				"intent":              payload["intent"],
				"observed_at":         toFloatOr(event["created_at"], 0),
			}
			if eventType == "topic_click" {
				if counter, known := counts[identifier]; known {
					counter["clicks"] = toFloatOr(counter["clicks"], 0) + 1
				}
				positive = append(positive, exemplar)
			} else {
				if counter, known := counts[identifier]; known {
					counter["not_interested"] = toFloatOr(counter["not_interested"], 0) + 1
				}
				negative = append(negative, exemplar)
			}
			continue
		}
		if eventType == "post_exposure_direct" {
			slateSignals = append(slateSignals, map[string]any{
				"event_type": "post_exposure_direct",
				"slate_id":   pyStr(event["slate_id"]),
			})
		}
	}

	// 步骤 5：曝光天数回填（:280-282）
	for _, identifier := range identifiers {
		counts[identifier]["exposure_days"] = len(exposureDays[identifier])
	}

	// 步骤 6：反馈上下文（:283-290）
	return map[string]any{
		"feedback_counts":    anyValueMapOfMaps(counts),
		"positive_exemplars": positive,
		"negative_exemplars": negative,
		"slate_signals":      slateSignals,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// exposureTopics 对齐 Python :144-150 的 topics 列表推导
func exposureTopics(exactCards []map[string]any) []any {
	out := make([]any, 0, len(exactCards))
	for _, card := range exactCards {
		out = append(out, map[string]any{
			"id":                  card["interest_summary_id"],
			"interest_summary_id": card["interest_summary_id"],
			"card_revision_id":    card["card_revision_id"],
		})
	}
	return out
}

// localDayString 对齐 Python time.strftime("%Y-%m-%d", time.localtime(ts))
func localDayString(ts float64) string {
	return time.Unix(int64(ts), 0).Format("2006-01-02")
}

// nilValueIfEmpty 空串转 nil（对齐 Python identifier or None）
func nilValueIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// containsString 保序包含判定
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// containsPreference 负偏好去重（对齐 Python `preference not in preferences`）
func containsPreference(preferences []any, preference map[string]any) bool {
	for _, raw := range preferences {
		existing, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if anyMapEqual(existing, preference) {
			return true
		}
	}
	return false
}

// deepCopyMap 经 JSON 往返的深拷贝（对齐 Python json.loads(json.dumps(...))）
func deepCopyMap(source map[string]any) map[string]any {
	data := mustMarshalNoEscape(source)
	decoded := map[string]any{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}

// anySliceCopy 列表拷贝（nil → 空列表，对齐 Python list(x or [])）
func anySliceCopy(value any) []any {
	if list, ok := value.([]any); ok {
		out := make([]any, 0, len(list))
		out = append(out, list...)
		return out
	}
	return []any{}
}
