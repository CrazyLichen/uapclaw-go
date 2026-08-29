package rails

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCompletionRail 任务完成护栏，检测 LLM 输出中的完成承诺标签并通知评估器。
// 对齐 Python: TaskCompletionRail (task_completion_rail.py)
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
}

// ──────────────────────────── 枚举 ────────────────────────────

// TaskCompletionOption TaskCompletionRail 可选配置函数
type TaskCompletionOption func(*TaskCompletionRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskCompletionRailPriority TaskCompletionRail 优先级
	// 对齐 Python: TaskCompletionRail.priority = 10
	taskCompletionRailPriority = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

var _ agentinterfaces.AgentRail = (*TaskCompletionRail)(nil)

var taskCompLogComponent = logger.ComponentAgentCore

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

// NewTaskCompletionRail 创建任务完成护栏实例。
// 对齐 Python: TaskCompletionRail.__init__()
func NewTaskCompletionRail(opts ...TaskCompletionOption) *TaskCompletionRail {
	r := &TaskCompletionRail{
		DeepAgentRail:         *NewDeepAgentRail(),
		requiredConfirmations: 1,
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

// BeforeModelCall 注入完成信号提示词节
func (r *TaskCompletionRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.completionPromise == "" {
		return nil
	}

	builder := cbc.Agent().SystemPromptBuilder()
	if builder == nil {
		return nil
	}

	lang := builder.Language()
	section := sections.BuildCompletionSignalSection(r.completionPromise, lang)
	builder.AddSection(section)

	logger.Debug(taskCompLogComponent).
		Str("event_type", "task_completion_before_model_call").
		Str("completion_promise", r.completionPromise).
		Msg("已注入完成信号提示词节")
	return nil
}

// BeforeTaskIteration 首轮迭代时格式化任务指令
func (r *TaskCompletionRail) BeforeTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
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

	// 对齐 Python: inputs.query = self.task_instruction.format(query=query)
	inputs.Query = strings.ReplaceAll(r.taskInstruction, "{query}", inputs.Query)

	logger.Debug(taskCompLogComponent).
		Str("event_type", "task_completion_before_task_iteration").
		Msg("已格式化首轮任务指令")
	return nil
}

// AfterTaskIteration 检测 LLM 输出中的完成承诺标签，满足时通知评估器
func (r *TaskCompletionRail) AfterTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
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

	logger.Info(taskCompLogComponent).
		Str("event_type", "task_completion_promise_fulfilled").
		Str("matched", matched).
		Msg("任务完成承诺已满足")

	notifyEvaluator(cbc, matched)
	return nil
}

// GetCallbacks 返回回调映射
func (r *TaskCompletionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
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

	// 对齐 Python: block_lines = [line.strip() for line in block.splitlines() if line.strip()]
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

func normalizePromiseText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

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

func notifyEvaluator(cbc *agentinterfaces.AgentCallbackContext, text string) {
	agent := cbc.Agent()

	// 通过 DeepAgentInterface 获取 LoopCoordinator
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		logger.Warn(taskCompLogComponent).
			Str("event_type", "task_completion_notify_evaluator_skip").
			Str("agent_type", fmt.Sprintf("%T", agent)).
			Msg("agent 未实现 DeepAgentInterface，无法通知 CompletionPromiseEvaluator")
		return
	}
	coord := deepAgent.LoopCoordinator()
	if coord == nil {
		logger.Warn(taskCompLogComponent).
			Str("event_type", "task_completion_notify_evaluator_skip").
			Msg("LoopCoordinator 为 nil，无法通知 CompletionPromiseEvaluator")
		return
	}

	ev := coord.GetCompletionPromiseEvaluator()
	if ev == nil {
		logger.Warn(taskCompLogComponent).
			Str("event_type", "task_completion_notify_evaluator_skip").
			Msg("CompletionPromiseEvaluator 为 nil，无法通知")
		return
	}
	ev.NotifyFulfilled(text)
}
