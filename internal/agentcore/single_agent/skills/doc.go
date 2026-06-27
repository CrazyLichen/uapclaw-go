// Package skills 提供 Agent 技能注册、管理与提示词生成能力。
//
// 本包实现 Agent 的 Skill 机制：通过将技能元数据（名称/描述/目录路径）注入
// 系统 prompt 的 skills section，告知 LLM 当前可用的技能，LLM 可使用 read_file
// 工具读取 SKILL.md 获取具体工作流，然后按工作流执行任务。
//
// 文件目录：
//
//	skills/
//	├── doc.go                # 包文档
//	├── skill.go              # Skill 模型 — 技能元数据结构体
//	├── skill_manager.go      # SkillManager — 技能注册/注销/查询管理 + YAML front-matter 加载
//	├── skill_util.go         # SkillUtil — 高层门面，组合 SkillManager + RemoteSkillUtil
//	└── remote_skill_util.go  # GitHubTree/GitHubError/RemoteSkillUtil — GitHub 远程技能下载
//
// 对应 Python 代码：openjiuwen/core/single_agent/skills/
package skills
