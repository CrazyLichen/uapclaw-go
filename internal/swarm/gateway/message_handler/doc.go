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
//	├── forward_loop.go     # 入站转发循环（forwardLoop 11步骤 + processStream + processNonStreamRequest）
//	├── outbound.go         # 出站 push 消费 + handleAgentServerPush + cron push
//	├── channel_state.go    # ChannelControlState + ChannelMode + ApplyChannelState + 复合键
//	├── convert.go          # E2AResponse→Message 转换函数（ResponseToMessage、ChunkToMessage、MergeAgentMetadata）
//	├── cancel.go           # 取消/中断逻辑（CancelAgentWorkForSession + sendInterruptResultNotification）
//	├── slash_cmd.go        # Slash 命令处理（handleChannelControl + skills/branch/rewind RPC）
//	├── at_file.go          # @file 引用解析 + @agent 提及 + ResolveStructuredAttachments
//	├── evolution.go        # Evolution 状态机（15 个方法，审批/排队/自动接受）
//	├── dispatch.go         # 派发辅助（prepareAgentDispatchMessage + nonStreamRPCMayRunParallel）
//	├── config_inject.go    # SetOutboundPipeline 注入 + getConfigRaw/updateChannelInConfig
//	├── disconnect.go       # CancelAgentSessionsOnDisconnect（断连取消）
//	└── command_parser/     # Slash 命令解析器子包
//	    ├── doc.go          # 子包文档
//	    └── slash_command.go # Slash 命令解析实现
//
// 对应 Python 代码：jiuwenswarm/gateway/message_handler/
package message_handler
