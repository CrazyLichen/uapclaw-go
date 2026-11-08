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
// Python: PlanModeController.reset_session
type SessionPlanController interface {
	// ResetSession 清空会话计划状态
	ResetSession(sessionID string)
}

// TrajectoryDeleteHook 轨迹库删除钩子。
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
// Python: server/runtime/team_binding_store.get_team_binding_store().unbind_session
// spec §10 R-1：Go 侧为桩实现。
type TeamBindingUnbinder interface {
	// UnbindSession 解绑会话
	UnbindSession(teamName, sessionID string) error
}

// PromptSuggestionForks 会话建议生成的分支持有者。
// Python: agents/harness/common/recommendation/prompt_suggestion.py
//
//	PromptSuggestionForks.session_deleted（:428）
//
// spec §8 D-10：实现待 10.3.30 recommendation，桩被调用时记 Debug 并返回。
type PromptSuggestionForks interface {
	// SessionDeleted 清除该会话的轮次、已回答主调用与在飞生成
	SessionDeleted(sessionID string)
}

// SubagentRuntimeStopper 会话常驻子代理运行时停止器。
// Python: runtime/service.py stop_subagent_runtimes（:3183）
// spec §8 D-9：实现待 25.8 subagent_runtime，桩被调用时记 Debug 并返回 nil。
type SubagentRuntimeStopper interface {
	// StopSubagentRuntimes 释放该会话常驻的子代理
	StopSubagentRuntimes(ctx context.Context, sessionID, reason string) error
}

// SessionRuntimeCleaner 会话内存运行时清理器。
// Python: runtime/service.py cleanup_session（:3024）
//
// 与 RunnerRelease 不同：本接口清的是门面队列与 adapter 侧缓存，
// Runner.release 由 provisioner 在 dispose 之后调用。
type SessionRuntimeCleaner interface {
	// CleanupSession 释放该会话的内存运行时（resetPlanState 恒 false：事务内保留 plan 供回滚）
	CleanupSession(ctx context.Context, channelID, sessionID string) error
}

// SessionLifecycleJournal 会话生命周期操作日志（删除可恢复性）。
// Python: server/runtime/session/lifecycle.py 的 state / update
// spec §10 R-4：Go 侧为桩实现，recovering 恒 false。
type SessionLifecycleJournal interface {
	// RecoveringDelete 该会话是否处于未完成的删除操作
	RecoveringDelete(sessionID string) bool
	// MarkCleanup 标记删除进入清理阶段
	MarkCleanup(sessionID string) error
}

// SessionDeleteLifecycle 参与会话删除的可选 Runtime 能力。
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
// Python: AgentManager.get_agent_nowait 返回的 agent（仅用 ensure_instance / card）
type SessionAgent interface {
	// EnsureInstance 确保实例就绪
	EnsureInstance(ctx context.Context) (any, error)
	// Card 返回 agent 卡片
	Card() any
}

// TeamSwitchPreparer 团队会话归属准备能力。
// Python: TeamManager.prepare_session_switch
type TeamSwitchPreparer interface {
	// PrepareSessionSwitch 准备目标会话的团队归属切换
	PrepareSessionSwitch(ctx context.Context, targetSessionID, previousSessionID, reason string) error
}

// SessionOps 会话复制与元数据访问能力。
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
// 用带字段的结构体而非 struct{}：Go 对零大小对象会复用地址，
// 两个 provisioner 的 &struct{}{} 可能相等，无法区分归属。
type provisionOwnerToken struct {
	// seq 非零填充，保证每个实例有独立地址
	seq byte
}

// PrewarmClaimRequest 预热池认领请求。
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
// Python: runtime/session_provisioner.py SessionProvisionCommitContext
// 唯一字段是前台作用域标识；它**不是**会话身份。
type SessionProvisionCommitContext struct {
	// ForegroundScopeID 前台作用域标识（服务端视图身份）
	ForegroundScopeID string
}

// SessionProvisionError 传输无关的业务失败。
// Python: runtime/session_provisioner.py SessionProvisionError
type SessionProvisionError struct {
	// Message 错误说明
	Message string
	// Code 可选错误码（与上游一致：可能为空）
	Code string
}

// SessionProvisionStateError 租约被错误的拥有者或顺序终结。
// Python: runtime/session_provisioner.py SessionProvisionStateError
type SessionProvisionStateError struct {
	// Message 错误说明
	Message string
}

// PreparedSessionProvision 一次成功 prepare 返回的不透明结果租约。
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

// CreateResultOf 替代 `lease.Result().(SessionCreateResult)` 的直接断言：
// 断言失败时返回 error 而不是零值结构体（handler 里的 `x, _ :=` 会静默拿到
// 空 SessionID，排查成本极高）。switch/fork 在提交后直接用值断言并显式处理
// 失败，故不再需要独立访问器。

// CreateResultOf 从 create 租约取结果，类型不符返回错误。
func CreateResultOf(lease *PreparedSessionProvision) (SessionCreateResult, error) {
	if lease == nil {
		return SessionCreateResult{}, errors.New("session provision lease is nil")
	}
	typed, ok := lease.Result().(SessionCreateResult)
	if !ok {
		return SessionCreateResult{}, fmt.Errorf(
			"session provision result is %T, want SessionCreateResult", lease.Result())
	}
	return typed, nil
}

// ProvisionerDeps provisioner 构造依赖。
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
	// PromptForks 会话建议生成分支持有者
	PromptForks PromptSuggestionForks
	// SubagentStopper 子代理运行时停止器
	SubagentStopper SubagentRuntimeStopper
	// SessionCleaner 会话内存运行时清理器
	SessionCleaner SessionRuntimeCleaner
}

// RuntimeSessionProvisioner 单个 Runtime 的会话供给事务编排器。
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
	// PromptForks 会话建议生成分支持有者
	PromptForks PromptSuggestionForks
	// SubagentStopper 子代理运行时停止器
	SubagentStopper SubagentRuntimeStopper
	// SessionCleaner 会话内存运行时清理器
	SessionCleaner SessionRuntimeCleaner

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
// Python: PreparedSessionProvision.result
func (p *PreparedSessionProvision) Result() any { return p.result }

// State 返回当前两阶段状态。
// Python: PreparedSessionProvision.state
func (p *PreparedSessionProvision) State() SessionProvisionState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// CommitTiming 返回调用方必须使用的提交时机。
// Python: PreparedSessionProvision.commit_timing
func (p *PreparedSessionProvision) CommitTiming() SessionProvisionCommitTiming {
	return p.commitTiming
}

// NewEmptyCommitContext 构造零值提交上下文。
func NewEmptyCommitContext() SessionProvisionCommitContext {
	return SessionProvisionCommitContext{}
}

// NewRuntimeSessionProvisioner 创建 provisioner。
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
		PromptForks:             deps.PromptForks,
		SubagentStopper:         deps.SubagentStopper,
		SessionCleaner:          deps.SessionCleaner,
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

// SetResolveSessionDir 注入会话目录解析函数。
func (p *RuntimeSessionProvisioner) SetResolveSessionDir(fn func(sessionID string) (string, bool)) {
	p.extra.ResolveSessionDir = fn
}

// SetRemoveSessionDir 注入会话目录删除函数。
func (p *RuntimeSessionProvisioner) SetRemoveSessionDir(fn func(sessionDir string) error) {
	p.extra.RemoveSessionDir = fn
}

// SetTeamManagerProvider 注入团队管理器获取函数。
// SetListTeamSessions 注入团队会话清单读取器（团队删除用）。
// Python: session_provisioner.py _inventory_team_session_ids（:1873）
func (p *RuntimeSessionProvisioner) SetListTeamSessions(fn func(teamName string) []string) {
	p.extra.ListTeamSessions = fn
}

func (p *RuntimeSessionProvisioner) SetTeamManagerProvider(fn func(channelID string) TeamSwitchPreparer) {
	p.extra.teamManagerProvider = fn
}

// CommitSessionProvision 在声明的投递边界提交一次已准备操作。
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
// Python: _RecentDeleteResults.__setitem__
func (r *recentDeleteResults[T]) put(key string, value T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, key)
	r.entries[key] = recentDeleteEntry[T]{expiresAt: r.now().Add(r.ttl), value: value}
	r.order = append(r.order, key)
	// 超出容量时按写入顺序淘汰最老的键。
	// 队列里的键可能已被覆盖或过期删除，delete 对不存在的键是空操作，无需先判断存在。
	for len(r.entries) > r.capacity && len(r.order) > 0 {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.entries, oldest)
	}
}

// remove 删除条目。
// Python: _RecentDeleteResults.pop
func (r *recentDeleteResults[T]) remove(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, key)
}

// ──────────────────────────── 导出函数（switch / fork） ────────────────────────────

// PrepareSessionSwitch 准备一次会话切换。
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

// ──────────────────────────── 导出函数（create） ────────────────────────────

// PrepareSessionCreate 准备一次会话创建。
// Python: RuntimeSessionProvisioner.prepare_session_create（L595-1010）
// 21 步见 spec §5.3；每一步的顺序与短路位置不得调整（规则 9.3）。
func (p *RuntimeSessionProvisioner) PrepareSessionCreate(
	ctx context.Context,
	input SessionCreateInput,
) (lease *PreparedSessionProvision, err error) {
	// 1. channel_id 归一
	channelID := input.ChannelID
	if channelID == "" {
		channelID = "default"
	}

	// 2-3. TUI 显式 ID 判定与拒绝
	requestedSessionID := input.RequestedSessionID
	explicitTUI := requestedSessionID != "" && equalFold(channelID, "tui")
	if requestedSessionID != "" && !explicitTUI {
		return nil, &SessionProvisionError{
			Message: "session.create no longer accepts session_id; use session.switch to restore",
		}
	}

	var (
		externalLock *sync.Mutex
		lockAcquired bool
		claimedID    string
	)
	// 异常补偿（defer 顺序与上游 finally 一致）
	defer func() {
		if err != nil {
			p.compensateCreateFailure(ctx, claimedID, externalLock, lockAcquired)
		}
	}()

	// 4. TUI 显式 ID：校验 + 取外锁
	if explicitTUI {
		if p.extra.IsValidSessionID != nil && !p.extra.IsValidSessionID(requestedSessionID) {
			return nil, &SessionProvisionError{Message: "invalid session_id"}
		}
		actual, _ := p.externalCreateLocks.LoadOrStore(requestedSessionID, &sync.Mutex{})
		externalLock = actual.(*sync.Mutex)
		externalLock.Lock()
		lockAcquired = true
	}

	// 5. 组装 params（11 个键）
	params := map[string]any{
		"mode":          input.Mode,
		"project_id":    input.ProjectID,
		"project_dir":   input.ProjectDir,
		"cwd":           input.Cwd,
		"work_mode":     input.WorkMode,
		"title":         input.Title,
		"user_id":       input.UserID,
		"model_name":    input.ModelName,
		"cron_id":       input.CronID,
		"is_swarm":      input.IsSwarm,
		"team":          input.TeamHint,
		"previous_mode": input.PreviousMode,
	}

	// 6. 已有 metadata 分支
	var existing map[string]any
	if explicitTUI && p.extra.SessionOps != nil {
		existing = p.extra.SessionOps.GetSessionMetadata(requestedSessionID)
	}
	persistSession := input.PersistSession
	if len(existing) > 0 {
		existingChannel, _ := existing["channel_id"].(string)
		if existingChannel != "" && !equalFold(existingChannel, "tui") {
			return nil, &SessionProvisionError{Message: "session_id is already owned by another channel"}
		}
		storedPersist, _ := existing["persist_session"].(bool)
		if input.PersistSessionSupplied && persistSession != storedPersist {
			return nil, &SessionProvisionError{
				Message: "persist_session is immutable after session creation",
				Code:    "CONFLICT",
			}
		}
		persistSession = storedPersist
		for _, field := range []string{"project_id", "project_dir", "work_mode", "mode"} {
			if v, ok := existing[field].(string); ok && v != "" {
				params[field] = v
			}
		}
	} else if explicitTUI && !p.usesProjectlessWorkspace(params, channelID) {
		// 7. TUI 项目查找或创建
		if p.extra.FindOrCreateProject != nil {
			if binding, ok := p.extra.FindOrCreateProject(params); ok {
				params["project_id"] = binding.ProjectID
				params["project_dir"] = binding.ProjectDir
				params["work_mode"] = binding.WorkMode
			}
		}
	}

	// 8-10. work_mode 与项目绑定解析
	binding := projectBinding{}
	if p.extra.ResolveWorkMode != nil {
		binding = p.extra.ResolveWorkMode(params, channelID)
		if binding.Error != "" {
			return nil, &SessionProvisionError{Message: binding.Error, Code: binding.Code}
		}
	}
	hasExplicitWorkMode := binding.HasExplicitWorkMode
	if input.WorkModeExplicit != nil {
		hasExplicitWorkMode = *input.WorkModeExplicit
	}
	if !isDefaultProjectID(binding.ProjectID) && hasExplicitWorkMode &&
		binding.WorkMode != "" && binding.ProjectWorkMode != "" &&
		binding.WorkMode != binding.ProjectWorkMode {
		return nil, &SessionProvisionError{
			Message: fmt.Sprintf("work_mode mismatch: project is '%s'%sbut request specified '%s'",
				binding.ProjectWorkMode, legacyWorkModeMismatchSeparator, binding.WorkMode),
			Code: "BAD_REQUEST",
		}
	}
	finalWorkMode := binding.WorkMode
	if finalWorkMode == "" {
		finalWorkMode = input.WorkMode
	}
	params["project_id"] = binding.ProjectID
	params["project_dir"] = binding.ProjectDir
	params["work_mode"] = finalWorkMode

	// 11. 归一化模式
	canonicalMode := input.Mode
	// 上游 is_swarm = input.is_swarm or resolved.is_team；resolved.is_team 由
	// resolve_request_mode 综合 params["team"] 与 mode 得出。Go 的 provider 契约
	// 只回 mode 层面的判定，故此处显式并入 TeamHint（等价 upstream params["team"]）。
	isSwarm := input.IsSwarm || input.TeamHint
	if p.extra.ResolveCanonicalMode != nil {
		var resolvedTeam bool
		canonicalMode, resolvedTeam = p.extra.ResolveCanonicalMode(params, finalWorkMode)
		isSwarm = isSwarm || resolvedTeam
	}

	// 12-13. MOA 解析
	moaEnabled := false
	if input.MoaEnabled != nil {
		moaEnabled = *input.MoaEnabled
	}
	if p.extra.ResolveMoa != nil {
		enabled, moaErr := p.extra.ResolveMoa(params, canonicalMode, input.CronID != "")
		if moaErr != nil {
			return nil, &SessionProvisionError{Message: moaErr.Error(), Code: "BAD_REQUEST"}
		}
		moaEnabled = moaEnabled || enabled
	}

	// 14. 预热资格
	prewarmEligible := p.prewarmEligible(input, channelID, isSwarm, moaEnabled, canonicalMode)

	// 15. 认领会话
	var sessionID, prewarmStatus string
	prewarmHit := false
	if explicitTUI {
		sessionID = requestedSessionID
		prewarmStatus = "bypassed"
	} else {
		if trimSpaces(input.CreateToken) == "" {
			return nil, &SessionProvisionError{Message: "create_token is required"}
		}
		if p.AgentManager == nil {
			return nil, &SessionProvisionError{Message: errAgentManagerUnavailable.Error()}
		}
		claim, claimErr := p.AgentManager.ClaimPrewarmedSession(ctx, PrewarmClaimRequest{
			ChannelID:       channelID,
			ProjectID:       binding.ProjectID,
			ProjectDir:      binding.ProjectDir,
			WorkMode:        finalWorkMode,
			IsSwarm:         isSwarm,
			PersistSession:  persistSession,
			PrewarmEligible: prewarmEligible,
			CreateToken:     input.CreateToken,
			MoaEnabled:      moaEnabled,
		})
		if claimErr != nil {
			return nil, &SessionProvisionError{Message: claimErr.Error()}
		}
		sessionID = claim.SessionID
		claimedID = sessionID
		prewarmHit = claim.PrewarmHit
		prewarmStatus = claim.PrewarmStatus
	}

	// 16. metadata 已存在且非 TUI：激活预热并返回 created=false
	metadataExists := false
	if p.extra.SessionOps != nil && !explicitTUI {
		metadataExists = p.extra.SessionOps.SessionDirHasMetadata(sessionID)
	}
	if metadataExists && !explicitTUI {
		if p.AgentManager != nil {
			p.AgentManager.ActivateSessionPrewarm(sessionID)
		}
		stored := p.extra.SessionOps.GetSessionMetadata(sessionID)
		storedPersist, _ := stored["persist_session"].(bool)
		storedMoa, _ := stored["moa_enabled"].(bool)
		result := SessionCreateResult{
			ChannelID: channelID, SessionID: sessionID,
			ProjectID: binding.ProjectID, ProjectDir: binding.ProjectDir,
			WorkMode: finalWorkMode, PersistSession: storedPersist,
			PrewarmHit: prewarmHit, PrewarmStatus: prewarmStatus,
			Created: false, CanonicalMode: canonicalMode, MoaEnabled: storedMoa,
		}
		return p.stageCreateResult(ctx, result, claimedID, externalLock, nil, nil), nil
	}

	// 17. 新建 metadata
	sessionCreated := !metadataExists
	if sessionCreated && p.extra.SessionOps != nil {
		var channelMetadata map[string]any
		if equalFold(channelID, "tui") {
			workspace := firstNonEmpty(input.Cwd, binding.ProjectDir)
			if workspace != "" && (!p.usesProjectlessWorkspace(params, channelID) || binding.ProjectDir != "") {
				channelMetadata = map[string]any{
					"cwd":         workspace,
					"project_dir": firstNonEmpty(binding.ProjectDir, workspace),
				}
			}
		}
		if initErr := p.extra.SessionOps.InitSessionMetadata(InitMetadataParams{
			SessionID: sessionID, ChannelID: channelID,
			UserID: trimSpaces(input.UserID), Title: input.Title,
			Mode: canonicalMode, ProjectDir: binding.ProjectDir, ProjectID: binding.ProjectID,
			PersistSession: persistSession, WorkMode: finalWorkMode,
			Model: trimSpaces(input.ModelName), MoaEnabled: moaEnabled,
			CronID: trimSpaces(input.CronID), ChannelMetadata: channelMetadata,
		}); initErr != nil {
			return nil, &SessionProvisionError{Message: initErr.Error()}
		}
		if !explicitTUI && p.AgentManager != nil {
			p.AgentManager.ActivateSessionPrewarm(sessionID)
		}
	}

	// 18. 模型选择
	if input.ModelSelection != nil && p.extra.SessionOps != nil {
		_ = p.extra.SessionOps.SetSessionModelSelection(sessionID, input.ModelSelection)
	}

	// 19. 归属准备
	ownerParams := make(map[string]any, len(params)+4)
	for k, v := range params {
		ownerParams[k] = v
	}
	ownerParams["mode"] = canonicalMode
	ownerParams["project_dir"] = binding.ProjectDir
	ownerParams["work_mode"] = finalWorkMode
	ownerParams["project_id"] = binding.ProjectID
	ownerParams["user_id"] = input.UserID
	target, previousTarget := p.prepareCreateOwner(ctx, channelID, sessionID,
		input.PreviousSessionID, ownerParams, isSwarm)

	// 20. 结果
	result := SessionCreateResult{
		ChannelID: channelID, SessionID: sessionID,
		ProjectID: binding.ProjectID, ProjectDir: binding.ProjectDir,
		WorkMode: finalWorkMode, PersistSession: persistSession,
		PrewarmHit: prewarmHit, PrewarmStatus: prewarmStatus,
		Created: sessionCreated, CanonicalMode: canonicalMode,
		ExplicitIDCompatibility: explicitTUI, MoaEnabled: moaEnabled,
	}
	return p.stageCreateResult(ctx, result, claimedID, externalLock, &target, previousTarget), nil
}

// ──────────────────────────── 非导出函数（create 辅助） ────────────────────────────

// compensateCreateFailure create 失败的补偿（释放认领 + 释放外锁）。
// Python: prepare_session_create 的 except BaseException 分支
// 补偿自身失败只告警，保留原始错误。
func (p *RuntimeSessionProvisioner) compensateCreateFailure(
	ctx context.Context,
	claimedID string,
	externalLock *sync.Mutex,
	lockAcquired bool,
) {
	if claimedID != "" && p.AgentManager != nil {
		if releaseErr := p.AgentManager.ReleaseSessionPrewarmClaim(ctx, claimedID); releaseErr != nil {
			logger.Warn(logComponent).Err(releaseErr).Str("session_id", claimedID).
				Msg("session create prepare compensation failed")
		}
	}
	if lockAcquired && externalLock != nil {
		externalLock.Unlock()
	}
}

// prewarmEligible 判定是否可走预热池（上游 6 条判据）。
// Python: prepare_session_create 的 prewarm_eligible 表达式
func (p *RuntimeSessionProvisioner) prewarmEligible(
	input SessionCreateInput,
	channelID string,
	isSwarm, moaEnabled bool,
	canonicalMode string,
) bool {
	if isSwarm {
		return false
	}
	if input.CronID != "" && equalFold(channelID, "web") {
		return false
	}
	if moaEnabled {
		return false
	}
	switch canonicalMode {
	case "agent", "code", "code.normal", "agent.work.normal", "agent.code.normal":
	default:
		return false
	}
	if p.extra.PrewarmModelEligible != nil {
		return p.extra.PrewarmModelEligible(input.ModelName)
	}
	return true
}

// usesProjectlessWorkspace 判定是否为无项目工作区。
// Python: RuntimeSessionProvisioner._uses_projectless_workspace
func (p *RuntimeSessionProvisioner) usesProjectlessWorkspace(params map[string]any, channelID string) bool {
	for _, key := range []string{"project_dir", "cwd"} {
		if v, ok := params[key].(string); ok && v != "" {
			return false
		}
	}
	// 取出并归一化 work_mode；缺省或非法时按渠道回落到默认工作模式。
	// 注意：上游把 work_mode 传给模式解析作为「补齐维度」（plan/code 等旧别名
	// 要靠它折叠成 code.normal / code.plan）。Go 侧 resolveSwitchMode 只做规范化、
	// 不接收补齐维度，故此处解析只看 mode 本身——对 agent/code 两个判定结果等价，
	// 差异仅在依赖 work_mode 才能折叠的旧别名上。
	workMode, _ := params["work_mode"].(string)
	workMode = toLowerSpaces(workMode)
	if workMode != "code" && workMode != "work" {
		workMode = defaultWorkModeForChannel(channelID)
	}
	mode, _ := params["mode"].(string)
	resolved, _ := p.resolveSwitchMode(mode, false)
	_ = workMode
	return resolved == "agent" || resolved == "code"
}

// prepareCreateOwner 准备 create 的归属与前置 target。
// Python: RuntimeSessionProvisioner._prepare_create_owner
func (p *RuntimeSessionProvisioner) prepareCreateOwner(
	ctx context.Context,
	channelID, sessionID, previousSessionID string,
	params map[string]any,
	isSwarm bool,
) (sessionpkg.SessionLifecycleTarget, *sessionpkg.SessionLifecycleTarget) {
	mode, _ := params["mode"].(string)
	if mode == "" {
		mode = "agent.plan"
	}
	target := sessionpkg.SessionLifecycleTarget{
		Descriptor: sessionpkg.SessionDescriptor{
			SessionID:  sessionID,
			ChannelID:  channelID,
			Mode:       mode,
			WorkMode:   stringOr(params["work_mode"], "work"),
			ProjectID:  stringOr(params["project_id"], ""),
			ProjectDir: firstNonEmpty(stringOr(params["project_dir"], ""), stringOr(params["cwd"], "")),
			UserID:     stringOr(params["user_id"], ""),
		},
		Kind:     sessionKindOf(isSwarm),
		TeamName: stringOr(params["team_name"], ""),
	}
	previousTarget := p.participants.Target(previousSessionID)
	previousIsTeam := previousTarget != nil && previousTarget.Kind == sessionpkg.SessionKindTeam
	if isSwarm || previousIsTeam {
		_ = p.prepareTeamOwnerSwitch(ctx, channelID, sessionID, previousIsTeam, previousSessionID,
			"session.create switch: ")
	}
	return target, previousTarget
}

// stageCreateResult 组装 create 的租约（AFTER_RESULT_DELIVERY，提交尝试不可回滚）。
// Python: RuntimeSessionProvisioner._stage_create_result
func (p *RuntimeSessionProvisioner) stageCreateResult(
	ctx context.Context,
	result SessionCreateResult,
	claimedID string,
	externalLock *sync.Mutex,
	switchTarget *sessionpkg.SessionLifecycleTarget,
	dispatchSignals *sessionpkg.SessionLifecycleTarget,
) *PreparedSessionProvision {
	finalized := false
	releaseResources := func() error {
		if finalized {
			return nil
		}
		if claimedID != "" && p.AgentManager != nil {
			if releaseErr := p.AgentManager.ReleaseSessionPrewarmClaim(
				context.Background(), claimedID); releaseErr != nil {
				logger.Warn(logComponent).Err(releaseErr).Str("session_id", claimedID).
					Msg("session create abort compensation failed")
			}
		}
		if externalLock != nil {
			externalLock.Unlock()
		}
		finalized = true
		return nil
	}
	commitCreate := func(commitCtx SessionProvisionCommitContext) error {
		defer func() {
			if externalLock != nil {
				externalLock.Unlock()
			}
		}()
		p.completedDeletes.remove(result.SessionID)
		if switchTarget != nil {
			viewID := commitCtx.ForegroundScopeID
			if viewID == "" {
				viewID = "default-view"
			}
			p.backgroundTasks.Add(1)
			go func() {
				defer p.backgroundTasks.Done()
				// KVC 前台切换为后台终结器（上游 create_task + done_callback）
				p.dispatchForegroundTransition(context.Background(),
					*switchTarget, dispatchSignals, viewID)
			}()
		}
		return nil
	}
	return p.stageSessionProvision(result, SessionCreateCommitTiming, commitCreate, releaseResources, true)
}

// stringOr 取字符串值或默认值。
func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

// isDefaultProjectID 判定是否为默认项目标识。
// Python: gateway/common/work_mode.is_default_project_id
func isDefaultProjectID(projectID string) bool {
	switch projectID {
	case "", "default", "none":
		return true
	}
	return false
}

// equalFold 大小写不敏感相等（仅 ASCII，避免引入 strings 仅为大小写）。
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// trimSpaces 去首尾空白。
func trimSpaces(s string) string {
	start, end := 0, len(s)
	for start < end && isSpaceByte(s[start]) {
		start++
	}
	for end > start && isSpaceByte(s[end-1]) {
		end--
	}
	return s[start:end]
}

// toLowerSpaces 去首尾空白并转小写。
func toLowerSpaces(s string) string {
	s = trimSpaces(s)
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// isSpaceByte 是否空白字节。
func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// ──────────────────────────── 结构体（删除） ────────────────────────────

// DeleteSessionOptions 删除会话的调用参数。
// Python: RuntimeSessionProvisioner.delete_session 关键字参数
// QuiesceSession / DisposeSession 由 AgentRuntime 传入（Go 用闭包，等价上游 Callable）。
type DeleteSessionOptions struct {
	// ChannelID 渠道标识
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// QuiesceSession 静默会话执行（关闭准入 + 等执行停下）
	QuiesceSession func(ctx context.Context, channelID, sessionID string) error
	// DisposeSession 释放会话内存运行时
	DisposeSession func(ctx context.Context, channelID, sessionID string) error
}

// ──────────────────────────── 导出函数（删除） ────────────────────────────

// DeleteSession 通过共享 Runtime 边界删除一个持久会话。
// Python: RuntimeSessionProvisioner.delete_session（L1444-1465）
// 七阶段事务见 spec §6；destructive_started 是回滚的唯一分界。
func (p *RuntimeSessionProvisioner) DeleteSession(
	ctx context.Context,
	opts DeleteSessionOptions,
) (sessionpkg.SessionDeleteResult, error) {
	target := trimSpaces(opts.SessionID)
	if target == "" {
		return sessionpkg.SessionDeleteResultFailure(target, "BAD_REQUEST",
			"session_id is required"), nil
	}
	lock := sessionpkg.SessionDeleteLock(target)
	lock.Lock()
	defer lock.Unlock()
	return p.deleteSessionLocked(ctx, opts, target), nil
}

// DeleteTeam 排他地通过 Runtime 业务边界删除一个团队。
// Python: RuntimeSessionProvisioner.delete_team（L1775-1878）
func (p *RuntimeSessionProvisioner) DeleteTeam(
	ctx context.Context,
	teamName, channelID string,
) (sessionpkg.TeamDeleteResult, error) {
	target := trimSpaces(teamName)
	if target == "" {
		return sessionpkg.TeamDeleteResult{OK: false, ErrorCode: "BAD_REQUEST",
			ErrorMessage: "team_name is required"}, nil
	}
	lock := sessionpkg.TeamDeleteLock(target)
	lock.Lock()
	defer lock.Unlock()
	return p.deleteTeamLocked(ctx, target, channelID), nil
}

// ──────────────────────────── 非导出函数（删除事务） ────────────────────────────

// deleteSessionLocked 持锁执行删除事务（七阶段）。
// Python: RuntimeSessionProvisioner._delete_session_locked（L1467-1773）
// 阶段顺序（spec §6.1）：
//
//	① participant.BeforeDelete
//	② trajectory.Begin
//	③ lifecycle.Begin
//	④ quiesce（team: QuiesceForDelete；agent: QuiesceSession）
//	⑤ participant.ReleaseResources         ← destructive_started 在此之后置 true
//	⑥ dispose（team: DisposeAfterResourceRelease；agent: DisposeSession + Runner.Release）
//	⑦ rmtree → commit 观察者 → commit_session_delete
func (p *RuntimeSessionProvisioner) deleteSessionLocked(
	ctx context.Context,
	opts DeleteSessionOptions,
	target string,
) sessionpkg.SessionDeleteResult {
	// 会话目录解析
	sessionDir := ""
	dirExists := false
	if p.extra.ResolveSessionDir != nil {
		sessionDir, dirExists = p.extra.ResolveSessionDir(target)
	}
	recovering := false
	if p.LifecycleJournal != nil {
		recovering = p.LifecycleJournal.RecoveringDelete(target)
	}
	p.mu.Lock()
	_, commitPending := p.pendingDeleteMeta[target]
	p.mu.Unlock()

	if !dirExists && !recovering && !commitPending {
		if cached, ok := p.completedDeletes.get(target); ok {
			return cached
		}
		return sessionpkg.SessionDeleteResultFailure(target, "NOT_FOUND", "session not found")
	}

	// metadata
	var metadata map[string]any
	if p.extra.SessionOps != nil {
		metadata = p.extra.SessionOps.GetSessionMetadata(target)
	}
	if metadata == nil {
		p.mu.Lock()
		metadata = p.pendingDeleteMeta[target]
		p.mu.Unlock()
	}
	isTeamSession := false
	if mode, _ := metadata["mode"].(string); mode != "" {
		_, isTeamSession = p.resolveSwitchMode(mode, false)
	}
	teamName, _ := metadata["team_name"].(string)
	channelID, _ := metadata["channel_id"].(string)
	if channelID == "" {
		channelID = opts.ChannelID
	}
	descriptor := sessionpkg.SessionDescriptor{
		SessionID:  target,
		ChannelID:  firstNonEmpty(channelID, "default"),
		Mode:       stringOr(metadata["mode"], "agent.plan"),
		WorkMode:   stringOr(metadata["work_mode"], "work"),
		ProjectID:  stringOr(metadata["project_id"], ""),
		ProjectDir: stringOr(metadata["project_dir"], ""),
		UserID:     stringOr(metadata["user_id"], ""),
	}
	deleteTarget := sessionpkg.SessionLifecycleTarget{
		Descriptor: descriptor,
		Kind:       sessionKindOf(isTeamSession),
		TeamName:   teamName,
	}
	result := sessionpkg.SessionDeleteResult{
		OK: true, SessionID: target, ChannelID: channelID,
		IsTeam: isTeamSession, TeamName: teamName,
	}

	// ① participant.BeforeDelete
	participants := p.participants.SnapshotDelete()
	entered := make([]sessionpkg.SessionDeleteParticipant, 0, len(participants))
	for _, participant := range participants {
		entered = append(entered, participant)
		if beforeErr := participant.BeforeDelete(ctx, deleteTarget); beforeErr != nil {
			logger.Warn(logComponent).Err(beforeErr).Str("session_id", target).
				Msg("Runtime delete participant before_delete failed")
		}
	}

	// 依赖检查（checkpoint）
	if p.CheckpointEnsure != nil {
		if cpErr := p.CheckpointEnsure(ctx); cpErr != nil {
			return sessionpkg.SessionDeleteResultFailure(target, "CHECKPOINT_UNAVAILABLE",
				"persistent checkpointer is unavailable")
		}
	}

	var (
		trajectoryPrepared bool
		lifecyclePrepared  bool
		destructiveStarted bool
		deleted            bool
	)
	lifecycle := p.pickDeleteLifecycle(target)

	// ② ③ 准备
	if p.TrajectoryHook != nil {
		if beginErr := p.TrajectoryHook.BeginSessionDelete(ctx, target); beginErr != nil {
			p.abortDelete(ctx, result, false, false, lifecycle, false)
			p.notifyDeleteFailed(ctx, entered, deleteTarget, false)
			return sessionpkg.SessionDeleteResultFailure(target, "DELETE_FAILED",
				"session runtime cleanup failed")
		}
		trajectoryPrepared = true
	}
	if prepErr := p.prepareDeleteLifecycle(target, lifecycle); prepErr == nil && lifecycle != nil {
		lifecyclePrepared = true
	}

	// ④ ⑥ 分流
	switch {
	case !dirExists && (recovering || commitPending):
		deleted = true
	case isTeamSession:
		outcome, ok := p.deleteTeamSessionPhase(ctx, opts, target, result,
			deleteTarget, entered, &destructiveStarted)
		if !ok {
			p.abortDelete(ctx, result, trajectoryPrepared, lifecyclePrepared, lifecycle,
				destructiveStarted)
			p.notifyDeleteFailed(ctx, entered, deleteTarget, destructiveStarted)
			return outcome
		}
		deleted = outcome.Deleted
	default:
		outcome, ok := p.deleteAgentSessionPhase(ctx, opts, target, result,
			deleteTarget, entered, &destructiveStarted)
		if !ok {
			p.abortDelete(ctx, result, trajectoryPrepared, lifecyclePrepared, lifecycle,
				destructiveStarted)
			p.notifyDeleteFailed(ctx, entered, deleteTarget, destructiveStarted)
			return outcome
		}
		deleted = outcome.Deleted
	}

	if !deleted {
		p.abortDelete(ctx, result, trajectoryPrepared, lifecyclePrepared, lifecycle, destructiveStarted)
		p.notifyDeleteFailed(ctx, entered, deleteTarget, destructiveStarted)
		return cleanupFailedResult(result, destructiveStarted)
	}
	// ⑦ rmtree
	if dirExists && p.extra.RemoveSessionDir != nil {
		if rmErr := p.extra.RemoveSessionDir(sessionDir); rmErr != nil {
			p.abortDelete(ctx, result, trajectoryPrepared, lifecyclePrepared, lifecycle,
				destructiveStarted)
			p.notifyDeleteFailed(ctx, entered, deleteTarget, destructiveStarted)
			return cleanupFailedResult(result, destructiveStarted)
		}
	}
	if recovering && p.LifecycleJournal != nil {
		_ = p.LifecycleJournal.MarkCleanup(target)
	}

	// 提交观察者
	p.mu.Lock()
	p.pendingDeleteMeta[target] = metadata
	p.mu.Unlock()
	committed := sessionpkg.SessionDeleteResult{
		OK: true, SessionID: result.SessionID, ChannelID: result.ChannelID,
		IsTeam: result.IsTeam, TeamName: result.TeamName, Deleted: true,
	}
	if commitErr := p.commitDeleteObservers(ctx, committed, trajectoryPrepared,
		lifecyclePrepared, lifecycle); commitErr != nil {
		p.notifyDeleteFailed(ctx, entered, deleteTarget, true)
		return sessionpkg.SessionDeleteResult{
			OK: false, SessionID: target, ChannelID: result.ChannelID,
			IsTeam: result.IsTeam, TeamName: result.TeamName,
			Deleted: true, RecoveryRequired: true,
			ErrorCode:    "DELETE_COMMIT_PENDING",
			ErrorMessage: "session was deleted but commit is pending",
		}
	}
	p.commitSessionDelete(committed)
	if p.TeamExecutionController != nil && isTeamSession {
		p.TeamExecutionController.DeleteCommitted(deleteTarget)
	}
	p.notifyDeleteCommitted(ctx, entered, deleteTarget)
	p.completedDeletes.put(target, committed)
	p.mu.Lock()
	delete(p.pendingDeleteMeta, target)
	p.mu.Unlock()
	return committed
}

// deleteAgentSessionPhase 单 agent 会话的 ④⑤⑥ 阶段。
// Python: _delete_session_locked 中 `else` 分支（quiesce → release → dispose → Runner.release）
// 返回 (结果, 是否继续)；ok=false 表示已失败应回滚。
func (p *RuntimeSessionProvisioner) deleteAgentSessionPhase(
	ctx context.Context,
	opts DeleteSessionOptions,
	_ string,
	result sessionpkg.SessionDeleteResult,
	deleteTarget sessionpkg.SessionLifecycleTarget,
	entered []sessionpkg.SessionDeleteParticipant,
	destructiveStarted *bool,
) (sessionpkg.SessionDeleteResult, bool) {
	fail := sessionpkg.SessionDeleteResultFailure(result.SessionID, "DELETE_FAILED",
		"session runtime cleanup failed")
	if opts.QuiesceSession == nil {
		return fail, false
	}
	if quiesceErr := opts.QuiesceSession(ctx, result.ChannelID, result.SessionID); quiesceErr != nil {
		logger.Warn(logComponent).Err(quiesceErr).Str("session_id", result.SessionID).
			Msg("Runtime session quiesce failed")
		return fail, false
	}
	p.releaseParticipants(ctx, entered, deleteTarget)
	*destructiveStarted = true
	if opts.DisposeSession != nil {
		if disposeErr := opts.DisposeSession(ctx, result.ChannelID, result.SessionID); disposeErr != nil {
			logger.Warn(logComponent).Err(disposeErr).Str("session_id", result.SessionID).
				Msg("Runtime session dispose failed")
			return cleanupFailedResult(result, *destructiveStarted), false
		}
	}
	if p.RunnerRelease != nil {
		if releaseErr := p.RunnerRelease(ctx, result.SessionID); releaseErr != nil {
			logger.Warn(logComponent).Err(releaseErr).Str("session_id", result.SessionID).
				Msg("Runtime Runner.release failed")
			return cleanupFailedResult(result, *destructiveStarted), false
		}
	}
	out := result
	out.Deleted = true
	return out, true
}

// deleteTeamSessionPhase 团队会话的 ④⑤⑥ 阶段。
// Python: _delete_session_locked 中 `elif is_team_session` 分支
// （QuiesceForDelete → release → DisposeAfterResourceRelease → _delete_team_runner）
func (p *RuntimeSessionProvisioner) deleteTeamSessionPhase(
	ctx context.Context,
	_ DeleteSessionOptions,
	_ string,
	result sessionpkg.SessionDeleteResult,
	deleteTarget sessionpkg.SessionLifecycleTarget,
	entered []sessionpkg.SessionDeleteParticipant,
	destructiveStarted *bool,
) (sessionpkg.SessionDeleteResult, bool) {
	fail := sessionpkg.SessionDeleteResultFailure(result.SessionID, "DELETE_FAILED",
		"session runtime cleanup failed")
	if p.TeamExecutionController == nil {
		return fail, false
	}
	if quiesceErr := p.TeamExecutionController.QuiesceForDelete(ctx, deleteTarget,
		"session.delete: "); quiesceErr != nil {
		logger.Warn(logComponent).Err(quiesceErr).Str("session_id", result.SessionID).
			Msg("Runtime team quiesce failed")
		return fail, false
	}
	p.releaseParticipants(ctx, entered, deleteTarget)
	*destructiveStarted = true
	if disposeErr := p.TeamExecutionController.DisposeAfterResourceRelease(ctx, deleteTarget,
		"session.delete: "); disposeErr != nil {
		logger.Warn(logComponent).Err(disposeErr).Str("session_id", result.SessionID).
			Msg("Runtime team dispose failed")
		return cleanupFailedResult(result, *destructiveStarted), false
	}
	out := result
	out.Deleted = p.deleteTeamRunner(ctx, result)
	return out, true
}

// pickDeleteLifecycle 取本次事务使用的删除生命周期参与者。
// Python: _delete_session_locked 里的
// _prepared_session_deletes.get(target, self._delete_lifecycle)
func (p *RuntimeSessionProvisioner) pickDeleteLifecycle(target string) SessionDeleteLifecycle {
	p.mu.Lock()
	defer p.mu.Unlock()
	if prepared, ok := p.preparedDeletes[target]; ok {
		return prepared
	}
	return p.DeleteLifecycle
}

// prepareDeleteLifecycle 准备删除生命周期（幂等）。
// Python: RuntimeSessionProvisioner._prepare_delete_lifecycle
func (p *RuntimeSessionProvisioner) prepareDeleteLifecycle(target string, lifecycle SessionDeleteLifecycle) error {
	if lifecycle == nil {
		return nil
	}
	p.mu.Lock()
	already := p.preparedDeletes[target] == lifecycle
	p.mu.Unlock()
	if already {
		return nil
	}
	if beginErr := lifecycle.BeginSessionDelete(target); beginErr != nil {
		return beginErr
	}
	if lifecycle == p.DeleteLifecycle {
		p.mu.Lock()
		p.preparedDeletes[target] = lifecycle
		p.mu.Unlock()
	}
	return nil
}

// releaseParticipants 逐个释放参与者资源（单条失败只告警）。
// Python: RuntimeSessionProvisioner._release_participants
func (p *RuntimeSessionProvisioner) releaseParticipants(
	ctx context.Context,
	participants []sessionpkg.SessionDeleteParticipant,
	target sessionpkg.SessionLifecycleTarget,
) {
	for _, participant := range participants {
		if releaseErr := participant.ReleaseResources(ctx, target); releaseErr != nil {
			logger.Warn(logComponent).Err(releaseErr).
				Str("participant", participant.Name()).
				Str("session_id", target.Descriptor.SessionID).
				Msg("Runtime delete participant release failed")
		}
	}
}

// notifyDeleteFailed 逆序通知参与者删除失败。
// Python: RuntimeSessionProvisioner._notify_delete_failed（逆序遍历）
func (p *RuntimeSessionProvisioner) notifyDeleteFailed(
	ctx context.Context,
	participants []sessionpkg.SessionDeleteParticipant,
	target sessionpkg.SessionLifecycleTarget,
	destructiveStarted bool,
) {
	for i := len(participants) - 1; i >= 0; i-- {
		participant := participants[i]
		if failErr := participant.DeleteFailed(ctx, target, destructiveStarted); failErr != nil {
			logger.Warn(logComponent).Err(failErr).
				Str("participant", participant.Name()).
				Msg("Runtime delete participant failure notification failed")
		}
	}
}

// notifyDeleteCommitted 顺序通知参与者删除已提交。
// Python: RuntimeSessionProvisioner._notify_delete_committed
func (p *RuntimeSessionProvisioner) notifyDeleteCommitted(
	ctx context.Context,
	participants []sessionpkg.SessionDeleteParticipant,
	target sessionpkg.SessionLifecycleTarget,
) {
	for _, participant := range participants {
		if commitErr := participant.DeleteCommitted(ctx, target); commitErr != nil {
			logger.Warn(logComponent).Err(commitErr).
				Str("participant", participant.Name()).
				Msg("Runtime delete participant commit notification failed")
		}
	}
}

// abortDelete 回滚（destructiveStarted 之后直接返回）。
// Python: RuntimeSessionProvisioner._abort_delete
func (p *RuntimeSessionProvisioner) abortDelete(
	ctx context.Context,
	result sessionpkg.SessionDeleteResult,
	trajectoryPrepared, lifecyclePrepared bool,
	lifecycle SessionDeleteLifecycle,
	destructiveStarted bool,
) {
	if destructiveStarted {
		return
	}
	if trajectoryPrepared && p.TrajectoryHook != nil {
		if abortErr := p.TrajectoryHook.AbortSessionDelete(ctx, result.SessionID); abortErr != nil {
			logger.Warn(logComponent).Err(abortErr).
				Msg("Runtime trajectory delete rollback failed")
		}
	}
	if lifecyclePrepared && lifecycle != nil {
		// 失败的产品删除仍保持阻塞并会被重试：保留其已准备的心跳围栏
		if p.extra.DeletePending != nil && p.extra.DeletePending(result.SessionID) {
			return
		}
		if abortErr := lifecycle.AbortSessionDelete(result.SessionID, result.ChannelID); abortErr != nil {
			logger.Warn(logComponent).Err(abortErr).
				Msg("Runtime Session delete lifecycle rollback failed")
			return
		}
		p.mu.Lock()
		if p.preparedDeletes[result.SessionID] == lifecycle {
			delete(p.preparedDeletes, result.SessionID)
		}
		p.mu.Unlock()
	}
}

// commitDeleteObservers 提交轨迹与生命周期观察者。
// Python: RuntimeSessionProvisioner._commit_delete_observers
func (p *RuntimeSessionProvisioner) commitDeleteObservers(
	ctx context.Context,
	result sessionpkg.SessionDeleteResult,
	trajectoryPrepared, lifecyclePrepared bool,
	lifecycle SessionDeleteLifecycle,
) error {
	if trajectoryPrepared && p.TrajectoryHook != nil {
		if commitErr := p.TrajectoryHook.CommitSessionDelete(ctx, result.SessionID); commitErr != nil {
			logger.Warn(logComponent).Err(commitErr).Msg("Runtime trajectory delete commit failed")
			return commitErr
		}
	}
	if lifecyclePrepared && lifecycle != nil {
		if commitErr := lifecycle.CommitSessionDelete(result.SessionID); commitErr != nil {
			logger.Warn(logComponent).Err(commitErr).
				Msg("Runtime Session delete lifecycle commit failed")
			return commitErr
		}
		p.mu.Lock()
		if p.preparedDeletes[result.SessionID] == lifecycle {
			delete(p.preparedDeletes, result.SessionID)
		}
		p.mu.Unlock()
	}
	return nil
}

// commitSessionDelete 提交 Runtime 侧的 Plan/缓存/绑定状态。
// Python: RuntimeSessionProvisioner.commit_session_delete（L2133-2164）
// 团队会话解绑失败时上游 raise（删除已提交，但绑定未解）。
func (p *RuntimeSessionProvisioner) commitSessionDelete(result sessionpkg.SessionDeleteResult) {
	if p.PlanController != nil {
		p.PlanController.ResetSession(result.SessionID)
	}
	p.participants.ForgetTarget(result.SessionID)
	if p.RemoveMetadataCache != nil {
		p.RemoveMetadataCache(result.SessionID)
	}
	if !result.IsTeam {
		return
	}
	if p.TeamBinding == nil {
		return
	}
	if unbindErr := p.TeamBinding.UnbindSession(result.TeamName, result.SessionID); unbindErr != nil {
		logger.Warn(logComponent).Err(unbindErr).
			Str("session_id", result.SessionID).Str("team_name", result.TeamName).
			Msg("Runtime failed to unbind deleted team session")
	}
}

// deleteTeamRunner 删除团队会话的 Runner 侧资源。
// Python: RuntimeSessionProvisioner._delete_team_runner
func (p *RuntimeSessionProvisioner) deleteTeamRunner(
	ctx context.Context,
	result sessionpkg.SessionDeleteResult,
) bool {
	if p.DeleteAgentTeam == nil {
		return false
	}
	if result.TeamName != "" {
		deleted, err := p.DeleteAgentTeam(ctx, result.TeamName, []string{result.SessionID})
		if err != nil {
			return false
		}
		return deleted
	}
	logger.Warn(logComponent).Str("session_id", result.SessionID).
		Msg("Runtime team Session delete fell back to Runner.release: reason=missing_team_name")
	if p.RunnerRelease == nil {
		return true
	}
	return p.RunnerRelease(ctx, result.SessionID) == nil
}

// disposeSessionAfterResourceRelease 释放会话内存运行时。
// Python: runtime/service.py _dispose_agent_session_after_resource_release（:3424）
//
// 三步顺序不得调整（短路顺序即语义边界）：
//  1. PromptForks.SessionDeleted(session_id)               清建议生成
//  2. SubagentStopper.StopSubagentRuntimes(session_id)     停常驻子代理
//  3. SessionCleaner.CleanupSession(session_id)            清内存运行时
//
// 注意第 3 步的 resetPlanState 恒为 false：删除事务还在进行，plan 状态要留到
// commit 之后由 commitSessionDelete 重置，否则回滚时状态已丢。
//
// 单条失败只告警不中断（对齐上游逐步 try/except 语义），方法本身恒返回 nil。
func (p *RuntimeSessionProvisioner) disposeSessionAfterResourceRelease(
	ctx context.Context, channelID, sessionID string,
) error {
	if p.PromptForks != nil {
		p.PromptForks.SessionDeleted(sessionID)
	}
	if p.SubagentStopper != nil {
		if err := p.SubagentStopper.StopSubagentRuntimes(ctx, sessionID, "session_deleted"); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Msg("Runtime session delete could not release subagents")
		}
	}
	if p.SessionCleaner != nil {
		if err := p.SessionCleaner.CleanupSession(ctx, channelID, sessionID); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Msg("Runtime session delete could not clean session runtime")
		}
	}
	return nil
}

// DisposeSessionAfterResourceRelease 导出给 server 侧注入 dispose 回调。
// Python: 同一函数的对外入口（server 侧把 self._dispose_agent_session_after_resource_release 传入）
func (p *RuntimeSessionProvisioner) DisposeSessionAfterResourceRelease(
	ctx context.Context, channelID, sessionID string,
) error {
	return p.disposeSessionAfterResourceRelease(ctx, channelID, sessionID)
}

// cleanupFailedResult 构造 DELETE_FAILED 结果。
// Python: RuntimeSessionProvisioner._cleanup_failed
func cleanupFailedResult(
	result sessionpkg.SessionDeleteResult,
	destructiveStarted bool,
) sessionpkg.SessionDeleteResult {
	return sessionpkg.SessionDeleteResult{
		OK: false, SessionID: result.SessionID, ChannelID: result.ChannelID,
		IsTeam: result.IsTeam, TeamName: result.TeamName,
		Deleted: false, RecoveryRequired: destructiveStarted,
		ErrorCode:    "DELETE_FAILED",
		ErrorMessage: "session runtime cleanup failed",
	}
}

// deleteTeamLocked 持锁删除团队。
// Python: RuntimeSessionProvisioner._delete_team_locked（L1910-2131）
func (p *RuntimeSessionProvisioner) deleteTeamLocked(
	ctx context.Context,
	teamName, channelID string,
) sessionpkg.TeamDeleteResult {
	target := trimSpaces(teamName)
	if target == "" {
		return sessionpkg.TeamDeleteResult{OK: false, ErrorCode: "BAD_REQUEST",
			ErrorMessage: "team_name is required"}
	}

	gate := sessionpkg.GlobalTeamDeletionGate()
	if p.TeamDeletionGate != nil {
		gate = p.TeamDeletionGate
	}
	gate.BeginDeleteLocked(target)
	defer gate.FinishDeleteLocked(target)

	// Python: TeamDeletionGate.mutation_lock 包住 binding/entity 读写。
	// 没有这把锁时，删除期间并发的团队变更（改名/加成员）会与删除交错，
	// 留下半删的绑定记录。
	mutationLock := gate.MutationLock(target)
	mutationLock.Lock()
	defer mutationLock.Unlock()

	if cached, ok := p.completedTeamDeletes.get(target); ok {
		return cached
	}

	if p.TeamExecutionController != nil {
		if quiesceErr := p.TeamExecutionController.QuiesceForDelete(ctx,
			sessionpkg.SessionLifecycleTarget{TeamName: target}, "team.delete: "); quiesceErr != nil {
			logger.Warn(logComponent).Err(quiesceErr).Str("team_name", target).
				Msg("Runtime team quiesce failed")
		}
	}

	var sessionIDs []string
	if p.extra.ListTeamSessions != nil {
		sessionIDs = p.extra.ListTeamSessions(target)
	}

	failed := []string{}
	for _, sessionID := range sessionIDs {
		res, _ := p.DeleteSession(ctx, DeleteSessionOptions{
			ChannelID: channelID,
			SessionID: sessionID,
		})
		if !res.Deleted {
			failed = append(failed, sessionID)
		}
	}

	// Runner 侧团队删除（一次）
	deleted := true
	if p.DeleteAgentTeam != nil {
		ok, err := p.DeleteAgentTeam(ctx, target, sessionIDs)
		if err != nil || !ok {
			deleted = false
		}
	}

	if p.TeamExecutionController != nil {
		lifecycleTarget := sessionpkg.SessionLifecycleTarget{TeamName: target}
		if deleted {
			p.TeamExecutionController.DeleteCommitted(lifecycleTarget)
		} else {
			p.TeamExecutionController.DeleteAborted(lifecycleTarget)
		}
	}

	result := sessionpkg.TeamDeleteResult{
		OK:               deleted && len(failed) == 0,
		TeamName:         target,
		SessionIDs:       sessionIDs,
		FailedSessionIDs: failed,
		Deleted:          deleted,
	}
	if len(failed) > 0 {
		result.ErrorCode = "DELETE_FAILED"
		result.ErrorMessage = "one or more team sessions failed to delete"
		result.RecoveryRequired = true
	}
	p.completedTeamDeletes.put(target, result)
	return result
}
