// Package agent 提供生产级团队 Agent（TeamAgent）实现。
// TeamAgent 是整个多 Agent 协作系统的核心编排节点，既可充当 Leader
// （分发任务、协调成员），也可充当 Teammate（执行具体任务）。
// 采用组合式架构：内部包裹 DeepAgent 实例（而非继承），
// 委托给专职 Manager 管理配置/生成/恢复/会话/流式/协调。
// 四象限分解：
//   - 静态蓝图（TeamAgentBlueprint）：构造时确定、不可变
//   - 可变状态（TeamAgentState）：运行时可变值，跨 Manager 共享
//   - 进程级基础设施（TeamInfra）：每进程一份
//   - 实例级资源（PrivateAgentResources）：每实例一份
//
// 文件目录：
//
//	agent/
//	├── doc.go                # 包文档
//	├── team_agent.go         # TeamAgent 主体（9.55）
//	├── state.go              # TeamAgentState 可变状态（9.55）
//	├── member.go             # TeamMember 成员状态管理（9.55）
//	├── member_factory.go     # create_member_handle 工厂（9.55）
//	├── blueprint.go          # TeamAgentBlueprint 不可变蓝图（9.55）
//	├── infra.go              # TeamInfra 进程级基础设施（9.55）
//	├── resources.go          # PrivateAgentResources 实例级资源（9.55）
//	├── agent_configurator.go # AgentConfigurator Agent 配置器（9.57）
//	├── payload.go            # SpawnPayloadBuilder 跨进程载荷构造器（9.57）
//	├── spawn_manager.go      # SpawnManager 子进程管理（9.58）
//	├── session_manager.go    # SessionManager 会话三态管理（9.59）
//	├── stream_controller.go  # StreamController 流式控制器（9.60）
//	├── recovery_manager.go   # 恢复管理器（团队恢复、会话切换、配置持久化）
//	└── coordination/         # ✅(#9.62-9.63) 协调子系统
//	    ├── kernel.go         # ✅(#9.62) 协调内核
//	    ├── event_bus.go      # ✅(#9.63) 事件总线
//	    ├── dispatcher.go     # ✅(#9.63) 事件分发器
//	    ├── types/            # ✅(#9.63) 共享类型（打破循环依赖）
//	    └── handlers/         # ✅(#9.63) 事件处理器
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/
package agent

import (
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"

	"strings"

	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	spawnpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	harness "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/harness"
	ateaminteraction "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability"
	reliabilitycore "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	atevents "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tracker"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentcoreharness "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/spawn"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	sessinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamAgent 生产级团队 Agent。
// 组合式架构：包裹内部 DeepAgent 实例，委托给专职 Manager。
// Python: TeamAgent (openjiuwen/agent_teams/agent/team_agent.py)
//
// 可充当 Leader 或 Teammate：
//   - Leader：分发任务、协调成员、管理团队生命周期
//   - Teammate：执行具体任务、与 Leader/其他成员协作
//
// 内层 DeepAgent 不由本结构体直接持有：对齐 Python `TeamAgent._harness`
// （team_agent.py:120-124「不要绕过 harness 直接找 DeepAgent」），
// 统一通过 configurator / TeamHarness 访问。
type TeamAgent struct {
	// card Agent 身份卡片
	card *agentschema.AgentCard
	// configurator Agent 配置器
	configurator *AgentConfigurator
	// state 可变运行时状态
	state *TeamAgentState
	// spawnManager 子进程管理器
	spawnManager *SpawnManager
	// recoveryManager 恢复管理器
	recoveryManager *RecoveryManager
	// sessionManager 会话管理器
	sessionManager *SessionManager
	// streamController 流式控制器
	streamController *StreamController
	// coordination 协调内核
	coordination *coordination.CoordinationKernel
	// taskEventTracker 任务通知器（非 leader 才有，⤴️ 25.0c）
	//
	// 由 TeamAgent 持有、TeamPolicyRail 借用——基线因此超出 rail 的生命周期，
	// 与运行时的「事件驱动宣告」共享同一份（Python 同构：两个 rail 各自持有一份
	// tracker 会把同一次编辑播两遍）。
	taskEventTracker tracker.TaskEventTracker

	// ── 25.5 fork 半区：命名 checkpoint 索引 ──
	// namedCheckpointsMu 保护 namedCheckpoints（Python 单线程共享状态，
	// asyncio→Go 并发移植按惯例加锁）。
	namedCheckpointsMu sync.RWMutex
	// namedCheckpoints 命名 checkpoint 索引（权威源，名字团队全局唯一）。
	// 值只存下标不存消息内容。
	// Python: self._named_checkpoints（team_agent.py:185）
	namedCheckpoints map[string]metadata.CheckpointRecord
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期检查：确保具体类型直接满足窄接口（无需适配器）
var (
	_ types.SessionController   = (*SessionManager)(nil)
	_ types.TeamBackendAccessor = (*tools.TeamBackend)(nil)
	_ types.HarnessAccessor     = (*harness.TeamHarness)(nil)
	_ types.KernelHost          = (*TeamAgent)(nil)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TeamMemberExecutionSubject 构建一次 Team 成员执行的轨迹归属主体。
//
// 每个以成员名执行的主体（长驻成员、swarmflow worker 与头像）共用它，
// 从而落进同一条轨迹泳道。一次性 reviewer 用 TeamReviewerExecutionSubject。
//
// Python: team_member_execution_subject (team_agent.py:80-101)
// 偏差：Python 关键字参数无默认值处 Go 以显式形参表达；is_leader 缺省
// Python =false，Go 以布尔形参由调用方传入（无可选参数机制）。
// 暂缺：TeamReviewerExecutionSubject（team_agent.py:113-168）随其唯一消费方
// scheduling/scheduler.py（Go 侧未移植）⤵️ 一并回填，不丢失。
func TeamMemberExecutionSubject(sessionID, teamName, memberName, displayName string, isLeader bool) agentcoreharness.ExecutionSubject {
	kind := "team_member"
	if isLeader {
		kind = "team_leader"
	}
	return agentcoreharness.ExecutionSubject{
		SubjectID:       "team-member:" + sessionID + ":" + teamName + ":" + memberName,
		DisplayName:     displayName,
		Kind:            kind,
		ParentSubjectID: "",
		SessionID:       sessionID,
	}
}

// ObservabilityExecutionSubject 返回本 Team 成员的稳定轨迹归属主体。
// Python: TeamAgent.observability_execution_subject (team_agent.py:889-899)
func (a *TeamAgent) ObservabilityExecutionSubject(sessionID string) agentcoreharness.ExecutionSubject {
	memberName := a.MemberName()
	if memberName == "" {
		if a.card != nil {
			memberName = a.card.Name
		}
		if memberName == "" && a.card != nil {
			memberName = a.card.ID
		}
	}
	displayName := memberName
	if rc := a.RuntimeContext(); rc != nil && rc.DisplayName != "" {
		displayName = rc.DisplayName
	}
	return TeamMemberExecutionSubject(
		sessionID,
		a.TeamName(),
		memberName,
		displayName,
		a.Role() == atschema.TeamRoleLeader,
	)
}

// NewTeamAgent 创建新的 TeamAgent 实例。
// Python: TeamAgent.__init__(card)
// S-37: ctx 作为后续 SpawnManager.baseCtx 的传播链根，
// 恢复任务（OnTeammateUnhealthy 等）随 Leader ctx 取消而停止
func NewTeamAgent(ctx context.Context, card *agentschema.AgentCard) *TeamAgent {
	a := &TeamAgent{
		card:         card,
		state:        NewTeamAgentState(),
		configurator: NewAgentConfigurator(card),
	}
	// 构建 SpawnManager
	a.spawnManager = NewSpawnManager(ctx, a.state, a.configurator, func() *TeamAgent { return a })
	a.recoveryManager = NewRecoveryManager(a.configurator, a.spawnManager)
	a.sessionManager = NewSessionManager(a.state, a.configurator, a.recoveryManager)
	// 9.55: 构建 CoordinationKernel(self)，TeamAgent 通过适配器实现 KernelHost。
	// 注意顺序：必须先于 StreamController——后者通过 WithWakeMailbox 注入
	// a.coordination.WakeMailboxIfInterruptCleared 方法值，若 coordination 仍为 nil，
	// 会绑定 nil receiver，等到订阅驱动的 onIdleSettled 触发该回调时 panic。
	a.coordination = coordination.NewCoordinationKernel(a)
	a.streamController = NewStreamController(
		a.configurator.Blueprint,
		a.state,
		a.configurator.Resources(),
		a.UpdateStatus,
		a.updateExecution,
		// S-33: 对齐 Python team_agent.py L83-91，注入协调内核回调
		WithWakeMailbox(a.coordination.WakeMailboxIfInterruptCleared),
		WithRequestCompletionPoll(a.requestCompletionPoll),
		// 25.1a 回填（Task 8）：轮结束边的成员启动对账。
		// Python: member_startup_reconciler=self._reconcile_member_startup（team_agent.py L213）
		WithReconcileMemberStartup(a.ReconcileMemberStartup),
		// Python: task_board_probe=self.is_task_board_settled（team_agent.py L214）
		WithTaskBoardProbe(a.isTaskBoardSettled),
		// Python: team_agent.py:216-217——模型组名访问器与路由记账回调
		WithModelGroupLabelGetter(a.modelGroupLabel),
		WithServedModelRecorder(a.recordServedModel),
	)
	// Python: team_agent.py L777-780 —— 只有 leader 持团队级活动视图：它是唯一
	// 观察每个成员转换的角色，也是 team-idle 标记所属的流。
	// Python 位置在 configure(ctx) 内（role 已知）；Go 侧构造期即有 spec role，
	// 等价地在装配尾部落位（幂等，非 leader 保持 nil）。
	if a.configurator != nil {
		if bp := a.configurator.Blueprint(); bp != nil &&
			bp.Role() == atschema.TeamRoleLeader && bp.Ctx.MemberName != "" {
			a.state.MemberRegistry = NewMemberActivityRegistry(bp.Ctx.MemberName)
		}
	}
	return a
}

// ObserveMemberStatus 把一个成员的当前状态折入活动名册，并驱动 team-idle
// 标记计时器。名册欠什么由 MemberActivityRegistry.Record 决定；流控制器拥有
// 在安静保持之后投递它的计时器。非 leader（无名册）或空成员名 no-op。
// Python: TeamAgent.observe_member_status（team_agent.py L1262-1281）
func (a *TeamAgent) ObserveMemberStatus(memberName string, status atschema.MemberStatus) {
	registry := a.state.MemberRegistry
	if registry == nil || memberName == "" {
		return
	}
	signal := registry.Record(memberName, status)
	switch signal {
	case IdleSignalSchedule:
		a.streamController.ScheduleTeamIdle()
	case IdleSignalCancel:
		a.streamController.CancelTeamIdle()
	}
}

// modelGroupLabel 团队模型组名；非组池或无 backend 返回空串。
// Python: TeamAgent._model_group_label (team_agent.py:1246-1250)
func (a *TeamAgent) modelGroupLabel() string {
	backend := a.TeamBackend()
	if backend == nil {
		return ""
	}
	return backend.ModelGroupLabel()
}

// recordServedModel 持久化模型组刚把本成员路由到的模型。
// backend 缺失或成员名为空返回 false。
// Python: TeamAgent._record_served_model (team_agent.py:1251-1257)
func (a *TeamAgent) recordServedModel(modelName string) bool {
	backend := a.TeamBackend()
	memberName := a.MemberName()
	if backend == nil || memberName == "" {
		return false
	}
	return backend.DB().Member().UpdateMemberServedModel(
		context.Background(), memberName, backend.TeamName(), modelName)
}

// 属性 — 委托给 configurator

// AgentCard 返回 Agent 身份卡片。
// 满足 spawn.SpawnableAgent 接口。
// Python: TeamAgent.card property
func (a *TeamAgent) AgentCard() *agentschema.AgentCard {
	return a.card
}

// Blueprint 返回静态装配蓝图，configure() 前为 nil。
// Python: TeamAgent.blueprint property
func (a *TeamAgent) Blueprint() *TeamAgentBlueprint {
	if a.configurator != nil {
		return a.configurator.Blueprint()
	}
	return nil
}

// State 返回可变运行时状态容器。
// Python: TeamAgent.state property
func (a *TeamAgent) State() *TeamAgentState {
	return a.state
}

// Infra 返回每进程团队基础设施容器。
// Python: TeamAgent.infra property
func (a *TeamAgent) Infra() *TeamInfra {
	if a.configurator != nil {
		return a.configurator.Infra()
	}
	return nil
}

// Resources 返回每实例运行时资源容器。
// Python: TeamAgent.resources property
func (a *TeamAgent) Resources() *PrivateAgentResources {
	if a.configurator != nil {
		return a.configurator.Resources()
	}
	return nil
}

// Harness 返回拥有底层 DeepAgent 运行时的 Harness。
// Python: TeamAgent.harness property
//
// 所有对 DeepAgent 运行时的访问（config、model、workspace、
// rails、streaming）必须通过此对象。
func (a *TeamAgent) Harness() *harness.TeamHarness {
	if a.configurator != nil {
		return a.configurator.Harness()
	}
	return nil
}

// GetCurrentContext 返回本成员 DeepAgent 当前上下文消息。
// fork 继承的源读取面（spawn.ForkContextSource 窄接口由 *TeamAgent 满足）。
// Python: agent.resources.harness.get_deep_agent().get_current_context(session_id=...)
func (a *TeamAgent) GetCurrentContext(ctx context.Context, sessionID string, contextID string) ([]llmschema.BaseMessage, error) {
	h := a.Harness()
	if h == nil || h.Native() == nil || h.Native().DeepAgent() == nil {
		return nil, fmt.Errorf("harness 未装配，无法读取上下文")
	}
	return h.Native().DeepAgent().GetCurrentContext(ctx, sessionID, contextID)
}

// CreateNewContextEngine 以 messages 为种子创建新的上下文引擎（fork 注入面）。
// Python: native.create_new_context_engine(session_id=..., messages=...)
func (a *TeamAgent) CreateNewContextEngine(ctx context.Context, sessionID string, messages []llmschema.BaseMessage) (string, error) {
	h := a.Harness()
	if h == nil || h.Native() == nil || h.Native().DeepAgent() == nil {
		return "", fmt.Errorf("harness 未装配，无法创建上下文引擎")
	}
	return h.Native().DeepAgent().CreateNewContextEngine(ctx, sessionID, messages)
}

// MemberRuntime 返回外部成员运行时（external CLI 成员）；nil 表示 DeepAgent 路。
// Python: TeamAgent.harness property（team_agent.py:325-333）——
// 默认是 DeepAgent 背书的 TeamHarness，外部 CLI 成员携带 ExternalCliRuntime。
// 所有轮次/runtime 访问都应经本 MemberRuntime 表面。
func (a *TeamAgent) MemberRuntime() MemberRuntime {
	if a.configurator != nil {
		return a.configurator.MemberRuntime()
	}
	return nil
}

// ShareWorkspaceCacheWith 与进程内 teammate 共享团队级 workspace manager。
// leader 只持一个 TeamWorkspaceManager（装配期构建），进程内成员按引用复用
// 同一 manager（镜像 share_checkpoints_with）——它们的成员 workspace 装配
// 复用检查命中 leader 的缓存，不会自建。必须在 teammate.Configure 之前调：
// 之后 teammate 已建好自己的 manager。
// Python: share_workspace_cache_with (team_agent.py:1702-1715)
func (a *TeamAgent) ShareWorkspaceCacheWith(other *TeamAgent) {
	if a.configurator == nil {
		return
	}
	own := a.configurator.WorkspaceManager()
	if own != nil {
		other.AttachWorkspaceManager(own)
	}
}

// AttachWorkspaceManager 采用共享 workspace manager（进程内成员共享）。
// 由 leader 的 ShareWorkspaceCacheWith 调——teammate 按引用复用 leader 的
// manager（含驻留 WorkspaceCache），不再自建。
// Python: attach_workspace_manager (team_agent.py:1717-1722)
func (a *TeamAgent) AttachWorkspaceManager(manager *team_workspace.TeamWorkspaceManager) {
	if a.configurator != nil {
		a.configurator.SetWorkspaceManager(manager)
	}
}

// Spec 返回 TeamAgentSpec。
// Python: TeamAgent.spec property
func (a *TeamAgent) Spec() *atschema.TeamAgentSpec {
	if a.configurator != nil {
		return a.configurator.Spec()
	}
	return nil
}

// RuntimeContext 返回 TeamRuntimeContext。
// Python: TeamAgent.runtime_context property
func (a *TeamAgent) RuntimeContext() *atschema.TeamRuntimeContext {
	if a.configurator != nil {
		return a.configurator.RuntimeContext()
	}
	return nil
}

// Coordination 返回协调内核。
// Python: TeamAgent.coordination property
func (a *TeamAgent) Coordination() *coordination.CoordinationKernel {
	return a.coordination
}

// CoordinationLoop 返回底层事件总线。
// Python: TeamAgent.coordination_loop property
//
// 保留为测试和遗留调用者的公开访问器；
// 新代码应通过 coordination 访问。
func (a *TeamAgent) CoordinationLoop() *coordination.EventBus {
	if a.coordination != nil {
		return a.coordination.EventBus()
	}
	return nil
}

// Role 返回团队角色。
// Python: TeamAgent.role property
func (a *TeamAgent) Role() atschema.TeamRole {
	if a.configurator != nil {
		return a.configurator.Role()
	}
	return atschema.TeamRoleLeader
}

// Lifecycle 返回生命周期模式。
// Python: TeamAgent.lifecycle property
func (a *TeamAgent) Lifecycle() string {
	if a.configurator != nil {
		return a.configurator.Lifecycle()
	}
	// Python: AgentConfigurator.lifecycle — spec 为 None 时返回 "temporary"
	return "temporary"
}

// TeamSpec 返回 TeamSpec。
// Python: TeamAgent.team_spec property
func (a *TeamAgent) TeamSpec() *atschema.TeamSpec {
	if a.configurator != nil {
		return a.configurator.TeamSpec()
	}
	return nil
}

// MemberName 返回成员名。
// Python: TeamAgent.member_name property
func (a *TeamAgent) MemberName() string {
	if a.configurator != nil {
		return a.configurator.MemberName()
	}
	return ""
}

// MessageManager 返回消息管理器。
// Python: TeamAgent.message_manager property
func (a *TeamAgent) MessageManager() *tools.TeamMessageManager {
	if a.configurator != nil {
		return a.configurator.MessageManager()
	}
	return nil
}

// TaskManager 返回任务管理器。
// Python: TeamAgent.task_manager property
func (a *TeamAgent) TaskManager() *tools.TeamTaskManager {
	if a.configurator != nil {
		return a.configurator.TaskManager()
	}
	return nil
}

// TeamBackend 返回 TeamBackend。
// Python: TeamAgent.team_backend property
func (a *TeamAgent) TeamBackend() *tools.TeamBackend {
	if a.configurator != nil {
		return a.configurator.TeamBackend()
	}
	return nil
}

// Messager 返回成员 infra 的 messager（未配置为 nil）。
// Python: teammate.infra.messager —— spawn 经 bind_reliability_context 消费
// （external_cli_spawn.py:732，重试进度事件发布面）。
func (a *TeamAgent) Messager() messager.Messager {
	if a.configurator != nil {
		return a.configurator.Messager()
	}
	return nil
}

// SetTeamBackend 设置 TeamBackend（测试辅助）。
func (a *TeamAgent) SetTeamBackend(tb *tools.TeamBackend) {
	if a.configurator != nil {
		a.configurator.SetTeamBackend(tb)
	}
}

// SessionID 返回当前会话 ID（从 agent_teams contextvar 读取）。
// Python: TeamAgent.session_id property → get_session_id()
func (a *TeamAgent) SessionID(ctx context.Context) string {
	return agentteams.GetSessionID(ctx)
}

// SessionManager 返回会话管理器。
// Python: TeamAgent.session_manager property
func (a *TeamAgent) SessionManager() *SessionManager {
	return a.sessionManager
}

// RecoveryManager 返回恢复管理器。
// Python: TeamAgent.recovery_manager property
func (a *TeamAgent) RecoveryManager() *RecoveryManager {
	return a.recoveryManager
}

// SpawnManager 返回生成管理器。
// Python: TeamAgent.spawn_manager property
func (a *TeamAgent) SpawnManager() *SpawnManager {
	return a.spawnManager
}

// StreamController 返回流式控制器。
// Python: TeamAgent.stream_controller property
//
// 注意：返回 types.StreamControllerAccessor 窄接口以满足 KernelHost 编译约束。
// 调用方需要 *StreamController 具体类型时，用 InternalStreamController()。
func (a *TeamAgent) StreamController() types.StreamControllerAccessor {
	if a.streamController == nil {
		return nil
	}
	return a.streamController
}

// InternalStreamController 返回 *StreamController 具体类型（供同包代码使用）。
func (a *TeamAgent) InternalStreamController() *StreamController {
	return a.streamController
}

// EventListeners 返回已注册的事件监听器句柄列表。
// Python: TeamAgent.event_listeners property
func (a *TeamAgent) EventListeners() []*messager.EventListenerHandle {
	return a.state.EventListeners
}

// TeamMemberHandle 返回此 Agent 的 TeamMember 句柄（可能为 nil）。
// Python: TeamAgent.team_member property
func (a *TeamAgent) TeamMemberHandle() *TeamMember {
	return a.state.TeamMember
}

// PendingUserQuery 返回待处理的用户查询字符串。
// Python: TeamAgent.pending_user_query property
func (a *TeamAgent) PendingUserQuery() string {
	return a.state.PendingUserQuery
}

// TeamName 返回团队名。
// Python: TeamAgent.team_name property
func (a *TeamAgent) TeamName() string {
	if a.configurator != nil {
		return a.configurator.TeamName()
	}
	return ""
}

// IsShutdownRequested 此 Teammate 是否已被请求关闭或已关闭。
// Python: TeamAgent.is_shutdown_requested()
//
// Leader 从不持有 TeamMember 句柄，因此对 Leader 总返回 False。
// 包括 SHUTDOWN 本身，因为 shutdown_self 在拆卸流之前直接写入终态，
// finalize 路径必须将其视为"已在退出"而不是通过暂停决策将状态翻转回 READY。
func (a *TeamAgent) IsShutdownRequested(ctx context.Context) (bool, error) {
	// Python: member = self._state.team_member; if member is None: return False
	member := a.state.TeamMember
	if member == nil {
		return false, nil
	}
	// Python: status = await member.status()
	// 通过 TeamBackend 查询 DB 状态
	backend := a.TeamBackend()
	if backend == nil {
		return false, nil
	}
	dbMember, err := backend.GetMember(ctx, a.MemberName())
	if err != nil || dbMember == nil {
		return false, nil
	}
	status := atschema.MemberStatus(dbMember.Status)
	return status == atschema.MemberStatusShutdownRequested || status == atschema.MemberStatusShutdown, nil
}

// ReconcileMemberStartup 把板面有活但没人启动的窗口关掉（25.1a 回填 Task 8）。
// 任务创建路径自带的 auto-start 的兜底，跑在 leader 的轮结束边（IDLE edge）上：
// 本轮意图此时已完整落库，名册仍停在 UNSTARTED 而板面有开放任务就是缺口——
// 代码关掉它，而不是指望模型自己注意到。注册与启动刻意分离
// （spawn_teammate 只写 DB 行），这正是留给本钩子的窗口。
//
// 仅 leader：只有它持有名册。先探板面（一条聚合 COUNT）——没有开放任务的团队
// 没有什么可启动的（leader 可以先于工作注册成员）。autostart_unstarted 在
// UNSTARTED 集为空时是 no-op，所以静止的团队每轮几乎零开销。全程尽力而为：
// 它跑在拆机邻接的边上，这里的失败不得阻断它后面的完成轮询。
// Python: TeamAgent._reconcile_member_startup（team_agent.py L1371-1414）
func (a *TeamAgent) ReconcileMemberStartup(ctx context.Context) error {
	if a.Role() != atschema.TeamRoleLeader {
		return nil
	}
	backend := a.TeamBackend()
	if backend == nil {
		return nil
	}
	_, nonTerminal, err := backend.DB().Task().CountTasksTerminality(ctx, backend.TeamName())
	if err != nil {
		logger.Error(logComponent).Err(err).Str("member_name", a.MemberName()).
			Msg("round-idle member startup reconcile failed")
		return nil
	}
	if nonTerminal == 0 {
		return nil
	}
	started, err := backend.AutostartUnstarted(ctx, false)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("member_name", a.MemberName()).
			Msg("round-idle member startup reconcile failed")
		return nil
	}
	if len(started) > 0 {
		logger.Info(logComponent).Str("member_name", a.MemberName()).
			Str("started", strings.Join(started, ",")).
			Msg("round-idle reconcile started members")
	}
	return nil
}

// UpdateStatus 更新成员状态到数据库。
// Python: TeamAgent.update_status(status)
func (a *TeamAgent) UpdateStatus(ctx context.Context, status atschema.MemberStatus) error {
	backend := a.TeamBackend()
	if backend == nil {
		logger.Debug(logComponent).Str("member_name", a.MemberName()).
			Str("member_status", string(status)).Msg("updatestate（no TeamBackend, skipped")
	} else {
		backend.DB().Member().UpdateMemberStatus(ctx, a.MemberName(), backend.TeamName(), string(status))
		logger.Debug(logComponent).Str("member_name", a.MemberName()).
			Str("member_status", string(status)).Msg("updatestate")
	}
	// Python: _update_status 尾部无条件 observe（team_agent.py L1239-1244）——
	// leader 自身的转换不会以事件形式回来（messager 自过滤丢弃它们），这里是
	// 它们进入活动视图的唯一入口。DB 写是 no-op 也要折入：build_team 之前
	// leader 没有行，但就 idle 而言它仍是自己团队的成员。
	a.ObserveMemberStatus(a.MemberName(), status)
	return nil
}

// PersistAllocatorState 持久化模型分配器状态到当前会话。
// Python: TeamAgent.persist_allocator_state()
func (a *TeamAgent) PersistAllocatorState() {
	if a.recoveryManager != nil {
		// M-29: TeamSession 已是具体类型 *session.AgentTeamSession（实现 SessionFacade），无需断言
		if sf := a.sessionManager.TeamSession(); sf != nil {
			a.recoveryManager.PersistAllocatorState(sf)
		}
	}
}

// AddEventListener 添加事件监听器，返回可移除的句柄。
// Python: TeamAgent.add_event_listener(handler)
func (a *TeamAgent) AddEventListener(handler messager.MessagerHandler) *messager.EventListenerHandle {
	h := messager.NewEventListenerHandle(handler)
	a.state.EventListeners = append(a.state.EventListeners, h)
	return h
}

// RemoveEventListener 移除事件监听器。
// Python: TeamAgent.remove_event_listener(handler)
func (a *TeamAgent) RemoveEventListener(handle *messager.EventListenerHandle) {
	for i, h := range a.state.EventListeners {
		if h.ID() == handle.ID() {
			a.state.EventListeners = append(a.state.EventListeners[:i], a.state.EventListeners[i+1:]...)
			return
		}
	}
}

// LookupHumanAgentRuntime 解析进程内生成的人类代理的活跃 TeamAgent。
// Python: TeamAgent.lookup_human_agent_runtime(member_name)
func (a *TeamAgent) LookupHumanAgentRuntime(memberName string) *TeamAgent {
	// 修复 S-13: 先检查 is_human_agent，对齐 Python: backend.is_human_agent(member_name)
	backend := a.TeamBackend()
	if backend == nil || !backend.IsHumanAgent(memberName) {
		return nil
	}
	if a.spawnManager == nil {
		return nil
	}
	agent := a.spawnManager.LookupInprocessAgent(memberName)
	if agent == nil {
		return nil
	}
	// 类型断言：SpawnableAgent → *TeamAgent
	ta, ok := agent.(*TeamAgent)
	if !ok {
		return nil
	}
	return ta
}

// IsAgentReady Agent 是否已就绪。
// Python: TeamAgent.is_agent_ready()
func (a *TeamAgent) IsAgentReady() bool {
	if a.configurator != nil {
		// Python: configurator.harness is not None——外部成员 runtime 同样算就绪
		return a.configurator.Harness() != nil || a.configurator.MemberRuntime() != nil
	}
	return false
}

// IsAgentRunning Agent 是否正在运行。
// Python: TeamAgent.is_agent_running()
func (a *TeamAgent) IsAgentRunning() bool {
	if a.streamController != nil {
		return a.streamController.IsAgentRunning()
	}
	return false
}

// HasInFlightRound 是否有飞行中的轮次。
// Python: TeamAgent.has_in_flight_round()
func (a *TeamAgent) HasInFlightRound() bool {
	if a.streamController != nil {
		return a.streamController.HasInFlightRound()
	}
	return false
}

// HasPendingInterrupt 是否有待处理的中断。
// Python: TeamAgent.has_pending_interrupt()
func (a *TeamAgent) HasPendingInterrupt() bool {
	if a.streamController != nil {
		return a.streamController.HasPendingInterrupt()
	}
	return false
}

// Configure 配置 TeamAgent。
// 装配失败（角色策略/提示词模板缺失等）时返回错误，不再静默降级。
// Python: TeamAgent.configure(spec, context)
func (a *TeamAgent) Configure(ctx context.Context, spec atschema.TeamAgentSpec, runtimeCtx atschema.TeamRuntimeContext, memberRuntime ...MemberRuntime) error {
	// Phase 1: 基础设施搭建
	if err := a.configurator.SetupInfra(spec, runtimeCtx,
		WithOnTeammateCreated(a.onTeammateCreated),
		WithOnTeamCleaned(a.markTeamCleaned),
		WithOnTeamBuilt(a.markTeamBuilt),
	); err != nil {
		return err
	}

	// 25.5: leader 自身 checkpoint() 工具的存储回调进 leader 权威命名空间。
	// set_store_checkpoint_fn 否则只为 in-process 队友接线（inprocess_spawn）；
	// 不接这条的话 leader 自己的快照会落进 backend fallback dict，fork 解析
	// 永远看不到。
	// Python: team_agent.py:730-741（role == TeamRole.LEADER 分支）
	if runtimeCtx.Role == atschema.TeamRoleLeader {
		if backend := a.configurator.TeamBackend(); backend != nil {
			backend.SetStoreCheckpointFn(a.SetCheckpoint)
			backend.SetCheckpointListFn(func() map[string]map[string]any {
				snapshot := a.checkpointsSnapshot()
				out := make(map[string]map[string]any, len(snapshot))
				for name, rec := range snapshot {
					out[name] = map[string]any{
						"count":       rec.Count,
						"description": rec.Description,
						"created_by":  rec.CreatedBy,
					}
				}
				return out
			})
		}
	}

	// Phase 2: Agent 组装
	if _, err := a.configurator.SetupAgent(spec, runtimeCtx, memberRuntime...); err != nil {
		return err
	}

	// 构建 TeamMember 句柄
	if runtimeCtx.MemberName != "" {
		a.state.TeamMember = CreateMemberHandle(
			runtimeCtx.MemberName,
			a.configurator.Blueprint(),
			a.configurator.Infra(),
			a.card,
		)
	}

	// 9.55: 设置协调角色 + 注册团队完成回调
	// S-32/M-02: 对齐 Python kernel.setup(role)——blueprint/infra 由 kernel 从 host 取
	a.coordination.Setup(runtimeCtx.Role)

	// 修复 S-04: 对齐 Python _register_team_completion_callbacks()
	// 在 DeepAgent 完全构建后、dispatcher 存在时注册 TeamSkillRail 的完成回调
	a.registerTeamCompletionCallbacks()

	// 25.1g: 对齐 Python _register_reliability_local_sink()（team_agent.py:783）
	// leader 自检的可靠性 rail 绑到进程内异常 sink，绕过 messager self-filter
	a.registerReliabilityLocalSink()

	logger.Info(logComponent).Str("member_name", runtimeCtx.MemberName).
		Str("role", string(runtimeCtx.Role)).Msg("TeamAgent config")
	return nil
}

// Invoke 非流式调用 TeamAgent。
// Python: TeamAgent.invoke(inputs, session)
func (a *TeamAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
	defer a.coordination.FinalizeRound(ctx) // S-35: 对齐 Python try/finally

	memberName := a.MemberName()
	logger.Info(logComponent).Str("member_name", memberName).
		Str("role", string(a.Role())).Msg("TeamAgent Invoke started")

	// 未配置时直接返回（对齐 Python：未 configure 的 TeamAgent 无法执行 invoke）
	if a.configurator == nil || a.configurator.Blueprint() == nil {
		logger.Warn(logComponent).Msg("TeamAgent Invoke: not configured, skipped")
		return nil, nil
	}

	// Python: self._stream_controller.stream_queue = asyncio.Queue()
	if a.streamController != nil {
		a.streamController.streamQueue = make(chan stream.Schema, 64)
	}

	// Python: self._state.pending_user_query = inputs.get("query", "") if isinstance(inputs, dict) else str(inputs)
	// S-07: 无条件覆盖（对齐 Python inputs.get("query", "")）——无 key 或非 string 时
	// 清空，不保留上轮旧值（原实现仅 key 存在才写，跨轮残留过期查询注入 kernel）。
	// Python else str(inputs) 分支因 Go 签名固定 map[string]any 不可达，无需实现。
	query, _ := inputs["query"].(string)
	a.state.PendingUserQuery = query

	// Python: await self._coordination.start(session)
	// M-19: session 优先取 opts 传入（对齐 Python invoke(inputs, session) 显式参数）；
	// opts 未传时回退 ctx 中的 SessionState（对齐 Python contextvar 全局可见语义）。
	// 25.1a：改传会话对象（sessionFromOptsOrCtx），kernel 需对象启动 harness native supervisor。
	a.coordination.Start(ctx, a.sessionFromOptsOrCtx(ctx, opts...))

	// Python team_agent.py:970-976：
	//   if raw_query: enqueue_user_input(inputs)
	//   enqueue_initial_mailbox_poll()
	//   enqueue_initial_task_poll()
	// 两个初始 poll 是「流开始时立刻做一次完成判定」的源头，不等 30s 周期 tick。
	a.coordination.EnqueueUserInput(query)
	a.coordination.EnqueueMailboxAfterFirstIteration(ctx)
	a.coordination.EnqueueInitialTaskPoll(ctx)

	// Python: last_result = None
	//   while True:
	//     chunk = await self._stream_controller.stream_queue.get()
	//     if chunk is None: break
	//     last_result = chunk
	//   return last_result
	var lastResult map[string]any
	if a.streamController != nil && a.streamController.streamQueue != nil {
		for {
			select {
			case chunk, ok := <-a.streamController.streamQueue:
				if !ok || chunk == nil {
					return lastResult, nil
				}
				// Python last_result = chunk 保留最后一个 chunk 对象；
				// Go 接口返回 map[string]any，经 ToMap()（pydantic model_dump 等价）转换
				lastResult = chunk.ToMap()
			case <-ctx.Done():
				return lastResult, nil
			}
		}
	}

	return lastResult, nil
}

// Stream 流式调用 TeamAgent。
// Python: TeamAgent.stream(inputs, session, stream_modes)
func (a *TeamAgent) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	memberName := a.MemberName()
	logger.Info(logComponent).Str("member_name", memberName).
		Str("role", string(a.Role())).Msg("TeamAgent Stream started")

	// 未配置时返回**已关闭**的空流。
	//
	// 上游 team_agent.py:958-980 的 stream 是 async generator，无条件走流程；
	// generator 自然 return 等价于流立即结束，不存在「返回 nil 流」这一态。
	// Go 用 channel 表达流，返回 nil channel 会让下游 `for range agentCh`
	// 永久阻塞（连带 mergedCh 的 defer close 执行不到）。故这里显式造一个
	// 已关闭的通道，让 range 立即结束——这才是一比一等价。
	if a.configurator == nil || a.configurator.Blueprint() == nil {
		logger.Warn(logComponent).Msg("TeamAgent Stream: not configured, skipped")
		closed := make(chan stream.Schema)
		close(closed)
		return closed, nil
	}

	// Python: self._stream_controller.stream_queue = asyncio.Queue()
	if a.streamController != nil {
		a.streamController.streamQueue = make(chan stream.Schema, 64)
	}

	// Python: self._state.pending_user_query = inputs.get("query", "") if isinstance(inputs, dict) else str(inputs)
	// S-07: 无条件覆盖（对齐 Python inputs.get("query", "")）——无 key 或非 string 时
	// 清空，不保留上轮旧值（原实现仅 key 存在才写，跨轮残留过期查询注入 kernel）。
	// Python else str(inputs) 分支因 Go 签名固定 map[string]any 不可达，无需实现。
	query, _ := inputs["query"].(string)
	a.state.PendingUserQuery = query

	// Python: await self._coordination.start(session)
	// M-19: session 优先取 opts 传入（对齐 Python stream(inputs, session) 显式参数）；
	// opts 未传时回退 ctx 中的 SessionState（对齐 Python contextvar 全局可见语义）。
	// 25.1a：改传会话对象（sessionFromOptsOrCtx），kernel 需对象启动 harness native supervisor。
	a.coordination.Start(ctx, a.sessionFromOptsOrCtx(ctx, opts...))

	// Python team_agent.py:970-976：
	//   if raw_query: enqueue_user_input(inputs)
	//   enqueue_initial_mailbox_poll()
	//   enqueue_initial_task_poll()
	// 两个初始 poll 是「流开始时立刻做一次完成判定」的源头，不等 30s 周期 tick。
	a.coordination.EnqueueUserInput(query)
	a.coordination.EnqueueMailboxAfterFirstIteration(ctx)
	a.coordination.EnqueueInitialTaskPoll(ctx)

	// Python: while True:
	//   chunk = await self._stream_controller.stream_queue.get()
	//   if chunk is None: break
	//   yield chunk
	//
	// Go: 将内部 streamQueue 通过 goroutine 转发到只读输出 channel
	outCh := make(chan stream.Schema, 64)
	if a.streamController != nil && a.streamController.streamQueue != nil {
		go func() {
			defer close(outCh)
			for chunk := range a.streamController.streamQueue {
				if chunk == nil {
					break
				}
				// S-31 顺手修: select+ctx.Done 保护，消费者停止读取且取消 ctx 时 goroutine 不泄漏
				select {
				case outCh <- chunk:
				case <-ctx.Done():
					// Python: 消费者放弃读取触发 GeneratorExit → finally
					a.finalizeRoundDetached(ctx)
					return
				}
			}
			// Python: finally: await self._coordination.finalize_round()
			// S-30: finally 语义不受调用方取消影响，用 WithoutCancel 派生
			a.finalizeRoundDetached(ctx)
		}()
	} else {
		// S-31: 空 streamQueue 路径也要执行 FinalizeRound（对齐 Python finally 双路径语义）
		a.finalizeRoundDetached(ctx)
		close(outCh)
	}

	return outCh, nil
}

// Interact 向团队发送输入。
// Python: TeamAgent.interact(message)
func (a *TeamAgent) Interact(ctx context.Context, message string) error {
	if a.coordination != nil {
		a.coordination.EnqueueUserInput(message)
	}
	return nil
}

// Broadcast 广播用户侧公告。
// Python: TeamAgent.broadcast(content) → DeliverResult
// 修复 S-14: 改用 UserInbox.Broadcast 对齐 Python
func (a *TeamAgent) Broadcast(ctx context.Context, content string) (*ateaminteraction.DeliverResult, error) {
	backend := a.TeamBackend()
	if backend == nil {
		return ateaminteraction.NewDeliverResultFailure("no_team_backend"), nil
	}
	msgMgr := backend.MessageManager()
	if msgMgr == nil {
		return ateaminteraction.NewDeliverResultFailure("no_message_manager"), nil
	}
	// 对齐 Python: UserInbox(backend.message_manager).broadcast(content)
	// M-27: 透传 ctx
	inbox := ateaminteraction.NewUserInbox(msgMgr)
	result, err := inbox.Broadcast(ctx, content)
	if err != nil {
		return ateaminteraction.NewDeliverResultFailure("broadcast_failed:" + err.Error()), nil
	}
	return result, nil
}

// HumanAgentSay 以注册的 human_agent 成员身份发言。
// Python: TeamAgent.human_agent_say(content, to, sender) → DeliverResult
// 修复 S-15: 改用 HumanAgentInbox.Send 对齐 Python
func (a *TeamAgent) HumanAgentSay(ctx context.Context, content string, to string, sender string) (*ateaminteraction.DeliverResult, error) {
	backend := a.TeamBackend()
	if backend == nil {
		return ateaminteraction.NewDeliverResultFailure("no_team_backend"), nil
	}
	msgMgr := backend.MessageManager()
	if msgMgr == nil {
		return ateaminteraction.NewDeliverResultFailure("no_message_manager"), nil
	}
	// 对齐 Python: HumanAgentInbox(backend, backend.message_manager, agent_lookup, on_inbound).send(content, to=to, sender=sender)
	// agent_lookup: 查找 human_agent 的 TeamAgent 实例用于驱动 avatar
	agentLookup := ateaminteraction.AgentLookup(func(name string) ateaminteraction.DeliverInputer {
		if ta := a.LookupHumanAgentRuntime(name); ta != nil {
			return ta
		}
		return nil
	})
	inbox := ateaminteraction.NewHumanAgentInbox(backend, msgMgr, agentLookup, nil)
	var toPtr *string
	if to != "" {
		toPtr = &to
	}
	var senderPtr *string
	if sender != "" {
		senderPtr = &sender
	}
	result, err := inbox.Send(ctx, content, toPtr, senderPtr)
	if err != nil {
		return ateaminteraction.NewDeliverResultFailure("send_failed:" + err.Error()), nil
	}
	return result, nil
}

// DeliverInput 投递输入到成员脑。
// Python: TeamAgent.deliver_input(content, use_steer=True)（team_agent.py:550-570）
// 运行时的单 supervisor 串行化所有输入：idle 开轮，running 转向或排队。
// 因为没有状态转换窗口的竞态，这里不需要手写分支，也不需要 pending 队列。
func (a *TeamAgent) DeliverInput(ctx context.Context, content string, useSteer bool, runContext *agentinterfaces.RunContext) error {
	if mr := a.MemberRuntime(); mr != nil {
		// 外部成员：MemberRuntime.Send(content, immediate)，无 ctx 形参
		// （Python send(content, immediate) 为协程调用；Go 外部 runtime 自持 ctx）
		_, err := mr.Send(content, useSteer)
		return err
	}
	h := a.Harness()
	if h == nil {
		return nil
	}
	// Python: run_context is None → 省略（team_agent.py L565-571）；nil 形参等价。
	return h.Send(ctx, content, useSteer, runContext)
}

// StartAgent 启动成员首轮。
// Python: TeamAgent.start_agent(content) → _start_agent → harness.send(initial_message)
func (a *TeamAgent) StartAgent(ctx context.Context, content string) error {
	if mr := a.MemberRuntime(); mr != nil {
		_, err := mr.Send(content, false)
		return err
	}
	h := a.Harness()
	if h == nil {
		return nil
	}
	return h.Send(ctx, content, false, nil)
}

// FollowUp 追加输入，等本轮结束后再跑。
// Python: TeamAgent.follow_up(content)（team_agent.py:604）
func (a *TeamAgent) FollowUp(ctx context.Context, content string) error {
	if mr := a.MemberRuntime(); mr != nil {
		_, err := mr.Send(content, false)
		return err
	}
	h := a.Harness()
	if h == nil {
		return nil
	}
	return h.Send(ctx, content, false, nil)
}

// CancelAgent 取消 Agent。
// Python: TeamAgent.cancel_agent()
func (a *TeamAgent) CancelAgent(ctx context.Context) error {
	memberName := a.MemberName()
	logger.Debug(logComponent).Str("member_name", memberName).Msg("TeamAgent.CancelAgent requested")
	if a.streamController != nil {
		return a.streamController.CancelAgent(ctx)
	}
	return nil
}

// Steer 转向当前轮。
// Python: TeamAgent.steer(content)（team_agent.py:676）
func (a *TeamAgent) Steer(ctx context.Context, content string) error {
	if mr := a.MemberRuntime(); mr != nil {
		_, err := mr.Send(content, true)
		return err
	}
	h := a.Harness()
	if h == nil {
		return nil
	}
	return h.Send(ctx, content, true, nil)
}

// ResumeInterrupt 用用户输入恢复挂起的中断。
// Python: TeamAgent.resume_interrupt(user_input)（team_agent.py:688-700）
// 校验有效后直接 send：排队与否由 supervisor 决定，不再手写 pendingInterruptResumes。
func (a *TeamAgent) ResumeInterrupt(ctx context.Context, userInput *interaction.InteractiveInput) error {
	if mr := a.MemberRuntime(); mr != nil {
		// 外部成员：MemberRuntime 表面（Python: harness.is_pending_interrupt_resume_valid
		// + send——两个实现同一鸭子面）
		if !mr.IsPendingInterruptResumeValid(userInput) {
			return nil
		}
		_, err := mr.Send(userInput, false)
		return err
	}
	h := a.Harness()
	if h == nil {
		return nil
	}
	if !h.IsPendingInterruptResumeValid(userInput) {
		return nil
	}
	return h.Send(ctx, userInput, false, nil)
}

// ShutdownSelf 请求自身关闭。
// Python: TeamAgent.shutdown_self()
func (a *TeamAgent) ShutdownSelf(ctx context.Context) error {
	memberName := a.MemberName()
	logger.Info(logComponent).Str("member_name", memberName).Msg("TeamAgent.ShutdownSelf requested")
	// Python: streamController.cooperative_cancel()
	if a.streamController != nil {
		_ = a.streamController.CooperativeCancel(ctx)
	}
	// Python: team_member.update_status(SHUTDOWN)
	_ = a.UpdateStatus(ctx, atschema.MemberStatusShutdown)
	// 修复 S-12: 关闭流队列，让 invoke/stream 读取循环退出
	// Python: self._close_stream()
	a.CloseStream()
	return nil
}

// ConcludeCompletedRound 发出团队完成标记块并关闭 Leader 流。
// Python: TeamAgent.conclude_completed_round(member_count, task_count)
func (a *TeamAgent) ConcludeCompletedRound(ctx context.Context, memberCount, taskCount int) error {
	memberName := a.MemberName()
	logger.Info(logComponent).Str("member_name", memberName).
		Int("member_count", memberCount).Int("task_count", taskCount).
		Msg("TeamAgent: round completed")
	if a.streamController != nil {
		a.streamController.EmitCompletionAndClose(memberCount, taskCount)
	}
	return nil
}

// DestroyTeam 销毁团队。
// Python: TeamAgent.destroy_team(force=True)
func (a *TeamAgent) DestroyTeam(ctx context.Context, force bool) (bool, error) {
	// Python 步骤 1: 取消运行中的 agent（best-effort）
	if a.streamController != nil {
		_ = a.streamController.CancelAgent(ctx)
	}

	// Python 步骤 2: 停止协调（best-effort）
	_ = a.StopCoordination(ctx)

	// Python 步骤 3: 从运行时池移除
	sessionIDSnapshot := a.SessionID(ctx)
	a.removeSelfFromPool(ctx, sessionIDSnapshot)

	// Python 步骤 4: ForceCleanTeam
	backend := a.TeamBackend()
	if backend == nil {
		return false, nil
	}
	return backend.ForceCleanTeam(ctx, force)
}

// StartCoordination 启动协调。
// Python: TeamAgent._start_coordination(session)
func (a *TeamAgent) StartCoordination(ctx context.Context, session any) error {
	if a.coordination != nil {
		a.coordination.Start(ctx, session)
	}
	return nil
}

// PauseCoordination 暂停协调（不拆卸 Teammate 进程）。
// Python: TeamAgent.pause_coordination()
func (a *TeamAgent) PauseCoordination(ctx context.Context) error {
	if a.coordination != nil {
		a.coordination.Pause(ctx)
	}
	return nil
}

// StopCoordination 停止协调（关闭所有生成的 Teammate）。
// Python: TeamAgent.stop_coordination()
func (a *TeamAgent) StopCoordination(ctx context.Context) error {
	if a.coordination != nil {
		a.coordination.Stop(ctx)
	}
	return nil
}

// SpawnTeammate 生成 Teammate。
// Python: TeamAgent.spawn_teammate(ctx, initial_message, session, spawn_config)
func (a *TeamAgent) SpawnTeammate(ctx context.Context, runtimeCtx atschema.TeamRuntimeContext, initialMessage string, sessionID string, spawnCfg *runnerspawn.SpawnConfig, forkFrom *agentteams.ForkContext) error {
	if a.spawnManager != nil {
		return a.spawnManager.SpawnTeammate(ctx, runtimeCtx, initialMessage, sessionID, spawnCfg, forkFrom)
	}
	return nil
}

// AutoStartMember 启动单个 UNSTARTED 成员。
// Python: TeamAgent.auto_start_member(member_name)
func (a *TeamAgent) AutoStartMember(ctx context.Context, memberName string) bool {
	backend := a.TeamBackend()
	if backend == nil || !backend.IsLeader() {
		return false
	}
	started, err := backend.StartupMember(ctx, memberName, func(ctx context.Context, name string) error {
		return a.onTeammateCreated(ctx, name)
	})
	if err != nil {
		logger.Error(logComponent).Str("member_name", memberName).Err(err).Msg("AutoStartMember failed")
		return false
	}
	return started
}

// AnnounceTaskRevisions 让成员运行时立刻投递待发任务编辑通知（F-129）。
//
// 通知的唯一来源是成员的任务通知器；TASK_UPDATED 事件只是"要求提前说"。
// 运行时不支持则静默跳过——下一个输入会携带它。
//
// Python: TeamAgent.announce_task_revisions (team_agent.py:539-550)
func (a *TeamAgent) AnnounceTaskRevisions(ctx context.Context) error {
	h := a.Harness()
	if h == nil {
		return nil
	}
	return h.AnnounceTaskRevisions(ctx)
}

// AutoStartAll 启动所有 UNSTARTED 成员。
//
// 只有交互广播（@all / 未指名消息）会走到这里，所以 task-scoped 且没有开放
// 任务的成员也要起来：广播同样是发给它的。
//
// Python: TeamAgent.auto_start_all (team_agent.py:1976-1997)
func (a *TeamAgent) AutoStartAll(ctx context.Context) []string {
	backend := a.TeamBackend()
	if backend == nil || !backend.IsLeader() {
		return nil
	}
	started, err := backend.AutostartUnstarted(ctx, true)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("AutoStartAll failed")
		return nil
	}
	return started
}

// BuildSpawnPayload 构建生成载荷。
// Python: TeamAgent.build_spawn_payload(ctx, initial_message)
func (a *TeamAgent) BuildSpawnPayload(runtimeCtx atschema.TeamRuntimeContext, initialMessage string) map[string]any {
	if a.configurator != nil {
		return a.configurator.BuildSpawnPayload(runtimeCtx, initialMessage)
	}
	return nil
}

// BuildMemberContext 构建成员上下文。
// Python: TeamAgent.build_member_context(member_spec)
func (a *TeamAgent) BuildMemberContext(memberSpec atschema.TeamMemberSpec) atschema.TeamRuntimeContext {
	if a.configurator != nil {
		return a.configurator.BuildMemberContext(memberSpec)
	}
	return atschema.TeamRuntimeContext{}
}

// BuildSpawnConfig 构建生成配置。
// Python: TeamAgent.build_spawn_config(ctx)
func (a *TeamAgent) BuildSpawnConfig(runtimeCtx atschema.TeamRuntimeContext) runnerspawn.SpawnAgentConfig {
	if a.configurator != nil {
		return a.configurator.BuildSpawnConfig(runtimeCtx)
	}
	return runnerspawn.SpawnAgentConfig{}
}

// FromSpawnPayload 从生成载荷重构 TeamAgent。
// Python: TeamAgent.from_spawn_payload(payload)（team_agent.py L721-746）
// payload 的 spec/context 由 BuildSpawnConfig 写入（model_dump 等价的
// map[string]any），经跨进程 JSON 线路传输后子进程拿到的是 map 而非 struct——
// S-26/S-27 关联修复：原直接类型断言 map→struct 恒失败（跨进程 payload 必然
// 全部走 map），改为 map→JSON→struct 桥接（对齐 Python model_validate）。
func FromSpawnPayload(ctx context.Context, payload map[string]any) (*TeamAgent, error) {
	// 步骤 1: 反序列化 spec/context（Python: TeamAgentSpec.model_validate / TeamRuntimeContext.model_validate）
	spec, err := teamAgentSpecFromAny(payload["spec"])
	if err != nil {
		return nil, fmt.Errorf("payload 中 spec 字段无效: %w", err)
	}
	runtimeCtx, err := teamRuntimeContextFromAny(payload["context"])
	if err != nil {
		return nil, fmt.Errorf("payload 中 context 字段无效: %w", err)
	}

	// 步骤 1b: 从种子重建 build_context（Python team_agent.py:1467）：
	// build_context 是 Field(exclude=True)（blueprint.py:645），跨进程
	// 反序列化后为 nil，不 materialize 则 provider 装配的成员能力全丢；
	// 无种子的旧路径为 no-op。
	if err := spec.MaterializeBuildContext(); err != nil {
		return nil, fmt.Errorf("materialize build context 失败: %w", err)
	}

	// 步骤 2: 构造 AgentCard（Python: agent_spec = spec.agents.get(role.value) or spec.agents["leader"]；
	// card = agent_spec.card or AgentCard(id=team_member, ...)）
	card := buildTeamAgentCardFromSpec(spec, runtimeCtx)

	// 步骤 3: NewTeamAgent + Configure
	agent := NewTeamAgent(ctx, card)
	if err := agent.Configure(ctx, *spec, runtimeCtx); err != nil {
		return nil, fmt.Errorf("配置 TeamAgent 失败: %w", err)
	}

	// 步骤 4: RefreshHumanAgentRoster（Python: 子进程持有独立 TeamBackend，
	// HITT roster 缓存为空，需从 DB 拉取当前名册使 is_human_agent 同步检查生效）
	if backend := agent.TeamBackend(); backend != nil {
		backend.RefreshHumanAgentRoster(ctx)
	}

	return agent, nil
}

// RecoverFromSession 从 session 检查点重构 Leader TeamAgent。
// 流程：读取 team namespace → 解析 spec/context → NewTeamAgent → configure → restore_allocator_state → set_session_id
//
// Python: TeamAgent.recover_from_session(session, team_name, runtime_spec)
func RecoverFromSession(ctx context.Context, session any, teamName string, runtimeSpec *atschema.TeamAgentSpec) (*TeamAgent, error) {
	// 步骤 1: 读取 team namespace
	var sf sessinterfaces.SessionFacade
	if s, ok := session.(sessinterfaces.SessionFacade); ok {
		sf = s
	} else {
		return nil, fmt.Errorf("session 不满足 SessionFacade 接口")
	}
	bucket := metadata.ReadTeamNamespace(sf, teamName)
	if bucket == nil {
		return nil, fmt.Errorf("team namespace %s 不存在", teamName)
	}

	// 步骤 2: 反序列化 spec（S-26: bucket["spec"] 是 PersistLeaderConfig 写入的
	// map[string]any（model_dump 等价物），原直接断言 atschema.TeamAgentSpec 恒失败，
	// COLD_RECOVER 冷恢复路径 100% 报错——改用 map→JSON 桥接对齐 Python model_validate）
	spec, err := teamAgentSpecFromAny(bucket["spec"])
	if err != nil {
		return nil, fmt.Errorf("team spec 反序列化失败: %w", err)
	}

	// 步骤 3: 如果 runtimeSpec 有自定义器则覆盖
	// Python: agent_customizer 是 Field(exclude=True) 的 Callable，序列化时被丢弃，
	// 冷恢复必须从运行时 spec 重新注入，否则平台适配器的 rails/tools 钩子被静默禁用
	if runtimeSpec != nil && runtimeSpec.AgentCustomizer != nil {
		spec.AgentCustomizer = runtimeSpec.AgentCustomizer
	}
	// 步骤 3b: embedding_config 同样是 Field(exclude=True) 不参与序列化，冷恢复必须
	// 从运行时 spec 重注，否则 resolve_embedding_config 找不到它、记忆检索退化为纯关键词。
	// 对齐 Python TeamAgent.recover_from_session（team_agent.py:1119-1124）。
	if runtimeSpec != nil && runtimeSpec.Memory != nil &&
		runtimeSpec.Memory.EmbeddingConfig != nil && spec.Memory != nil {
		spec.Memory.EmbeddingConfig = runtimeSpec.Memory.EmbeddingConfig
	}

	// 步骤 3c: build_context 同为 Field(exclude=True) 不参与 checkpoint 序列化：
	// 暖恢复优先沿用活 runtime spec 的上下文，冷恢复从可序列化种子重建。
	// Python: TeamAgent.recover_from_session（team_agent.py:2074-2075）
	if runtimeSpec != nil && runtimeSpec.BuildContext != nil {
		spec.BuildContext = runtimeSpec.BuildContext
	}

	// 步骤 3d: 物化种子（Python team_agent.py:2081）；无种子旧路径为 no-op。
	if err := spec.MaterializeBuildContext(); err != nil {
		return nil, fmt.Errorf("materialize build context 失败: %w", err)
	}

	// 步骤 4: 反序列化 context（S-26: 同步骤 2 的桥接修复）
	runtimeCtx, err := teamRuntimeContextFromAny(bucket["context"])
	if err != nil {
		return nil, fmt.Errorf("team context 反序列化失败: %w", err)
	}

	// 步骤 5: 构造 AgentCard（对齐 Python: agent_spec.card or AgentCard(card_id, ...)）
	card := buildTeamAgentCardFromSpec(spec, runtimeCtx)

	// 步骤 6: NewTeamAgent + Configure
	agent := NewTeamAgent(ctx, card)
	if err := agent.Configure(ctx, *spec, runtimeCtx); err != nil {
		return nil, fmt.Errorf("配置 TeamAgent 失败: %w", err)
	}

	// 步骤 7: 恢复 allocator state
	allocatorStateAny := bucket["model_allocator_state"]
	if allocatorState, ok := allocatorStateAny.(map[string]any); ok {
		agent.RestoreAllocatorState(allocatorState)
	}

	// 步骤 8: 设置 session_id
	if sessionIDer, ok := session.(interface{ GetSessionID() string }); ok {
		if state := sessionctx.SessionStateFromCtx(ctx); state != nil {
			state.SetSessionID(sessionIDer.GetSessionID())
		}
	}

	return agent, nil
}

// teamAgentSpecFromAny 从 any（struct 直传或 map 桥接）解析 TeamAgentSpec。
// S-26: 跨进程/checkpoint 路径拿到的是 map[string]any（model_dump 等价物），
// 原直接断言 atschema.TeamAgentSpec 恒失败。
func teamAgentSpecFromAny(v any) (*atschema.TeamAgentSpec, error) {
	if v == nil {
		return nil, fmt.Errorf("spec 数据为空")
	}
	// 同进程直传 struct（测试/内存传递路径）
	if s, ok := v.(atschema.TeamAgentSpec); ok {
		return &s, nil
	}
	if ps, ok := v.(*atschema.TeamAgentSpec); ok && ps != nil {
		return ps, nil
	}
	// 跨进程 map 桥接（Python model_validate 等价）
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("spec 数据类型 %T 非 map[string]any", v)
	}
	if spec := atschema.NewTeamAgentSpecFromDict(m); spec != nil {
		return spec, nil
	}
	return nil, fmt.Errorf("spec 反序列化失败")
}

// teamRuntimeContextFromAny 从 any（struct 直传或 map 桥接）解析 TeamRuntimeContext。
// S-26: 原直接断言 atschema.TeamRuntimeContext 恒失败（同 teamAgentSpecFromAny）。
func teamRuntimeContextFromAny(v any) (atschema.TeamRuntimeContext, error) {
	if v == nil {
		return atschema.TeamRuntimeContext{}, fmt.Errorf("context 数据为空")
	}
	if c, ok := v.(atschema.TeamRuntimeContext); ok {
		return c, nil
	}
	if pc, ok := v.(*atschema.TeamRuntimeContext); ok && pc != nil {
		return *pc, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return atschema.TeamRuntimeContext{}, fmt.Errorf("context 数据类型 %T 非 map[string]any", v)
	}
	// map→JSON→struct 桥接（对齐 Python TeamRuntimeContext.model_validate）
	data, err := json.Marshal(m)
	if err != nil {
		return atschema.TeamRuntimeContext{}, err
	}
	var ctx atschema.TeamRuntimeContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return atschema.TeamRuntimeContext{}, err
	}
	return ctx, nil
}

// buildTeamAgentCardFromSpec 对齐 Python from_spawn_payload/recover_from_session 的
// card 构造逻辑：优先 spec.agents[role] 的 card，缺失时按 team_name+member_name
// 构建兜底卡片。
func buildTeamAgentCardFromSpec(spec *atschema.TeamAgentSpec, runtimeCtx atschema.TeamRuntimeContext) *agentschema.AgentCard {
	// Python: team_name = (context.team_spec.team_name if context.team_spec else None) or spec.team_name
	teamName := spec.TeamName
	if runtimeCtx.TeamSpec != nil && runtimeCtx.TeamSpec.TeamName != "" {
		teamName = runtimeCtx.TeamSpec.TeamName
	}
	// Python: agent_spec = spec.agents.get(context.role.value) or spec.agents["leader"]
	var agentSpec *agentschema.AgentCard
	if as, ok := spec.Agents[string(runtimeCtx.Role)]; ok && as.Card != nil {
		agentSpec = as.Card
	} else if as, ok := spec.Agents["leader"]; ok && as.Card != nil {
		agentSpec = as.Card
	}
	if agentSpec != nil {
		return agentSpec
	}
	cardID := "unknown"
	if runtimeCtx.MemberName != "" {
		cardID = fmt.Sprintf("%s_%s", teamName, runtimeCtx.MemberName)
	}
	name := runtimeCtx.MemberName
	if name == "" {
		name = "unknown"
	}
	description := "Teammate"
	if runtimeCtx.Persona != "" {
		description = fmt.Sprintf("Teammate: %s", runtimeCtx.Persona)
	}
	return agentschema.NewAgentCard(
		agentschema.WithAgentID(cardID),
		agentschema.WithAgentName(name),
		agentschema.WithAgentDescription(description),
	)
}

// ResumeForNewSession 为新会话恢复。
// Python: TeamAgent.resume_for_new_session(session)
// 返回新的 context.Context（含 session_id），调用方必须用于后续传播。
func (a *TeamAgent) ResumeForNewSession(ctx context.Context, session any) (context.Context, error) {
	if a.sessionManager != nil {
		return a.sessionManager.ResumeForNewSession(ctx, session)
	}
	return ctx, nil
}

// RecoverForExistingSession 恢复已有会话检查点。
// Python: TeamAgent.recover_for_existing_session(session)
//
// 与 RecoverFromSession 不同，此方法复用当前 Agent，
// 假定 session.pre_run() 已恢复检查点状态。
// 返回新的 context.Context（含 session_id），调用方必须用于后续传播。
func (a *TeamAgent) RecoverForExistingSession(ctx context.Context, session any) (context.Context, error) {
	// 修复 S-09: 先停止协调，对齐 Python: await self._stop_coordination()
	_ = a.StopCoordination(ctx)
	if a.sessionManager != nil {
		return a.sessionManager.RecoverForExistingSession(ctx, session)
	}
	return ctx, nil
}

// RecoverTeam 恢复团队。
// Python: TeamAgent.recover_team()
func (a *TeamAgent) RecoverTeam(ctx context.Context) ([]string, error) {
	if a.recoveryManager != nil {
		return a.recoveryManager.RecoverTeam(ctx), nil
	}
	return nil, nil
}

// PersistSessionManifest 持久化恢复和清理所需的最小会话清单。
// Python: TeamAgent.persist_session_manifest(session)
func (a *TeamAgent) PersistSessionManifest(session any) {
	if a.recoveryManager != nil {
		if sf, ok := session.(sessinterfaces.SessionFacade); ok {
			a.recoveryManager.PersistLeaderConfig(sf)
		} else {
			logger.Warn(logComponent).Msg("PersistSessionManifest: session does not implement SessionFacade, skipping persistence")
		}
	}
}

// UpdateModelPool 更新模型池。
// Python: TeamAgent.update_model_pool(new_pool)
func (a *TeamAgent) UpdateModelPool(newPool []models.ModelPoolEntry) {
	// 对齐 Python: if self.role != TeamRole.LEADER: return
	if a.configurator != nil && a.configurator.Role() != atschema.TeamRoleLeader {
		return
	}
	if a.configurator != nil {
		a.configurator.UpdateModelPool(newPool)
	}
	// 持久化领导者配置（模型池变更后）
	if a.recoveryManager != nil && a.sessionManager != nil {
		// M-29: TeamSession 已是具体类型 *session.AgentTeamSession（实现 SessionFacade），无需断言
		if sf := a.sessionManager.TeamSession(); sf != nil {
			a.recoveryManager.PersistLeaderConfig(sf)
		}
	}
}

// AttachModelAllocator 附加模型分配器。
// Python: TeamAgent.attach_model_allocator(allocator, leader_allocation)
func (a *TeamAgent) AttachModelAllocator(allocator models.ModelAllocator, leaderAllocation *models.Allocation) {
	if a.configurator != nil {
		a.configurator.AttachModelAllocator(allocator, leaderAllocation)
	}
}

// RestoreAllocatorState 恢复分配器状态。
// Python: TeamAgent.restore_allocator_state(state)
func (a *TeamAgent) RestoreAllocatorState(state map[string]any) {
	if a.configurator != nil {
		a.configurator.RestoreAllocatorState(state)
	}
}

// RegisterRail 注册 Rail。
// Python: TeamAgent.register_rail(rail)
func (a *TeamAgent) RegisterRail(ctx context.Context, rail interfaces.AgentRail) (*TeamAgent, error) {
	if a.configurator != nil && a.configurator.Harness() != nil {
		if err := a.configurator.Harness().RegisterRail(ctx, rail); err != nil {
			return a, err
		}
	}
	return a, nil
}

// UnregisterRail 注销 Rail。
// Python: TeamAgent.unregister_rail(rail)
func (a *TeamAgent) UnregisterRail(ctx context.Context, rail interfaces.AgentRail) (*TeamAgent, error) {
	if a.configurator != nil && a.configurator.Harness() != nil {
		if err := a.configurator.Harness().UnregisterRail(ctx, rail); err != nil {
			return a, err
		}
	}
	return a, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionFromOptsOrCtx 返回本轮应绑定的会话对象（any，nil 表示无会话）。
// M-19: 对齐 Python invoke(inputs, session=None) / stream(inputs, session=None) 的
// 显式 session 参数语义——优先取 opts 传入的 SessionFacade；
// opts 未传时回退 ctx 中 SessionState 的 sessionID（对齐 Python contextvar 兜底）。
// 25.1a 接线：CoordinationKernel.Start 需要 AgentTeamSession 对象来启动
// harness 的 native supervisor（harness.Start(team_session=session) 会拿它建子会话）。
// 旧实现 sessionIDFromOptsOrCtx 只提取 sessionID，把对象丢了——TeamSession 存 nil，
// harness.Start 退化成"无团队会话"路径。此处保留对象（SessionFacade）透传。
func (a *TeamAgent) sessionFromOptsOrCtx(ctx context.Context, opts ...interfaces.AgentOption) any {
	if len(opts) > 0 {
		agentOpts := interfaces.NewAgentOptions(opts...)
		if agentOpts.Session != nil {
			return agentOpts.Session
		}
	}
	// ctx 里只有 sessionID（SessionState），没有会话对象；返回 nil 让 kernel 走解绑路径。
	// 有会话对象的调用方（team_runner 经 WithSession 传入）都会命中上方分支。
	return nil
}

// finalizeRoundDetached 执行轮次收尾清理，不受调用方 ctx 取消影响。
// S-30: 对齐 Python finally 块不受 cancellation 影响的语义——
// 用 context.WithoutCancel 派生清理 ctx，并加 30s 超时防止清理逻辑挂死。
// S-31: 统一 Invoke（defer）/Stream（goroutine 结束 + 空 queue 路径）两处调用。
func (a *TeamAgent) finalizeRoundDetached(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	a.coordination.FinalizeRound(cleanupCtx)
}

// requestCompletionPoll 轮次干净结束时请求立即完成轮询。
// S-33: 对齐 Python team_agent.py L660-676 _request_completion_poll——
// Leader + persistent 专属守卫，enqueue POLL_TASK 让上升沿完成检查立即触发，
// best-effort（事件总线缺失/停止时静默丢弃）。
func (a *TeamAgent) requestCompletionPoll(ctx context.Context) error {
	if a.Role() != atschema.TeamRoleLeader || a.Lifecycle() != "persistent" {
		return nil
	}
	a.coordination.Enqueue(types.CoordinationEvent{
		Inner: &types.InnerEventMessage{
			EventType: types.InnerEventTypePollTask,
		},
	})
	return nil
}

// updateExecution 更新执行状态。
// Python: TeamAgent._update_execution(status)
func (a *TeamAgent) updateExecution(ctx context.Context, status atschema.ExecutionStatus) error {
	if a.state.TeamMember != nil {
		_, err := a.state.TeamMember.UpdateExecutionStatus(ctx, status)
		return err
	}
	logger.Debug(logComponent).Str("member_name", a.MemberName()).
		Str("execution_status", string(status)).Msg("updating execution status (no TeamMember handle)")
	return nil
}

// registerTeamCompletionCallbacks 将 TeamSkillRail 的 notify_team_completed 注册到 TeamCompletionHandler。
// 对齐 Python: TeamAgent._register_team_completion_callbacks()
// Python: team_agent.py:447-467
func (a *TeamAgent) registerTeamCompletionCallbacks() {
	harness := a.Harness()
	// Python: if harness is None or dispatcher is None: return
	if harness == nil || a.coordination == nil || a.coordination.Dispatcher() == nil {
		return
	}
	teamCompletion := a.coordination.Dispatcher().TeamCompletion
	if teamCompletion == nil {
		return
	}
	// 对齐 Python: for rail_type in (TeamSkillEvolutionRail, TeamSkillCreateRail):
	for _, railType := range []reflect.Type{
		reflect.TypeOf((*evolution.TeamSkillEvolutionRail)(nil)),
		reflect.TypeOf((*evolution.TeamSkillCreateRail)(nil)),
	} {
		for _, railAny := range harness.FindRails(railType) {
			// TeamSkillEvolutionRail 的完成回调签名为 NotifyTeamCompleted(ctx) (bool, error)，此处包装成仅返回 error 的回调
			if evoRail, ok := railAny.(*evolution.TeamSkillEvolutionRail); ok {
				cb := func(ctx context.Context) error {
					_, err := evoRail.NotifyTeamCompleted(ctx)
					return err
				}
				teamCompletion.RegisterCompletionCallback(cb)
				continue
			}
			// TeamSkillCreateRail 的完成回调签名为 NotifyTeamCompleted(cbc *AgentCallbackContext) bool，无返回错误，包装为恒返回 nil 的回调
			if createRail, ok := railAny.(*evolution.TeamSkillCreateRail); ok {
				cb := func(ctx context.Context) error {
					createRail.NotifyTeamCompleted(nil)
					return nil
				}
				teamCompletion.RegisterCompletionCallback(cb)
				continue
			}
		}
	}
	logger.Debug(logComponent).Str("member_name", a.MemberName()).
		Msg("registerTeamCompletionCallbacks completed")
}

// registerReliabilityLocalSink 把 leader 的可靠性轨绑到进程内异常 sink。
//
// leader 自检的异常直送 ReliabilityHandler（绕过 messager self-filter——
// kernel 会丢弃 sender_id 等于本地成员的事件），其待发 route notices（leader
// 自己的模型组切换）以同样方式绑定：rail 把它们折进 leader 下一次被受理的输入。
// dispatcher 建好后调用一次，镜像 registerTeamCompletionCallbacks。多重守卫
// 对齐上游：reliability 未启用（dispatcher 无 reliability handler）或非 leader
// 成员（轨上无 local reporter）时 no-op。
//
// Python: TeamAgent._register_reliability_local_sink()（team_agent.py:807-838）
func (a *TeamAgent) registerReliabilityLocalSink() {
	harness := a.Harness()
	// Python: if harness is None or dispatcher is None: return
	if harness == nil || a.coordination == nil || a.coordination.Dispatcher() == nil {
		return
	}
	handler := a.coordination.Dispatcher().Reliability
	if handler == nil {
		return
	}
	// Python: for rail in harness.find_rails(ReliabilityRail):
	for _, railAny := range harness.FindRails(reflect.TypeOf((*reliability.ReliabilityRail)(nil))) {
		if rail, ok := railAny.(*reliability.ReliabilityRail); ok {
			rail.BindLocalSink(func(ctx context.Context, anomaly *reliabilitycore.Anomaly) {
				handler.HandleLocalAnomaly(ctx, anomaly)
			})
			rail.BindRouteNoticeSource(handler.TakePendingRouteNotices)
		}
	}
	logger.Debug(logComponent).Str("member_name", a.MemberName()).
		Msg("registerReliabilityLocalSink completed")
}

// ── KernelHost 接口适配方法 ──

// SessionController 返回会话控制器。
// 满足 types.SessionAccessor 接口。
// *SessionManager 直接满足 types.SessionController（编译期检查）。
func (a *TeamAgent) SessionController() types.SessionController {
	if a.sessionManager == nil {
		return nil
	}
	return a.sessionManager
}

// TeamBackendAccessor 返回 TeamBackend 访问器。
// 满足 types.InfraAccessor 接口。
// *tools.TeamBackend 直接满足 types.TeamBackendAccessor（编译期检查）。
// 显式判空后再返回：直接 return a.TeamBackend() 会把 nil *TeamBackend 装进
// 接口，调用方 backendAccessor != nil 判定为真，随后解引用 nil 接收者 panic
// （typed-nil 陷阱）。Python 侧属性为 None 时判空天然成立。
func (a *TeamAgent) TeamBackendAccessor() types.TeamBackendAccessor {
	tb := a.TeamBackend()
	if tb == nil {
		return nil
	}
	return tb
}

// WorkspaceManager 返回工作空间管理器。
// 满足 types.InfraAccessor 接口。
// *team_workspace.TeamWorkspaceManager 直接满足 types.WorkspaceAccessor（编译期检查）。
func (a *TeamAgent) WorkspaceManager() types.WorkspaceAccessor {
	if a.configurator == nil {
		return nil
	}
	// 防御 typed-nil：configurator.WorkspaceManager() 返回 *TeamWorkspaceManager
	// 具体类型，nil 时赋值给 WorkspaceAccessor 接口会得到非 nil 接口值，
	// 下游 kernel.Start 的 `wsMgr != nil` 判真后调用 Initialize 会 nil receiver panic。
	if m := a.configurator.WorkspaceManager(); m != nil {
		return m
	}
	return nil
}

// SetWorkspaceInitialized 标记工作空间已初始化。
// 满足 types.InfraAccessor 接口。
func (a *TeamAgent) SetWorkspaceInitialized() {
	if a.configurator != nil {
		a.configurator.SetWorkspaceInitialized(true)
	}
}

// IsWorkspaceInitialized 工作空间是否已初始化。
// 满足 types.InfraAccessor 接口。
// S-36: 对齐 Python kernel.py L147 `infra.workspace_initialized` 守卫检查。
func (a *TeamAgent) IsWorkspaceInitialized() bool {
	if a.configurator != nil {
		return a.configurator.WorkspaceInitialized()
	}
	return false
}

// MemoryManager 返回记忆管理器。
// 满足 types.ResourceAccessor 接口。
// 直接返回 *memory.TeamMemoryManager 具体类型，无需适配器。
func (a *TeamAgent) MemoryManager() *memory.TeamMemoryManager {
	if a.configurator == nil {
		return nil
	}
	return a.configurator.MemoryManager()
}

// HarnessAccessor 返回 Harness 访问器。
// 满足 types.ResourceAccessor 接口。
// *harness.TeamHarness 直接满足 types.HarnessAccessor（编译期检查）。
func (a *TeamAgent) HarnessAccessor() types.HarnessAccessor {
	// 15.16b：外部 CLI 成员——MemberRuntime 即大脑（Python: configurator.harness
	// 联合承载 ExternalCliRuntime，kernel 经同一面 start/stop）。Go 以适配器
	// 把 MemberRuntime 装进 HarnessAccessor 面；rail/记忆钩子在适配器上 no-op
	//（configurator 对外部成员不调它们，member_runtime.go 头注同源）。
	if mr := a.MemberRuntime(); mr != nil {
		return &memberRuntimeAccessor{runtime: mr}
	}
	// 防御 typed-nil：a.Harness() 返回 *harness.TeamHarness 具体类型，
	// nil 时赋值给 HarnessAccessor 接口会得到非 nil 接口值。
	if h := a.Harness(); h != nil {
		return h
	}
	return nil
}

// memberRuntimeAccessor 把外部成员 MemberRuntime 适配成 kernel 的
// HarnessAccessor 面。Start/Stop/State 直转；rail/记忆/paused-query 钩子
// no-op（外部 CLI 成员的 DeepAgent rails 不适用）。
type memberRuntimeAccessor struct {
	runtime MemberRuntime
}

// RegisterMemberTools no-op（外部成员无本地 rail 工具面）。
func (a *memberRuntimeAccessor) RegisterMemberTools(*memory.TeamMemoryManager) {}

// InjectMemberMemory no-op（外部成员无记忆注入）。
func (a *memberRuntimeAccessor) InjectMemberMemory(context.Context, *memory.TeamMemoryManager, string) error {
	return nil
}

// Model no-op 返回 nil（外部成员无本地模型对象）。
func (a *memberRuntimeAccessor) Model() *llm.Model { return nil }

// Start 转发 MemberRuntime.Start(teamSession)。
// Python: ExternalCliRuntime.start(team_session=session)。
func (a *memberRuntimeAccessor) Start(_ context.Context, teamSession any) error {
	return a.runtime.Start(teamSession)
}

// Stop 转发 MemberRuntime.Stop()。
func (a *memberRuntimeAccessor) Stop(context.Context) error {
	a.runtime.Stop()
	return nil
}

// PausedQuery 外部成员无暂停轮（Python ExternalCliRuntime 无 paused_query）。
func (a *memberRuntimeAccessor) PausedQuery() (string, bool) { return "", false }

// State 转发生命周期相位。
func (a *memberRuntimeAccessor) State() harnessprotocol.HarnessState { return a.runtime.State() }

// Dispose 转发 Stop（外部成员无进程级资源回收面）。
func (a *memberRuntimeAccessor) Dispose(context.Context) error {
	a.runtime.Stop()
	return nil
}

// SubscribeTransport 订阅团队传输主题。
// 满足 types.TransportAccessor 接口。
// 修复 S-10: 对齐 Python subscribe_transport 完整实现 (kernel.py:330-356)
//
// 步骤：
// 1. 注册 direct_message_handler（将点对点消息推入 eventBus）
// 2. 遍历 TeamTopic 订阅（team/task/message），使用 self-filter 回调
// 3. self-filter: 先触发 event_listeners，然后跳过自己发出的事件，非自身事件推入 eventBus
func (a *TeamAgent) SubscribeTransport(ctx context.Context, teamName string) error {
	if a.configurator == nil {
		return nil
	}
	mgr := a.configurator.Messager()
	if mgr == nil {
		return nil
	}
	if teamName == "" {
		return nil
	}
	sessionID := agentteams.GetSessionID(ctx)

	// 对齐 Python: messager.register_direct_message_handler(self._event_bus.enqueue)
	if a.coordination != nil && a.coordination.EventBus() != nil {
		bus := a.coordination.EventBus()
		dmHandler := func(ctx context.Context, msg *atevents.EventMessage) error {
			bus.Enqueue(types.CoordinationEvent{Transport: msg})
			return nil
		}
		_ = mgr.RegisterDirectMessageHandler(ctx, dmHandler)
	}

	// 对齐 Python: for topic in TeamTopic:
	//   topic_str = topic.build(session_id, team_name)
	//   await messager.subscribe(topic_str, _filter_self)
	//   self._subscribed_topics.append(topic_str)
	localMemberName := a.MemberName()
	for _, topic := range atevents.AllTeamTopics {
		topicID := topic.Build(sessionID, teamName)
		// 对齐 Python: _filter_self 回调
		handler := func(ctx context.Context, event *atevents.EventMessage) error {
			// Python: for listener in host.state.event_listeners: await listener(event)
			for _, handle := range a.state.EventListeners {
				if handle != nil && handle.Handler() != nil {
					_ = handle.Handler()(ctx, event)
				}
			}
			// Python: if local_member_name and event.sender_id == local_member_name: return
			if localMemberName != "" && event.SenderID == localMemberName {
				// F_62: 调度器必须观察到 leader 进程自己执行的板面变更
				//（create_task、settle）。协调层不得重处理 echo，被丢弃的事件
				// 降级为同一总线循环上的裸「板面已变」扫描提示。
				// Python: kernel.py L646-658
				if a.coordination != nil && a.coordination.EventBus() != nil {
					if sched := a.coordination.Scheduler(); sched != nil && sched.IsActive() &&
						len(event.EventType) >= 5 && event.EventType[:5] == "task_" {
						a.coordination.EventBus().Enqueue(types.CoordinationEvent{Inner: &types.InnerEventMessage{
							EventType: types.InnerEventTypeSchedulerScan,
						}})
					}
				}
				return nil
			}
			// Python: await self._event_bus.enqueue(event)
			if a.coordination != nil && a.coordination.EventBus() != nil {
				a.coordination.EventBus().Enqueue(types.CoordinationEvent{Transport: event})
			}
			return nil
		}
		_ = mgr.Subscribe(ctx, topicID, handler)
		// 跟踪已订阅的主题
		if a.coordination != nil {
			a.coordination.AddSubscribedTopic(topicID)
		}
	}
	return nil
}

// UnsubscribeTransport 取消订阅。
// 满足 types.TransportAccessor 接口。
// 修复 S-11: 对齐 Python unsubscribe_transport 完整实现
//
// 步骤：
// 1. 取消注册 direct_message_handler
// 2. 逐个取消订阅所有已订阅的 topics
// 3. 清空 subscribedTopics
// 4. 传递 ctx 而非 context.Background()
func (a *TeamAgent) UnsubscribeTransport(ctx context.Context) error {
	if a.configurator == nil {
		return nil
	}
	mgr := a.configurator.Messager()
	if mgr == nil {
		return nil
	}
	// 对齐 Python: messager.unregister_direct_message_handler()
	_ = mgr.UnregisterDirectMessageHandler(ctx)

	// 对齐 Python: for topic in self._subscribed_topics: await messager.unsubscribe(topic)
	if a.coordination != nil {
		for _, topicID := range a.coordination.SubscribedTopics() {
			_ = mgr.Unsubscribe(ctx, topicID)
		}
		// 对齐 Python: self._subscribed_topics.clear()
		a.coordination.ClearSubscribedTopics()
	}
	return nil
}

// PublishTeamEvent 发布团队事件到 TEAM 主题。
// 满足 types.TransportAccessor 接口。
// 对齐 Python: messager.publish(TeamTopic.TEAM.build(session_id, team_name), EventMessage.from_event(event))
func (a *TeamAgent) PublishTeamEvent(ctx context.Context, eventType string, payload map[string]any) error {
	if a.configurator == nil {
		return nil
	}
	mgr := a.configurator.Messager()
	if mgr == nil {
		return nil
	}
	teamName := a.TeamName()
	if teamName == "" {
		return nil
	}
	// 对齐 Python: TeamTopic.TEAM.build(get_session_id(), team_name)
	sessionID := agentteams.GetSessionID(ctx)
	topicID := atevents.TeamTopicTeam.Build(sessionID, teamName)
	// 对齐 Python: EventMessage.from_event(TeamStandbyEvent(team_name=team_name))
	em := &atevents.EventMessage{
		EventType: eventType,
		Payload:   payload,
	}
	return mgr.Publish(ctx, topicID, em)
}

// DrainAgentTask 等待当前 agent round 完成。
// 满足 types.LifecycleAccessor 接口。
// 对齐 Python: drain_agent_task()
func (a *TeamAgent) DrainAgentTask(ctx context.Context) {
	if a.streamController != nil {
		_ = a.streamController.DrainAgentTask(ctx)
	}
}

// MarkLiveTeammates 标记所有活跃成员为指定状态。
// 满足 types.LifecycleAccessor 接口。
// 对齐 Python: _mark_live_teammates(status) (kernel.py:232-276)
//
// 仅标记 spawned_handles 中存在的成员，跳过 leader 自身、UNSTARTED 和 SHUTDOWN 成员。
func (a *TeamAgent) MarkLiveTeammates(ctx context.Context, status string) error {
	backend := a.TeamBackend()
	if backend == nil {
		return nil
	}
	members, err := backend.ListMembers(ctx)
	if err != nil {
		return err
	}
	// 修复 S-08: 对齐 Python 过滤逻辑
	// Python: spawned = set(host.spawn_manager.spawned_handles.keys())
	spawnedNames := make(map[string]bool)
	for _, name := range a.SpawnedHandleNames() {
		spawnedNames[name] = true
	}
	leaderName := a.MemberName()
	for _, m := range members {
		// Python: if member.member_name == leader: continue
		if m.MemberName == leaderName {
			continue
		}
		// Python: if member.member_name not in spawned: continue
		if !spawnedNames[m.MemberName] {
			continue
		}
		// Python: if current in {MemberStatus.UNSTARTED, MemberStatus.SHUTDOWN}: continue
		current := atschema.MemberStatus(m.Status)
		if current == atschema.MemberStatusUnstarted || current == atschema.MemberStatusShutdown {
			continue
		}
		backend.DB().Member().UpdateMemberStatus(ctx, m.MemberName, backend.TeamName(), status)
	}
	return nil
}

// CloseStream 关闭流（向 streamQueue 写 nil sentinel 结束读取循环）。
// 满足 types.LifecycleAccessor 接口。
// 对齐 Python: _close_stream → coordination.close_stream → stream_controller.close_stream
// （put_nowait(None)，非阻塞写 nil sentinel）。
// 注意：不得 close(channel)——forwardOutputs 可能仍在该 channel 上 send，
// close 与 send 并发会 panic/race；正确语义是写 nil sentinel 让读取方退出。
func (a *TeamAgent) CloseStream() {
	if a.streamController != nil {
		a.streamController.CloseStream()
	}
}

// SetMemberID 设置成员 ID 上下文。
// 满足 types.LifecycleAccessor 接口。
// 对齐 Python: set_member_id(member_name) — Python 用 ContextVar，Go 用 context.WithValue
func (a *TeamAgent) SetMemberID(ctx context.Context, name string) context.Context {
	memberName := name
	if memberName == "" {
		memberName = a.MemberName()
	}
	logger.Debug(logComponent).Str("member_name", memberName).Msg("SetMemberID")
	return sessionctx.WithMemberID(ctx, memberName)
}

// CancelRecoveryTasks 取消所有恢复任务。
// 满足 types.LifecycleAccessor 接口。
// 对齐 Python: host.spawn_manager.cancel_recovery_tasks()
func (a *TeamAgent) CancelRecoveryTasks() {
	if a.spawnManager != nil {
		a.spawnManager.CancelRecoveryTasks()
	}
}

// ShutdownAllHandles 关闭所有已生成的句柄。
// 满足 types.LifecycleAccessor 接口。
// 对齐 Python: host.spawn_manager.shutdown_all_handles()
func (a *TeamAgent) ShutdownAllHandles(ctx context.Context) {
	if a.spawnManager != nil {
		a.spawnManager.ShutdownAllHandles(ctx)
	}
}

// SpawnedHandleNames 返回已生成的句柄成员名集合。
// 满足 types.LifecycleAccessor 接口。
// 对齐 Python: host.spawn_manager.spawned_handles.keys()
func (a *TeamAgent) SpawnedHandleNames() []string {
	if a.spawnManager == nil {
		return nil
	}
	return a.spawnManager.SpawnedHandleNames()
}

// SpecAny 返回 TeamAgentSpec（any 类型）。
// 满足 types.BlueprintAccessor 接口。
// 与 Spec() *atschema.TeamAgentSpec 签名不同，避免冲突。
func (a *TeamAgent) SpecAny() any {
	return a.Spec()
}

// DispatcherBlueprint 返回 Dispatcher 使用的 blueprint。
// M-02: 对齐 Python kernel.setup() 从 self._host.blueprint 取值。
// 未 configure 或 blueprint 未就绪时返回 true-nil 接口（而非 typed-nil），
// 否则 Setup 的 `bp == nil` 守卫会被绕过。
func (a *TeamAgent) DispatcherBlueprint() types.DispatcherBlueprint {
	if a.configurator == nil {
		return nil
	}
	bp := a.configurator.Blueprint()
	if bp == nil {
		return nil
	}
	return bp
}

// DispatcherInfra 返回 Dispatcher 使用的 infra。
// M-02: 对齐 Python kernel.setup() 从 self._host.infra 取值（同样规避 typed-nil）。
func (a *TeamAgent) DispatcherInfra() types.DispatcherInfra {
	if a.configurator == nil {
		return nil
	}
	inf := a.configurator.Infra()
	if inf == nil {
		return nil
	}
	return inf
}

// isTaskBoardSettled 团队任务板是否没有开放任务（team-idle 判定的后半）。
// 成员停了只说明「此刻」没人动——交接中的团队同样满足。每条任务都终态（完成
// 或取消）或板面为空，才让安静意味着「没有东西可动」。
// 一条聚合 COUNT，不是全量任务加载——它在每个撑过去抖窗口的 idle 边上跑。
// 无 backend 的成员没有板，其板平凡静止；数据库错误答 False——只是把标记
// 扣住，而不是宣告本进程无法确认的空闲。
// Python: TeamAgent.is_task_board_settled（team_agent.py L1283-1307）
func (a *TeamAgent) isTaskBoardSettled(ctx context.Context) (bool, error) {
	backend := a.TeamBackend()
	if backend == nil {
		return true, nil
	}
	_, nonTerminal, err := backend.DB().Task().CountTasksTerminality(ctx, backend.TeamName())
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("member_name", a.MemberName()).
			Msg("failed to probe the task board for team idleness")
		return false, nil
	}
	return nonTerminal == 0, nil
}

// SeedMemberRegistry 每轮启动前从团队数据库重建 leader 的活动视图基线。
// 冷启动、resume、recover 都从数据库实际持有的状态开始，而不是上一轮内存里
// 留下的。未 build 的团队或没有成员行的团队种出空名册——普通状态而非错误，
// leader 自己无论如何保留在名册里。
// Python: TeamAgent.seed_member_registry（team_agent.py L1309-1334）
func (a *TeamAgent) SeedMemberRegistry(ctx context.Context) {
	registry := a.state.MemberRegistry
	backend := a.TeamBackend()
	if registry == nil || backend == nil {
		return
	}
	rows, err := backend.DB().Member().GetTeamMembers(ctx, backend.TeamName(), "")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("member_name", a.MemberName()).
			Msg("failed to seed member activity registry")
		return
	}
	seeded := make(map[string]atschema.MemberStatus, len(rows))
	for _, row := range rows {
		if status, ok := atschema.ParseMemberStatus(row.Status); ok {
			seeded[row.MemberName] = status
		}
	}
	registry.Seed(seeded)
}

// IdleSeconds 本成员连续 idle 的秒数；未 idle 返回 nil。
// Python: TeamAgent.idle_seconds（team_agent.py L505-517）
func (a *TeamAgent) IdleSeconds() *float64 {
	if a.state.IdleSince.IsZero() {
		return nil
	}
	secs := time.Since(a.state.IdleSince).Seconds()
	return &secs
}

// RefreshIdleBaseline 重置 idle 时钟基线，使 pause 窗口不被计为 idle。
// 团队 pause 时已经 idle 的成员，其 idle_since 打点在单调时钟跨越整个 pause
// 窗口期间保持不动——而它没有挂起的轮可续，永远不会重进 IDLE 重新打点。
// 不重置基线的话，resume 后第一次轮询会看到一段凭空捏造的超长 idle。
// busy 成员持有零值，不动；冷启动也是零值，天然 no-op。
// Python: TeamAgent.refresh_idle_baseline（team_agent.py L522-535）
func (a *TeamAgent) RefreshIdleBaseline() {
	if !a.state.IdleSince.IsZero() {
		a.state.IdleSince = time.Now()
	}
}

// ──────────────────────────── 25.5 fork 半区：命名 checkpoint 索引 ────────────────────────────

// SetCheckpoint 存命名 checkpoint：名字团队全局唯一，重名返回既有记录
// （nil=成功）。leader 侧把全量映射镜像进 bound team session 的 per-team
// 命名空间以扛进程重启——持久化随运行循环的 post_run 落盘（无显式 flush），
// 对齐 allocator / lifecycle / pending_resume 语义。
// 返回 map 形态（与 storeCheckpointFn 回调签名一致）：nil 成功；冲突返回
// {count, description, created_by}。
// Python: set_checkpoint (team_agent.py:1774-1797)
func (a *TeamAgent) SetCheckpoint(name string, count int, description, createdBy string) map[string]any {
	a.namedCheckpointsMu.Lock()
	defer a.namedCheckpointsMu.Unlock()
	if a.namedCheckpoints == nil {
		a.namedCheckpoints = make(map[string]metadata.CheckpointRecord)
	}
	if existing, ok := a.namedCheckpoints[name]; ok {
		return map[string]any{
			"count":       existing.Count,
			"description": existing.Description,
			"created_by":  existing.CreatedBy,
		}
	}
	a.namedCheckpoints[name] = metadata.CheckpointRecord{
		Count:       count,
		Description: description,
		CreatedBy:   createdBy,
	}
	if a.Role() == atschema.TeamRoleLeader {
		a.mergeCheckpointsIntoSession()
	}
	return nil
}

// mergeCheckpointsIntoSession 把当前索引表全量镜像进 bound team session 的
// per-team 命名空间（TEAM_CHECKPOINTS_KEY）。走 persistTeamDbState 同款通道。
// Python: _merge_checkpoints_into_session (team_agent.py:1799-1813)
func (a *TeamAgent) mergeCheckpointsIntoSession() {
	teamSession := a.sessionManager.TeamSession()
	teamName := a.TeamName()
	if teamSession == nil || teamName == "" {
		return
	}
	snapshot := a.checkpointsSnapshot()
	metadata.MergeTeamCheckpoints(teamSession, teamName, snapshot)
}

// checkpointsSnapshot 返回索引表的只读快照（RLock 拷贝）。
// 作为 TeamBackend.listCheckpointFn 回调注入（Python: set_checkpoint_list_fn）。
func (a *TeamAgent) checkpointsSnapshot() map[string]metadata.CheckpointRecord {
	a.namedCheckpointsMu.RLock()
	defer a.namedCheckpointsMu.RUnlock()
	out := make(map[string]metadata.CheckpointRecord, len(a.namedCheckpoints))
	for name, rec := range a.namedCheckpoints {
		out[name] = rec
	}
	return out
}

// checkpointNamesSnapshot 返回索引表名字快照（通知 leader 时列 available 用）。
func (a *TeamAgent) checkpointNamesSnapshot() []string {
	snapshot := a.checkpointsSnapshot()
	names := make([]string, 0, len(snapshot))
	for name := range snapshot {
		names = append(names, name)
	}
	return names
}

// setCheckpointsFrom 整表覆盖索引表（冷恢复还原 / share 用）。
// Python: set_checkpoints_from (team_agent.py:1815-1822)
func (a *TeamAgent) setCheckpointsFrom(source map[string]metadata.CheckpointRecord) {
	a.namedCheckpointsMu.Lock()
	defer a.namedCheckpointsMu.Unlock()
	a.namedCheckpoints = make(map[string]metadata.CheckpointRecord, len(source))
	for name, rec := range source {
		a.namedCheckpoints[name] = rec
	}
}

// ShareCheckpointsWith 把 leader 的索引表共享给另一个 TeamAgent（in-process
// 队友复用同一份权威映射）。
// Python: share_checkpoints_with (team_agent.py:1699-1701)
func (a *TeamAgent) ShareCheckpointsWith(other *TeamAgent) {
	if other == nil {
		return
	}
	other.setCheckpointsFrom(a.checkpointsSnapshot())
}

// PersistedContextMessages 读取绑定会话持久化的上下文消息（fork 抓取兜底）。
// context engine 每轮把 ModelContext.SaveState()（含 messages）写入绑定会话
// state 的 "context" 命名空间（default_context_id），pre_run 的 checkpoint 恢复
// 也吃这份——内存池为空时它是 fork 抓取的持久事实源。
// Python: _read_persisted_messages (fork.py:120-148)
func (a *TeamAgent) PersistedContextMessages() []llmschema.BaseMessage {
	teamSession := a.sessionManager.TeamSession()
	if teamSession == nil {
		return nil
	}
	raw, err := teamSession.GetState(state.StringKey("context"))
	if err != nil || raw == nil {
		return nil
	}
	stateMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	ctxState, ok := stateMap["default_context_id"].(map[string]any)
	if !ok {
		return nil
	}
	values, ok := ctxState["messages"].([]any)
	if !ok {
		return nil
	}
	// Python: cls.normalize_messages(messages)——畸形输入整体退化 nil
	return agentteams.NormalizeForkMessageValues(values)
}

// onTeammateCreated 队友创建回调。
// Python: TeamAgent._on_teammate_created(teammate_id)（team_agent.py:1480-1662）
//
// 步骤顺序对齐 Python：先解析 fork 上下文（含五模式分派），再
// build_context_from_db，fork 命中时回填 ctx.fork_source（身份块转换公告），
// 最后 spawn。Go 的 SpawnTeammate 以 forkFrom 形参承载 Python 的 fork_from=。
func (a *TeamAgent) onTeammateCreated(ctx context.Context, teammateID string) error {
	// ── Resolve fork context ──（Python: team_agent.py:1484-1491）
	var forkInfo *tools.ForkInfo
	if backend := a.TeamBackend(); backend != nil {
		forkInfo = backend.ConsumeForkOnSpawn(teammateID)
		logger.Debug(logComponent).
			Str("member", teammateID).
			Any("fork_info", forkInfo).
			Str("checkpoints", strings.Join(a.checkpointNamesSnapshot(), ",")).
			Str("spawned_handles", strings.Join(a.spawnManager.SpawnedHandleNames(), ",")).
			Msg("[fork] onTeammateCreated")
	}

	// Python: team_agent.py:1492-1622——五模式分派构造 ForkContext
	var forkCtx *agentteams.ForkContext
	if forkInfo != nil {
		forkCtx = a.buildForkContext(ctx, teammateID, forkInfo)
	}

	runtimeCtx, err := a.spawnManager.BuildContextFromDB(ctx, teammateID)
	if err != nil || runtimeCtx.MemberName == "" {
		return err
	}
	// Python: team_agent.py:1627-1636——记录继承来源，供身份块转换公告
	//（TeamContextTracker 渲染 <identity-conversion>）。空 fork 上下文
	//（抓取没拿到消息）意味着什么都没继承，不记公告。
	if forkCtx != nil && !forkCtx.IsEmpty() {
		source := forkInfo.Source
		if source == "" {
			source = a.MemberName()
		}
		runtimeCtx.ForkSource = source
	}
	teammate, _ := a.TeamBackend().GetMember(ctx, teammateID)
	prompt := ""
	if teammate != nil {
		prompt = teammate.Prompt
	}
	return a.SpawnTeammate(ctx, runtimeCtx, prompt, a.SessionID(ctx), nil, forkCtx)
}

// buildForkContext 按 fork_info 构造继承上下文（五模式分派）。
// 整段异常兜底：warning + nil（无继承继续 spawn）——fork 抓取失败绝不阻塞 spawn。
// Python: team_agent.py:1494-1622
func (a *TeamAgent) buildForkContext(ctx context.Context, teammateID string, forkInfo *tools.ForkInfo) (forkCtx *agentteams.ForkContext) {
	// Python: try: ... except Exception: warning + fork_ctx = None（:1618-1622）
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("member", teammateID).
				Any("panic", r).
				Msg("[fork] fork capture failed; spawning without inherited context")
			forkCtx = nil
		}
	}()

	native := a.resolveForkSource(forkInfo.Source)
	logger.Debug(logComponent).
		Str("source", forkInfo.Source).
		Bool("native_resolved", native != nil).
		Msg("[fork] resolve_fork_source")
	if native == nil {
		return nil
	}

	forkValue := forkInfo.Fork
	// Python: is_named = isinstance(fork_value, str) and fork_value not in ("true", "false")（:1506）
	forkStr, isStr := forkValue.(string)
	isNamed := isStr && forkStr != "true" && forkStr != "false"
	// Python: 默认 mode——live→full；named→before（:1508-1509）
	forkMode := forkInfo.ForkMode
	if forkMode == "" {
		if isNamed {
			forkMode = "before"
		} else {
			forkMode = "full"
		}
	}

	// 命名 checkpoint 索引查找
	var ckptRecord *metadata.CheckpointRecord
	var ckptIdx *int
	if isNamed {
		snapshot := a.checkpointsSnapshot()
		if rec, ok := snapshot[forkStr]; ok {
			r := rec
			ckptRecord = &r
			idx := rec.Count
			ckptIdx = &idx
		}
	}

	// Python: mode 是否消费下标（:1514-1518）
	modeUsesCkpt := forkMode == "before" || forkMode == "after" ||
		forkMode == "keep_before_compact_after" || forkMode == "keep_after_compact_before"

	// Python: creator/fork_source 不匹配（:1519-1545）——下标只对创建者的上下文
	// 有意义，错源时清空下标降级 full 并通知 leader。
	sourceName := forkInfo.Source
	if sourceName == "" {
		sourceName = a.MemberName()
	}
	if isNamed && modeUsesCkpt && ckptRecord != nil {
		creator := ckptRecord.CreatedBy
		if creator != "" && creator != sourceName {
			logger.Warn(logComponent).
				Str("checkpoint", forkStr).
				Str("creator", creator).
				Str("fork_source", sourceName).
				Msg("[fork] checkpoint created by another member; falling back to full context")
			ckptIdx = nil
			detail, _ := agentteams.T("checkpoint.fork_source_mismatch",
				map[string]any{"creator": creator, "source": sourceName})
			a.notifyForkNameNotFound(ctx, teammateID, forkStr, detail)
		}
	}

	// Python: fork_ctx = ForkContext.from_agent(native)（:1549）——模式分支前
	// 先全量抓取；compact 模式靠 compact_split 让 split 下标对齐注入上下文。
	forkCtx = agentteams.ForkContextFromAgent(ctx, native, nil, "before")

	if !isNamed {
		// live fork：唯一有意义的 mode 是 full
		if forkMode != "full" {
			logger.Warn(logComponent).
				Str("fork_mode", forkMode).
				Str("member", teammateID).
				Msg("[fork] fork_mode ignored for live fork; using full context")
		}
	} else if forkMode == "full" {
		// named + full：忽略下标（Python :1568-1570 pass）
	} else if ckptIdx == nil {
		if ckptRecord == nil {
			// 未知名：warning + 通知 + full 兜底（creator 不匹配也已通知过）
			logger.Warn(logComponent).
				Str("checkpoint", forkStr).
				Str("member", teammateID).
				Msg("[fork] checkpoint not found; falling back to full context")
			a.notifyForkNameNotFound(ctx, teammateID, forkStr, "")
		}
	} else {
		// Python: :1571-1596
		switch forkMode {
		case "before":
			forkCtx = agentteams.ForkContextFromAgent(ctx, native, ckptIdx, "before")
		case "after":
			forkCtx = agentteams.ForkContextFromAgent(ctx, native, ckptIdx, "after")
		case "keep_before_compact_after":
			// Python: fork_ctx.compact_split=ckpt_idx; compact_direction="after"
			forkCtx.CompactSplit = ckptIdx
			forkCtx.CompactDirection = "after"
		case "keep_after_compact_before":
			forkCtx.CompactSplit = ckptIdx
		default:
			logger.Warn(logComponent).
				Str("fork_mode", forkMode).
				Str("member", teammateID).
				Msg("[fork] unknown fork_mode; using full context")
		}
	}

	kind := "live fork"
	if forkCtx.CompactSplit != nil {
		kind = "compacted fork"
	} else if isNamed && forkMode != "full" {
		kind = "checkpoint fork"
	}
	logger.Info(logComponent).
		Str("kind", kind).
		Str("member", teammateID).
		Int("msgs", len(forkCtx.Messages)).
		Msg("[fork] context inheritance resolved")
	return forkCtx
}

// resolveForkSource 返回源成员的上下文读取面；nil=无法解析（跳过继承）。
//   - source 为空/等于自己 → 本 agent（leader 视角）
//   - spawned_handles[source] 命中 → 队友的 DeepAgent 读取面
//   - 未命中/非 in-process（外部 CLI）→ warning + nil，继承整体跳过
//
// Python: _resolve_fork_native (team_agent.py:1644-1661)
func (a *TeamAgent) resolveForkSource(sourceName string) spawnpkg.ForkContextSource {
	if sourceName == "" || sourceName == a.MemberName() {
		return a
	}
	handleAny := a.spawnManager.SpawnedHandles()[sourceName]
	handle, ok := handleAny.(*spawnpkg.InProcessSpawnHandle)
	if !ok {
		logger.Warn(logComponent).
			Str("fork_source", sourceName).
			Str("leader", a.MemberName()).
			Str("spawned", strings.Join(a.spawnManager.SpawnedHandleNames(), ",")).
			Msg("[fork] fork source not found or not in-process; fork inheritance will be SKIPPED")
		return nil
	}
	src := handle.ForkSource()
	if src == nil {
		logger.Warn(logComponent).
			Str("fork_source", sourceName).
			Str("leader", a.MemberName()).
			Str("spawned", strings.Join(a.spawnManager.SpawnedHandleNames(), ",")).
			Msg("[fork] fork source not found or not in-process; fork inheritance will be SKIPPED")
	}
	return src
}

// notifyForkNameNotFound 把错误的 fork checkpoint 名告知 leader。
// spawn 照常以全量兜底继续，但 leader 被告知请求了哪个名字、实际有哪些名字
// ——命名错配（未知名或 creator 与 fork_source 不匹配）不再静默。
// best-effort：发送失败 warning，绝不阻塞 spawn。
// Python: _notify_fork_name_not_found (team_agent.py:1664-1683)
func (a *TeamAgent) notifyForkNameNotFound(ctx context.Context, member, forkName, detail string) {
	available := strings.Join(a.checkpointNamesSnapshot(), ", ")
	if available == "" {
		available = "（无）"
	}
	content, err := agentteams.T("checkpoint.fork_not_found", map[string]any{
		"fork":      forkName,
		"member":    member,
		"available": available,
		"detail":    detail,
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[checkpoint] fork_not_found 文案渲染失败")
		return
	}
	mgr := a.MessageManager()
	if mgr == nil {
		return
	}
	if _, err := mgr.SendMessage(ctx, content, a.MemberName(), ""); err != nil {
		logger.Warn(logComponent).
			Str("checkpoint", forkName).
			Err(err).
			Msg("[fork] failed to notify leader about missing checkpoint")
	}
}

// markTeamCleaned 团队清理回调。
// Python: TeamAgent._mark_team_cleaned()
func (a *TeamAgent) markTeamCleaned(ctx context.Context) error {
	a.persistTeamDbState(ctx, "cleaned")
	a.state.TeamCleaned = true
	return nil
}

// markTeamBuilt 团队构建回调。
// Python: TeamAgent._mark_team_built()
func (a *TeamAgent) markTeamBuilt(ctx context.Context) error {
	a.persistTeamDbState(ctx, "created")
	// F_62: build_team 物化团队后武装调度器——任何 teammate spawn 或任务创建
	// 之前就活（对齐提示词承诺的「框架会启动承担者」）。无调度器时 no-op。
	// Python: on_team_built 钩子 → CoordinationKernel.notify_team_built（kernel.py L314-326）
	if a.coordination != nil {
		a.coordination.NotifyTeamBuilt(ctx)
	}
	return nil
}

// persistTeamDbState 持久化团队 DB 生命周期状态到当前检查点。
// Python: TeamAgent._persist_team_db_state(db_state)
func (a *TeamAgent) persistTeamDbState(ctx context.Context, dbState string) {
	// M-29: TeamSession 已是具体类型 *session.AgentTeamSession（实现 SessionFacade 且自带 FlushCheckpoint），无需断言
	teamSession := a.sessionManager.TeamSession()
	teamName := a.TeamName()
	if teamSession == nil || teamName == "" {
		return
	}
	// Python: metadata.MergeTeamDBState(session, team_name, db_state)
	metadata.MergeTeamDBState(teamSession, teamName, dbState)
	_ = teamSession.FlushCheckpoint(ctx)
}

// removeSelfFromPool 从运行时池中移除自身。
// Python: TeamAgent._remove_self_from_pool()
func (a *TeamAgent) removeSelfFromPool(ctx context.Context, sessionID string) {
	teamName := a.TeamName()
	if teamName == "" || sessionID == "" {
		return
	}
	// 通过 runner 获取 PoolAccessor 接口（registry.PoolAccessor 类型，无需断言）
	mgr := runner.GetTeamRuntimeManager()
	if mgr == nil {
		return
	}
	pool := mgr.PoolReader()
	if pool == nil {
		return
	}
	// 检查 session ID 匹配后移除
	if pool.GetSessionIDForTeam(teamName) != sessionID {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("team_name", teamName).
				Any("panic", r).
				Msg("removeSelfFromPool panic recovered")
		}
	}()
	pool.RemoveTeam(teamName)
}

// TaskEventTracker 返回本成员的任务通知器。
func (a *TeamAgent) TaskEventTracker() tracker.TaskEventTracker {
	return a.taskEventTracker
}

// SetTaskEventTracker 设置任务通知器（装配期由 configurator 调用）。
//
// 同时写到 configurator 的资源区，供 rail 构造时借用。
func (a *TeamAgent) SetTaskEventTracker(t tracker.TaskEventTracker) {
	a.taskEventTracker = t
	if a.configurator != nil {
		a.configurator.SetTaskEventTracker(t)
	}
}
