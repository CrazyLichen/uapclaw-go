# Reranker 接口实现计划（4.23）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Reranker 抽象接口、RerankerConfig、Document、RerankOption 类型和 rerankerBase 基类

**Architecture:** 在 `internal/agentcore/store/reranker/` 包下定义纯接口和类型，与 embedding/index 等包的分层模式一致。rerankerBase 基类提供通用方法供后续实现复用，模式对齐 MemoryIndexBase。

**Tech Stack:** Go 标准库 + github.com/google/uuid + 内部 exception/logger 包

---

### Task 1: 创建 doc.go 包文档

**Files:**
- Create: `internal/agentcore/store/reranker/doc.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p internal/agentcore/store/reranker`

- [ ] **Step 2: 编写 doc.go**

```go
// Package reranker 提供重排序模型的抽象接口和数据模型。
//
// 本包定义了所有重排序模型实现必须满足的 BaseReranker 接口，
// 以及 RerankerConfig 配置、Document 文档模型、RerankOption 可选参数
// 和 rerankerBase 默认实现基类。
// 具体实现类（如 StandardReranker）嵌入 rerankerBase 后
// 只需实现核心 Rerank/RerankDocs 等方法即可满足 BaseReranker 接口。
//
// 文件目录：
//
//	reranker/
//	├── doc.go              # 包文档
//	├── base.go             # BaseReranker 接口 + RerankerConfig + Document + RerankOption
//	├── reranker_base.go    # rerankerBase 基类 + 通用方法
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
//	rerankerBase  — 默认实现基类，提供通用 HTTP 请求/响应处理方法
package reranker
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/reranker/`
Expected: 编译通过（空包无报错）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/reranker/doc.go
git commit -m "feat(reranker): 添加 reranker 包 doc.go 包文档"
```

---

### Task 2: 创建 base.go — Document、RerankerConfig、RerankOption 类型定义

**Files:**
- Create: `internal/agentcore/store/reranker/base.go`

- [ ] **Step 1: 编写 base.go 类型定义**

```go
package reranker

import (
	"context"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultRerankerTimeout 默认请求超时时间（秒）
	defaultRerankerTimeout float64 = 10
	// defaultTemperature 默认生成温度
	defaultTemperature float64 = 0.95
	// defaultTopP 默认 Top-P 采样参数
	defaultTopP float64 = 0.1
	// defaultInstruct 默认指令文本
	defaultInstruct = "Given a search query, retrieve relevant candidates that answer the query."
	// queryTemplate 查询模板，包含指令和查询
	queryTemplate = "<Instruct>: {instruct}\n<Query>: {query}\n"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDocument 创建文档，自动生成 UUID 作为 ID。
func NewDocument(text string, metadata ...map[string]any) *Document {
	doc := &Document{
		ID:   uuid.New().String(),
		Text: text,
	}
	if len(metadata) > 0 {
		doc.Metadata = metadata[0]
	}
	return doc
}

// DocID 提取文档标识：如果输入是 *Document 返回其 ID，否则返回字符串本身。
// 用于 rerank 结果 map 的键。
func DocID(doc any) string {
	if d, ok := doc.(*Document); ok {
		return d.ID
	}
	if d, ok := doc.(string); ok {
		return d
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateConfig 校验 RerankerConfig 字段。
func validateConfig(config *RerankerConfig) error {
	if config.APIBase == "" {
		return exception.ValidateError(exception.StatusRetrievalRerankerInputInvalid,
			exception.WithParam("error_msg", "APIBase is required"),
		)
	}
	if config.Timeout < 0 {
		return exception.ValidateError(exception.StatusRetrievalRerankerInputInvalid,
			exception.WithParam("error_msg", "Timeout must be greater than 0"),
		)
	}
	return nil
}

// resolveInstruct 解析 instruct 选项，返回最终的查询字符串。
// InstructEnabled = nil 或 &true + CustomInstruct = "" → 使用默认指令
// InstructEnabled = &true + CustomInstruct != "" → 使用自定义指令
// InstructEnabled = &false → 不使用指令
func resolveInstruct(query string, opt *RerankOption) string {
	if opt == nil {
		return formatQuery(query, defaultInstruct)
	}
	if opt.InstructEnabled != nil && !*opt.InstructEnabled {
		return query
	}
	instruct := defaultInstruct
	if opt.CustomInstruct != "" {
		instruct = opt.CustomInstruct
	}
	return formatQuery(query, instruct)
}

// formatQuery 使用模板格式化查询字符串。
func formatQuery(query, instruct string) string {
	result := queryTemplate
	result = replacePlaceholder(result, "{instruct}", instruct)
	result = replacePlaceholder(result, "{query}", query)
	return result
}

// replacePlaceholder 替换字符串中的占位符（避免引入 strings.Replace 的性能开销）。
func replacePlaceholder(s, old, new string) string {
	for i := 0; i < len(s)-len(old)+1; i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/reranker/`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/reranker/base.go
git commit -m "feat(reranker): 添加 BaseReranker 接口和类型定义（Document/RerankerConfig/RerankOption）"
```

---

### Task 3: 创建 reranker_base.go — rerankerBase 基类

**Files:**
- Create: `internal/agentcore/store/reranker/reranker_base.go`

- [ ] **Step 1: 编写 reranker_base.go**

```go
package reranker

import (
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// rerankerBase 重排序模型的默认实现基类。
//
// 嵌入此结构体后，实现类只需实现核心的 Rerank/RerankDocs 等方法即可满足 BaseReranker 接口。
// 默认提供 requestHeaders / requestParams / parseResponse / extractDocIDs 等通用方法，
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
// 对内使用（供同包或通过桥接函数供实现包使用），导出以便跨包访问。
func NewRerankerBase(config RerankerConfig, maxRetries int, retryWait time.Duration) *rerankerBase {
	return &rerankerBase{
		config:     config,
		headers:    buildDefaultHeaders(config.APIKey),
		maxRetries: maxRetries,
		retryWait:  retryWait,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRerankerBaseWithDefaults 使用默认值创建重排序基类实例。
func newRerankerBaseWithDefaults(config RerankerConfig) *rerankerBase {
	return NewRerankerBase(config, defaultMaxRetries, defaultRetryWait)
}

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

// requestHeaders 返回默认请求头，子类可覆盖。
func (b *rerankerBase) requestHeaders() map[string]string {
	return b.headers
}

// requestParams 构建请求参数（StandardReranker 风格）。
// 子类应覆盖此方法以适配不同 API 格式（如 DashScope、ChatReranker）。
func (b *rerankerBase) requestParams(query string, documents []string, topN int, opt *RerankOption) map[string]any {
	params := map[string]any{
		"model":           b.config.ModelName,
		"return_documents": false,
		"query":           query,
		"documents":       documents,
		"top_n":           topN,
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
func (b *rerankerBase) parseResponse(responseData map[string]any, docIDs []string) map[string]float64 {
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
func (b *rerankerBase) extractDocIDs(docs []any) []string {
	ids := make([]string, len(docs))
	for i, doc := range docs {
		ids[i] = DocID(doc)
	}
	return ids
}

// extractTexts 从文档列表提取文本列表。
// 字符串直接使用，*Document 使用其 Text 字段。
func (b *rerankerBase) extractTexts(docs []any) []string {
	texts := make([]string, len(docs))
	for i, doc := range docs {
		if d, ok := doc.(*Document); ok {
			texts[i] = d.Text
		} else if s, ok := doc.(string); ok {
			texts[i] = s
		}
	}
	return texts
}

// resolveTopN 解析 TopN 选项，0 或未设置时使用文档总数。
func (b *rerankerBase) resolveTopN(opt *RerankOption, docCount int) int {
	if opt != nil && opt.TopN > 0 {
		return opt.TopN
	}
	return docCount
}

// assembleParams 组装请求参数，将文档和查询合并为完整的请求参数。
// 返回请求头和请求参数。
func (b *rerankerBase) assembleParams(query string, docs []any, opt *RerankOption) (map[string]string, map[string]any) {
	docIDs := b.extractDocIDs(docs)
	texts := b.extractTexts(docs)
	topN := b.resolveTopN(opt, len(docs))
	resolvedQuery := resolveInstruct(query, opt)

	headers := b.requestHeaders()
	params := b.requestParams(resolvedQuery, texts, topN, opt)

	// 确保参数中有 documents 和 top_n
	params["documents"] = texts
	params["top_n"] = topN

	_ = docIDs // docIDs 由调用方用于 parseResponse
	return headers, params
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/reranker/`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/reranker/reranker_base.go
git commit -m "feat(reranker): 添加 rerankerBase 基类和通用方法"
```

---

### Task 4: 创建 base_test.go — 单元测试

**Files:**
- Create: `internal/agentcore/store/reranker/base_test.go`

- [ ] **Step 1: 编写 base_test.go**

```go
package reranker

import (
	"context"
	"encoding/json"
	"testing"
)

// ──────────────────────────── Document 测试 ────────────────────────────

func TestDocument_JSON序列化(t *testing.T) {
	doc := &Document{
		ID:       "doc-001",
		Text:     "测试文档内容",
		Metadata: map[string]any{"source": "test"},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored Document
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ID != doc.ID {
		t.Errorf("ID: 期望 %q, 实际 %q", doc.ID, restored.ID)
	}
	if restored.Text != doc.Text {
		t.Errorf("Text: 期望 %q, 实际 %q", doc.Text, restored.Text)
	}
	if restored.Metadata["source"] != "test" {
		t.Errorf("Metadata[source]: 期望 test, 实际 %v", restored.Metadata["source"])
	}
}

func TestDocument_Metadata为nil时序列化(t *testing.T) {
	doc := &Document{
		ID:   "doc-002",
		Text: "无元数据",
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("反序列化到 map 失败: %v", err)
	}
	if _, ok := raw["metadata"]; !ok {
		t.Error("Metadata 为 nil 时 JSON 中应包含 metadata 字段")
	}
}

func TestDocument_零值序列化(t *testing.T) {
	doc := &Document{}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored Document
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.ID != "" {
		t.Errorf("ID: 期望空字符串, 实际 %q", restored.ID)
	}
	if restored.Text != "" {
		t.Errorf("Text: 期望空字符串, 实际 %q", restored.Text)
	}
}

// ──────────────────────────── NewDocument 测试 ────────────────────────────

func TestNewDocument_自动生成UUID(t *testing.T) {
	doc := NewDocument("测试文本")
	if doc.ID == "" {
		t.Error("NewDocument 应自动生成 UUID 作为 ID")
	}
	if doc.Text != "测试文本" {
		t.Errorf("Text: 期望 %q, 实际 %q", "测试文本", doc.Text)
	}
	if doc.Metadata != nil {
		t.Errorf("无 metadata 参数时 Metadata 应为 nil, 实际 %v", doc.Metadata)
	}
}

func TestNewDocument_带Metadata(t *testing.T) {
	meta := map[string]any{"key": "value"}
	doc := NewDocument("文本", meta)
	if doc.Metadata == nil {
		t.Error("带 metadata 参数时 Metadata 不应为 nil")
	}
	if doc.Metadata["key"] != "value" {
		t.Errorf("Metadata[key]: 期望 value, 实际 %v", doc.Metadata["key"])
	}
}

func TestNewDocument_ID唯一(t *testing.T) {
	doc1 := NewDocument("a")
	doc2 := NewDocument("b")
	if doc1.ID == doc2.ID {
		t.Error("两次调用 NewDocument 生成的 ID 应不同")
	}
}

// ──────────────────────────── DocID 测试 ────────────────────────────

func TestDocID_字符串输入(t *testing.T) {
	result := DocID("hello")
	if result != "hello" {
		t.Errorf("字符串输入: 期望 %q, 实际 %q", "hello", result)
	}
}

func TestDocID_Document输入(t *testing.T) {
	doc := &Document{ID: "doc-123", Text: "内容"}
	result := DocID(doc)
	if result != "doc-123" {
		t.Errorf("Document 输入: 期望 %q, 实际 %q", "doc-123", result)
	}
}

func TestDocID_其他类型(t *testing.T) {
	result := DocID(123)
	if result != "" {
		t.Errorf("不支持的类型应返回空字符串, 实际 %q", result)
	}
}

// ──────────────────────────── RerankerConfig 测试 ────────────────────────────

func TestRerankerConfig_校验通过(t *testing.T) {
	config := &RerankerConfig{APIBase: "https://api.example.com"}
	if err := validateConfig(config); err != nil {
		t.Errorf("有效配置校验应通过, 实际错误: %v", err)
	}
}

func TestRerankerConfig_APIBase必填(t *testing.T) {
	config := &RerankerConfig{APIBase: ""}
	err := validateConfig(config)
	if err == nil {
		t.Fatal("APIBase 为空时应返回错误")
	}
}

// ──────────────────────────── RerankOption 测试 ────────────────────────────

func TestRerankOption_默认指令(t *testing.T) {
	query := resolveInstruct("搜索查询", nil)
	if query == "搜索查询" {
		t.Error("默认应使用指令模板，查询不应等于原始值")
	}
	// 验证包含默认指令和查询
	expectedInstruct := defaultInstruct
	if !contains(query, expectedInstruct) {
		t.Errorf("查询应包含默认指令 %q, 实际: %q", expectedInstruct, query)
	}
	if !contains(query, "搜索查询") {
		t.Errorf("查询应包含原始查询文本, 实际: %q", query)
	}
}

func TestRerankOption_自定义指令(t *testing.T) {
	customInstruct := "自定义指令内容"
	opt := &RerankOption{CustomInstruct: customInstruct}
	query := resolveInstruct("搜索查询", opt)
	if !contains(query, customInstruct) {
		t.Errorf("查询应包含自定义指令 %q, 实际: %q", customInstruct, query)
	}
	if contains(query, defaultInstruct) {
		t.Error("使用自定义指令时不应包含默认指令")
	}
}

func TestRerankOption_禁用指令(t *testing.T) {
	disabled := false
	opt := &RerankOption{InstructEnabled: &disabled}
	query := resolveInstruct("搜索查询", opt)
	if query != "搜索查询" {
		t.Errorf("禁用指令时查询应等于原始值, 实际: %q", query)
	}
}

func TestRerankOption_启用指令无自定义(t *testing.T) {
	enabled := true
	opt := &RerankOption{InstructEnabled: &enabled}
	query := resolveInstruct("搜索查询", opt)
	if !contains(query, defaultInstruct) {
		t.Errorf("启用指令但无自定义时应使用默认指令, 实际: %q", query)
	}
}

func TestRerankOption_启用指令带自定义(t *testing.T) {
	enabled := true
	opt := &RerankOption{InstructEnabled: &enabled, CustomInstruct: "我的指令"}
	query := resolveInstruct("搜索查询", opt)
	if !contains(query, "我的指令") {
		t.Errorf("应包含自定义指令, 实际: %q", query)
	}
}

// ──────────────────────────── rerankerBase 测试 ────────────────────────────

func TestNewRerankerBase(t *testing.T) {
	config := RerankerConfig{
		APIKey:  "test-key",
		APIBase: "https://api.example.com",
	}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	if base == nil {
		t.Fatal("NewRerankerBase 返回 nil")
	}
	if base.maxRetries != 3 {
		t.Errorf("maxRetries: 期望 3, 实际 %d", base.maxRetries)
	}
}

func TestRerankerBase_RequestHeaders_有APIKey(t *testing.T) {
	config := RerankerConfig{
		APIKey:  "test-key",
		APIBase: "https://api.example.com",
	}
	base := NewRerankerBase(config, 3, defaultRetryWait)
	headers := base.requestHeaders()

	if headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type: 期望 application/json, 实际 %q", headers["Content-Type"])
	}
	if headers["Authorization"] != "Bearer test-key" {
		t.Errorf("Authorization: 期望 Bearer test-key, 实际 %q", headers["Authorization"])
	}
}

func TestRerankerBase_RequestHeaders_无APIKey(t *testing.T) {
	config := RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)
	headers := base.requestHeaders()

	if _, ok := headers["Authorization"]; ok {
		t.Error("无 APIKey 时不应包含 Authorization 头")
	}
}

func TestRerankerBase_ParseResponse_标准格式(t *testing.T) {
	config := RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	responseData := map[string]any{
		"results": []any{
			map[string]any{"index": float64(0), "relevance_score": 0.95},
			map[string]any{"index": float64(1), "relevance_score": 0.5},
		},
	}
	docIDs := []string{"doc-0", "doc-1"}

	result := base.parseResponse(responseData, docIDs)
	if result["doc-0"] != 0.95 {
		t.Errorf("doc-0 分数: 期望 0.95, 实际 %f", result["doc-0"])
	}
	if result["doc-1"] != 0.5 {
		t.Errorf("doc-1 分数: 期望 0.5, 实际 %f", result["doc-1"])
	}
}

func TestRerankerBase_ParseResponse_嵌套output格式(t *testing.T) {
	config := RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	responseData := map[string]any{
		"output": map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.8},
			},
		},
	}
	docIDs := []string{"doc-0"}

	result := base.parseResponse(responseData, docIDs)
	if result["doc-0"] != 0.8 {
		t.Errorf("doc-0 分数: 期望 0.8, 实际 %f", result["doc-0"])
	}
}

func TestRerankerBase_ParseResponse_无匹配结果(t *testing.T) {
	config := RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	responseData := map[string]any{}
	docIDs := []string{"doc-0", "doc-1"}

	result := base.parseResponse(responseData, docIDs)
	if result["doc-0"] != 0.0 {
		t.Errorf("无匹配结果时分数应为 0, 实际 %f", result["doc-0"])
	}
	if result["doc-1"] != 0.0 {
		t.Errorf("无匹配结果时分数应为 0, 实际 %f", result["doc-1"])
	}
}

func TestRerankerBase_ExtractDocIDs(t *testing.T) {
	config := RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	docs := []any{
		"plain-text",
		&Document{ID: "doc-123", Text: "内容"},
	}
	ids := base.extractDocIDs(docs)

	if ids[0] != "plain-text" {
		t.Errorf("字符串文档 ID: 期望 plain-text, 实际 %q", ids[0])
	}
	if ids[1] != "doc-123" {
		t.Errorf("Document ID: 期望 doc-123, 实际 %q", ids[1])
	}
}

func TestRerankerBase_ExtractTexts(t *testing.T) {
	config := RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	docs := []any{
		"plain-text",
		&Document{ID: "doc-123", Text: "文档内容"},
	}
	texts := base.extractTexts(docs)

	if texts[0] != "plain-text" {
		t.Errorf("字符串文本: 期望 plain-text, 实际 %q", texts[0])
	}
	if texts[1] != "文档内容" {
		t.Errorf("Document 文本: 期望 文档内容, 实际 %q", texts[1])
	}
}

func TestRerankerBase_ResolveTopN_设置值(t *testing.T) {
	config := RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	opt := &RerankOption{TopN: 5}
	topN := base.resolveTopN(opt, 10)
	if topN != 5 {
		t.Errorf("TopN: 期望 5, 实际 %d", topN)
	}
}

func TestRerankerBase_ResolveTopN_未设置(t *testing.T) {
	config := RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	topN := base.resolveTopN(nil, 10)
	if topN != 10 {
		t.Errorf("未设置 TopN 时应使用文档总数, 期望 10, 实际 %d", topN)
	}
}

func TestRerankerBase_RequestParams(t *testing.T) {
	config := RerankerConfig{
		APIBase:   "https://api.example.com",
		ModelName: "rerank-model",
		ExtraBody: map[string]any{"custom_field": "custom_value"},
	}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	opt := &RerankOption{ExtraParams: map[string]any{"extra_key": "extra_val"}}
	params := base.requestParams("查询文本", []string{"文档1", "文档2"}, 2, opt)

	if params["model"] != "rerank-model" {
		t.Errorf("model: 期望 rerank-model, 实际 %v", params["model"])
	}
	if params["return_documents"] != false {
		t.Errorf("return_documents: 期望 false, 实际 %v", params["return_documents"])
	}
	if params["custom_field"] != "custom_value" {
		t.Errorf("custom_field: 期望 custom_value, 实际 %v", params["custom_field"])
	}
	if params["extra_key"] != "extra_val" {
		t.Errorf("extra_key: 期望 extra_val, 实际 %v", params["extra_key"])
	}
}

// ──────────────────────────── BaseReranker 接口约束测试 ────────────────────────────

// fakeReranker 用于测试的模拟重排序模型
type fakeReranker struct{}

func (f *fakeReranker) Rerank(_ context.Context, query string, docs []string, _ ...RerankOption) (map[string]float64, error) {
	result := make(map[string]float64, len(docs))
	for _, doc := range docs {
		result[doc] = 1.0
	}
	return result, nil
}

func (f *fakeReranker) RerankDocs(_ context.Context, query string, docs []*Document, _ ...RerankOption) (map[string]float64, error) {
	result := make(map[string]float64, len(docs))
	for _, doc := range docs {
		result[doc.ID] = 1.0
	}
	return result, nil
}

func (f *fakeReranker) RerankSync(_ context.Context, query string, docs []string, _ ...RerankOption) (map[string]float64, error) {
	result := make(map[string]float64, len(docs))
	for _, doc := range docs {
		result[doc] = 1.0
	}
	return result, nil
}

func (f *fakeReranker) RerankDocsSync(_ context.Context, query string, docs []*Document, _ ...RerankOption) (map[string]float64, error) {
	result := make(map[string]float64, len(docs))
	for _, doc := range docs {
		result[doc.ID] = 1.0
	}
	return result, nil
}

func TestBaseReranker_接口约束(t *testing.T) {
	// 验证 fakeReranker 满足 BaseReranker 接口
	var _ BaseReranker = &fakeReranker{}
}

func TestBaseReranker_FakeRerank(t *testing.T) {
	reranker := &fakeReranker{}
	result, err := reranker.Rerank(context.Background(), "查询", []string{"文档1", "文档2"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("结果数量: 期望 2, 实际 %d", len(result))
	}
	if result["文档1"] != 1.0 {
		t.Errorf("文档1 分数: 期望 1.0, 实际 %f", result["文档1"])
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/store/reranker/`
Expected: 所有测试通过

- [ ] **Step 3: 检查测试覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/reranker/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/reranker/base_test.go
git commit -m "test(reranker): 添加 Document/RerankerConfig/RerankOption/rerankerBase 单元测试"
```

---

### Task 5: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` (第 321 行)

- [ ] **Step 1: 更新 4.23 状态为已完成**

将 `| 4.23 | ☐ | Reranker 接口` 改为 `| 4.23 | ✅ | Reranker 接口`

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 4.23 Reranker 接口状态为已完成"
```

---

### Task 6: 最终验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过，无错误

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/reranker/ -v`
Expected: 所有测试通过

- [ ] **Step 3: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/store/reranker/ && go tool cover -func=coverage.out`
Expected: 覆盖率 ≥ 85%
