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
	"math"
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

// ──────────────────────────── ⤴️ 31c 契约切片 B：摘要/grounding 辅助校验（topic_product.py:921-950/:961-1109/:1885-2022/:2412-2425/:3392-3458/:1578-1620） ────────────────────────────

// receiptFields / bindingFields / publicationDateProvenanceFields
// 字段集（topic_product.py:243-256/:254-262/:434-443）。
var (
	receiptFields = []string{"receipt_version", "binding", "input_snapshot_id",
		"request_id", "score_semantics", "output_status", "fallback_reason"}
	bindingFields = []string{"component_id", "contract_version", "provider_id",
		"provider_version", "config_version"}
	publicationDateProvenanceFields = []string{"source_type", "source_field",
		"raw_value", "search_provider_id", "search_receipt_id", "search_result_url"}
)

// UniqueNonEmptyStrings 全部非空且唯一（topic_product.py:2412-2420）。
func UniqueNonEmptyStrings(value any) bool {
	items, isList := value.([]any)
	if !isList {
		return false
	}
	seen := map[string]bool{}
	for _, item := range items {
		if !nonEmptyStr(item) {
			return false
		}
		text := pyStr(item)
		if seen[text] {
			return false
		}
		seen[text] = true
	}
	return true
}

// ValidNullableFiniteNumber 可空有限数（topic_product.py:2421-2426）。
func ValidNullableFiniteNumber(value any) bool {
	if value == nil {
		return true
	}
	return finiteTimestamp(value)
}

// ValidReceipt 组件小票校验（topic_product.py:3392-3417 _valid_receipt）。
func ValidReceipt(receipt any, componentID, inputSnapshotID string) bool {
	root, isMap := receipt.(map[string]any)
	if !isMap || !mapKeysEqual(root, receiptFields) {
		return false
	}
	binding, bindingIsMap := root["binding"].(map[string]any)
	if !bindingIsMap || !mapKeysEqual(binding, bindingFields) ||
		pyStr(binding["component_id"]) != componentID {
		return false
	}
	for _, field := range []string{"contract_version", "provider_id", "provider_version", "config_version"} {
		if !nonEmptyStr(binding[field]) {
			return false
		}
	}
	return pyStr(root["input_snapshot_id"]) == inputSnapshotID &&
		nonEmptyStr(root["receipt_version"]) && nonEmptyStr(root["request_id"]) &&
		pyStr(root["output_status"]) == "success" &&
		pyStr(root["score_semantics"]) == "uncalibrated_api_proxy" &&
		root["fallback_reason"] == nil
}

// ValidReceipts 组件小票表校验（topic_product.py:3419-3457 _valid_receipts；
// global 分支按 corpus_lineage/host_evidence/news_lead 裁剪期望集）。
func ValidReceipts(receipts any, inputSnapshotID, contextScope, category string,
	corpusLineage, hostEvidence, newsLead bool) bool {
	root, isMap := receipts.(map[string]any)
	if !isMap {
		return false
	}
	expected := map[string]bool{}
	for id := range TerminalComponentIds {
		expected[id] = true
	}
	if contextScope == GlobalNewChatScope {
		if !corpusLineage {
			delete(expected, "pa_bridge_fixed_retriever")
		}
		if hostEvidence {
			delete(expected, "verbatim_web_evidence")
		}
		if newsLead {
			// 新闻卡领衔池但不在多样性判定中占位
			delete(expected, "diversity_scorer")
		}
	} else {
		expected = map[string]bool{}
		base := TerminalComponentIds
		if category == "project_continuation" {
			for id := range base {
				if id != "pa_bridge_fixed_retriever" {
					expected[id] = true
				}
			}
		} else {
			for id := range base {
				if id != "pa_bridge_fixed_retriever" && id != "verbatim_web_evidence" {
					expected[id] = true
				}
			}
		}
	}
	if len(root) != len(expected) {
		return false
	}
	for componentID := range root {
		if !expected[componentID] {
			return false
		}
		if !ValidReceipt(root[componentID], componentID, inputSnapshotID) {
			return false
		}
	}
	return true
}

// ValidSessionSummaryArtifacts 摘要工件表校验
// （topic_product.py:921-950 _valid_session_summary_artifacts）。
func ValidSessionSummaryArtifacts(manifest map[string]any) bool {
	raw, isList := manifest["session_summary_artifacts"].([]any)
	if !isList {
		return false
	}
	var summaries []*SessionSummaryArtifact
	for _, item := range raw {
		summary, err := SessionSummaryArtifactFromPayload(item)
		if err != nil {
			return false
		}
		summaries = append(summaries, summary)
	}
	summaryIDs := map[string]bool{}
	sessionIDs := map[string]bool{}
	for _, summary := range summaries {
		if summaryIDs[summary.SessionSummaryID] || sessionIDs[summary.SessionID] {
			return false
		}
		summaryIDs[summary.SessionSummaryID] = true
		sessionIDs[summary.SessionID] = true
	}
	contextScope := pyStr(manifest["context_scope"])
	for _, summary := range summaries {
		prefix := fmt.Sprintf("summary:%s:", summary.SessionID)
		if summary.ContextScope != contextScope ||
			!strings.HasPrefix(summary.SessionSummaryID, prefix) {
			return false
		}
		digest := strings.TrimPrefix(summary.SessionSummaryID, prefix)
		if utf8.RuneCountInString(digest) != 64 {
			return false
		}
		for _, r := range digest {
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// ValidSessionSummaryCoverageReceipt 摘要覆盖小票校验
// （topic_product.py:961-1030；allow_legacy 对齐上游参数）。
func ValidSessionSummaryCoverageReceipt(manifest map[string]any, allowLegacySummaryCoverage bool) bool {
	receipt, isMap := manifest["session_summary_coverage_receipt"].(map[string]any)
	if !isMap {
		return false
	}
	rawArtifacts, artifactsIsList := manifest["session_summary_artifacts"].([]any)
	if pyStr(receipt["input_snapshot_id"]) != pyStr(manifest["generation_input_snapshot_id"]) {
		return false
	}
	if pyStr(receipt["context_scope"]) != pyStr(manifest["context_scope"]) {
		return false
	}
	if anyListOf(receipt["sessions"]) == nil {
		return false
	}
	if !artifactsIsList {
		return false
	}
	schemaVersion := pyStr(receipt["schema_version"])
	switch schemaVersion {
	case LegacySessionSummaryCoverageReceiptVersion:
		if !allowLegacySummaryCoverage {
			return false
		}
		if !mapKeysEqual(receipt, []string{"schema_version", "input_snapshot_id", "context_scope", "sessions"}) {
			return false
		}
		if manifest["session_summary_materialization_receipt"] != nil {
			return false
		}
	case SessionSummaryCoverageReceiptVersion:
		if !mapKeysEqual(receipt, []string{"schema_version", "input_snapshot_id", "context_scope", "summary_config_version", "sessions"}) ||
			!nonEmptyStr(receipt["summary_config_version"]) {
			return false
		}
	default:
		return false
	}
	var summaries []*SessionSummaryArtifact
	for _, item := range rawArtifacts {
		summary, err := SessionSummaryArtifactFromPayload(item)
		if err != nil {
			return false
		}
		summaries = append(summaries, summary)
	}
	for _, summary := range summaries {
		if schemaVersion == SessionSummaryCoverageReceiptVersion &&
			summary.SummaryConfigVersion != pyStr(receipt["summary_config_version"]) {
			return false
		}
	}
	expected := make([]any, 0, len(summaries))
	for _, summary := range summaries {
		var exclusionReason any
		if !summary.NaturalRevisitEligible() {
			exclusionReason = "non_natural_or_assisted_input"
		}
		expected = append(expected, map[string]any{
			"session_id":           summary.SessionID,
			"session_summary_id":   summary.SessionSummaryID,
			"input_origin":         summary.InputOrigin,
			"assistance_origin":    summary.AssistanceOrigin,
			"rid_eligible":         summary.NaturalRevisitEligible(),
			"rid_exclusion_reason": exclusionReason,
		})
	}
	return anyEqual(receipt["sessions"], expected)
}

// ValidSessionSummaryMaterializationReceipt 摘要物化小票校验
// （topic_product.py:1032-1109）。
func ValidSessionSummaryMaterializationReceipt(manifest map[string]any) bool {
	receiptValue := manifest["session_summary_materialization_receipt"]
	if receiptValue == nil {
		return true
	}
	receipt, isMap := receiptValue.(map[string]any)
	if !isMap {
		return false
	}
	rawArtifacts, artifactsIsList := manifest["session_summary_artifacts"].([]any)
	if !mapKeysEqual(receipt, []string{"schema_version", "input_snapshot_id",
		"context_scope", "summary_config_version",
		"generated_session_summary_ids", "reused_session_summary_ids"}) {
		return false
	}
	if pyStr(receipt["schema_version"]) != SessionSummaryMaterializationReceiptVersion {
		return false
	}
	if pyStr(receipt["input_snapshot_id"]) != pyStr(manifest["generation_input_snapshot_id"]) {
		return false
	}
	if pyStr(receipt["context_scope"]) != pyStr(manifest["context_scope"]) {
		return false
	}
	if !nonEmptyStr(receipt["summary_config_version"]) {
		return false
	}
	coverage := mapAnyOf(manifest["session_summary_coverage_receipt"])
	if pyStr(receipt["summary_config_version"]) != pyStr(coverage["summary_config_version"]) {
		return false
	}
	if !artifactsIsList {
		return false
	}
	var artifactIDs []string
	for _, item := range rawArtifacts {
		summary, err := SessionSummaryArtifactFromPayload(item)
		if err != nil {
			return false
		}
		if summary.SummaryConfigVersion != pyStr(receipt["summary_config_version"]) {
			return false
		}
		artifactIDs = append(artifactIDs, summary.SessionSummaryID)
	}
	generated, genIsList := receipt["generated_session_summary_ids"].([]any)
	reused, reuseIsList := receipt["reused_session_summary_ids"].([]any)
	if !genIsList || !reuseIsList {
		return false
	}
	genSet := map[string]bool{}
	for _, id := range generated {
		if !nonEmptyStr(id) || genSet[pyStr(id)] {
			return false
		}
		genSet[pyStr(id)] = true
	}
	reuseSet := map[string]bool{}
	for _, id := range reused {
		if !nonEmptyStr(id) || reuseSet[pyStr(id)] {
			return false
		}
		reuseSet[pyStr(id)] = true
	}
	for id := range genSet {
		if reuseSet[id] {
			return false
		}
	}
	artifactIDSet := map[string]bool{}
	for _, id := range artifactIDs {
		artifactIDSet[id] = true
	}
	if len(genSet)+len(reuseSet) != len(artifactIDs) {
		return false
	}
	for id := range genSet {
		if !artifactIDSet[id] {
			return false
		}
	}
	for id := range reuseSet {
		if !artifactIDSet[id] {
			return false
		}
	}
	// 顺序必须与工件表一致
	var generatedInOrder []string
	var reusedInOrder []string
	for _, id := range artifactIDs {
		if genSet[id] {
			generatedInOrder = append(generatedInOrder, id)
		}
		if reuseSet[id] {
			reusedInOrder = append(reusedInOrder, id)
		}
	}
	if !anyEqual(receipt["generated_session_summary_ids"], stringsToAny(generatedInOrder)) {
		return false
	}
	return anyEqual(receipt["reused_session_summary_ids"], stringsToAny(reusedInOrder))
}

// ValidGroundingPublicationDateProvenance 发布日期溯源校验
// （topic_product.py:1885-1939；linked_document_metadata 分支经
// topic_retrieval.valid_linked_date_page，Go 已有 ValidLinkedDatePage）。
func ValidGroundingPublicationDateProvenance(value any, sourceURL string, publishedAt any) bool {
	if value == nil {
		return true
	}
	root, isMap := value.(map[string]any)
	if isMap && pyStr(root["source_type"]) == "linked_document_metadata" {
		if publishedAt == nil {
			return false
		}
		expected := append(append([]string{}, publicationDateProvenanceFields...), "linked_page")
		if !mapKeysEqual(root, expected) {
			return false
		}
		for _, key := range []string{"source_field", "raw_value"} {
			if !nonEmptyStr(root[key]) {
				return false
			}
		}
		return ValidLinkedDatePage(root["linked_page"], sourceURL)
	}
	if publishedAt == nil {
		return false
	}
	if !isMap {
		return false
	}
	// set(value) - {listed_at, page_published_at, api_url} == 基础六字段
	extra := map[string]bool{"listed_at": true, "page_published_at": true, "api_url": true}
	count := 0
	for key := range root {
		if !extra[key] {
			count++
		}
	}
	if count != len(publicationDateProvenanceFields) {
		return false
	}
	for _, field := range publicationDateProvenanceFields {
		if _, present := root[field]; !present {
			return false
		}
	}
	if _, present := root["listed_at"]; present &&
		(pyStr(root["source_type"]) != "page_explicit_metadata" || !nonEmptyStr(root["listed_at"])) {
		return false
	}
	if _, present := root["api_url"]; present &&
		(pyStr(root["source_type"]) != "page_explicit_metadata" || !ValidHTTPURL(root["api_url"])) {
		return false
	}
	if _, present := root["page_published_at"]; present &&
		(pyStr(root["source_type"]) != "structured_search_result" || !nonEmptyStr(root["page_published_at"])) {
		return false
	}
	for _, field := range []string{"source_type", "source_field", "raw_value"} {
		if !nonEmptyStr(root[field]) {
			return false
		}
	}
	searchProviderID := root["search_provider_id"]
	searchReceiptID := root["search_receipt_id"]
	searchResultURL := root["search_result_url"]
	if pyStr(root["source_type"]) == "page_explicit_metadata" {
		return searchProviderID == nil && searchReceiptID == nil && searchResultURL == nil
	}
	if pyStr(root["source_type"]) != "structured_search_result" {
		return false
	}
	return nonEmptyStr(searchProviderID) && nonEmptyStr(searchReceiptID) &&
		nonEmptyStr(searchResultURL) && pyStr(searchResultURL) == sourceURL &&
		ValidHTTPURL(searchResultURL)
}

// ValidGroundingComponentReceipt grounding 组件小票校验
// （topic_product.py:1941-1979；成功走 ValidReceipt，失败走显式契约）。
func ValidGroundingComponentReceipt(receipt any, inputSnapshotID, requestID,
	auditStatus string, failureReason any) bool {
	if auditStatus == "success" {
		root, isMap := receipt.(map[string]any)
		return isMap && ValidReceipt(receipt, "verbatim_web_evidence", inputSnapshotID) &&
			pyStr(root["request_id"]) == requestID
	}
	root, isMap := receipt.(map[string]any)
	if !isMap {
		return false
	}
	if !mapKeysEqual(root, receiptFields) {
		return false
	}
	if pyStr(root["input_snapshot_id"]) != inputSnapshotID {
		return false
	}
	if pyStr(root["request_id"]) != requestID {
		return false
	}
	if !nonEmptyStr(root["receipt_version"]) {
		return false
	}
	if pyStr(root["score_semantics"]) != "not_scored" {
		return false
	}
	if pyStr(root["output_status"]) != "failed" {
		return false
	}
	if !anyEqual(root["fallback_reason"], failureReason) {
		return false
	}
	binding, bindingIsMap := root["binding"].(map[string]any)
	return bindingIsMap && mapKeysEqual(binding, bindingFields) &&
		pyStr(binding["component_id"]) == "verbatim_web_evidence" &&
		nonEmptyStr(binding["provider_id"]) && nonEmptyStr(binding["provider_version"])
}

// GroundingSearchResultUrls 小票 → 结果 URL 表
// （topic_product.py:1981-2021 _grounding_search_result_urls）。
func GroundingSearchResultUrls(receipt any, requestedQuery, providerID string) []string {
	root, isMap := receipt.(map[string]any)
	if !isMap || hasSecretField(root) {
		return nil
	}
	version := pyStr(root["receipt_version"])
	nativeVersions := map[string]bool{
		OpenjiuwenNativeSearchReceiptVersion:       true,
		LegacyOpenjiuwenNativeSearchReceiptVersion: true,
		NewsFeedReceiptVersion:                     true,
	}
	if nativeVersions[version] {
		if !ValidSearchReceipt(receipt) {
			return nil
		}
		if pyStr(root["requested_query"]) != requestedQuery ||
			pyStr(root["provider_id"]) != providerID {
			return nil
		}
		var urls []string
		for _, urlValue := range anyListOf(root["result_urls"]) {
			urls = append(urls, pyStr(urlValue))
		}
		return urls
	}
	if version != ArkWebSearchReceiptVersion {
		return nil
	}
	// Ark 分支多一个 result_urls 字段
	arkFields := append(append([]string{}, arkSearchReceiptFields...), "result_urls")
	if !mapKeysEqual(root, arkFields) {
		return nil
	}
	resultURLs, isList := root["result_urls"].([]any)
	if !isList {
		return nil
	}
	baseReceipt := map[string]any{}
	for key, value := range root {
		baseReceipt[key] = value
	}
	delete(baseReceipt, "result_urls")
	if !ValidSearchReceipt(baseReceipt) {
		return nil
	}
	if pyStr(root["requested_query"]) != requestedQuery ||
		pyStr(root["provider_id"]) != providerID {
		return nil
	}
	seen := map[string]bool{}
	var urls []string
	for _, urlValue := range resultURLs {
		text := pyStr(urlValue)
		if seen[text] || !ValidHTTPURL(text) {
			return nil
		}
		seen[text] = true
		urls = append(urls, text)
	}
	if intOf(root["result_count"]) < len(urls) {
		return nil
	}
	return urls
}

// ExternalOperationCoversCandidate 共享抓取保留物理所有者小票与一致页面元数据
// （topic_product.py:1578-1604）。
func ExternalOperationCoversCandidate(manifest, audit map[string]any,
	key [3]string, candidateID string) bool {
	ownerID := pyStr(audit["candidate_id"])
	if ownerID == candidateID {
		return true
	}
	if key[0] != "fetch" {
		return false
	}
	receipt, isMap := manifest["grounding_evidence_receipt"].(map[string]any)
	if !isMap {
		return false
	}
	candidates, isList := receipt["candidates"].([]any)
	if !isList {
		return false
	}
	fetches := map[string][3]string{}
	for _, candidateValue := range candidates {
		candidate, isMap := candidateValue.(map[string]any)
		if !isMap {
			return false
		}
		id := pyStr(candidate["candidate_id"])
		if id != ownerID && id != candidateID {
			continue
		}
		fetchList, isList := candidate["fetches"].([]any)
		if !isList {
			return false
		}
		for _, fetchValue := range fetchList {
			fetch, isMap := fetchValue.(map[string]any)
			if !isMap {
				continue
			}
			if key[0] == "fetch" && pyStr(fetch["operation_id"]) == key[1] &&
				pyStr(fetch["request_id"]) == key[2] {
				fetches[id] = [3]string{pyStr(fetch["requested_url"]),
					pyStr(fetch["final_url"]), pyStr(fetch["http_status_code"])}
			}
		}
	}
	_, ownerOK := fetches[ownerID]
	_, candidateOK := fetches[candidateID]
	return ownerOK && candidateOK && fetches[ownerID] == fetches[candidateID]
}

// TerminalStageMatchesOperations 记录的操作是否终止于决策所述阶段
// （topic_product.py:1606-1619）。
func TerminalStageMatchesOperations(terminalStage string, operationNames []string) bool {
	switch terminalStage {
	case "search":
		return len(operationNames) == 1 && operationNames[0] == "search"
	case "fetch":
		return len(operationNames) > 0 && operationNames[len(operationNames)-1] == "fetch"
	case "extraction", "offset_validation":
		return len(operationNames) > 0 && operationNames[len(operationNames)-1] == "extractor"
	}
	return true
}

// ──────────────────────────── ⤴️ 31c 契约切片 C：RID 系校验器（topic_product.py:1111-1245/:1247-1527/:3053-3067） ────────────────────────────

// ridResultSchemaVersion RID 结果版本（topic_terminal.py:22）。
const ridResultSchemaVersion = RID_RESULT_SCHEMA_VERSION

// ridDecisionFields RID 决策字段集（topic_product.py:537-551）。
var ridDecisionFields = []string{"interest_summary_id", "source_kind",
	"source_session_summary_ids", "source_profile_interest_ids",
	"source_profile_unit_ids", "source_event_ids", "source_locators",
	"selected", "rank", "resonance_score_nullable", "score_semantics"}

// RetainedCandidateIds 本池从前任携带的候选（topic_product.py:3053-3067）。
func RetainedCandidateIds(manifest any) map[string]bool {
	root, isMap := manifest.(map[string]any)
	if !isMap {
		return map[string]bool{}
	}
	retained, isMap := root["retained_candidates"].(map[string]any)
	if !isMap {
		return map[string]bool{}
	}
	result := map[string]bool{}
	for candidateID := range retained {
		result[candidateID] = true
	}
	return result
}

// ExternalInvocationAuditIndex 审计小票 → (operation, operation_id, request_id) 索引
// （topic_product.py:1111-1245 _external_invocation_audit_index；nil 表示契约失败）。
func ExternalInvocationAuditIndex(manifest map[string]any) map[[3]string]map[string]any {
	audit, isMap := manifest["external_invocation_audit"].(map[string]any)
	if !isMap {
		return nil
	}
	if !mapKeysEqual(audit, []string{"schema_version", "input_snapshot_id",
		"context_scope", "invocations", "counters"}) {
		return nil
	}
	if pyStr(audit["schema_version"]) != EXTERNAL_INVOCATION_AUDIT_SCHEMA_VERSION {
		return nil
	}
	if pyStr(audit["input_snapshot_id"]) != pyStr(manifest["generation_input_snapshot_id"]) {
		return nil
	}
	if pyStr(audit["context_scope"]) != pyStr(manifest["context_scope"]) {
		return nil
	}
	invocations, isList := audit["invocations"].([]any)
	if !isList {
		return nil
	}
	invocationFields := []string{"sequence", "input_snapshot_id", "context_scope",
		"operation", "operation_id", "provider_id", "candidate_id", "status",
		"request_id", "result_id", "latency_ms", "failure_reason"}
	index := map[[3]string]map[string]any{}
	usedOperationIDs := map[string]bool{}
	for sequence, invocationValue := range invocations {
		invocation, invocationIsMap := invocationValue.(map[string]any)
		if !invocationIsMap || !mapKeysEqual(invocation, invocationFields) {
			return nil
		}
		if intOf(invocation["sequence"]) != sequence+1 {
			return nil
		}
		if pyStr(invocation["input_snapshot_id"]) != pyStr(manifest["generation_input_snapshot_id"]) {
			return nil
		}
		if pyStr(invocation["context_scope"]) != pyStr(manifest["context_scope"]) {
			return nil
		}
		operation := pyStr(invocation["operation"])
		if operation != "search" && operation != "fetch" && operation != "extractor" {
			return nil
		}
		for _, field := range []string{"operation_id", "provider_id", "candidate_id", "request_id"} {
			if !nonEmptyStr(invocation[field]) {
				return nil
			}
		}
		status := pyStr(invocation["status"])
		if status != "success" && status != "failure" {
			return nil
		}
		switch latency := invocation["latency_ms"].(type) {
		case int:
			if float64(latency) < 0 {
				return nil
			}
		case float64:
			if math.IsInf(latency, 0) || math.IsNaN(latency) || latency < 0 {
				return nil
			}
		default:
			return nil
		}
		if status == "success" {
			if !nonEmptyStr(invocation["result_id"]) || invocation["failure_reason"] != nil {
				return nil
			}
		} else if invocation["result_id"] != nil || !nonEmptyStr(invocation["failure_reason"]) {
			return nil
		}
		key := [3]string{operation, pyStr(invocation["operation_id"]), pyStr(invocation["request_id"])}
		if _, exists := index[key]; exists || usedOperationIDs[key[1]] {
			return nil
		}
		usedOperationIDs[key[1]] = true
		index[key] = invocation
	}
	counters, countersIsMap := audit["counters"].(map[string]any)
	if !countersIsMap || !mapKeysEqual(counters, []string{"total", "success", "failure", "by_operation"}) {
		return nil
	}
	byOperation, byIsMap := counters["by_operation"].(map[string]any)
	if !byIsMap || !mapKeysEqual(byOperation, []string{"search", "fetch", "extractor"}) {
		return nil
	}
	successCount := 0
	failureCount := 0
	perOperation := map[string][3]int{}
	for key, invocation := range index {
		if mapAnyOf(invocation)["status"] == "success" {
			successCount++
		} else {
			failureCount++
		}
		stats := perOperation[key[0]]
		if mapAnyOf(invocation)["status"] == "success" {
			stats[1]++
		} else {
			stats[2]++
		}
		stats[0]++
		perOperation[key[0]] = stats
	}
	expectedByOperation := map[string]any{}
	for _, operation := range []string{"search", "fetch", "extractor"} {
		stats := perOperation[operation]
		expectedByOperation[operation] = map[string]any{
			"total": stats[0], "success": stats[1], "failure": stats[2],
		}
	}
	expectedCounters := map[string]any{
		"total": len(index), "success": successCount, "failure": failureCount,
		"by_operation": expectedByOperation,
	}
	if !anyEqual(counters, expectedCounters) {
		return nil
	}
	return index
}

// ridResultRejectionReason RID 结果校验
// （topic_product.py:1247-1390 _rid_result_rejection_reason；分支顺序逐条对应）。
func ridResultRejectionReason(manifest map[string]any, newsOnly bool) string {
	rawArtifacts, _ := manifest["session_summary_artifacts"].([]any)
	ridResult, ridIsMap := manifest["rid_result"].(map[string]any)
	if !ridIsMap {
		return "manifest_field_rid_result_contract_failed"
	}
	if rawArtifacts == nil {
		if _, isList := manifest["session_summary_artifacts"].([]any); !isList {
			return "manifest_field_rid_result_contract_failed"
		}
	}
	if !mapKeysEqual(ridResult, []string{"rid_result_version", "context_scope",
		"activity_group", "threshold_version", "component_receipt",
		"trigger_interests", "rejected_interests"}) {
		return "manifest_field_rid_result_contract_failed"
	}
	if pyStr(ridResult["rid_result_version"]) != ridResultSchemaVersion {
		return "manifest_field_rid_result_contract_failed"
	}
	if pyStr(ridResult["context_scope"]) != pyStr(manifest["context_scope"]) {
		return "manifest_field_rid_result_contract_failed"
	}
	if pyStr(ridResult["activity_group"]) != "active" && pyStr(ridResult["activity_group"]) != "steady" {
		return "manifest_field_rid_result_contract_failed"
	}
	if !nonEmptyStr(ridResult["threshold_version"]) {
		return "manifest_field_rid_result_contract_failed"
	}
	componentReceipts := mapAnyOf(manifest["component_receipts"])
	if !anyEqual(ridResult["component_receipt"], componentReceipts["icebreaker_rid"]) {
		return "manifest_field_rid_result_contract_failed"
	}
	triggerInterests, triggerIsList := ridResult["trigger_interests"].([]any)
	if !triggerIsList {
		return "manifest_field_rid_result_contract_failed"
	}
	if len(triggerInterests) == 0 && !newsOnly {
		return "manifest_field_rid_result_contract_failed"
	}
	rejectedInterests, rejectedIsList := ridResult["rejected_interests"].([]any)
	if !rejectedIsList {
		return "manifest_field_rid_result_contract_failed"
	}
	var artifacts []*SessionSummaryArtifact
	for _, item := range rawArtifacts {
		summary, err := SessionSummaryArtifactFromPayload(item)
		if err != nil {
			return "manifest_field_rid_result_contract_failed"
		}
		artifacts = append(artifacts, summary)
	}
	artifactByID := map[string]*SessionSummaryArtifact{}
	naturalSummaryIDs := map[string]bool{}
	for _, artifact := range artifacts {
		artifactByID[artifact.SessionSummaryID] = artifact
		if artifact.NaturalRevisitEligible() {
			naturalSummaryIDs[artifact.SessionSummaryID] = true
		}
	}
	seenIDs := map[string]bool{}
	consumedNaturalSummaryIDs := map[string]bool{}
	var selectedRanks []int
	for _, pair := range []struct {
		selected bool
		rawItems []any
	}{
		{true, triggerInterests},
		{false, rejectedInterests},
	} {
		for _, rawValue := range pair.rawItems {
			raw, rawIsMap := rawValue.(map[string]any)
			if !rawIsMap || !mapKeysEqual(raw, ridDecisionFields) {
				return "manifest_field_rid_result_contract_failed"
			}
			interestID := pyStr(raw["interest_summary_id"])
			if !nonEmptyStr(raw["interest_summary_id"]) || seenIDs[interestID] {
				return "manifest_field_rid_result_contract_failed"
			}
			sourceKind := pyStr(raw["source_kind"])
			if sourceKind != "session_summary" && sourceKind != "long_term_profile" {
				return "manifest_field_rid_result_contract_failed"
			}
			if !raw["selected"].(bool) && truthy(raw["selected"]) == pair.selected && raw["selected"] == nil {
				return "manifest_field_rid_result_contract_failed"
			}
			selectedBool, isBool := raw["selected"].(bool)
			if !isBool || selectedBool != pair.selected {
				return "manifest_field_rid_result_contract_failed"
			}
			if pyStr(raw["score_semantics"]) != "uncalibrated_api_proxy" {
				return "manifest_field_rid_result_contract_failed"
			}
			rank := raw["rank"]
			if pair.selected {
				if !plainInt(rank) || intOf(rank) < 1 {
					return "manifest_field_rid_result_contract_failed"
				}
				selectedRanks = append(selectedRanks, intOf(rank))
			} else if rank != nil {
				return "manifest_field_rid_result_contract_failed"
			}
			if raw["resonance_score_nullable"] != nil {
				return "manifest_field_rid_result_contract_failed"
			}
			for _, field := range []string{"source_session_summary_ids", "source_profile_interest_ids",
				"source_profile_unit_ids", "source_event_ids", "source_locators"} {
				if anyListOf(raw[field]) == nil {
					if _, isList := raw[field].([]any); !isList {
						return "manifest_field_rid_result_contract_failed"
					}
				}
			}
			if sourceKind == "session_summary" {
				artifact, exists := artifactByID[interestID]
				if !exists || !artifact.NaturalRevisitEligible() {
					return "manifest_field_rid_result_contract_failed"
				}
				if !anyEqual(raw["source_session_summary_ids"], []any{interestID}) {
					return "manifest_field_rid_result_contract_failed"
				}
				if len(anyListOf(raw["source_profile_interest_ids"])) != 0 ||
					len(anyListOf(raw["source_profile_unit_ids"])) != 0 {
					return "manifest_field_rid_result_contract_failed"
				}
				var eventIDs []any
				for _, eventID := range artifact.SourceEventIDs {
					eventIDs = append(eventIDs, eventID)
				}
				if !anyEqual(raw["source_event_ids"], eventIDs) {
					return "manifest_field_rid_result_contract_failed"
				}
				var locatorPayloads []any
				for _, locator := range artifact.SourceLocators {
					locatorPayloads = append(locatorPayloads, locator.AsPayload())
				}
				if !anyEqual(raw["source_locators"], locatorPayloads) {
					return "manifest_field_rid_result_contract_failed"
				}
				consumedNaturalSummaryIDs[interestID] = true
			} else {
				if len(anyListOf(raw["source_session_summary_ids"])) != 0 ||
					len(anyListOf(raw["source_event_ids"])) != 0 ||
					len(anyListOf(raw["source_locators"])) != 0 {
					return "manifest_field_rid_result_contract_failed"
				}
				if !anyEqual(raw["source_profile_interest_ids"], []any{interestID}) {
					return "manifest_field_rid_result_contract_failed"
				}
				if !UniqueNonEmptyStrings(raw["source_profile_unit_ids"]) {
					return "manifest_field_rid_result_contract_failed"
				}
			}
			seenIDs[interestID] = true
		}
	}
	// RID 决定每个自然摘要，但全局页不读否定扣留的会话
	sortedRanks := append([]int{}, selectedRanks...)
	sort.Ints(sortedRanks)
	for i, rank := range sortedRanks {
		if rank != i+1 {
			return "manifest_field_rid_result_contract_failed"
		}
	}
	if pyStr(manifest["context_scope"]) != GlobalNewChatScope {
		for id := range naturalSummaryIDs {
			if !consumedNaturalSummaryIDs[id] {
				return "manifest_field_rid_result_contract_failed"
			}
		}
	}
	return ""
}

// ridInterestSelectionRejectionReason RID 兴趣选择校验
// （topic_product.py:1392-1527；分支顺序逐条对应）。
func ridInterestSelectionRejectionReason(manifest map[string]any, cards []map[string]any, persisted bool) string {
	const reason = "manifest_field_rid_interest_selection_contract_failed"
	ridResult, ridIsMap := manifest["rid_result"].(map[string]any)
	if !ridIsMap {
		return reason
	}
	accepted, acceptedIsList := ridResult["trigger_interests"].([]any)
	if !acceptedIsList || (len(accepted) == 0 && !NewsOnlyPool(manifest, cards)) {
		return reason
	}
	var acceptedIDs []string
	for _, item := range accepted {
		if m, isMap := item.(map[string]any); isMap {
			acceptedIDs = append(acceptedIDs, pyStr(m["interest_summary_id"]))
		} else {
			acceptedIDs = append(acceptedIDs, "")
		}
	}
	if len(acceptedIDs) != len(accepted) {
		return reason
	}
	scope := pyStr(manifest["context_scope"])
	selection, selectionIsMap := manifest["rid_interest_selection"].(map[string]any)
	if scope != GlobalNewChatScope {
		if selectionIsMap {
			return reason
		}
		selectedIDs := map[string]bool{}
		for _, id := range acceptedIDs {
			selectedIDs[id] = true
		}
		for _, card := range cards {
			if !selectedIDs[pyStr(card["interest_summary_id"])] {
				return reason
			}
		}
		return ""
	}
	expectedFields := []string{"policy", "selected_interest_summary_ids",
		"original_rid_ranks", "delta_linked_interest_summary_id",
		"short_trigger_delta_event_ids", "extra_recent_slot_interest_summary_id",
		"direction_withheld_candidates", "restated_offered_candidates"}
	// 第四近槽写出前的池继续服役
	if persisted && selectionIsMap {
		if _, present := selection["extra_recent_slot_interest_summary_id"]; !present {
			expectedFields = removeString(expectedFields, "extra_recent_slot_interest_summary_id")
		}
		if _, present := selection["direction_withheld_candidates"]; !present {
			expectedFields = removeString(expectedFields, "direction_withheld_candidates")
		}
		if _, present := selection["restated_offered_candidates"]; !present {
			expectedFields = removeString(expectedFields, "restated_offered_candidates")
		}
	}
	if !selectionIsMap || !mapKeysEqual(selection, expectedFields) {
		return reason
	}
	withheld, withheldIsList := selection["direction_withheld_candidates"].([]any)
	if !withheldIsList {
		return reason
	}
	for _, item := range withheld {
		entry, isMap := item.(map[string]any)
		if !isMap || !mapKeysEqual(entry, []string{"candidate_id", "interest_summary_id", "direction", "title"}) {
			return reason
		}
		for key, value := range entry {
			if _, isString := value.(string); !isString {
				return reason
			}
			_ = key
		}
	}
	restated, restatedIsList := selection["restated_offered_candidates"].([]any)
	if !restatedIsList {
		return reason
	}
	for _, item := range restated {
		entry, isMap := item.(map[string]any)
		if !isMap || !mapKeysEqual(entry, []string{"candidate_id", "interest_summary_id",
			"direction", "title", "offered_title", "similarity"}) {
			return reason
		}
		for key, value := range entry {
			if key == "similarity" {
				sim, isFloat := value.(float64)
				if !isFloat || sim < 0.0 || sim > 1.0 {
					return reason
				}
			} else if _, isString := value.(string); !isString {
				return reason
			}
		}
	}
	selectedIDs, selectedIsList := selection["selected_interest_summary_ids"].([]any)
	extraSlotID := selection["extra_recent_slot_interest_summary_id"]
	if extraSlotID != nil && (!selectedIsList || !anyListContains(selectedIDs, extraSlotID)) {
		return reason
	}
	ranks, ranksIsMap := selection["original_rid_ranks"].(map[string]any)
	deltaID := selection["delta_linked_interest_summary_id"]
	shortDeltaIDs, shortDeltaIsList := selection["short_trigger_delta_event_ids"].([]any)
	// 唯一非空，或显式空表且无接受兴趣
	if !selectedIsList {
		return reason
	}
	uniqueSelected := true
	seenSelected := map[string]bool{}
	for _, id := range selectedIDs {
		if !nonEmptyStr(id) || seenSelected[pyStr(id)] {
			uniqueSelected = false
		}
		seenSelected[pyStr(id)] = true
	}
	if !uniqueSelected && !(len(selectedIDs) == 0 && len(accepted) == 0) {
		return reason
	}
	if !ranksIsMap {
		return reason
	}
	if len(ranks) != len(seenSelected) {
		return reason
	}
	for id := range seenSelected {
		if _, present := ranks[id]; !present {
			return reason
		}
	}
	for i, id := range acceptedIDs {
		if seenSelected[pyStr(id)] && !anyEqual(ranks[pyStr(id)], mapAnyOf(accepted[i])["rank"]) {
			return reason
		}
	}
	for id := range seenSelected {
		if !acceptedIDContains(acceptedIDs, id) {
			return reason
		}
	}
	if deltaID != nil && !acceptedIDContains(acceptedIDs, pyStr(deltaID)) {
		return reason
	}
	if !shortDeltaIsList {
		return reason
	}
	for _, eventID := range shortDeltaIDs {
		if !nonEmptyStr(eventID) {
			return reason
		}
	}
	seenDelta := map[string]bool{}
	for _, eventID := range shortDeltaIDs {
		if seenDelta[pyStr(eventID)] {
			return reason
		}
		seenDelta[pyStr(eventID)] = true
	}
	if pyStr(selection["policy"]) != "all_rid_selected_v1" ||
		!anyEqual(selectedIDs, stringsToAny(acceptedIDs)) {
		return reason
	}
	// 携带卡可以保留本轮被称重但未释放的兴趣
	retainedIDs := RetainedCandidateIds(manifest)
	weighedIDs := map[string]bool{}
	for _, item := range anyListOf(ridResult["rejected_interests"]) {
		if m, isMap := item.(map[string]any); isMap {
			weighedIDs[pyStr(m["interest_summary_id"])] = true
		}
	}
	newsLeadID := NewsLeadCardId(manifest)
	for _, card := range cards {
		interestID := card["interest_summary_id"]
		cardID := card["card_id"]
		if seenSelected[pyStr(interestID)] {
			continue
		}
		if truthy(retainedIDs[pyStr(cardID)]) && weighedIDs[pyStr(interestID)] {
			continue
		}
		if anyEqual(cardID, newsLeadID) &&
			(interestID == nil || weighedIDs[pyStr(interestID)]) {
			continue
		}
		return reason
	}
	return ""
}

// removeString 移除首个匹配元素。
func removeString(values []string, target string) []string {
	result := []string{}
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

// anyListContains any 列表包含（anyEqual 深比较）。
func anyListContains(values []any, target any) bool {
	for _, value := range values {
		if anyEqual(value, target) {
			return true
		}
	}
	return false
}

// acceptedIDContains 接受 ID 表包含。
func acceptedIDContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// ──────────────────────────── ⤴️ 31c 契约切片 D：grounding 系校验器（topic_product.py:1621-1876/:2023-2410） ────────────────────────────

// grounding 字段集（topic_product.py:395-467）。
var (
	groundingDecisionFields = []string{"candidate_id", "interest_summary_id",
		"category", "decision", "terminal_stage", "reason", "external_operations"}
	groundingExternalOperationFields = []string{"operation", "operation_id", "request_id"}
	groundingEvidenceFields          = []string{"schema_version", "input_snapshot_id",
		"context_scope", "document_snapshot", "candidates"}
	groundingDocumentSnapshotFields = []string{"snapshot_date", "snapshot_version",
		"document_revision_ids", "documents"}
	groundingDocumentFields = []string{"document_revision_id", "source_url", "publisher",
		"source_kind", "published_at", "publication_date_provenance",
		"fetched_at", "snapshot_date", "snapshot_version"}
	groundingEvidenceCandidateFields = []string{"candidate_id", "search", "fetches", "extractor"}
	groundingSearchEvidenceFields    = []string{"operation_id", "request_id", "requested_query", "provider_receipt"}
	groundingFetchEvidenceFields     = []string{"operation_id", "request_id", "requested_url",
		"final_url", "http_status_code", "document_revision_id"}
	groundingExtractorEvidenceFields = []string{"operation_id", "request_id",
		"input_document_revision_ids", "component_receipt"}
	groundingAdmissionRejectionReasons = map[string]bool{
		"provider_low_relevance": true, "provider_timeout": true,
		"provider_http_status": true, "provider_challenge": true,
		"provider_proxy": true, "provider_transport": true,
		"provider_unknown": true, "provider_all_engines_failed": true,
		"private_query": true, "extraction_failed": true,
		"insufficient_evidence": true, "no_eligible_document": true,
	}
)

// hostEvidenceDecision 宿主证据决策判定（topic_product.py:3519-3529）。
func hostEvidenceDecision(decision map[string]any) bool {
	category := pyStr(decision["category"])
	if category == "project_continuation" || category == ProfileInterestCategory {
		return true
	}
	terminalStage := pyStr(decision["terminal_stage"])
	return (terminalStage == "fixed_corpus" && fixedCorpusCategoriesOf[category]) ||
		(terminalStage == "hot_list" && category == "profile_recent_progress")
}

// fixedCorpusCategoriesOf 固定语料两类目集合。
var fixedCorpusCategoriesOf = map[string]bool{fixedCorpusCategoryA: true, fixedCorpusCategoryB: true}

// searchReceiptRequestId 搜索小票请求 ID（topic_product.py:877-881）。
func searchReceiptRequestId(receipt map[string]any) string {
	if pyStr(receipt["provider_id"]) == ArkWebSearchProviderID {
		return pyStr(receipt["provider_response_id"])
	}
	return pyStr(receipt["host_request_id"])
}

// groundingDecisionRejectionReason grounding 决策小票校验
// （topic_product.py:1621-1876；分支顺序逐条对应）。
func groundingDecisionRejectionReason(manifest map[string]any, cards []map[string]any) string {
	const reason = "manifest_field_grounding_decision_receipt_contract_failed"
	receipt, isMap := manifest["grounding_decision_receipt"].(map[string]any)
	if !isMap {
		return reason
	}
	if !mapKeysEqual(receipt, []string{"schema_version", "input_snapshot_id",
		"context_scope", "decision", "candidate_decisions"}) {
		return reason
	}
	if pyStr(receipt["schema_version"]) != GroundingDecisionReceiptVersion {
		return reason
	}
	if pyStr(receipt["input_snapshot_id"]) != pyStr(manifest["generation_input_snapshot_id"]) {
		return reason
	}
	if pyStr(receipt["context_scope"]) != pyStr(manifest["context_scope"]) {
		return reason
	}
	candidateDecisions, isList := receipt["candidate_decisions"].([]any)
	if !isList || len(candidateDecisions) == 0 {
		return reason
	}
	auditIndex := ExternalInvocationAuditIndex(manifest)
	if auditIndex == nil {
		return "manifest_field_external_invocation_audit_contract_failed"
	}
	referencedExternalOperations := map[[3]string]bool{}
	operationOwner := map[[3]string]string{}
	decisions := map[string]map[string]any{}
	admittedCount := 0
	for _, rawValue := range candidateDecisions {
		raw, rawIsMap := rawValue.(map[string]any)
		if !rawIsMap || !mapKeysEqual(raw, groundingDecisionFields) {
			return reason
		}
		candidateID := pyStr(raw["candidate_id"])
		if !nonEmptyStr(raw["candidate_id"]) || decisions[candidateID] != nil {
			return reason
		}
		if raw["interest_summary_id"] != nil && !nonEmptyStr(raw["interest_summary_id"]) {
			return reason
		}
		if !nonEmptyStr(raw["category"]) {
			return reason
		}
		if pyStr(raw["decision"]) != "admit" && pyStr(raw["decision"]) != "reject" {
			return reason
		}
		if !nonEmptyStr(raw["terminal_stage"]) || !nonEmptyStr(raw["reason"]) {
			return reason
		}
		operations, opsIsList := raw["external_operations"].([]any)
		if !opsIsList {
			return reason
		}
		var operationKeys [][3]string
		for _, operationValue := range operations {
			operation, opIsMap := operationValue.(map[string]any)
			if !opIsMap || !mapKeysEqual(operation, groundingExternalOperationFields) {
				return reason
			}
			operationName := pyStr(operation["operation"])
			if operationName != "search" && operationName != "fetch" && operationName != "extractor" {
				return reason
			}
			if !nonEmptyStr(operation["operation_id"]) || !nonEmptyStr(operation["request_id"]) {
				return reason
			}
			key := [3]string{operationName, pyStr(operation["operation_id"]), pyStr(operation["request_id"])}
			if containsKey(operationKeys, key) || auditIndex[key] == nil {
				return reason
			}
			if !ExternalOperationCoversCandidate(manifest, auditIndex[key], key, candidateID) {
				return reason
			}
			if _, owned := operationOwner[key]; owned && key[0] != "fetch" {
				return reason
			}
			operationKeys = append(operationKeys, key)
			operationOwner[key] = candidateID
		}
		var operationNames []string
		for _, key := range operationKeys {
			operationNames = append(operationNames, key[0])
		}
		hostDecision := hostEvidenceDecision(raw)
		terminalStage := pyStr(raw["terminal_stage"])
		searchCount := 0
		extractorCount := 0
		for _, name := range operationNames {
			if name == "search" {
				searchCount++
			}
			if name == "extractor" {
				extractorCount++
			}
		}
		chainTailValid := true
		for _, name := range operationNames[minInt(1, len(operationNames)):] {
			if name != "fetch" && name != "extractor" {
				chainTailValid = false
			}
		}
		externalChainInvalid := len(operationNames) == 0 ||
			operationNames[0] != "search" || searchCount != 1 ||
			extractorCount > 1 ||
			(extractorCount == 1 && operationNames[len(operationNames)-1] != "extractor") ||
			!chainTailValid
		if hostDecision {
			// 宿主证据（兴趣/项目/语料/热榜）不做自有检索，因此不带外部操作
			if len(operationNames) > 0 {
				return reason
			}
		} else if externalChainInvalid {
			return reason
		}
		if !TerminalStageMatchesOperations(terminalStage, operationNames) {
			return reason
		}
		for _, key := range operationKeys {
			referencedExternalOperations[key] = true
		}
		var auditEntries []map[string]any
		for _, key := range operationKeys {
			auditEntries = append(auditEntries, auditIndex[key])
		}
		validBeforeTerminal := func(entry map[string]any) bool {
			return pyStr(entry["status"]) == "success" ||
				(pyStr(entry["operation"]) == "fetch" &&
					pyStr(entry["status"]) == "failure" &&
					pyStr(entry["failure_reason"]) == "provider_invocation_failed")
		}
		decision := pyStr(raw["decision"])
		if decision == "admit" {
			var expectedStage, expectedReason string
			switch {
			case pyStr(raw["category"]) == "project_continuation":
				expectedStage, expectedReason = "project_history", "project_history_exact_locator"
			case pyStr(raw["category"]) == ProfileInterestCategory:
				expectedStage, expectedReason = "profile_interest", "profile_interest_exact_summary"
			case terminalStage == "hot_list":
				expectedStage, expectedReason = "hot_list", "hot_list_entry"
			case hostDecision:
				expectedStage, expectedReason = "fixed_corpus", "fixed_corpus_entry"
			default:
				expectedStage, expectedReason = "admission", "admitted"
			}
			if terminalStage != expectedStage || pyStr(raw["reason"]) != expectedReason {
				return reason
			}
			hasFetchSuccess := false
			for _, entry := range auditEntries {
				if pyStr(entry["operation"]) == "fetch" && pyStr(entry["status"]) == "success" {
					hasFetchSuccess = true
				}
			}
			failed := !hostDecision && (!hasFetchSuccess ||
				len(auditEntries) == 0 ||
				pyStr(auditEntries[len(auditEntries)-1]["operation"]) != "extractor" ||
				pyStr(auditEntries[len(auditEntries)-1]["status"]) != "success" ||
				anyEntryInvalid(auditEntries, validBeforeTerminal))
			if failed {
				return reason
			}
			admittedCount++
		} else if terminalStage == "search" || terminalStage == "fetch" ||
			terminalStage == "extraction" || terminalStage == "offset_validation" {
			expectedOperation := map[string]string{
				"search": "search", "fetch": "fetch",
				"extraction": "extractor", "offset_validation": "extractor",
			}[terminalStage]
			if len(auditEntries) == 0 {
				return reason
			}
			if pyStr(auditEntries[len(auditEntries)-1]["operation"]) != expectedOperation {
				return reason
			}
			if terminalStage == "fetch" {
				matchFailure := false
				for _, entry := range auditEntries {
					if pyStr(entry["operation"]) == "fetch" && pyStr(entry["status"]) == "failure" &&
						pyStr(entry["failure_reason"]) == pyStr(raw["reason"]) {
						matchFailure = true
					}
				}
				if !matchFailure || anyEntryInvalid(auditEntries, validBeforeTerminal) {
					return reason
				}
			} else {
				invalidPrefix := false
				for _, entry := range auditEntries[:len(auditEntries)-1] {
					if !validBeforeTerminal(entry) {
						invalidPrefix = true
					}
				}
				hasFetchSuccess := false
				for _, entry := range auditEntries[:len(auditEntries)-1] {
					if pyStr(entry["operation"]) == "fetch" && pyStr(entry["status"]) == "success" {
						hasFetchSuccess = true
					}
				}
				failed := terminalStage != "fetch" && (invalidPrefix ||
					pyStr(auditEntries[len(auditEntries)-1]["status"]) != "failure" ||
					pyStr(auditEntries[len(auditEntries)-1]["failure_reason"]) != pyStr(raw["reason"]) ||
					((terminalStage == "extraction" || terminalStage == "offset_validation") && !hasFetchSuccess))
				if failed {
					return reason
				}
			}
		} else if terminalStage == "admission" {
			if anyEntryInvalid(auditEntries, validBeforeTerminal) {
				return reason
			}
			if !groundingAdmissionRejectionReasons[pyStr(raw["reason"])] {
				return reason
			}
			if len(operationNames) > 0 && operationNames[len(operationNames)-1] == "extractor" {
				lastSuccess := pyStr(auditEntries[len(auditEntries)-1]["status"]) == "success"
				hasFetchSuccess := false
				for _, entry := range auditEntries[:len(auditEntries)-1] {
					if pyStr(entry["operation"]) == "fetch" && pyStr(entry["status"]) == "success" {
						hasFetchSuccess = true
					}
				}
				if !lastSuccess || !hasFetchSuccess {
					return reason
				}
			}
		} else {
			return reason
		}
		decisions[candidateID] = raw
	}
	if len(referencedExternalOperations) != len(auditIndex) {
		return reason
	}
	expectedDecision := "partial"
	if admittedCount == len(decisions) {
		expectedDecision = "admit"
	} else if admittedCount == 0 {
		expectedDecision = "abstain"
	}
	if pyStr(receipt["decision"]) != expectedDecision {
		return reason
	}
	for _, card := range cards {
		decision := decisions[pyStr(card["card_id"])]
		if decision == nil {
			return reason
		}
		if pyStr(decision["decision"]) != "admit" ||
			pyStr(decision["category"]) != pyStr(card["category"]) ||
			!anyEqual(decision["interest_summary_id"], card["interest_summary_id"]) {
			return reason
		}
	}
	return ""
}

// containsKey 键表包含。
func containsKey(keys [][3]string, target [3]string) bool {
	for _, key := range keys {
		if key == target {
			return true
		}
	}
	return false
}

// anyEntryInvalid 任一条目校验失败。
func anyEntryInvalid(entries []map[string]any, valid func(map[string]any) bool) bool {
	for _, entry := range entries {
		if !valid(entry) {
			return true
		}
	}
	return false
}

// GroundingEvidenceReceiptRejectionReason grounding 证据小票校验
// （topic_product.py:2023-2410；分支顺序逐条对应）。
func GroundingEvidenceReceiptRejectionReason(manifest any, cards []map[string]any) string {
	const failure = "manifest_field_grounding_evidence_receipt_contract_failed"
	root, manifestIsMap := manifest.(map[string]any)
	if !manifestIsMap {
		return failure
	}
	receipt, isMap := root["grounding_evidence_receipt"].(map[string]any)
	decisionReceipt, drIsMap := root["grounding_decision_receipt"].(map[string]any)
	var rawDecisions []any
	if drIsMap {
		rawDecisions, _ = decisionReceipt["candidate_decisions"].([]any)
	}
	if !isMap {
		return failure
	}
	if !mapKeysEqual(receipt, groundingEvidenceFields) {
		return failure
	}
	if pyStr(receipt["schema_version"]) != GroundingEvidenceReceiptVersion {
		return failure
	}
	if pyStr(receipt["input_snapshot_id"]) != pyStr(root["generation_input_snapshot_id"]) {
		return failure
	}
	if pyStr(receipt["context_scope"]) != pyStr(root["context_scope"]) {
		return failure
	}
	if rawDecisions == nil {
		if _, isList := decisionReceipt["candidate_decisions"].([]any); !isList {
			return failure
		}
	}
	if len(rawDecisions) == 0 {
		return failure
	}
	decisions := map[string]map[string]any{}
	for _, decisionValue := range rawDecisions {
		decision, isMap := decisionValue.(map[string]any)
		candidateID := ""
		if isMap {
			candidateID = pyStr(decision["candidate_id"])
		}
		if candidateID == "" || decisions[candidateID] != nil {
			return failure
		}
		if _, isList := decision["external_operations"].([]any); !isList {
			return failure
		}
		decisions[candidateID] = decision
	}

	// 文档快照
	snapshot, snapIsMap := receipt["document_snapshot"].(map[string]any)
	if !snapIsMap || !mapKeysEqual(snapshot, groundingDocumentSnapshotFields) {
		return failure
	}
	if !nonEmptyStr(snapshot["snapshot_date"]) || !nonEmptyStr(snapshot["snapshot_version"]) {
		return failure
	}
	documentIDs, idsIsList := snapshot["document_revision_ids"].([]any)
	documentsRaw, docsIsList := snapshot["documents"].([]any)
	if !idsIsList || !docsIsList {
		return failure
	}
	seenDocIDs := map[string]bool{}
	for _, id := range documentIDs {
		if !nonEmptyStr(id) || seenDocIDs[pyStr(id)] {
			return failure
		}
		seenDocIDs[pyStr(id)] = true
	}
	documents := map[string]map[string]any{}
	var documentOrder []string
	for _, documentValue := range documentsRaw {
		document, docIsMap := documentValue.(map[string]any)
		if !docIsMap || !mapKeysEqual(document, groundingDocumentFields) {
			return failure
		}
		documentID := pyStr(document["document_revision_id"])
		publishedAt := document["published_at"]
		if documentID == "" || documents[documentID] != nil {
			return failure
		}
		if !ValidHTTPURL(document["source_url"]) ||
			!nonEmptyStr(document["publisher"]) || !nonEmptyStr(document["source_kind"]) {
			return failure
		}
		if publishedAt != nil && !nonEmptyStr(publishedAt) {
			return failure
		}
		switch fetchedAt := document["fetched_at"].(type) {
		case int:
			if float64(fetchedAt) < 0 {
				return failure
			}
		case float64:
			if math.IsInf(fetchedAt, 0) || math.IsNaN(fetchedAt) || fetchedAt < 0 {
				return failure
			}
		default:
			return failure
		}
		if pyStr(document["snapshot_date"]) != pyStr(snapshot["snapshot_date"]) ||
			pyStr(document["snapshot_version"]) != pyStr(snapshot["snapshot_version"]) {
			return failure
		}
		if !ValidGroundingPublicationDateProvenance(document["publication_date_provenance"],
			pyStr(document["source_url"]), publishedAt) {
			return failure
		}
		documents[documentID] = document
		documentOrder = append(documentOrder, documentID)
	}
	if !anyEqual(documentIDs, stringsToAny(documentOrder)) {
		return failure
	}
	if ds, present := root["document_snapshot"]; present && !anyEqual(ds, snapshot) {
		return failure
	}

	auditIndex := ExternalInvocationAuditIndex(root)
	rawCandidates, candidatesIsList := receipt["candidates"].([]any)
	if auditIndex == nil || !candidatesIsList {
		return failure
	}
	cardsByID := map[string]map[string]any{}
	for _, card := range cards {
		if card != nil {
			cardsByID[pyStr(card["card_id"])] = card
		}
	}
	for _, candidateValue := range rawCandidates {
		candidate, candIsMap := candidateValue.(map[string]any)
		candidateID := ""
		if candIsMap {
			candidateID = pyStr(candidate["candidate_id"])
		}
		if candidateID == "" || evidenceCandidateSeen(candidateID, rawCandidates) {
			return failure
		}
		if !mapKeysEqual(candidate, groundingEvidenceCandidateFields) {
			return failure
		}
		decision := decisions[candidateID]
		if decision == nil {
			return failure
		}
		fetches, fetchesIsList := candidate["fetches"].([]any)
		if !fetchesIsList {
			return failure
		}
		expectedOperations, _ := decision["external_operations"].([]any)
		if hostEvidenceDecision(decision) {
			if candidate["search"] != nil || len(fetches) > 0 || candidate["extractor"] != nil {
				return failure
			}
			if len(expectedOperations) > 0 {
				return failure
			}
			continue
		}
		// 搜索证据
		search, searchIsMap := candidate["search"].(map[string]any)
		if !searchIsMap || !mapKeysEqual(search, groundingSearchEvidenceFields) {
			return failure
		}
		for _, field := range []string{"operation_id", "request_id", "requested_query"} {
			if !nonEmptyStr(search[field]) {
				return failure
			}
		}
		searchKey := [3]string{"search", pyStr(search["operation_id"]), pyStr(search["request_id"])}
		searchAudit := auditIndex[searchKey]
		if searchAudit == nil || pyStr(searchAudit["candidate_id"]) != candidateID {
			return failure
		}
		providerReceipt := search["provider_receipt"]
		var resultURLs []string
		if pyStr(searchAudit["status"]) == "failure" {
			failureReason := searchAudit["failure_reason"]
			nativeFailure := openjiuwenProviderIDs[pyStr(searchAudit["provider_id"])] &&
				nativeFailureReasons[pyStr(failureReason)]
			validFailureReceipt := false
			if pr, prIsMap := providerReceipt.(map[string]any); prIsMap &&
				pyStr(searchAudit["provider_id"]) == pyStr(pr["provider_id"]) &&
				ValidNativeSearchFailureReceipt(pr, failureReason) {
				validFailureReceipt = true
			}
			if (nativeFailure && !validFailureReceipt) || (!nativeFailure && providerReceipt != nil) {
				return failure
			}
			resultURLs = []string{}
		} else {
			pr, prIsMap := providerReceipt.(map[string]any)
			if !prIsMap {
				return failure
			}
			validated := GroundingSearchResultUrls(pr, pyStr(search["requested_query"]),
				pyStr(searchAudit["provider_id"]))
			if validated == nil {
				return failure
			}
			resultURLs = validated
		}
		var operationKeys [][3]string
		operationKeys = append(operationKeys, searchKey)
		for _, fetchValue := range fetches {
			fetch, fetchIsMap := fetchValue.(map[string]any)
			if !fetchIsMap || !mapKeysEqual(fetch, groundingFetchEvidenceFields) {
				return failure
			}
			for _, field := range []string{"operation_id", "request_id"} {
				if !nonEmptyStr(fetch[field]) {
					return failure
				}
			}
			if !ValidHTTPURL(fetch["requested_url"]) ||
				!stringListContains(resultURLs, pyStr(fetch["requested_url"])) {
				return failure
			}
			fetchKey := [3]string{"fetch", pyStr(fetch["operation_id"]), pyStr(fetch["request_id"])}
			fetchAudit := auditIndex[fetchKey]
			if fetchAudit == nil || !ExternalOperationCoversCandidate(root, fetchAudit, fetchKey, candidateID) {
				return failure
			}
			finalURL := fetch["final_url"]
			statusCode := fetch["http_status_code"]
			documentID := fetch["document_revision_id"]
			if pyStr(fetchAudit["status"]) == "failure" {
				if finalURL != nil || statusCode != nil || documentID != nil {
					return failure
				}
			} else {
				if !ValidHTTPURL(finalURL) {
					return failure
				}
				if statusCode != nil && (!plainInt(statusCode) || intOf(statusCode) < 0) {
					return failure
				}
				if documentID != nil {
					if !nonEmptyStr(documentID) || documents[pyStr(documentID)] == nil {
						return failure
					}
					doc := documents[pyStr(documentID)]
					if pyStr(doc["source_url"]) != pyStr(finalURL) {
						return failure
					}
					if provenance, provIsMap := doc["publication_date_provenance"].(map[string]any); provIsMap &&
						pyStr(provenance["source_type"]) == "structured_search_result" {
						pr, prIsMap := providerReceipt.(map[string]any)
						if !prIsMap ||
							pyStr(provenance["search_provider_id"]) != pyStr(searchAudit["provider_id"]) ||
							pyStr(provenance["search_receipt_id"]) != searchReceiptRequestId(pr) ||
							!stringListContains(resultURLs, pyStr(provenance["search_result_url"])) {
							return failure
						}
					}
				}
			}
			operationKeys = append(operationKeys, fetchKey)
		}
		// linked_page 必须指向本候选的一次实际抓取
		for _, fetchValue := range fetches {
			fetched, _ := fetchValue.(map[string]any)
			document := documents[pyStr(fetched["document_revision_id"])]
			provenance := mapAnyOf(document["publication_date_provenance"])
			if pyStr(provenance["source_type"]) != "linked_document_metadata" {
				continue
			}
			linked := mapAnyOf(provenance["linked_page"])
			matched := false
			for _, otherValue := range fetches {
				other, _ := otherValue.(map[string]any)
				if pyStr(other["final_url"]) == pyStr(linked["date_page_url"]) &&
					pyStr(other["operation_id"]) == pyStr(linked["operation_id"]) &&
					pyStr(other["request_id"]) == pyStr(linked["request_id"]) &&
					anyEqual(other["http_status_code"], linked["http_status_code"]) {
					matched = true
				}
			}
			if !matched {
				return failure
			}
		}
		// 提取器证据
		var extractor map[string]any
		if candidate["extractor"] != nil {
			extractor, _ = candidate["extractor"].(map[string]any)
			if extractor == nil || !mapKeysEqual(extractor, groundingExtractorEvidenceFields) {
				return failure
			}
			for _, field := range []string{"operation_id", "request_id"} {
				if !nonEmptyStr(extractor[field]) {
					return failure
				}
			}
			if !UniqueNonEmptyStrings(extractor["input_document_revision_ids"]) {
				return failure
			}
			for _, id := range anyListOf(extractor["input_document_revision_ids"]) {
				if documents[pyStr(id)] == nil {
					return failure
				}
			}
			extractorKey := [3]string{"extractor", pyStr(extractor["operation_id"]), pyStr(extractor["request_id"])}
			extractorAudit := auditIndex[extractorKey]
			if extractorAudit == nil || pyStr(extractorAudit["candidate_id"]) != candidateID {
				return failure
			}
			componentReceipt := extractor["component_receipt"]
			if componentReceipt == nil {
				if !(pyStr(extractorAudit["status"]) == "failure" &&
					pyStr(decision["reason"]) == "extraction_failed") {
					return failure
				}
			} else if !ValidGroundingComponentReceipt(componentReceipt,
				pyStr(root["generation_input_snapshot_id"]),
				pyStr(extractor["request_id"]),
				pyStr(extractorAudit["status"]),
				extractorAudit["failure_reason"]) {
				return failure
			}
			operationKeys = append(operationKeys, extractorKey)
		}
		var expectedKeys [][3]string
		for _, operationValue := range expectedOperations {
			operation, _ := operationValue.(map[string]any)
			expectedKeys = append(expectedKeys, [3]string{
				pyStr(operation["operation"]), pyStr(operation["operation_id"]), pyStr(operation["request_id"])})
		}
		if len(operationKeys) != len(expectedKeys) {
			return failure
		}
		for i, key := range operationKeys {
			if key != expectedKeys[i] {
				return failure
			}
		}
		// 卡片小票一致
		card := cardsByID[candidateID]
		if card != nil {
			cardReceipt := card["search_receipt"]
			evidenceSearchReceipt := map[string]any{}
			if pr, prIsMap := providerReceipt.(map[string]any); prIsMap {
				for key, value := range pr {
					evidenceSearchReceipt[key] = value
				}
			}
			delete(evidenceSearchReceipt, "result_urls")
			if pyStr(evidenceSearchReceipt["provider_id"]) == ArkWebSearchProviderID {
				delete(evidenceSearchReceipt, "result_urls")
			}
			if !anyEqual(evidenceSearchReceipt, cardReceipt) {
				return failure
			}
			cardDocumentID := mapAnyOf(card["evidence_bundle"])["document_revision_id"]
			if documents[pyStr(cardDocumentID)] == nil {
				return failure
			}
			if extractor == nil {
				return failure
			}
			if !anyListContains(anyListOf(extractor["input_document_revision_ids"]), cardDocumentID) {
				return failure
			}
		}
	}
	// 决策与证据候选集合恰等
	decisionCount := 0
	for _, decisionValue := range rawDecisions {
		decision, _ := decisionValue.(map[string]any)
		if decision != nil && decisions[pyStr(decision["candidate_id"])] != nil {
			decisionCount++
		}
	}
	evidenceCount := 0
	seenEvidence := map[string]bool{}
	for _, candidateValue := range rawCandidates {
		candidate, _ := candidateValue.(map[string]any)
		if candidate != nil {
			id := pyStr(candidate["candidate_id"])
			if !seenEvidence[id] {
				seenEvidence[id] = true
				evidenceCount++
			}
		}
	}
	if decisionCount != evidenceCount {
		return failure
	}
	return ""
}

// evidenceCandidateSeen 候选是否重复出现（Python candidate_id in evidence_candidates 守卫）。
func evidenceCandidateSeen(candidateID string, rawCandidates []any) bool {
	seen := 0
	for _, candidateValue := range rawCandidates {
		candidate, _ := candidateValue.(map[string]any)
		if candidate != nil && pyStr(candidate["candidate_id"]) == candidateID {
			seen++
		}
	}
	return seen > 1
}

// openjiuwenProviderIDs 原生搜索提供方集合（topic_product.py:228-233）。
var openjiuwenProviderIDs = map[string]bool{
	OpenjiuwenNativeSearchProviderID: true, LegacyOpenjiuwenNativeSearchProviderID: true,
}

// nativeFailureReasons 原生失败原因值集合。
var nativeFailureReasons = func() map[string]bool {
	result := map[string]bool{}
	for _, reason := range OpenjiuwenNativeSearchFailureReasonByKind {
		result[reason] = true
	}
	return result
}()

// stringListContains 字符串表包含。
func stringListContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// TerminalLearningLineageRejectionReason 串联宿主 grounding/摘要/RID 生产血缘校验
// （topic_product.py:1529-1576 terminal_learning_lineage_rejection_reason）。
func TerminalLearningLineageRejectionReason(manifest any, cards []map[string]any,
	allowLegacySummaryCoverage bool) string {
	root, isMap := manifest.(map[string]any)
	if !isMap {
		return "manifest_type_contract_failed"
	}
	for _, field := range []string{"grounding_decision_receipt",
		"grounding_evidence_receipt", "session_summary_artifacts",
		"session_summary_coverage_receipt", "rid_result", "rid_interest_selection"} {
		if _, present := root[field]; !present {
			return fmt.Sprintf("manifest_required_field_%s_missing", field)
		}
	}
	if reason := groundingDecisionRejectionReason(root, cards); reason != "" {
		return reason
	}
	if reason := GroundingEvidenceReceiptRejectionReason(root, cards); reason != "" {
		return reason
	}
	if !ValidSessionSummaryArtifacts(root) {
		return "manifest_field_session_summary_artifacts_contract_failed"
	}
	if !ValidSessionSummaryCoverageReceipt(root, allowLegacySummaryCoverage) {
		return "manifest_field_session_summary_coverage_receipt_contract_failed"
	}
	if !ValidSessionSummaryMaterializationReceipt(root) {
		return "manifest_field_session_summary_materialization_receipt_contract_failed"
	}
	if reason := ridResultRejectionReason(root, NewsOnlyPool(root, cards)); reason != "" {
		return reason
	}
	return ridInterestSelectionRejectionReason(root, cards, allowLegacySummaryCoverage)
}
