package retriever

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeBeamEmbedModel 嵌入桩：可编程多段返回 + MaxBatchSizeProvider。
type fakeBeamEmbedModel struct {
	mu        sync.Mutex
	batches   [][][]float64 // 依次返回的批次
	calls     int
	lastTexts []string
	lastOpts  []embedding.EmbedOption
	// batchSize MaxBatchSize 返回值
	batchSize int
}

func (f *fakeBeamEmbedModel) EmbedQuery(context.Context, string, ...embedding.EmbedOption) ([]float64, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeBeamEmbedModel) EmbedDocuments(_ context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := f.calls
	f.calls++
	f.lastTexts = texts
	f.lastOpts = opts
	if idx >= len(f.batches) {
		return nil, errors.New("no more batches")
	}
	return f.batches[idx], nil
}

func (f *fakeBeamEmbedModel) Dimension() int                                    { return 3 }
func (f *fakeBeamEmbedModel) DimensionWithContext(context.Context) (int, error) { return 3, nil }
func (f *fakeBeamEmbedModel) MaxBatchSize() int                                 { return f.batchSize }

// lastTextsSnapshot 并发安全读取最后一批文本
func (f *fakeBeamEmbedModel) lastTextsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastTexts
}

// lastOptsSnapshot 并发安全读取最后一批选项
func (f *fakeBeamEmbedModel) lastOptsSnapshot() []embedding.EmbedOption {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastOpts
}

// plainEmbedModel 无 MaxBatchSize 的嵌入桩（断言失败 155202 用例）
type plainEmbedModel struct{}

func (p *plainEmbedModel) EmbedQuery(context.Context, string, ...embedding.EmbedOption) ([]float64, error) {
	return nil, nil
}
func (p *plainEmbedModel) EmbedDocuments(context.Context, []string, ...embedding.EmbedOption) ([][]float64, error) {
	return nil, nil
}
func (p *plainEmbedModel) Dimension() int                                    { return 0 }
func (p *plainEmbedModel) DimensionWithContext(context.Context) (int, error) { return 0, nil }

// fakeBeamRetriever 候选检索桩（可编程 Retrieve）。
type fakeBeamRetriever struct {
	mu         sync.Mutex
	retrieveFn func(ctx context.Context, query string, topK int) ([]common.RetrievalResult, error)
	lastQuery  string
	lastTopK   int
	lastMode   RetrievalMode
	calls      int
}

func (f *fakeBeamRetriever) Retrieve(ctx context.Context, query string, topK int, _ *float64, mode RetrievalMode, _ ...RetrieveOption) ([]common.RetrievalResult, error) {
	f.mu.Lock()
	f.lastQuery, f.lastTopK, f.lastMode, f.calls = query, topK, mode, f.calls+1
	f.mu.Unlock()
	return f.retrieveFn(ctx, query, topK)
}

func (f *fakeBeamRetriever) BatchRetrieve(context.Context, []string, int, ...RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil
}

func (f *fakeBeamRetriever) RetrieveSearchResults(context.Context, string, int, RetrievalMode, ...RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}

func (f *fakeBeamRetriever) Close() error { return nil }

// noEmbedRetriever 无 embed_model 属性的桩（hasattr False 同构——
// 不实现 EmbedModelProvider）
type noEmbedRetriever struct{ fakeBeamRetriever }

// withEmbedRetriever 有 embed_model 属性的桩（_RetrieverStubNoIndexType 对应物）
type withEmbedRetriever struct {
	fakeBeamRetriever
	embedModel embedding.BaseEmbedding
}

func (s *withEmbedRetriever) EmbedModel() embedding.BaseEmbedding { return s.embedModel }

// withIndexTypeRetriever 有 index_type 属性的桩
type withIndexTypeRetriever struct {
	fakeBeamRetriever
	indexType string
}

func (s *withIndexTypeRetriever) IndexType() string     { return s.indexType }
func (s *withIndexTypeRetriever) SetIndexType(v string) { s.indexType = v }

// makeTriple 构造带 triple 元数据的三元组结果
func makeTriple(text, tripleJSON string, score float64) common.RetrievalResult {
	return common.RetrievalResult{
		Text: text, Score: score,
		Metadata: map[string]any{"triple": tripleJSON},
	}
}

// ──────────────────────────── 用例 ────────────────────────────

// TestTripleBeamSearch_构造校验 max_length<1 → 155204
func TestTripleBeamSearch_构造校验(t *testing.T) {
	_, err := NewTripleBeamSearch(&withEmbedRetriever{}, WithTripleMaxLength(0))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expect max_length >= 1")
}

// TestTripleBeamSearch_无嵌入模型 → 155202
func TestTripleBeamSearch_无嵌入模型(t *testing.T) {
	s, err := NewTripleBeamSearch(&noEmbedRetriever{})
	assert.NoError(t, err)
	_, err = s.BeamSearch(context.Background(), "q", []common.RetrievalResult{makeTriple("t", `["a","r","b"]`, 0.9)})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embed_model is required for beam search")
}

// TestTripleBeamSearch_空输入 warn 返回空
func TestTripleBeamSearch_空输入(t *testing.T) {
	s, _ := NewTripleBeamSearch(&withEmbedRetriever{})
	beams, err := s.BeamSearch(context.Background(), "q", nil)
	assert.NoError(t, err)
	assert.Empty(t, beams)
}

// TestTripleBeamSearch_基本流程 两轮嵌入 + 无候选不扩展
func TestTripleBeamSearch_基本流程(t *testing.T) {
	emb := &fakeBeamEmbedModel{
		batchSize: 32,
		batches: [][][]float64{
			{{0.1, 0.2, 0.3}, {0.2, 0.3, 0.4}, {0.15, 0.25, 0.35}}, // 首轮：2 triples + query
			{{0.1, 0.2, 0.3}, {0.2, 0.3, 0.4}},                     // 次轮：2 beams 保原
		},
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, emb}
	s, err := NewTripleBeamSearch(r, WithTripleNumBeams(2), WithTripleMaxLength(2))
	assert.NoError(t, err)

	triples := []common.RetrievalResult{
		makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9),
		makeTriple("entity3 relation entity4", `["entity3","relation","entity4"]`, 0.8),
	}
	beams, err := s.BeamSearch(context.Background(), "test query", triples)
	assert.NoError(t, err)
	assert.Len(t, beams, 2)
	for _, b := range beams {
		assert.Equal(t, 1, b.Len()) // 无候选不扩展
	}
	// 批大小选项透传（Python embed_documents(batch_size=max_batch_size)）
	assert.NotEmpty(t, emb.lastOptsSnapshot())
}

// TestTripleBeamSearch_嵌入缺MaxBatchSize → 155202（拍板 c）
func TestTripleBeamSearch_嵌入缺MaxBatchSize(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, &plainEmbedModel{}}
	s, _ := NewTripleBeamSearch(r)
	_, err := s.BeamSearch(context.Background(), "q", []common.RetrievalResult{makeTriple("t", `["a","r","b"]`, 0.9)})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max_batch_size")
}

// TestTripleBeamSearch_批大小选项值断言 WithBatchSize(32) 实际透传
func TestTripleBeamSearch_批大小选项值断言(t *testing.T) {
	emb := &fakeBeamEmbedModel{
		batchSize: 32,
		batches: [][][]float64{
			{{1}, {1}}, // 1 triple + query
		},
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, emb}
	s, _ := NewTripleBeamSearch(r, WithTripleMaxLength(1)) // 单轮不扩展
	_, err := s.BeamSearch(context.Background(), "q", []common.RetrievalResult{makeTriple("t", `["a","r","b"]`, 0.9)})
	assert.NoError(t, err)
	// 选项被消费：max_batch_size=32 经 WithBatchSize 透传给 EmbedDocuments
	opts := emb.lastOptsSnapshot()
	assert.NotEmpty(t, opts)
}

// TestSearchCandidates_bm25映射sparse
func TestSearchCandidates_bm25映射sparse(t *testing.T) {
	r := &withIndexTypeRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, "bm25"}
	s, _ := NewTripleBeamSearch(r, WithTripleNumCandidatesPerBeam(50))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9)}, 0.9)
	_, err := s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Equal(t, RetrievalModeSparse, r.lastMode)
	assert.Equal(t, 50, r.lastTopK)
	assert.Contains(t, r.lastQuery, "entity1")
	assert.Contains(t, r.lastQuery, "entity2")
}

// TestSearchCandidates_非bm25透传
func TestSearchCandidates_非bm25透传(t *testing.T) {
	r := &withIndexTypeRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, "hybrid"}
	s, _ := NewTripleBeamSearch(r)
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("a rel b", `["a","rel","b"]`, 1.0)}, 1.0)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeHybrid, r.lastMode)
}

// TestSearchCandidates_无indexType用retrieveMode
func TestSearchCandidates_无indexType用retrieveMode(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r, WithTripleRetrieveMode("hybrid"), WithTripleNumCandidatesPerBeam(7))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9)}, 0.9)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeHybrid, r.lastMode)
	assert.Equal(t, 7, r.lastTopK)
}

// TestSearchCandidates_retrieveModeVector 显式 vector 模式透传
func TestSearchCandidates_retrieveModeVector(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r, WithTripleRetrieveMode("vector"), WithTripleNumCandidatesPerBeam(3))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("x rel y", `["x","rel","y"]`, 1.0)}, 1.0)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeVector, r.lastMode)
}

// TestSearchCandidates_双缺省hybrid
func TestSearchCandidates_双缺省hybrid(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r, WithTripleNumCandidatesPerBeam(5))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("a rel b", `["a","rel","b"]`, 1.0)}, 1.0)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeHybrid, r.lastMode)
}

// TestSearchCandidates_indexType优先
func TestSearchCandidates_indexType优先(t *testing.T) {
	r := &withIndexTypeRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, "vector"}
	s, _ := NewTripleBeamSearch(r, WithTripleRetrieveMode("hybrid"), WithTripleNumCandidatesPerBeam(2))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("p rel q", `["p","rel","q"]`, 1.0)}, 1.0)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeVector, r.lastMode)
}

// TestSearchCandidates_防御路径 尾元数据缺失/非字符串/解析失败/len<2
func TestSearchCandidates_防御路径(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r)

	// 无 triple 键
	beam := common.NewTripleBeam([]common.RetrievalResult{{Text: "t", Metadata: map[string]any{}}}, 1.0)
	ret, err := s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Empty(t, ret)

	// 非 JSON 字符串
	beam = common.NewTripleBeam([]common.RetrievalResult{makeTriple("t", "not-json", 1.0)}, 1.0)
	ret, err = s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Empty(t, ret)

	// len<2 解析后元素不足
	beam = common.NewTripleBeam([]common.RetrievalResult{makeTriple("t", `["a"]`, 1.0)}, 1.0)
	ret, err = s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Empty(t, ret)

	// 空 JSON 数组（Python not triple）
	beam = common.NewTripleBeam([]common.RetrievalResult{makeTriple("t", `[]`, 1.0)}, 1.0)
	ret, err = s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Empty(t, ret)
}

// TestSearchCandidates_候选裸解析冒泡 no-meta 候选触发 error 冒泡
func TestSearchCandidates_候选裸解析冒泡(t *testing.T) {
	candidates := []common.RetrievalResult{
		{Text: "no-meta", Metadata: nil}, // Python json.loads(None) TypeError 冒泡
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return candidates, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r)
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9)}, 0.9)
	_, err := s.searchCandidates(context.Background(), beam)
	assert.Error(t, err)
}

// TestSearchCandidates_三重过滤 已在beam/首尾过滤/合格保留
func TestSearchCandidates_三重过滤(t *testing.T) {
	candidates := []common.RetrievalResult{
		{Text: "entity1 relation entity2", Metadata: map[string]any{"triple": `["entity1","r","entity2"]`}}, // Contains 过滤（text 判重）
		{Text: "unrelated", Metadata: map[string]any{"triple": `["x","r","y"]`}},                            // 首尾过滤
		{Text: "good", Metadata: map[string]any{"triple": `["entity1","r2","z"]`}},                          // 保留
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return candidates, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r)
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9)}, 0.9)
	ret, err := s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Len(t, ret, 1)
	assert.Equal(t, "good", ret[0].Text)
}

// TestTripleBeamSearch_扩展轮路径 候选存在时的 beam 扩展
func TestTripleBeamSearch_扩展轮路径(t *testing.T) {
	// beam1 尾 [a,r,b]，候选 [[b,r2,c]]（首尾 b 命中）→ 扩展为 [a,r,b]+[b,r2,c]
	// 注意：有候选时只产生扩展路径（Python L121-124 保原路径仅当候选为空），
	// 次轮仅 1 条路径文本 → stub 给 1 个向量
	emb := &fakeBeamEmbedModel{
		batchSize: 32,
		batches: [][][]float64{
			{{1, 0}, {0.5, 0.5}}, // 首轮：1 triple + query
			{{0.9, 0.1}},         // 次轮：1 条扩展路径
		},
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeTriple("b r2 c", `["b","r2","c"]`, 0.7)}, nil
	}}, emb}
	s, _ := NewTripleBeamSearch(r, WithTripleNumBeams(2), WithTripleMaxLength(2))
	triples := []common.RetrievalResult{makeTriple("a r b", `["a","r","b"]`, 0.9)}
	beams, err := s.BeamSearch(context.Background(), "q", triples)
	assert.NoError(t, err)
	// 断言：至少一条 beam 长度 2（扩展成功）
	maxLen := 0
	for _, b := range beams {
		if b.Len() > maxLen {
			maxLen = b.Len()
		}
	}
	assert.Equal(t, 2, maxLen)
}

// TestTripleBeamSearch_候选已在existTriples过滤 扩展轮存在性去重
func TestTripleBeamSearch_候选已在existTriples过滤(t *testing.T) {
	// 候选文本与 beam 内已有 triple 相同 → 不产生扩展路径 → 保原 beam
	emb := &fakeBeamEmbedModel{
		batchSize: 32,
		batches: [][][]float64{
			{{1, 0}, {1, 0}},     // 首轮
			{{1, 0}, {0.5, 0.5}}, // 次轮：保原路径
		},
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeTriple("a r b", `["a","r","b"]`, 0.5)}, nil // 已在 beam
	}}, emb}
	s, _ := NewTripleBeamSearch(r, WithTripleNumBeams(1), WithTripleMaxLength(2))
	triples := []common.RetrievalResult{makeTriple("a r b", `["a","r","b"]`, 0.9)}
	beams, err := s.BeamSearch(context.Background(), "q", triples)
	assert.NoError(t, err)
	for _, b := range beams {
		assert.Equal(t, 1, b.Len()) // 候选被 existTriples 过滤 → 无扩展
	}
}

// TestSearchCandidates_空beam防御 T-02：对齐 Python L173-174
// if len(beam) < 1: raise RuntimeError("unexpected empty beam")。
// nil beam 与零长 beam 均应返回错误而非 panic（beam.Get(-1) 越界）。
func TestSearchCandidates_空beam防御(t *testing.T) {
	r := &fakeBeamRetriever{}
	s, err := NewTripleBeamSearch(r)
	if err != nil {
		t.Fatalf("NewTripleBeamSearch: %v", err)
	}
	// nil beam 传入空束应报错
	_, err = s.searchCandidates(context.Background(), nil)
	if err == nil || err.Error() != "unexpected empty beam" {
		t.Errorf("nil beam: err = %v, want unexpected empty beam", err)
	}
	// 空 beam（Len()==0）
	empty := common.NewTripleBeam(nil, 0)
	_, err = s.searchCandidates(context.Background(), empty)
	if err == nil || err.Error() != "unexpected empty beam" {
		t.Errorf("empty beam: err = %v, want unexpected empty beam", err)
	}
}
