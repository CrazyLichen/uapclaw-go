// Package prompts 提供 Agent 簇（harness）的系统提示词构建器扩展，
// 在 single_agent/prompts 基础上增加 PromptMode 模式过滤和诊断报告能力。
//
// SystemPromptBuilder 继承基础构建器，根据 PromptMode（FULL/MINIMAL/NONE）
// 过滤节后拼接系统提示词；PromptReport 生成提示词诊断统计；
// sanitize 系列函数防御提示词注入攻击。
//
// 文件目录：
//
//	prompts/
//	├── doc.go           # 包文档
//	├── builder.go       # harness 扩展 SystemPromptBuilder + 语言/模式解析
//	├── report.go        # PromptReport 提示词诊断报告
//	└── sanitize.go      # 提示词注入防御函数
//
// 对应 Python 代码：openjiuwen/harness/prompts/
package prompts
