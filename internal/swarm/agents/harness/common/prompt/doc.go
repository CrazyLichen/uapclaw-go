// Package prompt 提供 Agent 模式（通用）的系统提示词构建入口函数。
//
// 包含 Agent 身份提示词构建入口和响应节（消息格式说明）构建函数。
// 这些函数由 adapter 层调用，在 Agent 创建时构建系统提示词。
//
// 文件目录：
//
//	prompt/
//	├── doc.go               # 包文档
//	├── prompt_builder.go    # BuildResponseSection + BuildAgentIdentityPrompt + readWorkspaceFile
//	└── prompt_builder_test.go # 单元测试
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/prompt/prompt_builder.py
package prompt
