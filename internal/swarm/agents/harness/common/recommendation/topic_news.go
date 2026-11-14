package recommendation

// topic_news.go 新闻 lane 来源：Bing News RSS 与公共热搜榜。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_news.py
// （494 行，一比一移植；aiohttp/ElementTree → net/http/encoding+xml 等价转换）。
// 既有切片：HotListEntry/HotListPull/HotEventKey/LoadHotListPull/NewsCardEvent 与
// NEWS_* 常量已随校验族切片落在 topic_product_helpers.go；
// NormalizeNewsQuery 已落 topic_pipeline_b.go（⤴️31c 归位）。

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// NewsPreferredWindowHours 首选时间窗小时数（topic_news.py:40）。
	NewsPreferredWindowHours = 24
	feedURL = "https://www.bing.com/news/search"
	// userAgent 抓取 UA（:43-45）。
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/128.0 Safari/537.36"
	// HotListPullReceiptVersion 热榜拉取小票版本（:235）。
	HotListPullReceiptVersion = "topic-starter-v6-hot-list-pull-v1"
	// HotListReuseSeconds 复用窗口 30 分钟（:237）。
	HotListReuseSeconds = 30 * 60
	// HotListTimeoutSeconds 一次拉取超时（:242）。
	HotListTimeoutSeconds = 5
	// WeiboProviderID 微博热榜提供方（:243）。
	WeiboProviderID = "weibo-hot-band"
	// WeiboHotBandURL 微博热榜地址（:244）。
	WeiboHotBandURL = "https://weibo.com/ajax/statuses/hot_band"
	// BaiduProviderID 百度热搜提供方（:245）。
	BaiduProviderID = "baidu-realtime"
	// BaiduRealtimeURL 百度热搜地址（:246）。
	BaiduRealtimeURL = "https://top.baidu.com/board?tab=realtime"
	// maxFeedRedirects 重定向逐跳复核上限（:130）。
	maxFeedRedirects = 5
)

// HotListFetcher 热榜抓取执行器（:250）。
type HotListFetcher = func(urlText string, timeoutSeconds int, headers map[string]string) (string, error)

// FeedFetcher Feed 抓取执行器（:47）。
type FeedFetcher = func(urlText string, timeoutSeconds int) (string, error)

// ──────────────────────────── 全局变量 ────────────────────────────

// baiduDataRE 百度页面嵌入数据（:247）。
var baiduDataRE = regexp.MustCompile(`(?s)<!--s-data:(.*?)-->`)

// ──────────────────────────── 导出函数 ────────────────────────────

// FeedURL Bing News RSS 请求地址（对齐 :58-66 feed_url；interval 映射：24h=7、168h=8）。
func FeedURL(query string, windowHours int) string {
	interval := "8"
	for _, bound := range []struct {
		hours int
		code  string
	}{{24, "7"}, {168, "8"}} {
		if windowHours <= bound.hours {
			interval = bound.code
			break
		}
	}
	return fmt.Sprintf("%s?q=%s&format=rss&qft=sortbydate%%3D%%221%%22+interval%%3D%%22%s%%22",
		feedURL, url.QueryEscape(query), interval)
}


// ParseNewsFeed RSS 解析为搜索行：url、title、date（ISO）、publisher、snippet
// （对齐 :82-112 parse_news_feed）。
func ParseNewsFeed(xmlText string) []map[string]string {
	var root feedEnvelope
	if err := xml.Unmarshal([]byte(xmlText), &root); err != nil {
		return nil
	}
	rows := make([]map[string]string, 0)
	seen := map[string]bool{}
	for _, item := range root.Channel.Items {
		fields := map[string]string{}
		for _, child := range item.Children {
			key := child.tagLocalName()
			fields[key] = strings.TrimSpace(child.Text)
		}
		rowURL := articleURL(fields["link"])
		parsed, err := url.Parse(rowURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			continue
		}
		if seen[rowURL] {
			continue
		}
		published, err := time.Parse(time.RFC1123Z, fields["pubdate"])
		if err != nil {
			published, err = time.Parse(time.RFC1123, fields["pubdate"])
		}
		if err != nil {
			continue
		}
		seen[rowURL] = true
		rows = append(rows, map[string]string{
			"url":       rowURL,
			"title":     fields["title"],
			"date":      published.UTC().Format("2006-01-02T15:04:05Z07:00"),
			"publisher": fields["source"],
			"snippet":   fields["description"],
		})
	}
	return rows
}

// RowsWithinWindow 时间窗过滤：[now-window_hours*3600, now+3600]
// （对齐 :114-127 rows_within_window）。
func RowsWithinWindow(rows []map[string]string, now float64, windowHours int) []map[string]string {
	floor := now - float64(windowHours)*3600
	kept := make([]map[string]string, 0)
	for _, row := range rows {
		published, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", row["date"])
		if err != nil {
			continue
		}
		ts := float64(published.Unix())
		if floor <= ts && ts <= now+3600 {
			kept = append(kept, row)
		}
	}
	return kept
}

// FetchFeedText GET 一个公共列表或 feed 为文本：不带 cookie，仅浏览器 UA 与给定头；
// 重定向逐跳按公网规则复核（对齐 :132-156 fetch_feed_text）。
func FetchFeedText(urlText string, timeoutSeconds int, headers map[string]string) (string, error) {
	target := urlText
	for i := 0; i <= maxFeedRedirects; i++ {
		if reason := PublicFetchURLRejectionReason(context.Background(), target); reason != "" {
			return "", fmt.Errorf("unsafe fetch target: %s", reason)
		}
		resp, err := feedHTTPGet(target, timeoutSeconds, headers)
		if err != nil {
			return "", err
		}
		if resp.statusCode >= 300 && resp.statusCode < 400 && resp.location != "" {
			base, _ := url.Parse(target)
			ref, _ := url.Parse(resp.location)
			if base != nil && ref != nil {
				target = base.ResolveReference(ref).String()
			} else {
				target = resp.location
			}
			_ = resp.body.Close()
			continue
		}
		if resp.statusCode < 200 || resp.statusCode >= 300 {
			_ = resp.body.Close()
			return "", fmt.Errorf("news feed http error: status %d", resp.statusCode)
		}
		text := resp.text
		_ = resp.body.Close()
		return text, nil
	}
	return "", fmt.Errorf("news feed too many redirects: %s", urlText)
}

// ParseWeiboHotBand 微博热榜词条：广告无位置、置顶词单列（对齐 :318-350）。
func ParseWeiboHotBand(text string) ([]*HotListEntry, error) {
	var envelope struct {
		Data struct {
			BandList []map[string]any `json:"band_list"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return nil, errUnreadableList()
	}
	band := envelope.Data.BandList
	if band == nil {
		return nil, errUnreadableList()
	}
	entries := make([]*HotListEntry, 0)
	for _, item := range band {
		if isTruthy(item["is_ad"]) {
			continue
		}
		word := strings.TrimSpace(stringValue(item["word"]))
		rank, ok := newsPlainInt(item["realpos"])
		if word == "" || !ok {
			continue
		}
		listedAt := anyFloat(item["onboard_time"])
		entries = append(entries, &HotListEntry{
			EntryID:  fmt.Sprintf("%s:%d", WeiboProviderID, rank),
			ProviderID: WeiboProviderID,
			Rank:     rank,
			Word:     word,
			ListedAt: listedAt,
		})
	}
	return entries, nil
}

// ParseBaiduRealtime 百度热搜：从页面嵌入数据读编号词条与摘要（对齐 :352-382）。
func ParseBaiduRealtime(htmlText string) ([]*HotListEntry, error) {
	match := baiduDataRE.FindStringSubmatch(htmlText)
	if match == nil {
		return nil, errUnreadableList()
	}
	var envelope struct {
		Data struct {
			Cards []struct {
				Content []map[string]any `json:"content"`
			} `json:"cards"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(match[1]), &envelope); err != nil {
		return nil, errUnreadableList()
	}
	var content []map[string]any
	if len(envelope.Data.Cards) > 0 {
		content = envelope.Data.Cards[0].Content
	}
	if content == nil {
		return nil, errUnreadableList()
	}
	entries := make([]*HotListEntry, 0)
	for _, item := range content {
		if isTruthy(item["isTop"]) {
			continue
		}
		word := strings.TrimSpace(stringValue(item["word"]))
		index, ok := newsPlainInt(item["index"])
		if word == "" || !ok {
			continue
		}
		summary := strings.TrimSpace(stringValue(item["desc"]))
		entry := &HotListEntry{
			EntryID:    fmt.Sprintf("%s:%d", BaiduProviderID, index+1),
			ProviderID: BaiduProviderID,
			Rank:       index + 1,
			Word:       word,
		}
		if summary != "" {
			entry.Summary = summary
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// hotListSources 热榜来源表（对齐 :384-389 _SOURCES）。
var hotListSources = []struct {
	providerID string
	listURL    string
	headers    map[string]string
	parse      func(string) ([]*HotListEntry, error)
}{
	{WeiboProviderID, WeiboHotBandURL, map[string]string{"Referer": "https://weibo.com/"}, ParseWeiboHotBand},
	{BaiduProviderID, BaiduRealtimeURL, map[string]string{}, ParseBaiduRealtime},
}

// ──────────────────────────── 结构体 ────────────────────────────

// BingNewsFeed Bing News RSS 的搜索厂商形态门面（对齐 :125-207）。
type BingNewsFeed struct {
	// ProviderId 厂商 ID
	ProviderId string

	feedFetcher FeedFetcher
	now         func() float64
	windowHours int
}

// NewBingNewsFeed 构造（对齐 __init__：默认 fetcher=FetchFeedText）。
func NewBingNewsFeed(feedFetcher FeedFetcher, now func() float64, windowHours int) *BingNewsFeed {
	if feedFetcher == nil {
		feedFetcher = func(urlText string, timeoutSeconds int) (string, error) {
			return FetchFeedText(urlText, timeoutSeconds, nil)
		}
	}
	if windowHours <= 0 {
		windowHours = int(NewsMaxWindowHours)
	}
	return &BingNewsFeed{ProviderId: NewsFeedProviderID, feedFetcher: feedFetcher, now: now, windowHours: windowHours}
}

// ListItems 有界、零模型、按窗口过滤的列表（对齐 :141-148 list_items）。
func (f *BingNewsFeed) ListItems(query string, maxResults, timeoutSeconds int) ([]map[string]string, error) {
	text, err := f.feedFetcher(FeedURL(query, f.windowHours), timeoutSeconds)
	if err != nil {
		return nil, err
	}
	rows := ParseNewsFeed(text)
	now := f.now()
	if now == 0 {
		now = float64(time.Now().Unix())
	}
	filtered := RowsWithinWindow(rows, now, f.windowHours)
	if len(filtered) > maxResults {
		filtered = filtered[:maxResults]
	}
	return filtered, nil
}

// Call 搜索形态调用（对齐 :173-207 __call__）。
func (f *BingNewsFeed) Call(ctx context.Context, query string, maxResults, timeoutSeconds int) (*OpenJiuwenNativeSearchResult, error) {
	hostRequestID := fmt.Sprintf("news-feed-%s", newUUIDHex())
	if strings.TrimSpace(query) == "" || maxResults < 1 || maxResults > 20 || timeoutSeconds <= 0 {
		return nil, &OpenJiuwenNativeSearchContractError{
			Reason: "search_request_contract_failed", HostRequestID: hostRequestID}
	}
	rows, err := f.ListItems(query, maxResults, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("news feed request failed")
	}
	if len(rows) == 0 {
		return nil, &OpenJiuwenNativeSearchContractError{
			Reason: "news_feed_no_recent_items", HostRequestID: hostRequestID}
	}
	urls := make([]string, 0, len(rows))
	for _, row := range rows {
		urls = append(urls, row["url"])
	}
	receipt := map[string]any{
		"receipt_version": NewsFeedReceiptVersion,
		"provider_id":     NewsFeedProviderID,
		"host_request_id": hostRequestID,
		"requested_query": query,
		"engine":          NewsFeedEngine,
		"result_urls":     urls,
		"result_count":    len(rows),
	}
	return &OpenJiuwenNativeSearchResult{Engine: NewsFeedEngine, Rows: rows, Receipt: receipt}, nil
}

// HotListFeed 拉取当前热榜并复用 30 分钟内的拉取（对齐 :391-460；
// 实现 31b 的 HotListFeedInterface 消费缝，⤴️31c 归位即真实类型）。
type HotListFeed struct {
	store   *TopicStarterStore
	fetcher HotListFetcher
	now     func() float64
}

// NewHotListFeed 构造（对齐 __init__：默认 fetcher=FetchFeedText）。
func NewHotListFeed(store *TopicStarterStore, fetcher HotListFetcher, now func() float64) *HotListFeed {
	if fetcher == nil {
		fetcher = func(urlText string, timeoutSeconds int, headers map[string]string) (string, error) {
			return FetchFeedText(urlText, timeoutSeconds, headers)
		}
	}
	if now == nil {
		now = func() float64 { return float64(time.Now().Unix()) }
	}
	return &HotListFeed{store: store, fetcher: fetcher, now: now}
}

// CachedPull 复用窗口内的最近成功拉取（对齐 :402-405 cached_pull）。
func (f *HotListFeed) CachedPull() *HotListPull {
	return f.recentPull(f.now())
}

// Pull 最新热榜；全部来源皆空时返回 nil（对齐 :407-449 pull）。
func (f *HotListFeed) Pull(ctx context.Context) *HotListPull {
	now := f.now()
	if recent := f.recentPull(now); recent != nil {
		return recent
	}
	for _, source := range hotListSources {
		receiptID := fmt.Sprintf("hot-list-pull-%s", newUUIDHex())
		started := time.Now()
		var entries []*HotListEntry
		var failure string
		text, fetchErr := f.fetcher(source.listURL, HotListTimeoutSeconds, source.headers)
		switch {
		case fetchErr != nil && isTimeoutErr(fetchErr):
			failure = "timeout"
		case fetchErr != nil && isHTTPStatusErr(fetchErr):
			failure = "http_error"
		case fetchErr != nil:
			failure = "request_failed"
		default:
			parsed, parseErr := source.parse(text)
			if parseErr != nil {
				failure = "unreadable_list"
			} else {
				entries = parsed
			}
		}
		_ = ctx
		if failure == "" && len(entries) == 0 {
			failure = "empty_list"
		}
		f.record(hotListPullRecord{
			receiptID:  receiptID,
			providerID: source.providerID,
			listURL:    source.listURL,
			entries:    entries,
			failure:    failure,
			pulledAt:   now,
			latencyMS:  float64(time.Since(started).Milliseconds()),
		})
		if failure == "" {
			return &HotListPull{
				ReceiptID:  receiptID,
				ProviderID: source.providerID,
				ListURL:    source.listURL,
				PulledAt:   now,
				Entries:    entries,
			}
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// feedEnvelope/feedItem RSS 解析结构（等价 ElementTree ./channel/item）。
type feedEnvelope struct {
	XMLName xml.Name  `xml:"rss"`
	Channel feedChannel `xml:"channel"`
}

type feedChannel struct {
	Items []feedItem `xml:"item"`
}

type feedItem struct {
	Children []feedChild `xml:",any"`
}

type feedChild struct {
	XMLName xml.Name
	Text    string `xml:",chardata"`
}

// tagLocalName 去命名空间前缀（等价 child.tag.split("}")[-1].casefold()）。
func (c *feedChild) tagLocalName() string {
	name := c.XMLName.Local
	return strings.ToLower(name)
}

// articleURL bing 跳转壳解包（对齐 :72-80 _article_url）。
func articleURL(link string) string {
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err == nil && strings.HasSuffix(parsed.Path, "/news/apiclick.aspx") {
		values := parsed.Query()
		if target := strings.TrimSpace(values.Get("url")); target != "" {
			return target
		}
	}
	return strings.TrimSpace(link)
}

// errUnreadableList 榜单不可读（对齐 _UnreadableList）。
func errUnreadableList() error {
	return fmt.Errorf("unreadable hot list")
}

// isTruthy any 真值判定（等价 Python truthiness）。
func isTruthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	case map[string]any:
		return v != nil
	case []any:
		return v != nil
	default:
		return value != nil
	}
}

// plainInt 严格整数判定：bool 不是计数（对齐 :49-51 _plain_int）。
func newsPlainInt(value any) (int, bool) {
	if v, ok := value.(bool); ok {
		_ = v
		return 0, false
	}
	if v, ok := value.(float64); ok && v == float64(int(v)) {
		return int(v), true
	}
	return 0, false
}

// anyFloat 数字转 float（对齐 type(onboard) in (int, float)）。
func anyFloat(value any) any {
	if _, ok := value.(bool); ok {
		return nil
	}
	if v, ok := value.(float64); ok {
		return v
	}
	return nil
}

// isTimeoutErr 超时类错误。
func isTimeoutErr(err error) bool {
	return err == context.DeadlineExceeded ||
		strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "Client.Timeout")
}

// isHTTPStatusErr HTTP 状态类错误（等价 aiohttp.ClientResponseError）。
func isHTTPStatusErr(err error) bool {
	return strings.Contains(err.Error(), "http error: status")
}

// hotListPullRecord 记录参数（对齐 :421-447 _record）。
type hotListPullRecord struct {
	receiptID  string
	providerID string
	listURL    string
	entries    []*HotListEntry
	failure    string
	pulledAt   float64
	latencyMS  float64
}

// record 落一条热榜拉取事件（对齐 :421-447）。
func (f *HotListFeed) record(rec hotListPullRecord) {
	status := "success"
	if rec.failure != "" {
		status = "failure"
	}
	entryPayloads := make([]map[string]any, 0, len(rec.entries))
	for _, entry := range rec.entries {
		// 对齐 Python asdict：六键恒等（summary/listed_at 为 None 也要有键，
		// recordedPull 的 HotListEntry(**entry) 恰等契约）
		payload := map[string]any{
			"entry_id": entry.EntryID, "provider_id": entry.ProviderID,
			"rank": entry.Rank, "word": entry.Word,
			"summary": entry.Summary, "listed_at": entry.ListedAt,
		}
		entryPayloads = append(entryPayloads, payload)
	}
	_, _ = f.store.RecordEvent(map[string]any{
		"session_id":        "topic-starter-hot-list",
		"slate_id":          rec.providerID,
		"event_type":        HotListPullEventType,
		"topic_id":          rec.receiptID,
		"policy_id":         ProductPolicyID,
		"selection_mode":    "hot_list_pull",
		"propensity_reason": "not_applicable",
		"payload": map[string]any{
			"receipt_version": HotListPullReceiptVersion,
			"receipt_id":      rec.receiptID,
			"provider_id":     rec.providerID,
			"list_url":        rec.listURL,
			"status":          status,
			"failure_reason":  newsNilIfEmpty(rec.failure),
			"entry_count":     len(rec.entries),
			"latency_ms":      roundOne(rec.latencyMS),
			"pulled_at":       rec.pulledAt,
			"entries":         entryPayloads,
		},
		"created_at": rec.pulledAt,
	})
}

// recentPull 复用窗口内的最近成功拉取（对齐 :449-459 _recent_pull）。
func (f *HotListFeed) recentPull(now float64) *HotListPull {
	rows, err := f.store.QueryEvents(QueryEventsOptions{
		EventType: HotListPullEventType,
		PolicyID:  &[]string{ProductPolicyID}[0],
		Limit:     10,
		NewestFirst: true,
	})
	if err != nil {
		return nil
	}
	for _, row := range rows {
		payload, _ := row["payload"].(map[string]any)
		if payload == nil || payload["status"] != "success" {
			continue
		}
		pulledAt, _ := payload["pulled_at"].(float64)
		if now-pulledAt >= HotListReuseSeconds {
			return nil
		}
		pull, pullErr := recordedPull(payload)
		if pullErr != nil {
			return nil
		}
		return pull
	}
	return nil
}


// feedHTTPGet 底层 GET（等价 aiohttp session.get 的被使用面）。
func feedHTTPGet(urlText string, timeoutSeconds int, headers map[string]string) (*fetchedResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlText, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil && len(raw) == 0 {
		_ = resp.Body.Close()
		return nil, readErr
	}
	_ = resp.Body.Close()
	return &fetchedResponse{
		statusCode:  resp.StatusCode,
		finalURL:    resp.Request.URL.String(),
		contentType: resp.Header.Get("Content-Type"),
		text:        string(raw),
		location:    resp.Header.Get("Location"),
		body:        io.NopCloser(strings.NewReader("")),
	}, nil
}

// nilIfEmpty 空串转 nil（对齐 failure or None）。
func newsNilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// roundOne 保留一位小数（对齐 round(latency_ms, 1)）。
func roundOne(value float64) float64 {
	return float64(int(value*10+0.5)) / 10
}

