# 4.25 DashScopeReranker 设计文档

## 概述

实现阿里云 DashScope 云服务重排序客户端（DashScopeReranker），支持纯文本和多模态文档输入。同时将 `MultimodalDocument` 从 `embedding` 包提升到 `retrieval/common/` 公共包，供 embedding 和 reranker 共同引用。

## 关键决策

| 决策项 | 结论 |
|--------|------|
| 多模态支持 | 4.25 同时实现 |
| AliyunReranker 别名 | 不保留 |
| 文件位置 | `retrieval/reranker/dashscope.go`（与 StandardReranker 同级） |
| MultimodalDocument 位置 | 提升到 `retrieval/common/` |
| 嵌入方式 | 嵌入 RerankerBase 独立实现（类似 StandardReranker） |
| instruct 处理 | DashScopeReranker 自行处理，只看 CustomInstruct |
| RerankOption | 不修改现有结构体 |

## 文件结构

### 新增文件

```
internal/agentcore/retrieval/
├── common/
│   ├── doc.go                    # 包文档
│   ├── document.go               # MultimodalDocument（从 embedding/multimodal.go 迁移）
│   └── document_test.go          # MultimodalDocument 单元测试（从 embedding/multimodal_test.go 迁移）
├── reranker/
│   ├── dashscope.go              # DashScopeReranker 实现
│   └── dashscope_test.go         # DashScopeReranker 单元测试
```

### 修改文件

| 文件 | 变更 |
|------|------|
| `embedding/multimodal.go` | 删除（迁移到 common/document.go） |
| `embedding/multimodal_test.go` | 删除（迁移到 common/document_test.go） |
| `embedding/dashscope.go` | MultimodalDocument 引用改为 common 包 |
| `embedding/vllm.go` | MultimodalDocument 引用改为 common 包 |
| `embedding/embedding.go`（或其他） | MultimodalEmbedder 接口迁移到此处，import common |
| `embedding/doc.go` | 更新文件目录 |
| `reranker/doc.go` | 更新文件目录，新增 dashscope.go 条目 |
| `reranker/reranker_base.go` | 无变更 |

## 设计段 1：MultimodalDocument 迁移

### 迁移内容（从 embedding → common）

- `ModalityKind` 类型 + 4 个常量（`ModalityText/Image/Audio/Video`）
- `ModalityField` 结构体
- `MultimodalDocument` 结构体 + 全部方法
- `loadMultimodalData`、`loadFromFile` 非导出函数

### 不迁移的内容

- `MultimodalOption` — 嵌入场景专用参数，留在 embedding 包
- `MultimodalEmbedder` 接口 — 嵌入接口，留在 embedding 包，import `common.MultimodalDocument`
- `Document` 类型 — 已在 `store/reranker/base.go`，与 Python 一致
- `TextChunk`/`SearchResult` 等 — Go 中尚未实现，不属于 4.25 范围

### 包名变更

`package embedding` → `package common`

### embedding 包适配

- 删除 `embedding/multimodal.go` 和 `embedding/multimodal_test.go`
- 所有 `MultimodalDocument` 引用改为 `common.MultimodalDocument`
- `ModalityKind`/`ModalityField` 引用同样改为 `common.ModalityKind`/`common.ModalityField`
- `MultimodalEmbedder` 接口移到 embedding 包的现有文件中

## 设计段 2：DashScopeReranker 核心结构

### 结构体

```go
type DashScopeReranker struct {
    *RerankerBase
    httpClient *http.Client
    endPoint   string
}
```

### 端点与请求格式差异

| 维度 | StandardReranker | DashScopeReranker |
|------|-----------------|-------------------|
| 端点 | `/rerank` | `/services/rerank/text-rerank/text-rerank` |
| 请求体 | `{model, query, documents, top_n, return_documents}` | `{model, input: {query, documents}, parameters: {return_documents, top_n, instruct}}` |
| instruct 处理 | 拼入 query 模板 | 作为 `parameters.instruct` 字段传递 |
| 多模态 | 不支持 | 支持（MultimodalDocument.DashscopeInput 转换） |
| 响应格式 | `output.results` 或 `results` | 相同（复用基类 parseResponse） |

### 方法覆盖

- **覆盖 `requestParams`**：构造 DashScope 专用请求格式
- **覆盖 `assembleParams`**：处理多模态文档，签名增加 error 返回值
- **不覆盖 `parseResponse`**：DashScope 响应格式与 StandardReranker 相同

### 构造函数

```go
func NewDashScopeReranker(config reranker.RerankerConfig, opts ...DashScopeRerankerOption) (*DashScopeReranker, error)
```

支持的 Option：
- `WithDashScopeMaxRetries(n int)`
- `WithDashScopeRetryWait(d time.Duration)`
- `WithDashScopeHTTPClient(client *http.Client)`
- `WithDashScopeExtraHeaders(headers map[string]string)`

### BaseReranker 接口实现

```go
Rerank(ctx, query, docs []string, opts...) → map[string]float64
RerankDocs(ctx, query, docs []*Document, opts...) → map[string]float64
RerankSync(ctx, query, docs []string, opts...) → map[string]float64
RerankDocsSync(ctx, query, docs []*Document, opts...) → map[string]float64
```

### 多模态专用方法

```go
RerankMultimodal(ctx, query, docs []*common.MultimodalDocument, opts...) → map[string]float64
RerankMultimodalSync(ctx, query, docs []*common.MultimodalDocument, opts...) → map[string]float64
```

### 编译时接口校验

```go
var _ reranker.BaseReranker = (*DashScopeReranker)(nil)
```

## 设计段 3：数据流与 instruct 处理

### assembleParams 数据流

```
assembleParams(query, docs, opt)
  │
  ├─ 遍历 docs，分类处理：
  │   ├─ string → docID=字符串, text=字符串
  │   ├─ *Document → docID=ID, text=Text
  │   └─ *MultimodalDocument → docID=Text, multimodalInput=DashscopeInput()
  │
  ├─ 记录 hasMultimodal 标志
  │
  ├─ 有 multimodal 时：
  │   ├─ 纯文本 doc 包装为 {"text": text}
  │   ├─ 多模态 doc 使用 DashscopeInput() 结果
  │   └─ documents = 组装后的 []map[string]any
  │
  ├─ 无 multimodal 时：
  │   └─ documents = []string（纯文本列表）
  │
  ├─ requestParams(query, documents, topN, opt) → DashScope 请求体
  │   ├─ instruct 处理：
  │   │   ├─ CustomInstruct非空 → parameters.instruct = CustomInstruct
  │   │   └─ CustomInstruct为空 → 不设置 instruct
  │   └─ 返回 {model, input:{query, documents}, parameters:{...}}
  │
  └─ 返回 (headers, params, docIDs, error)
```

### instruct 处理逻辑

DashScopeReranker 只看 `CustomInstruct` 字段：

| CustomInstruct | 行为 |
|----------------|------|
| `""` (空) | 不设置 `parameters.instruct` |
| `"自定义文本"` | `parameters["instruct"] = "自定义文本"` |

**不使用 InstructEnabled 字段**，因为 DashScope 的默认行为就是不传 instruct，与 StandardReranker 的默认行为（使用默认指令拼入 query）不同。

### doRerank / doRerankSync 流程

```
doRerank(ctx, query, docs, opt)
  │
  ├─ assembleParams → (headers, params, docIDs, error)
  │   └─ error 非 nil → 直接返回 error
  │
  ├─ utils.RequestWithRetry(ctx, httpClient, apiBase+endPoint, params, headers, cfg)
  │
  ├─ error → Error 日志 + 返回 error
  │
  └─ parseResponse(result, docIDs) → map[string]float64
```

## 设计段 4：错误处理与日志

### 错误处理

| 场景 | 异常码 | 处理方式 |
|------|--------|---------|
| config 校验失败 | `StatusRetrievalRerankerInputInvalid` | 构造函数返回 error |
| 文档类型不支持 | `StatusRetrievalRerankerInputInvalid` | assembleParams 返回 error |
| HTTP 请求失败 | `StatusRetrievalRerankerRequestCallFailed` | doRerank 返回 error + Error 日志 |
| DashScope API 返回非 200 | 由 utils.RequestWithRetry 处理 | - |
| 响应解析异常 | 基类 parseResponse 处理 | 未知 index 分数为 0.0 |

### 日志

遵循项目规则 3.4，在异常路径补充 Error 日志：

```go
logger.Error(logComponent).
    Str("event_type", "llm_call_error").
    Str("method", "DashScopeRerank").
    Str("model_provider", r.config.ModelName).
    Err(err).
    Msg("DashScopeReranker 请求失败")
```

## 设计段 5：测试策略

### 测试文件

| 文件 | 内容 |
|------|------|
| `reranker/dashscope_test.go` | DashScopeReranker 单元测试 |
| `common/document_test.go` | MultimodalDocument 迁移后测试 |

### DashScopeReranker 测试用例

| 用例 | 说明 |
|------|------|
| `TestNewDashScopeReranker` | 构造函数正常创建 |
| `TestNewDashScopeReranker_配置缺失时返回错误` | APIBase 为空 |
| `TestDashScopeReranker_Rerank` | 纯文本字符串重排序（httptest mock） |
| `TestDashScopeReranker_RerankDocs` | Document 列表重排序 |
| `TestDashScopeReranker_RerankMultimodal` | MultimodalDocument 列表重排序 |
| `TestDashScopeReranker_RerankSync` | 同步重排序 |
| `TestDashScopeReranker_Rerank_请求失败` | HTTP 错误返回 |
| `TestDashScopeReranker_requestParams` | 请求参数构造（验证 input/parameters 结构） |
| `TestDashScopeReranker_requestParams_自定义Instruct` | CustomInstruct 传入 parameters.instruct |
| `TestDashScopeReranker_requestParams_无Instruct` | 不传 instruct |
| `TestDashScopeReranker_assembleParams_混合文档类型` | string + Document + MultimodalDocument 混合 |
| `TestDashScopeReranker_assembleParams_不支持的类型` | 非法类型报错 |
| `TestDashScopeReranker_parseResponse` | DashScope output.results 格式解析 |

### Mock 策略

- HTTP 请求：`net/http/httptest` 模拟 DashScope API 响应
- 不使用 build tag
- 不调用真实 DashScope API

### 覆盖率目标

≥ 85%

## Python 对应路径

| Go 文件 | Python 对应 |
|---------|-------------|
| `reranker/dashscope.go` | `openjiuwen/core/retrieval/reranker/dashscope_reranker.py` |
| `common/document.go` | `openjiuwen/core/retrieval/common/document.py` |
