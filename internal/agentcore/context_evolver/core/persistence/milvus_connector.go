package persistence

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// milvusClient Milvus 客户端操作接口（用于解耦和测试）。
//
// 生产代码使用真实 milvusclient.Client，测试代码注入 fakeMilvusClient。
type milvusClient interface {
	CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption) error
	HasCollection(ctx context.Context, option milvusclient.HasCollectionOption) (bool, error)
	DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption) (*entity.Collection, error)
	Insert(ctx context.Context, option milvusclient.InsertOption) (milvusclient.InsertResult, error)
	Search(ctx context.Context, option milvusclient.SearchOption) ([]milvusclient.ResultSet, error)
	Query(ctx context.Context, option milvusclient.QueryOption) (milvusclient.ResultSet, error)
	Delete(ctx context.Context, option milvusclient.DeleteOption) (milvusclient.DeleteResult, error)
	LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption) error
	Flush(ctx context.Context, option milvusclient.FlushOption) error
	CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption) error
	Close(ctx context.Context) error
}

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusConnectorImpl Milvus 向量数据库连接器实现。
//
// 独立实现（对齐 Python milvus_connector.py），不复用 foundation/store/vector/MilvusVectorStore。
// 固定 5 字段 schema：id(VARCHAR) / namespace(VARCHAR) / content(VARCHAR) / embedding(FLOAT_VECTOR) / metadata(JSON)。
// 命名空间分区：通过 namespace 字段过滤实现逻辑分区。
//
// 客户端惰性创建，初始化时不需要 Milvus 可用。
//
// Python: openjiuwen/extensions/context_evolver/core/db_connector/milvus_connector.py
type MilvusConnectorImpl struct {
	// host Milvus 服务主机
	host string
	// port Milvus gRPC 端口
	port int
	// collectionName Milvus 集合名
	collectionName string
	// dim 嵌入向量维度，0 = 自动检测
	dim int
	// alias 连接别名
	alias string
	// metricType 距离度量类型
	metricType string
	// client Milvus 客户端实例
	client milvusClient
	// mu 读写锁
	mu sync.RWMutex
	// createClient 客户端创建函数，用于依赖注入和测试
	createClient func(ctx context.Context, host string, port int, alias string) (milvusClient, error)
}

// MilvusConnectorOption 构造选项函数。
type MilvusConnectorOption func(*MilvusConnectorImpl)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// fieldID 主键字段名
	fieldID = "id"
	// fieldNS 命名空间字段名
	fieldNS = "namespace"
	// fieldContent 内容字段名
	fieldContent = "content"
	// fieldEmbedding 嵌入向量字段名
	fieldEmbedding = "embedding"
	// fieldMetadata 元数据字段名
	fieldMetadata = "metadata"

	// idMaxLen ID 字段最大长度
	idMaxLen = 256
	// nsMaxLen 命名空间字段最大长度
	nsMaxLen = 256
	// contentMaxLen 内容字段最大长度（Milvus VARCHAR 上限）
	contentMaxLen = 65535

	// milvusDefaultCollectionName 默认集合名
	milvusDefaultCollectionName = "vector_nodes"
	// milvusDefaultAlias 默认连接别名
	milvusDefaultAlias = "default"
	// milvusDefaultMetricType 默认距离度量
	milvusDefaultMetricType = "COSINE"
	// milvusProbeTimeout 探测超时
	milvusProbeTimeout = 5 * time.Second

	// milvusLogComponent 日志组件标识
	milvusLogComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusConnectorImpl 创建 Milvus 连接器实例。
// 客户端惰性创建，初始化时不需要 Milvus 可用。
//
// 对齐 Python: MilvusConnector(host, port, collection_name, dim, alias, metric_type)
func NewMilvusConnectorImpl(opts ...MilvusConnectorOption) *MilvusConnectorImpl {
	m := &MilvusConnectorImpl{
		collectionName: milvusDefaultCollectionName,
		alias:          milvusDefaultAlias,
		metricType:     milvusDefaultMetricType,
		createClient:   defaultMilvusCreateClient,
	}
	for _, opt := range opts {
		opt(m)
	}
	logger.Info(milvusLogComponent).
		Str("host", m.host).Int("port", m.port).
		Str("collection", m.collectionName).Int("dim", m.dim).
		Msg("MilvusConnectorImpl 初始化完成（客户端惰性创建）")
	return m
}

// SaveToDB 保存数据到 Milvus 命名空间。实现 persistence.MilvusConnector 接口。
//
// Upsert 语义：delete 已有 PK → insert → flush。
// 无 embedding 的节点被跳过（Milvus 要求非 null 向量）。
//
// 对齐 Python: MilvusConnector.save_to_db(namespace, data)
func (m *MilvusConnectorImpl) SaveToDB(ctx context.Context, namespace string, data map[string]any) error {
	if len(data) == 0 {
		logger.Info(milvusLogComponent).Msg("save_to_db: 空 data，跳过")
		return nil
	}

	// 自动检测嵌入维度（对齐 Python: 从首个有 embedding 的节点推断 dim）
	var dim int
	for _, nodeDataAny := range data {
		nd, ok := nodeDataAny.(map[string]any)
		if !ok {
			continue
		}
		if emb := extractEmbedding(nd["embedding"]); len(emb) > 0 {
			dim = len(emb)
			break
		}
	}

	// 如果未能推断 dim，使用构造时传入的 dim
	if dim <= 0 {
		dim = m.dim
	}

	if dim <= 0 {
		logger.Warn(milvusLogComponent).Msg("save_to_db: 无法确定嵌入维度，跳过保存")
		return nil
	}

	c, err := m.getClient(ctx, dim)
	if err != nil {
		return fmt.Errorf("save_to_db: 获取客户端失败: %w", err)
	}

	// 收集可插入的行
	var ids, namespaces, contents []string
	var embeddings [][]float32
	var metadatas []map[string]any
	skipped := 0

	for nodeID, nodeData := range data {
		nd, ok := nodeData.(map[string]any)
		if !ok {
			skipped++
			continue
		}
		emb := extractEmbedding(nd["embedding"])
		if emb == nil {
			skipped++
			continue
		}
		ids = append(ids, Truncate(nodeID, idMaxLen))
		namespaces = append(namespaces, Truncate(namespace, nsMaxLen))
		content, _ := nd["content"].(string)
		contents = append(contents, Truncate(content, contentMaxLen))
		embeddings = append(embeddings, emb)
		meta, _ := nd["metadata"].(map[string]any)
		if meta == nil {
			meta = map[string]any{}
		}
		metadatas = append(metadatas, meta)
	}

	if skipped > 0 {
		logger.Warn(milvusLogComponent).
			Int("skipped", skipped).Str("namespace", namespace).
			Msg("save_to_db: 跳过无 embedding 的节点")
	}

	if len(ids) == 0 {
		logger.Warn(milvusLogComponent).Msg("save_to_db: 无可嵌入节点，跳过保存")
		return nil
	}

	// Upsert: delete 已有 PK → insert
	deleteExpr := IDsExpr(ids)
	_, _ = c.Delete(ctx, milvusclient.NewDeleteOption(m.collectionName).WithExpr(deleteExpr))

	// 构造列格式数据
	idColData := make([]string, len(ids))
	nsColData := make([]string, len(ids))
	contentColData := make([]string, len(ids))
	embColData := make([][]float32, len(ids))
	metaColData := make([]map[string]any, len(ids))
	for i := range ids {
		idColData[i] = ids[i]
		nsColData[i] = namespaces[i]
		contentColData[i] = contents[i]
		embColData[i] = embeddings[i]
		metaColData[i] = metadatas[i]
	}

	insertColumns := []column.Column{
		column.NewColumnVarChar(fieldID, idColData),
		column.NewColumnVarChar(fieldNS, nsColData),
		column.NewColumnVarChar(fieldContent, contentColData),
		column.NewColumnFloatVector(fieldEmbedding, dim, embColData),
		// metadata (JSON) 字段通过 JSONBytes 列插入
		column.NewColumnJSONBytes(fieldMetadata, mapsToJSONBytes(metaColData)),
	}

	insertOpt := milvusclient.NewColumnBasedInsertOption(m.collectionName, insertColumns...)

	if _, err := c.Insert(ctx, insertOpt); err != nil {
		logger.Error(milvusLogComponent).Err(err).Str("namespace", namespace).Msg("save_to_db: 插入失败")
		return fmt.Errorf("save_to_db: 插入失败: %w", err)
	}

	// Flush
	_ = c.Flush(ctx, milvusclient.NewFlushOption(m.collectionName))

	logger.Info(milvusLogComponent).
		Int("count", len(ids)).Str("namespace", namespace).Str("collection", m.collectionName).
		Msg("save_to_db: 保存节点完成")

	return nil
}

// LoadFromDB 从 Milvus 命名空间加载所有节点。实现 persistence.MilvusConnector 接口。
//
// 对齐 Python: MilvusConnector.load_from_db(namespace)
func (m *MilvusConnectorImpl) LoadFromDB(ctx context.Context, namespace string) (map[string]any, error) {
	c, err := m.getClient(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("load_from_db: 获取客户端失败: %w", err)
	}

	expr := fmt.Sprintf(`%s == "%s"`, fieldNS, namespace)
	resultSet, err := c.Query(ctx, milvusclient.NewQueryOption(m.collectionName).
		WithFilter(expr).
		WithOutputFields(fieldID, fieldNS, fieldContent, fieldEmbedding, fieldMetadata))
	if err != nil {
		logger.Error(milvusLogComponent).Err(err).Str("namespace", namespace).Msg("load_from_db: 查询失败")
		return nil, fmt.Errorf("load_from_db: 查询失败: %w", err)
	}

	data := make(map[string]any)
	idCol := findColumn(resultSet.Fields, fieldID)
	contentCol := findColumn(resultSet.Fields, fieldContent)
	embCol := findColumn(resultSet.Fields, fieldEmbedding)
	metaCol := findColumn(resultSet.Fields, fieldMetadata)

	for i := 0; i < resultSet.ResultCount; i++ {
		nodeID := getColumnString(idCol, i)
		data[nodeID] = map[string]any{
			"id":        nodeID,
			"content":   getColumnString(contentCol, i),
			"embedding": getColumnAny(embCol, i),
			"metadata":  getColumnAny(metaCol, i),
		}
	}

	logger.Info(milvusLogComponent).
		Int("count", len(data)).Str("namespace", namespace).
		Msg("load_from_db: 加载节点完成")

	return data, nil
}

// Exists 检查命名空间是否有数据。实现 persistence.MilvusConnector 接口。
//
// 对齐 Python: MilvusConnector.exists(namespace)
func (m *MilvusConnectorImpl) Exists(ctx context.Context, namespace string) bool {
	c, err := m.getClient(ctx, 0)
	if err != nil {
		return false
	}

	expr := fmt.Sprintf(`%s == "%s"`, fieldNS, namespace)
	resultSet, err := c.Query(ctx, milvusclient.NewQueryOption(m.collectionName).
		WithFilter(expr).
		WithOutputFields(fieldID).
		WithLimit(1))
	if err != nil {
		logger.Error(milvusLogComponent).Err(err).Str("namespace", namespace).Msg("exists: 查询失败")
		return false
	}

	return resultSet.ResultCount > 0
}

// Delete 删除命名空间的所有数据。实现 persistence.MilvusConnector 接口。
//
// 对齐 Python: MilvusConnector.delete(namespace)
func (m *MilvusConnectorImpl) Delete(ctx context.Context, namespace string) bool {
	c, err := m.getClient(ctx, 0)
	if err != nil {
		return false
	}

	expr := fmt.Sprintf(`%s == "%s"`, fieldNS, namespace)
	resultSet, err := c.Query(ctx, milvusclient.NewQueryOption(m.collectionName).
		WithFilter(expr).
		WithOutputFields(fieldID))
	if err != nil {
		logger.Error(milvusLogComponent).Err(err).Str("namespace", namespace).Msg("delete: 查询失败")
		return false
	}

	if resultSet.ResultCount == 0 {
		logger.Info(milvusLogComponent).Str("namespace", namespace).Msg("delete: 命名空间为空")
		return false
	}

	ids := make([]string, 0, resultSet.ResultCount)
	idCol := findColumn(resultSet.Fields, fieldID)
	for i := 0; i < resultSet.ResultCount; i++ {
		if id := getColumnString(idCol, i); id != "" {
			ids = append(ids, id)
		}
	}

	if len(ids) > 0 {
		_, err = c.Delete(ctx, milvusclient.NewDeleteOption(m.collectionName).WithExpr(IDsExpr(ids)))
		if err != nil {
			logger.Error(milvusLogComponent).Err(err).Str("namespace", namespace).Msg("delete: 删除失败")
			return false
		}
		_ = c.Flush(ctx, milvusclient.NewFlushOption(m.collectionName))
	}

	logger.Info(milvusLogComponent).
		Int("count", len(ids)).Str("namespace", namespace).
		Msg("delete: 删除节点完成")
	return true
}

// Search 在命名空间内执行 ANN 搜索。
//
// 对齐 Python: MilvusConnector.search(namespace, embedding, top_k, metric)
func (m *MilvusConnectorImpl) Search(ctx context.Context, namespace string, embedding []float32, topK int, metric string) ([]map[string]any, error) {
	if topK <= 0 {
		topK = 10
	}

	c, err := m.getClient(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("search: 获取客户端失败: %w", err)
	}

	// metric 回退到 index 的 metricType
	milvusMetric := strings.ToUpper(m.metricType)
	requestedMetric := strings.ToUpper(metric)
	// 映射简写
	switch requestedMetric {
	case "COSINE", "L2", "IP":
		// 保持
	default:
		if requestedMetric == "INNER_PRODUCT" {
			requestedMetric = "IP"
		} else {
			requestedMetric = "COSINE"
		}
	}
	if requestedMetric != milvusMetric {
		logger.Warn(milvusLogComponent).
			Str("requested", requestedMetric).Str("index_metric", milvusMetric).
			Msg("search: 请求 metric 与索引不一致，回退到索引 metric")
	}

	vectors := []entity.Vector{entity.FloatVector(embedding)}
	expr := fmt.Sprintf(`%s == "%s"`, fieldNS, namespace)

	searchOpt := milvusclient.NewSearchOption(m.collectionName, topK, vectors).
		WithANNSField(fieldEmbedding).
		WithFilter(expr).
		WithOutputFields(fieldID, fieldContent, fieldMetadata)

	resultSets, err := c.Search(ctx, searchOpt)
	if err != nil {
		logger.Error(milvusLogComponent).Err(err).Str("namespace", namespace).Msg("search: 搜索失败")
		return nil, fmt.Errorf("search: 搜索失败: %w", err)
	}

	var results []map[string]any
	for _, rs := range resultSets {
		idCol := findColumn(rs.Fields, fieldID)
		contentCol := findColumn(rs.Fields, fieldContent)
		metaCol := findColumn(rs.Fields, fieldMetadata)

		for j := 0; j < rs.ResultCount; j++ {
			entry := map[string]any{
				"id":        getColumnString(idCol, j),
				"content":   getColumnString(contentCol, j),
				"embedding": nil, // 搜索结果不返回向量
				"metadata":  getColumnAny(metaCol, j),
				"score":     float64(rs.Scores[j]),
			}
			results = append(results, entry)
		}
	}

	logger.Debug(milvusLogComponent).
		Int("count", len(results)).Str("namespace", namespace).
		Msg("search: 搜索完成")

	return results, nil
}

// DeleteNodes 按 ID 删除指定节点。
//
// 对齐 Python: MilvusConnector.delete_nodes(namespace, node_ids)
func (m *MilvusConnectorImpl) DeleteNodes(ctx context.Context, namespace string, nodeIDs []string) bool {
	if len(nodeIDs) == 0 {
		return true
	}

	c, err := m.getClient(ctx, 0)
	if err != nil {
		return false
	}

	_, err = c.Delete(ctx, milvusclient.NewDeleteOption(m.collectionName).WithExpr(IDsExpr(nodeIDs)))
	if err != nil {
		logger.Error(milvusLogComponent).Err(err).Str("namespace", namespace).Msg("delete_nodes: 删除失败")
		return false
	}

	_ = c.Flush(ctx, milvusclient.NewFlushOption(m.collectionName))

	logger.Info(milvusLogComponent).
		Int("count", len(nodeIDs)).Str("namespace", namespace).
		Msg("delete_nodes: 删除节点完成")
	return true
}

// ListNamespaces 列出所有命名空间。
//
// 对齐 Python: MilvusConnector.list_namespaces()
func (m *MilvusConnectorImpl) ListNamespaces(ctx context.Context) []string {
	c, err := m.getClient(ctx, 0)
	if err != nil {
		return nil
	}

	resultSet, err := c.Query(ctx, milvusclient.NewQueryOption(m.collectionName).
		WithFilter(fmt.Sprintf(`%s != ""`, fieldID)).
		WithOutputFields(fieldNS))
	if err != nil {
		logger.Error(milvusLogComponent).Err(err).Msg("list_namespaces: 查询失败")
		return nil
	}

	nsSet := make(map[string]bool)
	nsCol := findColumn(resultSet.Fields, fieldNS)
	for i := 0; i < resultSet.ResultCount; i++ {
		if ns := getColumnString(nsCol, i); ns != "" {
			nsSet[ns] = true
		}
	}

	namespaces := make([]string, 0, len(nsSet))
	for ns := range nsSet {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

// Count 统计节点数量。
//
// 对齐 Python: MilvusConnector.count(namespace)
func (m *MilvusConnectorImpl) Count(ctx context.Context, namespace string) int {
	c, err := m.getClient(ctx, 0)
	if err != nil {
		return 0
	}

	if namespace != "" {
		expr := fmt.Sprintf(`%s == "%s"`, fieldNS, namespace)
		resultSet, err := c.Query(ctx, milvusclient.NewQueryOption(m.collectionName).
			WithFilter(expr).
			WithOutputFields(fieldID))
		if err != nil {
			logger.Error(milvusLogComponent).Err(err).Msg("count: 查询失败")
			return 0
		}
		return resultSet.ResultCount
	}

	// 全局计数
	resultSet, err := c.Query(ctx, milvusclient.NewQueryOption(m.collectionName).
		WithFilter(fmt.Sprintf(`%s != ""`, fieldID)).
		WithOutputFields(fieldID))
	if err != nil {
		logger.Error(milvusLogComponent).Err(err).Msg("count: 全局查询失败")
		return 0
	}
	return resultSet.ResultCount
}

// Flush 刷写缓冲区。
//
// 对齐 Python: MilvusConnector.flush()
func (m *MilvusConnectorImpl) Flush(ctx context.Context) {
	m.mu.RLock()
	c := m.client
	m.mu.RUnlock()

	if c != nil {
		_ = c.Flush(ctx, milvusclient.NewFlushOption(m.collectionName))
		logger.Debug(milvusLogComponent).Str("collection", m.collectionName).Msg("Flush 完成")
	}
}

// Close 关闭连接。
//
// 对齐 Python: MilvusConnector.close()
func (m *MilvusConnectorImpl) Close(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil {
		_ = m.client.Close(ctx)
		m.client = nil
		logger.Info(milvusLogComponent).
			Str("host", m.host).Int("port", m.port).
			Msg("MilvusConnectorImpl 连接已关闭")
	}
}

// SetClient 注入客户端（用于单元测试）。
//
// 对齐 Python: MilvusConnector.set_collection(collection)
func (m *MilvusConnectorImpl) SetClient(client milvusClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.client = client
}

// Host 返回配置的主机。
func (m *MilvusConnectorImpl) Host() string { return m.host }

// Port 返回配置的端口。
func (m *MilvusConnectorImpl) Port() int { return m.port }

// CollectionName 返回集合名。
func (m *MilvusConnectorImpl) CollectionName() string { return m.collectionName }

// Dim 返回配置的维度。
func (m *MilvusConnectorImpl) Dim() int { return m.dim }

// ProbeReachable 探测 Milvus 是否可达（用于 auto 模式）。
func (m *MilvusConnectorImpl) ProbeReachable(ctx context.Context) bool {
	c, err := m.getClient(ctx, 0)
	if err != nil {
		return false
	}
	// 轻量操作：检查集合是否存在
	_, err = c.HasCollection(ctx, milvusclient.NewHasCollectionOption(m.collectionName))
	return err == nil
}

// Truncate UTF-8 安全截断。对齐 Python: MilvusConnector.truncate(text, max_bytes)。
func Truncate(text string, maxBytes int) string {
	encoded := []byte(text)
	if len(encoded) <= maxBytes {
		return text
	}
	// 向后找到完整的 UTF-8 边界
	truncated := encoded[:maxBytes]
	for i := len(truncated) - 1; i >= 0; i-- {
		if truncated[i] < 0x80 {
			// ASCII 字符，完整
			break
		}
		// 检查是否为 UTF-8 起始字节（高两位为 11）
		if truncated[i]&0xC0 == 0xC0 {
			// 计算该字符应有的字节数
			expectedLen := 1
			b := truncated[i]
			for b&0x40 != 0 {
				expectedLen++
				b <<= 1
			}
			if expectedLen > len(truncated)-i {
				// 字符被截断，丢弃
				truncated = truncated[:i]
			}
			break
		}
	}
	return string(truncated)
}

// IDsExpr 构建 Milvus id in [...] 表达式。对齐 Python: MilvusConnector.ids_expr(ids)。
func IDsExpr(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf(`"%s"`, id)
	}
	return fmt.Sprintf(`%s in [%s]`, fieldID, strings.Join(quoted, ", "))
}

// WithConnectorHost 设置 Milvus 主机。
func WithConnectorHost(host string) MilvusConnectorOption {
	return func(m *MilvusConnectorImpl) { m.host = host }
}

// WithConnectorPort 设置 Milvus 端口。
func WithConnectorPort(port int) MilvusConnectorOption {
	return func(m *MilvusConnectorImpl) { m.port = port }
}

// WithConnectorCollection 设置集合名。
func WithConnectorCollection(name string) MilvusConnectorOption {
	return func(m *MilvusConnectorImpl) { m.collectionName = name }
}

// WithConnectorDim 设置向量维度。
func WithConnectorDim(dim int) MilvusConnectorOption {
	return func(m *MilvusConnectorImpl) { m.dim = dim }
}

// WithConnectorMetricType 设置距离度量。
func WithConnectorMetricType(mt string) MilvusConnectorOption {
	return func(m *MilvusConnectorImpl) { m.metricType = mt }
}

// WithConnectorCreateClient 设置客户端创建函数（测试用）。
func WithConnectorCreateClient(fn func(ctx context.Context, host string, port int, alias string) (milvusClient, error)) MilvusConnectorOption {
	return func(m *MilvusConnectorImpl) { m.createClient = fn }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient 惰性获取或创建 Milvus 客户端。
// 对齐 Python: MilvusConnector._get_collection(dim)
func (m *MilvusConnectorImpl) getClient(ctx context.Context, dim int) (milvusClient, error) {
	m.mu.RLock()
	if m.client != nil {
		m.mu.RUnlock()
		return m.client, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	// 双重检查
	if m.client != nil {
		return m.client, nil
	}

	// 连接 Milvus
	c, err := m.createClient(ctx, m.host, m.port, m.alias)
	if err != nil {
		logger.Error(milvusLogComponent).Err(err).
			Str("host", m.host).Int("port", m.port).
			Msg("连接 Milvus 失败")
		return nil, fmt.Errorf("连接 Milvus 失败: %w", err)
	}

	m.client = c

	// 如果 dim > 0，初始化 collection
	if dim > 0 || m.dim > 0 {
		effectiveDim := dim
		if effectiveDim <= 0 {
			effectiveDim = m.dim
		}
		if err := m.initCollection(ctx, effectiveDim); err != nil {
			// 初始化失败不阻断客户端，后续操作会再尝试
			logger.Warn(milvusLogComponent).Err(err).Int("dim", effectiveDim).Msg("初始化集合失败，非致命")
		}
	}

	logger.Info(milvusLogComponent).
		Str("host", m.host).Int("port", m.port).
		Msg("连接 Milvus 成功")

	return m.client, nil
}

// initCollection 创建或复用 Milvus 集合。
// 对齐 Python: MilvusConnector._init_collection(dim)
func (m *MilvusConnectorImpl) initCollection(ctx context.Context, dim int) error {
	c := m.client
	if c == nil {
		return fmt.Errorf("客户端未初始化")
	}

	// 检查集合是否已存在
	has, err := c.HasCollection(ctx, milvusclient.NewHasCollectionOption(m.collectionName))
	if err != nil {
		return fmt.Errorf("检查集合失败: %w", err)
	}

	if has {
		logger.Info(milvusLogComponent).Str("collection", m.collectionName).Msg("集合已存在，复用")
		// 加载集合
		_ = c.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(m.collectionName))
		return nil
	}

	// 创建集合：固定 5 字段 schema
	milvusSchema := entity.NewSchema().WithName(m.collectionName).
		WithDescription("VectorNode storage for context evolver")

	fields := []*entity.Field{
		entity.NewField().WithName(fieldID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(int64(idMaxLen)).WithIsPrimaryKey(true),
		entity.NewField().WithName(fieldNS).WithDataType(entity.FieldTypeVarChar).WithMaxLength(int64(nsMaxLen)),
		entity.NewField().WithName(fieldContent).WithDataType(entity.FieldTypeVarChar).WithMaxLength(int64(contentMaxLen)),
		entity.NewField().WithName(fieldEmbedding).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(dim)),
		entity.NewField().WithName(fieldMetadata).WithDataType(entity.FieldTypeJSON),
	}

	for _, f := range fields {
		milvusSchema = milvusSchema.WithField(f)
	}

	// 创建索引
	metricType := mapMetricType(m.metricType)
	hnswIndex := index.NewHNSWIndex(metricType, 16, 64)

	createOpt := milvusclient.NewCreateCollectionOption(m.collectionName, milvusSchema).
		WithIndexOptions(milvusclient.NewCreateIndexOption(m.collectionName, fieldEmbedding, hnswIndex))

	if err := c.CreateCollection(ctx, createOpt); err != nil {
		return fmt.Errorf("创建集合失败: %w", err)
	}

	// 为 namespace 字段创建 INVERTED 索引（加速按 namespace 过滤）
	invertedIdx := index.NewInvertedIndex()
	_ = c.CreateIndex(ctx, milvusclient.NewCreateIndexOption(m.collectionName, fieldNS, invertedIdx))

	// 加载集合
	_ = c.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(m.collectionName))

	m.dim = dim
	logger.Info(milvusLogComponent).
		Str("collection", m.collectionName).Int("dim", dim).
		Msg("创建集合并加载完成")

	return nil
}

// defaultMilvusCreateClient 默认客户端创建函数。
func defaultMilvusCreateClient(ctx context.Context, host string, port int, alias string) (milvusClient, error) {
	uri := fmt.Sprintf("%s:%d", host, port)
	c, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: uri,
	})
	if err != nil {
		return nil, err
	}
	return &persistenceClientAdapter{client: c}, nil
}

// mapMetricType 将字符串映射为 entity.MetricType。
func mapMetricType(metric string) entity.MetricType {
	switch strings.ToUpper(metric) {
	case "L2":
		return entity.L2
	case "IP":
		return entity.IP
	case "COSINE":
		return entity.COSINE
	default:
		return entity.COSINE
	}
}

// extractEmbedding 从 any 类型中提取 float32 切片。
func extractEmbedding(v any) []float32 {
	if v == nil {
		return nil
	}
	if f32, ok := v.([]float32); ok {
		return f32
	}
	if f64, ok := v.([]float64); ok {
		result := make([]float32, len(f64))
		for i, f := range f64 {
			result[i] = float32(f)
		}
		return result
	}
	return nil
}

// getColumnString 从 column.Column 中提取字符串值。
func getColumnString(col column.Column, idx int) string {
	if col == nil || idx >= col.Len() {
		return ""
	}
	val, err := col.Get(idx)
	if err != nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

// getColumnAny 从 column.Column 中提取任意值。
func getColumnAny(col column.Column, idx int) any {
	if col == nil || idx >= col.Len() {
		return nil
	}
	val, err := col.Get(idx)
	if err != nil {
		return nil
	}
	return val
}

// mapsToJSONBytes 将 map 列表序列化为 JSON 字节切片列表。
func mapsToJSONBytes(maps []map[string]any) [][]byte {
	result := make([][]byte, len(maps))
	for i, m := range maps {
		b, err := json.Marshal(m)
		if err != nil {
			logger.Warn(milvusLogComponent).Err(err).Int("index", i).Msg("序列化 metadata 失败，使用空 JSON")
			b = []byte("{}")
		}
		result[i] = b
	}
	return result
}

// findColumn 在 DataSet（[]column.Column）中按名称查找列。
func findColumn(ds milvusclient.DataSet, name string) column.Column {
	for _, col := range ds {
		if col.Name() == name {
			return col
		}
	}
	return nil
}

// ──────────────────────────── 适配器 ────────────────────────────

// persistenceClientAdapter 将 milvusclient.Client 适配到 milvusClient 接口。
//
// 桥接真实 SDK 的 Client 方法签名到内部 milvusClient 接口。
// 对齐 foundation/store/vector/milvus_adapter.go 的 milvusClientAdapter。
type persistenceClientAdapter struct {
	client *milvusclient.Client
}

func (a *persistenceClientAdapter) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption) error {
	return a.client.CreateCollection(ctx, option)
}

func (a *persistenceClientAdapter) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption) (bool, error) {
	return a.client.HasCollection(ctx, option)
}

func (a *persistenceClientAdapter) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption) (*entity.Collection, error) {
	return a.client.DescribeCollection(ctx, option)
}

func (a *persistenceClientAdapter) Insert(ctx context.Context, option milvusclient.InsertOption) (milvusclient.InsertResult, error) {
	return a.client.Insert(ctx, option)
}

func (a *persistenceClientAdapter) Search(ctx context.Context, option milvusclient.SearchOption) ([]milvusclient.ResultSet, error) {
	return a.client.Search(ctx, option)
}

func (a *persistenceClientAdapter) Query(ctx context.Context, option milvusclient.QueryOption) (milvusclient.ResultSet, error) {
	return a.client.Query(ctx, option)
}

func (a *persistenceClientAdapter) Delete(ctx context.Context, option milvusclient.DeleteOption) (milvusclient.DeleteResult, error) {
	return a.client.Delete(ctx, option)
}

func (a *persistenceClientAdapter) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption) error {
	task, err := a.client.LoadCollection(ctx, option)
	if err != nil {
		return err
	}
	return task.Await(ctx)
}

func (a *persistenceClientAdapter) Flush(ctx context.Context, option milvusclient.FlushOption) error {
	task, err := a.client.Flush(ctx, option)
	if err != nil {
		return err
	}
	if task != nil {
		return task.Await(ctx)
	}
	return nil
}

func (a *persistenceClientAdapter) CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption) error {
	task, err := a.client.CreateIndex(ctx, option)
	if err != nil {
		return err
	}
	if task != nil {
		return task.Await(ctx)
	}
	return nil
}

func (a *persistenceClientAdapter) Close(ctx context.Context) error {
	return a.client.Close(ctx)
}
