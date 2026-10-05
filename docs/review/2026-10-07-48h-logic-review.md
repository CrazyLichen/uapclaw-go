# 48h 逻辑审查报告 — 2026-10-07

> 审查范围：48 小时内提交记录覆盖的章节，对照 Python 参考项目逐方法比对

## 审查章节与对应 Python 参考

| 章节 | Go 包路径 | Python 参考路径 | 审查状态 |
|------|-----------|-----------------|----------|
| 10.3.8 TeamHelpers | `internal/swarm/server/adapter/team_helpers.go` | `jiuwenswarm/server/runtime/agent_adapter/team_helpers.py` | ✅ |
| 10.6.12 SessionOps | `internal/swarm/agents/harness/common/sessionops/` | `jiuwenswarm/agents/harness/common/session_ops_service.py` | ✅ |
| 7.20 Dreaming + Sweeper | `internal/agentcore/memory/dreaming/` + `internal/swarm/agents/harness/common/memory/dreaming/` | `openjiuwen/core/memory/dreaming/` + `jiuwenswarm/agents/harness/common/memory/dreaming/` | ✅ |
| Monitor 模块 | `internal/agent_teams/monitor/` + `internal/swarm/agents/harness/team/` | `openjiuwen/agent_teams/monitor/` + `jiuwenswarm/agents/harness/team/` | ✅ |
| 7.16 OpenJiuwenProvider + 7.17 AgentArtsProvider | `internal/agentcore/memory/external/` | `openjiuwen/core/memory/external/` | ✅ |
| 13.1 Indexer + 13.4 ComputeChunkEmbeddings | `internal/agentcore/retrieval/` | `openjiuwen/core/retrieval/` | ✅ |
| Evolution 重构 | `internal/swarm/server/adapter/evolution/` | `jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py` | ✅ |
| Team 类型重构 | `internal/swarm/agents/harness/team/` + `internal/agent_teams/` | `jiuwenswarm/agents/harness/team/` + `openjiuwen/agent_teams/` | ✅ |

---

## 问题统计

| 章节 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| 10.3.8 TeamHelpers | 7 | 5 | 4 | 16 |
| 10.6.12 SessionOps | 4 | 5 | 4 | 13 |
| 7.20 Dreaming+Sweeper | 5 | 5 | 2 | 12 |
| Monitor 模块 | 6 | 7 | 6 | 19 |
| 7.16+7.17 Memory Provider | 4 | 7 | 5 | 16 |
| 13.1+13.4 Indexer/Retrieval | 4 | 3 | 3 | 10 |
| Evolution 重构 | 4 | 6 | 3 | 13 |
| Team 类型重构 | 7 | 5 | 3 | 15 |
| **合计** | **41** | **43** | **30** | **114** |

---

## 一、10.3.8 TeamHelpers — 16 项

### S-01 processTeamMessageStream 首次请求核心流程完全缺失（⤵️ 占位）

**Python 参考**（`team_helpers.py` L699-823）：首次请求完整流程包含 9 个步骤：
1. `deep_agent` 校验
2. `get_enriched_team_spec()` 构建 TeamSpec
3. `resolve_team_rebuild_followup()` 解析 /evolve_rebuild
4. `ensure_team_shared_skills_initialized()` 共享技能初始化
5. `prepare_runtime_activation()` 准备运行时
6. 创建 `asyncio.Queue` + 注册 waiter
7. 设置 `stream_envs` (hide_dm / stream_trace)
8. `increment_session_round_count()` 递增轮次
9. `_consume_stream_with_query()` 启动后台流任务 + 事件循环 + waiter 清理

**Go 问题**（`deep_adapter_team.go` L272-285）：
```go
if isFirstRequest {
    // ⤵️(#9.85): TeamRunner 完整创建 TeamAgent + streaming 流程
    return
}
```
整个首次请求流程体为空，team 模式首次请求永远拿不到任何响应。

**修复建议**：实现 `⤵️(#9.85)` 标记的完整首次请求流程。

**流程示例**（Python 完整流程）：
```
processTeamMessageStream(is_first_request=True):
  ├─ deep_agent 校验 → raise RuntimeError if None
  ├─ get_enriched_team_spec(session_id, team_name)
  ├─ resolve_team_rebuild_followup(inputs) → followup_prompt, rebuild_error
  │   └─ if rebuild_error → yield chat.error chunk → return
  ├─ ensure_team_shared_skills_initialized(team_name)
  ├─ prepare_runtime_activation(team_name, session_id)
  ├─ queue = asyncio.Queue(); team_manager.register_team_message_waiter(session_id, queue)
  ├─ stream_envs = {"hide_dm": True, _STREAM_TRACE_ENV_KEY: "1"} (conditional)
  ├─ increment_session_round_count(session_id)
  ├─ stream_task = asyncio.create_task(_consume_stream_with_query(...))
  ├─ team_manager.register_team_stream_task(session_id, stream_task)
  ├─ while True:
  │   ├─ event = await queue.get()
  │   ├─ if is_chat_error(event) → yield error chunk → break
  │   ├─ yield AgentResponseChunk(event)
  │   └─ if is_chat_done(event) → break
  └─ finally: team_manager.unregister_team_message_waiter(session_id)
```

---

### S-02 processTeamMessageStream 缺失 deep_agent 校验 + TeamSpec 构建 + rebuild followup 处理

**Python 参考**（L699-750）：
```python
if deep_agent is None:
    raise RuntimeError("DeepAgent not initialized")
team_spec = await team_manager.get_enriched_team_spec(...)
followup_prompt, rebuild_error = await _resolve_team_rebuild_followup(...)
if rebuild_error is not None:
    yield AgentResponseChunk(payload={"event_type": "chat.error", ...})
    return
```

**Go 问题**：这些步骤应在 `isFirstRequest` 分支之前执行（Python 代码在 L699-750，分支判断在 L756），但 Go 代码没有在分支前做这些检查。

**修复建议**：将 deep_agent 校验、TeamSpec 构建和 rebuild followup 解析放在 `isFirstRequest` 分支之前，对齐 Python 的控制流顺序。

---

### S-03 processTeamMessageStream 后续请求缺失 waiter 注册 + error 响应 + complete chunk

**Python 参考**（L784-822）：
- 有 query 时：调用 `team_manager.interact(session_id, query)`，失败则 yield `chat.error` + return
- 无 query 时：仅提交 followup，不注册 waiter
- 两种情况都 yield `is_complete=True` 的 chunk

**Go 问题**（L288-301）：
1. 缺失 Python 的"无 query 时跳过 interact"逻辑（Python L790: `if query:`）
2. 缺失 interact 失败时的 `chat.error` 响应 chunk
3. 缺失后续请求的 waiter 注册逻辑
4. 缺失 `is_complete=True` 的结束 chunk

**修复建议**：补全后续请求路径：query 非空时 interact + 失败时 yield error chunk + 无 query 时跳过 interact + yield complete chunk。

---

### S-04 processTeamMessageStream 缺失 wait_for_pending_shutdown_cleanup 前置等待

**Python 参考**（L624-635）：
```python
try:
    from jiuwenswarm.agents.harness.team.remote_member_bootstrap import (
        wait_for_pending_shutdown_cleanup_for_session,
    )
    await wait_for_pending_shutdown_cleanup_for_session(session_id)
except Exception as exc:
    logger.warning(...)
```

**Go 问题**：`processTeamMessageStream` 完全缺失此步骤，搜索 `WaitForPendingShutdown` 结果为 0 匹配。Go 端在 session 可能还有正在清理的旧 team 时就启动新 team，可能造成竞态。

**修复建议**：实现 `WaitForPendingShutdownCleanupForSession` 并在 `processTeamMessageStream` 开头调用。

---

### S-05 processTeamMessageStream sessionID 默认值缺失

**Python 参考**（L617）：`session_id = request.session_id or "default"`

**Go 问题**（L186-189）：
```go
sessionID := ""
if req.SessionID != nil {
    sessionID = *req.SessionID
}
```
空 sessionID 会导致 TeamManager 查询失败，Python 以 `"default"` 作为默认值。

**修复建议**：
```go
sessionID := "default"
if req.SessionID != nil && *req.SessionID != "" {
    sessionID = *req.SessionID
}
```

---

### S-06 HandleTeamSlashCommand /evolve 结果 items 传 nil 跳过 Records

**Python 参考**（L605）：
```python
items=list(getattr(evolve_result, "records", []) or [])
```

**Go 问题**（L317）：
```go
return approvalResultFromEventOrItems(
    skillName,
    evolveResult.ApprovalEvent,
    nil, // Python: items=list(getattr(evolve_result, "records", []) or [])
```
当 `ApprovalEvent` 为空但 `Records` 非空时，Python 走 `invalid_output` 分支，Go 走 `noChangesOutput` 分支，语义不一致。

**修复建议**：将 `evolveResult.Records` 转为 `[]map[string]any` 传入 `approvalResultFromEventOrItems`。

---

### S-07 HandleTeamEvolveListCommand 表格缺失 Used/Effect 列

**Python 参考**（L466-467）：
```python
"| # | Score | Used | Effect | Section | Content (preview) |",
```
每行记录输出 `used_str` 和 `effect_str`。

**Go 问题**（L169-170）：
```go
"| # | Score | Section | Content (preview) |",
```
表格完全缺少 `Used` 和 `Effect` 两列。`EvolutionRecord.UsageStats` 已有 `TimesUsed`/`TimesPresented`/`TimesPositive`/`TimesNegative` 字段，可直接使用。

**修复建议**：在表格头增加 `Used | Effect` 两列，对齐 Python 的 `used_str`/`effect_str` 格式。

---

### M-01 processTeamMessageStream hideDM/debug 变量被丢弃

**Python 参考**（L767-782）：Python 将 `hide_dm` 和 `debug` 放入 `stream_envs` 传递给 `_consume_stream_with_query`。

**Go 问题**（L221-222）：
```go
queryText = cleanedQuery
_ = hideDM
_ = debug
```
`hideDM` 和 `debug` 被显式丢弃，未传递到后台流任务。

**修复建议**：在实现首次请求分支时，将 `hideDM`/`debug` 传递到后台流任务的 envs 参数中。

---

### M-02 processTeamMessageStream 后续请求 query 取值来源不一致

**Python 参考**（L791）：`if query:` 中的 `query` 是经过 directive 清洗后的 `query`。

**Go 问题**（L290）：
```go
query := paramsString(inputs, "query", "")
```
后续请求重新从 `inputs` 提取原始 `query`，而非使用清洗过的 `queryText`。

**修复建议**：后续请求应使用 `queryText`（已经过 directive 清洗）。

---

### M-03 onTeamWatcherDone 缺失 popTeamEvolutionWatcher 调用

**Python 参考**（L1086-1098）：
```python
def _on_team_watcher_done(task):
    ...
    get_team_manager(channel_id).pop_team_evolution_watcher(session_id)
```

**Go 问题**（L663-665）：仅记录日志，完全缺失 `popTeamEvolutionWatcher` 调用，可能导致内存泄漏和重复检测误判。

**修复建议**：在 `onTeamWatcherDone` 中调用 `team.GetTeamManager(channelID).PopTeamEvolutionWatcher(sessionID)`，同时增加 `channelID` 参数。

---

### M-04 approvalResultFromEventOrItems 因 S-06 导致 items 判断语义反转

与 S-06 关联。Go 端 `/evolve` 命令永远传 `nil` 作为 items，导致即使 `evolveResult.Records` 非空也永远走 `noChangesOutput`。

**修复建议**：见 S-06。

---

### M-05 SyncTeamIdentityMetadata ctx 参数未被使用

**Go 问题**：`SyncTeamIdentityMetadata(ctx context.Context, ...)` 接受 ctx 但函数体中完全未使用。

**修复建议**：检查 `UpdateSessionMetadata` 是否接受 ctx 参数，若接受则传递；否则在注释中说明 ctx 预留原因。

---

### T-01 consumeStreamWithQuery / consumeMonitorEvents / watchTeamEvolutionAndPushTeam 全部为空壳占位

**Go 问题**：三个核心后台 goroutine 函数体均为空，仅记录日志。属于 `⤵️(#9.85)` 范围。

---

### T-02 ensureMonitorForActiveRuntime 为空壳占位

同上，属于 `⤵️(#9.85)` 范围。

---

### T-03 ensureTeamEvolutionWatcher 启动 goroutine 为空壳

前置检查已实现对齐 Python，但最终启动 goroutine 处是占位。属于 `⤵️(#9.85)` 范围。

---

### T-04 broadcastEvent 浅拷贝与 Python 行为一致但嵌套 map 有潜在并发风险

**Python 参考**：`dict(event)` 是一层浅拷贝。Go 端行为等价。但嵌套 map（如 `payload`）在多个 waiter 间共享引用，可能并发修改。Python 也存在同样问题，行为一致。

---

## 二、10.6.12 SessionOps — 13 项

### S-01 ForkSession metadata 缺少默认值导致 nil 写入

**Python 参考**（L131-133）：
```python
"user_id": source_meta.get("user_id", ""),
"last_message_at": source_meta.get("last_message_at", 0),
"message_count": source_meta.get("message_count", 0),
```

**Go 问题**（L239-250）：
```go
"user_id":         sourceMeta["user_id"],        // 缺失时 nil, Python 为 ""
"last_message_at": sourceMeta["last_message_at"], // 缺失时 nil, Python 为 0
"message_count":   sourceMeta["message_count"],    // 缺失时 nil, Python 为 0
```
下游消费者做 string 类型断言时可能 panic。

**修复建议**：
```go
userID, _ := sourceMeta["user_id"].(string)
lastMsgAt, _ := sourceMeta["last_message_at"].(float64)
msgCount, _ := sourceMeta["message_count"].(float64)
```

---

### S-02 ForkSession source_mode 缺少默认值 "code.normal"

**Python 参考**：`source_mode = source_meta.get("mode", "code.normal")`

**Go 问题**（L236）：`sourceMode, _ := sourceMeta["mode"].(string)` — 缺失时 `sourceMode = ""`，Python 默认 `"code.normal"`。

**修复建议**：
```go
sourceMode, _ := sourceMeta["mode"].(string)
if sourceMode == "" {
    sourceMode = "code.normal"
}
```

---

### S-03 getUniqueForkName 数字解析逻辑错误

**Python 参考**（L49）：
```python
num = int(m.group(1)) if m.group(1) else 1
used_numbers.add(num)
```

**Go 问题**（L821-824）：
```go
if m[1] != "" {
    if num, err := fmt.Sscanf(m[1], "%d", new(int)); err == nil && num == 1 {
        var n int
        fmt.Sscanf(m[1], "%d", &n)
        usedNumbers[n] = true
    }
}
```
两次 `Sscanf` 且第一次结果被丢弃，若 `Sscanf` 返回 `num != 1` 数字被跳过。

**修复建议**：
```go
if m[1] != "" {
    if n, err := strconv.Atoi(m[1]); err == nil {
        usedNumbers[n] = true
    }
} else {
    usedNumbers[1] = true
}
```

---

### S-04 ListSessionTurns / RestoreSessionFiles diff service 调用无 recover 防御

**Python 参考**（L248-268）：
```python
try:
    diff_service = get_diff_service()
    turn_diffs = diff_service.get_turn_diffs(session_id)
except Exception as exc:
    logger.debug("list_session_turns: diff service unavailable: %s", exc)
```

**Go 问题**：`GetDiffService()`、`GetTurnDiffs()`、`GetFilesToRestore()` 无任何 `defer recover` 或 error 检查。如果 panic，整个 SessionOps 方法崩溃，无优雅降级。

**修复建议**：用 `defer func() { recover() }()` 包裹 diff service 调用块。

---

### M-01 CopySessionState plan_slug 只在 copyFile 成功时更新

**Python 参考**（L596-598）：
```python
if old_plan_path.exists():
    shutil.copy2(old_plan_path, new_plan_path)
plan_mode["plan_slug"] = new_slug  # 总是更新
```

**Go 问题**：`planMode["plan_slug"] = newSlug` 只在 `copyFile` 成功时执行。如果旧 plan 文件不存在，Python 仍给新 session 新 slug，Go 保留旧 slug。

**修复建议**：将 `planMode["plan_slug"] = newSlug` 移到 `os.Stat` 判断外面：
```go
if oldSlug != "" {
    workspaceRoot := workspace.AgentWorkspaceDir()
    newSlug := agentmode.GetOrCreatePlanSlug(workspaceRoot)
    oldPlanPath := agentmode.ResolvePlanFilePath(workspaceRoot, oldSlug)
    newPlanPath := agentmode.ResolvePlanFilePath(workspaceRoot, newSlug)
    if _, err := os.Stat(oldPlanPath); err == nil {
        copyFile(oldPlanPath, newPlanPath)
    }
    planMode["plan_slug"] = newSlug  // 无论 copyFile 是否成功都更新
    modifiedState["plan_mode"] = planMode
}
```

---

### M-02 RewindSessionContext SaveState 缺少 panic 保护

**Python 参考**（L486-492）：
```python
try:
    deep_agent.save_state(session)
except Exception as save_exc:
    logger.warning("rewind_session_context: deep_agent.save_state failed: %s", save_exc)
```

**Go 问题**（L643）：直接调用 `deepAgent.SaveState(sess, deepAgent.LoadState(sess))`，没有 recover。

**修复建议**：用 `defer recover` 包裹。

---

### M-03 RewindSessionContext state wipe 缺少异常保护

**Python 参考**（L467-471）：`session.update_state({"context": None})` 包在 try-except 中。

**Go 问题**（L624-625）：
```go
sess.UpdateState(map[string]any{"context": nil})
sess.UpdateState(map[string]any{agentschema.SessionStateKey: nil})
```
无错误处理。

**修复建议**：添加 recover 保护。

---

### M-04 CopySessionState plan file 复制失败时缺少日志

**Go 问题**：`copyFile` 失败时静默跳过，无任何日志。Python 至少记录 debug 日志。

**修复建议**：添加 `else` 分支记录 warn/debug 日志。

---

### M-05 ForkSession 获取 existing_titles 缺少异常保护

**Python 参考**（L115-123）：包在 try-except 中。Go 端无保护。

**修复建议**：用 defer recover 包裹。

---

### T-01 ForkSession forked_from.original_id 缺少默认值

**Python**：`record.get("id", "")` — 默认 `""`。**Go**：`record["id"]` — 缺失时为 `nil`。

**修复建议**：提供默认空字符串。

---

### T-02 getUniqueForkName 数字解析代码冗余

`fmt.Sscanf` 做了两次扫描。应使用 `strconv.Atoi` 一步完成（同 S-03）。

---

### T-03 RestoreSessionFiles "write" action 下 RestoreContent 为 nil 时静默跳过

**Go 问题**：不执行任何操作也不记录日志。

**修复建议**：添加 warn 日志。

---

### T-04 deepCopyMap 失败时静默返回空 map

**Go 问题**：JSON marshal/unmarshal 失败时返回 `make(map[string]any)`，后续修改空 map 而非深拷贝的原始数据。

**修复建议**：让 `deepCopyMap` 返回 error，调用方决定是否继续。

---

## 三、7.20 Dreaming + Sweeper — 12 项

### S-01 CJK 字符串截断 — byte 切片 vs 字符切片

**Python 参考**：
```python
content = str(e.get("content", ""))
parts.append(f"[User]: {content[:2000]}")     # 按 Unicode 字符切片
result = _UI_TEXT[self._language]["truncate_hard"] + result[-max_chars:]
```

**Go 问题**（`sweeper.go` L730, L737, L746, L762）：
```go
content = content[:2000]   // byte 切片 — 截断 CJK 字符产生无效 UTF-8!
result = ui.TruncateHard + result[len(result)-maxChars:]  // byte 切片
estTokens := len(result) / 2  // byte 长度/2 vs Python 字符长度/2，CJK 内容 ~3x 偏差
```

**修复建议**：
```go
func truncateString(s string, maxChars int) string {
    runes := []rune(s)
    if len(runes) > maxChars {
        return string(runes[:maxChars])
    }
    return s
}
// estTokens 使用 utf8.RuneCountInString(result) / 2
```

**流程示例**：
```
compress() 中 CJK 截断流程（Python 正确 vs Go 错误）:

Python: content = "你好世界..." (10 CJK chars = 30 bytes)
        content[:2000]  → 取前 2000 个字符 → 正确

Go:     content = "你好世界..." (10 CJK chars = 30 bytes)
        content[:2000]  → 取前 2000 个字节 → 截断点可能落在 3 字节 CJK 中间
                         → 产生无效 UTF-8 → 乱码

修复:   []rune(content)[:2000] → 取前 2000 个 rune → 与 Python 行为一致
```

---

### S-02 extractViaLLM 返回 error 导致 checkpoint 更新语义偏差

**Python 参考**：
```python
try:
    response = await model.invoke([...])
    return parsed if isinstance(parsed, list) else []
except Exception as exc:
    logger.warning("[Sweeper] LLM call failed: %s", exc)
    return []  # 永不抛异常
```

**Go 问题**：
```go
response, err := model.Invoke(ctx, messages)
if err != nil {
    return nil, fmt.Errorf("LLM 调用失败: %w", err)  // 返回 error!
}
```
Python 永不抛异常，LLM 失败返回 `[]`，session 仍被加入 `succeeded_ids`，checkpoint 更新。Go 返回 error，session 被加入 `failedIDs`，checkpoint 不更新，导致同一 session 每次循环都被重新处理。

**修复建议**：`extractViaLLM` 的 `NewModel` 和 `model.Invoke` 错误应返回 `nil, nil`（对齐 Python 的 `return []`），加 Warning 日志。

**流程示例**：
```
Sweeper.runSweep() 中 extractViaLLM 的结果处理:

Python 路径:
  extract_via_llm() 失败 → return []
  → session 加入 succeeded_ids → checkpoint 更新 → 下次不再扫描

Go 路径（当前 bug）:
  extractViaLLM() 失败 → return nil, err
  → session 加入 failedIDs → checkpoint 不更新
  → 下次扫描时 session 仍在 new_ids 中 → 重复处理 → LLM 反复失败 → 无限循环

修复后 Go 路径:
  extractViaLLM() 失败 → return nil, nil (对齐 Python)
  → session 加入 succeeded_ids → checkpoint 更新 → 下次不再扫描
```

---

### S-03 RunSweep defer/recover 范围与 Python 的 scan-only try/except 不匹配

**Python 参考**：scan 阶段的 `try/except` 只覆盖 scan 调用。

**Go 问题**：`defer recover` 覆盖整个 `RunSweep` 函数（包括 LLM 和 promotion 阶段），日志消息 "Scan stage failed" 误导。

**修复建议**：将 `defer recover` 缩小到只覆盖 scan 调用，或移除并让各阶段自行处理错误。

---

### S-04 ScanNewSessions 缺失 per-session try/except

**Python 参考**：
```python
for session_id in sorted(new_ids):
    try:
        mtime = session_dir.stat().st_mtime
        events = self._parse_history(session_dir)
        compressed = self._compress(events)
    except Exception as exc:
        logger.warning("[Sweeper] Scan session %s failed: %s", session_id, exc)
```

**Go 问题**：每个 session 的处理没有 `defer recover` 保护。如果 `ParseHistory` 或 `compress` panic，整个 scan 中止。

**修复建议**：为每个 session 迭代添加 `defer recover` 函数。

---

### S-05 HistoryEvent.Content 丢失非字符串内容

**Python 参考**：
```python
content = str(e.get("content", ""))  # 任意类型转字符串
```

**Go 问题**：
```go
type HistoryEvent struct {
    Content   string `json:"content"`  // 非 string 内容被 json.Unmarshal 丢弃
}
```
`history.json` 中 `content` 可能是 list 或 dict（如 tool call content），Go 反序列化后为空字符串。

**修复建议**：使用 `json.RawMessage` 或 `any` 类型字段，手动转为字符串对齐 Python 的 `str()` 行为。

---

### M-01 Checkpoint 向后兼容 — list 格式未处理

**Python 参考**：
```python
raw = cp.get("scanned_sessions", [])
if isinstance(raw, list):
    self.scanned_sessions = {sid: {} for sid in raw}
elif isinstance(raw, dict):
    self.scanned_sessions = raw
```

**Go 问题**：`CheckpointData.ScannedSessions` 是 `map[string]ScannedSession`，如果旧 checkpoint 的 `scanned_sessions` 是 list，Go 反序列化失败，返回空 `CheckpointData`，丢失所有扫描状态。

**修复建议**：使用 `json.RawMessage` 并手动处理 list 和 dict 两种格式。

---

### M-02 Go loop 缺少 Python 的 unexpected-exception 处理

**Python 参考**：
```python
except Exception:
    logger.exception("[%s] loop terminated by unexpected error (running=%s, interval=%.0fs)", ...)
    self._running = False
```

**Go 问题**：loop 只检查 `context.Canceled`，其他异常只被 `tick` 内部记录，loop 继续运行。Python 在意外异常时终止循环。

**修复建议**：在 loop 中增加非 Canceled 错误的上下文日志。

---

### M-03 context.DeadlineExceeded 未作为取消信号处理

**Python 参考**：`asyncio.CancelledError` 对应 Go 的 `context.Canceled`。

**Go 问题**：`context.DeadlineExceeded` 也应导致循环退出，但当前未检查。

**修复建议**：
```go
if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
    return err
}
```

---

### M-04 Sweep cancelled 日志级别偏差

**Python**：`logger.exception("[Sweeper] sweep cancelled")` — ERROR 级别 + 堆栈。
**Go**：`logger.Info(logComponent).Msg("sweep cancelled")` — INFO 级别，无错误详情。

**修复建议**：改为 `logger.Error` + `.Err(err)` 或保持 Info 级别但注释说明有意降级。

---

### M-05 extractContentStr 可能遗漏裸字符串项

**Python 参考**：
```python
if isinstance(item, str):
    parts.append(item)
elif isinstance(item, dict):
    parts.append(item.get("text", ""))
```

**Go 问题**：只处理 `p.Type == "text"` 的情况，裸字符串项被丢弃。Python 还有 `return str(content)` 的 fallback。

**修复建议**：验证 `Parts()` 是否始终返回类型化 part；添加裸字符串处理。

---

### T-01 Go sweeper logComponent 用 ComponentAgentServer，orchestrator 用 ComponentAgentCore

同一 dreaming pipeline 的两个组件使用不同日志组件，可能干扰日志关联分析。

---

### T-02 Prompt 模板格式差异 — Python 用 `{{` 转义，Go 用 backtick 字符串

两者对各自格式引擎正确，无 bug。

---

## 四、Monitor 模块 — 19 项

### S-01 TeamMonitor.Stop 关闭 eventCh 后 Start 无法恢复原有消费者

**Python 参考**：
```python
# stop(): self._event_queue.put_nowait(None)  # sentinel
# events(): while True: event = await self._event_queue.get(); if event is None: break
# start(): self._event_queue = asyncio.Queue()  # 新建 queue
```

**Go 问题**（`team_monitor.go` L149-171）：
```go
func (m *TeamMonitor) Stop(ctx context.Context) error {
    close(m.eventCh)  // 关闭 channel
    m.eventCh = nil    // 置 nil
}
```
`close` 后原有消费者 goroutine 永久退出，即使 `Start()` 重建 channel 也不会自动恢复。

**修复建议**：不 close channel，改用 sentinel nil 值终止迭代；或在 `collectEvents` 中检测 channel 关闭后重新获取 `Events()`。

**流程示例**：
```
Python Monitor 生命周期:
  start() → _event_queue = asyncio.Queue()
  events() → while True: event = await queue.get(); if None: break
  stop()   → queue.put_nowait(None)  # sentinel
  start()  → _event_queue = asyncio.Queue()  # 新 queue，可重新消费

Go Monitor 生命周期（当前 bug）:
  Start()  → eventCh = make(chan MonitorEvent, 256)
  Events() → return eventCh
  Stop()   → close(eventCh); eventCh = nil  # channel 关闭
  Start()  → eventCh = make(...)  # 新 channel
             但 collectEvents goroutine 已退出（range 关闭 channel 后退出）
             没有机制让 collectEvents 重新订阅新 channel

修复方案 1（sentinel）:
  Stop() → eventCh <- sentinel  // 不 close
  Events() → for evt := range eventCh { if isSentinel(evt) { break } }
  Start() → 不需要重建 channel

修复方案 2（重新订阅）:
  collectEvents 改为循环: 每次 mon.Events() 获取新 channel
```

---

### S-02 collectEvents 启动后 channel 关闭导致 goroutine 永久退出

**Go 问题**（`monitor_handler.go` L258-299）：
```go
eventCh := mon.Events()  // 只取一次
for evt := range eventCh {  // channel 关闭后永久退出
```
与 S-01 关联。channel 关闭后 goroutine 结束，后续无法恢复。

**修复建议**：改为 select 循环 + 动态获取 Events() channel。

---

### S-03 getMessageContent 遍历全部消息查找单条 — O(N) 性能问题

**Python 参考**：`_get_message_content(message_id)` 也是 O(N) 遍历。

**Go 问题**（`monitor_handler.go` L522）：`GetMessages(ctx, "", "")` 获取全部消息再遍历匹配。与 Python 行为一致，但每收到一个 message/broadcast 事件都全量查询，性能严重。

**修复建议**：添加 `GetMessageByID(ctx, messageID)` 方法，直接按 ID 查询单条消息。

---

### S-04 NewTeamStreamLogger 路径解析在 Windows 上可能失败

**Go 问题**（`stream_logger.go` L110）：
```go
if err := os.MkdirAll(filePath[:strings.LastIndex(filePath, "/")], 0o755); err != nil {
```
硬编码 Unix 路径分隔符 `/`，Windows 上 `LastIndex` 返回 -1，路径错误。

**修复建议**：使用 `filepath.Dir(filePath)` 替代手动字符串切割。

---

### S-05 renderRole 对 *TeamRole 指针使用 fmt.Sprintf("%v") 输出指针地址

**Python 参考**：`_render_role(role)` 直接调用 `role.value` 返回字符串值。

**Go 问题**（`stream_logger.go` L466-471）：
```go
func renderRole(role any) string {
    return fmt.Sprintf("%v", role)  // *TeamRole → 指针地址如 "0xc0001234"
}
```

**修复建议**：
```go
if tr, ok := role.(*TeamRole); ok { return string(*tr) }
```

---

### S-06 extractContent 对非 dict/非 string payload 返回 fmt.Sprintf("%v") 导致大量输出

**Python 参考**：`_extract_content(payload)` 对非标准类型也 `str(payload)`，但 Python 的 `str()` 输出较友好。

**Go 问题**（`stream_logger.go` L438-451）：`fmt.Sprintf("%v", payload)` 对 struct 输出字段名+值，可能产生巨量文本，缓冲到 `run.buf` 中导致内存问题。

**修复建议**：对非 dict/非 string 的 payload 返回空字符串或短摘要。

---

### M-01 SDK_TO_TEAM_EVENT_MAP 缺少 MemberCanceled 映射（Go 有但 Python 无）

**Go 端**添加了 `TeamEventMemberCanceled` 和对应映射，但 Python 的 `event_types.py` 没有此映射。

**修复建议**：如果有意扩展，保留并加注释；否则移除以对齐 Python。

---

### M-02 handleMemberCanceled 在 Python monitor_handler 中不存在

**Go 问题**：`handleMemberCanceled` 设置 `reason` 字段，但 Python 的 `MEMBER_CANCELED` 只设置 `force`，**没有 reason**。

**修复建议**：与 Python 对齐，移除 `reason` 字段或不处理 MEMBER_CANCELED。

---

### M-03 interactionSummary 缺少 getattr(payload, "id", "unknown") 回退分支

**Python 参考**：
```python
else:
    iid = getattr(payload, "id", "unknown")  # 从对象属性获取 ID
```

**Go 问题**：只处理 `map[string]any`，非 dict payload 固定 `iid = "unknown"`。

**修复建议**：如果交互 payload 在 Go 端始终是 map，可保持现状并加注释。

---

### M-04 GetTeamSnapshot 未传播 GetTeamInfo 的错误

**Go 问题**（`monitor_handler.go` L173）：
```go
teamInfo, _ := mon.GetTeamInfo(ctx)  // error 被忽略
```
GetTeamInfo 失败时 `leaderName` 为空，不会过滤 leader。

**修复建议**：处理 error，至少记录日志。

---

### M-05 consumeMonitorEvents 中 eventQueue channel 关闭后不会重新打开

**Go 问题**：`teamMonitorHandlerImpl` 的 `eventQueue` 只有写入没有 close，`for evt := range handler.Events()` 永不退出（除非 `ctx.Done()` 触发）。

**修复建议**：在 `Stop()` 中关闭 `eventQueue`，或在 `consumeMonitorEvents` 中用 `select + ctx.Done()` 代替 range。

---

### M-06 autoStartEvolutionWatcher 和 consumeMonitorEvents 竞争同一 eventQueue channel

**Go 问题**：两个 goroutine 都 `range handler.Events()`，Go channel 每个事件只被一个消费者收到（非广播）。Python 只有一个消费者。

**修复建议**：移除 `autoStartEvolutionWatcher` 中的 `range handler.Events()`，或让 `consumeMonitorEvents` 广播事件到多个 channel。

---

### M-07 extractContent 对 dict 中 content/output 为空字符串时语义差异

**Python**：`payload.get("content", "") or payload.get("output", "")` — 空字符串是 falsy，继续尝试 output。
**Go**：`if c, _ := m["content"].(string); c != ""` — 行为一致。无 bug，仅确认。

---

### T-01 toolCallSummary 中 argsRaw 对 nil 值输出 "<nil>"

**Go 问题**（`stream_logger.go` L334）：
```go
argsRaw := fmt.Sprintf("%v", m["tool_args"])  // key 不存在 → "<nil>"
if name == "" && argsRaw == "" {              // "<nil>" != ""，回退逻辑不触发
```

**修复建议**：先检查 key 是否存在：`if v, ok := m["tool_args"]; ok { argsRaw = fmt.Sprintf("%v", v) }`。

---

### T-02 safeWrite 中 file.Sync() 比 Python 的 flush() 更重

**Python**：`self._file.flush()` 只刷新写缓冲区。**Go**：`l.file.Sync()` 调用 fsync，每次写入刷磁盘，严重影响性能。

**修复建议**：只在最终关闭时调用 `Sync()`，feed 期间依赖 Go runtime 的自动刷新。

---

### T-03 Feed 缺少对 nil chunk 的防护

**Go 问题**：nil chunk 计入了 `chunkCount`，导致 `[INFO] stream end, N chunks` 中的计数虚高。

**修复建议**：将 `l.chunkCount++` 移到 `if !ok { return }` 之后。

---

### T-04 createMonitor 缺少 leader 角色校验

**Python 参考**：`create_monitor()` 校验 `team_agent.role != TeamRole.LEADER` 时抛 ValueError。**Go** 不校验角色。

**修复建议**：当 TeamAgent 角色查询方法稳定后补充。

---

### T-05 capStr 截断行为与 Python 一致

确认 Go 的 `[]rune` 切片与 Python 的 code point 切片行为一致。无 bug。

---

### T-06 boundSession 修改调用者 ctx 中的 SessionState

**Go 问题**（`team_monitor.go` L351-356）：
```go
if state != nil {
    state.SetSessionID(m.sessionID)  // 直接修改，退出后不恢复
}
```
Python 用 contextvars token 机制，退出 context manager 后自动恢复。

**修复建议**：创建新的 SessionState 副本而非修改原对象。

---

## 五、7.16 + 7.17 Memory Provider — 16 项

### S-01 AgentArts Initialize 无 sessionID 时跳过 ensureMemorySession

**Python 参考**（L101-102）：
```python
session_id = kwargs.get("session_id")
await self._ensure_memory_session(session_id, actor_id=..., assistant_id=...)
```
Python **始终**调用 `_ensure_memory_session`，即使 `session_id=None`。当 session_id 为 None 且 `self._session_id` 为空时抛出 `RuntimeError`。

**Go 问题**（L194-199）：
```go
if sessionID != "" {
    if _, err := p.ensureMemorySession(ctx, sessionID, ...); err != nil {
        return fmt.Errorf("ensureMemorySession 失败: %w", err)
    }
}
```
Go 在 `sessionID == ""` 时完全跳过 `ensureMemorySession`，可以在无 session_id 的情况下初始化成功，而 Python 必定失败。

**修复建议**：移除 `if sessionID != ""` 守卫，始终调用 `ensureMemorySession`。

---

### S-02 AgentArts Initialize actor_id/assistant_id 覆盖语义与 Python 不一致

**Python 参考**（L98-99）：
```python
self._actor_id = kwargs.get("user_id")  # None 时直接覆盖为 None
```

**Go 问题**（L175-181）：
```go
if po.UserID != "" {  // 仅非空时覆盖
    p.actorID = po.UserID
}
```
Python `initialize(user_id=None)` 会把 `actor_id` 清空，Go 保留旧值。

**修复建议**：对齐 Python，当 ProviderOption 中显式传入空值时也覆盖。Go 的 functional option 模式可能需要区分"未传入"和"传入空值"。

---

### S-03 OpenJiuwen Prefetch "Related History Summaries" 缺少前导换行符

**Python 参考**（L196）：`parts.append("\n## Related History Summaries")`
**Go 问题**（L437）：`parts = append(parts, "## Related History Summaries")`

`strings.Join(parts, "\n")` 只在元素间插入 `\n`，不会产生 Python 等价的前导空行。

**实际效果差异**：
- Python：`"## Related Memories\n- ...\n\n## Related History Summaries"`（双换行分隔）
- Go：`"## Related Memories\n- ...\n## Related History Summaries"`（单换行分隔）

**修复建议**：改为 `parts = append(parts, "\n## Related History Summaries")`。

---

### S-04 OpenJiuwen 全局 LTM 单例 vs Python 实例级 LTM — 架构偏差

**Python**：每个 Provider 实例持有自己的 `self._ltm`。
**Go**：所有 Provider 共享 `ltm.GetLongTermMemory()` 全局单例。

**修复建议**：非 bug，在 doc.go 或注释中标注此架构差异。

---

### M-01 AgentArts SyncTurn 缺少 assistant_id 优先级

**Python 参考**（L290-291）：
```python
assistant_id = self._runtime_assistant_id(kwargs)  # assistant_id → scope_id → self._assistant_id → default
```

**Go 问题**：只检查 `po.ScopeID`（对应 `scope_id`），完全缺失 `assistant_id` 优先级。`ProviderOptions` 没有 `AssistantID` 字段。

**修复建议**：在 `ProviderOptions` 中增加 `AssistantID string` 字段，并在 `SyncTurn` 中实现完整优先级链。

---

### M-02 OpenJiuwen HandleToolCall 两个已知工具 case 的 error 处理逻辑重复

**Go 问题**：`ltm_search` 和 `ltm_search_summary` 的 error 处理代码完全重复。

**修复建议**：合并 error 处理逻辑。

---

### M-03 AgentArts Search 中 actorID 空字符串 vs 缺失字段

**Python**：`_runtime_actor_id` 返回空字符串时也设到 filter_kwargs 中。
**Go**：`omitempty` 使空字符串不被序列化。

**修复建议**：Go 行为更合理，保持但标注差异。

---

### M-04 createKVStore 默认路径与 Python 不一致

**Python**：相对路径 `memory_kv.db` / `memory_kv`。
**Go**：绝对路径 `{workspace_dir}/memory/ltm/memory_kv.db` / `{workspace_dir}/memory/ltm/kv`。

**修复建议**：标注 Go 故意使用 workspace 绝对路径。

---

### M-05 createDBStore 默认文件名与 Python 不一致

**Python**：`memory.db`。**Go**：`ltm.db`。

**修复建议**：统一为 `memory.db` 或标注差异。

---

### M-06 AgentArts EnsureMemorySession actor_id/assistant_id 条件设置

**Python**：只在非空时设置到 payload。**Go**：始终设置（`omitempty` 处理序列化）。

行为等价，但语义上 Go 不做守卫检查。

---

### M-07 OpenJiuwen Prefetch 对空查询没有守卫检查

Go 和 Python 的 OpenJiuwen 都没有空查询检查。行为一致，无需修改。

---

### T-01 handleSearch/handleSearchSummary 中局部类型无法被外部反序列化

**Go 问题**：`searchResult`/`summaryResult` 是方法内局部类型，转为 `map[string]any` 后外部看到的是 map。

**修复建议**：考虑提升为包级类型。

---

### T-02 AgentArts IsInitialized 使用读写锁但 OpenJiuwen 不加锁

风格不一致。OpenJiuwenProvider 的 `initialized` 只在 Initialize/Shutdown 中写入，不应并发。

**修复建议**：在 OpenJiuwenProvider 中也加锁或注释说明原因。

---

### T-03 OpenJiuwen Shutdown 未清理 LTM 单例上的 store 注册

Python 也未清理。行为一致，但 Shutdown 后 LTM 单例仍保留旧 store。

---

### T-04 createEmbedding 缺少 api_key 默认值兜底

**Python**：`api_key=embed_cfg.get("api_key")` — 可以是 None。
**Go**：类型断言不存在时返回空字符串 `""`。

**修复建议**：确认 `EmbeddingConfig` 是否区分空字符串和缺失 API key。

---

### T-05 SyncTurn 返回 error vs Python void 语义

有意偏差：Go 签名返回 error，Python 吞错返回 None。需确认调用方是否预期 error。

---

## 六、13.1 + 13.4 Indexer/Retrieval — 10 项

### S-01 MultimodalDocument cache 未在 AddField 时失效

**Python 参考**（`document.py` L226-227）：
```python
def add_field(self, ...):
    self._content_cache.clear()
    self._dashscope_cache.clear()
```

**Go 问题**：使用 `sync.Once`，一旦计算缓存永不清除，即使 `AddField` 再次调用。

**修复建议**：在 `AddField` 中重置 `sync.Once`：
```go
d.contentCache = nil
d.contentOnce = sync.Once{}
d.dashscopeCache = nil
d.dashscopeOnce = sync.Once{}
```

---

### S-02 Content()/DashscopeInput() 返回可变缓存引用

**Python 参考**：
```python
return deepcopy(self._content_cache)  # 深拷贝
```

**Go 问题**：直接返回 `d.contentCache`，调用者修改会破坏缓存。

**修复建议**：返回深拷贝，或文档标注返回值不可修改。

---

### S-03 loadFromFile 缺少 MIME type kind-override 逻辑

**Python 参考**（L302-303）：
```python
if not mime_type.startswith(kind):
    mime_type = "/".join([kind] + mime_type.split("/")[1:])
```

**Go 问题**：`loadFromFile` 只检查 MIME type 是否为空，不做 kind-override。

**修复建议**：
```go
if !strings.HasPrefix(mimeType, string(kind)) {
    parts := strings.SplitN(mimeType, "/", 2)
    if len(parts) == 2 {
        mimeType = string(kind) + "/" + parts[1]
    }
}
```

---

### S-04 缺少 .jfif MIME type 注册

**Python 参考**（L27）：`mimetypes.add_type("image/jpeg", ".jfif", strict=True)`

**Go 问题**：`mime.TypeByExtension(".jfif")` 返回 `""`，导致 `loadFromFile` 失败。

**修复建议**：
```go
func init() {
    mime.AddExtensionType(".jfif", "image/jpeg")
}
```

---

### M-01 VectorStoreConfig 缺少 DistanceMetric 默认值

**Python**：`distance_metric: Literal[...] = Field(default="cosine")`
**Go**：零值 `""`，`Validate()` 会失败。

**修复建议**：在 `Validate()` 中将空字符串视为 `cosine`，或添加 `NewDefaultVectorStoreConfig` 构造函数。

---

### M-02 RetrievalConfig.Validate() 是空操作

**Python**：Pydantic 自动验证类型。**Go**：`Validate()` 返回 `nil` 不做任何检查。

**修复建议**：添加基本验证：`TopK > 0`，`ScoreThreshold` 在 [0,1] 范围内。

---

### M-03 ComputeChunkEmbeddings callback 参数不如 Python 显式

**Python**：显式 `doc_index_callback` 参数。**Go**：通过 `opts ...EmbedOption` 透传。

功能等价但 API 可发现性较差。

**修复建议**：添加注释说明 callback 应通过 `opts` 传递。

---

### T-01 Python 对空结果重新计算；Go sync.Once 真正只计算一次

微小的语义差异，不影响正确性。

---

### T-02 IndexOption vs Python **kwargs — 正确适配

Go 使用 `...IndexOption` + `Extra map[string]any`，是正确的 Go 惯用法。

---

### T-03 TextChunk JSON tag `id_` 对齐 Python — 正确

确认对齐。

---

## 七、Evolution 重构 — 13 项

### S-01 watchEvolutionAndPush 缺失完整生命周期管理

**Python 参考**（L5765-5901）：包含 9 个阶段：
1. auto_scan 检查（入口 + 每次循环）
2. active/just_started 状态追踪
3. 首个可见 progress 时推送 "start" status
4. 中间阶段推送 "progress" status
5. 终端阶段推送 "end" status
6. 事件超时检测（idle timer + `resolve_evolution_event_timeout_sec`）
7. `_cleanup_evolution_rail()` 清理
8. CancelledError 处理
9. 异常处理

**Go 问题**（`deep_adapter_evolution.go` L165-221）：极简骨架，只做：poll → push 非 approval/outcome dicts → push approval/outcome → return。完全跳过前端依赖的 status 生命周期（start/progress/end）。

**修复建议**：重写 `watchEvolutionAndPush` 对齐 Python 6 阶段结构。

**流程示例**：
```
Python watchEvolutionAndPush 完整流程:

Phase 1 (入口检查):
  if not auto_scan → return

Phase 2 (主循环):
  while True:
    events = drain_pending_approval_events(wait=False)
    if not events:
      idle_for = monotonic() - last_event_at
      if idle_for >= event_timeout_sec:
        if active: push_status("end", "hidden", timeout_msg)
        cleanup_evolution_rail(); return
      sleep(1.0); continue

    last_event_at = monotonic()

Phase 3 (start status):
    visible_progress = visible_evolution_progress_from_events(events)
    if not active and visible_progress:
      push_status("start", start_stage, start_message)
      active = True; just_started = True

Phase 4 (progress status):
    for progress in progress_statuses_to_push:
      if not progress.terminal:
        push_status("progress", progress.stage, progress.message)

Phase 5 (approval flow):
    if approval_events:
      if not active: push_status("start", "approval_required", "")
      for evt in approval_events: push_approval(evt)
      push_status("end", "approval_required", "")
      cleanup_evolution_rail(); return

Phase 6 (outcome/terminal flow):
    if outcomes:
      outcome = outcomes[-1]
      end_stage = map_hidden_terminal(outcome.status)
      if not active: push_status("start", end_stage, message)
      push_status("end", end_stage, message)
      cleanup_evolution_rail(); return

Error handling:
  except CancelledError: cleanup; raise
  except Exception: push_status("end", "hidden", ""); cleanup
```

---

### S-02 defaultSendPushFunc 是包级变量，写入无同步

**Go 问题**（`deep_adapter.go` L281）：
```go
var defaultSendPushFunc func(ctx context.Context, msg map[string]any) error
```
`SetDefaultSendPushFunc` 写入，`getSendPushFunc` 从并发 goroutine 读取，Go 内存模型不保证可见性，存在数据竞争。

**修复建议**：使用 `sync.Once` + atomic 模式，或 `sync.RWMutex` 保护读写。

---

### S-03 watchEvolutionAndPush 用 2s 固定 ticker + 阻塞式 DrainPendingApprovalEvents(wait=true)

**Python 参考**（L5787-5807）：
```python
events = await self._skill_evolution_rail.drain_pending_approval_events(wait=False) or []
if not events:
    idle_for = time.monotonic() - last_event_at
    if idle_for >= event_timeout_sec: ... timeout
    await asyncio.sleep(TEAM_EVOLUTION_IDLE_SLEEP_SEC)  # 1.0s
```

**Go 问题**：
1. 轮询间隔 2s vs Python 1s
2. `DrainPendingApprovalEvents(true, nil)` 可能无限阻塞，不检查 timeout
3. 无超时检测

**修复建议**：改用 `DrainPendingApprovalEvents(false, nil)` + `time.Sleep(1s)` + timeout 检查。

---

### S-04 pushEventToFrontend 不推送 "end" status，不处理 hidden terminal stages

**Python 参考**（L5848-5873）：
```python
outcomes = [evolution_outcome_from_event(evt) for evt in events if is_evolution_outcome_event(evt)]
if outcomes:
    outcome = outcomes[-1]
    end_stage = "hidden" if stage in TEAM_EVOLUTION_HIDDEN_TERMINAL_STAGES else stage
    if not active: await _push_status("start", end_stage, message)
    await _push_status("end", end_stage, message)
    await _cleanup_evolution_rail(); return
```

**Go 问题**：outcome 事件只推送原始 payload，不映射 hidden terminal stages，不推送 start/end status。

**修复建议**：添加 proper outcome batch 处理 + hidden terminal stage mapping + start/end lifecycle。

---

### M-01 EvolutionOutcomeFromEvent 从 evt 顶层读取而非 payload

**Python 参考**：所有字段从 `payload = event_payload_dict(evt)` 读取。

**Go 问题**（`evolution/logic/helpers.go` L231-261）：
```go
meta, _ := evt["_evolution_meta"].(map[string]any)  // 应从 payload 读取
if s, ok := evt["status"].(string); ok {              // 应从 payload 读取
if m, ok := evt["message"].(string); ok {             // 应从 payload 读取
```

**修复建议**：将所有 `evt["xxx"]` 改为 `payload["xxx"]`。

---

### M-02 EvolutionProgressStatusFromEvent 从 evt 读取 stage/message

同 M-01，`evt["stage"]`/`evt["message"]`/`evt["content"]` 应改为 `payload["stage"]`/`payload["message"]`/`payload["content"]`。

---

### M-03 ExtractEvolutionRequestID 从 evt 读取 _evolution_meta

**Go 问题**（L265-280）：
```go
if meta, ok := evt["_evolution_meta"].(map[string]any) {  // BUG: 应为 payload["_evolution_meta"]
```

**修复建议**：改为 `payload["_evolution_meta"]`。

---

### M-04 缺失 evolution_watcher_tasks 跟踪和异常日志

**Python 参考**：
```python
task = asyncio.create_task(self._watch_evolution_and_push(rid, cid, session_id))
task.add_done_callback(self._on_evolution_watcher_done)
self._evolution_watcher_tasks.add(task)
```

**Go 问题**：无 watcher 任务跟踪，`onEvolutionWatcherDone` 只记录简单日志，不检查错误。

**修复建议**：添加 `map[string]context.CancelFunc` 或 `sync.WaitGroup` 追踪 watcher 任务。

---

### M-05 watchEvolutionAndPush 退出时从不调用 CleanupBackgroundTasks

**Python 参考**：每个退出路径都调用 `await _cleanup_evolution_rail()`。

**Go 问题**：`watchEvolutionAndPush` 在任何退出路径（timeout、approval complete、outcome complete）都不清理后台任务，可能泄漏 goroutine。

**修复建议**：添加 `defer d.cleanupEvolutionRail()`。

---

### M-06 PushEvolutionProgress 用整个循环的 recover 而非 per-event 错误处理

**Python 参考**：per-event try/except，一个事件失败不影响其他事件。
**Go 问题**：`defer recover` 覆盖整个循环，一个 panic 导致所有后续事件被跳过。

**修复建议**：移除顶层 `defer recover()`，改为 per-event panic recovery。

---

### T-01 GroupEvolutionApprovals 返回 nil 而非空切片

**Python**：返回 `[]`。**Go**：返回 `nil`。JSON 序列化时 Python 输出 `[]`，Go 输出 `null`。

**修复建议**：改为 `return grouped, []string{}`。

---

### T-02 evolution/logic 子包依赖方向正确

重构后 `logic` 子包无外部依赖，`evolution/` 依赖 `gateway_push`。循环依赖正确避免。

---

### T-03 globalSendPushFunc → 字段注入重构架构正确

`DeepAdapter.sendPushFunc` + `defaultSendPushFunc` + `getSendPushFunc()` 优先级链设计正确。唯一问题是 S-02 的数据竞争。

---

## 八、Team 类型重构 — 15 项

### S-01 AgentCustomizer 签名在两个包中不兼容

**Python**：`AgentCustomizer = Callable[[Any, Optional[str], str], None]`

**Go agent_teams 包**（`harness.go` L52）：
```go
type AgentCustomizer func(deepAgent DeepAgentInterface, memberName string, roleValue string)
```

**Go team 包**（`team_manager_spec.go` L17）：
```go
type AgentCustomizer func(ctx context.Context, agent interfaces.DeepAgentInterface, memberName string, role string) error
```

两个不同签名。`blueprint.AgentCustomizer any` 在类型断言时可能静默失败。

**修复建议**：统一为一个类型。`memberName` 应改为 `*string` 以匹配 Python 的 `Optional[str]`。

---

### S-02 blueprint.AgentCustomizer 存为 any，类型断言可能静默失败

**Go 问题**（`agent_configurator.go` L426-430）：
```go
if customizer, ok := spec.AgentCustomizer.(agentteams.AgentCustomizer); ok {
    harness.RunAgentCustomizer(customizer)
}
// else: 无任何日志，静默跳过
```

**修复建议**：添加 `else` 分支记录 warning，或改用具体接口类型替代 `any`。

---

### S-03 SyncTeamIdentityMetadata 缺少 activation_kind 守卫

**Python 参考**（L95-127）：
```python
normalized_kind = str(activation_kind or "").strip()
if normalized_kind not in _TEAM_CREATE_KINDS:
    return  # 只有 CREATE/NEW_TEAM_IN_SESSION 才更新
if existing_team_name and existing_team_name != ready_team_name:
    return  # 身份不匹配时保留旧值
```

**Go 问题**（L97-110）：
```go
func (m *TeamManager) SyncTeamIdentityMetadata(ctx context.Context, sessionID string, teamName string) {
    // 无 activationKind 检查，无条件写入
    // 无身份不匹配检查
}
```

**修复建议**：添加 `activationKind string` 参数，实现 `_TEAM_CREATE_KINDS` 守卫和身份不匹配检查。

---

### S-04 InteractInput.Raw 是 any — 未实现类型安全目标

**Go 问题**：引入 `InteractInput` 的目的是消除 `any`，但 `Raw any` 使 `any` 只是被推了一层。提取时 `rawPayload = userInput.Raw` 回到 `any`。

**修复建议**：使用 tagged union 或添加类型化访问器方法 (`AsString()`, `AsDict()` 等)。

---

### S-05 LiveRailEntry.Rail 接口值比较不等价于 Python 的 `is` 比较

**Python 参考**：`if live_rail is rail` — 身份比较。
**Go 问题**：`entry.Rail == rail` — 接口值比较（类型描述符 + 数据指针），非身份比较。如果同一 concrete rail 被不同接口包装，比较失败。

**修复建议**：使用 `reflect.ValueOf(entry.Rail).Pointer() == reflect.ValueOf(rail).Pointer()` 匹配 Python 的 `is` 语义。

---

### S-06 BuildTeamHarness 创建 harness 时 deepAgent 为 nil

**Python 参考**（`harness.py` L118）：`deep_agent = agent_spec.build()` — 构建实际 DeepAgent。
**Go 问题**：`NewTeamHarness(nil, rails, role, memberName, initialPlanMode)` — 始终传 nil。`RunAgentCustomizer(customizer)` 中 `h.deepAgent == nil`，customizer 使用 nil agent 会 panic。

**修复建议**：从 agentSpec 构建 DeepAgent，或标注为 placeholder 并在可用前注入 DeepAgent。

---

### S-07 TeamManager.Interact 返回 (bool, error) 但 Python 只返回 bool

**Python 参考**：
```python
async def interact(self, session_id: str, user_input: Any) -> bool:
    try:
        success = await Runner.interact_agent_team(...)
        return success
    except Exception as exc:
        logger.error("interact failed: %s", exc)
        return False  # 永不抛异常
```

**Go 问题**：Go 传播 error 给调用方，Python 吞掉所有异常返回 `False`。

**修复建议**：匹配 Python 行为：捕获 error，记录日志，返回 `(false, nil)`。

---

### M-01 AgentCustomizer memberName 无法为 nil

**Python**：`customizer(agent, member_name=None, role)` — `None` 触发 fallback。
**Go**：`memberName string` — 零值 `""` 无法区分"未传入"和"空字符串"。

**修复建议**：改为 `memberName *string`。

---

### M-02 TeamRailMountContext.Agent 类型化但 AddRail 未使用

**Python**：`context.agent.add_rail(rail)` — 实际调用。
**Go**：Agent 存储但未使用，`UpdateEvolutionConfig` 有 TODO。

**修复建议**：确认 `DeepAgentInterface` 包含 `AddRail` 方法。

---

### M-03 DrainTeamSkillEvents 参数与 Python 不一致

**Python**：`rail.drain_pending_approval_events()` — 无参数。
**Go**：`DrainPendingApprovalEvents(true, nil)` — 两个参数。

**修复建议**：验证 Go 版本语义等价，确保无事件被类型断言静默丢弃。

---

### M-04 getEvolutionAutoScanEnabled 默认值与 Python 不一致

**Python**：`config.get("auto_scan", False)` — 默认 False。
**Go**：`return true` — 默认 True。且 Python 从嵌套路径 `config["react"]["evolution"]["auto_scan"]` 读取。

**修复建议**：默认改为 `false`，读取嵌套路径，检查 `EVOLUTION_AUTO_SCAN` 环境变量。

---

### M-05 TeamWorkspaceInfo.Config 类型不够明确

**Python**：`config: dict[str, Any] | None = None` — 显式可选。
**Go**：`Config map[string]any` — 零值为 nil 但类型签名不体现。

**修复建议**：改为 `*map[string]any` 或文档标注 nil 语义。

---

### T-01 InteractInput 缺少类型化便捷构造函数

**修复建议**：添加 `NewInteractInputFromString`、`NewInteractInputFromDict` 等。

---

### T-02 ptrToStr 名称误导

接收 `string` 非 `*string`，但返回 `"<nil>"` 表示空字符串。

**修复建议**：重命名为 `displayStr` 或 `strOrNil`。

---

### T-03 RegisterTeamRailContext 非 leader context 被静默丢弃

**Python**：只有 leader 注册，但无日志。
**Go**：同样无日志，建议添加 debug 日志便于调试。

---

## P0 最高优先修复清单（影响功能正确性）

| 编号 | 章节 | 描述 | 影响 |
|------|------|------|------|
| S-01 | 10.3.8 | processTeamMessageStream 首次请求核心流程完全缺失 | Team 模式首次请求无响应 |
| S-05 | 10.3.8 | sessionID 默认值缺失（空串 vs "default"） | TeamManager 查询失败 |
| S-01 | 7.20 | CJK 字符串截断 byte 切片 | 中文内容乱码 |
| S-02 | 7.20 | extractViaLLM 返回 error 导致 checkpoint 不更新 | 同一 session 每次循环被重新处理 |
| S-05 | 7.20 | HistoryEvent.Content 丢失非字符串内容 | Tool call 内容丢失 |
| S-01 | Monitor | TeamMonitor.Stop 后 channel 不可恢复 | Monitor 无法重启 |
| S-01 | 7.16 | AgentArts Initialize 无 sessionID 时跳过 ensureMemorySession | 无 session 时初始化成功但 Python 必定失败 |
| S-01 | 13.1 | MultimodalDocument cache 未在 AddField 时失效 | 多步使用时返回陈旧数据 |
| S-01 | Evolution | watchEvolutionAndPush 缺失完整生命周期 | 前端无法显示 evolution 进度状态 |
| S-03 | Team | SyncTeamIdentityMetadata 缺少 activation_kind 守卫 | 非创建场景也写入 team_name |
| S-01 | Team | AgentCustomizer 两个包签名不兼容 | 类型断言静默失败，自定义逻辑被跳过 |

---

## P1 次优先修复清单（影响健壮性/一致性）

| 编号 | 章节 | 描述 |
|------|------|------|
| S-02 | 10.3.8 | 缺失 deep_agent 校验 + TeamSpec 构建（应在分支前） |
| S-03 | 10.3.8 | 后续请求缺失 waiter + error 响应 + complete chunk |
| S-04 | 10.3.8 | 缺失 wait_for_pending_shutdown_cleanup 前置等待 |
| S-06 | 10.3.8 | /evolve 命令 items 传 nil 跳过 Records |
| S-01 | 10.6.12 | ForkSession metadata 缺少默认值导致 nil 写入 |
| S-02 | 10.6.12 | ForkSession source_mode 缺少默认值 "code.normal" |
| S-03 | 10.6.12 | getUniqueForkName 数字解析逻辑错误 |
| S-04 | 10.6.12 | diff service 调用无 recover 防御 |
| S-03+4 | 7.20 | RunSweep defer/recover 范围错误 + 缺失 per-session 保护 |
| S-05+6 | Monitor | renderRole 指针输出 + extractContent 大量输出 |
| S-02 | 7.16 | actor_id 覆盖语义不一致 |
| S-03 | 7.16 | Prefetch 缺前导换行符 |
| S-02+3 | 13.1 | Content() 返回可变引用 + 缺少 kind-override |
| S-04 | 13.1 | 缺少 .jfif MIME type 注册 |
| S-02+4 | Evolution | defaultSendPushFunc 数据竞争 + outcome 不推 end status |
| S-03 | Evolution | watchEvolutionAndPush 用阻塞式 Drain |
| S-05+7 | Team | LiveRailEntry 比较语义偏差 + Interact 返回 error |

> 完整 114 项问题详情见上文各章节。
