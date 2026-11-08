// Package prompts 提供团队级提示词模板和 Section 构建器。
// 本包负责：
//   - 从嵌入的 .md 模板加载中英文提示词
//   - 构建团队策略的静态 PromptSection（角色/HITT/工作流/生命周期/任务下发/
//     任务状态机/bridge 自契约/额外/入站标签）
//   - 团队状态消息正文（身份/团队信息/名册）+ 名册 diff
//   - 任务修订说明与正文 diff（message_template）
//   - team.plan 模式下的 plan_agent 提示词特化
//
// 本包承载上游 prompts/sections.py 的全部 15 个 build_* 函数：14 个已移植，
// build_team_member_system_prompt 因唯一消费者（external_cli_spawn）尚未移植
// 而缺失（spec D-9，⤵️ 15.7-15.9 / 25.9）。
//
// 注意：团队**状态**（身份/团队信息/名册/政策块）不进 system prompt，也不再走
// mtime 缓存全量重建——它由 agent_teams/tracker 的 TeamContextTracker 按增量写进
// 成员对话历史。
//
// Python: openjiuwen/agent_teams/prompts/
//
// 文件目录：
//
//	prompts/
//	├── doc.go                # 包文档
//	├── loader.go             # go:embed 模板加载器
//	├── policy.go             # RolePolicy + BuildSystemPrompt
//	├── sections.go           # 静态 PromptSection builder（角色/HITT/工作流/生命周期/
//	│                         #   任务下发/任务状态机/bridge/额外/入站标签）+ 统一入口与政策披露
//	├── sections_leader.go    # leader 三步披露 builder（bootstrap/prebuilt/bound_group）
//	├── messages.go           # 团队状态消息正文（身份/团队信息/名册）+ 名册 diff
//	├── message_template.go   # 任务修订说明与正文 diff（expand_message 全家 ⤵️ 25.6）
//	├── team_plan_agent.go    # ApplyTeamPlanAgentPrompt + 常量
//	├── team_plan_mode.go     # BuildTeamPlanModeSection + 常量
//	├── cn/                   # 30 个中文 .md 模板
//	├── en/                   # 30 个英文 .md 模板
//	└── system_prompt.md      # 语言无关顶层模板
//
// cn/ 与 en/ 各 30 个模板（20 个模板名 × 2 语言，含 inbound_tags.md）：
//
//	角色与策略：leader_policy / teammate_policy / teammate_policy_external /
//	           human_agent_policy / leader_workflow{,_predefined,_hybrid} /
//	           lifecycle_{persistent,temporary}
//	任务下发：  dispatch_{autonomous,scheduled}_{leader,teammate} +
//	           5 个 scheduled 槽位片段（messaging_open ×2 / messaging_closed /
//	           subtasks ×2）+ dispatch_scheduled_leader_inline_member
//	任务状态：  task_state_{autonomous,scheduled}
//	HITT：     hitt_{leader,teammate,teammate_anonymous,human_agent}
//	其它：     bridge_agent / leader_bootstrap / leader_prebuilt /
//	           leader_bound_group / leader_swarmflow / inbound_tags /
//	           team_plan_agent / team_plan_mode
//
// 对应 Python 代码：openjiuwen/agent_teams/prompts/
package prompts
