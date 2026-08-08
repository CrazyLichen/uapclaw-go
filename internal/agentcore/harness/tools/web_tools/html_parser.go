package web_tools

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// stripTagsRe HTML 标签清理正则
	stripTagsRe = regexp.MustCompile(`<[^>]+>`)
	// multiSpaceRe 多空白压缩正则
	multiSpaceRe = regexp.MustCompile(`\s+`)
	// multiNewlineRe3 多换行压缩正则
	multiNewlineRe3 = regexp.MustCompile(`\n{3,}`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseHTML 使用 goquery 解析 HTML
// 对齐 Python: _parse_html() (web_tools.py L295-302)
func parseHTML(htmlStr string) *goquery.Document {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		// 降级：直接返回空文档
		doc, _ = goquery.NewDocumentFromReader(strings.NewReader(""))
	}
	return doc
}

// stripTags 移除 HTML 标签并规范化空白
// 对齐 Python: _strip_tags() (web_tools.py L219-222)
func stripTags(value string) string {
	value = stripTagsRe.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = multiSpaceRe.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

// extractMainTextFromHTML 从 HTML 中提取正文和标题
// 对齐 Python: WebFetchWebpageTool._extract_main_text_from_html() (web_tools.py L1483-1552)
func extractMainTextFromHTML(text string) (title, content string) {
	soup := parseHTML(text)

	// 提取标题
	// 对齐 Python: L1486-1487
	title = strings.TrimSpace(soup.Find("title").Text())

	// 移除不需要的标签
	// 对齐 Python: L1489-1491
	removeSelectors := []string{"script", "style", "noscript", "svg", "canvas", "iframe"}
	for _, sel := range removeSelectors {
		soup.Find(sel).Remove()
	}

	// 移除导航/装饰性标签
	// 对齐 Python: L1493-1515
	decorSelectors := []string{
		"nav", "header", "footer", "aside", "form", "button",
		"[role='navigation']", ".nav", ".navbar", ".header",
		".footer", ".sidebar", ".aside", ".recommend", ".related",
		".share", ".breadcrumb", ".menu", ".toolbar",
	}
	for _, sel := range decorSelectors {
		soup.Find(sel).Remove()
	}

	// 候选正文选择器
	// 对齐 Python: L1517-1532
	candidateSelectors := []string{
		"main", "[role='main']", "article", ".article",
		".article-content", ".article-body", ".post", ".post-content",
		".entry-content", ".content", ".detail", ".news", "#content", "#main",
	}

	bestText := ""
	for _, sel := range candidateSelectors {
		node := soup.Find(sel)
		if node.Length() == 0 {
			continue
		}
		nodeText := strings.TrimSpace(node.Text())
		if len(nodeText) > len(bestText) {
			bestText = nodeText
		}
	}

	// 降级：从 body 提取段落
	// 对齐 Python: L1542-1549
	if bestText == "" {
		body := soup.Find("body")
		if body.Length() == 0 {
			body = soup.Selection
		}
		var blocks []string
		body.Find("p, li, h1, h2, h3").Each(func(i int, s *goquery.Selection) {
			piece := strings.TrimSpace(s.Text())
			if len(piece) >= 20 {
				blocks = append(blocks, piece)
			}
		})
		if len(blocks) > 0 {
			bestText = strings.Join(blocks, "\n")
		} else {
			bestText = strings.TrimSpace(body.Text())
		}
	}

	// 多换行压缩
	// 对齐 Python: L1551
	bestText = multiNewlineRe3.ReplaceAllString(strings.TrimSpace(bestText), "\n\n")

	return title, bestText
}
