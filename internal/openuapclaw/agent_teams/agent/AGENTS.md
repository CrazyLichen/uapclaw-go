# Agent Teams Agent（TeamAgent 运行时主骨架）

> 改编自上游 `agent_teams/agent/AGENTS.md`。Go 结构见本目录 `doc.go`。

`TeamAgent`（`team_agent.go`）是单一实现——同一个类承担 leader 与 teammate，按 `TeamRole` 切行为，
内部组合一个 DeepAgent 跑 LLM。

## 四象限分解

| 象限 | Go 文件 | 含义 |
|---|---|---|
| 静态数据 | `blueprint.go` | `TeamAgentBlueprint`，构造时确定，生命周期不变 |
| 运行时可变状态 | `state.go` | `TeamAgentState`，**只放跨 operator 字段**（`team_cleaned` latch、`idle_since` idle 时钟 F_65、`member_registry` 名册 F_74）；operator 内部状态留各 manager |
| 每实例资源 | `resources.go` | `PrivateAgentResources`：DeepAgent、worktree/memory manager、任务告知 tracker 等，每 member 一份 |
| 每进程基础设施 | `infra.go` | `TeamInfra`：进程内共享（messager/db/task_manager/message_manager/team_backend）。leader 与 teammate 不同进程，"共享"是 per-process |

新增字段前先想：跨 operator？跨实例？跨进程？放错象限 = "这个状态 3 处都在改" 的代码烂味。

## Manager / 协作层

| 文件 | 类 | 职责 |
|---|---|---|
| `team_agent.go` | `TeamAgent` | 唯一对外类；`AutoStartMember`/`AutoStartAll` 供 interact dispatch lazy startup |
| `agent_configurator.go` | `AgentConfigurator` | DeepAgent 装配：rail 经 RailSpec 声明式注入 + live handle 经 BuildContext extras（F_32）；`resolveTeamMode` 在此 |
| `member.go` / `member_factory.go` | `TeamMember` / `createMemberHandle` | 成员状态机封装 + 集中构造 |
| `member_activity.go` | `MemberActivityRegistry` + `IdleSignal` | **leader-only** 全成员状态内存视图：`record()` 返回 IdleSignal（有人动→CANCEL，全员静止上升沿→SCHEDULE）；静止集 = MEMBER_QUIESCENT_STATUSES ≠ settled；**不含计时**（2s 去抖归 StreamController）也**不含任务板概念**（F_74/F_77） |
| `payload.go` | `SpawnPayloadBuilder` | spawn 的跨进程 wire 格式；改字段要同步子进程入口 |
| `spawn_manager.go` | `SpawnManager` | 成员进程生命周期；`restart_teammate` 以 initial_message=None re-spawn——**恢复不 replay 首启指令** |
| `recovery_manager.go` | `RecoveryManager` | 团队容错：成员崩溃恢复；冷恢复跳过 SHUTDOWN_REQUESTED/SHUTDOWN |
| `session_manager.go` | `SessionManager` | session checkpoint 读写、生命周期 |
| `stream_controller.go` | `StreamController` | runtime 输出转发 + 状态映射：`_map_state` RUNNING→BUSY、IDLE→READY+`onIdleSettled`（维护 `idle_since`，F_65）；transient retry 181001 swallow+重驱；chunk 自动升级 TeamOutputSchema + observer fan-out；`scheduleTeamIdle` 2s 去抖的 team.idle（到点先查 registry 再经 `task_board_probe` 确认任务板，两条都过才 emit，**不关流**，F_74/F_77）；`_on_idle_settled` 顺序=先 `member_startup_reconciler`（F_84）后 completion poll——**顺序是正确性必需** |
| `member_runtime.go` | 成员 runtime 面 | HasPendingInterrupt / InjectMemberMemory / RegisterMemberTools 等成员级能力（15.13/15.16 演进） |

## 子目录

- `coordination/` — 唤醒循环（见 `coordination/AGENTS.md`）
- `coordination/scheduling/` — 调度模式决策引擎（见 `coordination/scheduling/AGENTS.md`）

## 跨文件协作关键点

- **同一 team 的 leader 和 teammate 不在同一进程**：infra 的 per-process 语义来源于此；两边都要看的
  状态走 db/messager，不走对象引用。
- **`payload.go` wire 格式是公共契约**：改构建端必须同步解析端。
- **stream_controller 不直接驱动 DeepAgent**：驱动入口是 `team_agent.go` 的
  start_agent / steer / follow_up / deliver_input。
- **初始消息只首启注入，空则不 send**（F_33）：spawn 不 fabricate 占位符；空 query 只起来订阅、靠
  `enqueue_initial_mailbox_poll` 投递真实消息。例外：external CLI 成员自带 join prompt。
- **stream chunk 跨成员 fan-out**：`_tag_chunk` 升级 TeamOutputSchema（source_member+role）后 fan-out
  给 observers；inprocess 下 SpawnManager 把 teammate chunk 转投 leader stream_queue；observer 抛错自动
  detach；subprocess 模式不转发（扩展点已留：messager-driven observer）。

## 边界

- LLM 在 `harness/`（DeepAgent/NativeHarness），本目录只组装 + 调度。
- 对象池 / 派发 / 并发门禁在 `runtime/`。
- 三视角交互经 `interaction/` 进 coordination；mention 解析在 `interaction/router.go`。
