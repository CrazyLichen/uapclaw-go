package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// warnThreshold 成本/时间预警阈值（80%，对齐 _WARN_THRESHOLD）
const warnThreshold = 0.8

// ──────────────────────────── 结构体 ────────────────────────────

// SessionBudgetController 管理单次会话的时钟预算与 API 成本预算
// （对齐 class SessionBudgetController）。
type SessionBudgetController struct {
	wallClockSecs   float64
	costLimitUSD    float64
	taskTimeoutSecs float64
	// start 会话起始时间（单调时钟；nil=未 start，用 isStarted 区分）
	start      time.Time
	isStarted  bool
	costUSD    float64
	timeWarned bool
	costWarned bool
	// nowFn 单调时钟注入（测试用；默认 time.Now）
	nowFn func() time.Time
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionBudgetController 构造会话预算控制器（对齐 __init__ 默认值
// wall_clock_secs=3600.0 / cost_limit_usd=10.0 / task_timeout_secs=1200.0）
func NewSessionBudgetController(wallClockSecs, costLimitUSD, taskTimeoutSecs float64) *SessionBudgetController {
	return &SessionBudgetController{
		wallClockSecs:   wallClockSecs,
		costLimitUSD:    costLimitUSD,
		taskTimeoutSecs: taskTimeoutSecs,
		nowFn:           time.Now,
	}
}

// Start 记录会话起始时间（对齐 start）
func (b *SessionBudgetController) Start() {
	b.start = b.nowFn()
	b.isStarted = true
	logger.Info(logComponent).
		Float64("wall_clock_secs", b.wallClockSecs).
		Float64("cost_limit_usd", b.costLimitUSD).
		Msg("Session budget started")
}

// AddCost 累加 API 成本并在达到阈值时发出警告（对齐 add_cost）
func (b *SessionBudgetController) AddCost(amountUSD float64) {
	b.costUSD += amountUSD
	ratio := b.costUSD / b.costLimitUSD
	if ratio >= warnThreshold && !b.costWarned {
		b.costWarned = true
		logger.Warn(logComponent).
			Float64("ratio_pct", ratio*100).
			Float64("cost_usd", b.costUSD).
			Float64("cost_limit_usd", b.costLimitUSD).
			Msg("Cost budget used")
	}
}

// ElapsedSecs 已用时长（秒）（对齐 elapsed_secs；未 start 返回 0）
func (b *SessionBudgetController) ElapsedSecs() float64 {
	if !b.isStarted {
		return 0.0
	}
	return b.nowFn().Sub(b.start).Seconds()
}

// RemainingSecs 剩余时钟预算（秒）（对齐 remaining_secs）
func (b *SessionBudgetController) RemainingSecs() float64 {
	return maxFloat(0.0, b.wallClockSecs-b.ElapsedSecs())
}

// RemainingCostUSD 剩余成本预算（美元）（对齐 remaining_cost_usd）
func (b *SessionBudgetController) RemainingCostUSD() float64 {
	return maxFloat(0.0, b.costLimitUSD-b.costUSD)
}

// ShouldStop 时钟或成本预算耗尽时返回 true（对齐 should_stop）
func (b *SessionBudgetController) ShouldStop() bool {
	if b.isStarted {
		elapsed := b.ElapsedSecs()
		ratio := elapsed / b.wallClockSecs
		if ratio >= warnThreshold && !b.timeWarned {
			b.timeWarned = true
			logger.Warn(logComponent).
				Float64("ratio_pct", ratio*100).
				Float64("elapsed", elapsed).
				Float64("wall_clock_secs", b.wallClockSecs).
				Msg("Wall-clock budget used")
		}
		if elapsed >= b.wallClockSecs {
			return true
		}
	}
	return b.costUSD >= b.costLimitUSD
}

// CheckTaskBudget 检查是否有足够时间启动下一个任务（对齐 check_task_budget）；
// taskTimeoutSecs 为 0 时使用构造参数
func (b *SessionBudgetController) CheckTaskBudget(taskTimeoutSecs float64) bool {
	timeout := taskTimeoutSecs
	if timeout == 0 {
		timeout = b.taskTimeoutSecs
	}
	return b.RemainingSecs() >= timeout
}

// SetNowFn 注入单调时钟（测试用）
func (b *SessionBudgetController) SetNowFn(fn func() time.Time) {
	b.nowFn = fn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// maxFloat 两值取大
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ──────────────────────────── 修复循环 ────────────────────────────

// FixLoopResult 修复循环执行结果（对齐 dataclass FixLoopResult）
type FixLoopResult struct {
	Success  bool     `yaml:"success" json:"success"`
	Attempts int      `yaml:"attempts" json:"attempts"`
	Phase    int      `yaml:"phase" json:"phase"`
	ErrorLog []string `yaml:"error_log" json:"error_log"`
}

// FixLoopCIRunner 运行 CI，返回 (passed, errors)（对齐 ci_runner 协程）；
// err 非 nil 视为超时/异常路径
type FixLoopCIRunner func(ctx context.Context) (passed bool, errs string, err error)

// FixLoopFixer 接收错误信息并尝试修复（对齐 agent_fixer 协程）
type FixLoopFixer func(ctx context.Context, errs string) error

// FixLoopEvaluator 可选，审查修复质量（对齐 evaluator 协程）；
// 返回 (approved, err)
type FixLoopEvaluator func(ctx context.Context) (approved bool, err error)

// FixLoopController 两阶段 CI 修复循环（IMMUTABLE，对齐 class FixLoopController）。
//
// Phase 1 — 直接修复：运行 CI → 解析错误 → agent 修复 → 重试。
// Phase 2 — 评审修复：evaluator 审查质量，不通过则继续修复。
type FixLoopController struct {
	p1Max   int
	p2Max   int
	timeout time.Duration
}

// NewFixLoopController 构造修复循环控制器（对齐 __init__ 默认值
// phase1_max_retries=10 / phase2_max_retries=9 / timeout_per_attempt=600.0）
func NewFixLoopController(phase1MaxRetries, phase2MaxRetries int, timeoutPerAttempt time.Duration) *FixLoopController {
	return &FixLoopController{
		p1Max:   phase1MaxRetries,
		p2Max:   phase2MaxRetries,
		timeout: timeoutPerAttempt,
	}
}

// Run 执行两阶段修复循环（对齐 run；asyncio.wait_for → ctx 超时）。
func (f *FixLoopController) Run(
	ciRunner FixLoopCIRunner,
	agentFixer FixLoopFixer,
	evaluator FixLoopEvaluator,
) *FixLoopResult {
	result := &FixLoopResult{}

	// ---- Phase 1: 直接修复 ----
	result.Phase = 1
	for i := 1; i <= f.p1Max; i++ {
		result.Attempts = i
		logger.Info(logComponent).
			Int("attempt", i).
			Int("max", f.p1Max).
			Msg("Phase 1 attempt")
		ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
		passed, errs, err := ciRunner(ctx)
		cancel()
		if err != nil || ctx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("Phase 1 attempt %d: CI timeout", i)
			logger.Warn(logComponent).Msg(msg)
			result.ErrorLog = append(result.ErrorLog, msg)
			continue
		}
		if passed {
			result.Success = true
			logger.Info(logComponent).
				Int("attempt", i).
				Msg("CI passed on phase 1 attempt")
			return result
		}
		if errs == "" {
			errs = "unknown error"
		}
		errorText := errs
		if len(errorText) > 200 {
			errorText = errorText[:200]
		}
		result.ErrorLog = append(result.ErrorLog, fmt.Sprintf("Phase 1 attempt %d: %s", i, errorText))
		fctx, fcancel := context.WithTimeout(context.Background(), f.timeout)
		ferr := agentFixer(fctx, errs)
		fcancel()
		if ferr != nil || fctx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("Phase 1 attempt %d: fixer timeout", i)
			logger.Warn(logComponent).Msg(msg)
			result.ErrorLog = append(result.ErrorLog, msg)
		}
	}

	// ---- Phase 2: 评审修复 ----
	if evaluator == nil {
		return result
	}

	result.Phase = 2
	for j := 1; j <= f.p2Max; j++ {
		result.Attempts++
		logger.Info(logComponent).
			Int("attempt", j).
			Int("max", f.p2Max).
			Msg("Phase 2 attempt")
		ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
		approved, err := evaluator(ctx)
		cancel()
		if err != nil || ctx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("Phase 2 attempt %d: evaluator timeout", j)
			logger.Warn(logComponent).Msg(msg)
			result.ErrorLog = append(result.ErrorLog, msg)
			continue
		}
		if approved {
			result.Success = true
			logger.Info(logComponent).
				Int("attempt", j).
				Msg("Evaluator approved on phase 2 attempt")
			return result
		}

		result.ErrorLog = append(result.ErrorLog, fmt.Sprintf("Phase 2 attempt %d: evaluator rejected", j))
		fctx, fcancel := context.WithTimeout(context.Background(), f.timeout)
		ferr := agentFixer(fctx, "evaluator rejected")
		fcancel()
		if ferr != nil || fctx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("Phase 2 attempt %d: fixer timeout", j)
			logger.Warn(logComponent).Msg(msg)
			result.ErrorLog = append(result.ErrorLog, msg)
		}
	}

	return result
}
