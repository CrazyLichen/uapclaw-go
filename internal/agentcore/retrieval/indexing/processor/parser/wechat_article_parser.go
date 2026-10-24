// Package parser wechat_article_parser.go：微信公众号文章解析器（13.14）。
//
// 本文件对齐 Python wechat_article_parser.py：WeChatArticleParser 继承
// WebPageParser，覆写 _parse_html（js_content 提取）与 _validate_url
// （微信正则），经 dispatch 注入生效；不覆写 parse（继承 WebPage 链）。
package parser

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WeChatArticleParser 微信公众号文章解析器（对齐 Python WeChatArticleParser(WebPageParser)）。
//
// 覆写 parseHTMLToDocs（js_content 提取）与 validateURLFor（微信正则），
// 经 WebPageParser.dispatch 注入生效。不覆写 Parse——继承 WebPage 的
// Parse→ParseURL 链。无 50 字符门槛只判空（Python L79-83 字面，
// 与 WebPage 的 <50 门槛对照）。
// Python: wechat_article_parser.py WeChatArticleParser
type WeChatArticleParser struct {
	// WebPageParser 父级（Parse/ParseURL/downloadHTML 继承）
	*WebPageParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewWeChatArticleParser 构造 WeChatArticleParser。
//
// 默认参数复用 WebPage 默认（timeout=30s/UA=DEFAULT/verify=default，
// 对齐 Python 无自定义 __init__）。dispatch 注入自身——覆写版本生效；
// self 链指向最终实例。
// Python: wechat_article_parser.py WeChatArticleParser（无 __init__）
func NewWeChatArticleParser() (*WeChatArticleParser, error) {
	wpp, err := NewWebPageParserWithDefaults()
	if err != nil {
		return nil, err
	}
	p := &WeChatArticleParser{WebPageParser: wpp}
	wpp.dispatch = p // 覆写注入：WeChat 版 parseHTMLToDocs/validateURLFor 生效
	// self 链指向最终实例：BaseParser 模板方法派发到 WeChat 继承的 Parse
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.WebPageParser.HTMLFileParser.TxtMdParser.BaseParser = bp
	return p, nil
}

// ParseWeChatArticleURL 包级别名（对齐 parse_wechat_article_url = WeChatArticleParser.parse_url）。
//
// 默认参数全链；覆盖参数经 ParseOption 传入（kwargs 对应物）。
// Python: wechat_article_parser.py 模块尾别名赋值
func ParseWeChatArticleURL(ctx context.Context, url string, docID string, opts ...ParseOption) ([]reranker.Document, error) {
	p, err := NewWeChatArticleParser()
	if err != nil {
		return nil, err
	}
	o := NewParseOptions(opts...)
	timeout := o.Timeout
	if timeout == 0 {
		timeout = p.timeout
	}
	userAgent := o.UserAgent
	if userAgent == "" {
		userAgent = p.userAgent
	}
	verify := o.Verify
	if verify == nil {
		verify = p.verify
	}
	return p.ParseURL(ctx, url, docID, timeout, userAgent, verify, o.HTTPClient)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseHTMLToDocs 解析微信文章页面（覆写，对齐 Python _parse_html classmethod）。
//
// title → extractJSContent 提取 js_content → 缺失 → 155110（注意冒号后
// 空格，与前四处 source= 不同——Python 字面）；→ getTextFromSoup →
// 空 → 155110（无 50 字符门槛只判空）→ 单 Document（source_type=
// wechat_article，title 兜底 "(无标题)"）。
// Python: wechat_article_parser.py WeChatArticleParser._parse_html
func (p *WeChatArticleParser) parseHTMLToDocs(html string, docID string, source string) ([]reranker.Document, error) {
	soup := parseHTML(html)
	title := extractTitle(soup)
	contentNode := extractJSContent(soup)
	if contentNode == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Could not find article content (js_content) in page (source: "+source+")"),
		)
	}
	text := getTextFromSoup(contentNode)
	if text == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Article content is empty after parsing (source: "+source+")"),
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
			"source_type": "wechat_article",
		},
	}}, nil
}

// validateURLFor 校验微信文章 URL（覆写，对齐 Python _validate_url 静态方法覆写）。
//
// 非微信 URL → 155110 "Not a WeChat article URL: %q"（%q 对齐 Python repr）。
// Python: wechat_article_parser.py WeChatArticleParser._validate_url
func (p *WeChatArticleParser) validateURLFor(url string) error {
	url = strings.TrimSpace(url)
	if !isWeChatArticleURL(url) {
		return exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg(fmt.Sprintf("Not a WeChat article URL: %q", url)),
		)
	}
	return nil
}

// Supports 覆写：微信文章 URL 才支持（对齐 Python supports 覆写）。
//
// Python: wechat_article_parser.py WeChatArticleParser.supports
func (p *WeChatArticleParser) Supports(doc string) bool {
	return isWeChatArticleURL(doc)
}

// extractJSContent 提取 js_content 节点（对齐 _extract_js_content）。
//
// div#js_content 微信文章正文容器；缺失返回空选区（调用方判 nil 语义：
// Length()==0 等价 Python find 返回 None）。
// Python: wechat_article_parser.py _extract_js_content
func extractJSContent(soup *goquery.Document) *goquery.Selection {
	sel := soup.Find("div#js_content")
	if sel.Length() == 0 {
		return nil
	}
	return sel
}
