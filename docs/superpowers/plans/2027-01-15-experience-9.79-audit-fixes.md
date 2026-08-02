# 9.79 Experience 审查修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 根据 brainstorming 确认的 11 项审查决策，修改 experience 包的代码（签名、类型、命名、结构体定义、测试拆分）

**Architecture:** 按依赖顺序修改——先改底层类型定义（types.go/lifecycle.go 的结构体和工厂方法），再改依赖它们的 manager/scorer/tracker/orchestrator，最后拆分测试文件并编译验证。所有修改保持包内一致性，不引入新的循环依赖。

**Tech Stack:** Go 1.22+, standard library, project 内部包（schema, checkpointing, llm, operator 等）

---

## 文件结构

| 文件 | 负责内容 | 修改类型 |
|------|---------|---------|
| `types.go` | 类型定义、RequestID 字段类型 | 修改 |
| `lifecycle.go` | HostFacingExperienceResult 工厂方法签名 | 修改 |
| `tracker.go` | RecordScoreUpdate 结构体定义 + 使用 | 修改+新增类型 |
| `manager.go` | PendingGovernance 结构体、StageRecords/StageApplyResults 签名、map[UpdateKey]→UpdateValue | 修改+新增类型 |
| `scorer.go` | NewExperienceScorer/UpdateLLM nil 校验 | 修改 |
| `orchestrator.go` | 适配 StageApplyResults 新签名 | 修改 |
| `common.go` | SetApplyUpdatesFn 签名适配 | 修改（已在 manager.go） |
| `types_test.go` | 拆分后的 types 测试 | 新建 |
| `lifecycle_test.go` | 拆分后的 lifecycle 测试 | 新建 |
| `common_test.go` | 拆分后的 common 测试 | 新建 |
| `scorer_test.go` | 拆分后的 scorer 测试 | 新建 |
| `tracker_test.go` | 拆分后的 tracker 测试 | 新建 |
| `manager_test.go` | 拆分后的 manager 测试 | 新建 |
| `orchestrator_test.go` | 拆分后的 orchestrator 测试 | 新建 |
| `experience_test.go` | 删除（拆分到 7 个文件） | 删除 |

---

## 决策索引

| 编号 | 决策 | 涉及文件 |
|------|------|---------|
| B7/B8 | requestID 统一为 string | lifecycle.go, types.go |
| B10/B11 | StageRecords/StageApplyResults 改为指针+error | manager.go, orchestrator.go |
| B14 | session → sessionID | tracker.go |
| B4/B15/B16 | llm.Model 统一为 *llm.Model + nil 校验 | scorer.go |
| A1 | map[UpdateKey]any → map[UpdateKey]UpdateValue | manager.go |
| A4 | pendingGovernance 内层 any → PendingGovernance 结构体 | manager.go |
| A5 | tracker updates any → RecordScoreUpdate 结构体 | tracker.go |
| A7 | LocalApplyPreview.Records 已是 []EvolutionRecord，无需修改（schema.ApplyResult.Records 是 []any 属于 schema 包范围） | 无需改动 |
| B1 | 测试拆分为 7 个文件 | 所有 _test.go |
| B5/B6/B9/B12/B13 | 保持现状（不改） | — |
| A3 | 保持 []map[string]any（不改） | — |

---

### Task 1: B7/B8 — requestID 统一为 string

**Files:**
- Modify: `internal/evolving/experience/types.go:43,69,166-172,177-182`
- Modify: `internal/evolving/experience/lifecycle.go:43,86-97,101-123,127-138`

**当前状态：**
- `HostFacingExperienceResult.RequestID` 字段：`*string`（lifecycle.go:43）
- `ExperienceApprovalRequest.RequestID` 字段：`*string`（types.go:69）
- `HostFacingExperienceResultPendingApproval(skillName string, requestID string, ...)` — requestID 是 string（lifecycle.go:87）
- `HostFacingExperienceResultPersisted(skillName string, requestID *string, ...)` — requestID 是 *string（lifecycle.go:102）
- `HostFacingExperienceResultRejected(skillName string, requestID *string, ...)` — requestID 是 *string（lifecycle.go:128）
- `ExperienceApprovalRequest.ToHostResult()` 内部 `requestID = *r.RequestID`（types.go:166-168）
- `ExperienceApplyResult.ToHostResult(requestID *string, ...)` — requestID 是 *string（types.go:177）

**修改计划：**

- [x] **Step 1: 修改 HostFacingExperienceResult.RequestID 字段类型**

在 lifecycle.go:43，将 `RequestID *string` 改为 `RequestID string`。

- [x] **Step 2: 修改三个工厂方法的 requestID 参数类型和内部逻辑**

lifecycle.go:
1. `HostFacingExperienceResultPendingApproval` — requestID 已是 string，去掉 `strPtr(requestID)`，直接赋值 `RequestID: requestID`
2. `HostFacingExperienceResultPersisted` — requestID 从 `*string` 改为 `string`，去掉 nil 检查，直接赋值 `RequestID: requestID`
3. `HostFacingExperienceResultRejected` — requestID 从 `*string` 改为 `string`，直接赋值 `RequestID: requestID`

- [x] **Step 3: 修改 ExperienceApprovalRequest.RequestID 字段类型**

在 types.go:69，将 `RequestID *string` 改为 `RequestID string`。

- [x] **Step 4: 修改 ExperienceApprovalRequest.ToHostResult()**

在 types.go:166-172，去掉 `requestID = *r.RequestID` 解引用逻辑，改为 `requestID := r.RequestID`（已是 string）。

- [x] **Step 5: 修改 ExperienceApplyResult.ToHostResult() 签名**

在 types.go:177，将 `requestID *string` 改为 `requestID string`。

- [x] **Step 6: 删除 strPtr 辅助函数**

lifecycle.go:143-145 的 `strPtr` 函数不再被本文件使用，检查其他文件是否有引用。如无引用则删除；如有则保留。

- [x] **Step 7: 搜索并适配所有调用端**

搜索包内所有使用 `&requestID`、`requestID *string`、`*r.RequestID`、`strPtr(requestID)` 的地方，改为 string 传递。重点位置：
- orchestrator.go:162-165 的 `requestID = *request.RequestID` → `requestID := request.RequestID`
- orchestrator.go:145-150 的 `StageApplyResults` 调用中 `&o.requestIDPrefix` → `o.requestIDPrefix`
- manager.go 中所有 `&requestID` / `*request.RequestID` 改为 string 传递
- manager.go stagePendingRequest 中 `requestID := stagedPending.ChangeID` 和 `RequestID: &requestID` → `RequestID: requestID`

- [x] **Step 8: 补充修改 requestIDPrefix 参数类型**

与 requestID 统一为 string 的决策逻辑一致，以下参数也应从 `*string` 改为 `string`：
- `StageRecords` 的 `requestIDPrefix *string` 参数（manager.go:215）→ `requestIDPrefix string`
- `StageApplyResults` 的 `requestIDPrefix *string` 参数（manager.go:251）→ `requestIDPrefix string`
- `stageRecordsInternal` 的 `requestIDPrefix *string` 参数（manager.go:567）→ `requestIDPrefix string`
- `stagePendingRequest` 的 `requestIDPrefix *string` 参数（manager.go:603）→ `requestIDPrefix string`
- `MakePendingChange` 的 `requestIDPrefix *string` 参数（common.go:29）→ `requestIDPrefix string`
- `makePendingChangeFromPreview` 的 `requestIDPrefix *string` 参数（manager.go:730）→ `requestIDPrefix string`
- `ExperienceProposal.SignalType *string` 和 `SignalSource *string`（types.go:53-55）— 这些保持 *string，因为信号类型/来源确实可能为空（语义不同于 requestID）

内部逻辑调整：
- `MakePendingChange` 中 `if requestIDPrefix != nil && *requestIDPrefix != ""` → `if requestIDPrefix != ""`
- `CommitProposal` 调用 `StageRecords` 时传 `nil` → 改为传 `""`
- orchestrator.go 中 `&o.requestIDPrefix` → `o.requestIDPrefix`

- [x] **Step 8: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/experience/...
```

---

### Task 2: A7 — LocalApplyPreview.Records 改为 []checkpointing.EvolutionRecord

**Files:**
- Modify: `internal/evolving/experience/lifecycle.go:17`
- Modify: `internal/evolving/experience/manager.go:480-485`

**当前状态：**
- `LocalApplyPreview.Records` 字段类型：`[]checkpointing.EvolutionRecord`（lifecycle.go:17）— **已经是具体类型**
- `BuildLocalApplyPreview` 中遍历 `result.Records []any`（来自 schema.ApplyResult）做类型断言（manager.go:481-484）

**分析：** A7 决策是"改为 []*checkpointing.PendingChange"，但检查代码后发现 `LocalApplyPreview.Records` **已经是 `[]checkpointing.EvolutionRecord`**，不是 `[]any`。实际的问题是 `schema.ApplyResult.Records` 是 `[]any`，BuildLocalApplyPreview 需要做类型断言来转换。这属于 schema 包的设计，不在 experience 包范围内修改。

**修正：** LocalApplyPreview.Records 已经是具体类型，无需修改。A7 决策中关于 BuildLocalApplyPreview 的 Records 字段类型不需要改——它已经对了。唯一可以改进的是 schema.ApplyResult.Records 的类型，但那属于 schema 包修改，不在本次范围内。

- [x] **Step 1: 确认 LocalApplyPreview.Records 类型已是 []checkpointing.EvolutionRecord**

读取 lifecycle.go:17 确认字段类型。如果已经是具体类型，则此 Task 标记完成，不做任何代码修改。

---

### Task 3: A4 — 定义 PendingGovernance 结构体

**Files:**
- Modify: `internal/evolving/experience/manager.go:34-35,147,160-166,181-182,382-386,404-416,423`

**当前状态：**
- `pendingGovernance` 字段：`map[string]map[string]any`（manager.go:35）
- 写入时（RequestSimplify, 行382-386）：`map[string]any{"kind": "simplify", "skill_name": skillName, "actions": actions}`
- 读取时（ApproveSimplify, 行404-416）：`gov["skill_name"].(string)` 和 `gov["actions"].([]map[string]any)`
- 读取时（RejectSimplify, 行423）：只做 `delete`，不读取内层值
- NewExperienceManager 参数（行147）：`pendingGovernance map[string]map[string]any`
- PendingGovernance() getter（行181-182）：返回 `map[string]map[string]any`

**结构体字段设计：**

```go
// PendingGovernance 暂存治理操作条目。
type PendingGovernance struct {
    // Kind 操作类型（当前仅 "simplify"）
    Kind string
    // SkillName 技能名称
    SkillName string
    // Actions 整理操作列表（来自 LLM 输出，保持 []map[string]any）
    Actions []map[string]any
}
```

**修改计划：**

- [x] **Step 1: 在 manager.go 结构体区块添加 PendingGovernance 定义**

在 manager.go 的结构体区块（行16-36 之后），添加 PendingGovernance 结构体定义。

- [x] **Step 2: 修改 ExperienceManager.pendingGovernance 字段类型**

行35：`pendingGovernance map[string]map[string]any` → `pendingGovernance map[string]*PendingGovernance`

- [x] **Step 3: 修改 NewExperienceManager 参数和初始化**

行147：`pendingGovernance map[string]map[string]any` → `pendingGovernance map[string]*PendingGovernance`
行160：`pendingGovernance: pendingGovernance,` 保持不变
行165-166：`em.pendingGovernance = map[string]map[string]any{}` → `em.pendingGovernance = map[string]*PendingGovernance{}`

- [x] **Step 4: 修改 PendingGovernance() getter 返回类型**

行181-182：`map[string]map[string]any` → `map[string]*PendingGovernance`

- [x] **Step 5: 修改 RequestSimplify 写入逻辑**

行382-386：从 `m.pendingGovernance[requestID] = map[string]any{"kind": ..., "skill_name": ..., "actions": ...}` 改为：
```go
m.pendingGovernance[requestID] = &PendingGovernance{
    Kind:       "simplify",
    SkillName:  skillName,
    Actions:    actions,
}
```

- [x] **Step 6: 修改 ApproveSimplify 读取逻辑**

行404-416：从 `gov := m.pendingGovernance[requestID]` + `gov["skill_name"].(string)` + `gov["actions"].([]map[string]any)` 改为：
```go
gov := m.pendingGovernance[requestID]
if gov == nil {
    return map[string]int{}, nil
}
delete(m.pendingGovernance, requestID)
return ExecuteSimplifyActions(ctx, m.store, gov.SkillName, gov.Actions), nil
``

- [x] **Step 7: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/experience/...
```

---

### Task 4: A5 — 定义 RecordScoreUpdate 结构体

**Files:**
- Modify: `internal/evolving/experience/tracker.go:83,104-107,122-125,174,189-192,206-209,269,281-284`

**当前状态：**
- tracker 中多处 `updates := map[string]map[string]any{}`
- 内层 map key 固定：`"score"`(float64) 和 `"usage_stats"`(map[string]any)
- 写入：`updates[record.ID] = map[string]any{"score": ..., "usage_stats": ...}`
- 读取：`updates[record.ID]["usage_stats"]` + 类型断言 `.(map[string]any)`
- `EvolutionStore.UpdateRecordScores()` 接口接受 `map[string]map[string]any`（checkpointing 包外部接口）

**结构体字段设计：**

```go
// RecordScoreUpdate 单条记录的评分更新数据。
type RecordScoreUpdate struct {
    // Score 新评分
    Score float64
    // UsageStats 使用统计字典（传给 EvolutionStore.UpdateRecordScores）
    UsageStats map[string]any
}
```

**注意：** 因为 `UpdateRecordScores` 接受 `map[string]map[string]any`，需要在调用时将 `map[string]*RecordScoreUpdate` 转换为 `map[string]map[string]any`。添加一个 `ToMap()` 方法：

```go
// ToMap 转换为 map[string]any，用于调用 EvolutionStore.UpdateRecordScores。
func (u *RecordScoreUpdate) ToMap() map[string]any {
    return map[string]any{
        "score":       u.Score,
        "usage_stats": u.UsageStats,
    }
}
```

**修改计划：**

- [x] **Step 1: 在 tracker.go 结构体区块添加 RecordScoreUpdate 定义**

在 tracker.go 的结构体区块（行12-34 之后），添加 RecordScoreUpdate 结构体和 ToMap 方法。

- [x] **Step 2: 修改 RecordPresented 方法中的 updates 类型**

行83：`updates := map[string]map[string]any{}` → `updates := map[string]*RecordScoreUpdate{}`
行104-107：`updates[record.ID] = map[string]any{"score": ..., "usage_stats": ...}` → `updates[record.ID] = &RecordScoreUpdate{Score: ..., UsageStats: ...}`
行110：调用 `UpdateRecordScores` 时，需要先转换为 `map[string]map[string]any`。添加转换函数或就地转换。
行122-125：`updates[record.ID]["usage_stats"]` → `updates[record.ID].UsageStats`

- [x] **Step 3: 修改 RecordPresentedRecords 方法中的 updates 类型**

行174：同 Step 2 的转换模式
行189-192：同上
行206-209：同上

- [x] **Step 4: 修改 EvaluatePresented 方法中的 updates 类型**

行269：同上
行281-284：同上

- [x] **Step 5: 添加 updatesToMap 转换辅助函数**

在 tracker.go 非导出函数区块添加：
```go
// updatesToMap 将 RecordScoreUpdate map 转换为 UpdateRecordScores 所需的 map[string]map[string]any 格式。
func updatesToMap(updates map[string]*RecordScoreUpdate) map[string]map[string]any {
    result := make(map[string]map[string]any, len(updates))
    for id, update := range updates {
        result[id] = update.ToMap()
    }
    return result
}
```

- [x] **Step 6: 修改所有 UpdateRecordScores 调用**

将 `t.store.UpdateRecordScores(ctx, skillName, updates)` 改为 `t.store.UpdateRecordScores(ctx, skillName, updatesToMap(updates))`

- [x] **Step 7: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/experience/...
```

---

### Task 5: A1 — map[UpdateKey]any → map[UpdateKey]UpdateValue（内部使用）

**Files:**
- Modify: `internal/evolving/experience/manager.go:126,133,552,693-705,711-723,824-876`

**当前状态：**
- `applyUpdatesFn` 变量：`func(map[string]operator.Operator, map[schema.UpdateKey]any) []schema.ApplyResult`
- `SetApplyUpdatesFn` 参数：同上
- `ApplyUpdatesFromManager` 参数：`map[schema.UpdateKey]any`
- `evolvingExecuteUpdates` 参数：`map[schema.UpdateKey]any`
- `defaultExecuteUpdates` 参数：`map[schema.UpdateKey]any`
- `previewApplyResults` 内部构造：`map[schema.UpdateKey]any{...}`

**关键设计决策：** `SetApplyUpdatesFn` 和 `applyUpdatesFn` 的签名**保持 `map[schema.UpdateKey]any`**，因为桥接函数连接 experience ↔ evolving 包，`evolving.ExecuteUpdates` 的签名是 `map[schema.UpdateKey]any`。如果改签名，需要修改 evolving 包接口，改动范围过大。

只在 experience 包内部的方法中使用 `map[schema.UpdateKey]schema.UpdateValue`：
- `defaultExecuteUpdates` 改为接受 `map[schema.UpdateKey]schema.UpdateValue`
- `previewApplyResults` 改为返回 `map[schema.UpdateKey]schema.UpdateValue`
- `ApplyUpdatesFromManager` 改为接受 `map[schema.UpdateKey]schema.UpdateValue`
- `evolvingExecuteUpdates` 改为接受 `map[schema.UpdateKey]schema.UpdateValue`

桥接处（`applyUpdatesFn`）的类型不变，在 `evolvingExecuteUpdates` 内部调用桥接前，需要将 `map[UpdateKey]UpdateValue` 转为 `map[UpdateKey]any`。添加辅助函数 `updatesToAnyMap`。

- [x] **Step 1: 修改 ApplyUpdatesFromManager 参数类型**

manager.go:552：`updates map[schema.UpdateKey]any` → `updates map[schema.UpdateKey]schema.UpdateValue`

- [x] **Step 2: 修改 previewApplyResults 中的构造**

manager.go:701-704：`map[schema.UpdateKey]any{...}` → `map[schema.UpdateKey]schema.UpdateValue{...}`

- [x] **Step 3: 修改 evolvingExecuteUpdates 参数类型和桥接适配**

manager.go:711-713：`updates map[schema.UpdateKey]any` → `updates map[schema.UpdateKey]schema.UpdateValue`

在调用桥接 `applyUpdatesFn(operators, updates)` 时，需要先转换为 `map[schema.UpdateKey]any`。添加辅助函数：
```go
// updatesToAnyMap 将 UpdateValue map 转换为桥接函数所需的 map[schema.UpdateKey]any 格式。
func updatesToAnyMap(updates map[schema.UpdateKey]schema.UpdateValue) map[schema.UpdateKey]any {
    result := make(map[schema.UpdateKey]any, len(updates))
    for key, value := range updates {
        result[key] = value
    }
    return result
}
```

修改 `evolvingExecuteUpdates` 中的桥接调用：`applyUpdatesFn(operators, updatesToAnyMap(updates))`

- [x] **Step 4: 修改 defaultExecuteUpdates 参数类型和简化逻辑**

manager.go:826：`updates map[schema.UpdateKey]any` → `updates map[schema.UpdateKey]schema.UpdateValue`

删除 nil 过滤分支（理由：UpdateValue 是结构体，Go map value 不能为 nil；实际调用中 UpdateValue 总有具体值；且 `evolving.ExecuteUpdates` 已有 nil 过滤保护）。简化后的 `defaultExecuteUpdates` 只做归一化后逐一应用：

```go
func defaultExecuteUpdates(
    operators map[string]operator.Operator,
    updates map[schema.UpdateKey]schema.UpdateValue,
) []schema.ApplyResult {
    var results []schema.ApplyResult
    normalized := schema.NormalizeUpdates(updatesToAnyMap(updates))
    for key, update := range normalized {
        op, ok := operators[key.OperatorID()]
        if !ok {
            results = append(results, schema.ApplyResult{
                OperatorID: key.OperatorID(),
                Target:     key.Target(),
                Applied:    false,
                Mode:       update.Mode,
                Effect:     update.Effect,
                Value:      update.Payload,
                ChangeType: update.ChangeType,
                Records:    []any{},
                Errors:     []string{fmt.Sprintf("operator not found: %s", key.OperatorID())},
                Metadata:   schema.MetadataClone(update.Metadata),
            })
            continue
        }
        results = append(results, op.ApplyUpdate(key.Target(), update))
    }
    return results
}
```

- [x] **Step 5: 搜索并修改包外调用端**

搜索整个项目中使用 `ApplyUpdatesFromManager` 的外部包。注意 `SetApplyUpdatesFn` 签名不变，所以 evolving 包的桥接代码不需要改。只需要确认外部直接调用 `ApplyUpdatesFromManager` 的地方，将 `map[UpdateKey]any` 改为 `map[UpdateKey]UpdateValue`。

```bash
cd /home/opensource/uap-claw-go && grep -rn 'ApplyUpdatesFromManager' --include='*.go' | grep -v 'experience/'
```

- [x] **Step 6: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/experience/... ./internal/evolving/...
```

---

### Task 6: B10/B11 — StageRecords/StageApplyResults 改为指针返回 + error

**Files:**
- Modify: `internal/evolving/experience/manager.go:205-239,244-275,562-571,597-631`
- Modify: `internal/evolving/experience/orchestrator.go:145-150,156,162-165`

**当前状态：**
- `StageRecords` 返回 `ExperienceApprovalRequest`（值类型，无 error）
- `StageApplyResults` 返回 `ExperienceApprovalRequest`（值类型，无 error）
- `stageRecordsInternal` 返回 `ExperienceApprovalRequest`（值类型）
- `stagePendingRequest` 返回 `ExperienceApprovalRequest`（值类型）
- orchestrator.go:145-150 调用 `StageApplyResults`，无 error 处理
- manager.go:305-319 的 `CommitProposal` 调用 `StageRecords`，无 error 处理

**修改：** 返回类型改为 `(*ExperienceApprovalRequest, error)`

- [x] **Step 1: 修改 StageRecords 返回类型**

manager.go:205-219：`ExperienceApprovalRequest` → `(*ExperienceApprovalRequest, error)`
方法体末尾改为 `return &result, nil`（如果成功）或 `return nil, err`（如果失败）

- [x] **Step 2: 修改 stageRecordsInternal 返回类型**

manager.go:562-571：`ExperienceApprovalRequest` → `(*ExperienceApprovalRequest, error)`
需要在调用 `previewApplyResults` 等间接 I/O 方法时添加 error 传播。但 `previewApplyResults` 调用 `ApplyUpdatesFromManager`（纯内存操作），不产生 I/O error。真正的 I/O 在 `stageRecordsInternal` 的上游——`ReadSkillContent` 和 `LoadFullEvolutionLog` 已在调用 StageRecords 之前完成（由 orchestrator 完成）。

因此 `stageRecordsInternal` 本身不产生 I/O error，但改为 `(*ExperienceApprovalRequest, error)` 以便后续扩展和与 StageApplyResults 统一。返回 `return &result, nil`。

- [x] **Step 3: 修改 stagePendingRequest 返回类型**

manager.go:597-631：同理改为 `(*ExperienceApprovalRequest, error)`，返回 `return &result, nil`。

- [x] **Step 4: 修改 StageApplyResults 返回类型**

manager.go:244-275：`ExperienceApprovalRequest` → `(*ExperienceApprovalRequest, error)`
返回 `return result, nil`（stagePendingRequest 已返回指针）

- [x] **Step 5: 修改 CommitProposal 适配新签名**

manager.go:305-319：`request := m.StageRecords(...)` → `request, err := m.StageRecords(...)`
添加 `if err != nil { return ExperienceApplyResult{}, err }` 处理。
`m.commitStagedRequest(ctx, request)` 中 `request` 现在是 `*ExperienceApprovalRequest`。

- [x] **Step 6: 修改 orchestrator.go 调用端**

orchestrator.go:145-150：
```go
request, err := o.manager.StageApplyResults(...)
if err != nil {
    return nil, err
}
```

orchestrator.go:156：`Request: &request` → `Request: request`（已是指针）
orchestrator.go:162-165：`request.RequestID` 已是 string（Task 1 已改），去掉解引用

- [x] **Step 7: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/experience/...
```

---

### Task 7: B4/B15/B16 — llm.Model 统一为 *llm.Model + nil 校验

**Files:**
- Modify: `internal/evolving/experience/scorer.go:24,236-258,533`

**当前状态：**
- ExperienceScorer.llm 字段：`*llm.Model`（已经是指针！scorer.go:24）
- NewExperienceScorer 参数：`llmModel *llm.Model`（已经是指针！scorer.go:237）
- UpdateLLM 参数：`llmModel *llm.Model`（已经是指针！scorer.go:533）

**发现：** scorer.go 中 llm.Model 的使用**已经全部是 `*llm.Model`**。不需要改类型。

但用户要求"换成指针后记得做好入参检验"——需要添加 nil 校验。

- [x] **Step 1: 在 NewExperienceScorer 中添加 nil 校验**

scorer.go:236-258：在构造函数开头添加：
```go
if llmModel == nil {
    return nil, fmt.Errorf("ExperienceScorer: llmModel 不能为 nil")
}
```

- [x] **Step 2: 在 UpdateLLM 中添加 nil 校验**

scorer.go:533：添加：
```go
if llmModel == nil {
    return // 或 panic，取决于设计。当前 UpdateLLM 不返回 error，用 panic 更合理
    panic("ExperienceScorer.UpdateLLM: llmModel 不能为 nil")
}
```
但 UpdateLLM 当前签名不返回 error。更安全的做法是：检查 nil 时直接不更新（跳过），或在日志中记录 error。最合理的是返回时不做任何修改——即 `if llmModel == nil { return }`。

- [x] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/experience/...
```

---

### Task 8: B14 — session → sessionID 命名统一

**Files:**
- Modify: `internal/evolving/experience/tracker.go`

**当前状态：**
- tracker.go 中变量名已经是 `sessionID`（行69,147,226,309）。
- 包级变量注释中用 "sessionID →"（行42,46）。

**发现：** tracker.go 中的变量命名**已经是 `sessionID`**。检查其他文件。

- [x] **Step 1: 搜索整个 experience 包中用 `session` 命名的变量（指代 ID 字符串的场景）**

```bash
cd /home/opensource/uap-claw-go && grep -n 'session[^ID]' internal/evolving/experience/*.go | grep -v '_test.go' | grep -v 'sessionID'
```

如果没有发现需要重命名的 `session` 变量（即所有已经是 `sessionID`），则此 Task 标记完成。

---

### Task 9: 全量编译验证 + 外部调用端适配

**Files:**
- 搜索整个项目中引用 experience 包中已修改类型/签名的代码

**需要检查的外部包：**
- `internal/evolving/` 包中调用 `SetApplyUpdatesFn`、`StageApplyResults` 的地方
- 其他包中引用 `map[schema.UpdateKey]any` 的地方
- `HostFacingExperienceResult` 工厂方法的调用端

- [x] **Step 1: 搜索整个项目中对已修改 API 的引用**

```bash
cd /home/opensource/uap-claw-go && grep -rn 'map\[schema\.UpdateKey\]any' --include='*.go' | grep -v '_test.go'
grep -rn 'StageApplyResults\|StageRecords\|SetApplyUpdatesFn\|ApplyUpdatesFromManager' --include='*.go' | grep -v 'experience/'
grep -rn 'HostFacingExperienceResultPendingApproval\|HostFacingExperienceResultPersisted\|HostFacingExperienceResultRejected' --include='*.go' | grep -v 'experience/'
grep -rn 'requestID \*string' --include='*.go' | grep -v 'experience/'
```

- [x] **Step 2: 适配所有外部调用端**

根据搜索结果逐一修改。预计涉及：
- `internal/evolving/` 包中的桥接代码（SetApplyUpdatesFn 的调用端需要改为 `map[schema.UpdateKey]schema.UpdateValue`）
- 其他可能的调用端

- [x] **Step 3: 全量编译**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

- [x] **Step 4: 运行现有测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/evolving/experience/... -v -count=1
```

---

### Task 10: B1 — 测试拆分为 7 个独立测试文件

**Files:**
- Delete: `internal/evolving/experience/experience_test.go`
- Create: `internal/evolving/experience/types_test.go`
- Create: `internal/evolving/experience/lifecycle_test.go`
- Create: `internal/evolving/experience/common_test.go`
- Create: `internal/evolving/experience/scorer_test.go`
- Create: `internal/evolving/experience/tracker_test.go`
- Create: `internal/evolving/experience/manager_test.go`
- Create: `internal/evolving/experience/orchestrator_test.go`

**拆分原则：** 每个测试文件只测试对应源码文件中定义的类型/函数。

**当前 experience_test.go 的测试分配：**

| 测试名 | 对应源码文件 | 目标 _test.go |
|--------|-------------|---------------|
| TestExperienceProposal_RecordCount | types.go | types_test.go |
| TestExperienceApplyResult_Ok | types.go | types_test.go |
| TestExperienceApprovalRequest_ToHostResult | types.go | types_test.go |
| TestExperienceApplyResult_ToHostResult | types.go | types_test.go |
| TestOnlineEvolutionStatus | types.go | types_test.go |
| TestHostFacingExperienceResult_PendingApproval | lifecycle.go | lifecycle_test.go |
| TestHostFacingExperienceResult_Persisted | lifecycle.go | lifecycle_test.go |
| TestHostFacingExperienceResult_Rejected | lifecycle.go | lifecycle_test.go |
| TestStrPtr | lifecycle.go | lifecycle_test.go（如果 strPtr 被删除则删除此测试） |
| TestCalcEffectiveness | scorer.go | scorer_test.go |
| TestCalcUtilization | scorer.go | scorer_test.go |
| TestCalcFreshness | scorer.go | scorer_test.go |
| TestCalcScore | scorer.go | scorer_test.go |
| TestUpdateScore | scorer.go | scorer_test.go |
| TestParseLLMJSON | scorer.go | scorer_test.go |
| TestFormatPresentedExperiences | scorer.go | scorer_test.go |
| TestFormatScoredExperiences | scorer.go | scorer_test.go |
| TestTruncateString | scorer.go | scorer_test.go |
| TestGetBoolFromEvalResult | scorer.go | scorer_test.go |
| TestNewExperienceScorer | scorer.go | scorer_test.go |
| TestUpdateLLM | scorer.go | scorer_test.go |
| TestMakePendingChange | common.go | common_test.go |
| TestRejectPendingChange | common.go | common_test.go |
| TestGenerateShortID | common.go | common_test.go |
| TestFilterBodyRecords | common.go | common_test.go |
| TestIsBodyRecord | common.go | common_test.go |
| TestCommitPendingChange | common.go | common_test.go |
| TestExecuteSimplifyActions (无) | common.go | common_test.go（当前无此测试） |
| TestGetStrFromAny | common.go | common_test.go |
| TestGetStrSliceFromAny | common.go | common_test.go |
| TestGetFloatPtrFromAny | common.go | common_test.go |
| TestExperienceTracker_ConsumeEvalState | tracker.go | tracker_test.go |
| TestExperienceTracker_ClearSession | tracker.go | tracker_test.go |
| TestNewExperienceTracker | tracker.go | tracker_test.go |
| TestParseTimestamp | tracker.go | tracker_test.go |
| TestNewExperienceManager | manager.go | manager_test.go |
| TestExperienceManager_Properties | manager.go | manager_test.go |
| TestExperienceManager_BindPendingApprovalSnapshots | manager.go | manager_test.go |
| TestExperienceManager_RejectSimplify | manager.go | manager_test.go |
| TestExperienceManager_getRebuildTemplate | manager.go | manager_test.go |
| TestExperienceManager_getDefaultRebuildIntent | manager.go | manager_test.go |
| TestToApplyResult | manager.go | manager_test.go |
| TestSetApplyUpdatesFn | manager.go | manager_test.go |
| TestDefaultExecuteUpdates | manager.go | manager_test.go |
| TestBuildLocalApplyPreview | manager.go | manager_test.go |
| TestFormatEvolutionRecords | manager.go | manager_test.go |
| TestNewOnlineEvolutionOrchestrator | orchestrator.go | orchestrator_test.go |
| TestGetPreferredSignal | orchestrator.go | orchestrator_test.go |
| TestGetSignalType | orchestrator.go | orchestrator_test.go |
| TestGetSignalSource | orchestrator.go | orchestrator_test.go |

- [x] **Step 1: 创建 7 个测试文件，按上表分配测试函数**

每个文件包含对应源码文件的测试函数。同时需要根据 Task 1-7 的修改更新测试代码（如 requestID 类型、PendingGovernanceEntry 类型、RecordScoreUpdate 类型等）。

- [x] **Step 2: 删除 experience_test.go**

```bash
rm /home/opensource/uap-claw-go/internal/evolving/experience/experience_test.go
```

- [x] **Step 3: 更新测试代码适配所有修改**

重点修改：
- lifecycle_test.go：requestID 从 `*string` 改为 `string`，去掉 `&reqID`
- manager_test.go：pendingGovernance 从 `map[string]map[string]any` 改为 `map[string]*PendingGovernanceEntry`，SetApplyUpdatesFn 从 `map[UpdateKey]any` 改为 `map[UpdateKey]UpdateValue`，defaultExecuteUpdates 适配
- scorer_test.go：NewExperienceScorer nil 校验测试

- [x] **Step 4: 运行所有测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/evolving/experience/... -v -count=1
```

- [x] **Step 5: 全量编译**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

---

### Task 11: 全量测试 + 提交

- [x] **Step 1: 运行全量测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/evolving/... -count=1
```

- [x] **Step 2: 运行全量编译**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

- [x] **Step 3: 提交**

```bash
cd /home/opensource/uap-claw-go && git add -A && git commit -m "refactor(experience): 审查决策实施 — requestID统一string、StageRecords指针+error、PendingGovernance结构体、RecordScoreUpdate结构体、UpdateKey→UpdateValue、llmModel nil校验、测试拆分7文件"
```
