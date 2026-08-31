# 48h 逻辑审查报告（2026-08-31）

> 审查范围：48 小时内提交记录涉及的章节，对比 Python 参考项目分析 Go 移植过程中的逻辑差异和遗漏。

---

## 审查范围

### 48h 内提交记录

共 57 个提交，涉及以下关键章节：

| 章节 | 内容 | 状态 |
|------|------|------|
| 7.7 | SummaryManager / VariableManager / KvPrefixRegistry | ✅ |
| 7.8 | WriteManager / SearchManager / MemUpdateChecker / PromptApplier | ✅ |
| 9.65a-5 | SQL 后端（SqlTeamDatabase + 4 个 SQL DAO） | ✅ |
| 9.72b | ToolOptimizer（beam_search/schema_extractor/customized_reviewer） | ✅ |
| 9.72c | MemoryOptimizerBase（Backward/Step 对齐） | ✅ |
| 9.72d | SkillExperienceOptimizer（EvolutionStoreReader 迁移） | ✅ |
| 9.72e | BaseOptimizer Backward/Step 模板方法对齐 | ✅ |
| 6.19 | IntentRecognizer.Recognize() LLM function calling 回填 | ✅ |
| 10.3.15-18 | 会话管理修复（TruncateHistoryRecords / readHistoryFile / AutoTitle） | ✅ |
| 10.6.4 | AvatarPromptRail 数字分身 Rail | ✅ |
| 10.6.17 | GetForbiddenMemoryPrompt 记忆禁止配置 | ✅ |
| signal | isResultUsable 严格类型检查 | ✅ |
| sessionctx | 提取独立子包 + SQL DAO GORM 结构体映射 | ✅ |

---

## 问题汇总

| 编号 | 章节 | 严重程度 | 简述 |
|------|------|---------|------|
| S01 | 6.19 | 🔴 严重 | IntentToolkits 返回值字符串翻译为中文，违反一比一复刻规则 |
| S02 | 9.65a-5 | 🔴 严重 | 动态表 DDL 缺少所有索引，大数据量查询性能严重退化 |
| S03 | 9.65a-5 | 🔴 严重 | `assignee` 列 nullable 语义不一致（NOT NULL DEFAULT '' vs Optional NULL） |
| S04 | 9.65a-5 | 🔴 严重 | `to_member_name` 列 nullable 语义不一致 |
| S05 | 9.65a-5 | 🔴 严重 | `AddTaskWithBidirectionalDependencies` 缺少反向依赖参数 `dependent_task_ids` |
| S06 | 9.65a-5 | 🔴 严重 | `MarkMessageRead` 缺少 `"user"` 伪成员检查和成员存在性检查 |
| S07 | 7.8 | 🔴 严重 | MemUpdateChecker.formatInput 排序方式与 Python 不一致 |
| S08 | 7.8 | 🔴 严重 | MemUpdateChecker CONFLICTING 缺少 old_memories 存在性检查 |
| M01 | 7.8 | 🟡 一般 | SearchManager.ListUserMem 返回值缺少 fields 展开 |
| M02 | 7.8 | 🟡 一般 | SearchManager.ListUserProfile/ListUserSummary batchSize=0 可能异常 |
| M03 | 7.7 | 🟡 一般 | VariableManager.QueryVariable 缺少 TrimSpace 空白字符检查 |
| M04 | 10.3.15-18 | 🟡 一般 | AppendHistoryRecord 元数据联动改为异步 goroutine |
| M05 | 10.3.15-18 | 🟡 一般 | SetSessionDeliveryContext 同步写盘 vs Python 异步队列 |
| M06 | 9.72e | 🟡 一般 | BaseOptimizer.step() error 路径不清空 trajectories |
| M07 | 10.6.4 | 🟡 一般 | AvatarPromptRail.injectForbiddenMemory 缺少异常捕获 |
| M08 | 9.65a-5 | 🟡 一般 | WithTx 返回具体类型而非接口 |
| M09 | 9.65a-5 | 🟡 一般 | HasUnreadMessages 语义差异（MAX(read_at) 聚合 vs per-member 逐一） |
| M10 | 9.65a-5 | 🟡 一般 | UpdateTask 无法将 title 设为空字符串 |
| M11 | 9.65a-5 | 🟡 一般 | detectCycleInAdjacencySQL 递归 DFS 可能栈溢出 |
| M12 | 6.19 | 🟡 一般 | Python get_openai_tool_schemas Bug — Go 已修复，行为与 Python 不同 |
| T01 | 10.3.15-18 | 🔵 提示 | AutoTitle 多字节字符按字节截断而非按字符 |
| T02 | 10.3.15-18 | 🔵 提示 | readTeamHistoryRecords 重试耗尽日志缺少 file_size |
| T03 | 10.6.4 | 🔵 提示 | AvatarPromptRail rejectTool 使用 string 而非 map[string]any |
| T04 | 7.7 | 🔵 提示 | SummaryManager 返回 nil vs 空 slice |
| T05 | 9.72b | 🔵 提示 | ToolOptimizerBase 未设置 Mixin.defaultTargets |
| T06 | 9.72b | 🔵 提示 | OptimizeTool result_descs 索引注释与代码矛盾 |

---

## 严重问题详细分析

### S01：IntentToolkits 返回值字符串翻译为中文

**章节**：6.19 IntentRecognizer

**Python 样例**：
```python
# intent_toolkits.py:266-267
), (f"Task ID: {target_task_id}, Task Description: {task_description}, "
    f"Current Status: Created and submitted for execution")

# intent_toolkits.py:247-250
clarification_prompt="Sorry, I couldn't understand your meaning. "
                     "Please clarify whether you want to create a new "
                     "task or modify an existing one.",
), f"Automatically converted to unknown_task due to low confidence"

# intent_toolkits.py:381
), f"Task supplementary information submitted."
```

**Go 问题**：
```go
// intent_toolkits.go:63
result := fmt.Sprintf("任务 ID: %s, 任务描述: %s, 当前状态: 已创建并提交执行", targetTaskID, taskDescription)

// intent_toolkits.go:211
result := "任务补充信息已提交。"

// intent_toolkits.go lowConfidenceIntent()
result := "由于置信度较低，自动转换为 unknown_task"
// clarification_prompt 也翻译为中文
```

**修复方案**：所有 IntentToolkits 方法返回值字符串和 `lowConfidenceIntent` 的 `clarification_prompt` 必须使用英文原文，一比一复刻 Python。受影响的方法共 8 个：`CreateTask`/`PauseTask`/`CancelTask`/`ResumeTask`/`UnknownTask`/`CreateDependentTask`/`ModifyTask`/`SupplementTask` + `lowConfidenceIntent`。

**影响**：这些字符串作为 ToolMessage 内容返回给 LLM，改变语言会干扰 LLM 的意图理解流程。Python 侧这些是英文，Go 应保持一致。

---

### S02：动态表 DDL 缺少所有索引

**章节**：9.65a-5 SQL 后端

**Python 样例**：
```python
# task_dao.py — TeamTaskBase 定义
team_name: str = Field(index=True)       # 索引
status: str = Field(index=True)          # 索引
assignee: Optional[str] = Field(index=True, nullable=True)  # 索引
updated_at: int = Field(index=True)      # 索引

# message_dao.py — TeamMessageBase 定义
timestamp: int = Field(index=True)       # 索引
broadcast: bool = Field(index=True)      # 索引
is_read: Optional[bool] = Field(index=True)  # 索引
resolved: Optional[bool] = Field(index=True) # 索引
to_member_name: Optional[str] = Field(index=True, nullable=True)  # 索引
read_at: Optional[int] = Field(index=True)  # 索引
```

**Go 问题**：
```go
// sql_engine.go — createSessionTablesDDL 只有 PRIMARY KEY，没有任何额外索引
// 例：team_task 表
CREATE TABLE IF NOT EXISTS %s (
    task_id TEXT PRIMARY KEY,
    team_name TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'created',
    assignee TEXT NOT NULL DEFAULT '',    -- 缺少索引
    updated_at INTEGER NOT NULL DEFAULT 0 -- 缺少索引
)
```

**修复方案**：在 `createSessionTablesDDL` 中为以下列添加索引：
- `team_task`：`(team_name)`, `(status)`, `(assignee)`, `(updated_at)`
- `team_task_dependency`：`(team_name)`, `(resolved)`
- `team_message`：`(team_name)`, `(timestamp)`, `(broadcast)`, `(is_read)`, `(to_member_name)`
- `message_read_status`：`(read_at)`

---

### S03/S04：assignee / to_member_name nullable 语义不一致

**章节**：9.65a-5 SQL 后端

**Python 样例**：
```python
# TeamTaskBase
assignee: Optional[str] = Field(index=True, nullable=True)  # 可为 NULL

# TeamMessageBase
to_member_name: Optional[str] = Field(index=True, nullable=True)  # 可为 NULL
```

**Go 问题**：
```go
// DDL 中 assignee 和 to_member_name 均为 NOT NULL DEFAULT ''
assignee TEXT NOT NULL DEFAULT ''
to_member_name TEXT NOT NULL DEFAULT ''

// GORM 结构体
Assignee      string `gorm:"column:assignee"`       // 零值为 ""，无法区分"未分配"和"空名称"
ToMemberName  string `gorm:"column:to_member_name"` // 同上
```

**修复方案**：
1. DDL 改为 `assignee TEXT` / `to_member_name TEXT`（去掉 NOT NULL DEFAULT ''）
2. GORM 结构体改为指针类型 `*string` 或使用 `sql.NullString`
3. 业务逻辑中 `WHERE assignee IS NULL` 查找未分配任务，`WHERE to_member_name IS NULL` 标识广播消息

**流程示例**：
```
Python: create_task(status="created") → assignee=NULL
  → ClaimTask: UPDATE SET assignee='alice' WHERE task_id=? AND assignee IS NULL
  → UnassignTask: UPDATE SET assignee=NULL

Go 当前: create_task(status="created") → assignee=""
  → ClaimTask: WHERE assignee = '' (但空名成员也用 "" 会导致冲突)
  → UnassignTask: 无法设回 NULL（只能设 ""）
```

---

### S05：AddTaskWithBidirectionalDependencies 缺少反向依赖参数

**章节**：9.65a-5 SQL 后端

**Python 样例**：
```python
# task_dao.py:643-659
async def add_task_with_bidirectional_dependencies(
    self, task_id, team_name, title, content, status, *,
    dependencies: Optional[List[str]] = None,       # 正向：我依赖谁
    dependent_task_ids: Optional[List[str]] = None,  # 反向：谁依赖我
) -> bool:
    edges = []
    for dep_id in dependencies or ():
        edges.append((task_id, dep_id))        # task → dep
    for dependent_id in dependent_task_ids or ():
        edges.append((dependent_id, task_id))  # dependent → task
```

**Go 问题**：
```go
// database.go:119
AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string,
    task *TeamTaskBase, dependsOnIDs []string) GraphMutationResult
// 只有 dependsOnIDs（正向依赖），缺少 dependentTaskIDs（反向依赖）
```

**修复方案**：接口签名添加 `dependentTaskIDs []string` 参数，`MutateDependencyGraph` 调用中追加反向边 `(dependentID → taskID)`。

---

### S06：MarkMessageRead 缺少 "user" 伪成员检查和成员存在性检查

**章节**：9.65a-5 SQL 后端

**Python 样例**：
```python
# message_dao.py mark_message_read
# 1. 检查成员存在性
member = await self.get_member(team_name=team_name, member_name=member_name)
if not member:
    return False

# 2. "user" 伪成员不能标记广播消息已读
if msg.broadcast and member_name == "user":
    return False
```

**Go 问题**：
```go
// sql_message_dao.go:192-220
func (d *SQLMessageDao) MarkMessageRead(ctx context.Context, messageID, memberName string) bool {
    // 无成员存在性检查
    // 无 "user" 伪成员检查
    if !msg.Broadcast {
        d.db.WithContext(ctx).Table(msgTable).Where("message_id = ?", messageID).Update("is_read", 1)
    } else {
        // 广播消息：任何 memberName（包括 "user"）都可以更新 read_status
        d.db.WithContext(ctx).Exec("INSERT INTO %s ...", memberName, ...)
    }
    return true
}
```

**修复方案**：
1. 在 MarkMessageRead 开头添加成员存在性查询
2. 广播消息路径添加 `if memberName == "user" { return false }` 检查

---

### S07：MemUpdateChecker.formatInput 排序方式与 Python 不一致

**章节**：7.8 MemUpdateChecker

**Python 样例**：
```python
# mem_update_checker.py _format_input
def _format_input(new_memories, old_memories):
    new_info_lines = [f"{k}: {v}" for k, v in new_memories.items()]
    old_info_lines = [f"{k}: {v}" for k, v in old_memories.items()]
    # new 倒序（按 dict 插入顺序的反序），old 不排序
    return "\n".join(new_info_lines[::-1]), "\n".join(old_info_lines)
```

**Go 问题**：
```go
// update_checker.go:260-289 formatInput
// 新记忆：收集行后按字典序倒序排列
sort.Sort(sort.Reverse(sort.StringSlice(newLines)))
// 旧记忆：按字典序正序排列
sort.Strings(oldLines)
```

**修复方案**：Go map 遍历顺序不确定，需要修改接口签名或调用方保证输入顺序：
- 方案 A：将 `newMemories` 和 `oldMemories` 改为有序结构（如 `[]struct{ID, Content string}`）
- 方案 B：在 `FragmentMemoryManager.addMemories` 构建输入时保持调用方的插入顺序

**流程示例**：
```
Python 调用方: new_memories = {"m1": "A", "m2": "B", "m3": "C"}
  → _format_input 输出: "m3: C\nm2: B\nm1: A" (插入顺序倒序)

Go 调用方: newMemories = map[string]string{"m1":"A", "m2":"B", "m3":"C"}
  → formatInput 输出: "m3: C\nm2: B\nm1: A" (字典序倒序，恰好一致)
  但如果 key 不是自然序: {"b1":"A", "a2":"B"}
  → Python: "a2: B\nb1: A" (插入倒序)
  → Go: "b1: A\na2: B" (字典序倒序) ← 不一致！
```

---

### S08：MemUpdateChecker CONFLICTING 缺少 old_memories 存在性检查

**章节**：7.8 MemUpdateChecker

**Python 样例**：
```python
# mem_update_checker.py check()
for old_id, old_content in check_item.related_infos.items():
    if old_id in old_memories:  # 只删除确实存在的旧记忆
        action_items.append(MemoryActionItem(id=old_id, content=old_content, status=MemoryStatus.DELETE))
```

**Go 问题**：
```go
// update_checker.go:319-325 mapCheckItemsToActionItems
for oldID, oldContent := range item.RelatedInfos {
    // 无条件添加 DELETE，未检查 old_id 是否在 oldMemories 中
    actionItems = append(actionItems, &MemoryActionItem{
        ID:      oldID,
        Content: oldContent,
        Status:  MemoryStatusDelete,
    })
}
```

**修复方案**：`mapCheckItemsToActionItems` 需要接收 `oldMemories` 参数，在添加 DELETE 前检查 `if _, ok := oldMemories[oldID]; ok`。

---

## 一般问题详细分析

### M01：SearchManager.ListUserMem 返回值缺少 fields 展开

**章节**：7.8 SearchManager

**Python**：`list_user_mem()` 返回 `list[dict]`，每项含 `id/user_id/scope_id/mem/mem_type/timestamp` + `**res.fields`（fields 展开到 dict 顶层，如 `source_id` 在顶层可直接访问）

**Go**：返回 `[]*storeindex.MemoryDoc`，`source_id` 等字段在嵌套的 `Fields` map 中，调用方需 `doc.Fields["source_id"]` 而非 `doc.SourceID`

**修复方案**：确认上层调用是否依赖 fields 展开到顶层。如依赖，在 `ListUserMem` 返回前将 `MemoryDoc.Fields` 的常用字段展开到结果结构体中。

---

### M02：SearchManager batchSize=0 可能异常

**章节**：7.8 SearchManager

**Python**：`list_fragment_memories(user_id=user_id, scope_id=scope_id)` — 使用默认 `batch_size=100`

**Go**：`fm.ListFragmentMemories(ctx, userID, scopeID, 0, 0, "")` — `batchSize=0`

**修复方案**：确认 `ListFragmentMemories` 对 `batchSize=0` 的处理。如果 0 表示无限制，则行为与 Python 默认 100 不一致。应改为传 `100` 或在 `ListFragmentMemories` 中将 0 作为默认值处理为 100。

---

### M03：VariableManager.QueryVariable 缺少 TrimSpace

**章节**：7.7 VariableManager

**Python**：`if not name or not name.strip():` — name 为空白时走前缀查询

**Go**：`if name == ""` — 只检查空字符串，`"   "` 会走按名称查询

**修复方案**：改为 `if strings.TrimSpace(name) == ""`。

---

### M04：AppendHistoryRecord 元数据联动异步化

**章节**：10.3.15-18

**Python**：`append_history_record` 内同步调用 `update_session_metadata` 和 `set_session_delivery_context`

**Go**：使用 `go func()` 异步调用，有 `defer recover()` 防御

**修复方案**：当前设计是有意的优化选择，但需确保极端场景下元数据更新延迟不影响主流程。可考虑在 `FlushHistoryQueue` 中等待异步联动完成。

---

### M05：SetSessionDeliveryContext 同步写盘

**章节**：10.3.15-18

**Python**：`set_session_delivery_context` 通过 `_enqueue_write` 异步写入

**Go**：直接 `WriteSessionMetadata` 同步写入

**修复方案**：考虑增加写入队列统一异步化，避免写入密集场景下性能问题。

---

### M06：BaseOptimizer.step() error 路径不清空 trajectories

**章节**：9.72e BaseOptimizer

**Python**：`step()` 的 `except` 块中先 `self.clear_trajectories()` 再 `raise`

**Go**：`Step()` 只在成功路径调用 `ClearTrajectories()`，error 路径不清空

**修复方案**：在 `Step()` 中使用 `defer b.ClearTrajectories()` 确保无论成功或失败都清空轨迹。

---

### M07：AvatarPromptRail.injectForbiddenMemory 缺少异常捕获

**章节**：10.6.4

**Python**：`try/except` 包裹 `get_forbidden_memory_prompt`，失败时 `logger.debug` 不中断

**Go**：无异常捕获，如果 `GetForbiddenMemoryPrompt` panic 则整个 BeforeModelCall 崩溃

**修复方案**：虽然 `GetForbiddenMemoryPrompt` 内部不会 panic，但为防御性编程，可在 `injectForbiddenMemory` 中添加 `defer func() { if r := recover(); r != nil { ... } }()` 或在调用处用 `recover` 包裹。

---

### M08-M11：9.65a-5 SQL 后端一般问题

| 编号 | 问题 | 修复方案 |
|------|------|---------|
| M08 | WithTx 返回具体类型而非接口 | 改为返回接口类型 `TeamDao`/`MemberDao`/`TaskDao`/`MessageDao` |
| M09 | HasUnreadMessages 语义差异 | Go 用 `MAX(read_at)` 聚合，Python 按 per-member 逐一判断。大数据量下 Go 更高效但需确认等价性 |
| M10 | UpdateTask 无法将 title 设为空字符串 | 改用 `*string` 指针参数区分 nil（不修改）和空串（修改为空） |
| M11 | detectCycleInAdjacencySQL 递归 DFS 可能栈溢出 | 改为迭代 DFS（显式栈），与 Python 一致 |

---

### M12：Python get_openai_tool_schemas Bug

**章节**：6.19 IntentToolkits

**Python Bug**：
```python
# intent_toolkits.py:390-392
def get_openai_tool_schemas(self, choices: List[str] = None) -> List[Dict]:
    if not choices:
        return list(self._tool_schema_choices.values())
    return [self._tool_schema_choices[k] for k in self._tool_schema_choices.keys()]
    # Bug: 遍历的是 self._tool_schema_choices.keys() 而非 choices
```

**Go 已修复**：
```go
// intent_toolkits.go GetOpenAIToolSchemas
for _, c := range choices {
    if v, ok := t.toolSchemaChoices[c]; ok {
        result = append(result, v)
    }
}
```

**修复方案**：Go 保持当前正确实现，Python 侧应修复此 Bug。记录为已知差异。

---

## 提示问题

| 编号 | 问题 | 说明 |
|------|------|------|
| T01 | AutoTitle 多字节字符截断 | `len(title)` 按字节截取，中文可能截断到无效 UTF-8。改用 `utf8.RuneCountInString` + 按 rune 截取 |
| T02 | readTeamHistoryRecords 日志缺 file_size | 排查时信息不足，建议添加 |
| T03 | rejectTool 返回 string 而非 map | 与其他 Go Rail 的 `map[string]any{"error": msg}` 惯例不一致，功能正确 |
| T04 | SummaryManager 返回 nil vs 空 slice | Go 惯例 nil/空 slice 等价，但调用方需注意 |
| T05 | ToolOptimizerBase 未设 Mixin.defaultTargets | 建议在 `NewToolOptimizerBase` 中设置 |
| T06 | OptimizeTool result_descs 索引注释与代码矛盾 | 注释说取 `[0]`，代码取 `[len-1]`，最终输出一致，建议统一 |

---

## ⤵️ 回填标记审计

### 已实现但标记未移除（3 处）

| 文件 | 位置 | 内容 |
|------|------|------|
| `agent_teams/agent/stream_controller.go:44` | ⤵️ wakeMailboxCb | ✅ `WithWakeMailbox` 已实现 |
| `agent_teams/agent/stream_controller.go:46-47` | ⤵️ requestCompletionPollCb | ✅ `WithRequestCompletionPoll` 已实现 |
| `agent_teams/agent/stream_controller.go:61-63` | ⤵️ pendingInterruptResumes 类型 | ✅ 类型已定义为 `[]any` |

**建议**：移除上述 3 处过时的 ⤵️ 标记。

### 关键未实现回填项（按阻塞链）

| 阻塞链 | ⤵️ 数量 | 依赖章节 |
|--------|--------|---------|
| 9.38-49 Playwright MCP | ~20 处 | BrowserAgent 端到端集成 |
| 9.55 TeamAgent 完善 | ~15 处 | TeamAgent 核心方法 |
| 9.62 CoordinationKernel | ~5 处 | 协调内核 |
| 10.5.7-10 Extensions | ~15 处 | Go 插件加载机制 |
| 8.7 Graph Store | ~12 处 | 图存储 |
| 7.2 MemberMemoryToolkit | ~10 处 | 记忆工具集 |
| Runner 分布式 | ~12 处 | TeamRunner / RemoteAgent |
| 8.15 WorkflowConfig | ~4 处 | 工作流配置 |

---

## 审查总结

### 按严重程度

| 严重程度 | 数量 | 编号 |
|---------|------|------|
| 🔴 严重 | 8 | S01-S08 |
| 🟡 一般 | 12 | M01-M12 |
| 🔵 提示 | 6 | T01-T06 |

### 按章节

| 章节 | 严重 | 一般 | 提示 |
|------|------|------|------|
| 6.19 IntentRecognizer | 1 | 1 | 0 |
| 7.7 SummaryManager/VariableManager | 0 | 1 | 1 |
| 7.8 WriteManager/SearchManager/MemUpdateChecker | 2 | 2 | 0 |
| 9.65a-5 SQL 后端 | 4 | 5 | 0 |
| 9.72b ToolOptimizer | 0 | 0 | 2 |
| 9.72e BaseOptimizer | 0 | 1 | 0 |
| 10.3.15-18 会话管理 | 0 | 2 | 2 |
| 10.6.4 AvatarPromptRail | 0 | 1 | 1 |
| 10.6.17 Forbidden Memory | 0 | 0 | 0 |

### 优先修复建议

1. **S01 IntentToolkits 字符串** — 违反一比一复刻规则，影响 LLM 行为，修复简单
2. **S03/S04 nullable 语义** — 影响 ClaimTask/UnassignTask 等核心流程，修复需改 DDL+结构体+业务逻辑
3. **S07/S08 MemUpdateChecker** — 影响记忆冲突检查结果正确性
4. **S02 DDL 索引** — 影响生产性能，修复简单（只改 DDL）
5. **S05 双向依赖** — 影响任务依赖图构建
6. **S06 MarkMessageRead** — 影响消息已读标记正确性
