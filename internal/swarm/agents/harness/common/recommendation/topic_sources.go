package recommendation

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 本文件对齐 topic_sources.py（757 行）的前半部分（:1-413 现场检索准入）与
// 后半部分（:415-757 固定起始语料包与 I3 检索）。Python 符号:行号逐条标注。
//
// 已知偏差（spec §5 登记）：
//   - DefaultSearch 依赖 harness 工具域 `search_tools.run_free_search_structured`
//     （search_tools.py:302，属工具域章节，本包不重复实现），DefaultFetch 依赖
//     31c 的 topic_page_fetch.go（⤴️ 31c Task 6-9）——两者在接线前 fail-closed
//     返回显式错误；grounding 逻辑本身经注入的 SearchFn/FetchFn 全量可用。
//   - _open_corpus 的 importlib.resources 包内读取映射为外部资源目录解析
//     （UAPCLAW_RESOURCES_DIR → 可执行文件同目录 → 工作目录，31a 同款先例）。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_sources.py

// ──────────────────────────── 常量 ────────────────────────────

const (
	// RetrievalConfigVersion 现场检索配置版本（topic_sources.py:25）
	RetrievalConfigVersion = "topic-starter-live-retrieval-v3"
	// MaxSearchResults 每次查询的搜索结果上限（:26）
	MaxSearchResults = 5
	// MaxFetchesPerCandidate 单候选抓取上限（:27）
	MaxFetchesPerCandidate = 3
	// SearchTimeoutSeconds 搜索超时秒数（:28）
	SearchTimeoutSeconds = 30
	// FetchTimeoutSeconds 抓取超时秒数（:29）
	FetchTimeoutSeconds = 30
	// EvidenceTextLimit 证据文本截断长度（:30）
	EvidenceTextLimit = 2000
	// CorpusVersion 语料包版本（:415）
	CorpusVersion = "topic-starter-corpus-v1"
	// CorpusResource 语料包资源路径（:416）
	CorpusResource = "resources/recommendation/topic_corpus.jsonl"
	// RetrievalTopK I3 检索 top_k 上限（:427）
	RetrievalTopK = 24
	// I3RetrievalPromptVersion I3 检索提示词版本（:428）
	I3RetrievalPromptVersion = "topic-starter-i3-retrieval-v1"
)

// i3SystemPrompt I3 系统提示词（topic_sources.py:429-437，逐字复刻）
const i3SystemPrompt = "You are the fixed starter retriever (" + I3RetrievalPromptVersion + ").\n" +
	"Rank at most top_k entries from the supplied corpus for the supplied interests.\n" +
	"Use only starter_id values from corpus and interest_summary_id values from\n" +
	"ranked_interests. Return exactly\n" +
	"{\"hits\":[{\"starter_id\":\"...\",\"interest_summary_id\":\"...\"}}]}.\n" +
	"For a cold request with no ranked_interests, interest_summary_id must be null.\n" +
	"Never return starter text, rewrites, explanations, scores, or extra fields.\n"

// topicBuckets 话题桶集合（topic_sources.py:417）
var topicBuckets = map[string]struct{}{
	"生活": {}, "科学": {}, "文化": {}, "实用": {}, "好奇": {},
}

// hookTypes 钩子类型集合（:418）
var hookTypes = map[string]struct{}{
	"surprise": {}, "relatability": {}, "self_curiosity": {},
}

// sourcesFreshnessClasses 保鲜等级集合（:419-421）
var sourcesFreshnessClasses = map[string]struct{}{
	"current_event": {}, "recent_development": {}, "evergreen_corpus": {},
}

// corpusFields 语料行冻结字段全集（:438-448）
var corpusFields = map[string]struct{}{
	"starter_id": {}, "text": {}, "topic": {}, "hook_type": {},
	"freshness_class": {}, "created_at": {}, "expires_at": {},
	"corpus_version": {}, "origin": {},
}

// ──────────────────────────── 结构体 ────────────────────────────

// SearchFn 搜索函数签名（Python SearchFn :32-37）
type SearchFn = func(ctx context.Context, query string, maxResults, timeoutSeconds int) (string, []map[string]string, error)

// FetchFn 抓取函数签名（Python FetchFn :38-39）
type FetchFn = func(ctx context.Context, target string, timeoutSeconds int) (map[string]any, error)

// SourceAssessment 来源评估结果（Python dataclass :47-50）
type SourceAssessment struct {
	// Kind 来源种类（authoritative / original / reliable_report / stable / unknown）
	Kind string
	// Publisher 发布方主机名
	Publisher string
}

// SourceClassifier 来源分类器签名（Python :52）
type SourceClassifier = func(targetURL, content string) SourceAssessment

// FixedStarterCorpusError 语料包契约违约（Python :452-454）
type FixedStarterCorpusError struct {
	// Reason 违约描述
	Reason string
}

// Error 实现 error
func (e *FixedStarterCorpusError) Error() string { return e.Reason }

// FixedStarterRetrievalError I3 输出违反冻结契约（Python :456-459）
type FixedStarterRetrievalError struct {
	// Reason 违约描述
	Reason string
}

// Error 实现 error（对齐 reason 前缀 "retrieval_contract_failed: ..."）
func (e *FixedStarterRetrievalError) Error() string {
	return "retrieval_contract_failed: " + e.Reason
}

// FixedStarter 语料行（Python dataclass :461-471）
type FixedStarter struct {
	// StarterId 起始语 ID
	StarterID string
	// Text 起始语文本
	Text string
	// Topic 话题桶
	Topic string
	// HookType 钩子类型
	HookType string
	// FreshnessClass 保鲜等级
	FreshnessClass string
	// CreatedAt 创建时间
	CreatedAt string
	// ExpiresAt 过期时间（可空）
	ExpiresAt any
	// CorpusVersion 语料版本
	CorpusVersion string
	// Origin 来源（固定 project_authored）
	Origin string
}

// SourceLocator 定位子二元组（Python SourceLocator :473）
type SourceLocator struct {
	// SessionID 会话 ID
	SessionID string
	// Ordinal 序号
	Ordinal int
}

// CorpusRankedInterest I3 输入的语料侧兴趣（Python dataclass :476-482）
type CorpusRankedInterest struct {
	// InterestSummaryID 兴趣摘要 ID
	InterestSummaryID string
	// InterestSummary 兴趣摘要文本
	InterestSummary string
	// Rank 秩
	Rank int
	// AllowedAnchors 允许锚点
	AllowedAnchors []string
	// SourceLocators 源定位子
	SourceLocators []SourceLocator
}

// FixedStarterRankingRequest I3 排序请求（Python dataclass :485-490）
type FixedStarterRankingRequest struct {
	// RankedInterests 排序后的兴趣
	RankedInterests []CorpusRankedInterest
	// Corpus 语料
	Corpus []FixedStarter
	// CorpusVersion 语料版本
	CorpusVersion string
	// TopK 上限
	TopK int
}

// FixedStarterRankingAdapter 可替换 I3 模型边界（Python Protocol :493-501）
type FixedStarterRankingAdapter interface {
	// RankFixedStarters 对一次请求做语料排序
	RankFixedStarters(ctx context.Context, request FixedStarterRankingRequest) (any, error)
}

// StarterHit 语料命中物化结果（Python dataclass :525-539）
type StarterHit struct {
	// StarterId 起始语 ID
	StarterID string
	// InterestSummaryID 兴趣摘要 ID（冷启动为 nil）
	InterestSummaryID any
	// Rank 命中秩（1 起连续）
	Rank int
	// Text 语料文本（精确回填）
	Text string
	// Topic 话题桶
	Topic string
	// HookType 钩子类型
	HookType string
	// FreshnessClass 保鲜等级
	FreshnessClass string
	// CreatedAt 创建时间
	CreatedAt string
	// ExpiresAt 过期时间
	ExpiresAt any
	// CorpusVersion 语料版本
	CorpusVersion string
	// Origin 来源
	Origin string
	// MatchedAnchor 命中的允许锚点（可空）
	MatchedAnchor any
	// SourceLocators 源定位子
	SourceLocators []SourceLocator
}

// ArkFixedStarterRankingAdapter I3 专属 Ark 提示词适配器（Python :508-523）
type ArkFixedStarterRankingAdapter struct {
	invokeStage StageInvokerFunc
}

// NewArkFixedStarterRankingAdapter 构造（Python __init__ :510-512）
func NewArkFixedStarterRankingAdapter(invokeStage StageInvokerFunc) *ArkFixedStarterRankingAdapter {
	return &ArkFixedStarterRankingAdapter{invokeStage: invokeStage}
}

// RankFixedStarters 调用 fixed_starter_retriever 阶段（Python :514-523）
func (a *ArkFixedStarterRankingAdapter) RankFixedStarters(ctx context.Context,
	request FixedStarterRankingRequest) (any, error) {
	rankedPayload := make([]any, 0, len(request.RankedInterests))
	for _, interest := range request.RankedInterests {
		anchors := make([]any, 0, len(interest.AllowedAnchors))
		for _, anchor := range interest.AllowedAnchors {
			anchors = append(anchors, anchor)
		}
		rankedPayload = append(rankedPayload, map[string]any{
			"interest_summary_id": interest.InterestSummaryID,
			"interest_summary":    interest.InterestSummary,
			"rank":                interest.Rank,
			"allowed_anchors":     anchors,
		})
	}
	corpusPayload := make([]any, 0, len(request.Corpus))
	for _, starter := range request.Corpus {
		corpusPayload = append(corpusPayload, map[string]any{
			"starter_id": starter.StarterID,
			"text":       starter.Text,
			"topic":      starter.Topic,
			"hook_type":  starter.HookType,
		})
	}
	payload := map[string]any{
		"corpus_version":   request.CorpusVersion,
		"top_k":            request.TopK,
		"ranked_interests": rankedPayload,
		"corpus":           corpusPayload,
	}
	return a.invokeStage(ctx, "fixed_starter_retriever", i3SystemPrompt, marshalCompactPayload(payload))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultSearch 默认搜索接线（Python default_search :55-61）。
// ⤴️ 工具域：search_tools.run_free_search_structured（search_tools.py:302）未迁
// 前恒返回显式错误（fail-closed）。
func DefaultSearch(ctx context.Context, _ string, _, _ int) (string, []map[string]string, error) {
	return "", nil, fmt.Errorf("default_search: run_free_search_structured 尚未接线（⤴️ 工具域 search_tools）")
}

// DefaultFetch 默认抓取接线（Python default_fetch :63-76）。
// ⤴️ 31c：topic_page_fetch.go（fetch_msn_detail_sync/fetch_topic_page_sync）未迁
// 前恒返回显式错误（fail-closed）；公网 URL 守卫语义由 31c 交付的
// topic_url_safety.go 在 31c 接线时前置。
func DefaultFetch(ctx context.Context, _ string, _ int) (map[string]any, error) {
	return nil, fmt.Errorf("default_fetch: topic_page_fetch 尚未接线（⤴️ 10.3.31c）")
}

// DefaultSourceClassifier 保守主机分类；未知发布方 fail-closed
// （Python default_source_classifier :78-100，步骤逐条对应）
func DefaultSourceClassifier(targetURL, _ string) SourceAssessment {
	parsed, err := url.Parse(targetURL)
	hostname := ""
	if err == nil {
		hostname = strings.ToLower(parsed.Hostname())
	}
	hostname = strings.TrimPrefix(hostname, "www.")
	authoritativeSuffixes := []string{
		".gov.cn", ".gov", ".edu.cn", ".edu", ".ac.cn", "who.int", "un.org",
	}
	kind := "unknown"
	if hostname != "" {
		for _, suffix := range authoritativeSuffixes {
			if hostname == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(hostname, suffix) {
				kind = "authoritative"
				break
			}
		}
	}
	return SourceAssessment{Kind: kind, Publisher: hostname}
}

// LoadFixedStarterCorpus 加载并校验一版不可变包内语料
// （Python load_fixed_starter_corpus :583-625，步骤顺序逐条对应）
func LoadFixedStarterCorpus(corpusVersion string) ([]FixedStarter, error) {
	// 步骤 1：逐行读取与解析（:590-607）
	starters := []FixedStarter{}
	handle, err := openCorpus()
	if err != nil {
		return nil, err
	}
	defer func() { _ = handle.Close() }()
	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		rawLine := scanner.Text()
		lineNumber++
		if strings.TrimSpace(rawLine) == "" {
			return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
				"line %d: blank JSONL rows are not allowed", lineNumber)}
		}
		var row any
		if jsonErr := json.Unmarshal([]byte(rawLine), &row); jsonErr != nil {
			return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
				"line %d: invalid JSON", lineNumber)}
		}
		starter, parseErr := parseStarter(row, lineNumber)
		if parseErr != nil {
			return nil, parseErr
		}
		starters = append(starters, *starter)
	}
	if err := scanner.Err(); err != nil {
		return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf("corpus read failed: %v", err)}
	}

	// 步骤 2：数量闸（:609-612）
	if len(starters) < 100 || len(starters) > 150 {
		return nil, &FixedStarterCorpusError{Reason: "corpus must contain between 100 and 150 starters"}
	}

	// 步骤 3：ID 唯一（:613-616）
	seen := map[string]struct{}{}
	for _, starter := range starters {
		if _, dup := seen[starter.StarterID]; dup {
			return nil, &FixedStarterCorpusError{Reason: "starter_id must be unique"}
		}
		seen[starter.StarterID] = struct{}{}
	}

	// 步骤 4：版本一致（:617-622）
	for _, starter := range starters {
		if starter.CorpusVersion != corpusVersion {
			return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
				"corpus version mismatch: expected %q", corpusVersion)}
		}
	}

	// 步骤 5：桶计数（:623-631）
	bucketCounts := map[string]int{}
	for _, starter := range starters {
		bucketCounts[starter.Topic]++
	}
	if len(bucketCounts) != len(topicBuckets) {
		return nil, &FixedStarterCorpusError{Reason: "each topic bucket must contain between 20 and 30 starters"}
	}
	for bucket, count := range bucketCounts {
		if _, known := topicBuckets[bucket]; !known {
			return nil, &FixedStarterCorpusError{Reason: "each topic bucket must contain between 20 and 30 starters"}
		}
		if count < 20 || count > 30 {
			return nil, &FixedStarterCorpusError{Reason: "each topic bucket must contain between 20 and 30 starters"}
		}
	}
	return starters, nil
}

// RetrieveFixedStartersSources 跑独立 I3 排序，再校验 ID 并精确回填语料文本。
// Python retrieve_fixed_starters（topic_sources.py:737-757）；与 topic_retrieval.py:1517
// 同名异参（Go 侧以 Sources 后缀消歧），步骤顺序逐条对应
func RetrieveFixedStartersSources(ctx context.Context, rankedInterests []CorpusRankedInterest,
	corpusVersion string, adapter FixedStarterRankingAdapter, topK int) ([]StarterHit, error) {
	// 步骤 1：top_k 校验（:741-744）
	if !plainInt(topK) || topK < 1 || topK > RetrievalTopK {
		return nil, &FixedStarterRetrievalError{Reason: fmt.Sprintf(
			"top_k must be between 1 and %d", RetrievalTopK)}
	}
	// 步骤 2：兴趣校验 + 语料加载（:745-750）
	validated, err := validateRankedInterests(rankedInterests)
	if err != nil {
		return nil, err
	}
	corpus, err := LoadFixedStarterCorpus(corpusVersion)
	if err != nil {
		return nil, err
	}
	request := FixedStarterRankingRequest{
		RankedInterests: validated,
		Corpus:          corpus,
		CorpusVersion:   corpusVersion,
		TopK:            topK,
	}
	// 步骤 3：调用适配器（:751-752）
	response, err := adapter.RankFixedStarters(ctx, request)
	if err != nil {
		return nil, err
	}
	// 步骤 4：命中校验与物化（:753）
	return validateAndMaterializeHits(response, request)
}

// GroundCandidate 摆一个冻结候选，不允许检索改变其计划
// （Python ground_candidate :410-418；内部转发 _ground_candidate :227-408）
func GroundCandidate(ctx context.Context, candidate map[string]any,
	search SearchFn, fetch FetchFn, classifySource SourceClassifier, now float64) map[string]any {
	return groundCandidate(ctx, candidate, search, fetch, classifySource, now,
		map[string]*fetchTask{})
}

// GroundCandidates 批量摆候选：共享一个 URL 抓取缓存，不做跨候选补位
// （Python ground_candidates :421-413；asyncio.gather → errgroup 并发）
func GroundCandidates(ctx context.Context, candidates []map[string]any,
	search SearchFn, fetch FetchFn, classifySource SourceClassifier, now float64) []map[string]any {
	fetchCache := map[string]*fetchTask{}
	var mu sync.Mutex // Python 单线程事件循环下 dict 并发安全；Go 需互斥（已知偏差 §5）
	results := make([]map[string]any, len(candidates))
	var wg sync.WaitGroup
	for i, candidate := range candidates {
		wg.Add(1)
		go func(idx int, c map[string]any) {
			defer wg.Done()
			mu.Lock()
			results[idx] = groundCandidate(ctx, c, search, fetch, classifySource, now, fetchCache)
			mu.Unlock()
		}(i, candidate)
	}
	wg.Wait()
	out := []map[string]any{}
	for _, result := range results {
		if result != nil {
			out = append(out, result)
		}
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fetchTask 共享抓取任务（对齐 Python fetch_cache: dict[str, asyncio.Task]）
type fetchTask struct {
	done chan struct{}
	page map[string]any
	err  error
}

// awaitFetch 等待抓取任务完成（每 URL 单飞，后来者复用结果）
func awaitFetch(task *fetchTask) (map[string]any, error) {
	<-task.done
	return task.page, task.err
}

// parsePublishedAt 对齐 Python _parse_published_at（:102-113）
func parsePublishedAt(value any) (float64, bool) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return 0, false
	}
	normalized := strings.TrimSpace(text)
	normalized = strings.Replace(normalized, "Z", "+00:00", 1)
	layouts := []string{
		time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05",
		"2006-01-02", "2006/01/02 15:04:05", "2006/01/02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			return float64(parsed.Unix()), true
		}
	}
	return 0, false
}

// dateIsFresh 对齐 Python _date_is_fresh（:116-128）
func dateIsFresh(page map[string]any, freshnessClass string, now float64) bool {
	if freshnessClass == "evergreen_corpus" {
		return true
	}
	publishedAt, ok := parsePublishedAt(page["published_at"])
	if !ok || strings.TrimSpace(pyStrOr(page["published_at_source"], "")) == "" {
		return false
	}
	age := now - publishedAt
	if age < 0 {
		return false
	}
	limitDays := 90.0
	if freshnessClass == "current_event" {
		limitDays = 7
	}
	return age <= limitDays*24*60*60
}

// lexicallyAdmitted 对齐 Python _lexically_admitted（:131-142）
func lexicallyAdmitted(candidate map[string]any, content string) bool {
	identity := pyStrOr(candidate["identity_anchor"], "")
	contextAnchors := []string{}
	for _, anchor := range anySliceCopy(candidate["context_anchors"]) {
		if s, ok := anchor.(string); ok && s != "" {
			contextAnchors = append(contextAnchors, s)
		}
	}
	if identity == "" {
		return true
	}
	if !strings.Contains(content, identity) {
		return false
	}
	if len(contextAnchors) == 0 {
		return true
	}
	for _, anchor := range contextAnchors {
		if strings.Contains(content, anchor) {
			return true
		}
	}
	return false
}

// admittedSourceSet 对齐 Python _admitted_source_set（:145-176）
func admittedSourceSet(sources []map[string]any, freshnessClass string) []map[string]any {
	if freshnessClass == "evergreen_corpus" {
		out := []map[string]any{}
		for _, source := range sources {
			kind := pyStr(source["source_kind"])
			if kind == "authoritative" || kind == "stable" {
				out = append(out, source)
				break // [:1]
			}
		}
		return out
	}
	primary := []map[string]any{}
	for _, source := range sources {
		kind := pyStr(source["source_kind"])
		if kind == "authoritative" || kind == "original" {
			primary = append(primary, source)
		}
	}
	if len(primary) > 0 {
		return primary[:1]
	}
	reliable := []map[string]any{}
	publishers := map[string]struct{}{}
	leadingTexts := map[string]struct{}{}
	for _, source := range sources {
		if pyStr(source["source_kind"]) != "reliable_report" {
			continue
		}
		publisher := pyStr(source["publisher"])
		boundary, _ := source["content_boundary"].(map[string]any)
		leadingText := ""
		if boundary != nil {
			leadingText = truncateRunes(pyStr(boundary["text"]), 200)
		}
		if publisher == "" {
			continue
		}
		if _, dup := publishers[publisher]; dup {
			continue
		}
		if _, dup := leadingTexts[leadingText]; dup {
			continue
		}
		publishers[publisher] = struct{}{}
		leadingTexts[leadingText] = struct{}{}
		reliable = append(reliable, source)
		if len(reliable) == 2 {
			return reliable
		}
	}
	return []map[string]any{}
}

// sourceRecord 对齐 Python _source_record（:179-203）
func sourceRecord(page map[string]any, assessment SourceAssessment, searchEngine string, now float64) map[string]any {
	content := strings.TrimSpace(pyStrOr(page["content"], ""))
	boundaryText := truncateRunes(content, EvidenceTextLimit)
	record := map[string]any{
		"source_url":    pyStrOr(page["url"], ""),
		"source_kind":   assessment.Kind,
		"publisher":     assessment.Publisher,
		"search_engine": searchEngine,
		"fetch_reader":  pyStrOr(page["fetch_reader"], "direct"),
		"retrieved_at":  time.Unix(int64(now), 0).UTC().Format("2006-01-02T15:04:05.000000+00:00"),
		"content_boundary": map[string]any{
			"start": 0,
			"end":   len([]rune(boundaryText)),
			"text":  boundaryText,
		},
	}
	if publishedAt, ok := page["published_at"].(string); ok && strings.TrimSpace(publishedAt) != "" {
		record["published_at"] = publishedAt
		record["published_at_source"] = pyStrOr(page["published_at_source"], "")
	}
	return record
}

// groundCandidate 对齐 Python _ground_candidate（:227-408，步骤顺序逐条对应）
func groundCandidate(ctx context.Context, candidate map[string]any,
	search SearchFn, fetch FetchFn, classifySource SourceClassifier, now float64,
	fetchCache map[string]*fetchTask) map[string]any {
	// 步骤 1：保鲜等级白名单（:231-238）
	freshnessClass := pyStrOr(candidate["freshness_class"], "")
	if _, known := sourcesFreshnessClasses[freshnessClass]; !known {
		return nil
	}
	// 步骤 2：主查询必填（:239-244）
	primaryQuery := strings.TrimSpace(pyStrOr(candidate["primary_query"], ""))
	fallbackQuery := strings.TrimSpace(pyStrOr(candidate["fallback_query"], ""))
	if primaryQuery == "" {
		return nil
	}

	// 步骤 3：双查询循环（:246-393）
	admitted := []map[string]any{}
	searchResults := []any{}
	seenURLs := map[string]struct{}{}
	fetchCount := 0
	selected := []map[string]any{}
	queries := []struct {
		kind  string
		query string
	}{{"primary", primaryQuery}, {"fallback", fallbackQuery}}
	for _, q := range queries {
		if q.query == "" || (q.kind == "fallback" && len(selected) > 0) {
			continue
		}
		searchEngine := ""
		var rows []map[string]string
		if search != nil {
			engine, resultRows, err := search(ctx, q.query, MaxSearchResults, SearchTimeoutSeconds)
			if err != nil {
				rows = []map[string]string{}
				searchEngine = ""
			} else {
				rows = resultRows
				searchEngine = engine
			}
		} else {
			rows = []map[string]string{}
		}
		if len(rows) > MaxSearchResults {
			rows = rows[:MaxSearchResults]
		}
		for _, row := range rows {
			searchResults = append(searchResults, map[string]any{
				"query_kind": q.kind,
				"title":      row["title"],
				"url":        row["url"],
				"snippet":    row["snippet"],
			})
		}
		for _, row := range rows {
			if fetchCount >= MaxFetchesPerCandidate {
				break
			}
			targetURL := strings.TrimSpace(row["url"])
			parsed, err := url.Parse(targetURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				continue
			}
			if parsed.Host == "" {
				continue
			}
			if _, dup := seenURLs[targetURL]; dup {
				continue
			}
			seenURLs[targetURL] = struct{}{}
			fetchCount++
			// 共享缓存：同 URL 单飞任务，后来者等待既有任务
			var page map[string]any
			var fetchErr error
			if fetch != nil {
				if cached, ok := fetchCache[targetURL]; ok {
					page, fetchErr = awaitFetch(cached)
				} else {
					task := &fetchTask{done: make(chan struct{})}
					fetchCache[targetURL] = task
					page, fetchErr = fetch(ctx, targetURL, FetchTimeoutSeconds)
					task.page, task.err = page, fetchErr
					close(task.done)
				}
			} else {
				fetchErr = fmt.Errorf("no fetch wiring")
			}
			if fetchErr != nil {
				continue
			}
			statusCode := int(toFloatOr(page["status_code"], 0))
			content := strings.TrimSpace(pyStrOr(page["content"], ""))
			if statusCode < 200 || statusCode >= 300 || content == "" {
				continue
			}
			if !dateIsFresh(page, freshnessClass, now) {
				continue
			}
			if !lexicallyAdmitted(candidate, content) {
				continue
			}
			finalURL := pyStrOr(page["url"], targetURL)
			assessment := classifySource(finalURL, content)
			if assessment.Publisher == "" || assessment.Kind == "unknown" {
				continue
			}
			admitted = append(admitted, sourceRecord(page, assessment, searchEngine, now))
		}
		selected = admittedSourceSet(admitted, freshnessClass)
		if fetchCount >= MaxFetchesPerCandidate {
			break
		}
	}

	// 步骤 4：无准入来源 → 候选落空（:395-397）
	if len(selected) == 0 {
		return nil
	}

	// 步骤 5：证据束组装（:399-408）
	primary := selected[0]
	bundle := map[string]any{
		"candidate_id":                 pyStrOr(candidate["candidate_id"], ""),
		"fetch_status":                 "success",
		"source_url":                   primary["source_url"],
		"source_kind":                  primary["source_kind"],
		"search_engine":                primary["search_engine"],
		"fetch_reader":                 primary["fetch_reader"],
		"retrieved_at":                 primary["retrieved_at"],
		"retrieval_recency_capability": "post_filter_only",
		"retrieval_config_version":     RetrievalConfigVersion,
		"content_boundary":             primary["content_boundary"],
		"search_results":               searchResults,
		"sources":                      mapsToAny(selected),
	}
	if _, ok := primary["published_at"]; ok {
		bundle["published_at"] = primary["published_at"]
		bundle["published_at_source"] = primary["published_at_source"]
	}
	merged := map[string]any{}
	for key, value := range candidate {
		merged[key] = value
	}
	merged["evidence_bundle"] = bundle
	return merged
}

// openCorpus 语料资源解析（Python _open_corpus :541-544 的 Go 资源目录等价）：
// UAPCLAW_RESOURCES_DIR 环境变量优先，其次沿目录层级向上查找 resources/ 锚点
// （31a 资源目录先例）。
func openCorpus() (*os.File, error) {
	candidates := []string{}
	if env := os.Getenv("UAPCLAW_RESOURCES_DIR"); env != "" {
		candidates = append(candidates, filepath.Join(env, CorpusResource))
	}
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 8; i++ {
			candidates = append(candidates, filepath.Join(dir, CorpusResource))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, candidate := range candidates {
		if handle, err := os.Open(candidate); err == nil {
			return handle, nil
		}
	}
	return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf("corpus resource not found: %s", CorpusResource)}
}

// requiredString 对齐 Python _required_string（:546-553）
func requiredString(row map[string]any, field string, lineNumber int) (string, error) {
	value, ok := row[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", &FixedStarterCorpusError{Reason: fmt.Sprintf(
			"line %d: %s must be a non-empty string", lineNumber, field)}
	}
	return value, nil
}

// parseStarter 对齐 Python _parse_starter（:556-581）
func parseStarter(row any, lineNumber int) (*FixedStarter, error) {
	rowMap, ok := row.(map[string]any)
	if !ok || !exactKeys(rowMap, mapKeysOfSet(corpusFields)) {
		return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
			"line %d: starter schema does not match corpus v1", lineNumber)}
	}
	starterID, err := requiredString(rowMap, "starter_id", lineNumber)
	if err != nil {
		return nil, err
	}
	text, err := requiredString(rowMap, "text", lineNumber)
	if err != nil {
		return nil, err
	}
	topic, err := requiredString(rowMap, "topic", lineNumber)
	if err != nil {
		return nil, err
	}
	hookType, err := requiredString(rowMap, "hook_type", lineNumber)
	if err != nil {
		return nil, err
	}
	freshnessClass, err := requiredString(rowMap, "freshness_class", lineNumber)
	if err != nil {
		return nil, err
	}
	createdAt, err := requiredString(rowMap, "created_at", lineNumber)
	if err != nil {
		return nil, err
	}
	corpusVersion, err := requiredString(rowMap, "corpus_version", lineNumber)
	if err != nil {
		return nil, err
	}
	origin, err := requiredString(rowMap, "origin", lineNumber)
	if err != nil {
		return nil, err
	}
	starter := &FixedStarter{
		StarterID: starterID, Text: text, Topic: topic, HookType: hookType,
		FreshnessClass: freshnessClass, CreatedAt: createdAt,
		ExpiresAt: rowMap["expires_at"], CorpusVersion: corpusVersion, Origin: origin,
	}
	if _, known := topicBuckets[starter.Topic]; !known {
		return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
			"line %d: unsupported topic bucket %q", lineNumber, starter.Topic)}
	}
	if _, known := hookTypes[starter.HookType]; !known {
		return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
			"line %d: unsupported hook_type %q", lineNumber, starter.HookType)}
	}
	if _, known := sourcesFreshnessClasses[starter.FreshnessClass]; !known {
		return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
			"line %d: unsupported freshness_class %q", lineNumber, starter.FreshnessClass)}
	}
	if starter.ExpiresAt != nil {
		if _, ok := starter.ExpiresAt.(string); !ok {
			return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
				"line %d: expires_at must be a string or null", lineNumber)}
		}
	}
	if starter.Origin != "project_authored" {
		return nil, &FixedStarterCorpusError{Reason: fmt.Sprintf(
			"line %d: origin must be 'project_authored'", lineNumber)}
	}
	return starter, nil
}

// validateRankedInterests 对齐 Python _validate_ranked_interests（:628-641）
func validateRankedInterests(rankedInterests []CorpusRankedInterest) ([]CorpusRankedInterest, error) {
	frozen := append([]CorpusRankedInterest{}, rankedInterests...)
	seen := map[string]struct{}{}
	for _, interest := range frozen {
		if interest.InterestSummaryID == "" {
			return nil, &FixedStarterRetrievalError{Reason: "interest_summary_id must be non-empty"}
		}
		if _, dup := seen[interest.InterestSummaryID]; dup {
			return nil, &FixedStarterRetrievalError{Reason: "interest_summary_id must be unique"}
		}
		seen[interest.InterestSummaryID] = struct{}{}
		if len(interest.SourceLocators) == 0 {
			return nil, &FixedStarterRetrievalError{Reason: "ranked interests must retain source locators"}
		}
	}
	return frozen, nil
}

// validateAndMaterializeHits 对齐 Python _validate_and_materialize_hits（:644-734）
func validateAndMaterializeHits(response any, request FixedStarterRankingRequest) ([]StarterHit, error) {
	resp, ok := response.(map[string]any)
	if !ok || !exactKeys(resp, []string{"hits"}) {
		return nil, &FixedStarterRetrievalError{Reason: "response must contain only hits"}
	}
	rawHits, ok := resp["hits"].([]any)
	if !ok {
		return nil, &FixedStarterRetrievalError{Reason: "hits must be a list"}
	}
	if len(rawHits) > request.TopK {
		return nil, &FixedStarterRetrievalError{Reason: "response exceeds requested top_k"}
	}

	corpusByID := map[string]FixedStarter{}
	for _, starter := range request.Corpus {
		corpusByID[starter.StarterID] = starter
	}
	interestsByID := map[string]CorpusRankedInterest{}
	for _, interest := range request.RankedInterests {
		interestsByID[interest.InterestSummaryID] = interest
	}
	seenStarterIDs := map[string]struct{}{}
	materialized := []StarterHit{}
	for rank, rawHit := range rawHits {
		hitMap, ok := rawHit.(map[string]any)
		if !ok || !exactKeys(hitMap, []string{"starter_id", "interest_summary_id"}) {
			return nil, &FixedStarterRetrievalError{Reason: "each hit must contain only starter_id and interest_summary_id"}
		}
		starterID, ok := hitMap["starter_id"].(string)
		if !ok || starterID == "" {
			return nil, &FixedStarterRetrievalError{Reason: "starter_id must be non-empty"}
		}
		if _, dup := seenStarterIDs[starterID]; dup {
			return nil, &FixedStarterRetrievalError{Reason: "starter_id must not repeat"}
		}
		seenStarterIDs[starterID] = struct{}{}
		starter, known := corpusByID[starterID]
		if !known {
			return nil, &FixedStarterRetrievalError{Reason: "starter_id is not in the requested corpus version"}
		}

		interestSummaryID := hitMap["interest_summary_id"]
		matchedAnchor := any(nil)
		sourceLocators := []SourceLocator{}
		if interestSummaryID == nil {
			if len(request.RankedInterests) > 0 {
				return nil, &FixedStarterRetrievalError{Reason: "warm request hit must reference a ranked interest"}
			}
		} else {
			interestID, ok := interestSummaryID.(string)
			if !ok {
				return nil, &FixedStarterRetrievalError{Reason: "interest_summary_id must be a string or null"}
			}
			interest, known := interestsByID[interestID]
			if !known {
				return nil, &FixedStarterRetrievalError{Reason: "interest_summary_id is not in the ranked input"}
			}
			for _, anchor := range interest.AllowedAnchors {
				if anchor != "" && strings.Contains(starter.Text, anchor) {
					matchedAnchor = anchor
					break
				}
			}
			if matchedAnchor == nil {
				return nil, &FixedStarterRetrievalError{Reason: "warm hit does not contain an allowed interest anchor"}
			}
			sourceLocators = interest.SourceLocators
		}
		materialized = append(materialized, StarterHit{
			StarterID:         starter.StarterID,
			InterestSummaryID: interestSummaryID,
			Rank:              rank + 1,
			Text:              starter.Text,
			Topic:             starter.Topic,
			HookType:          starter.HookType,
			FreshnessClass:    starter.FreshnessClass,
			CreatedAt:         starter.CreatedAt,
			ExpiresAt:         starter.ExpiresAt,
			CorpusVersion:     starter.CorpusVersion,
			Origin:            starter.Origin,
			MatchedAnchor:     matchedAnchor,
			SourceLocators:    sourceLocators,
		})
	}
	return materialized, nil
}

// sortStringsCopy 供内部排序场景使用的稳定拷贝排序（Python sorted 语义）
func sortStringsCopy(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}
