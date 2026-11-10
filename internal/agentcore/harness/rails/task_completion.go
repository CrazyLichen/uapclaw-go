package rails

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	goaltools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/goal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCompletionRail 任务完成护栏，检测 LLM 输出中的完成承诺标签并通知评估器。
// Python: TaskCompletionRail (task_completion_rail.py)
type TaskCompletionRail struct {
	DeepAgentRail
	// taskInstruction 带 {query} 占位符的格式模板，首轮迭代时应用到查询
	taskInstruction string
	// completionPromise 模型须在 <promise>...</promise> 标签内输出以宣告完成的令牌
	completionPromise string
	// requiredConfirmations 需要连续确认的次数
	requiredConfirmations int
	// allowPromiseDetails 是否允许 promise 块包含额外详情
	allowPromiseDetails bool
	// maxRounds 外层循环最大轮数（0 = 不限）
	maxRounds int
	// timeoutSeconds 整个任务循环的墙钟超时秒数（0 = 不限）
	timeoutSeconds float64
	// extraEvaluators 额外自定义评估器
	extraEvaluators []task_loop.StopConditionEvaluator
	// goalManager Goal 能力面（Start 后经 SetGoalManager 注入）
	// Python: self._goal_manager (task_completion_rail.py L132)
	goalManager *goal.GoalManager
	// goalLanguage goal 提示词语言
	// Python: self.goal_language (L133)
	goalLanguage string
	// goalReportSink 本轮尝试报告暂存
	// Python: self._goal_report_sink (L134)
	goalReportSink *goaltools.GoalReportSink
	// goalEvaluator 完成度评估器
	// Python: self._goal_evaluator (L135)
	goalEvaluator *goal.GoalEvaluator
	// goalTools 已注册的 goal 工具
	// Python: self._goal_tools (L136)
	goalTools []tool.Tool
	// isGoalRound 本轮是否为 goal 轮
	// Python: self._is_goal_round (L137)
	isGoalRound bool
	// currentGoalID / currentRevision / currentSessionID 本轮 goal 身份
	// Python: self._current_goal_id/_current_revision/_current_session_id (L138-140)
	currentGoalID    string
	currentRevision  int
	currentSessionID string
	// currentAttemptMessages 本轮捕获的模型上下文
	// Python: self._current_attempt_messages (L141)
	currentAttemptMessages []llmschema.BaseMessage
	// attachmentManager 会话级附件管理器（Init 中从 agent 取一次）
	// Python: self.attachment_manager (L142)
	attachmentManager *hprompts.Manager
}

// goalManagerProvider 提供 Goal 能力面的 Agent 能力。
// 由 harness.DeepAgent 实现（GoalManager 访问器）。
// Python: init() 里 getattr(agent, "goal_manager", None) 的继承兜底（task_completion_rail.py L238-241）
type goalManagerProvider interface {
	// GoalManager 返回会话级 Goal 能力面（未注入时 nil）
	GoalManager() *goal.GoalManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// TaskCompletionOption TaskCompletionRail 可选配置函数
type TaskCompletionOption func(*TaskCompletionRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskCompletionRailPriority TaskCompletionRail 优先级
	// Python: TaskCompletionRail.priority = 10
	taskCompletionRailPriority = 10
	// attemptContextMaxChars transcript assessor 输入上限，超限保留尾部（最新窗口）。
	// Python: _ATTEMPT_CONTEXT_MAX_CHARS = 64_000 (task_completion_rail.py L64)
	attemptContextMaxChars = 64000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证 TaskCompletionRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*TaskCompletionRail)(nil)

var promiseTagPattern = regexp.MustCompile(`(?i)(?s)<promise>\s*(.*?)\s*</promise>`)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTaskInstruction 设置任务指令模板，首轮迭代时应用到查询
func WithTaskInstruction(template string) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.taskInstruction = template }
}

// WithCompletionPromise 设置完成承诺令牌，模型须输出 <promise>令牌</promise> 宣告完成
func WithCompletionPromise(promise string) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.completionPromise = promise }
}

// WithRequiredConfirmations 设置需要连续确认的次数（最小为 1）
func WithRequiredConfirmations(n int) TaskCompletionOption {
	return func(r *TaskCompletionRail) {
		if n < 1 {
			n = 1
		}
		r.requiredConfirmations = n
	}
}

// WithAllowPromiseDetails 设置是否允许 promise 块包含额外详情
func WithAllowPromiseDetails(allow bool) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.allowPromiseDetails = allow }
}

// WithMaxRounds 设置外层循环最大轮数（0 = 不限）
func WithMaxRounds(n int) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.maxRounds = n }
}

// WithTimeoutSeconds 设置整个任务循环的墙钟超时秒数（0 = 不限）
func WithTimeoutSeconds(sec float64) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.timeoutSeconds = sec }
}

// WithExtraEvaluators 设置额外自定义评估器
func WithExtraEvaluators(evaluators ...task_loop.StopConditionEvaluator) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.extraEvaluators = evaluators }
}

// WithGoalLanguage 设置 goal 提示词语言（cn/en）。
// Python: TaskCompletionRail.__init__ 的 goal_language 参数 (task_completion_rail.py L116)
func WithGoalLanguage(lang string) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.goalLanguage = lang }
}

// NewTaskCompletionRail 创建任务完成护栏实例。
// Python: TaskCompletionRail.__init__()
func NewTaskCompletionRail(opts ...TaskCompletionOption) *TaskCompletionRail {
	r := &TaskCompletionRail{
		DeepAgentRail:         *NewDeepAgentRail(),
		requiredConfirmations: 1,
		goalLanguage:          "cn",
	}
	for _, opt := range opts {
		opt(r)
	}
	r.WithPriority(taskCompletionRailPriority)
	return r
}

// BuildEvaluators 构建停止条件评估器列表
func (r *TaskCompletionRail) BuildEvaluators() []task_loop.StopConditionEvaluator {
	var result []task_loop.StopConditionEvaluator
	if r.maxRounds > 0 {
		result = append(result, task_loop.NewMaxRoundsEvaluator(r.maxRounds))
	}
	if r.timeoutSeconds > 0 {
		result = append(result, task_loop.NewTimeoutEvaluator(r.timeoutSeconds))
	}
	if r.completionPromise != "" {
		result = append(result, task_loop.NewCompletionPromiseEvaluator(
			r.completionPromise, r.requiredConfirmations,
		))
	}
	result = append(result, r.extraEvaluators...)
	return result
}

// SetGoalManager 注入 GoalManager 并惰性建 sink/evaluator。
// Python: TaskCompletionRail.set_goal_manager (task_completion_rail.py L169)
func (r *TaskCompletionRail) SetGoalManager(m *goal.GoalManager) {
	r.goalManager = m
	if r.goalReportSink == nil {
		r.goalReportSink = goaltools.NewGoalReportSink()
	}
	if r.goalEvaluator == nil {
		r.goalEvaluator = goal.NewGoalEvaluator(nil)
	}
}

// SetGoalLanguage 设置 goal 提示词语言。
// Python: self._task_completion_rail.goal_language = language (deep_agent.py L832)
func (r *TaskCompletionRail) SetGoalLanguage(lang string) {
	r.goalLanguage = lang
}

// GoalReportSink 返回本轮报告暂存（供 submit_goal_report 工具与测试访问）。
// Python: self._goal_report_sink (task_completion_rail.py L134)
func (r *TaskCompletionRail) GoalReportSink() *goaltools.GoalReportSink {
	if r.goalReportSink == nil {
		r.goalReportSink = goaltools.NewGoalReportSink()
	}
	return r.goalReportSink
}

// Init 绑定 goal_manager 后注册 submit_goal_report / get_current_goal 两工具。
// Python: TaskCompletionRail.init (task_completion_rail.py L218)
func (r *TaskCompletionRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	// Python: self.attachment_manager = getattr(agent, "prompt_attachment_manager", None)
	r.attachmentManager = hprompts.ManagerFor(agent)
	// Python: if self._goal_manager is None: 从 agent.goal_manager 继承兜底 (L238-241)
	if r.goalManager == nil {
		if provider, ok := agent.(goalManagerProvider); ok {
			if inherited := provider.GoalManager(); inherited != nil {
				r.SetGoalManager(inherited)
			}
		}
	}
	if r.goalManager == nil {
		return nil
	}
	// 幂等：已注册过 goal 工具则直接返回（Start 绑定后二次 Init 与注册路径会重复调用；
	// ResourceMgr.AddTool 对已存在资源返回 error，AbilityManager.Add 对重名返回 Added=false）。
	if len(r.goalTools) > 0 {
		return nil
	}

	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}
	language := r.resolveGoalLanguage(agent.SystemPromptBuilder())
	r.goalLanguage = language

	sink := r.goalReportSink
	if sink == nil {
		sink = goaltools.NewGoalReportSink()
		r.goalReportSink = sink
	}
	submitTool, err := goaltools.NewSubmitGoalReportTool(sink, language, agentID)
	if err != nil {
		return fmt.Errorf("构建 submit_goal_report 工具失败: %w", err)
	}
	getTool, err := goaltools.NewGetCurrentGoalTool(r.goalManager, language, agentID)
	if err != nil {
		return fmt.Errorf("构建 get_current_goal 工具失败: %w", err)
	}

	tools := []tool.Tool{submitTool, getTool}
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range tools {
		name := t.Card().Name
		if am != nil {
			if result := am.Add(t.Card()); result.Added {
				r.goalTools = append(r.goalTools, t)
			} else {
				logger.Warn(logComponent).
					Str("tool_name", name).
					Msg("failed to add goal tool card to ability_manager")
			}
		} else {
			// 无 ability_manager 时仅注册 ResourceMgr（对齐 Python 打 Warn 但不阻断）
			r.goalTools = append(r.goalTools, t)
			logger.Warn(logComponent).Msg("agent has no ability_manager; goal tools not registered")
		}
		toolID := t.Card().ID
		if resourceMgr != nil && toolID != "" {
			if err := resourceMgr.AddTool(t); err != nil {
				logger.Warn(logComponent).
					Str("tool_id", toolID).
					Err(err).
					Msg("failed to add goal tool resource to resource_mgr")
			}
		}
	}
	logger.Info(logComponent).Msg("TaskCompletionRail: registered submit_goal_report and get_current_goal tools")
	return nil
}

// Uninit 注销 goal 工具。
// Python: TaskCompletionRail.uninit (task_completion_rail.py L274)
func (r *TaskCompletionRail) Uninit(agent agentinterfaces.BaseAgent) error {
	am := agent.AbilityManager()
	if am != nil {
		for _, t := range r.goalTools {
			name := t.Card().Name
			if name == "" {
				continue
			}
			am.Remove(name)
		}
	}
	r.goalTools = nil
	r.attachmentManager = nil
	return nil
}

// BeforeModelCall 注入完成信号提示词节与 goal 协议附件。
func (r *TaskCompletionRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil {
		return nil
	}
	var builder saprompt.SystemPromptBuilderInterface
	if agent := cbc.Agent(); agent != nil {
		builder = agent.SystemPromptBuilder()
	}

	if r.completionPromise != "" && builder != nil {
		lang := builder.Language()
		section := sections.BuildCompletionSignalSection(r.completionPromise, lang)
		// 完成信号节是稳定系统提示词，上游走 add_section
		// Python: builder.add_section(section)（task_completion_rail.py:321）
		builder.AddSection(section)

		logger.Debug(logComponent).
			Str("event_type", "task_completion_before_model_call").
			Str("completion_promise", r.completionPromise).
			Msg("completion signal prompt section injected")
	}

	// Python: await self._sync_goal_protocol_prompt(ctx, builder) (L323)
	r.syncGoalProtocolPrompt(ctx, cbc, builder)
	return nil
}

// BeforeInvoke 阶段化 goal 轮动态上下文。
// Python: TaskCompletionRail.before_invoke (task_completion_rail.py L297)
func (r *TaskCompletionRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil {
		return nil
	}
	var builder saprompt.SystemPromptBuilderInterface
	if agent := cbc.Agent(); agent != nil {
		builder = agent.SystemPromptBuilder()
	}
	r.syncGoalProtocolPrompt(ctx, cbc, builder)
	return nil
}

// BeforeTaskIteration 首轮迭代时格式化任务指令；goal 轮替换 query。
func (r *TaskCompletionRail) BeforeTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil {
		return nil
	}
	// Python: if run_kind == "goal" and goal_manager is not None: _do_goal_before_iteration (L387-390)
	if r.getRunKind(cbc) == string(agentinterfaces.RunKindGoal) && r.goalManager != nil {
		r.doGoalBeforeIteration(cbc)
		return nil
	}

	r.isGoalRound = false

	if r.taskInstruction == "" {
		return nil
	}

	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil {
		return nil
	}
	if inputs.Query == "" {
		return nil
	}
	if inputs.IsFollowUp {
		return nil
	}

	// Python: inputs.query = self.task_instruction.format(query=query)
	inputs.Query = strings.ReplaceAll(r.taskInstruction, "{query}", inputs.Query)

	logger.Debug(logComponent).
		Str("event_type", "task_completion_before_task_iteration").
		Msg("first round task instruction formatted")
	return nil
}

// AfterModelCall 累计 goal 轮 token 用量并捕获本轮模型上下文。
// Python: TaskCompletionRail.after_model_call (task_completion_rail.py L406)
func (r *TaskCompletionRail) AfterModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if !r.isGoalRound || r.goalManager == nil {
		return nil
	}

	if mi, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs); ok && mi != nil {
		r.captureAttemptModelContext(mi)
		if mi.Response != nil && mi.Response.UsageMetadata != nil {
			_ = r.goalManager.AccumulateUsage(
				r.currentGoalID,
				r.currentRevision,
				mi.Response.UsageMetadata.InputTokens,
				mi.Response.UsageMetadata.OutputTokens,
				mi.Response.UsageMetadata.CacheTokens,
			)
		}
	}
	return nil
}

// AfterTaskIteration 检测 LLM 输出中的完成承诺标签；goal 轮消费报告并评估。
func (r *TaskCompletionRail) AfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil {
		return nil
	}
	// Python: if _is_goal_round and goal_manager: _do_goal_after_iteration (L433-435)
	if r.isGoalRound && r.goalManager != nil {
		return r.doGoalAfterIteration(ctx, cbc)
	}

	if r.completionPromise == "" {
		return nil
	}

	content := extractOutput(cbc)
	if content == "" {
		return nil
	}

	promiseBlock := ExtractPromiseBlock(content)
	if promiseBlock == "" {
		return nil
	}

	matched := normalizePromiseText(promiseBlock)
	expected := normalizePromiseText(r.completionPromise)

	if matched != expected {
		if !r.allowPromiseDetails {
			return nil
		}
		if !PromiseMatches(promiseBlock, r.completionPromise) {
			return nil
		}
		matched = expected
	}

	logger.Info(logComponent).
		Str("event_type", "task_completion_promise_fulfilled").
		Str("matched", matched).
		Msg("task completion promise satisfied")

	notifyEvaluator(cbc, matched)
	return nil
}

// GetCallbacks 返回回调映射
func (r *TaskCompletionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.BeforeTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.AfterTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ExtractPromiseBlock 从文本中提取 <promise>...</promise> 标签内的内容
func ExtractPromiseBlock(text string) string {
	if text == "" {
		return ""
	}
	match := promiseTagPattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

// PromiseMatches 判断 promise 块是否匹配预期令牌（允许详情后缀）
func PromiseMatches(block string, expected string) bool {
	if block == "" || expected == "" {
		return false
	}

	expectedNorm := normalizePromiseText(expected)

	// Python: block_lines = [line.strip() for line in block.splitlines() if line.strip()]
	var blockLines []string
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			blockLines = append(blockLines, trimmed)
		}
	}

	firstLine := strings.TrimSpace(block)
	if len(blockLines) > 0 {
		firstLine = blockLines[0]
	}

	firstNorm := normalizePromiseText(firstLine)
	if firstNorm == expectedNorm {
		return true
	}
	return strings.HasPrefix(firstNorm, expectedNorm+" ")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizePromiseText 把完成承诺文本里的连续空白压成一个空格，便于比较。
func normalizePromiseText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// extractOutput 从本轮任务回调的入参里取出结果文本。
func extractOutput(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil {
		return ""
	}
	result := inputs.Result
	if result == nil {
		return ""
	}
	output := result["output"]
	if output == nil {
		return ""
	}
	s, ok := output.(string)
	if !ok {
		return fmt.Sprintf("%v", output)
	}
	return s
}

// notifyEvaluator 把结果文本交给完成承诺评估器判定是否真的完成。
func notifyEvaluator(cbc *agentinterfaces.AgentCallbackContext, text string) {
	agent := cbc.Agent()

	// 通过 DeepAgentInterface 获取 LoopCoordinator
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		logger.Warn(logComponent).
			Str("event_type", "task_completion_notify_evaluator_skip").
			Str("agent_type", fmt.Sprintf("%T", agent)).
			Msg("agent does not implement DeepAgentInterface, cannot notify CompletionPromiseEvaluator")
		return
	}
	coord := deepAgent.LoopCoordinator()
	if coord == nil {
		logger.Warn(logComponent).
			Str("event_type", "task_completion_notify_evaluator_skip").
			Msg("LoopCoordinator is nil, cannot notify CompletionPromiseEvaluator")
		return
	}

	ev := coord.GetCompletionPromiseEvaluator()
	if ev == nil {
		logger.Warn(logComponent).
			Str("event_type", "task_completion_notify_evaluator_skip").
			Msg("CompletionPromiseEvaluator is nil, cannot notify")
		return
	}
	ev.NotifyFulfilled(text)
}

// ──────────────────────────── goal 生命周期（对齐 task_completion_rail.py L462-933） ────────────────────────────

// syncGoalProtocolPrompt 按 run_kind 挂/清 goal_protocol 附件。
// Python: _sync_goal_protocol_prompt (task_completion_rail.py L325)
func (r *TaskCompletionRail) syncGoalProtocolPrompt(_ context.Context, cbc *agentinterfaces.AgentCallbackContext, builder saprompt.SystemPromptBuilderInterface) {
	runKind := r.getRunKind(cbc)
	wantGoal := runKind == string(agentinterfaces.RunKindGoal) && r.goalManager != nil
	language := r.resolveGoalLanguage(builder)

	// Python: :341-347 manager 缺失时仅 goal 轮告警，非 goal 静默
	if r.attachmentManager == nil {
		if wantGoal {
			logger.Warn(logComponent).
				Str("event_type", "task_completion_attachment").
				Msg("prompt attachment manager is unavailable; skip goal protocol attachment")
		}
		return
	}
	writer := r.attachmentManager.BindContext(cbc)
	if writer.SessionID() == "" {
		return
	}

	if wantGoal {
		section := sections.BuildGoalProtocolSection(language)
		if _, err := writer.AddFromPromptSection(
			&section, hprompts.AttachmentRuntime,
			"agent_core.task_completion_rail", language,
			hprompts.WithAttachmentContentKind("text/markdown"),
		); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "task_completion_attachment").
				Err(err).
				Msg("TaskCompletionRail: skip goal protocol prompt attachment")
		}
		return
	}

	// Python: :369-370 非 goal 轮清除协议附件
	if _, err := writer.ClearSection(sections.SectionGoalProtocol); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "task_completion_attachment_clear").
			Err(err).
			Msg("TaskCompletionRail: skip clearing goal protocol prompt attachment")
	}
}

// doGoalBeforeIteration 校验 goal 状态并把 query 替换为 <goal_task> XML。
// Python: _do_goal_before_iteration (task_completion_rail.py L466)
func (r *TaskCompletionRail) doGoalBeforeIteration(cbc *agentinterfaces.AgentCallbackContext) {
	goalID, revision, sessionID := r.getRunContext(cbc)

	manager := r.goalManager
	if manager == nil {
		return
	}
	record := manager.Get()

	if !validateGoalState(record, goalID, revision, false) {
		r.isGoalRound = false
		r.currentAttemptMessages = nil
		return
	}

	r.isGoalRound = true
	r.currentGoalID = goalID
	r.currentSessionID = sessionID
	r.currentRevision = record.Revision

	if r.goalReportSink != nil {
		r.goalReportSink.BeginAttempt(record.SessionID, record.GoalID, record.Revision, record.AttemptCount)
	}

	r.currentAttemptMessages = nil
	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil {
		return
	}
	language := r.goalLanguage
	if agent := cbc.Agent(); agent != nil {
		language = r.resolveGoalLanguage(agent.SystemPromptBuilder())
	}
	r.goalLanguage = language
	inputs.Query = sections.BuildGoalTaskQuery(record.TaskView(), language)
}

// doGoalAfterIteration 消费 goal 报告并运行评估。
// Python: _do_goal_after_iteration (task_completion_rail.py L513)
func (r *TaskCompletionRail) doGoalAfterIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	manager := r.goalManager
	if manager == nil {
		return nil
	}
	record := manager.Get()

	if !validateGoalState(record, r.currentGoalID, r.currentRevision, true) {
		logger.Info(logComponent).Msg("[GoalLifecycle] Goal state invalid after iteration, discarding results")
		return nil
	}

	// 特判 1：Permission / HITL 暂停（result_type=interrupt）跳过评估。
	// Python L538-549
	if resultType := extractResultType(cbc); resultType == "interrupt" {
		logger.Info(logComponent).Msg("[GoalLifecycle] skip assessment on interrupt (HITL/permission pause; attempt not finished)")
		return nil
	}

	// 特判 2：round error 直接 BLOCKED。
	// Python L559-580
	if roundErr := r.extractGoalRoundError(cbc); roundErr != "" {
		logger.Warn(logComponent).
			Str("event_type", "goal_round_error").
			Str("error", truncate(roundErr, 300)).
			Msg("[GoalLifecycle] Goal attempt failed; blocking goal")
		_, err := manager.ApplyAssessment(
			ctx,
			r.currentGoalID,
			r.currentRevision,
			goal.GoalAssessment{
				Status:          goal.GoalAssessmentStatusBlocked,
				Evidence:        "round_execution_error: " + roundErr,
				RemainingWork:   strPtr("Resolve the execution error before resuming the goal."),
				NextInstruction: strPtr("Fix the underlying failure (for example model or API configuration), then resume the goal."),
			},
		)
		return err
	}

	var agentReport *goal.GoalAssessment
	if r.goalReportSink != nil {
		agentReport = r.goalReportSink.Consume()
	}
	transcriptResponse := r.maybeInvokeTranscriptAssessor(ctx, record, agentReport, cbc)
	if r.goalEvaluator == nil {
		logger.Warn(logComponent).Msg("[GoalLifecycle] Goal evaluator unavailable")
		return nil
	}

	assessment := r.goalEvaluator.Assess(record, agentReport, transcriptResponse)
	_, err := manager.ApplyAssessment(ctx, r.currentGoalID, r.currentRevision, assessment)
	return err
}

// extractGoalRoundError 返回本轮抛错信息（无错返回空串）。
// Python: _extract_goal_round_error (task_completion_rail.py L606)
func (r *TaskCompletionRail) extractGoalRoundError(cbc *agentinterfaces.AgentCallbackContext) string {
	exc := cbc.Exception()
	if exc == nil {
		return ""
	}
	text := strings.TrimSpace(exc.Error())
	if text != "" {
		return text
	}
	return fmt.Sprintf("%T", exc)
}

// maybeInvokeTranscriptAssessor 按策略决定是否调监理会诊。
// Python: _maybe_invoke_transcript_assessor (task_completion_rail.py L622)
func (r *TaskCompletionRail) maybeInvokeTranscriptAssessor(ctx context.Context, record *goal.GoalRecord, agentReport *goal.GoalAssessment, cbc *agentinterfaces.AgentCallbackContext) *string {
	if r.goalEvaluator == nil {
		return nil
	}

	switch r.goalEvaluator.Strategy() {
	case goal.GoalStopStrategyAgentReport:
		return nil
	case goal.GoalStopStrategyTranscript:
		return r.invokeTranscriptAssessor(ctx, record, cbc)
	default: // hybrid
		shouldInvoke := agentReport == nil
		if agentReport != nil {
			shouldInvoke = agentReport.Status == goal.GoalAssessmentStatusComplete ||
				agentReport.Status == goal.GoalAssessmentStatusBlocked
			if !shouldInvoke {
				shouldInvoke = r.goalEvaluator.ShouldSpotCheck(record)
			}
		}
		if !shouldInvoke {
			return nil
		}
		return r.invokeTranscriptAssessor(ctx, record, cbc)
	}
}

// invokeTranscriptAssessor 跑一次无工具的 LLM 监理调用。
// Python: _invoke_transcript_assessor (task_completion_rail.py L656)
func (r *TaskCompletionRail) invokeTranscriptAssessor(ctx context.Context, record *goal.GoalRecord, cbc *agentinterfaces.AgentCallbackContext) *string {
	model := r.resolveTranscriptModel(cbc)
	if model == nil {
		logger.Warn(logComponent).Msg("[GoalLifecycle] Transcript assessor skipped: model unavailable")
		return nil
	}

	language := r.goalLanguage
	if agent := cbc.Agent(); agent != nil {
		language = r.resolveGoalLanguage(agent.SystemPromptBuilder())
	}
	r.goalLanguage = language
	systemPrompt := sections.TranscriptAssessorSystem(language)
	userPrompt := sections.BuildTranscriptAssessorPrompt(
		record.Objective,
		sections.BuildGoalCurrentInstruction(record.TaskView(), language),
		r.extractAttemptContext(cbc),
		language,
	)

	response, err := model.Invoke(ctx,
		model_clients.NewMessagesParam(
			llmschema.NewSystemMessage(systemPrompt),
			llmschema.NewUserMessage(userPrompt),
		),
	)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("[GoalLifecycle] Transcript assessor invocation failed")
		return nil
	}
	content := response.Content.Text()
	return &content
}

// resolveTranscriptModel 解析监理模型（经 modelSwitcher.GetLLM，对齐 Python 两级回退的最终结果）。
// Python: _resolve_transcript_model (task_completion_rail.py L719)
func (r *TaskCompletionRail) resolveTranscriptModel(cbc *agentinterfaces.AgentCallbackContext) *llm.Model {
	if sw, ok := cbc.Agent().(modelSwitcher); ok {
		m, err := sw.GetLLM()
		if err == nil && m != nil {
			return m
		}
	}
	return nil
}

// getRunKind 解析 run_kind：inputs.run_kind → ctx.extra["run_kind"]。
// Python: _get_run_kind (task_completion_rail.py L777)
func (r *TaskCompletionRail) getRunKind(cbc *agentinterfaces.AgentCallbackContext) string {
	if ti, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs); ok && ti.RunKind != "" {
		return string(ti.RunKind)
	}
	if inv, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok && inv.RunKind != "" {
		return string(inv.RunKind)
	}
	if raw, ok := cbc.GetExtra("run_kind"); ok {
		if v, ok2 := raw.(string); ok2 {
			return v
		}
	}
	return ""
}

// getRunContext 从迭代输入或 extra 取 goal_id/revision/session_id。
// Python: _get_run_context (task_completion_rail.py L803)
func (r *TaskCompletionRail) getRunContext(cbc *agentinterfaces.AgentCallbackContext) (string, int, string) {
	if ti, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs); ok && ti.RunContext != nil {
		rc := ti.RunContext
		return rc.GoalID, rc.Revision, rc.SessionID
	}
	if inv, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok && inv.RunContext != nil {
		rc := inv.RunContext
		return rc.GoalID, rc.Revision, rc.SessionID
	}
	if raw, ok := cbc.GetExtra("run_context"); ok {
		if rc, ok2 := raw.(*agentinterfaces.RunContext); ok2 {
			return rc.GoalID, rc.Revision, rc.SessionID
		}
	}
	return "", 0, "default"
}

// captureAttemptModelContext 保留最新实际 LLM 请求/响应供评估。
// Python: _capture_attempt_model_context (task_completion_rail.py L877)
func (r *TaskCompletionRail) captureAttemptModelContext(inputs *agentinterfaces.ModelCallInputs) {
	if inputs.Messages == nil {
		return
	}
	captured := make([]llmschema.BaseMessage, len(inputs.Messages))
	copy(captured, inputs.Messages)
	if inputs.Response != nil {
		captured = append(captured, inputs.Response)
	}
	r.currentAttemptMessages = captured
}

// extractAttemptContext 序列化本轮模型上下文（截断保尾部）。
// Python: _extract_attempt_context (task_completion_rail.py L860)
func (r *TaskCompletionRail) extractAttemptContext(cbc *agentinterfaces.AgentCallbackContext) string {
	if len(r.currentAttemptMessages) > 0 {
		return formatContextMessages(r.currentAttemptMessages)
	}
	return ""
}

// resolveGoalLanguage 解析 goal 语言：优先 builder.Language，回退 rail.goalLanguage。
// Python: _resolve_goal_language + _goal_language_from_ctx (task_completion_rail.py L152/L163)
func (r *TaskCompletionRail) resolveGoalLanguage(builder saprompt.SystemPromptBuilderInterface) string {
	raw := ""
	if builder != nil {
		raw = builder.Language()
	}
	if raw == "" {
		raw = r.goalLanguage
	}
	return resolveGoalLang(raw)
}

// ──────────────────────────── goal 辅助（包级） ────────────────────────────

// validateGoalState 校验 record 状态与代际。
// Python: _validate_goal_state (task_completion_rail.py L745)
//
// 对齐 Python Optional 语义：goal_id/revision 同源（要么都来自 run_context，
// 要么都为 None）。goalID 非空才比较 goal_id 与 revision；goalID 为空等价
// Python 的 expected_goal_id=None / expected_revision=None（只查 status）。
// 偏差说明（spec §5）：Python 用 `expected_revision is not None` 独立判断 revision，
// Go 的 int 无法表达 None，故以 goalID 是否为空作为「两者都有值」的开关。当前调用链
// 中 goal_id 与 revision 恒同源于 RunContext（9.87 定义），不会出现「一个有一个无」。
func validateGoalState(record *goal.GoalRecord, expectedGoalID string, expectedRevision int, allowPaused bool) bool {
	if record == nil {
		return false
	}
	allowedActive := record.Status == goal.GoalStatusActive
	allowedPaused := allowPaused && record.Status == goal.GoalStatusPaused
	if !allowedActive && !allowedPaused {
		return false
	}
	if expectedGoalID != "" {
		if record.GoalID != expectedGoalID {
			return false
		}
		if record.Revision != expectedRevision {
			return false
		}
	}
	return true
}

// resolveGoalLang 解析语言为 cn/en，未知回退 cn。
// Python: resolve_language
func resolveGoalLang(lang string) string {
	if lang == "en" {
		return "en"
	}
	return "cn"
}

// extractResultType 从迭代结果取 result_type。
// Python: result.get("result_type") (task_completion_rail.py L543)
func extractResultType(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil || inputs.Result == nil {
		return ""
	}
	s, _ := inputs.Result["result_type"].(string)
	return s
}

// formatContextMessages 把消息列表格式化为评估文本（截断保尾部）。
// Python: _format_context_messages (task_completion_rail.py L892)
func formatContextMessages(messages []llmschema.BaseMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var lines []string
	for i, m := range messages {
		data := messageToAssessmentDict(m)
		if len(data) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%d] %s", i+1, toJSON(data)))
	}
	text := strings.Join(lines, "\n")
	if len(text) <= attemptContextMaxChars {
		return text
	}
	omitted := len(text) - attemptContextMaxChars
	return fmt.Sprintf("...[truncated %d earlier chars]...\n%s", omitted, text[len(text)-attemptContextMaxChars:])
}

// messageToAssessmentDict 把消息转成评估字典。
// Python: _message_to_assessment_dict (task_completion_rail.py L915)
func messageToAssessmentDict(m llmschema.BaseMessage) map[string]any {
	if m == nil {
		return nil
	}
	return llmschema.BaseMessageToMap(m)
}

// toJSON 用 JSON 序列化（对齐 Python json.dumps(ensure_ascii=False, default=str)）。
func toJSON(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(b)
}

// strPtr 返回字符串指针（非空才返回，空串返回 nil）。
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// truncate 截断字符串到 maxLen。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
