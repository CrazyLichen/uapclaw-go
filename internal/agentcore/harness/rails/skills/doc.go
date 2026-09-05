// Package skills 提供技能使用护栏实现。
//
// SkillUseRail 负责技能提示词注入和工具注册，从 skills_dir 增量加载 SKILL.md 文件，
// 根据 skill_mode 决定注入方式（all/auto_list），并可选附加演化经验文本。
//
// 文件目录：
//
//	skills/
//	├── doc.go               # 包文档
//	├── skill_use_rail.go    # SkillUseRail 技能使用护栏
//
// 对应 Python 代码：openjiuwen/harness/rails/skills/
package skills
