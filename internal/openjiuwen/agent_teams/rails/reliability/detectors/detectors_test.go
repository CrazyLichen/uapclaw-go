// Package detectors 单元测试（base/tool_error/output_length）。
//
// 对应 Python：detectors/base.py、tool_error.py、output_length.py 行为内联断言。
package detectors

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/rails/reliability/core"
)

// errSignal 构造错误信号。
func errSignal(kind core.SignalKind, msg string) *core.Signal {
	m := msg
	return &core.Signal{Kind: kind, MemberName: "coder", Error: &m}
}

// okSignal 构造成功信号（无 error）。
func okSignal(kind core.SignalKind) *core.Signal {
	return &core.Signal{Kind: kind, MemberName: "coder"}
}

func TestErrorBurstDetector_成功清零连击(t *testing.T) {
	now := 0.0
	d := NewErrorBurstDetector("t", core.SignalKindToolException, core.SignalKindAfterToolCall,
		core.AnomalyKindToolErrorRate, 60, 5, 3, func() float64 { return now })
	d.Observe(errSignal(core.SignalKindToolException, "e1"))
	d.Observe(errSignal(core.SignalKindToolException, "e2"))
	// 成功信号清 streak 并清已发射级别
	assert.Nil(t, d.Observe(okSignal(core.SignalKindAfterToolCall)))
	assert.Nil(t, d.Observe(errSignal(core.SignalKindToolException, "e3")), "连击已清零，1/3 不触发")
}

func TestErrorBurstDetector_边沿触发只升不降(t *testing.T) {
	now := 0.0
	d := NewErrorBurstDetector("t", core.SignalKindToolException, core.SignalKindAfterToolCall,
		core.AnomalyKindToolErrorRate, 60, 5, 3, func() float64 { return now })
	// 第 3 次连续失败 → MEDIUM
	assert.Nil(t, d.Observe(errSignal(core.SignalKindToolException, "e1")))
	assert.Nil(t, d.Observe(errSignal(core.SignalKindToolException, "e2")))
	a := d.Observe(errSignal(core.SignalKindToolException, "e3"))
	assert.NotNil(t, a)
	assert.Equal(t, core.SeverityMedium, a.Severity)
	// 第 4 次（仍 MEDIUM 档）不再发射
	assert.Nil(t, d.Observe(errSignal(core.SignalKindToolException, "e4")))
	// 第 6 次（连续 ≥ 3*2）→ HIGH，重新发射
	d.Observe(errSignal(core.SignalKindToolException, "e5"))
	a2 := d.Observe(errSignal(core.SignalKindToolException, "e6"))
	assert.NotNil(t, a2)
	assert.Equal(t, core.SeverityHigh, a2.Severity)
}

func TestErrorBurstDetector_窗口速率触发(t *testing.T) {
	now := 0.0
	// consecutive_threshold 很高，只靠窗口速率触发
	d := NewErrorBurstDetector("t", core.SignalKindToolException, core.SignalKindAfterToolCall,
		core.AnomalyKindToolErrorRate, 60, 2, 100, func() float64 { return now })
	assert.Nil(t, d.Observe(errSignal(core.SignalKindToolException, "e1")))
	a := d.Observe(errSignal(core.SignalKindToolException, "e2"))
	assert.NotNil(t, a)
	assert.Equal(t, core.SeverityMedium, a.Severity)
	assert.Equal(t, "t", a.Detector)
	assert.Equal(t, "coder", a.MemberName)
	assert.Equal(t, 2, a.Evidence["consecutive"], "连击统计所有错误，与阈值无关")
	assert.Equal(t, 2, a.Evidence["window_count"])
	assert.Equal(t, 60.0, a.Evidence["window_seconds"])
}

func TestErrorBurstDetector_Summary格式(t *testing.T) {
	now := 0.0
	d := NewErrorBurstDetector("t", core.SignalKindToolException, core.SignalKindAfterToolCall,
		core.AnomalyKindToolErrorRate, 60, 5, 3, func() float64 { return now })
	d.Observe(errSignal(core.SignalKindToolException, "e1"))
	d.Observe(errSignal(core.SignalKindToolException, "e2"))
	a := d.Observe(errSignal(core.SignalKindToolException, "e3"))
	// Python: f"{consecutive} consecutive failures ({window_count} within {window_seconds:.0f}s)"
	assert.Equal(t, "3 consecutive failures (3 within 60s)", a.Summary)
}

func TestErrorBurstDetector_Reset(t *testing.T) {
	now := 0.0
	d := NewErrorBurstDetector("t", core.SignalKindToolException, core.SignalKindAfterToolCall,
		core.AnomalyKindToolErrorRate, 60, 5, 3, func() float64 { return now })
	d.Observe(errSignal(core.SignalKindToolException, "e1"))
	d.Observe(errSignal(core.SignalKindToolException, "e2"))
	d.Observe(errSignal(core.SignalKindToolException, "e3"))
	d.Reset()
	_, fired := d.FiredSeverity()
	assert.False(t, fired)
	assert.Nil(t, d.Observe(errSignal(core.SignalKindToolException, "e4")), "重置后连击从 1 重新起算")
}

func TestToolErrorRateDetector_标识与转调(t *testing.T) {
	now := 0.0
	d := NewToolErrorRateDetector(60, 5, 3, func() float64 { return now })
	assert.Equal(t, "tool_error_rate", d.Name())
	assert.Nil(t, d.Observe(okSignal(core.SignalKindBeforeToolCall)), "无关信号忽略")
	d.Observe(errSignal(core.SignalKindToolException, "e1"))
	d.Observe(errSignal(core.SignalKindToolException, "e2"))
	a := d.Observe(errSignal(core.SignalKindToolException, "e3"))
	assert.NotNil(t, a)
	assert.Equal(t, core.AnomalyKindToolErrorRate, a.Kind)
	d.Reset()
}

func TestOutputLengthDetector_超阈值发射LOW(t *testing.T) {
	d := NewOutputLengthDetector(100, 50)
	textLen := 200
	a := d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "coder", TextLen: &textLen})
	assert.NotNil(t, a)
	assert.Equal(t, core.SeverityLow, a.Severity)
	assert.Equal(t, core.AnomalyKindOutputTooLong, a.Kind)
	assert.Equal(t, "output_too_long: 200 chars exceeds 100", a.Summary, "对齐 Python kind.value 前缀")
	assert.Equal(t, map[string]any{"length": 200, "threshold": 100}, a.Evidence)
}

func TestOutputLengthDetector_同类别只发一次(t *testing.T) {
	d := NewOutputLengthDetector(100, 50)
	textLen := 200
	assert.NotNil(t, d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "c", TextLen: &textLen}))
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "c", TextLen: &textLen}))
	d.Reset()
	assert.NotNil(t, d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "c", TextLen: &textLen}), "reset 后重新发射")
}

func TestOutputLengthDetector_双超限先思考后文本(t *testing.T) {
	d := NewOutputLengthDetector(100, 50)
	textLen, thinkLen := 200, 90
	// 第一次：thinking 先报
	a1 := d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "c", TextLen: &textLen, ThinkingLen: &thinkLen})
	assert.Equal(t, core.AnomalyKindThinkingTooLong, a1.Kind)
	// 第二次：text 报（thinking 已 fired）
	a2 := d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "c", TextLen: &textLen, ThinkingLen: &thinkLen})
	assert.Equal(t, core.AnomalyKindOutputTooLong, a2.Kind)
}

func TestOutputLengthDetector_Nil长度按零(t *testing.T) {
	d := NewOutputLengthDetector(100, 50)
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "c"}))
}

func TestOutputLengthDetector_阈值内不发射(t *testing.T) {
	d := NewOutputLengthDetector(100, 50)
	textLen, thinkLen := 100, 50
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "c", TextLen: &textLen, ThinkingLen: &thinkLen}),
		"等于阈值不超（严格大于才触发）")
	assert.Nil(t, d.Observe(okSignal(core.SignalKindBeforeToolCall)), "无关信号忽略")
}
