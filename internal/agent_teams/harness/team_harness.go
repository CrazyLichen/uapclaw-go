package harness

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCustomizer 用户自定义钩子签名。
// S-01: 全项目唯一类型定义在 agent_teams/schema 包，此处保留类型别名。
// Python: AgentCustomizer = Callable[[DeepAgent, Optional[str], str], None]
//
// 参数：ctx, deepAgent, memberName, roleValue；memberName/role 空串等价 Python None。
type AgentCustomizer = atschema.AgentCustomizer

// TeamHarness TeamAgent 与底层 DeepAgent 之间的唯一适配器。
// Python: TeamHarness (openjiuwen/agent_teams/harness.py)
//
// 所有对 DeepAgent 的访问（配置、模型、工作空间、Rails、流式）
// 必须通过此对象。替换 DeepAgent 只需重新实现此模块；
// agent_teams 中的业务代码保持相同的调用面。
type TeamHarness struct {
	// deepAgent 内层 DeepAgent 实例（= native.DeepAgent()，同一对象）
	deepAgent hinterfaces.DeepAgentInterface
	// native NativeHarness（协议面载体；组合 deepAgent 的那台 DeepAgent）
	// ⤴️ 25.1a：团队成员脑的统一交互面。deepAgent 保留为 MemberRuntime 面
	// 方法（HasPendingInterrupt/FindRails/RegisterMemberTools 等）与既有
	// 调用方（InnerAgent）的向后访问；协议面方法（Start/Stop/Send/...）转 native。
	native *NativeHarness
	// rails 已挂载的 Rails 句柄
	// role 团队角色（对齐 Python TeamRole，底层为 string）
	role string
	// memberName 成员名
	memberName string
	// initialPlanMode 初始计划模式
	initialPlanMode bool
	// taskEventTracker 成员的任务通知器（上游 _task_event_tracker）
	// Python: TeamHarness._task_event_tracker (team_harness.py:93-130)
	taskEventTracker tracker.TaskEventTracker
	// mu 保护 activeAgentSession 与 initialPlanModeSeeded。
	// 写点在轮次 goroutine（prepareAgentSession），读点在消息处理 goroutine
	// （interactLoop ← StreamController.Steer）与协调内核的邮箱轮询
	// （HasPendingInterrupt），两者之间没有任何队列或同步边，必须显式加锁。
	// Python 是单线程 asyncio，赋值与读取天然串行，无对应结构。
	mu sync.RWMutex
	// initialPlanModeSeeded 初始计划模式是否已种子化
	initialPlanModeSeeded bool
	// activeAgentSession 最近一次轮次使用的 Agent 会话。
	// 语义是"最近一次"而非"活跃"：轮次结束后 DeepAgent.forceCleanupController
	// 会把 loop_session 置 nil，而 HITL 应答恰好发生在轮次结束之后，
	// 只能靠这里保留的引用读到中断状态（Python 的 _active_agent_session）。
	activeAgentSession *session.Session
	// nativeSessionID 当前 run cycle 对应的团队会话标识。
	// Python: _native_session_id（start 时记录、stop 时清空）
	nativeSessionID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentTeam
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errIncompatibleAgent 底层 deepAgent 不是 *agentcore.DeepAgent，无法投递运行中输入。
	errIncompatibleAgent = errors.New("team harness: deepAgent 不是 *agentcore.DeepAgent")
	// errNoActiveSession 没有可用于投递的 Agent 会话。
	errNoActiveSession = errors.New("team harness: 无活跃 Agent 会话")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamHarness 创建新的 TeamHarness 实例。
// Python: TeamHarness.__init__(deep_agent, rails, ...)
func NewTeamHarness(
	deepAgent hinterfaces.DeepAgentInterface,
	role string,
	memberName string,
	initialPlanMode bool,
) *TeamHarness {
	return &TeamHarness{
		deepAgent:       deepAgent,
		role:            role,
		memberName:      memberName,
		initialPlanMode: initialPlanMode,
	}
}

// newTeamHarnessWithNative 由 NativeHarness 构造（协议面已就位）。
func newTeamHarnessWithNative(
	native *NativeHarness,
	deepAgent hinterfaces.DeepAgentInterface,
	role string,
	memberName string,
	initialPlanMode bool,
	taskEventTracker tracker.TaskEventTracker,
) *TeamHarness {
	return &TeamHarness{
		deepAgent:        deepAgent,
		native:           native,
		role:             role,
		memberName:       memberName,
		initialPlanMode:  initialPlanMode,
		taskEventTracker: taskEventTracker,
	}
}

// BuildTeamHarness 从 AgentSpec 物化 DeepAgent 并挂载所有团队 Rails。
// Python: TeamHarness.build(...)
//
// S-06: 对齐 Python deep_agent = agent_spec.build()——从 spec 构建 DeepAgent
// 实例传入 harness，RunAgentCustomizer 不再面对 nil agent。
// 挂载顺序有语义：TeamToolRail 必须在 TeamPolicyRail 之前挂载并
// 急切初始化，以便 LLM 看到的能力快照与测试观察到的一致。
// ⤴️ 9.68 回填完成：Rails 挂载逻辑已实现
// BuildTeamHarness 前向构造团队线束（6 参数，对齐上游 TeamHarness.build）。
//
// 上游 team_harness.py:93-130：native 从 spec 物化自身，团队 rails 在
// agent_spec.rails 里声明，经 spec build + ensure_initialized 统一挂载——
// 这里不做手工挂载，也不建 throwaway 模板 DeepAgent。
//
// 前置：调用方（AgentConfigurator.SetupAgent）必须已经
//
//	① EnsureHarnessElementsRegistered()
//	② InjectTeamHandles(memberBuildCtx, handles)
//	③ 把 TeamRailSpecs 折进 agentSpec.Rails
//
// 否则 8 个 team rail provider 是死代码，声明会被静默跳过。
//
// Python: TeamHarness.build (team_harness.py:93-130)
func BuildTeamHarness(
	agentSpec atschema.DeepAgentSpec,
	role string,
	memberName string,
	initialPlanMode bool,
	buildCtx hschema.BuildContextInterface,
	taskEventTracker tracker.TaskEventTracker,
) *TeamHarness {
	native, err := NewNativeHarness(agentSpec, buildCtx)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("member_name", memberName).
			Msg("BuildTeamHarness: native construction failed")
		return newTeamHarnessWithNative(nil, nil, role, memberName, initialPlanMode, taskEventTracker)
	}
	return newTeamHarnessWithNative(native, native.DeepAgent(), role, memberName,
		initialPlanMode, taskEventTracker)
}

// RunAgentCustomizer 调用用户自定义钩子。
// Python: TeamHarness.run_agent_customizer(customizer)
//
// 在 Rail 挂载和依赖绑定（memory_manager 等）之后调用，
// 使自定义器看到完整准备的环境。吞掉异常以保持团队启动不被破坏；
// 失败记录到日志。
func (h *TeamHarness) RunAgentCustomizer(ctx context.Context, customizer AgentCustomizer) {
	if customizer == nil {
		return
	}
	// Python: try/except Exception — 吞掉 customizer 的 panic 和 error
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("member_name", h.memberName).
				Any("error", r).
				Msg("agent_customizer failed")
		}
	}()
	if h.deepAgent == nil {
		logger.Warn(logComponent).
			Str("member_name", h.memberName).
			Msg("RunAgentCustomizer: deepAgent is nil, skipping customizer")
		return
	}
	if err := customizer(ctx, h.deepAgent, h.memberName, h.role); err != nil {
		logger.Warn(logComponent).
			Str("member_name", h.memberName).
			Err(err).
			Msg("agent_customizer failed")
	}
}

// Role 返回团队角色。
func (h *TeamHarness) Role() string {
	return h.role
}

// MemberName 返回成员名。
func (h *TeamHarness) MemberName() string {
	return h.memberName
}

// InnerAgent 返回底层 DeepAgent 实例。
// Python: TeamHarness.inner_agent property
//
// 生产代码不得使用此方法。仅用于测试和少量迁移辅助。
func (h *TeamHarness) InnerAgent() hinterfaces.DeepAgentInterface {
	return h.deepAgent
}

// DeepConfig 返回 DeepAgent 配置快照。
// Python: TeamHarness.deep_config property
func (h *TeamHarness) DeepConfig() *hschema.DeepAgentConfig {
	if h.deepAgent == nil {
		return nil
	}
	return h.deepAgent.DeepConfig()
}

// Workspace 返回绑定到底层 Agent 的工作空间。
// Python: TeamHarness.workspace property
func (h *TeamHarness) Workspace() *workspace.Workspace {
	if h.deepAgent == nil {
		return nil
	}
	dc := h.deepAgent.DeepConfig()
	if dc == nil {
		return nil
	}
	return dc.Workspace
}

// SysOperation 返回绑定到底层 Agent 的系统操作。
// Python: TeamHarness.sys_operation property
func (h *TeamHarness) SysOperation() sysop.SysOperation {
	if h.deepAgent == nil {
		return nil
	}
	dc := h.deepAgent.DeepConfig()
	if dc == nil {
		return nil
	}
	return dc.SysOperation
}

// Model 返回底层 Agent 使用的模型。
// Python: TeamHarness.model property
func (h *TeamHarness) Model() *llm.Model {
	if h.deepAgent == nil {
		return nil
	}
	dc := h.deepAgent.DeepConfig()
	if dc == nil {
		return nil
	}
	return dc.Model
}

// HasPendingInterrupt 返回 Agent 是否有待恢复的中断状态。
// Python: TeamHarness.has_pending_interrupt()
//
// Python 判据是 `session.get_state(INTERRUPTION_KEY) is not None`，
// 中断状态由 single_agent 的 ToolInterruptHandler.Save 写进同一个会话。
func (h *TeamHarness) HasPendingInterrupt() bool {
	sess := h.interruptSession()
	if sess == nil {
		return false
	}
	val, err := sess.GetState(sessionstate.StringKey(agentschema.InterruptionKey))
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("member_name", h.memberName).
			Msg("TeamHarness.HasPendingInterrupt: 读取中断状态失败")
		return false
	}
	return interruptionStateOf(val) != nil
}

// IsPendingInterruptResumeValid 检查给定用户输入是否为有效中断恢复。
// Python: TeamHarness.is_pending_interrupt_resume_valid(user_input)
//
// 判据：user_input.user_inputs 的键必须非空且全部落在
// interrupted_tools[*].interrupt_requests 的键集合内（Python 用 issubset）。
// ⤴️ 9.60 已实现 StreamController，此方法由 StreamController.IsValidInterruptResume 通过 resources.Harness 调用
func (h *TeamHarness) IsPendingInterruptResumeValid(userInput any) bool {
	in, ok := userInput.(*interaction.InteractiveInput)
	if !ok || in == nil {
		// Python: if not isinstance(user_input, InteractiveInput): return False
		return false
	}
	sess := h.interruptSession()
	if sess == nil {
		return false
	}
	val, err := sess.GetState(sessionstate.StringKey(agentschema.InterruptionKey))
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("member_name", h.memberName).
			Msg("TeamHarness.IsPendingInterruptResumeValid: 读取中断状态失败")
		return false
	}
	st := interruptionStateOf(val)
	if st == nil {
		return false
	}
	pendingIDs := make(map[string]struct{}, len(st.InterruptedTools))
	for _, entry := range st.InterruptedTools {
		if entry == nil {
			continue
		}
		for id := range entry.InterruptRequests {
			pendingIDs[id] = struct{}{}
		}
	}
	if len(pendingIDs) == 0 || len(in.UserInputs) == 0 {
		return false
	}
	for id := range in.UserInputs {
		if _, hit := pendingIDs[id]; !hit {
			return false
		}
	}
	return true
}

// PersistTeamDBState 持久化团队数据库状态。
// ⤵️ 待 9.56 Manager 章节回填：实现锁存标志 + 持久化 DB 状态
func (h *TeamHarness) PersistTeamDBState() {}

// MarkTeamCleaned 标记团队已清理。
// ⤵️ 待 9.56 Manager 章节回填：实现清理标志持久化
func (h *TeamHarness) MarkTeamCleaned() {}

// MarkTeamBuilt 标记团队已构建。
// ⤵️ 待 9.56 Manager 章节回填：实现构建标志持久化
func (h *TeamHarness) MarkTeamBuilt() {}

// RequestCompletionPoll 请求完成轮询回调。
// ⤴️ 9.60 已实现 StreamController，此方法由 StreamController 通过 requestCompletionPollCb 回调
func (h *TeamHarness) RequestCompletionPoll() {}

// WakeMailboxIfInterruptCleared 唤醒邮箱如果中断已清除。
// ⤴️ 9.60 已实现 StreamController，此方法由 StreamController 通过 wakeMailboxCb 回调
func (h *TeamHarness) WakeMailboxIfInterruptCleared() {}

// InitCwdForRound 用工作空间根目录重建本轮的 cwd 状态，返回携带新状态的 ctx。
// Python: TeamHarness.init_cwd_for_round() —— init_cwd(root, workspace=root)
//
// 为什么必须是 ctx 进/ctx 出：Python 的 init_cwd 写 contextvar，天然"本轮生效"；
// Go 的 ctx 是值传递，不把新 ctx 交回调用方，工具执行仍会读上一轮的 CwdState，
// 于是上一轮里的 cd 会跨轮残留（Python 每轮都会重置回 workspace 根）。
// 这里不写 DeepAgent.cwdState：attachRunContexts 只在 ctx 缺 CwdState 时注入，
// 传入的 ctx 优先，因此不影响其它轮次/其它 Agent 的隔离。
func (h *TeamHarness) InitCwdForRound(ctx context.Context) context.Context {
	ws := h.Workspace()
	if ws == nil || ws.RootPath == "" {
		return ctx
	}
	state := cwd.InitCwd(ws.RootPath, cwd.WithWorkspace(ws.RootPath))
	return cwd.WithCwdState(ctx, state)
}

// Start 绑定子会话并启动 native 的 supervisor（一个 run cycle 一次）。
// Python: TeamHarness.start(team_session)（team_harness.py:135-169）
// 步骤对齐上游 9 步；25.1a 阶段 native 由 BuildTeamHarness 物化且 fresh，
// 故"重建 native / _remount_dynamic_tools / background_task_controller / kv_cache hook"
// 四步在本阶段为 no-op（⤵️ 25.1b spec 驱动 + ⤵️ 25.1d 异步工具 + ⤵️ 25.6 会话钩子）。
func (h *TeamHarness) Start(ctx context.Context, teamSession any) error {
	if h.native == nil {
		return fmt.Errorf("team harness: 未构建 native（须经 BuildTeamHarness）")
	}

	// 步骤 3：建子会话（对齐 _make_child_session）
	child := h.makeChildSession(teamSession)
	if child == nil {
		return fmt.Errorf("team harness: 建子会话失败")
	}
	// 步骤 5：子会话 pre_run
	if err := child.PreRun(ctx); err != nil {
		logger.Error(logComponent).Err(err).
			Str("member_name", h.memberName).
			Msg("TeamHarness.Start: agent session pre_run failed")
		return err
	}
	// 步骤 6：启动 native supervisor
	if err := h.native.Start(ctx, child); err != nil {
		return err
	}
	// 步骤 7：记录 native 会话标识
	h.nativeSessionID = sessionIDOf(teamSession)
	// 步骤 8：记录活跃会话
	h.setActiveSession(child)
	// 步骤 9：种子化初始 plan 模式
	h.ensureInitialPlanMode(child)
	return nil
}

// Stop 停止 native supervisor；保留（已终止的）native 供配置读取。
// Python: TeamHarness.stop()（team_harness.py:171-199）
func (h *TeamHarness) Stop(ctx context.Context) error {
	if h.native != nil && h.native.State() != HarnessStateTerminated {
		if err := h.native.Stop(ctx); err != nil {
			return err
		}
	}
	h.nativeSessionID = ""
	h.setActiveSession(nil)
	return nil
}

// RunOnce 执行一次非流式调用（绕过 supervisor）。
// Python: TeamHarness.run_once(content, team_session)（team_harness.py:201-250）
func (h *TeamHarness) RunOnce(ctx context.Context, content any, teamSession any) (map[string]any, error) {
	if h.native == nil {
		return nil, fmt.Errorf("team harness: 未构建 native（须经 BuildTeamHarness）")
	}
	child := h.makeChildSession(teamSession)
	if child == nil {
		return nil, fmt.Errorf("team harness: 建子会话失败")
	}
	if err := child.PreRun(ctx); err != nil {
		return nil, err
	}
	h.setActiveSession(child)
	defer func() {
		_ = child.PostRun(ctx)
		h.setActiveSession(nil)
	}()
	return h.native.RunOnce(ctx, content, child)
}

// Dispose 永久销毁 native 并释放进程级资源。
// Python: TeamHarness.dispose()（team_harness.py:252-264）
func (h *TeamHarness) Dispose(ctx context.Context) error {
	if h.native != nil {
		return h.native.Dispose(ctx)
	}
	return nil
}

// Outputs 返回 native 的输出迭代器。
// Python: TeamHarness.outputs()（team_harness.py:342-349）
func (h *TeamHarness) Outputs() *OutputIterator {
	if h.native == nil {
		return nil
	}
	return h.native.Outputs()
}

// Send 投递输入；immediate 转向当前轮。
// Python: TeamHarness.send(content, immediate=...)（team_harness.py:351-368）
func (h *TeamHarness) Send(ctx context.Context, content any, immediate bool) error {
	if h.native == nil {
		return fmt.Errorf("team harness: send() before start()")
	}
	return h.native.Send(ctx, content, immediate)
}

// Abort 中断当前轮（优雅或立即）。
// Python: TeamHarness.abort(immediate=...)（team_harness.py:370-381）
func (h *TeamHarness) Abort(ctx context.Context, immediate bool) error {
	if h.native == nil || h.getActiveSession() == nil {
		// 无活跃 run cycle：no-op（对齐 Python _is_cycle_active 守卫）
		return nil
	}
	return h.native.Abort(ctx, immediate)
}

// Pause 暂停当前轮（25.1c 前返回哨兵错误）。
// Python: TeamHarness.pause()（team_harness.py:383-390）
func (h *TeamHarness) Pause(ctx context.Context) error {
	if h.native == nil || h.getActiveSession() == nil {
		return nil
	}
	return h.native.Pause(ctx)
}

// Resume 继续暂停的轮（25.1c 前返回哨兵错误）。
// Python: TeamHarness.resume(query=...)（team_harness.py:392-400）
func (h *TeamHarness) Resume(ctx context.Context, query any) error {
	if h.native == nil || h.getActiveSession() == nil {
		return nil
	}
	return h.native.Resume(ctx, query)
}

// State 返回 native 生命周期相位；无 native 时返回 idle。
// Python: TeamHarness.state property（team_harness.py:327-331）
func (h *TeamHarness) State() HarnessState {
	if h.native == nil {
		return HarnessStateIdle
	}
	return h.native.State()
}

// SessionID 返回 native 的会话标识；无 native 时返回空串。
// Python: TeamHarness.session_id property（team_harness.py:333-340）
func (h *TeamHarness) SessionID() string {
	if h.native == nil {
		return ""
	}
	return h.native.SessionID()
}

// Subscribe 在 native 上注册相位/轮次回调。
// Python: TeamHarness.subscribe(on_state=..., on_round=...)（team_harness.py:402-412）
func (h *TeamHarness) Subscribe(ctx context.Context, onState StateHandler, onRound RoundHandler) error {
	if h.native == nil {
		return nil
	}
	return h.native.Subscribe(ctx, onState, onRound)
}

// Native 返回底层 NativeHarness（生产代码勿用，仅测试与迁移助手）。
// Python: TeamHarness.inner_agent property
func (h *TeamHarness) Native() *NativeHarness { return h.native }

// sessionIDOf 从团队会话提取会话标识。
// Python: TeamHarness._session_id_of(team_session)
func sessionIDOf(teamSession any) string {
	if teamSession == nil {
		return ""
	}
	if s, ok := teamSession.(interface{ GetSessionID() string }); ok {
		return s.GetSessionID()
	}
	return ""
}

// makeChildSession 创建 native 本轮运行的子会话。
// Python: TeamHarness._make_child_session(team_session)（team_harness.py:285-302）
func (h *TeamHarness) makeChildSession(teamSession any) *session.Session {
	card := h.card()
	if ts, ok := teamSession.(*session.AgentTeamSession); ok && ts != nil {
		// Python: team_session.create_agent_session(card=card, share_stream_writer=False)
		return ts.CreateAgentSession(card, "", false)
	}
	// Python: create_agent_session(card=card)
	return session.CreateAgentSession("", card, nil)
}

// card 返回底层 native 的 card（无 native 时返回 nil）。
func (h *TeamHarness) card() *agentschema.AgentCard {
	if h.native != nil {
		return h.native.DeepAgent().Card()
	}
	if h.deepAgent != nil {
		return h.deepAgent.Card()
	}
	return nil
}

// ensureInitialPlanMode 首次以 team.plan Leader 身份运行时把 Agent 切入 plan 模式。
// Python: TeamHarness._ensure_initial_plan_mode(session)
//
// 锁内只做"是否已种子化"的判断与置位，LoadState/SwitchMode 在锁外调用
// （避免持锁进入 DeepAgent 的内部锁）。
func (h *TeamHarness) ensureInitialPlanMode(agentSess sessioninterfaces.SessionFacade) {
	if !h.isInitialTeamPlanLeader() {
		return
	}
	h.mu.RLock()
	seeded := h.initialPlanModeSeeded
	h.mu.RUnlock()
	if seeded {
		return
	}
	state := h.deepAgent.LoadState(agentSess)
	if state == nil || state.PlanMode.Mode != "plan" {
		h.deepAgent.SwitchMode(agentSess, "plan")
	}
	h.mu.Lock()
	h.initialPlanModeSeeded = true
	h.mu.Unlock()
}

// isInitialTeamPlanLeader 判断是否为需要种子化 plan 模式的 Leader。
// Python: TeamHarness._is_initial_team_plan_leader()
func (h *TeamHarness) isInitialTeamPlanLeader() bool {
	return h.initialPlanMode && h.role == string(atschema.TeamRoleLeader)
}

// FindRails 返回挂载在底层 Agent 上的指定类型 Rails。
// 对齐 Python: TeamHarness.find_rails(rail_type) → self._deep_agent.find_rails_by_type((rail_type,))
func (h *TeamHarness) FindRails(railType reflect.Type) []any {
	if h.deepAgent == nil {
		return nil
	}
	rails := h.deepAgent.FindRailsByType(railType)
	result := make([]any, len(rails))
	for i, r := range rails {
		result[i] = r
	}
	return result
}

// RegisterRail 在运行中的 Agent 上注册额外 Rail。
// Python: TeamHarness.register_rail(rail) → await self._deep_agent.register_rail(rail)
//
// DeepAgentInterface 已声明 AddRail，原 TODO(#9.57) 的"类型升级"前置条件已满足。
func (h *TeamHarness) RegisterRail(_ context.Context, rail agentinterfaces.AgentRail) error {
	if rail == nil {
		return fmt.Errorf("TeamHarness.RegisterRail: rail 为 nil")
	}
	if h.deepAgent == nil {
		return errIncompatibleAgent
	}
	h.deepAgent.AddRail(rail)
	return nil
}

// UnregisterRail 注销先前注册的 Rail。
// Python: TeamHarness.unregister_rail(rail) → await self._deep_agent.unregister_rail(rail)
func (h *TeamHarness) UnregisterRail(ctx context.Context, rail agentinterfaces.AgentRail) error {
	if rail == nil {
		return fmt.Errorf("TeamHarness.UnregisterRail: rail 为 nil")
	}
	if h.deepAgent == nil {
		return errIncompatibleAgent
	}
	return h.deepAgent.UnregisterRail(ctx, rail)
}

// RegisterMemberTools 在底层 Agent 上注册团队记忆工具集。
// Python: TeamHarness.register_member_tools(memory_manager)
// ⤴️ 9.64 回填完成
func (h *TeamHarness) RegisterMemberTools(memoryManager *memory.TeamMemoryManager) {
	if memoryManager == nil {
		return
	}
	memoryManager.RegisterTools(h.deepAgent)
}

// InjectMemberMemory 向 Agent 的系统提示注入加载的记忆。
// Python: TeamHarness.inject_member_memory(memory_manager, query)
// ⤴️ 9.64 回填完成
func (h *TeamHarness) InjectMemberMemory(ctx context.Context, memoryManager *memory.TeamMemoryManager, query string) error {
	if memoryManager == nil {
		return nil
	}
	return memoryManager.LoadAndInject(ctx, h.deepAgent, query)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// AnnounceTaskRevisions 立刻投递成员待发的任务编辑通知（F_129）。
//
// 通知的唯一来源是成员的任务通知器（TeamAgent 的那个实例，策略 rail 借用的
// 是同一个）。轮次之外没有可认领的会话，因此什么都不会认领：rail 会在成员
// 下一个输入时告诉它。通知会转向忙碌成员、并为空闲成员开一轮。
//
// Python: TeamHarness.announce_task_revisions (team_harness.py:291-304)
func (h *TeamHarness) AnnounceTaskRevisions(ctx context.Context) error {
	if h.taskEventTracker == nil {
		return nil
	}
	sess := h.getActiveSession()
	if sess == nil {
		return nil
	}
	_, err := h.taskEventTracker.Announce(sess, func(text string) error {
		return h.Send(ctx, text, true)
	})
	return err
}

// getActiveSession 读取最近一次轮次的 Agent 会话（并发安全）。
func (h *TeamHarness) getActiveSession() *session.Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.activeAgentSession
}

// setActiveSession 记录本轮使用的 Agent 会话（并发安全）。
func (h *TeamHarness) setActiveSession(sess *session.Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activeAgentSession = sess
}

// interruptSession 返回用于读取中断状态的会话。
// Python: TeamHarness._interrupt_session() → self._deep_agent.loop_session or self._active_agent_session
//
// 优先取 loop_session（与 Python 一致）；轮次结束时 forceCleanupController 会把它置 nil，
// 而 HITL 应答恰好发生在"轮次已结束、等待用户输入"的时刻，所以必须回退到
// 最近一次轮次的 Agent 会话，否则 HasPendingInterrupt 在真正需要的时刻恒为 false。
func (h *TeamHarness) interruptSession() *session.Session {
	if d, ok := h.deepAgent.(*agentcore.DeepAgent); ok && d != nil {
		if loopSess := d.LoopSession(); loopSess != nil {
			return loopSess
		}
	}
	return h.getActiveSession()
}

// interruptionStateOf 把 GetState 的返回值收窄为中断状态。
// 返回 nil 表示"没有中断状态"，同时处理接口里存 typed-nil 的情况
// （Python 的 `is not None` 对 None 与未设置是同一判定）。
func interruptionStateOf(val any) *agentschema.ToolInterruptionState {
	if val == nil {
		return nil
	}
	st, ok := val.(*agentschema.ToolInterruptionState)
	if !ok {
		return nil
	}
	return st
}
