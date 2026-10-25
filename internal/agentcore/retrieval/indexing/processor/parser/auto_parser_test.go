package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeDelegateParser AutoParser 委托测试桩：supports 可编程 + Parse 记录调用。
type fakeDelegateParser struct {
	*BaseParser
	supports func(string) bool
	calls    int
	lastLLM  bool
	lastDoc  string
	docs     []reranker.Document
}

// newFakeDelegateParser 构造并完成 self 注入。
func newFakeDelegateParser(supports func(string) bool, docs []reranker.Document) *fakeDelegateParser {
	p := &fakeDelegateParser{supports: supports, docs: docs}
	bp, _ := NewBaseParser(p)
	p.BaseParser = bp
	return p
}

// Parse 覆写：记录调用。
func (p *fakeDelegateParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	p.calls++
	p.lastDoc = doc
	o := NewParseOptions(opts...)
	p.lastLLM = o.LLMClient != nil
	return p.docs, nil
}

// Supports 覆写：可编程谓词。
func (p *fakeDelegateParser) Supports(doc string) bool { return p.supports(doc) }

// ──────────────────────────── AutoParser 测试 ────────────────────────────

// TestAutoParserSupports_httpURL URL 委托 link parser。
// Python: test_supports_http_url
func TestAutoParserSupports_httpURL(t *testing.T) {
	p, err := NewAutoParser(nil, nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if !p.Supports("https://mp.weixin.qq.com/s/abc") {
		t.Error("微信 URL 应支持")
	}
	if !p.Supports("https://example.com/page") {
		t.Error("通用 URL 应支持")
	}
}

// TestAutoParserSupports_文件路径 存在文件委托 file parser。
// Python: test_supports_file_path
func TestAutoParserSupports_文件路径(t *testing.T) {
	p, _ := NewAutoParser(nil, nil)
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !p.Supports(path) {
		t.Error("存在 .txt 文件应支持")
	}
}

// TestAutoParserSupports_非URL非文件 既非 URL 也非存在文件 → false。
// Python: test_supports_non_url_non_file
func TestAutoParserSupports_非URL非文件(t *testing.T) {
	p, _ := NewAutoParser(nil, nil)
	if p.Supports("not-a-url") {
		t.Error("非 URL 应不支持")
	}
	if p.Supports("/nonexistent/path.txt") {
		t.Error("不存在路径应不支持")
	}
}

// TestAutoParserParse_URL委托linkParser URL 输入只调 link parser。
// Python: test_parse_url_delegates_to_link_parser
func TestAutoParserParse_URL委托linkParser(t *testing.T) {
	doc := reranker.Document{ID: "link_doc", Text: "from link", Metadata: map[string]any{}}
	link := newFakeDelegateParser(func(d string) bool { return strings.HasPrefix(d, "http") }, []reranker.Document{doc})
	file := newFakeDelegateParser(func(string) bool { return false }, nil)
	p, _ := NewAutoParser(link, file)
	docs, err := p.Parse(t.Context(), "https://example.com/page", WithDocID("id1"), WithLLMClient(fakeLLMModel(t)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "link_doc" {
		t.Fatalf("result = %#v", docs)
	}
	if link.calls != 1 || link.lastDoc != "https://example.com/page" {
		t.Fatalf("link parser 调用不符: calls=%d doc=%q", link.calls, link.lastDoc)
	}
	if !link.lastLLM {
		t.Fatal("AutoParser 转发 link 应透传 LLMClient（与 AutoLink 剔除形成对照）")
	}
	if file.calls != 0 {
		t.Fatal("file parser 不应被调用")
	}
}

// TestAutoParserParse_文件委托fileParser 文件输入只调 file parser。
// Python: test_parse_file_delegates_to_file_parser
func TestAutoParserParse_文件委托fileParser(t *testing.T) {
	doc := reranker.Document{ID: "file_doc", Text: "from file", Metadata: map[string]any{}}
	link := newFakeDelegateParser(func(string) bool { return false }, nil)
	file := newFakeDelegateParser(func(d string) bool {
		if _, err := os.Stat(d); err != nil {
			return false
		}
		return strings.HasSuffix(d, ".txt")
	}, []reranker.Document{doc})
	p, _ := NewAutoParser(link, file)
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs, err := p.Parse(t.Context(), path, WithDocID("id2"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "file_doc" {
		t.Fatalf("result = %#v", docs)
	}
	if file.calls != 1 || file.lastDoc != path {
		t.Fatalf("file parser 调用不符: calls=%d doc=%q", file.calls, file.lastDoc)
	}
	if link.calls != 0 {
		t.Fatal("link parser 不应被调用")
	}
}

// TestAutoParserParse_双不支持返回空 两类输入均不支持 → 空切片。
// Python: test_parse_unsupported_returns_empty
func TestAutoParserParse_双不支持返回空(t *testing.T) {
	no := newFakeDelegateParser(func(string) bool { return false }, nil)
	p, _ := NewAutoParser(no, no)
	for _, doc := range []string{"https://example.com", "/nonexistent.xyz"} {
		docs, err := p.Parse(t.Context(), doc, WithDocID("id3"))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", doc, err)
		}
		if len(docs) != 0 {
			t.Fatalf("Parse(%q) 应返回空切片, got %d", doc, len(docs))
		}
	}
}

// TestAutoParserParse_fallThrough分支 URL-like 但 link 不支持 → file 分支接管。
// Python parse 控制流字面验证：is_likely_url 为真但 link.supports 假时落到 file 分支。
func TestAutoParserParse_fallThrough分支(t *testing.T) {
	doc := reranker.Document{ID: "fall_doc", Text: "fallthrough", Metadata: map[string]any{}}
	link := newFakeDelegateParser(func(string) bool { return false }, nil) // URL-like 但不支持
	file := newFakeDelegateParser(func(string) bool { return true }, []reranker.Document{doc})
	p, _ := NewAutoParser(link, file)
	docs, err := p.Parse(t.Context(), "https://example.com/weird", WithDocID("id4"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "fall_doc" {
		t.Fatalf("result = %#v", docs)
	}
	if file.calls != 1 {
		t.Fatal("fall-through 应落到 file parser")
	}
}

// TestNewAutoParser_默认构造 双 nil → 默认 AutoLinkParser/AutoFileParser。
func TestNewAutoParser_默认构造(t *testing.T) {
	p, err := NewAutoParser(nil, nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if _, ok := p.linkParser.(*AutoLinkParser); !ok {
		t.Fatalf("linkParser 应为 *AutoLinkParser, got %T", p.linkParser)
	}
	if _, ok := p.fileParser.(*AutoFileParser); !ok {
		t.Fatalf("fileParser 应为 *AutoFileParser, got %T", p.fileParser)
	}
}
