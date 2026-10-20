package chunker

import (
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TokenizerProvider 可选 tokenizer 提供者。
//
// 对齐 Python getattr(embed_model, "tokenizer", None)：embed_model 类型
// 为 Any，Go 侧以 any 参数 + 类型断言提取（B 决策）。
type TokenizerProvider interface {
	// Tokenizer 返回嵌入模型关联的分词器；nil 表示未提供。
	Tokenizer() Tokenizer
}

// TextChunker 分块工具门面：预处理管线 + 单位路由组合的一站式分块器。
//
// 嵌入 BaseChunker 获得尺寸字段与校验；ChunkText 不被本类型支持
//（返回显式错误，A1 决策）；ChunkDocuments 完全覆写（预处理内嵌）；
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
//（normalize_whitespace/remove_url_email）；未知键 Python 静默忽略，
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
	// embedModel 嵌入模型（any，断言 TokenizerProvider 提取 tokenizer）
	embedModel any
	// preprocessOptions 预处理开关（nil 表示不开）
	preprocessOptions *PreprocessOptions
}

// TextChunkerOption TextChunker 构造选项函数。
type TextChunkerOption func(*textChunkerOptions)

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 TextChunker 实现 Chunker（Task 5 落地三方法后启用）
// var _ Chunker = (*TextChunker)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithChunkUnit 设置分块单位（对齐 Python chunk_unit 参数，默认 "char"）。
func WithChunkUnit(unit string) TextChunkerOption {
	return func(o *textChunkerOptions) { o.chunkUnit = unit }
}

// WithEmbedModel 设置嵌入模型（对齐 Python embed_model 参数）。
//
// token 路径经 TokenizerProvider 断言提取 tokenizer。
func WithEmbedModel(model any) TextChunkerOption {
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
	// SetSelf 注入完整 Chunker 引用（Task 5 落地三方法后启用）
	// bc.SetSelf(c)
	return c, nil
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
func getChunker(chunkSize, chunkOverlap int, chunkUnit string, embedModel any) (Chunker, error) {
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
