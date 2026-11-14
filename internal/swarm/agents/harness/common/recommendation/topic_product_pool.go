package recommendation

// topic_product_pool.go topic 终局池的写入、冷启动与曝光选卡。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_product.py
// :4390-4551、:4635-5175、:5286-5465（校验族与卡构建已随 31b/校验族切片
// 落 topic_product_validate*.go / topic_pipeline_*.go，本文件为剩余收口）。

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// currentSearchLanes 当前在役搜索 lane（topic_product.py:4449-4454）。
var currentSearchLanes = map[string]bool{
	OpenjiuwenNativeSearchProviderId + "|" + OpenjiuwenNativeSearchReceiptVersion: true,
	NewsFeedProviderID + "|" + NewsFeedReceiptVersion:                             true,
}

// currentSearchProviderIds 当前在役厂商（:4455-4458）。
var currentSearchProviderIds = map[string]bool{
	OpenjiuwenNativeSearchProviderId: true,
	NewsFeedProviderID:               true,
}

// evidenceReceiptVersions 证据小票版本集（:4487-4491）。
var evidenceReceiptVersions = map[string]bool{
	OpenjiuwenNativeSearchReceiptVersion:         true,
	OpenjiuwenNativeSearchFailureReceiptVersion:  true,
	NewsFeedReceiptVersion:                       true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsColdStartManifest 冷启动 manifest 判定（对齐 :4390-4392）。
func IsColdStartManifest(manifest any) bool {
	m, ok := manifest.(map[string]any)
	return ok && m["cold_start"] == true
}


// CurrentSearchLineageRejectionReason 当前搜索血缘拒绝（对齐 :4460-4519
// _current_search_lineage_rejection_reason）。
func CurrentSearchLineageRejectionReason(manifest map[string]any, cards []map[string]any) string {
	failure := "search_lineage_contract_failed"
	trigger, _ := manifest["generation_trigger_key"].(map[string]any)
	if trigger == nil {
		return failure
	}
	if pyStr(manifest["search_provider_id"]) != OpenjiuwenNativeSearchProviderId ||
		pyStr(trigger["search_provider_id"]) != OpenjiuwenNativeSearchProviderId {
		return failure
	}
	manifestReceipts, _ := manifest["search_receipts"].(map[string]any)
	for _, card := range cards {
		cardReceipt := card["search_receipt"]
		manifestReceipt := manifestReceipts[pyStr(card["card_id"])]
		if CardUsesHostEvidence(card) {
			if cardReceipt != nil || manifestReceipt != nil {
				return failure
			}
			continue
		}
		if pyStr(card["search_provider_id"]) != OpenjiuwenNativeSearchProviderId {
			return failure
		}
		if !anyEqual(cardReceipt, manifestReceipt) {
			return failure
		}
		if !ValidSearchReceipt(cardReceipt) {
			return failure
		}
		receiptMap, _ := cardReceipt.(map[string]any)
		if receiptMap == nil {
			return failure
		}
		if !currentSearchLanes[pyStr(receiptMap["provider_id"])+"|"+pyStr(receiptMap["receipt_version"])] {
			return failure
		}
	}
	groundingEvidence, _ := manifest["grounding_evidence_receipt"].(map[string]any)
	if groundingEvidence == nil {
		return failure
	}
	candidates, _ := groundingEvidence["candidates"].([]any)
	for _, raw := range candidates {
		candidate, _ := raw.(map[string]any)
		if candidate == nil {
			continue
		}
		search, _ := candidate["search"].(map[string]any)
		if search == nil {
			continue
		}
		providerReceipt, _ := search["provider_receipt"].(map[string]any)
		if providerReceipt != nil &&
			(!currentSearchProviderIds[pyStr(providerReceipt["provider_id"])] ||
				!evidenceReceiptVersions[pyStr(providerReceipt["receipt_version"])]) {
			return failure
		}
	}
	audit, _ := manifest["external_invocation_audit"].(map[string]any)
	if audit == nil {
		return failure
	}
	invocations, _ := audit["invocations"].([]any)
	for _, raw := range invocations {
		invocation, _ := raw.(map[string]any)
		if invocation == nil {
			continue
		}
		if pyStr(invocation["operation"]) == "search" &&
			!currentSearchProviderIds[pyStr(invocation["provider_id"])] {
			return failure
		}
	}
	return ""
}

// TerminalPoolRejectionReason 终局池整池拒绝原因（对齐 :4520-4551）。
func TerminalPoolRejectionReason(manifest map[string]any, cards []map[string]any, expectedModel string) string {
	orderedCards := OrderedCardsForDiagnosis(cards)
	for _, card := range orderedCards {
		if card == nil {
			return "terminal_pool_contract_failed"
		}
		for _, bound := range []struct {
			field    string
			maxChars int
		}{{"title", CardTitleMaxChars}, {"prompt", CardPromptMaxChars}} {
			reason := CardCopyRejectionReason(card[bound.field], bound.maxChars)
			if reason != "" {
				return fmt.Sprintf("card_%s_%s", bound.field, reason)
			}
		}
	}
	manifestReason := ManifestRejectionReason(manifest, cards, false)
	if manifestReason != "" {
		return manifestReason
	}
	currentSearchReason := CurrentSearchLineageRejectionReason(manifest, cards)
	if currentSearchReason != "" {
		return currentSearchReason
	}
	return TerminalModelBindingRejectionReason(manifest, cards, expectedModel)
}

// ShortDeltaSuccessorRejectionReason 短增量后继可见新颖性拒绝
// （对齐 :4635-4670）。
func ShortDeltaSuccessorRejectionReason(store *TopicStarterStore, manifest map[string]any, cards []map[string]any) string {
	deltaInterestID, _, hasValue := ShortDeltaInterestID(manifest)
	predecessorManifestID := manifest["supersedes_manifest_id"]
	if pyStr(manifest["context_scope"]) != GlobalNewChatScope {
		return ""
	}
	if !hasValue {
		return ""
	}
	if predecessorManifestID == nil {
		return ""
	}
	predecessorManifest, predecessorCards := LoadExactGlobalPool(store, predecessorManifestID)
	if predecessorManifest == nil {
		return "manifest_short_delta_predecessor_exact_load_failed"
	}
	predecessorCopy := map[string]bool{}
	for _, card := range predecessorCards {
		predecessorCopy[pyStr(card["category"])+"|"+pyStr(card["title"])+"|"+pyStr(card["prompt"])] = true
	}
	ordered := OrderedCardsForDiagnosis(cards)
	limit := 4
	if len(ordered) < limit {
		limit = len(ordered)
	}
	firstExposure := ordered[:limit]
	for _, card := range firstExposure {
		if pyStr(card["interest_summary_id"]) == deltaInterestID &&
			!predecessorCopy[pyStr(card["category"])+"|"+pyStr(card["title"])+"|"+pyStr(card["prompt"])] {
			return ""
		}
	}
	return "manifest_short_delta_visible_novelty_failed"
}

// TerminalPoolWriteResult 整池写入结果（对齐 :4441-4458）。
type TerminalPoolWriteResult struct {
	// Written 是否写入
	Written bool
	// Reason 拒绝原因（成功为空串）
	Reason string
}

// Bool 布尔等价（对齐 __bool__）。
func (r TerminalPoolWriteResult) Bool() bool { return r.Written }

// WriteTerminalPool 原子追加一个 1-7 卡全局池或 4 卡项目池
// （对齐 :4671-4734 write_terminal_pool）。
func WriteTerminalPool(store *TopicStarterStore, cards []map[string]any, manifest map[string]any,
	createdAt float64, expectedModel string) TerminalPoolWriteResult {
	// corpus_text_by_id 参数上游即 del（:4679），无消费面，Go 不收
	rejectionReason := TerminalPoolRejectionReason(manifest, cards, expectedModel)
	if rejectionReason != "" {
		return TerminalPoolWriteResult{Written: false, Reason: rejectionReason}
	}
	successorReason := ShortDeltaSuccessorRejectionReason(store, manifest, cards)
	if successorReason != "" {
		return TerminalPoolWriteResult{Written: false, Reason: successorReason}
	}
	manifestID := pyStr(manifest["manifest_id"])
	events := make([]map[string]any, 0, len(cards)+1)
	for _, card := range cards {
		events = append(events, map[string]any{
			"session_id":        "v6-pool",
			"slate_id":          manifestID,
			"event_type":        "starter_card",
			"topic_id":          card["card_revision_id"],
			"position":          card["selector_rank"],
			"profile_version":   card["profile_semantic_version"],
			"policy_id":         ProductPolicyID,
			"selection_mode":    "pool_generation",
			"propensity_reason": "not_estimated",
			"payload":           card,
			"created_at":        createdAt,
		})
	}
	events = append(events, map[string]any{
		"session_id":        "v6-pool",
		"slate_id":          manifestID,
		"event_type":        "starter_slate_manifest",
		"topic_id":          manifestID,
		"profile_version":   manifest["profile_semantic_version"],
		"policy_id":         ProductPolicyID,
		"selection_mode":    "pool_generation",
		"propensity_reason": "not_estimated",
		"payload":           manifest,
		"created_at":        createdAt,
	})
	written, _ := store.RecordEventsAtomic(events)
	if !written {
		return TerminalPoolWriteResult{Written: false, Reason: "atomic_write_failed"}
	}
	return TerminalPoolWriteResult{Written: true, Reason: ""}
}

// CorpusCategory 语料话题分类（对齐 :4735-4742 _corpus_category）。
func CorpusCategory(topic string) string {
	switch topic {
	case "科学", "文化", "好奇":
		return "adjacent_explanation"
	default:
		return "broad_timely_practical"
	}
}

// CorpusStarterIsLive 语料起始语在窗判定（对齐 :4743-4753）。
func CorpusStarterIsLive(starter *FixedStarter, now float64) bool {
	if starter.ExpiresAt == nil {
		return true
	}
	if created, ok := ParseISOTimestamp(starter.CreatedAt); ok && now < created {
		return false
	}
	expires, ok := ParseISOTimestamp(pyStr(starter.ExpiresAt))
	return ok && now < expires
}

// CorpusCardExpireTime 语料卡有效期（对齐 :4754-4757）。
func CorpusCardExpireTime(starter *FixedStarter, now float64) float64 {
	expireTime := now + 7*24*60*60
	if expires, ok := ParseISOTimestamp(pyStr(starter.ExpiresAt)); ok {
		if expires < expireTime {
			return expires
		}
	}
	return expireTime
}

// ColdStartCorpusCard 语料冷启动卡（对齐 :4759-4847）。
func ColdStartCorpusCard(starter *FixedStarter, rank int, batchID string,
	triggerKey map[string]any, now float64) map[string]any {
	text := strings.TrimSpace(starter.Text)
	if reason := CardCopyRejectionReason(text, CardPromptMaxChars); reason != "" {
		return nil
	}
	title := text
	if len([]rune(text)) > CardTitleMaxChars {
		title = string([]rune(text)[:CardTitleMaxChars])
	}
	if reason := CardCopyRejectionReason(title, CardTitleMaxChars); reason != "" {
		return nil
	}
	candidateID := fmt.Sprintf("cold-corpus:%s", starter.StarterID)
	documentRevisionID := fmt.Sprintf("corpus:%s:%s", starter.CorpusVersion, starter.StarterID)
	snapshotID := ""
	if receipt, ok := triggerKey["input_revision_receipt"].(map[string]any); ok {
		snapshotID = pyStr(receipt["receipt_id"])
	}
	inputSnapshotID := snapshotID
	if inputSnapshotID == "" {
		inputSnapshotID = fmt.Sprintf("v6-cold-start:%s", pyStr(triggerKey["local_date"]))
	}
	return map[string]any{
		"card_id":              candidateID,
		"card_revision_id":     fmt.Sprintf("v6-revision-%s", newUUIDHex()),
		"generation_batch_id":  batchID,
		"selector_rank":        rank,
		"category":             CorpusCategory(starter.Topic),
		"context_scope":        GlobalNewChatScope,
		"project_id":           nil,
		"interest_summary_id":  nil,
		"source_starter_id":    starter.StarterID,
		"intent":               title,
		"title":                title,
		"prompt":               text,
		"effective_time":       now,
		"expire_time":          CorpusCardExpireTime(starter, now),
		"evidence_bundle": map[string]any{
			"candidate_id":        candidateID,
			"fetch_status":        "success",
			"source_url":          fmt.Sprintf("corpus://%s/%s", starter.CorpusVersion, starter.StarterID),
			"source_kind":         "stable",
			"publisher":           "packaged_corpus",
			"document_revision_id": documentRevisionID,
			"retrieved_at":        unixToISO(now),
			"snapshot_date":       unixToDate(now),
			"snapshot_version":    fmt.Sprintf("corpus:%s", starter.CorpusVersion),
			"source_gate_mode":    "fixed_corpus_entry",
			"content_boundary":    map[string]any{"start": 0, "end": len(text), "text": text},
			"exact_spans": []any{map[string]any{
				"candidate_id": candidateID, "document_revision_id": documentRevisionID,
				"start": 0, "end": len(text), "exact_text": text,
			}},
			"host_receipt": map[string]any{
				"receipt_version":      FixedCorpusReceiptVersion,
				"lane":                 "host_fixed_corpus_entry",
				"input_snapshot_id":    inputSnapshotID,
				"corpus_version":       starter.CorpusVersion,
				"starter_id":           starter.StarterID,
				"document_revision_id": documentRevisionID,
			},
		},
		"search_provider_id":             triggerKey["search_provider_id"],
		"search_receipt":                 nil,
		"source_memory_unit_ids":         []any{},
		"source_locators":                []any{},
		"project_source_locators":        []any{},
		"generation_input_snapshot_id":   inputSnapshotID,
		"profile_semantic_version":       triggerKey["profile_semantic_version"],
		"short_intent_version":           triggerKey["short_intent_version"],
		"component_receipts":             map[string]any{},
		"component_invocation_receipts":  []any{},
		"provider_invocation_receipts":   []any{},
		"generation_config_version":      triggerKey["generation_config_version"],
		"corpus_version":                 triggerKey["corpus_version"],
		"document_snapshot_version":      triggerKey["document_snapshot_version"],
		"retrieval_config_version":       triggerKey["retrieval_config_version"],
		"evidence_extractor_version":     triggerKey["evidence_extractor_version"],
		"list_search_version":            LIST_SEARCH_VERSION,
		"policy_id":                      ProductPolicyID,
		"payload_version":                ProductPayloadVersion,
		"cold_start":                     true,
	}
}


// ColdStartHotListCard 热榜冷启动卡（对齐 :4852-4949）。
func ColdStartHotListCard(entry *HotListEntry, pull *HotListPull, rank int, batchID string,
	triggerKey map[string]any, now float64) map[string]any {
	title := HotListTitle(entry.Word)
	if reason := CardCopyRejectionReason(title, CardTitleMaxChars); reason != "" {
		title = strings.TrimSpace(entry.Word)
	}
	if reason := CardCopyRejectionReason(title, CardTitleMaxChars); reason != "" {
		return nil
	}
	text := entry.Word
	if entry.Summary != nil {
		text = fmt.Sprintf("%s\n%s", entry.Word, pyStr(entry.Summary))
	}
	candidateID := fmt.Sprintf("cold-hot:%s", entry.EntryID)
	documentRevisionID := fmt.Sprintf("hot-list:%s:%s", pull.ReceiptID, entry.EntryID)
	snapshotID := ""
	if receipt, ok := triggerKey["input_revision_receipt"].(map[string]any); ok {
		snapshotID = pyStr(receipt["receipt_id"])
	}
	inputSnapshotID := snapshotID
	if inputSnapshotID == "" {
		inputSnapshotID = fmt.Sprintf("v6-cold-start:%s", pyStr(triggerKey["local_date"]))
	}
	listHost := strings.TrimPrefix(pyHostname(pull.ListURL), "www.")
	var listedAt any
	if entry.ListedAt != nil {
		if seconds, ok := entry.ListedAt.(float64); ok {
			listedAt = unixToISO(seconds)
		} else {
			listedAt = nil
		}
	}
	return map[string]any{
		"card_id":              candidateID,
		"card_revision_id":     fmt.Sprintf("v6-revision-%s", newUUIDHex()),
		"generation_batch_id":  batchID,
		"selector_rank":        rank,
		"category":             "profile_recent_progress",
		"context_scope":        GlobalNewChatScope,
		"project_id":           nil,
		"interest_summary_id":  nil,
		"source_starter_id":    nil,
		"intent":               fmt.Sprintf("弄清热搜「%s」说的是什么事", entry.Word),
		"title":                title,
		"prompt":               title,
		"news_match":           "closest",
		"effective_time":       now,
		"expire_time":          now + 24*60*60,
		"evidence_bundle": map[string]any{
			"candidate_id":        candidateID,
			"fetch_status":        "success",
			"source_url":          SearchPageURL(entry.Word),
			"search_url":          SearchPageURL(entry.Word),
			"source_kind":         HotListSourceKind,
			"publisher":           listHost,
			"document_revision_id": documentRevisionID,
			"retrieved_at":        unixToISO(pull.PulledAt),
			"snapshot_date":       unixToDate(pull.PulledAt),
			"snapshot_version":    fmt.Sprintf("hot-list:%s", pull.ReceiptID),
			"source_gate_mode":    "hot_list_entry",
			"listed_at":           listedAt,
			"content_boundary":    map[string]any{"start": 0, "end": len(text), "text": text},
			"exact_spans": []any{map[string]any{
				"candidate_id": candidateID, "document_revision_id": documentRevisionID,
				"start": 0, "end": len(text), "exact_text": text,
			}},
			"host_receipt": map[string]any{
				"receipt_version":      HotListEntryReceiptVersion,
				"lane":                 "host_hot_list_entry",
				"input_snapshot_id":    inputSnapshotID,
				"pull_receipt_id":      pull.ReceiptID,
				"provider_id":          entry.ProviderID,
				"list_url":             pull.ListURL,
				"hot_entry_id":         entry.EntryID,
				"hot_list_rank":        entry.Rank,
				"document_revision_id": documentRevisionID,
			},
		},
		"search_provider_id":             triggerKey["search_provider_id"],
		"search_receipt":                 nil,
		"source_memory_unit_ids":         []any{},
		"source_locators":                []any{},
		"project_source_locators":        []any{},
		"generation_input_snapshot_id":   inputSnapshotID,
		"profile_semantic_version":       triggerKey["profile_semantic_version"],
		"short_intent_version":           triggerKey["short_intent_version"],
		"component_receipts":             map[string]any{},
		"component_invocation_receipts":  []any{},
		"provider_invocation_receipts":   []any{},
		"generation_config_version":      triggerKey["generation_config_version"],
		"corpus_version":                 triggerKey["corpus_version"],
		"document_snapshot_version":      triggerKey["document_snapshot_version"],
		"retrieval_config_version":       triggerKey["retrieval_config_version"],
		"evidence_extractor_version":     triggerKey["evidence_extractor_version"],
		"list_search_version":            LIST_SEARCH_VERSION,
		"policy_id":                      ProductPolicyID,
		"payload_version":                ProductPayloadVersion,
		"cold_start":                     true,
	}
}

// BuildColdStartCards 打包语料卡 + 仍在需求内的缓存热榜卡（对齐 :4950-5034）。
func BuildColdStartCards(triggerKey map[string]any, now float64, hotListPull *HotListPull,
	feedback *InterestFeedback, targetCount int, exclude func(map[string]any) bool) []map[string]any {
	cards := make([]map[string]any, 0)
	batchID := fmt.Sprintf("v6-batch-%s", newUUIDHex())
	seed := 0
	for index, ch := range pyStr(triggerKey["local_date"]) {
		seed += int(ch) * (index + 1)
	}
	rng := rand.New(rand.NewSource(int64(seed)))
	if hotListPull != nil {
		for _, entry := range hotListPull.Entries {
			news := ColdStartHotListCard(entry, hotListPull, 1, batchID, triggerKey, now)
			if news == nil {
				continue
			}
			if exclude != nil && exclude(news) {
				continue
			}
			if feedback != nil && !feedback.StillWanted(news) {
				continue
			}
			cards = append(cards, news)
			break
		}
	}
	starters, err := LoadFixedStarterCorpus(CorpusVersion)
	if err != nil {
		starters = nil
	}
	live := make([]*FixedStarter, 0)
	for i := range starters {
		if CorpusStarterIsLive(&starters[i], now) {
			live = append(live, &starters[i])
		}
	}
	byTopic := map[string][]*FixedStarter{}
	for _, starter := range live {
		byTopic[starter.Topic] = append(byTopic[starter.Topic], starter)
	}
	pickedIDs := map[string]bool{}
	appendCard := func(starter *FixedStarter) bool {
		card := ColdStartCorpusCard(starter, len(cards)+1, batchID, triggerKey, now)
		if card == nil {
			return false
		}
		if exclude != nil && exclude(card) {
			return false
		}
		if feedback != nil && !feedback.StillWanted(card) {
			return false
		}
		cards = append(cards, card)
		pickedIDs[starter.StarterID] = true
		return true
	}
	timely := make([]*FixedStarter, 0)
	for _, starter := range live {
		if starter.FreshnessClass == "current_event" || starter.FreshnessClass == "recent_development" {
			timely = append(timely, starter)
		}
	}
	rng.Shuffle(len(timely), func(i, j int) { timely[i], timely[j] = timely[j], timely[i] })
	timelyLimit := 2
	if hotListPull == nil {
		timelyLimit = 1
	}
	for _, starter := range timely {
		if len(cards) >= targetCount || len(pickedIDs) >= timelyLimit {
			break
		}
		appendCard(starter)
	}
	topics := make([]string, 0, len(byTopic))
	for topic := range byTopic {
		topics = append(topics, topic)
	}
	rng.Shuffle(len(topics), func(i, j int) { topics[i], topics[j] = topics[j], topics[i] })
	for _, topic := range topics {
		if len(cards) >= targetCount {
			break
		}
		bucket := append([]*FixedStarter{}, byTopic[topic]...)
		rng.Shuffle(len(bucket), func(i, j int) { bucket[i], bucket[j] = bucket[j], bucket[i] })
		for _, starter := range bucket {
			if pickedIDs[starter.StarterID] {
				continue
			}
			if appendCard(starter) {
				break
			}
		}
	}
	for rank, card := range cards {
		card["selector_rank"] = rank + 1
		card["generation_batch_id"] = batchID
	}
	if len(cards) > targetCount {
		cards = cards[:targetCount]
	}
	return cards
}

// WriteColdStartPool 持久化首次欢迎访问可服务的通用/热榜卡（对齐 :5035-5108）。
func WriteColdStartPool(store *TopicStarterStore, cards []map[string]any,
	triggerKey map[string]any, createdAt float64) TerminalPoolWriteResult {
	if len(cards) == 0 {
		return TerminalPoolWriteResult{Written: false, Reason: "cold_start_empty"}
	}
	batchID := pyStr(cards[0]["generation_batch_id"])
	manifestID := fmt.Sprintf("v6-manifest-%s", newUUIDHex())
	snapshotID := pyStr(cards[0]["generation_input_snapshot_id"])
	manifest := map[string]any{
		"manifest_id":         manifestID,
		"generation_batch_id": batchID,
		"card_revision_ids":   cardRevisionIDs(cards),
		"trigger_origin":      "lazy_seed",
		"local_date":          triggerKey["local_date"],
		"context_scope":       GlobalNewChatScope,
		"project_id":          nil,
		"generation_trigger_key":     poolCloneMap(triggerKey),
		"generation_input_snapshot_id": snapshotID,
		"serving_compatibility_key":    BuildServingCompatibilityKey(poolCloneMap(triggerKey)),
		"profile_semantic_version":     triggerKey["profile_semantic_version"],
		"short_intent_version":         triggerKey["short_intent_version"],
		"negative_preference_version":  triggerKey["negative_preference_version"],
		"component_receipts":           map[string]any{},
		"generation_config_version":    triggerKey["generation_config_version"],
		"search_provider_id":           triggerKey["search_provider_id"],
		"policy_id":                    ProductPolicyID,
		"payload_version":              ProductPayloadVersion,
		"cold_start":                   true,
	}
	if !ValidColdStartPool(manifest, cards) {
		return TerminalPoolWriteResult{Written: false, Reason: "cold_start_contract_failed"}
	}
	events := make([]map[string]any, 0, len(cards)+1)
	for _, card := range cards {
		events = append(events, map[string]any{
			"session_id":        "v6-pool",
			"slate_id":          manifestID,
			"event_type":        "starter_card",
			"topic_id":          card["card_revision_id"],
			"position":          card["selector_rank"],
			"profile_version":   card["profile_semantic_version"],
			"policy_id":         ProductPolicyID,
			"selection_mode":    "generic_dynamic",
			"propensity_reason": "not_estimated",
			"payload":           card,
			"created_at":        createdAt,
		})
	}
	events = append(events, map[string]any{
		"session_id":        "v6-pool",
		"slate_id":          manifestID,
		"event_type":        "starter_slate_manifest",
		"topic_id":          manifestID,
		"profile_version":   manifest["profile_semantic_version"],
		"policy_id":         ProductPolicyID,
		"selection_mode":    "generic_dynamic",
		"propensity_reason": "not_estimated",
		"payload":           manifest,
		"created_at":        createdAt,
	})
	written, _ := store.RecordEventsAtomic(events)
	if !written {
		return TerminalPoolWriteResult{Written: false, Reason: "atomic_write_failed"}
	}
	return TerminalPoolWriteResult{Written: true, Reason: ""}
}

// SelectExposure 从池中选一屏；shown_before 是上一窗已展示的卡
// （对齐 :5286-5361 _select_exposure）。
func SelectExposure(cards []map[string]any, eligibleRevisionIDs map[string]bool,
	contextScope string, windowIndex int, now float64, shownBefore map[string]bool,
	reducedRevisionIDs map[string]bool, retainedCardIDs []string) []map[string]any {
	if retainedCardIDs != nil {
		eligible := make([]map[string]any, 0)
		for _, card := range cards {
			if eligibleRevisionIDs[pyStr(card["card_revision_id"])] {
				eligible = append(eligible, card)
			}
		}
		newsEligible := differenceSet(eligibleRevisionIDs, reducedRevisionIDs)
		var news map[string]any
		if contextScope == GlobalNewChatScope {
			news = FirstSlotNewsCard(cards, newsEligible, now)
		}
		byID := map[string]map[string]any{}
		for _, card := range eligible {
			byID[pyStr(card["card_id"])] = card
		}
		selected := make([]map[string]any, 0)
		if news != nil {
			selected = append(selected, news)
		}
		retained := make([]map[string]any, 0)
		for _, cardID := range retainedCardIDs {
			if card, ok := byID[cardID]; ok {
				retained = append(retained, card)
			}
		}
		regular := make([]map[string]any, 0)
		reduced := make([]map[string]any, 0)
		for _, card := range eligible {
			revisionID := pyStr(card["card_revision_id"])
			if reducedRevisionIDs[revisionID] {
				reduced = append(reduced, card)
			} else {
				regular = append(regular, card)
			}
		}
		reducedCount := 0
		selectedDirections := map[string]bool{}
		type pass struct {
			candidates []map[string]any
			spread     bool
		}
		var passes []pass
		if contextScope == GlobalNewChatScope {
			passes = []pass{{retained, false}, {regular, true}, {append(append([]map[string]any{}, regular...), reduced...), false}}
		} else {
			passes = []pass{{append(append(append([]map[string]any{}, retained...), regular...), reduced...), false}}
		}
		for _, p := range passes {
			for _, card := range p.candidates {
				if len(selected) == FIRST_SCREEN_CARD_TARGET {
					break
				}
				if containsCard(selected, card) || (contextScope == GlobalNewChatScope && CardIsNews(card)) {
					continue
				}
				revisionID := pyStr(card["card_revision_id"])
				var direction string
				if contextScope == GlobalNewChatScope {
					interestID := pyStr(card["interest_summary_id"])
					starterID := pyStr(card["source_starter_id"])
					if interestID == "" && starterID == "" && !pythonTruthy(card["cold_start"]) {
						continue
					}
					direction = poolFirstNonEmpty(pyStr(card["direction"]), interestID, starterID, pyStr(card["card_id"]))
				} else {
					direction = poolFirstNonEmpty(pyStr(card["direction"]), pyStr(card["interest_summary_id"]))
				}
				if p.spread && selectedDirections[direction] {
					continue
				}
				if reducedRevisionIDs[revisionID] {
					if reducedCount > 0 {
						continue
					}
					reducedCount++
				}
				selected = append(selected, card)
				selectedDirections[direction] = true
			}
		}
		sortCardsReducedLast(selected, reducedRevisionIDs)
		return selected
	}
	reduced := make([]map[string]any, 0)
	for _, card := range cards {
		revisionID := pyStr(card["card_revision_id"])
		if eligibleRevisionIDs[revisionID] && reducedRevisionIDs[revisionID] {
			reduced = append(reduced, card)
		}
	}
	selected := SelectRegularExposure(cards, differenceSet(eligibleRevisionIDs, reducedRevisionIDs),
		contextScope, windowIndex, now, shownBefore)
	if len(reduced) > 0 && len(selected) < FIRST_SCREEN_CARD_TARGET {
		selected = append(selected, reduced[0])
	}
	return selected
}

// SelectRegularExposure 常规曝光选卡（对齐 :5363-5430）。
func SelectRegularExposure(cards []map[string]any, eligibleRevisionIDs map[string]bool,
	contextScope string, windowIndex int, now float64, shownBefore map[string]bool) []map[string]any {
	if len(eligibleRevisionIDs) == 0 {
		return []map[string]any{}
	}
	selected := make([]map[string]any, 0)
	selectedDirections := map[string]bool{}
	var newsFirst map[string]any
	if contextScope == GlobalNewChatScope {
		newsFirst = FirstSlotNewsCard(cards, eligibleRevisionIDs, now)
	}
	if newsFirst != nil {
		selected = append(selected, newsFirst)
	}
	ring := make([]map[string]any, 0)
	for _, card := range cards {
		if contextScope == GlobalNewChatScope && CardIsNews(card) {
			continue
		}
		ring = append(ring, card)
	}
	ringEligible := map[string]bool{}
	for _, card := range ring {
		revisionID := pyStr(card["card_revision_id"])
		if eligibleRevisionIDs[revisionID] {
			ringEligible[revisionID] = true
		}
	}
	targetCount := len(ringEligible) + len(selected)
	if targetCount > FIRST_SCREEN_CARD_TARGET {
		targetCount = FIRST_SCREEN_CARD_TARGET
	}
	secondWindow := contextScope == GlobalNewChatScope && windowIndex != 0
	start := 0
	if secondWindow {
		start = FIRST_SCREEN_CARD_TARGET - len(selected)
	}
	taken := map[int]bool{}
	var passes []string
	if secondWindow && len(shownBefore) > 0 {
		passes = []string{"unshown", "spread", "fill"}
	} else {
		passes = []string{"spread", "fill"}
	}
	for _, mode := range passes {
		for offset := 0; offset < len(ring); offset++ {
			if len(selected) == targetCount {
				break
			}
			index := (start + offset) % len(ring)
			card := ring[index]
			revisionID := pyStr(card["card_revision_id"])
			if taken[index] || !eligibleRevisionIDs[revisionID] {
				continue
			}
			if mode == "unshown" && shownBefore[revisionID] {
				continue
			}
			if contextScope == GlobalNewChatScope {
				interestID := pyStr(card["interest_summary_id"])
				starterID := pyStr(card["source_starter_id"])
				if interestID == "" && starterID == "" && !pythonTruthy(card["cold_start"]) {
					continue
				}
				direction := poolFirstNonEmpty(pyStr(card["direction"]), interestID, starterID, pyStr(card["card_id"]))
				if mode == "spread" && selectedDirections[direction] {
					continue
				}
				selectedDirections[direction] = true
			}
			taken[index] = true
			selected = append(selected, card)
		}
	}
	if secondWindow {
		// 第二窗：常规卡按环序自 start 起排列（lead 保住新闻首槽）
		var lead []map[string]any
		if newsFirst != nil {
			lead = append(lead, selected[0])
		}
		order := make([]int, 0, len(taken))
		for index := range taken {
			order = append(order, index)
		}
		sortRingIndices(order, start, len(ring))
		selected = append(lead, selected[1:]...)
		for _, index := range order {
			selected = append(selected, ring[index])
		}
	}
	return selected
}

// FirstSlotNewsCard 首槽新闻卡：在偏好窗内按 selector_rank 取最优
// （对齐 :5432-5465 _first_slot_news_card）。
func FirstSlotNewsCard(cards []map[string]any, eligibleRevisionIDs map[string]bool, now float64) map[string]any {
	type candidate struct {
		published float64
		card      map[string]any
	}
	candidates := make([]candidate, 0)
	for _, card := range cards {
		if !eligibleRevisionIDs[pyStr(card["card_revision_id"])] || !CardIsNews(card) {
			continue
		}
		bundle, _ := card["evidence_bundle"].(map[string]any)
		if bundle == nil {
			continue
		}
		var published float64
		var ok bool
		if pyStr(bundle["source_kind"]) == HotListSourceKind {
			published, ok = toFloatAny(card["effective_time"])
		} else {
			published, ok = ParseISOTimestamp(pyStr(bundle["published_at"]))
		}
		if !ok || now-published < 0 || now-published > NewsMaxWindowHours*3600 {
			continue
		}
		candidates = append(candidates, candidate{published, card})
	}
	if len(candidates) == 0 {
		return nil
	}
	var pool []candidate
	for _, item := range candidates {
		if now-item.published <= NewsPreferredWindowHours*3600 {
			pool = append(pool, item)
		}
	}
	if len(pool) == 0 {
		pool = candidates
	}
	best := pool[0]
	for _, item := range pool[1:] {
		if cardRank(item.card) < cardRank(best.card) {
			best = item
		}
	}
	return best.card
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// unixToISO UNIX 秒 → UTC ISO（对齐 datetime.fromtimestamp(..., tz=utc).isoformat()）。
func unixToISO(seconds float64) string {
	return time.Unix(int64(seconds), 0).UTC().Format("2006-01-02T15:04:05Z07:00")
}

// unixToDate UNIX 秒 → 本地日期（对齐 time.strftime("%Y-%m-%d", localtime)）。
func unixToDate(seconds float64) string {
	return time.Unix(int64(seconds), 0).Format("2006-01-02")
}

// pyHostname URL 宿主小写（等价 (urlparse(list_url).hostname or "")）。
func pyHostname(urlText string) string {
	parsed, err := url.Parse(urlText)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}


// cloneMap 浅拷贝（对齐 dict(trigger_key)）。
func poolCloneMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// differenceSet 集合差。
func differenceSet(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key := range a {
		if !b[key] {
			out[key] = true
		}
	}
	return out
}

// containsCard 指针级包含判定（等价 Python list 内 dict 引用比较）。
func containsCard(cards []map[string]any, card map[string]any) bool {
	for _, item := range cards {
		if &item == &card || mapIdentityEqual(item, card) {
			return true
		}
	}
	return false
}

// mapIdentityEqual 引用等价回退到内容等价。
func mapIdentityEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if !anyEqual(b[key], value) {
			return false
		}
	}
	return true
}

// firstNonEmpty 首个非空串。
func poolFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// sortCardsReducedLast 降权卡排尾（对齐 sort(key=revision in reduced)）。
func sortCardsReducedLast(cards []map[string]any, reducedRevisionIDs map[string]bool) {
	for i := 1; i < len(cards); i++ {
		for j := i; j > 0; j-- {
			a := reducedRevisionIDs[pyStr(cards[j-1]["card_revision_id"])]
			b := reducedRevisionIDs[pyStr(cards[j]["card_revision_id"])]
			if a && !b {
				cards[j-1], cards[j] = cards[j], cards[j-1]
			} else {
				break
			}
		}
	}
}

// sortRingIndices 环序按 (index-start) % len 排列（对齐 sorted(taken, key=...)）。
func sortRingIndices(indices []int, start, ringLen int) {
	for i := 1; i < len(indices); i++ {
		for j := i; j > 0; j-- {
			a := (indices[j-1] - start + ringLen*4) % ringLen
			b := (indices[j] - start + ringLen*4) % ringLen
			if a > b {
				indices[j-1], indices[j] = indices[j], indices[j-1]
			} else {
				break
			}
		}
	}
}

// cardRank selector_rank 整数（对齐 int(card.get("selector_rank") or 0)）。
func cardRank(card map[string]any) int {
	rank, ok := toFloatAny(card["selector_rank"])
	if !ok {
		return 0
	}
	return int(rank)
}

// toFloatAny 数字转 float。
func toFloatAny(value any) (float64, bool) {
	if v, ok := value.(float64); ok {
		return v, true
	}
	if v, ok := value.(int); ok {
		return float64(v), true
	}
	return 0, false
}
