# 48h 逻辑审查报告 — 2026-09-20

> 审查范围：48 小时内提交记录（dbde38a2 → 81a4233b）
> 涉及章节：9.24 P5 跨用户共享差距修复（G1-G8）、2026-09-09 审查修复 12 项、AgentServer.Stop 竞态修复
> 参考项目：`openjiuwen/`（agent-core）+ `jiuwenswarm-develop/`
> 审查方法：逐方法对比 Python 参考实现与 Go 移植代码，检查签名一致性、步骤完整性、占位代码真实性

---

## 摘要

| 分类 | 数量 |
|------|------|
| 严重 (P0) | 8 |
| 一般 (P1) | 12 |
| 提示 (P2) | 7 |
| **合计** | **27** |

---

## 严重问题 (P0)

### S-01 `SQLTaskDao.UpdateTaskStatus` 的 `refreshedIDs` 永远为空

**Python 参考**（`openjiuwen/agent_teams/tools/database/task_dao.py`）：
`update_task_status` 在终态传播后返回 `unblocked_task_ids`——被解除阻塞的下游任务 ID 列表，供调用方决定是否触发下游任务重新分配。

```python
async def update_task_status(self, task_id: str, status: str) -> list[str]:
    # ...事务内: 更新状态 → 标记依赖 resolved → 刷新下游 blocked→pending
    refreshed_ids = await self._refresh_status_in_tx(tx, team_name, ...)
    return refreshed_ids  # 返回被解除阻塞的任务ID列表
```

**Go 问题**（`internal/agent_teams/tools/database/sql_task_dao.go:195-230`）：
```go
func (d *SQLTaskDao) UpdateTaskStatus(ctx context.Context, taskID, newStatus string) ([]string, error) {
    var refreshedIDs []string  // 声明但从未赋值
    err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // ...更新状态 + 标记依赖 resolved
        // ❌ 未调用 refreshStatusInTx，也未收集 unblocked IDs
        return nil
    })
    return refreshedIDs, err  // 永远返回空切片
}
```

**影响**：任务完成后下游阻塞任务不会被自动刷新（blocked→pending），调度器无法感知到新可执行的任务，导致依赖图上的任务链断裂。

**修复方案**：
1. 在事务内终态分支中调用 `refreshStatusInTx` 或等价的下游状态刷新逻辑
2. 将刷新得到的 unblocked task IDs 收集到 `refreshedIDs` 中返回

```go
// 终态分支中添加:
if newStatus == fsm.TaskStatusCompleted {
    // 标记依赖 resolved（已有）
    // 刷新下游状态:
    refreshed, _ := d.refreshStatusInTx(tx, depTable, taskID)
    refreshedIDs = refreshed
}
```

---

### S-02 `InMemoryTeamDatabase.AddTaskWithBidirectionalDependencies` 丢弃调用方 ctx

**Python 参考**（`openjiuwen/agent_teams/tools/database/task_dao.py`）：
Python 的 `add_task_with_bidirectional_dependencies` 不存在显式 ctx（Python 无 context 概念），但所有数据库操作在同一事务内完成。

**Go 问题**（`internal/agent_teams/tools/database/memory_impl.go:784`）：
```go
func (db *InMemoryTeamDatabase) AddTaskWithBidirectionalDependencies(ctx context.Context, ...) (bool, error) {
    // ❌ 传入 context.Background() 而非 ctx
    db.MutateDependencyGraph(context.Background(), teamName, ...)
}
```

**影响**：如果调用方 ctx 带有超时或取消信号（如 HTTP 请求超时），操作会无视这些信号继续执行，可能导致长时间阻塞。

**修复方案**：
```go
db.MutateDependencyGraph(ctx, teamName, ...)  // 传播 ctx
```

---

### S-03 `DeepAdapter.buildMemoryRail` / `buildExternalMemoryRail` 返回 nil，记忆系统不可用

**Python 参考**（`jiuwenswarm/server/runtime/agent_adapter/interface_deep.py:2044-2072`）：
```python
def _build_memory_rail(self, mode: str) -> MemoryRail | None:
    config = get_config()
    embed_config = config.get("embed") if isinstance(config, dict) else None
    # ... 解析 embed_api_key/embed_base_url/embed_model
    memory_rail = MemoryRail(
        embedding_config=EmbeddingConfig(
            model_name=embed_config.get("embed_model"),
            base_url=embed_config.get("embed_base_url"),
            api_key=embed_config.get("embed_api_key"),
        ),
        is_proactive=self._is_proactive_memory,
    )
    return memory_rail  # 正常返回有效实例
```

**Go 问题**（`internal/swarm/server/adapter/deep_adapter_rails.go:450-461`）：
```go
func (d *DeepAdapter) buildMemoryRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 MemoryRail
    return nil  // ❌ 始终返回 nil
}

func (d *DeepAdapter) buildExternalMemoryRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 ExternalMemoryRail
    return nil  // ❌ 始终返回 nil
}
```

**影响**：`write_memory`、`read_memory`、`memory_search`、`memory_get` 等记忆工具无法被注册和拦截，Agent 的记忆读写功能完全不可用。Python 中这些 Rail 会在 `buildAgentRails` 中被创建并注册到 Agent 实例。

**修复方案**：实现 MemoryRail 和 ExternalMemoryRail，对齐 Python 中的 EmbeddingConfig 解析和 proactive memory 判断逻辑。这属于 10.6.3-10 章节回填，但当前标记为 ⤵️ 意味着**已经确认未实现**，严重性在于功能完全缺失而非逻辑错误。

---

### S-04 模式切换时记忆 Rail 处理未实现

**Python 参考**（`jiuwenswarm/server/runtime/agent_adapter/interface_deep.py`）：
Python 的 `_update_plan_mode_rails` 和 `_update_agent_mode_rails` 中都有明确的记忆 Rail 处理：
```python
# plan 模式:
await self._handle_memory_rail_by_config("slow")
await self._handle_external_memory_rail_by_config()

# agent 模式:
await self._handle_memory_rail_by_config("fast")
await self._handle_external_memory_rail_by_config()
```

**Go 问题**（`internal/swarm/server/adapter/deep_adapter_rails.go:773-779`）：
```go
// updateAgentModeRails 中:
// 2. 记忆 rail 处理
// Python: await self._handle_memory_rail_by_config("fast")
// ⤵️ 待回填: handleMemoryRailByConfig

// 3. 外接记忆 rail
// Python: await self._handle_external_memory_rail_by_config()
// ⤵️ 待回填: handleExternalMemoryRailByConfig
```

**影响**：即使 MemoryRail 后来被实现，模式切换时也不会动态注册/注销记忆 Rail，导致 plan/code 模式切换后记忆工具的可用性不会更新。

**修复方案**：实现 `handleMemoryRailByConfig` 和 `handleExternalMemoryRailByConfig`，对齐 Python 的配置驱动注册/注销逻辑。

---

### S-05 `SkillEvolutionRail.RunEvolution` 同步路径缺少 `consumeEvalState`

**Python 参考**（`openjiuwen/harness/rails/evolution/skill_evolution_rail.py:427-431`）：
```python
async def run_evolution(self, trajectory, ctx=None, *, snapshot=None):
    # 同步路径:
    messages = collect_messages_from_trajectory(trajectory)
    presented_entries = self._experience_tracker.consume_eval_state(session)  # ← 关键步骤
```

**Go 问题**（`internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`）：
Go 的 `RunEvolution` 在同步路径（`snapshot == nil`）中只调用了 `collectMessagesFromTrajectory(traj)`，但**未调用** `experienceTracker.ConsumeEvalState(sessionID)` 获取 `presentedEntries`。`presentedEntries` 始终为零值。

**影响**：同步模式下 `EvaluatePresented` 收到空切片，无法评估已展示给用户但尚未评分的经验记录。经验评分机制在同步模式下完全失效。

**修复方案**：
```go
// RunEvolution 同步路径中补充:
if sessionID != "" {
    presentedEntries = r.experienceTracker.ConsumeEvalState(sessionID)
}
```

---

### S-06 `CodingMemoryToolOps` 降级写入路径绕过 LLM 冲突检测

**Python 参考**（`openjiuwen/core/memory/lite/coding_memory_tool_ops.py:462-494`）：
Python 的降级路径也绕过了 LLM 冲突检测，但 Python 的降级只在**超过最大重试次数**时触发，且最后一次成功检测的结果 `result` 仍然被保留到返回值中。Go 的行为在这一点上一致。

**Go 问题**（`internal/agentcore/memory/lite/coding_memory_tool_ops.go:321-361`）：
虽然 Go 与 Python 的降级行为一致，但**降级路径中 `appendToExistingFile` 的 WriteFile 错误被忽略**：
```go
func appendToExistingFile(ctx context.Context, toolCtx *CodingMemoryToolContext, resolved, body string, fm map[string]string) {
    // ... 读取文件 → 更新 frontmatter → 写回
    _, _ = sysOp.Fs().WriteFile(ctx, resolved, updatedContent, ...)  // ❌ 错误被忽略
}
```
如果文件写入失败，frontmatter 已被修改但内容未持久化，导致索引与文件不一致。Python 中 `write_file` 抛出异常会被上层 `try/except` 捕获。

**影响**：降级路径下文件写入失败时返回 `success: true`，用户认为写入成功但实际未写入，且 MEMORY.md 索引可能已被更新，导致索引与实际内容不一致。

**修复方案**：
```go
if _, err := sysOp.Fs().WriteFile(ctx, resolved, updatedContent, ...); err != nil {
    logger.Error(logComponent).Err(err).Str("path", resolved).Msg("降级写入失败")
    return map[string]any{"success": false, "path": path, "mode": "", "error": err.Error()}
}
```

---

### S-07 `HookExecutor.runPromptHook` LLM goroutine 泄漏

**Python 参考**（`jiuwenswarm/server/hooks/executor.py`）：
```python
async def _run_prompt_hook(self, config, hook_input):
    timeout = config.get("timeout", 15)
    try:
        response_text = await asyncio.wait_for(self._query_llm(prompt, model), timeout=timeout)
        # asyncio.wait_for 超时后会 cancel task，LLM 调用被正确取消
    except asyncio.TimeoutError:
        return HookResult(outcome=NON_BLOCKING_ERROR, error=f"prompt hook timeout after {timeout}s")
```

**Go 问题**（`internal/swarm/server/hooks/executor.go:410-425`）：
```go
resultCh := make(chan llmResult, 1)
go func() {
    text, err := e.queryLLM(ctx, finalPrompt, modelName)
    resultCh <- llmResult{text, err}  // ❌ 超时后无人消费，永久阻塞
}()

select {
case <-time.After(time.Duration(timeout) * time.Second):
    return HookResult{...}  // 超时返回，但 goroutine 永久阻塞在 resultCh
case result = <-resultCh:
    ...
}
```

**影响**：每次 prompt hook 超时都会泄漏一个 goroutine（LLM 调用完成后尝试向无人消费的 channel 发送数据，由于 channel 缓冲为 1 且无人接收，goroutine 永久阻塞）。高频超时场景下会导致 goroutine 数量无限增长，最终 OOM。

**修复方案**：使用 `context.WithCancel` 取消 LLM 调用：
```go
llmCtx, llmCancel := context.WithCancel(ctx)
defer llmCancel()

resultCh := make(chan llmResult, 1)
go func() {
    text, err := e.queryLLM(llmCtx, finalPrompt, modelName)
    resultCh <- llmResult{text, err}
}()

select {
case <-time.After(time.Duration(timeout) * time.Second):
    llmCancel()  // 取消 LLM 调用
    // 等待 goroutine 退出（或用带缓冲的 channel 保证不阻塞）
    go func() { <-resultCh }()  // 消费结果释放 goroutine
    return HookResult{Outcome: HookOutcomeNonBlockingError, ...}
case result = <-resultCh:
    ...
}
```

---

### S-08 `AgentServer.Stop()` 在 `Start()` 未调用时永久阻塞

**Python 参考**：Python 中 `AgentServer.stop()` 有 `_running` 状态检查，未启动时不会阻塞。

**Go 问题**（`internal/swarm/server/agent_server.go:145-150`）：
```go
func (s *AgentServer) Stop() {
    if s.cancel != nil {
        s.cancel()
    }
    <-s.stopCh  // ❌ 如果 Start() 从未调用，stopCh 未被 close，永久阻塞
}
```

**影响**：如果调用方在 `Start()` 之前调用 `Stop()`（如初始化失败后的清理路径），程序会永久挂起。

**修复方案**：添加 running 状态检查：
```go
func (s *AgentServer) Stop() {
    s.mu.Lock()
    if !s.running {
        s.mu.Unlock()
        return
    }
    if s.cancel != nil {
        s.cancel()
    }
    s.mu.Unlock()
    <-s.stopCh
}
```

---

## 一般问题 (P1)

### M-01 `InMemoryTeamDatabase.Close()` 将 map 置 nil 后访问会 panic

**Python 参考**：Python 的 `close()` 通常不删除字典，或访问已关闭对象返回空结果。

**Go 问题**（`internal/agent_teams/tools/database/memory_impl.go:159-170`）：
```go
func (db *InMemoryTeamDatabase) Close() error {
    db.teams = nil
    db.members = nil
    // ... 所有 map 置 nil
}
```
`Close()` 后如果任何方法被调用（如 `GetTeam`），`db.teams[teamName]` 在 nil map 上会 panic。

**修复方案**：用 `make()` 重建空 map 代替置 nil，或在每个方法入口检查 `db.initialized`。

---

### M-02 `SQLMessageDao.HasUnreadMessages` GORM 错误被静默吞掉

**Python 参考**：Python 的 `has_unread_messages` 在数据库异常时抛出异常给调用方处理。

**Go 问题**（`internal/agent_teams/tools/database/sql_message_dao.go:167-200`）：
```go
d.db.WithContext(ctx).Table(msgTable).Where(...).Count(&count)
// ❌ error 返回值被忽略
```
数据库连接失败时 count 为 0，返回 `false`（无未读），掩盖了数据库故障。

**修复方案**：检查 error 并返回。

---

### M-03 `SQLMessageDao.MarkMessageRead` 更新错误被忽略

**Go 问题**（`internal/agent_teams/tools/database/sql_message_dao.go:247`）：
```go
d.db.WithContext(ctx).Table(msgTable).Where("message_id = ?", messageID).Update("is_read", 1)
// ❌ error 被忽略，更新失败仍返回 true
```

**修复方案**：检查 error 返回值，失败时返回 false。

---

### M-04 `SQLTaskDao.UpdateTask` GORM Updates 错误被忽略

**Go 问题**（`internal/agent_teams/tools/database/sql_task_dao.go:257`）：
```go
tx.Table(table).Where("task_id = ?", taskID).Updates(updates)
// ❌ error 未检查，更新失败仍返回 ok=true
```

**修复方案**：检查 `result.Error` 并在失败时返回 `(false, err)`。

---

### M-05 `SQLTaskDao.CreateTask` IntegrityError 与其他 error 混淆

**Go 问题**（`internal/agent_teams/tools/database/sql_task_dao.go:42-52`）：
```go
if result.Error != nil {
    return false, nil  // ❌ 无法区分主键冲突和数据库连接失败
}
```
调用方无法知道是"重复"还是"数据库挂了"。

**修复方案**：区分 `IntegrityError`（返回 false, nil）和其他错误（返回 false, err）。

---

### M-06 `CodingMemoryToolOps.searchSimilar` 使用 `context.Background()`

**Go 问题**（`internal/agentcore/memory/lite/coding_memory_tool_ops.go:456`）：
```go
toolCtx.Manager.Search(context.Background(), body, ...)
// ❌ 丢弃调用方 ctx，搜索操作无法被取消
```

**修复方案**：传播调用方的 ctx。

---

### M-07 `CodingMemoryToolOps.upsertMemoryIndex` WriteFile 错误被忽略

**Go 问题**（`internal/agentcore/memory/lite/coding_memory_tool_ops.go:682`）：
```go
_, _ = sysOp.Fs().WriteFile(ctx, indexPath, newContent, ...)
// ❌ 索引写入失败，MEMORY.md 不更新，但文件内容已写入成功
```
造成索引与文件不一致。

**修复方案**：检查 error 并在失败时回滚或记录警告。

---

### M-08 `AgentServer.cancelAllInflightWork` 使用 `context.Background()`

**Go 问题**（`internal/swarm/server/agent_server.go:426`）：
```go
s.agentManager.CancelAllInflightWork(context.Background(), ...)
// ❌ 传入 Background context，无法被超时/取消控制
```

**修复方案**：使用带超时的 context（如 `context.WithTimeout(ctx, 10*time.Second)`）。

---

### M-09 `WorktreeBackend.Remove` 分支删除失败被忽略

**Go 问题**（`internal/agentcore/harness/tools/worktree/backend.go:178-180`）：
```go
_ = BranchDelete(ctx, branch, repoRoot)
// ❌ 错误被忽略，worktree 已移除但分支残留
```
后续同名 worktree 创建可能冲突。

**修复方案**：检查 error 并记录 Warn 日志；如果删除失败，返回 error 或记录残留分支信息。

---

### M-10 `AvatarPromptRail.BeforeModelCall` 竞态条件

**Python 参考**：Python 的异步单线程模型避免了真正的并发问题。

**Go 问题**（`internal/swarm/agents/harness/common/rails/avatar_rail.go:92-96`）：
```go
func (r *AvatarPromptRail) BeforeModelCall(...) error {
    for name := range r.injectedSections {
        builder.RemoveSection(name)
    }
    r.injectedSections = make(map[string]bool)  // ❌ 并发请求共享同一实例时竞态
```
如果 `AvatarPromptRail` 是单例，并发请求的 `BeforeModelCall` 可能互相干扰。

**修复方案**：在 `BeforeModelCall` 中使用 mutex 保护 `injectedSections` 的读写，或改为每次请求独立的 sections 实例。

---

### M-11 `SkillEvolutionRail.RunEvolution` 缺少整体异常捕获

**Python 参考**（`openjiuwen/harness/rails/evolution/skill_evolution_rail.py:410-421`）：
```python
async def run_evolution(self, trajectory, ctx=None, *, snapshot=None):
    try:
        # ... 完整演化逻辑
    except Exception:
        logger.warning("[SkillEvolutionRail] run_evolution failed", exc_info=True)
```

**Go 问题**：Go 的 `RunEvolution` 直接返回 error，没有 `recover` 包裹。如果内部 goroutine panic 或 error 未正确传播，整个演进流程会中断且无日志。

**修复方案**：在 `RunEvolution` 入口处添加 `defer recover` 捕获异常并记录日志。

---

### M-12 `DeepAdapter.updateAgentModeRails` 在 agent 模式下注销 SkillEvolutionRail

**Python 参考**（`jiuwenswarm/server/runtime/agent_adapter/interface_deep.py:2854-2895`）：
Python 的 `_update_agent_mode_rails` 中 SkillEvolutionRail 的处理取决于配置和模式，并非始终注销。

**Go 问题**（`internal/swarm/server/adapter/deep_adapter_rails.go:748-755`）：
```go
if d.skillEvolutionRail != nil && d.instance != nil {
    d.instance.UnregisterRail(ctx, d.skillEvolutionRail)  // ❌ 始终注销
    d.skillEvolutionRail = nil
}
```

**影响**：Go 在 agent 模式下无条件注销 SkillEvolutionRail，可能导致 agent 模式下技能自动演进不可用。

**修复方案**：对齐 Python 的条件判断逻辑，根据配置决定是否保留 SkillEvolutionRail。

---

## 提示问题 (P2)

### T-01 `InMemoryTeamDatabase.terminateTaskInSession` 冗余赋值

**Go 问题**（`internal/agent_teams/tools/database/memory_impl.go:1197`）：
```go
dep.Resolved = true
db.deps[key] = dep  // ❌ dep 是 *TeamTaskDependencyBase 指针，修改已直接影响 map 中的值
```

**修复方案**：移除冗余赋值 `db.deps[key] = dep`。

---

### T-02 `SQLTaskDao.CancelAllTasks` 循环变量取地址

**Go 问题**（`internal/agent_teams/tools/database/sql_task_dao.go:511`）：
```go
for _, task := range tasks {
    result.Cancelled = append(result.Cancelled, &task)  // ⚠️ Go < 1.22 时所有指针指向同一地址
}
```

**修复方案**：确认 Go 版本 >= 1.22，或在循环内创建局部变量：
```go
task := task
result.Cancelled = append(result.Cancelled, &task)
```

---

### T-03 `CodingMemoryToolOps.CodingMemoryReadWithContext` offset/limit 重复处理

**Go 问题**（`internal/agentcore/memory/lite/coding_memory_tool_ops.go:99-129`）：
`WithFsLineRange` 和后续的手动切片 `rows[fromIdx:toIdx]` 可能导致二次截断。

**修复方案**：确认 sysop 的 `ReadFile` 是否已按 lineRange 截断，如果是则移除手动切片。

---

### T-04 `GitBackend.runGit` 硬编码 30s 超时

**Go 问题**（`internal/agentcore/harness/tools/worktree/git.go:324`）：
```go
execCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)  // 30s 硬编码
```
对于大型仓库的 `git fetch` 可能不够。

**修复方案**：将超时提取为可配置参数，或根据命令类型设置不同超时。

---

### T-05 `GitBackend.ReadWorktreeHeadSHA` worktree 本地 refs 优先级可能不正确

**Go 问题**（`internal/agentcore/harness/tools/worktree/git.go:286-288`）：
先读 worktree 本地 refs 再读 commondir refs。git worktree 的 refs 通常在 commondir 中共享，本地优先可能导致读取过期 SHA。

**修复方案**：确认 Python 的优先级逻辑是否一致，如不一致则调整。

---

### T-06 `getMemoryForbiddenConfigSafe` 每次调用都重新加载配置

**Go 问题**（`internal/swarm/agents/harness/common/memory/forbidden.go:76`）：
```go
cfg, _ := config.New("")
cfg.Load()  // ❌ BeforeModelCall 高频路径中不必要的 I/O
```

**修复方案**：添加 `sync.Once` 缓存或热重载机制。

---

### T-07 `HookExecutor.RunAll` 的 `sessionID` 参数未使用

**Go 问题**（`internal/swarm/server/hooks/executor.go:86`）：
```go
func (e *HookExecutor) RunAll(ctx context.Context, hookConfigs []HookConfig, hookInput map[string]any, sessionID string) []HookResult {
    // sessionID 未被使用
```
虽然 Python 也未使用此参数，但 Go 的未使用参数可能导致 lint 警告。

**修复方案**：添加 `_ = sessionID` 或在方法签名中移除。

---

## 占位代码真实性确认

| 占位标记 | 位置 | 状态 | 说明 |
|----------|------|------|------|
| `⤵️ 10.6.3-10` MemoryRail | `deep_adapter_rails.go:450-452` | **✅ 确认未实现** | 返回 nil，记忆系统完全不可用 |
| `⤵️ 10.6.3-10` ExternalMemoryRail | `deep_adapter_rails.go:458-460` | **✅ 确认未实现** | 返回 nil |
| `⤵️ 10.6.3-10` SkillCreateRail | `deep_adapter_rails.go` | **✅ 确认未实现** | 返回 nil |
| `⤵️ 10.6.3-10` ResponsePromptRail | `deep_adapter_rails.go:494-496` | **✅ 确认未实现** | 返回 nil |
| `⤵️ 10.6.3-10` StreamEventRail | `deep_adapter_rails.go` | **✅ 确认未实现** | 返回 nil |
| `⤵️ 待回填` handleMemoryRailByConfig | `deep_adapter_rails.go:775` | **✅ 确认未实现** | 模式切换时记忆Rail不更新 |
| `⤵️ 待回填` handleExternalMemoryRailByConfig | `deep_adapter_rails.go:779` | **✅ 确认未实现** | 模式切换时外接记忆Rail不更新 |
| `⤵️ AutoHarness` resetHarnessPackagesState | `agent_server.go` | **✅ 确认未实现** | |
| `⤵️ JiuwenBox` bootstrapInternalJiuwenbox | `agent_server.go` | **✅ 确认未实现** | |
| `⤵️ Team` startTeammateBootstrapDaemon | `agent_server.go` | **✅ 确认未实现** | |
| `⤵️ Team` cancelAllTeamStreamTasks | `agent_server.go` | **✅ 确认未实现** | |
| `⤵️ Scheduler` stopScheduler | `agent_server.go` | **✅ 确认未实现** | |

---

## G1-G8 修复验证

| 编号 | 修复内容 | Python 对照 | Go 对齐状态 |
|------|---------|-------------|------------|
| G1 | `resolveDownloadTopK` + `WithSharingConfigMap` | `_resolve_download_top_k(sharing_config)` | ✅ 对齐 |
| G2 | `isErrorNonePattern` → `FindAllStringIndex`+后置检查 | `_FAILURE_KEYWORDS` 负向前瞻正则 | ✅ 功能等价 |
| G3 | `buildExperienceSharer` backend 名称校验 | `_build_experience_sharer` backend 校验 | ✅ 对齐 |
| G4 | `uploadApprovedRecordsForSharing` 独立方法 | `_upload_approved_records_for_sharing` | ✅ 对齐（合并stage+flush） |
| G5 | `getExcerptOffsetByKey`/`setExcerptOffsetByKey` | `_get_excerpt_offset_by_key`/`_set_excerpt_offset_by_key` | ✅ 对齐 |
| G6 | `sharedRecordContextMarker` 常量 | `_SHARED_RECORD_CONTEXT_MARKER` | ✅ 对齐 |
| G7 | 正则提升为包级预编译变量 | 模块级 `_FAILURE_KEYWORDS` | ✅ 对齐 |
| G8 | `ExtractQueryKeywords` panic recovery | `try/except` 异常捕获 | ✅ 对齐 |

---

## TeamSkillEvolutionRail 对齐验证

| 方面 | Python | Go | 对齐状态 |
|------|--------|-----|---------|
| 继承关系 | `EvolutionRail`（无 SharingMixin） | `*EvolutionRail` 嵌入（无 Sharing 字段） | ✅ 一致 |
| approve_record 无共享上传 | 无 `_upload_approved_records_for_sharing` | 无 `uploadApprovedRecordsForSharing` | ✅ 一致 |
| handleEvolutionFromSignals 无共享 | `_on_auto_approved` 无 sharing 调用 | `onAutoApproved` 无 sharing 调用 | ✅ 一致 |
| NotifyTeamCompleted | 返回 bool | 返回 (bool, error) | ⚠️ 签名差异（Go 加 error） |
| OnAfterToolCall 拦截 view_task | 8 步 | 8 步 | ✅ 一致 |
| run_evolution 信号检测 | issue_count 精确计算 | 只统计信号数量 | ⚠️ 微小差异 |

---

## 修复优先级建议

| 优先级 | 问题编号 | 建议 |
|--------|---------|------|
| 🔴 立即修复 | S-01, S-05, S-07 | 功能缺失/goroutine 泄漏，影响核心流程 |
| 🔴 本迭代修复 | S-02, S-06, S-08 | 上下文传播/数据一致性/死锁风险 |
| 🟡 计划修复 | S-03, S-04, M-01~M-12 | 记忆系统占位/错误处理/竞态 |
| 🟢 低优先级 | T-01~T-07 | 代码质量/性能优化 |
