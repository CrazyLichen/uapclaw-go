package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	retrievalEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeBaseEmbedding 不实现 MultimodalEmbedder 的纯文本嵌入 mock
type fakeBaseEmbedding struct {
	// embedDocsResult EmbedDocuments 返回的嵌入向量
	embedDocsResult [][]float64
	// embedDocsErr EmbedDocuments 返回的错误
	embedDocsErr error
	// embedDocsCalled EmbedDocuments 被调用次数
	embedDocsCalled int
	// embedDocsInput EmbedDocuments 收到的输入文本
	embedDocsInput []string
}

// fakeMultimodalEmbedding 同时实现 BaseEmbedding 和 MultimodalEmbedder 的 mock
type fakeMultimodalEmbedding struct {
	fakeBaseEmbedding
	// embedMMResult EmbedMultimodal 返回的嵌入向量
	embedMMResult []float64
	// embedMMErr EmbedMultimodal 返回的错误
	embedMMErr error
	// embedMMCalled EmbedMultimodal 被调用次数
	embedMMCalled int
	// embedMMInputs EmbedMultimodal 收到的文档列表
	embedMMInputs []*common.MultimodalDocument
}

// ──────────────────────────── 导出函数 ────────────────────────────

// fakeBaseEmbedding 实现 BaseEmbedding

func (f *fakeBaseEmbedding) EmbedQuery(_ context.Context, _ string, _ ...embedding.EmbedOption) ([]float64, error) {
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeBaseEmbedding) EmbedDocuments(_ context.Context, texts []string, _ ...embedding.EmbedOption) ([][]float64, error) {
	f.embedDocsCalled++
	f.embedDocsInput = texts
	if f.embedDocsErr != nil {
		return nil, f.embedDocsErr
	}
	return f.embedDocsResult, nil
}

func (f *fakeBaseEmbedding) Dimension() int {
	return 3
}

func (f *fakeBaseEmbedding) DimensionWithContext(_ context.Context) (int, error) {
	return 3, nil
}

// fakeMultimodalEmbedding 实现 MultimodalEmbedder

func (f *fakeMultimodalEmbedding) EmbedMultimodal(_ context.Context, doc *common.MultimodalDocument, _ ...retrievalEmbedding.MultimodalOption) ([]float64, error) {
	f.embedMMCalled++
	f.embedMMInputs = append(f.embedMMInputs, doc)
	if f.embedMMErr != nil {
		return nil, f.embedMMErr
	}
	return f.embedMMResult, nil
}

// newFakeBaseEmbedding 创建纯文本 fake，自动生成 N 个 3 维向量
func newFakeBaseEmbedding(n int) *fakeBaseEmbedding {
	vecs := make([][]float64, n)
	for i := range vecs {
		vecs[i] = []float64{float64(i), float64(i + 1), float64(i + 2)}
	}
	return &fakeBaseEmbedding{embedDocsResult: vecs}
}

// newFakeMultimodalEmbedding 创建多模态 fake
func newFakeMultimodalEmbedding(textCount, mmCount int) *fakeMultimodalEmbedding {
	textVecs := make([][]float64, textCount)
	for i := range textVecs {
		textVecs[i] = []float64{float64(i) * 10, float64(i)*10 + 1, float64(i)*10 + 2}
	}
	mmVec := []float64{99.0, 99.1, 99.2}
	return &fakeMultimodalEmbedding{
		fakeBaseEmbedding: fakeBaseEmbedding{embedDocsResult: textVecs},
		embedMMResult:     mmVec,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createTempImageFile 在临时目录创建图片文件并返回路径
func createTempImageFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_image.png")
	err := os.WriteFile(path, []byte("fake image data"), 0644)
	require.NoError(t, err)
	return path
}

// makeTextChunks 创建 N 个纯文本 TextChunk
func makeTextChunks(n int) []common.TextChunk {
	chunks := make([]common.TextChunk, n)
	for i := range chunks {
		chunks[i] = common.TextChunk{
			ID:       fmt.Sprintf("chunk-%d", i),
			Text:     fmt.Sprintf("文本内容 %d", i),
			DocID:    "doc-1",
			Metadata: map[string]any{},
		}
	}
	return chunks
}

// makeImageChunk 创建带 image_path 的 TextChunk
func makeImageChunk(imagePath string) common.TextChunk {
	return common.TextChunk{
		ID:    "img-chunk-1",
		Text:  "图片描述文本",
		DocID: "doc-1",
		Metadata: map[string]any{
			"image_path": imagePath,
		},
	}
}

// ──────────────────────────── 测试 ────────────────────────────

// TestComputeChunkEmbeddings_空chunks 测试空分块列表不执行任何操作
func TestComputeChunkEmbeddings_空chunks(t *testing.T) {
	fake := newFakeBaseEmbedding(0)
	chunks := []common.TextChunk{}
	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)
	assert.NoError(t, err)
	assert.Equal(t, 0, fake.embedDocsCalled)
}

// TestComputeChunkEmbeddings_模型不支持多模态 测试纯文本路径
// 对齐 Python: if not callable(embed_multimodal) or use_caption_for_images
func TestComputeChunkEmbeddings_模型不支持多模态(t *testing.T) {
	fake := newFakeBaseEmbedding(3)
	chunks := makeTextChunks(3)

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.NoError(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
	assert.Equal(t, []string{"文本内容 0", "文本内容 1", "文本内容 2"}, fake.embedDocsInput)
	// 验证 Embedding 已填充
	for i, c := range chunks {
		assert.Equal(t, []float64{float64(i), float64(i + 1), float64(i + 2)}, c.Embedding)
	}
}

// TestComputeChunkEmbeddings_useCaptionForImages为true 测试强制纯文本路径
// 对齐 Python: use_caption_for_images=True 时图片分块也走 embed_documents
func TestComputeChunkEmbeddings_useCaptionForImages为true(t *testing.T) {
	fake := newFakeMultimodalEmbedding(1, 0)
	imgPath := createTempImageFile(t)
	chunks := []common.TextChunk{makeImageChunk(imgPath)}

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, true)

	assert.NoError(t, err)
	// useCaptionForImages=true → 走纯文本路径，不调 EmbedMultimodal
	assert.Equal(t, 1, fake.embedDocsCalled)
	assert.Equal(t, 0, fake.embedMMCalled)
}

// TestComputeChunkEmbeddings_图片文本混合 测试混合嵌入路径
// 对齐 Python: image_indices + text_only 分别处理
func TestComputeChunkEmbeddings_图片文本混合(t *testing.T) {
	imgPath := createTempImageFile(t)
	fake := newFakeMultimodalEmbedding(2, 1)
	// 2 个文本 chunk + 1 个图片 chunk
	chunks := []common.TextChunk{
		{ID: "t1", Text: "文本1", DocID: "doc-1", Metadata: map[string]any{}},
		{ID: "t2", Text: "文本2", DocID: "doc-1", Metadata: map[string]any{}},
		makeImageChunk(imgPath),
	}

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.NoError(t, err)
	// 图片 chunk 调 EmbedMultimodal 1 次
	assert.Equal(t, 1, fake.embedMMCalled)
	// 文本 chunk 调 EmbedDocuments 1 次（批量）
	assert.Equal(t, 1, fake.embedDocsCalled)
	// 图片 chunk 的 embedding 是多模态结果
	assert.Equal(t, []float64{99.0, 99.1, 99.2}, chunks[2].Embedding)
	// 文本 chunk 的 embedding 是文本结果
	assert.NotNil(t, chunks[0].Embedding)
	assert.NotNil(t, chunks[1].Embedding)
}

// TestComputeChunkEmbeddings_imagePath文件不存在 测试降级到纯文本
// 对齐 Python: os.path.isfile(img_path) 返回 False 时归入 text_only
func TestComputeChunkEmbeddings_imagePath文件不存在(t *testing.T) {
	fake := newFakeMultimodalEmbedding(1, 0)
	chunks := []common.TextChunk{
		{ID: "img1", Text: "图片描述", DocID: "doc-1", Metadata: map[string]any{"image_path": "/nonexistent/path.png"}},
	}

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.NoError(t, err)
	// 文件不存在 → 降级到纯文本
	assert.Equal(t, 1, fake.embedDocsCalled)
	assert.Equal(t, 0, fake.embedMMCalled)
}

// TestComputeChunkEmbeddings_imagePath非string类型 测试类型断言失败降级
// Python 中 metadata["image_path"] 如果不是 string 也会 falsy，归入 text_only
func TestComputeChunkEmbeddings_imagePath非string类型(t *testing.T) {
	fake := newFakeMultimodalEmbedding(1, 0)
	chunks := []common.TextChunk{
		{ID: "img1", Text: "图片描述", DocID: "doc-1", Metadata: map[string]any{"image_path": 12345}},
	}

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.NoError(t, err)
	// 类型断言失败 → 降级到纯文本
	assert.Equal(t, 1, fake.embedDocsCalled)
	assert.Equal(t, 0, fake.embedMMCalled)
}

// TestComputeChunkEmbeddings_无imagePath键 测试 metadata 中无 image_path 键
// 对齐 Python: (chunk.metadata or {}).get("image_path") 返回 None
func TestComputeChunkEmbeddings_无imagePath键(t *testing.T) {
	fake := newFakeMultimodalEmbedding(2, 0)
	chunks := makeTextChunks(2)

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.NoError(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
	assert.Equal(t, 0, fake.embedMMCalled)
}

// TestComputeChunkEmbeddings_imagePath为空字符串 测试空串降级
// 对齐 Python: img_path 为空串时 if img_path 为 False
func TestComputeChunkEmbeddings_imagePath为空字符串(t *testing.T) {
	fake := newFakeMultimodalEmbedding(1, 0)
	chunks := []common.TextChunk{
		{ID: "img1", Text: "图片描述", DocID: "doc-1", Metadata: map[string]any{"image_path": ""}},
	}

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.NoError(t, err)
	// 空串 → 降级到纯文本
	assert.Equal(t, 1, fake.embedDocsCalled)
	assert.Equal(t, 0, fake.embedMMCalled)
}

// TestComputeChunkEmbeddings_EmbedDocuments失败 测试嵌入失败返回错误
func TestComputeChunkEmbeddings_EmbedDocuments失败(t *testing.T) {
	fake := &fakeBaseEmbedding{embedDocsErr: fmt.Errorf("嵌入服务不可用")}
	chunks := makeTextChunks(2)

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.Error(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
}

// TestComputeChunkEmbeddings_EmbedMultimodal失败 测试多模态嵌入失败返回错误
// 对齐 Python: embed_multimodal 调用失败时立即中断
func TestComputeChunkEmbeddings_EmbedMultimodal失败(t *testing.T) {
	imgPath := createTempImageFile(t)
	fake := &fakeMultimodalEmbedding{
		fakeBaseEmbedding: fakeBaseEmbedding{embedDocsResult: [][]float64{{0.1, 0.2, 0.3}}},
		embedMMErr:        fmt.Errorf("多模态嵌入失败"),
	}
	chunks := []common.TextChunk{makeImageChunk(imgPath)}

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.Error(t, err)
	assert.Equal(t, 1, fake.embedMMCalled)
}

// TestComputeChunkEmbeddings_imagePath是目录 测试目录降级到纯文本
// 对齐 Python: os.path.isfile 对目录返回 False
func TestComputeChunkEmbeddings_imagePath是目录(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeMultimodalEmbedding(1, 0)
	chunks := []common.TextChunk{
		{ID: "img1", Text: "图片描述", DocID: "doc-1", Metadata: map[string]any{"image_path": dir}},
	}

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	// dir 是目录不是文件，isImageChunk 返回 false → 降级到纯文本
	assert.NoError(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
	assert.Equal(t, 0, fake.embedMMCalled)
}

// TestIsImageChunk 测试 isImageChunk 辅助函数的各分支
func TestIsImageChunk(t *testing.T) {
	imgPath := createTempImageFile(t)

	tests := []struct {
		name     string
		chunk    common.TextChunk
		expected bool
	}{
		{
			name:     "nil metadata",
			chunk:    common.TextChunk{Metadata: nil},
			expected: false,
		},
		{
			name:     "无 image_path 键",
			chunk:    common.TextChunk{Metadata: map[string]any{}},
			expected: false,
		},
		{
			name:     "image_path 为非 string 类型",
			chunk:    common.TextChunk{Metadata: map[string]any{"image_path": 123}},
			expected: false,
		},
		{
			name:     "image_path 为空串",
			chunk:    common.TextChunk{Metadata: map[string]any{"image_path": ""}},
			expected: false,
		},
		{
			name:     "image_path 文件不存在",
			chunk:    common.TextChunk{Metadata: map[string]any{"image_path": "/nonexistent/file.png"}},
			expected: false,
		},
		{
			name:     "image_path 是目录",
			chunk:    common.TextChunk{Metadata: map[string]any{"image_path": t.TempDir()}},
			expected: false,
		},
		{
			name:     "image_path 是有效图片文件",
			chunk:    common.TextChunk{Metadata: map[string]any{"image_path": imgPath}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isImageChunk(&tt.chunk)
			assert.Equal(t, tt.expected, result)
		})
	}
}
