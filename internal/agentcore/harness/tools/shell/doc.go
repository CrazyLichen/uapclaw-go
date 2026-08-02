// Package shell 提供 Shell 工具（BashTool/PowerShellTool）及其基础设施层，包括
// 权限配置与5层检查管道、注入检测、退出码语义解释、输出渲染与截断、以及 rm 目标记录。
//
// BashTool 和 PowerShellTool 是面向 LLM 的 Shell 命令执行工具，分别对齐 Python
// bash/_tool.py 和 powershell/_tool.py，提供安全守卫、语义退出码解释、智能输出截断。
// BashStreamTool 和 PowerShellStreamTool 是对应的流式版本，逐步返回命令输出块，
// 并在流结束后返回汇总渲染内容。
//
// 文件目录：
//
//	shell/
//	├── doc.go               # 包文档
//	├── bash.go              # BashTool：bash 命令执行工具（含 rm 目标记录 + deletion 检测）
//	├── bash_stream.go       # BashStreamTool：bash 命令流式执行工具
//	├── powershell.go        # PowerShellTool：PowerShell 命令执行工具
//	├── powershell_stream.go # PowerShellStreamTool：PowerShell 命令流式执行工具
//	├── permission.go        # 权限配置、5层检查管道、管道拆分、命令提取
//	├── security.go          # 注入检测（bash/PowerShell）和破坏性命令警告
//	├── semantics.go         # 退出码语义解释、命令分类
//	├── output.go            # 输出渲染、截断、大输出持久化
//	└── rm_tracker.go        # rm 目标记录 + recordRmTargetsBeforeDeletion + buildHistoryPathFromOpts
//
// 对应 Python 代码：openjiuwen/harness/tools/shell/bash/ 和 openjiuwen/harness/tools/shell/powershell/
package shell
