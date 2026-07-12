// Package channel_manager 提供渠道注册、管理和消息分发功能。
//
// 渠道是 Gateway 与外部客户端之间的通信通道，如 Web（WebSocket）、
// 飞书、钉钉等。ChannelManager 负责渠道的生命周期管理、入站消息中转
// （存活检查+转发到 MessageHandler）和出站消息派发（按 channel_id 定向投递）。
//
// 核心接口：
//   - MessageHandlerInterface — 消息处理器接口（合并入站+出站），由 MessageHandler 实现
//
// 文件目录：
//
//	channel_manager/
//	├── doc.go              # 包文档
//	├── base.go             # BaseChannel 接口 + ChannelType + ChannelMetadata
//	├── channel_manager.go  # ChannelManager 注册/入站中转/出站派发/配置管理
//	└── web/                # Web 渠道实现（WebSocket + RPC 分发 + 帧协议）
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/
package channel_manager
