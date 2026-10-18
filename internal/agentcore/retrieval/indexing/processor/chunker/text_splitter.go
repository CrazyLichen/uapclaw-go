package chunker

import (
	"strings"

	"github.com/google/uuid"
	"github.com/tiktoken-go/tokenizer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TextSplitter 文本分割器抽象接口。
//
// 对齐 Python TextSplitter ABC（仅 split 一个抽象方法）。
// Python split 入参为 TextChunk | Document 联合类型，Go 侧收窄为
// *reranker.Document（生产调用方 CharChunker/TokenizerChunker 只传
// Document；13.7 IndexSentenceSplitter 的 TextChunk→Document 转换留在
// 实现内部）。error 返回为 13.7 预留，CharSplitter 恒返回 nil。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/text_splitter.py (TextSplitter)
type TextSplitter interface {
	// Split 将文档分割为文本块列表。
	Split(doc *reranker.Document) ([]common.TextChunk, error)
}

// CharSplitter 基于字符长度的分割器，不依赖 tokenizer。
//
// 构造为钳制语义（不报错）：chunkSize==0→默认 200、overlap→clamp
// [0, size-1]、size<1→1。与 BaseChunker 的"越界报错"相反是有意为之：
// 真实路径 CharChunker 已过基类校验，此处钳制是防御层（对齐 Python
// CharSplitter 原样行为）。
//
// Python: text_splitter.py (CharSplitter)
type CharSplitter struct {
	// ChunkSize 分块大小（字符数），构造后恒 ≥ 1
	ChunkSize int
	// ChunkOverlap 相邻块重叠（字符数），构造后恒 ∈ [0, ChunkSize-1]
	ChunkOverlap int
}

// Tokenizer token 编码器最小接口（Encode 必选能力）。
//
// 对齐 Python _resolve_tokenizer 分支②③（tokenize-only / callable）——
// Go 接口本身就是 callable 语义，两类分支天然合并。
//
// Python: text_splitter.py IndexSentenceSplitter._resolve_tokenizer（分支②③）
type Tokenizer interface {
	// Encode 编码文本为 token id 列表。失败上抛（决策 2b）。
	Encode(text string) ([]int, error)
}

// DecodeTokenizer 带 Decode 的完整 token 化器。
//
// 对齐 Python 分支①（HF encode+decode）：实现者使 SentenceSplitter
// 获得长句子切分能力（子窗口需 decode 回文本）。
//
// Python: text_splitter.py IndexSentenceSplitter._resolve_tokenizer（分支①）
type DecodeTokenizer interface {
	Tokenizer
	// Decode 解码 token id 列表为文本。
	Decode(ids []int) (string, error)
}

// MaxLengthProvider 可选长度上限提供者。
//
// 对齐 Python _max_length 的 4 属性 hasattr 探测（model_max_length/
// max_len_single_sentence/max_position_embeddings/max_seq_length），
// 简化为单方法——由实现者决定返回哪个语义的值（0 视为未提供）。
//
// Python: text_splitter.py IndexSentenceSplitter._max_length
type MaxLengthProvider interface {
	// MaxLength 返回模型最大序列长度；0 表示未提供。
	MaxLength() int
}

// TiktokenTokenizer tiktoken-go Codec 适配器。
//
// 对齐 Python 分支④（nil → lazy import tiktoken cl100k_base）。
// 不实现 MaxLengthProvider：Python 把词表大小 max_token_value 当
// max_length 是无意识行为，Go Codec 无该属性（设计文档偏差 1）。
//
// Python: text_splitter.py IndexSentenceSplitter._resolve_tokenizer（分支④）
type TiktokenTokenizer struct {
	// codec tiktoken 编解码器
	codec tokenizer.Codec
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultCharChunkSize CharSplitter 默认分块大小
	//（对齐 Python DEFAULT_CHAR_CHUNK_SIZE，注意与 CharChunker 的 512 是两套默认值）
	defaultCharChunkSize = 200
	// defaultCharChunkOverlap CharSplitter 默认重叠（对齐 Python DEFAULT_CHAR_CHUNK_OVERLAP）。
	// Go 无 None 语义：overlap 的 0 是合法显式值不做默认替换，需要默认行为时调用方显式传 40
	defaultCharChunkOverlap = 40
	// defaultSentenceChunkSize IndexSentenceSplitter 默认分块大小
	//（对齐 Python DEFAULT_CHUNK_SIZE=200；注意与 registry.go 的
	// defaultChunkSize=512 是两套语义——后者是 ChunkerOptions 工厂默认）
	defaultSentenceChunkSize = 200
	// defaultSafeEncodeMaxLength HF encode truncation 上限的文档锚点。
	//
	// Python: DEFAULT_SAFE_ENCODE_MAX_LENGTH = 65536（HF encode(max_length=...)
	// 的截断保护，防 tokenizer 不报 model_max_length 时的无界分配）。Go 接口
	// 无 truncation 参数概念，此常量仅作文档锚点保留，无运行时消费者
	//（设计文档决策 2c）。
	defaultSafeEncodeMaxLength = 65536
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 编译期断言 CharSplitter 实现 TextSplitter
	_ TextSplitter = (*CharSplitter)(nil)
	// 编译期引用 defaultCharChunkOverlap：Go 无 None 语义该默认值不参与运行时
	// 构造（overlap 0 为合法显式值），保留常量作为 Python 对齐锚点供文档引用
	_ = defaultCharChunkOverlap
	// 编译期引用 defaultSafeEncodeMaxLength：文档锚点常量无运行时消费者（决策 2c）
	_ = defaultSafeEncodeMaxLength
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCharSplitter 构造并钳制参数（不报错，对齐 Python CharSplitter.__init__）。
//
// 钳制规则（执行顺序对齐 Python 源码）：
//  1. chunkSize==0 → 200（对齐 `chunk_size or DEFAULT` 的 0 值语义；负数保持原值）
//  2. overlap → clamp [0, size-1]（size 为步骤 1 后的值）
//  3. size < 1 → 1
//
// Python: text_splitter.py CharSplitter.__init__
func NewCharSplitter(chunkSize, chunkOverlap int) *CharSplitter {
	size := chunkSize
	if size == 0 {
		size = defaultCharChunkSize
	}
	overlap := chunkOverlap
	if overlap > size-1 {
		overlap = size - 1
	}
	if overlap < 0 {
		overlap = 0
	}
	if size < 1 {
		size = 1
	}
	return &CharSplitter{ChunkSize: size, ChunkOverlap: overlap}
}

// NewTiktokenTokenizer 构造 cl100k_base 适配器。
//
// Python: _resolve_tokenizer 分支④ tiktoken.get_encoding("cl100k_base")
func NewTiktokenTokenizer() (*TiktokenTokenizer, error) {
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		return nil, err
	}
	return &TiktokenTokenizer{codec: codec}, nil
}

// Encode 编码文本（[]uint→[]int 转换 + 薄转发）。
func (t *TiktokenTokenizer) Encode(text string) ([]int, error) {
	ids, _, err := t.codec.Encode(text)
	if err != nil {
		return nil, err
	}
	out := make([]int, len(ids))
	for i, id := range ids {
		out[i] = int(id)
	}
	return out, nil
}

// Decode 解码 token id 列表（[]int→[]uint 转换 + 薄转发）。
func (t *TiktokenTokenizer) Decode(ids []int) (string, error) {
	uints := make([]uint, len(ids))
	for i, id := range ids {
		uints[i] = uint(id)
	}
	return t.codec.Decode(uints)
}

// Split 按滑窗切分文档。
//
// step = size-overlap（size≤overlap 时退化 step=size——钳制后不可达，
// 保留对齐 Python 源码）；每块生成 uuid，携带 doc_id 与 metadata
// （共享引用，对齐 Python）；rune 切片保证多字节字符（中文）不被截断。
//
// Python: text_splitter.py CharSplitter.split
func (s *CharSplitter) Split(doc *reranker.Document) ([]common.TextChunk, error) {
	if doc == nil {
		return []common.TextChunk{}, nil
	}
	runes := []rune(doc.Text)
	meta := doc.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}
	step := s.ChunkSize - s.ChunkOverlap
	if s.ChunkSize <= s.ChunkOverlap {
		step = s.ChunkSize
	}
	res := make([]common.TextChunk, 0)
	for start := 0; start < len(runes); start += step {
		end := start + s.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		res = append(res, common.TextChunk{
			ID:       uuid.NewString(),
			Text:     string(runes[start:end]),
			DocID:    doc.ID,
			Metadata: meta,
		})
	}
	return res, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveTokenizers 解析 Tokenizer 接口三态为函数对。
//
// 执行顺序严格对齐 Python _resolve_tokenizer：
//  1. MaxLengthProvider 断言（对齐 _max_length hasattr 探测）
//  2. DecodeTokenizer 断言（对齐 encode+decode 分支①）
//  3. 仅 Tokenizer（对齐 tokenize-only/callable 分支②③，类型系统合并）
//  4. nil → TiktokenTokenizer fallback；失败 Warn + 空格分词 proxy（偏差 4 保留）
//
// Python: text_splitter.py IndexSentenceSplitter._resolve_tokenizer
func resolveTokenizers(t Tokenizer) (func(string) ([]int, error), func([]int) (string, error), int) {
	maxLen := 0
	if t != nil {
		if ml, ok := t.(MaxLengthProvider); ok {
			maxLen = ml.MaxLength()
		}
	}

	if t != nil {
		if dt, ok := t.(DecodeTokenizer); ok {
			return wrapSafeEncode(dt.Encode), dt.Decode, maxLen
		}
		return wrapSafeEncode(t.Encode), nil, maxLen
	}

	// 分支④：nil → tiktoken cl100k_base（偏差 1：maxLen 恒 0）
	tk, err := NewTiktokenTokenizer()
	if err != nil {
		// tiktoken-go 编译期嵌入 BPE 表，Get 失败实际不可达；
		// 完整保留 Python 失败链：warning + 空格分词 proxy（偏差 4）
		logger.Warn(logComponent).
			Err(err).
			Msg("Failed to load tiktoken fallback tokenizer")
		return func(text string) ([]int, error) {
			words := strings.Fields(text)
			ids := make([]int, len(words))
			for i := range words {
				ids[i] = i
			}
			return ids, nil
		}, nil, 0
	}
	return tk.Encode, tk.Decode, 0
}

// wrapSafeEncode 包装 Encode：空白文本返回空切片。
//
// 对齐 Python safe_encode 的 `if not (text and text.strip()): return []`；
// 其余直调（失败上抛，决策 2b——Go 无 truncation 参数，裸重试场景不存在）。
func wrapSafeEncode(enc func(string) ([]int, error)) func(string) ([]int, error) {
	return func(text string) ([]int, error) {
		if strings.TrimSpace(text) == "" {
			return []int{}, nil
		}
		return enc(text)
	}
}

// resolveChunkSize 决定最终 chunk_size。
//
// 对齐 Python _resolve_chunk_size：未设(0)+有max→max；都设→min；
// 否则 chunkSize（0 则 DEFAULT_CHUNK_SIZE）。Go 无 None，0 表示未设。
//
// Python: text_splitter.py IndexSentenceSplitter._resolve_chunk_size
func resolveChunkSize(chunkSize, maxTokenLength int) int {
	if chunkSize == 0 && maxTokenLength > 0 {
		return maxTokenLength
	}
	if chunkSize > 0 && maxTokenLength > 0 {
		if chunkSize < maxTokenLength {
			return chunkSize
		}
		return maxTokenLength
	}
	if chunkSize > 0 {
		return chunkSize
	}
	return defaultSentenceChunkSize
}
