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

// ──────────────────────────── 非导出函数 ────────────────────────────

// SessionAccessor 会话管理器访问接口。
// Python: host.session_manager
type SessionAccessor interface {
	// SessionController 返回会话控制器
	SessionController() SessionController
}

// SessionController 会话管理器的 Start/Pause/Stop 所需窄接口。
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
type TeamBackendAccessor interface {
	// DB 返回数据库访问器
	DB() DBAccessor
	// TeamName 返回团队名
	TeamName() string
	// IsLeader 是否是 Leader
	IsLeader() bool
	// CleanTeam 清理团队
	CleanTeam(ctx context.Context) error
	// ListMembers 列出非 Leader 成员
	ListMembers(ctx context.Context) ([]any, error)
}

// DBAccessor 数据库窄接口。
type DBAccessor interface {
	// Initialize 初始化数据库
	Initialize(ctx context.Context) error
	// Team 返回团队表访问器
	Team() TeamTableAccessor
}

// TeamTableAccessor 团队表窄接口。
type TeamTableAccessor interface {
	// GetTeam 获取团队信息
	GetTeam(ctx context.Context, teamName string) (any, error)
}

// WorkspaceAccessor 工作空间管理器窄接口。
type WorkspaceAccessor interface {
	// Initialize 初始化工作空间
	Initialize(ctx context.Context, remoteURL string) error
}

// ResourceAccessor 运行时资源访问接口。
// Python: host.resources
type ResourceAccessor interface {
	// MemoryManager 返回记忆管理器（可能为 nil）
	MemoryManager() MemoryAccessor
	// HarnessAccessor 返回 Harness 访问器（可能为 nil）
	HarnessAccessor() HarnessAccessor
}

// MemoryAccessor 记忆管理器窄接口。
type MemoryAccessor interface {
	// InitToolkit 初始化记忆工具包
	InitToolkit(ctx context.Context) (bool, error)
	// SetExtractionModel 设置抽取模型
	SetExtractionModel(model any)
	// ExtractionModel 返回抽取模型
	ExtractionModel() any
	// Close 关闭记忆管理器
	Close()
}

// HarnessAccessor Harness 窄接口。
type HarnessAccessor interface {
	// RegisterMemberTools 注册成员工具
	RegisterMemberTools(memMgr MemoryAccessor)
	// InjectMemberMemory 注入成员记忆
	InjectMemberMemory(ctx context.Context, memMgr MemoryAccessor) error
	// Model 返回当前模型
	Model() any
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
}

// KernelHost CoordinationKernel 对宿主 TeamAgent 所需的窄接口。
// 细粒度拆分避免循环依赖：每个子接口由 TeamAgent 实现。
// 与 Python CoordinationKernel.__init__(host: TeamAgent) 对应。
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
