# 13.25 AgenticRetriever 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `retriever` 包实现 AgenticRetriever——任意底层 Retriever 之上的 LLM 多跳检索外壳（事实抽取 + 充分性驱动改写 + RRF 融合；GraphRetriever 底层时启用图扩展与三元组链接）。

**Architecture:** 同包单文件 `agentic_retriever.go`（方案 A）：构造期类型断言 `*GraphRetriever` 判图路径 + `IndexTypeCarrier` 推导 defaultMode；图/通用双路径多轮循环（每轮 chunk 检索 → _read 抽三元组入 TripleMemory → _rewrite 判充分性）；`_linkTriples`/`_linkPassages` 裸 gather 三分类聚合；`pyStr` 包内私有复刻（13.23 pyAnyStr 先例）。base.go 加 `GraphExpansion *bool` 潜伏激活字段。

**Tech Stack:** Go 1.x / json-repair（`github.com/RealAlexandreAI/json-repair` 已有依赖）/ golang.org/x/sync（errgroup，已有）/ 项目自有 logger+exception 框架。

**Spec:** `docs/superpowers/specs/2026-10-28-agenticretriever-13.25-design.md`（11 偏差 + 5 拍板已归档，实施时对照）

**关键参考（实施前必读）：**
- Python 源码：`/home/opensource/agent-core/openjiuwen/core/retrieval/retriever/agentic_retriever.py`（548 行）
- Python 测试：`/home/opensource/agent-core/tests/unit_tests/core/retrieval/retriever/test_agentic_retriever.py`（23 例）
- Go 先例：`graph_retriever_impl.go`（同包 GraphRetriever 全链）、`indexing/processor/extractor/triple_extractor.go` L395-470（pyStr/pyFloatStr/isPureIntegerLiteral 复刻源）、`triple_extractor_test.go` L404-470（captionTripleClient 并发安全桩样板）
- 编译纪律：先 `pgrep -f 'go (build|test)'` 查残留进程；`export GOPROXY=https://goproxy.cn,direct`；全量回归带 `CGO_ENABLED=1 -tags "sqlite_fts5 test"`

---

## 文件结构

| 文件 | 动作 | 职责 |
|---|---|---|
| `internal/agentcore/retrieval/retriever/base.go` | 修改 | RetrieveOptions 加 GraphExpansion 字段 + WithRetrieveGraphExpansion |
| `internal/agentcore/retrieval/retriever/base_test.go` | 修改 | 补 GraphExpansion 字段单测 |
| `internal/agentcore/retrieval/retriever/agentic_retriever.go` | 新建 | AgenticRetriever 全量（常量→结构体→构造→Retrieve→图/通用路径→LLM 三件套→link 两件→pyStr 复刻） |
| `internal/agentcore/retrieval/retriever/agentic_retriever_test.go` | 新建 | 桩基建 + 23 例 1:1 + 10 项增强 |
| `internal/agentcore/retrieval/retriever/doc.go` | 修改 | 文件目录加 agentic_retriever.go 条目 + 核心类型索引 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 13.25 状态 ☐→✅ + 顶部备注行 |

---

### Task 1: base.go 加 GraphExpansion 字段

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/base.go`（RetrieveOptions 结构体，约 L57-72）
- Test: `internal/agentcore/retrieval/retriever/base_test.go`

- [ ] **Step 1: 写失败测试**

在 `base_test.go` 末尾追加（对齐既有 Alpha 字段测试形态，先查该文件中现有 `WithRetrieveAlpha` 用例命名再落位）：

```go
// TestNewRetrieveOptions_graphExpansion字段 GraphExpansion 指针三态（13.25 消费）。
func TestNewRetrieveOptions_graphExpansion字段(t *testing.T) {
	// 未设 → nil（kwargs.pop("graph_expansion", True) 的 Go 侧未设语义）
	o := NewRetrieveOptions()
	assert.Nil(t, o.GraphExpansion)

	// 显式 true
	o = NewRetrieveOptions(WithRetrieveGraphExpansion(true))
	require.NotNil(t, o.GraphExpansion)
	assert.True(t, *o.GraphExpansion)

	// 显式 false（零值陷阱：bool 无法区分未设，必须指针）
	o = NewRetrieveOptions(WithRetrieveGraphExpansion(false))
	require.NotNil(t, o.GraphExpansion)
	assert.False(t, *o.GraphExpansion)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run TestNewRetrieveOptions_graphExpansion -count=1 2>&1 | tail -3
```
预期：编译失败 `undefined: WithRetrieveGraphExpansion`。

- [ ] **Step 3: 最小实现**

`base.go` 的 RetrieveOptions 结构体 `NumCandidatesPerBeam` 字段后追加：

```go
	// GraphExpansion 图路径是否启用图扩展（13.25 AgenticRetriever 消费）。
	// nil → true（对齐 Python kwargs.pop("graph_expansion", True)）。
	// 指针类型：13.28 GraphKB 传 RetrievalConfig.graph_expansion 默认 False，
	// bool 零值无法区分"未设"与"显式 false"（Python 两层默认值不一致是
	// 真实行为，字面复刻）。
	GraphExpansion *bool
```

导出函数区 `WithRetrieveNumCandidatesPerBeam` 后追加：

```go
// WithRetrieveGraphExpansion 设置图路径是否启用图扩展的 RetrieveOption。
func WithRetrieveGraphExpansion(v bool) RetrieveOption {
	return func(o *RetrieveOptions) { o.GraphExpansion = &v }
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/retrieval/retriever/ -run TestNewRetrieveOptions -count=1 2>&1 | tail -2
```
预期：PASS（含既有字段用例）。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/retriever/base.go internal/agentcore/retrieval/retriever/base_test.go
git commit -m "feat(retriever): RetrieveOptions 新增 GraphExpansion 指针字段（13.25 消费，nil→true 对齐 kwargs.pop 默认）"
```

---

### Task 2: agentic_retriever.go 骨架——常量/结构体/构造/访问器

**Files:**
- Create: `internal/agentcore/retrieval/retriever/agentic_retriever.go`
- Test: `internal/agentcore/retrieval/retriever/agentic_retriever_test.go`（本任务建桩基建 + 构造测试）

- [ ] **Step 1: 写失败测试（桩基建 + 构造 9 例中的 6 例）**

新建 `agentic_retriever_test.go`，先落桩基建与构造用例。桩设计依据：spec §6.1（agenticFakeRetriever 可编程 Retrieve/rsr + IndexTypeCarrier；fakeAgenticLLM 按 prompt 内容分派）。13.25 的 LLM 桩**直接持有** `model_clients.BaseModelClient` 接口（不像 13.17 走 llm.NewModel 包装——Python AgenticRetriever 构造参数就是裸 client，不走 Model）：

```go
package retriever

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// stubAgenticLLM 零行为桩（stubTripleClient 先例）——嵌入补全 BaseModelClient 全接口。
type stubAgenticLLM struct{}

func (s *stubAgenticLLM) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubAgenticLLM) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubAgenticLLM) SupportsKVCacheRelease() bool { return false }

// agenticLLMDispatch 按 prompt 内容分派的可编程 LLM 桩。
//
// 分派规则（13.17 dispatchTripleClient 先例——与调度序解耦）：
//   - prompt 含 "knowledge triples, which are structured" → readFn
//   - prompt 含 "sufficient" → rewriteFn
//   - 都不命中 → 其他响应（neverHit 计数，测试断言零意外调用）
//
// mu 保护：BatchRetrieve 双 query 并发 + _link gather 并发下 Invoke 竞争。
type agenticLLMDispatch struct {
	stubAgenticLLM
	mu         sync.Mutex
	readFn     func(prompt string) (string, error)
	rewriteFn  func(prompt string) (string, error)
	readCalls  int
	rewriteCalls int
	otherCalls int
}

func (d *agenticLLMDispatch) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	prompt := ""
	for _, m := range messages.Messages() {
		if u, ok := m.(*llmschema.UserMessage); ok {
			prompt = u.Content.Text()
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var content string
	var err error
	switch {
	case strings.Contains(prompt, "knowledge triples, which are structured"):
		d.readCalls++
		content, err = d.readFn(prompt)
	case strings.Contains(prompt, "\"sufficient\""):
		d.rewriteCalls++
		content, err = d.rewriteFn(prompt)
	default:
		d.otherCalls++
		content, err = "", nil
	}
	if err != nil {
		return nil, err
	}
	return llmschema.NewAssistantMessage(content), nil
}

// agenticFakeRetriever 可编程检索器桩（不动 13.23 现有桩）。
//
// retrieveFn/rsrFn 闭包注入；调用记录 mu 保护（_link gather 并发）；
// indexType 实现 IndexTypeCarrier（defaultMode 三态测试）。
type agenticFakeRetriever struct {
	mu          sync.Mutex
	retrieveFn  func(ctx context.Context, query string, topK int, threshold *float64, mode RetrievalMode) ([]common.RetrievalResult, error)
	rsrFn       func(ctx context.Context, query string, topK int, mode RetrievalMode) ([]common.SearchResult, error)
	lastQuery   string
	lastTopK    int
	lastMode    RetrievalMode
	lastThreshold *float64
	calls       int
	rsrCalls    int
	closed      int
	indexType   string
}

func (f *agenticFakeRetriever) Retrieve(ctx context.Context, query string, topK int, threshold *float64, mode RetrievalMode, _ ...RetrieveOption) ([]common.RetrievalResult, error) {
	f.mu.Lock()
	f.lastQuery, f.lastTopK, f.lastMode, f.lastThreshold, f.calls = query, topK, mode, threshold, f.calls+1
	f.mu.Unlock()
	return f.retrieveFn(ctx, query, topK, threshold, mode)
}

func (f *agenticFakeRetriever) BatchRetrieve(context.Context, []string, int, ...RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil
}

func (f *agenticFakeRetriever) RetrieveSearchResults(ctx context.Context, query string, topK int, mode RetrievalMode, _ ...RetrieveOption) ([]common.SearchResult, error) {
	f.mu.Lock()
	f.rsrCalls++
	f.mu.Unlock()
	return f.rsrFn(ctx, query, topK, mode)
}

func (f *agenticFakeRetriever) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
}

// IndexType 实现 IndexTypeCarrier（defaultMode 推导测试用）。
func (f *agenticFakeRetriever) IndexType() string { return f.indexType }

// ──────────────────────────── 构造测试 ────────────────────────────

// TestNewAgenticRetriever_图检索器构造 对齐 test_init_success_with_graph_retriever。
func TestNewAgenticRetriever_图检索器构造(t *testing.T) {
	gr := NewGraphRetriever()
	r := NewAgenticRetriever(gr, &stubAgenticLLM{}, WithAgenticMaxIter(3))
	assert.True(t, r.IsGraphRetriever())
	assert.Equal(t, 3, r.MaxIter())
	assert.Equal(t, RetrievalModeHybrid, r.DefaultMode()) // 零值 indexType → hybrid
}

// TestNewAgenticRetriever_通用检索器构造 对齐 test_init_success_with_base_retriever。
func TestNewAgenticRetriever_通用检索器构造(t *testing.T) {
	fr := &agenticFakeRetriever{indexType: "hybrid"}
	r := NewAgenticRetriever(fr, &stubAgenticLLM{}, WithAgenticMaxIter(2))
	assert.False(t, r.IsGraphRetriever())
	assert.Equal(t, 2, r.MaxIter())
}

// TestNewAgenticRetriever_默认值 对齐 test_init_with_defaults。
func TestNewAgenticRetriever_默认值(t *testing.T) {
	r := NewAgenticRetriever(NewGraphRetriever(), &stubAgenticLLM{})
	assert.Equal(t, 2, r.MaxIter())
}

// TestNewAgenticRetriever_非法maxIter回落 对齐 test_init_with_invalid_max_iter。
func TestNewAgenticRetriever_非法maxIter回落(t *testing.T) {
	r := NewAgenticRetriever(NewGraphRetriever(), &stubAgenticLLM{}, WithAgenticMaxIter(-1))
	assert.Equal(t, 2, r.MaxIter())
}

// TestNewAgenticRetriever_缺retriever 对齐 test_init_without_retriever（155208）。
func TestNewAgenticRetriever_缺retriever(t *testing.T) {
	_, err := NewAgenticRetriever(nil, &stubAgenticLLM{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retriever is required for AgenticRetriever")
}

// TestNewAgenticRetriever_缺llm客户端 对齐 test_init_without_llm_client（155209）。
func TestNewAgenticRetriever_缺llm客户端(t *testing.T) {
	_, err := NewAgenticRetriever(NewGraphRetriever(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm_client is required for AgenticRetriever")
}

// TestNewAgenticRetriever_默认模式三态 对齐 test_default_mode_vector/_bm25/_hybrid。
func TestNewAgenticRetriever_默认模式三态(t *testing.T) {
	for _, tc := range []struct {
		indexType string
		want      RetrievalMode
	}{
		{"vector", RetrievalModeVector},
		{"bm25", RetrievalModeSparse},
		{"hybrid", RetrievalModeHybrid},
		{"", RetrievalModeHybrid},          // getattr None → or "hybrid"
		{"unknown-type", RetrievalModeHybrid}, // 其余分支
	} {
		fr := &agenticFakeRetriever{indexType: tc.indexType}
		r := NewAgenticRetriever(fr, &stubAgenticLLM{})
		assert.Equal(t, tc.want, r.DefaultMode(), "indexType=%q", tc.indexType)
	}
}
```

注意：`messages.Messages()` 的具体方法名实施时以 `model_clients.MessagesParam` 实际定义为准（grep `type MessagesParam` 确认；若为 `[]Message` 切片别名则直接 range）。

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/agentcore/retrieval/retriever/ -run TestNewAgenticRetriever -count=1 2>&1 | tail -3
```
预期：编译失败 `undefined: NewAgenticRetriever` 等。

- [ ] **Step 3: 写骨架实现**

新建 `agentic_retriever.go`。声明顺序严格按项目规范（结构体→常量→导出函数→非导出函数）。提示词常量从 Python L30-110 **逐字节复制**（三引号内首行 `\n` 起）：

```go
// 多跳 agentic 检索器：任意 Retriever 之上的 LLM 查询改写与多轮融合。
//
// 底层为 GraphRetriever 时自动启用图扩展与三元组链接；其他 Retriever
// 走迭代改写 + 结果融合（GeAR 策略，arXiv:2507.17399）。
//
// Python: retriever/agentic_retriever.py (AgenticRetriever)
package retriever

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgenticRetriever 多跳 agentic 检索器。
//
// 构造期类型断言判定图路径；TripleMemory 跨轮累积事实；LLM 三件套
// （读/改写/链接）软失败降级不中断循环。
type AgenticRetriever struct {
	// retriever 底层检索器（nil→155208）
	retriever Retriever
	// isGraph 构造期类型断言 *GraphRetriever（isinstance 同构）
	isGraph bool
	// defaultMode IndexTypeCarrier 探测推导（vector→vector/bm25→sparse/其余→hybrid）
	defaultMode RetrievalMode
	// llm LLM 客户端（nil→155209）
	llm model_clients.BaseModelClient
	// maxIter 最大迭代轮数（默认 2；非法值 Warn 回落）
	maxIter int
}

// AgenticRetrieverOption 构造选项。
type AgenticRetrieverOption func(*AgenticRetriever)

// 编译期断言：AgenticRetriever 满足 Retriever 接口。
var _ Retriever = (*AgenticRetriever)(nil)

// ──────────────────────────── 常量 ────────────────────────────

// readPrompt 读步骤提示词（Python _READ_PROMPT 逐字节复制）。
// 占位符 {docs}/{query}/{facts} 经 NewReplacer 填充。
const readPrompt = `
Your task is to find facts that help answer an input question.
...（Python L31-48 逐字节复制，含 {docs}/{query}/{facts} 三占位符）
`

// rewritePrompt 改写步骤提示词（Python _REWRITE_PROMPT 逐字节复制）。
// 注意保留 {{ 双花括号字面量（Replacer 只替换命名槽不误伤）。
const rewritePrompt = `
Given a question and its associated retrieved knowledge triples, you
...（Python L52-109 逐字节复制，含 {query}/{question_rewriting_history}/{triples} 三占位符与 {{...}} 字面双花括号）
`

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgenticRetriever 创建多跳 agentic 检索器。
//
// 对齐 Python __init__（L129-161）：retriever/llm_client 必填（None→
// 155208/155209，error_msg 逐字符）；max_iter 非法（≤0）Warn 回落 2；
// isinstance 判图路径；getattr(retriever,"index_type",None) or "hybrid"
// 推导默认模式。
func NewAgenticRetriever(retriever Retriever, llm model_clients.BaseModelClient, opts ...AgenticRetrieverOption) *AgenticRetriever {
	if retriever == nil {
		panic(exception.BuildError(
			exception.StatusRetrievalRetrieverNotFound,
			exception.WithParam("error_msg", "retriever is required for AgenticRetriever"),
		))
	}
	if llm == nil {
		panic(exception.BuildError(
			exception.StatusRetrievalRetrieverLlmClientNotFound,
			exception.WithParam("error_msg", "llm_client is required for AgenticRetriever"),
		))
	}
	// ↑ 实施时核对：exception.BuildError 返回 error（非 panic）——查 13.23
	// GetRetrieverForMode 的用法是 return nil, err。构造函数无 error 返回位，
	// 查项目先例：若 NewXxx 惯例是 (T, error) 双返回值则改签名。
	a := &AgenticRetriever{
		retriever: retriever,
		isGraph:   false,
		llm:       llm,
		maxIter:   2,
	}
	if _, ok := retriever.(*GraphRetriever); ok {
		a.isGraph = true
	}
	// getattr(retriever, "index_type", None) or "hybrid"
	indexType := ""
	if c, ok := retriever.(IndexTypeCarrier); ok {
		indexType = c.IndexType()
	}
	if indexType == "" {
		indexType = "hybrid"
	}
	switch indexType {
	case "vector":
		a.defaultMode = RetrievalModeVector
	case "bm25":
		a.defaultMode = RetrievalModeSparse
	default:
		a.defaultMode = RetrievalModeHybrid
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// WithAgenticMaxIter 设置最大迭代轮数（非法值 Warn 回落 2）。
func WithAgenticMaxIter(n int) AgenticRetrieverOption {
	return func(a *AgenticRetriever) {
		if n > 0 {
			a.maxIter = n
			return
		}
		logger.Warn(logComponent).
			Int("max_iter", n).
			Msg("Invalid max_iter provided, falling back to default of 2")
		a.maxIter = 2
	}
}

// Retriever_ 返回底层检索器。
func (a *AgenticRetriever) Retriever_() Retriever { return a.retriever }

// LLM 返回 LLM 客户端。
func (a *AgenticRetriever) LLM() model_clients.BaseModelClient { return a.llm }

// MaxIter 返回最大迭代轮数。
func (a *AgenticRetriever) MaxIter() int { return a.maxIter }

// IsGraphRetriever 底层是否为 GraphRetriever。
func (a *AgenticRetriever) IsGraphRetriever() bool { return a.isGraph }

// DefaultMode 默认检索模式（index_type 推导）。
func (a *AgenticRetriever) DefaultMode() RetrievalMode { return a.defaultMode }
```

**实施时必须核对的三件事：**
1. `exception.BuildError` 返回 `error`——构造错误怎么抛？查 13.19 `NewBaseChunker`（155100）或 13.23 的先例：Go 构造函数惯例是 `(T, error)` 双返回还是 panic。**按先例统一**（预判：双返回值，则 `NewAgenticRetriever` 签名为 `(*AgenticRetriever, error)`，测试同步改）。
2. 提示词常量从 Python 复制时用 `cat -A` 或 md5 校验（13.24 prompts 的 md5 校验纪律）：`md5sum` Python 源码 L31-48/L52-109 提取串 vs Go 常量。
3. `Retriever_()` 这个名字有下划线是因为字段名 `retriever` 与接口方法 `Retrieve` 无冲突但方法名 `Retriever()` 会与类型名撞——若包内无冲突可直接叫 `UnderlyingRetriever()`，以编译器为准，doc 注释注明对齐 Python `retriever` 属性。

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/retrieval/retriever/ -run TestNewAgenticRetriever -count=1 2>&1 | tail -3
```
预期：7 个构造用例 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/retriever/agentic_retriever.go internal/agentcore/retrieval/retriever/agentic_retriever_test.go
git commit -m "feat(retriever): AgenticRetriever 骨架——常量/结构体/构造/访问器（图路径断言+defaultMode 推导+桩基建）"
```

---

### Task 3: Retrieve 主链 + 通用路径

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/agentic_retriever.go`
- Test: `internal/agentcore/retrieval/retriever/agentic_retriever_test.go`

- [ ] **Step 1: 写失败测试（通用路径 5 例 + top_k 2 例 + 透传 2 例 + batch 1 例）**

追加到 `agentic_retriever_test.go`：

```go
// ──────────────────────────── 通用路径测试 ────────────────────────────

// newGenericRetriever 通用路径测试脚手架：固定单结果 chunk 桩 + 可分派 LLM。
func newGenericRetriever(t *testing.T, llm *agenticLLMDispatch) *AgenticRetriever {
	t.Helper()
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	return NewAgenticRetriever(fr, llm)
}

// TestRetrieve_通用路径单轮 对齐 test_retrieve_generic_single_iteration。
func TestRetrieve_通用路径单轮(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestRetrieve_通用路径多轮 对齐 test_retrieve_generic_multiple_iterations（3 轮，calls==3）。
func TestRetrieve_通用路径多轮(t *testing.T) {
	rewriteCount := 0
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			rewriteCount++
			return fmt.Sprintf(`{"sufficient": false, "next_question": "rewritten query %d"}`, rewriteCount), nil
		},
	}
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	r := NewAgenticRetriever(fr, llm, WithAgenticMaxIter(3))
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 3, fr.calls)
}

// TestRetrieve_通用路径到顶 对齐 test_retrieve_generic_max_iterations（默认 max_iter=2）。
func TestRetrieve_通用路径到顶(t *testing.T) {
	rewriteCount := 0
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			rewriteCount++
			return `{"sufficient": false, "next_question": "rewritten query 1"}`, nil
		},
	}
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	r := NewAgenticRetriever(fr, llm)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 2, fr.calls)
}

// TestRetrieve_融合多结果 对齐 test_retrieve_fusion_multiple_results。
func TestRetrieve_融合多结果(t *testing.T) {
	callIdx := 0
	var mu sync.Mutex
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(_ context.Context, _ string, _ int, _ *float64, _ RetrievalMode) ([]common.RetrievalResult, error) {
			mu.Lock()
			defer mu.Unlock()
			callIdx++
			if callIdx == 1 {
				return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
			}
			return []common.RetrievalResult{{Text: "Result 2", Score: 0.8}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			return `{"sufficient": false, "next_question": "rewritten query"}`, nil
		},
	}
	r := NewAgenticRetriever(fr, llm, WithAgenticMaxIter(2))
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	// RRF text 去重：两轮两个不同 text → 2 条
	assert.Len(t, results, 2)
}

// TestRetrieve_topK无效 对齐 test_retrieve_without_valid_top_k + _negative。
func TestRetrieve_topK无效(t *testing.T) {
	r := newGenericRetriever(t, &agenticLLMDispatch{})
	for _, topK := range []int{0, -1} {
		_, err := r.Retrieve(context.Background(), "test query", topK, nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "top_k is invalid, must be a positive integer")
	}
}

// TestRetrieve_模式透传 对齐 test_retrieve_with_custom_mode（桩 lastMode 断言）。
func TestRetrieve_模式透传(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := NewAgenticRetriever(fr, llm)
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.NoError(t, err)
	fr.mu.Lock()
	assert.Equal(t, RetrievalModeVector, fr.lastMode)
	assert.Equal(t, 5, fr.lastTopK)
	fr.mu.Unlock()
}

// TestRetrieve_阈值透传 对齐 test_retrieve_with_score_threshold。
func TestRetrieve_阈值透传(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := NewAgenticRetriever(fr, llm)
	threshold := 0.8
	_, err := r.Retrieve(context.Background(), "test query", 5, &threshold, "")
	require.NoError(t, err)
	fr.mu.Lock()
	require.NotNil(t, fr.lastThreshold)
	assert.Equal(t, 0.8, *fr.lastThreshold)
	fr.mu.Unlock()
}

// TestBatchRetrieve 对齐 test_batch_retrieve（双 query 各自走完循环）。
func TestBatchRetrieve(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := NewAgenticRetriever(fr, llm)
	resultsList, err := r.BatchRetrieve(context.Background(), []string{"query 1", "query 2"}, 5)
	require.NoError(t, err)
	assert.Len(t, resultsList, 2)
	assert.Len(t, resultsList[0], 1)
	assert.Len(t, resultsList[1], 1)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/agentcore/retrieval/retriever/ -run 'TestRetrieve_|TestBatchRetrieve' -count=1 2>&1 | tail -3
```
预期：编译失败（Retrieve/BatchRetrieve 方法未实现）。

- [ ] **Step 3: 实现 Retrieve/BatchRetrieve/通用路径/_read/_rewrite/_llmCall**

`agentic_retriever.go` 导出函数区追加（`_read`/`_rewrite`/`_llmCall` 放非导出区）。核心代码：

```go
// Retrieve 多跳检索主链。
//
// 对齐 Python retrieve（L173-194）：top_k 校验（155210）→ mode 零值回落
// defaultMode → 图/通用路径分派。
func (a *AgenticRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if topK <= 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverTopKInvalid,
			exception.WithParam("error_msg", "top_k is invalid, must be a positive integer"),
		)
	}
	resolvedMode := mode
	if mode == "" {
		resolvedMode = a.defaultMode
	}
	if a.isGraph {
		return a.retrieveWithGraph(ctx, query, topK, scoreThreshold, resolvedMode, opts...)
	}
	return a.retrieveGeneric(ctx, query, topK, scoreThreshold, resolvedMode, opts...)
}

// BatchRetrieve 批量并发（裸 errgroup，13.19 惯例无 WithContext 不 cancel）。
func (a *AgenticRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	results := make([][]common.RetrievalResult, len(queries))
	var gg errgroup.Group
	for i, query := range queries {
		i, query := i, query
		gg.Go(func() error {
			ret, err := a.Retrieve(ctx, query, topK, nil, "", opts...)
			if err != nil {
				return err
			}
			results[i] = ret
			return nil
		})
	}
	if err := gg.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// RetrieveSearchResults 返回 nil（Python 未覆写，继承基类空方法 None）。
func (a *AgenticRetriever) RetrieveSearchResults(context.Context, string, int, RetrievalMode, ...RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}

// Close 关闭底层检索器（Python hasattr+iscoroutinefunction 编译期消解）。
func (a *AgenticRetriever) Close() {
	a.retriever.Close()
}
```

非导出区追加通用路径（对齐 Python L253-296）：

```go
// retrieveGeneric 通用路径：迭代改写 + RRF 融合。
//
// 对齐 Python _retrieve_generic：每轮 retrieve → _read 入 memory →
// _rewrite 判充分性；GeAR 论文 arXiv:2507.17399。
func (a *AgenticRetriever) retrieveGeneric(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	queries := []string{query}
	var historyResults [][]common.RetrievalResult
	memory := common.NewTripleMemory()

	for turn := 1; turn <= a.maxIter; turn++ {
		q := queries[len(queries)-1]
		chunkResults, err := a.retriever.Retrieve(ctx, q, topK, scoreThreshold, mode, opts...)
		if err != nil {
			return nil, err
		}
		triples := a.read(ctx, query, chunkResults, memory.Memory())
		memory.BatchExtendMemory(triples)
		historyResults = append(historyResults, chunkResults)

		if turn >= a.maxIter {
			break
		}
		rewritten := a.rewrite(ctx, query, memory.TriplesStr(), queries)
		if rewritten == "" {
			break
		}
		queries = append(queries, rewritten)
	}
	return utils.RRFFusion(historyResults, 60)[:topK], nil
}
```

注意 Python `rrf_fusion(history_results)[:top_k]`——切片 `[:topK]` 需防越界（Go slice 切片超 len panic，Python 切片超界安全截断）：`truncateResults(historyResults 融合结果, &topK)` 或手写 `if len(fused) > topK { fused = fused[:topK] }`。**用 13.23 已有的 `truncateResults`**（graph_retriever_impl.go L302 先例，确认其签名后复用）。

`_llmCall`/`read`/`rewrite`（对齐 Python L298-372）：

```go
// llmCall 单次 LLM 调用（软失败：err → Warn + 空串）。
// 返回 (content, ok)；ok=false 表示调用失败，调用方按 None 语义降级。
func (a *AgenticRetriever) llmCall(ctx context.Context, prompt string) (string, bool) {
	resp, err := a.llm.Invoke(ctx,
		model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt)),
		model_clients.WithInvokeTemperature(0.0))
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Agentic LLM invocation failed")
		return "", false
	}
	if resp == nil {
		return "", false
	}
	return resp.Content.Text(), true
}

// read 读步骤：从 passages 抽知识三元组（软失败 → 空切片）。
//
// 对齐 Python _read（L374-413）：docs 截前 5 + 跳过空文本 + facts 串
// Python tuple repr + jsonrepair + 根非 list Warn + len==3 过滤 + pyStr。
func (a *AgenticRetriever) read(ctx context.Context, query string, passages []common.RetrievalResult, existingFacts []common.TripleFact) []common.TripleFact {
	var docParts []string
	for i, p := range passages {
		if i >= 5 {
			break
		}
		if p.Text != "" {
			docParts = append(docParts, p.Text)
		}
	}
	docs := strings.Join(docParts, "\n\n")
	factsStr := "None"
	if len(existingFacts) > 0 {
		parts := make([]string, 0, len(existingFacts))
		for _, fact := range existingFacts {
			parts = append(parts, pyTupleRepr(fact))
		}
		factsStr = strings.Join(parts, ", ")
	}
	prompt := strings.NewReplacer(
		"{docs}", docs,
		"{query}", query,
		"{facts}", factsStr,
	).Replace(readPrompt)

	content, ok := a.llmCall(ctx, prompt)
	if !ok {
		return nil
	}
	result, err := jsonrepair.RepairWithOptions(content, jsonrepair.RepairOptions{})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Failed to parse read response as JSON")
		return nil
	}
	arr, ok := result.Value().([]any)
	if !ok {
		logger.Warn(logComponent).Any("response", result.Value()).Msg("Read response JSON is not a list")
		return nil
	}
	var triples []common.TripleFact
	for _, item := range arr {
		triple, ok := item.([]any)
		if !ok || len(triple) != 3 {
			continue
		}
		var fact common.TripleFact
		for j, elem := range triple {
			fact[j] = pyStr(elem)
		}
		triples = append(triples, fact)
	}
	return triples
}

// rewrite 改写步骤：判充分性，不足则生成下一问（软失败 → 空串=break）。
//
// 对齐 Python _rewrite（L326-372）：历史行渲染 + jsonrepair + 根非 dict
// Warn + sufficient/next_question 提取（非 string 收窄空串，spec §8-6）。
func (a *AgenticRetriever) rewrite(ctx context.Context, query, triplesStr string, questionHistory []string) string {
	historyStr := "(No rewriting steps yet)"
	if len(questionHistory) > 1 {
		lines := make([]string, 0, len(questionHistory)-1)
		for i, q := range questionHistory[1:] {
			lines = append(lines, fmt.Sprintf("Rewritten Question %d: %s", i+1, q))
		}
		historyStr = strings.Join(lines, "\n")
	}
	prompt := strings.NewReplacer(
		"{query}", query,
		"{triples}", triplesStr,
		"{question_rewriting_history}", historyStr,
	).Replace(rewritePrompt)

	content, ok := a.llmCall(ctx, prompt)
	if !ok {
		return ""
	}
	result, err := jsonrepair.RepairWithOptions(content, jsonrepair.RepairOptions{})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Failed to parse rewrite response as JSON")
		return ""
	}
	obj, ok := result.Value().(map[string]any)
	if !ok {
		logger.Warn(logComponent).Any("response", result.Value()).Msg("Rewrite response JSON is not an object")
		return ""
	}
	sufficient := pyTruthy(obj["sufficient"]) // 缺失 → False
	nextQuestion, _ := obj["next_question"].(string)
	if sufficient || nextQuestion == "" {
		return ""
	}
	return nextQuestion
}

// pyTupleRepr Python tuple repr：('s', 'p', 'o')——单引号+逗号空格。
func pyTupleRepr(f common.TripleFact) string {
	quoted := make([]string, 3)
	for i, s := range f {
		quoted[i] = "'" + s + "'"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

// pyTruthy Python truthy 判定（sufficient 字段：缺失=nil falsy；bool 直判；
// 其余类型 Python 非零数值 truthy——此处 sufficient 语义只可能是 bool，
// 数值容错按 !=0）。
func pyTruthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case float64:
		return val != 0
	case json.Number:
		f, err := val.Float64()
		return err == nil && f != 0
	default:
		return true
	}
}
```

`pyStr`/`pyFloatStr`/`isPureIntegerLiteral` 从 `indexing/processor/extractor/triple_extractor.go` L406-490 **整段复制**（包内私有复刻，spec 偏差 §8-9），注释改为"13.17 pyStr 同源语义，读取侧复刻"。

**核对点**：`jsonrepair.RepairWithOptions` 返回值 `.Value()` 的方法名以 13.17/13.24 实际用法为准（grep `RepairWithOptions` 现有两处用法照抄取值形态）。

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/retrieval/retriever/ -run 'TestRetrieve_|TestBatchRetrieve|TestNewAgenticRetriever' -count=1 2>&1 | tail -3
```
预期：全 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/retriever/agentic_retriever.go internal/agentcore/retrieval/retriever/agentic_retriever_test.go
git commit -m "feat(retriever): AgenticRetriever 通用路径——迭代改写+RRF 融合+_read/_rewrite/_llmCall+pyStr 复刻"
```

---

### Task 4: 图路径（retrieveWithGraph + linkTriples/linkPassages）

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/agentic_retriever.go`
- Test: `internal/agentcore/retrieval/retriever/agentic_retriever_test.go`

- [ ] **Step 1: 写失败测试（图路径 2 例 + link 聚合增强 + close 1 例）**

```go
// ──────────────────────────── 图路径测试 ────────────────────────────

// newGraphSetup 图路径脚手架：真 GraphRetriever + 双桩（chunk/triple）+ LLM。
//
// 链路可控性（spec §6.1）：GetRetrieverForMode 固定优先返回桩；
// GraphExpansion 真方法在「proximal 空 → linked 空 → fetchTriples 空 →
// 返回原 chunks」路径行为确定（beam search 深路径归 13.23 的 48 测试）。
func newGraphSetup(t *testing.T, llm *agenticLLMDispatch) (*AgenticRetriever, *agenticFakeRetriever, *agenticFakeRetriever) {
	t.Helper()
	chunk := &agenticFakeRetriever{
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
		rsrFn: func(context.Context, string, int, RetrievalMode) ([]common.SearchResult, error) {
			return nil, nil
		},
	}
	triple := &agenticFakeRetriever{
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return nil, nil
		},
		rsrFn: func(context.Context, string, int, RetrievalMode) ([]common.SearchResult, error) {
			return nil, nil
		},
	}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunk),
		WithGraphTripleRetriever(triple),
	)
	return NewAgenticRetriever(gr, llm), chunk, triple
}

// TestRetrieve_图路径单轮 对齐 test_retrieve_with_graph_single_iteration。
func TestRetrieve_图路径单轮(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r, chunk, _ := newGraphSetup(t, llm)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	_ = chunk
}

// TestRetrieve_图路径多轮 对齐 test_retrieve_with_graph_multiple_iterations（chunk calls==2）。
func TestRetrieve_图路径多轮(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			return `{"sufficient": false, "next_question": "rewritten query 1"}`, nil
		},
	}
	r, chunk, _ := newGraphSetup(t, llm)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	chunk.mu.Lock()
	assert.Equal(t, 2, chunk.calls)
	chunk.mu.Unlock()
}

// TestRetrieve_图路径graphExpansion关闭 nil（默认 true）/显式 false 对比。
//
// graphExpansion=false 时 GraphExpansion 不被调用：chunk 桩返回带标记文本，
// 断言结果中保留原始标记（true 路径因 _read 返回 [] 走原 chunks 返回，
// 两者结果相同——本用例改用 triple 桩 rsrCalls 断言：true 时 _linkTriples
// 有 rsr 调用（若 proximal 非空）；false 时无）。
func TestRetrieve_图路径graphExpansion开关(t *testing.T) {
	// proximal _read 返回一条三元组 → _linkTriples 走 rsr
	tripleOut := `[["Neville A. Stanton", "employer", "University of Southampton"]]`
	for _, tc := range []struct {
		name      string
		expansion *bool
		wantRsr   int
	}{
		{"未设默认true", nil, 1},
		{"显式false", func() *bool { b := false; return &b }(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readCount := 0
			llm := &agenticLLMDispatch{
				readFn: func(string) (string, error) {
					readCount++
					if readCount == 1 {
						return tripleOut, nil // proximal read
					}
					return "[]", nil // 主 read
				},
				rewriteFn: func(string) (string, error) {
					return `{"sufficient": true, "next_question": null}`, nil
				},
			}
			r, _, triple := newGraphSetup(t, llm)
			var opts []RetrieveOption
			if tc.expansion != nil {
				opts = append(opts, WithRetrieveGraphExpansion(*tc.expansion))
			}
			_, err := r.Retrieve(context.Background(), "original query", 5, nil, "", opts...)
			require.NoError(t, err)
			triple.mu.Lock()
			assert.Equal(t, tc.wantRsr, triple.rsrCalls)
			triple.mu.Unlock()
		})
	}
}

// TestClose 对齐 test_close 三态（Go 接口编译期消解，1 例断言委托）。
func TestClose(t *testing.T) {
	fr := &agenticFakeRetriever{}
	r := NewAgenticRetriever(fr, &stubAgenticLLM{})
	r.Close()
	fr.mu.Lock()
	assert.Equal(t, 1, fr.closed)
	fr.mu.Unlock()
}

// TestLinkTriples_部分失败聚合 rsr 桩混排 error/空列表/正常（Go 增强 5）。
func TestLinkTriples_部分失败聚合(t *testing.T) {
	// 直接构造 AgenticRetriever（isGraph=true）调 linkTriples：
	// triple 桩 rsrFn 按调用序返回 error / 空列表 / 正常 2 条（同 ID 去重）
	gr := NewGraphRetriever()
	r := NewAgenticRetriever(gr, &stubAgenticLLM{})
	idx := 0
	var mu sync.Mutex
	triple := &agenticFakeRetriever{
		rsrFn: func(context.Context, string, int, RetrievalMode) ([]common.SearchResult, error) {
			mu.Lock()
			defer mu.Unlock()
			idx++
			switch idx {
			case 1:
				return nil, errors.New("boom")
			case 2:
				return []common.SearchResult{}, nil
			default:
				return []common.SearchResult{
					{ID: "t1", Text: "(a b c)", Score: 0.9, Metadata: map[string]any{"doc_id": "d1", "chunk_id": "c1"}},
					{ID: "t1", Text: "(a b c) dup", Score: 0.8}, // 同 ID 去重
				}, nil
			}
		},
	}
	_ = NewGraphRetriever(WithGraphTripleRetriever(triple)) // gr 需带桩
	// ↑ 修正：gr 应直接带桩构造——见下方 Step 3 实现说明，测试里改为：
	// gr := NewGraphRetriever(WithGraphTripleRetriever(triple))
	results, err := r.linkTriples(context.Background(), []common.TripleFact{{"a", "b", "c"}, {"x", "y", "z"}, {"p", "q", "r"}}, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 1)          // 3 输入：1 失败 + 1 空 + 1 成功（同 ID 去重后 1 条）
	assert.Equal(t, "d1", results[0].DocID)
	assert.Equal(t, "c1", results[0].ChunkID)
}

// TestLinkPassages_失败聚合 Retrieve 桩混排（Go 增强 6）。
func TestLinkPassages_失败聚合(t *testing.T) {
	idx := 0
	var mu sync.Mutex
	chunk := &agenticFakeRetriever{
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			mu.Lock()
			defer mu.Unlock()
			idx++
			if idx == 1 {
				return nil, errors.New("boom")
			}
			return []common.RetrievalResult{{Text: "passage", Score: 0.5}}, nil
		},
	}
	gr := NewGraphRetriever(WithGraphChunkRetriever(chunk))
	r := NewAgenticRetriever(gr, &stubAgenticLLM{})
	results, err := r.linkPassages(context.Background(), []common.TripleFact{{"a", "b", "c"}, {"x", "y", "z"}}, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 1) // 1 失败 + 1 成功
	assert.Equal(t, "passage", results[0][0].Text)
}
```

注意 `TestLinkTriples_部分失败聚合` 里标注的构造修正——写测试时直接把 gr 带桩构造（去掉冗余行）。

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/agentcore/retrieval/retriever/ -run 'TestRetrieve_图路径|TestClose|TestLink' -count=1 2>&1 | tail -3
```
预期：编译失败（retrieveWithGraph/linkTriples/linkPassages 未定义）。

- [ ] **Step 3: 实现图路径三件**

```go
// retrieveWithGraph 图路径：图扩展 + 三元组链接 + 多轮融合。
//
// 对齐 Python _retrieve_with_graph（L196-251）。graphExpansion 默认 true
// （kwargs.pop）；主 _read 传原始 query 非 q（spec §8-2 字面复刻）；
// 末轮后 _linkPassages 把 memory 三元组链回 passages，RRF 输入
// ret 在前 + history 在后。
func (a *AgenticRetriever) retrieveWithGraph(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if err := a.assertGraphRetriever(); err != nil {
		return nil, err
	}
	options := NewRetrieveOptions(opts...)
	graphExpansion := true
	if options.GraphExpansion != nil {
		graphExpansion = *options.GraphExpansion
	}

	queries := []string{query}
	var historyResults [][]common.RetrievalResult
	memory := common.NewTripleMemory()

	for turn := 1; turn <= a.maxIter; turn++ {
		q := queries[len(queries)-1]

		graphRetriever := a.retriever.(*GraphRetriever)
		chunkRetriever, err := graphRetriever.GetRetrieverForMode(mode, true)
		if err != nil {
			return nil, err
		}
		chunkResults, err := chunkRetriever.Retrieve(ctx, q, topK, scoreThreshold, mode, opts...)
		if err != nil {
			return nil, err
		}

		if graphExpansion {
			proximalTriples := a.read(ctx, q, chunkResults, nil)
			linkedTriples, err := a.linkTriples(ctx, proximalTriples, mode, opts...)
			if err != nil {
				return nil, err
			}
			chunkResults, err = graphRetriever.GraphExpansion(ctx, q, chunkResults, linkedTriples, &topK, mode, opts...)
			if err != nil {
				return nil, err
			}
		}

		triples := a.read(ctx, query, chunkResults, memory.Memory())
		memory.BatchExtendMemory(triples)
		historyResults = append(historyResults, chunkResults)

		if turn >= a.maxIter {
			break
		}
		rewritten := a.rewrite(ctx, query, memory.TriplesStr(), queries)
		if rewritten == "" {
			break
		}
		queries = append(queries, rewritten)
	}

	ret, err := a.linkPassages(ctx, memory.Memory(), mode, opts...)
	if err != nil {
		return nil, err
	}
	fusionInput := append(ret, historyResults...)
	fused := utils.RRFFusion(fusionInput, 60)
	if len(fused) > topK {
		fused = fused[:topK]
	}
	return fused, nil
}

// assertGraphRetriever 防御断言（Python _assert_graph_retriever；Go 类型
// 系统保证仅 isGraph 分支可达——保留防御，spec §8-10）。
func (a *AgenticRetriever) assertGraphRetriever() error {
	if _, ok := a.retriever.(*GraphRetriever); !ok {
		return exception.BuildError(
			exception.StatusRetrievalRetrieverInvalid,
			exception.WithParam("error_msg", "Underlying retriever must be a GraphRetriever to link passages"),
		)
	}
	return nil
}

// linkTriples 邻近三元组链接到 KB 三元组（裸 gather 三分类聚合）。
//
// 对齐 Python _link_triples（L415-479）：每 triple 一个 rsr（top_k=1）
// → 三分类（error/空/正常取[0]）→ Deduplicate(key=ID) → 映射
// RetrievalResult（doc_id/chunk_id 非 string 收窄空串，spec §8-7）。
func (a *AgenticRetriever) linkTriples(ctx context.Context, triples []common.TripleFact, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if err := a.assertGraphRetriever(); err != nil {
		return nil, err
	}
	graphRetriever := a.retriever.(*GraphRetriever)
	tripleRetriever, err := graphRetriever.GetRetrieverForMode(mode, false)
	if err != nil {
		return nil, err
	}

	// 预分配槽位 + WaitGroup（13.17 方案 A 无信号量版）
	searchResults := make([][]common.SearchResult, len(triples))
	failedFlags := make([]bool, len(triples))
	var wg sync.WaitGroup
	for i, triple := range triples {
		i, triple := i, triple
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := tripleRetriever.RetrieveSearchResults(ctx, tripleFactStr(triple), 1, mode, opts...)
			if err != nil || len(results) == 0 {
				if err == nil {
					logger.Warn(logComponent).
						Str("type", fmt.Sprintf("%T", results)).
						Msg("Empty or unexpected result type during link triples")
				}
				failedFlags[i] = true
				return
			}
			searchResults[i] = results
		}()
	}
	wg.Wait()

	var allTriples []common.SearchResult
	failed := 0
	for i := range triples {
		if failedFlags[i] {
			failed++
			continue
		}
		allTriples = append(allTriples, searchResults[i][0])
	}
	if failed > 0 {
		logger.Warn(logComponent).
			Int("failed", failed).
			Int("total", len(triples)).
			Msg("Triple linking failed for triples")
	}

	deduped := utils.Deduplicate(allTriples, func(r common.SearchResult) string { return r.ID })
	retrievalResults := make([]common.RetrievalResult, 0, len(deduped))
	for _, result := range deduped {
		metadata := result.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		docID, _ := metadata["doc_id"].(string)
		chunkID, _ := metadata["chunk_id"].(string)
		retrievalResults = append(retrievalResults, common.RetrievalResult{
			Text:     result.Text,
			Score:    result.Score,
			Metadata: metadata,
			DocID:    docID,
			ChunkID:  chunkID,
		})
	}
	return retrievalResults, nil
}

// linkPassages memory 三元组链接回 passages（裸 gather，整列表保留）。
//
// 对齐 Python _link_passages（L481-528）：每 triple 一个 Retrieve（top_k=5）
// → 三分类聚合 → 返回 [][]RetrievalResult。
func (a *AgenticRetriever) linkPassages(ctx context.Context, triples []common.TripleFact, mode RetrievalMode, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	if err := a.assertGraphRetriever(); err != nil {
		return nil, err
	}
	graphRetriever := a.retriever.(*GraphRetriever)
	chunkRetriever, err := graphRetriever.GetRetrieverForMode(mode, true)
	if err != nil {
		return nil, err
	}

	results := make([][]common.RetrievalResult, len(triples))
	failedFlags := make([]bool, len(triples))
	var wg sync.WaitGroup
	for i, triple := range triples {
		i, triple := i, triple
		wg.Add(1)
		go func() {
			defer wg.Done()
			ret, err := chunkRetriever.Retrieve(ctx, tripleFactStr(triple), 5, nil, mode, opts...)
			if err != nil || len(ret) == 0 {
				if err == nil {
					logger.Warn(logComponent).
						Str("type", fmt.Sprintf("%T", ret)).
						Msg("Empty or unexpected result type during link passages")
				}
				failedFlags[i] = true
				return
			}
			results[i] = ret
		}()
	}
	wg.Wait()

	var out [][]common.RetrievalResult
	failed := 0
	for i := range triples {
		if failedFlags[i] {
			failed++
			continue
		}
		out = append(out, results[i])
	}
	if failed > 0 {
		logger.Warn(logComponent).
			Int("failed", failed).
			Int("total", len(triples)).
			Msg("Link passages failed for triples")
	}
	return out, nil
}

// tripleFactStr 三元组转检索串（" ".join(map(str, triple))）。
func tripleFactStr(triple common.TripleFact) string {
	return strings.Join(triple[:], " ")
}
```

**核对点**：
1. Python `_link_triples` 的 Warn 日志两条路径：Exception → 只计数；非 list/空 → "Empty or Unexpected result type" + 计数。上面实现里 error 分支不单独打 "Empty"（对齐 Python：`isinstance(result, Exception)` 走 else 前的 if 分支，不打印 type 日志）。
2. `append(ret, historyResults...)` 注意 Go 陷阱：ret 是 `[][]RetrievalResult`、historyResults 是 `[][]RetrievalResult`，`append(ret, history...)` 语义即 Python `ret + history` ✅。但**不要** `append(historyResults, ret...)`（顺序反了，Python 是 `ret + history_results` ret 在前）。

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/retrieval/retriever/ -count=1 2>&1 | tail -3
```
预期：包内全量 PASS（含 13.23 既有 48+ 测试不回归）。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/retriever/agentic_retriever.go internal/agentcore/retrieval/retriever/agentic_retriever_test.go
git commit -m "feat(retriever): AgenticRetriever 图路径——retrieveWithGraph+linkTriples/linkPassages 裸 gather 三分类聚合"
```

---

### Task 5: Go 增强测试补全

**Files:**
- Test: `internal/agentcore/retrieval/retriever/agentic_retriever_test.go`

- [ ] **Step 1: 追加增强测试（软失败链/pyStr 边界/facts 形态/RRF 序/max_iter=1/race）**

```go
// ──────────────────────────── Go 增强测试 ────────────────────────────

// TestRead_pyStr边界 jsonrepair 数字三态 + nil（增强 2）。
func TestRead_pyStr边界(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) {
			// 1862 数字、true bool、null → None、长度≠3 过滤
			return `[["a", "b", 1862], ["x", "y", true], ["p", "q", null], ["short"], "notlist"]`, nil
		},
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	// 走一轮后从 prompt 抓 facts——改为直接测 read：
	results := r.read(context.Background(), "q", []common.RetrievalResult{{Text: "doc", Score: 1}}, nil)
	require.Len(t, results, 3)
	assert.Equal(t, "1862", results[0][2])
	assert.Equal(t, "True", results[1][2])
	assert.Equal(t, "None", results[2][2])
}

// TestRead_facts串形态 tuple repr + None 兜底（增强 3）。
func TestRead_facts串形态(t *testing.T) {
	var capturedPrompt string
	llm := &agenticLLMDispatch{
		readFn: func(prompt string) (string, error) {
			capturedPrompt = prompt
			return "[]", nil
		},
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	r.read(context.Background(), "q", []common.RetrievalResult{{Text: "doc", Score: 1}},
		[]common.TripleFact{{"s", "p", "o"}, {"S2", "P2", "O2"}})
	assert.Contains(t, capturedPrompt, "('s', 'p', 'o'), ('S2', 'P2', 'O2')")
	// 空时兜底
	r.read(context.Background(), "q", []common.RetrievalResult{{Text: "doc", Score: 1}}, nil)
	assert.Contains(t, capturedPrompt, "Facts: None")
}

// TestRead_docs截断与空文本过滤 前 5 + 跳过空（增强 3 附带）。
func TestRead_docs截断与空文本过滤(t *testing.T) {
	var capturedPrompt string
	llm := &agenticLLMDispatch{
		readFn: func(prompt string) (string, error) { capturedPrompt = prompt; return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	passages := []common.RetrievalResult{
		{Text: "p1"}, {Text: ""}, {Text: "p2"}, {Text: "p3"}, {Text: "p4"}, {Text: "p5"}, {Text: "p6"},
	}
	r.read(context.Background(), "q", passages, nil)
	assert.Contains(t, capturedPrompt, "p1")
	assert.NotContains(t, capturedPrompt, "p6") // 截前 5（跳过空后 p1-p5）
}

// TestRetrieve_软失败链 LLM err→降级继续；解析非 list/dict（增强 4）。
func TestRetrieve_软失败链(t *testing.T) {
	// LLM err → _read 返回 [] → 循环继续走完 → 返回 RRF 结果
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "", errors.New("llm down") },
		rewriteFn: func(string) (string, error) { return "", errors.New("llm down") },
	}
	r := NewAgenticRetriever(fr, llm, WithAgenticMaxIter(2))
	results, err := r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)

	// _read 解析非 list
	llm.readFn = func(string) (string, error) { return `{"not": "a list"}`, nil }
	results, err = r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)

	// _rewrite 解析非 dict
	llm.readFn = func(string) (string, error) { return "[]", nil }
	llm.rewriteFn = func(string) (string, error) { return `["not", "dict"]`, nil }
	results, err = r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestRetrieve_maxIter为1 单轮无 rewrite 调用（增强 8）。
func TestRetrieve_maxIter为1(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { t.Error("max_iter=1 不应调用 rewrite"); return "", errors.New("unreachable") },
	}
	r := newGenericRetriever(t, llm)
	r.maxIter = 1 // 同包直改（或 WithAgenticMaxIter(1)）
	_, err := r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	llm.mu.Lock()
	assert.Equal(t, 1, llm.readCalls)
	assert.Equal(t, 0, llm.rewriteCalls)
	llm.mu.Unlock()
}

// TestRetrieve_RRF融合序 图路径 ret 在前+history 在后（增强 9）。
func TestRetrieve_RRF融合序(t *testing.T) {
	// 通用路径验证 text 去重保首见：两轮同 text → 1 条；分数为 RRF 累加值
	callIdx := 0
	var mu sync.Mutex
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(_ context.Context, _ string, _ int, _ *float64, _ RetrievalMode) ([]common.RetrievalResult, error) {
			mu.Lock()
			defer mu.Unlock()
			callIdx++
			if callIdx == 1 {
				return []common.RetrievalResult{{Text: "same", Score: 0.9}}, nil
			}
			return []common.RetrievalResult{{Text: "same", Score: 0.8}}, nil // 同 text 不同 metadata
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": false, "next_question": "q2"}`, nil },
	}
	r := NewAgenticRetriever(fr, llm, WithAgenticMaxIter(2))
	results, err := r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1) // text 去重
	assert.InDelta(t, 1.0/61+1.0/61, results[0].Score, 1e-9) // 两轮 rank1 RRF 累加
}

// TestBatchRetrieve_并发race -race 下双 query 并发 + LLM 分派桩共享（增强 10）。
func TestBatchRetrieve_并发race(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "r", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := NewAgenticRetriever(fr, llm)
	_, err := r.BatchRetrieve(context.Background(), []string{"q1", "q2", "q3"}, 5)
	require.NoError(t, err)
}
```

- [ ] **Step 2: 跑测试（含 -race）确认通过**

```bash
go test ./internal/agentcore/retrieval/retriever/ -run 'TestRead_|TestRetrieve_|TestBatchRetrieve_|TestLink|TestClose' -race -count=1 2>&1 | tail -3
```
预期：全 PASS，race 干净。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/retrieval/retriever/agentic_retriever_test.go
git commit -m "test(retriever): AgenticRetriever 增强测试——软失败链/pyStr 边界/facts 形态/RRF 序/max_iter=1/race"
```

---

### Task 6: doc.go 更新 + 覆盖率 + 全量回归

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go**

文件目录加一行（graph_retriever_impl.go 之后）：

```
//	├── agentic_retriever.go   # AgenticRetriever 多跳 agentic 检索器（13.25，GeAR 策略）
```

核心类型索引加：

```
//	AgenticRetriever — 多跳 agentic 检索器（LLM 事实抽取 + 充分性驱动改写 + RRF 融合；图底层时启用图扩展）
```

包概述段落补一句 13.25 职责（对应 Python 参考路径列表加 `agentic_retriever.py`）。

- [ ] **Step 2: 覆盖率检查**

```bash
go test ./internal/agentcore/retrieval/retriever/ -cover -count=1 2>&1 | tail -2
```
预期：≥ 85%（spec 目标 92%+；未达标补分支用例——重点看 linkTriples/linkPassages 的失败分支与 rewrite 收窄分支）。

- [ ] **Step 3: 全量回归（CI 同款构建标签）**

```bash
pgrep -f 'go (build|test)' || true
export CGO_ENABLED=1
go test -tags "sqlite_fts5 test" ./... -count=1 2>&1 | grep -E 'FAIL|ok .*(retriever|retrieval)' | head -20
```
预期：零 FAIL；retrieval 相关包全 ok。跑前确认无残留 go 进程（编译纪律）。

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

1. 13.25 行：`☐` → `✅`，产出列填：`AgenticRetriever(构造类型断言判图路径+IndexTypeCarrier 推导 defaultMode / Retrieve top_k 155210+mode 零值回落 / retrieveGeneric 迭代改写+_read/_rewrite 软失败+RRF 融合 / retrieveWithGraph graph_expansion kwargs.pop 默认 true+主 read 传原 query 字面复刻 / linkTriples 裸 gather 三分类+Deduplicate ID 键+metadata 映射收窄 / linkPassages top_k=5 整列表 / pyStr 包内复刻 13.17 同源 / 错误码 155208-155211 零消费转已消费 / RetrieveOptions 新增 GraphExpansion *bool 潜伏激活字段(13.28 GraphKB 消费))`
2. 领域十三顶部备注追加：`> 2026-10-28：13.25 AgenticRetriever 完成（多跳 agentic 检索：图/通用双路径+GeAR 三件套+RRF 融合，覆盖率 XX%）。下一节 13.26 KnowledgeBase 抽象。设计：docs/superpowers/specs/2026-10-28-agenticretriever-13.25-design.md`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/retriever/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 13.25 AgenticRetriever 完成——doc.go 目录+IMPLEMENTATION_PLAN 状态同步"
```

---

## 实施注意事项（全任务通用）

1. **编译纪律**：每次 go test 前不需要 pgrep（短测试），但全量回归前必须查残留进程；GOPROXY 已配则跳过 export。
2. **git -am 陷阱**（记忆 feedback-git-am-stowaway-commits）：工作区有他任务未提交改动时**禁止 -am/-A**——本计划所有 commit 命令已用显式 `git add <files>`，保持这个习惯，提交后 `git show --stat HEAD` 核对无搭车文件。当前工作区已有 `internal/swarm/server/handle_initialize.go` 未提交改动（他任务），务必不要卷入。
3. **提示词逐字节**：readPrompt/rewritePrompt 从 Python L30-110 复制后跑 md5 对照（13.24 纪律）。
4. **核对锚点**：Task 2 Step 3 的三个核对点（BuildError 返回值形态/MessagesParam 取 prompt 方法/Retriever_ 命名）在写码时逐一 grep 确认，与先例统一。
5. **日志文案全英文**、注释全中文、文件声明序按项目规范（结构体→常量→导出→非导出）。
6. **实施后审查**：全部任务完成后按惯例跑实施后审查（对照 Python 源码逐方法核语义+偏差表核对+循环依赖检查），发现偏差按 review-doc-workflow 记忆的流程修复归档。
