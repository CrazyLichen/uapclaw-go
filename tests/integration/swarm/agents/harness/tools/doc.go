//go:build integration

// Package tools 提供 Swarm SkillToolkit 模块的集成测试。
//
// 测试覆盖：
//   - SkillToolkit 构造与工具注册
//   - GetTools 返回工具数量和名称
//   - SkillSearchItem/InstalledItem 序列化
//   - normalizeSource 来源校验
//   - AgentRail 接口无关（本包无 Rail）
//
// 文件目录：
//
//	tools/
//	├── doc.go                    # 包文档
//	└── skill_toolkit_test.go    # SkillToolkit 集成测试
//
// 对应 Python 代码：tests/system_tests/agent/skill/test_skill_real_system.py
package tools
