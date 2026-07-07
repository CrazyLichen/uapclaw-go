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

// TaskCompletionRail 任务完成检测 Rail。
//
// 负责三件事：
//  1. BeforeModelCall:        向系统提示词注入 <promise> 完成信号节
//  2. BeforeTaskIteration:    首轮迭代时将 taskInstruction 模板应用到查询
//  3. AfterTaskIteration:     检测输出中的 <promise>...</promise> 标签，通知 CompletionPromiseEvaluator
//
// 仅当 enable_task_loop=True 时生效。
// 对齐 Python: TaskCompletionRail (openjiuwen/harness/rails/task_completion_rail.py)
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

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskCompletionRailPriority TaskCompletionRail 优先级
	// 对齐 Python: TaskCompletionRail.priority = 10
	taskCompletionRailPriority = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 TaskCompletionRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*TaskCompletionRail)(nil)

// taskCompLogComponent 日志组件标识
var taskCompLogComponent = logger.ComponentAgentCore

// promiseTagPattern 匹配 <promise>...</promise> 标签的正则
// 对齐 Python: PROMISE_TAG_PATTERN = re.compile(r"<promise>\s*(.*?)\s*</promise>", re.DOTALL | re.IGNORECASE)
var promiseTagPattern = regexp.MustCompile(`(?i)(?s)<promise>\s*(.*?)\s*</promise>`)

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskCompletionOption TaskCompletionRail 构造选项函数。
type TaskCompletionOption func(*TaskCompletionRail)

// WithTaskInstruction 设置任务指令模板。
func WithTaskInstruction(template string) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.taskInstruction = template }
}

// WithCompletionPromise 设置完成承诺令牌。
func WithCompletionPromise(promise string) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.completionPromise = promise }
}

// WithRequiredConfirmations 设置连续确认次数。
func WithRequiredConfirmations(n int) TaskCompletionOption {
	return func(r *TaskCompletionRail) {
		if n < 1 {
			n = 1
		}
		r.requiredConfirmations = n
	}
}

// WithAllowPromiseDetails 设置是否允许 promise 块含详情。
func WithAllowPromiseDetails(allow bool) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.allowPromiseDetails = allow }
}

// WithMaxRounds 设置外层循环最大轮数。
func WithMaxRounds(n int) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.maxRounds = n }
}

// WithTimeoutSeconds 设置墙钟超时秒数。
func WithTimeoutSeconds(sec float64) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.timeoutSeconds = sec }
}

// WithExtraEvaluators 设置额外自定义评估器。
func WithExtraEvaluators(evaluators ...task_loop.StopConditionEvaluator) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.extraEvaluators = evaluators }
}

// NewTaskCompletionRail 创建 TaskCompletionRail 实例。
//
// 默认无参创建，与 Python TaskCompletionRail() 对齐。
// 用户通过 opts 传入策略参数。
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

// BuildEvaluators 根据Rail参数构建评估器链。
//
// 内置评估器顺序：MaxRounds → Timeout → CompletionPromise，
// 然后追加用户传入的额外评估器。
// 对齐 Python: TaskCompletionRail.build_evaluators()
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

// BeforeModelCall 在每次 LLM 调用前注入完成信号提示词节。
//
// 对齐 Python: TaskCompletionRail.before_model_call()
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

// BeforeTaskIteration 在首轮迭代时将 taskInstruction 模板应用到查询。
//
// 仅在第一轮（非 follow-up）迭代时格式化查询。
// 对齐 Python: TaskCompletionRail.before_task_iteration()
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

// AfterTaskIteration 在迭代完成后检测输出中的 promise 标签。
//
// 匹配成功则通知 CompletionPromiseEvaluator，使外层循环在下一轮停止。
// 对齐 Python: TaskCompletionRail.after_task_iteration()
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

// GetCallbacks 覆盖基类回调映射，增加 BeforeModelCall + BeforeTaskIteration + AfterTaskIteration。
//
// 对齐 Python: TaskCompletionRail 隐式覆盖 before_model_call + before_task_iteration + after_task_iteration
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizePromiseText 折叠空白字符用于 promise 比较。
// 对齐 Python: _normalize(text)
func normalizePromiseText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// ExtractPromiseBlock 用正则提取 <promise>...</promise> 内容。
// 导出供测试使用。
// 对齐 Python: extract_promise_block(text)
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

// PromiseMatches 判断 promise 块首行是否以期望令牌开头。
// 导出供测试使用。
// 对齐 Python: promise_matches(block, expected)
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

// extractOutput 从 TaskIterationInputs.Result["output"] 提取输出文本。
// 对齐 Python: TaskCompletionRail._extract_output(ctx)
func extractOutput(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil {
		return ""
	}
	result := inputs.Result
	if result == nil {
		return ""
	}
	output, _ := result["output"]
	if output == nil {
		return ""
	}
	s, ok := output.(string)
	if !ok {
		return fmt.Sprintf("%v", output)
	}
	return s
}

// notifyEvaluator 获取 LoopCoordinator → CompletionPromiseEvaluator → NotifyFulfilled。
// 对齐 Python: TaskCompletionRail._notify_evaluator(ctx, text)
func notifyEvaluator(cbc *agentinterfaces.AgentCallbackContext, text string) {
	agent := cbc.Agent()

	// 通过 DeepAgentInterface 获取 LoopCoordinator
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		return
	}
	coord := deepAgent.LoopCoordinator()
	if coord == nil {
		return
	}

	// 类型断言到 *task_loop.LoopCoordinator 以访问 GetCompletionPromiseEvaluator
	coordConcrete, ok := coord.(*task_loop.LoopCoordinator)
	if !ok {
		return
	}
	ev := coordConcrete.GetCompletionPromiseEvaluator()
	if ev == nil {
		return
	}
	ev.NotifyFulfilled(text)
}
