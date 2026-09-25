# 48h 逻辑审查报告 (2026-09-25)

> 审查范围：48 小时内提交的代码，覆盖 7.11 GraphMemory、7.12 Graph Extraction、9.82 P4-P6 Context Evolver、10.6.3-10 Swarm Rails
> 审查方法：逐方法对比 Python 参考实现，检查签名一致性、步骤缺失、逻辑偏差、占位代码

---

## 审查范围确认

48 小时内完成的实现计划章节：

| 章节 | 状态 | 关键提交 |
|------|------|---------|
| 7.11 GraphMemory | ✅ | `c8fba646` 完成 7.11 + 7.12 |
| 7.12 Graph Extraction | ✅ | `c8fba646` 完成 7.11 + 7.12 |
| 9.82 P4 ReasoningBank | ✅ | `ee2d45dc` 实现 RB 检索/摘要/MaTTS |
| 9.82 P5 ACE | ✅ | `c9eb190e` 实现 ACE Summary Ops |
| 9.82 P6 服务层+Agent集成 | ✅ | `be83a97b` 实现 TaskMemoryService+ContextEvolvingReActAgent |
| 10.6.3-10 Swarm Rails | ✅ | `2a3fecfe` StreamEventRail, `4c21e171` RuntimePromptRail修复 |

---

## 1. 7.11 GraphMemory

### S-01 🔴 严重：`updateEntitiesForRelationRemoval` 将关系 UUID 误当实体 UUID 查询

**Python 样例** (`base.py:1114-1116`):
```python
for relation in state.to_remove:
    entities_to_remove_relations_from.add(relation.lhs if isinstance(relation.lhs, str) else relation.lhs.uuid)
    entities_to_remove_relations_from.add(relation.rhs if isinstance(relation.rhs, str) else relation.rhs.uuid)
```
Python 遍历 `state.to_remove`（Relation 对象列表），取每个 relation 的 **lhs** 和 **rhs**（关系两端的实体 UUID），加入待查集合。

**Go 问题** (`base.go:1695-1697`):
```go
for _, item := range state.ToRemove {
    entitiesToRemoveRelationsFrom[item.UUID] = struct{}{}
}
```
Go 使用 `item.UUID`，这是**关系自身的 UUID**，不是关系两端实体的 UUID。导致查询 Entity 集合时用的是关系 UUID，查询结果为空，整个函数实际无效——被删除关系的实体不会从其 `entity.relations` 中移除对应关系引用。

**修复方案**:
```go
for _, item := range state.ToRemove {
    // 对齐 Python: relation.lhs / relation.rhs
    entitiesToRemoveRelationsFrom[item.LHS] = struct{}{}
    entitiesToRemoveRelationsFrom[item.RHS] = struct{}{}
}
```
需确认 `toRemoveItem` 结构体是否有 LHS/RHS 字段；如果没有，需要修改 `state.ToRemove` 的类型定义，使其包含关系两端的实体 UUID。

---

### S-02 🔴 严重：`resolveEachRelation` 使用 `context.Background()` 丢弃传入的 ctx

**Python 样例** (`base.py:885`):
```python
query_result = await self.db_backend.query(RELATION_COLLECTION, ids=src_entity.relations)
```
Python 中 `query` 是 async 调用，天然受 asyncio 取消信号控制。

**Go 问题** (`base.go:2147`):
```go
queryResult, err := gm.DBBackend.Query(context.Background(), graph.RelationCollection, graph.WithIDs(srcRelationIDs...))
```
Go 硬编码 `context.Background()`，完全忽略了传入的 `ctx` 参数。当 ctx 被取消（超时/用户中断），此处仍会执行数据库查询，无法响应取消信号。

**修复方案**:
```go
queryResult, err := gm.DBBackend.Query(ctx, graph.RelationCollection, graph.WithIDs(srcRelationIDs...))
```

---

### S-03 🔴 严重：`entityMerge` 中 `resolved` 返回值丢弃了已有实体，导致关系抽取时实体列表不完整

**Python 样例** (`base.py:933` + `parse_llm_response.py:resolve_entities`):
```python
# resolve_entities 返回 (extracted_declarations, merging_args, entity_uuids_to_remove)
# extracted_declarations 是混合列表，可能包含 EntityDeclaration 和 Entity 对象
# 已合并到已有实体的候选声明被替换为 Entity 对象
extracted_declarations = resolved[0]  # 包含 EntityDeclaration + Entity 混合列表
```
Python 中 `resolve_entities` 返回的 `extracted_declarations` 可能包含 `Entity` 对象（代表已合并到的已有实体），这些已有实体会继续参与后续 `ParseAllRelations` 的关系抽取。

**Go 问题** (`base.go:1313-1321`):
```go
var result []extraction.EntityDeclaration
for _, item := range resolved {
    switch v := item.(type) {
    case *extraction.EntityDeclaration:
        result = append(result, *v)
    case *graph.Entity:
        // 已有实体不需要加入声明列表（Python: 它们已在 existing_entities 中）
    }
}
```
Go 丢弃了 `*graph.Entity` 类型的已有实体，导致：
1. 传给 `ParseAllRelations` 的 `entities` 列表缺失已有实体
2. 关系抽取时的 `source_id`/`target_id` 可能索引越界（Python 中这些索引对应混合列表，Go 中只对应声明子集）

**修复方案**:
需要重新设计 `result` 的类型为混合列表（`[]any` 或定义 `ResolvedEntity` 联合类型），保留 `Entity` 对象：
```go
type resolvedItem struct {
    declaration *extraction.EntityDeclaration
    entity      *graph.Entity
}
var result []resolvedItem
for _, item := range resolved {
    switch v := item.(type) {
    case *extraction.EntityDeclaration:
        result = append(result, resolvedItem{declaration: v})
    case *graph.Entity:
        result = append(result, resolvedItem{entity: v})
    }
}
```
然后 `ParseAllRelations` 需要能处理混合类型的输入列表。

---

### M-01 🟡 一般：`entityDedupe` 任务获取时未等待完成

**Python 样例** (`base.py:920-923`):
```python
if state.tasks:
    await asyncio.wait(state.tasks)  # 先等待所有任务完成
if existing_entities_list:
    response_resolve_entity = await state.tasks.pop()  # 再取结果
```

**Go 问题** (`base.go:1280-1281`):
```go
dedupeTask := state.Tasks[len(state.Tasks)-1]
```
Go 直接取最后一个 task 引用，但该 task 可能尚未完成（没有 `Wait()` 调用）。虽然后续 `dedupeTask.Wait()` 会等待，但如果在 `entityDedupe` 入口和 `Wait()` 之间有其他逻辑依赖 task 结果，可能读到空数据。

**修复方案**: 在取 task 前先等待所有 tasks 完成：
```go
for _, t := range state.Tasks {
    t.Wait()
}
dedupeTask := state.Tasks[len(state.Tasks)-1]
```

---

### M-02 🟡 一般：`initState` 中 `summary_target` 格式化使用 `%d` 只支持整数

**Python 样例** (`base.py:542`):
```python
state.extras = dict(summary_target=str(strategy.summary_target))
```
Python 的 `str()` 可以处理 int/enum/float 等多种类型。

**Go 问题** (`base.go:866`):
```go
state.Extras = map[string]any{"summary_target": fmt.Sprintf("%d", strategy.SummaryTarget)}
```
Go 用 `%d` 只处理整数。如果 `SummaryTarget` 不是整数类型（如枚举），会输出错误格式。

**修复方案**: 改用 `%v` 或 `fmt.Sprint()`:
```go
state.Extras = map[string]any{"summary_target": fmt.Sprint(strategy.SummaryTarget)}
```

---

### M-03 🟡 一般：`search` 返回的图对象字段不完整

**Python 样例**: Python search 结果中 `BaseGraphObject` 是完整的图对象（包含 relations/episodes/attributes 等完整字段），从搜索结果重建。

**Go 问题**: Go 的 `stateLookupEntity/Relation/Episode` 只填充了部分字段（uuid/name/content/user_id 等），缺少 relations/episodes/attributes 等字段。调用者如果依赖完整字段，会得到空切片/nil。

**修复方案**: 在 `search` 方法中，对查询到的图对象通过 `state.LookupTable` 补充缺失字段，或在 doc 中明确说明 search 返回的是轻量对象。

---

### M-04 🟡 一般：`relationDedupe` 中 LLM 调用同步执行，Python 异步并发

**Python 样例** (`base.py:1100`):
```python
state.tasks.append(asyncio.create_task(self._invoke_llm(...)))  # 异步并发
```

**Go 问题** (`base.go:1672`):
```go
gm.InvokeLLM(ctx, ...)  // 同步逐个调用
```

**影响**: 性能差异（Go 串行执行关系去重 LLM 调用），功能结果相同。

**修复方案**: 使用 goroutine + errgroup 并发调用，对齐 Python 的并发模型。

---

### M-05 🟡 一般：`entityEnrich` 中 `entities` 和 `state.Tasks` 顺序依赖

**Python 样例** (`base.py:1003-1010`):
```python
for entity, future in zip(entities, state.tasks):
    response = await future
```

**Go 问题** (`base.go:1444-1465`):
```go
for _, task := range state.Tasks { task.Wait() }  // 等待所有
// 然后逐个更新
```
Go 假设 `entities` 和 `state.Tasks` 顺序一致（与 Python 相同），但没有显式 zip 关联。如果顺序不一致，会更新错误的实体。

**修复方案**: 考虑在 Task 中记录关联的 entity 名称，更新时按名称匹配而非按位置。

---

### T-01 🔵 提示：`invokeLLMAsync` 中 LLM 客户端为 nil 时只设 Err 不返回错误

**Go 问题** (`base.go:1854-1855`):
```go
task.Err = fmt.Errorf("LLM client is not set")
```
Goroutine 继续运行并关闭 done channel。调用者通过 `task.Wait()` + `task.Err` 检查错误，功能正确但与 `InvokeLLM` 方法直接返回 error 的风格不一致。

---

### T-02 🔵 提示：`parseRelationFilteringResult` 添加了 Python 不存在的边界检查

**Go 防御性代码** (`base.go:1496`):
```go
if id, ok := toInt(idVal); ok && id >= 1 && id <= len(relationList)
```
Python 直接用列表推导 `relations_filtered = [new_relation_list[i - 1] for i in keep_ids]`，无边界检查。Go 的防御性检查更好，不会因 LLM 返回无效索引而 panic。

---

### T-03 🔵 提示：Go 中 `DedupeRelationTask` 使用同步 Response 字段

**Python**: `List[Tuple[Relation, List[Relation], asyncio.Future]]` — 保留 future
**Go**: `DedupeRelationTask{Relation, ExistingRelations, Response string}` — 直接存已解析响应

Go 的设计更简单直接，因为 LLM 调用是同步的。正确的设计差异。

---

## 2. 9.82 Context Evolver P4-P6

### S-04 🔴 严重：`ContextEvolvingReActAgent.Invoke` 缺少 `auto_summarize` 后处理逻辑

**Python 样例** (`context_evolving_react_agent.py:217-218`):
```python
# invoke() docstring:
# If ``auto_summarize`` is enabled, it automatically summarizes the interaction
# as a side effect before returning.
```
Python 的 `invoke()` 方法在 docstring 中承诺 `auto_summarize` 时自动总结交互，且 `auto_summarize` 和 `auto_summarize_matts_mode` 作为构造参数存储。

**Go 问题** (`context_evolving_react_agent.go:268-329`):
```go
func (a *ContextEvolvingReActAgent) invokeWithMemory(...) (map[string]any, error) {
    // ... 检索记忆 → 增强输入 → 调父类
    result, err := a.ReActAgent.Invoke(ctx, augmentedInputs, opts...)
    // 直接返回，没有任何 auto_summarize 逻辑
}
```
Go 的 `invokeWithMemory` 在调用父类 `Invoke` 后直接返回，完全没有检查 `a.autoSummarize` 标志，也没有调用 `SummarizeTrajectories`。当 `auto_summarize=true` 时，交互轨迹不会被自动总结为记忆，功能完全缺失。

**修复方案**: 在 `invokeWithMemory` 返回前添加：
```go
// 对齐 Python: auto_summarize 后处理
if a.autoSummarize && a.memoryService != nil {
    trajectory := formatTrajectoryFromResult(result)
    if trajectory != "" {
        gt := ""
        sumParams := service.SummarizeTrajectoriesInput{
            Query:      query,
            Trajectory: []string{trajectory},
            MattsMode:  a.autoSummarizeMattsMode,
        }
        if _, summarizeErr := service.SummarizeTrajectories(ctx, a.memoryService, a.userID, sumParams); summarizeErr != nil {
            logger.Error(logger.ComponentAgentCore).Err(summarizeErr).Msg("Auto-summarize failed")
        }
    }
}
```

---

### S-05 🔴 严重：`NewContextEvolvingReActAgent` 双路径构造时缺少 config 初始化

**Python 样例** (`context_evolving_react_agent.py:100-106`):
```python
if memory_service is not None:
    self.memory_service = memory_service
else:
    self.memory_service = TaskMemoryService(
        persist_type=persist_type,
        persist_path=persist_path,
        milvus_host=milvus_host,
        milvus_port=milvus_port,
        milvus_collection=milvus_collection,
    )
```
Python 中 `TaskMemoryService()` 无参构造时会自动从 `config.yaml` 读取 `MODEL_NAME/EMBEDDING_MODEL/API_KEY` 等。

**Go 问题** (`context_evolving_react_agent.go:93-104`):
```go
if memoryService == nil && persistType != nil {
    cfg := &service.TaskMemoryServiceConfig{
        PersistType:      persistType,
        PersistPath:      persistPath,
        MilvusHost:       milvusHost,
        MilvusPort:       milvusPort,
        MilvusCollection: milvusCollection,
    }
    memoryService, err = service.NewTaskMemoryService(cfg)
}
```
Go 的双路径构造只在 `persistType != nil` 时才创建 `TaskMemoryService`，而 Python 在 `memory_service is None` 时**无论 persist_type 是否为 None** 都会创建。此外，Go 只传了持久化参数，没有传 `LLMModel/EmbeddingModel/APIKey/APIBase` 等核心参数，这些在 `applyConfigDefaults` 中会使用硬编码默认值 `gpt-5.2`，而不是从 config.yaml 读取。

**修复方案**:
1. 去掉 `persistType != nil` 条件，对齐 Python：`memoryService == nil` 就创建
2. 从 `ceconfig` 读取 LLM/Embedding 配置填入 `TaskMemoryServiceConfig`：
```go
if memoryService == nil {
    cfg := &service.TaskMemoryServiceConfig{
        LLMModel:       ceconfig.GetString("MODEL_NAME", "gpt-5.2"),
        EmbeddingModel: ceconfig.GetString("EMBEDDING_MODEL", "text-embedding-3-small"),
        APIKey:         ceconfig.GetString("API_KEY", ""),
        APIBase:        ceconfig.GetString("API_BASE", "https://api.openai.com/v1"),
        // ... persist 参数
    }
    memoryService, err = service.NewTaskMemoryService(cfg)
}
```

---

### S-06 🔴 严重：`AutoConfigure` 只重建 TaskMemoryService 但丢失了 persist 配置和算法选择

**Python 样例** (`context_evolving_react_agent.py:124-150`):
```python
def _auto_configure(self) -> None:
    api_key = memory_config.get("API_KEY", "")
    if not api_key:
        return
    config = ReActAgentConfig()
    config.configure_model_client(
        provider=memory_config.get("MODEL_PROVIDER", "OpenAI"),
        api_key=api_key,
        api_base=memory_config.get("API_BASE", "https://api.openai.com/v1"),
        model_name=memory_config.get("MODEL_NAME", "gpt-4"),
    )
    config.configure_prompt_template([{"role": "system", "content": default_system_prompt}])
    config.configure_max_iterations(5)
    self.configure(config)
```
Python 的 `_auto_configure` 只配置 **ReActAgent** 的模型客户端，不重建 `TaskMemoryService`。

**Go 问题** (`context_evolving_react_agent.go:233-262`):
```go
func (a *ContextEvolvingReActAgent) AutoConfigure(ctx context.Context) error {
    // ...
    if a.memoryService != nil {
        cfg := &service.TaskMemoryServiceConfig{
            LLMModel:       modelName,
            EmbeddingModel: ceconfig.GetString("EMBEDDING_MODEL", "text-embedding-3-small"),
            APIKey:         apiKey,
            APIBase:        apiBase,
        }
        newSvc, err := service.NewTaskMemoryService(cfg)
        // ...
        a.memoryService = newSvc  // 替换了整个 memoryService
    }
    return nil
}
```
Go 的 `AutoConfigure` **重建了整个 TaskMemoryService**，但新创建的 `cfg` 没有传递 `RetrievalAlgo/SummaryAlgo/PersistType` 等参数，导致：
1. 算法选择丢失（回退到默认 ACE）
2. 持久化配置丢失（persistenceHelper 变为 nil）
3. 已加载的记忆（vectorStore 中的数据）丢失
4. 缺少对 ReActAgent 本身的模型配置（Python 会 `self.configure(config)` 更新 Agent 的模型）

**修复方案**:
1. 不重建 TaskMemoryService，而是更新 LLM/Embedding wrapper 的模型配置
2. 添加对 ReActAgent 的 `Configure()` 调用，对齐 Python

---

### M-06 🟡 一般：`SummarizeTrajectories` 中 `score` 类型不匹配 — Python 用 `float`，Go 用 `int`

**Python 样例** (`trajectory_generator.py:221`):
```python
extra_kwargs["label"] = [s == 1.0 for s in scores]  # score 是 float 类型
```

**Go 问题** (`trajectory_generator.go:201`):
```go
labels[i] = (s == 1)  // score 是 int 类型
```
Python 中 `score` 可能为浮点数（如 0.5），`s == 1.0` 只有精确等于 1.0 时为 True。Go 中 `s == 1` 是整数比较，行为等价。但 Python 中 `scores` 可能来自评估函数返回的浮点分数，如果 Go 传入浮点分数会被截断为整数。

**修复方案**: 将 `Score []int` 改为 `Score []float64`，对齐 Python 的浮点分数语义。

---

### M-07 🟡 一般：`RunTrials` 缺少 `matts_k` 从 config 读取的 fallback

**Python 样例** (`trajectory_generator.py:496-497`):
```python
if matts_k is None:
    matts_k = int(memory_config.get("MATTS_DEFAULT_K", 3))
```
Python 中 `matts_k` 可以是 `None`，此时从 config 读取 `MATTS_DEFAULT_K`。

**Go 问题** (`trajectory_generator.go:124-127`):
```go
mattsK := params.MattsK
if mattsK <= 0 {
    mattsK = 3  // 硬编码 fallback
}
```
Go 用硬编码 `3` 而非从 `ceconfig` 读取 `MATTS_DEFAULT_K`。

**修复方案**:
```go
if mattsK <= 0 {
    mattsK = ceconfig.GetInt("MATTS_DEFAULT_K", 3)
}
```

---

### M-08 🟡 一般：`AddMemory` 中 ACE memoryID 格式与 Python 不一致

**Python 样例** (`task_memory_service.py:978-979`):
```python
content_hash = hashlib.md5(request.content.encode()).hexdigest()[:8]
memory_id = f"{request.section}-{content_hash}"
```
Python 使用 `-` 连接 section 和 hash。

**Go 问题** (`task_memory_service.go:809-810`):
```go
hash := md5.Sum([]byte(req.Content))
memoryID := fmt.Sprintf("%s_%x", section, hash[:8])
```
Go 使用 `_` 连接，且 `%x` 对 8 字节输出 16 个 hex 字符（不是 8 个），Python `hexdigest()[:8]` 取 8 个 hex 字符。

**修复方案**:
```go
contentHash := md5.Sum([]byte(req.Content))
hexStr := fmt.Sprintf("%x", contentHash)
memoryID := fmt.Sprintf("%s-%s", section, hexStr[:8])
```

---

### M-09 🟡 一般：`AddMemory` 中 `ReMe` memoryID 格式与 Python 不一致

**Python 样例** (`task_memory_service.py:954-958`): Python 中 ReMe 记忆的 ID 由 `VectorNode` 的 `to_vector_node()` 方法生成（UUID 格式），不是 `reme_{workspaceID}_{md5(when_to_use)[:12]}`。

**Go 问题** (`task_memory_service.go:783-784`):
```go
contentHash := md5.Sum([]byte(whenToUse))
memoryID := fmt.Sprintf("reme_%s_%x", userID, contentHash[:6])
```
Go 自行生成了 MD5 格式的 ID，而非使用 Python 的 `ReMeMemory.to_vector_node()` 逻辑。这可能导致持久化后 Python 和 Go 读取对方的数据时 ID 不兼容。

**修复方案**: 对齐 Python 的 `ReMeMemory.to_vector_node()` 中的 ID 生成逻辑。

---

### M-10 🟡 一般：`LoadMemories` 持久化加载后未记录加载计数日志

**Python 样例** (`task_memory_service.py:732-735`):
```python
logger.info("Loaded %d memories into vector store (algo=%s)", count, algo_name)
```

**Go 问题** (`task_memory_service.go:447`):
```go
return nil  // 没有记录加载计数
```

**修复方案**:
```go
logger.Info(logComponent).
    Int("count", loaded).
    Str("algo", algoName).
    Msg("Loaded memories into vector store")
```

---

### M-11 🟡 一般：`formatTrajectoryFromResult` 实现过于简化

**Python 样例** (`trajectory_generator.py:109-158`):
```python
def format_trajectory(messages: list) -> str:
    # 完整的轨迹格式化逻辑：处理 Task: 前缀、去除记忆注入、保留 Question: 标记等
```
Python 从 session context 获取完整消息列表，逐条格式化为 USER/THOUGHT/ACTION/OBSERVATION 格式。

**Go 问题** (`context_evolving_react_agent.go:341-351`):
```go
func formatTrajectoryFromResult(result map[string]any) string {
    output, _ := result["output"].(string)
    if output != "" {
        return fmt.Sprintf("USER: query\nASSISTANT: %s", output)
    }
    return ""
}
```
Go 的实现极度简化——只取 `output` 字段，丢失了完整的工具调用轨迹（THOUGHT/ACTION/OBSERVATION），导致摘要时 LLM 看不到工具使用过程，影响记忆质量。

**注意**: Go 的 `trajectory_generator.go` 中已实现 `formatTrajectory(messages []Message)` 正确逻辑，但 `Execute()` 使用的是 `formatTrajectoryFromResult()` 桩函数，两者未连接。

**修复方案**: 在 `Execute()` 中改为使用 `formatTrajectory()`，或从 session context 提取完整消息列表传入。

---

### S-16 🔴 严重：`RunTrials` 缺少持久化前加载和后保存逻辑

**Python 样例** (`trajectory_generator.py:452-531`):
```python
# 运行前：加载已有记忆
if params.persist_type is not None:
    persistence_helper = MemoryPersistenceHelper(...)
    data = persistence_helper.load(params.user_id, algo_name)
    for node_id, node_data in data.items():
        node = VectorNode.from_dict(node_data)
        params.memory_service.vector_store.load_node(node_id, node)

# 运行后：保存更新记忆
if persistence_helper is not None:
    all_nodes = params.memory_service.vector_store.get_all()
    nodes_dict = {n.id: n.to_dict() for n in all_nodes}
    persistence_helper.save(params.user_id, algo_name, nodes_dict)
```

**Go 问题** (`trajectory_generator.go:116-169`):
```go
func RunTrials(ctx context.Context, ...) ([]TrialOutput, error) {
    // 直接运行试验，没有加载已有记忆
    results, err := runTrialsInner(...)
    // 调用 SummarizeTrajectories，但没有保存更新后的记忆
    if params.MemoryService != nil {
        SummarizeTrajectories(...)
    }
    return results, nil
}
```
Go 的 `RunTrials` 完全缺少：
1. **运行前加载**：`persistType` 非空时不从持久化后端加载已有记忆到 vectorStore
2. **运行后保存**：总结后不将更新后的 vectorStore 保存回持久化后端

**影响**: 在独立模式（`PersistType` 设置）下运行试验时，Agent 以空记忆开始，总结后的新记忆也不被保存，下次运行还是空的，记忆系统完全失效。

**修复方案**: 对齐 Python 的两段逻辑：
```go
// 运行前加载
var persistenceHelper *cepersistence.MemoryPersistenceHelper
if params.PersistType != nil {
    persistenceHelper = cepersistence.NewMemoryPersistenceHelper(...)
    nodesDict, _ := persistenceHelper.Load(params.UserID, algoName)
    for id, nodeData := range nodesDict {
        vn, _ := schema.VectorNodeFromDict(nodeData.(map[string]any))
        params.MemoryService.vectorStore.Upsert(ctx, vn)
    }
}

// ... 运行试验 + 摘要 ...

// 运行后保存
if persistenceHelper != nil {
    allNodes := params.MemoryService.vectorStore.GetAll(nil)
    saveDict := make(map[string]any)
    for _, n := range allNodes {
        saveDict[n.ID] = n.ToDict()
    }
    persistenceHelper.Save(params.UserID, algoName, saveDict)
}
```

---

### M-19 🟡 一般：`AddMemory` 中 ACE `section` 字段零值为空字符串，Python 默认 "general"

**Python 样例** (`task_memory_service.py:84`):
```python
@dataclass
class AddMemoryRequest:
    section: str = "general"  # 默认 "general"
```

**Go 问题** (`task_memory_service.go:91`):
```go
Section string  // 零值为 ""，无默认值
```
Go 的 `AddMemoryRequest.Section` 是 `string`（零值 `""`），如果调用者未设置，Go 会因 `req.Section == ""` 校验失败而拒绝请求，Python 则默认 "general" 通过。

**修复方案**: 在 `applyConfigDefaults` 中或 `AddMemory` 入口处设置默认值：
```go
if req.Section == "" {
    req.Section = "general"
}
```

---

### T-05 🔵 提示：`NewContextEvolvingReActAgent` 构造时未调用 `AutoConfigure`

**Python 样例** (`context_evolving_react_agent.py:116`):
```python
self._auto_configure()  # 构造时自动从 .env 配置
```

**Go 问题** (`context_evolving_react_agent.go:76-135`): 构造函数从不调用 `AutoConfigure()`，用户必须手动调用。

**影响**: 不调用 `AutoConfigure()` 时 Agent 的模型客户端不会被配置（除非通过外部传入已配置好的 `memoryService`）。

**修复方案**: 在构造函数末尾添加 `agent.AutoConfigure(ctx)` 调用（需处理 API_KEY 缺失时静默返回，对齐 Python）。

---

## 3. 7.12 Graph Extraction

### S-07 🔴 严重：`multilingualResponseFormat` 缺少 `StrictSchemaEnforce` 调用

**Python 样例** (`extraction/base.py` 中 `response_format(language)` → `multilingual_model_json_schema(language, strict=True)`):
```python
# Python 在生成 schema 后自动调用 strict 模式：
# BFS 设置 additionalProperties=false 和 required=all_keys 于所有 type=object 节点
multilingual_model_json_schema(language, strict=True)
```
Python 所有 9 个 `Extract*` 函数生成的 response_format schema 都经过 `strict=True` 处理。

**Go 问题** (`extraction_prompts.go:523-542`):
```go
func multilingualResponseFormat(...) ResponseFormat {
    // ... extract schema → replace descriptions → ToJSONSchemaMap
    schemaMap := commonschema.ToJSONSchemaMap(replaced)
    // 缺少：StrictSchemaEnforce(schemaMap)
    return ResponseFormat(...)
}
```
Go 的 `multilingualResponseFormat` 从未调用 `StrictSchemaEnforce`，而 `BuildResponseFormat`（单独的函数）已正确调用。这导致所有通过 `multilingualResponseFormat` 生成的 schema 缺少 `additionalProperties: false` 和 `required` 字段，OpenAI structured output schema 不完整，LLM 可能产生多余字段或格式错误的 JSON。

**修复方案**: 在 `schemaMap := commonschema.ToJSONSchemaMap(replaced)` 之后添加一行：
```go
StrictSchemaEnforce(schemaMap)
```

---

### S-08 🔴 严重：`ReadableSchema` 缺少 Python 的 title/required 删除和 $ref 替换逻辑

**Python 样例** (`base.py:48-69` `readable_schema()`):
```python
# 1. 移除所有 "title" 键
_recursive_replace(schema, lookup={}, from_key="title")
# 2. 移除所有 "required" 键
_recursive_replace(schema, lookup={}, from_key="required")
# 3. 替换 $ref 引用：#/$defs/EntityDeclaration → EntityDeclaration
# 4. 删除 $defs
```

**Go 问题** (`base.go:67-91` `ReadableSchema()`):
Go 的 `ReadableSchema` → `formatReadableSchema` → `extractRefDict`：
1. **没有**移除 "title" 或 "required" 键
2. `extractRefDict` 在 `properties` 子字典内搜索 `$defs`（非顶层），与 Python 的 `schema["$defs"]` 结构不同
3. **没有**替换 `$ref` 引用为类型名
4. **没有**删除 `$defs`

**影响**: `extra_message`（schema 信息）内容与 Python 不同。title/required 未移除增加了 schema 噪音，$ref 未替换导致嵌套类型定义未解析，可能让 LLM 困惑。

**修复方案**: 对齐 Python 的 4 步处理：
1. 递归删除所有 "title" 键
2. 递归删除所有 "required" 键
3. 顶层取 `$defs`，替换 `$ref: #/$defs/X` → `type: X`
4. 删除 `$defs` 键

---

### M-15 🟡 一般：`FormatExistingRelations` 中 `valid_since` 默认值行为差异

**Python 样例**: `valid_since = rel.get("valid_since", 0)` — 默认 0，然后检查 `valid_since != -1`，0 通过检查，调用 `load_stored_time_from_db(0, 0)` → 返回 epoch 1970-01-01。

**Go 问题**: `toInt64(rel["valid_since"])` — 如果键缺失，返回 `(0, false)`，`ok` 检查失败，跳过时间格式化。

**影响**: 当 `valid_since` 缺失时，Python 输出 `valid_since=1970-01-01T00:00:00+00:00`，Go 完全跳过。行为差异。

---

### M-16 🟡 一般：`formatReadableSchema` 输出格式差异

**Python 样例**: 遍历 `cls.model_fields.items()` 生成扁平格式 `field_name: type  # description\n`

**Go 问题**: `base.go:176-211` 递归遍历 JSON schema map，输出带 `class Output:` 的嵌套块格式

**影响**: readable schema 字符串格式不同，可能导致 LLM 行为差异。

---

## 4. 10.6.3-10 Swarm Rails

### S-09 🔴 严重：`toolCallArguments` 返回 string 而非 parsed dict — 前端解析失败

**Python 样例** (`stream_event_rail.py:479`):
```python
"arguments": getattr(tool_call, "arguments", {})  # ToolCall.arguments 是 dict
```

**Go 问题** (`stream_event_helpers.go:212-220`):
```go
func toolCallArguments(tc *llmschema.ToolCall) any {
    return tc.Arguments  // Arguments 是 string 类型，不是 map[string]any
}
```
Go 的 `ToolCall.Arguments` 是 `string`（JSON 字符串），直接发给前端会导致前端收到 `"arguments": "{\"key\": \"val\"}"` 而非 `"arguments": {"key": "val"}`。

**修复方案**: 解析 JSON 字符串为 `map[string]any` 后发送：
```go
func toolCallArguments(tc *llmschema.ToolCall) any {
    var m map[string]any
    if err := json.Unmarshal([]byte(tc.Arguments), &m); err == nil {
        return m
    }
    return map[string]any{}
}
```

---

### S-10-rails 🔴 严重：`checkpointWait` 阻塞不响应 context 取消

**Python 样例** (`stream_event_rail.py:384-388`):
```python
await self._get_pause_event(sid).wait()  # asyncio.Event 可被取消
```

**Go 问题** (`stream_event_rail.go:476-481`):
```go
func (r *JiuClawStreamEventRail) checkpointWait(sid string) {
    pc.Wait()  // sync.Cond.Wait() 无限阻塞，无法响应 context 取消
}
```
如果暂停永远不被恢复（如会话断开），goroutine 永久阻塞。

**修复方案**: 使用 channel + select 或 context 感知的条件等待实现，传入 `ctx context.Context` 参数并在 `ctx.Done()` 时返回。

---

### S-11 🔴 严重：`truncateString` 按字节截断而非 Unicode 字符 — 产生无效 UTF-8

**Python 样例** (`stream_event_rail.py:495`):
```python
"result": str(result)[:60000]  # Python 字符串切片操作 Unicode 字符
```

**Go 问题** (`stream_event_helpers.go:188-193`):
```go
func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen]  // 按字节截断
}
```
对中文等多字节字符，`s[:60000]` 可能在 UTF-8 序列中间截断，产生无效 UTF-8，导致 JSON payload 损坏。

**修复方案**:
```go
func truncateString(s string, maxLen int) string {
    runes := []rune(s)
    if len(runes) <= maxLen {
        return s
    }
    return string(runes[:maxLen])
}
```

---

### S-12 🔴 严重：`inferStringError` 正则缺少大小写不敏感标志

**Python 样例** (`stream_event_rail.py:164`):
```python
re.search(r"\bsuccess\s*[:=]\s*False\b", text, re.IGNORECASE)
```

**Go 问题** (`stream_event_helpers.go:311`):
```go
regexp.MatchString(`\bsuccess\s*[:=]\s*False\b`, text)  // 缺少 (?i)
```
`SUCCESS=FALSE` 或 `success=false` 在 Go 中不会被检测为错误，但在 Python 中会。`exit_code` 正则也缺少 `(?i)`。

**修复方案**: 使用 `(?i)` 前缀：
```go
regexp.MatchString(`(?i)\bsuccess\s*[:=]\s*False\b`, text)
```

---

### S-13 🔴 严重：`AvatarPromptRail` 缺少 `_build_memory_disabled_prompt`（写入禁用但读取允许）

**Python 样例** (`avatar_rail.py:246-264`):
```python
def _build_memory_disabled_prompt(language):
    # 生成"记忆写入已禁用"section — 写入禁止但读取允许
```
Python `before_model_call` 中有两个独立条件：
1. 记忆完全禁用 → `memory_fully_disabled` section
2. **仅写入禁用（群聊数字人模式）** → `memory_disabled` section

**Go 问题** (`avatar_rail.go:148-159`): 只实现了"完全禁用"情况（`buildMemoryFullyDisabledPrompt`），"写入禁用但读取允许"的 `memory_disabled` section 完全缺失。

**修复方案**: 添加 `buildMemoryDisabledPrompt` 函数，在 `is_group_digital_avatar && enable_memory` 条件下注入 `memory_disabled` section。

---

### M-12 🟡 一般：`RuntimePromptRail.BeforeModelCall` 忽略传入的 ctx

**Python 样例** (`runtime_prompt_rail.py:before_model_call`):
Python 的 `before_model_call(ctx)` 会使用 ctx 中的请求级参数（如 `language`、`channel`）。

**Go 问题** (`runtime_prompt_rail.go:188`):
```go
func (r *RuntimePromptRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
```
Go 用 `_` 忽略了 ctx，改为从 `cbc` 和 rail 自身字段读取。但 `cbc` 中可能携带 per-request 的参数（如语言、频道），Go 需要从 `cbc` 中提取这些参数并更新 rail 状态，否则每次模型调用用的是上次 `Set*` 设置的值。

**影响**: 在并发场景下，多个请求可能交叉设置语言/频道，导致数据竞争。Python 通过 `AgentCallbackContext` 的 per-request 属性避免了此问题。

**修复方案**: 在 `BeforeModelCall` 入口处从 `cbc` 提取 per-request 参数，或在 rail 中加锁保护读写。

---

### M-13 🟡 一般：`JiuClawStreamEventRail` 缺少 `_extract_tool_interrupt` 递归解析

**Python 样例** (`stream_event_rail.py:62-74`):
```python
def _extract_tool_interrupt(value: Any) -> Any | None:
    if value.__class__.__name__ == "ToolInterruptException" and hasattr(value, "request"):
        return value
    for attr_name in ("cause", "__cause__"):
        cause = getattr(value, attr_name, None)
        if cause is not None and cause is not value:
            interrupt = _extract_tool_interrupt(cause)
            if interrupt is not None:
                return interrupt
    return None
```
Python 递归检查异常链（`cause` / `__cause__`），从嵌套的错误中提取 `ToolInterruptException`。

**Go 问题**: Go 的 `AfterToolCall` 处理中断时，可能只检查了顶层的 error，没有递归解包 `errors.Unwrap()` 链来查找 `ToolInterruptException`。如果工具调用异常被外层 error 包装，Go 可能无法检测到中断。

**修复方案**: 使用 `errors.As()` 递归查找中断异常，对齐 Python 的 `_extract_tool_interrupt`：
```go
var toolIntErr *ToolInterruptError
if errors.As(err, &toolIntErr) {
    // 处理中断
}
```

---

### M-14 🟡 一般：`RuntimePromptRail` 缺少 `timezone_offset` 参数

**Python 样例** (`runtime_prompt_rail.py:36-37`):
```python
def __init__(self, language: str = "cn", channel: str = "web", timezone_offset: int = 8):
```

**Go 问题** (`runtime_prompt_rail.go:109`):
```go
func NewRuntimePromptRail(language, channel string) *RuntimePromptRail {
```
Go 构造函数缺少 `timezoneOffset` 参数，时间注入节使用的是 Go 默认时区而非配置指定的时区偏移量（如 UTC+8）。

**修复方案**: 添加 `timezoneOffset int` 参数，在 `injectTimeSection` 中使用：
```go
func NewRuntimePromptRail(language, channel string, timezoneOffset int) *RuntimePromptRail {
```

---

## 5. 9.82 P5 ACE

### S-14 🔴 严重：`Bullet.ApplyMetadata` 用 `+=` 增量叠加，Python 用 `setattr` 直接赋值

**Python 样例** (`playbook.py:86-89`):
```python
def apply_metadata(self, metadata: Dict[str, int]) -> None:
    for key, value in metadata.items():
        if hasattr(self, key):
            setattr(self, key, int(value))  # 直接赋值（覆盖）
```

**Go 问题** (`playbook.go:429-440`):
```go
func (b *Bullet) ApplyMetadata(metadata map[string]int) {
    for key, value := range metadata {
        switch key {
        case "helpful":
            b.Helpful += value  // 增量叠加，非覆盖
        case "harmful":
            b.Harmful += value
        case "neutral":
            b.Neutral += value
        }
    }
}
```

**影响**: `AddBullet` 场景下（bullet 初始值为 0），`0 += value` = `0 = value` 等效。但 `UpdateBullet` 场景下，如果传入 `metadata={"helpful": 3}`，Python 将 helpful 设为 3，Go 将 helpful **增加** 3。CurateOp 的 TAG 操作走 `tag_bullet` 不受影响，但 UPDATE 操作行为不同。

**修复方案**: 改为直接赋值对齐 Python `setattr` 语义：
```go
case "helpful":
    b.Helpful = value
```

---

### S-15 🔴 严重：`LoadPlaybookOp` 闭包内 playbook 赋值不传播到闭包外

**Python 样例** (`update.py:86-88`):
```python
try:
    # 加载 playbook 逻辑
except Exception as e:
    logger.warning("Failed to load playbook: %s. Starting with empty playbook.", e)
    context.playbook = Playbook()  # 直接修改 context
```

**Go 问题** (`update.go:164-188`): 使用匿名函数 + `defer recover` 模式，但闭包内 `playbook = NewPlaybook()` 修改的是闭包内的局部变量，不会影响外部 `playbook` 变量。外部 `rc.Set("playbook", playbook)` 仍用闭包外声明的 `playbook`。

**影响**: 如果 `playbook.LoadBullet(bullet)` 内部 panic，`recover` 会重置闭包内的 `playbook`，但外部仍用旧值。异常回退功能失效。

**修复方案**: 将整个逻辑不使用匿名函数/闭包，直接在 Execute 方法体内用 error 处理，对齐 Python 的 try/except 模式。

---

### M-17 🟡 一般：`ApplyDeltaOp` 中 `removeCount` 未做 `max(0, ...)` 保护

**Python 样例** (`update.py:439`):
```python
remove_count = max(0, add_count + current_count - self.max_bullets)
```

**Go 问题** (`update.go:607`):
```go
removeCount := addCount + currentCount - o.maxBullets
```
Go 虽然有 `if removeCount > 0` 阻止负数进入循环，但缺少 `max(0, ...)` 的显式语义保证，后续逻辑扩展时可能遗漏。

**修复方案**:
```go
removeCount := addCount + currentCount - o.maxBullets
if removeCount < 0 {
    removeCount = 0
}
```

---

### M-18 🟡 一般：`ACERecallMemoryOp` 存 `ACEMemory` 而非 `ACERetrievedMemory`

**Python 样例** (`run.py:66`): 将 VectorNode 转为 `ACERetrievedMemory`（专门用于检索结果的类型，只含 id/section/content/helpful/harmful/neutral）

**Go 问题** (`run.go:82-91`): 转为 `ACEMemory`（内部存储类型，包含 created_at/updated_at 等额外字段）

**影响**: 存储了不必要的字段。Python 有专门的检索结果类型，Go 缺少。

**修复方案**: 对齐 Python，将 ACEMemory 转为只含检索所需字段的类型后存入 RuntimeContext。

---

### T-04 🔵 提示：`SafeJSONLoads` regex 缺少 `(?s)` flag，跨行 JSON 可能失败

**Python 样例** (`utils.py:27`):
```python
re.search(r'```(?:json)?\s*(\{.*?\})\s*```', text, re.DOTALL)
```

**Go 问题** (`utils.go:20`):
```go
regexp.MustCompile("```(?:json)?\\s*(\\{.*?\\})\\s*```")  // 缺少 (?s)
```
Go 标准库 `regexp` 的 `.` 默认不匹配 `\n`，如果 JSON 跨行，策略 2（markdown code block 提取）会失败。

**修复方案**: 将 regex 改为 `(?s)```(?:json)?\s*(\{.*?\})\s*```` 以匹配跨行 JSON。同样 `reAnyJSONObject` 也需加 `(?s)`。

---

## 6. ⤵️ 占位代码审查

### P-01: `IMPLEMENTATION_PLAN.md` 中 9.82 P7 标记为 ☐

`9.82 P7(☐ ContextEvolutionRail + MilvusConnector + 工具层)` — 确认尚未实现，标记正确。

### P-02: `ContextEvolvingReActAgent` 中 `llm_temperature` 处理为 TODO

```go
// TODO(#9.82): ReActAgent.Invoke 支持读取 llm_temperature 传给 Model.Generate
if llmTemp, ok := inputs["llm_temperature"]; ok {
    _ = llmTemp // 当前 ReActAgent 不支持动态温度，待后续集成
}
```
确认：这是已知占位，MaTTS ParallelScalingOp 通过此路径注入不同温度以提高多样性，当前功能缺失。

### P-03: `TaskMemoryService.createReMeMemory` 缺少 `created_at/updated_at` 时间戳

**Python 样例** (`task_memory_service.py:958-959`):
```python
created_at=datetime.now(timezone.utc),
updated_at=datetime.now(timezone.utc),
```

**Go 问题** (`task_memory_service.go:786-798`): Go 的 `createReMeMemory` metadata 中没有 `created_at`/`updated_at` 字段，Python 中这些是 `ReMeMemory` 的标准字段。

**修复方案**: 在 metadata 中添加时间戳：
```go
now := time.Now().UTC().Format(time.RFC3339)
metadata["created_at"] = now
metadata["updated_at"] = now
```

### P-04: `TaskMemoryService.createACEMemory` 缺少 `created_at/updated_at` 时间戳

同 P-03，Python 的 `ACEMemory` 包含 `created_at`/`updated_at` 字段，Go 的 `createACEMemory` 中缺失。

---

## 问题统计

| 级别 | 数量 | 编号 |
|------|------|------|
| 🔴 严重 | 16 | S-01 ~ S-16 |
| 🟡 一般 | 19 | M-01 ~ M-19 |
| 🔵 提示 | 6 | T-01 ~ T-06 |
| **合计** | **41** | |

### 按章节分布

| 章节 | 🔴 严重 | 🟡 一般 | 🔵 提示 |
|------|---------|---------|---------|
| 7.11 GraphMemory | 3 (S-01,S-02,S-03) | 5 (M-01~M-05) | 3 (T-01~T-03) |
| 7.12 Graph Extraction | 2 (S-07,S-08) | 2 (M-15,M-16) | 0 |
| 9.82 P5 ACE | 2 (S-14,S-15) | 2 (M-17,M-18) | 1 (T-04) |
| 9.82 P6 Service/Agent | 4 (S-04,S-05,S-06,S-16) | 7 (M-06~M-11,M-19) | 1 (T-05) |
| 10.6.3-10 Swarm Rails | 5 (S-09,S-10-rails,S-11,S-12,S-13) | 3 (M-12~M-14) | 1 (T-06) |

### 严重问题优先修复顺序

| 优先级 | 编号 | 章节 | 问题 | 影响范围 |
|--------|------|------|------|---------|
| P0 | S-01 | 7.11 | `updateEntitiesForRelationRemoval` 用关系UUID当实体UUID | 关系删除后实体引用不更新，数据一致性破坏 |
| P0 | S-03 | 7.11 | `entityMerge` 丢弃已有实体 | 关系抽取实体列表不完整，索引越界风险 |
| P0 | S-14 | P5 ACE | `Bullet.ApplyMetadata` 用+=而非= | UPDATE 操作 helpful/harmful 计数错误 |
| P0 | S-16 | P6 | `RunTrials` 缺持久化前加载+后保存 | 独立模式下记忆系统完全失效 |
| P1 | S-04 | P6 Agent | `auto_summarize` 逻辑完全缺失 | 记忆自动总结功能无效 |
| P1 | S-07 | 7.12 | `multilingualResponseFormat` 缺 StrictSchemaEnforce | LLM structured output schema 不完整 |
| P1 | S-08 | 7.12 | `ReadableSchema` 缺 title/required/$ref 处理 | schema 信息噪音，LLM 困惑 |
| P1 | S-09 | 10.6 | `toolCallArguments` 返回 string 非 dict | 前端解析失败 |
| P1 | S-11 | 10.6 | `truncateString` 按字节截断 | 中文截断产生无效 UTF-8 |
| P2 | S-02 | 7.11 | `context.Background()` 丢弃 ctx | 取消信号无法传播 |
| P2 | S-05 | P6 Agent | 双路径构造条件不一致 | memoryService 可能为 nil |
| P2 | S-06 | P6 Agent | `AutoConfigure` 丢失 persist/算法配置 | 重新配置后功能退化 |
| P2 | S-10-rails | 10.6 | `checkpointWait` 不响应 context 取消 | goroutine 永久阻塞 |
| P2 | S-12 | 10.6 | `inferStringError` 缺大小写不敏感 | 错误检测结果遗漏 |
| P2 | S-13 | 10.6 | Avatar 缺 `_build_memory_disabled_prompt` | 群聊数字人记忆写入提示缺失 |
| P2 | S-15 | P5 ACE | LoadPlaybookOp 闭包作用域 bug | 异常回退功能失效 |
