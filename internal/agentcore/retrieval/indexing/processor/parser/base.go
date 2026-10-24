package parser

import (
	"context"
	"fmt"
	"net/http"
	"time"

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
// （Python parse(self, doc, doc_id="", llm_client=None, **kwargs)）；
// 13.14 扩展网页解析四结构化字段（对齐 Python WebPageParser.parse 的
// kwargs 四键 timeout/user_agent/verify/client——运行时覆盖实例字段）。
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
	// Timeout 请求超时（Python timeout float 秒→Duration）。零值=未设，
	// 调用方回落实例字段。仅自建 client 时生效（注入 client 自带配置生效）。
	Timeout time.Duration
	// UserAgent HTTP UA 头（Python user_agent）。空串=未设，回落实例字段。
	UserAgent string
	// Verify TLS 校验模式（Python verify: bool|str|SSLContext）。nil=未设，
	// 回落实例字段。密封 sum type 四变体（13.14 定义于 web_page_parser.go）。
	Verify verifyMode
	// HTTPClient 注入共享客户端（Python httpx.AsyncClient）。非 nil 即生效，
	// 此时 Verify/Timeout/UserAgent 失效（client 自身配置生效）。
	HTTPClient *http.Client
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

// WithTimeout 设置请求超时（13.14 网页解析，对齐 Python kwargs timeout 键）。
func WithTimeout(d time.Duration) ParseOption {
	return func(o *ParseOptions) { o.Timeout = d }
}

// WithUserAgent 设置 HTTP User-Agent（13.14 网页解析，对齐 Python kwargs user_agent 键）。
func WithUserAgent(ua string) ParseOption {
	return func(o *ParseOptions) { o.UserAgent = ua }
}

// WithVerify 设置 TLS 校验模式（13.14 网页解析，对齐 Python kwargs verify 键）。
func WithVerify(v verifyMode) ParseOption {
	return func(o *ParseOptions) { o.Verify = v }
}

// WithHTTPClient 注入共享 HTTP 客户端（13.14 网页解析，对齐 Python kwargs client 键）。
func WithHTTPClient(c *http.Client) ParseOption {
	return func(o *ParseOptions) { o.HTTPClient = c }
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

// Parse 解析文档（默认实现，可被嵌入方继承）。
//
// 经 self 动态派发子类覆写的 ParseContent：非空内容包装为单文档
// （Metadata 显式空 map 非 nil，对齐 Python metadata={} 的 default_factory），
// 空内容返回空切片（对齐 Python if content: truthiness）。
// 钩子错误原样传播（软失败语义由子类自行返回空串实现）。
//
// Python: base.py Parser.parse
func (b *BaseParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	o := NewParseOptions(opts...)
	content, err := b.self.ParseContent(ctx, doc, o.LLMClient)
	if err != nil {
		return nil, err
	}
	if content == "" {
		return []reranker.Document{}, nil
	}
	return []reranker.Document{{
		ID:       o.DocID,
		Text:     content,
		Metadata: map[string]any{},
	}}, nil
}

// LazyParse 惰性解析（默认实现）。
//
// 先完整 Parse（对齐 Python lazy_parse 默认实现"parse 完再逐个 yield"，
// 非流式），再逐个回调 fn；fn 返回 error 时提前终止并原样返回
// （filepath.Walk 惯例）。经 self 派发，子类覆写 Parse 即生效。
//
// Python: base.py Parser.lazy_parse
func (b *BaseParser) LazyParse(ctx context.Context, doc string, fn func(reranker.Document) error, opts ...ParseOption) error {
	docs, err := b.self.Parse(ctx, doc, opts...)
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

// Process 处理文档（Processor 契约兼容），转发 Parse。
//
// Python: base.py Parser.process
func (b *BaseParser) Process(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	return b.self.Parse(ctx, doc, opts...)
}

// Supports 检查是否支持该文档源（默认 false，对齐 Python 基类）。
//
// Python: base.py Parser.supports
func (b *BaseParser) Supports(doc string) bool {
	return false
}

// ParseContent 子类覆写点（对齐 Python _parse(file_path, llm_client)）。
//
// 默认返回 ("", nil)——对齐 Python 基类 _parse 的 `pass` → None →
// parse 返回 []。BaseParser 单独不构成有意义的解析器。
//
// Python: base.py Parser._parse
func (b *BaseParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	_ = ctx // 对齐 Python 同步语义，当前无 ctx 消费
	_ = doc
	_ = llmClient
	return "", nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
