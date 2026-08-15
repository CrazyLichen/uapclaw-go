package messager

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessagerHandler 消息处理回调函数类型。
// 对齐 Python: MessagerHandler = Callable[[EventMessage], Awaitable[None]]
type MessagerHandler func(ctx context.Context, msg *schema.EventMessage) error

// Messager 团队事件消息通信接口。
// 对齐 Python: Messager (openjiuwen/agent_teams/messager/messager.py)
// 解耦工具层与消息传输实现，使 TaskManager 和 MessageManager 能通过接口发布团队事件。
type Messager interface {
	// Start 启动消息传输层
	Start(ctx context.Context) error
	// Stop 停止消息传输层
	Stop(ctx context.Context) error
	// Publish 向主题发布事件消息
	Publish(ctx context.Context, topicID string, message *schema.EventMessage) error
	// Subscribe 订阅主题，注册回调
	Subscribe(ctx context.Context, topicID string, handler MessagerHandler) error
	// Unsubscribe 取消订阅
	Unsubscribe(ctx context.Context, topicID string) error
	// Send 点对点发送消息给指定 agent
	Send(ctx context.Context, agentID string, message *schema.EventMessage) error
	// RegisterDirectMessageHandler 注册点对点消息回调
	RegisterDirectMessageHandler(ctx context.Context, handler MessagerHandler) error
	// UnregisterDirectMessageHandler 取消注册点对点消息回调
	UnregisterDirectMessageHandler(ctx context.Context) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
