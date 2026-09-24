# 9.82 P6 审查修复设计文档

## 概述

本文档记录 9.82 P6（Service Layer + Agent Integration）实现审查发现的 26 个偏差 + 15 个不当 `any` 的修复方案。所有方案经与用户逐项讨论确认。

## 一、自建 Config 模块（核心变更）

### 1.1 背景

Python 的 `context_evolver/core/config.py` 是模块级懒加载全局单例，任何包可随时 `config.get(key, default)` 读取配置。Go 的 `common/config.Config` 是实例化的，且加载的是全局配置文件，不是 context_evolver 自己的 `.env`/`config.yaml`。

### 1.2 方案

在 `context_evolver/core/config/` 包自建配置模块，对齐 Python：

```
context_evolver/core/config/
├── doc.go           # 包文档
├── config.go        # 懒加载全局单例 + Get/Load/Set
└── config_test.go   # 单元测试
```

### 1.3 核心设计

```go
// 模块级全局变量（对齐 Python _config + _config_loaded）
var (
    _config       map[string]any
    _configLoaded bool
    _configMu     sync.RWMutex
)

// Get 获取配置值（对齐 Python config.get(key, default)）。
// 查找顺序：1. 已加载 _config  2. os.Getenv(key)  3. 返回 default
// 首次调用自动触发 Load()。
func Get(key string, defaultVal any) any

// Load 加载配置文件（对齐 Python config.load(config_path)）。
// 数据源优先级：.env（最高）→ config.yaml（不覆盖 .env）→ os.Getenv（兜底）
// configPath 为空时使用 context_evolver 根目录下的 .env 和 config.yaml。
func Load(configPath ...string) error

// Set 运行时修改配置值（对齐 Python config.set_value(key, value)）。
func Set(key string, value any)
```

### 1.4 对 TaskMemoryServiceConfig 的影响

删除 TaskMemoryServiceConfig 中的运行时参数（改为 config.Get() 实时读取）：

| 删除的字段 | 对应 Python config 键 | 读取位置 |
|-----------|----------------------|---------|
| TopKRetrieval | TOPK_RETRIEVAL | createRetrieveFlow |
| TopKRerank | TOPK_RERANK | createRetrieveFlow |
| LLMRerank | LLM_RERANK | createRetrieveFlow |
| LLMRewrite | LLM_REWRITE | createRetrieveFlow |
| TopKQuery | TOPK_QUERY | createRetrieveFlow |
| UseGroundTruth | USE_GROUNDTRUTH | createSummaryFlow |
| MaxPlaybookSize | MAX_PLAYBOOK_SIZE | createSummaryFlow |
| ExtractBestTraj | EXTRACT_BEST_TRAJ | createSummaryFlow |
| ExtractWorstTraj | EXTRACT_WORST_TRAJ | createSummaryFlow |
| ExtractComparativeTraj | EXTRACT_COMPARATIVE_TRAJ | createSummaryFlow |
| MemoryValidation | MEMORY_VALIDATION | createSummaryFlow |
| MemoryDeduplication | MEMORY_DEDUPLICATION | createSummaryFlow |

保留的字段（Python 中在 `__init__` 时一次性读取存入 `self.xxx`）：

| 保留字段 | 对应 Python 实例属性 |
|---------|---------------------|
| LLMModel | llm_model（构造参数） |
| EmbeddingModel | embedding_model（构造参数） |
| APIKey | api_key（构造参数） |
| APIBase | api_base（构造参数） |
| RetrievalAlgo | self.retrieval_algorithm |
| SummaryAlgo | self.summary_algorithm |
| PersistType | self._persist_type |
| PersistPath | self._persist_path |
| MilvusHost/Port/Collection | self._milvus_* |

### 1.5 对 trajectory_generator.go 的影响

| 函数 | 当前 | 修复后 |
|------|------|--------|
| getUseGoldLabel | 硬编码 `return false` | `config.Get("USE_GOLDLABEL", false).(bool)` |
| getUseGroundTruth | 硬编码 `return false` | `config.Get("USE_GROUNDTRUTH", false).(bool)` |
| runTrialsInner | 硬编码 selfRefinePrompt | `config.Get("COMBINED_MATTS_PROMPT", "refine")` 选择 refine/diversity prompt |
| SummarizeTrajectories | 从 memoryService.summaryAlgorithm 读取 | 从 `config.Get("SUMMARY_ALGO", "ACE")` 读取 |

## 二、MemoryItem 接口 + 类型断言 Bug 修复（D-16/D-17）

### 2.1 问题

- `Retrieve()` 中 `retrievedMemories.([]any)` 类型断言会失败（Go 中 `[]T ≠ []any`）
- `formatRBMemoryString`/`formatACEMemoryString` 为占位实现
- LoadMemories 数据结构与 Python 不匹配

### 2.2 方案：定义 MemoryItem 接口

```go
// schema 包中定义
type MemoryItem interface {
    FormatMemoryString() string
}
```

各类型实现：

```go
// ReasoningBankRetrievedMemory
func (m ReasoningBankRetrievedMemory) FormatMemoryString() string {
    return fmt.Sprintf("Title: %s\nDescription: %s\nContent: %s", m.Title, m.Description, m.Content)
}

// ACEMemory
func (m ACEMemory) FormatMemoryString() string {
    return fmt.Sprintf("[%s] helpful=%d harmful=%d neutral=%d\nSection: %s\nContent: %s",
        m.ID, m.Helpful, m.Harmful, m.Neutral, m.Section, m.Content)
}

// ReMeRetrievedMemory
func (m ReMeRetrievedMemory) FormatMemoryString() string {
    return fmt.Sprintf("When to use: %s\nContent: %s", m.WhenToUse, m.Content)
}
```

### 2.3 P2/P3/P4 存入类型修改

各 retrieve op 中 `rc.Set("retrieved_memories", ...)` 改为存 `[]MemoryItem`：

```go
// 修改前（P3 RB recall op）
rc.Set("retrieved_memories", retrieved)  // retrieved 是 []ReasoningBankRetrievedMemory

// 修改后
items := make([]ceschema.MemoryItem, len(retrieved))
for i, m := range retrieved {
    items[i] = m
}
rc.Set("retrieved_memories", items)
```

**影响范围**：`retrieve/task/rb/run.go`、`retrieve/task/ace/run.go`、`retrieve/task/reme/run.go`、以及下游消费 `retrieved_memories` 的 op（RerankOp、RewriteOp 等）需适配。

### 2.4 Retrieve() 统一提取

```go
// 修改后
memories, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
if ok && len(memories) > 0 {
    items := make([]string, 0, len(memories))
    for _, m := range memories {
        items = append(items, m.FormatMemoryString())
    }
    memoryString = strings.Join(items, "\n\n")
}
```

### 2.5 LoadMemories 数据结构修复

对齐 Python 的 `{node_id: node_data}` 迭代模式：

```go
// 修改前
if nodes, ok := nodesDict["nodes"]; ok {  // ❌ 不存在 "nodes" 键
    if nodeList, ok := nodes.([]any); ok { ... }
}

// 修改后（对齐 Python）
for nodeID, nodeData := range nodesDict {
    if data, ok := nodeData.(map[string]any); ok {
        vn, err := schema.VectorNodeFromDict(data)
        if err != nil { continue }
        if vs, ok := s.vectorStore.(interface{ LoadNode(string, *schema.VectorNode) }); ok {
            vs.LoadNode(nodeID, vn)
        } else {
            s.vectorStore.Upsert(ctx, vn)  // 回退用 Upsert
        }
    }
}
```

### 2.6 AddMemory 持久化格式修复（D-21）

```go
// 修改前
s.persistenceHelper.Save(userID, algoName, node.ToDict())  // ❌ 裸 dict

// 修改后（对齐 Python）
s.persistenceHelper.Save(userID, algoName, map[string]any{node.ID: node.ToDict()})
```

## 三、ContextEvolvingReActAgent 修复（D-06/D-07/D-08）

### 3.1 双路径构造（D-06）

```go
func NewContextEvolvingReActAgent(
    card *agentschema.AgentCard,
    config MemoryAgentConfigInput,
    userID string,
    memoryService *service.TaskMemoryService,  // nil 时自建
    persistType *string,
    persistPath string,
    milvusHost string,
    milvusPort int,
    milvusCollection string,
    injectMemoriesInContext bool,
    autoSummarize bool,
    autoSummarizeMattsMode string,
) (*ContextEvolvingReActAgent, error)  // 返回 error（D-07）
```

memoryService 为 nil 时用 persist 参数 + TaskMemoryServiceConfig 自建。

### 3.2 构造时加载记忆（D-07）

New 返回 `(*ContextEvolvingReActAgent, error)`，构造末尾调用 `memoryService.LoadMemories(ctx, userID)`。

### 3.3 AutoConfigure 方法（D-08）

```go
func (a *ContextEvolvingReActAgent) AutoConfigure(ctx context.Context) error {
    apiKey := ceconfig.Get("API_KEY", "").(string)
    apiBase := ceconfig.Get("API_BASE", "").(string)
    modelName := ceconfig.Get("MODEL_NAME", "gpt-5.2").(string)
    modelProvider := ceconfig.Get("MODEL_PROVIDER", "openai").(string)
    // 构建 ReActAgentConfig 并 Configure
}
```

## 四、TaskMemoryService 修复（D-01/D-04/D-05/D-10/D-18/D-20/D-24）

### 4.1 字段改回具体类型（D-01）

```go
type TaskMemoryService struct {
    llm      *OpenAILLMWrapper           // 改回具体类型
    embedding *OpenAIEmbeddingWrapper     // 改回具体类型
    // ...
}
```

取消 `NewTaskMemoryServiceWithServices`，测试通过 mock client 注入（构造 `OpenAILLMWrapper` 时注入 mock `BaseModelClient`）。

### 4.2 createFlow 从 config 实时读取（D-04/D-05）

```go
func (s *TaskMemoryService) createRetrieveFlow() op.BaseOp {
    switch s.retrievalAlgorithm {
    case "ReMe":
        topkRetrieval := ceconfig.Get("TOPK_RETRIEVAL", 10).(int)
        topkRerank := ceconfig.Get("TOPK_RERANK", 5).(int)
        llmRerank := ceconfig.Get("LLM_RERANK", true).(bool)
        llmRewrite := ceconfig.Get("LLM_REWRITE", true).(bool)
        // ...
    }
}
```

### 4.3 GetPlaybook / ClearPlaybook（D-10）

```go
func (s *TaskMemoryService) GetPlaybook(ctx context.Context, userID string) ([]*schema.VectorNode, error)
func (s *TaskMemoryService) ClearPlaybook(ctx context.Context, userID string) error
```

### 4.4 LoadNode 改为 Upsert（D-18）

LoadMemories 中不再强制断言 `*MemoryVectorStore`，改用 `Upsert` 或接口检测。

### 4.5 AddMemory 输入校验（D-20）

各算法校验必填字段：
- RB：title/description/content 非空
- ReMe：when_to_use 非空
- ACE：content/section 非空

### 4.6 统一初始化失败 error 日志（D-24）

NewTaskMemoryService 构造流程的 error 返回前补 `logger.Error(logComponent).Err(err).Msg("TaskMemoryService initialization failed")`。

## 五、TrajectoryGenerator 修复（D-11/D-14/D-19）

### 5.1 RunTrials 加持久化逻辑（D-11）

```go
type RunTrialsInput struct {
    Agent         cecontext.AgentFlowService
    UserID        string
    Question      string
    GroundTruth   string
    MattsK        int
    MattsMode     string
    MemoryService *TaskMemoryService  // 新增
    PersistType   *string             // 新增
    PersistPath   string              // 新增
    MilvusHost    string              // 新增
    MilvusPort    int                 // 新增
    MilvusCollection string           // 新增
}
```

RunTrials 内部：
1. 运行前：如果 persistType 非 nil，创建 persistenceHelper 加载记忆
2. 运行后：保存记忆
3. 末尾调用 SummarizeTrajectories（对齐 Python）

### 5.2 Diversity prompt（D-14）

```go
const (
    selfRefinePrompt = "Let's carefully re-examine..."    // 已有
    selfDiversityPrompt = "Let's carefully re-examine... using DIFFERENT reasoning approach..."  // 新增
)

// runTrialsInner 中
combinedPrompt := ceconfig.Get("COMBINED_MATTS_PROMPT", "refine").(string)
if combinedPrompt == "refine" {
    currentQuery = prevTraj + selfRefinePrompt + question
} else {
    currentQuery = prevTraj + selfDiversityPrompt + question
}
```

### 5.3 Sequential 截断同步（D-19）

```go
if params.MattsMode == "sequential" && len(trajectories) > 1 {
    trajectories = trajectories[len(trajectories)-1:]
    feedbacks = feedbacks[len(feedbacks)-1:]  // 新增
    scores = scores[len(scores)-1:]           // 新增
}
```

## 六、不当 any 修复

### 6.1 P0：返回值定义具体 struct

```go
// RetrieveResult 替代 map[string]any
type RetrieveResult struct {
    MemoryString    string
    RetrievedMemory []ceschema.MemoryItem
    Query           string
    UserID          string
    Algorithm       string
}

// AddMemoryResult 替代 map[string]any
type AddMemoryResult struct {
    Status   string
    MemoryID string
    UserID   string
    Algorithm string
}

// MaTTSResult 替代 map[string]any
type MaTTSResult struct {
    Trials    []TrialOutput
    Query     string
    UserID    string
    MattsMode string
}
```

### 6.2 P1：Summarize 返回值 + Metadata struct

```go
// SummarizeResult 替代 map[string]any
type SummarizeResult struct {
    Memories  []*schema.VectorNode
    UserID    string
    Query     string
    Algorithm string
}

// RB metadata（value 全为 string，可用 map[string]string）
// ReMe/ACE metadata 定义具体 struct
```

### 6.3 P2：暂不动

`BaseAgent.Invoke` 签名中的 `map[string]any` 受接口体系约束，不在本次修复范围。

## 七、低严重度修复

| 编号 | 修复内容 |
|------|---------|
| D-02 | 不修，`op.BaseOp` 是正确类型名，更新设计文档 |
| D-03 | 删除 TaskMemoryService 中的接口断言和 mock |
| D-22 | Invoke 无 query 时补 `logger.Warn("No query provided in inputs")` |
| D-23 | 随 D-06 修复自动解决 |
| D-24 | NewTaskMemoryService 构造 error 前补统一 error 日志 |
| D-25 | RB memoryID 改为 `reasoning_bank_{workspace_id}_{md5(title\|content)}` |
| D-26 | ACE memoryID 分隔符 `_` 改为 `-`（对齐 Python `ace_{workspace_id}_{id}`） |
| D-12 | SUMMARY_ALGO 从自建 config 读取 |

## 八、影响范围汇总

| 修改范围 | 涉及文件 |
|---------|---------|
| 新建 | `core/config/config.go`、`core/config/config_test.go`、`core/config/doc.go` |
| service 层 | `task_memory_service.go`、`trajectory_generator.go`、`llm_wrapper.go`、`embedding_wrapper.go` |
| agent 层 | `context_evolving_react_agent.go` |
| schema 层 | `core/schema/` 添加 MemoryItem 接口及各类型 FormatMemoryString 方法 |
| P2/P3/P4 retrieve op | `retrieve/task/rb/run.go`、`retrieve/task/ace/run.go`、`retrieve/task/reme/run.go` |
| P2/P3/P4 下游 op | RerankOp、RewriteOp 等消费 `retrieved_memories` 的 op 需适配 `[]MemoryItem` |
| 测试 | 所有相关测试文件需适配 |
