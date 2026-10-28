package tools

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// TestDuckduckgoSearch_挑战页降级 验证 DDG 挑战页报错后 searchFree 循环降级到 jina 引擎。
func TestDuckduckgoSearch_挑战页降级(t *testing.T) {
	challengeCount := 0
	jinaCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/html/", func(w http.ResponseWriter, r *http.Request) {
		challengeCount++
		w.WriteHeader(202) // Python _is_ddg_challenge_page: 202 视为挑战
		_, _ = w.Write([]byte(`<html>anomaly.js challenge-form</html>`))
	})
	mux.HandleFunc("/jina/", func(w http.ResponseWriter, r *http.Request) {
		jinaCalled = true
		_, _ = w.Write([]byte("[Title1](https://a.com/x)\n[Title2](https://b.com/y)"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// URL 注入点（包级变量，生产代码生产侧走 env 覆写）。
	// 注：jinaSearchURLPrefix 需以 / 结尾，拼出的完整 URL path 含 http:// 会被
	// Go URL 规范化合并斜杠——httptest 服务端 handler 用前缀匹配 /jina/ 即可命中。
	origDDG, origJina := duckduckgoHTMLURL, jinaSearchURLPrefix
	duckduckgoHTMLURL = srv.URL + "/html/"
	jinaSearchURLPrefix = srv.URL + "/jina/"
	t.Cleanup(func() {
		duckduckgoHTMLURL = origDDG
		jinaSearchURLPrefix = origJina
	})

	// Python 语义：_search_free_sync 引擎顺序 DDG → DDG-jina；
	// DDG 挑战页 raise 后循环 continue 降级到 jina。
	t.Setenv("FREE_SEARCH_DDG_ENABLED", "1")
	t.Setenv("FREE_SEARCH_BING_ENABLED", "")
	engine, rows, err := searchFree("query", 8, 10)
	if err != nil {
		t.Fatalf("挑战页应触发 Jina 兜底: %v", err)
	}
	if engine != "duckduckgo-jina" {
		t.Errorf("降级后引擎应为 duckduckgo-jina: %s", engine)
	}
	if !jinaCalled || challengeCount == 0 {
		t.Errorf("降级链路未走通: challenge=%d jina=%v", challengeCount, jinaCalled)
	}
	if len(rows) != 2 {
		t.Fatalf("Jina 结果解析异常: %d", len(rows))
	}
}

// TestSearchDuckduckgo_HTML抽取 验证 result__a 正则 + uddg 重定向解码。
func TestSearchDuckduckgo_HTML抽取(t *testing.T) {
	fixture := `<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa">Title A</a>
<a class="result__snippet" href="#">Snippet A</a>
<a class="result__a" href="https://b.com">Title B</a>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fixture))
	}))
	defer srv.Close()
	origDDG := duckduckgoHTMLURL
	duckduckgoHTMLURL = srv.URL + "/?q="
	t.Cleanup(func() { duckduckgoHTMLURL = origDDG })

	rows, err := searchDuckduckgo("query", 8, 10, srv.Client())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望 2 行，实际 %d", len(rows))
	}
	if rows[0].URL != "https://example.com/a" {
		t.Errorf("DDG redirect 解码失败: %s", rows[0].URL)
	}
	if rows[0].Title != "Title A" {
		t.Errorf("标题抽取失败: %s", rows[0].Title)
	}
}

// TestDecodeBingRedirect_a1前缀 base64 解码验证。
func TestDecodeBingRedirect_a1前缀(t *testing.T) {
	encoded := "a1" + base64.RawURLEncoding.EncodeToString([]byte("https://example.com/z"))
	if got := decodeBingRedirect("https://www.bing.com/ck/a?u=" + encoded); got != "https://example.com/z" {
		t.Errorf("base64 解码失败: %s", got)
	}
	// 非 bing 域返回原值
	if got := decodeBingRedirect("https://example.com/ck/a?u=a1x"); got != "https://example.com/ck/a?u=a1x" {
		t.Errorf("非 bing 域应原样返回: %s", got)
	}
}

// TestSearchDuckduckgoViaJina_过滤规则 验证 Image 前缀过滤 + duckduckgo.com 域过滤 + 去重。
func TestSearchDuckduckgoViaJina_过滤规则(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(
			"[Image: logo](https://duckduckgo.com/logo.png)\n" +
				"[Nav Link](https://duckduckgo.com/?q=x)\n" +
				"[Good Result](https://example.com/one)\n" +
				"[Dup Result](https://example.com/one)\n" +
				"[Other](https://other.org/two)"))
	}))
	defer srv.Close()
	origJina := jinaSearchURLPrefix
	jinaSearchURLPrefix = srv.URL + "/"
	t.Cleanup(func() { jinaSearchURLPrefix = origJina })

	rows, err := searchDuckduckgoViaJina("query", 8, 10, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("过滤+去重后应剩 2: %v", rows)
	}
	if rows[0].URL != "https://example.com/one" || rows[1].URL != "https://other.org/two" {
		t.Errorf("过滤结果异常: %v", rows)
	}
}

// TestStripTagsSearch_标签剥离 验证标签替换 + 实体解码 + 空白折叠。
func TestStripTagsSearch_标签剥离(t *testing.T) {
	if got := stripTagsSearch("<b>Hello</b> &amp; <i>world</i>\n  next"); got != "Hello & world next" {
		t.Errorf("stripTags 异常: %q", got)
	}
}

// TestSearchBing_b_algo抽取 验证 b_algo 块 + h2>a + p snippet。
func TestSearchBing_b_algo抽取(t *testing.T) {
	fixture := `<li class="b_algo"><h2><a href="https://x.com/a">Result X</a></h2><p>Snippet X</p></li>
<li class="b_algo"><h2><a href="https://y.com/b">Result Y</a></h2><p>Snippet Y</p></li>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fixture))
	}))
	defer srv.Close()
	origBing := bingSearchURL
	bingSearchURL = srv.URL
	t.Cleanup(func() { bingSearchURL = origBing })

	rows, err := searchBing("query", 8, 10, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("b_algo 抽取异常: %d", len(rows))
	}
	if rows[0].URL != "https://x.com/a" || rows[0].Snippet != "Snippet X" {
		t.Errorf("bing 行内容异常: %+v", rows[0])
	}
}

// TestParsePerplexityCitations_四键序 验证 citations/search_results/web_search_results/sources 候选序。
func TestParsePerplexityCitations_四键序(t *testing.T) {
	data := map[string]any{
		"search_results": []any{"https://from-search-results.com"},
		"citations":      []any{"https://from-citations.com"},
	}
	if got := parsePerplexityCitations(data); len(got) != 1 || got[0] != "https://from-citations.com" {
		t.Errorf("citations 应优先: %v", got)
	}
	// dict 条目取 url/link/source_url
	data2 := map[string]any{
		"sources": []any{map[string]any{"link": "https://from-dict.com"}},
	}
	if got := parsePerplexityCitations(data2); len(got) != 1 || got[0] != "https://from-dict.com" {
		t.Errorf("dict 条目提取异常: %v", got)
	}
}

// TestExtractBochaURLs_六容器候选序 验证六容器顺序提取。
func TestExtractBochaURLs_六容器候选序(t *testing.T) {
	// 第一优先：data.webPages.value
	data := map[string]any{
		"data": map[string]any{
			"webPages": map[string]any{
				"value": []any{map[string]any{"url": "https://first.com"}},
			},
		},
		"webPages": map[string]any{
			"value": []any{map[string]any{"url": "https://second.com"}},
		},
	}
	if got := extractBochaURLs(data, 5); len(got) != 1 || got[0] != "https://first.com" {
		t.Errorf("候选序异常: %v", got)
	}
	// 第三优先：data.webPages 直接是 list
	data2 := map[string]any{
		"data": map[string]any{
			"webPages": []any{map[string]any{"url": "https://third.com"}},
		},
	}
	if got := extractBochaURLs(data2, 5); len(got) != 1 || got[0] != "https://third.com" {
		t.Errorf("data.webPages 直接 list 异常: %v", got)
	}
}

// TestExtractBochaAnswer_五候选验证 answer 候选链 + webPages 前 3 snippet 兜底。
func TestExtractBochaAnswer_五候选(t *testing.T) {
	data := map[string]any{"answer": "direct answer"}
	if got := extractBochaAnswer(data); got != "direct answer" {
		t.Errorf("answer 候选异常: %q", got)
	}
	// webPages 兜底
	data2 := map[string]any{
		"data": map[string]any{
			"webPages": map[string]any{
				"value": []any{
					map[string]any{"snippet": "s1"},
					map[string]any{"summary": "s2"},
					map[string]any{"snippet": "s3"},
					map[string]any{"snippet": "s4"}, // 超过前 3
				},
			},
		},
	}
	if got := extractBochaAnswer(data2); got != "s1\n\ns2\n\ns3" {
		t.Errorf("webPages 兜底异常: %q", got)
	}
}

// TestMcpFreeSearch_入参钳制与引擎禁用 验证无 engine enabled 报错文案。
func TestMcpFreeSearch_入参钳制与引擎禁用(t *testing.T) {
	t.Setenv("FREE_SEARCH_DDG_ENABLED", "")
	t.Setenv("FREE_SEARCH_BING_ENABLED", "")
	result, err := McpFreeSearch.Invoke(t.Context(), map[string]any{"query": "x"}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result["result"].(string), "all free search engines are disabled") {
		t.Errorf("期望 all disabled 错误: %s", result["result"])
	}
	// 空 query
	result, err = McpFreeSearch.Invoke(t.Context(), map[string]any{"query": "  "}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if result["result"] != "[ERROR]: query cannot be empty." {
		t.Errorf("空 query 文案异常: %s", result["result"])
	}
}

// TestMcpPaidSearch_校验链 验证 provider 白名单 + key 检测 + 顺序。
func TestMcpPaidSearch_校验链(t *testing.T) {
	// 无任何 key
	t.Setenv("BOCHA_API_KEY", "")
	t.Setenv("PERPLEXITY_API_KEY", "")
	t.Setenv("SERPER_API_KEY", "")
	t.Setenv("JINA_API_KEY", "")
	result, err := McpPaidSearch.Invoke(t.Context(), map[string]any{"query": "x"}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result["result"].(string), "no paid search API keys configured") {
		t.Errorf("期望 no keys 错误: %s", result["result"])
	}
	// 非法 provider
	result, err = McpPaidSearch.Invoke(t.Context(), map[string]any{"query": "x", "provider": "google"}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result["result"].(string), "provider must be one of") {
		t.Errorf("期望 provider 校验错误: %s", result["result"])
	}
	// 指定 provider 无 key
	t.Setenv("BOCHA_API_KEY", "bk")
	result, err = McpPaidSearch.Invoke(t.Context(), map[string]any{"query": "x", "provider": "serper"}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result["result"].(string), "serper API key not configured") {
		t.Errorf("期望 not configured 错误: %s", result["result"])
	}
}

// TestMcpPaidSearch_真实请求bocha 用 httptest 假 bocha 服务验证完整链路。
func TestMcpPaidSearch_真实请求bocha(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"webPages":{"value":[{"url":"https://r1.com","summary":"答案一"}]}}}`))
	}))
	defer srv.Close()
	origURL := bochaAPIURL
	bochaAPIURL = srv.URL
	t.Cleanup(func() { bochaAPIURL = origURL })

	t.Setenv("BOCHA_API_KEY", "test-key")
	result, err := McpPaidSearch.Invoke(t.Context(), map[string]any{"query": "test"}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	s := result["result"].(string)
	if !strings.Contains(s, "Paid search provider: bocha") || !strings.Contains(s, "https://r1.com") {
		t.Errorf("bocha 链路异常: %s", s)
	}
	if !strings.Contains(s, "Answer:") || !strings.Contains(s, "答案一") {
		t.Errorf("answer 输出异常: %s", s)
	}
}

// TestMcpPaidSearch_真实请求serper 用 httptest 假 serper 服务验证。
func TestMcpPaidSearch_真实请求serper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organic":[{"link":"https://s1.com"},{"link":"https://s2.com"}]}`))
	}))
	defer srv.Close()
	origURL := serperAPIURL
	serperAPIURL = srv.URL
	t.Cleanup(func() { serperAPIURL = origURL })

	t.Setenv("BOCHA_API_KEY", "")
	t.Setenv("PERPLEXITY_API_KEY", "")
	t.Setenv("JINA_API_KEY", "")
	t.Setenv("SERPER_API_KEY", "serper-key")
	result, err := McpPaidSearch.Invoke(t.Context(), map[string]any{"query": "test", "provider": "serper"}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	s := result["result"].(string)
	if !strings.Contains(s, "Paid search provider: serper") || !strings.Contains(s, "https://s2.com") {
		t.Errorf("serper 链路异常: %s", s)
	}
}

// TestSearchDuckduckgo_默认verify校验证书 验证免费搜索默认构造走证书校验（S-02 修复）。
// Python _http_request L101 setdefault("verify", get_requests_verify()) 先行短路，
// 实际默认 verify=true；JIUWENSWARM_SSL_VERIFY 未设置时对自签证书服务应报 TLS 错误。
func TestSearchDuckduckgo_默认verify校验证书(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过网络栈用例")
	}
	// 自签证书 HTTPS 服务（非 TLShttptest 不可用时跳过）
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a class="result__a" href="https://r1.com">T1</a>`))
	}))
	defer srv.Close()

	t.Setenv("JIUWENSWARM_SSL_VERIFY", "")
	t.Setenv("FREE_SEARCH_SSL_VERIFY", "")
	t.Setenv("FREE_SEARCH_DDG_URL", srv.URL)
	t.Setenv("FREE_SEARCH_PROXY_URL", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	// client=nil 触发默认构造路径（getRequestsVerify() → InsecureSkipVerify=false）
	_, err := searchDuckduckgo("test", 3, 5, nil)
	if err == nil {
		t.Fatal("自签证书服务默认 verify=true 应报 TLS 校验错误")
	}
	if !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "tls") {
		t.Errorf("应为证书校验错误，实际: %v", err)
	}
}

// TestSearchBing_默认verify校验证书 同 S-02，Bing 引擎默认构造验证。
func TestSearchBing_默认verify校验证书(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过网络栈用例")
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<li class="b_algo"><h2><a href="https://r1.com">T1</a></h2></li>`))
	}))
	defer srv.Close()

	t.Setenv("JIUWENSWARM_SSL_VERIFY", "")
	t.Setenv("FREE_SEARCH_SSL_VERIFY", "")
	t.Setenv("FREE_SEARCH_PROXY_URL", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	origURL := bingSearchURL
	bingSearchURL = srv.URL
	t.Cleanup(func() { bingSearchURL = origURL })

	_, err := searchBing("test", 3, 5, nil)
	if err == nil {
		t.Fatal("自签证书服务默认 verify=true 应报 TLS 校验错误")
	}
	if !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "tls") {
		t.Errorf("应为证书校验错误，实际: %v", err)
	}
}
