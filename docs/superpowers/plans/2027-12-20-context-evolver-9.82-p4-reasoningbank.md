# Context Evolver 9.82 P4 ReasoningBank Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.82 P4 ReasoningBank 算法，包括 RB 核心 Op、MaTTS 4 个 Op、trajectory_generator 函数，以及 P3 目录回填和 prompt 模板统一改造。

**Architecture:** 新增 AgentFlowService 接口到 ServiceContext，实现 RB retrieve/summary Op 链、MaTTS Op 和独立 trajectory_generator 函数。P3 回填将 reme/ 目录迁移到 task/reme/ 并将 prompt 从 strings.ReplaceAll 改为 text/template。

**Tech Stack:** Go 1.x, text/template, 已有 context_evolver 核心框架 (P1/P2/P3)

**Design Spec:** `docs/superpowers/specs/2027-12-20-context-evolver-9.82-p4-reasoningbank-design.md`

**Python Reference:** `openjiuwen/extensions/context_evolver/` (at `/home/opensource/agent-core/openjiuwen/`)

---

## Task 1: 核心层变更 — AgentFlowService

**Files:**
- Modify: `internal/agentcore/context_evolver/core/context/service_context.go`
- Modify: `internal/agentcore/context_evolver/core/op/base_op.go`
- Test: `internal/agentcore/context_evolver/core/context/service_context_test.go`
- Test: `internal/agentcore/context_evolver/core/op/base_op_test.go`

- [ ] **Step 1: 在 service_context.go 中添加 AgentFlowService 接口和 TrajectoryResult 结构体**

在 `VectorStoreService` 接口之后、`// ──────────────────────────── 结构体 ────────────────────────────` 之前添加：

```go
// AgentFlowService Agent 执行服务接口。
// MaTTS 的 ParallelScalingOp 通过此接口执行 Agent 轨迹，
// 解耦 Op 与具体 Agent 实现的依赖。
type AgentFlowService interface {
	// Execute 执行一次 Agent 推理，返回轨迹结果。
	Execute(ctx context.Context, query string, sessionID string) (*TrajectoryResult, error)
}

// TrajectoryResult 单次 Agent 执行的轨迹结果。
type TrajectoryResult struct {
	// Answer Agent 最终回答
	Answer string
	// Steps 执行步骤数
	Steps []any
	// Success 是否成功
	Success bool
	// Trajectory 格式化后的轨迹文本
	Trajectory string
}
```

- [ ] **Step 2: 在 service_context.go 中添加 AgentFlow()/RegisterAgentFlow() 方法**

在 `VectorStore()` 方法之后添加：

```go
// AgentFlow 获取 Agent 执行服务。
// 未注册或类型不匹配时返回 nil。
func (sc *ServiceContext) AgentFlow() AgentFlowService {
	svc := sc.GetService("agent_flow")
	if svc == nil {
		return nil
	}
	af, ok := svc.(AgentFlowService)
	if !ok {
		return nil
	}
	return af
}

// RegisterAgentFlow 注册 Agent 执行服务。
func (sc *ServiceContext) RegisterAgentFlow(af AgentFlowService) {
	sc.RegisterService("agent_flow", af)
}
```

- [ ] **Step 3: 在 base_op.go 中添加 AgentFlow() 方法**

在 `VectorStore()` 方法之后添加：

```go
// AgentFlow 返回 Agent 执行服务。未注册时返回 nil。
func (b *OpBase) AgentFlow() cecontext.AgentFlowService {
	if b.sc == nil {
		return nil
	}
	return b.sc.AgentFlow()
}
```

- [ ] **Step 4: 编写 service_context_test.go 中的 AgentFlow 相关测试**

添加测试用例：
- `TestServiceContext_AgentFlow_未注册` — 返回 nil
- `TestServiceContext_AgentFlow_已注册` — 返回正确的 AgentFlowService
- `TestServiceContext_AgentFlow_类型不匹配` — 返回 nil
- `TestServiceContext_RegisterAgentFlow` — 注册后 AgentFlow() 返回非 nil

- [ ] **Step 5: 编写 base_op_test.go 中的 AgentFlow 相关测试**

添加测试用例：
- `TestOpBase_AgentFlow_未注册` — 返回 nil
- `TestOpBase_AgentFlow_已注册` — 返回正确服务

- [ ] **Step 6: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/context/... ./internal/agentcore/context_evolver/core/op/... -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/context_evolver/core/context/service_context.go internal/agentcore/context_evolver/core/op/base_op.go internal/agentcore/context_evolver/core/context/service_context_test.go internal/agentcore/context_evolver/core/op/base_op_test.go
git commit -m "feat(9.82-p4): 添加 AgentFlowService 接口到 ServiceContext 和 OpBase"
```

---

## Task 2: P3 回填 — 目录迁移 (retrieve/reme → retrieve/task/reme)

**Files:**
- Move: `internal/agentcore/context_evolver/retrieve/reme/*` → `internal/agentcore/context_evolver/retrieve/task/reme/*`
- Modify: 所有文件中的 package 声明保持 `package reme`
- Modify: 所有 import 路径从 `.../retrieve/reme` → `.../retrieve/task/reme`
- Modify: doc.go 中的文件目录段

- [ ] **Step 1: 创建目标目录并移动文件**

```bash
mkdir -p internal/agentcore/context_evolver/retrieve/task/reme
cd internal/agentcore/context_evolver/retrieve
git mv reme/doc.go task/reme/doc.go
git mv reme/prompt.go task/reme/prompt.go
git mv reme/prompt_test.go task/reme/prompt_test.go
git mv reme/run.go task/reme/run.go
git mv reme/run_test.go task/reme/run_test.go
git mv reme/utils.go task/reme/utils.go
git mv reme/utils_test.go task/reme/utils_test.go
```

- [ ] **Step 2: 更新所有 import 路径**

在项目中搜索 `context_evolver/retrieve/reme` 的 import，替换为 `context_evolver/retrieve/task/reme`。涉及文件：
- `retrieve/task/reme/` 内部文件间的 import
- `summary/reme/` 中对 retrieve 包的引用（如有）
- 其他包中对 `retrieve/reme` 的引用

使用 `grep -rn "context_evolver/retrieve/reme" --include="*.go"` 查找所有引用。

- [ ] **Step 3: 更新 doc.go 文件目录段**

将 doc.go 中的文件目录从 `reme/` 改为 `task/reme/`。

- [ ] **Step 4: 删除空的旧目录**

```bash
rmdir internal/agentcore/context_evolver/retrieve/reme
```

- [ ] **Step 5: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/retrieve/task/reme/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add -A
git commit -m "refactor(9.82): 迁移 retrieve/reme → retrieve/task/reme 对齐 Python 目录结构"
```

---

## Task 3: P3 回填 — 目录迁移 (summary/reme → summary/task/reme)

**Files:**
- Move: `internal/agentcore/context_evolver/summary/reme/*` → `internal/agentcore/context_evolver/summary/task/reme/*`
- Modify: 所有 import 路径

- [ ] **Step 1: 创建目标目录并移动文件**

```bash
mkdir -p internal/agentcore/context_evolver/summary/task/reme
cd internal/agentcore/context_evolver/summary
git mv reme/doc.go task/reme/doc.go
git mv reme/prompt.go task/reme/prompt.go
git mv reme/prompt_test.go task/reme/prompt_test.go
git mv reme/update.go task/reme/update.go
git mv reme/update_test.go task/reme/update_test.go
git mv reme/utils.go task/reme/utils.go
git mv reme/utils_test.go task/reme/utils_test.go
```

- [ ] **Step 2: 更新所有 import 路径**

搜索 `context_evolver/summary/reme` 替换为 `context_evolver/summary/task/reme`。

- [ ] **Step 3: 更新 doc.go 文件目录段**

- [ ] **Step 4: 删除空的旧目录**

```bash
rmdir internal/agentcore/context_evolver/summary/reme
```

- [ ] **Step 5: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/reme/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add -A
git commit -m "refactor(9.82): 迁移 summary/reme → summary/task/reme 对齐 Python 目录结构"
```

---

## Task 4: P3 回填 — prompt 模板改造 (retrieve/task/reme)

**Files:**
- Modify: `internal/agentcore/context_evolver/retrieve/task/reme/prompt.go`
- Modify: `internal/agentcore/context_evolver/retrieve/task/reme/prompt_test.go`
- Modify: `internal/agentcore/context_evolver/retrieve/task/reme/run.go`
- Modify: `internal/agentcore/context_evolver/retrieve/task/reme/run_test.go` (如有 prompt 相关断言)

- [ ] **Step 1: 改造 prompt.go — 将 ReMeRetrievePrompts 字段类型从 string 改为 *template.Template**

```go
import "text/template"

// ReMeRetrievePrompts ReMe 检索管线的提示词配置。
type ReMeRetrievePrompts struct {
	// RerankPrompt 重排序提示词模板
	RerankPrompt *template.Template
	// RewritePrompt 改写提示词模板
	RewritePrompt *template.Template
}
```

将 `NewReMeRetrievePrompts()` 构造函数改为解析模板：

```go
func NewReMeRetrievePrompts() *ReMeRetrievePrompts {
	return &ReMeRetrievePrompts{
		RerankPrompt:  template.Must(template.New("rerank").Parse(memoryRerankPrompt)),
		RewritePrompt: template.Must(template.New("rewrite").Parse(memoryRewritePrompt)),
	}
}
```

将 const 中的占位符从 `{query}` 格式改为 `{{.Query}}` 格式。

- [ ] **Step 2: 改造 run.go — 将 strings.ReplaceAll 调用改为 template.Execute**

例如 RerankMemoryOp.Execute 中：

```go
// 改造前：
prompt := op.prompts.RerankPrompt
prompt = strings.ReplaceAll(prompt, "{query}", query)
prompt = strings.ReplaceAll(prompt, "{num_candidates}", fmt.Sprintf("%d", len(retrieved)))
prompt = strings.ReplaceAll(prompt, "{candidates}", candidates)

// 改造后：
var buf bytes.Buffer
op.prompts.RerankPrompt.Execute(&buf, map[string]any{
    "Query":          query,
    "NumCandidates":  len(retrieved),
    "Candidates":     candidates,
})
prompt := buf.String()
```

对 RewritePrompt 做同样改造。

- [ ] **Step 3: 改造 prompt_test.go — 适配 template 测试方式**

将测试从 `strings.ReplaceAll` 改为 `template.Execute`：

```go
func TestRerankPrompt_格式化(t *testing.T) {
    prompts := NewReMeRetrievePrompts()
    var buf bytes.Buffer
    err := prompts.RerankPrompt.Execute(&buf, map[string]any{
        "Query":         "test query",
        "NumCandidates": 3,
        "Candidates":    "candidate text",
    })
    // 验证
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/retrieve/task/reme/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "refactor(9.82): retrieve/task/reme prompt 从 strings.ReplaceAll 改为 text/template"
```

---

## Task 5: P3 回填 — prompt 模板改造 (summary/task/reme)

**Files:**
- Modify: `internal/agentcore/context_evolver/summary/task/reme/prompt.go`
- Modify: `internal/agentcore/context_evolver/summary/task/reme/prompt_test.go`
- Modify: `internal/agentcore/context_evolver/summary/task/reme/update.go`

- [ ] **Step 1: 改造 prompt.go — 将 ReMeSummaryPrompts 字段类型从 string 改为 *template.Template**

将所有 5 个字段改为 `*template.Template`，添加 `NewReMeSummaryPrompts()` 构造函数解析模板。
将 const 中的占位符改为 `{{.Xxx}}` 格式。

- [ ] **Step 2: 改造 update.go — 将 strings.ReplaceAll 调用改为 template.Execute**

对所有 prompt 填充点做同样改造（SuccessExtractionOp、FailureExtractionOp、ComparativeExtractionOp 等）。

- [ ] **Step 3: 改造 prompt_test.go — 适配 template 测试方式**

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/reme/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "refactor(9.82): summary/task/reme prompt 从 strings.ReplaceAll 改为 text/template"
```

---

## Task 6: RB Retrieve — prompt.go

**Files:**
- Create: `internal/agentcore/context_evolver/retrieve/task/rb/doc.go`
- Create: `internal/agentcore/context_evolver/retrieve/task/rb/prompt.go`
- Create: `internal/agentcore/context_evolver/retrieve/task/rb/prompt_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package rb 提供 ReasoningBank 算法的检索管线和 MaTTS 操作。
//
// ReasoningBank 通过向量搜索检索推理策略记忆，检索路径比 ReMe 更简单
// （无重排/改写步骤）。MaTTS（Memory-aware Test-Time Scaling）提供
// 并行/串行多轨迹缩放和自对比记忆提取能力。
//
// 文件目录：
//
//	rb/
//	├── doc.go           # 包文档
//	├── run.go           # RBRecallMemoryOp 检索操作
//	├── matts.go         # MaTTS 操作（ParallelScalingOp/SequentialScalingOp/BestOfNOp/SelfContrastMemoryOp）
//	└── prompt.go        # 检索和 MaTTS 相关提示词模板
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/retrieve/task/reasoning_bank/
package rb
```

- [ ] **Step 2: 创建 prompt.go**

定义 6 个 prompt 模板常量（一比一复刻 Python），定义 `ReasoningBankPrompt` 结构体（持有 `*template.Template`），实现 `NewReasoningBankPrompt()` 构造函数。

模板常量包括：
- `llmJudgeSystemPrompt` — 无占位符
- `llmJudgeUserPrompt` — `{{.Query}}` `{{.Trajectory}}`
- `bestOfNEvalPrompt` — `{{.NumTrajectories}}` `{{.Query}}` `{{.TrajDescriptions}}` `{{.MaxIndex}}`
- `selfContrastPrompt` — `{{.Query}}` `{{.NumSuccessful}}` `{{.SuccessfulTrajectories}}` `{{.NumFailed}}` `{{.FailedTrajectories}}`
- `sequentialFirstRefinePrompt` — `{{.CurrentAnswer}}` `{{.Query}}`
- `sequentialFollowUpRefinePrompt` — `{{.CurrentAnswer}}` `{{.Query}}`

Python 原文参考路径：
- LLM Judge prompt: `/home/opensource/agent-core/openjiuwen/extensions/context_evolver/summary/task/reasoning_bank/prompt.py`
- BestOfN prompt: `/home/opensource/agent-core/openjiuwen/extensions/context_evolver/retrieve/task/reasoning_bank/matts.py` BestOfNOp.async_execute
- SelfContrast prompt: 同上 SelfContrastMemoryOp.async_execute
- Sequential prompts: 同上 SequentialScalingOp.async_execute

- [ ] **Step 3: 创建 prompt_test.go**

测试 `NewReasoningBankPrompt()` 不 panic，每个模板 Execute 输出包含预期的占位符替换结果。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/retrieve/task/rb/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 添加 retrieve/task/rb prompt 模板"
```

---

## Task 7: RB Retrieve — RBRecallMemoryOp

**Files:**
- Create: `internal/agentcore/context_evolver/retrieve/task/rb/run.go`
- Create: `internal/agentcore/context_evolver/retrieve/task/rb/run_test.go`

- [ ] **Step 1: 编写 run_test.go 失败测试**

测试用例：
- `TestRBRecallMemoryOp_正常检索` — mock Embedding+VectorStore，验证 retrieved_memories 正确
- `TestRBRecallMemoryOp_Embedding未注册` — 返回 error
- `TestRBRecallMemoryOp_VectorStore未注册` — 返回 error
- `TestRBRecallMemoryOp_默认UserID` — user_id 为空时使用 "default"
- `TestRBRecallMemoryOp_向量节点转换失败` — Warn 日志跳过，其余正常

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/retrieve/task/rb/... -v`
Expected: FAIL (编译错误)

- [ ] **Step 3: 实现 run.go**

```go
// RBRecallMemoryOp ReasoningBank 记忆检索操作。
type RBRecallMemoryOp struct {
    op.OpBase
    topK int
}

func NewRBRecallMemoryOp(sc *cecontext.ServiceContext, topK int) *RBRecallMemoryOp

func (op *RBRecallMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error
```

Execute 流程对照 Python `RecallMemoryOp.async_execute()`:
1. 获取 query 和 user_id
2. 校验 EmbeddingModel 和 VectorStore
3. `EmbeddingModel.Embed(ctx, query)`
4. `VectorStore.Search(ctx, embedding, topK, filter)`
5. 遍历 VectorNode → `NewReasoningBankMemoryFromVectorNode` → 取 Memory 字段构建 `[]ReasoningBankRetrievedMemory`
6. `rc.Set("retrieved_memories", retrieved)`
7. 日志：Debug(embedding)、Debug(search)、Warn(转换失败)、Info(结果数)

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/retrieve/task/rb/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 实现 RBRecallMemoryOp"
```

---

## Task 8: RB Summary — prompt.go

**Files:**
- Create: `internal/agentcore/context_evolver/summary/task/rb/doc.go`
- Create: `internal/agentcore/context_evolver/summary/task/rb/prompt.go`
- Create: `internal/agentcore/context_evolver/summary/task/rb/prompt_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package rb 提供 ReasoningBank 算法的摘要管线操作。
//
// 包含轨迹标签判定（LabelDeterminator）、记忆项解析（MemoryItemParser）、
// 单轨迹/多轨迹策略提取（SummarizeMemoryOp/SummarizeMemoryParallelOp）、
// 向量存储更新和持久化操作。
//
// 文件目录：
//
//	rb/
//	├── doc.go           # 包文档
//	├── update.go        # SummarizeMemoryOp + SummarizeMemoryParallelOp + UpdateVectorStoreOp + PersistMemoryOp
//	├── label.go         # LabelDeterminator 轨迹标签判定器
//	├── parser.go        # MemoryItemParser 记忆项解析器
//	└── prompt.go        # 摘要相关提示词模板
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/summary/task/reasoning_bank/
package rb
```

- [ ] **Step 2: 创建 prompt.go**

定义 7 个 prompt 模板常量（一比一复刻 Python），定义 `ReasoningBankSummaryPrompt` 结构体，实现 `NewReasoningBankSummaryPrompt()` 构造函数。

Python 原文参考路径：`/home/opensource/agent-core/openjiuwen/extensions/context_evolver/summary/task/reasoning_bank/prompt.py`

模板包括：
- `extractSuccessTrajSystemPrompt` — 无占位符
- `extractFailTrajSystemPrompt` — 无占位符
- `extractTrajUserPrompt` — `{{.Query}}` `{{.Trajectory}}`
- `llmJudgeSystemPrompt` — 无占位符
- `llmJudgeUserPrompt` — `{{.Query}}` `{{.Trajectory}}`
- `parallelScalingSystemPrompt` — 无占位符
- `parallelScalingUserPrompt` — `{{.Query}}` `{{.Trajectories}}`

- [ ] **Step 3: 创建 prompt_test.go**

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/rb/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 添加 summary/task/rb prompt 模板"
```

---

## Task 9: RB Summary — LabelDeterminator

**Files:**
- Create: `internal/agentcore/context_evolver/summary/task/rb/label.go`
- Create: `internal/agentcore/context_evolver/summary/task/rb/label_test.go`

- [ ] **Step 1: 编写 label_test.go 失败测试**

测试用例：
- `TestLabelDeterminator_判断成功` — mock LLM 返回含 "Status: success" 的响应
- `TestLabelDeterminator_判断失败` — mock LLM 返回含 "Status: failure" 的响应
- `TestLabelDeterminator_正则不匹配回退` — mock LLM 返回含 "success" 但无 Status: 行
- `TestLabelDeterminator_LLM调用失败` — 返回 error

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 label.go**

```go
type LabelDeterminator struct {
    sc *cecontext.ServiceContext
}

func NewLabelDeterminator(sc *cecontext.ServiceContext) *LabelDeterminator

func (d *LabelDeterminator) DetermineLabel(ctx context.Context, query string, trajectory string) (bool, error)
```

对照 Python `LabelDeterminator.determine_label()`:
1. 获取 LLM
2. 用 `llmJudgeUserPrompt` 模板填充 Query+Trajectory
3. 调用 `LLM.Generate(ctx, userPrompt, WithSystemPrompt(...))`
4. 正则 `(?i)Status:\s*(success|failure)` 解析
5. 回退：检查响应中是否包含 "success"

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 实现 LabelDeterminator"
```

---

## Task 10: RB Summary — MemoryItemParser

**Files:**
- Create: `internal/agentcore/context_evolver/summary/task/rb/parser.go`
- Create: `internal/agentcore/context_evolver/summary/task/rb/parser_test.go`

- [ ] **Step 1: 编写 parser_test.go 失败测试**

测试用例：
- `TestMemoryItemParser_Parse_正常解析` — 标准 Markdown 输入，验证返回 []*ReasoningBankMemory
- `TestMemoryItemParser_Parse_代码围栏` — 输入被 ``` 包裹，验证 cleanResponse 去除
- `TestMemoryItemParser_Parse_多段落` — 多个 # Memory Item N 段落
- `TestMemoryItemParser_Parse_缺少字段` — 缺 Title/Description/Content 之一的段落返回 nil
- `TestMemoryItemParser_Parse_空响应` — 返回空列表
- `TestMemoryItemParser_CleanResponse` — 验证去除 ``` 围栏
- `TestMemoryItemParser_SplitIntoSections` — 验证分割
- `TestMemoryItemParser_ExtractMemoryItem` — 验证单项提取
- `TestMemoryItemParser_ExtractField` — 验证字段提取

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 parser.go**

```go
type MemoryItemParser struct{}

func NewMemoryItemParser() *MemoryItemParser
func (p *MemoryItemParser) Parse(llmResponse string, query string, label *bool) []*schema.ReasoningBankMemory
func (p *MemoryItemParser) cleanResponse(response string) string
func (p *MemoryItemParser) splitIntoSections(response string) []string
func (p *MemoryItemParser) extractMemoryItem(section string) *schema.ReasoningBankMemoryItem
func (p *MemoryItemParser) extractField(lines []string, startIdx int, fieldPattern string) (string, int)
```

对照 Python `MemoryItemParser` 的 5 个方法逐一实现。

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 实现 MemoryItemParser"
```

---

## Task 11: RB Summary — SummarizeMemoryOp + SummarizeMemoryParallelOp + UpdateVectorStoreOp + PersistMemoryOp

**Files:**
- Create: `internal/agentcore/context_evolver/summary/task/rb/update.go`
- Create: `internal/agentcore/context_evolver/summary/task/rb/update_test.go`

- [ ] **Step 1: 编写 update_test.go 失败测试**

测试用例：
- `TestSummarizeMemoryOp_matts为none` — mock LLM，验证 memories 写入 rc
- `TestSummarizeMemoryOp_matts为sequential` — 同上
- `TestSummarizeMemoryOp_matts不匹配` — matts="parallel" 时返回 error
- `TestSummarizeMemoryOp_label从rc获取` — rc 已有 label
- `TestSummarizeMemoryOp_label通过LLM判断` — rc 无 label，调 LabelDeterminator
- `TestSummarizeMemoryParallelOp_matts为parallel` — mock LLM，验证
- `TestSummarizeMemoryParallelOp_matts为combined` — 同上
- `TestSummarizeMemoryParallelOp_轨迹不足` — 只有1条轨迹时返回 error
- `TestSummarizeMemoryParallelOp_matts不匹配` — 返回 error
- `TestUpdateVectorStoreOp_正常更新` — mock Embedding+VectorStore
- `TestUpdateVectorStoreOp_服务未注册` — 返回 error
- `TestPersistMemoryOp_正常持久化` — mock VectorStore+PersistenceHelper
- `TestPersistMemoryOp_VectorStore未注册` — 返回 error

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 update.go**

对照 Python `update.py` 实现 4 个 Op。注意：
- `SummarizeMemoryOp` 内部创建 `LabelDeterminator` 和 `MemoryItemParser`
- `SummarizeMemoryParallelOp` 内部创建 `MemoryItemParser`
- `UpdateVectorStoreOp` 对齐 ReMe 的同名 Op 但 metadata type 不同
- `PersistMemoryOp` algoName="rb"，对齐 ReMe 但 metadata type 不同

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 实现 RB summary Op (Summarize/Update/Persist)"
```

---

## Task 12: MaTTS — 4 个 Op (matts.go)

**Files:**
- Create: `internal/agentcore/context_evolver/retrieve/task/rb/matts.go`
- Create: `internal/agentcore/context_evolver/retrieve/task/rb/matts_test.go`

- [ ] **Step 1: 编写 matts_test.go 失败测试**

测试用例：
- `TestParallelScalingOp_正常执行` — mock AgentFlowService，验证 parallel_trajectories
- `TestParallelScalingOp_AgentFlow未注册` — Warn 日志跳过
- `TestParallelScalingOp_部分轨迹失败` — 某条 AgentFlow.Execute 返回 error，跳过
- `TestSequentialScalingOp_正常执行` — mock LLM，验证 refinement_history 和 refined_answer
- `TestSequentialScalingOp_无当前答案` — 从 rc 获取 answer 为空
- `TestBestOfNOp_正常选择` — mock LLM 返回索引，验证 best_trajectory
- `TestBestOfNOp_索引解析失败` — 回退到第 0 条
- `TestBestOfNOp_无parallel_trajectories` — 返回 error
- `TestSelfContrastMemoryOp_正常提取` — mock LLM 返回 Markdown，验证 contrastive_memories
- `TestSelfContrastMemoryOp_全部成功无失败` — failed 列表为空
- `TestSelfContrastMemoryOp_无parallel_trajectories` — 返回 error

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 matts.go**

4 个 Op 对照 Python `matts.py` 实现。注意：
- `ParallelScalingOp`: 保存/恢复 LLM temperature，循环 k 次调 `o.AgentFlow().Execute()`
- `SequentialScalingOp`: 第1轮和后续轮用不同 prompt，调 `o.LLM().Generate()`
- `BestOfNOp`: 构建 eval prompt（含所有轨迹描述），LLM temperature=0.0，正则解析索引
- `SelfContrastMemoryOp`: 分 success/failed，LLM temperature=1.0，解析 Markdown

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 实现 MaTTS 4 个 Op (Parallel/Sequential/BestOfN/SelfContrast)"
```

---

## Task 13: trajectory_generator 函数

**Files:**
- Create: `internal/agentcore/context_evolver/service/doc.go`
- Create: `internal/agentcore/context_evolver/service/trajectory_generator.go`
- Create: `internal/agentcore/context_evolver/service/trajectory_generator_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package service 提供 Context Evolver 的服务层函数。
//
// 包含轨迹格式化、试验评估、MaTTS 试验运行等独立函数，
// 供 ContextEvolutionRail 和其他上层调用方使用。
//
// 文件目录：
//
//	service/
//	├── doc.go                     # 包文档
//	└── trajectory_generator.go    # 轨迹生成和 MaTTS 试验函数
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/service/trajectory_generator.py
package service
```

- [ ] **Step 2: 编写 trajectory_generator_test.go 失败测试**

测试用例：
- `TestFormatTrajectory_正常格式化` — 构造消息列表，验证 USER/THOUGHT/ACTION/OBSERVATION 格式
- `TestFormatTrajectory_去除记忆注入块` — 验证去除 "Some Related Experience" 块
- `TestFormatTrajectory_去除Task前缀` — 验证去除 "Task:" 前缀
- `TestEvaluateTrial_有GroundTruth成功` — 子串匹配成功
- `TestEvaluateTrial_有GroundTruth失败` — 子串匹配失败
- `TestEvaluateTrial_无GroundTruth` — 默认成功
- `TestRunTrials_none模式` — 1次执行
- `TestRunTrials_parallel模式` — k次独立执行
- `TestRunTrials_sequential模式` — k次自我修正
- `TestRunTrials_AgentFlow失败` — 某条失败跳过

- [ ] **Step 3: 运行测试验证失败**

- [ ] **Step 4: 实现 trajectory_generator.go**

对照 Python `trajectory_generator.py` 实现：
- `formatTrajectory(messages)` — 按 USER/THOUGHT/ACTION/OBSERVATION 格式化
- `EvaluateTrial(question, output, groundTruth)` — 评估单次试验
- `RunTrials(ctx, agent, params)` — MaTTS 试验入口
- `runTrialsInner(ctx, agent, question, groundTruth, mattsK, selfRefine)` — 内部循环

`SummarizeTrajectories` 函数先定义签名（依赖 TaskMemoryService），方法体返回 `fmt.Errorf("not implemented: depends on TaskMemoryService (P6)")`。

- [ ] **Step 5: 运行测试验证通过**

- [ ] **Step 6: 提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 实现 trajectory_generator (formatTrajectory/EvaluateTrial/RunTrials)"
```

---

## Task 14: 全局编译验证和覆盖率检查

**Files:** 无新增

- [ ] **Step 1: 检查残留 go 进程**

Run: `pgrep -f 'go (build|test)'`

- [ ] **Step 2: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 无编译错误

- [ ] **Step 3: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_evolver/... -v`
Expected: 全部 PASS

- [ ] **Step 4: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/context_evolver/retrieve/task/rb/... ./internal/agentcore/context_evolver/summary/task/rb/... ./internal/agentcore/context_evolver/service/... ./internal/agentcore/context_evolver/core/context/... ./internal/agentcore/context_evolver/core/op/...`
Expected: 各包覆盖率 ≥ 85%

- [ ] **Step 5: 补充覆盖率不足的测试**（如有）

- [ ] **Step 6: 更新 IMPLEMENTATION_PLAN.md 中 9.82 P4 状态**

将 P4 从 `☐` 改为 `✅`，回填项标注完成。

- [ ] **Step 7: 最终提交**

```bash
git add -A
git commit -m "feat(9.82-p4): 完成 ReasoningBank P4 实现（RB Op + MaTTS Op + trajectory_generator + P3 回填）"
```

---

## Self-Review

### 1. Spec Coverage

| Spec Section | Task |
|---|---|
| 1. 决策汇总 | 全部决策已在 Task 1-13 中体现 |
| 2. 目录结构 | Task 2-3 (P3 迁移), Task 6,8,13 (P4 新增) |
| 3. AgentFlowService | Task 1 |
| 4. RB Retrieve | Task 6-7 |
| 5. MaTTS 4 Op | Task 12 |
| 6. RB Summary | Task 8-11 |
| 7. trajectory_generator | Task 13 |
| 8. Prompt 模板 | Task 4-6-8 (P3 回填+P4 新增) |
| 9. 错误处理 | 各 Task 实现中体现 |
| 10. 测试策略 | 各 Task 的测试文件 |
| 11. 实现顺序 | Task 1-14 按序 |

### 2. Placeholder Scan

无 TBD/TODO/占位符。SummarizeTrajectories 签名已明确标注 P6 实现。

### 3. Type Consistency

- `AgentFlowService` 接口在 Task 1 定义，Task 12/13 使用一致
- `TrajectoryResult` 在 Task 1 定义，Task 12/13 使用一致
- `ReasoningBankPrompt` 在 Task 6 定义，Task 7/12 使用
- `ReasoningBankSummaryPrompt` 在 Task 8 定义，Task 9/11 使用
- `MemoryItemParser` 在 Task 10 定义，Task 11 使用
- `LabelDeterminator` 在 Task 9 定义，Task 11 使用
