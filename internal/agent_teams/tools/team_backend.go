package tools

import (
	"context"
	"encoding/json"
	"errors"
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
func (tb *TeamBackend) SpawnMember(ctx context.Context, memberName, displayName, agentCard, role, desc, prompt, modelName string) atschema.MemberOpResult {
	// 步骤 1: 查已有成员
	existing, _ := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if existing != nil {
		logger.Warn(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("SpawnMember: 成员已存在")
		return atschema.NewMemberOpResultFail("member " + memberName + " already exists in team " + tb.teamName)
	}
	// 步骤 2: 模型分配
	modelRefJSON := ""
	if tb.modelConfigAllocator != nil {
		if alloc := tb.modelConfigAllocator(modelName); alloc != nil {
			refMap := map[string]any{"model_name": alloc.Entry.ModelName, "model_index": alloc.GroupIndex}
			if data, err := json.Marshal(refMap); err == nil {
				modelRefJSON = string(data)
			}
		}
	}
	// 步骤 3: DB 写入
	ok := tb.db.Member().CreateMember(ctx, memberName, tb.teamName, displayName, agentCard,
		string(atschema.MemberStatusUnstarted), role, desc,
		string(atschema.ExecutionStatusIdle), tb.teammateMode, prompt, modelRefJSON)
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
// 返回已启动的成员名列表。
func (tb *TeamBackend) Startup(ctx context.Context) ([]string, error) {
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, string(atschema.MemberStatusUnstarted))
	if err != nil {
		return nil, err
	}
	var started []string
	for _, m := range members {
		if m.MemberName == tb.memberName {
			continue // 跳过自身
		}
		ok := tb.db.Member().TryTransitionMemberStatus(ctx, m.MemberName, tb.teamName,
			string(atschema.MemberStatusUnstarted), string(atschema.MemberStatusStarting))
		if ok {
			started = append(started, m.MemberName)
			logger.Info(tbLogComponent).Str("member_name", m.MemberName).Str("team_name", tb.teamName).
				Msg("Startup: 成员已启动")
		}
	}
	return started, nil
}

// StartupMember CAS 启动单个成员（UNSTARTED→STARTING）。
// 对齐 Python: TeamBackend.startup_member(member_name, on_created=...)
func (tb *TeamBackend) StartupMember(ctx context.Context, memberName string) (bool, error) {
	ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		string(atschema.MemberStatusUnstarted), string(atschema.MemberStatusStarting))
	if ok {
		logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("StartupMember: 成员已启动")
	}
	return ok, nil
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
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) atschema.MemberOpResult {
	// 步骤 1: 查成员
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return atschema.NewMemberOpResultFail("member not found: " + memberName)
	}
	// 步骤 2: 若已是终态
	if member.Status == string(atschema.MemberStatusShutdown) ||
		member.Status == string(atschema.MemberStatusError) {
		return atschema.NewMemberOpResultFail("member already in terminal state: " + memberName)
	}
	// 步骤 3: CAS 转换
	ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		member.Status, string(atschema.MemberStatusShutdownRequested))
	if !ok {
		return atschema.NewMemberOpResultFail("CAS transition failed for: " + memberName)
	}
	// 步骤 4: 取消该成员的任务
	_, _ = tb.taskManager.CancelAllTasks(ctx, []string{memberName})
	// 步骤 5: 发布事件
	tb.publishEvent(ctx, atschema.MemberShutdownEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		Force:            false,
	})
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("ShutdownMember: 成员已请求关闭")
	return atschema.NewMemberOpResultSuccess()
}

// CancelMember 取消成员执行（重置 CLAIMED 任务 + 事件）。
// 对齐 Python: TeamBackend.cancel_member(member_name)
//
// Python 步骤：
//  1. 查成员
//  2. 若不存在/已是终态 → fail
//  3. CAS: current → SHUTDOWN_REQUESTED
//  4. 重置该成员的 CLAIMED 任务
//  5. 发布 MemberCanceledEvent
//  6. 返回 MemberOpResult
func (tb *TeamBackend) CancelMember(ctx context.Context, memberName string) atschema.MemberOpResult {
	// 步骤 1: 查成员
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return atschema.NewMemberOpResultFail("member not found: " + memberName)
	}
	// 步骤 2: 若已是终态
	if member.Status == string(atschema.MemberStatusShutdown) ||
		member.Status == string(atschema.MemberStatusError) {
		return atschema.NewMemberOpResultFail("member already in terminal state: " + memberName)
	}
	// 步骤 3: CAS 转换
	ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		member.Status, string(atschema.MemberStatusShutdownRequested))
	if !ok {
		return atschema.NewMemberOpResultFail("CAS transition failed for: " + memberName)
	}
	// 步骤 4: 重置该成员的 CLAIMED 任务
	tasks, _ := tb.db.Task().GetTasksByAssignee(ctx, tb.teamName, memberName, string(atschema.TaskStatusClaimed))
	for _, t := range tasks {
		tb.db.Task().ResetTask(ctx, t.TaskID) //nolint:errcheck // 清理路径，忽略错误
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
	tb.db.Team().CreateTeam(ctx, tb.teamName, displayName, tb.leaderMemberName, desc, "")

	// 步骤 2: 注册 Leader（status=BUSY, execution=RUNNING）
	leaderModelRefJSON := ""
	if tb.leaderAllocation != nil {
		refMap := map[string]any{"model_name": tb.leaderAllocation.Entry.ModelName, "model_index": tb.leaderAllocation.GroupIndex}
		if data, err := json.Marshal(refMap); err == nil {
			leaderModelRefJSON = string(data)
		}
	}
	tb.db.Member().CreateMember(ctx, tb.memberName, tb.teamName, leaderDisplayName, "",
		string(atschema.MemberStatusBusy), string(atschema.TeamRoleLeader), leaderDesc,
		string(atschema.ExecutionStatusRunning), string(atschema.MemberModeBuildMode), "", leaderModelRefJSON)

	// 步骤 3: 注册预定义成员（跳过 HUMAN_AGENT）
	for _, pm := range tb.predefinedMembers {
		if pm.RoleType == atschema.TeamRoleHumanAgent {
			continue // 由后续 spawn_human_agent 处理
		}
		memberCardID := tb.teamName + "_" + pm.MemberName
		agentCard := memberCardID // 对齐 Python: AgentCard(id=member_card_id, name=display_name, description=persona)
		_ = agentCard
		tb.SpawnMember(ctx, pm.MemberName, pm.DisplayName, memberCardID, string(pm.RoleType),
			pm.Persona, pm.PromptHint, pm.ModelName)
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
		if m.Status != string(atschema.MemberStatusShutdown) &&
			m.Status != string(atschema.MemberStatusError) {
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
	tb.RemoveCleanupPaths(ctx)
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
	// 步骤 1: 可选关闭所有成员
	if shutdownMembers {
		members, _ := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
		for _, m := range members {
			if m.Status != string(atschema.MemberStatusShutdown) {
				tb.db.Member().UpdateMemberStatus(ctx, m.MemberName, tb.teamName, string(atschema.MemberStatusShutdown))
			}
		}
	}
	// 步骤 2: 强制删除
	tb.db.ForceDeleteTeamSession(ctx, tb.teamName)
	// 步骤 3: 回调触发
	if tb.onTeamCleaned != nil {
		if err := tb.onTeamCleaned(ctx); err != nil {
			logger.Warn(tbLogComponent).Err(err).Msg("ForceCleanTeam: onTeamCleaned 回调失败")
		}
	}
	// 步骤 4: 清理路径
	tb.RemoveCleanupPaths(ctx)
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("ForceCleanTeam: 团队已强制清理")
	return true, nil
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
	_, err := tb.taskManager.CancelAllTasks(ctx, skipAssignees)
	if err != nil {
		return atschema.NewMemberOpResultFail("cancel_all_tasks failed: " + err.Error())
	}
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("CancelAllTasks: 所有任务已取消")
	return atschema.NewMemberOpResultSuccess()
}

// ApprovePlan 审批计划。
// 对齐 Python: TeamBackend.approve_plan(task_id)
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) atschema.MemberOpResult {
	err := tb.taskManager.ApprovePlan(ctx, taskID, true, "")
	if err != nil {
		return atschema.NewMemberOpResultFail("approve_plan failed: " + err.Error())
	}
	task, _ := tb.taskManager.Get(ctx, taskID)
	memberName := ""
	if task != nil {
		memberName = task.Assignee
	}
	tb.publishEvent(ctx, atschema.TaskPlanResponseEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		TaskID:           taskID,
		Approved:         true,
		Status:           string(atschema.TaskStatusPlanApproved),
	})
	logger.Info(tbLogComponent).Str("task_id", taskID).Msg("ApprovePlan: 计划已审批")
	return atschema.NewMemberOpResultSuccess()
}

// ApproveTool 审批工具调用。
// 对齐 Python: TeamBackend.approve_tool(member_name, tool_call_id, approved, feedback, auto_confirm)
func (tb *TeamBackend) ApproveTool(ctx context.Context, memberName, toolCallID string, approved bool, feedback string, autoConfirm bool) atschema.MemberOpResult {
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
	result := tb.SpawnMember(ctx, memberName, displayName, "", string(atschema.TeamRoleHumanAgent), desc, prompt, "")
	if !result.OK {
		return result
	}
	logger.Info(tbLogComponent).Str("member_name", memberName).Msg("SpawnHumanAgent: human-agent 已创建")
	return atschema.NewMemberOpResultSuccess()
}

// RefreshHumanAgentRoster 从 DB 重建 HITT 名册缓存。
// 对齐 Python: TeamBackend.refresh_human_agent_roster()
func (tb *TeamBackend) RefreshHumanAgentRoster(ctx context.Context) {
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
	tb.cleanupPaths[expanded] = struct{}{}
}

// RemoveCleanupPaths 串行删除清理路径（按深度排序，失败不中止）。
// 对齐 Python: TeamBackend._remove_cleanup_paths()
func (tb *TeamBackend) RemoveCleanupPaths(ctx context.Context) {
	if len(tb.cleanupPaths) == 0 {
		return
	}
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
		} else {
			logger.Info(tbLogComponent).Str("path", p).Msg("RemoveCleanupPaths: 已删除")
		}
	}
	tb.cleanupPaths = make(map[string]struct{})
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
