// 图检索器：chunk 检索 + 三元组 beam search 图扩展。
//
// Python: graph_retriever.py (GraphRetriever L228-814)
//
// 文件目录：
//
//	（见 retriever/doc.go）
package retriever

import (
	"context"
	"fmt"
	"sort"
	"strings"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GraphRetriever 图检索器。
//
// chunk 检索 + 三元组 beam search 图扩展 + RRF 融合。六可选构造
// （nil = Python None → 动态创建路径）。
type GraphRetriever struct {
	// chunkRetriever 固定 chunk 检索器（nil = 动态创建）
	chunkRetriever Retriever
	// tripleRetriever 固定三元组检索器（nil = 动态创建）
	tripleRetriever Retriever
	// vectorStore 向量存储（动态创建用）
	vectorStore vector_store.VectorStore
	// embedModel 嵌入模型（动态创建用）
	embedModel embedding.BaseEmbedding
	// chunkCollection chunk 集合名
	chunkCollection string
	// tripleCollection 三元组集合名
	tripleCollection string
	// indexType 索引类型（"" = Python None；上层 GraphKB 注入）
	indexType string
}

// GraphRetrieverOption 构造选项。
type GraphRetrieverOption func(*GraphRetriever)

// 编译期断言：GraphRetriever 满足 Retriever 接口。
var _ Retriever = (*GraphRetriever)(nil)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultGraphHops 默认图跳数（Python kwargs.get("graph_hops", 2)）
	defaultGraphHops = 2
	// defaultGraphFallbackTopK 空 chunks sparse 回落默认 top_k（topk or 5）
	defaultGraphFallbackTopK = 5
	// graphFetchLimit fetch 查询 limit（Python L646/L557 字面 100）
	graphFetchLimit = 100
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithGraphChunkRetriever 设置固定 chunk 检索器。
func WithGraphChunkRetriever(r Retriever) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.chunkRetriever = r }
}

// WithGraphTripleRetriever 设置固定三元组检索器。
func WithGraphTripleRetriever(r Retriever) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.tripleRetriever = r }
}

// WithGraphVectorStore 设置向量存储（动态创建用）。
func WithGraphVectorStore(s vector_store.VectorStore) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.vectorStore = s }
}

// WithGraphEmbedModel 设置嵌入模型（动态创建用）。
func WithGraphEmbedModel(m embedding.BaseEmbedding) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.embedModel = m }
}

// WithGraphChunkCollection 设置 chunk 集合名。
func WithGraphChunkCollection(name string) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.chunkCollection = name }
}

// WithGraphTripleCollection 设置三元组集合名。
func WithGraphTripleCollection(name string) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.tripleCollection = name }
}

// NewGraphRetriever 创建图检索器。
//
// 对齐 Python __init__（L231-260）：六可选参数全收，零校验；index_type
// 留空由上层注入（SetIndexType）。
func NewGraphRetriever(opts ...GraphRetrieverOption) *GraphRetriever {
	g := &GraphRetriever{}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// IndexType 返回索引类型（"" = Python None）。
func (g *GraphRetriever) IndexType() string { return g.indexType }

// SetIndexType 注入索引类型（上层 GraphKB 调用，Python L260 注释语义）。
func (g *GraphRetriever) SetIndexType(indexType string) { g.indexType = indexType }

// GetRetrieverForMode 按 mode 取对应检索器。
//
// 对齐 Python get_retriever_for_mode（L304-378）：固定优先（不支持
// 155205）→ 动态创建（无 store 155206）→ SetCollectionName 先于空校验
// （155207，L339-346 字面顺序）→ 三分支 → index_type 注入。
func (g *GraphRetriever) GetRetrieverForMode(mode RetrievalMode, isChunk bool) (Retriever, error) {
	if err := g.ensureModeAllowed(mode); err != nil {
		return nil, err
	}

	fixed := g.chunkRetriever
	if !isChunk {
		fixed = g.tripleRetriever
	}
	if fixed != nil {
		if !g.retrieverSupportsMode(fixed, mode) {
			kind := "chunk"
			if !isChunk {
				kind = "triple"
			}
			return nil, exception.BuildError(
				exception.StatusRetrievalRetrieverCapabilityNotSupport,
				exception.WithParam("error_msg", fmt.Sprintf("Provided %s retriever %s does not support mode=%s",
					kind, retrieverTypeName(fixed), mode)),
			)
		}
		return fixed, nil
	}

	if g.vectorStore == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverVectorStoreNotFound,
			exception.WithParam("error_msg", "vector_store is required for dynamic retriever creation"),
		)
	}

	collectionName := g.chunkCollection
	if !isChunk {
		collectionName = g.tripleCollection
	}
	// Python L339-346：先污染 collection_name 再判空（字面顺序复刻）。
	// 字段赋值经 collectionNameSetter 断言（接口无此方法，spec §8-9）。
	if setter, ok := g.vectorStore.(collectionNameSetter); ok {
		setter.SetCollectionName(collectionName)
	}
	if collectionName == "" {
		collectionType := "chunk"
		if !isChunk {
			collectionType = "triple"
		}
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverCollectionNotFound,
			exception.WithParam("error_msg", fmt.Sprintf("%s_collection is required for dynamic retriever creation", collectionType)),
		)
	}

	var retriever Retriever
	switch mode {
	case RetrievalModeVector:
		if g.embedModel == nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalRetrieverEmbedModelNotFound,
				exception.WithParam("error_msg", "embed_model is required for vector mode"),
			)
		}
		retriever = NewVectorRetriever(g.vectorStore, g.embedModel)
	case RetrievalModeSparse:
		retriever = NewSparseRetriever(g.vectorStore)
	default: // hybrid（Python else 分支）
		retriever = NewHybridRetriever(g.vectorStore, g.embedModel)
	}

	// Python L375-376：index_type 注入动态 retriever（对未实现
	// IndexTypeCarrier 的跳过——宽松语义收窄归档 spec §8-9）
	if g.indexType != "" {
		if ic, ok := retriever.(IndexTypeCarrier); ok {
			ic.SetIndexType(g.indexType)
		}
	}
	return retriever, nil
}

// Retrieve 图检索主入口。
//
// 对齐 Python retrieve（L380-433）：mode 校验 → graph_hops 读取（nil→2）
// → threshold+mode≠vector 155201 → chunk 检索 → GraphExpansion。
func (g *GraphRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if mode == "" {
		// Python 签名默认 "hybrid"（spec §8-1 零值回落同构）
		mode = RetrievalModeHybrid
	}
	if err := g.ensureModeAllowed(mode); err != nil {
		return nil, err
	}
	options := NewRetrieveOptions(opts...)
	graphHops := defaultGraphHops
	if options.GraphHops != nil {
		graphHops = *options.GraphHops
	}
	if scoreThreshold != nil && mode != RetrievalModeVector {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverScoreThresholdInvalid,
			exception.WithParam("error_msg", "score_threshold is only supported when mode='vector'"),
		)
	}

	chunkRetriever, err := g.GetRetrieverForMode(mode, true)
	if err != nil {
		return nil, err
	}
	chunkResults, err := chunkRetriever.Retrieve(ctx, query, topK, scoreThreshold, mode)
	if err != nil {
		return nil, err
	}

	logger.Info(logComponent).
		Str("event_type", "GRAPH_RETRIEVAL").
		Bool("graph_expansion", true).
		Int("chunk_hits", len(chunkResults)).
		Int("topk", topK).
		Str("mode", string(mode)).
		Msg("[graph] Graph retrieval: graph_expansion=True")

	// Python graph_expansion(max_length=graph_hops) kwargs 透传
	return g.GraphExpansion(ctx, query, chunkResults, nil, &topK, mode, append(opts, WithRetrieveGraphHops(graphHops))...)
}

// GraphExpansion 图扩展。
//
// 对齐 Python graph_expansion（L435-526）：空 chunks sparse 二次回落；
// triples 未传走 _fetchTriples（异常 warn 回落空）；空 triples 返回原
// chunks；beam search（异常 warn 回落原 chunks——降级终点）；列对齐展开；
// _fetchChunks；RRF k=60 融合；topk 截断。
func (g *GraphRetriever) GraphExpansion(ctx context.Context, query string, chunks, triples []common.RetrievalResult, topk *int, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if mode == "" {
		// Python 签名默认 "hybrid"（spec §8-1 零值回落同构）
		mode = RetrievalModeHybrid
	}
	if err := g.ensureModeAllowed(mode); err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		logger.Warn(logComponent).
			Str("mode", string(mode)).
			Msg("[graph] chunk_retriever returned empty, no results to expand")
		if mode == RetrievalModeSparse {
			sparseRetriever, err := g.GetRetrieverForMode(RetrievalModeSparse, true)
			if err != nil {
				return nil, err
			}
			fallbackTopK := defaultGraphFallbackTopK
			if topk != nil && *topk > 0 {
				fallbackTopK = *topk
			}
			fallback, err := sparseRetriever.Retrieve(ctx, query, fallbackTopK, nil, RetrievalModeSparse)
			if err != nil {
				return nil, err
			}
			// Python fallback[:topk] if topk else fallback（topk=0 falsy 不截断）
			if topk != nil && *topk > 0 && *topk < len(fallback) {
				return fallback[:*topk], nil
			}
			return fallback, nil
		}
		return nil, nil
	}

	if len(triples) == 0 {
		var err error
		triples, err = g.fetchTriples(ctx, chunks, mode)
		if err != nil {
			// Python try/except：异常 warn 回落空列表
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Failed to fetch triples from chunk index")
			triples = nil
		} else {
			logger.Info(logComponent).
				Int("chunks", len(chunks)).
				Int("triples", len(triples)).
				Msg("[graph] Fetching triples from chunk index")
		}
	}

	if len(triples) == 0 {
		logger.Info(logComponent).Msg("[graph] No triples found, returning original chunks")
		return truncateResults(chunks, topk), nil
	}

	// beam search（含 TripleBeamSearch 构造的 kwargs 透传）
	var beams []*common.TripleBeam
	func() {
		tripleRetriever, err := g.GetRetrieverForMode(mode, false)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Beam search failed, falling back to chunks")
			return
		}
		options := NewRetrieveOptions(opts...)
		// Python retrieve 传 max_length=graph_hops（retrieve 层已折算
		// 为 opts 的 GraphHops；此处在未设时保持 TripleBeamSearch 默认 2）
		searchOpts := []TripleBeamSearchOption{WithTripleRetrieveMode(string(mode))}
		if options.GraphHops != nil {
			searchOpts = append(searchOpts, WithTripleMaxLength(*options.GraphHops))
		}
		if options.NumBeams != nil {
			searchOpts = append(searchOpts, WithTripleNumBeams(*options.NumBeams))
		}
		if options.NumCandidatesPerBeam != nil {
			searchOpts = append(searchOpts, WithTripleNumCandidatesPerBeam(*options.NumCandidatesPerBeam))
		}
		search, err := NewTripleBeamSearch(tripleRetriever, searchOpts...)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Beam search failed, falling back to chunks")
			return
		}
		beams, err = search.BeamSearch(ctx, query, triples)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Beam search failed, falling back to chunks")
			beams = nil
		}
	}()

	if len(beams) == 0 {
		logger.Info(logComponent).Msg("[graph] Beam search returned empty, returning original chunks")
		return truncateResults(chunks, topk), nil
	}

	// 列对齐展开（Python L504-510 按列优先）
	maxLength := 0
	for _, b := range beams {
		if b.Len() > maxLength {
			maxLength = b.Len()
		}
	}
	var expandedTriples []common.RetrievalResult
	for col := 0; col < maxLength; col++ {
		for _, beam := range beams {
			if col >= beam.Len() {
				continue
			}
			expandedTriples = append(expandedTriples, beam.Get(col))
		}
	}

	newChunks, err := g.fetchChunks(ctx, expandedTriples, mode)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).
		Int("triples", len(expandedTriples)).
		Int("additional_chunks", len(newChunks)).
		Msg("[graph] Graph expansion beam results")

	var fused []common.RetrievalResult
	if len(newChunks) > 0 {
		fused = utils.RRFFusion([][]common.RetrievalResult{newChunks, chunks}, 60)
	} else {
		fused = chunks
	}
	return truncateResults(fused, topk), nil
}

// BatchRetrieve 批量并发检索。
//
// 对齐 Python batch_retrieve（L782-790）：裸 gather 全启动、错误负载与
// 传播序一致。裸 errgroup（不派生 ctx、不 cancel——13.20 先例；WithContext
// 会在首错时取消兄弟协程，偏离 Python gather 语义）。
func (g *GraphRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	results := make([][]common.RetrievalResult, len(queries))
	var gg errgroup.Group
	for i, query := range queries {
		i, query := i, query
		gg.Go(func() error {
			ret, err := g.Retrieve(gctx0(ctx), query, topK, nil, "", opts...)
			if err != nil {
				return err
			}
			results[i] = ret
			return nil
		})
	}
	if err := gg.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// RetrieveSearchResults 返回 nil（对齐 Python 基类空方法体 None）。
func (g *GraphRetriever) RetrieveSearchResults(context.Context, string, int, RetrievalMode, ...RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}

// Close 关闭两个固定检索器（动态创建的不持有——对齐 Python 只关
// chunk_retriever/triple_retriever 两字段）。
func (g *GraphRetriever) Close() {
	if g.chunkRetriever != nil {
		g.chunkRetriever.Close()
	}
	if g.tripleRetriever != nil {
		g.tripleRetriever.Close()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// gctx0 BatchRetrieve 占位（裸 errgroup 直传 ctx）。
func gctx0(ctx context.Context) context.Context { return ctx }

// ensureModeAllowed mode 校验（Python _ensure_mode_allowed L269-285）。
//
// indexType ""（None）免检；未知 155203；不兼容 155204（allowed 清单
// sorted 逗号空格拼接）。
func (g *GraphRetriever) ensureModeAllowed(mode RetrievalMode) error {
	if g.indexType == "" {
		return nil // index_type 未注入时免检（上层保证）
	}
	allowed, ok := allowedModesForIndexType(g.indexType)
	if !ok {
		return exception.BuildError(
			exception.StatusRetrievalRetrieverIndexTypeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("Unsupported index_type=%s", g.indexType)),
		)
	}
	if !allowed[mode] {
		sorted := make([]string, 0, len(allowed))
		for m := range allowed {
			sorted = append(sorted, string(m))
		}
		sort.Strings(sorted)
		return exception.BuildError(
			exception.StatusRetrievalRetrieverModeInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("mode=%s is incompatible with index_type=%s; allowed modes: [%s]",
				mode, g.indexType, strings.Join(sorted, ", "))),
		)
	}
	return nil
}

// allowedModesForIndexType 对齐 Python _allowed_modes（L262-267）。
func allowedModesForIndexType(indexType string) (map[RetrievalMode]bool, bool) {
	switch indexType {
	case "vector":
		return map[RetrievalMode]bool{RetrievalModeVector: true}, true
	case "bm25":
		return map[RetrievalMode]bool{RetrievalModeSparse: true}, true
	case "hybrid":
		return map[RetrievalMode]bool{
			RetrievalModeVector: true, RetrievalModeSparse: true, RetrievalModeHybrid: true,
		}, true
	default:
		return nil, false
	}
}

// retrieverSupportsMode 对齐 Python _retriever_supports_mode（L287-302）：
// 类型 switch 三兄弟 + SupportedModesProvider 探测 + 兜底 true。
func (g *GraphRetriever) retrieverSupportsMode(r Retriever, mode RetrievalMode) bool {
	switch r.(type) {
	case *VectorRetriever:
		return mode == RetrievalModeVector
	case *SparseRetriever:
		return mode == RetrievalModeSparse
	case *HybridRetriever:
		return mode == RetrievalModeVector || mode == RetrievalModeSparse || mode == RetrievalModeHybrid
	}
	if sp, ok := r.(SupportedModesProvider); ok {
		for _, m := range sp.SupportedModes() {
			if m == mode {
				return true
			}
		}
		return false
	}
	return true
}

// retrieverTypeName 检索器类型名（Python __class__.__name__）。
func retrieverTypeName(r Retriever) string {
	return fmt.Sprintf("%T", r)
}

// truncateResults topk 截断（nil 不截断；Python fused[:topk] if topk）。
//
// Python if topk 为 truthiness 判断（0 视为未设不截断），Go 统一为
// *topk > 0 才截断（spec §8-10 偏差归档：两语言 0 语义对齐为不截断）。
func truncateResults(results []common.RetrievalResult, topk *int) []common.RetrievalResult {
	if topk == nil || *topk <= 0 {
		return results
	}
	if *topk < len(results) {
		return results[:*topk]
	}
	return results
}

// fetchTriples 按 chunk_id 反查三元组（Python _fetch_triples L598-680）。
func (g *GraphRetriever) fetchTriples(ctx context.Context, chunks []common.RetrievalResult, mode RetrievalMode) ([]common.RetrievalResult, error) {
	var chunkIDs []string
	for _, x := range chunks {
		if x.Metadata != nil {
			if cid, ok := x.Metadata["chunk_id"].(string); ok && cid != "" {
				chunkIDs = append(chunkIDs, cid)
			}
		}
	}
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	tripleRetriever, err := g.GetRetrieverForMode(mode, false)
	if err != nil {
		return nil, err
	}

	vs, ok := tripleRetriever.(VectorStoreAccessor)
	if !ok {
		logger.Warn(logComponent).
			Msg("[graph] triple_retriever does not have vector_store, cannot fetch triples")
		return nil, nil
	}
	store := vs.VectorStore()
	if store == nil {
		logger.Warn(logComponent).
			Msg("[graph] Cannot fetch triples: missing client or collection_name")
		return nil, nil
	}

	// store 具体类型断言分发（Python isinstance(client, MilvusClient)/
	// _is_chroma_client 的 store 层等价物，spec §3.6）
	var items []fetchItem
	switch s := store.(type) {
	case *vector_store.MilvusVectorStore:
		client, err := s.Client(ctx)
		if err != nil {
			return nil, err
		}
		collectionName := s.CollectionName()
		if collectionName == "" {
			logger.Warn(logComponent).
				Msg("[graph] Cannot fetch triples: missing client or collection_name")
			return nil, nil
		}
		escaped := make([]string, len(chunkIDs))
		for i, cid := range chunkIDs {
			escaped[i] = fmt.Sprintf("%q", cid)
		}
		filterExpr := fmt.Sprintf("chunk_id IN [%s]", strings.Join(escaped, ", "))
		items, err = queryByFilterMilvus(ctx, client, collectionName, filterExpr, graphFetchLimit)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Failed to fetch triples for chunks")
			return nil, nil
		}
	case *vector_store.ChromaVectorStore:
		client := s.Client()
		collectionName := s.CollectionName()
		if collectionName == "" {
			logger.Warn(logComponent).
				Msg("[graph] Cannot fetch triples: missing client or collection_name")
			return nil, nil
		}
		where := chromav2.InString("chunk_id", chunkIDs...)
		items, err = queryByFilterChroma(ctx, client, collectionName, where, graphFetchLimit)
		if err != nil {
			// Python _query_by_filter_chroma 整体 try/except 吞错返回 []
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Chroma query failed")
			return nil, nil
		}
	default:
		logger.Warn(logComponent).
			Msg("[graph] Unsupported client type in fetch_for_chunks")
		return nil, nil
	}

	// _fetch_triples 归一化：chunk_id 回落空串（Python L670 metadata.get("chunk_id", "")）
	return fetchItemsToRetrievalResults(items, ""), nil
}

// fetchChunks 按 chunk_id 正查 chunk（Python _fetch_chunks L682-781）。
func (g *GraphRetriever) fetchChunks(ctx context.Context, triples []common.RetrievalResult, mode RetrievalMode) ([]common.RetrievalResult, error) {
	chunkIDSet := make(map[string]struct{})
	for _, node := range triples {
		if node.ChunkID != "" {
			chunkIDSet[node.ChunkID] = struct{}{}
		}
	}
	if len(chunkIDSet) == 0 {
		return nil, nil
	}

	chunkRetriever, err := g.GetRetrieverForMode(mode, true)
	if err != nil {
		return nil, err
	}

	vs, ok := chunkRetriever.(VectorStoreAccessor)
	if !ok {
		logger.Warn(logComponent).
			Msg("[graph] chunk_retriever does not have vector_store, cannot fetch chunks")
		return nil, nil
	}
	store := vs.VectorStore()
	if store == nil {
		logger.Warn(logComponent).
			Msg("[graph] Cannot fetch chunks: missing client or collection_name")
		return nil, nil
	}

	// 逐 chunk_id 并发 fetch（Python gather 全启动 + fetchChunk 内部吞错
	// 返回 None；Go 裸 errgroup 语义同构——吞错分支不返回 error）
	chunkIDs := make([]string, 0, len(chunkIDSet))
	for cid := range chunkIDSet {
		chunkIDs = append(chunkIDs, cid)
	}
	results := make([]*common.RetrievalResult, len(chunkIDs))
	var gg errgroup.Group
	for i, cid := range chunkIDs {
		i, cid := i, cid
		gg.Go(func() error {
			result, err := g.fetchChunk(ctx, store, cid)
			if err != nil {
				// Python fetch_chunk except 返回 None（L746-749 warn）
				logger.Warn(logComponent).Err(err).
					Str("chunk_id", cid).
					Msg("[graph] Failed to query chunk")
				return nil
			}
			results[i] = result
			return nil
		})
	}
	if err := gg.Wait(); err != nil {
		return nil, err
	}

	var chunks []common.RetrievalResult
	for _, r := range results {
		if r != nil {
			chunks = append(chunks, *r)
		}
	}
	return chunks, nil
}

// fetchChunk 单 chunk 查询（Python fetch_chunk L728-768：吞错返回 nil）。
func (g *GraphRetriever) fetchChunk(ctx context.Context, store vector_store.VectorStore, chunkID string) (*common.RetrievalResult, error) {
	switch s := store.(type) {
	case *vector_store.MilvusVectorStore:
		client, err := s.Client(ctx)
		if err != nil {
			return nil, err
		}
		filterExpr := fmt.Sprintf("chunk_id == %q", chunkID)
		items, err := queryByFilterMilvus(ctx, client, s.CollectionName(), filterExpr, 1)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, nil
		}
		// chunk_id 回落查询值（Python L766 metadata.get("chunk_id", chunk_id)）
		results := fetchItemsToRetrievalResults(items[:1], chunkID)
		return &results[0], nil
	case *vector_store.ChromaVectorStore:
		where := chromav2.EqString("chunk_id", chunkID)
		items, err := queryByFilterChroma(ctx, s.Client(), s.CollectionName(), where, 1)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, nil
		}
		results := fetchItemsToRetrievalResults(items[:1], chunkID)
		return &results[0], nil
	default:
		logger.Warn(logComponent).
			Msg("[graph] Unsupported client type in fetch_chunk")
		return nil, nil
	}
}
