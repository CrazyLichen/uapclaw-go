package knowledgebase

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/chunker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/extractor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/parser"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseKnowledgeBase 子类嵌入的共享基座（13.10 BaseParser 先例）。
//
// 持有知识库的 7 大组件与配置；模板方法（ParseFiles/ParseURLs/
// DeleteCollection/Close）与配置一致性防线由基座提供，5 抽象方法
// 由 SimpleKnowledgeBase（13.27）/GraphKnowledgeBase（13.28）实现。
//
// Python: knowledge_base.py (KnowledgeBase 的 __init__ 部分)
type BaseKnowledgeBase struct {
	// strictValidation 严格校验开关（Python strict_validation，默认 true）
	strictValidation bool
	// config 知识库配置
	config common.KnowledgeBaseConfig
	// vectorStore 读侧组件（检索）
	vectorStore vector_store.VectorStore
	// embedModel 嵌入模型
	embedModel embedding.BaseEmbedding
	// parser 文档解析器
	parser parser.Parser
	// chunker 文本分块器
	chunker chunker.Chunker
	// extractor 信息抽取器（如三元组）
	extractor extractor.Extractor
	// indexManager 写侧组件（建索引）
	indexManager indexer.Indexer
	// llmClient LLM 客户端（agentic 检索等）
	llmClient model_clients.BaseModelClient
}

// KBOptions 知识库方法选项（Python **kwargs 收纳）。
type KBOptions struct {
	// FileName ParseFiles 的 file_name 参数（空串=未设回落路径末段）
	FileName string
	// Extra 其余额外参数：ParseURLs 全量透传 parser；抽象方法子类消费
	Extra map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// KnowledgeBaseOption BaseKnowledgeBase 构造选项。
type KnowledgeBaseOption func(*BaseKnowledgeBase)

// NewBaseKnowledgeBase 构造知识库基座。
//
// 构造尾部：vectorStore 与 indexManager 双非空时触发 postAssignValidation
// （对齐 Python __init__ 赋值序：index_manager 赋值时刻两者配齐触发）。
func NewBaseKnowledgeBase(config common.KnowledgeBaseConfig, opts ...KnowledgeBaseOption) (*BaseKnowledgeBase, error) {
	b := &BaseKnowledgeBase{
		strictValidation: true, // Python 默认 true
		config:           config,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.vectorStore != nil && b.indexManager != nil {
		if err := b.postAssignValidation(context.Background()); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// WithVectorStore 设置读侧组件。
func WithVectorStore(vs vector_store.VectorStore) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.vectorStore = vs }
}

// WithEmbedModel 设置嵌入模型。
func WithEmbedModel(em embedding.BaseEmbedding) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.embedModel = em }
}

// WithParser 设置解析器。
func WithParser(p parser.Parser) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.parser = p }
}

// WithChunker 设置分块器。
func WithChunker(c chunker.Chunker) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.chunker = c }
}

// WithExtractor 设置抽取器。
func WithExtractor(e extractor.Extractor) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.extractor = e }
}

// WithIndexManager 设置写侧组件。
func WithIndexManager(im indexer.Indexer) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.indexManager = im }
}

// WithLLMClient 设置 LLM 客户端。
func WithLLMClient(c model_clients.BaseModelClient) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.llmClient = c }
}

// WithStrictValidationFalse 关闭严格校验（Python strict_validation=False）。
func WithStrictValidationFalse() KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.strictValidation = false }
}

// Config 返回知识库配置（Python kb.config 属性镜像）。
func (b *BaseKnowledgeBase) Config() common.KnowledgeBaseConfig { return b.config }

// StrictValidation 返回严格校验开关。
func (b *BaseKnowledgeBase) StrictValidation() bool { return b.strictValidation }

// VectorStore 返回读侧组件。
func (b *BaseKnowledgeBase) VectorStore() vector_store.VectorStore { return b.vectorStore }

// IndexManager 返回写侧组件。
func (b *BaseKnowledgeBase) IndexManager() indexer.Indexer { return b.indexManager }

// ──────────────────────────── 非导出函数 ────────────────────────────
