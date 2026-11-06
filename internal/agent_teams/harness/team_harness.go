package harness

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MountedRails 已挂载的团队侧 Rails 句柄。
// Python: _MountedRails (openjiuwen/agent_teams/harness.py)
//
// 保留为数据类使 Rails 阵容（及哪些是可选的）对读者和测试可见。
// 字段顺序与 BuildTeamHarness 中 Rails 挂载顺序一致。
type MountedRails struct {
	// TeamTool 团队工具轨
	// ⤴️ 9.68 回填完成：TeamToolRail 具体类型
	TeamTool *rails.TeamToolRail
	// TeamPolicy 团队策略轨
	// ⤴️ 9.68 回填完成：TeamPolicyRail 具体类型
	TeamPolicy *rails.TeamPolicyRail
	// FirstIterGate 首轮迭代门控
	// ⤴️ 9.68 回填完成：FirstIterationGate 具体类型
	FirstIterGate *rails.FirstIterationGate
	// TeamWorkspace 团队工作空间轨
	// ⤴️ 9.66 回填完成：TeamWorkspaceRail 具体类型
	TeamWorkspace *team_workspace.TeamWorkspaceRail
	// ToolApproval 工具审批轨
	// ⤴️ 9.68 回填完成：TeamToolApprovalRail 具体类型
	ToolApproval *rails.TeamToolApprovalRail
	// TeamPlanMode 团队计划模式轨
	// ⤴️ 9.68 回填完成：TeamPlanModeRail 具体类型
	TeamPlanMode *rails.TeamPlanModeRail
}

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
	//
	// ⤴️ 25.1a：团队成员脑的统一交互面。deepAgent 保留为 MemberRuntime 面
	// 方法（HasPendingInterrupt/FindRails/RegisterMemberTools 等）与既有
	// 调用方（InnerAgent）的向后访问；协议面方法（Start/Stop/Send/...）转 native。
	native *NativeHarness
	// rails 已挂载的 Rails 句柄
	rails *MountedRails
	// role 团队角色（对齐 Python TeamRole，底层为 string）
	role string
	// memberName 成员名
	memberName string
	// initialPlanMode 初始计划模式
	initialPlanMode bool
	// mu 保护 activeAgentSession 与 initialPlanModeSeeded。
	//
	// 写点在轮次 goroutine（prepareAgentSession），读点在消息处理 goroutine
	// （interactLoop ← StreamController.Steer）与协调内核的邮箱轮询
	// （HasPendingInterrupt），两者之间没有任何队列或同步边，必须显式加锁。
	// Python 是单线程 asyncio，赋值与读取天然串行，无对应结构。
	mu sync.RWMutex
	// initialPlanModeSeeded 初始计划模式是否已种子化
	initialPlanModeSeeded bool
	// activeAgentSession 最近一次轮次使用的 Agent 会话。
	//
	// 语义是"最近一次"而非"活跃"：轮次结束后 DeepAgent.forceCleanupController
	// 会把 loop_session 置 nil，而 HITL 应答恰好发生在轮次结束之后，
	// 只能靠这里保留的引用读到中断状态（Python 的 _active_agent_session）。
	activeAgentSession *session.Session
}

// ──────────────────────────── 枚举 ────────────────────────────

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
	rails *MountedRails,
	role string,
	memberName string,
	initialPlanMode bool,
) *TeamHarness {
	return &TeamHarness{
		deepAgent:       deepAgent,
		rails:           rails,
		role:            role,
		memberName:      memberName,
		initialPlanMode: initialPlanMode,
	}
}

// newTeamHarnessWithNative 由 NativeHarness 构造（协议面已就位）。
func newTeamHarnessWithNative(
	native *NativeHarness,
	deepAgent hinterfaces.DeepAgentInterface,
	rails *MountedRails,
	role string,
	memberName string,
	initialPlanMode bool,
) *TeamHarness {
	return &TeamHarness{
		deepAgent:       deepAgent,
		native:          native,
		rails:           rails,
		role:            role,
		memberName:      memberName,
		initialPlanMode: initialPlanMode,
	}
}

// BuildTeamHarness 从 AgentSpec 物化 DeepAgent 并挂载所有团队 Rails。
// Python: TeamHarness.build(...)
//
// S-06: 对齐 Python deep_agent = agent_spec.build()——从 spec 构建 DeepAgent
// 实例传入 harness，RunAgentCustomizer 不再面对 nil agent。
//
// 挂载顺序有语义：TeamToolRail 必须在 TeamPolicyRail 之前挂载并
// 急切初始化，以便 LLM 看到的能力快照与测试观察到的一致。
//
// ⤴️ 9.68 回填完成：Rails 挂载逻辑已实现
func BuildTeamHarness(
	agentSpec atschema.DeepAgentSpec,
	role string,
	memberName string,
	teamToolRail *rails.TeamToolRail,
	teamPolicyRail *rails.TeamPolicyRail,
	firstIterGate *rails.FirstIterationGate,
	teamWorkspaceRail *team_workspace.TeamWorkspaceRail, // ⤴️ 9.66: 团队工作空间Rail
	toolApprovalRail *rails.TeamToolApprovalRail,
	teamPlanModeRail *rails.TeamPlanModeRail,
	initialPlanMode bool,
) *TeamHarness {
	// S-06: 对齐 Python deep_agent = agent_spec.build()——物化 DeepAgent
	agent := buildDeepAgentFromSpec(agentSpec)

	// S-06: 对齐 Python build() 的 Rail 挂载顺序（L118-140）：
	// deep_agent.add_rail(team_tool_rail) + 急切初始化（set_sys_operation/
	// set_workspace/init），随后 team_policy、first_iter_gate、team_workspace、
	// tool_approval、team_plan_mode 依次挂载。
	// 挂载顺序有语义：TeamToolRail 必须在 TeamPolicyRail 之前挂载并急切初始化，
	// 以便 LLM 看到的能力快照与测试观察到的一致（Init 幂等，延迟注册变为 no-op）。
	if agent != nil {
		if teamToolRail != nil {
			agent.AddRail(teamToolRail)
			// 急切初始化：rail 的延迟 init 在首次 invoke 时运行，
			// 但在那之前获取的工具快照会遗漏团队工具
			if dc := agent.DeepConfig(); dc != nil {
				teamToolRail.SetSysOperation(dc.SysOperation)
				teamToolRail.SetWorkspace(dc.Workspace)
			}
			if err := teamToolRail.Init(context.Background(), agent); err != nil {
				logger.Warn(logComponent).Err(err).
					Str("member_name", memberName).
					Msg("BuildTeamHarness: TeamToolRail eager init failed")
			}
		}
		if teamPolicyRail != nil {
			agent.AddRail(teamPolicyRail)
		}
		if firstIterGate != nil {
			agent.AddRail(firstIterGate)
		}
		if teamWorkspaceRail != nil {
			agent.AddRail(teamWorkspaceRail)
		}
		if toolApprovalRail != nil {
			agent.AddRail(toolApprovalRail)
		}
		if teamPlanModeRail != nil {
			agent.AddRail(teamPlanModeRail)
		}
	}

	rails := &MountedRails{
		TeamTool:      teamToolRail,
		TeamPolicy:    teamPolicyRail,
		FirstIterGate: firstIterGate,
		TeamWorkspace: teamWorkspaceRail,
		ToolApproval:  toolApprovalRail,
		TeamPlanMode:  teamPlanModeRail,
	}
	if agent == nil {
		return NewTeamHarness(nil, rails, role, memberName, initialPlanMode)
	}
	// ⤴️ 25.1a：把 DeepAgent 包成 NativeHarness，作为团队成员的协议面载体
	native := NewNativeHarness(agent, agentSpec)
	return newTeamHarnessWithNative(native, agent, rails, role, memberName, initialPlanMode)
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

// Rails 返回已挂载的团队侧 Rails 句柄。
// Python: TeamHarness.rails property
func (h *TeamHarness) Rails() *MountedRails {
	return h.rails
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

// Steer 转向指令到底层 Agent。
// Python: TeamHarness.steer(content) → await self._deep_agent.steer(content)
// 已回填（#9.57）：转调 interactLoop，投递失败返回具名 error
func (h *TeamHarness) Steer(ctx context.Context, content string) error {
	return h.interactLoop(ctx, content, true)
}

// FollowUp 追加消息到底层 Agent。
// Python: TeamHarness.follow_up(content) → await self._deep_agent.follow_up(content)
func (h *TeamHarness) FollowUp(ctx context.Context, content string) error {
	return h.interactLoop(ctx, content, false)
}

// Abort 协作中止底层任务循环。
// Python: TeamHarness.abort() → await self._deep_agent.abort()
func (h *TeamHarness) Abort(_ context.Context) error {
	// DeepAgentInterface 未声明 RequestAbort（属 LoopCoordinatorInterface），按能力收窄
	if aborter, ok := h.deepAgent.(interface{ RequestAbort() }); ok {
		aborter.RequestAbort()
	}
	return nil
}

// RunStreaming 从底层 Agent 流式输出 chunk。
// Python: TeamHarness.run_streaming(inputs, session_id, team_session)
//
// 分支 1（teamSession 为 nil 且非 initialPlanMode）：直接以 session_id 调
// runner.RunAgentStreaming。
// 分支 2（有 teamSession，或 initialPlanMode）：先显式准备子 AgentSession
// （team.plan 需要把 Leader DeepAgent 种子化进 plan 模式），再以该会话
// 调 runner.RunAgentStreaming。
func (h *TeamHarness) RunStreaming(ctx context.Context, inputs map[string]any, sessionID string, teamSession any) (<-chan stream.Schema, error) {
	if teamSession == nil && !h.initialPlanMode {
		return h.streamWithSessionRef(ctx, inputs, runner.BySessionID(sessionID))
	}

	agentSess := h.prepareAgentSession(ctx, inputs, sessionID, teamSession)
	if agentSess == nil {
		return closedChunkChan(), nil
	}
	return h.streamWithSessionRef(ctx, inputs, runner.BySession(agentSess))
}

// streamWithSessionRef 以给定会话引用调用 Runner 的流式执行。
func (h *TeamHarness) streamWithSessionRef(ctx context.Context, inputs map[string]any, sessionRef runner.SessionRef) (<-chan stream.Schema, error) {
	if h.deepAgent == nil {
		return closedChunkChan(), nil
	}
	// DeepAgent 实现 BaseAgent（编译期断言），断言失败理论不可能
	baseAgent, ok := h.deepAgent.(agentinterfaces.BaseAgent)
	if !ok {
		logger.Warn(logComponent).
			Str("member_name", h.memberName).
			Msg("TeamHarness.RunStreaming: deepAgent does not implement BaseAgent, returning empty stream")
		return closedChunkChan(), nil
	}
	return runner.RunAgentStreaming(ctx, runner.ByAgent(baseAgent), inputs, sessionRef, nil, nil, nil)
}

// interactLoop 把运行中的输入投递给底层 DeepAgent 的任务循环。
// Python: TeamHarness.steer / follow_up —— 直接 await self._deep_agent.steer(...)
//
// 原实现是 TODO(#9.57) 空桩，导致运行中到达的第二条 chat.send 被静默丢弃
// （adapter → Interact → DeliverInput → StreamController.Steer → 这里 return nil）。
// DeepAgentInterface 未声明 steer/follow_up（只有 RequestAbort），故此处按具体类型收窄。
// 失败分支返回具名 error（投递失败必须让上层可回退，见 M-02），日志保留供定位。
func (h *TeamHarness) interactLoop(ctx context.Context, content string, steer bool) error {
	d, ok := h.deepAgent.(*agentcore.DeepAgent)
	if !ok || d == nil {
		logger.Warn(logComponent).
			Str("member_name", h.memberName).
			Bool("steer", steer).
			Msg("TeamHarness: deepAgent 不是 *agentcore.DeepAgent，输入无法投递")
		return errIncompatibleAgent
	}
	sess := h.getActiveSession()
	if sess == nil {
		logger.Warn(logComponent).
			Str("member_name", h.memberName).
			Bool("steer", steer).
			Msg("TeamHarness: 无活跃 Agent 会话，输入无法投递")
		return errNoActiveSession
	}
	if steer {
		return d.Steer(ctx, content, sess)
	}
	return d.FollowUp(ctx, content, "", sess)
}

// prepareAgentSession 为底层 Agent 准备本轮使用的会话。
// Python: TeamHarness._prepare_agent_session(inputs, session_id, team_session)（harness.py L303-330）
//
// 顺序：取 card → 优先从团队会话派生（不共享流写入器）→ PreRun(inputs)
// → 记录活跃会话 → 种子化初始 plan 模式。任一环节失败返回 nil，由调用方
// 退化为空流（对齐 Python 异常向上抛出前的空转语义，不吞 panic）。
func (h *TeamHarness) prepareAgentSession(ctx context.Context, inputs map[string]any, sessionID string, teamSession any) *session.Session {
	var card *agentschema.AgentCard
	if h.deepAgent != nil {
		card = h.deepAgent.Card()
	}

	var agentSess *session.Session
	if ts, ok := teamSession.(*session.AgentTeamSession); ok && ts != nil {
		// Python: team_session.create_agent_session(card=card, share_stream_writer=False)
		agentSess = ts.CreateAgentSession(card, "", false)
	} else {
		// Python: create_agent_session(session_id=session_id, card=card)
		agentSess = session.CreateAgentSession(sessionID, card, nil)
	}
	if agentSess == nil {
		return nil
	}

	if err := agentSess.PreRun(ctx, inputs); err != nil {
		logger.Error(logComponent).Err(err).
			Str("member_name", h.memberName).
			Msg("TeamHarness.prepareAgentSession: agent session pre_run failed")
		return nil
	}
	h.setActiveSession(agentSess)
	h.ensureInitialPlanMode(agentSess)
	return agentSess
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

// closedChunkChan 返回已关闭的空 chunk 通道。
func closedChunkChan() <-chan stream.Schema {
	ch := make(chan stream.Schema)
	close(ch)
	return ch
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
func (h *TeamHarness) RegisterRail(_ context.Context, rail any) error {
	r, ok := rail.(agentinterfaces.AgentRail)
	if !ok || r == nil {
		return fmt.Errorf("TeamHarness.RegisterRail: 不支持的 Rail 类型 %T", rail)
	}
	if h.deepAgent == nil {
		return errIncompatibleAgent
	}
	h.deepAgent.AddRail(r)
	return nil
}

// UnregisterRail 注销先前注册的 Rail。
// Python: TeamHarness.unregister_rail(rail) → await self._deep_agent.unregister_rail(rail)
func (h *TeamHarness) UnregisterRail(ctx context.Context, rail any) error {
	r, ok := rail.(agentinterfaces.AgentRail)
	if !ok || r == nil {
		return fmt.Errorf("TeamHarness.UnregisterRail: 不支持的 Rail 类型 %T", rail)
	}
	if h.deepAgent == nil {
		return errIncompatibleAgent
	}
	return h.deepAgent.UnregisterRail(ctx, r)
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
//
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

// buildDeepAgentFromSpec 从 DeepAgentSpec 物化 DeepAgent 实例。
// S-06: 对齐 Python DeepAgentSpec.build() (deep_agent_spec.py L461-515)
//
// Python 步骤：
//
//	Python: 1. llm_model = self.model.build()（TeamModelConfig → Model）
//	Python: 2. language = resolve_language(self.language)
//	3. vision_config / audio_config / workspace / rails / subagents / sys_operation 逐项 build
//	Python: 4. create_deep_agent(...)
//
// Go 差异：团队 Rails（TeamToolRail 等 6 个）由调用方（AgentConfigurator）先构建、
// 经 BuildTeamHarness 参数传入后挂载（挂载顺序有语义），不在此处从 RailSpec 构建；
// subagents/sys_operation 等字段由 CreateDeepAgent 内部处理。
// 构建失败时记日志并返回 nil（对齐 Python 异常向上传播由调用方兜底的容错语义，
// 避免团队启动因单个成员物化失败而中断）。
func buildDeepAgentFromSpec(spec atschema.DeepAgentSpec) *agentcore.DeepAgent {
	// Python: llm_model = self.model.build() if self.model else None
	var llmModel *llm.Model
	if spec.Model != nil {
		cfg := spec.Model.ModelClientConfig
		m, err := llm.NewModel(&cfg, spec.Model.ModelRequestConfig)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Str("member_card", spec.Card.GetName()).
				Msg("buildDeepAgentFromSpec: model build failed, agent will have no model")
		} else {
			llmModel = m
		}
	}

	// Python: workspace = self.workspace.build() if self.workspace else None
	// Go 差异：WorkspaceSpec → workspace.Workspace 由 CreateDeepAgent 内部
	// buildWorkspace 处理（harness_config.Params.Workspace 为 *Workspace），
	// 此处传入 nil 让 factory 走默认构建。

	params := hconfig.CreateDeepAgentParams{
		Model:                  llmModel,
		Card:                   spec.Card,
		SystemPrompt:           spec.SystemPrompt,
		ToolCards:              spec.ToolCards,
		Mcps:                   spec.Mcps,
		EnableTaskLoop:         true, // Python: enable_task_loop=True（build 中硬编码）
		EnableAsyncSubagent:    spec.EnableAsyncSubagent,
		AddGeneralPurposeAgent: spec.AddGeneralPurposeAgent,
		MaxIterations:          spec.MaxIterations,
		Skills:                 spec.Skills,
		EnableSkillDiscovery:   spec.EnableSkillDiscovery,
		Language:               spec.Language,
		AutoCreateWorkspace:    spec.AutoCreateWorkspace,
	}

	// Python: vision_config = self.vision_model.build() if self.vision_model else None
	if spec.VisionModel != nil {
		params.VisionModelConfig = &hschema.VisionModelConfig{
			APIKey:     spec.VisionModel.APIKey,
			BaseURL:    spec.VisionModel.BaseURL,
			Model:      spec.VisionModel.Model,
			MaxRetries: spec.VisionModel.MaxRetries,
		}
	}
	// Python: audio_config = self.audio_model.build() if self.audio_model else None
	if spec.AudioModel != nil {
		params.AudioModelConfig = &hschema.AudioModelConfig{
			APIKey:             spec.AudioModel.APIKey,
			BaseURL:            spec.AudioModel.BaseURL,
			TranscriptionModel: spec.AudioModel.TranscriptionModel,
			QAModel:            spec.AudioModel.QAModel,
			MaxRetries:         spec.AudioModel.MaxRetries,
		}
	}
	// Python: completion_timeout=self.completion_timeout
	if spec.CompletionTimeout > 0 {
		params.CompletionTimeout = spec.CompletionTimeout
	}
	// Python: enable_task_planning=self.enable_task_planning
	params.EnableTaskPlanning = spec.EnableTaskPlanning
	// Python: restrict_to_work_dir=self.restrict_to_sandbox（*bool，非零时设置）
	if spec.RestrictToSandbox {
		restrict := true
		params.RestrictToWorkDir = &restrict
	}
	// Python: prompt_mode=self.prompt_mode（字符串解析，失败保持默认 Full）
	if spec.PromptMode != "" {
		if pm, err := hschema.ParsePromptMode(spec.PromptMode); err == nil {
			params.PromptMode = pm
		} else {
			logger.Warn(logComponent).Err(err).Str("prompt_mode", spec.PromptMode).
				Msg("buildDeepAgentFromSpec: invalid prompt_mode, using default")
		}
	}

	agent, err := agentcore.CreateDeepAgent(context.Background(), params)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Msg("buildDeepAgentFromSpec: CreateDeepAgent failed")
		return nil
	}
	return agent
}
