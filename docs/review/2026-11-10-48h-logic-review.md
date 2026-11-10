# 2026-11-10 48 小时上库质量审查

> 审查范围：`7ffb0e073 .. HEAD(51663d344)`（2026-11-08 02:00 ~ 2026-11-10 03:33，172 个非合并提交；上一轮审查截止到 `7ffb0e073`，本轮即这 172 个提交）
> 代码规模：非测试 Go 文件 1321 个（+28,378 / −9,368）；全部 `*.go` 1576 个（+61,876 / −10,918，含测试与集成用例）；其余为 spec/plan 与豁免登记
> 审查对象：提交原因与实现正确性；对齐 Python（产品层 `jiuwenswarm` 0.2.5 / 库层 `openjiuwen`）；桩与回填完整性；层级与步骤归属；稳定性、性能、可维护性、并发安全
> 审查方式：按 25.1b / 25.1c / 25.0g / 10.3.28c-e / memory E 组 / 9.90b 六块逐批读生产代码，并对照 `~/opensource/agent-core` 与 `~/opensource/jiuwenswarm-develop` 同位置源码。下文 `文件:行号` 以 HEAD `51663d344` 为准
> **只读审查，未修改任何代码**

---

## 零、结论摘要

| 级别 | 数量 | 一句话 |
|---|---|---|
| 严重 | 4 | 团队"取消"和"删除会话"两条主链路都停在空桩上；沙箱限制开关的映射写反了；并发第二次暂停会永久挂住调用方 |
| 一般 | 11 | 网关暂停把串行消息循环堵住、中断检查点漏写、会话关闭的超时与竞态、暂停后成员执行状态不复位、停止时不销毁原生运行时、子会话不提交、补充输入校验没接上网关、spec 路径丢配置字段 |
| 提示 | 8 | 死字段、过期注释、未迁移的小入口、日志缺失等 |

**先说这批做得好的地方**（避免只看问题）：

1. **`4f995a277` 是一个真实的文件损坏修复**。`UploadFileStream` / `DownloadFileStream` 用同一块 buffer 做 peek-ahead，`chunk := buf[:n]` 与 `buf` 共享底层数组，下一块读入后待写分块已被覆盖——落盘结果是"第二块重复 N 次"。修法（独立 `nextBuf` + 写后交换）正确，并且新增的 `fs_transfer_test.go` 是逐字节比对，旧实现必然失败。这类"先证明旧代码确实错、再改"的提交质量最高。
2. **25.1c 的快照轨与状态机结构忠实**。`snapshot_rail.go` 的 5 个钩子、`captureSnapshot` 的 `with_history=false` 口径、`ActiveRound` 的原子量字段、`settleRoundDone` 的 15 步优先级，与 `snapshot_rail.py` / `native_harness.py` 基本逐段对应；spec 里的 X-1~X-14 实施期修正也都有对应代码，不是"写完就算"。
3. **25.0c 的任务通知三兄弟正确应用了 Go 嵌入陷阱的教训**。`taskNoticeTracker` 的 `hookCandidates/hookEntry/hookRender/hookSuperseded` 用构造期注入函数指针，而不是指望子类覆盖方法；`claim` 里"锁外查 DB、锁内绑定+标记"的划分也写清了理由。这正是历史上踩过的坑。
4. **memory E 组是"根因级"修复**。E-02 没有停在"重建索引"，而是先修 `providerKey`（只拼 `id:model` 换端点/密钥检测不到），再补 `needsReindex` 比对、`runReindex` 的 `force`，最后处理 `sqlite_master` 里 `tokenize='trigram'` 的引号陷阱；E-03 把 mock 向量从默认回落里摘掉，避免"用噪声污染 0.7 权重的向量检索并把假向量持久化"。这类修复能解释"为什么以前检索永久为空"。
5. **`c0ce07b13` 的网关加锁是完整闭环**。`ChannelControlState` 加了 `Get/Set/Snapshot`，并且全目录 grep 后没有残留的裸 `.Mode` / `.SessionID` 访问；不是"加了锁但还有一半地方直接读字段"。
6. **`dd4d52483` 的 `AddAbility` 与上游一一对应**。`stateless → skip_if_exists`、`stateful → QualifyToolID 后 refresh`、`teardown_tools` 只摘本 owner 的限定 id，顺序和判定条件都与 `ability_manager.py:806-883` 对得上。

**这批最需要警惕的模式**：问题集中在"新入口建好了，但没有人调用"和"依赖已经完成，桩却没有回填"。

- `StreamSessionInput` / `ValidateSessionInput` / `ResolveSessionInputModeChecked` 全仓生产零调用（G-09）。
- `NativeHarness.Dispose` / `TeamHarness.Dispose` 全仓生产零调用（G-06）。
- `TeamManager.CancelSessionRuntime` 全仓生产零调用，而团队取消分支还在写 `cancelled := false`（S-01）。
- `session_stubs.go` 的团队 `quiesce_for_delete` / `dispose_after_resource_release` 仍是 Debug 日志 + `return nil`（S-02）。

这不是"算法写错"，而是"环节之间没接上"。只读自己这一层发现不了，必须顺着调用链走到执行体。

---

## 一、严重（S）

### S-01 团队会话"取消"是空桩：点停止不会停，还回"没有可取消的任务"

**这是哪条流程**：用户点"停止"→ 网关 `CancelAgentWorkForSession` → AgentServer `ProcessInterrupt`（`internal/swarm/server/runtime/interface.go:563`）→ `isTeamMode` → `processTeamInterrupt`（`:801`）→ `case "cancel"`（`:853`）。

**缺陷本身**：

```go
// internal/swarm/server/runtime/interface.go:853-877
case "cancel":
    // ⤵️ 10.6.19-23: team_manager.cancel_session_runtime(sessionID, reason)
    // S10: cancelled 硬编码为 false，Team cancel 功能完全无效，依赖 TeamManager
    cancelled := false
    // Python: cancelled = teamManager.CancelSessionRuntime(ctx, sessionID, reason)
    _, _ = uc.coordinator.CancelExecution(sessionID, swsession.CancelExecutionOptions{
        RequestID: &sessionID,
    })
    message := "团队当前执行已结束"
    if !cancelled {
        message = "当前没有可取消的团队任务"
    }
```

三个问题叠加：

1. `cancelled` 恒为 `false`，所以无论实际有没有任务，前端都收到"当前没有可取消的团队任务"。
2. 唯一"看起来在取消"的 `coordinator.CancelExecution` 仍然把 **sessionID 当 requestID** 传（上一轮 S-01 的同一个错），匹配不到任何执行记录。
3. Python 的两步都没做：`team_manager.cancel_session_runtime(session_id, workflow_disposition=...)` 和随后的 `session_manager.cancel_session_task(...)`；连 `client_disconnect → pause` / 用户终止 → `stop` 的处置分支也没有。

**依赖其实早就有了**：`TeamManager.CancelSessionRuntime`（`internal/swarm/agents/harness/team/team_manager_lifecycle.go:359`）在 2026-10-02 就已落地，内部会 `runner.StopAgentTeam` + `cleanupRuntimeLocals` + `ClearActive/PendingRuntime`。全仓生产调用方是 **0**，只在注释里被引用了一次（`:857`）。

**影响**：团队模式（leader/teammate）下用户主动终止 → runtime 继续跑、继续产出、继续占用模型与工具；`session.delete` 之前也可能先走这条路径。这是主链路上的"按钮没接线"。

**建议**：`case "cancel"` 直接调 `tm.CancelSessionRuntime`，按 metadata 的 `E2A_INTERNAL_CANCEL_SOURCE_KEY` 选 `pause`/`stop` 处置，再补 `sessionManager.CancelSessionTask`；同时把 `cancelTeamWorkForSession`（`:890-900`，仍是日志 + `return false`）改为调 `TerminateSessionRuntime`。

---

### S-02 删除团队会话时，团队的 quiesce / dispose 仍是空桩，依赖却已完成

**这是哪条流程**：`session.delete` → `RuntimeSessionProvisioner`（`internal/swarm/runtime/session_provisioner.go:2082-2110`）→ 团队分支 `QuiesceForDelete` → 释放资源 → `DisposeAfterResourceRelease` → `DeleteAgentTeam`。

**缺陷本身**：

```go
// internal/swarm/server/session_stubs.go:305-321
func (teamExecutionControllerAdapter) QuiesceForDelete(...) error {
    logger.Debug(...).Msg("team quiesce for delete not implemented (see 10.3.28 spec D-13)")
    return nil
}
func (teamExecutionControllerAdapter) DisposeAfterResourceRelease(...) error {
    logger.Debug(...).Msg("team dispose after release not implemented (see 10.3.28 spec D-13)")
    return nil
}
```

生产装配就是 `TeamExecutionController: teamExecutionControllerAdapter{}`（`session_stubs.go:110`），没有第二条实现。

**Python 做了什么**（`jiuwenswarm/agents/harness/team/team_manager.py:3711-3760`）：

- `quiesce_for_delete`：`_dispatch_swarmflow_controller(stop)` → `_cleanup_runtime_locals`，并加 `_terminal_delete_sessions` 准入栅栏。
- `dispose_after_resource_release`：pop `team_agent` → `_stop_local_team_runtime` → 释放 A2X 预留 → `clear_active/pending_runtime` → `release_current_round` → 清终止标记。

Go 的 `TeamManager` 已经有 `stopLocalTeamRuntime` / `cleanupRuntimeLocals` / `StopSessionRuntime` / `CancelSessionRuntime`（`team_manager_lifecycle.go:304/359/410/676`），缺的是 `_dispatch_swarmflow_controller`（swarmflow 域未迁）。也就是说 **D-13 登记的理由"团队体系未迁移"已经过时**：主体依赖已完成，桩却没有回填。

**影响**：删除团队会话时，本地 TeamAgent 不 pop、原生 harness 不 dispose、`sys_operation` 不从全局 ResourceMgr 摘除、supervisor/forwarder 不停；随后 DB 表被删，仍在跑的团队任务可能继续写已删除的表，或至少把 goroutine、SQLite 连接、模型调用泄漏到进程结束。

**建议**：按 Python 两步实现，`swarmflow` 一段留空但其余全做；`dispose` 至少完成 `team_agents.pop` + `stopLocalTeamRuntime` + `clearActive/Pending`。

---

### S-03 沙箱限制开关映射写反：默认更严，打开反而放行

**这是哪条流程**：团队每个成员建脑 → `NewNativeHarness(spec, buildCtx)`（`internal/agent_teams/harness/native_harness.go:104`）→ `ResolveDeepAgentSpecParts`（`internal/agentcore/harness/resolve_spec_parts.go:39`）→ `specToParams` → `ResolveDeepAgentParts` → `buildSysOperation` → `LocalWorkConfig.RestrictToSandbox`。

**缺陷本身**：

```go
// internal/agentcore/harness/resolve_spec_parts.go:134-137
if spec.RestrictToSandbox {
    v := false
    params.RestrictToWorkDir = &v
}
```

Python 的对应行是：

```python
# openjiuwen/harness/schema/deep_agent_spec.py:626
restrict_to_work_dir=self.restrict_to_sandbox,
```

`restrict_to_work_dir` 在 Go 里经 `buildSysOperation` 直接变成 `RestrictToSandbox`（`internal/agentcore/harness/factory.go:310-329`，`nil → true`）。于是两个方向都反了：

| `spec.RestrictToSandbox` | Python 结果 | Go 结果 |
|---|---|---|
| `false`（默认） | 不限制在工作目录 | `nil → true`，限制 |
| `true` | 限制在工作目录 | `&false`，不限制 |

**影响**：

- 所有 spec 驱动的 agent（团队每个成员都走这条）默认被限制在工作目录，而上游默认不限制。成员访问工作空间之外的工程目录、用户目录会拿到权限拒绝，属于静默的行为回归。
- 一旦有人把 `RestrictToSandbox` 置 `true`（字段带 JSON tag，配置里可以出现），得到的是**解除**限制——安全方向完全相反。

目前 `DeepAgentSpec.RestrictToSandbox` 在生产代码里没有赋值点，所以 `true` 分支是潜伏的；但 `false` 默认值这一半**已经对所有团队成员生效**。

**建议**：改成 `params.RestrictToWorkDir = &spec.RestrictToSandbox`（或始终显式赋值），并补一条 spec 路径的用例，断言 `restrict_to_sandbox=true → LocalWorkConfig.RestrictToSandbox=true`。

---

### S-04 并发第二次 `Pause` 的应答永远不会兑现，调用方挂住

**这是哪条流程**：`TeamAgent.PauseCoordination` → `CoordinationKernel.Pause` → `StreamController.PauseAgent` → `NativeHarness.Pause(ctx)`（`native_harness.go:336`）→ `enqueue` → `waitAck`。`kernel.Pause` 的幂等守卫是 `lifecycleState != running` 才早退（`kernel.go:334`），而 `lifecycleState` 要到 `Pause` 最后一行才改成 `paused`；中间的"等工具跑完再停"可以持续数秒。所以两次暂停请求可以同时进入 `harness.Pause`。

**缺陷本身**：

```go
// internal/agent_teams/harness/native_harness.go:559-565
case *cmdPause:
    // 工具相位的暂停把 ack 延迟到轮末结算，此处不能提前应答。
    if err := h.onPause(cmd); err != nil {
        ack(cmd.reply, err)
    } else if h.st.Phase() != HarnessStatePausing {
        ack(cmd.reply, nil)
    }
```

`dispatch` 用"当前相位是不是 PAUSING"来猜"这次 `onPause` 有没有延迟应答"。但 `onPause` 对**非 RUNNING** 的相位是直接 `return nil`：

```go
// native_harness.go:806-810
if h.st.Phase() != HarnessStateRunning {
    // PAUSING：协作暂停已在飞；PAUSED/IDLE/TERMINATED：无可暂停。
    return nil
}
```

第二次 `Pause` 进来时相位已经是 `PAUSING`：`onPause` 返回 nil、没有保存 `PauseAck`；`dispatch` 看到相位是 PAUSING，也跳过 ack。`waitAck` 只能等 ctx 取消——如果调用方传的是长生命周期 ctx，就是永久挂住。

Python 的写法没有这个歧义：

```python
# native_harness.py:1001-1005
if phase is not HarnessState.RUNNING:
    self._ack(cmd.ack, None)   # 无论谁在暂停，本次调用立即应答
    return
```

**影响**：用户重复点暂停、或前端超时重试、或两条路径同时暂停时，第二个请求的 AgentServer handler 会卡住；`PauseSessionRuntime` 的调用方也拿不到返回。

**建议**：`onPause` 返回 `(deferred bool, err error)`，`dispatch` 据此决定是否 ack；不要用相位反推。

---

## 二、一般（G）

### G-01 网关暂停改成同步等待，会把串行消息循环堵住最长 600 秒

**流程**：网关 `forwardLoop` → `handlePauseResume`（`internal/swarm/gateway/message_handler/forward_loop.go:584`）→ `sendInterruptToAgentWithEnvelope`（同步 `SendRequest`）→ 等 AgentServer ack → `cancelStreamsForPausedSession`。

**问题**：Python 是 `asyncio.create_task(self._send_pause_and_govern_streams(...))`（`gateway/message_handler/message_handler.py:5269-5272`），发出去就继续，不阻塞 forward loop。Go 改成同步等待（spec §8.6 有意为之），但 `AgentClient.SendRequest` 的超时是 600 秒（`internal/swarm/gateway/routing/agent_client.go:275`）。AgentServer 一旦卡住（例如暂停时要等一个长工具跑完），网关这条串行循环就 10 分钟不处理任何消息。

**建议**：保留"先 ack 再治理"的顺序，但把等待放到独立 goroutine，并给一个短超时（如 5s）；或至少让 forward loop 在等待期间可被新消息打断。

### G-02 团队一轮结束时漏了 `session.commit_interrupt()`

**流程**：`NativeHarness.runRound`（`native_harness.go:730`）拿到 `result` 后写流。

Python 的 `_persist_and_write_round_result`（`native_harness.py:1472-1483`）在写流之前有一句：

```python
if isinstance(result, dict) and result.get("result_type") == "interrupt" and self._session is not None:
    await self._session.commit_interrupt()
```

Go 只做了：

```go
// native_harness.go:777-780
if !isControlResult(result) {
    h.agent.WriteRoundResultToStream(ctx, result, h.sess)
    inv.Result = result
}
```

`WriteInvokeResultToStream` 内部只负责 HITL 的流帧，不会落检查点；Go 的 `Session` 也根本没有 `CommitInterrupt`（只有 `Commit → PostAgentExecute`）。`InterruptAgentExecute` 在 checkpointer 里是有的（`persistence.go:606`），只是没暴露到 `Session`。

**影响**：团队成员走到 ask_user / 工具中断时，中断检查点不落盘；进程重启或冷恢复时，这个中断可能不可恢复。

**建议**：给 `Session` 补 `CommitInterrupt(ctx)`（调 `inner.Checkpointer().InterruptAgentExecute`），在 `runRound` 写流前按 `result_type=="interrupt"` 调用。

### G-03 `SessionManager.CloseSession` 共用一个 timer，第一个目标超时后其余目标会无限等

**流程**：`session.delete` / `close_session` → `SessionManager.CloseSession`（`internal/swarm/runtime/session_manager.go:199`）。

```go
// session_manager.go:256-272
timer := time.NewTimer(...)
for _, target := range waitTargets {
    select {
    case <-target.result: ...
    case <-timer.C:      pending++
    case <-ctx.Done():   pending++
    }
}
```

`time.Timer` 的通道只发一次。第一个目标超时后 `timer.C` 再也不会就绪；后续目标既没有结果、ctx 又没取消时，就会一直阻塞在 `<-target.result`，整个 `CloseSession` 超过 `wait_timeout` 无限延长。Python 是一次 `asyncio.wait(..., timeout=...)` 管全部目标。

**建议**：改成 `deadline := time.Now().Add(timeout)` + 每个目标用 `time.Until(deadline)` 派生一个 timer，或先算剩余时间再 select。

### G-04 `CloseSession` 的 `drainCancelled` 无锁改队列，与处理器 `heap.Pop` 竞态

**流程**：同上。`CloseSession` 在 `m.mu.Unlock()` 之后调 `queue.drainCancelled()`（`session_manager.go:226`），而 `drainCancelled` 直接改 `q.items`（`:688-698`），完全没拿锁。处理器侧的 `popTask` 是在 `m.mu` 内做 `heap.Pop(queue)`（`:556-560`）。两者可以同时操作同一个 `q.items` 切片。

虽然 `CloseSession` 已经删掉了 `sessionProcessors[sessionID]`，`popTask` 下一轮会看到 `closed=true` 退出，但"检查—Pop"是在锁内一次完成的，`drainCancelled` 在锁外，竞态窗口仍然存在。`-race` 在高压下可能报 `slice append/assign` 竞态。

**建议**：`drainCancelled` 要么在 `m.mu` 内调用，要么由 `CloseSession` 在锁内把 `queue.items` 摘走再在锁外处理。

### G-05 暂停后成员 `execution_status` 不复位

**流程**：`CoordinationKernel.Pause`（`internal/agent_teams/agent/coordination/kernel.go:334-400`）。

Python 的 pause 在 `_mark_live_teammates(PAUSED)`、`cancel_recovery_tasks`、`shutdown_all_handles` 之后还有一步：

```python
# kernel.py:355-357
if team_backend is not None and host.team_name is not None:
    await team_backend.db.member.reset_paused_member_execution_status(host.team_name, paused_teammates)
```

Go 的 `MarkLiveTeammates` 只写 `status=PAUSED`（`team_agent.go:1626`），没有对应的 `execution_status=IDLE` 批量重置；`ResetPausedMemberExecutionStatus` 全仓不存在（`UpdateMemberExecutionStatus` 有 FSM 校验，不能拿来批量改）。

**影响**：暂停后成员在 DB 里仍是暂停前的非 IDLE 执行状态（例如 `running`），恢复/列表/看板会显示"还在跑"，且再次启动时 FSM 可能拒绝合法转换。

**建议**：在 `database.TeamDatabase` 补批量重置（对齐 `member_dao.py:649-681`：`status=PAUSED AND execution_status != IDLE → IDLE`），在 `kernel.Pause` 的 leader 分支调用。

### G-06 `kernel.Stop` 不销毁原生运行时、不停 messager

**流程**：团队停止 → `CoordinationKernel.Stop`（`kernel.go:424-489`）。

Python 的 `stop` 在 `close_stream()` 之后有：

```python
# kernel.py:590-593
if host.resources.harness is not None:
    await host.resources.harness.dispose()
messager = host.infra.messager
if messager is not None:
    await messager.stop()
```

Go 的 10 步里既没有 `harness.Dispose()`，也没有 `messager.Stop()`。而 `NativeHarness.Dispose` / `TeamHarness.Dispose` 已经实现（`native_harness.go:413`、`team_harness.go:449`），**生产零调用**。

**影响**：每次团队停止后，原生 supervisor/forwarder 继续跑、持有会话与流；messager 的收发循环不退出；`sys_operation` 不从全局 ResourceMgr 摘除。这是上一轮 S-03 的同一个缺口，本轮新增了 `Dispose` 却没接上。

**建议**：`kernel.Stop` 尾部补 `harnessAcc.Dispose(ctx)` 与 `messager.Stop(ctx)`；注意顺序要在 `release_session` 之前。

### G-07 `TeamHarness.Stop` 不提交子会话

**流程**：一个 run cycle 结束 → `TeamHarness.Stop`（`internal/agent_teams/harness/team_harness.go:415-428`）。

Python 的 `stop` 有：

```python
# team_harness.py:171-190
try:
    if self._active_agent_session is not None:
        await self._active_agent_session.commit()
except Exception:
    logger.debug(...)
finally:
    self._native_session_id = None
    self._active_agent_session = None
```

Go 只做了 `native.Stop` + 清空 `nativeSessionID` / `activeSession`，没有 `Commit`。`NativeHarness.Stop` 的 `post_run` 只在 `ownSession=true` 时执行，而团队成员的子会话是注入的（`ownSession=false`），所以这一轮的状态没有落盘点。

**影响**：跨 run cycle 依赖"同一个 session id 的 checkpoint"恢复计划/历史/plan mode；不提交就可能恢复到上一轮之前。

**建议**：在 `Stop` 里补 `activeSession.Commit(ctx)`，失败只记 Debug（对齐上游）。

### G-08 `NativeHarness.Stop` 不复位 `started`，`Start` 也不会重建已终止的 native

**流程**：`TeamHarness.Start`（`team_harness.go:383`）→ `native.Start`（`native_harness.go:187`）。

```go
if h.started {
    ...
    return nil   // 静默成功，但 supervisor 已经退出
}
```

`Stop` 只把 `stopping` 置回 false，`started` 一直是 true。所以同一个 `NativeHarness` 在 `Stop` 之后再 `Start` 会直接返回 nil，supervisor/forwarder 不会重启；之后 `Send` 因为 `started==true` 通过 `enqueue`，命令进入一个没有消费者的队列，`waitAck` 一直等；`Resume` 则会因为相位 `TERMINATED` 返回"已终止"。

Python 的 `TeamHarness.start` 明确处理了这一点：

```python
if self._native is None or self._native.state is HarnessState.TERMINATED:
    self._native = NativeHarness(self._agent_spec, self._build_context)
    self._remount_dynamic_tools()
```

**影响**：目前团队每次 run cycle 是否复用同一个 `TeamHarness` 决定了这个问题是否暴露。只要有一条路径复用（持久团队 pause→stop→start 的冷恢复就是候选），第二轮就会静默空转。

**建议**：`Stop` 里把 `started=false`；`TeamHarness.Start` 在 `native == nil || native.State()==TERMINATED` 时重建 native（对齐上游），而不是返回错误。

### G-09 补充输入（supplement/steer）的校验与 `input_mode` 没接网关

**流程**：`chat.send` / `chat.cancel(intent=supplement)` → 网关 `forwardLoop`。

10.3.28d 新增了 `internal/swarm/runtime/session_input.go`（`ResolveSessionInputMode` / `ResolveSessionInputModeChecked` / `ValidateSteerImageReferences` / `ValidateSessionInput`）和 `RuntimeSessionCoordinator.StreamSessionInput`，但：

- 四个校验/解析函数生产调用方为 **0**（只有定义）。
- `StreamSessionInput` 生产调用方为 **0**。
- 网关侧 `handleSupplement`（`forward_loop.go:478`）和 `chat.send` 主路径都没有 `input_mode` 写入、没有 `runtime_mode` 剥离、没有图片引用校验。

Python 的 `_forward_loop` 在 `chat.send` 前明确做了：

```python
# message_handler.py:4983-4990
is_session_input = self._is_session_input_message(msg)
if is_session_input:
    validate_session_input(msg.params)
    msg.params["input_mode"] = resolve_session_input_mode(msg.params).value
    msg.params.pop("runtime_mode", None)
```

**影响**：带 `input_mode` 的补充输入/转向消息会按普通发送处理，可能被当成新一轮而不是并入当前轮；非法图片引用不会被拒绝。

**建议**：在 `forwardLoop` 的 `chat.send` 分支接上 `ValidateSessionInput` + `input_mode` + 剥 `runtime_mode`；`supplement` 路径接 `StreamSessionInput`。

### G-10 spec 驱动路径丢了一批配置字段

**流程**：`DeepAgentSpec` → `specToParams`（`resolve_spec_parts.go:104-172`）→ `CreateDeepAgentParams`。

`specToParams` 只搬了一部分字段，Python `resolve_parts` 的关键字参数里还有这些没搬（Go 的 `DeepAgentSpec` 甚至没有对应字段）：

- `context_engine_config`
- `enable_read_image_multimodal`
- `enable_security_rail`
- `enable_tool_resilience_rail`
- `enable_subagent_runtime`
- `enable_sys_operation`
- `cwd` / `project_root`
- `kv_cache_affinity_config`
- `progressive_tool_enabled` / `tool_search_limit`

25.1b spec 第 1186 行声称"`enable_security_rail` / `model_selection` / `todo_modify_factory` / `restrict_to_work_dir` 等 spec 开关静默失效已随本次重构一并消除"，但至少 `enable_security_rail` 在 Go 的 spec 与 params 里都不存在，这个结论是超范围的。

**影响**：团队成员的 spec 如果带 `context_engine_config`（压缩/窗口）、`enable_read_image_multimodal`（读图）等配置，会被静默丢弃；用户改配置不生效且无告警。

**建议**：要么补齐字段与搬运，要么在 spec 的「已知偏差」逐项登记并给出回填章节；不能只写一句"已消除"。

### G-11 `TeamContextTracker.persist` 在锁内做 I/O

**流程**：每轮结束 / 每次输入受理 → `persist`（`internal/agent_teams/tracker/team_context.go:817`）。

```go
t.mu.Lock()
defer t.mu.Unlock()
...
sess.UpdateState(...)
committer.Commit(context.Background())   // 可能落盘
```

`claim` / `release` / `announce` 都要拿同一把 `t.mu`。`Commit` 是 SQLite 写，慢的时候会把同一成员的所有通知路径阻塞住。Python 单线程没有锁，但也没有"持锁做 I/O"的问题。

**建议**：锁内只做"算 current / 更新内存基线"，`UpdateState` + `Commit` 放到锁外，失败再回滚内存（或允许下一轮重试）。

---

## 三、提示（T）

### T-01 `InboxMessage.Seq` 与 `HarnessInternalState.seq` 是死字段

`state.go:80-87` 定义了 `Seq`，`:102-104` 定义了 `seq`，但全仓没有任何 `nextSeq` / `Seq:` 赋值（`grep` 零命中）。Python 的 `_on_send` 每次都 `seq = self._st.next_seq()` 并把 seq 作为 ack 返回给调用方。Go 的 `Send` 返回 error，seq 不再需要，字段应删除或注明保留原因。

### T-02 `cmdStop` 不清 `pausedQuery`

Python `_on_stop` 有 `self._st.paused_query = None`（`native_harness.py:1272`）；Go 的 `dispatch`（`native_harness.go:548-554`）只 `SetActive(nil)` + 转 TERMINATED，没有 `ClearPausedQuery()`。停止后 `PausedQuery()` 仍会返回旧值，后续若有代码在 IDLE 读它，会拿到已作废的查询。

### T-03 `remove_ability` 未迁移

Python 的 `remove_ability` 会把有状态工具按限定 id 从 ResourceMgr 摘掉（`ability_manager.py:844-858`）。Go 只有 `Remove`（只删本地 map，`ability_manager.go:373`），没有 `RemoveAbility`。多数生产调用方自己补了 `resourceMgr.RemoveTool`（如 `member_skill_toolkit_rail.go:106`、`deep_adapter_tools.go:758`），但通用入口缺失，新调用方容易只调 `Remove` 留下资源泄漏。

### T-04 `finishClosingTask` 从已被消费的结果通道读错误，日志形同虚设

`CloseSession` 已经在 `case <-target.result` 里取走了结果，随后 `finishClosingTask` 又 `select { case result := <-task.result: ... default: }`（`session_manager.go:345-351`）。通道已空，永远走 default，任务失败的错误不会被记录。Python 用 `task.add_done_callback` + `task.exception()`，能拿到真实错误。

### T-05 `after_react_iteration` 少了迭代边界的 debug 日志

Python 在捕获 `last_iter_snapshot` 后打一条 `round_id/iteration/msgs` 的 debug（`snapshot_rail.py:219-224`）。Go 的 `AfterReactIteration`（`snapshot_rail.go:102-119`）没有。排查"快照为什么是空的"时这条日志很有用。

### T-06 `NativeHarness.Stop` 不注销事件订阅

Python `stop` 有 `await self._events.unregister_namespace(_EVENT_NAMESPACE)`（`native_harness.py:452`），用于断开消费者闭包引用。Go 的 `eventBus` 没有 unregister，`Stop` 也不处理。目前因为 supervisor 已退出、不再发事件，泄漏风险不高，但与上游的清理契约不一致。

### T-07 过期/重复注释

- `native_harness.go:326`：`Abort` 注释仍写"本阶段 immediate=true 只取消调度任务，不回滚上下文（spec D-2，⤵️ 25.1c 补回滚）"，25.1c 已经补了回滚，注释应更新。
- `native_harness.go:87-103`：`NewNativeHarness` 上方同时存在"25.1a 阶段接收外部已建好的 agent"和"25.1b 改为接收 spec"两段互相矛盾的说明，应合并。
- `team_harness.go:382`：`Start` 注释仍写"四步在本阶段为 no-op（⤵️ 25.1b spec 驱动…）"，25.1b 已标记完成，需逐项确认哪些仍 no-op（`_remount_dynamic_tools` 与 `background_task_controller` 是 D-12 明确的 25.1d/25.6，`kv_cache` 钩子才是 25.6）。

### T-08 `session_stubs.go` 的 `DeleteAborted` / `DeleteCommitted` 只打 Debug

Python 这两个方法会清 `_terminal_delete_sessions` 准入栅栏（`team_manager.py:3762-3768`）。Go 侧是空实现，意味着删除事务失败/提交后没有"重新开放准入"的动作。当前 Go 也没有对应的栅栏，属于"整套机制未迁"，建议在 spec 里显式登记，而不是留一个看起来实现了的 Debug 日志。

---

## 四、上一轮（11-08）遗留项复核

| 编号 | 上轮结论 | 本轮复核 |
|---|---|---|
| S-01 会话取消把 sessionID 当 requestID | 取消永远匹配 0 条 | **仍在**：`internal/swarm/server/runtime/interface.go:580-583`、`:590-593` 两处都是 `RequestID: &sessionID`（10.3.28d 重构后原 `:477` 那处已不存在）。本轮的 S-01 也复用了这行 |
| S-02 `MarkTerminalIfActive` 先返回后校验 | `cancelled` 计数恒 0 | **已修**：`execution_registry.go:197-206` 现在先校验再标记并返回 true |
| S-03 团队轮结束不调用 `harness.Stop` | 原生运行时从不收尾 | **仍在**，且范围更大：`kernel.Stop` 连 `harness.Dispose` 也没有（见 G-06）；本轮新增的 `Dispose` 生产零调用 |

---

## 五、建议的处置顺序

1. **先修 S-01 / S-02**：这两条是用户可见的"按钮没接线"，改动集中在 `processTeamInterrupt` 与 `session_stubs.go`，依赖都已具备，风险低、收益大。
2. **再修 S-03 / S-04**：一个是安全方向反了，一个是并发挂起；都是小改动，但需要补用例锁住（`restrict_to_sandbox=true` 的 sysop 配置、两次并发 Pause）。
3. **G-06 / G-08 一起做**：把 `Dispose` 接到 `kernel.Stop`，并让 `TeamHarness.Start` 在 TERMINATED 时重建 native；这两件事互为前提（不复位 `started` 就没法重建）。
4. **G-02 / G-07**：补 `Session.CommitInterrupt` 与 `TeamHarness.Stop` 的 commit，属于"状态能不能恢复"的底座。
5. **G-03 / G-04**：`CloseSession` 的 timer 与队列竞态，建议直接补一个多目标 + 超时的单测，`-race` 跑一遍。
6. **G-09 / G-10**：补充输入接线与 spec 字段补齐，涉及面广，建议单独一批并同步更新 25.1b/10.3.28d 的 spec 偏差表。
7. 提示项可随相关文件的下一次改动顺手清理（尤其 T-07 的过期注释，避免下一轮审查再按错误前提判断）。

---

> 本文只记录审查结论，未修改任何代码。所有结论均可在 HEAD `51663d344` 上按文中 `文件:行号` 复核。
