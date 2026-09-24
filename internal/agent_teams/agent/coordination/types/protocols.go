package types

import (
	"context"

	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

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
	ResumeInterrupt(ctx context.Context, userInput any) error
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
	ResumePolls()
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
type DispatcherInfra interface{}

// ──────────────────────────── 非导出函数 ────────────────────────────
