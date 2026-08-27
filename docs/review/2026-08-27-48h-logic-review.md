# 48h 逻辑审查报告（2026-08-27）

> 审查范围：2026-08-23 ~ 2026-08-26 期间提交
> 涉及章节：9.65a-4 TeamBackend 门面、9.51-9.53 Harness 资源/Schema/Prompts、memory/lite 排序补充、skilltoolkit 类型修复
> 对比标准：Python 源码方法签名、步骤流程、提示词一比一复刻

---

## 一、审查范围

| 提交 | 日期 | 涉及章节 | 说明 |
|------|------|---------|------|
| `5df0dc84` | 08-23 | 9.65a-4 | 提取 spawnAndPublish + Startup/StartupMember 补全 onCreated 回调和失败回滚 |
| `fe77bdfc` | 08-23 | 9.65a-4 | ActiveTeam.Agent any→*TeamAgent + AgentLookup 类型回填 |
| `9db406d9` | 08-23 | 9.65a-4 | member.go DB/Messager any→具体类型回填 |
| `dd1147b3` | 08-23 | 9.50 | Score 字段从 any 改为 *int |
| `a7324ead` | 08-23 | 9.51-9.53 | 对齐 Python 六处不一致修复 |
| `5ce9b614` | 08-23 | 7.6 | 补充 ListFragmentMemories 排序 |
| `80f87475` | 08-26 | 多模块 | 统一错误消息为中文，修正声明排序 |

---

## 二、问题汇总

| # | 分类 | 模块 | 问题摘要 |
|---|------|------|---------|
| 1 | 🔴 严重 | 9.65a-4 | RefreshHumanAgentRoster 缺少 db.Initialize 调用 |
| 2 | 🔴 严重 | 9.65a-4 | ShutdownMember 多了 CancelAllTasks 步骤（Python 没有） |
| 3 | 🔴 严重 | 9.65a-4 | ForceCleanTeam 中 ShutdownMember 不传 force=true |
| 4 | 🟡 一般 | 9.65a-4 | ForceCleanTeam 多了 onTeamCleaned 回调（Python 不触发） |
| 5 | 🟡 一般 | 9.65a-4 | ShutdownMember 缺少 force 参数 |
| 6 | 🟡 一般 | 9.65a-4 | ShutdownMember 跳过了 is_valid_transition 校验（仅用 CAS） |
| 7 | 🟡 一般 | 9.51-9.53 | reload.go 多了 filesystem 类型（Python 没有） |
| 8 | 🟡 一般 | 9.51-9.53 | TaskPlan.ToMarkdown 格式三处偏差（标题/标记/result_summary） |
| 9 | 🟡 一般 | memory/lite | RebuildContentWithFrontmatter map 遍历顺序不确定 |
| 10 | 🟡 一般 | memory/lite | 缺少 group_chat_mode 保护 |
| 11 | 🟡 一般 | ability_manager | Add 重复能力时 Python 覆盖/Go 保留 |
| 12 | 🟡 一般 | ability_manager | ListToolInfo 缺少按 name 去重 |
| 13 | 🟡 一般 | skilltoolkit | SkillNet fallback 名称逻辑缺失 |
| 14 | 🟡 一般 | skilltoolkit | HandleSkillsUninstall error 被丢弃 |
| 15 | 🟢 提示 | memory/lite | EditMemoryWithContext 英文错误消息残留 |
| 16 | 🟢 提示 | memory/lite | write_memory 追加换行行为不一致 |
| 17 | 🟢 提示 | skilltoolkit | auto 模式排序硬编码 vs Python sorted() |
| 18 | 🟢 提示 | auth | HeaderQuery 认证空配置返回 Success=true |
| 19 | 🔴 严重 | 9.65a-4 | CleanTeam 中 ERROR 状态成员判断不一致 |
| 20 | 🟡 一般 | 9.65a-4 | Startup 额外跳过自身成员（Python 不跳过） |
| 21 | 🟡 一般 | 9.65a-4 | BuildTeam 注册 Leader 绕过 SpawnMember |
| 22 | 🟢 提示 | 9.65a-4 | ForceCleanTeam 返回值始终为 true（不检查失败） |
| 23 | 🟢 提示 | 9.65a-4 | CancelMember 消息发送失败仍返回 Success |
| 24 | 🟢 提示 | 9.65a-4 | RegisterCleanupPath 用 ExpandEnv 而非展开 ~ |

---

## 三、严重问题详情

### 问题 1：RefreshHumanAgentRoster 缺少 db.Initialize 调用

**分类**：🔴 严重 — 冷恢复路径中 DB DAO 未初始化导致查询失败

**Python 参考**（`agent_teams/tools/team.py:1156-1173`）：
```python
async def refresh_human_agent_roster(self) -> None:
    """...Calls db.initialize() first so callers can drive the refresh
    before any other DB-touching method has lazily warmed the DAOs."""
    initializer = getattr(self.db, "initialize", None)
    if initializer is not None:
        await initializer()
    member_dao = getattr(self.db, "member", None)
    if member_dao is None:
        team_logger.debug("Skipping human-agent roster refresh...")
        return
    names = await member_dao.list_human_agent_names(self.team_name)
    # ...
```

**Go 问题**（`team_backend.go:776-789`）：
```go
func (tb *TeamBackend) RefreshHumanAgentRoster(ctx context.Context) {
    names, err := tb.db.Member().ListHumanAgentNames(ctx, tb.teamName)
    // ❌ 缺少 db.Initialize() 调用
    // ❌ 缺少 Member DAO 可用性检查
    if err != nil {
        logger.Error(tbLogComponent).Err(err).Msg("RefreshHumanAgentRoster: 查询失败")
        return
    }
    // ...
}
```

**影响**：在冷恢复路径（`recover_team`、`from_spawn_payload`）中，DB DAO 可能还未初始化。Python 先调用 `db.initialize()` 预热 DAO，然后才查询。Go 跳过这一步，直接查询 Member DAO 可能失败，导致 HITT 名册缓存无法重建。

**修复方案**：
```go
func (tb *TeamBackend) RefreshHumanAgentRoster(ctx context.Context) {
    // 步骤 0: 对齐 Python — 先初始化 DB（预热 DAO）
    if tb.db != nil {
        if initializer, ok := tb.db.(interface{ Initialize(context.Context) error }); ok {
            if err := initializer.Initialize(ctx); err != nil {
                logger.Debug(tbLogComponent).Err(err).Msg("RefreshHumanAgentRoster: DB 初始化失败")
            }
        }
    }
    // 步骤 1: 查询 human_agent 成员名
    names, err := tb.db.Member().ListHumanAgentNames(ctx, tb.teamName)
    // ...
}
```

---

### 问题 2：ShutdownMember 多了 CancelAllTasks 步骤

**分类**：🔴 严重 — Python shutdown_member 不取消任务，Go 多执行了取消

**Python 参考**（`agent_teams/tools/team.py:514-598`）：
```python
async def shutdown_member(self, member_name: str, force: bool = False) -> MemberOpResult:
    # 1. 查成员
    # 2. 若已 SHUTDOWN → 幂等 success
    # 3. is_valid_transition 校验
    # 4. db.member.update_member_status(SHUTDOWN_REQUESTED)
    # 5. message_manager.send_message(shutdown_request_content)
    # 6. messager.publish(MemberShutdownEvent(force=force))
    # ❌ 没有 CancelAllTasks 步骤
```

**Go 问题**（`team_backend.go:418-447`）：
```go
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) atschema.MemberOpResult {
    // 1-3: 查成员 + 幂等 + CAS
    // 4: 发送 shutdown 消息
    _, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.shutdown_request_content"), memberName, tb.memberName)
    // ❌ 多了这一步：取消该成员的任务
    _, _ = tb.taskManager.CancelAllTasks(ctx, []string{memberName})
    // 5: 发布事件
    // ...
}
```

**影响**：Python 中 shutdown_member 只更新状态和发通知，成员的任务不会被自动取消。成员进程收到 shutdown 事件后自行处理清理。Go 的额外 CancelAllTasks 可能导致：任务被意外取消后无法恢复，与 Python 行为不一致。

**修复方案**：删除 ShutdownMember 中的 CancelAllTasks 调用。如果确实需要在 Go 中保留任务取消逻辑，应在 `cancel_member` 方法中执行（Python 也是在 cancel_member 中重置 CLAIMED 任务）。

---

### 问题 3：ForceCleanTeam 中 ShutdownMember 不传 force=true

**分类**：🔴 严重 — Python 传 force=True，Go 的 ShutdownMember 没有 force 参数

**Python 参考**（`agent_teams/tools/team.py:729-761`）：
```python
async def force_clean_team(self, shutdown_members: bool = True) -> bool:
    if shutdown_members:
        members = await self.db.member.get_team_members(self.team_name)
        for member_data in members:
            if member_data.member_name == self.member_name:
                continue
            try:
                await self.shutdown_member(member_data.member_name, force=True)  # ✅ 传 force=True
            except Exception as e:
                team_logger.warning("Failed to request shutdown for member {} ...", ...)
```

**Go 问题**（`team_backend.go:629-659`）：
```go
func (tb *TeamBackend) ForceCleanTeam(ctx context.Context, shutdownMembers bool) (bool, error) {
    if shutdownMembers {
        for _, m := range members {
            if m.MemberName == tb.memberName { continue }
            if m.Status != string(atschema.MemberStatusShutdown) {
                result := tb.ShutdownMember(ctx, m.MemberName)  // ❌ 没有传 force
                // ...
            }
        }
    }
    // ...
}
```

**影响**：Python 在强制清理时传 `force=True`，这影响 `MemberShutdownEvent.force` 字段和 FSM 状态转换行为（force=True 可能绕过某些检查）。Go 的 `ShutdownMember` 方法签名不接受 `force` 参数，事件中 `Force` 固定为 `false`。

**修复方案**：
1. 给 ShutdownMember 增加 `force bool` 参数
2. ForceCleanTeam 调用时传 `force=true`
3. 发布 MemberShutdownEvent 时传递 force 值

```go
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string, force bool) atschema.MemberOpResult {
    // ... 现有逻辑 ...
    tb.publishEvent(ctx, atschema.MemberShutdownEvent{
        BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
        Force:            force,  // 使用传入的 force 值
    })
    // ...
}

// ForceCleanTeam 中:
result := tb.ShutdownMember(ctx, m.MemberName, true)  // force=true
```

---

## 四、一般问题详情

### 问题 4：ForceCleanTeam 多了 onTeamCleaned 回调

**Python 参考**（`agent_teams/tools/team.py:729-761`）：
```python
async def force_clean_team(self, shutdown_members: bool = True) -> bool:
    # 1. shutdown_members → shutdown_member(force=True)
    # 2. db.force_delete_team_session
    # 3. _remove_cleanup_paths
    # ❌ 没有 on_team_cleaned 回调
```

**Go 问题**（`team_backend.go:649-654`）：
```go
// 步骤 3: 回调触发
if tb.onTeamCleaned != nil {
    if err := tb.onTeamCleaned(ctx); err != nil {
        logger.Warn(tbLogComponent).Err(err).Msg("ForceCleanTeam: onTeamCleaned 回调失败")
    }
}
```

**影响**：Python 的 `on_team_cleaned` 回调仅在 `clean_team()` 成功路径触发，`force_clean_team()` 不触发。Go 在 ForceCleanTeam 中也触发了该回调，可能导致宿主 TeamAgent 重复执行清理逻辑。

**修复方案**：删除 ForceCleanTeam 中的 onTeamCleaned 回调调用。

---

### 问题 5：ShutdownMember 缺少 force 参数

**Python 参考**（`agent_teams/tools/team.py:514`）：
```python
async def shutdown_member(self, member_name: str, force: bool = False) -> MemberOpResult:
```

**Go 问题**（`team_backend.go:418`）：
```go
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) atschema.MemberOpResult:
    // ❌ 缺少 force 参数
```

**影响**：无法区分普通关闭和强制关闭。Python 的 force 参数会影响 MemberShutdownEvent 的 force 字段，成员进程可能据此决定是否立即中断当前操作。

**修复方案**：同问题 3 的修复方案。

---

### 问题 6：ShutdownMember 跳过了 is_valid_transition 校验

**Python 参考**（`agent_teams/tools/team.py:552-558`）：
```python
from openjiuwen.agent_teams.schema.status import MEMBER_TRANSITIONS, is_valid_transition

if not is_valid_transition(current_status, MemberStatus.SHUTDOWN_REQUESTED, MEMBER_TRANSITIONS):
    return MemberOpResult.fail(f"Member {member_name} cannot shut down from status '{current_status.value}'")
```

**Go 问题**（`team_backend.go:429-434`）：
```go
ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
    member.Status, string(atschema.MemberStatusShutdownRequested))
if !ok {
    return atschema.NewMemberOpResultFail("CAS transition failed for: " + memberName)
}
```

**影响**：Python 先用 FSM 规则验证转换是否合法，再执行 DB 更新。Go 直接用 CAS（from→to），如果 from_status 不匹配 CAS 失败，但错误信息不够明确（"CAS transition failed" vs "cannot shut down from status 'xxx'"）。在并发场景下 CAS 更安全，但错误信息对调试不友好。

**修复方案**：保留 CAS 行为（更安全），但改进错误消息，包含当前状态信息：
```go
if !ok {
    return atschema.NewMemberOpResultFail(fmt.Sprintf(
        "member %s cannot shut down from status '%s'", memberName, member.Status))
}
```

---

### 问题 7：reload.go 多了 filesystem 类型

**Python 参考**（`harness/prompts/sections/reload.py:19,29`）：
```python
RELOAD_HINT_CN = (
    ...
    '存储类型："in_memory"（会话缓存）'  # ✅ 只有 in_memory
)
RELOAD_HINT_EN = (
    ...
    'Storage types: "in_memory" (session cache)'  # ✅ 只有 in_memory
)
```

**Go 问题**（`harness/prompts/sections/reload.go:20,28`）：
```go
reloadHintCN = ... +
    `存储类型："in_memory"（会话缓存）、"filesystem"（磁盘文件）`  // ❌ 多了 filesystem

reloadHintEN = ... +
    `Storage types: "in_memory" (session cache), "filesystem" (disk file)`  // ❌ 多了 filesystem
```

**影响**：提示词告诉 LLM 有 filesystem 类型，但 Python 的提示词只声明了 in_memory。虽然 Go 的 context_engine 确实支持 filesystem 类型 offload，但按照项目规则（提示词一比一复刻 Python），应删除 filesystem 以对齐 Python。

**修复方案**：
```go
reloadHintCN = ... +
    `存储类型："in_memory"（会话缓存）`

reloadHintEN = ... +
    `Storage types: "in_memory" (session cache)"`
```

---

### 问题 8：TaskPlan.ToMarkdown 格式三处偏差

**Python 参考**（`harness/schema/task.py:177-195`）：
```python
def to_markdown(self) -> str:
    lines = [f"## Goal: {self.goal}", ""]
    for t in self.tasks:
        if t.status == TodoStatus.COMPLETED:
            mark = "√"
        elif t.status == TodoStatus.IN_PROGRESS:
            mark = ">"
        elif t.status == TodoStatus.CANCELLED:
            mark = "×"
        else:
            mark = " "
        suffix = ""
        if t.result_summary:
            suffix = f" — {t.result_summary}"
        lines.append(f"- [{mark}] {t.content}{suffix}")
    return "\n".join(lines)
```

**Go 问题**（`harness/schema/task.go:250-268`）：
```go
func (tp *TaskPlan) ToMarkdown() string {
    sb.WriteString("# Task Plan\n\n**Goal:** ")  // ❌ 偏差1: 标题格式不同
    // ...
    sb.WriteString(icon)  // ❌ 偏差2: 用 [√]/[→] 等带方括号格式
    // ❌ 偏差3: 没有 result_summary 后缀
}
```

**三处偏差**：
1. **标题**：Python `## Goal: {goal}`，Go `# Task Plan\n\n**Goal:** {goal}`
2. **标记**：Python `√/>/×/ ` 单字符，Go `[√]/[→]/[×]/[ ]` 带方括号
3. **后缀**：Python 有 ` — {result_summary}`，Go 没有

**修复方案**：
```go
func (tp *TaskPlan) ToMarkdown() string {
    var sb strings.Builder
    sb.WriteString("## Goal: ")
    sb.WriteString(tp.Goal)
    sb.WriteString("\n\n")
    for _, task := range tp.Tasks {
        var mark string
        switch task.Status {
        case TodoStatusCompleted:
            mark = "√"
        case TodoStatusInProgress:
            mark = ">"
        case TodoStatusCancelled:
            mark = "×"
        default:
            mark = " "
        }
        sb.WriteString("- [")
        sb.WriteString(mark)
        sb.WriteString("] ")
        sb.WriteString(task.Content)
        if task.ResultSummary != "" {
            sb.WriteString(" — ")
            sb.WriteString(task.ResultSummary)
        }
        sb.WriteString("\n")
    }
    return sb.String()
}
```

---

### 问题 9：RebuildContentWithFrontmatter map 遍历顺序不确定

**Python 参考**：Python 的 `_parse_frontmatter` 解析 YAML，重建时按 YAML 的插入顺序保持，但 Go 的 `map` 遍历随机化。

**Go 问题**（`memory/lite/frontmatter.go:91`）：
```go
for key, value := range fm {  // ❌ map 遍历顺序不确定
    // ...
}
```

**影响**：frontmatter 输出中字段顺序随机，导致同一文件多次保存后 frontmatter 格式可能不同，干扰 diff 比对和 LLM 理解。

**修复方案**：按固定顺序输出 name/description/type，其余字段按 key 排序：
```go
// 固定顺序字段
fixedOrder := []string{"name", "description", "type"}
for _, k := range fixedOrder {
    if v, ok := fm[k]; ok {
        // 输出 k: v
    }
}
// 其余字段按 key 排序
var rest []string
for k := range fm {
    if !isFixed(k) { rest = append(rest, k) }
}
sort.Strings(rest)
for _, k := range rest {
    // 输出 k: v
}
```

---

### 问题 10：memory/lite 缺少 group_chat_mode 保护

**Python 参考**（`memory_tools.py:24-26`）：
```python
_GROUP_CHAT_MODE: contextvars.ContextVar = contextvars.ContextVar("group_chat_mode", default=False)

def is_group_chat_mode() -> bool:
    return _GROUP_CHAT_MODE.get()

# write_memory 和 edit_memory 在群聊模式下返回:
# {"success": False, "error": "群聊模式下禁止写入记忆文件"}
```

**Go 问题**：`tool_ops.go` 的 `WriteMemoryWithContext` 和 `EditMemoryWithContext` 没有群聊模式检查。

**影响**：在群聊场景下，多个 Agent 可能同时写入记忆文件，导致冲突。Python 通过 group_chat_mode 阻止写入，Go 缺少此保护。

**修复方案**：在 MemoryToolContext 中添加 group_chat_mode 检查，或在 WriteMemoryWithContext/EditMemoryWithContext 入口检查上下文标志。

---

### 问题 11：ability_manager Add 重复能力时 Python 覆盖/Go 保留

**Python 参考**（`ability_manager.py:48-55`）：
```python
def add(self, _ability):
    self._tools[_ability.name] = _ability  # ✅ 直接覆盖
```

**Go 问题**（`ability_manager.go:97-104`）：
```go
if _, exists := am.tools[name]; exists {
    logger.Warn(amLogComponent).Str("name", name).Msg("Add: 重复注册")
    return AddedResult{Added: false, Reason: "duplicate_tool"}  // ❌ 保留已有的
}
```

**影响**：Python 允许后注册的覆盖先注册的（对于 MCP 懒加载工具重新注册场景有用），Go 拒绝覆盖。如果 MCP 工具热重载时需要重新注册，Go 会失败。

**修复方案**：根据业务需求决定——如果需要对齐 Python，应改为覆盖模式；否则在注释中明确说明差异。

---

### 问题 12：ability_manager ListToolInfo 缺少按 name 去重

**Python 参考**（`ability_manager.py:216-224`）：
```python
def list_tool_info(self, ...):
    seen_names: set = set()
    unique_tool_infos = []
    for info in tool_infos:
        if info.name not in seen_names:
            seen_names.add(info.name)
            unique_tool_infos.append(info)
    return unique_tool_infos
```

**Go 问题**（`ability_manager.go:283-395`）：`ListToolInfo` 无去重。

**影响**：MCP 懒加载工具可能与已注册工具同名，导致 LLM function calling 中出现重复工具定义。

**修复方案**：在 ListToolInfo 末尾添加按 name 去重逻辑。

---

### 问题 13：skilltoolkit SkillNet fallback 名称逻辑缺失

**Python 参考**（`skill_toolkits.py:412`）：
```python
name = Path(target).name if resolved_source == "skillnet" else target
# 对 URL 形如 https://example.com/skills/my-skill，取 "my-skill"
```

**Go 问题**（`skill_toolkit.go:396`）：
```go
name = identifier  // ❌ 统一用 identifier 作为 fallback
```

**影响**：SkillNet 的 URL 形如 `https://example.com/skills/my-skill`，Python 取 `my-skill`，Go 用完整 URL 作为名称，导致安装后的技能名称不可读。

**修复方案**：
```go
var name string
if normalizedSource == "skillnet" {
    name = path.Base(identifier)  // 对齐 Python: Path(target).name
} else {
    name = identifier
}
```

---

### 问题 14：skilltoolkit HandleSkillsUninstall error 被丢弃

**Go 问题**（`skill_toolkit.go`）：
```go
payload, _ := tk.manager.HandleSkillsUninstall(ctx, args)
// ❌ error 返回值被丢弃
```

**Python 参考**（`skill_toolkits.py:476-483`）：
```python
try:
    payload = await self._manager.handle_skills_uninstall(params)
except Exception as exc:
    return {"success": False, "detail": str(exc)}
```

**影响**：卸载失败时 LLM 收到的是空/错误 payload 而不是明确的失败信息。

**修复方案**：
```go
payload, err := tk.manager.HandleSkillsUninstall(ctx, args)
if err != nil {
    return map[string]any{"success": false, "detail": err.Error()}
}
```

---

### 问题 19：CleanTeam 中 ERROR 状态成员判断不一致

**分类**：🔴 严重 — ERROR 状态成员在 Go 中被视为"已关闭"，Python 不允许

**Python 参考**（`agent_teams/tools/team.py:684`）：
```python
if member_data.status != MemberStatus.SHUTDOWN.value:
    # Python 只认 SHUTDOWN 一种终态
    # ERROR 状态的成员会阻止 clean_team
```

**Go 问题**（`team_backend.go:599-600`）：
```go
if m.Status != string(atschema.MemberStatusShutdown) &&
    m.Status != string(atschema.MemberStatusError) {
    // ❌ Go 同时允许 SHUTDOWN 和 ERROR 状态
```

**影响**：Python 只认 `SHUTDOWN` 一种终态。如果一个成员因异常进入 ERROR 状态，Python 的 `clean_team` 会因为"not all members are shutdown"而返回 False，阻止清理。Go 则允许 ERROR 状态通过，可能导致 Python 中无法清理的团队在 Go 中被清理，数据状态不一致。

**修复方案**：对齐 Python 只检查 `SHUTDOWN`，删除 `MemberStatusError` 的判断。如果确实需要允许 ERROR 状态清理，应在注释中说明理由。

---

## 五、补充一般问题详情

### 问题 15：EditMemoryWithContext 英文错误消息残留

**Go 位置**（`tool_ops.go:267`）：
```go
"oldText appears %d times in file. Be more specific."  // 仍是英文
```

**修复方案**：改为 `"old_text 出现 %d 次，请更精确地指定"`，对齐 80f87475 提交的中文化统一规范。

---

### 问题 16：write_memory 追加换行行为不一致

**Python 参考**（`memory_tools.py:328-329`）：
```python
f.write(content)
f.write("\n")  # ✅ 无论创建还是追加都写末尾换行
```

**Go 问题**（`tool_ops.go:210-212`）：追加模式时在内容前加换行，但未在内容后追加换行。

**修复方案**：确认 FsOperation 写入行为是否自动追加换行，若非则对齐 Python 在 content 末尾加 `\n`。

---

### 问题 17：skilltoolkit auto 模式排序硬编码

**Python 参考**（`skill_toolkits.py:230`）：
```python
sources = sorted(_SUPPORTED_SOURCES)  # ✅ 动态排序
```

**Go 问题**（`skill_toolkit.go:214`）：
```go
sources = []string{"clawhub", "skillnet", "teamskillshub"}  // 硬编码
```

**修复方案**：使用 `sort.Strings(sources)` 动态排序，确保新增 source 时行为一致。

---

### 问题 18：HeaderQuery 认证空配置返回 Success=true

**Go 位置**（`auth_callback.go:178-194`）：当 auth_headers 和 auth_query_params 都为 nil 时返回 `{Success: true, AuthData: {"auth_provider": nil}}`。

**影响**：调用方拿到 nil 的 auth_provider 但不知道该怎么处理。

**修复方案**：当 headers 和 query_params 都为空时，设置明确的 Message 说明"无需额外认证"。

---

### 问题 22：ForceCleanTeam 返回值始终为 true（不检查失败）

**Python 参考**（`agent_teams/tools/team.py:751-761`）：
```python
success = await self.db.force_delete_team_session(self.team_name)
try:
    await self._remove_cleanup_paths()
except Exception as e:
    success = False  # ✅ 清理路径失败会将结果改为 False
if success:
    team_logger.info(...)
return success
```

**Go 问题**（`team_backend.go:629-658`）：
```go
func (tb *TeamBackend) ForceCleanTeam(...) (bool, error) {
    tb.db.ForceDeleteTeamSession(ctx, tb.teamName)  // ❌ 无返回值检查
    tb.RemoveCleanupPaths(ctx)                       // ❌ 失败不影响返回值
    return true, nil                                 // ❌ 始终返回 true
```

**影响**：Go 始终返回 `(true, nil)`，而 Python 根据 `ForceDeleteTeamSession` 和 `RemoveCleanupPaths` 的结果返回 `bool`。调用方无法知道清理是否真正成功。

**修复方案**：检查 `ForceDeleteTeamSession` 的返回值，`RemoveCleanupPaths` 失败时将结果设为 false。

---

### 问题 23：CancelMember 消息发送失败仍返回 Success

**Python 参考**（`agent_teams/tools/team.py:645-650`）：
```python
success = await self.message_manager.send_message(
    content=t("team.cancel_request_content"), to_member_name=member_name
)
if not success:
    team_logger.error(f"Failed to send cancel request message to member {member_name}")
    return False  # ✅ 发送失败返回 False
```

**Go 问题**（`team_backend.go:480`）：
```go
_, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
// ❌ 忽略了发送结果，始终返回 Success
```

**影响**：Python 在消息发送失败时返回 False，Go 即使发送失败也返回 `MemberOpResultSuccess()`，调用方无法得知取消消息是否成功送达。

**修复方案**：检查 SendMessage 的返回值，发送失败时返回 `MemberOpResultFail`。

---

### 问题 24：RegisterCleanupPath 用 ExpandEnv 而非展开 ~

**Python 参考**（`agent_teams/tools/team.py:204`）：
```python
self._cleanup_paths.add(str(Path(path).expanduser()))
# ✅ expanduser() 展开 ~ 为 home 目录
```

**Go 问题**（`team_backend.go:855`）：
```go
expanded := filepath.Clean(os.ExpandEnv(path))
// ❌ os.ExpandEnv 不处理 ~，只展开环境变量
```

**影响**：如果路径包含 `~/xxx`，Python 会正确展开为 `/home/user/xxx`，Go 不会（`os.ExpandEnv` 不处理 `~`），可能导致清理路径指向错误位置，清理操作无法执行。

**修复方案**：
```go
func (tb *TeamBackend) RegisterCleanupPath(path string) {
    if path == "" {
        return
    }
    expanded := filepath.Clean(os.ExpandEnv(path))
    // 对齐 Python: Path.expanduser() — 展开 ~ 为 $HOME
    if strings.HasPrefix(expanded, "~/") {
        if home, err := os.UserHomeDir(); err == nil {
            expanded = filepath.Join(home, expanded[2:])
        }
    }
    tb.cleanupPaths[expanded] = struct{}{}
}
```

---

## 六、已确认的正确修复（a7324ead 提交）

| 修复项 | 状态 | 说明 |
|--------|------|------|
| TaskPlan 删除 TaskName 字段 | ✅ 正确 | Python 只有 goal |
| OFFLOAD 标记改为单括号 | ✅ 正确 | 对齐 Python reload.py |
| builtin_rules 第10条删除 severity | ✅ 正确 | 对齐 Python |
| AudioModelConfig 删除 AUDIO_MODEL_NAME | ✅ 正确 | Python QAModel 只读 AUDIO_QUESTION_ANSWERING_MODEL |
| PromptReport.Summary 加前缀 | ✅ 正确 | 对齐 Python 格式 |
| reload EN 版精简文案 | ✅ 正确 | 对齐 Python |

---

## 七、待回填代码检查

| 位置 | 标记 | 状态 | 说明 |
|------|------|------|------|
| fragment_manager.go AddMemories | ⤵️ 7.8 | 🔴 未实现 | MemUpdateChecker 仍是 stub（直接返回 ADD 无冲突），LLM 驱动的冲突检查未实现 |
| fragment_manager.go convertToMemoryDoc | ⤵️ 7.8 | 🟡 部分实现 | 设计决策注释已补充，Fields 保持 map[string]any，等 7.8 回填时评估 |
| ability_manager.go IntentRecognizer | ⤵️ 6.23 | 🔴 未实现 | IntentRecognizer 仍是骨架，LLM 调用未实现 |
| spawn_manager.go BuildContextFromDB | TODO #9.64 | 🔴 未实现 | 返回空上下文 |
| spawn_manager.go PublishRestartEvent | TODO #9.65 | 🔴 未实现 | 当前为 no-op |
| spawn_manager.go RestartTeammate | ⤵️ 待回填 | 🔴 未实现 | initialMessage 和 sessionID 未实现 |
| member.go TeamMember | TODO 桩 | 🔴 未实现 | Status/ExecutionStatus/UpdateStatus/UpdateExecutionStatus 全部是桩实现 |

---

## 八、修复优先级建议

### 立即修复（严重）
1. **问题 3+5**：ShutdownMember 增加 force 参数 + ForceCleanTeam 传 force=true
2. **问题 2**：删除 ShutdownMember 中的 CancelAllTasks
3. **问题 1**：RefreshHumanAgentRoster 补充 db.Initialize 调用
4. **问题 19**：CleanTeam 对齐 Python 只检查 SHUTDOWN 状态

### 尽快修复（一般）
5. **问题 7**：reload.go 删除 filesystem 类型
6. **问题 8**：TaskPlan.ToMarkdown 对齐 Python 格式
7. **问题 9**：RebuildContentWithFrontmatter 排序修复
8. **问题 4**：ForceCleanTeam 删除 onTeamCleaned 回调
9. **问题 20**：Startup 移除跳过自身逻辑
10. **问题 21**：BuildTeam 注册 Leader 改为通过 SpawnMember 统一处理

### 排期修复（提示）
11. **问题 22**：ForceCleanTeam 返回值检查
12. **问题 23**：CancelMember 消息发送失败返回 Fail
13. **问题 24**：RegisterCleanupPath 展开 ~ 路径
14. 问题 15-18 的提示级问题
