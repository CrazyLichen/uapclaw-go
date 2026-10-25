package retriever

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// TestNewRetrieveOptions_空参数 空参数构建零值选项
func TestNewRetrieveOptions_空参数(t *testing.T) {
	o := NewRetrieveOptions()
	assert.Nil(t, o.Extra)
}

// TestWithRetrieveExtra_单键 Extra 单键写入
func TestWithRetrieveExtra_单键(t *testing.T) {
	o := NewRetrieveOptions(WithRetrieveExtra("alpha", 0.8))
	assert.Equal(t, map[string]any{"alpha": 0.8}, o.Extra)
}

// TestWithRetrieveExtra_多选项 多选项合并（惰性建 map）
func TestWithRetrieveExtra_多选项(t *testing.T) {
	o := NewRetrieveOptions(
		WithRetrieveExtra("alpha", 0.5),
		WithRetrieveExtra("filters", map[string]any{"doc_id": "d1"}),
	)
	assert.Equal(t, 0.5, o.Extra["alpha"])
	assert.Equal(t, map[string]any{"doc_id": "d1"}, o.Extra["filters"])
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
}

func (f *fakeVectorStore) CheckVectorField() error { return nil }
func (f *fakeVectorStore) Add(_ context.Context, _ []map[string]any, _ ...vector_store.StoreOption) error {
	return nil
}
func (f *fakeVectorStore) Search(_ context.Context, queryVector []float64, topK int, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]common.SearchResult, error) {
	f.searchCalls++
	f.lastTopK = topK
	f.lastQueryVec = queryVector
	if f.searchErr != nil && (f.searchErrAfter == 0 || f.searchCalls > f.searchErrAfter) {
		return nil, f.searchErr
	}
	return f.searchResult, f.searchErr
}
func (f *fakeVectorStore) SparseSearch(_ context.Context, queryText string, topK int, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]common.SearchResult, error) {
	f.sparseCalls++
	f.lastTopK = topK
	f.lastQueryText = queryText
	return f.sparseResult, f.sparseErr
}
func (f *fakeVectorStore) HybridSearch(_ context.Context, _ string, _ []float64, _ int, _ float64, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *fakeVectorStore) Delete(_ context.Context, _ []string, _ query.QueryExpr) (bool, error) {
	return false, nil
}
func (f *fakeVectorStore) TableExists(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeVectorStore) DeleteTable(_ context.Context, _ string) error { return nil }
func (f *fakeVectorStore) Close()                                        { f.closeCalls++ }

// fakeEmbedModel 实现 embedding.BaseEmbedding 的测试桩。
type fakeEmbedModel struct {
	queryVec        []float64
	err             error
	embedQueryCalls int
}

func (f *fakeEmbedModel) EmbedQuery(_ context.Context, _ string, _ ...embedding.EmbedOption) ([]float64, error) {
	f.embedQueryCalls++
	return f.queryVec, f.err
}
func (f *fakeEmbedModel) EmbedDocuments(_ context.Context, _ []string, _ ...embedding.EmbedOption) ([][]float64, error) {
	return nil, nil
}
func (f *fakeEmbedModel) Dimension() int                                    { return 384 }
func (f *fakeEmbedModel) DimensionWithContext(context.Context) (int, error) { return 384, nil }
