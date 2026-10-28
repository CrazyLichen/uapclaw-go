package retriever

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCosineScores_基本余弦 对齐 Python _cosine_scores：归一化后点积
func TestCosineScores_基本余弦(t *testing.T) {
	query := []float64{1, 0}
	cands := [][]float64{
		{1, 0},  // cos=1
		{0, 1},  // cos=0
		{-1, 0}, // cos=-1
		{3, 0},  // 归一化后 cos=1（量纲无关）
	}
	scores := cosineScores(query, cands)
	assert.InDelta(t, 1.0, scores[0], 1e-9)
	assert.InDelta(t, 0.0, scores[1], 1e-9)
	assert.InDelta(t, -1.0, scores[2], 1e-9)
	assert.InDelta(t, 1.0, scores[3], 1e-9)
}

// TestCosineScores_零向量哨兵 1e-12 防 NaN
func TestCosineScores_零向量哨兵(t *testing.T) {
	scores := cosineScores([]float64{0, 0}, [][]float64{{1, 0}})
	assert.False(t, math.IsNaN(scores[0]))
	assert.InDelta(t, 0.0, scores[0], 1e-9)
}

// TestCosineScores_空候选
func TestCosineScores_空候选(t *testing.T) {
	scores := cosineScores([]float64{1}, nil)
	assert.Empty(t, scores)
}

// TestTopkIndices_基本 对齐 argpartition+argsort 降序
func TestTopkIndices_基本(t *testing.T) {
	idx, scores := topkIndices([]float64{0.1, 0.9, 0.5, 0.7}, 2)
	assert.Equal(t, []int{1, 3}, idx)
	assert.InDelta(t, 0.9, scores[0], 1e-9)
	assert.InDelta(t, 0.7, scores[1], 1e-9)
}

// TestTopkIndices_k超出N 对齐 k=min(k, N)
func TestTopkIndices_k超出N(t *testing.T) {
	idx, scores := topkIndices([]float64{0.3, 0.1}, 5)
	assert.Len(t, idx, 2)
	assert.InDelta(t, 0.3, scores[0], 1e-9)
}

// TestTopkIndices_k非正 对齐 k<=0 → ([], [])
func TestTopkIndices_k非正(t *testing.T) {
	idx, scores := topkIndices([]float64{0.3, 0.1}, 0)
	assert.Empty(t, idx)
	assert.Empty(t, scores)
	idx, _ = topkIndices([]float64{0.3}, -1)
	assert.Empty(t, idx)
}

// TestTopkIndices_空输入
func TestTopkIndices_空输入(t *testing.T) {
	idx, scores := topkIndices(nil, 3)
	assert.Empty(t, idx)
	assert.Empty(t, scores)
}

// TestTopkIndices_等值非稳定 与 Python argpartition 同构（顺序无保证）
func TestTopkIndices_等值(t *testing.T) {
	idx, scores := topkIndices([]float64{0.5, 0.5, 0.5}, 2)
	assert.Len(t, idx, 2)
	assert.InDelta(t, 0.5, scores[0], 1e-9)
	assert.InDelta(t, 0.5, scores[1], 1e-9)
}
