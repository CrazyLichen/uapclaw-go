package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionRail 所有演化轨道的基类。
// 嵌入 DeepAgentRail，4 个 final 回调自动完成轨迹收集，
// 通过 ext EvolutionExtension 接口实现子类多态分派。
// 核心设计（对齐 Python EvolutionRail）：
//   - 轨迹收集是自动的（由基类 4 个 final 回调处理）
//   - 扩展点：OnBeforeInvoke / OnAfterModelCall / OnAfterToolCall /
//     OnAfterInvoke / OnAfterTaskIteration / RunEvolution（方法名保持英文）
//   - 演化触发时机通过 evolution_trigger 参数配置
//
// Python: EvolutionRail(DeepAgentRail)
type EvolutionRail struct {
	rails.DeepAgentRail
	// ext 扩展点接口，实现多态分派
	ext EvolutionExtension
	// trajectoryStore 轨迹存储
	trajectoryStore trajectory.TrajectoryStore
	// scopeMu 窗口/订阅锁（Python: _window_lock + _subscription_lock 合一）
	scopeMu sync.Mutex
	// scopeWindows scope 键 → 窗口状态（Python: _scope_windows）
	scopeWindows map[string]*scopeWindowState
	// activeCaptures 活跃采集集（Python: _active_captures）
	activeCaptures map[*invokeCapture]struct{}
	// currentCapture 当前 invoke 采集（Python: _invoke_capture ContextVar 的
	// 显式化，D-25.2b-4）
	currentCapture *invokeCapture
	// builder 轨迹构造器（跨 invoke 复用）
	builder *trajectory.TrajectoryBuilder
	// maxTrajectorySteps 最大轨迹步骤数
	maxTrajectorySteps int
	// evolutionTrigger 演化触发时机
	evolutionTrigger EvolutionTriggerPoint
	// asyncEvolution 是否异步执行演化
	asyncEvolution bool
	// evolutionSem 演化并发控制信号量
	evolutionSem chan struct{}
	// disabledSkills 禁用的技能名称集合
	disabledSkills map[string]bool
	// teamID 团队标识
	teamID string
	// memberRole 成员角色
	memberRole string
	// defaultMemberRole 默认成员角色。
	// Python: EvolutionRail._default_member_role
	defaultMemberRole string
	// bgTasks 后台任务集合
	bgTasks map[*utils.BackgroundTask]bool
	// pendingHostEvents 待排空的主机事件缓冲
	pendingHostEvents []*stream.OutputSchema
}

// EvolutionRailOption EvolutionRail 构造选项函数。
type EvolutionRailOption func(*EvolutionRail)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionRail 创建演化轨道基类实例。
// ext 参数为扩展点接口，不能为 nil；opts 可选覆盖默认配置。
// Python: EvolutionRail.__init__(
//
//	trajectory_store=None, team_trajectory_store=None(已废弃),
//	max_trajectory_steps=200, evolution_trigger=AFTER_INVOKE,
//	async_evolution=True, max_concurrent_evolution=1, disabled_skills=None
//
//	Python: )
func NewEvolutionRail(ext EvolutionExtension, opts ...EvolutionRailOption) *EvolutionRail {
	r := &EvolutionRail{
		ext:                ext,
		trajectoryStore:    trajectory.NewInMemoryTrajectoryStore(),
		maxTrajectorySteps: 200,
		evolutionTrigger:   TriggerAfterInvoke,
		asyncEvolution:     true,
		evolutionSem:       make(chan struct{}, 1),
		disabledSkills:     make(map[string]bool),
		bgTasks:            make(map[*utils.BackgroundTask]bool),
		pendingHostEvents:  make([]*stream.OutputSchema, 0),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithTrajectoryStore 设置轨迹存储。
func WithTrajectoryStore(store trajectory.TrajectoryStore) EvolutionRailOption {
	return func(r *EvolutionRail) { r.trajectoryStore = store }
}

// WithMaxTrajectorySteps 设置最大轨迹步骤数。
func WithMaxTrajectorySteps(n int) EvolutionRailOption {
	return func(r *EvolutionRail) { r.maxTrajectorySteps = n }
}

// WithEvolutionTrigger 设置演化触发时机。
func WithEvolutionTrigger(trigger EvolutionTriggerPoint) EvolutionRailOption {
	return func(r *EvolutionRail) { r.evolutionTrigger = trigger }
}

// WithAsyncEvolution 设置是否异步执行演化。
func WithAsyncEvolution(async bool) EvolutionRailOption {
	return func(r *EvolutionRail) { r.asyncEvolution = async }
}

// WithMaxConcurrentEvolution 设置最大并发演化数。
func WithMaxConcurrentEvolution(n int) EvolutionRailOption {
	return func(r *EvolutionRail) { r.evolutionSem = make(chan struct{}, n) }
}

// WithDisabledSkills 设置禁用的技能名称列表。
func WithDisabledSkills(names []string) EvolutionRailOption {
	return func(r *EvolutionRail) { r.disabledSkills = normalizeSkillNames(names) }
}

// WithDefaultMemberRole 设置默认成员角色。
// Python: EvolutionRail(default_member_role=...)
func WithDefaultMemberRole(role string) EvolutionRailOption {
	return func(r *EvolutionRail) {
		r.defaultMemberRole = role
	}
}

// Priority 返回优先级 60。
// Python: EvolutionRail.priority = 60
func (r *EvolutionRail) Priority() int { return 60 }

// TrajectoryStore 返回轨迹存储。
func (r *EvolutionRail) TrajectoryStore() trajectory.TrajectoryStore {
	return r.trajectoryStore
}

// DisabledSkills 返回禁用技能集合。
func (r *EvolutionRail) DisabledSkills() map[string]bool {
	return r.disabledSkills
}

// Builder 返回当前轨迹构造器（子类需要）。
// Python: EvolutionRail.builder property
func (r *EvolutionRail) Builder() *trajectory.TrajectoryBuilder {
	return r.builder
}

// 【12-04 迁移注记】SetTrajectorySink / publishTrajectorySnapshot /
// MemberTrajectorySnapshot 已删除：上游 HEAD 无 set_trajectory_sink /
// publish_member_trajectory（registry 链为 Go 侧旧模型遗产，规则 8）。
// 跨成员轨迹可见性改由 TrajectorySpanProcessor 订阅窗口承载（25.2b）。

// EmitHostEvent 缓存一个主机事件。
// Python: EvolutionRail.emit_host_event(event)
func (r *EvolutionRail) EmitHostEvent(event *stream.OutputSchema) {
	if event != nil {
		r.pendingHostEvents = append(r.pendingHostEvents, event)
	}
}

// DrainPendingHostEvents 返回并清空主机事件缓冲。
// 如果 wait=true，先等待所有未完成的后台任务；timeout 控制最大等待时间。
// timeout=nil 时自动回退到 GetEvolutionTotalTimeoutSecs；timeout > 0 时精确超时，超时后返回已收集的事件。
// Python: EvolutionRail.drain_pending_host_events(wait, timeout)
// Python 在 timeout=None 时回退：timeout = self._get_evolution_total_timeout_secs()
func (r *EvolutionRail) DrainPendingHostEvents(wait bool, timeout *time.Duration) []*stream.OutputSchema {
	if wait && len(r.bgTasks) > 0 {
		// 筛选未完成的任务
		pending := make([]*utils.BackgroundTask, 0)
		for task := range r.bgTasks {
			select {
			case <-task.Done():
				// 已完成，从集合中移除
				delete(r.bgTasks, task)
			default:
				pending = append(pending, task)
			}
		}

		if len(pending) > 0 {
			// Python: if timeout is None: timeout = self._get_evolution_total_timeout_secs()
			effectiveTimeout := timeout
			if effectiveTimeout == nil {
				timeoutSecs := r.ext.GetEvolutionTotalTimeoutSecs()
				if timeoutSecs > 0 {
					d := time.Duration(timeoutSecs * float64(time.Second))
					effectiveTimeout = &d
				}
			}

			if effectiveTimeout != nil && *effectiveTimeout > 0 {
				// 精确超时等待
				ctx, cancel := context.WithTimeout(context.Background(), *effectiveTimeout)
				defer cancel()
				for _, task := range pending {
					select {
					case <-task.Done():
						// 任务在超时前完成
					case <-ctx.Done():
						// 超时，停止等待后续任务
						goto collectEvents
					}
				}
			} else {
				// 无超时限制，等待所有任务完成
				for _, task := range pending {
					_ = task.Wait()
				}
			}
			// 清理已完成任务
			for t := range r.bgTasks {
				select {
				case <-t.Done():
					delete(r.bgTasks, t)
				default:
				}
			}
		}
	}

collectEvents:
	drainedList := r.collectPendingHostEvents()

	logger.Debug(logComponent).
		Int("drained_count", len(drainedList)).
		Msg("DrainPendingHostEvents completed")

	return drainedList
}

// DrainPendingApprovalEvents 兼容别名，返回并清空主机事件缓冲。
// Python: EvolutionRail.drain_pending_approval_events(wait, timeout)
func (r *EvolutionRail) DrainPendingApprovalEvents(wait bool, timeout *time.Duration) []*stream.OutputSchema {
	return r.DrainPendingHostEvents(wait, timeout)
}

// CleanupBackgroundTasks 取消并清空所有后台任务。
// Python: EvolutionRail.cleanup_background_tasks()
func (r *EvolutionRail) CleanupBackgroundTasks() error {
	for task := range r.bgTasks {
		select {
		case <-task.Done():
			// 任务已完成
		default:
			_ = task.Stop(5 * time.Second)
		}
		delete(r.bgTasks, task)
	}
	return nil
}

// GetCallbacks 合并 DeepAgent 基础事件 + Evolution 扩展事件的回调映射。
// 注册 5 个事件：BeforeInvoke / AfterModelCall / AfterToolCall / AfterInvoke / AfterTaskIteration。
// Python: EvolutionRail 注册 before_invoke / after_model_call / after_tool_call / after_invoke / after_task_iteration
func (r *EvolutionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterInvoke] = func(ctx context.Context, railCtx any) error {
		return r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.AfterTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// BeforeInvoke 在每次 invoke 开始时初始化轨迹收集。
// Final 回调——子类不应覆写，通过 ext.OnBeforeInvoke 扩展。
// Python: EvolutionRail.before_invoke(ctx)
func (r *EvolutionRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok {
		return nil
	}

	sessionID := r.resolveSessionID(cbc, inputs)
	if sessionID == "" {
		// 偏差 D-25.2b-9：Go harness 在 BeforeInvoke 时会话 ID 可为空（旧实现
		// 容忍空串）；上游 offline/builder.py 校验 session_id 必填。此处回退
		// "unknown" 保持收集不中断，回收条件=无。
		sessionID = "unknown"
	}

	// Python: 复用同 session 的 builder
	if r.builder != nil && r.BuilderSessionID() == sessionID {
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Msg("reusing existing trajectory builder")
		return r.ext.OnBeforeInvoke(ctx, cbc)
	}

	// Python: 捕获 member_id 用于团队轨迹聚合
	memberID := ""
	if agent := cbc.Agent(); agent != nil {
		if card := agent.Card(); card != nil {
			memberID = card.GetID()
		}
	}

	// 构建 meta
	meta := map[string]any{}
	if r.memberRole != "" {
		meta["member_role"] = r.memberRole
	}

	maxSpans := 0 // 0 = 无上限（对齐 Python max_trajectory_spans 默认 None）
	if r.maxTrajectorySteps > 0 {
		maxSpans = r.maxTrajectorySteps
	}
	var memberIDPtr *string
	if memberID != "" {
		memberIDPtr = &memberID
	}
	builder, buildErr := trajectory.NewTrajectoryBuilder(sessionID, "online",
		nil, memberIDPtr, nil, meta, maxSpans, nil, nil)
	if buildErr != nil {
		logger.Error(logComponent).Str("session_id", sessionID).Err(buildErr).
			Msg("trajectory builder construction failed")
		return buildErr
	}
	r.builder = builder
	r.beginScopeCapture(sessionID, memberID, r.teamID)

	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Str("member_id", memberID).
		Str("member_role", r.memberRole).
		Msg("creating new trajectory builder")

	return r.ext.OnBeforeInvoke(ctx, cbc)
}

// AfterModelCall 在每次模型调用后记录 LLM 步骤并触发演化扩展点。
// Final 回调——子类不应覆写，通过 ext.OnAfterModelCall 扩展。
// Python: EvolutionRail.after_model_call(ctx)
func (r *EvolutionRail) AfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.builder == nil {
		return nil
	}

	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}

	// 12-04 span 模型迁移：LLM 步骤构造为规范 GenAI span（WriteLLMExchange 属性），
	// 经 builder.RecordSpan 累积（等价 Python 的 span 采集面）。
	// Python: evolution_rail.py after_model_call 的 window 增量收集语义。
	if len(inputs.Messages) > 0 || inputs.Response != nil {
		modelName := "unknown"
		if cbc.Agent() != nil {
			if config := cbc.Agent().Config(); config != nil {
				if m := config.ModelName(); m != "" {
					modelName = m
				}
			}
		}

		responseDict, promptTokenIDs, completionTokenIDs, logprobs := splitResponseTokenFields(inputs.Response)
		if responseDict == nil {
			responseDict = map[string]any{}
		}

		prompts := make([]map[string]any, 0, len(inputs.Messages))
		for _, msg := range inputs.Messages {
			if msgMap := llmschema.BaseMessageToMap(msg); msgMap != nil {
				prompts = append(prompts, msgMap)
			}
		}
		// completion：响应消息（response dict 或首条回复文本）。
		completions := []map[string]any{{"role": "assistant", "content": responseToTextAttr(responseDict)}}

		attrs := map[string]any{
			"gen_ai.operation.name": "chat",
			"gen_ai.request.model":  modelName,
		}
		for key, value := range trajectory.WriteLLMExchange(prompts, completions) {
			attrs[key] = value
		}
		if usage, ok := responseDict["usage"].(map[string]any); ok {
			for _, pair := range []struct{ key, attr string }{
				{"prompt_tokens", "gen_ai.usage.input_tokens"},
				{"completion_tokens", "gen_ai.usage.output_tokens"},
			} {
				if v, exists := usage[pair.key]; exists {
					attrs[pair.attr] = v
				}
			}
		}
		if len(promptTokenIDs) > 0 {
			attrs[trajectory.RLPromptTokenIDs] = promptTokenIDs
		}
		if len(completionTokenIDs) > 0 {
			attrs[trajectory.RLCompletionTokenIDs] = completionTokenIDs
		}
		if len(logprobs) > 0 {
			attrs[trajectory.RLLogprobs] = logprobs
		}
		if len(inputs.Tools) > 0 {
			tools := make([]any, 0, len(inputs.Tools))
			for _, tool := range inputs.Tools {
				tools = append(tools, toolInfoToMap(tool))
			}
			attrs["gen_ai.tool.definitions"] = tools
		}

		errRecord := r.builder.RecordSpan(map[string]any{
			"name":       fmt.Sprintf("chat %s", modelName),
			"attributes": attrs,
		})
		if errRecord != nil {
			logger.Warn(logComponent).Err(errRecord).Msg("llm span record failed")
		}
	} else {
	}

	// Python: after_model_call 以 merge=False drain，随后把 enrich 后的
	// increment 合并回 scope 窗口（evolution_rail.py:400-407）——
	// 消费 + 回并缺一不可：只消费不合并会让窗口丢失全部 llm span
	//（15.24a 集成实证，main 既有红根因之一）。
	window, increment, issues, derr := r.drainForHook(r.currentCapture, "llm", false)
	if derr != nil {
		logger.Warn(logComponent).Err(derr).Msg("scope window drain failed")
	}
	logCaptureIssues(issues)
	if r.currentCapture != nil && increment != nil && len(issues) == 0 {
		// Go 的 token 字段在记录时已挂 span（inputs.Response 就地可得），
		// Python 的 _enrich_latest_llm 后处理等价物无需再补。
		if merged, merr := r.mergeAdditions(r.currentCapture,
			trajectory.IterSpans(increment.ToOTLP())); merr == nil {
			window = merged
		} else {
			logger.Warn(logComponent).Err(merr).Msg("llm increment window merge failed")
		}
	}

	// Python: 触发扩展点
	if err := r.ext.OnAfterModelCall(ctx, cbc); err != nil {
		return err
	}

	// Python: if trigger == AFTER_MODEL_CALL and _allow_evolution_trigger → _trigger_evolution
	if r.evolutionTrigger == TriggerAfterModelCall &&
		r.ext.AllowEvolutionTrigger(TriggerAfterModelCall, cbc) {
		if window != nil {
			return r.triggerEvolution(ctx, window, cbc)
		}
	}

	return nil
}

// AfterToolCall 在每次工具调用后记录 Tool 步骤并触发演化扩展点。
// Final 回调——子类不应覆写，通过 ext.OnAfterToolCall 扩展。
// Python: EvolutionRail.after_tool_call(ctx)
func (r *EvolutionRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.builder == nil {
		logger.Debug(logComponent).Msg("after_tool_call skipped: trajectory builder is nil")
		return nil
	}

	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	// 12-04 span 模型迁移：工具步骤构造为规范 GenAI 工具 span。
	if inputs.ToolName != "" {
		attrs := map[string]any{
			"gen_ai.operation.name":      "execute_tool",
			"gen_ai.tool.name":           inputs.ToolName,
			"gen_ai.tool.call.arguments": argsToJSONAttr(inputs.ToolArgs),
			"gen_ai.tool.call.result":    argsToJSONAttr(inputs.ToolResult),
			"gen_ai.tool.call.id":        "",
		}
		if err := r.builder.RecordSpan(map[string]any{
			"name":       fmt.Sprintf("execute_tool %s", inputs.ToolName),
			"attributes": attrs,
		}); err != nil {
			logger.Warn(logComponent).Err(err).Msg("tool span record failed")
		}
	}

	// 12-04：scope 窗口增量（D-25.2b-11 collector 源）。
	// 2026-11-21 审计修复：改经 drainForHook（required="tool"）。
	window, _, issues, derr := r.drainForHook(r.currentCapture, "tool", true)
	if derr != nil {
		logger.Warn(logComponent).Err(derr).Msg("scope window drain failed")
	}
	logCaptureIssues(issues)

	// Python: 触发扩展点
	if err := r.ext.OnAfterToolCall(ctx, cbc); err != nil {
		return err
	}

	// Python: if trigger == AFTER_TOOL_CALL → _trigger_evolution
	if r.evolutionTrigger == TriggerAfterToolCall &&
		r.ext.AllowEvolutionTrigger(TriggerAfterToolCall, cbc) {
		if window != nil {
			return r.triggerEvolution(ctx, window, cbc)
		}
	}

	return nil
}

// AfterTaskIteration 在每次任务循环迭代后触发扩展点。
// Python: EvolutionRail.after_task_iteration(ctx)
func (r *EvolutionRail) AfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 2026-11-21 审计修复：最终 drain（Python after_task_iteration 的 _drain_for_hook）。
	window, _, issues, derr := r.drainForHook(r.currentCapture, "", true)
	if derr != nil {
		logger.Warn(logComponent).Err(derr).Msg("scope window drain failed")
	}
	logCaptureIssues(issues)

	// Python: await self._on_after_task_iteration(ctx)
	if err := r.ext.OnAfterTaskIteration(ctx, cbc); err != nil {
		return err
	}

	// Python: if trigger == AFTER_TASK_ITERATION → _trigger_evolution
	if r.evolutionTrigger == TriggerAfterTaskIteration &&
		r.ext.AllowEvolutionTrigger(TriggerAfterTaskIteration, cbc) {
		if window != nil {
			return r.triggerEvolution(ctx, window, cbc)
		}
	}

	return nil
}

// AfterInvoke 在每次 invoke 结束后保存轨迹并触发演化。
// Final 回调——子类不应覆写，通过 ext.OnAfterInvoke 扩展。
// Python: EvolutionRail.after_invoke(ctx)
func (r *EvolutionRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.builder == nil {
		return nil
	}
	// 2026-11-21 审计修复（17.6 基座缺口 1-6）：对齐 Python after_invoke——
	// 最终 drain（gate 生效）→ ext 扩展点（TrajectoryRail 归档挂点）→
	// 触发（trajectory 非 nil 才触发）→ finally 释放订阅。
	// Python 基类 after_invoke 不落 store（归档属 TrajectoryRail 覆写），
	// 原 Go 无条件 saveTrajectory 为结构漂移，已删除。
	defer r.endScopeCapture(r.currentCapture)

	window, _, issues, derr := r.drainForHook(r.currentCapture, "", true)
	if derr != nil {
		logger.Warn(logComponent).Err(derr).Msg("scope window final drain failed")
	}
	logCaptureIssues(issues)

	// Python: await self._on_after_invoke(ctx)
	if err := r.ext.OnAfterInvoke(ctx, cbc); err != nil {
		return err
	}

	// Python: if trigger == AFTER_INVOKE and _allow_evolution_trigger:
	//     if trajectory is not None: await _trigger_evolution + _on_after_evolution_triggered
	if r.evolutionTrigger == TriggerAfterInvoke &&
		r.ext.AllowEvolutionTrigger(TriggerAfterInvoke, cbc) {
		if window != nil {
			if err := r.triggerEvolution(ctx, window, cbc); err != nil {
				return err
			}
			if err := r.ext.OnAfterEvolutionTriggered(ctx, window, cbc); err != nil {
				return err
			}
		}
	}

	return nil
}

// resolveSessionID 解析运行时 session ID。
// Python: _resolve_trajectory_session_id(ctx, inputs)
// Python 实现：从 ctx.session 获取 session_id，回退到 inputs.conversation_id
func (r *EvolutionRail) resolveSessionID(cbc *agentinterfaces.AgentCallbackContext, inputs *agentinterfaces.InvokeInputs) string {
	sess := cbc.Session()
	if sess != nil {
		if sid := sess.GetSessionID(); sid != "" {
			return sid
		}
	}
	if inputs != nil && inputs.ConversationID != "" {
		return inputs.ConversationID
	}
	return ""
}

// buildTrajectory 从 builder 构建轨迹并 snapshot steps。
// Python: _build_trajectory()
// Python 实现：trajectory = self._builder.build(); trajectory.steps = list(trajectory.steps)
func (r *EvolutionRail) buildTrajectory() *trajectory.Trajectory {
	if r.builder == nil {
		return nil
	}
	traj, err := r.builder.Build()
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("trajectory build failed")
		return nil
	}
	return traj
}

// BuilderSessionID 返回当前 builder 的会话标识（无 builder 返回空）。
func (r *EvolutionRail) BuilderSessionID() string {
	if r.builder == nil {
		return ""
	}
	return r.builder.SessionID()
}

// ──────────────────────────── 非导出函数（drain/gate 链） ────────────────────────────

// drainForHook 排空一次增量并应用 clean-window quality gate。
// Python: evolution_rail.py:777 _drain_for_hook。
// 返回 (窗口轨迹, 增量轨迹, 质量问题清单, 错误)；gate 生效时窗口为 nil。
// 偏差登记（D-25.2b-11 既有拍板的延伸）：增量源=builder 游标而非
// processor.Drain 订阅——processor 转换 issues 不接入，quality issues 由
// captureQualityIssues 等价实现；回收条件同 D-25.2b-11。
func (r *EvolutionRail) drainForHook(capture *invokeCapture, requiredCategory string,
	merge bool) (*trajectory.Trajectory, *trajectory.Trajectory, []map[string]any, error) {
	if capture == nil {
		return nil, nil, nil, nil
	}
	additions, err := r.collectScopeIncrement(capture)
	if err != nil {
		return nil, nil, nil, err
	}
	increment, err := incrementFromAdditions(additions)
	if err != nil {
		return nil, nil, nil, err
	}
	var issues []map[string]any
	if increment != nil {
		// Python: self._record_execution_increment(capture, increment)
		r.recordExecutionIncrement(increment)
		// Python: issues = tuple(issues) + self._capture_quality_issues(increment)
		issues = captureQualityIssues(increment)
	}
	// Python: RSITrajectoryRail._drain_for_hook 覆写收集 capture_issues——
	// Go 经可选观察接口在 gate 判定前回调（含空清单）。
	if observer, ok := r.ext.(CaptureIssueObserver); ok {
		observer.ObserveCaptureIssues(issues)
	}
	if len(issues) > 0 {
		// Python: if issues: return None, increment, issues——clean-window gate。
		return nil, increment, issues, nil
	}
	if requiredCategory != "" {
		// Python: 增量中不存在对应类别 ended span → missing_required_span
		// （捕获质量失败，非业务错误）。
		hasRequired := false
		if increment != nil {
			for _, span := range trajectory.IterSpans(increment.ToOTLP()) {
				if trajectory.SpanCategory(span) == requiredCategory {
					hasRequired = true
					break
				}
			}
		}
		if !hasRequired {
			return nil, increment, []map[string]any{
				{"code": "missing_required_span", "category": requiredCategory},
			}, nil
		}
	}
	if increment != nil && merge {
		window, err := r.mergeAdditions(capture, additions)
		if err != nil {
			return nil, increment, nil, err
		}
		return window, increment, nil, nil
	}
	// Python: return self._project_window(capture), None-or-increment, ()
	projection, err := r.projectScopeWindow(capture)
	if err != nil {
		return nil, increment, nil, err
	}
	return projection, increment, nil, nil
}

// captureQualityIssues 报告 processor 无法推断的解析/连续性失败。
// Python: evolution_rail.py:754 _capture_quality_issues——tool span 的
// arguments/result 形似 JSON（[/{ 开头）时必须可解析，否则记
// tool_payload_json_error。
func captureQualityIssues(traj *trajectory.Trajectory) []map[string]any {
	if traj == nil {
		return nil
	}
	var issues []map[string]any
	for _, span := range trajectory.IterSpans(traj.ToOTLP()) {
		if trajectory.SpanCategory(span) != "tool" {
			continue
		}
		attrs := trajectory.SpanAttributes(span)
		for _, key := range []string{
			obs.GenAIToolCallArguments,
			obs.GenAIToolCallResult,
		} {
			value, ok := attrs[key].(string)
			if !ok {
				continue
			}
			trimmed := strings.TrimSpace(value)
			if trimmed == "" || (trimmed[0] != '[' && trimmed[0] != '{') {
				continue
			}
			var probe any
			if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
				issues = append(issues, map[string]any{
					"code": "tool_payload_json_error", "attribute": key,
				})
			}
		}
	}
	return issues
}

// logCaptureIssues 记录采集质量问题（Python gate 丢弃轨迹的可见性日志）。
func logCaptureIssues(issues []map[string]any) {
	for _, issue := range issues {
		code, _ := issue["code"].(string)
		logger.Warn(logComponent).
			Str("issue_code", code).
			Msg("trajectory capture quality issue; increment excluded from clean window")
	}
}

// recordExecutionIncrement 执行增量记录扩展点（基类不归档）。
// Python: evolution_rail.py:816 _record_execution_increment——基类空实现，
// TrajectoryRail 覆写归档；Go 经可选接口 ExecutionIncrementRecorder 分派
// （偏差登记：Python 方法覆写 → Go 可选接口断言，与 Final 回调设计同源）。
func (r *EvolutionRail) recordExecutionIncrement(increment *trajectory.Trajectory) {
	if recorder, ok := r.ext.(ExecutionIncrementRecorder); ok {
		recorder.RecordExecutionIncrement(increment)
	}
}

// triggerEvolution 根据配置异步或同步触发演化。
// Python: _trigger_evolution(trajectory, ctx)
// Python 实现：
//   - 异步模式：快照 → 创建后台任务 → _safe_run_evolution
//   - 同步模式：直接调用 run_evolution(trajectory, ctx)
func (r *EvolutionRail) triggerEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.asyncEvolution {
		// Python: Phase 1 — 同步捕获快照
		snapshot := r.ext.SnapshotForEvolution(ctx, traj, cbc)
		if snapshot == nil {
			return nil
		}

		// Python: Phase 2 — 启动后台任务
		skillName := formatSkillName(snapshot)
		bgTask, err := utils.CreateBackgroundTask(
			ctx,
			func(bgCtx context.Context) error {
				return r.safeRunEvolution(bgCtx, snapshot)
			},
			"evolution-"+skillName, "evolution",
		)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to create evolution background task")
			return err
		}
		r.bgTasks[bgTask] = true

		// Python: 清理已完成任务
		for t := range r.bgTasks {
			select {
			case <-t.Done():
				delete(r.bgTasks, t)
			default:
				// 任务仍在运行
			}
		}
	} else {
		// Python: 同步模式 — 对齐 Python 双层异常保护，也走 safeRunEvolution
		// Python 的 sync 路径由 run_evolution 内部 try/except（静默捕获）+
		// _safe_run_evolution 外部 try/except（emit 事件）共同保护
		snapshot := r.ext.SnapshotForEvolution(ctx, traj, cbc)
		if snapshot == nil {
			return nil
		}
		return r.safeRunEvolution(ctx, snapshot)
	}
	return nil
}

// safeRunEvolution 后台安全执行演化（信号量 + 异常捕获）。
// Python: _safe_run_evolution(snapshot)
// Python 实现：
//   - 获取 evolution_sem 信号量
//   - 调用 run_evolution(trajectory, ctx=None, snapshot=snapshot)
//   - 捕获异常 → _emit_background_outcome_event
func (r *EvolutionRail) safeRunEvolution(ctx context.Context, snapshot *EvolutionSnapshot) error {
	// Python: total_timeout = self._get_evolution_total_timeout_secs()
	timeoutSecs := r.ext.GetEvolutionTotalTimeoutSecs()
	if timeoutSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSecs*float64(time.Second)))
		defer cancel()
	}

	// Python: async with self._evolution_sem
	select {
	case r.evolutionSem <- struct{}{}:
		defer func() { <-r.evolutionSem }()
	case <-ctx.Done():
		logger.Warn(logComponent).Err(ctx.Err()).Msg("safeRunEvolution: semaphore acquisition canceled")
		return ctx.Err()
	}

	traj := snapshot.Trajectory
	err := r.ext.RunEvolution(ctx, traj, snapshot)
	if err != nil {
		// Python: except TimeoutError → outcome = {"status": "timed_out", "message": f"background evolution timed out after {timeout}s"}
		if ctx.Err() == context.DeadlineExceeded {
			timeoutText := formatTimeoutSecs(timeoutSecs)
			outcome := map[string]string{
				"status":  "timed_out",
				"message": "background evolution timed out after " + timeoutText + "s",
			}
			logger.Warn(logComponent).Str("timeout_secs", timeoutText).Msg("Background evolution execution timed out")
			r.emitBackgroundOutcomeEvent(outcome)
			return err
		}
		// Python: except Exception as exc → outcome = {"status": "failed", "message": str(exc)}
		outcome := map[string]string{
			"status":  "failed",
			"message": err.Error(),
		}
		logger.Warn(logComponent).Err(err).Msg("Background evolution execution failed")
		r.emitBackgroundOutcomeEvent(outcome)
	}
	return err
}

// emitBackgroundOutcomeEvent 将后台执行结果写入主机事件缓冲。
// Python: _emit_background_outcome_event(outcome)
// Python 实现：构建 EvolutionHostEventMeta + OutputSchema → emit_host_event
func (r *EvolutionRail) emitBackgroundOutcomeEvent(outcome map[string]string) {
	status := outcome["status"]
	if status == "" {
		status = "unknown"
	}
	meta := EvolutionHostEventMeta{
		EventKind:  EvolutionEventKindOutcome,
		RailKind:   stringPtr(outcome["rail_kind"]),
		Stage:      stringPtr(outcome["stage"]),
		SkillName:  stringPtr(outcome["skill_name"]),
		RequestID:  stringPtr(outcome["request_id"]),
		SignalType: stringPtr(outcome["signal_type"]),
		Source:     stringPtr(outcome["source"]),
		Status:     &status,
	}

	// Python: content = f"[Evolution] {outcome['message']}\n"
	message := outcome["message"]
	if message == "" {
		message = "background evolution completed with unknown outcome"
	}

	r.EmitHostEvent(&stream.OutputSchema{
		Type:  "llm_reasoning",
		Index: 0,
		Payload: map[string]any{
			"content":         "[Evolution] " + message + "\n",
			"_evolution_meta": meta.ToPayload(),
		},
	})
}

// collectPendingHostEvents 返回并清空主机事件缓冲。
// Python: _collect_pending_host_events()
func (r *EvolutionRail) collectPendingHostEvents() []*stream.OutputSchema {
	events := r.pendingHostEvents
	r.pendingHostEvents = make([]*stream.OutputSchema, 0)
	return events
}

// resetTrajectoryBuilder 重置当前轨迹构造器。
// Python: _reset_trajectory_builder()
// Python 注释：子类在自身生命周期边界使用此方法，例如上传 RL episode 后。
func (r *EvolutionRail) resetTrajectoryBuilder() {
	r.builder = nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringPtr 返回字符串指针，空字符串返回 nil。
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isSkillDisabled 检查技能是否被禁用。
// Python: skill_name in self._disabled_skills
func (r *EvolutionRail) isSkillDisabled(skillName string) bool {
	return r.disabledSkills[skillName]
}

// getAgentIDStr 从回调上下文获取 agent ID 字符串。
func getAgentIDStr(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil || cbc.Agent() == nil {
		return "unknown"
	}
	if card := cbc.Agent().Card(); card != nil {
		return card.GetID()
	}
	return "unknown"
}

// formatTimeoutSecs 将超时秒数格式化为字符串，去除尾部无意义的零。
// Python: f"{total_timeout:.2f}".rstrip("0").rstrip(".")
func formatTimeoutSecs(secs float64) string {
	s := fmt.Sprintf("%.2f", secs)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// responseToTextAttr 从响应字典提取回复文本（LLM span completion 属性）。
// 12-04 注记：Python 侧 window 增量收集由观测链完成；Go 在 rail 内手工
// 构造 span（collector 风格，与 Python offline/builder.py 的 write_llm_exchange
// 路径同一套属性 schema）。
func responseToTextAttr(response map[string]any) string {
	if response == nil {
		return ""
	}
	if choices, ok := response["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if content, ok := message["content"].(string); ok {
					return content
				}
			}
		}
	}
	if content, ok := response["content"].(string); ok {
		return content
	}
	return ""
}

// argsToJSONAttr 参数/结果 → JSON 字符串属性（工具 span 约定）。
func argsToJSONAttr(value any) string {
	if value == nil {
		return ""
	}
	if m, ok := value.(map[string]any); ok && len(m) == 0 {
		return "{}"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

// DefaultMemberRole 返回默认成员角色（测试观测面）。
func (r *EvolutionRail) DefaultMemberRole() string { return r.defaultMemberRole }

// SetExtension 换装扩展点实现（TrajectoryRail/RSITrajectoryRail ext 包装挂载用；
// 跨包导出——RSI evaluator 子包构造时换装）。
func (r *EvolutionRail) SetExtension(ext EvolutionExtension) { r.ext = ext }

// GetExtension 读取当前扩展点实现。
func (r *EvolutionRail) GetExtension() EvolutionExtension { return r.ext }

// TrajectoryToMessages 基类级轨迹消息投影（子类跨包访问入口）。
// Python: EvolutionRail._trajectory_to_messages（evolution_rail.py:1008）——
// Python 子类经继承访问；Go 跨包（rails/skills 子 rail）以导出方法承载同一归属。
func (r *EvolutionRail) TrajectoryToMessages(traj *trajectory.Trajectory) []map[string]any {
	return trajectoryToMessages(traj)
}

// SetBuilder 注入轨迹组装器（跨包子类的测试注入缝）。
// Python: 测试同模块白盒写 rail._trajectory；Go 白盒访问不跨包，以导出
// setter 承载同语义（规则 12.3 偏差：Python 无对应公共方法）。
func (r *EvolutionRail) SetBuilder(builder *trajectory.TrajectoryBuilder) {
	r.builder = builder
}
