package indexer

// Package indexer 内 ChromaBleveIndexer：构建在 ChromaBleveStore（chroma 向量 +
// Bleve 倒排组合存储，spec 2026-12-08 §10）之上的入库侧索引管理器（Python 无对应物）。
//
// 与 ChromaIndexer 的差异（spec §10 登记）：
//   - 构建在组合 store 之上：chroma 与 Bleve 双写一致性收口到 store.Add 单咽喉
//   - 去重：chroma collection.Get 查重跳过 → 由编排层「先删后建」保证
//     （kbadmin 全链唯一调用形态；重复 Add 同 id 会触发 chroma 主键冲突上抛）
//   - DeleteIndex 经 store.DeleteByDocID（Bleve 按 doc_id 字段 + chroma 按 filter 双删）

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	vs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChromaBleveIndexer chroma+Bleve 组合存储的入库侧索引管理器
type ChromaBleveIndexer struct {
	// store 组合存储（chroma 向量 + Bleve 倒排，单实例共享读写）
	store *vs.ChromaBleveStore
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChromaBleveIndexer 构造（store 为 factory 缓存的组合存储单实例）
func NewChromaBleveIndexer(store *vs.ChromaBleveStore) *ChromaBleveIndexer {
	return &ChromaBleveIndexer{store: store}
}

// DatabaseName 返回数据库名（13.26 validate_index 消费）
func (ci *ChromaBleveIndexer) DatabaseName() string { return ci.store.DatabaseName() }

// TextField 返回文本字段名
func (ci *ChromaBleveIndexer) TextField() string { return ci.store.TextField() }

// VectorField 返回向量字段配置
func (ci *ChromaBleveIndexer) VectorField() any { return ci.store.VectorField() }

// SparseVectorField 返回稀疏向量字段名
func (ci *ChromaBleveIndexer) SparseVectorField() string { return ci.store.SparseVectorField() }

// MetadataField 返回元数据字段名
func (ci *ChromaBleveIndexer) MetadataField() string { return ci.store.MetadataField() }

// DocIDField 返回文档 ID 字段名
func (ci *ChromaBleveIndexer) DocIDField() string { return ci.store.DocIDField() }

// Store 返回底层组合存储（检索侧复用同一实例）
func (ci *ChromaBleveIndexer) Store() *vs.ChromaBleveStore { return ci.store }

// Close 关闭索引管理器（空操作，对齐 ChromaIndexer 惯例：store 由 factory
// CloseAll 统一关闭，单实例共享不能单点关闭）
func (ci *ChromaBleveIndexer) Close() error { return nil }

// BuildIndex 构建索引：嵌入 → 组合 store 双写（chroma 向量 + Bleve 倒排）。
// 2 步流程（对照 ChromaIndexer 4 步：去重由编排层先删后建保证、转换在 store.Add 内联）。
func (ci *ChromaBleveIndexer) BuildIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	if len(chunks) == 0 {
		return false, nil
	}
	// ① 嵌入（与 chroma/sqlite 共用 embed_chunks 助手）
	if embedModel != nil {
		if err := ComputeChunkEmbeddings(ctx, chunks, embedModel,
			WithUseCaptionForImages(config.UseCaptionForImages)); err != nil {
			logger.Error(logComponent).
				Str("event_type", "INDEX_BUILD_ERROR").
				Str("method", "BuildIndex").
				Str("provider", "chroma_bleve").
				Int("chunk_count", len(chunks)).
				Err(err).
				Msg("chunk embedding failed")
			return false, err
		}
	}
	// ② TextChunk → 字段 map（chroma 与 Bleve 双写经 store.Add 单咽喉）
	data := make([]map[string]any, 0, len(chunks))
	for _, ch := range chunks {
		node := map[string]any{
			"id":                  ch.ID,
			ci.store.TextField():  ch.Text,
			"metadata":            ch.Metadata,
			ci.store.DocIDField(): ch.DocID,
		}
		if len(ch.Embedding) > 0 {
			node["vector"] = ch.Embedding
		}
		if chunkID, ok := ch.Metadata["chunk_id"]; ok {
			node["chunk_id"] = chunkID
		}
		data = append(data, node)
	}
	if err := ci.store.Add(ctx, data); err != nil {
		logger.Error(logComponent).
			Str("event_type", "INDEX_BUILD_ERROR").
			Str("method", "BuildIndex").
			Str("provider", "chroma_bleve").
			Int("chunk_count", len(data)).
			Err(err).
			Msg("chroma_bleve index build failed")
		return false, err
	}
	logger.Info(logComponent).
		Str("provider", "chroma_bleve").
		Int("chunk_count", len(data)).
		Msg("chroma_bleve index built")
	return true, nil
}

// UpdateIndex 更新索引：按 docID 先删后建（双存储同步删，语义对齐重解析编排）
func (ci *ChromaBleveIndexer) UpdateIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	docID string,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	if _, err := ci.DeleteIndex(ctx, docID, config.IndexName); err != nil {
		return false, err
	}
	return ci.BuildIndex(ctx, chunks, config, embedModel, opts...)
}

// DeleteIndex 按 docID 删除该文档全部分块（经 store.DeleteByDocID 双删）
func (ci *ChromaBleveIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	return ci.store.DeleteByDocID(ctx, docID)
}

// IndexExists 检查索引存在性（透传 store）
func (ci *ChromaBleveIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	return ci.store.TableExists(ctx, ci.store.CollectionName())
}

// GetIndexInfo 获取索引元信息
func (ci *ChromaBleveIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	return map[string]any{
		"index_name":      ci.store.CollectionName(),
		"provider":        "chroma_bleve",
		"database_name":   ci.store.DatabaseName(),
		"text_field":      ci.TextField(),
		"metadata_field":  ci.MetadataField(),
		"doc_id_field":    ci.DocIDField(),
		"distance_metric": ci.store.DistanceMetric(),
	}, nil
}

// ensure indexer 接口编译期兜底
var _ Indexer = (*ChromaBleveIndexer)(nil)
