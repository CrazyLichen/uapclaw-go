package parser

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Parser 文档解析抽象接口。
//
// 五方法对齐 Python parse/lazy_parse/process/supports/_parse。
// 具体解析器嵌入 BaseParser 仅覆写 ParseContent（对齐 Python 子类
// 仅覆写 _parse）；组合解析器（13.11 AutoParser）覆写 Parse 整体路由。
//
// 知识库入库时经 KnowledgeBase.parse_files/parse_urls 调用（后续 KB 层章节）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/base.py (Parser)
type Parser interface {
	// Parse 解析文档源（文件路径/URL 等），返回 Document 列表。
	// 子类软失败（Python except→return None）由实现方返回空切片表达。
	Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error)
	// LazyParse 惰性解析：完整 Parse 后逐个回调 fn（对齐 Python lazy_parse
	// 默认实现"parse 完再逐个 yield"，非流式）。fn 返回 error 提前终止。
	LazyParse(ctx context.Context, doc string, fn func(reranker.Document) error, opts ...ParseOption) error
	// Process 处理文档（Processor 契约兼容），默认转发 Parse。
	Process(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error)
	// Supports 检查是否支持该文档源，默认 false。
	Supports(doc string) bool
	// ParseContent 子类覆写点（对齐 Python _parse(file_path, llm_client)），
	// 返回解析出的文本内容；空串表示无内容（对齐 Python None）。
	// 注意不收 Extra：Python **kwargs 在基类 parse 被接收但丢弃，
	// Extra 只留给覆写 Parse 的子类（如 13.11 AutoParser）。
	// 必须进接口：BaseParser 模板方法经 b.self 动态派发此方法
	// （13.5 chunkTextOf 同款机制）。
	ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error)
}

// ParseOptions Parser 可选参数集合。
//
// 收纳 Python parse 的三个可选参数 doc_id/llm_client/**kwargs
// （Python parse(self, doc, doc_id="", llm_client=None, **kwargs)）。
type ParseOptions struct {
	// DocID 文档 ID（Python doc_id）。未设置时 Document.ID 保持空串——
	// 字面对齐 Python：pydantic default_factory 只在"未传"时触发，
	// 显式传空串保持空串；Go options 无法区分"未设置"与"显式空串"。
	// 13.11 生产路径 AutoFileParser 预生成 uuid 传入。
	DocID string
	// LLMClient 可选 LLM 客户端（Python llm_client），13.15 Captioner 消费。
	LLMClient *llm.Model
	// Extra 额外参数（Python **kwargs），13.11 AutoFileParser 消费 file_name 等。
	Extra map[string]any
}

// ParseOption Parser 可选参数函数。
type ParseOption func(*ParseOptions)

// BaseParser 解析器默认适配器（模板方法）。
//
// 对齐 Python Parser 基类的可复用部分：parse 的单文档包装、
// lazy_parse 的逐个 yield、process 转发、supports 默认 False、
// _parse 的空实现。具体解析器嵌入本结构体 + 构造注入 self +
// 覆写 ParseContent；模板方法经 b.self 动态派发到子类覆写。
//
// Python: base.py (Parser)
type BaseParser struct {
	// self 完整 Parser 引用（子类构造时经 NewBaseParser 注入）
	self Parser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewParseOptions 从可变参数构建 ParseOptions。
func NewParseOptions(opts ...ParseOption) *ParseOptions {
	o := &ParseOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithDocID 设置文档 ID。
func WithDocID(docID string) ParseOption {
	return func(o *ParseOptions) {
		o.DocID = docID
	}
}

// WithLLMClient 设置 LLM 客户端。
func WithLLMClient(client *llm.Model) ParseOption {
	return func(o *ParseOptions) {
		o.LLMClient = client
	}
}

// WithExtra 设置额外参数键值（对齐 Python **kwargs 逐键传入）。
func WithExtra(key string, value any) ParseOption {
	return func(o *ParseOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}

// NewBaseParser 构造 BaseParser 并注入 self。
//
// self 为 nil 时返回错误：显式传 nil 无法靠编译期约束拦截
// （对齐 13.8 NewHybridChunker 的 inner nil 防御先例）。
//
// 子类构造模式：
//
//	func NewTxtMdParser() (*TxtMdParser, error) {
//	    p := &TxtMdParser{}
//	    bp, err := NewBaseParser(p)
//	    if err != nil { return nil, err }
//	    p.BaseParser = bp
//	    return p, nil
//	}
func NewBaseParser(self Parser) (*BaseParser, error) {
	if self == nil {
		return nil, fmt.Errorf("self parser is required for BaseParser")
	}
	return &BaseParser{self: self}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
