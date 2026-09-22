# 9.82 Context Evolver P3 ReMe 算法实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ReMe（Reflective Memory）算法——Context Evolver 三条记忆管线中最先实现的一条，包含 core 层扩展、9 个 Summarize Op、3 个 Retrieve Op、7 个提示词、5 个工具函数。

**Architecture:** P3 对 core 层做必要扩展（LLMService/EmbeddingService 本地接口 + OpBase 结构体 + VectorStoreService.GetAll），然后新增 `retrieve/reme/` 和 `summary/reme/` 两个子包，目录结构对齐 Python。每个 Op 嵌入 OpBase 并通过构造注入 *ServiceContext 访问服务。测试通过 fakeLLMService/fakeEmbeddingService mock 接口。

**Tech Stack:** Go 1.18+、标准库（context/encoding/json/regexp/math/fmt/time）、testify、errgroup

**设计文档:** `docs/superpowers/specs/2027-12-15-context-evolver-9.82-p3-reme-design.md`

---

## 编码规范提醒

- 所有注释使用中文
- 源码声明排列顺序：接口→结构体→枚举→常量→全局变量→导出函数→非导出函数，各类之间用分隔注释区分
- 每个包需要 doc.go
- 测试覆盖率 ≥ 85%
- 编译前必须 `export GOPROXY=https://goproxy.cn,direct`
- 编译前检查 `pgrep -f 'go (build|test)'` 杀掉残留进程
- 提示词必须一比一复刻 Python 原文，不做自行翻译
- Python 的 f-string 使用 `{{` 和 `}}` 转义大括号，Go 反引号字符串中不需要转义，直接用 `{xxx}` 即可

---

## Task 1: core/context 层扩展

**目标：** 在 `core/context/service_context.go` 中新增 LLMService/EmbeddingService 接口 + GenerateConfig + GenerateOption + WithXxx 选项函数；VectorStoreService 加 GetAll 方法；ServiceContext.LLM() 改返回 LLMService；ServiceContext.EmbeddingModel() 改返回 EmbeddingService。同步更新测试和 doc.go。

**涉及文件：**
- `internal/agentcore/context_evolver/core/context/service_context.go` — 修改
- `internal/agentcore/context_evolver/core/context/service_context_test.go` — 修改
- `internal/agentcore/context_evolver/core/context/doc.go` — 修改

### 1.1 service_context.go 新增内容

在接口区块（VectorStoreService 之后）新增：

```go
// LLMService LLM 服务本地接口。
// 对齐 Python OpenAILLMWrapper.async_generate(prompt, system_prompt?, temperature?, max_tokens?)。
// P6 的 OpenAILLMWrapper 实现此接口。
type LLMService interface {
	// Generate 调用 LLM 生成文本响应。
	// 对齐 Python async_generate(prompt) → str。
	Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error)
}

// EmbeddingService Embedding 模型本地接口。
// 对齐 Python OpenAIEmbeddingWrapper.async_embed(text)/async_embed_batch(texts)。
// P6 的 OpenAIEmbeddingWrapper 实现此接口。
type EmbeddingService interface {
	// Embed 生成单文本的向量嵌入。
	// 对齐 Python async_embed(text) → List[float]。
	Embed(ctx context.Context, text string) ([]float64, error)
	// EmbedBatch 批量生成文本的向量嵌入。
	// 对齐 Python async_embed_batch(texts) → List[List[float]]。
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
}
```

VectorStoreService 接口新增 GetAll 方法：

```go
// GetAll 获取所有向量节点，可选按 metadata 过滤。
// 对齐 Python MemoryVectorStore.get_all(metadata_filter)。
// MemoryDeduplicationOp 和 PersistMemoryOp 依赖此方法。
GetAll(metadataFilter map[string]any) []*schema.VectorNode
```

在结构体区块新增 GenerateConfig：

```go
// GenerateConfig LLM 调用的可选配置。
type GenerateConfig struct {
	// SystemPrompt 系统提示词，空字符串表示未设置
	SystemPrompt string
	// Temperature 生成温度，0 表示未设置，使用模型默认值
	Temperature float64
	// MaxTokens 最大生成 token 数，0 表示未设置，使用模型默认值
	MaxTokens int
}
```

在导出函数区块新增选项函数：

```go
// WithSystemPrompt 设置 LLM 调用的系统提示词。
func WithSystemPrompt(s string) GenerateOption {
	return func(c *GenerateConfig) { c.SystemPrompt = s }
}

// WithTemperature 设置 LLM 调用的生成温度。
func WithTemperature(f float64) GenerateOption {
	return func(c *GenerateConfig) { c.Temperature = f }
}

// WithMaxTokens 设置 LLM 调用的最大生成 token 数。
func WithMaxTokens(n int) GenerateOption {
	return func(c *GenerateConfig) { c.MaxTokens = n }
}
```

GenerateOption 类型定义（放在结构体区块，与 GenerateConfig 相邻）：

```go
// GenerateOption LLM 调用选项函数。
type GenerateOption func(*GenerateConfig)
```

### 1.2 ServiceContext 方法返回值变更

```go
// LLM 获取 LLM 服务。
// 对齐 Python ServiceContext.llm 属性。
// 未注册或类型不匹配时返回 nil。
func (sc *ServiceContext) LLM() LLMService {
	svc := sc.GetService("llm")
	if svc == nil {
		return nil
	}
	llm, ok := svc.(LLMService)
	if !ok {
		return nil
	}
	return llm
}

// EmbeddingModel 获取 Embedding 模型服务。
// 对齐 Python ServiceContext.embedding_model 属性。
// 未注册或类型不匹配时返回 nil。
func (sc *ServiceContext) EmbeddingModel() EmbeddingService {
	svc := sc.GetService("embedding_model")
	if svc == nil {
		return nil
	}
	emb, ok := svc.(EmbeddingService)
	if !ok {
		return nil
	}
	return emb
}
```

### 1.3 service_context_test.go 变更

1. mockVectorStore 新增 GetAll 方法：

```go
func (m *mockVectorStore) GetAll(_ map[string]any) []*schema.VectorNode {
	var result []*schema.VectorNode
	for _, node := range m.nodes {
		result = append(result, node)
	}
	return result
}
```

2. 新增 fakeLLMService 和 fakeEmbeddingService：

```go
// fakeLLMService LLMService 的 mock 实现
type fakeLLMService struct {
	response string
	err      error
}

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...GenerateOption) (string, error) {
	return f.response, f.err
}

// fakeEmbeddingService EmbeddingService 的 mock 实现
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return f.embeddings, f.err
}
```

3. 更新 TestServiceContext_LLM 和 TestServiceContext_EmbeddingModel：

```go
func TestServiceContext_LLM(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.LLM())

	// 注册实现 LLMService 接口的 mock
	llm := &fakeLLMService{response: "test-response"}
	sc.RegisterService("llm", llm)
	result := sc.LLM()
	require.NotNil(t, result)

	// 注册未实现接口的值，返回 nil
	sc2 := NewServiceContext()
	sc2.RegisterService("llm", "not-a-llm")
	assert.Nil(t, sc2.LLM())
}

func TestServiceContext_EmbeddingModel(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.EmbeddingModel())

	// 注册实现 EmbeddingService 接口的 mock
	emb := &fakeEmbeddingService{embeddings: [][]float64{{0.1, 0.2}}}
	sc.RegisterService("embedding_model", emb)
	result := sc.EmbeddingModel()
	require.NotNil(t, result)

	// 注册未实现接口的值，返回 nil
	sc2 := NewServiceContext()
	sc2.RegisterService("embedding_model", "not-an-embedding")
	assert.Nil(t, sc2.EmbeddingModel())
}
```

4. 新增 GenerateOption 测试：

```go
func TestGenerateOption(t *testing.T) {
	c := &GenerateConfig{}
	WithSystemPrompt("sys")(c)
	assert.Equal(t, "sys", c.SystemPrompt)
	WithTemperature(0.7)(c)
	assert.Equal(t, 0.7, c.Temperature)
	WithMaxTokens(100)(c)
	assert.Equal(t, 100, c.MaxTokens)
}
```

### 1.4 doc.go 更新

```go
// 文件目录：
//
//	context/
//	├── doc.go                # 包文档
//	├── runtime_context.go    # RuntimeContext 操作间上下文
//	└── service_context.go    # ServiceContext + LLMService + EmbeddingService + VectorStoreService + GenerateConfig
```

### 1.5 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/core/context/... -v -count=1
go test ./internal/agentcore/context_evolver/core/context/... -cover
```

---

## Task 2: core/op 层扩展

**目标：** 在 `core/op/base_op.go` 中新增 OpBase 结构体 + NewOpBase + 服务访问方法。新增测试。更新 doc.go。

**涉及文件：**
- `internal/agentcore/context_evolver/core/op/base_op.go` — 修改
- `internal/agentcore/context_evolver/core/op/base_op_test.go` — 新增
- `internal/agentcore/context_evolver/core/op/doc.go` — 修改

### 2.1 base_op.go 新增内容

在结构体区块（BaseOp 接口之后）新增：

```go
// OpBase 操作基类，提供对 ServiceContext 中服务的统一访问。
//
// 对齐 Python BaseOp._service_context 属性及其 llm/embedding_model/vector_store 属性。
// 具体 Op 嵌入此结构体以复用服务访问逻辑，避免每个 Op 重复写字段和方法。
//
// Python: openjiuwen/extensions/context_evolver/core/op/base_op.py
type OpBase struct {
	// sc 服务上下文引用，构造时注入
	sc *cecontext.ServiceContext
}
```

在导出函数区块新增：

```go
// NewOpBase 创建操作基类。
func NewOpBase(sc *cecontext.ServiceContext) *OpBase {
	return &OpBase{sc: sc}
}
```

在导出方法区块新增：

```go
// LLM 返回 LLM 服务。未注册时返回 nil。
// 对齐 Python BaseOp.llm 属性。
func (b *OpBase) LLM() cecontext.LLMService {
	if b.sc == nil {
		return nil
	}
	return b.sc.LLM()
}

// EmbeddingModel 返回 Embedding 模型服务。未注册时返回 nil。
// 对齐 Python BaseOp.embedding_model 属性。
func (b *OpBase) EmbeddingModel() cecontext.EmbeddingService {
	if b.sc == nil {
		return nil
	}
	return b.sc.EmbeddingModel()
}

// VectorStore 返回向量存储服务。未注册时返回 nil。
// 对齐 Python BaseOp.vector_store 属性。
func (b *OpBase) VectorStore() cecontext.VectorStoreService {
	if b.sc == nil {
		return nil
	}
	return b.sc.VectorStore()
}
```

### 2.2 base_op_test.go 完整内容

```go
package op

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// fakeLLMService LLMService 的 mock 实现
type fakeLLMService struct {
	response string
	err      error
}

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	return f.response, f.err
}

// fakeEmbeddingService EmbeddingService 的 mock 实现
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return f.embeddings, f.err
}

// mockVectorStore VectorStoreService 的 mock 实现
type mockVectorStore struct {
	nodes map[string]*schema.VectorNode
}

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{nodes: make(map[string]*schema.VectorNode)}
}

func (m *mockVectorStore) Upsert(_ context.Context, node *schema.VectorNode) error {
	m.nodes[node.ID] = node
	return nil
}

func (m *mockVectorStore) Search(_ context.Context, _ []float64, _ int, _ map[string]any) ([]*schema.VectorNode, error) {
	return nil, nil
}

func (m *mockVectorStore) Delete(_ context.Context, nodeID string) (bool, error) {
	_, ok := m.nodes[nodeID]
	if ok {
		delete(m.nodes, nodeID)
	}
	return ok, nil
}

func (m *mockVectorStore) Clear() {
	m.nodes = make(map[string]*schema.VectorNode)
}

func (m *mockVectorStore) Count() int {
	return len(m.nodes)
}

func (m *mockVectorStore) GetAll(_ map[string]any) []*schema.VectorNode {
	var result []*schema.VectorNode
	for _, node := range m.nodes {
		result = append(result, node)
	}
	return result
}

func TestNewOpBase(t *testing.T) {
	sc := cecontext.NewServiceContext()
	ob := NewOpBase(sc)
	require.NotNil(t, ob)
}

func TestOpBase_LLM_未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	ob := NewOpBase(sc)
	assert.Nil(t, ob.LLM())
}

func TestOpBase_LLM_已注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "test"}
	sc.RegisterService("llm", llm)
	ob := NewOpBase(sc)
	result := ob.LLM()
	require.NotNil(t, result)
	resp, err := result.Generate(context.Background(), "hi")
	assert.NoError(t, err)
	assert.Equal(t, "test", resp)
}

func TestOpBase_LLM_类型不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", "not-a-llm")
	ob := NewOpBase(sc)
	assert.Nil(t, ob.LLM())
}

func TestOpBase_EmbeddingModel_未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	ob := NewOpBase(sc)
	assert.Nil(t, ob.EmbeddingModel())
}

func TestOpBase_EmbeddingModel_已注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{embeddings: [][]float64{{0.1, 0.2}}}
	sc.RegisterService("embedding_model", emb)
	ob := NewOpBase(sc)
	result := ob.EmbeddingModel()
	require.NotNil(t, result)
	vec, err := result.Embed(context.Background(), "hi")
	assert.NoError(t, err)
	assert.Equal(t, []float64{0.1, 0.2}, vec)
}

func TestOpBase_EmbeddingModel_类型不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("embedding_model", "not-an-embedding")
	ob := NewOpBase(sc)
	assert.Nil(t, ob.EmbeddingModel())
}

func TestOpBase_VectorStore_未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	ob := NewOpBase(sc)
	assert.Nil(t, ob.VectorStore())
}

func TestOpBase_VectorStore_已注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := newMockVectorStore()
	sc.RegisterService("vector_store", vs)
	ob := NewOpBase(sc)
	result := ob.VectorStore()
	require.NotNil(t, result)
	assert.Equal(t, 0, result.Count())
}

func TestOpBase_VectorStore_类型不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("vector_store", "not-a-vector-store")
	ob := NewOpBase(sc)
	assert.Nil(t, ob.VectorStore())
}

func TestOpBase_空ServiceContext(t *testing.T) {
	ob := NewOpBase(nil)
	assert.Nil(t, ob.LLM())
	assert.Nil(t, ob.EmbeddingModel())
	assert.Nil(t, ob.VectorStore())
}
```

### 2.3 doc.go 更新

```go
// 文件目录：
//
//	op/
//	├── doc.go              # 包文档
//	├── base_op.go          # BaseOp 接口 + OpBase 结构体 + Seq/Par 工厂函数
//	├── sequential_op.go    # SequentialOp 顺序组合
//	└── parallel_op.go      # ParallelOp 并行组合
```

### 2.4 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/core/op/... -v -count=1
go test ./internal/agentcore/context_evolver/core/op/... -cover
```

---

## Task 3: summary/reme/utils.go + 测试

**目标：** 创建 `summary/reme/utils.go`，实现 ParseJSONExperienceResponse + CalculateCosineSimilarity + isValidExperience，以及对应测试文件。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/utils.go` — 新增
- `internal/agentcore/context_evolver/summary/reme/utils_test.go` — 新增

### 3.1 utils.go 完整内容

```go
package reme

import (
	"encoding/json"
	"math"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseJSONExperienceResponse 解析 LLM 返回的 JSON 格式经验响应。
// 对齐 Python parse_json_experience_response(response)。
// 从 markdown 代码块中提取 JSON，支持数组和单对象格式，
// 过滤无效的经验条目（必须同时含 experience 和 when_to_use/condition）。
func ParseJSONExperienceResponse(response string) []map[string]any {
	jsonPattern := regexp.MustCompile("```json\\s*([\\s\\S]*?)\\s*```")
	jsonBlocks := jsonPattern.FindAllStringSubmatch(response, -1)

	if len(jsonBlocks) > 0 {
		var parsed any
		if err := json.Unmarshal([]byte(jsonBlocks[0][1]), &parsed); err == nil {
			switch v := parsed.(type) {
			case []any:
				var experiences []map[string]any
				for _, item := range v {
					if data, ok := item.(map[string]any); ok && isValidExperience(data) {
						experiences = append(experiences, data)
					}
				}
				return experiences
			case map[string]any:
				if isValidExperience(v) {
					return []map[string]any{v}
				}
			}
		}
	}

	// 回退：尝试解析整个响应
	var parsed any
	if err := json.Unmarshal([]byte(response), &parsed); err == nil {
		switch v := parsed.(type) {
		case []any:
			return toMapSlice(v)
		case map[string]any:
			return []map[string]any{v}
		}
	}

	logger.Warn(logComponent).Str("response_preview", truncate(response, 100)).Msg("解析 JSON 经验响应失败")
	return nil
}

// CalculateCosineSimilarity 计算两个向量的余弦相似度。
// 对齐 Python calculate_cosine_similarity(embedding1, embedding2)。
// 零范数向量返回 0.0（避免除零）。
func CalculateCosineSimilarity(a, b []float64) float64 {
	dot := 0.0
	normA := 0.0
	normB := 0.0
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (normA * normB)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isValidExperience 检查数据是否包含必需的经验字段。
// 对齐 Python _is_valid_experience(data)。
// 必须同时含 experience 和 when_to_use 或 condition。
func isValidExperience(data map[string]any) bool {
	_, hasExperience := data["experience"]
	_, hasWhenToUse := data["when_to_use"]
	_, hasCondition := data["condition"]
	return hasExperience && (hasWhenToUse || hasCondition)
}

// toMapSlice 将 []any 转换为 []map[string]any。
func toMapSlice(items []any) []map[string]any {
	var result []map[string]any
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// truncate 截断字符串到指定长度。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

### 3.2 utils_test.go 完整内容

```go
package reme

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseJSONExperienceResponse_数组格式(t *testing.T) {
	response := "```json\n[{\"when_to_use\": \"test\", \"experience\": \"exp1\"}]\n```"
	result := ParseJSONExperienceResponse(response)
	assert.Len(t, result, 1)
	assert.Equal(t, "exp1", result[0]["experience"])
	assert.Equal(t, "test", result[0]["when_to_use"])
}

func TestParseJSONExperienceResponse_单对象格式(t *testing.T) {
	response := "```json\n{\"when_to_use\": \"test\", \"experience\": \"exp1\"}\n```"
	result := ParseJSONExperienceResponse(response)
	assert.Len(t, result, 1)
	assert.Equal(t, "exp1", result[0]["experience"])
}

func TestParseJSONExperienceResponse_缺少必要字段(t *testing.T) {
	response := "```json\n[{\"tags\": [\"test\"]}]\n```"
	result := ParseJSONExperienceResponse(response)
	assert.Len(t, result, 0)
}

func TestParseJSONExperienceResponse_condition替代when_to_use(t *testing.T) {
	response := "```json\n{\"condition\": \"test\", \"experience\": \"exp1\"}\n```"
	result := ParseJSONExperienceResponse(response)
	assert.Len(t, result, 1)
}

func TestParseJSONExperienceResponse_混合有效无效(t *testing.T) {
	response := "```json\n[{\"when_to_use\": \"ok\", \"experience\": \"good\"}, {\"bad\": true}]\n```"
	result := ParseJSONExperienceResponse(response)
	assert.Len(t, result, 1)
}

func TestParseJSONExperienceResponse_无JSON代码块_回退解析(t *testing.T) {
	response := `[{"when_to_use": "test", "experience": "exp1"}]`
	result := ParseJSONExperienceResponse(response)
	assert.Len(t, result, 1)
}

func TestParseJSONExperienceResponse_畸形JSON(t *testing.T) {
	response := "```json\n{broken json}\n```"
	result := ParseJSONExperienceResponse(response)
	assert.Nil(t, result)
}

func TestParseJSONExperienceResponse_空响应(t *testing.T) {
	result := ParseJSONExperienceResponse("")
	assert.Nil(t, result)
}

func TestCalculateCosineSimilarity_相同向量(t *testing.T) {
	a := []float64{1.0, 0.0, 0.0}
	b := []float64{1.0, 0.0, 0.0}
	sim := CalculateCosineSimilarity(a, b)
	assert.InDelta(t, 1.0, sim, 0.001)
}

func TestCalculateCosineSimilarity_正交向量(t *testing.T) {
	a := []float64{1.0, 0.0}
	b := []float64{0.0, 1.0}
	sim := CalculateCosineSimilarity(a, b)
	assert.InDelta(t, 0.0, sim, 0.001)
}

func TestCalculateCosineSimilarity_零向量(t *testing.T) {
	a := []float64{0.0, 0.0}
	b := []float64{1.0, 1.0}
	sim := CalculateCosineSimilarity(a, b)
	assert.InDelta(t, 0.0, sim, 0.001)
}

func TestCalculateCosineSimilarity_不同长度(t *testing.T) {
	a := []float64{1.0, 2.0, 3.0}
	b := []float64{1.0, 2.0}
	sim := CalculateCosineSimilarity(a, b)
	// 只比较前 2 个维度
	expected := (1.0*1.0 + 2.0*2.0) / (math.Sqrt(1+4+9) * math.Sqrt(1+4))
	assert.InDelta(t, expected, sim, 0.001)
}

func TestCalculateCosineSimilarity_一般情况(t *testing.T) {
	a := []float64{1.0, 2.0, 3.0}
	b := []float64{4.0, 5.0, 6.0}
	sim := CalculateCosineSimilarity(a, b)
	expected := (4.0 + 10.0 + 18.0) / (math.Sqrt(14) * math.Sqrt(77))
	assert.InDelta(t, expected, sim, 0.001)
}

func TestIsValidExperience_有效when_to_use(t *testing.T) {
	data := map[string]any{"experience": "exp", "when_to_use": "when"}
	assert.True(t, isValidExperience(data))
}

func TestIsValidExperience_有效condition(t *testing.T) {
	data := map[string]any{"experience": "exp", "condition": "cond"}
	assert.True(t, isValidExperience(data))
}

func TestIsValidExperience_缺少experience(t *testing.T) {
	data := map[string]any{"when_to_use": "when"}
	assert.False(t, isValidExperience(data))
}

func TestIsValidExperience_缺少触发条件(t *testing.T) {
	data := map[string]any{"experience": "exp"}
	assert.False(t, isValidExperience(data))
}
```

### 3.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestParseJSONExperienceResponse
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestCalculateCosineSimilarity
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestIsValidExperience
```

---

## Task 4: summary/reme/prompt.go + 测试

**目标：** 创建 `summary/reme/prompt.go`，一比一复刻 Python 的 5 个提示词常量 + ReMeSummaryPrompts 结构体，以及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/prompt.go` — 新增
- `internal/agentcore/context_evolver/summary/reme/prompt_test.go` — 新增

### 4.1 prompt.go 完整内容

注意：Python 的 f-string 使用 `{{` / `}}` 转义大括号，Go 反引号字符串中不需要转义，直接写 `{higher_score}` 等。

```go
package reme

// ──────────────────────────── 常量 ────────────────────────────

// comparativeMemoryPrompt 对比高分/低分轨迹提取性能洞察。
// 一比一复刻 Python COMPARATIVE_MEMORY_PROMPT。
// 占位符: {higher_score}, {higher_steps}, {lower_score}, {lower_steps}
const comparativeMemoryPrompt = `You are an expert AI analyst comparing higher-scoring and lower-scoring step sequences to extract performance insights.

Your task is to identify the key differences between higher and lower performing approaches at the step level.
Focus on what made the higher-scoring approach more effective, even when both approaches may have had partial success.

SOFT COMPARATIVE ANALYSIS FRAMEWORK:
● PERFORMANCE FACTORS: Identify what specifically contributed to the higher score
● APPROACH DIFFERENCES: Compare methodologies and execution strategies
● EFFICIENCY ANALYSIS: Analyze why one approach was more efficient or effective
● OPTIMIZATION INSIGHTS: Extract lessons for improving performance

EXTRACTION PRINCIPLES:
● Focus on INCREMENTAL IMPROVEMENTS and performance optimization
● Extract QUALITY INDICATORS that differentiate better vs good approaches
● Identify REFINEMENT STRATEGIES that lead to higher scores
● Frame insights as PERFORMANCE ENHANCEMENT guidelines

# Higher-Scoring Step Sequence (Score: {higher_score})
{higher_steps}

# Lower-Scoring Step Sequence (Score: {lower_score})
{lower_steps}


OUTPUT FORMAT:
Generate up to 5 performance improvement insights as JSON objects:
` + "```json" + `
[
{
    "when_to_use": "Specific scenarios where this performance insight applies",
    "experience": "Detailed analysis of what made the higher-scoring approach more effective",
    "tags": ["performance_optimization", "score_improvement", "relevant_keywords"],
    "confidence": 0.7,
    "step_type": "reasoning|action|observation|decision",
    "tools_used": ["list", "of", "tools"]
}
]
` + "```"

// successMemoryPrompt 从成功轨迹提取可复用经验。
// 一比一复刻 Python SUCCESS_MEMORY_PROMPT。
// 占位符: {query}, {step_sequence}, {outcome}
const successMemoryPrompt = `You are an expert AI analyst reviewing successful step sequences from an AI agent execution.

Your task is to extract reusable, actionable step-level task memories that can guide future agent executions.
Focus on identifying specific patterns, techniques, and decision points that contributed to success.

ANALYSIS FRAMEWORK:
● STEP PATTERN ANALYSIS: Identify the specific sequence of actions that led to success
● DECISION POINTS: Highlight critical decisions made during these steps
● TECHNIQUE EFFECTIVENESS: Analyze why specific approaches worked well
● REUSABILITY: Extract patterns that can be applied to similar scenarios

EXTRACTION PRINCIPLES:
● Focus on TRANSFERABLE TECHNIQUES and decision frameworks
● Frame insights as actionable guidelines and best practices

# Original Query
{query}

# Step Sequence Analysis
{step_sequence}

# Outcome
This step sequence was part of a {outcome} trajectory.

OUTPUT FORMAT:
Generate 1-3 step-level success insights as JSON objects:
` + "```json" + `
[
{
    "when_to_use": "Specific conditions when this step pattern should be applied",
    "experience": "Detailed description of the successful step pattern and why it works",
    "tags": ["relevant", "keywords", "for", "categorization"],
    "confidence": 0.8,
    "step_type": "reasoning|action|observation|decision",
    "tools_used": ["list", "of", "tools"]
}
]
` + "```"

// failureMemoryPrompt 从失败轨迹提取教训。
// 一比一复刻 Python FAILURE_MEMORY_PROMPT。
// 占位符: {query}, {step_sequence}, {outcome}
const failureMemoryPrompt = `You are an expert AI analyst reviewing failed step sequences from an AI agent execution.

Your task is to extract learning task memories from failures to prevent similar mistakes in future executions.
Focus on identifying error patterns, missed opportunities, and alternative approaches.

ANALYSIS FRAMEWORK:
● FAILURE POINT IDENTIFICATION: Pinpoint where and why the steps went wrong
● ERROR PATTERN ANALYSIS: Identify recurring mistakes or problematic approaches
● ALTERNATIVE APPROACHES: Suggest what could have been done differently
● PREVENTION STRATEGIES: Extract actionable insights to avoid similar failures

EXTRACTION PRINCIPLES:
● Extract GENERAL PRINCIPLES as well as SPECIFIC INSTRUCTIONS
● Focus on PATTERNS and RULES as well as particular instances

# Original Query
{query}

# Step Sequence Analysis
{step_sequence}

# Outcome
This step sequence was part of a {outcome} trajectory.

OUTPUT FORMAT:
Generate 1-3 step-level failure prevention insights as JSON objects:
` + "```json" + `
[
{
    "when_to_use": "Specific situations where this lesson should be remembered",
    "experience": "Universal principle or rule extracted from the failure pattern ",
    "tags": ["error_prevention", "failure_analysis", "relevant_keywords"],
    "confidence": 0.7,
    "step_type": "reasoning|action|observation|decision",
    "tools_used": ["list", "of", "tools"]
}
]
` + "```"

// comparativeAllMemoryPrompt 对比所有轨迹提取差异洞察。
// 一比一复刻 Python COMPARATIVE_ALL_MEMORY_PROMPT。
// 占位符: {trajectory}
const comparativeAllMemoryPrompt = `You are an expert AI analyst comparing multiple step sequences which might be successful or failed to extract differential insights.

Your task is to compare and contrast these trajectories to identify the most useful and generalizable strategies as memory items using self-contrast reasoning.
Focus on critical decision points, technique variations, and approach differences.

COMPARATIVE ANALYSIS FRAMEWORK:
● DECISION CONTRAST: Compare critical decisions made in success vs failure cases
● TECHNIQUE VARIATIONS: Identify different approaches and their outcomes
● TIMING DIFFERENCES: Analyze when certain actions were taken and their impact
● SUCCESS FACTORS: Extract what specifically made the difference

EXTRACTION PRINCIPLES:
● Frame comparisons as PRINCIPLES as well as case-specific SOLUTIONS
● Identify PATTERNS that differentiate effective vs ineffective approaches
● Extract RULES that can guide future similar situations
● Focus on UNDERLYING MECHANISMS rather than surface-level differences

{trajectory}
OUTPUT FORMAT:
Generate up to 5 comparative insights as JSON objects:
` + "```json" + `
[
{
    "when_to_use": "Specific scenarios where this comparative insight applies",
    "experience": "Detailed comparison highlighting why success approach works better",
    "tags": ["comparative_analysis", "success_factors", "relevant_keywords"],
    "confidence": 0.8,
    "step_type": "reasoning|action|observation|decision",
    "tools_used": ["list", "of", "tools"]
}
]
` + "```"

// memoryValidationPrompt 校验记忆质量。
// 一比一复刻 Python MEMORY_VALIDATION_PROMPT。
// 占位符: {condition}, {task_memory_content}
const memoryValidationPrompt = `You are an expert AI analyst tasked with validating the quality and usefulness of extracted step-level task memories.

Your task is to access whether the extracted task memory is actionable, accurate, and valuable for future agent executions.

VALIDATION CRITERIA:
● ACTIONABILITY: Is the task memory specific enough to guide future actions?
● ACCURACY: Does the task memory correctly reflect the patterns observed?
● RELEVANCE: Is the task memory applicable to similar future scenarios?
● CLARITY: Is the task memory clearly articulated and understandable?
● UNIQUENESS: Does the task memory provide novel insights or common knowledge?

# Task Memory to Validate
Condition: {condition}
Task Memory Content: {task_memory_content}

OUTPUT FORMAT:
Provide validation assessment:
` + "```json" + `
{
"is_valid": true/false,
"score": 0.8,
"feedback": "Detailed explanation of validation decision",
"recommendations": "Suggestions for improvement if applicable"
}

Score should be between 0.0 (poor quality) and 1.0 (excellent quality).
Mark as invalid if score is below 0.3 or if there are fundamental issues with the task memory.`

// ──────────────────────────── 结构体 ────────────────────────────

// ReMeSummaryPrompts ReMe 摘要管线的提示词集合。
// 对齐 Python ReMePrompt dataclass（summary/task/reme/prompt.py）。
type ReMeSummaryPrompts struct {
	// ComparativeMemoryPrompt 对比提取提示词
	ComparativeMemoryPrompt string
	// SuccessMemoryPrompt 成功提取提示词
	SuccessMemoryPrompt string
	// FailureMemoryPrompt 失败提取提示词
	FailureMemoryPrompt string
	// ComparativeAllMemoryPrompt 全量对比提取提示词
	ComparativeAllMemoryPrompt string
	// MemoryValidationPrompt 校验提示词
	MemoryValidationPrompt string
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ReMeSummaryDefaultPrompts 默认提示词实例。
	// 对齐 Python ReMePrompts = ReMePrompt()。
	ReMeSummaryDefaultPrompts = ReMeSummaryPrompts{
		ComparativeMemoryPrompt:    comparativeMemoryPrompt,
		SuccessMemoryPrompt:        successMemoryPrompt,
		FailureMemoryPrompt:        failureMemoryPrompt,
		ComparativeAllMemoryPrompt: comparativeAllMemoryPrompt,
		MemoryValidationPrompt:     memoryValidationPrompt,
	}
)
```

### 4.2 prompt_test.go 完整内容

```go
package reme

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComparativeMemoryPrompt_占位符(t *testing.T) {
	assert.True(t, strings.Contains(comparativeMemoryPrompt, "{higher_score}"))
	assert.True(t, strings.Contains(comparativeMemoryPrompt, "{higher_steps}"))
	assert.True(t, strings.Contains(comparativeMemoryPrompt, "{lower_score}"))
	assert.True(t, strings.Contains(comparativeMemoryPrompt, "{lower_steps}"))
}

func TestSuccessMemoryPrompt_占位符(t *testing.T) {
	assert.True(t, strings.Contains(successMemoryPrompt, "{query}"))
	assert.True(t, strings.Contains(successMemoryPrompt, "{step_sequence}"))
	assert.True(t, strings.Contains(successMemoryPrompt, "{outcome}"))
}

func TestFailureMemoryPrompt_占位符(t *testing.T) {
	assert.True(t, strings.Contains(failureMemoryPrompt, "{query}"))
	assert.True(t, strings.Contains(failureMemoryPrompt, "{step_sequence}"))
	assert.True(t, strings.Contains(failureMemoryPrompt, "{outcome}"))
}

func TestComparativeAllMemoryPrompt_占位符(t *testing.T) {
	assert.True(t, strings.Contains(comparativeAllMemoryPrompt, "{trajectory}"))
}

func TestMemoryValidationPrompt_占位符(t *testing.T) {
	assert.True(t, strings.Contains(memoryValidationPrompt, "{condition}"))
	assert.True(t, strings.Contains(memoryValidationPrompt, "{task_memory_content}"))
}

func TestReMeSummaryDefaultPrompts_字段填充(t *testing.T) {
	assert.Equal(t, comparativeMemoryPrompt, ReMeSummaryDefaultPrompts.ComparativeMemoryPrompt)
	assert.Equal(t, successMemoryPrompt, ReMeSummaryDefaultPrompts.SuccessMemoryPrompt)
	assert.Equal(t, failureMemoryPrompt, ReMeSummaryDefaultPrompts.FailureMemoryPrompt)
	assert.Equal(t, comparativeAllMemoryPrompt, ReMeSummaryDefaultPrompts.ComparativeAllMemoryPrompt)
	assert.Equal(t, memoryValidationPrompt, ReMeSummaryDefaultPrompts.MemoryValidationPrompt)
}

func TestReMeSummaryPrompts_格式化可执行(t *testing.T) {
	// 验证提示词可以用 strings.Replace 进行占位符替换
	result := strings.ReplaceAll(successMemoryPrompt, "{query}", "test query")
	result = strings.ReplaceAll(result, "{step_sequence}", "step1")
	result = strings.ReplaceAll(result, "{outcome}", "successful")
	assert.Contains(t, result, "test query")
	assert.Contains(t, result, "step1")
	assert.Contains(t, result, "successful")
	// 原始占位符不应再出现
	assert.NotContains(t, result, "{query}")
	assert.NotContains(t, result, "{step_sequence}")
	assert.NotContains(t, result, "{outcome}")
}
```

### 4.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestComparative
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestSuccessMemory
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestFailureMemory
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestMemoryValidation
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestReMeSummary
```

---

## Task 5: summary/reme/update.go — TrajectoryPreprocessOp + 测试

**目标：** 在 `summary/reme/update.go` 中实现 TrajectoryPreprocessOp 及测试。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/update.go` — 新增（逐步追加内容）
- `internal/agentcore/context_evolver/summary/reme/update_test.go` — 新增（逐步追加内容）

### 5.1 update.go — TrajectoryPreprocessOp

```go
package reme

import (
	"context"
	"fmt"
	"strings"
	"time"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识（与 utils.go 共用，仅在此处声明）
// 注意：utils.go 中已声明，此处用变量引用避免重复声明
// 实际编译时只有一个声明在 utils.go 中

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryPreprocessOp 轨迹预处理操作。
// 按 score 阈值分组为 success/failure。
// 对齐 Python TrajectoryPreprocessOp。
type TrajectoryPreprocessOp struct {
	op.OpBase
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTrajectoryPreprocessOp 创建轨迹预处理操作。
func NewTrajectoryPreprocessOp(sc *cecontext.ServiceContext) *TrajectoryPreprocessOp {
	return &TrajectoryPreprocessOp{OpBase: *op.NewOpBase(sc)}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Execute 执行轨迹预处理。
// 对齐 Python TrajectoryPreprocessOp.async_execute(context)。
// 从 rc 获取 trajectories 和 score，按 threshold 分组。
func (o *TrajectoryPreprocessOp) Execute(_ context.Context, rc *cecontext.RuntimeContext) error {
	trajectories := getTypedSlice[string](rc, "trajectories")
	if len(trajectories) == 0 {
		logger.Warn(logComponent).Msg("无轨迹可处理")
		rc.Set("success_trajectories", []string{})
		rc.Set("failure_trajectories", []string{})
		rc.Set("all_trajectories", []string{})
		return nil
	}

	scores := getTypedSlice[float64](rc, "score")
	threshold, _ := cecontext.GetTyped[float64](rc, "threshold")
	if threshold == 0 {
		threshold = 1
	}

	var successTrajectories []string
	var failureTrajectories []string

	for i, trajectory := range trajectories {
		if i < len(scores) && scores[i] >= threshold {
			successTrajectories = append(successTrajectories, trajectory)
		} else if i < len(scores) {
			failureTrajectories = append(failureTrajectories, trajectory)
		} else {
			// score 列表不够长时，归入 failure
			failureTrajectories = append(failureTrajectories, trajectory)
		}
	}

	allTrajectories := make([]string, len(trajectories))
	copy(allTrajectories, trajectories)

	rc.Set("success_trajectories", successTrajectories)
	rc.Set("failure_trajectories", failureTrajectories)
	rc.Set("all_trajectories", allTrajectories)

	logger.Info(logComponent).
		Int("total", len(trajectories)).
		Int("success", len(successTrajectories)).
		Int("failure", len(failureTrajectories)).
		Msg("轨迹预处理完成")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getTypedSlice 从 RuntimeContext 获取类型化切片。
func getTypedSlice[T any](rc *cecontext.RuntimeContext, key string) []T {
	val := rc.Get(key)
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []T:
		return v
	case []any:
		var result []T
		for _, item := range v {
			if typed, ok := item.(T); ok {
				result = append(result, typed)
			}
		}
		return result
	}
	return nil
}

// buildReMeMemory 从解析的经验数据构建 ReMeMemory。
// 对齐 Python 中各 ExtractionOp 的 memory 构建逻辑。
func buildReMeMemory(expData map[string]any, userID string) *ceschema.ReMeMemory {
	tags := getStringSlice(expData, "tags")
	toolsUsed := getStringSlice(expData, "tools_used")

	metadata := ceschema.ReMeMemoryMetadata{
		Tags:       tags,
		StepType:   getStringField(expData, "step_type"),
		ToolsUsed:  toolsUsed,
		Confidence: getFloatField(expData, "confidence"),
		Freq:       0,
		Utility:    0,
	}

	now := time.Now().UTC()
	return &ceschema.ReMeMemory{
		BaseMemory: ceschema.BaseMemory{WorkspaceID: userID},
		WhenToUse:  getStringField(expData, "when_to_use"),
		Content:    getStringField(expData, "experience"),
		Score:      1.0,
		CreatedAt:  now,
		UpdatedAt:  now,
		Metadata:   metadata,
	}
}

// getStringField 从 map 中安全获取字符串字段。
func getStringField(data map[string]any, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getFloatField 从 map 中安全获取浮点数字段。
func getFloatField(data map[string]any, key string) float64 {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}

// getStringSlice 从 map 中安全获取字符串切片。
func getStringSlice(data map[string]any, key string) []string {
	if v, ok := data[key]; ok {
		switch arr := v.(type) {
		case []string:
			return arr
		case []any:
			var result []string
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}

// formatCandidatesForRerank 格式化候选记忆用于重排序提示词。
// 对齐 Python RerankMemoryOp._format_candidates_for_rerank。
func formatCandidatesForRerank(candidates []ceschema.ReMeRetrievedMemory) string {
	var formatted []string
	for i, candidate := range candidates {
		text := fmt.Sprintf("Candidate %d:\nCondition: %s\nExperience: %s\n", i, candidate.WhenToUse, candidate.Content)
		formatted = append(formatted, text)
	}
	return strings.Join(formatted, "---\n")
}

// formatMemoriesForContext 格式化记忆用于上下文生成。
// 对齐 Python RewriteMemoryOp._format_memories_for_context。
func formatMemoriesForContext(memories []ceschema.ReMeRetrievedMemory) string {
	var formatted []string
	for i, memory := range memories {
		text := fmt.Sprintf("Memory %d:\n  When to use: %s\n  Content: %s\n", i+1, memory.WhenToUse, memory.Content)
		formatted = append(formatted, text)
	}
	return strings.Join(formatted, "\n")
}
```

### 5.2 update_test.go — TrajectoryPreprocessOp 测试

```go
package reme

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
)

func TestTrajectoryPreprocessOp_正常分组(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewTrajectoryPreprocessOp(sc)
	rc := cecontext.NewRuntimeContext()

	rc.Set("trajectories", []string{"traj1", "traj2", "traj3"})
	rc.Set("score", []float64{1.5, 0.5, 2.0})
	rc.Set("threshold", 1.0)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	success, ok := cecontext.GetTyped[[]string](rc, "success_trajectories")
	require.True(t, ok)
	assert.Equal(t, []string{"traj1", "traj3"}, success)

	failure, ok := cecontext.GetTyped[[]string](rc, "failure_trajectories")
	require.True(t, ok)
	assert.Equal(t, []string{"traj2"}, failure)

	all, ok := cecontext.GetTyped[[]string](rc, "all_trajectories")
	require.True(t, ok)
	assert.Len(t, all, 3)
}

func TestTrajectoryPreprocessOp_默认阈值(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewTrajectoryPreprocessOp(sc)
	rc := cecontext.NewRuntimeContext()

	rc.Set("trajectories", []string{"traj1", "traj2"})
	rc.Set("score", []float64{1.0, 0.5})
	// 不设 threshold，默认 1

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	success, ok := cecontext.GetTyped[[]string](rc, "success_trajectories")
	require.True(t, ok)
	assert.Equal(t, []string{"traj1"}, success)

	failure, ok := cecontext.GetTyped[[]string](rc, "failure_trajectories")
	require.True(t, ok)
	assert.Equal(t, []string{"traj2"}, failure)
}

func TestTrajectoryPreprocessOp_空轨迹(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewTrajectoryPreprocessOp(sc)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	success, ok := cecontext.GetTyped[[]string](rc, "success_trajectories")
	require.True(t, ok)
	assert.Empty(t, success)

	failure, ok := cecontext.GetTyped[[]string](rc, "failure_trajectories")
	require.True(t, ok)
	assert.Empty(t, failure)
}

func TestTrajectoryPreprocessOp_score列表不足(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewTrajectoryPreprocessOp(sc)
	rc := cecontext.NewRuntimeContext()

	rc.Set("trajectories", []string{"traj1", "traj2", "traj3"})
	rc.Set("score", []float64{1.5}) // 只有 1 个 score

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	success, ok := cecontext.GetTyped[[]string](rc, "success_trajectories")
	require.True(t, ok)
	assert.Equal(t, []string{"traj1"}, success)

	failure, ok := cecontext.GetTyped[[]string](rc, "failure_trajectories")
	require.True(t, ok)
	assert.Len(t, failure, 2)
}
```

### 5.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestTrajectoryPreprocessOp
```

---

## Task 6: summary/reme/update.go — SuccessExtractionOp + FailureExtractionOp + 测试

**目标：** 在 `summary/reme/update.go` 中追加 SuccessExtractionOp 和 FailureExtractionOp，及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/update.go` — 追加
- `internal/agentcore/context_evolver/summary/reme/update_test.go` — 追加

### 6.1 update.go 追加内容

在结构体区块追加：

```go
// SuccessExtractionOp 从成功轨迹提取经验。
// 对齐 Python SuccessExtractionOp。
type SuccessExtractionOp struct {
	op.OpBase
	useExtraction bool
	prompts       ReMeSummaryPrompts
}

// FailureExtractionOp 从失败轨迹提取教训。
// 对齐 Python FailureExtractionOp。
type FailureExtractionOp struct {
	op.OpBase
	useExtraction bool
	prompts       ReMeSummaryPrompts
}
```

在导出函数区块追加：

```go
// NewSuccessExtractionOp 创建成功提取操作。
func NewSuccessExtractionOp(sc *cecontext.ServiceContext, useExtraction bool) *SuccessExtractionOp {
	return &SuccessExtractionOp{
		OpBase:        *op.NewOpBase(sc),
		useExtraction: useExtraction,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}

// NewFailureExtractionOp 创建失败提取操作。
func NewFailureExtractionOp(sc *cecontext.ServiceContext, useExtraction bool) *FailureExtractionOp {
	return &FailureExtractionOp{
		OpBase:        *op.NewOpBase(sc),
		useExtraction: useExtraction,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}
```

在导出方法区块追加：

```go
// Execute 执行成功轨迹经验提取。
// 对齐 Python SuccessExtractionOp.async_execute(context)。
func (o *SuccessExtractionOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !o.useExtraction {
		rc.Set("success_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	successTrajectories := getTypedSlice[string](rc, "success_trajectories")
	if len(successTrajectories) == 0 {
		logger.Info(logComponent).Msg("无成功轨迹可提取")
		rc.Set("success_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}
	query, _ := cecontext.GetTyped[string](rc, "query")

	logger.Debug(logComponent).Int("count", len(successTrajectories)).Msg("从成功轨迹提取经验")

	var memories []*ceschema.ReMeMemory
	for _, trajectory := range successTrajectories {
		prompt := strings.ReplaceAll(o.prompts.SuccessMemoryPrompt, "{query}", query)
		prompt = strings.ReplaceAll(prompt, "{step_sequence}", trajectory)
		prompt = strings.ReplaceAll(prompt, "{outcome}", "successful")

		response, err := llm.Generate(ctx, prompt)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("LLM 生成失败")
			continue
		}

		experiences := ParseJSONExperienceResponse(response)
		for _, expData := range experiences {
			memory := buildReMeMemory(expData, userID)
			if memory.WhenToUse == "" || memory.Content == "" {
				logger.Warn(logComponent).Msg("构建 ReMeMemory 失败：缺少必要字段")
				continue
			}
			memories = append(memories, memory)
		}
	}

	rc.Set("success_memories", memories)
	return nil
}

// Execute 执行失败轨迹教训提取。
// 对齐 Python FailureExtractionOp.async_execute(context)。
func (o *FailureExtractionOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !o.useExtraction {
		rc.Set("failure_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	failureTrajectories := getTypedSlice[string](rc, "failure_trajectories")
	if len(failureTrajectories) == 0 {
		logger.Info(logComponent).Msg("无失败轨迹可提取")
		rc.Set("failure_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}
	query, _ := cecontext.GetTyped[string](rc, "query")

	logger.Debug(logComponent).Int("count", len(failureTrajectories)).Msg("从失败轨迹提取教训")

	var memories []*ceschema.ReMeMemory
	for _, trajectory := range failureTrajectories {
		prompt := strings.ReplaceAll(o.prompts.FailureMemoryPrompt, "{query}", query)
		prompt = strings.ReplaceAll(prompt, "{step_sequence}", trajectory)
		prompt = strings.ReplaceAll(prompt, "{outcome}", "failed")

		response, err := llm.Generate(ctx, prompt)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("LLM 生成失败")
			continue
		}

		experiences := ParseJSONExperienceResponse(response)
		for _, expData := range experiences {
			memory := buildReMeMemory(expData, userID)
			if memory.WhenToUse == "" || memory.Content == "" {
				logger.Warn(logComponent).Msg("构建 ReMeMemory 失败：缺少必要字段")
				continue
			}
			memories = append(memories, memory)
		}
	}

	rc.Set("failure_memories", memories)
	return nil
}
```

### 6.2 update_test.go 追加测试

```go
// fakeLLMService LLMService 的 mock 实现（summary/reme 测试用）
type fakeLLMService struct {
	response string
	err      error
}

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	return f.response, f.err
}

func TestSuccessExtractionOp_正常提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n[{\"when_to_use\": \"when test\", \"experience\": \"exp1\", \"tags\": [\"test\"], \"confidence\": 0.8}]\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewSuccessExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_trajectories", []string{"step1; step2"})
	rc.Set("user_id", "user1")
	rc.Set("query", "test query")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "success_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
	assert.Equal(t, "when test", memories[0].WhenToUse)
	assert.Equal(t, "exp1", memories[0].Content)
	assert.Equal(t, "user1", memories[0].WorkspaceID)
}

func TestSuccessExtractionOp_跳过提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSuccessExtractionOp(sc, false)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "success_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

func TestSuccessExtractionOp_无成功轨迹(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSuccessExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "success_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

func TestSuccessExtractionOp_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSuccessExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_trajectories", []string{"traj1"})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

func TestFailureExtractionOp_正常提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n[{\"when_to_use\": \"avoid this\", \"experience\": \"lesson1\", \"tags\": [\"error_prevention\"]}]\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewFailureExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("failure_trajectories", []string{"failed step"})
	rc.Set("user_id", "user1")
	rc.Set("query", "test query")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "failure_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
	assert.Equal(t, "avoid this", memories[0].WhenToUse)
}

func TestFailureExtractionOp_跳过提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewFailureExtractionOp(sc, false)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "failure_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

func TestFailureExtractionOp_LLM返回无效JSON(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "not json at all"}
	sc.RegisterService("llm", llm)

	op := NewFailureExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("failure_trajectories", []string{"failed step"})
	rc.Set("user_id", "user1")
	rc.Set("query", "test query")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "failure_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}
```

### 6.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run "TestSuccessExtractionOp|TestFailureExtractionOp"
```

---

## Task 7: summary/reme/update.go — ComparativeExtractionOp + ComparativeAllExtractionOp + 测试

**目标：** 在 `summary/reme/update.go` 中追加 ComparativeExtractionOp 和 ComparativeAllExtractionOp，及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/update.go` — 追加
- `internal/agentcore/context_evolver/summary/reme/update_test.go` — 追加

### 7.1 update.go 追加内容

在结构体区块追加：

```go
// ComparativeExtractionOp 对比最高/最低分轨迹提取差异洞察。
// 对齐 Python ComparativeExtractionOp。
type ComparativeExtractionOp struct {
	op.OpBase
	useExtraction bool
	prompts       ReMeSummaryPrompts
}

// ComparativeAllExtractionOp 对比所有轨迹（自对比）提取差异洞察。
// 对齐 Python ComparativeAllExtractionOp。
type ComparativeAllExtractionOp struct {
	op.OpBase
	useExtraction bool
	prompts       ReMeSummaryPrompts
}
```

在导出函数区块追加：

```go
// NewComparativeExtractionOp 创建对比提取操作。
func NewComparativeExtractionOp(sc *cecontext.ServiceContext, useExtraction bool) *ComparativeExtractionOp {
	return &ComparativeExtractionOp{
		OpBase:        *op.NewOpBase(sc),
		useExtraction: useExtraction,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}

// NewComparativeAllExtractionOp 创建全量对比提取操作。
func NewComparativeAllExtractionOp(sc *cecontext.ServiceContext, useExtraction bool) *ComparativeAllExtractionOp {
	return &ComparativeAllExtractionOp{
		OpBase:        *op.NewOpBase(sc),
		useExtraction: useExtraction,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}
```

在导出方法区块追加：

```go
// Execute 执行对比提取（最高分 vs 最低分）。
// 对齐 Python ComparativeExtractionOp.async_execute(context)。
func (o *ComparativeExtractionOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !o.useExtraction {
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	allTrajectories := getTypedSlice[string](rc, "all_trajectories")
	if len(allTrajectories) < 2 {
		logger.Info(logComponent).Msg("轨迹不足 2 条，跳过对比提取")
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	scores := getTypedSlice[float64](rc, "score")
	if len(scores) == 0 {
		logger.Info(logComponent).Msg("无分数信息，跳过对比提取")
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	maxScore := scores[0]
	minScore := scores[0]
	maxIdx := 0
	minIdx := 0
	for i, s := range scores {
		if s > maxScore {
			maxScore = s
			maxIdx = i
		}
		if s < minScore {
			minScore = s
			minIdx = i
		}
	}

	if maxScore == minScore {
		logger.Info(logComponent).Msg("最高分与最低分相同，跳过对比提取")
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	highTrajectory := ""
	lowTrajectory := ""
	if maxIdx < len(allTrajectories) {
		highTrajectory = allTrajectories[maxIdx]
	}
	if minIdx < len(allTrajectories) {
		lowTrajectory = allTrajectories[minIdx]
	}

	prompt := strings.ReplaceAll(o.prompts.ComparativeMemoryPrompt, "{higher_score}", fmt.Sprintf("%v", maxScore))
	prompt = strings.ReplaceAll(prompt, "{higher_steps}", highTrajectory)
	prompt = strings.ReplaceAll(prompt, "{lower_score}", fmt.Sprintf("%v", minScore))
	prompt = strings.ReplaceAll(prompt, "{lower_steps}", lowTrajectory)

	logger.Debug(logComponent).Float64("higher_score", maxScore).Float64("lower_score", minScore).Msg("对比轨迹")

	response, err := llm.Generate(ctx, prompt)
	if err != nil {
		return fmt.Errorf("LLM 生成失败: %w", err)
	}

	experiences := ParseJSONExperienceResponse(response)
	var memories []*ceschema.ReMeMemory
	for _, expData := range experiences {
		memory := buildReMeMemory(expData, userID)
		if memory.WhenToUse == "" || memory.Content == "" {
			logger.Warn(logComponent).Msg("构建 ReMeMemory 失败：缺少必要字段")
			continue
		}
		memories = append(memories, memory)
	}

	rc.Set("comparative_memories", memories)
	return nil
}

// Execute 执行全量对比提取。
// 对齐 Python ComparativeAllExtractionOp.async_execute(context)。
func (o *ComparativeAllExtractionOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !o.useExtraction {
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	allTrajectories := getTypedSlice[string](rc, "all_trajectories")
	if len(allTrajectories) < 2 {
		logger.Info(logComponent).Msg("轨迹不足 2 条，跳过全量对比提取")
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	// 构建轨迹文本
	var trajParts []string
	for i, t := range allTrajectories {
		trajParts = append(trajParts, fmt.Sprintf("# Trajectory %d\n%s", i+1, t))
	}
	trajectory := strings.Join(trajParts, "\n\n")

	prompt := strings.ReplaceAll(o.prompts.ComparativeAllMemoryPrompt, "{trajectory}", trajectory)

	response, err := llm.Generate(ctx, prompt)
	if err != nil {
		return fmt.Errorf("LLM 生成失败: %w", err)
	}

	experiences := ParseJSONExperienceResponse(response)
	var memories []*ceschema.ReMeMemory
	for _, expData := range experiences {
		memory := buildReMeMemory(expData, userID)
		if memory.WhenToUse == "" || memory.Content == "" {
			logger.Warn(logComponent).Msg("构建 ReMeMemory 失败：缺少必要字段")
			continue
		}
		memories = append(memories, memory)
	}

	rc.Set("comparative_memories", memories)
	return nil
}
```

### 7.2 update_test.go 追加测试

```go
func TestComparativeExtractionOp_正常提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n[{\"when_to_use\": \"comparative when\", \"experience\": \"comp exp\"}]\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewComparativeExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"high traj", "low traj"})
	rc.Set("score", []float64{2.0, 0.5})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
}

func TestComparativeExtractionOp_轨迹不足(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"only one"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

func TestComparativeExtractionOp_分数相同(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"traj1", "traj2"})
	rc.Set("score", []float64{1.0, 1.0})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

func TestComparativeExtractionOp_跳过提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeExtractionOp(sc, false)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

func TestComparativeAllExtractionOp_正常提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n[{\"when_to_use\": \"all comp when\", \"experience\": \"all comp exp\"}]\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewComparativeAllExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"traj1", "traj2", "traj3"})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
}

func TestComparativeAllExtractionOp_轨迹不足(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeAllExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"only one"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}
```

### 7.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run "TestComparative"
```

---

## Task 8: summary/reme/update.go — MemoryValidationOp + 测试

**目标：** 在 `summary/reme/update.go` 中追加 MemoryValidationOp 及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/update.go` — 追加
- `internal/agentcore/context_evolver/summary/reme/update_test.go` — 追加

### 8.1 update.go 追加内容

在结构体区块追加：

```go
// MemoryValidationOp 校验记忆质量。
// 对齐 Python MemoryValidationOp。
type MemoryValidationOp struct {
	op.OpBase
	useValidation bool
	prompts       ReMeSummaryPrompts
}
```

在导出函数区块追加：

```go
// NewMemoryValidationOp 创建记忆校验操作。
func NewMemoryValidationOp(sc *cecontext.ServiceContext, useValidation bool) *MemoryValidationOp {
	return &MemoryValidationOp{
		OpBase:        *op.NewOpBase(sc),
		useValidation: useValidation,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}
```

在导出方法区块追加：

```go
// Execute 执行记忆校验。
// 对齐 Python MemoryValidationOp.async_execute(context)。
// 合并三组 memories，逐条调用 LLM 校验，阈值 0.5。
func (o *MemoryValidationOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	var allMemories []*ceschema.ReMeMemory
	allMemories = append(allMemories, getReMeMemories(rc, "success_memories")...)
	allMemories = append(allMemories, getReMeMemories(rc, "failure_memories")...)
	allMemories = append(allMemories, getReMeMemories(rc, "comparative_memories")...)

	if len(allMemories) == 0 {
		logger.Info(logComponent).Msg("无记忆可校验")
		rc.Set("validated_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	if !o.useValidation {
		rc.Set("validated_memories", allMemories)
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	var validated []*ceschema.ReMeMemory
	for _, memory := range allMemories {
		isValid, score, reason := o.validateMemory(ctx, llm, memory)
		if isValid {
			memory.Score = score
			validated = append(validated, memory)
		} else {
			logger.Warn(logComponent).Str("reason", reason).Msg("记忆校验失败")
		}
	}

	rc.Set("validated_memories", validated)
	return nil
}
```

在非导出方法区块追加（注意 Go 不允许同名方法，这里用函数形式）：

```go
// validateMemory 校验单条记忆。
// 返回是否有效、分数、原因。
func (o *MemoryValidationOp) validateMemory(ctx context.Context, llm cecontext.LLMService, memory *ceschema.ReMeMemory) (bool, float64, string) {
	prompt := strings.ReplaceAll(o.prompts.MemoryValidationPrompt, "{condition}", memory.WhenToUse)
	prompt = strings.ReplaceAll(prompt, "{task_memory_content}", memory.Content)

	response, err := llm.Generate(ctx, prompt)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("LLM 校验失败")
		return false, 0.0, fmt.Sprintf("LLM 校验错误: %s", err.Error())
	}

	// 解析校验结果
	result := ParseJSONField(response, "is_valid")
	isValid := result == "true"

	scoreStr := ParseJSONField(response, "score")
	score := 0.5
	if s, err := strconv.ParseFloat(scoreStr, 64); err == nil {
		score = s
	}

	const validationThreshold = 0.5
	if isValid && score >= validationThreshold {
		return true, score, ""
	}
	return false, score, fmt.Sprintf("校验分数 %.2f 低于阈值 %.2f 或标记无效", score, validationThreshold)
}
```

注意：需要在 import 中追加 `"strconv"` 和 `"strings"`（已有 strings）。

### 8.2 update_test.go 追加测试

```go
func TestMemoryValidationOp_正常校验通过(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n{\"is_valid\": true, \"score\": 0.9, \"feedback\": \"good\"}\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewMemoryValidationOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "test", Content: "good memory", Score: 1.0},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
	assert.InDelta(t, 0.9, memories[0].Score, 0.01)
}

func TestMemoryValidationOp_校验不通过(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n{\"is_valid\": false, \"score\": 0.2, \"feedback\": \"poor\"}\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewMemoryValidationOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "test", Content: "bad memory"},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

func TestMemoryValidationOp_跳过校验(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryValidationOp(sc, false)
	rc := cecontext.NewRuntimeContext()

	input := []*ceschema.ReMeMemory{{WhenToUse: "test", Content: "mem"}}
	rc.Set("success_memories", input)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
}

func TestMemoryValidationOp_无记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryValidationOp(sc, true)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}
```

同时需要在 update_test.go 中添加辅助函数（如果没有在其他地方定义）：

```go
// getReMeMemories 从 RuntimeContext 获取 ReMeMemory 列表。
func getReMeMemoriesFromRC(rc *cecontext.RuntimeContext, key string) []*ceschema.ReMeMemory {
	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, key)
	if !ok {
		return nil
	}
	return memories
}
```

注意：由于 `getReMeMemories` 是在 update.go 中定义的非导出函数，测试文件可以直接访问。需要确保 update.go 中有此辅助函数：

```go
// getReMeMemories 从 RuntimeContext 获取 ReMeMemory 列表。
func getReMeMemories(rc *cecontext.RuntimeContext, key string) []*ceschema.ReMeMemory {
	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, key)
	if !ok {
		return nil
	}
	return memories
}
```

### 8.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestMemoryValidationOp
```

---

## Task 9: summary/reme/update.go — MemoryDeduplicationOp + 测试

**目标：** 在 `summary/reme/update.go` 中追加 MemoryDeduplicationOp 及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/update.go` — 追加
- `internal/agentcore/context_evolver/summary/reme/update_test.go` — 追加

### 9.1 update.go 追加内容

在结构体区块追加：

```go
// MemoryDeduplicationOp 基于 embedding 余弦相似度去重。
// 对齐 Python MemoryDeduplicationOp。
type MemoryDeduplicationOp struct {
	op.OpBase
	useDeduplication    bool
	similarityThreshold float64
}
```

在导出函数区块追加：

```go
// NewMemoryDeduplicationOp 创建记忆去重操作。
func NewMemoryDeduplicationOp(sc *cecontext.ServiceContext, useDeduplication bool, similarityThreshold float64) *MemoryDeduplicationOp {
	return &MemoryDeduplicationOp{
		OpBase:              *op.NewOpBase(sc),
		useDeduplication:    useDeduplication,
		similarityThreshold: similarityThreshold,
	}
}
```

在导出方法区块追加：

```go
// Execute 执行记忆去重。
// 对齐 Python MemoryDeduplicationOp.async_execute(context)。
// 从 VectorStore 获取已有记忆 embedding，与当前批次比对去重。
func (o *MemoryDeduplicationOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	memories := getReMeMemories(rc, "validated_memories")
	if len(memories) == 0 {
		logger.Info(logComponent).Msg("无记忆可去重")
		rc.Set("deduplicated_memories", []*ceschema.ReMeMemory{})
		rc.Set("duplicate_count", 0)
		return nil
	}

	if !o.useDeduplication {
		rc.Set("deduplicated_memories", memories)
		rc.Set("duplicate_count", 0)
		return nil
	}

	embModel := o.EmbeddingModel()
	if embModel == nil {
		return fmt.Errorf("EmbeddingModel not configured in ServiceContext")
	}

	vs := o.VectorStore()
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	// 获取已有记忆 embedding
	var existingEmbeddings [][]float64
	if vs != nil {
		existingNodes := vs.GetAll(map[string]any{"workspace_id": userID, "type": "reme_memory"})
		for _, node := range existingNodes {
			if node.Embedding != nil {
				existingEmbeddings = append(existingEmbeddings, node.Embedding)
			}
		}
	}

	var uniqueMemories []*ceschema.ReMeMemory
	var uniqueEmbeddings [][]float64
	duplicateCount := 0

	for _, memory := range memories {
		textForEmbedding := memory.WhenToUse + " " + memory.Content
		currentEmbedding, err := embModel.Embed(ctx, textForEmbedding)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("when_to_use", truncate(memory.WhenToUse, 50)).Msg("生成 embedding 失败")
			continue
		}

		// 与已有记忆比对
		if isSimilarToExisting(currentEmbedding, existingEmbeddings, o.similarityThreshold) {
			duplicateCount++
			logger.Debug(logComponent).Str("when_to_use", truncate(memory.WhenToUse, 30)).Msg("与已有记忆重复")
			continue
		}

		// 与当前批次已接受的记忆比对
		if isSimilarToBatch(currentEmbedding, uniqueEmbeddings, o.similarityThreshold) {
			duplicateCount++
			logger.Debug(logComponent).Str("when_to_use", truncate(memory.WhenToUse, 30)).Msg("与批次内记忆重复")
			continue
		}

		uniqueMemories = append(uniqueMemories, memory)
		uniqueEmbeddings = append(uniqueEmbeddings, currentEmbedding)
	}

	rc.Set("deduplicated_memories", uniqueMemories)
	rc.Set("duplicate_count", duplicateCount)

	logger.Info(logComponent).
		Int("total", len(memories)).
		Int("unique", len(uniqueMemories)).
		Int("removed", duplicateCount).
		Msg("记忆去重完成")

	return nil
}
```

在非导出函数区块追加：

```go
// isSimilarToExisting 检查 embedding 是否与已有记忆相似。
func isSimilarToExisting(current []float64, existing [][]float64, threshold float64) bool {
	for _, emb := range existing {
		if CalculateCosineSimilarity(current, emb) > threshold {
			return true
		}
	}
	return false
}

// isSimilarToBatch 检查 embedding 是否与当前批次中已有记忆相似。
func isSimilarToBatch(current []float64, batch [][]float64, threshold float64) bool {
	for _, emb := range batch {
		if CalculateCosineSimilarity(current, emb) > threshold {
			return true
		}
	}
	return false
}
```

### 9.2 update_test.go 追加测试

需要新增 fakeEmbeddingService：

```go
// fakeEmbeddingService EmbeddingService 的 mock 实现（summary/reme 测试用）
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
	callCount  int
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		idx := f.callCount % len(f.embeddings)
		f.callCount++
		return f.embeddings[idx], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return f.embeddings, f.err
}
```

测试函数：

```go
func TestMemoryDeduplicationOp_正常去重(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{
		embeddings: [][]float64{
			{1.0, 0.0, 0.0}, // 记忆1
			{0.99, 0.01, 0.0}, // 记忆2（与记忆1 相似）
			{0.0, 1.0, 0.0}, // 记忆3（与记忆1 不相似）
		},
	}
	sc.RegisterService("embedding_model", emb)

	op := NewMemoryDeduplicationOp(sc, true, 0.8)
	rc := cecontext.NewRuntimeContext()
	rc.Set("validated_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "when1", Content: "content1"},
		{WhenToUse: "when2", Content: "content2"},
		{WhenToUse: "when3", Content: "content3"},
	})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "deduplicated_memories")
	require.True(t, ok)
	assert.Len(t, memories, 2) // 记忆2 被去重

	count, ok := cecontext.GetTyped[int](rc, "duplicate_count")
	require.True(t, ok)
	assert.Equal(t, 1, count)
}

func TestMemoryDeduplicationOp_跳过去重(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryDeduplicationOp(sc, false, 0.8)
	rc := cecontext.NewRuntimeContext()

	input := []*ceschema.ReMeMemory{{WhenToUse: "test", Content: "mem"}}
	rc.Set("validated_memories", input)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "deduplicated_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
}

func TestMemoryDeduplicationOp_无记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryDeduplicationOp(sc, true, 0.8)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "deduplicated_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

func TestMemoryDeduplicationOp_EmbeddingModel未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryDeduplicationOp(sc, true, 0.8)
	rc := cecontext.NewRuntimeContext()
	rc.Set("validated_memories", []*ceschema.ReMeMemory{{WhenToUse: "test", Content: "mem"}})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EmbeddingModel not configured")
}
```

### 9.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run TestMemoryDeduplicationOp
```

---

## Task 10: summary/reme/update.go — UpdateVectorStoreOp + PersistMemoryOp + 测试

**目标：** 在 `summary/reme/update.go` 中追加 UpdateVectorStoreOp 和 PersistMemoryOp，及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/update.go` — 追加
- `internal/agentcore/context_evolver/summary/reme/update_test.go` — 追加

### 10.1 update.go 追加内容

在结构体区块追加：

```go
// UpdateVectorStoreOp 生成 embedding + 写入向量库。
// 对齐 Python UpdateVectorStoreOp。
type UpdateVectorStoreOp struct {
	op.OpBase
}

// PersistMemoryOp 持久化到 JSON/Milvus。
// 对齐 Python PersistMemoryOp。
type PersistMemoryOp struct {
	op.OpBase
	helper *persistence.MemoryPersistenceHelper
}
```

在导出函数区块追加：

```go
// NewUpdateVectorStoreOp 创建向量库更新操作。
func NewUpdateVectorStoreOp(sc *cecontext.ServiceContext) *UpdateVectorStoreOp {
	return &UpdateVectorStoreOp{OpBase: *op.NewOpBase(sc)}
}

// NewPersistMemoryOp 创建持久化操作。
func NewPersistMemoryOp(sc *cecontext.ServiceContext, helper *persistence.MemoryPersistenceHelper) *PersistMemoryOp {
	return &PersistMemoryOp{
		OpBase: *op.NewOpBase(sc),
		helper: helper,
	}
}
```

在导出方法区块追加：

```go
// Execute 执行向量库更新。
// 对齐 Python UpdateVectorStoreOp.async_execute(context)。
// 对每条记忆生成 embedding 并 Upsert 到向量库。
func (o *UpdateVectorStoreOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	memories := getReMeMemories(rc, "deduplicated_memories")
	if len(memories) == 0 {
		logger.Info(logComponent).Msg("无记忆可存储")
		rc.Set("stored_count", 0)
		rc.Set("memory_ids", []string{})
		rc.Set("memories", []*ceschema.ReMeMemory{})
		return nil
	}

	embModel := o.EmbeddingModel()
	if embModel == nil {
		return fmt.Errorf("EmbeddingModel not configured in ServiceContext")
	}

	vs := o.VectorStore()
	if vs == nil {
		return fmt.Errorf("VectorStore not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	logger.Info(logComponent).Int("count", len(memories)).Msg("开始存储 ReMe 记忆")

	// 转换为 VectorNode 并批量生成 embedding
	vectorNodes := make([]*coreschema.VectorNode, len(memories))
	contents := make([]string, len(memories))
	for i, memory := range memories {
		memory.WorkspaceID = userID
		node := memory.ToVectorNode()
		vectorNodes[i] = node
		contents[i] = node.Content
	}

	embeddings, err := embModel.EmbedBatch(ctx, contents)
	if err != nil {
		return fmt.Errorf("批量生成 embedding 失败: %w", err)
	}

	// 写入向量库
	var storedIDs []string
	for i, node := range vectorNodes {
		if i < len(embeddings) {
			node.Embedding = embeddings[i]
		}
		if err := vs.Upsert(ctx, node); err != nil {
			logger.Warn(logComponent).Err(err).Str("node_id", node.ID).Msg("Upsert 失败")
			continue
		}
		storedIDs = append(storedIDs, node.ID)
	}

	rc.Set("stored_count", len(storedIDs))
	rc.Set("memory_ids", storedIDs)
	rc.Set("memories", memories)

	logger.Info(logComponent).Int("count", len(storedIDs)).Msg("成功存储 ReMe 记忆")

	return nil
}

// Execute 执行持久化。
// 对齐 Python PersistMemoryOp.async_execute(context)。
// 从 VectorStore 获取所有节点，调用 helper 保存。
func (o *PersistMemoryOp) Execute(_ context.Context, rc *cecontext.RuntimeContext) error {
	vs := o.VectorStore()
	if vs == nil {
		return fmt.Errorf("VectorStore not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	allNodes := vs.GetAll(map[string]any{"workspace_id": userID, "type": "reme_memory"})
	if len(allNodes) == 0 {
		logger.Info(logComponent).Str("user_id", userID).Msg("无记忆可持久化")
		rc.Set("persist_count", 0)
		return nil
	}

	nodesDict := make(map[string]any, len(allNodes))
	for _, node := range allNodes {
		nodesDict[node.ID] = node.ToDict()
	}

	if err := o.helper.Save(userID, "reme", nodesDict); err != nil {
		return fmt.Errorf("持久化失败: %w", err)
	}

	rc.Set("persist_count", len(nodesDict))

	logger.Info(logComponent).
		Int("count", len(nodesDict)).
		Str("user_id", userID).
		Str("backend", o.helper.ResolvedType()).
		Msg("PersistMemoryOp 持久化完成")

	return nil
}
```

注意：import 需要添加 `coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"` 和 `persistence` 包。

### 10.2 update_test.go 追加测试

```go
func TestUpdateVectorStoreOp_正常存储(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{embeddings: [][]float64{{0.1, 0.2}}}
	sc.RegisterService("embedding_model", emb)

	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("deduplicated_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "when test", Content: "content test"},
	})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	count, ok := cecontext.GetTyped[int](rc, "stored_count")
	require.True(t, ok)
	assert.Equal(t, 1, count)

	ids, ok := cecontext.GetTyped[[]string](rc, "memory_ids")
	require.True(t, ok)
	assert.Len(t, ids, 1)
}

func TestUpdateVectorStoreOp_无记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	count, ok := cecontext.GetTyped[int](rc, "stored_count")
	require.True(t, ok)
	assert.Equal(t, 0, count)
}

func TestUpdateVectorStoreOp_EmbeddingModel未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("deduplicated_memories", []*ceschema.ReMeMemory{{WhenToUse: "test", Content: "mem"}})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EmbeddingModel not configured")
}

func TestPersistMemoryOp_正常持久化(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()

	// 预存一个节点
	node := coreschema.NewVectorNode("reme_user1_test", "content", []float64{0.1}, map[string]any{
		"workspace_id": "user1",
		"type":         "reme_memory",
	})
	_ = vs.Upsert(context.Background(), node)

	sc.RegisterService("vector_store", vs)

	helper := persistence.NewMemoryPersistenceHelper(persistence.WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"))
	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	count, ok := cecontext.GetTyped[int](rc, "persist_count")
	require.True(t, ok)
	assert.Equal(t, 1, count)
}

func TestPersistMemoryOp_无记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	helper := persistence.NewMemoryPersistenceHelper()
	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	count, ok := cecontext.GetTyped[int](rc, "persist_count")
	require.True(t, ok)
	assert.Equal(t, 0, count)
}

func TestPersistMemoryOp_VectorStore未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	helper := persistence.NewMemoryPersistenceHelper()
	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VectorStore not configured")
}
```

注意：测试文件 import 需要添加 `vector_store` 和 `coreschema` 包：

```go
import (
	...
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
)
```

为了避免包名冲突，使用别名：

```go
import (
	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
)
```

### 10.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/summary/reme/... -v -count=1 -run "TestUpdateVectorStoreOp|TestPersistMemoryOp"
```

---

## Task 11: summary/reme/doc.go + 更新顶层 doc.go

**目标：** 创建 `summary/reme/doc.go`，更新顶层 `context_evolver/doc.go`。

**涉及文件：**
- `internal/agentcore/context_evolver/summary/reme/doc.go` — 新增
- `internal/agentcore/context_evolver/doc.go` — 修改

### 11.1 summary/reme/doc.go

```go
// Package reme 提供 ReMe（Reflective Memory）算法的摘要管线操作。
//
// ReMe 摘要管线从任务执行轨迹中提取蒸馏后的经验知识，
// 以 ReMeMemory 形式写入向量库，并在后续任务中检索注入 Agent 上下文。
//
// 管线流程：
//
//	TrajectoryPreprocessOp → SuccessExtractionOp/FailureExtractionOp →
//	ComparativeExtractionOp/ComparativeAllExtractionOp → MemoryValidationOp →
//	MemoryDeduplicationOp → UpdateVectorStoreOp → PersistMemoryOp
//
// 文件目录：
//
//	summary/reme/
//	├── doc.go           # 包文档
//	├── update.go        # 9 个 Summarize Op
//	├── prompt.go        # 5 个提示词常量 + ReMeSummaryPrompts
//	└── utils.go         # ParseJSONExperienceResponse + CalculateCosineSimilarity + isValidExperience
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/summary/task/reme/
package reme
```

### 11.2 顶层 doc.go 更新

```go
// 文件目录：
//
//	context_evolver/
//	├── doc.go                                # 包文档
//	├── core/                                 # 核心框架子包
//	│   ├── context/                          # RuntimeContext + ServiceContext + LLMService + EmbeddingService
//	│   ├── op/                               # BaseOp + OpBase + SequentialOp + ParallelOp
//	│   ├── schema/                           # VectorNode
//	│   ├── vector_store/                     # MemoryVectorStore
//	│   ├── file_connector/                   # JSONFileConnector
//	│   └── persistence/                      # MemoryPersistenceHelper
//	├── schema/                               # IO Schema 子包
//	│   ├── doc.go                            # 包文档
//	│   ├── trajectory.go                     # FeedbackType + Trajectory + TrajectoryBatch
//	│   ├── memory.go                         # BaseMemory + MemoryInterface + ACE/RB/ReMe Memory
//	│   └── io_schema.go                      # ACE/RB/ReMe Request/Response + 泛型 Response
//	├── summary/                              # 摘要管线子包
//	│   └── reme/                             # ReMe 摘要管线
//	│       ├── doc.go                        # 包文档
//	│       ├── update.go                     # 9 个 Summarize Op
//	│       ├── prompt.go                     # 5 个提示词 + ReMeSummaryPrompts
//	│       └── utils.go                      # ParseJSONExperienceResponse + CalculateCosineSimilarity
//	└── retrieve/                             # 检索管线子包
//	    └── reme/                             # ReMe 检索管线
//	        ├── doc.go                        # 包文档
//	        ├── run.go                        # 3 个 Retrieve Op
//	        ├── prompt.go                     # 2 个提示词 + ReMeRetrievePrompts
//	        └── utils.go                      # ParseJSONListResponse + ParseJSONField
```

### 11.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go build ./internal/agentcore/context_evolver/summary/reme/
```

---

## Task 12: retrieve/reme/utils.go + 测试

**目标：** 创建 `retrieve/reme/utils.go`，实现 ParseJSONListResponse + ParseJSONField，以及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/retrieve/reme/utils.go` — 新增
- `internal/agentcore/context_evolver/retrieve/reme/utils_test.go` — 新增

### 12.1 utils.go 完整内容

```go
package reme

import (
	"encoding/json"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseJSONListResponse 解析 LLM 响应中的整数列表。
// 对齐 Python parse_json_list_response(response, key)。
// 从 markdown 代码块中提取 JSON，获取指定 key 的整数列表。
// 解析失败时回退到数字提取，再失败返回 nil。
func ParseJSONListResponse(response string, key string) []int {
	jsonPattern := regexp.MustCompile("```json\\s*([\\s\\S]*?)\\s*```")
	jsonBlocks := jsonPattern.FindAllStringSubmatch(response, -1)

	if len(jsonBlocks) > 0 {
		var parsed any
		if err := json.Unmarshal([]byte(jsonBlocks[0][1]), &parsed); err == nil {
			switch v := parsed.(type) {
			case map[string]any:
				if val, ok := v[key]; ok {
					if nums, ok := toIntSlice(val); ok {
						return nums
					}
				}
			case []any:
				if nums, ok := toIntSlice(v); ok {
					return nums
				}
			}
		}
	}

	// 回退：提取文本中的数字
	numPattern := regexp.MustCompile(`\b\d+\b`)
	numStrs := numPattern.FindAllString(response, -1)
	var result []int
	for _, s := range numStrs {
		var n int
		if _, err := json.Unmarshal([]byte(s), &n); err == nil && n < 100 {
			result = append(result, n)
		}
	}

	if len(result) == 0 {
		logger.Warn(logComponent).Str("key", key).Msg("解析列表响应失败")
	}

	return result
}

// ParseJSONField 解析 LLM 响应中的指定字符串字段。
// 对齐 Python parse_json_field(response, key)。
// 从 markdown 代码块中提取 JSON，获取指定 key 的字符串值。
// 未找到时返回空字符串（Go 不用 Optional）。
func ParseJSONField(response string, key string) string {
	jsonPattern := regexp.MustCompile("```json\\s*([\\s\\S]*?)\\s*```")
	jsonBlocks := jsonPattern.FindAllStringSubmatch(response, -1)

	if len(jsonBlocks) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(jsonBlocks[0][1]), &parsed); err == nil {
			if val, ok := parsed[key]; ok {
				if s, ok := val.(string); ok {
					return s
				}
			}
		}
	}

	// 回退：尝试解析整个响应
	var parsed map[string]any
	if err := json.Unmarshal([]byte(response), &parsed); err == nil {
		if val, ok := parsed[key]; ok {
			if s, ok := val.(string); ok {
				return s
			}
		}
	}

	logger.Warn(logComponent).Str("key", key).Msg("解析 JSON 字段失败")
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toIntSlice 将 any 转换为 []int。
func toIntSlice(val any) ([]int, bool) {
	switch v := val.(type) {
	case []int:
		return v, true
	case []any:
		var result []int
		for _, item := range v {
			switch n := item.(type) {
			case float64:
				result = append(result, int(n))
			case int:
				result = append(result, n)
			default:
				return nil, false
			}
		}
		return result, true
	}
	return nil, false
}
```

### 12.2 utils_test.go 完整内容

```go
package reme

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseJSONListResponse_正常解析(t *testing.T) {
	response := "```json\n{\"ranked_indices\": [2, 0, 4, 1, 3]}\n```"
	result := ParseJSONListResponse(response, "ranked_indices")
	assert.Equal(t, []int{2, 0, 4, 1, 3}, result)
}

func TestParseJSONListResponse_直接数组(t *testing.T) {
	response := "```json\n[2, 0, 4]\n```"
	result := ParseJSONListResponse(response, "ranked_indices")
	assert.Equal(t, []int{2, 0, 4}, result)
}

func TestParseJSONListResponse_无代码块_回退数字提取(t *testing.T) {
	response := "The order is 2, 0, 4, 1, 3"
	result := ParseJSONListResponse(response, "ranked_indices")
	assert.Equal(t, []int{2, 0, 4, 1, 3}, result)
}

func TestParseJSONListResponse_畸形JSON(t *testing.T) {
	response := "```json\n{broken}\n```"
	result := ParseJSONListResponse(response, "ranked_indices")
	assert.Nil(t, result)
}

func TestParseJSONListResponse_空响应(t *testing.T) {
	result := ParseJSONListResponse("", "ranked_indices")
	assert.Nil(t, result)
}

func TestParseJSONField_正常解析(t *testing.T) {
	response := "```json\n{\"rewritten_context\": \"A cohesive guidance\"}\n```"
	result := ParseJSONField(response, "rewritten_context")
	assert.Equal(t, "A cohesive guidance", result)
}

func TestParseJSONField_字段不存在(t *testing.T) {
	response := "```json\n{\"other_field\": \"value\"}\n```"
	result := ParseJSONField(response, "rewritten_context")
	assert.Equal(t, "", result)
}

func TestParseJSONField_无代码块_回退解析(t *testing.T) {
	response := `{"rewritten_context": "fallback value"}`
	result := ParseJSONField(response, "rewritten_context")
	assert.Equal(t, "fallback value", result)
}

func TestParseJSONField_值不是字符串(t *testing.T) {
	response := "```json\n{\"score\": 0.8}\n```"
	result := ParseJSONField(response, "score")
	assert.Equal(t, "", result) // 0.8 是 float64，不是 string
}

func TestParseJSONField_空响应(t *testing.T) {
	result := ParseJSONField("", "key")
	assert.Equal(t, "", result)
}
```

### 12.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/retrieve/reme/... -v -count=1 -run "TestParseJSONListResponse|TestParseJSONField"
```

---

## Task 13: retrieve/reme/prompt.go + 测试

**目标：** 创建 `retrieve/reme/prompt.go`，一比一复刻 Python 的 2 个提示词常量 + ReMeRetrievePrompts 结构体，以及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/retrieve/reme/prompt.go` — 新增
- `internal/agentcore/context_evolver/retrieve/reme/prompt_test.go` — 新增

### 13.1 prompt.go 完整内容

```go
package reme

// ──────────────────────────── 常量 ────────────────────────────

// memoryRerankPrompt LLM 重排序提示词。
// 一比一复刻 Python MEMORY_RERANK_PROMPT。
// 占位符: {query}, {num_candidates}, {candidates}
const memoryRerankPrompt = `You are an expert AI analyst tasked with reranking retrieved experiences based on their relevance to a specific query.

Your task is to analyze the candidates and rank them by relevance, considering:
● DIRECT RELEVANCE: How directly applicable the experience is to the current query
● SITUATION SIMILARITY: How similar the experience context is to the current situation
● ACTIONABILITY: How actionable and specific the experience is
● QUALITY: The overall quality and clarity of the experience

# Current Query
{query}

# Candidate Experiences (Total: {num_candidates})
{candidates}

OUTPUT FORMAT:
Provide a ranked list of candidate indices (0-based) from most relevant to least relevant:
` + "```json" + `
{
"ranked_indices": [2, 0, 4, 1, 3],
"reasoning": "Brief explanation of ranking rationale"
}
```

Note: Include ALL candidate indices in the ranking, even if some are less relevant.`

// memoryRewritePrompt LLM 改写提示词。
// 一比一复刻 Python MEMORY_REWRITE_PROMPT。
// 占位符: {current_query}, {original_context}
const memoryRewritePrompt = `You are an expert AI assistant tasked with rewriting and reorganizing context content to make it more relevant and actionable for the current task.

Your task is to take the original context (containing multiple experiences) and rewrite it as a cohesive, task-specific guidance that directly addresses the current situation.

REWRITING GUIDELINES:
● RELEVANCE FOCUS: Emphasize the most relevant aspects of each experience. Prioritize the most relevant experiences. Use clear, direct language.
● ACTIONABLE INSIGHTS: Extract specific, actionable guidance. Make the context immediately actionable
● COHERENT NARRATIVE: Create a flowing narrative rather than disconnected tips
● SITUATIONAL AWARENESS: Adapt the guidance to the current situation

# Current Task/Query
{current_query}

# Original Context Content (Multiple Experiences)
{original_context}

OUTPUT FORMAT:
Provide the rewritten context:
` + "```json" + `
{
"rewritten_context": "A cohesive, task-specific context message that reorganizes and adapts the original experiences for the current task. This should be written as a unified guidance rather than separate experience items.",
}
```

Guidelines:
- Rewrite as a unified, flowing guidance
- Adapt terminology and examples to match the current task domain
- Consolidate overlapping insights into coherent recommendations
- Prioritize experiences most relevant to the current situation
- Make the guidance feel custom-written for this specific task`

// ──────────────────────────── 结构体 ────────────────────────────

// ReMeRetrievePrompts ReMe 检索管线的提示词集合。
// 对齐 Python ReMePrompt dataclass（retrieve/task/reme/prompt.py）。
type ReMeRetrievePrompts struct {
	// RerankPrompt 重排序提示词
	RerankPrompt string
	// RewritePrompt 改写提示词
	RewritePrompt string
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ReMeRetrieveDefaultPrompts 默认提示词实例。
	// 对齐 Python ReMePrompts = ReMePrompt()。
	ReMeRetrieveDefaultPrompts = ReMeRetrievePrompts{
		RerankPrompt:  memoryRerankPrompt,
		RewritePrompt: memoryRewritePrompt,
	}
)
```

### 13.2 prompt_test.go 完整内容

```go
package reme

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemoryRerankPrompt_占位符(t *testing.T) {
	assert.True(t, strings.Contains(memoryRerankPrompt, "{query}"))
	assert.True(t, strings.Contains(memoryRerankPrompt, "{num_candidates}"))
	assert.True(t, strings.Contains(memoryRerankPrompt, "{candidates}"))
}

func TestMemoryRewritePrompt_占位符(t *testing.T) {
	assert.True(t, strings.Contains(memoryRewritePrompt, "{current_query}"))
	assert.True(t, strings.Contains(memoryRewritePrompt, "{original_context}"))
}

func TestReMeRetrieveDefaultPrompts_字段填充(t *testing.T) {
	assert.Equal(t, memoryRerankPrompt, ReMeRetrieveDefaultPrompts.RerankPrompt)
	assert.Equal(t, memoryRewritePrompt, ReMeRetrieveDefaultPrompts.RewritePrompt)
}

func TestReMeRetrievePrompts_格式化可执行(t *testing.T) {
	result := strings.ReplaceAll(memoryRerankPrompt, "{query}", "test query")
	result = strings.ReplaceAll(result, "{num_candidates}", "5")
	result = strings.ReplaceAll(result, "{candidates}", "candidate text")
	assert.Contains(t, result, "test query")
	assert.Contains(t, result, "5")
	assert.Contains(t, result, "candidate text")
	assert.NotContains(t, result, "{query}")
	assert.NotContains(t, result, "{num_candidates}")
	assert.NotContains(t, result, "{candidates}")
}
```

### 13.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/retrieve/reme/... -v -count=1 -run "TestMemoryRerank|TestMemoryRewrite|TestReMeRetrieve"
```

---

## Task 14: retrieve/reme/run.go — RecallMemoryOp + 测试

**目标：** 创建 `retrieve/reme/run.go`，实现 RecallMemoryOp 及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/retrieve/reme/run.go` — 新增
- `internal/agentcore/context_evolver/retrieve/reme/run_test.go` — 新增

### 14.1 run.go — RecallMemoryOp

```go
package reme

import (
	"context"
	"fmt"
	"strings"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RecallMemoryOp 按 query embedding 检索 top-K ReMe 记忆。
// 对齐 Python RecallMemoryOp。
type RecallMemoryOp struct {
	op.OpBase
	topK int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRecallMemoryOp 创建记忆召回操作。
func NewRecallMemoryOp(sc *cecontext.ServiceContext, topK int) *RecallMemoryOp {
	return &RecallMemoryOp{OpBase: *op.NewOpBase(sc), topK: topK}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Execute 执行记忆召回。
// 对齐 Python RecallMemoryOp.async_execute(context)。
// 从 rc 获取 query 和 user_id，生成 embedding 检索 top-K 记忆。
func (o *RecallMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	query, _ := cecontext.GetTyped[string](rc, "query")
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	embModel := o.EmbeddingModel()
	if embModel == nil {
		return fmt.Errorf("EmbeddingModel not configured in ServiceContext")
	}

	vs := o.VectorStore()
	if vs == nil {
		return fmt.Errorf("VectorStore not configured in ServiceContext")
	}

	logger.Debug(logComponent).Msg("生成查询 embedding")
	queryEmbedding, err := embModel.Embed(ctx, query)
	if err != nil {
		return fmt.Errorf("生成 query embedding 失败: %w", err)
	}

	logger.Debug(logComponent).Int("top_k", o.topK).Msg("搜索 ReMe 记忆")
	vectorNodes, err := vs.Search(ctx, queryEmbedding, o.topK, map[string]any{
		"workspace_id": userID,
		"type":         "reme_memory",
	})
	if err != nil {
		return fmt.Errorf("向量搜索失败: %w", err)
	}

	// 转换为 ReMeRetrievedMemory
	var memories []ceschema.ReMeRetrievedMemory
	for _, node := range vectorNodes {
		memory, convErr := ceschema.NewReMeMemoryFromVectorNode(node), error(nil)
		_ = convErr // NewReMeMemoryFromVectorNode 不返回 error
		m := ceschema.NewReMeMemoryFromVectorNode(node)
		memories = append(memories, ceschema.ReMeRetrievedMemory{
			WhenToUse: m.WhenToUse,
			Content:   m.Content,
		})
	}

	rc.Set("retrieved_memories", memories)
	logger.Info(logComponent).Int("count", len(memories)).Msg("召回 ReMe 记忆")

	return nil
}
```

注意：上面的代码有冗余，修正为：

```go
// 转换为 ReMeRetrievedMemory
var memories []ceschema.ReMeRetrievedMemory
for _, node := range vectorNodes {
	m := ceschema.NewReMeMemoryFromVectorNode(node)
	memories = append(memories, ceschema.ReMeRetrievedMemory{
		WhenToUse: m.WhenToUse,
		Content:   m.Content,
	})
}
```

### 14.2 run_test.go — RecallMemoryOp 测试

```go
package reme

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// fakeEmbeddingService EmbeddingService 的 mock 实现（retrieve/reme 测试用）
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return f.embeddings, f.err
}

// fakeLLMService LLMService 的 mock 实现（retrieve/reme 测试用）
type fakeLLMService struct {
	response string
	err      error
}

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	return f.response, f.err
}

func TestRecallMemoryOp_正常召回(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{embeddings: [][]float64{{1.0, 0.0, 0.0}}}
	sc.RegisterService("embedding_model", emb)

	vs := vector_store.NewMemoryVectorStore()
	// 预存一些记忆
	node := coreschema.NewVectorNode("reme_user1_test", "when to use test", []float64{1.0, 0.0, 0.0}, map[string]any{
		"workspace_id": "user1",
		"type":         "reme_memory",
		"when_to_use":  "when test",
		"content":      "content test",
		"score":        1.0,
	})
	_ = vs.Upsert(context.Background(), node)
	sc.RegisterService("vector_store", vs)

	op := NewRecallMemoryOp(sc, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
	assert.Equal(t, "when test", memories[0].WhenToUse)
}

func TestRecallMemoryOp_EmbeddingModel未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewRecallMemoryOp(sc, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EmbeddingModel not configured")
}

func TestRecallMemoryOp_VectorStore未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{embeddings: [][]float64{{0.1}}}
	sc.RegisterService("embedding_model", emb)

	op := NewRecallMemoryOp(sc, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VectorStore not configured")
}
```

### 14.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/retrieve/reme/... -v -count=1 -run TestRecallMemoryOp
```

---

## Task 15: retrieve/reme/run.go — RerankMemoryOp + RewriteMemoryOp + 测试

**目标：** 在 `retrieve/reme/run.go` 中追加 RerankMemoryOp 和 RewriteMemoryOp，及对应测试。

**涉及文件：**
- `internal/agentcore/context_evolver/retrieve/reme/run.go` — 追加
- `internal/agentcore/context_evolver/retrieve/reme/run_test.go` — 追加

### 15.1 run.go 追加内容

在结构体区块追加：

```go
// RerankMemoryOp LLM 对召回结果重新排序。
// 对齐 Python RerankMemoryOp。
type RerankMemoryOp struct {
	op.OpBase
	llmRerank  bool
	topKRerank int
	prompts    ReMeRetrievePrompts
}

// RewriteMemoryOp LLM 将记忆改写为连贯上下文。
// 对齐 Python RewriteMemoryOp。
type RewriteMemoryOp struct {
	op.OpBase
	llmRewrite bool
	prompts    ReMeRetrievePrompts
}
```

在导出函数区块追加：

```go
// NewRerankMemoryOp 创建重排序操作。
func NewRerankMemoryOp(sc *cecontext.ServiceContext, llmRerank bool, topKRerank int) *RerankMemoryOp {
	return &RerankMemoryOp{
		OpBase:     *op.NewOpBase(sc),
		llmRerank:  llmRerank,
		topKRerank: topKRerank,
		prompts:    ReMeRetrieveDefaultPrompts,
	}
}

// NewRewriteMemoryOp 创建改写操作。
func NewRewriteMemoryOp(sc *cecontext.ServiceContext, llmRewrite bool) *RewriteMemoryOp {
	return &RewriteMemoryOp{
		OpBase:     *op.NewOpBase(sc),
		llmRewrite: llmRewrite,
		prompts:    ReMeRetrieveDefaultPrompts,
	}
}
```

在导出方法区块追加：

```go
// Execute 执行记忆重排序。
// 对齐 Python RerankMemoryOp.async_execute(context)。
func (o *RerankMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !o.llmRerank {
		return nil
	}

	retrievedMemories, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	if !ok || len(retrievedMemories) == 0 {
		logger.Info(logComponent).Msg("无召回记忆，跳过重排序")
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	query, _ := cecontext.GetTyped[string](rc, "query")

	// 格式化候选
	candidates := formatCandidatesForRerank(retrievedMemories)
	prompt := strings.ReplaceAll(o.prompts.RerankPrompt, "{query}", query)
	prompt = strings.ReplaceAll(prompt, "{num_candidates}", fmt.Sprintf("%d", len(retrievedMemories)))
	prompt = strings.ReplaceAll(prompt, "{candidates}", candidates)

	response, err := llm.Generate(ctx, prompt)
	if err != nil {
		return fmt.Errorf("LLM 重排序失败: %w", err)
	}

	// 解析重排序结果
	rerankedIndices := ParseJSONListResponse(response, "ranked_indices")
	if len(rerankedIndices) > 0 {
		var rerankedMemories []ceschema.ReMeRetrievedMemory
		rankedSet := make(map[int]bool)
		for _, idx := range rerankedIndices {
			if 0 <= idx && idx < len(retrievedMemories) && !rankedSet[idx] {
				rerankedMemories = append(rerankedMemories, retrievedMemories[idx])
				rankedSet[idx] = true
			}
		}
		// 添加未被排到的记忆
		for i, mem := range retrievedMemories {
			if !rankedSet[i] {
				rerankedMemories = append(rerankedMemories, mem)
			}
		}
		retrievedMemories = rerankedMemories
	} else {
		logger.Warn(logComponent).Msg("解析重排序响应失败，使用原始顺序")
	}

	// 截取 top-k
	if o.topKRerank < len(retrievedMemories) {
		retrievedMemories = retrievedMemories[:o.topKRerank]
	}

	rc.Set("retrieved_memories", retrievedMemories)
	return nil
}

// Execute 执行记忆改写。
// 对齐 Python RewriteMemoryOp.async_execute(context)。
func (o *RewriteMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	retrievedMemories, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	if !ok || len(retrievedMemories) == 0 {
		logger.Info(logComponent).Msg("无召回记忆，跳过改写")
		rc.Set("memory_string", "")
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	originalMemories := formatMemoriesForContext(retrievedMemories)
	if !o.llmRewrite {
		rc.Set("memory_string", originalMemories)
		return nil
	}

	query, _ := cecontext.GetTyped[string](rc, "query")
	prompt := strings.ReplaceAll(o.prompts.RewritePrompt, "{current_query}", query)
	prompt = strings.ReplaceAll(prompt, "{original_context}", originalMemories)

	response, err := llm.Generate(ctx, prompt)
	if err != nil {
		return fmt.Errorf("LLM 改写失败: %w", err)
	}

	rewrittenContext := ParseJSONField(response, "rewritten_context")
	if rewrittenContext != "" {
		rc.Set("memory_string", rewrittenContext)
	} else {
		logger.Warn(logComponent).Msg("解析改写结果失败，使用格式化原文")
		rc.Set("memory_string", originalMemories)
	}

	return nil
}
```

在非导出函数区块追加（在 retrieve/reme 包内）：

```go
// formatCandidatesForRerank 格式化候选记忆用于重排序提示词。
// 对齐 Python RerankMemoryOp._format_candidates_for_rerank。
func formatCandidatesForRerank(candidates []ceschema.ReMeRetrievedMemory) string {
	var formatted []string
	for i, candidate := range candidates {
		text := fmt.Sprintf("Candidate %d:\nCondition: %s\nExperience: %s\n", i, candidate.WhenToUse, candidate.Content)
		formatted = append(formatted, text)
	}
	return strings.Join(formatted, "---\n")
}

// formatMemoriesForContext 格式化记忆用于上下文生成。
// 对齐 Python RewriteMemoryOp._format_memories_for_context。
func formatMemoriesForContext(memories []ceschema.ReMeRetrievedMemory) string {
	var formatted []string
	for i, memory := range memories {
		text := fmt.Sprintf("Memory %d:\n  When to use: %s\n  Content: %s\n", i+1, memory.WhenToUse, memory.Content)
		formatted = append(formatted, text)
	}
	return strings.Join(formatted, "\n")
}
```

### 15.2 run_test.go 追加测试

```go
func TestRerankMemoryOp_正常重排序(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n{\"ranked_indices\": [1, 0], \"reasoning\": \"test\"}\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewRerankMemoryOp(sc, true, 2)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when1", Content: "content1"},
		{WhenToUse: "when2", Content: "content2"},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	require.True(t, ok)
	assert.Len(t, memories, 2)
	assert.Equal(t, "when2", memories[0].WhenToUse) // index 1 排在前面
	assert.Equal(t, "when1", memories[1].WhenToUse)
}

func TestRerankMemoryOp_跳过重排序(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewRerankMemoryOp(sc, false, 5)
	rc := cecontext.NewRuntimeContext()

	input := []ceschema.ReMeRetrievedMemory{{WhenToUse: "test", Content: "mem"}}
	rc.Set("retrieved_memories", input)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	require.True(t, ok)
	assert.Len(t, memories, 1)
}

func TestRerankMemoryOp_无召回记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewRerankMemoryOp(sc, true, 5)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)
}

func TestRerankMemoryOp_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewRerankMemoryOp(sc, true, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{{WhenToUse: "test", Content: "mem"}})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

func TestRerankMemoryOp_LLM返回无效JSON(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "not json"}
	sc.RegisterService("llm", llm)

	op := NewRerankMemoryOp(sc, true, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when1", Content: "content1"},
		{WhenToUse: "when2", Content: "content2"},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 解析失败时保持原始顺序
	memories, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	require.True(t, ok)
	assert.Len(t, memories, 2)
}

func TestRewriteMemoryOp_正常改写(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n{\"rewritten_context\": \"A unified guidance for the task\"}\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when1", Content: "content1"},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	result, ok := cecontext.GetTyped[string](rc, "memory_string")
	require.True(t, ok)
	assert.Equal(t, "A unified guidance for the task", result)
}

func TestRewriteMemoryOp_跳过改写(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewRewriteMemoryOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when1", Content: "content1"},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	result, ok := cecontext.GetTyped[string](rc, "memory_string")
	require.True(t, ok)
	assert.Contains(t, result, "when1")
}

func TestRewriteMemoryOp_无召回记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	result, ok := cecontext.GetTyped[string](rc, "memory_string")
	require.True(t, ok)
	assert.Equal(t, "", result)
}

func TestRewriteMemoryOp_LLM返回无效JSON(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "invalid json"}
	sc.RegisterService("llm", llm)

	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when1", Content: "content1"},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 解析失败时使用格式化原文
	result, ok := cecontext.GetTyped[string](rc, "memory_string")
	require.True(t, ok)
	assert.Contains(t, result, "when1")
}
```

### 15.3 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go test ./internal/agentcore/context_evolver/retrieve/reme/... -v -count=1 -run "TestRerankMemoryOp|TestRewriteMemoryOp"
```

---

## Task 16: retrieve/reme/doc.go + 更新顶层 doc.go

**目标：** 创建 `retrieve/reme/doc.go`。顶层 doc.go 已在 Task 11 更新。

**涉及文件：**
- `internal/agentcore/context_evolver/retrieve/reme/doc.go` — 新增

### 16.1 retrieve/reme/doc.go

```go
// Package reme 提供 ReMe（Reflective Memory）算法的检索管线操作。
//
// ReMe 检索管线在任务开始时从向量库中召回相关记忆，
// 通过 LLM 重排序和改写后注入 Agent 上下文。
//
// 管线流程：
//
//	RecallMemoryOp → RerankMemoryOp → RewriteMemoryOp
//
// 文件目录：
//
//	retrieve/reme/
//	├── doc.go           # 包文档
//	├── run.go           # 3 个 Retrieve Op (RecallMemoryOp + RerankMemoryOp + RewriteMemoryOp)
//	├── prompt.go        # 2 个提示词常量 + ReMeRetrievePrompts
//	└── utils.go         # ParseJSONListResponse + ParseJSONField
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/retrieve/task/reme/
package reme
```

### 16.2 验证

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go build ./internal/agentcore/context_evolver/retrieve/reme/
```

---

## Task 17: 编译验证 + 覆盖率检查 + IMPLEMENTATION_PLAN 状态更新

**目标：** 全量编译验证、覆盖率检查、更新 IMPLEMENTATION_PLAN.md 中 P3 步骤的状态标记。

### 17.1 编译前检查

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || echo "无残留进程"
```

### 17.2 全量编译

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
go build ./internal/agentcore/context_evolver/...
```

### 17.3 覆盖率检查

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go

# core/context
go test ./internal/agentcore/context_evolver/core/context/... -cover -count=1

# core/op
go test ./internal/agentcore/context_evolver/core/op/... -cover -count=1

# summary/reme
go test ./internal/agentcore/context_evolver/summary/reme/... -cover -count=1

# retrieve/reme
go test ./internal/agentcore/context_evolver/retrieve/reme/... -cover -count=1

# 整体
go test ./internal/agentcore/context_evolver/... -cover -count=1
```

目标：每个包覆盖率 ≥ 85%。如果不足，补充测试用例。

### 17.4 更新 IMPLEMENTATION_PLAN.md

将 P3 相关步骤从 `☐` 改为 `✅`：

- P3 core 层扩展步骤
- P3 summary/reme 步骤
- P3 retrieve/reme 步骤

---

## 注意事项

1. **提示词必须一比一复刻**：Python 中 `{{` / `}}` 在 f-string 中转义为大括号，Go 反引号字符串中不需要转义，直接写 `{xxx}`。但 Python 提示词中 ```` ```json ```` 和 ```` ``` ```` 这类 markdown 代码块标记在 Go 反引号字符串中无法直接出现（反引号是字符串定界符），必须用字符串拼接：`` ` + "```json" + ` ``。

2. **两个 reme 包是不同的 Go 包**：`summary/reme` 和 `retrieve/reme` 包名都是 `package reme`，但路径不同，互不影响。

3. **MemoryVectorStore.GetAll 已实现**：在 P1 阶段已实现，VectorStoreService 接口需要补充 GetAll 方法声明，但 MemoryVectorStore 的实现无需改动。

4. **fakeLLMService/fakeEmbeddingService 在各测试文件中独立定义**：因为两个 reme 包是不同的 Go 包，测试文件中的 mock 需要各自定义。

5. **strconv import**：Task 8 的 MemoryValidationOp 中 `strconv.ParseFloat` 需要在 update.go 的 import 中添加 `"strconv"`。
