// Package tools 提供技能管理工具集，将 SkillManager 的能力封装为模型可调用的工具。
//
// SkillToolkit 是将 SkillManager 暴露为模型友好工具的聚合器，提供 3 个工具：
// search_skill、install_skill、uninstall_skill。
//
// 文件目录：
//
//	tools/
//	├── doc.go              # 包文档
//	└── skill_toolkit.go    # SkillToolkit 结构体与 3 个工具实现
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/skill_toolkits.py
package tools
