package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
		manager:        deps.Manager,
		planController: controller,
		onControlEvent: deps.OnControlEvent,
		prepare:        deps.Prepare,
	}
}

// PlanController 返回运行时持有的计划模式控制器。
// Python: AgentRuntime.plan_controller
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
// Python: AgentRuntime.close
func (r *AgentRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.started = false
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
