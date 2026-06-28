// Package message_queue 提供消息队列接口及内存/本地实现。
//
// 本包对齐 Python 的 MessageQueueBase + MessageQueueInMemory + LocalMessageQueue，
// 支持按 topic 路由、同步/火忘发布、订阅生命周期管理。
//
// 核心概念：
//
//	QueueMessageBase   — 消息基础接口（GetMessageID/GetPayload 等），统一 Produce 参数类型
//	MessageQueueBase   — 消息队列基础接口（Start/Stop/Subscribe/Unsubscribe/Produce）
//	SubscriptionBase   — 订阅基础接口（SetMessageHandler/Activate/Deactivate/IsActive）
//	MessageQueueInMemory — 基于 Go channel 的内存消息队列实现
//	LocalMessageQueue   — no-op 本地消息队列（start/stop 空操作）
//	QueueMessage        — 火忘消息（发布后不等待处理完成），实现 QueueMessageBase
//	InvokeQueueMessage  — 同步消息（发布后等待 handler 处理完成），实现 QueueMessageBase
//	StreamQueueMessage  — 流式消息（发布后等待流式处理结果），实现 QueueMessageBase
//
// Produce 统一入口：MessageQueueBase.Produce(ctx, topic, QueueMessageBase)。
// 消息类型决定发布模式，对齐 Python produce_message(topic, message) + isinstance 判断：
//   - *QueueMessage: 火忘发布
//   - *InvokeQueueMessage: 同步发布，handler 完成后 CompleteResponse 通知
//   - *StreamQueueMessage: 流式发布，handler 完成后 CompleteResponse 通知
//
// 文件目录：
//
//	message_queue/
//	├── doc.go        # 包文档
//	├── base.go       # MessageQueueBase / SubscriptionBase 接口 + QueueMessage / InvokeQueueMessage / StreamQueueMessage
//	├── local.go      # LocalMessageQueue / LocalSubscription（no-op 实现）
//	└── queue.go      # MessageQueueInMemory / Subscription 核心实现
//
// 对应 Python 代码：
//
//	openjiuwen/core/runner/message_queue_base.py      （接口 + QueueMessage/InvokeQueueMessage/StreamQueueMessage）
//	openjiuwen/core/runner/message_queue_inmemory.py  （MessageQueueInMemory + SubscriptionInMemory）
//	openjiuwen/core/runner/message_queue_base.py      （LocalMessageQueue no-op）
package message_queue
