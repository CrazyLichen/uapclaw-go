//go:build integration

// Package skills 提供 SkillManager / SkillUtil 系统级集成测试。
//
// 测试覆盖：
//   - SkillManager 技能注册/注销/查询全流程
//   - 从 MockFS 加载 SKILL.md 文件的 E2E 场景
//   - SkillUtil 门面类注册+提示词生成
//   - 技能目录扫描、YAML front matter 解析、注册覆盖
//
// 文件目录：
//
//	skills/
//	├── doc.go                    # 包文档
//	└── skill_manager_e2e_test.go # SkillManager + SkillUtil E2E 集成测试
//
// 对应 Python 代码：openjiuwen/single_agent/skills/ + tests/system_tests/agent/skill/test_skill_real_system.py
package skills
