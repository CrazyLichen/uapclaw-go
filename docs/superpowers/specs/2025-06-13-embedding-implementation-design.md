# 4.19-4.22 Embedding 接口及具体实现设计

> 本文档描述 IMPLEMENTATION_PLAN.md 中领域 4 第 4.19-4.22 小节的实现设计。
> 涵盖 Embedding 接口扩展、APIEmbedding 通用客户端、OpenAIEmbedding、DashscopeEmbedding、VLLMEmbedding 及多模态支持。

---

## 1. 背景与目标

### 1.1 现状

- `internal/agentcore/store/embedding/base.go` 已定义 `BaseEmbedding` 接口（`EmbedQuery`/`EmbedDocuments`/`Dimension`），为 4.18 SimpleMemoryIndex 前置定义
- `SimpleMemoryIndex` 已依赖 `BaseEmbedding` 接口，但无具体实现可注入
- Python 端有完整的继承体系：`Embedding` → `APIEmbedding` → `OpenAIEmbedding` → `VLLMEmbedding`，以及 `APIEmbedding` → `DashscopeEmbedding`

### 1.2 目标

实现 4.19-4.22 四个任务：

| 编号 | 内容 | Python 对应 |
|------|------|-------------|
| 4.19 | Embedding 接口 | `base_embedding.py` (Embedding ABC) |
| 4.20 | OpenAIEmbedding | `openai_embedding.py` |
| 4.21 | DashScopeEmbedding | `dashscope_embedding.py` |
| 4.22 | APIEmbedding / VLLMEmbedding | `api_embedding.py` + `vllm_embedding.py` |

同时实现多模态支持（MultimodalDocument + MultimodalEmbedder 接口），与 Python 完全对齐。

---

## 2. 架构决策

### 2.1 目录结构

采用接口层与实现层分离：接口保留在 `store/embedding/`，具体实现放 `retrieval/embedding/`，与 Python 结构对齐：

- 基础接口 `Embedding` → `openjiuwen/core/foundation/store/base_embedding.py`
- 具体实现 → `openjiuwen/core/retrieval/embedding/`

### 2.2 实现方式：接口组合 + 独立实现

每个实现类独立，不使用结构体嵌入模拟 Python 继承。共享逻辑提取到 `common.go` 的独立函数，各实现按需调用。

选择理由：
- Go 没有动态分派，结构体嵌入模拟继承需要显式委托（接口字段注入子类引用），增加复杂性
- 独立实现每个类自包含，无隐式依赖
- 共享逻辑提取为函数后重复代码可控

### 2.3 SDK 选型

| 实现 | SDK | 理由 |
|------|-----|------|
| APIEmbedding | `net/http` 标准库 | 通用 HTTP 客户端，无第三方依赖 |
| OpenAIEmbedding | `github.com/openai/openai-go`（官方 SDK） | 对齐 Python 使用 `openai` SDK |
| DashscopeEmbedding | dashscope Go SDK（官方） | 对齐 Python 使用 `dashscope` SDK |
| VLLMEmbedding | 组合 `OpenAIEmbedding` 实例 | 对齐 Python 继承关系，has-a 组合 |

### 2.4 多模态支持

本次完整实现多模态，包括：
- `MultimodalDocument` 数据模型（文本/图片/音频/视频）
- `MultimodalEmbedder` 接口
- `OpenAIEmbedding.EmbedMultimodal` / `DashscopeEmbedding.EmbedMultimodal` / `VLLMEmbedding.EmbedMultimodal`

---

## 3. 接口设计

### 3.1 BaseEmbedding 接口扩展

文件：`internal/agentcore/store/embedding/base.go`

```go
// EmbedOption 批量嵌入的可选参数
type EmbedOption struct {
    // BatchSize 批大小，0 表示使用默认值
    BatchSize int
    // Callback 进度回调，nil 表示不回调
    Callback Callback
}

// Callback 嵌入进度回调接口
type Callback interface {
    // OnBatchComplete 一批嵌入完成时回调
    OnBatchComplete(startIdx, endIdx int, batch []string)
}

// BaseEmbedding 向量嵌入模型的抽象接口
type BaseEmbedding interface {
    // EmbedQuery 将单条查询文本转换为向量
    EmbedQuery(ctx context.Context, text string) ([]float64, error)
    // EmbedDocuments 将多条文档文本批量转换为向量
    EmbedDocuments(ctx context.Context, texts []string, opts ...EmbedOption) ([][]float64, error)
    // Dimension 返回嵌入向量的维度
    Dimension() int
}
```

变更点：
- `EmbedDocuments` 增加 `opts ...EmbedOption` 参数，对齐 Python 的 `batch_size` 和 `callback_cls`
- 新增 `EmbedOption` 结构体
- 新增 `Callback` 接口，对齐 Python 的 `BaseCallback`
- Python 的同步版本（`embed_query_sync`/`embed_documents_sync`）在 Go 中不需要，Go 函数天然同步，`context.Context` 控制超时

### 3.2 多模态接口

文件：`internal/agentcore/retrieval/embedding/multimodal.go`

```go
// MultimodalEmbedder 多模态嵌入接口，支持文本+图片+音频+视频
type MultimodalEmbedder interface {
    BaseEmbedding
    // EmbedMultimodal 将多模态文档转换为向量
    EmbedMultimodal(ctx context.Context, doc *MultimodalDocument, opts ...MultimodalOption) ([]float64, error)
}

// MultimodalOption 多模态嵌入的可选参数
type MultimodalOption struct {
    // Instruction 多模态嵌入指令（VLLM 使用）
    Instruction string
}
```

---

## 4. MultimodalDocument 数据模型

文件：`internal/agentcore/retrieval/embedding/multimodal.go`

对应 Python: `openjiuwen/core/retrieval/common/document.py` (MultimodalDocument)

```go
// ModalityKind 内容模态类型
type ModalityKind string

const (
    ModalityText  ModalityKind = "text"
    ModalityImage ModalityKind = "image"
    ModalityAudio ModalityKind = "audio"
    ModalityVideo ModalityKind = "video"
)

// ModalityField 单个模态字段
type ModalityField struct {
    // Kind 模态类型
    Kind ModalityKind
    // Data 文本内容、URL 或 base64 编码数据
    Data string
    // ID 多模态缓存用的 UUID（文本模态为空）
    ID string
}

// MultimodalDocument 多模态文档，支持文本+图片+音频+视频混合输入
type MultimodalDocument struct {
    // Text 文本回退字段，供不支持多模态的服务使用
    Text string
    // fields 有序模态字段列表
    fields []ModalityField
}
```

核心方法：

| 方法 | 功能 | 对齐 Python |
|------|------|-------------|
| `AddField(kind, data, filePath...)` | 添加模态字段，支持链式调用 | `add_field()` |
| `Content()` | 返回 OpenAI/vLLM 格式结构化内容 | `content` 属性 |
| `DashscopeInput()` | 返回 DashScope 格式输入字典 | `dashscope_input` 属性 |
| `Strip()` | 无字段时返回 nil | `strip()` |

设计决策：
- `AddField` 用可变参数 `filePath ...string` 代替 Python 的 `data=NOT_SET, file_path=NOT_SET`
- 文件路径加载为 base64 的逻辑放在 `AddField` 内部，MIME 类型用 `mime.TypeByExtension` 自动推断
- 不使用 Python 的 `PrivateAttr` + 缓存模式，Go 端按需计算

`Content()` 输出格式示例：
```json
[
  {"type": "text", "text": "描述文本"},
  {"type": "image_url", "image_url": {"url": "https://..."}},
  {"type": "input_audio", "input_audio": {"data": "data:audio/wav;base64,...", "format": "wav"}}
]
```

`DashscopeInput()` 输出格式示例：
```json
{"text": "描述文本", "image": "https://...", "video": "https://..."}
```

---

## 5. APIEmbedding — 通用 HTTP 客户端

文件：`internal/agentcore/retrieval/embedding/api.go`

对应 Python: `openjiuwen/core/retrieval/embedding/api_embedding.py`

### 5.1 结构体

```go
// EmbeddingConfig 嵌入模型配置
type EmbeddingConfig struct {
    ModelName string    // 模型名称
    BaseURL   string    // API 地址
    APIKey    string    // API 密钥
}

// APIEmbedding 通用 HTTP 嵌入客户端
// 支持 OpenAI 兼容的三种响应格式：embedding/embeddings/data[]
type APIEmbedding struct {
    config       EmbeddingConfig
    timeout      time.Duration
    maxRetries   int
    maxBatchSize int
    limiter      chan struct{}     // 并发信号量
    headers      map[string]string
    dimension    int               // 缓存的向量维度
    httpClient   *http.Client      // 支持 TLS 配置
}
```

### 5.2 方法

| 方法 | 功能 |
|------|------|
| `NewAPIEmbedding(config, opts...)` | 构造函数 |
| `EmbedQuery(ctx, text)` | 单条查询嵌入 |
| `EmbedDocuments(ctx, texts, opts...)` | 批量文档嵌入 |
| `Dimension()` | 返回向量维度 |

### 5.3 HTTP 请求流程

1. 构造 payload: `{"model": modelName, "input": text, **kwargs}`
2. 设置 headers: `Content-Type: application/json` + 可选 `Authorization: Bearer <api_key>`
3. POST 请求，带重试（最多 maxRetries 次）
4. 解析响应：`ParseEmbeddingResponse()` 支持三种格式
5. 自动探测并缓存 dimension

---

## 6. OpenAIEmbedding — OpenAI 官方 SDK 实现

文件：`internal/agentcore/retrieval/embedding/openai.go`

对应 Python: `openjiuwen/core/retrieval/embedding/openai_embedding.py`

### 6.1 结构体

```go
// OpenAIEmbedding OpenAI 向量嵌入客户端
type OpenAIEmbedding struct {
    config             EmbeddingConfig
    client             *openai.Client
    timeout            time.Duration
    maxRetries         int
    maxBatchSize       int
    limiter            chan struct{}
    dimension          int
    matryoshkaDimension bool             // 是否启用 Matryoshka 维度截断
    httpClient         *http.Client
}
```

### 6.2 方法

| 方法 | 功能 |
|------|------|
| `NewOpenAIEmbedding(config, opts...)` | 构造函数，初始化 openai-go SDK 客户端 |
| `EmbedQuery(ctx, text)` | 单条查询嵌入 |
| `EmbedDocuments(ctx, texts, opts...)` | 批量文档嵌入 |
| `EmbedMultimodal(ctx, doc, opts...)` | 多模态嵌入（实现 MultimodalEmbedder） |
| `Dimension()` | 返回向量维度 |

### 6.3 特有逻辑

- **SDK 调用**：用 `openai.Client.Embeddings.New()` 发起请求
- **响应解析**：`parseOpenAIResponse()` — 处理 `openai.Embedding` 对象，支持 base64 解码（`ParseBase64Embedding`）
- **Matryoshka 维度**：初始化时指定 `dimension`，请求时传入 `dimensions` 参数
- **多模态**：`EmbedMultimodal` 将 `MultimodalDocument.Content()` 作为 `input` 传入
- **API URL 处理**：移除末尾 `/` 和 `/embeddings`，与 Python 对齐

### 6.4 复用 common.go 的函数

| 函数 | 使用场景 |
|------|----------|
| `ValidateEmbedDocs` | `EmbedDocuments` 输入校验 |
| `BatchTexts` | `EmbedDocuments` 批量分片 |
| `ExecuteWithConcurrency` | `EmbedDocuments` 并发执行 |
| `RetryWithBackoff` | SDK 调用重试 |

---

## 7. DashscopeEmbedding — 阿里云 DashScope SDK 实现

文件：`internal/agentcore/retrieval/embedding/dashscope.go`

对应 Python: `openjiuwen/core/retrieval/embedding/dashscope_embedding.py`

### 7.1 结构体

```go
// DashscopeEmbedding 阿里云 DashScope 向量嵌入客户端
type DashscopeEmbedding struct {
    config             EmbeddingConfig
    timeout            time.Duration
    maxRetries         int
    maxBatchSize       int
    limiter            chan struct{}
    dimension          int
    matryoshkaDimension bool
    httpClient         *http.Client
}
```

### 7.2 方法

| 方法 | 功能 |
|------|------|
| `NewDashscopeEmbedding(config, opts...)` | 构造函数，初始化 dashscope SDK |
| `EmbedQuery(ctx, text)` | 单条查询嵌入 |
| `EmbedDocuments(ctx, texts, opts...)` | 批量文档嵌入（支持 MultimodalDocument 混合输入） |
| `EmbedMultimodal(ctx, doc, opts...)` | 多模态嵌入（实现 MultimodalEmbedder） |
| `Dimension()` | 返回向量维度 |

### 7.3 特有逻辑

- **SDK 调用**：用 dashscope Go SDK 的 `MultiModalEmbedding.Call()`
- **响应解析**：`handleDashscopeAPIResp()` — 解析 `DashScopeAPIResponse`，提取 `output.embeddings`，按 `index` 排序
- **多模态输入**：`EmbedDocuments` 中对 `MultimodalDocument` 调用 `DashscopeInput()` 转换
- **Matryoshka 维度**：初始化时将 `dimension` 写入请求参数，每次请求自动携带
- DashScope 不支持音频模态的 `input_audio` 格式，多模态仅支持文本+图片+视频

### 7.4 与 Python 端的关键差异

| Python | Go |
|--------|-----|
| `requests.Session` 同步请求 | dashscope Go SDK 同步方法 |
| `aiohttp.ClientSession` 异步请求 | goroutine + ctx，无需分 async/sync |
| `ThreadPoolExecutor` 同步并发 | goroutine + channel |
| `__del__` 析构关闭连接 | Go GC 自动回收，可选 `runtime.SetFinalizer` |

---

## 8. VLLMEmbedding — vLLM 多模态嵌入

文件：`internal/agentcore/retrieval/embedding/vllm.go`

对应 Python: `openjiuwen/core/retrieval/embedding/vllm_embedding.py`

### 8.1 结构体

```go
// VLLMEmbedding vLLM 向量嵌入客户端
// 组合 OpenAIEmbedding 实例，添加多模态指令注入
type VLLMEmbedding struct {
    openAI *OpenAIEmbedding
}
```

### 8.2 方法

| 方法 | 功能 |
|------|------|
| `NewVLLMEmbedding(openAI)` | 构造函数，接收已配置的 OpenAIEmbedding |
| `EmbedQuery(ctx, text)` | 委托 `openAI.EmbedQuery` |
| `EmbedDocuments(ctx, texts, opts...)` | 委托 `openAI.EmbedDocuments` |
| `EmbedMultimodal(ctx, doc, opts...)` | 注入 instruction → 构造 messages → 委托 `openAI` |
| `Dimension()` | 委托 `openAI.Dimension()` |

### 8.3 多模态指令注入

`EmbedMultimodal` 的处理流程：

1. 从 `MultimodalOption.Instruction` 获取指令（默认 `"Represent the user's input."`）
2. 构造 messages：
   ```json
   [
     {"role": "system", "content": [{"type": "text", "text": "instruction..."}]},
     {"role": "user", "content": doc.Content()}
   ]
   ```
3. 通过 `extra_body.messages` 传入 SDK 调用
4. `input` 设为 `nil`（vLLM 多模态模式下 input 由 messages 提供）

---

## 9. 共享逻辑 — common.go

文件：`internal/agentcore/retrieval/embedding/common.go`

```go
// ValidateEmbedDocs 校验输入文本列表，返回非空文档
// 对齐 Python: APIEmbedding.validate_embed_docs
func ValidateEmbedDocs(texts []string) ([]string, error)

// BatchTexts 按 batchSize 将文本列表分片
func BatchTexts(texts []string, batchSize int) [][]string

// ExecuteWithConcurrency 通用并发执行框架
// tasks 为待执行的任务函数，limiter 为并发信号量
func ExecuteWithConcurrency(
    ctx context.Context,
    tasks []func() ([][]float64, error),
    limiter chan struct{},
) ([][]float64, error)

// ParseEmbeddingResponse 通用 HTTP 响应解析
// 支持三种格式：
//   {"embedding": [...]} / {"embeddings": [...]} / {"data": [{"embedding": [...]}]}
func ParseEmbeddingResponse(body []byte) ([][]float64, error)

// RetryWithBackoff 通用重试 + 指数退避
func RetryWithBackoff(
    ctx context.Context,
    maxRetries int,
    fn func(attempt int) ([][]float64, error),
) ([][]float64, error)
```

SSL 配置：

```go
// NewEmbeddingHTTPClient 创建嵌入客户端的 HTTP Client
// 根据 EMBEDDING_SSL_VERIFY / EMBEDDING_SSL_CERT 环境变量配置 TLS
func NewEmbeddingHTTPClient(apiURL string) *http.Client
```

---

## 10. 工具函数 — utils.go

文件：`internal/agentcore/retrieval/embedding/utils.go`

```go
// ParseBase64Embedding 将 base64 编码的嵌入向量解码为 []float64
// Python 用 numpy.frombuffer(decoded, dtype=np.float32).tolist()
// Go 用 encoding/base64 解码 + float32 字节序解析
func ParseBase64Embedding(b64Str string) ([]float64, error)
```

---

## 11. 完整文件目录

```
internal/agentcore/
├── store/embedding/              # 接口层（已有，需修改）
│   ├── doc.go                    # 包文档（需更新文件目录和核心类型索引）
│   ├── base.go                   # BaseEmbedding 接口（需扩展 EmbedOption/Callback）
│   └── base_test.go              # 接口约束测试（需适配新签名）
│
└── retrieval/embedding/          # 实现层（新建）
    ├── doc.go                    # 包文档
    ├── api.go                    # APIEmbedding 通用 HTTP 客户端
    ├── openai.go                 # OpenAIEmbedding（openai-go SDK）
    ├── dashscope.go              # DashscopeEmbedding（dashscope SDK）
    ├── vllm.go                   # VLLMEmbedding（组合 OpenAIEmbedding）
    ├── multimodal.go             # MultimodalDocument + MultimodalEmbedder 接口 + ModalityKind
    ├── callback.go               # Callback 进度回调接口
    ├── common.go                 # 共享工具函数
    ├── utils.go                  # base64 解码等工具
    ├── api_test.go               # APIEmbedding 单元测试
    ├── openai_test.go            # OpenAIEmbedding 单元测试
    ├── dashscope_test.go         # DashscopeEmbedding 单元测试
    ├── vllm_test.go              # VLLMEmbedding 单元测试
    ├── multimodal_test.go        # MultimodalDocument 单元测试
    ├── common_test.go            # 共享函数测试
    ├── utils_test.go             # 工具函数测试
    ├── openai_llm_test.go        # OpenAI 真实调用测试 //go:build llm
    └── dashscope_llm_test.go     # DashScope 真实调用测试 //go:build llm
```

---

## 12. 测试策略

### 12.1 单元测试

| 实现类 | Mock 方式 |
|--------|-----------|
| APIEmbedding | `httptest.NewServer` mock 三种响应格式 |
| OpenAIEmbedding | `httptest.NewServer` mock OpenAI API endpoint |
| DashscopeEmbedding | `httptest.NewServer` mock DashScope API endpoint |
| VLLMEmbedding | mock `OpenAIEmbedding` 实例 |
| MultimodalDocument | 纯逻辑测试，无需 mock |
| common.go | 纯函数测试 |
| utils.go | 纯函数测试 |

### 12.2 集成测试

需要真实 API Key 和网络连接的测试用 `//go:build llm` 标签隔离：

| 文件 | 测试内容 |
|------|----------|
| `openai_llm_test.go` | OpenAI Embedding 真实 API 调用 |
| `dashscope_llm_test.go` | DashScope Embedding 真实 API 调用 |

运行方式：`go test -tags=llm ./internal/agentcore/retrieval/embedding/...`

### 12.3 覆盖率目标

≥ 85%（排除 `//go:build llm` 隔离的集成测试代码）

---

## 13. 需要修改的已有文件

| 文件 | 修改内容 |
|------|----------|
| `store/embedding/base.go` | `EmbedDocuments` 签名增加 `opts ...EmbedOption`；添加 `EmbedOption` 结构体和 `Callback` 接口 |
| `store/embedding/doc.go` | 更新文件目录，标注接口扩展内容 |
| `store/embedding/base_test.go` | 更新 `fakeEmbedding.EmbedDocuments` 签名适配 |
| `store/index/simple.go` | `EmbedDocuments` 调用适配新签名 |

---

## 14. 新增依赖

| 依赖 | 用途 |
|------|------|
| `github.com/openai/openai-go` | OpenAIEmbedding 使用 |
| 阿里云 dashscope Go SDK | DashscopeEmbedding 使用（具体包路径在实现时确认，需验证 SDK 的可用性和 API 兼容性） |

---

## 15. 实现顺序建议

1. **接口扩展**：扩展 `BaseEmbedding`、新增 `EmbedOption`/`Callback`、更新已有文件适配
2. **MultimodalDocument**：纯数据模型，无外部依赖
3. **common.go + utils.go**：共享逻辑和工具函数
4. **APIEmbedding**：通用 HTTP 客户端，`net/http` 实现
5. **OpenAIEmbedding**：引入 openai-go SDK，实现文本 + 多模态嵌入
6. **DashscopeEmbedding**：引入 dashscope SDK，实现文本 + 多模态嵌入
7. **VLLMEmbedding**：组合 OpenAIEmbedding，实现多模态指令注入
8. **集成测试**：`//go:build llm` 标签的真实调用测试
9. **回填更新**：更新 `doc.go` 文件目录、`IMPLEMENTATION_PLAN.md` 状态标记、`SimpleMemoryIndex` 的 ⤵️ 标记
