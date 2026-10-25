package retriever

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
