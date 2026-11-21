# Agent Team Prompts

> 改编自上游 `agent_teams/prompts/AGENTS.md`。Go 结构见 `doc.go`；模板清单以 `prompts/{cn,en}/` 实际
> 文件为准（25.0g 后每种语言 34 个）。

Markdown 模板是团队 Agent 的行为契约。Go 侧只做装配（`sections.go`/`sections_leader.go` 按 section
构造 PromptSection，各 builder 直接 `LoadTemplate` 读 `cn/`/`en/` 下的 `.md`），所有文案都在 `.md`
文件里——改提示词不需要动 Go。

## 文件地图

| Go 文件 | 作用 |
|---|---|
| `loader.go` | `LoadTemplate(name, lang)` 加载器，默认语言 cn |
| `sections.go` | `TeamSectionName` + `BuildTeam*Section` 构造 PromptSection（系统提示词唯一装配路径，由 TeamPolicyRail / `BuildTeamMemberSystemPrompt` 消费） |
| `sections_leader.go` | leader 专用三入口（F_76/F_122/F_125）：`BuildLeaderBootstrapSection`（系统提示词唯一 section，`{{collaboration_mechanism}}` 槽 gate `swarmflow_enabled`）/ `BuildLeaderPrebuiltSection`（framework 建队）/ `BuildLeaderBoundGroupSection` / `BuildLeaderPolicyDisclosure`（其余 section 拼成 build_team 的 ToolResult 文本） |
| `messages.go` | 团队状态**消息**正文：`BuildIdentity*`/`BuildTeamInfo*`/`BuildRosterSnapshot/Delta` + `DiffRoster`（orderedmap 保首个位置+后写覆盖）+ `FormatMemberLine`。纯渲染，投递时机与基线归 `tracker/` |
| `message_template.go` · `message_template_expand.go` | 框架模板消息两阶段渲染（F_63）：发送存意图（content 空 + meta={template,refs,params}），投递时按收件人语言加载模板、`{{task.*}}` 填当前行；F_129 新增 task.revision 与 `change_notes`/`revision_diff`（Ratcliff-Obershelp 复刻 difflib）/`subtask_brief` |
| `policy.go` / `team_plan_mode.go` / `team_plan_agent.go` | HITT 316 行契约内联串、plan mode 模板 |

所有模板 cn/en **成对存在**，新增语言只需加子目录。

## 模板目录要点（全量表格见上游原文，此处留决策要点）

- **`leader_bootstrap.md`**：leader 系统提示词里唯一团队内容（F_76 渐进式披露）——身份 + collaboration
  槽 + 「先调 build_team，准则随返回下发」。建队之前唯一成立的内容。
- **`leader_prebuilt.md`**（framework 建队，F_122）替换 bootstrap；无 collaboration 槽。
- **`task_state_autonomous/scheduled.md`** **按模式拆两份而不是一份带 caveat**（F_76）：autonomous 版
  对验证闸一字不提（点名一个不存在的能力正是让模型去够它的原因）。
- **dispatch/workflow/task_state 三维正交**（模板数 3+2+2 而非 3×2）：workflow 模板不许写"怎么让成员
  开工"，dispatch 模板不许写"能不能 spawn"，task_state 只写状态与转换。`leader_policy.md`/`teammate_policy.md`
  全模式共用，不许出现模式专属指令。
- **`human_agent_policy.md`** 不能落回 `teammate_policy`（无条件的"必须回 user"会让 avatar 把控制者私话
  答给另一个真人）。
- **`inbound_tags.md`**：XML 标签体系 + `from="user"` 与 `from="controller"` 角色定义（都是团队外部真人、
  都不在名册、**但不是同一个人**）。
- **`tool_namespace.md`**：裸工具名归哪个 MCP server；只说 server 名不说全限定形式（那是 provider 的事）。

## 编辑规则（硬约束）

1. **cn/en 双语对齐**：结构、小节顺序、字段名一致，只翻译文本。
2. **动态值走 `{{name}}` 占位符**；capability 槽（`{{collaboration_mechanism}}` 等）gate 用的信号必须与
   挂载工具的信号同源（`leader_swarmflow` 是范本）。**能力关掉时讲这个能力的文案必须跟着消失**。
   **一个槽只能有一个消费者**。per-member 变量一律不许进静态模板（普通 teammate 的自身名字走
   `<team-context>` 消息，前缀 KV cache 稳定）。
3. loader 缓存基于 (name, lang)；进程不感知文件改动，开发期热更需重启。
4. **空分节省略而不是空字符串**：role != LEADER 时 workflow/lifecycle section 直接返回 None，
   不要在 .md 写占位文字。
5. **策略分层不重复**：leader_policy 谈身份/原则、leader_workflow 谈步骤、tools/locales 谈工具语义。
6. 排版：顶层 `##`，不用 emoji。

## Runtime 装配

唯一装配入口 `BuildTeam*Section`：由 `rails/team_policy_rail.go` 按优先级合并进 SystemPromptBuilder；
外部 CLI 成员经 `BuildTeamMemberSystemPrompt` 渲染成独立字符串。

**leader 的投递时刻与其他角色不同（F_76 渐进式披露）**：teammate/human_agent/bridge 构造期全进系统
提示词；leader 只拿 team_bootstrap + team_extra，其余经 `BuildLeaderPolicyDisclosure` 拼成 build_team
的 ToolResult，由 `BuildTeamTool.RenderForLLM` 附在建队结果之后下发。
- 为什么是 build_team：它是 dispatch_mode/enable_hitt 等模式变量全部落定的那一刻。
- 代价与兜底：准则变成普通历史消息会被 compaction 摘要掉——压缩后原样重注入那条 ToolResult
  （不截断）；cold recovery 的 leader 拿不到准则属已知遗留。

**团队状态不走这条路**：正文由 `messages.go` 渲染、由 `tracker/` 的 TeamContextTracker 在数据第一次
出现或变化的那次调用写进成员对话历史（只插不删）。
