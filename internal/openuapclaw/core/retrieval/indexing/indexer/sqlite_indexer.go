// Package indexer 内 SQLiteIndexer：SQLite 向量存储（D-25-7 私有 provider）的
// 入库侧索引管理器（30.2 provider 分派批次引入，Python 无对应物）。
//
// 与 ChromaIndexer 的语义差异（spec 2026-12-08 §9 登记）：
//   - 去重：chroma 走 collection.Get 查重跳过；sqlite Add 为 UPSERT 同 id 覆写，
//     幂等等价，重解析「先删后建」语义不变
//   - DeleteIndex：sqlite Delete 仅支持 id 过滤 → 经 store.IDsByDoc 取 chunk id 再删
//   - Close：chroma 为空操作（进程级共享）；sqlite store 持专用连接，Close 真关闭
package indexer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	vs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SQLiteIndexer SQLite 向量存储的入库侧索引管理器
type SQLiteIndexer struct {
	// store 底层 SQLite 向量存储（Add/Search/Sparse/Hybrid/Delete 全走它）
	store *vs.SQLiteVectorStore
	// databaseName 数据库名（与 config.DatabaseName 同源，13.26 validate_index 消费）
	databaseName string
	// collectionName collection 名（= IndexName 前缀）
	collectionName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSQLiteIndexer 构造 SQLiteIndexer（字段名对齐 kbadmin chroma 路径约定：
// content/vector/sparse_vector/metadata/document_id；dbPath 为 sqlite 库文件路径）
func NewSQLiteIndexer(config common.VectorStoreConfig, dbPath string) (*SQLiteIndexer, error) {
	store, err := vs.NewSQLiteVectorStore(config, dbPath,
		"content", "vector", "sparse_vector", "metadata", "document_id")
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("sqlite indexer store construction failed")
		return nil, err
	}
	return &SQLiteIndexer{
		store:          store,
		databaseName:   config.DatabaseName,
		collectionName: config.CollectionName,
	}, nil
}

// DatabaseName 返回数据库名称（13.26 validate_index 消费）
func (si *SQLiteIndexer) DatabaseName() string { return si.databaseName }

// TextField 返回文本字段名（13.26 validate_index 消费）
func (si *SQLiteIndexer) TextField() string { return "content" }

// VectorField 返回向量字段名（13.26 validate_index 消费，any 装箱联合类型）
func (si *SQLiteIndexer) VectorField() any { return "vector" }

// SparseVectorField 返回稀疏向量字段名（13.26 validate_index 消费）
func (si *SQLiteIndexer) SparseVectorField() string { return "sparse_vector" }

// MetadataField 返回元数据字段名（13.26 validate_index 消费）
func (si *SQLiteIndexer) MetadataField() string { return "metadata" }

// DocIDField 返回文档 ID 字段名（13.26 validate_index 消费）
func (si *SQLiteIndexer) DocIDField() string { return "document_id" }

// Store 返回底层向量存储（检索侧复用同一实例，避免同库多连接）
func (si *SQLiteIndexer) Store() *vs.SQLiteVectorStore { return si.store }

// Close 关闭索引管理器（真关闭：sqlite store 持专用连接，与 chroma 空操作不同）
func (si *SQLiteIndexer) Close() error { return si.store.Close() }

// BuildIndex 构建索引。将 chunks 嵌入→写入（sqlite UPSERT 幂等，无查重步骤）。
// 3 步流程（对照 ChromaIndexer.BuildIndex 4 步，去查重步）：
//  1. 嵌入（embedModel 非 nil 时；nil 合法——bm25-only 场景 store 跳过向量写）
//  2. 转换 TextChunk → sqlite 字段 map
//  3. 写入（SQLiteVectorStore.Add，UPSERT）
func (si *SQLiteIndexer) BuildIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	if len(chunks) == 0 {
		return false, nil
	}
	// ① 嵌入（向量原地写回 chunk.Embedding；与 chroma 共用 embed_chunks 助手）
	if embedModel != nil {
		if err := ComputeChunkEmbeddings(ctx, chunks, embedModel,
			WithUseCaptionForImages(config.UseCaptionForImages)); err != nil {
			logger.Error(logComponent).
				Str("event_type", "INDEX_BUILD_ERROR").
				Str("method", "BuildIndex").
				Str("provider", "sqlite").
				Int("chunk_count", len(chunks)).
				Err(err).
				Msg("chunk embedding failed")
			return false, err
		}
	}
	// ② TextChunk → sqlite 字段 map
	data := make([]map[string]any, 0, len(chunks))
	for _, ch := range chunks {
		node := map[string]any{
			"id":          ch.ID,
			"content":     ch.Text,
			"metadata":    ch.Metadata,
			"document_id": ch.DocID,
		}
		if len(ch.Embedding) > 0 {
			node["vector"] = ch.Embedding
		}
		if chunkID, ok := ch.Metadata["chunk_id"]; ok {
			node["chunk_id"] = chunkID
		}
		data = append(data, node)
	}
	// ③ 写入（UPSERT 幂等）
	if err := si.store.Add(ctx, data); err != nil {
		logger.Error(logComponent).
			Str("event_type", "INDEX_BUILD_ERROR").
			Str("method", "BuildIndex").
			Str("provider", "sqlite").
			Int("chunk_count", len(data)).
			Err(err).
			Msg("sqlite index build failed")
		return false, err
	}
	logger.Info(logComponent).
		Str("provider", "sqlite").
		Int("chunk_count", len(data)).
		Msg("sqlite index built")
	return true, nil
}

// UpdateIndex 更新索引：按 docID 先删后建（对齐 indexer 接口契约，与
// kbadmin 重解析「先删后建」编排语义一致）。
func (si *SQLiteIndexer) UpdateIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	docID string,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	if _, err := si.DeleteIndex(ctx, docID, config.IndexName); err != nil {
		return false, err
	}
	return si.BuildIndex(ctx, chunks, config, embedModel, opts...)
}

// DeleteIndex 按 docID 删除该文档全部分块（经 IDsByDoc 取 id——sqlite Delete
// 仅支持 id 过滤）；无该文档记录时返回 false（幂等空操作）。
func (si *SQLiteIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	ids, err := si.store.IDsByDoc(ctx, docID)
	if err != nil {
		return false, err
	}
	if len(ids) == 0 {
		return false, nil
	}
	return si.store.Delete(ctx, ids, nil)
}

// IndexExists 检查索引（chunks 主表存在性；TableExists 内部对入参追加
// _chunks/_fts/_vec/_vec_map 后缀，此处只传 collection 名）。
func (si *SQLiteIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	return si.store.TableExists(ctx, si.collectionName)
}

// GetIndexInfo 获取索引元信息。
func (si *SQLiteIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	return map[string]any{
		"index_name":      si.collectionName,
		"provider":        "sqlite",
		"database_name":   si.databaseName,
		"text_field":      si.TextField(),
		"vector_field":    si.VectorField(),
		"metadata_field":  si.MetadataField(),
		"doc_id_field":    si.DocIDField(),
		"distance_metric": si.store.DistanceMetric(),
	}, nil
}

// ensure indexer 接口编译期兜底
var _ Indexer = (*SQLiteIndexer)(nil)
