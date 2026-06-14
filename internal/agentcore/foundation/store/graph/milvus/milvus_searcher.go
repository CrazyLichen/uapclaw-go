package milvus

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	querypkg "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
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
	if bfsDepth <= 0 || (collection != CollectionEntity && collection != CollectionRelation) {
		return s.rawHybridSearch(ctx, query, collection, k, o)
	}

	// 有 BFS：对齐 Python 实现
	// Python: 每轮搜索用 skip_ranking=True（不做 rerank），最后统一排序
	uuids := make(map[string]struct{})
	allResults := make(map[string]map[string]any) // uuid -> result
	isSimilarity := s.metric == "IP" || s.metric == "COSINE"

	// 获取查询向量（所有轮次共用）
	queryEmb, err := s.queryEmbedding(ctx, query, o)
	if err != nil {
		return nil, fmt.Errorf("获取查询向量失败: %w", err)
	}
	bfsOpts := o
	bfsOpts.QueryEmbedding = queryEmb

	for i := 0; i <= bfsDepth; i++ {
		isExpansionRound := i < bfsDepth

		// 构建搜索选项：每轮搜索用 skip_ranking=True（不传 reranker），对齐 Python
		searchOpts := bfsOpts
		searchOpts.Reranker = nil
		searchOpts.K = k

		// 构建 expr：有新 UUID 时按 UUID 过滤
		if len(uuids) > 0 {
			uuidSlice := uuidSetToSlice(uuids)
			if collection == CollectionEntity {
				searchOpts.IDs = stringsToAny(uuidSlice)
			} else {
				// Relation: 按 lhs/rhs 过滤，对齐 Python
				searchOpts.IDs = nil
				uuidsAny := stringsToAny(uuidSlice)
				lhsExpr := querypkg.InList("lhs", uuidsAny)
				rhsExpr := querypkg.InList("rhs", uuidsAny)
				searchOpts.FilterExpr = querypkg.Or(lhsExpr, rhsExpr)
				if o.FilterExpr != nil {
					searchOpts.FilterExpr = querypkg.And(o.FilterExpr, searchOpts.FilterExpr)
				}
			}
		}

		// 执行搜索
		res, err := s.rawHybridSearch(ctx, query, collection, k, searchOpts)
		if err != nil {
			logger.Warn(logComponent).Err(err).Int("bfs_round", i+1).Msg("BFS 搜索失败")
			break
		}

		// 收集新 UUID 和结果
		newUUIDs := make(map[string]struct{})
		for _, doc := range res {
			if uuid, ok := doc["uuid"].(string); ok && uuid != "" {
				if _, exists := uuids[uuid]; !exists {
					newUUIDs[uuid] = struct{}{}
				}
				allResults[uuid] = doc
			}
		}

		// 将新发现的 UUID 加入总集合
		for uuid := range newUUIDs {
			uuids[uuid] = struct{}{}
		}

		// 图扩展
		if isExpansionRound && len(newUUIDs) > 0 {
			var expanded map[string]struct{}
			if collection == CollectionEntity {
				expanded, err = s.expandEntities(ctx, newUUIDs)
			} else {
				expanded, err = s.expandRelations(ctx, newUUIDs, allResults)
			}
			if err != nil {
				logger.Warn(logComponent).Err(err).Int("bfs_round", i+1).Msg("BFS 扩展失败")
				break
			}

			// 过滤掉已知的 UUID
			newExpanded := make(map[string]struct{})
			for uuid := range expanded {
				if _, exists := uuids[uuid]; !exists {
					newExpanded[uuid] = struct{}{}
				}
			}

			if len(newExpanded) == 0 {
				break
			}

			// bfs_k 裁剪：按 distance 排序只保留 top-k，对齐 Python
			if bfsK < len(newExpanded) {
				newExpanded = s.topKByDistance(newExpanded, allResults, bfsK, isSimilarity)
			}

			// 更新下一轮搜索的 UUID 集合
			for uuid := range newExpanded {
				uuids[uuid] = struct{}{}
			}
		}
	}

	// 最后统一排序（对齐 Python: _rank_results）
	candidates := make([]map[string]any, 0, len(allResults))
	for _, doc := range allResults {
		candidates = append(candidates, doc)
	}

	// 按 distance 排序
	sortByDistance(candidates, isSimilarity)

	// 截取 top-k
	if len(candidates) > k {
		candidates = candidates[:k]
	}

	// 如果配置了 reranker，做最终排序
	if o.Reranker != nil && len(candidates) > 0 {
		documents := make([]string, len(candidates))
		for i, item := range candidates {
			if content, ok := item["content"].(string); ok {
				documents[i] = content
			}
		}
		scoreMap, err := o.Reranker.Rerank(ctx, query, documents)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("BFS 最终重排序失败，使用距离排序")
			return candidates, nil
		}
		minScore := o.MinScore
		candidates = filterByScore(candidates, scoreMap, minScore)
	}

	return candidates, nil
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
			return nil, fmt.Errorf("milvus 后端应返回 string 类型的表达式")
		}
		expr = strExpr
	}

	// 构建输出字段
	outputFields := o.OutputFields
	if len(outputFields) == 0 {
		outputFields = []string{"uuid", "content", "obj_type", "user_id"}
		switch collection {
		case CollectionEntity:
			outputFields = append(outputFields, "name", "relations", "episodes")
		case CollectionRelation:
			outputFields = append(outputFields, "name", "lhs", "rhs", "valid_since", "valid_until")
		case CollectionEpisode:
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
	// BM25 搜索：使用查询文本作为输入，Milvus BM25 Function 需要文本输入来生成分词稀疏向量
	// 对齐 Python: sparse_req = AnnRequest("content_bm25", limit, [query])
	sparseReq := milvusclient.NewAnnRequest("content_bm25", k, entity.Text(query))
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
		WithOutputFields(outputFields...).
		WithReranker(reranker)

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
// 对齐 Python: _expand_entities — 通过 Relation 集合的 lhs/rhs 字段扩展，
// 而非通过 Entity 集合的 relations 字段。
func (s *graphSearcher) expandEntities(ctx context.Context, uuidSet map[string]struct{}) (map[string]struct{}, error) {
	if len(uuidSet) == 0 {
		return nil, nil
	}

	expanded := make(map[string]struct{})
	ids := uuidSetToSlice(uuidSet)

	// 对齐 Python: 在 Relation 集合中按 lhs/rhs 查找关联实体
	lhsExpr := buildIDFilterExprWithField(ids, "lhs")
	rhsExpr := buildIDFilterExprWithField(ids, "rhs")
	combinedExpr := fmt.Sprintf("(%s) or (%s)", lhsExpr, rhsExpr)

	queryOpt := milvusclient.NewQueryOption(CollectionRelation).WithFilter(combinedExpr).
		WithOutputFields("lhs", "rhs")

	resultSet, err := s.client.Query(ctx, queryOpt)
	if err != nil {
		return nil, fmt.Errorf("BFS 扩展实体查询失败: %w", err)
	}

	// 解析查询结果，收集 lhs/rhs 对应的实体 UUID
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

// expandRelations 通过 BFS 扩展关系 UUID 集合。
// 对齐 Python: _expand_relations — 先从 lookup 取 lhs/rhs 实体 UUID，
// 再查 Entity 集合的 relations 字段获取关联的关系 UUID。
func (s *graphSearcher) expandRelations(ctx context.Context, uuidSet map[string]struct{}, lookup map[string]map[string]any) (map[string]struct{}, error) {
	if len(uuidSet) == 0 {
		return nil, nil
	}

	// 从 lookup 中提取 lhs/rhs 实体 UUID，对齐 Python
	nodeUUIDs := make(map[string]struct{})
	for uuid := range uuidSet {
		if relation, ok := lookup[uuid]; ok {
			if lhs, ok := relation["lhs"].(string); ok && lhs != "" {
				nodeUUIDs[lhs] = struct{}{}
			}
			if rhs, ok := relation["rhs"].(string); ok && rhs != "" {
				nodeUUIDs[rhs] = struct{}{}
			}
		}
	}

	if len(nodeUUIDs) == 0 {
		return nil, nil
	}

	// 查询 Entity 集合的 relations 字段，对齐 Python
	ids := uuidSetToSlice(nodeUUIDs)
	expr := buildIDFilterExpr(ids)

	queryOpt := milvusclient.NewQueryOption(CollectionEntity).WithFilter(expr).
		WithOutputFields("uuid", "relations")

	resultSet, err := s.client.Query(ctx, queryOpt)
	if err != nil {
		return nil, fmt.Errorf("BFS 扩展关系查询失败: %w", err)
	}

	expanded := make(map[string]struct{})
	for _, row := range resultSetToMaps(resultSet) {
		if relations, ok := row["relations"].([]string); ok {
			for _, r := range relations {
				expanded[r] = struct{}{}
			}
		}
	}

	return expanded, nil
}

// topKByDistance 按 distance 字段保留 top-k 个 UUID，对齐 Python bfs_k 裁剪逻辑。
// isSimilarity=true 时 distance 越大越相关（降序），false 时 distance 越小越相关（升序）。
func (s *graphSearcher) topKByDistance(uuids map[string]struct{}, lookup map[string]map[string]any, k int, isSimilarity bool) map[string]struct{} {
	type uuidDist struct {
		uuid     string
		distance float64
	}

	items := make([]uuidDist, 0, len(uuids))
	for uuid := range uuids {
		dist := math.Inf(-1) // 默认最小值
		if doc, ok := lookup[uuid]; ok {
			if d, ok := doc["distance"].(float64); ok {
				dist = d
			}
		}
		items = append(items, uuidDist{uuid: uuid, distance: dist})
	}

	if isSimilarity {
		// 相似度度量：降序排列，取前 k
		sort.Slice(items, func(i, j int) bool { return items[i].distance > items[j].distance })
	} else {
		// 距离度量：升序排列，取前 k
		sort.Slice(items, func(i, j int) bool { return items[i].distance < items[j].distance })
	}

	if k > len(items) {
		k = len(items)
	}
	result := make(map[string]struct{}, k)
	for i := 0; i < k; i++ {
		result[items[i].uuid] = struct{}{}
	}
	return result
}

// sortByDistance 按 distance 字段对搜索结果排序。
// isSimilarity=true 时降序（越大约相关），false 时升序（越小越相关）。
func sortByDistance(results []map[string]any, isSimilarity bool) {
	sort.Slice(results, func(i, j int) bool {
		di, _ := results[i]["distance"].(float64)
		dj, _ := results[j]["distance"].(float64)
		if isSimilarity {
			return di > dj
		}
		return di < dj
	})
}

// buildIDFilterExprWithField 构建指定字段的 UUID 过滤表达式。
// 例如 buildIDFilterExprWithField(ids, "lhs") 生成 "lhs in ["id1","id2"]"
func buildIDFilterExprWithField(ids []string, field string) string {
	if len(ids) == 0 {
		return ""
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf(`"%s"`, id)
	}
	return fmt.Sprintf(`%s in [%s]`, field, strings.Join(quoted, ", "))
}

// combinedRerank 跨集合增强重排序。
// 对齐 Python: MilvusGraphStore._combined_rerank
// 核心逻辑：利用关系信息增强实体排序 — 遍历每个 Entity 的 relations，
// 将关联 Relation 的 content 拼接到 Entity 的 content 中再 rerank。
func (s *graphSearcher) combinedRerank(ctx context.Context, query string, results map[string][]map[string]any, o graph.Options) (map[string][]map[string]any, error) {
	if o.Reranker == nil {
		return results, nil
	}

	// 关系增强：对齐 Python _combined_rerank 中的实体内容增强逻辑
	if entities, ok := results[CollectionEntity]; ok && len(entities) > 0 {
		relations := results[CollectionRelation]
		relUUIDs := make(map[string]map[string]any)
		for _, rel := range relations {
			if uuid, ok := rel["uuid"].(string); ok {
				relUUIDs[uuid] = rel
			}
		}

		// 遍历每个 Entity，将关联 Relation 的 content 拼接到 Entity 的 content 中
		for _, ent := range entities {
			// 保存原始 content
			originalContent, _ := ent["content"].(string)
			ent["original_content"] = originalContent

			// 收集关联 Relation 的 (content, distance)
			type relContent struct {
				content  string
				distance float64
			}
			var relatedContent []relContent

			if relIDs, ok := ent["relations"].([]string); ok {
				for _, relID := range relIDs {
					if rel, ok := relUUIDs[relID]; ok {
						content, _ := rel["content"].(string)
						distance, _ := rel["distance"].(float64)
						if content != "" {
							relatedContent = append(relatedContent, relContent{content: content, distance: distance})
						}
					}
				}
			}

			// 按 distance 降序排序，对齐 Python: content.sort(key=lambda rel: rel[1], reverse=True)
			sort.Slice(relatedContent, func(i, j int) bool {
				return relatedContent[i].distance > relatedContent[j].distance
			})

			// 拼接内容，对齐 Python: [原始content, 分隔线, ...关联Relation的content]
			if len(relatedContent) > 0 {
				var parts []string
				parts = append(parts, originalContent)
				parts = append(parts, "----------")
				for _, rc := range relatedContent {
					parts = append(parts, rc.content)
				}
				ent["content"] = strings.Join(parts, "\n - ")
			}
		}
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

	// 恢复 Entity 的原始 content，对齐 Python: ent["content"] = ent["original_content"]
	if entities, ok := reranked[CollectionEntity]; ok {
		for _, ent := range entities {
			if originalContent, ok := ent["original_content"].(string); ok {
				ent["content"] = originalContent
				delete(ent, "original_content")
			}
		}
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
