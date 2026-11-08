// Package prompts 提供团队级提示词模板和 Section 构建器。
// 本包负责：
//   - 从嵌入的 .md 模板加载中英文提示词
//   - 构建团队策略的静态 PromptSection（角色/HITT/工作流/生命周期/额外/入站标签）
//   - 团队状态消息正文（身份/团队信息/名册）+ 名册 diff
//   - 任务修订说明与正文 diff（message_template）
//   - team.plan 模式下的 plan_agent 提示词特化
//
// 注意：团队**状态**（身份/团队信息/名册）不进 system prompt，也不再走 mtime
// 缓存全量重建——它由 agent_teams/tracker 的 TeamContextTracker 按增量写进成员
// 对话历史。
//
// Python: openjiuwen/agent_teams/prompts/
//
// 文件目录：
//
//	prompts/
//	├── doc.go                # 包文档
//	├── loader.go             # go:embed 模板加载器
//	├── policy.go             # RolePolicy + BuildSystemPrompt
//	├── sections.go           # 8 个 PromptSection builder + TeamSectionName + _LABELS + HITT
//	├── messages.go           # 团队状态消息正文（身份/团队信息/名册）+ 名册 diff
//	├── message_template.go   # 任务修订说明与正文 diff（expand_message 全家 ⤵️ 25.6）
//	├── team_plan_agent.go    # ApplyTeamPlanAgentPrompt + 常量
//	├── team_plan_mode.go     # BuildTeamPlanModeSection + 常量
//	├── cn/                   # 10 个中文 .md 模板（含 inbound_tags.md）
//	├── en/                   # 10 个英文 .md 模板（含 inbound_tags.md）
//	└── system_prompt.md      # 语言无关顶层模板
//
// 对应 Python 代码：openjiuwen/agent_teams/prompts/
package prompts
