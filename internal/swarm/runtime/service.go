package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PrepareChatTurnFunc 选 agent 并返回 (mode, subMode, agent)，测试可注入。
type PrepareChatTurnFunc func(
	ctx context.Context,
	req *schema.AgentRequest,
	channelID string,
) (mode, subMode string, agent ChatAgentPort, err error)

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
}

// SessionCoordinatorPort 清理路径需要的协调器能力。
// Python: service.py:3052-3057 self._session_coordinator.close_session + :1235-1242 snapshot_session
//
// 窄接口而非具体类型：CleanupSession 只需关闭与快照两能力，避免把千行协调器
// 的完整方法面拖进 service.go 依赖面（与 goalActivityChecker 同款约定）。
type SessionCoordinatorPort interface {
	// CloseSession 关闭某会话的运行中执行
	CloseSession(sessionID string, generation *int, waitTimeout *time.Duration) (sessionpkg.CloseSessionResult, error)
	// SnapshotSession 取会话快照（不存在返回 nil）
	SnapshotSession(sessionID string) *sessionpkg.RuntimeSessionSnapshot
}

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
	// resetPlanSession 重置会话 plan 状态（可注入）
	// Python: service.py:3070 self._plan_controller.reset_session
	resetPlanSession func(sessionID string)
	// iterManagers 遍历 host 事件可达的 manager（可注入）
	// Python: service.py:3060-3064 iter_managers_for_host_event
	iterManagers func() []AgentManagerPort
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

// NewAgentRuntime 创建运行时。
// Python: AgentRuntime.__init__
func NewAgentRuntime(deps AgentRuntimeDeps) *AgentRuntime {
	controller := deps.PlanController
	if controller == nil {
		controller = NewPlanModeController()
	}
	return &AgentRuntime{
		manager:              deps.Manager,
		planController:       controller,
		onControlEvent:       deps.OnControlEvent,
		prepare:              deps.Prepare,
		agentExecutionOwners: make(map[agentExecutionOwnerKey]*RuntimeAgentExecution),
		coordinator:          deps.Coordinator,
		resetPlanSession:     deps.ResetPlanSession,
		iterManagers:         deps.IterManagers,
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
func (r *AgentRuntime) Invoke(ctx context.Context, req *schema.AgentRequest) ([]*RuntimeEvent, error) {
	req = NormalizeRequest(req)
	// 会话补充输入的准入段（同 Stream）。
	if r.isSessionInputRequest(req) {
		if err := r.validateSessionInputRequest(req); err != nil {
			return nil, err
		}
	}
	_, subMode, agent, err := r.prepareTurn(ctx, req)
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
) error {
	req = NormalizeRequest(req)
	// 会话补充输入的准入段（Python: service.py:2232-2249）。
	// 必须在 prepareTurn 之前：跨会话消息在 plan/goal 活跃时要被拒收排队，
	// 不能先建 agent 再拒。
	if r.isSessionInputRequest(req) {
		if err := r.validateSessionInputRequest(req); err != nil {
			return err
		}
	}
	_, subMode, agent, err := r.prepareTurn(ctx, req)
	if err != nil {
		return err
	}
	// 执行前：同步 plan 状态（Python: service.py:1860 `if not readonly_goal_get`）
	if r.shouldSyncPlanState(req) {
		r.ensurePlanState(ctx, req, subMode, agent)
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

// prepareTurn 选 agent（对齐 _prepare_chat_turn + prepare_chat_turn）。
func (r *AgentRuntime) prepareTurn(
	ctx context.Context,
	req *schema.AgentRequest,
) (mode, subMode string, agent ChatAgentPort, err error) {
	if r.prepare != nil {
		return r.prepare(ctx, req, req.ChannelID)
	}
	if r.manager == nil {
		return "", "", nil, errors.New("agent runtime manager is nil")
	}
	// Python: service.py:1848 `"sync_metadata": not readonly_goal_get` —— 只读的
	// command.goal get 不得兜底新建 session metadata（否则一次状态查询就凭空写出
	// metadata.json）。
	return PrepareChatTurn(ctx, r.manager, req, req.ChannelID, !isReadonlyGoalGetRequest(req))
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
