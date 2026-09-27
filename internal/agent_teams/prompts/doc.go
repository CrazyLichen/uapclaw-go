// Package prompts 提供团队级提示词模板和 Section 构建器。
//
// 本包负责：
//   - 从嵌入的 .md 模板加载中英文提示词
//   - 构建团队策略的 8 个 PromptSection（角色/工作流/生命周期/人设/额外/信息/成员/HITT）
//   - MtimeSectionCache 基于 DB 更新时间戳的探针缓存
//   - team.plan 模式下的 plan_agent 提示词特化
//
// Python: openjiuwen/agent_teams/prompts/
//
// 文件目录：
//
//	prompts/
//	├── doc.go                # 包文档
//	├── loader.go             # go:embed 模板加载器
//	├── section_cache.go      # MtimeSectionCache mtime 探针缓存
//	├── policy.go             # RolePolicy + BuildSystemPrompt
//	├── sections.go           # 8 个 PromptSection builder + TeamSectionName + _LABELS + HITT
//	├── team_plan_agent.go    # ApplyTeamPlanAgentPrompt + 常量
//	├── team_plan_mode.go     # BuildTeamPlanModeSection + 常量
//	├── cn/                   # 9 个中文 .md 模板
//	├── en/                   # 9 个英文 .md 模板
//	└── system_prompt.md      # 语言无关顶层模板
//
// 对应 Python 代码：openjiuwen/agent_teams/prompts/
package prompts
