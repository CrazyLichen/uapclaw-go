// Package prompts 提供记忆系统的提示词模板及运行时加载器。
//
// 本包实现 PromptApplier 单例，运行时从 .md 文件加载提示词模板并缓存，
// 支持变量替换后输出完整提示词文本。4 个 .md 模板文件从 Python 项目 1:1 复制，
// 不做翻译，保持原始语言。
//
// 文件目录：
//
//	prompts/
//	├── doc.go                      # 包文档
//	├── prompt_applier.go           # PromptApplier 单例（运行时读文件 + 缓存）
//	├── fragment_memory_prompt.md   # 碎片记忆提取提示词
//	├── memory_analysis_prompt.md   # 记忆分析提示词
//	├── memory_update_check.md      # 记忆冲突检查提示词
//	└── semantic_validation.md      # 语义一致性校验提示词
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/prompts/
package prompts
