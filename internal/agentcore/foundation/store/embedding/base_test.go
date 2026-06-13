package embedding

import (
	"context"
	"testing"
)

// fakeEmbedding 用于测试的模拟嵌入模型
type fakeEmbedding struct {
	dimension int
}

func newFakeEmbedding(dim int) *fakeEmbedding {
	return &fakeEmbedding{dimension: dim}
}

func (f *fakeEmbedding) EmbedQuery(_ context.Context, text string) ([]float64, error) {
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

func TestBaseEmbedding_接口约束(t *testing.T) {
	// 验证 fakeEmbedding 满足 BaseEmbedding 接口
	var _ BaseEmbedding = &fakeEmbedding{}
}
