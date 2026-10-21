package chunker

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TokenizerProvider 可选 tokenizer 提供者。
//
// 对齐 Python getattr(embed_model, "tokenizer", None) 的鸭子探测：
// embed_model 收窄为 embedding.BaseEmbedding（审查决策 4——Python 侧
// 标注 Any 但生态内实际只传 Embedding；Embedding 基类与全部实现类
// 均无 tokenizer 属性，探测分支在 Python 生态内是死路径，Go 侧保留
// 断言语义供自定义嵌入模型启用）。
type TokenizerProvider interface {
	// Tokenizer 返回嵌入模型关联的分词器；nil 表示未提供。
	Tokenizer() Tokenizer
}

// TextChunker 分块工具门面：预处理管线 + 单位路由组合的一站式分块器。
//
// 嵌入 BaseChunker 获得尺寸字段与校验；ChunkText 不被本类型支持
// （返回显式错误，A1 决策）；ChunkDocuments 完全覆写（预处理内嵌）；
// Process 局部覆写转发（13.8 决策 3 同款）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/chunking.py (TextChunker)
type TextChunker struct {
	// BaseChunker 默认适配器（ChunkSize/ChunkOverlap）
	*BaseChunker
	// pipeline 预处理管线（构造时由 preprocessOptions 组装）
	pipeline *PreprocessingPipeline
	// inner 路由产物（CharChunker 或 TokenizerChunker）
	inner Chunker
}

// PreprocessOptions 预处理开关。
//
// 对齐 Python preprocess_options dict 的 2 个已知键
// （normalize_whitespace/remove_url_email）；未知键 Python 静默忽略，
// 结构体天然收窄（偏差 2）。
type PreprocessOptions struct {
	// NormalizeWhitespace 启用空白归一化（组装序：先）
	NormalizeWhitespace bool
	// RemoveURLEmail 启用 URL/邮箱移除（组装序：后）
	RemoveURLEmail bool
}

// textChunkerOptions 构造选项聚合。
type textChunkerOptions struct {
	// chunkUnit 分块单位（"char"|"token"，默认 "char"）
	chunkUnit string
	// embedModel 嵌入模型（断言 TokenizerProvider 提取 tokenizer）
	embedModel embedding.BaseEmbedding
	// preprocessOptions 预处理开关（nil 表示不开）
	preprocessOptions *PreprocessOptions
}

// TextChunkerOption TextChunker 构造选项函数。
type TextChunkerOption func(*textChunkerOptions)

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 TextChunker 实现 Chunker
var _ Chunker = (*TextChunker)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithChunkUnit 设置分块单位（对齐 Python chunk_unit 参数，默认 "char"）。
func WithChunkUnit(unit string) TextChunkerOption {
	return func(o *textChunkerOptions) { o.chunkUnit = unit }
}

// WithEmbedModel 设置嵌入模型（对齐 Python embed_model 参数）。
//
// token 路径经 TokenizerProvider 断言提取 tokenizer（接口到接口断言，
// 语义对齐 Python getattr 鸭子探测）。
func WithEmbedModel(model embedding.BaseEmbedding) TextChunkerOption {
	return func(o *textChunkerOptions) { o.embedModel = model }
}

// WithPreprocessOptions 设置预处理开关（对齐 Python preprocess_options 参数）。
func WithPreprocessOptions(opts PreprocessOptions) TextChunkerOption {
	return func(o *textChunkerOptions) { o.preprocessOptions = &opts }
}

// NewTextChunker 构造 TextChunker。
//
// 构造链严格对齐 Python __init__ 执行序：
//  1. super().__init__（NewBaseChunker 校验 155100/155101）
//  2. preprocessors 组装：normalize_whitespace → WhitespaceNormalizer、
//     remove_url_email → URLEmailRemover（顺序：whitespace 先、url 后）
//  3. getChunker 路由（char → CharChunker；token → tokenizer 提取链）
//
// Python: chunking.py TextChunker.__init__
func NewTextChunker(chunkSize, chunkOverlap int, opts ...TextChunkerOption) (*TextChunker, error) {
	o := &textChunkerOptions{chunkUnit: "char"}
	for _, opt := range opts {
		opt(o)
	}

	bc, err := NewBaseChunker(chunkSize, chunkOverlap)
	if err != nil {
		return nil, err
	}

	var preprocessors []TextPreprocessor
	if o.preprocessOptions != nil {
		if o.preprocessOptions.NormalizeWhitespace {
			preprocessors = append(preprocessors, WhitespaceNormalizer{})
		}
		if o.preprocessOptions.RemoveURLEmail {
			preprocessors = append(preprocessors, URLEmailRemover{RemoveURLs: true, RemoveEmails: true})
		}
	}
	pipeline := NewPreprocessingPipeline(preprocessors...)

	inner, err := getChunker(chunkSize, chunkOverlap, o.chunkUnit, o.embedModel)
	if err != nil {
		return nil, err
	}

	c := &TextChunker{BaseChunker: bc, pipeline: pipeline, inner: inner}
	bc.SetSelf(c)
	return c, nil
}

// ChunkText 返回显式错误。
//
// Python 继承基类 chunk_text 返回 None（事实不可用——调用者迭代即崩），
// Go 无 None 语义，显式错误是最贴近表达（A1 决策；对齐 base.go
// chunkTextOf 的 standalone 错误先例）。
//
// Python: chunking.py TextChunker（未覆写 chunk_text，继承 base.py L79 return None）
func (c *TextChunker) ChunkText(text string) ([]string, error) {
	return nil, fmt.Errorf("TextChunker does not support ChunkText; use ChunkDocuments instead")
}

// ChunkDocuments 分块文档列表（完全覆写：预处理内嵌在循环）。
//
// 对齐 Python chunk_documents L97-125 执行序：每文档
// doc_text = pipeline(doc.text) → self.chunker.chunk_text(doc_text)（注意
// 调 inner 的 ChunkText，非本类型覆写版）→ 组块（uuid + 溯源三键，
// mergeChunkMetadata 复用 13.8 提取的 base.go 函数）。
//
// Python: chunking.py TextChunker.chunk_documents
func (c *TextChunker) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	_ = ctx // 对齐 Python 同步签名，当前无 ctx 消费
	chunks := make([]common.TextChunk, 0)
	for _, doc := range docs {
		docText := doc.Text
		if c.pipeline != nil {
			docText = c.pipeline.Process(docText)
		}
		texts, err := c.inner.ChunkText(docText)
		if err != nil {
			return nil, err
		}
		for i, text := range texts {
			uid := uuid.NewString()
			meta := mergeChunkMetadata(doc.Metadata, i, len(texts), uid)
			chunks = append(chunks, common.TextChunk{
				ID:       uid,
				Text:     text,
				DocID:    doc.ID,
				Metadata: meta,
			})
		}
	}
	return chunks, nil
}

// Process 处理文档（Processor 契约入口），局部覆写转发到自身 ChunkDocuments。
//
// BaseChunker.Process 静态解析无法路由子类覆写的 ChunkDocuments（Go 嵌入
// 是组合非继承），与 HybridChunker 同款局部覆写（13.8 决策 3 预警的惯例）。
//
// Python: chunking.py（继承 base.py process → 动态分发到覆写版 chunk_documents）
func (c *TextChunker) Process(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	return c.ChunkDocuments(ctx, docs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getChunker 单位路由（对齐 Python get_chunker 方法）。
//
// 执行序严格对齐 Python chunking.py L56-95：
//  1. chunk_unit == "char" → CharChunker
//  2. 否则：embedModel 断言 TokenizerProvider 提取 tokenizer
//  3. tokenizer == nil → tiktoken fallback（155102 失败分支保留，
//     "未安装"分支 Go 不可达——编译期依赖，偏差 3）
//  4. chunk_size 超限下调（MaxLengthProvider 断言，0=未提供）
//  5. NewTokenizerChunker（language="auto"/splitterConfig=nil 为 Python 默认值）
//
// Python: chunking.py TextChunker.get_chunker
func getChunker(chunkSize, chunkOverlap int, chunkUnit string, embedModel embedding.BaseEmbedding) (Chunker, error) {
	if chunkUnit == "char" {
		return NewCharChunker(WithChunkSize(chunkSize), WithChunkOverlap(chunkOverlap))
	}

	var tk Tokenizer
	if embedModel != nil {
		if p, ok := embedModel.(TokenizerProvider); ok {
			tk = p.Tokenizer()
		}
	}

	if tk == nil {
		tt, err := NewTiktokenTokenizer()
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalIndexingTokenizerProcessError,
				exception.WithParam("error_msg", "Failed to load tokenizer for token-based chunking: "+err.Error()),
			)
		}
		tk = tt
		// 对齐 Python: logger.info("Using tiktoken(cl100k_base) as tokenizer")
		logger.Info(logComponent).Msg("Using tiktoken(cl100k_base) as tokenizer")
	}

	if p, ok := tk.(MaxLengthProvider); ok {
		if maxLen := p.MaxLength(); maxLen > 0 && chunkSize > maxLen {
			original := chunkSize
			chunkSize = maxLen
			// 对齐 Python: logger.warning("chunk_size (%d) exceeds tokenizer limit, automatically adjusted to: %d")
			logger.Warn(logComponent).
				Int("original_size", original).
				Int("chunk_size", chunkSize).
				Msg("chunk_size exceeds tokenizer limit, automatically adjusted")
		}
	}

	return NewTokenizerChunker(chunkSize, chunkOverlap, tk, "auto", nil)
}
