// Package prompts_tool 提供演进工具的 agent 侧元数据（六工具双语卡与 subject schema）。
//
// Python 真相源：openjiuwen/agent_evolving/prompts/tools/evolution.py（439 行）
// + __init__.py（29 行导出清单）。六个 ToolMetadataProvider 注册进
// harness/prompts/tools 全局注册表（init 自注册），经 BuildToolCard 统一建卡——
// 与 Python 经 harness.prompts.tools.base.ToolMetadataProvider 注册同构。
// 提示词字符串一比一复刻 Python 原文，不做自行翻译。
//
// 文件目录：
//
//	tools/
//	├── doc.go         # 包文档
//	├── evolution.go   # 六个元数据提供者 + subject schema + 注册段
//	└── evolution_test.go（测试，不入文件树）
//
// 对应 Python 代码：openjiuwen/agent_evolving/prompts/tools/
package prompts_tool
