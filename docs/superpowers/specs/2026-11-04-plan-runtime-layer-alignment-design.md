# Plan / Runtime 层对齐设计（S3）

- 日期：2026-11-04
- 状态：待评审
- 上游基线：jiuwenswarm `a2abceccf`（`jiuwenswarm/runtime/`、`jiuwenswarm/server/agent_ws_server.py`、`jiuwenswarm/server/runtime/agent_adapter/`）
- 前序：`2026-11-04-prompt-attachment-foundation-design.md`（S1）、`2026-11-04-s2-rail-and-mode-design.md`（S2）

## 1. 背景：S2 把 plan 状态机放错了层

S2 把 `PlanModeController` 的调用点放进 `DeepAdapter`（`internal/swarm/server/adapter/deep_adapter_plan.go`），而上游把它放在 **`AgentRuntime`**（`jiuwenswarm/runtime/service.py:350`），比 adapter 高两层。`interface_deep.py` 对 plan 状态零感知（`grep '.ensure_state('` 在 `interface*.py` 中 0 命中）。

由此产生三个可核实缺陷：

| # | 现象 | 根因 |
|---|---|---|
| 1 | 模型收到的用户消息被裸原文覆盖，`BuildUserPrompt` 生成的 JSON 信封（`source`/`preferred_response_language`/`timestamp`/`files_updated_by_user`/`trusted_dirs`/`skills_to_use`）整段丢失 | `d861f632` 在 adapter 里写 `inputs["query"] = query`；而信封在 `UapClaw.BuildInputs` 已经生成 |
| 2 | 进入 plan 的那一轮用户原文丢失 | 同上，`ensureState` 只读 `params["query"]`，改写结果写回 `inputs["query"]` |
| 3 | `plan_mode_exited` 控制事件没有出口 | 事件缓冲在 `DeepAdapter.controlEvents`，`DrainControlEvents` 全仓无消费方 |

## 2. 步骤级差异（本轮一并修）

| # | Python | Go 现状 | 处置 |
|---|---|---|---|
| 1 | `AgentRuntime.normalize_request`（`service.py:3790`）、`uses_session_runtime`（`:3842`）在入口调用 | 无 | 新增，`Invoke/Stream` 首步调用 |
| 2 | `_session_stream_tasks[sid][task] = stream_stop_event`，仅 `targeted_expert or not uses_session_runtime` 时登记 | `beginStreamTask` 一律登记，无 stop event | 登记项补 `stopEvent`，登记条件按 `usesSessionRuntime` |
| 3 | `StreamKeepalive` 类（`agent_ws_server.py:699`）：`start/notify_activity(terminal)/signal_stop/stop` | `runKeepalive` goroutine + `heartbeatTrigger` 通道 | 包成 `StreamKeepalive` 类，行为保持 |
| 4 | `_send_runtime_event`（`:5024`）返回是否发送成功，失败即停止流 | `writeChunk` 无返回值 | 新增 `sendRuntimeEvent(...) bool`，失败停止流 |
| 5 | 流循环里 `plan_mode_exited` 走独立发帧分支（不占 sequence、不重置 keepalive） | 无 | 新增分支 |
| 6 | `TurnOutcomeTracker`、`_persist_repeated_session_question`、`_open_session_message_resume` | 无（session-message 能力在 Go 是注释桩） | **延后**，记 ⤵️ |
| 7 | `_handle_session_plan_status`（`:3779`）`session.plan_status` 只读查询 | handler 与 RPC 常量均缺失 | 本轮补 |
| 8 | `resolve_agent_request_mode`（`runtime/request.py:266`）单点解析 | Go 三份实现：`resolveMode`（默认 `"agent","plan"`）、`applyResolvedModeToRequest`、`resolveModeFromRequest` | 统一为一份 |

## 3. 目标与非目标

### 目标

1. 新增 `AgentRuntime`（`internal/swarm/runtime/service.go`），承载 chat turn 全流程，方法名与上游同名。
2. `AgentServer` 的 chat 入口退化为「分发 + 发帧 + keepalive + 任务登记」，内部步骤与 `_handle_unary_impl` / `_handle_stream_impl` 一一对应。
3. `RuntimeEvent` 落地，控制事件（`plan_mode_exited`）真正下发。
4. `params` 成为 query 的唯一事实源，`inputs["query"]` 只是 `BuildInputs` 的投影（顺序天然正确，删除 `resolveRequestQuery`）。
5. `runtime/request.py` 的 8 个顶层函数在 Go 有同名实现。
6. 补 `session.plan_status` RPC，前端可读当前 plan 状态。

### 非目标（本轮不做，记 ⤵️）

| 项 | 原因 | 上游锚点 |
|---|---|---|
| admission controller / `get_agent_for_request` | Go 无该抽象，本轮用 `AgentManager.GetAgent` | `runtime/request.py:524-539` |
| `wait_for_session_prewarm` / agent warm pool | Go 无预热池 | `runtime/request.py:521`、`server/runtime/agent_warm_pool.py` |
| session-message resume 三件套（差异 6） | 依赖 session message service，Go 为注释桩 | `agent_ws_server.py:4856/4881`、`runtime/plan.py:275` 的 `SessionMessageRequiresQueue` 恒 false |
| `session_provisioner.py` | 96KB 独立模块 | `jiuwenswarm/runtime/session_provisioner.py` |
| `service.py` 其余 130+ 方法（模型/模式/权限目录、团队、离线清理等） | 与本次缺陷无关 | `runtime/service.py` 其余部分 |
| runner/checkpointer 生命周期上移 | Go 现在 adapter 懒初始化（`deep_adapter.go:1899`），改动风险高 | `service.py:_initialize_runtime_dependencies` |
| 所有 chunk 走 `RuntimeEvent` | 采用回调式（Python 同形），chunk 保持直传 | `service.py` 的 `yield event` 形态 |

## 4. 目录与文件映射

### 4.1 `internal/swarm/runtime/`（← `jiuwenswarm/runtime/`）

| 文件 | 动作 | 内容 |
|---|---|---|
| `doc.go` | 更新 | 文件树补 `service.go`/`events.go` |
| `plan.go` | 改 | `PlanModeController` 去掉构造期 store 绑定；`EnsureState/CheckPostProcessExit` 增加 `PlanStateAccessor` 入参；新增 `ActiveSessions()` |
| `request.go` | 补 7 个函数 | `ResolveRequestProjectDir`、`ResolveCancelProjectDir`、`SyncChatRequestMetadata`、`ResolveAgentRequestMode`、`ApplyResolvedModeToRequest`、`PrepareChatTurn`、`CancelRequest` |
| `service.go` | 新建 | `AgentRuntime` |
| `events.go` | 新建 | `RuntimeEvent` |
| `session_provisioner.go` | 不建 | ⤵️ 后继切片 |

### 4.2 `internal/swarm/server/runtime/`

| 文件 | 动作 |
|---|---|
| `uapclaw.go` → `interface.go` | 改名；类型 `UapClaw` → `UapClawSwarm` |
| `uapclaw_test.go` → `interface_test.go`、`uapclaw_history_test.go` → `interface_history_test.go` | 改名 |
| `agent_manager.go` | 删 `resolveModeFromRequest`，改调 `runtime.ResolveAgentRequestMode` |

### 4.3 `internal/swarm/server/`（← `agent_ws_server.py`，按方法归类拆文件）

| 文件 | 承载方法 | 上游 |
|---|---|---|
| `handle_message.go` | `handleMessage`（原 `handleEnvelope`）、`writeResponse`/`writeChunk`/`writeErrorResponse`/`sendToGateway` | `_handle_message` |
| `handle_chat.go` | `handleUnary`/`handleUnaryImpl`/`handleStream`/`handleStreamImpl`/`handleCancel`/`sendRuntimeEvent` | `_handle_unary(_impl)`/`_handle_stream(_impl)`/`_handle_cancel` |
| `keepalive.go` | `StreamKeepalive` | `StreamKeepalive`（`:699`） |
| `handle_session_plan.go` | `handleSessionPlanStatus` | `_handle_session_plan_status`（`:3779`） |
| `handle_session.go` 等 | 不动 | — |

拆分原则：**只搬方法、不改方法体**；每个文件的方法集合与上表一致，方法名与上游同名（导出形式）。

## 5. 符号映射

| Python | Go |
|---|---|
| `AgentRuntime.start / close` | `Start / Close` |
| `AgentRuntime.agent_manager / plan_controller` | `AgentManager() / PlanController()` |
| `AgentRuntime.invoke / stream` | `Invoke / Stream` |
| `AgentRuntime._invoke_started / _stream_started` | `invokeStarted / streamStarted` |
| `AgentRuntime._prepare_chat_turn` | `prepareChatTurn`（薄壳）→ `runtime.PrepareChatTurn` |
| `AgentRuntime._emit_control_events / _control_events` | `emitControlEvents / controlEvents` |
| `AgentRuntime.cancel_request / cleanup_session / reset_session` | `CancelRequest / CleanupSession / ResetSession` |
| `AgentRuntime.normalize_request / uses_session_runtime` | `NormalizeRequest / UsesSessionRuntime`（静态） |
| `RuntimeEvent.control / from_agent_message / to_dict / event_type` | `Control / FromAgentResponse / ToDict / EventType` |
| `_handle_message / _handle_unary(_impl) / _handle_stream(_impl)` | `handleMessage / handleUnary(+Impl) / handleStream(+Impl)` |
| `_send_runtime_event` | `sendRuntimeEvent` |
| `StreamKeepalive.start / notify_activity / signal_stop / stop` | `Start / NotifyActivity / SignalStop / Stop` |
| `_handle_session_plan_status` | `handleSessionPlanStatus` |
| `resolve_agent_request_mode` | `ResolveAgentRequestMode` |
| `apply_resolved_mode_to_request` | `ApplyResolvedModeToRequest` |
| `resolve_request_project_dir / _resolve_cancel_project_dir` | `ResolveRequestProjectDir / ResolveCancelProjectDir` |
| `sync_chat_request_metadata` | `SyncChatRequestMetadata` |
| `prepare_chat_turn / cancel_request` | `PrepareChatTurn / CancelRequest` |

## 6. 步骤规格

### 6.0 `AgentRuntime.Start` / `Close`（← `service.py:655/691` 及 `close`）

- `Start`：幂等置位 `started=true`（重复调用记 Warn 返回 nil），不接管 Runner/checkpointer 生命周期（偏差见 §13）。
- `Close`：置 `closed=true`，拒绝后续调用；不释放 Runner/checkpointer。
- `AgentRuntime` 由 `AgentServer` 持有，构造时注入 `AgentManager` 与 `*runtime.PlanModeController`。

### 6.1 `AgentRuntime.Invoke`（← `service.py:1694 → 1786 _invoke_started`）

```
1  requireStarted()
2  request := NormalizeRequest(request)
3  mode, subMode, agent := prepareChatTurn(agentManager, request, channelID)
   3.1 ApplyResolvedModeToRequest（work_mode 参与，写回 params.mode）
   3.2 resolveRequestProjectDir
   3.3 GetAgent(mode, subMode, projectDir)          // ⤵️ admission/prewarm 延后
   3.4 bindAgent(sessionID, requestID, agent)       // Go 暂无协调器：空实现 + Debug，记 ⤵️
4  planEnabled := !resolved.IsTeam && !isInterruptResume && ShouldSync(reqMethod)
5  if planEnabled:
      res := planController.EnsureState(request, mode, subMode, accessor=agent)
      emitControlEvents(res.Events)                  // 进入事件
6  resp := agent.ProcessMessage(request)             // 内部才 BuildInputs
7  defer: if planEnabled:
          events := planController.CheckPostProcessExit(request, accessor=agent)
          emitControlEvents(events)                  // 退出事件
8  return []*RuntimeEvent{FromAgentResponse(resp)}
```

> 步骤 3 恒定执行，`planEnabled` 只决定 5/7 是否同步 plan 状态（对应上游 `if not readonly_goal_get` 与 `ShouldSync` 两处守卫）。

### 6.2 `AgentRuntime.Stream`（← `service.py:2183 → 2482 _stream_started`）

```
1-5 同 Invoke（含 planEnabled 守卫）
6  ch := agent.ProcessMessageStream(request)；逐 chunk 转 RuntimeEvent 交给渲染（不缓存）
7 流结束：checkPostProcessExit → emitControlEvents（在 channel 关闭前发帧）
8 错误：保留主错误，plan 错误单独记录（对应 service.py plan_error/primary_error）
```

### 6.3 `handleUnaryImpl`（← `agent_ws_server.py:3941`）

```
1 特殊方法分流：initialize / session.create / session.fork / acp.tool_response
2 resumeState := nil（⤵️ openSessionMessageResume 延后）
3 events := runtime.Invoke(request, onControlEvent=sendPush 控制帧)
4 for ev := range events: sendRuntimeEvent(ev, streaming=false, sequence=0)
5 写最终响应
```

### 6.4 `handleStreamImpl`（← `agent_ws_server.py:4654`）

```
1 sessionID := organizationStreamKey(request)
2 request = NormalizeRequest(request)
3 streamStop := newStreamStopEvent()
   if !usesSessionRuntime(request): registerStreamTask(sessionID, task, streamStop)
4 keepalive := NewStreamKeepalive(server, request, sendLock); keepalive.Start()
5 runtimeStream := runtime.Stream(request, onControlEvent=sendControlEvent)
6 for ev := range runtimeStream:
     if ev.EventType() == plan_mode_exited: sendControlEvent(ev); continue   // 不占 sequence
     chunkCount++
     keepalive.NotifyActivity(terminal=ev.IsComplete)
     if !sendRuntimeEvent(ev, streaming=true, sequence=chunkCount-1) {
         Warn("流式响应因单个 chunk 发送失败而停止"); return
     }
7 finally: keepalive.SignalStop() → runtimeStream.Close() → keepalive.Stop() → releaseStreamTask
```

### 6.5 `sendRuntimeEvent`（← `_send_runtime_event:5024`）

```
func (s *AgentServer) sendRuntimeEvent(ev *runtime.RuntimeEvent, streaming bool, sequence int) bool
1 若 ev.EventType() == plan_mode_exited → s.sendPush(frame) ; return true
2 若 streaming → chunk := ev.ToChunk(); data := e2a.EncodeAgentChunkForWire(...)
   否则 → resp := ev.ToResponse(); data := e2a.EncodeAgentResponseForWire(...)
3 return s.sendWirePayload(data)     // false = transport 关闭或缓冲满
```

> 与上游差异：Python 的 `false` 来自 `send_wire_payload` 的帧大小上限；Go 侧来自 `ChannelTransport.TrySendToRecvCh` 的关闭/缓冲满。语义对齐到「发送失败即停止流」，具体判定条件记入 spec 偏差。

### 6.6 `handleSessionPlanStatus`（← `_handle_session_plan_status:3779`）

```
1 sid := params["session_id"] or request.session_id
2 有 live agent 时以 DeepAgentState.PlanMode 为准
3 否则回退 session metadata 的 mode
4 不调用 SwitchMode / EnsureState，不修改活跃集合
返回 {session_id, plan_mode, mode}
```

`PlanModeController` 只读接口：`ActiveSessions() map[string]struct{}`（对应 `get_plan_active_sessions()`），供活跃集合分支使用。

## 7. mode 解析统一

三份实现收敛为一套：

| 现状 | 处置 |
|---|---|
| `handle_envelope.go: resolveMode`（默认 `"agent","plan"`） | 删除，改调 `runtime.ResolveAgentRequestMode` |
| `handle_envelope.go: applyResolvedModeToRequest` | 移入 `runtime/request.go: ApplyResolvedModeToRequest` |
| `agent_manager.go: resolveModeFromRequest` | 删除，改调 `runtime.ResolveAgentRequestMode` |

`ResolveAgentRequestMode(rawMode any, workMode string) (mode, subMode, canonical string)`，默认值按上游：无 mode → `("agent", "", "agent")`。

## 8. `RuntimeEvent` 契约

```go
type RuntimeEvent struct {
    RequestID string
    ChannelID string
    SessionID string
    Payload   map[string]any
    IsComplete bool
    OK        bool
}
// Control 构造控制事件（payload 即 Python 的 payload dict）
func Control(request *schema.AgentRequest, payload map[string]any) *RuntimeEvent
// FromAgentResponse 由 AgentResponse 包装（对齐 from_agent_message）
func FromAgentResponse(resp *schema.AgentResponse) *RuntimeEvent
// ToDict 对齐 to_dict（不含 runtime_completion）
func (e *RuntimeEvent) ToDict() map[string]any
// EventType 取 payload["event_type"]
func (e *RuntimeEvent) EventType() string
```

控制事件 payload：`{"event_type":"plan_mode_exited","mode":"agent.work.normal"}`。

## 9. 错误处理

| 场景 | 行为 | 上游 |
|---|---|---|
| `EnsureState` 失败 | 记 Error，**继续**主流程 | `service.py:1861`（仅记录 plan_result） |
| `CheckPostProcessExit` 失败 | 保存 `planError`，不影响主错误上抛 | `service.py:2668-2682` |
| `sendRuntimeEvent` 返回 false（流式） | Warn + 停止流 | `agent_ws_server.py:4730-4737` |
| `GetAgent` 失败 | 原错误路径不变（`GET_AGENT_ERROR`） | — |
| `plan_status` 无会话 | 返回 `plan_mode=false, mode=""`，不报错 | `:3779+` |

## 10. 测试

### 单测

| 文件 | 覆盖 |
|---|---|
| `runtime/request_test.go` | 7 个新函数；`ResolveAgentRequestMode` 默认值与三段/legacy 串 |
| `runtime/service_test.go` | 用 fake manager/agent 断言 `Invoke` 步骤顺序（prepare → ensure → execute → check → emit）与 team/resume 跳过条件 |
| `runtime/events_test.go` | `Control/FromAgentResponse/ToDict/EventType` |
| `runtime/plan_test.go` | `PlanStateAccessor` 注入后 `EnsureState` 行为不变；`ActiveSessions` |
| `server/keepalive_test.go` | `NotifyActivity(terminal)` 重置语义 |
| `server/handle_session_plan_test.go` | live 优先 / metadata 回退 / 不写状态 |

### 集成（`tests/integration/swarm/server/adapter/attachment_mode`）

- IT-S2-3~7 改为从 **server 入口**驱动（`AgentServer.handleStream`），断言 plan 状态与控制事件。
- 新增 IT：进 plan 首轮 user 消息**同时**含原文与 `"preferred_response_language"`（信封完整）。
- 新增 IT：`plan_mode_exited` 帧到达客户端（unary 与 stream 各一例）。
- 新增 IT：`session.plan_status` 返回与后端状态一致。

### 门禁

`go run ./cmd/integration-audit -verify` 通过；改动行覆盖率 85%/5% 达标（新增函数同步补单测；确实不可集成的分支进 `.integration-coverage-exempt` 并写理由）。

## 11. 迁移顺序（每步可编译可测）

1. `runtime/events.go` + 单测。
2. `runtime/request.go` 7 函数 + 单测；`handle_envelope.go`/`agent_manager.go` 三处解析改造。
3. `runtime/plan.go` 解耦（`PlanStateAccessor` + `ActiveSessions`），adapter 侧 `planStateStore` 改为运行期构造。
4. `runtime/service.go` `AgentRuntime`（Invoke/Stream 五步）+ 单测。
5. `handle_session_plan.go` + RPC 常量。
6. `keepalive.go` `StreamKeepalive` 类化。
7. `handle_chat.go` 重构入口（含 `sendRuntimeEvent`、control 分支、登记条件）。
8. `uapclaw.go` → `interface.go`、`UapClaw` → `UapClawSwarm` 全量改名。
9. 删 `deep_adapter_plan.go` 的 ensure/check/emit/drain 与 `controlEvents` 字段；adapter 只留 `PlanStateAccessor`。
10. 文档：`doc.go` 文件树、`IMPLEMENTATION_PLAN.md` 24.12 行、缺陷台账 H-02 更新。

## 12. 验收

```bash
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ ./internal/swarm/server/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/...
go run ./cmd/integration-audit -verify
```
远端 CI 四 job 全绿后才算完成（Build / Unit Test / Integration Test / Lint）。

## 13. 已知偏差（写在此处避免重复讨论）

1. **chunk 不经 `RuntimeEvent`**：Python 整条流 `yield RuntimeEvent`，Go 保留 `AgentResponseChunk` 直传，仅控制事件走 `RuntimeEvent`。
2. **`false` 语义**：Python 是帧大小超限，Go 是 transport 关闭/缓冲满。
3. **resume/outcome 三件套缺席**：Go 无 session message service，`_open_session_message_resume`、`_persist_repeated_session_question`、`TurnOutcomeTracker` 记 ⤵️。
4. **admission 与预热**：`get_agent_for_request` 原子准入与 `wait_for_session_prewarm` 用 `GetAgent` 代替，记 ⤵️。
5. **文件名拆分**：Python 单文件 `agent_ws_server.py`（1.2 万行）在 Go 拆为 `handle_message.go`/`handle_chat.go`/`keepalive.go`/`handle_session_plan.go`，方法名与步骤不变（用户确认的约定）。
