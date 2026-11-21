# Agent Teams Harness（NativeHarness 运行时外壳）

> 改编自上游 `agent_teams/harness/AGENTS.md`。Go 结构见 `doc.go`；25.1a-1e 交付，spec 见
> `docs/superpowers/specs/2026-11-06-25.1a-harness-surface-design.md` 系列。

team 成员的运行时外壳层。Go 侧 `NativeHarness`（`native_harness.go`）**组合** `*harness.DeepAgent`
（不嵌入——嵌入会与 `DeepAgent.Abort` 同名冲突），自建 command queue + supervisor + forwarder；
保留 DeepAgent 全部能力（task loop/中断恢复/plan mode/rails/state）。`TeamHarness` 在其上做 team 适配。

## 模块地图

| Go 文件 | 职责 |
|---|---|
| `protocol.go` | `HarnessProtocol` 12 成员（state/session_id/start/stop/outputs/send/abort/pause/resume/subscribe…）；并发安全是契约；Pause/Resume 在 25.1a 为哨兵错误、25.1c 换真实现 |
| `native_harness.go` | NativeHarness：supervisor + round 驱动 + send/steer/follow_up；run_once 非流式单次 invoke |
| `team_harness.go` | TeamHarness：team 适配（build/role）；run_once 转发；任务告知（AnnounceTaskRevisions，F_129） |
| `control.go` | 6 条控制命令（每条带 reply chan，等价 Python `ack: Future`） |
| `state.go` | HarnessInternalState / InboxMessage / ActiveRound / HarnessState / RoundPhase |
| `turn.go` | `MemberTurn` + `ResolveMemberTurn`：成员轨迹 turn 身份（session state 键 `trajectory_member_turn`，25.1e） |
| `outputs.go` | 输出队列 + `_END` 哨兵，单消费者迭代器 |
| `snapshot_rail.go` | `PhaseSnapshotRail`：inner-loop 阶段追踪 + 迭代/round 边界快照 + 协作停止（25.1c） |
| `async_tools.go` | 异步后台工具框架（25.1d） |
| `checkpoint.go` | ⤴️ 25.1f NativeCheckpoint（上游 checkpoint.py 59 行） |

## 输入队列：steering 与 follow-up

两条队列同一语义——**攒住的东西整批喂给下一次模型调用**：

- `send(immediate=true)` 推进当前 round boundary（steer）；上下文不同不降级成 follow-up。
- `send(immediate=false)` 进 follow-up 队列，round 结束时**整批** drain 驱动下一轮（F_71）。

**steering 侧每次取多少由 rail 现场定**（F_78）：drain 前触发 `BEFORE_STEERING_DRAIN`，rail 写 limit；
不写=全取。team 非 leader 由 TeamPolicyRail 限 `steer_batch_size`（默认 2）；取不走的原封留队；
**队列非空时至少取 1 条**。两条批次都不在本层拼接——整批列表往下走，由 inner `admitUserMessage`
先把列表交给 ON_USER_MESSAGE rail、rail 增删完才拼 user message（team 侧靠这个整条剔除被覆盖的任务看板）。

## pause / abort / resume（两个正交动词）

停止点落在 **inner ReAct iteration 边界**。`abort` 丢弃当前 round（→IDLE）；`pause` 停在干净边界并
**保留 round**（→PAUSED，resume 原地续跑）。`PhaseSnapshotRail` 是正确性权威：在 before/after_model_call
上协作式 force_finish，**绝不打断运行中的 tool**（副作用不可撤销）；supervisor hard-cancel 只是及时性
优化且 gated 到 model_call_in_flight。RoundPhase BOUNDARY/MODEL/TOOL 决定硬取消还是协作停止；
`abort(immediate=true)` 补回滚语义（SafeStateSnapshot，with_history=false 只回退本轮段落）；
`emitRoundAborted` 标记 chunk（已发出的 chunk 收不回，靠 marker 让消费者丢弃）。

## 轨迹 turn（turn.go）

一次 Team 运行周期所有成员共用一个 `team.<name>` trace，每个 round 自带 turn 身份：
- **开新 turn**：IDLE 起 round、drain follow-up 批次起 round；
- **沿用当前 turn**：steer、resume_continuation、InteractiveInput 回答、failure retry、task-plan 续跑。

计数器存 session state 的 `trajectory_member_turn` 键——**不在** DeepAgent blob 里（pause/abort 回滚会
把编号倒回去）。`TurnNumber` 经 InvokeInputs.Extra 透传为 span 属性（领域二十二映射）。

## 异步工具框架（async_tools.go，25.1d）

Claude Code 两段式范式的通用异步后台工具框架，**限定在 NativeHarness 范围、零 TeamAgent 依赖**：
- **两段式**：① `invoke` 立即返回 launched(task_id) 闭合配对；② 完成结果**绕过工具协议**，经
  `NativeHarness.send(immediate=false)` 作为独立注入式消息进下一轮（IDLE 起新轮、RUNNING 排 follow-up）。
- `AsyncTool` 基类（持 parent_agent；子类实现 run_background）+ `AsyncToolRuntime`（注册表+完成注入器，
  launch/has_running/cancel_all + 管控面 get/list_all/cancel/wait）+ `AsyncToolRecord`。
- 统一 task_id：`id_generator.go`（前缀体系）。
- 磁盘溢写：结果 >spill_threshold（默认 32KB）写盘 + 注入"摘要+路径"；⤴️ spill 接线 + 管控工具随 25.12。
- **铁律**：框架不牵扯 TeamAgent；没有异步 tool_result（完成结果绝不回原 tool_use_id）；
  不收编 sessions_spawn。

## NativeHarness 公共 protocol 适配

⤴️ 25.1f（`protocol_adapter.py` 490 行 / `checkpoint.py` 59 行）：`NativeHarnessProtocolAdapter`
（card=native_v2，六能力）+ `NativeV2HarnessProvider` + checkpoint save/restore。输入排队由 protocol
adapter 独占，不能直接驱动 native_harness.send 或读 outputs。
