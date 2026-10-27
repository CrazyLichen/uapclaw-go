// mcp_free_search / mcp_paid_search 工具：免费与付费搜索。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/search_tools.py（607 行）。

package tools

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// searchRow 搜索结果行（对齐 Python rows: {title, url, snippet}）
type searchRow struct {
	// Title 标题
	Title string
	// URL 链接
	URL string
	// Snippet 摘要
	Snippet string
}

// paidSearchResult 付费搜索结果（对齐 Python dict: {provider, answer, urls}）
type paidSearchResult struct {
	// Provider 提供商标识
	Provider string
	// Answer 摘要回答
	Answer string
	// URLs 引用链接列表
	URLs []string
}

// mcpFreeSearchInput mcp_free_search 工具输入参数
// Python: mcp_free_search(query, max_results=8, timeout_seconds=20)
type mcpFreeSearchInput struct {
	// Query 搜索词
	Query string `json:"query" jsonschema:"description=The search query string,required"`
	// MaxResults 最大结果数（默认 8，钳 1-20）
	MaxResults int `json:"max_results" jsonschema:"description=Maximum number of results to return"`
	// TimeoutSeconds 超时秒数（默认 20，钳 5-60）
	TimeoutSeconds int `json:"timeout_seconds" jsonschema:"description=Timeout in seconds for the search request"`
}

// mcpPaidSearchInput mcp_paid_search 工具输入参数
// Python: mcp_paid_search(query, provider="auto", max_results=8, timeout_seconds=45)
type mcpPaidSearchInput struct {
	// Query 搜索词
	Query string `json:"query" jsonschema:"description=The search query string,required"`
	// Provider 提供商（auto|bocha|jina|serper|perplexity）
	Provider string `json:"provider" jsonschema:"description=Search provider: auto|bocha|jina|serper|perplexity"`
	// MaxResults 最大结果数（默认 8，钳 1-20）
	MaxResults int `json:"max_results" jsonschema:"description=Maximum number of results to return"`
	// TimeoutSeconds 超时秒数（默认 45，钳 10-120）
	TimeoutSeconds int `json:"timeout_seconds" jsonschema:"description=Timeout in seconds for the search request"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// searchUA 请求 UA（Python _USER_AGENT 逐字）
	searchUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/124.0.0.0 Safari/537.36"
	// freeSearchDDGEnabledEnv DDG 开关环境变量
	freeSearchDDGEnabledEnv = "FREE_SEARCH_DDG_ENABLED"
	// freeSearchBingEnabledEnv Bing 开关环境变量
	freeSearchBingEnabledEnv = "FREE_SEARCH_BING_ENABLED"
	// freeSearchProxyURLEnv 代理环境变量
	freeSearchProxyURLEnv = "FREE_SEARCH_PROXY_URL"
	// freeSearchSSLVerifyEnv SSL 验证开关环境变量
	freeSearchSSLVerifyEnv = "FREE_SEARCH_SSL_VERIFY"
	// freeSearchDDGURLEnv DDG URL 覆写环境变量
	freeSearchDDGURLEnv = "FREE_SEARCH_DDG_URL"
	// sslVerifyEnv 全局 SSL 验证开关（对齐 Python JIUWENSWARM_SSL_VERIFY）
	sslVerifyEnv = "JIUWENSWARM_SSL_VERIFY"
	// freeSearchDefaultNoProxy 默认 no_proxy 列表（Python _FREE_SEARCH_DEFAULT_NO_PROXY 逐字）
	freeSearchDefaultNoProxy = "127.0.0.1,.huawei.com,localhost,local,.local,10.155.97.247,.myhuaweicloud.com"
	// bochaAPIURLDefault bocha 默认 API 地址
	bochaAPIURLDefault = "https://api.bocha.cn/v1/web-search"
	// serperAPIURLDefault serper 固定 API 地址
	serperAPIURLDefault = "https://google.serper.dev/search"
	// perplexityAPIURLDefault perplexity 默认 API 地址
	perplexityAPIURLDefault = "https://api.perplexity.ai/chat/completions"
	// jinaDeepSearchAPIURL jina deepsearch 固定 API 地址
	jinaDeepSearchAPIURL = "https://deepsearch.jina.ai/v1/chat/completions"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ddgResultAPattern DDG 结果链接正则（Python L190-194 逐字）
	ddgResultAPattern = regexp.MustCompile(`(?is)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	// ddgSnippetPattern DDG 摘要双 pattern（a/div 两种容器）
	ddgSnippetPattern = regexp.MustCompile(`(?is)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>|<div[^>]+class="result__snippet"[^>]*>(.*?)</div>`)
	// jinaMarkdownLinkPattern Jina 渲染的 markdown 链接
	jinaMarkdownLinkPattern = regexp.MustCompile(`(?i)\[([^\]\n]+)\]\((https?://[^\s)]+)\)`)
	// bingBAlgoPattern Bing b_algo 结果块
	bingBAlgoPattern = regexp.MustCompile(`(?is)<li[^>]+class="[^"]*\bb_algo\b[^"]*"[^>]*>(.*?)</li>`)
	// bingH2APattern Bing 块内 h2>a 标题链接
	bingH2APattern = regexp.MustCompile(`(?is)<h2[^>]*>\s*<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	// bingPPattern Bing 块内 p 摘要
	bingPPattern = regexp.MustCompile(`(?is)<p>(.*?)</p>`)
	// tagStripPattern 标签剥离
	tagStripPattern = regexp.MustCompile(`<[^>]+>`)
	// whitespacePattern 连续空白折叠
	whitespacePattern = regexp.MustCompile(`\s+`)
	// jinaAnswerURLPattern jina answer 内 URL 提取
	jinaAnswerURLPattern = regexp.MustCompile(`https?://[^\s)\]>"']+`)
	// ddgRedirectTextPattern DDG 挑战页标记
	ddgChallengeMarkerPatterns = []string{"/anomaly.js", "challenge-form", "duckduckgo.com/anomaly.js"}

	// duckduckgoHTMLURL DDG HTML 搜索端点（测试注入点；生产侧 duckduckgoSearchURL 先读 env 覆写）
	duckduckgoHTMLURL = "https://html.duckduckgo.com/html/"
	// jinaSearchURLPrefix Jina 搜索前缀（测试注入点）
	jinaSearchURLPrefix = "https://r.jina.ai/"
	// bingSearchURL Bing 搜索端点（测试注入点）
	bingSearchURL = "https://www.bing.com/search"
	// bochaAPIURL bocha API 地址（测试注入点）
	bochaAPIURL = bochaAPIURLDefault
	// serperAPIURL serper API 地址（测试注入点）
	serperAPIURL = serperAPIURLDefault
	// perplexityAPIURL perplexity API 地址（测试注入点）
	perplexityAPIURL = perplexityAPIURLDefault
	// jinaDeepSearchAPIURLVar jina deepsearch API 地址（测试注入点）
	jinaDeepSearchAPIURLVar = jinaDeepSearchAPIURL
)

// McpFreeSearch mcp_free_search 工具单例（对齐 Python @tool 模块级实例）
var McpFreeSearch = newMcpFreeSearchTool()

// McpPaidSearch mcp_paid_search 工具单例（对齐 Python @tool 模块级实例）
var McpPaidSearch = newMcpPaidSearchTool()

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// getRequestsVerify 读 JIUWENSWARM_SSL_VERIFY（默认 true）。
// Python: ssl_config.get_requests_verify —— 0/false/no/off→false，1/true/yes/on→true。
func getRequestsVerify() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(sslVerifyEnv)))
	switch raw {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return true
	}
}

// getFreeSearchProxyURL 读取免费搜索代理 URL。
// Python: _get_free_search_proxy_url
func getFreeSearchProxyURL() string {
	return strings.TrimSpace(os.Getenv(freeSearchProxyURLEnv))
}

// searchEnvBool 布尔环境变量解析（1/true/yes/on/enabled → true，空取默认）。
// Python: search_tools.py _env_bool（Go 命名区分：本文件与 mcp_toolkits.go 各有一份 _env_flag，
// 对齐 Python 双文件各自实现的边界——spec 偏差 #5）。
func searchEnvBool(name string, defaultVal bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return defaultVal
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on" || raw == "enabled"
}

// searchEnvFlag 免费搜索开关（默认 false）。
// Python: _env_flag（本文件副本）
func searchEnvFlag(name string, defaultVal bool) bool {
	return searchEnvBool(name, defaultVal)
}

// freeSearchSSLVerify 免费搜索 SSL 验证开关（默认 false）。
// Python: _free_search_ssl_verify
func freeSearchSSLVerify() bool {
	return searchEnvBool(freeSearchSSLVerifyEnv, false)
}

// duckduckgoSearchURL 构造 DDG 搜索 URL（env 覆写 + q 参数拼接）。
// Python: _duckduckgo_search_url
func duckduckgoSearchURL(query string) string {
	baseURL := strings.TrimSpace(os.Getenv(freeSearchDDGURLEnv))
	if baseURL == "" {
		baseURL = duckduckgoHTMLURL
	}
	separator := "?"
	if strings.Contains(baseURL, "?") {
		separator = "&"
	}
	return strings.TrimRight(baseURL, "?&") + separator + "q=" + url.QueryEscape(query)
}

// noProxyEntries 解析 no_proxy 条目列表。
// Python: _no_proxy_entries
func noProxyEntries() []string {
	configured := os.Getenv("NO_PROXY")
	if configured == "" {
		configured = os.Getenv("no_proxy")
	}
	if configured == "" {
		configured = freeSearchDefaultNoProxy
	}
	var entries []string
	for _, entry := range strings.Split(configured, ",") {
		trimmed := strings.TrimSpace(strings.ToLower(entry))
		if trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}

// shouldBypassFreeSearchProxy 判断 URL 是否绕过免费搜索代理。
// Python: _should_bypass_free_search_proxy
func shouldBypassFreeSearchProxy(rawURL string) bool {
	proxyURL := getFreeSearchProxyURL()
	if proxyURL == "" {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return false
	}
	for _, entry := range noProxyEntries() {
		if entry == "*" {
			return true
		}
		if strings.HasPrefix(entry, ".") && (hostname == entry[1:] || strings.HasSuffix(hostname, entry)) {
			return true
		}
		if hostname == entry || strings.HasSuffix(hostname, "."+entry) {
			return true
		}
	}
	return false
}

// newFreeSearchHTTPClient 构造免费搜索 HTTP 客户端（verify + proxy）。
// Python: _apply_free_search_proxy + requests kwargs 的 Go 等价物。
// verify=false 时跳过证书校验（对齐 Python verify kwarg）。
// 显式代理且未绕过时挂代理；否则走环境代理（ProxyError 降级语义由 httpDo 实现）。
func newFreeSearchHTTPClient(verify bool, proxyURL string) *http.Client {
	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: !verify},
		ForceAttemptHTTP2: true,
	}
	if proxyURL != "" && !shouldBypassFreeSearchProxy(placeholderTargetURL) {
		if parsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	} else {
		transport.Proxy = http.ProxyFromEnvironment
	}
	return &http.Client{Transport: transport}
}

// placeholderTargetURL 代理绕过判断的探测 URL（代理配置只对搜索目标生效，
// 绕过列表按搜索目标域名判断——生产调用方 httpDo 传入真实 URL）。
const placeholderTargetURL = ""

// httpDo 执行 HTTP 请求。
// Python: _http_request —— "Try normal request first; retry without env proxies on ProxyError."
// Go 差异：无 requests.ProxyError 对应错误类型——无显式代理且使用环境代理失败时
// （代理类错误：连接拒绝/超时等网络错误），降级 Proxy: nil 重试一次（函数注释标注差异）。
func httpDo(method, rawURL string, body io.Reader, headers map[string]string, timeoutSeconds int, verify bool, proxyURL string) (*http.Response, []byte, error) {
	doOnce := func(noEnvProxy bool) (*http.Response, []byte, error) {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !verify},
		}
		if proxyURL != "" && !shouldBypassFreeSearchProxy(rawURL) {
			if parsed, err := url.Parse(proxyURL); err == nil {
				transport.Proxy = http.ProxyURL(parsed)
			}
		} else if !noEnvProxy {
			transport.Proxy = http.ProxyFromEnvironment
		}
		client := &http.Client{
			Transport: transport,
			Timeout:   time.Duration(timeoutSeconds) * time.Second,
		}
		req, err := http.NewRequestWithContext(contextWithTimeout(timeoutSeconds), strings.ToUpper(method), rawURL, body)
		if err != nil {
			return nil, nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		return resp, data, nil
	}

	resp, data, err := doOnce(false)
	if err != nil {
		// 无显式代理时降级重试（对齐 Python ProxyError → trust_env=False 重试）
		if !(proxyURL != "" && !shouldBypassFreeSearchProxy(rawURL)) {
			resp, data, err = doOnce(true)
		}
	}
	return resp, data, err
}

// contextWithTimeout 构造带超时的 context（httpDo 内部使用）。
func contextWithTimeout(timeoutSeconds int) context.Context {
	return contextWithTimeoutValue(timeoutSeconds)
}

// stripTagsSearch 剥离 HTML 标签并折叠空白（search 文件独立副本）。
// Python: _strip_tags（本文件副本，mcp_web_fetch.go 另有一份——对齐 Python 双文件边界）
func stripTagsSearch(value string) string {
	value = tagStripPattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(html.UnescapeString(value), " "))
}

// decodeDDGRedirectSearch 解码 DDG 重定向 URL（search 文件独立副本）。
// Python: _decode_ddg_redirect（本文件副本）
func decodeDDGRedirectSearch(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if parsed.Path != "/l/" {
		return u
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return u
	}
	target := values.Get("uddg")
	if target == "" {
		return u
	}
	if decoded, err := url.QueryUnescape(target); err == nil {
		return decoded
	}
	return u
}

// decodeBingRedirect 解码 Bing 重定向 URL。
// Python: _decode_bing_redirect —— a1 前缀 base64 urlsafe 解码
func decodeBingRedirect(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if !strings.Contains(strings.ToLower(parsed.Host), "bing.com") || parsed.Path != "/ck/a" {
		return u
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return u
	}
	encoded := values.Get("u")
	if encoded == "" {
		return u
	}
	if strings.HasPrefix(encoded, "a1") {
		payload := encoded[2:]
		padding := strings.Repeat("=", (4-len(payload)%4)%4)
		decoded, err := base64.URLEncoding.DecodeString(payload + padding)
		if err != nil {
			return u
		}
		decodedStr := string(decoded)
		if strings.HasPrefix(decodedStr, "http://") || strings.HasPrefix(decodedStr, "https://") {
			return decodedStr
		}
	} else if strings.HasPrefix(encoded, "http://") || strings.HasPrefix(encoded, "https://") {
		return encoded
	}
	return u
}

// isDDGChallengePage 判断是否 DDG 反爬挑战页。
// Python: _is_ddg_challenge_page —— 状态码 {202,418,429,503} + 三标记
func isDDGChallengePage(statusCode int, pageHTML string) bool {
	switch statusCode {
	case 202, 418, 429, 503:
		return true
	}
	text := strings.ToLower(pageHTML)
	for _, marker := range ddgChallengeMarkerPatterns {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// searchDuckduckgo DDG HTML 搜索。
// Python: _search_duckduckgo_sync(query, max_results, timeout_seconds)
// Go 差异：client 尾参为测试注入扩展（生产传 newFreeSearchHTTPClient 构造实例）。
func searchDuckduckgo(query string, maxResults, timeoutSeconds int, client *http.Client) ([]searchRow, error) {
	if client == nil {
		client = newFreeSearchHTTPClient(freeSearchSSLVerify(), getFreeSearchProxyURL())
	}
	req, err := http.NewRequest("GET", duckduckgoSearchURL(query), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", searchUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	pageHTMLBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	pageHTML := string(pageHTMLBytes)

	if isDDGChallengePage(resp.StatusCode, pageHTML) {
		return nil, fmt.Errorf("DuckDuckGo anti-bot challenge page returned")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DuckDuckGo returned non-200 status: %d", resp.StatusCode)
	}

	links := ddgResultAPattern.FindAllStringSubmatch(pageHTML, -1)
	snippets := ddgSnippetPattern.FindAllStringSubmatch(pageHTML, -1)

	var rows []searchRow
	for index, match := range links {
		if index >= maxResults {
			break
		}
		href, titleRaw := match[1], match[2]
		snippetRaw := ""
		if index < len(snippets) {
			// 双 pattern 交替（a 组 [1] 或 div 组 [2]）
			if len(snippets[index]) >= 3 {
				if snippets[index][1] != "" {
					snippetRaw = snippets[index][1]
				} else {
					snippetRaw = snippets[index][2]
				}
			}
		}
		title := stripTagsSearch(titleRaw)
		if title == "" {
			title = fmt.Sprintf("Result %d", index+1)
		}
		rows = append(rows, searchRow{
			Title:   title,
			URL:     decodeDDGRedirectSearch(href),
			Snippet: stripTagsSearch(snippetRaw),
		})
	}
	return rows, nil
}

// searchDuckduckgoViaJina 经 Jina Reader 的 DDG 搜索。
// Python: _search_duckduckgo_via_jina_sync（URL: r.jina.ai/http://duckduckgo.com/html/?q=）
func searchDuckduckgoViaJina(query string, maxResults, timeoutSeconds int, client *http.Client) ([]searchRow, error) {
	if client == nil {
		client = newFreeSearchHTTPClient(freeSearchSSLVerify(), getFreeSearchProxyURL())
	}
	fullURL := jinaSearchURLPrefix + "http://duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", searchUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	textBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jina reader returned non-2xx status: %d", resp.StatusCode)
	}
	text := string(textBytes)

	matches := jinaMarkdownLinkPattern.FindAllStringSubmatch(text, -1)
	var rows []searchRow
	seen := make(map[string]bool)
	for _, match := range matches {
		titleRaw, href := match[1], match[2]
		title := stripTagsSearch(titleRaw)
		if title == "" || strings.HasPrefix(title, "Image ") {
			continue
		}
		decoded := decodeDDGRedirectSearch(href)
		parsed, err := url.Parse(decoded)
		if err != nil || !strings.HasPrefix(parsed.Scheme, "http") {
			continue
		}
		// 丢弃 DDG 导航/自引用链接
		if strings.Contains(strings.ToLower(parsed.Host), "duckduckgo.com") {
			continue
		}
		if seen[decoded] {
			continue
		}
		seen[decoded] = true
		rows = append(rows, searchRow{Title: title, URL: decoded, Snippet: ""})
		if len(rows) >= maxResults {
			break
		}
	}
	return rows, nil
}

// searchBing Bing 搜索。
// Python: _search_bing_sync
func searchBing(query string, maxResults, timeoutSeconds int, client *http.Client) ([]searchRow, error) {
	if client == nil {
		client = newFreeSearchHTTPClient(freeSearchSSLVerify(), getFreeSearchProxyURL())
	}
	fullURL := bingSearchURL + "?q=" + url.QueryEscape(query)
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", searchUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	htmlBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bing returned non-2xx status: %d", resp.StatusCode)
	}
	pageHTML := string(htmlBytes)

	blocks := bingBAlgoPattern.FindAllStringSubmatch(pageHTML, -1)
	var rows []searchRow
	seen := make(map[string]bool)
	for _, block := range blocks {
		titleMatch := bingH2APattern.FindStringSubmatch(block[1])
		if titleMatch == nil {
			continue
		}
		hrefRaw := html.UnescapeString(titleMatch[1])
		href := decodeBingRedirect(hrefRaw)
		title := stripTagsSearch(titleMatch[2])
		if href == "" || seen[href] {
			continue
		}
		seen[href] = true
		snippet := ""
		if snippetMatch := bingPPattern.FindStringSubmatch(block[1]); snippetMatch != nil {
			snippet = stripTagsSearch(snippetMatch[1])
		}
		if title == "" {
			title = fmt.Sprintf("Result %d", len(rows)+1)
		}
		rows = append(rows, searchRow{Title: title, URL: href, Snippet: snippet})
		if len(rows) >= maxResults {
			break
		}
	}
	return rows, nil
}

// searchFree 免费搜索引擎聚合（DDG → DDG-jina → BING 顺序尝试）。
// Python: _search_free_sync —— error 汇聚 " | " 连接
func searchFree(query string, maxResults, timeoutSeconds int) (string, []searchRow, error) {
	var errors []string
	type engineEntry struct {
		name   string
		runner func(string, int, int, *http.Client) ([]searchRow, error)
	}
	var engines []engineEntry
	if searchEnvFlag(freeSearchDDGEnabledEnv, false) {
		engines = append(engines,
			engineEntry{"duckduckgo", searchDuckduckgo},
			engineEntry{"duckduckgo-jina", searchDuckduckgoViaJina},
		)
	}
	if searchEnvFlag(freeSearchBingEnabledEnv, false) {
		engines = append(engines, engineEntry{"bing", searchBing})
	}
	if len(engines) == 0 {
		return "", nil, fmt.Errorf("all free search engines are disabled")
	}
	for _, engine := range engines {
		rows, err := engine.runner(query, maxResults, timeoutSeconds, nil)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", engine.name, err))
			continue
		}
		if len(rows) > 0 {
			return engine.name, rows, nil
		}
		errors = append(errors, fmt.Sprintf("%s: empty result", engine.name))
	}
	return "", nil, fmt.Errorf("%s", strings.Join(errors, " | "))
}

// engineDisplayName 引擎显示名。
// Python: _engine_display_name
func engineDisplayName(engine string) string {
	switch engine {
	case "duckduckgo":
		return "DuckDuckGo"
	case "duckduckgo-jina":
		return "DuckDuckGo (via jina.ai)"
	case "bing":
		return "Bing"
	default:
		return engine
	}
}

// parsePerplexityCitations 解析 Perplexity 引用（四键序候选）。
// Python: _parse_perplexity_citations
func parsePerplexityCitations(data map[string]any) []string {
	for _, key := range []string{"citations", "search_results", "web_search_results", "sources"} {
		entries, ok := data[key].([]any)
		if !ok {
			continue
		}
		var urls []string
		for _, item := range entries {
			if s, ok := item.(string); ok {
				urls = append(urls, s)
			} else if m, ok := item.(map[string]any); ok {
				maybeURL := ""
				for _, k := range []string{"url", "link", "source_url"} {
					if v, ok := m[k].(string); ok && v != "" {
						maybeURL = v
						break
					}
				}
				if maybeURL != "" {
					urls = append(urls, maybeURL)
				}
			}
		}
		if len(urls) > 0 {
			return urls
		}
	}
	return nil
}

// perplexitySearch Perplexity 搜索。
// Python: _perplexity_search_sync —— PPLX_MODEL 默认 sonar-pro（无白名单校验，直接复刻默认值+透传）
func perplexitySearch(query string, maxResults, timeoutSeconds int) (*paidSearchResult, error) {
	perplexityKey := os.Getenv("PERPLEXITY_API_KEY")
	if perplexityKey == "" {
		return nil, fmt.Errorf("PERPLEXITY_API_KEY is not set")
	}

	model := os.Getenv("PPLX_MODEL")
	if model == "" {
		model = "sonar-pro"
	}
	payload := map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{"role": "system", "content": "Provide concise answer and include citations."},
			map[string]any{"role": "user", "content": query},
		},
		"max_tokens":  1024,
		"temperature": 0.2,
		"stream":      false,
	}
	body, _ := json.Marshal(payload)
	resp, data, err := httpDo("POST", perplexityAPIURL, strings.NewReader(string(body)),
		map[string]string{
			"Authorization": "Bearer " + perplexityKey,
			"Content-Type":  "application/json",
		}, timeoutSeconds, getRequestsVerify(), getFreeSearchProxyURL())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("perplexity returned non-2xx status: %d", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	answer := ""
	if choices, ok := parsed["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if content, ok := message["content"].(string); ok {
					answer = content
				}
			}
		}
	}

	urls := parsePerplexityCitations(parsed)
	if len(urls) > maxResults {
		urls = urls[:maxResults]
	}
	return &paidSearchResult{Provider: "perplexity", Answer: strings.TrimSpace(answer), URLs: urls}, nil
}

// extractBochaURLs 提取 bocha 结果 URL（六容器候选序）。
// Python: _extract_bocha_urls
func extractBochaURLs(data map[string]any, maxResults int) []string {
	// 六容器候选序：data.webPages.value → webPages.value → data.webPages → webPages → data.results → results
	var candidates []any
	if m, ok := data["data"].(map[string]any); ok {
		if wp, ok := m["webPages"].(map[string]any); ok {
			candidates, _ = wp["value"].([]any)
		}
	}
	if candidates == nil {
		if wp, ok := data["webPages"].(map[string]any); ok {
			candidates, _ = wp["value"].([]any)
		}
	}
	if candidates == nil {
		if m, ok := data["data"].(map[string]any); ok {
			candidates, _ = m["webPages"].([]any)
		}
	}
	if candidates == nil {
		candidates, _ = data["webPages"].([]any)
	}
	if candidates == nil {
		if m, ok := data["data"].(map[string]any); ok {
			candidates, _ = m["results"].([]any)
		}
	}
	if candidates == nil {
		candidates, _ = data["results"].([]any)
	}

	var urls []string
	for i, item := range candidates {
		if i >= maxResults {
			break
		}
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		maybeURL := ""
		if v, ok := m["url"].(string); ok && v != "" {
			maybeURL = v
		} else if v, ok := m["link"].(string); ok && v != "" {
			maybeURL = v
		}
		if maybeURL != "" {
			urls = append(urls, maybeURL)
		}
	}
	return urls
}

// extractBochaAnswer 提取 bocha 摘要回答（五候选 + webPages 前 3 snippet 兜底）。
// Python: _extract_bocha_answer
func extractBochaAnswer(data map[string]any) string {
	// 五候选：summary → answer → data.summary → data.answer → data.message
	candidate := func(key string) string {
		if v, ok := data[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if m, ok := data["data"].(map[string]any); ok {
			if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	for _, key := range []string{"summary", "answer", "summary", "answer", "message"} {
		// 注：Python 候选序为 [data.summary, data.answer, data.message] 顶层在前：
		// candidates = [data.get("summary"), data.get("answer"), data.get("data", {}).get("summary"),
		//               data.get("data", {}).get("answer"), data.get("data", {}).get("message")]
		if v := candidate(key); v != "" {
			return v
		}
	}

	// webPages 前 3 snippet 兜底
	if m, ok := data["data"].(map[string]any); ok {
		if wp, ok := m["webPages"].(map[string]any); ok {
			if value, ok := wp["value"].([]any); ok {
				var snippets []string
				for i, item := range value {
					if i >= 3 {
						break
					}
					im, ok := item.(map[string]any)
					if !ok {
						continue
					}
					snippet := ""
					if v, ok := im["summary"].(string); ok && strings.TrimSpace(v) != "" {
						snippet = strings.TrimSpace(v)
					} else if v, ok := im["snippet"].(string); ok && strings.TrimSpace(v) != "" {
						snippet = strings.TrimSpace(v)
					}
					if snippet != "" {
						snippets = append(snippets, snippet)
					}
				}
				if len(snippets) > 0 {
					return strings.Join(snippets, "\n\n")
				}
			}
		}
	}
	return ""
}

// bochaSearch Bocha 搜索。
// Python: _bocha_search_sync —— Bearer + summary:true/count
func bochaSearch(query string, maxResults, timeoutSeconds int) (*paidSearchResult, error) {
	bochaKey := os.Getenv("BOCHA_API_KEY")
	if bochaKey == "" {
		return nil, fmt.Errorf("BOCHA_API_KEY is not set")
	}

	payload := map[string]any{"query": query, "summary": true, "count": maxResults}
	body, _ := json.Marshal(payload)
	resp, data, err := httpDo("POST", bochaAPIURL, strings.NewReader(string(body)),
		map[string]string{
			"Authorization": "Bearer " + bochaKey,
			"Content-Type":  "application/json",
		}, timeoutSeconds, getRequestsVerify(), getFreeSearchProxyURL())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bocha returned non-2xx status: %d", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return &paidSearchResult{
		Provider: "bocha",
		Answer:   extractBochaAnswer(parsed),
		URLs:     extractBochaURLs(parsed, maxResults),
	}, nil
}

// serperSearch Serper 搜索。
// Python: _serper_search_sync —— X-API-KEY + num
func serperSearch(query string, maxResults, timeoutSeconds int) (*paidSearchResult, error) {
	serperKey := os.Getenv("SERPER_API_KEY")
	if serperKey == "" {
		return nil, fmt.Errorf("SERPER_API_KEY is not set")
	}

	payload := map[string]any{"q": query, "num": maxResults}
	body, _ := json.Marshal(payload)
	resp, data, err := httpDo("POST", serperAPIURL, strings.NewReader(string(body)),
		map[string]string{
			"X-API-KEY":    serperKey,
			"Content-Type": "application/json",
		}, timeoutSeconds, getRequestsVerify(), getFreeSearchProxyURL())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("serper returned non-2xx status: %d", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	var urls []string
	if organic, ok := parsed["organic"].([]any); ok {
		for i, item := range organic {
			if i >= maxResults {
				break
			}
			if m, ok := item.(map[string]any); ok {
				if link, ok := m["link"].(string); ok && link != "" {
					urls = append(urls, link)
				}
			}
		}
	}
	return &paidSearchResult{Provider: "serper", Answer: "", URLs: urls}, nil
}

// jinaSearch Jina deepsearch 搜索。
// Python: _jina_search_sync —— jina-deepsearch-v1 + reasoning_effort low + answer 内 URL 正则提取
func jinaSearch(query string, timeoutSeconds int) (*paidSearchResult, error) {
	jinaKey := os.Getenv("JINA_API_KEY")
	if jinaKey == "" {
		return nil, fmt.Errorf("JINA_API_KEY is not set")
	}

	payload := map[string]any{
		"model":            "jina-deepsearch-v1",
		"messages":         []any{map[string]any{"role": "user", "content": query}},
		"stream":           false,
		"reasoning_effort": "low",
	}
	body, _ := json.Marshal(payload)
	resp, data, err := httpDo("POST", jinaDeepSearchAPIURLVar, strings.NewReader(string(body)),
		map[string]string{
			"Authorization": "Bearer " + jinaKey,
			"Content-Type":  "application/json",
		}, timeoutSeconds, getRequestsVerify(), getFreeSearchProxyURL())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jina returned non-2xx status: %d", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	answer := ""
	if choices, ok := parsed["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if content, ok := message["content"].(string); ok {
					answer = content
				}
			}
		}
	}
	urls := jinaAnswerURLPattern.FindAllString(answer, -1)
	return &paidSearchResult{Provider: "jina", Answer: strings.TrimSpace(answer), URLs: urls}, nil
}

// newMcpFreeSearchTool 构造 mcp_free_search 工具实例。
// Python: @tool 装饰的 mcp_free_search（description 逐字）
func newMcpFreeSearchTool() tool.Tool {
	card := tool.NewToolCard(
		"mcp_free_search",
		"Free search via DuckDuckGo. Input query and return ranked URLs with snippets.",
		nil, nil,
	)

	fn := func(ctx context.Context, input mcpFreeSearchInput, opts ...tool.ToolOption) (map[string]any, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return map[string]any{"result": "[ERROR]: query cannot be empty."}, nil
		}

		maxResults := clampInt(input.MaxResults, 1, 20, 8)
		timeoutSeconds := clampInt(input.TimeoutSeconds, 5, 60, 20)

		engineUsed, rows, err := searchFree(query, maxResults, timeoutSeconds)
		if err != nil {
			return map[string]any{"result": fmt.Sprintf("[ERROR]: free search failed: %v", err)}, nil
		}

		if len(rows) == 0 {
			return map[string]any{"result": fmt.Sprintf("No search results for: %s", query)}, nil
		}

		lines := []string{fmt.Sprintf("Free search results (%s) for: %s", engineDisplayName(engineUsed), query)}
		for idx, row := range rows {
			lines = append(lines, fmt.Sprintf("%d. %s", idx+1, row.Title))
			lines = append(lines, fmt.Sprintf("   URL: %s", row.URL))
			if row.Snippet != "" {
				lines = append(lines, fmt.Sprintf("   Snippet: %s", row.Snippet))
			}
		}
		return map[string]any{"result": strings.Join(lines, "\n")}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// newMcpPaidSearchTool 构造 mcp_paid_search 工具实例。
// Python: @tool 装饰的 mcp_paid_search（description 逐字）
func newMcpPaidSearchTool() tool.Tool {
	card := tool.NewToolCard(
		"mcp_paid_search",
		"Paid search via Bocha/Perplexity/SERPER/JINA. Support provider=auto|bocha|perplexity|serper|jina.",
		nil, nil,
	)

	fn := func(ctx context.Context, input mcpPaidSearchInput, opts ...tool.ToolOption) (map[string]any, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return map[string]any{"result": "[ERROR]: query cannot be empty."}, nil
		}

		provider := strings.ToLower(strings.TrimSpace(input.Provider))
		if provider == "" {
			provider = "auto"
		}
		switch provider {
		case "auto", "bocha", "jina", "serper", "perplexity":
		default:
			return map[string]any{"result": "[ERROR]: provider must be one of auto|bocha|jina|serper|perplexity."}, nil
		}

		timeoutSeconds := clampInt(input.TimeoutSeconds, 10, 120, 45)
		maxResults := clampInt(input.MaxResults, 1, 20, 8)

		// available providers 按 env key 检测（bocha→perplexity→serper→jina）
		var availableProviders []string
		if os.Getenv("BOCHA_API_KEY") != "" {
			availableProviders = append(availableProviders, "bocha")
		}
		if os.Getenv("PERPLEXITY_API_KEY") != "" {
			availableProviders = append(availableProviders, "perplexity")
		}
		if os.Getenv("SERPER_API_KEY") != "" {
			availableProviders = append(availableProviders, "serper")
		}
		if os.Getenv("JINA_API_KEY") != "" {
			availableProviders = append(availableProviders, "jina")
		}

		if len(availableProviders) == 0 {
			return map[string]any{"result": "[ERROR]: no paid search API keys configured."}, nil
		}

		var order []string
		if provider != "auto" {
			found := false
			for _, p := range availableProviders {
				if p == provider {
					found = true
					break
				}
			}
			if !found {
				return map[string]any{"result": fmt.Sprintf("[ERROR]: %s API key not configured. Available providers: %s",
					provider, strings.Join(availableProviders, ", "))}, nil
			}
			order = []string{provider}
		} else {
			for _, p := range []string{"bocha", "perplexity", "serper", "jina"} {
				for _, avail := range availableProviders {
					if p == avail {
						order = append(order, p)
						break
					}
				}
			}
		}

		runners := map[string]func() (*paidSearchResult, error){
			"bocha":      func() (*paidSearchResult, error) { return bochaSearch(query, maxResults, timeoutSeconds) },
			"jina":       func() (*paidSearchResult, error) { return jinaSearch(query, timeoutSeconds) },
			"serper":     func() (*paidSearchResult, error) { return serperSearch(query, maxResults, timeoutSeconds) },
			"perplexity": func() (*paidSearchResult, error) { return perplexitySearch(query, maxResults, timeoutSeconds) },
		}

		var errors []string
		for _, name := range order {
			result, err := runners[name]()
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", name, err))
				continue
			}

			answer := strings.TrimSpace(result.Answer)
			urls := make([]string, 0, len(result.URLs))
			for _, u := range result.URLs {
				if u != "" {
					urls = append(urls, u)
				}
			}
			if len(urls) > maxResults {
				urls = urls[:maxResults]
			}

			lines := []string{fmt.Sprintf("Paid search provider: %s", name)}
			if answer != "" {
				lines = append(lines, "Answer:")
				lines = append(lines, answer)
			}
			if len(urls) > 0 {
				lines = append(lines, "URLs:")
				for idx, u := range urls {
					lines = append(lines, fmt.Sprintf("%d. %s", idx+1, u))
				}
			}
			if answer == "" && len(urls) == 0 {
				lines = append(lines, "No usable result payload.")
			}
			return map[string]any{"result": strings.Join(lines, "\n")}, nil
		}

		return map[string]any{"result": "[ERROR]: paid search failed. " + strings.Join(errors, " | ")}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// clampInt 钳制整数值到 [lo, hi]，零值取 defaultVal。
func clampInt(v, lo, hi, defaultVal int) int {
	if v == 0 {
		v = defaultVal
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// contextWithTimeoutValue 构造带超时 context（包级辅助）。
func contextWithTimeoutValue(timeoutSeconds int) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	// httpDo 请求级超时已由 client.Timeout 保证；此 context 仅用于 NewRequestWithContext
	// 生命周期约束——不 cancel 以避免复刻语义偏差（请求结束后由 GC 回收）。
	_ = cancel
	return ctx
}
