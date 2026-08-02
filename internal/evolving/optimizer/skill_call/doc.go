// Package skill_call 提供技能经验维度优化器。
//
// SkillExperienceOptimizerBase 固定 domain="skill_experience"，默认优化目标为 experiences，
// 对齐 Python SkillExperienceOptimizer 的共享基类行为。
// SkillExperienceOptimizer 通过 LLM 三通道分析生成经验草稿，
// TeamSkillExperienceOptimizer 在此基础上增加双路径生成逻辑和团队专属功能。
//
// 文件目录：
//
//	skill_call/
//	├── doc.go                    # 包文档
//	├── base.go                   # SkillExperienceOptimizerBase（技能经验优化器基类） 共享字段/方法/常量
//	├── draft_parser.go           # ParsedExperienceDraft + JSON 提取/解析辅助函数
//	├── experience_optimizer.go   # SkillExperienceOptimizer（个体技能经验优化器） Backward/Step/GenerateRecords/RetryParse + 辅助函数
//	├── team_optimizer.go         # TeamSkillExperienceOptimizer（团队技能经验优化器） 双路径 GenerateRecords/UserPatch/TrajectoryPatch/RegenerateBody/callLLM + 辅助函数
//	└── templates.go              # 提示词模板（CN+EN 双语，一比一复刻 Python 原文）
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/skill_call/
package skill_call
