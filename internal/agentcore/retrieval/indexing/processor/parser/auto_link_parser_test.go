package parser

import (
	"context"
	"regexp"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeRouteParser 可控路由测试桩：记录 Parse 收到的 doc 与 opts 展开。
type fakeRouteParser struct {
	*BaseParser
	supports   bool
	lastDoc    string
	lastDocID  string
	lastLLM    bool // 是否收到非 nil LLMClient
	calls      int
	returnDocs []reranker.Document
}

// fakeLLMModel 测试用非 nil LLM 客户端（复用 newTestModel 真实构造链，
// captioner_test.go 同包既有辅助）。
func fakeLLMModel(t *testing.T) *llm.Model {
	t.Helper()
	cfg := llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen3-vl-plus"))
	return newTestModel(t, cfg)
}

// newFakeRouteParser 构造并完成 self 注入。
func newFakeRouteParser(supports bool, docs []reranker.Document) *fakeRouteParser {
	p := &fakeRouteParser{supports: supports, returnDocs: docs}
	bp, _ := NewBaseParser(p)
	p.BaseParser = bp
	return p
}

// Parse 覆写：记录调用参数后返回预置文档。
func (p *fakeRouteParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	p.calls++
	p.lastDoc = doc
	o := NewParseOptions(opts...)
	p.lastDocID = o.DocID
	p.lastLLM = o.LLMClient != nil
	return p.returnDocs, nil
}

// Supports 覆写：可编程。
func (p *fakeRouteParser) Supports(doc string) bool { return p.supports }

// ──────────────────────────── AutoLinkParser 测试 ────────────────────────────

// TestAutoLinkParserSupports_微信URL 微信 URL 两形态均支持。
// Python: test_supports_wechat_url
func TestAutoLinkParserSupports_微信URL(t *testing.T) {
	p, err := NewAutoLinkParser(nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if !p.Supports("https://mp.weixin.qq.com/s/abc123") {
		t.Error("https 微信 URL 应支持")
	}
	if !p.Supports("http://mp.weixin.qq.com/s/xyz") {
		t.Error("http 微信 URL 应支持")
	}
}

// TestAutoLinkParserSupports_通用httpURL 通用 http(s) URL 支持。
// Python: test_supports_generic_http_url
func TestAutoLinkParserSupports_通用httpURL(t *testing.T) {
	p, _ := NewAutoLinkParser(nil)
	if !p.Supports("https://example.com/article") {
		t.Error("https URL 应支持")
	}
	if !p.Supports("http://blog.google/foo") {
		t.Error("http URL 应支持")
	}
}

// TestAutoLinkParserSupports_非http为false 非 http 输入不支持。
// Python: test_supports_non_http_false
func TestAutoLinkParserSupports_非http为false(t *testing.T) {
	p, _ := NewAutoLinkParser(nil)
	for _, doc := range []string{"ftp://example.com", "/local/path/file.txt", "not-a-url"} {
		if p.Supports(doc) {
			t.Errorf("非 http 输入 %q 应不支持", doc)
		}
	}
}

// TestAutoLinkParserSupports_空串 空/空白输入不支持。
// Python: test_supports_empty_or_none
func TestAutoLinkParserSupports_空串(t *testing.T) {
	p, _ := NewAutoLinkParser(nil)
	if p.Supports("") {
		t.Error("空串应不支持")
	}
	if p.Supports("   ") {
		t.Error("空白串应不支持")
	}
}

// TestAutoLinkParserParse_首路由命中转发 首个命中路由的解析器收到调用并返回其结果。
// Python: test_parse_delegates_to_first_matching_route
func TestAutoLinkParserParse_首路由命中转发(t *testing.T) {
	doc := reranker.Document{ID: "d1", Text: "parsed", Metadata: map[string]any{}}
	wechatParser := newFakeRouteParser(true, []reranker.Document{doc})
	webParser := newFakeRouteParser(true, nil)
	p, err := NewAutoLinkParser([]linkRoute{
		{matcher: NewRegexpMatcher(regexp.MustCompile(`^https?://wechat\.com/`)), parser: wechatParser},
		{matcher: NewRegexpMatcher(httpURLPattern), parser: webParser},
	})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	docs, err := p.Parse(t.Context(), "https://wechat.com/article", WithDocID("id1"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "d1" {
		t.Fatalf("result = %#v", docs)
	}
	if wechatParser.lastDoc != "https://wechat.com/article" || wechatParser.lastDocID != "id1" {
		t.Fatalf("底层解析器参数不符: doc=%q docID=%q", wechatParser.lastDoc, wechatParser.lastDocID)
	}
	if webParser.calls != 0 {
		t.Fatal("第二路由不应被调用")
	}
}

// TestAutoLinkParserParse_首路由不命中走第二 第二路由兜底。
// Python: test_parse_second_route_when_first_no_match
func TestAutoLinkParserParse_首路由不命中走第二(t *testing.T) {
	doc := reranker.Document{ID: "d2", Text: "web", Metadata: map[string]any{}}
	webParser := newFakeRouteParser(true, []reranker.Document{doc})
	p, _ := NewAutoLinkParser([]linkRoute{
		{matcher: NewRegexpMatcher(regexp.MustCompile(`^https?://wechat\.com/`)), parser: newFakeRouteParser(true, nil)},
		{matcher: NewRegexpMatcher(httpURLPattern), parser: webParser},
	})
	docs, err := p.Parse(t.Context(), "https://example.com/page", WithDocID("id2"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "d2" {
		t.Fatalf("result = %#v", docs)
	}
	if webParser.lastDoc != "https://example.com/page" {
		t.Fatalf("web parser doc = %q", webParser.lastDoc)
	}
}

// TestAutoLinkParserParse_无命中返回空 无路由命中返回空切片。
// Python: test_parse_no_match_returns_empty
func TestAutoLinkParserParse_无命中返回空(t *testing.T) {
	p, _ := NewAutoLinkParser([]linkRoute{
		{matcher: NewRegexpMatcher(regexp.MustCompile(`^https?://only\.this/`)), parser: newFakeRouteParser(true, nil)},
	})
	docs, err := p.Parse(t.Context(), "https://other.com/page", WithDocID("id3"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected 0 docs, got %d", len(docs))
	}
}

// TestAutoLinkParserParse_LLMClient剔除 llm_client 转发前被剔除（决策 3 核心验证）。
// Python auto_link_parser.py L62：parser.parse(doc, doc_id=doc_id, **kwargs) 不含 llm_client。
func TestAutoLinkParserParse_LLMClient剔除(t *testing.T) {
	fake := newFakeRouteParser(true, nil)
	p, _ := NewAutoLinkParser([]linkRoute{
		{matcher: NewRegexpMatcher(httpURLPattern), parser: fake},
	})
	_, err := p.Parse(t.Context(), "https://example.com/page", WithDocID("id1"), WithLLMClient(fakeLLMModel(t)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if fake.lastLLM {
		t.Fatal("底层解析器不应收到 LLMClient（决策 3 字面剔除）")
	}
	if fake.lastDocID != "id1" {
		t.Fatalf("DocID 应透传, got %q", fake.lastDocID)
	}
}

// TestAutoLinkParserParse_自定义callable路由 NewFuncMatcher 奇偶长度路由。
// Python: test_custom_callable_route（len("http://a.b")==10 偶数 false / len("http://a.bc")==11 奇数 true）
func TestAutoLinkParserParse_自定义callable路由(t *testing.T) {
	onlyOddLength := func(url string) bool { return len(url)%2 == 1 }
	p, _ := NewAutoLinkParser([]linkRoute{
		{matcher: NewFuncMatcher(onlyOddLength), parser: newFakeRouteParser(true, nil)},
	})
	if len("http://a.b") != 10 || len("http://a.bc") != 11 {
		t.Fatal("前置断言失败：长度前提不成立")
	}
	if p.Supports("http://a.b") {
		t.Error("偶数长度 URL 应不命中")
	}
	if !p.Supports("http://a.bc") {
		t.Error("奇数长度 URL 应命中")
	}
}

// TestAutoLinkParserParse_strip语义 前后空格 URL 经 TrimSpace 后正则命中。
// regexpMatcher 对齐 Python pattern.match(doc.strip())。
func TestAutoLinkParserParse_strip语义(t *testing.T) {
	fake := newFakeRouteParser(true, nil)
	p, _ := NewAutoLinkParser([]linkRoute{
		{matcher: NewRegexpMatcher(httpURLPattern), parser: fake},
	})
	if !p.Supports("  https://example.com/page  ") {
		t.Error("前后空格 URL 应命中（MatchDoc strip 对齐 Python .match(doc.strip())）")
	}
}

// TestNewAutoLinkParser_默认路由 nil → 两条默认路由且微信优先。
func TestNewAutoLinkParser_默认路由(t *testing.T) {
	p, err := NewAutoLinkParser(nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if len(p.routes) != 2 {
		t.Fatalf("默认路由应 2 条, got %d", len(p.routes))
	}
	// 微信优先：首路由解析器为 WeChatArticleParser
	if _, ok := p.routes[0].parser.(*WeChatArticleParser); !ok {
		t.Fatalf("首路由应为 WeChatArticleParser, got %T", p.routes[0].parser)
	}
	if _, ok := p.routes[1].parser.(*WebPageParser); !ok {
		t.Fatalf("第二路由应为 WebPageParser, got %T", p.routes[1].parser)
	}
}

// TestNewAutoLinkParser_空表字面语义 非nil空切片 → 字面空表（Parse 恒空切片）。
// 决策 7 二分验证：nil→默认 / []→字面空表（Python routes=None / routes=[]）。
func TestNewAutoLinkParser_空表字面语义(t *testing.T) {
	p, err := NewAutoLinkParser([]linkRoute{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if len(p.routes) != 0 {
		t.Fatalf("空表应 0 条路由, got %d", len(p.routes))
	}
	docs, err := p.Parse(t.Context(), "https://example.com/page")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("空表 Parse 应恒返回空切片, got %d", len(docs))
	}
	if p.Supports("https://example.com/page") {
		t.Fatal("空表 supports 应 false（无路由命中）")
	}
}
