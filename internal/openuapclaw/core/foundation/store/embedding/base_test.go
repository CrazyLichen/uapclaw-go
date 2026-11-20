package embedding

import (
	"context"
	"testing"
)

// fakeCallback 用于测试的模拟回调
type fakeCallback struct {
	called bool
}

func (f *fakeCallback) OnBatchComplete(_, _ int, _ []string) {
	f.called = true
}

// fakeEmbedding 用于测试的模拟嵌入模型
type fakeEmbedding struct {
	dimension int
}

func newFakeEmbedding(dim int) *fakeEmbedding {
	return &fakeEmbedding{dimension: dim}
}

func (f *fakeEmbedding) EmbedQuery(_ context.Context, text string, _ ...EmbedOption) ([]float64, error) {
	vec := make([]float64, f.dimension)
	for i := range vec {
		vec[i] = float64(len(text) + i)
	}
	return vec, nil
}

func (f *fakeEmbedding) EmbedDocuments(_ context.Context, texts []string, _ ...EmbedOption) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		vec := make([]float64, f.dimension)
		for j := range vec {
			vec[j] = float64(len(text) + j + i)
		}
		result[i] = vec
	}
	return result, nil
}

func (f *fakeEmbedding) Dimension() int {
	return f.dimension
}

func (f *fakeEmbedding) DimensionWithContext(_ context.Context) (int, error) {
	return f.dimension, nil
}

func TestBaseEmbedding_接口约束(t *testing.T) {
	// 验证 fakeEmbedding 满足 BaseEmbedding 接口
	var _ BaseEmbedding = &fakeEmbedding{}
}

func TestWithBatchSize(t *testing.T) {
	opt := WithBatchSize(32)
	opts := EmbedOptions{}
	opt(&opts)
	if opts.BatchSize != 32 {
		t.Errorf("期望 BatchSize=32, 实际=%d", opts.BatchSize)
	}
}

func TestWithBatchSize_零值(t *testing.T) {
	opt := WithBatchSize(0)
	opts := EmbedOptions{}
	opt(&opts)
	if opts.BatchSize != 0 {
		t.Errorf("期望 BatchSize=0, 实际=%d", opts.BatchSize)
	}
}

func TestWithCallback(t *testing.T) {
	cb := &fakeCallback{}
	opt := WithCallback(cb)
	opts := EmbedOptions{}
	opt(&opts)
	if opts.Callback != cb {
		t.Errorf("期望 Callback 被正确设置")
	}
}

func TestWithCallback_nil(t *testing.T) {
	opt := WithCallback(nil)
	opts := EmbedOptions{}
	opt(&opts)
	if opts.Callback != nil {
		t.Errorf("期望 Callback 为 nil")
	}
}

func TestNewEmbedOptions_空参数(t *testing.T) {
	opts := NewEmbedOptions()
	if opts.BatchSize != 0 {
		t.Errorf("期望默认 BatchSize=0, 实际=%d", opts.BatchSize)
	}
	if opts.Callback != nil {
		t.Errorf("期望默认 Callback=nil")
	}
}

func TestNewEmbedOptions_仅BatchSize(t *testing.T) {
	opts := NewEmbedOptions(WithBatchSize(64))
	if opts.BatchSize != 64 {
		t.Errorf("期望 BatchSize=64, 实际=%d", opts.BatchSize)
	}
	if opts.Callback != nil {
		t.Errorf("期望 Callback=nil")
	}
}

func TestNewEmbedOptions_仅Callback(t *testing.T) {
	cb := &fakeCallback{}
	opts := NewEmbedOptions(WithCallback(cb))
	if opts.BatchSize != 0 {
		t.Errorf("期望 BatchSize=0, 实际=%d", opts.BatchSize)
	}
	if opts.Callback == nil {
		t.Errorf("期望 Callback 不为 nil")
	}
}

func TestNewEmbedOptions_全部选项(t *testing.T) {
	cb := &fakeCallback{}
	opts := NewEmbedOptions(WithBatchSize(128), WithCallback(cb))
	if opts.BatchSize != 128 {
		t.Errorf("期望 BatchSize=128, 实际=%d", opts.BatchSize)
	}
	if opts.Callback == nil {
		t.Errorf("期望 Callback 不为 nil")
	}
}

func TestFakeEmbedding_EmbedQuery(t *testing.T) {
	emb := newFakeEmbedding(3)
	vec, err := emb.EmbedQuery(context.Background(), "hi")
	if err != nil {
		t.Fatalf("未期望错误: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("期望向量维度=3, 实际=%d", len(vec))
	}
}

func TestFakeEmbedding_EmbedDocuments(t *testing.T) {
	emb := newFakeEmbedding(2)
	vecs, err := emb.EmbedDocuments(context.Background(), []string{"a", "bb"})
	if err != nil {
		t.Fatalf("未期望错误: %v", err)
	}
	if len(vecs) != 2 {
		t.Errorf("期望结果数=2, 实际=%d", len(vecs))
	}
	if len(vecs[0]) != 2 || len(vecs[1]) != 2 {
		t.Errorf("期望每个向量维度=2")
	}
}

func TestFakeEmbedding_Dimension(t *testing.T) {
	emb := newFakeEmbedding(768)
	if d := emb.Dimension(); d != 768 {
		t.Errorf("期望维度=768, 实际=%d", d)
	}
}

func TestFakeEmbedding_DimensionWithContext(t *testing.T) {
	emb := newFakeEmbedding(768)
	d, err := emb.DimensionWithContext(context.Background())
	if err != nil {
		t.Fatalf("未期望错误: %v", err)
	}
	if d != 768 {
		t.Errorf("期望维度=768, 实际=%d", d)
	}
}

func TestFakeCallback_OnBatchComplete(t *testing.T) {
	cb := &fakeCallback{}
	cb.OnBatchComplete(0, 10, []string{"a", "b"})
	if !cb.called {
		t.Errorf("期望回调被调用")
	}
}
