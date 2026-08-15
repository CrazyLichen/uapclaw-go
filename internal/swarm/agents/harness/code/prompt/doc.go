// Package prompt 提供 Code 模式的系统提示词构建入口函数。
//
// 包含 8 个 Code 模式专属提示词节构建函数和 BuildCodeSystemPrompt 入口函数。
// Code 模式提示词为英文，与 Agent 模式的双语提示词不同。
//
// 文件目录：
//
//	prompt/
//	├── doc.go                   # 包文档
//	└── code_prompt_builder.go   # 8 个 section builder + BuildCodeSystemPrompt
//
// 对应 Python 代码：jiuwenswarm/agents/harness/code/prompt/code_prompt_builder.py
package prompt
