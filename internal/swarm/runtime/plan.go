package runtime

import (
	"context"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanState plan 状态快照（只取控制器关心的字段）。
type PlanState struct {
	// Mode 当前 plan 模式："plan" | "normal"
	Mode string
	// PlanSlug 活跃计划文件短名
	PlanSlug string
}

// PlanRequest 控制器需要的请求视图。
type PlanRequest struct {
	// SessionID 会话 id
	SessionID string
	// Mode 请求 mode（权威串）
	Mode string
	// SubMode 二级模式
	SubMode string
	// ReqMethod 请求方法（空串视为 chat）
	ReqMethod string
	// EntrySource plan_entry_source
	EntrySource string
	// IsInterruptResume 是否中断续答请求
	IsInterruptResume bool
	// PreviousSessionMode previous_session_mode 参数
	PreviousSessionMode string
	// Query 本轮用户输入（进入 plan 时会被前置提醒）
	Query string
}

// ControlEvent 控制事件（下发通道/前端）。
type ControlEvent struct {
	// EventType 事件类型
	EventType string
	// Mode 事件携带的权威模式
	Mode string
}

// PlanStateResult 一次状态同步的结果。
type PlanStateResult struct {
	// Entered 本轮是否进入了 plan
	Entered bool
	// Restored 是否从 plan 退回 normal
	Restored bool
	// Events 需要下发的控制事件
	Events []ControlEvent
	// Query 改写后的 query（未改写时等于入参）
	Query string
	// ModeOverride 需要回写 params.mode 的权威串（仅阻断进入分支非空）
	ModeOverride string
}

// PlanStateAccessor plan 状态读写入口（运行期由调用方传入，对齐上游
// ensure_state(request, mode, sub_mode, agent) 的 agent 参数）。
type PlanStateAccessor interface {
	// LoadPlanState 读取会话 plan 状态
	LoadPlanState(ctx context.Context, sessionID string) (PlanState, error)
	// SavePlanState 保存会话 plan 状态
	SavePlanState(ctx context.Context, sessionID string, st PlanState) error
	// SwitchPlanMode 切 plan 模式
	SwitchPlanMode(ctx context.Context, sessionID, mode string) error
	// CommitSession 提交会话
	CommitSession(ctx context.Context, sessionID string) error
}

// PlanModeController 计划模式状态机守门人。
//
// Python: jiuwenswarm/runtime/plan.py PlanModeController
type PlanModeController struct {
	// mu 保护记忆与锁表
	mu sync.Mutex
	// locks 会话级串行锁
	locks map[string]*sync.Mutex
	// active 处于 plan 的会话
	active map[string]struct{}
	// exited 刚显式退出的会话（阻断模式串自动拉回）
	exited map[string]struct{}
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentGateway
	// modePlan plan 状态值
	modePlan = "plan"
	// modeNormal normal 状态值
	modeNormal = "normal"
	// planActivationReminder 进入 plan 的约束提醒（逐字对齐上游 inject_activation_reminder）
	planActivationReminder = "\n\n<system-reminder>\n" +
		"Plan mode is active. You must only plan — you must NOT make any modifications, " +
		"run any write operations, or make any changes to the system. This constraint takes " +
		"priority over any other instructions.\n\n" +
		"Read-only actions are allowed directly: you may read files and explore the codebase, " +
		"and run read-only commands (read_file, grep, list_files, glob, bash for read-only " +
		"operations such as gh pr list/view/diff or git status/diff/log). Write operations and " +
		"non-read-only tools are blocked.\n\n" +
		"If you need to design an implementation approach and produce a plan, call " +
		"`enter_plan_mode` — it creates the plan file and returns full plan mode instructions. " +
		"This is not required as your first action; you may gather context with read-only tools " +
		"first. Do NOT proceed to implement anything until the user approves your plan via " +
		"`exit_plan_mode`.\n" +
		"</system-reminder>"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPlanModeController 创建控制器（不绑定状态存储）。
//
// Python: PlanModeController.__init__（无参）
func NewPlanModeController() *PlanModeController {
	return &PlanModeController{
		locks:  map[string]*sync.Mutex{},
		active: map[string]struct{}{},
		exited: map[string]struct{}{},
	}
}

// EnsureState 每轮执行前同步 plan 状态。
//
// Python: PlanModeController.ensure_state（runtime/plan.py:176-259）。
// 返回的 Query 是可能被前置提醒改写后的文本，调用方需回写请求。
func (c *PlanModeController) EnsureState(ctx context.Context, req PlanRequest, acc PlanStateAccessor) (PlanStateResult, error) {
	res := PlanStateResult{Query: req.Query}
	resolved := ResolveRequestRuntimeMode(req.Mode, req.SubMode)
	if resolved.IsTeam || req.SessionID == "" {
		return res, nil
	}
	if !isSingleAgentMode(resolved.CanonicalMode) {
		return res, nil
	}

	target := modeNormal
	if resolved.IsPlan {
		target = modePlan
	}
	// Python: 非 code-single 且退回 normal 且会话不可能持有状态 → 不写状态
	if !isCodeSingleMode(resolved.CanonicalMode, req.SubMode) && target == modeNormal &&
		!c.MayHoldState(req.SessionID, req.PreviousSessionMode) {
		return res, nil
	}
	if req.IsInterruptResume || !ShouldSync(req.ReqMethod) {
		return res, nil
	}

	lock := c.LockFor(req.SessionID)
	lock.Lock()
	defer lock.Unlock()

	st, err := acc.LoadPlanState(ctx, req.SessionID)
	if err != nil {
		return res, err
	}
	previous := st.Mode
	if previous == target {
		return res, nil
	}

	if target == modePlan {
		switch {
		case IsPlanEntrySource(req.EntrySource):
			c.clearExited(req.SessionID)
		case c.isExited(req.SessionID):
			// 退出记忆是一次性令牌：命中即消费
			c.clearExited(req.SessionID)
			return c.blockEntry(ctx, req.SessionID, resolved, res, acc)
		case st.PlanSlug != "":
			return c.blockEntry(ctx, req.SessionID, resolved, res, acc)
		}
	}

	if err := acc.SwitchPlanMode(ctx, req.SessionID, target); err != nil {
		return res, err
	}
	if previous == modePlan && target == modeNormal {
		c.clearActive(req.SessionID)
		res.Restored = true
		res.Events = append(res.Events, ControlEvent{EventType: PlanModeExitedEvent, Mode: resolved.NormalMode})
	}
	if target == modePlan {
		c.markActive(req.SessionID)
		res.Entered = true
		// Python: 进入后清空残留 plan_slug（阻断分支之外的兜底）
		if st.PlanSlug != "" {
			st.PlanSlug = ""
			if err := acc.SavePlanState(ctx, req.SessionID, st); err != nil {
				return res, err
			}
		}
	}
	if err := acc.CommitSession(ctx, req.SessionID); err != nil {
		return res, err
	}
	if res.Entered {
		res.Query = InjectActivationReminder(req.Query)
	}
	return res, nil
}

// MayHoldState 判断会话是否可能仍持有 plan 状态。
//
// Python: PlanModeController.may_hold_state
func (c *PlanModeController) MayHoldState(sessionID, previousSessionMode string) bool {
	if c.IsActive(sessionID) {
		return true
	}
	return schema.IsPlanMode(schema.CanonicalizeMode(previousSessionMode))
}

// IsExited 会话是否刚显式退出过 plan（退出记忆仍未被消费）。
func (c *PlanModeController) IsExited(sessionID string) bool { return c.isExited(sessionID) }

// CheckPostProcessExit 执行后检测工具触发的退出。
//
// Python: PlanModeController.check_post_process_exit
func (c *PlanModeController) CheckPostProcessExit(ctx context.Context, req PlanRequest, acc PlanStateAccessor) ([]ControlEvent, error) {
	resolved := ResolveRequestRuntimeMode(req.Mode, req.SubMode)
	if req.SessionID == "" || resolved.IsTeam || !resolved.IsPlan {
		return nil, nil
	}
	st, err := acc.LoadPlanState(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	if st.Mode != modeNormal {
		return nil, nil
	}
	c.markExited(req.SessionID)
	c.clearActive(req.SessionID)
	return []ControlEvent{{EventType: PlanModeExitedEvent, Mode: resolved.NormalMode}}, nil
}

// ResetSession 删除会话时清理记忆。
//
// Python: PlanModeController.reset_session
func (c *PlanModeController) ResetSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.exited, sessionID)
	delete(c.active, sessionID)
	delete(c.locks, sessionID)
}

// LockFor 返回会话级锁。
//
// Python: PlanModeController.lock_for
func (c *PlanModeController) LockFor(sessionID string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[sessionID]
	if !ok {
		l = &sync.Mutex{}
		c.locks[sessionID] = l
	}
	return l
}

// IsActive 会话是否处于 plan。
func (c *PlanModeController) IsActive(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.active[sessionID]
	return ok
}

// ActiveSessions 返回当前处于 plan 的会话集合快照。
//
// Python: jiuwenswarm/server/agent_ws_server.py get_plan_active_sessions()
func (c *PlanModeController) ActiveSessions() map[string]struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]struct{}, len(c.active))
	for sid := range c.active {
		out[sid] = struct{}{}
	}
	return out
}

// SessionMessageRequiresQueue 判断会话是否要求跨会话消息排队。
//
// 上游 runtime/plan.py:252-261 的步骤（Go 侧尚无 cross-session 消息链路，暂不实现）：
//  1. 读取该会话 plan 状态：state.plan_mode.mode == "plan" → is_plan = true
//  2. 读取该会话 goal 状态：goal 活跃 → is_goal_active = true
//  3. is_plan || is_goal_active → 跨会话消息必须排队，不得打断当前计划
//  4. 否则直接透传（不需要排队）
//
// TODO(跨会话消息链路): runtime 的 session_message 通道落地后按上述 4 步复刻。
func (c *PlanModeController) SessionMessageRequiresQueue(sessionID string) bool {
	logger.Warn(logComponent).
		Str("event_type", "plan_cross_session_queue_unimplemented").
		Str("session_id", sessionID).
		Msg("cross-session message queueing is not implemented; passing through")
	return false
}

// InjectActivationReminder 给本轮 query 前置 plan 约束提醒。
//
// Python: PlanModeController.inject_activation_reminder
func InjectActivationReminder(query string) string {
	return planActivationReminder + query
}

// ApplyPlanResult 把状态同步结果回写到请求参数（query 提醒与 mode 阻断）。
//
// Python: ensure_state 直接改写 request.params["query"]/["mode"]；Go 用返回值
// 传递，由调用方在此统一回写，保证 BuildInputs 之前 params 已是最终值。
func ApplyPlanResult(req *schema.AgentRequest, res PlanStateResult) {
	if req == nil {
		return
	}
	params := parseRequestParams(req)
	changed := false
	if res.ModeOverride != "" {
		params["mode"] = res.ModeOverride
		changed = true
	}
	if res.Entered && res.Query != "" {
		original, _ := params["query"].(string)
		params[PlanReminderOriginalQueryKey] = original
		params["query"] = res.Query
		changed = true
	}
	if changed {
		writeRequestParams(req, params)
	}
}

// ApplyCanonicalMode 把请求的权威 mode 串写回 params["mode"]。
//
// Python: check_post_process_exit 首步 `request.params["mode"] = resolved.canonical_mode`
func ApplyCanonicalMode(req *schema.AgentRequest) {
	if req == nil {
		return
	}
	params := parseRequestParams(req)
	params["mode"] = ResolveRequestRuntimeModeFromRequest(req).CanonicalMode
	writeRequestParams(req, params)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isSingleAgentMode 判断是否单 agent 模式（agent 族或 code 族；team 族不参与）。
func isSingleAgentMode(canonical string) bool {
	return schema.IsSingleAgentMode(canonical)
}

// blockEntry 阻断进入：清 slug、发退出事件。
//
// Python: ensure_state 的 blocked 分支——此时状态已是 normal，只需清 slug、
// 回写 params.mode 为退出模式并下发退出事件。
func (c *PlanModeController) blockEntry(ctx context.Context, sessionID string, resolved ResolvedMode, res PlanStateResult, acc PlanStateAccessor) (PlanStateResult, error) {
	st, err := acc.LoadPlanState(ctx, sessionID)
	if err != nil {
		return res, err
	}
	st.PlanSlug = ""
	if err := acc.SavePlanState(ctx, sessionID, st); err != nil {
		return res, err
	}
	if err := acc.CommitSession(ctx, sessionID); err != nil {
		return res, err
	}
	res.Entered = false
	res.Restored = false
	res.ModeOverride = resolved.NormalMode
	res.Events = append(res.Events, ControlEvent{EventType: PlanModeExitedEvent, Mode: resolved.NormalMode})
	return res, nil
}

// isCodeSingleMode 判断是否 code 单 agent 模式（不含 team）。
//
// Python: is_code_single = mode == "code" and sub_mode != "team"
func isCodeSingleMode(canonical, subMode string) bool {
	if strings.TrimSpace(subMode) == "team" {
		return false
	}
	return canonical == "code" || strings.HasPrefix(canonical, "agent.code.")
}

// markActive 记录会话进入 plan。
func (c *PlanModeController) markActive(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active[sessionID] = struct{}{}
}

// clearActive 移出 plan 记忆。
func (c *PlanModeController) clearActive(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.active, sessionID)
}

// markExited 记录会话刚退出。
func (c *PlanModeController) markExited(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exited[sessionID] = struct{}{}
}

// clearExited 清除退出记忆。
func (c *PlanModeController) clearExited(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.exited, sessionID)
}

// isExited 会话是否刚退出。
func (c *PlanModeController) isExited(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.exited[sessionID]
	return ok
}
