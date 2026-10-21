package parser

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 测试 fake ────────────────────────────

// fakeContentParser 仅覆写 ParseContent 的最小解析器（模板路径主测）。
type fakeContentParser struct {
	*BaseParser
	// content 预置返回内容
	content string
	// err 预置返回错误
	err error
	// gotLLMClient 记录钩子收到的 llmClient（透传断言用）
	gotLLMClient *llm.Model
	// gotDoc 记录钩子收到的 doc
	gotDoc string
}

// newFakeContentParser 构造并完成 self 注入。
func newFakeContentParser(content string, err error) *fakeContentParser {
	p := &fakeContentParser{content: content, err: err}
	bp, _ := NewBaseParser(p)
	p.BaseParser = bp
	return p
}

// ParseContent 覆写钩子，记录入参并返回预置结果。
func (p *fakeContentParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	p.gotDoc = doc
	p.gotLLMClient = llmClient
	return p.content, p.err
}

// fakeMultiParser 覆写 Parse 返回多文档（LazyParse self 派发测试）。
type fakeMultiParser struct {
	*BaseParser
	// docs 预置返回文档
	docs []reranker.Document
	// err 预置返回错误
	err error
}

// newFakeMultiParser 构造并完成 self 注入。
func newFakeMultiParser(docs []reranker.Document, err error) *fakeMultiParser {
	p := &fakeMultiParser{docs: docs, err: err}
	bp, _ := NewBaseParser(p)
	p.BaseParser = bp
	return p
}

// Parse 覆写整体入口，返回预置结果（跳过 ParseContent 模板路径）。
func (p *fakeMultiParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.docs, nil
}

// fakeFullParser 整体实现 Parser 五方法的独立实现（不嵌 BaseParser）。
type fakeFullParser struct {
	// supports 预置 Supports 返回值
	supports bool
}

// Parse 返回单文档。
func (p *fakeFullParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	return []reranker.Document{{ID: "full-1", Text: doc, Metadata: map[string]any{}}}, nil
}

// LazyParse 转发 Parse 后逐个回调。
func (p *fakeFullParser) LazyParse(ctx context.Context, doc string, fn func(reranker.Document) error, opts ...ParseOption) error {
	docs, err := p.Parse(ctx, doc, opts...)
	if err != nil {
		return err
	}
	for _, d := range docs {
		if err := fn(d); err != nil {
			return err
		}
	}
	return nil
}

// Process 转发 Parse。
func (p *fakeFullParser) Process(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	return p.Parse(ctx, doc, opts...)
}

// Supports 返回预置值。
func (p *fakeFullParser) Supports(doc string) bool {
	return p.supports
}

// ParseContent 返回固定内容。
func (p *fakeFullParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	return "full", nil
}

// ──────────────────────────── NewBaseParser 测试 ────────────────────────────

// TestNewBaseParser 测试构造注入
func TestNewBaseParser(t *testing.T) {
	p := newFakeContentParser("x", nil)
	if p.BaseParser == nil {
		t.Fatal("NewBaseParser 应返回非空 BaseParser")
	}
}

// TestNewBaseParser_nilSelf 测试 nil 防御
func TestNewBaseParser_nilSelf(t *testing.T) {
	if _, err := NewBaseParser(nil); err == nil {
		t.Fatal("nil self 应返回错误")
	}
}

// ──────────────────────────── Parse 测试 ────────────────────────────

// TestParse_默认空内容 测试钩子返回空串时产出空切片
func TestParse_默认空内容(t *testing.T) {
	p := newFakeContentParser("", nil)
	docs, err := p.Parse(context.Background(), "doc.txt")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("空内容应返回空切片, got %d", len(docs))
	}
}

// TestParse_非空内容包装 测试单文档包装
func TestParse_非空内容包装(t *testing.T) {
	p := newFakeContentParser("hello", nil)
	docs, err := p.Parse(context.Background(), "doc.txt", WithDocID("id-1"))
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("应返回 1 个文档, got %d", len(docs))
	}
	if docs[0].ID != "id-1" {
		t.Fatalf("ID 应为 id-1, got %s", docs[0].ID)
	}
	if docs[0].Text != "hello" {
		t.Fatalf("Text 应为 hello, got %s", docs[0].Text)
	}
	if docs[0].Metadata == nil || len(docs[0].Metadata) != 0 {
		t.Fatalf("Metadata 应为非 nil 空 map, got %#v", docs[0].Metadata)
	}
}

// TestParse_错误传播 测试钩子错误原样返回
func TestParse_错误传播(t *testing.T) {
	sentinel := errors.New("parse failed")
	p := newFakeContentParser("", sentinel)
	docs, err := p.Parse(context.Background(), "doc.txt")
	if !errors.Is(err, sentinel) {
		t.Fatalf("应原样传播错误, got %v", err)
	}
	if docs != nil {
		t.Fatalf("错误时应返回 nil 文档, got %d", len(docs))
	}
}

// TestParse_DocID未设置 测试 ID 保持空串（决策 6 字面对齐）
func TestParse_DocID未设置(t *testing.T) {
	p := newFakeContentParser("hello", nil)
	docs, _ := p.Parse(context.Background(), "doc.txt")
	if len(docs) != 1 || docs[0].ID != "" {
		t.Fatalf("未设置 DocID 时 ID 应保持空串, got %#v", docs)
	}
}

// TestParse_LLMClient透传 测试钩子收到 llmClient
func TestParse_LLMClient透传(t *testing.T) {
	p := newFakeContentParser("hello", nil)
	// llm.Model 结构体零值即可（不发起真实调用，仅透传断言）
	client := &llm.Model{}
	p.Parse(context.Background(), "doc.txt", WithLLMClient(client))
	if p.gotLLMClient != client {
		t.Fatal("钩子应收到透传的 llmClient")
	}
}

// TestParse_Extra不进钩子 测试 Extra 不传给 ParseContent（对齐 Python kwargs 丢弃）
func TestParse_Extra不进钩子(t *testing.T) {
	p := newFakeContentParser("hello", nil)
	p.Parse(context.Background(), "doc.txt", WithExtra("file_name", "a.txt"), WithExtra("k2", 1))
	// Extra 不进钩子——钩子签名只有 doc/llmClient，能编译即证明；
	// 此用例验证 Extra 可经 NewParseOptions 读取（13.11 AutoFileParser 消费面）
	o := NewParseOptions(WithExtra("file_name", "a.txt"))
	if o.Extra["file_name"] != "a.txt" {
		t.Fatal("WithExtra 应写入 Extra")
	}
}

// TestParse_Options零参 测试零 options 路径
func TestParse_Options零参(t *testing.T) {
	p := newFakeContentParser("x", nil)
	docs, err := p.Parse(context.Background(), "doc.txt")
	if err != nil || len(docs) != 1 {
		t.Fatalf("零 options 应正常解析: %v %d", err, len(docs))
	}
	if p.gotLLMClient != nil {
		t.Fatal("零 options 时钩子 llmClient 应为 nil")
	}
}

// defaultParser 直接构造的零值 Parser 实现（测 BaseParser 默认行为）。
type defaultParser struct{}

func (defaultParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	return nil, nil
}
func (defaultParser) LazyParse(ctx context.Context, doc string, fn func(reranker.Document) error, opts ...ParseOption) error {
	return nil
}
func (defaultParser) Process(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	return nil, nil
}
func (defaultParser) Supports(doc string) bool { return false }
func (defaultParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	return "", nil
}

// TestParse_BaseParser独立实例 测试默认钩子 ("", nil) → 空切片 + Supports 默认 false
func TestParse_BaseParser独立实例(t *testing.T) {
	// 用 defaultParser 作 self：BaseParser 默认 ParseContent 经 self 派发仍是默认空实现
	bp, err := NewBaseParser(defaultParser{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	docs, err := bp.Parse(context.Background(), "doc.txt")
	if err != nil || len(docs) != 0 {
		t.Fatalf("默认钩子应产出空切片: %v %d", err, len(docs))
	}
	if bp.Supports("doc.txt") {
		t.Fatal("默认 Supports 应为 false")
	}
}

// ──────────────────────────── LazyParse 测试 ────────────────────────────

// TestLazyParse_逐个回调 测试多文档顺序回调（经 self 派发到覆写的 Parse）
func TestLazyParse_逐个回调(t *testing.T) {
	p := newFakeMultiParser([]reranker.Document{
		{ID: "1", Text: "a"},
		{ID: "2", Text: "b"},
		{ID: "3", Text: "c"},
	}, nil)
	var got []string
	err := p.LazyParse(context.Background(), "doc.txt", func(d reranker.Document) error {
		got = append(got, d.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if len(got) != 3 || got[0] != "1" || got[2] != "3" {
		t.Fatalf("应按顺序回调 3 个文档, got %v", got)
	}
}

// TestLazyParse_回调中断 测试 fn 返回 error 提前终止
func TestLazyParse_回调中断(t *testing.T) {
	p := newFakeMultiParser([]reranker.Document{
		{ID: "1", Text: "a"},
		{ID: "2", Text: "b"},
		{ID: "3", Text: "c"},
	}, nil)
	stop := errors.New("stop")
	var calls int
	err := p.LazyParse(context.Background(), "doc.txt", func(d reranker.Document) error {
		calls++
		if d.ID == "1" {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("应原样返回中断错误, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("应在第 1 个文档后终止, got %d 次回调", calls)
	}
}

// TestLazyParse_空结果 测试 Parse 返回空时 fn 零调用
func TestLazyParse_空结果(t *testing.T) {
	p := newFakeMultiParser(nil, nil)
	called := false
	err := p.LazyParse(context.Background(), "doc.txt", func(d reranker.Document) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if called {
		t.Fatal("空结果时 fn 不应被调用")
	}
}

// TestLazyParse_Parse错误传播 测试 Parse 错误经 LazyParse 返回
func TestLazyParse_Parse错误传播(t *testing.T) {
	sentinel := errors.New("boom")
	p := newFakeMultiParser(nil, sentinel)
	err := p.LazyParse(context.Background(), "doc.txt", func(d reranker.Document) error { return nil })
	if !errors.Is(err, sentinel) {
		t.Fatalf("应原样传播 Parse 错误, got %v", err)
	}
}

// ──────────────────────────── Process / Supports 测试 ────────────────────────────

// TestProcess_转发 测试 Process 等价 Parse
func TestProcess_转发(t *testing.T) {
	p := newFakeContentParser("hello", nil)
	docs, err := p.Process(context.Background(), "doc.txt", WithDocID("p-1"))
	if err != nil || len(docs) != 1 || docs[0].ID != "p-1" {
		t.Fatalf("Process 应转发 Parse: %v %#v", err, docs)
	}
}

// TestSupports_默认与覆写 测试默认 false 与覆写 true
func TestSupports_默认与覆写(t *testing.T) {
	p := newFakeContentParser("x", nil)
	if p.Supports("doc.txt") {
		t.Fatal("BaseParser 默认 Supports 应为 false")
	}
	full := &fakeFullParser{supports: true}
	if !full.Supports("http://x") {
		t.Fatal("覆写后 Supports 应为 true")
	}
}
