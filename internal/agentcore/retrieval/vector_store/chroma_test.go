package vector_store

import (
	"testing"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// TestChromaVectorField_字符串字段名 测试通过字段名创建 ChromaVectorField
func TestChromaVectorField_字符串字段名(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	dir := t.TempDir()

	// 使用 NewChromaVectorFieldFromName 从字段名创建
	vf := vector_fields.NewChromaVectorFieldFromName("embedding")
	vs, err := NewChromaVectorStore(config, dir,
		"content", vf, "sparse_vector", "metadata", "document_id",
	)

	require.NoError(t, err)
	require.NotNil(t, vs)
	assert.Equal(t, "cosine", vs.DistanceMetric())
	assert.Equal(t, "embedding", vs.vectorField.VectorFieldName)
}

// TestChromaVectorField_ChromaVectorField类型 测试 ChromaVectorField 类型
func TestChromaVectorField_ChromaVectorField类型(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	dir := t.TempDir()
	vf := vector_fields.NewDefaultChromaVectorField()

	vs, err := NewChromaVectorStore(config, dir,
		"content", vf, "sparse_vector", "metadata", "document_id",
	)

	require.NoError(t, err)
	require.NotNil(t, vs)
	assert.Equal(t, "cosine", vs.DistanceMetric())
}

// TestChromaVectorStore_chromaPath为空 测试空路径
func TestChromaVectorStore_chromaPath为空(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	_, err := NewChromaVectorStore(config, "",
		"content", vector_fields.NewChromaVectorFieldFromName("embedding"), "sparse_vector", "metadata", "document_id",
	)

	assert.Error(t, err)
}

// TestChromaVectorStore_vectorField为nil 测试 nil vectorField
func TestChromaVectorStore_vectorField为nil(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	dir := t.TempDir()

	_, err := NewChromaVectorStore(config, dir,
		"content", nil, "sparse_vector", "metadata", "document_id",
	)

	assert.Error(t, err)
}

// TestChromaVectorStore_距离度量归一化 测试 dot→ip, euclidean→l2
func TestChromaVectorStore_距离度量归一化(t *testing.T) {
	tests := []struct {
		input    common.DistanceMetricKind
		expected string
	}{
		{common.DistanceMetricCosine, "cosine"},
		{common.DistanceMetricEuclidean, "l2"},
		{common.DistanceMetricDot, "ip"},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			config := common.VectorStoreConfig{
				StoreProvider:  common.StoreTypeChroma,
				CollectionName: "test-collection",
				DistanceMetric: tt.input,
			}
			dir := t.TempDir()

			vs, err := NewChromaVectorStore(config, dir,
				"content", vector_fields.NewChromaVectorFieldFromName("embedding"), "sparse_vector", "metadata", "document_id",
			)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, vs.DistanceMetric())
		})
	}
}

// TestChromaVectorStore_ConstructConfig 测试构建配置含 space
func TestChromaVectorStore_ConstructConfig(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	dir := t.TempDir()

	vs, err := NewChromaVectorStore(config, dir,
		"content", vector_fields.NewChromaVectorFieldFromName("embedding"), "sparse_vector", "metadata", "document_id",
	)

	require.NoError(t, err)
	assert.Equal(t, "cosine", vs.ConstructConfig()["space"])
}

// TestBuildChromaFilterPair_nil 测试 nil 过滤器（M-27）
func TestBuildChromaFilterPair_nil(t *testing.T) {
	s := newChromaStoreForTest("cosine")
	wf, wdf := s.buildChromaFilterPair(nil)
	assert.Nil(t, wf)
	assert.Nil(t, wdf)
}

// TestBuildChromaFilterPair_EqMap 测试 dict 折叠为 And(Eq...) 链（M-27）
func TestBuildChromaFilterPair_EqMap(t *testing.T) {
	s := newChromaStoreForTest("cosine")
	filters := query.EqMap(map[string]any{
		"document_id": "doc-1",
		"category":    "tech",
	})
	wf, wdf := s.buildChromaFilterPair(filters)
	// 纯元数据等值过滤：where 非空，where_document 为空被过滤
	assert.NotNil(t, wf)
	assert.Nil(t, wdf)

	// 序列化验证 wire 格式：两个等值条件折叠为 $and（EqMap 按 key 排序，
	// category < document_id）
	b, err := wf.MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `{"$and": [{"category": "tech"}, {"document_id": "doc-1"}]}`, string(b))
}

// TestBuildChromaFilterPair_空map 测试 EqMap 空 dict 返回 nil（M-27）
func TestBuildChromaFilterPair_空map(t *testing.T) {
	assert.Nil(t, query.EqMap(map[string]any{}))
	assert.Nil(t, query.EqMap(nil))
}

// TestBuildChromaFilterPair_文本匹配 测试 MatchExpr 只注入 where_document（M-27）
func TestBuildChromaFilterPair_文本匹配(t *testing.T) {
	s := newChromaStoreForTest("cosine")
	// 对齐 Python filters.to_expr("chroma")：MatchExpr 的 where 为空 dict，
	// 只有 where_document 非空——对齐 Python {k: v ... if v} falsy 过滤。
	// Python 端 MatchExpr 无工厂函数，直接构造类（Go 同构直构）
	filters := &query.MatchExpr{Field: "content", Value: "hello", MatchMode: query.MatchModeExact}
	wf, wdf := s.buildChromaFilterPair(filters)
	assert.Nil(t, wf)
	assert.NotNil(t, wdf)

	b, err := wdf.MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `{"$contains": "hello"}`, string(b))
}

// TestWrapChromaWhereMap 测试 dict 直传 where 包装（M-27，indexer 查重路径）
func TestWrapChromaWhereMap(t *testing.T) {
	assert.Nil(t, WrapChromaWhereMap(nil))
	assert.Nil(t, WrapChromaWhereMap(map[string]any{}))

	wf := WrapChromaWhereMap(map[string]any{"document_id": "doc-1"})
	require.NotNil(t, wf)
	b, err := wf.MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `{"document_id": "doc-1"}`, string(b))
}

// TestMapToChromaDistanceMetric 测试距离度量映射
func TestMapToChromaDistanceMetric(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"cosine", "cosine"},
		{"l2", "l2"},
		{"ip", "ip"},
		{"COSINE", "cosine"},
		{"unknown", "cosine"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapToChromaDistanceMetric(tt.input)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

// newChromaStoreForTest 构造测试用 ChromaVectorStore（仅填充结果转换所需字段）。
func newChromaStoreForTest(distanceMetric string) *ChromaVectorStore {
	return &ChromaVectorStore{
		distanceMetric:    distanceMetric,
		sparseVectorField: "sparse_vector",
		docIDField:        "document_id",
	}
}

// buildQueryResult 直构 chromav2.QueryResult（单查询组）。
func buildQueryResult(ids []string, docs []string, dists []float32, meta chromav2.DocumentMetadata) chromav2.QueryResult {
	idList := make(chromav2.DocumentIDs, 0, len(ids))
	for _, id := range ids {
		idList = append(idList, chromav2.DocumentID(id))
	}
	docList := make(chromav2.Documents, 0, len(docs))
	for _, d := range docs {
		docList = append(docList, chromav2.NewTextDocument(d))
	}
	distList := make(embeddings.Distances, 0, len(dists))
	for _, d := range dists {
		distList = append(distList, embeddings.Distance(d))
	}
	result := &chromav2.QueryResultImpl{
		IDLists:        []chromav2.DocumentIDs{idList},
		DocumentsLists: []chromav2.Documents{docList},
		DistancesLists: []embeddings.Distances{distList},
	}
	if meta != nil {
		result.MetadatasLists = []chromav2.DocumentMetadatas{{meta}}
	}
	return result
}

// TestChromaResultToSearchResults_L2归一化 S-19 回归测试：
// l2 度量下 raw_score 必须按 max_dist=4.0 归一化（对齐 Python 默认值），
// 传 0 时 (0-raw)/0 会产生 NaN/归零，导致 l2 得分全部失效。
func TestChromaResultToSearchResults_L2归一化(t *testing.T) {
	// raw_score=1.0 → (4.0-1.0)/4.0 = 0.75
	s := newChromaStoreForTest("l2")
	meta, err := chromav2.NewDocumentMetadataFromMap(map[string]any{"kind": "test"})
	require.NoError(t, err)

	result := buildQueryResult(
		[]string{"id-1"}, []string{"text-1"},
		[]float32{1.0}, meta,
	)
	results := s.chromaResultToSearchResults(result, "vector")

	require.Len(t, results, 1)
	// S-19: 归一化得分 0.75（修复前传 0 会得到 0 或 NaN）
	assert.InDelta(t, 0.75, results[0].Score, 1e-9)
	assert.InDelta(t, 0.75, results[0].Metadata["raw_score_scaled"], 1e-9)
	assert.InDelta(t, 1.0, results[0].Metadata["raw_score"], 1e-9)
}

// TestChromaResultToSearchResults_L2零距离 S-19 边界：raw=0 → 1.0（完美匹配）
func TestChromaResultToSearchResults_L2零距离(t *testing.T) {
	s := newChromaStoreForTest("l2")
	meta, err := chromav2.NewDocumentMetadataFromMap(map[string]any{"kind": "test"})
	require.NoError(t, err)

	result := buildQueryResult(
		[]string{"id-1"}, []string{"text-1"},
		[]float32{0.0}, meta,
	)
	results := s.chromaResultToSearchResults(result, "vector")

	require.Len(t, results, 1)
	assert.InDelta(t, 1.0, results[0].Score, 1e-9)
}

// TestChromaResultToSearchResults_L2超界钳零 S-19 边界：raw=5 > maxDist=4 → 钳 0
func TestChromaResultToSearchResults_L2超界钳零(t *testing.T) {
	s := newChromaStoreForTest("l2")
	meta, err := chromav2.NewDocumentMetadataFromMap(map[string]any{"kind": "test"})
	require.NoError(t, err)

	result := buildQueryResult(
		[]string{"id-1"}, []string{"text-1"},
		[]float32{5.0}, meta,
	)
	results := s.chromaResultToSearchResults(result, "vector")

	require.Len(t, results, 1)
	assert.InDelta(t, 0.0, results[0].Score, 1e-9)
}

// TestChromaResultToSearchResults_docID字段迁移 docIDField 存在时迁移到 doc_id 并删除原键
func TestChromaResultToSearchResults_docID字段迁移(t *testing.T) {
	s := newChromaStoreForTest("cosine")
	meta, err := chromav2.NewDocumentMetadataFromMap(map[string]any{
		"document_id": "doc-42",
	})
	require.NoError(t, err)

	result := buildQueryResult(
		[]string{"id-1"}, []string{"text-1"},
		[]float32{0.5}, meta,
	)
	results := s.chromaResultToSearchResults(result, "vector")

	require.Len(t, results, 1)
	assert.Equal(t, "doc-42", results[0].Metadata["doc_id"])
	_, hasOld := results[0].Metadata["document_id"]
	assert.False(t, hasOld, "迁移后原 document_id 键应被删除")
}

// TestChromaResultToSearchResults_docID缺失设nil M-20 回归测试：
// sparse 模式下 doc_id 与 docIDField 均缺失时，对齐 Python
// metadata.pop(self.doc_id_field, None) 的键存在性语义——必须显式设 doc_id=nil。
func TestChromaResultToSearchResults_docID缺失设nil(t *testing.T) {
	s := newChromaStoreForTest("cosine")
	meta, err := chromav2.NewDocumentMetadataFromMap(map[string]any{"kind": "sparse"})
	require.NoError(t, err)

	result := buildQueryResult(
		[]string{"id-1"}, []string{"text-1"},
		[]float32{0.5}, meta,
	)
	results := s.chromaResultToSearchResults(result, "sparse")

	require.Len(t, results, 1)
	// M-20: 键必须存在且值为 nil（修复前键直接缺失）
	val, ok := results[0].Metadata["doc_id"]
	assert.True(t, ok, "doc_id 键必须存在（对齐 Python pop(key, None) 语义）")
	assert.Nil(t, val)
}

// TestChromaResultToSearchResults_已有docID不覆盖 metadata 已含 doc_id 时不迁移
func TestChromaResultToSearchResults_已有docID不覆盖(t *testing.T) {
	s := newChromaStoreForTest("cosine")
	meta, err := chromav2.NewDocumentMetadataFromMap(map[string]any{
		"doc_id":      "original",
		"document_id": "shadowed",
	})
	require.NoError(t, err)

	result := buildQueryResult(
		[]string{"id-1"}, []string{"text-1"},
		[]float32{0.5}, meta,
	)
	results := s.chromaResultToSearchResults(result, "vector")

	require.Len(t, results, 1)
	assert.Equal(t, "original", results[0].Metadata["doc_id"])
}

// TestBuildChromaHNSWCreateOptions_全量选项 S-18 残留回归测试：
// num_threads/resize_factor 在 SDK v0.4.1 有对应 Create 选项
// （WithHNSWNumThreadsCreate/WithHNSWResizeFactorCreate），配置存在时必须传递。
func TestBuildChromaHNSWCreateOptions_全量选项(t *testing.T) {
	constructConfig := map[string]any{
		"max_neighbors":   32,
		"ef_construction": 200,
		"ef_search":       100,
	}
	searchConfig := map[string]any{
		"num_threads":    4,
		"resize_factor":  1.2,
		"batch_size":     100,
		"sync_threshold": 1000,
	}
	opts := buildChromaHNSWCreateOptions("l2", constructConfig, searchConfig)

	// space(1) + max_neighbors(1) + ef_construction(1) + ef_search(1)
	// + batch_size(1) + sync_threshold(1) + num_threads(1) + resize_factor(1) = 8
	// S-18 修复前只有 6（缺 num_threads/resize_factor）
	assert.Len(t, opts, 8)
}

// TestBuildChromaHNSWCreateOptions_resizeFactor整型 resize_factor 传 int 时的兼容分支
func TestBuildChromaHNSWCreateOptions_resizeFactor整型(t *testing.T) {
	opts := buildChromaHNSWCreateOptions("l2",
		map[string]any{},
		map[string]any{"resize_factor": 2},
	)
	// space(1) + resize_factor(1) = 2
	assert.Len(t, opts, 2)
}

// TestBuildChromaHNSWCreateOptions_缺省仅space 无任何 HNSW 配置时只有 space 选项
func TestBuildChromaHNSWCreateOptions_缺省仅space(t *testing.T) {
	opts := buildChromaHNSWCreateOptions("cosine", map[string]any{}, map[string]any{})
	assert.Len(t, opts, 1)
}
