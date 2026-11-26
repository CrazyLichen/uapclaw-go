// HTML 压缩器（报告页检测 + 噪声移除 + 主内容密度选取 + Markdown 渲染）。
// 对应 Python 代码：offloader/rule_compression/compressors/html_compressor.py（572 行）
//
// 偏差 D-24.15-b4：Python trafilatura 抽取半区（HTMLExtractor，html_compressor.py:55-122）
// 依赖 Python-only 的 trafilatura/BeautifulSoup 库，Go 无对应物——compress 直接走
// Python 的 beautifulsoup-fallback 分支等价实现（golang.org/x/net/html），
// 并保留 _looks_like_report_html/:report 路径语义。回收条件=引入 Go 可读性抽取库时补齐。
package rulecompression

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ──────────────────────────── 常量 ────────────────────────────

// mainSelectors 主内容候选选择器（首个文本量达标的）。
// Python: html_compressor.py:125 (_MAIN_SELECTORS)
var mainSelectors = []string{
	"article", "main", `[role="main"]`, ".article-body", ".post-content",
	".entry-content", ".post-body", "#content", ".main-content",
}

// noiseTags 噪声标签。
// Python: html_compressor.py:136 (_NOISE_TAGS)
var noiseTags = map[string]bool{
	"script": true, "style": true, "nav": true, "footer": true, "aside": true,
	"form": true, "noscript": true, "template": true, "svg": true,
}

// noiseNameRe 噪声类名/id 模式。
// Python: html_compressor.py:137 (_NOISE_NAME_RE)
var noiseNameRe = regexp.MustCompile(`(?i)(^|[-_])(ad|ads|advert|banner|breadcrumb|cookie|footer|header|menu|nav|newsletter|promo|related|share|sidebar|social|sponsor|toolbar)([-_]|$)`)

// hiddenStyleRe 隐藏样式。
// Python: html_compressor.py:140 (_HIDDEN_STYLE_RE)
var hiddenStyleRe = regexp.MustCompile(`(?i)(?:display\s*:\s*none|visibility\s*:\s*hidden)`)

// htmlContentStartRe HTML 内容起始行。
// Python: html_compressor.py:142 (_HTML_CONTENT_START_RE)
var htmlContentStartRe = regexp.MustCompile(`(?i)^\s*</?(?:article|main|section|div|table|thead|tbody|tr|th|td|ul|ol|li|p|pre|code|strong|span|h[1-6])\b`)

// cssFragmentLineRe CSS 片段行。
// Python: html_compressor.py:145 (_CSS_FRAGMENT_LINE_RE)
var cssFragmentLineRe = regexp.MustCompile(`^\s*(?:[.#:@\w-][^<>]*\{|[-\w]+\s*:\s*[^;]+;|}|/\*|\*/)`)

// blockTags 块级标签集合。
// Python: html_compressor.py:148 (_BLOCK_TAGS)
var blockTags = map[string]bool{
	"article": true, "blockquote": true, "div": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "main": true, "ol": true,
	"p": true, "pre": true, "section": true, "table": true, "ul": true,
}

// reportRowKeepRe 报告行保留模式。
// Python: html_compressor.py:241 (_REPORT_ROW_KEEP_RE)
var reportRowKeepRe = regexp.MustCompile(`(?i)\b(fail(?:ed|ure)?|error|broken|skipped|xfailed|xpassed|rerun|unexpected|assertionerror|traceback)\b`)

// reportRowDropRe 报告行丢弃模式。
// Python: html_compressor.py:244 (_REPORT_ROW_DROP_RE)
var reportRowDropRe = regexp.MustCompile(`(?i)\b(pass(?:ed)?|ok)\b`)

// whitespaceRe 多空白归一。
var whitespaceRe = regexp.MustCompile(`\s+`)

// plainTextStripRe Markdown 记号剥离。
var plainTextStripRe = regexp.MustCompile(`[#>*_` + "`" + `\[\]()|\-]`)

// ──────────────────────────── 结构体 ────────────────────────────

// HTMLExtractorConfig 抽取器配置（保留 Python 字段面，trafilatura 半区偏差 b4）。
// Python: html_compressor.py:18 (HTMLExtractorConfig)
type HTMLExtractorConfig struct {
	OutputFormat      string
	IncludeLinks      bool
	IncludeImages     bool
	IncludeTables     bool
	IncludeComments   bool
	IncludeFormatting bool
	FavorPrecision    bool
	FavorRecall       bool
	ExtractMetadata   bool
}

// NewHTMLExtractorConfig 创建配置，字段使用 Python 默认值。
func NewHTMLExtractorConfig() *HTMLExtractorConfig {
	return &HTMLExtractorConfig{
		OutputFormat: "markdown", IncludeLinks: true, IncludeImages: false,
		IncludeTables: true, IncludeComments: false, IncludeFormatting: true,
		FavorPrecision: false, FavorRecall: true, ExtractMetadata: true,
	}
}

// HtmlCompressor HTML 压缩器。
// Python: html_compressor.py:156 (HtmlCompressor)
type HtmlCompressor struct {
	ExtractorConfig *HTMLExtractorConfig
}

// Compress 入口（报告页走 :report 路径；其余走通用 fallback；trafilatura 半区见 b4）。
// Python: html_compressor.py:162 (compress)
func (c HtmlCompressor) Compress(content string, ctx *RuleContext) RuleCompressionResult {
	label := "beautifulsoup"
	isReport := looksLikeReportHTML(content)
	if isReport {
		label = "beautifulsoup:report"
	}
	result := compressHTMLFallback(content, ctx, label)
	if !result.Modified {
		// Python：trafilatura 抽出空/过短且非报告页 → unchanged；此处同型
		return result
	}
	return result
}

// compressHTMLFallback BeautifulSoup-fallback 等价路径。
// Python: html_compressor.py:211 (_compress_with_beautifulsoup_fallback)
func compressHTMLFallback(content string, ctx *RuleContext, extractorLabel string) RuleCompressionResult {
	doc, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return unchangedHTML(content)
	}
	title := extractTitle(doc)
	isReport := strings.HasSuffix(extractorLabel, ":report")
	removedNodeCount := 0
	if isReport {
		removedNodeCount = removeReportNoise(doc)
	} else {
		removedNodeCount = removeNoise(doc)
	}
	reportRowsRemoved := 0
	if isReport {
		reportRowsRemoved = pruneReportResultRows(doc)
	}
	main, source := findMainContent(doc, ctx.HTMLMinContentChars, isReport)
	if main == nil {
		return unchangedHTML(content)
	}

	blocks := renderContainer(main)
	if title != "" && !containsTitle(blocks, title) {
		blocks = append([]string{"# " + title}, blocks...)
	}
	blocks, duplicateCount := deduplicateBlocks(blocks)
	candidate := strings.TrimSpace(strings.Join(blocks, "\n\n"))
	if len(plainText(candidate)) < ctx.HTMLMinContentChars {
		return unchangedHTML(content)
	}

	linkCount, tableCount, codeBlockCount := countDescendants(main)
	details := map[string]any{
		"extractor":             extractorLabel,
		"main_content_source":   source,
		"removed_node_count":    removedNodeCount,
		"report_rows_removed":   reportRowsRemoved,
		"duplicate_block_count": duplicateCount,
		"link_count":            linkCount,
		"table_count":           tableCount,
		"code_block_count":      codeBlockCount,
		"output_block_count":    len(blocks),
	}
	if candidate != content && MeetsSavingsRatio(content, candidate, ctx) {
		return RuleCompressionResult{Content: candidate, ContentType: ContentTypeHTML, Modified: true, Lossy: true, Details: details}
	}
	return RuleCompressionResult{Content: content, ContentType: ContentTypeHTML, Modified: false, Lossy: false, Details: details}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// unchangedHTML 未修改结果。
// Python: html_compressor.py:565 (_unchanged)
func unchangedHTML(content string) RuleCompressionResult {
	return RuleCompressionResult{Content: content, ContentType: ContentTypeHTML, Modified: false}
}

// looksLikeReportHTML 报告页（pytest-html 等）判定。
// Python: html_compressor.py:209 (_looks_like_report_html)
func looksLikeReportHTML(content string) bool {
	lowered := strings.ToLower(content)
	if len(lowered) > 20000 {
		lowered = lowered[:20000]
	}
	reportMarkers := []string{
		"pytest-html", "results-table", "environment", "test results",
		"tests took", "report generated",
	}
	if strings.Contains(lowered, "pytest-html") || strings.Contains(lowered, "results-table") {
		return true
	}
	tableCount := strings.Count(lowered, "<table")
	markerHits := 0
	for _, marker := range reportMarkers {
		if strings.Contains(lowered, marker) {
			markerHits++
		}
	}
	return tableCount >= 2 && markerHits >= 2
}

// extractTitle og:title → title 兜底。
// Python: html_compressor.py:274 (_extract_title)
func extractTitle(doc *html.Node) string {
	var found string
	var walkTitle func(*html.Node)
	walkTitle = func(n *html.Node) {
		if found != "" || n.Type != html.ElementNode {
			if n.FirstChild != nil && found == "" {
				walkTitle(n.FirstChild)
			}
			if n.NextSibling != nil && found == "" {
				walkTitle(n.NextSibling)
			}
			return
		}
		if n.DataAtom == atom.Meta {
			var prop, content string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "property":
					prop = attr.Val
				case "content":
					content = attr.Val
				}
			}
			if prop == "og:title" && strings.TrimSpace(content) != "" {
				found = normalizeText(content)
				return
			}
		}
		if n.DataAtom == atom.Title {
			found = normalizeText(nodeText(n, " "))
			return
		}
		if n.FirstChild != nil {
			walkTitle(n.FirstChild)
		}
		if n.NextSibling != nil && found == "" {
			walkTitle(n.NextSibling)
		}
	}
	walkTitle(doc)
	return found
}

// removeNoise 噪声节点移除（标签 + 隐藏/噪声类名）。
// Python: html_compressor.py:283 (_remove_noise)
func removeNoise(doc *html.Node) int {
	removed := 0
	var removeIf func(n *html.Node, pred func(*html.Node) bool)
	removeIf = func(n *html.Node, pred func(*html.Node) bool) {
		if n.Type == html.ElementNode && pred(n) {
			removed++
			// 标记删除：置注释型空节点不可行，改为摘除子树内容
			n.Attr = nil
			detachChildren(n)
			return
		}
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			removeIf(child, pred)
			child = next
		}
	}
	removeIf(doc, func(n *html.Node) bool {
		if noiseTags[n.DataAtom.String()] {
			return true
		}
		classes, id, style := nodeClassIDStyle(n)
		hidden := hasAttr(n, "hidden") ||
			strings.EqualFold(attrValue(n, "aria-hidden"), "true") ||
			hiddenStyleRe.MatchString(style)
		return hidden || noiseNameRe.MatchString(classes) || noiseNameRe.MatchString(id)
	})
	return removed
}

// removeReportNoise 报告页仅移除标签噪声。
// Python: html_compressor.py:299 (_remove_report_noise)
func removeReportNoise(doc *html.Node) int {
	removed := 0
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && noiseTags[n.DataAtom.String()] {
			removed++
			detachChildren(n)
			return
		}
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			walk(child)
			child = next
		}
	}
	walk(doc)
	return removed
}

// pruneReportResultRows 报告结果行裁剪（保留 fail/error 等行，剔除纯 pass 行）。
// Python: html_compressor.py:247 (_prune_report_result_rows)
func pruneReportResultRows(doc *html.Node) int {
	var dataRows []*html.Node
	walkRows(doc, func(row *html.Node) {
		if hasDirectCell(row) {
			dataRows = append(dataRows, row)
		}
	})
	anyKeep := false
	for _, row := range dataRows {
		if reportRowKeepRe.MatchString(nodeText(row, " ")) {
			anyKeep = true
			break
		}
	}
	if !anyKeep {
		return 0
	}
	removed := 0
	for _, row := range dataRows {
		text := normalizeText(nodeText(row, " "))
		if reportRowKeepRe.MatchString(text) {
			continue
		}
		if reportRowDropRe.MatchString(text) || looksLikeTestResultRow(text) {
			detachChildren(row)
			removed++
		}
	}
	return removed
}

// looksLikeTestResultRow 测试结果行形态。
// Python: html_compressor.py:267 (_looks_like_test_result_row)
func looksLikeTestResultRow(text string) bool {
	return strings.Contains(text, "::test") || strings.HasPrefix(text, "tests/")
}

// findMainContent 主内容定位（选择器 → 密度评分）。
// Python: html_compressor.py:272 (_find_main_content)
func findMainContent(doc *html.Node, minChars int, isReport bool) (*html.Node, string) {
	if isReport {
		if body := findFirst(doc, atom.Body); body != nil {
			return body, "body:report"
		}
		return nil, "none"
	}
	for _, selector := range mainSelectors {
		node := selectOne(doc, selector)
		if node != nil && textLength(node) >= minChars {
			return node, selector
		}
	}
	body := findFirst(doc, atom.Body)
	if body == nil {
		return nil, "none"
	}
	var candidates []*html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Article, atom.Main, atom.Section, atom.Div:
				if textLength(n) >= minChars {
					candidates = append(candidates, n)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(body)
	if len(candidates) == 0 {
		if textLength(body) >= minChars {
			return body, "body"
		}
		return nil, "none"
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return contentScore(candidates[i]) > contentScore(candidates[j])
	})
	return candidates[0], "density:" + candidates[0].DataAtom.String()
}

// contentScore 内容密度评分（文本 - 2*链接 + 结构奖励）。
// Python: html_compressor.py:305 (_content_score)
func contentScore(node *html.Node) int {
	textLength := textLength(node)
	linkLength := 0
	structural := 0
	walkNode(node, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		switch n.DataAtom {
		case atom.A:
			linkLength += len(normalizeText(nodeText(n, " ")))
		case atom.P, atom.Pre, atom.Table, atom.Li:
			structural += 30
		}
	})
	return textLength - 2*linkLength + structural
}

// textLength 归一文本长度。
// Python: html_compressor.py:315 (_text_length)
func textLength(node *html.Node) int {
	return len(normalizeText(nodeText(node, " ")))
}

// renderContainer 块级渲染子节点列表。
// Python: html_compressor.py:320 (_render_container)
func renderContainer(node *html.Node) []string {
	var blocks []string
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			if text := normalizeText(child.Data); text != "" {
				blocks = append(blocks, text)
			}
		} else if child.Type == html.ElementNode {
			blocks = append(blocks, renderTag(child)...)
		}
	}
	var kept []string
	for _, block := range blocks {
		if strings.TrimSpace(block) != "" {
			kept = append(kept, block)
		}
	}
	return kept
}

// renderTag 单标签渲染。
// Python: html_compressor.py:334 (_render_tag)
func renderTag(node *html.Node) []string {
	name := node.DataAtom.String()
	switch {
	case regexp.MustCompile(`^h[1-6]$`).MatchString(name):
		text := inlineText(node)
		if text == "" {
			return nil
		}
		level := int(name[1] - '0')
		return []string{strings.Repeat("#", level) + " " + text}
	case name == "p":
		text := inlineText(node)
		if text == "" {
			return nil
		}
		return []string{text}
	case name == "ul" || name == "ol":
		ordered := name == "ol"
		var items []string
		index := 0
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.DataAtom == atom.Li {
				index++
				text := inlineText(child)
				if text != "" {
					if ordered {
						items = append(items, fmt.Sprintf("%d. %s", index, text))
					} else {
						items = append(items, "- "+text)
					}
				}
			}
		}
		if len(items) == 0 {
			return nil
		}
		return []string{strings.Join(items, "\n")}
	case name == "blockquote":
		text := inlineText(node)
		if text == "" {
			return nil
		}
		var quoted []string
		for _, line := range splitLines(text) {
			quoted = append(quoted, "> "+line)
		}
		return []string{strings.Join(quoted, "\n")}
	case name == "pre":
		code := strings.Trim(nodeText(node, "\n"), "\n")
		if strings.TrimSpace(code) == "" {
			return nil
		}
		return []string{"```\n" + code + "\n```"}
	case name == "table":
		table := renderTable(node)
		if table == "" {
			return nil
		}
		return []string{table}
	case name == "hr":
		return []string{"---"}
	case name == "br":
		return nil
	case name == "article" || name == "main" || name == "section" || name == "div" || name == "body":
		return renderContainer(node)
	}
	hasBlockChild := false
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && blockTags[child.DataAtom.String()] {
			hasBlockChild = true
			break
		}
	}
	if hasBlockChild {
		return renderContainer(node)
	}
	text := inlineText(node)
	if text == "" {
		return nil
	}
	return []string{text}
}

// inlineText 行内文本（链接/行内代码/换行）。
// Python: html_compressor.py:384 (_inline_text)
func inlineText(node *html.Node) string {
	var parts []string
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			parts = append(parts, child.Data)
			continue
		}
		if child.Type != html.ElementNode {
			continue
		}
		switch child.DataAtom {
		case atom.A:
			label := normalizeText(nodeText(child, " "))
			href := attrValue(child, "href")
			if label != "" && strings.TrimSpace(href) != "" {
				parts = append(parts, fmt.Sprintf("[%s](%s)", label, href))
			} else {
				parts = append(parts, label)
			}
		case atom.Code:
			if node.DataAtom != atom.Pre {
				code := normalizeText(nodeText(child, " "))
				if code != "" {
					parts = append(parts, "`"+code+"`")
				}
			} else {
				parts = append(parts, inlineText(child))
			}
		case atom.Br:
			parts = append(parts, "\n")
		default:
			parts = append(parts, inlineText(child))
		}
	}
	return normalizeInline(strings.Join(parts, ""))
}

// renderTable Markdown 表格。
// Python: html_compressor.py:413 (_render_table)
func renderTable(table *html.Node) string {
	var rows [][]string
	walkRows(table, func(row *html.Node) {
		var cells []string
		for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
			if cell.Type == html.ElementNode && (cell.DataAtom == atom.Th || cell.DataAtom == atom.Td) {
				cells = append(cells, strings.ReplaceAll(normalizeText(nodeText(cell, " ")), "|", `\|`))
			}
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	})
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}
	normalized := make([][]string, len(rows))
	for i, row := range rows {
		normalized[i] = append(append([]string(nil), row...), make([]string, width-len(row))...)
	}
	lines := []string{
		"| " + strings.Join(normalized[0], " | ") + " |",
		"| " + strings.TrimSuffix(strings.Repeat(" --- |", width), " |") + " |",
	}
	for _, row := range normalized[1:] {
		lines = append(lines, "| "+strings.Join(row, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

// deduplicateBlocks 块去重。
// Python: html_compressor.py:435 (_deduplicate_blocks)
func deduplicateBlocks(blocks []string) ([]string, int) {
	seen := map[string]bool{}
	var kept []string
	duplicates := 0
	for _, block := range blocks {
		key := strings.ToLower(normalizeText(block))
		if key == "" {
			continue
		}
		if seen[key] {
			duplicates++
			continue
		}
		seen[key] = true
		kept = append(kept, strings.TrimSpace(block))
	}
	return kept, duplicates
}

// containsTitle 标题是否已含于块。
// Python: html_compressor.py:450 (_contains_title)
func containsTitle(blocks []string, title string) bool {
	normalized := strings.ToLower(normalizeText(title))
	for _, block := range blocks {
		if strings.ToLower(normalizeText(strings.TrimLeft(block, "# "))) == normalized {
			return true
		}
	}
	return false
}

// normalizeText 多空白归一。
// Python: html_compressor.py:456 (_normalize_text)
func normalizeText(text string) string {
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(text, " "))
}

// normalizeInline 行级归一（空行剔除）。
// Python: html_compressor.py:459 (_normalize_inline)
func normalizeInline(text string) string {
	var lines []string
	for _, line := range splitLines(text) {
		if normalized := normalizeText(line); normalized != "" {
			lines = append(lines, normalized)
		}
	}
	return strings.Join(lines, "\n")
}

// plainText Markdown 记号剥离。
// Python: html_compressor.py:462 (_plain_text)
func plainText(markdown string) string {
	return plainTextStripRe.ReplaceAllString(markdown, " ")
}

// ──────────────────────────── DOM 助手 ────────────────────────────

// walkNode 深度遍历元素节点。
func walkNode(node *html.Node, fn func(*html.Node)) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode {
			fn(child)
		}
		walkNode(child, fn)
	}
}

// findFirst 首个指定元素。
func findFirst(node *html.Node, a atom.Atom) *html.Node {
	if node.Type == html.ElementNode && node.DataAtom == a {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, a); found != nil {
			return found
		}
	}
	return nil
}

// walkRows 遍历所有 tr 行。
func walkRows(node *html.Node, fn func(*html.Node)) {
	if node.Type == html.ElementNode && node.DataAtom == atom.Tr {
		fn(node)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkRows(child, fn)
	}
}

// hasDirectCell 行是否含直接 th/td。
func hasDirectCell(row *html.Node) bool {
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && (child.DataAtom == atom.Td || child.DataAtom == atom.Th) {
			return true
		}
	}
	return false
}

// countDescendants 统计链接/表格/代码块。
func countDescendants(node *html.Node) (int, int, int) {
	links, tables, pres := 0, 0, 0
	walkNode(node, func(n *html.Node) {
		switch n.DataAtom {
		case atom.A:
			links++
		case atom.Table:
			tables++
		case atom.Pre:
			pres++
		}
	})
	return links, tables, pres
}

// attrValue 属性值。
func attrValue(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// hasAttr 是否含属性。
func hasAttr(node *html.Node, key string) bool {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

// nodeClassIDStyle class/id/style 三元组。
func nodeClassIDStyle(node *html.Node) (string, string, string) {
	return attrValue(node, "class"), attrValue(node, "id"), attrValue(node, "style")
}

// nodeText 递归取文本。
func nodeText(node *html.Node, sep string) string {
	var parts []string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			parts = append(parts, n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(parts, sep)
}

// detachChildren 摘除子树（decompose 等价——子节点脱离文档树，GC 回收）。
func detachChildren(node *html.Node) {
	node.FirstChild = nil
	node.LastChild = nil
}

// selectOne 简化选择器（article/main 标签 + [role=main]/.class/#id 三形态）。
func selectOne(doc *html.Node, selector string) *html.Node {
	switch {
	case strings.HasPrefix(selector, "["):
		key := strings.Trim(selector, "[]")
		parts := strings.SplitN(key, "=", 2)
		if len(parts) != 2 {
			return nil
		}
		attrName, want := strings.Trim(parts[0], `"`), strings.Trim(parts[1], `"`)
		return findWithAttr(doc, attrName, want)
	case strings.HasPrefix(selector, "."):
		class := selector[1:]
		return findWithClass(doc, class)
	case strings.HasPrefix(selector, "#"):
		id := selector[1:]
		return findByID(doc, id)
	default:
		return findFirst(doc, atom.Lookup([]byte(selector)))
	}
}

func findWithAttr(node *html.Node, name, want string) *html.Node {
	if node.Type == html.ElementNode && attrValue(node, name) == want {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findWithAttr(child, name, want); found != nil {
			return found
		}
	}
	return nil
}

func findWithClass(node *html.Node, class string) *html.Node {
	if node.Type == html.ElementNode {
		for _, c := range strings.Fields(attrValue(node, "class")) {
			if c == class {
				return node
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findWithClass(child, class); found != nil {
			return found
		}
	}
	return nil
}

func findByID(node *html.Node, id string) *html.Node {
	if node.Type == html.ElementNode && attrValue(node, "id") == id {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findByID(child, id); found != nil {
			return found
		}
	}
	return nil
}
