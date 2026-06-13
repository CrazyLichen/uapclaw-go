# Reranker 接口设计（4.23）

## 概述

实现领域 4 第 4.23 小节：Reranker 接口定义。提供重排序模型的抽象接口、配置结构体、文档模型和基类，为后续 4.24（StandardReranker/ChatReranker）和 4.25（DashscopeReranker/AliyunReranker）的具体实现奠定基础。

对应 Python 代码：`openjiuwen/core/foundation/store/base_reranker.py`

## 文件结构与包布局

```
internal/agentcore/store/reranker/       # 接口包 + 基类（4.23 产出）
├── doc.go                                # 包文档
├── base.go                               # 接口 + 类型定义
├── reranker_base.go                      # rerankerBase 基类
└── base_test.go                          # 单元测试

internal/agentcore/retrieval/reranker/    # 具体实现包（4.24/4.25 产出）
├── doc.go                                # 4.24 时创建
├── standard.go                           # StandardReranker（4.24）
├── chat.go                               # ChatReranker（4.24）
└── dashscope.go                          # DashscopeReranker（4.25）
```

基类 `rerankerBase` 放在 `store/reranker/` 包中，与 `MemoryIndexBase` 放在 `store/index/` 的模式一致。

## 类型定义

### Document

```go
// Document 文档数据模型，表示待重排序的文档。
//
// 对应 Python: openjiuwen/core/foundation/store/base_reranker.py (Document)
type Document struct {
    // ID 唯一标识，未设置时自动生成 UUID
    ID string `json:"id"`
    // Text 文档文本内容
    Text string `json:"text"`
    // Metadata 文档元数据
    Metadata map[string]any `json:"metadata"`
}
```

辅助函数：

- `NewDocument(text string, metadata ...map[string]any) *Document` — 创建文档，自动生成 UUID
- `DocID(doc any) string` — 提取文档标识：Document 返回其 ID，字符串返回自身

### RerankerConfig

```go
// RerankerConfig 重排序模型配置。
//
// 对应 Python: openjiuwen/core/foundation/store/base_reranker.py (RerankerConfig)
type RerankerConfig struct {
    // APIKey API 密钥
    APIKey string
    // APIBase API 地址（必填）
    APIBase string
    // ModelName 模型名称
    ModelName string
    // Timeout 请求超时时间（秒），默认 10
    Timeout float64
    // Temperature 生成温度，默认 0.95
    Temperature float64
    // TopP Top-P 采样参数，默认 0.1
    TopP float64
    // YesNoIDs "yes" 和 "no" 的 token ID，ChatReranker 必填
    YesNoIDs [2]int
    // ExtraBody 传递给 API 的额外参数
    ExtraBody map[string]any
}
```

与 Python RerankerConfig 完全对齐。

验证方法：

```go
// Validate 校验配置字段，APIBase 必填，Timeout 必须大于 0。
func (c *RerankerConfig) Validate() error
```

### RerankOption

```go
// RerankOption 重排序可选参数。
type RerankOption struct {
    // InstructEnabled 是否启用指令模板，nil 表示使用默认行为（启用）
    InstructEnabled *bool
    // CustomInstruct 自定义指令文本，非空时使用此值替代默认指令
    CustomInstruct string
    // TopN 返回的最大文档数量，0 表示返回全部
    TopN int
    // ExtraParams 额外请求参数
    ExtraParams map[string]any
}
```

instruct 语义规则：

| Go 设置 | 等价 Python | 行为 |
|---------|------------|------|
| `InstructEnabled = nil`（零值） | `instruct=True` | 使用默认指令 |
| `InstructEnabled = &true` + `CustomInstruct = ""` | `instruct=True` | 使用默认指令 |
| `InstructEnabled = &true` + `CustomInstruct != ""` | `instruct="custom"` | 使用自定义指令 |
| `InstructEnabled = &false` | `instruct=False` | 不使用指令 |

## BaseReranker 接口

```go
// BaseReranker 重排序模型抽象接口，定义文档相关性重排序操作。
//
// 所有重排序模型实现必须满足此接口。给定查询和一组文档，
// 返回文档到相关性分数的映射，分数越高表示越相关。
//
// 对应 Python: openjiuwen/core/foundation/store/base_reranker.py (Reranker)
type BaseReranker interface {
    // Rerank 对字符串文档列表进行异步重排序，返回文档到相关性分数的映射。
    Rerank(ctx context.Context, query string, docs []string, opts ...RerankOption) (map[string]float64, error)

    // RerankDocs 对 Document 列表进行异步重排序，返回文档 ID 到相关性分数的映射。
    RerankDocs(ctx context.Context, query string, docs []*Document, opts ...RerankOption) (map[string]float64, error)

    // RerankSync 对字符串文档列表进行同步重排序。
    RerankSync(ctx context.Context, query string, docs []string, opts ...RerankOption) (map[string]float64, error)

    // RerankDocsSync 对 Document 列表进行同步重排序。
    RerankDocsSync(ctx context.Context, query string, docs []*Document, opts ...RerankOption) (map[string]float64, error)
}
```

4 个方法 = 2 维度 × 2 模式：
- 维度：`[]string` vs `[]*Document`
- 模式：异步 vs 同步

## rerankerBase 基类

```go
// rerankerBase 重排序模型的默认实现基类。
//
// 嵌入此结构体后，实现类只需实现核心的 Rerank/RerankDocs 等方法即可满足 BaseReranker 接口。
// 默认提供 requestHeaders / requestParams / parseResponse / assembleParams 等通用方法，
// 子类可按需覆盖。
//
// 对应 Python: Reranker ABC 中的 _request_headers / _request_params / _parse_response
type rerankerBase struct {
    // config 重排序模型配置
    config RerankerConfig
    // headers 默认请求头
    headers map[string]string
    // maxRetries 最大重试次数
    maxRetries int
    // retryWait 重试等待时间
    retryWait time.Duration
}
```

基类方法（均为非导出，供嵌入的实现类使用）：

| 方法 | 签名 | 说明 |
|------|------|------|
| `newRerankerBase` | `(config RerankerConfig, maxRetries int, retryWait time.Duration) *rerankerBase` | 创建基类实例 |
| `requestHeaders` | `() map[string]string` | 构建请求头，子类可覆盖 |
| `requestParams` | `(query string, documents []string, topN int, opt *RerankOption) map[string]any` | 构建请求参数，子类应覆盖 |
| `parseResponse` | `(responseData map[string]any, docIDs []string) map[string]float64` | 解析 API 响应，默认实现 StandardReranker 风格（`results[index].relevance_score`） |
| `extractDocIDs` | `(docs []any) []string` | 从文档列表提取 ID |
| `resolveInstruct` | `(query string, opt *RerankOption) string` | 解析 instruct 选项，返回最终查询字符串 |

### 跨包访问说明

基类方法为非导出，4.24 的实现类在 `retrieval/reranker/` 包中无法直接访问。解决方案在 4.24 实现时确定，可能方案：
- 将基类方法改为导出（简单直接，但暴露内部细节）
- 在 `store/reranker/` 包提供导出的包装函数
- 实现类不嵌入基类，而是持有一个 `*rerankerBase` 字段，通过包内桥接函数调用

## 错误码

已有错误码（无需新增）：

| 常量 | 编码 | 说明 |
|------|------|------|
| `StatusRetrievalRerankerRequestCallFailed` | 155600 | API 调用失败 |
| `StatusRetrievalRerankerUnreachableCallFailed` | 155601 | 服务不可达 |
| `StatusRetrievalRerankerInputInvalid` | 155602 | 输入无效 |

## 测试覆盖

4.23 的测试聚焦于类型和基类方法，不涉及 HTTP 调用：

| 测试项 | 说明 |
|--------|------|
| `TestDocument` | 序列化/反序列化 |
| `TestNewDocument` | 自动生成 UUID |
| `TestDocID` | string 和 *Document 的 ID 提取 |
| `TestRerankOption` | instruct 语义解析（默认/自定义/禁用） |
| `TestRerankerBase_ResolveInstruct` | 三种 instruct 场景 |
| `TestRerankerBase_ExtractDocIDs` | 混合输入提取 ID |
| `TestRerankerBase_ParseResponse` | 标准 vLLM 格式响应解析 |
| `TestRerankerBase_RequestHeaders` | 默认头构建（含/不含 APIKey） |
| `TestRerankerConfig` | 字段验证（APIBase 必填等） |

## 产出文件清单

| 文件 | 内容 |
|------|------|
| `store/reranker/doc.go` | 包文档 |
| `store/reranker/base.go` | BaseReranker 接口 + RerankerConfig + Document + RerankOption + 辅助函数 |
| `store/reranker/reranker_base.go` | rerankerBase 基类 + 通用方法实现 |
| `store/reranker/base_test.go` | 单元测试 |
