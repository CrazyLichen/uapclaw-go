package session

import (
	"context"
	"sync"
)

// ──────────────────────────── 接口 ────────────────────────────

// SessionActivityParticipant 会话活动事件参与者。
// Python: runtime/session_lifecycle.py SessionActivityParticipant
type SessionActivityParticipant interface {
	// SessionInputIntent 用户意图继续会话
	SessionInputIntent(ctx context.Context, event SessionInputIntentEvent) (SessionInputIntentDisposition, error)
	// ForegroundChanged 前台可见性变更
	ForegroundChanged(ctx context.Context, event SessionForegroundEvent) error
	// ExecutionStarted 执行开始
	ExecutionStarted(ctx context.Context, event SessionExecutionEvent) error
	// ExecutionFinished 执行结束
	ExecutionFinished(event SessionExecutionFinishedEvent)
	// SessionInactive 会话空闲
	SessionInactive(ctx context.Context, event SessionInactiveEvent) error
}

// SessionDeleteParticipant 会话永久删除参与者。
// Python: runtime/session_lifecycle.py SessionDeleteParticipant
type SessionDeleteParticipant interface {
	// Name 参与者名称（日志用）
	Name() string
	// BeforeDelete 破坏性删除前静默依赖工作
	BeforeDelete(ctx context.Context, target SessionLifecycleTarget) error
	// ReleaseResources 释放会话资源
	ReleaseResources(ctx context.Context, target SessionLifecycleTarget) error
	// DeleteFailed 删除失败通知（destructiveStarted 表示破坏性操作是否已开始）
	DeleteFailed(ctx context.Context, target SessionLifecycleTarget, destructiveStarted bool) error
	// DeleteCommitted 删除已提交通知
	DeleteCommitted(ctx context.Context, target SessionLifecycleTarget) error
}

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
	RemoveResponseWaiter(ctx context.Context, exec ExecutionLifecycleContext, reason string) error
	// PauseSession 暂停一个团队会话代次，返回是否暂停或排空
	PauseSession(ctx context.Context, sess SessionLifecycleContext, reason string, channelID string) (bool, error)
	// StopSession 停止一个团队会话代次
	StopSession(ctx context.Context, sess SessionLifecycleContext, reason string) error
	// PublishPauseCompleted 通过存活的会话观察者发布暂停完成
	PublishPauseCompleted(ctx context.Context, sessionID string, generation *int, channelID string) error
	// IsRunning 一个团队代次是否拥有活跃工作
	IsRunning(sess SessionLifecycleContext) bool
}

// ──────────────────────────── 结构体 ────────────────────────────

// SessionDescriptor 传输无关的持久会话身份与路由元数据。
// Python: runtime/session_lifecycle.py SessionDescriptor
type SessionDescriptor struct {
	// SessionID 会话标识
	SessionID string
	// ChannelID 渠道标识
	ChannelID string
	// Mode 运行模式
	Mode string
	// WorkMode 工作模式
	WorkMode string
	// ProjectID 项目标识
	ProjectID string
	// ProjectDir 项目目录
	ProjectDir string
	// UserID 用户标识
	UserID string
}

// SessionLifecycleTarget 活动事件与永久删除事件共享的已解析身份。
// Python: runtime/session_lifecycle.py SessionLifecycleTarget
type SessionLifecycleTarget struct {
	// Descriptor 会话描述符
	Descriptor SessionDescriptor
	// Kind 业务种类
	Kind SessionKind
	// TeamName 团队名（team 会话才有）
	TeamName string
}

// SessionInputIntentEvent 用户意图继续会话，与消费者无关。
// Python: runtime/session_lifecycle.py SessionInputIntentEvent
type SessionInputIntentEvent struct {
	// Target 会话身份
	Target SessionLifecycleTarget
	// HasHistory 是否已有历史
	HasHistory bool
	// ViewID 视图标识
	ViewID string
	// IntentID 意图标识
	IntentID string
}

// SessionForegroundEvent 会话前台可见性变更。
// Python: runtime/session_lifecycle.py SessionForegroundEvent
type SessionForegroundEvent struct {
	// Target 会话身份
	Target SessionLifecycleTarget
	// ViewID 视图标识
	ViewID string
	// Visible 是否可见
	Visible bool
	// HasHistory 是否已有历史
	HasHistory bool
}

// SessionExecutionEvent 会话内一次执行开始。
// Python: runtime/session_lifecycle.py SessionExecutionEvent
type SessionExecutionEvent struct {
	// Target 会话身份
	Target SessionLifecycleTarget
	// RequestID 请求标识
	RequestID string
	// HasHistory 是否已有历史
	HasHistory bool
}

// SessionExecutionFinishedEvent 会话内一次执行结束。
// Python: runtime/session_lifecycle.py SessionExecutionFinishedEvent
type SessionExecutionFinishedEvent struct {
	// Target 会话身份
	Target SessionLifecycleTarget
	// RequestID 请求标识
	RequestID string
	// Succeeded 是否成功
	Succeeded bool
}

// SessionInactiveEvent 会话已无活跃工作。
// Python: runtime/session_lifecycle.py SessionInactiveEvent
type SessionInactiveEvent struct {
	// Target 会话身份
	Target SessionLifecycleTarget
}

// RuntimeParticipantRegistry 单个 AgentRuntime 内的参与者登记表。
// Python: runtime/session_lifecycle.py RuntimeParticipantRegistry
//
// 上游是单线程 asyncio 故无锁；Go 有并发 goroutine，用 RWMutex 保护
// （spec §8 D-7），语义不变。
type RuntimeParticipantRegistry struct {
	// mu 保护以下字段
	mu sync.RWMutex
	// activity 活动参与者快照
	activity []SessionActivityParticipant
	// deleteParticipants 删除参与者快照
	deleteParticipants []SessionDeleteParticipant
	// targets 会话标识 → 已解析身份
	targets map[string]SessionLifecycleTarget
}

// ──────────────────────────── 枚举 ────────────────────────────

// SessionKind 生命周期参与者使用的稳定业务种类。
// Python: runtime/session_lifecycle.py SessionKind
type SessionKind string

const (
	// SessionKindAgent 单 agent 会话
	SessionKindAgent SessionKind = "agent"
	// SessionKindTeam 团队会话
	SessionKindTeam SessionKind = "team"
)

// SessionInputIntentDisposition 输入意图的处置结果。
// Python: runtime/session_lifecycle.py SessionInputIntentDisposition
type SessionInputIntentDisposition string

const (
	// SessionInputIntentDispositionScheduled 已安排
	SessionInputIntentDispositionScheduled SessionInputIntentDisposition = "scheduled"
	// SessionInputIntentDispositionNotNeeded 无需安排
	SessionInputIntentDispositionNotNeeded SessionInputIntentDisposition = "not_needed"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRuntimeParticipantRegistry 创建空的参与者登记表。
// Python: RuntimeParticipantRegistry.__init__
func NewRuntimeParticipantRegistry() *RuntimeParticipantRegistry {
	return &RuntimeParticipantRegistry{
		targets: map[string]SessionLifecycleTarget{},
	}
}

// ReplaceSessionParticipants 整体替换活动与删除参与者。
// Python: RuntimeParticipantRegistry.replace_session_participants
func (r *RuntimeParticipantRegistry) ReplaceSessionParticipants(
	activity []SessionActivityParticipant,
	deleteParticipants []SessionDeleteParticipant,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activity = append([]SessionActivityParticipant(nil), activity...)
	r.deleteParticipants = append([]SessionDeleteParticipant(nil), deleteParticipants...)
}

// SnapshotActivity 返回活动参与者快照（副本）。
// Python: RuntimeParticipantRegistry.snapshot_activity
func (r *RuntimeParticipantRegistry) SnapshotActivity() []SessionActivityParticipant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]SessionActivityParticipant(nil), r.activity...)
}

// SnapshotDelete 返回删除参与者快照（副本）。
// Python: RuntimeParticipantRegistry.snapshot_delete
func (r *RuntimeParticipantRegistry) SnapshotDelete() []SessionDeleteParticipant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]SessionDeleteParticipant(nil), r.deleteParticipants...)
}

// RememberTarget 记住已解析的会话身份。
// Python: RuntimeParticipantRegistry.remember_target
func (r *RuntimeParticipantRegistry) RememberTarget(target SessionLifecycleTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targets[target.Descriptor.SessionID] = target
}

// Target 取已记住的会话身份（返回副本，nil 表示未记住）。
// Python: RuntimeParticipantRegistry.target
func (r *RuntimeParticipantRegistry) Target(sessionID string) *SessionLifecycleTarget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.targets[sessionID]
	if !ok {
		return nil
	}
	return &t
}

// ForgetTarget 遗忘某个会话身份。
// Python: RuntimeParticipantRegistry.forget_target
func (r *RuntimeParticipantRegistry) ForgetTarget(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.targets, sessionID)
}

// ClearTargets 清空全部已记住身份。
// Python: RuntimeParticipantRegistry.clear_targets
func (r *RuntimeParticipantRegistry) ClearTargets() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targets = map[string]SessionLifecycleTarget{}
}
