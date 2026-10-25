# 13.17 TripleExtractor 三元组抽取器 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 OpenIE 三元组抽取器 TripleExtractor——LLM 并发抽取 chunk 文本为 RDF 风格 (subject, predicate, object) 三元组，作为 GraphRAG 图谱索引的写入原料（对齐 Python triple_extractor.py 339 行 + base.py 49 行 + common/triple.py 20 行）。

**Architecture:** 新建 extractor 子包（Extractor 接口 Extract+Process 双方法 + TripleExtractor LLM 实现）；Triple 数据模型放 retrieval/common 包（对齐 Python 文件位置 + 读取侧 13.22/13.28 复用）。并发模型为方案 A（预分配槽位 + buffered chan 信号量 + wg 收齐后按 chunk 序扫描），逐字对齐 Python `asyncio.gather(return_exceptions=True)` 的"全跑完再扫、首错按 chunk 序"语义。JSON 解析走 `jsonrepair.RepairWithOptions` 一步取 `result.Value`（对应 Python `repair_json(return_objects=True)`）。

**Tech Stack:** Go 1.x / `*llm.Model` + `llm.WithClient` mock 注入（13.15 先例）/ `github.com/RealAlexandreAI/json-repair v0.0.18`（go.mod 已有）/ `strings.NewReplacer` 提示词渲染（正文花括号禁 Sprintf）/ exception.BuildError 错误码 155507。

**设计文档:** `docs/superpowers/specs/2026-10-27-tripleextractor-13.17-design.md`（已批准，偏差归档 §8 共 7 条——实施时必须遵守）

**关键对齐事实（已核实，勿重查）:**

- Python 源：`/home/opensource/agent-core/openjiuwen/core/retrieval/indexing/processor/extractor/triple_extractor.py`（339 行）+ `extractor/base.py`（49 行）+ `common/triple.py`（20 行）
- Python 测试：`tests/unit_tests/core/retrieval/indexing/processor/extractor/test_triple_extractor.py`（16 例）+ `test_base.py`（3 例）+ `tests/unit_tests/core/retrieval/common/test_triple.py`（4 例）
- `jsonrepair.RepairWithOptions(src, jsonrepair.RepairOptions{}) (RepairResult, error)`，`RepairResult{Value any; JSON string; Log []RepairLogEntry}`，零值 options 走正常修复路径且自带 recover（options.go:49-55）
- **jsonrepair 数字类型三态（2026-10-27 探针实测）**：合法 JSON 路径（`json.Valid` 短路）经 `decodeJSONValue` 的 `UseNumber()` 产出 `json.Number`；深修复路径 `parseNumber` 产出 `float64`（含 . e E）或 `int`（纯整数）。pyStr 必须三分支全覆盖（json.Number→`string(val)` 原字面、float64→pyFloatStr、int→strconv.Itoa）。极坏输入（空串/纯文本）不报 err——`Value` 为 `string`，走 parseTriples default 分支返回 false（与 Python repair 语义对齐）。
- `llm.NewModel(clientCfg, modelCfg, llm.WithClient(mock))` + `llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))` + `llmschema.NewModelRequestConfig()`（captioner_test.go:46-57 先例）
- `model_clients.NewMessagesParam(msg)` 接收 `...llmschema.BaseMessage`；`llmschema.NewUserMessage(prompt)` 单文本；`resp.Content.Text()` 取文本
- `model_clients.WithInvokeTemperature(t float64)`（invoke_params.go:257）；mock 内 `model_clients.NewInvokeParams(opts...).Temperature` 为 `*float64`
- 错误码已预埋：`exception.StatusRetrievalKbTripleExtractionProcessError`（155507，模板 "...reason: {error_msg}"）；`errors.As(err, &be)` 判 BaseError；`be.Code()`/`be.Message()` 访问
- `common.TextChunk{ID, Text, DocID, Metadata map[string]any}`（document.go:63-74，json tag `id_`）
- BaseModelClient 接口 7 方法：Invoke/Stream/GenerateImage/GenerateSpeech/GenerateVideo/TranscribeAudio/Release/SupportsKVCacheRelease（stubCaptionClient 全实现先例 captioner_test.go:20-43）
- 提示词铁律：Python 模板**逐字复制**（`{{`→`{` 归一），禁止翻译改写（feedback 记忆）

---

## 文件结构

```
internal/agentcore/retrieval/common/
├── triple.go                  # 新增：Triple 数据模型（Task 1）
├── triple_test.go              # 新增：test_triple.py 4 例 1:1（Task 1）
└── doc.go                      # 修改：文件目录树加 triple.go 条目（Task 8）

internal/agentcore/retrieval/indexing/processor/extractor/
├── doc.go                      # 新增：包文档（Task 8）
├── base.go                     # 新增：Extractor 接口 + ExtractOption（Task 2）
├── base_test.go                # 新增：test_base.py 2 例 1:1（Task 2）
├── triple_extractor.go         # 新增：TripleExtractor 主体（Task 3-6）
└── triple_extractor_test.go    # 新增：16 例 1:1 + 8 项 Go 增强（Task 3-7）
```

Task 3-6 顺序说明：先构造器（可测字段），再 buildPrompt（纯函数），再 parseTriples+pyStr（纯函数），最后 Extract/extractChunk 并发编排 + Process 转发——依赖链自底向上，每层都有独立可跑测试。

---

### Task 1: common/triple.go — Triple 数据模型

**Files:**
- Create: `internal/agentcore/retrieval/common/triple.go`
- Test: `internal/agentcore/retrieval/common/triple_test.go`

- [ ] **Step 1: Write the failing test**

```go
package common

import "testing"

// TestTriple_创建 测试创建三元组（对齐 test_triple.py test_create_triple）
func TestTriple_创建(t *testing.T) {
	triple := Triple{
		Subject:   "Alice",
		Predicate: "knows",
		Object:    "Bob",
	}
	if triple.Subject != "Alice" {
		t.Errorf("Subject 应为 Alice，实际 %s", triple.Subject)
	}
	if triple.Predicate != "knows" {
		t.Errorf("Predicate 应为 knows，实际 %s", triple.Predicate)
	}
	if triple.Object != "Bob" {
		t.Errorf("Object 应为 Bob，实际 %s", triple.Object)
	}
	// 对齐 assert triple.metadata == {}：Python default_factory=dict 零值为 nil，
	// Go 结构体零值 nil map 语义等价（读取/len/遍历安全，赋值前需初始化）
	if triple.Metadata != nil {
		t.Errorf("Metadata 零值应为 nil（对齐 Python 未传时 default_factory=dict 空字典的语义等价物），实际 %v", triple.Metadata)
	}
}

// TestTriple_带metadata 测试带元数据创建（对齐 test_create_triple_with_metadata）
func TestTriple_带metadata(t *testing.T) {
	metadata := map[string]any{"source": "test", "doc_id": "doc_1"}
	triple := Triple{
		Subject:   "Alice",
		Predicate: "knows",
		Object:    "Bob",
		Metadata:  metadata,
	}
	if len(triple.Metadata) != 2 {
		t.Errorf("Metadata 应有 2 个键，实际 %d", len(triple.Metadata))
	}
	if triple.Metadata["source"] != "test" {
		t.Errorf("Metadata[source] 应为 test，实际 %v", triple.Metadata["source"])
	}
	if triple.Metadata["doc_id"] != "doc_1" {
		t.Errorf("Metadata[doc_id] 应为 doc_1，实际 %v", triple.Metadata["doc_id"])
	}
}

// TestTriple_全字段 测试全字段创建（对齐 test_create_triple_with_all_fields）
func TestTriple_全字段(t *testing.T) {
	metadata := map[string]any{"source": "test"}
	triple := Triple{
		Subject:   "Alice",
		Predicate: "knows",
		Object:    "Bob",
		Metadata:  metadata,
	}
	if triple.Subject != "Alice" || triple.Predicate != "knows" || triple.Object != "Bob" {
		t.Errorf("三字段断言失败: %+v", triple)
	}
	if triple.Metadata["source"] != "test" {
		t.Errorf("Metadata[source] 应为 test，实际 %v", triple.Metadata["source"])
	}
}

// TestTriple_JSON序列化 测试 JSON tag（Go 增强项：验证 spec §4.1 无 omitempty 决策）
func TestTriple_JSON序列化(t *testing.T) {
	// marshal nil Metadata 恒输出 "metadata" 键（对齐 Python default_factory=dict
	// 序列化输出 {} 而非省略——无 omitempty 决策）
	triple := Triple{Subject: "a", Predicate: "b", Object: "c"}
	data, err := json.Marshal(triple)
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	if !strings.Contains(string(data), `"metadata"`) {
		t.Errorf("序列化应含 metadata 键（无 omitempty），实际 %s", data)
	}
}
```

```go
// triple_test.go 顶部 import 块（与上面测试同文件）：
import (
	"encoding/json"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test ./internal/agentcore/retrieval/common/ -run 'TestTriple' -v 2>&1 | tail -5
```

Expected: FAIL — `undefined: Triple`

- [ ] **Step 3: Write minimal implementation**

```go
// Package common 内文件。文件头只保留 package + import（规范 2）。
package common

// ──────────────────────────── 结构体 ────────────────────────────

// Triple 三元组数据模型，OpenIE 抽取的最小知识单元。
//
// Python: openjiuwen/core/retrieval/common/triple.py (Triple)
// pydantic 必填校验（ValidationError）无 Go 显式对应物——
// str(x).strip() 构造点保证字段非空天然满足（偏差归档 spec §8-4）。
type Triple struct {
	// Subject 主语
	Subject string `json:"subject"`
	// Predicate 谓语
	Predicate string `json:"predicate"`
	// Object 宾语
	Object string `json:"object"`
	// Metadata 元数据（对齐 Python default_factory=dict；无 omitempty——
	// 序列化恒输出 metadata 键，nil 序列化为 null 对齐 Python 空字典场景由
	// 构造方初始化保证）
	Metadata map[string]any `json:"metadata"`
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -run 'TestTriple' -v 2>&1 | tail -6
```

Expected: 4 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/common/triple.go internal/agentcore/retrieval/common/triple_test.go
git commit -m "feat(13.17): common.Triple 三元组数据模型（对齐 triple.py 4 例 1:1）"
```

---

### Task 2: extractor 包 — Extractor 接口 + ExtractOption

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/extractor/base.go`
- Test: `internal/agentcore/retrieval/indexing/processor/extractor/base_test.go`

- [ ] **Step 1: Write the failing test**

```go
package extractor

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// concreteExtractor 测试用具体抽取器（对齐 test_base.py ConcreteExtractor）
type concreteExtractor struct{}

// Extract 简单抽取逻辑：文本含 "knows" 时产出一条三元组（对齐 Python 实现）
func (c *concreteExtractor) Extract(_ context.Context, chunks []common.TextChunk, _ ...ExtractOption) ([]common.Triple, error) {
	triples := make([]common.Triple, 0)
	for _, chunk := range chunks {
		if strings.Contains(chunk.Text, "knows") {
			triples = append(triples, common.Triple{
				Subject:   "Alice",
				Predicate: "knows",
				Object:    "Bob",
				Metadata:  map[string]any{"doc_id": chunk.DocID},
			})
		}
	}
	return triples, nil
}

// Process 转发 Extract（对齐 Python 基类 process → self.extract）
func (c *concreteExtractor) Process(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error) {
	return c.Extract(ctx, chunks, opts...)
}

// TestExtractor_Extract 测试具体实现 Extract（对齐 test_base.py test_extract）
func TestExtractor_Extract(t *testing.T) {
	extractor := &concreteExtractor{}
	chunks := []common.TextChunk{
		{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"},
		{ID: "2", Text: "Some other text", DocID: "doc_1"},
	}
	triples, err := extractor.Extract(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Extract 不应报错: %v", err)
	}
	if len(triples) != 1 {
		t.Fatalf("应抽取 1 条三元组，实际 %d", len(triples))
	}
	if triples[0].Subject != "Alice" || triples[0].Predicate != "knows" || triples[0].Object != "Bob" {
		t.Errorf("三元组字段断言失败: %+v", triples[0])
	}
}

// TestExtractor_Process 测试 Process 转发（对齐 test_base.py test_process）
func TestExtractor_Process(t *testing.T) {
	extractor := &concreteExtractor{}
	chunks := []common.TextChunk{{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"}}
	triples, err := extractor.Process(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Process 不应报错: %v", err)
	}
	if len(triples) != 1 {
		t.Fatalf("Process 应转发产出 1 条三元组，实际 %d", len(triples))
	}
}
```

注意：cannot_instantiate 抽象类用例归档（spec §8-4：Go 接口编译期天然防实现外实例化，零代码对应）。

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestExtractor' -v 2>&1 | tail -5
```

Expected: FAIL — 包不存在编译错误（`no Go files` 或 undefined: ExtractOption）

- [ ] **Step 3: Write minimal implementation**

```go
package extractor

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────
// ──────────────────────────── 非导出函数 ────────────────────────────

// Extractor 抽取器抽象接口，用于三元组等信息抽取。
//
// Python: openjiuwen/core/retrieval/indexing/processor/extractor/base.py (Extractor)
// 注意：不建 Processor 基接口（13.5/13.10 既定决策），Process 作为
// Processor 契约兼容入口直接挂在接口上（对齐 Chunker/Parser 风格）。
type Extractor interface {
	// Extract 从文本分块中抽取信息（如三元组）。
	// Python **kwargs 由 opts 收纳（当前无消费者传参，预留）。
	Extract(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error)
	// Process 处理文本分块（Processor 契约兼容入口），默认转发 Extract。
	// 对齐 Python Extractor.process → self.extract(chunks, **kwargs)。
	Process(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error)
}

// ExtractOption Extract 的可选参数（对齐 Python **kwargs 收纳）。
type ExtractOption struct {
	// Extra 透传给实现的额外参数
	Extra map[string]any
}
```

注：interface 归结构体区块（规范 2 特例），struct ExtractOption 同区块紧随接口后。

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestExtractor' -v 2>&1 | tail -5
```

Expected: 2 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/extractor/base.go internal/agentcore/retrieval/indexing/processor/extractor/base_test.go
git commit -m "feat(13.17): extractor.Extractor 接口 + ExtractOption（对齐 base.py 2 例 1:1）"
```

---

### Task 3: TripleExtractor 构造器 + 选项函数

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor.go`
- Test: `internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go`

- [ ] **Step 1: Write the failing test**

triple_extractor_test.go 分多步追加，本步先写构造测试 + mock 基建：

```go
package extractor

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)
```

（**import 按 Task 增量追加**——Go 编译器对未使用 import 报错，禁止一次性全量引入：Task 4 追加 `strings`、Task 5 追加 `encoding/json`、Task 6 追加 `sync` + `sync/atomic` + `common` + `exception`。）

```go
// ──────────────────────────── 测试辅助 ────────────────────────────

// stubTripleClient 惰性 mock：所有方法零行为（captioner_test.go stubCaptionClient 先例）
type stubTripleClient struct{}

func (s *stubTripleClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubTripleClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubTripleClient) SupportsKVCacheRelease() bool { return false }

// newTripleTestModel 构造带 stub 客户端的 Model（WithClient 注入，走真实 Invoke 链）
func newTripleTestModel(t *testing.T) *llm.Model {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(&stubTripleClient{}))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// TestTripleExtractor_构造 测试初始化（对齐 test_init）
func TestTripleExtractor_构造(t *testing.T) {
	model := newTripleTestModel(t)
	ex := NewTripleExtractor(model, "test-model",
		WithTripleTemperature(0.0), WithTripleMaxConcurrent(10))
	if ex.llmClient != model {
		t.Error("llmClient 应为构造传入的 model")
	}
	if ex.modelName != "test-model" {
		t.Errorf("modelName 应为 test-model，实际 %s", ex.modelName)
	}
	if ex.temperature != 0.0 {
		t.Errorf("temperature 应为 0.0，实际 %v", ex.temperature)
	}
	if cap(ex.limiter) != 10 {
		t.Errorf("limiter 容量应为 10，实际 %d", cap(ex.limiter))
	}
}

// TestTripleExtractor_默认值构造 测试默认构造（对齐 test_init_with_defaults）
func TestTripleExtractor_默认值构造(t *testing.T) {
	ex := NewTripleExtractor(newTripleTestModel(t), "test-model")
	if ex.temperature != 0.0 {
		t.Errorf("默认 temperature 应为 0.0，实际 %v", ex.temperature)
	}
	if cap(ex.limiter) != 50 {
		t.Errorf("默认 limiter 容量应为 50，实际 %d", cap(ex.limiter))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestTripleExtractor' -v 2>&1 | tail -5
```

Expected: FAIL — `undefined: NewTripleExtractor`

- [ ] **Step 3: Write minimal implementation**

triple_extractor.go 首版（后续 Task 4-6 逐步充实）：

```go
package extractor

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TripleExtractor 基于 LLM 的 OpenIE 三元组抽取器。
//
// Python: openjiuwen/core/retrieval/indexing/processor/extractor/triple_extractor.py
type TripleExtractor struct {
	// llmClient LLM 客户端实例（Python: llm_client: Any → Go 具体类型，13.15 先例）
	llmClient *llm.Model
	// modelName 模型名（Python 死参数：存字段但 invoke 从不使用——偏差 §8-1）
	modelName string
	// temperature 温度参数（invoke 时透传）
	temperature float64
	// limiter 并发信号量（对齐 asyncio.Semaphore(max_concurrent)）
	limiter chan struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// TripleExtractorOption TripleExtractor 构造选项函数。
type TripleExtractorOption func(*TripleExtractor)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 TripleExtractor 实现 Extractor
var _ Extractor = (*TripleExtractor)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTripleTemperature 设置温度参数。
func WithTripleTemperature(t float64) TripleExtractorOption {
	return func(e *TripleExtractor) { e.temperature = t }
}

// WithTripleMaxConcurrent 设置最大并发数。
func WithTripleMaxConcurrent(n int) TripleExtractorOption {
	return func(e *TripleExtractor) { e.limiter = make(chan struct{}, n) }
}

// NewTripleExtractor 构造 TripleExtractor。
// 默认 temperature=0.0 / maxConcurrent=50（对齐 Python 默认参数）。
//
// Python: triple_extractor.py TripleExtractor.__init__
func NewTripleExtractor(llmClient *llm.Model, modelName string, opts ...TripleExtractorOption) *TripleExtractor {
	e := &TripleExtractor{
		llmClient:   llmClient,
		modelName:   modelName,
		temperature: 0.0,
		limiter:     make(chan struct{}, 50),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：`TripleExtractorOption` 直接定义为导出函数类型（归枚举区块——规范 2 特例"类型别名/简单类型定义归枚举区块"），无需非导出底类型+别名两步。Task 3 版本 Extract/Process 尚未实现但编译期断言已存在——**Task 3 时先注释断言行**（`// var _ Extractor = ...`），Task 6 实现 Extract/Process 后取消注释，否则编译失败。

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestTripleExtractor_构造|TestTripleExtractor_默认值构造' -v 2>&1 | tail -5
```

Expected: 2 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor.go internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go
git commit -m "feat(13.17): TripleExtractor 构造器 + 温度/并发选项（对齐 test_init 2 例）"
```

---

### Task 4: buildPrompt + tripleExtractionPrompt 常量

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor.go`
- Test: `internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go`（追加）

- [ ] **Step 1: Write the failing test**

triple_extractor_test.go 追加：

```go
// ──────────────────────────── buildPrompt 测试 ────────────────────────────

// TestBuildPrompt_标题为空 测试空标题回落 Untitled（Go 增强 §7.2-4）
func TestBuildPrompt_标题为空(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("Alice knows Bob", "")
	if !strings.Contains(prompt, "Title:\nUntitled\n") {
		t.Error("空标题应渲染为 Untitled")
	}
	if !strings.Contains(prompt, "Passage:\nAlice knows Bob") {
		t.Error("passage 应原样渲染")
	}
}

// TestBuildPrompt_非空标题 测试非空标题渲染
func TestBuildPrompt_非空标题(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("body text", "My Title")
	if !strings.Contains(prompt, "Title:\nMy Title\n") {
		t.Error("非空标题应原样渲染")
	}
}

// TestBuildPrompt_花括号安全 测试 passage 含 { 特殊字符不破坏 Replacer（Go 增强 §7.2-4）
func TestBuildPrompt_花括号安全(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("var x = {a: 1}; func() { return 2; }", "T")
	// 花括号原样保留（NewReplacer 只替换 {title}/{passage} 字面量，
	// passage 内部花括号替换后注入不参与二次解析）
	if !strings.Contains(prompt, "var x = {a: 1}") {
		t.Error("passage 花括号应原样保留")
	}
	if !strings.Contains(prompt, "Title:\nT\n") {
		t.Error("标题替换应不受 passage 花括号影响")
	}
}

// TestBuildPrompt_模板完整性 测试模板关键段落齐备（Go 增强：防逐字复制走样）
func TestBuildPrompt_模板完整性(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("p", "t")
	for _, want := range []string{
		"# Instruction",
		"# Demonstration 1",
		"# Demonstration 2",
		"Magic Johnson",
		"Elden Ring",
		`"named_entities"`,
		`"triples"`,
		`If no triples are found, return {"named_entities": [...], "triples": []}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("模板应包含 %q", want)
		}
	}
}
```

顶部 import 追加 `"strings"`。

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestBuildPrompt' -v 2>&1 | tail -5
```

Expected: FAIL — `e.buildPrompt undefined`

- [ ] **Step 3: Write minimal implementation**

triple_extractor.go 常量区块加入提示词常量（**逐字复制 Python L158-262 模板，`{{`→`{`、`}}`→`}` 归一**——下面展示完整内容，实施者直接复制本块，禁止翻译改写）：

```go
const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// tripleExtractionPrompt OpenIE 三元组抽取提示词模板。
	//
	// Python: triple_extractor.py TripleExtractor._build_prompt 内模板
	//（逐字复制，{{/}} 归一为 {/}——Python .format 转义字面花括号；
	// 禁止翻译改写——记忆 feedback_prompt_verbatim_copy 既定规则）。
	// 归一后 {title}/{passage} 为唯二命名槽。
	tripleExtractionPrompt = `# Instruction

Your task is to construct an RDF-style graph from the given title and passage.
Extract named entities and relationships, then return the result as exactly one valid JSON object.

Each triple should represent a meaningful relationship in the graph.
Each triple should contain at least one, and preferably two, named entities from the title or passage.
Clearly resolve pronouns to specific names whenever possible.

Return only one valid JSON object in this format:
{
  "named_entities": ["entity1", "entity2"],
  "triples": [
    ["subject1", "predicate1", "object1"],
    ["subject2", "predicate2", "object2"]
  ]
}

Requirements:
- Output valid JSON only. Do not use markdown, comments, or extra text.
- Return exactly one top-level JSON object.
- The top-level object must contain exactly two keys: "named_entities" and "triples".
- "named_entities" must be a JSON array of strings.
- "triples" must be a JSON array.
- Each item in "triples" must be a JSON array of exactly three strings.
- Do not output tuples, objects, or arrays with more than three elements inside "triples".
- Use double quotes for all JSON strings.
- If no triples are found, return {"named_entities": [...], "triples": []}.
- Resolve pronouns to specific names when possible.
- Prefer triples that use at least one, and preferably two, named entities from the title or passage.
- Keep entity and predicate wording consistent with the source language.
- Do not include duplicate triples.

# Demonstration 1

Title:
Magic Johnson

Passage:
After winning a national championship with Michigan State in 1979, Johnson was selected first overall in the 1979 NBA draft by the Lakers, leading the team to five NBA championships during their "Showtime" era.

Output:
{
  "named_entities": [
    "Michigan State",
    "national championship",
    "1979",
    "Magic Johnson",
    "National Basketball Association",
    "Los Angeles Lakers",
    "NBA Championship"
  ],
  "triples": [
    ["Magic Johnson", "member of sports team", "Michigan State"],
    ["Michigan State", "award", "national championship"],
    ["Michigan State", "award date", "1979"],
    ["Magic Johnson", "draft pick number", "1"],
    ["Magic Johnson", "drafted in", "1979"],
    ["Magic Johnson", "drafted by", "Los Angeles Lakers"],
    ["Magic Johnson", "member of sports team", "Los Angeles Lakers"],
    ["Magic Johnson", "league", "National Basketball Association"],
    ["Los Angeles Lakers", "league", "National Basketball Association"],
    ["Los Angeles Lakers", "award received", "NBA Championship"]
  ]
}

# Demonstration 2

Title:
Elden Ring

Passage:
Elden Ring is a 2022 action role-playing game developed by FromSoftware. It was directed by Hidetaka Miyazaki with worldbuilding provided by American fantasy writer George R. R. Martin.

Output:
{
  "named_entities": [
    "Elden Ring",
    "2022",
    "action role-playing game",
    "FromSoftware",
    "Hidetaka Miyazaki",
    "United States of America",
    "fantasy",
    "George R. R. Martin"
  ],
  "triples": [
    ["Elden Ring", "publication", "2022"],
    ["Elden Ring", "genre", "action role-playing game"],
    ["Elden Ring", "publisher", "FromSoftware"],
    ["Elden Ring", "director", "Hidetaka Miyazaki"],
    ["Elden Ring", "screenwriter", "George R. R. Martin"],
    ["George R. R. Martin", "country of citizenship", "United States of America"],
    ["George R. R. Martin", "genre", "fantasy"]
  ]
}

# Input

Title:
{title}

Passage:
{passage}
`
)
```

非导出函数区块加入：

```go
// buildPrompt 构建 OpenIE 三元组抽取提示词。
//
// title 空 → "Untitled"（对齐 Python title or "Untitled"）；strings.NewReplacer
// 仅替换 {title}/{passage} 唯二命名槽——不用 fmt.Sprintf（正文大量花括号会炸）。
// 渲染结果与 Python .format() 逐字节等价。
//
// Python: triple_extractor.py TripleExtractor._build_prompt
func (e *TripleExtractor) buildPrompt(passage, title string) string {
	if title == "" {
		title = "Untitled"
	}
	return strings.NewReplacer("{title}", title, "{passage}", passage).Replace(tripleExtractionPrompt)
}
```

文件头部 import 追加：

```go
import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestBuildPrompt' -v 2>&1 | tail -6
```

Expected: 4 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor.go internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go
git commit -m "feat(13.17): buildPrompt + tripleExtractionPrompt 逐字模板（花括号 Replacer 渲染）"
```

---

### Task 5: parseTriples + pyStr

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor.go`
- Test: `internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go`（追加）

- [ ] **Step 1: Write the failing test**

triple_extractor_test.go 追加（对齐 Python parse 7 例 1:1 + Go 增强 pyStr/fence/极坏输入）：

```go
// ──────────────────────────── parseTriples 测试（Python 7 例 1:1）────────────────────────────

// TestParseTriples_顶层数组 对齐 test_parse_triples_json_array
func TestParseTriples_顶层数组(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["a", "b", "c"]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d，应为 true/1", ok, len(triples))
	}
	if triples[0].Subject != "a" || triples[0].Predicate != "b" || triples[0].Object != "c" {
		t.Errorf("三元组字段断言失败: %+v", triples[0])
	}
}

// TestParseTriples_多余字段截取 对齐 test_parse_triples_extra_fields_ignored
func TestParseTriples_多余字段截取(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["a", "b", "c", "ignored", 99]]`, "d1", "c1")
	if !ok {
		t.Fatal("应为 true")
	}
	if triples[0].Object != "c" {
		t.Errorf("Object 应截取前三项为 c，实际 %s", triples[0].Object)
	}
}

// TestParseTriples_包裹dict 对齐 test_parse_triples_wrapped_dict
func TestParseTriples_包裹dict(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"triples": [["x", "y", "z"]]}`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d，应为 true/1", ok, len(triples))
	}
	if triples[0].Subject != "x" {
		t.Errorf("Subject 应为 x，实际 %s", triples[0].Subject)
	}
}

// TestParseTriples_含named_entities 对齐 test_parse_triples_prompt_shape
func TestParseTriples_含named_entities(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"named_entities": ["Alice", "Bob"], "triples": [["Alice", "knows", "Bob"]]}`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d，应为 true/1", ok, len(triples))
	}
	if triples[0].Subject != "Alice" {
		t.Errorf("Subject 应为 Alice，实际 %s", triples[0].Subject)
	}
}

// TestParseTriples_缺triples键 对齐 test_parse_triples_missing_triples_key_fails
func TestParseTriples_缺triples键(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"named_entities": ["Alice", "Bob"]}`, "d1", "c1")
	if ok {
		t.Fatal("缺 triples 键应为 false")
	}
	if len(triples) != 0 {
		t.Errorf("失败时应返回空列表，实际 %d", len(triples))
	}
}

// TestParseTriples_invalid混合 对齐 test_parse_triples_invalid_items_ignored
func TestParseTriples_invalid混合(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"triples": [["a", "b", "c"], ["x"], {"bad": 1}, ["y", ["nested"], "z"]]}`, "d1", "c1")
	if !ok {
		t.Fatal("混合场景应 ok=true（有 1 条合格）")
	}
	if len(triples) != 1 {
		t.Fatalf("应保留 1 条合格三元组，实际 %d", len(triples))
	}
	if triples[0].Subject != "a" {
		t.Errorf("唯一合格项 Subject 应为 a，实际 %s", triples[0].Subject)
	}
}

// TestParseTriples_全invalid 对齐 test_parse_triples_all_invalid_fails
func TestParseTriples_全invalid(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"triples": [["x"], {"bad": 1}]}`, "d1", "c1")
	if ok {
		t.Fatal("全 invalid 应为 false")
	}
	if len(triples) != 0 {
		t.Errorf("失败时应返回空列表，实际 %d", len(triples))
	}
}

// ──────────────────────────── parseTriples Go 增强 ────────────────────────────

// TestParseTriples_metadata注入 测试 doc_id/chunk_id 元数据注入（Go 增强）
func TestParseTriples_metadata注入(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["a", "b", "c"]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d", ok, len(triples))
	}
	if triples[0].Metadata["doc_id"] != "d1" || triples[0].Metadata["chunk_id"] != "c1" {
		t.Errorf("Metadata 应注入 doc_id/chunk_id，实际 %v", triples[0].Metadata)
	}
}

// TestParseTriples_fence剥离 测试 3 行 markdown fence 剥离（Go 增强 §7.2-6）
func TestParseTriples_fence剥离(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	content := "```json\n{\"triples\": [[\"a\", \"b\", \"c\"]]}\n```"
	triples, ok := e.parseTriples(content, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("fence 剥离后应解析成功，ok=%v len=%d", ok, len(triples))
	}
}

// TestParseTriples_fence两行不剥 测试 2 行 fence 不剥（Go 增强 §7.2-6）
func TestParseTriples_fence两行不剥(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	// len(lines)==2 不满足 >2，走原文 → 修复器无法解析 → false
	_, ok := e.parseTriples("```\n```", "d1", "c1")
	if ok {
		t.Fatal("2 行 fence 不剥离应解析失败")
	}
}

// TestParseTriples_极坏输入 测试空串/纯文本输入（Go 增强 §7.2-7）
func TestParseTriples_极坏输入(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	for _, content := range []string{"", "Invalid JSON response", "   "} {
		if _, ok := e.parseTriples(content, "d1", "c1"); ok {
			t.Errorf("极坏输入 %q 不应解析成功", content)
		}
	}
}

// TestParseTriples_空triples列表 测试空列表返回 true（Python ([], True) 分支）
func TestParseTriples_空triples列表(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"triples": []}`, "d1", "c1")
	if !ok {
		t.Fatal("空 triples 列表应为 true（Python ([], True)）")
	}
	if len(triples) != 0 {
		t.Errorf("应为空列表，实际 %d", len(triples))
	}
}

// TestParseTriples_字段strip 测试 str(x).strip() 两步语义（Go 增强）
func TestParseTriples_字段strip(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[[" a ", " b ", " c "]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d", ok, len(triples))
	}
	if triples[0].Subject != "a" || triples[0].Predicate != "b" || triples[0].Object != "c" {
		t.Errorf("三字段应 strip，实际 %+v", triples[0])
	}
}

// TestParseTriples_数字类型字段 测试数字字段经 pyStr 字符串化（Go 增强 §7.2-5）
//
// 合法 JSON 主路径：jsonrepair 短路解码产出 json.Number——pyStr 整数字面
// 原样输出（对齐 Python json.loads int → str）
func TestParseTriples_数字类型字段(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["Magic Johnson", "draft pick number", 1]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d", ok, len(triples))
	}
	if triples[0].Object != "1" {
		t.Errorf("数字 1 应字符串化为 \"1\"，实际 %q", triples[0].Object)
	}
}

// TestParseTriples_浮点数字段 测试浮点数字段（json.Number → Atoi 失败 →
// pyFloatStr 记法还原，对齐 Python str(1.5)="1.5"）
func TestParseTriples_浮点数字段(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["a", "value", 1.5]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d", ok, len(triples))
	}
	if triples[0].Object != "1.5" {
		t.Errorf("数字 1.5 应字符串化为 \"1.5\"，实际 %q", triples[0].Object)
	}
}

// ──────────────────────────── pyStr 测试（Go 增强 §7.2-5）────────────────────────────

// TestPyStr_类型字符串化 对齐 Python str() 逐类型语义
func TestPyStr_类型字符串化(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{1.0, "1.0"},            // float64 整数值补 .0
		{1e-4, "0.0001"},        // f 记法下界
		{1e16, "1e+16"},         // e 记法上界
		{true, "True"},          // Python str(True)
		{false, "False"},        // Python str(False)
		{42, "42"},              // int
		{"abc", "abc"},          // string 原样
		{nil, "None"},           // 不可达防御分支
	}
	for _, c := range cases {
		if got := pyStr(c.in); got != c.want {
			t.Errorf("pyStr(%v) 应为 %q，实际 %q", c.in, c.want, got)
		}
	}
}

// TestPyStr_jsonNumber 对齐合法 JSON 路径的 json.Number 字面输出（探针实测：
// json.Valid 短路路径经 UseNumber 产出 json.Number，三元组内数字以此形态到达）
func TestPyStr_jsonNumber(t *testing.T) {
	cases := []struct {
		in   json.Number
		want string
	}{
		{json.Number("1"), "1"},         // Python json 解码 int → str(1)="1"
		{json.Number("1.0"), "1.0"},     // Python float → str(1.0)="1.0"
		{json.Number("1.5"), "1.5"},     // 最短往返同形
		{json.Number("10000000000000000"), "10000000000000000"}, // int 超界仍整数字面（Python int 无界）
		{json.Number("1e16"), "1e+16"},  // Python float(1e16) → str → "1e+16"
		{json.Number("0.0001"), "0.0001"},
	}
	for _, c := range cases {
		if got := pyStr(c.in); got != c.want {
			t.Errorf("pyStr(%v) 应为 %q，实际 %q", c.in, c.want, got)
		}
	}
}
```

顶部 import 追加 `"encoding/json"`。

注意 pyStr 的 json.Number 语义：Python `json.loads` 把 `1` 解为 int、`1.0`/`1e16` 解为 float——str() 输出不同记法。json.Number 保留原字面无法区分 int/float 意图，Go 侧处理：**先试 strconv.Atoi（整数字面原样输出），失败再 ParseFloat 走 pyFloatStr 记法还原**——`"1"`→"1"、`"1.0"`→"1.0"、`"1e16"`→ParseFloat→pyFloatStr→"1e+16"、`"10000000000000000"`→Atoi 成功（Go int64 界内）→原样输出（Python int 无界 str 同为原样）。

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestParseTriples|TestPyStr' -v 2>&1 | tail -5
```

Expected: FAIL — `e.parseTriples undefined` / `pyStr undefined`

- [ ] **Step 3: Write minimal implementation**

triple_extractor.go 追加（结构体区块加 chunkResult；非导出函数区块加 parseTriples/pyStr/metadataTitle）：

```go
// chunkResult 单 chunk 抽取结果（方案 A 预分配槽位载体）。
type chunkResult struct {
	// triples 抽取成功的三元组
	triples []common.Triple
	// err 抽取失败错误
	err error
}
```

```go
// parseTriples 解析 LLM 输出为 Triple 列表。
//
// 返回 (triples, parseSuccess)：硬解析失败 false；空三元组列表 true
// （对齐 Python ([], True)）；全 invalid → (空, false) 触发上层 155507。
//
// Python: triple_extractor.py TripleExtractor._parse_triples（L265-338）
func (e *TripleExtractor) parseTriples(content, docID, chunkID string) ([]common.Triple, bool) {
	triples := []common.Triple{}

	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	result, err := jsonrepair.RepairWithOptions(content, jsonrepair.RepairOptions{})
	if err != nil {
		// 对齐 Python: logger.error("Failed to parse triples from content: %s. Content: %s", e, content[:200])
		logger.Error(logComponent).
			Err(err).
			Str("content", truncateContent(content, 200)).
			Msg("Failed to parse triples from content")
		return []common.Triple{}, false
	}

	var tripleList []any
	switch parsed := result.Value.(type) {
	case map[string]any:
		triplesField, exists := parsed["triples"]
		if !exists {
			return []common.Triple{}, false
		}
		list, isList := triplesField.([]any)
		if !isList {
			return []common.Triple{}, false
		}
		tripleList = list
	case []any:
		tripleList = parsed
	default:
		return []common.Triple{}, false
	}

	if len(tripleList) == 0 {
		return []common.Triple{}, true
	}

	invalidCount := 0
	for _, tripleData := range tripleList {
		arr, isArr := tripleData.([]any)
		if !isArr {
			// 对齐 isinstance(triple_data, (list, tuple))：JSON 解码无 tuple 形态
			invalidCount++
			continue
		}
		if len(arr) < 3 {
			invalidCount++
			continue
		}
		head := arr[:3]
		if hasInvalidHeadItem(head) {
			invalidCount++
			continue
		}
		triples = append(triples, common.Triple{
			Subject:   strings.TrimSpace(pyStr(head[0])),
			Predicate: strings.TrimSpace(pyStr(head[1])),
			Object:    strings.TrimSpace(pyStr(head[2])),
			// 对齐 metadata={"doc_id": doc_id, "chunk_id": chunk_id}
			Metadata: map[string]any{"doc_id": docID, "chunk_id": chunkID},
		})
	}

	if invalidCount > 0 {
		// 对齐 Python: logger.warning("Ignored %d invalid triples for chunk %s during parsing", invalid_count, chunk_id)
		logger.Warn(logComponent).
			Int("invalid_count", invalidCount).
			Str("chunk_id", chunkID).
			Msg("Ignored invalid triples for chunk during parsing")
	}

	return triples, len(triples) > 0
}

// hasInvalidHeadItem 检查三元组头部是否含嵌套容器或 null
//（对齐 any(isinstance(x, (list, tuple, dict)) or x is None for x in head)）。
func hasInvalidHeadItem(head []any) bool {
	for _, x := range head {
		switch x.(type) {
		case []any, map[string]any, nil:
			return true
		}
	}
	return false
}

// pyStr 对齐 Python str() 的字符串化（13.13 cellStr 同源语义，包内独立实现）。
//
// 仅做类型字符串化，不做 strip（对齐 str() 单步；strip 由 parseTriples
// 统一 TrimSpace 三字段）。
//
// jsonrepair Value 数字三态全覆盖（探针实测）：
//   - json.Number（合法 JSON 短路路径）：先 Atoi 整数字面原样，失败
//     ParseFloat 走 pyFloatStr 记法还原（对齐 Python json.loads 的
//     int/float 分野：str(1)="1"、str(1.0)="1.0"、str(1e16)="1e+16"）
//   - float64（深修复路径小数）：pyFloatStr
//   - int（深修复路径整数）：strconv.Itoa
//
// bool → "True"/"False"；string 原样；nil 不可达（hasInvalidHeadItem
// 已滤 None）——防御保留返回 "None"；其余 fmt.Sprint。
func pyStr(v any) string {
	switch val := v.(type) {
	case nil:
		return "None"
	case string:
		return val
	case bool:
		if val {
			return "True"
		}
		return "False"
	case json.Number:
		// 整数字面优先（Python int 无界，str 原样）；浮点字面走记法还原
		if i, err := strconv.Atoi(val.String()); err == nil {
			_ = i
			return val.String()
		}
		if f, err := val.Float64(); err == nil {
			return pyFloatStr(f)
		}
		return val.String()
	case float64:
		return pyFloatStr(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprint(v)
	}
}

// pyFloatStr 还原 Python str(float) 记法（13.13 excel_value.go 同源语义，
// 包内独立实现）。abs ∈ [1e-4, 1e16) 用 f 记法（整数值补 ".0"），
// 其余 e 记法；nan/inf 特殊字面。
func pyFloatStr(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	abs := math.Abs(f)
	if abs == 0 || (abs >= 1e-4 && abs < 1e16) {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	return strconv.FormatFloat(f, 'e', -1, 64)
}

// metadataTitle 读取 chunk 元数据 title（对齐 chunk.metadata.get("title", "")）。
func metadataTitle(metadata map[string]any) string {
	if v, ok := metadata["title"]; ok {
		if s, isStr := v.(string); isStr {
			return s
		}
	}
	return ""
}

// truncateContent 截取字符串前 n 个字符用于日志输出（content[:200] 对齐）。
func truncateContent(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
```

import 追加：

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestParseTriples|TestPyStr' -v 2>&1 | tail -8
```

Expected: 全 PASS（约 20 用例）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor.go internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go
git commit -m "feat(13.17): parseTriples + pyStr/pyFloatStr（对齐 _parse_triples 7 例 1:1 + Go 增强）"
```

---

### Task 6: Extract 并发编排 + extractChunk + Process

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor.go`
- Test: `internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go`（追加）

- [ ] **Step 1: Write the failing test**

triple_extractor_test.go 追加。先加可编程 mock（覆写 Invoke 的 stub 嵌入模式）：

```go
// captionTripleClient 可编程 mock：按序返回预置响应或错误，记录 Invoke 参数。
//
// 并发安全设计：responses/errs 构造后只读；callCount/current/concurrentPeak
// 走 atomic；lastMessages/lastTemperature 经 mu 保护（多 chunk 并发 Invoke
// 写入不 race，go test -race 下安全）。
type captionTripleClient struct {
	stubTripleClient
	// mu 保护 last* 字段（并发 Invoke 写入）
	mu sync.Mutex
	// responses 按调用序预置响应（content 字段）
	responses []string
	// errs 按调用序预置错误（与 responses 索引对应，优先于 content）
	errs []error
	// callCount Invoke 调用计数
	callCount int32
	// current 当前并发数（进入 +1 / 退出 -1）
	current int32
	// concurrentPeak 并发峰值（CAS 更新）
	concurrentPeak int32
	// lastTemperature 最近一次 Invoke 的温度（NewInvokeParams 提取）
	lastTemperature *float64
	// lastMessages 最近一次 Invoke 的消息
	lastMessages model_clients.MessagesParam
}

// Invoke 按预置返回；耗尽时返回错误。
func (c *captionTripleClient) Invoke(_ context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	cur := atomic.AddInt32(&c.current, 1)
	for {
		peak := atomic.LoadInt32(&c.concurrentPeak)
		if cur <= peak || atomic.CompareAndSwapInt32(&c.concurrentPeak, peak, cur) {
			break
		}
	}
	defer atomic.AddInt32(&c.current, -1)

	idx := int(atomic.AddInt32(&c.callCount, 1)) - 1
	c.mu.Lock()
	c.lastMessages = messages
	c.lastTemperature = model_clients.NewInvokeParams(opts...).Temperature
	c.mu.Unlock()

	if c.errs != nil && idx < len(c.errs) {
		if c.errs[idx] != nil {
			return nil, c.errs[idx]
		}
	}
	if idx < len(c.responses) {
		return llmschema.NewAssistantMessage(c.responses[idx]), nil
	}
	return nil, errors.New("mock responses exhausted")
}
```

1:1 用例（对齐 test_extract_* 4 例）：

```go
// newExtractTestModelWithMock 构造带可编程 mock 的 Model 并返回 mock 引用。
func newExtractTestModelWithMock(t *testing.T, responses []string, errs []error) (*llm.Model, *captionTripleClient) {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	mock := &captionTripleClient{responses: responses, errs: errs}
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(mock))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model, mock
}

// TestExtract_多分块 对齐 test_extract_multiple_chunks（invoke 计数=2）
//
// 注意：多 chunk 并发下 triples 顺序 = chunk 序 × 每 chunk 内列表序（方案 A
// 扫描合并），两 chunk 各 2 条共 4 条。
func TestExtract_多分块(t *testing.T) {
	content := `{"triples": [["Alice", "knows", "Bob"], ["Bob", "works_at", "Company"]]}`
	model, mock := newExtractTestModelWithMock(t, []string{content, content}, nil)
	ex := NewTripleExtractor(model, "test-model", WithTripleMaxConcurrent(2))
	chunks := []common.TextChunk{
		{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"},
		{ID: "2", Text: "Charlie knows David", DocID: "doc_1"},
	}
	triples, err := ex.Extract(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Extract 不应报错: %v", err)
	}
	if got := atomic.LoadInt32(&mock.callCount); got != 2 {
		t.Errorf("invoke 应调用 2 次，实际 %d", got)
	}
	if len(triples) != 4 {
		t.Errorf("应抽取 4 条三元组（2 chunk × 2 条），实际 %d", len(triples))
	}
	// 首条应来自 chunk 1（按 chunk 序合并）
	if triples[0].Subject != "Alice" {
		t.Errorf("首条三元组 Subject 应为 Alice（chunk 序），实际 %q", triples[0].Subject)
	}
	// metadata 注入 doc_id/chunk_id
	if triples[0].Metadata["chunk_id"] != "1" || triples[0].Metadata["doc_id"] != "doc_1" {
		t.Errorf("metadata 应注入 doc_id=doc_1 chunk_id=1，实际 %v", triples[0].Metadata)
	}
}

// TestExtract_invoke异常 对齐 test_extract_with_exception（断言 155507 + 消息包含）
func TestExtract_invoke异常(t *testing.T) {
	model, _ := newExtractTestModelWithMock(t, nil, []error{errors.New("429 too many requests")})
	ex := NewTripleExtractor(model, "test-model")
	chunks := []common.TextChunk{{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"}}
	_, err := ex.Extract(context.Background(), chunks)
	if err == nil {
		t.Fatal("invoke 异常应报错")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应为 BaseError，实际 %T", err)
	}
	if be.Code() != 155507 {
		t.Errorf("错误码应为 155507，实际 %d", be.Code())
	}
	if !strings.Contains(be.Message(), "429 too many requests") {
		t.Errorf("消息应包含 \"429 too many requests\"，实际 %q", be.Message())
	}
}

// TestExtract_invalidJSON 对齐 test_extract_invalid_json（"parsed" in msg）
func TestExtract_invalidJSON(t *testing.T) {
	model, _ := newExtractTestModelWithMock(t, []string{"Invalid JSON response"}, nil)
	ex := NewTripleExtractor(model, "test-model")
	chunks := []common.TextChunk{{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"}}
	_, err := ex.Extract(context.Background(), chunks)
	if err == nil {
		t.Fatal("invalid JSON 应报错")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应为 BaseError，实际 %T", err)
	}
	if be.Code() != 155507 {
		t.Errorf("错误码应为 155507，实际 %d", be.Code())
	}
	// 对齐 assert "parsed" in (exc_info.value.message or "").lower()
	if !strings.Contains(strings.ToLower(be.Message()), "parsed") {
		t.Errorf("消息应包含 \"parsed\"（lower），实际 %q", be.Message())
	}
}

// TestExtract_空chunks 对齐 test_extract_empty_chunks（返回空且 invoke 未调）
func TestExtract_空chunks(t *testing.T) {
	model, mock := newExtractTestModelWithMock(t, []string{"unused"}, nil)
	ex := NewTripleExtractor(model, "test-model")
	triples, err := ex.Extract(context.Background(), []common.TextChunk{})
	if err != nil {
		t.Fatalf("空 chunks 不应报错: %v", err)
	}
	if len(triples) != 0 {
		t.Errorf("应返回空列表，实际 %d", len(triples))
	}
	if got := atomic.LoadInt32(&mock.callCount); got != 0 {
		t.Errorf("invoke 不应被调用，实际 %d 次", got)
	}
}
```

Go 增强用例（§7.2 之 1/2/3/8）。本步测试文件 import 块追加四个（`sync`/`sync/atomic`/`common`/`exception`）：

```go
// triple_extractor_test.go import 块追加后完整形态：
import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)
```

```go
// TestExtract_nil客户端 对齐 §8-5 路径（nil → 155507 硬失败）
func TestExtract_nil客户端(t *testing.T) {
	ex := NewTripleExtractor(nil, "test-model")
	chunks := []common.TextChunk{{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"}}
	_, err := ex.Extract(context.Background(), chunks)
	if err == nil {
		t.Fatal("nil llmClient 应报错")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Code() != 155507 {
		t.Fatalf("应为 155507 BaseError，实际 %v", err)
	}
}

// TestExtract_首错按chunk序 §7.2-2：chunk[0] 慢成功 + chunk[1] 快失败 → 首错取 [1]
//
// maxConcurrent=1 强制串行：chunk0 先执行成功落槽，chunk1 后执行失败。
// 扫描按 chunk 序 → 首错 = chunk1 的 155507（chunk0 成功结果随 raise 丢弃）。
func TestExtract_首错按chunk序(t *testing.T) {
	model, _ := newExtractTestModelWithMock(t,
		[]string{`{"triples": [["a", "b", "c"]]}`},
		[]error{nil, errors.New("boom")},
	)
	ex := NewTripleExtractor(model, "test-model", WithTripleMaxConcurrent(1))
	chunks := []common.TextChunk{
		{ID: "c0", Text: "first", DocID: "doc"},
		{ID: "c1", Text: "second", DocID: "doc"},
	}
	_, err := ex.Extract(context.Background(), chunks)
	if err == nil {
		t.Fatal("chunk1 失败应报错")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Code() != 155507 {
		t.Fatalf("应为 155507 BaseError，实际 %v", err)
	}
	if !strings.Contains(be.Message(), "c1") {
		t.Errorf("首错消息应含 chunk1 的 id \"c1\"，实际 %q", be.Message())
	}
}

// TestExtract_temperature透传 §7.2-3：mock 记录 InvokeParams 断言温度
//
// 单 chunk 场景（lastTemperature 断言无歧义）
func TestExtract_temperature透传(t *testing.T) {
	content := `{"triples": [["a", "b", "c"]]}`
	// 默认 0.0
	model, mock := newExtractTestModelWithMock(t, []string{content}, nil)
	ex := NewTripleExtractor(model, "test-model")
	_, _ = ex.Extract(context.Background(), []common.TextChunk{{ID: "1", Text: "t", DocID: "d"}})
	mock.mu.Lock()
	lt := mock.lastTemperature
	mock.mu.Unlock()
	if lt != nil && *lt != 0.0 {
		t.Errorf("默认温度应为 0.0，实际 %v", *lt)
	}
	// 自定义 0.7
	model2, mock2 := newExtractTestModelWithMock(t, []string{content}, nil)
	ex2 := NewTripleExtractor(model2, "test-model", WithTripleTemperature(0.7))
	_, _ = ex2.Extract(context.Background(), []common.TextChunk{{ID: "1", Text: "t", DocID: "d"}})
	mock2.mu.Lock()
	lt2 := mock2.lastTemperature
	mock2.mu.Unlock()
	if lt2 == nil || *lt2 != 0.7 {
		t.Errorf("自定义温度应为 0.7，实际 %v", lt2)
	}
}

// TestExtract_maxConcurrent串行性 §7.2-8：信号量并发峰值=1
func TestExtract_maxConcurrent串行性(t *testing.T) {
	content := `{"triples": [["a", "b", "c"]]}`
	responses := []string{content, content, content, content}
	model, mock := newExtractTestModelWithMock(t, responses, nil)
	ex := NewTripleExtractor(model, "test-model", WithTripleMaxConcurrent(1))
	chunks := []common.TextChunk{
		{ID: "1", Text: "a", DocID: "d"},
		{ID: "2", Text: "b", DocID: "d"},
		{ID: "3", Text: "c", DocID: "d"},
		{ID: "4", Text: "e", DocID: "d"},
	}
	_, err := ex.Extract(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Extract 不应报错: %v", err)
	}
	if peak := atomic.LoadInt32(&mock.concurrentPeak); peak != 1 {
		t.Errorf("maxConcurrent=1 时并发峰值应为 1，实际 %d", peak)
	}
}
```

```go
// TestProcess_转发 对齐 Process 契约（Python 基类一行转发）
func TestProcess_转发(t *testing.T) {
	content := `{"triples": [["a", "b", "c"]]}`
	model, mock := newExtractTestModelWithMock(t, []string{content}, nil)
	ex := NewTripleExtractor(model, "test-model")
	chunks := []common.TextChunk{{ID: "1", Text: "t", DocID: "d"}}
	triples, err := ex.Process(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Process 不应报错: %v", err)
	}
	if len(triples) != 1 || atomic.LoadInt32(&mock.callCount) != 1 {
		t.Errorf("Process 应转发 Extract 产出 1 条，实际 %d", len(triples))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -run 'TestExtract|TestProcess' -v 2>&1 | tail -5
```

Expected: FAIL — `e.Extract undefined` / `e.Process undefined`

- [ ] **Step 3: Write minimal implementation**

triple_extractor.go 导出函数区块追加（完整实现——方案 A 并发编排）：

```go
// Extract 并发抽取 chunks 的三元组（方案 A：预分配槽位 + 信号量 + 收齐扫描）。
//
// 语义逐字对齐 Python asyncio.gather(return_exceptions=True)：
//   - 全部任务跑完才扫描（无 fail-fast 取消）
//   - 成功结果按 chunk 序合并
//   - 首错按 chunk 序（非完成序）选取
//   - BaseError 原样返回（对齐 isinstance first_error BaseError raise）
//   - 首错时成功结果不返回（对齐 Python extend 后随 raise 丢弃——偏差 §8-7）
//
// Python: triple_extractor.py TripleExtractor.extract（L47-145）
func (e *TripleExtractor) Extract(ctx context.Context, chunks []common.TextChunk, _ ...ExtractOption) ([]common.Triple, error) {
	results := make([]chunkResult, len(chunks)) // 空 chunks → 空 make，扫描零循环直接返回
	var wg sync.WaitGroup
	for i := range chunks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// 对齐 async with self.limiter（包住 invoke+解析全流程）
			e.limiter <- struct{}{}
			defer func() { <-e.limiter }()
			results[i] = e.extractChunk(ctx, chunks[i]) // 各写各的槽位，无竞争
		}(i)
	}
	wg.Wait() // 收齐全部（gather 全跑完语义）

	allTriples := make([]common.Triple, 0)
	var firstErr error
	var firstErrChunkID string
	for i := range chunks { // 按 chunk 序扫描（对齐 for idx, result in enumerate）
		if results[i].err != nil {
			// 对齐 Python: logger.error(f"Task failed for chunk {chunk_id}: {result}")
			logger.Error(logComponent).
				Str("chunk_id", chunks[i].ID).
				Err(results[i].err).
				Msg("Task failed for chunk")
			if firstErr == nil {
				firstErr = results[i].err
				firstErrChunkID = chunks[i].ID
			}
			continue
		}
		allTriples = append(allTriples, results[i].triples...)
	}

	if firstErr != nil {
		var be *exception.BaseError
		if errors.As(firstErr, &be) { // isinstance(first_error, BaseError): raise 原样
			return nil, firstErr
		}
		return nil, exception.BuildError(
			exception.StatusRetrievalKbTripleExtractionProcessError,
			exception.WithParam("error_msg", firstErrChunkID+": "+firstErr.Error()),
			exception.WithCause(firstErr),
		)
	}
	return allTriples, nil
}

// Process 处理文本分块（Processor 契约兼容入口），转发 Extract。
//
// Python: base.py Extractor.process → self.extract(chunks, **kwargs)
func (e *TripleExtractor) Process(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error) {
	return e.Extract(ctx, chunks, opts...)
}
```

非导出函数区块追加 extractChunk：

```go
// extractChunk 单 chunk 抽取：构建提示词 → Invoke → 解析。
//
// 错误统一 wrap 155507（对齐 Python except Exception as e → build_error）。
// nil llmClient → 155507 硬失败（Python None.invoke AttributeError 被 wrap
// 的等价路径——偏差 §8-5）。
//
// Python: triple_extractor.py TripleExtractor.extract 内 _extract_chunk（L70-115）
func (e *TripleExtractor) extractChunk(ctx context.Context, chunk common.TextChunk) chunkResult {
	if e.llmClient == nil {
		return chunkResult{err: exception.BuildError(
			exception.StatusRetrievalKbTripleExtractionProcessError,
			exception.WithParam("error_msg", chunk.ID+": llm client is nil"),
		)}
	}

	// 对齐 prompt = self._build_prompt(chunk.text, chunk.metadata.get("title", ""))
	prompt := e.buildPrompt(chunk.Text, metadataTitle(chunk.Metadata))
	messages := model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt))

	resp, err := e.llmClient.Invoke(ctx, messages,
		model_clients.WithInvokeTemperature(e.temperature))
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to extract triples from chunk {chunk.id_}: {e}")
		logger.Error(logComponent).
			Str("chunk_id", chunk.ID).
			Err(err).
			Msg("Failed to extract triples from chunk")
		return chunkResult{err: exception.BuildError(
			exception.StatusRetrievalKbTripleExtractionProcessError,
			exception.WithParam("error_msg", chunk.ID+": "+err.Error()),
			exception.WithCause(err),
		)}
	}

	var content string
	if resp != nil {
		content = resp.Content.Text()
	}
	triples, ok := e.parseTriples(content, chunk.DocID, chunk.ID)
	if !ok {
		return chunkResult{err: exception.BuildError(
			exception.StatusRetrievalKbTripleExtractionProcessError,
			exception.WithParam("error_msg", chunk.ID+": LLM response could not be parsed as valid triple JSON"),
		)}
	}
	return chunkResult{triples: triples}
}
```

import 更新（sync + errors + model_clients + llmschema + exception）：

```go
import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

- [ ] **Step 4: Run test to verify it passes（含 race 检测）**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/extractor/ -v 2>&1 | tail -8 && go test -race ./internal/agentcore/retrieval/indexing/processor/extractor/ 2>&1 | tail -3
```

Expected: 全 PASS 且 race 检测零报告（槽位写入互不竞争；mock last* 字段经 mu 保护）

- [ ] **Step 5: 取消编译期断言注释**

Task 3 中注释的 `// var _ Extractor = (*TripleExtractor)(nil)` 取消注释（Extract/Process 已实现，断言恢复编译期接口检查）。重跑：

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/indexing/processor/extractor/
```

Expected: 编译通过零输出。

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor.go internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go
git commit -m "feat(13.17): Extract 方案A并发编排 + extractChunk + Process 转发（gather 全跑完语义 1:1）"
```

---

### Task 7: 覆盖率 + 全量回归

**Files:**
- Test: `internal/agentcore/retrieval/indexing/processor/extractor/`（可能补测试）

- [ ] **Step 1: 跑包覆盖率**

```bash
cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/retrieval/indexing/processor/extractor/ ./internal/agentcore/retrieval/common/ 2>&1 | tail -4
```

Expected: extractor ≥90% / common ≥90%（spec §7.3 目标）

- [ ] **Step 2: 未达标补测**

若 extractor <90%，优先补覆盖路径：`hasInvalidHeadItem` 的 map 嵌套分支（`{"bad": 1}` 混入用例已覆盖 map 分支）、`pyStr` 的 int64/default 分支、`truncateContent` 截断分支（直接调用非导出函数）。示例补测：

```go
// TestTruncateContent_截断分支（覆盖率补充：直接调用非导出函数走 >200 分支；
// 注意探针实测纯文本经 repair 成 string 走 default false——不触发 repair err
// 路径，故 truncateContent 的截断分支直接单测）
func TestTruncateContent_截断分支(t *testing.T) {
	s := strings.Repeat("x", 250)
	got := truncateContent(s, 200)
	if len(got) != 203 { // 200 字符 + "..." 
		t.Errorf("截断结果应为 200+3 字符，实际 %d", len(got))
	}
	if got[:200] != s[:200] || got[200:] != "..." {
		t.Error("截断结果应保留前 200 字符 + \"...\" 后缀")
	}
	// ≤n 原样返回
	if got := truncateContent("short", 200); got != "short" {
		t.Errorf("短字符串应原样返回，实际 %q", got)
	}
}

// TestPyStr_int64字段（覆盖率补充：深修复路径 int64 分支防御）
func TestPyStr_int64字段(t *testing.T) {
	if got := pyStr(int64(99)); got != "99" {
		t.Errorf("pyStr(int64(99)) 应为 \"99\"，实际 %q", got)
	}
}
```

- [ ] **Step 3: 全量回归（CI 同款标签）**

```bash
cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "FAIL|ok " | tail -20
```

Expected: 零 FAIL（testing-build-tags 记忆：必须带 CI 同款标签否则 memory 包 FTS 假失败）

- [ ] **Step 4: Commit（如有补测）**

```bash
git add internal/agentcore/retrieval/indexing/processor/extractor/triple_extractor_test.go
git commit -m "test(13.17): 覆盖率补充至 ≥90%（截断/防御分支）"
```

---

### Task 8: doc.go + IMPLEMENTATION_PLAN.md 回填

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/extractor/doc.go`
- Modify: `internal/agentcore/retrieval/common/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`（13.17 行 + 7.36 行）

- [ ] **Step 1: 创建 extractor/doc.go**

```go
// Package extractor 提供知识抽取抽象与 LLM 三元组抽取实现：把非结构化
// chunk 文本抽取为结构化知识（RDF 风格三元组），供图谱索引消费。
//
// 知识检索入库管线位置：Chunker 产 TextChunk 后，本包 Extractor 把
// 文本抽成 (subject, predicate, object) 三元组（13.17 TripleExtractor，
// use_graph=true 时启用），三元组转 TextChunk 再走同一 Indexer 入库
// kb_{id}_triples——是 GraphRAG 图谱索引的写入原料。读取侧消费方为
// 13.23 GraphRetriever（beam search）与 13.28 GraphKnowledgeBase
// （add_documents 中调用 extractor.extract）。
//
// 并发模型（方案 A，对齐 Python asyncio.gather(return_exceptions=True)）：
// goroutine 预分配槽位各写 results[i] + buffered chan 信号量
// （asyncio.Semaphore 等价）+ wg 收齐后按 chunk 序扫描——全部任务跑完
// 才扫、首错按 chunk 序选、BaseError 原样返回。该 gather 模式在
// 13.23/13.25/13.27 共 5 处再现，本包实现为复用资产。
//
// JSON 解析走 jsonrepair.RepairWithOptions 一步取 result.Value（对齐
// Python repair_json(return_objects=True)）；提示词模板逐字复制 Python
// 源（{{/}} 归一为 {/}）经 strings.NewReplacer 渲染唯二命名槽。
//
// 文件目录：
//
//	extractor/
//	├── doc.go               # 包文档
//	├── base.go              # Extractor 接口 + ExtractOption（13.17）
//	└── triple_extractor.go  # TripleExtractor：LLM OpenIE 三元组抽取（13.17）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/indexing/processor/extractor/
//	  （base.py + triple_extractor.py）
//	openjiuwen/core/retrieval/common/triple.py（Triple 数据模型，Go 侧在
//	retrieval/common/triple.go）
//
// 核心类型索引：
//
//	Extractor      — 抽取器抽象接口（Extract + Process 双方法）
//	ExtractOption  — Extract 可选参数（**kwargs 收纳）
//	TripleExtractor — 基于 LLM 的 OpenIE 三元组抽取器
package extractor
```

- [ ] **Step 2: 更新 common/doc.go 文件目录树**

在文件目录区块的 `├── document.go` 行后插入：

```go
//	├── triple.go        # Triple 三元组数据模型（OpenIE 抽取产物）
```

（位于 document.go 与 callbacks.go 之间，排序按 doc.go → 核心类型 → 辅助 → 配置。）

核心类型索引区块追加：

```go
//	Triple            — OpenIE 三元组数据模型
```

（追加在 TextChunk 行后。）

- [ ] **Step 3: 回填 IMPLEMENTATION_PLAN.md 13.17 行**

先读当前行：

```bash
grep -n "13.17" /home/opensource/uapclaw-gateway/IMPLEMENTATION_PLAN.md | head -3
```

把 13.17 行 `☐`→`✅`，产出描述对齐相邻已完成行格式（参考 13.16 行样式），附 spec 链接。示例（以实际表格列结构为准）：

```markdown
| 13.17 | ✅ | TripleExtractor 三元组抽取 | internal/agentcore/retrieval/indexing/processor/extractor/（Extractor 接口+TripleExtractor+common/triple.go） | OpenIE LLM 抽取 chunk→RDF三元组，方案A gather 全跑完语义，jsonrepair 一步解析；spec: docs/superpowers/specs/2026-10-27-tripleextractor-13.17-design.md |
```

**禁止**：不触碰 13.19-13.28 十行已登记内容、13.1-13.16/13.18 既有行（spec §9-1 红线）。

- [ ] **Step 4: 回填 7.36 行 ⤵️ 表述**

```bash
grep -n "7.36" /home/opensource/uapclaw-gateway/IMPLEMENTATION_PLAN.md | head -3
```

7.36 行 `⤵️` 表述补"含 2026-10-27 扩充的检索查询管线 13.19-13.28"（spec §9-2）。

- [ ] **Step 5: 编译验证 + 全量回归**

```bash
cd /home/opensource/uapclaw-gateway && go build ./... && GOPROXY=https://goproxy.cn,direct CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/... 2>&1 | grep -E "FAIL|ok " | tail -10
```

Expected: 零 FAIL

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/extractor/doc.go internal/agentcore/retrieval/common/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(13.17): extractor 包 doc.go + common doc.go 目录树 + 计划回填 ✅"
```

注意（feedback 记忆 feedback-git-am-stowaway-commits）：本任务工作区可能混有其他任务未提交改动——**commit 一律显式 `git add <文件>`，禁止 `-am`/-A**，提交后 `git show --stat HEAD` 核对无搭车文件。

---

## 附录：Python 基线 → Go 用例映射总表

| Python 用例 | Go 测试 | Task |
|---|---|---|
| test_triple_extractor.py test_init / test_init_with_defaults | TestTripleExtractor_构造 / TestTripleExtractor_默认值构造 | 3 |
| test_extract_multiple_chunks | TestExtract_多分块（计数=2+三元组数） | 6 |
| test_extract_with_exception | TestExtract_invoke异常（155507+消息包含） | 6 |
| test_extract_invalid_json | TestExtract_invalidJSON（"parsed" in msg） | 6 |
| test_extract_empty_chunks | TestExtract_空chunks（invoke 未调） | 6 |
| parse 7 例（json_array/extra_fields/wrapped_dict/prompt_shape/missing_key/invalid_items/all_invalid） | TestParseTriples_顶层数组/多余字段截取/包裹dict/含named_entities/缺triples键/invalid混合/全invalid | 5 |
| test_base.py test_extract / test_process | TestExtractor_Extract / TestExtractor_Process | 2 |
| test_base.py test_cannot_instantiate | 归档 spec §8-4（编译期天然防） | — |
| test_triple.py 4 例 | TestTriple_创建/带metadata/全字段 + JSON 序列化增强 | 1 |
| Go 增强 §7.2 8 项 | nil客户端/首错chunk序/temperature透传/prompt渲染4例/pyStr边界含jsonNumber三态/fence边界/极坏输入/maxConcurrent串行 | 3-6 |

## 附录：实施注意事项（从 spec 与记忆提炼）

1. **提示词逐字铁律**：Task 4 的模板常量从本计划直接复制，禁止翻译改写（feedback_prompt_verbatim_copy）。`{{`→`{` 归一已在计划内完成。
2. **声明顺序规范**：结构体→枚举→常量→全局变量→导出函数→非导出函数，分隔注释齐全（go-code-conventions 规范 2）。`TripleExtractorOption func(*TripleExtractor)` 简单函数类型归枚举区块。
3. **注释全中文**（规范 1）；日志 Msg 英文（feedback-logs-english——Python 原文本已英文直取）。
4. **禁止 -am 提交**（feedback-git-am-stowaway-commits）：工作区有他任务未提交改动时显式 add + 事后 `git show --stat HEAD` 核对。
5. **编译互斥**（规则 5）：编译前 `pgrep -f 'go (build|test)'` 查残留；设 `GOPROXY=https://goproxy.cn,direct`；勿 clean cache。
6. **pyStr 边界**：`{1e-4, "0.0001"}` 验证 f 记法下界含等号；`{1e16, "1e+16"}` 验证 e 记法上界不含等号（`abs < 1e16`）。json.Number 分支：整数字面 Atoi 成功原样（`"1"`→"1"、`"10000000000000000"`→原样——Go int64 界内且 Python int 无界同形）；浮点字面 ParseFloat 走 pyFloatStr（`"1e16"`→"1e+16"、`"1.0"`→"1.0"）。
7. **jsonrepair 语义（探针实测修正）**：极坏输入（空串/纯文本）不返回 err——`Value` 为 `string`（含空串），走 parseTriples 的 `default` 分支返回 false。测试断言 `ok=false` 而非断言 err。**数字类型三态**：合法 JSON 路径 `json.Number`、深修复小数 `float64`、深修复整数 `int`——pyStr 已三分支覆盖（计划 Task 5 实现含完整 switch）。
8. **gather 语义红线**：不引入 errgroup fail-fast（方案 B 已否决）；wg.Wait() 后才扫描；首错按 chunk 序非完成序。
9. **sync.Goroutine 槽位**：`results[i] = e.extractChunk(...)` 在 goroutine 内写各自槽位零竞争；`make([]chunkResult, len(chunks))` 空 chunks 语义安全。
10. **llmClient nil resp 防御**：`resp != nil` 判空后取 Content（13.15 captioner 先例 llmCall 尾部）。
11. **race 检测**：Task 6 Step 4 含 `-race` 全跑——mock last* 字段经 mu 保护、results 槽位各写各的、atomic 计数 CAS 更新，全链零竞争。
12. **深修复路径 float32 精度陷阱**：jsonrepair 深修复的 float 经 ParseFloat(32) 转 float64 会带精度噪声（如 0.00001 → 9.999999747378752e-06）——parseTriples 对三元组内数字走 pyStr 的 json.Number 路径时无此问题（合法 JSON 主路径）；只有截断 JSON 等深修复输入才遇 int/float64——测试用例全部用合法 JSON 字面，避开该噪声。
13. **编译期断言两段式**：`var _ Extractor = (*TripleExtractor)(nil)` 在 Task 3 注释、Task 6 实现双方法后恢复——避免中间态编译失败。
