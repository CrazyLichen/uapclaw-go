package message_queue

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// QueueMessageBase 消息基础接口，统一 Produce 参数类型。
//
// 对齐 Python QueueMessage 继承体系：
// Python 中 InvokeQueueMessage/StreamQueueMessage 继承 QueueMessage，
// produce_message(topic, message: QueueMessage) 通过 isinstance 判断子类型。
// Go 中通过 QueueMessageBase 接口 + 类型断言实现等价语义。
type QueueMessageBase interface {
	// GetMessageID 获取消息唯一标识
	GetMessageID() string
	// SetMessageID 设置消息唯一标识（订阅端填充）
	SetMessageID(id string)
	// GetPayload 获取消息载荷
	GetPayload() map[string]any
	// GetErrorCode 获取错误码
	GetErrorCode() int
	// SetErrorCode 设置错误码
	SetErrorCode(code int)
	// GetErrorMsg 获取错误信息
	GetErrorMsg() string
	// SetErrorMsg 设置错误信息
	SetErrorMsg(msg string)
}

// SubscriptionBase 订阅基础接口。
//
// 对应 Python: SubscriptionBase(ABC)
type SubscriptionBase interface {
	// SetMessageHandler 设置消息处理回调
	SetMessageHandler(handler func(ctx context.Context, payload map[string]any) (any, error))
	// Activate 激活订阅，启动消费
	Activate()
	// Deactivate 停用订阅，停止消费
	Deactivate()
	// IsActive 返回订阅是否活跃
	IsActive() bool
}

// MessageQueueBase 消息队列基础接口。
//
// 对应 Python: MessageQueueBase(ABC)
// Produce 接收 QueueMessageBase 接口，内部通过类型断言判断同步/火忘/流式，
// 对齐 Python produce_message(topic, message) + isinstance 判断模式。
type MessageQueueBase interface {
	// Start 启动消息队列
	Start()
	// Stop 停止消息队列
	Stop(ctx context.Context) error
	// Subscribe 创建 topic 订阅，返回 SubscriptionBase 接口
	Subscribe(topic string) (SubscriptionBase, error)
	// Unsubscribe 取消 topic 订阅
	Unsubscribe(ctx context.Context, topic string) error
	// Produce 发布消息到指定 topic。
	// 消息类型决定发布模式：
	//   - QueueMessage: 火忘发布，不等待处理完成
	//   - InvokeQueueMessage: 同步发布，等待处理完成
	//   - StreamQueueMessage: 流式发布，等待流式处理结果
	//
	// 对齐 Python: produce_message(topic, message) 由 isinstance(message, InvokeQueueMessage) 判断
	Produce(ctx context.Context, topic string, msg QueueMessageBase) error
}

// QueueMessage 火忘消息，发布后不等待处理完成。
// 实现 QueueMessageBase 接口。
//
// 对应 Python: openjiuwen/core/runner/message_queue_base.py (QueueMessage)
type QueueMessage struct {
	// MessageID 消息唯一标识
	MessageID string
	// Payload 消息载荷
	Payload map[string]any
	// ErrorCode 错误码
	ErrorCode int
	// ErrorMsg 错误信息
	ErrorMsg string
}

// InvokeQueueMessage 同步消息，发布后等待处理完成。
// 实现 QueueMessageBase 接口。
//
// 对应 Python: openjiuwen/core/runner/message_queue_base.py (InvokeQueueMessage)
// Python 中 InvokeQueueMessage 继承 QueueMessage 并增加 response Future。
// Go 中使用 channel 实现等价的同步等待语义。
type InvokeQueueMessage struct {
	// MessageID 消息唯一标识
	MessageID string
	// Payload 消息载荷
	Payload map[string]any
	// ErrorCode 错误码
	ErrorCode int
	// ErrorMsg 错误信息
	ErrorMsg string
	// response 处理结果通道
	response chan invokeResponse
}

// StreamQueueMessage 流式消息，发布后等待流式处理结果。
// 实现 QueueMessageBase 接口。
//
// 对应 Python: openjiuwen/core/runner/message_queue_base.py (StreamQueueMessage)
type StreamQueueMessage struct {
	// MessageID 消息唯一标识
	MessageID string
	// Payload 消息载荷
	Payload map[string]any
	// ErrorCode 错误码
	ErrorCode int
	// ErrorMsg 错误信息
	ErrorMsg string
	// response 处理结果通道
	response chan invokeResponse
}

// invokeResponse handler 处理结果
type invokeResponse struct {
	// result 处理结果
	result any
	// err 处理错误
	err error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewQueueMessage 创建火忘消息。
func NewQueueMessage(payload map[string]any) *QueueMessage {
	return &QueueMessage{Payload: payload}
}

// NewInvokeQueueMessage 创建同步消息。
func NewInvokeQueueMessage(payload map[string]any) *InvokeQueueMessage {
	return &InvokeQueueMessage{
		Payload:  payload,
		response: make(chan invokeResponse, 1),
	}
}

// NewStreamQueueMessage 创建流式消息。
func NewStreamQueueMessage(payload map[string]any) *StreamQueueMessage {
	return &StreamQueueMessage{
		Payload:  payload,
		response: make(chan invokeResponse, 1),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// GetMessageID 实现 QueueMessageBase 接口。
func (m *QueueMessage) GetMessageID() string { return m.MessageID }

// SetMessageID 实现 QueueMessageBase 接口。
func (m *QueueMessage) SetMessageID(id string) { m.MessageID = id }

// GetPayload 实现 QueueMessageBase 接口。
func (m *QueueMessage) GetPayload() map[string]any { return m.Payload }

// GetErrorCode 实现 QueueMessageBase 接口。
func (m *QueueMessage) GetErrorCode() int { return m.ErrorCode }

// SetErrorCode 实现 QueueMessageBase 接口。
func (m *QueueMessage) SetErrorCode(code int) { m.ErrorCode = code }

// GetErrorMsg 实现 QueueMessageBase 接口。
func (m *QueueMessage) GetErrorMsg() string { return m.ErrorMsg }

// SetErrorMsg 实现 QueueMessageBase 接口。
func (m *QueueMessage) SetErrorMsg(msg string) { m.ErrorMsg = msg }

// GetMessageID 实现 QueueMessageBase 接口。
func (m *InvokeQueueMessage) GetMessageID() string { return m.MessageID }

// SetMessageID 实现 QueueMessageBase 接口。
func (m *InvokeQueueMessage) SetMessageID(id string) { m.MessageID = id }

// GetPayload 实现 QueueMessageBase 接口。
func (m *InvokeQueueMessage) GetPayload() map[string]any { return m.Payload }

// GetErrorCode 实现 QueueMessageBase 接口。
func (m *InvokeQueueMessage) GetErrorCode() int { return m.ErrorCode }

// SetErrorCode 实现 QueueMessageBase 接口。
func (m *InvokeQueueMessage) SetErrorCode(code int) { m.ErrorCode = code }

// GetErrorMsg 实现 QueueMessageBase 接口。
func (m *InvokeQueueMessage) GetErrorMsg() string { return m.ErrorMsg }

// SetErrorMsg 实现 QueueMessageBase 接口。
func (m *InvokeQueueMessage) SetErrorMsg(msg string) { m.ErrorMsg = msg }

// WaitResponse 阻塞等待 handler 处理完成。
//
// 对应 Python: await queue_message.response
func (m *InvokeQueueMessage) WaitResponse(ctx context.Context) (any, error) {
	select {
	case resp := <-m.response:
		return resp.result, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CompleteResponse handler 调用此方法通知处理完成。
//
// 对应 Python: queue_message.response.set_result(result) /
//
//	queue_message.response.set_exception(err)
func (m *InvokeQueueMessage) CompleteResponse(result any, err error) {
	select {
	case m.response <- invokeResponse{result: result, err: err}:
	default:
		// response channel 已有值，忽略重复完成
	}
}

// GetMessageID 实现 QueueMessageBase 接口。
func (m *StreamQueueMessage) GetMessageID() string { return m.MessageID }

// SetMessageID 实现 QueueMessageBase 接口。
func (m *StreamQueueMessage) SetMessageID(id string) { m.MessageID = id }

// GetPayload 实现 QueueMessageBase 接口。
func (m *StreamQueueMessage) GetPayload() map[string]any { return m.Payload }

// GetErrorCode 实现 QueueMessageBase 接口。
func (m *StreamQueueMessage) GetErrorCode() int { return m.ErrorCode }

// SetErrorCode 实现 QueueMessageBase 接口。
func (m *StreamQueueMessage) SetErrorCode(code int) { m.ErrorCode = code }

// GetErrorMsg 实现 QueueMessageBase 接口。
func (m *StreamQueueMessage) GetErrorMsg() string { return m.ErrorMsg }

// SetErrorMsg 实现 QueueMessageBase 接口。
func (m *StreamQueueMessage) SetErrorMsg(msg string) { m.ErrorMsg = msg }

// WaitResponse 阻塞等待 handler 流式处理完成。
//
// 对应 Python: await queue_message.response
func (m *StreamQueueMessage) WaitResponse(ctx context.Context) (any, error) {
	select {
	case resp := <-m.response:
		return resp.result, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CompleteResponse handler 调用此方法通知流式处理完成。
func (m *StreamQueueMessage) CompleteResponse(result any, err error) {
	select {
	case m.response <- invokeResponse{result: result, err: err}:
	default:
		// response channel 已有值，忽略重复完成
	}
}
