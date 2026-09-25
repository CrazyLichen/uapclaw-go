# 9.82 P7 ContextEvolutionRail + MilvusConnector 设计

## 概述

9.82 P7 是 Context Evolver 的最后一环，将 P1-P6 构建的算法+服务基础设施接入 Agent 运行时。

### 交付物

| # | 交付物 | 文件位置 | 说明 |
|---|--------|----------|------|
| 1 | ContextEvolutionRail | `harness/rails/evolution/context_evolution_rail.go` | DeepAgentRail 子类，记忆检索注入 + 自动轨迹总结 |
| 2 | MilvusConnector 实现 | `context_evolver/core/persistence/milvus_connector.go` | 独立实现，直接用 milvus-sdk-go |
| 3 | MemoryPersistenceHelper auto 模式 | `context_evolver/core/persistence/persistence_helper.go` | 惰性探测 Milvus 可达性→回退 JSON |
| 4 | 9.24 P6 回填 | `evolution/doc.go` + `IMPLEMENTATION_PLAN.md` | 包导出和状态更新 |

### 明确跳过

- **工具层**（experience_retrieve / experience_learn / experience_clear）：Python 中不存在这些工具，ContextEvolutionRail 完全通过 before/after 钩子透明运作。实现计划中的工具层标记为"Python 无此实现，跳过"。

---

## 1. ContextEvolutionRail

### 1.1 继承结构

对齐 Python 原始设计：**直接嵌入 `rails.DeepAgentRail`**，不经过 `EvolutionRail` 基类。

```
DeepAgentRail
├── EvolutionRail ──→ SkillEvolutionRail / TeamSkillEvolutionRail / TrajectoryRail
└── ContextEvolutionRail  ← P7 新增，独立子类
```

Python 中 ContextEvolutionRail 也是 DeepAgentRail 的独立子类，不继承 EvolutionRail。

### 1.2 结构体

```go
type ContextEvolutionRail struct {
    rails.DeepAgentRail

    // 构造参数
    userID                  string
    memoryService           *service.TaskMemoryService
    injectMemoriesInContext bool
    autoSummarize           bool
    autoSummarizeMattsMode  string

    // 每次迭代重置的状态
    memoriesUsed            int
    originalPromptTemplate  []map[string]any

    // 检索缓存
    lastRetrievedQuery      string
    lastRetrievalResult     map[string]any

    // Agent 引用（首次迭代捕获）
    agent                   agentinterfaces.BaseAgent

    // 当前查询（before 保存，after 使用）
    currentQuery            string
}
```

Priority = 50（对齐 Python）。

### 1.3 构造函数

```go
type ContextEvolutionRailOption func(*ContextEvolutionRail)

func NewContextEvolutionRail(
    userID string,
    memoryService *service.TaskMemoryService,
    opts ...ContextEvolutionRailOption,
) *ContextEvolutionRail
```

- `memoryService` 为 nil 时，自动创建默认 `TaskMemoryService`（对齐 Python）
- 构造末尾调用 `memoryService.LoadMemories(userID)`（对齐 Python `__init__` 第 68 行）
- 选项函数：`WithInjectMemoriesInContext`, `WithAutoSummarize`, `WithAutoSummarizeMattsMode`

### 1.4 回调注册

```go
func (r *ContextEvolutionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
    callbacks := r.DeepAgentRail.GetCallbacks()
    callbacks[agentinterfaces.CallbackBeforeTaskIteration] = r.beforeTaskIteration
    callbacks[agentinterfaces.CallbackAfterTaskIteration] = r.afterTaskIteration
    return callbacks
}
```

只注册 2 个回调，没有空方法噪音。

### 1.5 BeforeTaskIteration 流程

对齐 Python `context_evolution_rail.py` 108-193 行：

```
1. 重置每次迭代状态：memoriesUsed=0, originalPromptTemplate=nil
2. 捕获 agent 引用（首次迭代时 cbc.Agent()）
3. 从 ctx.Inputs() 获取 query 和 retrieval_query
   - type-assert inputs 到 *TaskIterationInputs
   - retrieval_query 优先，否则 fallback 到 query
4. 保存 currentQuery 供 afterTaskIteration 使用
5. 检索记忆：
   - 命中缓存（lastRetrievedQuery == retrievalQuery）→ 复用 lastRetrievalResult
   - 否则调用 memoryService.Retrieve(ctx, userID, retrievalQuery)
   - 更新 lastRetrievedQuery / lastRetrievalResult
   - 提取 memory_string 和 retrieved_memory 数量 → memoriesUsed
   - 检索失败：记 Error 日志，return（不中断 Agent）
6. 记忆注入（条件：memoriesUsed>0 && memoryString!="" && injectMemoriesInContext）：
   a. type-assert agent → *DeepAgent → ReactAgent() → Config() → *ReActAgentConfig
   b. 深拷贝当前 PromptTemplate → originalPromptTemplate（备份）
   c. 构造 memory_block：
      "Some Related Experience to help you complete the task:\n{memoryString}"
   d. 遍历 PromptTemplate，在 system message 的 content 末尾追加 "\n\n{memory_block}"
   e. 复制 config，设置新的 PromptTemplate
   f. 通过 ReactAgent.Configure(ctx, newConfig) 应用
```

### 1.6 AfterTaskIteration 流程

对齐 Python `context_evolution_rail.py` 199-243 行：

```
1. 恢复原始提示词模板：
   - 若 originalPromptTemplate != nil：
     a. type-assert agent → *DeepAgent → ReactAgent() → Config() → *ReActAgentConfig
     b. 复制 config，设置 PromptTemplate = originalPromptTemplate
     c. Configure() 应用
     d. 清空 originalPromptTemplate = nil

2. 标注 memoriesUsed：
   - 通过 cbc.Extra()["memories_used"] = r.memoriesUsed 写入跨 Rail 通信字典
   - 下游 Rail 或调用方可从 Extra 中读取

3. 自动总结（条件：autoSummarize && currentQuery != ""）：
   a. 调用 extractTrajectory(ctx) 获取轨迹文本
   b. 若轨迹非空：
      - 调用 TrajectoryGenerator.EvaluateTrial(currentQuery, trajectory) → (feedback, score)
      - 构造 SummarizeTrajectoriesInput：
        Query: currentQuery
        Trajectory: [trajectory]
        MattsMode: "none"（仅支持 none，对齐 Python 注释）
        Feedback: [feedback]
        Score: [score]
      - 调用 TrajectoryGenerator.SummarizeTrajectories(ctx, memoryService, userID, input)
   c. 异常只记 Error 日志，不中断（对齐 Python except 后 logger.error）
```

### 1.7 ExtractTrajectory

对齐 Python `context_evolution_rail.py` 249-267 行：

```
1. 获取 agent 和 session（cbc.Session()）
2. type-assert agent → *DeepAgent → ReactAgent()
3. 通过 ReactAgent().ContextEngine() 获取 ContextEngine
4. 获取对话历史（GetMessages 或等效方式）
5. 调用 TrajectoryGenerator.FormatTrajectory(messages) 格式化
6. 异常只记 Warning 日志，返回 nil
```

### 1.8 只读访问器

对齐 Python 的 @property：

- `PendingTools() []any` — 返回 nil（Python 中也未使用，测试验证为空）
- `ToolsApplied() bool` — 返回 false（同上）
- `Agent() agentinterfaces.BaseAgent`
- `CurrentQuery() string`
- `MemoriesUsed() int`

---

## 2. MilvusConnector 独立实现

### 2.1 设计决策

对齐 Python：**独立实现**，不复用 `foundation/store/vector/MilvusVectorStore`。

理由（和 Python 相同）：
- context_evolver 有自己的固定 5 字段 schema
- 命名空间分区逻辑（namespace 字段过滤）
- upsert 语义（delete-then-insert）
- 和通用向量存储的使用模式差异较大

### 2.2 文件位置

`internal/agentcore/context_evolver/core/persistence/milvus_connector.go`

### 2.3 结构体

```go
type MilvusConnectorImpl struct {
    host           string
    port           int
    collectionName string
    dim            int       // 0 = 自动检测（从首次 save 推断）
    alias          string    // 连接别名，默认 "default"
    metricType     string    // "COSINE"(默认) / "L2" / "IP"
    client         milvusClient  // 窄接口，用于解耦和测试
    mu             sync.RWMutex
}
```

### 2.4 固定 Schema 常量

```go
const (
    fieldID        = "id"
    fieldNS        = "namespace"
    fieldContent   = "content"
    fieldEmbedding = "embedding"
    fieldMetadata  = "metadata"

    idMaxLen      = 256
    nsMaxLen      = 256
    contentMaxLen = 65535
)
```

### 2.5 实现方法

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `SaveToDB(namespace, data)` | `save_to_db` | Upsert：skip 无 embedding 节点 → delete 已有 PK → insert → flush |
| `LoadFromDB(namespace)` | `load_from_db` | query `namespace == "{ns}"`，返回 `map[string]any` |
| `Exists(namespace)` | `exists` | query limit=1 |
| `Delete(namespace)` | `delete` | query 获取 IDs → delete by PK → flush |
| `Search(namespace, embedding, topK, metric)` | `search` | ANN 搜索，metric 回退到 index 的 metricType |
| `DeleteNodes(namespace, nodeIDs)` | `delete_nodes` | 按 ID 删除 → flush |
| `ListNamespaces()` | `list_namespaces` | query 去重 namespace 字段 |
| `Count(namespace)` | `count` | namespace 非空时按分区计数，否则用 num_entities |
| `Flush()` | `flush` | 刷写缓冲 |
| `Close()` | `close` | 关闭 gRPC 连接 |

`MilvusConnectorImpl` 实现 `persistence.MilvusConnector` 接口（4 方法），同时导出额外的 Search/DeleteNodes/ListNamespaces/Count/Flush/Close 方法。

### 2.6 milvusClient 测试接口

```go
type milvusClient interface {
    CreateCollection(ctx context.Context, ...) error
    HasCollection(ctx context.Context, ...) (bool, error)
    DescribeCollection(ctx context.Context, ...) (*entity.Collection, error)
    Insert(ctx context.Context, ...) (milvusclient.InsertResult, error)
    Search(ctx context.Context, ...) ([]milvusclient.ResultSet, error)
    Query(ctx context.Context, ...) ([]milvusclient.QueryResult, error)
    Delete(ctx context.Context, ...) (milvusclient.DeleteResult, error)
    LoadCollection(ctx context.Context, ...) error
    Flush(ctx context.Context, ...) error
    CreateIndex(ctx context.Context, ...) error
    HasIndex(ctx context.Context, ...) (bool, error)
    Close(ctx context.Context) error
}
```

比 `foundation/store/vector` 的 `milvusClient` 多了 `HasIndex` 和 `Query`，少了一些不需要的方法。

### 2.7 辅助函数

- `Truncate(text string, maxBytes int) string` — UTF-8 安全截断（对齐 Python `MilvusConnector.truncate`）
- `IDsExpr(ids []string) string` — 构建 `id in ["a", "b"]` 表达式（对齐 Python `MilvusConnector.ids_expr`）
- `SetClient(client milvusClient)` — 注入 mock client（对齐 Python `set_collection`，用于单元测试）

### 2.8 测试隔离

- 需要 Milvus 的测试：`//go:build integration`
- 单元测试：注入 fakeMilvusClient，验证接口契约、schema 构造、数据转换逻辑

---

## 3. MemoryPersistenceHelper auto 模式

### 3.1 当前状态

- `resolvedType` 固定为 `"json"`
- `persistType` 默认为 `"json"`（应改为 `"auto"`）
- `SetMilvusConnector()` 已存在但未被使用
- Save/Load 只走 JSON 路径

### 3.2 改动点

1. **默认 persistType 改为 `"auto"`**：对齐 Python。显式传 `"json"` 时固定 JSON。

2. **惰性解析**：首次 Save/Load 时调用 `resolveBackend()`，用 `sync.Once` 保证只执行一次：

   ```
   resolveBackend():
     if persistType == "auto":
       if milvusConnector != nil:
         尝试 Milvus 可达性探测（HasCollection 或等效轻量操作）
           成功 → resolvedType = "milvus"
           失败 → resolvedType = "json"，Warning 日志
       else if milvusHost 可配:
         尝试创建 MilvusConnectorImpl 并探测
           成功 → 注入 milvusConnector，resolvedType = "milvus"
           失败 → resolvedType = "json"，Warning 日志
       else:
         resolvedType = "json"
     elif persistType == "milvus":
       resolvedType = "milvus"
     else:
       resolvedType = "json"
   ```

3. **Save/Load 路由**：

   ```
   Save():
     resolveBackend()（惰性）
     switch resolvedType:
       "milvus" → milvusConnector.SaveToDB(Namespace(userID, algoName), nodesDict)
       "json"   → jsonConnector（现有逻辑）

   Load():
     同上路由
   ```

4. **Milvus 自动创建**：auto 模式下，如果 `milvusConnector` 未注入但有 `milvusHost` 配置，自动创建 `MilvusConnectorImpl` 并注入。

### 3.3 Go 特有设计

- Python 在构造时同步探测 Milvus。Go 改为**惰性探测**避免构造时阻塞（Milvus 连接可能超时）。
- `persistType == "auto"` + Milvus 不可达时，回退 JSON 只记 Warning 不报错（对齐 Python）。

---

## 4. 9.24 P6 回填

### 4.1 evolution/doc.go 更新

文件目录新增：
```
├── context_evolution_rail.go       # ContextEvolutionRail 上下文演化轨道（Priority=50）
```

包功能概述补充：
```
P6（ContextEvolutionRail 上下文演化轨道：记忆检索注入 + 自动轨迹总结）已完成
```

### 4.2 context_evolver/core/persistence/doc.go 更新

文件目录新增：
```
├── milvus_connector.go            # MilvusConnector Milvus 向量数据库连接器实现
```

### 4.3 IMPLEMENTATION_PLAN.md 状态更新

- **9.82 行**：P7 从 `☐` 改为 `✅`，内容追加 `P7(✅ ContextEvolutionRail + MilvusConnector + 9.24 P6回填; 跳过工具层: Python无此实现)`
- **9.24 行**：P6 从 `☐ 上下文演化: ContextEvolutionRail; ⤴️9.82 P7` 改为 `P6(✅ 上下文演化: ContextEvolutionRail(Priority=50+记忆注入+自动总结); ⤴️9.82 P7 ✅)`
- 9.24 行 P1-P5 的 ✅ 状态保持不变
- 9.82 行 P1-P6 的 ✅ 状态保持不变

---

## 5. 日志对齐

对齐 Python `context_evolution_rail.py` 中的所有 logger 调用：

| Python 位置 | Python 日志 | Go 日志 |
|-------------|------------|---------|
| `__init__` 70 | `info "ContextEvolutionRail initialised for user=%s, inject_in_context=%s, auto_summarize=%s"` | `logger.Info(logComponent).Str("user_id",...).Bool("inject",...).Bool("auto_summarize",...).Msg(...)` |
| before 135 | `info "Reusing cached memory retrieval result"` | `logger.Info(logComponent).Msg("复用缓存的记忆检索结果")` |
| before 147 | `info "Retrieved %s memories for query"` | `logger.Info(logComponent).Int("memories_used",...).Msg("检索到记忆")` |
| before 150 | `error "Failed to retrieve memories: %s"` | `logger.Error(logComponent).Err(err).Msg("检索记忆失败")` |
| before 166 | `warning "Agent has no config.prompt_template – skipping memory injection"` | `logger.Warn(logComponent).Msg("Agent 无 config.prompt_template，跳过记忆注入")` |
| before 193 | `debug "Injected memory context into agent system prompt"` | `logger.Debug(logComponent).Msg("已注入记忆上下文到 Agent 系统提示词")` |
| after 212 | `debug "Restored original agent system prompt"` | `logger.Debug(logComponent).Msg("已恢复原始 Agent 系统提示词")` |
| after 230 | `info "Running auto-summarize for current trajectory"` | `logger.Info(logComponent).Msg("正在为当前轨迹运行自动总结")` |
| after 243 | `error "Auto-summarize in after_task_iteration failed: %s"` | `logger.Error(logComponent).Err(err).Msg("after_task_iteration 自动总结失败")` |
| extract 266 | `warning "Failed to extract trajectory: %s"` | `logger.Warn(logComponent).Err(err).Msg("提取轨迹失败")` |

MilvusConnector 日志对齐 Python `milvus_connector.py` 中的所有 logger 调用（约 20 处），字段逐一对齐。

---

## 6. 测试策略

### 6.1 ContextEvolutionRail 测试

放在 `harness/rails/evolution/context_evolution_rail_test.go`（同包测试）：

- `TestNewContextEvolutionRail` — 构造 + 默认值 + LoadMemories 调用验证
- `TestNewContextEvolutionRail_NilMemoryService` — nil 时自动创建 TaskMemoryService
- `TestBeforeTaskIteration_记忆注入` — mock TaskMemoryService.Retrieve → 验证 prompt_template 被修改
- `TestBeforeTaskIteration_缓存命中` — 连续两次相同 query → 第二次走缓存
- `TestBeforeTaskIteration_检索失败` — Retrieve 返回 error → 不修改 prompt，记 Error 日志
- `TestBeforeTaskIteration_无记忆` — Retrieve 返回空 → 不修改 prompt
- `TestBeforeTaskIteration_不注入` — injectMemoriesInContext=false → 不修改 prompt
- `TestAfterTaskIteration_恢复Prompt` — 验证 originalPromptTemplate 恢复
- `TestAfterTaskIteration_AutoSummarize` — mock extractTrajectory + EvaluateTrial + SummarizeTrajectories
- `TestAfterTaskIteration_AutoSummarize失败` — SummarizeTrajectories 返回 error → 只记日志不中断
- `TestExtractTrajectory` — 正常路径
- `TestExtractTrajectory_无ContextEngine` — 返回 nil
- `TestAccessors` — PendingTools/ToolsApplied/Agent/CurrentQuery/MemoriesUsed

覆盖率目标 ≥ 85%。

### 6.2 MilvusConnectorImpl 测试

放在 `context_evolver/core/persistence/milvus_connector_test.go`：

**单元测试（不需要真实 Milvus）：**
- `TestNewMilvusConnectorImpl` — 构造
- `TestSaveToDB_跳过无Embedding` — fakeClient 验证 skip 逻辑
- `TestSaveToDB_Upsert` — fakeClient 验证 delete+insert
- `TestLoadFromDB` — fakeClient 验证 query 转换
- `TestExists` / `TestDelete` — fakeClient
- `TestSearch` — fakeClient
- `TestTruncate` — UTF-8 安全截断
- `TestIDsExpr` — 表达式生成
- `TestIDsExpr_空列表` — 边界
- `TestCount` / `TestListNamespaces` — fakeClient

**集成测试（`//go:build integration`）：**
- `TestMilvusConnectorImpl_端到端` — 需要 `MILVUS_HOST` 环境变量

覆盖率目标 ≥ 85%（不含集成测试）。

### 6.3 MemoryPersistenceHelper auto 模式测试

放在 `context_evolver/core/persistence/persistence_helper_test.go`：

- `TestAuto_默认Auto` — 验证 persistType="auto"
- `TestAuto_Milvus可达` — 注入 mock MilvusConnector → resolvedType="milvus" → Save 走 milvus 路径
- `TestAuto_Milvus不可达` — mock 连接失败 → resolvedType="json" → Save 走 json 路径
- `TestAuto_显式JSON` — persistType="json" → 直接 json，不探测
- `TestAuto_显式Milvus` — persistType="milvus" → resolvedType="milvus"
