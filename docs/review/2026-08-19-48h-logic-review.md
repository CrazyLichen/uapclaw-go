# 48小时逻辑审查报告

> **审查日期**: 2026-08-17 ~ 2026-08-19
> **审查范围**: 8月15-17日提交记录覆盖的实现计划章节
> **审查方法**: 逐方法对照 Python 参考项目，检查方法签名、步骤完整性、⤵️标记验证；关键发现经二次验证

---

## 一、审查范围

8月15-17日共 50+ 个提交，涉及以下章节的实现/修复：

| 章节 | 内容 | 审查状态 |
|------|------|---------|
| 7.6 | FragmentMemoryManager | ✅ 已审查 |
| 7.8 | MemUpdateChecker stub | ✅ 已审查（⤵️ 回填标记） |
| 7.9 | 记忆数据模型 (MemoryType/OperationType/FragmentMemoryUnit 等) | ✅ 已审查 |
| 7.3 | CodingMemoryRail / MemoryRail 回填 | ✅ 已审查 |
| 9.65a-4 | TeamBackend 门面 (30+ 方法) | ✅ 已审查 |
| 10.3.7-11 | 适配器辅助 (SkillToolkit/CodeAdapter/DeepAdapter) | ✅ 已审查 |
| 10.6.1-2 | Prompt Builder (Agent/Code 模式提示词) | ✅ 已审查 |
| 10.6.3 | StructuredAskUserRail + StructuredAskUserTool | ✅ 已审查 |

---

## 二、问题汇总

| 严重程度 | 数量 |
|---------|------|
| 严重 | 6 |
| 一般 | 13 |
| 提示 | 12 |

---

## 三、严重问题

### S-01: CodeAdapter.resolveEmbeddingConfig 从错误配置源读取 embed 配置

**Go文件**: `internal/swarm/server/runtime/agent_adapter/code_adapter.go:689-720`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:114-151`
**验证状态**: ✅ 已确认（二次验证通过，同文件 `multimodal_config.go:244` 正确读取 `configBase["embed"]`）

**问题描述**: Go 的 `resolveEmbeddingConfig()` 从 `c.deep.configCache["embed"]` 读取嵌入配置，但 `configCache` 是 `configBase["react"]`（react 子段），而 `embed` 配置位于配置文件的**顶层**（`configBase["embed"]`），不在 `react` 下。这导致 `resolveEmbeddingConfig()` 几乎总是返回 nil，CodingMemoryRail 将始终使用空 EmbeddingConfig，向量搜索功能完全失效。

**Python样例**:
```python
# interface_code.py:114-120
def create_coding_memory_rail(*, project_dir, agent_workspace_dir, config):
    embed_config = config.get("embed") if isinstance(config, dict) else None
    # config = get_config() → 顶层 config.yaml
```

**Go问题**:
```go
// code_adapter.go:695
func (c *CodeAdapter) resolveEmbeddingConfig() *embedding.EmbeddingConfig {
    embedRaw, ok := c.deep.configCache["embed"]  // ← BUG: configCache = configBase["react"]
    // ...
}
```

**对比正确实现**:
```go
// multimodal_config.go:244 — 正确从 configBase 读取
embed, _ := configBase["embed"].(map[string]any)
```

**修复方案**: 将 `c.deep.configCache["embed"]` 改为 `c.deep.configBase["embed"]`，或新增方法从 configBase 读取 embed 配置。需要确保 `CodeAdapter` 能访问到 `configBase`。

---

### S-02: TeamBackend.CancelMember 语义完全偏离 Python — 使用 SHUTDOWN_REQUESTED 而非任务重置

**Go文件**: `internal/agent_teams/tools/team_backend.go:426-455`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:600-663`
**验证状态**: ✅ 已确认

**问题描述**: Go 的 `CancelMember` 使用 CAS 转换 `→SHUTDOWN_REQUESTED`，但 Python 的 `cancel_member` **不修改成员状态**。Python 仅在成员 BUSY 时重置其 CLAIMED 任务并发送取消消息+事件。Go 版本将"取消当前工作"变成了"关闭成员"，语义完全错误。此外缺少 `send_message` 通知。

**Python样例**:
```python
# team.py:600-663
async def cancel_member(self, member_name: str) -> bool:
    if current_status != MemberStatus.BUSY:
        return True  # 不忙，无需取消
    # 重置 CLAIMED 任务
    claimed_tasks = await self.task_manager.get_tasks_by_assignee(...)
    for task in claimed_tasks:
        await self.task_manager.reset(task.task_id)
    # 发送消息
    await self.message_manager.send_message(
        content=t("team.cancel_request_content"), to_member_name=member_name
    )
    # 发布事件（不修改成员状态）
    await self.messager.publish(... MemberCanceledEvent(...))
```

**Go问题**:
```go
// team_backend.go:437-441
ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
    member.Status, string(atschema.MemberStatusShutdownRequested))  // ← Python 不做状态转换
// ... 缺少 send_message 调用
```

**修复方案**:
1. 移除 CAS 转换（cancel 不改变成员状态）
2. 成员 BUSY 时：重置 CLAIMED 任务 → 调用 `messageManager.SendMessage` → 发布 `MemberCanceledEvent`
3. 成员非 BUSY 时：直接返回成功

---

### S-03: TeamBackend.ShutdownMember 缺少幂等性和消息发送

**Go文件**: `internal/agent_teams/tools/team_backend.go:387-414`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:514-598`
**验证状态**: ✅ 已确认

**问题描述**: Go 的 `ShutdownMember` 对已处于 `SHUTDOWN_REQUESTED` 状态的成员返回失败，但 Python 对 `SHUTDOWN` 或 `SHUTDOWN_REQUESTED` 状态返回幂等成功。此外缺少关键的 `send_message` 步骤——成员永远不会收到关闭请求消息。

**Python样例**:
```python
# team.py:514-598
async def shutdown_member(self, member_name: str, force: bool = False) -> MemberOpResult:
    if current_status in (MemberStatus.SHUTDOWN, MemberStatus.SHUTDOWN_REQUESTED):
        return MemberOpResult.success()  # 幂等成功
    # ... 状态转换 ...
    msg_id = await self.message_manager.send_message(
        content=t("team.shutdown_request_content"),
        to_member_name=member_name,
    )
    await self.messager.publish(... MemberShutdownEvent(..., force=force))
```

**Go问题**:
```go
// team_backend.go:393-396
if member.Status == string(atschema.MemberStatusShutdown) ||
    member.Status == string(atschema.MemberStatusError) {
    return atschema.NewMemberOpResultFail("member already in terminal state: " + memberName)
    // ← Python 对 SHUTDOWN/SHUTDOWN_REQUESTED 返回 success
}
// 缺少 send_message 调用
```

**修复方案**:
1. 对 `SHUTDOWN` 和 `SHUTDOWN_REQUESTED` 状态返回 `MemberOpResultSuccess`（幂等）
2. CAS 转换后调用 `tb.messageManager.SendMessage(ctx, shutdownContent, memberName, "")`
3. 添加 `force` 参数，传递给 `MemberShutdownEvent.Force`

---

### S-04: CodingMemoryRail.registerCodingMemoryTools nil card 访问导致 panic

**Go文件**: `internal/swarm/agents/harness/common/rails/coding_memory_rail.go:397-403`
**Python参考**: 无直接对应（Python 不会出现无 card 的工具）
**验证状态**: ✅ 已确认

**问题描述**: nil 检查后立刻访问 nil 指针的字段，会导致运行时 panic。

**Go问题**:
```go
// coding_memory_rail.go:398-402
toolCard := t.Card()
if toolCard == nil {
    logger.Warn(codingMemoryLogComponent).
        Str("event_type", "coding_memory_rail_register_tools").
        Str("tool_name", toolCard.Name).  // ← PANIC: toolCard is nil!
        Msg("工具无 card，跳过注册")
    continue
}
```

**修复方案**: 移除 `toolCard.Name` 引用，或使用工具的其他标识符（如类型名）：
```go
if toolCard == nil {
    logger.Warn(codingMemoryLogComponent).
        Str("event_type", "coding_memory_rail_register_tools").
        Msg("工具无 card，跳过注册")  // 移除 toolCard.Name
    continue
}
```

---

### S-05: code_agent 子 Agent 缺少 CodingMemoryRail 注入

**Go文件**: `internal/swarm/server/runtime/agent_adapter/code_adapter.go:543-561`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:711-717`
**验证状态**: ✅ 已确认（代码中有 ⤵️ 7.8 标注）

**问题描述**: Go 的 code_agent 子 Agent 创建时从未注入 CodingMemoryRail，而 Python 在主 Agent 拥有 CodingMemoryRail 时会将其传递给子 Agent。这导致 code_agent 子 Agent 无法读写编程记忆。

**Python样例**:
```python
# interface_code.py:711-717
code_agent_rails = None
coding_memory_rail = self._coding_memory_rail
if coding_memory_rail is not None:
    code_agent_rails = [SysOperationRail(), coding_memory_rail]
code_spec = build_code_agent_config(model, workspace=..., rails=code_agent_rails, ...)
```

**Go问题**:
```go
// code_adapter.go:543-561
// ⤵️ 7.8: CodingMemoryRail 条件注入（当前 nil 占位）
// Python: if coding_memory_rail is not None: code_agent_rails = [SysOperationRail(), coding_memory_rail]
```

**修复方案**: 在 `buildCodeAgentConfig` 中添加 CodingMemoryRail 条件注入逻辑：
```go
var codeAgentRails []sainterfaces.AgentRail
codeAgentRails = append(codeAgentRails, sysOpRail)
if c.codingMemoryRail != nil {
    codeAgentRails = append(codeAgentRails, c.codingMemoryRail)
}
```

---

### S-06: SkillToolkit.summarizeSearchPayload 缺少字段回退链，ClawHub/TeamSkillsHub 结果 sample 全为空

**Go文件**: `internal/swarm/server/runtime/skill/skill_toolkit.go:695-719`
**Python参考**: `jiuwenswarm/server/runtime/skill/skill_toolkits.py:163-172`
**验证状态**: ✅ 已确认

**问题描述**: Go 只读取 SkillNet 专有字段（`skill_name`/`skill_url`/`skill_description`），而 Python 有完整的回退链。对于 ClawHub 和 TeamSkillsHub 搜索结果，Go 的 sample 字段全部为空字符串，导致日志无法正确展示搜索摘要，且影响 LLM 对搜索结果的判断。

**Python样例**:
```python
# skill_toolkits.py:163-172
"sample": {
    "skill_name": first.get("skill_name")
        or first.get("display_name")
        or first.get("slug")
        or first.get("name")
        or "",
    "skill_url": first.get("skill_url") or "",
    "asset_id": first.get("asset_id") or "",
    "summary": first.get("skill_description") or first.get("summary") or "",
},
```

**Go问题**:
```go
// skill_toolkit.go:712-718
"sample": map[string]any{
    "skill_name": toString(first["skill_name"]),       // ClawHub 没有 skill_name → ""
    "skill_url":  toString(first["skill_url"]),         // ClawHub 没有 skill_url → ""
    "asset_id":   toString(first["asset_id"]),
    "summary":    toString(first["skill_description"]), // ClawHub 没有 skill_description → ""
},
```

**修复方案**: 增加 `normalizeSearchItem` 同款回退链逻辑：
```go
"skill_name": firstStr(first, "skill_name", "display_name", "slug", "name"),
"summary":    firstStr(first, "skill_description", "summary"),
```

---

## 四、一般问题

### M-01: TeamBackend.CancelTask 缺少幂等性和消息通知

**Go文件**: `internal/agent_teams/tools/team_backend.go:618-640`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:851-896`

**问题描述**: Go 缺少任务不存在和已取消的预检查（Python 返回 False/True），且缺少向被分配者发送取消通知消息。

**Python样例**:
```python
async def cancel_task(self, task_id: str) -> bool:
    task = await self.task_manager.get_task(task_id)
    if task is None: return False
    if task.status == TaskStatus.CANCELLED: return True  # 幂等
    result = await self.task_manager.cancel(task_id)
    if result:
        await self.message_manager.send_message(
            content=f"Task '{task.title}' (ID: {task_id}) has been cancelled by the team leader.",
            to_member_name=task.assignee,
        )
```

**修复方案**:
1. 调用 Cancel 前先 GetTask 检查存在性和已取消状态
2. 取消成功后调用 `messageManager.SendMessage` 通知被分配者

---

### M-02: TeamBackend.CancelAllTasks 缺少广播消息

**Go文件**: `internal/agent_teams/tools/team_backend.go:644-651`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:898-935`

**问题描述**: Python 在取消所有任务后向全部成员发送广播消息，Go 缺少此步骤。

**Python样例**:
```python
await self.message_manager.send_message(
    content=f"All tasks ({count}) have been cancelled by team leader.",
    to_member_name="all",
)
```

**修复方案**: 取消完成后调用 `messageManager.SendMessage` 发送 `"all"` 广播。

---

### M-03: TeamBackend.CleanTeam 包含自身且接受 ERROR 终态

**Go文件**: `internal/agent_teams/tools/team_backend.go:553-586`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:665-727`

**问题描述**: Python 的 `clean_team` 跳过自身 (`member_name == self.member_name: continue`)，且只接受 `SHUTDOWN` 状态。Go 包含自身检查，且接受 `SHUTDOWN` 和 `ERROR` 两种终态。

**修复方案**:
1. 添加 `if member.MemberName == tb.leaderMemberName { continue }` 跳过自身
2. 将 ERROR 移出终态判断或改为警告日志

---

### M-04: TeamBackend.ForceCleanTeam 绕过 FSM 直接设置状态

**Go文件**: `internal/agent_teams/tools/team_backend.go:590-612`
**Python参考**: `openjiuwen/agent_teams/tools/team.py:729-761`

**问题描述**: Go 的 `ForceCleanTeam` 直接调用 `UpdateMemberStatus` 设置 SHUTDOWN，绕过了 FSM 校验。Python 调用 `shutdown_member(force=True)` 走完整的 FSM 路径。此外 Go 包含自身，Python 跳过自身。

**修复方案**: 改为调用 `ShutdownMember` (force=true) 或至少跳过自身。

---

### M-05: TeamBackend.Startup/StartupMember 缺少 on_created 回调和 MemberSpawnedEvent

**Go文件**: `internal/agent_teams/tools/team_backend.go:344-375`
**Python参考**: `openjiuwen/agent_teams/tools/team.py` `_spawn_and_publish`

**问题描述**: Python 的 startup 通过 `_spawn_and_publish` 调用 `on_created` 回调来实际创建 Agent 进程，并发布 `MemberSpawnedEvent`。Go 仅有 CAS 转换，缺少实际的 Agent 创建机制和事件通知。

**修复方案**: 添加 `onCreated` 回调机制和 `MemberSpawnedEvent` 发布。注意：这可能是有意架构差异（Go 的 Agent 创建在别处处理），但事件缺失仍需修复。

---

### M-06: StructuredAskUserRail 非结构化路径空字符串行为不一致

**Go文件**: `internal/swarm/server/rails/structured_ask_user_rail.go:264`
**Python参考**: `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py:327-328`

**问题描述**: Python 对非结构化路径的任何字符串（含空字符串）都直接 Reject，Go 对空字符串回退到 parentResolve（最终中断而非 Reject）。

**Python样例**:
```python
elif isinstance(user_input, str):
    return self.reject(tool_result=user_input)  # 含空字符串
```

**Go问题**:
```go
if strInput, ok := userInput.(string); ok && strInput != "" {
    return r.AskUserRail.BaseInterruptRail.Reject(strInput)
}
return r.parentResolve(ctx, cbc, toolCall, userInput, autoConfirmConfig)  // 空字符串走此路径
```

**修复方案**: 移除 `&& strInput != ""` 条件，对空字符串也走 Reject 路径以对齐 Python。

---

### M-07: resolveEmbeddingConfig 缺少嵌入配置不完整的警告日志

**Go文件**: `internal/swarm/server/runtime/agent_adapter/code_adapter.go:931-956`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:132-137`

**问题描述**: Python 在嵌入配置不完整时记录 warning 日志并显式清空 api_key，Go 静默创建空 EmbeddingConfig 无任何警告，难以排查向量搜索失效原因。

**Python样例**:
```python
if not embedding_config_complete:
    embed_api_key = None
    logger.warning(
        "[JiuwenClawCodeAdapter] CodingMemoryRail: incomplete embedding config; "
        "registering tools with memory fallback provider"
    )
```

**修复方案**: 在 `resolveEmbeddingConfig` 返回 nil 或空配置时记录 Warn 日志。

---

### M-08: MemoryRail.BeforeModelCall 模式判断逻辑与 Python 不同

**Go文件**: `internal/swarm/agents/harness/common/rails/memory_rail.go:188-212`
**Python参考**: `jiuwenswarm/agents/harness/common/rails/memory_rail.py:115-131`

**问题描述**: Python 的 `build_memory_section` 接收 `read_only` 和 `is_proactive` 两个独立布尔值，Go 将其映射为单一 `mode` 字符串。当 `is_proactive=False` 且 `is_read_only=False` 时，Python 传递两个 false 由 `build_memory_section` 决定内容，Go 映射为 `"inactive"`，可能导致不同的提示词行为。

**修复方案**: 检查 Python `build_memory_section` 对 `read_only=False, is_proactive=False` 的实际行为，确认 Go 的 `"inactive"` 模式是否等价。如果不等价，改用双布尔参数。

---

### M-09: installSkillnetSyncWait 双重超时问题

**Go文件**: `internal/swarm/server/runtime/skill/skill_toolkit.go:773-815`
**Python参考**: `jiuwenswarm/server/runtime/skill/skill_toolkits.py:283-322`

**问题描述**: Go 的 `context.WithTimeout(ctx, ...)` 继承自父 ctx（可能已有更短 deadline），Python 的 `asyncio.wait_for` 创建独立超时。若父 ctx 有 30s deadline 而 timeoutSec=60，轮询实际只有 30s。

**Python样例**:
```python
async def _install_skillnet_sync_wait(self, identifier, timeout_sec):
    # asyncio.wait_for 创建独立超时，不受外层 deadline 影响
    await asyncio.wait_for(_poll_status(), timeout=timeout_sec)
```

**Go问题**:
```go
pollCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
// ctx 可能已有更短 deadline，导致实际超时 < timeoutSec
```

**修复方案**: 使用 `context.WithTimeout(context.Background(), ...)` 创建独立超时，但需要手动传播取消信号（如通过 select 监听 ctx.Done）。

---

### M-10: CodingMemoryWriteTool 拒绝空内容，Python 允许

**Go文件**: `internal/swarm/agents/harness/tools/coding_memory_tool.go:102-104`
**Python参考**: `openjiuwen/harness/tools/coding_memory.py:53`

**问题描述**: Python 检查 `content is None`（允许空字符串 `""`），Go 检查 `content == ""`（拒绝空字符串）。LLM 可能有意写入空文件来清除内容。

**Python样例**:
```python
content = inputs.get("content")
if content is None:
    return ToolOutput(success=False, error="content is required")
```

**Go问题**:
```go
content, _ := inputs["content"].(string)
if content == "" {
    return nil, fmt.Errorf("content 不能为空")
}
```

**修复方案**: 改为检查 key 是否存在而非值是否为空：
```go
if v, ok := inputs["content"]; !ok {
    return nil, fmt.Errorf("content is required")
}
content, _ := v.(string)
```

---

### M-11: CodingMemoryRail 缺少缓存检查 — 每次创建新实例

**Go文件**: `internal/swarm/server/runtime/agent_adapter/code_adapter.go`
**Python参考**: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py:524-527`

**问题描述**: Python 的 `build_coding_memory_rail` 先检查 `self._coding_memory_rail is not None` 直接复用缓存实例。Go 的 `buildCodingMemoryRail()` 每次都创建新实例，虽然创建后会存入 `c.codingMemoryRail`，但方法入口没有检查已有实例。

**Python样例**:
```python
if self._coding_memory_rail is not None:
    logger.info("[JiuwenClawCodeAdapter] CodingMemoryRail reuse cached instance")
    return self._coding_memory_rail
```

**修复方案**: 在 `buildCodingMemoryRail` 入口添加缓存检查：
```go
if c.codingMemoryRail != nil {
    return c.codingMemoryRail
}
```

---

### M-12: FragmentMemoryManager.list_fragment_memories 缺少排序和 mem_type 校验

**Go文件**: `internal/swarm/agents/harness/common/memory/manage/fragment_manager.go`
**Python参考**: `openjiuwen/core/memory/manage/fragment_memory_manager.py`

**问题描述**: Python 的 `list_fragment_memories` 对结果排序（`mem` 降序 + `timestamp` 降序），且对无效 `mem_type` 记录 error 日志并返回空。Go 缺少排序和校验。

**修复方案**:
1. 添加排序：`sort.Slice(docs, func(i,j) bool { ... })`
2. 添加 mem_type 校验：不在 `FRAGMENT_MEMORY_TYPE` 中时返回 error 或空列表

---

### M-13: DataIdManager 哈希算法与 Python 不同

**Go文件**: `internal/swarm/agents/harness/common/memory/manage/` 相关文件
**Python参考**: `openjiuwen/core/memory/manage/`

**问题描述**: Python 使用内置 `hash()` 函数，Go 使用 FNV-1a。相同 user_id 生成不同 ID。如需跨语言数据兼容（如共享记忆数据库），会导致 ID 冲突。

**影响**: 当前各语言独立运行不受影响，但未来如需迁移 Python 记忆数据到 Go，需注意 ID 不兼容。

---

## 五、提示问题

### T-01: Code Prompt Builder session_guidance 尾部缺 `\n`

**Go文件**: `internal/swarm/agents/harness/code/prompt/code_prompt_builder.go:613`
**Python参考**: `_code_session_guidance_prompt()` 末尾含 `\n`

**问题描述**: Python 三引号字符串自动包含尾部换行，Go 字符串拼接需要显式添加。`session_guidance` 是优先级最高的 section（排在最后），缺少尾部 `\n` 在最终提示词末尾少一个换行符，不影响 `\n\n` section 分隔逻辑。

**修复**: `"will reduce fix rounds after."` → `"will reduce fix rounds after.\n"`

---

### T-02: BuildResponseSection CN/EN 尾部缺 `\n`

**Go文件**: `internal/swarm/agents/harness/common/prompt/prompt_builder.go:60,85`
**Python参考**: `_response_prompt()` 末尾含 `\n`

**修复**: 在 `responseCN` 和 `responseEN` 字符串末尾添加 `\n`

---

### T-03: BuildResponseSection language 参数未使用

**Go文件**: `internal/swarm/agents/harness/common/prompt/prompt_builder.go:35`

**问题描述**: `BuildResponseSection(language string)` 忽略 language 参数，始终构建 CN+EN 双语内容。功能正确（Render 按语言选取），但有不必要的字符串构建开销。

**修复**: 根据 language 只构建对应语言内容，或移除 language 参数。

---

### T-04: Identity Section 产品名与 Code Prompt 产品名不一致

**Go文件**: `sections/identity.go:42,90` (UapClaw) vs `code_prompt_builder.go:72,285` (JiuwenSwarm)
**Python参考**: Python 统一使用 "JiuwenSwarm"

**问题描述**: Identity section 使用 "UapClaw"/`.uapclaw`，但 Code prompt 8个 section 仍使用 "JiuwenSwarm"。如是有意品牌重命名则需同步更新 code prompt；如非有意则 identity section 应改回 "JiuwenSwarm"。

**修复**: 确认品牌命名策略后统一。

---

### T-05: StructuredAskUserTool schema 来源不同

**Go文件**: `internal/swarm/server/rails/structured_ask_user_tool.go:32-44`
**Python参考**: `jiuwenswarm/agents/harness/common/rails/ask_user_rail.py:165-183`

**问题描述**: Go 使用 `BuildToolCard` 从注册表（AskUserMetadataProvider）获取 schema，Python 硬编码 `EXTENDED_INPUT_PARAMS`。需确认注册表的 questions 参数定义与 Python `_QUESTIONS_ITEM_SCHEMA` 完全一致。

---

### T-06: StructuredAskUserRail 构造函数缺少 tool_names 参数

**Go文件**: `internal/swarm/server/rails/structured_ask_user_rail.go`
**Python参考**: `ask_user_rail.py:209-213`

**问题描述**: Python 允许自定义拦截的工具名列表，Go 硬编码 `"ask_user"`。当前 CodeAdapter 未传 tool_names 所以无影响，但缺少扩展能力。

---

### T-07: StructuredAskUserRail 缺少 AskUserPayload.answer 兼容分支

**Go文件**: `internal/swarm/server/rails/structured_ask_user_rail.go:280-284`
**Python参考**: `ask_user_rail.py:282-291`

**问题描述**: Python 对旧版 `answer` (str) 字段做 `__free_text__` 转换，Go 直接跳过。当前 Go 的 AskUserPayload 确实只有 Answers 字段，不会出问题，但缺少向前兼容。

---

### T-08: MemUpdateChecker 是纯 stub — 已正确标注 ⤵️

**Go文件**: `internal/swarm/agents/harness/common/memory/manage/mem_update_checker.go`
**标注**: `⤵️ 回填: 7.8`

**问题描述**: 当前 stub 实现直接返回所有新记忆为 ADD，对齐 Python `base_chat_model=None` 行为。7.8 回填时需实现完整的 LLM 驱动冲突检查（含 `_format_input`, `PromptApplier`, LLM 调用, 重试, 结果映射, `_process_conflict_info`）。

---

### T-09: FragmentMemoryManager.AddMemories 缺少 llm 参数 — 为 7.8 回填预留

**Go文件**: `internal/swarm/agents/harness/common/memory/manage/fragment_manager.go:67`
**Python参考**: `add_memories(llm: Model | None = None)`

**问题描述**: Python 的 `add_memories` 接受 `llm` 参数传递给 `MemUpdateChecker.check(base_chat_model=llm)`。Go 当前 stub 不需要（MemUpdateChecker 不调用 LLM），但 7.8 回填时需要扩展签名。**不是当前 bug，是预留点。**

---

### T-10: CodingMemoryRail autoRecall 字节/字符截断不一致

**Go文件**: `internal/swarm/agents/harness/common/rails/coding_memory_rail.go:575,584`

**问题描述**: 剩余预算用字节（`len(body)`）计算，但截断用 rune 数（`utf8.RuneCountInString`）。多字节字符可能导致实际截断后超出字节预算。

**修复**: 截断时使用字节偏移而非 rune 数，或按 rune 截断后重新计算字节长度。

---

### T-11: CodingMemoryRail.ListFiles 缺少 recursive=False 参数

**Go文件**: `internal/swarm/agents/harness/common/rails/coding_memory_rail.go:702`
**Python参考**: `coding_memory_rail.py:512-513`

**问题描述**: Python 的 `list_files(memory_dir, recursive=False)` 只列顶层文件，Go 的 `ListFiles` 可能递归列出子目录文件，导致记忆文件计数不同。

---

### T-12: recover 日志缺少结构化错误字段

**Go文件**: `internal/swarm/server/rails/structured_ask_user_rail.go:216-218`

**问题描述**: Go 使用 `Msgf("解析结构化输入异常...: %v", rec)` 记录 panic，应改用 `.Any("panic", rec)` 结构化字段，对齐项目日志规范。

**修复**:
```go
logger.Warn(structuredAskUserRailLogComponent).
    Str("event_type", "structured_ask_user_rail_resolve").
    Any("panic", rec).
    Msg("解析结构化输入异常，回退到 interrupt")
```

---

## 六、⤵️ 标记验证

以下 ⤵️ 标记经确认仍为未实现占位：

| 位置 | 标记 | 状态 |
|------|------|------|
| `mem_update_checker.go` | ⤵️ 回填: 7.8 | ✅ 正确标注，stub 实现对齐 Python base_chat_model=None |
| `code_adapter.go:543` | ⤵️ 7.8: CodingMemoryRail 条件注入 | ✅ 正确标注，但应为 **严重问题**（S-05） |
| `deep_adapter.go:114-162` | ⤵️ 10.6.3-10: 多个 Rail 字段 | ✅ 正确标注，多个 Rail（SkillUseRail/MemoryRail/AvatarRail 等）尚未实现 |
| `fragment_manager.go` | AddMemories 缺 llm 参数 | 见 T-09，7.8 回填时扩展 |

---

## 七、修复优先级建议

### 第一优先级（严重，影响核心功能）
1. **S-01**: resolveEmbeddingConfig 从 configBase 读取 embed → 向量搜索恢复
2. **S-02**: CancelMember 语义修正 → 不修改成员状态，重置任务+发消息
3. **S-03**: ShutdownMember 幂等+消息发送 → 成员正确接收关闭通知
4. **S-05**: code_agent 子 Agent 注入 CodingMemoryRail → 子 Agent 编程记忆可用
5. **S-04**: nil card panic 修复 → 防止运行时崩溃
6. **S-06**: summarizeSearchPayload 回退链 → ClawHub/TeamSkillsHub 搜索日志可读

### 第二优先级（一般，影响体验/完整性）
7. **M-01~M-04**: TeamBackend 消息通知补齐
8. **M-06**: 空字符串 Reject 对齐 Python
9. **M-07**: 嵌入配置不完整警告日志
10. **M-10**: CodingMemoryWriteTool 允许空内容

### 第三优先级（提示，优化/一致性）
11. **T-01~T-04**: Prompt Builder 换行符/产品名一致性
12. **T-08~T-09**: 7.8 回填预留点（按计划实施）
