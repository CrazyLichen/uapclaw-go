package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	srvruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PrepareChatTurnFunc 选 agent 并返回 (mode, subMode, agent)，测试可注入。
// agentDefinition / agentDefinitionFingerprint 来自 _agent_execution，
// 供 manager 建声明式 agent（name/instructions/tools/skills）。
// Python: service.py:1279-1283
//
//	prepare_kwargs.update(agent_definition=..., agent_definition_fingerprint=...)
type PrepareChatTurnFunc func(
	ctx context.Context,
	req *schema.AgentRequest,
	channelID string,
	agentDefinition map[string]any,
	agentDefinitionFingerprint string,
) (mode, subMode string, agent ChatAgentPort, err error)

// streamOptions Stream/Invoke 的可选参数。
// Python: stream(request, *, trigger_hook, on_control_event, background,
// on_agent_ready, _agent_execution)
// 偏差 D-7：Python 用关键字参数，Go 无此能力，用 Functional Option。
type streamOptions struct {
	// onAgentReady agent 就绪回调，首次模型调用前触发。
	// Python: on_agent_ready: Callable[[Any], Any] | None
	onAgentReady func(ctx context.Context, agent harnessinterfaces.DeepAgentInterface) error
	// agentExecution 声明式 agent 执行体（携带 definition 与 mode，指纹由 definition 计算）。
	// Python: _agent_execution: RuntimeAgentExecution | None
	agentExecution *RuntimeAgentExecution
}

// StreamOption Stream/Invoke 的可选参数函数。
type StreamOption func(*streamOptions)

// goalActivityChecker 协作方（AgentManager）的 goal 活跃判定能力。
// Python: service.py:545-548 `checker = getattr(manager, "has_active_goal", None)`
//
// 具名窄接口而非匿名断言：与 task_loop.interactionQueuesProvider 同款约定，
// 能力面可检索、可注释；不塞进 AgentManagerPort 是因为 goal 判定只有这一处
// 使用方，塞进去会让全部测试替身各多实现一个方法。
type goalActivityChecker interface {
	// HasActiveGoal 会话是否持有活跃 goal
	HasActiveGoal(channelID, sessionID string) bool
}

// AgentRuntimeDeps AgentRuntime 依赖（构造注入）。
type AgentRuntimeDeps struct {
	// Manager agent 实例管理器
	Manager AgentManagerPort
	// PlanController 计划模式控制器（nil 时新建，server 生命周期内唯一）
	PlanController *PlanModeController
	// OnControlEvent 控制事件回调（nil 时仅记日志）
	OnControlEvent func(*RuntimeEvent)
	// Prepare 聊天轮次准备（nil 时用 PrepareChatTurn）
	Prepare PrepareChatTurnFunc
	// Coordinator 会话执行协调器（nil 时清理路径跳过关执行）
	// Python: service.py:412 self._session_coordinator = session_coordinator or RuntimeSessionCoordinator()
	Coordinator SessionCoordinatorPort
	// ResetPlanSession 重置会话 plan 状态（nil 时跳过）
	// Python: service.py:3070 self._plan_controller.reset_session(session_id)
	ResetPlanSession func(sessionID string)
	// IterManagers 遍历 host 事件可达的 manager（nil 时只用 Manager）
	// Python: service.py:3060-3064 self._tenant.iter_managers_for_host_event() or [self.agent_manager]
	IterManagers func() []AgentManagerPort
	// ResolveModelCapability 模型目录解析（nil 时跳过 model 存在性校验）
	// Python: service.py:706-714 resolve_model_capability / :777-782
	ResolveModelCapability func(model string) (RuntimeModelDescriptor, error)
}

// ErrProvisionerUnavailable 会话开通器未注入。
// Python: 无对应（__init__ 必造 provisioner）；Go 注入式装配的防御性错误。
var ErrProvisionerUnavailable = errors.New("session provisioner is not configured")

// SessionCoordinatorPort 协调器能力接口别名（定义在 runtime/session 包）。
//
// 搬到 runtime/session 的原因：它描述的是协调器自身的能力边界，与
// RuntimeSessionCoordinator 同包；server 侧需要它时只 import runtime/session，
// 不必为一个接口 import 整个 runtime（P-7 前置）。
type SessionCoordinatorPort = sessionpkg.CoordinatorPort

// ErrCoordinatorUnavailable 协调器未注入（定义在 runtime/session 包）。
var ErrCoordinatorUnavailable = sessionpkg.ErrCoordinatorUnavailable

// ErrModelCatalogUnavailable 模型目录未装配
//
// 上游 resolve_model_capability 恒可用（AgentRuntime 自持模型列表）；Go 侧由
// 注入提供。**未装配时必须报错而不是静默跳过校验**：否则"配置了不存在的模型"
// 会被放行到下游，变成运行期才暴露的失败，且错误信息指向不到配置问题。
var ErrModelCatalogUnavailable = errors.New("model catalog is not available")

// sessionRuntimeCleaner 可清理会话内存运行时的 manager 能力。
// Python: server/runtime/agent_manager.py:863 cleanup_session_runtime
//
// 匿名接口断言而非扩 AgentManagerPort：该能力只有清理一处使用，
// 扩主端口会迫使全部测试替身各增一个方法（与 goalActivityChecker 同款取舍）。
type sessionRuntimeCleaner interface {
	// CleanupSessionRuntime 清理会话内存运行时，返回是否清理到内容
	CleanupSessionRuntime(ctx context.Context, channelID, sessionID string) (bool, error)
}

// AgentRuntime 运行时编排：选 agent、同步 plan 状态、执行、下发控制事件。
// Python: jiuwenswarm/runtime/service.py AgentRuntime
type AgentRuntime struct {
	// mu 保护生命周期字段
	mu sync.Mutex
	// started 是否已启动
	started bool
	// closed 是否已关闭
	closed bool
	// manager agent 实例管理器
	manager AgentManagerPort
	// planController 计划模式控制器（runtime 持有，对齐上游 self._plan_controller）
	planController *PlanModeController
	// onControlEvent 控制事件回调
	onControlEvent func(*RuntimeEvent)
	// prepare 聊天轮次准备（可注入）
	prepare PrepareChatTurnFunc
	// agentExecutionOwners 声明式 Agent 定义的会话绑定表
	// Python: service.py:423-426 self._agent_execution_owners
	agentExecutionOwners map[agentExecutionOwnerKey]*RuntimeAgentExecution
	// agentExecutionOwnerMu 保护 owner 表
	// Python: service.py:426 self._agent_execution_owner_lock = asyncio.Lock()
	//
	// 读端也加锁：Python 单线程下同步读安全，Go map 并发读写会 panic（spec D-4）。
	agentExecutionOwnerMu sync.Mutex
	// coordinator 会话执行协调器（清理路径用）
	// Python: service.py:412 self._session_coordinator
	coordinator SessionCoordinatorPort
	// provisioner 会话开通器（创建/切换/分叉/删除的唯一出口）
	// Python: service.py:403-409 self._session_provisioner = RuntimeSessionProvisioner(...)
	//
	// 字段由 server 层注入而非 Runtime 自造：provisioner 的依赖（runner / adapter /
	// recommendation）都在 server 层，runtime 不得反向依赖。注入点见
	// internal/swarm/server/agent_server.go 的 newSessionProvisioner 调用处。
	provisioner *RuntimeSessionProvisioner
	// resetPlanSession 重置会话 plan 状态（可注入）
	// Python: service.py:3070 self._plan_controller.reset_session
	resetPlanSession func(sessionID string)
	// iterManagers 遍历 host 事件可达的 manager（可注入）
	// Python: service.py:3060-3064 iter_managers_for_host_event
	iterManagers func() []AgentManagerPort
	// resolveModelCapability 模型目录解析（可注入）
	// Python: service.py:706-714 resolve_model_capability
	resolveModelCapability func(model string) (RuntimeModelDescriptor, error)
}

// agentExecutionOwnerKey 执行 owner 表的键。
// Python: service.py:423-425 dict[tuple[str, str], RuntimeAgentExecution]
type agentExecutionOwnerKey struct {
	// ChannelID 归一化渠道标识（小写去空白，空则 default）
	ChannelID string
	// SessionID 去空白会话标识（空则不建键）
	SessionID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithOnAgentReady 设置 agent 就绪回调（首次模型调用前触发）。
// Python: stream(request, on_agent_ready=fn)
//
// 偏差 D-8：Python 传 child adapter 对象（Any），Go 传 DeepAgentInterface。
// 理由：configure_process_cli_run 的实质只用到 react_agent 与 ability_manager，
// 二者 DeepAgentInterface 都有。
func WithOnAgentReady(fn func(ctx context.Context, agent harnessinterfaces.DeepAgentInterface) error) StreamOption {
	return func(o *streamOptions) { o.onAgentReady = fn }
}

// WithAgentExecution 携带声明式 agent 执行体。
// Python: stream(request, _agent_execution=execution)
func WithAgentExecution(exec *RuntimeAgentExecution) StreamOption {
	return func(o *streamOptions) { o.agentExecution = exec }
}

// NewAgentRuntime 创建运行时。
// Python: AgentRuntime.__init__
func NewAgentRuntime(deps AgentRuntimeDeps) *AgentRuntime {
	controller := deps.PlanController
	if controller == nil {
		controller = NewPlanModeController()
	}
	// Python:414 self._session_coordinator = session_coordinator or RuntimeSessionCoordinator()
	//
	// 账本归属 Runtime：外部给就用，不给就自己造一本。这是"全局只有一本账"的
	// 前提——任何创建 Runtime 的路径都自带账本，不需要调用方再配一次。
	coordinator := deps.Coordinator
	if coordinator == nil {
		coordinator = sessionpkg.NewRuntimeSessionCoordinator(sessionpkg.DefaultCoordinatorOptions())
	}
	// manager 归属：外部给就用，不给就自建。
	// Python: plane.py:155 `SingleTenantStrategy(agent_manager or AgentManager())`
	// —— 上游 AgentRuntime() 无参调用时正是在这里 new 出 AgentManager，因此
	// process_cli/client.py:80 的 `AgentRuntime()` 一行即可用。
	// Go 侧能这样写依赖两点：server/runtime 已不 import swarm/runtime（无环），
	// 且 AgentManager 实现 AgentManagerPort（接口定义在本包）。
	manager := deps.Manager
	if manager == nil {
		manager = srvruntime.NewAgentManager()
	}
	return &AgentRuntime{
		manager:                manager,
		planController:         controller,
		onControlEvent:         deps.OnControlEvent,
		prepare:                deps.Prepare,
		agentExecutionOwners:   make(map[agentExecutionOwnerKey]*RuntimeAgentExecution),
		coordinator:            coordinator,
		resetPlanSession:       deps.ResetPlanSession,
		iterManagers:           deps.IterManagers,
		resolveModelCapability: deps.ResolveModelCapability,
	}
}

// CleanupSession 释放一个 Runtime 会话占用的内存资源。
// Python: service.py:3024-3071 cleanup_session（keyword-only 参数）
//
// 四步顺序不得调整（短路顺序即语义边界）：
//  1. 若协调器仍持有该会话 → 关闭执行；超时则报错
//  2. 逐 manager 清会话运行时（request 非 nil 用该 request 解析的 manager，
//     否则扫 host 事件可达的池；单条失败只告警，spec D-2）
//  3. 解绑 owner（无论前面成败，Python:3066）
//  4. resetPlanState 为真时重置 plan（Python:3070）
//
// resetPlanState=false 支撑事务式 session.delete：Runtime 工作先排空，
// plan 状态保留到下游删除步骤全部提交后才重置，以便回滚。
func (r *AgentRuntime) CleanupSession(
	ctx context.Context, channelID, sessionID string, resetPlanState bool,
) (bool, error) {
	// Python:3037 if self._closed: raise RuntimeStateError
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return false, errors.New("runtime is already closed")
	}

	// Python:3052-3057 第 1 步：协调器仍持有该会话则关闭执行
	if r.ownsSession(sessionID) {
		result, err := r.coordinator.CloseSession(sessionID, nil, nil)
		if err != nil {
			return false, err
		}
		// Python:3056-3057 if result.timed_out: raise SessionCloseTimeoutError
		if len(result.TimedOut) > 0 {
			return false, &sessionCloseTimeoutError{
				SessionID: sessionID, TimedOut: result.TimedOut,
			}
		}
	}

	// Python:3058-3064 第 2 步：确定要清理的 manager 集合
	managers := r.managersForCleanup()
	cleaned := false
	for _, manager := range managers {
		cleaner, ok := manager.(sessionRuntimeCleaner)
		if !ok {
			continue
		}
		result, err := cleaner.CleanupSessionRuntime(ctx, channelID, sessionID)
		if err != nil {
			// 上游是聚合后统一 raise；Go 侧只告警（spec D-2）：
			// 删除事务其余步骤不该因一次清理失败整体回滚。
			logger.Warn(logComponent).Err(err).
				Str("channel_id", channelID).Str("session_id", sessionID).
				Msg("session runtime cleanup failed")
			continue
		}
		cleaned = result || cleaned
	}

	// Python:3066-3069 第 3 步：无条件解绑 owner（★R-4 清端）
	r.forgetAgentExecutionOwner(channelID, sessionID)

	// Python:3070-3071 第 4 步：按需重置 plan
	if resetPlanState && r.resetPlanSession != nil {
		r.resetPlanSession(sessionID)
	}
	return cleaned, nil
}

// PlanController 返回运行时持有的计划模式控制器。// Python: AgentRuntime.plan_controller
func (r *AgentRuntime) PlanController() *PlanModeController { return r.planController }

// Start 幂等启动（不接管 Runner/checkpointer 生命周期）。
// Python: AgentRuntime.start（service.py:655）
func (r *AgentRuntime) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("agent runtime is closed")
	}
	if r.started {
		logger.Warn(logComponent).Msg("agent runtime already started")
		return nil
	}
	r.started = true
	return nil
}

// Close 幂等关闭。
// Python: AgentRuntime.close（service.py:3442-3470）
func (r *AgentRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.started = false

	// Python:3545-3546 asyncio.Lock 下 _agent_execution_owners.clear()
	// 关闭时整表清空，避免声明式定义跨 Runtime 生命周期残留。
	r.agentExecutionOwnerMu.Lock()
	clear(r.agentExecutionOwners)
	r.agentExecutionOwnerMu.Unlock()
	return nil
}

// NormalizeRequest 归一化请求（保证 Params 是合法 JSON map）。
// Python: AgentRuntime.normalize_request（service.py:3790）
func NormalizeRequest(req *schema.AgentRequest) *schema.AgentRequest {
	if req == nil {
		return &schema.AgentRequest{}
	}
	writeRequestParams(req, parseRequestParams(req))
	return req
}

// Invoke 执行非流式 chat turn。
// Python: AgentRuntime.invoke → _invoke_started（service.py:1694/1786）
func (r *AgentRuntime) Invoke(ctx context.Context, req *schema.AgentRequest, opts ...StreamOption) ([]*RuntimeEvent, error) {
	// Python: service.py:1707 await self.start()（幂等）
	if err := r.Start(); err != nil {
		return nil, err
	}
	// Python: service.py:1712 _agent_execution 一路下传到 prepare_chat_turn，
	// 再拆成 agent_definition + agent_definition_fingerprint（:1279-1283）
	agentDefinition, agentDefinitionFingerprint, defErr := splitAgentExecution(opts)
	if defErr != nil {
		return nil, defErr
	}
	req = NormalizeRequest(req)
	// 会话补充输入的准入段（同 Stream）。
	if r.isSessionInputRequest(req) {
		if err := r.validateSessionInputRequest(req); err != nil {
			return nil, err
		}
	}
	_, subMode, agent, err := r.prepareTurn(ctx, req, agentDefinition, agentDefinitionFingerprint)
	if err != nil {
		return nil, err
	}
	// 执行前：同步 plan 状态（Python: service.py:1860 `if not readonly_goal_get`）
	if r.shouldSyncPlanState(req) {
		r.ensurePlanState(ctx, req, subMode, agent)
	}

	// Python: service.py:1873 if uses_session_runtime(request): execute_message else: process_message
	var resp *schema.AgentResponse
	var execErr error
	if UsesSessionRuntime(req, false) {
		resp, execErr = agent.ExecuteMessage(ctx, req)
	} else {
		resp, execErr = agent.ProcessMessage(ctx, req)
	}

	// 执行后检查工具触发的 plan 退出（对齐 Python check_post_process_exit）
	if r.shouldCheckPlanExit(req) {
		r.checkPlanExit(ctx, req, subMode, agent)
	}
	if execErr != nil {
		return nil, execErr
	}
	return []*RuntimeEvent{FromAgentResponse(resp)}, nil
}

// Stream 执行流式 chat turn；onEvent 返回 false 时停止消费。
// Python: AgentRuntime.stream → _stream_started（service.py:2183/2482）
func (r *AgentRuntime) Stream(
	ctx context.Context,
	req *schema.AgentRequest,
	onEvent func(*RuntimeEvent) bool,
	opts ...StreamOption,
) error {
	// Python: service.py:2194 await self.start()（幂等）
	if err := r.Start(); err != nil {
		return err
	}
	// Python: service.py:2371 _agent_execution 下传；:1279-1283 拆成 definition + fingerprint
	agentDefinition, agentDefinitionFingerprint, defErr := splitAgentExecution(opts)
	if defErr != nil {
		return defErr
	}
	req = NormalizeRequest(req)
	// 会话补充输入的准入段（Python: service.py:2232-2249）。
	// 必须在 prepareTurn 之前：跨会话消息在 plan/goal 活跃时要被拒收排队，
	// 不能先建 agent 再拒。
	if r.isSessionInputRequest(req) {
		if err := r.validateSessionInputRequest(req); err != nil {
			return err
		}
	}
	_, subMode, agent, err := r.prepareTurn(ctx, req, agentDefinition, agentDefinitionFingerprint)
	if err != nil {
		return err
	}
	// 执行前：同步 plan 状态（Python: service.py:1860 `if not readonly_goal_get`）
	if r.shouldSyncPlanState(req) {
		r.ensurePlanState(ctx, req, subMode, agent)
	}
	// agent 就绪回调：plan 同步之后、启动 stream 之前。
	// Python: service.py:2587-2590 —— `if on_agent_ready is not None: await on_agent_ready(agent)`
	// 位置不得前移（上游在 ensure_state 之后）也不得后移（必须在 process_message_stream 之前）。
	if readyErr := applyStreamOptions(opts).notifyAgentReady(ctx, agent); readyErr != nil {
		if r.shouldCheckPlanExit(req) {
			r.checkPlanExit(ctx, req, subMode, agent)
		}
		return readyErr
	}

	var ch <-chan *schema.AgentResponseChunk
	var streamErr error
	if UsesSessionRuntime(req, false) {
		ch, streamErr = agent.ExecuteMessageStream(ctx, req)
	} else {
		ch, streamErr = agent.ProcessMessageStream(ctx, req)
	}
	if streamErr != nil {
		if r.shouldCheckPlanExit(req) {
			r.checkPlanExit(ctx, req, subMode, agent)
		}
		return streamErr
	}
	for chunk := range ch {
		if onEvent != nil && !onEvent(FromAgentResponseChunk(chunk)) {
			break
		}
	}
	// 流结束：退出事件在 channel 关闭前发出（对齐 Python generator finally 里的 yield）
	if r.shouldCheckPlanExit(req) {
		r.checkPlanExit(ctx, req, subMode, agent)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// notifyAgentReady 触发 agent 就绪回调；未设置时返回 nil。
//
// Python: service.py:2587-2590
//
//	if on_agent_ready is not None:
//	    ready_result = on_agent_ready(agent)
//	    if inspect.isawaitable(ready_result):
//	        await ready_result
//
// 上游用 inspect.isawaitable 兼容同步/异步回调；Go 回调签名自带 ctx 且返回 error，
// 天然是同步阻塞语义，无需再分辨可等待性。
func (o *streamOptions) notifyAgentReady(
	ctx context.Context, agent ChatAgentPort,
) error {
	if o == nil || o.onAgentReady == nil {
		return nil
	}
	// DeepAgentInterface 由门面实现自身满足；agent 是 ChatAgentPort 窄接口，
	// 其实现（UapClawSwarm）同时满足 DeepAgentInterface（D-8 的收窄依据）。
	// 上游 on_agent_ready 的实参恒为门面（agent_manager.py:2060 返回 JiuWenSwarm，
	// service.py:2588 原样传入），Go 收窄为 DeepAgentInterface（D-8）后门面必然满足。
	// 不满足只可能是测试替身或装配错误：记 Warn 并跳过，不阻断主流程
	//（上游也不会因回调参数类型问题中断 chat turn）。
	deepAgent, ok := agent.(harnessinterfaces.DeepAgentInterface)
	if !ok {
		logger.Warn(logComponent).
			Str("agent_type", fmt.Sprintf("%T", agent)).
			Msg("on_agent_ready: agent does not implement DeepAgentInterface, skipped")
		return nil
	}
	return o.onAgentReady(ctx, deepAgent)
}

// applyStreamOptions 折叠可选参数；nil option 跳过。
func applyStreamOptions(opts []StreamOption) *streamOptions {
	o := &streamOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

// splitAgentExecution 把 _agent_execution 拆成 (definition dict, fingerprint)。
// Python: service.py:1279-1283
//
//	prepare_kwargs.update(
//	    agent_definition=agent_execution.definition.to_dict(),
//	    agent_definition_fingerprint=agent_execution.fingerprint,
//	)
//
// 无 execution 时返回 (nil, "", nil)，普通聊天路径不受影响。
func splitAgentExecution(opts []StreamOption) (map[string]any, string, error) {
	exec := applyStreamOptions(opts).agentExecution
	if exec == nil {
		return nil, "", nil
	}
	// Python: agent_execution.definition.to_dict()
	definition := RuntimeAgentDefinitionToDict(exec.Definition)
	// Python: agent_execution.fingerprint
	fingerprint, err := RuntimeAgentExecutionFingerprint(exec)
	if err != nil {
		return nil, "", err
	}
	return definition, fingerprint, nil
}

// prepareTurn 选 agent（对齐 _prepare_chat_turn + prepare_chat_turn）。
func (r *AgentRuntime) prepareTurn(
	ctx context.Context,
	req *schema.AgentRequest,
	agentDefinition map[string]any,
	agentDefinitionFingerprint string,
) (mode, subMode string, agent ChatAgentPort, err error) {
	if r.prepare != nil {
		return r.prepare(ctx, req, req.ChannelID, agentDefinition, agentDefinitionFingerprint)
	}
	if r.manager == nil {
		return "", "", nil, errors.New("agent runtime manager is nil")
	}
	// Python: service.py:1848 `"sync_metadata": not readonly_goal_get` —— 只读的
	// command.goal get 不得兜底新建 session metadata（否则一次状态查询就凭空写出
	// metadata.json）。
	return PrepareChatTurn(ctx, r.manager, req, req.ChannelID,
		!isReadonlyGoalGetRequest(req), agentDefinition, agentDefinitionFingerprint)
}

// isSessionInputRequest 是否为会话补充输入请求。
// Python: service.py:3852 _is_session_input_request
//
// 三个条件缺一不可：chat 轮方法、params 里能解析出 input_mode（或遗留别名
// runtime_mode）、且不是 interrupt 恢复载荷。
func (r *AgentRuntime) isSessionInputRequest(req *schema.AgentRequest) bool {
	if req == nil || !isChatTurnMethod(req.ReqMethod) {
		return false
	}
	params := parseRequestParams(req)
	return ResolveSessionInputMode(params) != nil && !IsInterruptResumeRequest(params)
}

// teamSessionMessageID 跨会话消息的 message_id（非 dict 或缺 id 返回空串）。
// Python: service.py:3938 _team_session_message_id
func teamSessionMessageID(params map[string]any) string {
	message, ok := params[sessionMessageInternalKey].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := message["message_id"].(string)
	return strings.TrimSpace(id)
}

// sessionMessageRequiresQueue 跨会话消息是否必须排队。
// Python: service.py:513-518 session_message_requires_queue
//
// plan 活跃或 goal 活跃时都不得打断当前工作：goal 是会话级持久目标，
// 被跨会话消息打断会让生产者死掉、该会话后端冻结。
func (r *AgentRuntime) sessionMessageRequiresQueue(sessionID string) bool {
	if r.planController != nil {
		if _, ok := r.planController.ActiveSessions()[sessionID]; ok {
			return true
		}
	}
	return r.sessionMessageGoalBusy(sessionID)
}

// sessionMessageGoalBusy 会话是否持有活跃 goal。
// Python: service.py:532-549 _session_message_goal_busy
//
// [Go 偏差] 上游前半段遍历 session_coordinator 的执行快照，筛
// GOAL_STREAM/GOAL_ATTACH 且未终止的执行；Go 侧没有该快照结构，只保留
// manager.HasActiveGoal 这一路——它按 adapter 的 GoalRecord 判定，已覆盖
// 「goal 记录仍 ACTIVE」的情形。回收条件：session coordinator 落地后补齐。
func (r *AgentRuntime) sessionMessageGoalBusy(sessionID string) bool {
	checker, ok := r.manager.(goalActivityChecker)
	if !ok {
		return false
	}
	return checker.HasActiveGoal("", sessionID)
}

// requireCrossSessionInputAdmission 跨会话补充输入的准入校验。
// Python: service.py:520-530 _require_cross_session_input_admission
func (r *AgentRuntime) requireCrossSessionInputAdmission(req *schema.AgentRequest) error {
	params := parseRequestParams(req)
	if !isSessionMessageParams(params) {
		return nil
	}
	mode, _ := params["mode"].(string)
	if schema.IsPlanMode(mode) || r.sessionMessageRequiresQueue(requestSessionID(req)) {
		return &SessionInputQueueRequiredError{
			Message: "cross-session messages must queue while a plan or goal is active",
		}
	}
	return nil
}

// validateSessionInputRequest 会话补充输入的准入段。
// Python: service.py:2232-2249（_stream_under_tenant 的 session input 分支）
//
// [Go 偏差] 上游还有两项此处未复刻：
//   - background 投递必须走前台（Go 的 Stream/Invoke 无 background 形参）
//   - 会话归属与关闭态校验（Go 无 session_coordinator 快照）
//
// 回收条件同上：session coordinator 落地后补齐。
func (r *AgentRuntime) validateSessionInputRequest(req *schema.AgentRequest) error {
	params := parseRequestParams(req)
	// 团队会话消息走团队链路，不做单 agent 会话输入校验
	if requestTargetsTeam(req, params) && teamSessionMessageID(params) != "" {
		return nil
	}
	if err := ValidateSessionInput(params); err != nil {
		return err
	}
	if err := r.requireCrossSessionInputAdmission(req); err != nil {
		return err
	}
	if requestSessionID(req) == "" || !isSingleAgentSessionMode(params) {
		return errors.New("session input requires a supported single-agent Session")
	}
	return nil
}

// requestSessionID 取请求的会话标识（空串表示未指定）。
func requestSessionID(req *schema.AgentRequest) string {
	if req == nil || req.SessionID == nil {
		return ""
	}
	return strings.TrimSpace(*req.SessionID)
}

// isReadonlyGoalGetRequest 是否为只读的持续目标状态查询。
// Python: service.py:4321 _is_readonly_goal_get_request
//
// 只有 command.goal 且 action=get（含 action 缺省）为真。这类请求仍需真实执行
// （可能从 checkpointer / Session state 读出已有 GoalRecord），但三个副作用必须
// 关掉：不写 session metadata、不同步 plan 状态、不检查 plan 退出。
func isReadonlyGoalGetRequest(req *schema.AgentRequest) bool {
	if req == nil || req.ReqMethod != schema.ReqMethodCommandGoal {
		return false
	}
	// Python: str(params.get("action") or "get").strip().lower() == "get"
	action := strings.ToLower(strings.TrimSpace(stringParam(parseRequestParams(req), "action")))
	if action == "" {
		action = "get"
	}
	return action == "get"
}

// shouldSyncPlanState 本轮执行前是否同步 plan 状态。
// Python: service.py:1860 `if not readonly_goal_get:` → ensure_state
//
// 上游此处只判 not readonly_goal_get；is_team / resume / should_sync 三个条件
// 都在 EnsureState 内部（plan.py:196-204），Go 侧同样由控制器负责，不在此重复。
func (r *AgentRuntime) shouldSyncPlanState(req *schema.AgentRequest) bool {
	if req == nil || r.planController == nil {
		return false
	}
	return !isReadonlyGoalGetRequest(req)
}

// shouldCheckPlanExit 本轮结束后是否检查工具触发的 plan 退出。
// Python: service.py:1908 `if agent is not None and not stateless and not readonly_goal_get:`
//
// 上游该处的 agent is not None 在 Go 恒成立（agent 来自 prepareTurn）；stateless
// 方法在门面层已被 handleSkillsRequest / handleSkillDevRequest 拦下，到不了
// Runtime。session_id / is_team / is_plan 三个条件在 CheckPostProcessExit 内部
// （plan.go），不在此重复。
func (r *AgentRuntime) shouldCheckPlanExit(req *schema.AgentRequest) bool {
	if req == nil || r.planController == nil {
		return false
	}
	return !isReadonlyGoalGetRequest(req)
}

// ensurePlanState 同步本轮 plan 状态，回写 query/mode 并下发进入事件。
// Python: PlanModeController.ensure_state（service.py:1861）——上游直接改写
// request.params，Go 由控制器返回结果后在此回写，保证 BuildInputs 之前生效。
func (r *AgentRuntime) ensurePlanState(ctx context.Context, req *schema.AgentRequest, subMode string, agent ChatAgentPort) {
	res, err := r.planController.EnsureState(ctx, planRequestOf(req, subMode), agent.PlanStateAccessor())
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("ensure plan state failed")
		return
	}
	ApplyPlanResult(req, res)
	r.emitControlEvents(req, res.Events)
}

// checkPlanExit 检测工具触发的 plan 退出并下发事件。
// Python: PlanModeController.check_post_process_exit（service.py:1911）——
// 首步把 canonical mode 回写 params，再判定是否退出。
func (r *AgentRuntime) checkPlanExit(ctx context.Context, req *schema.AgentRequest, subMode string, agent ChatAgentPort) {
	ApplyCanonicalMode(req)
	events, err := r.planController.CheckPostProcessExit(ctx, planRequestOf(req, subMode), agent.PlanStateAccessor())
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("check plan post process failed")
		return
	}
	r.emitControlEvents(req, events)
}

// emitControlEvents 把控制事件交给回调；无回调时记 Debug。
// Python: AgentRuntime._emit_control_events（service.py:4405，handler=None 分支）
func (r *AgentRuntime) emitControlEvents(req *schema.AgentRequest, events []ControlEvent) {
	for _, ce := range events {
		ev := Control(req, map[string]any{"event_type": ce.EventType, "mode": ce.Mode})
		if r.onControlEvent != nil {
			r.onControlEvent(ev)
			continue
		}
		logger.Debug(logComponent).
			Str("event_type", ce.EventType).
			Str("mode", ce.Mode).
			Msg("control event without handler")
	}
}

// planRequestOf 把请求投影为控制器入参。
func planRequestOf(req *schema.AgentRequest, subMode string) PlanRequest {
	params := parseRequestParams(req)
	resolved := ResolveRequestRuntimeModeFromRequest(req)
	sid := ""
	if req != nil && req.SessionID != nil {
		sid = *req.SessionID
	}
	query, _ := params["query"].(string)
	entry, _ := params[PlanEntrySourceKey].(string)
	prev, _ := params[PreviousSessionModeKey].(string)
	reqMethod := ""
	if req != nil {
		reqMethod = req.ReqMethod.String()
	}
	return PlanRequest{
		SessionID:           sid,
		Mode:                resolved.CanonicalMode,
		SubMode:             subMode,
		ReqMethod:           reqMethod,
		EntrySource:         entry,
		IsInterruptResume:   params["resume"] == true,
		PreviousSessionMode: prev,
		Query:               query,
	}
}

// ──────────────────────────── owner 表（R-4） ────────────────────────────

// agentExecutionOwnerKeyOf 计算 owner 表键；session 为空时 ok=false。
// Python: service.py:1584-1591 _agent_execution_owner_key（staticmethod）
//
// 与上游逐条一致：
//   - session_id = str(request.session_id or "").strip()；空则返回 None
//   - channel_id = str(request.channel_id or "default").strip().lower() or "default"
func (r *AgentRuntime) agentExecutionOwnerKeyOf(req *schema.AgentRequest) (agentExecutionOwnerKey, bool) {
	sessionID := ""
	if req != nil && req.SessionID != nil {
		sessionID = strings.TrimSpace(*req.SessionID)
	}
	if sessionID == "" {
		return agentExecutionOwnerKey{}, false
	}
	channelID := ""
	if req != nil {
		channelID = strings.ToLower(strings.TrimSpace(req.ChannelID))
	}
	if channelID == "" {
		channelID = "default"
	}
	return agentExecutionOwnerKey{ChannelID: channelID, SessionID: sessionID}, true
}

// claimAgentExecutionOwner 把一个声明式 root Agent 钉到一个活跃会话上。
// Python: service.py:1593-1618 _claim_agent_execution_owner
//
// 语义：已有条目且指纹不同 → SESSION_CONFLICT；指纹相同 → 幂等返回；
// 无条目 → 写入。
func (r *AgentRuntime) claimAgentExecutionOwner(
	req *schema.AgentRequest, execution *RuntimeAgentExecution,
) error {
	// Python:1598-1600 无键直接返回
	key, ok := r.agentExecutionOwnerKeyOf(req)
	if !ok {
		return nil
	}
	if execution == nil {
		return nil
	}
	fingerprint, err := RuntimeAgentExecutionFingerprint(execution)
	if err != nil {
		return err
	}

	// Python:1607 async with self._agent_execution_owner_lock
	r.agentExecutionOwnerMu.Lock()
	defer r.agentExecutionOwnerMu.Unlock()

	// Python:1608-1609 current = self._agent_execution_owners.get(key)
	current, exists := r.agentExecutionOwners[key]
	if exists {
		currentFingerprint, err := RuntimeAgentExecutionFingerprint(current)
		if err != nil {
			return err
		}
		// Python:1609-1615 指纹不同 → SESSION_CONFLICT（field="agent"）
		if currentFingerprint != fingerprint {
			return &agentDefinitionError{
				sentinel: ErrAgentExecutionSessionConflict,
				field:    strPtr("agent"),
				message: "session is already bound to another Agent definition in this " +
					"Runtime lifecycle",
			}
		}
		// Python:1616-1617 指纹相同 → 幂等返回
		return nil
	}
	// Python:1618 self._agent_execution_owners[key] = execution
	r.agentExecutionOwners[key] = execution
	return nil
}

// agentExecutionOwner 查 owner 表；无条目返回 nil。
// Python: service.py:1620-1625 _agent_execution_owner
func (r *AgentRuntime) agentExecutionOwner(req *schema.AgentRequest) *RuntimeAgentExecution {
	key, ok := r.agentExecutionOwnerKeyOf(req)
	if !ok {
		return nil
	}
	r.agentExecutionOwnerMu.Lock()
	defer r.agentExecutionOwnerMu.Unlock()
	return r.agentExecutionOwners[key]
}

// forgetAgentExecutionOwner 解绑某会话的 owner。
// Python: service.py:1627-1640 _forget_agent_execution_owner（keyword-only 参数）
//
// 注意：上游此方法**独立构造键**（不复用 _agent_execution_owner_key），
// 键为 (归一化 channel_id, 去空白 session_id)，session 为空则直接返回。
func (r *AgentRuntime) forgetAgentExecutionOwner(channelID, sessionID string) {
	// Python:1635-1638 key 构造
	channel := strings.ToLower(strings.TrimSpace(channelID))
	if channel == "" {
		channel = "default"
	}
	session := strings.TrimSpace(sessionID)
	// Python:1638-1639 if not key[1]: return
	if session == "" {
		return
	}
	r.agentExecutionOwnerMu.Lock()
	defer r.agentExecutionOwnerMu.Unlock()
	// Python:1640 self._agent_execution_owners.pop(key, None)
	delete(r.agentExecutionOwners, agentExecutionOwnerKey{ChannelID: channel, SessionID: session})
}

// hasAgentExecutionOwner 判断某会话是否已有 owner（测试与门面只读探针用）。
// Python: service.py:1620-1625 的等价只读查询
func (r *AgentRuntime) hasAgentExecutionOwner(channelID, sessionID string) bool {
	channel := strings.ToLower(strings.TrimSpace(channelID))
	if channel == "" {
		channel = "default"
	}
	session := strings.TrimSpace(sessionID)
	if session == "" {
		return false
	}
	r.agentExecutionOwnerMu.Lock()
	defer r.agentExecutionOwnerMu.Unlock()
	_, exists := r.agentExecutionOwners[agentExecutionOwnerKey{ChannelID: channel, SessionID: session}]
	return exists
}

// ──────────────────────────── 清理辅助 ────────────────────────────

// sessionCloseTimeoutError 协调器关闭会话超时。
// Python: service.py:3056-3057 raise SessionCloseTimeoutError(session_id, result.timed_out)
type sessionCloseTimeoutError struct {
	// SessionID 会话标识
	SessionID string
	// TimedOut 超时未停的执行标识
	TimedOut []string
}

// Error 实现 error 接口。
func (e *sessionCloseTimeoutError) Error() string {
	return "session runtime executions have not stopped: " + e.SessionID
}

// ownsSession 协调器是否持有该会话的活跃代次。
// Python: service.py:1235-1242 _owns_session
//
// snapshot_session(session_id) if session_id else None；
// 仅当快照存在且 state 不为 CLOSED 时为真。
func (r *AgentRuntime) ownsSession(sessionID string) bool {
	if r.coordinator == nil || sessionID == "" {
		return false
	}
	snapshot := r.coordinator.SnapshotSession(sessionID)
	if snapshot == nil {
		return false
	}
	return snapshot.State != sessionpkg.RuntimeSessionStateClosed
}

// managersForCleanup 返回清理要遍历的 manager 集合。
// Python: service.py:3058-3064
//
// 上游分支：request 非 nil → [resolve_manager(request)]；
// 否则 → iter_managers_for_host_event() or [self.agent_manager]。
// Go 侧无 per-request resolve_manager（清理不接收 request），统一走
// iterManagers()，未注入时退化为 [manager]（等价于上游 or 分支）。
func (r *AgentRuntime) managersForCleanup() []AgentManagerPort {
	if r.iterManagers != nil {
		if managers := r.iterManagers(); len(managers) > 0 {
			return managers
		}
	}
	if r.manager == nil {
		return nil
	}
	return []AgentManagerPort{r.manager}
}

// ──────────────────────────── 声明式执行（R-4 写端） ────────────────────────────

// requireOwnedSingleAgentSession 在新 SDK 调用可能接管外部会话前 fail-closed。
// Python: service.py:1584 前的 _require_owned_single_agent_session
//
// 上游步骤：
//  1. 无 session_id → 直接放行
//  2. 协调器有快照且未关闭 → 渠道必须一致，否则 NOT_FOUND
//  3. 目录能读到该会话 → 放行
//  4. 协调器有活跃快照（刚分配、尚无 metadata）且 describe_session 为空 → 放行
//  5. 其余（缺失 / 外部渠道 / 非单 Agent）共用 NOT_FOUND
//
// D-5 已于 20.2（R-5）回收：DescribeSession 落地，第 4 步按上游原序判定，
// 不再退化为「协调器活跃即放行」。
func (r *AgentRuntime) requireOwnedSingleAgentSession(req *schema.AgentRequest) error {
	// 第 1 步：无 session_id 直接放行
	sessionID := requestSessionIDValue(req)
	if sessionID == "" {
		return nil
	}

	// 第 2 步：协调器活跃时校验渠道归属
	active := r.sessionSnapshot(sessionID)
	if active != nil && active.State != sessionpkg.RuntimeSessionStateClosed {
		requestedChannel := normalizedChannelOrDefault(requestChannelValue(req))
		activeChannel := normalizedChannelOrDefault(active.ChannelID)
		if activeChannel != requestedChannel {
			return ErrSessionNotFound
		}
	}

	// 第 3 步：目录能读到即放行
	owned, ownedErr := getSessionForAdmission(SessionGetInput{
		ChannelID: requestChannelValue(req),
		SessionID: sessionID,
	})
	if ownedErr != nil {
		// 渠道缺失视为 fail-closed 的 NOT_FOUND（上游共用一条响应）
		if errors.Is(ownedErr, ErrSessionChannelRequired) {
			requested := normalizedChannelOrDefault(requestChannelValue(req))
			owned, ownedErr = getSessionForAdmission(SessionGetInput{
				ChannelID: requested, SessionID: sessionID,
			})
		}
		if ownedErr != nil {
			return ErrSessionNotFound
		}
	}
	if owned != nil {
		return nil
	}

	// 第 4 步：协调器有活跃快照（刚分配、尚无 metadata）且 describe_session 为空 → 放行
	// Python: :860-869
	//   if active is not None and active.state is not CLOSED:
	//       descriptor = await self.describe_session(session_id=session_id)
	//       if descriptor is None: return
	// D-5 已回收（20.2 R-5）：describe_session 落地后按上游原序判定，
	// 不再退化为「协调器活跃即放行」。
	if active != nil && active.State != sessionpkg.RuntimeSessionStateClosed {
		descriptor, dErr := r.DescribeSession(sessionID)
		if dErr != nil {
			return dErr
		}
		if descriptor == nil {
			return nil
		}
	}

	// 第 5 步：缺失 / 外部渠道 / 非单 Agent 共用 NOT_FOUND
	return ErrSessionNotFound
}

// InvokeAgent 通过既有 Runtime 链执行一个声明式 root Agent。
// Python: service.py:1642-1663 invoke_agent
func (r *AgentRuntime) InvokeAgent(
	ctx context.Context, req *schema.AgentRequest, definition any, opts ...StreamOption,
) ([]*RuntimeEvent, error) {
	// Python:1648 await self.start()
	if err := r.Start(); err != nil {
		return nil, err
	}
	// Python:1649 await self._require_owned_single_agent_session(request)
	if err := r.requireOwnedSingleAgentSession(req); err != nil {
		return nil, err
	}
	// Python:1652-1653 params 与 mode 解析
	params := parseRequestParams(req)
	var requestedMode any
	if params != nil {
		requestedMode = params["mode"]
	}
	mode, err := ResolveModeCapability(requestedMode)
	if err != nil {
		// Python:1654 resolve_mode_capability 抛 ModeCatalogError
		return nil, err
	}
	// Python:1655 execution = self.validate_agent_definition(definition, mode=mode)
	execution, err := r.validateAgentDefinition(definition, mode.Mode)
	if err != nil {
		return nil, err
	}
	// Python:1656 bound_request = self._bind_agent_execution_request(request, execution)
	if _, err := r.bindAgentExecutionRequest(req, execution); err != nil {
		return nil, err
	}
	// Python:1657 await self._claim_agent_execution_owner(request, execution) ★R-4 写端
	if err := r.claimAgentExecutionOwner(req, execution); err != nil {
		return nil, err
	}
	// Python:1658-1663 invoke(bound_request, ..., _agent_execution=execution)
	return r.invokeWithExecution(ctx, req, execution)
}

// StreamAgent 通过既有 Runtime 链流式执行一个声明式 root Agent。
// Python: service.py:1665-1697 stream_agent
func (r *AgentRuntime) StreamAgent(
	ctx context.Context, req *schema.AgentRequest, definition any,
	onEvent func(*RuntimeEvent) bool, opts ...StreamOption,
) error {
	// Python:1674 await self.start()
	if err := r.Start(); err != nil {
		return err
	}
	// Python:1675 await self._require_owned_single_agent_session(request)
	if err := r.requireOwnedSingleAgentSession(req); err != nil {
		return err
	}
	// Python:1678-1679 mode 解析
	params := parseRequestParams(req)
	var requestedMode any
	if params != nil {
		requestedMode = params["mode"]
	}
	mode, err := ResolveModeCapability(requestedMode)
	if err != nil {
		return err
	}
	// Python:1679 execution = self.validate_agent_definition(...)
	execution, err := r.validateAgentDefinition(definition, mode.Mode)
	if err != nil {
		return err
	}
	// Python:1680 bound_request = self._bind_agent_execution_request(...)
	if _, err := r.bindAgentExecutionRequest(req, execution); err != nil {
		return err
	}
	// Python:1681 await self._claim_agent_execution_owner(request, execution) ★R-4 写端
	if err := r.claimAgentExecutionOwner(req, execution); err != nil {
		return err
	}
	// Python:1682-1696 stream(bound_request, ..., _agent_execution=execution)
	return r.streamWithExecution(ctx, req, execution, onEvent)
}

// ──────────────────────────── 交互回答（R-4 读端） ────────────────────────────

// AnswerInteraction 通过既有 Agent 回答一个暂停的交互。
// Python: service.py:1981-1998 answer_interaction
func (r *AgentRuntime) AnswerInteraction(
	ctx context.Context, req *schema.AgentRequest,
) ([]*RuntimeEvent, error) {
	// Python:1990-1992 req_method 必须是 CHAT_ANSWER
	if req == nil || req.ReqMethod != schema.ReqMethodChatAnswer {
		return nil, errors.New("interaction answer must use ReqMethod.CHAT_ANSWER")
	}
	// Python:1995-1997 owner 存在才透传；为空是正常态（不报错）
	execution := r.agentExecutionOwner(req)
	return r.invokeWithExecution(ctx, req, execution)
}

// AnswerInteractionInput 通过一个类型化的输入回答任一交互协议。
// Python: service.py:2000-2028 answer_interaction_input
//
// 这是三个读端里最上层的一个：调用方只需给出 InteractionAnswerInput，
// 由它决定走 chat.answer（普通交互）还是 chat.send（续跑被中断的轮次），
// 并统一完成准入校验与 owner 透传。
//
// 返回 []*RuntimeEvent 而非流：续跑路径要收齐全部事件（上游 :2028
// `[event async for event in self.stream(...)]`）。
func (r *AgentRuntime) AnswerInteractionInput(
	ctx context.Context, answer *InteractionAnswerInput,
) ([]*RuntimeEvent, error) {
	// Python:2010-2011 类型守卫
	if answer == nil {
		return nil, errors.New("answer must be an InteractionAnswerInput")
	}
	// Python:2012 await self.start()
	if err := r.Start(); err != nil {
		return nil, err
	}
	// Python:2013 request = answer.to_agent_request()
	request, err := answer.ToAgentRequest()
	if err != nil {
		return nil, err
	}
	// Python:2014 await self._require_owned_single_agent_session(request)
	if err := r.requireOwnedSingleAgentSession(request); err != nil {
		return nil, err
	}
	// Python:2015-2020 非续跑 → 直接转发给 answer_interaction
	if !answer.ResumesInterruptedTurn() {
		return r.AnswerInteraction(ctx, request)
	}
	// Python:2021-2028 续跑 → 收集流式事件
	events := make([]*RuntimeEvent, 0)
	execution := r.agentExecutionOwner(request)
	if err := r.streamWithExecution(ctx, request, execution, func(event *RuntimeEvent) bool {
		events = append(events, event)
		return true
	}); err != nil {
		return nil, err
	}
	return events, nil
}

// StreamInteractionAnswer 以流式回答一个暂停的交互。
// Python: service.py:2030-2090 stream_interaction_answer
func (r *AgentRuntime) StreamInteractionAnswer(
	ctx context.Context, req *schema.AgentRequest,
	onEvent func(*RuntimeEvent) bool,
) error {
	qaProbe("StreamInteractionAnswer进入", "method", req.ReqMethod, "session", req.SessionID, "params", string(req.Params))
	// Python:2083 _agent_execution=self._agent_execution_owner(request)
	execution := r.agentExecutionOwner(req)
	return r.streamWithExecution(ctx, req, execution, onEvent)
}

// ──────────────────────────── 执行辅助 ────────────────────────────

// invokeWithExecution 带执行体的非流式调用。
// Python: service.py:1700 invoke(..., _agent_execution=...) → 透传到 :1712/:1772/:1781
func (r *AgentRuntime) invokeWithExecution(
	ctx context.Context, req *schema.AgentRequest, execution *RuntimeAgentExecution,
) ([]*RuntimeEvent, error) {
	// Python: service.py:1712 _agent_execution=execution 一路下传到 prepare_chat_turn
	return r.Invoke(ctx, req, WithAgentExecution(execution))
}

// streamWithExecution 带执行体的流式调用。
// Python: service.py:1690 stream(..., _agent_execution=...)
func (r *AgentRuntime) streamWithExecution(
	ctx context.Context, req *schema.AgentRequest,
	execution *RuntimeAgentExecution, onEvent func(*RuntimeEvent) bool,
) error {
	// Python: service.py:2371 stream(..., _agent_execution=execution)
	return r.Stream(ctx, req, onEvent, WithAgentExecution(execution))
}

// ──────────────────────────── 小型取值辅助 ────────────────────────────

// sessionSnapshot 取协调器快照；协调器缺失返回 nil。
// Python: service.py:1237-1241 self._session_coordinator.snapshot_session(session_id)
func (r *AgentRuntime) sessionSnapshot(sessionID string) *sessionpkg.RuntimeSessionSnapshot {
	if r.coordinator == nil || sessionID == "" {
		return nil
	}
	return r.coordinator.SnapshotSession(sessionID)
}

// requestSessionIDValue 取请求会话标识（去空白）。
// Python: service.py:1588 str(request.session_id or "").strip()
func requestSessionIDValue(req *schema.AgentRequest) string {
	if req == nil || req.SessionID == nil {
		return ""
	}
	return strings.TrimSpace(*req.SessionID)
}

// requestChannelValue 取请求渠道标识。
// Python: request.channel_id or "default"
func requestChannelValue(req *schema.AgentRequest) string {
	if req == nil {
		return "default"
	}
	return req.ChannelID
}

// normalizedChannelOrDefault 归一化渠道，空则 default。
// Python: str(x or "default").strip().lower() or "default"
func normalizedChannelOrDefault(channelID string) string {
	normalized := strings.ToLower(strings.TrimSpace(channelID))
	if normalized == "" {
		return "default"
	}
	return normalized
}

// ──────────────────────────── 定义校验与请求绑定 ────────────────────────────

// validateAgentDefinition 校验一份自定义 root-Agent 定义。
// Python: service.py:691-714 validate_agent_definition
func (r *AgentRuntime) validateAgentDefinition(
	definition any, mode string,
) (*RuntimeAgentExecution, error) {
	// Python:706 execution = prepare_agent_execution(definition, mode=mode)
	execution, err := PrepareAgentExecution(definition, mode)
	if err != nil {
		return nil, err
	}
	// Python:707-714 配置了 model 则校验其存在性
	if execution.Definition.Model != nil && *execution.Definition.Model != "" {
		// 未装配解析器 → 报错（fail-closed）。此处不能静默放行：校验点仍在，
		// 但门必须真的关（见 ErrModelCatalogUnavailable 注释）。
		if r.resolveModelCapability == nil {
			return nil, ErrModelCatalogUnavailable
		}
		if _, err := r.resolveModelCapability(*execution.Definition.Model); err != nil {
			return nil, &agentDefinitionError{
				sentinel: ErrAgentDefinitionModelNotFound,
				field:    strPtr("model"),
				message:  "configured Agent model was not found",
			}
		}
	}
	return execution, nil
}

// bindAgentExecutionRequest 把已校验的定义绑到请求副本上。
// Python: service.py:1551-1582 _bind_agent_execution_request
//
// 步骤：req_method 必须是 chat.send → params 必须是对象 → 写 mode/work_mode →
// 定义带 model 且请求未显式给 model_name 时补齐 selection_key。
func (r *AgentRuntime) bindAgentExecutionRequest(
	req *schema.AgentRequest, execution *RuntimeAgentExecution,
) (*schema.AgentRequest, error) {
	// Python:1556-1562 只接受 chat.send
	if req == nil || req.ReqMethod != schema.ReqMethodChatSend {
		return nil, &agentDefinitionError{
			sentinel: ErrAgentDefinitionInvalidRequest,
			field:    strPtr("request"),
			message:  "custom Agent execution requires a chat.send request",
		}
	}
	// Python:1563-1568 params 必须是对象
	//
	// 注意：包内 parseRequestParams 对非对象输入会退回空 map 而不报错，
	// 因此这里必须显式判定"是 JSON 对象"，否则会把数组/标量静默当成对象。
	params, ok := requestParamsAsObject(req)
	if !ok {
		return nil, &agentDefinitionError{
			sentinel: ErrAgentDefinitionInvalidRequest,
			field:    strPtr("request.params"),
			message:  "custom Agent execution requires object request params",
		}
	}

	// Python:1571-1576 写 canonical mode 与 work_mode
	params["mode"] = execution.Mode
	if strings.HasPrefix(execution.Mode, "agent.code.") {
		params["work_mode"] = "code"
	} else {
		params["work_mode"] = "work"
	}

	// Python:1577-1581 未显式给 model_name 时用定义里的模型
	if execution.Definition.Model != nil && *execution.Definition.Model != "" {
		existing, _ := params["model_name"].(string)
		if strings.TrimSpace(existing) == "" {
			// 同 validateAgentDefinition：未装配解析器即报错，不静默丢弃 selection_key。
			if r.resolveModelCapability == nil {
				return nil, ErrModelCatalogUnavailable
			}
			descriptor, err := r.resolveModelCapability(*execution.Definition.Model)
			if err != nil {
				return nil, &agentDefinitionError{
					sentinel: ErrAgentDefinitionModelNotFound,
					field:    strPtr("model"),
					message:  "configured Agent model was not found",
				}
			}
			params["model_name"] = descriptor.SelectionKey
		}
	}

	writeRequestParams(req, params)
	return req, nil
}

// requestParamsAsObject 把请求 params 解析为 JSON 对象；非对象返回 ok=false。
// Python: service.py:1554 isinstance(request.params, dict)
//
// 与包内 parseRequestParams 的差别：后者对数组/标量/坏 JSON 一律退回空 map，
// 用于宽松读取；本函数用于上游明确要求"必须是对象"的判定点。
func requestParamsAsObject(req *schema.AgentRequest) (map[string]any, bool) {
	if req == nil || len(req.Params) == 0 {
		// 上游：params 缺省在 AgentRequest 上是 None → 不是 dict
		return nil, false
	}
	params := map[string]any{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, false
	}
	return params, true
}

// getSessionForAdmission 准入阶段的会话目录查询（可替换以便测试注入）。
// Python: service.py:1600-1608 self.get_session(SessionGetInput(...))
//
// 上游此步直接调用公开的 get_session；Go 侧抽出变量是为了让准入链路
// 的单元测试不必依赖真实会话目录（生产实现就是 GetSession）。
var getSessionForAdmission = GetSession

// ──────────────────────────── 目录门面方法 ────────────────────────────

// ListModelCapabilities 返回配置的模型目录与当前生效选择。
// Python: service.py:718-775 list_model_capabilities
//
// ⤵️ **待回填（模型路由子系统）**：上游此方法的 entries 来自
// `get_default_models()`，且中间有一道"用 build_model_from_entry 验证条目
// 可执行、剔除不可执行者"的过滤步骤（service.py:733-758）。该验证函数目前在
// Go 侧只存在于 DeepAdapter 的未导出方法上（deep_adapter.go:2583），而
// server/adapter 包已反向依赖 swarm/runtime，runtime 不能 import 它。
//
// 本批按用户决策登记待回填，**不保留空桩注入链**（规则 8）：此前有个
// `IterModelEntries func() []any` 注入点，生产零装配且类型被擦成 any，
// 属于"看着有线其实没接"。等模型路由子系统落地时，按真实类型重建：
// 入口应是 config.GetDefaultModels(cfg) 得到的 []map[string]any 经
// buildModelFromEntry 过滤后的结果。
//
// 当前返回空目录（未配置模型的上游等价态）；ResolveModelCapability 因此
// 必然解析失败，这是诚实的失败而非假成功。
func (r *AgentRuntime) ListModelCapabilities(currentSelection string) ModelCatalogResult {
	if err := r.requireStarted(); err != nil {
		return ModelCatalogResult{Models: []RuntimeModelDescriptor{}}
	}
	return ModelCatalogResult{Models: []RuntimeModelDescriptor{}}
}

// ResolveModelCapability 解析一个模型引用为描述符。
// Python: service.py:777-782 resolve_model_capability
func (r *AgentRuntime) ResolveModelCapability(requested string) (RuntimeModelDescriptor, error) {
	if err := r.requireStarted(); err != nil {
		return RuntimeModelDescriptor{}, err
	}
	catalog := r.ListModelCapabilities("")
	return ResolveModelSelection(catalog, requested)
}

// ListModeCapabilities 返回受支持的单 Agent 模式目录。
// Python: service.py:784-789 list_mode_capabilities
func (r *AgentRuntime) ListModeCapabilities() ModeCatalogResult {
	return ListModeCapabilities()
}

// ResolveModeCapability 解析一个模式。
// Python: service.py:791-796 resolve_mode_capability
func (r *AgentRuntime) ResolveModeCapability(requested any) (RuntimeModeDescriptor, error) {
	return ResolveModeCapability(requested)
}

// GetSession 读取一个拥有的单 Agent 会话。
// Python: service.py:798-803 get_session
func (r *AgentRuntime) GetSession(request SessionGetInput) (*SessionSummary, error) {
	if err := r.requireStarted(); err != nil {
		return nil, err
	}
	return GetSession(request)
}

// ListSessions 读取一页拥有的会话。
// Python: service.py:805-810 list_sessions
func (r *AgentRuntime) ListSessions(request SessionListInput) (SessionListResult, error) {
	if err := r.requireStarted(); err != nil {
		return SessionListResult{}, err
	}
	return ListSessions(request, false, false)
}

// DescribeSession 返回持久化会话的路由事实，不暴露存储内部细节。
// Python: service.py:917-955 describe_session
//
// 传输无关的查找口：本地 Runtime 客户端据此校验显式续聊目标，
// 无需 import 存储助手，也不必走 AgentServer 的 session.list handler。
func (r *AgentRuntime) DescribeSession(sessionID string) (*sessionpkg.SessionDescriptor, error) {
	// 1. Python: self._require_started()
	if err := r.requireStarted(); err != nil {
		return nil, err
	}
	// 2. Python: target = str(session_id or "").strip(); if not target: return None
	target := strings.TrimSpace(sessionID)
	if target == "" {
		return nil, nil
	}
	// 3. Python: resolve_session_dir(target, sessions_root=get_agent_sessions_dir())
	sessionDir, exists, err := common.ResolveSessionDir(target, session.GetSessionsDir())
	// 4. Python: if session_dir is None or not session_dir.is_dir(): return None
	//    注：ResolveSessionDir 的 bool 表示「目录存在」，第二值 false 即回退 None
	if err != nil || !exists || sessionDir == "" {
		return nil, nil
	}
	if info, statErr := os.Stat(sessionDir); statErr != nil || !info.IsDir() {
		return nil, nil
	}
	// 5. Python: metadata = get_session_metadata(target, cache_bust=True, enable_writeback=False)
	//    Go 的 GetSessionMetadata 是纯读路径，等价 enable_writeback=False（已有偏差登记）
	metadata := session.GetSessionMetadata(target)
	if metadata == nil {
		metadata = map[string]any{}
	}
	// 6. Python: SessionDescriptor 七字段，全部 strip()，work_mode 额外 lower()
	return &sessionpkg.SessionDescriptor{
		SessionID:  target,
		ChannelID:  strings.TrimSpace(metadataString(metadata, "channel_id")),
		Mode:       strings.TrimSpace(metadataString(metadata, "mode")),
		WorkMode:   strings.ToLower(strings.TrimSpace(metadataString(metadata, "work_mode"))),
		ProjectID:  strings.TrimSpace(metadataString(metadata, "project_id")),
		ProjectDir: strings.TrimSpace(metadataString(metadata, "project_dir")),
		UserID:     strings.TrimSpace(metadataString(metadata, "user_id")),
	}, nil
}

// metadataString 安全取 metadata 里的字符串字段。
// Python: str(metadata.get(key) or "").strip() 的前半段（strip 由调用方做）
func metadataString(metadata map[string]any, key string) string {
	if v, ok := metadata[key].(string); ok {
		return v
	}
	return ""
}

// sessionCreator AgentManager 的建会话能力（Python manager.create_session）。
// Python: jiuwenswarm/server/runtime/agent_manager.py AgentManager.create_session
type sessionCreator interface {
	// CreateSession 有显式 ID 时原样返回，否则按渠道规则分配
	CreateSession(channelID, sessionID string) (string, error)
}

// CreateOrResumeSession 分配一个新的 Runtime 会话 ID，或保留显式给出的持久化 ID。
// Python: service.py:874-909 create_or_resume_session
//
// 偏差 D-12：上游 resolve_manager(request) 按租户分槽，Go 侧只有
// AgentRuntimeDeps.Manager 单槽（无多租户池，IterManagers 仅用于 host 事件遍历），
// 故此处固定使用单槽。
func (r *AgentRuntime) CreateOrResumeSession(channelID, sessionID string) (string, error) {
	// 1. Python: self._require_started()
	if err := r.requireStarted(); err != nil {
		return "", err
	}
	requested := strings.TrimSpace(sessionID)

	// 2. Python: if requested: ...（校验 ID + 渠道归属）
	if requested != "" {
		// 2a. Python: if not is_valid_session_id(requested): raise ValueError
		if !common.IsValidSessionID(requested) {
			return "", errors.New("invalid session_id")
		}
		// 2b. Python: descriptor = await self.describe_session(session_id=requested)
		descriptor, err := r.DescribeSession(requested)
		if err != nil {
			return "", err
		}
		// 2c. Python: 渠道不一致 -> SessionCatalogError("session not found")
		//     不泄露「存在但属于别人」
		if descriptor != nil {
			requestedChannel := normalizedChannelOrDefault(channelID)
			persistedChannel := normalizedChannelOrDefault(descriptor.ChannelID)
			if persistedChannel != requestedChannel {
				return "", ErrSessionNotFound
			}
		}
	}

	// 3. Python: manager = await self.resolve_manager(request)（Go 单槽，见偏差 D-12）
	if r.manager == nil {
		return "", errors.New("agent runtime manager is nil")
	}

	// 4. Python: resolved = await manager.create_session(channel_id, session_id=requested or None)
	creator, ok := r.manager.(sessionCreator)
	if !ok {
		return "", errors.New("agent manager does not implement CreateSession")
	}
	resolved, err := creator.CreateSession(channelID, requested)
	if err != nil {
		return "", err
	}

	// 5. Python: await self._register_session(session_id=resolved, channel_id=channel_id)
	if _, err := r.RegisterSession(resolved, channelID, sessionpkg.SessionPersistencePolicyPersistent); err != nil {
		return "", err
	}
	return resolved, nil
}

// GetPermissionSnapshot 返回一致的只读权限快照。
// Python: service.py:812-820 get_permission_snapshot
func (r *AgentRuntime) GetPermissionSnapshot(
	request PermissionSnapshotInput,
) (PermissionSnapshotResult, error) {
	if err := r.requireStarted(); err != nil {
		return PermissionSnapshotResult{}, err
	}
	return ReadPermissionSnapshot(request)
}

// Coordinator 返回 Runtime 持有的会话执行协调器（未注入时为 nil）。
// Python: service.py:412 self._session_coordinator
func (r *AgentRuntime) Coordinator() SessionCoordinatorPort { return r.coordinator }

// AgentManager 返回 Runtime 持有的 agent 实例管理器。
// Python: service.py:476 self._tenant.get_default_manager()
//
// 门面与 AgentServer 不再各自持有 manager，一律经此取，消除多份持有。
func (r *AgentRuntime) AgentManager() AgentManagerPort { return r.manager }

// SetSessionProvisioner 注入会话开通器（server 层构造后调用一次）。
// Python: service.py:403-409 __init__ 里自造；Go 侧依赖方向相反（server → runtime），
// 故改为注入。必须在 Start() 之前调用。
func (r *AgentRuntime) SetSessionProvisioner(provisioner *RuntimeSessionProvisioner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provisioner = provisioner
}

// PrepareSessionFork 在本 Runtime 上准备一次传输无关的会话分叉。
// Python: service.py:1086-1103 Runtime.prepare_session_fork
func (r *AgentRuntime) PrepareSessionFork(
	ctx context.Context, input SessionForkInput,
) (*PreparedSessionProvision, error) {
	// Python: :1091-1094 async with self._lifecycle_lock: self._require_started()
	//                          self._session_provision_prepares += 1
	if err := r.requireStarted(); err != nil {
		return nil, err
	}
	// Python: :1099 prepared = await self._session_provisioner.prepare_session_fork(input)
	//         finally: if prepared is not None: self._pending_session_provisions.add(prepared)
	provisioner := r.provisionerFor()
	if provisioner == nil {
		return nil, ErrProvisionerUnavailable
	}
	return provisioner.PrepareSessionFork(ctx, input)
}

// PrepareSessionCreate 在本 Runtime 上准备一次传输无关的会话创建。
// Python: service.py:1106-1123 Runtime.prepare_session_create
func (r *AgentRuntime) PrepareSessionCreate(
	ctx context.Context, input SessionCreateInput,
) (*PreparedSessionProvision, error) {
	// Python: :1111-1115 lifecycle_lock + require_started + prepares += 1
	if err := r.requireStarted(); err != nil {
		return nil, err
	}
	provisioner := r.provisionerFor()
	if provisioner == nil {
		return nil, ErrProvisionerUnavailable
	}
	return provisioner.PrepareSessionCreate(ctx, input)
}

// PrepareSessionSwitch 在本 Runtime 上准备一次传输无关的会话切换。
// Python: service.py:1126-1143 Runtime.prepare_session_switch
func (r *AgentRuntime) PrepareSessionSwitch(
	ctx context.Context, input SessionSwitchInput,
) (*PreparedSessionProvision, error) {
	// Python: :1131-1135 lifecycle_lock + require_started + prepares += 1
	if err := r.requireStarted(); err != nil {
		return nil, err
	}
	provisioner := r.provisionerFor()
	if provisioner == nil {
		return nil, ErrProvisionerUnavailable
	}
	return provisioner.PrepareSessionSwitch(ctx, input)
}

// CommitSessionProvision 在 Runtime 关闭前提交一次已准备的会话操作。
// Python: service.py:1146-1180 Runtime.commit_session_provision
func (r *AgentRuntime) CommitSessionProvision(
	prepared *PreparedSessionProvision,
	timing SessionProvisionCommitTiming,
	commitContext *SessionProvisionCommitContext,
) (any, error) {
	// Python: :1152-1153 async with self._lifecycle_lock: self._require_started()
	if err := r.requireStarted(); err != nil {
		return nil, err
	}
	// Python: :1155-1162 result = await provisioner.commit_session_provision(...)
	provisioner := r.provisionerFor()
	if provisioner == nil {
		return nil, ErrProvisionerUnavailable
	}
	result, err := provisioner.CommitSessionProvision(prepared, timing, commitContext)
	// Python: :1163-1175 单 Agent 会话模式时登记会话
	if err == nil {
		if regErr := r.registerCommittedSession(result); regErr != nil {
			return result, regErr
		}
	}
	// Python: :1176-1177 finally: self._discard_finalized_session_provision(prepared)
	// ⤵️ 回填 20.5：_pending_session_provisions 集合与 _discard_finalized_session_provision
	// 尚未实现（Go 侧 provisioner 内部已按 owner token 管理租约）。
	return result, err
}

// AbortSessionProvision 在 Runtime 关闭前放弃一次已准备的会话操作。
// Python: service.py:1183-1194 Runtime.abort_session_provision
func (r *AgentRuntime) AbortSessionProvision(prepared *PreparedSessionProvision) error {
	// Python: :1188-1189 async with self._lifecycle_lock: self._require_started()
	if err := r.requireStarted(); err != nil {
		return err
	}
	// Python: :1191 await provisioner.abort_session_provision(prepared)
	provisioner := r.provisionerFor()
	if provisioner == nil {
		return ErrProvisionerUnavailable
	}
	return provisioner.AbortSessionProvision(prepared)
}

// DeleteSession 通过共享 Runtime 边界删除一个已持久化的会话。
// Python: service.py:3361-3376 Runtime.delete_session
func (r *AgentRuntime) DeleteSession(
	ctx context.Context, opts DeleteSessionOptions,
) (sessionpkg.SessionDeleteResult, error) {
	// Python: :3369-3370 if self._closed: raise RuntimeStateError
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return sessionpkg.SessionDeleteResult{}, errors.New("runtime is already closed")
	}
	// Python: :3371-3375 await self._session_provisioner.delete_session(...)
	provisioner := r.provisionerFor()
	if provisioner == nil {
		return sessionpkg.SessionDeleteResult{}, ErrProvisionerUnavailable
	}
	return provisioner.DeleteSession(ctx, opts)
}

// provisionerFor 取 provisioner；未注入时返回 nil。
// Python: 不存在该路径（__init__ 必造 provisioner）；Go 侧因注入式装配需要防御。
func (r *AgentRuntime) provisionerFor() *RuntimeSessionProvisioner {
	r.mu.Lock()
	provisioner := r.provisioner
	r.mu.Unlock()
	// 未注入时返回 nil，调用方必须判空（见 requireProvisioner）
	return provisioner
}

// registerCommittedSession 把已提交的单 Agent 会话登记进协调器。
// Python: service.py:1163-1175 commit_session_provision 里按结果类型取 mode/work_mode
//
//	then if self._is_single_agent_session_mode(mode, work_mode=work_mode):
//	         await self._register_session(session_id=..., channel_id=...)
func (r *AgentRuntime) registerCommittedSession(result any) error {
	sessionID, channelID, mode, workMode, ok := CommittedSessionIdentity(result)
	if !ok || mode == "" {
		return nil
	}
	// Python: _is_single_agent_session_mode(mode, work_mode=work_mode)
	params := map[string]any{"mode": mode}
	if workMode != "" {
		params["work_mode"] = workMode
	}
	if !isSingleAgentSessionMode(params) {
		return nil
	}
	// Python: await self._register_session(session_id=..., channel_id=...)
	if _, err := r.RegisterSession(sessionID, channelID, sessionpkg.SessionPersistencePolicyPersistent); err != nil {
		return err
	}
	return nil
}

// CommittedSessionIdentity 从提交结果里取 (sessionID, channelID, mode, workMode)。
// Python: service.py:1163-1172
//
//	if isinstance(result, SessionCreateResult): mode = result.canonical_mode; work_mode = result.work_mode
//	elif isinstance(result, SessionSwitchResult): mode = result.mode; work_mode = None
//	else: mode = None; work_mode = None
func CommittedSessionIdentity(result any) (string, string, string, string, bool) {
	switch typed := result.(type) {
	case SessionCreateResult:
		return typed.SessionID, typed.ChannelID, typed.CanonicalMode, typed.WorkMode, true
	case *SessionCreateResult:
		if typed == nil {
			return "", "", "", "", false
		}
		return typed.SessionID, typed.ChannelID, typed.CanonicalMode, typed.WorkMode, true
	case SessionSwitchResult:
		return typed.SessionID, typed.ChannelID, typed.Mode, "", true
	case *SessionSwitchResult:
		if typed == nil {
			return "", "", "", "", false
		}
		return typed.SessionID, typed.ChannelID, typed.Mode, "", true
	default:
		return "", "", "", "", false
	}
}

// requireStarted Runtime 必须已启动（对应上游 _require_started）。
// Python: service.py 各门面的 self._require_started()
func (r *AgentRuntime) requireStarted() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("agent runtime is already closed")
	}
	if !r.started {
		return errors.New("agent runtime is not started")
	}
	return nil
}

// ──────────────────────────── 执行账本承接（P4 归属重构） ────────────────────────────
//
// Python 里下列能力全部由 AgentRuntime 直接调用（service.py 的 self._session_coordinator
// / self._session_manager），门面（interface.py）0 处使用。Go 10.3.28d 把它们放在了
// 门面上，导致同一账本被多个 UapClawSwarm 各持一份。以下方法把这批调用收归
// Runtime，门面改为经注入的 Runtime 调用。

// RegisterSession 在协调器中登记/刷新一个会话代次。
// Python: service.py:1229 self._session_coordinator.register_session(...)
func (r *AgentRuntime) RegisterSession(
	sessionID, channelID string, policy sessionpkg.SessionPersistencePolicy,
) (sessionpkg.RuntimeSessionSnapshot, error) {
	if r.coordinator == nil {
		return sessionpkg.RuntimeSessionSnapshot{}, ErrCoordinatorUnavailable
	}
	return r.coordinator.RegisterSession(sessionID, channelID, policy)
}

// RunUnary 在协调器中执行一次非流式任务并等待结果。
// Python: service.py:1764 return await self._session_coordinator.run_unary(...)
func (r *AgentRuntime) RunUnary(
	ctx context.Context, sessionID, requestID string, kind sessionpkg.SessionWorkKind,
	task func(context.Context) (any, error), options sessionpkg.UnaryOptions,
) (any, error) {
	if r.coordinator == nil {
		return nil, ErrCoordinatorUnavailable
	}
	return r.coordinator.RunUnary(ctx, sessionID, requestID, kind, task, options)
}

// CancelExecution 取消某会话的执行。
// Python: service.py:1373 results.append(await self._session_coordinator.cancel_execution(...))
func (r *AgentRuntime) CancelExecution(
	sessionID string, options sessionpkg.CancelExecutionOptions,
) (sessionpkg.CancelExecutionResult, error) {
	if r.coordinator == nil {
		return sessionpkg.CancelExecutionResult{}, ErrCoordinatorUnavailable
	}
	return r.coordinator.CancelExecution(sessionID, options)
}

// ActiveExecutions 返回协调器中仍有未终态执行的会话数（dreaming 探针用）。
// Python: service.py:845/:1216 snapshot_session(...).executions 判定
func (r *AgentRuntime) ActiveExecutions() bool {
	if r.coordinator == nil {
		return false
	}
	for _, snapshot := range r.coordinator.SnapshotSessions() {
		for _, execution := range snapshot.Executions {
			if !execution.State.Terminal() {
				return true
			}
		}
	}
	return false
}

// CloseCoordinator 关闭协调器并返回超时会话。
// Python: AgentRuntime.close 内的协调器关闭
func (r *AgentRuntime) CloseCoordinator(waitTimeout *time.Duration, timedOut *[]string) error {
	if r.coordinator == nil {
		return nil
	}
	return r.coordinator.Close(waitTimeout, timedOut)
}

// QuiesceSession 把会话置 QUIESCING 并返回生命周期上下文。
// Python: service.py 内 self._session_coordinator.quiesce_session(...)
//
// 供 session.delete 事务先清场再删：置 QUIESCING 阻止新准入，
// 拿到 generation 后由调用方 CloseSession 逐条取消并等执行停下。
func (r *AgentRuntime) QuiesceSession(
	sessionID string, generation *int,
) (*sessionpkg.SessionLifecycleContext, error) {
	if r.coordinator == nil {
		return nil, ErrCoordinatorUnavailable
	}
	return r.coordinator.QuiesceSession(sessionID, generation)
}

// CloseSessionGeneration 按指定代次关闭某会话的执行并等待停止。
// Python: service.py 内 self._session_coordinator.close_session(session_id, generation=..., wait_timeout=...)
//
// 与 CleanupSession 第 1 步的区别：那里不限定代次（generation=nil），本方法由
// session.delete 事务传入 QuiesceSession 返回的代次，避免误关新代次。
func (r *AgentRuntime) CloseSessionGeneration(
	sessionID string, generation *int, waitTimeout *time.Duration,
) (sessionpkg.CloseSessionResult, error) {
	if r.coordinator == nil {
		return sessionpkg.CloseSessionResult{}, ErrCoordinatorUnavailable
	}
	return r.coordinator.CloseSession(sessionID, generation, waitTimeout)
}
