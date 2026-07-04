// Package schema 提供 harness 模式的类型定义，包括 AgentMode、PromptMode、
// DeepAgentConfig 及相关类型。
//
// 这些类型用于控制 Agent 的执行模式（普通/规划）和提示词注入模式（完整/精简/无），
// 对应 Python 中 openjiuwen/harness/schema/ 目录下的枚举和配置定义。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── agent_mode.go    # AgentMode 枚举及 JSON 序列化
//	└── prompt_mode.go   # PromptMode 枚举及 JSON 序列化
//
// 对应 Python 代码：openjiuwen/harness/schema/
package schema
