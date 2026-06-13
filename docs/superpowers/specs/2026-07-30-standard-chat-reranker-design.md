# StandardReranker / ChatReranker 设计（4.24）

## 概述

实现领域 4 第 4.24 小节：StandardReranker 和 ChatReranker 具体实现。提供标准重排序和对话式重排序的 HTTP 客户端实现，同时提取公共 HTTP 重试工具。

对应 Python 代码：
- `openjiuwen/core/retrieval/reranker/standard_reranker.py`（StandardReranker）
- `openjiuwen/core/retrieval/reranker/chat_reranker.py`（ChatReranker）
- `openjiuwen/core/retrieval/utils/api_requests.py`（公共 HTTP 重试）

## 设计决策

| 决策项 | 选择 | 原因 |
|--------|------|------|
| 基类方法访问 | 导出结构体 `RerankerBase`，方法保持小写（同包访问） | 接口留在 `store/reranker/`，基类实现迁到 `retrieval/reranker/`，同包内无需导出方法 |
| 基类位置 | `retrieval/reranker/reranker_base.go` | 与 StandardReranker/ChatReranker 同包，对齐 MemoryIndexBase 同包模式 |
| 接口位置 | 保留在 `store/reranker/base.go` | 纯接口层与 store 下其他接口定义一致，对齐 `store/embedding/` + `retrieval/embedding/` 模式 |
| HTTP 请求 | 提取公共 `retrieval/utils/api_requests.go` | 对齐 Python `retrieval/utils/api_requests.py`，Reranker 和未来 Embedding 可共享 |
| ChatReranker 多文档 | 严格限制 size=1 | 对齐 Python，传入多个文档时直接返回错误 |
| test_compatibility | 保留为导出方法 | 对齐 Python，调用方可主动验证服务是否支持 logprobs |

## 文件结构

```
internal/agentcore/
├── store/
│   └── reranker/
│       ├── doc.go              # 包文档（需更新）
│       ├── base.go             # BaseReranker 接口 + Document/RerankerConfig/RerankOption + 辅助函数
│       └── base_test.go        # 接口/类型测试（保留，需更新）
│
├── retrieval/
│   ├── utils/
│   │   ├── doc.go              # 包文档
│   │   ├── api_requests.go     # 公共 HTTP 重试工具
│   │   └── api_requests_test.go
│   │
│   └── reranker/
│       ├── doc.go              # 包文档
│       ├── reranker_base.go    # RerankerBase 基类（从 store/reranker/ 迁入）
│       ├── reranker_base_test.go
│       ├── standard.go         # StandardReranker
│       ├── standard_test.go
│       ├── chat.go             # ChatReranker
│       └── chat_test.go
```

### 变更说明

- `store/reranker/reranker_base.go` 中的 `rerankerBase` 结构体及所有方法迁入 `retrieval/reranker/reranker_base.go`，重命名为 `RerankerBase`（导出结构体，方法保持小写）
- `store/reranker/reranker_base.go` 文件删除
- `store/reranker/base.go` 中保留 `BaseReranker` 接口、`Document`、`RerankerConfig`、`RerankOption`、`NewDocument`、`DocID`、`validateConfig`、`resolveInstruct`、`formatQuery`、`replacePlaceholder` 及所有常量
- `store/reranker/base_test.go` 中基类相关测试迁入 `retrieval/reranker/reranker_base_test.go`
- `store/reranker/doc.go` 更新文件目录

---

## 一、store/reranker/ 变更（接口层）

### base.go 保留内容

保留所有接口定义、类型定义、辅助函数和常量，无 API 变更。仅移除 `rerankerBase` 相关代码。

### doc.go 更新

移除 `reranker_base.go` 条目，保留 `base.go` 条目。

---

## 二、retrieval/utils/api_requests.go（公共 HTTP 重试工具）

对齐 Python `openjiuwen/core/retrieval/utils/api_requests.py`。

### 类型定义

```go
// TaskName 任务类型，决定错误码前缀。
// 对齐 Python: Literal["Reranker", "Embedding"]
type TaskName string

const (
    // TaskReranker 重排序任务
    TaskReranker TaskName = "Reranker"
    // TaskEmbedding 嵌入任务
    TaskEmbedding TaskName = "Embedding"
)

// RetryConfig 重试配置。
// 对齐 Python: sync_request_with_retry / async_request_with_retry 的参数
type RetryConfig struct {
    // MaxRetries 最大重试次数，默认 3
    MaxRetries int
    // RetryWait 重试等待基数，默认 100ms
    RetryWait time.Duration
    // Task 任务类型，决定错误码前缀，默认 TaskReranker
    Task TaskName
}
```

### 导出函数

```go
// RequestWithRetry 发送带重试的 HTTP POST 请求。
// 对齐 Python: async_request_with_retry。
// httpClient 由调用方创建和管理，cfg 控制重试行为。
// 返回响应的 JSON 解析结果 map[string]any。
func RequestWithRetry(
    ctx context.Context,
    httpClient *http.Client,
    url string,
    jsonBody map[string]any,
    headers map[string]string,
    cfg RetryConfig,
) (map[string]any, error)

// RequestWithRetrySync 发送带重试的同步 HTTP POST 请求。
// 对齐 Python: sync_request_with_retry。
// 参数和返回值与 RequestWithRetry 一致。
func RequestWithRetrySync(
    ctx context.Context,
    httpClient *http.Client,
    url string,
    jsonBody map[string]any,
    headers map[string]string,
    cfg RetryConfig,
) (map[string]any, error)
```

### 重试逻辑（对齐 Python）

1. **退避策略**：`rand.Float64() * retryWait * backoff`（线性退避 + 抖动），对齐 Python `random.random() * retry_wait * backoff`
2. **状态码处理**：
   - `200` → 解析 JSON 返回
   - `400` → 记录 Error 日志，检测审查内容关键词（safety/violation/policy/inspection/appropriate），记录 Warning 日志，不重试
   - `429/500/503` → 重试
   - 其他状态码 → 不重试
3. **网络异常** → 重试
4. **错误抛出**（超过最大重试次数后）：
   - 有 HTTP 响应 → `StatusRetrieval{Task}RequestCallFailed`，cause 为 HTTP 错误
   - 无 HTTP 响应 → `StatusRetrieval{Task}UnreachableCallFailed`
5. **task 参数**：通过 `TaskName` 动态选择错误码，对齐 Python `getattr(StatusCode, f"RETRIEVAL_{task.upper()}_REQUEST_CALL_FAILED")`

### Python → Go 对齐表

| Python | Go | 说明 |
|--------|-----|------|
| `client: httpx.Client` | `httpClient *http.Client` | 客户端由调用方传入 |
| `max_retries: int = 3` | `RetryConfig.MaxRetries` | 默认 3 |
| `retry_wait: float = 0.1` | `RetryConfig.RetryWait` | 默认 100ms |
| `task: Literal[...]` | `RetryConfig.Task` | 决定错误码前缀 |
| `**kwargs` → `client.post(url=, json=, headers=)` | `url/jsonBody/headers` 显式参数 | Go 不支持 **kwargs |
| `custom_callback` | 包内硬编码 429/500/503 重试 | Go 侧暂不开放回调 |
| `random.random() * retry_wait * backoff` | `rand.Float64() * retryWait * backoff` | 线性退避 + 抖动 |
| 200 → 返回 JSON | 200 → 返回 `map[string]any` | 一致 |
| 400 → 记日志 + 检测审查 | 400 → 记日志 + 检测审查 | 一致 |
| 429/500/503 → 重试 | 429/500/503 → 重试 | 一致 |

---

## 三、retrieval/reranker/reranker_base.go（基类）

从 `store/reranker/reranker_base.go` 迁入，重命名 `rerankerBase` → `RerankerBase`。

### 结构体

```go
// RerankerBase 重排序模型的默认实现基类。
//
// 嵌入此结构体后，实现类只需实现核心的 Rerank/RerankDocs 等方法即可满足 BaseReranker 接口。
// 默认提供 requestHeaders / requestParams / parseResponse / assembleParams 等通用方法，
// 子类可按需覆盖。
//
// 对应 Python: Reranker ABC 中的 _request_headers / _request_params / _parse_response
type RerankerBase struct {
    // config 重排序模型配置
    config reranker.RerankerConfig
    // headers 默认请求头
    headers map[string]string
    // maxRetries 最大重试次数
    maxRetries int
    // retryWait 重试等待时间
    retryWait time.Duration
}
```

**字段保持小写**：同包内可直接访问，无需导出。为子类提供 `Config()` 访问器。

### 导出函数

```go
// NewRerankerBase 创建重排序基类实例。
func NewRerankerBase(config reranker.RerankerConfig, maxRetries int, retryWait time.Duration) *RerankerBase

// NewRerankerBaseWithDefaults 使用默认值创建重排序基类实例。
func NewRerankerBaseWithDefaults(config reranker.RerankerConfig) *RerankerBase
```

### 小写方法（同包访问，子类可覆盖）

| 方法 | 签名 | 说明 |
|------|------|------|
| `Config` | `() reranker.RerankerConfig` | 返回配置（访问器） |
| `requestHeaders` | `() map[string]string` | 构建请求头，子类可覆盖 |
| `requestParams` | `(query string, documents []string, topN int, opt *reranker.RerankOption) map[string]any` | 构建请求参数（StandardReranker 风格），子类可覆盖 |
| `parseResponse` | `(responseData map[string]any, docIDs []string) map[string]float64` | 解析 API 响应（StandardReranker 风格），子类可覆盖 |
| `assembleParams` | `(query string, docs []any, opt *reranker.RerankOption) (map[string]string, map[string]any, []string)` | 组装请求参数，返回 (headers, params, docIDs) |
| `extractDocIDs` | `(docs []any) []string` | 从文档列表提取 ID |
| `extractTexts` | `(docs []any) []string` | 从文档列表提取文本 |
| `resolveTopN` | `(opt *reranker.RerankOption, docCount int) int` | 解析 TopN 选项 |

**注意**：`assembleParams` 返回值增加了 `docIDs`，因为 Python 中 `_assemble_params` 返回后调用方需要 docIDs 传给 `_parse_response`。原 Go 版本中 docIDs 被丢弃了（`_ = docIDs`），这是一个 bug，此处修正。

---

## 四、retrieval/reranker/standard.go（StandardReranker）

对应 Python: `openjiuwen/core/retrieval/reranker/standard_reranker.py`

### 结构体

```go
// StandardReranker 标准重排序客户端，支持 vLLM 风格的 /rerank API。
//
// 对应 Python: openjiuwen/core/retrieval/reranker/standard_reranker.py (StandardReranker)
type StandardReranker struct {
    // RerankerBase 嵌入基类
    *RerankerBase
    // httpClient HTTP 客户端
    httpClient *http.Client
    // endPoint API 端点，默认 "/rerank"
    endPoint string
}
```

### 构造函数

```go
// StandardRerankerOption StandardReranker 可选配置
type StandardRerankerOption func(*StandardReranker)

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(n int) StandardRerankerOption

// WithRetryWait 设置重试等待时间
func WithRetryWait(d time.Duration) StandardRerankerOption

// WithExtraHeaders 设置额外请求头
func WithExtraHeaders(headers map[string]string) StandardRerankerOption

// WithHTTPClient 设置自定义 HTTP 客户端
func WithHTTPClient(client *http.Client) StandardRerankerOption

// NewStandardReranker 创建标准重排序客户端。
// APIBase 尾部去除 "/rerank" 后缀（对齐 Python removesuffix(self.end_point)）。
func NewStandardReranker(config reranker.RerankerConfig, opts ...StandardRerankerOption) (*StandardReranker, error)
```

构造函数逻辑：
1. `validateConfig(&config)` 校验配置
2. APIBase 尾部去除 `/rerank` 后缀
3. 创建 `RerankerBase`（含默认 headers：Content-Type + Authorization）
4. 应用 `WithExtraHeaders` 等选项
5. 创建 `*http.Client`（或使用传入的自定义客户端），支持 TLS 配置

### BaseReranker 接口实现

#### Rerank

```go
func (r *StandardReranker) Rerank(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error)
```

逻辑：
1. 将 `[]string` 转为 `[]any`
2. 调用 `assembleParams(query, docs, opt)` 获取 headers, params, docIDs
3. 调用 `utils.RequestWithRetry(ctx, r.httpClient, r.Config().APIBase+r.endPoint, params, headers, retryCfg)`
4. 调用 `parseResponse(result, docIDs)`

#### RerankDocs

```go
func (r *StandardReranker) RerankDocs(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error)
```

逻辑：
1. 将 `[]*Document` 转为 `[]any`
2. 后续同 Rerank

#### RerankSync / RerankDocsSync

同步版本，使用 `utils.RequestWithRetrySync`，逻辑与异步版本一致。

### assembleParams（覆盖基类）

对齐 Python `StandardReranker._assemble_params`：

1. 校验输入类型：必须是 `[]string` 或 `[]*Document`，否则返回 `StatusRetrievalRerankerInputInvalid` 错误
2. 提取文档文本（`Document.Text`）
3. 调用 `requestHeaders()` / `requestParams()` 组装参数
4. 返回 (headers, params, docIDs)

### 日志同步（对齐 Python）

Python 中的日志调用及 Go 对等实现：

| Python | Go |
|--------|-----|
| `logger.warning("Reranker received a multimodal reranking request, not supported in openJiuwen %s", __version__)` | `logger.Warn(logComponent).Str("event_type", "reranker_multimodal_not_supported").Str("version", version.Version).Msg("Reranker 收到多模态重排序请求，当前版本不支持")` |

---

## 五、retrieval/reranker/chat.go（ChatReranker）

对应 Python: `openjiuwen/core/retrieval/reranker/chat_reranker.py`

### 结构体

```go
// ChatReranker 基于 Chat Completion 的重排序客户端。
//
// 利用 Chat 模型 + logit_bias 限制输出为 yes/no，
// 从 logprobs 中提取 P("yes") 作为相关性分数。
// 每次只能对 1 个文档进行重排序。
//
// 对应 Python: openjiuwen/core/retrieval/reranker/chat_reranker.py (ChatReranker)
type ChatReranker struct {
    // StandardReranker 嵌入标准重排序器
    *StandardReranker
    // yesNoIDs "yes" 和 "no" 的 token ID
    yesNoIDs [2]int
    // endPoint API 端点，覆盖为 "/chat/completions"
    endPoint string
}
```

### 常量

```go
const (
    // chatEndPoint Chat Completion API 端点
    chatEndPoint = "/chat/completions"
    // docTemplate 文档模板
    docTemplate = "<Document>: {doc}"
    // systemInstruct 系统指令
    systemInstruct = `Judge whether the Document meets the requirements based on the Query and the Instruct provided. Note that the answer can only be "yes" or "no".`
)
```

### 构造函数

```go
// NewChatReranker 创建 Chat 重排序客户端。
// 必须提供 config.YesNoIDs（长度为 2 的有效 int 数组）。
// 记录实验性功能警告日志。
func NewChatReranker(config reranker.RerankerConfig, opts ...StandardRerankerOption) (*ChatReranker, error)
```

构造函数逻辑：
1. 记录警告日志：`"ChatReranker support is experimental, you have been warned."`（对齐 Python）
2. 校验 `config.YesNoIDs`：
   - `[2]int` 零值为 `{0, 0}`，两个都为 0 视为未设置，返回 `StatusRetrievalRerankerInputInvalid` 错误
   - 错误消息：`'chat reranker require "yes_no_ids" to be specified in RerankerConfig'`（对齐 Python）
3. 调用 `NewStandardReranker(config, opts...)` 创建嵌入的 StandardReranker
4. 覆盖 `endPoint` 为 `/chat/completions`

### 重写方法

#### assembleParams（覆盖 StandardReranker）

对齐 Python `ChatReranker._assemble_params`：

1. 严格限制 `len(docs) == 1`，否则返回 `StatusRetrievalRerankerInputInvalid` 错误
2. 错误消息：`"input to chat reranker must be a list[str | Document] of size 1"`（对齐 Python）
3. 后续同 StandardReranker 的组装逻辑

#### requestParams（覆盖 StandardReranker）

对齐 Python `ChatReranker._request_params`：

1. 从 `documents` 中取出第一个文档文本
2. 构造 instruct：如果 `CustomInstruct` 非空则使用自定义，否则使用默认 instruct
3. 构造用户消息内容：`queryTemplate + docTemplate`
4. 构造 messages：
   ```go
   messages := []map[string]any{
       {"role": "system", "content": systemInstruct},
       {"role": "user", "content": content},
   }
   ```
5. 构造请求参数：
   ```go
   params := map[string]any{
       "model":        r.Config().ModelName,
       "messages":     messages,
       "temperature":  0,
       "max_tokens":   1,
       "logprobs":     true,
       "top_logprobs": 5,
       "logit_bias":   map[int]int{r.yesNoIDs[0]: 5, r.yesNoIDs[1]: 5},
   }
   ```
6. 合并 `ExtraBody`

#### parseResponse（覆盖 StandardReranker）

对齐 Python `ChatReranker._parse_response`：

1. 从 `responseData["choices"][0]` 获取 choice
2. 获取 `logprobs`：
   ```go
   logprobs, ok := choice["logprobs"].(map[string]any)
   ```
   如果 logprobs 为空或无内容，返回 `StatusRetrievalRerankerRequestCallFailed` 错误：
   `"the service does not support logprobs for chat reranker to function"`（对齐 Python）
3. 获取 `logprobs["content"]`（优先）或 `logprobs` 本身，取 `[0]["top_logprobs"]`
4. 遍历 top_logprobs：
   - `token["token"]` → strip + casefold
   - 以 "yes" 开头 → `yesScores = append(yesScores, math.Exp(logprob))`
   - 以 "no" 开头 → `noScores = append(noScores, math.Exp(logprob))`
5. 计算：`confidence = max(yesScores)`, `total = confidence + max(noScores)`
6. 如果 `total == 0`，返回 `{docID: 0.0}`
7. 否则返回 `{docID: confidence / total}`

**注意**：Python 中 `yesScores`/`noScores` 初始值为 `[0]`（非空列表），确保 `max()` 有返回值。Go 侧需同样初始化为 `[]float64{0}`。

### 新增导出方法

#### TestCompatibility

```go
// TestCompatibility 测试服务是否支持基于 chat completion 的重排序。
// 对齐 Python: ChatReranker.test_compatibility
func (c *ChatReranker) TestCompatibility(ctx context.Context) (bool, error)
```

逻辑：
1. 调用 `c.RerankSync(ctx, "test", []string{"test"})`（instruct=false）
2. 成功返回 `(true, nil)`
3. 失败记录 Error 日志：`"The selected service does not support chat-completion-based reranking"`（对齐 Python），返回 `(false, err)`

### 日志同步（对齐 Python）

| Python | Go |
|--------|-----|
| `logger.warning("ChatReranker support is experimental in openJiuwen %s, you have been warned.", __version__)` | `logger.Warn(logComponent).Str("event_type", "chat_reranker_experimental").Str("version", version.Version).Msg("ChatReranker 支持处于实验阶段，请注意")` |
| `logger.error("The selected service does not support chat-completion-based reranking: %r", e)` | `logger.Error(logComponent).Str("event_type", "chat_reranker_compatibility_error").Err(err).Msg("所选服务不支持基于 chat completion 的重排序")` |

---

## 六、测试策略

### retrieval/utils/api_requests_test.go

使用 `net/http/httptest` 模拟服务端：

| 测试项 | 说明 |
|--------|------|
| `TestRequestWithRetry_成功响应` | 200 返回 JSON |
| `TestRequestWithRetry_重试429` | 429 → 重试 → 成功 |
| `TestRequestWithRetry_重试500` | 500 → 重试 → 成功 |
| `TestRequestWithRetry_重试503` | 503 → 重试 → 成功 |
| `TestRequestWithRetry_400不重试` | 400 → 直接返回错误 |
| `TestRequestWithRetry_400审查内容` | 400 含 safety 关键词 → 记录 Warning |
| `TestRequestWithRetry_最大重试耗尽` | 全部 500 → 返回 RequestCallFailed |
| `TestRequestWithRetry_网络错误` | 连接失败 → 返回 UnreachableCallFailed |
| `TestRequestWithRetrySync_同步调用` | 验证同步版本行为一致 |
| `TestRequestWithRetry_取消上下文` | ctx 取消时立即返回 |

### retrieval/reranker/reranker_base_test.go

从 `store/reranker/base_test.go` 迁入基类相关测试：

| 测试项 | 说明 |
|--------|------|
| `TestNewRerankerBase` | 构造基类实例 |
| `TestRerankerBase_RequestHeaders` | 默认头（含/不含 APIKey） |
| `TestRerankerBase_ParseResponse` | 标准 vLLM 格式 / 嵌套 output 格式 / 空结果 |
| `TestRerankerBase_ExtractDocIDs` | 混合输入提取 ID |
| `TestRerankerBase_ExtractTexts` | 混合输入提取文本 |
| `TestRerankerBase_ResolveTopN` | 设置/未设置/零值 |
| `TestRerankerBase_RequestParams` | model、ExtraBody、ExtraParams 合并 |
| `TestRerankerBase_AssembleParams` | 完整组装流程，验证返回 docIDs |

### retrieval/reranker/standard_test.go

使用 `net/http/httptest` 模拟 `/rerank` 端点：

| 测试项 | 说明 |
|--------|------|
| `TestStandardReranker_Rerank_正常` | 标准重排序，返回文档-分数映射 |
| `TestStandardReranker_RerankDocs_Document输入` | 传入 []*Document |
| `TestStandardReranker_Rerank_Instruct选项` | 默认/自定义/禁用 instruct |
| `TestStandardReranker_Rerank_ExtraBody合并` | 验证 ExtraBody 合并到请求参数 |
| `TestStandardReranker_Rerank_API调用失败` | 服务端返回 500 → 重试 |
| `TestStandardReranker_RerankSync_同步调用` | 验证同步版本 |
| `TestStandardReranker_Rerank_空结果` | 返回空 results |
| `TestStandardReranker_ExtraHeaders` | 额外请求头透传 |
| `TestStandardReranker_配置校验` | APIBase 缺失时报错 |

### retrieval/reranker/chat_test.go

使用 `net/http/httptest` 模拟 `/chat/completions` 端点：

| 测试项 | 说明 |
|--------|------|
| `TestChatReranker_Rerank_正常解析` | 返回含 logprobs 的响应，计算 yes/no 概率 |
| `TestChatReranker_Rerank_yes概率计算` | 验证 `math.Exp(logprob)` 和 `confidence/total` |
| `TestChatReranker_Rerank_多文档报错` | len(docs) > 1 时返回 InputInvalid |
| `TestChatReranker_YesNoIDs缺失报错` | 构造时 config.YesNoIDs 零值 → 报错 |
| `TestChatReranker_TestCompatibility_成功` | 服务支持 logprobs → (true, nil) |
| `TestChatReranker_TestCompatibility_失败` | 服务不支持 logprobs → (false, err) |
| `TestChatReranker_Rerank_logprobs不支持` | 响应无 logprobs → RequestCallFailed |
| `TestChatReranker_Rerank_总概率为零` | yes/no 都无匹配 → 返回 0.0 |
| `TestChatReranker_RerankSync_同步调用` | 验证同步版本 |

---

## 七、错误码

已有错误码（无需新增）：

| 常量 | 编码 | 说明 |
|------|------|------|
| `StatusRetrievalRerankerRequestCallFailed` | 155600 | API 调用失败 |
| `StatusRetrievalRerankerUnreachableCallFailed` | 155601 | 服务不可达 |
| `StatusRetrievalRerankerInputInvalid` | 155602 | 输入无效 |

`api_requests.go` 根据 `TaskName` 选择对应错误码：

| TaskName | RequestCallFailed | UnreachableCallFailed |
|----------|-------------------|-----------------------|
| `TaskReranker` | `StatusRetrievalRerankerRequestCallFailed` | `StatusRetrievalRerankerUnreachableCallFailed` |
| `TaskEmbedding` | `StatusRetrievalEmbeddingRequestCallFailed` | `StatusRetrievalEmbeddingUnreachableCallFailed` |

---

## 八、产出文件清单

| 文件 | 操作 | 内容 |
|------|------|------|
| `store/reranker/base.go` | 修改 | 移除 rerankerBase 相关代码，保留接口+类型 |
| `store/reranker/reranker_base.go` | 删除 | 迁入 retrieval/reranker/ |
| `store/reranker/base_test.go` | 修改 | 移除基类相关测试，保留接口/类型测试 |
| `store/reranker/doc.go` | 修改 | 更新文件目录 |
| `retrieval/utils/doc.go` | 新建 | 包文档 |
| `retrieval/utils/api_requests.go` | 新建 | 公共 HTTP 重试工具 |
| `retrieval/utils/api_requests_test.go` | 新建 | 重试工具测试 |
| `retrieval/reranker/doc.go` | 新建 | 包文档 |
| `retrieval/reranker/reranker_base.go` | 新建 | RerankerBase 基类 |
| `retrieval/reranker/reranker_base_test.go` | 新建 | 基类测试 |
| `retrieval/reranker/standard.go` | 新建 | StandardReranker |
| `retrieval/reranker/standard_test.go` | 新建 | StandardReranker 测试 |
| `retrieval/reranker/chat.go` | 新建 | ChatReranker |
| `retrieval/reranker/chat_test.go` | 新建 | ChatReranker 测试 |
