package task_loop

import (
	"runtime"
	"sync"
	"time"

	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopCoordinatorState 循环协调器可序列化状态。
// 对齐 Python: LoopCoordinator.get_state() 返回值
type LoopCoordinatorState struct {
	// Iteration 迭代次数
	Iteration int `json:"iteration"`
	// TokenUsage 累计 token 用量
	TokenUsage int `json:"token_usage"`
	// StopReason 停止原因（评估器名称或 "Aborted"）
	StopReason string `json:"stop_reason"`
	// EvaluatorStates 各评估器状态（按 Name() 索引）
	EvaluatorStates map[string]map[string]any `json:"evaluator_states"`
}

// LoopCoordinator 外层任务循环协调器。
// 追踪迭代次数、token 用量、耗时和中止标记，
// 每轮迭代前通过评估器链（OR 语义）决定是否继续循环。
// 对齐 Python: LoopCoordinator
type LoopCoordinator struct {
	mu         sync.Mutex
	iteration  int
	tokenUsage int
	aborted    bool
	startTime  time.Time
	stopReason string
	lastResult map[string]any
	evaluators []StopConditionEvaluator
}

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoopCoordinator 创建循环协调器。
// 对齐 Python: LoopCoordinator.__init__
func NewLoopCoordinator(evaluators []StopConditionEvaluator) *LoopCoordinator {
	if evaluators == nil {
		evaluators = []StopConditionEvaluator{}
	}
	return &LoopCoordinator{
		evaluators: evaluators,
		startTime:  time.Time{},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ShouldContinue 评估是否应该继续循环。
// 先检查中止标记，再遍历评估器（OR 语义：第一个 ShouldStop=true 即停止）。
// 对齐 Python: LoopCoordinator.should_continue
func (lc *LoopCoordinator) ShouldContinue() bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.aborted {
		lc.stopReason = "Aborted"
		return false
	}

	ctx := lc.buildEvalContext()
	for _, ev := range lc.evaluators {
		stopped := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					logger.Warn(logComponent).
						Str("evaluator", ev.Name()).
						Any("panic", r).
						Str("stack", string(buf[:n])).
						Msg("评估器 panic，跳过")
				}
			}()
			stopped = ev.ShouldStop(ctx)
		}()
		if stopped {
			lc.stopReason = ev.Name()
			logger.Info(logComponent).
				Str("stop_condition", ev.Name()).
				Msg("满足停止条件")
			return false
		}
	}
	return true
}

// IncrementIteration 递增迭代次数。
// 对齐 Python: LoopCoordinator.increment_iteration
func (lc *LoopCoordinator) IncrementIteration() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.iteration++
}

// AddTokenUsage 累加 token 用量（仅正数有效）。
// 对齐 Python: LoopCoordinator.add_token_usage
func (lc *LoopCoordinator) AddTokenUsage(tokens int) {
	if tokens <= 0 {
		return
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.tokenUsage += tokens
}

// SetLastResult 设置上一轮结果。
// 对齐 Python: LoopCoordinator.set_last_result
func (lc *LoopCoordinator) SetLastResult(result map[string]any) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.lastResult = result
}

// RequestAbort 请求中止循环。
// 对齐 Python: LoopCoordinator.request_abort
func (lc *LoopCoordinator) RequestAbort() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.aborted = true
}

// Reset 重置所有状态，用于新的 invoke 周期。
// 对齐 Python: LoopCoordinator.reset
func (lc *LoopCoordinator) Reset() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.iteration = 0
	lc.tokenUsage = 0
	lc.aborted = false
	lc.startTime = time.Now()
	lc.stopReason = ""
	lc.lastResult = nil
	for _, ev := range lc.evaluators {
		ev.Reset()
	}
}

// IsAborted 返回是否已中止。
func (lc *LoopCoordinator) IsAborted() bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.aborted
}

// StopReason 返回停止原因。
func (lc *LoopCoordinator) StopReason() string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.stopReason
}

// Iteration 返回当前迭代次数。
func (lc *LoopCoordinator) Iteration() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.iteration
}

// TokenUsage 返回累计 token 用量。
func (lc *LoopCoordinator) TokenUsage() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.tokenUsage
}

// ElapsedSeconds 返回已用时间（秒）。
// 若 startTime 为零值（尚未 Reset），返回 0.0。
func (lc *LoopCoordinator) ElapsedSeconds() float64 {
	lc.mu.Lock()
	startTime := lc.startTime
	lc.mu.Unlock()
	if startTime.IsZero() {
		return 0.0
	}
	return time.Since(startTime).Seconds()
}

// ExportState 导出状态用于持久化。
// 对齐 Python: LoopCoordinator.get_state
func (lc *LoopCoordinator) ExportState() LoopCoordinatorState {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	evStates := make(map[string]map[string]any, len(lc.evaluators))
	for _, ev := range lc.evaluators {
		s := ev.ExportState()
		if s != nil {
			evStates[ev.Name()] = s
		}
	}

	return LoopCoordinatorState{
		Iteration:       lc.iteration,
		TokenUsage:      lc.tokenUsage,
		StopReason:      lc.stopReason,
		EvaluatorStates: evStates,
	}
}

// ImportState 从持久化状态恢复。
// startTime 重置为当前时间，使 TimeoutEvaluator 从恢复点开始计时。
// 对齐 Python: LoopCoordinator.load_state
func (lc *LoopCoordinator) ImportState(state LoopCoordinatorState) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.iteration = state.Iteration
	lc.tokenUsage = state.TokenUsage
	lc.stopReason = state.StopReason
	lc.startTime = time.Now()

	if state.EvaluatorStates != nil {
		for _, ev := range lc.evaluators {
			if evState, ok := state.EvaluatorStates[ev.Name()]; ok {
				ev.ImportState(evState)
			}
		}
	}
}

// GetCompletionPromiseEvaluator 返回第一个 CompletionPromiseEvaluator（如有）。
// 返回接口类型以满足 LoopCoordinatorInterface 约束。
// 对齐 Python: LoopCoordinator.get_completion_promise_evaluator
func (lc *LoopCoordinator) GetCompletionPromiseEvaluator() hinterfaces.CompletionPromiseEvaluatorInterface {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for _, ev := range lc.evaluators {
		if cpe, ok := ev.(*CompletionPromiseEvaluator); ok {
			return cpe
		}
	}
	return nil
}

// Evaluators 返回评估器切片的副本（只读访问）。
func (lc *LoopCoordinator) Evaluators() []StopConditionEvaluator {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	result := make([]StopConditionEvaluator, len(lc.evaluators))
	copy(result, lc.evaluators)
	return result
}

// buildEvalContext 构建评估上下文（调用者需持有锁）。
// 对齐 Python: LoopCoordinator._build_eval_context
func (lc *LoopCoordinator) buildEvalContext() StopEvaluationContext {
	elapsed := 0.0
	if !lc.startTime.IsZero() {
		elapsed = time.Since(lc.startTime).Seconds()
	}
	return StopEvaluationContext{
		Iteration:      lc.iteration,
		TokenUsage:     lc.tokenUsage,
		ElapsedSeconds: elapsed,
		LastResult:     lc.lastResult,
	}
}
