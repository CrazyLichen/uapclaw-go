// Package detectors 超长输出/思考检测（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/detectors/output_length.py
package detectors

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OutputLengthDetector 标记超长的模型输出文本或思考内容。
//
// Python: output_length.py OutputLengthDetector。直接阈值检测（无窗口）：
// 每个 AFTER_MODEL_CALL 信号独立判定。严重度恒为 LOW——超长输出通常是
// 质量提示而非失败——且每类只发射一次直到 Reset()，避免逐轮刷屏。
// 同一次调用双超限时先报 thinking、下一次报 text。
type OutputLengthDetector struct {
	// textThreshold 输出文本长度阈值
	textThreshold int
	// thinkingThreshold 思考内容长度阈值
	thinkingThreshold int
	// fired 已发射类别集合（reset 前每类只发一次）
	fired map[core.AnomalyKind]bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOutputLengthDetector 构造超长输出检测器。
//
// Python: output_length.py OutputLengthDetector.__init__（默认 32000/16000）。
func NewOutputLengthDetector(textThreshold, thinkingThreshold int) *OutputLengthDetector {
	return &OutputLengthDetector{
		textThreshold:     textThreshold,
		thinkingThreshold: thinkingThreshold,
		fired:             make(map[core.AnomalyKind]bool),
	}
}

// Name 返回 detector 稳定标识。
//
// Python: output_length.py OutputLengthDetector.name。
func (d *OutputLengthDetector) Name() string { return "output_length" }

// Observe 在文本或思考超阈值时发射 LOW 异常。
//
// Python: output_length.py OutputLengthDetector.observe。
func (d *OutputLengthDetector) Observe(signal *core.Signal) *core.Anomaly {
	if signal.Kind != core.SignalKindAfterModelCall {
		return nil
	}
	// Python: signal.thinking_len or 0 — nil 按 0 处理
	thinkingLen := 0
	if signal.ThinkingLen != nil {
		thinkingLen = *signal.ThinkingLen
	}
	if thinkingLen > d.thinkingThreshold && !d.fired[core.AnomalyKindThinkingTooLong] {
		d.fired[core.AnomalyKindThinkingTooLong] = true
		return makeLengthAnomaly(signal.MemberName, core.AnomalyKindThinkingTooLong, thinkingLen, d.thinkingThreshold)
	}
	textLen := 0
	if signal.TextLen != nil {
		textLen = *signal.TextLen
	}
	if textLen > d.textThreshold && !d.fired[core.AnomalyKindOutputTooLong] {
		d.fired[core.AnomalyKindOutputTooLong] = true
		return makeLengthAnomaly(signal.MemberName, core.AnomalyKindOutputTooLong, textLen, d.textThreshold)
	}
	return nil
}

// Reset 清空已发射集合（新一轮）。
//
// Python: output_length.py OutputLengthDetector.reset。
func (d *OutputLengthDetector) Reset() {
	d.fired = make(map[core.AnomalyKind]bool)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeLengthAnomaly 构造携带实测长度的长度异常。
//
// Python: output_length.py OutputLengthDetector._make（staticmethod）。
func makeLengthAnomaly(member string, kind core.AnomalyKind, length, threshold int) *core.Anomaly {
	return &core.Anomaly{
		Detector:   "output_length",
		Kind:       kind,
		Severity:   core.SeverityLow,
		MemberName: member,
		Summary:    fmt.Sprintf("%s: %d chars exceeds %d", kind, length, threshold),
		Evidence: map[string]any{
			"length":    length,
			"threshold": threshold,
		},
	}
}
