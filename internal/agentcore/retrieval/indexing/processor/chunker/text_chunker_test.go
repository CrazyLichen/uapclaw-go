package chunker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 构造（char 路径） ────────────────────────────

// TestNewTextChunker_默认值 对齐 Python test_init_with_char_unit（默认 char 单位）
func TestNewTextChunker_默认值(t *testing.T) {
	c, err := NewTextChunker(512, 50)
	assert.NoError(t, err)
	assert.Equal(t, 512, c.ChunkSize)
	assert.Equal(t, 50, c.ChunkOverlap)
	// inner 路由到 CharChunker
	_, ok := c.inner.(*CharChunker)
	assert.True(t, ok)
}

// TestNewTextChunker_自定义值 对齐 Python test_init_with_custom_values
func TestNewTextChunker_自定义值(t *testing.T) {
	c, err := NewTextChunker(256, 25)
	assert.NoError(t, err)
	assert.Equal(t, 256, c.ChunkSize)
	assert.Equal(t, 25, c.ChunkOverlap)
}

// TestNewTextChunker_构造校验穿透 参数经 NewBaseChunker 校验（155100/155101）
func TestNewTextChunker_构造校验穿透(t *testing.T) {
	_, err := NewTextChunker(0, 50)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155100")

	_, err = NewTextChunker(512, -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")

	_, err = NewTextChunker(10, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")
}

// TestNewTextChunker_无预处理选项 对齐 Python test_init_without_preprocess_options
func TestNewTextChunker_无预处理选项(t *testing.T) {
	c, err := NewTextChunker(512, 50)
	assert.NoError(t, err)
	assert.Equal(t, 0, c.pipeline.Len())
}

// TestNewTextChunker_预处理选项全开 对齐 Python test_init_with_preprocess_options
func TestNewTextChunker_预处理选项全开(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithPreprocessOptions(PreprocessOptions{
		NormalizeWhitespace: true,
		RemoveURLEmail:      true,
	}))
	assert.NoError(t, err)
	assert.Equal(t, 2, c.pipeline.Len())
}

// TestNewTextChunker_只开空白归一 对齐 Python test_init_with_normalize_whitespace
func TestNewTextChunker_只开空白归一(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}))
	assert.NoError(t, err)
	assert.Equal(t, 1, c.pipeline.Len())
}

// TestNewTextChunker_只开URL邮箱移除 对齐 Python test_init_with_remove_url_email
func TestNewTextChunker_只开URL邮箱移除(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithPreprocessOptions(PreprocessOptions{RemoveURLEmail: true}))
	assert.NoError(t, err)
	assert.Equal(t, 1, c.pipeline.Len())
}

// TestNewTextChunker_组装顺序whitespace先url后 Go 专项：TextChunker 组装的
// preprocessors[0] 是 WhitespaceNormalizer、[1] 是 URLEmailRemover（对齐 Python if 序）
func TestNewTextChunker_组装顺序whitespace先url后(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithPreprocessOptions(PreprocessOptions{
		NormalizeWhitespace: true,
		RemoveURLEmail:      true,
	}))
	assert.NoError(t, err)
	assert.IsType(t, WhitespaceNormalizer{}, c.pipeline.preprocessors[0])
	assert.IsType(t, URLEmailRemover{}, c.pipeline.preprocessors[1])
}
