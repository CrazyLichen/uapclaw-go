package messager

import (
	"context"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessagerHandler 消息处理回调函数类型。
// Python: MessagerHandler = Callable[[EventMessage], Awaitable[None]]
type MessagerHandler func(ctx context.Context, msg *events.EventMessage) error

// EventListenerHandle 事件监听器句柄，用于移除监听器时的身份识别。
// Go 中函数类型不可直接比较（==），故用递增 ID 实现引用相等语义，
// 对齐 Python list.remove(handler) 的行为。
type EventListenerHandle struct {
	// id 唯一标识
	id uint64
	// handler 实际回调
	handler MessagerHandler
}

// Handler 返回底层回调函数。
func (h *EventListenerHandle) Handler() MessagerHandler {
	return h.handler
}

// ID 返回句柄的唯一标识，用于跨包比较。
func (h *EventListenerHandle) ID() uint64 {
	return h.id
}

// Messager 团队事件消息通信接口。
// Python: Messager (openjiuwen/agent_teams/messager/messager.py)
// 解耦工具层与消息传输实现，使 TaskManager 和 MessageManager 能通过接口发布团队事件。
type Messager interface {
	// Start 启动消息传输层
	Start(ctx context.Context) error
	// Stop 停止消息传输层
	Stop(ctx context.Context) error
	// Publish 向主题发布事件消息
	Publish(ctx context.Context, topicID string, message *events.EventMessage) error
	// Subscribe 订阅主题，注册回调
	Subscribe(ctx context.Context, topicID string, handler MessagerHandler) error
	// Unsubscribe 取消订阅
	Unsubscribe(ctx context.Context, topicID string) error
	// Send 点对点发送消息给指定 agent
	Send(ctx context.Context, agentID string, message *events.EventMessage) error
	// RegisterDirectMessageHandler 注册点对点消息回调
	RegisterDirectMessageHandler(ctx context.Context, handler MessagerHandler) error
	// UnregisterDirectMessageHandler 取消注册点对点消息回调
	UnregisterDirectMessageHandler(ctx context.Context) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// eventListenerSeq 全局监听器 ID 序列
	eventListenerSeq uint64
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEventListenerHandle 创建带唯一 ID 的监听器句柄。
// Go 中函数类型不可直接比较（==），故用递增 ID 实现引用相等语义，
// 对齐 Python list.remove(handler) 的行为。
func NewEventListenerHandle(handler MessagerHandler) *EventListenerHandle {
	return &EventListenerHandle{
		id:      atomic.AddUint64(&eventListenerSeq, 1),
		handler: handler,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
