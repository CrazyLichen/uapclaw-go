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
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OnInbound 团队→用户通知回调。
// 对齐 Python: OnInbound = Callable[[HumanAgentInboundEvent], Awaitable[None]]
// 复用 interaction 包的 OnInbound 类型签名，但 tools 包不 import interaction（避免循环），
// 因此在此独立定义。
type OnInbound func(ctx context.Context, memberName string, payload any) error

// TeamBackend 团队后端门面，组合 DB + TaskManager + MessageManager + Messager。
// 对齐 Python: TeamBackend (openjiuwen/agent_teams/tools/team.py)
//
// 提供团队级业务方法：成员生命周期、团队生命周期、HITT 名册、
// 文件清理、任务操作、跨域组合操作。
// HITT 缓存由独立 hittMu 保护，DB 操作靠 DAO 层并发安全。
type TeamBackend struct {
	// ── 必填字段 ──
	teamName         string
	memberName       string
	isLeader         bool
	leaderMemberName string
	db               database.TeamDatabase
	messager         messager.Messager
	taskManager      *TeamTaskManager
	messageManager   *TeamMessageManager

	// ── 可选字段（Functional Options 注入）──
	teammateMode         string
	predefinedMembers    []atschema.TeamMemberSpec
	modelConfigAllocator func(modelName string) *models.Allocation
	leaderAllocation     *models.Allocation
	onTeamCleaned        func(ctx context.Context) error
	onTeamBuilt          func(ctx context.Context) error
	planStorageDir       string
	planID               string

	// ── HITT 缓存（hittMu 保护）──
	hittMu               sync.RWMutex
	specEnableHITT       bool
	enableHITT           bool
	hittNames            map[string]struct{}
	hittInboundCallbacks map[string]OnInbound

	// ── 文件系统清理路径 ──
	cleanupPaths map[string]struct{}
}

// TeamBackendOption TeamBackend 构造可选参数。
type TeamBackendOption func(*TeamBackend)

// ShutdownOption ShutdownMember 可选参数。
// 对齐 Python: shutdown_member(force=False)
type ShutdownOption func(*shutdownConfig)

// shutdownConfig ShutdownMember 配置
type shutdownConfig struct {
	// force 是否强制关闭（对齐 Python: force=False）
	force bool
}

// ApprovePlanOption ApprovePlan 可选参数。
// 对齐 Python: approve_plan(approved=True, feedback=None)
type ApprovePlanOption func(*approvePlanConfig)

// approvePlanConfig ApprovePlan 配置
type approvePlanConfig struct {
	// approved 是否批准（对齐 Python: approved=True）
	approved bool
	// feedback 反馈意见（对齐 Python: feedback=None）
	feedback string
}

// SpawnMemberOption SpawnMember 可选参数。
// 对齐 Python: spawn_member(status=UNSTARTED, execution_status=IDLE, mode=BUILD_MODE, allocation=None)
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
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// tbLogComponent TeamBackend 日志组件
	tbLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrHITTConfigInvalid build_team(enable_hitt=True) 但 spec_enable_hitt=False 时返回。
	// 对齐 Python: raise_error(StatusCode.AGENT_TEAM_CONFIG_INVALID, ...)
	ErrHITTConfigInvalid = errors.New("build_team(enable_hitt=True) 要求 spec_enable_hitt=True；能力天花板被违反")
)

// ──────────────────────────── 导出函数 ────────────────────────────

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

// WithOnTeamCleaned 设置团队清理回调。
// 对齐 Python: on_team_cleaned 参数
func WithOnTeamCleaned(fn func(ctx context.Context) error) TeamBackendOption {
	return func(tb *TeamBackend) { tb.onTeamCleaned = fn }
}

// WithOnTeamBuilt 设置团队构建回调。
// 对齐 Python: on_team_built 参数
func WithOnTeamBuilt(fn func(ctx context.Context) error) TeamBackendOption {
	return func(tb *TeamBackend) { tb.onTeamBuilt = fn }
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

// NewTeamBackend 创建团队后端门面。
// 对齐 Python: TeamBackend.__init__(team_name, member_name, is_leader, db, messager, ...)
func NewTeamBackend(
	teamName, memberName string, isLeader bool,
	db database.TeamDatabase, msg messager.Messager,
	opts ...TeamBackendOption,
) *TeamBackend {
	tb := &TeamBackend{
		teamName:             teamName,
		memberName:           memberName,
		isLeader:             isLeader,
		leaderMemberName:     memberName, // 默认值，WithLeaderMemberName 可覆盖
		db:                   db,
		messager:             msg,
		teammateMode:         string(atschema.MemberModeBuildMode),
		hittNames:            make(map[string]struct{}),
		hittInboundCallbacks: make(map[string]OnInbound),
		cleanupPaths:         make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(tb)
	}
	// 内部构造 TaskManager 和 MessageManager
	tb.taskManager = NewTeamTaskManager(
		db, teamName, memberName, msg,
		tb.planStorageDir, tb.planID, tb.leaderMemberName,
	)
	tb.messageManager = NewTeamMessageManager(db, teamName, memberName, msg)

	logger.Info(tbLogComponent).Str("team_name", teamName).Str("member_name", memberName).
		Msg("TeamBackend 初始化完成")

	return tb
}

// ── 属性访问 ──

// TeamName 返回团队名。
// 对齐 Python: TeamBackend.team_name
func (tb *TeamBackend) TeamName() string { return tb.teamName }

// MemberName 返回当前成员名。
// 对齐 Python: TeamBackend.member_name
func (tb *TeamBackend) MemberName() string { return tb.memberName }

// IsLeader 返回是否 Leader。
// 对齐 Python: TeamBackend.is_leader
func (tb *TeamBackend) IsLeader() bool { return tb.isLeader }

// LeaderMemberName 返回 Leader 成员名。
// 对齐 Python: TeamBackend.leader_member_name
func (tb *TeamBackend) LeaderMemberName() string { return tb.leaderMemberName }

// DB 返回团队数据库实例。
// 对齐 Python: TeamBackend.db
func (tb *TeamBackend) DB() database.TeamDatabase { return tb.db }

// TaskManager 返回任务管理器。
// 对齐 Python: TeamBackend.task_manager
func (tb *TeamBackend) TaskManager() *TeamTaskManager { return tb.taskManager }

// MessageManager 返回消息管理器。
// 对齐 Python: TeamBackend.message_manager
func (tb *TeamBackend) MessageManager() *TeamMessageManager { return tb.messageManager }

// ── 查询方法 ──

// GetMember 获取成员信息。
// 对齐 Python: TeamBackend.get_member(member_name)
func (tb *TeamBackend) GetMember(ctx context.Context, memberName string) (*database.TeamMember, error) {
	return tb.db.Member().GetMember(ctx, memberName, tb.teamName)
}

// ListMembers 列出团队成员（排除自身）。
// 对齐 Python: TeamBackend.list_members()
func (tb *TeamBackend) ListMembers(ctx context.Context) ([]*database.TeamMember, error) {
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
	if err != nil {
		return nil, err
	}
	// 对齐 Python: 排除自身
	filtered := make([]*database.TeamMember, 0, len(members))
	for _, m := range members {
		if m.MemberName != tb.memberName {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// GetTeamInfo 获取团队信息。
// 对齐 Python: TeamBackend.get_team_info()
func (tb *TeamBackend) GetTeamInfo(ctx context.Context) (*database.Team, error) {
	return tb.db.Team().GetTeam(ctx, tb.teamName)
}

// IsTeamCompleted 判断团队是否完成（所有任务终态 + 所有成员 settled + 无未读消息）。
// 对齐 Python: TeamBackend.is_team_completed()
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
	// 步骤 4: 判定 — 所有任务终态
	for _, t := range tasks {
		if !fsm.IsTaskTerminal(t.Status) {
			return nil, nil
		}
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
// 对齐 Python: TeamBackend.get_team_updated_at()
func (tb *TeamBackend) GetTeamUpdatedAt(ctx context.Context) int64 {
	return tb.db.Team().GetTeamUpdatedAt(ctx, tb.teamName)
}

// GetMembersMaxUpdatedAt 获取成员 MAX(updated_at)。
// 对齐 Python: TeamBackend.get_members_max_updated_at()
func (tb *TeamBackend) GetMembersMaxUpdatedAt(ctx context.Context) int64 {
	return tb.db.Member().GetMembersMaxUpdatedAt(ctx, tb.teamName)
}

// ── 成员生命周期 ──

// SpawnMember 创建成员记录。
// 对齐 Python: TeamBackend.spawn_member(member_name, display_name, agent_card, role, ...)
//
// Python 步骤：
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
		logger.Warn(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("SpawnMember: 成员已存在")
		return atschema.NewMemberOpResultFail("成员 " + memberName + " 已存在于团队 " + tb.teamName)
	}
	// 步骤 2: 模型分配（优先使用 opts 中的 allocation，否则使用 modelConfigAllocator）
	modelRefJSON := ""
	if cfg.allocation != nil {
		refMap := map[string]any{"model_name": cfg.allocation.Entry.ModelName, "model_index": cfg.allocation.GroupIndex}
		if data, err := json.Marshal(refMap); err == nil {
			modelRefJSON = string(data)
		}
	} else if tb.modelConfigAllocator != nil {
		if alloc := tb.modelConfigAllocator(modelName); alloc != nil {
			refMap := map[string]any{"model_name": alloc.Entry.ModelName, "model_index": alloc.GroupIndex}
			if data, err := json.Marshal(refMap); err == nil {
				modelRefJSON = string(data)
			}
		}
	}
	// 步骤 3: DB 写入（使用 cfg 中的状态值，对齐 Python: create_member(status=cfg.status, ...)）
	// 对齐 Python: agent_card.model_dump_json() — 将 AgentCard 序列化为 JSON 存入 DB
	agentCardJSON := "{}"
	if agentCard != nil {
		if data, err := json.Marshal(agentCard); err == nil {
			agentCardJSON = string(data)
		}
	}
	ok := tb.db.Member().CreateMember(ctx, memberName, tb.teamName, displayName, agentCardJSON,
		cfg.status, role, desc,
		cfg.executionStatus, cfg.mode, prompt, modelRefJSON)
	if !ok {
		logger.Warn(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("SpawnMember: DB 拒绝创建")
		return atschema.NewMemberOpResultFail("database rejected create_member for " + memberName + " in team " + tb.teamName)
	}
	// 步骤 4: HITT 缓存写透
	if role == string(atschema.TeamRoleHumanAgent) {
		tb.hittMu.Lock()
		tb.hittNames[memberName] = struct{}{}
		tb.hittMu.Unlock()
	}
	// 步骤 5: 日志
	logger.Info(tbLogComponent).Str("member_name", memberName).Msg("SpawnMember: 成员已创建")
	return atschema.NewMemberOpResultSuccess()
}

// Startup 启动所有 UNSTARTED 成员。
// 对齐 Python: TeamBackend.startup(on_created=...)
//
// Python 步骤：
//  1. 查询所有 UNSTARTED 成员
//  2. 逐个调用 startup_member(member_name, on_created)
//  3. 返回已启动的成员名列表
//
// startup_member 失败时回滚 STARTING→UNSTARTED 并 re-raise，
// Python 的 startup() 会在异常时中断循环。Go 也对齐此行为。
func (tb *TeamBackend) Startup(
	ctx context.Context,
	onCreated func(ctx context.Context, memberName string) error,
) ([]string, error) {
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, string(atschema.MemberStatusUnstarted))
	if err != nil {
		return nil, err
	}
	var started []string
	for _, m := range members {
		ok, err := tb.StartupMember(ctx, m.MemberName, onCreated)
		if err != nil {
			return started, err
		}
		if ok {
			started = append(started, m.MemberName)
		}
	}
	return started, nil
}

// StartupMember CAS 启动单个成员（UNSTARTED→STARTING）。
// 对齐 Python: TeamBackend.startup_member(member_name, on_created=...)
//
// Python 步骤：
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
// 对齐 Python: TeamBackend.shutdown_member(member_name)
//
// Python 步骤：
//  1. 查成员
//  2. 若不存在/已是终态 → fail
//  3. CAS: current → SHUTDOWN_REQUESTED
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
	ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		member.Status, string(atschema.MemberStatusShutdownRequested))
	if !ok {
		return atschema.NewMemberOpResultFail("CAS 状态转换失败: " + memberName)
	}
	// 步骤 4: 发送 shutdown 消息（对齐 Python: message_manager.send_message）
	shutdownMsg, shutdownI18nErr := atschema.T("team.shutdown_request_content")
	if shutdownI18nErr != nil {
		logger.Warn(tbLogComponent).Err(shutdownI18nErr).Msg("i18n 键缺失，使用回退值")
		shutdownMsg = "team.shutdown_request_content"
	}
	_, _ = tb.messageManager.SendMessage(ctx, shutdownMsg, memberName, tb.memberName)
	// 步骤 5: 发布事件（对齐 Python: MemberShutdownEvent(force=force)）
	tb.publishEvent(ctx, atschema.MemberShutdownEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		Force:            cfg.force,
	})
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Bool("force", cfg.force).
		Msg("ShutdownMember: 成员已请求关闭")
	return atschema.NewMemberOpResultSuccess()
}

// CancelMember 取消成员执行（仅 BUSY 成员，重置 CLAIMED 任务 + 发送取消消息 + 事件）。
// 对齐 Python: TeamBackend.cancel_member(member_name)
//
// Python 步骤：
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
		logger.Info(tbLogComponent).Str("member_name", memberName).Str("status", member.Status).
			Msg("CancelMember: 成员非忙碌状态，无需取消")
		return atschema.NewMemberOpResultSuccess()
	}
	// 步骤 3: 重置该成员的 CLAIMED 任务（通过 taskManager.Reset，对齐 Python）
	tasks, _ := tb.taskManager.GetTasksByAssignee(ctx, memberName, string(atschema.TaskStatusClaimed))
	// 对齐 Python: reset_count 统计 + 汇总日志
	resetCount := 0
	for _, t := range tasks {
		if err := tb.taskManager.Reset(ctx, t.TaskID); err != nil {
			logger.Warn(tbLogComponent).Str("task_id", t.TaskID).Err(err).
				Msg("CancelMember: reset task failed")
		} else {
			resetCount++
		}
	}
	if resetCount > 0 {
		logger.Info(tbLogComponent).Str("member_name", memberName).
			Int("reset_count", resetCount).Msg("CancelMember: reset tasks from member")
	}
	// 步骤 4: 发送取消消息（对齐 Python: success = send_message; if not success → return False）
	cancelMsg, cancelI18nErr := atschema.T("team.cancel_request_content")
	if cancelI18nErr != nil {
		logger.Warn(tbLogComponent).Err(cancelI18nErr).Msg("i18n 键缺失，使用回退值")
		cancelMsg = "team.cancel_request_content"
	}
	_, msgErr := tb.messageManager.SendMessage(ctx, cancelMsg, memberName, tb.memberName)
	if msgErr != nil {
		logger.Error(tbLogComponent).Str("member_name", memberName).Err(msgErr).
			Msg("CancelMember: 发送取消消息失败")
		return atschema.NewMemberOpResultFail("取消消息发送失败: " + memberName)
	}
	// 步骤 5: 发布事件
	tb.publishEvent(ctx, atschema.MemberCanceledEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
	})
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("CancelMember: 成员已取消")
	return atschema.NewMemberOpResultSuccess()
}

// ── 团队生命周期 ──

// BuildTeam 创建团队 + 注册 leader + 预定义成员 + HITT。
// 对齐 Python: TeamBackend.build_team(display_name, desc, leader_display_name, leader_desc, enable_hitt)
//
// Python 步骤：
//
//	A. 强制 spec 天花板：enable_hitt=True 但 spec_enable_hitt=False → 报错
//	B. 计算有效 HITT 开关：enable_hitt=None 继承 spec，否则覆盖
//	1. 创建团队行
//	2. 注册 Leader（status=BUSY, execution=RUNNING, allocation=leader_allocation）
//	3. 注册预定义成员（跳过 HUMAN_AGENT，由后续 spawn_human_agent 处理）
//	4. HITT 处理：若 effective_enable_hitt，为每个 HUMAN_AGENT spec 调用 spawn_human_agent
//	5. 回调触发
//	6. 事件发布
func (tb *TeamBackend) BuildTeam(ctx context.Context, displayName, desc, leaderDisplayName, leaderDesc string, enableHITT *bool) error {
	// 步骤 A: 强制 spec 天花板
	if enableHITT != nil && *enableHITT && !tb.specEnableHITT {
		logger.Error(tbLogComponent).Bool("enable_hitt", *enableHITT).Bool("spec_enable_hitt", tb.specEnableHITT).
			Msg("BuildTeam: enable_hitt=True 但 spec_enable_hitt=False，无法启用 HITT")
		return ErrHITTConfigInvalid
	}

	// 步骤 B: 计算有效 HITT 开关
	effectiveHITT := tb.specEnableHITT
	if enableHITT != nil {
		effectiveHITT = *enableHITT
	}
	tb.hittMu.Lock()
	tb.enableHITT = effectiveHITT
	tb.hittMu.Unlock()

	// 步骤 1: 创建团队行
	if !tb.db.Team().CreateTeam(ctx, tb.teamName, displayName, tb.leaderMemberName, desc, "") {
		return fmt.Errorf("创建团队 %s 失败", tb.teamName)
	}

	// 步骤 2: 注册 Leader（改走 SpawnMember 统一路径，对齐 Python: spawn_member(status=BUSY, execution_status=RUNNING, mode=BUILD_MODE)）
	// 对齐 Python: leader_card = AgentCard(id=leader_card_id, name=leader_display_name, description=leader_desc)
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
		return fmt.Errorf("注册 Leader 失败: %s", tb.memberName)
	}

	// 步骤 3: 注册预定义成员（跳过 HUMAN_AGENT）
	for _, pm := range tb.predefinedMembers {
		if pm.RoleType == atschema.TeamRoleHumanAgent {
			continue // 由后续 spawn_human_agent 处理
		}
		memberCardID := tb.teamName + "_" + pm.MemberName
		// 对齐 Python: member_card = AgentCard(id=member_card_id, name=member_spec.display_name, description=member_spec.persona)
		memberCard := agentschema.NewAgentCard(
			agentschema.WithAgentID(memberCardID),
			agentschema.WithAgentName(pm.DisplayName),
			agentschema.WithAgentDescription(pm.Persona),
		)
		// 对齐 Python: allocation = self._allocate_model_config(member_spec.model_name) if self._allocate_model_config else None
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
		logger.Warn(tbLogComponent).Int("count", len(humanSpecs)).Str("team_name", tb.teamName).
			Msg("BuildTeam: 跳过预定义 HUMAN_AGENT 成员（HITT 未启用）")
	}

	// 步骤 5: 回调触发
	if tb.onTeamBuilt != nil {
		if err := tb.onTeamBuilt(ctx); err != nil {
			logger.Error(tbLogComponent).Str("team_name", tb.teamName).Err(err).
				Msg("BuildTeam: onTeamBuilt 回调失败")
		}
	}
	// 步骤 6: 事件发布
	tb.publishEvent(ctx, atschema.TeamCreatedEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName},
		DisplayName:      displayName,
		LeaderMemberName: tb.leaderMemberName,
		Created:          database.GetCurrentTime(),
	})
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("BuildTeam: 团队已创建")
	return nil
}

// CleanTeam 清理团队（全部 SHUTDOWN → 删 DB → 回调 → 清理路径 → 事件）。
// 对齐 Python: TeamBackend.clean_team()
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
		// 对齐 Python: 只允许 SHUTDOWN 状态
		if m.Status != string(atschema.MemberStatusShutdown) {
			logger.Warn(tbLogComponent).Str("team_name", tb.teamName).
				Str("active_member", m.MemberName).Str("status", m.Status).
				Msg("CleanTeam: 仍有活跃成员，无法清理")
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
			logger.Warn(tbLogComponent).Err(err).Msg("CleanTeam: onTeamCleaned 回调失败")
		}
	}
	// 步骤 5: 清理路径
	if err := tb.RemoveCleanupPaths(ctx); err != nil {
		logger.Warn(tbLogComponent).Err(err).Msg("CleanTeam: 移除清理路径失败")
	}
	// 步骤 6: 事件发布
	tb.publishEvent(ctx, atschema.TeamCleanedEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName},
	})
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("CleanTeam: 团队已清理")
	return true, nil
}

// ForceCleanTeam 强制清理团队（shutdown_all + force_delete + 清理路径）。
// 对齐 Python: TeamBackend.force_clean_team(shutdown_members=force)
func (tb *TeamBackend) ForceCleanTeam(ctx context.Context, shutdownMembers bool) (bool, error) {
	// 步骤 1: 可选关闭所有成员（对齐 Python: 调用 shutdown_member(force=True)，跳过 self）
	if shutdownMembers {
		members, _ := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
		for _, m := range members {
			if m.MemberName == tb.memberName {
				continue // 跳过 leader 自身
			}
			// 对齐 Python: 直接调用 shutdown_member(force=True)，不做前置状态检查
			result := tb.ShutdownMember(ctx, m.MemberName, WithForce(true))
			if !result.OK {
				logger.Warn(tbLogComponent).Str("member_name", m.MemberName).
					Str("reason", result.Reason).
					Msg("ForceCleanTeam: 关闭成员失败，继续执行")
			}
		}
	}
	// 步骤 2: 强制删除（对齐 Python: success = force_delete_team_session(...)）
	success := tb.db.ForceDeleteTeamSession(ctx, tb.teamName)
	// 步骤 3: 清理路径（对齐 Python: 清理路径失败设 success=false）
	if err := tb.RemoveCleanupPaths(ctx); err != nil {
		logger.Error(tbLogComponent).Err(err).Str("team_name", tb.teamName).
			Msg("ForceCleanTeam: 清理路径失败")
		success = false
	}
	if success {
		logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("ForceCleanTeam: 团队已强制清理")
	}
	return success, nil
}

// ── 任务操作 ──

// CancelTask 取消任务 + 通知 assignee。
// 对齐 Python: TeamBackend.cancel_task(task_id)
func (tb *TeamBackend) CancelTask(ctx context.Context, taskID string) atschema.MemberOpResult {
	unblocked, err := tb.taskManager.Cancel(ctx, taskID)
	if err != nil {
		return atschema.NewMemberOpResultFail("cancel_task failed: " + err.Error())
	}
	// 通知 assignee（如果有）
	task, _ := tb.taskManager.Get(ctx, taskID)
	if task != nil && task.Assignee != "" {
		// 发送取消消息通知（对齐 Python: message_manager.send_message）
		content := fmt.Sprintf("Task '%s' (ID: %s) has been cancelled by the team leader.", task.Title, taskID)
		_, _ = tb.messageManager.SendMessage(ctx, content, task.Assignee, tb.memberName)
		// 发布取消事件
		tb.publishEvent(ctx, atschema.TaskCancelledEvent{
			BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: task.Assignee},
			TaskID:           taskID,
		})
	}
	// 通知 unblocked 任务
	for _, uid := range unblocked {
		tb.publishEvent(ctx, atschema.TaskUnblockedEvent{
			BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName},
			TaskID:           uid,
		})
	}
	logger.Info(tbLogComponent).Str("task_id", taskID).Msg("CancelTask: 任务已取消")
	return atschema.NewMemberOpResultSuccess()
}

// CancelAllTasks 批量取消 + 广播。
// 对齐 Python: TeamBackend.cancel_all_tasks(skip_assignees)
func (tb *TeamBackend) CancelAllTasks(ctx context.Context, skipAssignees []string) atschema.MemberOpResult {
	cancelled, err := tb.taskManager.CancelAllTasks(ctx, skipAssignees)
	if err != nil {
		return atschema.NewMemberOpResultFail("cancel_all_tasks failed: " + err.Error())
	}
	// 广播取消消息（对齐 Python: message_manager.broadcast_message）
	if len(cancelled) > 0 {
		content := fmt.Sprintf("All tasks (%d) have been cancelled by team leader.", len(cancelled))
		_, _ = tb.messageManager.BroadcastMessage(ctx, content, tb.memberName)
	}
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("CancelAllTasks: 所有任务已取消")
	return atschema.NewMemberOpResultSuccess()
}

// ApprovePlan 审批计划。
// 对齐 Python: TeamBackend.approve_plan(task_id)
// ApprovePlan 审批计划。
// 对齐 Python: TeamBackend.approve_plan(plan_id, approved=True, feedback=None)
func (tb *TeamBackend) ApprovePlan(ctx context.Context, planID string, opts ...ApprovePlanOption) atschema.MemberOpResult {
	// 解析可选参数（对齐 Python: approved=True, feedback=None）
	cfg := &approvePlanConfig{approved: true}
	for _, opt := range opts {
		opt(cfg)
	}
	// 对齐 Python: 三层前置校验
	// 校验 1: planID 非空（对齐 Python: if not plan_id → return False）
	if planID == "" {
		logger.Error(tbLogComponent).Msg("ApprovePlan: plan_id 不能为空")
		return atschema.NewMemberOpResultFail("approve_plan 需要 plan_id")
	}
	// 校验 2: plan record 存在（对齐 Python: plan_record = self.task_manager.get_plan_record(plan_id); if not plan_record → return False）
	planIndex, planErr := tb.taskManager.loadPlanIndex()
	if planErr != nil || planIndex == nil {
		logger.Error(tbLogComponent).Str("plan_id", planID).Err(planErr).Msg("ApprovePlan: 计划索引未找到")
		return atschema.NewMemberOpResultFail("计划索引未找到")
	}
	planRecord, planExists := planIndex.TaskPlans[planID]
	if !planExists || planRecord == nil {
		logger.Error(tbLogComponent).Str("plan_id", planID).Msg("ApprovePlan: 计划未找到")
		return atschema.NewMemberOpResultFail("计划未找到: " + planID)
	}
	memberName := planRecord.MemberName
	taskID := planRecord.TaskID
	// 校验 3: member 存在（对齐 Python: member_data = get_member(member_name); if member_data is None → return False）
	if memberName == "" {
		logger.Error(tbLogComponent).Str("plan_id", planID).Msg("ApprovePlan: 计划缺少 member_name")
		return atschema.NewMemberOpResultFail("计划缺少 member_name: " + planID)
	}
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		logger.Error(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("ApprovePlan: 成员不在团队中")
		return atschema.NewMemberOpResultFail(fmt.Sprintf("成员 %s 不在团队 %s 中", memberName, tb.teamName))
	}
	// 执行审批
	err = tb.taskManager.ApprovePlan(ctx, planID, cfg.approved, cfg.feedback)
	if err != nil {
		return atschema.NewMemberOpResultFail("approve_plan failed: " + err.Error())
	}
	tb.publishEvent(ctx, atschema.TaskPlanResponseEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		TaskID:           taskID,
		Approved:         cfg.approved,
		Status:           string(atschema.TaskStatusPlanApproved),
	})
	logger.Info(tbLogComponent).Str("plan_id", planID).Str("task_id", taskID).Str("member_name", memberName).
		Bool("approved", cfg.approved).Msg("ApprovePlan: 计划已审批")
	return atschema.NewMemberOpResultSuccess()
}

// ApproveTool 审批工具调用。
// 对齐 Python: TeamBackend.approve_tool(member_name, tool_call_id, approved, feedback, auto_confirm)
func (tb *TeamBackend) ApproveTool(ctx context.Context, memberName, toolCallID string, approved bool, feedback string, autoConfirm bool) atschema.MemberOpResult {
	// 成员存在性检查（对齐 Python: db.member.get_member(member_name)，不存在返回 False）
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return atschema.NewMemberOpResultFail("成员未找到: " + memberName)
	}
	tb.publishEvent(ctx, atschema.ToolApprovalResultEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		ToolCallID:       toolCallID,
		Approved:         approved,
		Feedback:         feedback,
		AutoConfirm:      autoConfirm,
	})
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("tool_call_id", toolCallID).
		Bool("approved", approved).Msg("ApproveTool: 工具调用审批结果")
	return atschema.NewMemberOpResultSuccess()
}

// ── HITT 管理 ──

// SpawnHumanAgent 注册 human-agent 成员。
// 对齐 Python: TeamBackend.spawn_human_agent(member_name, display_name, desc, prompt)
func (tb *TeamBackend) SpawnHumanAgent(ctx context.Context, memberName, displayName, desc, prompt string) atschema.MemberOpResult {
	if !tb.HITTEnabled() {
		return atschema.NewMemberOpResultFail("hitt_not_enabled")
	}
	// i18n 默认值（对齐 Python: display_name = t("hitt.human_agent_display_name"), desc = t("hitt.human_agent_default_persona")）
	if displayName == "" {
		if val, err := atschema.T("hitt.human_agent_display_name"); err == nil {
			displayName = val
		} else {
			logger.Warn(tbLogComponent).Err(err).Msg("i18n 键缺失，使用回退值")
			displayName = "hitt.human_agent_display_name"
		}
	}
	if desc == "" {
		if val, err := atschema.T("hitt.human_agent_default_persona"); err == nil {
			desc = val
		} else {
			logger.Warn(tbLogComponent).Err(err).Msg("i18n 键缺失，使用回退值")
			desc = "hitt.human_agent_default_persona"
		}
	}
	// 对齐 Python: member_card = AgentCard(id=f"{self.team_name}_{member_name}", name=resolved_display_name, description=resolved_desc)
	memberCard := agentschema.NewAgentCard(
		agentschema.WithAgentID(tb.teamName+"_"+memberName),
		agentschema.WithAgentName(displayName),
		agentschema.WithAgentDescription(desc),
	)
	result := tb.SpawnMember(ctx, memberName, displayName, memberCard, string(atschema.TeamRoleHumanAgent), desc, prompt, "")
	if !result.OK {
		return result
	}
	logger.Info(tbLogComponent).Str("member_name", memberName).Msg("SpawnHumanAgent: human-agent 已创建")
	return atschema.NewMemberOpResultSuccess()
}

// RefreshHumanAgentRoster 从 DB 重建 HITT 名册缓存。
// 对齐 Python: TeamBackend.refresh_human_agent_roster()
func (tb *TeamBackend) RefreshHumanAgentRoster(ctx context.Context) {
	// 步骤 0: 对齐 Python — 先初始化 DB（预热 DAO），确保冷恢复路径中 DAO 已就绪
	if err := tb.db.Initialize(ctx); err != nil {
		logger.Debug(tbLogComponent).Err(err).Msg("RefreshHumanAgentRoster: DB 初始化失败")
	}
	// 步骤 1: 查询 human_agent 成员名
	names, err := tb.db.Member().ListHumanAgentNames(ctx, tb.teamName)
	if err != nil {
		logger.Error(tbLogComponent).Err(err).Msg("RefreshHumanAgentRoster: 查询失败")
		return
	}
	tb.hittMu.Lock()
	tb.hittNames = make(map[string]struct{}, len(names))
	for _, n := range names {
		tb.hittNames[n] = struct{}{}
	}
	tb.hittMu.Unlock()
	logger.Info(tbLogComponent).Int("count", len(names)).Msg("RefreshHumanAgentRoster: 名册已重建")
}

// IsHumanAgent 判断是否 human-agent（读缓存）。
// 对齐 Python: TeamBackend.is_human_agent(member_name)
func (tb *TeamBackend) IsHumanAgent(memberName string) bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	_, ok := tb.hittNames[memberName]
	return ok
}

// RegisterHumanAgentInbound 注册/清除 inbound 回调。
// 对齐 Python: TeamBackend.register_human_agent_inbound(member_name, callback)
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
// 对齐 Python: TeamBackend.get_human_agent_inbound(member_name)
func (tb *TeamBackend) GetHumanAgentInbound(memberName string) OnInbound {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.hittInboundCallbacks[memberName]
}

// HumanAgentNames 返回 HITT 名册快照。
// 对齐 Python: TeamBackend.human_agent_names()
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
// 对齐 Python: TeamBackend.hitt_enabled
func (tb *TeamBackend) HITTEnabled() bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.enableHITT
}

// ── 文件清理 ──

// RegisterCleanupPath 注册清理路径（去重）。
// 对齐 Python: TeamBackend.register_cleanup_path(path)
func (tb *TeamBackend) RegisterCleanupPath(path string) {
	if path == "" {
		return
	}
	expanded := filepath.Clean(os.ExpandEnv(path))
	// 对齐 Python: Path.expanduser() — 展开 ~ 为 $HOME
	if strings.HasPrefix(expanded, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, expanded[2:])
		}
	}
	tb.cleanupPaths[expanded] = struct{}{}
}

// RemoveCleanupPaths 串行删除清理路径（按深度排序，失败不中止）。
// 对齐 Python: TeamBackend._remove_cleanup_paths()
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
			logger.Error(tbLogComponent).Str("path", p).Err(err).Msg("RemoveCleanupPaths: 删除失败")
			if firstErr == nil {
				firstErr = err
			}
		} else {
			logger.Info(tbLogComponent).Str("path", p).Msg("RemoveCleanupPaths: 已删除")
		}
	}
	tb.cleanupPaths = make(map[string]struct{})
	return firstErr
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// publishEvent 发布团队事件。
// 对齐 Python: TeamBackend 中通过 messager.publish 调用
func (tb *TeamBackend) publishEvent(ctx context.Context, event atschema.TypedEvent) {
	if tb.messager == nil {
		return
	}
	topicID := atschema.TeamTopicTeam.Build(atschema.GetSessionID(ctx), tb.teamName)
	msg := atschema.EventMessageFromEvent(event)
	if err := tb.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(tbLogComponent).Str("event_type", event.EventTypeName()).Err(err).
			Msg("publishEvent: 发布事件失败")
	}
}

// spawnAndPublish 启动成员 agent 并发布 MemberSpawnedEvent。
// 对齐 Python: _spawn_and_publish(member_name, on_created)
//
// Python 步骤：
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
	tb.publishEvent(ctx, atschema.MemberSpawnedEvent{
		BaseEventMessage: atschema.BaseEventMessage{
			TeamName:   tb.teamName,
			MemberName: memberName,
		},
	})
	logger.Debug(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("spawnAndPublish: MemberSpawnedEvent 已发布")

	// 步骤 3: 日志
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("spawnAndPublish: 成员已启动")
	return nil
}
