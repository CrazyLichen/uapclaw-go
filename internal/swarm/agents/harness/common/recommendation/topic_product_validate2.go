package recommendation

// topic_product_validate2.go 一比一对齐上游 topic_product.py:3843-4017 的
// _valid_card、:3266-3273 的 _valid_scope、:4027-4043 的
// _valid_pool_composition、:4046-4119 的 _short_delta_interest_id/
// _ordered_cards_for_diagnosis、:5239-5256 的 _card_opened 与
// :5589-5603 的 _project_sources_still_belong（⤴️31c 归位）。
// 已知偏差：_project_sources_still_belong 的 get_session_metadata 带
// cache_bust/enable_writeback 参数（Python 会话域签名）；Go 用
// swarmsession.GetSessionMetadata（无缓存旁路参数，读侧等价）。

import (
	"sort"
	"strings"

	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// cardFields 卡字段全集（topic_product.py:294-330 _CARD_FIELDS）。
var cardFields = map[string]bool{
	"card_id": true, "card_revision_id": true, "generation_batch_id": true,
	"selector_rank": true, "category": true, "context_scope": true,
	"project_id": true, "interest_summary_id": true, "source_starter_id": true,
	"intent": true, "title": true, "prompt": true, "effective_time": true,
	"expire_time": true, "evidence_bundle": true, "search_provider_id": true,
	"search_receipt": true, "source_memory_unit_ids": true,
	"source_locators": true, "project_source_locators": true,
	"generation_input_snapshot_id": true, "state_maintenance_receipt": true,
	"profile_semantic_version": true, "short_intent_version": true,
	"component_receipts": true, "component_invocation_receipts": true,
	"provider_invocation_receipts": true, "generation_config_version": true,
	"corpus_version": true, "document_snapshot_version": true,
	"retrieval_config_version": true, "evidence_extractor_version": true,
	"list_search_version": true, "policy_id": true, "payload_version": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidScope scope 与项目 ID 匹配判定（topic_product.py:3266-3273）。
func ValidScope(contextScope, projectID any) bool {
	scope, isStr := contextScope.(string)
	if !isStr {
		return false
	}
	expected, err := ProjectIDForScope(scope)
	if err != nil {
		return false
	}
	return pyStr(projectID) == expected && scopeProjectNullEqual(projectID, expected)
}

// scopeProjectNullEqual Python project_id == expected_project_id：None 与
// 空串语义分界——expected 为 "" 时 project_id 须为 None。
func scopeProjectNullEqual(projectID any, expected string) bool {
	if expected == "" {
		return projectID == nil
	}
	return pyStr(projectID) == expected
}

// OrderedCardsForDiagnosis 诊断排序：秩升序、并列按修订 ID
// （topic_pipeline.py:4105-4119 _ordered_cards_for_diagnosis）。
func OrderedCardsForDiagnosis(cards []map[string]any) []map[string]any {
	sorted := append([]map[string]any(nil), cards...)
	sort.SliceStable(sorted, func(i, j int) bool {
		rankI, rankJ := 0, 0
		if value, isInt := plainIntValue(sorted[i]["selector_rank"]); isInt {
			rankI = value
		}
		if value, isInt := plainIntValue(sorted[j]["selector_rank"]); isInt {
			rankJ = value
		}
		if rankI != rankJ {
			return rankI < rankJ
		}
		return pyStrOr(sorted[i]["card_revision_id"], "") <
			pyStrOr(sorted[j]["card_revision_id"], "")
	})
	return sorted
}

// ShortDeltaInterestID 摘要短增量要求的兴趣 ID（topic_pipeline.py:4046-4102
// _short_delta_interest_id）。返回 (id, hasValue)：空串为契约失败，nil 为无要求。
func ShortDeltaInterestID(manifest map[string]any) (any, bool) {
	selection, _, _ := payloadMap(manifest["rid_interest_selection"])
	if selection == nil {
		return nil, true
	}
	rawDeltaIDs, present := selection["short_trigger_delta_event_ids"]
	if !present || rawDeltaIDs == nil {
		return nil, true
	}
	deltaIDs := pyListStrings(rawDeltaIDs)
	if deltaIDs == nil {
		return "", true
	}
	for _, eventID := range deltaIDs {
		if eventID == "" {
			return "", true
		}
	}
	seen := map[string]bool{}
	for _, eventID := range deltaIDs {
		if seen[eventID] {
			return "", true
		}
		seen[eventID] = true
	}
	if len(deltaIDs) == 0 {
		return nil, true
	}
	interestID := pyStr(selection["delta_linked_interest_summary_id"])
	ridResult, _, _ := payloadMap(manifest["rid_result"])
	var interests []any
	if ridResult != nil {
		if items, ok := ridResult["trigger_interests"].([]any); ok {
			interests = items
		}
	}
	if interestID == "" || interests == nil {
		return "", true
	}
	deltaSet := map[string]bool{}
	for _, eventID := range deltaIDs {
		deltaSet[eventID] = true
	}
	for _, raw := range interests {
		interest, isMap := raw.(map[string]any)
		if !isMap || pyStr(interest["interest_summary_id"]) != interestID {
			continue
		}
		sourceEventIDs := pyListStrings(interest["source_event_ids"])
		if sourceEventIDs == nil {
			return "", true
		}
		for _, eventID := range sourceEventIDs {
			if eventID == "" {
				return "", true
			}
		}
		sourceLocators := pyListMaps(interest["source_locators"])
		if sourceLocators == nil {
			return "", true
		}
		locatorEventIDs := map[string]bool{}
		for _, locator := range sourceLocators {
			if pyStr(locator["event_id"]) == "" {
				return "", true
			}
			locatorEventIDs[pyStr(locator["event_id"])] = true
		}
		for _, eventID := range sourceEventIDs {
			if deltaSet[eventID] || locatorEventIDs[eventID] {
				return interestID, true
			}
		}
		return "", true
	}
	return "", true
}

// ValidPoolComposition 池组成校验（topic_pipeline.py:4027-4043）。
func ValidPoolComposition(contextScope string, cards []map[string]any) bool {
	actual := map[string]int{}
	for _, card := range cards {
		actual[pyStrOr(card["category"], "")]++
	}
	expected := ProjectComposition
	if contextScope == GlobalNewChatScope {
		expected = GlobalProjectComposition
	}
	if contextScope != GlobalNewChatScope {
		if actual["project_continuation"] == 0 {
			return false
		}
		for category, count := range actual {
			if count > expected[category] {
				return false
			}
		}
	}
	limit := lenOf(expected)
	if contextScope == GlobalNewChatScope {
		limit = GLOBAL_POOL_TARGET_SIZE
	}
	if len(cards) == 0 || len(cards) > limit {
		return false
	}
	for category := range actual {
		if _, known := expected[category]; !known {
			return false
		}
	}
	return true
}

// CardOpened 用户已从该卡发起会话的卡不再进后续窗口
// （topic_product.py:5239-5256 _card_opened）。固定 starter 在 scope 内任意
// 修订下都算已打开；被继任生成携带的卡同理。
func CardOpened(store *TopicStarterStore, card map[string]any) bool {
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "topic_click",
		TopicID:   stringPtr(pyStrOr(card["card_revision_id"], "")),
		PolicyID:  stringPtr(POLICY_ID),
		Limit:     1,
	})
	if err != nil {
		return false
	}
	if len(rows) > 0 {
		return true
	}
	for _, field := range []string{"card_id", "source_starter_id"} {
		value := card[field]
		if pyStr(value) == "" {
			continue
		}
		rows, err := store.QueryEvents(QueryEventsOptions{
			EventType: "topic_click",
			PolicyID:  stringPtr(POLICY_ID),
			PayloadEquals: map[string]any{
				"context_scope": card["context_scope"], field: value,
			},
			Limit: 1,
		})
		if err != nil {
			continue
		}
		if len(rows) > 0 {
			return true
		}
	}
	return false
}

// ProjectSourcesStillBelong 项目卡跟随其可读源会话的当前项目
// （topic_product.py:5589-5603）。
func ProjectSourcesStillBelong(manifest map[string]any, card map[string]any) bool {
	if pyStr(card["context_scope"]) == GlobalNewChatScope {
		return true
	}
	sessionIDs := map[string]bool{}
	for sessionID := range InterestSessionIds(manifest)[pyStr(card["interest_summary_id"])] {
		sessionIDs[sessionID] = true
	}
	for _, locator := range pyListAny(card["source_locators"]) {
		if locatorMap, ok := locator.([]any); ok && len(locatorMap) > 0 {
			sessionIDs[pyStr(locatorMap[0])] = true
		} else if locatorMap, ok := locator.(map[string]any); ok {
			sessionIDs[pyStr(locatorMap["session_id"])] = true
		}
	}
	for _, locator := range pyListMaps(card["project_source_locators"]) {
		sessionIDs[pyStr(locator["session_id"])] = true
	}
	for sessionID := range sessionIDs {
		metadata := swarmsession.GetSessionMetadata(sessionID)
		if metadata != nil && pyStr(metadata["project_id"]) != pyStr(card["project_id"]) {
			return false
		}
	}
	return true
}

// ValidCard 一张已落卡的结构与契约校验（topic_product.py:3843-4017
// _valid_card）。newsLead 标记新闻领衔卡。
func ValidCard(card any, trigger map[string]any, newsLead bool) bool {
	root, isMap := card.(map[string]any)
	if !isMap || !mapKeysSuperset(root, cardFields) {
		return false
	}
	generationVersion := parseGenerationVersion(pyStrOr(trigger["generation_config_version"], ""))
	if pyStr(root["policy_id"]) != POLICY_ID {
		return false
	}
	if pyStr(root["payload_version"]) != PAYLOAD_VERSION {
		return false
	}
	if !ValidScope(root["context_scope"], root["project_id"]) {
		return false
	}
	if pyStr(root["context_scope"]) != pyStr(trigger["context_scope"]) {
		return false
	}
	if !anyEqual(root["project_id"], trigger["project_id"]) {
		return false
	}
	if pyIntOf(root["profile_semantic_version"]) != pyIntOf(trigger["profile_semantic_version"]) {
		return false
	}
	if pyIntOf(root["short_intent_version"]) != pyIntOf(trigger["short_intent_version"]) {
		return false
	}
	if pyStr(root["generation_config_version"]) != pyStr(trigger["generation_config_version"]) {
		return false
	}
	if pyStr(root["corpus_version"]) != pyStr(trigger["corpus_version"]) {
		return false
	}
	if pyStr(root["document_snapshot_version"]) != pyStr(trigger["document_snapshot_version"]) {
		return false
	}
	if pyStr(root["retrieval_config_version"]) != pyStr(trigger["retrieval_config_version"]) {
		return false
	}
	if pyStr(root["evidence_extractor_version"]) != pyStr(trigger["evidence_extractor_version"]) {
		return false
	}
	if pyStr(root["search_provider_id"]) != pyStr(trigger["search_provider_id"]) {
		return false
	}
	if hasSecretField(root) {
		return false
	}
	if !ValidEvidence(root) {
		return false
	}
	hostEvidence := CardUsesHostEvidence(root)
	if hostEvidence && root["search_receipt"] != nil {
		return false
	}
	if !hostEvidence && !ValidSearchReceipt(root["search_receipt"]) {
		return false
	}
	if corpusHostReceipt(root) != nil && !ValidCorpusHostReceipt(root) {
		return false
	}
	if ProfileInterestHostReceipt(root) != nil && !ValidProfileInterestHostReceipt(root) {
		return false
	}
	if hotListHostReceipt(root) != nil && !ValidHotListHostReceipt(root) {
		return false
	}
	if !ValidCardCopy(root["title"], CardTitleMaxChars) {
		return false
	}
	if !ValidCardCopy(root["prompt"], CardPromptMaxChars) {
		return false
	}
	// v16+ 全局页画像/近期卡必须具名方向或为新闻卡（v17+ 新闻还不得带兴趣）
	if pyStr(root["context_scope"]) == GlobalNewChatScope &&
		(pyStr(root["category"]) == ProfileInterestCategory ||
			pyStr(root["category"]) == "profile_recent_progress") &&
		generationVersion >= 16 &&
		(!CardIsNews(root) || (generationVersion >= 17 && root["interest_summary_id"] != nil)) &&
		!pythonTruthy(root["direction"]) {
		return false
	}
	// v17+ 新闻卡的匹配语义必须与兴趣槽一致
	if CardIsNews(root) && generationVersion >= 17 {
		expectedMatch := "closest"
		if root["interest_summary_id"] != nil {
			expectedMatch = "matched"
		}
		if pyStr(root["news_match"]) != expectedMatch ||
			(expectedMatch == "closest" && root["direction"] != nil) {
			return false
		}
	}
	if !ValidStateMaintenanceReceipt(root["state_maintenance_receipt"],
		root["profile_semantic_version"], trigger["negative_preference_version"]) {
		return false
	}
	// Python float(card[...]) 的 TypeError → false
	effectiveTime, effectiveOK := numericField(root["effective_time"])
	expireTime, expireOK := numericField(root["expire_time"])
	if !effectiveOK || !expireOK {
		return false
	}
	if effectiveTime >= expireTime {
		return false
	}
	for _, field := range []string{"card_id", "card_revision_id",
		"generation_batch_id", "category", "intent", "title", "prompt",
		"generation_input_snapshot_id", "list_search_version"} {
		if pyStr(root[field]) == "" {
			return false
		}
	}
	if pyStr(root["context_scope"]) == GlobalNewChatScope &&
		FixedCorpusCategories[pyStr(root["category"])] &&
		pyStr(root["source_starter_id"]) == "" {
		return false
	}
	receipts, receiptsIsMap := root["component_receipts"].(map[string]any)
	if !receiptsIsMap {
		return false
	}
	category := pyStr(root["category"])
	if !ValidReceipts(receipts, pyStr(root["generation_input_snapshot_id"]),
		pyStr(root["context_scope"]), category,
		FixedCorpusCategories[category], hostEvidence, newsLead) ||
		!ReceiptsMatchTrigger(receipts, trigger) {
		return false
	}
	projectLocators, locatorsIsList := root["project_source_locators"].([]any)
	if !locatorsIsList {
		return false
	}
	if pyStr(root["context_scope"]) == GlobalNewChatScope {
		return root["project_id"] == nil && len(projectLocators) == 0
	}
	if category == "project_continuation" {
		if !ValidProjectHostReceipt(root) {
			return false
		}
		for _, locator := range projectLocators {
			locatorMap, ok := locator.(map[string]any)
			if !ok || pyStr(locatorMap["project_id"]) != pyStr(root["project_id"]) {
				return false
			}
		}
		return true
	}
	return len(projectLocators) == 0
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseGenerationVersion topic-starter-market-generation-v(\d+)(?::.*)? 的版本
// 提取；不匹配返回 0（Python None 语义的不可比较档）。
func parseGenerationVersion(value string) int {
	if value == "" {
		return 0
	}
	prefix := "topic-starter-market-generation-v"
	if !strings.HasPrefix(value, prefix) {
		return 0
	}
	body := strings.TrimPrefix(value, prefix)
	digits := ""
	for _, character := range body {
		if character >= '0' && character <= '9' {
			digits += string(character)
		} else {
			break
		}
	}
	if digits == "" {
		return 0
	}
	result := 0
	for _, character := range digits {
		result = result*10 + int(character-'0')
	}
	return result
}

// plainIntValue 整数取值（bool 排除）。
func plainIntValue(value any) (int, bool) {
	if !plainInt(value) {
		return 0, false
	}
	return pyIntOf(value), true
}

// lenOf 映射值求和。
func lenOf(values map[string]int) int {
	total := 0
	for _, count := range values {
		total += count
	}
	return total
}

// mapKeysSuperset root 包含全部必需字段（Python _CARD_FIELDS.issubset(card)）。
func mapKeysSuperset(root map[string]any, required map[string]bool) bool {
	for field := range required {
		if _, present := root[field]; !present {
			return false
		}
	}
	return true
}
