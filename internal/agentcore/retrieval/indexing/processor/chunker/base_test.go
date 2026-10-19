package chunker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// fakeChunker 用于测试的 Chunker mock 实现：把文本按空格切成块
type fakeChunker struct {
	*BaseChunker
	err error // ChunkText 可注入错误
}

// ChunkText 按空白切分文本
func (f *fakeChunker) ChunkText(text string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if text == "" {
		return []string{}, nil
	}
	return strings.Fields(text), nil
}

// newFakeChunker 构造注入了 self 的 fakeChunker
func newFakeChunker(t *testing.T, size, overlap int, err error) *fakeChunker {
	t.Helper()
	bc, e := NewBaseChunker(size, overlap)
	assert.NoError(t, e)
	fc := &fakeChunker{BaseChunker: bc, err: err}
	bc.SetSelf(fc)
	return fc
}

var _ Chunker = (*fakeChunker)(nil)

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

// TestChunker_接口满足性 验证 fakeChunker 实现 Chunker 接口
func TestChunker_接口满足性(t *testing.T) {
	var _ Chunker = (*fakeChunker)(nil)
}

// TestBaseChunker_ChunkDocuments默认实现 对齐 Python chunk_documents：
// uuid 生成 + metadata 合并 chunk_index/total_chunks/chunk_id + doc_id 传递
func TestBaseChunker_ChunkDocuments默认实现(t *testing.T) {
	fc := newFakeChunker(t, 100, 10, nil)

	docs := []reranker.Document{
		{ID: "doc-1", Text: "hello world foo", Metadata: map[string]any{"source": "test"}},
		{ID: "doc-2", Text: "bar baz", Metadata: nil},
	}
	chunks, err := fc.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 5)

	// doc-1：3 块，metadata 合并 + chunk_index/total_chunks/chunk_id
	c0, c1, c2 := chunks[0], chunks[1], chunks[2]
	assert.Equal(t, "hello", c0.Text)
	assert.Equal(t, "world", c1.Text)
	assert.Equal(t, "foo", c2.Text)
	for _, c := range []common.TextChunk{c0, c1, c2} {
		assert.NotEmpty(t, c.ID)
		assert.Equal(t, "doc-1", c.DocID)
	}
	assert.Equal(t, "test", c0.Metadata["source"])
	assert.Equal(t, 0, c0.Metadata["chunk_index"])
	assert.Equal(t, 1, c1.Metadata["chunk_index"])
	assert.Equal(t, 2, c2.Metadata["chunk_index"])
	assert.Equal(t, 3, c0.Metadata["total_chunks"])
	assert.Equal(t, c0.ID, c0.Metadata["chunk_id"])
	assert.Equal(t, c1.ID, c1.Metadata["chunk_id"])

	// doc-2：metadata 为 nil 时仍生成 chunk 元数据（Python **None 会炸，Go 侧防御）
	c3 := chunks[3]
	assert.Equal(t, "doc-2", c3.DocID)
	assert.Equal(t, 0, c3.Metadata["chunk_index"])
	assert.Equal(t, 2, c3.Metadata["total_chunks"])
}

// TestBaseChunker_ChunkDocuments空文档列表 空输入返回空结果
func TestBaseChunker_ChunkDocuments空文档列表(t *testing.T) {
	fc := newFakeChunker(t, 100, 10, nil)
	chunks, err := fc.ChunkDocuments(context.Background(), nil)
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestBaseChunker_ChunkDocuments分块失败中断 任何文档分块失败即中断返回
func TestBaseChunker_ChunkDocuments分块失败中断(t *testing.T) {
	boom := errors.New("boom")
	fc := newFakeChunker(t, 100, 10, boom)
	chunks, err := fc.ChunkDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "x"}})
	assert.Nil(t, chunks)
	assert.Equal(t, boom, err)
}

// TestBaseChunker_Process委托ChunkDocuments 对齐 Python process = chunk_documents
func TestBaseChunker_Process委托ChunkDocuments(t *testing.T) {
	fc := newFakeChunker(t, 100, 10, nil)
	docs := []reranker.Document{{ID: "d", Text: "a b"}}
	viaProcess, err := fc.Process(context.Background(), docs)
	assert.NoError(t, err)
	viaDirect, err := fc.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	// 两次调用各自生成 uuid，不能整 struct 比较；比较确定性字段
	assert.Equal(t, len(viaDirect), len(viaProcess))
	for i := range viaDirect {
		assert.Equal(t, viaDirect[i].Text, viaProcess[i].Text)
		assert.Equal(t, viaDirect[i].DocID, viaProcess[i].DocID)
		assert.Equal(t, viaDirect[i].Metadata["chunk_index"], viaProcess[i].Metadata["chunk_index"])
		assert.Equal(t, viaDirect[i].Metadata["total_chunks"], viaProcess[i].Metadata["total_chunks"])
		assert.Equal(t, viaProcess[i].ID, viaProcess[i].Metadata["chunk_id"]) // 内部一致性
	}
}

// TestBaseChunker_Length默认按字符计数 对齐 Python length_function or len
func TestBaseChunker_Length默认按字符计数(t *testing.T) {
	bc, _ := NewBaseChunker(100, 10)
	assert.Equal(t, 5, bc.Length("hello"))
	assert.Equal(t, 2, bc.Length("你好"))
	bc2, _ := NewBaseChunker(100, 10, func(s string) int { return len(s) })
	assert.Equal(t, 6, bc2.Length("你好")) // 字节数
}

// TestBaseChunker_未注入self时ChunkDocuments报错 standalone 防御路径
// （Python 基类 chunk_text 返回 None 无人消费，Go 侧返回显式错误）
func TestBaseChunker_未注入self时ChunkDocuments报错(t *testing.T) {
	bc, _ := NewBaseChunker(100, 10)
	chunks, err := bc.ChunkDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "x"}})
	assert.Nil(t, chunks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "call SetSelf before ChunkDocuments")
}

// TestDimsProvider_接口满足性 嵌入 BaseChunker 的实现自动满足 DimsProvider
// （方法提升），对齐 Python getattr(inner, "chunk_size", 512) 能取到属性
func TestDimsProvider_接口满足性(t *testing.T) {
	fc := newFakeChunker(t, 2048, 100, nil)
	var c Chunker = fc // 断言需接口语境（具体类型无断言语义）
	p, ok := c.(DimsProvider)
	assert.True(t, ok, "嵌入 BaseChunker 的实现必须满足 DimsProvider")
	size, overlap := p.ChunkDims()
	assert.Equal(t, 2048, size)
	assert.Equal(t, 100, overlap)
}

// TestChunkDims_返回构造值 纯 BaseChunker 直接调用也满足（防御路径）
func TestChunkDims_返回构造值(t *testing.T) {
	bc, err := NewBaseChunker(300, 30)
	assert.NoError(t, err)
	assert.Equal(t, 300, bc.ChunkSize)
	assert.Equal(t, 30, bc.ChunkOverlap)
	size, overlap := bc.ChunkDims()
	assert.Equal(t, 300, size)
	assert.Equal(t, 30, overlap)
}
