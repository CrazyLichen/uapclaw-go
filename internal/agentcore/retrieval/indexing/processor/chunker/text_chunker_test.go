package chunker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
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

// ──────────────────────────── ChunkText（A1 决策） ────────────────────────────

// TestTextChunker_ChunkText_显式错误 Go 专项（A1 决策回归）：
// Python 继承基类 chunk_text 返回 None（事实不可用），Go 返回显式错误
func TestTextChunker_ChunkText_显式错误(t *testing.T) {
	c, err := NewTextChunker(512, 50)
	assert.NoError(t, err)
	chunks, err := c.ChunkText("any text")
	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "does not support ChunkText")
}

// ──────────────────────────── ChunkDocuments ────────────────────────────

// TestTextChunker_ChunkDocuments_预处理生效 对齐 Python test_chunk_documents_with_preprocessing
// Python 基准: 'This   is   document   1'（normalize_whitespace）→ 块文本无三连空格
func TestTextChunker_ChunkDocuments_预处理生效(t *testing.T) {
	c, err := NewTextChunker(100, 10, WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}))
	assert.NoError(t, err)
	docs := []reranker.Document{{
		ID:       "doc_1",
		Text:     "This   is   document   1",
		Metadata: map[string]any{"source": "test"},
	}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.NotContains(t, chunk.Text, "   ")
		assert.Equal(t, "doc_1", chunk.DocID)
		assert.Contains(t, chunk.Metadata, "chunk_index")
	}
}

// TestTextChunker_ChunkDocuments_无预处理 对齐 Python test_chunk_documents_without_preprocessing
func TestTextChunker_ChunkDocuments_无预处理(t *testing.T) {
	c, err := NewTextChunker(100, 10)
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This is document 1"}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.Equal(t, "doc_1", chunk.DocID)
	}
}

// TestTextChunker_ChunkDocuments_多文档 对齐 Python test_chunk_documents_multiple_docs
func TestTextChunker_ChunkDocuments_多文档(t *testing.T) {
	c, err := NewTextChunker(100, 10)
	assert.NoError(t, err)
	docs := []reranker.Document{
		{ID: "doc_1", Text: "This is document 1"},
		{ID: "doc_2", Text: "This is document 2"},
	}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	docIDs := map[string]bool{}
	for _, chunk := range chunks {
		docIDs[chunk.DocID] = true
	}
	assert.True(t, docIDs["doc_1"])
	assert.True(t, docIDs["doc_2"])
}

// TestTextChunker_ChunkDocuments_元数据保留 对齐 Python test_chunk_documents_preserves_metadata
func TestTextChunker_ChunkDocuments_元数据保留(t *testing.T) {
	c, err := NewTextChunker(100, 10)
	assert.NoError(t, err)
	docs := []reranker.Document{{
		ID:       "doc_1",
		Text:     "This is document 1",
		Metadata: map[string]any{"source": "test", "author": "test_author"},
	}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.Equal(t, "test", chunk.Metadata["source"])
		assert.Equal(t, "test_author", chunk.Metadata["author"])
		// 溯源三键
		assert.Contains(t, chunk.Metadata, "chunk_index")
		assert.Contains(t, chunk.Metadata, "total_chunks")
		assert.Contains(t, chunk.Metadata, "chunk_id")
		assert.Equal(t, chunk.ID, chunk.Metadata["chunk_id"])
	}
}

// TestTextChunker_ChunkDocuments_预处理后空文本 Go 专项：清洗后空串 → 0 块
// （inner CharChunker.ChunkText("") 返回 []）
func TestTextChunker_ChunkDocuments_预处理后空文本(t *testing.T) {
	c, err := NewTextChunker(100, 10, WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}))
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "   \n\t  "}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestTextChunker_ChunkDocuments_实际分块值 Go 专项（真实 CharChunker 路径，Python 对拍推演）
// 'This is a test text'(19 字符) size=10 overlap=2 → 3 块（对齐 CharChunker 既有测试）
func TestTextChunker_ChunkDocuments_实际分块值(t *testing.T) {
	c, err := NewTextChunker(10, 2)
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This is a test text"}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
	assert.Equal(t, "This is a ", chunks[0].Text)
	assert.Equal(t, "a test tex", chunks[1].Text)
	assert.Equal(t, "ext", chunks[2].Text)
}

// ──────────────────────────── Process（局部覆写） ────────────────────────────

// TestTextChunker_Process_路由到覆写版ChunkDocuments Go 专项（A1 决策回归）：
// 经 Process 入口预处理仍生效（BaseChunker.Process 静态解析缺陷的防御，13.8 决策 3 同款）
func TestTextChunker_Process_路由到覆写版ChunkDocuments(t *testing.T) {
	c, err := NewTextChunker(100, 10, WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}))
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This   is   document   1"}}
	chunks, err := c.Process(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.NotContains(t, chunk.Text, "   ")
	}
}
