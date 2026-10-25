package parser

import (
	"context"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// docMatcher 路由匹配器（Python Union[re.Pattern, Callable[[str], bool]]
// 对应物，非导出接口）。
//
// 两实现：regexpMatcher（正则形态）/ funcMatcher（可调用形态）。
// 经 NewRegexpMatcher/NewFuncMatcher 导出包装供外部注入自定义路由。
//
// Python: auto_link_parser.py Union[re.Pattern, Callable[[str], bool]]
type docMatcher interface {
	// MatchDoc 匹配文档源。
	MatchDoc(doc string) bool
}

// regexpMatcher 正则形态匹配器。
//
// MatchDoc 对齐 Python pattern_or_callable.match(doc.strip())。
type regexpMatcher struct {
	// re 编译后正则（复用 13.14 httpURLPattern/wechatMPURLPattern 同款 *regexp.Regexp）
	re *regexp.Regexp
}

// funcMatcher 可调用形态匹配器。
//
// MatchDoc 对齐 Python callable(pattern_or_callable(doc))——原样传入不 strip。
type funcMatcher struct {
	// fn 匹配函数
	fn func(string) bool
}

// linkRoute 单条路由（匹配器 + 命中后委托的解析器）。
type linkRoute struct {
	// matcher 匹配器
	matcher docMatcher
	// parser 命中后委托的解析器
	parser Parser
}

// AutoLinkParser URL 模式路由解析器（先匹配先赢）。
//
// 组合解析器（13.10 spec 预告模式）：嵌入 BaseParser + 构造注入 self +
// 覆写整体 Parse/Supports。默认路由：微信文章 → WeChatArticleParser、
// 通用 http(s) → WebPageParser（13.14 产出，每次构造新实例）。
//
// Python: auto_link_parser.py AutoLinkParser
type AutoLinkParser struct {
	// BaseParser 模板适配器
	*BaseParser
	// routes 路由表（先匹配先赢）
	routes []linkRoute
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewRegexpMatcher 构造正则形态匹配器。
func NewRegexpMatcher(re *regexp.Regexp) docMatcher {
	return &regexpMatcher{re: re}
}

// NewFuncMatcher 构造可调用形态匹配器。
func NewFuncMatcher(fn func(string) bool) docMatcher {
	return &funcMatcher{fn: fn}
}

// NewAutoLinkParser 构造 AutoLinkParser（切片参数二分，决策 7）。
//
// routes == nil → defaultRoutes() 两条默认路由（微信优先）；
// routes == 非nil空切片 → 字面空表（Parse 恒返回空切片，对齐 Python routes=[]）；
// routes 非空 → 字面使用调用方路由表。
//
// Python: auto_link_parser.py AutoLinkParser.__init__
//（routes if routes is not None else _default_routes()——None/[]/非空三态）
func NewAutoLinkParser(routes []linkRoute) (*AutoLinkParser, error) {
	if routes == nil {
		var err error
		routes, err = defaultRoutes()
		if err != nil {
			return nil, err
		}
	}
	p := &AutoLinkParser{routes: routes}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// Parse 解析 URL：遍历路由表首个命中即委托。
//
// 命中 → Debug 日志 → 底层 parser.Parse（决策 3：转发前剔除 LLMClient——
// Python parser.parse(doc, doc_id=doc_id, **kwargs) 关键字调用天然不含
// llm_client，Go opts 整体转发无法天然丢弃，显式剔除字面对齐）。
// 无命中 → 空切片。
//
// Python: auto_link_parser.py AutoLinkParser.parse
func (p *AutoLinkParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	for _, route := range p.routes {
		if matchDoc(route.matcher, doc) {
			// 对齐 Python: logger.debug("AutoLinkParser delegating to %s", type(parser).__name__)
			logger.Debug(logComponent).
				Str("parser_name", parserTypeName(route.parser)).
				Msg("AutoLinkParser delegating to parser")
			return route.parser.Parse(ctx, doc, withoutLLMClient(opts...)...)
		}
	}
	return []reranker.Document{}, nil
}

// Supports 检查是否支持该文档源：非空 http(s) URL 且任一路由命中。
//
// Python: auto_link_parser.py AutoLinkParser.supports
func (p *AutoLinkParser) Supports(doc string) bool {
	if doc == "" || !httpURLPattern.MatchString(strings.TrimSpace(doc)) {
		return false
	}
	for _, route := range p.routes {
		if matchDoc(route.matcher, doc) {
			return true
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultRoutes 构造默认路由表（每次新解析器实例，对齐 Python _default_routes()）。
//
// 微信优先：WECHAT_MP_URL_PATTERN → WeChatArticleParser；
// 兜底：HTTP_URL_PATTERN → WebPageParser。两构造 error 直接上抛
//（Python 侧构造无异常面，Go 构造含 error 为语言差异）。
//
// Python: auto_link_parser.py _default_routes
func defaultRoutes() ([]linkRoute, error) {
	wechatParser, err := NewWeChatArticleParser()
	if err != nil {
		return nil, err
	}
	webParser, err := NewWebPageParserWithDefaults()
	if err != nil {
		return nil, err
	}
	return []linkRoute{
		{matcher: NewRegexpMatcher(wechatMPURLPattern), parser: wechatParser},
		{matcher: NewRegexpMatcher(httpURLPattern), parser: webParser},
	}, nil
}

// matchDoc 双形态匹配 dispatch（Python _match_doc 对应物）。
//
// regexpMatcher 内部 strip（对齐 pattern.match(doc.strip())）；
// funcMatcher 原样传入（对齐 callable(doc)）。
//
// Python: auto_link_parser.py _match_doc
func matchDoc(m docMatcher, doc string) bool {
	return m.MatchDoc(doc)
}

// MatchDoc 正则形态：strip 后匹配（对齐 Python pattern.match(doc.strip())）。
func (m *regexpMatcher) MatchDoc(doc string) bool {
	return m.re.MatchString(strings.TrimSpace(doc))
}

// MatchDoc 可调用形态：原样传入（对齐 Python callable(doc) 不 strip）。
func (m *funcMatcher) MatchDoc(doc string) bool {
	return m.fn(doc)
}

// withoutLLMClient 剔除 LLMClient 后重建 opts（决策 3）。
//
// Python parser.parse(doc, doc_id=doc_id, **kwargs) 关键字调用天然不含
// llm_client；Go opts 整体转发无法天然丢弃，显式重建字面对齐。
// 保留 DocID/Extra/Timeout/UserAgent/Verify/HTTPClient，仅置空 LLMClient。
func withoutLLMClient(opts ...ParseOption) []ParseOption {
	o := NewParseOptions(opts...)
	o.LLMClient = nil
	return []ParseOption{func(no *ParseOptions) {
		*no = *o
	}}
}
