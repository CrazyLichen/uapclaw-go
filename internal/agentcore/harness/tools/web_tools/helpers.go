package web_tools

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ──────────────────────────── 结构体 ────────────────────────────

// searchRow 搜索结果行
// 对齐 Python: 搜索结果行 dict (web_tools.py L708-714)
type searchRow struct {
	// Title 标题
	Title string `json:"title"`
	// URL 链接
	URL string `json:"url"`
	// Snippet 摘要
	Snippet string `json:"snippet"`
	// Origin 来源
	Origin string `json:"origin,omitempty"`
	// Date 日期
	Date string `json:"date,omitempty"`
	// Source 来源引擎
	Source string `json:"source"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// queryTermsRe 查询词提取正则
	// 对齐 Python: re.findall(r"[A-Za-z0-9\u4e00-\u9fff]+", ...) (web_tools.py L490)
	queryTermsRe = regexp.MustCompile(`[A-Za-z0-9` + "\u4e00-\u9fff" + `]+`)
	// cjkChunkRe CJK 连续字符正则
	cjkChunkRe = regexp.MustCompile(`[` + "\u4e00-\u9fff" + `]+`)
	// numericRe 数字提取正则
	numericRe = regexp.MustCompile(`\d{4}|\d+` + "\u6708" + `|\d+` + "\u65e5" + `|\d+`)
	// dateSuffixRe 日期后缀正则
	dateSuffixRe = regexp.MustCompile(`^\d+` + "\u6708" + `$|^\d+` + "\u65e5" + `$`)
	// urlExtractRe URL 提取正则
	// 对齐 Python: re.findall(r"https?://[^\s)\]>\"']+", ...) (web_tools.py L1129)
	urlExtractRe = regexp.MustCompile(`https?://[^\s)\]>"']+`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsCJK 字符串是否包含 CJK 字符
// 对齐 Python: _contains_cjk() (web_tools.py L431-433)
func containsCJK(value string) bool {
	for _, ch := range value {
		if ch >= '\u4e00' && ch <= '\u9fff' {
			return true
		}
	}
	return false
}

// decodeDDGRedirect 解码 DuckDuckGo 重定向 URL
// 对齐 Python: _decode_ddg_redirect() (web_tools.py L225-234)
func decodeDDGRedirect(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if parsed.Path != "/l/" {
		return rawURL
	}
	target := parsed.Query().Get("uddg")
	if target == "" {
		return rawURL
	}
	decoded, err := url.QueryUnescape(target)
	if err != nil {
		return rawURL
	}
	return decoded
}

// decodeBingRedirect 解码 Bing 重定向 URL
// 对齐 Python: _decode_bing_redirect() (web_tools.py L237-266)
func decodeBingRedirect(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if !strings.Contains(strings.ToLower(parsed.Host), "bing.com") || parsed.Path != "/ck/a" {
		return rawURL
	}
	values := parsed.Query()["u"]
	if len(values) == 0 || values[0] == "" {
		return rawURL
	}
	encoded := values[0]

	// 对齐 Python: L251-262 — a1 前缀 → base64 解码
	if strings.HasPrefix(encoded, "a1") {
		payload := encoded[2:]
		padding := ""
		if mod := len(payload) % 4; mod != 0 {
			padding = strings.Repeat("=", 4-mod)
		}
		decoded, err := base64.URLEncoding.DecodeString(payload + padding)
		if err != nil {
			return rawURL
		}
		result := string(decoded)
		if strings.HasPrefix(result, "http://") || strings.HasPrefix(result, "https://") {
			return result
		}
		return rawURL
	}

	// 对齐 Python: L263-264 — 直接是 http URL
	if strings.HasPrefix(encoded, "http://") || strings.HasPrefix(encoded, "https://") {
		return encoded
	}

	return rawURL
}

// normalizedDomain 规范化域名
// 对齐 Python: _normalized_domain() (web_tools.py L269-274)
func normalizedDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	netloc := strings.ToLower(strings.TrimSpace(parsed.Host))
	if strings.HasPrefix(netloc, "www.") {
		return netloc[4:]
	}
	return netloc
}

// isLowFetchValueURL 是否为低抓取价值 URL
// 对齐 Python: _is_low_fetch_value_url() (web_tools.py L277-280)
func isLowFetchValueURL(rawURL string) bool {
	domain := normalizedDomain(rawURL)
	for item := range lowFetchValueDomains {
		if domain == item || strings.HasSuffix(domain, "."+item) {
			return true
		}
	}
	return false
}

// isTimelyQuery 是否为时效性查询
// 对齐 Python: _is_timely_query() (web_tools.py L283-286)
func isTimelyQuery(query string) bool {
	lowered := strings.ToLower(query)
	for hint := range timelyQueryHints {
		if strings.Contains(lowered, hint) {
			return true
		}
	}
	return false
}

// isLowConfidenceResultDomain 是否为低置信度结果域名
// 对齐 Python: _is_low_confidence_result_domain() (web_tools.py L289-292)
func isLowConfidenceResultDomain(rawURL string) bool {
	domain := normalizedDomain(rawURL)
	for item := range lowConfidenceResultDomains {
		if domain == item || strings.HasSuffix(domain, "."+item) {
			return true
		}
	}
	return false
}

// searchRequestHeaders 根据查询语言返回请求头
// 对齐 Python: _search_request_headers() (web_tools.py L478-485)
func searchRequestHeaders(query string) map[string]string {
	headers := map[string]string{"User-Agent": userAgent}
	if containsCJK(query) {
		headers["Accept-Language"] = "zh-CN,zh-Hans;q=0.9,zh;q=0.8,en;q=0.6"
	} else {
		headers["Accept-Language"] = "en-US,en;q=0.9"
	}
	return headers
}

// duckduckgoSearchURL 构建 DDG 搜索 URL
// 对齐 Python: _duckduckgo_search_url() (web_tools.py L138-144)
func duckduckgoSearchURL(query string) string {
	baseURL := strings.TrimSpace(os.Getenv(freeSearchDDGURLEnv))
	if baseURL == "" {
		baseURL = "https://html.duckduckgo.com/html/"
	}
	separator := "&"
	if !strings.Contains(baseURL, "?") {
		separator = "?"
	}
	baseURL = strings.TrimRight(baseURL, "?&")
	return baseURL + separator + "q=" + url.QueryEscape(query)
}

// queryTerms 提取查询词
// 对齐 Python: _query_terms() (web_tools.py L488-513)
func queryTerms(query string) []string {
	lowered := strings.ToLower(query)
	rawTerms := queryTermsRe.FindAllString(lowered, -1)

	var terms []string
	seen := map[string]bool{}
	for _, term := range rawTerms {
		var expandedTerms []string
		if containsCJK(term) {
			// 对齐 Python: L494-505 — CJK 词汇展开
			numParts := numericRe.FindAllString(term, -1)
			expandedTerms = append(expandedTerms, numParts...)
			chunks := cjkChunkRe.FindAllString(term, -1)
			for _, chunk := range chunks {
				runeCount := utf8.RuneCountInString(chunk)
				if runeCount >= 2 && runeCount <= 4 {
					expandedTerms = append(expandedTerms, chunk)
					// 对齐 Python: L501 — 2-gram
					runes := []rune(chunk)
					for i := 0; i < len(runes)-1; i++ {
						expandedTerms = append(expandedTerms, string(runes[i:i+2]))
					}
				} else if runeCount > 4 {
					runes := []rune(chunk)
					expandedTerms = append(expandedTerms, string(runes[:2]))
					expandedTerms = append(expandedTerms, string(runes[:4]))
					expandedTerms = append(expandedTerms, string(runes[len(runes)-2:]))
				}
			}
		} else {
			expandedTerms = append(expandedTerms, term)
		}
		for _, expanded := range expandedTerms {
			if len(expanded) < 2 || queryStopwords[expanded] || seen[expanded] {
				continue
			}
			seen[expanded] = true
			terms = append(terms, expanded)
		}
		if len(terms) >= 12 {
			break
		}
	}
	return terms
}

// queryCoreTerms 提取核心查询词
// 对齐 Python: _query_core_terms() (web_tools.py L516-528)
func queryCoreTerms(query string) []string {
	var coreTerms []string
	seen := map[string]bool{}
	for _, term := range queryTerms(query) {
		if isDigitOnly(term) {
			continue
		}
		if dateSuffixRe.MatchString(term) {
			continue
		}
		if len(term) < 2 || seen[term] {
			continue
		}
		seen[term] = true
		coreTerms = append(coreTerms, term)
		if len(coreTerms) >= 8 {
			break
		}
	}
	return coreTerms
}

// matchTermCount 统计查询词在行中出现的次数
// 对齐 Python: _match_term_count() (web_tools.py L531-541)
func matchTermCount(query string, row searchRow) int {
	haystack := strings.ToLower(row.Title + " " + row.Snippet + " " + row.URL + " " + row.Origin)
	count := 0
	for _, term := range queryTerms(query) {
		if strings.Contains(haystack, term) {
			count++
		}
	}
	return count
}

// scoreRow 对搜索结果行评分
// 对齐 Python: WebFreeSearchTool._score_row() (web_tools.py L830-853)
func scoreRow(query string, row searchRow) float64 {
	haystack := strings.ToLower(row.Title + " " + row.Snippet + " " + row.URL + " " + row.Origin)

	score := 0.0
	if row.Title != "" {
		score += 2.0
	}
	if row.Snippet != "" {
		score += 1.0
	}
	if row.Origin != "" {
		score += 0.5
	}
	if row.Source == "bing-card" {
		score += 1.0
	}
	if isLowConfidenceResultDomain(row.URL) {
		score -= 1.5
	}

	for _, term := range queryTerms(query) {
		if strings.Contains(haystack, term) {
			score += 0.35
		}
	}
	return score
}

// buildBingRow 构建规范化的 Bing 结果行
// 对齐 Python: _build_bing_row() (web_tools.py L544-561)
func buildBingRow(href, title, snippet, origin, date, source string) searchRow {
	if source == "" {
		source = "bing-web"
	}
	return searchRow{
		Title:   strings.TrimSpace(title),
		URL:     strings.TrimSpace(href),
		Snippet: strings.TrimSpace(snippet),
		Origin:  strings.TrimSpace(origin),
		Date:    strings.TrimSpace(date),
		Source:  source,
	}
}

// normalizeURL 规范化 URL
// 对齐 Python: WebFetchWebpageTool._normalize_url() (web_tools.py L1586-1594)
func normalizeURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return rawURL
	}
	decoded := decodeDDGRedirect(rawURL)
	if strings.HasPrefix(decoded, "http://") || strings.HasPrefix(decoded, "https://") {
		return decoded
	}
	return "https://" + decoded
}

// clipText 截断文本
// 对齐 Python: WebFetchWebpageTool._clip_text() (web_tools.py L1579-1583)
func clipText(value string, maxChars int) string {
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	return value[:maxChars] + "\n...[truncated]"
}

// isDigitOnly 是否为纯数字
func isDigitOnly(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// htmlUnescape HTML 反转义
func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&#x27;", "'")
	s = strings.ReplaceAll(s, "&#x2F;", "/")
	return s
}

// min 返回两个整数中的较小值
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getNestedValue 从嵌套 map 中获取值（支持点号路径如 "data.webPages.value"）
func getNestedValue(data map[string]any, path string) any {
	keys := strings.Split(path, ".")
	var current any = data
	for _, key := range keys {
		if m, ok := current.(map[string]any); ok {
			current = m[key]
		} else {
			return nil
		}
	}
	return current
}

// engineDisplayName 获取引擎显示名称
// 对齐 Python: WebFreeSearchTool._engine_display_name() (web_tools.py L1028-1035)
func engineDisplayName(engine string) string {
	mapping := map[string]string{
		"duckduckgo":      "DuckDuckGo",
		"duckduckgo-jina": "DuckDuckGo (via jina.ai)",
		"bing":            "Bing",
	}
	if name, ok := mapping[engine]; ok {
		return name
	}
	return engine
}

// formatSearchResult 格式化搜索结果为文本
// 对齐 Python: WebFreeSearchTool.invoke 中的格式化逻辑 (web_tools.py L1058-1083)
func formatSearchResult(engine string, query string, rows []searchRow) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("Free search results (%s) for: %s", engineDisplayName(engine), query))
	for idx, row := range rows {
		lines = append(lines, fmt.Sprintf("%d. %s", idx+1, row.Title))
		lines = append(lines, fmt.Sprintf("   URL: %s", row.URL))
		if row.Snippet != "" {
			lines = append(lines, fmt.Sprintf("   Snippet: %s", row.Snippet))
		}
	}

	// 对齐 Python: L1065-1082 — 推荐抓取 URL
	var topFetchURLs []string
	for _, row := range rows {
		if row.URL != "" {
			topFetchURLs = append(topFetchURLs, row.URL)
			if len(topFetchURLs) >= 3 {
				break
			}
		}
	}
	lines = append(lines, "")
	lines = append(lines, "Required next step: before reformulating the query, fetch at least 2 relevant URLs from the top results. If the first fetch fails, is a dynamic shell page, or is still incomplete, continue with the next recommended URLs instead of searching again.")
	if len(topFetchURLs) > 0 {
		lines = append(lines, "Recommended fetch targets:")
		for idx, u := range topFetchURLs {
			lines = append(lines, fmt.Sprintf("%d. %s", idx+1, u))
		}
	}
	return strings.Join(lines, "\n")
}
