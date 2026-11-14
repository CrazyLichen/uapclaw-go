package rails

import (
	"context"
	"fmt"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	interruptrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JiuClawStreamEventRail 前端流事件发射 + 暂停/终止检查点 + 上下文修复。
// 对齐 Python: JiuClawStreamEventRail (priority=80)
//
// 暂停/终止状态由本 Rail 持有（而非 DeepAgent），使得 interface_deep
// 可以直接调用 rail.Pause() / rail.Resume() / rail.Abort() 而无需修改 DeepAgent。
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

	// quarantinedSessions 已隔离的会话集合，对齐 Python: _quarantined_sessions
	//
	// 隔离是永久的：一旦会话的运行时被判定损坏（fail-closed 清理失败），
	// 在它所属的会话被销毁前不再接受任何新工作。
	quarantinedSessions map[string]bool

	// projectionDeps 中断投影实现（装配期注入，见 InterruptionProjectionDeps）
	projectionDeps InterruptionProjectionDeps

	// todoTool 共享 TodoTool 实例，对齐 Python: _main_todo_tool
	todoTool todoToolLoader

	// rootPermissionQueue 根权限队列，供中断快照投影与清理范围校验使用。
	// 装配期由 permission_rail_group 注入；为 nil 表示非自动权限模式。
	// Python: _root_permission_queue
	//
	// 用窄接口承载以避免 rails ↔ rails/permissions 循环依赖
	// （规则 10.2：本包是使用方，描述协作方能力）。
	rootPermissionQueue RootPermissionQueueAccessor
}

// pauseCond 封装 sync.Mutex + sync.Cond 实现 asyncio.Event 语义
type pauseCond struct {
	mu     sync.Mutex
	cond   *sync.Cond
	paused bool // true = 已暂停（等待中）
}

// inflightToolCallInfo 跟踪执行中的工具调用，对齐 Python: _inflight_tool_calls value
type inflightToolCallInfo struct {
	toolCall  *llmschema.ToolCall // 工具调用引用
	sessionID string              // 关联的会话 ID
}

// deepConfigProvider 暴露 harness DeepAgent 的 deep_config（Python: da.deep_config）。
// 由 *harness.DeepAgent 实现（deep_agent.go 的 DeepConfig()）；ReActAgent 等不实现，
// 类型断言失败时返回 nil——与 Python 侧 rail 只在 DeepAgent 路径生效一致。
type deepConfigProvider interface {
	// DeepConfig 返回 DeepAgent 的运行时配置（含 Workspace/SysOperation/Language/Card）
	DeepConfig() *hschema.DeepAgentConfig
}

// RootPermissionQueueAccessor 根权限队列在本 rail 视角下的能力面。
//
// 只列举 stream rail 真正需要的方法，避免引入整个 RootPermissionQueue
// （同时打破 rails ↔ rails/permissions 的循环依赖）。
type RootPermissionQueueAccessor interface {
	// IsRootPermissionQueue 标识该值是可用的根权限队列
	IsRootPermissionQueue()
}

// InterruptionProjectionDeps 中断投影的外部实现（装配期注入）。
//
// # 为什么用回调而不是直接调方法
//
// Python 的 stream_event_rail.py 直接 import root_permission_queue，两边是平铺
// 模块，互引没有约束。Go 里 permissions 包已经 import rails（root_ask_user.go /
// permission_interrupt_rail.go），rails 反向 import permissions 就成环。
//
// 但投影函数（_project_complete_interruption 等）必须碰 RootPermissionQueue /
// RootPermissionSnapshot / RootPermissionCard 这些具体类型。解法：rail 只声明
// 函数签名，由 permissions 包提供实现，装配层（code_adapter）负责接线。
//
// 偏差登记：Python 这 4 个是 rail 的方法，Go 是 permissions 包的包级函数 +
// 本结构体的回调字段。
type InterruptionProjectionDeps struct {
	// ProjectCompleteInterruption 中断结束后投影「已完成的权限审批」。
	//
	// Python: JiuSwarmStreamEventRail._project_complete_interruption(ctx)
	ProjectCompleteInterruption func(ctx context.Context, rail *JiuClawStreamEventRail, cbc *sainterfaces.AgentCallbackContext) error
	// OrdinaryInterruptionRetryMessage 返回中断恢复失败时给用户的提示文案。
	//
	// Python: JiuSwarmStreamEventRail._interruption_retry_message()
	OrdinaryInterruptionRetryMessage func(rail *JiuClawStreamEventRail) string
}

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
		quarantinedSessions:  make(map[string]bool),
	}
}

// SetRootPermissionQueue 注入根权限队列（装配期调用）。
//
// Python: JiuSwarmStreamEventRail.__init__(root_permission_queue=...)
func (r *JiuClawStreamEventRail) SetRootPermissionQueue(queue RootPermissionQueueAccessor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rootPermissionQueue = queue
}

// RootPermissionQueue 返回已注入的根权限队列（未注入为 nil）。
func (r *JiuClawStreamEventRail) RootPermissionQueue() RootPermissionQueueAccessor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rootPermissionQueue
}

// SetInterruptionProjectionDeps 注入中断投影实现（装配期调用）。
//
// 见 InterruptionProjectionDeps 的类型注释：Go 用回调绕开 rails ↔ permissions
// 的循环依赖。
func (r *JiuClawStreamEventRail) SetInterruptionProjectionDeps(deps InterruptionProjectionDeps) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projectionDeps = deps
}

// ProjectionDeps 返回已注入的中断投影实现。
func (r *JiuClawStreamEventRail) ProjectionDeps() InterruptionProjectionDeps {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.projectionDeps
}

// Priority 返回执行优先级，对齐 Python: priority = 80
func (r *JiuClawStreamEventRail) Priority() int {
	return 80
}

// Init Rail 初始化钩子，对齐 Python: init(self, agent)
//
// 仅保存 agent 引用；TodoTool 采用懒创建（对齐 Python `_get_todo_tool()`：
// 首次发射 todo.updated 时才从 agent 的 deep_config 构造并缓存）。
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

// QuarantineSession 永久隔离一个损坏的运行时，直到其所属会话被销毁。
//
// Python: quarantine_session(session_id, session=None) (stream_event_rail.py L675-684)
//
// 三步：
//  1. 记入隔离集合
//  2. 有 session 时把隔离标记写进会话状态（跨进程重启后仍生效）
//  3. 终止该会话
//
// 注意：Abort 自己加锁，这里不能持锁调用它——Python 的 abort 同样只是往
// dict 里写标记，没有独立的锁语义。为避免死锁，第 3 步在锁外做。
func (r *JiuClawStreamEventRail) QuarantineSession(
	sessionID string,
	session sessioninterfaces.SessionFacade,
) {
	sid := resolveSessionID(sessionID)
	r.mu.Lock()
	r.quarantinedSessions[sid] = true
	r.mu.Unlock()

	if session != nil {
		session.UpdateState(map[string]any{
			interruptrail.PermissionRuntimeQuarantinedKey: true,
		})
	}
	r.Abort(sid)
}

// RaiseIfQuarantined 在 fail-closed 中断清理失败后拒绝新工作。
//
// Python: raise_if_quarantined(session_id, session=None) (stream_event_rail.py L686-701)
//
// 两个来源任一命中即视为隔离：
//   - 内存中的隔离集合
//   - 会话状态里持久化的隔离标记（跨进程重启后仍生效）
//
// 命中时把 sid 补写进内存集合（内存态可能因重启丢失），再返回错误。
func (r *JiuClawStreamEventRail) RaiseIfQuarantined(
	sessionID string,
	session sessioninterfaces.SessionFacade,
) error {
	sid := resolveSessionID(sessionID)

	persisted := false
	if session != nil {
		if value, err := session.GetState(state.StringKey(interruptrail.PermissionRuntimeQuarantinedKey)); err == nil {
			persisted = value == true
		}
	}

	r.mu.Lock()
	quarantined := r.quarantinedSessions[sid]
	if quarantined || persisted {
		r.quarantinedSessions[sid] = true
	}
	r.mu.Unlock()

	if quarantined || persisted {
		return fmt.Errorf("session_runtime_quarantined")
	}
	return nil
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
//
// 生产路径不再依赖显式注入：首次发射 todo.updated 时由 getTodoTool 懒创建。
// 本方法仅供单测注入 fake（Python 无对应 API，不改变生产语义）。
func (r *JiuClawStreamEventRail) SetTodoTool(loader todoToolLoader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.todoTool = loader
}

// getTodoTool 返回主 Agent 的 TodoTool，首次调用时构造并缓存。
// 对齐 Python: _get_todo_tool()——刻意不复用 AbilityManager / ResourceMgr：
//  1. 资源管理器是共享的，子 agent 会把自己的 todo 工具注册进去覆盖主 agent 条目，
//     导致 load_todos 读到错误的 workspace 路径（Python docstring 原话）；
//  2. Go 的 AbilityManager 只存 *tool.ToolCard，取不回 TodoTool 基类（LoadTodos 在基类上）；
//  3. 主 agent 的 deep_config.workspace 在 init 时固定，用它构造的句柄路径稳定。
//
// 状态一致性靠文件：自建句柄与模型调用的 todo_modify 读写同一份
// {workspace}/{session_id}/todo.json。
func (r *JiuClawStreamEventRail) getTodoTool() todoToolLoader {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.todoTool != nil {
		return r.todoTool
	}
	provider, ok := r.deepAgent.(deepConfigProvider)
	if !ok {
		return nil
	}
	r.todoTool = newTodoToolFromDeepConfig(provider.DeepConfig(), r.promptLanguageLocked())
	return r.todoTool
}

// promptLanguageLocked 返回系统提示词语言，调用方需持有 r.mu。
// 对齐 Python: getattr(da, "system_prompt_builder", None).language
func (r *JiuClawStreamEventRail) promptLanguageLocked() string {
	if r.deepAgent == nil {
		return ""
	}
	builder := r.deepAgent.SystemPromptBuilder()
	if builder == nil {
		return ""
	}
	return builder.Language()
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

	// 隔离检查：fail-closed 中断清理失败后，该会话不再接受任何新工作。
	// Python: raise_if_quarantined(session_id, session) (stream_event_rail.py L686-701)
	if err := r.RaiseIfQuarantined(sid, session); err != nil {
		logger.Warn(logComponent).
			Str("session_id", sid).
			Msg("[StreamEventRail] reject work for quarantined session")
		return err
	}

	r.mu.Lock()
	if rawConvID != "" {
		r.conversationIDs[sid] = rawConvID
	}
	r.mainSessions[sid] = session
	r.mu.Unlock()

	// 通过 ctx.extra 传递 session_id，对齐 Python: ctx.extra[self._SID_KEY] = sid
	// S-24: 裸 map 写迁移 SetExtra（线程安全）
	cbc.SetExtra(sidKey, sid)
	return nil
}

// BeforeModelCall LLM 调用前：暂停检查 + 上下文修复，对齐 Python: before_model_call
func (r *JiuClawStreamEventRail) BeforeModelCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	sid := r.resolveSID(cbc, cbc.Session())

	// 暂停检查点，对齐 Python: await self._get_pause_event(sid).wait()
	r.checkpointWait(ctx, sid)

	// 终止检查，对齐 Python: if self._abort_requested.get(sid, False): raise asyncio.CancelledError
	if r.isAbortRequested(sid) {
		return fmt.Errorf("agent abort requested")
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
	r.checkpointWait(ctx, sid)

	// 终止检查
	if r.isAbortRequested(sid) {
		return fmt.Errorf("agent abort requested")
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

// AfterToolCall 工具执行后：发送 tool_result + todo.updated，对齐 Python: after_tool_call
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

	// 检查是否为 TODO 工具，对齐 Python: if tool_name in _TODO_TOOL_NAMES
	sid := r.resolveSID(cbc, session)
	convID := r.getConversationID(sid)
	if convID == "" {
		return nil
	}
	if todoToolNames[inputs.ToolName] {
		emitTodoUpdated(ctx, session, r.getTodoTool(), convID)
	}

	return nil
}

// AfterInvoke 中断结束后的投影钩子。
//
// Python: after_invoke(ctx) (stream_event_rail.py L792-809)
//
// 只做一件事：若是正常 invoke 且会话存在，投影「已完成的权限审批」。
// 投影实现由 permissions 包提供（见 InterruptionProjectionDeps）。
//
// Python 的 finally 里还有 reset_shell_session_id(token) —— Go 侧无对应逻辑：
// Python 用全局 registry 的栈式 token（set 返回 token，reset 弹栈）；Go 的
// SetShellSessionID 是 context.WithValue 派生新 ctx（不可变），没有跨调用
// 残留的全局状态要清，故此处不做 reset。
func (r *JiuClawStreamEventRail) AfterInvoke(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	if _, ok := cbc.Inputs().(*sainterfaces.InvokeInputs); !ok {
		return nil
	}
	if cbc.Session() == nil {
		return nil
	}

	deps := r.ProjectionDeps()
	if deps.ProjectCompleteInterruption == nil {
		return nil
	}
	return deps.ProjectCompleteInterruption(ctx, r, cbc)
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
		r.CallbackFrom(sainterfaces.CallbackAfterInvoke, r.wrapAfterInvoke),
		r.CallbackFrom(sainterfaces.CallbackBeforeModelCall, r.wrapBeforeModelCall),
		r.CallbackFrom(sainterfaces.CallbackAfterModelCall, r.wrapAfterModelCall),
		r.CallbackFrom(sainterfaces.CallbackBeforeToolCall, r.wrapBeforeToolCall),
		r.CallbackFrom(sainterfaces.CallbackAfterToolCall, r.wrapAfterToolCall),
		r.CallbackFrom(sainterfaces.CallbackOnModelException, r.wrapOnModelException),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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

// WaitWithContext 阻塞直到未暂停或 context 被取消，避免 goroutine 泄漏
func (pc *pauseCond) WaitWithContext(ctx context.Context) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if !pc.paused {
		return nil
	}

	// 启动辅助 goroutine：context 取消时唤醒 cond.Wait()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			pc.cond.Broadcast()
		case <-done:
		}
	}()
	defer close(done)

	for pc.paused {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		pc.cond.Wait()
	}
	return nil
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

// newTodoToolFromDeepConfig 从 deep_config 构造 TodoTool。
// 对齐 Python: _get_todo_tool() 的构造段——
//
//	TodoListTool(operation=deep_config.sys_operation,
//	             workspace=deep_config.workspace.get_node_path(TODO),
//	             language=deep_config.language or system_prompt_builder.language or "cn",
//	             agent_id=da.card.id)
//
// 差异说明：Python 的 TodoTool 只使用 operation.fs()（todo.py:80），故 Go 传 FsOperation 等价。
// 任一必需项缺失返回 nil（与 Python 返回 None 等价，调用方跳过事件发射）。
func newTodoToolFromDeepConfig(cfg *hschema.DeepAgentConfig, fallbackLang string) todoToolLoader {
	if cfg == nil || cfg.Workspace == nil || cfg.SysOperation == nil {
		return nil
	}
	fs := cfg.SysOperation.Fs()
	if fs == nil {
		return nil
	}
	todoPath := cfg.Workspace.GetNodePath(workspace.WorkspaceNodeTODO)
	if todoPath == nil || *todoPath == "" {
		return nil
	}
	language := cfg.Language
	if language == "" {
		language = fallbackLang
	}
	if language == "" {
		language = "cn"
	}
	agentID := ""
	if cfg.Card != nil {
		agentID = cfg.Card.ID
	}
	_, base := todo.CreateTodosTool(*todoPath, fs, language, agentID)
	return &base
}

// resolveSessionID 解析会话 ID，空字符串回退到 "default"
func resolveSessionID(sessionID string) string {
	if sessionID == "" {
		return defaultSessionID
	}
	return sessionID
}

// ResolveSID 解析跨 Rail 传递的 session ID（对外访问器）。
//
// 供 permissions 包的中断投影回调使用：投影需要判断当前会话键以定位队列范围。
func (r *JiuClawStreamEventRail) ResolveSID(
	cbc *sainterfaces.AgentCallbackContext,
	session sessioninterfaces.SessionFacade,
) string {
	return r.resolveSID(cbc, session)
}

// PromptLanguage 返回当前提示词语言（"cn" / "en"）。
//
// 供 permissions 包生成中断恢复提示文案使用。
func (r *JiuClawStreamEventRail) PromptLanguage() string {
	return r.getPromptLanguage()
}

// resolveSID 解析跨 Rail 传递的 session ID，对齐 Python: _resolve_sid
//
// 大多数回调从 before_invoke 继承 ctx.extra，但工具回调可能缺少该值。
// 回退到已捕获的主会话标识。
func (r *JiuClawStreamEventRail) resolveSID(cbc *sainterfaces.AgentCallbackContext, session sessioninterfaces.SessionFacade) string {
	// 首先从 ctx.extra 获取
	// S-24: 裸 map 读迁移 GetExtra（线程安全）
	if sidVal, ok := cbc.GetExtra(sidKey); ok {
		if sid, ok := sidVal.(string); ok && sid != "" {
			return sid
		}
	}
	// 回退：遍历 mainSessions 查找匹配
	if session != nil {
		var matchedSID string
		r.mu.Lock()
		for knownSID, knownSession := range r.mainSessions {
			if knownSession == session {
				matchedSID = knownSID
				break
			}
		}
		r.mu.Unlock()
		// S-24: 裸 map 写迁移 SetExtra（线程安全）；
		// 锁外写入避免 r.mu 与 extraMu 嵌套死锁
		if matchedSID != "" {
			cbc.SetExtra(sidKey, matchedSID)
			return matchedSID
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
// 接受 ctx 参数，当 context 取消时退出等待，避免 goroutine 泄漏
func (r *JiuClawStreamEventRail) checkpointWait(ctx context.Context, sid string) {
	r.mu.Lock()
	pc := r.getPauseCond(sid)
	r.mu.Unlock()
	if err := pc.WaitWithContext(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sid).Msg("checkpointWait context cancelled")
	}
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

// wrapAfterInvoke 适配通用回调签名，把入参断言成具体上下文后转调 AfterInvoke。
func (r *JiuClawStreamEventRail) wrapAfterInvoke(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.AfterInvoke(ctx, cbc)
}

// wrapBeforeInvoke 适配通用回调签名，把入参断言成具体上下文后转调 BeforeInvoke。
func (r *JiuClawStreamEventRail) wrapBeforeInvoke(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeInvoke(ctx, cbc)
}

// wrapBeforeModelCall 适配通用回调签名，转调 BeforeModelCall。
func (r *JiuClawStreamEventRail) wrapBeforeModelCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeModelCall(ctx, cbc)
}

// wrapAfterModelCall 适配通用回调签名，转调 AfterModelCall。
func (r *JiuClawStreamEventRail) wrapAfterModelCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.AfterModelCall(ctx, cbc)
}

// wrapBeforeToolCall 适配通用回调签名，转调 BeforeToolCall。
func (r *JiuClawStreamEventRail) wrapBeforeToolCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeToolCall(ctx, cbc)
}

// wrapAfterToolCall 适配通用回调签名，转调 AfterToolCall。
func (r *JiuClawStreamEventRail) wrapAfterToolCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.AfterToolCall(ctx, cbc)
}

// wrapOnModelException 适配通用回调签名，转调 OnModelException。
func (r *JiuClawStreamEventRail) wrapOnModelException(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.OnModelException(ctx, cbc)
}
