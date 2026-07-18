// Package types 提供 swarm/server 子包间共享的类型定义，避免循环依赖。
//
// 当两个包（如 adapter 和 runtime）需要共享同一数据模型，
// 但因编译期循环依赖无法直接互导时，将共享类型提取到此包。
//
// 对齐 Python 的模式：Python 通过函数内延迟 import 绕过循环依赖，
// Go 则需要通过独立包解耦。
//
// 文件目录：
//
//	types/
//	├── doc.go               # 包文档
//	├── agent_definition.go  # AgentDefinition + AgentSource + BuiltinAgents
//	└── agent_tools.go       # DisallowedForSubagents + ToolGroups 共享常量（10.3.7）
package types
