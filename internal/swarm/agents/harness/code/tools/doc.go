// Package tools 提供 code 模式的 todo 工具卡片（CC 对齐文案与 schema）。
//
// 与 agentcore 的 todo 工具实现分离：工具行为复用
// `internal/agentcore/harness/tools/todo`，本包只覆盖卡片文案与入参 schema。
//
// 文件目录：
//
//	tools/
//	├── doc.go             # 包文档
//	└── code_todo_tools.go # code 模式 todo 卡片描述与 schema
//
// 对应 Python 代码：jiuwenswarm/agents/harness/code/tools/code_todo_tools.py
// 与 jiuwenswarm/agents/harness/code/prompt/code_todo_tool_prompts.py
package tools
