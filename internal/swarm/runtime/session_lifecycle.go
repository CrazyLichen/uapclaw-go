package runtime

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// 会话生命周期端口与描述符。
// Python: runtime/session_lifecycle.py（port/lease/descriptor 三符号；本文件与
// Python 顶层位置一比一）。participant/target/event 半边因被 session 包内
// session_delete.go 消费而留在 runtime/session/session_lifecycle.go（上移会
// 使 session → swruntime 成环），spec D-25.3-4 已登记。

// ──────────────────────────── 接口 ────────────────────────────

// RuntimeResourceLease Runtime 持有的资源租约。
// Python: runtime/session_lifecycle.py RuntimeResourceLease
type RuntimeResourceLease interface {
	// Release 释放资源
	Release(ctx context.Context) error
}

// SessionRuntimeLifecyclePort 由 AgentRuntime 编排的团队域生命周期动作。
// Python: runtime/session_lifecycle.py SessionRuntimeLifecyclePort
type SessionRuntimeLifecyclePort interface {
	// RemoveResponseWaiter 移除当前请求拥有的响应等待者
	RemoveResponseWaiter(ctx context.Context, exec session.ExecutionLifecycleContext, reason string) error
	// PauseSession 暂停一个团队会话代次，返回是否暂停或排空
	PauseSession(ctx context.Context, sess session.SessionLifecycleContext, reason string, channelID string) (bool, error)
	// StopSession 停止一个团队会话代次
	StopSession(ctx context.Context, sess session.SessionLifecycleContext, reason string) error
	// PublishPauseCompleted 通过存活的会话观察者发布暂停完成
	PublishPauseCompleted(ctx context.Context, sessionID string, generation *int, channelID string) error
	// IsRunning 一个团队代次是否拥有活跃工作
	IsRunning(sess session.SessionLifecycleContext) bool
}


// ──────────────────────────── 结构体 ────────────────────────────

