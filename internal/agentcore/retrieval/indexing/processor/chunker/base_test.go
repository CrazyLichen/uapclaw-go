package chunker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewBaseChunker_默认参数成功 验证合法参数构造成功
func TestNewBaseChunker_默认参数成功(t *testing.T) {
	bc, err := NewBaseChunker(512, 50)
	assert.NoError(t, err)
	assert.Equal(t, 512, bc.ChunkSize)
	assert.Equal(t, 50, bc.ChunkOverlap)
	assert.Nil(t, bc.lengthFunction) // 字段保持 nil，Length() 方法内默认 utf8.RuneCountInString
}

// TestNewBaseChunker_chunkSize非正数报155100 对齐 Python chunk_size<=0 校验
func TestNewBaseChunker_chunkSize非正数报155100(t *testing.T) {
	for _, size := range []int{0, -1} {
		bc, err := NewBaseChunker(size, 50)
		assert.Nil(t, bc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "155100")
		assert.Contains(t, err.Error(), "chunk_size must be greater than 0")
	}
}

// TestNewBaseChunker_overlap负数报155101 对齐 Python chunk_overlap<0 校验
func TestNewBaseChunker_overlap负数报155101(t *testing.T) {
	bc, err := NewBaseChunker(512, -1)
	assert.Nil(t, bc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")
	assert.Contains(t, err.Error(), "chunk_overlap must be greater than or equal to 0")
}

// TestNewBaseChunker_overlap大于等于size报155101 对齐 Python chunk_overlap>=chunk_size 校验
func TestNewBaseChunker_overlap大于等于size报155101(t *testing.T) {
	for _, overlap := range []int{512, 600} {
		bc, err := NewBaseChunker(512, overlap)
		assert.Nil(t, bc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "155101")
		assert.Contains(t, err.Error(), "chunk_overlap must be less than chunk_size")
	}
}
