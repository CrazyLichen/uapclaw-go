// Package tools 提供技能管理工具集，将 SkillManager 的能力封装为模型可调用的工具。
//
// SkillToolkit 是将 SkillManager 暴露为模型友好工具的聚合器，提供 3 个工具：
// search_skill（搜索技能）、install_skill（安装技能）、uninstall_skill（卸载技能）。
//
// 核心类型：
//   - SkillToolkit: 工具聚合器，持有 SkillManager 实例
//   - SkillSearchItem: 搜索结果归一化项（内部方法返回具体类型，ToMap 输出给 MapFunction）
//   - InstalledItem: 已安装技能展示信息（内部方法返回具体类型，ToMap 输出给 MapFunction）
//   - ListInstalledResult: 列出已安装技能的结果
//
// 文件目录：
//
//	tools/
//	├── doc.go              # 包文档
//	└── skill_toolkit.go    # SkillToolkit 结构体与 3 个工具实现
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/skill_toolkits.py
package tools
