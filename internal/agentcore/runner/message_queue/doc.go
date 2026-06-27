// Package message_queue 提供基于 Go channel 的内存消息队列实现。
//
// 本包对齐 Python 的 MessageQueueInMemory，支持按 topic 路由、
// 同步/火忘发布、订阅生命周期管理。
//
// 核心概念：
//
//	MessageQueueInMemory  — 消息队列管理器，管理 topic → Subscription 映射
//	Subscription          — 订阅句柄，持有关联 topic + message handler 回调
//	QueueMessage          — 火忘消息（发布后不等待处理完成）
//	InvokeQueueMessage    — 同步消息（发布后等待 handler 处理完成）
//
// 同步发布语义：使用 InvokeQueueMessage，handler 处理完后通过
// CompleteResponse 通知调用方，调用方通过 WaitResponse 阻塞等待。
// 对应 Python 中 await queue_message.response 的语义。
//
// 文件目录：
//
//	message_queue/
//	├── doc.go        # 包文档
//	├── message.go    # QueueMessage / InvokeQueueMessage 消息类型
//	└── queue.go      # MessageQueueInMemory / Subscription 核心
//
// 对应 Python 代码：openjiuwen/core/runner/message_queue_inmemory.py
package message_queue
