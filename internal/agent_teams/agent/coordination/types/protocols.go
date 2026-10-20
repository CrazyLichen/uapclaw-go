package types

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
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
// S-32: 签名对齐 *TeamAgentBlueprint 具体类型——Role() 直接匹配；
// 成员名在具体类型上是 MemberName() *string（nil 表示未设置），
// 接口方法命名为 DispatcherMemberName() string 以便具体类型直接满足
// （Go 无方法重载，不能同名共存）。
type DispatcherBlueprint interface {
	// Role 返回团队角色
	Role() schema.TeamRole
	// DispatcherMemberName 返回成员名（空串表示未设置）
	DispatcherMemberName() string
}

// DispatcherInfra Dispatcher 构造所需的 infra 接口。
// S-32: 签名对齐 *TeamInfra 具体类型的真实方法——TaskManager/MessageManager/
// TeamBackend/Messager 直接返回具体类型（*tools.TeamTaskManager 等），
// 不再使用 Disp* 窄接口（具体类型本就按 Python 一比一移植）。
// 对齐 Python: EventDispatcher.__init__(infra: TeamInfra)
type DispatcherInfra interface {
	// TaskManager 返回任务管理器（可能为 nil）
	TaskManager() *tools.TeamTaskManager
	// MessageManager 返回消息管理器（可能为 nil）
	MessageManager() *tools.TeamMessageManager
	// TeamBackend 返回团队后端（可能为 nil）
	TeamBackend() *tools.TeamBackend
	// Messager 返回消息总线（可能为 nil）
	Messager() messager.Messager
}
