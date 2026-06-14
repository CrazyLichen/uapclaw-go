package milvus

import (
	"context"
	"fmt"
	"testing"

	"github.com/milvus-io/milvus/client/v2/column"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSearcherClient 用于搜索器测试的 Milvus 客户端模拟
type fakeSearcherClient struct {
	*fakeMilvusClient
	searchResults []milvusclient.ResultSet
	searchErr     error
	queryResult   milvusclient.ResultSet
	queryErr      error
}

func newFakeSearcherClient() *fakeSearcherClient {
	return &fakeSearcherClient{
		fakeMilvusClient: newFakeMilvusClient(),
	}
}

func (f *fakeSearcherClient) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return f.searchResults, f.searchErr
}

func (f *fakeSearcherClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...interface{}) (milvusclient.ResultSet, error) {
	return f.queryResult, f.queryErr
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGraphSearcher_Search_单集合(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	results, err := s.search(ctx, "test query", graph.WithCollection(CollectionEntity))
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if _, ok := results[CollectionEntity]; !ok {
		t.Error("search 单集合应返回对应集合的结果")
	}
}

func TestGraphSearcher_Search_全部集合(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	results, err := s.search(ctx, "test query")
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("搜索全部集合应返回3个集合，实际 %d", len(results))
	}
}

func TestGraphSearcher_Search_空集合名(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	results, err := s.search(ctx, "test query", graph.WithCollection(""))
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	// 空集合名等于 AllCollections，搜索全部
	if len(results) != 3 {
		t.Errorf("空集合名应搜索全部，实际 %d 个集合", len(results))
	}
}

func TestGraphSearcher_RawHybridSearch_Entity(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	nameCol := column.NewColumnVarChar("name", []string{"test"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol, nameCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	results, err := s.rawHybridSearch(ctx, "test", CollectionEntity, 5, graph.Options{})
	if err != nil {
		t.Fatalf("rawHybridSearch() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("rawHybridSearch 应返回1条结果，实际 %d", len(results))
	}
}

func TestGraphSearcher_RawHybridSearch_Relation(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	results, err := s.rawHybridSearch(ctx, "test", CollectionRelation, 5, graph.Options{})
	if err != nil {
		t.Fatalf("rawHybridSearch() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("rawHybridSearch 应返回1条结果，实际 %d", len(results))
	}
}

func TestGraphSearcher_RawHybridSearch_带过滤(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	o := graph.Options{IDs: []any{"abc"}}
	results, err := s.rawHybridSearch(ctx, "test", CollectionEntity, 5, o)
	if err != nil {
		t.Fatalf("rawHybridSearch() error = %v", err)
	}
	_ = results
}

func TestGraphSearcher_RawHybridSearch_带OutputFields(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	o := graph.Options{OutputFields: []string{"uuid", "content"}}
	results, err := s.rawHybridSearch(ctx, "test", CollectionEntity, 5, o)
	if err != nil {
		t.Fatalf("rawHybridSearch() error = %v", err)
	}
	_ = results
}

func TestGraphSearcher_RawHybridSearch_无Embedder(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	_, err := s.rawHybridSearch(ctx, "test", CollectionEntity, 5, graph.Options{})
	if err == nil {
		t.Error("rawHybridSearch 无 embedder 应返回错误")
	}
}

func TestGraphSearcher_RawHybridSearch_HybridSearch失败(t *testing.T) {
	fake := newFakeSearcherClient()
	fake.searchErr = fmt.Errorf("hybrid search error")
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	_, err := s.rawHybridSearch(ctx, "test", CollectionEntity, 5, graph.Options{})
	if err == nil {
		t.Error("rawHybridSearch HybridSearch 失败应返回错误")
	}
}

func TestGraphSearcher_QueryEmbedding_已提供(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	emb, err := s.queryEmbedding(ctx, "test", graph.Options{QueryEmbedding: []float64{0.5, 0.6, 0.7}})
	if err != nil {
		t.Fatalf("queryEmbedding() error = %v", err)
	}
	if len(emb) != 3 || emb[0] != 0.5 {
		t.Errorf("queryEmbedding 应返回已提供的向量，实际 %v", emb)
	}
}

func TestGraphSearcher_QueryEmbedding_无Embedder(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	_, err := s.queryEmbedding(ctx, "test", graph.Options{})
	if err == nil {
		t.Error("queryEmbedding 无 embedder 应返回错误")
	}
}

func TestGraphSearcher_QueryEmbedding_调用Embedder(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	emb := &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}
	s := newGraphSearcher(fake, emb, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	result, err := s.queryEmbedding(ctx, "test", graph.Options{})
	if err != nil {
		t.Fatalf("queryEmbedding() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("queryEmbedding 应返回3维向量，实际 %d 维", len(result))
	}
}

func TestBuildReranker_默认RRF(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	reranker, err := s.buildReranker(nil, CollectionEntity, 3)
	if err != nil {
		t.Fatalf("buildReranker() error = %v", err)
	}
	if reranker == nil {
		t.Error("buildReranker 默认应返回 RRF Reranker")
	}
}

func TestBuildReranker_Weighted(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	cfg := graph.NewWeightedRankConfig()
	reranker, err := s.buildReranker(cfg, CollectionEntity, 3)
	if err != nil {
		t.Fatalf("buildReranker() error = %v", err)
	}
	if reranker == nil {
		t.Error("buildReranker Weighted 应返回 Weighted Reranker")
	}
}

func TestBuildReranker_Weighted_Relation(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	cfg := graph.NewWeightedRankConfig()
	reranker, err := s.buildReranker(cfg, CollectionRelation, 2)
	if err != nil {
		t.Fatalf("buildReranker() error = %v", err)
	}
	if reranker == nil {
		t.Error("buildReranker Weighted Relation 应返回 Weighted Reranker")
	}
}

func TestBuildReranker_Weighted_零权重(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	cfg := &graph.WeightedRankConfig{NameDense: 0, ContentDense: 0, ContentSparse: 0}
	reranker, err := s.buildReranker(cfg, CollectionEntity, 3)
	if err != nil {
		t.Fatalf("buildReranker() error = %v", err)
	}
	if reranker == nil {
		t.Error("buildReranker 零权重应自动均衡")
	}
}

func TestBuildReranker_RRF(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	cfg := graph.NewRRFRankConfig()
	reranker, err := s.buildReranker(cfg, CollectionEntity, 3)
	if err != nil {
		t.Fatalf("buildReranker() error = %v", err)
	}
	if reranker == nil {
		t.Error("buildReranker RRF 应返回 RRF Reranker")
	}
}

func TestBuildReranker_RRF_K为零(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	cfg := &graph.RRFRankConfig{K: 0}
	reranker, err := s.buildReranker(cfg, CollectionEntity, 3)
	if err != nil {
		t.Fatalf("buildReranker() error = %v", err)
	}
	if reranker == nil {
		t.Error("buildReranker RRF K=0 应使用默认值")
	}
}

func TestAutoBalanceWeights(t *testing.T) {
	weights := autoBalanceWeights(3)
	if len(weights) != 3 {
		t.Fatalf("autoBalanceWeights(3) 应返回3个权重，实际 %d", len(weights))
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total < 0.99 || total > 1.01 {
		t.Errorf("autoBalanceWeights 总和应接近1.0，实际 %f", total)
	}
}

func TestExtractUUIDs(t *testing.T) {
	results := []map[string]any{
		{"uuid": "abc", "content": "hello"},
		{"uuid": "def", "content": "world"},
		{"uuid": "", "content": "empty uuid"},
		{"content": "no uuid"},
	}
	uuidSet := extractUUIDs(results)
	if len(uuidSet) != 2 {
		t.Errorf("extractUUIDs 应返回2个UUID，实际 %d", len(uuidSet))
	}
	if _, ok := uuidSet["abc"]; !ok {
		t.Error("extractUUIDs 应包含 abc")
	}
	if _, ok := uuidSet["def"]; !ok {
		t.Error("extractUUIDs 应包含 def")
	}
}

func TestUUIDSetToSlice(t *testing.T) {
	uuidSet := map[string]struct{}{
		"abc": {},
		"def": {},
	}
	slice := uuidSetToSlice(uuidSet)
	if len(slice) != 2 {
		t.Errorf("uuidSetToSlice 应返回2个元素，实际 %d", len(slice))
	}
}

func TestStringsToAny(t *testing.T) {
	ss := []string{"a", "b", "c"}
	result := stringsToAny(ss)
	if len(result) != 3 {
		t.Errorf("stringsToAny 应返回3个元素，实际 %d", len(result))
	}
	if result[0].(string) != "a" {
		t.Errorf("stringsToAny[0] = %v, want a", result[0])
	}
}

func TestFilterByScore(t *testing.T) {
	items := []map[string]any{
		{"content": "hello", "uuid": "1"},
		{"content": "world", "uuid": "2"},
		{"content": "foo", "uuid": "3"},
	}
	// 对齐 G-23 修复：scoreMap 使用 uuid 作为 key
	scoreMap := map[string]float64{
		"1": 0.9,
		"2": 0.5,
		"3": 0.1,
	}

	result := filterByScore(items, scoreMap, 0.3)
	if len(result) != 2 {
		t.Fatalf("filterByScore(minScore=0.3) 应返回2条，实际 %d", len(result))
	}
	// 应按分数降序排列
	if result[0]["content"] != "hello" {
		t.Errorf("filterByScore[0] 应为最高分 'hello'，实际 %v", result[0]["content"])
	}
}

func TestFilterByScore_无匹配(t *testing.T) {
	items := []map[string]any{
		{"content": "hello"},
	}
	scoreMap := map[string]float64{
		"other": 0.9,
	}

	result := filterByScore(items, scoreMap, 0.0)
	if len(result) != 0 {
		t.Errorf("filterByScore 无匹配应返回0条，实际 %d", len(result))
	}
}

func TestFilterByScore_全低于阈值(t *testing.T) {
	items := []map[string]any{
		{"content": "hello"},
	}
	scoreMap := map[string]float64{
		"hello": 0.2,
	}

	result := filterByScore(items, scoreMap, 0.5)
	if len(result) != 0 {
		t.Errorf("filterByScore 全低于阈值应返回0条，实际 %d", len(result))
	}
}

func TestBuildSearchFilterExpr_按ID(t *testing.T) {
	o := graph.Options{IDs: []any{"abc", "def"}}
	expr := buildSearchFilterExpr(o)
	if expr != `uuid in ["abc", "def"]` {
		t.Errorf("buildSearchFilterExpr(IDs) = %q", expr)
	}
}

func TestBuildSearchFilterExpr_空选项(t *testing.T) {
	o := graph.Options{}
	expr := buildSearchFilterExpr(o)
	if expr != "" {
		t.Errorf("buildSearchFilterExpr(空) 应返回空字符串，实际 %q", expr)
	}
}

func TestEnsureSuffix(t *testing.T) {
	if ensureSuffix("hello", "o") != "hello" {
		t.Error("已有后缀不应重复添加")
	}
	if ensureSuffix("hell", "o") != "hello" {
		t.Error("无后缀应添加")
	}
}

func TestGraphSearcher_SearchSingle_无BFS(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	result, err := s.searchSingle(ctx, "test", CollectionEntity, graph.Options{})
	if err != nil {
		t.Fatalf("searchSingle() error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("searchSingle 应返回1条，实际 %d", len(result))
	}
}

func TestGraphSearcher_SearchSingle_有BFS(t *testing.T) {
	fake := newFakeSearcherClient()
	// 第一轮搜索结果
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	// BFS 扩展查询结果
	relationCol := column.NewColumnVarCharArray("relations", [][]string{{"r1", "r2"}})
	episodeCol := column.NewColumnVarCharArray("episodes", [][]string{{"e1"}})
	bfsUUIDCol := column.NewColumnVarChar("uuid", []string{"id1"})
	fake.queryResult = milvusclient.ResultSet{
		ResultCount: 1,
		Fields:      []column.Column{bfsUUIDCol, relationCol, episodeCol},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	result, err := s.searchSingle(ctx, "test", CollectionEntity, graph.Options{BFSDepth: 1, BFSK: 5})
	if err != nil {
		t.Fatalf("searchSingle() error = %v", err)
	}
	_ = result
}

func TestGraphSearcher_SearchSingle_BFS_Relation(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	// BFS 扩展关系查询结果
	lhsCol := column.NewColumnVarChar("lhs", []string{"e1"})
	rhsCol := column.NewColumnVarChar("rhs", []string{"e2"})
	bfsUUIDCol := column.NewColumnVarChar("uuid", []string{"id1"})
	fake.queryResult = milvusclient.ResultSet{
		ResultCount: 1,
		Fields:      []column.Column{bfsUUIDCol, lhsCol, rhsCol},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	result, err := s.searchSingle(ctx, "test", CollectionRelation, graph.Options{BFSDepth: 1, BFSK: 5})
	if err != nil {
		t.Fatalf("searchSingle() error = %v", err)
	}
	_ = result
}

func TestGraphSearcher_SearchSingle_BFS搜索失败返回空结果(t *testing.T) {
	fake := newFakeSearcherClient()
	fake.searchErr = fmt.Errorf("search error")

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	result, err := s.searchSingle(ctx, "test", CollectionEntity, graph.Options{BFSDepth: 1, BFSK: 5})
	// BFS 搜索失败时 break 返回空结果，不报错（对齐 Python 尽力而为行为）
	if err != nil {
		t.Fatalf("searchSingle BFS 搜索失败不应返回错误，实际: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("searchSingle BFS 搜索失败应返回空结果，实际 %d 条", len(result))
	}
}

func TestGraphSearcher_ExpandEntities(t *testing.T) {
	fake := newFakeSearcherClient()
	// 对齐 Python: _expand_entities 查询 Relation 集合的 lhs/rhs 字段
	lhsCol := column.NewColumnVarChar("lhs", []string{"e1"})
	rhsCol := column.NewColumnVarChar("rhs", []string{"e2"})
	fake.queryResult = milvusclient.ResultSet{
		ResultCount: 1,
		Fields:      []column.Column{lhsCol, rhsCol},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	uuidSet := map[string]struct{}{"id1": {}}
	expanded, err := s.expandEntities(ctx, uuidSet, nil)
	if err != nil {
		t.Fatalf("expandEntities() error = %v", err)
	}
	// lhs 和 rhs 中的 UUID 应被扩展
	if len(expanded) != 2 {
		t.Errorf("expandEntities 应扩展2个UUID，实际 %d", len(expanded))
	}
	if _, ok := expanded["e1"]; !ok {
		t.Error("expandEntities 应包含 e1")
	}
	if _, ok := expanded["e2"]; !ok {
		t.Error("expandEntities 应包含 e2")
	}
}

func TestGraphSearcher_ExpandEntities_空集合(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	expanded, err := s.expandEntities(ctx, nil, nil)
	if err != nil {
		t.Fatalf("expandEntities() error = %v", err)
	}
	if expanded != nil {
		t.Error("expandEntities 空集合应返回 nil")
	}
}

func TestGraphSearcher_ExpandEntities_Query失败(t *testing.T) {
	fake := newFakeSearcherClient()
	fake.queryErr = fmt.Errorf("query error")
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	uuidSet := map[string]struct{}{"id1": {}}
	_, err := s.expandEntities(ctx, uuidSet, nil)
	if err == nil {
		t.Error("expandEntities Query 失败应返回错误")
	}
}

func TestGraphSearcher_ExpandRelations(t *testing.T) {
	fake := newFakeSearcherClient()
	// 对齐 Python: _expand_relations 查询 Entity 集合的 relations 字段
	relationCol := column.NewColumnVarCharArray("relations", [][]string{{"r1", "r2"}})
	uuidCol := column.NewColumnVarChar("uuid", []string{"e1"})
	fake.queryResult = milvusclient.ResultSet{
		ResultCount: 1,
		Fields:      []column.Column{uuidCol, relationCol},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	uuidSet := map[string]struct{}{"id1": {}}
	lookup := map[string]map[string]any{
		"id1": {"uuid": "id1", "lhs": "e1", "rhs": "e2"},
	}
	expanded, err := s.expandRelations(ctx, uuidSet, lookup, nil)
	if err != nil {
		t.Fatalf("expandRelations() error = %v", err)
	}
	if len(expanded) != 2 {
		t.Errorf("expandRelations 应扩展2个UUID，实际 %d", len(expanded))
	}
	if _, ok := expanded["r1"]; !ok {
		t.Error("expandRelations 应包含 r1")
	}
	if _, ok := expanded["r2"]; !ok {
		t.Error("expandRelations 应包含 r2")
	}
}

func TestGraphSearcher_ExpandRelations_空集合(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	expanded, err := s.expandRelations(ctx, nil, nil, nil)
	if err != nil {
		t.Fatalf("expandRelations() error = %v", err)
	}
	if expanded != nil {
		t.Error("expandRelations 空集合应返回 nil")
	}
}

func TestGraphSearcher_ExpandRelations_Query失败(t *testing.T) {
	fake := newFakeSearcherClient()
	fake.queryErr = fmt.Errorf("query error")
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	uuidSet := map[string]struct{}{"id1": {}}
	lookup := map[string]map[string]any{
		"id1": {"uuid": "id1", "lhs": "e1", "rhs": "e2"},
	}
	_, err := s.expandRelations(ctx, uuidSet, lookup, nil)
	if err == nil {
		t.Error("expandRelations Query 失败应返回错误")
	}
}

func TestGraphSearcher_SearchAll_搜索失败继续(t *testing.T) {
	fake := newFakeSearcherClient()
	fake.searchErr = fmt.Errorf("search error")

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	results, err := s.searchAll(ctx, "test", graph.Options{})
	if err != nil {
		t.Fatalf("searchAll() error = %v", err)
	}
	// 搜索失败应初始化空切片，对齐 Python: output_dict[col] = []
	if len(results) != 3 {
		t.Errorf("searchAll 搜索失败应返回3个集合（含空切片），实际 %d", len(results))
	}
	for coll, items := range results {
		if len(items) != 0 {
			t.Errorf("集合 %s 搜索失败应为空切片，实际 %d 条", coll, len(items))
		}
	}
}

func TestGraphSearcher_Search_K默认值(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	result, err := s.searchSingle(ctx, "test", CollectionEntity, graph.Options{K: 0})
	if err != nil {
		t.Fatalf("searchSingle() error = %v", err)
	}
	_ = result
}

func TestGraphSearcher_Search_BFSK默认值(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	// BFSK=0 应使用默认值 5
	result, err := s.searchSingle(ctx, "test", CollectionEntity, graph.Options{BFSDepth: 1, BFSK: 0})
	if err != nil {
		t.Fatalf("searchSingle() error = %v", err)
	}
	_ = result
}

func TestBuildReranker_未知类型(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	// 使用一个未知的 BaseRankConfig 实现
	unknownCfg := &fakeRankConfig{}
	reranker, err := s.buildReranker(unknownCfg, CollectionEntity, 3)
	if err != nil {
		t.Fatalf("buildReranker() error = %v", err)
	}
	if reranker == nil {
		t.Error("buildReranker 未知类型应返回默认 RRF Reranker")
	}
}

// fakeRankConfig 用于测试的未知 RankConfig
type fakeRankConfig struct{}

func (f *fakeRankConfig) Name() string                  { return "fake" }
func (f *fakeRankConfig) HigherIsBetter() bool          { return true }
func (f *fakeRankConfig) IsActive() [3]int              { return [3]int{1, 1, 1} }
func (f *fakeRankConfig) Args() ([]any, map[string]any) { return nil, nil }

func TestBuildReranker_Weighted_通道不匹配(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	cfg := graph.NewWeightedRankConfig()
	// Entity 有3个通道但传入2个通道数，应自动均衡
	reranker, err := s.buildReranker(cfg, CollectionEntity, 2)
	if err != nil {
		t.Fatalf("buildReranker() error = %v", err)
	}
	if reranker == nil {
		t.Error("buildReranker 通道不匹配应自动均衡")
	}
}

func TestGraphSearcher_RawHybridSearch_WithFilterExpr(t *testing.T) {
	fake := newFakeSearcherClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{ResultCount: 1, Fields: []column.Column{uuidCol, contentCol}},
	}

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	o := graph.Options{FilterExpr: &testQueryExpr{expr: "user_id == 'test'"}}
	results, err := s.rawHybridSearch(ctx, "test", CollectionEntity, 5, o)
	if err != nil {
		t.Fatalf("rawHybridSearch() error = %v", err)
	}
	_ = results
}

func TestGraphSearcher_RawHybridSearch_FilterExpr失败(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	o := graph.Options{FilterExpr: &errorQueryExpr{}}
	_, err := s.rawHybridSearch(ctx, "test", CollectionEntity, 5, o)
	if err == nil {
		t.Error("rawHybridSearch FilterExpr 失败应返回错误")
	}
}

func TestGraphSearcher_Search_搜索失败(t *testing.T) {
	fake := newFakeSearcherClient()
	fake.searchErr = fmt.Errorf("search error")

	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, &fakeGraphSearchEmbedder{emb: []float64{0.1, 0.2, 0.3}}, indexCfg, graph.GlobalRankerRegistry, "cosine")

	ctx := context.Background()
	_, err := s.search(ctx, "test query", graph.WithCollection(CollectionEntity))
	if err == nil {
		t.Error("search 搜索失败应返回错误")
	}
}

func TestNewGraphSearcher(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")
	if s == nil {
		t.Fatal("newGraphSearcher() 返回 nil")
	}
	if s.metric != "cosine" {
		t.Errorf("metric = %q, want cosine", s.metric)
	}
}

// fakeReranker 用于测试的 BaseReranker 模拟
type fakeReranker struct {
	scoreMap map[string]float64
	err      error
}

func (f *fakeReranker) Rerank(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.scoreMap, nil
}

func (f *fakeReranker) RerankSync(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.scoreMap, nil
}

func (f *fakeReranker) RerankDocs(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.scoreMap, nil
}

func (f *fakeReranker) RerankDocsSync(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.scoreMap, nil
}

func TestGraphSearcher_CombinedRerank_基本(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	// 对齐 G-23 修复：scoreMap 使用 uuid 作为 key
	reranker := &fakeReranker{
		scoreMap: map[string]float64{
			"1": 0.9,
			"2": 0.5,
		},
	}

	results := map[string][]map[string]any{
		CollectionEntity: {
			{"uuid": "1", "content": "hello"},
			{"uuid": "2", "content": "world"},
		},
		CollectionRelation: {
			{"uuid": "3", "content": "hello"},
		},
	}

	ctx := context.Background()
	reranked, err := s.combinedRerank(ctx, "test", results, graph.Options{Reranker: reranker})
	if err != nil {
		t.Fatalf("combinedRerank() error = %v", err)
	}
	if len(reranked) != 2 {
		t.Errorf("combinedRerank 应返回2个集合，实际 %d", len(reranked))
	}
}

func TestGraphSearcher_CombinedRerank_无Reranker(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	results := map[string][]map[string]any{
		CollectionEntity: {
			{"uuid": "1", "content": "hello"},
		},
	}

	ctx := context.Background()
	reranked, err := s.combinedRerank(ctx, "test", results, graph.Options{Reranker: nil})
	if err != nil {
		t.Fatalf("combinedRerank() error = %v", err)
	}
	// 无 Reranker 应直接返回原结果
	if len(reranked[CollectionEntity]) != 1 {
		t.Error("combinedRerank 无 Reranker 应返回原结果")
	}
}

func TestGraphSearcher_CombinedRerank_空结果(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	reranker := &fakeReranker{
		scoreMap: map[string]float64{},
	}

	results := map[string][]map[string]any{
		CollectionEntity:   {},
		CollectionRelation: {},
	}

	ctx := context.Background()
	reranked, err := s.combinedRerank(ctx, "test", results, graph.Options{Reranker: reranker})
	if err != nil {
		t.Fatalf("combinedRerank() error = %v", err)
	}
	// 空结果应直接通过
	_ = reranked
}

func TestGraphSearcher_CombinedRerank_Rerank失败(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	reranker := &fakeReranker{
		err: fmt.Errorf("rerank error"),
	}

	results := map[string][]map[string]any{
		CollectionEntity: {
			{"uuid": "1", "content": "hello"},
		},
	}

	ctx := context.Background()
	reranked, err := s.combinedRerank(ctx, "test", results, graph.Options{Reranker: reranker})
	if err != nil {
		t.Fatalf("combinedRerank() Rerank 失败应使用原始顺序, error = %v", err)
	}
	// Rerank 失败应使用原始顺序
	if len(reranked[CollectionEntity]) != 1 {
		t.Error("combinedRerank Rerank 失败应保留原始结果")
	}
}

func TestGraphSearcher_CombinedRerank_带MinScore(t *testing.T) {
	fake := newFakeSearcherClient()
	indexCfg := graph.NewDefaultIndexConfig()
	s := newGraphSearcher(fake, nil, indexCfg, graph.GlobalRankerRegistry, "cosine")

	// 对齐 G-23 修复：scoreMap 使用 uuid 作为 key
	reranker := &fakeReranker{
		scoreMap: map[string]float64{
			"1": 0.9,
			"2": 0.3,
		},
	}

	results := map[string][]map[string]any{
		CollectionEntity: {
			{"uuid": "1", "content": "hello"},
			{"uuid": "2", "content": "world"},
		},
	}

	ctx := context.Background()
	reranked, err := s.combinedRerank(ctx, "test", results, graph.Options{Reranker: reranker, MinScore: 0.5})
	if err != nil {
		t.Fatalf("combinedRerank() error = %v", err)
	}
	// MinScore=0.5 应过滤掉 world(0.3)
	if len(reranked[CollectionEntity]) != 1 {
		t.Errorf("combinedRerank MinScore=0.5 应返回1条，实际 %d", len(reranked[CollectionEntity]))
	}
}

func TestBuildSearchFilterExpr_按FilterExpr(t *testing.T) {
	o := graph.Options{FilterExpr: &testQueryExpr{expr: "user_id == 'test'"}}
	expr := buildSearchFilterExpr(o)
	if expr != "user_id == 'test'" {
		t.Errorf("buildSearchFilterExpr(FilterExpr) = %q", expr)
	}
}

func TestBuildSearchFilterExpr_FilterExpr失败(t *testing.T) {
	o := graph.Options{FilterExpr: &errorQueryExpr{}}
	expr := buildSearchFilterExpr(o)
	if expr != "" {
		t.Errorf("buildSearchFilterExpr(FilterExpr失败) 应返回空字符串，实际 %q", expr)
	}
}
