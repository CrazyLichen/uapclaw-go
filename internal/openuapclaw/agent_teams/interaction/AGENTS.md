# Agent Teams Interaction

> 改编自上游 `agent_teams/interaction/AGENTS.md`。Go 结构见 `doc.go`。

外部交互入口子系统。SDK / 用户 / leader 经这里把消息送进团队的三种视角（GodView / Operator /
HumanAgent），由 runtime/manager.go 的 dispatch 路由到 `UserInbox` / `HumanAgentInbox`。HITT
（Human in the Team）的全部 runtime 表面落在这层。

## 文件地图

| Go 文件 | 作用 |
|---|---|
| `payload.go` | 三种交互视角 payload + `DeliverResult(ok, message_id, reason, output, data)` 统一返回 |
| `router.go` | `ParseInteractStr`（`# / $ / @member` 前缀 → typed payload，纯语法解析不查 roster）；`ParseMention`；`ResolveTargets`（严格匹配 @member，未知 mention 折回无 @ 消息投给 leader/avatar，F_23）；保留名校验 |
| `user_inbox.go` | `UserInbox`：user 侧显式 API（broadcast/direct/deliver_to_leader） |
| `human_agent_inbox.go` | `HumanAgentInbox`：人类成员对外发声，仅 HITT 启用时可用；to=None 仅对 avatar 型有意义（passive 裸输入在 dispatch 被拒） |
| `interact_input.go` | interact 输入类型（25.1a 后与投递面合流） |

Bridge Agent（`bridge_protocol.py` 对应物）⤴️ Go 未实施（依赖 F_07 capability 面）。

## 调用链

- `Runner.interact_agent_team(payload, team_name, session_id)`；bare str 作为 GodViewMessage 便捷形式。
- 三视角到 inbox 的 dispatch 统一在 `runtime/manager.go`：GodView→deliver_to_leader，Operator→
  UserInbox，HumanAgent→HumanAgentInbox。
- `@xxx body` 解析只在这一处（router.go）——dispatcher 只保留调用点。

## HITT 要点

- **`enable_hitt` 是分层开关**：spec 层=能力天花板；`build_team(enable_hitt=...)`=运行时开关
  （None 继承 / True 要求 spec=True / False 显式禁用并跳过预配）。
- **人类成员来源**：静态（predefined_members 声明）+ 动态（spawn_member(role_type='human_agent')，
  禁传 model/prompt）。
- **被动人类成员**（F_111，passive_human）：无 avatar——纯名册身份 + 消息地址 + 透传工具执行器，出生
  即 READY 零运行时；经 HumanAgentToolCall 透传 member_complete_task 完成。
- **一致性约束**（build fail-fast）：enable_hitt=False 且预配 HUMAN_AGENT → 配置错误。
- **运行约束**：human_agent 是保留成员名；工具集仅 view_task+member_complete_task；**assignee 指向
  human-agent 且 CLAIMED 的任务 leader-immutable**（UpdateTask 拒 reassign/cancel，批量 cancel 跳过）；
  发给 human-agent 的消息**保持 is_read=False**——avatar 与 teammate 共用 poll 路径，写入侧自动标已读
  会绕过投递（F_20）；控制者输入带显式来源标记（`<team-inbound from="controller">`，运行约束 6）；
  团队事件流向 human-agent 渲染走 `hitt.*` i18n 模板（前缀「控制者」区别「用户」）。
- `_resolve_team_mode` 只把非 HUMAN_AGENT 的 predefined member 计入 hybrid 派生——纯 HITT 团队仍是
  default 模式，leader 保留 spawn_member 工具。
