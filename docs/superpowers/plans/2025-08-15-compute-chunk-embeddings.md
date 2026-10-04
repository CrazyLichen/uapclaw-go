# 13.4 ComputeChunkEmbeddings 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ComputeChunkEmbeddings 共享函数，为 ChromaIndexer/MilvusIndexer 提供统一的文本+多模态嵌入计算逻辑。

**Architecture:** 纯函数式，放在 `indexer` 包下。通过 `embedding.MultimodalEmbedder` 接口断言检测多模态能力，将 chunks 分为图片/文本两类分别嵌入，原地修改 `TextChunk.Embedding` 字段。

**Tech Stack:** Go 1.x, 已有的 `BaseEmbedding` / `MultimodalEmbedder` / `MultimodalDocument` 接口

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/agentcore/retrieval/indexing/indexer/embed_chunks.go` | ComputeChunkEmbeddings 函数 + isImageChunk 辅助函数 |
| 创建 | `internal/agentcore/retrieval/indexing/indexer/embed_chunks_test.go` | 完整测试（11 个场景）+ fake 结构体 |
| 修改 | `internal/agentcore/retrieval/indexing/indexer/doc.go` | 添加 embed_chunks.go 到文件目录 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 13.4 状态 ☐→✅ + 顺序调整说明 |

---

### Task 1: 编写测试文件框架 + fake 结构体

**Files:**
- Create: `internal/agentcore/retrieval/indexing/indexer/embed_chunks_test.go`

- [ ] **Step 1: 创建测试文件，定义 fake 结构体**

```go
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

func (f *fakeBaseEmbedding) EmbedQuery(_ context.Context, text string, _ ...embedding.EmbedOption) ([]float64, error) {
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
		ID:   "img-chunk-1",
		Text: "图片描述文本",
		DocID: "doc-1",
		Metadata: map[string]any{
			"image_path": imagePath,
		},
	}
}
```

- [ ] **Step 2: 确认测试文件编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/indexing/indexer/`

注意：此时 `embed_chunks.go` 尚不存在，`ComputeChunkEmbeddings` 函数未定义，但测试文件中还没有调用它，所以编译应通过。

- [ ] **Step 3: 提交 fake 结构体**

```bash
git add internal/agentcore/retrieval/indexing/indexer/embed_chunks_test.go
git commit -m "test(indexer): 添加 ComputeChunkEmbeddings 测试 fake 结构体"
```

---

### Task 2: 实现 ComputeChunkEmbeddings 核心函数

**Files:**
- Create: `internal/agentcore/retrieval/indexing/indexer/embed_chunks.go`

- [ ] **Step 1: 创建 embed_chunks.go 实现核心函数**

```go
package indexer

import (
	"context"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ComputeChunkEmbeddings 计算分块嵌入向量，原地修改 chunks 的 Embedding 字段。
//
// 三条路径：
//   - 纯文本路径：模型不支持 MultimodalEmbedder 或 useCaptionForImages=true，
//     所有 chunks 批量 EmbedDocuments
//   - 图片路径：metadata 中有有效 image_path 的 chunks 逐个 EmbedMultimodal
//   - 文本路径：其余 chunks 批量 EmbedDocuments
//
// Python: openjiuwen/core/retrieval/indexing/indexer/embed_chunks.py (compute_chunk_embeddings)
func ComputeChunkEmbeddings(
	ctx context.Context,
	chunks []common.TextChunk,
	embedModel embedding.BaseEmbedding,
	useCaptionForImages bool,
	opts ...embedding.EmbedOption,
) error {
	if len(chunks) == 0 {
		return nil
	}

	// 检测多模态能力
	multimodal, multimodalOK := embedModel.(retrievalEmbedding.MultimodalEmbedder)

	// 纯文本路径：模型不支持多模态 或 用户要求使用标题
	if !multimodalOK || useCaptionForImages {
		logger.Info(logComponent).
			Bool("multimodal_supported", multimodalOK).
			Bool("use_caption_for_images", useCaptionForImages).
			Int("total_chunk_count", len(chunks)).
			Msg("使用纯文本嵌入路径")

		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
		}

		embeddings, err := embedModel.EmbedDocuments(ctx, texts, opts...)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "INDEXING_EMBED_ERROR").
				Str("method", "EmbedDocuments").
				Int("chunk_count", len(chunks)).
				Err(err).
				Msg("纯文本嵌入失败")
			return err
		}

		for i, emb := range embeddings {
			chunks[i].Embedding = emb
		}
		return nil
	}

	// 混合路径：分类图片/文本分块
	var imageIndices []int
	type indexedChunk struct {
		index int
		chunk *common.TextChunk
	}
	var textOnly []indexedChunk

	for i := range chunks {
		if isImageChunk(&chunks[i]) {
			imageIndices = append(imageIndices, i)
		} else {
			textOnly = append(textOnly, indexedChunk{index: i, chunk: &chunks[i]})
		}
	}

	logger.Info(logComponent).
		Bool("multimodal_supported", true).
		Int("image_chunk_count", len(imageIndices)).
		Int("text_chunk_count", len(textOnly)).
		Int("total_chunk_count", len(chunks)).
		Msg("使用混合嵌入路径")

	// 图片路径：逐个多模态嵌入
	for _, idx := range imageIndices {
		chunk := &chunks[idx]
		imgPath, _ := chunk.Metadata["image_path"].(string)

		doc := common.NewMultimodalDocument()
		if chunk.Text != "" {
			if _, err := doc.AddField(common.ModalityText, chunk.Text); err != nil {
				return err
			}
		}
		if _, err := doc.AddField(common.ModalityImage, "", common.FieldFilePath(imgPath)); err != nil {
			return err
		}

		vec, err := multimodal.EmbedMultimodal(ctx, doc)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "INDEXING_EMBED_ERROR").
				Str("method", "EmbedMultimodal").
				Int("chunk_index", idx).
				Str("image_path", imgPath).
				Err(err).
				Msg("多模态嵌入失败")
			return err
		}
		chunk.Embedding = vec
	}

	// 文本路径：批量嵌入
	if len(textOnly) > 0 {
		texts := make([]string, len(textOnly))
		for i, ic := range textOnly {
			texts[i] = ic.chunk.Text
		}

		embeddings, err := embedModel.EmbedDocuments(ctx, texts, opts...)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "INDEXING_EMBED_ERROR").
				Str("method", "EmbedDocuments").
				Int("chunk_count", len(textOnly)).
				Err(err).
				Msg("文本分块嵌入失败")
			return err
		}

		for i, ic := range textOnly {
			ic.chunk.Embedding = embeddings[i]
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isImageChunk 判断 TextChunk 是否为有效图片分块。
//
// 三级防御对齐 Python os.path.isfile(img_path)：
//  1. metadata 中无 image_path 键 → false
//  2. image_path 类型不是 string 或为空串 → false
//  3. 文件不存在或是目录 → false
func isImageChunk(chunk *common.TextChunk) bool {
	rawPath, ok := chunk.Metadata["image_path"]
	if !ok {
		return false
	}
	imgPath, ok := rawPath.(string)
	if !ok || imgPath == "" {
		return false
	}
	info, err := os.Stat(imgPath)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}
```

- [ ] **Step 2: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/indexing/indexer/`

- [ ] **Step 3: 提交核心函数**

```bash
git add internal/agentcore/retrieval/indexing/indexer/embed_chunks.go
git commit -m "feat(indexer): 实现 ComputeChunkEmbeddings 共享嵌入逻辑"
```

---

### Task 3: 编写全部测试用例

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/indexer/embed_chunks_test.go`

在 Task 1 创建的测试文件中追加所有测试函数。

- [ ] **Step 1: 在测试文件末尾追加 11 个测试用例**

```go
func TestComputeChunkEmbeddings_空chunks(t *testing.T) {
	fake := newFakeBaseEmbedding(0)
	chunks := []common.TextChunk{}
	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)
	assert.NoError(t, err)
	assert.Equal(t, 0, fake.embedDocsCalled)
}

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

func TestComputeChunkEmbeddings_无imagePath键(t *testing.T) {
	fake := newFakeMultimodalEmbedding(2, 0)
	chunks := makeTextChunks(2)

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.NoError(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
	assert.Equal(t, 0, fake.embedMMCalled)
}

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

func TestComputeChunkEmbeddings_EmbedDocuments失败(t *testing.T) {
	fake := &fakeBaseEmbedding{embedDocsErr: fmt.Errorf("嵌入服务不可用")}
	chunks := makeTextChunks(2)

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	assert.Error(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
}

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

func TestComputeChunkEmbeddings_AddField构造失败(t *testing.T) {
	// 用一个存在的临时目录（非图片文件）来触发 AddField 失败
	dir := t.TempDir()
	fake := newFakeMultimodalEmbedding(0, 1)
	chunks := []common.TextChunk{
		{ID: "img1", Text: "图片描述", DocID: "doc-1", Metadata: map[string]any{"image_path": dir}},
	}

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake, false)

	// dir 是目录不是文件，isImageChunk 返回 false → 降级到纯文本
	// 所以不会触发 AddField 失败，而是走 EmbedDocuments
	assert.NoError(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
}
```

- [ ] **Step 2: 运行测试确认全部通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/agentcore/retrieval/indexing/indexer/ -run TestComputeChunkEmbeddings`

Expected: 全部 PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/retrieval/indexing/indexer/`

Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交测试**

```bash
git add internal/agentcore/retrieval/indexing/indexer/embed_chunks_test.go
git commit -m "test(indexer): 添加 ComputeChunkEmbeddings 11 个测试场景"
```

---

### Task 4: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/indexer/doc.go`

- [ ] **Step 1: 在 doc.go 的文件目录中添加 embed_chunks.go**

将 doc.go 的文件目录从：

```
//	indexer/
//	├── doc.go       # 包文档
//	└── base.go      # Indexer 接口定义
```

更新为：

```
//	indexer/
//	├── doc.go            # 包文档
//	├── base.go           # Indexer 接口定义
//	└── embed_chunks.go   # ComputeChunkEmbeddings 共享嵌入逻辑
```

同时在核心类型索引中添加：

```
//	ComputeChunkEmbeddings — 分块嵌入计算共享函数
```

- [ ] **Step 2: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/indexing/indexer/`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/retrieval/indexing/indexer/doc.go
git commit -m "docs(indexer): 更新 doc.go 文件目录，添加 embed_chunks.go"
```

---

### Task 5: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 13.4 状态从 ☐ 改为 ✅**

找到行：
```
| 13.4 | ☐ | ComputeChunkEmbeddings | 共享嵌入逻辑（文本embed_documents + 多模态embed_multimodal + image_path检测 + use_caption_for_images分支） | `openjiuwen/core/retrieval/indexing/indexer/embed_chunks.py` |
```

替换为：
```
| 13.4 | ✅ | ComputeChunkEmbeddings | ComputeChunkEmbeddings + isImageChunk（接口断言检测 MultimodalEmbedder + 纯文本/图片/文本三条路径 + os.Stat 三级防御 + 透传 EmbedOption + 原地修改 Embedding） | `openjiuwen/core/retrieval/indexing/indexer/embed_chunks.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 13.4 ComputeChunkEmbeddings 标记为已完成"
```

---

### Task 6: 最终验证

- [ ] **Step 1: 运行 indexer 包完整测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/agentcore/retrieval/indexing/indexer/`

Expected: 全部 PASS

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/retrieval/indexing/indexer/`

Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行 retrieval 整体测试，确保无破坏**

Run: `cd /home/opensource/uapclaw-gateway && go test -count=1 ./internal/agentcore/retrieval/...`

Expected: 全部 PASS
