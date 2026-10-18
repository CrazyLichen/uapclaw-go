package chunker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// TestNewCharChunker_默认值512和50 对齐 Python test_init_with_defaults
func TestNewCharChunker_默认值512和50(t *testing.T) {
	c, err := NewCharChunker()
	assert.NoError(t, err)
	assert.Equal(t, 512, c.ChunkSize)
	assert.Equal(t, 50, c.ChunkOverlap)
}

// TestNewCharChunker_自定义值 对齐 Python test_init_with_custom_values
func TestNewCharChunker_自定义值(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(256), WithChunkOverlap(25))
	assert.NoError(t, err)
	assert.Equal(t, 256, c.ChunkSize)
	assert.Equal(t, 25, c.ChunkOverlap)
}

// TestNewCharChunker_构造校验穿透 参数经 NewBaseChunker 校验（155100/155101）
func TestNewCharChunker_构造校验穿透(t *testing.T) {
	_, err := NewCharChunker(WithChunkSize(0))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155100")

	_, err = NewCharChunker(WithChunkOverlap(-1))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")

	_, err = NewCharChunker(WithChunkSize(10), WithChunkOverlap(10))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")
}

// TestCharChunker_ChunkText_真实分块 Python mock 版用例改为真实行为断言：
// size=10 overlap=2，"This is a test text"(19 字符) → step=8 → 3 块
func TestCharChunker_ChunkText_真实分块(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	assert.NoError(t, err)
	chunks, err := c.ChunkText("This is a test text")
	assert.NoError(t, err)
	assert.Equal(t, []string{"This is a ", "a test tex", "ext"}, chunks)
}

// TestCharChunker_ChunkText_空文本 对齐 Python test_chunk_text_empty + test_chunk_text_none
//（Go 无法表达 None，两用例并入空串）
func TestCharChunker_ChunkText_空文本(t *testing.T) {
	c, err := NewCharChunker()
	assert.NoError(t, err)
	chunks, err := c.ChunkText("")
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestCharChunker_ChunkText_中文rune不截断 Go 专项：多字节字符按 rune 切分
func TestCharChunker_ChunkText_中文rune不截断(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(3), WithChunkOverlap(1))
	assert.NoError(t, err)
	chunks, err := c.ChunkText("你好世界再见")
	assert.NoError(t, err)
	assert.Equal(t, []string{"你好世", "世界再", "再见"}, chunks)
}

// TestCharChunker_ChunkDocuments_全链 SetSelf 注入后默认实现路由到覆写的 ChunkText
// 断言确定性字段（含 uuid 的产出不可整 struct 比较——13.5 审查规则）
func TestCharChunker_ChunkDocuments_全链(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	assert.NoError(t, err)
	docs := []reranker.Document{{
		ID:       "doc_1",
		Text:     "This is a test text",
		Metadata: map[string]any{"k": "v"},
	}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
	for i, chunk := range chunks {
		assert.Equal(t, "doc_1", chunk.DocID)
		assert.Equal(t, "v", chunk.Metadata["k"])
		assert.Equal(t, i, chunk.Metadata["chunk_index"])
		assert.Equal(t, 3, chunk.Metadata["total_chunks"])
		assert.Equal(t, chunk.ID, chunk.Metadata["chunk_id"])
	}
	assert.Equal(t, "This is a ", chunks[0].Text)
	assert.Equal(t, "a test tex", chunks[1].Text)
	assert.Equal(t, "ext", chunks[2].Text)
}

// TestCharChunker_Process_等价ChunkDocuments 对齐 Python process = chunk_documents。
// 两次调用各自生成 uuid，按确定性字段断言等价（不可整 struct 比较）
func TestCharChunker_Process_等价ChunkDocuments(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This is a test text"}}

	viaProcess, err := c.Process(context.Background(), docs)
	assert.NoError(t, err)
	viaDocs, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)

	assert.Len(t, viaProcess, 3)
	assert.Len(t, viaDocs, 3)
	for i := range viaDocs {
		assert.Equal(t, viaDocs[i].Text, viaProcess[i].Text)
		assert.Equal(t, viaDocs[i].DocID, viaProcess[i].DocID)
		assert.Equal(t, viaDocs[i].Metadata["chunk_index"], viaProcess[i].Metadata["chunk_index"])
		assert.Equal(t, viaDocs[i].Metadata["total_chunks"], viaProcess[i].Metadata["total_chunks"])
	}
}

// TestGetChunker_char按名获取 对齐 Python register_chunker("char", CharChunker)
func TestGetChunker_char按名获取(t *testing.T) {
	c, err := GetChunker("char")
	assert.NoError(t, err)
	cc, ok := c.(*CharChunker)
	assert.True(t, ok)
	assert.Equal(t, 512, cc.ChunkSize)
	assert.Equal(t, 50, cc.ChunkOverlap)

	c2, err := GetChunker("char", WithChunkSize(64), WithChunkOverlap(8))
	assert.NoError(t, err)
	cc2, ok := c2.(*CharChunker)
	assert.True(t, ok)
	assert.Equal(t, 64, cc2.ChunkSize)
	assert.Equal(t, 8, cc2.ChunkOverlap)
}

// TestGetChunker_char忽略Extra 对齐 Python **kwargs 透传后吞掉
func TestGetChunker_char忽略Extra(t *testing.T) {
	c, err := GetChunker("char", WithExtra("foo", "bar"))
	assert.NoError(t, err)
	assert.NotNil(t, c)
}

// TestHybridFactory_inner缺省走CharChunker参数校验穿透 ⤴️ 13.6 回填验证：
// 非法参数经默认 CharChunker 构造校验报 155100/155101
func TestHybridFactory_inner缺省走CharChunker参数校验穿透(t *testing.T) {
	_, err := GetChunker("hybrid", WithChunkSize(0))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155100")

	_, err = GetChunker("hybrid", WithChunkSize(10), WithChunkOverlap(10))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")
}
