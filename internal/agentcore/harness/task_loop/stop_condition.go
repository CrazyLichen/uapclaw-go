package task_loop

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// StopConditionEvaluator 停止条件评估器接口。
// 每个评估器回答一个问题："循环该停了吗？"
// LoopCoordinator 持有评估器切片，OR 语义：第一个 ShouldStop=true 即停止。
// 对齐 Python: StopConditionEvaluator
type StopConditionEvaluator interface {
	// ShouldStop 评估是否应该停止循环
	ShouldStop(ctx StopEvaluationContext) bool

	// Name 返回评估器名称（用于状态序列化索引、停止原因和日志）
	Name() string

	// ExportState 导出评估器状态用于持久化（无状态评估器返回 nil）
	ExportState() map[string]any

	// ImportState 从持久化状态恢复（无状态评估器忽略参数）
	ImportState(data map[string]any)

	// Reset 重置内部状态，用于新的 invoke 周期
	Reset()
}

// StopEvaluationContext 停止条件评估上下文。
// 与 AgentCallbackContext 解耦，使评估器不依赖 Agent 回调系统。
// 对齐 Python: StopEvaluationContext
type StopEvaluationContext struct {
	// Iteration 当前迭代次数（已完成的轮数）
	Iteration int
	// TokenUsage 累计 token 用量
	TokenUsage int
	// ElapsedSeconds 已用时间（秒）
	ElapsedSeconds float64
	// LastResult 上一轮结果
	LastResult map[string]any
	// Extra 额外上下文（供自定义评估器使用）
	Extra map[string]any
}

// MaxRoundsEvaluator 最大轮次评估器。
// 当已完成轮数 >= maxRounds 时判定应停止。
// 对齐 Python: MaxRoundsEvaluator
type MaxRoundsEvaluator struct {
	// maxRounds 最大轮次
	maxRounds int
}

// TokenBudgetEvaluator token 预算评估器。
// 当累计 token 用量 >= maxTokens 时判定应停止。
// 对齐 Python: TokenBudgetEvaluator
type TokenBudgetEvaluator struct {
	// maxTokens 最大 token 数
	maxTokens int
}

// TimeoutEvaluator 超时评估器。
// 当墙钟时间 >= timeoutSeconds 时判定应停止。
// 对齐 Python: TimeoutEvaluator
type TimeoutEvaluator struct {
	// timeoutSeconds 超时秒数
	timeoutSeconds float64
}

// CompletionPromiseEvaluator 完成承诺评估器。
// 追踪连续检测到 promise 标签的次数，连续达到 requiredConfirmations 次时判定完成。
// TaskCompletionRail 在 before_model_call 时注入 promise 提示，
// 在 after_model_call 时检测输出中的 promise 标签并调用 NotifyFulfilled/NotifyAbsent。
// 对齐 Python: CompletionPromiseEvaluator
type CompletionPromiseEvaluator struct {
	// promise 要匹配的标签（如 "<promise>"）
	promise string
	// requiredConfirmations 需要连续检测到的次数
	requiredConfirmations int
	// confirmationCount 当前已连续检测到的次数
	confirmationCount int
	// fulfilled 是否已达到所需确认次数
	fulfilled bool
	// matchedText 最近一次匹配到的文本
	matchedText string
}

// CustomPredicateEvaluator 自定义谓词评估器。
// 通过用户提供的函数判断是否停止循环。
// 对齐 Python: CustomPredicateEvaluator
type CustomPredicateEvaluator struct {
	// name 评估器名称
	name string
	// predicate 自定义判断函数
	predicate func(ctx StopEvaluationContext) bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMaxRoundsEvaluator 创建最大轮次评估器。
// maxRounds 为 0 或负数时，ShouldStop 立即返回 true（即"立即停止"）。
// 对齐 Python: MaxRoundsEvaluator
func NewMaxRoundsEvaluator(maxRounds int) *MaxRoundsEvaluator {
	return &MaxRoundsEvaluator{maxRounds: maxRounds}
}

// NewTokenBudgetEvaluator 创建 token 预算评估器。
// maxTokens 为 0 或负数时，ShouldStop 立即返回 true（即"立即停止"）。
// 对齐 Python: TokenBudgetEvaluator
func NewTokenBudgetEvaluator(maxTokens int) *TokenBudgetEvaluator {
	return &TokenBudgetEvaluator{maxTokens: maxTokens}
}

// NewTimeoutEvaluator 创建超时评估器。
// timeoutSeconds 为 0 或负数时，ShouldStop 立即返回 true（即"立即停止"）。
// 对齐 Python: TimeoutEvaluator
func NewTimeoutEvaluator(timeoutSeconds float64) *TimeoutEvaluator {
	return &TimeoutEvaluator{timeoutSeconds: timeoutSeconds}
}

// NewCompletionPromiseEvaluator 创建完成承诺评估器。
// promise 为要匹配的标签，requiredConfirmations 为需连续检测到的次数（至少 1）。
// 对齐 Python: CompletionPromiseEvaluator.__init__
func NewCompletionPromiseEvaluator(promise string, requiredConfirmations int) *CompletionPromiseEvaluator {
	if requiredConfirmations < 1 {
		requiredConfirmations = 1
	}
	return &CompletionPromiseEvaluator{
		promise:               promise,
		requiredConfirmations: requiredConfirmations,
	}
}

// NewCustomPredicateEvaluator 创建自定义谓词评估器。
// 对齐 Python: CustomPredicateEvaluator
func NewCustomPredicateEvaluator(name string, predicate func(ctx StopEvaluationContext) bool) *CustomPredicateEvaluator {
	if predicate == nil {
		panic("NewCustomPredicateEvaluator: predicate 不能为 nil")
	}
	return &CustomPredicateEvaluator{name: name, predicate: predicate}
}

// ShouldStop 当 iteration >= maxRounds 时返回 true。
func (e *MaxRoundsEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	return ctx.Iteration >= e.maxRounds
}

// Name 返回评估器名称 "MaxRoundsEvaluator"。
func (e *MaxRoundsEvaluator) Name() string {
	return "MaxRoundsEvaluator"
}

// ExportState 无状态评估器，返回 nil。
// 对齐 Python: StopConditionEvaluator.get_state() 返回 None
func (e *MaxRoundsEvaluator) ExportState() map[string]any {
	return nil
}

// ImportState 无状态评估器，忽略参数。
func (e *MaxRoundsEvaluator) ImportState(_ map[string]any) {}

// Reset 无状态评估器，无操作。
func (e *MaxRoundsEvaluator) Reset() {}

// ShouldStop 当 tokenUsage >= maxTokens 时返回 true。
func (e *TokenBudgetEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	return ctx.TokenUsage >= e.maxTokens
}

// Name 返回评估器名称 "TokenBudgetEvaluator"。
func (e *TokenBudgetEvaluator) Name() string {
	return "TokenBudgetEvaluator"
}

// ExportState 无状态评估器，返回 nil。
// 对齐 Python: StopConditionEvaluator.get_state() 返回 None
func (e *TokenBudgetEvaluator) ExportState() map[string]any {
	return nil
}

// ImportState 无状态评估器，忽略参数。
func (e *TokenBudgetEvaluator) ImportState(_ map[string]any) {}

// Reset 无状态评估器，无操作。
func (e *TokenBudgetEvaluator) Reset() {}

// ShouldStop 当 elapsedSeconds >= timeoutSeconds 时返回 true。
func (e *TimeoutEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	return ctx.ElapsedSeconds >= e.timeoutSeconds
}

// Name 返回评估器名称 "TimeoutEvaluator"。
func (e *TimeoutEvaluator) Name() string {
	return "TimeoutEvaluator"
}

// ExportState 无状态评估器，返回 nil。
// 对齐 Python: StopConditionEvaluator.get_state() 返回 None
func (e *TimeoutEvaluator) ExportState() map[string]any {
	return nil
}

// ImportState 无状态评估器，忽略参数。
func (e *TimeoutEvaluator) ImportState(_ map[string]any) {}

// Reset 无状态评估器，无操作。
func (e *TimeoutEvaluator) Reset() {}

// ShouldStop 当 fulfilled 标志为 true 时返回 true。
// 对齐 Python: CompletionPromiseEvaluator.should_stop
func (e *CompletionPromiseEvaluator) ShouldStop(_ StopEvaluationContext) bool {
	return e.fulfilled
}

// Name 返回评估器名称 "CompletionPromiseEvaluator"。
func (e *CompletionPromiseEvaluator) Name() string {
	return "CompletionPromiseEvaluator"
}

// ExportState 导出状态：fulfilled, matchedText, requiredConfirmations, confirmationCount。
// 对齐 Python: CompletionPromiseEvaluator.get_state
func (e *CompletionPromiseEvaluator) ExportState() map[string]any {
	return map[string]any{
		"fulfilled":              e.fulfilled,
		"matched_text":           e.matchedText,
		"required_confirmations": e.requiredConfirmations,
		"confirmation_count":     e.confirmationCount,
	}
}

// ImportState 从持久化状态恢复。
// 对齐 Python: CompletionPromiseEvaluator.load_state
func (e *CompletionPromiseEvaluator) ImportState(data map[string]any) {
	if data == nil {
		return
	}
	e.fulfilled = toBool(data["fulfilled"])
	e.matchedText = toStr(data["matched_text"])
	if v, ok := data["required_confirmations"]; ok {
		if n := toInt(v); n >= 1 {
			e.requiredConfirmations = n
		}
	}
	if v, ok := data["confirmation_count"]; ok {
		e.confirmationCount = max(0, toInt(v))
	}
	// 恢复后重新计算 fulfilled
	e.fulfilled = e.fulfilled || (e.confirmationCount >= e.requiredConfirmations)
}

// Reset 重置状态，用于新的 invoke 周期。
// 对齐 Python: CompletionPromiseEvaluator.reset
func (e *CompletionPromiseEvaluator) Reset() {
	e.fulfilled = false
	e.matchedText = ""
	e.confirmationCount = 0
}

// NotifyFulfilled 标记 promise 已满足。
// 对齐 Python: CompletionPromiseEvaluator.notify_fulfilled
func (e *CompletionPromiseEvaluator) NotifyFulfilled(matchedText string) {
	e.confirmationCount++
	e.fulfilled = e.confirmationCount >= e.requiredConfirmations
	e.matchedText = matchedText
}

// NotifyAbsent 记录 promise 未出现，连续计数归零。
// 对齐 Python: CompletionPromiseEvaluator.notify_absent
func (e *CompletionPromiseEvaluator) NotifyAbsent() {
	e.confirmationCount = 0
	e.fulfilled = false
	e.matchedText = ""
}

// Confirmations 返回当前连续确认次数。
func (e *CompletionPromiseEvaluator) Confirmations() int {
	return e.confirmationCount
}

// Promise 返回要匹配的标签。
func (e *CompletionPromiseEvaluator) Promise() string {
	return e.promise
}

// RequiredConfirmations 返回所需连续确认次数。
func (e *CompletionPromiseEvaluator) RequiredConfirmations() int {
	return e.requiredConfirmations
}

// ShouldStop 委托给用户提供的谓词函数。
// 对齐 Python: CustomPredicateEvaluator.should_stop
func (e *CustomPredicateEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	return e.predicate(ctx)
}

// Name 返回评估器名称。
// 对齐 Python: self.__class__.__name__ 默认返回 "CustomPredicateEvaluator"
func (e *CustomPredicateEvaluator) Name() string {
	if e.name == "" {
		return "CustomPredicateEvaluator"
	}
	return e.name
}

// ExportState 无状态评估器，返回 nil。
// 对齐 Python: StopConditionEvaluator.get_state() 返回 None
func (e *CustomPredicateEvaluator) ExportState() map[string]any {
	return nil
}

// ImportState 无状态评估器，忽略参数。
func (e *CustomPredicateEvaluator) ImportState(_ map[string]any) {}

// Reset 无状态评估器，无操作。
func (e *CustomPredicateEvaluator) Reset() {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toBool 安全转换 any 到 bool。
func toBool(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// toStr 安全转换 any 到 string。
func toStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// toInt 安全转换 any 到 int。
func toInt(v any) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
