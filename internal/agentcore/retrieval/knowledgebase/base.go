package knowledgebase

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/chunker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/extractor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/parser"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// KnowledgeBase 知识库抽象接口（Python ABC：5 抽象 + 4 模板 + Config 访问器）。
//
// 5 抽象方法由 SimpleKnowledgeBase（13.27）/GraphKnowledgeBase（13.28）
// 实现；4 模板方法由 BaseKnowledgeBase 提供默认实现。
type KnowledgeBase interface {
	// AddDocuments 添加文档到知识库（抽象）
	AddDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error)
	// Retrieve 检索相关文档（抽象）。config nil = 用默认值。
	Retrieve(ctx context.Context, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error)
	// DeleteDocuments 删除文档（抽象）
	DeleteDocuments(ctx context.Context, docIDs []string, opts ...KBOption) (bool, error)
	// UpdateDocuments 更新文档（抽象）
	UpdateDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error)
	// GetStatistics 获取知识库统计（抽象）
	GetStatistics(ctx context.Context) (map[string]any, error)
	// ParseFiles 解析文件列表为 Document（模板）
	ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error)
	// ParseURLs 解析 URL 列表为 Document（模板）
	ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error)
	// DeleteCollection 删除集合（模板）
	DeleteCollection(ctx context.Context, collection string) error
	// Close 关闭并释放资源（模板）
	Close()
	// Config 返回知识库配置
	Config() common.KnowledgeBaseConfig
}

// 编译期断言：BaseKnowledgeBase 满足模板方法与 Config 部分。
// （5 抽象方法未实现——完整 KnowledgeBase 断言由 13.27 SimpleKnowledgeBase 承接）
var _ interface {
	ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error)
	ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error)
	DeleteCollection(ctx context.Context, collection string) error
	Close()
	Config() common.KnowledgeBaseConfig
} = (*BaseKnowledgeBase)(nil)

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

// KnowledgeBaseOption BaseKnowledgeBase 构造选项。
type KnowledgeBaseOption func(*BaseKnowledgeBase)

// KBOptions 知识库方法选项（Python **kwargs 收纳）。
type KBOptions struct {
	// FileName ParseFiles 的 file_name 参数（空串=未设回落路径末段）
	FileName string
	// Extra 其余额外参数：ParseURLs 全量透传 parser；抽象方法子类消费
	Extra map[string]any
}

// KBOption 知识库方法选项。
type KBOption func(*KBOptions)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent knowledgebase 包日志组件常量。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

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

// SetVectorStore 构造后替换读侧组件（Python kb.vector_store = vs 显式对应物）。
//
// 赋值后双组件非空时触发 postAssignValidation（对齐 Python __setattr__）。
func (b *BaseKnowledgeBase) SetVectorStore(vs vector_store.VectorStore) error {
	b.vectorStore = vs
	if b.vectorStore != nil && b.indexManager != nil {
		return b.postAssignValidation(context.Background())
	}
	return nil
}

// SetIndexManager 构造后替换写侧组件（Python kb.index_manager = im 显式对应物）。
func (b *BaseKnowledgeBase) SetIndexManager(im indexer.Indexer) error {
	b.indexManager = im
	if b.vectorStore != nil && b.indexManager != nil {
		return b.postAssignValidation(context.Background())
	}
	return nil
}

// NewKBOptions 从可变参数构建 KBOptions。
func NewKBOptions(opts ...KBOption) KBOptions {
	o := KBOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithKBFileName 设置 file_name（ParseFiles 专用）。
func WithKBFileName(name string) KBOption {
	return func(o *KBOptions) { o.FileName = name }
}

// WithKBExtra 设置额外参数键值（ParseURLs 透传/抽象方法子类消费）。
func WithKBExtra(key string, value any) KBOption {
	return func(o *KBOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}

// ParseFiles 解析文件列表为 Document 列表（模板方法）。
//
// 单文件解析失败 Error 日志后继续（Python 字面）。
// 只透传 file_name（Python parse 只收 file_name/file_id——与 ParseURLs 不对称）。
//
// Python: knowledge_base.py parse_files
func (b *BaseKnowledgeBase) ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error) {
	if b.parser == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbParserNotFound,
			exception.WithMsg("parser is required for parse_files"),
		)
	}
	kbOpts := NewKBOptions(opts...)
	var allDocuments []reranker.Document
	for _, filePath := range filePaths {
		fileName := kbOpts.FileName
		if fileName == "" {
			// Python 字面：kwargs.get("file_name", file_path.split("/")[-1])
			parts := strings.Split(filePath, "/")
			fileName = parts[len(parts)-1]
		}
		fileID := uuid.NewString()
		documents, err := b.parser.Parse(ctx, filePath,
			parser.WithDocID(fileID), parser.WithExtra("file_name", fileName))
		if err != nil {
			logger.Error(logComponent).Str("file_path", filePath).Err(err).Msg("Failed to parse file")
			continue
		}
		allDocuments = append(allDocuments, documents...)
	}
	return allDocuments, nil
}

// ParseURLs 解析 URL 列表为 Document 列表（模板方法）。
//
// supports 不过 → Warn 跳过；单 URL 失败 Error 继续；
// KBOptions.Extra 全量透传 parser（Python **kwargs 直通）。
//
// Python: knowledge_base.py parse_urls
func (b *BaseKnowledgeBase) ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error) {
	if b.parser == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbParserNotFound,
			exception.WithMsg("parser is required for parse_urls"),
		)
	}
	kbOpts := NewKBOptions(opts...)
	var allDocuments []reranker.Document
	for _, url := range urls {
		if !b.parser.Supports(url) {
			logger.Warn(logComponent).Str("url", url).Msg("Parser does not support URL, skipping")
			continue
		}
		docID := uuid.NewString()
		parseOpts := []parser.ParseOption{parser.WithDocID(docID)}
		for k, v := range kbOpts.Extra {
			parseOpts = append(parseOpts, parser.WithExtra(k, v))
		}
		documents, err := b.parser.Parse(ctx, url, parseOpts...)
		if err != nil {
			logger.Error(logComponent).Str("url", url).Err(err).Msg("Failed to parse URL")
			continue
		}
		allDocuments = append(allDocuments, documents...)
	}
	return allDocuments, nil
}

// DeleteCollection 删除当前数据库中的一个集合（模板方法）。
//
// Python: knowledge_base.py delete_collection
func (b *BaseKnowledgeBase) DeleteCollection(ctx context.Context, collection string) error {
	if b.vectorStore == nil {
		return exception.BuildError(
			exception.StatusRetrievalKbVectorStoreNotFound,
			exception.WithMsg("vector_store is required for delete_collection"),
		)
	}
	return b.vectorStore.DeleteTable(ctx, collection)
}

// Close 关闭知识库并释放资源（模板方法）。
//
// 逐组件关闭（vectorStore → indexManager 顺序字面）；
// void Close 直调；error Close 变体失败 Warn 吞（Python
// _maybe_await 异常 Warn 同构；async 形态编译期消解，偏差 §8-3）。
//
// Python: knowledge_base.py close
func (b *BaseKnowledgeBase) Close() {
	b.maybeClose(b.vectorStore)
	b.maybeClose(b.indexManager)
}

// maybeClose 关闭单个组件（双变体可选接口）。
func (b *BaseKnowledgeBase) maybeClose(obj any) {
	if obj == nil {
		return
	}
	if c, ok := obj.(interface{ Close() }); ok {
		c.Close()
		return
	}
	if c, ok := obj.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to close object")
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
