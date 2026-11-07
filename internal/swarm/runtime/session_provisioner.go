package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// ──────────────────────────── 接口 ────────────────────────────

// SessionAgentManager provisioner 依赖的 agent 管理器能力。
//
// Python: jiuwenswarm/server/runtime/agent_manager.py 的 5 个方法
type SessionAgentManager interface {
	// ClaimPrewarmedSession 认领预热会话
	ClaimPrewarmedSession(ctx context.Context, req PrewarmClaimRequest) (PrewarmClaim, error)
	// ActivateSessionPrewarm 激活已认领的预热会话
	ActivateSessionPrewarm(sessionID string)
	// ReleaseSessionPrewarmClaim 释放认领（失败补偿）
	ReleaseSessionPrewarmClaim(ctx context.Context, sessionID string) error
	// CreateSession 新建会话
	CreateSession(ctx context.Context, channelID, sessionID string) (string, error)
	// GetAgentNoWait 非阻塞取 agent
	GetAgentNoWait(channelID string) SessionAgent
}

// SessionPlanController provisioner 依赖的计划模式控制能力。
//
// Python: PlanModeController.reset_session
type SessionPlanController interface {
	// ResetSession 清空会话计划状态
	ResetSession(sessionID string)
}

// TrajectoryDeleteHook 轨迹库删除钩子。
//
// Python: jiuwenswarm/observability/session_delete.py 三个函数
// spec §10 R-3：Go 侧为桩实现，等领域二十二 trajectory 落地后回填。
type TrajectoryDeleteHook interface {
	// BeginSessionDelete 排空写入并加围栏
	BeginSessionDelete(ctx context.Context, sessionID string) error
	// AbortSessionDelete 回滚围栏
	AbortSessionDelete(ctx context.Context, sessionID string) error
	// CommitSessionDelete 提交并删除库文件
	CommitSessionDelete(ctx context.Context, sessionID string) error
}

// TeamBindingUnbinder 团队会话绑定解绑器。
//
// Python: server/runtime/team_binding_store.get_team_binding_store().unbind_session
// spec §10 R-1：Go 侧为桩实现。
type TeamBindingUnbinder interface {
	// UnbindSession 解绑会话
	UnbindSession(teamName, sessionID string) error
}

// SessionLifecycleJournal 会话生命周期操作日志（删除可恢复性）。
//
// Python: server/runtime/session/lifecycle.py 的 state / update
// spec §10 R-4：Go 侧为桩实现，recovering 恒 false。
type SessionLifecycleJournal interface {
	// RecoveringDelete 该会话是否处于未完成的删除操作
	RecoveringDelete(sessionID string) bool
	// MarkCleanup 标记删除进入清理阶段
	MarkCleanup(sessionID string) error
}

// SessionDeleteLifecycle 参与会话删除的可选 Runtime 能力。
//
// Python: runtime/session_provisioner.py SessionDeleteLifecycle
type SessionDeleteLifecycle interface {
	// BeginSessionDelete 破坏性删除前静默依赖工作
	BeginSessionDelete(sessionID string) error
	// AbortSessionDelete 删除失败后恢复依赖工作
	AbortSessionDelete(sessionID string, channelID string) error
	// CommitSessionDelete 会话消失后应用依赖删除策略
	CommitSessionDelete(sessionID string) error
}

// SessionAgent provisioner 需要的 agent 能力面。
//
// Python: AgentManager.get_agent_nowait 返回的 agent（仅用 ensure_instance / card）
type SessionAgent interface {
	// EnsureInstance 确保实例就绪
	EnsureInstance(ctx context.Context) (any, error)
	// Card 返回 agent 卡片
	Card() any
}

// TeamSwitchPreparer 团队会话归属准备能力。
//
// Python: TeamManager.prepare_session_switch
type TeamSwitchPreparer interface {
	// PrepareSessionSwitch 准备目标会话的团队归属切换
	PrepareSessionSwitch(ctx context.Context, targetSessionID, previousSessionID, reason string) error
}

// SessionOps 会话复制与元数据访问能力。
//
// Python: agents/harness/common/session_ops_service.py 的三个函数
type SessionOps interface {
	// ForkSession 复制持久历史与 metadata
	ForkSession(params ForkSessionParams) (ForkSessionResult, error)
	// CopySessionContext 复制内存上下文
	CopySessionContext(ctx context.Context, agent SessionAgent, sourceID, targetID string, forceHistory bool) error
	// CopySessionState 复制 checkpoint state
	CopySessionState(ctx context.Context, sourceID, targetID string, card any, agent SessionAgent) error
	// GetSessionMetadata 读会话元数据
	GetSessionMetadata(sessionID string) map[string]any
	// SessionDirHasMetadata 会话目录下是否有 metadata.json
	SessionDirHasMetadata(sessionID string) bool
	// InitSessionMetadata 初始化会话元数据
	InitSessionMetadata(params InitMetadataParams) error
	// SetSessionModelSelection 写会话模型选择
	SetSessionModelSelection(sessionID string, selection map[string]any) error
	// HistoryExists 会话是否有历史
	HistoryExists(sessionID string) bool
}

// ──────────────────────────── 结构体 ────────────────────────────

// provisionOwnerToken 租约归属凭据。
//
// 用带字段的结构体而非 struct{}：Go 对零大小对象会复用地址，
// 两个 provisioner 的 &struct{}{} 可能相等，无法区分归属。
type provisionOwnerToken struct {
	// seq 非零填充，保证每个实例有独立地址
	seq byte
}

// PrewarmClaimRequest 预热池认领请求。
//
// Python: AgentManager.claim_prewarmed_session 关键字参数
type PrewarmClaimRequest struct {
	// ChannelID 渠道标识
	ChannelID string
	// ProjectID 项目标识
	ProjectID string
	// ProjectDir 项目目录
	ProjectDir string
	// WorkMode 工作模式
	WorkMode string
	// IsSwarm 是否团队
	IsSwarm bool
	// PersistSession 是否持久化
	PersistSession bool
	// PrewarmEligible 是否可走预热
	PrewarmEligible bool
	// CreateToken 认领令牌
	CreateToken string
	// MoaEnabled 多模型协作开关
	MoaEnabled bool
}

// PrewarmClaim 预热池认领结果。
type PrewarmClaim struct {
	// SessionID 会话标识
	SessionID string
	// PrewarmHit 是否命中
	PrewarmHit bool
	// PrewarmStatus 预热状态
	PrewarmStatus string
}

// ForkSessionParams fork 参数。
//
// Python: session_ops_service.fork_session 关键字参数
type ForkSessionParams struct {
	// SourceSessionID 源会话标识
	SourceSessionID string
	// TargetSessionID 目标会话标识
	TargetSessionID string
	// Title 标题
	Title string
	// ChannelID 渠道标识
	ChannelID string
	// CutoffMessageID 截断消息标识
	CutoffMessageID string
	// CutoffRole 截断消息角色
	CutoffRole string
	// CutoffContent 截断消息内容
	CutoffContent string
	// CutoffTimestamp 截断消息时间戳
	CutoffTimestamp any
	// SessionEquipmentOverride 会话设备覆盖
	SessionEquipmentOverride map[string]any
}

// ForkSessionResult fork 结果。
type ForkSessionResult struct {
	// SourceSessionID 源会话标识
	SourceSessionID string
	// SessionID 目标会话标识
	SessionID string
	// Title 标题
	Title string
	// MoaEnabled 多模型协作开关
	MoaEnabled bool
}

// InitMetadataParams 会话元数据初始化参数。
//
// Python: session_metadata.init_session_metadata 关键字参数
type InitMetadataParams struct {
	// SessionID 会话标识
	SessionID string
	// ChannelID 渠道标识
	ChannelID string
	// UserID 用户标识
	UserID string
	// Title 标题
	Title string
	// Mode 归一化模式
	Mode string
	// ProjectDir 项目目录
	ProjectDir string
	// ProjectID 项目标识
	ProjectID string
	// PersistSession 是否持久化
	PersistSession bool
	// WorkMode 工作模式
	WorkMode string
	// Model 模型名
	Model string
	// MoaEnabled 多模型协作开关
	MoaEnabled bool
	// CronID 定时任务标识
	CronID string
	// ChannelMetadata 渠道元数据
	ChannelMetadata map[string]any
}

// SessionCreateInput create 的归一化业务输入，不含任何传输对象。
//
// Python: runtime/session_provisioner.py SessionCreateInput
type SessionCreateInput struct {
	// ChannelID 渠道标识
	ChannelID string
	// RequestedSessionID 请求指定的会话标识（仅 TUI 兼容路径允许非空）
	RequestedSessionID string
	// PreviousSessionID 上一个会话标识
	PreviousSessionID string
	// CreateToken 预热认领令牌
	CreateToken string
	// PersistSession 是否持久化
	PersistSession bool
	// PersistSessionSupplied 调用方是否显式传了 PersistSession
	PersistSessionSupplied bool
	// Mode 运行模式
	Mode string
	// PreviousMode 上一模式
	PreviousMode string
	// IsSwarm 是否团队模式
	IsSwarm bool
	// TeamHint 团队提示
	TeamHint bool
	// ProjectID 项目标识
	ProjectID string
	// ProjectDir 项目目录
	ProjectDir string
	// Cwd 工作目录
	Cwd string
	// WorkMode 工作模式
	WorkMode string
	// WorkModeExplicit 是否显式指定工作模式
	WorkModeExplicit *bool
	// Title 标题
	Title string
	// UserID 用户标识
	UserID string
	// ModelName 模型名
	ModelName string
	// ModelSelection 模型选择
	ModelSelection map[string]any
	// MoaEnabled 多模型协作开关
	MoaEnabled *bool
	// CronID 定时任务标识
	CronID string
}

// SessionSwitchInput 恢复或切换会话的归一化业务输入。
//
// Python: runtime/session_provisioner.py SessionSwitchInput
type SessionSwitchInput struct {
	// ChannelID 渠道标识
	ChannelID string
	// TargetSessionID 目标会话标识
	TargetSessionID string
	// PreviousSessionID 上一个会话标识
	PreviousSessionID string
	// Mode 运行模式
	Mode string
	// PreviousMode 上一模式
	PreviousMode string
	// TeamHint 团队提示
	TeamHint bool
}

// SessionForkInput 复制一个持久会话的归一化业务输入。
//
// Python: runtime/session_provisioner.py SessionForkInput
type SessionForkInput struct {
	// ChannelID 渠道标识
	ChannelID string
	// SourceSessionID 源会话标识
	SourceSessionID string
	// TargetSessionID 目标会话标识（空则由 AgentManager 生成）
	TargetSessionID string
	// Title 标题
	Title string
	// CutoffMessageID 截断消息标识
	CutoffMessageID string
	// CutoffRole 截断消息角色
	CutoffRole string
	// CutoffContent 截断消息内容
	CutoffContent string
	// CutoffTimestamp 截断消息时间戳
	CutoffTimestamp any
	// SessionEquipmentOverride 会话设备覆盖
	SessionEquipmentOverride map[string]any
}

// SessionCreateResult 成功 create 的结果（未经 Server 线协议映射）。
//
// Python: runtime/session_provisioner.py SessionCreateResult
type SessionCreateResult struct {
	// ChannelID 渠道标识
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// ProjectID 项目标识
	ProjectID string
	// ProjectDir 项目目录
	ProjectDir string
	// WorkMode 工作模式
	WorkMode string
	// PersistSession 是否持久化
	PersistSession bool
	// PrewarmHit 是否命中预热池
	PrewarmHit bool
	// PrewarmStatus 预热状态
	PrewarmStatus string
	// Created 是否新建（false 表示复用已有 metadata）
	Created bool
	// CanonicalMode 归一化模式
	CanonicalMode string
	// ExplicitIDCompatibility 是否走 TUI 显式 ID 兼容路径
	ExplicitIDCompatibility bool
	// MoaEnabled 多模型协作开关
	MoaEnabled bool
}

// SessionSwitchResult 成功 switch 的结果。
//
// Python: runtime/session_provisioner.py SessionSwitchResult
type SessionSwitchResult struct {
	// ChannelID 渠道标识
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// Mode 运行模式
	Mode string
	// Switched 是否切换
	Switched bool
	// MoaEnabled 多模型协作开关
	MoaEnabled bool
}

// SessionForkResult 成功 fork 的结果。
//
// Python: runtime/session_provisioner.py SessionForkResult
type SessionForkResult struct {
	// ChannelID 渠道标识
	ChannelID string
	// SourceSessionID 源会话标识
	SourceSessionID string
	// SessionID 目标会话标识
	SessionID string
	// Title 标题
	Title string
	// MoaEnabled 多模型协作开关
	MoaEnabled bool
}

// SessionProvisionCommitContext 提交期由调用方提供的不透明上下文。
//
// Python: runtime/session_provisioner.py SessionProvisionCommitContext
// 唯一字段是前台作用域标识；它**不是**会话身份。
type SessionProvisionCommitContext struct {
	// ForegroundScopeID 前台作用域标识（服务端视图身份）
	ForegroundScopeID string
}

// SessionProvisionError 传输无关的业务失败。
//
// Python: runtime/session_provisioner.py SessionProvisionError
type SessionProvisionError struct {
	// Message 错误说明
	Message string
	// Code 可选错误码（与上游一致：可能为空）
	Code string
}

// SessionProvisionStateError 租约被错误的拥有者或顺序终结。
//
// Python: runtime/session_provisioner.py SessionProvisionStateError
type SessionProvisionStateError struct {
	// Message 错误说明
	Message string
}

// PreparedSessionProvision 一次成功 prepare 返回的不透明结果租约。
//
// Python: runtime/session_provisioner.py PreparedSessionProvision
// 调用方只能看 Result / State / CommitTiming；commit/abort 由持有 ownerToken 的
// provisioner 串行执行（互斥锁保证）。
type PreparedSessionProvision struct {
	// mu 串行化 commit/abort
	mu sync.Mutex
	// ownerToken 归属凭据
	ownerToken *provisionOwnerToken
	// result 不可变领域结果
	result any
	// commitTiming 提交时机
	commitTiming SessionProvisionCommitTiming
	// commitContext 首次成功进入 COMMITTING 时保存的上下文
	commitContext *SessionProvisionCommitContext
	// commitHook 提交钩子
	commitHook func(SessionProvisionCommitContext) error
	// abortHook 中止钩子
	abortHook func() error
	// commitAttemptIsTerminal 提交尝试是否不可回滚
	commitAttemptIsTerminal bool
	// state 当前状态
	state SessionProvisionState
}

// ProvisionerDeps provisioner 构造依赖。
//
// spec §3 D4/D5/D6：全部依赖以字段注入，接口与上游调用点一一对应。
type ProvisionerDeps struct {
	// AgentManager agent 管理器
	AgentManager SessionAgentManager
	// PlanController 计划模式控制器
	PlanController SessionPlanController
	// TrajectoryHook 轨迹库删除钩子（桩）
	TrajectoryHook TrajectoryDeleteHook
	// TeamBinding 团队会话绑定解绑器（桩）
	TeamBinding TeamBindingUnbinder
	// LifecycleJournal 生命周期日志（桩）
	LifecycleJournal SessionLifecycleJournal
	// NowFunc 时钟（默认 time.Now）
	NowFunc func() time.Time
	// ParticipantRegistry 参与者登记表
	ParticipantRegistry *sessionpkg.RuntimeParticipantRegistry
	// TeamExecutionController 团队执行控制面
	TeamExecutionController sessionpkg.TeamExecutionController
	// DeleteLifecycle 删除生命周期参与者
	DeleteLifecycle SessionDeleteLifecycle
}

// RuntimeSessionProvisioner 单个 Runtime 的会话供给事务编排器。
//
// Python: runtime/session_provisioner.py RuntimeSessionProvisioner
type RuntimeSessionProvisioner struct {
	// AgentManager agent 管理器
	AgentManager SessionAgentManager
	// PlanController 计划模式控制器
	PlanController SessionPlanController
	// TrajectoryHook 轨迹库删除钩子
	TrajectoryHook TrajectoryDeleteHook
	// TeamBinding 团队绑定解绑器
	TeamBinding TeamBindingUnbinder
	// LifecycleJournal 生命周期日志
	LifecycleJournal SessionLifecycleJournal
	// TeamExecutionController 团队执行控制面
	TeamExecutionController sessionpkg.TeamExecutionController
	// DeleteLifecycle 删除生命周期参与者
	DeleteLifecycle SessionDeleteLifecycle
	// TeamDeletionGate 团队删除围栏（nil 时用全局）
	TeamDeletionGate *sessionpkg.TeamDeletionGate
	// CheckpointEnsure 持久化检查点器可用性检查（删除前置）
	CheckpointEnsure func(ctx context.Context) error
	// RunnerRelease 释放 Runner 会话（agent 删除末步）
	RunnerRelease func(ctx context.Context, sessionID string) error
	// DeleteAgentTeam 删除团队 Runner（team 删除末步）
	DeleteAgentTeam func(ctx context.Context, teamName string, sessionIDs []string) (bool, error)
	// RemoveMetadataCache 清理会话元数据内存缓存
	RemoveMetadataCache func(sessionID string)

	// mu 保护以下全部可变状态
	mu sync.Mutex
	// provisionOwnerToken 租约归属凭据
	provisionOwnerToken *provisionOwnerToken
	// nowFunc 时钟
	nowFunc func() time.Time
	// participants 参与者登记表
	participants *sessionpkg.RuntimeParticipantRegistry
	// preparedDeletes 会话标识 → 已准备删除的参与者
	preparedDeletes map[string]SessionDeleteLifecycle
	// pendingDeleteMeta 会话标识 → 待提交删除的 metadata
	pendingDeleteMeta map[string]map[string]any
	// completedDeletes 最近成功删除结果的有界缓存
	completedDeletes *recentDeleteResults[sessionpkg.SessionDeleteResult]
	// completedTeamDeletes 最近成功团队删除结果的有界缓存
	completedTeamDeletes *recentDeleteResults[sessionpkg.TeamDeleteResult]
	// backgroundTasks 后台任务等待组（create 的 KVC 派发）
	backgroundTasks sync.WaitGroup
	// externalCreateLocks 显式 TUI 会话创建锁
	externalCreateLocks sync.Map
	// extra 可选扩展能力（work_mode 解析、项目绑定、会话目录操作等）
	extra provisionerExtra
}

// provisionerExtra provisioner 的可选扩展字段。
//
// 用独立结构体承载，避免主结构体字段过多；装配方通过 Set* 方法注入。
type provisionerExtra struct {
	// teamManagerProvider 团队管理器获取
	teamManagerProvider teamManagerProviderFn
	// SessionOps 会话复制与元数据能力
	SessionOps SessionOps
	// ResolveWorkMode 工作模式与项目绑定解析
	ResolveWorkMode func(params map[string]any, channelID string) projectBinding
	// ResolveCanonicalMode 归一化模式解析
	ResolveCanonicalMode func(params map[string]any, workMode string) (canonical string, isTeam bool)
	// ResolveMoa MOA 开关与设置解析
	ResolveMoa func(params map[string]any, canonicalMode string, isCron bool) (enabled bool, err error)
	// PrewarmModelEligible 模型是否可走预热
	PrewarmModelEligible func(modelName string) bool
	// FindOrCreateProject TUI 项目查找或创建
	FindOrCreateProject func(params map[string]any) (projectBinding, bool)
	// IsValidSessionID 会话标识合法性
	IsValidSessionID func(sessionID string) bool
	// ResolveSessionDir 解析会话目录（返回目录路径与是否存在）
	ResolveSessionDir func(sessionID string) (string, bool)
	// RemoveSessionDir 删除会话目录
	RemoveSessionDir func(sessionDir string) error
	// DeletePending 该会话是否有未完成的删除操作（保持围栏）
	DeletePending func(sessionID string) bool
	// ListTeamSessions 列出团队下全部会话标识
	ListTeamSessions func(teamName string) []string
	// SessionTeamName 取会话所属团队名
	SessionTeamName func(sessionID string) string
}

// teamManagerProviderFn 团队管理器获取函数。
type teamManagerProviderFn func(channelID string) TeamSwitchPreparer

// projectBinding 项目绑定解析结果。
type projectBinding struct {
	// ProjectID 项目标识
	ProjectID string
	// ProjectDir 项目目录
	ProjectDir string
	// WorkMode 请求的工作模式
	WorkMode string
	// ProjectWorkMode 项目自身的工作模式
	ProjectWorkMode string
	// HasExplicitWorkMode 是否显式指定工作模式
	HasExplicitWorkMode bool
	// Error 错误说明
	Error string
	// Code 错误码
	Code string
}

// recentDeleteResults 有界重试缓存（60 秒 TTL / 256 条上限）。
//
// Python: runtime/session_provisioner.py _RecentDeleteResults
// 不是已删除身份的持久记录，仅用于重试窗口内幂等。
type recentDeleteResults[T any] struct {
	// mu 保护 entries 与 order
	mu sync.Mutex
	// capacity 条目上限
	capacity int
	// ttl 存活时长
	ttl time.Duration
	// now 时钟注入（spec D10 / D-8）
	now func() time.Time
	// entries 键 → (过期时刻, 结果)
	entries map[string]recentDeleteEntry[T]
	// order 插入顺序（FIFO 淘汰）
	order []string
}

// recentDeleteEntry 缓存条目。
type recentDeleteEntry[T any] struct {
	// expiresAt 过期时刻
	expiresAt time.Time
	// value 结果
	value T
}

// ──────────────────────────── 枚举 ────────────────────────────

// SessionProvisionState 已准备操作的可观测状态。
//
// Python: runtime/session_provisioner.py SessionProvisionState
type SessionProvisionState string

const (
	// SessionProvisionStatePrepared 已准备
	SessionProvisionStatePrepared SessionProvisionState = "prepared"
	// SessionProvisionStateCommitting 提交中
	SessionProvisionStateCommitting SessionProvisionState = "committing"
	// SessionProvisionStateAborting 中止中
	SessionProvisionStateAborting SessionProvisionState = "aborting"
	// SessionProvisionStateCommitted 已提交
	SessionProvisionStateCommitted SessionProvisionState = "committed"
	// SessionProvisionStateAborted 已中止
	SessionProvisionStateAborted SessionProvisionState = "aborted"
)

// SessionProvisionCommitTiming 相对"结果投递"的提交位置。
//
// Python: runtime/session_provisioner.py SessionProvisionCommitTiming
type SessionProvisionCommitTiming string

const (
	// SessionProvisionCommitTimingBeforeResultDelivery 结果投递前提交
	SessionProvisionCommitTimingBeforeResultDelivery SessionProvisionCommitTiming = "before_result_delivery"
	// SessionProvisionCommitTimingAfterResultDelivery 结果投递后提交
	SessionProvisionCommitTimingAfterResultDelivery SessionProvisionCommitTiming = "after_result_delivery"
)

const (
	// SessionCreateCommitTiming create 的提交时机（KVC 派发在响应之后）
	SessionCreateCommitTiming = SessionProvisionCommitTimingAfterResultDelivery
	// SessionSwitchCommitTiming switch 的提交时机（前台切换在响应之前）
	SessionSwitchCommitTiming = SessionProvisionCommitTimingBeforeResultDelivery
	// SessionForkCommitTiming fork 的提交时机（无后置副作用）
	SessionForkCommitTiming = SessionProvisionCommitTimingBeforeResultDelivery
	// legacyWorkModeMismatchSeparator 上游 work_mode 不匹配文案的 37 空格分隔符
	legacyWorkModeMismatchSeparator = "                                     "
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *SessionProvisionError) Error() string { return e.Message }

// Error 实现 error 接口。
func (e *SessionProvisionStateError) Error() string { return e.Message }

// Result 返回 prepare 期间生成的不可变领域结果。
//
// Python: PreparedSessionProvision.result
func (p *PreparedSessionProvision) Result() any { return p.result }

// State 返回当前两阶段状态。
//
// Python: PreparedSessionProvision.state
func (p *PreparedSessionProvision) State() SessionProvisionState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// CommitTiming 返回调用方必须使用的提交时机。
//
// Python: PreparedSessionProvision.commit_timing
func (p *PreparedSessionProvision) CommitTiming() SessionProvisionCommitTiming {
	return p.commitTiming
}

// NewEmptyCommitContext 构造零值提交上下文。
func NewEmptyCommitContext() SessionProvisionCommitContext {
	return SessionProvisionCommitContext{}
}

// NewRuntimeSessionProvisioner 创建 provisioner。
//
// Python: RuntimeSessionProvisioner.__init__
func NewRuntimeSessionProvisioner(deps ProvisionerDeps) *RuntimeSessionProvisioner {
	nowFunc := deps.NowFunc
	if nowFunc == nil {
		nowFunc = time.Now
	}
	participants := deps.ParticipantRegistry
	if participants == nil {
		participants = sessionpkg.NewRuntimeParticipantRegistry()
	}
	return &RuntimeSessionProvisioner{
		AgentManager:            deps.AgentManager,
		PlanController:          deps.PlanController,
		TrajectoryHook:          deps.TrajectoryHook,
		TeamBinding:             deps.TeamBinding,
		LifecycleJournal:        deps.LifecycleJournal,
		TeamExecutionController: deps.TeamExecutionController,
		DeleteLifecycle:         deps.DeleteLifecycle,
		provisionOwnerToken:     &provisionOwnerToken{},
		nowFunc:                 nowFunc,
		participants:            participants,
		preparedDeletes:         map[string]SessionDeleteLifecycle{},
		pendingDeleteMeta:       map[string]map[string]any{},
		completedDeletes:        newRecentDeleteResults[sessionpkg.SessionDeleteResult](256, 60*time.Second, nowFunc),
		completedTeamDeletes:    newRecentDeleteResults[sessionpkg.TeamDeleteResult](256, 60*time.Second, nowFunc),
	}
}

// SetSessionOps 注入会话复制能力（测试与装配用）。
func (p *RuntimeSessionProvisioner) SetSessionOps(ops SessionOps) { p.extra.SessionOps = ops }

// SetTeamManagerProvider 注入团队管理器获取函数。
func (p *RuntimeSessionProvisioner) SetTeamManagerProvider(fn func(channelID string) TeamSwitchPreparer) {
	p.extra.teamManagerProvider = fn
}

// CommitSessionProvision 在声明的投递边界提交一次已准备操作。
//
// Python: RuntimeSessionProvisioner.commit_session_provision
// commitContext 为 nil 时按零值处理（对齐上游 `context or CommitContext()`）。
func (p *RuntimeSessionProvisioner) CommitSessionProvision(
	lease *PreparedSessionProvision,
	timing SessionProvisionCommitTiming,
	commitContext *SessionProvisionCommitContext,
) (any, error) {
	ctx := NewEmptyCommitContext()
	if commitContext != nil {
		ctx = *commitContext
	}
	return lease.commitForOwner(p.provisionOwnerToken, timing, ctx)
}

// AbortSessionProvision 中止一次已准备操作（幂等）。
//
// Python: RuntimeSessionProvisioner.abort_session_provision
func (p *RuntimeSessionProvisioner) AbortSessionProvision(lease *PreparedSessionProvision) error {
	return lease.abortForOwner(p.provisionOwnerToken)
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errAgentManagerUnavailable agent 管理器缺失
	errAgentManagerUnavailable = errors.New("agent manager unavailable")
	// errSessionOpsUnavailable 会话复制能力缺失
	errSessionOpsUnavailable = errors.New("session ops unavailable")
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// logProvisionWarn 记一条告警日志。
func logProvisionWarn(msg string, err error) {
	logger.Warn(logComponent).Err(err).Msg(msg)
}

// stageSessionProvision 构造归属本 provisioner 的租约。
//
// Python: RuntimeSessionProvisioner._stage_session_provision
// 工厂方法故意私有：调用方只能拿到不透明租约，无法注入可执行回调。
func (p *RuntimeSessionProvisioner) stageSessionProvision(
	result any,
	timing SessionProvisionCommitTiming,
	commitHook func(SessionProvisionCommitContext) error,
	abortHook func() error,
	commitAttemptIsTerminal bool,
) *PreparedSessionProvision {
	return &PreparedSessionProvision{
		ownerToken:              p.provisionOwnerToken,
		result:                  result,
		commitTiming:            timing,
		commitHook:              commitHook,
		abortHook:               abortHook,
		commitAttemptIsTerminal: commitAttemptIsTerminal,
		state:                   SessionProvisionStatePrepared,
	}
}

// assertOwner 校验租约归属。
//
// Python: PreparedSessionProvision._assert_owner
func (p *PreparedSessionProvision) assertOwner(ownerToken *provisionOwnerToken) error {
	if ownerToken != p.ownerToken {
		return &SessionProvisionStateError{
			Message: "prepared session provision belongs to a different provisioner",
		}
	}
	return nil
}

// commitForOwner 由持有 ownerToken 的 provisioner 调用提交。
//
// Python: PreparedSessionProvision.commit_for_owner
func (p *PreparedSessionProvision) commitForOwner(
	ownerToken *provisionOwnerToken,
	timing SessionProvisionCommitTiming,
	commitContext SessionProvisionCommitContext,
) (any, error) {
	if err := p.assertOwner(ownerToken); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if timing != p.commitTiming {
		return nil, &SessionProvisionStateError{
			Message: fmt.Sprintf("session provision commit timing mismatch: expected %s, got %s",
				p.commitTiming, timing),
		}
	}
	switch p.state {
	case SessionProvisionStateCommitted:
		return p.result, nil
	case SessionProvisionStateAborted:
		return nil, &SessionProvisionStateError{Message: "cannot commit an aborted session provision"}
	case SessionProvisionStateAborting:
		return nil, &SessionProvisionStateError{Message: "cannot commit after abort finalization started"}
	case SessionProvisionStatePrepared:
		p.commitContext = &commitContext
		p.state = SessionProvisionStateCommitting
	default:
		if p.commitContext == nil || *p.commitContext != commitContext {
			return nil, &SessionProvisionStateError{
				Message: "session provision commit context does not match first attempt",
			}
		}
	}

	if p.commitHook != nil {
		if err := p.commitHook(commitContext); err != nil {
			if p.commitAttemptIsTerminal {
				p.state = SessionProvisionStateCommitted
				p.commitContext = nil
				p.commitHook = nil
				p.abortHook = nil
			}
			return nil, err
		}
	}
	p.state = SessionProvisionStateCommitted
	p.commitContext = nil
	p.commitHook = nil
	p.abortHook = nil
	return p.result, nil
}

// abortForOwner 由持有 ownerToken 的 provisioner 调用中止。
//
// Python: PreparedSessionProvision.abort_for_owner
func (p *PreparedSessionProvision) abortForOwner(ownerToken *provisionOwnerToken) error {
	if err := p.assertOwner(ownerToken); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	switch p.state {
	case SessionProvisionStateAborted:
		return nil
	case SessionProvisionStateCommitted:
		return &SessionProvisionStateError{Message: "cannot abort a committed session provision"}
	case SessionProvisionStateCommitting:
		return &SessionProvisionStateError{Message: "cannot abort after commit finalization started"}
	case SessionProvisionStatePrepared:
		p.state = SessionProvisionStateAborting
	}
	if p.abortHook != nil {
		if err := p.abortHook(); err != nil {
			return err
		}
	}
	p.state = SessionProvisionStateAborted
	p.commitContext = nil
	p.commitHook = nil
	p.abortHook = nil
	return nil
}

// sessionForkErrorCode 把 fork 的 ValueError 文本映射为错误码。
//
// Python: runtime/session_provisioner.py _session_fork_error_code
func sessionForkErrorCode(err error) string {
	msg := err.Error()
	switch {
	case containsSubstring(msg, "not found"):
		return "NOT_FOUND"
	case containsSubstring(msg, "already exists"):
		return "ALREADY_EXISTS"
	default:
		return "BAD_REQUEST"
	}
}

// containsSubstring 是否包含子串（等价 Python `in`）。
func containsSubstring(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// newRecentDeleteResults 创建缓存。
func newRecentDeleteResults[T any](capacity int, ttl time.Duration, now func() time.Time) *recentDeleteResults[T] {
	return &recentDeleteResults[T]{
		capacity: capacity,
		ttl:      ttl,
		now:      now,
		entries:  map[string]recentDeleteEntry[T]{},
	}
}

// get 取未过期条目。
//
// Python: _RecentDeleteResults.get
func (r *recentDeleteResults[T]) get(key string) (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[key]
	if !ok {
		var zero T
		return zero, false
	}
	if !entry.expiresAt.After(r.now()) {
		delete(r.entries, key)
		var zero T
		return zero, false
	}
	return entry.value, true
}

// put 写入条目并按容量淘汰。
//
// Python: _RecentDeleteResults.__setitem__
func (r *recentDeleteResults[T]) put(key string, value T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, key)
	r.entries[key] = recentDeleteEntry[T]{expiresAt: r.now().Add(r.ttl), value: value}
	r.order = append(r.order, key)
	for len(r.entries) > r.capacity && len(r.order) > 0 {
		oldest := r.order[0]
		r.order = r.order[1:]
		if _, exists := r.entries[oldest]; exists {
			delete(r.entries, oldest)
		}
	}
}

// remove 删除条目。
//
// Python: _RecentDeleteResults.pop
func (r *recentDeleteResults[T]) remove(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, key)
}

// ──────────────────────────── 导出函数（switch / fork） ────────────────────────────

// PrepareSessionSwitch 准备一次会话切换。
//
// Python: RuntimeSessionProvisioner.prepare_session_switch（L1167-1263）
// 步骤见 spec §5.4（8 步），顺序与短路位置不得调整（规则 9.3）。
func (p *RuntimeSessionProvisioner) PrepareSessionSwitch(
	ctx context.Context,
	input SessionSwitchInput,
) (*PreparedSessionProvision, error) {
	channelID := input.ChannelID
	if channelID == "" {
		channelID = "default"
	}
	targetSessionID := input.TargetSessionID
	previousSessionID := input.PreviousSessionID
	if targetSessionID == "" {
		return nil, &SessionProvisionError{Message: "session_id is required", Code: "BAD_REQUEST"}
	}

	// 2. 解析请求模式
	resolvedMode, targetIsTeam := p.resolveSwitchMode(input.Mode, input.TeamHint)

	// 3. 目标 metadata 优先
	var targetMeta map[string]any
	if p.extra.SessionOps != nil {
		targetMeta = p.extra.SessionOps.GetSessionMetadata(targetSessionID)
	}
	if storedMode, _ := targetMeta["mode"].(string); storedMode != "" {
		resolvedMode, targetIsTeam = p.resolveSwitchMode(storedMode, input.TeamHint)
	}

	// 4-5. previous target 解析与团队归属准备
	previous := p.participants.Target(previousSessionID)
	previousIsTeam := previous != nil && previous.Kind == sessionpkg.SessionKindTeam
	if previous == nil && previousSessionID != "" {
		var prevMeta map[string]any
		if p.extra.SessionOps != nil {
			prevMeta = p.extra.SessionOps.GetSessionMetadata(previousSessionID)
		}
		prevMode, _ := prevMeta["mode"].(string)
		if prevMode == "" {
			prevMode = input.PreviousMode
		}
		if prevMode != "" {
			prevChannel, _ := prevMeta["channel_id"].(string)
			if prevChannel == "" {
				prevChannel = channelID
			}
			prevWorkMode, _ := prevMeta["work_mode"].(string)
			if prevWorkMode == "" {
				prevWorkMode = "work"
			}
			prevTeamName, _ := prevMeta["team_name"].(string)
			_, prevIsTeamMode := p.resolveSwitchMode(prevMode, false)
			kind := sessionpkg.SessionKindAgent
			if prevIsTeamMode {
				kind = sessionpkg.SessionKindTeam
			}
			built := sessionpkg.SessionLifecycleTarget{
				Descriptor: sessionpkg.SessionDescriptor{
					SessionID: previousSessionID, ChannelID: prevChannel,
					Mode: prevMode, WorkMode: prevWorkMode,
				},
				Kind:     kind,
				TeamName: prevTeamName,
			}
			previous = &built
			previousIsTeam = kind == sessionpkg.SessionKindTeam
		}
	}
	if targetIsTeam || previousIsTeam {
		if err := p.prepareTeamOwnerSwitch(ctx, channelID, targetSessionID,
			previousIsTeam, previousSessionID, "session.switch: "); err != nil {
			return nil, err
		}
	}

	target := sessionpkg.SessionLifecycleTarget{
		Descriptor: sessionpkg.SessionDescriptor{
			SessionID: targetSessionID, ChannelID: channelID,
			Mode: resolvedMode, WorkMode: "work",
		},
		Kind: sessionKindOf(targetIsTeam),
	}

	moaEnabled, _ := targetMeta["moa_enabled"].(bool)
	result := SessionSwitchResult{
		ChannelID:  channelID,
		SessionID:  targetSessionID,
		Mode:       resolvedMode,
		Switched:   true,
		MoaEnabled: moaEnabled,
	}
	commitHook := func(commitCtx SessionProvisionCommitContext) error {
		viewID := commitCtx.ForegroundScopeID
		if viewID == "" {
			viewID = "default-view"
		}
		p.dispatchForegroundTransition(ctx, target, previous, viewID)
		return nil
	}
	return p.stageSessionProvision(result, SessionSwitchCommitTiming, commitHook, nil, true), nil
}

// PrepareSessionFork 准备一次会话复制。
//
// Python: RuntimeSessionProvisioner.prepare_session_fork（L1265-1385）
// 步骤见 spec §5.5（8 步）。
func (p *RuntimeSessionProvisioner) PrepareSessionFork(
	ctx context.Context,
	input SessionForkInput,
) (*PreparedSessionProvision, error) {
	sourceSessionID := input.SourceSessionID
	targetSessionID := input.TargetSessionID
	channelID := input.ChannelID
	if channelID == "" {
		channelID = "default"
	}
	if sourceSessionID == "" {
		return nil, &SessionProvisionError{Message: "source_session_id is required", Code: "BAD_REQUEST"}
	}
	hasCutoff := input.CutoffMessageID != "" || input.CutoffRole != "" ||
		input.CutoffContent != "" || input.CutoffTimestamp != nil

	var forkResult ForkSessionResult
	err := func() error {
		if targetSessionID == "" {
			if p.AgentManager == nil {
				return errAgentManagerUnavailable
			}
			generated, genErr := p.AgentManager.CreateSession(ctx, channelID, "")
			if genErr != nil {
				return genErr
			}
			targetSessionID = generated
		}
		if p.extra.SessionOps == nil {
			return errSessionOpsUnavailable
		}
		params := ForkSessionParams{
			SourceSessionID: sourceSessionID, TargetSessionID: targetSessionID,
			Title: input.Title, ChannelID: channelID,
		}
		if hasCutoff {
			params.CutoffMessageID = input.CutoffMessageID
			params.CutoffRole = input.CutoffRole
			params.CutoffContent = input.CutoffContent
			params.CutoffTimestamp = input.CutoffTimestamp
		}
		if input.SessionEquipmentOverride != nil {
			params.SessionEquipmentOverride = input.SessionEquipmentOverride
		}
		var forkErr error
		forkResult, forkErr = p.extra.SessionOps.ForkSession(params)
		if forkErr != nil {
			return forkErr
		}

		var agent SessionAgent
		if p.AgentManager != nil {
			agent = p.AgentManager.GetAgentNoWait(channelID)
		}
		if agent != nil {
			if _, ensureErr := agent.EnsureInstance(ctx); ensureErr != nil {
				return ensureErr
			}
			if copyErr := p.extra.SessionOps.CopySessionContext(ctx, agent,
				sourceSessionID, targetSessionID, hasCutoff); copyErr != nil {
				return copyErr
			}
		}
		if !hasCutoff {
			var card any
			if agent != nil {
				card = agent.Card()
			}
			if stateErr := p.extra.SessionOps.CopySessionState(ctx,
				sourceSessionID, targetSessionID, card, agent); stateErr != nil {
				return stateErr
			}
		}
		return nil
	}()
	if err != nil {
		return nil, &SessionProvisionError{Message: err.Error(), Code: sessionForkErrorCode(err)}
	}

	result := SessionForkResult{
		ChannelID:       channelID,
		SourceSessionID: firstNonEmpty(forkResult.SourceSessionID, sourceSessionID),
		SessionID:       firstNonEmpty(forkResult.SessionID, targetSessionID),
		Title:           forkResult.Title,
		MoaEnabled:      forkResult.MoaEnabled,
	}
	return p.stageSessionProvision(result, SessionForkCommitTiming, nil, nil, false), nil
}

// ──────────────────────────── 非导出函数（switch / fork 辅助） ────────────────────────────

// resolveSwitchMode 解析模式并判定是否团队。
//
// Python: resolve_agent_request_mode + is_team_mode
func (p *RuntimeSessionProvisioner) resolveSwitchMode(mode string, teamHint bool) (string, bool) {
	if p.extra.ResolveCanonicalMode != nil {
		canonical, isTeam := p.extra.ResolveCanonicalMode(map[string]any{"mode": mode}, "")
		return canonical, isTeam || teamHint
	}
	return mode, teamHint
}

// sessionKindOf 布尔转 SessionKind。
func sessionKindOf(isTeam bool) sessionpkg.SessionKind {
	if isTeam {
		return sessionpkg.SessionKindTeam
	}
	return sessionpkg.SessionKindAgent
}

// firstNonEmpty 取首个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// prepareTeamOwnerSwitch 准备团队会话归属切换。
//
// Python: _prepare_create_owner / prepare_session_switch 内的
// get_team_manager(...).prepare_session_switch
func (p *RuntimeSessionProvisioner) prepareTeamOwnerSwitch(
	ctx context.Context,
	channelID, targetSessionID string,
	previousIsTeam bool,
	previousSessionID, reason string,
) error {
	if p.extra.teamManagerProvider == nil {
		return nil
	}
	manager := p.extra.teamManagerProvider(channelID)
	if manager == nil {
		return nil
	}
	previous := ""
	if previousIsTeam {
		previous = previousSessionID
	}
	return manager.PrepareSessionSwitch(ctx, targetSessionID, previous, reason)
}

// dispatchForegroundTransition 派发前台可见性切换。
//
// Python: RuntimeSessionProvisioner._dispatch_foreground_transition
// 参与者为空时只记住 target（对齐上游）。
func (p *RuntimeSessionProvisioner) dispatchForegroundTransition(
	ctx context.Context,
	target sessionpkg.SessionLifecycleTarget,
	previous *sessionpkg.SessionLifecycleTarget,
	viewID string,
) {
	participants := p.participants.SnapshotActivity()
	if len(participants) == 0 {
		p.participants.RememberTarget(target)
		return
	}
	if previous != nil && previous.Descriptor.SessionID != target.Descriptor.SessionID {
		event := sessionpkg.SessionForegroundEvent{
			Target:     *previous,
			ViewID:     viewID,
			Visible:    false,
			HasHistory: p.historyExists(previous.Descriptor.SessionID),
		}
		for _, participant := range participants {
			if err := participant.ForegroundChanged(ctx, event); err != nil {
				logProvisionWarn("Runtime foreground participant failed", err)
			}
		}
	}
	p.participants.RememberTarget(target)
	event := sessionpkg.SessionForegroundEvent{
		Target:     target,
		ViewID:     viewID,
		Visible:    true,
		HasHistory: p.historyExists(target.Descriptor.SessionID),
	}
	for _, participant := range participants {
		if err := participant.ForegroundChanged(ctx, event); err != nil {
			logProvisionWarn("Runtime foreground participant failed", err)
		}
	}
}

// historyExists 会话是否有历史。
func (p *RuntimeSessionProvisioner) historyExists(sessionID string) bool {
	if p.extra.SessionOps == nil {
		return false
	}
	return p.extra.SessionOps.HistoryExists(sessionID)
}
