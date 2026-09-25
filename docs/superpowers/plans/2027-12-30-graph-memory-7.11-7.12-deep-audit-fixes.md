# 7.11+7.12 Python 对齐度深度审查修复 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 修复 Go 实现中与 Python 源码的 30 项对齐偏差，确保 GraphMemory + Graph Extraction 功能正确性

**Architecture:** 按依赖顺序实现：先改基础类型（Relation.LHS/RHS → *Entity、asyncTask channel 化），再改上游逻辑（EntityOrDeclaration、ToRemove、Dict2Relation），最后改提取包（StrictSchemaEnforce、EntityDefAttr、recursiveReplace 等）

**Tech Stack:** Go 1.22+，项目现有 exception/logger/embed 包

---

## Task 1: Relation.LHS/RHS 改为 *Entity（S6）

**Files:**
- Modify: `internal/agentcore/foundation/store/graph/graph_object.go:59-74`（Relation 结构体）
- Modify: `internal/agentcore/foundation/store/graph/graph_object.go:207-217`（Relation.ToMap）
- Modify: `internal/agentcore/foundation/store/graph/graph_object.go:219-238`（Relation.UpdateConnectedEntities）
- Test: `internal/agentcore/foundation/store/graph/graph_object_test.go`

- [x] **Step 1: 写 Relation.LHS/RHS 改为 *Entity 的测试**

在 `graph_object_test.go` 中新增测试：
```go
func TestRelation_LHS_RHS_EntityRef(t *testing.T) {
    lhs := NewEntity()
    lhs.UUID = "entity-lhs-001"
    lhs.Name = "张三"
    rhs := NewEntity()
    rhs.UUID = "entity-rhs-002"
    rhs.Name = "北京"

    rel := NewRelation()
    rel.LHS = lhs
    rel.RHS = rhs

    // LHSUUID/RHSUUID 辅助方法
    if rel.LHSUUID() != "entity-lhs-001" {
        t.Errorf("LHSUUID() = %q, want %q", rel.LHSUUID(), "entity-lhs-001")
    }
    if rel.RHSUUID() != "entity-rhs-002" {
        t.Errorf("RHSUUID() = %q, want %q", rel.RHSUUID(), "entity-rhs-002")
    }

    // ToMap 输出 UUID 字符串
    m := rel.ToMap()
    if m["lhs"] != "entity-lhs-001" {
        t.Errorf("ToMap()[lhs] = %v, want %q", m["lhs"], "entity-lhs-001")
    }
    if m["rhs"] != "entity-rhs-002" {
        t.Errorf("ToMap()[rhs] = %v, want %q", m["rhs"], "entity-rhs-002")
    }
}

func TestRelation_LHS_RHS_Nil(t *testing.T) {
    rel := NewRelation()
    rel.LHS = nil
    rel.RHS = nil

    if rel.LHSUUID() != "" {
        t.Errorf("LHSUUID() nil = %q, want empty", rel.LHSUUID())
    }
    if rel.RHSUUID() != "" {
        t.Errorf("RHSUUID() nil = %q, want empty", rel.RHSUUID())
    }

    m := rel.ToMap()
    if m["lhs"] != "" {
        t.Errorf("ToMap()[lhs] nil = %v, want empty", m["lhs"])
    }
    if m["rhs"] != "" {
        t.Errorf("ToMap()[rhs] nil = %v, want empty", m["rhs"])
    }
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; sleep 1; GOPROXY=https://goproxy.cn,direct go test ./internal/agentcore/foundation/store/graph/... -run TestRelation_LHS_RHS -v -count=1`

- [x] **Step 3: 修改 Relation 结构体**

在 `graph_object.go` 第 59-74 行，将 `LHS string` 和 `RHS string` 改为 `*Entity`：
```go
type Relation struct {
    NamedGraphObject
    ValidSince int64 `json:"valid_since"`
    ValidUntil int64 `json:"valid_until"`
    OffsetSince int8 `json:"offset_since"`
    OffsetUntil int8 `json:"offset_until"`
    // LHS 左侧实体（对齐 Python: lhs: BaseGraphObject | str）
    LHS *Entity `json:"-"`
    // RHS 右侧实体
    RHS *Entity `json:"-"`
}
```

- [x] **Step 4: 添加 LHSUUID/RHSUUID 辅助方法**

在 `graph_object.go` 的导出函数区块新增：
```go
// LHSUUID 返回左侧实体 UUID
func (r *Relation) LHSUUID() string {
    if r.LHS != nil {
        return r.LHS.UUID
    }
    return ""
}

// RHSUUID 返回右侧实体 UUID
func (r *Relation) RHSUUID() string {
    if r.RHS != nil {
        return r.RHS.UUID
    }
    return ""
}
```

- [x] **Step 5: 修改 Relation.ToMap 输出 UUID 字符串**

在 `graph_object.go` 第 207-217 行，将 ToMap 中的 lhs/rhs 改为从 *Entity 取 UUID：
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
    m["valid_since"] = r.ValidSince
    m["valid_until"] = r.ValidUntil
    m["offset_since"] = r.OffsetSince
    m["offset_until"] = r.OffsetUntil
    return m
}
```

- [x] **Step 6: 修改 UpdateConnectedEntities 签名不变，实现适配**

`UpdateConnectedEntities(lhs, rhs *Entity)` 签名不变，因为参数已经是 `*Entity`。但需确认调用方传入的参数类型正确。

- [x] **Step 7: 修改 NewRelation 默认值**

确保 `NewRelation()` 不再初始化 LHS/RHS 为空字符串（零值 nil 即可）。

- [x] **Step 8: 修复 graph_memory 包中所有访问 relation.LHS/relation.RHS 的地方**

在 `graph_memory/` 所有文件中将 `relation.LHS`（原为 string）改为 `relation.LHSUUID()` 或 `relation.LHS.UUID`（根据上下文）。需要逐一替换：
- 字符串比较场景 → `relation.LHSUUID()`
- 需要访问 Entity 内容 → `relation.LHS.Content` 等
- 赋值场景 → `relation.LHS = entityPtr`

- [x] **Step 9: 修复 Milvus 反序列化**

在 Milvus 图存储的反序列化代码中，将读取到的 `lhs`/`rhs` UUID 字符串构造为 `*Entity` 空壳：
```go
lhsUUID, _ := doc["lhs"].(string)
rhsUUID, _ := doc["rhs"].(string)
rel := NewRelation()
if lhsUUID != "" {
    rel.LHS = &Entity{}
    rel.LHS.UUID = lhsUUID
}
if rhsUUID != "" {
    rel.RHS = &Entity{}
    rel.RHS.UUID = rhsUUID
}
```

- [x] **Step 10: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test ./internal/agentcore/foundation/store/graph/... ./internal/agentcore/memory/graph/... -v -count=1 2>&1 | tail -50`

- [x] **Step 11: 提交**

```bash
git add -A && git commit -m "fix(S6): Relation.LHS/RHS 改为 *Entity 对齐 Python BaseGraphObject | str

- LHS/RHS 从 string 改为 *Entity，json:\"-\" 不直接序列化
- 新增 LHSUUID()/RHSUUID() 辅助方法
- ToMap 输出 UUID 字符串，反序列化填 UUID 空壳
- graph_memory 包所有访问 LHS/RHS 处适配"
```

---

## Task 2: asyncTask channel 化（S1）

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/states.go:127-152`（asyncTask 类型）
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go:1849-1866`（invokeLLMAsync）
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`（所有 state.Tasks 消费处）
- Test: `internal/agentcore/memory/graph/graph_memory/states_test.go`

- [x] **Step 1: 写 asyncTask channel 化的测试**

```go
func TestAsyncTask_Wait(t *testing.T) {
    ch := make(asyncTask, 1)
    go func() {
        ch <- asyncResult{Content: "hello", Err: nil}
    }()
    content, err := ch.Wait()
    if err != nil {
        t.Errorf("Wait() err = %v, want nil", err)
    }
    if content != "hello" {
        t.Errorf("Wait() content = %q, want %q", content, "hello")
    }
}

func TestAsyncTask_Wait_Error(t *testing.T) {
    ch := make(asyncTask, 1)
    testErr := fmt.Errorf("test error")
    go func() {
        ch <- asyncResult{Content: "", Err: testErr}
    }()
    content, err := ch.Wait()
    if content != "" {
        t.Errorf("Wait() content = %q, want empty", content)
    }
    if err == nil || err.Error() != "test error" {
        t.Errorf("Wait() err = %v, want 'test error'", err)
    }
}
```

- [x] **Step 2: 运行测试确认失败**

- [x] **Step 3: 修改 states.go 中 asyncTask 类型**

```go
// asyncResult 异步 LLM 调用结果
type asyncResult struct {
    // Content LLM 响应内容
    Content string
    // Err 调用错误
    Err error
}

// asyncTask 异步 LLM 调用任务，通过 channel 传递结果
// 对齐 Python: asyncio.create_task 返回的 Future
type asyncTask chan asyncResult

// Wait 阻塞等待异步任务完成，返回结果
// 对齐 Python: await future
func (t asyncTask) Wait() (string, error) {
    result := <-t
    return result.Content, result.Err
}
```

删除旧的 `asyncTask` struct 定义和 `Wait()` 方法。

- [x] **Step 4: 修改 invokeLLMAsync**

```go
func (gm *GraphMemory) invokeLLMAsync(ctx context.Context, kwargs map[string]any, tmpl *prompt.PromptTemplate, outputModel map[string]any) asyncTask {
    ch := make(asyncTask, 1)
    go func() {
        resp, err := gm.InvokeLLM(ctx, kwargs, tmpl, outputModel)
        ch <- asyncResult{Content: resp, Err: err}
    }()
    return ch
}
```

- [x] **Step 5: 修改所有 state.Tasks 消费处**

将 `state.Tasks[i].Result` / `state.Tasks[i].Err` 改为 `state.Tasks[i].Wait()`。

- [x] **Step 6: 运行测试**

- [x] **Step 7: 提交**

```bash
git add -A && git commit -m "fix(S1): asyncTask channel 化，对齐 Python await Future 语义

- asyncTask 从 struct 改为 chan asyncResult
- 新增 Wait() 阻塞读取方法
- invokeLLMAsync 返回带缓冲 channel
- 所有 state.Tasks 消费处改用 Wait()"
```

---

## Task 3: BaseGraphStore.ReturnSimilarityScore + metricIsSim 修复（S2）

**Files:**
- Modify: `internal/agentcore/foundation/store/graph/base.go:18-56`（接口）
- Modify: `internal/agentcore/foundation/store/graph/milvus/milvus.go`（Milvus 实现）
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go:136-140,231`（metricIsSim）
- Test: `internal/agentcore/foundation/store/graph/base_test.go`

- [x] **Step 1: 写测试**

```go
// mockGraphStoreForSim 用于测试 ReturnSimilarityScore
type mockGraphStoreForSim struct {
    mockGraphStore
    simScore bool
}
func (m *mockGraphStoreForSim) ReturnSimilarityScore() bool { return m.simScore }
```

- [x] **Step 2: 在 BaseGraphStore 接口加 ReturnSimilarityScore**

```go
type BaseGraphStore interface {
    // ... 现有方法 ...
    // ReturnSimilarityScore 返回搜索分数是否为相似度（越大越相似）
    // 对齐 Python: GraphStore.return_similarity_score
    ReturnSimilarityScore() bool
}
```

- [x] **Step 3: Milvus 实现 ReturnSimilarityScore 返回 true**

- [x] **Step 4: 其他后端根据 DistanceMetric 推导**

- [x] **Step 5: 修改 GraphMemory 构造函数**

```go
// 当前：
metricIsSim: false,
// 修复后：
metricIsSim: dbBackend.ReturnSimilarityScore(),
```

- [x] **Step 6: 运行测试**

- [x] **Step 7: 提交**

---

## Task 4: ToRemove 类型变更（S3）

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/states.go:191`（ToRemove 字段）
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`（所有读写 ToRemove 处）
- Test: `internal/agentcore/memory/graph/graph_memory/base_test.go`

- [x] **Step 1: 写测试**

- [x] **Step 2: 修改 GraphMemState.ToRemove 类型**

```go
// 当前：
ToRemove map[string]struct{}
// 修复后：
ToRemove map[string]*graph.Relation
```

- [x] **Step 3: 修改所有写入 ToRemove 处**

`ToRemove[uuid] = struct{}{}` → `ToRemove[uuid] = relation`

- [x] **Step 4: 修改 updateEntitiesForRelationRemoval**

```go
for _, relation := range state.ToRemove {
    if relation.LHS != nil {
        entitiesToRemoveRelationsFrom[relation.LHS.UUID] = struct{}{}
    }
    if relation.RHS != nil {
        entitiesToRemoveRelationsFrom[relation.RHS.UUID] = struct{}{}
    }
}
```

- [x] **Step 5: 修改所有读取 ToRemove 处**

遍历从 `for uuid := range state.ToRemove` 改为 `for _, relation := range state.ToRemove`

- [x] **Step 6: 运行测试**

- [x] **Step 7: 提交**

---

## Task 5: EntityOrDeclaration 联合结构体（S4）

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/parse_llm_response.go:225-298`（ResolveEntities）
- Modify: `internal/agentcore/memory/graph/graph_memory/parse_llm_response.go:86-198`（Dict2Relation/ParseAllRelations/DeclareEntities）
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`（entityMerge 消费处）
- Test: `internal/agentcore/memory/graph/graph_memory/parse_llm_response_test.go`

- [x] **Step 1: 定义 EntityOrDeclaration 结构体**

在 `parse_llm_response.go` 结构体区块新增：
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
func (e EntityOrDeclaration) Name() string {
    if e.Entity != nil {
        return e.Entity.Name
    }
    if e.Decl != nil {
        return e.Decl.Name
    }
    return ""
}

// EntityTypeID 获取实体类型 ID
func (e EntityOrDeclaration) EntityTypeID() int {
    if e.Decl != nil {
        return e.Decl.EntityTypeID
    }
    return 0
}
```

- [x] **Step 2: 修改 ResolveEntities 返回 []EntityOrDeclaration**

- [x] **Step 3: 修改 DeclareEntities 接收 []EntityOrDeclaration**

- [x] **Step 4: 修改 Dict2Relation entities 参数为 []EntityOrDeclaration**

- [x] **Step 5: 修改 ParseAllRelations entities 参数**

- [x] **Step 6: 修改 base.go 中 entityMerge 的 resolved 消费**

- [x] **Step 7: 运行测试**

- [x] **Step 8: 提交**

---

## Task 6: Dict2Relation 参数补齐（S5）

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/parse_llm_response.go:86-155`（Dict2Relation）
- Modify: `internal/agentcore/memory/graph/graph_memory/parse_llm_response.go:157-198`（ParseAllRelations）
- Test: `internal/agentcore/memory/graph/graph_memory/parse_llm_response_test.go`

- [x] **Step 1: 修改 Dict2Relation 签名加 createdAt/userID**

```go
func Dict2Relation(response map[string]any, entities []EntityOrDeclaration, createdAt int64, userID string) *graph.Relation
```

- [x] **Step 2: 在 Dict2Relation 内部设置 Relation 的 CreatedAt 和 UserID**

- [x] **Step 3: 修改 ParseAllRelations 签名加 createdAt/userID 并传递给 Dict2Relation**

- [x] **Step 4: 修改调用方传入 state.CurrentTimestamp 和 userID**

- [x] **Step 5: 运行测试**

- [x] **Step 6: 提交**

---

## Task 7: StrictSchemaEnforce + multilingualResponseFormat 修复（S7）

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/base.go:93-128`（StrictSchemaEnforce）
- Modify: `internal/agentcore/memory/graph/extraction/extraction_prompts.go:523-542`（multilingualResponseFormat）
- Test: `internal/agentcore/memory/graph/extraction/base_test.go`

- [x] **Step 1: 修改 StrictSchemaEnforce 去掉 len(props)>0 条件**

```go
// 当前：
if props, ok := node["properties"].(map[string]any); ok && len(props) > 0 {
// 修复后：
if props, ok := node["properties"].(map[string]any); ok {
```

- [x] **Step 2: 在 multilingualResponseFormat 中加 StrictSchemaEnforce 调用**

```go
func multilingualResponseFormat(modelType reflect.Type, language string) map[string]any {
    // ... 现有逻辑 ...
    replaced := ReplaceDescriptions(params, langMap)
    schemaMap := commonschema.ToJSONSchemaMap(replaced)
    StrictSchemaEnforce(schemaMap)  // 新增
    return ResponseFormat(modelType.Name(), schemaMap)
}
```

- [x] **Step 3: 运行测试**

- [x] **Step 4: 提交**

---

## Task 8: EntityDefAttr + EntityDef.Attributes（S8）

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/registry/entity_type_definition.go:5-69`
- Modify: `internal/agentcore/memory/graph/extraction/entity_type_definition.go:16-41`
- Test: `internal/agentcore/memory/graph/extraction/registry/` (新增测试)

- [x] **Step 1: 在 registry/entity_type_definition.go 新增 EntityDefAttr**

```go
// EntityDefAttr 实体定义属性模板
// 对齐 Python: EntityDefAttr(MultilingualBaseModel)
type EntityDefAttr struct {
    // Content 实体摘要模板（默认空字符串）
    Content string
}

// DefaultEntityDefAttr 默认实体属性模板
var DefaultEntityDefAttr = &EntityDefAttr{Content: ""}
```

- [x] **Step 2: EntityDef 加 Attributes 字段**

```go
type EntityDef struct {
    Name        string
    Description map[string]string
    // Attributes 实体属性模板
    Attributes *EntityDefAttr
}
```

- [x] **Step 3: DefaultEntity/HumanEntity/AIEntity 设置 Attributes**

- [x] **Step 4: entity_type_definition.go re-export EntityDefAttr**

- [x] **Step 5: 运行测试**

- [x] **Step 6: 提交**

---

## Task 9: FormatExistingEntities 动态替换（S9）

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base.go:76-92`
- Test: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base_test.go`

- [x] **Step 1: 写测试验证动态替换**

- [x] **Step 2: 修改 FormatExistingEntities**

```go
func FormatExistingEntities(entities []map[string]any, startIdx int, language string) string {
    if len(entities) == 0 { return "" }
    tmpl := registry.DisplayEntity[language]
    var lines []string
    for i, ent := range entities {
        line := strings.ReplaceAll(tmpl, "{i}", fmt.Sprintf("%d", startIdx+i))
        for key, val := range ent {
            line = strings.ReplaceAll(line, "{"+key+"}", fmt.Sprintf("%v", val))
        }
        lines = append(lines, line)
    }
    return strings.Join(lines, "\n\n")
}
```

- [x] **Step 3: 运行测试**

- [x] **Step 4: 提交**

---

## Task 10: 并行调度修复（S10）

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`（extractEntityDeclarations、fetchRelevantEntities、entityMerge、entityEnrich）

- [x] **Step 1: extractEntityDeclarations 中嵌入操作放入 goroutine+channel**

- [x] **Step 2: fetchRelevantEntities 中先 Wait() 嵌入结果**

- [x] **Step 3: entityMerge 中阻塞/非阻塞任务并发发起**

- [x] **Step 4: entityEnrich 中并发发起 LLM 调用，统一 Wait()**

- [x] **Step 5: 运行测试**

- [x] **Step 6: 提交**

---

## Task 11: recursiveReplace 通用 BFS 函数 + ReadableSchema 修复（M1）

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/base.go:64-91,167-211`
- Test: `internal/agentcore/memory/graph/extraction/base_test.go`

- [x] **Step 1: 写 recursiveReplace 测试**

- [x] **Step 2: 实现 recursiveReplace 函数**

- [x] **Step 3: 修改 ReadableSchema 使用 recursiveReplace 删除 title/required + 内联 $ref**

- [x] **Step 4: 修改 formatReadableSchema 删除 class Output 包裹（M14）**

- [x] **Step 5: 运行测试**

- [x] **Step 6: 提交**

---

## Task 12: extraction 包中等问题批量修复（M3/M9/L1）

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/base.go`（FormatSchemaInfo refDict 类型 M3 + replacePlaceholders 精确匹配 L1）
- Modify: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base.go`（EnsureValidLanguage 异常体系 M9）
- Test: 对应测试文件

- [x] **Step 1: 修改 FormatSchemaInfo refDict 类型为 map[string]any（M3）**

- [x] **Step 2: 修改 ReadableSchema 返回类型为 map[string]any（M3）**

- [x] **Step 3: 修改 replacePlaceholders 为正则精确匹配（L1）**

```go
var placeholderPattern = regexp.MustCompile(`\{\{\[([^\]]+)\]\}\}`)

func replacePlaceholders(desc string, langMap map[string]string) string {
    return placeholderPattern.ReplaceAllStringFunc(desc, func(match string) string {
        if replacement, ok := langMap[match]; ok {
            return replacement
        }
        return match
    })
}
```

- [x] **Step 4: 修改 EnsureValidLanguage 使用异常体系（M9）**

- [x] **Step 5: 运行测试**

- [x] **Step 6: 提交**

---

## Task 13: prompts 包中低问题批量修复（M10/M11/M12/L2/L3）

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/prompts/manager.go`（M10 + M11 + L2）
- Modify: `internal/agentcore/memory/graph/extraction/prompts/pr_parser.go`（M12 + L3）

- [x] **Step 1: registerInBulk 空文件返回异常（M10）**

- [x] **Step 2: TemplateManager 锁注释标注不可重入（M11）**

- [x] **Step 3: registerInBulk 锁范围+日志时序对齐（L2）**

- [x] **Step 4: pr_parser.go tool 角色返回 nil（M12）**

- [x] **Step 5: pr_parser.go 删除死代码第 51-78 行（L3）**

- [x] **Step 6: 运行测试**

- [x] **Step 7: 提交**

---

## Task 14: graph_memory 包中等问题批量修复（M4/M6/M7/M8/M13）

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`（M4 + M6 + M7 + M13）
- Modify: `internal/agentcore/memory/graph/graph_memory/parse_llm_response.go`（M8）

- [x] **Step 1: entityEnrich 改用 zip 语义（M4）**

- [x] **Step 2: InvokeLLM 重试释放信号量（M6）**

- [x] **Step 3: InvokeLLM 加 WithCause（M13）**

- [x] **Step 4: extractEntityDeclarations name 读取后删除（M7）**

- [x] **Step 5: ParseISO 不追加 Z，用本地时区解析（M8）**

- [x] **Step 6: 运行测试**

- [x] **Step 7: 提交**

---

## Task 15: 全量测试验证 + 覆盖率检查

**Files:**
- All modified packages

- [x] **Step 1: 运行全量单元测试**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test ./internal/agentcore/foundation/store/graph/... ./internal/agentcore/memory/graph/... -v -count=1`

- [x] **Step 2: 运行覆盖率检查**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test -cover ./internal/agentcore/foundation/store/graph/... ./internal/agentcore/memory/graph/...`

- [x] **Step 3: 补充未达 85% 的包的测试**

- [x] **Step 4: 更新 IMPLEMENTATION_PLAN.md 中 7.11/7.12 状态**

- [x] **Step 5: 最终提交**
