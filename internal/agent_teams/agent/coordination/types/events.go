package types

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InnerEventMessage 协调层内部事件消息，与跨进程 EventMessage 隔离。
// Python: InnerEventMessage
type InnerEventMessage struct {
	// EventType 事件类型
	EventType InnerEventType
	// Payload 事件载荷
	Payload map[string]any
}

// CoordinationEvent 事件总线处理的统一事件包装。
// Python: CoordinationEvent = Union[InnerEventMessage, EventMessage]
// Go 用包装结构体实现：Inner 和 Transport 恰好一个非 nil。
type CoordinationEvent struct {
	// Inner 内部事件（非 nil 时 Transport 为 nil）
	Inner *InnerEventMessage
	// Transport 跨进程事件（非 nil 时 Inner 为 nil）
	Transport *events.EventMessage
}

// ──────────────────────────── 枚举 ────────────────────────────

// InnerEventType 协调层内部事件类型枚举。
// Python: InnerEventType
type InnerEventType string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InnerEventTypeUserInput 用户输入事件
	InnerEventTypeUserInput InnerEventType = "user_input"
	// InnerEventTypePollMailbox 邮箱轮询事件
	InnerEventTypePollMailbox InnerEventType = "coordination_poll_mailbox"
	// InnerEventTypePollTask 任务轮询事件
	InnerEventTypePollTask InnerEventType = "coordination_poll_task"
	// InnerEventTypeShutdown 关闭事件
	InnerEventTypeShutdown InnerEventType = "shutdown"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IsInner 返回是否为内部事件。
func (e CoordinationEvent) IsInner() bool {
	return e.Inner != nil
}

// IsTransport 返回是否为跨进程事件。
func (e CoordinationEvent) IsTransport() bool {
	return e.Transport != nil
}

// EventType 返回事件类型字符串（用于 dispatcher 粗筛和 CallbackFramework 注册）。
func (e CoordinationEvent) EventType() string {
	if e.Inner != nil {
		return string(e.Inner.EventType)
	}
	return e.Transport.EventType
}

// ──────────────────────────── 非导出函数 ────────────────────────────
