// Package skill_call 提供技能经验预览参数句柄。
//
// SkillExperienceOperator 是仅预览的参数句柄，管理技能经验记录。
// 它仅拥有 "updates_generated → local_apply_completed" 阶段；
// 暂存审批由 ExperienceManager 负责，持久化由 EvolutionStore 负责。
//
// 文件目录：
//
//	skill_call/         # 技能调用操作器
//	├── doc.go                          # 子包文档
//	└── skill_experience_operator.go    # SkillExperienceOperator
//
// 对应 Python 代码：openjiuwen/core/operator/skill_call/
package skill_call
