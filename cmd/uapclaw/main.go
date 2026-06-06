//go:build !test

// Package main 提供 uapclaw 统一 CLI 入口。
//
// uapclaw 是 Agent 平台的命令行工具，支持以下子命令：
//   - chat: CLI REPL 聊天模式（REPL 作为 Channel 接入 Gateway）
//   - serve: HTTP REST API + SSE 流式服务（HTTP 作为 Channel 接入 Gateway）
//   - app: 完整模式（IM/Web 等全部 Channel 接入 Gateway）
//   - agentserver: 仅启动 AgentServer（独立进程，监听 WS，等待外部 Gateway 连入）
//   - gateway: 仅启动 Gateway（独立进程，通过 WS 连接外部 AgentServer）
//   - web: 启动 Web UI（Web Channel 接入 Gateway）
//   - init: 初始化工作区
//   - acp: ACP stdio JSON-RPC 协议模式（stdio 作为 Channel 接入 Gateway）
//
// 架构要点：
//   - 所有外部入口统一经过 Gateway，Gateway 与 AgentServer 之间始终走 E2A 协议
//   - 进程内（chat/serve/acp/app/web）：Gateway 通过 ChannelTransport（Go channel）连 AgentServer
//   - 跨进程（agentserver + gateway 独立部署）：Gateway 通过 WebSocketTransport 连 AgentServer
//   - 命令区分的只是进程组合方式和 Channel 类型，底层通信路径统一
//
// 详细架构决策见 IMPLEMENTATION_PLAN.md "Gateway-AgentServer 通信架构决策" 章节。
package main

import "os"

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行根命令
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func main() {
	Execute()
}
