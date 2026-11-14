// Package harness_providers 提供第三方 agent harness 的 provider 实现与适配工具。
//
// 本包对应 Python 的 openjiuwen/harness_providers/：把 Claude Code、Codex、
// DeepSeek Harness 等外部 CLI 的 SDK 对象投影到 harness_protocol 的协议值对象上。
//
// 包结构：
//
//	本包（基座）：base.go / factory.go / inputs.go / io_adapter.go /
//	mcp_naming.go / stream.go / jsonsafe.go
//	子包：native/（in-process DeepAgent 的协议实现，15.6）
//
// 依赖方向：本包可 import harness_protocol；反之禁止（协议层不得依赖 provider）。
//
// 文件目录：
//
//	harness_providers/
//	├── doc.go        # 包文档（本文件）
//	├── base.go       # SerializedTurnHarness 基座（事件发射、轮次监督、checkpoint 发布）
//	├── factory.go    # provider 注册表与构造
//	├── inputs.go     # HarnessInput 文本提取
//	├── io_adapter.go # 跨进程 IO 适配
//	├── mcp_naming.go # MCP server 命名规范化
//	├── stream.go     # 有界事件缓冲与单消费者游标
//	├── jsonsafe.go   # SDK 对象 → JSON 安全值的递归归一化
//	└── native/      # in-process DeepAgent 协议实现（deepagent provider）
//
// 对应 Python 代码：openjiuwen/harness_providers/
package harness_providers
