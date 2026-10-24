// Package parser html_file_parser.go：本地 .htm/.html 文件解析器（13.14）。
//
// 本文件对齐 Python html_file_parser.py 的模块级共享工具
// （_parse_html/_extract_title/_find_main_content/_text_length/_get_text_from_soup）
// 与 HTMLFileParser 本体（Task 4 追加解析器结构体）。
package parser

import (
	"context"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/dlclark/regexp2"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HTMLFileParser 本地 .htm/.html 文件解析器（对齐 Python HTMLFileParser(TxtMdParser)）。
//
// 嵌入 TxtMdParser 复用编码容错读文件（super()._parse 对应物：
// p.TxtMdParser.ParseContent 显式穿透）。覆写整体 Parse（非 ParseContent）
// 产出带 title/source_type metadata 的单 Document。
// 硬错误范式：读不到文件/找不到主内容/正文过短 → 155110。
//
// Python: html_file_parser.py HTMLFileParser
type HTMLFileParser struct {
	// TxtMdParser 父级（编码检测读文件）
	*TxtMdParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// htmlMainSelectorTextLength 主内容选择器文本门槛（对齐 Python _find_main_content > 100）。
	htmlMainSelectorTextLength = 100
	// htmlFallbackTextLength fallback 块文本门槛（对齐 Python > 200）。
	htmlFallbackTextLength = 200
	// htmlMinArticleLength 正文最小长度（对齐 Python len(text) < 50）。
	htmlMinArticleLength = 50
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// mainContentSelectors 主内容选择器列表（对齐 MAIN_CONTENT_SELECTORS 字面复制）。
	mainContentSelectors = []string{
		"article",
		"main",
		`[role="main"]`,
		".article-body",
		".post-content",
		".content",
		".entry-content",
		".post-body",
		"#content",
		".main-content",
	}
	// htmlSpaceRe 行内空白压缩（对齐 re.sub(r"[ \t]+", " ", text)；标准 regexp 无语义差异）。
	htmlSpaceRe = regexp.MustCompile(`[ \t]+`)
	// htmlBlankLineRe 空行压缩（对齐 re.sub(r"\n\s*\n", "\n\n", text)；
	// regexp2 对齐 Python \s 语义——含 \x1c-\x1f 等标准 Go regexp \s 不认的字符）。
	htmlBlankLineRe = regexp2.MustCompile(`\n\s*\n`, regexp2.None)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHTMLFileParser 构造 HTMLFileParser。
//
// 零参数（对齐 Python __init__(**kwargs): super().__init__(**kwargs)）。
// 多级嵌入链构造：TxtMdParser 层的 BaseParser.self 注入最终实例——
// BaseParser 模板方法（LazyParse/Process/Supports）经 self 派发到
// HTMLFileParser 覆写版本。
// Python: html_file_parser.py HTMLFileParser.__init__
func NewHTMLFileParser() (*HTMLFileParser, error) {
	p := &HTMLFileParser{}
	// 重建 TxtMdParser 链（对齐 Python super().__init__ 链）
	tmp, err := NewTxtMdParser()
	if err != nil {
		return nil, err
	}
	p.TxtMdParser = tmp
	// 覆盖 BaseParser.self 为最终实例：多级链 self 指最终实例
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.TxtMdParser.BaseParser = bp
	return p, nil
}

// Parse 解析本地 HTML 文件（覆写整体，对齐 Python HTMLFileParser.parse）。
//
//  1. super()._parse 读文件（TxtMdParser 编码检测，软失败返回空串）
//     空串 → 155110 "Could not read HTML file or file is empty (source=%s)"
//  2. parseHTMLToDocs：title/findMainContent/getTextFromSoup/门槛校验
//     → 单 Document（对齐 Python parse 调 _parse_html classmethod）
//  3. DocID 未设回落 doc（对齐 doc_id=doc_id or doc）
//
// Python: html_file_parser.py HTMLFileParser.parse
func (p *HTMLFileParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	o := NewParseOptions(opts...)
	html, _ := p.TxtMdParser.ParseContent(ctx, doc, o.LLMClient)
	if html == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Could not read HTML file or file is empty (source="+doc+")"),
		)
	}
	docID := o.DocID
	if docID == "" {
		docID = doc
	}
	return p.parseHTMLToDocs(html, docID, doc)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 .htm/.html（对齐 @register_parser([".htm", ".HTM", ".html", ".HTML"])，
// 小写归一 first-wins 静默跳过——registry 既定语义，.HTM/.HTML 与 13.12/13.13 同款）。
func init() {
	RegisterParser([]string{".htm", ".HTM", ".html", ".HTML"}, func() Parser {
		p, _ := NewHTMLFileParser()
		return p
	})
}

// parseHTML 解析 HTML 字符串（对齐 _parse_html）。
//
// goquery 对应 BeautifulSoup（lxml 优先/HTMLParser 回落的 Go 等价物：
// goquery 统一 x/net/html 解析器）。解析失败返回空文档（web_tools 先例降级）。
// Python: html_file_parser.py _parse_html
func parseHTML(html string) *goquery.Document {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		doc, _ = goquery.NewDocumentFromReader(strings.NewReader(""))
		return doc
	}
	return doc
}

// extractTitle 提取标题（对齐 _extract_title）。
//
// og:title meta content 优先（TrimSpace；空串/缺属性回落——对齐 Python
// meta.get("content") falsy 判断），title 标签文本兜底（对齐 title_tag.string）。
// Python: html_file_parser.py _extract_title
func extractTitle(soup *goquery.Document) string {
	meta := soup.Find(`meta[property="og:title"]`)
	if meta.Length() > 0 {
		if content, ok := meta.Attr("content"); ok && content != "" {
			return strings.TrimSpace(content)
		}
	}
	title := soup.Find("title")
	if title.Length() > 0 {
		return strings.TrimSpace(title.Text())
	}
	return ""
}

// findMainContent 查找主内容节点（对齐 _find_main_content 两段式）。
//
// ①选择器列表逐一命中且 textLength > 100 即返；②fallback：body 内
// article/main/div/section 树序第一个 textLength > 200；③body 兜底返回
// （goquery 解析自动补全 body，Python 侧 body None 场景在 Go 不达）。
// Python: html_file_parser.py _find_main_content
func findMainContent(soup *goquery.Document) *goquery.Selection {
	for _, sel := range mainContentSelectors {
		node := soup.Find(sel)
		if node.Length() > 0 && textLength(node) > htmlMainSelectorTextLength {
			return node
		}
	}
	body := soup.Find("body")
	if body.Length() == 0 {
		return nil
	}
	found := body.Find("article,main,div,section").FilterFunction(func(i int, s *goquery.Selection) bool {
		return textLength(s) > htmlFallbackTextLength
	})
	if found.Length() > 0 {
		return found.First()
	}
	return body
}

// textLength 近似主文本长度（对齐 _text_length：排除 script/style，不改树）。
//
// 遍历选区内全部文本节点（含选区自身直接文本），跳过 script/style
// 子树，累加 TrimSpace 后长度——"without mutating the tree"字面对齐。
// Python: html_file_parser.py _text_length
func textLength(sel *goquery.Selection) int {
	total := 0
	collectTextLength(sel, &total)
	return total
}

// getTextFromSoup 从选区提取文本（对齐 _get_text_from_soup）。
//
// ①移除选区内 script/style（对齐 decompose——直接 Remove 修改树，
// 与 textLength 的不改树相反，Python 语义即如此）；②文本节点按文档序
// 收集 TrimSpace 后 "\n" 拼接（对齐 get_text(separator="\n", strip=True)）；
// ③[ \t]+→单空格；④\n\s*\n→\n\n；⑤TrimSpace。
// Python: html_file_parser.py _get_text_from_soup
func getTextFromSoup(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	sel.Find("script,style").Remove()
	var parts []string
	appendTextNodes(sel, &parts)
	joined := strings.Join(parts, "\n")
	joined = htmlSpaceRe.ReplaceAllString(joined, " ")
	replaced, err := htmlBlankLineRe.Replace(joined, "\n\n", -1, -1)
	if err != nil { // regexp2 替换失败保持原串（防御，理论不达）
		replaced = joined
	}
	return strings.TrimSpace(replaced)
}

// collectTextLength 递归收集文本节点长度（textLength 辅助）。
//
// 文本节点 TrimSpace 后计入；script/style 标签整体跳过（子树不递归）。
func collectTextLength(sel *goquery.Selection, total *int) {
	sel.Contents().Each(func(i int, s *goquery.Selection) {
		if goquery.NodeName(s) == "#text" {
			*total += len(strings.TrimSpace(s.Text()))
			return
		}
		name := strings.ToLower(goquery.NodeName(s))
		if name == "script" || name == "style" {
			return
		}
		collectTextLength(s, total)
	})
}

// appendTextNodes 递归收集文本节点（getTextFromSoup 辅助）。
//
// 文本节点 TrimSpace 后非空才收集（对齐 get_text strip=True）；
// script/style 已被 getTextFromSoup 前置移除，此处无跳过逻辑。
func appendTextNodes(sel *goquery.Selection, parts *[]string) {
	sel.Contents().Each(func(i int, s *goquery.Selection) {
		if goquery.NodeName(s) == "#text" {
			if t := strings.TrimSpace(s.Text()); t != "" {
				*parts = append(*parts, t)
			}
			return
		}
		appendTextNodes(s, parts)
	})
}

// parseHTMLToDocs 解析 HTML 字符串产出 Document（对齐 Python _parse_html classmethod）。
//
// title 提取 → findMainContent → getTextFromSoup → 门槛校验 → 单 Document。
// 门槛 htmlMinArticleLength=50（对齐 Python len(text) < 50；text 已 TrimSpace，
// not text 与 len<50 前者包含后者，两条件字面保留）。
// title 空串兜底 "(无标题)"（对齐 Python title or "(无标题)"——数据值保留中文）。
// Python: html_file_parser.py HTMLFileParser._parse_html
func (p *HTMLFileParser) parseHTMLToDocs(html string, docID string, source string) ([]reranker.Document, error) {
	soup := parseHTML(html)
	title := extractTitle(soup)
	contentNode := findMainContent(soup)
	if contentNode == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Could not find main content in HTML (source="+source+")"),
		)
	}
	text := getTextFromSoup(contentNode)
	if len(text) == 0 || len(text) < htmlMinArticleLength {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Article content too short or empty after parsing html (source="+source+")"),
		)
	}
	effectiveTitle := title
	if effectiveTitle == "" {
		effectiveTitle = "(无标题)"
	}
	return []reranker.Document{{
		ID:   docID,
		Text: text,
		Metadata: map[string]any{
			"title":       effectiveTitle,
			"source_type": "web_page",
		},
	}}, nil
}
