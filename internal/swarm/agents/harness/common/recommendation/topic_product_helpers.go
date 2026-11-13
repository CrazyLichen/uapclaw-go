package recommendation

// topic_product_helpers.go 一比一对齐上游 topic_product.py / topic_news.py 中
// 被 topic_outcomes 交互记录半区与 topic_collector 消费的辅助符号。
//
// ⤴️ 31c Task 6-9 归位标注（规则 9.1 一文件对一文件）：本文件是过渡聚合文件，
// Python 侧无对应单一文件——各符号落地后按归属搬入 topic_product.go /
// topic_news.go（搬移非重写），搬完删除本文件（规则 8）。
// ProjectSessionMetadataLoader 过渡默认跳过（Python get_session_metadata
// 属 server 会话域），31c/31d 会话域接线后替换。

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ProductPolicyID v6 产物/结果面策略 ID（topic_product.py:76；注意与
	// topic_pool.py:27 的 v5 POLICY_ID 是两个策略面，outcomes 从 product 导入）
	ProductPolicyID = "topic-starter-mvp-v6"
	// ProductPayloadVersion v6 池 payload 版本（topic_product.py:77）
	ProductPayloadVersion = "topic-starter-pool-v2"
	// GenerationConfigVersionV6 v6 生成配置版本（topic_product.py:78）
	GenerationConfigVersionV6 = "topic-starter-market-generation-v19"
	// HotListSourceKind 热榜条目来源（topic_product.py:86）
	HotListSourceKind = "hot_list_entry"
	// FixedCorpusCategoryA/B 固定语料两类目（:101）
	fixedCorpusCategoryA = "adjacent_explanation"
	fixedCorpusCategoryB = "broad_timely_practical"
	// ProfileInterestCategory 画像兴趣续聊类目（:102）
	ProfileInterestCategory = "profile_interest_continuation"
	// ArkWebSearchProviderID Ark Web Search 提供方（topic_search.py:340）
	ArkWebSearchProviderID = "ark-responses-web-search"
	// ArkWebSearchReceiptVersion Ark Web Search 小票版本（topic_search.py:341）
	ArkWebSearchReceiptVersion = "topic-starter-v6-ark-responses-web-search-v1"
	// OpenjiuwenNativeSearchProviderID 原生免费搜索提供方（topic_search.py:24）
	OpenjiuwenNativeSearchProviderID = "openjiuwen-native-free-search"
	// OpenjiuwenNativeSearchReceiptVersion（topic_search.py:25-27）
	OpenjiuwenNativeSearchReceiptVersion = "topic-starter-v6-openjiuwen-native-free-search-v1"
	// LegacyOpenjiuwenNativeSearchProviderID 旧版提供方（:34）
	LegacyOpenjiuwenNativeSearchProviderID = "openjiuwen-native-search"
	// LegacyOpenjiuwenNativeSearchReceiptVersion（:35-37）
	LegacyOpenjiuwenNativeSearchReceiptVersion = "topic-starter-v6-openjiuwen-native-search-v1"
	// NewsFeedProviderID 新闻源提供方（topic_news.py:37）
	NewsFeedProviderID = "bing-news-rss"
	// NewsFeedReceiptVersion 新闻小票版本（topic_news.py:38）
	NewsFeedReceiptVersion = "topic-starter-v6-news-feed-v1"
	// NewsFeedEngine 新闻引擎（topic_news.py:39）
	NewsFeedEngine = "bing-news-rss"
	// HotListPullEventType 热榜拉取事件类型（topic_news.py:239）
	HotListPullEventType = "v6_hot_list_pull"
)

// OpenjiuwenNativeSearchEngines 原生搜索引擎集合（topic_search.py:31-33）。
var OpenjiuwenNativeSearchEngines = map[string]bool{"duckduckgo": true, "duckduckgo-jina": true, "bing": true}

// arkSearchReceiptFields / nativeSearchReceiptFields / searchUsageFields
// 字段集合（topic_product.py:191-245）。
var (
	arkSearchReceiptFields = []string{
		"receipt_version", "provider_id", "provider_response_id", "response_object",
		"response_status", "actual_model", "web_search_call_id", "action",
		"usage", "result_count", "requested_query", "engine",
	}
	nativeSearchReceiptFields = []string{
		"receipt_version", "provider_id", "host_request_id", "requested_query",
		"engine", "result_urls", "result_count",
	}
	searchUsageFields = map[string]bool{
		"input_tokens": true, "output_tokens": true, "total_tokens": true, "tool_usage": true,
	}
)

// productPolicyIDPtr v6 策略指针（QueryEventsOptions 载体）。
var productPolicyIDPtr = ProductPolicyID

// ProjectSessionMetadataLoader 会话元数据加载器（⤴️31c 会话域接线）。
// nil/返回 nil 表示会话不可读（对齐 Python metadata 为空时跳过）。
var ProjectSessionMetadataLoader func(sessionID string) map[string]any

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidSearchReceipt 校验受支持搜索小票 schema（topic_product.py:811-813 → :715-775）。
func ValidSearchReceipt(receipt any) bool {
	root, ok := receipt.(map[string]any)
	if !ok || hasSecretField(root) {
		return false
	}
	version := pyStr(root["receipt_version"])
	switch version {
	case OpenjiuwenNativeSearchReceiptVersion, LegacyOpenjiuwenNativeSearchReceiptVersion,
		NewsFeedReceiptVersion:
		providerID := OpenjiuwenNativeSearchProviderID
		engines := OpenjiuwenNativeSearchEngines
		if version == LegacyOpenjiuwenNativeSearchReceiptVersion {
			providerID = LegacyOpenjiuwenNativeSearchProviderID
			engines = map[string]bool{"duckduckgo": true}
		} else if version == NewsFeedReceiptVersion {
			providerID = NewsFeedProviderID
			engines = map[string]bool{NewsFeedEngine: true}
		}
		if !mapKeysEqual(root, nativeSearchReceiptFields) ||
			pyStr(root["provider_id"]) != providerID ||
			!nonEmptyStr(root["host_request_id"]) ||
			!nonEmptyStr(root["requested_query"]) ||
			!engines[pyStr(root["engine"])] {
			return false
		}
		urls, isList := root["result_urls"].([]any)
		if !isList || len(urls) == 0 {
			return false
		}
		seenURLs := map[string]bool{}
		for _, urlValue := range urls {
			text, isString := urlValue.(string)
			if !isString || text != strings.TrimSpace(text) {
				return false
			}
			parsed, err := url.Parse(text)
			if err != nil {
				return false
			}
			if (parsed.Scheme != "http" && parsed.Scheme != "https") ||
				urlHostname(parsed) == "" || seenURLs[text] {
				return false
			}
			seenURLs[text] = true
		}
		if !plainInt(root["result_count"]) || intOf(root["result_count"]) != len(urls) {
			return false
		}
		return true
	case ArkWebSearchReceiptVersion:
		if !mapKeysEqual(root, arkSearchReceiptFields) {
			return false
		}
		return arkSearchReceiptFieldsValid(root)
	}
	return false
}

// arkSearchReceiptFieldsValid Ark 小票字段契约（topic_product.py:667-712，检查顺序保持）。
func arkSearchReceiptFieldsValid(receipt map[string]any) bool {
	if pyStr(receipt["provider_id"]) != ArkWebSearchProviderID ||
		pyStr(receipt["response_object"]) != "response" ||
		pyStr(receipt["response_status"]) != "completed" ||
		pyStr(receipt["engine"]) != ArkWebSearchProviderID ||
		!nonEmptyStr(receipt["provider_response_id"]) ||
		!nonEmptyStr(receipt["actual_model"]) ||
		!nonEmptyStr(receipt["web_search_call_id"]) {
		return false
	}
	action, ok := receipt["action"].(map[string]any)
	if !ok || !mapKeysEqual(action, []string{"type", "query"}) ||
		pyStr(action["type"]) != "search" || !nonEmptyStr(action["query"]) {
		return false
	}
	if pyStr(receipt["requested_query"]) != pyStr(action["query"]) {
		return false
	}
	usage, ok := receipt["usage"].(map[string]any)
	if !ok {
		return false
	}
	for key := range usage {
		if !searchUsageFields[key] {
			return false
		}
	}
	toolUsage, present := usage["tool_usage"]
	if !present {
		return false
	}
	expected := map[string]any{"web_search": 1}
	if len(mapAnyOf(toolUsage)) != len(expected) {
		return false
	}
	for key, value := range expected {
		if !anyEqual(mapAnyOf(toolUsage)[key], value) {
			return false
		}
	}
	if !plainInt(receipt["result_count"]) || intOf(receipt["result_count"]) <= 0 {
		return false
	}
	return true
}

// urlHostname 提取主机名（对齐 urlparse(url).hostname：小写、去端口）。
func urlHostname(parsed *url.URL) string {
	host := parsed.Hostname()
	return strings.ToLower(host)
}

// ValidHTTPURL 合法 http(s) URL（topic_product.py:1878-1882）。
func ValidHTTPURL(value any) bool {
	text, ok := value.(string)
	if !ok || text != strings.TrimSpace(text) {
		return false
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && urlHostname(parsed) != ""
}

// CardProviderResponseIds 卡片的模型调用响应血缘（topic_product.py:817-837）。
func CardProviderResponseIds(card any) []string {
	root, ok := card.(map[string]any)
	if !ok {
		return nil
	}
	receipts, isList := root["provider_invocation_receipts"].([]any)
	if !isList {
		return nil
	}
	var responseIDs []string
	for _, receiptValue := range receipts {
		receipt, isMap := receiptValue.(map[string]any)
		if !isMap {
			return nil
		}
		responseIDs = append(responseIDs, pyStr(receipt["provider_response_id"]))
	}
	if len(responseIDs) != len(receipts) {
		return nil
	}
	seen := map[string]bool{}
	for _, id := range responseIDs {
		if id == "" || seen[id] {
			return nil
		}
		seen[id] = true
	}
	return responseIDs
}

// CardSearchReceipt 校验并分离搜索小票（topic_product.py:839-848）。
func CardSearchReceipt(card any) map[string]any {
	root, ok := card.(map[string]any)
	if !ok {
		return nil
	}
	if CardUsesHostEvidence(root) {
		return nil
	}
	receipt, isMap := root["search_receipt"].(map[string]any)
	if !isMap || !ValidSearchReceipt(receipt) {
		return nil
	}
	return receipt
}

// CardIsNews 新闻卡：新闻源报道，或未找到报道时的热榜条目
// （topic_product.py:850-855）。
func CardIsNews(card any) bool {
	root, ok := card.(map[string]any)
	if !ok {
		return false
	}
	bundle, isMap := root["evidence_bundle"].(map[string]any)
	if !isMap {
		return false
	}
	kind := pyStr(bundle["source_kind"])
	return kind == NewsSourceKind || kind == HotListSourceKind
}

// CardUsesHostEvidence 项目、固定语料与热榜卡携带宿主证据且无搜索小票
// （topic_product.py:3667-3674）。
func CardUsesHostEvidence(card map[string]any) bool {
	category := pyStr(card["category"])
	if category == "project_continuation" || category == ProfileInterestCategory {
		return true
	}
	return corpusHostReceipt(card) != nil || hotListHostReceipt(card) != nil
}

// corpusHostReceipt 固定语料宿主小票（topic_product.py:3531-3540）。
func corpusHostReceipt(card map[string]any) map[string]any {
	category := pyStr(card["category"])
	if category != fixedCorpusCategoryA && category != fixedCorpusCategoryB {
		return nil
	}
	bundle, isMap := card["evidence_bundle"].(map[string]any)
	if !isMap {
		return nil
	}
	receipt, isMap := bundle["host_receipt"].(map[string]any)
	if isMap && pyStr(receipt["lane"]) == "host_fixed_corpus_entry" {
		return receipt
	}
	return nil
}

// hotListHostReceipt 热榜宿主小票（topic_product.py:3616-3626）。
func hotListHostReceipt(card map[string]any) map[string]any {
	bundle, isMap := card["evidence_bundle"].(map[string]any)
	if !isMap {
		return nil
	}
	receipt, isMap := bundle["host_receipt"].(map[string]any)
	if pyStr(card["category"]) == "profile_recent_progress" && isMap &&
		pyStr(receipt["lane"]) == "host_hot_list_entry" {
		return receipt
	}
	return nil
}

// IsColdStartCard 冷启动卡（topic_product.py:4394-4395）。
func IsColdStartCard(card any) bool {
	root, ok := card.(map[string]any)
	return ok && truthy(root["cold_start"]) && root["cold_start"] != nil
}

// MemoryEvidenceID Memory 记录的会话事件 ID（topic_product.py:5504-5506）。
func MemoryEvidenceID(sessionID, eventID string) string {
	return fmt.Sprintf("%s:%s", sessionID, eventID)
}

// MemorySessionsByUnit 每个 Memory 单元抽取证据的会话集合
// （topic_product.py:5534-5545）。
func MemorySessionsByUnit(state map[string]any) map[string]map[string]bool {
	units := anyListOf(mapAnyOf(state["memory"])["units"])
	result := map[string]map[string]bool{}
	for _, companion := range unitCompanions(units) {
		sessions := map[string]bool{}
		for _, locatorValue := range anyListOf(companion["canonical_source_locators"]) {
			locator, isList := locatorValue.([]any)
			if !isList || len(locator) == 0 {
				continue
			}
			sessions[pyStr(locator[0])] = true
		}
		result[pyStr(companion["unit_id"])] = sessions
	}
	return result
}

// unitCompanions 自我命名的单元 companion 投影（topic_product.py:5510-5519）。
func unitCompanions(units []any) []map[string]any {
	var result []map[string]any
	for _, unitValue := range units {
		unit, isMap := unitValue.(map[string]any)
		if !isMap {
			continue
		}
		companion, isMap := unit["companion"].(map[string]any)
		if isMap && pyStr(companion["unit_id"]) != "" {
			result = append(result, companion)
		}
	}
	return result
}

// weighedInterests RID 结果中被称重兴趣（topic_product.py:5547-5557）。
func weighedInterests(manifest map[string]any) []map[string]any {
	ridResult, isMap := manifest["rid_result"].(map[string]any)
	if !isMap {
		return nil
	}
	var interests []map[string]any
	for _, field := range []string{"trigger_interests", "rejected_interests"} {
		for _, item := range anyListOf(ridResult[field]) {
			if m, isMap := item.(map[string]any); isMap {
				interests = append(interests, m)
			}
		}
	}
	return interests
}

// InterestEvidenceIds 每个兴趣 RID 背后的会话事件
// （topic_product.py:5559-5573）。
func InterestEvidenceIds(manifest map[string]any) map[string][]string {
	result := map[string][]string{}
	for _, interest := range weighedInterests(manifest) {
		id := pyStr(interest["interest_summary_id"])
		var events []string
		for _, locatorValue := range anyListOf(interest["source_locators"]) {
			locator, isMap := locatorValue.(map[string]any)
			if !isMap {
				continue
			}
			events = append(events, MemoryEvidenceID(pyStr(locator["session_id"]), pyStr(locator["event_id"])))
		}
		result[id] = events
	}
	return result
}

// InterestSessionIds 每个兴趣 RID 背后的会话（topic_product.py:5576-5586）。
func InterestSessionIds(manifest map[string]any) map[string]map[string]bool {
	result := map[string]map[string]bool{}
	for _, interest := range weighedInterests(manifest) {
		id := pyStr(interest["interest_summary_id"])
		sessions := map[string]bool{}
		for _, locatorValue := range anyListOf(interest["source_locators"]) {
			locator, isMap := locatorValue.(map[string]any)
			if !isMap {
				continue
			}
			sessions[pyStr(locator["session_id"])] = true
		}
		result[id] = sessions
	}
	return result
}

// HotListEntry 榜单上的一个搜索词及其位置（topic_news.py:260-270）。
type HotListEntry struct {
	EntryID    string `json:"entry_id"`
	ProviderID string `json:"provider_id"`
	Rank       int    `json:"rank"`
	Word       string `json:"word"`
	Summary    any    `json:"summary,omitempty"`
	ListedAt   any    `json:"listed_at,omitempty"`
}

// HotListPull 一次成功热榜拉取（topic_news.py:272-278）。
type HotListPull struct {
	ReceiptID  string          `json:"receipt_id"`
	ProviderID string          `json:"provider_id"`
	ListURL    string          `json:"list_url"`
	PulledAt   float64         `json:"pulled_at"`
	Entries    []*HotListEntry `json:"entries"`
}

// HotEventKey 一个事件无论榜单怎么写词：宽度、大小写、空格与标点搁置
// （topic_news.py:280-283）。
func HotEventKey(word string) string {
	// NFKC 归一 + casefold + 仅保留字母数字
	var builder strings.Builder
	for _, r := range strings.ToLower(norm.NFKC.String(word)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// LoadHotListPull 按 receipt ID 读已记录的成功拉取（topic_news.py:286-296）。
func LoadHotListPull(store *TopicStarterStore, receiptID any) (*HotListPull, error) {
	id, ok := receiptID.(string)
	if !ok || id == "" {
		return nil, nil
	}
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: HotListPullEventType, TopicID: &id, PolicyID: &productPolicyIDPtr, Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	payload, isMap := rows[0]["payload"].(map[string]any)
	if !isMap || pyStr(payload["status"]) != "success" {
		return nil, nil
	}
	return recordedPull(payload)
}

// recordedPull 载荷 → 拉取记录（topic_news.py:487-494）。
func recordedPull(payload map[string]any) (*HotListPull, error) {
	pull := &HotListPull{
		ReceiptID:  pyStr(payload["receipt_id"]),
		ProviderID: pyStr(payload["provider_id"]),
		ListURL:    pyStr(payload["list_url"]),
		PulledAt:   toFloatOr(payload["pulled_at"], 0),
	}
	for _, entryValue := range anyListOf(payload["entries"]) {
		entry, isMap := entryValue.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("hot list pull entry contract failed")
		}
		// 对齐 HotListEntry(**entry)：键必须恰等（多余键 → TypeError → 上抛）
		if !mapKeysEqual(entry, []string{"entry_id", "provider_id", "rank", "word", "summary", "listed_at"}) {
			return nil, fmt.Errorf("hot list pull entry contract failed")
		}
		pull.Entries = append(pull.Entries, &HotListEntry{
			EntryID: pyStr(entry["entry_id"]), ProviderID: pyStr(entry["provider_id"]),
			Rank: intOf(entry["rank"]), Word: pyStr(entry["word"]),
			Summary: entry["summary"], ListedAt: entry["listed_at"],
		})
	}
	return pull, nil
}

// NewsCardEvent 池内新闻卡为之生成的热榜词条事件键
// （topic_news.py:300-314）。
func NewsCardEvent(store *TopicStarterStore, manifest map[string]any, cardID any) (string, error) {
	slot, isMap := manifest["news_slot"].(map[string]any)
	if !isMap || !anyEqual(slot["candidate_id"], cardID) {
		return "", nil
	}
	pull, err := LoadHotListPull(store, slot["hot_list_pull_receipt_id"])
	if err != nil {
		return "", err
	}
	if pull == nil {
		return "", nil
	}
	for _, entry := range pull.Entries {
		if entry.EntryID == pyStr(slot["hot_entry_id"]) {
			return HotEventKey(entry.Word), nil
		}
	}
	return "", nil
}

// FindExactTerminalCard 经精确不可变清单成员关系联接一张 v6 卡
// （topic_product.py:5182-5232）。
func FindExactTerminalCard(store *TopicStarterStore, manifestID, cardRevisionID,
	generationBatchID, contextScope string, projectID any) map[string]any {
	// 对齐 :5192 _valid_scope(context_scope, project_id)
	expectedProjectID, err := ProjectIDForScope(contextScope)
	if err != nil {
		return nil
	}
	var expectedValue any
	if expectedProjectID != "" {
		expectedValue = expectedProjectID
	}
	if !anyEqual(projectID, expectedValue) {
		return nil
	}
	manifestRows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_slate_manifest", SlateID: &manifestID,
		PolicyID: &productPolicyIDPtr, Limit: 2, NewestFirst: true,
	})
	if err != nil || len(manifestRows) != 1 {
		return nil
	}
	manifest, isMap := manifestRows[0]["payload"].(map[string]any)
	if !isMap ||
		!anyEqual(manifest["generation_batch_id"], generationBatchID) ||
		!anyEqual(manifest["context_scope"], contextScope) ||
		!anyEqual(manifest["project_id"], projectID) ||
		!manifestContainsRevision(manifest, cardRevisionID) {
		return nil
	}
	cardRows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_card", SlateID: &manifestID, TopicID: &cardRevisionID,
		PolicyID: &productPolicyIDPtr, Limit: 2, NewestFirst: true,
	})
	if err != nil || len(cardRows) != 1 {
		return nil
	}
	card, isMap := cardRows[0]["payload"].(map[string]any)
	if !isMap ||
		!anyEqual(card["generation_batch_id"], generationBatchID) ||
		!anyEqual(card["card_revision_id"], cardRevisionID) ||
		!anyEqual(card["context_scope"], contextScope) ||
		!anyEqual(card["project_id"], projectID) {
		return nil
	}
	if !projectSourcesStillBelong(manifest, card) {
		return nil
	}
	return card
}

// manifestContainsRevision 清单成员校验。
func manifestContainsRevision(manifest map[string]any, cardRevisionID any) bool {
	for _, revision := range anyListOf(manifest["card_revision_ids"]) {
		if anyEqual(revision, cardRevisionID) {
			return true
		}
	}
	return false
}

// projectSourcesStillBelong 项目卡跟随其可读源会话的当前项目
// （topic_product.py:5589-5603）。
func projectSourcesStillBelong(manifest, card map[string]any) bool {
	if pyStr(card["context_scope"]) == GlobalNewChatScope {
		return true
	}
	sessionIDs := map[string]bool{}
	for sessionID := range InterestSessionIds(manifest)[pyStr(card["interest_summary_id"])] {
		sessionIDs[sessionID] = true
	}
	for _, locator := range anyListOf(card["source_locators"]) {
		pair, isList := locator.([]any)
		if isList && len(pair) > 0 {
			sessionIDs[pyStr(pair[0])] = true
		}
	}
	for _, locatorValue := range anyListOf(card["project_source_locators"]) {
		locator, isMap := locatorValue.(map[string]any)
		if isMap {
			sessionIDs[pyStr(locator["session_id"])] = true
		}
	}
	for sessionID := range sessionIDs {
		if ProjectSessionMetadataLoader == nil {
			// ⤴️31c 接线：过渡期无会话元数据源，跳过（Python metadata 缺失同样跳过）
			continue
		}
		metadata := ProjectSessionMetadataLoader(sessionID)
		if len(metadata) > 0 && !anyEqual(metadata["project_id"], card["project_id"]) {
			return false
		}
	}
	return true
}
