# Agent Teams Runtime

> 改编自上游 `agent_teams/runtime/AGENTS.md`。Go 结构见 `doc.go`。

`TeamAgent` 对象池 + 派发 + 并发门禁子系统。Runner 经这一层把 `TeamAgentSpec`（或已激活的 team_name）
映射成可被 SDK facade（streaming 运行 / interact / human-agent inbound 注册 / pause / stop / release /
delete）操纵的 pool entry。

## 文件地图

| Go 文件 | 职责 |
|---|---|
| `pool.go` | `TeamRuntimePool` / `ActiveTeam` / `RuntimeState`。pool key=team_name，同一 team 内存中至多一个 entry；`RuntimeState.RUNNING/PAUSED` 与 schema 的 TeamLifecycle（temporary/persistent）是不同层次枚举，不混用 |
| `gate.go` | `InteractGate`：run/interact 并发门禁（admit/consume_done/close_and_drain/reset）；每 ActiveTeam 一个 |
| `dispatch.go` | `decideRunAction` 纯函数 + 7 路 truth table：CREATE / NEW_TEAM_IN_SESSION / COLD_RECOVER / RESUME_FROM_PAUSE / REJECT_RUNNING / REJECT_ORPHANED / REJECT_INCONSISTENT |
| `manager.go` | `TeamRuntimeManager`：activate 先 tear-down 跨 session stale entry 再派发；`finalize`/`finalize_member` 在 run 退出 finally 决定 pause vs stop；interact 接 InteractPayload 走 gate；`_dispatch_payload` OperatorMessage 分支先 auto_start 再写消息（start→write order）；R-7 交付的 round-state/waiter 账本（25.2b） |
| `team_runner.go` | Runner facade 的 team 路径 |
| `external_cli_spawn_wiring.go` | 外部 CLI spawn 接线（15.16b） |

## 行为铁律

- **派发决策是纯函数**：decideRunAction 只看输入元组，无副作用；manager 把 IO 收集进派发输入，
  `_apply_action` 才动 TeamAgent/pool/session。改派发改 dispatch.go，不塞回 manager。
- **lifecycle 直达 TeamAgent**：没有 factory wrapper 中间层。
- **同一 team 内存中只有一个实例，跨 session 必走 stop+rebuild**：不出现多个同 team_name entry，
  不复用 TeamAgent 实例跨 session（WARM_RECOVER 已删，F_05）。
- **InteractGate 生命周期与 run cycle 对齐**：退出 finally 先 manager.finalize 再关 gate；
  RESUME_FROM_PAUSE 在 activate 时 gate.reset()。
- **finalize 决策权归 manager，不归 kernel**：pause vs stop（shutdown_requested 或 lifecycle!=persistent
  → stop+remove，否则 pause）在 manager；`finalize_member` 必须保留其他生命周期所有者已写的终态，
  不能把 runtime failure 写下的 ERROR 盖回 READY。
- **静止前置**：release_session/delete_team 在 pool 仍持 entry 时报 BUSY，调用方必须先 stop_team。

## Stream 生命周期 ≠ OuterLoop 单轮

stream 不因 leader DeepAgent 单轮跑完就结束。真正终止由 team 层显式动作触发：pause/stop/clean_team
（或退出 finally 收尾）。临时团队 leader 调 clean_team 的结束机制：`TeamBackend` 成功回调同步置位
`TeamAgentState.team_cleaned`，`StreamController._on_idle_settled` 读到后 close_stream——**不依赖**会与
round-end 竞态的总线事件（leader 故意忽略自己的 CLEANED 事件，F_10/S_02）。leader 跑完一轮后 idle 等
下一个唤醒事件，dispatcher 仍在调度，pool entry 仍 RUNNING。基于"OuterLoop 完成=stream 结束=entry 失活"
的判断都是错的。

## Pool 只持 leader

leader 由 spec 路径（manager.activate）入 pool；teammate/human-agent 走 member=True 入口（spawn 调用），
完全不碰 pool。没有"已 build 实例 → pool"的快捷入口（register_instance 已删——pool 写入语义只剩一种）。
