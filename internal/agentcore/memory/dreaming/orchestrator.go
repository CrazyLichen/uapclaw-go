package dreaming

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingOrchestrator 空闲感知的定时记忆整理编排器。
//
// 生命周期：
//
//	Start(ctx) → 启动后台 goroutine
//	Stop(ctx)  → 取消 goroutine
//
// 调度策略：
//
//  1. Busy Backoff: busyChecker() 返回 true → 跳过本轮
//  2. 定时执行: 间隔 interval 执行 sweepFn
//
// 幂等：重复 Start()/Stop() 安全。
//
// Python: openjiuwen/core/memory/dreaming/orchestrator.py (DreamingOrchestrator)
type DreamingOrchestrator struct {
	// sweepFn 每轮执行的 sweep 函数
	sweepFn func(ctx context.Context) error
	// interval 调度间隔，最小 60s
	interval time.Duration
	// busyChecker 忙碌检查回调，返回 true 表示跳过本轮
	busyChecker func() bool
	// name 编排器名称，用于日志
	name string

	// running 是否运行中
	running bool
	// mu 保护 running 字段
	mu sync.Mutex
	// cancelFunc 用于取消后台 goroutine
	cancelFunc context.CancelFunc
	// doneCh goroutine 退出信号
	doneCh chan struct{}
}

// OrchestratorHealth 编排器健康状态。
// Python: DreamingOrchestrator.health (property)
type OrchestratorHealth struct {
	// Running 是否运行中
	Running bool `json:"running"`
	// IntervalSeconds 调度间隔秒数
	IntervalSeconds float64 `json:"interval_seconds"`
}

// OrchestratorOption 编排器可选参数函数。
type OrchestratorOption func(*DreamingOrchestrator)

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// minInterval 最小调度间隔，对齐 Python: self._interval = max(60.0, interval_seconds)
	minInterval = 60 * time.Second
	// initialDelay 初始延迟，对齐 Python: await asyncio.sleep(120.0)
	initialDelay = 120 * time.Second
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDreamingOrchestrator 创建编排器实例。
//
// interval 小于 60s 时钳位到 60s（对齐 Python: self._interval = max(60.0, interval_seconds)）。
// name 默认 "dreaming"（对齐 Python: name: str = "dreaming"）。
//
// Python: DreamingOrchestrator.__init__(sweep_fn, interval_seconds, busy_checker, name)
func NewDreamingOrchestrator(
	sweepFn func(ctx context.Context) error,
	interval time.Duration,
	opts ...OrchestratorOption,
) *DreamingOrchestrator {
	if interval < minInterval {
		interval = minInterval
	}
	o := &DreamingOrchestrator{
		sweepFn:     sweepFn,
		interval:    interval,
		busyChecker: nil,
		name:        "dreaming",
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithBusyChecker 设置忙碌检查回调。
func WithBusyChecker(checker func() bool) OrchestratorOption {
	return func(o *DreamingOrchestrator) { o.busyChecker = checker }
}

// WithName 设置编排器名称。
func WithName(name string) OrchestratorOption {
	return func(o *DreamingOrchestrator) { o.name = name }
}

// Health 返回编排器健康状态。
// Python: DreamingOrchestrator.health (property)
func (o *DreamingOrchestrator) Health() OrchestratorHealth {
	o.mu.Lock()
	defer o.mu.Unlock()
	return OrchestratorHealth{
		Running:         o.running,
		IntervalSeconds: o.interval.Seconds(),
	}
}

// IsRunning 返回编排器是否正在运行。
func (o *DreamingOrchestrator) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running
}

// Start 启动后台 goroutine。幂等：已运行时重复调用无效。
// Python: DreamingOrchestrator.start()
func (o *DreamingOrchestrator) Start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.running {
		return nil
	}
	o.running = true
	ctx, o.cancelFunc = context.WithCancel(ctx)
	o.doneCh = make(chan struct{})
	go o.loop(ctx)
	logger.Info(logComponent).
		Str("name", o.name).
		Float64("interval_seconds", o.interval.Seconds()).
		Msg("Orchestrator started")
	return nil
}

// Stop 停止后台 goroutine。幂等：未运行时重复调用无效。
// Python: DreamingOrchestrator.stop()
func (o *DreamingOrchestrator) Stop(ctx context.Context) error {
	o.mu.Lock()
	if !o.running {
		o.mu.Unlock()
		return nil
	}
	o.running = false
	cancel := o.cancelFunc
	doneCh := o.doneCh
	o.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		<-doneCh
	}
	o.mu.Lock()
	o.cancelFunc = nil
	o.doneCh = nil
	o.mu.Unlock()

	logger.Info(logComponent).Str("name", o.name).Msg("Orchestrator stopped")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loop 后台主循环。
// Python: DreamingOrchestrator._loop()
func (o *DreamingOrchestrator) loop(ctx context.Context) {
	defer close(o.doneCh)
	// M-06: 确保任何退出路径都清除 running 标志
	defer func() {
		o.mu.Lock()
		o.running = false
		o.mu.Unlock()
	}()

	// 初始延迟 120s（对齐 Python: await asyncio.sleep(120.0)）
	select {
	case <-time.After(initialDelay):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := o.tick(ctx); err != nil {
				// M-02: tick 返回 context.Canceled 时退出 loop，对齐 Python raise
				if errors.Is(err, context.Canceled) {
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// tick 执行一轮 sweep。
// Python: DreamingOrchestrator._tick()
// 返回 error 以便 loop 根据错误类型决定是否退出。
func (o *DreamingOrchestrator) tick(ctx context.Context) error {
	// Busy Backoff（对齐 Python: busy_checker() 返回 True → 跳过）
	// Python: try: if self._busy_checker(): return
	//         except Exception: logger.warning("busy_checker raised exception, skipping check")
	busy := false
	if o.busyChecker != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// 对齐 Python: except Exception → logger.warning("busy_checker raised exception, skipping check")
					// 注意：Python 中异常后不跳过 sweep，继续执行
					logger.Warn(logComponent).
						Str("name", o.name).
						Any("panic", r).
						Msg("busy_checker raised exception, skipping check")
				}
			}()
			if o.busyChecker() {
				busy = true
				logger.Info(logComponent).Str("name", o.name).Msg("agent busy, delay sweep")
			}
		}()
	}
	if busy {
		return nil
	}

	// 执行 sweep（对齐 Python: await self._sweep_fn()）
	logger.Info(logComponent).Str("name", o.name).Msg("start sweep")
	if err := o.sweepFn(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			// 对齐 Python: CancelledError → raise（由 loop 外层处理）
			logger.Info(logComponent).Str("name", o.name).Msg("sweep cancelled")
			return err // M-02: 传播 Canceled 给 loop，让 loop 退出
		}
		// 对齐 Python: except Exception → logger.exception("sweep exception: %s", exc)
		logger.Error(logComponent).Err(err).Str("name", o.name).Msg("sweep exception")
	} else {
		logger.Info(logComponent).Str("name", o.name).Msg("sweep completed")
	}
	return nil
}
