# 48 小时逻辑审查报告 (2026-09-01)

> 审查范围：2026-08-30 ~ 2026-09-01 提交
> 审查方法：对照 Python 参考项目，逐方法/步骤对比 Go 移植实现的逻辑一致性

## 审查覆盖章节

48 小时内完成的实现计划章节：

| 章节 | 内容 | 提交数 |
|------|------|--------|
| 7.7 | SummaryManager / VariableManager / KvPrefixRegistry | 1 |
| 7.8 | WriteManager / SearchManager / MemUpdateChecker / PromptApplier | 8+ |
| 10.6.4 | AvatarPromptRail 数字分身 Rail | 2 |
| 10.6.17 | Forbidden Memory 禁止记忆配置与提示词 | 2 |
| 9.65a-5 | SQL 后端（SqlTeamDatabase + 4 个 SQL DAO） | 1 |
| 6.19/6.23 | IntentRecognizer LLM function calling 回填 | 1 |
| 9.70/9.72a-e | 优化器 Backward/Step 模板方法对齐 | 2 |
| 10.3.15-18 | 会话管理修复（TruncateHistoryRecords/readHistoryFile/AutoTitle） | 3 |
| 多项 fix | isResultUsable/OwnerScopes/generateToolID/i18n/AddInstalledPlugin 等 | 10+ |
| sessionctx | 提取独立子包 + SQL DAO 改用 GORM | 1 |

---

## 问题汇总

| # | 严重度 | 章节 | 问题简述 |
|---|--------|------|---------|
| 1 | 🔴严重 | 6.19 | processModifyTaskIntent 用 TargetTaskID（新生成 UUID）查找旧任务，应使用 DependTaskID[0] |
| 2 | 🔴严重 | 9.70 | 缺少 BaseOptimizerMixin 层 Backward/Step 模板方法，5 个优化器缺失错误包装 |
| 3 | 🔴严重 | 9.72d | SkillExperienceOptimizer.AddTrajectory 签名参数类型错误 |
| 4 | 🔴严重 | 10.3.15 | AutoTitle UTF-8 按字节截断，中文约 16 字符就截断（Python 50 字符） |
| 5 | 🔴严重 | 10.3.15 | ApplySessionRename UTF-8 按字节截断，可能截断在多字节中间产生无效 UTF-8 |
| 6 | 🔴严重 | 10.1.8 | PermissionContext.Scene() 优先级顺序与 Python 相反（先 web 后 group_digital_avatar） |
| 7 | 🔴严重 | 10.1.8 | PermissionContext.OwnerScopeKey() 缺少 TrimSpace |
| 8 | 🔴严重 | 7.8(lite) | runChecker 未真正调用 MemUpdateChecker，始终返回 nil，导致无 LLM 时所有相似文件被误报冲突 |
| 9 | 🔴严重 | 9.65a-5 | SQL 动态表 DDL 缺少索引，查询性能严重退化 |
| 10 | 🔴严重 | 9.65a-5 | CreateMessage 不区分 IntegrityError 和 OperationalError，主键冲突时应直接返回 False |
| 11 | 🟡一般 | 7.8 | MemUpdateChecker.mapCheckItemsToActionItems 中 CONFLICTING 分支缺少 `old_id in old_memories` 校验 |
| 12 | 🟡一般 | 7.7 | VariableManager.QueryVariable name 纯空白字符串处理缺失 |
| 13 | 🟡一般 | 7.7 | VariableManager 缺少 LEGACY_PREFIXES 和循环注册逻辑 |
| 14 | 🟡一般 | 7.7 | NewVariableManager 忽略 RegisterCurrent 返回的 error |
| 15 | 🟡一般 | 9.72b | ToolOptimizerBase.RequiresForwardData() Go=false Python=隐式True |
| 16 | 🟡一般 | 6.19 | IntentToolkits result 字符串中文化，违反"提示词一比一复刻"规则 |
| 17 | 🟡一般 | 10.6.17 | memory.forbidden.get/set RPC 为 stub，Python 为完整实现 |
| 18 | 🟡一般 | 10.6.17 | config.set forbidden description 合并语义需验证 |
| 19 | 🟡一般 | 9.72d | TeamSkillExperienceOptimizer._build_frontmatter() 未实现 |
| 20 | 🟡一般 | 9.65a-5 | AddTaskWithBidirectionalDependencies 缺少 dependentTaskIDs 参数 |
| 21 | 🟡一般 | 9.65a-5 | MarkMessageRead SQL 版缺少成员存在性检查（InMemory 版有） |
| 22 | 🟡一般 | 9.65a-5 | HasUnreadMessages 广播未读检测逻辑与 Python 不等价 |
| 23 | 🟡一般 | 8.35 | BuildTeam Leader role 差异：Go 传 TeamRoleLeader，Python 未传（默认 TEAMMATE） |
| 24 | 🔵提示 | 6.19 | processUnknownTaskIntent 使用 context.Background() 而非传入 ctx |
| 25 | 🔵提示 | 6.19 | processSupplementTaskIntent 吞掉 pause 错误，与 Python 不一致 |
| 26 | 🔵提示 | 9.70 | Step 无论成功失败都应 ClearTrajectories，Go 仅成功路径清理 |
| 27 | 🔵提示 | 9.72d | TeamSkillExperienceOptimizer._dump_raw() 仅记日志，Python 写文件 |
| 28 | 🔵提示 | 7.8 | MemUpdateChecker 缺少 processed_new_ids 追踪（Python 有，Go 无） |
| 29 | 🔵提示 | 7.8 | formatInput 排序方式差异：Go 按字符串排序，Python 按 dict 插入顺序 |
| 30 | 🔵提示 | 10.6.17 | description 无硬编码默认值 |
| 31 | 🔵提示 | 9.72a | isResultUsable recover 路径未设置 lastError |
| 32 | 🔵提示 | 9.65a-5 | CreateMessage 退避策略：Go 固定 100/300/500ms，Python 指数退避 |
| 33 | 🔵提示 | 9.65a-5 | 环检测使用递归 DFS，深层依赖链可能栈溢出 |

---

## 严重问题详细分析

### 问题 1：processModifyTaskIntent 用 TargetTaskID 查找旧任务

**章节**：6.19 IntentRecognizer
**Go 文件**：`internal/agentcore/controller/modules/event_handler.go`
**Python 文件**：`openjiuwen/core/controller/modules/event_handler.py`

**Python 样例**：
```python
# modify_task 方法中：
target_task_id = str(uuid.uuid4())  # 新生成 UUID
depend_task_id = [task_id]          # 原始 task_id

# _process_modify_task_intent 中：
intent = intents[0]
tasks = await self.task_manager.get_task(TaskFilter(task_id=intent.target_task_id))
# ↑ Python 自己也有这个 bug，用新生成的 UUID 查找旧任务
```

**Go 问题**：
```go
// ModifyTask 中：
targetTaskID := generateUUID()     // 新生成 UUID
intent = schema.NewIntent(..., schema.WithTargetTaskID(targetTaskID), schema.WithDependTaskID([]string{taskID}))

// processModifyTaskIntent 中：
tasks := h.TaskManager.GetTask(ctx, schema.TaskFilter{TaskID: intent.TargetTaskID})
// ↑ 用 TargetTaskID 查找，但它是新生成的 UUID，不是 LLM 返回的原始 task_id
```

**修复方案**：`processModifyTaskIntent` 应使用 `intent.DependTaskID[0]` 查找旧任务。

```go
// 修改前：
tasks := h.TaskManager.GetTask(ctx, schema.TaskFilter{TaskID: intent.TargetTaskID})

// 修改后：
if len(intent.DependTaskID) == 0 {
    return nil, exception.NewBaseError(...)
}
tasks := h.TaskManager.GetTask(ctx, schema.TaskFilter{TaskID: intent.DependTaskID[0]})
```

---

### 问题 2：缺少 BaseOptimizerMixin 层 Backward/Step 模板方法

**章节**：9.70/9.72a-e 优化器
**Go 文件**：`internal/evolving/optimizer/base.go`
**Python 文件**：`openjiuwen/agent_evolving/optimizer/base.py`

**Python 样例**：
```python
# BaseOptimizer.backward()
async def backward(self, signals):
    self._validate_parameters()
    self._selected_signals = self._select_signals(signals)
    try:
        await self._backward(signals)
    except Exception as e:
        raise build_error(StatusCode.TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR, ...) from e

# BaseOptimizer.step()
def step(self):
    self._validate_parameters()
    try:
        updates = self._step()
        self.clear_trajectories()
        return updates or {}
    except Exception as e:
        self.clear_trajectories()
        raise build_error(StatusCode.TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR, ...) from e
```

**Go 问题**：
- Go 的 `BaseOptimizerMixin` 没有提供 `Backward()` / `Step()` 模板方法
- 各子优化器自己实现 Backward/Step，缺少统一错误包装
- 仅 InstructionOptimizer.Backward() 有 `TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR` 包装
- 所有 Step 缺少 `TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR` 包装
- Step 中 Python 无论成功失败都 `clear_trajectories()`，Go 仅成功路径清理

**修复方案**：在 `BaseOptimizerMixin` 上增加 `BackwardTemplate` / `StepTemplate` 模板方法：

```go
// BackwardTemplate 模板方法，统一 ValidateParameters + SelectSignals + _backward + 错误包装
func (m *BaseOptimizerMixin) BackwardTemplate(
    ctx context.Context,
    signals []*signal.EvolutionSignal,
    backwardFn func(context.Context, []*signal.EvolutionSignal) error,
) error {
    m.ValidateParameters()
    m.SetSelectedSignals(m.SelectSignals(signals))
    if err := backwardFn(ctx, signals); err != nil {
        return exception.NewBaseError(exception.StatusToolchainOptimizerBackwardExecutionError,
            exception.WithErr(err))
    }
    return nil
}

// StepTemplate 模板方法，统一 ValidateParameters + _step + ClearTrajectories + 错误包装
func (m *BaseOptimizerMixin) StepTemplate(
    stepFn func() map[schema.UpdateKey]any,
) map[schema.UpdateKey]any {
    m.ValidateParameters()
    defer m.ClearTrajectories()
    return stepFn()
}
```

各子类改为调用 `m.BackwardTemplate(ctx, signals, o._backward)` / `m.StepTemplate(o._step)`。

---

### 问题 3：SkillExperienceOptimizer.AddTrajectory 签名参数类型错误

**章节**：9.72d
**Go 文件**：`internal/evolving/optimizer/skill_call/experience_optimizer.go:64`

**Python 样例**：
```python
# BaseOptimizer 接口
def add_trajectory(self, trajectory: Trajectory):
    self._trajectories.append(trajectory)
```

**Go 问题**：
```go
func (o *SkillExperienceOptimizer) AddTrajectory(traj *signal.EvolutionSignal) {
    // SkillExperienceOptimizer 不使用 trajectory
}
```
签名参数为 `*signal.EvolutionSignal`，而 `BaseOptimizer` 接口要求 `*trajectory.Trajectory`。方法体为空（不使用 trajectory），但签名不匹配意味着不满足 `BaseOptimizer` 接口。

**修复方案**：
```go
func (o *SkillExperienceOptimizer) AddTrajectory(traj *trajectory.Trajectory) {
    // SkillExperienceOptimizer 不使用 trajectory
}
```

---

### 问题 4：AutoTitle UTF-8 按字节截断

**章节**：10.3.15 会话管理
**Go 文件**：`internal/swarm/server/session/session_utils.go:35-41`
**Python 文件**：`jiuwenswarm/server/runtime/session/session_metadata.py:120-125`

**Python 样例**：
```python
def _auto_title(content: str) -> str:
    title = content.strip().replace("\n", " ")
    if len(title) > _TITLE_MAX_LEN:    # len("你好世界") = 4（字符数）
        title = title[:_TITLE_MAX_LEN] + "..."  # 截取前 50 个字符
    return title
```

**Go 问题**：
```go
func AutoTitle(content string) string {
    title := strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
    if len(title) > titleMaxLen {       // len("你好世界") = 12（字节数）
        title = title[:titleMaxLen] + "..."  // 按字节截取，约 16 个中文字符就截断
    }
    return title
}
```

**具体流程示例**：
```
输入: "今天天气很好，我想出去散步，但是外面在下雨"（20个中文字符）
Python: len() = 20 字符，不截断，返回原文
Go:     len() = 60 字节，> 50，截取 title[:50] = "今天天气很好，我想出去散步，但是外面" + "..."
        而且 title[:50] 可能在 UTF-8 多字节字符中间截断，产生无效 UTF-8
```

**修复方案**：
```go
func AutoTitle(content string) string {
    title := strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
    runes := []rune(title)
    if len(runes) > titleMaxLen {
        title = string(runes[:titleMaxLen]) + "..."
    }
    return title
}
```

---

### 问题 5：ApplySessionRename UTF-8 按字节截断

**章节**：10.3.15 会话管理
**Go 文件**：`internal/swarm/server/session/session_rename.go:69-70`
**Python 文件**：`jiuwenswarm/server/runtime/session/session_rename.py:14-69`

**Python 样例**：
```python
raw_title = str(raw_title).strip()[:_RENAME_TITLE_MAX_LEN]  # 按 200 字符截取
```

**Go 问题**：
```go
if len(newTitle) > renameTitleMaxLen {   // 按字节比较
    newTitle = newTitle[:renameTitleMaxLen]  // 按字节截取，可能截断在多字节中间
}
```

**修复方案**：
```go
if utf8.RuneCountInString(newTitle) > renameTitleMaxLen {
    runes := []rune(newTitle)
    newTitle = string(runes[:renameTitleMaxLen])
}
```

---

### 问题 6：PermissionContext.Scene() 优先级顺序与 Python 相反

**章节**：10.1.8 Schema 层
**Go 文件**：`internal/swarm/schema/permission.go:155-163`
**Python 文件**：`jiuwenswarm/common/schema/agent.py` (PermissionContext)

**Python 样例**：
```python
@property
def scene(self) -> str:
    if self.group_digital_avatar:          # ① 先检查 group_digital_avatar
        return "group_digital_avatar"
    if self.channel_id.strip() == "web":   # ② 再检查 web
        return "web"
    return "normal_im"
```

**Go 问题**：
```go
func (p *PermissionContext) Scene() string {
    if p.ChannelID == "web" {              // ① 先检查 web（顺序反了！）
        return "web"
    }
    if p.GroupDigitalAvatar {              // ② 再检查 group_digital_avatar
        return "group_digital_avatar"
    }
    return "normal_im"
}
```

**具体流程示例**：
```
GroupDigitalAvatar=true, ChannelID="web" 时：
Python → "group_digital_avatar"（数字分身场景，权限更严格）
Go     → "web"（普通 web 场景，权限更宽松）

后果：权限判断走不同分支，可能导致数字分身在 web 渠道下权限偏松
```

**修复方案**：调整判断顺序，先 GroupDigitalAvatar 后 web：
```go
func (p *PermissionContext) Scene() string {
    if p.GroupDigitalAvatar {
        return "group_digital_avatar"
    }
    if strings.TrimSpace(p.ChannelID) == "web" {
        return "web"
    }
    return "normal_im"
}
```
注意：同时补充 `TrimSpace`，与 `owner_scopes.go` 中的实现一致。

---

### 问题 8：lite runChecker 未真正调用 MemUpdateChecker

**章节**：7.8 (lite 包)
**Go 文件**：`internal/agentcore/memory/lite/coding_memory_tool_ops.go:491-493`
**Python 文件**：`openjiuwen/core/memory/lite/coding_memory_tool_ops.py`

**Python 样例**：
```python
async def _run_checker(coding_memory_manager, new_id, new_body, old_memories):
    checker = MemUpdateChecker()
    new_memories = {new_id: new_body}
    llm = coding_memory_manager.llm if coding_memory_manager else None
    if not llm:
        return []
    try:
        return await checker.check(new_memories=new_memories, old_memories=old_memories,
                                    base_chat_model=llm)
    except Exception as e:
        logger.warning(f"MemUpdateChecker failed: {e}")
        return []
```

**Go 问题**：
```go
func runChecker(newID string, newBody string, oldMemories map[string]string) []*update.MemoryActionItem {
    // 当前无 LLM 模型可用，返回 nil
    return nil
}
```

**具体流程示例**：
```
场景：用户写入 coding_memory，已有相似文件
Python: if not llm → return [] → 不做冲突检测 → 直接写入
Go:     runChecker → return nil → len(actions)==0 → 跳过 REDUNDANT 检查
        → 进入 "else if len(similarFiles) > 0" 分支 → 误报所有相似文件为冲突
```

**修复方案**：
1. 给 `CodingMemoryToolContext` 添加 LLM 模型字段
2. `runChecker` 从 `CodingMemoryToolContext` 获取 LLM 模型并调用 `MemUpdateChecker.Check()`
3. 无 LLM 时返回空切片（与 Python `return []` 一致），而不是 nil

```go
func runChecker(toolCtx *CodingMemoryToolContext, newID string, newBody string,
    oldMemories map[string]string) []*update.MemoryActionItem {
    if toolCtx.llm == nil {
        return []*update.MemoryActionItem{}  // 对齐 Python: if not llm: return []
    }
    checker := &update.MemUpdateChecker{}
    actions, err := checker.Check(context.Background(),
        map[string]string{newID: newBody}, oldMemories, update.WithModel(toolCtx.llm))
    if err != nil {
        logger.Warn(logComponent).Err(err).Msg("MemUpdateChecker 调用失败")
        return []*update.MemoryActionItem{}
    }
    return actions
}
```

---

### 问题 9：SQL 动态表 DDL 缺少索引

**章节**：9.65a-5
**Go 文件**：`internal/agent_teams/tools/database/sql_team_database.go`（DDL 建表语句）
**Python 文件**：`openjiuwen/agent_teams/tools/database/engine.py` + `models.py`

**Python 样例**（SQLModel 声明式索引）：
```python
class TeamTaskBase(SQLModel):
    team_name: str = Field(foreign_key="team_info.team_name", ondelete="CASCADE", index=True)
    status: str = Field(index=True)
    assignee: Optional[str] = Field(default=None, index=True)
    updated_at: int = Field(index=True)

class TeamMessageBase(SQLModel):
    team_name: str = Field(foreign_key="team_info.team_name", ondelete="CASCADE", index=True)
    to_member_name: Optional[str] = Field(default=None, index=True)
    timestamp: int = Field(index=True)
    broadcast: Optional[bool] = Field(default=None, index=True)
    is_read: Optional[bool] = Field(default=None, index=True)
```

**Go 问题**：Go 的 DDL 中 4 张动态表完全没有索引和外键约束。

**具体流程示例**：
```
get_tasks_by_assignee(team_name="team1", assignee="member1")：
  Python: WHERE assignee = 'member1' — 有索引，O(log N)
  Go:     WHERE assignee = 'member1' — 无索引，全表扫描 O(N)

has_unread_messages(team_name="team1")：
  Python: WHERE is_read = False — 有索引
  Go:     WHERE is_read = 0 — 无索引，全表扫描
```

**修复方案**：在 DDL 中补充索引：
```go
// team_task 表索引
CREATE INDEX IF NOT EXISTS idx_task_team_name ON team_task_%s(team_name)
CREATE INDEX IF NOT EXISTS idx_task_status ON team_task_%s(status)
CREATE INDEX IF NOT EXISTS idx_task_assignee ON team_task_%s(assignee)
CREATE INDEX IF NOT EXISTS idx_task_updated_at ON team_task_%s(updated_at)

// team_message 表索引
CREATE INDEX IF NOT EXISTS idx_msg_team_name ON team_message_%s(team_name)
CREATE INDEX IF NOT EXISTS idx_msg_to_member ON team_message_%s(to_member_name)
CREATE INDEX IF NOT EXISTS idx_msg_timestamp ON team_message_%s(timestamp)
CREATE INDEX IF NOT EXISTS idx_msg_broadcast ON team_message_%s(broadcast)
CREATE INDEX IF NOT EXISTS idx_msg_is_read ON team_message_%s(is_read)
```

---

### 问题 10：CreateMessage 不区分 IntegrityError 和 OperationalError

**章节**：9.65a-5
**Go 文件**：`internal/agent_teams/tools/database/sql_message_dao.go`

**Python 样例**：
```python
try:
    session.add(db_msg)
    session.commit()
except IntegrityError:     # 主键冲突 → 直接返回 False
    session.rollback()
    return False
except OperationalError:   # SQLite 锁 → 重试
    session.rollback()
    # retry...
```

**Go 问题**：Go 不区分错误类型，所有错误都触发重试，包括主键冲突（IntegrityError）。主键冲突是确定性失败，重试无意义。

**修复方案**：在重试前检查错误类型，IntegrityError（如 `UNIQUE constraint failed`）直接返回 False：
```go
if isIntegrityError(err) {
    return false  // 主键冲突，不重试
}
```

---

## 一般问题详细分析（续）

### 问题 7：PermissionContext.OwnerScopeKey() 缺少 TrimSpace

**章节**：10.1.8
**Go 文件**：`internal/swarm/schema/permission.go`
**Python 文件**：`jiuwenswarm/common/schema/agent.py`

**Python 样例**：
```python
@property
def owner_scope_key(self) -> tuple[str, str]:
    return self.channel_id.strip(), self.principal_user_id.strip()
```

**Go 问题**：
```go
func (p *PermissionContext) OwnerScopeKey() [2]string {
    return [2]string{p.ChannelID, p.PrincipalUserID}  // 缺少 TrimSpace
}
```

**修复方案**：
```go
func (p *PermissionContext) OwnerScopeKey() [2]string {
    return [2]string{strings.TrimSpace(p.ChannelID), strings.TrimSpace(p.PrincipalUserID)}
}
```

---

### 问题 8：MemUpdateChecker CONFLICTING 分支缺少 old_id 校验

**章节**：7.8
**Go 文件**：`internal/agentcore/memory/manage/update/update_checker.go:296-342`
**Python 文件**：`openjiuwen/core/memory/manage/update/mem_update_checker.py:252-263`

**Python 样例**：
```python
elif check_item.result == CheckResult.CONFLICTING:
    new_content = new_memories.get(new_id, check_item.info_text)
    action_items.append(MemoryActionItem(id=new_id, content=new_content, status=MemoryStatus.ADD))
    for old_id, old_content in check_item.related_infos.items():
        if old_id in old_memories:           # ← Python 校验旧记忆确实存在
            action_items.append(MemoryActionItem(id=old_id, content=old_content, status=MemoryStatus.DELETE))
```

**Go 问题**：
```go
case CheckResultConflicting:
    // ...添加新记忆 ADD...
    for oldID, oldContent := range item.RelatedInfos {
        // 缺少 oldID in oldMemories 校验
        actionItems = append(actionItems, &MemoryActionItem{
            ID:      oldID,
            Content: oldContent,
            Status:  MemoryStatusDelete,
        })
    }
```

**修复方案**：
```go
for oldID, oldContent := range item.RelatedInfos {
    if _, exists := oldMemories[oldID]; !exists {
        continue  // 对齐 Python: if old_id in old_memories
    }
    actionItems = append(actionItems, &MemoryActionItem{...})
}
```

---

### 问题 9：VariableManager.QueryVariable name 纯空白字符串处理缺失

**章节**：7.7
**Go 文件**：`internal/agentcore/memory/manage/index/variable_manager.go`
**Python 文件**：`openjiuwen/core/memory/manage/index/variable_manager.py`

**Python 样例**：
```python
if not name or not name.strip():    # "   " 被视为空，走前缀查询
```

**Go 问题**：
```go
if name == "" {                     // "   " 不被视为空，走具体键查询
```

**修复方案**：
```go
if strings.TrimSpace(name) == "" {
```

---

### 问题 10：VariableManager 缺少 LEGACY_PREFIXES

**章节**：7.7
**Python 文件**：`openjiuwen/core/memory/manage/index/variable_manager.py`

**Python 样例**：
```python
class VariableManager:
    LEGACY_PREFIXES: List[str] = []

    def __init__(self, kv_store, crypto_key):
        # ...
        for legacy_prefix in self.LEGACY_PREFIXES:
            kv_prefix_registry.register_legacy(legacy_prefix)
```

**Go 问题**：Go 中完全没有 `legacyPrefixes` 常量和循环注册逻辑。虽然当前列表为空不影响运行，但结构缺失。

**修复方案**：补充 `legacyPrefixes` 变量和注册循环：
```go
var legacyPrefixes []string  // 对齐 Python LEGACY_PREFIXES: List[str] = []

func NewVariableManager(kvStore kv.BaseKVStore, cryptoKey []byte) (*VariableManager, error) {
    // ...
    if err := common.KVPrefixRegistry.RegisterCurrent(userVarPrefix); err != nil {
        return nil, err
    }
    if err := common.KVPrefixRegistry.RegisterCurrent(sessionVarPrefix); err != nil {
        return nil, err
    }
    for _, prefix := range legacyPrefixes {
        if err := common.KVPrefixRegistry.RegisterLegacy(prefix); err != nil {
            return nil, err
        }
    }
    // ...
}
```

---

### 问题 11：NewVariableManager 忽略 RegisterCurrent 返回的 error

**章节**：7.7
**Go 文件**：`internal/agentcore/memory/manage/index/variable_manager.go:71-73`

**Go 问题**：
```go
_ = common.KVPrefixRegistry.RegisterCurrent(userVarPrefix)
_ = common.KVPrefixRegistry.RegisterCurrent(sessionVarPrefix)
```
Python 中 `register_current` 失败会抛 `ValueError` 中断 `__init__`，Go 忽略了 error。

**修复方案**：见问题 10 修复方案，检查 error 返回值。

---

### 问题 12：ToolOptimizerBase.RequiresForwardData() 与 Python 不一致

**章节**：9.72b
**Go 文件**：`internal/evolving/optimizer/tool_call/base.go:117`
**Python 文件**：`openjiuwen/agent_evolving/optimizer/tool_call/base.py`

**Python 样例**：
```python
# ToolOptimizerBase 没有覆盖 requires_forward_data()，继承 BaseOptimizer → 返回 True
```

**Go 问题**：
```go
func (b *ToolOptimizerBase) RequiresForwardData() bool {
    return false  // Go 显式返回 false
}
```

**分析**：Go 注释说明 ToolOptimizer 是黑盒优化器不需要前向数据，语义上更合理。但 Python 实际行为是 True（Trainer 会执行前向推理）。如果 Python 是 bug（忘了覆盖），Go 更合理；如果 Python 有意为之，则需对齐。

**修复方案**：建议保持 Go=false（更合理），但需确认 Python 端行为。若需对齐，改为 `return true`。

---

### 问题 13：IntentToolkits result 字符串中文化

**章节**：6.19
**Go 文件**：`internal/agentcore/controller/modules/intent_toolkits.go`
**Python 文件**：`openjiuwen/core/controller/modules/intent_toolkits.py`

**Python 样例**：
```python
# CreateTask result
return f"Task ID: {task_id}, Task Description: {task_description}, Current Status: Created and submitted for execution"
```

**Go 问题**：
```go
// CreateTask result
return fmt.Sprintf("任务 ID: %s, 任务描述: %s, 当前状态: 已创建并提交执行", ...)
```

这些 result 作为 ToolMessage 内容发给 LLM，应保持英文原文以匹配 Python。根据项目规则"提示词一比一复刻 Python"，此处的 result 字符串属于 LLM 可见的输出内容，不应翻译。

**修复方案**：将所有 IntentToolkits 方法的 result 字符串改回英文原文。

---

### 问题 14：memory.forbidden.get/set RPC 为 stub

**章节**：10.6.17
**Go 文件**：`internal/swarm/server/agent_ws_server.go`（stub 注册）
**Python 文件**：`jiuwenswarm/server/agent_ws_server.py`

**Python 样例**：
```python
# memory.forbidden.get：读取 config["memory"]["forbidden_memory_definition"] 并返回
# memory.forbidden.set：接收 params，调用 update_memory_forbidden_in_config(params) 写回
```

**Go 问题**：
```go
stubHandler("memory.forbidden.get", map[string]any{})
stubHandler("memory.forbidden.set", map[string]any{})
```

前端如果直接调用 `memory.forbidden.get` 会拿到空数据。目前前端走 `config.get/set` 路径，暂不影响功能。

**修复方案**：实现完整的 `memory.forbidden.get/set` RPC handler，或确认前端已改用 config 路径后标注为已知偏差。

---

### 问题 15：config.set forbidden description 合并语义需验证

**章节**：10.6.17
**Python 文件**：`jiuwenswarm/server/runtime/config_apply.py`

**Python 样例**：
```python
def update_memory_forbidden_description_in_config(description: dict):
    current_desc = config.get("memory.forbidden_memory_definition.description", {})
    config.set("memory.forbidden_memory_definition.description", {**current_desc, **description})
    # ↑ 显式合并：保留其他语言的描述
```

**Go 问题**：
```go
cfg.Set(fmt.Sprintf("memory.forbidden_memory_definition.description.%s", preferredLang), descVal)
// ↑ 通过点路径设置单语言 key，如果底层是替换而非深度设置，会丢失其他语言
```

**修复方案**：验证 Go `config.Set` 对嵌套路径的行为。如果是替换整个 `description` 子树，需改为先读取再合并后写入。

---

### 问题 16：TeamSkillExperienceOptimizer._build_frontmatter() 未实现

**章节**：9.72d
**Go 文件**：`internal/evolving/optimizer/skill_call/team_optimizer.go`
**Python 文件**：`openjiuwen/agent_evolving/optimizer/skill_call/team_skill_experience_optimizer.py`

**Python 样例**：
```python
@staticmethod
def _build_frontmatter(name: str, description: str, roles: list[str]) -> str:
    roles_str = "\n".join(f"  - {r}" for r in roles)
    return f"---\nname: {name}\ndescription: {description}\nroles:\n{roles_str}\n---\n"
```

**Go 问题**：Go 中没有对应实现。

**修复方案**：添加 `buildFrontmatter` 函数：
```go
func buildFrontmatter(name, description string, roles []string) string {
    var rolesStr strings.Builder
    for _, r := range roles {
        rolesStr.WriteString("  - " + r + "\n")
    }
    return fmt.Sprintf("---\nname: %s\ndescription: %s\nroles:\n%s---\n", name, description, rolesStr.String())
}
```

---

## 提示问题汇总

### 问题 17：processUnknownTaskIntent 使用 context.Background()

**章节**：6.19
**Go 文件**：`internal/agentcore/controller/modules/event_handler.go`

方法签名中 ctx 被忽略（`_ context.Context`），内部用 `context.Background()` 调用 WriteStream。建议改用传入的 ctx。

---

### 问题 18：processSupplementTaskIntent 吞掉 pause 错误

**章节**：6.19
**Go 文件**：`internal/agentcore/controller/modules/event_handler.go`

Python 中 `pause_task` 失败会抛异常中断流程，Go 吞掉错误只记 Warn 日志并继续。这是有意的偏差（Go 更健壮），建议记录为已知差异。

---

### 问题 19：Step 无论成功失败都应 ClearTrajectories

**章节**：9.70
**Python 文件**：`openjiuwen/agent_evolving/optimizer/base.py`

Python `step()` 中 `clear_trajectories()` 在 try 和 except 两个分支都调用。Go 的所有 Step 实现仅在成功路径调用 `ClearTrajectories()`，失败路径（error 返回）不会清理轨迹。

**修复方案**：在问题 2 的 StepTemplate 模板方法中用 `defer m.ClearTrajectories()` 统一处理。

---

### 问题 20：TeamSkillExperienceOptimizer._dump_raw() 仅记日志

**章节**：9.72d
**Go 文件**：`internal/evolving/optimizer/skill_call/team_optimizer.go:982`

Python 会将原始 LLM 输出写入 debug 目录下文件，Go 仅记录日志。功能缺失但影响较小（仅调试用途）。

---

### 问题 21：MemUpdateChecker 缺少 processed_new_ids 追踪

**章节**：7.8
**Python 文件**：`openjiuwen/core/memory/manage/update/mem_update_checker.py:238-239`

**Python 样例**：
```python
processed_new_ids = set()
for check_item in check_results:
    new_id = check_item.info_id
    processed_new_ids.add(new_id)     # 追踪已处理的 ID
```

Go 中没有 `processed_new_ids` 追踪。当前不影响功能（Python 也没有根据此集合做后续过滤），但 Python 保留此变量可能有未来用途。

---

### 问题 22：Forbidden Memory description 无硬编码默认值

**章节**：10.6.17
**Go 文件**：`internal/swarm/agents/harness/common/memory/forbidden.go`

Python 在 `_get_memory_forbidden_config()` 中为 `description` 提供了硬编码默认值（含中英文敏感信息描述文本），Go 没有。如果配置文件只设了 `enabled: true` 但缺失 `description`，Python 会显示默认描述，Go 输出无描述段的提示词。

---

### 问题 23：isResultUsable recover 路径未设置 lastError

**章节**：9.72a
**Go 文件**：`internal/evolving/optimizer/llm_resilience/llm_resilience.go:302-341`

Python catch 异常时 `last_error = exc`，Go 的 recover 路径中没有设置 `lastError`，导致重试耗尽后的错误信息缺失最后一次 isResultUsable 异常信息。

---

## 附录：已确认一致的关键实现

以下实现经对比确认与 Python 高度一致，无需修改：

| 章节 | 实现点 | 验证结论 |
|------|--------|---------|
| 7.8 | MemUpdateChecker.Check() 主流程 | 一致（LLM 调用 + JSON 解析 + 重试 + fallback） |
| 7.8 | WriteManager 所有方法 | 一致（add_memories/update_mem_by_id/delete_mem_by_id/delete_mem_by_user_id） |
| 7.8 | SearchManager.search() | 一致（search_type 路由 + 去重 + 排序 + threshold） |
| 7.8 | PromptApplier.Apply() | 一致（单例 + 文件加载 + 缓存 + Format） |
| 7.8 | formatInput() 新旧记忆格式化 | 一致（新记忆倒序、旧记忆正序） |
| 7.8 | 4 个 prompt .md 模板文件 | 一比一复刻 Python |
| 7.7 | SummaryManager 所有核心方法 | 一致（返回类型差异不影响功能） |
| 7.7 | KvPrefixRegistry | 一致（Go 额外加了 RWMutex 并发安全，合理） |
| 10.6.4 | AvatarPromptRail BeforeModelCall 全流程 | 一致（9 个步骤全部对齐） |
| 10.6.4 | AvatarPromptRail BeforeToolCall 全流程 | 一致 |
| 10.6.4 | 所有提示词内容 | 中英文完全一比一复刻 |
| 10.6.4 | buildAvatarRail 构建函数 | 一致 |
| 10.6.17 | GetForbiddenMemoryPrompt 主流程 | 一致 |
| 10.6.17 | 中英文禁止记忆提示词 | 一比一复刻 |
| 10.3.15 | TruncateHistoryRecords 截断逻辑 | 一致（FlushHistoryQueue + cutIndex 裁剪） |
| 10.3.15 | readHistoryFile JSON 解析失败处理 | 一致（Warn 日志 + 返回空列表） |
| 10.3.15 | FlushHistoryQueue 哨兵机制 | 等价于 Python _WRITE_QUEUE.join() |
| 10.3.15 | SessionManager 各方法 | 一致 |
| 10.3.15 | UpdateSessionMetadata 三态语义 | 一致（nil/空/非空） |
| 10.3.15 | sessionctx 子包 | 一致（context.Value 替代 contextvars，合理） |
| 9.65a-5 | SqlTeamDatabase DDL 结构 | 一致（缺少索引和外键，见问题 9） |
| 9.65a-5 | TeamDao 5 方法 | 一致 |
| 9.65a-5 | MemberDao 8 方法 | 一致 |
| 9.65a-5 | TaskDao 18+5 辅助方法 | 基本一致（环检测算法等价、签名差异合理） |
| 9.65a-5 | MessageDao 7 方法 | 基本一致 |
| 9.65a-5 | NewTeamDatabase 工厂+降级 | Go 增加降级逻辑，合理 |
| 9.65a-5 | WithTx 跨表事务 | Go 特有设计，正确且必要 |
| 9.65a-5 | sessionctx 子包 | 一致 |
| 8.35 | ShutdownMember FSM 校验 | 一致 |
| 8.35 | ApprovePlan 三层前置校验 | 一致 |
| 8.35 | SpawnMember 签名改用 AgentCard | 一致 |
| 8.35 | CancelMember 消息失败阻断+reset 汇总 | 一致 |
| 8.35 | CleanTeam ERROR 豁免移除 | 一致 |
| 8.35 | ForceCleanTeam 综合返回 | 一致 |
| 8.35 | BuildTeam allocation 传递 | 一致 |
| 7.8 | WriteManager 所有方法 | 一致 |
| 7.8 | SearchManager.search() 路由逻辑 | 一致 |
| 7.8 | PromptApplier 单例+缓存 | 一致 |
| 7.8 | 4 个 prompt .md 模板文件 | 一比一复刻 |
| 7.8 | FragmentMemoryManager 冲突检查回填主流程 | 一致 |
| fix | AddInstalledPlugin normalizePlugin + saveState | 一致 |
| fix | generateToolID crypto/rand | 一致（128 位密码学安全随机） |
| fix | isResultUsable 严格类型检查 | 一致（defer/recover 等价 try/except） |
