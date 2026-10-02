package types

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionAccessor 会话管理器访问接口。
// Python: host.session_manager
type SessionAccessor interface {
	// SessionController 返回会话控制器
	SessionController() SessionController
}

// SessionController 会话管理器的 Start/Pause/Stop 所需窄接口。
// 签名对齐 *SessionManager 具体类型，编译期检查通过 var _ 验证。
type SessionController interface {
	// BindSession 绑定会话
	BindSession(ctx context.Context, session any) (context.Context, error)
	// ReleaseSession 释放会话
	ReleaseSession(ctx context.Context) error
	// TeamSession 返回当前团队会话
	TeamSession() any
}

// InfraAccessor 基础设施访问接口。
// Python: host.infra
type InfraAccessor interface {
	// TeamBackendAccessor 返回 TeamBackend 访问器
	TeamBackendAccessor() TeamBackendAccessor
	// WorkspaceManager 返回工作空间管理器（可能为 nil）
	WorkspaceManager() WorkspaceAccessor
	// SetWorkspaceInitialized 标记工作空间已初始化
	SetWorkspaceInitialized()
}

// TeamBackendAccessor TeamBackend 的窄接口。
// 签名对齐 *tools.TeamBackend 具体类型，编译期检查通过 var _ 验证。
type TeamBackendAccessor interface {
	// DB 返回数据库
	DB() database.TeamDatabase
	// TeamName 返回团队名
	TeamName() string
	// IsLeader 是否是 Leader
	IsLeader() bool
	// CleanTeam 清理团队
	CleanTeam(ctx context.Context) (bool, error)
	// ListMembers 列出非 Leader 成员
	ListMembers(ctx context.Context) ([]*database.TeamMember, error)
}

// WorkspaceAccessor 工作空间管理器窄接口。
// 签名对齐 *team_workspace.TeamWorkspaceManager 具体类型。
type WorkspaceAccessor interface {
	// Initialize 初始化工作空间
	Initialize(ctx context.Context, remoteURL ...string) error
}

// ResourceAccessor 运行时资源访问接口。
// Python: host.resources
// MemoryManager() 直接返回 *memory.TeamMemoryManager 具体类型，
// 避免 HarnessAccessor 需要反向断言。
type ResourceAccessor interface {
	// MemoryManager 返回记忆管理器（可能为 nil）
	MemoryManager() *memory.TeamMemoryManager
	// HarnessAccessor 返回 Harness 访问器
	HarnessAccessor() HarnessAccessor
	// FirstIterGate 返回首轮迭代门控（可能为 nil，HUMAN_AGENT 不设置）
	// 对齐 Python: host.resources.first_iter_gate
	FirstIterGate() FirstIterGateAccessor
	// StreamController 返回流式控制器
	// 对齐 Python: host.stream_controller
	StreamController() StreamControllerAccessor
}

// FirstIterGateAccessor 首轮迭代门控窄接口。
// 签名对齐 *rails.FirstIterationGate 具体类型。
type FirstIterGateAccessor interface {
	// Wait 阻塞直到首次迭代开始
	Wait(ctx context.Context) error
	// IsReady 门是否已打开
	IsReady() bool
}

// StreamControllerAccessor 流式控制器窄接口。
// 对齐 Python: host.stream_controller（仅 finalize_round 使用的子集）
type StreamControllerAccessor interface {
	// ResetStreamQueue 释放 streamQueue 引用（设为 nil）
	// 对齐 Python: host.stream_controller.stream_queue = None
	ResetStreamQueue()
}

// HarnessAccessor Harness 窄接口。
// 签名对齐 *agentteams.TeamHarness 具体类型，编译期检查通过 var _ 验证。
type HarnessAccessor interface {
	// RegisterMemberTools 注册成员工具
	RegisterMemberTools(memMgr *memory.TeamMemoryManager)
	// InjectMemberMemory 注入成员记忆
	InjectMemberMemory(ctx context.Context, memMgr *memory.TeamMemoryManager, query string) error
	// Model 返回当前模型
	Model() *llm.Model
}

// BlueprintAccessor 蓝图扩展接口。
// Python: host.blueprint.spec
// 不嵌入 DispatcherBlueprint，因为 KernelHost 已通过 DispatcherHost 包含 Role()/MemberName()。
type BlueprintAccessor interface {
	// SpecAny 返回 TeamAgentSpec（可能为 nil），以 any 类型避免与 TeamAgent.Spec() 签名冲突
	SpecAny() any
}

// TransportAccessor 传输层访问接口。
// Python: host.infra.team_backend → messager
type TransportAccessor interface {
	// SubscribeTransport 订阅团队传输主题
	SubscribeTransport(ctx context.Context) error
	// UnsubscribeTransport 取消订阅
	UnsubscribeTransport() error
	// PublishTeamEvent 发布团队事件到 TEAM 主题
	// 对齐 Python: messager.publish(TeamTopic.TEAM.build(session_id, team_name), EventMessage.from_event(event))
	PublishTeamEvent(ctx context.Context, eventType string, payload map[string]any) error
}

// LifecycleAccessor 生命周期效果接口。
// Python: CoordinationKernel.pause/stop 中的 side-effects
type LifecycleAccessor interface {
	// PersistAllocatorState 持久化模型分配器状态
	PersistAllocatorState()
	// DrainAgentTask 等待当前 agent round 完成
	DrainAgentTask(ctx context.Context)
	// MarkLiveTeammates 标记所有活跃成员为指定状态
	MarkLiveTeammates(ctx context.Context, status string) error
	// CloseStream 关闭流
	CloseStream()
	// SetMemberID 设置成员 ID 上下文
	SetMemberID(name string)
	// Lifecycle 返回生命周期模式
	Lifecycle() string
	// CancelRecoveryTasks 取消所有恢复任务
	// 对齐 Python: host.spawn_manager.cancel_recovery_tasks()
	CancelRecoveryTasks()
	// ShutdownAllHandles 关闭所有已生成的句柄
	// 对齐 Python: host.spawn_manager.shutdown_all_handles()
	ShutdownAllHandles(ctx context.Context)
	// SpawnedHandleNames 返回已生成的句柄成员名集合（用于 MarkLiveTeammates 过滤）
	// 对齐 Python: host.spawn_manager.spawned_handles.keys()
	SpawnedHandleNames() []string
}

// KernelHost CoordinationKernel 对宿主 TeamAgent 所需的窄接口。
// 细粒度拆分避免循环依赖：每个子接口由 TeamAgent 实现。
// 与 Python CoordinationKernel.__init__(host: TeamAgent) 对应。
// 编译期检查：var _ KernelHost = (*TeamAgent)(nil)
type KernelHost interface {
	DispatcherHost
	SessionAccessor
	InfraAccessor
	ResourceAccessor
	BlueprintAccessor
	TransportAccessor
	LifecycleAccessor
	// Role 返回团队角色
	Role() schema.TeamRole
	// MemberName 返回成员名
	MemberName() string
	// RecoverTeam 恢复团队
	RecoverTeam(ctx context.Context) ([]string, error)
	// UpdateStatus 更新成员状态
	UpdateStatus(ctx context.Context, status schema.MemberStatus) error
	// TeamName 返回团队名
	TeamName() string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
