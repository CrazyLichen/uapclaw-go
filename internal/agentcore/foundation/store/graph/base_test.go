package graph

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// fakeGraphStore 用于测试的模拟图存储
type fakeGraphStore struct {
	config *GraphConfig
	query  func(ctx context.Context, collection string, opts ...Option) ([]map[string]any, error)
}

func (f *fakeGraphStore) Config() *GraphConfig                              { return f.config }
func (f *fakeGraphStore) Rebuild(ctx context.Context) error                 { return nil }
func (f *fakeGraphStore) Refresh(ctx context.Context, opts ...Option) error { return nil }
func (f *fakeGraphStore) Close() error                                      { return nil }
func (f *fakeGraphStore) AddEntity(ctx context.Context, entities []*Entity, opts ...Option) error {
	return nil
}
func (f *fakeGraphStore) AddRelation(ctx context.Context, relations []*Relation, opts ...Option) error {
	return nil
}
func (f *fakeGraphStore) AddEpisode(ctx context.Context, episodes []*Episode, opts ...Option) error {
	return nil
}
func (f *fakeGraphStore) Query(ctx context.Context, collection string, opts ...Option) ([]map[string]any, error) {
	if f.query != nil {
		return f.query(ctx, collection, opts...)
	}
	return nil, nil
}
func (f *fakeGraphStore) Delete(ctx context.Context, collection string, opts ...Option) error {
	return nil
}
func (f *fakeGraphStore) IsEmpty(ctx context.Context, collection string) (bool, error) {
	return true, nil
}
func (f *fakeGraphStore) Search(ctx context.Context, query string, opts ...Option) (map[string][]map[string]any, error) {
	return nil, nil
}
func (f *fakeGraphStore) AttachEmbedder(embedder embedding.BaseEmbedding) {}

// TestRegisterBackend_正常注册 测试正常注册
func TestRegisterBackend_正常注册(t *testing.T) {
	// 使用独立的 factory 避免污染全局
	f := &GraphStoreFactory{backends: make(map[string]func(*GraphConfig) (BaseGraphStore, error))}
	constructor := func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{config: cfg}, nil
	}
	f.backends["test"] = constructor
	if _, ok := f.backends["test"]; !ok {
		t.Error("注册后应能找到后端")
	}
}

// TestNewFromConfig_正常创建 测试正常创建
func TestNewFromConfig_正常创建(t *testing.T) {
	// 临时替换全局 factory 的 backends
	origBackends := globalFactory.backends
	globalFactory.backends = make(map[string]func(*GraphConfig) (BaseGraphStore, error))
	defer func() { globalFactory.backends = origBackends }()

	err := RegisterBackend("test_backend", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{config: cfg}, nil
	})
	if err != nil {
		t.Fatalf("注册后端失败: %v", err)
	}

	cfg := NewGraphConfig("http://localhost:19530")
	cfg.Backend = "test_backend"
	store, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("创建图存储失败: %v", err)
	}
	if store == nil {
		t.Error("创建的图存储不应为 nil")
	}
}

// TestNewFromConfig_未找到后端 测试未找到后端
func TestNewFromConfig_未找到后端(t *testing.T) {
	origBackends := globalFactory.backends
	globalFactory.backends = make(map[string]func(*GraphConfig) (BaseGraphStore, error))
	defer func() { globalFactory.backends = origBackends }()

	cfg := NewGraphConfig("http://localhost:19530")
	cfg.Backend = "nonexistent"
	_, err := NewFromConfig(cfg)
	if err == nil {
		t.Error("未注册的后端应返回错误")
	}
}

// TestRegisterBackend_重复注册 测试重复注册
func TestRegisterBackend_重复注册(t *testing.T) {
	origBackends := globalFactory.backends
	globalFactory.backends = make(map[string]func(*GraphConfig) (BaseGraphStore, error))
	defer func() { globalFactory.backends = origBackends }()

	_ = RegisterBackend("dup", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{}, nil
	})
	err := RegisterBackend("dup", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{}, nil
	})
	if err == nil {
		t.Error("重复注册应返回错误")
	}
}

// TestRegisterBackend_强制覆盖 测试强制覆盖
func TestRegisterBackend_强制覆盖(t *testing.T) {
	origBackends := globalFactory.backends
	globalFactory.backends = make(map[string]func(*GraphConfig) (BaseGraphStore, error))
	defer func() { globalFactory.backends = origBackends }()

	_ = RegisterBackend("dup", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{}, nil
	})
	err := RegisterBackend("dup", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{}, nil
	}, true)
	if err != nil {
		t.Errorf("强制覆盖不应返回错误: %v", err)
	}
}

// TestNewOptions 测试选项应用
func TestNewOptions(t *testing.T) {
	opts := newOptions(
		WithFlush(true),
		WithUpsert(false),
		WithK(10),
		WithCollection(EntityCollection),
	)
	if !opts.Flush {
		t.Error("Flush 应为 true")
	}
	if opts.Upsert {
		t.Error("Upsert 应为 false")
	}
	if opts.K != 10 {
		t.Errorf("K 应为 10，实际为 %d", opts.K)
	}
	if opts.Collection != EntityCollection {
		t.Errorf("Collection 应为 %s，实际为 %s", EntityCollection, opts.Collection)
	}
}

// TestWithNoEmbed 测试 WithNoEmbed 选项
func TestWithNoEmbed(t *testing.T) {
	opts := newOptions(WithNoEmbed(true))
	if !opts.NoEmbed {
		t.Error("NoEmbed 应为 true")
	}
}

// TestWithExpr 测试 WithExpr 选项
func TestWithExpr(t *testing.T) {
	expr := &fakeQueryExpr{}
	opts := newOptions(WithExpr(expr))
	if opts.Expr != expr {
		t.Error("Expr 应为传入的表达式")
	}
}

// TestWithSilenceErrors 测试 WithSilenceErrors 选项
func TestWithSilenceErrors(t *testing.T) {
	opts := newOptions(WithSilenceErrors(true))
	if !opts.SilenceErrors {
		t.Error("SilenceErrors 应为 true")
	}
}

// TestWithBFS 测试 WithBFS 选项
func TestWithBFS(t *testing.T) {
	opts := newOptions(WithBFS(3, 20))
	if opts.BFSDepth != 3 {
		t.Errorf("BFSDepth 应为 3，实际为 %d", opts.BFSDepth)
	}
	if opts.BFSK != 20 {
		t.Errorf("BFSK 应为 20，实际为 %d", opts.BFSK)
	}
}

// TestWithFilterExpr 测试 WithFilterExpr 选项
func TestWithFilterExpr(t *testing.T) {
	expr := &fakeQueryExpr{}
	opts := newOptions(WithFilterExpr(expr))
	if opts.FilterExpr != expr {
		t.Error("FilterExpr 应为传入的表达式")
	}
}

// TestWithOutputFields 测试 WithOutputFields 选项
func TestWithOutputFields(t *testing.T) {
	opts := newOptions(WithOutputFields("uuid", "name"))
	if len(opts.OutputFields) != 2 {
		t.Fatalf("OutputFields 长度应为 2，实际为 %d", len(opts.OutputFields))
	}
	if opts.OutputFields[0] != "uuid" || opts.OutputFields[1] != "name" {
		t.Errorf("OutputFields 不正确: %v", opts.OutputFields)
	}
}

// TestWithQueryEmbedding 测试 WithQueryEmbedding 选项
func TestWithQueryEmbedding(t *testing.T) {
	emb := []float64{0.1, 0.2, 0.3}
	opts := newOptions(WithQueryEmbedding(emb))
	if len(opts.QueryEmbedding) != 3 {
		t.Errorf("QueryEmbedding 长度应为 3，实际为 %d", len(opts.QueryEmbedding))
	}
}

// TestWithLanguage 测试 WithLanguage 选项
func TestWithLanguage(t *testing.T) {
	opts := newOptions(WithLanguage("en"))
	if opts.Language != "en" {
		t.Errorf("Language 应为 en，实际为 %s", opts.Language)
	}
}

// TestWithMinScore 测试 WithMinScore 选项
func TestWithMinScore(t *testing.T) {
	opts := newOptions(WithMinScore(0.5))
	if opts.MinScore != 0.5 {
		t.Errorf("MinScore 应为 0.5，实际为 %v", opts.MinScore)
	}
}

// TestWithRankerConfig 测试 WithRankerConfig 选项
func TestWithRankerConfig(t *testing.T) {
	rc := NewWeightedRankConfig()
	opts := newOptions(WithRankerConfig(rc))
	if opts.RankerConfig != rc {
		t.Error("RankerConfig 应为传入的配置")
	}
}

// TestWithReranker 测试 WithReranker 选项
func TestWithReranker(t *testing.T) {
	r := &fakeReranker{}
	opts := newOptions(WithReranker(r))
	if opts.Reranker == nil {
		t.Error("Reranker 不应为 nil")
	}
}

// TestEnsureUniqueUUIDs_跳过 测试跳过去重
func TestEnsureUniqueUUIDs_跳过(t *testing.T) {
	ids := []string{"a", "b"}
	result, err := EnsureUniqueUUIDs(context.Background(), nil, ids, "test", true)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("跳过时应返回原列表，实际为 %v", result)
	}
}

// TestEnsureUniqueUUIDs_空列表 测试空列表
func TestEnsureUniqueUUIDs_空列表(t *testing.T) {
	result, err := EnsureUniqueUUIDs(context.Background(), nil, nil, "test", false)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("空列表应返回空，实际为 %v", result)
	}
}

// TestEnsureUniqueUUIDs_去重 测试UUID去重
func TestEnsureUniqueUUIDs_去重(t *testing.T) {
	store := &fakeGraphStore{
		query: func(ctx context.Context, collection string, opts ...Option) ([]map[string]any, error) {
			return []map[string]any{
				{"uuid": "a"},
				{"uuid": "c"},
			}, nil
		},
	}
	ids := []string{"a", "b", "c", "d"}
	result, err := EnsureUniqueUUIDs(context.Background(), store, ids, EntityCollection, false)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("去重后应为 2 个，实际为 %d", len(result))
	}
	if result[0] != "b" || result[1] != "d" {
		t.Errorf("去重结果不正确: %v", result)
	}
}

// fakeQueryExpr 用于测试的模拟查询表达式
type fakeQueryExpr struct{}

func (f *fakeQueryExpr) ToExpr(backend string) (any, error) { return "", nil }

// fakeReranker 用于测试的模拟重排序器
type fakeReranker struct{}

func (f *fakeReranker) Rerank(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	return nil, nil
}

func (f *fakeReranker) RerankDocs(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	return nil, nil
}

func (f *fakeReranker) RerankSync(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	return nil, nil
}

func (f *fakeReranker) RerankDocsSync(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	return nil, nil
}
