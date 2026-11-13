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
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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

// ──────────────────────────── ⤴️ 31c 契约切片（topic_product.py:76-606/:617-652/:767-808/:952-960/:2427-2434/:3276-3392/:4122-4134） ────────────────────────────

const (
	// NewsOnlySuccessor 替换前任新闻卡且其余卡原样保留的后继标识（topic_product.py:87）
	NewsOnlySuccessor = "news_only"
	// ProjectLocatorReceiptVersion 项目定位小票版本（:82）
	ProjectLocatorReceiptVersion = "topic-starter-v6-project-locator-v1"
	// FixedCorpusReceiptVersion 固定语料小票版本（:83）
	FixedCorpusReceiptVersion = "topic-starter-v6-corpus-entry-v1"
	// ProfileInterestReceiptVersion 画像兴趣小票版本（:84）
	ProfileInterestReceiptVersion = "topic-starter-v6-profile-interest-v1"
	// HotListEntryReceiptVersion 热榜条目小票版本（:85）
	HotListEntryReceiptVersion = "topic-starter-v6-hot-list-entry-v1"
	// ComponentInvocationReceiptVersion 组件调用小票版本（:105-107）
	ComponentInvocationReceiptVersion = "topic-starter-v6-component-invocation-v2"
	// GroundingDecisionReceiptVersion grounding 决策小票版本（:108）
	GroundingDecisionReceiptVersion = "topic-starter-v6-grounding-decision-v2"
	// GroundingEvidenceReceiptVersion grounding 证据小票版本（:109）
	GroundingEvidenceReceiptVersion = "topic-starter-v6-grounding-evidence-v2"
	// SessionSummaryCoverageReceiptVersion 摘要覆盖小票版本（:110-112）
	SessionSummaryCoverageReceiptVersion = "topic-starter-v6-session-summary-coverage-v2"
	// LegacySessionSummaryCoverageReceiptVersion 旧版覆盖小票版本（:113-115）
	LegacySessionSummaryCoverageReceiptVersion = "topic-starter-v6-session-summary-coverage-v1"
	// SessionSummaryMaterializationReceiptVersion 摘要物化小票版本（:116-118）
	SessionSummaryMaterializationReceiptVersion = "topic-starter-v6-session-summary-materialization-v2"
	// SessionSummaryCoverageEventType 摘要覆盖事件类型（:119）
	SessionSummaryCoverageEventType = "session_summary_coverage"
	// InputRevisionReceiptVersion 输入修订小票版本（:104）
	InputRevisionReceiptVersion = "topic-starter-v6-input-revision-v1"
	// ArkReasoningPolicyVersion Ark 推理策略版本（:79）
	ArkReasoningPolicyVersion = "topic-starter-v6-reasoning-policy-v4"
	// CardTitleMaxChars 卡标题上限（:132）
	CardTitleMaxChars = 60
	// CardPromptMaxChars 卡提示词上限（:133）
	CardPromptMaxChars = 180
	// OpenjiuwenNativeSearchFailureReceiptVersion 原生搜索失败小票版本（topic_search.py:28-30）
	OpenjiuwenNativeSearchFailureReceiptVersion = "topic-starter-v6-openjiuwen-native-free-search-failure-v1"
	// LegacyOpenjiuwenNativeSearchFailureReceiptVersion 旧版失败小票版本（topic_search.py:38-40）
	LegacyOpenjiuwenNativeSearchFailureReceiptVersion = "topic-starter-v6-openjiuwen-search-failure-v1"
)

// GlobalPoolTargetSizeV6 全局池目标规模引用（topic_selection.py:36；Go 侧
// GlobalPoolTargetSize 已在 topic_selection.go 定义）。
const GlobalPoolTargetSizeV6 = GLOBAL_POOL_TARGET_SIZE

// NewsSlotAbsentReasons 新闻槽缺席原因五态（topic_product.py:89-97）。
var NewsSlotAbsentReasons = map[string]bool{
	"hot_list_unavailable": true, "hot_news_matcher_failed": true,
	"news_utility_rejected": true, "news_dismissed": true, "optional_lane_timeout": true,
}

// newsSlotFields 新闻槽字段集（:98-100）。
var newsSlotFields = []string{"candidate_id", "card_form", "match", "hot_entry_id", "hot_list_pull_receipt_id", "absent_reason"}

// TerminalComponentIds 终局组件 ID 全集（topic_product.py:179-187）。
var TerminalComponentIds = map[string]bool{
	"icebreaker_rid": true, "pa_bridge_fixed_retriever": true,
	"verbatim_web_evidence": true, "interaction_utility_scorer": true,
	"diversity_scorer": true,
}

// GlobalProjectComposition / ProjectComposition 池组成配额
// （topic_product.py:121-130；GlobalPoolTargetSizeV6=7）。
var (
	GlobalProjectComposition = map[string]int{
		"profile_recent_progress": 3, "adjacent_explanation": 2,
		"broad_timely_practical": 2, ProfileInterestCategory: GlobalPoolTargetSizeV6,
	}
	ProjectComposition = map[string]int{
		"project_continuation": 3, "project_domain_progress": 1,
	}
)

// 卡片文案拒绝正则（:134-178，逐字翻译 Go RE2；Python MULTILINE→Go (?m)）。
var (
	copyMarkupOrListRE = regexp.MustCompile(`(?m)(?:\r|\n|\t|` + "`" + `|\*\*|__|^\s{0,3}#{1,6}\s|` +
		`^\s*(?:[-*+]|\d+[.)、])\s+|\[[^\]]+\]\([^)]*\))`)
	copyLabelRE = regexp.MustCompile(`(?i)^(?:api\s*error|error|错误|异常|标题|title|提示|prompt|` +
		`建议|suggestion|回答|answer|response)\s*[:：]`)
	copyNoOpRE        = regexp.MustCompile(`(?i)^(?:done|no[ -]?op|silence|stay silent|staying silent)\W*$`)
	copyMetaWrappedRE = regexp.MustCompile(`(?s)^(?:\(.*\)|\[.*\])$`)
	copyErrorRE       = regexp.MustCompile(`(?i)^(?:api\s*error|prompt is too long|request timed out|invalid api key|` +
		`image was too large|错误|异常|请求超时|提示词过长|无效.*api.*key)\s*[:：]?`)
	copyMetaRE = regexp.MustCompile(`(?i)^(?:nothing found|no suggestion|nothing to suggest|` +
		`silence is\b|无可推荐|没有建议|未找到|下面是|以下是)`)
	copyEvaluativeRE = regexp.MustCompile(`(?i)^(?:(?:thanks|thank you|looks good|sounds good|that works|that worked|` +
		`that's all|nice|great|perfect|makes sense|awesome|excellent)\b` +
		`(?:[\s,!.?]*thanks?)?[\s,!.?]*|` +
		`(?:谢谢|感谢|很好|不错|完美|明白了|有道理|就这样)[！!。.]*)$`)
	copyAssistantVoiceRE = regexp.MustCompile(`(?i)^(?:let me\b|i'll\b|i've\b|i'm\b|i can\b|i would\b|i think\b|` +
		`i notice\b|here's\b|here is\b|here are\b|that's\b|this is\b|` +
		`this will\b|you can\b|you should\b|you could\b|sure,|of course\b|` +
		`certainly\b|as an ai\b|i am an ai\b|i cannot\b|i can't\b|sorry\b|` +
		`作为\s*(?:一个\s*)?(?:ai|人工智能)|我是\s*(?:一个\s*)?(?:ai|人工智能)|` +
		`我来|我可以|我认为|我注意到|我无法|我不能|抱歉|` +
		`(?:需要|要不要|是否需要)我(?:帮你|帮您|为你|为您))`)
	// sentenceEndRE 的 [.](?=...) 前瞻 RE2 不支持，以 countSentenceEnds 手写等价
)

// OpenjiuwenNativeSearchFailureReasonByKind 失败种类 → 失败原因映射
// （topic_search.py:45-54）。
var OpenjiuwenNativeSearchFailureReasonByKind = map[string]string{
	"all_engines_failed": "provider_all_engines_failed", "challenge": "provider_challenge",
	"http_status": "provider_http_status", "low_relevance": "provider_low_relevance",
	"timeout": "provider_timeout", "proxy": "provider_proxy",
	"transport": "provider_transport", "unknown": "provider_unknown",
}

// nativeSearchFailureReceiptFields 失败小票字段集（topic_product.py:218-227）。
var nativeSearchFailureReceiptFields = []string{
	"receipt_version", "provider_id", "host_request_id", "engine", "failure_kind", "http_status",
}

// CardCopyRejectionReason 非产品卡文案的稳定拒绝码
// （topic_product.py:617-648；分支顺序逐条对应）。
func CardCopyRejectionReason(value any, maxChars int) string {
	text, ok := value.(string)
	if !ok || text == "" {
		return "empty"
	}
	if text != strings.TrimSpace(text) {
		return "untrimmed"
	}
	if copyNoOpRE.MatchString(text) {
		return "no_op"
	}
	if copyMetaWrappedRE.MatchString(text) {
		return "meta_wrapped"
	}
	if copyErrorRE.MatchString(text) {
		return "error_message"
	}
	if copyLabelRE.MatchString(text) {
		return "prefixed_label"
	}
	if copyMetaRE.MatchString(text) {
		return "meta_text"
	}
	if utf8.RuneCountInString(text) > maxChars {
		return "too_long"
	}
	if copyMarkupOrListRE.MatchString(text) {
		return "has_formatting"
	}
	if copyEvaluativeRE.MatchString(text) {
		return "evaluative"
	}
	if copyAssistantVoiceRE.MatchString(text) || copyAssistantVoiceRequested(text) {
		// 前瞻分支 RE2 不支持，手写等价（topic_product.py:166-178 最后一分支）
		return "assistant_voice"
	}
	if countSentenceEnds(text) > 1 {
		return "multiple_sentences"
	}
	return ""
}

// copyAssistantVoiceRequestPrefixRE 前瞻分支前缀。
var copyAssistantVoiceRequestPrefixRE = regexp.MustCompile(`^(?:需要|要不要|是否需要)我(?:提供|给出)`)

// copyAssistantVoiceRequested 手写等价：
// `(?![^？?]*(?:什么|哪些|哪种|多少|复现))[^？?]*(?:步骤|方案|建议|教程)[^？?]*吗?[？?]?$`
// （前缀后：R 去掉结尾可选项后必须含步骤/方案/建议/教程之一，且不含禁词与问号）。
func copyAssistantVoiceRequested(text string) bool {
	loc := copyAssistantVoiceRequestPrefixRE.FindStringIndex(text)
	if loc == nil {
		return false
	}
	runes := []rune(text[loc[1]:])
	// 剥结尾 [？?]?
	if n := len(runes); n > 0 && (runes[n-1] == '？' || runes[n-1] == '?') {
		runes = runes[:n-1]
	}
	// 剥结尾 吗?
	if n := len(runes); n > 0 && runes[n-1] == '吗' {
		runes = runes[:n-1]
	}
	rest := string(runes)
	// 主模式 [^？?]* 要求无问号；前瞻要求 [^？?]* 到禁词之间无问号（R 无问号则恒可触发）
	if strings.ContainsAny(rest, "？？?") {
		return false
	}
	hasKeyword := false
	for _, keyword := range []string{"步骤", "方案", "建议", "教程"} {
		if strings.Contains(rest, keyword) {
			hasKeyword = true
		}
	}
	if !hasKeyword {
		return false
	}
	for _, forbidden := range []string{"什么", "哪些", "哪种", "多少", "复现"} {
		if strings.Contains(rest, forbidden) {
			return false
		}
	}
	return true
}

// countSentenceEnds 句末计数手写等价：`[。！？!?]|[.](?=\s*(?:[A-Z]|$))`
// （前瞻 RE2 不支持；. 仅在其后为可选空白+大写或文本结尾时计入）。
func countSentenceEnds(text string) int {
	runes := []rune(text)
	count := 0
	for i, r := range runes {
		switch r {
		case '。', '！', '？', '!', '?':
			count++
		case '.':
			rest := runes[i+1:]
			j := 0
			for j < len(rest) && unicode.IsSpace(rest[j]) {
				j++
			}
			if j == len(rest) || (rest[j] >= 'A' && rest[j] <= 'Z') {
				count++
			}
		}
	}
	return count
}

// ValidCardCopy 文案合格（:650-652）。
func ValidCardCopy(value any, maxChars int) bool {
	return CardCopyRejectionReason(value, maxChars) == ""
}

// ValidNativeSearchFailureReceipt 原生搜索失败小票校验
// （topic_product.py:767-808；分支顺序逐条对应）。
func ValidNativeSearchFailureReceipt(receipt any, failureReason any) bool {
	root, ok := receipt.(map[string]any)
	if !ok {
		return false
	}
	if hasSecretField(root) {
		return false
	}
	if !mapKeysEqual(root, nativeSearchFailureReceiptFields) {
		return false
	}
	if !nonEmptyStr(root["host_request_id"]) {
		return false
	}
	version := pyStr(root["receipt_version"])
	providerID := pyStr(root["provider_id"])
	engine := pyStr(root["engine"])
	currentReceipt := version == OpenjiuwenNativeSearchFailureReceiptVersion &&
		providerID == OpenjiuwenNativeSearchProviderID && engine == "openjiuwen-free-search"
	legacyReceipt := version == LegacyOpenjiuwenNativeSearchFailureReceiptVersion &&
		providerID == LegacyOpenjiuwenNativeSearchProviderID && engine == "duckduckgo" &&
		pyStr(root["failure_kind"]) != "all_engines_failed" &&
		pyStr(root["failure_kind"]) != "low_relevance"
	if !currentReceipt && !legacyReceipt {
		return false
	}
	failureKind, isString := root["failure_kind"].(string)
	if !isString || OpenjiuwenNativeSearchFailureReasonByKind[failureKind] != pyStr(failureReason) {
		return false
	}
	httpStatus := root["http_status"]
	if failureKind == "http_status" {
		return plainInt(httpStatus) && intOf(httpStatus) >= 100 && intOf(httpStatus) <= 599
	}
	return httpStatus == nil
}

// ValidInputRevisionReceipt 输入修订小票校验（topic_product.py:3276-3285）。
func ValidInputRevisionReceipt(receipt any, contextScope string) bool {
	root, isMap := receipt.(map[string]any)
	if !isMap {
		return false
	}
	// set(receipt) - {digest} == 三键：去掉 digest 后恰好三键且含 digest 键可缺席
	keys := map[string]bool{}
	for key := range root {
		keys[key] = true
	}
	delete(keys, "discussion_coverage_digest")
	if len(keys) != 3 || !keys["receipt_version"] || !keys["context_scope"] || !keys["revision_digest"] {
		return false
	}
	if digest, present := root["discussion_coverage_digest"]; present && !nonEmptyStr(digest) {
		return false
	}
	return pyStr(root["receipt_version"]) == InputRevisionReceiptVersion &&
		pyStr(root["context_scope"]) == contextScope &&
		nonEmptyStr(root["revision_digest"])
}

// BuildV6GenerationTriggerKey 构造 v6 后继池完整非秘密触发键
// （topic_product.py:3287-3373 build_generation_trigger_key；与 topic_pool.py:83
// 的 v5 同名函数是两个模块的同名函数，Go 以 V6 后缀区分）。
func BuildV6GenerationTriggerKey(localDate, contextScope string, projectID any,
	profileSemanticVersion, shortIntentVersion, negativePreferenceVersion int,
	componentContractVersions map[string]string, model, generationConfigVersion,
	corpusVersion, documentSnapshotVersion, retrievalConfigVersion,
	evidenceExtractorVersion, searchProviderID string,
	inputRevisionReceipt map[string]any) (map[string]any, error) {
	// 对齐 :3295-3297 _valid_scope(context_scope, project_id)
	expectedProjectID, scopeErr := ProjectIDForScope(contextScope)
	if scopeErr != nil {
		return nil, fmt.Errorf("context_scope and project_id disagree")
	}
	var expectedValue any
	if expectedProjectID != "" {
		expectedValue = expectedProjectID
	}
	if !anyEqual(projectID, expectedValue) {
		return nil, fmt.Errorf("context_scope and project_id disagree")
	}
	// 对齐 :3298-3300 组件版本全集
	if len(componentContractVersions) != len(TerminalComponentIds) {
		return nil, fmt.Errorf("all terminal component versions are required")
	}
	for componentID := range componentContractVersions {
		if !TerminalComponentIds[componentID] {
			return nil, fmt.Errorf("all terminal component versions are required")
		}
	}
	// 对齐 :3301-3306 输入修订小票
	if !ValidInputRevisionReceipt(inputRevisionReceipt, contextScope) {
		return nil, fmt.Errorf("scoped input revision receipt is required")
	}
	// 对齐 :3307-3320 版本非空
	for _, value := range []string{localDate, model, generationConfigVersion,
		corpusVersion, documentSnapshotVersion, retrievalConfigVersion,
		evidenceExtractorVersion, searchProviderID} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("trigger versions must be non-empty")
		}
	}
	for _, value := range componentContractVersions {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("trigger versions must be non-empty")
		}
	}
	// 对齐 :3321-3330 状态版本非负整数
	for _, value := range []int{profileSemanticVersion, shortIntentVersion, negativePreferenceVersion} {
		if value < 0 {
			return nil, fmt.Errorf("trigger state versions must be non-negative integers")
		}
	}
	// 对齐 :3331-3348 dict(sorted(component_contract_versions.items()))
	sortedVersions := map[string]any{}
	keys := make([]string, 0, len(componentContractVersions))
	for key := range componentContractVersions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sortedVersions[key] = componentContractVersions[key]
	}
	return map[string]any{
		"local_date": localDate, "context_scope": contextScope, "project_id": projectID,
		"profile_semantic_version":    profileSemanticVersion,
		"short_intent_version":        shortIntentVersion,
		"negative_preference_version": negativePreferenceVersion,
		"component_contract_versions": sortedVersions,
		"model":                       model, "generation_config_version": generationConfigVersion,
		"policy_id": ProductPolicyID, "payload_version": ProductPayloadVersion,
		"corpus_version":             corpusVersion,
		"document_snapshot_version":  documentSnapshotVersion,
		"retrieval_config_version":   retrievalConfigVersion,
		"evidence_extractor_version": evidenceExtractorVersion,
		"search_provider_id":         searchProviderID,
		"input_revision_receipt":     copyMapAny(inputRevisionReceipt),
	}, nil
}

// freshnessTriggerFields 新鲜度字段集（topic_product.py:3376-3386）。
var freshnessTriggerFields = map[string]bool{
	"input_revision_receipt": true, "negative_preference_version": true,
	"local_date": true, "document_snapshot_version": true,
	"profile_semantic_version": true, "short_intent_version": true,
}

// BuildV6ServingCompatibilityKey 只保留池必须共享的代码与配置身份
// （topic_product.py:3375-3390）。
func BuildV6ServingCompatibilityKey(generationTriggerKey map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range generationTriggerKey {
		if !freshnessTriggerFields[key] {
			result[key] = value
		}
	}
	return result
}

// SessionSummaryCoverageTopicID 区分完结协助会话而不改自然状态
// （topic_product.py:952-959）。
func SessionSummaryCoverageTopicID(coverage map[string]any) string {
	var summaryIDs []any
	for _, item := range anyListOf(coverage["sessions"]) {
		summaryIDs = append(summaryIDs, mapAnyOf(item)["session_summary_id"])
	}
	raw, _ := json.Marshal(summaryIDs)
	return fmt.Sprintf("%s|%s|%s",
		pyStr(coverage["input_snapshot_id"]),
		pyStr(coverage["summary_config_version"]),
		string(raw))
}

// newsSlot 清单新闻槽（topic_product.py:4122-4124）。
func newsSlot(manifest any) map[string]any {
	root, isMap := manifest.(map[string]any)
	if !isMap {
		return nil
	}
	slot, isMap := root["news_slot"].(map[string]any)
	if !isMap {
		return nil
	}
	return slot
}

// NewsLeadCardId 全局池首先展示的新闻卡（topic_product.py:4127-4133）。
func NewsLeadCardId(manifest any) string {
	slot := newsSlot(manifest)
	if slot == nil || slot["absent_reason"] != nil {
		return ""
	}
	return pyStr(slot["candidate_id"])
}

// NewsOnlyPool 全局池仅其新闻卡（topic_product.py:2427-2434）。
func NewsOnlyPool(manifest map[string]any, cards []map[string]any) bool {
	leadID := NewsLeadCardId(manifest)
	if leadID == "" {
		return false
	}
	if len(cards) != 1 {
		return false
	}
	return pyStr(cards[0]["card_id"]) == leadID
}

// copyMapAny 一层 map 拷贝。
func copyMapAny(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
