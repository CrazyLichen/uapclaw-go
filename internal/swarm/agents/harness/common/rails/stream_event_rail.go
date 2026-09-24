package rails

import (
	"context"
	"fmt"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JiuClawStreamEventRail 前端流事件发射 + 暂停/终止检查点 + 上下文修复。
// 对齐 Python: JiuClawStreamEventRail (priority=80)
//
// 暂停/终止状态由本 Rail 持有（而非 DeepAgent），使得 interface_deep
// 可以直接调用 rail.Pause() / rail.Resume() / rail.Abort() 而无需修改 DeepAgent。
//
// 流程位置：
//
//	DeepAgent.ReActLoop
//	  ├─ BeforeModelCall → 暂停检查 + 上下文修复
//	  ├─ AfterModelCall  → 发送 context.usage
//	  ├─ BeforeToolCall  → 暂停检查 + 发送 tool_call/tool_update + 跟踪 in-flight
//	  ├─ AfterToolCall   → 发送 tool_result + ask_user_question + todo.updated
//	  └─ OnModelException → 上下文修复
type JiuClawStreamEventRail struct {
	sainterfaces.BaseRail
	mu sync.Mutex

	// deepAgent 持有当前 DeepAgent 引用，用于获取 prompt language 等
	deepAgent sainterfaces.BaseAgent

	// ── 每会话暂停/终止状态 ──
	// Key: session_id (conversation_id)；共享适配器实例服务多个并发会话，
	// 标量状态会导致跨会话污染（会话 A 取消杀死会话 B）。

	// abortRequested 每会话终止标记，对齐 Python: _abort_requested
	abortRequested map[string]bool
	// pauseConds 每会话暂停条件变量，对齐 Python: _pause_events (asyncio.Event)
	pauseConds map[string]*pauseCond
	// conversationIDs 每会话 conversation_id，对齐 Python: _conversation_ids
	conversationIDs map[string]string
	// mainSessions 每会话主 Session 引用，对齐 Python: _main_sessions
	mainSessions map[string]sessioninterfaces.SessionFacade

	// ── 工具调用跟踪 ──
	// inflightToolCalls 跟踪执行中的工具调用，对齐 Python: _inflight_tool_calls
	inflightToolCalls map[string]inflightToolCallInfo
	// cancelledToolResults 取消的工具结果，对齐 Python: _cancelled_tool_results
	cancelledToolResults map[string][]map[string]any

	// todoTool 共享 TodoTool 实例，对齐 Python: _main_todo_tool
	todoTool todoToolLoader
}

// pauseCond 封装 sync.Mutex + sync.Cond 实现 asyncio.Event 语义
type pauseCond struct {
	mu     sync.Mutex
	cond   *sync.Cond
	paused bool // true = 已暂停（等待中）
}

// newPauseCond 创建暂停条件变量，初始为未暂停状态（对齐 Python: asyncio.Event 初始 set）
func newPauseCond() *pauseCond {
	pc := &pauseCond{paused: false}
	pc.cond = sync.NewCond(&pc.mu)
	return pc
}

// Wait 阻塞直到未暂停，对齐 Python: await asyncio.Event.wait()
func (pc *pauseCond) Wait() {
	pc.mu.Lock()
	for pc.paused {
		pc.cond.Wait()
	}
	pc.mu.Unlock()
}

// Pause 设置暂停状态，对齐 Python: asyncio.Event.clear()
func (pc *pauseCond) Pause() {
	pc.mu.Lock()
	pc.paused = true
	pc.mu.Unlock()
}

// Resume 恢复执行，对齐 Python: asyncio.Event.set()
func (pc *pauseCond) Resume() {
	pc.mu.Lock()
	pc.paused = false
	pc.mu.Unlock()
	pc.cond.Broadcast()
}

// inflightToolCallInfo 跟踪执行中的工具调用，对齐 Python: _inflight_tool_calls value
type inflightToolCallInfo struct {
	toolCall  *llmschema.ToolCall // 工具调用引用
	sessionID string             // 关联的会话 ID
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJiuClawStreamEventRail 创建 StreamEvent Rail 实例
func NewJiuClawStreamEventRail() *JiuClawStreamEventRail {
	return &JiuClawStreamEventRail{
		abortRequested:       make(map[string]bool),
		pauseConds:           make(map[string]*pauseCond),
		conversationIDs:      make(map[string]string),
		mainSessions:         make(map[string]sessioninterfaces.SessionFacade),
		inflightToolCalls:    make(map[string]inflightToolCallInfo),
		cancelledToolResults: make(map[string][]map[string]any),
	}
}

// Priority 返回执行优先级，对齐 Python: priority = 80
func (r *JiuClawStreamEventRail) Priority() int {
	return 80
}

// Init Rail 初始化钩子，对齐 Python: init(self, agent)
func (r *JiuClawStreamEventRail) Init(_ context.Context, agent sainterfaces.BaseAgent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deepAgent = agent
	return nil
}

// Uninit Rail 注销钩子
func (r *JiuClawStreamEventRail) Uninit(_ sainterfaces.BaseAgent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deepAgent = nil
	return nil
}

// ── 外部 API：暂停/恢复/终止 ──

// Pause 暂停指定会话，对齐 Python: pause(session_id)
func (r *JiuClawStreamEventRail) Pause(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)
	r.getPauseCond(sid).Pause()
}

// Resume 恢复指定会话并清除终止标记，对齐 Python: resume(session_id)
func (r *JiuClawStreamEventRail) Resume(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)
	delete(r.abortRequested, sid)
	r.getPauseCond(sid).Resume()
}

// Abort 终止指定会话（设终止标记 + 恢复暂停），对齐 Python: abort(session_id)
func (r *JiuClawStreamEventRail) Abort(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)
	r.abortRequested[sid] = true
	r.getPauseCond(sid).Resume() // 恢复暂停以便 checkpoint 可以检查 abort 标记
}

// ResetAbort 清除终止标记，对齐 Python: reset_abort(session_id)
func (r *JiuClawStreamEventRail) ResetAbort(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)
	delete(r.abortRequested, sid)
}

// ResetForNewTask 为新任务重置暂停状态（不碰终止标记），对齐 Python: reset_for_new_task(session_id)
//
// 取消时调用，以便下一个任务不会卡在 _pause_event.wait() 检查点。
// 终止标记故意不清除——必须保持 True 直到下一个任务的 process_message_*_impl
// 在入口处调用 reset_abort()。
func (r *JiuClawStreamEventRail) ResetForNewTask(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)
	r.getPauseCond(sid).Resume()
	delete(r.conversationIDs, sid)
	delete(r.mainSessions, sid)
}

// CleanupSession 移除指定会话的所有状态，对齐 Python: cleanup_session(session_id)
//
// 当适配器上最后一个任务完成（计数器降为 0）时调用，防止长期运行适配器上的无限增长。
func (r *JiuClawStreamEventRail) CleanupSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)
	delete(r.abortRequested, sid)
	delete(r.pauseConds, sid)
	delete(r.conversationIDs, sid)
	delete(r.mainSessions, sid)
	delete(r.cancelledToolResults, sid)
}

// GetCancelledToolResults 获取取消的工具结果，对齐 Python: get_cancelled_tool_results(session_id)
func (r *JiuClawStreamEventRail) GetCancelledToolResults(sessionID string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)
	results := r.cancelledToolResults[sid]
	if results == nil {
		return nil
	}
	// 返回副本
	return append([]map[string]any(nil), results...)
}

// ClearCancelledToolResults 清除取消的工具结果，对齐 Python: clear_cancelled_tool_results(session_id)
func (r *JiuClawStreamEventRail) ClearCancelledToolResults(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)
	delete(r.cancelledToolResults, sid)
}

// CollectCancelledToolUpdates 收集取消的工具信息用于中断响应，对齐 Python: collect_cancelled_tool_updates(session_id)
func (r *JiuClawStreamEventRail) CollectCancelledToolUpdates(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sid := resolveSessionID(sessionID)

	if r.cancelledToolResults[sid] == nil {
		r.cancelledToolResults[sid] = make([]map[string]any, 0)
	}

	for tcID, info := range r.inflightToolCalls {
		// 仅收集匹配目标会话的工具
		if sessionID != "" && info.sessionID != sessionID {
			continue
		}
		tcName := toolCallName(info.toolCall)
		r.cancelledToolResults[sid] = append(r.cancelledToolResults[sid], map[string]any{
			"tool_name":    tcName,
			"tool_call_id": tcID,
			"result":       "[Interrupted] Tool execution cancelled by user.",
			"status":       "error",
		})
		delete(r.inflightToolCalls, tcID)
	}
	logger.Info(logComponent).
		Int("count", len(r.cancelledToolResults[sid])).
		Str("session_id", sessionID).
		Msg("collected cancelled tools")
}

// SetTodoTool 设置共享的 TodoTool 实例，对齐 Python: _main_todo_tool
func (r *JiuClawStreamEventRail) SetTodoTool(loader todoToolLoader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.todoTool = loader
}

// ── 生命周期钩子 ──

// BeforeInvoke invoke 开始前，捕获 conversation_id，对齐 Python: before_invoke
func (r *JiuClawStreamEventRail) BeforeInvoke(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*sainterfaces.InvokeInputs)
	if !ok {
		return nil
	}
	// 子代理在 before_invoke 时 session 为 nil，跳过
	session := cbc.Session()
	if session == nil {
		return nil
	}
	// 使用真实的 conversation_id 作为会话键
	rawConvID := inputs.ConversationID
	sid := rawConvID
	if sid == "" {
		sid = defaultSessionID
	}
	r.mu.Lock()
	if rawConvID != "" {
		r.conversationIDs[sid] = rawConvID
	}
	r.mainSessions[sid] = session
	r.mu.Unlock()

	// 通过 ctx.extra 传递 session_id，对齐 Python: ctx.extra[self._SID_KEY] = sid
	cbc.Extra()[sidKey] = sid
	return nil
}

// BeforeModelCall LLM 调用前：暂停检查 + 上下文修复，对齐 Python: before_model_call
func (r *JiuClawStreamEventRail) BeforeModelCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	sid := r.resolveSID(cbc, cbc.Session())

	// 暂停检查点，对齐 Python: await self._get_pause_event(sid).wait()
	r.checkpointWait(sid)

	// 终止检查，对齐 Python: if self._abort_requested.get(sid, False): raise asyncio.CancelledError
	if r.isAbortRequested(sid) {
		return fmt.Errorf("Agent abort requested")
	}

	// 上下文修复，对齐 Python: await self._fix_incomplete_tool_context(ctx.context)
	if cbc.ModelContext() != nil {
		if err := fixIncompleteToolContext(ctx, cbc.ModelContext(), r.getPromptLanguage); err != nil {
			logger.Warn(logComponent).Err(err).Msg("fix_incomplete_tool_context failed in before_model_call")
		}
	}

	return nil
}

// AfterModelCall LLM 响应后：发送 context.usage，对齐 Python: after_model_call
func (r *JiuClawStreamEventRail) AfterModelCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	emitContextUsage(ctx, cbc)
	return nil
}

// BeforeToolCall 工具执行前：暂停检查 + 发送 tool_call/tool_update + 跟踪 in-flight，对齐 Python: before_tool_call
func (r *JiuClawStreamEventRail) BeforeToolCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	sid := r.resolveSID(cbc, cbc.Session())

	// 暂停检查点
	r.checkpointWait(sid)

	// 终止检查
	if r.isAbortRequested(sid) {
		return fmt.Errorf("Agent abort requested")
	}

	session := cbc.Session()
	if session == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*sainterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	tc := inputs.ToolCall

	// 发送 tool_call 和 tool_update 事件
	emitToolCall(ctx, session, tc)
	emitToolUpdate(ctx, session, tc, "in_progress")

	// 跟踪 in-flight 工具调用，对齐 Python: self._inflight_tool_calls[tc_id] = {...}
	if tc != nil && tc.ID != "" {
		r.mu.Lock()
		r.inflightToolCalls[tc.ID] = inflightToolCallInfo{
			toolCall:  tc,
			sessionID: sid,
		}
		r.mu.Unlock()
	}

	return nil
}

// AfterToolCall 工具执行后：发送 tool_result + ask_user_question + todo.updated，对齐 Python: after_tool_call
func (r *JiuClawStreamEventRail) AfterToolCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	session := cbc.Session()
	if session == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*sainterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	tc := inputs.ToolCall
	tcID := toolCallID(tc)

	// 从 in-flight 跟踪中移除，对齐 Python: self._inflight_tool_calls.pop(tc_id, None)
	if tcID != "" {
		r.mu.Lock()
		delete(r.inflightToolCalls, tcID)
		r.mu.Unlock()
	}

	// 发送 tool_result 事件
	emitToolResult(ctx, session, tc, inputs.ToolResult)

	// 发送 ask_user_question 事件
	emitAskUserQuestionIfInterrupted(ctx, session, tc, inputs.ToolName, inputs.ToolResult, cbc.Exception())

	// 检查是否为 TODO 工具，对齐 Python: if tool_name in _TODO_TOOL_NAMES
	sid := r.resolveSID(cbc, session)
	convID := r.getConversationID(sid)
	if convID == "" {
		return nil
	}
	if todoToolNames[inputs.ToolName] {
		r.mu.Lock()
		loader := r.todoTool
		r.mu.Unlock()
		emitTodoUpdated(ctx, session, loader, convID)
	}

	return nil
}

// OnModelException LLM 调用异常：上下文修复，对齐 Python: on_model_exception
func (r *JiuClawStreamEventRail) OnModelException(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	if cbc.ModelContext() != nil {
		logger.Info(logComponent).Msg("Attempting context repair after model exception")
		if err := fixIncompleteToolContext(ctx, cbc.ModelContext(), r.getPromptLanguage); err != nil {
			logger.Warn(logComponent).Err(err).Msg("fix_incomplete_tool_context failed in on_model_exception")
		}
	}
	return nil
}

// GetCallbacks 提取已覆盖的钩子方法映射，供 RegisterRail 批量注册
func (r *JiuClawStreamEventRail) GetCallbacks() map[sainterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(sainterfaces.CallbackBeforeInvoke, r.wrapBeforeInvoke),
		r.CallbackFrom(sainterfaces.CallbackBeforeModelCall, r.wrapBeforeModelCall),
		r.CallbackFrom(sainterfaces.CallbackAfterModelCall, r.wrapAfterModelCall),
		r.CallbackFrom(sainterfaces.CallbackBeforeToolCall, r.wrapBeforeToolCall),
		r.CallbackFrom(sainterfaces.CallbackAfterToolCall, r.wrapAfterToolCall),
		r.CallbackFrom(sainterfaces.CallbackOnModelException, r.wrapOnModelException),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSessionID 解析会话 ID，空字符串回退到 "default"
func resolveSessionID(sessionID string) string {
	if sessionID == "" {
		return defaultSessionID
	}
	return sessionID
}

// resolveSID 解析跨 Rail 传递的 session ID，对齐 Python: _resolve_sid
//
// 大多数回调从 before_invoke 继承 ctx.extra，但工具回调可能缺少该值。
// 回退到已捕获的主会话标识。
func (r *JiuClawStreamEventRail) resolveSID(cbc *sainterfaces.AgentCallbackContext, session sessioninterfaces.SessionFacade) string {
	// 首先从 ctx.extra 获取
	extra := cbc.Extra()
	if sid, ok := extra[sidKey].(string); ok && sid != "" {
		return sid
	}
	// 回退：遍历 mainSessions 查找匹配
	if session != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		for knownSID, knownSession := range r.mainSessions {
			if knownSession == session {
				extra[sidKey] = knownSID
				return knownSID
			}
		}
	}
	return defaultSessionID
}

// getPauseCond 获取/创建指定会话的暂停条件变量，对齐 Python: _get_pause_event
//
// 创建的条件变量初始为未暂停状态（对齐 Python: asyncio.Event 初始 set()）
func (r *JiuClawStreamEventRail) getPauseCond(sid string) *pauseCond {
	if pc, ok := r.pauseConds[sid]; ok {
		return pc
	}
	pc := newPauseCond()
	r.pauseConds[sid] = pc
	return pc
}

// checkpointWait 执行暂停检查点，对齐 Python: await self._get_pause_event(sid).wait()
func (r *JiuClawStreamEventRail) checkpointWait(sid string) {
	r.mu.Lock()
	pc := r.getPauseCond(sid)
	r.mu.Unlock()
	pc.Wait()
}

// isAbortRequested 检查是否请求了终止，对齐 Python: self._abort_requested.get(sid, False)
func (r *JiuClawStreamEventRail) isAbortRequested(sid string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.abortRequested[sid]
}

// getConversationID 获取指定会话的 conversation_id
func (r *JiuClawStreamEventRail) getConversationID(sid string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conversationIDs[sid]
}

// getPromptLanguage 获取当前提示词语言，对齐 Python: _get_prompt_language
func (r *JiuClawStreamEventRail) getPromptLanguage() string {
	if r.deepAgent == nil {
		return "cn"
	}
	builder := r.deepAgent.SystemPromptBuilder()
	if builder == nil {
		return "cn"
	}
	lang := builder.Language()
	if lang == "" {
		return "cn"
	}
	return lang
}

// ── GetCallbacks 包装方法 ──
// PerAgentCallbackFunc 签名为 func(ctx context.Context, agentCallbackContext any) error
// 需要将 any 断言为 *AgentCallbackContext

func (r *JiuClawStreamEventRail) wrapBeforeInvoke(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeInvoke(ctx, cbc)
}

func (r *JiuClawStreamEventRail) wrapBeforeModelCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeModelCall(ctx, cbc)
}

func (r *JiuClawStreamEventRail) wrapAfterModelCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.AfterModelCall(ctx, cbc)
}

func (r *JiuClawStreamEventRail) wrapBeforeToolCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeToolCall(ctx, cbc)
}

func (r *JiuClawStreamEventRail) wrapAfterToolCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.AfterToolCall(ctx, cbc)
}

func (r *JiuClawStreamEventRail) wrapOnModelException(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.OnModelException(ctx, cbc)
}
