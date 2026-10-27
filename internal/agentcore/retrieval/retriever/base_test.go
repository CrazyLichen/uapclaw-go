package retriever

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// TestNewRetrieveOptions_空参数 空参数构建零值选项（Alpha nil=未设）
func TestNewRetrieveOptions_空参数(t *testing.T) {
	o := NewRetrieveOptions()
	assert.Nil(t, o.Alpha)
}

// TestWithRetrieveAlpha_设置 13.21 平铺字段：设置后非 nil、值正确
func TestWithRetrieveAlpha_设置(t *testing.T) {
	o := NewRetrieveOptions(WithRetrieveAlpha(0.8))
	require.NotNil(t, o.Alpha)
	assert.Equal(t, 0.8, *o.Alpha)
}

// TestWithRetrieveAlpha_显式零 显式 0 是合法纯稀疏语义，与未设（nil）区分
func TestWithRetrieveAlpha_显式零(t *testing.T) {
	o := NewRetrieveOptions(WithRetrieveAlpha(0))
	require.NotNil(t, o.Alpha)
	assert.Equal(t, 0.0, *o.Alpha)
}

// TestRetrievalMode_常量值 枚举常量对齐 Python 字面量
func TestRetrievalMode_常量值(t *testing.T) {
	assert.Equal(t, RetrievalMode("vector"), RetrievalModeVector)
	assert.Equal(t, RetrievalMode("sparse"), RetrievalModeSparse)
	assert.Equal(t, RetrievalMode("hybrid"), RetrievalModeHybrid)
}

// fakeVectorStore 实现 vector_store.VectorStore 全接口的测试桩。
//
// 检索三方法可编程返回值/错误并计数；其余方法零实现（两检索器不触达）。
// searchErrAfter > 0 时前 N 次 Search 成功、之后返回 searchErr（G5 首错
// 传播用：q1 成功 q2 失败）。
// mu 保护计数/捕获字段：BatchRetrieve 裸 errgroup 并发打桩时多 goroutine
// 并发写（-race 实测抓到，13.17 dispatchTripleClient 先例）。测试断言在
// g.Wait() 之后读，WaitGroup happens-before 保证无需加锁读。
type fakeVectorStore struct {
	searchResult   []common.SearchResult
	sparseResult   []common.SearchResult
	searchErr      error
	sparseErr      error
	searchErrAfter int
	searchCalls    int
	sparseCalls    int
	closeCalls     int
	lastTopK       int
	lastQueryVec   []float64
	lastQueryText  string
	// hybridResult/hybridErr/lastAlpha/lastHybridVec/hybridCalls 为 13.21
	// HybridSearch 可编程化桩（mu 保护，BatchRetrieve 并发打桩先例）
	hybridResult  []common.SearchResult
	hybridErr     error
	hybridCalls   int
	lastAlpha     float64   // alpha 覆写断言用（Python call_args 对应物）
	lastHybridVec []float64 // embed nil 纯稀疏路径断言用（区别于 search 的 lastQueryVec）
	mu            sync.Mutex
}

func (f *fakeVectorStore) CheckVectorField() error { return nil }
func (f *fakeVectorStore) Add(_ context.Context, _ []map[string]any, _ ...vector_store.StoreOption) error {
	return nil
}
func (f *fakeVectorStore) Search(_ context.Context, queryVector []float64, topK int, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]common.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searchCalls++
	f.lastTopK = topK
	f.lastQueryVec = queryVector
	if f.searchErr != nil && (f.searchErrAfter == 0 || f.searchCalls > f.searchErrAfter) {
		return nil, f.searchErr
	}
	return f.searchResult, f.searchErr
}
func (f *fakeVectorStore) SparseSearch(_ context.Context, queryText string, topK int, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]common.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sparseCalls++
	f.lastTopK = topK
	f.lastQueryText = queryText
	return f.sparseResult, f.sparseErr
}
func (f *fakeVectorStore) HybridSearch(_ context.Context, queryText string, queryVector []float64, _ int, alpha float64, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]common.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hybridCalls++
	f.lastQueryText = queryText
	f.lastHybridVec = queryVector
	f.lastAlpha = alpha
	return f.hybridResult, f.hybridErr
}
func (f *fakeVectorStore) Delete(_ context.Context, _ []string, _ query.QueryExpr) (bool, error) {
	return false, nil
}
func (f *fakeVectorStore) TableExists(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeVectorStore) DeleteTable(_ context.Context, _ string) error { return nil }
func (f *fakeVectorStore) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
}

// fakeEmbedModel 实现 embedding.BaseEmbedding 的测试桩。
// mu 保护 embedQueryCalls：批量并发打桩场景（同上）。
type fakeEmbedModel struct {
	queryVec        []float64
	err             error
	embedQueryCalls int
	mu              sync.Mutex
}

func (f *fakeEmbedModel) EmbedQuery(_ context.Context, _ string, _ ...embedding.EmbedOption) ([]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.embedQueryCalls++
	return f.queryVec, f.err
}
func (f *fakeEmbedModel) EmbedDocuments(_ context.Context, _ []string, _ ...embedding.EmbedOption) ([][]float64, error) {
	return nil, nil
}
func (f *fakeEmbedModel) Dimension() int                                    { return 384 }
func (f *fakeEmbedModel) DimensionWithContext(context.Context) (int, error) { return 384, nil }
