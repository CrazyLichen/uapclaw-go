package kbadmin

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/processor/chunker"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/processor/extractor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/processor/parser"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/knowledgebase"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrExtractingNotImplemented extracting 阶段唯一未实现分支（⤵️30.4，spec §1 拍板）
var ErrExtractingNotImplemented = errors.New("extracting stage not implemented (30.4)")

// ──────────────────────────── 结构体 ────────────────────────────

// ParseJob 单次解析任务执行上下文
type ParseJob struct {
	// TaskID/DocID/KBID 任务与归属标识
	TaskID string
	DocID  string
	KBID   string
	// KBType 库类型（document/graph；MultiKB 不入此链——spec §2）
	KBType string
	// FilePath 落盘文件绝对路径
	FilePath string
	// Config KB config 解析结果
	Config KBConfig

	// docs parsing 中间产物
	docs []reranker.Document
	// chunks chunking 中间产物（embed/triple 步与台账 id 对齐用）
	chunks []common.TextChunk
}

// stageStep 声明式步骤（worker 骨架零类型判断——spec §1 拍板）
type stageStep struct {
	// name 阶段名（stages_json 记录键）
	name string
	// run 步骤执行体
	run func(ctx context.Context, job *ParseJob, env *pipelineEnv) error
}

// pipelineEnv 步骤共享装配环境
type pipelineEnv struct {
	// store kbadmin SQLite 台账
	store *Store
	// kbFactory collection/indexer 构造接线
	kbFactory *RetrievalKBFactory
	// embedModel embedding 客户端（KB config 构造）
	embedModel embedding.BaseEmbedding
	// tripleExtractor graph 类型专用（nil 时 triple 步跳过——UseGraph=false 等价语义）
	tripleExtractor extractor.Extractor
	// sink 进度观察者
	sink *ProgressSink
}

// ──────────────────────────── 常量 ────────────────────────────

// pipelinePlans 类型→步骤计划（spec §2：新增类型 = 加一行声明，worker 零改动）
var pipelinePlans = map[string][]stageStep{
	"document": {
		{name: "parsing", run: parseStep},
		{name: "chunking", run: chunkStep},
		{name: "embedding", run: embedStep},
	},
	"graph": {
		{name: "parsing", run: parseStep},
		{name: "chunking", run: chunkStep},
		{name: "embedding", run: embedStep},
		{name: "triple", run: tripleStep},
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// runPipeline 执行骨架：按序执行、每步 ctx 检查、错误即停、每步向 sink 上报
func runPipeline(ctx context.Context, job *ParseJob, steps []stageStep, env *pipelineEnv) error {
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if env.sink != nil {
			env.sink.OnStageStart(job, step.name)
		}
		if err := step.run(ctx, job, env); err != nil {
			if env.sink != nil {
				env.sink.OnStageError(job, step.name, err)
			}
			return err
		}
		if env.sink != nil {
			env.sink.OnStageEnd(job, step.name)
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseStep parsing：AutoFileParser 按扩展名路由解析（真链复用 13.x 组件；
// 语义对齐 SimpleKB 导入链的解析段）
func parseStep(ctx context.Context, job *ParseJob, env *pipelineEnv) error {
	ap, err := parser.NewAutoFileParser()
	if err != nil {
		return err
	}
	docs, err := ap.Process(ctx, job.FilePath)
	if err != nil {
		return err
	}
	// 文档 ID 统一为 kb_doc.ID（向量库 doc_id 维度对齐；删除/重建以 docID 为粒度）
	for i := range docs {
		docs[i].ID = job.DocID
	}
	job.docs = docs
	return nil
}

// chunkStep chunking：TextChunker 切分 + 中间产物落 kb_chunk_meta
// （台账只存管理字段不存正文——spec §1 拍板；chunk 正文一份在向量库）
func chunkStep(ctx context.Context, job *ParseJob, env *pipelineEnv) error {
	tc, err := chunker.NewTextChunker(job.Config.Chunker.ChunkSize, job.Config.Chunker.ChunkOverlap)
	if err != nil {
		return err
	}
	chunks, err := tc.ChunkDocuments(ctx, job.docs)
	if err != nil {
		return err
	}
	for i := range chunks {
		if chunks[i].ID == "" {
			chunks[i].ID = uuid.NewString()
		}
		meta := &KBChunkMeta{ID: chunks[i].ID, DocID: job.DocID, KBID: job.KBID,
			ChunkType: "text", Enabled: true, Revision: 1}
		if err := env.store.Chunk().CreateMeta(meta); err != nil {
			return err
		}
	}
	job.chunks = chunks
	return nil
}

// embedStep embedding：ChromaIndexer.BuildIndex（IndexConfig 构造逐字段对照
// graph_kb.go:110-116；错误码语义对齐 155505/RetrievalKbChunkIndexBuildExecutionError）
func embedStep(ctx context.Context, job *ParseJob, env *pipelineEnv) error {
	idx, err := env.kbFactory.CreateIndexer(ctx, job.KBID)
	if err != nil {
		return err
	}
	indexConfig := common.IndexConfig{
		IndexName:           "kb_" + job.KBID + "_chunks",
		IndexType:           "vector",
		UseCaptionForImages: false,
	}
	// 重解析语义（spec §3）：先删旧 doc 索引再建（幂等；不存在时删除为空操作）。
	// 索引器为工厂缓存单实例（同 client 读一致，dedup 不会误报），错误真实上抛
	if _, derr := idx.DeleteIndex(ctx, job.DocID, "kb_"+job.KBID+"_chunks"); derr != nil {
		logger.Warn(logComponent).Str("doc_id", job.DocID).Err(derr).
			Msg("delete stale doc index before build failed")
	}
	success, err := idx.BuildIndex(ctx, job.chunks, indexConfig, env.embedModel,
		indexer.WithIndexExtra("database_name", "kb_"+job.KBID))
	if err != nil {
		return err
	}
	if !success {
		return errors.New("Failed to build chunk index")
	}
	return nil
}

// tripleStep graph 追加步骤：TripleExtractor 抽取 + triple 索引
// （逐行对照 graph_kb.go:126-156：抽取→空集跳过→TriplesToTextChunks→BuildIndex，
// use_caption_for_images 硬编码 true——三元组恒走 text-only 嵌入路径）
func tripleStep(ctx context.Context, job *ParseJob, env *pipelineEnv) error {
	if env.tripleExtractor == nil {
		// 未装配抽取器 = UseGraph 关闭等价语义：跳过不失败
		logger.Info(logComponent).Str("kb_id", job.KBID).
			Msg("Graph extractor not configured, skipping triple index")
		return nil
	}
	idx, err := env.kbFactory.CreateIndexer(ctx, job.KBID)
	if err != nil {
		return err
	}
	triples, err := env.tripleExtractor.Extract(ctx, job.chunks)
	if err != nil {
		return err
	}
	if len(triples) > 0 {
		tripleConfig := common.IndexConfig{
			IndexName:           "kb_" + job.KBID + "_triples",
			IndexType:           "vector",
			UseCaptionForImages: true,
		}
		tripleChunks := knowledgebase.TriplesToTextChunks(triples)
		success, err := idx.BuildIndex(ctx, tripleChunks, tripleConfig, env.embedModel,
			indexer.WithIndexExtra("database_name", "kb_"+job.KBID))
		if err != nil {
			return err
		}
		if !success {
			return errors.New("Failed to build triple index")
		}
		logger.Info(logComponent).Int("triple_chunk_count", len(tripleChunks)).
			Msg("Built triple index with triples")
	}
	return nil
}


