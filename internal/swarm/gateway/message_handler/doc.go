// Package message_handler 提供消息处理器，负责入站消息转发和出站消息分发。
//
// MessageHandler 是 Gateway 中 Channel 与 AgentServer 之间的桥梁：
// - 入站方向：接收 Channel 投递的用户消息，转换为 E2A 信封发送到 AgentServer
// - 出站方向：从 AgentServer 接收响应，转换为 Message 分发到 Channel
//
// 文件目录：
//
//	message_handler/
//	├── doc.go              # 包文档
//	├── message_handler.go  # MessageHandler 核心结构（双 channel、transport、state）
//	├── forward_loop.go     # 入站转发循环（forwardLoop + processStream + processNonStreamRequest）
//	├── outbound.go         # 出站循环 + push 消费 + cron push
//	├── channel_state.go    # ChannelControlState + ChannelMode + ApplyChannelState
//	├── convert.go          # E2AResponse→Message 转换函数（ResponseToMessage、ChunkToMessage）
//	├── cancel.go           # 取消/中断逻辑（CancelAgentWorkForSession）
//	├── slash_cmd.go        # Slash 命令处理（handleChannelControl）
//	├── at_file.go          # @file 引用解析 + @agent 提及
//	└── command_parser/     # Slash 命令解析器子包
//	    ├── doc.go
//	    └── slash_command.go
//
// 对应 Python 代码：jiuwenswarm/gateway/message_handler/
package message_handler
