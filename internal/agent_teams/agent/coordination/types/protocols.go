package types

import (
	"context"

	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentRoundController Round 级控制面，与 TeamHarness 打交道。
// Python: AgentRoundController(Protocol)
type AgentRoundController interface {
	// IsAgentReady 返回 agent 是否已完成初始化
	IsAgentReady() bool
	// IsAgentRunning 返回 agent 是否在活跃 round 中
	IsAgentRunning() bool
	// HasInFlightRound 返回是否有已调度但未完成的 round
	HasInFlightRound() bool
	// HasPendingInterrupt 返回是否有未解决的工具中断
	HasPendingInterrupt() bool
	// CancelAgent 取消运行中的 agent 任务
	CancelAgent(ctx context.Context) error
	// DeliverInput 确保内容到达 DeepAgent，不论当前状态
	DeliverInput(ctx context.Context, content any, useSteer bool) error
	// ResumeInterrupt 以结构化输入恢复 HITL 中断
	// Python: resume_interrupt(user_input: Any) — 实际调用始终传 InteractiveInput
	ResumeInterrupt(ctx context.Context, userInput *interaction.InteractiveInput) error
}

// TeamLifecycleController TeamAgent 级生命周期效果，跨多个 manager。
// Python: TeamLifecycleController(Protocol)
type TeamLifecycleController interface {
	// ShutdownSelf 响应团队解散强制关闭自身
	ShutdownSelf(ctx context.Context) error
	// ConcludeCompletedRound 发出 team-completed 标记块，关闭 leader 流
	ConcludeCompletedRound(ctx context.Context, memberCount, taskCount int) error
}

// PollController EventBus 自身的周期轮询控制面。
// Python: PollController(Protocol)
// Handler 直接操作此接口而非通过 host 中转——EventBus 已知道如何暂停/恢复自己的 poll 任务。
type PollController interface {
	// PausePolls 暂停事件总线的周期轮询
	PausePolls()
	// ResumePolls 恢复事件总线的周期轮询
	ResumePolls(ctx context.Context)
}

// DispatcherHost 组合 host 契约，供 kernel 和 dispatcher 使用。
// Python: DispatcherHost(AgentRoundController, TeamLifecycleController, Protocol)
type DispatcherHost interface {
	AgentRoundController
	TeamLifecycleController
}

// DispatcherBlueprint Dispatcher 构造所需的 blueprint 接口。
// 从 TeamAgentBlueprint 中提取 dispatcher 实际需要的窄接口。
type DispatcherBlueprint interface {
	// Role 返回团队角色
	Role() schema.TeamRole
	// MemberName 返回成员名
	MemberName() string
}

// DispatcherInfra Dispatcher 构造所需的 infra 接口。
// 从 TeamInfra 中提取 dispatcher 实际需要的窄接口。
// 对齐 Python: EventDispatcher.__init__(infra: TeamInfra)
type DispatcherInfra interface {
	// TaskManager 返回任务管理器（可能为 nil）
	TaskManager() DispTaskManager
	// MessageManager 返回消息管理器（可能为 nil）
	MessageManager() DispMessageManager
	// TeamBackend 返回团队后端（可能为 nil）
	TeamBackend() DispTeamBackend
	// Messager 返回消息总线（可能为 nil）
	Messager() DispMessager
}

// DispTaskManager Dispatcher 级任务管理器窄接口。
// 与 lifecycle_accessors.go 中的 TeamBackendAccessor 区分：
// DispTaskManager 用于 EventDispatcher handler，TeamBackendAccessor 用于 KernelHost 生命周期。
type DispTaskManager interface {
	// ListTasks 列出所有任务
	ListTasks(ctx context.Context) ([]TaskBrief, error)
	// Get 获取指定 ID 的任务
	Get(ctx context.Context, taskID string) (TaskDetail, error)
}

// DispMessageManager Dispatcher 级消息管理器窄接口。
type DispMessageManager interface {
	// MarkMessageRead 标记消息已读
	MarkMessageRead(ctx context.Context, messageID, memberName string) error
	// SendMessage 发送消息给指定成员
	SendMessage(ctx context.Context, content any, recipient string) error
	// ListUnread 列出未读消息
	ListUnread(ctx context.Context, memberName string) ([]UnreadMessage, error)
}

// DispTeamBackend Dispatcher 级团队后端窄接口。
// 注意：与 lifecycle_accessors.go 中的 TeamBackendAccessor 不同，此处仅包含 handler 所需方法。
type DispTeamBackend interface {
	// IsTeamCompleted 检查团队是否已完成
	IsTeamCompleted(ctx context.Context) (*TeamCompletionSnapshot, error)
	// IsHumanAgent 检查指定成员是否为 human-agent
	IsHumanAgent(memberName string) bool
	// TeamName 返回团队名称
	TeamName() string
}

// DispMessager Dispatcher 级消息总线窄接口。
type DispMessager interface {
	// PublishEvent 发布团队事件
	PublishEvent(ctx context.Context, eventType string, payload map[string]any) error
}

// TaskBrief 任务简要信息。
type TaskBrief struct {
	// ID 任务标识
	ID string
	// Title 任务标题
	Title string
	// Assignee 任务分配对象
	Assignee string
	// Status 任务状态
	Status string
}

// TaskDetail 任务详情。
type TaskDetail struct {
	// ID 任务标识
	ID string
	// Title 任务标题
	Title string
	// Assignee 任务分配对象
	Assignee string
	// Status 任务状态
	Status string
	// ClaimedAt 任务认领时间
	ClaimedAt int64
	// CreatedAt 任务创建时间
	CreatedAt int64
}

// UnreadMessage 未读消息。
type UnreadMessage struct {
	// MessageID 消息标识
	MessageID string
	// Sender 发送者
	Sender string
	// Content 消息内容
	Content string
}

// TeamCompletionSnapshot 团队完成快照。
type TeamCompletionSnapshot struct {
	// MemberCount 成员数
	MemberCount int
	// TaskCount 任务数
	TaskCount int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
