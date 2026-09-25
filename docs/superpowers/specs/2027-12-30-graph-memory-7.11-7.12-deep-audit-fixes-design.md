# 7.11+7.12 Python 对齐度深度审查修复设计

> 基于逐文件对比 Python 源码与 Go 实现，发现 22 个严重、26 个中等、28 个低严重度差异。
> 本文档记录需修复的 30 项确认修复方案，18 项跳过（功能等价/Go 更正确），4 项已被其他修复间接覆盖。

## 1. 修复清单总览

### 1.1 严重问题（10 项）

| # | 问题 | 修复方案 |
|---|------|---------|
| S1 | asyncTask 缺少同步等待机制 | channel 化：`type asyncTask chan asyncResult` + `Wait()` |
| S2 | metricIsSim 硬编码 false | BaseGraphStore 接口加 `ReturnSimilarityScore() bool`，构造时从 dbBackend 获取 |
| S3 | updateEntitiesForRelationRemoval 用关系 UUID 替代 LHS/RHS | `ToRemove` 改为 `map[string]*graph.Relation`，从 LHS/RHS 取端点 UUID |
| S4 | ResolveEntities 丢弃 Entity 元素 | 新增 `EntityOrDeclaration` 联合结构体 |
| S5 | Dict2Relation 缺 created_at/user_id | 签名加 `createdAt int64, userID string` |
| S6 | Relation.LHS/RHS 是 UUID 无法引用实体 | 改为 `*Entity`，`json:"-"`，ToMap 输出 UUID |
| S7 | multilingualResponseFormat 缺 StrictSchemaEnforce | 函数内加调用 + StrictSchemaEnforce 去掉 `len(props)>0` 条件 |
| S8 | EntityDefAttr + EntityDef.Attributes 缺失 | 新增 EntityDefAttr 结构体，EntityDef 加 Attributes 字段 |
| S9 | FormatExistingEntities 硬编码 3 字段 | 遍历 ent map 所有 key 动态 ReplaceAll |
| S10 | 并行调度退化为串行 | goroutine 并发 + 统一 Wait()，对齐 Python asyncio 并行语义 |

### 1.2 中等问题（14 项需修复，4 项已覆盖，4 项跳过）

| # | 问题 | 修复方案 |
|---|------|---------|
| M1 | readable_schema 缺删除 title/required + $ref 内联 | 实现 `recursiveReplace` 通用 BFS 函数 |
| M2 | StrictSchemaEnforce 空 properties 条件 | 已在 S7 一并修复 |
| M3 | FormatSchemaInfo refDict 两层嵌套限制 | 改为 `map[string]any` |
| M4 | entityEnrich tasks/entities 对齐方式 | 用 zip 语义取 min |
| M5 | updateEntitiesForRelationRemoval 名字匹配 | 已被 S3+S6 间接解决 |
| M6 | InvokeLLM 重试 sleep 持有信号量 | 先释放信号量再 sleep，重试时重新获取 |
| M7 | extractEntityDeclarations name 只读不移除 | 读取后 `delete(entMap, "name")` |
| M8 | ParseISO 无时区时追加 Z | 不追加 Z，用 `ParseInLocation` 本地时区解析 |
| M9 | EnsureValidLanguage 返回普通 error | 改用 `exception.New` 带 StatusCode |
| M10 | registerInBulk 空文件不返回 error | 返回带 StatusCode 的异常 |
| M11 | TemplateManager RWMutex 不可重入 | 加注释标注不可重入约束 |
| M12 | tool 角色 ToolMessage 空 name | case "tool" 返回 nil 跳过 |
| M13 | InvokeLLM 缺 cause 参数 | 加 `exception.WithCause(lastErr)` |
| M14 | formatReadableSchema 多 class Output 包裹 | 删除 class Output 包裹行 |
| — | extractRefDictRecursive 不搜索顶层 $defs | 已被 M1 recursiveReplace 覆盖 |

跳过的中等问题：summary_target 格式化（实际一致）、RelationDef.LHS/RHS type→pointer（功能等价）、dedupe_relation_list 参数简化（接口更清晰）、JSONLike 类型约束（不影响运行时）。

### 1.3 低严重度问题（3 项需修复，25 项跳过）

| # | 问题 | 修复方案 |
|---|------|---------|
| L1 | replacePlaceholders 全局替换可能二次替换 | 正则提取占位符 + langMap 精确查找 |
| L2 | registerInBulk 锁范围+日志时序 | 文件扫描移入锁内，日志移到锁外 |
| L3 | pr_parser.go 第 53-78 行死代码 | 删除无用 for 循环 |

## 2. 严重问题详细设计

### 2.1 S1：asyncTask channel 化

**问题**：Go 的 `asyncTask` 是简单 struct，goroutine 写入 `Result` 后无同步屏障，主流程读取时可能为零值。影响整个 AddMemory 管线中所有 LLM 异步调用。

**Python 行为**：`asyncio.create_task` 返回 Future，`await future` 保证结果就绪。

**修复方案**：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// asyncResult 异步 LLM 调用结果
type asyncResult struct {
    // Content LLM 响应内容
    Content string
    // Err 调用错误
    Err error
}

// asyncTask 异步 LLM 调用任务，通过 channel 传递结果
type asyncTask chan asyncResult

// ──────────────────────────── 导出函数 ────────────────────────────

// Wait 阻塞等待异步任务完成，返回结果（对齐 Python await future）
func (t asyncTask) Wait() (string, error) {
    result := <-t
    return result.Content, result.Err
}
```

**启动端改动**：

```go
func (gm *GraphMemory) invokeLLMAsync(...) asyncTask {
    ch := make(asyncTask, 1)  // 带缓冲，goroutine 写入后不阻塞
    go func() {
        resp, err := gm.InvokeLLM(ctx, ...)
        ch <- asyncResult{Content: resp, Err: err}
    }()
    return ch
}
```

**消费端改动**：

```go
// 对齐 Python: await future
content, err := state.Tasks[0].Wait()
```

**影响文件**：`states.go`（类型定义）、`base.go`（所有 invokeLLMAsync 和 state.Tasks 消费处）

### 2.2 S2：metricIsSim 从 dbBackend 初始化

**问题**：`metricIsSim` 硬编码 `false`，Python 从 `db_backend.return_similarity_score` 获取。Milvus 后端返回 `True`（余弦相似度越大越相似），Go 永远 `false`，搜索排序方向可能反转。

**修复方案**：

1. `BaseGraphStore` 接口新增方法：
```go
// ReturnSimilarityScore 返回搜索分数是否为相似度（越大越相似）
// 对齐 Python: GraphStore.return_similarity_score
ReturnSimilarityScore() bool
```

2. Milvus 实现：`return true`（对齐 Python `MilvusSupport.return_similarity_score -> Literal[True]`）

3. 其他后端：根据 `DistanceMetric` 推导（cosine/dot → true，euclidean → false）

4. GraphMemory 构造函数：
```go
// 当前：
metricIsSim: false, // 默认值，后续从 dbBackend 获取

// 修复后：
metricIsSim: dbBackend.ReturnSimilarityScore(),
```

**影响文件**：`foundation/store/graph/base.go`（接口）、各后端实现、`graph_memory/base.go`（构造函数）

### 2.3 S3：ToRemove 类型变更

**问题**：Python 的 `state.to_remove` 存储 Relation 对象（含 LHS/RHS），Go 只存 UUID 字符串。`updateEntitiesForRelationRemoval` 用关系自身 UUID 替代 LHS/RHS 端点 UUID，导致错误实体被清理。

**修复方案**：

```go
// 当前：
ToRemove map[string]struct{}  // 只存 UUID

// 修复后（对齐 Python state.to_remove: set）：
ToRemove map[string]*graph.Relation  // key=UUID, value=Relation 对象
```

所有写入 `ToRemove` 的地方改为 `ToRemove[uuid] = relation`。

`updateEntitiesForRelationRemoval` 修复：
```go
// 当前（错误）：
for _, item := range state.ToRemove {
    entitiesToRemoveRelationsFrom[item.UUID] = struct{}{}
}

// 修复后（对齐 Python）：
for _, relation := range state.ToRemove {
    if relation.LHS != nil {
        entitiesToRemoveRelationsFrom[relation.LHS.UUID] = struct{}{}
    }
    if relation.RHS != nil {
        entitiesToRemoveRelationsFrom[relation.RHS.UUID] = struct{}{}
    }
}
```

**影响文件**：`states.go`（类型定义）、`base.go`（所有读写 ToRemove 处）

### 2.4 S4：EntityOrDeclaration 联合结构体

**问题**：Python 的 `resolve_entities` 返回 `list[Union[EntityDeclaration, Entity]]`，Go 丢弃了 Entity 类型元素，导致后续关系索引错位。

**修复方案**：

```go
// EntityOrDeclaration 实体声明或已存在实体的联合类型
// 对齐 Python: Union[EntityDeclaration, Entity]
type EntityOrDeclaration struct {
    // Decl 新抽取的实体声明（非 nil 时 Entity 为 nil）
    Decl *extraction.EntityDeclaration
    // Entity 已存在的实体对象（非 nil 时 Decl 为 nil）
    Entity *graph.Entity
}

// IsEntity 是否为已存在实体
func (e EntityOrDeclaration) IsEntity() bool {
    return e.Entity != nil
}

// Name 获取实体名称
func (e EntityOrDeclaration) Name() string { ... }

// EntityTypeID 获取实体类型 ID
func (e EntityOrDeclaration) EntityTypeID() int { ... }
```

**影响文件**：`parse_llm_response.go`（ResolveEntities/DeclareEntities/Dict2Relation/ParseAllRelations 返回值）、`base.go`（entityMerge 消费处）

### 2.5 S5：Dict2Relation 补齐 created_at/user_id

**问题**：Python 通过 `**kwargs` 传递 `created_at` 和 `user_id` 给 Relation 构造函数，Go 缺失。

**修复方案**：

```go
// 当前：
func Dict2Relation(response map[string]any, entities []EntityOrDeclaration) *graph.Relation

// 修复后（对齐 Python **kwargs）：
func Dict2Relation(response map[string]any, entities []EntityOrDeclaration, createdAt int64, userID string) *graph.Relation
```

`ParseAllRelations` 同步加参数，调用方传入 `state.CurrentTimestamp` 和 `userID`。

**影响文件**：`parse_llm_response.go`、`base.go`

### 2.6 S6：Relation.LHS/RHS 改为 *Entity

**问题**：Python `Relation.lhs: BaseGraphObject | str` 是联合类型，可直接通过对象引用访问实体内容。Go 只有 UUID 字符串，每次访问实体都需要查找。

**修复方案**：

```go
// 当前：
type Relation struct {
    NamedGraphObject
    // ...
    LHS string `json:"lhs"`   // UUID 字符串
    RHS string `json:"rhs"`   // UUID 字符串
}

// 修复后（对齐 Python: lhs: BaseGraphObject | str）：
type Relation struct {
    NamedGraphObject
    // ...
    // LHS 左侧实体（对齐 Python: lhs: BaseGraphObject | str）
    LHS *Entity `json:"-"`  // json:"-" 不直接序列化
    // RHS 右侧实体
    RHS *Entity `json:"-"`
}
```

**序列化适配**（ToMap）：
```go
func (r *Relation) ToMap() map[string]any {
    m := r.NamedGraphObject.ToMap()
    if r.LHS != nil {
        m["lhs"] = r.LHS.UUID
    } else {
        m["lhs"] = ""
    }
    if r.RHS != nil {
        m["rhs"] = r.RHS.UUID
    } else {
        m["rhs"] = ""
    }
    // ...
}
```

**反序列化适配**（FromMap）：只填充 UUID，构造 `*Entity` 空壳。

**辅助方法**：
```go
func (r *Relation) LHSUUID() string {
    if r.LHS != nil { return r.LHS.UUID }
    return ""
}
func (r *Relation) RHSUUID() string {
    if r.RHS != nil { return r.RHS.UUID }
    return ""
}
```

**连带解决**：
- D43（classify_relations_extracted 自指向关系）：直接 `relation.LHS.Content` 访问，无需查找
- D11（updateEntitiesForRelationRemoval）：直接 `relation.LHS.UUID` 取端点

**影响文件**：`foundation/store/graph/graph_object.go`、`graph/milvus/`（反序列化）、`graph_memory/`（所有访问 LHS/RHS 处）

### 2.7 S7：multilingualResponseFormat 加 StrictSchemaEnforce

**问题**：所有 8 个提示词组装函数调用 `multilingualResponseFormat`，但该函数没有调用 `StrictSchemaEnforce`。`BuildResponseFormat` 有调用，但提示词流程不走那条路径。

**修复方案**：

```go
// multilingualResponseFormat 中加调用：
func multilingualResponseFormat(modelType reflect.Type, language string) map[string]any {
    replaced := ReplaceDescriptions(params, langMap)
    schemaMap := commonschema.ToJSONSchemaMap(replaced)
    StrictSchemaEnforce(schemaMap)  // 新增：对齐 Python strict=True
    return ResponseFormat(modelType.Name(), schemaMap)
}
```

**StrictSchemaEnforce 修复空 properties 条件**：
```go
// 当前：
if props, ok := node["properties"].(map[string]any); ok && len(props) > 0 {

// 修复后（对齐 Python：isinstance(property_field, dict) 即设置）：
if props, ok := node["properties"].(map[string]any); ok {
```

**影响文件**：`extraction/base.go`

### 2.8 S8：EntityDefAttr + EntityDef.Attributes

**问题**：Python `EntityDefAttr` 含 `content: str` 字段，`EntityDef` 有 `attributes` 字段。Go 完全缺失。

**修复方案**：

```go
// registry/entity_type_definition.go 中新增：

// EntityDefAttr 实体定义属性模板
// 对齐 Python: EntityDefAttr(MultilingualBaseModel)
type EntityDefAttr struct {
    // Content 实体摘要模板（默认空字符串）
    // 对齐 Python: content: str = Field(default="", description="{{[ent_summary]}}")
    Content string
}

// DefaultEntityDefAttr 默认实体属性模板实例
var DefaultEntityDefAttr = &EntityDefAttr{Content: ""}

// EntityDef 增加 Attributes 字段：
type EntityDef struct {
    Name        string
    Description map[string]string
    // Attributes 实体属性模板
    // 对齐 Python: attributes: MultilingualBaseModel = Field(default_factory=EntityDefAttr)
    Attributes *EntityDefAttr
}

// DefaultEntity/HumanEntity/AIEntity 设置默认 Attributes
var DefaultEntity = &EntityDef{
    Name:        "Entity",
    Description: EntityDefinitionDescription,
    Attributes:  DefaultEntityDefAttr,
}
```

`entity_type_definition.go` 的 re-export 同步加上 `EntityDefAttr`。

**影响文件**：`extraction/registry/entity_type_definition.go`、`extraction/entity_type_definition.go`（re-export）

### 2.9 S9：FormatExistingEntities 动态替换

**问题**：Python 用 `template.format(i=i, **ent)` 展开 entity dict 所有字段。Go 硬编码只替换 `{name}/{content}`。

**修复方案**：

```go
// 当前：
line := strings.ReplaceAll(tmpl, "{i}", fmt.Sprintf("%d", startIdx+i))
line = strings.ReplaceAll(line, "{name}", fmt.Sprintf("%v", ent["name"]))
line = strings.ReplaceAll(line, "{content}", fmt.Sprintf("%v", ent["content"]))

// 修复后（对齐 Python template.format(i=i, **ent)）：
line := strings.ReplaceAll(tmpl, "{i}", fmt.Sprintf("%d", startIdx+i))
for key, val := range ent {
    line = strings.ReplaceAll(line, "{"+key+"}", fmt.Sprintf("%v", val))
}
```

**影响文件**：`extraction/prompts/entity_extraction/base.go`

### 2.10 S10：对齐 Python 并行调度

**问题**：Python 中实体嵌入与关系抽取并发、entityMerge 阻塞/非阻塞任务并发。Go 全部串行。

**修复方案**（基于 S1 channel 化后）：

**extractEntityDeclarations** — 嵌入放入 goroutine+channel 并与关系抽取并发：
```go
embedTask := make(asyncTask, 1)
go func() {
    embedded, err := gm.Embedder.EmbedDocuments(ctx, names)
    embedTask <- asyncResult{Content: "", Err: err}
}()
state.Tasks = append(state.Tasks, embedTask)
```

**fetchRelevantEntities** — 先 Wait() 等嵌入结果：
```go
if len(state.Tasks) >= 2 {
    embedResult := state.Tasks[len(state.Tasks)-2]
    embedResult.Wait()
}
```

**entityMerge** — 阻塞/非阻塞任务并发发起，最后统一 Wait()。

**影响文件**：`base.go`

## 3. 中等问题详细设计

### 3.1 M1：recursiveReplace 通用 BFS 函数

**问题**：Python 的 `_recursive_replace` 承担 3 个职责（替换 description、删除 title/required、内联 $ref→type），Go 缺少此通用函数。

**修复方案**：

```go
// recursiveReplace 递归替换 JSON Schema map 中的字段
// 对齐 Python: _recursive_replace(to_search, lookup, from_key, to_key=None)
//
// 行为：
//   - BFS 遍历 map[string]any 和 []any
//   - 在每个 map 中查找 from_key 对应的值 descKey
//   - 用 descKey 在 lookup 中查找替换值
//   - 如果 toKey != ""：设置 current[toKey] = lookup[descKey]（或 descKey 本身）
//   - 如果 toKey == ""：删除 current[from_key]
//   - 返回是否执行了替换
func recursiveReplace(data any, lookup map[string]string, fromKey string, toKey string) bool {
    replaced := false
    switch d := data.(type) {
    case map[string]any:
        // 先处理当前层
        if val, exists := d[fromKey]; exists {
            if toKey == "" {
                // 删除模式
                delete(d, fromKey)
                replaced = true
            } else {
                // 替换模式
                descKey, ok := val.(string)
                if ok && lookup != nil {
                    if replacement, found := lookup[descKey]; found {
                        d[toKey] = replacement
                    } else {
                        d[toKey] = descKey
                    }
                } else if ok {
                    d[toKey] = descKey
                }
                if fromKey != toKey {
                    delete(d, fromKey)
                }
                replaced = true
            }
        }
        // 递归处理子节点
        for _, v := range d {
            if recursiveReplace(v, lookup, fromKey, toKey) {
                replaced = true
            }
        }
    case []any:
        for _, v := range d {
            if recursiveReplace(v, lookup, fromKey, toKey) {
                replaced = true
            }
        }
    }
    return replaced
}
```

**ReadableSchema 中使用**（对齐 Python readable_schema）：
```go
// 1. 替换 description — 已有 replacePlaceholders 处理
// 2. 删除 title
recursiveReplace(schemaMap, nil, "title", "")
// 3. 删除 required
recursiveReplace(schemaMap, nil, "required", "")
// 4. $ref → type 内联
if defs, ok := schemaMap["$defs"].(map[string]any); ok {
    refLookup := make(map[string]string)
    for key := range defs {
        refLookup["#/$defs/"+key] = key
    }
    recursiveReplace(schemaMap, refLookup, "$ref", "type")
    delete(schemaMap, "$defs")
}
```

**影响文件**：`extraction/base.go`

### 3.2 M3：refDict 类型改为 map[string]any

**修复方案**：

```go
// 当前：
func FormatSchemaInfo(outStr string, refDict map[string]map[string]any, ...) string
func ReadableSchema(...) (string, map[string]map[string]any)

// 修复后：
func FormatSchemaInfo(outStr string, refDict map[string]any, ...) string
func ReadableSchema(...) (string, map[string]any)
```

`FormatSchemaInfo` 内部 `json.MarshalIndent` 对 `any` 递归序列化，无需改动。

**影响文件**：`extraction/base.go`、`extraction/prompts/entity_extraction/base.go`

### 3.3 M4：entityEnrich zip 语义

```go
// 对齐 Python: for entity, future in zip(entities, state.tasks)
n := len(entities)
if len(state.Tasks) < n {
    n = len(state.Tasks)
}
for i := 0; i < n; i++ {
    content, err := state.Tasks[i].Wait()
    if err != nil { continue }
    UpdateEntity(entities[i], content, ...)
}
```

**影响文件**：`base.go`

### 3.4 M6：InvokeLLM 重试释放信号量

```go
// 当前（信号量内 sleep）：
gm.Semaphore <- struct{}{}
defer func() { <-gm.Semaphore }()
for retries > 0 {
    resp, err := gm.LLMClient.Invoke(ctx, ...)
    if err == nil { return resp, nil }
    time.Sleep(...)
    retries--
}

// 修复后（对齐 Python：sleep 期间释放信号量）：
var lastErr error
for retries > 0 {
    gm.Semaphore <- struct{}{}  // 获取信号量
    resp, err := gm.LLMClient.Invoke(ctx, ...)
    <-gm.Semaphore             // 立即释放信号量
    if err == nil { return resp, nil }
    lastErr = err
    retries--
    if retries > 0 {
        time.Sleep(time.Duration(rand.Float64()*500) * time.Millisecond)
    }
}
return "", lastErr
```

**影响文件**：`base.go`

### 3.5 M7：name pop 对齐

```go
name, _ := entMap["name"].(string)
delete(entMap, "name")  // 对齐 Python: extraction.pop("name", "")
```

**影响文件**：`base.go`（extractEntityDeclarations）

### 3.6 M8：ParseISO 本地时区

```go
// 当前：
if !hasOffset {
    isoStr += "Z"  // 强制 UTC
}
t, err := time.Parse("2006-01-02T15:04:05Z07:00", isoStr)

// 修复后（对齐 Python：无时区信息时按本地时区解析）：
if hasOffset {
    t, err = time.Parse("2006-01-02T15:04:05Z07:00", isoStr)
} else {
    local := time.Local
    t, err = time.ParseInLocation("2006-01-02T15:04:05", isoStr, local)
}
```

**影响文件**：`parse_llm_response.go`

### 3.7 M9：EnsureValidLanguage 异常体系

```go
// 当前：
return "", fmt.Errorf("graph memory 不支持语言 %s，已注册: %v", language, registered)

// 修复后：
return "", exception.BuildError(
    exception.StatusMemoryStoreValidationInvalid,
    exception.WithParam("store_type", "graph memory"),
    exception.WithParam("error_msg", fmt.Sprintf("不支持语言 %s，已注册: %v", language, registered)),
)
```

**影响文件**：`extraction/prompts/entity_extraction/base.go`

### 3.8 M10：registerInBulk 空文件返回 error

```go
// 当前：
if len(promptPaths) == 0 {
    logger.Warn(...).Msg(...)
    return 0, nil
}

// 修复后（对齐 Python: raise build_error(StatusCode.MEMORY_GRAPH_PROMPT_FILES_MISSING)）：
if len(promptPaths) == 0 {
    return 0, exception.BuildError(
        exception.StatusMemoryGraphPromptFilesMissing,
        exception.WithParam("prompt_dir", promptDir),
    )
}
```

**影响文件**：`extraction/prompts/manager.go`

### 3.9 M11：TemplateManager 锁注释

```go
// mu 保护 templates map 的读写锁。
// 注意：sync.RWMutex 不可重入，禁止在 Get/registerInBulk 中递归调用自身。
mu sync.RWMutex
```

**影响文件**：`extraction/prompts/manager.go`

### 3.10 M12：tool 角色返回 nil

```go
func newMessageByRole(role, content string) schema.BaseMessage {
    switch role {
    case "system":
        return schema.NewSystemMessage(content)
    case "user":
        return schema.NewUserMessage(content)
    case "assistant":
        return schema.NewAssistantMessage(content)
    // case "tool" 跳过：当前 .pr.md 模板无 #tool# 角色，
    // ToolMessage 需要 tool_call_id，而模板中无此字段。
    // 待后续 #tool# 模板出现时正确实现。
    default:
        return nil
    }
}
```

**影响文件**：`extraction/prompts/pr_parser.go`

### 3.11 M13：InvokeLLM 加 cause

```go
// 当前：
exception.WithParam("error_msg", lastErr.Error())

// 修复后（对齐 Python: build_error(..., cause=e)）：
exception.WithCause(lastErr),
exception.WithParam("error_msg", lastErr.Error()),
```

**影响文件**：`base.go`

### 3.12 M14：删除 class Output 包裹

`formatReadableSchema` 中去掉 `"class Output:\n"` 包裹行和额外缩进，顶层字段直接输出。对齐 Python `readable_schema` 输出格式。

**影响文件**：`extraction/base.go`

## 4. 低严重度问题详细设计

### 4.1 L1：replacePlaceholders 精确匹配

```go
// 占位符正则：匹配 {{[xxx]}} 格式
var placeholderPattern = regexp.MustCompile(`\{\{\[([^\]]+)\]\}\}`)

// replacePlaceholders 精确匹配替换（对齐 Python _recursive_replace 的 lookup 语义）
func replacePlaceholders(desc string, langMap map[string]string) string {
    return placeholderPattern.ReplaceAllStringFunc(desc, func(match string) string {
        if replacement, ok := langMap[match]; ok {
            return replacement
        }
        return match  // 未找到则保留原占位符
    })
}
```

**影响文件**：`extraction/base.go`

### 4.2 L2：registerInBulk 锁范围+日志时序

```go
func (tm *TemplateManager) registerInBulk(...) (int, error) {
    tm.mu.Lock()
    promptPaths, _ := fs.Glob(...)     // 文件扫描移入锁内
    // 校验 + 注册
    tm.mu.Unlock()                     // 显式释放

    logger.Info(...).Msg(...)          // 日志移到锁外
    return count, nil
}
```

**影响文件**：`extraction/prompts/manager.go`

### 4.3 L3：pr_parser.go 死代码清理

删除第 53-78 行无用的第一段 for 循环（设了 `_ = fullEnd` 等变量后未使用，第 80 行重新实现）。

**影响文件**：`extraction/prompts/pr_parser.go`

## 5. Python Bug 已被 Go 修复（无需回退）

| 问题 | 说明 |
|------|------|
| parse_json mustContainKey 过滤 | Python 先清空 result 再查找空 dict，永远返回空；Go 正确实现 |
| _resolve_entity_merges extend 调用两次 | Python 重复 extend 再去重；Go 只 append 一次 |
| HumanEntity.description 引用错误变量 | Python 引用 ENTITY_DEFINITION_DESCRIPTION；Go 引用正确的 HumanEntityDescription |

## 6. 涉及文件清单

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `foundation/store/graph/base.go` | 修改 | 接口加 ReturnSimilarityScore |
| `foundation/store/graph/graph_object.go` | 修改 | Relation.LHS/RHS 改为 *Entity |
| `foundation/store/graph/milvus/` | 修改 | 实现 ReturnSimilarityScore + 反序列化适配 |
| `extraction/base.go` | 修改 | recursiveReplace + replacePlaceholders + ReadableSchema + multilingualResponseFormat + formatReadableSchema |
| `extraction/registry/entity_type_definition.go` | 修改 | 新增 EntityDefAttr + EntityDef.Attributes |
| `extraction/entity_type_definition.go` | 修改 | re-export EntityDefAttr |
| `extraction/extraction_models.go` | 无改动 | 已对齐 |
| `extraction/extraction_prompts.go` | 无改动 | 已对齐 |
| `extraction/parse_response.go` | 无改动 | Go 修复了 Python bug |
| `extraction/prompts/manager.go` | 修改 | registerInBulk error + 锁范围 + 日志时序 |
| `extraction/prompts/pr_parser.go` | 修改 | 死代码清理 + tool 角色跳过 |
| `extraction/prompts/entity_extraction/base.go` | 修改 | FormatExistingEntities 动态替换 + EnsureValidLanguage 异常体系 |
| `graph_memory/base.go` | 修改 | asyncTask channel + metricIsSim + ToRemove + 并行调度 + InvokeLLM 重试/cause + entityEnrich zip + name delete |
| `graph_memory/parse_llm_response.go` | 修改 | EntityOrDeclaration + Dict2Relation 参数 + ParseISO 时区 |
| `graph_memory/states.go` | 修改 | asyncTask 类型 + ToRemove 类型 |

## 7. 实现顺序建议

基于依赖关系，建议按以下顺序实现：

1. **S6 Relation.LHS/RHS 改为 *Entity** — 基础类型变更，后续多个修复依赖它
2. **S1 asyncTask channel 化** — 并发基础设施，S10 依赖它
3. **S4 EntityOrDeclaration** — 依赖 S6（Entity 类型）
4. **S3 ToRemove 类型变更** — 依赖 S6（从 Relation 取 LHS/RHS）
5. **S5 Dict2Relation 参数补齐** — 依赖 S4（EntityOrDeclaration）
6. **S2 metricIsSim** — 独立，可并行
7. **S7 StrictSchemaEnforce** — 独立
8. **S8 EntityDefAttr** — 独立
9. **S9 FormatExistingEntities** — 独立
10. **S10 并行调度** — 依赖 S1
11. **M1 recursiveReplace** — 独立
12. **其余中低问题** — 按文件分组批量修改
