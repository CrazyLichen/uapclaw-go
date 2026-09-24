# 9.82 P6 审查修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 9.82 P6 实现审查发现的 26 个偏差 + 15 个不当 any，包括自建 config 模块、MemoryItem 接口、类型安全改进等。

**Architecture:** 核心变更为自建 `context_evolver/core/config` 包（对齐 Python 懒加载全局单例），定义 MemoryItem 接口统一 retrieved_memories 类型，将 map[string]any 返回值改为具体 struct，TaskMemoryService 字段改回具体类型。

**Tech Stack:** Go 1.x，现有 context_evolver P1-P5 基础设施

---

## Task 1: 自建 Config 模块

**Files:**
- Create: `internal/agentcore/context_evolver/core/config/doc.go`
- Create: `internal/agentcore/context_evolver/core/config/config.go`
- Create: `internal/agentcore/context_evolver/core/config/config_test.go`

- [x] **Step 1: 写 doc.go**

```go
// Package config 提供 context_evolver 的配置管理。
//
// 对齐 Python openjiuwen/extensions/context_evolver/core/config.py，
// 实现懒加载全局单例，支持 .env + config.yaml + 环境变量三级数据源。
// 任何包可随时调用 config.Get(key, default) 读取配置。
//
// 文件目录：
//
//	config/
//	├── doc.go           # 包文档
//	├── config.go        # 懒加载全局单例 + Get/Load/Set
//	└── config_test.go   # 单元测试
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/config.py
package config
```

- [x] **Step 2: 写 config.go 核心逻辑**

实现要点：
- 模块级全局变量 `_config map[string]any` + `_configLoaded bool` + `_configMu sync.RWMutex`
- `Get(key string, defaultVal any) any`：首次调用自动触发 Load()，查找顺序 _config → os.Getenv → defaultVal
- `Load(configPath ...string) error`：读取 .env + config.yaml，.env 优先不覆盖
- `Set(key string, value any)`：运行时修改
- `GetString(key string, defaultVal string) string`：类型安全便捷方法
- `GetInt(key string, defaultVal int) int`：类型安全便捷方法
- `GetBool(key string, defaultVal bool) bool`：类型安全便捷方法（对齐 Python 中 `"true"`/`"false"` 字符串转 bool 的逻辑）
- `.env` 解析：逐行 `KEY=VALUE`，支持 `#` 注释，带 bool/int/float 类型推断（对齐 Python）
- `config.yaml` 解析：`gopkg.in/yaml.v3`，合并到 _config 但不覆盖 .env 已有 key
- `rootDir()` 计算：`core/config/` → `core/` → `context_evolver/`（对齐 Python `os.path.dirname(os.path.dirname(os.path.abspath(__file__)))`）

- [x] **Step 3: 写 config_test.go**

测试用例：
- TestGet_未加载时自动触发Load
- TestGet_优先级（_config > os.Getenv > default）
- TestGetString/GetInt/GetBool 类型安全
- TestGetBool_字符串true_false
- TestLoad_env文件
- TestLoad_configYaml不覆盖env
- TestSet_运行时修改
- TestLoad_文件不存在返回空配置
- TestLoad_configPath参数

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/config/... -v`

- [x] **Step 5: 更新 core/config/doc.go（父目录）和 core/doc.go**

在 `context_evolver/core/doc.go` 中添加 config/ 子包到文件目录。

- [x] **Step 6: 提交**

```bash
git add internal/agentcore/context_evolver/core/config/
git commit -m "feat(context-evolver): 自建 config 模块，懒加载全局单例对齐 Python"
```

---

## Task 2: MemoryItem 接口 + FormatMemoryString 方法

**Files:**
- Modify: `internal/agentcore/context_evolver/schema/io_schema.go`（添加 MemoryItem 接口 + RB/ReMe 的 FormatMemoryString 方法）
- Modify: `internal/agentcore/context_evolver/schema/memory.go`（ACE 的 FormatMemoryString 方法）
- Modify: `internal/agentcore/context_evolver/schema/io_schema_test.go`
- Modify: `internal/agentcore/context_evolver/schema/memory_test.go`

- [x] **Step 1: 在 io_schema.go 结构体区块添加 MemoryItem 接口**

```go
// MemoryItem 记忆项通用接口，统一不同算法的检索结果类型。
// 各算法的 RetrievedMemory 类型实现此接口，使 Retrieve() 可统一处理。
type MemoryItem interface {
	// FormatMemoryString 将记忆格式化为可注入上下文的文本。
	FormatMemoryString() string
}
```

- [x] **Step 2: 为 ReasoningBankRetrievedMemory 添加 FormatMemoryString 方法**

```go
// FormatMemoryString 对齐 Python ReasoningBank 的记忆格式化。
// 输出格式：Title: {title}\nDescription: {desc}\nContent: {content}
func (m ReasoningBankRetrievedMemory) FormatMemoryString() string {
	return fmt.Sprintf("Title: %s\nDescription: %s\nContent: %s", m.Title, m.Description, m.Content)
}
```

- [x] **Step 3: 为 ReMeRetrievedMemory 添加 FormatMemoryString 方法**

```go
// FormatMemoryString 对齐 Python ReMe 的记忆格式化。
// 输出格式：When to use: {when_to_use}\nContent: {content}
func (m ReMeRetrievedMemory) FormatMemoryString() string {
	return fmt.Sprintf("When to use: %s\nContent: %s", m.WhenToUse, m.Content)
}
```

- [x] **Step 4: 为 ACEMemory 添加 FormatMemoryString 方法**

在 memory.go 中添加：

```go
// FormatMemoryString 对齐 Python ACE 的记忆格式化。
// 输出格式：[{id}] helpful={helpful} harmful={harmful} neutral={neutral}\nSection: {section}\nContent: {content}
func (m ACEMemory) FormatMemoryString() string {
	return fmt.Sprintf("[%s] helpful=%d harmful=%d neutral=%d\nSection: %s\nContent: %s",
		m.ID, m.Helpful, m.Harmful, m.Neutral, m.Section, m.Content)
}
```

- [x] **Step 5: 编写测试**

TestReasoningBankRetrievedMemory_FormatMemoryString、TestReMeRetrievedMemory_FormatMemoryString、TestACEMemory_FormatMemoryString

- [x] **Step 6: 运行测试**

Run: `go test ./internal/agentcore/context_evolver/schema/... -v`

- [x] **Step 7: 提交**

```bash
git add internal/agentcore/context_evolver/schema/
git commit -m "feat(context-evolver): 定义 MemoryItem 接口，各类型实现 FormatMemoryString"
```

---

## Task 3: P2/P3/P4 retrieve op 存入类型改为 []MemoryItem

**Files:**
- Modify: `internal/agentcore/context_evolver/retrieve/task/rb/run.go`
- Modify: `internal/agentcore/context_evolver/retrieve/task/ace/run.go`
- Modify: `internal/agentcore/context_evolver/retrieve/task/reme/run.go`
- Modify: `internal/agentcore/context_evolver/retrieve/task/rb/matts.go`
- Modify: `internal/agentcore/context_evolver/retrieve/task/reme/run.go`（RerankOp/RewriteOp 适配）
- Modify: 对应测试文件

- [x] **Step 1: RB recall op 存入改为 []MemoryItem**

`rb/run.go` 中 `rc.Set("retrieved_memories", retrieved)` 改为：

```go
items := make([]ceschema.MemoryItem, len(retrieved))
for i, m := range retrieved {
    items[i] = m
}
rc.Set("retrieved_memories", items)
```

- [x] **Step 2: ACE recall op 存入改为 []MemoryItem**

`ace/run.go` 同理。

- [x] **Step 3: ReMe recall op 存入改为 []MemoryItem**

`reme/run.go` 的 RecallMemoryOp.Execute 同理。

- [x] **Step 4: ReMe RerankOp/RewriteOp 适配 []MemoryItem**

当前 `GetTyped[[]ceschema.ReMeRetrievedMemory]` 改为 `GetTyped[[]ceschema.MemoryItem]`，内部类型 switch 断言回具体类型：

```go
memories, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
// 内部遍历时类型 switch
for _, item := range memories {
    if m, ok := item.(ceschema.ReMeRetrievedMemory); ok {
        // 访问 m.WhenToUse, m.Content
    }
}
```

- [x] **Step 5: RB matts.go 适配**

`rc.Get("retrieved_memories")` 改为 `GetTyped[[]ceschema.MemoryItem]`，透传到 trajRC。

- [x] **Step 6: 更新所有相关测试文件**

- [x] **Step 7: 运行测试**

Run: `go test ./internal/agentcore/context_evolver/retrieve/... -v`

- [x] **Step 8: 提交**

```bash
git add internal/agentcore/context_evolver/retrieve/
git commit -m "refactor(context-evolver): retrieve op 存入类型改为 []MemoryItem，适配下游 op"
```

---

## Task 4: 定义具体 struct 替代 map[string]any 返回值（P0 any 修复）

**Files:**
- Modify: `internal/agentcore/context_evolver/service/task_memory_service.go`
- Modify: `internal/agentcore/context_evolver/service/task_memory_service_test.go`
- Modify: `internal/agentcore/context_evolver/service/trajectory_generator.go`
- Modify: `internal/agentcore/context_evolver/context_evolving_react_agent.go`
- Modify: `internal/agentcore/context_evolver/context_evolving_react_agent_test.go`

- [x] **Step 1: 在 task_memory_service.go 结构体区块添加返回值 struct**

```go
// RetrieveResult 检索结果。对齐 Python RetrieveResponse(BaseModel)。
type RetrieveResult struct {
	// MemoryString 格式化后的记忆文本
	MemoryString string
	// RetrievedMemory 检索到的记忆项列表
	RetrievedMemory []ceschema.MemoryItem
	// Query 查询
	Query string
	// UserID 用户标识
	UserID string
	// Algorithm 算法名称
	Algorithm string
}

// SummarizeResult 摘要结果。对齐 Python SummarizeResponse(BaseModel)。
type SummarizeResult struct {
	// Memories 更新后的记忆节点列表
	Memories []*schema.VectorNode
	// UserID 用户标识
	UserID string
	// Query 查询
	Query string
	// Algorithm 算法名称
	Algorithm string
}

// AddMemoryResult 添加记忆结果。
type AddMemoryResult struct {
	// Status 操作状态
	Status string
	// MemoryID 记忆标识
	MemoryID string
	// UserID 用户标识
	UserID string
	// Algorithm 算法名称
	Algorithm string
}

// MaTTSResult MaTTS 试验结果。
type MaTTSResult struct {
	// Trials 试验结果列表
	Trials []TrialOutput
	// Query 查询
	Query string
	// UserID 用户标识
	UserID string
	// MattsMode MaTTS 模式
	MattsMode string
}
```

- [x] **Step 2: 修改 Retrieve() 返回值类型**

`Retrieve() (map[string]any, error)` → `Retrieve() (*RetrieveResult, error)`

内部逻辑配合 MemoryItem 接口（Task 2/3 完成后）：
- `GetTyped[[]ceschema.MemoryItem]` 提取
- 遍历调 `FormatMemoryString()` 构建 MemoryString
- 返回 `&RetrieveResult{...}`

- [x] **Step 3: 修改 Summarize() 返回值类型**

`Summarize() (map[string]any, error)` → `Summarize() (*SummarizeResult, error)`

- [x] **Step 4: 修改 AddMemory() 返回值类型**

`AddMemory() (map[string]any, error)` → `AddMemory() (*AddMemoryResult, error)`

- [x] **Step 5: 修改 SummarizeTrajectories() 返回值类型**

`SummarizeTrajectories() (map[string]any, error)` → 透传 SummarizeResult

- [x] **Step 6: 修改 ContextEvolvingReActAgent 相关**

- `lastRetrievalResult map[string]any` → `lastRetrievalResult *RetrieveResult`
- MaTTS 返回值改为 `*MaTTSResult`
- invokeWithMemory 中从 `lastRetrievalResult` 取值改为直接访问 struct 字段

- [x] **Step 7: 更新所有相关测试**

- [x] **Step 8: 运行测试**

Run: `go test ./internal/agentcore/context_evolver/... -v`

- [x] **Step 9: 提交**

```bash
git add internal/agentcore/context_evolver/service/ internal/agentcore/context_evolver/context_evolving_react_agent.go internal/agentcore/context_evolver/context_evolving_react_agent_test.go
git commit -m "refactor(context-evolver): 返回值 map[string]any 改为具体 struct，消除 P0 any"
```

---

## Task 5: TaskMemoryService 核心修复（D-01/D-04/D-05/D-10/D-16/D-17/D-18/D-20/D-21/D-24/D-25/D-26）

**Files:**
- Modify: `internal/agentcore/context_evolver/service/task_memory_service.go`
- Modify: `internal/agentcore/context_evolver/service/task_memory_service_test.go`

- [x] **Step 1: 字段改回具体类型（D-01），取消 WithServices 构造函数**

`llm cecontext.LLMService` → `llm *OpenAILLMWrapper`
`embedding cecontext.EmbeddingService` → `embedding *OpenAIEmbeddingWrapper`
删除 `NewTaskMemoryServiceWithServices` 函数
删除 `mockLLMServiceForTMS`/`mockEmbeddingServiceForTMS` mock 类型
测试改用 `newWrapperWithMock` 构造含 mock client 的 wrapper

- [x] **Step 2: TaskMemoryServiceConfig 删除运行时参数（D-04/D-05 关联）**

删除 12 个运行时参数字段（TopKRetrieval/TopKRerank/LLMRerank/LLMRewrite/TopKQuery/UseGroundTruth/MaxPlaybookSize/ExtractBestTraj/ExtractWorstTraj/ExtractComparativeTraj/MemoryValidation/MemoryDeduplication）
删除 `applyConfigDefaults` 函数（不再需要，默认值由 config.Get 提供）

- [x] **Step 3: createRetrieveFlow 从 config 实时读取（D-04/D-05）**

替换硬编码为 `ceconfig.Get("TOPK_RETRIEVAL", 10).(int)` 等。
RB：`ceconfig.Get("TOPK_QUERY", 1).(int)`
ReMe：`TOPK_RETRIEVAL`/`TOPK_RERANK`/`LLM_RERANK`/`LLM_REWRITE`
RefCon/DivCon：保持硬编码（对齐 Python）

- [x] **Step 4: createSummaryFlow 从 config 实时读取（D-04/D-05）**

ACE：`ceconfig.Get("USE_GROUNDTRUTH", false).(bool)` / `ceconfig.Get("MAX_PLAYBOOK_SIZE", 50).(int)`
ReMe：`EXTRACT_BEST_TRAJ`/`EXTRACT_WORST_TRAJ`/`EXTRACT_COMPARATIVE_TRAJ`/`MEMORY_VALIDATION`/`MEMORY_DEDUPLICATION`
RefCon/DivCon：保持硬编码

- [x] **Step 5: 修复 Retrieve() 类型断言 bug + 格式化（D-16/D-17）**

用 `GetTyped[[]ceschema.MemoryItem]` 提取，遍历调 `FormatMemoryString()`
删除 `formatRBMemoryString`/`formatACEMemoryString` 函数（已被接口方法替代）

- [x] **Step 6: 修复 LoadMemories 数据结构（D-17/D-18）**

迭代 `nodesDict` 的 key-value 对，`VectorNodeFromDict` 后用 `Upsert` 替代 `LoadNode` 强制断言

- [x] **Step 7: 修复 AddMemory 持久化格式（D-21）**

`Save(userID, algoName, map[string]any{node.ID: node.ToDict()})`

- [x] **Step 8: AddMemory 输入校验（D-20）**

RB 校验 title/description/content 非空；ReMe 校验 when_to_use 非空；ACE 校验 content/section 非空

- [x] **Step 9: 统一初始化失败 error 日志（D-24）**

NewTaskMemoryService 构造流程 error 返回前补 `logger.Error(logComponent).Err(err).Msg("TaskMemoryService initialization failed")`

- [x] **Step 10: 对齐 memoryID 格式（D-25/D-26）**

RB：`reasoning_bank_{workspaceID}_{md5(title|content)}`
ACE：`ace_{workspaceID}_{id}`（分隔符 `_` 保留，Python 也是 `_`）
ReMe：`reme_{workspaceID}_{md5(when_to_use)[:12]}`（Python 取 12 位）

- [x] **Step 11: 添加 GetPlaybook / ClearPlaybook（D-10）**

```go
func (s *TaskMemoryService) GetPlaybook(ctx context.Context, userID string) ([]*schema.VectorNode, error)
func (s *TaskMemoryService) ClearPlaybook(ctx context.Context, userID string) error
```

- [x] **Step 12: 更新测试**

适配所有签名变更，补充新增方法测试

- [x] **Step 13: 运行测试**

Run: `go test ./internal/agentcore/context_evolver/service/... -v`

- [x] **Step 14: 提交**

```bash
git add internal/agentcore/context_evolver/service/
git commit -m "fix(context-evolver): TaskMemoryService 核心修复 — config 实时读取/类型断言/持久化格式/校验"
```

---

## Task 6: TrajectoryGenerator 修复（D-11/D-13/D-14/D-19）

**Files:**
- Modify: `internal/agentcore/context_evolver/service/trajectory_generator.go`
- Modify: `internal/agentcore/context_evolver/service/trajectory_generator_test.go`

- [x] **Step 1: RunTrialsInput 加 persist + memoryService 字段（D-11）**

```go
type RunTrialsInput struct {
    Agent              cecontext.AgentFlowService
    UserID             string
    Question           string
    GroundTruth        string
    MattsK             int
    MattsMode          string
    MemoryService      *TaskMemoryService  // 新增
    PersistType        *string             // 新增
    PersistPath        string              // 新增：默认 "./memories/{algo_name}/{user_id}.json"
    MilvusHost         string              // 新增：默认 "localhost"
    MilvusPort         int                 // 新增：默认 19530
    MilvusCollection   string              // 新增：默认 "vector_nodes"
}
```

- [x] **Step 2: RunTrials 内部加持久化逻辑（D-11）**

运行前：persistType 非 nil 时创建 persistenceHelper 加载记忆
运行后：保存记忆到 persistence
末尾调用 SummarizeTrajectories（对齐 Python run_trials）

- [x] **Step 3: getUseGoldLabel/getUseGroundTruth 改从 config 读取（D-13）**

```go
func getUseGoldLabel() bool {
    return ceconfig.GetBool("USE_GOLDLABEL", false)
}

func getUseGroundTruth() bool {
    return ceconfig.GetBool("USE_GROUNDTRUTH", false)
}
```

- [x] **Step 4: 添加 diversity prompt + COMBINED_MATTS_PROMPT（D-14）**

```go
const selfDiversityPrompt = "Let's carefully re-examine the previous trajectory, including your reasoning " +
    "steps and action taken. The solution might be correct or wrong. Now, solve the " +
    "same problem again from scratch using DIFFERENT reasoning approach. " +
    "Focus on exploring alternative strategies.\n\n"
```

runTrialsInner 中 self-refine 分支：
```go
combinedPrompt := ceconfig.GetString("COMBINED_MATTS_PROMPT", "refine")
if combinedPrompt == "refine" {
    currentQuery = fmt.Sprintf("Previous attempt:\n%s\n\n%sQuestion: %s", prevTraj, selfRefinePrompt, question)
} else {
    currentQuery = fmt.Sprintf("Previous attempt:\n%s\n\n%sQuestion: %s", prevTraj, selfDiversityPrompt, question)
}
```

- [x] **Step 5: Sequential 截断同步（D-19）**

```go
if params.MattsMode == "sequential" && len(trajectories) > 1 {
    trajectories = trajectories[len(trajectories)-1:]
    feedbacks = feedbacks[len(feedbacks)-1:]
    scores = scores[len(scores)-1:]
}
```

注意：当前 SummarizeTrajectories 参数中没有 feedbacks/scores 局部变量，需从 params 中截断后再传入。

- [x] **Step 6: 更新测试**

- [x] **Step 7: 运行测试**

Run: `go test ./internal/agentcore/context_evolver/service/... -v`

- [x] **Step 8: 提交**

```bash
git add internal/agentcore/context_evolver/service/
git commit -m "fix(context-evolver): TrajectoryGenerator 修复 — 持久化/config 读取/diversity prompt/截断同步"
```

---

## Task 7: ContextEvolvingReActAgent 修复（D-06/D-07/D-08/D-22）

**Files:**
- Modify: `internal/agentcore/context_evolver/context_evolving_react_agent.go`
- Modify: `internal/agentcore/context_evolver/context_evolving_react_agent_test.go`

- [x] **Step 1: 双路径构造（D-06）**

修改 NewContextEvolvingReActAgent 签名：
- 添加 persistType *string, persistPath string, milvusHost string, milvusPort int, milvusCollection string 参数
- 返回值改为 `(*ContextEvolvingReActAgent, error)`
- memoryService 为 nil 时用 persist 参数 + TaskMemoryServiceConfig 自建

- [x] **Step 2: 构造时加载记忆（D-07）**

构造末尾调用 `memoryService.LoadMemories(ctx, userID)`，失败则返回 error。

- [x] **Step 3: 添加 AutoConfigure 方法（D-08）**

```go
func (a *ContextEvolvingReActAgent) AutoConfigure(ctx context.Context) error {
    apiKey := ceconfig.GetString("API_KEY", "")
    if apiKey == "" {
        return fmt.Errorf("API_KEY not configured")
    }
    apiBase := ceconfig.GetString("API_BASE", "https://api.openai.com/v1")
    modelName := ceconfig.GetString("MODEL_NAME", "gpt-5.2")
    modelProvider := ceconfig.GetString("MODEL_PROVIDER", "openai")
    // 构建 ReActAgentConfig 并调用 a.Configure(ctx, config)
    ...
}
```

- [x] **Step 4: 补无 query 警告日志（D-22）**

Invoke 无 query 时补 `logger.Warn(logComponent).Msg("No query provided in inputs")`

- [x] **Step 5: 更新测试**

- [x] **Step 6: 运行测试**

Run: `go test ./internal/agentcore/context_evolver/... -v -count=1`

- [x] **Step 7: 提交**

```bash
git add internal/agentcore/context_evolver/context_evolving_react_agent.go internal/agentcore/context_evolver/context_evolving_react_agent_test.go
git commit -m "fix(context-evolver): ContextEvolvingReActAgent 双路径构造/LoadMemories/AutoConfigure/警告日志"
```

---

## Task 8: LLM/Embedding Wrapper 清理 + 编译验证

**Files:**
- Modify: `internal/agentcore/context_evolver/service/llm_wrapper.go`
- Modify: `internal/agentcore/context_evolver/service/llm_wrapper_test.go`
- Modify: `internal/agentcore/context_evolver/service/embedding_wrapper.go`
- Modify: `internal/agentcore/context_evolver/service/embedding_wrapper_test.go`

- [x] **Step 1: 删除 llm_wrapper.go 中的编译期接口断言（D-03）**

删除 `var _ cecontext.LLMService = (*OpenAILLMWrapper)(nil)`
删除 embedding_wrapper.go 中的 `var _ cecontext.EmbeddingService = (*OpenAIEmbeddingWrapper)(nil)`

- [x] **Step 2: 删除 embedding_wrapper_test.go 中的接口断言测试**

删除 `TestOpenAIEmbeddingWrapper_实现EmbeddingService接口`

- [x] **Step 3: 调整 NewOpenAILLMWrapper 构造中的 APIKey 兜底**

APIKey 参数为空时从 `ceconfig.GetString("API_KEY", "")` 读取（对齐 Python `api_key = api_key or config.get("API_KEY")`）

- [x] **Step 4: 调整 NewOpenAIEmbeddingWrapper 构造中的 APIKey 兜底**

同上。

- [x] **Step 5: 运行测试**

Run: `go test ./internal/agentcore/context_evolver/... -v -count=1`

- [x] **Step 6: 提交**

```bash
git add internal/agentcore/context_evolver/service/llm_wrapper.go internal/agentcore/context_evolver/service/llm_wrapper_test.go internal/agentcore/context_evolver/service/embedding_wrapper.go internal/agentcore/context_evolver/service/embedding_wrapper_test.go
git commit -m "refactor(context-evolver): 删除接口断言，wrapper 构造加 config APIKey 兜底"
```

---

## Task 9: doc.go 更新 + IMPLEMENTATION_PLAN.md 状态同步

**Files:**
- Modify: `internal/agentcore/context_evolver/core/doc.go`
- Modify: `internal/agentcore/context_evolver/service/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [x] **Step 1: 更新 core/doc.go 添加 config/ 子包**

- [x] **Step 2: 更新 service/doc.go 反映签名变更**

- [x] **Step 3: 更新 IMPLEMENTATION_PLAN.md 状态**

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/context_evolver/core/doc.go internal/agentcore/context_evolver/service/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 doc.go 和 IMPLEMENTATION_PLAN.md 状态"
```

---

## Task 10: 全量编译 + 测试验证

- [x] **Step 1: 检查残留 go 进程**

Run: `pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true`

- [x] **Step 2: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

- [x] **Step 3: 全量测试**

Run: `go test -cover ./internal/agentcore/context_evolver/... -count=1`

- [x] **Step 4: 推送所有提交**

Run: `git push`
