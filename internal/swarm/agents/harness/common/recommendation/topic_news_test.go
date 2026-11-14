package recommendation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// isoTime UNIX 秒 → ISO 字符串。
func isoTime(seconds float64) string {
	return time.Unix(int64(seconds), 0).UTC().Format("2006-01-02T15:04:05Z07:00")
}

// rfc1123 UNIX 秒 → RFC1123 GMT 字符串。
func rfc1123(seconds float64) string {
	return time.Unix(int64(seconds), 0).UTC().Format(time.RFC1123)
}

// contextBackground 测试上下文。
func contextBackground() context.Context { return context.Background() }

// TestFeedURL 窗口→interval 映射与 URL 形态（对齐 topic_news.py:58-66 feed_url）。
func TestFeedURL(t *testing.T) {
	got := FeedURL("AI 芯片", 24)
	wantPrefix := "https://www.bing.com/news/search?q="
	wantSuffix := "&format=rss&qft=sortbydate%3D%221%22+interval%3D%227%22"
	if !strings.HasPrefix(got, wantPrefix) || !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("24h: got=%q", got)
	}
	got = FeedURL("AI 芯片", 72)
	if !strings.HasSuffix(got, "interval%3D%228%22") {
		t.Fatalf("72h: got=%q", got)
	}
}

// TestSearchPageURL 阅读器打开形态（对齐 :68-70）。
func TestSearchPageURL(t *testing.T) {
	got := SearchPageURL("AI 芯片")
	if !strings.HasPrefix(got, "https://www.bing.com/news/search?q=") || strings.Contains(got, "format=") {
		t.Fatalf("got=%q", got)
	}
}

// TestArticleUrl bing 跳转壳解包（对齐 :72-80 _article_url）。
func TestArticleUrl(t *testing.T) {
	wrapped := "https://www.bing.com/news/apiclick.aspx?url=https%3A%2F%2Freal.example.com%2Fa"
	if got := articleURL(wrapped); got != "https://real.example.com/a" {
		t.Fatalf("got=%q", got)
	}
	if got := articleURL("https://real.example.com/a"); got != "https://real.example.com/a" {
		t.Fatalf("got=%q", got)
	}
}

// TestParseNewsFeed RSS 解析（对齐 :82-112 parse_news_feed）。
func TestParseNewsFeed(t *testing.T) {
	xmlText := `<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>标题一</title><link>https://www.bing.com/news/apiclick.aspx?url=https%3A%2F%2Fa.example.com%2F1</link>
<pubDate>Mon, 02 Nov 2026 10:00:00 GMT</pubDate><source>新浪</source><description>摘要一</description></item>
<item><title>标题二</title><link>https://a.example.com/2</link>
<pubDate>Tue, 03 Nov 2026 09:00:00 GMT</pubDate><source>网易</source><description>摘要二</description></item>
<item><title>重复跳过</title><link>https://a.example.com/2</link>
<pubDate>Tue, 03 Nov 2026 10:00:00 GMT</pubDate></item>
<item><title>坏日期跳过</title><link>https://a.example.com/3</link><pubDate>not-a-date</pubDate></item>
</channel></rss>`
	rows := ParseNewsFeed(xmlText)
	if len(rows) != 2 {
		t.Fatalf("rows=%d: %v", len(rows), rows)
	}
	first := rows[0]
	if first["url"] != "https://a.example.com/1" || first["title"] != "标题一" ||
		first["publisher"] != "新浪" || first["snippet"] != "摘要一" {
		t.Fatalf("first=%v", first)
	}
	// RFC1123 GMT → UTC ISO
	if !strings.HasPrefix(first["date"], "2026-11-02T10:00:00Z") {
		t.Fatalf("date=%q", first["date"])
	}
}

// TestRowsWithinWindow 时间窗过滤（对齐 :114-127 rows_within_window）。
func TestRowsWithinWindow(t *testing.T) {
	now := 1800000000.0
	rows := []map[string]string{
		{"date": isoTime(now - 3600)},
		{"date": isoTime(now - 10*3600)},
		{"date": isoTime(now + 1800)},
		{"date": isoTime(now + 2*3600)},
		{"date": "bad"},
	}
	kept := RowsWithinWindow(rows, now, 24)
	if len(kept) != 3 {
		t.Fatalf("kept=%d: %v", len(kept), kept)
	}
}

// TestParseWeiboHotBand 微博热榜解析（对齐 :318-350 parse_weibo_hot_band）。
func TestParseWeiboHotBand(t *testing.T) {
	text := `{"data":{"band_list":[
{"word":"词条一","realpos":1,"onboard_time":1800000000},
{"word":"广告位","realpos":2,"is_ad":true},
{"word":"","realpos":3},
{"word":"词条二","realpos":"not-int"}
]}}`
	entries, err := ParseWeiboHotBand(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Word != "词条一" || entries[0].Rank != 1 ||
		entries[0].EntryID != "weibo-hot-band:1" {
		t.Fatalf("entries=%v", entries)
	}
	if _, err := ParseWeiboHotBand("not-json"); err == nil {
		t.Fatal("坏 JSON 必须报 UnreadableList")
	}
}

// TestParseBaiduRealtime 百度热搜解析（对齐 :352-382 parse_baidu_realtime）。
func TestParseBaiduRealtime(t *testing.T) {
	data := `{"data":{"cards":[{"content":[
{"word":"词条一","index":0,"desc":"描述一"},
{"word":"置顶","index":1,"isTop":true},
{"word":"词条二","index":2}
]}]}}`
	htmlText := "<!--s-data:" + data + "-->"
	entries, err := ParseBaiduRealtime(htmlText)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Word != "词条一" || entries[0].Rank != 1 ||
		entries[0].Summary != "描述一" || entries[1].Rank != 3 {
		t.Fatalf("entries=%v", entries)
	}
	if _, err := ParseBaiduRealtime("<html>无数据</html>"); err == nil {
		t.Fatal("无嵌入数据必须报 UnreadableList")
	}
}

// TestBingNewsFeed_ListItems BingNewsFeed 门面（对齐 :141-171）。
func TestBingNewsFeed_ListItems(t *testing.T) {
	now := 1800000000.0
	rss := `<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>新</title><link>https://a.example.com/1</link>
<pubDate>` + rfc1123(now-1800) + `</pubDate></item>
<item><title>旧</title><link>https://a.example.com/2</link>
<pubDate>` + rfc1123(now-100*3600) + `</pubDate></item>
</channel></rss>`
	feed := NewBingNewsFeed(func(urlText string, timeoutSeconds int) (string, error) {
		if !strings.Contains(urlText, "format=rss") {
			t.Fatalf("feed url=%q", urlText)
		}
		return rss, nil
	}, func() float64 { return now }, 72)
	rows, err := feed.ListItems("AI 芯片", 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["title"] != "新" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestBingNewsFeed_Call 新闻搜索形态调用（对齐 :173-207 __call__）。
func TestBingNewsFeed_Call(t *testing.T) {
	now := 1800000000.0
	rss := `<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>新</title><link>https://a.example.com/1</link>
<pubDate>` + rfc1123(now-1800) + `</pubDate></item>
</channel></rss>`
	feed := NewBingNewsFeed(func(_ string, _ int) (string, error) { return rss, nil },
		func() float64 { return now }, 72)
	result, err := feed.Call(contextBackground(), "AI 芯片", 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Engine != NewsFeedEngine || len(result.Rows) != 1 {
		t.Fatalf("result=%+v", result)
	}
	receipt := result.Receipt
	for key, want := range map[string]any{
		"receipt_version": NewsFeedReceiptVersion,
		"provider_id":     NewsFeedProviderID,
		"engine":          NewsFeedEngine,
		"result_count":    1,
	} {
		if receipt[key] != want {
			t.Fatalf("receipt[%q]=%v want %v", key, receipt[key], want)
		}
	}
	// 空结果 → news_feed_no_recent_items
	emptyFeed := NewBingNewsFeed(func(_ string, _ int) (string, error) { return "<rss><channel></channel></rss>", nil },
		func() float64 { return now }, 72)
	_, err = emptyFeed.Call(contextBackground(), "AI 芯片", 5, 5)
	var contract *OpenJiuwenNativeSearchContractError
	if !errors.As(err, &contract) || contract.Reason != "news_feed_no_recent_items" {
		t.Fatalf("err=%v", err)
	}
	// 请求契约
	if _, err = feed.Call(contextBackground(), "", 5, 5); !errors.As(err, &contract) ||
		contract.Reason != "search_request_contract_failed" {
		t.Fatalf("err=%v", err)
	}
}

// TestHotListFeed_PullAndReuse 热榜拉取与 30 分钟复用（对齐 :384-460）。
func TestHotListFeed_PullAndReuse(t *testing.T) {
	store := newTestStore(t)
	now := 1800000000.0
	clock := now
	calls := 0
	fetcher := func(urlText string, timeoutSeconds int, headers map[string]string) (string, error) {
		calls++
		if strings.Contains(urlText, "weibo.com") {
			return `{"data":{"band_list":[{"word":"词条一","realpos":1}]}}`, nil
		}
		return "<html>无数据</html>", nil
	}
	feed := NewHotListFeed(store, fetcher, func() float64 { return clock })
	pull := feed.Pull(contextBackground())
	if pull == nil || pull.ProviderID != WeiboProviderID || len(pull.Entries) != 1 {
		t.Fatalf("pull=%+v", pull)
	}
	// 复用窗口内再拉：不新增抓取
	if got := feed.Pull(contextBackground()); got == nil || got.ReceiptID != pull.ReceiptID {
		t.Fatalf("reuse=%+v", got)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	// 超过 30 分钟重新拉取
	clock += HotListReuseSeconds + 1
	if got := feed.Pull(contextBackground()); got == nil || got.ReceiptID == pull.ReceiptID {
		t.Fatalf("expired reuse=%+v", got)
	}
}

// TestHotListFeed_全源皆空 全部来源失败返回 nil（对齐 :447-449）。
func TestHotListFeed_全源皆空(t *testing.T) {
	store := newTestStore(t)
	now := 1800000000.0
	feed := NewHotListFeed(store, func(string, int, map[string]string) (string, error) {
		return "<html>无数据</html>", nil
	}, func() float64 { return now })
	if pull := feed.Pull(contextBackground()); pull != nil {
		t.Fatalf("pull=%+v", pull)
	}
	// 失败必须留痕
	rows, err := store.QueryEvents(QueryEventsOptions{EventType: HotListPullEventType, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	failureCount := 0
	for _, row := range rows {
		payload, _ := row["payload"].(map[string]any)
		if payload["status"] == "failure" {
			failureCount++
		}
	}
	if failureCount != 2 {
		t.Fatalf("failureCount=%d", failureCount)
	}
}

// TestHotListFeed_CachedPull 复用窗口内读最近成功（对齐 :402-405）。
func TestHotListFeed_CachedPull(t *testing.T) {
	store := newTestStore(t)
	now := 1800000000.0
	clock := now
	feed := NewHotListFeed(store, func(string, int, map[string]string) (string, error) {
		return `{"data":{"band_list":[{"word":"词","realpos":1}]}}`, nil
	}, func() float64 { return clock })
	if feed.CachedPull() != nil {
		t.Fatal("空库不得有缓存")
	}
	pull := feed.Pull(contextBackground())
	if feed.CachedPull() == nil || feed.CachedPull().ReceiptID != pull.ReceiptID {
		t.Fatal("复用窗口内必须有缓存")
	}
	clock += HotListReuseSeconds + 1
	if feed.CachedPull() != nil {
		t.Fatal("超窗后缓存必须失效")
	}
}
