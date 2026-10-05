package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// TestRRFFusion_单路结果 测试单路结果顺序不变
func TestRRFFusion_单路结果(t *testing.T) {
	list := [][]common.RetrievalResult{
		{
			{Text: "a", Score: 0.9, Metadata: map[string]any{}},
			{Text: "b", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	result := RRFFusion(list, 60)
	assert.Len(t, result, 2)
	// 单路结果顺序不变
	assert.Equal(t, "a", result[0].Text)
	assert.Equal(t, "b", result[1].Text)
}

// TestRRFFusion_两路融合 测试 RRF 融合逻辑
func TestRRFFusion_两路融合(t *testing.T) {
	list := [][]common.RetrievalResult{
		{
			{Text: "a", Score: 0.9, Metadata: map[string]any{"source": "vec"}},
			{Text: "b", Score: 0.8, Metadata: map[string]any{"source": "vec"}},
		},
		{
			{Text: "b", Score: 0.95, Metadata: map[string]any{"source": "text"}},
			{Text: "c", Score: 0.7, Metadata: map[string]any{"source": "text"}},
		},
	}
	result := RRFFusion(list, 60)
	assert.Len(t, result, 3)
	// "b" 在两路都出现，RRF 分数最高
	assert.Equal(t, "b", result[0].Text)
	// "b" 保留首次出现的 metadata（来自 vec）
	assert.Equal(t, "vec", result[0].Metadata["source"])
}

// TestRRFFusion_空列表 测试空输入
func TestRRFFusion_空列表(t *testing.T) {
	result := RRFFusion(nil, 60)
	assert.Empty(t, result)
}

// TestRRFFusion_空子列表 测试空子列表
func TestRRFFusion_空子列表(t *testing.T) {
	list := [][]common.RetrievalResult{
		{},
		{{Text: "a", Score: 0.9, Metadata: map[string]any{}}},
	}
	result := RRFFusion(list, 60)
	assert.Len(t, result, 1)
}

// TestRRFFusion_分数计算 测试 RRF 分数公式
func TestRRFFusion_分数计算(t *testing.T) {
	list := [][]common.RetrievalResult{
		{
			{Text: "x", Score: 0, Metadata: map[string]any{}}, // rank=1, k=2: 1/(2+1) = 0.333
		},
		{
			{Text: "x", Score: 0, Metadata: map[string]any{}}, // rank=1, k=2: 1/(2+1) = 0.333
		},
	}
	result := RRFFusion(list, 2)
	assert.Len(t, result, 1)
	// 两路 rank=1: 1/(2+1) + 1/(2+1) = 2/3 ≈ 0.667
	assert.InDelta(t, 2.0/3.0, result[0].Score, 0.001)
}

// TestRRFFusion_保留Metadata 测试首次出现 metadata 保留
func TestRRFFusion_保留Metadata(t *testing.T) {
	list := [][]common.RetrievalResult{
		{
			{Text: "doc1", Score: 0.9, Metadata: map[string]any{"doc_id": "d1", "chunk_id": "c1"}},
		},
		{
			{Text: "doc1", Score: 0.8, Metadata: map[string]any{"doc_id": "d2", "chunk_id": "c2"}},
		},
	}
	result := RRFFusion(list, 60)
	assert.Len(t, result, 1)
	// 保留首次出现的 metadata
	assert.Equal(t, "d1", result[0].Metadata["doc_id"])
	assert.Equal(t, "c1", result[0].Metadata["chunk_id"])
}
