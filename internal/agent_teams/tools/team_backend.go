package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OnInbound 团队→用户通知回调。
// Python: OnInbound = Callable[[HumanAgentInboundEvent], Awaitable[None]]
// 复用 interaction 包的 OnInbound 类型签名，但 tools 包不 import interaction（避免循环），
// 因此在此独立定义。
type OnInbound func(ctx context.Context, memberName string, payload any) error

// TeamBackend 团队后端门面，组合 DB + TaskManager + MessageManager + Messager。
// Python: TeamBackend (openjiuwen/agent_teams/tools/team.py)
//
// 提供团队级业务方法：成员生命周期、团队生命周期、HITT 名册、
// 文件清理、任务操作、跨域组合操作。
// HITT 缓存由独立 hittMu 保护，DB 操作靠 DAO 层并发安全。
type TeamBackend struct {
	// ── 必填字段 ──
	// teamName 团队名称
	teamName string
	// memberName 当前成员名称
	memberName string
	// isLeader 是否为领导者
	isLeader bool
	// leaderMemberName 领导者成员名称
	leaderMemberName string
	// db 团队数据库
	db database.TeamDatabase
	// messager 消息发送器
	messager messager.Messager
	// taskManager 任务管理器
	taskManager *TeamTaskManager
	// messageManager 消息管理器
	messageManager *TeamMessageManager

	// ── 可选字段（Functional Options 注入）──
	// teammateMode 队友模式
	teammateMode string
	// predefinedMembers 预定义成员规格
	predefinedMembers []atschema.TeamMemberSpec
	// modelConfigAllocator 模型配置分配器
	modelConfigAllocator func(modelName string) *models.Allocation
	// leaderAllocation 领导者资源分配
	leaderAllocation *models.Allocation
	// onTeamCleaned 团队清理回调
	onTeamCleaned func(ctx context.Context) error
	// onTeamBuilt 团队构建完成回调
	onTeamBuilt func(ctx context.Context) error
	// onMemberStarted 成员启动回调（spawn 落地）
	//
	// 存在 backend 上而不是每次 startup 传参：调用方（发消息、建任务）只需
	// 表达"确保名册都起来了"，不必各自揣着 spawn 回调。
	//
	// Python: self._on_member_started (tools/team.py:348)
	onMemberStarted func(ctx context.Context, memberName string) error
	// planStorageDir 计划存储目录
	planStorageDir string
	// planID 当前计划标识
	planID string

	// ── HITT 缓存（hittMu 保护）──
	// hittMu HITT 缓存读写锁
	hittMu sync.RWMutex
	// specEnableHITT 配置是否启用 HITT
	specEnableHITT bool
	// enableHITT 运行时是否启用 HITT
	enableHITT bool
	// hittNames HITT 名册名称集合
	hittNames map[string]struct{}
	// hittInboundCallbacks HITT 入站回调映射
	hittInboundCallbacks map[string]OnInbound

	// ── 文件系统清理路径 ──
	// cleanupPaths 待清理的文件路径集合
	cleanupPaths map[string]struct{}

	// ── 能力开关（hittMu 保护）──
	// dispatchMode 本团队的调度模式（"autonomous" / "scheduled"）
	dispatchMode string
	// specEnableTaskVerification 配置层的任务验证开关天花板
	specEnableTaskVerification bool
	// enableTaskVerification 运行时的任务验证开关生效值
	enableTaskVerification bool
	// enableMemberMessaging scheduled 成员可否互发消息（F_126）
	enableMemberMessaging bool
	// maxSubtaskDepth 成员拆子任务的深度上限（F_128），0=不启用
	maxSubtaskDepth int
}

// TeamBackendOption TeamBackend 构造可选参数。
type TeamBackendOption func(*TeamBackend)

// ShutdownOption ShutdownMember 可选参数。
// Python: shutdown_member(force=False)
type ShutdownOption func(*shutdownConfig)

// shutdownConfig ShutdownMember 配置
type shutdownConfig struct {
	// force 是否强制关闭（对齐 Python: force=False）
	force bool
}

// ApprovePlanOption ApprovePlan 可选参数。
// Python: approve_plan(approved=True, feedback=None)
type ApprovePlanOption func(*approvePlanConfig)

// approvePlanConfig ApprovePlan 配置
type approvePlanConfig struct {
	// approved 是否批准（对齐 Python: approved=True）
	approved bool
	// feedback 反馈意见（对齐 Python: feedback=None）
	feedback string
}

// SpawnMemberOption SpawnMember 可选参数。
// Python: spawn_member(status=UNSTARTED, execution_status=IDLE, mode=BUILD_MODE, allocation=None)
type SpawnMemberOption func(*spawnMemberConfig)

// spawnMemberConfig SpawnMember 配置
type spawnMemberConfig struct {
	// status 成员状态（对齐 Python: status=UNSTARTED）
	status string
	// executionStatus 执行状态（对齐 Python: execution_status=IDLE）
	executionStatus string
	// mode 成员模式（对齐 Python: mode=BUILD_MODE）
	mode string
	// allocation 模型配置分配（对齐 Python: allocation=None）
	allocation *models.Allocation
	// fallbackAllocation 外部 CLI 成员的认证兜底分配
	fallbackAllocation *models.Allocation
	// isolation 隔离模式（"worktree" 或空）
	isolation string
	// cliAgent 外部 CLI 后端名
	cliAgent string
	// builtinModel 外部 CLI 成员自带登录态使用的内置模型
	builtinModel *database.MemberBuiltinModel
	// permissionsOverride 成员级权限收窄
	permissionsOverride map[string]string
	// lifetime task-scoped 成员的退役策略（空 = 常驻）
	lifetime *database.MemberLifetime
	// agentTemplate 专家模板
	agentTemplate *database.MemberAgentTemplate
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrHITTConfigInvalid build_team(enable_hitt=True) 但 spec_enable_hitt=False 时返回。
	// Python: raise_error(StatusCode.AGENT_TEAM_CONFIG_INVALID, ...)
	ErrHITTConfigInvalid = errors.New("build_team(enable_hitt=True) 要求 spec_enable_hitt=True；能力天花板被违反")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTeamOutcome build_team 的执行结果。
//
// TakenOver 为 true 表示本次是接管已有团队：没写行、没注册成员、没发
// TeamCreated 事件，只把 onTeamBuilt 回调走了一遍。
//
// EnableTaskVerification 是生效的任务验证开关。接管时它从已有团队行读回
// ——那次调用的能力参数对已经配好的团队不适用，因为团队在建队时就配置完了、
// 成员已按那套配置在跑、任务可能也已被它塑形；leader 只能从工具结果里得知
// 自己实际拿到了什么。
type BuildTeamOutcome struct {
	// TakenOver 本次是接管已有团队
	TakenOver bool
	// EnableTaskVerification 生效的任务验证开关
	EnableTaskVerification bool
}

// WithTeammateMode 设置默认成员模式。
func WithTeammateMode(mode string) TeamBackendOption {
	return func(tb *TeamBackend) { tb.teammateMode = mode }
}

// WithPredefinedMembers 设置预定义成员列表。
func WithPredefinedMembers(members []atschema.TeamMemberSpec) TeamBackendOption {
	return func(tb *TeamBackend) { tb.predefinedMembers = members }
}

// WithModelConfigAllocator 设置模型分配回调。
func WithModelConfigAllocator(fn func(modelName string) *models.Allocation) TeamBackendOption {
	return func(tb *TeamBackend) { tb.modelConfigAllocator = fn }
}

// WithLeaderAllocation 设置 Leader 模型分配。
func WithLeaderAllocation(a *models.Allocation) TeamBackendOption {
	return func(tb *TeamBackend) { tb.leaderAllocation = a }
}

// WithEnableHITT 设置 HITT 能力开关（spec 级天花板）。
func WithEnableHITT(enable bool) TeamBackendOption {
	return func(tb *TeamBackend) { tb.specEnableHITT = enable; tb.enableHITT = enable }
}

// WithDispatchMode 设置本团队的调度模式（"autonomous" / "scheduled"）。
// Python: TeamBackend.dispatch_mode（由 spec 传入）
func WithDispatchMode(mode string) TeamBackendOption {
	return func(tb *TeamBackend) {
		if mode != "" {
			tb.dispatchMode = mode
		}
	}
}

// WithMemberMessaging 设置 scheduled 成员可否互发消息（F_126）。
// Python: TeamBackend._enable_member_messaging
func WithMemberMessaging(enabled bool) TeamBackendOption {
	return func(tb *TeamBackend) { tb.enableMemberMessaging = enabled }
}

// WithMaxSubtaskDepth 设置成员拆子任务的深度上限（F_128）。
// Python: TeamBackend._max_subtask_depth
func WithMaxSubtaskDepth(depth int) TeamBackendOption {
	return func(tb *TeamBackend) { tb.maxSubtaskDepth = depth }
}

// WithSpecEnableTaskVerification 设置任务验证开关的天花板。
// Python: TeamBackend._spec_enable_task_verification
func WithSpecEnableTaskVerification(enabled bool) TeamBackendOption {
	return func(tb *TeamBackend) { tb.specEnableTaskVerification = enabled }
}

// WithOnTeamCleaned 设置团队清理回调。
// Python: on_team_cleaned 参数
func WithOnTeamCleaned(fn func(ctx context.Context) error) TeamBackendOption {
	return func(tb *TeamBackend) { tb.onTeamCleaned = fn }
}

// WithOnTeamBuilt 设置团队构建回调。
// Python: on_team_built 参数
func WithOnTeamBuilt(fn func(ctx context.Context) error) TeamBackendOption {
	return func(tb *TeamBackend) { tb.onTeamBuilt = fn }
}

// WithOnMemberStarted 设置成员启动回调（spawn 落地时由 TeamManager 通知）。
//
// Python: on_member_started 参数（tools/team.py:148）
func WithOnMemberStarted(fn func(ctx context.Context, memberName string) error) TeamBackendOption {
	return func(tb *TeamBackend) { tb.onMemberStarted = fn }
}

// WithPlanStorageDir 设置计划文件存储目录。
func WithPlanStorageDir(dir string) TeamBackendOption {
	return func(tb *TeamBackend) { tb.planStorageDir = dir }
}

// WithPlanID 设置团队级计划标识。
func WithPlanID(id string) TeamBackendOption {
	return func(tb *TeamBackend) { tb.planID = id }
}

// WithLeaderMemberName 设置 Leader 成员名（覆盖默认值）。
func WithLeaderMemberName(name string) TeamBackendOption {
	return func(tb *TeamBackend) { tb.leaderMemberName = name }
}

// WithForce 设置强制关闭（对齐 Python: shutdown_member(force=True)）。
func WithForce(force bool) ShutdownOption {
	return func(c *shutdownConfig) { c.force = force }
}

// WithApproved 设置是否批准（对齐 Python: approve_plan(approved=False) 可拒绝计划）。
func WithApproved(approved bool) ApprovePlanOption {
	return func(c *approvePlanConfig) { c.approved = approved }
}

// WithFeedback 设置反馈意见（对齐 Python: approve_plan(feedback="...")）。
func WithFeedback(feedback string) ApprovePlanOption {
	return func(c *approvePlanConfig) { c.feedback = feedback }
}

// WithStatus 设置成员状态（对齐 Python: spawn_member(status=...)）。
func WithStatus(s string) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.status = s }
}

// WithExecutionStatus 设置执行状态（对齐 Python: spawn_member(execution_status=...)）。
func WithExecutionStatus(s string) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.executionStatus = s }
}

// WithMode 设置成员模式（对齐 Python: spawn_member(mode=...)）。
func WithMode(m string) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.mode = m }
}

// WithAllocation 设置模型配置分配（对齐 Python: spawn_member(allocation=...)）。
func WithAllocation(a *models.Allocation) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.allocation = a }
}

// WithFallbackAllocation 设置认证兜底分配（对齐 Python: spawn_member(fallback_allocation=...)）。
func WithFallbackAllocation(a *models.Allocation) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.fallbackAllocation = a }
}

// WithIsolation 设置隔离模式（对齐 Python: spawn_member(isolation=...)）。
func WithIsolation(isolation string) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.isolation = isolation }
}

// WithCLIAgent 设置外部 CLI 后端名（对齐 Python: spawn_member(cli_agent=...)）。
func WithCLIAgent(name string) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.cliAgent = name }
}

// WithBuiltinModel 设置内置 CLI 模型（对齐 Python: spawn_member(builtin_model=...)）。
func WithBuiltinModel(m *database.MemberBuiltinModel) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.builtinModel = m }
}

// WithPermissionsOverride 设置成员级权限收窄（对齐 Python: spawn_member(permissions_override=...)）。
func WithPermissionsOverride(o map[string]string) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.permissionsOverride = o }
}

// WithLifetime 设置退役策略（对齐 Python: spawn_member(lifetime=...)）。
func WithLifetime(l *database.MemberLifetime) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.lifetime = l }
}

// WithAgentTemplate 设置专家模板（对齐 Python: spawn_member(agent_template=...)）。
func WithAgentTemplate(t *database.MemberAgentTemplate) SpawnMemberOption {
	return func(c *spawnMemberConfig) { c.agentTemplate = t }
}

// RegisterTeammate 注册一条普通 LLM 成员行（UNSTARTED）。
//
// 唯一知道"成员的 card 与 mode 怎么派生"的地方。spawn_teammate、
// create_task 的内联 member、spawn_expert（带 agentTemplate）都走它，
// 三处不会在 card id / mode / options 上漂移。
//
// Python: register_teammate (tools/tool_member.py:626-667)
func RegisterTeammate(
	team *TeamBackend,
	memberName, displayName, desc string,
	prompt *string,
	allocation *models.Allocation,
	opts ...SpawnMemberOption,
) atschema.MemberOpResult {
	if team == nil {
		return atschema.NewMemberOpResultFail("team backend is nil")
	}
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID(fmt.Sprintf("%s_%s", team.teamName, memberName)),
		agentschema.WithAgentName(displayName),
		agentschema.WithAgentDescription(desc),
	)
	promptText := ""
	if prompt != nil {
		promptText = *prompt
	}
	allOpts := append([]SpawnMemberOption{
		WithMode(string(team.teammateMode)),
		WithAllocation(allocation),
	}, opts...)
	return team.SpawnMember(context.Background(), memberName, displayName, card,
		string(atschema.TeamRoleTeammate), desc, promptText, "", allOpts...)
}

// NewTeamBackend 创建团队后端门面。
// Python: TeamBackend.__init__(team_name, member_name, is_leader, db, messager, ...)
func NewTeamBackend(
	teamName, memberName string, isLeader bool,
	db database.TeamDatabase, msg messager.Messager,
	opts ...TeamBackendOption,
) *TeamBackend {
	tb := &TeamBackend{
		teamName:             teamName,
		memberName:           memberName,
		isLeader:             isLeader,
		leaderMemberName:     "", // 由 WithLeaderMemberName 或下方逻辑设置
		db:                   db,
		messager:             msg,
		teammateMode:         string(atschema.MemberModeBuildMode),
		dispatchMode:         "autonomous",
		hittNames:            make(map[string]struct{}),
		hittInboundCallbacks: make(map[string]OnInbound),
		cleanupPaths:         make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(tb)
	}
	// Python: self.leader_member_name = str(leader_member_name or (member_name if is_leader else "")).strip()
	// 如果 WithLeaderMemberName 未设置（空字符串），则 isLeader 时回退到 memberName
	if tb.leaderMemberName == "" && isLeader {
		tb.leaderMemberName = memberName
	}
	// 内部构造 MessageManager（需在 TaskManager 之前，因 TaskManager 依赖 MessageManager）
	tb.messageManager = NewTeamMessageManager(db, teamName, memberName, msg)
	tb.taskManager = NewTeamTaskManager(
		db, teamName, memberName, msg,
		tb.messageManager,
		tb.planStorageDir, tb.planID, tb.leaderMemberName,
	)

	logger.Info(logComponent).Str("team_name", teamName).Str("member_name", memberName).
		Msg("TeamBackend initialized")

	return tb
}

// TeamName 返回团队名。
// Python: TeamBackend.team_name
func (tb *TeamBackend) TeamName() string { return tb.teamName }

// MemberName 返回当前成员名。
// Python: TeamBackend.member_name
func (tb *TeamBackend) MemberName() string { return tb.memberName }

// IsLeader 返回是否 Leader。
// Python: TeamBackend.is_leader
func (tb *TeamBackend) IsLeader() bool { return tb.isLeader }

// TeammateMode 返回队友模式。
// Python: TeamBackend.teammate_mode
func (tb *TeamBackend) TeammateMode() string { return tb.teammateMode }

// LeaderMemberName 返回 Leader 成员名。
// Python: TeamBackend.leader_member_name
func (tb *TeamBackend) LeaderMemberName() string { return tb.leaderMemberName }

// DB 返回团队数据库实例。
// Python: TeamBackend.db
func (tb *TeamBackend) DB() database.TeamDatabase { return tb.db }

// TaskManager 返回任务管理器。
// Python: TeamBackend.task_manager
func (tb *TeamBackend) TaskManager() *TeamTaskManager { return tb.taskManager }

// MessageManager 返回消息管理器。
// Python: TeamBackend.message_manager
func (tb *TeamBackend) MessageManager() *TeamMessageManager { return tb.messageManager }

// GetMember 获取成员信息。
// Python: TeamBackend.get_member(member_name)
func (tb *TeamBackend) GetMember(ctx context.Context, memberName string) (*database.TeamMember, error) {
	return tb.db.Member().GetMember(ctx, memberName, tb.teamName)
}

// ListMembers 列出团队成员（排除自身）。
// Python: TeamBackend.list_members()
func (tb *TeamBackend) ListMembers(ctx context.Context) ([]*database.TeamMember, error) {
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
	if err != nil {
		return nil, err
	}
	// Python: 排除自身
	filtered := make([]*database.TeamMember, 0, len(members))
	for _, m := range members {
		if m.MemberName != tb.memberName {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// GetTeamInfo 获取团队信息。
// Python: TeamBackend.get_team_info()
func (tb *TeamBackend) GetTeamInfo(ctx context.Context) (*database.Team, error) {
	return tb.db.Team().GetTeam(ctx, tb.teamName)
}

// IsTeamCompleted 判断团队是否完成（所有任务终态 + 所有成员 settled + 无未读消息）。
// Python: TeamBackend.is_team_completed()
// 返回 TeamCompletionSnapshot 或 nil（未完成）。
func (tb *TeamBackend) IsTeamCompleted(ctx context.Context) (*atschema.TeamCompletionSnapshot, error) {
	// 步骤 1: 查团队
	team, err := tb.db.Team().GetTeam(ctx, tb.teamName)
	if err != nil || team == nil {
		return nil, nil
	}
	// 步骤 2: 查成员
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
	if err != nil {
		return nil, err
	}
	// 步骤 3: 查任务
	tasks, err := tb.db.Task().GetTeamTasks(ctx, tb.teamName, "")
	if err != nil {
		return nil, err
	}
	// 对齐 Python: if not tasks: return None — 无任务时无法判定完成
	if len(tasks) == 0 {
		return nil, nil
	}
	// 步骤 4: 判定 — 所有任务终态
	for _, t := range tasks {
		if !fsm.IsTaskTerminal(t.Status) {
			return nil, nil
		}
	}
	// 对齐 Python: if not members: return None — 无成员时无法判定完成
	if len(members) == 0 {
		return nil, nil
	}
	// 步骤 5: 判定 — 所有成员 settled
	for _, m := range members {
		if !atschema.MemberSettledStatuses[atschema.MemberStatus(m.Status)] {
			return nil, nil
		}
	}
	// 步骤 6: 判定 — 无未读消息
	if tb.messageManager.HasUnreadMessages(ctx, true) {
		return nil, nil
	}
	return &atschema.TeamCompletionSnapshot{
		MemberCount: len(members),
		TaskCount:   len(tasks),
	}, nil
}

// GetTeamUpdatedAt 获取团队 updated_at 时间戳。
// Python: TeamBackend.get_team_updated_at()
func (tb *TeamBackend) GetTeamUpdatedAt(ctx context.Context) int64 {
	return tb.db.Team().GetTeamUpdatedAt(ctx, tb.teamName)
}

// GetMembersMaxUpdatedAt 获取成员 MAX(updated_at)。
// Python: TeamBackend.get_members_max_updated_at()
func (tb *TeamBackend) GetMembersMaxUpdatedAt(ctx context.Context) int64 {
	return tb.db.Member().GetMembersMaxUpdatedAt(ctx, tb.teamName)
}

// GetTeamUpdatedAtState 探测团队 card 的 updated_at 及「字段是否存在」标志。
//
// 偏差 D-1：上游在 workspace_cache 存在时读 team_card.md 的 frontmatter，且
// present=false（字段空白——演化方改了正文却没戳时间戳）表示「必须重投一次」。
// Go 侧没有 md mtime 覆盖层，探针直读 DB 列，present 恒 true——行为等价于上游
// 「演化关闭」路径，即只有真的 build_team / 团队变更才会重投。
//
// Python: get_team_updated_at_state (team.py L1940-1969)
func (tb *TeamBackend) GetTeamUpdatedAtState(ctx context.Context) (int64, bool) {
	return tb.db.Team().GetTeamUpdatedAt(ctx, tb.teamName), true
}

// StampTeamCardUpdatedAt 把 ts 戳进 team_card.md 的 updated_at（仅 meta）。
//
// 偏差 D-1/D-4：Go 无 workspace_cache，该分支永不进入（present 恒 true），
// 故为空实现。保留符号是为了与上游 1:1 对照，避免 tracker 代码出现
// 「Go 少一个方法」的结构漂移（规则 9）。
//
// Python: stamp_team_card_updated_at (team.py L1971-1986)
func (tb *TeamBackend) StampTeamCardUpdatedAt(_ context.Context, _ int64) {}

// GetMemberUpdatedAtState 探测单成员 md 的 updated_at 及存在标志。
//
// 偏差 D-1/D-4 同 GetTeamUpdatedAtState：present 恒 true，只保留 mtime 比较
// 分支。成员行的 UpdatedAt 由 GetMember 一并返回，无需新增 DAO 查询；
// field 参数保留以对齐上游签名（"desc" / "prompt"），Go 侧两者同值。
//
// ⚠️ present 恒 true 会让上游的 `present=false` 分支不可达，而那个分支不是
// 可有可无的：它表示「frontmatter 的 updated_at 为空（演化方改了正文没戳）」，
// 上游在那里做「强制重投一次 + 回戳时间戳防循环」。跟踪侧要记住这个不变量
// （见 TeamContextTracker.identityBody 的注释），将来本方法能返回 false 时
// 必须同步恢复该分支，否则会每轮重投。
//
// Python: get_member_updated_at_state (team.py L2007-2024)
func (tb *TeamBackend) GetMemberUpdatedAtState(ctx context.Context, memberName, _ string) (int64, bool) {
	if memberName == "" {
		return 0, true
	}
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return 0, true
	}
	return member.UpdatedAt, true
}

// StampMemberPromptUpdatedAt 把 ts 戳进 member_prompt.md 的 updated_at（仅 meta）。
//
// 偏差 D-1/D-4：空实现，理由同 StampTeamCardUpdatedAt。
//
// Python: stamp_member_prompt_updated_at (team.py L2025-2038)
func (tb *TeamBackend) StampMemberPromptUpdatedAt(_ context.Context, _ string, _ int64) {}

// ForkEnabled 该团队的上下文继承（fork）是否开放。
//
// 偏差 D-8：上游直读 TeamAgentSpec.enable_fork，Go 的 TeamAgentSpec 无该字段
// （grep 零命中），恒 false。后果：身份块不带 fork 能力声明、不嵌 <identity>
// 元素——与上游 enable_fork=false 的字节表现一致。
//
// Python: fork_enabled (team.py L505-517)
func (tb *TeamBackend) ForkEnabled() bool { return false }

// SpawnMember 创建成员记录。
// Python: TeamBackend.spawn_member(member_name, display_name, agent_card, role, ...)
//
// Python 步骤：
//
//  1. 查已有成员 → 若已存在则 fail
//  2. 序列化 allocation → model_ref_json
//  3. DB 写入：db.member.create_member(...)
//  4. 若 DB 拒绝 → fail
//  5. HITT 缓存写透：若 role == HUMAN_AGENT，hittNames.add
//  6. 日志
//  7. 返回 MemberOpResult
func (tb *TeamBackend) SpawnMember(ctx context.Context, memberName, displayName string, agentCard *agentschema.AgentCard, role, desc, prompt, modelName string, opts ...SpawnMemberOption) atschema.MemberOpResult {
	// 解析可选参数（对齐 Python: spawn_member(status=UNSTARTED, execution_status=IDLE, mode=BUILD_MODE, allocation=None)）
	cfg := &spawnMemberConfig{
		status:          string(atschema.MemberStatusUnstarted),
		executionStatus: string(atschema.ExecutionStatusIdle),
		mode:            tb.teammateMode,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	// 步骤 1: 查已有成员
	existing, _ := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if existing != nil {
		logger.Warn(logComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("SpawnMember: member already exists")
		return atschema.NewMemberOpResultFail("成员 " + memberName + " 已存在于团队 " + tb.teamName)
	}
	// 步骤 2: 模型分配 + options 构造
	// Python: options = build_member_options(model_ref=allocation.to_db_ref() if ... else None, ...)
	allocation := cfg.allocation
	if allocation == nil && tb.modelConfigAllocator != nil {
		allocation = tb.modelConfigAllocator(modelName)
	}
	var modelRef map[string]any
	if allocation != nil {
		modelRef = allocation.ToDBRef()
	}
	var fallbackRef map[string]any
	if cfg.fallbackAllocation != nil {
		fallbackRef = cfg.fallbackAllocation.ToDBRef()
	}
	var cliAgentPtr *string
	if cfg.cliAgent != "" {
		cliAgent := cfg.cliAgent
		cliAgentPtr = &cliAgent
	}
	options := database.BuildMemberOptions(database.MemberOptionsInput{
		ModelRef:            modelRef,
		FallbackModelRef:    fallbackRef,
		BuiltinModel:        cfg.builtinModel,
		CLIAgent:            cliAgentPtr,
		WorktreeIsolation:   cfg.isolation,
		PermissionsOverride: cfg.permissionsOverride,
		Lifetime:            cfg.lifetime,
		AgentTemplate:       cfg.agentTemplate,
	})
	// 步骤 3: DB 写入（使用 cfg 中的状态值，对齐 Python: create_member(status=cfg.status, ...)）
	// Python: agent_card.model_dump_json() — 将 AgentCard 序列化为 JSON 存入 DB
	agentCardJSON := "{}"
	if agentCard != nil {
		if data, err := json.Marshal(agentCard); err == nil {
			agentCardJSON = string(data)
		}
	}
	ok := tb.db.Member().CreateMember(ctx, memberName, tb.teamName, displayName, agentCardJSON,
		cfg.status, role, desc,
		cfg.executionStatus, cfg.mode, prompt, options)
	if !ok {
		logger.Warn(logComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("SpawnMember: database refused creation")
		return atschema.NewMemberOpResultFail("数据库拒绝创建成员 " + memberName + " 在团队 " + tb.teamName)
	}
	// 步骤 4: HITT 缓存写透
	if role == string(atschema.TeamRoleHumanAgent) {
		tb.hittMu.Lock()
		tb.hittNames[memberName] = struct{}{}
		tb.hittMu.Unlock()
	}
	// 步骤 5: 日志
	logger.Info(logComponent).Str("member_name", memberName).Msg("SpawnMember: member created")
	return atschema.NewMemberOpResultSuccess()
}

// Startup 启动所有"有活要干"的 UNSTARTED 成员。
// Python: TeamBackend.startup(on_created=..., *, include_idle_task_scoped=False)
//
// 步骤：
//
//  1. 查询所有 UNSTARTED 成员
//  2. task-scoped 成员（options.lifetime 非空）在没有开放任务时跳过——
//     它只为任务而生，空板子上叫起来只会白跑一轮再被调度器退役
//     （lifetime="shutdown" 是永久退役），把 leader 后续交给它的任务搁死。
//     它仍可被指派；调度器的启动交接或指名消息（wake_member）会把它叫起来。
//  3. 逐个调用 startup_member(member_name, on_created)
//  4. 返回已启动的成员名列表
//
// 广播类路径传 includeIdleTaskScoped=true：广播也是发给它的，必须能读到。
//
// startup_member 失败时回滚 STARTING→UNSTARTED 并 re-raise，
// Python 的 startup() 会在异常时中断循环。Go 也对齐此行为。
func (tb *TeamBackend) Startup(
	ctx context.Context,
	onCreated func(ctx context.Context, memberName string) error,
	includeIdleTaskScoped bool,
) ([]string, error) {
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, string(atschema.MemberStatusUnstarted))
	if err != nil {
		return nil, err
	}
	var openCounts map[string]int
	var started []string
	for _, m := range members {
		if !includeIdleTaskScoped && database.GetMemberLifetime(m) != nil {
			if openCounts == nil {
				counts, countsErr := tb.taskManager.OpenTaskCountsByAssignee(ctx)
				if countsErr != nil {
					return started, countsErr
				}
				openCounts = counts
			}
			if openCounts[m.MemberName] == 0 {
				logger.Debug(logComponent).
					Str("member_name", m.MemberName).
					Msg("skip starting task-scoped member with no open task")
				continue
			}
		}
		ok, startErr := tb.StartupMember(ctx, m.MemberName, onCreated)
		if startErr != nil {
			return started, startErr
		}
		// Python: started.append(member.member_name) — 无条件 append
		_ = ok
		started = append(started, m.MemberName)
	}
	return started, nil
}

// AutostartUnstarted 用 backend 上注入的 spawn 回调启动所有 UNSTARTED 成员。
//
// 自动启动漏斗的唯一入口：即将交给成员的工作（一条消息、一个新任务）不能落到
// 一个进程从未启动过的成员身上。调用方只表达"确保名册都起来了"，不必各自
// 揣着 spawn 回调。
//
// 仅 leader 且回调已注入时才动作——teammate backend 或外部进程 backend
// 返回空列表，而不是去 spawn 一个它管不了的东西。并发由 startup_member 的
// UNSTARTED→STARTING CAS 在一层之下兜住，重复调用幂等。
//
// Python: TeamBackend.autostart_unstarted (tools/team.py:1034-1064)
func (tb *TeamBackend) AutostartUnstarted(ctx context.Context, includeIdleTaskScoped bool) ([]string, error) {
	if !tb.isLeader || tb.onMemberStarted == nil {
		return nil, nil
	}
	return tb.Startup(ctx, tb.onMemberStarted, includeIdleTaskScoped)
}

// StartupMember CAS 启动单个成员（UNSTARTED→STARTING）。
// Python: TeamBackend.startup_member(member_name, on_created=...)
//
// Python 步骤：
//
//  1. CAS: UNSTARTED→STARTING（若失败返回 false）
//  2. 调用 _spawn_and_publish(member_name, on_created)
//  3. 如果 _spawn_and_publish 失败 → 回滚 STARTING→UNSTARTED + re-raise
//  4. 返回 true
func (tb *TeamBackend) StartupMember(
	ctx context.Context,
	memberName string,
	onCreated func(ctx context.Context, memberName string) error,
) (bool, error) {
	// 步骤 1: CAS 转换
	transitioned := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		string(atschema.MemberStatusUnstarted), string(atschema.MemberStatusStarting))
	if !transitioned {
		return false, nil
	}

	// 步骤 2: 调用 spawnAndPublish
	err := tb.spawnAndPublish(ctx, memberName, onCreated)
	if err != nil {
		// 步骤 3: 失败回滚 STARTING→UNSTARTED
		tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
			string(atschema.MemberStatusStarting), string(atschema.MemberStatusUnstarted))
		return false, err
	}

	return true, nil
}

// ShutdownMember 关闭成员（FSM + 取消任务 + 事件）。
// Python: TeamBackend.shutdown_member(member_name)
//
// Python 步骤：
//
//  1. 查成员
//  2. 若不存在/已是终态 → fail
//  3. CAS 操作: current → SHUTDOWN_REQUESTED
//  4. 取消该成员的任务（skip self）
//  5. 发布 MemberShutdownEvent
//  6. 返回 MemberOpResult
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string, opts ...ShutdownOption) atschema.MemberOpResult {
	// 解析可选参数（对齐 Python: shutdown_member(force=False)）
	cfg := &shutdownConfig{force: false}
	for _, opt := range opts {
		opt(cfg)
	}
	// 步骤 1: 查成员
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return atschema.NewMemberOpResultFail("成员未找到: " + memberName)
	}
	// 步骤 2: 若已是终态（幂等返回 success，对齐 Python）
	if member.Status == string(atschema.MemberStatusShutdown) ||
		member.Status == string(atschema.MemberStatusShutdownRequested) {
		return atschema.NewMemberOpResultSuccess()
	}
	// 步骤 3: FSM 状态转换校验（对齐 Python: is_valid_transition(current_status, SHUTDOWN_REQUESTED, MEMBER_TRANSITIONS)）
	if !fsm.IsValidMemberTransition(member.Status, string(atschema.MemberStatusShutdownRequested)) {
		return atschema.NewMemberOpResultFail(
			fmt.Sprintf("成员 %s 无法从状态 '%s' 关闭", memberName, member.Status))
	}
	// 步骤 4: CAS 转换
	// 注：Python 使用非 CAS 的 update_member_status，Go 使用 TryTransitionMemberStatus（CAS）
	// 这是有意改进——CAS 更安全，防止并发场景下状态竞态条件
	ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		member.Status, string(atschema.MemberStatusShutdownRequested))
	if !ok {
		return atschema.NewMemberOpResultFail("Database rejected status update: " + memberName)
	}
	// 步骤 5: 发送 shutdown 消息（对齐 Python: message_manager.send_message）
	shutdownMsg, shutdownI18nErr := atschema.T("team.shutdown_request_content")
	if shutdownI18nErr != nil {
		logger.Warn(logComponent).Err(shutdownI18nErr).Msg("i18n key missing, using fallback value")
		shutdownMsg = "team.shutdown_request_content"
	}
	_, _ = tb.messageManager.SendMessage(ctx, shutdownMsg, memberName, tb.memberName)
	// 步骤 6: 发布事件（对齐 Python: MemberShutdownEvent(force=force)）
	tb.PublishEvent(ctx, events.MemberShutdownEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		Force:            cfg.force,
	})
	logger.Info(logComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Bool("force", cfg.force).
		Msg("ShutdownMember: shutdown requested")
	return atschema.NewMemberOpResultSuccess()
}

// CancelMember 取消成员执行（仅 BUSY 成员，重置 CLAIMED 任务 + 发送取消消息 + 事件）。
// Python: TeamBackend.cancel_member(member_name)
//
// Python 步骤：
//
//  1. 查成员
//  2. 若非 BUSY → 直接返回（不改变状态）
//  3. 重置该成员的 CLAIMED 任务（通过 task_manager.reset）
//  4. 发送取消消息
//  5. 发布 MemberCanceledEvent
//  6. 返回 MemberOpResult
func (tb *TeamBackend) CancelMember(ctx context.Context, memberName string) atschema.MemberOpResult {
	// 步骤 1: 查成员
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return atschema.NewMemberOpResultFail("成员未找到: " + memberName)
	}
	// 步骤 2: 仅对 BUSY 成员操作（对齐 Python: 非 BUSY 直接返回）
	if member.Status != string(atschema.MemberStatusBusy) {
		logger.Info(logComponent).Str("member_name", memberName).Str("status", member.Status).
			Msg("CancelMember: member not busy, no action needed")
		return atschema.NewMemberOpResultSuccess()
	}
	// 步骤 3: 重置该成员的 CLAIMED 任务（通过 taskManager.Reset，对齐 Python）
	tasks, _ := tb.taskManager.GetTasksByAssignee(ctx, memberName, string(atschema.TaskStatusClaimed))
	// Python: reset_count 统计 + 汇总日志
	resetCount := 0
	for _, t := range tasks {
		if result, _ := tb.taskManager.Reset(ctx, t.TaskID); !result.OK {
			logger.Warn(logComponent).Str("task_id", t.TaskID).Msg("CancelMember: failed to reset task")
		} else {
			resetCount++
		}
	}
	if resetCount > 0 {
		logger.Info(logComponent).Str("member_name", memberName).
			Int("reset_count", resetCount).Msg("CancelMember: member tasks reset")
	}
	// 步骤 4: 发送取消消息（对齐 Python: success = send_message; if not success → return False）
	cancelMsg, cancelI18nErr := atschema.T("team.cancel_request_content")
	if cancelI18nErr != nil {
		logger.Warn(logComponent).Err(cancelI18nErr).Msg("i18n key missing, using fallback value")
		cancelMsg = "team.cancel_request_content"
	}
	_, msgErr := tb.messageManager.SendMessage(ctx, cancelMsg, memberName, tb.memberName)
	if msgErr != nil {
		logger.Error(logComponent).Str("member_name", memberName).Err(msgErr).
			Msg("CancelMember: failed to send cancel message")
		return atschema.NewMemberOpResultFail("取消消息发送失败: " + memberName)
	}
	// 步骤 5: 发布事件
	tb.PublishEvent(ctx, events.MemberCanceledEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
	})
	logger.Info(logComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("CancelMember: member cancelled")
	return atschema.NewMemberOpResultSuccess()
}

// BuildTeam 创建团队 + 注册 leader + 预定义成员 + HITT。
//
// **对已存在的团队幂等**：团队行已经在时接管（见 reattachTeam）而不报错，
// 因为这也是把协作政策交给 leader 的唯一一次调用——而继承团队的 leader 至少
// 同样需要那份政策。
//
// Python 步骤：
//
//  0. 团队行已存在 → 接管（写零行、注册零人、不发 TeamCreated）
//     A. 强制 spec 天花板：enable_hitt=True 但 spec_enable_hitt=False → 报错
//     B. 计算有效 HITT 开关：enable_hitt=None 继承 spec，否则覆盖
//     B2. 计算生效的任务验证开关（spec 天花板 ∧ 本次覆盖）
//  1. 创建团队行
//  2. 注册 Leader（status=BUSY, execution=RUNNING, allocation=leader_allocation）
//  3. 注册预定义成员（跳过 HUMAN_AGENT，由后续 spawn_human_agent 处理）
//  4. HITT 处理：若 effective_enable_hitt，为每个 HUMAN_AGENT spec 调用 spawn_human_agent
//  5. 回调触发
//  6. 事件发布
//
// Python: build_team (team.py L2211-2340)
func (tb *TeamBackend) BuildTeam(ctx context.Context, displayName, desc,
	leaderDisplayName, leaderDesc string, enableHITT, enableTaskVerification *bool) (*BuildTeamOutcome, error) {
	// 步骤 0: 团队已存在 → 接管，不重建。
	//
	// 这个分支为 NEW_TEAM_IN_SESSION 而存在——一个新会话继承了一个已经在跑的
	// 团队。子 agent session 共享 team session id，所以新 session 意味着空历史：
	// 承载协作政策的 build_team 结果没了，而团队、名册、任务都还在。既然那次
	// 调用是政策唯一的交付点（F_76），在这里失败会让那个 leader 只剩 bootstrap。
	//
	// Python: team.py L2253-2258
	existing, err := tb.db.Team().GetTeam(ctx, tb.teamName)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return tb.reattachTeam(ctx, existing)
	}

	// 步骤 A: 强制 spec 天花板
	if enableHITT != nil && *enableHITT && !tb.specEnableHITT {
		logger.Error(logComponent).Bool("enable_hitt", *enableHITT).Bool("spec_enable_hitt", tb.specEnableHITT).
			Msg("BuildTeam: enable_hitt=True but spec_enable_hitt=False, cannot enable HITT")
		return nil, ErrHITTConfigInvalid
	}

	// 步骤 B: 计算有效 HITT 开关
	effectiveHITT := tb.specEnableHITT
	if enableHITT != nil {
		effectiveHITT = *enableHITT
	}
	tb.hittMu.Lock()
	tb.enableHITT = effectiveHITT
	tb.hittMu.Unlock()

	// 步骤 B2: 计算生效的任务验证开关。
	// spec 天花板与本次调用覆盖取与；enableTaskVerification 为 nil 时继承天花板。
	// Python: effective_task_verification = spec_enable_task_verification and
	//         (enable_task_verification if enable_task_verification is not None else True)
	effectiveTaskVerification := tb.specEnableTaskVerification
	if enableTaskVerification != nil {
		effectiveTaskVerification = tb.specEnableTaskVerification && *enableTaskVerification
	}
	tb.hittMu.Lock()
	tb.enableTaskVerification = effectiveTaskVerification
	tb.hittMu.Unlock()

	// 步骤 1: 创建团队行
	if !tb.db.Team().CreateTeam(ctx, tb.teamName, displayName, tb.leaderMemberName, desc, "", tb.dispatchMode, effectiveTaskVerification) {
		return nil, fmt.Errorf("创建团队 %s 失败", tb.teamName)
	}

	// 步骤 2: 注册 Leader（改走 SpawnMember 统一路径，对齐 Python: spawn_member(status=BUSY, execution_status=RUNNING, mode=BUILD_MODE)）
	// Python: leader_card = AgentCard(id=leader_card_id, name=leader_display_name, description=leader_desc)
	leaderCard := agentschema.NewAgentCard(
		agentschema.WithAgentID(tb.teamName+"_"+tb.memberName),
		agentschema.WithAgentName(leaderDisplayName),
		agentschema.WithAgentDescription(leaderDesc),
	)
	result := tb.SpawnMember(ctx, tb.memberName, leaderDisplayName, leaderCard, string(atschema.TeamRoleLeader),
		leaderDesc, "", "",
		WithStatus(string(atschema.MemberStatusBusy)),
		WithExecutionStatus(string(atschema.ExecutionStatusRunning)),
		WithMode(string(atschema.MemberModeBuildMode)),
		WithAllocation(tb.leaderAllocation),
	)
	if !result.OK {
		return nil, fmt.Errorf("注册 Leader 失败: %s", tb.memberName)
	}

	// 步骤 3: 注册预定义成员（跳过 HUMAN_AGENT）
	for _, pm := range tb.predefinedMembers {
		if pm.RoleType == atschema.TeamRoleHumanAgent {
			continue // 由后续 spawn_human_agent 处理
		}
		memberCardID := tb.teamName + "_" + pm.MemberName
		// Python: member_card = AgentCard(id=member_card_id, name=member_spec.display_name, description=member_spec.persona)
		memberCard := agentschema.NewAgentCard(
			agentschema.WithAgentID(memberCardID),
			agentschema.WithAgentName(pm.DisplayName),
			agentschema.WithAgentDescription(pm.Persona),
		)
		// Python: allocation = self._allocate_model_config(member_spec.model_name) if self._allocate_model_config else None
		var spawnOpts []SpawnMemberOption
		if tb.modelConfigAllocator != nil {
			spawnOpts = append(spawnOpts, WithAllocation(tb.modelConfigAllocator(pm.ModelName)))
		}
		tb.SpawnMember(ctx, pm.MemberName, pm.DisplayName, memberCard, string(pm.RoleType),
			pm.Persona, pm.PromptHint, pm.ModelName, spawnOpts...)
	}

	// 步骤 4: HITT 处理
	humanSpecs := make([]atschema.TeamMemberSpec, 0)
	for _, pm := range tb.predefinedMembers {
		if pm.RoleType == atschema.TeamRoleHumanAgent {
			humanSpecs = append(humanSpecs, pm)
		}
	}
	if effectiveHITT {
		for _, humanSpec := range humanSpecs {
			tb.SpawnHumanAgent(ctx, humanSpec.MemberName, humanSpec.DisplayName, "", humanSpec.PromptHint)
		}
	} else if len(humanSpecs) > 0 {
		logger.Warn(logComponent).Int("count", len(humanSpecs)).Str("team_name", tb.teamName).
			Msg("BuildTeam: skipping predefined HUMAN_AGENT members (HITT not enabled)")
	}

	// 步骤 5: 回调触发
	if tb.onTeamBuilt != nil {
		if err := tb.onTeamBuilt(ctx); err != nil {
			logger.Error(logComponent).Str("team_name", tb.teamName).Err(err).
				Msg("BuildTeam: onTeamBuilt callback failed")
		}
	}
	// 步骤 6: 事件发布
	tb.PublishEvent(ctx, events.TeamCreatedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tb.teamName},
		DisplayName:      displayName,
		LeaderMemberName: tb.leaderMemberName,
		Created:          database.GetCurrentTime(),
	})
	logger.Info(logComponent).Str("team_name", tb.teamName).Msg("BuildTeam: team created")
	return &BuildTeamOutcome{TakenOver: false, EnableTaskVerification: effectiveTaskVerification}, nil
}

// reattachTeam 接管一个已经存在的团队，而不是新建一个。
//
// leader 对一个行已在静态表里的团队调 build_team 时走到这里——实践上就是
// 一个新会话继承一个正在跑的团队（NEW_TEAM_IN_SESSION）。那时行、名册和
// 成员各自的配置都是既定事实——teammate 由 recover_team 按那份名册重新拉起，
// 不是由这里决定的东西——所以本路径不写任何行、也不重新注册任何人。
//
// **本次调用的能力参数不适用。** 团队在建队时就配置完了，它的成员已按那套
// 配置在跑，它的任务也可能已被它塑形（验证闸看板上的 reviewer）。所以生效的
// 验证开关从行里读回、而不是重新计算，leader 从工具结果得知它实际拿到了什么
// ——与它在新建路径上得知天花板被收窄是同一种方式。
//
// onTeamBuilt 照常触发：本 session 的 checkpoint 需要记下团队 DB 行存在，
// 否则后续某次运行会按一个与数据库不符的状态调度。不发 TeamCreated 事件
// ——什么都没创建，照它行动的成员会是在对一个它们已经在其中的团队的事件
// 做出反应。
//
// Python: _reattach_team (team.py L2164-2209)
func (tb *TeamBackend) reattachTeam(ctx context.Context, existing *database.Team) (*BuildTeamOutcome, error) {
	tb.hittMu.Lock()
	tb.enableTaskVerification = existing.EnableTaskVerification
	tb.hittMu.Unlock()

	logger.Info(logComponent).Str("team_name", tb.teamName).Bool("task_verification", existing.EnableTaskVerification).
		Msg("team already exists; taking it over")

	if tb.onTeamBuilt != nil {
		if err := tb.onTeamBuilt(ctx); err != nil {
			logger.Error(logComponent).Str("team_name", tb.teamName).Err(err).
				Msg("reattachTeam: onTeamBuilt callback failed")
		}
	}
	return &BuildTeamOutcome{TakenOver: true, EnableTaskVerification: existing.EnableTaskVerification}, nil
}

// CleanTeam 清理团队（全部 SHUTDOWN → 删 DB → 回调 → 清理路径 → 事件）。
// Python: TeamBackend.clean_team()
// 返回 true 表示成功清理，false 表示仍有活跃成员。
func (tb *TeamBackend) CleanTeam(ctx context.Context) (bool, error) {
	// 步骤 1: 查询活跃成员
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
	if err != nil {
		return false, err
	}
	for _, m := range members {
		// 跳过 leader 自身（对齐 Python: member_data.member_name == self.member_name → continue）
		if m.MemberName == tb.memberName {
			continue
		}
		// Python: 只允许 SHUTDOWN 状态
		if m.Status != string(atschema.MemberStatusShutdown) {
			logger.Error(logComponent).Str("team_name", tb.teamName).
				Str("active_member", m.MemberName).Str("status", m.Status).
				Msg("CleanTeam: active members still present, cannot clean")
			return false, nil
		}
	}
	// 步骤 2: 删除团队行
	tb.db.Team().DeleteTeam(ctx, tb.teamName)
	// 步骤 3: 删动态表
	_ = tb.db.DropCurSessionTables(ctx)
	// 步骤 4: 回调触发
	if tb.onTeamCleaned != nil {
		if err := tb.onTeamCleaned(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Msg("CleanTeam: onTeamCleaned callback failed")
		}
	}
	// 步骤 5: 清理路径
	if err := tb.RemoveCleanupPaths(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("CleanTeam: failed to remove cleanup paths")
	}
	// 步骤 6: 事件发布
	tb.PublishEvent(ctx, events.TeamCleanedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tb.teamName},
	})
	logger.Info(logComponent).Str("team_name", tb.teamName).Msg("CleanTeam: team cleaned")
	return true, nil
}

// ForceCleanTeam 强制清理团队（shutdown_all + force_delete + 清理路径）。
// Python: TeamBackend.force_clean_team(shutdown_members=force)
func (tb *TeamBackend) ForceCleanTeam(ctx context.Context, shutdownMembers bool) (bool, error) {
	// 步骤 1: 可选关闭所有成员（对齐 Python: 调用 shutdown_member(force=True)，跳过 self）
	if shutdownMembers {
		members, _ := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
		for _, m := range members {
			if m.MemberName == tb.memberName {
				continue // 跳过 leader 自身
			}
			// Python: 直接调用 shutdown_member(force=True)，不做前置状态检查
			result := tb.ShutdownMember(ctx, m.MemberName, WithForce(true))
			if !result.OK {
				logger.Warn(logComponent).Str("member_name", m.MemberName).
					Str("reason", result.Reason).
					Msg("ForceCleanTeam: failed to shutdown member, continuing")
			}
		}
	}
	// 步骤 2: 强制删除（对齐 Python: success = force_delete_team_session(...)）
	success := tb.db.ForceDeleteTeamSession(ctx, tb.teamName)
	// 步骤 3: 清理路径（对齐 Python: 清理路径失败设 success=false）
	if err := tb.RemoveCleanupPaths(ctx); err != nil {
		logger.Error(logComponent).Err(err).Str("team_name", tb.teamName).
			Msg("ForceCleanTeam: cleanup paths failed")
		success = false
	}
	if success {
		logger.Info(logComponent).Str("team_name", tb.teamName).Msg("ForceCleanTeam: team force-cleaned")
	}
	return success, nil
}

// CancelTask 取消任务 + 通知 assignee。
// Python: TeamBackend.cancel_task(task_id)
func (tb *TeamBackend) CancelTask(ctx context.Context, taskID string) atschema.MemberOpResult {
	// Python: task = await self.task_manager.get(task_id)
	// Python: if not task: return False
	task, _ := tb.taskManager.Get(ctx, taskID)
	if task == nil {
		return atschema.NewMemberOpResultFail("任务不存在: " + taskID)
	}
	// Python: if task.status == TaskStatus.CANCELLED.value: return True
	if task.Status == string(atschema.TaskStatusCancelled) {
		return atschema.NewMemberOpResultSuccess() // 幂等返回
	}

	_, err := tb.taskManager.Cancel(ctx, taskID)
	if err != nil {
		return atschema.NewMemberOpResultFail("取消任务失败: " + err.Error())
	}
	// 通知 assignee（如果有）— 复用上方已获取的 task
	if task != nil && task.Assignee != nil {
		// 发送取消消息通知（对齐 Python: message_manager.send_message）
		content := fmt.Sprintf("任务 '%s'（ID: %s）已被团队负责人取消。", task.Title, taskID)
		_, _ = tb.messageManager.SendMessage(ctx, content, *task.Assignee, tb.memberName)
	}
	logger.Info(logComponent).Str("task_id", taskID).Msg("CancelTask: task cancelled")
	return atschema.NewMemberOpResultSuccess()
}

// CancelAllTasks 批量取消 + 广播。
// Python: TeamBackend.cancel_all_tasks(skip_assignees) → int（取消的任务数量）
func (tb *TeamBackend) CancelAllTasks(ctx context.Context, skipAssignees []string) (int, error) {
	cancelled, err := tb.taskManager.CancelAllTasks(ctx, skipAssignees)
	if err != nil {
		return 0, fmt.Errorf("批量取消任务失败: %w", err)
	}
	cancelledCount := len(cancelled)
	// 广播取消消息（对齐 Python: message_manager.broadcast_message）
	if cancelledCount > 0 {
		content := fmt.Sprintf("所有任务（%d 个）已被团队负责人取消。", cancelledCount)
		_, _ = tb.messageManager.BroadcastMessage(ctx, content, tb.memberName)
	}
	logger.Info(logComponent).Str("team_name", tb.teamName).Int("cancelled_count", cancelledCount).Msg("CancelAllTasks: all tasks cancelled")
	return cancelledCount, nil
}

// ApprovePlan 审批计划。
// Python: TeamBackend.approve_plan(plan_id, approved=True, feedback=None)
func (tb *TeamBackend) ApprovePlan(ctx context.Context, planID string, opts ...ApprovePlanOption) atschema.MemberOpResult {
	// 解析可选参数（对齐 Python: approved=True, feedback=None）
	cfg := &approvePlanConfig{approved: true}
	for _, opt := range opts {
		opt(cfg)
	}
	// Python: 三层前置校验
	// 校验 1: planID 非空（对齐 Python: if not plan_id → return False）
	if planID == "" {
		logger.Error(logComponent).Msg("ApprovePlan: plan_id must not be empty")
		return atschema.NewMemberOpResultFail("approve_plan 需要 plan_id")
	}
	// 校验 2: plan record 存在（对齐 Python: plan_record = self.task_manager.get_plan_record(plan_id); if not plan_record → return False）
	planIndex, planErr := tb.taskManager.loadPlanIndex()
	if planErr != nil || planIndex == nil {
		logger.Error(logComponent).Str("plan_id", planID).Err(planErr).Msg("ApprovePlan: plan index not found")
		return atschema.NewMemberOpResultFail("计划索引未找到")
	}
	planRecord, planExists := planIndex.TaskPlans[planID]
	if !planExists || planRecord == nil {
		logger.Error(logComponent).Str("plan_id", planID).Msg("ApprovePlan: plan not found")
		return atschema.NewMemberOpResultFail("计划未找到: " + planID)
	}
	memberName := planRecord.MemberName
	taskID := planRecord.TaskID
	// 校验 3: member 存在（对齐 Python: member_data = get_member(member_name); if member_data is None → return False）
	if memberName == "" {
		logger.Error(logComponent).Str("plan_id", planID).Msg("ApprovePlan: plan missing member_name")
		return atschema.NewMemberOpResultFail("计划缺少 member_name: " + planID)
	}
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		logger.Error(logComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("ApprovePlan: member not in team")
		return atschema.NewMemberOpResultFail(fmt.Sprintf("成员 %s 不在团队 %s 中", memberName, tb.teamName))
	}
	// 执行审批
	result, _ := tb.taskManager.ApprovePlan(ctx, planID, cfg.approved, cfg.feedback)
	if !result.OK {
		return atschema.NewMemberOpResultFail("审批计划失败: " + result.Reason)
	}
	logger.Info(logComponent).Str("plan_id", planID).Str("task_id", taskID).Str("member_name", memberName).
		Bool("approved", cfg.approved).Msg("ApprovePlan: plan approved")
	return atschema.NewMemberOpResultSuccess()
}

// ApproveTool 审批工具调用。
// Python: TeamBackend.approve_tool(member_name, tool_call_id, approved, feedback, auto_confirm)
func (tb *TeamBackend) ApproveTool(ctx context.Context, memberName, toolCallID string, approved bool, feedback string, autoConfirm bool) atschema.MemberOpResult {
	// 成员存在性检查（对齐 Python: db.member.get_member(member_name)，不存在返回 False）
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return atschema.NewMemberOpResultFail("成员未找到: " + memberName)
	}
	tb.PublishEvent(ctx, events.ToolApprovalResultEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		ToolCallID:       toolCallID,
		Approved:         approved,
		Feedback:         feedback,
		AutoConfirm:      autoConfirm,
	})
	logger.Info(logComponent).Str("member_name", memberName).Str("tool_call_id", toolCallID).
		Bool("approved", approved).Msg("ApproveTool: tool approval result")
	return atschema.NewMemberOpResultSuccess()
}

// SpawnHumanAgent 注册 human-agent 成员。
// Python: TeamBackend.spawn_human_agent(member_name, display_name, desc, prompt)
func (tb *TeamBackend) SpawnHumanAgent(ctx context.Context, memberName, displayName, desc, prompt string) atschema.MemberOpResult {
	if !tb.HITTEnabled() {
		return atschema.NewMemberOpResultFail("hitt_not_enabled")
	}
	// i18n 默认值（对齐 Python: display_name = t("hitt.human_agent_display_name"), desc = t("hitt.human_agent_default_persona")）
	if displayName == "" {
		if val, err := atschema.T("hitt.human_agent_display_name"); err == nil {
			displayName = val
		} else {
			logger.Warn(logComponent).Err(err).Msg("i18n key missing, using fallback value")
			displayName = "hitt.human_agent_display_name"
		}
	}
	if desc == "" {
		if val, err := atschema.T("hitt.human_agent_default_persona"); err == nil {
			desc = val
		} else {
			logger.Warn(logComponent).Err(err).Msg("i18n key missing, using fallback value")
			desc = "hitt.human_agent_default_persona"
		}
	}
	// Python: member_card = AgentCard(id=f"{self.team_name}_{member_name}", name=resolved_display_name, description=resolved_desc)
	memberCard := agentschema.NewAgentCard(
		agentschema.WithAgentID(tb.teamName+"_"+memberName),
		agentschema.WithAgentName(displayName),
		agentschema.WithAgentDescription(desc),
	)
	result := tb.SpawnMember(ctx, memberName, displayName, memberCard, string(atschema.TeamRoleHumanAgent), desc, prompt, "")
	if !result.OK {
		logger.Warn(logComponent).Str("member_name", memberName).Str("reason", result.Reason).
			Msg("SpawnHumanAgent: failed to register human-agent")
		return result
	}
	logger.Info(logComponent).Str("member_name", memberName).Msg("SpawnHumanAgent: human-agent created")
	return atschema.NewMemberOpResultSuccess()
}

// RefreshHumanAgentRoster 从 DB 重建 HITT 名册缓存。
// Python: TeamBackend.refresh_human_agent_roster()
func (tb *TeamBackend) RefreshHumanAgentRoster(ctx context.Context) {
	// 步骤 0: 对齐 Python — 先初始化 DB（预热 DAO），确保冷恢复路径中 DAO 已就绪
	if err := tb.db.Initialize(ctx); err != nil {
		logger.Debug(logComponent).Err(err).Msg("RefreshHumanAgentRoster: DB init failed")
	}
	// 步骤 1: 查询 human_agent 成员名
	names, err := tb.db.Member().ListHumanAgentNames(ctx, tb.teamName)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("RefreshHumanAgentRoster: query failed")
		return
	}
	tb.hittMu.Lock()
	tb.hittNames = make(map[string]struct{}, len(names))
	for _, n := range names {
		tb.hittNames[n] = struct{}{}
	}
	tb.hittMu.Unlock()
	logger.Info(logComponent).Int("count", len(names)).Msg("RefreshHumanAgentRoster: roster rebuilt")
}

// IsHumanAgent 判断是否 human-agent（读缓存）。
// Python: TeamBackend.is_human_agent(member_name)
func (tb *TeamBackend) IsHumanAgent(memberName string) bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	_, ok := tb.hittNames[memberName]
	return ok
}

// RegisterHumanAgentInbound 注册/清除 inbound 回调。
// Python: TeamBackend.register_human_agent_inbound(member_name, callback)
// callback 为 nil 时清除。
func (tb *TeamBackend) RegisterHumanAgentInbound(ctx context.Context, memberName string, callback OnInbound) error {
	tb.hittMu.Lock()
	defer tb.hittMu.Unlock()
	if callback == nil {
		delete(tb.hittInboundCallbacks, memberName)
		return nil
	}
	// 校验：member_name 必须在 hittNames 中
	if _, ok := tb.hittNames[memberName]; !ok {
		return &atschema.UnknownHumanAgentError{Sender: memberName}
	}
	tb.hittInboundCallbacks[memberName] = callback
	return nil
}

// GetHumanAgentInbound 获取 inbound 回调。
// Python: TeamBackend.get_human_agent_inbound(member_name)
func (tb *TeamBackend) GetHumanAgentInbound(memberName string) OnInbound {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.hittInboundCallbacks[memberName]
}

// HumanAgentNames 返回 HITT 名册快照。
// Python: TeamBackend.human_agent_names()
func (tb *TeamBackend) HumanAgentNames() []string {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	names := make([]string, 0, len(tb.hittNames))
	for n := range tb.hittNames {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// HITTEnabled 返回 HITT 能力开关。
// Python: TeamBackend.hitt_enabled
func (tb *TeamBackend) HITTEnabled() bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.enableHITT
}

// MemberMessagingEnabled scheduled 成员可否互发消息（F_126）。
//
// 直接来自 TeamAgentSpec.enable_member_messaging，只在 scheduled 下有意义：
// autonomous 成员本来就能互相触达，所以消费方会结合自己所属的调度模式一起用。
// 这是成员 send_message 变体与 scheduled dispatch 提示词中消息散文的同一个
// 门控信号。
//
// Python: member_messaging_enabled (team.py L516-526)
func (tb *TeamBackend) MemberMessagingEnabled() bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.enableMemberMessaging
}

// MemberSubtasksEnabled scheduled 成员可否把任务拆成子任务（F_128）。
//
// 成员 create_task、scheduled dispatch 提示词的子任务散文、leader 政策披露
// 三处的同一个门控信号。只在 scheduled 下有意义。
//
// Python: member_subtasks_enabled (team.py L536-543)
func (tb *TeamBackend) MemberSubtasksEnabled() bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.maxSubtaskDepth > 0
}

// EnableTaskVerification 返回生效的任务验证开关。
// Python: TeamBackend._enable_task_verification
func (tb *TeamBackend) EnableTaskVerification() bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.enableTaskVerification
}

// RegisterCleanupPath 注册清理路径（去重）。
// Python: TeamBackend.register_cleanup_path(path)
func (tb *TeamBackend) RegisterCleanupPath(path string) {
	if path == "" {
		return
	}
	expanded := filepath.Clean(os.ExpandEnv(path))
	// Python: Path.expanduser() — 展开 ~ 为 $HOME
	if strings.HasPrefix(expanded, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, expanded[2:])
		}
	}
	tb.cleanupPaths[expanded] = struct{}{}
}

// RemoveCleanupPaths 串行删除清理路径（按深度排序，失败不中止）。
// Python: TeamBackend._remove_cleanup_paths()
func (tb *TeamBackend) RemoveCleanupPaths(ctx context.Context) error {
	if len(tb.cleanupPaths) == 0 {
		return nil
	}
	var firstErr error
	// 按深度排序（最深先删）
	ordered := make([]string, 0, len(tb.cleanupPaths))
	for p := range tb.cleanupPaths {
		ordered = append(ordered, p)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return len(strings.Split(ordered[i], string(filepath.Separator))) >
			len(strings.Split(ordered[j], string(filepath.Separator)))
	})
	for _, p := range ordered {
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			logger.Error(logComponent).Str("path", p).Err(err).Msg("RemoveCleanupPaths: failed to delete")
			if firstErr == nil {
				firstErr = err
			}
		} else {
			logger.Info(logComponent).Str("path", p).Msg("RemoveCleanupPaths: deleted")
		}
	}
	tb.cleanupPaths = make(map[string]struct{})
	return firstErr
}

// PublishEvent 发布团队事件。
// Python: TeamBackend 中通过 messager.publish 调用
func (tb *TeamBackend) PublishEvent(ctx context.Context, event events.TypedEvent) {
	if tb.messager == nil {
		return
	}
	topicID := events.TeamTopicTeam.Build(atschema.GetSessionID(ctx), tb.teamName)
	msg := events.EventMessageFromEvent(event)
	if err := tb.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logComponent).Str("event_type", event.EventTypeName()).Err(err).
			Msg("PublishEvent: failed to publish event")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// spawnAndPublish 启动成员 agent 并发布 MemberSpawnedEvent。
// Python: _spawn_and_publish(member_name, on_created)
//
// Python 步骤：
//
//  1. await on_created(member_name) — 调用回调启动 agent 进程
//  2. try: messager.publish(MemberSpawnedEvent) — 事件发布失败仅记日志不抛异常
//  3. 日志：Member {member_name} started
func (tb *TeamBackend) spawnAndPublish(
	ctx context.Context,
	memberName string,
	onCreated func(ctx context.Context, memberName string) error,
) error {
	// 步骤 1: 调用 onCreated 回调（启动 agent 进程）
	if onCreated != nil {
		if err := onCreated(ctx, memberName); err != nil {
			return err
		}
	}

	// 步骤 2: 发布 MemberSpawnedEvent（失败只记日志不抛异常）
	tb.PublishEvent(ctx, events.MemberSpawnedEvent{
		BaseEventMessage: events.BaseEventMessage{
			TeamName:   tb.teamName,
			MemberName: memberName,
		},
	})
	logger.Debug(logComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("spawnAndPublish: MemberSpawnedEvent published")

	// 步骤 3: 日志
	logger.Info(logComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("spawnAndPublish: member started")
	return nil
}
