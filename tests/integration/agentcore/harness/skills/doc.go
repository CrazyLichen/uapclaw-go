// Package skills 提供技能会话基线的跨子系统集成测试。
//
// 覆盖链路：SkillUseRail（基线落盘 / 提示词节 / 运行时附件）→ SkillTool（会话视图读文件）
// → session state（skill_use 读写）→ PromptAttachmentManager（skills.runtime_changes）。
//
// 对应 Python：openjiuwen/harness/rails/skills/skill_use_rail.py
package skills
