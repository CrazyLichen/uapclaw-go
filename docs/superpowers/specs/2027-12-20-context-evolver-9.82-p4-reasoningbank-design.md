# Context Evolver 9.82 P4 ReasoningBank 设计文档

## 概述

本文档描述 9.82 P4 ReasoningBank 算法的 Go 实现设计，包括 RB 核心 Op、MaTTS Op、trajectory_generator 函数、P3 目录回填以及 prompt 模板统一改造。

## 1. 决策汇总

| # | 决策 | 选择 |
|---|------|------|
| 1 | MaTTS 范围 | 完整实现 4 个 MaTTS Op |
| 2 | AgentFlowService 位置 | 放在 ServiceContext 上，与 LLM/Embedding/VectorStore 同级 |
| 3 | 目录结构 | 加回 `task/` 层级：`retrieve/task/rb/` + `summary/task/rb/`；P3 回填：`retrieve/task/reme/` + `summary/task/reme/` |
| 4 | P3 回填范围 | 只做目录移动 + import 更新 |
| 5 | SummarizeMemoryOp/ParallelOp | 照搬 Python，两个 Op 各自判断 matts 值 |
| 6 | UpdateVectorStoreOp/PersistMemoryOp | RB 包下独立实现，不复用 ReMe 的 |
| 7 | LabelDeterminator | 独立结构体 + 方法 |
| 8 | MemoryItemParser | 独立结构体 + 方法 |
| 9 | messages_to_text | 不需要实现，Trajectory 已是 `[]string` |
| 10 | MaTTS 实现路径 | 两者都实现：4 个 MaTTS Op + trajectory_generator 函数，独立实现不互相委托 |
| 11 | trajectory_generator 位置 | 新建 `service/` 目录，与 Python 对齐 |
| 12 | 提前实现章节 | 不提前，P4 只实现 Op + 函数 |
| 13 | Prompt 模板 | 统一用 `text/template`，P3 回填时一起改 |
| 14 | AgentFlowService 访问 | 通过 OpBase 的 `o.AgentFlow()` 访问 |

## 2. 目录结构与文件清单

### 2.1 P3 回填（目录迁移）

```
现有：
  internal/agentcore/context_evolver/
    retrieve/reme/          → 移动到  retrieve/task/reme/
    summary/reme/           → 移动到  summary/task/reme/

迁移后：
  internal/agentcore/context_evolver/
    retrieve/task/reme/     ← doc.go + run.go + prompt.go + utils.go + 测试
    summary/task/reme/      ← doc.go + update.go + prompt.go + utils.go + 测试
```

同步更新：所有 import 路径、doc.go 文件目录段。

### 2.2 P4 新增

```
internal/agentcore/context_evolver/
├── retrieve/task/rb/
│   ├── doc.go                # 包文档
│   ├── run.go                # RBRecallMemoryOp
│   ├── matts.go              # ParallelScalingOp + SequentialScalingOp + BestOfNOp + SelfContrastMemoryOp
│   ├── prompt.go             # ReasoningBankPrompt 结构体 + 检索/MaTTS 相关 prompt 模板
│   ├── run_test.go           # RBRecallMemoryOp 测试
│   ├── matts_test.go         # 4 个 MaTTS Op 测试
│   └── prompt_test.go        # prompt 模板测试
│
├── summary/task/rb/
│   ├── doc.go                # 包文档
│   ├── update.go             # SummarizeMemoryOp + SummarizeMemoryParallelOp + UpdateVectorStoreOp + PersistMemoryOp
│   ├── label.go              # LabelDeterminator 结构体
│   ├── parser.go             # MemoryItemParser 结构体
│   ├── prompt.go             # 摘要相关 prompt 模板
│   ├── update_test.go        # summary Op 测试
│   ├── label_test.go         # LabelDeterminator 测试
│   ├── parser_test.go        # MemoryItemParser 测试
│   └── prompt_test.go        # prompt 模板测试
│
├── service/
│   ├── doc.go                # 包文档
│   ├── trajectory_generator.go  # formatTrajectory + EvaluateTrial + RunTrials + SummarizeTrajectories + 数据结构
│   └── trajectory_generator_test.go
```

### 2.3 核心层变更

```
internal/agentcore/context_evolver/core/
├── context/service_context.go   # 新增 AgentFlowService 接口 + AgentFlow() 访问器 + RegisterAgentFlow()
├── op/base_op.go                # 新增 o.AgentFlow() 方法
```

### 2.4 P3 回填（prompt 模板改造）

```
retrieve/task/reme/prompt.go     # strings.ReplaceAll → text/template
retrieve/task/reme/run.go        # 调用方式适配 template.Execute
summary/task/reme/prompt.go      # strings.ReplaceAll → text/template
summary/task/reme/update.go      # 调用方式适配 template.Execute
```

## 3. 核心层变更 — AgentFlowService

### 3.1 接口定义

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

### 3.2 ServiceContext 变更

```go
type ServiceContext struct {
    llm             LLMService
    embeddingModel  EmbeddingService
    vectorStore     VectorStoreService
    agentFlow       AgentFlowService  // 新增
}

// AgentFlow 返回 Agent 执行服务，未注册时返回 nil。
func (sc *ServiceContext) AgentFlow() AgentFlowService

// RegisterAgentFlow 注册 Agent 执行服务。
func (sc *ServiceContext) RegisterAgentFlow(af AgentFlowService)
```

### 3.3 OpBase 变更

```go
// AgentFlow 返回 Agent 执行服务。
func (ob *OpBase) AgentFlow() cecontext.AgentFlowService
```

## 4. RB Retrieve 管线

### 4.1 RBRecallMemoryOp

```go
// RBRecallMemoryOp ReasoningBank 记忆检索操作。
// 从向量库中检索与 query 相似的推理策略记忆。
// 对齐 Python RecallMemoryOp (retrieve/task/reasoning_bank/run.py)。
type RBRecallMemoryOp struct {
    op.OpBase
    // topK 检索返回数量，默认 1
    topK int
}

func NewRBRecallMemoryOp(sc *cecontext.ServiceContext, topK int) *RBRecallMemoryOp
```

Execute 流程：
1. 从 RuntimeContext 获取 query 和 user_id（默认 "default"）
2. 校验 EmbeddingModel 和 VectorStore 已注册
3. 对 query 生成嵌入向量
4. 向量搜索，metadata filter: `{type: "reasoning_bank_memory", workspace_id: userID}`
5. 将 VectorNode 转为 `[]ReasoningBankRetrievedMemory`（遍历 Memory 字段）
6. 写入 `rc.Set("retrieved_memories", retrieved)`

### 4.2 与 ReMe RecallMemoryOp 的关键差异

| 维度 | ReMe RecallMemoryOp | RB RBRecallMemoryOp |
|------|---------------------|---------------------|
| metadata type | `"reme_memory"` | `"reasoning_bank_memory"` |
| topK 默认值 | 10 | 1 |
| 后续 Op | RerankMemoryOp >> RewriteMemoryOp | 无（直接返回） |
| 记忆字段 | WhenToUse + Content | Title + Description + Content |
| 转换方式 | 从 metadata 取 when_to_use/content | `NewReasoningBankMemoryFromVectorNode` → 遍历 Memory 字段 |

## 5. MaTTS 4 个 Op

### 5.1 ParallelScalingOp

```go
// ParallelScalingOp 并行缩放操作。
// 生成 k 条多样化轨迹，供 BestOfNOp 选最优。
// 对齐 Python ParallelScalingOp (retrieve/task/reasoning_bank/matts.py)。
type ParallelScalingOp struct {
    op.OpBase
    k           int
    temperature float64
}

func NewParallelScalingOp(sc *cecontext.ServiceContext, k int, temperature float64) *ParallelScalingOp
```

Execute 流程：
1. 保存原始 LLM temperature，设为 self.temperature
2. 循环 k 次：创建新 RuntimeContext → 复制 query/user_id/retrieved_memories → `o.AgentFlow().Execute()`
3. 收集 `{index, answer, steps, success}` 到 trajectories
4. 写入 `rc.Set("parallel_trajectories", trajectories)` 和 `rc.Set("scaling_factor", k)`
5. finally 中恢复原始 temperature
6. AgentFlow 未注册时 Warn 日志跳过（对齐 Python `hasattr` 检查）

### 5.2 SequentialScalingOp

```go
// SequentialScalingOp 串行缩放操作。
// 对单条轨迹进行 k 轮自我修正。
// 对齐 Python SequentialScalingOp。
type SequentialScalingOp struct {
    op.OpBase
    k int
}

func NewSequentialScalingOp(sc *cecontext.ServiceContext, k int) *SequentialScalingOp
```

Execute 流程：
1. 从 RuntimeContext 获取 query 和当前 answer
2. 第1轮：用 SequentialFirstRefinePrompt
3. 第2~k轮：用 SequentialFollowUpRefinePrompt
4. 每轮调用 `o.LLM().Generate(ctx, prompt)`
5. 写入 refinement_history、refined_answer、answer、scaling_factor

### 5.3 BestOfNOp

```go
// BestOfNOp N 中选优操作。
// LLM 评估多条轨迹，选择最优。
// 对齐 Python BestOfNOp。
type BestOfNOp struct {
    op.OpBase
}

func NewBestOfNOp(sc *cecontext.ServiceContext) *BestOfNOp
```

Execute 流程：
1. 从 RuntimeContext 获取 parallel_trajectories
2. 构建评估 prompt（BestOfNEvalPrompt），列出每条轨迹的 answer + success + steps 数
3. 调用 `o.LLM().Generate(ctx, evalPrompt, temperature=0.0)`
4. 解析 LLM 返回的索引（正则匹配数字）
5. 设置 answer、best_trajectory_index、best_trajectory、pass_at_k
6. 解析失败时回退到第 0 条轨迹

### 5.4 SelfContrastMemoryOp

```go
// SelfContrastMemoryOp 自对比记忆提取操作。
// 从成功/失败轨迹对比中提取推理策略记忆。
// 对齐 Python SelfContrastMemoryOp。
type SelfContrastMemoryOp struct {
    op.OpBase
}

func NewSelfContrastMemoryOp(sc *cecontext.ServiceContext) *SelfContrastMemoryOp
```

Execute 流程：
1. 从 RuntimeContext 获取 parallel_trajectories
2. 分为 successful / failed 两组
3. 构建对比 prompt（SelfContrastPrompt）
4. 调用 `o.LLM().Generate(ctx, prompt, temperature=1.0)`
5. 解析 LLM 返回的 Markdown（## Title / ## Description / ## Content）
6. 构建 `[]ReasoningBankMemory`，source_type="comparative"
7. 设置 `rc.Set("contrastive_memories", memories)`

## 6. RB Summary 管线

### 6.1 LabelDeterminator

```go
// LabelDeterminator 轨迹标签判定器。
// 使用 LLM-as-judge 判断轨迹执行成功或失败。
// 对齐 Python LabelDeterminator (summary/task/reasoning_bank/update.py)。
type LabelDeterminator struct {
    sc *cecontext.ServiceContext
}

func NewLabelDeterminator(sc *cecontext.ServiceContext) *LabelDeterminator

func (d *LabelDeterminator) DetermineLabel(ctx context.Context, query string, trajectory string) (bool, error)
```

DetermineLabel 流程：
1. 用 LLMJudgeSystemPrompt + LLMJudgeUserPrompt 调用 LLM
2. 解析响应：正则 `Status:\s*(success|failure)`
3. 回退：正则不匹配时检查响应中是否包含 "success"

### 6.2 MemoryItemParser

```go
// MemoryItemParser 记忆项解析器。
// 将 LLM 返回的 Markdown 格式解析为结构化 ReasoningBankMemoryItem。
// 对齐 Python MemoryItemParser (summary/task/reasoning_bank/update.py)。
type MemoryItemParser struct{}

func NewMemoryItemParser() *MemoryItemParser

func (p *MemoryItemParser) Parse(llmResponse string, query string, label *bool) []*schema.ReasoningBankMemory
func (p *MemoryItemParser) cleanResponse(response string) string
func (p *MemoryItemParser) splitIntoSections(response string) []string
func (p *MemoryItemParser) extractMemoryItem(section string) *schema.ReasoningBankMemoryItem
func (p *MemoryItemParser) extractField(lines []string, startIdx int, fieldPattern string) (string, int)
```

### 6.3 SummarizeMemoryOp

```go
// SummarizeMemoryOp 单轨迹推理策略提取操作（matts="none"/"sequential"）。
// 对齐 Python SummarizeMemoryOp。
type SummarizeMemoryOp struct {
    op.OpBase
    parser *MemoryItemParser
}

func NewSummarizeMemoryOp(sc *cecontext.ServiceContext) *SummarizeMemoryOp
```

Execute 流程：
1. 获取 matts（必须 "none" 或 "sequential"）、query、trajectories
2. 获取/判定 label：rc 已有则用，否则调 LabelDeterminator
3. 根据 label 选择 system prompt：true→ExtractSuccessTrajSystemPrompt，false→ExtractFailTrajSystemPrompt
4. 构建 user prompt（ExtractTrajUserPrompt），调用 LLM
5. 用 MemoryItemParser.Parse() 解析响应
6. 写入 `rc.Set("memories", memories)`

### 6.4 SummarizeMemoryParallelOp

```go
// SummarizeMemoryParallelOp 多轨迹推理策略提取操作（matts="parallel"/"combined"）。
// 对齐 Python SummarizeMemoryParallelOp。
type SummarizeMemoryParallelOp struct {
    op.OpBase
    parser *MemoryItemParser
}

func NewSummarizeMemoryParallelOp(sc *cecontext.ServiceContext) *SummarizeMemoryParallelOp
```

Execute 流程：
1. 获取 matts（必须 "parallel" 或 "combined"）、query、trajectories（必须 >=2）
2. 使用 ParallelScalingSystemPrompt
3. 构建轨迹字符串（`<Trajectory N>` 标签），调用 LLM
4. 用 MemoryItemParser.Parse() 解析（label=nil）
5. 写入 `rc.Set("memories", memories)`

### 6.5 UpdateVectorStoreOp

```go
// UpdateVectorStoreOp 记忆向量存储更新操作。
// 对齐 Python UpdateVectorStoreOp。
type UpdateVectorStoreOp struct {
    op.OpBase
}

func NewUpdateVectorStoreOp(sc *cecontext.ServiceContext) *UpdateVectorStoreOp
```

Execute 流程：
1. 获取 memories 和 user_id，校验服务
2. 对每个 memory 设置 workspace_id
3. 批量嵌入 Query 字段，转为 VectorNode
4. 逐个 Upsert
5. 写入 stored_count 和 memory_ids

### 6.6 PersistMemoryOp

```go
// PersistMemoryOp 记忆持久化操作。
// algoName 固定为 "rb"。
// 对齐 Python PersistMemoryOp。
type PersistMemoryOp struct {
    op.OpBase
    helper *persistence.MemoryPersistenceHelper
}

func NewPersistMemoryOp(sc *cecontext.ServiceContext, helper *persistence.MemoryPersistenceHelper) *PersistMemoryOp
```

Execute 流程：
1. 获取 user_id，校验 VectorStore
2. GetAll(filter: {type: "reasoning_bank_memory", workspace_id: userID})
3. helper.Save(userID, "rb", nodesDict)
4. 写入 persist_count

## 7. trajectory_generator 函数

### 7.1 数据结构

```go
// TrialOutput 单次试验执行结果。对齐 Python TrialOutput。
type TrialOutput struct {
    Trajectory string
    Feedback   string
    Score      int
}

// RunTrialsInput MaTTS 试验运行参数。对齐 Python RunTrialsInput。
type RunTrialsInput struct {
    Agent       AgentFlowService
    UserID      string
    Question    string
    GroundTruth string
    MattsK      int
    MattsMode   string
}

// SummarizeTrajectoriesInput 轨迹摘要参数。对齐 Python SummarizeTrajectoriesInput。
type SummarizeTrajectoriesInput struct {
    Query       string
    Trajectory  []string
    MattsMode   string
    GroundTruth *string
    Feedback    []string
    Score       []int
}
```

### 7.2 核心函数

```go
// formatTrajectory 将消息列表格式化为轨迹文本。对齐 Python format_trajectory()。
func formatTrajectory(messages []Message) string

// EvaluateTrial 评估单次试验结果。对齐 Python evaluate_trial()。
func EvaluateTrial(question string, output string, groundTruth string) (string, int)

// RunTrials 运行 MaTTS 试验并返回轨迹结果。对齐 Python run_trials()。
func RunTrials(ctx context.Context, agent AgentFlowService, params RunTrialsInput) ([]TrialOutput, error)

// runTrialsInner 内部试验循环。对齐 Python _run_trials_inner()。
func runTrialsInner(ctx context.Context, agent AgentFlowService, question string, groundTruth string, mattsK int, selfRefine bool) ([]TrialOutput, error)

// SummarizeTrajectories 将轨迹总结为记忆。对齐 Python summarize_trajectories()。
// 注：依赖 TaskMemoryService（P6），P4 阶段先定义签名，实现留 P6。
func SummarizeTrajectories(ctx context.Context, memoryService TaskMemoryService, userID string, params SummarizeTrajectoriesInput) (map[string]any, error)
```

### 7.3 与 MaTTS Op 的关系（独立实现）

trajectory_generator 和 MaTTS Op 各自独立实现，不互相委托：
- trajectory_generator：直接调 AgentFlowService，不走 Op
- MaTTS Op：通过 `o.AgentFlow()` 调 Agent，结果写在 RuntimeContext 上

## 8. Prompt 模板设计

### 8.1 统一用 text/template

所有 prompt 字符串定义为 `const` 原文（一比一复刻 Python），通过 `template.New().Parse()` 解析，执行时用 `Execute()` 填充变量。

### 8.2 retrieve/task/rb/prompt.go — 6 个模板

| 模板名 | 对应 Python | 占位符变量 |
|--------|------------|-----------|
| `llmJudgeSystemPrompt` | `LLM_JUDGE_SYSTEM_PROMPT` | 无 |
| `llmJudgeUserPrompt` | `LLM_JUDGE_USER_PROMPT` | `{{.Query}}`, `{{.Trajectory}}` |
| `bestOfNEvalPrompt` | BestOfNOp 内联 prompt | `{{.NumTrajectories}}`, `{{.Query}}`, `{{.TrajDescriptions}}`, `{{.MaxIndex}}` |
| `selfContrastPrompt` | SelfContrastMemoryOp 内联 prompt | `{{.Query}}`, `{{.NumSuccessful}}`, `{{.SuccessfulTrajectories}}`, `{{.NumFailed}}`, `{{.FailedTrajectories}}` |
| `sequentialFirstRefinePrompt` | SequentialScalingOp 第1轮内联 prompt | `{{.CurrentAnswer}}`, `{{.Query}}` |
| `sequentialFollowUpRefinePrompt` | SequentialScalingOp 第2+轮内联 prompt | `{{.CurrentAnswer}}`, `{{.Query}}` |

### 8.3 summary/task/rb/prompt.go — 7 个模板

| 模板名 | 对应 Python | 占位符变量 |
|--------|------------|-----------|
| `extractSuccessTrajSystemPrompt` | `EXTRACT_SUCCESS_TRAJ_SYSTEM_PROMPT` | 无 |
| `extractFailTrajSystemPrompt` | `EXTRACT_FAIL_TRAJ_SYSTEM_PROMPT` | 无 |
| `extractTrajUserPrompt` | `EXTRACT_TRAJ_USER_PROMPT` | `{{.Query}}`, `{{.Trajectory}}` |
| `llmJudgeSystemPrompt` | `LLM_JUDGE_SYSTEM_PROMPT` | 无 |
| `llmJudgeUserPrompt` | `LLM_JUDGE_USER_PROMPT` | `{{.Query}}`, `{{.Trajectory}}` |
| `parallelScalingSystemPrompt` | `PARALLEL_SCALING_SYSTEM_PROMPT` | 无 |
| `parallelScalingUserPrompt` | `PARALLEL_SCALING_USER_PROMPT` | `{{.Query}}`, `{{.Trajectories}}` |

> `llmJudgeSystemPrompt` 和 `llmJudgeUserPrompt` 在 retrieve 和 summary 中重复。以 summary 为主定义。

### 8.4 P3 回填改造

现有 `strings.ReplaceAll` 调用全部改为 `text/template`：

```go
// 改造前：
prompt := strings.ReplaceAll(op.prompts.RerankPrompt, "{query}", query)
prompt = strings.ReplaceAll(prompt, "{candidates}", candidatesStr)

// 改造后：
var buf bytes.Buffer
op.prompts.RerankPrompt.Execute(&buf, map[string]any{
    "Query":      query,
    "Candidates": candidatesStr,
})
prompt := buf.String()
```

## 9. 错误处理

| 场景 | 处理方式 | 日志级别 |
|------|---------|---------|
| ServiceContext 服务未注册 | 返回 `fmt.Errorf("xxx service not configured")` | Error |
| LLM 调用失败 | 返回 error，Error 日志含 event_type + 方法 + 模型 | Error |
| VectorNode 转换失败 | 单条跳过，Warn 日志继续处理剩余 | Warn |
| Prompt 模板解析失败 | `NewXxxPrompt()` 中 panic（初始化阶段错误） | - |
| Prompt 模板执行失败 | 返回 error | Error |
| MemoryItemParser 解析失败 | 单条返回 nil，整体跳过 | Warn |
| BestOfNOp 索引解析失败 | 回退到第 0 条轨迹 | Warn |
| ParallelScalingOp 中某条轨迹失败 | 单条跳过，继续生成剩余轨迹 | Warn |
| AgentFlow 未注册 | ParallelScalingOp 中 Warn 日志跳过（对齐 Python hasattr 检查） | Warn |

## 10. 测试策略

| 测试对象 | 测试方式 | Mock 需求 |
|----------|---------|----------|
| RBRecallMemoryOp | mock EmbeddingService + VectorStoreService | 已有模式 |
| LabelDeterminator | mock LLMService | 已有模式 |
| MemoryItemParser | 纯字符串解析，无需 mock | 无 |
| SummarizeMemoryOp | mock LLMService | 已有模式 |
| SummarizeMemoryParallelOp | mock LLMService | 已有模式 |
| UpdateVectorStoreOp | mock EmbeddingService + VectorStoreService | 已有模式 |
| PersistMemoryOp | mock VectorStoreService + MemoryPersistenceHelper | 已有模式 |
| ParallelScalingOp | mock AgentFlowService + LLMService | AgentFlowService 需新增 mock |
| SequentialScalingOp | mock LLMService | 已有模式 |
| BestOfNOp | mock LLMService | 已有模式 |
| SelfContrastMemoryOp | mock LLMService | 已有模式 |
| formatTrajectory | 纯函数，构造 Message 列表测试 | 无 |
| EvaluateTrial | 纯函数，各种 groundTruth 组合 | 无 |
| RunTrials | mock AgentFlowService | AgentFlowService 需新增 mock |
| Prompt 模板 | 验证 Parse 不报错 + Execute 输出正确 | 无 |

覆盖率目标：≥ 85%。

## 11. 实现顺序

1. **核心层变更**：ServiceContext + OpBase 新增 AgentFlowService
2. **P3 回填**：目录迁移 + prompt 模板改造
3. **RB retrieve**：RBRecallMemoryOp + prompt
4. **RB summary**：LabelDeterminator + MemoryItemParser + SummarizeMemoryOp + SummarizeMemoryParallelOp + UpdateVectorStoreOp + PersistMemoryOp + prompt
5. **MaTTS Op**：ParallelScalingOp + SequentialScalingOp + BestOfNOp + SelfContrastMemoryOp
6. **trajectory_generator**：formatTrajectory + EvaluateTrial + RunTrials + 数据结构
7. **测试补全**：确保所有包覆盖率 ≥ 85%
