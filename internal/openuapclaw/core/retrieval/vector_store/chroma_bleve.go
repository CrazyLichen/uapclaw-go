// Package vector_store 内 ChromaBleveStore：chroma 域的稀疏半边扩展实现
// （spec 2026-12-08 §10，Python 无对应物——Go 侧私有扩展，同 D-25-7 类别）。
//
// 背景：chroma 官方无 BM25 关键词排序检索（Python chroma_store.py:308 注释承认，
// 以 query_texts 变通）；本地模式为规避 chroma-go v2 强制 ONNX 默认 EF（CI
// SIGSEGV）挂了 ConsistentHash 伪 EF，导致稀疏半边为伪语义（chroma.go:265-269）。
// 本实现以独立 Bleve 倒排索引（scorch BM25）接管 SparseSearch 半边，向量半边
// 原样透传 chroma——不动 chroma 存储与 13.x 对齐实现。
//
// 双写纪律（无事务，固定顺序）：
//   - 写入：chroma 先行（难回滚侧）→ Bleve 后写；Bleve 失败上抛由调用方回滚 chroma
//   - 删除：Bleve 先删（可重建侧）→ chroma 后删；Bleve 失败上抛不动 chroma
//
// 已知限制（spec §10 登记）：
//   - Bleve 半边不支持 chroma QueryExpr filter（现状 sparse 调用方传 nil，非 nil Warn 忽略）
//   - 去重语义由编排层「先删后建」保证（kbadmin 全链唯一调用形态）
//   - Bleve 异常中断后的索引重建兜底 ⤵️（启动检测 chroma 有数据/Bleve 空时从台账重建）
package vector_store

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/cjk" // 注册 cjk 分词器（中英混合，spec §10 限制④）
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/utils"
)

// ──────────────────────────── 常量 ────────────────────────────

// bleve 索引字段名（与 chroma textField/docIDField 对齐）
const (
	bleveContentField = "content"
	bleveDocIDField   = "doc_id"
	// defaultAnalyzer cjk 内置分词器（中英混合；ASCII 词原样、CJK 二元切分）
	defaultAnalyzer = "cjk"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChromaBleveStore chroma 向量 + Bleve 倒排组合存储（实现 VectorStore 接口）
type ChromaBleveStore struct {
	// inner chroma 向量半边（Search/Add/Delete 透传目标）
	inner *ChromaVectorStore
	// bleveIndex 关键词倒排索引（SparseSearch 半边）
	bleveIndex bleve.Index
	// blevePath 索引持久化目录（DeleteTable 清理用）
	blevePath string
	// collectionName / databaseName 元信息透传
	collectionName string
	databaseName   string
	textField      string
	docIDField     string

	// bleveCloseOnce bleve 索引关闭幂等保护（DeleteTable 与 Close 都会触发）
	bleveCloseOnce sync.Once
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChromaBleveStore 构造组合存储（chromaPath/字段参数对齐 NewChromaVectorStore；
// blevePath 为倒排索引持久化目录，已存在则打开复用，否则新建）
func NewChromaBleveStore(config common.VectorStoreConfig, chromaPath, blevePath,
	textField string, vectorField *vector_fields.ChromaVectorField, sparseVectorField,
	metadataField, docIDField string,
) (*ChromaBleveStore, error) {
	inner, err := NewChromaVectorStore(config, chromaPath,
		textField, vectorField, sparseVectorField, metadataField, docIDField)
	if err != nil {
		return nil, err
	}
	idx, err := openOrCreateBleve(blevePath)
	if err != nil {
		logger.Error(logComponent).Str("bleve_path", blevePath).Err(err).
			Msg("bleve index open/create failed")
		return nil, fmt.Errorf("打开/创建 bleve 索引失败: %w", err)
	}
	return &ChromaBleveStore{
		inner:          inner,
		bleveIndex:     idx,
		blevePath:      blevePath,
		collectionName: config.CollectionName,
		databaseName:   config.DatabaseName,
		textField:      textField,
		docIDField:     docIDField,
	}, nil
}

// openOrCreateBleve 打开或创建 bleve 索引（cjk 分词；content 全文 + doc_id keyword）
func openOrCreateBleve(path string) (bleve.Index, error) {
	if _, statErr := os.Stat(path); statErr == nil {
		idx, err := bleve.Open(path)
		if err == nil {
			return idx, nil
		}
		// 目录存在但损坏（异常中断）：清空重建（登记限制 ③ 的兜底雏形）
		logger.Warn(logComponent).Str("bleve_path", path).Err(err).
			Msg("bleve index corrupt, rebuilding")
		if err := os.RemoveAll(path); err != nil {
			return nil, err
		}
	}
	im := mapping.NewIndexMapping()
	docMapping := mapping.NewDocumentMapping()
	contentField := bleve.NewTextFieldMapping()
	contentField.Store = true
	contentField.IncludeInAll = false
	docIDFieldMapping := bleve.NewTextFieldMapping()
	docIDFieldMapping.Store = false
	docIDFieldMapping.IncludeInAll = false
	docMapping.AddFieldMappingsAt(bleveContentField, contentField)
	docMapping.AddFieldMappingsAt(bleveDocIDField, docIDFieldMapping)
	im.DefaultMapping = docMapping
	im.DefaultAnalyzer = defaultAnalyzer
	return bleve.NewUsing(path, im, bleve.Config.DefaultIndexType, bleve.Config.DefaultKVStore, nil)
}

// ──────────────────────────── 元信息透传 ────────────────────────────

// CollectionName 返回 collection 名
func (s *ChromaBleveStore) CollectionName() string { return s.inner.CollectionName() }

// DatabaseName 返回数据库名
func (s *ChromaBleveStore) DatabaseName() string { return s.inner.DatabaseName() }

// DistanceMetric 返回距离度量
func (s *ChromaBleveStore) DistanceMetric() string { return s.inner.DistanceMetric() }

// TextField 返回文本字段名
func (s *ChromaBleveStore) TextField() string { return s.textField }

// VectorField 返回向量字段配置（any 装箱联合类型）
func (s *ChromaBleveStore) VectorField() any { return s.inner.VectorField() }

// SparseVectorField 返回稀疏向量字段名
func (s *ChromaBleveStore) SparseVectorField() string { return s.inner.SparseVectorField() }

// MetadataField 返回元数据字段名
func (s *ChromaBleveStore) MetadataField() string { return s.inner.MetadataField() }

// DocIDField 返回文档 ID 字段名
func (s *ChromaBleveStore) DocIDField() string { return s.inner.DocIDField() }

// CheckVectorField 校验向量字段配置（透传 chroma）
func (s *ChromaBleveStore) CheckVectorField() error { return s.inner.CheckVectorField() }

// Inner 返回底层 chroma 存储（需要 chroma 原生能力的场景）
func (s *ChromaBleveStore) Inner() *ChromaVectorStore { return s.inner }

// ──────────────────────────── 导出函数（写路径） ────────────────────────────

// Add 添加向量数据：chroma 先行（难回滚侧）→ Bleve 后写
func (s *ChromaBleveStore) Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error {
	if err := s.inner.Add(ctx, data, opts...); err != nil {
		return err
	}
	batch := s.bleveIndex.NewBatch()
	for _, node := range data {
		id, _ := node["id"].(string)
		if id == "" {
			id, _ = node["pk"].(string)
		}
		if id == "" {
			continue
		}
		text, _ := node[s.textField].(string)
		docID, _ := node[s.docIDField].(string)
		doc := map[string]any{
			bleveContentField: text,
			bleveDocIDField:   docID,
		}
		batch.Index(id, doc)
	}
	if err := s.bleveIndex.Batch(batch); err != nil {
		logger.Error(logComponent).Str("collection", s.collectionName).Err(err).
			Msg("bleve batch index failed (chroma already written)")
		return fmt.Errorf("bleve 索引写入失败: %w", err)
	}
	return nil
}

// Delete 删除数据：Bleve 先删（可重建侧）→ chroma 后删
func (s *ChromaBleveStore) Delete(ctx context.Context, ids []string, filterExpr query.QueryExpr) (bool, error) {
	if len(ids) > 0 {
		for _, id := range ids {
			if err := s.bleveIndex.Delete(id); err != nil {
				logger.Error(logComponent).Str("id", id).Err(err).
					Msg("bleve delete failed (chroma untouched)")
				return false, fmt.Errorf("bleve 删除失败: %w", err)
			}
		}
	}
	return s.inner.Delete(ctx, ids, filterExpr)
}

// DeleteByDocID 按文档 ID 删除该文档全部分块（扩展方法，非接口面；
// 重解析「先删后建」消费）：Bleve 按 doc_id 字段查询收集 id 删 → chroma 按 filter 删
func (s *ChromaBleveStore) DeleteByDocID(ctx context.Context, docID string) (bool, error) {
	// ① Bleve 半边：doc_id term 查询收集 chunk id
	mq := bleve.NewTermQuery(docID)
	mq.SetField(bleveDocIDField)
	req := bleve.NewSearchRequestOptions(mq, 1<<16, 0, false)
	// 只要 hit.ID（chunk id），不取 stored 字段
	res, err := s.bleveIndex.SearchInContext(ctx, req)
	if err != nil {
		logger.Error(logComponent).Str("doc_id", docID).Err(err).
			Msg("bleve delete-by-doc query failed (chroma untouched)")
		return false, fmt.Errorf("bleve 按 doc 查询失败: %w", err)
	}
	deleted := false
	for _, hit := range res.Hits {
		if err := s.bleveIndex.Delete(hit.ID); err != nil {
			return false, fmt.Errorf("bleve 删除失败: %w", err)
		}
		deleted = true
	}
	// ② chroma 半边：filter 删除（ids 为空走 filterExpr 路径）
	filter := query.Eq(s.docIDField, docID)
	if _, err := s.inner.Delete(ctx, nil, filter); err != nil {
		return false, err
	}
	return deleted, nil
}

// ──────────────────────────── 导出函数（读路径） ────────────────────────────

// Search 向量搜索（透传 chroma——真向量语义半边）
func (s *ChromaBleveStore) Search(ctx context.Context, queryVector []float64, topK int,
	filters query.QueryExpr, _ ...StoreOption,
) ([]common.SearchResult, error) {
	return s.inner.Search(ctx, queryVector, topK, filters)
}

// SparseSearch 稀疏搜索（Bleve scorch BM25 关键词排序——替代伪 EF 路径）。
// 限制：QueryExpr filter 不支持（spec §10），非 nil 时 Warn 忽略
func (s *ChromaBleveStore) SparseSearch(ctx context.Context, queryText string, topK int,
	filters query.QueryExpr, _ ...StoreOption,
) ([]common.SearchResult, error) {
	if filters != nil {
		logger.Warn(logComponent).
			Str("method", "SparseSearch").
			Str("provider", "chroma_bleve").
			Msg("bleve half does not support chroma filters, ignoring (spec §10)")
	}
	matchQ := bleve.NewMatchQuery(queryText)
	matchQ.SetField(bleveContentField) // 字段均 IncludeInAll=false，必须显式指定查询字段
	req := bleve.NewSearchRequestOptions(matchQ, topK, 0, false)
	req.Fields = []string{bleveContentField}
	res, err := s.bleveIndex.SearchInContext(ctx, req)
	if err != nil {
		// 对齐 Python sparse_search 容错：logger.warning 后返回空
		logger.Warn(logComponent).
			Str("method", "SparseSearch").
			Str("provider", "chroma_bleve").
			Err(err).
			Msg("bleve keyword search failed")
		return nil, nil
	}
	results := make([]common.SearchResult, 0, len(res.Hits))
	for _, hit := range res.Hits {
		results = append(results, common.SearchResult{
			ID:    hit.ID,
			Text:  bleveStoredText(hit),
			Score: hit.Score,
		})
	}
	return results, nil
}

// HybridSearch 混合搜索：chroma 向量 + Bleve 稀疏 → RRF 融合（sqlite 同款）
func (s *ChromaBleveStore) HybridSearch(ctx context.Context, queryText string, queryVector []float64,
	topK int, alpha float64, filters query.QueryExpr, _ ...StoreOption,
) ([]common.SearchResult, error) {
	_ = alpha // RRF 不使用加权系数（对齐 sqlite HybridSearch 注释语义）
	vectorResults, err := s.inner.Search(ctx, queryVector, topK, filters)
	if err != nil {
		return nil, err
	}
	sparseResults, err := s.SparseSearch(ctx, queryText, topK, filters)
	if err != nil {
		sparseResults = nil // 稀疏半边失败降级为纯向量（对齐 sqlite 容错）
	}
	toRetrieval := func(in []common.SearchResult) []common.RetrievalResult {
		out := make([]common.RetrievalResult, 0, len(in))
		for _, r := range in {
			out = append(out, common.RetrievalResult{Text: r.Text, Score: r.Score, Metadata: r.Metadata})
		}
		return out
	}
	fused := utils.RRFFusion([][]common.RetrievalResult{
		toRetrieval(vectorResults), toRetrieval(sparseResults),
	}, 60)
	if len(fused) > topK {
		fused = fused[:topK]
	}
	results := make([]common.SearchResult, 0, len(fused))
	for _, r := range fused {
		results = append(results, common.SearchResult{Text: r.Text, Score: r.Score, Metadata: r.Metadata})
	}
	return results, nil
}

// TableExists 检查集合是否存在（透传 chroma）
func (s *ChromaBleveStore) TableExists(ctx context.Context, tableName string) (bool, error) {
	return s.inner.TableExists(ctx, tableName)
}

// DeleteTable 删除集合：Bleve 索引目录清理 + chroma collection 删除
func (s *ChromaBleveStore) DeleteTable(ctx context.Context, tableName string) error {
	s.closeBleve()
	if err := os.RemoveAll(s.blevePath); err != nil {
		logger.Warn(logComponent).Str("bleve_path", s.blevePath).Err(err).
			Msg("bleve index dir removal failed")
	}
	return s.inner.DeleteTable(ctx, tableName)
}

// Close 关闭存储（chroma Close 为空操作惯例 + bleve 索引真关闭，幂等）
func (s *ChromaBleveStore) Close() error {
	if err := s.inner.Close(); err != nil {
		return err
	}
	s.closeBleve()
	return nil
}

// closeBleve 幂等关闭 bleve 索引（DeleteTable/Close/双重关闭场景）
func (s *ChromaBleveStore) closeBleve() {
	s.bleveCloseOnce.Do(func() {
		if err := s.bleveIndex.Close(); err != nil {
			logger.Warn(logComponent).Str("bleve_path", s.blevePath).Err(err).
				Msg("bleve index close failed")
		}
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// bleveStoredText 从命中结果取 stored 的 content 字段（取不到时退化为空串——
// 排序语义不受影响，正文可由上层从向量库/台账回查）
func bleveStoredText(hit *search.DocumentMatch) string {
	raw, ok := hit.Fields[bleveContentField]
	if !ok {
		return ""
	}
	if text, ok := raw.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", raw)
}
