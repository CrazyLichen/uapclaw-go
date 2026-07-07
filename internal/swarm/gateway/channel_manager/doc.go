// Package channel_manager 提供渠道注册、管理和消息分发功能。
//
// 渠道是 Gateway 与外部客户端之间的通信通道，如 Web（WebSocket）、
// 飞书、钉钉等。ChannelManager 负责渠道的生命周期管理和消息路由。
//
// 文件目录：
//
//	channel_manager/
//	├── doc.go              # 包文档
//	├── base.go             # BaseChannel 接口 + ChannelType + ChannelMetadata
//	├── channel_manager.go  # ChannelManager 注册/分发
//	└── web/                # Web 渠道实现（WebSocket + RPC 分发 + 帧协议）
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/
package channel_manager
