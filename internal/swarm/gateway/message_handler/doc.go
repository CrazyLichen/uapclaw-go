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
//	└── message_handler.go  # MessageHandler 骨架实现
//
// 对应 Python 代码：jiuwenswarm/gateway/message_handler/
package message_handler
