# Coordination — 唤醒循环

> 改编自上游 `agent_teams/agent/coordination/AGENTS.md`。Go 结构见 `doc.go`。

`TeamAgent` 的事件驱动唤醒层。把传输层来的 `EventMessage` 与内部 poll 计时器的 `InnerEventMessage`
收成统一的 `CoordinationEvent`，按粗筛规则放行后由回调框架分发到场景 handler。**自身不做业务决策**
——handler 经三类 narrow protocol（`AgentRoundController` / `TeamLifecycleController` / `PollController`）
触发行为，最终驱动 DeepAgent + team tools。

## 文件地图

| 文件 | 类 | 职责 |
|---|---|---|
| `event_bus.go` | `EventBus` / `InnerEventType` / `InnerEventMessage` | 事件入队 + 周期 poll timer + lifecycle（start(wake_callback)/stop）；天然实现 PollController。**HUMAN_AGENT 角色不启动周期 poll timer**（dispatch mute，起 timer 纯空转）。`SCHEDULER_SCAN`（F_62）是 self 事件调度器扫描回声——无 coordination handler 监听，仅 TeamScheduler 消费 |
| `dispatcher.go` | `EventDispatcher` + 三 protocol + `DispatcherHost` | 粗筛规则（agent_ready / inner-vs-transport / 角色门控）+ 持私有回调框架 + 注册 handler 的 `GetCallbacks()`；handler 实例作字段直接暴露。**回调异常被框架吞掉（log+continue）**，handler 必须自己记日志 |
| `kernel.go` | `CoordinationKernel` | 整体协调 facade：setup 构造 event_bus→dispatcher（按 DispatchMode 装配；scheduled leader 额外构造休眠态 TeamScheduler）；start 时闭合循环依赖（组合 dispatch→scheduler.on_event）、读到已有 team 行激活调度器、leader 读不到 team 行且 bootstrap_mode=framework 时就地 build_team（F_122）；pause/stop 即 scheduler.deactivate；start 尾部 leader-only 发 `team.recovered` 标记帧（B_07） |
| `types/` | 事件与 protocol 类型 | CoordinationEvent / EventCallbackFunc 等 |

## handler 拆分

`handlers/` 下一个 handler 一个业务域，各自声明 event→method 映射经 `GetCallbacks()` 注册。
`BaseCoordinationHandler` 一次性收 5 类依赖（blueprint 静态身份 / infra per-process 容器 / round
控制 / lifecycle 控制 / poll 控制），**handler 不持有原始 host 引用**——新增依赖走 narrow protocol
或显式构造参数，不塞回 host（上帝接口）。

| handler | 监听 | 状态 | 一句话职责 |
|---|---|---|---|
| `AgentLifecycleHandler` | USER_INPUT(inner)/STANDBY/CLEANED/TOOL_APPROVAL_RESULT/TASK_PLAN_RESPONSE | 无 | 本地生命周期信号直通 host |
| `MemberHandler` | REFRESH_TEAM_CONTEXT(inner) + 6 个 MEMBER_* | 无 | leader 侧纯观测 + 活动登记（F_74）；**不清理团队**——名册全空是状态不是意图（F_81）；非 leader 只反应自身事件；名册变动触发外部 CLI 成员 announce_team_context（F_70） |
| `MessageHandler` | MESSAGE/BROADCAST/POLL_MAILBOX(inner) + MEMBER_SHUTDOWN(fan-out) | 无 | 三条投递路径统一先过 `_harness_input_blocked` 闸门（S_03 机制 18）；投递前 `_expand` 做 F_63 两阶段模板渲染；leader ack user-bound + 通知 human-agent inbound |
| `TaskBoardHandler` | INITIAL_POLL_TASK(F_69) + 15 个 TASK_*（4 定向 + 3 verify 闸 + PLAN_RESPONSE + 6 看板巡视）；调度子类加 TASK_REVIEW_VOTE | 无 | 三组策略 + 首启板巡视（见 `handlers/agent.md`） |
| `StaleTaskHandler` | POLL_TASK(inner) | 4 个私有记账 | 两个按 assignee 划分的 sweep（成员自催 self-only / leader 催无主 PENDING） |
| `TeamCompletionHandler` | POLL_TASK / TASK_LIST_DRAINED / TEAM_COMPLETED | 上升沿标志 + 回调表 | leader 且 round 真空闲的三条件完成判定，上升沿发 TEAM_COMPLETED；persistent 额外结束 leader 流触发 auto-pause |
| `WorkflowHandler` | WORKFLOW_PROGRESS | 无 | leader-only，叙述 swarmflow 中途里程碑（⤴️ 消费 25.12） |
| `ReliabilityHandler`(opt-in) | ANOMALY_DETECTED/MESSAGE/BROADCAST | RemediationPolicy + PingPongDetector | leader-only 异常处置；25.1g 交付，dispatcher 在 7 固定 handler 之后条件注册 |

**停滞检测计时源是运行时 idle 时钟，不是 DB updated_at**（F_65）：`TeamAgentState.idle_since`（monotonic，
进程本地）由 StreamController 在 READY/BUSY 边打戳/清空，handler 经 `AgentRoundController.idle_seconds()`
读（忙时为 None——正在干活的成员类型层面不可能被判停滞）；kernel.start 调 `refresh_idle_baseline` 在
poll 恢复前重置，pause 窗口不计入。调度模式仍走 updated_at，记为 F_65 已知遗留。

## 三条铁律

**铁律 1：coordination 不做决策。** loop 只管 wake-up，业务行为由 DeepAgent + team tools 驱动。
新功能想塞进 dispatcher/handler 之前先问：是不是应该做成一个新工具，让 LLM 自己决定调？

**铁律 2：handler 不持有原始 host 引用。** 依赖一律 narrow protocol / 显式构造参数。

**铁律 3：异常自己记。** 回调框架吞普通异常（AbortError 上抛），handler 内部失败必须自己记
error 日志，不能把框架的 swallow 当隐式错误处理。

策略细节（五个决策维度、各 handler 分支规则、fan-out 契约）见 [`handlers/agent.md`](handlers/agent.md)；
调度引擎见 [`scheduling/agent.md`](scheduling/agent.md)。
