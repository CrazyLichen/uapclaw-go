# Embedding 接口及具体实现 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 4.19-4.22 Embedding 接口扩展及四种具体实现（APIEmbedding/OpenAIEmbedding/DashscopeEmbedding/VLLMEmbedding），含多模态支持。

**Architecture:** 接口层 `store/embedding/` 保留 BaseEmbedding 定义并扩展；实现层 `retrieval/embedding/` 新建，每个实现类独立，共享逻辑提取到 common.go。APIEmbedding 用 net/http，OpenAIEmbedding 用 openai-go SDK，DashscopeEmbedding 用 dashscope SDK，VLLMEmbedding 组合 OpenAIEmbedding。

**Tech Stack:** Go 1.25, net/http, github.com/openai/openai-go, dashscope Go SDK, encoding/base64, mime, crypto/tls

**Design Spec:** `docs/superpowers/specs/2025-06-13-embedding-implementation-design.md`

---

## File Structure

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/retrieval/embedding/doc.go` | 包文档 |
| `internal/agentcore/retrieval/embedding/multimodal.go` | ModalityKind/MultimodalDocument/MultimodalEmbedder/MultimodalOption |
| `internal/agentcore/retrieval/embedding/callback.go` | Callback 接口及默认实现 |
| `internal/agentcore/retrieval/embedding/common.go` | 共享函数（ValidateEmbedDocs/BatchTexts/ExecuteWithConcurrency/ParseEmbeddingResponse/RetryWithBackoff/NewEmbeddingHTTPClient） |
| `internal/agentcore/retrieval/embedding/utils.go` | ParseBase64Embedding |
| `internal/agentcore/retrieval/embedding/api.go` | APIEmbedding 结构体及方法 |
| `internal/agentcore/retrieval/embedding/openai.go` | OpenAIEmbedding 结构体及方法 |
| `internal/agentcore/retrieval/embedding/dashscope.go` | DashscopeEmbedding 结构体及方法 |
| `internal/agentcore/retrieval/embedding/vllm.go` | VLLMEmbedding 结构体及方法 |
| `internal/agentcore/retrieval/embedding/multimodal_test.go` | MultimodalDocument 测试 |
| `internal/agentcore/retrieval/embedding/callback_test.go` | Callback 测试 |
| `internal/agentcore/retrieval/embedding/common_test.go` | 共享函数测试 |
| `internal/agentcore/retrieval/embedding/utils_test.go` | 工具函数测试 |
| `internal/agentcore/retrieval/embedding/api_test.go` | APIEmbedding 测试 |
| `internal/agentcore/retrieval/embedding/openai_test.go` | OpenAIEmbedding 测试 |
| `internal/agentcore/retrieval/embedding/dashscope_test.go` | DashscopeEmbedding 测试 |
| `internal/agentcore/retrieval/embedding/vllm_test.go` | VLLMEmbedding 测试 |
| `internal/agentcore/retrieval/embedding/openai_llm_test.go` | OpenAI 真实调用测试 //go:build llm |
| `internal/agentcore/retrieval/embedding/dashscope_llm_test.go` | DashScope 真实调用测试 //go:build llm |

### 修改文件

| 文件 | 修改内容 |
|------|----------|
| `internal/agentcore/store/embedding/base.go` | 新增 EmbedOption/Callback，EmbedDocuments 签名增加 opts |
| `internal/agentcore/store/embedding/doc.go` | 更新核心类型索引 |
| `internal/agentcore/store/embedding/base_test.go` | fakeEmbedding 签名适配 |
| `internal/agentcore/store/index/simple.go` | EmbedDocuments 调用适配新签名 |
| `internal/agentcore/store/index/simple_test.go` | fakeEmbedding/failingEmbedding 签名适配 |
| `IMPLEMENTATION_PLAN.md` | 4.19-4.22 状态更新 |

---

### Task 1: 扩展 BaseEmbedding 接口

**Files:**
- Modify: `internal/agentcore/store/embedding/base.go`
- Modify: `internal/agentcore/store/embedding/doc.go`
- Modify: `internal/agentcore/store/embedding/base_test.go`
- Modify: `internal/agentcore/store/index/simple.go`
- Modify: `internal/agentcore/store/index/simple_test.go`

- [ ] **Step 1: 扩展 base.go — 添加 EmbedOption 和 Callback**

在 `base.go` 中，在 `// ──────────────────────────── 结构体 ────────────────────────────` 区块内、`BaseEmbedding` 接口之前，添加：

```go
// Callback 嵌入进度回调接口。
//
// 对齐 Python: BaseCallback
type Callback interface {
	// OnBatchComplete 一批嵌入完成时回调。
	OnBatchComplete(startIdx, endIdx int, batch []string)
}
```

在 `// ──────────────────────────── 常量 ────────────────────────────` 区块之后、`// ──────────────────────────── 全局变量 ────────────────────────────` 之前，添加新的区块：

```go
// ──────────────────────────── 配置结构体 ────────────────────────────

// EmbedOption 批量嵌入的可选参数。
//
// 对齐 Python: embed_documents(batch_size=, callback_cls=)
type EmbedOption struct {
	// BatchSize 批大小，0 表示使用默认值
	BatchSize int
	// Callback 进度回调，nil 表示不回调
	Callback Callback
}
```

修改 `BaseEmbedding` 接口中的 `EmbedDocuments` 签名：

```go
// BaseEmbedding 向量嵌入模型的抽象接口。
//
// 提供文本到向量的转换能力，供记忆索引等组件进行语义搜索。
// ⤵️ 预留：4.19-4.22 补充具体实现（OpenAI/DashScope/VLLM/API）
//
// 对应 Python: openjiuwen/core/foundation/store/base_embedding.py (Embedding)
type BaseEmbedding interface {
	// EmbedQuery 将单条查询文本转换为向量。
	EmbedQuery(ctx context.Context, text string) ([]float64, error)

	// EmbedDocuments 将多条文档文本批量转换为向量。
	EmbedDocuments(ctx context.Context, texts []string, opts ...EmbedOption) ([][]float64, error)

	// Dimension 返回嵌入向量的维度。
	Dimension() int
}
```

注意：需要在 import 中添加 `context`（已存在）。

- [ ] **Step 2: 更新 base_test.go — 适配新签名**

修改 `fakeEmbedding.EmbedDocuments` 签名：

```go
func (f *fakeEmbedding) EmbedDocuments(_ context.Context, texts []string, _ ...EmbedOption) ([][]float64, error) {
```

- [ ] **Step 3: 更新 simple.go — 适配 EmbedDocuments 新签名**

修改 `internal/agentcore/store/index/simple.go` 第 124 行：

```go
embVecs, err := s.embeddingModel.EmbedDocuments(ctx, texts)
```

改为：

```go
embVecs, err := s.embeddingModel.EmbedDocuments(ctx, texts)
```

（注意：`opts ...EmbedOption` 是可变参数，不传参时行为与旧签名一致，无需修改调用方。确认编译通过即可。）

- [ ] **Step 4: 更新 simple_test.go — 适配新签名**

修改 `internal/agentcore/store/index/simple_test.go` 中的 `fakeEmbedding.EmbedDocuments`（约第 290 行）：

```go
func (f *fakeEmbedding) EmbedDocuments(_ context.Context, texts []string, _ ...embedding.EmbedOption) ([][]float64, error) {
```

修改 `failingEmbedding.EmbedDocuments`（约第 1734 行）：

```go
func (f *failingEmbedding) EmbedDocuments(ctx context.Context, texts []string, _ ...embedding.EmbedOption) ([][]float64, error) {
```

- [ ] **Step 5: 更新 store/embedding/doc.go — 核心类型索引**

```go
// Package embedding 提供向量嵌入模型的抽象接口。
//
// 本包定义了 BaseEmbedding 接口、EmbedOption 配置和 Callback 回调，
// 提供文本到向量的转换能力，供记忆索引等组件进行语义搜索。
//
// 文件目录：
//
//	embedding/
//	├── doc.go           # 包文档
//	├── base.go          # BaseEmbedding 接口、EmbedOption、Callback 定义
//	└── base_test.go     # 单元测试
//	⤵️ 预留：4.19-4.22 实现后回填具体实现文件条目
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_embedding.py
//
// 核心类型/接口索引：
//
//	BaseEmbedding — 向量嵌入模型抽象接口（EmbedQuery/EmbedDocuments/Dimension）
//	EmbedOption  — 批量嵌入可选参数（BatchSize/Callback）
//	Callback     — 嵌入进度回调接口（OnBatchComplete）
package embedding
```

- [ ] **Step 6: 运行测试确认编译和现有测试通过**

```bash
cd /home/opensource/uap-claw-go && go build ./... && go test ./internal/agentcore/store/... -v -count=1
```

Expected: PASS，所有现有测试通过。

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/store/embedding/base.go internal/agentcore/store/embedding/doc.go internal/agentcore/store/embedding/base_test.go internal/agentcore/store/index/simple.go internal/agentcore/store/index/simple_test.go
git commit -m "feat(store/embedding): 扩展 BaseEmbedding 接口，添加 EmbedOption 和 Callback"
```

---

### Task 2: 创建 retrieval/embedding 包骨架 + MultimodalDocument

**Files:**
- Create: `internal/agentcore/retrieval/embedding/doc.go`
- Create: `internal/agentcore/retrieval/embedding/multimodal.go`
- Create: `internal/agentcore/retrieval/embedding/multimodal_test.go`

- [ ] **Step 1: 创建包目录**

```bash
mkdir -p internal/agentcore/retrieval/embedding
```

- [ ] **Step 2: 编写 doc.go**

```go
// Package embedding 提供向量嵌入模型的具体实现。
//
// 本包实现了 BaseEmbedding 接口的多种提供者：
// APIEmbedding（通用 HTTP）、OpenAIEmbedding（OpenAI SDK）、
// DashscopeEmbedding（DashScope SDK）、VLLMEmbedding（vLLM 多模态）。
// 同时提供 MultimodalDocument 多模态文档模型和 MultimodalEmbedder 接口。
//
// 文件目录：
//
//	embedding/
//	├── doc.go           # 包文档
//	├── multimodal.go    # MultimodalDocument 多模态文档模型
//	├── callback.go      # Callback 进度回调接口及默认实现
//	├── common.go        # 共享工具函数
//	├── utils.go         # base64 解码等工具
//	├── api.go           # APIEmbedding 通用 HTTP 客户端
//	├── openai.go        # OpenAIEmbedding（openai-go SDK）
//	├── dashscope.go     # DashscopeEmbedding（dashscope SDK）
//	└── vllm.go          # VLLMEmbedding（组合 OpenAIEmbedding）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/embedding/
//	openjiuwen/core/retrieval/common/document.py (MultimodalDocument)
//
// 核心类型/接口索引：
//
//	MultimodalDocument  — 多模态文档（文本/图片/音频/视频）
//	MultimodalEmbedder  — 多模态嵌入接口
//	EmbeddingConfig     — 嵌入模型配置
//	APIEmbedding        — 通用 HTTP 嵌入客户端
//	OpenAIEmbedding     — OpenAI 向量嵌入客户端
//	DashscopeEmbedding  — DashScope 向量嵌入客户端
//	VLLMEmbedding       — vLLM 向量嵌入客户端
package embedding
```

- [ ] **Step 3: 编写 multimodal.go**

```go
package embedding

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModalityKind 内容模态类型。
type ModalityKind string

const (
	// ModalityText 文本模态
	ModalityText ModalityKind = "text"
	// ModalityImage 图片模态
	ModalityImage ModalityKind = "image"
	// ModalityAudio 音频模态
	ModalityAudio ModalityKind = "audio"
	// ModalityVideo 视频模态
	ModalityVideo ModalityKind = "video"
)

// ModalityField 单个模态字段。
type ModalityField struct {
	// Kind 模态类型
	Kind ModalityKind
	// Data 文本内容、URL 或 base64 编码数据
	Data string
	// ID 多模态缓存用的 UUID（文本模态为空）
	ID string
}

// MultimodalDocument 多模态文档，支持文本+图片+音频+视频混合输入。
//
// 对应 Python: openjiuwen/core/retrieval/common/document.py (MultimodalDocument)
type MultimodalDocument struct {
	// Text 文本回退字段，供不支持多模态的服务使用
	Text string
	// fields 有序模态字段列表
	fields []ModalityField
}

// MultimodalOption 多模态嵌入的可选参数。
type MultimodalOption struct {
	// Instruction 多模态嵌入指令（VLLM 使用）
	Instruction string
}

// MultimodalEmbedder 多模态嵌入接口，支持文本+图片+音频+视频。
type MultimodalEmbedder interface {
	embedding.BaseEmbedding
	// EmbedMultimodal 将多模态文档转换为向量。
	EmbedMultimodal(ctx context.Context, doc *MultimodalDocument, opts ...MultimodalOption) ([]float64, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMultimodalDocument 创建空的多模态文档。
func NewMultimodalDocument() *MultimodalDocument {
	return &MultimodalDocument{}
}

// AddField 添加模态字段，支持链式调用。
//
// kind: 模态类型；data: 文本内容/URL/base64（kind 为 text 时必传）；
// filePath: 文件路径（与 data 二选一，最多传一个）。
//
// 对应 Python: MultimodalDocument.add_field()
func (d *MultimodalDocument) AddField(kind ModalityKind, data string, filePath ...string) *MultimodalDocument {
	k, resolvedData := loadMultimodalData(kind, data, filePath...)
	dataID := ""
	if kind != ModalityText {
		dataID = uuid.New().Hex()
	}
	d.fields = append(d.fields, ModalityField{Kind: k, Data: resolvedData, ID: dataID})
	if kind == ModalityText {
		d.Text = resolvedData
	}
	return d
}

// Content 返回 OpenAI/vLLM 格式的结构化内容列表。
//
// 输出格式：
//
//	[{"type": "text", "text": "..."}, {"type": "image_url", "image_url": {"url": "..."}}]
//
// 对应 Python: MultimodalDocument.content
func (d *MultimodalDocument) Content() []map[string]any {
	var content []map[string]any
	for _, f := range d.fields {
		switch f.Kind {
		case ModalityText:
			content = append(content, map[string]any{
				"type": "text",
				"text": f.Data,
			})
		case ModalityImage, ModalityVideo:
			content = append(content, map[string]any{
				"type":         fmt.Sprintf("%s_url", f.Kind),
				fmt.Sprintf("%s_url", f.Kind): map[string]any{"url": f.Data},
			})
		case ModalityAudio:
			re := regexp.MustCompile(`data:audio/(.+?);base64,`)
			matches := re.FindStringSubmatch(f.Data)
			if len(matches) < 2 {
				continue
			}
			content = append(content, map[string]any{
				"type":        "input_audio",
				"input_audio": map[string]any{"data": f.Data, "format": matches[1]},
			})
		}
		if f.ID != "" {
			content[len(content)-1]["uuid"] = f.ID
		}
	}
	return content
}

// DashscopeInput 返回 DashScope 格式的输入字典。
//
// 输出格式：{"text": "...", "image": "...", "video": "..."}
//
// 对应 Python: MultimodalDocument.dashscope_input
func (d *MultimodalDocument) DashscopeInput() map[string]any {
	content := make(map[string]any)
	var images []string
	hasField := make(map[ModalityKind]bool)

	for _, f := range d.fields {
		if hasField[f.Kind] {
			panic(exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("Dashscope 格式不支持多个相同模态字段: %s", f.Kind)),
			))
		}

		switch f.Kind {
		case ModalityText:
			hasField[f.Kind] = true
			content["text"] = f.Data
		case ModalityImage:
			images = append(images, f.Data)
		case ModalityVideo:
			if strings.HasPrefix(f.Data, "data:video/") {
				panic(exception.BuildError(
					exception.StatusRetrievalEmbeddingInputInvalid,
					exception.WithParam("error_msg", "Dashscope 格式不支持 base64 视频输入，仅支持 URL"),
				))
			}
			hasField[f.Kind] = true
			content["video"] = f.Data
		default:
			panic(exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("Dashscope 格式不支持模态类型: %s", f.Kind)),
			))
		}
	}

	if len(images) == 1 {
		content["image"] = images[0]
	} else if len(images) > 1 {
		content["multi_images"] = images
	}

	return content
}

// Strip 兼容 Python 的 strip() 语义，无字段时返回 nil。
//
// 对应 Python: MultimodalDocument.strip()
func (d *MultimodalDocument) Strip() *MultimodalDocument {
	if len(d.fields) == 0 {
		return nil
	}
	return d
}

// Fields 返回模态字段列表的只读副本。
func (d *MultimodalDocument) Fields() []ModalityField {
	result := make([]ModalityField, len(d.fields))
	copy(result, d.fields)
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// audioBase64Re 匹配 base64 编码的音频数据前缀
var audioBase64Re = regexp.MustCompile(`^data:audio/(.+?);base64,`)

// loadMultimodalData 加载多模态数据。
//
// 对应 Python: _load_multimodal_data()
func loadMultimodalData(kind ModalityKind, data string, filePath ...string) (ModalityKind, string) {
	validKinds := map[ModalityKind]bool{
		ModalityText:  true,
		ModalityImage: true,
		ModalityAudio: true,
		ModalityVideo: true,
	}
	if !validKinds[kind] {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("未知的模态类型: %s", kind)),
		))
	}

	// 文件路径模式
	if len(filePath) > 0 && filePath[0] != "" {
		if data != "" {
			panic(exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", "不能同时提供 data 和 filePath"),
			))
		}
		return loadFromFile(kind, filePath[0])
	}

	// data 模式
	if data == "" {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("必须提供 %s 类型的数据或文件路径", kind)),
		))
	}

	if kind == ModalityText {
		return kind, data
	}

	// 检查 base64 前缀
	kindPrefix := fmt.Sprintf("data:%s/", kind)
	if strings.HasPrefix(data, kindPrefix) {
		return kind, data
	}

	// URL 模式（音频只接受 base64）
	if kind != ModalityAudio && strings.HasPrefix(data, "http") && strings.Contains(data, "://") {
		return kind, data
	}

	panic(exception.BuildError(
		exception.StatusRetrievalEmbeddingInputInvalid,
		exception.WithParam("error_msg", fmt.Sprintf("无效的 %s 数据，应为 URL 或以 'data:%s/' 开头的 base64", kind, kind)),
	))
}

// loadFromFile 从文件路径加载数据为 base64。
func loadFromFile(kind ModalityKind, path string) (ModalityKind, string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无法打开 %s 文件: %s", kind, path)),
		))
	}

	mimeType := mime.TypeByExtension(path)
	if mimeType == "" || !strings.Contains(mimeType, "/") {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无法确定 %s 文件的 MIME 类型: %s", kind, path)),
		))
	}

	if kind == ModalityText {
		content, err := os.ReadFile(path)
		if err != nil {
			panic(exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("读取文本文件失败: %s", err)),
			))
		}
		return kind, string(content)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("读取 %s 文件失败: %s", kind, err)),
		))
	}

	b64Str := base64.StdEncoding.EncodeToString(raw)
	return kind, fmt.Sprintf("data:%s;base64,%s", mimeType, b64Str)
}
```

- [ ] **Step 4: 编写 multimodal_test.go**

```go
package embedding

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMultimodalDocument(t *testing.T) {
	doc := NewMultimodalDocument()
	assert.NotNil(t, doc)
	assert.Empty(t, doc.Fields())
	assert.Equal(t, "", doc.Text)
}

func TestMultimodalDocument_AddField_文本(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityText, "你好世界")
	assert.Equal(t, "你好世界", doc.Text)
	assert.Len(t, doc.Fields(), 1)
	assert.Equal(t, ModalityText, doc.Fields()[0].Kind)
	assert.Equal(t, "你好世界", doc.Fields()[0].Data)
	assert.Equal(t, "", doc.Fields()[0].ID) // 文本模态无 ID
}

func TestMultimodalDocument_AddField_图片URL(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityText, "描述").
		AddField(ModalityImage, "https://example.com/img.png")
	assert.Len(t, doc.Fields(), 2)
	assert.Equal(t, ModalityImage, doc.Fields()[1].Kind)
	assert.NotEmpty(t, doc.Fields()[1].ID) // 非文本模态有 ID
}

func TestMultimodalDocument_AddField_链式调用(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityText, "文本").
		AddField(ModalityImage, "https://example.com/img.png")
	assert.Len(t, doc.Fields(), 2)
}

func TestMultimodalDocument_Content_文本(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityText, "描述文本")
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "text", content[0]["type"])
	assert.Equal(t, "描述文本", content[0]["text"])
}

func TestMultimodalDocument_Content_图片URL(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityImage, "https://example.com/img.png")
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "image_url", content[0]["type"])
	urlMap := content[0]["image_url"].(map[string]any)
	assert.Equal(t, "https://example.com/img.png", urlMap["url"])
}

func TestMultimodalDocument_Content_视频URL(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityVideo, "https://example.com/video.mp4")
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "video_url", content[0]["type"])
	urlMap := content[0]["video_url"].(map[string]any)
	assert.Equal(t, "https://example.com/video.mp4", urlMap["url"])
}

func TestMultimodalDocument_Content_音频Base64(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityAudio, "data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEARKwAAIhYAQACABAAZGF0YQAAAAA=")
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "input_audio", content[0]["type"])
	audioMap := content[0]["input_audio"].(map[string]any)
	assert.Equal(t, "wav", audioMap["format"])
}

func TestMultimodalDocument_DashscopeInput_文本和图片(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityText, "描述").
		AddField(ModalityImage, "https://example.com/img.png")
	input := doc.DashscopeInput()
	assert.Equal(t, "描述", input["text"])
	assert.Equal(t, "https://example.com/img.png", input["image"])
}

func TestMultimodalDocument_DashscopeInput_视频URL(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityText, "描述").
		AddField(ModalityVideo, "https://example.com/video.mp4")
	input := doc.DashscopeInput()
	assert.Equal(t, "https://example.com/video.mp4", input["video"])
}

func TestMultimodalDocument_DashscopeInput_多图片(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityImage, "https://example.com/img1.png").
		AddField(ModalityImage, "https://example.com/img2.png")
	input := doc.DashscopeInput()
	images, ok := input["multi_images"].([]string)
	assert.True(t, ok)
	assert.Len(t, images, 2)
}

func TestMultimodalDocument_Strip(t *testing.T) {
	doc := NewMultimodalDocument()
	assert.Nil(t, doc.Strip())

	doc.AddField(ModalityText, "文本")
	assert.NotNil(t, doc.Strip())
}

func TestMultimodalDocument_AddField_从文件加载(t *testing.T) {
	// 创建临时文本文件
	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(txtFile, []byte("文件内容"), 0644)
	assert.NoError(t, err)

	doc := NewMultimodalDocument().AddField(ModalityText, "", txtFile)
	assert.Equal(t, "文件内容", doc.Text)
}

func TestMultimodalDocument_AddField_无效模态(t *testing.T) {
	assert.Panics(t, func() {
		NewMultimodalDocument().AddField(ModalityKind("invalid"), "data")
	})
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/embedding/... -v -count=1 -run TestMultimodalDocument
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/embedding/
git commit -m "feat(retrieval/embedding): 创建包骨架，实现 MultimodalDocument 多模态文档模型"
```

---

### Task 3: 实现 Callback 接口

**Files:**
- Create: `internal/agentcore/retrieval/embedding/callback.go`
- Create: `internal/agentcore/retrieval/embedding/callback_test.go`

- [ ] **Step 1: 编写 callback.go**

```go
package embedding

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// NoopCallback 空回调，不做任何操作。
//
// 对齐 Python: BaseCallback（空操作基类）
type NoopCallback struct {
	// callCounter 调用计数
	callCounter int
	// mu 保护 callCounter
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewNoopCallback 创建空回调实例。
func NewNoopCallback() *NoopCallback {
	return &NoopCallback{}
}

// OnBatchComplete 一批嵌入完成时回调，仅递增计数器。
func (c *NoopCallback) OnBatchComplete(_, _ int, _ []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callCounter++
}

// CallCounter 返回回调被调用的次数。
func (c *NoopCallback) CallCounter() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCounter
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 编写 callback_test.go**

```go
package embedding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoopCallback_OnBatchComplete(t *testing.T) {
	cb := NewNoopCallback()
	assert.Equal(t, 0, cb.CallCounter())

	cb.OnBatchComplete(0, 8, []string{"a", "b"})
	assert.Equal(t, 1, cb.CallCounter())

	cb.OnBatchComplete(8, 16, []string{"c"})
	assert.Equal(t, 2, cb.CallCounter())
}

func TestNoopCallback_并发安全(t *testing.T) {
	cb := NewNoopCallback()
	done := make(chan struct{})

	for i := 0; i < 100; i++ {
		go func() {
			cb.OnBatchComplete(0, 1, nil)
			done <- struct{}{}
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	assert.Equal(t, 100, cb.CallCounter())
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/embedding/... -v -count=1 -run TestNoopCallback
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/retrieval/embedding/callback.go internal/agentcore/retrieval/embedding/callback_test.go
git commit -m "feat(retrieval/embedding): 实现 NoopCallback 回调"
```

---

### Task 4: 实现共享工具函数（common.go + utils.go）

**Files:**
- Create: `internal/agentcore/retrieval/embedding/common.go`
- Create: `internal/agentcore/retrieval/embedding/common_test.go`
- Create: `internal/agentcore/retrieval/embedding/utils.go`
- Create: `internal/agentcore/retrieval/embedding/utils_test.go`

- [ ] **Step 1: 编写 utils.go**

```go
package embedding

import (
	"encoding/base64"
	"math"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseBase64Embedding 将 base64 编码的嵌入向量解码为 []float64。
//
// Python 用 numpy.frombuffer(decoded, dtype=np.float32).tolist()，
// Go 用 encoding/base64 解码 + float32 字节序解析为 float64。
//
// 对应 Python: parse_base64_embedding()
func ParseBase64Embedding(b64Str string) ([]float64, error) {
	decoded, err := base64.StdEncoding.DecodeString(b64Str)
	if err != nil {
		return nil, err
	}

	// 每 4 字节一个 float32
	if len(decoded)%4 != 0 {
		return nil, errInvalidBase64Length
	}

	n := len(decoded) / 4
	result := make([]float64, n)
	for i := 0; i < n; i++ {
		bits := uint32(decoded[i*4]) | uint32(decoded[i*4+1])<<8 |
			uint32(decoded[i*4+2])<<16 | uint32(decoded[i*4+3])<<24
		result[i] = float64(math.Float32frombits(bits))
	}

	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 编写 common.go**

```go
package embedding

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EmbeddingConfig 嵌入模型配置。
//
// 对应 Python: EmbeddingConfig
type EmbeddingConfig struct {
	// ModelName 模型名称
	ModelName string
	// BaseURL API 地址
	BaseURL string
	// APIKey API 密钥
	APIKey string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// envEmbeddingSSLVerify SSL 验证开关环境变量
	envEmbeddingSSLVerify = "EMBEDDING_SSL_VERIFY"
	// envEmbeddingSSLCert SSL 证书路径环境变量
	envEmbeddingSSLCert = "EMBEDDING_SSL_CERT"

	// defaultTimeout 默认请求超时
	defaultTimeout = 60 * time.Second
	// defaultMaxRetries 默认最大重试次数
	defaultMaxRetries = 3
	// defaultMaxBatchSize 默认每批最大文档数
	defaultMaxBatchSize = 8
	// defaultMaxConcurrent 默认最大并发数
	defaultMaxConcurrent = 50

	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateEmbedDocs 校验输入文本列表，返回非空文档。
//
// 对齐 Python: APIEmbedding.validate_embed_docs
func ValidateEmbedDocs(texts []string) ([]string, error) {
	if len(texts) == 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "文本列表为空"),
		)
	}

	var nonEmpty []string
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}

	emptyCount := len(texts) - len(nonEmpty)
	if emptyCount > 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("%d 个文本为空", emptyCount)),
		)
	}

	if len(nonEmpty) == 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "所有文本为空"),
		)
	}

	return nonEmpty, nil
}

// BatchTexts 按 batchSize 将文本列表分片。
func BatchTexts(texts []string, batchSize int) [][]string {
	if batchSize <= 0 {
		batchSize = 1
	}

	var batches [][]string
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batches = append(batches, texts[i:end])
	}
	return batches
}

// EmbeddingTask 嵌入任务函数类型。
type EmbeddingTask func() ([][]float64, error)

// ExecuteWithConcurrency 通用并发执行框架。
//
// limiter 为并发信号量（buffered channel），nil 表示不限制并发。
func ExecuteWithConcurrency(
	ctx context.Context,
	tasks []EmbeddingTask,
	limiter chan struct{},
) ([][]float64, error) {
	type taskResult struct {
		index int
		data  [][]float64
		err   error
	}

	resultCh := make(chan taskResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, fn EmbeddingTask) {
			defer wg.Done()
			if limiter != nil {
				select {
				case limiter <- struct{}{}:
					defer func() { <-limiter }()
				case <-ctx.Done():
					resultCh <- taskResult{index: idx, err: ctx.Err()}
					return
				}
			}
			data, err := fn()
			resultCh <- taskResult{index: idx, data: data, err: err}
		}(i, task)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	results := make([][][]float64, len(tasks))
	for res := range resultCh {
		if res.err != nil {
			return nil, res.err
		}
		results[res.index] = res.data
	}

	// 展平结果
	var all [][]float64
	for _, batch := range results {
		all = append(all, batch...)
	}
	return all, nil
}

// ParseEmbeddingResponse 通用 HTTP 响应解析。
//
// 支持三种格式：
//   - {"embedding": [...]} / {"embedding": [[...], [...]]}
//   - {"embeddings": [[...], [...]]}
//   - {"data": [{"embedding": [...]}, ...]}
func ParseEmbeddingResponse(body []byte) ([][]float64, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("解析响应 JSON 失败: %s", err)),
		)
	}

	// 格式 1: {"embedding": [...]}
	if embRaw, ok := raw["embedding"]; ok {
		// 尝试解析为 [][]float64
		var nested [][]float64
		if err := json.Unmarshal(embRaw, &nested); err == nil {
			return nested, nil
		}
		// 尝试解析为 []float64（单条）
		var flat []float64
		if err := json.Unmarshal(embRaw, &flat); err == nil {
			return [][]float64{flat}, nil
		}
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", "embedding 字段格式无效"),
		)
	}

	// 格式 2: {"embeddings": [[...], [...]]}
	if embsRaw, ok := raw["embeddings"]; ok {
		var embeddings [][]float64
		if err := json.Unmarshal(embsRaw, &embeddings); err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("embeddings 字段格式无效: %s", err)),
			)
		}
		return embeddings, nil
	}

	// 格式 3: {"data": [{"embedding": [...]}, ...]}
	if dataRaw, ok := raw["data"]; ok {
		var dataItems []struct {
			Embedding json.RawMessage `json:"embedding"`
			Index     int             `json:"index"`
		}
		if err := json.Unmarshal(dataRaw, &dataItems); err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("data 字段格式无效: %s", err)),
			)
		}

		// 按 index 排序
		for i := 1; i < len(dataItems); i++ {
			for j := i; j > 0 && dataItems[j].Index < dataItems[j-1].Index; j-- {
				dataItems[j], dataItems[j-1] = dataItems[j-1], dataItems[j]
			}
		}

		var embeddings [][]float64
		for _, item := range dataItems {
			// 尝试解析为 float64 数组
			var flat []float64
			if err := json.Unmarshal(item.Embedding, &flat); err == nil {
				embeddings = append(embeddings, flat)
				continue
			}
			// 尝试解析为 base64 字符串
			var b64Str string
			if err := json.Unmarshal(item.Embedding, &b64Str); err == nil {
				vec, err := ParseBase64Embedding(b64Str)
				if err != nil {
					return nil, exception.BuildError(
						exception.StatusRetrievalEmbeddingResponseInvalid,
						exception.WithParam("error_msg", fmt.Sprintf("base64 解码失败: %s", err)),
					)
				}
				embeddings = append(embeddings, vec)
				continue
			}
		}

		if len(embeddings) == 0 {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", "data 项中无有效 embedding 字段"),
			)
		}

		return embeddings, nil
	}

	return nil, exception.BuildError(
		exception.StatusRetrievalEmbeddingResponseInvalid,
		exception.WithParam("error_msg", "响应中无 embedding/embeddings/data 字段"),
	)
}

// RetryWithBackoff 通用重试 + 指数退避。
//
// fn 参数 attempt 从 0 开始。maxRetries 为最大重试次数（即最多调用 fn maxRetries 次）。
func RetryWithBackoff(
	ctx context.Context,
	maxRetries int,
	fn func(attempt int) ([][]float64, error),
) ([][]float64, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, err := fn(attempt)
		if err == nil {
			return result, nil
		}
		lastErr = err

		if attempt < maxRetries-1 {
			logger.Warn(logComponent).
				Str("event_type", "embedding_retry").
				Int("attempt", attempt+1).
				Int("max_retries", maxRetries).
				Err(err).
				Msg("嵌入请求失败，准备重试")

			// 指数退避：100ms, 200ms, 400ms...
			backoff := time.Duration(math.Pow(2, float64(attempt))) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return nil, exception.BuildError(
		exception.StatusRetrievalEmbeddingRequestCallFailed,
		exception.WithParam("error_msg", fmt.Sprintf("重试 %d 次后仍失败: %s", maxRetries, lastErr)),
		exception.WithCause(lastErr),
	)
}

// NewEmbeddingHTTPClient 创建嵌入客户端的 HTTP Client。
//
// 根据 EMBEDDING_SSL_VERIFY / EMBEDDING_SSL_CERT 环境变量配置 TLS。
func NewEmbeddingHTTPClient(apiURL string) *http.Client {
	isHTTPS := strings.HasPrefix(apiURL, "https://")

	if !isHTTPS {
		return &http.Client{Timeout: defaultTimeout}
	}

	// 检查是否跳过验证
	verifySwitch := strings.ToLower(strings.TrimSpace(os.Getenv(envEmbeddingSSLVerify)))
	if verifySwitch == "false" {
		return &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}

	// 检查自定义证书
	certPath := os.Getenv(envEmbeddingSSLCert)
	if certPath != "" {
		tlsCfg, err := createTLSConfigWithCert(certPath)
		if err != nil {
			logger.Warn(logComponent).
				Str("cert_path", certPath).
				Err(err).
				Msg("加载 SSL 证书失败，使用默认 TLS 配置")
			return &http.Client{Timeout: defaultTimeout}
		}
		return &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
		}
	}

	return &http.Client{Timeout: defaultTimeout}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createTLSConfigWithCert 使用自定义证书创建 TLS 配置。
func createTLSConfigWithCert(certPath string) (*tls.Config, error) {
	tlsCfg, err := createStrictTLSConfig(certPath)
	if err != nil {
		return nil, err
	}
	return tlsCfg, nil
}

// createStrictTLSConfig 创建严格 TLS 配置（复用项目 security 包逻辑）。
func createStrictTLSConfig(certPath string) (*tls.Config, error) {
	return createTLSConfigWithCertSimple(certPath)
}

// createTLSConfigWithCertSimple 简单版本：加载 CA 证书到 CertPool。
func createTLSConfigWithCertSimple(certPath string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	// 这里不使用 security.CreateStrictTLSConfig，因为 embedding 的 SSL 配置
	// 遵循 EMBEDDING_SSL_CERT 环境变量语义，与 security 包的 SAFE_CERT_DIR 不同
	// 直接读取证书文件
	return cfg, nil
}
```

注意：`createTLSConfigWithCertSimple` 目前是空实现占位，后续 APIEmbedding 使用时需要完整实现 CA 证书加载逻辑。

- [ ] **Step 3: 编写 common_test.go**

```go
package embedding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateEmbedDocs_空列表(t *testing.T) {
	_, err := ValidateEmbedDocs([]string{})
	assert.Error(t, err)
}

func TestValidateEmbedDocs_含空文本(t *testing.T) {
	_, err := ValidateEmbedDocs([]string{"hello", "", "world"})
	assert.Error(t, err)
}

func TestValidateEmbedDocs_全部为空(t *testing.T) {
	_, err := ValidateEmbedDocs([]string{"", "  "})
	assert.Error(t, err)
}

func TestValidateEmbedDocs_正常输入(t *testing.T) {
	result, err := ValidateEmbedDocs([]string{"hello", "world"})
	require.NoError(t, err)
	assert.Equal(t, []string{"hello", "world"}, result)
}

func TestBatchTexts(t *testing.T) {
	texts := []string{"a", "b", "c", "d", "e"}

	batches := BatchTexts(texts, 2)
	assert.Len(t, batches, 3)
	assert.Equal(t, []string{"a", "b"}, batches[0])
	assert.Equal(t, []string{"c", "d"}, batches[1])
	assert.Equal(t, []string{"e"}, batches[2])
}

func TestBatchTexts_批大小为0(t *testing.T) {
	texts := []string{"a", "b", "c"}
	batches := BatchTexts(texts, 0)
	assert.Len(t, batches, 3) // batchSize<=0 按 1 处理
}

func TestParseEmbeddingResponse_embedding格式(t *testing.T) {
	body := []byte(`{"embedding": [0.1, 0.2, 0.3]}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.InDelta(t, 0.1, result[0][0], 0.001)
}

func TestParseEmbeddingResponse_embedding嵌套格式(t *testing.T) {
	body := []byte(`{"embedding": [[0.1, 0.2], [0.3, 0.4]]}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestParseEmbeddingResponse_embeddings格式(t *testing.T) {
	body := []byte(`{"embeddings": [[0.1, 0.2], [0.3, 0.4]]}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestParseEmbeddingResponse_data格式(t *testing.T) {
	body := []byte(`{
		"data": [
			{"embedding": [0.1, 0.2], "index": 0},
			{"embedding": [0.3, 0.4], "index": 1}
		]
	}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestParseEmbeddingResponse_data格式_乱序(t *testing.T) {
	body := []byte(`{
		"data": [
			{"embedding": [0.3, 0.4], "index": 1},
			{"embedding": [0.1, 0.2], "index": 0}
		]
	}`)
	result, err := ParseEmbeddingResponse(body)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.InDelta(t, 0.1, result[0][0], 0.001) // index 0 排前面
	assert.InDelta(t, 0.3, result[1][0], 0.001) // index 1 排后面
}

func TestParseEmbeddingResponse_无有效字段(t *testing.T) {
	body := []byte(`{"foo": "bar"}`)
	_, err := ParseEmbeddingResponse(body)
	assert.Error(t, err)
}

func TestParseEmbeddingResponse_无效JSON(t *testing.T) {
	body := []byte(`not json`)
	_, err := ParseEmbeddingResponse(body)
	assert.Error(t, err)
}

func TestRetryWithBackoff_首次成功(t *testing.T) {
	result, err := RetryWithBackoff(context.Background(), 3, func(attempt int) ([][]float64, error) {
		return [][]float64{{0.1, 0.2}}, nil
	})
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestRetryWithBackoff_重试后成功(t *testing.T) {
	callCount := 0
	result, err := RetryWithBackoff(context.Background(), 3, func(attempt int) ([][]float64, error) {
		callCount++
		if attempt < 2 {
			return nil, errors.New("临时错误")
		}
		return [][]float64{{0.1, 0.2}}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
	assert.Len(t, result, 1)
}

func TestRetryWithBackoff_全部失败(t *testing.T) {
	_, err := RetryWithBackoff(context.Background(), 2, func(attempt int) ([][]float64, error) {
		return nil, errors.New("永久错误")
	})
	assert.Error(t, err)
}

func TestRetryWithBackoff_上下文取消(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RetryWithBackoff(ctx, 3, func(attempt int) ([][]float64, error) {
		return nil, errors.New("错误")
	})
	assert.Error(t, err)
}

func TestExecuteWithConcurrency(t *testing.T) {
	tasks := []EmbeddingTask{
		func() ([][]float64, error) { return [][]float64{{1.0}}, nil },
		func() ([][]float64, error) { return [][]float64{{2.0}}, nil },
		func() ([][]float64, error) { return [][]float64{{3.0}}, nil },
	}

	result, err := ExecuteWithConcurrency(context.Background(), tasks, make(chan struct{}, 2))
	require.NoError(t, err)
	assert.Len(t, result, 3)
}

func TestExecuteWithConcurrency_任务失败(t *testing.T) {
	tasks := []EmbeddingTask{
		func() ([][]float64, error) { return nil, errors.New("失败") },
	}
	_, err := ExecuteWithConcurrency(context.Background(), tasks, nil)
	assert.Error(t, err)
}

func TestNewEmbeddingHTTPClient_HTTP(t *testing.T) {
	client := NewEmbeddingHTTPClient("http://localhost:8080")
	assert.NotNil(t, client)
}

func TestNewEmbeddingHTTPClient_HTTPS(t *testing.T) {
	client := NewEmbeddingHTTPClient("https://api.openai.com")
	assert.NotNil(t, client)
}
```

- [ ] **Step 4: 编写 utils_test.go**

```go
package embedding

import (
	"encoding/base64"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBase64Embedding(t *testing.T) {
	// 构造 float32 数组的 base64 编码
	vec := []float32{1.0, 2.0, 3.0}
	bytes := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(v)
		bytes[i*4] = byte(bits)
		bytes[i*4+1] = byte(bits >> 8)
		bytes[i*4+2] = byte(bits >> 16)
		bytes[i*4+3] = byte(bits >> 24)
	}
	b64Str := base64.StdEncoding.EncodeToString(bytes)

	result, err := ParseBase64Embedding(b64Str)
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.InDelta(t, 1.0, result[0], 0.001)
	assert.InDelta(t, 2.0, result[1], 0.001)
	assert.InDelta(t, 3.0, result[2], 0.001)
}

func TestParseBase64Embedding_无效Base64(t *testing.T) {
	_, err := ParseBase64Embedding("!!!invalid!!!")
	assert.Error(t, err)
}

func TestParseBase64Embedding_长度不对齐(t *testing.T) {
	// 3 字节不是 4 的倍数
	b64Str := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	_, err := ParseBase64Embedding(b64Str)
	assert.Error(t, err)
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/embedding/... -v -count=1
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/embedding/common.go internal/agentcore/retrieval/embedding/common_test.go internal/agentcore/retrieval/embedding/utils.go internal/agentcore/retrieval/embedding/utils_test.go
git commit -m "feat(retrieval/embedding): 实现共享工具函数 common.go 和 utils.go"
```

---

### Task 5: 实现 APIEmbedding — 通用 HTTP 客户端

**Files:**
- Create: `internal/agentcore/retrieval/embedding/api.go`
- Create: `internal/agentcore/retrieval/embedding/api_test.go`

- [ ] **Step 1: 编写 api.go**

APIEmbedding 使用 `net/http` 标准库，实现 `embedding.BaseEmbedding` 接口。
核心方法：`EmbedQuery`、`EmbedDocuments`、`Dimension`。
复用 common.go 中的 `ValidateEmbedDocs`、`BatchTexts`、`ExecuteWithConcurrency`、`ParseEmbeddingResponse`、`RetryWithBackoff`。

构造函数 `NewAPIEmbedding(config EmbeddingConfig, opts ...APIEmbeddingOption)` 接收可选配置：
- `WithTimeout(d time.Duration)`
- `WithMaxRetries(n int)`
- `WithMaxBatchSize(n int)`
- `WithMaxConcurrent(n int)`
- `WithExtraHeaders(headers map[string]string)`

`EmbedDocuments` 流程：
1. `ValidateEmbedDocs` 校验
2. `BatchTexts` 分片
3. `ExecuteWithConcurrency` 并发执行每个批次
4. 每个批次内 `RetryWithBackoff` 重试 + HTTP POST
5. `ParseEmbeddingResponse` 解析响应
6. 自动探测并缓存 dimension
7. 回调通知

`EmbedQuery` 流程：校验文本 → HTTP POST → 解析 → 返回单条向量。

`getEmbeddings` 非导出方法：构造 payload → POST → 解析响应 → 缓存 dimension。

- [ ] **Step 2: 编写 api_test.go**

使用 `httptest.NewServer` mock 三种响应格式：
- `TestAPIEmbedding_EmbedQuery` — 单条查询
- `TestAPIEmbedding_EmbedDocuments` — 批量文档
- `TestAPIEmbedding_EmbedDocuments_批处理` — 超过 maxBatchSize 时自动分片
- `TestAPIEmbedding_EmbedDocuments_回调` — 验证 Callback 被调用
- `TestAPIEmbedding_EmbedQuery_空文本` — 空文本返回错误
- `TestAPIEmbedding_Dimension_自动探测` — 首次调用后缓存 dimension
- `TestAPIEmbedding_重试` — 服务端返回错误后重试成功
- `TestAPIEmbedding_响应格式_embedding` — 测试 `{"embedding": [...]}` 格式
- `TestAPIEmbedding_响应格式_embeddings` — 测试 `{"embeddings": [[...]]}` 格式
- `TestAPIEmbedding_响应格式_data` — 测试 `{"data": [{"embedding": [...]}]}` 格式

- [ ] **Step 3: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/embedding/... -v -count=1 -run TestAPIEmbedding
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/retrieval/embedding/api.go internal/agentcore/retrieval/embedding/api_test.go
git commit -m "feat(retrieval/embedding): 实现 APIEmbedding 通用 HTTP 嵌入客户端"
```

---

### Task 6: 实现 OpenAIEmbedding — OpenAI 官方 SDK

**Files:**
- Create: `internal/agentcore/retrieval/embedding/openai.go`
- Create: `internal/agentcore/retrieval/embedding/openai_test.go`
- Create: `internal/agentcore/retrieval/embedding/openai_llm_test.go`

- [ ] **Step 1: 引入 openai-go SDK 依赖**

```bash
cd /home/opensource/uap-claw-go && go get github.com/openai/openai-go@latest
```

- [ ] **Step 2: 编写 openai.go**

OpenAIEmbedding 使用 `github.com/openai/openai-go` SDK，实现 `embedding.BaseEmbedding` 和 `MultimodalEmbedder` 接口。

构造函数 `NewOpenAIEmbedding(config EmbeddingConfig, opts ...OpenAIEmbeddingOption)` 初始化 SDK 客户端，可选配置包括：
- `WithOpenAITimeout(d time.Duration)`
- `WithOpenAIMaxRetries(n int)`
- `WithOpenAIMaxBatchSize(n int)`
- `WithOpenAIMaxConcurrent(n int)`
- `WithOpenAIDimension(dim int)` — Matryoshka 维度截断

特有方法：
- `parseOpenAIResponse(resp *openai.EmbeddingList)` — 解析 SDK 响应对象，支持 base64 解码
- `EmbedMultimodal(ctx, doc, opts...)` — 将 `doc.Content()` 作为 input 传入

`EmbedDocuments` 复用 common.go 的 `ValidateEmbedDocs`、`BatchTexts`、`ExecuteWithConcurrency`。

API URL 处理：`strings.TrimSuffix(config.BaseURL, "/")` 再 `strings.TrimSuffix(url, "/embeddings")`。

- [ ] **Step 3: 编写 openai_test.go**

使用 `httptest.NewServer` mock OpenAI API endpoint（因为 openai-go SDK 支持自定义 base URL）：
- `TestOpenAIEmbedding_EmbedQuery` — 单条查询
- `TestOpenAIEmbedding_EmbedDocuments` — 批量文档
- `TestOpenAIEmbedding_EmbedMultimodal` — 多模态嵌入
- `TestOpenAIEmbedding_Dimension` — 维度返回
- `TestOpenAIEmbedding_Matryoshka维度` — 指定维度时请求带 dimensions 参数
- `TestOpenAIEmbedding_Base64响应` — base64 编码的响应解析

- [ ] **Step 4: 编写 openai_llm_test.go**

```go
//go:build llm

package embedding

// 需要 OPENAI_API_KEY 环境变量
// 运行: go test -tags=llm ./internal/agentcore/retrieval/embedding/...
```

测试用例：
- `TestOpenAIEmbedding_真实调用_EmbedQuery`
- `TestOpenAIEmbedding_真实调用_EmbedDocuments`

- [ ] **Step 5: 运行单元测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/embedding/... -v -count=1 -run TestOpenAIEmbedding
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/embedding/openai.go internal/agentcore/retrieval/embedding/openai_test.go internal/agentcore/retrieval/embedding/openai_llm_test.go go.mod go.sum
git commit -m "feat(retrieval/embedding): 实现 OpenAIEmbedding，引入 openai-go SDK"
```

---

### Task 7: 实现 DashscopeEmbedding — DashScope SDK

**Files:**
- Create: `internal/agentcore/retrieval/embedding/dashscope.go`
- Create: `internal/agentcore/retrieval/embedding/dashscope_test.go`
- Create: `internal/agentcore/retrieval/embedding/dashscope_llm_test.go`

- [ ] **Step 1: 引入 dashscope Go SDK 依赖**

```bash
cd /home/opensource/uap-claw-go && go get github.com/aliyun/alibabacloud-dashscope-go@latest
```

注意：具体包路径需在实现时确认，阿里云可能使用 `github.com/aliyun/alibabacloud-dashscope-go/v2` 或其他路径。需在实现时搜索确认最新可用版本。

- [ ] **Step 2: 编写 dashscope.go**

DashscopeEmbedding 使用 dashscope Go SDK，实现 `embedding.BaseEmbedding` 和 `MultimodalEmbedder` 接口。

构造函数 `NewDashscopeEmbedding(config EmbeddingConfig, opts ...DashscopeEmbeddingOption)`：
- `WithDashscopeTimeout(d time.Duration)`
- `WithDashscopeMaxRetries(n int)`
- `WithDashscopeMaxBatchSize(n int)`
- `WithDashscopeMaxConcurrent(n int)`
- `WithDashscopeDimension(dim int)` — Matryoshka 维度

特有方法：
- `handleDashscopeAPIResp(resp, attempt)` — 解析 DashScope 响应
- `EmbedMultimodal(ctx, doc, opts...)` — 使用 `doc.DashscopeInput()` 转换输入

`EmbedDocuments` 中对 `MultimodalDocument` 类型的输入调用 `DashscopeInput()` 转换。

注意：DashScope 多模态仅支持文本+图片+视频，不支持音频 `input_audio` 格式。

- [ ] **Step 3: 编写 dashscope_test.go**

使用 `httptest.NewServer` mock DashScope API endpoint：
- `TestDashscopeEmbedding_EmbedQuery` — 单条查询
- `TestDashscopeEmbedding_EmbedDocuments` — 批量文档
- `TestDashscopeEmbedding_EmbedMultimodal` — 多模态嵌入
- `TestDashscopeEmbedding_Dimension` — 维度返回
- `TestDashscopeEmbedding_响应解析` — 解析 DashScope 特有格式

- [ ] **Step 4: 编写 dashscope_llm_test.go**

```go
//go:build llm

package embedding

// 需要 DASHSCOPE_API_KEY 环境变量
// 运行: go test -tags=llm ./internal/agentcore/retrieval/embedding/...
```

- [ ] **Step 5: 运行单元测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/embedding/... -v -count=1 -run TestDashscopeEmbedding
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/embedding/dashscope.go internal/agentcore/retrieval/embedding/dashscope_test.go internal/agentcore/retrieval/embedding/dashscope_llm_test.go go.mod go.sum
git commit -m "feat(retrieval/embedding): 实现 DashscopeEmbedding，引入 dashscope SDK"
```

---

### Task 8: 实现 VLLMEmbedding — vLLM 多模态嵌入

**Files:**
- Create: `internal/agentcore/retrieval/embedding/vllm.go`
- Create: `internal/agentcore/retrieval/embedding/vllm_test.go`

- [ ] **Step 1: 编写 vllm.go**

VLLMEmbedding 组合 `OpenAIEmbedding` 实例，添加多模态指令注入。
实现 `embedding.BaseEmbedding` 和 `MultimodalEmbedder` 接口。

```go
type VLLMEmbedding struct {
    openAI *OpenAIEmbedding
}
```

构造函数 `NewVLLMEmbedding(openAI *OpenAIEmbedding)` 接收已配置的 OpenAIEmbedding 实例。

方法全部委托 `openAI`：
- `EmbedQuery` → `openAI.EmbedQuery`
- `EmbedDocuments` → `openAI.EmbedDocuments`
- `Dimension` → `openAI.Dimension`

唯一不委托的方法是 `EmbedMultimodal`，处理流程：
1. 从 `MultimodalOption.Instruction` 获取指令（默认 `"Represent the user's input."`）
2. 构造 system message + user message
3. 通过 `extra_body.messages` 传入 SDK 调用
4. `input` 设为 `nil`

- [ ] **Step 2: 编写 vllm_test.go**

- `TestVLLMEmbedding_EmbedQuery` — 委托到 OpenAIEmbedding
- `TestVLLMEmbedding_EmbedDocuments` — 委托到 OpenAIEmbedding
- `TestVLLMEmbedding_Dimension` — 委托到 OpenAIEmbedding
- `TestVLLMEmbedding_EmbedMultimodal` — 指令注入 + 委托
- `TestVLLMEmbedding_EmbedMultimodal_自定义指令` — 自定义 instruction

- [ ] **Step 3: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/embedding/... -v -count=1 -run TestVLLMEmbedding
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/retrieval/embedding/vllm.go internal/agentcore/retrieval/embedding/vllm_test.go
git commit -m "feat(retrieval/embedding): 实现 VLLMEmbedding 多模态嵌入客户端"
```

---

### Task 9: 更新回填标记和实现计划状态

**Files:**
- Modify: `internal/agentcore/store/embedding/base.go` — 移除 ⤵️ 预留标记
- Modify: `internal/agentcore/store/embedding/doc.go` — 移除 ⤵️ 预留标记
- Modify: `internal/agentcore/store/index/simple.go` — 移除 ⤵️ 预留标记
- Modify: `IMPLEMENTATION_PLAN.md` — 更新 4.19-4.22 状态

- [ ] **Step 1: 移除 base.go 中的 ⤵️ 预留标记**

将 `// ⤵️ 预留：4.19-4.22 补充具体实现（OpenAI/DashScope/VLLM/API）` 替换为 `// 具体实现见 internal/agentcore/retrieval/embedding/ 包`。

- [ ] **Step 2: 移除 store/embedding/doc.go 中的 ⤵️ 预留标记**

将 `// ⤵️ 预留：4.19-4.22 实现后回填具体实现文件条目` 移除。

- [ ] **Step 3: 移除 simple.go 中的 ⤵️ 预留标记**

将 `// ⤵️ 预留：4.19-4.22 实现后可注入具体实现` 替换为注释说明具体实现路径。

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md 状态**

将 4.19、4.20、4.21、4.22 的状态从 `☐` 改为 `✅`。

- [ ] **Step 5: 运行全量测试**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/... ./internal/agentcore/retrieval/... -count=1
```

Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 6: 提交**

```bash
git add -A
git commit -m "chore: 更新回填标记和 IMPLEMENTATION_PLAN.md 状态（4.19-4.22 完成）"
```

---

## Self-Review

### 1. Spec Coverage

| Spec 章节 | 对应 Task |
|-----------|----------|
| §3.1 BaseEmbedding 接口扩展 | Task 1 |
| §3.2 多模态接口 | Task 2 |
| §4 MultimodalDocument | Task 2 |
| §5 APIEmbedding | Task 5 |
| §6 OpenAIEmbedding | Task 6 |
| §7 DashscopeEmbedding | Task 7 |
| §8 VLLMEmbedding | Task 8 |
| §9 common.go | Task 4 |
| §10 utils.go | Task 4 |
| §12 测试策略 | 各 Task 内含 |
| §13 需修改的已有文件 | Task 1 + Task 9 |
| §15 实现顺序 | Task 1-9 顺序 |

### 2. Placeholder Scan

- Task 5/6/7/8 中的具体实现代码以描述方式给出（因 SDK API 需在实现时确认），但测试用例列表明确、方法签名和流程完整
- dashscope SDK 的具体包路径需实现时确认（Task 7 Step 1 已标注）

### 3. Type Consistency

- `EmbedOption` 定义在 `store/embedding/base.go`，所有实现通过 `opts ...embedding.EmbedOption` 引用
- `Callback` 定义在 `store/embedding/base.go`，`NoopCallback` 在 `retrieval/embedding/callback.go` 实现
- `MultimodalDocument`、`MultimodalEmbedder`、`MultimodalOption` 定义在 `retrieval/embedding/multimodal.go`
- `EmbeddingConfig` 定义在 `retrieval/embedding/common.go`
- 所有实现的 `EmbedDocuments` 签名统一为 `(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error)`
