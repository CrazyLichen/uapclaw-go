package indexer

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusIndexer Milvus 文档索引管理器。
//
// 实现 Indexer 接口，负责将 TextChunk 写入 Milvus 向量库：
// ensureCollection（含 BM25 Function/dense 索引/INVERTED 标量索引）、
// BuildIndex（查重→嵌入→写入）、UpdateIndex（先删后建）、
// DeleteIndex（filter 删除）、IndexExists、GetIndexInfo。
//
// Python: retrieval/indexing/indexer/milvus_indexer.py (MilvusIndexer)
type MilvusIndexer struct {
	// milvusURI Milvus 服务地址
	milvusURI string
	// milvusToken Milvus 认证 token
	milvusToken string
	// textField 文本字段名，默认 "content"
	textField string
	// vectorField 向量字段配置
	vectorField vector_fields.MilvusVectorFieldConfig
	// sparseVectorField 稀疏向量字段名，默认 "sparse_vector"
	sparseVectorField string
	// metadataField 元数据字段名，默认 "metadata"
	metadataField string
	// docIDField 文档 ID 字段名，默认 "document_id"
	docIDField string
	// databaseName 数据库名称
	databaseName string
	// constructConfig 构建阶段配置（AUTO 时仅含 metric_type）
	constructConfig map[string]any
	// searchConfig 搜索阶段配置
	searchConfig map[string]any
	// distanceMetric 归一化距离度量：IP/L2/COSINE
	distanceMetric string
	// docIndexCallback 嵌入进度回调
	docIndexCallback common.DocIndexCallback
	// milvusAlias 连接别名
	milvusAlias string
	// client Milvus 客户端（构造时创建，全方法共享）
	client vector_store.MilvusClient
	// createClientFunc 客户端创建函数（依赖注入）
	createClientFunc func(ctx context.Context, databaseName, uri, token string) (vector_store.MilvusClient, error)
}

// MilvusIndexerOption MilvusIndexer 的可选参数函数。
type MilvusIndexerOption func(*MilvusIndexer)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// milvusBM25FunctionName BM25 函数名，对齐 Python "text_bm25_emb"
	milvusBM25FunctionName = "text_bm25_emb"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：MilvusIndexer 实现 Indexer 接口
// TODO(#13.3-Task8): BuildIndex/UpdateIndex 实现后恢复
// var _ Indexer = (*MilvusIndexer)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusIndexer 创建 MilvusIndexer 实例。
//
// 对齐 Python MilvusIndexer.__init__：
//   - vectorField 字符串自动转 MilvusAUTO（Go 用构造函数显式转换，默认值即 MilvusAUTO）
//   - 距离度量归一化 dot→ip / euclidean→l2 / 大写
//   - constructConfig（AUTO → {} + metric_type；否则 ToConstructDict + metric_type）
//   - searchConfig 初始化
//   - milvusAlias 生成
//   - client 创建
func NewMilvusIndexer(
	config common.VectorStoreConfig,
	milvusURI string,
	milvusToken string,
	opts ...MilvusIndexerOption,
) (*MilvusIndexer, error) {
	idx := &MilvusIndexer{
		milvusURI:         milvusURI,
		milvusToken:       milvusToken,
		textField:         "content",
		vectorField:       vector_fields.NewMilvusAUTO("embedding"),
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      config.DatabaseName,
		createClientFunc:  vector_store.DefaultMilvusCreateClient,
	}

	for _, opt := range opts {
		opt(idx)
	}

	// 对齐 Python: if isinstance(vector_field, str) → MilvusAUTO（默认值已处理）

	// 对齐 Python: self._distance_metric = ...replace("dot","ip").replace("euclidean","l2").upper()
	idx.distanceMetric = strings.ToUpper(
		strings.ReplaceAll(
			strings.ReplaceAll(string(config.DistanceMetric), "dot", "ip"),
			"euclidean", "l2",
		),
	)

	// 对齐 Python: if self.vector_field.index_type == "auto": {} else to_dict("construct")
	if idx.vectorField.IndexType() == vector_fields.IndexTypeAUTO {
		idx.constructConfig = map[string]any{}
	} else {
		idx.constructConfig = idx.vectorField.ToConstructDict()
	}
	// 对齐 Python: self._construct_config["metric_type"] = self._distance_metric
	idx.constructConfig["metric_type"] = idx.distanceMetric

	// 对齐 Python: self._search_config = self.vector_field.to_dict(stage="search")
	idx.searchConfig = idx.vectorField.ToSearchDict()

	// 对齐 Python: self.milvus_alias = create_milvus_alias(milvus_alias, uri=..., token=...)
	if idx.milvusAlias == "" {
		idx.milvusAlias = utils.CreateMilvusAlias("", milvusURI, milvusToken)
	}

	// 对齐 Python: self._client = MilvusVectorStore.create_client(...)
	if idx.client == nil {
		client, err := idx.createClientFunc(context.Background(), idx.databaseName, idx.milvusURI, idx.milvusToken)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("创建 Milvus 客户端失败: %s", err)),
				exception.WithCause(err),
			)
		}
		idx.client = client
	}

	return idx, nil
}

// WithMilvusIndexerClient 注入 Milvus 客户端（测试用，跳过默认创建）。
func WithMilvusIndexerClient(c vector_store.MilvusClient) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.client = c }
}

// WithMilvusIndexerCreateClient 注入客户端创建函数。
func WithMilvusIndexerCreateClient(f func(ctx context.Context, databaseName, uri, token string) (vector_store.MilvusClient, error)) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.createClientFunc = f }
}

// WithMilvusIndexerTextField 设置文本字段名。
func WithMilvusIndexerTextField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.textField = field }
}

// WithMilvusIndexerVectorField 设置向量字段配置。
func WithMilvusIndexerVectorField(vf vector_fields.MilvusVectorFieldConfig) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.vectorField = vf }
}

// WithMilvusIndexerSparseVectorField 设置稀疏向量字段名。
func WithMilvusIndexerSparseVectorField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.sparseVectorField = field }
}

// WithMilvusIndexerMetadataField 设置元数据字段名。
func WithMilvusIndexerMetadataField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.metadataField = field }
}

// WithMilvusIndexerDocIDField 设置文档 ID 字段名。
func WithMilvusIndexerDocIDField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.docIDField = field }
}

// WithMilvusIndexerDocIndexCallback 设置嵌入进度回调。
func WithMilvusIndexerDocIndexCallback(cb common.DocIndexCallback) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.docIndexCallback = cb }
}

// WithMilvusIndexerAlias 设置连接别名。
func WithMilvusIndexerAlias(alias string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.milvusAlias = alias }
}

// Client 返回底层 Milvus 客户端。
// 对齐 Python client property。
func (mi *MilvusIndexer) Client() vector_store.MilvusClient { return mi.client }

// DistanceMetric 返回归一化距离度量。
// 对齐 Python distance_metric property。
func (mi *MilvusIndexer) DistanceMetric() string { return mi.distanceMetric }

// IndexExists 检查索引是否存在。
//
// 对齐 Python MilvusIndexer.index_exists：has_collection，异常时返回 False。
func (mi *MilvusIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	ok, err := mi.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(indexName))
	if err != nil {
		// 对齐 Python: except Exception as e: logger.error(...); return False
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("检查索引存在性失败")
		return false, nil
	}
	return ok, nil
}

// DeleteIndex 删除索引。
//
// 对齐 Python MilvusIndexer.delete_index：
// filter_expr = 'document_id == "xxx"'，delete 后按 delete_count 判断。
func (mi *MilvusIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	// 对齐 Python: filter_expr = f'{self.doc_id_field} == "{doc_id}"'
	filterExpr := fmt.Sprintf("%s == %q", mi.docIDField, docID)

	result, err := mi.client.Delete(ctx, milvusclient.NewDeleteOption(indexName).WithExpr(filterExpr))
	if err != nil {
		// 对齐 Python: except MilvusException / except Exception → logger.error + return False
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Str("index_name", indexName).
			Err(err).
			Msg("删除索引条目失败")
		return false, nil
	}

	// 对齐 Python: logger.info(f"Deleted {delete_count} entries for doc_id={doc_id}")
	logger.Info(logComponent).
		Str("doc_id", docID).
		Int64("delete_count", result.DeleteCount).
		Msg("成功删除文档条目")

	return result.DeleteCount > 0, nil
}

// GetIndexInfo 获取索引信息。
//
// 对齐 Python MilvusIndexer.get_index_info：
//   - 不存在返回 {"exists": False}
//   - get_collection_stats 获取 row_count（避免 query 分页错误）
//   - describe_collection 获取字段信息
//   - 任何异常返回 {"exists": False, "error": ...}
func (mi *MilvusIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	exists, err := mi.IndexExists(ctx, indexName)
	if err != nil || !exists {
		return map[string]any{"exists": false}, nil
	}

	// 对齐 Python: stats = get_collection_stats(...)；row_count 可能是字符串
	stats, err := mi.client.GetCollectionStats(ctx, milvusclient.NewGetCollectionStatsOption(indexName))
	if err != nil {
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("获取集合统计失败")
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}
	rowCount := 0
	if rc, ok := stats["row_count"]; ok {
		if n, parseErr := strconv.Atoi(rc); parseErr == nil {
			rowCount = n
		}
	}

	// 对齐 Python: collection_info = describe_collection(...)
	collectionInfo, err := mi.client.DescribeCollection(ctx, milvusclient.NewDescribeCollectionOption(indexName))
	if err != nil {
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("获取集合描述失败")
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}

	return map[string]any{
		"exists":          true,
		"collection_name": indexName,
		"info":            collectionInfo,
		"count":           rowCount,
	}, nil
}

// Close 关闭索引管理器。
// 对齐 Python MilvusIndexer.close。
func (mi *MilvusIndexer) Close() {
	if mi.client == nil {
		return
	}
	if err := mi.client.Close(context.Background()); err != nil {
		// 对齐 Python: logger.warning(f"Failed to close Milvus client: {e}")
		logger.Warn(logComponent).Err(err).Msg("关闭 Milvus 客户端失败")
	}
}
