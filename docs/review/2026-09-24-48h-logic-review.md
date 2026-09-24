# 48小时逻辑审查报告（2026-09-24）

> 审查范围：最近48小时内完成的章节实现
> 审查方法：逐方法对比 Python 参考项目 vs Go 实现，检查方法签名、步骤完整性、占位代码
> 审查时间：2026-09-24

## 审查章节

| 章节 | 描述 | 对应 Python 路径 | Go 实现路径 |
|------|------|------------------|-------------|
| 7.11 | GraphMemory 图记忆 | `openjiuwen/core/memory/graph/graph_memory/` | `internal/agentcore/memory/graph/graph_memory/` |
| 7.12 | Graph Extraction 图抽取 | `openjiuwen/core/memory/graph/extraction/` | `internal/agentcore/memory/graph/extraction/` |
| 9.82 P4 | ReasoningBank 推理银行 | `openjiuwen/extensions/context_evolver/` | `internal/agentcore/context_evolver/` |
| 9.82 P5 | ACE (Adaptive Context Evolution) | `openjiuwen/extensions/context_evolver/` | `internal/agentcore/context_evolver/` |
| 10.6.3-10 | Swarm Rails + DeepAdapter + Worktree | `jiuwenswarm/agents/harness/common/rails/` + `agent_teams/` | `internal/swarm/agents/harness/common/rails/` + `internal/agentcore/harness/tools/worktree/` |

---

## 问题汇总

| 级别 | 数量 |
|------|------|
| 严重 (S) | 28 |
| 一般 (M) | 26 |
| 提示 (T) | 17 |
| **合计** | **71** |

---

## 严重问题 (S)

### S-01 GraphMemory.AddMemory 缺少 `created_at` 和 `user_id` 传递给 `parse_all_relations`

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:328-341`):
```python
relations, entities = parse_all_relations(
    ensure_list(parse_json(response.content, ...) or []),
    entities=extracted_declarations,
    entity_types=state.entity_types,
    created_at=state.reference_timestamp,
    user_id=user_id,
)
```

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:459-463`):
```go
relations, entities := ParseAllRelations(
    anyListToMapList(extraction.EnsureList(parsedRelations)),
    extractedDeclarations,
    state.EntityTypes,
    // 缺少 created_at 和 user_id 参数
)
```

**影响**：关系对象缺少 `created_at` 和 `user_id` 字段，导致后续持久化时这些字段为零值，影响数据完整性和用户隔离。

**修复方案**：`ParseAllRelations` 函数签名增加 `createdAt float64, userID string` 参数，并在构造 `Relation` 对象时设置这两个字段。

---

### S-02 GraphMemory.extractEntityDeclarations 缺少实体名称嵌入异步任务

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:729-736`):
```python
# 当 not no_existing_entity and extracted_entity_names 时
entity_embed_results = await self.db_backend.embedder.embed_documents(
    extracted_entity_names, batch_size=self.db_backend.config.embed_batch_size
)
state.tasks.append(asyncio.create_task(embed_task))
```

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:1092-1099`):
Go 只判断了 `noExistingEntity`，但**完全没有启动嵌入任务**，也没有将其放入 `state.Tasks`。

**影响**：`fetchRelevantEntities` 依赖 `state.tasks` 中的嵌入结果（Python: `entity_embed_results = await state.tasks.pop(-2)`），Go 中缺失此步骤。虽然 Go 的 `fetchRelevantEntities` 中直接同步调用了嵌入，但丧失了 Python 中嵌入与关系抽取并行执行的并发优势。

**修复方案**：在 `extractEntityDeclarations` 末尾添加异步嵌入任务的创建并追加到 `state.Tasks`，同时在 `fetchRelevantEntities` 中从 `state.Tasks` 获取嵌入结果。

---

### S-03 GraphMemory.fetchRelevantEntities 未从 state.tasks 弹出嵌入结果

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:748-751`):
```python
if len(state.tasks) <= 1:
    return  # 只有关系抽取任务，无嵌入结果
entity_embed_results = await state.tasks.pop(-2)
state.tasks = state.tasks[-1:]
```

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:1106-1129`):
Go 直接调用 `embedder.EmbedDocuments()` 同步嵌入，不从 `state.Tasks` 取嵌入结果，也不修改 `state.Tasks`。且缺少 `len(state.Tasks) <= 1` 的早期返回条件。

**影响**：在 Python 中，嵌入任务与关系抽取并行执行；Go 的实现是同步串行调用，丧失了并发性能优势，且 `state.Tasks` 索引管理逻辑不一致。

**修复方案**：重构为从 `state.Tasks` 中获取嵌入结果（与 S-02 的异步嵌入任务配合），对齐 Python 的并发模型。

---

### S-04 GraphMemory.AddMemory 缺少 `del` 清理步骤和 refresh 语义区分

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:356-368`):
```python
state.clear_references()
del relations, entities, current_episode, response, existing_entities_list
del extracted_declarations
await self.db_backend.refresh(skip_compact=True)   # 步骤15：不压缩

# GC 检查
if self.time_till_next_gc >= 0:
    if time.time() - self._last_gc > self.time_till_next_gc:
        gc.collect()
        self._last_gc = time.time()
        await self.db_backend.refresh(skip_compact=False)  # GC 后：压缩
```

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:503-510`):
```go
state.ClearReferences()
if err := gm.DBBackend.Refresh(ctx, graph.WithFlush(false)); err != nil {
    logger.Warn(logComponent).Err(err).Msg("Graph Memory: refresh 失败")
}
gm.maybeGC(ctx)
```

**影响**：
1. Python 步骤15用 `skip_compact=True`（不压缩），步骤16 GC 时用 `skip_compact=False`（压缩）。Go 两次都用了 `WithFlush(false)`，丢失了压缩语义区分。
2. `maybeGC` 中也没有区分压缩语义。

**修复方案**：
1. 在步骤15改用 `graph.WithSkipCompact(true)` 选项（如果 BaseGraphStore 支持）
2. `maybeGC` 中改用 `graph.WithSkipCompact(false)` 触发压缩

---

### S-05 GraphMemory.Search 结果缺少完整的图对象字段

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:433-438`):
```python
g_obj_cls = dict(ENTITY_COLLECTION=Entity, RELATION_COLLECTION=Relation, EPISODE_COLLECTION=Episode)[col]
result[col] = [
    (returned_dict.pop("distance", 0.0), g_obj_cls(**returned_dict)) for returned_dict in returned_list
]
```

Python 中 `g_obj_cls(**returned_dict)` 使用**完整搜索结果字典**构造对象，所有字段被填充。

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:1919-1933`):
```go
func stateLookupEntity(r map[string]any, uuid string) *graph.Entity {
    e := graph.NewEntity()
    e.UUID = uuid
    if v, ok := r["name"].(string); ok { e.Name = v }
    if v, ok := r["content"].(string); ok { e.Content = v }
    if v, ok := r["user_id"].(string); ok { e.UserID = v }
    return e  // 遗漏了 Relations、Episodes 等列表字段
}
```

**影响**：搜索返回的图对象不完整，调用方无法获取完整的关系/片段引用。

**修复方案**：使用通用方法从 `map[string]any` 完整构造图对象，对齐 Python 的 `g_obj_cls(**returned_dict)` 模式。

---

### S-06 GraphMemory.resolveEachRelation 使用 context.Background() 丢弃传入的 ctx

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:2104`):
```go
gm.DBBackend.Query(context.Background(), ...)  // 丢弃了传入的 ctx
```

**影响**：当上层 ctx 被取消时（超时/用户取消），关系查询不会中断，可能导致资源泄漏或超时。

**修复方案**：改为使用传入的 `ctx`。

---

### S-07 GraphMemory.relationDedupe 中 lhs/rhs 缺少 content 空值检查

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:1060-1064`):
```python
lhs_rhs = [e if isinstance(e, str) else (e.uuid if e.content.strip() else None)
           for e in (new_relation.lhs, new_relation.rhs)]
if not all(lhs_rhs):
    continue  # 跳过端点为空的关系
```

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:1566-1568`):
```go
lhs := newRelation.LHS
rhs := newRelation.RHS
if lhs == "" || rhs == "" {
    continue
}
```

Go 只检查空字符串，不检查 entity content 是否为空（Python 中 lhs/rhs 可能是 Entity 对象，需检查其 content）。

**影响**：Python 中如果 entity content 为空，该端点被设为 None，整个关系被跳过；Go 不会跳过空 content 的关系。

**修复方案**：在 Go 中添加与 Python 等效的 content 空值检查逻辑。当 lhs/rhs 是 Entity 对象（非 UUID 字符串）时，检查 content 是否为空。

---

### S-08 GraphMemory.relationDedupe 缺少 `_endpoint_to_entity_dict` 等价逻辑

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:1042-1056`):
```python
def _endpoint_to_entity_dict(endpoint, state):
    if isinstance(endpoint, str):
        return state.retrieved_entities.get(endpoint)
    elif isinstance(endpoint, Entity):
        return endpoint.model_dump()
    ...
```

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:1610-1617`):
Go 仅从 `state.LookupTable.Entities` 查找实体，缺少对 Entity 对象（而非 UUID）的处理，也没有错误处理。

**修复方案**：对齐 Python 的 `_endpoint_to_entity_dict` 逻辑，包括对 Entity 对象和 UUID 字符串两种情况的处理。

---

### S-09 GraphMemory.entityMerge 中 resolved 列表排除 Entity 导致摘要不更新

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:926-935`):
```python
resolved = resolve_entities(extracted_declarations, existing_entities, dedupe_list)
# resolved 包含混合类型列表 list[EntityDeclaration | Entity]
# Entity 类型的元素会继续参与后续流程
```

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:1283-1291`):
```go
var result []extraction.EntityDeclaration
for _, item := range resolved {
    switch v := item.(type) {
    case *extraction.EntityDeclaration:
        result = append(result, *v)
    case *graph.Entity:
        // 已有实体不需要加入声明列表 ← 错误！Python 中它们仍参与后续流程
    }
}
```

**影响**：Python 中 `resolve_entities` 后的列表包含已存在的 Entity（合并目标），这些 Entity 会继续参与后续的关系抽取和实体摘要流程；Go 中排除了它们，可能导致合并目标实体的摘要/属性不会被更新。

**修复方案**：保留 resolved 列表中的 `*graph.Entity`，让它们参与后续的 `ParseAllRelations` 和 `entityEnrich` 流程。需要调整 `ParseAllRelations` 的签名以接受混合类型。

---

### S-10 GraphMemory.updateEntitiesForRelationRemoval 将关系 UUID 当作实体 UUID

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:1114-1116`):
```python
for relation in state.to_remove:
    # 从 relation.lhs 和 relation.rhs 获取实体 UUID
    entities_to_remove_relations_from.add(relation.lhs if isinstance(relation.lhs, str) else relation.lhs.uuid)
    entities_to_remove_relations_from.add(relation.rhs if isinstance(relation.rhs, str) else relation.rhs.uuid)
```

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:1652-1654`):
```go
for _, item := range state.ToRemove {
    entitiesToRemoveRelationsFrom[item.UUID] = struct{}{}  // 关系的 UUID，不是实体的 UUID！
}
```

**影响**：**严重逻辑错误** — Go 将关系的 UUID 当作实体的 UUID 来查询，导致：
1. 查询不到正确的受影响实体
2. 实体的 `relations` 列表中残留已删除关系的引用

**修复方案**：`toRemoveItem` 需要存储完整的 `*graph.Relation` 对象，在 `updateEntitiesForRelationRemoval` 中使用 `relation.LHS`/`relation.RHS` 查找受影响实体。

---

### S-11 GraphMemory.handleRelationDedupe 遍历中修改切片导致索引偏移

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:1510-1517`):
```go
for _, item := range state.ToRemove {
    for i, rel := range relations {
        if rel.UUID == item.UUID {
            relations = append(relations[:i], relations[i+1:]...)  // 修改切片后索引偏移
            break
        }
    }
}
```

**影响**：修改切片后索引偏移，可能跳过某些关系或越界。

**修复方案**：改为从后向前遍历，或使用过滤方式构建新切片：
```go
filtered := make([]*graph.Relation, 0, len(relations))
removeSet := make(map[string]struct{}, len(state.ToRemove))
for _, item := range state.ToRemove { removeSet[item.UUID] = struct{}{} }
for _, rel := range relations {
    if _, ok := removeSet[rel.UUID]; !ok {
        filtered = append(filtered, rel)
    }
}
relations = filtered
```

---

### S-12 ApplyMetadata 语义不一致 — Python 直接赋值 vs Go 增量累加

**Python 样例** (`openjiuwen/extensions/context_evolver/summary/task/ace/playbook.py:86-89`):
```python
def apply_metadata(self, metadata: Dict[str, int]) -> None:
    for key, value in metadata.items():
        if hasattr(self, key):
            setattr(self, key, int(value))  # 直接赋值！
```

**Go 问题** (`internal/agentcore/context_evolver/summary/task/ace/playbook.go:429-439`):
```go
func (b *Bullet) ApplyMetadata(metadata map[string]int) {
    for key, value := range metadata {
        switch key {
        case "helpful":
            b.Helpful += value  // 增量累加！应改为直接赋值
        case "harmful":
            b.Harmful += value
        case "neutral":
            b.Neutral += value
        }
    }
}
```

**影响**：`setattr(self, key, int(value))` 是**直接赋值**（如 `self.helpful = 5`），Go 的 `+=` 是增量累加。在 `AddBullet` 中，新 Bullet 初始计数为 0，`apply_metadata` 设置初始值，此时直接赋值和增量累加结果相同。但在 `UpdateBullet` 场景中（如 metadata=`{"helpful": 3}`），Python 直接设为 3，Go 会在当前值上累加 3，结果不一致。

**修复方案**：将 `+=` 改为直接赋值 `=`：
```go
case "helpful":
    b.Helpful = value
case "harmful":
    b.Harmful = value
case "neutral":
    b.Neutral = value
```

---

### S-13 SafeJSONLoads 正则缺少 `(?s)` DOTALL 标志

**Python 样例** (`openjiuwen/extensions/context_evolver/summary/task/ace/utils.py:27`):
```python
json_match = re.search(r'```(?:json)?\s*(\{.*?\})\s*```', text, re.DOTALL)
```

**Go 问题** (`internal/agentcore/context_evolver/summary/task/ace/utils.go:20`):
```go
reMarkdownCodeBlock = regexp.MustCompile("```(?:json)?\\s*(\\{.*?\\})\\s*```")
```

**影响**：Go 的 `regexp` 包默认 `.` 不匹配 `\n`，而 Python 的 `re.DOTALL` 使 `.` 匹配换行。如果 LLM 返回的 JSON 跨多行，Go 的 markdown code block 正则无法提取，导致 JSON 解析失败。

**修复方案**：在 Go 正则中启用 `(?s)` 标志使 `.` 匹配换行：
```go
reMarkdownCodeBlock = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
reAnyJSONObject = regexp.MustCompile(`(?s)\{.*\}`)
```

---

### S-14 SafeJSONLoads 非贪婪匹配 `.*?` 导致嵌套 JSON 提取不完整

**Python 和 Go 都使用了 `\{.*?\}`（非贪婪）**：
对于嵌套 JSON 如 `{"a": {"b": 1}}`，非贪婪匹配会匹配到 `{"a": {"b": 1}`（到第一个 `}` 就停止），导致 JSON 解析失败。

**影响**：这是 Python 原有的 bug，Go 一比一复刻了。对于 LLM 返回的嵌套 JSON（ACE 场景中很常见），`_safe_json_loads` 在 markdown code block 回退路径下会失败。

**修复方案**：markdown code block 回退应改用贪婪匹配 `(\{.*\})`，因为 code block 边界 ```` ``` ```` 已经界定了 JSON 范围：
```go
reMarkdownCodeBlock = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*\\})\\s*```")
```

---

### S-15 ACERecallMemoryOp 返回类型不对齐 Python — 缺少 ACERetrievedMemory 转换

**Python 样例** (`openjiuwen/extensions/context_evolver/retrieve/task/ace/run.py:53-62`):
```python
retrieved_memory = ACERetrievedMemory(
    id=ace_memory.id, section=ace_memory.section, content=ace_memory.content,
    helpful=ace_memory.helpful, harmful=ace_memory.harmful, neutral=ace_memory.neutral
)
retrieved_memories.append(retrieved_memory)
```

**Go 问题** (`internal/agentcore/context_evolver/retrieve/task/ace/run.go:79-92`):
```go
memories := make([]ceschema.ACEMemory, 0, len(nodes))
// 直接存储 []ACEMemory，缺少到 ACERetrievedMemory 的转换
rc.Set("retrieved_memories", memories)
```

**影响**：`ACERetrievedMemory` 是 `ACEMemory` 的精简子集（不含 `workspace_id`、`created_at`、`updated_at`），接口契约不一致。下游消费者可能期望 `ACERetrievedMemory` 类型。

**修复方案**：定义 `ACERetrievedMemory` 结构体（对齐 Python），在 `ACERecallMemoryOp.Execute` 中将 `ACEMemory` 转换为 `ACERetrievedMemory`。

---

### S-16 GraphMemory.initState 中 referenceTime 转换可能与 safe_timestamp 不一致

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/base.py:544-553`):
```python
state.reference_timestamp = int(safe_timestamp(reference_time))
```

`safe_timestamp` 可能对时区做特殊处理。

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/base.go:840-844`):
```go
if referenceTime == nil {
    state.ReferenceTimestamp = state.CurrentTimestamp
} else {
    state.ReferenceTimestamp = referenceTime.Unix()
}
```

Go 直接用 `.Unix()` 丢弃了时区信息，而 Python 的 `safe_timestamp` 保留时区处理。

**修复方案**：确认 `safe_timestamp` 的实现逻辑是否与 `.Unix()` 一致；如有差异，需对齐转换方式。

---

### S-17 GraphMemory.ClassifyRelationsExtracted 自指向关系实体查找不完整

**Python 样例** (`openjiuwen/core/memory/graph/graph_memory/states.py:333-335`):
Python 中 `relation.lhs` 是 Entity 对象引用，可以直接访问 entity.content。

**Go 问题** (`internal/agentcore/memory/graph/graph_memory/states.go:595-603`):
Go 中 `relation.LHS` 是 UUID 字符串，只从 `state.RetrievedEntities` 中查找实体。如果自指向关系的 lhs 实体不在 `RetrievedEntities` 中，内容不会被追加到实体。

**修复方案**：同时从 `LookupTable` 中尝试查找实体。

---

### S-18 GraphMemory.RegisterSearchStrategy Entity 默认 MinScore 与 Python 不一致

**Python** (`base.py:141-147`):
```python
default=(
    SearchConfig(rank_config=WeightedRankConfig()),  # Entity 默认 MinScore 由 WeightedRankConfig 决定
    SearchConfig(min_score=0.02),                     # Relation
    SearchConfig(min_score=0.025),                    # Episode
)
```

**Go** (`base.go:226-232`):
```go
EntityConfig:   config.NewSearchConfig(),              // Entity 默认 MinScore 可能是 0.3
RelationConfig: newSearchConfigWithMinScore(0.02),
EpisodeConfig:  newSearchConfigWithMinScore(0.025),
```

**影响**：Go 的 `NewSearchConfig()` 默认 MinScore=0.3，而 Python Entity 的默认 MinScore 由 `WeightedRankConfig` 隐含（可能不同）。Entity 搜索的 MinScore 差异可能导致搜索结果数量大幅不同。

**修复方案**：确认 Python 中 Entity SearchConfig 的默认 MinScore 值，对齐到 Go。

---

### S-19 LoadPlaybookOp 用 recover 处理错误而非 Go 惯用的 error 返回

**Go 问题** (`internal/agentcore/context_evolver/summary/task/ace/update.go:164-173`):
```go
func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Any("recover", r)...
            playbook = NewPlaybook()
        }
    }()
```

Python 用 try/except 回退，Go 用 `defer/recover`。虽然功能等效，但 Go 惯用法应通过 error 返回值处理。recover 只捕获 panic，如果 `Search` 返回 error（而非 panic），recover 不会触发。

**修复方案**：移除 recover，直接用 `if err != nil` 处理 Search 失败，回退到空 Playbook。

---

### S-20 StreamEventRail 完全未实现（914 行 Python rail）

**Python 样例** (`jiuwenswarm/agents/harness/common/rails/stream_event_rail.py`):
Python 中 `StreamEventRail` 是一个 914 行的完整实现，负责：
- `before_model_call`: 发送 `todo.updated` 和 `todo.cancelled_tools` 事件
- `after_model_call`: 发送 `todo.updated` 事件更新工具状态
- `after_tool_call`: 发送 `todo.updated` 和 `todo.cancelled_tools` 事件
- `on_session_pause` / `on_session_resume` / `on_session_abort`: 管理会话生命周期
- `on_disconnect`: 连接断开时中止会话
- `cleanup_session`: 清理会话状态

**Go 问题** (`internal/swarm/server/adapter/deep_adapter_rails.go:403-409`):
```go
func (d *DeepAdapter) buildStreamEventRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 JiuClawStreamEventRail
    return nil
}
```

**影响**：StreamEventRail 返回 nil，导致 `buildAgentRails` 步骤 5 不注册此 rail。以下 S-21~S-25 均为 StreamEventRail 缺失导致的级联问题。

**修复方案**：完整实现 `StreamEventRail`，对齐 Python 的 914 行实现。这是本次审查中工作量最大的缺失功能。

---

### S-21 StreamEventRail 缺失导致 `on_session_pause/resume/abort` 不被调用

**Python 样例** (`stream_event_rail.py`):
```python
async def on_session_pause(self, session_id, ...):
    await self._fire_pause(session_id, ...)
async def on_session_resume(self, session_id, ...):
    await self._fire_resume(session_id, ...)
async def on_session_abort(self, session_id, ...):
    await self._fire_abort(session_id, ...)
```

**Go 问题**：由于 `buildStreamEventRail` 返回 nil，DeepAdapter 中 `streamEventRail` 字段为 nil，所有 `OnSessionPause`/`OnSessionResume`/`OnSessionAbort` 调用均被跳过。

**影响**：会话暂停/恢复/中止事件不会发送到客户端，用户无法感知会话状态变化。

**修复方案**：实现 StreamEventRail 后，在 DeepAdapter 的会话生命周期方法中调用对应 hook。

---

### S-22 StreamEventRail 缺失导致 `cleanup_session` 不被调用

**Python 样例** (`stream_event_rail.py`):
```python
async def cleanup_session(self, session_id):
    # 清理 session 状态、停止 pending 任务
```

**Go 问题**：`cleanup_session` 等价方法不存在，session 结束后状态残留。

**影响**：会话状态泄漏，长时间运行后内存持续增长。

**修复方案**：在 StreamEventRail 实现中添加 `CleanupSession` 方法，并在 DeepAdapter 的会话结束流程中调用。

---

### S-23 StreamEventRail 缺失导致 `on_disconnect` 不被调用

**Python 样例** (`stream_event_rail.py`):
```python
async def on_disconnect(self, session_id):
    # 客户端断连时中止当前会话
    await self.on_session_abort(session_id, reason="disconnect")
```

**Go 问题**：连接断开时不会中止会话，可能导致 Agent 在客户端已断开后继续执行。

**影响**：资源浪费，且可能导致已完成的结果无法被任何客户端接收。

**修复方案**：在 DeepAdapter 的连接断开处理中，调用 StreamEventRail 的 `OnDisconnect`。

---

### S-24 StreamEventRail 缺失导致 `todo.updated` / `todo.cancelled_tools` 事件不发送

**Python 样例** (`stream_event_rail.py`):
```python
# before_model_call: 发送 todo.updated（agent 思考中）和 todo.cancelled_tools
# after_model_call: 发送 todo.updated（工具调用列表更新）
# after_tool_call: 发送 todo.updated（工具执行结果）
```

**Go 问题**：所有 `todo.updated` 和 `todo.cancelled_tools` 事件都不会被发送。

**影响**：前端无法获得实时的工具执行状态更新，用户体验严重受损。

**修复方案**：在 StreamEventRail 实现中，在每个 hook 点发送对应的事件。

---

### S-25 SummarizeTrajectories 仅为 stub，返回 "not implemented" 错误

**Python 样例** (`openjiuwen/extensions/context_evolver/service/trajectory_generator.py:161-241`):
```python
async def summarize_trajectories(
    memory_service: TaskMemoryService,
    user_id: str,
    params: SummarizeTrajectoriesInput,
) -> Optional[Dict[str, Any]]:
    # 完整实现：处理 trajectories、feedbacks、scores，
    # 根据 SUMMARY_ALGO 构建 extra_kwargs，调用 memory_service.summarize()
```

**Go 问题** (`internal/agentcore/context_evolver/service/trajectory_generator.go:119-121`):
```go
func SummarizeTrajectories(ctx context.Context, memoryService any, userID string, params SummarizeTrajectoriesInput) (map[string]any, error) {
    return nil, fmt.Errorf("not implemented: depends on TaskMemoryService (P6)")
}
```

**影响**：ReasoningBank 的核心持久化流程断裂 — `run_trials` 执行完成后无法将轨迹摘要存入记忆。

**修复方案**：等待 P6 移植 TaskMemoryService 后实现。当前应将此标记为 ⤵️ 并在调用点做错误处理。

---

### S-26 ReasoningBank 中 BestOfNOp/SelfContrastMemoryOp 缺少 try/except fallback

**Python 样例** (`matts.py:241-268, 350-397`):
```python
try:
    response = await llm.async_generate(eval_prompt, temperature=0.0)
    # ... 处理结果
except Exception as e:
    logger.error("Error in Best-of-N selection: %s", e)
    # Fallback: use first trajectory
    context.answer = trajectories[0]['answer']
```

**Go 问题** (`internal/agentcore/context_evolver/retrieve/task/rb/matts.go`):
```go
// BestOfNOp: 无 try/catch，LLM 调用失败直接返回 error
response, err := llm.Generate(ctx, buf.String(), ...)
if err != nil {
    return fmt.Errorf("BestOfNOp: LLM 评估调用失败: %w", err)
}
// SelfContrastMemoryOp: 同样无 fallback
response, err := llm.Generate(ctx, buf.String(), ...)
if err != nil {
    return fmt.Errorf("SelfContrastMemoryOp: LLM 调用失败: %w", err)
}
```

**影响**：Python 中 BestOfNOp 和 SelfContrastMemoryOp 的 LLM 调用失败有 graceful fallback（BestOfN 回退到第一条轨迹，SelfContrast 返回空列表），Go 中直接返回 error 中断整个流程。

**修复方案**：对齐 Python 的 fallback 策略：
- BestOfNOp：LLM 评估失败时，回退到第一条轨迹（`bestIdx = 0`）
- SelfContrastMemoryOp：LLM 调用失败时，设置 `contrastive_memories = []`，返回 nil error

---

### S-27 ReasoningBankMemory 缺少 source_type 字段

**Python 样例** (`openjiuwen/extensions/context_evolver/schema/memory.py:190`):
```python
class ReasoningBankMemory(BaseMemory):
    title: str = Field(...)
    description: str = Field(...)
    source_type: str = Field(default="success", description="Source: success, failure, or comparative")
    helpful_count: int = Field(default=0, ...)
    harmful_count: int = Field(default=0, ...)
```

**Go 问题** (`internal/agentcore/context_evolver/schema/memory.go:82-90`):
```go
type ReasoningBankMemory struct {
    BaseMemory
    Query  string                      `json:"query"`
    Memory []ReasoningBankMemoryItem   `json:"memory"`
    Label  *bool                       `json:"label,omitempty"`
    // 缺少 SourceType、HelpfulCount、HarmfulCount 字段
}
```

Go 的 `ReasoningBankMemory` 缺少：
1. `SourceType` 字段（Python 中 SelfContrastMemoryOp 设置为 `"comparative"`）
2. `HelpfulCount` / `HarmfulCount` 字段（Python 中用于反馈追踪）
3. `Title` / `Description` 字段在顶层（Python 中这些是 ReasoningBankMemory 自身字段，不在 MemoryItem 中）

**影响**：`parseContrastiveMemories` 无法设置 `source_type='comparative'`；`ToVectorNode` 中不保存 `source_type`/`helpful_count`/`harmful_count`，导致向量化后信息丢失。

**修复方案**：对齐 Python schema：
1. 在 `ReasoningBankMemory` 中添加 `SourceType`、`HelpfulCount`、`HarmfulCount` 字段
2. 在 `ReasoningBankMemoryItem` 中保留 `Title`/`Description`/`Content`
3. 在 `ToVectorNode` 和 `NewReasoningBankMemoryFromVectorNode` 中处理这些字段
4. 在 `parseContrastiveMemories` 中设置 `SourceType = "comparative"`

---

### S-28 WorktreeManager.Enter/Exit 未调用 fire lifecycle hooks

**Python 样例** (`openjiuwen/harness/tools/worktree/rails.py:408-479`):
```python
# AutoSetupRail.after_worktree_create: 在 worktree 创建后执行 setup 命令
# DiffSummaryRail.before_worktree_exit: 在 worktree 退出前记录 diff
```

Python 中 `_fire_rail` 在 `enter`/`exit` 流程中被调用，触发 `AutoSetupRail.after_worktree_create` 和 `DiffSummaryRail.before_worktree_exit`。

**Go 问题** (`internal/agentcore/harness/tools/worktree/manager.go:100-242`):
Go 的 `Enter()` 和 `Exit()` 方法中没有调用 `fireBeforeCreate`/`fireAfterCreate`/`fireBeforeExit`/`fireAfterExit`。这些 fire 方法定义在 manager.go L457-558 但从未被调用（死代码）。

**影响**：`AutoSetupRail` 的 setup 命令不会在 worktree 创建后执行；`DiffSummaryRail` 的 diff 记录不会在退出前保存。

**修复方案**：
1. 在 `Enter()` 中，`backend.Create()` 之前调用 `fireBeforeCreate`，`postCreationSetup` 之后调用 `fireAfterCreate`
2. 在 `Exit()` 中，退出前调用 `fireBeforeExit`，之后调用 `fireAfterExit`
3. 需要获取 `AgentCallbackContext`，可能需要修改 `Enter`/`Exit` 签名或在 manager 中存储引用

---

## 一般问题 (M)

### M-01 GraphMemory NewGraphMemory 缺少 db_kwargs 传递

**Python** (`base.py:125-131`): `self.db_backend = GraphStoreFactory.from_config(config=db_config, **db_kwargs)`
**Go** (`base.go:188`): `graph.NewFromConfig(dbConfig)` — 没有 db_kwargs 参数

**修复方案**：增加 `WithDBKwargs` 选项。

---

### M-02 GraphMemory.metricIsSim 初始化硬编码 false

**Python** (`base.py:140`): `self.metric_is_sim = self.db_backend.return_similarity_score`
**Go** (`base.go:225`): `metricIsSim: false` — 注释说"后续从 dbBackend 获取"但从未获取

**修复方案**：在 `NewGraphMemory` 中从 `dbBackend` 获取实际值。

---

### M-03 GraphMemory.Search 中 user_id 接口差异

**Python**: `user_id: Union[str, list[str]]`，自动转换
**Go**: `userIDs []string`，调用者需自己转换

**修复方案**：可提供便捷方法接受单个 string。

---

### M-04 GraphMemory.prepareEpisodes 中 IsEmpty 检查缺少 error 处理

**Go** (`base.go:933-934`): `isEmpty, err := gm.DBBackend.IsEmpty(ctx, ...)` — error 时静默跳过

**修复方案**：对 `IsEmpty` 返回的 error 至少记录一条 Warn 日志。

---

### M-05 GraphMemory.extractEntityDeclarations 中 entityNames 大小写过滤不完整

**Go** (`base.go:1042-1059`): 只过滤了 6 种大小写变体（user/assistant/User/Assistant/USER/ASSISTANT）
**Python**: 可能使用 `casefold()` 做更全面的过滤

**修复方案**：使用 `strings.ToLower(name)` 做大小写不敏感比较。

---

### M-06 GraphMemory.entityEnrich 阻塞任务改为串行执行丧失并发性

**Python** (`base.py:990-994`): 阻塞实体的 pending merge 是异步等待
**Go** (`base.go:1389-1400`): pending merge 在 entityMerge 中同步调用后直接填充 Result

**修复方案**：对齐 Python 的并发模型，让 blocking merge 在后台异步执行。

---

### M-07 GraphMemory.RelationDedupe 中 TmpBuffer 类型为 `[]any` 而非 `[]string`

**Go** (`base.go:1520`): `len(state.TmpBuffer) > 0` — `TmpBuffer` 类型为 `[]any`
**Python**: 存储的是 `[]string`

**修复方案**：将 `TmpBuffer` 类型改为 `[]string`，或添加注释说明。

---

### M-08 GraphMemory.InvokeLLM 中 buildMessages 方法需确认

**Go** (`base.go:666`): `messages, err := gm.buildMessages(params)` — 需确认此方法存在且逻辑对齐

**修复方案**：确认 `buildMessages` 存在于 `utils.go` 且逻辑正确。

---

### M-09 ApplyDeltaOp 的 section 提取逻辑与 Python rsplit 不一致

**Python** (`update.py:498`): `operation.bullet_id.rsplit('-', 1)` — 从右侧分割
**Go** (`update.go:653`): `strings.Split(*operation.BulletID, "-")` — 从所有 `-` 处分割

对于含多连字符的 section 名（如 `"strategies_and_hard_rules-00001"`），虽然 Go 用 `Join(parts[:len(parts)-1], "-")` 结果巧合一致，但代码意图不清晰。

**修复方案**：使用 `strings.LastIndex` 替代 `strings.Split`：
```go
if idx := strings.LastIndex(*operation.BulletID, "-"); idx >= 0 {
    section = strings.ReplaceAll((*operation.BulletID)[:idx], "_", " ")
}
```

---

### M-10 LoadPlaybookOp 提取 maxID 用 Split 而非 rsplit 等价方法

**Python** (`update.py:73-76`): `bullet_id.rsplit('-', 1)` — 从右侧分割
**Go** (`update.go:209-213`): `strings.Split(bulletID, "-")` — 从所有 `-` 处分割

**修复方案**：使用 `strings.LastIndex` 实现与 Python `rsplit('-', 1)` 等价的拆分。

---

### M-11 CurateOp/ParallelCurateOp 反射 JSON 序列化忽略 error

**Go** (`update.go:439`):
```go
reflectionJSON, _ := json.Marshal(reflection)  // 忽略了 error
```

**修复方案**：检查 error：
```go
reflectionJSON, err := json.Marshal(reflection)
if err != nil {
    return fmt.Errorf("CurateOp: reflection 序列化失败: %w", err)
}
```

---

### M-12 ValidateSearchInput 缺少 settings 类型校验

**Python** (`validate_input.py:76-81`): 校验 `all(isinstance(s, bool) for s in settings)`
**Go** (`validate_input.go:86-109`): 完全跳过了 settings 类型校验

**修复方案**：添加 settings 中每个元素必须是 bool 的校验。

---

### M-13 GraphMemory.AttachEmbedder 返回值与 Python 不一致

**Python**: `attach_embedder()` 不返回值，也不缓存 embedder
**Go**: `AttachEmbedder()` 返回 error 并额外缓存了 `embedderVal`

**修复方案**：保持 Go 当前实现方式，但添加注释说明设计差异原因。

---

### M-14 ValidateAddMemoryInput 中 userID 校验被绕过

**Go** (`base.go:854-858`):
```go
if err := ValidateAddMemoryInput(...); err != nil {
    if !strings.Contains(err.Error(), "user_id") {  // 绕过了 userID 校验错误
        return "", err
    }
}
```

**修复方案**：移除此条件绕过，让 userID 校验错误正常返回。

---

### M-15 ACERecallMemoryOp 中 Search 失败直接返回 error

**Go** (`run.go:70-76`): 搜索失败直接返回 error
**Python**: `LoadPlaybookOp` 搜索失败后回退到空 Playbook

**修复方案**：当前行为差异是 Go 设计决策——搜索失败是致命错误。考虑是否应对齐 Python 的回退策略。

---

### M-16 GraphMemory.parseRelationFilteringResult 中 `id > 0` 检查 Python 没有

**Go** (`base.go:1453`): `if id, ok := toInt(idVal); ok && id > 0 && id <= len(relationList)`
**Python** (`base.py:470-471`): `keep_ids = set(dedupe_entity.get("relevant_relations"))` — 无 `> 0` 检查

**修复方案**：Go 更安全，保持当前实现。

---

### M-17 GraphMemory.TokenRecord 并发安全性

**Go** (`base.go:692-694`): `TokenRecord` 是 `map[string]int`，并发调用 `InvokeLLM` 时没有加锁

**修复方案**：使用 `sync.Mutex` 或 `sync/atomic.Int64` 保护 `TokenRecord`。

---

### M-18 ReflectOp 获取 query 但未使用

**Go** (`update.go:247`): `_, _ = cecontext.GetTyped[string](rc, "query")` — 丢弃了 query 值
**Python** (`update.py:124`): `query = context.query` — 也获取了但未在 reflector prompt 中使用

**修复方案**：保留此行对齐 Python，但添加注释说明 query 在 reflector prompt 中未使用。

---

### M-19 runTrialsInner 缺少 `retrieval_query` 参数

**Python 样例** (`trajectory_generator.py:355-356`):
```python
invoke_inputs: Dict[str, Any] = {"query": current_query}
if self_refine:
    invoke_inputs["retrieval_query"] = question
```

Python 在 self_refine 模式（sequential/combined）下，额外传入 `retrieval_query=question`，确保记忆检索使用原始问题而非精炼后的查询。

**Go 问题** (`internal/agentcore/context_evolver/service/trajectory_generator.go:196`):
```go
result, err := agent.Execute(ctx, currentQuery, sessionID)
// 缺少 retrieval_query 参数
```

Go 的 `AgentFlowService.Execute` 签名只接受 `(ctx, query, sessionID)`，没有 `retrieval_query` 传递机制。

**影响**：self_refine 模式下，记忆检索使用精炼后的查询而非原始问题，可能导致检索到不相关的记忆。

**修复方案**：扩展 `AgentFlowService.Execute` 签名或在 `RunTrialsInput` 中添加 `RetrievalQuery` 字段。

---

### M-20 缺少 `_SELF_DIVERSITY_PROMPT` — combined 模式的多样性提示词

**Python 样例** (`trajectory_generator.py:256-261`):
```python
_SELF_DIVERSITY_PROMPT = (
    "Let's carefully re-examine the previous trajectory, including your reasoning "
    "steps and action taken. The solution might be correct or wrong. Now, solve the "
    "same problem again from scratch using DIFFERENT reasoning approach. "
    "Focus on exploring alternative strategies.\n\n"
)
```

Python 的 `_run_trials_inner` 中，当 `COMBINED_MATTS_PROMPT` 配置为 `"diversity"` 时，使用 `_SELF_DIVERSITY_PROMPT` 替代 `_SELF_REFINE_PROMPT`。

**Go 问题** (`trajectory_generator.go:74-79`):
Go 只定义了 `selfRefinePrompt`，缺少 `selfDiversityPrompt`。`runTrialsInner` 中也没有检查配置选择。

**影响**：Go 不支持 combined 模式的多样性策略变体。

**修复方案**：添加 `selfDiversityPrompt` 常量，在 `runTrialsInner` 中根据配置选择使用哪个提示词。

---

### M-21 ParallelScalingOp 温度修改无效 — 设置 RuntimeContext 而非 LLM 实例

**Python 样例** (`matts.py:53-60, 87-90`):
```python
original_temp = getattr(llm, 'temperature', 0.7)
if hasattr(llm, 'temperature'):
    llm.temperature = self.temperature  # 直接修改 LLM 实例的温度
# ...
if hasattr(llm, 'temperature'):
    llm.temperature = original_temp     # 恢复
```

**Go 问题** (`matts.go:161-162, 204-205`):
```go
originalTemp := rc.Get("llm_temperature")
rc.Set("llm_temperature", o.temperature)
// ...
rc.Set("llm_temperature", originalTemp)
```

Go 设置的是 `RuntimeContext` 中的值，而非 LLM 实例的属性。如果 `AgentFlowService.Execute` 不读取 `RuntimeContext` 的 `llm_temperature`，温度修改无效。

**影响**：并行缩放中的多样性通过温度控制，如果温度修改无效，生成的轨迹多样性不足。

**修复方案**：确认 `AgentFlowService` 是否使用 `RuntimeContext` 中的 `llm_temperature`；如果不使用，需要通过其他机制（如 `WithTemperature` 选项）传递温度给 LLM 调用。

---

### M-22 WorktreeRail.BeforeInvoke 恢复 CWD 时未对 stored 进行类型校验

**Python 样例** (`openjiuwen/harness/tools/worktree/rails.py:184-186`):
```python
if isinstance(stored, dict):
    stored = WorktreeSession.model_validate(stored)
# 也处理已经是 WorktreeSession 实例的情况
```

**Go 问题** (`internal/agentcore/harness/tools/worktree/rails.go:282-299`):
Go 只处理 `map[string]any` 类型，不处理已序列化的 `WorktreeSession` 类型。

**影响**：非预期类型可能导致 panic 或数据丢失。

**修复方案**：添加 `case *WorktreeSession:` 分支，直接使用；对齐 Python 的 defensive fallback。

---

### M-23 WorktreeRail.AfterInvoke 未恢复 CWD 到原始位置

**Go 问题** (`internal/agentcore/harness/tools/worktree/rails.go:315-342`):
Go 的 `BeforeInvoke` 在恢复 worktree 时设置了 CWD，如果 `AfterInvoke` 时 worktree 已退出（`current == nil`），CWD 仍指向 worktree 路径。

Python 不需要，因为 Python 的 CWD 是 ContextVar 自动恢复的。

**修复方案**：在 `AfterInvoke` 中，当 `current == nil` 时，恢复 CWD 到 session 的 OriginalCWD。

---

### M-24 DeepAdapter.updateRuntimeConfig 未被 CreateInstance 调用

**Go 问题** (`internal/swarm/server/adapter/deep_adapter_config.go:82-141`):
`updateRuntimeConfig()` 完整实现但搜索整个代码库没有找到调用点。

**影响**：Language/Channel/Mode 等运行时配置永远不会更新，RuntimePromptRail 注入的 section 将始终使用默认值。

**修复方案**：在 DeepAdapter 的 CreateInstance 或 handleCommand 方法中，在 seedRuntimeCwd 之后调用 updateRuntimeConfig。

---

### M-25 CreateWorktreeManager 中 eventHandler 在 TeamBackend 尚未初始化时为 nil

**Go 问题** (`internal/agent_teams/agent/agent_configurator.go:217-220`):
```go
// 步骤 9: 工作树管理器（在步骤 8 之前创建）
if ctx.Role != atschema.TeamRoleLeader {
    wtMgr := c.CreateWorktreeManager(spec)
    c.SetWorktreeManager(wtMgr)
}
// 步骤 8: 团队后端（标记为 TODO，尚未实现）
// TODO(#9.58): 设置团队后端
```

`CreateWorktreeManager` 在 L438 检查 `c.TeamBackend() != nil`，但步骤 8（TeamBackend 设置）标记为 TODO 未实现。因此 `c.TeamBackend()` 始终为 nil，eventHandler 永远不会创建。

**影响**：worktree 事件不会桥接到 team_events，团队成员无法感知 worktree 变化。

**修复方案**：调整步骤 8 和 9 的顺序，或在 `SetupTeamBackend` 完成后回填 WorktreeManager 的 eventHandler。

---

### M-26 SummarizeMemoryOp 中 label 值未写回 RuntimeContext

**Go 问题** (`internal/agentcore/context_evolver/summary/task/rb/update.go:111-124`):
```go
var label bool
labelVal, ok := cecontext.GetTyped[[]bool](rc, "label")
if ok && len(labelVal) > 0 {
    label = labelVal[0]
} else {
    det := NewLabelDeterminator(o.ServiceContext())
    determinedLabel, err := det.DetermineLabel(ctx, query, trajectories[0])
    // ... label 已判定但未写回 rc
}
```

Python 中 `context.label` 是在 summarize 阶段设置的，后续 ops（如 PersistMemoryOp）可能依赖此值。Go 中判定后的 label 只在本地使用，未写回 RuntimeContext。

**修复方案**：在 label 判定后，写回 `rc.Set("label", []bool{label})`。

---

## 提示问题 (T)

### T-01 GraphMemory.AddMemory 步骤间缺少日志

Python 在每个关键步骤之间有 debug/info 级别日志，Go 的 `AddMemory` 方法中步骤间缺少日志。

**修复方案**：在 AddMemory 的每个步骤后添加 Debug 日志。

---

### T-02 GraphMemory.InvokeLLM 中 retry sleep 阻塞 goroutine

**Go** (`base.go:725`): `time.Sleep(...)` — 持有信号量期间阻塞
**Python**: `await asyncio.sleep(...)` — 异步非阻塞

**修复方案**：考虑使用 `select` + `time.After` 实现可取消的等待。

---

### T-03 Playbook.Dumps 缺少 indent=2 格式化

**Python**: `json.dumps(..., ensure_ascii=False, indent=2)`
**Go**: `json.Marshal(p.ToDict())` — 无缩进

**修复方案**：改为 `json.MarshalIndent(p.ToDict(), "", "  ")`。

---

### T-04 PlaybookLoads 类型校验

**Python**: `if not isinstance(payload, dict): raise ValueError(...)`
**Go**: `json.Unmarshal` 到 `map[string]any` 天然只接受 JSON 对象

**修复方案**：行为等效，添加注释说明。

---

### T-05 ACEPrompt 使用 Go text/template 替代 Python format

Go 使用 `{{.Field}}` 替代 Python 的 `{field}`，JSON 示例中的花括号用 `{{"{{"}}` 转义。功能等价。

**修复方案**：无需修改，已正确适配。

---

### T-06 ACEReflectorScalingPrompt 包含 Python 原文拼写错误 "executionfeedback"

Python 原文有 typo（缺空格），Go 一比一复刻。按项目"一比一复刻"原则保留。

**修复方案**：添加注释标注此 typo 源自 Python。

---

### T-07 GraphMemory.Search 中 performSearch 使用闭包访问 searchStrategies 无锁保护

如果 `RegisterSearchStrategy` 在搜索期间被调用，可能导致 data race。

**修复方案**：在 `performSearch` 开始时复制 strategy 引用，或使用 `sync.RWMutex`。

---

### T-08 ReflectOp.Playbook 类型断言可能失败

**Go** (`update.go:249`): `playbook, _ := rc.Get("playbook").(*Playbook)` — 类型断言失败时无日志

**修复方案**：添加错误日志记录类型断言失败的情况。

---

### T-09 PersistMemoryOp.helper 可能为 nil 时静默跳过

**Go** (`update.go:796`): `if o.helper != nil` — helper 为 nil 时持久化被静默跳过

**修复方案**：如果 helper 为 nil，记录一条 Warn 日志。

---

### T-10 RuntimePromptRail 缺少 timezone_offset 参数

**Python** (`runtime_prompt_rail.py:33`): 构造函数有 `timezone_offset: int = 8`
**Go** (`runtime_prompt_rail.go:109`): `NewRuntimePromptRail(language, channel string)` — 缺少此参数

**修复方案**：评估是否需要保留 `timezoneOffset` 参数。如不需要，添加注释说明。

---

### T-11 RuntimePromptRail.before_model_call 中同步 I/O

**Python**: `async def before_model_call` — 异步方法
**Go**: `func BeforeModelCall` — 同步方法，`readRuntimeStateYAML()` 和 `runGit` 都是同步 I/O

**修复方案**：Go 中 goroutine 阻塞不影响其他 goroutine，优先级较低。

---

### T-12 Playbook.as_prompt 排序行为

Python 使用 `sorted()` 字典序，Go 使用 `sort.Strings` 字典序。行为一致。

**修复方案**：无需修改。

---

### T-13 GraphMemory.lastGC 初始化

Go: `float64(time.Now().Unix())` | Python: `time.time()`
两者都是 wall clock，行为一致。

**修复方案**：无需修改。

---

### T-14 WorktreeLifecycleRail 空方法实现缺少分隔注释

**Go 问题** (`internal/agentcore/harness/tools/worktree/rails.go:379-434`):
`AutoSetupRail` 和 `DiffSummaryRail` 的空方法实现没有放在声明顺序规范的位置，缺少分隔注释。

**修复方案**：在空方法实现区域添加分隔注释标注。

---

### T-15 AutoSetupRail.AfterWorktreeCreate 缺少 stderr 捕获

**Python 样例** (`openjiuwen/harness/tools/worktree/rails.py:421-429`):
```python
# 捕获 stderr，失败时记录到日志
logger.warning("Setup command '%s' failed: %s", cmd, stderr.decode())
```

**Go 问题** (`internal/agentcore/harness/tools/worktree/rails.go:352-363`):
Go 将 `cmdObj.Stderr = nil`，不捕获 stderr，且日志中只有 cmd 没有 stderr 信息。

**修复方案**：将 `cmdObj.Stderr` 设为 `&bytes.Buffer{}`，在失败日志中追加 stderr 内容。

---

### T-16 WorktreeCreatedEvent/WorktreeRemovedEvent 缺少 OwnerID 和 Tag 字段

**Go 问题** (`internal/agent_teams/schema/events/worktree_events.go:7-25`):
`WorktreeCreatedEvent` 和 `WorktreeRemovedEvent` 缺少 `OwnerID` 和 `Tag` 字段，而 Python 和 Go 的 harness 层事件都有这些字段。

**修复方案**：在两个事件结构体中添加 OwnerID 和 Tag 字段，同步更新 ToPayload 方法。

---

### T-17 ReasoningBank 中 messages_to_text 辅助函数缺失

**Python** 中 `format_trajectory` 是独立函数（`trajectory_generator.py:109-158`），Go 中已实现为 `formatTrajectory`（`trajectory_generator.go:126-163`）。但 Python 中 `format_trajectory` 在 `matts.py` 的 `SelfContrastMemoryOp` 内也有内联使用的变体（如 `messages_to_text`），Go 缺少此类辅助。

**修复方案**：当前 `formatTrajectory` 已覆盖主要场景，此为提示级问题。如有需要，可添加 `MessagesToText` 辅助函数。

---

## 待回填占位检查

| 占位标记 | 位置 | 状态 | 说明 |
|---------|------|------|------|
| ⤵️ StreamEventRail | `10.6.3-10` | 未实现 | Python 914 行完整 rail，返回 nil |
| ⤵️ SkillCreateRail | `10.6.3-10` | 未实现 | `buildSkillCreateRail` 返回 nil |
| ⤵️ ExternalMemoryRail | `10.6.3-10` | 未实现 | `buildExternalMemoryRail` 返回 nil |
| ⤵️ SummarizeTrajectories | `9.82 P4` | stub | 返回 "not implemented"，依赖 P6 TaskMemoryService |
| ⤵️ TeamBackend (SetupInfra step 8) | `agent_configurator.go:213` | TODO | TeamBackend 未实现导致 WorktreeManager eventHandler 为 nil |
| ⤵️ ActorManager 返回类型 | `5.2/5.3` | 未实现 | `BaseSession.ActorManager()` 返回类型待后续回填 |
| ⤵️ Team后续请求绕过/TeamManager调用 | `10.3.2` | 未实现 | JiuWenClaw 中 Team 相关逻辑待 10.6.19-23 |
| ⤵️ UserConfig.is_sensitive() | `2.16` | 未实现 | 敏感信息过滤待 common/security/UserConfig 迁移 |
| ⤵️ UrlUtils SSRF 防护 | `3.8` | 未实现 | RestfulApi 的 SSRF 防护待 common/security 迁移后回填 |

---

## 重点检查结论

### 1. 方法签名和步骤完整性

| 模块 | 对齐程度 | 说明 |
|------|---------|------|
| GraphMemory.AddMemory | 75% | 缺少嵌入异步调度、`created_at`/`user_id` 传递、refresh 语义区分 |
| GraphMemory.Search | 90% | 结果图对象字段不完整，但核心搜索逻辑对齐 |
| Graph Extraction | 95% | 基本对齐 |
| ACE Playbook | 95% | ApplyMetadata 语义错误、SafeJSONLoads 正则缺陷 |
| ACE Summary Ops | 93% | LoadPlaybookOp recover 不规范、ACERecallMemoryOp 返回类型不对 |
| RuntimePromptRail | 95% | 7 个 section 全部实现，缺少 timezone_offset |
| ResponsePromptRail | 100% | 完全对齐 |
| StreamEventRail | 0% | **完全未实现**，返回 nil，级联影响 6 个 S 级问题 |
| ReasoningBank MaTTS | 85% | 核心 ops 已实现，缺少 fallback、source_type、diversity prompt |
| ReasoningBank Trajectory | 70% | SummarizeTrajectories 为 stub，缺少 retrieval_query |
| WorktreeRail | 90% | 缺少 InitWorktreeSessionState 调用、lifecycle fire 未接入 |
| DeepAdapter | 95% | updateRuntimeConfig 未调用、TeamBackend 依赖顺序错误 |

### 2. 占位代码检查

所有 ⤵️ 标记的代码确实尚未实现，主要集中在：
- StreamEventRail / SkillCreateRail / ExternalMemoryRail（依赖 10.6.3-10）
- SummarizeTrajectories（依赖 P6 TaskMemoryService）
- TeamBackend（依赖 9.58）
- 敏感信息过滤（依赖 common/security/UserConfig）
- SSRF 防护（依赖 common/security）

### 3. 优先修复建议

| 优先级 | 问题编号 | 描述 |
|--------|---------|------|
| **P0** | S-10 | `updateEntitiesForRelationRemoval` 将关系 UUID 当作实体 UUID（严重逻辑错误）|
| **P0** | S-01 | `ParseAllRelations` 缺少 `created_at`/`user_id` |
| **P0** | S-05 | 搜索结果图对象字段不完整 |
| **P0** | S-12 | `ApplyMetadata` 应为直接赋值而非增量累加 |
| **P0** | S-13/S-14 | SafeJSONLoads 正则缺少 DOTALL + 非贪婪导致嵌套 JSON 失败 |
| **P0** | S-11 | `handleRelationDedupe` 遍历中修改切片导致索引偏移 |
| **P0** | S-20 | StreamEventRail 完全未实现（914 行 Python rail）|
| **P0** | S-27 | ReasoningBankMemory 缺少 source_type/helpful_count/harmful_count |
| **P1** | S-02/S-03 | 嵌入任务异步调度和 state.tasks 管理 |
| **P1** | S-04 | refresh 语义区分和 GC 逻辑 |
| **P1** | S-06 | `resolveEachRelation` 使用 context.Background() |
| **P1** | S-07/S-08 | relationDedupe 中 lhs/rhs 处理 |
| **P1** | S-09 | entityMerge 排除 Entity 导致摘要不更新 |
| **P1** | S-15 | ACERecallMemoryOp 缺少 ACERetrievedMemory 转换 |
| **P1** | S-18 | RegisterSearchStrategy Entity 默认 MinScore 不一致 |
| **P1** | S-26 | BestOfNOp/SelfContrastMemoryOp 缺少 try/catch fallback |
| **P1** | S-28 | WorktreeManager.Enter/Exit 未调用 fire lifecycle hooks |
| **P1** | S-21/S-22/S-23/S-24 | StreamEventRail 级联问题（随 S-20 一起修复）|
| **P1** | S-25 | SummarizeTrajectories 为 stub（依赖 P6）|
| **P2** | M-02 | metricIsSim 硬编码 false |
| **P2** | M-09/M-10 | rsplit vs Split 不一致 |
| **P2** | M-06 | entityEnrich 串行执行丧失并发性 |
| **P2** | M-11 | json.Marshal 忽略 error |
| **P2** | M-17 | TokenRecord 并发安全性 |
| **P2** | M-19 | runTrialsInner 缺少 retrieval_query |
| **P2** | M-21 | ParallelScalingOp 温度修改无效 |
| **P2** | M-24 | updateRuntimeConfig 未被调用 |
| **P2** | M-25 | TeamBackend 未实现导致 eventHandler 为 nil |

### 4. 审查确认的正确修复

以下之前审查发现的问题已确认在当前代码中正确修复：

1. **graph_memory userID any → []string**: `ValidateSearchInput` 正确接受 `[]string`，`normalizeUserIDs` 已删除
2. **FormatNewEntities 删除**: 导出的 `FormatNewEntities` 已删除，非导出的 `formatNewEntities` 保留了对齐逻辑
3. **extractRefDict 只保留 properties 子集**: 正确只提取 `defMap["properties"]`
4. **initState 使用 BuildResponseFormat**: `newGraphMemPrompting()` 和 `initState` 都使用 `BuildResponseFormat`
5. **RuntimePromptRail 3 个差异修复**: Init/Uninit 补全了 7 个 section、BeforeModelCall 读取 YAML 一次
6. **context_evolver 本地接口**: LLMService/EmbeddingService/VectorStoreService 都是本地接口，ServiceContext 不加锁标注
7. **DeepAdapter 配置修复**: `deep_adapter_config.go` 结构和逻辑完整，`updateRuntimeConfig` 实现对齐 Python
8. **PublishEvent 导出**: `TeamBackend.PublishEvent` 已正确导出，worktree 事件桥接在 `agent_configurator.go` 中正确实现
