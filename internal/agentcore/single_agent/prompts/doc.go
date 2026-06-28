// Package prompts 提供基于节的系统提示词构建器，支持多语言内容和优先级排序。
//
// SystemPromptBuilder 将多个命名的、带优先级的、多语言的提示词片段（PromptSection）
// 组装为完整的系统提示词，供 LLM 调用使用。节按 Priority 升序排列，
// 按 Language 渲染，用 "\n\n" 拼接。
//
// 扩展机制：sectionsFilter 函数字段允许调用方在 Build 时过滤节，
// 例如 harness 层的 PromptMode（FULL/MINIMAL/NONE）过滤。
//
// 文件目录：
//
//	prompts/
//	├── doc.go           # 包文档
//	└── builder.go       # PromptSection + SystemPromptBuilder + 常量
//
// 对应 Python 代码：openjiuwen/core/single_agent/prompts/
package prompts
