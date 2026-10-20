package chunker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakePlainChunker 不嵌入 BaseChunker 的纯接口实现。
//
// 用于验证 DimsProvider 断言失败分支（对齐 Python getattr 取不到
// chunk_size 属性时 fallback 512/50）。
type fakePlainChunker struct{}

// ChunkText 原样返回单块
func (f *fakePlainChunker) ChunkText(text string) ([]string, error) {
	return []string{text}, nil
}

// ChunkDocuments 单文档单块
func (f *fakePlainChunker) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	chunks := make([]common.TextChunk, 0)
	for _, doc := range docs {
		uid := doc.ID + "-plain"
		chunks = append(chunks, common.TextChunk{
			ID:       uid,
			Text:     doc.Text,
			DocID:    doc.ID,
			Metadata: mergeChunkMetadata(doc.Metadata, 0, 1, uid),
		})
	}
	return chunks, nil
}

// Process 等价 ChunkDocuments
func (f *fakePlainChunker) Process(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	return f.ChunkDocuments(ctx, docs)
}

// fakeRecordingInner 记录委托调用的 inner，用于验证逐文档单元素委托
type fakeRecordingInner struct {
	*BaseChunker
	calledWith [][]reranker.Document // 每次委托传入的文档切片快照
}

// ChunkText 原样返回单块
func (f *fakeRecordingInner) ChunkText(text string) ([]string, error) {
	return []string{text}, nil
}

// ChunkDocuments 记录入参后走 BaseChunker 默认实现
func (f *fakeRecordingInner) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	f.calledWith = append(f.calledWith, docs)
	return f.BaseChunker.ChunkDocuments(ctx, docs)
}

// newFakeRecordingInner 构造注入 self 的记录型 inner
func newFakeRecordingInner(t *testing.T) *fakeRecordingInner {
	t.Helper()
	bc, err := NewBaseChunker(100, 10)
	assert.NoError(t, err)
	inner := &fakeRecordingInner{BaseChunker: bc}
	bc.SetSelf(inner)
	return inner
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHybridChunker_inner为nil报错 Go 侧防御（Python 由类型标注保证非空）
func TestNewHybridChunker_inner为nil报错(t *testing.T) {
	h, err := NewHybridChunker(nil)
	assert.Nil(t, h)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "inner chunker is required")
}

// TestNewHybridChunker_尺寸继承自inner 对齐 Python getattr(inner_chunker, "chunk_size", 512)
func TestNewHybridChunker_尺寸继承自inner(t *testing.T) {
	inner, err := NewCharChunker(WithChunkSize(2048), WithChunkOverlap(100))
	assert.NoError(t, err)
	h, err := NewHybridChunker(inner)
	assert.NoError(t, err)
	assert.Equal(t, 2048, h.ChunkSize)
	assert.Equal(t, 100, h.ChunkOverlap)
}

// TestNewHybridChunker_纯接口实现fallback512和50 对齐 Python getattr 取不到属性走默认值
func TestNewHybridChunker_纯接口实现fallback512和50(t *testing.T) {
	h, err := NewHybridChunker(&fakePlainChunker{})
	assert.NoError(t, err)
	assert.Equal(t, 512, h.ChunkSize)
	assert.Equal(t, 50, h.ChunkOverlap)
}

// TestNewHybridChunker_谓词缺省生效 noSplitWhen 可变参数空 → defaultNoSplit
func TestNewHybridChunker_谓词缺省生效(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, err := NewHybridChunker(inner)
	assert.NoError(t, err)
	// source_type=row 文档应单块（默认谓词生效的间接验证，直接验证见 TestDefaultNoSplit）
	docs := []reranker.Document{{
		ID:       "excel-row",
		Text:     "姓名: 张三, 年龄: 25",
		Metadata: map[string]any{"source_type": "row"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1)
}

// TestDefaultNoSplit 对齐 Python _default_no_split 判定：
// metadata 取 (doc.metadata or {})，source_type 值为 "row" 或 "column" 时不分块
func TestDefaultNoSplit(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]any
		want     bool
	}{
		{"row命中", map[string]any{"source_type": "row"}, true},
		{"column命中", map[string]any{"source_type": "column"}, true},
		{"其他类型不命中", map[string]any{"source_type": "paragraph"}, false},
		{"无source_type键", map[string]any{"k": "v"}, false},
		{"metadata为nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &reranker.Document{Metadata: tc.metadata}
			assert.Equal(t, tc.want, defaultNoSplit(doc))
		})
	}
}

// TestHybridChunker_ChunkText纯委托 对齐 Python chunk_text = self._inner.chunk_text
func TestHybridChunker_ChunkText纯委托(t *testing.T) {
	inner, err := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	assert.NoError(t, err)
	h, err := NewHybridChunker(inner)
	assert.NoError(t, err)

	viaHybrid, err := h.ChunkText("This is a test text")
	assert.NoError(t, err)
	viaInner, err := inner.ChunkText("This is a test text")
	assert.NoError(t, err)
	assert.Equal(t, viaInner, viaHybrid)
}

// TestHybridChunker_ChunkDocuments_谓词命中单块 对齐 Python L69-83：
// 命中且文本非空 → 1 块，text 为 strip 后值，metadata 三键规范
func TestHybridChunker_ChunkDocuments_谓词命中单块(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "excel-col",
		Text:     "  Column name: 年龄. Values: 25, 30  ",
		Metadata: map[string]any{"source_type": "column", "sheet_name": "Sheet1"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1)
	c := chunks[0]
	assert.Equal(t, "Column name: 年龄. Values: 25, 30", c.Text, "单块文本必须 strip")
	assert.Equal(t, "excel-col", c.DocID)
	assert.Equal(t, "Sheet1", c.Metadata["sheet_name"], "原 metadata 必须保留")
	assert.Equal(t, 0, c.Metadata["chunk_index"])
	assert.Equal(t, 1, c.Metadata["total_chunks"])
	assert.Equal(t, c.ID, c.Metadata["chunk_id"])
	assert.NotEmpty(t, c.ID)
}

// TestHybridChunker_ChunkDocuments_谓词命中但空白文本 对齐 Python
// (doc.text or "").strip() 为空 → 走 inner。注意 Python 真实行为：
// CharChunker 只拦截空串，非空纯空白经滑窗产出 1 块（整段空白，
// 委托路径不 strip）——不是 0 块
func TestHybridChunker_ChunkDocuments_谓词命中但空白文本(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "empty-row",
		Text:     "   \n\t  ",
		Metadata: map[string]any{"source_type": "row"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1, "空白文本走 inner 后滑窗产出 1 块（整段空白）")
	assert.Equal(t, "   \n\t  ", chunks[0].Text, "inner 委托路径文本不 strip")
	assert.Equal(t, 1, chunks[0].Metadata["total_chunks"])

	// 空字符串对照：谓词命中但 text=="" 走 inner → CharChunker 返回 0 块
	docs2 := []reranker.Document{{
		ID:       "true-empty",
		Text:     "",
		Metadata: map[string]any{"source_type": "row"},
	}}
	chunks2, err := h.ChunkDocuments(context.Background(), docs2)
	assert.NoError(t, err)
	assert.Empty(t, chunks2)
}

// TestHybridChunker_ChunkDocuments_谓词不命中走inner 普通文档正常分块（多块+inner metadata 规范）
func TestHybridChunker_ChunkDocuments_谓词不命中走inner(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "word-doc",
		Text:     "This is a test text",
		Metadata: map[string]any{"source_type": "paragraph"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 3, "19 字符 size=10 overlap=2 → 3 块")
	for i, c := range chunks {
		assert.Equal(t, "word-doc", c.DocID)
		assert.Equal(t, i, c.Metadata["chunk_index"])
		assert.Equal(t, 3, c.Metadata["total_chunks"])
		assert.Equal(t, c.ID, c.Metadata["chunk_id"])
	}
}

// TestHybridChunker_ChunkDocuments_空metadata 对齐 Python (doc.metadata or {}) 防御。
// "plain text" 10 字符经滑窗产出 2 块（第 2 块为重叠尾部残余 "xt"）
func TestHybridChunker_ChunkDocuments_空metadata(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "no-meta",
		Text:     "plain text",
		Metadata: nil,
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 2, "10 字符 size=10 overlap=2 → 主块+重叠尾块")
	assert.Equal(t, 0, chunks[0].Metadata["chunk_index"])
	assert.Equal(t, 1, chunks[1].Metadata["chunk_index"])
	assert.Equal(t, 2, chunks[0].Metadata["total_chunks"])
}

// TestHybridChunker_ChunkDocuments_混合列表 分支交错：row + 长文 + column + 空白 row。
// 空白 row 走 inner 后滑窗产出 1 块（整段空白，Python 真实行为）
func TestHybridChunker_ChunkDocuments_混合列表(t *testing.T) {
	inner := newFakeRecordingInner(t)
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{
		{ID: "r1", Text: "姓名: 张三", Metadata: map[string]any{"source_type": "row"}},
		{ID: "w1", Text: "This is a test text", Metadata: map[string]any{"source_type": "paragraph"}},
		{ID: "c1", Text: "Column name: 年龄. Values: 25", Metadata: map[string]any{"source_type": "column"}},
		{ID: "r2", Text: "  ", Metadata: map[string]any{"source_type": "row"}},
	}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	// r1 单块 + w1 经 fakeRecordingInner（ChunkText 原样单块 → 1 块）
	// + c1 单块 + r2 空白走 inner 滑窗产出 1 块（整段空白）
	assert.Len(t, chunks, 4)
	assert.Equal(t, "r1", chunks[0].DocID)
	assert.Equal(t, 0, chunks[0].Metadata["chunk_index"])
	assert.Equal(t, 1, chunks[0].Metadata["total_chunks"])
	assert.Equal(t, "w1", chunks[1].DocID)
	assert.Equal(t, "c1", chunks[2].DocID)
	assert.Equal(t, "r2", chunks[3].DocID, "空白 row 走 inner 产出空白块")

	// 逐文档单元素委托验证：w1 和 r2 两次调用，每次恰好 1 个文档
	assert.Len(t, inner.calledWith, 2)
	assert.Equal(t, []reranker.Document{docs[1]}, inner.calledWith[0])
	assert.Equal(t, []reranker.Document{docs[3]}, inner.calledWith[1])
}

// TestHybridChunker_ChunkDocuments_inner错误传播 inner 分块失败立即中断
func TestHybridChunker_ChunkDocuments_inner错误传播(t *testing.T) {
	inner := newFakeChunker(t, 100, 10, errors.New("inner boom"))
	h, err := NewHybridChunker(inner)
	assert.NoError(t, err)
	docs := []reranker.Document{
		{ID: "ok", Text: "姓名: 张三", Metadata: map[string]any{"source_type": "row"}},
		{ID: "bad", Text: "will fail"},
	}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.Nil(t, chunks)
	assert.EqualError(t, err, "inner boom")
}

// TestHybridChunker_Process局部覆写 决策 3 回归：Process 入口不绕过谓词判定
// （BaseChunker.Process 静态解析到默认 ChunkDocuments，hybrid 必须局部覆写）
func TestHybridChunker_Process局部覆写(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "excel-row",
		Text:     "姓名: 张三, 年龄: 25",
		Metadata: map[string]any{"source_type": "row"},
	}}
	viaProcess, err := h.Process(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, viaProcess, 1, "Process 入口必须路由到覆写版 ChunkDocuments（谓词生效）")
	assert.Equal(t, "姓名: 张三, 年龄: 25", viaProcess[0].Text)
	assert.Equal(t, 1, viaProcess[0].Metadata["total_chunks"])
}

// TestHybridChunker_自定义谓词覆盖 对齐 Python no_split_when=lambda doc: ...
func TestHybridChunker_自定义谓词覆盖(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	// 自定义谓词：标题文档不切
	h, err := NewHybridChunker(inner, func(doc *reranker.Document) bool {
		_, ok := doc.Metadata["is_title"]
		return ok
	})
	assert.NoError(t, err)
	docs := []reranker.Document{
		{ID: "title", Text: "第一章 概述", Metadata: map[string]any{"is_title": true}},
		{ID: "excel", Text: "姓名: 张三", Metadata: map[string]any{"source_type": "row"}},
	}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	// title 单块（自定义谓词命中）；excel 走 inner（默认谓词被覆盖不再生效）
	assert.Len(t, chunks, 2)
	assert.Equal(t, "title", chunks[0].DocID)
	assert.Equal(t, 1, chunks[0].Metadata["total_chunks"])
	assert.Equal(t, "excel", chunks[1].DocID)
}

// TestGetChunker_hybrid全参路径 工厂全参数集成：尺寸穿透 + 谓词透传
func TestGetChunker_hybrid全参路径(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(64), WithChunkOverlap(8))
	c, err := GetChunker("hybrid", WithInnerChunker(inner), WithNoSplitWhen(func(doc *reranker.Document) bool {
		return doc.Metadata["keep"] == true
	}))
	assert.NoError(t, err)
	h, ok := c.(*HybridChunker)
	assert.True(t, ok)
	assert.Equal(t, 64, h.ChunkSize)
	assert.Equal(t, 8, h.ChunkOverlap)

	docs := []reranker.Document{
		{ID: "keep-doc", Text: "  keep whole  ", Metadata: map[string]any{"keep": true}},
		{ID: "split-doc", Text: "normal paragraph"},
	}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	// keep-doc 自定义谓词命中单块（strip）；split-doc 走 inner(size=64) 1 块
	assert.Len(t, chunks, 2)
	assert.Equal(t, "keep whole", chunks[0].Text)
	assert.Equal(t, "keep-doc", chunks[0].DocID)
}

// TestGetChunker_hybrid尺寸穿透 无 inner 时 chunk_size/chunk_overlap 决定默认 CharChunker
func TestGetChunker_hybrid尺寸穿透(t *testing.T) {
	c, err := GetChunker("hybrid", WithChunkSize(300), WithChunkOverlap(30))
	assert.NoError(t, err)
	h, ok := c.(*HybridChunker)
	assert.True(t, ok)
	assert.Equal(t, 300, h.ChunkSize, "尺寸穿透到默认 CharChunker 再断言提取回 hybrid")
	assert.Equal(t, 30, h.ChunkOverlap)
}
