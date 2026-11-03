package dmessage_queue

import (
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// MaxRecurseDepth payload 递归深度上限。
//
// Python: message_serializer.MAX_RECURSE_DEPTH = 10
const MaxRecurseDepth = 10

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// MessageToMap 将消息对象转为带 __class__ 标记的 map。
//
// Go 独有辅助：写侧 SerializeMessage 复用它，读侧 consumeLoop 用同一形态
// 把强类型消息交给 handler（handler 契约为 map[string]any）。
//
// 仅接受 DmqRequestMessage / DmqResponseMessage；其它类型立即报错（偏差 D6，
// Python 是发出去、对面 Unknown payload class 才炸）。
func MessageToMap(msg message_queue.QueueMessageBase) (map[string]any, error) {
	switch m := msg.(type) {
	case *DmqRequestMessage:
		payload, err := serializePayload(m.Payload, 1)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"__class__":     "DmqRequestMessage",
			"message_id":    m.MessageID,
			"payload":       payload,
			"error_code":    m.ErrorCode,
			"error_msg":     m.ErrorMsg,
			"type":          string(m.Type),
			"reply_topic":   m.ReplyTopic,
			"request_id":    m.RequestID,
			"sender_id":     m.SenderID,
			"receiver_id":   m.ReceiverID,
			"enable_stream": m.EnableStream,
			"expire_at":     m.ExpireAt,
		}, nil
	case *DmqResponseMessage:
		payload, err := serializePayload(m.Payload, 1)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"__class__":   "DmqResponseMessage",
			"message_id":  m.MessageID,
			"payload":     payload,
			"error_code":  m.ErrorCode,
			"error_msg":   m.ErrorMsg,
			"type":        string(m.Type),
			"result_type": string(m.ResultType),
			"request_id":  m.RequestID,
			"sender_id":   m.SenderID,
			"receiver_id": m.ReceiverID,
			"seq":         m.Seq,
			"last_chunk":  m.LastChunk,
			"expire_at":   m.ExpireAt,
		}, nil
	default:
		return nil, exception.BuildError(
			exception.StatusMessageQueueTopicMessageProductionError,
			exception.WithParam("topic", ""),
			exception.WithParam("message", msg),
			exception.WithParam("reason", "unsupported message type for pulsar wire"),
		)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// serializePayload 递归处理 payload 值，注入 datetime 特例并做深度检查。
//
// Python: message_serializer._serialize_payload(payload, depth)
// datetime 用 RFC3339 输出，等价 Python datetime.isoformat()。
func serializePayload(v any, depth int) (any, error) {
	if depth > MaxRecurseDepth {
		return nil, fmt.Errorf("payload nested too deep (> %d)", MaxRecurseDepth)
	}

	switch val := v.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return map[string]any{
			"__type__": "datetime",
			"value":    val.Format(time.RFC3339),
		}, nil
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			converted, err := serializePayload(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[k] = converted
		}
		return out, nil
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			converted, err := serializePayload(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil
	default:
		return v, nil
	}
}
