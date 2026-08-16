# 48h 逻辑审查报告 — 2026-08-16

> 审查范围：48 小时内（2026-08-14 ~ 2026-08-16）提交的代码变更
> 审查方法：对照 Python 参考项目，逐一比对 Go 移植代码的方法签名、步骤流程、参数传递
> 涉及章节：7.3 CodingMemoryRail/MemoryRail、9.65a-4 TeamBackend、Skill Toolkit、10.6.1-2 Prompt Builder、10.3.4-6 适配器回填

---

## 一、严重问题（功能影响）

### S-01: CodingMemoryRail.registerCodingMemoryTools — nil pointer panic

**文件**: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:397-404`

**Python 样例**:
```python
# coding_memory_rail.py:180-184
tool_card = getattr(tool, "card", None)
if not tool_card:
    logger.warning(f"[CodingMemoryRail] Tool {tool.__name__} has no card")
    continue
```

**Go 问题**:
```go
toolCard := t.Card()
if toolCard == nil {
    logger.Warn(...).
        Str("tool_name", toolCard.Name).   // ← toolCard 是 nil！panic!
        Msg("工具无 card，跳过注册")
    continue
}
```

当 `toolCard == nil` 时，访问 `toolCard.Name` 会触发 nil pointer panic。

**修复方案**: 在 nil 检查后不访问 `toolCard` 字段，改用工具索引或类型名：
```go
if toolCard == nil {
    logger.Warn(...).Msg("工具无 card，跳过注册")
    continue
}
```

---

### S-02: CodingMemoryRail._init_coding_memory_manager — 缺少 LLM 参数传递

**文件**: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:462-508`、`internal/agentcore/memory/lite/tools.go:44-69`

**Python 样例**:
```python
# coding_memory_rail.py:253-267
llm = None
get_llm_func = getattr(ctx.agent, "_get_llm", None)
if get_llm_func is not None:
    try:
        llm = get_llm_func()
    except ValueError:
        llm = None

manager = await init_memory_manager_async(
    workspace=self.workspace,
    agent_id=agent_id,
    embedding_config=self._embedding_config,
    sys_operation=self.sys_operation,
    llm=llm,  # ← Python 传递了 llm
)
```

```python
# coding_memory_tools.py:46-48
if manager:
    if llm:
        manager.llm = llm  # ← LLM 被设置到 manager
```

**Go 问题**: `InitCodingMemoryManagerAsync` 没有 `llm` 参数，`MemoryIndexManager.llm` 始终为空。即使后续实现 MemUpdateChecker（7.8），没有 LLM 也无法运行冗余判断。

**修复方案**:
1. 在 `InitCodingMemoryManagerAsync` 签名中添加 `llm any` 参数
2. 初始化成功后设置 `mgr.llm = llm`
3. 在 `initCodingMemoryManager` 中从 agent 获取 LLM 实例并传入

---

### S-03: CodingMemoryRail._extract_last_user_query — 逻辑与 Python 完全不同

**文件**: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:727-737`

**Python 样例**:
```python
# coding_memory_rail.py:452-479
def _extract_last_user_query(self, ctx) -> Optional[str]:
    messages = getattr(ctx.inputs, "messages", None) or []
    for msg in reversed(messages):
        if hasattr(msg, "role") and msg.role == "user":
            content = msg.content
            if isinstance(content, str):
                return content
            if isinstance(content, list):
                texts = [p.get("text", "") for p in content
                         if isinstance(p, dict) and p.get("type") == "text"]
                return " ".join(texts) if texts else None
    return None
```

**Go 问题**:
```go
func (r *CodingMemoryRail) extractLastUserQuery(cbc *agentinterfaces.AgentCallbackContext) string {
    inputs := cbc.Inputs()
    invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs)
    if !ok {
        return ""
    }
    if invokeInputs.Query == nil {
        return ""
    }
    return invokeInputs.Query.PlainText()
}
```

Python 从 `ctx.inputs.messages` 列表中倒序查找最后一条 `role == "user"` 的消息，支持多模态内容提取。Go 直接从 `invokeInputs.Query.PlainText()` 获取。如果 `InvokeInputs.Query` 等价于 Python 的 `InvokeInputs.query`（Python 的 `InvokeInputs` 也有 `query` 字段），则行为可能一致。但 Python 的 `_extract_last_user_query` 访问的是 `messages` 而非 `query`，这两个字段在 `before_invoke` 回调中可能不同。

**修复方案**: 确认 `InvokeInputs.Query` 是否与 Python 的 `messages` 列表中的最后一条用户消息等价。如果 `Query` 是 `before_invoke` 时的用户查询文本，则行为等价；否则需要改为从消息列表中提取。

---

### S-04: CodingMemoryRail.buildCodingMemoryRail — codingMemoryDir 路径可能使用错误的 workspace

**文件**: `internal/swarm/server/adapter/code_adapter.go:895-900`

**Python 样例**:
```python
# interface_code.py:139-140
agent_workspace_dir = self._agent_workspace_dir  # = str(get_agent_workspace_dir())
project_name = os.path.basename(project_dir) if project_dir else "default"
coding_memory_dir = os.path.join(agent_workspace_dir, "coding_memory", project_name)
```

Python 中 `_agent_workspace_dir` 和 `_workspace_dir` 是两个不同的值：
- `_workspace_dir` = 项目目录（用于 LspTool、Workspace root_path）
- `_agent_workspace_dir` = 系统数据目录（用于 coding_memory、todo 文件等）

**Go 问题**:
```go
agentWorkspaceDir := workspace.AgentRootDir()
```

Go 的 `workspace.AgentRootDir()` 返回 `WorkspaceDir()/agent`，需要确认这是否等价于 Python 的 `get_agent_workspace_dir()`。如果 `AgentRootDir()` 返回的是项目目录而非系统数据目录，coding_memory 文件会被写入用户项目目录，污染项目。

**修复方案**: 确认 `workspace.AgentRootDir()` 与 Python 的 `get_agent_workspace_dir()` 返回值是否一致。如果 `AgentRootDir()` 等于 `WorkspaceDir()/agent`，而 Python 的 `get_agent_workspace_dir()` 等于 `get_user_workspace_dir()/agent/home`，则路径不一致，需要修正。

---

### S-05: TeamBackend.shutdown_member — 缺少消息发送 + 错误的幂等行为

**文件**: `internal/agent_teams/tools/team_backend.go:387-414`

**Python 样例**:
```python
# team.py:514-598
if current_status == MemberStatus.SHUTDOWN or current_status == MemberStatus.SHUTDOWN_REQUESTED:
    return MemberOpResult.success()  # ← Python: 幂等返回 success

# CAS transition 成功后，发送 shutdown 消息
msg_id = await self.message_manager.send_message(
    content=t("team.shutdown_request_content"),
    to_member_name=member_name,
)
# 然后发布事件
```

**Go 问题**:
1. 对 SHUTDOWN/SHUTDOWN_REQUESTED 状态返回 `fail` 而非 `success`（Python 幂等返回 success）
2. 完全缺少 `messageManager.SendMessage()` 调用 — 不发送 shutdown 消息给成员
3. 只发布事件，不发送消息

**修复方案**:
1. 将 SHUTDOWN/SHUTDOWN_REQUESTED 的返回值改为 `MemberOpResultSuccess()`
2. 在 CAS transition 后添加 `messageManager.SendMessage()` 调用
3. 然后发布事件

---

### S-06: TeamBackend.cancel_member — 逻辑与 Python 完全不同

**文件**: `internal/agent_teams/tools/team_backend.go:426-455`

**Python 样例**:
```python
# team.py:600-663
# 只对 BUSY 状态的成员操作
if member_data.status != MemberStatus.BUSY:
    return True  # 非 BUSY 直接返回

# 重置 CLAIMED 的任务
for task in tasks:
    if task.status == "CLAIMED":
        await self.task_manager.reset(task.id)

# 发送取消消息
await self.message_manager.send_message(
    content=t("team.cancel_request_content"),
    to_member_name=member_name,
)
# 发布事件
# 注意：不改变成员状态为 SHUTDOWN_REQUESTED
```

**Go 问题**:
1. 对任何非 terminal 状态的成员都操作（Python 只对 BUSY）
2. 将成员状态改为 SHUTDOWN_REQUESTED（Python 不改状态）
3. 用 `db.Task().ResetTask()` 而非 `taskManager.reset()`
4. 没有发送取消消息

**修复方案**: 重写 `CancelMember`：
- 仅对 BUSY 成员操作
- 不改变成员状态
- 使用 `taskManager.Reset()` 重置 CLAIMED 任务
- 添加 `messageManager.SendMessage()` 调用

---

### S-07: TeamBackend.clean_team — 不跳过 self 导致无法清理

**文件**: `internal/agent_teams/tools/team_backend.go:553-586`

**Python 样例**:
```python
# team.py:665-727
for member_data in all_members:
    if member_data.member_name == self.member_name:  # ← 跳过 self
        continue
    if member_data.status not in (MemberStatus.SHUTDOWN, MemberStatus.ERROR):
        return False
```

**Go 问题**: 不跳过 `self`（leader），要求所有成员包括 leader 都处于 SHUTDOWN 状态。但 leader 永远不会进入 SHUTDOWN 状态，导致 `clean_team` 永远返回 false。

**修复方案**: 在循环中添加 `m.MemberName != tb.memberName` 检查，跳过 leader 自己。

---

### S-08: TeamBackend.force_clean_team — 直接更新状态而非调用 shutdown_member

**文件**: `internal/agent_teams/tools/team_backend.go:590-612`

**Python 样例**:
```python
# team.py:729-761
for member_data in all_members:
    if member_data.member_name != self.member_name:
        await self.shutdown_member(member_data.member_name, force=True)
```

**Go 问题**: 直接用 `UpdateMemberStatus` 更新状态为 SHUTDOWN，跳过了 shutdown 消息发送、事件发布等生命周期。

**修复方案**: 改为调用 `ShutdownMember`（添加 `force` 参数支持）。

---

### S-09: TeamBackend.startup/startup_member — 缺少 on_created 回调和 MemberSpawnedEvent

**文件**: `internal/agent_teams/tools/team_backend.go:344-375`

**Python 样例**:
```python
# team.py:336-396
async def startup(self, *, on_created=None):
    # CAS transition...
    if on_created:
        await on_created(member_name)
    # 发布 MemberSpawnedEvent
    # 失败时回滚 STARTING→UNSTARTED
```

**Go 问题**:
1. 没有 `onCreated` 回调参数 — 调用方无法在启动时生成 Agent
2. 没有发布 `MemberSpawnedEvent`
3. 没有失败回滚逻辑

**修复方案**: 添加 `onCreated func(memberName string) error` 参数，发布 `MemberSpawnedEvent`，添加失败回滚。

---

### S-10: TeamBackend.cancel_task — 缺少通知消息

**文件**: `internal/agent_teams/tools/team_backend.go:618-640`

**Python 样例**:
```python
# team.py:851-896
task_info = await self.task_manager.get_task(task_id)
# ... 取消任务后
await self.message_manager.send_message(
    content=f"Task '{task_info.title}' (ID: {task_id}) has been cancelled by the team leader.",
    to_member_name=task_info.assignee,
)
```

**Go 问题**: 取消任务后不通知被分配者。

**修复方案**: 添加 `messageManager.SendMessage()` 通知被分配者。

---

### S-11: TeamBackend.cancel_all_tasks — 缺少广播消息

**文件**: `internal/agent_teams/tools/team_backend.go:644-651`

**Python 样例**:
```python
# team.py:898-935
# 取消所有任务后
await self.message_manager.broadcast_message(
    content=f"All tasks ({count}) have been cancelled by team leader."
)
```

**Go 问题**: 取消所有任务后不广播通知。

**修复方案**: 添加 `messageManager.BroadcastMessage()` 广播。

---

### S-12: TeamBackend.build_team — 缺少 CreateTeam 错误检查 + allocation 参数

**文件**: `internal/agent_teams/tools/team_backend.go:472-548`

**Python 样例**:
```python
# team.py:937-1083
success = await db.team.create_team(...)
if not success:
    raise RuntimeError("Team already exists or creation failed")
# 传递 allocation 对象给 spawn_member
```

**Go 问题**:
1. `CreateTeam` 没有错误检查 — 如果团队已存在则静默忽略
2. 预定义成员没有传递 `allocation` 对象给 `SpawnMember`

**修复方案**: 添加 `CreateTeam` 返回值检查，失败时返回错误。为预定义成员传递 allocation。

---

### S-13: SkillToolkit.InstallSkill — skillnet 调用错误方法

**文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go:248-251`

**Python 样例**:
```python
# skill_toolkits.py:383-384
if resolved_source == "skillnet":
    payload = await self._install_skillnet_sync_wait(target, wait_timeout)
```

Python 的 `_install_skillnet_sync_wait` 调用 `handle_skills_skillnet_install({"url": identifier, "force": False})`，然后轮询 `handle_skills_skillnet_install_status` 直到完成或超时。

**Go 问题**:
```go
case "skillnet":
    payload, _ = tk.manager.HandleSkillsInstall(ctx, map[string]any{
        "url": identifier, "force": false, "source": "skillnet",
    })
```

1. 调用了 `HandleSkillsInstall`（marketplace 安装方法，期望 `spec` 格式为 `name@marketplace`），而不是 `HandleSkillsSkillnetInstall`
2. 传入 `{"url": identifier, ...}` 时 `spec` 为空，会返回 "spec 格式应为 skill@marketplace" 错误
3. 缺少轮询等待逻辑

**修复方案**: 对 skillnet 源改用 `HandleSkillsSkillnetInstall`，并实现轮询等待逻辑。

---

### S-14: SkillToolkit.InstallSkill — skillnet name 回退逻辑缺失

**文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go:276-279`

**Python 样例**:
```python
# skill_toolkits.py:409-412
name = str(skill.get("name", "")).strip()
if not name:
    name = Path(target).name if resolved_source == "skillnet" else target
```

**Go 问题**:
```go
name := strings.TrimSpace(toString(skill["name"]))
if name == "" {
    name = identifier  // ← 对 skillnet 源，identifier 是完整 URL
}
```

对于 skillnet，identifier 是 URL（如 `https://example.com/skills/my-skill`），Python 用 `Path(target).name` 提取 `my-skill`，Go 用完整 URL。

**修复方案**: 当 `source == "skillnet"` 且 `name` 为空时，使用 `filepath.Base(target)` 代替 `identifier`。

---

### S-15: SkillManager.resolveLocalSkillDir — 缺少 SKILL.md 元数据回退搜索

**文件**: `internal/swarm/server/runtime/skill/skill_manager.go:1874-1880`

**Python 样例**:
```python
# skill_manager.py:2438-2459
def _resolve_local_skill_dir(self, skill_name: str) -> Path | None:
    direct = _safe_child_path(self._skills_dir, skill_name, "skill")
    if direct.is_dir():
        return direct
    # 回退：遍历所有子目录，通过 SKILL.md 的 name 字段匹配
    for child in self._skills_dir.iterdir():
        if not child.is_dir() or child.name.startswith("_"):
            continue
        md = self._try_find_skill_file(child)
        if md is None:
            continue
        meta = self._parse_skill_md(md)
        if meta and meta.get("name") == skill_name:
            return child
    return None
```

**Go 问题**: 只检查 `skillsDir/name` 是否存在。当目录名与 SKILL.md 中的 name 不一致时（如目录名 `my-skill-v2` 但 SKILL.md 中 name 是 `my-skill`），Go 找不到该技能。

**修复方案**: 在 `resolveLocalSkillDir` 中添加回退搜索逻辑：遍历 `skillsDir` 子目录，通过 SKILL.md 的 name 字段匹配。

---

### S-16: CodeAdapter._build_configured_subagents — 不应加载自定义 subagent

**文件**: `internal/swarm/server/adapter/code_adapter.go:552-553`

**Python 样例**:
```python
# interface_code.py:761-764
# ── 自定义 agent 不加入 deep_config.subagents ──
# Code 模式下，自定义 agent 由 CodeAgentRail 的 Agent 工具管理，
# 不走 SubagentRail 的 task_tool 路径。
# （agent.plan / agent.fast 模式仍由 interface_deep.py 的 _load_custom_subagents 管理）
```

**Go 问题**:
```go
customSpecs := c.deep.loadCustomSubagents(subagentsCfg)
specs = append(specs, customSpecs...)
```

Code 模式下不应加载自定义 subagent，这会导致自定义 agent 被错误注册为 SubagentRail 的子代理，而非 CodeAgentRail 的工具。

**修复方案**: 移除 `c.deep.loadCustomSubagents(subagentsCfg)` 调用，或添加注释说明 Code 模式不加载自定义 subagent。

---

### S-17: CodeAdapter.CreateInstance — 缺少 _jiuwenswarm_code_project_dir 和 _jiuwenswarm_project_dir 属性设置

**文件**: `internal/swarm/server/adapter/code_adapter.go:318-320`

**Python 样例**:
```python
# interface_code.py:302-312
setattr(self._instance, "_jiuwenswarm_adapter_mode", "code")
setattr(self._instance, "_jiuwenswarm_code_project_dir", self._project_dir or self._workspace_dir)
setattr(self._instance, "_jiuwenswarm_project_dir", self._project_dir or self._workspace_dir)
```

**Go 问题**: 步骤 21.1 只注释了 `_jiuwenswarm_adapter_mode` 的回填标记，但完全遗漏了 `_jiuwenswarm_code_project_dir` 和 `_jiuwenswarm_project_dir`。Python 中这两个属性被 `configure_team_member_agent` 等多处使用（如 `_resolve_member_workspace_root` 会读取 `parent_agent._jiuwenswarm_code_project_dir`），缺失会导致 Team 模式下成员 Agent 无法解析项目目录。

**修复方案**: 在步骤 21.1 处添加注释标记，明确列出三个缺失的 setattr：
```go
// ⤵️ 10.6.3-10: 待 DeepAgent 实例属性扩展后回填
//   setattr(instance, "_jiuwenswarm_adapter_mode", "code")
//   setattr(instance, "_jiuwenswarm_code_project_dir", projectDir or workspaceDir)
//   setattr(instance, "_jiuwenswarm_project_dir", projectDir or workspaceDir)
```

---

## 二、一般问题

### M-01: CodeAdapter.CreateInstance — 缺少 load_dotenv 调用

**文件**: `internal/swarm/server/adapter/code_adapter.go:164-191`

**Python 样例**:
```python
# interface_code.py:230（继承自 DeepAdapter 的 create_instance）
load_dotenv(dotenv_path=get_env_file(), override=True)
```

**Go 问题**: DeepAdapter.CreateInstance 步骤 4 调用了 `dotenv.Load()`，但 CodeAdapter 完全重写了 `CreateInstance`，没有调用 dotenv 加载。

**修复方案**: 在步骤 3 之前添加 dotenv 加载：
```go
if err := dotenv.Load(workspace.EnvFile()); err != nil {
    logger.Warn(logComponent).Err(err).Msg("load_dotenv 失败，继续使用当前环境变量")
}
```

---

### M-02: CodeAdapter.CreateInstance — 缺少 _refresh_multimodal_configs 调用

**文件**: `internal/swarm/server/adapter/code_adapter.go:192`

**Python 样例**:
```python
# interface_code.py:232
self._refresh_multimodal_configs(config_base)
```

**Go 问题**: 步骤 4 标注了 `⤵️ 10.6.24`，但 DeepAdapter 已有 `refreshMultimodalConfigs` 方法。CodeAdapter 应调用 `c.deep.refreshMultimodalConfigs(configBase)`。

**修复方案**: 添加 `c.deep.refreshMultimodalConfigs(configBase)` 调用。

---

### M-03: CodingMemoryRail countMemoryFiles — 缺少 recursive=False 参数

**文件**: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:701`

**Python 样例**:
```python
# coding_memory_rail.py:512-514
result = await self.sys_operation.fs().list_files(
    memory_dir, recursive=False
)
```

**Go 问题**:
```go
listResult, err := fsOp.ListFiles(ctx, r.codingMemoryDir)
```

没有显式传 `recursive=False`。同样影响 `snapshotMemoryFiles`（`coding_memory_tool_ops.go:485`）。

**修复方案**: 显式传 `sysop.WithFsRecursive(false)` 选项。

---

### M-04: CodingMemoryRail before_invoke — manager_initialized 设置时机与 Python 不一致

**文件**: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:199-201`

**Python 样例**:
```python
# coding_memory_rail.py:217-219
if not self._manager_initialized:
    await self._init_coding_memory_manager(ctx)
    self._manager_initialized = True  # ← 无论成功失败都设为 True
```

**Go 问题**: Go 在 `initCodingMemoryManager` 内部仅在 `mgr != nil` 时设置 `managerInitialized = true`。如果初始化失败，Go 会在每次 `before_invoke` 时重试，而 Python 不会重试。

**修复方案**: Go 的行为更合理（失败后重试），但需确认是否与 Python 行为一致。如果需要与 Python 一致，应在调用后无条件设置 `managerInitialized = true`。

---

### M-05: TeamBackend.approve_plan — 签名和实现与 Python 完全不同

**文件**: `internal/agent_teams/tools/team_backend.go:655-673`

**Python 样例**:
```python
# team.py:398-462
async def approve_plan(self, plan_id, approved, feedback=None):
    record = await self.task_manager.get_plan_record(plan_id)
    member_name = record.member_name
    task_id = record.task_id
    # 验证成员存在
    await self.task_manager.approve_plan(plan_id, approved, feedback, self.member_name)
```

**Go 问题**: Go 接收 `taskID` 而非 `planID`，没有 plan record 查找，没有成员验证。

**修复方案**: 对齐 Python — 接收 `planID` 参数，查找 plan record，验证成员。

---

### M-06: TeamBackend.approve_tool — 缺少成员存在性检查

**文件**: `internal/agent_teams/tools/team_backend.go:677-688`

**Python 样例**:
```python
# team.py:464-512
member_data = await db.member.get_member(member_name)
if member_data is None:
    return False
```

**Go 问题**: 不检查成员是否存在，始终返回成功。

**修复方案**: 添加成员存在性检查。

---

### M-07: TeamBackend.spawn_human_agent — 缺少 i18n 默认值

**文件**: `internal/agent_teams/tools/team_backend.go:694-704`

**Python 样例**:
```python
# team.py:1085-1154
if display_name is None:
    display_name = t("hitt.human_agent_display_name")
if desc is None:
    desc = t("hitt.human_agent_default_persona")
```

**Go 问题**: 传空字符串，没有 i18n 默认值。

**修复方案**: 添加 i18n 默认值。

---

### M-08: SkillManager.locateSkillDir — 搜索策略与 Python 不完全一致

**文件**: `internal/swarm/server/runtime/skill/skill_manager.go:2041-2063`

**Python 样例**:
```python
# skill_manager.py:2641-2647
# 先精确搜索 SKILL.md
for md in path.rglob("SKILL.md"):
    if md.is_file():
        return md.parent
# 再模糊搜索 *.md
for md in path.rglob("*.md"):
    if md.is_file() and md.name.lower() == "skill.md":
        return md.parent
```

**Go 问题**: Go 单步搜索，对所有文件做大小写不敏感匹配。可能返回小写 `skill.md` 的结果，即使同目录下有精确匹配的 `SKILL.md`。

**修复方案**: 分两步搜索，先匹配精确 `SKILL.md`，再匹配大小写不敏感。

---

### M-09: SkillToolkit.SearchSkill/InstallSkill/UninstallSkill — 缺少顶层异常捕获

**文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go`

**Python 样例**:
```python
# skill_toolkits.py:209-281
async def search_skill(self, query, source, limit):
    try:
        # ... 正常逻辑
    except Exception as exc:
        logger.exception("search_skill failed")
        return {"success": False, "source": str(source), "items": [], "detail": str(exc)}
```

**Go 问题**: 没有 `defer/recover` 保护，panic 会导致程序崩溃。

**修复方案**: 在三个方法中添加 `defer func() { if r := recover(); r != nil { ... } }()` 捕获 panic，返回结构化错误响应。

---

### M-10: CodeAdapter.getToolCards — 缺少 acp_chat 工具映射

**文件**: `internal/swarm/server/adapter/code_adapter.go:578-609`

**Python 样例**:
```python
# interface_code.py:934-965
# _get_tool_build_names 包含 "acp_chat"
```

**Go 问题**: 缺少 `acp_chat` 工具的处理分支。

**修复方案**: 添加 `acp_chat` 工具的处理分支（标注 `⤵️ 10.6.24`）。

---

### M-11: SpawnManager.build_context_from_db — 返回空（stub）

**文件**: `internal/agent_teams/agent/spawn_manager.go:295-303`

**Python 样例**:
```python
# spawn_manager.py:218-268
# 完整实现：读取成员 DB、解析 model_ref_json、解析 member_model、构建 TeamRuntimeContext
```

**Go 问题**: 返回空 `TeamRuntimeContext` — stub。

**修复方案**: 实现完整逻辑匹配 Python。

---

### M-12: SpawnManager.on_teammate_unhealthy — 缺少 RESTARTING 状态更新

**文件**: `internal/agent_teams/agent/spawn_manager.go:261-290`

**Python 样例**:
```python
# spawn_manager.py:205-216
await self.cleanup_teammate(member_name)
await db.update_member_status(member_name, MemberStatus.RESTARTING)
await self.restart_teammate(member_name)
```

**Go 问题**: 不调用 `cleanup_teammate`，不更新状态为 RESTARTING，直接重启。

**修复方案**: 添加 `cleanup_teammate` 和 `RESTARTING` 状态更新。

---

## 三、提示问题

### L-01: CodingMemoryWriteTool — content 为空字符串时验证与 Python 不一致

**文件**: `internal/agentcore/harness/tools/coding_memory/coding_memory_tool.go:94-109`

**Python**: `if content is None`（只检查 None，不检查空字符串）
**Go**: `if content == ""`（拒绝空字符串）

Python 允许空字符串通过到下游，Go 在工具层就拒绝了。实际影响较小，因为空字符串内容确实无意义。

---

### L-02: CodingMemoryEditTool — new_text 为空时验证与 Python 不一致

**文件**: `internal/agentcore/harness/tools/coding_memory/coding_memory_tool.go:120-141`

**Python**: `if new_text is None`（只检查 None）
**Go**: `if newText == ""`（拒绝空字符串）

Python 允许 `new_text=""` 来删除内容，Go 不允许。但这是有意的设计差异。

---

### L-03: MemoryRail before_model_call — todayDate 使用本地时间而非北京时间

**文件**: `internal/agentcore/harness/rails/memory/memory_rail.go:208`

**Python**: `today_date = _get_beijing_date()`（UTC+8）
**Go**: `todayDate := time.Now().Format("2006-01-02")`（本地时间）

如果运行环境不在北京时区，日期可能不一致。

---

### L-04: CodingMemoryRail countMemoryFiles — .md 文件名判断有冗余条件

**文件**: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:713-715`

**Python**: `if not f.name.lower().endswith(".md")`
**Go**: `!strings.EqualFold(f.Name, ".md") && !strings.HasSuffix(strings.ToLower(f.Name), ".md")`

第一个条件被第二个条件覆盖，冗余但不会导致错误。

---

### L-05: SkillToolkit.newInstallSkillTool — 缺少 market_url 参数定义

**文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go:709-740`

工具 schema 只有 `identifier`、`source`、`timeout_sec` 三个参数，但 `InstallSkill` 方法实际读取了 `inputs["market_url"]`。模型不会传入这个值。

---

### L-06: CodingMemoryRail Uninit — 未清理 manager 引用

**文件**: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:172-176`

设置 `r.managerInitialized = false` 但没有 `r.manager = nil`。Python 也没有显式清理，两者行为一致。但 `r.manager` 未被清理可能导致 manager 对象无法被 GC 回收。

---

## 四、⤵️ 回填标记状态汇总

### 已确认真正未实现的 ⤵️ 标记（48h 内变更区域）

| 区域 | ⤵️ 标记 | 状态 |
|------|---------|------|
| 7.3 CodingMemoryRail | `⤵️ 7.8 MemUpdateChecker — LLM 冗余判断` | 未实现（runChecker 返回空） |
| 7.3 CodingMemoryRail | `⤵️ 7.8 MemUpdateChecker — LLM 冗余判断（prepareAppendMode）` | 未实现 |
| 9.65a-4 TeamBackend | `⤵️ 9.64 GetSharedDB — returns nil` | 未实现 |
| 9.65a-4 TeamBackend | `⤵️ 9.85 GetSharedRuntime — returns nil` | 未实现 |
| 10.3.4-6 CodeAdapter | `⤵️ 10.6.3-10 _jiuwenswarm_adapter_mode` | 未实现 |
| 10.3.4-6 CodeAdapter | `⤵️ 10.6.24 多模态工具 _refresh_multimodal_configs` | 可回填（DeepAdapter 已实现） |
| 10.3.4-6 CodeAdapter | `⤵️ 10.6.3-10 load_user_rails` | 未实现 |
| 10.3.4-6 CodeAdapter | `⤵️ 10.6.3-10 _workspace_path` | 未实现 |

### 已确认标记正确但代码实际已可回填的 ⤵️

| ⤵️ 标记 | 实际状态 | 建议 |
|---------|---------|------|
| `⤵️ 10.6.24 多模态工具 _refresh_multimodal_configs` | DeepAdapter 已实现 `refreshMultimodalConfigs` | 可立即回填：调用 `c.deep.refreshMultimodalConfigs(configBase)` |

---

## 五、修复优先级建议

### P0 — 必须立即修复（会导致 panic 或严重功能错误）

1. **S-01** — nil pointer panic（`coding_memory_rail.go:401`）
2. **S-05** — TeamBackend.shutdown_member 幂等行为 + 缺少消息发送
3. **S-06** — TeamBackend.cancel_member 逻辑完全不同
4. **S-07** — TeamBackend.clean_team 不跳过 self
5. **S-13** — SkillToolkit.InstallSkill skillnet 调用错误方法

### P1 — 尽快修复（功能缺失但不会崩溃）

6. **S-02** — LLM 参数缺失（影响 7.8 MemUpdateChecker 回填）
7. **S-03** — extractLastUserQuery 逻辑差异（需确认等价性）
8. **S-04** — codingMemoryDir 路径可能错误
9. **S-08** — TeamBackend.force_clean_team 直接更新状态
10. **S-09** — TeamBackend.startup 缺少 on_created 回调
11. **S-10** — TeamBackend.cancel_task 缺少通知消息
12. **S-12** — TeamBackend.build_team 缺少错误检查
13. **S-15** — resolveLocalSkillDir 缺少回退搜索
14. **S-16** — CodeAdapter 不应加载自定义 subagent
15. **S-17** — CodeAdapter 缺少 _jiuwenswarm_code_project_dir

### P2 — 计划修复

16. **S-11** — TeamBackend.cancel_all_tasks 缺少广播
17. **S-14** — SkillToolkit.InstallSkill name 回退逻辑
18. **M-01~M-12** — 一般问题
19. **L-01~L-06** — 提示问题
