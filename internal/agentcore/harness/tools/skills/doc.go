// Package skills 提供技能查看工具（SkillTool）和技能列表工具（ListSkillTool）。
//
// SkillTool 通过 SysOperation 读取技能目录下的指定文件（默认 SKILL.md），
// ListSkillTool 列出所有可用技能或通过 LLM 路由筛选相关技能。
// 两个工具均通过 getSkills 回调获取当前已启用的技能列表，
// 由 SkillUseRail（9.19-23）在 init 时注册到 Agent。
//
// routeSkills 方法依赖真实 LLM API 调用（llm.Model 是具体结构体，非接口），
// 其集成测试延后到 SkillUseRail 实现时，使用 //go:build llm 标签隔离。
//
// 文件目录：
//
//	skills/
//	├── doc.go           # 包文档
//	├── skill_tool.go    # SkillTool 技能查看工具
//	├── list_skill.go    # ListSkillTool 技能列表/路由工具
//	└── skills_test.go   # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/tools/skills/
package skills
