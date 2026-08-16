package web_tools

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	xhtml "golang.org/x/net/html"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FreeSearchInput free_search 工具的输入参数
// 对齐 Python: WebFreeSearchTool.invoke inputs (web_tools.py L1039-1041)
type FreeSearchInput struct {
	// Query 搜索查询文本
	Query string `json:"query"`
	// MaxResults 最大结果数
	MaxResults int `json:"max_results"`
	// TimeoutSeconds 请求超时时间
	TimeoutSeconds int `json:"timeout_seconds"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ddgLinkRe DDG 结果链接正则
	// 对齐 Python: re.findall(r'<a[^>]+class="result__a"...>', ...) (web_tools.py L691-694)
	ddgLinkRe = regexp.MustCompile(`(?i)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	// ddgSnippetRe DDG 结果摘要正则
	// 对齐 Python: re.findall(r'<a[^>]+class="result__snippet"...>', ...) (web_tools.py L696-700)
	ddgSnippetRe = regexp.MustCompile(`(?is)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>|<div[^>]+class="result__snippet"[^>]*>(.*?)</div>`)
	// jinaLinkRe Jina 代理结果链接正则
	// 对齐 Python: re.findall(r"\[([^\]\n]+)\]\((https?://[^\s)]+)\)", ...) (web_tools.py L725)
	jinaLinkRe = regexp.MustCompile(`(?i)\[([^\]\n]+)\]\((https?://[^\s)]+)\)`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWebFreeSearchTool 创建免费搜索工具
// 对齐 Python: WebFreeSearchTool.__init__ + invoke (web_tools.py L645-1088)
func NewWebFreeSearchTool(language, agentID string) tool.Tool {
	card, _ := hprompts.BuildToolCard("free_search", "WebFreeSearchTool", language, nil, agentID)

	fn := func(ctx context.Context, input FreeSearchInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: WebFreeSearchTool.invoke (web_tools.py L1037-1088)
		query := strings.TrimSpace(input.Query)
		maxResults := input.MaxResults
		timeoutSeconds := input.TimeoutSeconds

		if query == "" {
			return map[string]any{"result": "[ERROR]: query cannot be empty."}, nil
		}

		// 对齐 Python: L1046-1047
		if maxResults <= 0 {
			maxResults = 8
		}
		maxResults = max(1, min(maxResults, 20))
		if timeoutSeconds <= 0 {
			timeoutSeconds = 20
		}
		timeoutSeconds = max(5, min(timeoutSeconds, 60))

		// 对齐 Python: L1048-1053 — search_free
		engineUsed, rows, err := searchFree(query, maxResults, timeoutSeconds)
		if err != nil {
			logger.Error(logComponent).Str("query", query).Err(err).Msg("免费搜索失败")
			return map[string]any{"result": fmt.Sprintf("[ERROR]: free search failed: %s", err)}, nil
		}

		if len(rows) == 0 {
			return map[string]any{"result": fmt.Sprintf("No search results for: %s", query)}, nil
		}

		// 对齐 Python: L1058-1083 — 格式化输出
		result := formatSearchResult(engineUsed, query, rows)
		return map[string]any{"result": result}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// searchDuckDuckGo 搜索 DuckDuckGo HTML 端点
// 对齐 Python: WebFreeSearchTool._search_duckduckgo_sync() (web_tools.py L672-715)
func searchDuckDuckGo(query string, maxResults, timeoutSeconds int) ([]searchRow, error) {
	searchURL := duckduckgoSearchURL(query)
	resp, err := httpRequest("GET", searchURL,
		withHeaders(searchRequestHeaders(query)),
		withTimeout(timeoutSeconds),
	)
	if err != nil {
		return nil, fmt.Errorf("DuckDuckGo 搜索失败: %w", err)
	}

	// 对齐 Python: L676-688 — 反爬检测
	if isDDGChallengePage(resp.statusCode, resp.text) {
		return nil, fmt.Errorf("duckduckgo: 反爬挑战页面返回")
	}
	if resp.statusCode != 200 {
		return nil, fmt.Errorf("duckduckgo: 非 200 状态码: %d", resp.statusCode)
	}

	html := resp.text
	// 对齐 Python: L691-700 — 正则提取
	links := ddgLinkRe.FindAllStringSubmatch(html, maxResults)
	snippets := ddgSnippetRe.FindAllStringSubmatch(html, -1)

	var rows []searchRow
	for i, match := range links {
		if i >= maxResults {
			break
		}
		href := match[1]
		titleRaw := match[2]
		// 对齐 Python: L704-706 — 摘要提取
		snippetRaw := ""
		if i < len(snippets) {
			snippetRaw = snippets[i][1]
			if snippetRaw == "" {
				snippetRaw = snippets[i][2]
			}
		}
		title := stripTags(titleRaw)
		if title == "" {
			title = fmt.Sprintf("Result %d", i+1)
		}
		rows = append(rows, searchRow{
			Title:   title,
			URL:     decodeDDGRedirect(href),
			Snippet: stripTags(snippetRaw),
			Source:  "duckduckgo",
		})
	}
	return rows, nil
}

// searchDuckDuckGoViaJina 通过 Jina 代理搜索 DuckDuckGo
// 对齐 Python: WebFreeSearchTool._search_duckduckgo_via_jina_sync() (web_tools.py L718-745)
func searchDuckDuckGoViaJina(query string, maxResults, timeoutSeconds int) ([]searchRow, error) {
	// 对齐 Python: L720
	jinaURL := fmt.Sprintf("https://r.jina.ai/http://duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	resp, err := httpRequest("GET", jinaURL,
		withHeaders(searchRequestHeaders(query)),
		withTimeout(timeoutSeconds),
	)
	if err != nil {
		return nil, fmt.Errorf("DuckDuckGo-Jina 搜索失败: %w", err)
	}
	if err := raiseForStatusWithBody(resp); err != nil {
		return nil, err
	}

	// 对齐 Python: L725 — markdown 链接正则
	text := resp.text
	matches := jinaLinkRe.FindAllStringSubmatch(text, -1)

	var rows []searchRow
	seen := map[string]bool{}
	for _, match := range matches {
		titleRaw := match[1]
		href := match[2]
		title := stripTags(titleRaw)
		// 对齐 Python: L731-732
		if title == "" || strings.HasPrefix(title, "Image ") {
			continue
		}
		decoded := decodeDDGRedirect(href)
		parsed, err := url.Parse(decoded)
		if err != nil || !strings.HasPrefix(parsed.Scheme, "http") {
			continue
		}
		// 对齐 Python: L737-738
		if strings.Contains(strings.ToLower(parsed.Host), "duckduckgo.com") {
			continue
		}
		if seen[decoded] {
			continue
		}
		seen[decoded] = true
		rows = append(rows, searchRow{
			Title:  title,
			URL:    decoded,
			Source: "duckduckgo-jina",
		})
		if len(rows) >= maxResults {
			break
		}
	}
	return rows, nil
}

// searchBing 搜索 Bing
// 对齐 Python: WebFreeSearchTool._search_bing_sync() (web_tools.py L748-827)
func searchBing(query string, maxResults, timeoutSeconds int, debugRunID ...string) ([]searchRow, error) {
	// 对齐 Python: L757-759 — URL 构建
	var bingURL string
	if containsCJK(query) {
		bingURL = fmt.Sprintf("https://www.bing.com/search?q=%s&setlang=zh-Hans&mkt=zh-CN&cc=CN", url.QueryEscape(query))
	} else {
		bingURL = fmt.Sprintf("https://www.bing.com/search?q=%s&setlang=en-US&mkt=en-US&cc=US", url.QueryEscape(query))
	}

	resp, err := httpRequest("GET", bingURL,
		withHeaders(searchRequestHeaders(query)),
		withTimeout(timeoutSeconds),
	)
	if err != nil {
		return nil, fmt.Errorf("bing 搜索失败: %w", err)
	}
	if err := raiseForStatusWithBody(resp); err != nil {
		return nil, err
	}

	html := resp.text
	soup := parseHTML(html)

	// 对齐 Python: L767-778 — 调试
	runID := ""
	if len(debugRunID) > 0 {
		runID = debugRunID[0]
	}
	writeDebugPayload(runID, "bing", "raw", map[string]any{
		"query": query, "request_url": bingURL,
		"status_code": resp.statusCode, "raw_html": html,
	})

	var rows []searchRow
	seen := map[string]bool{}

	// 对齐 Python: L783-787 — 提取 answer cards
	answerRows := extractBingAnswerCards(soup, bingURL)
	for _, row := range answerRows {
		if row.URL != "" && !seen[row.URL] {
			seen[row.URL] = true
		}
		rows = append(rows, row)
	}

	// 对齐 Python: L789-813 — 提取有机结果 li.b_algo
	resultsArea := extractBingResultsArea(soup)
	resultsArea.Find("li.b_algo").Each(func(i int, s *goquery.Selection) {
		anchor := s.Find("h2 a[href]")
		if anchor.Length() == 0 {
			return
		}
		hrefRaw, _ := anchor.Attr("href")
		href := decodeBingRedirect(xhtml.UnescapeString(hrefRaw))
		title := strings.TrimSpace(anchor.Text())
		if href == "" || seen[href] {
			return
		}
		seen[href] = true

		// 对齐 Python: L799-804
		captionNode := s.Find(".b_caption p")
		if captionNode.Length() == 0 {
			captionNode = s.Find("p")
		}
		snippet := strings.TrimSpace(captionNode.Text())

		originNode := s.Find(".tptt")
		if originNode.Length() == 0 {
			originNode = s.Find(".source")
		}
		if originNode.Length() == 0 {
			originNode = s.Find("cite")
		}
		origin := strings.TrimSpace(originNode.Text())

		dateNode := s.Find(".news_dt")
		date := strings.TrimSpace(dateNode.Text())

		if title == "" {
			title = fmt.Sprintf("Result %d", len(rows)+1)
		}
		rows = append(rows, buildBingRow(href, title, snippet, origin, date, "bing-web"))
	})

	if len(rows) > maxResults {
		rows = rows[:maxResults]
	}

	// 对齐 Python: L816-826 — 调试
	writeDebugPayload(runID, "bing", "parsed", map[string]any{
		"query": query, "request_url": bingURL, "rows": rows,
		"row_summary": summarizeRows(rows),
	})

	return rows, nil
}

// searchFree 多引擎免费搜索（带降级）
// 对齐 Python: WebFreeSearchTool._search_free_sync() (web_tools.py L901-1025)
func searchFree(query string, maxResults, timeoutSeconds int) (string, []searchRow, error) {
	var errors []string
	debugRunID := newDebugRunID()
	bestEngine := ""
	var bestRows []searchRow

	// 对齐 Python: L908-917 — 引擎列表构建
	type engineEntry struct {
		name   string
		runner func(string, int, int, ...string) ([]searchRow, error)
	}
	var engines []engineEntry

	if envFlag(freeSearchDDGEnabledEnv, false) {
		engines = append(engines,
			engineEntry{"duckduckgo", func(q string, mr, ts int, _ ...string) ([]searchRow, error) {
				return searchDuckDuckGo(q, mr, ts)
			}},
			engineEntry{"duckduckgo-jina", func(q string, mr, ts int, _ ...string) ([]searchRow, error) {
				return searchDuckDuckGoViaJina(q, mr, ts)
			}},
		)
	}
	if envFlag(freeSearchBingEnabledEnv, false) {
		engines = append(engines, engineEntry{"bing", searchBing})
	}

	// 对齐 Python: L919-923
	if len(engines) == 0 {
		return "", nil, fmt.Errorf("所有免费搜索引擎已禁用")
	}

	// 对齐 Python: L925-1003 — 逐引擎尝试
	for _, engine := range engines {
		rows, err := engine.runner(query, maxResults, timeoutSeconds, debugRunID)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %s", engine.name, err))
			writeDebugPayload(debugRunID, engine.name, "error", map[string]any{
				"query": query, "error": err.Error(),
			})
			continue
		}

		// 对齐 Python: L946-947
		filteredRows := filterRankedRows(query, rows)
		if len(filteredRows) > maxResults {
			filteredRows = filteredRows[:maxResults]
		}

		writeDebugPayload(debugRunID, engine.name, "candidate", map[string]any{
			"query": query, "row_summary": summarizeRows(filteredRows), "rows": filteredRows,
		})

		writeDebugPayload(debugRunID, engine.name, "filtered", map[string]any{
			"query": query, "row_summary": summarizeRows(filteredRows),
			"rows": scoredRows(query, filteredRows),
		})

		// 对齐 Python: L981-983
		if len(filteredRows) > 0 && len(bestRows) == 0 {
			bestEngine = engine.name
			bestRows = filteredRows
		}

		// 对齐 Python: L985-998
		if rowsAreUsable(query, filteredRows) {
			writeDebugPayload(debugRunID, engine.name, "decision", map[string]any{
				"query": query, "decision": "accepted", "row_summary": summarizeRows(filteredRows),
			})
			return engine.name, filteredRows, nil
		}

		// 对齐 Python: L1000-1003
		if len(filteredRows) > 0 && len(bestRows) == 0 {
			bestEngine = engine.name
			bestRows = filteredRows
		}
		errors = append(errors, fmt.Sprintf("%s: low-quality or empty result", engine.name))
	}

	// 对齐 Python: L1005-1017
	if len(bestRows) > 0 {
		writeDebugPayload(debugRunID, bestEngine, "decision", map[string]any{
			"query": query, "decision": "best_effort", "row_summary": summarizeRows(bestRows),
		})
		return bestEngine, bestRows, nil
	}

	// 对齐 Python: L1019-1025
	writeDebugPayload(debugRunID, "final", "decision", map[string]any{
		"query": query, "decision": "error", "errors": strings.Join(errors, " | "),
	})
	return "", nil, fmt.Errorf("所有搜索引擎均失败: %s", strings.Join(errors, " | "))
}

// isDDGChallengePage 检测 DDG 反爬页面
// 对齐 Python: WebFreeSearchTool._is_ddg_challenge_page() (web_tools.py L659-669)
func isDDGChallengePage(statusCode int, html string) bool {
	if statusCode == 202 || statusCode == 418 || statusCode == 429 || statusCode == 503 {
		return true
	}
	lowered := strings.ToLower(html)
	markers := []string{"/anomaly.js", "challenge-form", "duckduckgo.com/anomaly.js"}
	for _, marker := range markers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// filterRankedRows 过滤和排序搜索结果
// 对齐 Python: WebFreeSearchTool._filter_ranked_rows() (web_tools.py L856-872)
func filterRankedRows(query string, rows []searchRow) []searchRow {
	var preferred, deferred []searchRow
	seenURLs := map[string]bool{}
	for _, row := range rows {
		if row.URL == "" || strings.TrimSpace(row.Title) == "" || seenURLs[row.URL] {
			continue
		}
		seenURLs[row.URL] = true
		if isLowFetchValueURL(row.URL) {
			deferred = append(deferred, row)
		} else {
			preferred = append(preferred, row)
		}
	}
	return append(preferred, deferred...)
}

// rowsAreUsable 判断搜索结果是否可用
// 对齐 Python: WebFreeSearchTool._rows_are_usable() (web_tools.py L875-892)
func rowsAreUsable(query string, rows []searchRow) bool {
	if len(rows) == 0 {
		return false
	}
	timely := isTimelyQuery(query)
	topRows := rows
	if len(topRows) > 3 {
		topRows = topRows[:3]
	}
	if !timely {
		return true
	}
	for _, row := range topRows {
		if row.Source == "bing-card" {
			return true
		}
		if !isLowConfidenceResultDomain(row.URL) {
			if matchTermCount(query, row) >= 1 {
				return true
			}
			if row.Date != "" || row.Origin != "" {
				return true
			}
		}
	}
	return false
}

// extractBingResultsArea 提取 Bing 搜索结果区域
// 对齐 Python: _extract_bing_results_area() (web_tools.py L587-589)
func extractBingResultsArea(soup *goquery.Document) *goquery.Selection {
	main := soup.Find(`main[aria-label="Search Results"]`)
	if main.Length() > 0 {
		return main
	}
	return soup.Find("body")
}

// extractBingAnswerCards 提取 Bing 答案卡片
// 对齐 Python: _extract_bing_answer_cards() (web_tools.py L592-642)
func extractBingAnswerCards(soup *goquery.Document, fallbackURL string) []searchRow {
	var rows []searchRow
	seenTitles := map[string]bool{}

	// 对齐 Python: L596-611 — 选择器列表
	selectors := []string{
		"#b_context .b_ans", "#b_context .b_entityTP", "#b_context .b_card",
		"#b_context .b_rich", "#b_topw .b_ans", "#b_topw .b_entityTP",
		"#b_topw .b_card", "#b_topw .b_rich", ".b_ans", ".b_rich",
		".b_entityTP", ".b_vList", ".b_tpcn", ".b_card",
	}
	selector := strings.Join(selectors, ", ")

	soup.Find(selector).Each(func(i int, s *goquery.Selection) {
		// 对齐 Python: L613-620 — href 提取
		anchor := s.Find("a[href]")
		var href string
		if anchor.Length() > 0 {
			hrefRaw, _ := anchor.Attr("href")
			href = decodeBingRedirect(xhtml.UnescapeString(hrefRaw))
			parsed, err := url.Parse(href)
			if err != nil || !strings.HasPrefix(parsed.Scheme, "http") || strings.Contains(strings.ToLower(parsed.Host), "bing.com") {
				href = ""
			}
		}

		// 对齐 Python: L621-628 — 标题提取
		var titleParts []string
		titleSelectors := []string{".b_focusLabel", ".b_focusTextLarge", ".b_focusTextMedium", ".b_focusTextSmall", "h2", "h3"}
		for _, sel := range titleSelectors {
			s.Find(sel).Each(func(_ int, selNode *goquery.Selection) {
				text := strings.TrimSpace(selNode.Text())
				if text != "" {
					titleParts = append(titleParts, text)
				}
			})
			if len(titleParts) > 0 {
				break
			}
		}
		title := strings.Join(titleParts, " ")
		if len(title) < 4 {
			return
		}

		// 对齐 Python: L632-635 — 摘要
		caption := s.Find(".b_caption p")
		if caption.Length() == 0 {
			caption = s.Find("p")
		}
		snippet := strings.TrimSpace(caption.Text())

		// 对齐 Python: L636-641 — 去重
		rowURL := href
		if rowURL == "" {
			rowURL = fallbackURL
		}
		dedupeKey := title + "|" + rowURL
		if seenTitles[dedupeKey] {
			return
		}
		seenTitles[dedupeKey] = true

		rows = append(rows, buildBingRow(rowURL, title, snippet, "", "", "bing-card"))
	})

	return rows
}

// scoredRows 为行添加评分信息（用于调试）
// 对齐 Python: L969-976 — filtered 调试输出中的 score 字段
func scoredRows(query string, rows []searchRow) []map[string]any {
	var result []map[string]any
	for _, row := range rows {
		result = append(result, map[string]any{
			"title":       row.Title,
			"url":         row.URL,
			"snippet":     row.Snippet,
			"origin":      row.Origin,
			"date":        row.Date,
			"source":      row.Source,
			"score":       scoreRow(query, row),
			"match_count": matchTermCount(query, row),
		})
	}
	return result
}
