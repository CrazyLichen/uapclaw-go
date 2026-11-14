package recommendation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTopicFetchServer 固定响应的 httptest 辅助。
func newTopicFetchServer(t *testing.T, body, contentType string, status int) *httptest.Server {
	t.Helper()
	return newTopicFetchServerFunc(t, func(*httpRequestRecord) (int, string, string) {
		return status, contentType, body
	})
}

// httpRequestRecord 记录一次请求的 URL 与路径。
type httpRequestRecord struct {
	url  string
	path string
}

// newTopicFetchServerFunc 按请求回调响应的 httptest 辅助（同端口可应答多个 URL 形态）。
func newTopicFetchServerFunc(t *testing.T, handler func(*httpRequestRecord) (int, string, string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, contentType, body := handler(&httpRequestRecord{url: r.URL.String(), path: r.URL.Path})
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return srv
}

// TestExtractPublishedAt_meta标签 提取 og/article 元发布时间（对齐 Python
// topic_page_fetch.py:65-100 meta 分支）。
func TestExtractPublishedAt_meta标签(t *testing.T) {
	text := `<html><head><meta property="article:published_time" content="2026-11-01T08:00:00+00:00"></head></html>`
	got, source := ExtractPublishedAt(text, "https://example.com/a")
	if got != "2026-11-01T08:00:00+00:00" || source != "meta.article:published_time" {
		t.Fatalf("got=%q source=%q", got, source)
	}
}

// TestExtractPublishedAt_citationDate citation_date 需 %Y/%m/%d 格式
// （对齐 :101-119）。
func TestExtractPublishedAt_citationDate(t *testing.T) {
	got, source := ExtractPublishedAt(
		`<meta name="citation_date" content="2026/11/01">`, "https://example.com/a")
	if got != "2026-11-01T00:00:00Z" || source != "meta.citation_date" {
		t.Fatalf("got=%q source=%q", got, source)
	}
}

// TestExtractPublishedAt_citationOnlineDate citation_online_date 兜底键
// （对齐 :105-109 elif 分支）。
func TestExtractPublishedAt_citationOnlineDate(t *testing.T) {
	got, source := ExtractPublishedAt(
		`<meta name="citation_online_date" content="2026/11/05">`, "https://example.com/a")
	if got != "2026-11-05T00:00:00Z" || source != "meta.citation_online_date" {
		t.Fatalf("got=%q source=%q", got, source)
	}
}

// TestExtractPublishedAt_jsonLD 对齐 :120-123 json_ld 分支。
func TestExtractPublishedAt_jsonLD(t *testing.T) {
	got, source := ExtractPublishedAt(
		`{"datePublished":"2026-11-02"}`, "https://example.com/a")
	if got != "2026-11-02" || source != "json_ld.datePublished" {
		t.Fatalf("got=%q source=%q", got, source)
	}
}

// TestExtractPublishedAt_jinaReader 对齐 :124-126。
func TestExtractPublishedAt_jinaReader(t *testing.T) {
	got, source := ExtractPublishedAt("Published Time: 2026-11-03 10:00:00", "https://example.com/a")
	if got != "2026-11-03 10:00:00" || source != "jina_reader.published_time" {
		t.Fatalf("got=%q source=%q", got, source)
	}
}

// TestExtractPublishedAt_arxiv arXiv abs 页版本历史分支（对齐 :48-64）。
func TestExtractPublishedAt_arxiv(t *testing.T) {
	text := `[v2] Mon, 02 Nov 2026 10:00:00 UTC (revised)`
	got, source := ExtractPublishedAt(text, "https://arxiv.org/abs/2511.12345v2")
	if got == "" || source != "arxiv.version_history" {
		t.Fatalf("got=%q source=%q", got, source)
	}
}

// TestExtractPublishedAt_arxivV1无历史 v1 且无历史段必须回 None（对齐 :62-64）。
func TestExtractPublishedAt_arxivV1无历史(t *testing.T) {
	got, _ := ExtractPublishedAt("no history", "https://arxiv.org/abs/2511.12345v1")
	if got != "" {
		t.Fatalf("got=%q", got)
	}
}

// TestExtractPublishedAt_sqlite sqlite releaselog 标题分支（对齐 :127-133）。
func TestExtractPublishedAt_sqlite(t *testing.T) {
	text := `<h2>SQLite Release 3.50.0 On 2026-10-01</h2>`
	got, source := ExtractPublishedAt(text, "https://sqlite.org/releaselog/3_50_0.html")
	if got != "2026-10-01" || source != "sqlite.release_heading" {
		t.Fatalf("got=%q source=%q", got, source)
	}
}

// TestExtractPublishedAt_无发布时间 不允许从正文任意日期推断（对齐 :48 docstring）。
func TestExtractPublishedAt_无发布时间(t *testing.T) {
	got, _ := ExtractPublishedAt("随便一篇文章 2026-11-01 出炉", "https://example.com/a")
	if got != "" {
		t.Fatalf("正文日期不得被推断: %q", got)
	}
}

// TestMsnDetailAPIUrl msn 文章页 detail API 推导（对齐 :168-176）。
func TestMsnDetailAPIUrl(t *testing.T) {
	if got := MsnDetailAPIURL("https://www.msn.com/zh-cn/news/ar-AA1abcXYZ"); got != "https://assets.msn.com/content/view/v2/Detail/zh-cn/AA1abcXYZ" {
		t.Fatalf("got=%q", got)
	}
	if got := MsnDetailAPIURL("https://example.com/ar-AA1abcXYZ"); got != "" {
		t.Fatalf("非 msn 域必须返回空: %q", got)
	}
	if got := MsnDetailAPIURL("https://www.msn.com/zh-cn/news/other"); got != "" {
		t.Fatalf("无 ar- 段必须返回空: %q", got)
	}
}

// TestFetchTopicPageSync_html正文 httptest 页面：正文/标题/发布时间提取
// （对齐 :241-283 fetch_topic_page_sync）。
func TestFetchTopicPageSync_html正文(t *testing.T) {
	body := `<html><head><title>测试页</title>` +
		`<meta property="og:published_time" content="2026-11-01T08:00:00+00:00"></head>` +
		`<body><nav>导航</nav><article><p>正文 内容</p></article></body></html>`
	srv := newTopicFetchServer(t, body, "text/html; charset=utf-8", 200)
	defer srv.Close()
	result, err := FetchTopicPageSync(srv.URL, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if result["fetch_reader"] != "direct" || result["title"] != "测试页" {
		t.Fatalf("result=%v", result)
	}
	if got := result["content"].(string); got != "正文 内容" {
		t.Fatalf("content=%q", got)
	}
	if result["published_at_source"] != "meta.og:published_time" {
		t.Fatalf("published_at=%v", result["published_at"])
	}
}

// TestFetchTopicPageSync_非HTML文本 非内容类型按空白折叠（对齐 :271-273 else 分支）。
func TestFetchTopicPageSync_非HTML文本(t *testing.T) {
	srv := newTopicFetchServer(t, "纯文本  内容\n多行", "text/plain", 200)
	defer srv.Close()
	result, err := FetchTopicPageSync(srv.URL, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := result["content"].(string); got != "纯文本 内容 多行" {
		t.Fatalf("content=%q", got)
	}
}

// TestFetchTopicPageSync_unsafe目标 私网地址在 publicHTTPS 模式必须拒绝
// （对齐 _fetch_verified_response :179-181：public_https=False 不校验，
// True 逐跳校验——127.0.0.1 在 True 模式被拒）。
func TestFetchTopicPageSync_unsafe目标(t *testing.T) {
	_, err := FetchTopicPageSync("http://127.0.0.1:1/x", 1, true)
	if err == nil || !strings.Contains(err.Error(), "unsafe fetch target") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchTopicPageSync_重定向到私网拒绝 公网 URL 重定向进私网必须拒绝
// （对齐 :178-183 public_https 每跳复核 docstring）。
func TestFetchTopicPageSync_重定向到私网拒绝(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/private", http.StatusFound)
	}))
	defer srv.Close()
	_, err := FetchTopicPageSync(srv.URL, 5, true)
	if err == nil || !strings.Contains(err.Error(), "unsafe fetch target") {
		t.Fatalf("重定向进私网必须拒绝: %v", err)
	}
}

// TestFetchTopicPageSync_401回退Jina 401/403/429 走 Jina reader
// （对齐 :247-250；jina 基址可注入以避免真实外呼）。
func TestFetchTopicPageSync_401回退Jina(t *testing.T) {
	blockSrv := newTopicFetchServer(t, "<html><body>blocked</body></html>", "text/html", 403)
	defer blockSrv.Close()
	readerSrv := newTopicFetchServerFunc(t, func(*httpRequestRecord) (int, string, string) {
		return 200, "text/plain", "Title: 被挡页\n\nMarkdown Content:\n# 被挡页\n正文内容"
	})
	defer readerSrv.Close()
	old := jinaReaderBaseURL
	jinaReaderBaseURL = readerSrv.URL + "/"
	t.Cleanup(func() { jinaReaderBaseURL = old })
	result, err := FetchTopicPageSync(blockSrv.URL, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if result["fetch_reader"] != "jina-reader" || result["title"] != "被挡页" {
		t.Fatalf("result=%v", result)
	}
	if got := result["content"].(string); got != "正文内容" {
		t.Fatalf("content=%q（ATX 标题重复必须剔除）", got)
	}
}

// TestFetchMsnDetailSync msn detail API 分支（对齐 :285-319）。
func TestFetchMsnDetailSync(t *testing.T) {
	srv := newTopicFetchServer(t,
		`{"title":"msn标题","sourceHref":"https://example.com/orig","body":"<article>msn正文</article>","publishedDateTime":"2026-11-04T09:00:00Z"}`,
		"application/json", 200)
	defer srv.Close()
	result, err := FetchMsnDetailSync("https://www.msn.com/ar-AA1", srv.URL, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if result["url"] != "https://example.com/orig" || result["fetch_reader"] != "msn-detail-api" {
		t.Fatalf("result=%v", result)
	}
	if result["published_at_source"] != "msn_detail_api.publishedDateTime" {
		t.Fatalf("published_at=%v", result["published_at"])
	}
	if got := result["content"].(string); got != "msn正文" {
		t.Fatalf("content=%q", got)
	}
}
