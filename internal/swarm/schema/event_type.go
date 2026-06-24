package schema

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// EventType E2A 协议事件类型枚举。
//
// 定义 AgentServer→Gateway 通信链路中所有合法的事件类型标识，
// 用于 AgentResponse/AgentResponseChunk 的 event_type 字段和 Gateway 消息路由。
// 值为点分字符串格式（如 "chat.delta"），与 Python EventType 枚举值一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (EventType)
type EventType string

const (
	// ─── 连接 ───

	// EventTypeConnectionAck 连接确认
	EventTypeConnectionAck EventType = "connection.ack"
	// EventTypeHello 握手
	EventTypeHello EventType = "hello"

	// ─── chat 流式 ───

	// EventTypeChatDelta 流式文本增量
	EventTypeChatDelta EventType = "chat.delta"
	// EventTypeChatReasoning 推理过程
	EventTypeChatReasoning EventType = "chat.reasoning"
	// EventTypeChatUsageMetadata 用量元数据
	EventTypeChatUsageMetadata EventType = "chat.usage_metadata"
	// EventTypeChatUsageSummary 用量汇总
	EventTypeChatUsageSummary EventType = "chat.usage_summary"
	// EventTypeChatFinal 最终完整响应
	EventTypeChatFinal EventType = "chat.final"
	// EventTypeChatMedia 媒体内容
	EventTypeChatMedia EventType = "chat.media"
	// EventTypeChatFile 文件内容
	EventTypeChatFile EventType = "chat.file"

	// ─── chat 工具 ───

	// EventTypeChatToolCall 工具调用
	EventTypeChatToolCall EventType = "chat.tool_call"
	// EventTypeChatToolUpdate 工具更新
	EventTypeChatToolUpdate EventType = "chat.tool_update"
	// EventTypeChatToolResult 工具结果
	EventTypeChatToolResult EventType = "chat.tool_result"

	// ─── chat 状态 ───

	// EventTypeChatProcessingStatus 处理状态
	EventTypeChatProcessingStatus EventType = "chat.processing_status"
	// EventTypeChatError 对话错误
	EventTypeChatError EventType = "chat.error"
	// EventTypeChatInterruptResult 中断结果
	EventTypeChatInterruptResult EventType = "chat.interrupt_result"
	// EventTypeChatEvolutionStatus 进化状态
	EventTypeChatEvolutionStatus EventType = "chat.evolution_status"
	// EventTypeChatSubtaskUpdate 子任务更新
	EventTypeChatSubtaskUpdate EventType = "chat.subtask_update"
	// EventTypeChatAskUserQuestion Agent 提问
	EventTypeChatAskUserQuestion EventType = "chat.ask_user_question"
	// EventTypeChatSessionResult 会话结果
	EventTypeChatSessionResult EventType = "chat.session_result"

	// ─── context ───

	// EventTypeContextUsage 上下文用量
	EventTypeContextUsage EventType = "context.usage"

	// ─── todo ───

	// EventTypeTodoUpdated 待办更新
	EventTypeTodoUpdated EventType = "todo.updated"

	// ─── team ───

	// EventTypeTeamMember 团队成员
	EventTypeTeamMember EventType = "team.member"
	// EventTypeTeamTask 团队任务
	EventTypeTeamTask EventType = "team.task"
	// EventTypeTeamMessage 团队消息
	EventTypeTeamMessage EventType = "team.message"

	// ─── heartbeat ───

	// EventTypeHeartbeatRelay 心跳中继
	EventTypeHeartbeatRelay EventType = "heartbeat.relay"

	// ─── history ───

	// EventTypeHistoryGet 历史消息
	EventTypeHistoryGet EventType = "history.message"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// eventTypeLookup 字符串值到 EventType 枚举的查找表，用于 ParseEventType/IsValidEventType 的 O(1) 查找。
var eventTypeLookup map[string]EventType

// ──────────────────────────── 导出函数 ────────────────────────────

// AllEventTypes 返回所有 EventType 枚举值。
// 用于遍历清理等场景。
func AllEventTypes() []EventType {
	return []EventType{
		// 连接
		EventTypeConnectionAck,
		EventTypeHello,
		// chat 流式
		EventTypeChatDelta,
		EventTypeChatReasoning,
		EventTypeChatUsageMetadata,
		EventTypeChatUsageSummary,
		EventTypeChatFinal,
		EventTypeChatMedia,
		EventTypeChatFile,
		// chat 工具
		EventTypeChatToolCall,
		EventTypeChatToolUpdate,
		EventTypeChatToolResult,
		// chat 状态
		EventTypeChatProcessingStatus,
		EventTypeChatError,
		EventTypeChatInterruptResult,
		EventTypeChatEvolutionStatus,
		EventTypeChatSubtaskUpdate,
		EventTypeChatAskUserQuestion,
		EventTypeChatSessionResult,
		// context
		EventTypeContextUsage,
		// todo
		EventTypeTodoUpdated,
		// team
		EventTypeTeamMember,
		EventTypeTeamTask,
		EventTypeTeamMessage,
		// heartbeat
		EventTypeHeartbeatRelay,
		// history
		EventTypeHistoryGet,
	}
}

// ParseEventType 从字符串解析 EventType，不合法返回错误。
// 使用包级查找表实现 O(1) 查找，与 Python EventType 枚举严格对齐。
func ParseEventType(s string) (EventType, error) {
	if et, ok := eventTypeLookup[s]; ok {
		return et, nil
	}
	return EventType(""), fmt.Errorf("不合法的 EventType 值: %q", s)
}

// IsValidEventType 判断字符串是否为合法的 EventType 值。
func IsValidEventType(s string) bool {
	_, ok := eventTypeLookup[s]
	return ok
}

// String 实现 fmt.Stringer 接口。
func (et EventType) String() string {
	return string(et)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (et EventType) GoString() string {
	return fmt.Sprintf("schema.EventType(%q)", string(et))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 构建查找表
	events := AllEventTypes()
	eventTypeLookup = make(map[string]EventType, len(events))
	for _, et := range events {
		eventTypeLookup[string(et)] = et
	}
}
