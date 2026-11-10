# 2026-11-08 48 小时上库质量审查

> 审查范围：`a3a8a21e .. HEAD(7ffb0e073)`（2026-11-06 07:12 ~ 2026-11-08 01:59，258 个提交；上轮审查截止到 `a3a8a21e`，本轮增量即这 258 个提交）
> 代码规模：非测试 Go 文件 +57,611 / −13,136（含 247 个新增文件、10 个删除、7 个重命名）；测试与 `tests/integration` +31,523 / −3,586（其余为 docs 的 spec/plan 与豁免登记）
> 审查对象：提交原因与实现正确性；对齐 Python（产品层 `jiuwenswarm` 0.2.5 / 库层 `openjiuwen`）；桩与回填完整性；层级与步骤归属；稳定性、性能、可维护性、并发安全
> 审查方式：逐批读生产代码 + 对照 Python 同位置源码；关键结论均在本地读码复核，下文 `文件:行号` 以 HEAD `7ffb0e073` 为准。本地 `go build ./...` / `go vet` 通过，`swarm/runtime/session` 与 `agent_teams/harness` 的 `-race` 单测通过（仅用于确认基线，不作为结论依据）
> **只读审查，未修改任何代码**

---

## 零、结论摘要

| 级别 | 数量 | 一句话 |
|---|---|---|
| 严重 | 3 | 会话执行协调器"取消"在事实上不生效（两处），团队轮结束后 harness 从不收尾（一处）——三处都在主链路上，且都是"看起来实现了、实际没接上" |
| 一般 | 10 | `settleRoundDone` 丢护栏与批次语义、崩溃后 supervisor 静默退出、后台进程 Wait 未收尾、管道读端被遗留、行尾追加记录序错、死代码、上游分支缺失、文档行号与实际不符、桩的回填来源错位、空壳方法 |
| 提示 | 8 | 把整条对齐逻辑写成占位再 `_ =` 掉、time.After 累积、冗余分支、文档漂移、测试依赖本地时区、批量 cancel 超时统计、`GetCurrentGoal` 失效、`_ =` 吞错惯例 |

**先说这批里做得好的地方**（避免只看问题）：

1. **10.3.28 的四层拆分（model / execution_registry / session_lifecycle / session_delete）质量最高**。`model.go` 542 行对 `runtime/session/model.py` 261 行（Go 注释更密），`SessionExecutionState` / `SessionWorkKind` / `ExecutionCancelCause` / `TeamWaiterKind` 四个枚举取值与 `terminal` / `scheduled` / `latest_first` 三个派生属性逐值对得上；`MarkTerminal` 唯一 close 点与 `Done` 由执行方 defer 关闭这条"双信号"约定写进了注释并被测试锁住，是本轮最扎实的设计。
2. **10.3.28b 的 create 21 步主流程结构忠实**。`PrepareSessionCreate`（`session_provisioner.go:1248-1504`）与 Python `prepare_session_create`（`session_provisioner.py:595-900`）的段落顺序、`if existing_metadata:` / `elif explicit_tui_session and ...` / `elif channel_id.lower()=="tui" and ...` 三个分支的短路顺序完全一致，连 `"work_mode mismatch: project is '%s'%sbut request specified '%s'"` 的 37 空格分隔符都逐字保留。这是规则 9/12 要求的样子。
3. **25.0b 的纯渲染层是"逐字节对齐"的范例**。`inbound_tags.md` 中英两份 md5 与上游完全一致（`96533e0c…` / `29093d87…`）；`inbound_render.py` 12 个符号 ↔ `inbound_render.go` 12 个符号一一对应；`snapshot_kind_of` / `drop_superseded_snapshots` 这两个容易写成"另一个算法"的函数，Go 侧连"非快照条目取自身下标自然保留"的注释都照抄了，行为确实等价。
4. **D 组缺陷修复（D-01~D-16）逐个对得上上游提交**。尤其 D-05 发现的是**真实生产 bug**：`git worktree add` 写入的 `gitdir` 是绝对路径，旧代码无条件 `filepath.Join(wtPath, gitdir)` 把 `/tmp/x/wt` 与 `/tmp/x/repo/.git/worktrees/wt` 拼成 `/tmp/x/wt/tmp/x/repo/...`，读 HEAD 永远失败、快速恢复路径形同虚设。修法（绝对/相对分别处理 + 要求目标目录同时有 `HEAD` 和 `commondir`）比上游 Python 直接 `open(join(path))` 更稳，且理由写清了。
5. **D-12 的 PowerShell 判定修复有实证**。`_PS_VARIABLE_PATTERN → _PS_ASSIGNMENT_PATTERN` 的理由（裸 `$var` 在 bash/PowerShell 都合法，不能单独判定）说清了"旧实现为什么错"，不是照着上游 diff 抄一遍；后台 stderr 从 `nil` 改成管道 + 持续排空 + 早退拼进错误，解决了"只给退出码、调用方无从判断依赖缺失还是命令写错"的真实痛点。
6. **9.90a-2 的 `ResolveToolBinding` 自我纠错值得记一笔**。`bd1a62b37` 把原先"先断言 `*tool.ToolCard` 再查工具"改成与上游一字不差的四步顺序，并在提交说明里承认"原设计多出一步、理由是错的"。这类"发现自己在步骤顺序上加了戏，主动改回去"的提交，正是规则 12 想看到的。

**这批最需要警惕的模式**：三个严重问题共享同一个形状——**接口方法写全了、调用点也"有"，但两边用的不是同一个东西**。

- 取消链路：`runtime/interface.go` 用 `RequestID: &sessionID` 取消，而登记执行时 `request_id` 是真实请求号，于是取消永远匹配 0 条（S-01）。
- 取消后状态：`MarkTerminalIfActive` 先返回、后校验，`cancelled` 计数恒为 0（S-02）。
- 团队收尾：`CoordinationKernel.Stop` 有 10 步、`FinalizeRound` 有 2 步，但没有一处调用 `harness.Stop`（S-03），而 Python 的 `finalize_round` 明确有一句 `await host.resources.harness.stop()`。

三处都不是"写错了算法"，而是"环节之间没接上"。上一轮审查记录里提到的 `settleRoundDone` 丢护栏，本轮同类问题换了个位置又出现（G-01/G-02）——说明**光靠"读自己这一层"发现不了这类缺陷，必须顺着调用链走到执行体**。

---

## 一、严重（S）

### S-01 会话取消在事实上不生效：拿会话ID当请求ID去匹配

**这是哪条流程**：用户点"停止"→ WebSocket `chat.interrupt` → `UapClawSwarm.ProcessInterrupt`（`internal/swarm/server/runtime/interface.go:437-485`）→ `coordinator.CancelExecution(sessionID, opts)`（`internal/swarm/runtime/session/coordinator.go:915`）→ `registry.Select(...ActiveOnly)` 挑出要取消的执行 → 逐个发 ctx 取消。运行中的工具 / 模型调用正是靠这次 ctx 取消停下来的。

**缺陷本身**：`CancelExecution` 的筛选条件是 `RequestID: &sessionID`：

```go
// internal/swarm/server/runtime/interface.go:477-483
_, _ = uc.coordinator.CancelExecution(sessionID, swsession.CancelExecutionOptions{
    RequestID:   &sessionID,     // ← 会话ID 被当成请求ID
    WaitTimeout: &waitTimeout,
})
```

而执行登记时写入的 `request_id` 是真实请求号：`RunUnary(ctx, sessionID, request.RequestID, ...)`（`interface.go:200`）→ `newExecution(record, requestID, ...)`（`coordinator.go:452-485`）→ `NewSessionExecutionHandle(newExecutionID(), record.sessionID, requestID, ...)`。`sessionID`（形如 `sess_xxx`）与 `requestID` 是两个完全不同的值，`Select` 里 `RequestID` 一旦非空就做等值过滤：

```go
// coordinator.go:230-267 的 Select 语义（registry.Select）
if opts.RequestID != nil && h.RequestID != *opts.RequestID { continue }
```

结果：匹配集为 0，`len(handles) == 0` 直接 `return CancelExecutionResult{}, nil`（`coordinator.go:926-928`）。**取消请求返回"成功"，但一个执行都没被取消。**

上游同位置的写法是：

```python
# jiuwenswarm/runtime/service.py:1465-1471
cancellation = await self._session_coordinator.cancel_execution(
    request.session_id,
    request_id=target_request_id or None,   # ← 只传 target_request_id，空则 None（=不过滤）
    generation=snapshot.generation if snapshot is not None else None,
    excluded_execution_ids=excluded_execution_ids,
)
```

`target_request_id` 从 `params.get("target_request_id")` 取，**默认是空串 → 转成 `None` → 不按 request_id 过滤**。Go 侧把"没有 target_request_id"这个最常见的场景，用会话ID填成了过滤条件。

**同一处还有第二个缺口**：上游传了 `generation=snapshot.generation`，Go 侧 `CancelExecutionOptions.Generation` 是 `nil`。会话被重新注册过时（`RegisterSession` 每次 `RecordSession` 都会把 `generation+1`，见 `coordinator.go:243-249`），不按代次过滤会把旧代次残留执行一起算进匹配集。

**影响面**：这是最核心的用户动作。用户按"停止"之后，正在跑的工具（可能是 `bash` 里一个 `find /`）不会收到 ctx 取消，会一直跑；`supplement`（补充信息）路径同样 `RequestID: &sessionID`（`interface.go:469-472`），也取消不到任何东西。为避免误判，需要说明：这条链路在本仓目前已经**部分断头**——`ProcessMessageStream`（`interface.go:243-430`）根本没走 `coordinator.RunStream`，只调了 `RegisterSession`；但 `ProcessMessage`（非流式）确实走 `RunUnary` 登记了带真实 `requestID` 的执行，所以"取消匹配不到"这件事对非流式路径是**当前可复现**的，对流式路径则是等 10.3.28e 接线后立刻暴露。

**建议**：
1. 从请求参数里取 `target_request_id`（上游 `params.get("target_request_id")`），空串时传 `nil`，**绝不要**用 `sessionID` 兜底；
2. 补 `Generation`（来自 `snapshot_session(sessionID).generation`）；
3. 补 `ExcludedExecutionIDs`（team pause 时排除 `observation_only` 执行，上游 `service.py:1455-1463`）；
4. 加一条回归用例：登记 `RunUnary(reqID="r1")` → `CancelExecution(sessionID)`（不带 target）→ 断言执行确实被 cancel 且状态为 `cancelled`。

---

### S-02 `MarkTerminalIfActive` 先返回后校验，取消计数恒为 0

**这是哪条流程**：同 S-01 的取消链路尾段——筛选出执行后，对每个执行置终态并统计"真正取消掉几个"，结果回给上层用于 `chat.interrupt_result` 的 `success` 字段与"是否还有任务可取消"的提示。

**缺陷本身**：

```go
// internal/swarm/runtime/session/coordinator.go:989-995
for _, h := range handles {
    if _, isTimedOut := timedOutSet[h.ExecutionID]; isTimedOut {
        continue
    }
    c.registry.MarkTerminalIfActive(h.ExecutionID, SessionExecutionStateCancelled)  // ← 返回值被丢弃
    if c.registry.StateOf(h.ExecutionID) == SessionExecutionStateCancelled {        // ← 再次查询
        cancelled++
    }
}
```

`MarkTerminalIfActive` 的语义是"**非终态**时才置终态"（`execution_registry.go:195-206`：`if !ok || h.State.Terminal() { return false }`）。若某个执行在取消到达前已经自己跑完了（`succeeded` / `failed`），这里不覆盖状态——这本身是对的。但 `if c.registry.StateOf(...) == Cancelled` 只在"刚被我们置成 `cancelled`"时成立，而已是终态的执行不会被改，**于是 `cancelled` 只统计到"本次真的改成 cancelled 的"**——问题是，`MarkTerminalIfActive` 已经返回了"是否真的置了"，返回值被丢掉后再查一次状态，等于用一次额外的加锁查询复现同一个信息，且读的是**可能已被其他协程改过**的值（`StateOf` 与 `MarkTerminalIfActive` 之间没有原子性）。

更直接的问题是：**`MarkTerminalIfActive` 返回 false 的分支里 `StateOf` 可能读到 `cancelled`**（别的路径刚好把它置成 cancelled），于是计入 `cancelled++`；反之返回 true 之后被别的路径覆盖成别的终态（`markTerminalLocked` 遇终态会早返回，不会覆盖，所以这条不成立）。也就是说当前实现"碰巧"大体正确，但**逻辑上把可用的返回值丢了，反而用两次非原子查询去重建**，属于典型的"抄了动作、丢了语义"。

`CloseSession`（`coordinator.go:1060-1068`）里同样先用 `MarkTerminalIfActive`、后面没有再校验，写法不一致，进一步说明这里不是有意为之。

**影响面**：`CancelExecutionResult.Cancelled` 是上层判断"有没有真的取消掉东西"的唯一依据；`ProcessInterrupt` 把它用于提示文案（"当前没有可取消的团队任务"）。计数不准会让用户看到"没取消到"而实际取消到了，或者反之。

**建议**：直接用返回值计数，并在同一次 registry 加锁内完成"检查终态 → 置终态 → 计数"，避免两次非原子查询：

```go
if c.registry.MarkTerminalIfActive(h.ExecutionID, SessionExecutionStateCancelled) {
    cancelled++
} else if c.registry.StateOf(h.ExecutionID) == SessionExecutionStateCancelled {
    cancelled++
}
```

（或者更干净：给 registry 加一个 `MarkTerminalIfActiveCounting` 在锁内返回最终状态。）

---

### S-03 团队轮次结束从不调用 harness.Stop，成员会话状态可能整轮丢失

**这是哪条流程**：团队成员跑完一轮 → `TeamAgent` 的 `defer a.coordination.FinalizeRound(ctx)`（`internal/agent_teams/agent/team_agent.go:553`、`:1303`）→ `CoordinationKernel.FinalizeRound`（`internal/agent_teams/agent/coordination/kernel.go:523`）→ 轮末收尾（停流控制器、停成员脑、持久化）。

**缺陷本身**：Python 的 `finalize_round` 明确有三步（`openjiuwen/agent_teams/agent/coordination/kernel.py:812-826`）：

```python
async def finalize_round(self) -> None:
    host = self._host
    await host.stream_controller.stop()                      # ① 停流控制器
    if host.resources.harness is not None:
        await host.resources.harness.stop()                  # ② 停成员脑（native supervisor）
    host.stream_controller.stream_queue = None               # ③ 释放队列
```

而 `TeamHarness.stop()` 里有一句关键的持久化（`openjiuwen/agent_teams/harness/team_harness.py:187-194`）：

```python
try:
    if self._active_agent_session is not None:
        await self._active_agent_session.commit()    # ← 提交子会话
except Exception:
    logger.debug("[TeamHarness] commit raised during teardown, ignoring", exc_info=True)
```

Go 侧 `CoordinationKernel.FinalizeRound` 只做了两件事：

```go
// kernel.go:523-540
func (k *CoordinationKernel) FinalizeRound(ctx context.Context) {
    if memMgr := k.host.MemoryManager(); memMgr != nil {
        _ = memMgr.ExtractAfterRound(ctx)     // ① 记忆提取
    }
    if sc := k.host.StreamController(); sc != nil {
        sc.ResetStreamQueue()                 // ③ 释放队列
    }
}
// ② 完全没有：既不停 StreamController（只重置了队列），也不调 harness.Stop
```

`CoordinationKernel.Stop`（`kernel.go:390-441`）的 10 步里同样没有 `harness.Stop`；`Pause`（`kernel.go:330-386`）的 11 步里也没有。全仓 `NativeHarness.Stop` 的调用点只有 `TeamHarness.Stop`（`team_harness.go:488`）与 `Dispose`，而 `TeamHarness.Stop` 在生产链路上**零调用**（grep `\.Stop(ctx)` 在 `internal/agent_teams/` 只命中 `native_harness.go:203`、`team_harness.go:488` 两处内部定义，以及 `team_agent.go:899` 的 `a.coordination.Stop`）。

**影响面**（按顺序）：
1. 成员脑的 supervisor / forwarder 两条协程从 `Start` 活到进程结束，每轮 `Start` 都 `ensureInitialized`；流不关（`sess.CloseStream()` 只在 `NativeHarness.Stop` 里），输出队列持续被泵；
2. `TeamHarness.activeAgentSession` 引用的子会话**从不 `commit()`**——轮末未落盘的部分（未提交的对话增量、停止条件状态等）在下一轮 `pre_run` 重建时可能读到旧 checkpoint；
3. 上一个 `NativeHarness` 绑定的 session 永不解绑（`ctrl.UnbindSession` 在 `Stop` 里），`TaskLoopController` 的 session 表随轮次增长；
4. 因为 `NativeHarness` 是"一个 run cycle 一个实例"的语义（Python `start()` 里 `if self._native is None or state is TERMINATED: 重建`），Go 现在**同一个实例被反复 Start**，`h.started` 一旦为真后续 `Start` 直接 `return nil`——也就是说成员脑的 second cycle 根本没重建，状态机停留在上一轮的相位。

**建议**：在 `FinalizeRound` 里补 `harness.Stop`（先停流控制器，再停成员脑，最后清 `stream_queue`，顺序照上游），并确认 `TeamHarness.Stop` 内部补上 `activeAgentSession.commit()`（当前 `team_harness.go:486-496` 只有 `native.Stop` + 清 `nativeSessionID` + `setActiveSession(nil)`，**没有 commit**）。这条需要和 25.1b/25.1d 的装配一起看，建议单独立一个修复项而不是塞进风格提交。

---

## 二、一般（M）

### M-01 `settleRoundDone` 丢了「只重试一次」护栏与 coordinator 重置（上轮 S-01/S-02 未修）

**这是哪条流程**：成员脑一轮结束后的决策——`runRound` 提交完拿结果 → `cmdRoundFinished` → supervisor 的 `settleRoundDone`（`internal/agent_teams/harness/native_harness.go:564-659`）决定下一轮跑什么。

**缺陷本身**：上一轮审查（`docs/review/2026-11-07-48h-logic-review.md` S-01/S-02）已经指出两点，本轮代码与当时一字未变：

```go
// native_harness.go:612-620
if runtimeCrashed {
    if replayable, ok := active.Query.(string); ok && replayable != "" {
        nxt := h.startRound(ctx, active.Query)     // 无条件重试，无 failure_retry 标记
        ...
        return
    }
    h.transition(ctx, HarnessStateIdle)
    return
}
```

上游对应段（`native_harness.py:1175-1197`）：

```python
if runtime_crashed:
    self._reset_coordinator()                        # ← Go 无
    ...
    if replayable and not active.failure_retry:      # ← Go 无 failure_retry 字段
        nxt = self._start_round(active.original_query, _StartMode.FAILURE_RETRY)
```

`ActiveRound`（`state.go:17-29`）仍然没有 `failure_retry` 字段，`settleRoundDone` 里也没有 `h.coordinator.Reset()`。结果是：确定性崩溃（例如 scheduler 已 Stop、session 已解绑但仍被驱动）会让 `runtimeCrashed` 恒真 → 每轮再 `startRound` → **无限循环**，每圈 `IncrementIteration()` 递增；且上一轮 cancel 遗留的 `IsAborted()` 会把重试成功后本该继续的 follow-up / 剩余任务误判为"用户中止"。

**影响面**：团队流程在确定性故障下卡死（不产出、不报错、不回 IDLE）。这是本轮**唯一没能关闭的上一轮严重项**，而且本轮在同一个函数里继续加了新逻辑（`WriteRoundResultToStream`），风险叠加。

**建议**：补 `failure_retry` 字段 + `Reset()`，并按上游 decision priority 重排（见 M-02）。

### M-02 `settleRoundDone` 丢了两处决策分支与 pending 批次语义

**这是哪条流程**：同 M-01。

**缺陷本身**：三处与上游不一致：

| 上游（`native_harness.py`） | Go（`native_harness.go`） | 差异性质 |
|---|---|---|
| L1110 `was_graceful = active.graceful_abort`；L1160-1163 优雅中止时 `_drain_follow_ups_discard` 后回 IDLE | 无 `graceful_abort` 字段，无该分支 | 缺失（但 `Abort(immediate=false)` 已设 `RequestAbort()`，该分支在 25.1c 前不可达 → 属"已在 25.1c 登记"范围内，**未登记**） |
| L1232 `if self._has_remaining_tasks(session) and active.original_query:` | `if h.hasRemainingTasks() { if q, ok := active.Query.(string); ok && q != "" {` | 结构等价，但读 `active.Query` 而非 `original_query`（`ActiveRound` 无 `original_query`），字段语义被合并——后续加 turn 解析（25.1e）时无从区分 |
| L1206-1213 `self._drain_pending_messages()` 取**同 invocation context 的一批**（返回 `str \| list[str]`） | `st.dequeueInbox()` + `st.popInboxContent()` 只取**一条** | 语义缩水：一次积压 3 条消息时，上游一轮吸收同上下文的一批，Go 拆成 3 轮 |

第三处是规则 9.3 的"步骤合并"：`_drain_pending_messages` 是一个独立方法（上游 L1239-1259），Go 用两个 registry 辅助函数拼了"取一条"的简化版，等于**把"按 invocation context 分批"这一步整体删掉了**。上游注释写得很明确（"Drain the next FIFO batch without crossing invocation contexts"），靠 `run_context` 相同才合批——Go 的 `InboxMessage` 连 `run_context` 字段都没有（`state.go:32-41` 只有 `Seq/Content/Immediate`）。

**影响面**：成员在忙碌期积压多条消息时，每轮只消化一条，且不同调用上下文的消息可能被合批（一旦补上 `content` 为非字符串的分支）；未来 25.1e 加 turn 身份时会发现字段不够用，要么再改一次结构、要么写成又一处"字段语义被合并"。

**建议**：`InboxMessage` 补 `RunContext`；`drainPendingMessages` 按上游实现（同上下文且同为字符串才合批，遇到非字符串停下）；同步把 `graceful_abort` / `original_query` 补进 `ActiveRound` 并在 25.1c 偏差表登记当前不可达。

### M-03 崩溃重试后 supervisor 可能整体静默退出，无人接手队列

**这是哪条流程**：`settleRoundDone` 崩溃重试 → `runRound` 收尾投递 `cmdRoundFinished` → supervisor 若已退出，所有后续命令走 `failRemainingCommands`。

**缺陷本身**：`runRound` 的投递是"ctx 已取消就放弃"：

```go
// native_harness.go:532-537
reply := make(chan error, 1)
select {
case h.cmd <- &cmdRoundFinished{...}:
    _ = waitAck(context.WithoutCancel(ctx), reply)
case <-ctx.Done():
}
```

`ctx` 是 `startRound` 里派生的 `roundCtx`（L494-499），`abortActiveRound` 会 `active.Cancel()`。问题在于 `settleRoundDone` 里 `h.st.SetActive(nil)`（L579）之后才判断 `runtimeCrashed`（L583/612）——如果这轮是被 abort 取消的，`active` 已置 nil，再 `startRound` 起的新轮用的是 `ctx`（supervisor 的 ctx，不是 roundCtx），这个还好；但如果 supervisor 的父 ctx 已被 `Stop` 取消，`h.cmd` 的发送分支读到 `<-ctx.Done()` 就**静默丢弃**这条 `cmdRoundFinished`，接着 `defer close(active.Done)` 关掉 Done。

更关键的是 `supervisorLoop`（L334-361）：

```go
for {
    if ctx.Err() != nil { return }
    select {
    case <-ctx.Done(): return
    case c := <-h.cmd:
        if stop := h.dispatch(ctx, c); stop { return }
    }
}
```

`dispatch` 里 `*cmdRoundFinished` 分支调用 `settleRoundDone` 时传入的是 **supervisor 的 ctx**，而 `settleRoundDone` → `startRound(ctx, ...)` 用它派生 roundCtx。也就是说崩溃重试的重试轮拿的是 supervisor ctx，一旦 supervisor 因任何原因退出（父 ctx 取消 / `cmdStop`），重试轮仍在跑，跑完投 `cmdRoundFinished` 时 `h.cmd` 没有消费者，只能落到 `failRemainingCommands`（在 `supervisorDone` 之后才 defer 执行）——而该 defer 里的 `ack(c.ack(), ...)` 会让 `runRound` 的 `waitAck` 拿到"supervisor 已退出"错误，但 `runRound` 已经 `_ = waitAck(...)` 忽略掉了。

**影响面**：崩溃重试路径下若 supervisor 退出，会出现"轮次在跑、无人结算、输出无人消费"的悬空态；同时 `Stop` 里的 `abortActiveRound` 只等 5 秒（见 T-02），过了就不再管。

**建议**：`settleRoundDone` 不再用 supervisor ctx 派生新一轮，改用独立可取消 ctx 并在 `Stop` 里统一取消；`runRound` 的 `cmdRoundFinished` 投递失败时记录 Warn（当前完全静默），至少让"结算丢失"可观测。

### M-04 后台进程 Wait 未被调用，早期退出的进程留下僵尸

**这是哪条流程**：`bash` / `powershell` 工具的后台执行 → `AsyncProcessHandler.Background(grace)`（`internal/agentcore/sys_operation/local/utils.go:472-556`）。

**缺陷本身**：`Background` 只在 `grace > 0` 时启动 `go func() { waitCh <- h.cmd.Wait() }()`（L510-511）。`grace == 0`（或在 grace 内没退出的路径）时**从不调用 `cmd.Wait()`**；`KillProcessTree`（L559-563）→ `killProcessTreePlatform`（`proc_unix.go:28-37`）只发 `SIGTERM` / `SIGKILL`，**也不 Wait**。

Go 的 `os/exec` 文档明确：不调用 `Wait` 的 `Cmd` 不会释放内部管道与 goroutine，进程退出后只能靠 GC 回收 `ProcessState`；在 Linux 上子进程退出会变僵尸直到父进程 `wait`（如果实现走的是 `os.Process.Wait` 而非 `cmd.Wait`，僵尸会更早被回收，但 `cmd.Wait` 才是关闭 `stdout`/`stderr` 管道的那一环）。

这与 D-12 的"stderr 管道必须读满"形成隐患：`drainStderr`（L613-645）在读到 EOF 前一直阻塞；`Wait` 才关闭管道。当前只是因为"子进程自己退出 → 管道写端关闭 → EOF"，才碰巧不挂；如果 `grace == 0` 且进程长期运行、由 `KillProcessTree` 收尾，管道写端由 kill 关闭、读端仍能读到 EOF，勉强过关，但**`cmd.Wait()` 的缺失意味着 `Cmd` 的 `closers` 从未被调用**——`StderrPipe` 创建的 `os.Pipe` 的文件描述符会泄漏。

**影响面**：长时间运行的会话里反复后台执行命令，慢速累积 fd 泄漏；`grace` 判定的"进程是否在 grace 内退出"与 `Wait` 的返回值存在轻微竞态（`grace == 0` 那条路径根本没判定）。

**建议**：无论 `grace` 是否为 0，都在启动成功后起一个统一收尾协程 `go func() { h.waitErr = h.cmd.Wait(); close(h.done) }()`，`Background` 只在 grace 窗口内选择"等 done"或"超时返回"；`KillProcessTree` 之后等 `done`（带超时）。

### M-05 shell 输出截断按字节切，切在 UTF-8 中间会产生乱码

**这是哪条流程**：`bash` 工具输出 → `truncateOutputWithRatio`（`internal/agentcore/harness/tools/shell/output.go:156-180`）。

**缺陷本身**：

```go
if maxChars == 0 || len(text) <= maxChars { return text }   // len 是字节
head := text[:headBudget]                                    // 字节切
tail := text[len(text)-tailBudget:]                          // 字节切
omitted := text[headBudget : len(text)-tailBudget]
```

上游是 Python `text[:head_budget]`，**按字符**切（`bash/_output.py:34-39`）。Go 侧 `len()` 与切片都是字节——一个中文 `max_output_chars=20000` 的截断会落在多字节字符中间，输出里出现 `\xef\xbf\xbd` 一类的替换字节；更隐蔽的是 `omittedLines := strings.Count(omitted, "\n")` 因为 omitted 边界被切坏，行数统计可能少算。

本仓已有成熟写法：`truncateString`（`rails/stream_event_helpers.go:229-236`）与 `truncateString` / `truncateStringTail`（`dreaming/sweeper.go:702-720`）都用 `[]rune` 且注释里明确写"对齐 Python 字符切片语义，避免 CJK 截断产生无效 UTF-8"。同一个仓里三套截断实现，只有 shell 这一套是字节的。

**影响面**：`max_output_chars` 越小越容易踩（默认 20000，但调用方可以显式传小值，D-01 刚把 `*int` 语义改好、允许显式设置）；中文/日文/emoji 输出被截时出现乱码，模型看到的文本里混进替换字节。

**建议**：改为 `[]rune` 切片（`runes := []rune(text)`，再用 `len(runes)` 判长、按 rune 切），与同仓另两处保持一致；或抽一个公共 `TruncateByRunes` 到 `common/utils` 供三处共用。

### M-06 `WriteRoundResultToStream` 的读-改-写没有锁，行尾追加记录可能被覆盖

**这是哪条流程**：成员跑完一轮 → `runRound` 把 answer 写成 chunk 写流（`native_harness.go:527-529`）→ `DeepAgent.WriteRoundResultToStream`。

**缺陷本身**：这个函数（被 `native_harness.go:528` 调用）实现对 `session` 的 history 是"读全量 → append 一条 → 整体写回"的形态；而同一个 session 的 history 在另一侧是**异步队列追加**（`AppendHistoryRecord` 把记录投给 `historyWriteQueue`，由 worker 顺序落盘，`internal/swarm/server/session/session_history.go:101-150`）。两条写路径没有任何互斥：`WriteRoundResultToStream` 读到 N 条、正在拼第 N+1 条时，worker 写入第 N+1 条；随后 `WriteRoundResultToStream` 整体写回它的 N+1 条 → **worker 写入的那条被覆盖**。

这条需要标注置信度：`WriteRoundResultToStream` 位于 `internal/agentcore/harness/deep_agent.go`，本轮提交 `af7e44adb` 首次在团队链路接线（"runRound 漏写 answer，task_completion 被丢"）。写回策略需要再核一次它的落盘实现（本轮时间有限未逐行验到写文件那一层），但**两个无协调的写者对同一份 history 做"整体覆盖"是确定的**，无论哪一侧先读，都存在丢记录窗口。

**影响面**：团队成员轮次的 answer 与 chat 历史记录互相覆盖，表现为"历史里少一条"或"answer 重复"。用户可见症状是刷新后对话内容缺一段。

**建议**：history 落盘统一走 `AppendHistoryRecord` 队列（单一写者），不要另开一条"整份重写"路径；若确实需要整份重写，必须与 worker 共用同一把锁并把"读-改-写"包成一次入队操作。

### M-07 TeamHarness 有 5 个空方法体，其中一个的注释与实现互相打脸

**这是哪条流程**：`TeamHarness` 的 MemberRuntime 面向协调层暴露的能力（团队状态持久化、任务完成轮询、邮箱唤醒）。

**缺陷本身**：

```go
// internal/agent_teams/harness/team_harness.go
// ⤵️ 待 9.56 Manager 章节回填：实现锁存标志 + 持久化 DB 状态
func (h *TeamHarness) PersistTeamDBState() {}          // L414
func (h *TeamHarness) MarkTeamCleaned() {}             // L418
func (h *TeamHarness) MarkTeamBuilt() {}               // L422
// ⤴️ 9.60 已实现 StreamController，此方法由 StreamController 通过 requestCompletionPollCb 回调
func (h *TeamHarness) RequestCompletionPoll() {}        // L426
func (h *TeamHarness) WakeMailboxIfInterruptCleared() {} // L430
```

后两个的注释说"已由 StreamController 通过回调实现"，但方法本身是空体——这类注释会让人以为"有实现、只是没用这个入口"，实际是**两处都空**（StreamController 侧是否真的调了回调未在注释里给出锚点）。

**影响面**：`PersistTeamDBState` / `MarkTeamCleaned` / `MarkTeamBuilt` 对应 Python 的团队 DB 状态锁存（`team_harness.py` 里有真实读写）；团队重启后 "team built / cleaned" 标志读不到时，行为取决于调用方兜底，容易被误判为"团队没建过"。

**建议**：确认 9.56 是否已完成——若已完成（本仓领域九/二十五里 `TeamManager` 相关多处标 ✅），这三个空方法属于"依赖已完成但没回填"，应当补齐；若确实未完成，把注释里"⤵️ 待 9.56"与"⤴️ 9.60 已实现"统一成一种口径，并给出回调锚点（文件:行号）。

### M-08 `session_lifecycle` 的参与者登记表零注册，前后台切换派发永远走"只记 target"分支

**这是哪条流程**：`session.switch` / `session.create` 提交阶段 → `dispatchForegroundTransition`（`internal/swarm/runtime/session_provisioner.go:1197-1233`）→ 遍历活动参与者通知"前台可见性变更"。

**缺陷本身**：

```go
func (p *RuntimeSessionProvisioner) dispatchForegroundTransition(...) {
    participants := p.participants.SnapshotActivity()
    if len(participants) == 0 {
        p.participants.RememberTarget(target)
        return                     // ← 生产环境永远走这里
    }
    ...
}
```

`ReplaceSessionParticipants` 在整个 `internal/` 里**只有测试文件调用**（`grep` 命中 `session_provisioner_test.go`、`session_lifecycle_test.go`、`team_session_lifecycle_test.go`），生产装配处（`internal/swarm/server/session_stubs.go:75-82`）只 `NewRuntimeParticipantRegistry()`，从不注册任何参与者。

**影响面**：上游 `_dispatch_foreground_transition` 的用途是让 plan 控制器、team manager、KVC 等参与者收到"前台会话切换"事件；Go 侧现在只是把 target 记进表里。这条是否属于"25.2 前预期空转"需要在 spec §10 登记——当前 §10 的 R-1~R-7 里没有这一项。

**建议**：要么按上游把至少一个真实参与者接上（plan 控制器是最小可用集合），要么在 spec §10 增加一条"R-8 前台切换参与者零注册（回填目标：25.2 / plan 控制器接线）"，避免"方法存在、生产无效"的无声状态。

### M-09 `SessionRuntimeLifecyclePort` 已定义但零实现，`ResumeWaitingForInput` 等能力缺失

**这是哪条流程**：团队轮的持久 owner 在轮间被置为 `waiting_for_input`，下一轮由 `resume_waiting_for_input` 恢复为 `running`——这是团队"跨轮存活"的核心状态迁移。

**缺陷本身**：`SessionRuntimeLifecyclePort`（`internal/swarm/runtime/session/session_lifecycle.go:54-70`，5 方法）与 `RoundStateCoordinator`（`internal/swarm/runtime/team_session_lifecycle.go:32-38`，2 方法）都**只有接口定义，没有任何实现类型**：

```
grep "ResumeWaitingForInput\|MarkSessionWaitingForInput" → 仅 team_session_lifecycle.go 的接口声明与 3 处调用点
grep "SessionRuntimeLifecyclePort" → 仅接口定义
```

上游对应实现是 `RuntimeSessionCoordinator.resume_waiting_for_input`（`coordinator.py:982-993`）与 `mark_session_waiting_for_input`（`:961-980`），都在协调器上。Go 的协调器（`coordinator.go`）里**没有这两个方法**（导出方法清单 21 个，无此项）。

**影响面**：`TeamSessionLifecycleAdapter`（`team_session_lifecycle.go:117/148/151`）的三处调用指向 nil 接口——只要适配器被实例化并挂到协调器上（当前生产零装配），就会 nil panic；即使补了装配，持久 owner 的状态迁移也无人执行，团队轮次在 `waiting_for_input` 与 `running` 之间不会切换，`IsGenerationRunning` 恒为调用方兜底值。

**建议**：按规则 13.2 这属于"缺失项"，需要明确给出三档建议。我的判断是**本批补上**：两个方法各约 20 行（照抄上游），且 `SessionExecutionState` 的迁移钩子已经在 registry 里（`MarkWaiting` / `ResumeWaiting` 都有了，只差"整代次批量迁移"的外壳）。

### M-10 `team_session_lifecycle` 的桩注释与实际回填来源错位

**这是哪条流程**：团队会话删除/暂停时对 TeamManager 的调用（`SetRoundStateReporter` / `RemoveWaiter` / `BroadcastEvent` / `IsGenerationRunning`）。

**缺陷本身**：`team_session_lifecycle.go:18-23` 注释写"其余 4 个为桩，spec §10 R-7"，而 spec `2026-11-07-10.3.28-…-design.md` §10 的 R-7 条目写的是"回填目标 25.2"。同时 `IMPLEMENTATION_PLAN` 的 10.3.28c 行写"4 桩登记 R-7"。

但同一份 spec 的 R-3 写"`cleanup_session` / `AgentManager.CleanupSessionRuntime`…… 回填目标 10.3.29"，而 `session_stubs.go:39-49` 的三个 noop 桩（`noopTrajectoryHook` / `noopTeamBindingUnbinder` / `noopLifecycleJournal`）的注释分别指向"领域二十二 trajectory 落地后回填"与"spec §10 R-1"，**R-1 在 spec 里的内容是 `prompt_suggestion.forks.session_deleted`**，与团队绑定解绑毫无关系。

**影响面**：这类"桩注释指向另一个不相干的回填项"会让后续回填时找不到真正的目标章节。本轮审查里我按注释去查"R-1 是否已满足"，查到的却是 prompt_suggestion，浪费了一轮定位。

**建议**：三个 noop 桩的注释各改一行，指向真实回填章节（trajectory → 领域二十二；team binding → 需要新增一条 R 编号；lifecycle journal → 10.3.29）；spec §10 补齐这三条的真实编号。

---

## 三、提示（T）

### T-01 把一整条上游逻辑写成占位再 `_ =` 掉

```go
// internal/agent_teams/harness/native_harness.go:222-226
// 步骤 6：工具拆除。
// Python: ability_manager.teardown_tools()（native_harness.py:457）。
// Go 的 AbilityManagerInterface 无 teardown_tools 等价物，属既有 parity 缺口
_ = h.agent.AbilityManager() // 占位保留接入点，避免误删步骤结构
```

规则 8.2 明确禁止用 `_ = oldFn` 消警。这里调用一个纯访问器只为"占住步骤序号"，不产生任何行为。建议：要么在 `AbilityManagerInterface` 补 `TeardownTools()` 真实现（上游 6 行），要么把该步骤从 `Stop` 里删掉并在偏差表登记，不要留一行假调用。

### T-02 `abortActiveRound` 用 `time.After` 且超时后仍继续

```go
// native_harness.go:683-689
select {
case <-active.Done:
case <-time.After(5 * time.Second):
}
```

两点：① `time.After` 在每次调用都会新建 Timer，快速连续 abort 时不会立即释放（Go 1.23 前定时器不回收；本仓 go.mod 版本需确认）；② 超时后**没有任何日志或标记**，`Stop` 继续往下走 `UnbindSession`，正是 L673-674 注释里说的 race 窗口——注释承认"必须等"，实现却是"最多等 5 秒然后不等了"。建议超时打 Warn 并带上 `round_id`，让这类 race 可观测。

### T-03 `isControlResult` 只判 error，cooperative-stop 形状未识别

`native_harness.go:697-707` 的注释已自认"25.1a 阶段无 pause/graceful-abort，cooperative-stop 分支不可达"。但代码里既没有该分支的占位判断，也没有在偏差表登记"上游 `_is_control_result` 识别两种形状"这一差异。等 25.1c 落地时，`{"stop_reason": "cooperative"}` 形态的结果会被当作真实答案写进流。建议现在就把判定补全（多判一个 key），成本 3 行。

### T-04 `waitForHandles` / `requestDirectCancel` 对瞬时结束的执行会误报超时

```go
// coordinator.go:1187-1194、1200-1213
deadline := time.After(timeout)
for _, h := range handles {
    if isDoneClosed(h.Done) { continue }
    select {
    case <-h.Done:
    case <-deadline:
        timedOut = append(timedOut, h.ExecutionID)
    }
}
```

`deadline` 是共享的：第 1 个 handle 等满 `timeout` 后，后续 handle **一次 select 就立刻命中** `<-deadline`，统统被记为超时。上游 `asyncio.wait(targets, timeout=…)` 是"全部并发等同一个窗口"，行为等价；Go 这里是串行等，语义**退化成"总窗口 timeout"**。若目标确实是总窗口，注释应写明；若是想每个都有 timeout 窗口，需要每个 handle 单独计时。两种都不是当前代码字面读出来的语义，建议明确并加用例锁住。

### T-05 `GetCurrentGoal` 内部 `GoalManager.Get()` 失败时无兜底

```go
// internal/agentcore/harness/tools/goal/goal.go:112-120
record := goalManager.Get()
if record == nil { ... return has_goal=false ... }
```

上游（`harness/tools/goal.py:170-181`）包了 `try/except`：

```python
try:
    record = await self._goal_manager.get()
except Exception:
    logger.debug("[GetCurrentGoal] Failed to load goal record", exc_info=True)
    record = None
```

Go 侧 `Get()` 无 error 返回，但内部 `m.store.Load()` 若走 DB 可能在异常状态下返回 nil 而非报错——两种"取不到"被合并成同一条 `has_goal=false` 路径，模型会被告知"没有持续目标"，而真实原因是读失败。建议给 `Get()` 加 error（或提供 `GetWithError`），工具里打 Debug 日志后再降级。

### T-06 `_ = ` 吞错已是这批的惯性写法

统计本轮新增/改动的生产代码里 `_, _ =` 与 `_ = ` 忽略 error 的位置，`interface.go` 单文件就有 8 处（L198、L203、L469、L476、L480、L710、L732…）。其中"取消失败被忽略"直接导致 S-01/S-02 无法从日志发现。不要求逐条改，但**取消 / 关闭 / 持久化**这三类调用的 error 至少应 Warf 一条，否则线上排障完全没有线索。

### T-07 CI 覆盖面在收紧而非扩大

`8a8f7aaf3 ci: 精简覆盖率门禁，只保留单测整体覆盖率（阈值抬至 80%）` 把既有的"改动行覆盖率 85%"门禁删掉了。结合本轮的三个严重问题（都是"新代码执行到了、但没被真实断言覆盖"），这条门禁恰好是能拦住它们的那一类。阈值从 85% 提到 80% 的"单测整体覆盖率"是**不同口径**（整体覆盖率对新增文件不敏感）。建议保留改动行覆盖率（哪怕是 70%），或至少在 PR 描述里说明为什么可以放弃它。

### T-08 25.0b 渲染层尚未接线，计划行已标 ✅

`IMPLEMENTATION_PLAN` 的 25.0b 行标 ✅，范围是"四文件纯渲染层"。但 `inbound_render.go` / `messages.go` / `timefmt.go` / `external/format.go` 四个文件的导出函数在 `internal/` **生产代码里零调用**（`grep RenderInbound\|RenderMessage\|DiffRoster` 只命中测试与 doc.go）。计划文档里确实写了"供 25.0c 的 tracker 消费"，属于预期；但 25.0c 行是 ☐，也就是这批交付的是"一段时间内无人调用的新代码"。建议在 25.0b 行补一句"当前零生产调用，首个消费者为 25.0c"，与 9.87 当初的处理口径一致。

---

## 四、按提交类型回看提交质量

| 类型 | 数量 | 观察 |
|---|---|---|
| feat | 104 | 10.3.28（a/b/c/d）与 25.0b 的提交切分粒度好（每个提交对应计划里一步），25.0a 的三个讨论点①②③也各自独立可回滚 |
| docs | 47 | spec/plan 普遍详细，但**行号引用不可靠**（见下） |
| fix | 38 | D 组 16 项逐个对得上上游提交；`bd1a62b37`（自我纠正步骤顺序）、`6839133c5`（真实 worktree bug）质量突出 |
| test | 31 | 集成用例普遍是"真跨子系统"，符合规则 7；`7ffb0e073` 修时区依赖的方式（注入 Location 而非改生产默认）正确 |
| style | 17 | 纯 gofmt 与注释整理，无夹带；`ab4014a50` 一次动了大量文件但都是空行/注释，review 成本低 |
| refactor | 16 | `da9ef2359`（删 `session_manager.go` 430 行 + 清 `SessionManager` 符号）删除干净，`grep SessionManager` 仅剩测试注释里提到"旧 SessionManager" |
| ci | 1 | 见 T-07 |

**一个反复出现的小问题**：spec/plan 里的 `文件:行号` 引用大量失准。例：
- `call_timeout.go:151-168` 说"Python: BashTool.get_call_timeout"，实际 Python 在 `bash/_tool.py:151-169`（这个不算错）；
- `psCallTimeout` 注释写 `Python: PowerShellTool.get_call_timeout (powershell/_tool.py:130-147)`，但上游 `powershell/_tool.py` **根本没有 `get_call_timeout` 方法**（全仓 grep 只有 bash 有）；Go 侧的实现是"照 bash 的语义推的"。这不是行为错误（PowerShell 工具此前没有外层预算，加一个是合理增强），但注释声称"对齐"会误导后续维护者。

---

## 五、给下一批实施的三条建议（不是缺陷，是经验）

1. **接线类提交必须带一条端到端断言**。本轮的 S-01/S-03 都属于"方法写了、调用点有、两边对不上"。只要有一条"调用 cancel → 断言执行真的 cancelled"的用例，S-01 当天就会红。
2. **`_ =` 调用前先问一句"这个 error 在真实故障时会不会是我唯一的线索"**。取消、关闭、持久化三类都属于"是"。
3. **删门禁要有对冲**。改动行覆盖率门禁被精简掉之后，本轮的三个严重问题没有任何一道静态或动态关卡能拦住；如果要简化 CI，建议先确认保留的那道门禁对"新增文件只被读不被断言"这种情况是否敏感（当前整体覆盖率不敏感）。

---

## 附：本次审查的方法与局限

- 逐批阅读了 10.3.28（a/b/c/d）、25.0a、25.0b、9.86/9.87/9.88/9.89、D 组 D-01~D-16、C 组、B 组的核心生产代码，并对每处关键逻辑找了上游 Python 同位置源码核对。
- `go build ./...`、`go vet`（受影响包）本地通过；`go test -race` 跑了 `swarm/runtime/session` 与 `agent_teams/harness` 两个关键包，均绿（用于确认基线，不作结论）。
- 未跑全量集成测试（体量大），因此 M-06（history 竞写）的落盘层未逐行验证，文中已标注置信度；其余结论均为读码直接得出。
- 未修改任何代码、未提交、未推送。
