// Package agent 提供生产级团队 Agent（TeamAgent）实现。
//
// TeamAgent 是整个多 Agent 协作系统的核心编排节点，既可充当 Leader
// （分发任务、协调成员），也可充当 Teammate（执行具体任务）。
// 采用组合式架构：内部包裹 DeepAgent 实例（而非继承），
// 委托给专职 Manager 管理配置/生成/恢复/会话/流式/协调。
//
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
//	├── spawn_manager.go      # TODO(#9.58) 生成管理器
//	├── session_manager.go    # TODO(#9.59) 会话管理器
//	├── stream_controller.go  # TODO(#9.60) 流式控制器
//	├── recovery_manager.go   # TODO(#9.61) 恢复管理器
//	└── coordination/         # TODO(#9.62-9.63) 协调子系统
//	    ├── kernel.go         # TODO(#9.62) 协调内核
//	    ├── event_bus.go      # TODO(#9.63) 事件总线
//	    ├── dispatcher.go     # TODO(#9.63) 事件分发器
//	    └── handlers/         # TODO(#9.63) 事件处理器
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/
package agent

import (
	"context"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamAgent 生产级团队 Agent。
// 组合式架构：包裹内部 DeepAgent 实例，委托给专职 Manager。
// 对齐 Python: TeamAgent (openjiuwen/agent_teams/agent/team_agent.py)
//
// 可充当 Leader 或 Teammate：
//   - Leader：分发任务、协调成员、管理团队生命周期
//   - Teammate：执行具体任务、与 Leader/其他成员协作
type TeamAgent struct {
	// card Agent 身份卡片
	card *schema.AgentCard
	// deepAgent 内层 DeepAgent 实例（TODO(#9.57): AgentConfigurator 构建后赋值）
	deepAgent hinterfaces.DeepAgentInterface
	// configurator Agent 配置器
	configurator *AgentConfigurator
	// state 可变运行时状态
	state *TeamAgentState
	// spawnManager 子进程管理器
	spawnManager *SpawnManager
	// recoveryManager 恢复管理器
	// TODO(#9.61): RecoveryManager 类型
	recoveryManager any
	// sessionManager 会话管理器
	// TODO(#9.59): SessionManager 类型
	sessionManager any
	// streamController 流式控制器
	// TODO(#9.60): StreamController 类型
	streamController any
	// coordination 协调内核
	// TODO(#9.62): CoordinationKernel 类型
	coordination any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamAgent 创建新的 TeamAgent 实例。
// 对齐 Python: TeamAgent.__init__(card)
func NewTeamAgent(card *schema.AgentCard) *TeamAgent {
	a := &TeamAgent{
		card:         card,
		state:        NewTeamAgentState(),
		configurator: NewAgentConfigurator(card),
	}
	// 构建 SpawnManager
	a.spawnManager = NewSpawnManager(a.state, a.configurator, func() *TeamAgent { return a })
	// TODO(#9.61): 构建 RecoveryManager(configurator, spawnManager)
	// TODO(#9.59): 构建 SessionManager(state, configurator, recoveryManager)
	// TODO(#9.60): 构建 StreamController(blueprintGetter, state, resources, ...)
	// TODO(#9.62): 构建 CoordinationKernel(self)
	return a
}

// ──────────────────────────────────────────────────────────────
// 属性 — 委托给 configurator
// ──────────────────────────────────────────────────────────────

// AgentCard 返回 Agent 身份卡片。
// 满足 spawn.SpawnableAgent 接口。
// 对齐 Python: TeamAgent.card property
func (a *TeamAgent) AgentCard() *schema.AgentCard {
	return a.card
}

// Blueprint 返回静态装配蓝图，configure() 前为 nil。
// 对齐 Python: TeamAgent.blueprint property
func (a *TeamAgent) Blueprint() *TeamAgentBlueprint {
	if a.configurator != nil {
		return a.configurator.Blueprint()
	}
	return nil
}

// State 返回可变运行时状态容器。
// 对齐 Python: TeamAgent.state property
func (a *TeamAgent) State() *TeamAgentState {
	return a.state
}

// Infra 返回每进程团队基础设施容器。
// 对齐 Python: TeamAgent.infra property
func (a *TeamAgent) Infra() *TeamInfra {
	if a.configurator != nil {
		return a.configurator.Infra()
	}
	return nil
}

// Resources 返回每实例运行时资源容器。
// 对齐 Python: TeamAgent.resources property
func (a *TeamAgent) Resources() *PrivateAgentResources {
	if a.configurator != nil {
		return a.configurator.Resources()
	}
	return nil
}

// Harness 返回拥有底层 DeepAgent 运行时的 Harness。
// 对齐 Python: TeamAgent.harness property
//
// 所有对 DeepAgent 运行时的访问（config、model、workspace、
// rails、streaming）必须通过此对象。
func (a *TeamAgent) Harness() *agentteams.TeamHarness {
	if a.configurator != nil {
		return a.configurator.Harness()
	}
	return nil
}

// Spec 返回 TeamAgentSpec。
// 对齐 Python: TeamAgent.spec property
func (a *TeamAgent) Spec() *atschema.TeamAgentSpec {
	if a.configurator != nil {
		return a.configurator.Spec()
	}
	return nil
}

// RuntimeContext 返回 TeamRuntimeContext。
// 对齐 Python: TeamAgent.runtime_context property
func (a *TeamAgent) RuntimeContext() *atschema.TeamRuntimeContext {
	if a.configurator != nil {
		return a.configurator.RuntimeContext()
	}
	return nil
}

// Coordination 返回协调内核。
// 对齐 Python: TeamAgent.coordination property
func (a *TeamAgent) Coordination() any {
	// TODO(#9.62): return coordination (CoordinationKernel 类型)
	return a.coordination
}

// CoordinationLoop 返回底层事件总线。
// 对齐 Python: TeamAgent.coordination_loop property
//
// 保留为测试和遗留调用者的公开访问器；
// 新代码应通过 coordination 访问。
func (a *TeamAgent) CoordinationLoop() any {
	// TODO(#9.62): 返回协调事件总线 return coordination.event_bus
	return nil
}

// Role 返回团队角色。
// 对齐 Python: TeamAgent.role property
func (a *TeamAgent) Role() atschema.TeamRole {
	if a.configurator != nil {
		return a.configurator.Role()
	}
	return atschema.TeamRoleLeader
}

// Lifecycle 返回生命周期模式。
// 对齐 Python: TeamAgent.lifecycle property
func (a *TeamAgent) Lifecycle() string {
	if a.configurator != nil {
		return a.configurator.Lifecycle()
	}
	// 对齐 Python: AgentConfigurator.lifecycle — spec 为 None 时返回 "temporary"
	return "temporary"
}

// TeamSpec 返回 TeamSpec。
// 对齐 Python: TeamAgent.team_spec property
func (a *TeamAgent) TeamSpec() *atschema.TeamSpec {
	if a.configurator != nil {
		return a.configurator.TeamSpec()
	}
	return nil
}

// MemberName 返回成员名。
// 对齐 Python: TeamAgent.member_name property
func (a *TeamAgent) MemberName() string {
	if a.configurator != nil {
		return a.configurator.MemberName()
	}
	return ""
}

// MessageManager 返回消息管理器。
// 对齐 Python: TeamAgent.message_manager property
func (a *TeamAgent) MessageManager() any {
	if a.configurator != nil {
		return a.configurator.MessageManager()
	}
	return nil
}

// TaskManager 返回任务管理器。
// 对齐 Python: TeamAgent.task_manager property
func (a *TeamAgent) TaskManager() any {
	if a.configurator != nil {
		return a.configurator.TaskManager()
	}
	return nil
}

// TeamBackend 返回 TeamBackend。
// 对齐 Python: TeamAgent.team_backend property
func (a *TeamAgent) TeamBackend() any {
	if a.configurator != nil {
		return a.configurator.TeamBackend()
	}
	return nil
}

// SessionID 返回当前会话 ID（从 agent_teams contextvar 读取）。
// 对齐 Python: TeamAgent.session_id property
func (a *TeamAgent) SessionID(ctx context.Context) string {
	// TODO(#9.59): 从 agent_teams.GetSessionID(ctx) 读取
	return ""
}

// SessionManager 返回会话管理器。
// 对齐 Python: TeamAgent.session_manager property
func (a *TeamAgent) SessionManager() any {
	return a.sessionManager
}

// RecoveryManager 返回恢复管理器。
// 对齐 Python: TeamAgent.recovery_manager property
func (a *TeamAgent) RecoveryManager() any {
	return a.recoveryManager
}

// SpawnManager 返回生成管理器。
// 对齐 Python: TeamAgent.spawn_manager property
func (a *TeamAgent) SpawnManager() *SpawnManager {
	return a.spawnManager
}

// StreamController 返回流式控制器。
// 对齐 Python: TeamAgent.stream_controller property
func (a *TeamAgent) StreamController() any {
	return a.streamController
}

// EventListeners 返回已注册的事件监听器。
// 对齐 Python: TeamAgent.event_listeners property
func (a *TeamAgent) EventListeners() []any {
	return a.state.EventListeners
}

// TeamMemberHandle 返回此 Agent 的 TeamMember 句柄（可能为 nil）。
// 对齐 Python: TeamAgent.team_member property
func (a *TeamAgent) TeamMemberHandle() any {
	return a.state.TeamMember
}

// PendingUserQuery 返回待处理的用户查询字符串。
// 对齐 Python: TeamAgent.pending_user_query property
func (a *TeamAgent) PendingUserQuery() string {
	return a.state.PendingUserQuery
}

// TeamName 返回团队名。
// 对齐 Python: TeamAgent.team_name property
func (a *TeamAgent) TeamName() string {
	if a.configurator != nil {
		return a.configurator.TeamName()
	}
	return ""
}

// IsShutdownRequested 此 Teammate 是否已被请求关闭或已关闭。
// 对齐 Python: TeamAgent.is_shutdown_requested()
//
// Leader 从不持有 TeamMember 句柄，因此对 Leader 总返回 False。
func (a *TeamAgent) IsShutdownRequested(ctx context.Context) (bool, error) {
	// TODO(#9.60): 检查 team_member.status() 是否为 SHUTDOWN_REQUESTED 或 SHUTDOWN
	return false, nil
}

// UpdateStatus 更新成员状态到数据库。
// 对齐 Python: TeamAgent.update_status(status)
func (a *TeamAgent) UpdateStatus(ctx context.Context, status atschema.MemberStatus) error {
	// TODO(#9.60): 委托 _updateStatus(status)
	return nil
}

// PersistAllocatorState 持久化模型分配器状态到当前会话。
// 对齐 Python: TeamAgent.persist_allocator_state()
func (a *TeamAgent) PersistAllocatorState() {
	// TODO(#9.64): 委托 _persistAllocatorState()
}

// AddEventListener 添加事件监听器。
// 对齐 Python: TeamAgent.add_event_listener(handler)
func (a *TeamAgent) AddEventListener(handler any) {
	a.state.EventListeners = append(a.state.EventListeners, handler)
}

// RemoveEventListener 移除事件监听器。
// 对齐 Python: TeamAgent.remove_event_listener(handler)
func (a *TeamAgent) RemoveEventListener(handler any) {
	for i, h := range a.state.EventListeners {
		if h == handler {
			a.state.EventListeners = append(a.state.EventListeners[:i], a.state.EventListeners[i+1:]...)
			return
		}
	}
}

// LookupHumanAgentRuntime 解析进程内生成的人类代理的活跃 TeamAgent。
// 对齐 Python: TeamAgent.lookup_human_agent_runtime(member_name)
func (a *TeamAgent) LookupHumanAgentRuntime(memberName string) *TeamAgent {
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
// 对齐 Python: TeamAgent.is_agent_ready()
func (a *TeamAgent) IsAgentReady() bool {
	if a.configurator != nil {
		return a.configurator.Harness() != nil
	}
	return false
}

// IsAgentRunning Agent 是否正在运行。
// 对齐 Python: TeamAgent.is_agent_running()
func (a *TeamAgent) IsAgentRunning() bool {
	// TODO(#9.60): 委托 streamController.is_agent_running()
	return false
}

// HasInFlightRound 是否有飞行中的轮次。
// 对齐 Python: TeamAgent.has_in_flight_round()
func (a *TeamAgent) HasInFlightRound() bool {
	// TODO(#9.60): 委托 streamController.has_in_flight_round()
	return false
}

// HasPendingInterrupt 是否有待处理的中断。
// 对齐 Python: TeamAgent.has_pending_interrupt()
func (a *TeamAgent) HasPendingInterrupt() bool {
	// TODO(#9.60): 委托 streamController.has_pending_interrupt()
	return false
}

// Configure 配置 TeamAgent。
// 对齐 Python: TeamAgent.configure(spec, context)
func (a *TeamAgent) Configure(ctx context.Context, spec atschema.TeamAgentSpec, runtimeCtx atschema.TeamRuntimeContext) *TeamAgent {
	// Phase 1: 基础设施搭建
	a.configurator.SetupInfra(spec, runtimeCtx,
		WithOnTeammateCreated(nil), // TODO(#9.55): 成员创建回调
		WithOnTeamCleaned(nil),     // TODO(#9.55): 团队清理回调
		WithOnTeamBuilt(nil),       // TODO(#9.55): 团队构建回调
	)

	// Phase 2: Agent 组装
	a.configurator.SetupAgent(spec, runtimeCtx)

	// 构建 TeamMember 句柄
	if runtimeCtx.MemberName != "" {
		a.state.TeamMember = CreateMemberHandle(
			runtimeCtx.MemberName,
			a.configurator.Blueprint(),
			a.configurator.Infra(),
			a.card,
		)
	}

	// TODO(#9.62): 设置协调角色 coordination.setup(role=ctx.Role)
	// TODO(#9.55): 注册团队完成回调 a.registerTeamCompletionCallbacks()

	logger.Info(logComponent).Str("member_name", runtimeCtx.MemberName).
		Str("role", string(runtimeCtx.Role)).Msg("TeamAgent.Configure")
	return a
}

// Invoke 非流式调用 TeamAgent。
// 对齐 Python: TeamAgent.invoke(inputs, session)
func (a *TeamAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
	// TODO(#9.60+#9.62): 创建 stream_queue → 缓存 pending_user_query → coordination.start(session)
	// TODO(#9.62): 入队用户输入 → 首次迭代后入队邮箱
	// TODO(#9.60): 从 stream_queue 读取直到 None sentinel
	// TODO(#9.62): 完成当前轮次 coordination.finalize_round()
	memberName := a.MemberName()
	logger.Info(logComponent).Str("member_name", memberName).
		Str("role", string(a.Role())).Msg("TeamAgent.Invoke start")
	return nil, nil
}

// Stream 流式调用 TeamAgent。
// 对齐 Python: TeamAgent.stream(inputs, session, stream_modes)
func (a *TeamAgent) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	// TODO(#9.60+#9.62): 同 Invoke 但持续 yield chunk 直到 None sentinel
	memberName := a.MemberName()
	logger.Info(logComponent).Str("member_name", memberName).
		Str("role", string(a.Role())).Msg("TeamAgent.Stream start")
	return nil, nil
}

// Interact 向团队发送输入。
// 对齐 Python: TeamAgent.interact(message)
func (a *TeamAgent) Interact(ctx context.Context, message string) error {
	// TODO(#9.62): 协调器入队用户输入 coordination.enqueue_user_input(message)
	return nil
}

// Broadcast 广播用户侧公告。
// 对齐 Python: TeamAgent.broadcast(content)
func (a *TeamAgent) Broadcast(ctx context.Context, content string) (any, error) {
	// TODO(#9.62): 用户收件箱广播 UserInbox(...).broadcast(content)
	return nil, nil
}

// HumanAgentSay 以注册的 human_agent 成员身份发言。
// 对齐 Python: TeamAgent.human_agent_say(content, to, sender)
func (a *TeamAgent) HumanAgentSay(ctx context.Context, content string, to string, sender string) (any, error) {
	// TODO(#9.62): 人类Agent收件箱发送 HumanAgentInbox(...).send(content, to, sender)
	return nil, nil
}

// DeliverInput 投递输入到 Agent。
// 对齐 Python: TeamAgent.deliver_input(content, use_steer=True)
func (a *TeamAgent) DeliverInput(ctx context.Context, content any, useSteer bool) error {
	// TODO(#9.60): 运行中→转向/跟进; 执行中→入队; 否则→启动Agent
	return nil
}

// StartAgent 启动 Agent。
// 对齐 Python: TeamAgent.start_agent(content)
func (a *TeamAgent) StartAgent(ctx context.Context, content string) error {
	// TODO(#9.60): 委托 _startAgent(content)
	return nil
}

// FollowUp 追加输入。
// 对齐 Python: TeamAgent.follow_up(content)
func (a *TeamAgent) FollowUp(ctx context.Context, content string) error {
	// TODO(#9.60): 流控制器跟进 streamController.follow_up(content)
	return nil
}

// CancelAgent 取消 Agent。
// 对齐 Python: TeamAgent.cancel_agent()
func (a *TeamAgent) CancelAgent(ctx context.Context) error {
	memberName := a.MemberName()
	logger.Debug(logComponent).Str("member_name", memberName).Msg("TeamAgent.CancelAgent requested")
	// TODO(#9.60): 委托 _cancelAgent()
	return nil
}

// Steer 转向输入。
// 对齐 Python: TeamAgent.steer(content)
func (a *TeamAgent) Steer(ctx context.Context, content string) error {
	// TODO(#9.60): 流控制器转向 streamController.steer(content)
	return nil
}

// ResumeInterrupt 恢复中断。
// 对齐 Python: TeamAgent.resume_interrupt(user_input)
func (a *TeamAgent) ResumeInterrupt(ctx context.Context, userInput any) error {
	// TODO(#9.60): 验证中断 → 飞行中则排队 → 否则 start_agent
	return nil
}

// ShutdownSelf 请求自身关闭。
// 对齐 Python: TeamAgent.shutdown_self()
func (a *TeamAgent) ShutdownSelf(ctx context.Context) error {
	memberName := a.MemberName()
	logger.Info(logComponent).Str("member_name", memberName).Msg("TeamAgent.ShutdownSelf requested")
	// TODO(#9.60): 流控制器协作取消 streamController.cooperative_cancel()
	// TODO(#9.60): 团队成员更新状态为关闭 team_member.update_status(SHUTDOWN)
	// TODO(#9.62): 关闭流 closeStream()
	return nil
}

// ConcludeCompletedRound 发出团队完成标记块并关闭 Leader 流。
// 对齐 Python: TeamAgent.conclude_completed_round(member_count, task_count)
func (a *TeamAgent) ConcludeCompletedRound(ctx context.Context, memberCount, taskCount int) error {
	memberName := a.MemberName()
	logger.Info(logComponent).Str("member_name", memberName).
		Int("member_count", memberCount).Int("task_count", taskCount).
		Msg("TeamAgent.ConcludeCompletedRound")
	// TODO(#9.60): 流控制器发送完成并关闭 streamController.emit_completion_and_close(...)
	return nil
}

// DestroyTeam 销毁团队。
// 对齐 Python: TeamAgent.destroy_team(force=True)
func (a *TeamAgent) DestroyTeam(ctx context.Context, force bool) (bool, error) {
	// TODO(#9.60+#9.62+#9.58): 取消Agent → 停止协调 → 从池中移除 → 强制清理团队
	return false, nil
}

// StartCoordination 启动协调。
// 对齐 Python: TeamAgent._start_coordination(session)
func (a *TeamAgent) StartCoordination(ctx context.Context, session any) error {
	// TODO(#9.62): 启动协调 coordination.start(session)
	return nil
}

// PauseCoordination 暂停协调（不拆卸 Teammate 进程）。
// 对齐 Python: TeamAgent.pause_coordination()
func (a *TeamAgent) PauseCoordination(ctx context.Context) error {
	// TODO(#9.62): 暂停协调 coordination.pause()
	return nil
}

// StopCoordination 停止协调（关闭所有生成的 Teammate）。
// 对齐 Python: TeamAgent.stop_coordination()
func (a *TeamAgent) StopCoordination(ctx context.Context) error {
	// TODO(#9.62): 停止协调 coordination.stop()
	return nil
}

// SpawnTeammate 生成 Teammate。
// 对齐 Python: TeamAgent.spawn_teammate(ctx, initial_message, session, spawn_config)
func (a *TeamAgent) SpawnTeammate(ctx context.Context, runtimeCtx atschema.TeamRuntimeContext, initialMessage string, sessionID string, spawnConfig any) error {
	if a.spawnManager != nil {
		return a.spawnManager.SpawnTeammate(ctx, runtimeCtx, initialMessage, sessionID, spawnConfig)
	}
	return nil
}

// AutoStartMember 启动单个 UNSTARTED 成员。
// 对齐 Python: TeamAgent.auto_start_member(member_name)
func (a *TeamAgent) AutoStartMember(ctx context.Context, memberName string) bool {
	// TODO(#9.58): 团队后端启动成员 team_backend.startup_member(...)
	return false
}

// AutoStartAll 启动所有 UNSTARTED 成员。
// 对齐 Python: TeamAgent.auto_start_all()
func (a *TeamAgent) AutoStartAll(ctx context.Context) []string {
	// TODO(#9.58): 团队后端启动 team_backend.startup(on_created)
	return nil
}

// BuildSpawnPayload 构建生成载荷。
// 对齐 Python: TeamAgent.build_spawn_payload(ctx, initial_message)
func (a *TeamAgent) BuildSpawnPayload(runtimeCtx atschema.TeamRuntimeContext, initialMessage string) map[string]any {
	if a.configurator != nil {
		return a.configurator.BuildSpawnPayload(runtimeCtx, initialMessage)
	}
	return nil
}

// BuildMemberContext 构建成员上下文。
// 对齐 Python: TeamAgent.build_member_context(member_spec)
func (a *TeamAgent) BuildMemberContext(memberSpec atschema.TeamMemberSpec) atschema.TeamRuntimeContext {
	if a.configurator != nil {
		return a.configurator.BuildMemberContext(memberSpec)
	}
	return atschema.TeamRuntimeContext{}
}

// BuildSpawnConfig 构建生成配置。
// 对齐 Python: TeamAgent.build_spawn_config(ctx)
func (a *TeamAgent) BuildSpawnConfig(runtimeCtx atschema.TeamRuntimeContext) any {
	if a.configurator != nil {
		return a.configurator.BuildSpawnConfig(runtimeCtx)
	}
	return nil
}

// FromSpawnPayload 从生成载荷重构 TeamAgent。
// 对齐 Python: TeamAgent.from_spawn_payload(payload)
func FromSpawnPayload(ctx context.Context, payload map[string]any) (*TeamAgent, error) {
	// TODO(#9.57): 解析 spec/context → 构造 card → NewTeamAgent → configure → refresh_human_agent_roster
	return nil, nil
}

// ResumeForNewSession 为新会话恢复。
// 对齐 Python: TeamAgent.resume_for_new_session(session)
func (a *TeamAgent) ResumeForNewSession(ctx context.Context, session any) error {
	// TODO(#9.59): 会话管理器恢复新会话 sessionManager.resume_for_new_session(session)
	return nil
}

// RecoverForExistingSession 恢复已有会话检查点。
// 对齐 Python: TeamAgent.recover_for_existing_session(session)
//
// 与 RecoverFromSession 不同，此方法复用当前 Agent，
// 假定 session.pre_run() 已恢复检查点状态。
func (a *TeamAgent) RecoverForExistingSession(ctx context.Context, session any) error {
	// TODO(#9.59+#9.62): 停止协调 → 会话管理器恢复已有会话 sessionManager.recover_for_existing_session(session)
	return nil
}

// RecoverTeam 恢复团队。
// 对齐 Python: TeamAgent.recover_team()
func (a *TeamAgent) RecoverTeam(ctx context.Context) ([]string, error) {
	// TODO(#9.61): 恢复管理器恢复团队 recoveryManager.recover_team()
	return nil, nil
}

// RecoverFromSession 从会话检查点重构 Leader TeamAgent。
// 对齐 Python: TeamAgent.recover_from_session(session, team_name, runtime_spec)
func RecoverFromSession(ctx context.Context, session any, teamName string, runtimeSpec *atschema.TeamAgentSpec) (*TeamAgent, error) {
	// TODO(#9.61): 从 session 读取 bucket → 解析 spec/context → NewTeamAgent → configure → restore_allocator_state → set_session_id
	return nil, nil
}

// PersistSessionManifest 持久化恢复和清理所需的最小会话清单。
// 对齐 Python: TeamAgent.persist_session_manifest(session)
func (a *TeamAgent) PersistSessionManifest(session any) {
	// TODO(#9.61): 持久化领导者配置 recoveryManager.persist_leader_config(session)
}

// UpdateModelPool 更新模型池。
// 对齐 Python: TeamAgent.update_model_pool(new_pool)
func (a *TeamAgent) UpdateModelPool(newPool any) {
	if a.configurator != nil {
		a.configurator.UpdateModelPool(newPool)
	}
	// TODO(#9.61): 持久化领导者配置 recoveryManager.persist_leader_config
}

// AttachModelAllocator 附加模型分配器。
// 对齐 Python: TeamAgent.attach_model_allocator(allocator, leader_allocation)
func (a *TeamAgent) AttachModelAllocator(allocator any, leaderAllocation any) {
	if a.configurator != nil {
		a.configurator.AttachModelAllocator(allocator, leaderAllocation)
	}
}

// RestoreAllocatorState 恢复分配器状态。
// 对齐 Python: TeamAgent.restore_allocator_state(state)
func (a *TeamAgent) RestoreAllocatorState(state map[string]any) {
	if a.configurator != nil {
		a.configurator.RestoreAllocatorState(state)
	}
}

// RegisterRail 注册 Rail。
// 对齐 Python: TeamAgent.register_rail(rail)
func (a *TeamAgent) RegisterRail(ctx context.Context, rail any) (*TeamAgent, error) {
	if a.configurator != nil && a.configurator.Harness() != nil {
		if err := a.configurator.Harness().RegisterRail(ctx, rail); err != nil {
			return a, err
		}
	}
	return a, nil
}

// UnregisterRail 注销 Rail。
// 对齐 Python: TeamAgent.unregister_rail(rail)
func (a *TeamAgent) UnregisterRail(ctx context.Context, rail any) (*TeamAgent, error) {
	if a.configurator != nil && a.configurator.Harness() != nil {
		if err := a.configurator.Harness().UnregisterRail(ctx, rail); err != nil {
			return a, err
		}
	}
	return a, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
