// Package prompts CLI agent 的系统提示词构建。
//
// 基于 harness 的 SystemPromptBuilder（identity / tools / safety / runtime
// 等内置节），叠加 CLI 专属动态节：environment（priority 20：CWD、平台、
// 模型、git 分支）与 project_memory（priority 120：OPENJIUWEN.md 内容，
// 用户级 + 项目级两层合并，上限 40k 字符超限截断）。
//
// 文件目录：
//
//	prompts/
//	├── doc.go          # 包文档
//	├── builder.go     # BuildSystemPrompt / LoadOpenjiuwenMd / FindProjectRoot
//	└── builder_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/prompts/builder.py（192 行）
package prompts
