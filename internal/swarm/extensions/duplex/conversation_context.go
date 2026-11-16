package duplex

import (
	"context"
	"errors"
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

// HistoryID 双工历史去重元数据键。
// 对齐 conversation_context.py:13 HISTORY_ID。
const HistoryID = "duplex_history_id"

// MessageOrigin 常量（openjiuwen 消息来源元数据）。
// 对齐 conversation_context.py:6-10 的三个 schema 常量。
const (
	MessageOriginMetadata        = "origin"
	MessageOriginExternalUser    = "external_user"
	MessageSourceKindMetadata    = "source_kind"
	MessageSourceKindVideoDuplex = "video_duplex"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConversationRecord 会话历史记录的最小形状。
// 对齐 conversation_context.py:18-31 conversation_record_id 的读取字段。
type ConversationRecord struct {
	// ChannelID 渠道标识
	ChannelID string
	// Role 角色
	Role string
	// EventType 事件类型
	EventType string
	// Presentation 呈现方式
	Presentation string
	// ID 记录 ID
	ID string
	// RequestID 请求 ID
	RequestID string
	// Content 内容
	Content string
}

// HistoryLoader 会话历史加载桥（网关装配层注入）。
// Python 侧无对应实现：Python 直接 import session_history 模块，Go 窄接口注入。
type HistoryLoader interface {
	// FlushPendingWrites 落盘未写历史（超时 5 秒）
	FlushPendingWrites(ctx context.Context) error
	// LoadHistoryRecords 加载会话历史
	LoadHistoryRecords(ctx context.Context, sessionID string) ([]ConversationRecord, error)
}

// DuplexContextAgent 共享 chat Agent 的窄接口（依赖倒置允许场景）。
// Python 侧无对应接口：Python 鸭子类型 agent.react_agent.context_engine。
type DuplexContextAgent interface {
	// GetContext 取会话上下文；nil 表示冷创建（由 warmup 从磁盘恢复）
	GetContext(ctx context.Context, sessionID string) (DuplexConversationContext, error)
}

// DuplexConversationContext 上下文引擎的窄接口。
type DuplexConversationContext interface {
	// Messages 现有消息（用于回填已消费 ID 集合）
	Messages(ctx context.Context) []map[string]any
	// AddMessages 追加消息（含元数据）
	AddMessages(ctx context.Context, messages []map[string]any) error
	// DuplexHistoryIDs 已消费的双工历史 ID 集合（Core 压缩窗口后仍保留）
	DuplexHistoryIDs() map[string]bool
	// SetDuplexHistoryIDs 写回 ID 集合
	SetDuplexHistoryIDs(seen map[string]bool)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ConversationRecordID 判定可导入的双工记录身份；不可导入返回空串。
// 对齐 conversation_context.py:18-31 conversation_record_id。
func ConversationRecordID(record map[string]any) string {
	if toString(record["channel_id"]) != "video_duplex" {
		return ""
	}
	role := toString(record["role"])
	if role == "user" {
		return orChainStr(record["id"], record["request_id"])
	}
	if role == "assistant" && toString(record["event_type"]) == "chat.final" &&
		toString(record["presentation"]) != "tool_result" {
		return orChainStr(record["id"], record["request_id"])
	}
	return ""
}

// SyncConversationContext 把持久化的实时语音导入共享 chat Agent。
// 对齐 conversation_context.py:33-66 sync_conversation_context。
func SyncConversationContext(ctx context.Context, agent DuplexContextAgent, sessionID string, loader HistoryLoader) error {
	if agent == nil {
		return nil // react_agent 缺失即跳过（对齐 :37-39）
	}
	conversation, err := agent.GetContext(ctx, sessionID)
	if err != nil {
		return err
	}
	if conversation == nil {
		return nil // 冷创建经 warmup 从磁盘恢复（对齐 :42 注释）
	}
	if err := loader.FlushPendingWrites(ctx); err != nil {
		return errors.New("语音对话历史尚未保存，请稍后重试。")
	}
	records, err := loader.LoadHistoryRecords(ctx, sessionID)
	if err != nil {
		return err
	}
	// Core 压缩消息窗口后仍保留已消费 ID（对齐 :47 注释）
	seen := conversation.DuplexHistoryIDs()
	if seen == nil {
		seen = map[string]bool{}
		for _, message := range conversation.Messages(ctx) {
			metadata := toMap(message["metadata"])
			if id := toString(metadata[HistoryID]); id != "" {
				seen[id] = true
			}
		}
		conversation.SetDuplexHistoryIDs(seen)
	}
	for _, record := range records {
		recordMap := map[string]any{
			"channel_id": record.ChannelID, "role": record.Role,
			"event_type": record.EventType, "presentation": record.Presentation,
			"id": record.ID, "request_id": record.RequestID, "content": record.Content,
		}
		identity := ConversationRecordID(recordMap)
		content := record.Content
		if identity == "" || seen[identity] {
			continue
		}
		if trimSpace(content) == "" {
			continue
		}
		role := record.Role
		metadata := map[string]any{HistoryID: identity}
		if role == "user" {
			metadata[MessageOriginMetadata] = MessageOriginExternalUser
			metadata[MessageSourceKindMetadata] = MessageSourceKindVideoDuplex
		}
		message := map[string]any{
			"role": role, "content": content, "metadata": metadata,
			"message_type": messageTypeFor(role),
		}
		if err := conversation.AddMessages(ctx, []map[string]any{message}); err != nil {
			return err
		}
		seen[identity] = true
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// messageTypeFor 消息类型标记。
// 对齐 conversation_context.py:55 UserMessage/AssistantMessage。
func messageTypeFor(role string) string {
	if role == "user" {
		return "user"
	}
	return "assistant"
}

// trimSpace strings.TrimSpace 别名（避免 video_voice 之外重复 import）。
func trimSpace(s string) string {
	out := s
	for len(out) > 0 && (out[0] == ' ' || out[0] == '\t' || out[0] == '\n' || out[0] == '\r') {
		out = out[1:]
	}
	for len(out) > 0 && (out[len(out)-1] == ' ' || out[len(out)-1] == '\t' || out[len(out)-1] == '\n' || out[len(out)-1] == '\r') {
		out = out[:len(out)-1]
	}
	return out
}

var _ = fmt.Sprintf
