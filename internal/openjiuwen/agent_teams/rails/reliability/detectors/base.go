// Package detectors 团队可靠性检测器（25.1g）。
//
// 纯信号→异常逻辑：消费统一 Signal 流，阈值触发时产出 Anomaly。
// 无 I/O、无锁（spec D-g1）。ErrorBurstDetector 为工具/模型错误率检测的
// 共享实现（滑动窗口速率 + 连续失败连击）。
//
// 文件目录：
//
//	detectors/
//	├── doc.go           # 包文档
//	├── base.go         # Detector 接口 + ErrorBurstDetector 共享实现
//	├── tool_error.go   # ToolErrorRateDetector 工具错误率
//	├── repeat_tool.go  # RepeatToolCallDetector 重复/循环调用（四档递进）
//	├── pingpong.go     # PingPongDetector 团队级乒乓（leader 进程消费）
//	├── model_error.go  # ModelStreamErrorDetector 模型错误（含路由裁决分流）
//	├── output_length.go # OutputLengthDetector 超长输出/思考
//	└── compaction.go  # FrequentCompactionDetector 频繁压缩推断
//
// 对应 Python 代码：openjiuwen/agent_teams/reliability/detectors/
package detectors

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/rails/reliability/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Detector 消费信号并在阈值触发时产出异常。
//
// Python: detectors/base.py Detector(Protocol)。纯逻辑、无 I/O、可单测。
type Detector interface {
	// Name 返回 detector 的稳定标识
	Name() string
	// Observe 消费一个信号；阈值触发时返回 Anomaly，否则 nil
	Observe(signal *core.Signal) *core.Anomaly
	// Reset 重置单轮检测状态
	Reset()
}

// ErrorBurstDetector 错误爆发检测：滑动窗口速率 + 连续失败连击。
//
// Python: detectors/base.py ErrorBurstDetector。统计 error_kind 的错误在尾随
// 窗口内的数量并追踪连续失败连击；reset_kind（成功）信号清零连击。
// 发射为边沿触发：仅当严重度高于上次已报级别时才发异常，持续爆发不会刷屏。
type ErrorBurstDetector struct {
	// name detector 稳定标识
	name string
	// errorKind 计数的错误信号类别
	errorKind core.SignalKind
	// resetKind 清零连击的成功信号类别
	resetKind core.SignalKind
	// anomalyKind 产出的异常类别
	anomalyKind core.AnomalyKind
	// windowSeconds 窗口宽度（秒）
	windowSeconds float64
	// rateThreshold 窗口内错误数阈值
	rateThreshold int
	// consecutiveThreshold 连续失败数阈值
	consecutiveThreshold int
	// nowFn 当前时间戳（秒），显式注入
	nowFn func() float64
	// window 滑动窗口计数器
	window *core.SlidingWindowCounter
	// consecutive 当前连续失败数
	consecutive int
	// firedSeverity 已发射的最高严重度（nil 表示未发射）
	firedSeverity *core.Severity
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewErrorBurstDetector 构造共享错误爆发检测器。
//
// Python: detectors/base.py ErrorBurstDetector.__init__。
func NewErrorBurstDetector(name string, errorKind, resetKind core.SignalKind, anomalyKind core.AnomalyKind,
	windowSeconds float64, rateThreshold, consecutiveThreshold int, nowFn func() float64) *ErrorBurstDetector {
	return &ErrorBurstDetector{
		name:                 name,
		errorKind:            errorKind,
		resetKind:            resetKind,
		anomalyKind:          anomalyKind,
		windowSeconds:        windowSeconds,
		rateThreshold:        rateThreshold,
		consecutiveThreshold: consecutiveThreshold,
		nowFn:                nowFn,
		window:               core.NewSlidingWindowCounter(windowSeconds),
	}
}

// Name 返回 detector 稳定标识。
//
// Python: detectors/base.py ErrorBurstDetector.name property。
func (d *ErrorBurstDetector) Name() string { return d.name }

// Observe 计数错误、成功清零、严重度上升时发射。
//
// Python: detectors/base.py ErrorBurstDetector.observe。
func (d *ErrorBurstDetector) Observe(signal *core.Signal) *core.Anomaly {
	if signal.Kind == d.resetKind {
		d.consecutive = 0
		d.firedSeverity = nil
		return nil
	}
	if signal.Kind != d.errorKind {
		return nil
	}
	now := d.nowFn()
	d.consecutive++
	windowCount := d.window.Add(now)
	severity := d.classify(windowCount)
	if severity == nil {
		return nil
	}
	if d.firedSeverity != nil && severity.Rank() <= d.firedSeverity.Rank() {
		return nil
	}
	d.firedSeverity = severity
	return &core.Anomaly{
		Detector:   d.name,
		Kind:       d.anomalyKind,
		Severity:   *severity,
		MemberName: signal.MemberName,
		Summary: fmt.Sprintf("%d consecutive failures (%d within %.0fs)",
			d.consecutive, windowCount, d.windowSeconds),
		Evidence: map[string]any{
			"consecutive":    d.consecutive,
			"window_count":   windowCount,
			"window_seconds": d.windowSeconds,
			// 对齐 Python None：*string nil 指针须转 nil interface（typed-nil 实锤）
			"last_error": errTextOrNil(signal.Error),
		},
	}
}

// Reset 重置窗口、连击与已发射级别。
//
// Python: detectors/base.py ErrorBurstDetector.reset。
func (d *ErrorBurstDetector) Reset() {
	d.window.Reset()
	d.consecutive = 0
	d.firedSeverity = nil
}

// FiredSeverity 返回已发射的最高严重度（未发射时第二返回值为 false）。
//
// Python: ModelStreamErrorDetector 直读 self._fired_severity；Go 以访问器暴露
// 供子类包装复用（同文件语义，非偏差）。
func (d *ErrorBurstDetector) FiredSeverity() (core.Severity, bool) {
	if d.firedSeverity == nil {
		return "", false
	}
	return *d.firedSeverity, true
}

// SetFiredSeverity 记录已发射严重度（子类 _unavailable 直写的等价入口）。
func (d *ErrorBurstDetector) SetFiredSeverity(sev core.Severity) {
	s := sev
	d.firedSeverity = &s
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// classify 把当前连击/窗口计数映射到严重度。
//
// Python: detectors/base.py ErrorBurstDetector._classify（两倍阈值跳 HIGH）。
func (d *ErrorBurstDetector) classify(windowCount int) *core.Severity {
	var s core.Severity
	switch {
	case d.consecutive >= d.consecutiveThreshold*2 || windowCount >= d.rateThreshold*2:
		s = core.SeverityHigh
	case d.consecutive >= d.consecutiveThreshold || windowCount >= d.rateThreshold:
		s = core.SeverityMedium
	default:
		return nil
	}
	return &s
}

// errTextOrNil 错误文本或 nil（typed-nil 防御：*string nil 指针对齐 Python None）。
func errTextOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
