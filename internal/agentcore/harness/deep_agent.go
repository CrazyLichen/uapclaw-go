package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	ctrlmodules "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hsecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/web_tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgent 面向生产的高级 Agent 封装层。
// 包装 ReActAgent + 任务循环 + Rails + 技能 + 子 Agent，
// 实现两层架构：外层（DeepAgent）负责多轮任务编排，
// 内层（ReActAgent）负责单轮 Think-Act-Observe 推理。
// Python: DeepAgent (openjiuwen/harness/deep_agent.py)
type DeepAgent struct {
	// card Agent 身份卡片
	card *agentschema.AgentCard
	// abilityManager 能力管理器
	abilityManager agentinterfaces.AbilityManagerInterface
	// callbackManager 回调管理器
	callbackManager *agentinterfaces.AgentCallbackManager

	// attachments 会话级动态提示词管理器（S1/S2：plan 指引、todo 提醒等走历史附件）
	attachments *hprompts.Manager
	// reactAgent 内层 ReActAgent
	reactAgent *agents.ReActAgent

	// innerInvokeOverride 内层 invoke 覆写函数（仅测试使用）。
	// 设置后，runSingleRoundInvoke 和 runSingleRoundStream 将调用此函数
	// 而非 reactAgent.Invoke/Stream，对齐 Python agent.set_react_agent(fake_react) 的能力。
	innerInvokeOverride func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
	// innerStreamOverride 内层 stream 覆写函数（仅测试使用）。
	innerStreamOverride func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error)
	// createSubagentOverride 子 Agent 创建覆写函数（仅测试使用）。
	// 设置后，CreateSubagent 将调用此函数而非工厂分派，
	// 对齐 Python 测试中 agent.create_subagent = _factory 的替身能力。
	createSubagentOverride func(ctx context.Context, subagentType string, subSessionID string) (hinterfaces.DeepAgentInterface, error)
	// scheduleAutoInvokeOverride 自动 invoke 调度覆写函数（生产 NativeHarness 与测试共用）。
	// 设置后，ScheduleAutoInvokeOnSpawnDone 将调用此函数而非延迟调度，
	// 对齐 Python 测试中 agent.schedule_auto_invoke_on_spawn_done = noop 的替身能力。
	scheduleAutoInvokeOverride func(ctx context.Context, steerText string, delay float64) error

	// deepConfig Harness 编排配置
	deepConfig *hschema.DeepAgentConfig
	// systemPromptBuilder 系统提示词构建器（与 ReActAgent 共享同一实例）
	systemPromptBuilder *hprompts.SystemPromptBuilder

	// loopCoordinator 循环协调器
	loopCoordinator *task_loop.LoopCoordinator
	// loopController 任务循环控制器
	loopController *task_loop.TaskLoopController
	// loopSession 活跃的循环会话
	loopSession *session.Session
	// boundSessionID 绑定的会话标识
	boundSessionID string
	// taskCompletionRail 任务完成 Rail（具体类型，BuildEvaluators 直接调用无需断言）
	// ⤴️ 9.12 回填：TaskCompletionRail
	taskCompletionRail *rails.TaskCompletionRail

	// pendingRails 待注册 Rail 列表
	pendingRails []agentinterfaces.AgentRail
	// staleRails 已废弃 Rail 列表
	staleRails []agentinterfaces.AgentRail
	// registeredRails 已注册 Rail 列表
	registeredRails []agentinterfaces.AgentRail
	// railsMu Rail 列表互斥锁
	railsMu sync.Mutex

	// initialized 是否已初始化
	initialized bool
	// initMu 初始化互斥锁，保护 ensureInitialized 的并发安全
	initMu sync.Mutex
	// cwdState agent 级 CWD 状态（初始化时建立，后续每次调用重新挂载到 ctx）
	// Python 用 contextvars 自动跨 await 传播；Go 必须在每次入口显式注入。
	cwdState *cwd.CwdState
	// spanState agent 级可观测 SpanState（同 cwdState，每次入口重新挂载）
	spanState *observability.OtelSpanState
	// invokeActive 是否有活跃 invoke
	invokeActive atomic.Bool
	// autoInvokeScheduled 是否已调度自动 invoke
	autoInvokeScheduled atomic.Bool

	// streamMu 流控制互斥锁
	streamMu sync.Mutex
	// streamCancel 流取消函数
	streamCancel context.CancelFunc

	// ── 交互监督器运行时（9.87）──
	// interactionSession 绑定的会话
	interactionSession *session.Session
	// interactionSessionID 绑定的会话标识（读它不加锁）
	interactionSessionID string
	// interactionPhase 相位机状态
	interactionPhase hschema.InteractionPhase
	// interactionStarted 是否已 Start
	interactionStarted bool
	// activeInteractionRound 当前执行中的轮次
	activeInteractionRound *hschema.ActiveInteractionRound
	// interactionEvents user/goal 双队列调度器（无内部锁，调用方持 controlMu）
	interactionEvents *task_loop.EventManager
	// goalCoordinator Goal 能力面（9.89 由 Start 注入 *goal.GoalManager）
	goalCoordinator goal.Coordinator
	// interactionOutput 输出租约管理器
	interactionOutput *hschema.OutputLeaseManager
	// interactionWakeup supervisor 唤醒通道（cap=1）
	interactionWakeup chan struct{}
	// interactionStateOnce 零件初始化只做一次
	interactionStateOnce sync.Once
	// interactionSupervisorMu 保护启停标记与 emit WaitGroup 的 Add
	interactionSupervisorMu sync.Mutex
	// interactionSupervisorRunning supervisor goroutine 是否在跑
	interactionSupervisorRunning bool
	// interactionStopping 正在停机（拒绝新的 emit goroutine）
	interactionStopping bool
	// interactionSupervisorCancel supervisor 取消函数
	interactionSupervisorCancel context.CancelFunc
	// interactionSupervisorDone supervisor 退出信号
	interactionSupervisorDone chan struct{}
	// interactionForwarderCancel 转发器取消函数
	interactionForwarderCancel context.CancelFunc
	// interactionForwarderDone 转发器退出信号
	interactionForwarderDone chan struct{}
	// interactionRoundCancel 当前轮取消函数（持 controlMu 读写）
	interactionRoundCancel context.CancelFunc
	// interactionRoundDone 当前轮退出信号（持 controlMu 读写）
	interactionRoundDone chan struct{}
	// interactionRoundForwarded 当前轮转发屏障（关闭=本轮 chunk 已全部入队）
	interactionRoundForwarded chan struct{}
	// interactionEmitWG 事件投递 goroutine 计数
	interactionEmitWG sync.WaitGroup
	// interactionStartMu Start/Stop 串行锁
	interactionStartMu sync.Mutex
	// interactionSendMu 输入/输出入口串行锁（先 send 后 control）
	interactionSendMu sync.Mutex
	// interactionControlMu 交互控制锁（相位、EventManager、租约、轮句柄）
	interactionControlMu sync.Mutex
	// worktreeSessionState 交互循环的 worktree 会话态 holder（每次 Start 新建）
	// Python: init_session_state() 的 ContextVar holder（worktree/session.py）
	worktreeSessionState *worktree.WorktreeSessionState
	// activeAgentTemplate 当前热加载的 AgentTemplate（⤵️ #9.3 由 load_agent_template 赋值，今天恒 nil）
	activeAgentTemplate *AgentTemplateRef
	// roundOverride 交互轮执行覆写（仅测试使用）
	roundOverride func(ctx context.Context, work *hschema.RoundWorkItem) hschema.RoundOutcome

	// sessionToolkit 会话工具包
	sessionToolkit *subagent.SessionToolkit

	// pendingHarnessConfigs 待加载的 Harness 配置路径
	pendingHarnessConfigs []string

	// uapswarmProjectDir 项目目录，对齐 Python: _jiuwenswarm_project_dir
	uapswarmProjectDir string
	// uapswarmCodeProjectDir 代码项目目录，对齐 Python: _jiuwenswarm_code_project_dir
	uapswarmCodeProjectDir string
	// uapswarmAdapterMode 适配器模式，对齐 Python: _jiuwenswarm_adapter_mode
	uapswarmAdapterMode string
	// uapswarmCodeTeamMember 是否为 code team 成员，对齐 Python: _jiuwenswarm_code_team_member
	uapswarmCodeTeamMember bool

	// configMu 配置读写锁
	configMu sync.RWMutex
}

// roundBoundaryFrame 内部轮边界哨兵帧。
// Python: _ROUND_BOUNDARY = object()（deep_agent.py L245）
// 只由 forwarder 识别，绝不转发给宿主；必须与普通 chunk 走同一条 session 流，
// 否则无法保证"本轮 chunk 已全部入队"的顺序性。
type roundBoundaryFrame struct{}

// AgentTemplateRef 当前热加载的 AgentTemplate 引用。
// Python: self._active_agent_template: tuple[str, str] | None（deep_agent.py L333）
type AgentTemplateRef struct {
	// LoadID 加载记录标识
	LoadID string
	// RoleName 模板声明的角色名（expert_role 附件文案用）
	RoleName string
}

// RoundExpectation 取消时的代际校验条件（nil 字段表示不校验）。
// Python: _cancel_active_round 的 expected_run_kind / expected_goal_id / expected_revision
// ⚠️ Revision 用指针：上游 revision 默认 0 且 0 是合法值，不能用 0 表示"不校验"。
type RoundExpectation struct {
	// RunKind 期望的运行类型
	RunKind *hschema.RoundWorkKind
	// GoalID 期望的目标标识（空串表示不校验）
	GoalID string
	// Revision 期望的修订号
	Revision *int
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// defaultAutoInvokeDelay 自动 invoke 延迟秒数
	// Python: schedule_auto_invoke_on_spawn_done delay=0.5
	defaultAutoInvokeDelay = 0.5
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// bridgeEvents 桥接到内层 ReActAgent 的事件集合
	// Python: _BRIDGE_EVENTS
	bridgeEvents = map[agentinterfaces.AgentCallbackEvent]bool{
		agentinterfaces.CallbackBeforeModelCall:  true,
		agentinterfaces.CallbackAfterModelCall:   true,
		agentinterfaces.CallbackOnModelException: true,
		agentinterfaces.CallbackBeforeToolCall:   true,
		agentinterfaces.CallbackAfterToolCall:    true,
		agentinterfaces.CallbackOnToolException:  true,
		// 下列三个事件由**内层** ReActAgent 触发，必须注册到内层 callback manager，
		// 否则经 DeepAgent.register_rail 注册的 rail 永远收不到（会落到「unknown
		// rail event」兜底分支被注册到外层）。
		// Python: _BRIDGE_EVENTS 的 AFTER_REACT_ITERATION / ON_USER_MESSAGE /
		// BEFORE_STEERING_DRAIN 三条，各带同样理由的注释。
		agentinterfaces.CallbackAfterReactIteration: true,
		agentinterfaces.CallbackOnUserMessage:       true,
		agentinterfaces.CallbackBeforeSteeringDrain: true,
	}

	// outerOnlyEvents 仅注册到外层 DeepAgent 的事件集合
	// Python: _OUTER_ONLY_EVENTS
	outerOnlyEvents = map[agentinterfaces.AgentCallbackEvent]bool{
		agentinterfaces.CallbackBeforeInvoke: true,
		agentinterfaces.CallbackAfterInvoke:  true,
	}

	// deepEvents DeepAgent 扩展事件集合
	// Python: _DEEP_EVENTS
	deepEvents = map[agentinterfaces.AgentCallbackEvent]bool{
		agentinterfaces.CallbackBeforeTaskIteration: true,
		agentinterfaces.CallbackAfterTaskIteration:  true,
	}

	// 编译期接口检查
	_ hinterfaces.DeepAgentInterface = (*DeepAgent)(nil)
	_ agentinterfaces.BaseAgent      = (*DeepAgent)(nil)

	// ErrNoActiveLoop 任务循环控制器已清理。
	// 典型场景是轮次刚结束、controller 已被 forceCleanupController 回收，
	// 此时投递运行中输入的调用方需要能区分"正常竞态"与真实故障。
	ErrNoActiveLoop = errors.New("deep agent: 任务循环控制器未激活")
	// ErrNoSession steer/follow_up 既无显式会话也无 loop 会话。
	ErrNoSession = errors.New("deep agent: 无可用会话")

	// ErrInteractionNotRunning 交互监督器未启动或已终止。
	// Python: RuntimeError("interaction_terminated")
	ErrInteractionNotRunning = errors.New("interaction_terminated")
)

// defaultDirectToolNames 渐进模式下保持直接可调用的工具名集合。
// Python: deep_agent.py _DEFAULT_DIRECT_TOOL_NAMES（36 项）
// Go 侧差异：上游 subagent_spawn/wait/list/send_input/close/resume 六项在 Go 侧
// 分别注册为 sessions_spawn/sessions_list/sessions_cancel；未实现的工具名保留原字符串
// （策略只对已注册工具生效，多余名字无副作用）。
var defaultDirectToolNames = []string{
	"tool_search", "tool_call",
	"read_file", "write_file", "edit_file", "glob", "grep", "bash",
	"task_tool", "sessions_spawn", "sessions_list", "sessions_cancel",
	"ask_user", "todo_create", "todo_list", "todo_get", "todo_modify", "skill_tool",
	"memory_search", "memory_get", "free_search", "paid_search", "fetch_webpage",
	"write_memory", "edit_memory", "read_memory", "ltm_search", "ltm_search_summary",
	"send_file_to_user", "enter_plan_mode", "exit_plan_mode",
	"skill_branch_explore", "skill_index_build",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeepAgent 创建 DeepAgent 实例。
// Python: DeepAgent.__init__(card) (line 126)
func NewDeepAgent(card *agentschema.AgentCard) *DeepAgent {
	d := &DeepAgent{
		card:            card,
		abilityManager:  ability.NewAbilityManager(runner.GetResourceMgr()),
		callbackManager: agentinterfaces.NewAgentCallbackManager(card.ID),
		attachments:     hprompts.NewManager(),
	}
	return d
}

// PromptAttachments 返回会话级动态提示词管理器。
// Python: DeepAgent.prompt_attachment_manager
func (d *DeepAgent) PromptAttachments() *hprompts.Manager { return d.attachments }

// ConfigureDeepConfig 配置 DeepAgent（使用 DeepAgentConfig）。
// 首次调用执行 initialConfigure，后续调用执行 hotReconfigure。
// Python: DeepAgent.configure(config) (line 150)
func (d *DeepAgent) ConfigureDeepConfig(ctx context.Context, deepCfg *hschema.DeepAgentConfig) error {
	// 锁序必须为 initMu → configMu：ensureInitialized 持 initMu 后再取 configMu，
	// 反向（configMu → initMu）会构成 ABBA 死锁（热配置与首次 Invoke 并发即可触发）。
	d.initMu.Lock()
	defer d.initMu.Unlock()

	d.configMu.Lock()
	d.filterDisabledTools(deepCfg)

	if d.deepConfig == nil {
		d.initialConfigure(ctx, deepCfg)
	} else {
		d.hotReconfigure(ctx, deepCfg)
	}
	d.configMu.Unlock()

	d.initialized = false
	return nil
}

// Configure 配置 Agent。满足 BaseAgent 接口。
// 注意：DeepAgent 的配置必须通过 ConfigureDeepConfig() 传入 DeepAgentConfig。
// 此方法仅满足接口签名，如果传入非 DeepAgentConfig 将返回错误。
// Python: DeepAgent.configure(config) (line 150)
func (d *DeepAgent) Configure(ctx context.Context, config agentinterfaces.AgentConfig) error {
	// DeepAgentConfig 不实现 AgentConfig 接口，
	// 调用方应使用 ConfigureDeepConfig() 代替
	return exception.BuildError(exception.StatusDeepagentConfigParamError,
		exception.WithMsg("DeepAgent 请使用 ConfigureDeepConfig() 传入 *DeepAgentConfig"))
}

// Invoke 非流式执行 Agent。
// Python: DeepAgent.invoke(inputs, session) (line 2261)
func (d *DeepAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	var initErr error
	ctx, initErr = d.ensureInitialized(ctx)
	if initErr != nil {
		return nil, initErr
	}

	d.configMu.RLock()
	reactAgent := d.reactAgent
	deepConfig := d.deepConfig
	d.configMu.RUnlock()

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 Configure()"))
	}

	invokeInputs, normErr := d.normalizeInputs(inputs)
	if normErr != nil {
		return nil, normErr
	}
	agentOpts := agentinterfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session
	// ownsSession 标记会话是否由本次调用创建：创建方负责 post_run 收尾。
	// Python: owns_session = session is None（deep_agent.py L3355）
	ownsSession := sess == nil
	prepared, prepErr := d.prepareSingleRoundSession(ctx, invokeInputs, sess)
	if prepErr != nil {
		return nil, prepErr
	}
	sess = prepared

	cbc := agentinterfaces.NewAgentCallbackContext(d, invokeInputs, sess)

	d.invokeActive.Store(true)
	defer d.invokeActive.Store(false)

	var result map[string]any

	// Python: async with ctx.lifecycle(BEFORE_INVOKE, AFTER_INVOKE):
	// 使用 FireLifecycle 包裹核心执行逻辑，保证 AFTER_INVOKE 必定触发
	if err := cbc.FireLifecycle(ctx, agentinterfaces.CallbackBeforeInvoke, agentinterfaces.CallbackAfterInvoke, func() error {
		if deepConfig != nil && deepConfig.EnableTaskLoop && !isResumeInput(invokeInputs) {
			r, err := d.runTaskLoopInvoke(ctx, cbc, sess)
			if err != nil {
				return err
			}
			result = r
		} else {
			r, err := d.runSingleRoundInvoke(ctx, cbc, sess)
			if err != nil {
				return err
			}
			result = r
		}
		invokeInputs.Result = result
		return nil
	}); err != nil {
		return nil, err
	}

	if sess != nil {
		d.saveState(sess, nil)
		d.clearState(sess, false)
	}
	// 自建会话由本次调用收尾（关流 + 提交），与 Python 的 owns_session 分支一致。
	// Python: if owns_session and session is not None: await session.post_run()（deep_agent.py L3411-3412）
	if ownsSession {
		postRunOwnedSession(ctx, sess)
	}
	return result, nil
}

// Stream 流式执行 Agent。
// Python: DeepAgent.stream(inputs, session, stream_modes) (line 2302)
func (d *DeepAgent) Stream(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	var initErr error
	ctx, initErr = d.ensureInitialized(ctx)
	if initErr != nil {
		return nil, initErr
	}

	if err := d.drainPendingHarnessConfigs(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("drainPendingHarnessConfigs failed")
	}

	d.configMu.RLock()
	reactAgent := d.reactAgent
	deepConfig := d.deepConfig
	d.configMu.RUnlock()

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 Configure()"))
	}

	invokeInputs, normErr := d.normalizeInputs(inputs)
	if normErr != nil {
		return nil, normErr
	}
	agentOpts := agentinterfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session
	streamModes := agentOpts.StreamModes
	// Python: owns_session = session is None（deep_agent.py L3444）
	ownsSession := sess == nil
	prepared, prepErr := d.prepareSingleRoundSession(ctx, invokeInputs, sess)
	if prepErr != nil {
		return nil, prepErr
	}
	sess = prepared

	cbc := agentinterfaces.NewAgentCallbackContext(d, invokeInputs, sess)

	outCh := make(chan stream.Schema, 64)

	d.invokeActive.Store(true)

	go func() {
		defer d.invokeActive.Store(false)
		defer close(outCh)

		// Python: async with ctx.lifecycle(BEFORE_INVOKE, AFTER_INVOKE):
		// 使用 FireLifecycle 包裹 chunk 循环，保证 AFTER_INVOKE 必定触发
		_ = cbc.FireLifecycle(ctx, agentinterfaces.CallbackBeforeInvoke, agentinterfaces.CallbackAfterInvoke, func() error {
			var streamResult map[string]any
			var streamOutputParts []string

			if deepConfig != nil && deepConfig.EnableTaskLoop && !isResumeInput(invokeInputs) {
				ch, err := d.runTaskLoopStream(ctx, invokeInputs, sess, streamModes)
				if err != nil {
					// ctx 取消不是错误：不写错误帧
					// （对齐 Python：取消时 generator 终止，不产出 error 帧）
					if !utils.Canceled(ctx) {
						logger.Error(logComponent).Err(err).Msg("runTaskLoopStream start failed")
						emitStreamError(outCh, err)
					}
					return err
				}
				for chunk := range ch {
					chunkResult := resultFromStreamChunk(chunk, &streamOutputParts)
					if chunkResult != nil {
						streamResult = chunkResult
					}
					outCh <- chunk
				}
			} else {
				ch, err := d.runSingleRoundStream(ctx, invokeInputs, sess, streamModes)
				if err != nil {
					// ctx 取消不是错误：不写错误帧（同上）
					if !utils.Canceled(ctx) {
						logger.Error(logComponent).Err(err).Msg("runSingleRoundStream start failed")
						emitStreamError(outCh, err)
					}
					return err
				}
				for chunk := range ch {
					chunkResult := resultFromStreamChunk(chunk, &streamOutputParts)
					if chunkResult != nil {
						streamResult = chunkResult
					}
					outCh <- chunk
				}
			}

			// Python: line 2354-2360: stream_result 合并
			if streamResult == nil && len(streamOutputParts) > 0 {
				streamResult = map[string]any{
					"output":      joinStrings(streamOutputParts),
					"result_type": "answer",
				}
			}
			if streamResult != nil {
				invokeInputs.Result = streamResult
			}
			return nil
		})

		if sess != nil {
			d.saveState(sess, nil)
			d.clearState(sess, false)
		}
		// 自建会话由本次调用收尾（与 Invoke 同一口径）。
		// Python: if owns_session and session is not None: await session.post_run()（deep_agent.py L3528-3529）
		if ownsSession {
			postRunOwnedSession(ctx, sess)
		}
	}()

	return outCh, nil
}

// prepareSingleRoundSession 在单轮模式下按 conversation_id 自建会话。
// 任务循环模式（且非中断恢复）下会话归调用方所有，这里不建。
// Python: DeepAgent._prepare_single_round_session (deep_agent.py L2053-2075)
func (d *DeepAgent) prepareSingleRoundSession(
	ctx context.Context,
	inputs *agentinterfaces.InvokeInputs,
	sess sessioninterfaces.SessionFacade,
) (sessioninterfaces.SessionFacade, error) {
	if sess != nil || inputs.ConversationID == "" {
		return sess, nil
	}
	d.configMu.RLock()
	deepConfig := d.deepConfig
	d.configMu.RUnlock()
	if deepConfig != nil && deepConfig.EnableTaskLoop && !isResumeInput(inputs) {
		return nil, nil // 任务循环会话由调用方持有
	}
	envs := make(map[string]any)
	if inputs.ParentSessionID != "" {
		envs[kv_cache.KVCacheAffinityParentSessionIDEnv] = inputs.ParentSessionID
	}
	newSess := session.NewSession(
		session.WithSessionID(inputs.ConversationID),
		session.WithCard(d.card),
		session.WithEnvs(envs),
	)
	if err := newSess.PreRun(ctx, toEffectiveInputs(inputs)); err != nil {
		return nil, err
	}
	return newSess, nil
}

// postRunOwnedSession 对本次调用自建的会话执行 post_run（关流 + 提交）。
// 会话实现不提供 post_run 时静默跳过（Python 侧会话必有该方法）。
func postRunOwnedSession(ctx context.Context, sess sessioninterfaces.SessionFacade) {
	if sess == nil {
		return
	}
	runner, ok := sess.(interface{ PostRun(context.Context) error })
	if !ok {
		return
	}
	if err := runner.PostRun(ctx); err != nil {
		logger.Debug(logComponent).Err(err).Msg("owned session post_run failed")
	}
}

// Card 返回 Agent 身份卡片。
// Python: BaseAgent.card 属性
func (d *DeepAgent) Card() *agentschema.AgentCard {
	return d.card
}

// Config 返回 nil。与 Python 对齐，DeepAgent.config 返回 None。
// 需要 DeepAgent 配置的消费者应使用 DeepConfig()，
// 需要内层 Agent 配置的消费者应使用 ReactAgent().Config()。
// Python: DeepAgent.config = None (line 474)
func (d *DeepAgent) Config() agentinterfaces.AgentConfig {
	return nil
}

// AbilityManager 返回能力管理器。
// Python: BaseAgent.ability_manager 属性
func (d *DeepAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return d.abilityManager
}

// SystemPromptBuilder 返回系统提示词构建器。
// Python: DeepAgent.system_prompt_builder 属性
func (d *DeepAgent) SystemPromptBuilder() saprompts.SystemPromptBuilderInterface {
	if d.systemPromptBuilder != nil {
		return d.systemPromptBuilder.SystemPromptBuilder
	}
	return nil
}

// CallbackManager 返回回调管理器。
// Python: BaseAgent.agent_callback_manager 属性
func (d *DeepAgent) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return d.callbackManager
}

// RegisterCallback 注册回调。
// Python: BaseAgent.register_callback(event, callback, priority)
func (d *DeepAgent) RegisterCallback(ctx context.Context, event agentinterfaces.AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) error {
	d.callbackManager.RegisterCallback(ctx, event, fn, opts...)
	return nil
}

// RegisterRail 立即注册 Rail（不排队，不走 ensureInitialized）。
// 与 pending 路径（AddRail → ensureInitialized）的关键差异：这条路跳过了
// pending 循环，因此 DeepAgentRail 的 sys_operation / workspace 注入必须
// 在这里自己做——否则运行期按模式注册的 rail（MemoryRail、
// ExternalMemoryRail、SkillEvolutionRail 等）拿到的永远是 nil，
// 表现为 read_memory 恒返回「工作空间未初始化」、fs 工具 nil 解引用。
// Python: BaseAgent.register_rail(rail) (deep_agent.py L1187-1196)
func (d *DeepAgent) RegisterRail(ctx context.Context, r agentinterfaces.AgentRail, opts ...cb.CallbackOption) error {
	d.railsMu.Lock()
	// 检查是否为 TaskCompletionRail
	if tc, ok := r.(*rails.TaskCompletionRail); ok {
		d.taskCompletionRail = tc
	}
	d.railsMu.Unlock()

	// Python: if isinstance(rail, DeepAgentRail):
	//   rail.set_sys_operation(self.deep_config.sys_operation)
	//   rail.set_workspace(self.deep_config.workspace)
	// 注入必须在 Init 之前：rail 的 Init 会用这两个引用构造工具上下文。
	if provider, ok := r.(rails.DeepAgentRailProvider); ok {
		d.configMu.RLock()
		cfg := d.deepConfig
		d.configMu.RUnlock()
		if cfg != nil {
			provider.SetSysOperation(cfg.SysOperation)
			provider.SetWorkspace(cfg.Workspace)
		}
	}

	if err := r.Init(ctx, d); err != nil {
		return err
	}
	d.registerRailSelective(ctx, r)

	d.railsMu.Lock()
	d.registeredRails = append(d.registeredRails, r)
	d.railsMu.Unlock()
	return nil
}

// UnregisterRail 注销 Rail。
// Python: BaseAgent.unregister_rail(rail) (line 1198)
func (d *DeepAgent) UnregisterRail(ctx context.Context, r agentinterfaces.AgentRail) error {
	d.railsMu.Lock()
	// 从 pendingRails 中移除
	d.pendingRails = removeRailByRef(d.pendingRails, r)
	// 从 registeredRails 中移除
	d.registeredRails = removeRailByRef(d.registeredRails, r)

	// 检查 TaskCompletionRail
	if tc, ok := r.(*rails.TaskCompletionRail); ok && d.taskCompletionRail == tc {
		d.taskCompletionRail = nil
	}
	d.railsMu.Unlock()

	// 从外层 DeepAgent 注销
	_ = d.callbackManager.UnregisterRail(ctx, r)

	// 从内层 ReActAgent 注销桥接回调
	d.configMu.RLock()
	reactAgent := d.reactAgent
	d.configMu.RUnlock()
	if reactAgent != nil {
		_ = reactAgent.CallbackManager().UnregisterRail(ctx, r)
	}

	_ = r.Uninit(d)
	return nil
}

// ReactAgent 返回内层 ReActAgent 实例。
// Python: DeepAgent.react_agent 属性 (line 479)
func (d *DeepAgent) ReactAgent() *agents.ReActAgent {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.reactAgent
}

// LoopCoordinator 返回循环协调器（可能为 nil）。
// Python: DeepAgent.loop_coordinator 属性 (line 677)
func (d *DeepAgent) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.loopCoordinator == nil {
		return nil
	}
	return d.loopCoordinator
}

// LoopController 返回任务循环控制器。
// Python: DeepAgent.loop_controller 属性 (line 682)
func (d *DeepAgent) LoopController() controller.ControllerInterface {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.loopController == nil {
		return nil
	}
	return d.loopController
}

// EventHandler 返回事件处理器。
// Python: DeepAgent.event_handler 属性 (line 694)
func (d *DeepAgent) EventHandler() ctrlmodules.EventHandler {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.loopController == nil {
		return nil
	}
	return d.loopController.EventHandler()
}

// LoadState 从会话加载 DeepAgentState。
// Python: DeepAgent.load_state(session) (line 1790)
func (d *DeepAgent) LoadState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	// 两级缓存：runtime attribute → persisted session state
	state := d.readRuntimeState(sess)
	if state != nil {
		return state
	}
	// 从持久化状态加载
	data, err := sess.GetState(sessstate.StringKey(hschema.SessionStateKey))
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("LoadState: GetState failed, using default state")
	}
	var loaded hschema.DeepAgentState
	if dataMap, ok := data.(map[string]any); ok {
		loaded = hschema.DeepAgentState{}.FromSessionDict(dataMap)
	} else {
		loaded = hschema.NewDeepAgentState()
	}
	d.writeRuntimeState(sess, &loaded)
	return &loaded
}

// DeepConfig 返回 DeepAgent 配置。
// Python: DeepAgent.deep_config 属性 (line 474)
func (d *DeepAgent) DeepConfig() *hschema.DeepAgentConfig {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.deepConfig
}

// IsInvokeActive 判断是否有活跃的 invoke。
// Python: DeepAgent.is_invoke_active 属性 (line 460)
func (d *DeepAgent) IsInvokeActive() bool {
	return d.invokeActive.Load()
}

// IsAutoInvokeScheduled 判断是否已调度自动 invoke。
// Python: DeepAgent.is_auto_invoke_scheduled 属性 (line 465)
func (d *DeepAgent) IsAutoInvokeScheduled() bool {
	return d.autoInvokeScheduled.Load()
}

// SetAutoInvokeScheduled 设置自动 invoke 调度标记。
// Python: DeepAgent.set_auto_invoke_scheduled(is_scheduled) (line 469)
func (d *DeepAgent) SetAutoInvokeScheduled(scheduled bool) {
	d.autoInvokeScheduled.Store(scheduled)
}

// ScheduleAutoInvokeOnSpawnDone 延迟调度自动 invoke。
// Python: DeepAgent.schedule_auto_invoke_on_spawn_done(query, delay) (line 1959)
func (d *DeepAgent) ScheduleAutoInvokeOnSpawnDone(ctx context.Context, steerText string, delay float64) error {
	d.autoInvokeScheduled.Store(true)

	// 覆写钩子：优先使用 scheduleAutoInvokeOverride（生产 NativeHarness 与测试共用）
	// 对齐 Python 测试 agent.schedule_auto_invoke_on_spawn_done = noop 的直接替换语义
	d.configMu.RLock()
	overrideFn := d.scheduleAutoInvokeOverride
	d.configMu.RUnlock()
	if overrideFn != nil {
		return overrideFn(ctx, steerText, delay)
	}

	go func() {
		time.Sleep(time.Duration(delay * float64(time.Second)))
		d.autoInvokeScheduled.Store(false)

		if d.invokeActive.Load() {
			return
		}

		d.configMu.RLock()
		loopSess := d.loopSession
		d.configMu.RUnlock()

		if loopSess == nil {
			logger.Warn(logComponent).Msg("[AutoInvoke] session cleaned up during delay, skipping")
			return
		}

		invokeCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()

		_, err := d.Invoke(invokeCtx, map[string]any{"query": steerText},
			agentinterfaces.WithSession(loopSess))
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("[AutoInvoke] auto invoke failed")
		}
	}()

	return nil
}

// CreateSubagent 创建子 Agent 实例。
// ⤵️ 9.3 / 9.25-9.27 / 9.31 回填：browser/code/research/mobile_gui 工厂待实现
// Python: DeepAgent.create_subagent(subagent_type, subsession_id) (line 898)
func (d *DeepAgent) CreateSubagent(ctx context.Context, subagentType string, subSessionID string) (hinterfaces.DeepAgentInterface, error) {
	// 测试覆写钩子：优先使用 createSubagentOverride（仅测试使用）
	// 对齐 Python 测试 agent.create_subagent = _factory 的直接替换语义
	d.configMu.RLock()
	overrideFn := d.createSubagentOverride
	d.configMu.RUnlock()
	if overrideFn != nil {
		return overrideFn(ctx, subagentType, subSessionID)
	}

	spec := d.findSubagentSpec(subagentType)
	if spec == nil {
		return nil, exception.BuildError(exception.StatusDeepagentCreateSubagentNotFound,
			exception.WithMsg(fmt.Sprintf("子 Agent 规格未找到: %s", subagentType)))
	}

	// 如果 spec 本身就是 *DeepAgent 实例，直接返回
	if deepAgent, ok := spec.(*DeepAgent); ok {
		logger.Info(logComponent).Str("subagent_type", subagentType).Msg("DeepAgent instance acquired, returning directly")
		return deepAgent, nil
	}

	// 从 spec 获取 SubAgentConfig（SubagentSpec 接口的另一个实现）
	subCfg, ok := spec.(*hschema.SubAgentConfig)
	if !ok {
		return nil, exception.BuildError(exception.StatusDeepagentCreateSubagentNotFound,
			exception.WithMsg(fmt.Sprintf("子 Agent 规格类型不支持: %T", spec)))
	}

	// 配置合并逻辑 — 对齐 Python L938-982
	kwargs := d.buildSubagentCreateKwargs(subCfg, subSessionID)

	// 工厂分派
	normalizedFactory := ""
	if subCfg.FactoryName != "" {
		normalizedFactory = normalizeFactoryName(subCfg.FactoryName)
	}

	switch normalizedFactory {
	case "browser_agent", "browser_runtime":
		return CreateBrowserAgent(ctx, kwargs)
	case "code_agent":
		return CreateCodeAgent(ctx, kwargs)
	case "research_agent":
		return CreateResearchAgent(ctx, kwargs)
	case "mobile_gui_agent", "mobile_agent":
		return nil, fmt.Errorf("mobile_gui_agent 工厂尚未实现，⤵️ 9.31 回填")
	default:
		// 通过 CreateDeepAgent 工厂创建子 Agent 实例
		createParams := buildCreateParamsFromSubagentKwargs(kwargs)
		// 子 Agent 创建独立 CwdState，实现 inter-Agent 隔离
		// Python: DeepAgent.create_subagent 中 init_cwd 调用
		var subWwRootPath string
		if createParams.Workspace != nil {
			subWwRootPath = createParams.Workspace.RootPath
		}
		if subWwRootPath == "" {
			currentCwd, cwdErr := cwd.GetCwd(ctx)
			if cwdErr != nil {
				return nil, cwdErr
			}
			subWwRootPath = currentCwd
		}
		subCwdState := cwd.InitCwd(subWwRootPath, cwd.WithWorkspace(subWwRootPath))
		subCtx := cwd.WithCwdState(ctx, subCwdState)
		// 子 Agent 创建独立 SpanState，实现 inter-Agent 隔离
		subSpanState := observability.InitSpanState()
		subCtx = observability.WithSpanState(subCtx, subSpanState)
		subAgent, createErr := CreateDeepAgent(subCtx, createParams)
		if createErr != nil {
			return nil, fmt.Errorf("创建子 Agent 失败: %w", createErr)
		}
		return subAgent, nil
	}
}

// SetSessionToolkit 设置会话工具包。
// Python: DeepAgent.set_session_toolkit(toolkit) (line 146)
func (d *DeepAgent) SetSessionToolkit(toolkit *subagent.SessionToolkit) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.sessionToolkit = toolkit
}

// GetSessionToolkit 返回会话工具包（可能为 nil）。
// 对齐 Python 测试 getattr(agent, "_session_toolkit") 的读取路径。
func (d *DeepAgent) GetSessionToolkit() *subagent.SessionToolkit {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.sessionToolkit
}

// UapswarmProjectDir 获取项目目录。
// Python: DeepAgent._jiuwenswarm_project_dir
func (d *DeepAgent) UapswarmProjectDir() string {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.uapswarmProjectDir
}

// SetUapswarmProjectDir 设置项目目录。
// Python: setattr(instance, "_jiuwenswarm_project_dir", value)
func (d *DeepAgent) SetUapswarmProjectDir(dir string) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.uapswarmProjectDir = dir
}

// UapswarmCodeProjectDir 获取代码项目目录。
// Python: DeepAgent._jiuwenswarm_code_project_dir
func (d *DeepAgent) UapswarmCodeProjectDir() string {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.uapswarmCodeProjectDir
}

// SetUapswarmCodeProjectDir 设置代码项目目录。
// Python: setattr(instance, "_jiuwenswarm_code_project_dir", value)
func (d *DeepAgent) SetUapswarmCodeProjectDir(dir string) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.uapswarmCodeProjectDir = dir
}

// UapswarmAdapterMode 获取适配器模式。
// Python: DeepAgent._jiuwenswarm_adapter_mode
func (d *DeepAgent) UapswarmAdapterMode() string {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.uapswarmAdapterMode
}

// SetUapswarmAdapterMode 设置适配器模式。
// Python: setattr(instance, "_jiuwenswarm_adapter_mode", value)
func (d *DeepAgent) SetUapswarmAdapterMode(mode string) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.uapswarmAdapterMode = mode
}

// UapswarmCodeTeamMember 获取是否为 code team 成员。
// Python: DeepAgent._jiuwenswarm_code_team_member
func (d *DeepAgent) UapswarmCodeTeamMember() bool {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.uapswarmCodeTeamMember
}

// SetUapswarmCodeTeamMember 设置是否为 code team 成员。
// Python: setattr(instance, "_jiuwenswarm_code_team_member", True)
func (d *DeepAgent) SetUapswarmCodeTeamMember(v bool) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.uapswarmCodeTeamMember = v
}

// SetReactAgent 注入内层 Agent 实现（用于运行时接线/测试）。
// Python: DeepAgent.set_react_agent(react_agent, initialized) (line 444)
func (d *DeepAgent) SetReactAgent(reactAgent *agents.ReActAgent, initd bool) {
	// 锁序同 ConfigureDeepConfig：initMu → configMu
	d.initMu.Lock()
	defer d.initMu.Unlock()
	d.configMu.Lock()
	d.reactAgent = reactAgent
	d.configMu.Unlock()
	d.initialized = initd
}

// SetInnerInvokeOverride 设置内层 invoke 覆写函数（仅测试使用）。
// 设置后，runSingleRoundInvoke 和 executor.ExecuteAbility 将调用此函数而非 reactAgent.Invoke。
// 对齐 Python agent.set_react_agent(fake_react) 的测试替身能力。
func (d *DeepAgent) SetInnerInvokeOverride(fn func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.innerInvokeOverride = fn
}

// InnerInvokeOverride 返回内层 invoke 覆写函数（仅测试使用）。
// executor 在 TaskLoop 模式下优先使用此函数替代 reactAgent.Invoke。
func (d *DeepAgent) InnerInvokeOverride() func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.innerInvokeOverride
}

// SetInnerStreamOverride 设置内层 stream 覆写函数（仅测试使用）。
func (d *DeepAgent) SetInnerStreamOverride(fn func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error)) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.innerStreamOverride = fn
}

// SetCreateSubagentOverride 设置子 Agent 创建覆写函数（仅测试使用）。
// 设置后，CreateSubagent 将调用此函数而非工厂分派。
// 对齐 Python 测试中 agent.create_subagent = _factory 的替身能力。
func (d *DeepAgent) SetCreateSubagentOverride(fn func(ctx context.Context, subagentType string, subSessionID string) (hinterfaces.DeepAgentInterface, error)) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.createSubagentOverride = fn
}

// CreateSubagentOverride 返回子 Agent 创建覆写函数（仅测试使用）。
func (d *DeepAgent) CreateSubagentOverride() func(ctx context.Context, subagentType string, subSessionID string) (hinterfaces.DeepAgentInterface, error) {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.createSubagentOverride
}

// SetScheduleAutoInvokeOverride 设置自动 invoke 调度覆写函数（生产 NativeHarness 与测试共用）。
// 对齐 Python 测试中 agent.schedule_auto_invoke_on_spawn_done = noop 的替身能力。
func (d *DeepAgent) SetScheduleAutoInvokeOverride(fn func(ctx context.Context, steerText string, delay float64) error) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.scheduleAutoInvokeOverride = fn
}

// IsInitialized 返回是否已完成懒初始化。
// Python: DeepAgent.is_initialized 属性 (line 455)
func (d *DeepAgent) IsInitialized() bool {
	d.initMu.Lock()
	v := d.initialized
	d.initMu.Unlock()
	return v
}

// AddRail 同步排队一个 Rail 以便延迟注册。
// Python: DeepAgent.add_rail(rail) (line 1139)
//
// M-02: 加入 DeepAgentInterface（对齐 Python add_rail 接口面）。
// 返回值从链式 *DeepAgent 改为无返回值——Python 链式返回 self 在 Go 接口无意义，
// 且全项目调用方均为语句形式无链式使用。
func (d *DeepAgent) AddRail(r agentinterfaces.AgentRail) {
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	if _, ok := r.(*rails.TaskCompletionRail); ok {
		// 移除已有的 TaskCompletionRail
		d.pendingRails = filterRailsNotType(d.pendingRails, isTaskCompletionRail)
	}
	d.pendingRails = append(d.pendingRails, r)
}

// FindRailsByType 返回排队和已注册中匹配指定类型的 Rail。
// Python: DeepAgent.find_rails_by_type(rail_types) (line 1155)
func (d *DeepAgent) FindRailsByType(railTypes ...reflect.Type) []agentinterfaces.AgentRail {
	if len(railTypes) == 0 {
		return nil
	}
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	var result []agentinterfaces.AgentRail
	for _, r := range d.pendingRails {
		if matchType(r, railTypes) {
			result = append(result, r)
		}
	}
	for _, r := range d.registeredRails {
		if matchType(r, railTypes) {
			result = append(result, r)
		}
	}
	return result
}

// StripRailsByType 按类型移除排队 Rail 并将已注册 Rail 标记为废弃。
// Python: DeepAgent.strip_rails_by_type(rail_types) (line 1166)
func (d *DeepAgent) StripRailsByType(railTypes ...reflect.Type) int {
	if len(railTypes) == 0 {
		return 0
	}
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	removed := 0

	// 从 pendingRails 移除（对齐 Python 差值计算）
	before := len(d.pendingRails)
	var newPending []agentinterfaces.AgentRail
	for _, r := range d.pendingRails {
		if !matchType(r, railTypes) {
			newPending = append(newPending, r)
		}
	}
	d.pendingRails = newPending
	removed = before - len(newPending)

	// 将已注册 Rail 标记为废弃
	for _, r := range d.registeredRails {
		if matchType(r, railTypes) {
			d.staleRails = append(d.staleRails, r)
			removed++
		}
	}
	return removed
}

// SwitchMode 切换 Agent 模式，更新会话级 PlanModeState。
// Python: DeepAgent.switch_mode(session, mode) (line 1859)
func (d *DeepAgent) SwitchMode(sess sessioninterfaces.SessionFacade, mode string) {
	state := d.LoadState(sess)
	if state.PlanMode.Mode == mode {
		state.PlanMode.PrePlanMode = state.PlanMode.Mode
		logger.Info(logComponent).Str("mode", mode).Msg("[DeepAgent] session mode unchanged, no switch needed")
		return
	}
	state.PlanMode.PrePlanMode = state.PlanMode.Mode
	state.PlanMode.Mode = mode
	d.saveState(sess, state)
}

// RestoreModeAfterPlanExit 恢复进入规划模式前的模式。
// Python: DeepAgent.restore_mode_after_plan_exit(session) (line 1875)
func (d *DeepAgent) RestoreModeAfterPlanExit(sess sessioninterfaces.SessionFacade) {
	state := d.LoadState(sess)
	state.PlanMode.Mode = state.PlanMode.PrePlanMode
	if state.PlanMode.Mode == "" {
		state.PlanMode.Mode = "normal"
	}
	state.PlanMode.PrePlanMode = ""
	d.saveState(sess, state)
}

// GetPlanFilePath 从会话状态中的 slug 推导计划文件路径。
// Python: DeepAgent.get_plan_file_path(session) (line 1889)
func (d *DeepAgent) GetPlanFilePath(sess sessioninterfaces.SessionFacade) string {
	state := d.LoadState(sess)
	slug := state.PlanMode.PlanSlug
	if slug == "" {
		return ""
	}
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()
	if cfg == nil || cfg.Workspace == nil {
		return ""
	}
	// resolvePlanFilePath 已在 tools/agent_mode/slug.go 中实现
	return filepath.Join(cfg.Workspace.RootPath, slug+".md")
}

// SaveState 持久化 DeepAgent 状态到会话。
// Python: DeepAgent.save_state(session, state) (line 1815)
func (d *DeepAgent) SaveState(sess sessioninterfaces.SessionFacade, state *hschema.DeepAgentState) {
	d.saveState(sess, state)
}

// ClearState 清除 DeepAgent 运行时缓存。
// Python: DeepAgent.clear_state(session, clear_persisted) (line 1843)
func (d *DeepAgent) ClearState(sess sessioninterfaces.SessionFacade, clearPersisted bool) {
	d.clearState(sess, clearPersisted)
}

// FollowUp 发布 FollowUp 事件到任务循环。
// Python: DeepAgent.follow_up(msg, task_id, session) (line 2368)
//
// 投递失败返回具名 error：Python 靠异常向上抛，Go 没有异常，只能走返回值，
// 否则调用方无法区分"已送达"与"被丢弃"。
func (d *DeepAgent) FollowUp(ctx context.Context, msg string, taskID string, sess *session.Session) error {
	d.configMu.RLock()
	ctrl := d.loopController
	loopSess := d.loopSession
	d.configMu.RUnlock()

	if ctrl == nil {
		logger.Warn(logComponent).
			Str("event_type", "FOLLOWUP_DROPPED").
			Str("reason", "no_active_loop").
			Str("msg_preview", steerLogPreview(msg)).
			Msg("follow up dropped: task loop controller is not active")
		return ErrNoActiveLoop
	}
	sessToUse := sess
	if sessToUse == nil {
		sessToUse = loopSess
	}
	if sessToUse == nil {
		logger.Warn(logComponent).
			Str("event_type", "FOLLOWUP_DROPPED").
			Str("reason", "no_session").
			Str("msg_preview", steerLogPreview(msg)).
			Msg("follow up dropped: neither arg session nor loop session is bound")
		return ErrNoSession
	}

	// Python: FollowUpEvent.from_text(msg)
	event := cschema.FromText(msg)
	if taskID != "" {
		event.SetMetadata(map[string]any{"task_id": taskID})
	}

	// Python: controller.event_queue.publish_event_async(card.id, sess, event)
	if err := ctrl.PublishEventAsync(ctx, sessToUse, event); err != nil {
		logger.Error(logComponent).Err(err).Str("msg", msg).Msg("FollowUp failed to publish event")
		return fmt.Errorf("follow up 发布失败: %w", err)
	}
	return nil
}

// Steer 发布 TaskInteraction 事件到任务循环。
// Python: DeepAgent.steer(msg, session) (line 2400)
//
// 投递失败返回具名 error（ErrNoActiveLoop / ErrNoSession / 发布错误）：
// Python 靠异常传播，Go 必须走返回值，否则上层无法在输入没进去时回退。
func (d *DeepAgent) Steer(ctx context.Context, msg string, sess *session.Session) error {
	d.configMu.RLock()
	ctrl := d.loopController
	loopSess := d.loopSession
	d.configMu.RUnlock()

	// 静默丢弃点补日志：外循环已结束（controller 已清理）或会话未绑定时，
	// steer 无法投递。Python 侧同样直接 return，但 Go 的异步发布让这类
	// 丢弃只能靠日志定位（历史问题：集成测试只看到"等待 steer 入队超时"）。
	if ctrl == nil {
		logger.Warn(logComponent).
			Str("event_type", "STEER_DROPPED").
			Str("reason", "no_active_loop").
			Str("msg_preview", steerLogPreview(msg)).
			Msg("steer dropped: task loop controller is not active")
		return ErrNoActiveLoop
	}
	s := sess
	if s == nil {
		s = loopSess
	}
	if s == nil {
		logger.Warn(logComponent).
			Str("event_type", "STEER_DROPPED").
			Str("reason", "no_session").
			Str("msg_preview", steerLogPreview(msg)).
			Msg("steer dropped: neither arg session nor loop session is bound")
		return ErrNoSession
	}

	// Python: TaskInteractionEvent(interaction=[TextDataFrame(text=msg)])
	// Python: controller.event_queue.publish_event_async(self.card.id, sess, event)
	// 注意：Python 的 TaskInteractionEvent 是 Pydantic 模型，event_type 字段
	// 有默认值 EventType.TASK_INTERACTION；Go 无字段默认值机制，字面量构造
	// 必须显式设置 BaseEvent，否则 EventTypeField 为零值空串——
	// PublishEventAsync 用它构建 topic "{agentID}_{sessionID}_{eventType}"，
	// 空事件类型构造出的 topic 无订阅者，Produce 返回 ErrTopicNotFound，
	// steer 指令被静默丢弃（对齐 task_scheduler.go publishTaskEvent 先例）。
	event := &cschema.TaskInteractionEvent{
		BaseEvent:   *cschema.NewBaseEvent(cschema.EventTaskInteraction),
		Interaction: []cschema.DataFrame{&cschema.TextDataFrame{Text: msg}},
	}
	if err := ctrl.PublishEventAsync(ctx, s, event); err != nil {
		logger.Warn(logComponent).Err(err).Str("msg", msg).Msg("Steer failed to publish TaskInteractionEvent")
		return fmt.Errorf("steer 发布失败: %w", err)
	}
	logger.Debug(logComponent).Str("msg", msg).Msg("Steer: TaskInteractionEvent published")
	return nil
}

// steerLogPreview 截断 steer 文本用于日志（rune 安全，最多 120 字符）。对齐 CJK 截断惯例。
func steerLogPreview(msg string) string {
	const maxRunes = 120
	runes := []rune(msg)
	if len(runes) <= maxRunes {
		return msg
	}
	return string(runes[:maxRunes])
}

// Abort 请求立即中止任务循环。
// Python: DeepAgent.abort(session) (line 2448)
func (d *DeepAgent) Abort(ctx context.Context) {
	d.configMu.RLock()
	coord := d.loopCoordinator
	ctrl := d.loopController
	d.configMu.RUnlock()

	if coord != nil && ctrl != nil {
		coord.RequestAbort()
		handler := ctrl.EventHandler()
		if handler != nil {
			// Python: handler.on_abort()
			if loopHandler, ok := handler.(*task_loop.TaskLoopEventHandler); ok {
				loopHandler.OnAbort()
			}
		}
	}

	d.cancelStreamProcess()
}

// EnqueueHarnessConfig 排队一个 harness_config.yaml 在下次 Stream() 时加载。
// Python: DeepAgent.enqueue_harness_config(config_path) (line 1601)
func (d *DeepAgent) EnqueueHarnessConfig(configPath string) {
	d.railsMu.Lock()
	defer d.railsMu.Unlock()
	d.pendingHarnessConfigs = append(d.pendingHarnessConfigs, configPath)
}

// LoadHarnessConfig 热加载 harness_config.yaml 中声明的资源。
// ⤵️ 9.3 回填：内部调 create_deep_agent，完整实现待工厂补全
// Python: DeepAgent.load_harness_config(config_path) (line 1218)
func (d *DeepAgent) LoadHarnessConfig(ctx context.Context, configPath string) ([]string, error) {
	// ⤵️ 9.3 回填：HarnessConfigLoader.load + 资源注册逻辑
	return nil, fmt.Errorf("load_harness_config 尚未实现，⤵️ 9.3 回填")
}

// UnloadHarnessConfig 卸载 harness_config.yaml 中声明的资源。
// ⤵️ 9.3 回填：内部调 create_deep_agent，完整实现待工厂补全
// Python: DeepAgent.unload_harness_config(config_path) (line 1359)
func (d *DeepAgent) UnloadHarnessConfig(ctx context.Context, configPath string) ([]string, error) {
	// ⤵️ 9.3 回填：资源卸载逻辑
	return nil, fmt.Errorf("unload_harness_config 尚未实现，⤵️ 9.3 回填")
}

// EnsureInitialized 执行懒初始化（仅用于测试）。
// Python: DeepAgent.ensure_initialized() (line 865)
func (d *DeepAgent) EnsureInitialized(ctx context.Context) (context.Context, error) {
	return d.ensureInitialized(ctx)
}

// InitWorkspace 初始化工作空间目录结构。
// Python: DeepAgent.init_workspace() (line 869)
func (d *DeepAgent) InitWorkspace(ctx context.Context) error {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil || cfg.Workspace == nil {
		return nil
	}

	// ⤵️ 9.3 回填：DirectoryBuilder 构建逻辑
	logger.Info(logComponent).Str("root_path", cfg.Workspace.RootPath).Msg("InitWorkspace: directory setup pending")
	return nil
}

// GetContextUsage 获取当前上下文占用统计。
// Python: DeepAgent.get_context_usage(session_id, context_id) (line 566)
func (d *DeepAgent) GetContextUsage(ctx context.Context, sessionID string, contextID string) (map[string]any, error) {
	modelCtx, err := d.getContextOrError(sessionID, contextID)
	if err != nil {
		return nil, err
	}
	resolvedSessionID, err := d.resolveContextSessionID(sessionID)
	if err != nil {
		return nil, err
	}
	stats := modelCtx.Statistic()
	contextWindowTokens := d.resolveContextWindowTokens()
	var usageRatio float64
	if contextWindowTokens > 0 {
		usageRatio = float64(stats.TotalTokens) / float64(contextWindowTokens)
	}
	return map[string]any{
		"session_id":            resolvedSessionID,
		"context_id":            modelCtx.ContextID(),
		"total_tokens":          stats.TotalTokens,
		"context_window_tokens": contextWindowTokens,
		"usage_ratio":           usageRatio,
		"usage_percent":         math.Round(usageRatio*10000) / 100,
		"stats": map[string]any{
			"total_messages":     stats.TotalMessages,
			"total_tokens":       stats.TotalTokens,
			"total_dialogues":    stats.TotalDialogues,
			"system_messages":    stats.SystemMessages,
			"user_messages":      stats.UserMessages,
			"assistant_messages": stats.AssistantMessages,
			"tool_messages":      stats.ToolMessages,
		},
	}, nil
}

// GetContextOccupancy GetContextUsage 的别名。
// Python: DeepAgent.get_context_occupancy (line 596)
func (d *DeepAgent) GetContextOccupancy(ctx context.Context, sessionID string, contextID string) (map[string]any, error) {
	return d.GetContextUsage(ctx, sessionID, contextID)
}

// GetCurrentContext 返回当前上下文消息。
// Python: DeepAgent.get_current_context(session_id, context_id) (line 604)
func (d *DeepAgent) GetCurrentContext(ctx context.Context, sessionID string, contextID string) ([]llmschema.BaseMessage, error) {
	modelCtx, err := d.getContextOrError(sessionID, contextID)
	if err != nil {
		return nil, err
	}
	// Python: context.get_messages() → size=0, withHistory=true
	messages, err := modelCtx.GetMessages(0, true)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// CreateNewContextEngine 创建新的上下文引擎。
// Python: DeepAgent.create_new_context_engine(session_id, messages) (line 643)
func (d *DeepAgent) CreateNewContextEngine(ctx context.Context, sessionID string, messages []llmschema.BaseMessage) (string, error) {
	d.configMu.RLock()
	reactAgent := d.reactAgent
	card := d.card
	d.configMu.RUnlock()

	if reactAgent == nil {
		return "", exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 configure()"))
	}

	// Python: new_session_id = session_id or str(uuid.uuid4())
	newSessionID := sessionID
	if newSessionID == "" {
		newSessionID = uuid.New().String()
	}

	normalizedMessages := normalizeContextMessages(messages)

	// Python: await self._react_agent.context_engine.create_context(
	//     Python: session=Session(session_id=new_session_id, card=self.card),
	//     Python: history_messages=normalized_messages,
	// )
	sess := session.NewSession(session.WithSessionID(newSessionID), session.WithCard(card))
	_, err := reactAgent.ContextEngine().CreateContext(ctx, "default_context_id", sess,
		ceinterface.WithHistoryMessages(normalizedMessages))
	if err != nil {
		return "", err
	}
	return newSessionID, nil
}

// NewContextEngine CreateNewContextEngine 的别名。
// Python: DeepAgent.new_context_engine (line 665)
func (d *DeepAgent) NewContextEngine(ctx context.Context, sessionID string, messages []llmschema.BaseMessage) (string, error) {
	return d.CreateNewContextEngine(ctx, sessionID, messages)
}

// LoopSession 返回活跃的循环会话。
// Python: DeepAgent.loop_session 属性 (line 888)
func (d *DeepAgent) LoopSession() *session.Session {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.loopSession
}

// InteractionSession 返回 start_interaction 绑定的长生命周期会话。
// Python: DeepAgent._interaction_session
// （interface_deep.py resolve_live_agent_session :1729 读它）
//
// 返回 nil 表示交互尚未启动。调用方需只读使用：该 Session 归 DeepAgent 所有。
// 存在意义：会话状态（plan_mode 等）缓存在绑定的 Session 上，改运行态的
// 调用方必须拿到这一个，用临时 Session 写入只会更新 checkpointer。
func (d *DeepAgent) InteractionSession() *session.Session {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.interactionSession
}

// ReactConfig 返回内层 ReActAgent 配置（仅用于测试）。
// Python: DeepAgent.react_config 属性 (line 894)
func (d *DeepAgent) ReactConfig() agentinterfaces.AgentConfig {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.reactAgent == nil {
		return nil
	}
	return d.reactAgent.Config()
}

// AgentID 返回 Agent 唯一标识。
// 实现 agentinterfaces.BaseAgent 接口。
func (d *DeepAgent) AgentID() string {
	if d.card == nil {
		return ""
	}
	return d.card.ID
}

// SpecName 返回规格名称，用于子 Agent 匹配。
// 实现 SubagentSpec 接口。
// Python: isinstance(spec, DeepAgent) 时通过 spec.card.name 匹配。
func (d *DeepAgent) SpecName() string {
	if d.card == nil {
		return ""
	}
	return d.card.Name
}

// resolveContextSessionID 解析上下文 API 使用的会话 ID。
// Python: DeepAgent._resolve_context_session_id(session_id) (line 483)
func (d *DeepAgent) resolveContextSessionID(sessionID string) (string, error) {
	if sessionID != "" {
		return sessionID, nil
	}
	d.configMu.RLock()
	boundID := d.boundSessionID
	loopSess := d.loopSession
	d.configMu.RUnlock()
	if boundID != "" {
		return boundID, nil
	}
	if loopSess != nil {
		return loopSess.GetSessionID(), nil
	}
	return "", exception.BuildError(exception.StatusDeepagentContextParamError,
		exception.WithMsg("未绑定上下文时 session_id 为必填项"))
}

// getContextOrError 获取内部 ReAct 上下文，不存在则返回错误。
// Python: DeepAgent._get_context_or_error(session_id, context_id) (line 501)
func (d *DeepAgent) getContextOrError(sessionID string, contextID string) (ceinterface.ModelContext, error) {
	d.configMu.RLock()
	reactAgent := d.reactAgent
	d.configMu.RUnlock()

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 configure()"))
	}

	resolvedSessionID, err := d.resolveContextSessionID(sessionID)
	if err != nil {
		return nil, err
	}

	ce := reactAgent.ContextEngine()
	if ce == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("ContextEngine 不可用，请先调用 ensureInitialized()"))
	}

	mc := ce.GetContext(contextID, resolvedSessionID)
	if mc == nil {
		return nil, exception.BuildError(exception.StatusDeepagentContextParamError,
			exception.WithMsg(fmt.Sprintf("cannot find context '%s' in session '%s'", contextID, resolvedSessionID)))
	}
	return mc, nil
}

// resolveContextWindowTokens 解析配置的模型上下文窗口大小。
// Python: DeepAgent._resolve_context_window_tokens() (line 547)
func (d *DeepAgent) resolveContextWindowTokens() int {
	d.configMu.RLock()
	reactAgent := d.reactAgent
	deepCfg := d.deepConfig
	d.configMu.RUnlock()

	var modelName string
	var ceConfig *ceschema.ContextEngineConfig

	if reactAgent != nil {
		if reactCfg, ok := reactAgent.Config().(*saconfig.ReActAgentConfig); ok && reactCfg != nil {
			ceConfig = &reactCfg.ContextEngineConfig
			modelName = reactCfg.ModelNameVal
		}
	}
	if deepCfg != nil && deepCfg.ContextEngineConfig != nil {
		if deepCfg.ContextEngineConfig.ModelName != "" {
			modelName = deepCfg.ContextEngineConfig.ModelName
		}
		ceConfig = deepCfg.ContextEngineConfig
	}

	var fallbackTokens int
	var modelContextWindowTokens map[string]int
	if ceConfig != nil {
		fallbackTokens = ceConfig.ContextWindowTokens
		modelContextWindowTokens = ceConfig.ModelContextWindowTokens
	}

	return cecontext.ResolveContextMax(modelName, fallbackTokens, modelContextWindowTokens)
}

// ──────────────────────────── 交互监督器导出方法（9.87）────────────────────────────

// Phase 返回当前交互相位。
// Python: DeepAgent.phase (L3892)
func (d *DeepAgent) Phase() hschema.InteractionPhase {
	d.ensureInteractionState()
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	return d.interactionPhase
}

// InteractionStarted 返回交互循环是否已启动。
// Python: DeepAgent.interaction_started (L3896)
func (d *DeepAgent) InteractionStarted() bool {
	d.ensureInteractionState()
	d.interactionSupervisorMu.Lock()
	defer d.interactionSupervisorMu.Unlock()
	return d.interactionStarted
}

// ActiveRound 返回当前执行中的轮次（无则 nil）。
// Python: DeepAgent.active_round (L3901)
func (d *DeepAgent) ActiveRound() *hschema.ActiveInteractionRound {
	d.ensureInteractionState()
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	return d.activeInteractionRound
}

// EventManager 返回交互调度器。
// Python: DeepAgent.event_manager (L3905)
func (d *DeepAgent) EventManager() *task_loop.EventManager {
	d.ensureInteractionState()
	return d.interactionEvents
}

// HasOutputStream 返回是否已有输出消费者。
// Python: DeepAgent.has_output_stream (L3909)
func (d *DeepAgent) HasOutputStream() bool {
	d.ensureInteractionState()
	return d.interactionOutput.HasConsumer()
}

// AttachOutput 抢占本交互唯一的输出读者。
// 已有消费者时返回 nil。若存在 ACTIVE goal，即使本次没抢到租约也要补排
// 缺失的 goal work，让已连接的读者继续收到目标进展（切会话/并发 attach_goal）。
// Python: DeepAgent.attach_output (L4048)
func (d *DeepAgent) AttachOutput(_ context.Context) (*hschema.InteractionOutputStream, error) {
	d.ensureInteractionState()

	d.interactionSendMu.Lock()
	defer d.interactionSendMu.Unlock()

	if d.ensureInteractionRunning() != nil {
		return nil, nil
	}

	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	if !d.isInteractionRunningLocked() {
		return nil, nil
	}

	stream := d.attachOutputLocked()
	if d.goalCoordinator != nil && d.goalCoordinator.HasActiveRecord() {
		if _, err := d.goalCoordinator.EnsureActiveGoalWorkLocked(); err != nil {
			return nil, err
		}
		d.notifyWork()
	}
	return stream, nil
}

// NextOutput 实现 hschema.InteractionOutputDriver。
// Python: next_output (L4217)
func (d *DeepAgent) NextOutput(ctx context.Context, lease *hschema.OutputLease) (stream.Schema, bool) {
	d.ensureInteractionState()
	return d.interactionOutput.NextItem(ctx, lease)
}

// DetachOutput 释放指定票号的租约并清场。
// Python: detach_output (L4220)
func (d *DeepAgent) DetachOutput(ctx context.Context, token string, abortActiveRound bool) {
	d.ensureInteractionState()

	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()

	if !d.interactionOutput.Detach(token, true) {
		return
	}
	d.interactionEvents.DiscardAllWork()
	if abortActiveRound {
		d.cancelActiveRoundLocked(ctx, "output_detached", nil)
	}
}

// Start 绑定任务循环内核并启动被动输出转发器。
// 持 start 锁期间被 await 的生命周期回调不得重入 Start/Stop。
// Python: DeepAgent.start (L3912)
func (d *DeepAgent) Start(ctx context.Context, sess *session.Session) error {
	d.ensureInteractionState()

	d.interactionStartMu.Lock()
	defer d.interactionStartMu.Unlock()

	if d.InteractionStarted() {
		if err := d.ensureInteractionRunning(); err != nil {
			return err
		}
	} else if !d.tryTransitionInteractionPhase(hschema.InteractionPhaseIdle) {
		return ErrInteractionNotRunning
	}

	if sess == nil {
		sess = session.CreateAgentSession("default", d.Card(), nil)
		if err := sess.PreRun(ctx, map[string]any{}); err != nil {
			return err
		}
	}
	sid := sess.GetSessionID()

	if d.InteractionStarted() {
		if d.interactionSessionID == sid {
			return nil
		}
		return fmt.Errorf("interaction loop already bound to session %s; cannot bind %s",
			d.interactionSessionID, sid)
	}

	d.interactionSession = sess
	d.interactionSessionID = sid

	// 对齐 Python start() L3953-3962：先建可变 worktree holder，再启动任务循环内核，
	// 使 scheduler/supervisor/round/工具任务共享同一对象。Go 的 context.Value 不跨
	// 调用传播，故在 attachRunContexts 里逐入口重新挂载（等价 contextvar 全局可见）。
	// 幂等语义对齐 init_session_state()：Python 的 _get_state() 是「已有则复用、没有才建」
	// （session.py L40-45），不是每次 start 都换新 holder。
	if d.worktreeSessionState == nil {
		d.worktreeSessionState = worktree.InitWorktreeSessionState()
	}

	if _, _, err := d.prepareInteractionTaskLoop(ctx, sess); err != nil {
		return err
	}
	// 9.89 已回填：goal 语言 / TaskCompletionRail 注册 / GoalManager 构造 / 绑定（Python L3964-3994）
	goalLanguage := d.goalPromptLanguage()
	d.railsMu.Lock()
	tcRail := d.taskCompletionRail
	d.railsMu.Unlock()
	if tcRail == nil {
		tcRail = rails.NewTaskCompletionRail(rails.WithGoalLanguage(goalLanguage))
		if err := d.RegisterRail(ctx, tcRail); err != nil {
			return fmt.Errorf("register goal TaskCompletionRail: %w", err)
		}
	}

	gm := goal.NewGoalManager(goal.GoalManagerConfig{
		Store:        goal.NewSessionGoalStore(sess),
		EventManager: d.interactionEvents,
		ControlLock:  &d.interactionControlMu,
		HasOutputStream: func() bool {
			return d.HasOutputStream()
		},
		// GoalManager 在持 controlMu 时调此回调，故直接走 Locked 变体，不再取锁。
		CancelActiveRound: func(ctx context.Context, kind hschema.RoundWorkKind, goalID, reason string) {
			d.cancelActiveRoundLocked(ctx, reason, &RoundExpectation{GoalID: goalID})
		},
		EmitEvent:  d.emitInteractionEvent,
		NotifyWork: d.notifyWork,
		Language:   goalLanguage,
	})
	d.SetGoalCoordinator(gm)
	d.bindLiveGoalManager(tcRail)
	// 绑定后必须再 Init 一次，goal 工具才会真正注册：prepareInteractionTaskLoop /
	// RegisterRail 阶段的 Init 早于 GoalManager 构造，goalManager 为 nil 时直接返回。
	// Python: _bind_live_goal_manager(rail) + init_rail(rail, self) (L3987-3992)
	if err := tcRail.Init(ctx, d); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[DeepAgent] Failed to register goal tools")
	}

	d.interactionSupervisorMu.Lock()
	d.interactionStarted = true
	d.interactionStopping = false
	d.interactionSupervisorMu.Unlock()

	d.startForwarder(ctx)
	d.waitSupervisorReady(d.ensureSupervisorRunning(ctx), 2*time.Second)
	return nil
}

// Stop 与 Start 串行，终止本交互运行时。
// Python: DeepAgent.stop (L3998)
func (d *DeepAgent) Stop(ctx context.Context) error {
	d.ensureInteractionState()
	d.interactionStartMu.Lock()
	defer d.interactionStartMu.Unlock()
	return d.stopInteractionLocked(ctx)
}

// SendInput 分发用户文本或打断恢复输入。
// 需要读输出的宿主必须先 AttachOutput；STEER / follow-up 通常跳过 attach，
// 让既有消费者继续接收输出。分发模式对 InteractiveInput 恢复请求不生效。
// Python: DeepAgent.send_input (L4072)
func (d *DeepAgent) SendInput(ctx context.Context, req hschema.SendInputRequest) error {
	d.ensureInteractionState()

	d.interactionSendMu.Lock()
	defer d.interactionSendMu.Unlock()

	if err := d.ensureInteractionRunning(); err != nil {
		return err
	}
	return d.sendUser(ctx, req)
}

// RunOneRound 执行一个交互 work item，并可选地提出下一轮工作。
// Python: run_one_round (L3744)
func (d *DeepAgent) RunOneRound(ctx context.Context, work *hschema.RoundWorkItem, taskID string, sess *session.Session) hschema.RoundOutcome {
	initCtx, err := d.ensureInitialized(ctx)
	if err != nil {
		return hschema.RoundOutcome{ErrorCode: "round_execution_error", ErrorMessage: err.Error()}
	}
	ctx = initCtx
	// ⤵️ MoA 偏差（9.89 spec D1 整段跳过）：proposal_owner begin_work/activate

	d.invokeActive.Store(true)
	defer d.invokeActive.Store(false)

	coord, ctrl, err := d.prepareInteractionTaskLoop(ctx, sess)
	if err != nil {
		return hschema.RoundOutcome{ErrorCode: "round_execution_error", ErrorMessage: err.Error()}
	}
	if work.ResetLoop() {
		coord.Reset()
		state := d.LoadState(sess)
		state.StopConditionState = nil
		d.saveState(sess, state)
	}

	inputs := utils.DeepCopyMap(work.Inputs)
	if inputs == nil {
		inputs = make(map[string]any)
	}
	if work.Kind == hschema.RoundWorkKindGoal {
		// Python: inputs["run"] = {"kind": "goal", "context": {...}} (L3773-3777)
		inputs["run"] = map[string]any{
			"kind": string(agentinterfaces.RunKindGoal),
			"context": map[string]any{
				"goal_id":    work.Context.GoalID,
				"revision":   work.Context.Revision,
				"session_id": work.Context.SessionID,
				"reset_loop": true,
			},
		}
	}
	invokeInputs, normErr := d.normalizeInputs(inputs)
	if normErr != nil {
		return hschema.RoundOutcome{ErrorCode: "round_execution_error", ErrorMessage: normErr.Error()}
	}

	cbc := agentinterfaces.NewAgentCallbackContext(d, invokeInputs, sess)
	var result map[string]any
	var nextWork *hschema.RoundWorkItem

	lifecycleErr := cbc.FireLifecycle(ctx,
		agentinterfaces.CallbackBeforeInvoke, agentinterfaces.CallbackAfterInvoke,
		func() error {
			// Python: 生命周期内第一件事就是把专家角色物化成本轮附件（L3787）
			d.syncExpertRoleAttachment(invokeInputs, sess)
			if isResumeInput(invokeInputs) {
				res, runErr := d.runSingleRoundInvoke(ctx, cbc, sess)
				if runErr != nil {
					return runErr
				}
				result = res
			} else {
				timeout := hschema.DefaultCompletionTimeout
				d.configMu.RLock()
				if d.deepConfig != nil && d.deepConfig.CompletionTimeout > 0 {
					timeout = d.deepConfig.CompletionTimeout
				}
				d.configMu.RUnlock()

				if submitErr := ctrl.SubmitRound(ctx, sess, fmt.Sprintf("%v", work.Query()),
					work.IsFollowUp, true,
					invokeInputs.RunKind, invokeInputs.RunContext,
					task_loop.WithTaskID(taskID)); submitErr != nil {
					return submitErr
				}
				result = ctrl.WaitRoundCompletion(ctx, &timeout)
			}
			d.writeRoundResultToStream(ctx, result, sess)
			invokeInputs.Result = result
			if !isResumeInput(invokeInputs) {
				nextWork = d.buildInteractionNextWork(ctx, work, result, sess, coord, ctrl)
			}
			return nil
		})
	if lifecycleErr != nil {
		logger.Error(logComponent).Err(lifecycleErr).Msg("interaction round execution failed")
		return hschema.RoundOutcome{
			ErrorCode:    "round_execution_error",
			ErrorMessage: "interaction round execution failed",
		}
	}

	// Python: self.save_state(session); self.clear_state(session)
	// Go 侧运行态直接落在 session state（LoadState/saveState），无需额外的
	// session 属性缓存清理，保留此注释标记对应关系。
	d.saveState(sess, d.LoadState(sess))
	return hschema.RoundOutcome{NextWork: nextWork}
}

// CancelRound 取消当前活跃轮但不停止交互循环。
// Python: cancel_round (L4268)
func (d *DeepAgent) CancelRound(ctx context.Context, reason string) (bool, error) {
	d.ensureInteractionState()
	if d.ActiveRound() == nil {
		return false, nil
	}
	d.interactionControlMu.Lock()
	d.cancelActiveRoundLocked(ctx, reason, nil)
	d.interactionControlMu.Unlock()
	return true, nil
}

// SetActiveAgentTemplate 记录热加载的 AgentTemplate。
// ⤵️ #9.3 回填：等待 AgentTemplate 热加载能力落地后由 load_agent_template 调用。
// Python: self._active_agent_template = (record.load_id, spec.agent_card.name)（L2301/L2328）
func (d *DeepAgent) SetActiveAgentTemplate(loadID string, roleName string) {
	d.ensureInteractionState()
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	d.activeAgentTemplate = &AgentTemplateRef{LoadID: loadID, RoleName: roleName}
}

// ClearActiveAgentTemplate 卸载 AgentTemplate 时清空引用。
// ⤵️ #9.3 回填：等待 AgentTemplate 热加载能力落地后由 unload 路径调用。
// Python: self._active_agent_template = None（L2386）
func (d *DeepAgent) ClearActiveAgentTemplate() {
	d.ensureInteractionState()
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	d.activeAgentTemplate = nil
}

// SetGoalCoordinator 注入 Goal 能力面（9.89 已在 Start 内构造 GoalManager 后调用）。
// Python: self.goal_manager = GoalManager(...) (L3970-3981)
func (d *DeepAgent) SetGoalCoordinator(coordinator goal.Coordinator) {
	d.ensureInteractionState()
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	d.goalCoordinator = coordinator
}

// GoalManager 返回会话级 Goal 能力面（未注入时 nil）。
// 供 TaskCompletionRail 的 Init 从 agent 继承兜底（Python `getattr(agent, "goal_manager")`），
// 以及后续服务面（10.3.27 command.goal）使用。按规则 10 属 DeepAgent 的注入缝，
// 不进 DeepAgentInterface（Python 侧同为属性而非继承接口面方法）。
// Python: self.goal_manager (L342)
func (d *DeepAgent) GoalManager() *goal.GoalManager {
	d.ensureInteractionState()
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	if gm, ok := d.goalCoordinator.(*goal.GoalManager); ok {
		return gm
	}
	return nil
}

// bindLiveGoalManager 把 GoalManager 绑到 TaskCompletionRail。
// Python: _bind_live_goal_manager (deep_agent.py L2157)
func (d *DeepAgent) bindLiveGoalManager(rail *rails.TaskCompletionRail) {
	if rail == nil {
		return
	}
	d.interactionControlMu.Lock()
	m := d.goalCoordinator
	d.interactionControlMu.Unlock()
	if m == nil {
		return
	}
	if gm, ok := m.(*goal.GoalManager); ok {
		rail.SetGoalManager(gm)
	}
}

// goalPromptLanguage 解析 GoalManager 与自动 TaskCompletionRail 的语言。
// Python: DeepAgent._goal_prompt_language (deep_agent.py L518)
func (d *DeepAgent) goalPromptLanguage() string {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.deepConfig == nil {
		return hprompts.ResolveLanguage("")
	}
	return d.deepConfig.EffectiveLanguage()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeContextMessages 规范化上下文消息输入。
// Python: DeepAgent._normalize_context_messages(messages) (line 614)
func normalizeContextMessages(messages []llmschema.BaseMessage) []llmschema.BaseMessage {
	if messages == nil {
		return nil
	}
	return messages
}

// filterDisabledTools 过滤掉被禁用的工具。
// Python: DeepAgent._filter_disabled_tools(config) (line 162)
func (d *DeepAgent) filterDisabledTools(config *hschema.DeepAgentConfig) {
	if config.Tools == nil {
		return
	}
	// Python: disabled_tool_names 集合
	disabledNames := make(map[string]bool)
	if !IsFreeSearchEnabled() {
		disabledNames["free_search"] = true
	}
	if !IsPaidSearchEnabled() {
		disabledNames["paid_search"] = true
	}
	if len(disabledNames) == 0 {
		return
	}
	// Python: config.tools = [card for card in config.tools if ...]
	var filtered []*tool.ToolCard
	for _, card := range config.Tools {
		if !disabledNames[card.Name] {
			filtered = append(filtered, card)
		}
	}
	config.Tools = filtered
}

// initialConfigure 首次配置：持久化配置、创建内层 ReActAgent、排队 Rails。
// Python: DeepAgent._initial_configure(config) (line 230)
func (d *DeepAgent) initialConfigure(ctx context.Context, config *hschema.DeepAgentConfig) {
	d.deepConfig = config
	if config.Card != nil {
		d.card = config.Card
	}
	// 曝光是注册期策略：必须在工厂注册 config.Tools 与 rail 自注册工具之前写入
	d.applyToolExposurePolicy(config)

	d.reactAgent = d.createReactAgent(ctx)
	d.queuePendingRails(config)
}

// hotReconfigure 热重配置：不重启 Agent，更新模型/工具/提示词等。
// Python: DeepAgent._hot_reconfigure(config) (line 239)
func (d *DeepAgent) hotReconfigure(ctx context.Context, config *hschema.DeepAgentConfig) {
	previousConfig := d.deepConfig
	d.deepConfig = config
	if config.Card != nil {
		d.card = config.Card
	}
	// 热重配只影响此后注册的卡片（已注册卡片的曝光已锁定）
	d.applyToolExposurePolicy(config)

	d.hotReloadRails(config)

	if config.Model != nil {
		d.hotReloadModel(ctx, config)
	}

	if config.Tools != nil && d.reactAgent != nil {
		previousTools := (*[]*tool.ToolCard)(nil)
		if previousConfig != nil {
			previousTools = &previousConfig.Tools
		}
		d.hotReloadTools(config, previousTools)
	}

	if config.SystemPrompt != "" && d.reactAgent != nil {
		d.hotReloadSystemPrompt(ctx, config)
	}

	d.queuePendingRails(config)

	// 注：Python 在 hotReconfigure 后调用 _sync_builder_to_active_rails() 将新 builder
	// 同步到已注册 rail。Go 的策略不同：agent 通过 SystemPromptBuilder() 接口返回最新
	// builder，rail 调用接口获取，无需显式同步。若后续 rail 体系需要持有 builder 引用，
	// 再补充 syncBuilderToActiveRails 逻辑。
}

// hotReloadRails 热重配置时循环废弃旧 Rail。
// Python: DeepAgent._hot_reload_rails(config) (line 263)
func (d *DeepAgent) hotReloadRails(config *hschema.DeepAgentConfig) {
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	if config.Rails != nil {
		// 部分更新：只替换类型相同的 Rail
		replacingTypes := make(map[reflect.Type]bool)
		for _, r := range config.Rails {
			replacingTypes[reflect.TypeOf(r)] = true
		}
		var retained []agentinterfaces.AgentRail
		for _, r := range d.registeredRails {
			if replacingTypes[reflect.TypeOf(r)] {
				d.staleRails = append(d.staleRails, r)
			} else {
				retained = append(retained, r)
			}
		}
		d.registeredRails = retained

		var newPending []agentinterfaces.AgentRail
		for _, r := range d.pendingRails {
			if !replacingTypes[reflect.TypeOf(r)] {
				newPending = append(newPending, r)
			}
		}
		d.pendingRails = newPending
	} else {
		// 全量替换：所有已注册 Rail 变为废弃
		d.staleRails = append(d.staleRails, d.registeredRails...)
		d.registeredRails = nil
		d.pendingRails = nil
	}
}

// hotReloadModel 热更新 ReActAgent 模型配置。
// Python: DeepAgent._hot_reload_model(config) (line 291)
func (d *DeepAgent) hotReloadModel(ctx context.Context, config *hschema.DeepAgentConfig) {
	if d.reactAgent == nil {
		return
	}

	// 获取当前 ReActAgent 配置的副本
	reactCfg, ok := d.reactAgent.Config().(*saconfig.ReActAgentConfig)
	if !ok || reactCfg == nil {
		return
	}
	newReactConfig := *reactCfg // 值拷贝

	if config.Model != nil {
		// 更新模型客户端配置
		model := config.Model
		if model.ClientConfig != nil {
			newReactConfig.ModelClientConfig = model.ClientConfig
		}
		if model.ModelConfig != nil {
			newReactConfig.ModelRequestConfig = model.ModelConfig
			if model.ModelConfig.ModelName != "" {
				newReactConfig.ModelNameVal = model.ModelConfig.ModelName
			}
		}
	}

	// 内层 ReAct 迭代上限：未配置=不限，显式配置优先（与 task loop 无关）
	// Python: _bind_inner_react_max_iterations() (deep_agent.py L250-263)
	newReactConfig.MaxIterations = config.ResolveInnerReactMaxIterations()
	logInnerReactMaxIterations(config.MaxIterations, newReactConfig.MaxIterations)

	if config.ContextEngineConfig != nil {
		newReactConfig.ContextEngineConfig = *config.ContextEngineConfig
	}

	if err := d.reactAgent.Configure(ctx, &newReactConfig); err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to hot-reconfigure ReActAgent model")
		return
	}

	logger.Info(logComponent).Msg("[DeepAgent] model config hot-reload completed")
}

// hotReloadTools 同步 AbilityManager 工具卡片。
// Python: DeepAgent._hot_reload_tools(config, previous_tools) (line 324)
func (d *DeepAgent) hotReloadTools(config *hschema.DeepAgentConfig, previousTools *[]*tool.ToolCard) {
	newByName := make(map[string]*tool.ToolCard)
	for _, card := range config.Tools {
		newByName[card.Name] = card
	}

	previousByName := make(map[string]*tool.ToolCard)
	if previousTools != nil {
		for _, card := range *previousTools {
			previousByName[card.Name] = card
		}
	}

	// 确定受管理的工具名称集合
	managedNames := make(map[string]bool)
	for name := range previousByName {
		managedNames[name] = true
	}
	if len(managedNames) == 0 {
		for name := range newByName {
			managedNames[name] = true
		}
	}

	// 移除不再存在的工具
	var stale []string
	for name := range managedNames {
		if _, exists := newByName[name]; !exists {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		for _, name := range stale {
			if card, ok := previousByName[name]; ok {
				d.unregisterToolResource(card)
			}
		}
		d.abilityManager.RemoveMany(stale)
	}

	// 添加或替换 ID 变更的工具
	for name, card := range newByName {
		existing := d.abilityManager.Get(name)
		existingTool, _ := existing.(*tool.ToolCard)

		if existingTool != nil && existingTool.ID == card.ID {
			d.ensureBuiltinToolResource(card, config)
			continue
		}
		if existingTool != nil {
			d.unregisterToolResource(existingTool)
			d.abilityManager.Remove(name)
		}
		d.abilityManager.Add(card)
		d.ensureBuiltinToolResource(card, config)
	}

	// 重排工具顺序
	var orderedNames []string
	for name := range newByName {
		orderedNames = append(orderedNames, name)
	}
	d.abilityManager.ReorderTools(orderedNames)
}

// hotReloadSystemPrompt 重建 SystemPromptBuilder。
// Python: DeepAgent._hot_reload_system_prompt(config) (line 370)
func (d *DeepAgent) hotReloadSystemPrompt(ctx context.Context, config *hschema.DeepAgentConfig) {
	language := hprompts.ResolveLanguage(config.Language)
	mode := hprompts.ResolveMode(config.PromptMode.String())
	promptBuilder := hprompts.NewSystemPromptBuilder(language, mode)

	if config.SystemPrompt != "" {
		promptBuilder.AddSection(saprompts.NewPromptSection(
			sections.SectionIdentity,
			map[string]string{"cn": config.SystemPrompt, "en": config.SystemPrompt},
			10,
		))
	} else {
		promptBuilder.AddSection(sections.BuildIdentitySection())
	}
	prompt := promptBuilder.Build()

	// 更新 ReActAgent 配置
	if d.reactAgent != nil {
		currentConfig := d.reactAgent.Config()
		if reactCfg, ok := currentConfig.(*saconfig.ReActAgentConfig); ok {
			newReactConfig := *reactCfg
			newReactConfig.PromptTemplate = []map[string]any{
				{"role": "system", "content": prompt},
			}
			if err := d.reactAgent.Configure(ctx, &newReactConfig); err != nil {
				logger.Error(logComponent).Err(err).Msg("Failed to hot-reconfigure system prompt")
			}
		}
	}

	d.systemPromptBuilder = promptBuilder
	if d.reactAgent != nil {
		d.reactAgent.SetPromptBuilder(promptBuilder.SystemPromptBuilder)
	}

	// Python: 语言热同步（deep_agent.py L831-834）
	d.railsMu.Lock()
	if d.taskCompletionRail != nil {
		d.taskCompletionRail.SetGoalLanguage(language)
	}
	gc := d.goalCoordinator
	d.railsMu.Unlock()
	if gm, ok := gc.(*goal.GoalManager); ok {
		gm.SetLanguage(language)
	}

	logger.Info(logComponent).Msg("[DeepAgent] system prompt hot-reload completed")
}

// queuePendingRails 将配置驱动的 Rail 追加到待注册列表。
// Python: DeepAgent._queue_pending_rails(config) (line 408)
func (d *DeepAgent) queuePendingRails(config *hschema.DeepAgentConfig) {
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	if config.Rails != nil {
		d.pendingRails = append(d.pendingRails, config.Rails...)
	}

	d.taskCompletionRail = nil

	if config.ProgressiveToolEnabled {
		// ⤴️ 9.11 回填：ProgressiveToolRail 创建
		d.pendingRails = append(d.pendingRails, rails.NewProgressiveToolRail(config))
		logger.Debug(logComponent).Msg("ProgressiveToolRail created, backfill from 9.11")
	}

	if config.EnableTaskLoop {
		// ⤴️ 9.12 回填：TaskCompletionRail 创建（⤴️ 9.89 带 goal_language，对齐 Python L907-915）
		d.pendingRails = append(d.pendingRails, rails.NewTaskCompletionRail(rails.WithGoalLanguage(config.EffectiveLanguage())))
		logger.Debug(logComponent).Msg("TaskCompletionRail created, backfill from 9.12")
	}

	if config.Permissions != nil {
		// Python: build_permission_interrupt_rail(permissions=..., llm=..., model_name=..., host=..., workspace_root=...)
		permConfig := permissionsSectionToMap(config.Permissions)
		var host *hsecurity.ToolPermissionHost
		if config.PermissionHost != nil {
			if h, ok := config.PermissionHost.(*hsecurity.ToolPermissionHost); ok {
				host = h
			}
		}
		wsRoot := ""
		if config.Workspace != nil {
			wsRoot = config.Workspace.RootPath
		}
		permRail := hsecurity.BuildPermissionInterruptRail(
			permConfig, nil, host, wsRoot, nil, "",
			func(config map[string]any, engine *hsecurity.PermissionEngine, toolNames []string, llmModel *llm.Model, modelName string, h *hsecurity.ToolPermissionHost) agentinterfaces.AgentRail {
				return securityrail.NewPermissionInterruptRail(config, engine, toolNames, llmModel, modelName, h)
			},
		)
		if permRail != nil {
			d.pendingRails = append(d.pendingRails, permRail)
			logger.Debug(logComponent).Msg("PermissionInterruptRail created, backfill from 9.19 SecurityRail")
		}
	}
}

// createReactAgent 从当前 DeepAgentConfig 构建内层 ReActAgent。
// Python: DeepAgent._create_react_agent() (line 703)
func (d *DeepAgent) createReactAgent(ctx context.Context) *agents.ReActAgent {
	cfg := d.deepConfig
	if cfg == nil {
		panic("DeepAgentConfig 为空，请先调用 Configure()")
	}

	innerCard := agentschema.NewAgentCard(
		agentschema.WithAgentName(d.card.Name+"_react"),
		agentschema.WithAgentDescription(d.card.Description),
	)

	reactConfig := saconfig.NewReActAgentConfig()

	// 内层 ReAct 迭代上限：未配置=不限，显式配置优先（与 task loop 无关）
	// Python: _bind_inner_react_max_iterations() (deep_agent.py L250-263)
	reactConfig.MaxIterations = cfg.ResolveInnerReactMaxIterations()
	logInnerReactMaxIterations(cfg.MaxIterations, reactConfig.MaxIterations)

	if cfg.ContextEngineConfig != nil {
		reactConfig.ContextEngineConfig = *cfg.ContextEngineConfig
	}

	// 设置工作空间
	if cfg.Workspace != nil {
		reactConfig.Workspace = cfg.Workspace
	}

	// 构建系统提示词
	language := hprompts.ResolveLanguage(cfg.Language)
	mode := hprompts.ResolveMode(cfg.PromptMode.String())
	promptBuilder := hprompts.NewSystemPromptBuilder(language, mode)

	if cfg.SystemPrompt != "" {
		promptBuilder.AddSection(saprompts.NewPromptSection(
			sections.SectionIdentity,
			map[string]string{"cn": cfg.SystemPrompt, "en": cfg.SystemPrompt},
			10,
		))
	} else {
		promptBuilder.AddSection(sections.BuildIdentitySection())
	}
	prompt := promptBuilder.Build()
	reactConfig.PromptTemplate = []map[string]any{
		{"role": "system", "content": prompt},
	}

	// 设置模型
	if cfg.Model != nil {
		model := cfg.Model
		if model.ClientConfig != nil {
			reactConfig.ModelClientConfig = model.ClientConfig
		}
		if model.ModelConfig != nil {
			reactConfig.ModelRequestConfig = model.ModelConfig
			if model.ModelConfig.ModelName != "" {
				reactConfig.ModelNameVal = model.ModelConfig.ModelName
			}
		}
	}

	agent := agents.NewReActAgent(innerCard, reactConfig)

	// Configure 内部会覆盖 promptBuilder
	if err := agent.Configure(ctx, reactConfig); err != nil {
		logger.Error(logComponent).Err(err).Msg("Inner ReActAgent Configure failed")
	}

	// 覆盖回共享 builder（对齐 Python L755-762）
	agent.SetPromptBuilder(promptBuilder.SystemPromptBuilder)

	// 共享 AbilityManager（对齐 Python L765）
	agent.SetAbilityManager(d.abilityManager)

	// 注入预构建 LLM（对齐 Python L768-769）
	if cfg.Model != nil {
		agent.SetLLM(cfg.Model)
	}

	// 创建并注入 ContextEngine（对齐 Python ReActAgent.__init__ 中 self.context_engine = ContextEngine(...)）
	// Python 中 ReActAgent 总是创建 ContextEngine，即使 context_engine_config 使用默认值
	ceConfig := ceschema.NewContextEngineConfig()
	if cfg.ContextEngineConfig != nil {
		ceConfig = *cfg.ContextEngineConfig
	}
	var ceOpts []ceinterface.ContextEngineOption
	if cfg.Workspace != nil {
		ceOpts = append(ceOpts, ceinterface.WithWorkspace(cfg.Workspace))
	}
	if cfg.SysOperation != nil {
		ceOpts = append(ceOpts, ceinterface.WithEngineSysOperation(cfg.SysOperation))
	}
	ce := context_engine.NewContextEngine(ceConfig, ceOpts...)
	agent.SetContextEngine(ce)

	// 保存 builder 引用
	d.systemPromptBuilder = promptBuilder

	// 注入会话级动态提示词管理器：ReAct 在写用户消息前与建窗口前同步
	agent.SetPromptAttachmentManager(d.attachments)

	return agent
}

// attachRunContexts 给「已初始化」的后续调用重新注入 agent 级 Context 值。
// Python 用 contextvars：init_cwd/InitSpanState 设置一次即对同一 context 内
// 所有后续 await 可见。Go 的 context.Value 不会跨调用传播——调用方每次
// Invoke/Stream 传入的 ctx 都是新的，若不重新挂载，第二次调用起：
//   - cwd.GetWorkspace(ctx) 为空 → 沙箱根为空 → 所有文件操作被拒（code=199003）
//   - span 断链
//
// 已由调用方注入（如子 agent 自己的 CwdState）时不覆盖。
func (d *DeepAgent) attachRunContexts(ctx context.Context) context.Context {
	if cwd.CwdStateFromCtx(ctx) == nil {
		d.configMu.RLock()
		state := d.cwdState
		d.configMu.RUnlock()
		if state != nil {
			ctx = cwd.WithCwdState(ctx, state)
		}
	}
	if observability.SpanStateFromCtx(ctx) == nil {
		d.configMu.RLock()
		state := d.spanState
		d.configMu.RUnlock()
		if state != nil {
			ctx = observability.WithSpanState(ctx, state)
		}
	}
	// worktree 会话态：Python 用 ContextVar（init_session_state() 设置一次即全局可见），
	// Go 必须逐入口挂载。交互循环在 Start 里新建 holder，其余路径（Invoke/Stream）
	// 若已由 Start 建立则同样继承。
	if worktree.WorktreeSessionStateFromCtx(ctx) == nil {
		d.interactionControlMu.Lock()
		state := d.worktreeSessionState
		d.interactionControlMu.Unlock()
		if state != nil {
			ctx = worktree.WithWorktreeSessionState(ctx, state)
		}
	}
	return ctx
}

// ensureInitialized 执行懒初始化。
// Python: DeepAgent._ensure_initialized() (line 813)
func (d *DeepAgent) ensureInitialized(ctx context.Context) (context.Context, error) {
	d.initMu.Lock()
	if d.initialized {
		d.initMu.Unlock()
		return d.attachRunContexts(ctx), nil
	}
	// 整个初始化过程在锁内执行，防止并发重复初始化
	defer d.initMu.Unlock()

	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	// 初始化工作空间 CWD
	// Python: DeepAgent._ensure_initialized() 中 init_cwd 调用 (line 813-828)
	if cfg != nil && cfg.Workspace != nil && cfg.Workspace.RootPath != "" {
		initRoot := cfg.Workspace.RootPath
		cwdState := cwd.InitCwd(initRoot, cwd.WithWorkspace(initRoot))
		d.configMu.Lock()
		d.cwdState = cwdState
		d.configMu.Unlock()
		ctx = cwd.WithCwdState(ctx, cwdState)
		logger.Info(logComponent).Str("init_root", initRoot).Msg("CWD initialized")
	}

	// 初始化可观测性 SpanState（对齐 SessionState 注入模式）
	// Python: contextvars 自动传播 span_context
	// Go: 通过 context.Value 传播 *OtelSpanState 指针
	// spanState 必须无条件回写 d.spanState：调用方自带 SpanState（子 agent / 嵌套
	// 调用）时若跳过赋值，后续任何不带 span 的 ctx 进 attachRunContexts 都注入不到，
	// tracing 断链（注释声称「每次入口重新挂载」需靠该字段兜底）。
	if state := observability.SpanStateFromCtx(ctx); state != nil {
		d.configMu.Lock()
		d.spanState = state
		d.configMu.Unlock()
	} else {
		spanState := observability.InitSpanState()
		d.configMu.Lock()
		d.spanState = spanState
		d.configMu.Unlock()
		ctx = observability.WithSpanState(ctx, spanState)
	}

	// 注册待处理的 MCP 服务器
	d.registerPendingMCPs(ctx)

	// 工作空间初始化
	if d.needsWorkspaceInit() {
		if err := d.InitWorkspace(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Workspace initialization failed")
		}
	}

	d.railsMu.Lock()
	// 注销废弃 Rail
	var staleToUnregister []agentinterfaces.AgentRail
	for _, staleRail := range d.staleRails {
		// 跳过同时也在 pending 中的同名实例
		found := false
		for _, pendingRail := range d.pendingRails {
			if pendingRail == staleRail {
				found = true
				break
			}
		}
		if !found {
			staleToUnregister = append(staleToUnregister, staleRail)
		}
	}
	d.staleRails = nil

	// 注册待处理 Rail
	// S-01：**不在遍历前清空队列**——对齐 Python「循环全部成功后才 clear」
	// （deep_agent.py 的 _pending_rails.clear() 位于 for 循环之后）。遍历前清空会导致
	// 任意一个 rail Init 失败后重试时队列为空、initialized 被置真、整批 rail 静默丢失。
	// 失败项与剩余项保留在队列中，已成功的项在下方按引用摘除，重试幂等。
	pendingToRegister := make([]agentinterfaces.AgentRail, len(d.pendingRails))
	copy(pendingToRegister, d.pendingRails)
	d.railsMu.Unlock()

	// 执行废弃 Rail 注销
	for _, staleRail := range staleToUnregister {
		if err := d.UnregisterRail(ctx, staleRail); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to unregister deprecated Rail")
		}
	}

	// 注册待处理 Rail
	for i, r := range pendingToRegister {
		if tc, ok := r.(*rails.TaskCompletionRail); ok {
			d.railsMu.Lock()
			d.taskCompletionRail = tc
			d.railsMu.Unlock()
			// Python: isinstance(rail_inst, TaskCompletionRail) → _bind_live_goal_manager (L1497-1499)
			d.bindLiveGoalManager(tc)
		}
		// Python: isinstance(rail_inst, DeepAgentRail) → set_sys_operation / set_workspace
		if provider, ok := r.(rails.DeepAgentRailProvider); ok {
			d.configMu.RLock()
			cfg := d.deepConfig
			d.configMu.RUnlock()
			if cfg != nil {
				provider.SetSysOperation(cfg.SysOperation)
				provider.SetWorkspace(cfg.Workspace)
			}
		}
		// Python: rail_inst.init(self) —— 无 try/except，异常直接冒泡中断初始化。
		// Go 侧此前 Warn + continue 会让 rail 静默缺失（例如 TeamPolicyRail 不生效），
		// 改为向上返回错误。
		if err := r.Init(ctx, d); err != nil {
			// S-01：仅摘除已成功的项（[0,i)），失败项与剩余项留在队列，重试时重新 Init。
			// Python 侧异常逃出 for 循环时 _pending_rails 完整保留，go 侧按引用摘除等价。
			d.removePendingRailsByRefs(pendingToRegister[:i])
			return ctx, fmt.Errorf("rail %s 初始化失败: %w", reflect.TypeOf(r).String(), err)
		}
		d.registerRailSelective(ctx, r)
		d.railsMu.Lock()
		d.registeredRails = append(d.registeredRails, r)
		d.railsMu.Unlock()
	}

	// S-01：全部成功后才摘除队列（对齐 Python clear 的位置）
	d.removePendingRailsByRefs(pendingToRegister)

	// ProgressiveToolRail 持有 BM25 目录：等所有 startup rail 注册完工具后再建首份索引。
	// Python: deep_agent.py:1518-1528（finalize_startup_async）
	d.finalizeProgressiveRails(ctx)

	d.initialized = true
	return ctx, nil
}

// applyToolExposurePolicy 在工具注册前写入注册期曝光策略。
// Python: DeepAgent._initial_configure 中的 ability_manager.set_tool_exposure_policy
func (d *DeepAgent) applyToolExposurePolicy(config *hschema.DeepAgentConfig) {
	if config == nil || d.abilityManager == nil {
		return
	}
	policy, ok := d.abilityManager.(agentinterfaces.ExposurePolicyController)
	if !ok {
		return
	}
	policy.SetToolExposurePolicy(config.ProgressiveToolEnabled, defaultDirectToolNames)
}

// finalizeProgressiveRails 让 ProgressiveToolRail 建立初始 BM25 索引。
// Python: DeepAgent.ensure_init 末尾遍历 _registered_rails 调用 finalize_startup_async
func (d *DeepAgent) finalizeProgressiveRails(ctx context.Context) {
	d.railsMu.Lock()
	snapshot := append([]agentinterfaces.AgentRail(nil), d.registeredRails...)
	d.railsMu.Unlock()

	seen := make(map[agentinterfaces.AgentRail]struct{}, len(snapshot))
	for _, railInst := range snapshot {
		if _, ok := seen[railInst]; ok {
			continue
		}
		seen[railInst] = struct{}{}
		progressive, ok := railInst.(*rails.ProgressiveToolRail)
		if !ok {
			continue
		}
		if err := progressive.FinalizeStartupAsync(ctx, d); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "progressive_rail_finalize").
				Err(err).
				Msg("failed to finalize progressive tool rail")
		}
	}
}

// needsWorkspaceInit 检查是否需要工作空间初始化。
// Python: DeepAgent._needs_workspace_init() (line 854)
func (d *DeepAgent) needsWorkspaceInit() bool {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()
	if cfg == nil {
		return false
	}
	return cfg.Workspace != nil && cfg.SysOperation != nil && cfg.AutoCreateWorkspace
}

// registerPendingMCPs 注册配置的 MCP 服务器。
// Python: DeepAgent._register_pending_mcps() (line 773)
func (d *DeepAgent) registerPendingMCPs(ctx context.Context) {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil || len(cfg.Mcps) == 0 {
		return
	}

	resourceMgr := runner.GetResourceMgr()

	for _, mcpConfig := range cfg.Mcps {
		existingConfig, getErr := resourceMgr.GetMcpServerConfig(mcpConfig.ServerID)

		if getErr != nil || existingConfig == nil {
			// Python: existing_config is None → add_mcp_server
			_, addErr := resourceMgr.AddMcpServer(ctx, mcpConfig, resources_manager.WithMcpTag(resources_manager.Tag(d.card.ID)))
			if addErr != nil {
				logger.Error(logComponent).Err(addErr).Str("server_id", mcpConfig.ServerID).Msg("MCP server registration failed")
				continue
			}
		} else {
			// Python: existing_config.model_dump() != mcp_config.model_dump() → error
			if !mcpConfigEqual(existingConfig, mcpConfig) {
				logger.Error(logComponent).
					Str("server_id", mcpConfig.ServerID).
					Msg("MCP server already exists with different config, skipping")
				continue
			}

			// Python: add_resource_tag(server_id, self.card.id)
			_, tagErr := resourceMgr.AddResourceTag(mcpConfig.ServerID, []resources_manager.Tag{resources_manager.Tag(d.card.ID)})
			if tagErr != nil {
				logger.Warn(logComponent).Err(tagErr).Str("server_id", mcpConfig.ServerID).Msg("MCP resource tag addition failed")
			}

			// Python: for tool_id in get_mcp_tool_ids → add_resource_tag
			for _, toolID := range resourceMgr.GetMcpToolIDs(mcpConfig.ServerID) {
				_, tagErr := resourceMgr.AddResourceTag(toolID, []resources_manager.Tag{resources_manager.Tag(d.card.ID)})
				if tagErr != nil {
					logger.Warn(logComponent).Err(tagErr).Str("tool_id", toolID).Msg("MCP tool tag addition failed")
				}
			}
		}

		// Python: self.ability_manager.add(mcp_config)
		d.abilityManager.Add(mcpConfig)
		logger.Debug(logComponent).Str("server_id", mcpConfig.ServerID).Msg("MCP config registered")
	}
}

// mcpConfigEqual 比较两个 McpServerConfig 是否等价。
// Python: existing_config.model_dump() != mcp_config.model_dump()
func mcpConfigEqual(a, b *mcptypes.McpServerConfig) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)
	if aErr != nil || bErr != nil {
		return false
	}
	return string(aJSON) == string(bJSON)
}

// normalizeInputs 解析用户输入为 InvokeInputs。
// Python: DeepAgent._normalize_inputs(inputs) (line 1056)
func (d *DeepAgent) normalizeInputs(inputs any) (*agentinterfaces.InvokeInputs, error) {
	switch v := inputs.(type) {
	case map[string]any:
		var query agentinterfaces.InvokeQuery
		// query 字段可能是 string 或 InteractiveInput
		if q, ok := v["query"]; ok {
			switch qv := q.(type) {
			case string:
				query = agentinterfaces.InvokeQueryString(qv)
			case *interaction.InteractiveInput:
				query = qv
			default:
				query = agentinterfaces.InvokeQueryString(fmt.Sprintf("%v", qv))
			}
		} else {
			query = agentinterfaces.InvokeQueryString("")
		}
		conversationID, _ := v["conversation_id"].(string)
		var runKind agentinterfaces.RunKind
		var runContext *agentinterfaces.RunContext
		if run, ok := v["run"].(map[string]any); ok {
			if kind, kOk := run["kind"].(string); kOk && kind != "" {
				runKind = agentinterfaces.RunKind(kind)
			} else {
				runKind = agentinterfaces.RunKindNormal
			}
			if contextData, cOk := run["context"].(map[string]any); cOk {
				// Python: RunContext(**context_data) (line 1069)
				runContext = &agentinterfaces.RunContext{}
				if reason, ok := contextData["reason"].(string); ok {
					runContext.Reason = agentinterfaces.HeartbeatReason(reason)
				}
				if sessionID, ok := contextData["session_id"].(string); ok {
					runContext.SessionID = sessionID
				}
				if mode, ok := contextData["context_mode"].(string); ok {
					runContext.ContextMode = mode
				}
				if extra, ok := contextData["extra"].(map[string]any); ok {
					runContext.Extra = extra
				}
				// ⤴️ 9.87 回填：goal 代际字段（Python: RunContext(**context_data) L1069）
				if goalID, ok := contextData["goal_id"].(string); ok {
					runContext.GoalID = goalID
				}
				// revision 在 Go 内部为 int，经 JSON 解码后为 float64
				switch rv := contextData["revision"].(type) {
				case int:
					runContext.Revision = rv
				case int64:
					runContext.Revision = int(rv)
				case float64:
					runContext.Revision = int(rv)
				}
			}
		}
		// 保留以 "_" 开头的额外 key 到 Extra（如 _steering_queue、_streaming）
		// 对齐 Python: inputs 为 dict 时，额外 key 自然保留到 effective_inputs
		extra := make(map[string]any)
		for k, v2 := range v {
			if strings.HasPrefix(k, "_") {
				extra[k] = v2
			}
		}
		// turn 号：只接受正整数，其余视为未提供。
		// Python: raw_turn_number = inputs.get("_turn_number") → isinstance(int) and not bool and > 0（deep_agent.py L1878-1883）
		var turnNumber int
		switch tn := v["_turn_number"].(type) {
		case int:
			if tn > 0 {
				turnNumber = tn
			}
		case int64:
			if tn > 0 {
				turnNumber = int(tn)
			}
		}
		// 用量归属的调用血缘：由调度方（harness/任务调度器）随输入带下。
		// Python: deep_agent.py L1871-1877（inputs.get 六键）+ L1877（depth = int(... or 0)）
		parentSessionID, _ := v["parent_session_id"].(string)
		invocationID, _ := v["invocation_id"].(string)
		parentInvocationID, _ := v["parent_invocation_id"].(string)
		delegationID, _ := v["delegation_id"].(string)
		var agentPath []string
		switch ap := v["agent_path"].(type) {
		case []string:
			agentPath = append([]string{}, ap...)
		case []any:
			for _, item := range ap {
				if s, ok := item.(string); ok {
					agentPath = append(agentPath, s)
				}
			}
		}
		var depth int
		switch dv := v["depth"].(type) {
		case int:
			depth = dv
		case int64:
			depth = int(dv)
		case float64:
			depth = int(dv)
		}
		return &agentinterfaces.InvokeInputs{
			Query:              query,
			ConversationID:     conversationID,
			RunKind:            runKind,
			RunContext:         runContext,
			ParentSessionID:    parentSessionID,
			InvocationID:       invocationID,
			ParentInvocationID: parentInvocationID,
			DelegationID:       delegationID,
			AgentPath:          agentPath,
			Depth:              depth,
			TurnNumber:         turnNumber,
			Extra:              extra,
		}, nil
	case string:
		return &agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString(v)}, nil
	case *interaction.InteractiveInput:
		// Python: isinstance(inputs, InteractiveInput) → query = inputs
		return &agentinterfaces.InvokeInputs{Query: v}, nil
	default:
		return nil, exception.BuildError(exception.StatusDeepagentContextParamError,
			exception.WithMsg(fmt.Sprintf("不支持的输入类型: %T，必须是 string、[]Message、map 或 InvokeInputs", v)))
	}
}

// toEffectiveInputs 将 InvokeInputs 转换为 ReAct 输入字典。
// Python: DeepAgent._to_effective_inputs(invoke_inputs) (line 1093)
func toEffectiveInputs(invokeInputs *agentinterfaces.InvokeInputs) map[string]any {
	result := map[string]any{"query": invokeInputs.Query}
	if invokeInputs.ConversationID != "" {
		result["conversation_id"] = invokeInputs.ConversationID
	}
	if invokeInputs.RunKind != "" {
		result["run_kind"] = invokeInputs.RunKind
	}
	if invokeInputs.RunContext != nil {
		result["run_context"] = invokeInputs.RunContext
	}
	// turn 号回写：ReActAgent 从 effective inputs 的 "_turn_number" 读。
	// Python: if invoke_inputs.turn_number is not None → effective_inputs["_turn_number"]（deep_agent.py L2002-2003）
	if invokeInputs.TurnNumber > 0 {
		result["_turn_number"] = invokeInputs.TurnNumber
	}
	// 调用血缘回写：ReActAgent 从 effective inputs 读同样六个键。
	// Python: _to_effective_inputs 逐键 if not None（deep_agent.py L1985-2001）
	if invokeInputs.ParentSessionID != "" {
		result["parent_session_id"] = invokeInputs.ParentSessionID
	}
	if invokeInputs.InvocationID != "" {
		result["invocation_id"] = invokeInputs.InvocationID
	}
	if invokeInputs.ParentInvocationID != "" {
		result["parent_invocation_id"] = invokeInputs.ParentInvocationID
	}
	if invokeInputs.DelegationID != "" {
		result["delegation_id"] = invokeInputs.DelegationID
	}
	if invokeInputs.AgentPath != nil {
		result["agent_path"] = append([]string{}, invokeInputs.AgentPath...)
	}
	if invokeInputs.Depth != 0 {
		result["depth"] = invokeInputs.Depth
	}
	// 合并 Extra 字段（如 _steering_queue），对齐 Python dict 自然保留额外 key 的行为
	for k, v := range invokeInputs.Extra {
		result[k] = v
	}
	return result
}

// isResumeInput 判断输入是否为中断恢复。
// Python: DeepAgent._is_resume_input(invoke_inputs) (line 1106)
func isResumeInput(invokeInputs *agentinterfaces.InvokeInputs) bool {
	// Python: isinstance(invoke_inputs.query, InteractiveInput)
	return invokeInputs.Query != nil && invokeInputs.Query.IsInteractiveInput()
}

// resultFromStreamChunk 从流块构建 invoke 风格的结果。
// Python: DeepAgent._result_from_stream_chunk(chunk, output_parts) (line 1111)
func resultFromStreamChunk(chunk stream.Schema, outputParts *[]string) map[string]any {
	// 只处理 OutputSchema 类型
	outChunk, ok := chunk.(stream.OutputSchema)
	if !ok {
		return nil
	}

	chunkType := outChunk.Type
	payload, ok := outChunk.Payload.(map[string]any)
	if !ok {
		return nil
	}

	switch chunkType {
	case "llm_output":
		// Python: llm_output → 追加 content 到 outputParts
		if content, ok := payload["content"].(string); ok {
			*outputParts = append(*outputParts, content)
		}
		return nil

	case "answer":
		// Python: answer → 从 payload 构建结果 dict
		result := make(map[string]any, len(payload))
		for k, v := range payload {
			result[k] = v
		}
		if _, exists := result["result_type"]; !exists {
			result["result_type"] = "answer"
		}
		if _, exists := result["output"]; !exists {
			if content, ok := result["content"].(string); ok {
				*outputParts = append(*outputParts, content)
				result["output"] = content
			} else {
				return nil
			}
		}
		return result

	default:
		return nil
	}
}

// registerRailSelective 选择性路由 Rail 回调到正确的 Agent。
// Python: DeepAgent._register_rail_selective(rail) (line 1626)
func (d *DeepAgent) registerRailSelective(ctx context.Context, r agentinterfaces.AgentRail) {
	callbacks := r.GetCallbacks()

	// 必须透传 rail 优先级：回调框架按优先级降序执行（同优先级按注册时间），
	// 缺省 0 会让排序退化为注册顺序——AgentModeRail(85) 的设计前提是
	// 「TaskPlanningRail(90) 先注入、本 Rail 后移除其节」，优先级丢失后
	// 后注册的 TaskPlanningRail（如模式切回 plan 时的重注册）会跑到
	// AgentModeRail 之后，todo 节加了没人删（2026-11-03）。
	// Python: register_rail → register_callback(callback, priority=rail.callback_priority(event))
	// 每事件单独取：上游 AgentRail.callback_priority(event) 允许按事件分档
	// （如 ProcessCliIterationLimitRail 在 AFTER_REACT_ITERATION 上是 -1000、其余 1000）。

	for event, callback := range callbacks {
		priorityOpt := cb.WithPriority(r.CallbackPriorityFor(event))
		if bridgeEvents[event] {
			// 桥接事件 → 注册到内层 ReActAgent
			d.configMu.RLock()
			reactAgent := d.reactAgent
			d.configMu.RUnlock()
			if reactAgent != nil {
				reactAgent.CallbackManager().RegisterCallback(ctx, event, callback, priorityOpt)
			}
			continue
		}

		if outerOnlyEvents[event] || deepEvents[event] {
			// 外层/Deep 事件 → 注册到外层 DeepAgent
			d.callbackManager.RegisterCallback(ctx, event, callback, priorityOpt)
			continue
		}

		// 未知事件注册到外层
		logger.Warn(logComponent).
			Str("event", string(event)).
			Msg("unknown rail event, registering to outer DeepAgent")
		d.callbackManager.RegisterCallback(ctx, event, callback, priorityOpt)
	}
}

// runSingleRoundInvoke 调用内层 ReActAgent 一次。
// Python: DeepAgent._run_single_round_invoke(ctx, session) (line 1647)
func (d *DeepAgent) runSingleRoundInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	modified, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok {
		return nil, exception.BuildError(exception.StatusDeepagentContextParamError,
			exception.WithMsg("ctx.inputs 必须为 InvokeInputs 类型"))
	}

	d.configMu.RLock()
	reactAgent := d.reactAgent
	invokeOverride := d.innerInvokeOverride
	d.configMu.RUnlock()

	effectiveInputs := toEffectiveInputs(modified)

	// 测试覆写优先
	if invokeOverride != nil {
		return invokeOverride(ctx, effectiveInputs, agentinterfaces.WithSession(sess))
	}

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 Configure()"))
	}

	return reactAgent.Invoke(ctx, effectiveInputs, agentinterfaces.WithSession(sess))
}

// runTaskLoopInvoke 运行外层任务循环，返回最后一轮结果。
// Python: DeepAgent._run_task_loop_invoke(ctx, session) (line 2112)
// runTaskLoopInvoke 执行外层任务循环，返回最后一轮结果。
// Python: DeepAgent._run_task_loop_invoke(ctx, session) (line 2112-2144)
func (d *DeepAgent) runTaskLoopInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	sessConcrete, ok := sess.(*session.Session)
	if !ok || sessConcrete == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要 *session.Session 类型会话"))
	}

	loopCh, err := d.runTaskLoop(ctx, cbc, sessConcrete, false)
	if err != nil {
		return nil, err
	}

	// Python: last_result: Dict[str, Any] = {}
	// Python: 异步迭代任务循环获取最终结果
	var lastResult map[string]any
	for result := range loopCh {
		lastResult = result
	}

	// Python: return last_result
	return lastResult, nil
}

// runTaskLoop 任务循环生成器，每轮完成后将 result 发送到 channel。
// Python: DeepAgent._run_task_loop(ctx, session) (line 1991-2110)
// invoke 和 stream 共用此方法：
//
//	invoke: 从 channel 读取最后一轮结果
//	stream: 后台goroutine从channel读取，每轮写入session流
func (d *DeepAgent) runTaskLoop(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, sess *session.Session, isStreaming bool) (<-chan map[string]any, error) {
	modified, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok {
		return nil, exception.BuildError(exception.StatusDeepagentContextParamError,
			exception.WithMsg("ctx.inputs 必须为 InvokeInputs 类型"))
	}

	coord, ctrl, err := d.setupTaskLoop(ctx, sess)
	if err != nil {
		return nil, err
	}

	// 绑定会话（对齐 Python line 2013-2017）
	d.configMu.RLock()
	boundID := d.boundSessionID
	d.configMu.RUnlock()

	sessionID := sess.GetSessionID()
	if boundID != sessionID {
		if err := ctrl.BindSession(ctx, sess); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to bind session")
		}
		d.configMu.Lock()
		d.boundSessionID = sessionID
		d.configMu.Unlock()
	}

	outCh := make(chan map[string]any, 1)

	go func() {
		defer close(outCh)

		// Python: try/finally 确保清理（line 2095-2110）
		defer func() {
			state := d.LoadState(sess)
			state.StopConditionState = nil
			d.saveState(sess, state)

			if !d.hasPendingSessionSpawn() {
				_ = ctrl.UnbindSession(ctx, sess)
				_ = ctrl.Stop(ctx)
				d.configMu.Lock()
				d.loopCoordinator = nil
				d.loopController = nil
				d.loopSession = nil
				d.boundSessionID = ""
				d.configMu.Unlock()
				logLoop("all tasks completed, controller cleaned up", "", 0)
			} else {
				logLoop("pending SESSION_SPAWN tasks, controller kept alive", "", 0)
			}
		}()

		currentQuery := modified.Query
		outerRound := 0

		d.configMu.RLock()
		timeout := hschema.DefaultCompletionTimeout
		if d.deepConfig != nil && d.deepConfig.CompletionTimeout > 0 {
			timeout = d.deepConfig.CompletionTimeout
		}
		d.configMu.RUnlock()

		for coord.ShouldContinue() {
			outerRound++
			if result, exceeded := outerRoundsExceededResult(outerRound); exceeded {
				logLoop("round=%d exceeded max_outer_rounds=%d, forcing stop", "", outerRound, maxOuterRounds)
				select {
				case outCh <- result:
				case <-ctx.Done():
					return
				}
				break
			}

			// 排空 follow-up，合并到 state 缓冲（对齐 Python line 2026-2040）
			newFollowUps := ctrl.DrainFollowUp()
			st := d.LoadState(sess)
			if len(newFollowUps) > 0 {
				st.PendingFollowUps = append(st.PendingFollowUps, newFollowUps...)
			}
			// 弹出第一个缓冲的 follow-up 作为当前查询
			isFollowUp := len(st.PendingFollowUps) > 0
			if isFollowUp {
				currentQuery = agentinterfaces.InvokeQueryString(st.PendingFollowUps[0])
				st.PendingFollowUps = st.PendingFollowUps[1:]
				d.saveState(sess, st)
			}

			queryPreview := currentQuery.PlainText()
			if len(queryPreview) > 120 {
				queryPreview = queryPreview[:120]
			}
			logLoop("round=%d started", fmt.Sprintf(", query=%s", queryPreview), outerRound)

			if err = ctrl.SubmitRound(ctx, sess, string(currentQuery.PlainText()), isFollowUp, isStreaming, modified.RunKind, modified.RunContext); err != nil {
				logger.Error(logComponent).Err(err).Int("round", outerRound).Msg("Failed to commit round")
				break
			}

			result := ctrl.WaitRoundCompletion(ctx, &timeout)
			// 判断结果类型（对齐 Python: result 中有 "error" key 表示失败）
			resultType := "N/A"
			if _, hasErr := result["error"]; hasErr {
				resultType = "error"
			} else if _, hasStatus := result["status"]; hasStatus {
				resultType = "completed"
			}

			outputPreview := ""
			if output, ok := result["output"].(string); ok {
				outputPreview = output
				if len(outputPreview) > 200 {
					outputPreview = outputPreview[:200]
				}
			}
			logLoop("round=%d completed, result_type=%s", fmt.Sprintf(", output=%s", outputPreview), outerRound, resultType)

			// yield result（对齐 Python line 2063: yield result）
			select {
			case outCh <- result:
			case <-ctx.Done():
				return
			}

			coord.IncrementIteration()
			coord.SetLastResult(result)

			// 更新状态
			st = d.LoadState(sess)
			exported := coord.ExportState()
			st.StopConditionState = map[string]any{
				"iteration":        exported.Iteration,
				"token_usage":      exported.TokenUsage,
				"stop_reason":      exported.StopReason,
				"evaluator_states": exported.EvaluatorStates,
			}
			d.saveState(sess, st)

			if resultType == "interrupt" {
				logLoop("round=%d interrupted", "", outerRound)
				break
			}
			if coord.IsAborted() {
				logLoop("round=%d aborted", "", outerRound)
				break
			}

			// Python: line 2079-2086: 退出条件检查
			st = d.LoadState(sess)
			if ctrl.HasFollowUp() || len(st.PendingFollowUps) > 0 {
				continue
			}
			if !d.hasRemainingTasks(sess) {
				logLoop("no remaining tasks, loop finished", "", 0)
				break
			}

			currentQuery = modified.Query
		}

		// Python: line 2090-2094: 循环结束后记录 stop_reason
		stopReason := coord.StopReason()
		if stopReason != "" {
			logLoop("loop stopped by: %s", "", stopReason)
		}
		// 无进展守卫触发：产出显式错误结果，避免上层把"连续空轮"当成功收尾
		// Python: deep_agent.py L2757-2768
		if stopReason == "NoProgressAnswerEvaluator" {
			select {
			case outCh <- map[string]any{
				"output":      "Task loop stopped after repeated empty or near-empty no-tool answers. This indicates the model is not making observable progress.",
				"result_type": "error",
				"stop_reason": stopReason,
			}:
			case <-ctx.Done():
			}
		}
	}()

	return outCh, nil
}

// writeRoundResultToStream 将任务循环轮次结果写入会话流。
// Python: DeepAgent._write_round_result_to_stream(result, session) (line 2214-2232)
func (d *DeepAgent) writeRoundResultToStream(ctx context.Context, result map[string]any, sess *session.Session) {
	d.configMu.RLock()
	reactAgent := d.reactAgent
	d.configMu.RUnlock()

	if reactAgent != nil {
		reactAgent.WriteInvokeResultToStream(ctx, result, sess)
	}
}

// WriteRoundResultToStream 将任务循环轮次结果写入会话流（跨包窄入口）。
// Python: DeepAgent._write_round_result_to_stream 是私有方法，NativeHarness 靠继承直取；
// Go 无继承（25.1a 偏差 D-3 组合），NativeHarness（agent_teams/harness）跨包需要窄入口，
// 与 D-7 PrepareInteractionTaskLoop 同类。NativeHarness.runRound 在 wait_round_completion
// 拿到 result 后调此方法，把 answer 写成 answer chunk 写流（否则 task_completion 被
// 消费端丢弃，chat.final 缺失）。
func (d *DeepAgent) WriteRoundResultToStream(ctx context.Context, result map[string]any, sess *session.Session) {
	d.writeRoundResultToStream(ctx, result, sess)
}

// runTaskLoopStream 流式执行外层任务循环。
// Python: DeepAgent._run_task_loop_stream(ctx, session, stream_modes) (line 2146-2212)
//
// 使用与 ReActAgent.Stream() 相同的"后台+前台"模式：
//
//	后台goroutine: 运行 _run_task_loop，每轮结果写入session流，最终 close_stream
//	前台: 从 session.StreamIterator() 读取chunk转发到 outCh
func (d *DeepAgent) runTaskLoopStream(ctx context.Context, invokeInputs *agentinterfaces.InvokeInputs, sess sessioninterfaces.SessionFacade, streamModes []stream.StreamMode) (<-chan stream.Schema, error) {
	// Python: _ = stream_modes（显式丢弃，task-loop stream 从 session.StreamIterator() 读取）
	_ = streamModes

	if sess == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要会话"))
	}

	sessConcrete, ok := sess.(*session.Session)
	if !ok || sessConcrete == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要 *session.Session 类型会话"))
	}

	// 构建 AgentCallbackContext
	cbc := agentinterfaces.NewAgentCallbackContext(d, invokeInputs, sess)

	// 派生子 context，用于取消后台 goroutine
	// Python: asyncio.create_task(_stream_process()) + CancelledError 处理
	streamCtx, streamCancel := context.WithCancel(ctx)
	d.streamMu.Lock()
	d.streamCancel = streamCancel
	d.streamMu.Unlock()

	outCh := make(chan stream.Schema, 64)

	// 后台 goroutine: 运行任务循环，每轮结果写入session流
	// Python: _stream_process() (line 2182-2194)
	go func() {
		// Python: line 2190-2194: finally → session.close_stream()
		defer func() {
			d.streamMu.Lock()
			d.streamCancel = nil
			d.streamMu.Unlock()
			_ = sessConcrete.CloseStream()
		}()

		loopCh, loopErr := d.runTaskLoop(streamCtx, cbc, sessConcrete, true)
		if loopErr != nil {
			// Python: line 2187-2189: except → 写入错误结果到流
			d.writeRoundResultToStream(streamCtx, map[string]any{
				"output":      loopErr.Error(),
				"result_type": "error",
			}, sessConcrete)
			return
		}

		// Python: line 2184-2185:
		// Python: 异步迭代任务循环写入流结果
		for result := range loopCh {
			d.writeRoundResultToStream(streamCtx, result, sessConcrete)
		}
	}()

	// 前台: 从 session.StreamIterator() 读取chunk转发到 outCh
	// Python: line 2199-2200: async for chunk in session.stream_iterator(): yield chunk
	go func() {
		defer close(outCh)
		defer d.cancelStreamProcess() // 前台退出时取消后台
		for chunk := range sessConcrete.StreamIterator() {
			select {
			case outCh <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return outCh, nil
}

// runSingleRoundStream 流式调用内层 ReActAgent 一次。
// Python: DeepAgent._run_single_round_stream(ctx, session, stream_modes) (line 2234)
func (d *DeepAgent) runSingleRoundStream(ctx context.Context, invokeInputs *agentinterfaces.InvokeInputs, sess sessioninterfaces.SessionFacade, streamModes []stream.StreamMode) (<-chan stream.Schema, error) {
	d.configMu.RLock()
	reactAgent := d.reactAgent
	streamOverride := d.innerStreamOverride
	d.configMu.RUnlock()

	effectiveInputs := toEffectiveInputs(invokeInputs)

	// 测试覆写优先
	if streamOverride != nil {
		return streamOverride(ctx, effectiveInputs,
			agentinterfaces.WithSession(sess),
			agentinterfaces.WithStreamModes(streamModes))
	}

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 Configure()"))
	}

	return reactAgent.Stream(ctx, effectiveInputs,
		agentinterfaces.WithSession(sess),
		agentinterfaces.WithStreamModes(streamModes))
}

// setupTaskLoop 创建或复用 Controller 基础设施。
// Python: DeepAgent._setup_task_loop(session) (line 1669)
func (d *DeepAgent) setupTaskLoop(ctx context.Context, sess *session.Session) (*task_loop.LoopCoordinator, *task_loop.TaskLoopController, error) {
	sessionID := sess.GetSessionID()

	d.configMu.RLock()
	existingCtrl := d.loopController
	boundID := d.boundSessionID
	coord := d.loopCoordinator
	d.configMu.RUnlock()

	// 复用已有控制器（session_id 匹配）
	if existingCtrl != nil && boundID == sessionID {
		if coord == nil {
			// 构建评估器（任务完成 rail 的评估器 + 无进展守卫）
			coord = task_loop.NewLoopCoordinator(d.buildTaskLoopEvaluators())
			d.configMu.Lock()
			d.loopCoordinator = coord
			d.configMu.Unlock()
		}
		coord.Reset()
		return coord, existingCtrl, nil
	}

	// 新控制器（首次或会话切换）
	if existingCtrl != nil {
		d.forceCleanupController(ctx)
	}

	// 构建评估器（任务完成 rail 的评估器 + 无进展守卫）
	coord = task_loop.NewLoopCoordinator(d.buildTaskLoopEvaluators())
	coord.Reset()

	queues := task_loop.NewLoopQueues(64)

	ctrlConfig := &config.ControllerConfig{
		SuppressCompletionSignal: true,
	}

	// 创建 ContextEngine
	// Python: context_engine = ContextEngine() (line 1717)
	var ceOpts []ceinterface.ContextEngineOption
	d.configMu.RLock()
	if d.deepConfig != nil && d.deepConfig.SysOperation != nil {
		ceOpts = append(ceOpts, ceinterface.WithEngineSysOperation(d.deepConfig.SysOperation))
	}
	d.configMu.RUnlock()
	ce := context_engine.NewContextEngine(ceschema.NewContextEngineConfig(), ceOpts...)

	ctrl := task_loop.NewTaskLoopController()
	ctrl.Init(d.card, ctrlConfig, d.abilityManager, ce)

	// 注册执行器
	ctrl.AddTaskExecutor(hschema.DeepTaskType, task_loop.BuildDeepExecutor(d))
	ctrl.AddTaskExecutor(hschema.SessionSpawnTaskType, task_loop.BuildSessionSpawnExecutor(d))

	handler := task_loop.NewTaskLoopEventHandler(d)
	handler.SetInteractionQueues(queues)
	ctrl.SetEventHandler(handler)

	// 注入 SessionToolkit
	d.configMu.RLock()
	toolkit := d.sessionToolkit
	d.configMu.RUnlock()
	if toolkit != nil {
		handler.SetSessionToolkit(toolkit)
	}

	d.configMu.Lock()
	d.loopCoordinator = coord
	d.loopController = ctrl
	d.loopSession = sess
	d.configMu.Unlock()

	return coord, ctrl, nil
}

// forceCleanupController 强制清理已有控制器（会话切换时）。
// Python: DeepAgent._force_cleanup_controller() (line 1928)
func (d *DeepAgent) forceCleanupController(ctx context.Context) {
	d.configMu.RLock()
	ctrl := d.loopController
	loopSess := d.loopSession
	d.configMu.RUnlock()

	if ctrl == nil {
		return
	}

	logLoop("forcing controller cleanup due to session switch", "", 0)

	if loopSess != nil {
		if err := ctrl.UnbindSession(ctx, loopSess); err != nil {
			logger.Warn(logComponent).Err(err).Msg("UnbindSession failed during force cleanup")
		}
	}

	if err := ctrl.Stop(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("Stop failed during force cleanup")
	}

	d.configMu.Lock()
	d.loopCoordinator = nil
	d.loopController = nil
	d.loopSession = nil
	d.boundSessionID = ""
	d.configMu.Unlock()
}

// hasRemainingTasks 检查任务计划是否还有待执行任务。
// Python: DeepAgent._has_remaining_tasks(session) (line 1909)
func (d *DeepAgent) hasRemainingTasks(sess sessioninterfaces.SessionFacade) bool {
	state := d.LoadState(sess)
	if state.TaskPlan == nil {
		return false
	}
	return state.TaskPlan.GetNextTask() != nil
}

// hasPendingSessionSpawn 检查是否有待处理的 SESSION_SPAWN 任务。
// Python: DeepAgent._has_pending_session_spawn() (line 1916)
func (d *DeepAgent) hasPendingSessionSpawn() bool {
	d.configMu.RLock()
	toolkit := d.sessionToolkit
	d.configMu.RUnlock()

	if toolkit == nil {
		return false
	}

	pendingTasks := toolkit.ListAll()
	for _, r := range pendingTasks {
		if r.Status == "running" {
			return true
		}
	}
	return false
}

// findSubagentSpec 查找匹配 subagentType 的子 Agent 规格。
// 返回 SubagentSpec 接口，可能是 *SubAgentConfig 或 *DeepAgent。
// Python: DeepAgent._find_subagent_spec(subagent_type) (line 1032)
func (d *DeepAgent) findSubagentSpec(subagentType string) hschema.SubagentSpec {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil {
		return nil
	}

	for _, spec := range cfg.Subagents {
		if spec.SpecName() == subagentType {
			return spec
		}
	}
	return nil
}

// buildSubagentCreateKwargs 构建子 Agent 创建参数。
// Python: DeepAgent.create_subagent L938-982
func (d *DeepAgent) buildSubagentCreateKwargs(subCfg *hschema.SubAgentConfig, subSessionID string) *hschema.SubagentCreateParams {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil {
		return nil
	}

	// 工作空间路径
	var ws *workspace.Workspace
	if cfg.Workspace != nil {
		ws = &workspace.Workspace{
			RootPath: filepath.Join(cfg.Workspace.RootPath, subSessionID),
			Language: cfg.Language,
		}
	}

	// 合并配置
	model := subCfg.Model
	if model == nil {
		model = cfg.Model
	}

	maxIterations := subCfg.MaxIterations
	if maxIterations == 0 {
		maxIterations = cfg.MaxIterations
	}

	var subWorkspace *workspace.Workspace
	if subCfg.Workspace != nil {
		subWorkspace = subCfg.Workspace
	}
	if subWorkspace == nil {
		subWorkspace = ws
	}

	backend := subCfg.Backend
	if backend == nil {
		backend = cfg.Backend
	}

	var sysOp sysop.SysOperation
	if subCfg.SysOperation != nil && subCfg.Workspace != nil {
		sysOp = subCfg.SysOperation
	}

	language := subCfg.Language
	if language == "" {
		language = cfg.Language
	}

	promptMode := subCfg.PromptMode
	if promptMode == 0 {
		promptMode = cfg.PromptMode
	}

	// RestrictToWorkDir：创建独立指针，避免取 subCfg 字段地址（悬空风险）
	restrictToWorkDir := subCfg.RestrictToWorkDir

	return &hschema.SubagentCreateParams{
		Model:                  model,
		Card:                   subCfg.AgentCard,
		SystemPrompt:           subCfg.SystemPrompt,
		Tools:                  subCfg.Tools,
		ToolInstances:          subCfg.ToolInstances,
		Mcps:                   subCfg.Mcps,
		Rails:                  subCfg.Rails,
		EnableTaskLoop:         subCfg.EnableTaskLoop,
		MaxIterations:          maxIterations,
		Workspace:              subWorkspace,
		Skills:                 subCfg.Skills,
		Backend:                backend,
		SysOperation:           sysOp,
		Language:               language,
		PromptMode:             promptMode,
		Subagents:              nil,
		EnableAsyncSubagent:    false,
		AddGeneralPurposeAgent: false,
		EnablePlanMode:         subCfg.EnablePlanMode,
		RestrictToWorkDir:      &restrictToWorkDir,
	}
}

// drainPendingHarnessConfigs 在处理查询前加载排队的 harness 配置。
// Python: DeepAgent._drain_pending_harness_configs() (line 1607)
func (d *DeepAgent) drainPendingHarnessConfigs(ctx context.Context) error {
	d.railsMu.Lock()
	configs := d.pendingHarnessConfigs
	d.pendingHarnessConfigs = nil
	d.railsMu.Unlock()

	for _, path := range configs {
		loaded, err := d.LoadHarnessConfig(ctx, path)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("path", path).Msg("Failed to auto-load harness config")
			continue
		}
		logger.Info(logComponent).Str("path", path).Any("loaded", loaded).Msg("Harness config auto-loaded successfully")
	}
	return nil
}

// cancelStreamProcess 取消进行中的流处理。
// Python: DeepAgent._cancel_stream_process_task() (line 2432)
func (d *DeepAgent) cancelStreamProcess() {
	d.streamMu.Lock()
	cancel := d.streamCancel
	d.streamCancel = nil
	d.streamMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// readRuntimeState 从会话读取运行时状态缓存。
// Python: DeepAgent._read_runtime_state(session) (line 1758)
func (d *DeepAgent) readRuntimeState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	// 通过 GetState 读取运行时属性
	data, err := sess.GetState(sessstate.StringKey(hschema.SessionRuntimeAttr))
	if err != nil || data == nil {
		return nil
	}
	state, ok := data.(*hschema.DeepAgentState)
	if !ok {
		return nil
	}
	return state
}

// writeRuntimeState 将运行时状态写入会话缓存。
// Python: DeepAgent._write_runtime_state(session, state) (line 1776)
func (d *DeepAgent) writeRuntimeState(sess sessioninterfaces.SessionFacade, state *hschema.DeepAgentState) {
	sess.UpdateState(map[string]any{hschema.SessionRuntimeAttr: state})
}

// clearRuntimeState 从会话清除运行时状态缓存。
// Python: DeepAgent._clear_runtime_state(session) (line 1784)
func (d *DeepAgent) clearRuntimeState(sess sessioninterfaces.SessionFacade) {
	sess.UpdateState(map[string]any{hschema.SessionRuntimeAttr: nil})
}

// saveState 持久化 DeepAgent 状态到会话。
// Python: DeepAgent.save_state(session, state) (line 1815)
func (d *DeepAgent) saveState(sess sessioninterfaces.SessionFacade, state *hschema.DeepAgentState) {
	target := state
	if target == nil {
		target = d.readRuntimeState(sess)
	}
	if target == nil {
		return
	}
	d.writeRuntimeState(sess, target)
	sess.UpdateState(map[string]any{hschema.SessionStateKey: target.ToSessionDict()})
}

// clearState 清除 DeepAgent 运行时缓存。
// Python: DeepAgent.clear_state(session, clear_persisted) (line 1843)
func (d *DeepAgent) clearState(sess sessioninterfaces.SessionFacade, clearPersisted bool) {
	d.clearRuntimeState(sess)
	if clearPersisted {
		sess.UpdateState(map[string]any{hschema.SessionStateKey: nil})
	}
}

// unregisterToolResource 注销工具资源。
// Python: DeepAgent._unregister_tool_resource(card) (line 178)
func (d *DeepAgent) unregisterToolResource(card *tool.ToolCard) {
	// Python: card.name not in {"free_search", "paid_search"} → return
	if card.Name != "free_search" && card.Name != "paid_search" {
		return
	}
	// Python: if not getattr(card, "id", None) → return
	if card.ID == "" {
		return
	}

	resourceMgr := runner.GetResourceMgr()

	// Python: if Runner.resource_mgr.get_tool(card.id) is None → return
	tools, getErr := resourceMgr.GetTool([]string{card.ID})
	if getErr != nil || len(tools) == 0 {
		return
	}

	// Python: tags = Runner.resource_mgr.get_resource_tag(card.id) or []
	tags := resourceMgr.GetResourceTag(card.ID)

	// Python: if self.card.id in tags and len(tags) > 1
	agentTag := resources_manager.Tag(d.card.ID)
	agentInTags := false
	for _, t := range tags {
		if t == agentTag {
			agentInTags = true
			break
		}
	}
	if agentInTags && len(tags) > 1 {
		// Python: remove_resource_tag(card.id, self.card.id, skip_if_tag_not_exists=True)
		_, err := resourceMgr.RemoveResourceTag(card.ID, []resources_manager.Tag{agentTag}, resources_manager.WithTagSkipIfNotExists())
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("tool_id", card.ID).Msg("Failed to remove tool tags")
		}
		return
	}

	// Python: if self.card.id in tags or not tags
	if agentInTags || len(tags) == 0 {
		_, err := resourceMgr.RemoveTool([]string{card.ID})
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("tool_id", card.ID).Msg("Failed to unregister tool")
		}
	}
}

// ensureBuiltinToolResource 确保内置工具资源已注册。
// Python: DeepAgent._ensure_builtin_tool_resource(card, config) (line 206)
func (d *DeepAgent) ensureBuiltinToolResource(card *tool.ToolCard, config *hschema.DeepAgentConfig) {
	// Python: card.name not in {"free_search", "paid_search"} → return
	if card.Name != "free_search" && card.Name != "paid_search" {
		return
	}

	resourceMgr := runner.GetResourceMgr()

	// Python: existing_tool = Runner.resource_mgr.get_tool(card.id)
	tools, getErr := resourceMgr.GetTool([]string{card.ID})
	if getErr == nil && len(tools) > 0 {
		// Python: add_resource_tag(card.id, self.card.id)
		_, tagErr := resourceMgr.AddResourceTag(card.ID, []resources_manager.Tag{resources_manager.Tag(d.card.ID)})
		if tagErr != nil {
			logger.Warn(logComponent).Err(tagErr).Str("tool_id", card.ID).Msg("Failed to tag existing search tool")
		}
		return
	}

	// Python: tool_cls = WebPaidSearchTool if card.name == "paid_search" else WebFreeSearchTool
	// Python: tool = tool_cls(language=resolve_language(config.language), card=card)
	// Python: Runner.resource_mgr.add_tool(tool, tag=self.card.id)
	language := hprompts.ResolveLanguage(config.Language)
	var webTool tool.Tool
	if card.Name == "paid_search" {
		webTool = web_tools.NewWebPaidSearchTool(language, d.card.ID)
	} else {
		webTool = web_tools.NewWebFreeSearchTool(language, d.card.ID)
	}
	if addErr := resourceMgr.AddTool(webTool, resources_manager.WithTag(resources_manager.Tag(d.card.ID))); addErr != nil {
		logger.Warn(logComponent).Err(addErr).Str("tool_id", card.ID).Msg("Failed to register search tool")
	}
}

// emitStreamError 把流启动失败转为 error 类型的输出帧投进流通道。
// 对齐 Python interface_deep.py L4889-4898：流式任务抛异常时由适配器
// `except Exception` 产出 chat.error。Go 的 agent 流无法抛异常，改为在流内
// 投一个 Type="error" 的输出帧，由 DeepAdapter.ParseStreamChunk 的
// `case "error"` 分支映射为 chat.error——否则启动期错误只落日志，
// 消费者看到空流，ACP 侧最终只剩 idle → end_turn（用户看不到任何错误）。
func emitStreamError(outCh chan stream.Schema, err error) {
	if err == nil {
		return
	}
	// 适配器按 Payload 桥接为 map（utils.PayloadToMap），载荷用 map 最直接
	outCh <- &stream.OutputSchema{
		Type: "error",
		Payload: map[string]any{
			"error": err.Error(),
		},
		IsLastSchema: true,
	}
}

// logLoop 记录外层循环日志。
// Python: DeepAgent._log_loop(msg, detail) (line 1951)
func logLoop(format string, detail string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if detail != "" {
		logger.Info(logComponent).Str("event_type", "OuterLoop").Str("msg", msg+detail).Msg("")
	} else {
		logger.Info(logComponent).Str("event_type", "OuterLoop").Str("msg", msg).Msg("")
	}
}

// normalizeFactoryName 规范化工厂名称。
func normalizeFactoryName(name string) string {
	result := make([]byte, 0, len(name))
	for _, c := range name {
		if c == ' ' || c == '\t' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// joinStrings 拼接字符串切片。
func joinStrings(parts []string) string {
	result := make([]byte, 0, len(parts)*32)
	for _, s := range parts {
		result = append(result, s...)
	}
	return string(result)
}

// isTaskCompletionRail 判断 Rail 是否为 TaskCompletionRail 类型。
func isTaskCompletionRail(r agentinterfaces.AgentRail) bool {
	// ⤴️ 9.12 回填：TaskCompletionRail 类型检查
	_, ok := r.(*rails.TaskCompletionRail)
	return ok
}

// removeRailByRef 按引用移除 Rail。
func removeRailByRef(rails []agentinterfaces.AgentRail, target agentinterfaces.AgentRail) []agentinterfaces.AgentRail {
	var result []agentinterfaces.AgentRail
	for _, r := range rails {
		if r != target {
			result = append(result, r)
		}
	}
	return result
}

// removePendingRailsByRefs 按引用批量摘除排队队列中的 Rail。
// S-01 专用：ensureInitialized 在每条 rail Init 成功后调用，把已处理项从
// d.pendingRails 中剔除；遍历期间经 AddRail 新追加的项不受影响。按引用（而非按类型）
// 摘除，避免同类型的其它实例被误删。
func (d *DeepAgent) removePendingRailsByRefs(done []agentinterfaces.AgentRail) {
	if len(done) == 0 {
		return
	}
	d.railsMu.Lock()
	defer d.railsMu.Unlock()
	remaining := make([]agentinterfaces.AgentRail, 0, len(d.pendingRails))
	for _, pending := range d.pendingRails {
		processed := false
		for _, r := range done {
			if pending == r {
				processed = true
				break
			}
		}
		if !processed {
			remaining = append(remaining, pending)
		}
	}
	d.pendingRails = remaining
}

// filterRailsNotType 按类型谓词过滤 Rail。
func filterRailsNotType(rails []agentinterfaces.AgentRail, predicate func(agentinterfaces.AgentRail) bool) []agentinterfaces.AgentRail {
	var result []agentinterfaces.AgentRail
	for _, r := range rails {
		if !predicate(r) {
			result = append(result, r)
		}
	}
	return result
}

// matchType 检查 Rail 是否匹配指定类型列表。
func matchType(r agentinterfaces.AgentRail, types []reflect.Type) bool {
	rType := reflect.TypeOf(r)
	for _, t := range types {
		if rType == t {
			return true
		}
	}
	return false
}

// permissionsSectionToMap 将 PermissionsSection 转换为 map[string]any 供 PermissionEngine 使用。
// Python: config = cast(dict[str, Any], config)
func permissionsSectionToMap(perm *hschema.PermissionsSection) map[string]any {
	if perm == nil {
		return make(map[string]any)
	}
	// 通过 JSON 序列化/反序列化实现结构体→map 转换
	data, err := json.Marshal(perm)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("permissionsSectionToMap serialization failed, returning empty config")
		return make(map[string]any)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("permissionsSectionToMap deserialization failed, returning empty config")
		return make(map[string]any)
	}
	return result
}

// buildTaskLoopEvaluators 构建外层任务循环的停止条件评估器。
// 任务完成 rail 产出的评估器在前，无进展守卫按配置追加在后（OR 语义：先命中先停）。
// Python: DeepAgent._build_task_loop_evaluators() (deep_agent.py L2279-2311)
func (d *DeepAgent) buildTaskLoopEvaluators() []task_loop.StopConditionEvaluator {
	var evaluators []task_loop.StopConditionEvaluator
	if d.taskCompletionRail != nil {
		evaluators = taskCompRailEvaluators(d.taskCompletionRail)
	}
	if d.deepConfig == nil {
		return evaluators
	}
	guard := d.deepConfig.TaskLoopNoProgressGuard
	if !guard.Enabled {
		return evaluators
	}
	// 阈值取上游的"鸭子类型回退"默认（3 / 80）：只开开关不填阈值时不至于退化成
	// 「1 轮任意短答案即停」。上游 DeepAgentConfig 构造默认是 3/20，回退分支是 3/80，
	// 这里按回退分支对齐（config.py:2563-2581）。
	maxConsecutive := guard.MaxConsecutiveEmptyAnswers
	if maxConsecutive <= 0 {
		maxConsecutive = 3
	}
	minChars := guard.MinAnswerChars
	if minChars <= 0 {
		minChars = 80
	}
	evaluators = append(evaluators, task_loop.NewNoProgressAnswerEvaluator(maxConsecutive, minChars))
	return evaluators
}

// taskCompRailEvaluators 取任务完成 rail 产出的评估器。
func taskCompRailEvaluators(rail *rails.TaskCompletionRail) []task_loop.StopConditionEvaluator {
	return rail.BuildEvaluators()
}

// maxOuterRounds 外层任务循环的轮次硬上限。
// 评估器可能一个都没配置（LoopCoordinator.ShouldContinue 恒真），这是防无限循环的最后兜底。
// Python: deep_agent.py L2665（max_outer_rounds = 50）
const maxOuterRounds = 50

// outerRoundsExceededResult 判断外层轮次是否超限，超限时返回显式错误结果。
// Python: deep_agent.py L2667-2677
func outerRoundsExceededResult(outerRound int) (map[string]any, bool) {
	if outerRound <= maxOuterRounds {
		return nil, false
	}
	return map[string]any{
		"output":      "Task loop stopped after exceeding the maximum number of outer rounds (50). This usually indicates the model is not making observable progress.",
		"result_type": "error",
		"stop_reason": "MaxOuterRounds",
	}, true
}

// logInnerReactMaxIterations 记录内层 ReAct 上限的解析结果（文案与上游逐字一致）。
// Python: _bind_inner_react_max_iterations() (deep_agent.py:248-263)
func logInnerReactMaxIterations(configured, resolved int) {
	if configured == 0 {
		logger.Info(logComponent).Msg("[DeepAgent] inner ReAct max_iterations=unbounded (default)")
		return
	}
	logger.Info(logComponent).Msgf("[DeepAgent] inner ReAct max_iterations=%d (configured)", resolved)
}

// ──────────────────────────── 交互监督器内部实现（9.87）────────────────────────────
//
// 注意：上游 _detach_current_output_locked（L4230）在全仓**无调用方**（纯遗留死方法），
// 按规则 8「禁止保留无调用方的实现」未移植（spec D13）。

// SchemaType 返回帧类型标识
func (roundBoundaryFrame) SchemaType() string { return "round_boundary" }

// Validate 边界帧无字段，恒通过
func (roundBoundaryFrame) Validate() error { return nil }

// ToMap 边界帧不参与序列化
func (roundBoundaryFrame) ToMap() map[string]any { return nil }

// ensureInteractionState 惰性创建交互零件（幂等）。
// Python: __init__ 里直接构造（单线程无竞态），Go 用 sync.Once 保证只建一次。
func (d *DeepAgent) ensureInteractionState() {
	d.interactionStateOnce.Do(func() {
		d.interactionEvents = task_loop.NewEventManager()
		d.interactionOutput = hschema.NewOutputLeaseManager()
		d.interactionWakeup = make(chan struct{}, 1)
		d.interactionPhase = hschema.InteractionPhaseIdle
	})
}

// tryTransitionInteractionPhase 迁移相位；从 terminated 迁出被拒绝。
// Python: _try_transition_interaction_phase (L4093)
func (d *DeepAgent) tryTransitionInteractionPhase(target hschema.InteractionPhase) bool {
	d.ensureInteractionState()
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	if d.interactionPhase == hschema.InteractionPhaseTerminated &&
		target != hschema.InteractionPhaseTerminated {
		return false
	}
	d.interactionPhase = target
	return true
}

// isInteractionRunning 返回交互循环是否在运行。
// Python: _is_interaction_running (L4090)
func (d *DeepAgent) isInteractionRunning() bool {
	d.ensureInteractionState()

	d.interactionControlMu.Lock()
	phase := d.interactionPhase
	d.interactionControlMu.Unlock()

	d.interactionSupervisorMu.Lock()
	started := d.interactionStarted
	d.interactionSupervisorMu.Unlock()

	return started && phase != hschema.InteractionPhaseTerminated
}

// isInteractionRunningLocked 同 isInteractionRunning，但要求调用方已持 controlMu。
// Go 的 sync.Mutex 不可重入：持锁路径必须用本变体，否则自锁。
func (d *DeepAgent) isInteractionRunningLocked() bool {
	phase := d.interactionPhase

	d.interactionSupervisorMu.Lock()
	started := d.interactionStarted
	d.interactionSupervisorMu.Unlock()

	return started && phase != hschema.InteractionPhaseTerminated
}

// ensureInteractionRunning 未运行时返回 ErrInteractionNotRunning。
// Python: _ensure_interaction_running (L4086)
func (d *DeepAgent) ensureInteractionRunning() error {
	if !d.isInteractionRunning() {
		return ErrInteractionNotRunning
	}
	return nil
}

// attachOutputLocked 领取租约并包成输出句柄（调用方持 controlMu）。
// Python: _attach_output_locked (L4213)
func (d *DeepAgent) attachOutputLocked() *hschema.InteractionOutputStream {
	lease := d.interactionOutput.Attach()
	if lease == nil {
		return nil
	}
	return hschema.NewInteractionOutputStream(d, lease)
}

// emitInteractionEvent 异步投递一条交互事件。
// 回调是同步的（rail 可从多个钩子调用），只能 fire-and-forget；
// 必须在同步上下文里先抓票号，避免延迟任务把旧事件投进新租约的流。
// Python: _emit_interaction_event (L4254)
func (d *DeepAgent) emitInteractionEvent(event hschema.InteractionEvent) {
	d.ensureInteractionState()

	token := d.interactionOutput.CurrentToken()
	if token == "" {
		return
	}

	d.interactionSupervisorMu.Lock()
	if d.interactionStopping {
		d.interactionSupervisorMu.Unlock()
		return
	}
	d.interactionEmitWG.Add(1)
	d.interactionSupervisorMu.Unlock()

	go func() {
		defer d.interactionEmitWG.Done()
		d.interactionOutput.Emit(event.ToOutputSchema(), token)
	}()
}

// waitInteractionEmits 有界等待事件投递 goroutine 收尾。
func (d *DeepAgent) waitInteractionEmits(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		d.interactionEmitWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		logger.Warn(logComponent).Msg("interaction emit tasks did not finish within timeout")
	}
}

// cancelActiveRoundLocked 取消在飞轮（调用方持 controlMu）。
// 顺序：① abort() ② 取消 round goroutine（**不等它退出**：其 defer 需要 controlMu，
// 等待会自锁）③ scheduler.CancelTask 有界等待 1.5s。
// Python: _cancel_active_round (L4287)
func (d *DeepAgent) cancelActiveRoundLocked(ctx context.Context, reason string, expected *RoundExpectation) {
	active := d.activeInteractionRound
	if active == nil {
		return
	}
	if expected != nil {
		if expected.RunKind != nil && active.RunKind() != *expected.RunKind {
			return
		}
		if expected.GoalID != "" && active.GoalID() != expected.GoalID {
			return
		}
		if expected.Revision != nil && active.Revision() != *expected.Revision {
			return
		}
	}

	logger.Info(logComponent).Str("run_kind", string(active.RunKind())).Str("reason", reason).
		Msg("cancelling interaction round")

	taskID := active.TaskID
	cancel := d.interactionRoundCancel

	// 1) 先发中止信号
	d.Abort(ctx)

	// 2) 取消 round goroutine —— 不等待其退出（见上）
	if cancel != nil {
		cancel()
	}

	// 3) 有界等待调度任务取消
	ctrl := d.currentLoopController()
	if taskID == "" || ctrl == nil {
		return
	}
	scheduler := ctrl.TaskScheduler()
	if scheduler == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := scheduler.CancelTask(ctx, taskID); err != nil {
			logger.Debug(logComponent).Err(err).Str("task_id", taskID).
				Msg("cancel_task raised during round cancel")
		}
	}()
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		logger.Warn(logComponent).Str("task_id", taskID).Str("reason", reason).
			Msg("cancel_task timed out after 1.5s; continuing without waiting for LLM")
	}
}

// currentLoopController 返回任务循环控制器（nil 安全）。
// Python: DeepAgent.loop_controller
func (d *DeepAgent) currentLoopController() *task_loop.TaskLoopController {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.loopController
}

// notifyWork 唤醒 supervisor（启动职责见 startInteractionSupervisor）。
// ⚠️ 不得取 interactionControlMu：本方法会在持 controlMu 的 sendUser 内被调用。
// Python: _notify_work (L4369)
func (d *DeepAgent) notifyWork() {
	d.ensureInteractionState()
	select {
	case d.interactionWakeup <- struct{}{}:
	default:
	}
	_ = d.ensureSupervisorRunning(context.Background())
}

// forwardSessionStream 把会话流转发到输出租约队列（唯一消费者）。
// 边界帧不转发，只置位本轮屏障；其余帧原样 Emit（无票即丢）。
// 转发器崩溃必须转成 execution.error(forwarder_crashed)，否则交互会静默卡死。
// Python: _forward_session_stream (L4462)
func (d *DeepAgent) forwardSessionStream(ctx context.Context) {
	sess := d.interactionSession
	if sess == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Interface("panic", r).Msg("output forwarder crashed")
			d.emitInteractionEvent(hschema.NewExecutionErrorEvent(
				"forwarder_crashed", "output forwarder crashed", nil))
		}
	}()
	// 迭代器只取一次：Session.StreamIterator() 每次调用都会新起一个队列消费者，
	// 在循环条件里反复调用会让多个消费者互相抢帧（Python 侧是 async for 单次获取）。
	iter := sess.StreamIterator()
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-iter:
			if !ok {
				return
			}
			if _, isBoundary := chunk.(roundBoundaryFrame); isBoundary {
				d.signalRoundForwarded()
				continue
			}
			d.interactionOutput.Emit(chunk, "")
		}
	}
}

// signalRoundForwarded 置位本轮转发屏障（关闭通道，幂等）。
func (d *DeepAgent) signalRoundForwarded() {
	d.interactionControlMu.Lock()
	forwarded := d.interactionRoundForwarded
	d.interactionControlMu.Unlock()
	closeChanSafely(forwarded)
}

// emitRoundBoundary 向会话流注入内部轮边界哨兵。
// 必须与普通 chunk 走同一条队列才能保证顺序；绕过 writer 回调与类型白名单
// （对齐 Python 的 session._inner.stream_writer_manager().stream_emitter().emit）。
// Python: _emit_round_boundary (L4481)
func (d *DeepAgent) emitRoundBoundary(sess *session.Session) bool {
	if sess == nil {
		return false
	}
	emitter := sess.StreamEmitter()
	if emitter == nil {
		return false
	}
	if err := emitter.Emit(context.Background(), roundBoundaryFrame{}); err != nil {
		logger.Debug(logComponent).Err(err).Msg("unable to emit ordered round boundary")
		return false
	}
	return true
}

// closeChanSafely 幂等关闭通道。
func closeChanSafely(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// prepareInteractionTaskLoop 准备并绑定常驻交互循环的任务循环内核。
// Python: prepare_interaction_task_loop (L3731)
func (d *DeepAgent) prepareInteractionTaskLoop(ctx context.Context, sess *session.Session) (*task_loop.LoopCoordinator, *task_loop.TaskLoopController, error) {
	coord, ctrl, err := d.setupTaskLoop(ctx, sess)
	if err != nil {
		return nil, nil, err
	}
	if d.boundSessionIDValue() != sess.GetSessionID() {
		if bindErr := ctrl.BindSession(ctx, sess); bindErr != nil {
			logger.Warn(logComponent).Err(bindErr).Msg("bind interaction session failed")
		}
	}
	d.configMu.Lock()
	d.loopSession = sess
	d.boundSessionID = sess.GetSessionID()
	d.configMu.Unlock()
	return coord, ctrl, nil
}

// PrepareInteractionTaskLoop 供宿主自建任务循环内核。
// Python: NativeHarness.start → self._setup_task_loop(session)
// Python 靠继承直接调用私有方法；Go 跨包无法访问，故开这一扇窄门
// （25.1a 唯一的私有缝）。语义与 prepareInteractionTaskLoop 完全一致：
// 创建或复用 Controller 基础设施，并把会话绑定到内核。
func (d *DeepAgent) PrepareInteractionTaskLoop(ctx context.Context, sess *session.Session) (*task_loop.LoopCoordinator, *task_loop.TaskLoopController, error) {
	return d.prepareInteractionTaskLoop(ctx, sess)
}

// boundSessionIDValue 返回已绑定会话标识（与字段 boundSessionID 区分命名）。
func (d *DeepAgent) boundSessionIDValue() string {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.boundSessionID
}

// stopInteractionLocked 停机实现。
// Python: _stop_interaction_locked (L4003)
func (d *DeepAgent) stopInteractionLocked(ctx context.Context) error {
	d.tryTransitionInteractionPhase(hschema.InteractionPhaseTerminated)

	d.interactionSupervisorMu.Lock()
	started := d.interactionStarted
	d.interactionStopping = true
	d.interactionSupervisorMu.Unlock()
	if !started {
		return nil
	}

	// EventManager 无内部锁（K3）：所有访问必须在 controlMu 内，
	// 否则会与 supervisor 的 nextWorkLocked / markStarted 竞争。
	d.interactionControlMu.Lock()
	d.interactionEvents.DiscardAllWork()
	d.interactionControlMu.Unlock()

	d.interactionOutput.Shutdown()

	d.interactionControlMu.Lock()
	d.cancelActiveRoundLocked(ctx, "stop", nil)
	d.interactionControlMu.Unlock()

	d.stopSupervisor()
	d.stopForwarder()
	d.waitInteractionEmits(2 * time.Second)

	if d.interactionSession != nil {
		_ = d.interactionSession.CloseStream()
	}
	if ctrl := d.currentLoopController(); ctrl != nil && d.interactionSession != nil {
		_ = ctrl.UnbindSession(ctx, d.interactionSession)
		_ = ctrl.Stop(ctx)
	}

	d.interactionSupervisorMu.Lock()
	d.interactionStarted = false
	d.interactionSupervisorMu.Unlock()

	d.interactionControlMu.Lock()
	d.interactionEvents.DiscardAllWork()
	d.interactionControlMu.Unlock()
	d.tryTransitionInteractionPhase(hschema.InteractionPhaseTerminated)

	d.interactionControlMu.Lock()
	d.activeInteractionRound = nil
	d.interactionRoundCancel = nil
	d.interactionRoundDone = nil
	d.interactionRoundForwarded = nil
	d.interactionControlMu.Unlock()
	return nil
}

// startForwarder 启动常驻输出转发器。
func (d *DeepAgent) startForwarder(ctx context.Context) {
	d.interactionSupervisorMu.Lock()
	defer d.interactionSupervisorMu.Unlock()
	if d.interactionForwarderDone != nil {
		return
	}
	// 转发器的生命周期归交互，**不继承调用方（请求/轮次）的取消**。
	// Python: _forward_session_stream 是挂在事件循环上的 task，活到 stop()；
	// 若继承请求 ctx，第一轮请求结束就退出，第 2 轮起租约拿到却 0 帧
	//（流式只剩 keepalive）。停止由 stopForwarder 显式 cancel。
	fctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	d.interactionForwarderCancel = cancel
	d.interactionForwarderDone = done
	go func() {
		defer close(done)
		d.forwardSessionStream(fctx)
	}()
}

// stopForwarder 取消转发器并等待退出（有界）。
func (d *DeepAgent) stopForwarder() {
	d.interactionSupervisorMu.Lock()
	cancel, done := d.interactionForwarderCancel, d.interactionForwarderDone
	d.interactionForwarderDone = nil
	d.interactionForwarderCancel = nil
	d.interactionSupervisorMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			logger.Warn(logComponent).Msg("interaction forwarder did not stop within 2s")
		}
	}
}

// ensureSupervisorRunning 若 supervisor 未运行则启动。
// ⚠️ 只碰 supervisor 锁与缓存的 started/stopping 标记，不取 controlMu。
// Python: _ensure_supervisor_running (L4373)
// 返回值为"supervisor 已进入首个等待点"的信号通道；若已在运行则返回 nil。
func (d *DeepAgent) ensureSupervisorRunning(ctx context.Context) <-chan struct{} {
	d.interactionSupervisorMu.Lock()
	defer d.interactionSupervisorMu.Unlock()
	if d.interactionSupervisorRunning || !d.interactionStarted || d.interactionStopping {
		return nil
	}
	// 同上：supervisor 是交互级常驻循环，不随单次请求结束。
	// Python: _ensure_supervisor_running 的 task 挂在事件循环上。
	sctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	ready := make(chan struct{}, 1)
	d.interactionSupervisorCancel = cancel
	d.interactionSupervisorDone = done
	d.interactionSupervisorRunning = true
	go func() {
		defer close(done)
		defer func() {
			d.interactionSupervisorMu.Lock()
			d.interactionSupervisorRunning = false
			d.interactionSupervisorMu.Unlock()
		}()
		d.supervisorLoop(sctx, ready)
	}()
	return ready
}

// waitSupervisorReady 等待 supervisor 停在首个等待点（有界）。
// 必要性（Go 特有）：Python 的 start() 里 create_task 之后同一协程继续，
// 宿主拿到 start 的返回值时 supervisor 尚未运行；它首次运行必然看到
// "无消费者 → 睡下"。Go 的 goroutine 是异步的，若首次迭代被推迟到宿主
// attach 之后，就会立刻走 close_idle 收尾，把刚建立的流结束掉。
// 因此 Start 必须等 supervisor 真正睡下再返回，复刻 Python 的时序。
func (d *DeepAgent) waitSupervisorReady(ready <-chan struct{}, timeout time.Duration) {
	if ready == nil {
		return
	}
	select {
	case <-ready:
	case <-time.After(timeout):
		logger.Warn(logComponent).Msg("interaction supervisor did not reach idle wait in time")
	}
}

// stopSupervisor 取消 supervisor 并等待退出（有界）。
func (d *DeepAgent) stopSupervisor() {
	d.interactionSupervisorMu.Lock()
	cancel, done := d.interactionSupervisorCancel, d.interactionSupervisorDone
	d.interactionSupervisorCancel = nil
	d.interactionSupervisorDone = nil
	d.interactionSupervisorMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			logger.Warn(logComponent).Msg("interaction supervisor did not stop within 2s")
		}
	}
}

// supervisorLoop 交互监督器主循环（唯一消费者）。
// Python: _supervisor_loop (L4377)
func (d *DeepAgent) supervisorLoop(ctx context.Context, ready chan struct{}) {
	signaled := false
	signalReady := func() {
		if signaled {
			return
		}
		signaled = true
		// ready 恒非 nil（仅 ensureSupervisorRunning 现造通道时传入），容量 1 且只送一次，
		// 不会阻塞；无人等待时（notifyWork 路径）信号留在缓冲里。
		ready <- struct{}{}
	}
	// 兜底：无论从哪条路径退出，都不能让 Start 卡在等待上
	defer signalReady()

	for d.isInteractionRunning() {
		if !d.interactionOutput.HasConsumer() {
			if !d.tryTransitionInteractionPhase(hschema.InteractionPhaseIdle) {
				return
			}
			signalReady()
			if !d.waitForWork(ctx) {
				return
			}
			continue
		}

		work := d.nextWorkLocked()
		if work == nil {
			d.promoteLoopFollowUps()
			work = d.nextWorkLocked()
		}
		if work == nil {
			d.closeIdleOutputIfFinished(ctx)
			if !d.isInteractionRunning() {
				return
			}
			if !d.tryTransitionInteractionPhase(hschema.InteractionPhaseIdle) {
				return
			}
			if d.hasPendingWorkLocked() {
				continue
			}
			signalReady()
			if !d.waitForWork(ctx) {
				return
			}
			continue
		}

		d.executeRoundAndWait(ctx, work)
	}
}

// waitForWork 阻塞等待唤醒或上下文取消；返回 false 表示应退出。
func (d *DeepAgent) waitForWork(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-d.interactionWakeup:
		return true
	}
}

// nextWorkLocked 在 controlMu 内取下一个 work。
// Python: event_manager.next_work（supervisor 侧调用点）
func (d *DeepAgent) nextWorkLocked() *hschema.RoundWorkItem {
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	return d.interactionEvents.NextWork()
}

// hasPendingWorkLocked 在 controlMu 内判断是否有待办。
func (d *DeepAgent) hasPendingWorkLocked() bool {
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	return d.interactionEvents.HasPendingWork()
}

// promoteLoopFollowUps 把控制器 follow-up 队列提升为 user work。
// Python: _promote_loop_follow_ups (L4418)
func (d *DeepAgent) promoteLoopFollowUps() {
	ctrl := d.currentLoopController()
	if ctrl == nil || !ctrl.HasFollowUp() {
		return
	}
	for _, query := range ctrl.DrainFollowUp() {
		work := hschema.NewUserWorkItem("", map[string]any{"query": query}, true, false)
		d.interactionControlMu.Lock()
		err := d.interactionEvents.PushUser(work)
		d.interactionControlMu.Unlock()
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("promote follow-up failed")
		}
	}
}

// closeIdleOutputIfFinished 在最后一批排队轮次结束后结束普通响应流。
// Python: _close_idle_output_if_finished (L4433)
func (d *DeepAgent) closeIdleOutputIfFinished(_ context.Context) bool {
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	if !d.isInteractionRunningLocked() {
		return false
	}
	keep, err := d.shouldKeepInteractionOpenLocked()
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("ensure goal work failed while closing idle output")
	}
	if keep {
		return true
	}
	d.interactionOutput.FinishCurrent()
	return true
}

// shouldKeepInteractionOpenLocked 返回当前输出流是否仍持有活的工作（调用方持 controlMu）。
// 副作用（刻意）：GoalRecord 为 ACTIVE 时顺带 ensureActiveGoalWorkLocked，
// 让下一次尝试在宿主观察到空闲完成之前就排好队。
// Python: _should_keep_interaction_open_locked (L4442)
func (d *DeepAgent) shouldKeepInteractionOpenLocked() (bool, error) {
	if d.interactionEvents.HasPendingWork() || d.activeInteractionRound != nil {
		return true, nil
	}
	if d.goalCoordinator != nil && d.goalCoordinator.HasActiveRecord() {
		if _, err := d.goalCoordinator.EnsureActiveGoalWorkLocked(); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// sendUser 输入分发实现（调用方持 sendMu）。
// Python: _send_user (L4104)
func (d *DeepAgent) sendUser(_ context.Context, req hschema.SendInputRequest) error {
	if req.Inputs == nil {
		return fmt.Errorf("send_input requires inputs['query'] to be a non-empty string or InteractiveInput")
	}
	query := req.Inputs["query"]
	_, isResume := query.(*interaction.InteractiveInput)
	if !isResume {
		text, ok := query.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fmt.Errorf("send_input requires inputs['query'] to be a non-empty string or InteractiveInput")
		}
	}

	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()

	if !d.isInteractionRunningLocked() {
		return ErrInteractionNotRunning
	}

	// ① 打断恢复输入：直接入队，不重置循环
	if isResume {
		if err := d.interactionEvents.PushUser(
			hschema.NewUserWorkItem(req.RequestID, req.Inputs, false, false)); err != nil {
			return err
		}
		d.notifyWork()
		return nil
	}

	// ② STEER：直塞活跃轮的转向队列
	mode := req.Mode
	if mode == "" {
		mode = hschema.InputDispatchModeFollowUp
	}
	if mode != hschema.InputDispatchModeFollowUp && mode != hschema.InputDispatchModeSteer {
		return fmt.Errorf("unsupported input dispatch mode: %s", req.Mode)
	}
	if mode == hschema.InputDispatchModeSteer && d.activeInteractionRound != nil {
		ctrl := d.currentLoopController()
		if ctrl == nil {
			return fmt.Errorf("active interaction round cannot accept steer without loop controller")
		}
		ctrl.EnqueueSteer(fmt.Sprintf("%v", query))
		d.notifyWork()
		return nil
	}

	// ③ 保持流开：作为 follow-up 入队
	keepOpen, keepErr := d.shouldKeepInteractionOpenLocked()
	if keepErr != nil {
		return keepErr
	}
	if keepOpen {
		if err := d.interactionEvents.PushUser(
			hschema.NewUserWorkItem(req.RequestID, req.Inputs, true, false)); err != nil {
			return err
		}
		d.notifyWork()
		return nil
	}

	// ④ 新的一轮：重置循环
	// 输出租约始终由宿主经 AttachOutput 持有；send_input 不抢也不建流。
	if err := d.interactionEvents.PushUser(
		hschema.NewUserWorkItem(req.RequestID, req.Inputs, false, true)); err != nil {
		return err
	}
	d.notifyWork()
	return nil
}

// buildInteractionNextWork 决定本轮之后是否还要排一个 user work。
// Python: _build_interaction_next_work (L3844)
func (d *DeepAgent) buildInteractionNextWork(
	_ context.Context,
	work *hschema.RoundWorkItem,
	result map[string]any,
	sess *session.Session,
	coord *task_loop.LoopCoordinator,
	ctrl *task_loop.TaskLoopController,
) *hschema.RoundWorkItem {
	coord.IncrementIteration()
	coord.SetLastResult(result)

	state := d.LoadState(sess)
	exported := coord.ExportState()
	state.StopConditionState = map[string]any{
		"iteration":        exported.Iteration,
		"token_usage":      exported.TokenUsage,
		"stop_reason":      exported.StopReason,
		"evaluator_states": exported.EvaluatorStates,
	}
	if ctrl != nil {
		state.PendingFollowUps = append(state.PendingFollowUps, ctrl.DrainFollowUp()...)
	}
	d.saveState(sess, state)

	if resultType, _ := result["result_type"].(string); resultType == "interrupt" || coord.IsAborted() {
		return nil
	}
	if !coord.ShouldContinue() || work.Kind == hschema.RoundWorkKindGoal {
		return nil
	}

	state = d.LoadState(sess)
	if len(state.PendingFollowUps) > 0 {
		query := state.PendingFollowUps[0]
		state.PendingFollowUps = state.PendingFollowUps[1:]
		d.saveState(sess, state)

		inputs := utils.DeepCopyMap(work.Inputs)
		if inputs == nil {
			inputs = make(map[string]any)
		}
		inputs["query"] = query
		return hschema.NewUserWorkItem(work.RequestID, inputs, true, false)
	}
	if d.hasRemainingTasks(sess) {
		return hschema.NewUserWorkItem(work.RequestID, work.Inputs, false, false)
	}
	return nil
}

// executeRoundAndWait 在独立 goroutine 执行一轮并等待其结束与转发屏障。
// Python: supervisor 的 create_task(_execute_round) + asyncio.wait + wait_for(forwarded, 2s)
func (d *DeepAgent) executeRoundAndWait(ctx context.Context, work *hschema.RoundWorkItem) {
	roundCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	forwarded := make(chan struct{})

	d.interactionControlMu.Lock()
	d.interactionRoundCancel = cancel
	d.interactionRoundDone = done
	d.interactionRoundForwarded = forwarded
	d.interactionControlMu.Unlock()

	go func() {
		defer close(done)
		d.executeRound(roundCtx, work)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		cancel()
		<-done
	}

	select {
	case <-forwarded:
	case <-time.After(2 * time.Second):
		logger.Warn(logComponent).Msg("round boundary not forwarded within 2s")
	}

	d.interactionControlMu.Lock()
	d.interactionRoundCancel = nil
	d.interactionRoundDone = nil
	d.interactionRoundForwarded = nil
	d.interactionControlMu.Unlock()
	cancel()
}

// executeRound 执行一轮：相位迁移、goal 尝试、调度下一轮、边界帧。
// Python: _execute_round (L4497)
func (d *DeepAgent) executeRound(ctx context.Context, work *hschema.RoundWorkItem) {
	if !d.isInteractionRunning() {
		return
	}
	if !d.tryTransitionInteractionPhase(hschema.InteractionPhaseRunning) {
		return
	}
	sess := d.interactionSession
	taskID := strings.ReplaceAll(uuid.NewString(), "-", "")

	d.interactionControlMu.Lock()
	d.activeInteractionRound = &hschema.ActiveInteractionRound{Work: work, TaskID: taskID}
	d.interactionEvents.MarkStarted(work)
	d.interactionControlMu.Unlock()

	defer func() {
		d.interactionControlMu.Lock()
		d.interactionEvents.MarkFinished(work)
		d.activeInteractionRound = nil
		forwarded := d.interactionRoundForwarded
		d.interactionControlMu.Unlock()

		if sess != nil && !d.emitRoundBoundary(sess) {
			// 无法注入有序边界帧 → 直接置位屏障兜底（Python 同）
			closeChanSafely(forwarded)
		}
		d.tryTransitionInteractionPhase(hschema.InteractionPhaseIdle)
	}()

	if sess == nil || !d.interactionOutput.HasConsumer() {
		return
	}

	if work.Kind == hschema.RoundWorkKindGoal {
		if d.goalCoordinator == nil {
			return
		}
		record, err := d.goalCoordinator.BeginAttempt(ctx, work.Context.GoalID, work.Context.Revision)
		if err != nil || record == nil {
			// 代际已过期（覆盖/清除/换代）：丢弃这个 work，不执行本轮。
			return
		}
	}

	d.interactionControlMu.Lock()
	override := d.roundOverride
	d.interactionControlMu.Unlock()

	var outcome hschema.RoundOutcome
	if override != nil {
		outcome = override(ctx, work)
	} else {
		outcome = d.RunOneRound(ctx, work, taskID, sess)
	}

	if !d.isInteractionRunning() {
		return
	}
	if outcome.NextWork != nil {
		d.interactionControlMu.Lock()
		if err := d.interactionEvents.PushUser(outcome.NextWork); err != nil {
			logger.Warn(logComponent).Err(err).Msg("enqueue next work failed")
		}
		d.interactionControlMu.Unlock()
		d.notifyWork()
	}
	if outcome.ErrorCode != "" {
		d.emitInteractionEvent(hschema.NewExecutionErrorEvent(outcome.ErrorCode, outcome.ErrorMessage, nil))
	}
}

// syncExpertRoleAttachment 物化专家角色提示词附件。
// ⤵️ #9.3 回填：等待 AgentTemplate 热加载能力落地后补完。
// Python: _sync_expert_role_attachment (L3267)，由 run_one_round (L3787) 在
// BEFORE_INVOKE 生命周期内、resume/submit 分支之前调用。
// 今天的等价性：activeAgentTemplate 恒为 nil（Go 无 load_agent_template），
// 而 expert_role 附件节也永远不会被创建，故直接返回与上游"会话里不存在
// expert_role → 清理分支直接 return"完全等价。
// ⚠️ 回填时必须**同时**补两条分支，只补①不补②会让模型在用户切走专家后
// 仍以为自己是该专家：
//  1. template 非 nil → add_section(expert_role, _expert_role_load_content(role, lang))
//  2. template 为 nil 但会话里已存在 expert_role 节 →
//     改写为 _expert_role_unload_content(role, lang)（降权提示）
//
// 上游对该方法整体 try/except 并只 warning，不阻塞主流程（尽力而为语义）。
func (d *DeepAgent) syncExpertRoleAttachment(_ *agentinterfaces.InvokeInputs, _ *session.Session) {
	d.interactionControlMu.Lock()
	active := d.activeAgentTemplate
	d.interactionControlMu.Unlock()
	if active == nil {
		return
	}
	// 非 nil 在今天是不可达状态；一旦可达说明 9.3 已接线但本方法尚未补完，
	// 必须显式告警而不是静默丢提示词。
	logger.Warn(logComponent).
		Str("load_id", active.LoadID).
		Str("role_name", active.RoleName).
		Msg("expert_role attachment sync not ported yet, waiting for AgentTemplate hot-load (⤵️ #9.3)")
}
