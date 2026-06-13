package milvus

import (
	"context"
	"fmt"
	"strings"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// graphSearcher 图存储搜索器，负责混合搜索、BFS 扩展和重排序。
//
// 对应 Python: MilvusGraphStore.search / search_all / _search_single / _raw_hybrid_search
type graphSearcher struct {
	// client Milvus 客户端
	client milvusClient
	// embedder 嵌入模型（用于查询向量）
	embedder embedding.BaseEmbedding
	// indexCfg 索引配置
	indexCfg *graph.GraphStoreIndexConfig
	// registry 排序策略注册表
	registry *graph.RankerRegistry
	// metric 距离度量方式
	metric string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultSearchK 默认搜索返回数量
	defaultSearchK = 5
	// defaultBFSDepth 默认 BFS 扩展深度
	defaultBFSDepth = 0
	// defaultBFSK 默认 BFS 每轮扩展数量
	defaultBFSK = 5
	// defaultMinScore 默认最低相似度分数
	defaultMinScore = 0.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// newGraphSearcher 创建图存储搜索器
func newGraphSearcher(client milvusClient, embedder embedding.BaseEmbedding, indexCfg *graph.GraphStoreIndexConfig, registry *graph.RankerRegistry, metric string) *graphSearcher {
	return &graphSearcher{
		client:   client,
		embedder: embedder,
		indexCfg: indexCfg,
		registry: registry,
		metric:   metric,
	}
}

// search 混合搜索入口。根据 collection 选项决定搜索模式：
// - "all"：并发搜索三集合 + combinedRerank
// - 单集合名：searchSingle（可选 BFS）
//
// 对应 Python: MilvusGraphStore.search
func (s *graphSearcher) search(ctx context.Context, query string, opts ...graph.Option) (map[string][]map[string]any, error) {
	o := applyGraphOptions(opts...)

	if o.Collection == graph.AllCollections || o.Collection == "" {
		return s.searchAll(ctx, query, o)
	}
	result, err := s.searchSingle(ctx, query, o.Collection, o)
	if err != nil {
		return nil, err
	}
	return map[string][]map[string]any{o.Collection: result}, nil
}

// searchAll 并发搜索三集合，然后合并结果。
//
// 对应 Python: MilvusGraphStore._search_all
func (s *graphSearcher) searchAll(ctx context.Context, query string, o graph.Options) (map[string][]map[string]any, error) {
	collections := []string{CollectionEntity, CollectionRelation, CollectionEpisode}
	results := make(map[string][]map[string]any)

	// 逐个搜索（Go 的 goroutine 需要 errgroup 等额外依赖，这里简化为串行）
	for _, coll := range collections {
		result, err := s.searchSingle(ctx, query, coll, o)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("collection", coll).Msg("搜索集合失败")
			continue
		}
		results[coll] = result
	}

	// combinedRerank（如果配置了 reranker）
	if o.Reranker != nil && len(results) > 0 {
		return s.combinedRerank(ctx, query, results, o)
	}

	return results, nil
}

// searchSingle 搜索单个集合，可选 BFS 扩展。
//
// 对应 Python: MilvusGraphStore._search_single
func (s *graphSearcher) searchSingle(ctx context.Context, query, collection string, o graph.Options) ([]map[string]any, error) {
	k := o.K
	if k <= 0 {
		k = defaultSearchK
	}
	bfsDepth := o.BFSDepth
	bfsK := o.BFSK
	if bfsK <= 0 {
		bfsK = defaultBFSK
	}

	// 无 BFS：直接搜索
	if bfsDepth <= 0 {
		return s.rawHybridSearch(ctx, query, collection, k, o)
	}

	// 有 BFS：
	// 1. 第1轮搜索获取初始 UUID
	firstResults, err := s.rawHybridSearch(ctx, query, collection, bfsK, o)
	if err != nil {
		return nil, err
	}

	uuidSet := extractUUIDs(firstResults)

	// 2. BFS 扩展循环
	for i := 0; i < bfsDepth; i++ {
		var expanded map[string]struct{}
		if collection == CollectionEntity {
			expanded, err = s.expandEntities(ctx, uuidSet)
		} else {
			expanded, err = s.expandRelations(ctx, uuidSet)
		}
		if err != nil {
			logger.Warn(logComponent).Err(err).Int("bfs_round", i+1).Msg("BFS 扩展失败")
			break
		}
		for uuid := range expanded {
			uuidSet[uuid] = struct{}{}
		}
	}

	// 3. 最终搜索（带扩展后的 UUID 过滤）
	if len(uuidSet) > 0 {
		uuids := uuidSetToSlice(uuidSet)
		bfsOpts := o
		bfsOpts.IDs = stringsToAny(uuids)
		return s.rawHybridSearch(ctx, query, collection, k, bfsOpts)
	}

	return firstResults, nil
}

// rawHybridSearch 原始混合搜索（3通道：name_embedding + content_embedding + content_bm25）。
//
// 对应 Python: MilvusGraphStore._raw_hybrid_search
func (s *graphSearcher) rawHybridSearch(ctx context.Context, query, collection string, k int, o graph.Options) ([]map[string]any, error) {
	// 获取查询向量
	queryEmb, err := s.queryEmbedding(ctx, query, o)
	if err != nil {
		return nil, fmt.Errorf("获取查询向量失败: %w", err)
	}

	// 构建过滤表达式
	expr := ""
	if len(o.IDs) > 0 {
		ids := make([]string, 0, len(o.IDs))
		for _, id := range o.IDs {
			ids = append(ids, fmt.Sprintf("%v", id))
		}
		expr = buildIDFilterExpr(ids)
	} else if o.FilterExpr != nil {
		exprVal, exprErr := o.FilterExpr.ToExpr("milvus")
		if exprErr != nil {
			return nil, fmt.Errorf("构建过滤表达式失败: %w", exprErr)
		}
		strExpr, ok := exprVal.(string)
		if !ok {
			return nil, fmt.Errorf("Milvus 后端应返回 string 类型的表达式")
		}
		expr = strExpr
	}

	// 构建输出字段
	outputFields := o.OutputFields
	if len(outputFields) == 0 {
		outputFields = []string{"uuid", "content", "obj_type", "user_id"}
		if collection == CollectionEntity {
			outputFields = append(outputFields, "name", "relations", "episodes")
		} else if collection == CollectionRelation {
			outputFields = append(outputFields, "name", "lhs", "rhs", "valid_since", "valid_until")
		} else if collection == CollectionEpisode {
			outputFields = append(outputFields, "entities", "valid_since")
		}
	}

	// 构建 3 路 AnnRequest
	var annRequests []*milvusclient.AnnRequest

	// 通道1: content_embedding (dense)
	vecFloat32 := make([]float32, len(queryEmb))
	for i, v := range queryEmb {
		vecFloat32[i] = float32(v)
	}
	vectors := []entity.Vector{entity.FloatVector(vecFloat32)}
	contentReq := milvusclient.NewAnnRequest("content_embedding", k, vectors...)
	if expr != "" {
		contentReq = contentReq.WithFilter(expr)
	}
	annRequests = append(annRequests, contentReq)

	// 通道2: name_embedding (dense, 仅 Entity 集合)
	if collection == CollectionEntity {
		nameReq := milvusclient.NewAnnRequest("name_embedding", k, vectors...)
		if expr != "" {
			nameReq = nameReq.WithFilter(expr)
		}
		annRequests = append(annRequests, nameReq)
	}

	// 通道3: content_bm25 (sparse, 使用查询文本)
	// BM25 搜索：使用查询文本作为稀疏向量输入
	sparseReq := milvusclient.NewAnnRequest("content_bm25", k, vectors...)
	if expr != "" {
		sparseReq = sparseReq.WithFilter(expr)
	}
	annRequests = append(annRequests, sparseReq)

	// 构建 Reranker
	reranker, err := s.buildReranker(o.RankerConfig, collection, len(annRequests))
	if err != nil {
		return nil, fmt.Errorf("构建 Reranker 失败: %w", err)
	}

	// 构建 HybridSearch 选项
	searchOpt := milvusclient.NewHybridSearchOption(collection, k, annRequests...).
		WithOutputFields(outputFields...)
	if reranker != nil {
		searchOpt = searchOpt.WithReranker(reranker)
	}

	// 执行搜索
	resultSets, err := s.client.HybridSearch(ctx, searchOpt)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection", collection).Msg("混合搜索失败")
		return nil, fmt.Errorf("搜索集合 %s 失败: %w", collection, err)
	}

	// 解析结果
	return parseResultSets(resultSets), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// queryEmbedding 获取查询向量。如果 opts 中已提供则直接使用，否则调用 embedder。
func (s *graphSearcher) queryEmbedding(ctx context.Context, query string, o graph.Options) ([]float64, error) {
	if len(o.QueryEmbedding) > 0 {
		return o.QueryEmbedding, nil
	}
	if s.embedder == nil {
		return nil, fmt.Errorf("未绑定嵌入模型且未提供查询向量")
	}
	return s.embedder.EmbedQuery(ctx, query)
}

// buildReranker 根据排序配置构建 Reranker。
func (s *graphSearcher) buildReranker(config graph.BaseRankConfig, collection string, numChannels int) (milvusclient.Reranker, error) {
	if config == nil {
		// 默认使用 RRF
		return milvusclient.NewRRFReranker(), nil
	}

	switch c := config.(type) {
	case *graph.WeightedRankConfig:
		// 根据 IsActive 和集合类型构建权重
		isActive := c.IsActive()
		var weights []float64
		if collection == CollectionEntity {
			// Entity: 3 通道（name_dense + content_dense + content_sparse）
			weights = []float64{c.NameDense, c.ContentDense, c.ContentSparse}
			// 归一化
			total := 0.0
			activeCount := 0
			for i, w := range weights {
				if isActive[i] > 0 {
					total += w
					activeCount++
				}
			}
			if total == 0 || activeCount != numChannels {
				weights = autoBalanceWeights(numChannels)
			}
		} else {
			// Relation/Episode: 2 通道（content_dense + content_sparse）
			weights = []float64{c.ContentDense, c.ContentSparse}
			total := weights[0] + weights[1]
			if total == 0 || len(weights) != numChannels {
				weights = autoBalanceWeights(numChannels)
			}
		}
		return milvusclient.NewWeightedReranker(weights), nil
	case *graph.RRFRankConfig:
		k := c.K
		if k <= 0 {
			k = 60
		}
		return milvusclient.NewRRFReranker().WithK(float64(k)), nil
	default:
		return milvusclient.NewRRFReranker(), nil
	}
}

// autoBalanceWeights 为指定数量的通道生成均衡权重。
func autoBalanceWeights(numChannels int) []float64 {
	weights := make([]float64, numChannels)
	w := 1.0 / float64(numChannels)
	for i := range weights {
		weights[i] = w
	}
	return weights
}

// expandEntities 通过 BFS 扩展实体 UUID 集合。
// 从已有实体出发，通过 relations 字段找到关联的实体 UUID。
func (s *graphSearcher) expandEntities(ctx context.Context, uuidSet map[string]struct{}) (map[string]struct{}, error) {
	if len(uuidSet) == 0 {
		return nil, nil
	}

	expanded := make(map[string]struct{})
	ids := uuidSetToSlice(uuidSet)
	expr := buildIDFilterExpr(ids)

	// 查询实体的 relations 和 episodes 字段
	queryOpt := milvusclient.NewQueryOption(CollectionEntity).WithFilter(expr).
		WithOutputFields("uuid", "relations", "episodes")

	resultSet, err := s.client.Query(ctx, queryOpt)
	if err != nil {
		return nil, fmt.Errorf("BFS 扩展实体查询失败: %w", err)
	}

	// 解析查询结果
	for _, row := range resultSetToMaps(resultSet) {
		if relations, ok := row["relations"].([]string); ok {
			for _, r := range relations {
				expanded[r] = struct{}{}
			}
		}
		if episodes, ok := row["episodes"].([]string); ok {
			for _, e := range episodes {
				expanded[e] = struct{}{}
			}
		}
	}

	return expanded, nil
}

// expandRelations 通过 BFS 扩展关系 UUID 集合。
// 从已有关系出发，通过 lhs/rhs 找到关联的实体 UUID。
func (s *graphSearcher) expandRelations(ctx context.Context, uuidSet map[string]struct{}) (map[string]struct{}, error) {
	if len(uuidSet) == 0 {
		return nil, nil
	}

	expanded := make(map[string]struct{})
	ids := uuidSetToSlice(uuidSet)
	expr := buildIDFilterExpr(ids)

	queryOpt := milvusclient.NewQueryOption(CollectionRelation).WithFilter(expr).
		WithOutputFields("uuid", "lhs", "rhs")

	resultSet, err := s.client.Query(ctx, queryOpt)
	if err != nil {
		return nil, fmt.Errorf("BFS 扩展关系查询失败: %w", err)
	}

	for _, row := range resultSetToMaps(resultSet) {
		if lhs, ok := row["lhs"].(string); ok && lhs != "" {
			expanded[lhs] = struct{}{}
		}
		if rhs, ok := row["rhs"].(string); ok && rhs != "" {
			expanded[rhs] = struct{}{}
		}
	}

	return expanded, nil
}

// combinedRerank 跨集合增强重排序。
//
// 对应 Python: MilvusGraphStore._combined_rerank
func (s *graphSearcher) combinedRerank(ctx context.Context, query string, results map[string][]map[string]any, o graph.Options) (map[string][]map[string]any, error) {
	if o.Reranker == nil {
		return results, nil
	}

	// 对每个集合的结果进行 rerank
	reranked := make(map[string][]map[string]any)
	for coll, items := range results {
		if len(items) == 0 {
			reranked[coll] = items
			continue
		}

		// 构造文档列表
		documents := make([]string, len(items))
		for i, item := range items {
			if content, ok := item["content"].(string); ok {
				documents[i] = content
			}
		}

		// 调用 reranker
		scoreMap, err := o.Reranker.Rerank(ctx, query, documents)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("collection", coll).Msg("重排序失败，使用原始顺序")
			reranked[coll] = items
			continue
		}

		// 按 reranker 分数排序
		minScore := o.MinScore
		reranked[coll] = filterByScore(items, scoreMap, minScore)
	}

	return reranked, nil
}

// filterByScore 按分数过滤和排序结果
func filterByScore(items []map[string]any, scoreMap map[string]float64, minScore float64) []map[string]any {
	type scoredItem struct {
		item  map[string]any
		score float64
	}

	var scored []scoredItem
	for _, item := range items {
		// 获取文档内容作为 scoreMap 的 key
		key := ""
		if content, ok := item["content"].(string); ok {
			key = content
		}
		if score, ok := scoreMap[key]; ok && score >= minScore {
			scored = append(scored, scoredItem{item: item, score: score})
		}
	}

	// 按分数降序排序（简单冒泡）
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	result := make([]map[string]any, len(scored))
	for i, s := range scored {
		result[i] = s.item
	}
	return result
}

// extractUUIDs 从搜索结果中提取 UUID 集合
func extractUUIDs(results []map[string]any) map[string]struct{} {
	uuidSet := make(map[string]struct{})
	for _, r := range results {
		if uuid, ok := r["uuid"].(string); ok && uuid != "" {
			uuidSet[uuid] = struct{}{}
		}
	}
	return uuidSet
}

// uuidSetToSlice 将 UUID 集合转为切片
func uuidSetToSlice(uuidSet map[string]struct{}) []string {
	result := make([]string, 0, len(uuidSet))
	for uuid := range uuidSet {
		result = append(result, uuid)
	}
	return result
}

// stringsToAny 将 []string 转为 []any
func stringsToAny(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

// parseResultSets 将 Milvus ResultSet 列表解析为 []map[string]any
func parseResultSets(resultSets []milvusclient.ResultSet) []map[string]any {
	var results []map[string]any
	for _, rs := range resultSets {
		rowCount := rs.ResultCount
		if rowCount == 0 {
			continue
		}

		// 收集所有列的数据
		colData := make(map[string][]any)
		for _, col := range rs.Fields {
			name := col.Name()
			values := make([]any, rowCount)
			for j := 0; j < rowCount; j++ {
				v, _ := col.Get(j)
				values[j] = v
			}
			colData[name] = values
		}

		// 转换为行
		for j := 0; j < rowCount; j++ {
			row := make(map[string]any)
			for name, values := range colData {
				if j < len(values) {
					row[name] = values[j]
				}
			}
			results = append(results, row)
		}
	}
	return results
}

// resultSetToMaps 将单个 ResultSet 转为 []map[string]any
func resultSetToMaps(rs milvusclient.ResultSet) []map[string]any {
	rowCount := rs.ResultCount
	if rowCount == 0 {
		return nil
	}

	colData := make(map[string][]any)
	for _, col := range rs.Fields {
		name := col.Name()
		values := make([]any, rowCount)
		for j := 0; j < rowCount; j++ {
			v, _ := col.Get(j)
			values[j] = v
		}
		colData[name] = values
	}

	var results []map[string]any
	for j := 0; j < rowCount; j++ {
		row := make(map[string]any)
		for name, values := range colData {
			if j < len(values) {
				row[name] = values[j]
			}
		}
		results = append(results, row)
	}
	return results
}

// buildSearchFilterExpr 从选项构建搜索过滤表达式
func buildSearchFilterExpr(o graph.Options) string {
	if len(o.IDs) > 0 {
		ids := make([]string, 0, len(o.IDs))
		for _, id := range o.IDs {
			ids = append(ids, fmt.Sprintf("%v", id))
		}
		return buildIDFilterExpr(ids)
	}
	if o.FilterExpr != nil {
		exprVal, err := o.FilterExpr.ToExpr("milvus")
		if err == nil {
			if strExpr, ok := exprVal.(string); ok && strExpr != "" {
				return strExpr
			}
		}
	}
	return ""
}

// ensureSuffix 确保字符串有指定后缀
func ensureSuffix(s, suffix string) string {
	if strings.HasSuffix(s, suffix) {
		return s
	}
	return s + suffix
}
