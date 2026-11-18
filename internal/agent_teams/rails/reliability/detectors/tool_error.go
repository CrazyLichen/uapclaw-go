// Package detectors 工具调用错误率检测（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/detectors/tool_error.py
package detectors

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolErrorRateDetector 检测频繁或连续的工具调用失败。
//
// Python: tool_error.py ToolErrorRateDetector（继承 ErrorBurstDetector）。
// 错误以 TOOL_EXCEPTION 信号到达；AFTER_TOOL_CALL（成功）清零连击。
// Go 无继承：以组合字段包装 ErrorBurstDetector 转调（等价改写，非偏差）。
type ToolErrorRateDetector struct {
	// burst 共享错误爆发检测器
	burst *ErrorBurstDetector
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolErrorRateDetector 构造工具错误率检测器。
//
// Python: tool_error.py ToolErrorRateDetector.__init__（默认 60s/5/3）。
func NewToolErrorRateDetector(windowSeconds float64, rateThreshold, consecutiveThreshold int, nowFn func() float64) *ToolErrorRateDetector {
	return &ToolErrorRateDetector{
		burst: NewErrorBurstDetector(
			"tool_error_rate",
			core.SignalKindToolException,
			core.SignalKindAfterToolCall,
			core.AnomalyKindToolErrorRate,
			windowSeconds, rateThreshold, consecutiveThreshold, nowFn,
		),
	}
}

// Name 返回 detector 稳定标识。
func (d *ToolErrorRateDetector) Name() string { return d.burst.Name() }

// Observe 转调共享爆发检测器。
//
// Python: 继承 ErrorBurstDetector.observe。
func (d *ToolErrorRateDetector) Observe(signal *core.Signal) *core.Anomaly { return d.burst.Observe(signal) }

// Reset 重置。
//
// Python: 继承 ErrorBurstDetector.reset。
func (d *ToolErrorRateDetector) Reset() { d.burst.Reset() }
