# 5.20 TokenCounter (Tiktoken Go 实现) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TiktokenCounter，为上下文引擎提供精确的 Token 计数能力

**Architecture:** 基于 `tiktoken-go/tokenizer` 纯 Go 库实现 TokenCounter 接口。利用 tokenizer 内置的 `ForModel()` 函数和 `Count()` 方法，避免自行维护模型映射表。对非 OpenAI 模型降级到 `Cl100kBase`，初始化或运行时失败时降级到 `len(text)//4`。完全对齐 Python 的消息格式化逻辑（包括 AssistantMessage ToolCalls 特殊处理）。

**Tech Stack:** Go 1.25, tiktoken-go/tokenizer v0.8.0, 项目内部 logger/llm_schema/schema 包

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/agentcore/context_engine/token/tiktoken_counter.go` | 新增 | TiktokenCounter 结构体、构造函数、三个接口方法、辅助函数 |
| `internal/agentcore/context_engine/token/tiktoken_counter_test.go` | 新增 | 单元测试 |
| `internal/agentcore/context_engine/token/doc.go` | 更新 | 添加 tiktoken_counter.go 条目，更新包概述 |
| `go.mod` | 更新 | 添加 tiktoken-go/tokenizer 依赖，移除 pkoukk 注释占位 |

### 关键 API 确认

`tiktoken-go/tokenizer` v0.8.0 的实际 API：

```go
// 类型
type Model string       // 模型名常量，如 tokenizer.GPT4, tokenizer.GPT4o
type Encoding string    // 编码名常量，如 tokenizer.Cl100kBase, tokenizer.O200kBase
type Codec interface {  // 编码器接口
    GetName() string
    Count(string) (int, error)         // 直接计数 ★
    Encode(string) ([]uint, []string, error)  // 编码返回 (ids, tokens, error)
    Decode([]uint) (string, error)
}

// 构造函数
func Get(encoding Encoding) (Codec, error)   // 按编码名创建
func ForModel(model Model) (Codec, error)    // 按模型名创建（内置映射表 + 前缀匹配）
```

**设计调整**：不再自行维护 `model2enc` 映射表，改为：
1. 先尝试 `tokenizer.ForModel(tokenizer.Model(model))` 利用内置映射
2. `ForModel` 失败时降级到 `tokenizer.Get(tokenizer.Cl100kBase)`
3. 仍保留 `model2enc` 映射表，用于 Python 对齐的模型名映射（`ForModel` 的模型常量与 Python 映射表不完全一致）

---

### Task 1: 添加 tiktoken-go/tokenizer 依赖

**Files:**
- Modify: `go.mod:24-25` (移除 pkoukk 注释占位)

- [ ] **Step 1: 添加依赖**

```bash
cd /home/opensource/uap-claw-go
export GOPROXY=https://goproxy.cn,direct
go get github.com/tiktoken-go/tokenizer@v0.8.0
```

- [ ] **Step 2: 更新 go.mod 注释**

将第 25 行：
```
// - github.com/pkoukk/tiktoken-go    Tiktoken
```
替换为：
```
// - github.com/tiktoken-go/tokenizer Tiktoken（纯 Go 实现，BPE 编译期嵌入）
```

- [ ] **Step 3: 运行 go mod tidy**

```bash
go mod tidy
```

- [ ] **Step 4: 验证依赖**

```bash
grep "tiktoken-go/tokenizer" go.mod
```

预期输出包含 `github.com/tiktoken-go/tokenizer v0.8.0`

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: 添加 tiktoken-go/tokenizer 依赖（5.20 TokenCounter）"
```

---

### Task 2: 实现 TiktokenCounter 核心代码

**Files:**
- Create: `internal/agentcore/context_engine/token/tiktoken_counter.go`

- [ ] **Step 1: 创建 tiktoken_counter.go**

```go
package token

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tiktoken-go/tokenizer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	common_schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TiktokenCounter 基于 tiktoken-go/tokenizer 的 Token 计数器。
//
// 提供文本、消息列表和工具定义的 Token 计数能力。
// 初始化时按模型名称选定 encoding，初始化失败时降级为 len(text)//4。
//
// 对应 Python: openjiuwen/core/context_engine/token/tiktoken_counter.py (TiktokenCounter)
type TiktokenCounter struct {
	// enc tiktoken 编码器实例，初始化失败时为 nil
	enc tokenizer.Codec
	// model 构造时指定的模型名称
	model string
	// fallbackWarned 是否已输出降级警告（只警告一次）
	fallbackWarned bool
	// mu 保护 fallbackWarned 的互斥锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// defaultModel 默认模型名称（对齐 Python: TiktokenCounter(model="gpt-4")）
const defaultModel = "gpt-4"

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// model2enc 模型名称到 tiktoken 编码名称的映射表。
//
// 对齐 Python: TiktokenCounter._MODEL2ENC。
// tokenizer.ForModel() 内置了更完整的映射，此表用于明确对齐 Python 端的映射关系，
// 以及覆盖 ForModel 不支持的模型名（如 "text-embedding-3-small"/"text-embedding-3-large"）。
var model2enc = map[string]tokenizer.Encoding{
	"gpt-3.5-turbo":          tokenizer.Cl100kBase,
	"gpt-4":                  tokenizer.Cl100kBase,
	"gpt-4-turbo":            tokenizer.Cl100kBase,
	"gpt-4o":                 tokenizer.O200kBase,
	"gpt-4o-mini":            tokenizer.O200kBase,
	"text-embedding-ada-002": tokenizer.Cl100kBase,
	"text-embedding-3-small": tokenizer.Cl100kBase,
	"text-embedding-3-large": tokenizer.Cl100kBase,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTiktokenCounter 创建 TiktokenCounter 实例。
//
// model 为模型名称，用于选择对应的 encoding；空字符串默认使用 "gpt-4"。
// 初始化失败时 enc 为 nil，后续计数降级为 len(text)//4。
//
// 对应 Python: TiktokenCounter(model="gpt-4")
func NewTiktokenCounter(model string) *TiktokenCounter {
	if model == "" {
		model = defaultModel
	}

	tc := &TiktokenCounter{
		model: model,
	}

	// 优先使用 model2enc 映射表（对齐 Python）
	if encName, ok := model2enc[model]; ok {
		enc, err := tokenizer.Get(encName)
		if err == nil {
			tc.enc = enc
			return tc
		}
		logger.Warn(logComponent).Str("model", model).
			Str("encoding", string(encName)).
			Err(err).
			Msg("Tiktoken 编码器初始化失败（按映射表），使用 len(text)//4 降级计算")
		return tc
	}

	// 其次尝试 tokenizer.ForModel（利用其内置映射 + 前缀匹配）
	enc, err := tokenizer.ForModel(tokenizer.Model(model))
	if err == nil {
		tc.enc = enc
		return tc
	}

	// 最后降级到 Cl100kBase
	enc, fallbackErr := tokenizer.Get(tokenizer.Cl100kBase)
	if fallbackErr == nil {
		tc.enc = enc
		logger.Warn(logComponent).Str("model", model).
			Err(err).
			Msg("模型不在映射表中且 ForModel 失败，降级使用 cl100k_base 编码")
		return tc
	}

	// 全部失败，enc 为 nil，后续使用 len(text)//4
	logger.Warn(logComponent).Str("model", model).
		Err(fallbackErr).
		Msg("Tiktoken 初始化完全失败，使用 len(text)//4 降级计算")
	return tc
}

// Count 计算文本的 Token 数量。
//
// 优先使用 tiktoken 编码器精确计算，失败时降级为 len(text)//4。
//
// 对应 Python: TiktokenCounter.count(text, model="")
func (tc *TiktokenCounter) Count(text string, model string) int {
	if tc.enc != nil {
		count, err := tc.enc.Count(text)
		if err == nil {
			return count
		}
		// 运行时编码失败
		logger.Warn(logComponent).Str("model", tc.model).
			Int("text_len", len(text)).
			Err(err).
			Msg("Tiktoken 编码失败，使用 len//4 降级计算")
	}
	return tc.fallbackCount(text)
}

// CountMessages 计算消息列表的 Token 数量。
//
// 按 OpenAI 惯例格式化消息：<|start|>{role}\n{content}<|end|>，
// AssistantMessage 额外序列化 ToolCalls 计入 token，末尾 +3 tokens。
//
// 对应 Python: TiktokenCounter.count_messages(messages, model="")
func (tc *TiktokenCounter) CountMessages(messages []llm_schema.BaseMessage, model string) int {
	if len(messages) == 0 {
		return 0
	}
	total := 0
	for _, msg := range messages {
		// 格式: <|start|>{role}\n{content}<|end|>
		content := contentToString(msg.GetContent())
		piece := fmt.Sprintf("<|start|>%s\n%s<|end|>", msg.GetRole(), content)
		total += tc.Count(piece, model)

		// AssistantMessage 特殊处理：额外计算 ToolCalls token
		if asst, ok := msg.(*llm_schema.AssistantMessage); ok && len(asst.ToolCalls) > 0 {
			toolCallsJSON, err := json.Marshal(asst.ToolCalls)
			if err == nil {
				total += tc.Count(string(toolCallsJSON), model)
			}
		}
	}
	return total + 3
}

// CountTools 计算工具定义的 Token 数量。
//
// 按格式 <|start|>functions.{name}:{idx}\n{json}<|end|> 计数，末尾 +3 tokens。
//
// 对应 Python: TiktokenCounter.count_tools(tools, model="")
func (tc *TiktokenCounter) CountTools(tools []*common_schema.ToolInfo, model string) int {
	if len(tools) == 0 {
		return 0
	}
	total := 0
	for idx, tool := range tools {
		functionObj := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		}
		jsonStr, err := json.Marshal(functionObj)
		if err != nil {
			// JSON 序列化失败，使用字段拼接作为降级
			jsonStr = []byte(tool.Name + tool.Description)
		}
		// 格式: <|start|>functions.{name}:{idx}\n{json}<|end|>
		piece := fmt.Sprintf("<|start|>functions.%s:%d\n%s<|end|>", tool.Name, idx, string(jsonStr))
		total += tc.Count(piece, model)
	}
	return total + 3
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fallbackCount 降级 token 计算：len(text)//4。
//
// 只在首次调用时输出警告日志（通过 fallbackWarned 标志控制），
// 对齐 Python: TiktokenCounter.count() 中的 fallback 逻辑。
func (tc *TiktokenCounter) fallbackCount(text string) int {
	tc.mu.Lock()
	if !tc.fallbackWarned {
		tc.fallbackWarned = true
		logger.Warn(logComponent).Str("model", tc.model).
			Msg("Tiktoken 初始化失败，使用 len(text)//4 降级计算")
	}
	tc.mu.Unlock()
	return len(text) / 4
}

// contentToString 将 MessageContent 转换为字符串用于 token 计数。
//
// 纯文本模式直接返回文本内容，多模态模式提取 Type=="text" 的分片文本拼接，
// 忽略 image_url 等非文本分片（它们在 LLM 端有独立的 token 计算规则）。
// 不使用 MessageContent.String()，因为多模态模式下它会 json.Marshal(parts)，
// 将 image_url 的 JSON 结构也计入 token，与 Python 行为不一致。
func contentToString(content llm_schema.MessageContent) string {
	if content.IsText() {
		return content.Text()
	}
	// 多模态模式：只提取 text 分片
	var sb strings.Builder
	for _, part := range content.Parts() {
		if part.Type == "text" {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}
```

**注意**：上述代码中 `import` 用了 `strings`，需要在 import 块中加入。完整 import：

```go
import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tiktoken-go/tokenizer"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	common_schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

- [ ] **Step 2: 验证编译**

```bash
cd /home/opensource/uap-claw-go
export GOPROXY=https://goproxy.cn,direct
go build ./internal/agentcore/context_engine/token/...
```

预期：编译通过，无错误

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/context_engine/token/tiktoken_counter.go
git commit -m "feat: 实现 TiktokenCounter 核心代码（5.20）"
```

---

### Task 3: 编写单元测试

**Files:**
- Create: `internal/agentcore/context_engine/token/tiktoken_counter_test.go`

- [ ] **Step 1: 创建 tiktoken_counter_test.go**

```go
package token

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/stretchr/testify/assert"
)

// TestNewTiktokenCounter_默认模型 验证 model 为空时默认使用 "gpt-4"
func TestNewTiktokenCounter_默认模型(t *testing.T) {
	tc := NewTiktokenCounter("")
	assert.NotNil(t, tc)
	assert.Equal(t, "gpt-4", tc.model)
	assert.NotNil(t, tc.enc, "enc 不应为 nil（gpt-4 是已知模型）")
}

// TestNewTiktokenCounter_GPT4o 验证 "gpt-4o" 使用 o200k_base 编码
func TestNewTiktokenCounter_GPT4o(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4o")
	assert.NotNil(t, tc)
	assert.NotNil(t, tc.enc, "enc 不应为 nil（gpt-4o 是已知模型）")
	assert.Equal(t, "gpt-4o", tc.model)
}

// TestNewTiktokenCounter_GPT35Turbo 验证 "gpt-3.5-turbo" 使用 cl100k_base
func TestNewTiktokenCounter_GPT35Turbo(t *testing.T) {
	tc := NewTiktokenCounter("gpt-3.5-turbo")
	assert.NotNil(t, tc)
	assert.NotNil(t, tc.enc)
}

// TestNewTiktokenCounter_未知模型降级 验证未知模型降级到 Cl100kBase
func TestNewTiktokenCounter_未知模型降级(t *testing.T) {
	tc := NewTiktokenCounter("qwen-max")
	assert.NotNil(t, tc)
	// 未知模型应降级到 Cl100kBase（enc 不为 nil）或 len(text)//4（enc 为 nil）
	// tokenizer.ForModel("qwen-max") 会失败，但降级到 Cl100kBase 应成功
	assert.NotNil(t, tc.enc, "未知模型应降级到 cl100k_base")
}

// TestCount_纯文本 验证英文/中文/混合文本的 token 计数
func TestCount_纯文本(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	// 英文文本
	enCount := tc.Count("hello world", "gpt-4")
	assert.Greater(t, enCount, 0, "英文文本 token 数应大于 0")

	// 中文文本
	zhCount := tc.Count("你好世界", "gpt-4")
	assert.Greater(t, zhCount, 0, "中文文本 token 数应大于 0")

	// 混合文本
	mixCount := tc.Count("hello 世界", "gpt-4")
	assert.Greater(t, mixCount, 0, "混合文本 token 数应大于 0")

	// 长文本 token 数应大于短文本
	longCount := tc.Count("This is a longer sentence with more words.", "gpt-4")
	assert.Greater(t, longCount, enCount, "长文本 token 数应大于短文本")
}

// TestCount_空字符串 验证返回 0
func TestCount_空字符串(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")
	assert.Equal(t, 0, tc.Count("", "gpt-4"))
}

// TestCountMessages_多角色 验证 system/user/assistant/tool 消息格式化后计数
func TestCountMessages_多角色(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	messages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("You are a helpful assistant."),
		llm_schema.NewUserMessage("What is the weather?"),
		llm_schema.NewAssistantMessage("The weather is sunny."),
	}

	count := tc.CountMessages(messages, "gpt-4")
	assert.Greater(t, count, 0, "多角色消息 token 数应大于 0")
	// 末尾 +3
	assert.GreaterOrEqual(t, count, 3, "应包含末尾 3 tokens")
}

// TestCountMessages_AssistantToolCalls 验证 AssistantMessage 带 ToolCalls 时额外计数
func TestCountMessages_AssistantToolCalls(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	// 不带 ToolCalls
	msgNoCalls := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("hello"),
	}
	countNoCalls := tc.CountMessages(msgNoCalls, "gpt-4")

	// 带 ToolCalls
	calls := []*llm_schema.ToolCall{
		llm_schema.NewToolCall("call_1", "search", `{"query":"weather"}`),
	}
	msgWithCalls := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(calls)),
	}
	countWithCalls := tc.CountMessages(msgWithCalls, "gpt-4")

	assert.Greater(t, countWithCalls, countNoCalls,
		"带 ToolCalls 的消息 token 数应大于不带 ToolCalls 的消息")
}

// TestCountMessages_空列表 验证返回 0
func TestCountMessages_空列表(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")
	assert.Equal(t, 0, tc.CountMessages(nil, "gpt-4"))
	assert.Equal(t, 0, tc.CountMessages([]llm_schema.BaseMessage{}, "gpt-4"))
}

// TestCountTools_多个工具 验证 tools 按 functions.{name}:{idx} 格式计数
func TestCountTools_多个工具(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	tools := []*schema.ToolInfo{
		schema.NewToolInfo("search", "Search the web", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		}),
		schema.NewToolInfo("calculator", "Calculate math", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{"type": "string"},
			},
		}),
	}

	count := tc.CountTools(tools, "gpt-4")
	assert.Greater(t, count, 0, "工具 token 数应大于 0")
	// 末尾 +3
	assert.GreaterOrEqual(t, count, 3, "应包含末尾 3 tokens")
}

// TestCountTools_空列表 验证返回 0
func TestCountTools_空列表(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")
	assert.Equal(t, 0, tc.CountTools(nil, "gpt-4"))
	assert.Equal(t, 0, tc.CountTools([]*schema.ToolInfo{}, "gpt-4"))
}

// TestCountTools_Parameters为空 验证 parameters 为 nil 时的处理
func TestCountTools_Parameters为空(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	tools := []*schema.ToolInfo{
		schema.NewToolInfo("simple_tool", "A simple tool", nil),
	}

	count := tc.CountTools(tools, "gpt-4")
	assert.Greater(t, count, 0, "即使 parameters 为空，token 数也应大于 0")
}

// TestModel2Enc_所有映射 遍历 model2enc 映射表验证每个映射可以创建编码器
func TestModel2Enc_所有映射(t *testing.T) {
	for model, encName := range model2enc {
		tc := NewTiktokenCounter(model)
		assert.NotNil(t, tc.enc, "模型 %s 的编码器不应为 nil", model)
		assert.Equal(t, model, tc.model)
		_ = encName // encName 仅用于确认映射值有效
	}
}

// TestContentToString_纯文本 验证纯文本内容直接返回
func TestContentToString_纯文本(t *testing.T) {
	content := llm_schema.NewTextContent("hello world")
	result := contentToString(content)
	assert.Equal(t, "hello world", result)
}

// TestContentToString_多模态提取文本 验证多模态内容提取 text 分片拼接
func TestContentToString_多模态提取文本(t *testing.T) {
	content := llm_schema.NewMultiModalContent(
		llm_schema.ContentPart{Type: "text", Text: "Hello "},
		llm_schema.ContentPart{Type: "image_url", ImageURL: &llm_schema.ImageURL{URL: "https://example.com/img.png"}},
		llm_schema.ContentPart{Type: "text", Text: "World"},
	)
	result := contentToString(content)
	assert.Equal(t, "Hello World", result, "应只提取 text 分片，忽略 image_url")
}

// TestContentToString_空文本 验证空文本内容
func TestContentToString_空文本(t *testing.T) {
	content := llm_schema.NewTextContent("")
	result := contentToString(content)
	assert.Equal(t, "", result)
}

// TestTiktokenCounter_实现接口 验证 TiktokenCounter 实现了 TokenCounter 接口
func TestTiktokenCounter_实现接口(t *testing.T) {
	var _ TokenCounter = (*TiktokenCounter)(nil)
}

// TestCount_不同模型结果一致 验证同一文本在不同编码下 token 数不同
func TestCount_不同模型结果(t *testing.T) {
	tc4 := NewTiktokenCounter("gpt-4")
	tc4o := NewTiktokenCounter("gpt-4o")

	text := "hello world"
	count4 := tc4.Count(text, "")
	count4o := tc4o.Count(text, "")

	// 两者都应能正常计数
	assert.Greater(t, count4, 0)
	assert.Greater(t, count4o, 0)
	// cl100k_base 和 o200k_base 对同一文本的 token 数可能不同
	// 但都应返回有效值
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go
export GOPROXY=https://goproxy.cn,direct
go test -v -cover ./internal/agentcore/context_engine/token/...
```

预期：所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 3: 检查覆盖率**

```bash
go test -coverprofile=coverage.out ./internal/agentcore/context_engine/token/...
go tool cover -func=coverage.out | tail -1
```

预期：total 覆盖率 ≥ 85%

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/context_engine/token/tiktoken_counter_test.go
git commit -m "test: 添加 TiktokenCounter 单元测试（5.20）"
```

---

### Task 4: 更新 doc.go

**Files:**
- Modify: `internal/agentcore/context_engine/token/doc.go`

- [ ] **Step 1: 更新 doc.go**

将现有内容替换为：

```go
// Package token 提供上下文引擎的 Token 计数能力。
//
// 定义 TokenCounter 抽象接口及其 TiktokenCounter 实现，供 ModelContext 统计
// 消息和工具定义的 Token 数量。TiktokenCounter 基于 tiktoken-go/tokenizer
// 纯 Go 库，BPE 字典编译期嵌入，无需运行时下载。
//
// 文件目录：
//
//	token/
//	├── doc.go                 # 包文档
//	├── base.go                # TokenCounter 接口定义
//	└── tiktoken_counter.go    # TiktokenCounter 实现（基于 tiktoken-go/tokenizer）
//
// 对应 Python 代码：openjiuwen/core/context_engine/token/
package token
```

- [ ] **Step 2: 验证编译**

```bash
go build ./internal/agentcore/context_engine/token/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/context_engine/token/doc.go
git commit -m "docs: 更新 token 包文档，添加 tiktoken_counter.go 条目（5.20）"
```

---

### Task 5: 验证整体编译与测试

**Files:**
- 无新增/修改

- [ ] **Step 1: 检查残留编译进程**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || echo "无残留进程"
```

- [ ] **Step 2: 全量编译**

```bash
cd /home/opensource/uap-claw-go
export GOPROXY=https://goproxy.cn,direct
go build ./...
```

预期：编译通过

- [ ] **Step 3: 运行 token 包测试**

```bash
go test -v -cover ./internal/agentcore/context_engine/token/...
```

预期：所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 4: 运行 context_engine 全包测试**

```bash
go test -v ./internal/agentcore/context_engine/...
```

预期：所有测试通过（确保新代码未破坏已有测试）

- [ ] **Step 5: 更新 IMPLEMENTATION_PLAN.md**

将 5.20 行的状态从 `☐` 改为 `✅`：

| 步骤 | 原状态 | 新状态 |
|------|--------|--------|
| 5.20 | `☐` | `✅` |

内容更新为：
```
| 5.20 | ✅ | TokenCounter | ✅ TiktokenCounter 实现（基于 tiktoken-go/tokenizer）；✅ model2enc 映射表 + ForModel 降级策略；✅ Count/CountMessages/CountTools 三个方法（含 AssistantMessage ToolCalls 特殊处理）；✅ 两级降级（初始化失败→Cl100kBase→len//4）；✅ contentToString 多模态内容提取；✅ 测试覆盖率 ≥ 85% | `openjiuwen/core/context_engine/token/` |
```

- [ ] **Step 6: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.20 TokenCounter 状态为已完成"
```
