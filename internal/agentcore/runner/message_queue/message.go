package message_queue

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// QueueMessage 火忘消息，发布后不等待处理完成。
//
// 对应 Python: openjiuwen/core/runner/message_queue_base.py (QueueMessage)
type QueueMessage struct {
	// Payload 消息载荷
	Payload map[string]any
}

// InvokeQueueMessage 同步消息，发布后等待处理完成。
//
// 对应 Python: openjiuwen/core/runner/message_queue_base.py (InvokeQueueMessage)
// Python 中 InvokeQueueMessage 继承 QueueMessage 并增加 response Future。
// Go 中使用 channel 实现等价的同步等待语义。
type InvokeQueueMessage struct {
	// Payload 消息载荷
	Payload map[string]any
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
