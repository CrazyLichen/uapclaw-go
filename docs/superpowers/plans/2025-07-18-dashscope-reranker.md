# 4.25 DashScopeReranker 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 DashScope 云服务重排序客户端，支持纯文本和多模态文档输入，并将 MultimodalDocument 从 embedding 包提升到 retrieval/common 公共包。

**Architecture:** 新建 `retrieval/common/` 公共包放置 MultimodalDocument，embedding 和 reranker 包共同引用。DashScopeReranker 嵌入 RerankerBase 独立实现，覆盖 requestParams 和 assembleParams 适配 DashScope API 格式（`input.query/input.documents/parameters` 结构），支持 MultimodalDocument 多模态输入。

**Tech Stack:** Go 1.22+, net/http, net/http/httptest, 内部 exception/logger/utils 包

---

## Task 1: 创建 retrieval/common 包，迁移 MultimodalDocument

**Files:**
- Create: `internal/agentcore/retrieval/common/doc.go`
- Create: `internal/agentcore/retrieval/common/document.go`
- Create: `internal/agentcore/retrieval/common/document_test.go`
- Delete: `internal/agentcore/retrieval/embedding/multimodal.go`
- Delete: `internal/agentcore/retrieval/embedding/multimodal_test.go`

### Step 1.1: 创建 common/doc.go

```go
// Package common 提供 retrieval 子包共享的数据类型和工具函数。
//
// 本包存放 embedding、reranker 等子包共同依赖的文档模型，
// 对齐 Python 的 openjiuwen/core/retrieval/common/ 模块。
//
// 文件目录：
//
//	common/
//	├── doc.go           # 包文档
//	├── document.go      # MultimodalDocument 多模态文档模型
//	└── document_test.go # MultimodalDocument 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/common/document.py
//
// 核心类型索引：
//
//	ModalityKind      — 内容模态类型枚举
//	ModalityField     — 单个模态字段
//	MultimodalDocument — 多模态文档（文本/图片/音频/视频）
package common
```

- [ ] 创建 `internal/agentcore/retrieval/common/doc.go`，写入以上内容

### Step 1.2: 创建 common/document.go

将 `embedding/multimodal.go` 的内容复制过来，做以下修改：

1. 包名 `package embedding` → `package common`
2. 删除 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"`（common 包不依赖 embedding 包）
3. 删除 `MultimodalOption` 结构体（留在 embedding 包）
4. 删除 `MultimodalEmbedder` 接口（留在 embedding 包）
5. 保留所有其他内容：`ModalityKind`、`ModalityField`、`MultimodalDocument`、`NewMultimodalDocument`、`AddField`、`Content`、`DashscopeInput`、`Strip`、`Fields`、`loadMultimodalData`、`loadFromFile`

```go
package common

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
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
		dataID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	d.fields = append(d.fields, ModalityField{Kind: k, Data: resolvedData, ID: dataID})
	if kind == ModalityText {
		d.Text = resolvedData
	}
	return d
}

// Content 返回 OpenAI/vLLM 格式的结构化内容列表。
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
				"type":                         fmt.Sprintf("%s_url", f.Kind),
				fmt.Sprintf("%s_url", f.Kind):  map[string]any{"url": f.Data},
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

	// 文本模态直接读取文件内容
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

	mimeType := mime.TypeByExtension(path)
	if mimeType == "" || !strings.Contains(mimeType, "/") {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无法确定 %s 文件的 MIME 类型: %s", kind, path)),
		))
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

- [ ] 创建 `internal/agentcore/retrieval/common/document.go`，写入以上内容

### Step 1.3: 创建 common/document_test.go

将 `embedding/multimodal_test.go` 的内容复制过来，做以下修改：

1. 包名 `package embedding` → `package common`
2. 删除 import 中 `"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"`（如果存在）

原测试内容保持不变，所有测试函数名和断言逻辑不变。

- [ ] 创建 `internal/agentcore/retrieval/common/document_test.go`

### Step 1.4: 删除 embedding 包中的旧文件

```bash
rm internal/agentcore/retrieval/embedding/multimodal.go
rm internal/agentcore/retrieval/embedding/multimodal_test.go
```

- [ ] 删除旧文件

### Step 1.5: 运行测试确认 common 包测试通过

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/common/... -v
```

Expected: PASS（所有 MultimodalDocument 测试通过）

- [ ] 运行测试确认通过

### Step 1.6: 提交

```bash
git add internal/agentcore/retrieval/common/
git add internal/agentcore/retrieval/embedding/multimodal.go internal/agentcore/retrieval/embedding/multimodal_test.go
git commit -m "feat(retrieval): 创建 common 包，迁移 MultimodalDocument 从 embedding 包"
```

- [ ] 提交

---

## Task 2: 更新 embedding 包引用

**Files:**
- Modify: `internal/agentcore/retrieval/embedding/dashscope.go`
- Modify: `internal/agentcore/retrieval/embedding/openai.go`
- Modify: `internal/agentcore/retrieval/embedding/vllm.go`
- Modify: `internal/agentcore/retrieval/embedding/dashscope_test.go`
- Modify: `internal/agentcore/retrieval/embedding/openai_test.go`
- Modify: `internal/agentcore/retrieval/embedding/vllm_test.go`
- Modify: `internal/agentcore/retrieval/embedding/doc.go`

### Step 2.1: 在 embedding 包中添加 MultimodalOption 和 MultimodalEmbedder 定义

由于 `MultimodalOption` 和 `MultimodalEmbedder` 留在 embedding 包，需要在某个现有文件中定义它们。建议添加到 `common.go` 末尾（该文件已有共享工具函数）。

在 `internal/agentcore/retrieval/embedding/common.go` 中：

1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"`
2. 在文件末尾添加：

```go
// MultimodalOption 多模态嵌入的可选参数。
type MultimodalOption struct {
	// Instruction 多模态嵌入指令（VLLM 使用）
	Instruction string
}

// MultimodalEmbedder 多模态嵌入接口，支持文本+图片+音频+视频。
type MultimodalEmbedder interface {
	BaseEmbedding
	// EmbedMultimodal 将多模态文档转换为向量。
	EmbedMultimodal(ctx context.Context, doc *common.MultimodalDocument, opts ...MultimodalOption) ([]float64, error)
}
```

注意：需确认 `common.go` 中已有的 `BaseEmbedding` 引用路径。如果 `BaseEmbedding` 是 `store/embedding` 包的接口，需要确保 import 正确。

- [ ] 在 `common.go` 末尾添加 `MultimodalOption` 和 `MultimodalEmbedder`

### Step 2.2: 更新所有 embedding 源码文件的引用

对以下文件进行统一修改：

1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"`
2. 所有 `MultimodalDocument` → `common.MultimodalDocument`
3. 所有 `ModalityKind` → `common.ModalityKind`
4. 所有 `ModalityField` → `common.ModalityField`
5. 所有 `NewMultimodalDocument()` → `common.NewMultimodalDocument()`
6. 删除 `ModalityText`/`ModalityImage`/`ModalityAudio`/`ModalityVideo` 的裸引用，改为 `common.ModalityText` 等

需要修改的文件：
- `embedding/dashscope.go` — `EmbedMultimodal` 方法的 `*MultimodalDocument` 参数和 `DashscopeInput()` 调用
- `embedding/openai.go` — `EmbedMultimodal` 方法的 `*MultimodalDocument` 参数和 `Content()` 调用
- `embedding/vllm.go` — `EmbedMultimodal` 方法、`parseMultimodalInput` 函数

- [ ] 更新 `dashscope.go` 引用
- [ ] 更新 `openai.go` 引用
- [ ] 更新 `vllm.go` 引用

### Step 2.3: 更新 embedding 测试文件的引用

对以下测试文件进行同样修改：
- `embedding/dashscope_test.go` — `NewMultimodalDocument()` → `common.NewMultimodalDocument()`, `ModalityText` → `common.ModalityText` 等, `MultimodalEmbedder` 保持不变（仍在 embedding 包）
- `embedding/openai_test.go` — 同上
- `embedding/vllm_test.go` — 同上

- [ ] 更新 `dashscope_test.go` 引用
- [ ] 更新 `openai_test.go` 引用
- [ ] 更新 `vllm_test.go` 引用

### Step 2.4: 更新 embedding/doc.go

```go
// Package embedding 提供向量嵌入模型的具体实现。
//
// 本包实现了 BaseEmbedding 接口的多种提供者：
// APIEmbedding（通用 HTTP）、OpenAIEmbedding（OpenAI SDK）、
// DashscopeEmbedding（DashScope SDK）、VLLMEmbedding（vLLM 多模态）。
// 同时提供 MultimodalEmbedder 接口。
// MultimodalDocument 多模态文档模型已提升到 retrieval/common 包。
//
// 文件目录：
//
//	embedding/
//	├── doc.go           # 包文档
//	├── common.go        # 共享工具函数 + MultimodalOption/MultimodalEmbedder
//	├── callback.go      # Callback 进度回调接口及默认实现
//	├── utils.go         # base64 解码等工具
//	├── api.go           # APIEmbedding 通用 HTTP 客户端
//	├── openai.go        # OpenAIEmbedding（openai-go SDK）
//	├── dashscope.go     # DashscopeEmbedding（dashscope SDK）
//	└── vllm.go          # VLLMEmbedding（组合 OpenAIEmbedding）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/embedding/
//
// 核心类型/接口索引：
//
//	MultimodalEmbedder  — 多模态嵌入接口
//	EmbeddingConfig     — 嵌入模型配置
//	APIEmbedding        — 通用 HTTP 嵌入客户端
//	OpenAIEmbedding     — OpenAI 向量嵌入客户端
//	DashscopeEmbedding  — DashScope 向量嵌入客户端
//	VLLMEmbedding       — vLLM 向量嵌入客户端
package embedding
```

- [ ] 更新 `embedding/doc.go`

### Step 2.5: 运行全部 embedding 测试确认通过

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/embedding/... -v
```

Expected: PASS

- [ ] 运行测试确认通过

### Step 2.6: 运行 reranker 包测试确认未受影响

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/reranker/... -v
```

Expected: PASS

- [ ] 运行测试确认通过

### Step 2.7: 提交

```bash
git add -A internal/agentcore/retrieval/embedding/
git commit -m "refactor(embedding): 更新 MultimodalDocument 引用为 common 包"
```

- [ ] 提交

---

## Task 3: 实现 DashScopeReranker 核心结构

**Files:**
- Create: `internal/agentcore/retrieval/reranker/dashscope.go`

### Step 3.1: 创建 dashscope.go

```go
package reranker

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DashScopeRerankerOption DashScopeReranker 可选配置。
type DashScopeRerankerOption func(*DashScopeReranker)

// DashScopeReranker 阿里云 DashScope 重排序客户端。
//
// 继承 RerankerBase，使用 DashScope 专用的 /services/rerank/text-rerank/text-rerank API，
// 支持纯文本和多模态文档输入。instruct 作为 parameters.instruct 字段传递，
// 而非像 StandardReranker 那样拼入 query 模板。
//
// 对应 Python: openjiuwen/core/retrieval/reranker/dashscope_reranker.py
type DashScopeReranker struct {
	// RerankerBase 嵌入基类
	*RerankerBase
	// httpClient HTTP 客户端
	httpClient *http.Client
	// endPoint API 端点
	endPoint string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// dashScopeEndPoint DashScope 重排序 API 端点
	dashScopeEndPoint = "/services/rerank/text-rerank/text-rerank"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithDashScopeMaxRetries 设置最大重试次数。
func WithDashScopeMaxRetries(n int) DashScopeRerankerOption {
	return func(r *DashScopeReranker) { r.maxRetries = n }
}

// WithDashScopeRetryWait 设置重试等待时间。
func WithDashScopeRetryWait(d time.Duration) DashScopeRerankerOption {
	return func(r *DashScopeReranker) { r.retryWait = d }
}

// WithDashScopeHTTPClient 设置自定义 HTTP 客户端。
func WithDashScopeHTTPClient(client *http.Client) DashScopeRerankerOption {
	return func(r *DashScopeReranker) { r.httpClient = client }
}

// WithDashScopeExtraHeaders 设置额外请求头。
func WithDashScopeExtraHeaders(headers map[string]string) DashScopeRerankerOption {
	return func(r *DashScopeReranker) {
		for k, v := range headers {
			r.headers[k] = v
		}
	}
}

// NewDashScopeReranker 创建 DashScope 重排序客户端。
//
// APIBase 尾部去除 DashScope 端点后缀（对齐 Python removesuffix）。
func NewDashScopeReranker(config reranker.RerankerConfig, opts ...DashScopeRerankerOption) (*DashScopeReranker, error) {
	if err := reranker.ValidateConfig(&config); err != nil {
		return nil, err
	}

	// 去除 APIBase 尾部的端点后缀
	apiBase := strings.TrimSuffix(config.APIBase, dashScopeEndPoint)

	base := NewRerankerBase(config, defaultMaxRetries, defaultRetryWait)

	r := &DashScopeReranker{
		RerankerBase: base,
		httpClient:   &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		endPoint:     dashScopeEndPoint,
	}

	// 如果 APIBase 被截断了，更新 config
	r.config.APIBase = apiBase

	for _, opt := range opts {
		opt(r)
	}

	// 如果未设置 HTTP 客户端超时且 config.Timeout > 0
	if r.httpClient.Timeout == 0 && config.Timeout > 0 {
		r.httpClient.Timeout = time.Duration(config.Timeout) * time.Second
	}

	return r, nil
}

// Rerank 对字符串文档列表进行异步重排序，返回文档到相关性分数的映射。
func (r *DashScopeReranker) Rerank(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankDocs 对 Document 列表进行异步重排序，返回文档 ID 到相关性分数的映射。
func (r *DashScopeReranker) RerankDocs(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankSync 对字符串文档列表进行同步重排序。
func (r *DashScopeReranker) RerankSync(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// RerankDocsSync 对 Document 列表进行同步重排序。
func (r *DashScopeReranker) RerankDocsSync(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// RerankMultimodal 对多模态文档列表进行异步重排序。
func (r *DashScopeReranker) RerankMultimodal(ctx context.Context, query string, docs []*common.MultimodalDocument, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankMultimodalSync 对多模态文档列表进行同步重排序。
func (r *DashScopeReranker) RerankMultimodalSync(ctx context.Context, query string, docs []*common.MultimodalDocument, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// doRerank 执行异步重排序。
func (r *DashScopeReranker) doRerank(ctx context.Context, query string, docs []any, opt *reranker.RerankOption) (map[string]float64, error) {
	headers, params, docIDs, err := r.assembleParams(query, docs, opt)
	if err != nil {
		return nil, err
	}

	cfg := utils.RetryConfig{
		MaxRetries: r.maxRetries,
		RetryWait:  r.retryWait,
		Task:       utils.TaskReranker,
	}

	result, err := utils.RequestWithRetry(ctx, r.httpClient, r.config.APIBase+r.endPoint, params, headers, cfg)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "DashScopeRerank").
			Str("model_provider", r.config.ModelName).
			Err(err).
			Msg("DashScopeReranker 请求失败")
		return nil, err
	}

	return r.parseResponse(result, docIDs), nil
}

// doRerankSync 执行同步重排序。
func (r *DashScopeReranker) doRerankSync(ctx context.Context, query string, docs []any, opt *reranker.RerankOption) (map[string]float64, error) {
	headers, params, docIDs, err := r.assembleParams(query, docs, opt)
	if err != nil {
		return nil, err
	}

	cfg := utils.RetryConfig{
		MaxRetries: r.maxRetries,
		RetryWait:  r.retryWait,
		Task:       utils.TaskReranker,
	}

	result, err := utils.RequestWithRetrySync(ctx, r.httpClient, r.config.APIBase+r.endPoint, params, headers, cfg)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "DashScopeRerankSync").
			Str("model_provider", r.config.ModelName).
			Err(err).
			Msg("DashScopeReranker 同步请求失败")
		return nil, err
	}

	return r.parseResponse(result, docIDs), nil
}

// assembleParams 组装请求参数，将文档和查询合并为 DashScope 格式的请求参数。
// 覆盖基类方法，增加多模态文档支持和类型校验。
// 对齐 Python: DashscopeReranker._assemble_params
func (r *DashScopeReranker) assembleParams(query string, docs []any, opt *reranker.RerankOption) (map[string]string, map[string]any, []string, error) {
	docIDs := make([]string, len(docs))
	texts := make([]string, 0, len(docs))
	multimodalInputs := make([]map[string]any, 0, len(docs))
	hasMultimodal := false

	for i, doc := range docs {
		switch d := doc.(type) {
		case string:
			docIDs[i] = d
			texts = append(texts, d)
		case *reranker.Document:
			docIDs[i] = d.ID
			texts = append(texts, d.Text)
		case *common.MultimodalDocument:
			docIDs[i] = d.Text
			multimodalInputs = append(multimodalInputs, d.DashscopeInput())
			hasMultimodal = true
		default:
			return nil, nil, nil, exception.ValidateError(
				exception.StatusRetrievalRerankerInputInvalid,
				exception.WithParam("error_msg", "input to reranker must be list[str | Document | MultimodalDocument]"),
			)
		}
	}

	topN := r.resolveTopN(opt, len(docs))
	headers := r.requestHeaders()

	// 构建 documents 字段
	var documents any
	if hasMultimodal {
		// 多模态模式：纯文本包装为 {"text": d}，多模态使用 DashscopeInput 结果
		docList := make([]map[string]any, 0, len(docs))
		textIdx := 0
		multimodalIdx := 0
		for _, doc := range docs {
			switch d := doc.(type) {
			case string:
				docList = append(docList, map[string]any{"text": d})
				textIdx++
			case *reranker.Document:
				docList = append(docList, map[string]any{"text": d.Text})
				textIdx++
			case *common.MultimodalDocument:
				docList = append(docList, multimodalInputs[multimodalIdx])
				multimodalIdx++
			}
		}
		documents = docList
	} else {
		documents = texts
	}

	params := r.requestParams(query, documents, topN, opt)

	return headers, params, docIDs, nil
}

// requestParams 构造 DashScope 专用请求参数。
// 覆盖基类方法，使用 DashScope 的 {model, input, parameters} 格式。
// 对齐 Python: DashscopeReranker._request_params
func (r *DashScopeReranker) requestParams(query string, documents any, topN int, opt *reranker.RerankOption) map[string]any {
	parameters := map[string]any{
		"return_documents": false,
		"top_n":            topN,
	}

	// instruct 处理：仅当 CustomInstruct 非空时设置 parameters.instruct
	if opt != nil && opt.CustomInstruct != "" {
		parameters["instruct"] = opt.CustomInstruct
	}

	params := map[string]any{
		"model": r.config.ModelName,
		"input": map[string]any{
			"query":     query,
			"documents": documents,
		},
		"parameters": parameters,
	}

	// 合并 ExtraBody
	for k, v := range r.config.ExtraBody {
		params[k] = v
	}

	// 合并 ExtraParams
	if opt != nil && opt.ExtraParams != nil {
		for k, v := range opt.ExtraParams {
			params[k] = v
		}
	}

	return params
}

// ensure compile-time interface compliance
var _ reranker.BaseReranker = (*DashScopeReranker)(nil)

// suppress unused import warning
var _ = fmt.Sprintf
```

- [ ] 创建 `internal/agentcore/retrieval/reranker/dashscope.go`，写入以上内容

### Step 3.2: 编译确认无语法错误

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/retrieval/reranker/...
```

Expected: 无编译错误

- [ ] 编译确认通过

### Step 3.3: 提交

```bash
git add internal/agentcore/retrieval/reranker/dashscope.go
git commit -m "feat(reranker): 实现 DashScopeReranker 核心结构"
```

- [ ] 提交

---

## Task 4: 编写 DashScopeReranker 单元测试

**Files:**
- Create: `internal/agentcore/retrieval/reranker/dashscope_test.go`

### Step 4.1: 创建 dashscope_test.go

```go
package reranker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/store/reranker"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// dashScopeTestResponse DashScope API 测试响应
const dashScopeTestResponse = `{
	"output": {
		"results": [
			{"index": 0, "relevance_score": 0.95},
			{"index": 1, "relevance_score": 0.75},
			{"index": 2, "relevance_score": 0.50}
		]
	}
}`

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestDashScopeServer 创建模拟 DashScope API 的测试服务器。
func newTestDashScopeServer(responseBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法和路径
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != dashScopeEndPoint {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// 验证 Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))
}

// newTestDashScopeReranker 创建测试用 DashScopeReranker 实例。
func newTestDashScopeReranker(serverURL string) *DashScopeReranker {
	config := reranker.RerankerConfig{
		APIKey:     "test-key",
		APIBase:    serverURL,
		ModelName:  "test-model",
		Timeout:    10,
		ExtraBody:  map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)
	return r
}

func TestNewDashScopeReranker(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, err := NewDashScopeReranker(config)
	if err != nil {
		t.Fatalf("NewDashScopeReranker 返回错误: %v", err)
	}
	if r.endPoint != dashScopeEndPoint {
		t.Errorf("endPoint 期望 %s, 实际 %s", dashScopeEndPoint, r.endPoint)
	}
	if r.config.ModelName != "test-model" {
		t.Errorf("ModelName 期望 test-model, 实际 %s", r.config.ModelName)
	}
}

func TestNewDashScopeReranker_配置缺失时返回错误(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	_, err := NewDashScopeReranker(config)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

func TestNewDashScopeReranker_去除端点后缀(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com/services/rerank/text-rerank/text-rerank",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, err := NewDashScopeReranker(config)
	if err != nil {
		t.Fatalf("NewDashScopeReranker 返回错误: %v", err)
	}
	if r.config.APIBase != "https://dashscope.aliyuncs.com" {
		t.Errorf("APIBase 期望 https://dashscope.aliyuncs.com, 实际 %s", r.config.APIBase)
	}
}

func TestDashScopeReranker_Rerank(t *testing.T) {
	server := newTestDashScopeServer(dashScopeTestResponse)
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	result, err := r.Rerank(context.Background(), "测试查询", []string{"文档1", "文档2", "文档3"})
	if err != nil {
		t.Fatalf("Rerank 返回错误: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("结果数量期望 3, 实际 %d", len(result))
	}
	if result["文档1"] != 0.95 {
		t.Errorf("文档1 分数期望 0.95, 实际 %f", result["文档1"])
	}
	if result["文档2"] != 0.75 {
		t.Errorf("文档2 分数期望 0.75, 实际 %f", result["文档2"])
	}
}

func TestDashScopeReranker_RerankDocs(t *testing.T) {
	server := newTestDashScopeServer(dashScopeTestResponse)
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	docs := []*reranker.Document{
		reranker.NewDocument("文档1"),
		reranker.NewDocument("文档2"),
	}
	result, err := r.RerankDocs(context.Background(), "测试查询", docs)
	if err != nil {
		t.Fatalf("RerankDocs 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("结果数量期望 2, 实际 %d", len(result))
	}
}

func TestDashScopeReranker_RerankMultimodal(t *testing.T) {
	server := newTestDashScopeServer(dashScopeTestResponse)
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	docs := []*common.MultimodalDocument{
		common.NewMultimodalDocument().AddField(common.ModalityText, "多模态文档1"),
		common.NewMultimodalDocument().
			AddField(common.ModalityText, "图文文档").
			AddField(common.ModalityImage, "https://example.com/img.png"),
	}
	result, err := r.RerankMultimodal(context.Background(), "测试查询", docs)
	if err != nil {
		t.Fatalf("RerankMultimodal 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("结果数量期望 2, 实际 %d", len(result))
	}
}

func TestDashScopeReranker_RerankSync(t *testing.T) {
	server := newTestDashScopeServer(dashScopeTestResponse)
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	result, err := r.RerankSync(context.Background(), "测试查询", []string{"文档1", "文档2"})
	if err != nil {
		t.Fatalf("RerankSync 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("结果数量期望 2, 实际 %d", len(result))
	}
}

func TestDashScopeReranker_Rerank_请求失败(t *testing.T) {
	// 使用一个会立即关闭的服务器模拟请求失败
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	_, err := r.Rerank(context.Background(), "测试查询", []string{"文档1"})
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

func TestDashScopeReranker_requestParams(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	params := r.requestParams("测试查询", []string{"文档1", "文档2"}, 2, nil)

	// 验证顶层结构
	if params["model"] != "test-model" {
		t.Errorf("model 期望 test-model, 实际 %v", params["model"])
	}

	// 验证 input 结构
	input, ok := params["input"].(map[string]any)
	if !ok {
		t.Fatal("input 不是 map[string]any 类型")
	}
	if input["query"] != "测试查询" {
		t.Errorf("input.query 期望 测试查询, 实际 %v", input["query"])
	}

	// 验证 parameters 结构
	parameters, ok := params["parameters"].(map[string]any)
	if !ok {
		t.Fatal("parameters 不是 map[string]any 类型")
	}
	if parameters["return_documents"] != false {
		t.Errorf("parameters.return_documents 期望 false, 实际 %v", parameters["return_documents"])
	}
	if parameters["top_n"] != 2 {
		t.Errorf("parameters.top_n 期望 2, 实际 %v", parameters["top_n"])
	}
	// 默认不传 instruct
	if _, exists := parameters["instruct"]; exists {
		t.Error("parameters.instruct 不应存在")
	}
}

func TestDashScopeReranker_requestParams_自定义Instruct(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	customInstruct := "自定义指令"
	opt := reranker.RerankOption{CustomInstruct: customInstruct}
	params := r.requestParams("测试查询", []string{"文档1"}, 1, &opt)

	parameters := params["parameters"].(map[string]any)
	if parameters["instruct"] != customInstruct {
		t.Errorf("parameters.instruct 期望 %s, 实际 %v", customInstruct, parameters["instruct"])
	}
}

func TestDashScopeReranker_requestParams_无Instruct(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	params := r.requestParams("测试查询", []string{"文档1"}, 1, nil)

	parameters := params["parameters"].(map[string]any)
	if _, exists := parameters["instruct"]; exists {
		t.Error("parameters.instruct 不应存在")
	}
}

func TestDashScopeReranker_assembleParams_混合文档类型(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	mmDoc := common.NewMultimodalDocument().
		AddField(common.ModalityText, "多模态文档").
		AddField(common.ModalityImage, "https://example.com/img.png")
	plainDoc := reranker.NewDocument("纯文本文档")

	docs := []any{"字符串文档", plainDoc, mmDoc}
	_, params, docIDs, err := r.assembleParams("测试查询", docs, nil)
	if err != nil {
		t.Fatalf("assembleParams 返回错误: %v", err)
	}

	// 验证 docIDs
	if len(docIDs) != 3 {
		t.Fatalf("docIDs 长度期望 3, 实际 %d", len(docIDs))
	}
	if docIDs[0] != "字符串文档" {
		t.Errorf("docIDs[0] 期望 字符串文档, 实际 %s", docIDs[0])
	}
	if docIDs[1] != plainDoc.ID {
		t.Errorf("docIDs[1] 期望 %s, 实际 %s", plainDoc.ID, docIDs[1])
	}

	// 验证 documents 是 []map[string]any 格式（因为有多模态）
	input := params["input"].(map[string]any)
	documents, ok := input["documents"].([]map[string]any)
	if !ok {
		t.Fatal("documents 不是 []map[string]any 类型（多模态模式下应为 map 列表）")
	}
	if len(documents) != 3 {
		t.Fatalf("documents 长度期望 3, 实际 %d", len(documents))
	}
}

func TestDashScopeReranker_assembleParams_不支持的类型(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	docs := []any{123} // int 类型不支持
	_, _, _, err := r.assembleParams("测试查询", docs, nil)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

func TestDashScopeReranker_parseResponse(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	var responseData map[string]any
	_ = json.Unmarshal([]byte(dashScopeTestResponse), &responseData)

	docIDs := []string{"文档1", "文档2", "文档3"}
	result := r.parseResponse(responseData, docIDs)

	if len(result) != 3 {
		t.Fatalf("结果数量期望 3, 实际 %d", len(result))
	}
	if result["文档1"] != 0.95 {
		t.Errorf("文档1 分数期望 0.95, 实际 %f", result["文档1"])
	}
	if result["文档2"] != 0.75 {
		t.Errorf("文档2 分数期望 0.75, 实际 %f", result["文档2"])
	}
	if result["文档3"] != 0.50 {
		t.Errorf("文档3 分数期望 0.50, 实际 %f", result["文档3"])
	}
}

func TestDashScopeReranker_WithOption(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, err := NewDashScopeReranker(config,
		WithDashScopeMaxRetries(5),
		WithDashScopeRetryWait(200*time.Millisecond),
		WithDashScopeExtraHeaders(map[string]string{"X-Custom": "value"}),
	)
	if err != nil {
		t.Fatalf("NewDashScopeReranker 返回错误: %v", err)
	}
	if r.maxRetries != 5 {
		t.Errorf("maxRetries 期望 5, 实际 %d", r.maxRetries)
	}
	if r.retryWait != 200*time.Millisecond {
		t.Errorf("retryWait 期望 200ms, 实际 %v", r.retryWait)
	}
	if r.headers["X-Custom"] != "value" {
		t.Error("额外请求头未设置")
	}
}
```

- [ ] 创建 `internal/agentcore/retrieval/reranker/dashscope_test.go`

### Step 4.2: 运行测试

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/reranker/... -v -run TestDashScope
```

Expected: 所有测试 PASS

- [ ] 运行测试确认通过

### Step 4.3: 检查覆盖率

```bash
cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/retrieval/reranker/... && go tool cover -func=coverage.out | grep dashscope
```

Expected: 覆盖率 ≥ 85%

- [ ] 确认覆盖率达标

### Step 4.4: 提交

```bash
git add internal/agentcore/retrieval/reranker/dashscope_test.go
git commit -m "test(reranker): 添加 DashScopeReranker 单元测试"
```

- [ ] 提交

---

## Task 5: 更新 reranker 包 doc.go

**Files:**
- Modify: `internal/agentcore/retrieval/reranker/doc.go`

### Step 5.1: 更新 doc.go

```go
// Package reranker 提供重排序模型的具体实现。
//
// 本包实现了 BaseReranker 接口的多种提供者：
// StandardReranker（vLLM 风格 /rerank API）、
// ChatReranker（Chat Completion + logit_bias 实验性方案）和
// DashScopeReranker（阿里云 DashScope 云服务重排序）。
// 同时提供 RerankerBase 默认实现基类。
//
// 文件目录：
//
//	reranker/
//	├── doc.go                # 包文档
//	├── reranker_base.go      # RerankerBase 基类 + 通用方法
//	├── standard.go           # StandardReranker 标准重排序客户端
//	├── chat.go               # ChatReranker 对话式重排序客户端
//	├── dashscope.go          # DashScopeReranker 云服务重排序客户端
//	├── reranker_base_test.go # 基类单元测试
//	├── standard_test.go      # StandardReranker 单元测试
//	├── chat_test.go          # ChatReranker 单元测试
//	└── dashscope_test.go     # DashScopeReranker 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/reranker/standard_reranker.py
//	openjiuwen/core/retrieval/reranker/chat_reranker.py
//	openjiuwen/core/retrieval/reranker/dashscope_reranker.py
//
// 核心类型/接口索引：
//
//	RerankerBase      — 默认实现基类，提供通用 HTTP 请求/响应处理方法
//	StandardReranker  — 标准重排序客户端（/rerank API）
//	ChatReranker      — 对话式重排序客户端（/chat/completions + logprobs）
//	DashScopeReranker — DashScope 云服务重排序客户端（支持多模态）
package reranker
```

- [ ] 更新 `reranker/doc.go`

### Step 5.2: 提交

```bash
git add internal/agentcore/retrieval/reranker/doc.go
git commit -m "docs(reranker): 更新包文档，添加 DashScopeReranker 条目"
```

- [ ] 提交

---

## Task 6: 全量测试与最终验证

**Files:**
- 无新增/修改

### Step 6.1: 运行全量 retrieval 包测试

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/retrieval/... -v
```

Expected: 所有测试 PASS

- [ ] 运行全量测试确认通过

### Step 6.2: 运行覆盖率检查

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/retrieval/...
```

Expected: 所有包覆盖率 ≥ 85%

- [ ] 确认覆盖率达标

### Step 6.3: 运行完整项目编译

```bash
cd /home/opensource/uap-claw-go && go build ./...
```

Expected: 编译成功

- [ ] 确认编译通过

### Step 6.4: 提交（如有未提交的变更）

```bash
git add -A
git status
```

确认所有变更已提交。

- [ ] 确认所有变更已提交
