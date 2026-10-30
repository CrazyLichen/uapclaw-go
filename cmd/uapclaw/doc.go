// Package main 提供 uapclaw 统一 CLI 入口。
//
// uapclaw 是 Agent 平台的命令行工具，所有外部入口统一经过 Gateway，
// Gateway 与 AgentServer 之间始终走 E2A 协议。
// 进程内通过 ChannelTransport（Go channel）通信，跨进程通过 WebSocketTransport 通信。
//
// 支持以下子命令：
//   - chat: CLI REPL 聊天模式（REPL 作为 Channel 接入 Gateway）
//   - serve: HTTP REST API + SSE 流式服务（HTTP 作为 Channel 接入 Gateway）
//   - app: 完整模式（AgentServer + Gateway 同进程，IM/Web 等全部 Channel 接入）
//   - agentserver: 仅启动 AgentServer（独立进程，监听 WS，等待外部 Gateway 连入）
//   - gateway: 仅启动 Gateway（独立进程，通过 WS + E2A 连接外部 AgentServer）
//   - web: 启动 Web UI（Web Channel 接入 Gateway）
//   - init: 初始化工作区
//   - acp: ACP stdio JSON-RPC 协议模式（stdio 作为 Channel 接入 Gateway）
//   - acp-chat: 外部 ACP 子代理冒烟入口（一次性 connect→chat→close，10.4.6）
//   - spawn-child: 作为子进程运行 Agent（内部命令，不应直接调用）
//
// 文件目录：
//
//	uapclaw/
//	├── doc.go           # 包文档
//	├── cmd.go           # 命令定义：newRootCmd / newChatCmd / newServeCmd / newAppCmd 等
//	├── acp_cmd.go       # ACP 子命令实现：runAcpCmd / resolveAcpGatewayURL / bootstrapInprocServers
//	├── acp_chat_cmd.go  # acp-chat 冒烟入口：runAcpChat / acpChatDeps / exitCodeFor（10.4.6）
//	└── main.go          # Execute() + main()（//go:build !test 隔离，不参与覆盖率）
//
// 对应 Python 代码：无（Go 特有 CLI 入口）
package main
