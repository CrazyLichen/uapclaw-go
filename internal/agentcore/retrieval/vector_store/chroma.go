package vector_store

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// chromaWhereFilter 适配器，将 WhereClause 包装为 WhereFilter 接口。
type chromaWhereFilter struct {
	clause chromav2.WhereClause
}

// chromaMapWhereFilter M-27: map 形态 where 过滤适配器。
//
// QueryExpr.ToExpr("chroma") 输出 Chroma 原生 where 字典（如
// {"$and": [...]}），SDK v0.4.1 未提供从 map 直构 WhereFilter 的工厂，
// 此适配器直接持有 map 并经 json.Marshal 序列化——SDK 的
// CollectionQueryOp.Where 字段以 json tag 调用 MarshalJSON 输出请求体。
type chromaMapWhereFilter struct {
	m map[string]any
}

// chromaMapWhereDocumentFilter M-27: map 形态 where_document 过滤适配器。
//
// 语义同 chromaMapWhereFilter，包装 Chroma 原生 where_document 字典
// （$contains/$regex 等文档内容过滤）。
type chromaMapWhereDocumentFilter struct {
	m map[string]any
}

// ChromaVectorStore retrieval 层 ChromaDB 向量存储实现。
//
// 支持向量搜索、稀疏搜索（文本匹配）和混合搜索（RRF 融合）。
// 对齐 Python retrieval/vector_store/chroma_store.py (ChromaVectorStore)。
type ChromaVectorStore struct {
	// config 向量存储配置
	config common.VectorStoreConfig
	// collectionName 集合名称
	collectionName string
	// chromaPath ChromaDB 持久化路径
	chromaPath string
	// textField 文本字段名
	textField string
	// vectorField 向量字段配置
	vectorField *vector_fields.ChromaVectorField
	// sparseVectorField 稀疏向量字段名
	sparseVectorField string
	// metadataField 元数据字段名
	metadataField string
	// docIDField 文档 ID 字段名
	docIDField string
	// databaseName 数据库名称
	databaseName string
	// distanceMetric 归一化距离度量：cosine/l2/ip
	distanceMetric string
	// constructConfig 构建阶段配置
	constructConfig map[string]any
	// searchConfig 搜索阶段配置
	searchConfig map[string]any
	// client ChromaDB 客户端
	client chromav2.Client
	// collection ChromaDB 集合
	collection chromav2.Collection
	// mu 读写锁
	mu sync.RWMutex
}

// ChromaStoreOption ChromaVectorStore 的可选参数函数。
//
// 13.23 审查拍板 A：对齐 MilvusStoreOption 先例，提供测试注入口
// （WithChromaClientOption），消除构造期真实连接导致的测试盲区。
type ChromaStoreOption func(*ChromaVectorStore)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultChromaBatchSize 默认批大小，对齐 Python batch_size=128
	defaultChromaBatchSize = 128
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// String 实现 WhereFilter 接口 — chromaWhereFilter
func (f *chromaWhereFilter) String() string {
	if f.clause != nil {
		return f.clause.String()
	}
	return ""
}

// Validate 实现 WhereFilter 接口 — chromaMapWhereFilter
// map 已是 Chroma 原生 where 语法，无 SDK 侧校验需求，恒通过。
func (f *chromaMapWhereFilter) Validate() error { return nil }

// String 实现 WhereFilter 接口 — chromaMapWhereFilter
func (f *chromaMapWhereFilter) String() string {
	if b, err := json.Marshal(f.m); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", f.m)
}

// MarshalJSON 实现 WhereFilter 接口 — chromaMapWhereFilter
func (f *chromaMapWhereFilter) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.m)
}

// UnmarshalJSON 实现 WhereFilter 接口 — chromaMapWhereFilter
func (f *chromaMapWhereFilter) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &f.m)
}

// Operator 实现 WhereDocumentFilter 接口 — chromaMapWhereDocumentFilter
func (f *chromaMapWhereDocumentFilter) Operator() chromav2.WhereDocumentFilterOperator {
	// map 已含操作符键（$contains/$regex/$and/$or），基类不单独暴露
	return ""
}

// Operand 实现 WhereDocumentFilter 接口 — chromaMapWhereDocumentFilter
func (f *chromaMapWhereDocumentFilter) Operand() any {
	return f.m
}

// Validate 实现 WhereDocumentFilter 接口 — chromaMapWhereDocumentFilter
// map 已是 Chroma 原生 where_document 语法，恒通过。
func (f *chromaMapWhereDocumentFilter) Validate() error { return nil }

// String 实现 WhereDocumentFilter 接口 — chromaMapWhereDocumentFilter
func (f *chromaMapWhereDocumentFilter) String() string {
	if b, err := json.Marshal(f.m); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", f.m)
}

// MarshalJSON 实现 WhereDocumentFilter 接口 — chromaMapWhereDocumentFilter
func (f *chromaMapWhereDocumentFilter) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.m)
}

// UnmarshalJSON 实现 WhereDocumentFilter 接口 — chromaMapWhereDocumentFilter
func (f *chromaMapWhereDocumentFilter) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &f.m)
}

// Validate 实现 WhereFilter 接口
func (f *chromaWhereFilter) Validate() error {
	if f.clause != nil {
		return f.clause.Validate()
	}
	return nil
}

// MarshalJSON 实现 WhereFilter 接口
func (f *chromaWhereFilter) MarshalJSON() ([]byte, error) {
	if f.clause != nil {
		return f.clause.MarshalJSON()
	}
	return []byte("null"), nil
}

// UnmarshalJSON 实现 WhereFilter 接口
func (f *chromaWhereFilter) UnmarshalJSON(b []byte) error {
	if f.clause != nil {
		return f.clause.UnmarshalJSON(b)
	}
	return nil
}

// NewChromaVectorStore 创建 retrieval 层 ChromaVectorStore。
//
// 对齐 Python ChromaVectorStore.__init__。
// vectorField 参数为 *vector_fields.ChromaVectorField 完整配置，
// 对齐 Python vector_field: ChromaVectorField。
func NewChromaVectorStore(
	config common.VectorStoreConfig,
	chromaPath string,
	textField string,
	vectorField *vector_fields.ChromaVectorField,
	sparseVectorField string,
	metadataField string,
	docIDField string,
	opts ...ChromaStoreOption,
) (*ChromaVectorStore, error) {
	// 对齐 Python: if not chroma_path or not chroma_path.strip():
	//   raise build_error(RETRIEVAL_VECTOR_STORE_PATH_NOT_FOUND)
	if strings.TrimSpace(chromaPath) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStorePathNotFound,
			exception.WithParam("error_msg", "chroma_path is required and cannot be empty"),
		)
	}

	if vectorField == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", "vector_field must not be nil"),
		)
	}

	s := &ChromaVectorStore{
		config:            config,
		collectionName:    config.CollectionName,
		chromaPath:        chromaPath,
		textField:         textField,
		sparseVectorField: sparseVectorField,
		metadataField:     metadataField,
		docIDField:        docIDField,
		databaseName:      config.DatabaseName,
		vectorField:       vectorField,
	}

	// 对齐 Python: self._distance_metric = config.distance_metric.replace("dot", "ip").replace("euclidean", "l2")
	s.distanceMetric = strings.ReplaceAll(
		strings.ReplaceAll(string(config.DistanceMetric), "dot", "ip"),
		"euclidean", "l2",
	)

	// 对齐 Python: self._construct_config = self.vector_field.to_dict(stage="construct")
	s.constructConfig = s.vectorField.ToConstructDict()
	s.constructConfig["space"] = s.distanceMetric

	// 对齐 Python: self._search_config = self.vector_field.to_dict(stage="search")
	s.searchConfig = s.vectorField.ToSearchDict()

	for _, opt := range opts {
		opt(s)
	}

	// S-50: 实装 databaseName（原为死字段）。
	// M-44: 建库逻辑抽取到 CreateChromaClient，供 indexer 等上层组件复用，
	// 对齐 Python ChromaVectorStore.create_client 静态方法。
	// 13.23 审查拍板 A：注入即跳过真实连接（对齐 WithMilvusClientOption
	// 先例）——测试经 WithChromaClientOption 注入桩客户端后，构造期
	// CreateChromaClient + GetOrCreateCollection 两步副作用全跳过；
	// s.collection 留 nil 安全：store 内部走惰性 getCollection()，
	// retriever 侧 queryByFilterChroma 直调 client.GetCollection。
	if s.client == nil {
		client, err := CreateChromaClient(config.DatabaseName, chromaPath)
		if err != nil {
			return nil, err
		}
		s.client = client

		// 对齐 Python: self._collection = self._client.get_or_create_collection(
		//   name=self.collection_name, configuration={"hnsw": self._construct_config | self._search_config})
		// S-48: 传完整 HNSW 配置（原仅传 distance metric）。
		// Python construct_config 含 ef_search（chroma_fields.py L60-67 标 IS_CONSTRUCT），
		// 合并 constructConfig|searchConfig 后 ef_search 由 construct 侧提供。
		//
		// 注：chroma-go v2 CreateCollectionOp 在 embeddingFunction==nil 时强制构建
		// ONNX 默认 EF（运行时下载 onnxruntime 原生库与 MiniLM 模型）。本项目所有向量
		// 均由 embedModel 预计算后显式传入，默认 EF 从未被真正使用，因此显式传入
		// chroma-go 自带的 consistent_hash 轻量 EF（纯 SHA-256，无原生库依赖）阻止
		// ONNX 下载——CI 冷缓存环境下曾因该下载触发 racefini SIGSEGV。
		collection, err := s.client.GetOrCreateCollection(context.Background(), s.collectionName,
			append(
				buildChromaHNSWCreateOptions(s.distanceMetric, s.constructConfig, s.searchConfig),
				chromav2.WithEmbeddingFunctionCreate(embeddings.NewConsistentHashEmbeddingFunction()),
			)...,
		)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("创建/获取 ChromaDB 集合失败: %s", err)),
				exception.WithCause(err),
			)
		}
		s.collection = collection
	}

	return s, nil
}

// Client 返回 ChromaDB 客户端
func (s *ChromaVectorStore) Client() chromav2.Client { return s.client }

// WithChromaClientOption 注入 ChromaDB 客户端实现。
//
// 13.23 审查拍板 A：对齐 WithMilvusClientOption 先例——注入即跳过
// 构造期 CreateChromaClient + GetOrCreateCollection 两步真实连接，
// 供测试桩注入消除 retriever 层 dispatch 胶水的测试盲区（spec §8-14）。
func WithChromaClientOption(c chromav2.Client) ChromaStoreOption {
	return func(s *ChromaVectorStore) {
		s.client = c
	}
}

// CreateChromaClient 创建 ChromaDB 持久化客户端并确保指定数据库存在。
//
// M-44: 对齐 Python ChromaVectorStore.create_client 静态方法
// （chroma_store.py L119-129）——非默认 dbName 时先经 AdminClient
// 检查/创建数据库，再以指定数据库创建主客户端；
// 默认/空 dbName 归一化为 DefaultDatabase（对齐 Python else 分支）。
// 供 indexer 等上层组件复用，避免每次操作重建 PersistentClient。
func CreateChromaClient(databaseName, chromaPath string) (chromav2.Client, error) {
	dbName := strings.TrimSpace(databaseName)
	if dbName == "" {
		dbName = chromav2.DefaultDatabase
	}
	clientOpts := []chromav2.PersistentClientOption{
		chromav2.WithPersistentPath(chromaPath),
		chromav2.WithPersistentLibraryAutoDownload(true),
	}
	if dbName != chromav2.DefaultDatabase {
		// 对齐 Python: admin_client = chromadb.AdminClient(...);
		//   if database_name not in {db.get("name") for db in admin_client.list_databases()}:
		//     admin_client.create_database(database_name)
		adminOpts := []chromav2.PersistentClientOption{
			chromav2.WithPersistentPath(chromaPath),
			chromav2.WithPersistentLibraryAutoDownload(true),
		}
		adminClient, err := chromav2.NewPersistentClient(adminOpts...)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("创建 ChromaDB AdminClient 失败: %s", err)),
				exception.WithCause(err),
			)
		}
		if err := ensureChromaDatabase(context.Background(), adminClient, dbName); err != nil {
			return nil, err
		}
		_ = adminClient.Close()
		clientOpts = append(clientOpts, chromav2.WithPersistentClientOptions(
			chromav2.WithDatabaseAndTenant(dbName, chromav2.DefaultTenant),
		))
	}
	client, err := chromav2.NewPersistentClient(clientOpts...)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("创建 ChromaDB 客户端失败: %s", err)),
			exception.WithCause(err),
		)
	}
	return client, nil
}

// Collection 返回 ChromaDB 集合
func (s *ChromaVectorStore) Collection() chromav2.Collection { return s.collection }

// DistanceMetric 返回距离度量字符串
func (s *ChromaVectorStore) DistanceMetric() string { return s.distanceMetric }

// DatabaseName 返回数据库名称（13.26 validate_index 消费）。
func (s *ChromaVectorStore) DatabaseName() string { return s.databaseName }

// TextField 返回文本字段名（13.26 validate_index 消费）。
func (s *ChromaVectorStore) TextField() string { return s.textField }

// VectorField 返回向量字段配置（13.26 validate_index 消费）。
// 返回 any 装箱联合类型事实：Chroma 侧 *ChromaVectorField、Milvus 侧
// MilvusVectorFieldConfig 接口——统一签名供 KB 包 attrEqual(DeepEqual) 比对。
func (s *ChromaVectorStore) VectorField() any { return s.vectorField }

// SparseVectorField 返回稀疏向量字段名（13.26 validate_index 消费）。
func (s *ChromaVectorStore) SparseVectorField() string { return s.sparseVectorField }

// MetadataField 返回元数据字段名（13.26 validate_index 消费）。
func (s *ChromaVectorStore) MetadataField() string { return s.metadataField }

// DocIDField 返回文档 ID 字段名（13.26 validate_index 消费）。
func (s *ChromaVectorStore) DocIDField() string { return s.docIDField }

// ConstructConfig 返回构建阶段配置
func (s *ChromaVectorStore) ConstructConfig() map[string]any { return s.constructConfig }

// SearchConfig 返回搜索阶段配置
func (s *ChromaVectorStore) SearchConfig() map[string]any { return s.searchConfig }

// CheckVectorField 校验向量字段配置是否一致。
// S-49: 从集合读取真实 HNSW 配置作 actual（原传空 map 校验形同虚设）。
// 对齐 Python: self._check_configs_matching(self._construct_config,
//
//	self.collection.configuration.get("hnsw", {}))
//
// Go SDK 差异补偿：chroma-go 老 metadata 路径键名为 hnsw:M/hnsw:construction_ef 等，
// Python chromadb 1.5.4 configuration 属性走 configuration_json 天然 snake_case 键，
// 故读回时需做一层还原映射（Go SDK 强加，Python 本身无此转换）。
func (s *ChromaVectorStore) CheckVectorField() error {
	collection, err := s.getCollection(context.Background())
	if err != nil {
		return err
	}
	actual := map[string]any{}
	if collection != nil && collection.Configuration() != nil {
		cfg := collection.Configuration()
		if rawHN, ok := cfg.GetRaw("hnsw"); ok {
			if hnMap, ok := rawHN.(map[string]any); ok {
				for k, v := range hnMap {
					actual[k] = v
				}
			}
		} else {
			// Go SDK 老 metadata 路径：配置平铺在 raw 顶层，键名 hnsw:M 等，
			// 需去掉 "hnsw:" 前缀并还原为 Python 键名。
			// 接口仅暴露 GetRaw，按已知 HNSW 常量集探测。
			for _, sdkKey := range []string{
				chromav2.HNSWM, chromav2.HNSWConstructionEF, chromav2.HNSWSearchEF,
				chromav2.HNSWSpace, chromav2.HNSWNumThreads, chromav2.HNSWBatchSize,
				chromav2.HNSWSyncThreshold, chromav2.HNSWResizeFactor,
			} {
				if v, ok := cfg.GetRaw(sdkKey); ok {
					pyKey := chromaMetadataKeyToPython(sdkKey)
					if pyKey != "" {
						actual[pyKey] = v
					}
				}
			}
		}
	}
	return CheckConfigsMatching(s.constructConfig, actual)
}

// Add 添加向量数据。
//
// 对齐 Python ChromaVectorStore.add + _add_batch。
func (s *ChromaVectorStore) Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error {
	storeOpts := NewStoreOptions(opts...)
	batchSize := storeOpts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultChromaBatchSize
	}

	processed := 0
	total := len(data)
	var cache []map[string]any

	for _, doc := range data {
		cache = append(cache, doc)
		if len(cache) >= batchSize {
			nodes := cache[:batchSize]
			cache = cache[batchSize:]
			if err := s.addBatch(ctx, nodes); err != nil {
				return err
			}
			processed += len(nodes)
			// 对齐 Python: if processed % 100 == 0: logger.info("Written %d/%d records to %s", ...)
			if processed%100 == 0 {
				logger.Info(logComponent).
					Int("processed", processed).
					Int("total", total).
					Str("collection_name", s.collectionName).
					Msg("write progress")
			}
		}
	}

	if len(cache) > 0 {
		if err := s.addBatch(ctx, cache); err != nil {
			return err
		}
		processed += len(cache)
	}

	// 对齐 Python: logger.info("Writing completed, total %d/%d records to %s", ...)
	logger.Info(logComponent).
		Int("processed", processed).
		Int("total", total).
		Str("collection_name", s.collectionName).
		Msg("write completed")

	return nil
}

// Search 向量搜索。
// 对齐 Python ChromaVectorStore.search。
// M-27: filters 对齐 Python dict | QueryExpr——QueryExpr 走 to_expr("chroma")
// 提取 where/where_document，dict 语义由调用方经 query.EqMap 折叠。
func (s *ChromaVectorStore) Search(ctx context.Context, queryVector []float64, topK int, filters query.QueryExpr, _ ...StoreOption) ([]common.SearchResult, error) {
	collection, err := s.getCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryEmb := embeddings.NewEmbeddingFromFloat64(queryVector)
	queryOpts := []chromav2.QueryOption{
		chromav2.WithQueryEmbeddings(queryEmb),
		chromav2.WithNResults(topK),
		chromav2.WithInclude(chromav2.IncludeDocuments, chromav2.IncludeMetadatas, chromav2.IncludeDistances),
	}
	if wf, wdf := s.buildChromaFilterPair(filters); wf != nil || wdf != nil {
		if wf != nil {
			queryOpts = append(queryOpts, chromav2.WithWhere(wf))
		}
		if wdf != nil {
			queryOpts = append(queryOpts, chromav2.WithWhereDocument(wdf))
		}
	}

	result, err := collection.Query(ctx, queryOpts...)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "SEARCH_ERROR").
			Str("method", "Search").
			Err(err).
			Msg("vector search failed")
		return nil, err
	}

	return s.chromaResultToSearchResults(result, "vector"), nil
}

// SparseSearch 稀疏搜索（文本匹配）。
// 对齐 Python ChromaVectorStore.sparse_search。
// M-27: filters 对齐 Python dict | QueryExpr，nil 表示无过滤。
func (s *ChromaVectorStore) SparseSearch(ctx context.Context, queryText string, topK int, filters query.QueryExpr, _ ...StoreOption) ([]common.SearchResult, error) {
	collection, err := s.getCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryOpts := []chromav2.QueryOption{
		chromav2.WithQueryTexts(queryText),
		chromav2.WithNResults(topK),
		chromav2.WithInclude(chromav2.IncludeDocuments, chromav2.IncludeMetadatas, chromav2.IncludeDistances),
	}
	if wf, wdf := s.buildChromaFilterPair(filters); wf != nil || wdf != nil {
		if wf != nil {
			queryOpts = append(queryOpts, chromav2.WithWhere(wf))
		}
		if wdf != nil {
			queryOpts = append(queryOpts, chromav2.WithWhereDocument(wdf))
		}
	}

	result, err := collection.Query(ctx, queryOpts...)
	if err != nil {
		// 对齐 Python: except: logger.warning(...); return []
		// T-22: 对齐 Python 删除自加的截断，直接输出完整 query_text
		logger.Warn(logComponent).
			Str("method", "SparseSearch").
			Str("query_text", queryText).
			Err(err).
			Msg("text search failed")
		return nil, nil
	}

	return s.chromaResultToSearchResults(result, "sparse"), nil
}

// HybridSearch 混合搜索（向量+文本 RRF 融合）。
// 对齐 Python ChromaVectorStore.hybrid_search。
// M-27: filters 对齐 Python dict | QueryExpr，nil 表示无过滤。
func (s *ChromaVectorStore) HybridSearch(ctx context.Context, queryText string, queryVector []float64, topK int, alpha float64, filters query.QueryExpr, _ ...StoreOption) ([]common.SearchResult, error) {
	type searchResult struct {
		results []common.SearchResult
		err     error
		mode    string
	}

	resultCh := make(chan searchResult, 2)

	// 并发执行向量搜索和文本搜索
	go func() {
		if queryVector != nil {
			results, err := s.Search(ctx, queryVector, topK*2, filters)
			resultCh <- searchResult{results: results, err: err, mode: "vector"}
		} else {
			resultCh <- searchResult{mode: "vector"}
		}
	}()

	go func() {
		results, err := s.SparseSearch(ctx, queryText, topK*2, filters)
		resultCh <- searchResult{results: results, err: err, mode: "text"}
	}()

	var resultsList [][]common.RetrievalResult
	idMapping := make(map[string]string)

	for i := 0; i < 2; i++ {
		sr := <-resultCh
		if sr.err != nil {
			logger.Warn(logComponent).Str("mode", sr.mode).Err(sr.err).Msg("hybrid search partial path failed")
			continue
		}
		if len(sr.results) == 0 {
			continue
		}

		var retrievalResults []common.RetrievalResult
		for _, searchRes := range sr.results {
			idMapping[searchRes.Text] = searchRes.ID
			metadata := make(map[string]any)
			for k, v := range searchRes.Metadata {
				metadata[k] = v
			}
			metadata["id"] = searchRes.ID

			docID, _ := searchRes.Metadata[s.docIDField].(string)
			chunkID, _ := searchRes.Metadata["chunk_id"].(string)
			retrievalResults = append(retrievalResults, common.RetrievalResult{
				Text:     searchRes.Text,
				Score:    searchRes.Score,
				Metadata: metadata,
				DocID:    docID,
				ChunkID:  chunkID,
			})
		}
		resultsList = append(resultsList, retrievalResults)
	}

	if len(resultsList) == 0 {
		return nil, nil
	}

	fusedRetrievalResults := utils.RRFFusion(resultsList, 60)

	var fusedResults []common.SearchResult
	for i, rr := range fusedRetrievalResults {
		if i >= topK {
			break
		}
		resultID, _ := rr.Metadata["id"].(string)
		if resultID == "" {
			resultID = idMapping[rr.Text]
		}
		// T-24: 对齐 Python `str(hash(rr.text))` 最终兜底——两层未命中时
		// 用文本哈希字符串化，保证 ID 恒非空（Go fnv 与 Python hash 算法不同，
		// 仅保证非空唯一性，不保证跨语言值一致）。
		if resultID == "" {
			h := fnv.New64a()
			_, _ = h.Write([]byte(rr.Text))
			resultID = fmt.Sprintf("%d", h.Sum64())
		}
		metadata := make(map[string]any)
		for k, v := range rr.Metadata {
			if k != "id" {
				metadata[k] = v
			}
		}
		fusedResults = append(fusedResults, common.SearchResult{
			ID:       resultID,
			Text:     rr.Text,
			Score:    rr.Score,
			Metadata: metadata,
		})
	}

	return fusedResults, nil
}

// Delete 删除向量。
// 对齐 Python ChromaVectorStore.delete。
// M-27: filterExpr 对齐 Python str | QueryExpr——QueryExpr 走 to_expr("chroma")
// 提取 where 过滤；Go 不支持裸字符串过滤（Python str 分支同样 warning + False）。
func (s *ChromaVectorStore) Delete(ctx context.Context, ids []string, filterExpr query.QueryExpr) (bool, error) {
	collection, err := s.getCollection(ctx)
	if err != nil {
		return false, err
	}

	if len(ids) > 0 {
		chromaIDs := make([]chromav2.DocumentID, len(ids))
		for i, id := range ids {
			chromaIDs[i] = chromav2.DocumentID(id)
		}
		if err := collection.Delete(ctx, chromav2.WithIDs(chromaIDs...)); err != nil {
			logger.Error(logComponent).Str("event_type", "DELETE_ERROR").Err(err).Msg("failed to delete vector")
			return false, err
		}
		return true, nil
	}

	if filterExpr != nil {
		if wf, wdf := s.buildChromaFilterPair(filterExpr); wf != nil || wdf != nil {
			delOpts := []chromav2.DeleteOption{}
			if wf != nil {
				delOpts = append(delOpts, chromav2.WithWhere(wf))
			}
			if wdf != nil {
				delOpts = append(delOpts, chromav2.WithWhereDocument(wdf))
			}
			if err := collection.Delete(ctx, delOpts...); err != nil {
				logger.Error(logComponent).Str("event_type", "DELETE_ERROR").Err(err).Msg("failed to delete vector")
				return false, err
			}
			return true, nil
		}
		// 对齐 Python: logger.warning("ChromaDB does not support string filter expressions.")
		logger.Warn(logComponent).Msg("ChromaDB does not support string filter expressions.")
		return false, nil
	}

	return false, nil
}

// TableExists 检查集合是否存在。
func (s *ChromaVectorStore) TableExists(ctx context.Context, tableName string) (bool, error) {
	collections, err := s.client.ListCollections(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range collections {
		if c.Name() == tableName {
			return true, nil
		}
	}
	return false, nil
}

// DeleteTable 删除集合。
func (s *ChromaVectorStore) DeleteTable(ctx context.Context, tableName string) error {
	return s.client.DeleteCollection(ctx, tableName)
}

// Close 关闭存储。
//
// Chroma 客户端关闭失败在 Python 侧为异常，这里降级为忽略并返回 nil
// （13.26 的 Warn 吞由调用方 maybeClose 承担）。
func (s *ChromaVectorStore) Close() error {
	if s.client != nil {
		_ = s.client.Close()
	}
	return nil
}

// CollectionName 返回集合名称（13.23 回填：GraphRetriever 动态创建读取）。
func (s *ChromaVectorStore) CollectionName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collectionName
}

// SetCollectionName 设置集合并失效集合缓存（13.23 回填）。
//
// Python 侧 collection_name 字段赋值后，collection.get 每次按名新建；
// Go 侧 getCollection 有 nil 缓存，Set 时必须清空对齐"每次按名取"
// 语义（spec §7-3）。
func (s *ChromaVectorStore) SetCollectionName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collectionName = name
	s.collection = nil
}

// buildChromaFilterPair M-27: 将 QueryExpr 转换为 Chroma where/where_document 过滤对。
//
// 对齐 Python `query_args.update({k: v for k, v in filters.to_expr("chroma").items() if v})`：
//   - ToExpr("chroma") 输出 {"where": ..., "where_document": ...} 双键字典
//   - falsy 过滤：where/where_document 为空 dict 时不注入对应选项（对齐 Python if v）
//
// filters 为 nil 时返回 (nil, nil)。where 恒非 nil 时才可能携带 where_document
// （纯文本过滤 MatchExpr 的 where 为空 dict，对齐 Python 不注入 where）。
func (s *ChromaVectorStore) buildChromaFilterPair(filters query.QueryExpr) (chromav2.WhereFilter, chromav2.WhereDocumentFilter) {
	if filters == nil {
		return nil, nil
	}

	raw, err := filters.ToExpr("chroma")
	if err != nil {
		// 对齐 Python: to_expr 内部异常沿调用链上抛（chroma_def 校验失败等）
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "buildChromaFilterPair").
			Err(err).
			Msg("failed to convert filter expression to chroma")
		return nil, nil
	}

	exprMap, ok := raw.(map[string]any)
	if !ok {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "buildChromaFilterPair").
			Str("expr_type", fmt.Sprintf("%T", raw)).
			Msg("chroma filter expression is not a map")
		return nil, nil
	}

	var whereFilter chromav2.WhereFilter
	var whereDocFilter chromav2.WhereDocumentFilter

	if where, ok := exprMap["where"].(map[string]any); ok && len(where) > 0 {
		whereFilter = &chromaMapWhereFilter{m: where}
	}
	if whereDoc, ok := exprMap["where_document"].(map[string]any); ok && len(whereDoc) > 0 {
		whereDocFilter = &chromaMapWhereDocumentFilter{m: whereDoc}
	}

	return whereFilter, whereDocFilter
}

// WrapChromaWhereMap M-27: 将 Chroma 原生 where 字典包装为 WhereFilter。
//
// 对齐 Python `collection.get(where={self.doc_id_field: doc_id})` 直传 dict 的
// wire 语义（{"field": "value"} 简写等值，ChromaDB 服务端原生支持）。
// 供 indexer 等写入路径跨包使用；nil 或空 map 返回 nil。
func WrapChromaWhereMap(m map[string]any) chromav2.WhereFilter {
	if len(m) == 0 {
		return nil
	}
	return &chromaMapWhereFilter{m: m}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureChromaDatabase 确保指定数据库存在，不存在则创建。
// S-50: 对齐 Python chroma_store.py L119-129 AdminClient 语义。
func ensureChromaDatabase(ctx context.Context, adminClient chromav2.Client, dbName string) error {
	dbs, err := adminClient.ListDatabases(ctx, chromav2.NewDefaultTenant())
	if err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("ChromaDB ListDatabases 失败: %s", err)),
			exception.WithCause(err),
		)
	}
	for _, db := range dbs {
		if db != nil && db.Name() == dbName {
			return nil
		}
	}
	if _, err := adminClient.CreateDatabase(ctx, chromav2.NewDatabase(dbName, chromav2.NewDefaultTenant())); err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("ChromaDB CreateDatabase %s 失败: %s", dbName, err)),
			exception.WithCause(err),
		)
	}
	return nil
}

// buildChromaHNSWCreateOptions 根据 construct/search 配置构建完整 HNSW 创建选项。
// S-48: 对齐 Python configuration={"hnsw": self._construct_config | self._search_config}——
// max_neighbors/ef_construction/ef_search 从合并配置读取，
// resize_factor/num_threads/batch_size/sync_threshold 从 extra（searchConfig）读取。
func buildChromaHNSWCreateOptions(distanceMetric string, constructConfig, searchConfig map[string]any) []chromav2.CreateCollectionOption {
	opts := []chromav2.CreateCollectionOption{
		chromav2.WithHNSWSpaceCreate(mapToChromaDistanceMetric(distanceMetric)),
	}
	// 合并配置（对齐 Python | 合并语义：search 键不覆盖 construct 已有键）
	merged := make(map[string]any, len(constructConfig)+len(searchConfig))
	for k, v := range searchConfig {
		merged[k] = v
	}
	for k, v := range constructConfig {
		merged[k] = v
	}
	if v, ok := merged["max_neighbors"].(int); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWMCreate(v))
	}
	if v, ok := merged["ef_construction"].(int); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWConstructionEfCreate(v))
	}
	// ef_search 在 Python chroma_fields.py 中标 IS_CONSTRUCT，合并在 constructConfig 侧
	if v, ok := merged["ef_search"].(float64); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWSearchEfCreate(int(v)))
	} else if v, ok := merged["ef_search"].(int); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWSearchEfCreate(v))
	}
	if v, ok := merged["batch_size"].(int); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWBatchSizeCreate(v))
	}
	if v, ok := merged["sync_threshold"].(int); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWSyncThresholdCreate(v))
	}
	// S-18 残留订正：SDK v0.4.1 实际有对应 Create 选项
	// （client.go L476 WithHNSWNumThreadsCreate / L489 WithHNSWResizeFactorCreate），
	// 原注释"无对应 Create 选项"为误记，此处补传对齐 Python 全量 HNSW 配置。
	if v, ok := merged["num_threads"].(int); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWNumThreadsCreate(v))
	}
	if v, ok := merged["resize_factor"].(float64); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWResizeFactorCreate(v))
	} else if v, ok := merged["resize_factor"].(int); ok && v > 0 {
		opts = append(opts, chromav2.WithHNSWResizeFactorCreate(float64(v)))
	}
	return opts
}

// chromaMetadataKeyToPython 将 Go SDK 老 metadata 键名还原为 Python configuration 键名。
// S-49: Go SDK 强加的补偿层（Python chromadb 1.5.4 configuration_json 键天然一致无需转换）。
func chromaMetadataKeyToPython(sdkKey string) string {
	switch sdkKey {
	case chromav2.HNSWM:
		return "max_neighbors"
	case chromav2.HNSWConstructionEF:
		return "ef_construction"
	case chromav2.HNSWSearchEF:
		return "ef_search"
	case chromav2.HNSWSpace:
		return "space"
	case chromav2.HNSWNumThreads:
		return "num_threads"
	case chromav2.HNSWBatchSize:
		return "batch_size"
	case chromav2.HNSWSyncThreshold:
		return "sync_threshold"
	case chromav2.HNSWResizeFactor:
		return "resize_factor"
	default:
		return ""
	}
}

// addBatch 批量添加数据到 ChromaDB。
// 对齐 Python _add_batch。
func (s *ChromaVectorStore) addBatch(ctx context.Context, nodes []map[string]any) error {
	var ids []chromav2.DocumentID
	var allEmbeddings []embeddings.Embedding
	var texts []string
	var metas []chromav2.DocumentMetadata

	for _, node := range nodes {
		// 对齐 Python: embedding = node.get(self.vector_field.vector_field, [])
		embedding, _ := node[s.vectorField.VectorFieldName].([]float64)
		if len(embedding) == 0 {
			// 对齐 Python: logger.warning(f"Node has no embedding, skipping: ...")
			nodeID, _ := node["id"].(string)
			logger.Warn(logComponent).Str("node_id", nodeID).Msg("node has no embedding, skipping")
			continue
		}

		// 对齐 Python: node_id = str(node.get("id", node.get("pk", ""))); if not node_id: uuid.uuid4()
		nodeID, _ := node["id"].(string)
		if nodeID == "" {
			nodeID, _ = node["pk"].(string)
		}
		if nodeID == "" {
			nodeID = uuid.NewString()
		}
		ids = append(ids, chromav2.DocumentID(nodeID))
		allEmbeddings = append(allEmbeddings, embeddings.NewEmbeddingFromFloat64(embedding))

		// 对齐 Python: text = node.get(self.text_field, "")
		text, _ := node[s.textField].(string)
		texts = append(texts, text)

		// 对齐 Python: metadata = {}; if self.metadata_field in node: ...
		metaMap := make(map[string]any)
		if rawMeta, ok := node[s.metadataField]; ok {
			switch m := rawMeta.(type) {
			case map[string]any:
				for k, v := range m {
					metaMap[k] = v
				}
			case string:
				var parsed map[string]any
				if err := json.Unmarshal([]byte(m), &parsed); err == nil {
					for k, v := range parsed {
						metaMap[k] = v
					}
				} else {
					// T-25: 对齐 Python addBatch 的 warning——metadata JSON
					// 解析失败必须留排查线索，静默丢弃会掩盖数据损坏。
					logger.Warn(logComponent).
						Str("event_type", "VECTOR_STORE_WARN").
						Str("metadata_field", s.metadataField).
						Err(err).
						Msg("failed to load metadata")
				}
			}
		}

		// 对齐 Python: if self.doc_id_field in node: metadata[self.doc_id_field] = str(node[self.doc_id_field])
		if docID, ok := node[s.docIDField]; ok {
			metaMap[s.docIDField] = fmt.Sprintf("%v", docID)
		}
		if chunkID, ok := node["chunk_id"]; ok {
			metaMap["chunk_id"] = fmt.Sprintf("%v", chunkID)
		}
		if sparseVec, ok := node[s.sparseVectorField]; ok {
			jsonBytes, err := json.Marshal(sparseVec)
			if err == nil {
				metaMap[s.sparseVectorField] = string(jsonBytes)
			}
		}

		docMeta, err := chromav2.NewDocumentMetadataFromMap(metaMap)
		if err != nil {
			docMeta = chromav2.NewDocumentMetadata()
		}
		metas = append(metas, docMeta)
	}

	if len(ids) == 0 {
		return nil
	}

	collection, err := s.getCollection(ctx)
	if err != nil {
		return err
	}

	addOpts := []chromav2.AddOption{
		chromav2.WithIDs(ids...),
		chromav2.WithEmbeddings(allEmbeddings...),
		chromav2.WithTexts(texts...),
		chromav2.WithMetadatas(metas...),
	}

	if err := collection.Add(ctx, addOpts...); err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("批量添加数据失败: %s", err)),
			exception.WithCause(err),
		)
	}
	return nil
}

// getCollection 获取最新集合引用。
func (s *ChromaVectorStore) getCollection(ctx context.Context) (chromav2.Collection, error) {
	s.mu.RLock()
	if s.collection != nil {
		s.mu.RUnlock()
		return s.collection, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	collection, err := s.client.GetCollection(ctx, s.collectionName)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("获取 ChromaDB 集合失败: %s", err)),
			exception.WithCause(err),
		)
	}
	s.collection = collection
	return collection, nil
}

// chromaResultToSearchResults 将 ChromaDB 搜索结果转为 SearchResult 列表。
// 对齐 Python ChromaVectorStore._chroma_result_to_search_results。
func (s *ChromaVectorStore) chromaResultToSearchResults(result chromav2.QueryResult, mode string) []common.SearchResult {
	if result == nil {
		return nil
	}

	idGroups := result.GetIDGroups()
	if len(idGroups) == 0 {
		return nil
	}

	docGroups := result.GetDocumentsGroups()
	metaGroups := result.GetMetadatasGroups()
	distGroups := result.GetDistancesGroups()

	var searchResults []common.SearchResult

	// 使用第一个查询组的结果（单查询向量）
	ids := idGroups[0]
	for idx, resultID := range ids {
		text := ""
		if len(docGroups) > 0 && idx < len(docGroups[0]) && docGroups[0][idx] != nil {
			text = docGroups[0][idx].ContentString()
		}

		metadata := make(map[string]any)
		if len(metaGroups) > 0 && idx < len(metaGroups[0]) && metaGroups[0][idx] != nil {
			meta := metaGroups[0][idx]
			for _, key := range chromaMetaKeys(meta) {
				if val, ok := meta.GetRaw(key); ok {
					if mv, isMv := val.(chromav2.MetadataValue); isMv {
						if rawVal, ok := mv.GetRaw(); ok {
							metadata[key] = rawVal
						}
					} else {
						metadata[key] = val
					}
				}
			}
		}

		// 对齐 Python: if "doc_id" not in metadata: metadata["doc_id"] = metadata.pop(self.doc_id_field, None)
		// M-20: Python dict.pop(key, None) 在键缺失时仍会设置 "doc_id": None，
		// sparse 模式下 doc_id 缺失必须显式设 nil，保持键存在性语义一致
		if _, ok := metadata["doc_id"]; !ok {
			if docID, ok := metadata[s.docIDField]; ok {
				metadata["doc_id"] = docID
				delete(metadata, s.docIDField)
			} else {
				metadata["doc_id"] = nil
			}
		}

		// M-30: 对齐 Python 搜索结果的 sparse_vector 反序列化——
		// 写入侧把稀疏向量 json.Marshal 成字符串存 metadata，读出时必须
		// json.Unmarshal 还原，否则消费方拿到的是 JSON 字符串而非结构。
		if sparseStr, ok := metadata[s.sparseVectorField].(string); ok {
			var sparseVec any
			if err := json.Unmarshal([]byte(sparseStr), &sparseVec); err == nil {
				metadata[s.sparseVectorField] = sparseVec
			} else {
				logger.Warn(logComponent).
					Str("event_type", "VECTOR_STORE_WARN").
					Str("sparse_vector_field", s.sparseVectorField).
					Err(err).
					Msg("failed to load sparse vector")
			}
		}

		var rawScoreVal *float64
		if len(distGroups) > 0 && idx < len(distGroups[0]) {
			v := float64(distGroups[0][idx])
			rawScoreVal = &v
		}

		var finalScore float64
		var rawScoreScaled *float64

		switch mode {
		case "vector":
			if rawScoreVal != nil {
				switch s.distanceMetric {
				case "l2":
					// S-19: 对齐 Python convert_l2_squared(raw_score, max_dist=4.0) 默认值，
					// 传 0 会导致 l2 度量得分全部归零/NaN（同仓其余 6 处调用均传 4.0）
					scaled := vector.ConvertL2Squared(*rawScoreVal, 4.0)
					rawScoreScaled = &scaled
					finalScore = scaled
				case "cosine":
					scaled := vector.ConvertCosineDistance(*rawScoreVal)
					rawScoreScaled = &scaled
					finalScore = scaled
				default: // ip
					scaled := vector.ConvertIPDistance(*rawScoreVal)
					rawScoreScaled = &scaled
					finalScore = scaled
				}
			}
		case "sparse":
			if rawScoreVal != nil {
				if *rawScoreVal <= 1.0 {
					finalScore = 1.0 - *rawScoreVal
				} else {
					finalScore = *rawScoreVal
				}
			} else {
				finalScore = 0.5
			}
		}

		if rawScoreVal != nil {
			metadata["raw_score"] = *rawScoreVal
		}
		if rawScoreScaled != nil {
			metadata["raw_score_scaled"] = *rawScoreScaled
		}

		searchResults = append(searchResults, common.SearchResult{
			ID:       string(resultID),
			Text:     text,
			Score:    finalScore,
			Metadata: metadata,
		})
	}

	return searchResults
}

// mapToChromaDistanceMetric 将距离度量字符串转为 ChromaDB 距离函数。
func mapToChromaDistanceMetric(metric string) embeddings.DistanceMetric {
	switch strings.ToUpper(metric) {
	case "L2":
		return embeddings.L2
	case "IP":
		return embeddings.IP
	case "COSINE":
		return embeddings.COSINE
	default:
		return embeddings.COSINE
	}
}

// chromaMetaKeys 获取 DocumentMetadata 的所有键。
func chromaMetaKeys(meta chromav2.DocumentMetadata) []string {
	if impl, ok := meta.(*chromav2.DocumentMetadataImpl); ok {
		return impl.Keys()
	}
	return nil
}
