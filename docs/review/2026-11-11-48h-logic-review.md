# 2026-11-11 48 小时上库质量审查

> 审查范围：`51663d344 .. HEAD(ff53815ff)`（2026-11-09 04:00 ~ 2026-11-11 03:02，**306 个非合并提交**；其中含 Go 改动 250 个、纯文档 56 个）
> 代码规模：`.go` 文件 505 个改动（+63,657 / −1,518），其中测试与集成用例 317 个文件（+35,228 / −548），生产代码 +28,429 行
> 主要投入方向（按文件改动数）：`agent_teams/harness`（125）· `permissions/auto_permission`（106）· `agent_teams/agent`（76）· `swarm/server/adapter`（70）· `harness_protocol`（54）· `agentcore/kv_cache`（35+）
> 审查对象：提交原因与实现正确性；对齐 Python（产品层 `jiuwenswarm` / 库层 `openjiuwen`）；桩与回填完整性；层级与步骤归属；稳定性、性能、可维护性、并发安全
> 对照源码：`/home/opensource/agent-core/openjiuwen/**`、`/home/opensource/jiuwenswarm-develop/jiuwenswarm/**`
> 下文 `文件:行号` 以 HEAD `ff53815ff` 为准
> **只读审查，未修改任何代码**

---

## 零、结论摘要

| 级别 | 数量 | 一句话 |
|---|---|---|
| 严重 | 2 | 「会话分身 + KV 亲和 + 主动推荐」这一整层，生产装配端完全没接上，功能等于没上线；团队会话的"停止/取消"和"删除"仍然停在空桩上，而后端能力早就写好了 |
| 一般 | 8 | 会话分叉（fork）出来的聊天记录会丢工具调用和思考过程；异步后台任务失败时模型看不到；测试里 mock 的东西和生产代码根本对不上，所以测试全绿不代表功能可用；还有若干并发/顺序隐患 |
| 提示 | 7 | 注释与代码对不上、`doc.go` 文件树缺条目、按规则 8 该删没删、提交说明引用不存在的文件等 |

**这批做得好的地方**（先说清楚，避免只看问题）：

1. **`9f60150cc` 是本轮质量最高的一笔**。它修的是"后台任务取消"和"任务刚好跑完"撞车的问题：原来取消路径"先改状态、解锁、再叫醒等待方"，中间被后台任务抢先标成"已完成"，等待方醒来看到的是 completed 而不是 cancelled。改法是把"删任务 + 关终态信号"的顺序固定下来，并把"改状态 + 叫醒"放进同一把锁——这是正确的时序修法，不是"多跑几次就过了"。
2. **`72d4c9e35` 承认了一句很关键的话**：上游 Python 是单线程，`get_task` 直接返回活对象没问题；Go 里有并发写，同一个对象就变成数据竞争。所以改返回浅拷贝，并且**明确登记为语言层偏差**而不是假装"语义等价"。这种"差异摆到台面上"的做法正是规则 12.3 要求的样子。
3. **`6c9502124` 是一个真正的"发现错误登记"**：它发现 `noopLifecycleJournal` 的注释引用的编号是错的（R-4 其实是执行 owner 表，跟删除可恢复性无关），并把恒 `false` 的 `RecoveringDelete` 接上真实现。**"删除中途崩溃、重启后认不出正在删的会话"这类问题，只有这种逐行回读上游才找得到。**
4. **`ca16ab971` 找到了"插座建好但没人插"的系统性问题**：五个登记 setter 生产调用数为 0，其中四个导致功能静默失效（只读快速路径永久 fail-closed、code 控制路由永不命中）。用观察者 + 装配期自动登记而不是手工调 setter，依赖方向也对。
5. **`08838a45f` 自曝两处"类型断言永远失败"**：`resolvePlanning` 恒 false、`generationOptions` 恒零值。这是本轮最容易漏的一类 bug（编译通过、测试通过、功能静默无效），能自己查出来并写清后果，质量意识在线。
6. **`05fde4fb2`（knownKeys 漏字段）** 与 **`4f995a277`（上传/下载 peek-ahead 覆盖待写分块）** 都属于"先证明旧代码确实错、再改"的类型，改法保守且对症。

**这批最需要警惕的两个模式**：

- **"模块建好了，装配端没接"**：`10.3.30`（会话分身 + KV 亲和 + 主动推荐）13 个提交、整层代码都在，却没有任何生产代码调用 `SetKVCacheAffinityConfig`，`resolveAffinityConfig` 直接 `return nil`。**功能等于不存在，但计划表已经打 ✅。**
- **"上一轮报的严重问题没人跟"**：`2026-11-10` 审查的 S-01（团队取消空桩）、S-02（删除时团队 quiesce/dispose 空桩）在本轮 306 个提交里**一次都没被碰过**，代码行号都没变。

---

## 一、严重（S）

### S-01 会话分身 / KV 亲和 / 主动推荐整层：生产端零接线，功能等于没上线

**这是哪条流程**：用户聊天 → 模型调用 `ReActAgent.reactModelCall`（`internal/agentcore/single_agent/agents/react_model_call.go:189`）→ 判断"要不要带 KV 亲和参数" → 同时 `PromptSuggestionForkRail` 在模型调用后抄一份父请求参数（`internal/swarm/agents/harness/common/recommendation/fork_rail.go:110-143`）→ 供"会话分身/主动推荐"重放请求时复用，保证父子两次请求的缓存键一致。

**缺陷本身**：这条链上"开关"和"参数"两头都是空的。

```go
// internal/swarm/agents/harness/common/recommendation/fork_rail.go:222-225
// resolveAffinityConfig 从 agent 取亲和开关。
func resolveAffinityConfig(agent sainterfaces.BaseAgent) *kv_cache.KVCacheAffinityConfig {
	return nil        // ← 恒 nil，注释写着"从 agent 取"，实际不取
}
```

1. **父请求侧**：`react_model_call.go:189` 拿的是 `a.kvCacheAffinityConfig()`，而它返回字段 `kvCacheAffinityCfg`；全仓 `grep SetKVCacheAffinityConfig` 只在 `react_agent.go:165` 有定义、**生产调用 0 处**（注释还写着"由 DeepAgent 配置装配时调用"——那个装配代码不存在）。
2. **重放侧**：`resolveAffinityConfig` 恒 `return nil`，`ResolveRuntime`（`internal/agentcore/kv_cache/hook.go:115`）里 `enable := cfg != nil && cfg.EnableKVCacheAffinity` 恒 `false`，于是 `ResolveLineage` 直接返回 `("","")`，`BuildInvokeKwargs` 从不被调用。
3. **上游对照**：Python 在 `interface_deep.py:11770` 构造 `kv_cache_affinity_config = _deep_agent_kv_cache_affinity_config(config_base, model)` 并 `:11781` 传给 agent，配套 `common/kv_cache_affinity_config.py:108-137` 的 `build_kv_cache_affinity_config`（读 `extensions.kv_cache.mode=affinity`，不满足则 fail-closed 降级并 Warn）。**Go 侧这两个函数都不存在**（`grep KVCacheAffinity internal/` 只有 rail 与 hook 自身）。

**影响**：
- 亲和永远关闭 → **跨轮 prompt cache 命中率没有任何改善**，而"10.3.30"这个任务的全部目的就是这个。
- 更隐蔽的是**父子一致性的论证失效**：`fork_rail_test.go:84-91` 的用例直接给 `MainCallParameters` 传 `cfg = &KVCacheAffinityConfig{EnableKVCacheAffinity: true}`，断言"父子 kwargs 一致"—— **生产里 cfg 恒 nil，这个用例证明的是一条永不执行的路径**。

**建议**：补 `buildKVCacheAffinityConfig`（对齐 `kv_cache_affinity_config.py:108-137`，含 mode 判定与 fail-closed Warn）+ 在 DeepAgent/adapter 装配处调用 `SetKVCacheAffinityConfig`；`resolveAffinityConfig` 改为从 agent 取（加 `AffinityConfig() *kv_cache.KVCacheAffinityConfig` 窄接口），与父请求侧用同一份配置。补完前，**10.3.30 不应标 ✅**。

---

### S-02 团队会话的"停止"与"删除"仍是空桩，而依赖的 TeamManager 能力早已就绪

**这是哪条流程**（两条）：

1. 用户点"停止" → `UapClawSwarm.ProcessInterrupt`（`internal/swarm/server/runtime/interface.go:581`）→ `isTeamMode` → `processTeamInterrupt`（`:887`）→ `case "cancel"`（`:933`）。
2. `session.delete` → `RuntimeSessionProvisioner` → 团队分支 `QuiesceForDelete` / `DisposeAfterResourceRelease`（`internal/swarm/server/session_stubs.go:341/351`）。

**缺陷本身**：

**(a) 团队取消**（`interface.go:933-951`）：

```go
case "cancel":
    // ⤵️ 10.6.19-23: team_manager.cancel_session_runtime(sessionID, reason)
    cancelled := false
    _, _ = uc.coordinator.CancelExecution(sessionID, swsession.CancelExecutionOptions{
        RequestID: &sessionID,   // ← 仍然把 sessionID 当 requestID
    })
    message := "团队当前执行已结束"
    if !cancelled {
        message = "当前没有可取消的团队任务"
    }
```

- `cancelled` 恒 `false` → 前端永远收到"当前没有可取消的团队任务"。
- `coordinator.CancelExecution` 用 sessionID 当 requestID，匹配不到任何执行记录（上一轮同一处问题，未修）。

**(b) 非团队"停止"的收尾也是空桩**（`interface.go:968-979`）：`cancelTeamWorkForSession` 只打日志 + `return false`，注释写"awaiting TeamManager backfill (10.6.19-23)"。它被 `:612` 在**每一次**非团队 cancel 时调用。

**(c) 删除团队会话**（`session_stubs.go:341-360`）：`QuiesceForDelete` / `DisposeAfterResourceRelease` 都是 `logger.Debug + return nil`。

**依赖其实早就有了**（这正是"该回填却没回填"的原型）：

| 桩 | 需要的能力 | 现状 |
|---|---|---|
| `cancelTeamWorkForSession` | `TeamManager.TerminateSessionRuntime` | `team_manager_lifecycle.go:304` 已实现，生产调用 **0** |
| 团队 `cancel` | `TeamManager.CancelSessionRuntime` | `team_manager_lifecycle.go:359` 已实现，生产调用 **0** |
| 删除团队会话 | `TeamManager.DeleteSessionRuntime` / `DestroyTeam` | `team_manager_lifecycle.go:521 / :268` 已实现 |
| 判定"是否还有团队任务" | `HasStreamTask`（`team_manager_monitor.go:43`）、`ActiveSessionID`（`team_manager.go:306`）、`PendingSessionID`（`:318`） | 全部已实现 |
| 文件头 | `interface.go:18` 已 `import ".../agents/harness/team"` | 且 `:903` 的 `pause` 分支已经在用 `team.GetTeamManager(...)` + `tm.PauseSessionRuntime(...)` |

也就是说：**同一个文件、同一个函数，`pause` 分支已经接上了真实现，`cancel` 分支还写着"依赖 TeamManager"**。`10.6.19`（TeamManager 主体）在计划里已是 ✅，10.6.21/10.6.22 与这三处无关。

**影响**：
- 团队模式（leader/teammate）下用户点"停止" → 后端继续跑、继续烧模型与工具、前端还被告知"没有可取消的任务"。这是主链路按钮级失效。
- 删除团队会话 → 不 quiesce、不 dispose → 团队执行体在后台滞留，**下次同 sessionID 再来会撞上旧状态**。

**建议**：`case "cancel"` 直接 `tm.CancelSessionRuntime`；`cancelTeamWorkForSession` 改调 `TerminateSessionRuntime`；`QuiesceForDelete`/`DisposeAfterResourceRelease` 转调 `teamExecutionController` 的真实现。三处合计不超过 60 行，且都已有可复用的原生方法，**不应再"等下一章"**。

---

## 二、一般（M）

### M-01 「会话分叉」出来的聊天记录丢失工具调用与思考过程

**这是哪条流程**：用户对一个会话点"分叉" → `sessionops.CopySessionContextForceHistory`（`internal/swarm/agents/harness/common/sessionops/session_ops.go:1039`）→ 读目标会话已复制的 `history.jsonl` → `buildContextMessagesFromHistory`（`:1131`）重建消息序列 → 种进新会话的上下文引擎。

**缺陷本身**（`session_ops.go:1131-1155`）：只处理 `chat.final` 与 `user` 两类记录，其余全部计入 `skipped`：

```go
default:
    // reasoning 分片 / tool_call / tool_result / usage / 交互事件：
    // 完整状态机尚未迁移，本轮跳过。
    skipped++
```

上游是 `session_ops_service.py:1211-1508` 约 297 行的状态机（reasoning 缓冲、tool_call 聚合、tool_result 配对、`chat.final` 收口、未解析 tool_call 清理）。注释已如实登记为 ⤵️ 10.3.31f，**这一点值得肯定**。

**但影响被低估了**：分叉出来的会话，历史里"用户问 → 模型调工具 → 工具返回 → 模型回答"会被压平成"用户问 → 模型答"，**中间的工具调用与结果全部消失**。新会话如果接着处理同一件事，模型会以为之前没执行过那些工具，可能重复执行写文件/跑命令。

**建议**：这条路径属于产品可见功能（分叉是入口按钮），不是"零消费者"的死代码。在 10.3.31f 落地前，建议要么在分叉入口显式提示"历史中工具调用不会带过去"，要么把状态集扩大到 `tool_call`/`tool_result` 配对（约 80 行，不依赖 10.3.31f 其余部分）。

---

### M-02 异步后台任务失败时，结果是"静默的"——模型不会被告知

**这是哪条流程**：模型调用一个异步工具（如长跑的搜索）→ `AsyncToolRuntime.Launch`（`internal/agent_teams/harness/async_tools.go:193`）起 goroutine → 后台跑完后 `reportCompleted`/`reportFailure` 把文案注入下一轮。

**缺陷本身**：取消路径不注入。

```go
case errors.Is(err, context.Canceled):
    rt.markError(taskID, rec, "cancelled")     // 只改状态，不注入
case err == nil && ctx.Err() != nil:
    rt.markError(taskID, rec, "cancelled")     // 同上
```

而 `run` 的 **panic 分支**反而会注入（`reportFailure` → `injectText`）。上游 `async_tools.py:227-264` 的 `_run` 里，`CancelledError` 分支是 `_mark_error` **之后 `raise`**，让异常继续冒泡（由上层 task 的完成回调处理）；Go 侧吞掉了，等于**"任务被取消"这个事实既不注入也不上报**。

叠加放大的是 `CancelAll`（`async_tools.go:295-305`）：它遍历 `rt.tasks` 只调 `task.cancel()`，**不置终态、不 close 事件、不删条目**。而 `NativeHarness.Stop`（`native_harness.go:377-384`）正是用它。于是：

- `Stop` 时被取消的任务，`registry` 里永远停在 `running`，`Wait` 会等满超时；
- 如果上层有"等所有异步任务收尾"的逻辑，会白等到超时。

**建议**：
1. 取消分支补 `injectText`（文案对齐 `async_tool.failed`，或新增"已取消"键），至少让模型知道这个后台任务不会再回来。
2. `CancelAll` 内联 `Cancel` 的语义（置 `cancelled` 终态 + 关事件 + 删 tasks），不要只 `cancel()`。这同时修掉 `Stop` 后 `registry` 残留 `running` 的问题。

---

### M-03 测试里断言的东西和生产代码里的东西不是同一个——"测试全绿"不成立

本轮有 3 处"测试通过与生产可用度脱节"，性质相同，值得合并成一条讲。

**(a) 父子缓存参数一致性用例**（`internal/swarm/agents/harness/common/recommendation/fork_rail_test.go:78-92`）：直接传 `cfg = &KVCacheAffinityConfig{EnableKVCacheAffinity: true}`，而生产 `resolveAffinityConfig` 恒 `nil`（见 S-01）。用例证明的是一条生产永不进入的分支。

**(b) `resolvePlanning` 的三态用例**只覆盖"agent 实现了 `LoadState`"这一条路；生产里 `PromptSuggestionForkRail` 的 `cbc.Agent()` 是 `single_agent/agents` 的 `ReActAgent`，是否实现 `LoadState(SessionFacade) *hschema.DeepAgentState` 没有端到端用例兜着（`08838a45f` 修了断言永远失败的问题，但没加"接到真实 ReActAgent 上"的用例）。

**(c) `_build_context_messages_from_history` 的重建用例**只喂 `user`/`chat.final` 两类记录，正好绕过 M-01 的丢失。

**影响**：这类用例给的是"假绿"——它们让人相信功能可用，实际不可用。本轮 306 个提交里，**功能"看起来完成"与"实际接线"之间的落差，主要就是靠这种用例掩盖过去的**。

**建议**：给"有开关/有装配"的能力，至少保留一条从**真实构造入口**（`BuildTeamHarness` / `CreateAdapter` / `DeepAgent.ConfigureDeepConfig`）走下来的用例；mock 只允许替换网络端点或时钟（规则 7 第二层同理）。

---

### M-04 `TeamHarness.Stop` 之后再 `Start`，重建出来的 native 不再持有异步任务运行时

**这是哪条流程**：团队成员一轮跑完 → `TeamHarness.Stop`（`internal/agent_teams/harness/team_harness.go:439`）→ 下一轮 `Start`（`:398`）。

**缺陷本身**：`Start` 在 `native.State() == Terminated` 时重建：

```go
rebuilt, err := NewNativeHarness(h.agentSpec, h.buildCtx)
if err != nil { ... }
h.native = rebuilt
h.deepAgent = rebuilt.DeepAgent()
```

而 `SetAsyncToolOutputDirResolver`（`native_harness.go:245`）是**构造之后**单独注入的，`Start` 的重建分支不重放这一步。后果：第二轮起 `h.asyncRuntime.outputDirResolver == nil`，`maybeSpill`（`async_tools.go:490-495`）直接 `return text` —— **大结果不再落盘，全部塞回模型上下文**，长任务很快把上下文撑爆。

**触发条件**：只在"25.12 注入了 spill resolver"之后才显现，属于埋伏型缺陷；`team_harness_rebuild_test.go` 只断言了"重建了新对象"，没断言 resolver。

**建议**：`Start` 重建时把 `asyncOutputDirResolver` 一并带过去（或在 `NewNativeHarness` 里从 spec/buildCtx 解析），并在重建用例里断言 `outputDirResolver != nil`。

---

### M-05 后台任务 goroutine 用的是 `context.Background()`，取消不继承任何上游

`async_tools.go:194`：`ctx, cancel := context.WithCancel(context.Background())`。

结果是后台任务**只受 `Cancel`/`CancelAll` 控制**，不随轮次 ctx、不随会话关闭而停。`:384` 的 `err == nil && ctx.Err() != nil` 分支本意是"factory 忽略了取消仍返回成功"，但 ctx 是自建的，几乎不可能非 nil。上游 asyncio task 也没有"随轮次取消"语义（这点不算偏差），但 Go 侧多了一个**进程退出前任务可能仍在跑**的窗口，且 `NativeHarness.Stop` 之后 `registry` 里还留着 `running`（见 M-02）。

**建议**：至少把 `Stop` 之后的所有任务统一置终态并清 `tasks`（见 M-02 第 2 点）；是否继承轮次 ctx 建议按上游语义保守不动，但要在注释里写清"为什么不是 Background"的反面理由。

---

### M-06 goal 轮"无限重试"缺陷只登记在测试注释里，计划表里没有与之对应的跟踪项

**这是哪条流程**：goal 模式下一轮结束 → `TaskCompletionRail.doGoalAfterIteration`（`internal/agentcore/harness/rails/task_completion.go:687`）→ `manager.ApplyAssessment(currentGoalID, ...)`。

**事实**：`628d8a9de`（goal 真实接棒用例）在注释里明确写下：

```
DIAG assess attempt=1 status=blocked
DIAG apply MISMATCH want goal= rev=0 got goal=d4b4bd270c8d rev=0 status=active
PROBE record status=active attempt=214
... max_attempts / token_budget 全部失效
```

即 `currentGoalID` 为空 → `ApplyAssessment` 静默返回 → 状态永远 ACTIVE → 监督器无限重派。**这是生产缺陷，不是测试问题**，且严重度高于本文件的多数"一般"项。

**为什么放在"一般"而不是"严重"**：它被如实登记了，并且本轮的用例用"有界收集"避开了它，没有造成假绿；但它**只在测试文件注释与 plan 文件里出现，`IMPLEMENTATION_PLAN.md` 的 10.3.27 行仍是 ✅**，等于没有跟踪项。

**建议**：把该缺陷提为独立条目登记进计划（或单开一节），并补一条"checkpoint 恢复路径下 `run_context.goal_id` 是否落盘"的用例——这条路径很可能是根因（`deep_agent.go:2785-2815` 只从 `v["run"].(map)["context"]` 解析 `goal_id`）。

---

### M-07 删除会话链路里还有三个"看着有、其实没有"的钩子

`internal/swarm/server/session_stubs.go:104-109`：

```go
TrajectoryHook:          noopTrajectoryHook{},
TeamBinding:             noopTeamBindingUnbinder{},
...
SubagentStopper:         noopSubagentRuntimeStopper{},
```

- `TeamBinding`（解绑团队绑定）：`team_binding_store` 已存在，`TeamManager` 也有删除能力（S-02 表），属于"依赖已有但没接"。
- `SubagentStopper`：25.1c 之后子代理运行时已由 harness 管理，可接 `NativeHarness.Stop`/`StopSubagentRuntimes`。
- `TrajectoryHook`：领域二十二未开工，这个可以留。

**影响**：删除会话后，团队绑定表与子代理运行时残留；下次同 ID 会话可能读到旧绑定。

**建议**：前两项按 S-02 一并接上；`noopTrajectoryHook` 保持并保留注释。

---

### M-08 一处函数内"先执行再判空"，与上游步骤顺序不一致

`internal/swarm/server/adapter/deep_adapter_interaction.go:237-243`（`openInteractionWithGoalOp`）：

```go
interactionStream, attachErr := d.instance.AttachOutput(ctx)   // 先真的去抢租约
if attachErr != nil { ... }
if interactionStream != nil {
    d.interactionOutputHandoff = newOutputHandoff(d.instance, interactionStream)
}
```

上游 `interface_deep.py:19827` 同样是 `attach_output()` 在前，**这一步对齐没问题**。问题在于随后：`control` 返回 `goal_confirm_required`/`goal_error` 时（`:258-274`），Go 走 `closeInteractionStream` 释放租约，而上游 `:19841-19843` 还多做一步"把 `run_failure` 记进运行失败态"。Go 侧没有这一步，导致**前端拿到 `goal_error`，但后端认为本轮没有失败**——下一轮的状态判定会与前端不一致。

**建议**：补 `run_failure` 的等价处理（Go 侧对应的失败态字段），或在 spec 中明确登记"不记录 run_failure"的理由与影响面。

---

## 三、提示（T）

### T-01 `placeholders.go` 里三个接口把"后续章节"写错成 15.3

`internal/agentcore/harness_protocol/placeholders.go:99-114`：`HarnessHookDispatcher` / `HarnessInteractionHandler` 的注释写「⤵️ 15.3 回填」，但 `IMPLEMENTATION_PLAN.md:966` 的 15.3 只覆盖 `interactions.py` / `hooks.py` 的协议值对象，**这两个接口的方法集（钩子分发、交互应答）在 Python 侧不属于 `harness_protocol`**（`hooks.py` 是 dataclass 契约，dispatcher 在 `harness_providers/base.py`）。指向 15.5 更准确。

### T-02 `deep_adapter_interaction.go` 的分节顺序违规

`0cd025f35` 在该文件新增 `interactionOpenResult` 结构体时，把它放在了「导出函数」段之后的「非导出函数」段里，形成「导出函数 → 非导出函数 → 结构体 → 非导出函数 → 常量 → 非导出函数」六段交叉，违反 `go-code-conventions.md` 规范 2（结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数）。

同一文件在 `1af3cfa0a`（合并 main）前后还有一批 `any` 收窄改动，说明这个文件的段结构已经漂移过多次，建议一次重排并在 `doc.go` 里固定该文件的分段约定，避免下次再乱。

> 说明：为核验"是否只影响分节顺序、不影响行为"，本轮按规范顺序做过一次临时重排并 `go build`/`go vet`/`gofmt` 全部通过（纯声明搬移，无逻辑改动），**核验后已 `git checkout` 还原**，仓库无残留修改。

### T-03 15.2 的 `ModelRequestEvent` 有两处"Go 专有"注释可以再收紧

- `events.go:706`：注释写「Python: events.py:414-416（any(identifier == "") 拒绝）在 Go 恒不成立」。这句是对的，但 Go 把 `str|None` 收成 `string` 之后，**"未提供"与"提供了空串"在线上就不可区分了**。15.4 做 codec 时必须明确：空串一律序列化为"字段缺席"，否则与 Python 对拍不过。
- `events.go:722-728`：`opts.Event == nil` 的拒绝是 Go 专有防御，注释已标"Python 侧无对应实现（Go 专有防御）"，这点符合规范，建议在 15.4 的对拍清单里也列一行，避免对拍时被判"多出行为"。

### T-04 `doc.go` 文件树与"包功能概述"都有缺口

- `internal/agentcore/harness_protocol/doc.go` 的文件树与类型索引**没有列出 `placeholders.go` 里的三个接口**（`HarnessCheckpointSink` 列了，另两个在核心类型索引里也没出现），而规则 1 要求"新增文件必须同步条目"。
- `internal/agent_teams/harness/doc.go` 的第 5 行「文件目录：」与上一行之间缺空行，`godoc` 不会把它当段落（格式细节，规范 1/规则 1 的书写约定）。

### T-05 按规则 8 该删但还在的"半残留"

- `internal/agentcore/single_agent/agents/react_agent.go:164-167`：`SetKVCacheAffinityConfig` 注释写"由 DeepAgent 配置装配时调用"，**该调用方不存在**（见 S-01）。要么补装配，要么按规则 8 删掉并让 `resolveAffinityConfig` 从别处取。
- `internal/swarm/server/runtime/interface.go:570-572`：`⤵️ 10.6.19-23: Team 后续请求绕过 Session 队列`。10.6.19 已 ✅，`TeamManager.HasStreamTask/ActiveSessionID/PendingSessionID` 都在（`:306/:318` + `team_manager_monitor.go:43`），属于"依赖已完成、桩未回填"。

### T-06 `15.1`/`15.2` 的"零生产消费者"要在计划里写死消费时点

15.1/15.2 各 4000+ 行协议层，生产消费者为 0（计划里写了"首个消费方为 15.6"）。这是**预期内的**，但连续两节都是"写完即零消费"，建议在计划表里给 15.6 加一行"⤴️ 必须回填 15.1/15.2 的消费接线"，否则容易重演 S-01（10.3.30 标 ✅ 但整层没接线）。

### T-07 提交说明里有引用不存在路径的情况

`9d9116454`/`f645563c1` 等 15.2 提交说明写"参照 `harness_providers/stream.py:19-155`"，而 `harness_providers/` 目录**在本仓尚不存在**（计划里是 15.5 才建）。属于"跨章节引用"，不算错，但读提交时容易误以为 15.2 已包含 provider 侧；建议写成"（⤵️ 15.5 落地）"。

---

## 四、对齐 Python 的逐项核对结论（本轮改动的重点文件）

| 文件 | 上游 | 结论 |
|---|---|---|
| `harness_protocol/events.go` | `events.py` 456 行 | **11 枚举 / 34 成员数量一致**（机器核对：`TurnEventKind 6, OutputKind 2, OutputChannel 3, OutputOperation 3, UsageUpdateMode 2, EventRetention 3, EventOverflowPolicy 3, ItemEventKind 3, HookEventPhase 2, DiagnosticLevel 4, ModelRequestStatus 3`，合计 11 类 34 成员）；`event_retention` 5 条 if 顺序逐条对应；`NewHarnessEvent` 的 9 步与 `__post_init__` 顺序一致（步骤 4 因 `str|None`→`string` 收敛后天然不成立，已注明） |
| `harness_protocol/stream.go` | `stream.py` 30 行 | 3 方法接口 + 哨兵，一致；缓冲/游标实现移 15.5 的登记到位 |
| `harness_protocol/placeholders.go` | `checkpoints.py` / `hooks.py` / `interactions.py` | 形状一致；**回填章节指向见 T-01** |
| `kv_cache/hook.go` | `single_agent/kv_cache/kv_cache_react_model_call_hook.py` | 9 方法名与步骤对应；**消费端未接线见 S-01** |
| `recommendation/fork_rail.go` | `prompt_suggestion.py:538+` | priority/AfterModelCall 恒 nil/12 项过滤均对齐；`resolveAffinityConfig` 见 S-01，`resolveGenerationOptions` 已修（`08838a45f`） |
| `agents/harness/async_tools.go` | `agent_teams/harness/async_tools.py` | `tasks` 完成即删、`events` 独立保留，与上游 `_tasks.pop`/`_events` 分工一致；**取消不注入见 M-02** |
| `agent_teams/harness/team_harness.go` | `agent_teams/harness/team_harness.py` | Start 重建 / Stop 提交子会话对齐；**重建丢 resolver 见 M-04** |
| `auto_permission/tool_registration.go` | `_auto_permission/models.py` 等 | 观察者模式解决循环依赖，依赖方向正确；登记的 9 个工具名与既有 `sessions_*` 合并决策一致 |
| `controller/modules/task_scheduler.go` | `controller/…/task_scheduler.py` | `context.WithoutCancel` 剥离轮次取消，语义正确；**调度器只在 `Controller.Stop` 停，而全仓无 `Controller.Stop` 生产调用**（提示：进程退出前不显式停，靠进程结束回收；若将来有"热重载控制器"需求需补） |

---

## 五、给下一批的建议顺序

1. **先修 S-01 的装配端**（约 120 行）：`buildKVCacheAffinityConfig` + 装配调用 + `resolveAffinityConfig` 取真值。这一条不通，10.3.30 的 13 个提交等于零产出。
2. **再修 S-02 的三处团队空桩**（约 60 行，方法都在，只差调用）。
3. **M-02 的异步任务收尾**（约 30 行）——它同时修掉"Stop 后 registry 残留 running"。
4. **M-01 的分叉历史**：先加提示，再等 10.3.31f。
5. **M-06 的 goal 无限重试**：先补跟踪项与一条 checkpoint 恢复用例，再动代码。

---

## 附：本轮抽查过的提交

`ff53815ff` `4ad5ea195` `72d4c9e35` `e2b03b2a1` `42d0ce6c3` `9f60150cc` `9b507b001` `9883358d8` `11012c742` `f3d09f6a1` `c1a69edde` `93857cc36` `f645563c1` `92cab5eeb` `475d19a9d` `9d9116454` `7828bb019` `ca16ab971` `08838a45f` `f3c726e2c` `394d45e4a` `c11505798` `ab14b93fb` `b3b01d413` `7a10b14e2` `751eb45b7` `635346423` `8789e0623` `be0c549f2` `15c8a1768` `259011273` `6c9502124` `628d8a9de` `25c5fb8a4` `05fde4fb2` `0cd025f35` `0d404c938` `2a55c4136` `d0ee42dc6` `a7d1b4e94` `25747b8b5` `1565e20a8` `5f37666c4` `4f995a277` `c26cbe8dd` `6f9e55746` `d029c477d` `41fa91812` `73299b94a` `a40c5816e` `9610177a7`
