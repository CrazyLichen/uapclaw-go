package message_handler

import (
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// MergeAgentMetadata 合并 Agent 响应 metadata 与网关请求 metadata。
//
// send_push / 工具链返回的响应常不带 metadata，通道（如钉钉 batchSend）需要
// 请求侧的 dingtalk_sender_id、conversation_type 等；响应中有同名字段时优先响应。
//
// 对齐 Python _merge_agent_metadata
func MergeAgentMetadata(requestMetadata, responseMetadata map[string]any) map[string]any {
	if requestMetadata == nil && responseMetadata == nil {
		return nil
	}
	if requestMetadata == nil {
		requestMetadata = make(map[string]any)
	}
	if responseMetadata == nil {
		responseMetadata = make(map[string]any)
	}
	if len(requestMetadata) == 0 && len(responseMetadata) == 0 {
		return nil
	}

	// 合并：请求 metadata 在底，响应 metadata 覆盖
	merged := make(map[string]any, len(requestMetadata)+len(responseMetadata))
	for k, v := range requestMetadata {
		merged[k] = v
	}
	for k, v := range responseMetadata {
		merged[k] = v
	}
	return merged
}

// ResponseToMessage 将 AgentResponse 转换为 Message（用于非流式处理）。
//
// 对齐 Python _response_to_message：
//  1. 合并 metadata
//  2. 提取 group_digital_avatar / enable_memory
//  3. 从 payload 提取 event_type → 如果合法 EventType 则构造事件消息
//  4. 否则构造响应消息（type=res, event_type=CHAT_FINAL）
func ResponseToMessage(resp *schema.AgentResponse, sessionID string, requestMetadata map[string]any) *schema.Message {
	metadata := MergeAgentMetadata(requestMetadata, resp.Metadata)

	// 从 metadata 中提取 group_digital_avatar 和 enable_memory 字段
	groupDigitalAvatar := false
	enableMemory := true
	if metadata != nil {
		if v, ok := metadata["group_digital_avatar"]; ok {
			groupDigitalAvatar = toBool(v)
		}
		if v, ok := metadata["enable_memory"]; ok {
			enableMemory = toBool(v)
		}
	}

	// 检查 payload 中是否包含 event_type，如果包含则创建事件消息
	if resp.Payload != nil {
		if etStr, ok := resp.Payload["event_type"]; ok {
			if s, isStr := etStr.(string); isStr && s != "" {
				et := schema.EventType(s)
				if schema.IsValidEventType(string(et)) {
					// 合法事件类型 → 构造事件消息
					return &schema.Message{
						ID:                 resp.RequestID,
						Type:               schema.MessageTypeEvent,
						ChannelID:          resp.ChannelID,
						SessionID:          sessionID,
						Timestamp:          schema.NowTimestamp(),
						OK:                 true,
						Payload:            resp.Payload,
						EventType:          et,
						Metadata:           metadata,
						GroupDigitalAvatar: groupDigitalAvatar,
						EnableMemory:       enableMemory,
						EnableStreaming:    true,
					}
				}
			}
		}
	}

	// 普通响应消息
	return &schema.Message{
		ID:                 resp.RequestID,
		Type:               schema.MessageTypeRes,
		ChannelID:          resp.ChannelID,
		SessionID:          sessionID,
		Timestamp:          schema.NowTimestamp(),
		OK:                 resp.OK,
		Payload:            resp.Payload,
		EventType:          schema.EventTypeChatFinal,
		Metadata:           metadata,
		GroupDigitalAvatar: groupDigitalAvatar,
		EnableMemory:       enableMemory,
		EnableStreaming:    true,
	}
}

// ChunkToMessage 将 AgentResponseChunk 转换为 Message（用于流式处理）。
//
// metadata 传入 request 的 metadata，供 Feishu/Xiaoyi 等通道回发时使用平台身份。
//
// 对齐 Python _chunk_to_message
func ChunkToMessage(chunk *schema.AgentResponseChunk, sessionID string, metadata map[string]any) *schema.Message {
	// 从 metadata 中提取 group_digital_avatar 和 enable_memory 字段
	groupDigitalAvatar := false
	enableMemory := true
	if metadata != nil {
		if v, ok := metadata["group_digital_avatar"]; ok {
			groupDigitalAvatar = toBool(v)
		}
		if v, ok := metadata["enable_memory"]; ok {
			enableMemory = toBool(v)
		}
	}

	// 从 payload 中提取 event_type（如果存在）
	var eventType schema.EventType
	if chunk.Payload != nil {
		if etStr, ok := chunk.Payload["event_type"]; ok {
			if s, isStr := etStr.(string); isStr && s != "" {
				et := schema.EventType(s)
				if schema.IsValidEventType(string(et)) {
					eventType = et
				}
			}
		}
	}

	return &schema.Message{
		ID:                 chunk.RequestID,
		Type:               schema.MessageTypeEvent,
		ChannelID:          chunk.ChannelID,
		SessionID:          sessionID,
		Timestamp:          schema.NowTimestamp(),
		OK:                 true,
		Payload:            chunk.Payload,
		EventType:          eventType,
		Metadata:           metadata,
		GroupDigitalAvatar: groupDigitalAvatar,
		EnableMemory:       enableMemory,
		EnableStreaming:    true,
	}
}

// IsTerminalStreamChunk 识别仅用于结束流的哨兵 chunk，避免被当作业务事件继续下发。
//
// 对齐 Python _is_terminal_stream_chunk
func IsTerminalStreamChunk(chunk *schema.AgentResponseChunk) bool {
	return chunk.IsTerminal()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toBool 将 any 值转为 bool（对齐 Python bool() 语义）
func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		return val != ""
	default:
		return v != nil
	}
}
