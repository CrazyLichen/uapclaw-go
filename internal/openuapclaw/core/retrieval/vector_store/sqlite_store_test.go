package vector_store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/query"
	retrievalcommon "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
)

// newTestSQLiteStore 构造测试用 sqlite store（TempDir 持久化，vec0 依赖 third_party 扩展）。
func newTestSQLiteStore(t *testing.T, collection string) *SQLiteVectorStore {
	t.Helper()
	chdirRepoRoot(t)
	dir := t.TempDir()
	cfg := retrievalcommon.VectorStoreConfig{
		StoreProvider:  retrievalcommon.StoreTypeSqlite,
		DatabaseName:   "test_db",
		CollectionName: collection,
		DistanceMetric: retrievalcommon.DistanceMetricEuclidean,
	}
	s, err := NewSQLiteVectorStore(cfg, filepath.Join(dir, "test.db"), "", "", "", "", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// chdirRepoRoot 把测试进程 cwd 切到仓库根（vec0.so 相对路径 third_party/... 按.cwd 解析；
// ResolveVec0Path 的可执行文件回落对 go test 临时二进制目录无效）。
// 本包测试默认串行（未用 t.Parallel），进程级 chdir 安全。
func chdirRepoRoot(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join("third_party", "sqlite-vec")); err == nil {
		return // 已在仓库根
	}
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "third_party", "sqlite-vec")); err == nil {
			require.NoError(t, os.Chdir(dir))
			return
		}
		dir = filepath.Dir(dir)
	}
	t.Skipf("third_party/sqlite-vec 不存在，vec0 依赖用例跳过")
}

func TestSQLiteStore_构造_缺collection名报错(t *testing.T) {	cfg := retrievalcommon.VectorStoreConfig{StoreProvider: retrievalcommon.StoreTypeSqlite}
	_, err := NewSQLiteVectorStore(cfg, t.TempDir(), "", "", "", "", "")
	require.Error(t, err)
}

func TestSQLiteStore_构造_不支持度量报错(t *testing.T) {
	cfg := retrievalcommon.VectorStoreConfig{
		StoreProvider:  retrievalcommon.StoreTypeSqlite,
		CollectionName: "c1",
		DistanceMetric: retrievalcommon.DistanceMetricKind("dot"), // sqlite 仅支持 l2/cosine
	}
	_, err := NewSQLiteVectorStore(cfg, t.TempDir(), "", "", "", "", "")
	require.Error(t, err)
}

func TestSQLiteStore_Add向量后Search命中排序(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_search")
	ctx := context.Background()
	// 三条文档：query 与 d1 最近（[1,0,0] vs [1,0,0]）
	docs := []map[string]any{
		{"id": "d1", "content": "gateway deploy guide", "embedding": []float64{1, 0, 0},
			"metadata": map[string]any{"doc_id": "doc-1"}},
		{"id": "d2", "content": "mysql charset config", "embedding": []float64{0, 1, 0}},
		{"id": "d3", "content": "timeout default", "embedding": []float64{0.9, 0.1, 0}},
	}
	require.NoError(t, s.Add(ctx, docs))

	results, err := s.Search(ctx, []float64{1, 0, 0}, 2, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "d1", results[0].ID) // 最近邻排第一
	assert.Equal(t, "gateway deploy guide", results[0].Text)
	assert.True(t, results[0].Score > results[1].Score, "l2 换算后分数越大越相关")
	// doc_id 键存在性语义（M-20 对齐）
	_, hasDocID := results[0].Metadata["doc_id"]
	assert.True(t, hasDocID)
}

func TestSQLiteStore_SparseSearch_bm25词面命中(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_fts")
	ctx := context.Background()
	docs := []map[string]any{
		{"id": "d1", "content": "gateway timeout configuration"},
		{"id": "d2", "content": "mysql charset utf8mb4 setup"},
	}
	require.NoError(t, s.Add(ctx, docs))

	// 无向量写入：FTS 仍可用（bm25-only 场景）
	results, err := s.SparseSearch(ctx, "timeout configuration", 2, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "d1", results[0].ID)
	assert.True(t, results[0].Score > 0, "-bm25 归一后应大于 0")
}

func TestSQLiteStore_SparseSearch_无命中返回空(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_fts_empty")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{{"id": "d1", "content": "alpha beta"}}))
	results, err := s.SparseSearch(ctx, "zeta", 3, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSQLiteStore_HybridSearch_两路融合(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_hybrid")
	ctx := context.Background()
	docs := []map[string]any{
		{"id": "d1", "content": "gateway timeout configuration", "embedding": []float64{1, 0, 0}},
		{"id": "d2", "content": "mysql charset utf8mb4 setup", "embedding": []float64{0, 1, 0}},
	}
	require.NoError(t, s.Add(ctx, docs))

	results, err := s.HybridSearch(ctx, "timeout", []float64{1, 0, 0}, 2, 0.5, nil)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	// d1 两路都命中 → RRF 融合分最高排第一
	assert.Equal(t, "d1", results[0].ID)
	assert.NotEmpty(t, results[0].ID)
}

func TestSQLiteStore_HybridSearch_vec不可用时纯FTS兜底(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_hybrid_degraded")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{{"id": "d1", "content": "gateway timeout"}}))
	// 传 nil 向量 → 仅 FTS 路
	results, err := s.HybridSearch(ctx, "timeout", nil, 2, 0.5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "d1", results[0].ID)
}

func TestSQLiteStore_无向量写入仅进FTS(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_no_vec")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{{"id": "d1", "content": "plain text only"}}))
	// 空 store（无 vec 表）→ Search 返回空结果不报错
	results, err := s.Search(ctx, []float64{1, 0, 0}, 2, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSQLiteStore_维度不匹配报错(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_dims")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{{"id": "d1", "content": "x", "embedding": []float64{1, 2, 3}}}))
	_, err := s.Search(ctx, []float64{1, 2}, 1, nil)
	require.Error(t, err)
	err = s.Add(ctx, []map[string]any{{"id": "d2", "content": "y", "embedding": []float64{1}}})
	require.Error(t, err)
}

func TestSQLiteStore_Delete连带清理(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_delete")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{
		{"id": "d1", "content": "alpha", "embedding": []float64{1, 0, 0}},
		{"id": "d2", "content": "beta", "embedding": []float64{0, 1, 0}},
	}))
	deleted, err := s.Delete(ctx, []string{"d1"}, nil)
	require.NoError(t, err)
	assert.True(t, deleted)

	// 向量检索不再命中 d1；FTS 也不再命中
	results, err := s.Search(ctx, []float64{1, 0, 0}, 2, nil)
	require.NoError(t, err)
	for _, r := range results {
		assert.NotEqual(t, "d1", r.ID)
	}
	ftsResults, err := s.SparseSearch(ctx, "alpha", 2, nil)
	require.NoError(t, err)
	assert.Empty(t, ftsResults)
}

func TestSQLiteStore_TableExists与DeleteTable(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_exists")
	ctx := context.Background()
	exists, err := s.TableExists(ctx, "kb_exists")
	require.NoError(t, err)
	assert.True(t, exists)

	require.NoError(t, s.DeleteTable(ctx, "kb_exists"))
	exists, err = s.TableExists(ctx, "kb_exists")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSQLiteStore_EqMap元数据过滤(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_filter")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{
		{"id": "d1", "content": "alpha", "embedding": []float64{1, 0, 0}, "metadata": map[string]any{"tenant": "a"}},
		{"id": "d2", "content": "beta", "embedding": []float64{0, 1, 0}, "metadata": map[string]any{"tenant": "b"}},
	}))
	filters := query.EqMap(map[string]any{"tenant": "b"})
	results, err := s.Search(ctx, []float64{1, 0, 0}, 2, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "d2", results[0].ID)
}

func TestSQLiteStore_访问器7属性(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_attrs")
	assert.Equal(t, "test_db", s.DatabaseName())
	assert.Equal(t, "l2", s.DistanceMetric())
	assert.Equal(t, "content", s.TextField())
	assert.Equal(t, "embedding", s.VectorField())
	assert.Equal(t, "sparse_vector", s.SparseVectorField())
	assert.Equal(t, "metadata", s.MetadataField())
	assert.Equal(t, "document_id", s.DocIDField())
	assert.Equal(t, "kb_attrs", s.CollectionName())
	s.SetCollectionName("kb_attrs2")
	assert.Equal(t, "kb_attrs2", s.CollectionName())
}

func TestSQLiteStore_重复id覆写UPSERT(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_upsert")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{{"id": "d1", "content": "v1", "embedding": []float64{1, 0, 0}}}))
	require.NoError(t, s.Add(ctx, []map[string]any{{"id": "d1", "content": "v2", "embedding": []float64{0, 1, 0}}}))
	results, err := s.SparseSearch(ctx, "v2", 2, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "d1", results[0].ID)
	// FTS 旧文本不再命中
	old, err := s.SparseSearch(ctx, "v1", 2, nil)
	require.NoError(t, err)
	assert.Empty(t, old)
}

func TestSQLiteStore_BuildFTSMatchQuery_转义(t *testing.T) {
	assert.Equal(t, "\"a\" OR \"b\"", buildFTSMatchQuery("a b"))
	assert.Equal(t, "\"x\"\"y\"", buildFTSMatchQuery("x\"y"))
	assert.Empty(t, buildFTSMatchQuery("   "))
}

func TestSQLiteStore_CheckVectorField_有表无表两态(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_checkvec")
	ctx := context.Background()
	// 无 vec 表：惰性建表前返回 nil
	require.NoError(t, s.CheckVectorField())
	// 建表后（有向量写入）仍 nil
	require.NoError(t, s.Add(ctx, []map[string]any{{"id": "d1", "content": "x", "embedding": []float64{1, 0, 0}}}))
	require.NoError(t, s.CheckVectorField())
}

func TestSQLiteStore_Add_顶层docID与chunkID键(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_topkeys")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{{
		"id": "d1", "content": "alpha", "embedding": []float64{1, 0, 0},
		"document_id": "doc-9", "chunk_id": "ck-1", "metadata": map[string]any{"k": "v"},
	}}))
	results, err := s.Search(ctx, []float64{1, 0, 0}, 1, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	// document_id → doc_id 键映射（chroma 同款读侧重命名）
	assert.Equal(t, "doc-9", results[0].Metadata["doc_id"])
	assert.Equal(t, "ck-1", results[0].Metadata["chunk_id"])
	assert.Equal(t, "v", results[0].Metadata["k"])
}

func TestSQLiteStore_Delete_空id列表忽略(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_del_empty")
	deleted, err := s.Delete(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestSQLiteStore_默认DatabaseName与cosine度量(t *testing.T) {
	dir := t.TempDir()
	cfg := retrievalcommon.VectorStoreConfig{
		StoreProvider:  retrievalcommon.StoreTypeSqlite,
		CollectionName: "kb_cos",
		DistanceMetric: retrievalcommon.DistanceMetricCosine,
	}
	s, err := NewSQLiteVectorStore(cfg, filepath.Join(dir, "c.db"), "", "", "", "", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	// DatabaseName 空配置 → 默认 "sqlite"
	assert.Equal(t, "sqlite", s.DatabaseName())
	assert.Equal(t, "cosine", s.DistanceMetric())
	require.NoError(t, s.Add(context.Background(), []map[string]any{
		{"id": "d1", "content": "alpha", "embedding": []float64{1, 0, 0}},
	}))
	results, err := s.Search(context.Background(), []float64{1, 0, 0}, 1, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Score >= 0 && results[0].Score <= 1.5, "cosine 换算分数应在相似度区间")
}

func TestSQLiteStore_重复Close幂等(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_close2")
	require.NoError(t, s.Close())
	assert.NoError(t, s.Close()) // 幂等
}

func TestSQLiteStore_Eq条件树含Or时忽略非与链(t *testing.T) {
	// Or 链不提取（只走 And 链）——条件为空 → 不过滤原样返回
	want := map[string]any{}
	extractEqConditions(query.Or(query.Eq("a", 1), query.Eq("b", 2)), want)
	assert.Empty(t, want)
}

func TestSQLiteStore_元数据字符串形态与appendMetaField(t *testing.T) {
	s := newTestSQLiteStore(t, "kb_metajson")
	ctx := context.Background()
	require.NoError(t, s.Add(ctx, []map[string]any{{
		"id": "d1", "content": "alpha", "embedding": []float64{1, 0, 0},
		"metadata": `{"k":"v"}`,
	}}))
	results, err := s.Search(ctx, []float64{1, 0, 0}, 1, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "v", results[0].Metadata["k"])
}
