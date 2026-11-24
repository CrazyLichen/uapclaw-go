// Package vector_store 内 SQLite 向量存储实现（Go 侧私有 provider，spec D-25-7）。
//
// 上游 Python 仅提供 milvus/chroma/pgvector 三种 provider；本实现是 Go 侧
// 新增的私有存储类型，服务于本机/CI 无外部向量库服务场景下的 bm25/hybrid
// 真链验证与轻量部署。回收条件：上游引入同能力 provider 时迁移对齐。
//
// 能力面：
//   - VectorSearch：sqlite-vec（vec0 虚拟表）KNN，dense 真 ANN
//   - SparseSearch：FTS5 MATCH + bm25() 打分（真 BM25，非子串匹配）
//   - HybridSearch：两路 + RRF 融合（复用 retrieval/utils.RRFFusion，chroma 同款）
//
// 约束：
//   - FTS5 需 mattn/go-sqlite3 以 `-tags sqlite_fts5` 编译（本仓 Makefile 标准标签）
//   - vec0 扩展按连接加载（third_party/sqlite-vec/<GOOS>-<GOARCH>/vec0.so），
//     本实现持单条专用连接（db.SetMaxOpenConns(1) + 专用 Conn）规避连接池
//     多连接重复加载问题，并发经 mu 串行化
//   - 无向量的写入仍进 chunks/FTS（bm25-only 场景可用），向量检索自然不命中
//
// 对应 Python 代码：无（Go 私有 provider，接口面对齐 VectorStore 契约）
package vector_store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"sync"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/vector"
	lite "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/memory/lite"
	retrievalcommon "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/utils"
)

// sqliteLogComponent SQLite 向量存储日志组件
const sqliteLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// SQLiteVectorStore SQLite 向量存储（vec0 + FTS5）。
// Go 侧私有 provider（spec D-25-7），接口面对齐 VectorStore 契约。
type SQLiteVectorStore struct {
	// config 存储配置
	config retrievalcommon.VectorStoreConfig
	// collectionName 集合名（表名前缀）
	collectionName string
	// textField 文本字段名
	textField string
	// vectorFieldName 向量字段名
	vectorFieldName string
	// sparseVectorField 稀疏向量字段名
	sparseVectorField string
	// metadataField 元数据字段名
	metadataField string
	// docIDField 文档 ID 字段名
	docIDField string
	// distanceMetric 距离度量（l2/cosine；vec0 默认 l2）
	distanceMetric string
	// dims 向量维度（首条带向量写入时确定并建 vec0 表）
	dims int
	// mu 串行化所有操作（单专用连接）
	mu sync.Mutex
	// db 底层句柄（Close 时兜底关闭）
	db *sql.DB
	// conn 专用连接（vec0 扩展按连接加载，池内其余连接不加载扩展）
	conn *sql.Conn
	// vecAvailable vec0 扩展可用标志（加载失败降级：dense 路不可用，FTS 仍可用）
	vecAvailable bool
	// closed 关闭标志
	closed bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSQLiteVectorStore 创建 SQLite 向量存储。
// dbPath 为 SQLite 文件路径（":memory:" 亦可，测试友好）。
// 字段名参数空值取默认（与 chroma 同款默认值）。
func NewSQLiteVectorStore(config retrievalcommon.VectorStoreConfig, dbPath,
	textField, vectorFieldName, sparseVectorField, metadataField, docIDField string,
) (*SQLiteVectorStore, error) {
	if config.CollectionName == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", "sqlite store: collection_name is required"),
		)
	}
	if strings.HasPrefix(config.CollectionName, "sqlite_") {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", "sqlite store: collection_name 不能以保留前缀 sqlite_ 开头"),
		)
	}

	if textField == "" {
		textField = defaultTextField
	}
	if vectorFieldName == "" {
		vectorFieldName = defaultVectorFieldName
	}
	if sparseVectorField == "" {
		sparseVectorField = defaultSparseVectorField
	}
	if metadataField == "" {
		metadataField = defaultMetadataField
	}
	if docIDField == "" {
		docIDField = defaultDocIDField
	}

	metric := string(config.DistanceMetric)
	switch metric {
	case "", string(retrievalcommon.DistanceMetricEuclidean):
		metric = "l2"
	case string(retrievalcommon.DistanceMetricCosine):
		metric = "cosine"
	default:
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 不支持的 distance_metric: %s（支持 l2/cosine）", metric)),
		)
	}

	// DSN：WAL + busy_timeout，与 harness memory 同款
	dsn := dbPath + "?_busy_timeout=5000&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 打开数据库失败: %s", err)),
			exception.WithCause(err),
		)
	}
	db.SetMaxOpenConns(1) // 单连接池：vec0 扩展按连接加载，避免池内新连接失效

	conn, err := db.Conn(context.Background())
	if err != nil {
		_ = db.Close()
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 获取专用连接失败: %s", err)),
			exception.WithCause(err),
		)
	}

	s := &SQLiteVectorStore{
		config:            config,
		collectionName:    config.CollectionName,
		textField:         textField,
		vectorFieldName:   vectorFieldName,
		sparseVectorField: sparseVectorField,
		metadataField:     metadataField,
		docIDField:        docIDField,
		distanceMetric:    metric,
		db:                db,
		conn:              conn,
	}

	if err := s.loadVec0(); err != nil {
		// 降级不失败：dense 路不可用，FTS/chunks 仍可用（Warn 留排查线索）
		logger.Warn(sqliteLogComponent).
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("sqlite-vec extension load failed; dense search disabled")
		s.vecAvailable = false
	}

	if err := s.ensureBaseTables(); err != nil {
		_ = conn.Close()
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// ──────────────────────────── 访问器（storageAttrProvider 契约） ────────────────────────────

// CollectionName 集合名
func (s *SQLiteVectorStore) CollectionName() string { return s.collectionName }

// SetCollectionName 修改集合名（表前缀随之切换）
func (s *SQLiteVectorStore) SetCollectionName(name string) { s.collectionName = name }

// DatabaseName 数据库名称（7 属性比对项）
func (s *SQLiteVectorStore) DatabaseName() string {
	if s.config.DatabaseName == "" {
		return "sqlite"
	}
	return s.config.DatabaseName
}

// DistanceMetric 距离度量（7 属性比对项）
func (s *SQLiteVectorStore) DistanceMetric() string { return s.distanceMetric }

// TextField 文本字段名（7 属性比对项）
func (s *SQLiteVectorStore) TextField() string { return s.textField }

// VectorField 向量字段名（7 属性比对项）
func (s *SQLiteVectorStore) VectorField() any { return s.vectorFieldName }

// SparseVectorField 稀疏向量字段名（7 属性比对项；13.26 validate_index 消费）
func (s *SQLiteVectorStore) SparseVectorField() string { return s.sparseVectorField }

// MetadataField 元数据字段名（7 属性比对项）
func (s *SQLiteVectorStore) MetadataField() string { return s.metadataField }

// DocIDField 文档 ID 字段名（7 属性比对项）
func (s *SQLiteVectorStore) DocIDField() string { return s.docIDField }

// ──────────────────────────── VectorStore 接口实现 ────────────────────────────

// CheckVectorField 校验向量字段配置与实际库一致：
// vec0 表已存在时确认表可查（无表 → 惰性建表前无需校验，返回 nil）。
func (s *SQLiteVectorStore) CheckVectorField() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkVectorFieldLocked()
}

// Add 添加向量数据。
// data 元素键：id/pk、{vectorFieldName}（[]float64）、{textField}（string）、
// {metadataField}（map/JSON 字符串）、{docIDField}、chunk_id。
// 无向量的节点仍写 chunks/FTS（bm25-only 场景），仅跳过 vec 表。
func (s *SQLiteVectorStore) Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(ctx, data)
}

// Search 向量搜索（sqlite-vec KNN；分数按度量换算，语义对齐 chroma）。
func (s *SQLiteVectorStore) Search(ctx context.Context, queryVector []float64, topK int,
	filters query.QueryExpr, _ ...StoreOption,
) ([]retrievalcommon.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.searchLocked(ctx, queryVector, topK, filters)
}

// SparseSearch 稀疏搜索（FTS5 bm25 真打分；分数取 -bm25 归一为越大越相关）。
func (s *SQLiteVectorStore) SparseSearch(ctx context.Context, queryText string, topK int,
	filters query.QueryExpr, _ ...StoreOption,
) ([]retrievalcommon.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sparseSearchLocked(ctx, queryText, topK, filters)
}

// HybridSearch 混合搜索（两路 + RRF 融合，chroma 同款语义）。
func (s *SQLiteVectorStore) HybridSearch(ctx context.Context, queryText string, queryVector []float64,
	topK int, alpha float64, filters query.QueryExpr, _ ...StoreOption,
) ([]retrievalcommon.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hybridSearchLocked(ctx, queryText, queryVector, topK, alpha, filters)
}

// Delete 删除向量（按 id；filterExpr 非 EqMap 形态降级忽略，对齐 chroma 裸字符串 warning 语义）。
func (s *SQLiteVectorStore) Delete(ctx context.Context, ids []string, filterExpr query.QueryExpr) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteLocked(ctx, ids, filterExpr)
}

// TableExists 检查集合是否存在（任一同前缀表存在即视为存在）。
func (s *SQLiteVectorStore) TableExists(ctx context.Context, tableName string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tableExistsLocked(ctx, tableName)
}

// DeleteTable 删除集合（四表连带清理）。
func (s *SQLiteVectorStore) DeleteTable(ctx context.Context, tableName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteTableLocked(ctx, tableName)
}

// Close 关闭存储（专用连接 + db 句柄）。
func (s *SQLiteVectorStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.conn.Close(); err != nil {
		return err
	}
	return s.db.Close()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadVec0 加载 vec0 扩展（复用 lite 包 loader；按专用连接加载一次）。
func (s *SQLiteVectorStore) loadVec0() error {
	vecPath := lite.ResolveVec0Path()
	if !lite.IsVec0Available() {
		return fmt.Errorf("vec0 不存在: %s", vecPath)
	}
	err := s.conn.Raw(func(driverConn any) error {
		dc, ok := driverConn.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("不支持 LoadExtension 的驱动类型")
		}
		return lite.LoadVec0Extension(dc, vecPath)
	})
	if err != nil {
		return err
	}
	s.vecAvailable = true
	logger.Info(sqliteLogComponent).
		Str("collection_name", s.collectionName).
		Msg("sqlite-vec extension loaded successfully")
	return nil
}

// ensureBaseTables 建 chunks 主表（FTS/vec 表建表延迟到首次使用）。
func (s *SQLiteVectorStore) ensureBaseTables() error {
	ddl := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s_chunks (id TEXT PRIMARY KEY, content TEXT, metadata TEXT, doc_id TEXT, chunk_id TEXT)",
		s.collectionName)
	_, err := s.conn.ExecContext(context.Background(), ddl)
	if err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 建表失败: %s", err)),
			exception.WithCause(err),
		)
	}
	return nil
}

// ensureFTSTable 建 FTS5 虚拟表（id UNINDEXED 便于按 id 删除）。
func (s *SQLiteVectorStore) ensureFTSTable(ctx context.Context) error {
	ddl := fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %s_fts USING fts5(content, id UNINDEXED)",
		s.collectionName)
	_, err := s.conn.ExecContext(ctx, ddl)
	if err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 建 FTS5 表失败（需 -tags sqlite_fts5 编译）: %s", err)),
			exception.WithCause(err),
		)
	}
	return nil
}

// ensureVecTable 建 vec0 虚拟表（惰性：首条带向量写入时按实际维度建）+ rowid 映射表。
func (s *SQLiteVectorStore) ensureVecTable(ctx context.Context, dims int) error {
	ddl := fmt.Sprintf("CREATE VIRTUAL TABLE IF NOT EXISTS %s_vec USING vec0(embedding float[%d])",
		s.collectionName, dims)
	if _, err := s.conn.ExecContext(ctx, ddl); err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 建 vec0 表失败: %s", err)),
			exception.WithCause(err),
		)
	}
	mapDDL := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s_vec_map (vec_rowid INTEGER PRIMARY KEY, id TEXT UNIQUE)",
		s.collectionName)
	if _, err := s.conn.ExecContext(ctx, mapDDL); err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 建 vec 映射表失败: %s", err)),
			exception.WithCause(err),
		)
	}
	s.dims = dims
	return nil
}

// checkVectorFieldLocked CheckVectorField 的锁内实现。
func (s *SQLiteVectorStore) checkVectorFieldLocked() error {
	if !s.vecAvailable {
		return nil
	}
	var vecSQL string
	err := s.conn.QueryRowContext(context.Background(),
		"SELECT sql FROM sqlite_master WHERE type='table' AND name=?",
		s.collectionName+"_vec").Scan(&vecSQL)
	if err == sql.ErrNoRows {
		return nil // 表未建：惰性建表前无实际配置可校验
	}
	return err
}

// addLocked Add 的锁内实现（三表写入：chunks UPSERT + FTS 先删后插 + vec0 有向量才写）。
func (s *SQLiteVectorStore) addLocked(ctx context.Context, data []map[string]any) error {
	for _, node := range data {
		nodeID, _ := node["id"].(string)
		if nodeID == "" {
			nodeID, _ = node["pk"].(string)
		}
		if nodeID == "" {
			nodeID = uuid.NewString()
		}

		text, _ := node[s.textField].(string)

		// metadata（map 或 JSON 字符串双形态，chroma addBatch 同款）
		metaJSON := "{}"
		if rawMeta, ok := node[s.metadataField]; ok {
			switch m := rawMeta.(type) {
			case map[string]any:
				b, err := json.Marshal(m)
				if err == nil {
					metaJSON = string(b)
				}
			case string:
				metaJSON = m
			}
		}
		if docID, ok := node[s.docIDField]; ok {
			metaJSON = appendMetaField(metaJSON, s.docIDField, fmt.Sprintf("%v", docID))
		}
		if chunkID, ok := node["chunk_id"]; ok {
			metaJSON = appendMetaField(metaJSON, "chunk_id", fmt.Sprintf("%v", chunkID))
		}
		docID, _ := node[s.docIDField].(string)
		chunkID, _ := node["chunk_id"].(string)

		// ① chunks 主表（UPSERT：同 id 覆写）
		upsert := fmt.Sprintf(
			"INSERT INTO %s_chunks (id, content, metadata, doc_id, chunk_id) VALUES (?,?,?,?,?) "+
				"ON CONFLICT(id) DO UPDATE SET content=excluded.content, metadata=excluded.metadata, "+
				"doc_id=excluded.doc_id, chunk_id=excluded.chunk_id",
			s.collectionName)
		if _, err := s.conn.ExecContext(ctx, upsert, nodeID, text, metaJSON, docID, chunkID); err != nil {
			return exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 写入 chunks 失败: %s", err)),
				exception.WithCause(err),
			)
		}

		// ② FTS5 表（先删后插保持 UPSERT 语义）
		if err := s.ensureFTSTable(ctx); err != nil {
			return err
		}
		if _, err := s.conn.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s_fts WHERE id = ?", s.collectionName), nodeID); err != nil {
			return exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 清理 FTS 旧记录失败: %s", err)),
				exception.WithCause(err),
			)
		}
		if _, err := s.conn.ExecContext(ctx,
			fmt.Sprintf("INSERT INTO %s_fts (content, id) VALUES (?, ?)", s.collectionName), text, nodeID); err != nil {
			return exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 写入 FTS 失败: %s", err)),
				exception.WithCause(err),
			)
		}

		// ③ vec0 表（有向量才写；无向量跳过——bm25-only 场景合法）
		embedding, _ := node[s.vectorFieldName].([]float64)
		if len(embedding) == 0 {
			logger.Warn(sqliteLogComponent).
				Str("node_id", nodeID).
				Msg("node has no embedding, skipping vector write")
			continue
		}
		if s.dims == 0 {
			if err := s.ensureVecTable(ctx, len(embedding)); err != nil {
				return err
			}
		}
		if len(embedding) != s.dims {
			return exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 向量维度不匹配：期望 %d，实际 %d", s.dims, len(embedding))),
			)
		}
		// 同 id 覆写：删旧 vec 行与映射
		var oldRowid sql.NullInt64
		err := s.conn.QueryRowContext(ctx,
			fmt.Sprintf("SELECT vec_rowid FROM %s_vec_map WHERE id = ?", s.collectionName), nodeID).Scan(&oldRowid)
		if err == nil && oldRowid.Valid {
			_, _ = s.conn.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s_vec WHERE rowid = ?", s.collectionName), oldRowid.Int64)
			_, _ = s.conn.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s_vec_map WHERE id = ?", s.collectionName), nodeID)
		}
		// vec0 写入：f32 blob + vec_f32()（harness manager.go:1271 同款形态）
		if _, err := s.conn.ExecContext(ctx,
			fmt.Sprintf("INSERT INTO %s_vec (rowid, embedding) VALUES (NULL, vec_f32(?))", s.collectionName),
			f32Blob(embedding)); err != nil {
			return exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 写入向量失败: %s", err)),
				exception.WithCause(err),
			)
		}
		var newRowid int64
		if err := s.conn.QueryRowContext(ctx, "SELECT last_insert_rowid()").Scan(&newRowid); err != nil {
			return exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 取 vec rowid 失败: %s", err)),
				exception.WithCause(err),
			)
		}
		if _, err := s.conn.ExecContext(ctx,
			fmt.Sprintf("INSERT INTO %s_vec_map (vec_rowid, id) VALUES (?, ?)", s.collectionName),
			newRowid, nodeID); err != nil {
			return exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 写入 vec 映射失败: %s", err)),
				exception.WithCause(err),
			)
		}
	}
	return nil
}

// searchLocked Search 的锁内实现：vec0 KNN → join 映射 → 度量换算（语义对齐 chroma）。
func (s *SQLiteVectorStore) searchLocked(ctx context.Context, queryVector []float64, topK int,
	filters query.QueryExpr,
) ([]retrievalcommon.SearchResult, error) {
	if !s.vecAvailable {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", "sqlite store: vec0 扩展不可用，向量搜索不可用"),
		)
	}
	if s.dims == 0 {
		return []retrievalcommon.SearchResult{}, nil // 空表：无向量可检
	}
	if len(queryVector) != s.dims {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 查询向量维度不匹配：期望 %d，实际 %d", s.dims, len(queryVector))),
		)
	}
	knn := fmt.Sprintf(
		"SELECT m.id, v.distance FROM %s_vec v JOIN %s_vec_map m ON m.vec_rowid = v.rowid "+
			"WHERE v.embedding MATCH ? AND k = ? ORDER BY distance LIMIT ?",
		s.collectionName, s.collectionName)
	rows, err := s.conn.QueryContext(ctx, knn, f32Blob(queryVector), topK, topK)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 向量搜索失败: %s", err)),
			exception.WithCause(err),
		)
	}
	defer rows.Close()

	var ids []string
	var dists []float64
	for rows.Next() {
		var id string
		var dist float64
		if err := rows.Scan(&id, &dist); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		dists = append(dists, dist)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.hydrateResults(ctx, ids, dists, filters, "vector")
}

// sparseSearchLocked SparseSearch 的锁内实现：FTS5 bm25 真打分。
// SQLite bm25() 越小越相关（负值），取负归一为越大越相关。
func (s *SQLiteVectorStore) sparseSearchLocked(ctx context.Context, queryText string, topK int,
	filters query.QueryExpr,
) ([]retrievalcommon.SearchResult, error) {
	if err := s.ensureFTSTable(ctx); err != nil {
		return nil, err
	}
	match := buildFTSMatchQuery(queryText)
	if match == "" {
		return []retrievalcommon.SearchResult{}, nil
	}
	q := fmt.Sprintf(
		"SELECT id, content, -bm25(%s_fts) AS score FROM %s_fts WHERE %s_fts MATCH ? "+
			"ORDER BY score LIMIT ?",
		s.collectionName, s.collectionName, s.collectionName)
	rows, err := s.conn.QueryContext(ctx, q, match, topK)
	if err != nil {
		// 对齐 chroma/Python：sparse 路失败 Warn 软失败返回空（不阻断 hybrid 另一路）
		logger.Warn(sqliteLogComponent).
			Str("method", "SparseSearch").
			Str("query_text", queryText).
			Err(err).
			Msg("text search failed")
		return nil, nil
	}
	defer rows.Close()

	var ids []string
	var scores []float64
	for rows.Next() {
		var id, content string
		var score float64
		if err := rows.Scan(&id, &content, &score); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		scores = append(scores, score)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.hydrateResults(ctx, ids, scores, filters, "sparse")
}

// hybridSearchLocked HybridSearch 的锁内实现（chroma HybridSearch 同款语义：两路 + RRF k=60）。
func (s *SQLiteVectorStore) hybridSearchLocked(ctx context.Context, queryText string, queryVector []float64,
	topK int, alpha float64, filters query.QueryExpr,
) ([]retrievalcommon.SearchResult, error) {
	// 单专用连接下顺序执行两路（连接非并发安全）；RRF 恒等权重与 chroma 一致，alpha 保留签名对齐
	_ = alpha

	var resultsList [][]retrievalcommon.RetrievalResult
	idMapping := make(map[string]string)

	if queryVector != nil && s.vecAvailable && s.dims > 0 {
		vecResults, err := s.searchLocked(ctx, queryVector, topK*2, filters)
		if err != nil {
			logger.Warn(sqliteLogComponent).Str("mode", "vector").Err(err).Msg("hybrid search partial path failed")
		} else if len(vecResults) > 0 {
			resultsList = append(resultsList, s.toRetrievalResults(vecResults, idMapping))
		}
	}

	textResults, err := s.sparseSearchLocked(ctx, queryText, topK*2, filters)
	if err != nil {
		logger.Warn(sqliteLogComponent).Str("mode", "text").Err(err).Msg("hybrid search partial path failed")
	} else if len(textResults) > 0 {
		resultsList = append(resultsList, s.toRetrievalResults(textResults, idMapping))
	}

	if len(resultsList) == 0 {
		return nil, nil
	}

	fused := utils.RRFFusion(resultsList, 60)
	var fusedResults []retrievalcommon.SearchResult
	for i, rr := range fused {
		if i >= topK {
			break
		}
		resultID, _ := rr.Metadata["id"].(string)
		if resultID == "" {
			resultID = idMapping[rr.Text]
		}
		if resultID == "" {
			// T-24 同款兜底：文本哈希保证 ID 恒非空
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
		fusedResults = append(fusedResults, retrievalcommon.SearchResult{
			ID:       resultID,
			Text:     rr.Text,
			Score:    rr.Score,
			Metadata: metadata,
		})
	}
	return fusedResults, nil
}

// deleteLocked Delete 的锁内实现（vec/vec_map/fts/chunks 四表连带清理）。
func (s *SQLiteVectorStore) deleteLocked(ctx context.Context, ids []string, filterExpr query.QueryExpr) (bool, error) {
	if len(ids) == 0 {
		if filterExpr != nil {
			logger.Warn(sqliteLogComponent).
				Str("method", "Delete").
				Msg("sqlite store: 非 id 过滤暂不支持，已忽略")
		}
		return false, nil
	}
	deleted := false
	for _, id := range ids {
		var oldRowid sql.NullInt64
		err := s.conn.QueryRowContext(ctx,
			fmt.Sprintf("SELECT vec_rowid FROM %s_vec_map WHERE id = ?", s.collectionName), id).Scan(&oldRowid)
		if err == nil && oldRowid.Valid {
			_, _ = s.conn.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s_vec WHERE rowid = ?", s.collectionName), oldRowid.Int64)
			_, _ = s.conn.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s_vec_map WHERE id = ?", s.collectionName), id)
		}
		r1, err := s.conn.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s_chunks WHERE id = ?", s.collectionName), id)
		if err != nil {
			return false, err
		}
		_, _ = s.conn.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s_fts WHERE id = ?", s.collectionName), id)
		if n, err := r1.RowsAffected(); err == nil && n > 0 {
			deleted = true
		}
	}
	return deleted, nil
}

// tableExistsLocked TableExists 的锁内实现。
func (s *SQLiteVectorStore) tableExistsLocked(ctx context.Context, tableName string) (bool, error) {
	var count int
	err := s.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table','view') AND name IN (?, ?, ?, ?)",
		tableName+"_chunks", tableName+"_fts", tableName+"_vec", tableName+"_vec_map").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// deleteTableLocked DeleteTable 的锁内实现。
func (s *SQLiteVectorStore) deleteTableLocked(ctx context.Context, tableName string) error {
	for _, suffix := range []string{"_vec", "_vec_map", "_fts", "_chunks"} {
		if _, err := s.conn.ExecContext(ctx,
			"DROP TABLE IF EXISTS "+tableName+suffix); err != nil {
			return exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("sqlite store: 删表失败 %s%s: %s", tableName, suffix, err)),
				exception.WithCause(err),
			)
		}
	}
	return nil
}

// hydrateResults 按 id 取回 chunks 行并组装 SearchResult（度量换算对齐 chroma）。
func (s *SQLiteVectorStore) hydrateResults(ctx context.Context, ids []string, rawScores []float64,
	filters query.QueryExpr, mode string,
) ([]retrievalcommon.SearchResult, error) {
	results := make([]retrievalcommon.SearchResult, 0, len(ids))
	for i, id := range ids {
		var content, metaJSON string
		err := s.conn.QueryRowContext(ctx,
			fmt.Sprintf("SELECT content, metadata FROM %s_chunks WHERE id = ?", s.collectionName), id).
			Scan(&content, &metaJSON)
		if err != nil {
			continue // vec/fts 有但 chunks 缺（异常态）：跳过
		}
		metadata := make(map[string]any)
		_ = json.Unmarshal([]byte(metaJSON), &metadata)
		// doc_id 键存在性语义对齐 chroma（M-20）：缺失显式 nil
		if _, ok := metadata["doc_id"]; !ok {
			if v, ok := metadata[s.docIDField]; ok {
				metadata["doc_id"] = v
				delete(metadata, s.docIDField)
			} else {
				metadata["doc_id"] = nil
			}
		}

		var finalScore float64
		if i < len(rawScores) {
			if mode == "vector" {
				switch s.distanceMetric {
				case "l2":
					finalScore = vector.ConvertL2Squared(rawScores[i], 4.0)
				default: // cosine
					finalScore = vector.ConvertCosineDistance(rawScores[i])
				}
			} else {
				finalScore = rawScores[i] // sparse：-bm25 已归一为越大越相关
			}
		}

		results = append(results, retrievalcommon.SearchResult{
			ID:       id,
			Text:     content,
			Score:    finalScore,
			Metadata: metadata,
		})
	}
	// 元数据过滤应用层兜底（EqMap 子集；与 chroma where 能力对齐的最小实现）
	if filters != nil {
		results = applyEqMapFilter(results, filters)
	}
	return results, nil
}

// toRetrievalResults SearchResult → RetrievalResult（chroma HybridSearch 同款转换）。
func (s *SQLiteVectorStore) toRetrievalResults(results []retrievalcommon.SearchResult,
	idMapping map[string]string,
) []retrievalcommon.RetrievalResult {
	var out []retrievalcommon.RetrievalResult
	for _, r := range results {
		idMapping[r.Text] = r.ID
		metadata := make(map[string]any)
		for k, v := range r.Metadata {
			metadata[k] = v
		}
		metadata["id"] = r.ID
		docID, _ := r.Metadata[s.docIDField].(string)
		chunkID, _ := r.Metadata["chunk_id"].(string)
		out = append(out, retrievalcommon.RetrievalResult{
			Text:     r.Text,
			Score:    r.Score,
			Metadata: metadata,
			DocID:    docID,
			ChunkID:  chunkID,
		})
	}
	return out
}

// ──────────────────────────── 包级助手 ────────────────────────────

// buildFTSMatchQuery 把自然语言 query 转成 FTS5 MATCH 语法（逐词引号包裹 OR 连接）。
// 词内双引号转义为双双引号，防 MATCH 语法注入。
func buildFTSMatchQuery(text string) string {
	fields := strings.Fields(text)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		tok := strings.ReplaceAll(f, "\"", "\"\"")
		quoted = append(quoted, "\""+tok+"\"")
	}
	return strings.Join(quoted, " OR ")
}

// appendMetaField 向 metadata JSON 追加字段。
func appendMetaField(metaJSON, key, val string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil {
		m = map[string]any{}
	}
	m[key] = val
	b, err := json.Marshal(m)
	if err != nil {
		return metaJSON
	}
	return string(b)
}

// f32Blob []float64 → f32 小端 blob（harness memory vectorToBlob 同款；
// vec0 以 blob 形态接收向量，经 vec_f32() 解释）。
func f32Blob(vec []float64) []byte {
	blob := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(float32(v))
		blob[i*4] = byte(bits)
		blob[i*4+1] = byte(bits >> 8)
		blob[i*4+2] = byte(bits >> 16)
		blob[i*4+3] = byte(bits >> 24)
	}
	return blob
}

// applyEqMapFilter EqMap 元数据过滤的应用层兜底：
// 走访 And 链提取全部 == 条件（query.EqMap 折叠产物），逐条比对 metadata。
func applyEqMapFilter(results []retrievalcommon.SearchResult, filters query.QueryExpr) []retrievalcommon.SearchResult {
	want := map[string]any{}
	extractEqConditions(filters, want)
	if len(want) == 0 {
		return results
	}
	out := make([]retrievalcommon.SearchResult, 0, len(results))
	for _, r := range results {
		match := true
		for k, v := range want {
			if r.Metadata[k] != v {
				match = false
				break
			}
		}
		if match {
			out = append(out, r)
		}
	}
	return out
}

// extractEqConditions 走访表达式树提取 == 条件（And 链）。
func extractEqConditions(expr query.QueryExpr, out map[string]any) {
	switch e := expr.(type) {
	case *query.ComparisonExpr:
		if e.Operator == "==" {
			out[e.Field] = e.Value
		}
	case *query.LogicalExpr:
		if e.Operator == "and" {
			extractEqConditions(e.Left, out)
			extractEqConditions(e.Right, out)
		}
	}
}
