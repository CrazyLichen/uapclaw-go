# StandardReranker / ChatReranker 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 StandardReranker 和 ChatReranker 具体实现，以及公共 HTTP 重试工具

**Architecture:** 将 rerankerBase 从 store/reranker/ 迁入 retrieval/reranker/ 包并重命名为 RerankerBase，接口层保留在 store/reranker/；新建 retrieval/utils/ 提供公共 HTTP 重试工具（对齐 Python api_requests.py）；StandardReranker 嵌入 RerankerBase 调用 /rerank 端点；ChatReranker 嵌入 StandardReranker 调用 /chat/completions 端点，用 logit_bias + logprobs 计算相关性分数

**Tech Stack:** Go 1.25, net/http + httptest（无第三方 HTTP 依赖）

---

## Task 1: 迁移 RerankerBase 到 retrieval/reranker/

**Files:**
- Create: `internal/agentcore/retrieval/reranker/reranker_base.go`
- Create: `internal/agentcore/retrieval/reranker/doc.go`
- Modify: `internal/agentcore/store/reranker/base.go`（移除 rerankerBase 相关代码）
- Delete: `internal/agentcore/store/reranker/reranker_base.go`
- Modify: `internal/agentcore/store/reranker/doc.go`

- [ ] **Step 1: 创建 retrieval/reranker/reranker_base.go**

将 store/reranker/reranker_base.go 的内容迁入，重命名 rerankerBase → RerankerBase，方法保持小写，import 路径改为引用 store/reranker 包。修复 assembleParams 返回值增加 docIDs。

```go
package reranker

import (
	"fmt"
	"time"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxRetries 默认最大重试次数
	defaultMaxRetries = 3
	// defaultRetryWait 默认重试等待时间
	defaultRetryWait = 100 * time.Millisecond
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRerankerBase 创建重排序基类实例。
func NewRerankerBase(config reranker.RerankerConfig, maxRetries int, retryWait time.Duration) *RerankerBase {
	return &RerankerBase{
		config:     config,
		headers:    buildDefaultHeaders(config.APIKey),
		maxRetries: maxRetries,
		retryWait:  retryWait,
	}
}

// NewRerankerBaseWithDefaults 使用默认值创建重排序基类实例。
func NewRerankerBaseWithDefaults(config reranker.RerankerConfig) *RerankerBase {
	return NewRerankerBase(config, defaultMaxRetries, defaultRetryWait)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildDefaultHeaders 构建默认请求头。
func buildDefaultHeaders(apiKey string) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if apiKey != "" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", apiKey)
	}
	return headers
}

// Config 返回重排序模型配置。
func (b *RerankerBase) Config() reranker.RerankerConfig {
	return b.config
}

// MaxRetries 返回最大重试次数。
func (b *RerankerBase) MaxRetries() int {
	return b.maxRetries
}

// RetryWait 返回重试等待时间。
func (b *RerankerBase) RetryWait() time.Duration {
	return b.retryWait
}

// requestHeaders 返回默认请求头，子类可覆盖。
func (b *RerankerBase) requestHeaders() map[string]string {
	return b.headers
}

// requestParams 构建请求参数（StandardReranker 风格）。
// 子类应覆盖此方法以适配不同 API 格式（如 DashScope、ChatReranker）。
func (b *RerankerBase) requestParams(query string, documents []string, topN int, opt *reranker.RerankOption) map[string]any {
	params := map[string]any{
		"model":            b.config.ModelName,
		"return_documents": false,
		"query":            query,
		"documents":        documents,
		"top_n":            topN,
	}
	// 合并 ExtraBody
	for k, v := range b.config.ExtraBody {
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

// parseResponse 解析 API 响应为文档-分数映射。
// 默认实现 StandardReranker 风格：从 results[index].relevance_score 提取分数。
// 子类可覆盖以适配不同响应格式。
func (b *RerankerBase) parseResponse(responseData map[string]any, docIDs []string) map[string]float64 {
	result := make(map[string]float64, len(docIDs))
	// 初始化所有文档分数为 0
	for _, id := range docIDs {
		result[id] = 0.0
	}

	// 尝试从 "output" 或根级别获取 "results"
	var results []any
	if output, ok := responseData["output"]; ok {
		if outputMap, ok := output.(map[string]any); ok {
			results, _ = outputMap["results"].([]any)
		}
	}
	if results == nil {
		results, _ = responseData["results"].([]any)
	}

	for _, item := range results {
		rankResult, ok := item.(map[string]any)
		if !ok {
			continue
		}
		index, _ := rankResult["index"].(float64)
		score, _ := rankResult["relevance_score"].(float64)
		idx := int(index)
		if idx >= 0 && idx < len(docIDs) {
			result[docIDs[idx]] = score
		}
	}

	return result
}

// extractDocIDs 从文档列表提取 ID 列表。
// 字符串直接作为 ID，*Document 使用其 ID 字段。
func (b *RerankerBase) extractDocIDs(docs []any) []string {
	ids := make([]string, len(docs))
	for i, doc := range docs {
		ids[i] = reranker.DocID(doc)
	}
	return ids
}

// extractTexts 从文档列表提取文本列表。
// 字符串直接使用，*Document 使用其 Text 字段。
func (b *RerankerBase) extractTexts(docs []any) []string {
	texts := make([]string, len(docs))
	for i, doc := range docs {
		if d, ok := doc.(*reranker.Document); ok {
			texts[i] = d.Text
		} else if s, ok := doc.(string); ok {
			texts[i] = s
		}
	}
	return texts
}

// resolveTopN 解析 TopN 选项，0 或未设置时使用文档总数。
func (b *RerankerBase) resolveTopN(opt *reranker.RerankOption, docCount int) int {
	if opt != nil && opt.TopN > 0 {
		return opt.TopN
	}
	return docCount
}

// assembleParams 组装请求参数，将文档和查询合并为完整的请求参数。
// 返回请求头、请求参数和文档 ID 列表。
func (b *RerankerBase) assembleParams(query string, docs []any, opt *reranker.RerankOption) (map[string]string, map[string]any, []string) {
	docIDs := b.extractDocIDs(docs)
	texts := b.extractTexts(docs)
	topN := b.resolveTopN(opt, len(docs))
	resolvedQuery := reranker.ResolveInstruct(query, opt)

	headers := b.requestHeaders()
	params := b.requestParams(resolvedQuery, texts, topN, opt)

	// 确保参数中有 documents 和 top_n
	params["documents"] = texts
	params["top_n"] = topN

	return headers, params, docIDs
}
```

- [ ] **Step 2: 修改 store/reranker/base.go**

将 `resolveInstruct`、`formatQuery`、`replacePlaceholder` 改为导出函数（供 retrieval/reranker 包调用），同时删除原有的 `NewRerankerBase` 构造函数和所有 rerankerBase 引用。

将以下函数改为导出：
- `resolveInstruct` → `ResolveInstruct`
- `formatQuery` → `FormatQuery`
- `replacePlaceholder` → `ReplacePlaceholder`

- [ ] **Step 3: 删除 store/reranker/reranker_base.go**

删除此文件，内容已迁入 retrieval/reranker/reranker_base.go。

- [ ] **Step 4: 修改 store/reranker/doc.go**

更新包文档，移除 rerankerBase 引用和 reranker_base.go 条目：

```go
// Package reranker 提供重排序模型的抽象接口和数据模型。
//
// 本包定义了所有重排序模型实现必须满足的 BaseReranker 接口，
// 以及 RerankerConfig 配置、Document 文档模型、RerankOption 可选参数。
// 具体实现类（如 StandardReranker、ChatReranker）位于
// retrieval/reranker 包中，嵌入 RerankerBase 基类后
// 只需实现核心 Rerank/RerankDocs 等方法即可满足 BaseReranker 接口。
//
// 文件目录：
//
//	reranker/
//	├── doc.go              # 包文档
//	├── base.go             # BaseReranker 接口 + RerankerConfig + Document + RerankOption
//	└── base_test.go        # 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_reranker.py
//
// 核心类型/接口索引：
//
//	BaseReranker  — 重排序模型抽象接口（Rerank/RerankDocs/RerankSync/RerankDocsSync）
//	RerankerConfig — 重排序模型配置（APIKey/APIBase/ModelName/Timeout 等）
//	Document      — 文档数据模型（ID/Text/Metadata）
//	RerankOption  — 重排序可选参数（InstructEnabled/CustomInstruct/TopN/ExtraParams）
package reranker
```

- [ ] **Step 5: 创建 retrieval/reranker/doc.go**

```go
// Package reranker 提供重排序模型的具体实现。
//
// 本包实现了 BaseReranker 接口的多种提供者：
// StandardReranker（vLLM 风格 /rerank API）和
// ChatReranker（Chat Completion + logit_bias 实验性方案）。
// 同时提供 RerankerBase 默认实现基类。
//
// 文件目录：
//
//	reranker/
//	├── doc.go              # 包文档
//	├── reranker_base.go    # RerankerBase 基类 + 通用方法
//	├── standard.go         # StandardReranker 标准重排序客户端
//	├── chat.go             # ChatReranker 对话式重排序客户端
//	├── reranker_base_test.go # 基类单元测试
//	├── standard_test.go    # StandardReranker 单元测试
//	└── chat_test.go        # ChatReranker 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/reranker/standard_reranker.py
//	openjiuwen/core/retrieval/reranker/chat_reranker.py
//
// 核心类型/接口索引：
//
//	RerankerBase     — 默认实现基类，提供通用 HTTP 请求/响应处理方法
//	StandardReranker — 标准重排序客户端（/rerank API）
//	ChatReranker     — 对话式重排序客户端（/chat/completions + logprobs）
package reranker
```

- [ ] **Step 6: 更新 store/reranker/base_test.go**

移除所有 rerankerBase 相关测试（迁入 retrieval/reranker/reranker_base_test.go），保留接口/类型测试和 fakeReranker 测试。将 `resolveInstruct` 调用改为 `ResolveInstruct`。

- [ ] **Step 7: 验证编译通过**

Run: `go build ./internal/agentcore/store/reranker/ ./internal/agentcore/retrieval/reranker/`
Expected: 无编译错误

- [ ] **Step 8: 运行 store/reranker 测试**

Run: `go test ./internal/agentcore/store/reranker/ -v`
Expected: 所有测试通过

- [ ] **Step 9: Commit**

```
feat(reranker): 迁移 RerankerBase 到 retrieval/reranker/ 包
```

---

## Task 2: 创建 RerankerBase 测试

**Files:**
- Create: `internal/agentcore/retrieval/reranker/reranker_base_test.go`

- [ ] **Step 1: 创建 reranker_base_test.go**

从 store/reranker/base_test.go 迁入基类相关测试，适配新包名和导出名。使用 `reranker.RerankerConfig` 代替 `RerankerConfig`，`reranker.RerankOption` 代替 `RerankOption`，`reranker.Document` 代替 `Document`，`reranker.NewDocument` 代替 `NewDocument`，`reranker.ResolveInstruct` 代替 `resolveInstruct`。

测试用例包括：
- TestNewRerankerBase
- TestNewRerankerBaseWithDefaults
- TestRerankerBase_RequestHeaders_有APIKey
- TestRerankerBase_RequestHeaders_无APIKey
- TestRerankerBase_ParseResponse_标准格式
- TestRerankerBase_ParseResponse_嵌套output格式
- TestRerankerBase_ParseResponse_无匹配结果
- TestRerankerBase_ParseResponse_越界索引
- TestRerankerBase_ExtractDocIDs
- TestRerankerBase_ExtractTexts
- TestRerankerBase_ResolveTopN_设置值
- TestRerankerBase_ResolveTopN_未设置
- TestRerankerBase_ResolveTopN_为零值
- TestRerankerBase_RequestParams
- TestRerankerBase_AssembleParams（验证返回 3 个值：headers, params, docIDs）

- [ ] **Step 2: 运行测试**

Run: `go test ./internal/agentcore/retrieval/reranker/ -v -run TestRerankerBase`
Expected: 所有测试通过

- [ ] **Step 3: Commit**

```
test(reranker): 添加 RerankerBase 基类单元测试
```

---

## Task 3: 创建公共 HTTP 重试工具

**Files:**
- Create: `internal/agentcore/retrieval/utils/doc.go`
- Create: `internal/agentcore/retrieval/utils/api_requests.go`
- Create: `internal/agentcore/retrieval/utils/api_requests_test.go`

- [ ] **Step 1: 创建 retrieval/utils/doc.go**

```go
// Package utils 提供 retrieval 层的公共工具函数。
//
// 本包提供 HTTP 重试请求工具，供 Reranker 和 Embedding 等组件共享。
// 对齐 Python: openjiuwen/core/retrieval/utils/api_requests.py
//
// 文件目录：
//
//	utils/
//	├── doc.go              # 包文档
//	├── api_requests.go     # HTTP 重试请求工具
//	└── api_requests_test.go # 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/utils/api_requests.py
//
// 核心类型/函数索引：
//
//	TaskName       — 任务类型（TaskReranker / TaskEmbedding）
//	RetryConfig    — 重试配置
//	RequestWithRetry      — 带重试的 HTTP POST 请求
//	RequestWithRetrySync  — 带重试的同步 HTTP POST 请求
package utils
```

- [ ] **Step 2: 创建 retrieval/utils/api_requests.go**

```go
package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RetryConfig 重试配置。
//
// 对齐 Python: sync_request_with_retry / async_request_with_retry 的参数
type RetryConfig struct {
	// MaxRetries 最大重试次数，默认 3
	MaxRetries int
	// RetryWait 重试等待基数，默认 100ms
	RetryWait time.Duration
	// Task 任务类型，决定错误码前缀，默认 TaskReranker
	Task TaskName
}

// ──────────────────────────── 枚举 ────────────────────────────

// TaskName 任务类型，决定错误码前缀。
//
// 对齐 Python: Literal["Reranker", "Embedding"]
type TaskName string

const (
	// TaskReranker 重排序任务
	TaskReranker TaskName = "Reranker"
	// TaskEmbedding 嵌入任务
	TaskEmbedding TaskName = "Embedding"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxRetries 默认最大重试次数
	defaultMaxRetries = 3
	// defaultRetryWait 默认重试等待基数
	defaultRetryWait = 100 * time.Millisecond
)

// 审查内容检测关键词，对齐 Python
var censorshipKeywords = []string{"safety", "violation", "policy", "inspection", "appropriate"}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RequestWithRetry 发送带重试的 HTTP POST 请求。
//
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
) (map[string]any, error) {
	return doRequestWithRetry(ctx, httpClient, url, jsonBody, headers, cfg)
}

// RequestWithRetrySync 发送带重试的同步 HTTP POST 请求。
//
// 对齐 Python: sync_request_with_retry。
// 参数和返回值与 RequestWithRetry 一致。
func RequestWithRetrySync(
	ctx context.Context,
	httpClient *http.Client,
	url string,
	jsonBody map[string]any,
	headers map[string]string,
	cfg RetryConfig,
) (map[string]any, error) {
	return doRequestWithRetry(ctx, httpClient, url, jsonBody, headers, cfg)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// doRequestWithRetry 执行带重试的 HTTP POST 请求。
// 对齐 Python: sync_request_with_retry / async_request_with_retry 的核心逻辑。
// Go 中同步和异步调用统一使用此函数（Go 的 goroutine 调度由调用方控制）。
func doRequestWithRetry(
	ctx context.Context,
	httpClient *http.Client,
	url string,
	jsonBody map[string]any,
	headers map[string]string,
	cfg RetryConfig,
) (map[string]any, error) {
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	retryWait := cfg.RetryWait
	if retryWait <= 0 {
		retryWait = defaultRetryWait
	}
	task := cfg.Task
	if task == "" {
		task = TaskReranker
	}

	attempt := 0
	shouldRetry := false
	respStr := "No request sent"
	var response *http.Response
	var lastError error

	for backoff := 1; backoff <= maxRetries; backoff++ {
		// 重试退避：线性退避 + 抖动，对齐 Python random.random() * retry_wait * backoff
		if shouldRetry {
			waitDuration := time.Duration(rand.Float64()*float64(retryWait)) * time.Duration(backoff)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitDuration):
			}
			shouldRetry = false
		}

		// 序列化请求体
		body, err := json.Marshal(jsonBody)
		if err != nil {
			return nil, exception.BuildError(
				requestCallFailedStatus(task),
				exception.WithParam("error_msg", fmt.Sprintf("序列化请求失败: %s", err)),
			)
		}

		// 创建 HTTP 请求
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, exception.BuildError(
				requestCallFailedStatus(task),
				exception.WithParam("error_msg", fmt.Sprintf("创建请求失败: %s", err)),
			)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		// 发送请求
		resp, err := httpClient.Do(req)
		if err != nil {
			respStr = err.Error()
			lastError = err
			shouldRetry = true
			continue
		}

		response = resp
		// 解析响应
		respBody, err := readResponseBody(response)
		if err != nil {
			respStr = err.Error()
			shouldRetry = true
			continue
		}
		respStr = string(respBody)

		// 按状态码处理
		result, handled := handleResponseByStatus(response, respBody, attempt, task)
		if handled {
			return result, nil
		}

		// 需要重试的状态码
		if isRetryableStatus(response.StatusCode) {
			attempt++
			shouldRetry = true
			continue
		}

		// 其他状态码不重试
		break
	}

	// 超过最大重试次数
	return nil, raiseErrors(task, maxRetries, respStr, response, lastError)
}

// handleResponseByStatus 按状态码处理 HTTP 响应。
// 返回 (result, handled)：handled=true 时表示请求成功或失败已确定，不再重试。
// 对齐 Python: _handle_response_by_status
func handleResponseByStatus(resp *http.Response, body []byte, attempt int, task TaskName) (map[string]any, bool) {
	switch resp.StatusCode {
	case http.StatusOK:
		// 200：成功，解析 JSON 返回
		if len(body) == 0 {
			return map[string]any{}, true
		}
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, false
		}
		return result, true

	case http.StatusBadRequest:
		// 400：客户端错误，不重试
		attempt++
		attemptStr := fmt.Sprintf(" (attempt=%d)", attempt)
		logger.Error(logComponent).
			Str("event_type", "api_request_error").
			Str("task", string(task)).
			Str("attempt", attemptStr).
			Str("response", string(body)).
			Msg("API 请求错误")

		// 检测审查内容关键词，对齐 Python
		var respJSON map[string]any
		if err := json.Unmarshal(body, &respJSON); err == nil {
			errObj, _ := respJSON["error"].(map[string]any)
			if errObj == nil {
				errObj = respJSON
			}
			errorCode := strings.ToLower(
				fmt.Sprintf("%v%v%v",
					errObj["code"], errObj["message"], errObj["content"]))
			for _, kw := range censorshipKeywords {
				if strings.Contains(errorCode, kw) {
					logger.Warn(logComponent).
						Str("event_type", "censored_content").
						Str("task", string(task)).
						Msg("请求可能包含被审查的内容")
					break
				}
			}
		}
		return nil, false

	default:
		return nil, false
	}
}

// isRetryableStatus 判断 HTTP 状态码是否可重试。
// 对齐 Python: 429/500/503 触发重试
func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusInternalServerError ||
		statusCode == http.StatusServiceUnavailable
}

// readResponseBody 读取 HTTP 响应体。
func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	return buf.Bytes(), nil
}

// raiseErrors 超过最大重试次数后构建错误。
// 对齐 Python: _raise_errors
func raiseErrors(task TaskName, maxRetries int, respStr string, resp *http.Response, lastError error) error {
	logger.Error(logComponent).
		Str("event_type", "api_request_exhausted").
		Str("task", string(task)).
		Int("max_retries", maxRetries).
		Str("response", respStr).
		Msg("API 请求重试耗尽")

	if resp != nil {
		// 有 HTTP 响应 → RequestCallFailed
		return exception.BuildError(
			requestCallFailedStatus(task),
			exception.WithParam("error_msg", fmt.Sprintf("Failed to get %s after %d attempts: HTTP %d", task, maxRetries, resp.StatusCode)),
			exception.WithCause(lastError),
		)
	}
	// 无 HTTP 响应 → UnreachableCallFailed
	return exception.BuildError(
		unreachableCallFailedStatus(task),
		exception.WithParam("error_msg", fmt.Sprintf("Failed to get %s after %d attempts", task, maxRetries)),
		exception.WithCause(lastError),
	)
}

// requestCallFailedStatus 根据 TaskName 返回对应的请求调用失败错误码。
func requestCallFailedStatus(task TaskName) exception.StatusCode {
	switch task {
	case TaskEmbedding:
		return exception.StatusRetrievalEmbeddingRequestCallFailed
	default:
		return exception.StatusRetrievalRerankerRequestCallFailed
	}
}

// unreachableCallFailedStatus 根据 TaskName 返回对应的不可达调用失败错误码。
func unreachableCallFailedStatus(task TaskName) exception.StatusCode {
	switch task {
	case TaskEmbedding:
		return exception.StatusRetrievalEmbeddingUnreachableCallFailed
	default:
		return exception.StatusRetrievalRerankerUnreachableCallFailed
	}
}
```

- [ ] **Step 3: 创建 retrieval/utils/api_requests_test.go**

使用 `net/http/httptest` 模拟服务端，编写以下测试用例：

- `TestRequestWithRetry_成功响应` — 200 返回 JSON
- `TestRequestWithRetry_重试429成功` — 第 1 次 429，第 2 次 200
- `TestRequestWithRetry_重试500成功` — 第 1 次 500，第 2 次 200
- `TestRequestWithRetry_重试503成功` — 第 1 次 503，第 2 次 200
- `TestRequestWithRetry_400不重试` — 400 直接返回错误
- `TestRequestWithRetry_400审查内容检测` — 400 含 safety 关键词
- `TestRequestWithRetry_最大重试耗尽` — 全部 500 → RequestCallFailed
- `TestRequestWithRetry_网络错误` — 连接失败 → UnreachableCallFailed
- `TestRequestWithRetrySync_同步调用` — 验证同步版本行为一致
- `TestRequestWithRetry_取消上下文` — ctx 取消时立即返回

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/agentcore/retrieval/utils/ -v`
Expected: 所有测试通过

- [ ] **Step 5: Commit**

```
feat(retrieval): 添加公共 HTTP 重试工具（对齐 Python api_requests.py）
```

---

## Task 4: 实现 StandardReranker

**Files:**
- Create: `internal/agentcore/retrieval/reranker/standard.go`
- Create: `internal/agentcore/retrieval/reranker/standard_test.go`

- [ ] **Step 1: 创建 standard.go**

实现 StandardReranker 结构体，嵌入 *RerankerBase，实现 BaseReranker 接口的 4 个方法。包含：
- StandardRerankerOption 函数选项模式
- NewStandardReranker 构造函数（校验配置、去除 APIBase 尾部 /rerank、创建 HTTP 客户端）
- Rerank / RerankDocs / RerankSync / RerankDocsSync 四个接口方法
- assembleParams 覆盖方法（校验输入类型、提取文本、组装参数）

- [ ] **Step 2: 创建 standard_test.go**

使用 `net/http/httptest` 模拟 `/rerank` 端点：

- `TestNewStandardReranker_配置校验` — APIBase 缺失报错
- `TestNewStandardReranker_APIBase去除rerank后缀` — 验证去除尾部 /rerank
- `TestStandardReranker_Rerank_正常` — 标准重排序，返回文档-分数映射
- `TestStandardReranker_RerankDocs_Document输入` — 传入 []*Document
- `TestStandardReranker_Rerank_Instruct选项` — 默认/自定义/禁用 instruct
- `TestStandardReranker_Rerank_ExtraBody合并` — 验证 ExtraBody 合并到请求参数
- `TestStandardReranker_Rerank_API调用失败重试` — 服务端返回 500 → 重试后成功
- `TestStandardReranker_RerankSync_同步调用` — 验证同步版本
- `TestStandardReranker_Rerank_空结果` — 返回空 results
- `TestStandardReranker_ExtraHeaders` — 额外请求头透传
- `TestStandardReranker_接口约束` — 验证满足 BaseReranker 接口

- [ ] **Step 3: 运行测试**

Run: `go test ./internal/agentcore/retrieval/reranker/ -v -run TestStandardReranker`
Expected: 所有测试通过

- [ ] **Step 4: Commit**

```
feat(reranker): 实现 StandardReranker 标准重排序客户端
```

---

## Task 5: 实现 ChatReranker

**Files:**
- Create: `internal/agentcore/retrieval/reranker/chat.go`
- Create: `internal/agentcore/retrieval/reranker/chat_test.go`

- [ ] **Step 1: 创建 chat.go**

实现 ChatReranker 结构体，嵌入 *StandardReranker，覆盖 3 个方法 + 新增 TestCompatibility：

- ChatReranker 结构体（嵌入 *StandardReranker + yesNoIDs + endPoint）
- NewChatReranker 构造函数（校验 YesNoIDs、警告日志、创建 StandardReranker、覆盖 endPoint）
- assembleParams 覆盖（严格限制 size=1）
- requestParams 覆盖（构造 chat completion 格式：messages + logprobs + logit_bias）
- parseResponse 覆盖（解析 logprobs，计算 yes/no 概率）
- TestCompatibility 新增方法（验证服务是否支持 logprobs）

常量：
- chatEndPoint = "/chat/completions"
- docTemplate = "<Document>: {doc}"
- systemInstruct = `Judge whether the Document meets the requirements based on the Query and the Instruct provided. Note that the answer can only be "yes" or "no".`

- [ ] **Step 2: 创建 chat_test.go**

使用 `net/http/httptest` 模拟 `/chat/completions` 端点：

- `TestNewChatReranker_YesNoIDs缺失报错` — 构造时 config.YesNoIDs 零值 → 报错
- `TestNewChatReranker_正常创建` — 有效 YesNoIDs 创建成功
- `TestChatReranker_Rerank_正常解析` — 返回含 logprobs 的响应，计算 yes/no 概率
- `TestChatReranker_Rerank_yes概率计算` — 验证 math.Exp(logprob) 和 confidence/total
- `TestChatReranker_Rerank_多文档报错` — len(docs) > 1 时返回 InputInvalid
- `TestChatReranker_Rerank_logprobs不支持` — 响应无 logprobs → RequestCallFailed
- `TestChatReranker_Rerank_总概率为零` — yes/no 都无匹配 → 返回 0.0
- `TestChatReranker_RerankSync_同步调用` — 验证同步版本
- `TestChatReranker_TestCompatibility_成功` — 服务支持 logprobs → (true, nil)
- `TestChatReranker_TestCompatibility_失败` — 服务不支持 logprobs → (false, err)
- `TestChatReranker_接口约束` — 验证满足 BaseReranker 接口

- [ ] **Step 3: 运行测试**

Run: `go test ./internal/agentcore/retrieval/reranker/ -v -run TestChatReranker`
Expected: 所有测试通过

- [ ] **Step 4: Commit**

```
feat(reranker): 实现 ChatReranker 对话式重排序客户端
```

---

## Task 6: 全量测试 + IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（4.24 状态 ☐ → ✅）

- [ ] **Step 1: 运行全量测试**

Run: `go test ./internal/agentcore/store/reranker/ ./internal/agentcore/retrieval/reranker/ ./internal/agentcore/retrieval/utils/ -v -cover`
Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 2: 运行完整项目编译检查**

Run: `go build ./...`
Expected: 无编译错误

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 4.24 行状态从 `☐` 改为 `✅`

- [ ] **Step 4: Commit**

```
chore: 更新实现计划 4.24 状态为已完成
```
