# 2026-11-02 48 小时逻辑审查

审查窗口：2026-10-31 04:00 ~ 2026-11-02 04:00（HEAD `ebc36dcc`）
范围：197 个提交、300 个文件、+16377 / −2701 行。

其中与上一轮审查（`docs/review/2026-11-01-48h-logic-review.md`，覆盖到 `4157122e` 2026-11-01 00:45）
**不重叠的新增部分**为 70 个提交、141 个非前端文件、+16377 / −2701 行；本文结论均落在这一区间
（`4157122e..HEAD`），重叠区间只做回归抽查，不重复上一轮已登记的问题。

基线 CI：`41330621436`（HEAD）四 job 全绿；Real LLM `41330627309` 绿。
审查依据：`IMPLEMENTATION_PLAN.md` 第 8.4/8.5/11.15/11.26 节、`docs/superpowers/specs|plans/` 下各节设计/计划，
逐条对照 Python 参考实现（`/home/opensource/agent-core` = openjiuwen；`/home/opensource/jiuwenswarm-develop` = jiuwenswarm）。

本轮覆盖的提交簇：

| 簇 | 章节 | 主要新代码 |
|----|------|-----------|
| Pregel 通道管理 | 8.4 | `graph/pregel/channels.go`(478) + `channels_test.go`(815) |
| Pregel 路由 | 8.5 | `graph/pregel/router.go`(142) + `router_test.go`(269) |
| team 本地链路打通 | 9.x/11.15 | `agent_teams/{harness.go,inprocess_spawn.go,agent/*}` 多文件修复 |
| 流式工具调用聚合 | — | `llm/model_clients/{openai,siliconflow,inference_affinity}`、`schema/tool_call.go`、`stream_utils.go`、`deep_adapter_team.go` |
| web 前端产物链 | 11.15 | `channels/web/{dist.go,embed.go,file_api.go}`、`cmd/uapclaw/cmd.go`、`Makefile`、CI |
| tui 路由迁移 | 11.15 | `gateway/channel_manager/tui/tui_connect.go`、`routing/route_binding.go` |
| 审查修复批次 | 09-27 19 项、skill-itest | `coordination/kernel.go`、`observability/setup.go`、`stream_event_rail.go`、`agent_data.go` |
| 集成测试迁移 | B 类 16 例 / B1-B3 | `tests/integration/swarm/{gateway,workspace,server}`、`testhelpers/pty_linux.go` |

统计：**严重 3 项 / 一般 8 项 / 提示 13 项**。

---

## 一、严重（3 项）

### S-01 团队模式的中断恢复是整条死链：三个桩恒返回 false/nil，而依赖章节早已完成

- Go：`internal/agent_teams/harness.go:301`、`:307`、`:505`、`:510`

```go
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) HasPendingInterrupt() bool { return false }

// TODO(#9.57): deepAgent 类型升级后实现具体逻辑
func (h *TeamHarness) IsPendingInterruptResumeValid(userInput any) bool { return false }

// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) RegisterRail(ctx context.Context, rail any) error { return nil }
func (h *TeamHarness) UnregisterRail(ctx context.Context, rail any) error { return nil }
```

- 消费方是**活的**，不是桩对着桩：

| 消费点 | 后果 |
|--------|------|
| `agent_teams/runtime/manager.go:754` `handleInteractiveInput` | `if !HasPendingInterrupt() → DeliverResult.failure("unsupported_interactive_input")`：团队模式下用户回答 `ask_user_question` 永远被判"不支持" |
| `agent_teams/agent/stream_controller.go:216` `HasPendingInterrupt` | 同上，且 `IsValidInterruptResume` 恒 false |
| `agent_teams/agent/team_agent.go:828` `ResumeInterrupt` | `if !IsValidInterruptResume → return nil`：即使前一步放行，这里也静默什么都不做 |
| `agent_teams/agent/coordination/kernel.go:456` | 恒判"无待处理中断"，邮箱轮询唤醒条件恒成立（语义反了） |

- 依赖是否就绪（本轮审查点 2 的核心）：**已就绪**。
  1. Python 对应实现只依赖"活跃 Agent 会话 + 会话状态读"：`agent_teams/harness.py:199-228`
     `session = self._interrupt_session(); session.get_state(INTERRUPTION_KEY)`。
  2. Go 侧的三个原语都在：`TeamHarness.activeAgentSession`（`harness.go:83`，`prepareAgentSession:457` 已写入）、
     `Session.GetState(key state.StateKey)`（`session/agent.go:254`）、`ToolInterruptHandler.Save(intState, sess)`
     （`single_agent/interrupt/handler.go:130`）——中断状态本来就被写进同一个会话。
  3. `TODO(#9.57)` 声称的"deepAgent 类型升级"在本窗口已发生：`2baccfde` 就在同一文件用
     `h.deepAgent.(*harness.DeepAgent)` 实现了 `Steer/FollowUp`；`DeepAgentInterface` 也已声明
     `AddRail`/`UnregisterRail`（`harness/interfaces/deep_agent.go:96-99`）——`RegisterRail/UnregisterRail`
     只需一行转调即可回填，`TODO` 的免责理由不再成立。
- 影响：team 模式下 HITL（人工确认/问答）恢复路径整条不可用，且失败信息是"unsupported_interactive_input"
  这种误导性文案——排查时会往"通道不支持"方向走，而真实原因是三个空桩。Python 侧同场景可恢复。
- 建议：按 Python 三方法落地（`activeAgentSession.(*session.Session).GetState(...)` 取中断状态、
  比对 `interactive_input` 的 tool_call_id）；`RegisterRail/UnregisterRail` 直接转调
  `deepAgent.AddRail`/`deepAgent.UnregisterRail`。补一条"team 模式 ask_user_question → 回答 → 轮次恢复"的用例，
  现有测试对此零覆盖。

### S-02 `TeamHarness.activeAgentSession` 跨 goroutine 无锁读写，本窗口新增的读点正好落在并发路径上

- Go：`internal/agent_teams/harness.go:83`（声明）、`:457`（写，`prepareAgentSession`）、`:411`（读，`interactLoop`）

```go
// activeAgentSession 活跃的 Agent 会话
// TODO(#9.session): AgentSession 类型
activeAgentSession any          // 非原子、非导出、无锁保护

// 写点（轮次 goroutine，处理每轮开始时）
h.activeAgentSession = agentSess

// 读点（消息处理 goroutine，本窗口 2baccfde 新增）
sess, _ := h.activeAgentSession.(*session.Session)
```

- 调用链是同步直穿到底，没有队列/锁隔离：
  `gateway 消息处理` → `TeamAgent.DeliverInput` → `StreamController.Steer`（`stream_controller.go:253`，
  无锁直接 `harness.Steer`）→ `TeamHarness.Steer` → `interactLoop` 读字段。
  写点则在本轮 `RunStreaming` 的 `prepareAgentSession` 里。两个 goroutine 不同步。
- 影响：①Go 接口值是两个机器字，并发读可能读到撕裂值（语言层面属 data race，UB），类型断言可能拿到
  `nil` 或错类型；②`-race` 在正确时序下会报；③即便没崩，Steer 也会静默丢（见 M-02 的吞错）。
  Python 是单线程 asyncio + 无 await 间隙的赋值，天然无此问题。
- 建议：字段收窄为 `*session.Session`（本文件所有使用点已经只认 `*session.Session`，
  `TODO(#9.session)` 可以顺手销掉），读写走 `sync.RWMutex`；或改由轮次 goroutine 自己接收
  steer 请求并投递到已有的事件队列。
- 附带：`initialPlanModeSeeded`（`ensureInitialPlanMode`）同样是无锁布尔，当前只有轮次 goroutine 读写，暂不构成 race，
  但与 `activeAgentSession` 同属"状态散落在 harness 上"的结构问题。

### S-03 端口分配表无锁并发写：`SpawnPayloadBuilder.memberPortMap` 可从多条重启 goroutine 同时写

- Go：`internal/agent_teams/agent/payload.go:30-33`、`:147-151`

```go
type SpawnPayloadBuilder struct {
	memberPortMap       map[string]int   // 无 mutex
	teammatePortCounter int
}
...
portBase, exists := b.memberPortMap[memberName]
if !exists {
	portBase = basePort + b.teammatePortCounter*portOffset
	b.teammatePortCounter++
	b.memberPortMap[memberName] = portBase   // 并发写 → fatal "concurrent map writes"
}
```

- 可达路径（`spawn_manager.go`）：

```
OnTeammateUnhealthy:315-351  → 每个成员一条 go func()
   └─ RestartTeammate:242 → BuildContextFromDB:381 → configurator.BuildMemberMessagerConfig
                                                      → spawnPayloadBuilder.BuildMemberMessagerConfig
   └─ SpawnTeammate:577   → configurator.BuildSpawnPayload → 同一 builder 同一 map
```

  `RestartTeammate` 全程不持 `m.mu`（只在 `OnTeammateUnhealthy` 里登记 cancel 时短暂加锁）。
  两个成员同时被判不健康，就是两次并发写同一个 map。Python 侧 `payload.py:100-107` 是单线程 asyncio，
  天然串行，Go 把"单线程方法"直接搬成"可被多 goroutine 调用的方法"。
- 影响：`concurrent map writes` 是**不可 recover 的 fatal error**，直接终止进程（不是 panic 能被 defer 接住的级别）。
  同一函数还顺带让端口号可能被重复分配（同名成员走 exists 分支，但 counter 递增与写入之间无原子性）。
- 建议：给 builder 加 `sync.Mutex` 保护 `memberPortMap`+`teammatePortCounter`（对齐 Python 的串行语义即可，
  不需要无锁算法）；补一条 `-race` 下并发 8 成员重启的用例。

---

## 二、一般（8 项）

### M-01 `tool_call` 碎片守卫用"arguments 不是合法 JSON"当判别式，会连合法调用一起丢掉

- Go：`internal/swarm/server/utils/stream_utils.go:176-190`、`internal/swarm/server/adapter/deep_adapter_team.go:1606-1623`

```go
name, _ := toolInfo["name"].(string)
callID, _ := toolInfo["tool_call_id"].(string)
if name == "" && callID == "" { return nil }              // ① 判"碎片"
if args, ok := toolInfo["arguments"].(string); ok && args != "" && !json.Valid([]byte(args)) {
	return nil                                            // ② 判"未闭合"
}
```

- 判别式 ② 的语义不是"增量帧"，而是"参数不是合法 JSON"。`emitToolCall` 的入参是 `before_tool_call` 的
  **完整** ToolCall（`stream_event_rail.go:374`，与 Python `stream_event_rail.py:400-410` 同位置），
  它的 `arguments` 一旦因为模型输出非严格 JSON（单引号、尾逗号、`\x` 转义、被截断）而不合法，
  整条 `chat.tool_call` 事件就被吞掉——而 `tool_result` 照常下发。前端表现为"凭空出现的结果"，无调用卡片。
- Python 侧完全没有这层过滤：`_emit_tool_call`（`stream_event_rail.py:470-485`）原样透传
  `getattr(tool_call, "arguments", {})`，非法 JSON 由下游工具执行层报错，事件本身不丢。
- 根因修复（`17b55eb8` 的 `ToolCallDeltaOptions`）已经把"碎片不聚合"治好了；②这条守卫在修复后是**双重保险**，
  代价却是把"参数非法的合法调用"这一真实场景误杀。建议把 ② 收窄为"增量帧专属"判据
  （例如 `name == "" && callID == ""` 已经覆盖 ①，再叠加 index 变化/无 finish_reason 判定），
  或直接删掉 ② 只留 ①，并在真 LLM 套件里对该分支做正反向断言。
- 附带（可维护性）：这段 14 行逻辑在 `stream_utils.go` 与 `deep_adapter_team.go` 各有一份**手抄副本**
  （团队路径 `parseTypedChunkMap` 与通用路径 `ParseStreamChunk` 本就是两份 switch）。本窗口两次
  （`e1bd34b5`/`169746df`）都是"改一处、再补另一处"，第三次漏改只是时间问题。建议抽到
  `utils.GuardToolCallFrame(toolInfo map[string]any) (map[string]any, bool)` 由两处共用。

### M-02 `Steer`/`FollowUp` 把"投递失败"吞成 nil，上层无法区分"已送达"和"被丢弃"

- Go：`internal/agent_teams/harness.go:402-430`

```go
func (h *TeamHarness) interactLoop(ctx context.Context, content string, steer bool) error {
	d, ok := h.deepAgent.(*harness.DeepAgent)
	if !ok { logger.Warn(...); return nil }         // 失败 → nil
	sess, _ := h.activeAgentSession.(*session.Session)
	if sess == nil { logger.Warn(...); return nil } // 失败 → nil
	...
	return nil
}
```

- 本窗口把"静默丢弃"升级成"记 Warn 后丢弃"，方向对，但返回值语义仍与 Python 不一致：
  `harness.py` 的 `steer/follow_up` 是 `await self._deep_agent.steer(...)`，异常向上抛。
  Go 侧 `DeliverResult` 因此恒为 success，`TeamRuntimeManager.interact` 无法在"输入没进去"时回退
  （例如降级为排队 pendingInput），用户看到的是"消息已发送"，实际丢失。
- 建议：两个失败分支返回具体 error（`errIncompatibleAgent` / `errNoActiveSession`），由调用方决定是否降级；
  日志保留。

### M-03 `ConditionalRouter` 未把 selector 的 nil 结果当错误（fail-open），且 D3 偏差的书面理由与事实不符

- Go：`internal/agentcore/graph/pregel/router.go:119-131`

```go
targets, err := r.selector.Select(ctx, nil)
if err != nil { return nil, err }
return buildTriggerMessages(sourceNode, targets), nil   // targets == nil → 零条消息
```

- Python `router.py:44-46`：`if isinstance(targets, str): targets = [targets]; return [TriggerMessage(...) for to in targets]`
  ——selector 返回 `None` 时 `for ... in None` 直接 `TypeError`，是**fail-closed**；Go 侧 `nil` 被当成"边终止"，
  静默走 END。构图写错（selector 漏返回值）在 Go 里表现为"图提前结束"，在 Python 里表现为异常。
- 同一处偏差 D3（`docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md` §4）把
  "Python 的 `str → [str]` 归一化"上移到调用方，理由写的是"无测试覆盖"。**这一点与事实不符**：
  `agent-core/tests/unit_tests/core/graph/test_pregel.py:131` 与 `:161` 的 `pick_target` 就返回裸字符串 `"D"`，
  并被 `test_conditional_routing`（`:815`）实际执行。结论（Go 用 `[]string` 强类型收窄）可以保留——它让
  单目标 selector 在编译期就被迫显式 `[]string{"D"}`，确实更安全——但**依据必须改**，
  否则后续维护者会按"Python 无此用例"去删掉包装逻辑。
- 附带：`NewConditionalRouter(SelectorFunc(nil))` 会造出非 nil 接口值，绕过 `:120` 的 nil 守卫，
  在 `:123` 调 `f(ctx, nil)` panic。现有用例只测了字面量 `nil`（`router_test.go:262`）。
- 建议：`len(targets) == 0` 时返回 error（对齐 fail-closed）；spec §4 的 D3 依据改为"Go 类型收窄使该分支在编译期消失"；
  `Dispatch` 内对 `r.selector == nil` 增加 `reflect` 无关的空值判定（或让 `SelectorFunc.Select` 对 nil 自守）。

### M-04 集成覆盖率豁免被当成"改到就豁免"的默认手段，理由与仓库事实不符

- 新增 3 条（`.integration-coverage-exempt`，`18be43e0`/`f957de14`）：`stream_utils.go`、`schema/tool_call.go`、`deep_adapter_team.go`
- 文件头写着"能通过集成测试覆盖的代码，不得加入此列表"，但 `stream_utils.go` 的理由是
  "其行为由 swarm/gateway 集成套件间接覆盖"——**这恰恰说明它应该被覆盖而不是豁免**。实测
  `grep -r "ParseStreamChunk\|parseTypedChunkMap" tests/integration/` 只命中 `real_llm/doc.go` 的注释，
  没有任何集成用例真的驱动这两条流式解析路径；`deep_adapter_team.go` 同理（4000+ 行的文件按整文件
  30% 卡，结构性必然不达标）。
- 影响：本窗口两处**生产行为改动**（碎片帧丢弃）全部落在"被豁免 > 无集成覆盖"的区里，回归信号只剩单测 +
  一个 nightly 的真 LLM 套件。豁免本身合理与否可以讨论，但"用豁免掩盖'该测没测'"是治理问题，会逐轮累积。
- 建议：①`deep_adapter_team.go` 这类巨型文件按"改动行覆盖率"而非"整文件覆盖率"判定（门禁脚本已能拿到 diff，
  加一次行级过滤即可）；②为 `chat.tool_call` 的碎片/完整帧两分支补一条 mock 驱动的集成用例
  （MockLLM 可以精确构造 SSE 帧），然后把 `stream_utils.go` 从豁免里删掉。

### M-05 `EnsureMonitor` 的 check-then-act 无 in-flight 去重，并发首条消息可能起两个 monitor 且旧 handler 不 Stop

- Go：`internal/swarm/agents/harness/team/team_manager_monitor.go:204-243`

```go
m.mu.Lock(); existing := m.teamMonitors[sessionID]; m.mu.Unlock()   // ① 只读检查
if existing != nil && existing.IsRunning() { return nil }
mon, err := runtime.GetAgentTeamMonitor(...)                        // ② 无锁窗口
handler := newTeamMonitorHandlerImpl(mon, sessionID)
handler.Start(ctx)                                                  // ③ 启动
m.mu.Lock(); m.teamMonitors[sessionID] = handler; m.mu.Unlock()     // ④ 覆盖
if handler.IsRunning() { go m.consumeMonitorEvents(context.Background(), ...) }
```

- ①→④ 之间没有"占位"标记：同一 session 的两条 chat.send 并发进入（本窗口刚验证过"同 session 不互斥"是设计行为）
  会各自 Start 一个 handler、各自起一条 `consumeMonitorEvents` goroutine（`context.Background()`，长期驻留），
  ④ 用后者覆盖前者，**前者的 goroutine 与事件队列没人 Stop**。
- Python 侧同样有 check-then-act，但（a）单线程只有 await 间隙可插队，（b) `get_monitor` 是池化的，
  同一 (team, session) 返回同一个 monitor，重复 handler 的影响被限制在"多一条消费循环"。
- 建议：在 ① 处直接写入 `pending` 占位（或把 ②③④ 整段放进 `m.mu`，注意 `Start` 内部是否取同一把锁），
  覆盖旧 handler 前显式 `existing.Stop()`。

### M-06 真 LLM 团队用例的断言策略在同一文件内自相矛盾

- `tests/integration/real_llm/team_continuous_dialog_real_test.go`
  - `TestTeamContinuousDialog_真实调用`（`:139`）：把建队/成员/回信全部降级为日志观测，只硬断言"不互斥"——这是对的，
    与"真 LLM 只硬断言确定性契约"的既有结论一致；
  - `TestTeamAutoBuild_自然语言需求触发建队`（`:237`）：仍然硬断言"模型一定先调 `build_team`"（`:322-327`）：

    ```go
    s.True(memberCreated, "自然语言团队需求下，leader 应自行建队并创建成员")
    s.True(hasTeamBuildToolCall(toolNames), "leader 应先调用 build_team 组建团队，实际工具序列=%v", toolNames)
    ```

- 这两条断言本质是**模型行为**，不是代码契约：换模型/换采样温度/上游提示词注入顺序变化都会红。
  同一文件里前一个用例刚刚因为"模型可能不建队"而降级，后一个用例又把同样的不确定性当硬契约。
- 建议：改成条件断言——"若出现建队流程，则第一跳必须是 build_team"（`if len(toolNames) > 0 { ... }`），
  并把"是否建队"记为观测；若确实要锁提示词回归，就把它标成 probe 语义（失败只报 notice），
  并说明依赖固定模型版本。

### M-07 `BindSession` 复用调用方 ctx 的隐患：跨会话的 SessionState 会带着旧 sessionID 去建表

- Go：`internal/agent_teams/agent/session_manager.go:108-133`

```go
newCtx := ctx
if agentteams.SessionStateFromCtx(ctx) == nil {      // 只判"有没有"，不判"是不是同一个"
	newCtx = agentteams.WithSessionState(ctx, m.sessionState)
}
...
teamBackend.DB().CreateCurSessionTables(newCtx)      // 表后缀取自 newCtx 里的 sessionID
```

- 若调用方 ctx 已带**另一个** SessionState（团队内多会话/成员上下文复用很常见），`newCtx == ctx`，
  `CreateCurSessionTables` 会用**旧** state 的 sessionID 拼表名——正是本提交要修的那个 bug 的另一种触发形态。
- 同时 `m.sessionState.SetSessionID(sessionID)`（`:108`）原地改共享指针：多个成员共用一个 SessionState 时
  互相覆盖 sessionID（Python 的 contextvar 是每任务一份，互不影响）。
- 建议：`state := SessionStateFromCtx(ctx); if state == nil || state.GetSessionID() != sessionID { newCtx = WithSessionState(ctx, m.sessionState) }`；
  更彻底的做法是每次 BindSession 新建独立 state 而非原地改。

### M-08 `ForceKill` 等 done 无超时 + 本窗口切断取消传播，成员不响应 cancel 时团队清理会永久阻塞

- Go：`internal/agent_teams/spawn/inprocess_handle.go:158-175`

```go
if h.cancelCtx != nil { h.cancelCtx() }
// 等待协程退出（无超时，对齐 Python await self._task）
if h.done != nil { <-h.done }
```

- `3e133e11` 把成员 runCtx 改成 `WithCancel(context.WithoutCancel(ctx))`（对齐 `create_task` 的 detached 语义，正确），
  代价是**唯一**的停机手段变成 `handle.ForceKill()`，而它 `<-h.done` 无超时。
- 两者的取消保证不对等：Python 的 `task.cancel()` 会在下一个 await 点强制抛出 `CancelledError`，即使被取消方
  没有显式检查；Go 的 `ctx.Done()` 只能通知，节点/LLM 调用不检查就永远不退出。
  一次卡住的 LLM/工具调用 → `CleanupTeammate`（`spawn_manager.go:188`）→ `ForceKill` 永久挂住，
  而清理路径已经被上一轮审查限定在 30s 预算内（M-15），这里会把预算彻底击穿。
- 建议：`ForceKill` 加 `select { case <-h.done: case <-time.After(shutdownTimeout): 记 Error 并返回超时 }`，
  与 `runnerspawn.DefaultShutdownTimeout` 对齐；同时给成员内的关键 await 点补 `ctx.Err()` 检查。

---

## 三、提示（13 项）

### T-01 pregel 8.4/8.5 全是"写了没接"的代码，但注释用现在时描述不存在的调用方

`channels.go`、`router.go` 的所有构造器与接口在生产代码里**零调用方**（`grep NewChannelManager|NewTriggerChannel|
NewBarrierChannel|NewStaticRouter|NewBarrierRouter|NewConditionalRouter`，包外只命中无关的 swarm `channel_manager`）。
按计划（8.3a→8.9b 尚未开工）这属正常的阶段性交付，但两处以现在时写死了不存在的消费者：

- `channels.go:105-107`："唯一使用者是 PregelLoop，它在超步循环所在 goroutine 内串行调用本类型全部方法"——`PregelLoop` 不存在；
- `doc.go:3-8`："均依赖本包……决定下一超步激活哪些节点"。

建议改成"计划由 8.3b 的 PregelLoop 串行调用（尚未接入）"。这类"无锁纪律依赖尚未存在的调用约定"是最容易在
8.3c 接入时被破坏的约定，用将来时写明比现在时更安全。

### T-02 pregel 的两个测试文件里有若干"改坏也不红"的用例

- `channels_test.go:532-538` `Consume未知节点`：在空 manager 上断言就绪集为空，删掉 `Consume` 函数体照样通过；
- `channels_test.go:785-797` `Restore空快照跳过`：删掉 `channels.go:443` 的空快照守卫后，`Restore(空)` 只是把
  `received` 重建成空 map，`IsReady()` 仍为 false，两条断言仍成立——需 spy channel 才能观测"未调用 Restore"；
- `channels_test.go:750-765` `Restore长度不匹配静默`：`:763` 声称"不应调用通道的 Restore"，但无法观测；
  若长度检查退化成 `min` 循环，用例依旧通过（现在仅靠下标越界 panic 间接钉住）；
- `channels_test.go:655-659` `Flush只重算状态改变的节点`：断言的是"通道级 Restore 不影响管理器就绪集"，属于恒真；
- `router_test.go:261-269` `selector为nil`：只测字面量 nil，未覆盖 M-03 说的 typed-nil。

覆盖率 100%（语句）成立，但语句覆盖不等于分支证据。建议对上述 5 处换成 spy/计数断言。

### T-03 通道快照的空形状不对称，8.9b 序列化会踩到

`TriggerChannel.Snapshot()` 空时返回 `Messages: nil`（JSON `null`），`BarrierChannel.Snapshot()` 空时返回
`Received: []string{}`（JSON `[]`）。`ChannelManager.Snapshot` 虽然会跳过"两字段皆空"的节点，但
"Trigger 空 + Barrier 非空"的混合节点里，`null` 会原样落进检查点。8.9b 反序列化时 `null` 与 `[]` 都会得到空切片，
功能上无碍，但这是未登记的第三处偏差，建议写进 spec 的偏差表或统一成 `[]`。

### T-04 已回填的代码上还挂着"占位"注释，会误导下一轮审查

- `internal/agent_teams/spawn/inprocess_spawn.go:66`："goroutine 内部调用 Runner.RunAgentTeam，当前 TODO(#9.85) 占位"
  ——实际 `:122-150` 已通过 `runTeamMemberFunc` 完整回填（同函数体注释里就写着"回填（#9.85）"）；
- `internal/agent_teams/harness.go:337`：`Steer` 上方仍留 `// TODO(#9.57): deepAgent 类型升级后实现`，而函数体已实现。

### T-05 `Gate 门禁` 的 push 基准依赖 `github.event.before`，分支首次 push 会退化成 `HEAD~1`

`5972e008` 的修复方向正确（显式失败替代静默跳过），但两条降级链叠加后语义要写清：
`before` 为全 0 → 回退 `HEAD~1` → 若也取不到才 `::error::`。一次 push 多个提交且触发首次 push 时，
门禁只检查最后一个提交的改动。当前无实际风险（都是小步提交），建议在注释里点明这个盲区。

### T-06 CI 只让 `build` job 构建前端，其余 3 个 job 仍嵌占位 `index.html`

`d64de00c` 在 build job 里加了 `npm ci && npm run build`，但 lint/unit/integration 三个 job 仍是
`fetch-depth` 浅克隆 + 直接 `go build`：它们编译进去的是 `dist/index.html` 占位页。
①任何"断言首页内容"的用例在这三个 job 里会看到占位文案；②build job 现在强依赖 npm registry，
一次 registry 抖动就整条流水线红，且没有依赖缓存/超时配置之外的重试。
建议：把前端构建产物做成 job artifact，或至少给 `npm ci` 加 `--prefer-offline` + 明确超时。

### T-07 `file_api` 的路径解析在"叶子不存在"时退化为 `Clean`，不解祖先符号链接

`internal/swarm/channels/web/file_api.go:485-500`：`EvalSymlinks` 失败即 `resolved = filepath.Clean(full)`，
于是一个"已存在符号链接祖先 + 不存在叶子"的路径不会被解析到链接目标，只做字符串清理。
Python 的 `Path.resolve()`（`app_web.py:610`）在 `strict=False` 下会解掉**已存在前缀**的符号链接。
当前不可利用（`GET` 要求文件存在，`POST` 还额外要求 `.md` 且必须已存在），但语义与 Python 不一致；
若将来放开"新建文件"，这里会变成一条逃逸路径。建议改成"逐段解析已存在前缀"，或直接 `EvalSymlinks` 父目录。

### T-08 `tui.Install` 的类型断言失败静默 return

`gateway/channel_manager/tui/tui_connect.go:240-244`：

```go
rs, ok := server.(RouteServer)
if !ok { return }     // 无日志
```

迁移后 `RouteServer` 是 `ResponseSender`（7 参）+ `RegisterLocalHandler` 的组合接口，任一处签名漂移都会让
断言失败；失败后 5 个 chat/history 本地 handler 全部不注册，消息走转发路径，现象是"ack 变慢/行为变了"，
日志里一个字都没有。建议加 Warn 并打出 `%T`。

### T-09 `SendResFrame` 的 7 个位置参数 + 布尔开关

`gateway/res_frame.go` 与 `tui.ResponseSender` 都是
`(ws, reqID, ok, payload, errMsg, code, includePayload)`。`includePayload` 是典型的布尔位参数，
调用方读起来要靠数位置。建议收成 `ResFrame{ReqID, OK, Payload, Code, ErrMsg}` + 一个 `OmitPayload` 变体，
接口本来就只在两处实现。

### T-10 `EnsureMonitor` 在"运行时未激活"时返回 nil，调用方无法区分

`team_manager_monitor.go:219-223`：`mon == nil` 记 Warn 后 `return nil`。`OnRuntimeReady`/adapter 两处
都只看 error，于是"监控没起来"与"监控已启动"在上层完全等价。建议返回哨兵 error（如 `errNoActiveRuntime`），
由调用方决定是重试还是接受。

### T-11 真 LLM 用例里两条断言由前一条蕴含

`team_continuous_dialog_real_test.go:225-229`：

```go
s.True(firstAccepted, ...)
s.True(secondSent, ...)            // firstAccepted 为真时必然已置 true
s.True(secondAccepted, ...)
s.False(secondSentAt.IsZero(), ...) // secondSent 为真时必然非零
```

冗余断言会在失败时给出误导性的"第二条没发出"，建议删掉第 2/4 条或改成对 `secondSentAt` 与首帧时间差的断言。

### T-12 spec/plan 里列的测试名与代码不一致

- `docs/superpowers/specs/2026-11-01-pregel-channels-8.4-design.md:351` 列 `TestChannelManager_Buffer引用语义`，
  该用例已被 `ab93f705`（Buffer 改返回副本）替换为 `Buffer返回副本`（`channels_test.go:590`），spec 未同步；
- `docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md:308` 列 `TestRouter_接口断言`，该用例不存在
  （只有 `router.go:64-71` 的编译期 `var _`）。

### T-13 `agent-data.json` 编码与 Python 差一个尾换行

`server/runtime/skill/agent_data.go:158-161` 用 `json.Encoder.Encode` 写出，末尾多一个 `\n`；
Python `app_web.py:127-130` 的 `write_text(json.dumps(...))` 没有。前端 JSON 解析无感，
但如果将来做 golden 对拍会立刻红。按"无消费者的字节一致性不改"的既有结论可以不动，登记为已知差异即可。

---

## 汇总与复核建议

| 级别 | 编号 | 一句话 |
|------|------|--------|
| 严重 | S-01 | team 中断恢复三桩恒 false/nil，依赖已就绪，HITL 恢复整条死链 |
| 严重 | S-02 | `activeAgentSession` 跨 goroutine 无锁读写（新增读点），且字段仍是 `any` |
| 严重 | S-03 | 端口分配 map 可从多 goroutine 并发写 → fatal concurrent map writes |
| 一般 | M-01 | tool_call 守卫用"非合法 JSON"当判别式，会丢合法调用；两处手抄副本 |
| 一般 | M-02 | Steer/FollowUp 失败吞成 nil，上层无法回退 |
| 一般 | M-03 | ConditionalRouter 对 nil 目标 fail-open；D3 偏差理由与 Python 测试事实不符 |
| 一般 | M-04 | 集成覆盖率豁免被当默认手段，`stream_utils.go` 的豁免理由自相矛盾 |
| 一般 | M-05 | EnsureMonitor check-then-act 无去重，旧 handler/消费 goroutine 泄漏 |
| 一般 | M-06 | 同一真 LLM 文件内断言策略自相矛盾（观测 vs 硬断言模型行为） |
| 一般 | M-07 | BindSession 复用调用方 SessionState，跨会话建表可能用错 sessionID |
| 一般 | M-08 | ForceKill 无超时 + 取消传播已切断，成员不响应 cancel 时清理永久阻塞 |
| 提示 | T-01..T-13 | 未接入代码的现在时注释、假断言用例、快照形状不对称、过时 TODO、门禁盲区、CI 前端不对称、路径解析语义、静默断言失败、参数结构、nil 语义丢失、冗余断言、spec 漂移、尾换行 |

建议复核顺序：

1. **S-01**（功能死链，且有 Python 一比一参照，改动小）→ **S-03**（一行锁，避免进程级 fatal）→ **S-02**（结构改动，与 S-01 同文件可一并联调）；
2. **M-01/M-02**（都在流式与 team 输入链上，直接决定"工具调用是否可见""消息是否真送达"）；
3. **M-04**（治理类，越晚越难收）：巨型文件的整文件覆盖率门禁应改为改动行覆盖率，否则后续每个大文件改动都会走豁免。  

已验证**没有问题**的部分（供下一轮减负）：

- `pregel/channels.go` 的 `Flush`/`Consume`/`Snapshot`/`Restore` 与 `channels.py:39-101` 语义逐条一致
  （错误前置、buffer 不清、就绪重算口径、三类静默）；
- `BarrierChannel` 的 CNF 判定、组内去重、`barrier:(A|B)&C->name` 合成键与 `channels.py:155-172` 一致；
- `ToolCallDeltaOptions` 的根因修复与 `openai_model_client.py:745-751` 一致，三客户端同步改动无遗漏；
- `agent_configurator` 的 messager 创建与 `agent_configurator.py:219-224` 一致（含 `node_id` 覆盖）；
- `agent_data.go` 的目录/键/展示路径/去重/排序与 `app_web.py:82-131` 一致（仅尾换行差异，见 T-13）；
- `7346d03e` 五项重点修复（todo 工具懒建、observability 逐条 Off、KernelHost 取 blueprint、
  ExternalMemoryRail 改 configBase、entity 阈值 0.3）经逐项对照 Python 均正确；
- tui 路由迁移后 `ForwardReqMethods`(80) / `ForwardNoLocalHandlerMethods`(75) 计数与集合未变。
