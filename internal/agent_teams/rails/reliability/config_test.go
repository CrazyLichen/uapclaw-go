// Package reliability 配置层单元测试。
//
// 对应 Python：config.py pydantic 默认值链。
package reliability

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultReliabilityConfig_总门卫默认关闭(t *testing.T) {
	cfg := DefaultReliabilityConfig()
	assert.False(t, cfg.Enabled, "enabled 默认 False：既有团队零影响")
	assert.Equal(t, []string{"leader", "teammate"}, cfg.MonitorRoles)
}

func TestDefaultReliabilityConfig_detector阈值逐字段对齐(t *testing.T) {
	d := DefaultReliabilityConfig().Detectors
	// Python config.py ToolErrorConfig
	assert.True(t, d.ToolError.Enabled)
	assert.InDelta(t, 60.0, d.ToolError.WindowSeconds, 1e-9)
	assert.Equal(t, 5, d.ToolError.RateThreshold)
	assert.Equal(t, 3, d.ToolError.ConsecutiveThreshold)
	// RepeatToolConfig
	assert.True(t, d.RepeatTool.Enabled)
	assert.Equal(t, []int{30, 10, 10, 20, 30}, []int{
		d.RepeatTool.HistorySize, d.RepeatTool.RepeatWarn,
		d.RepeatTool.PingpongWarn, d.RepeatTool.LoopBlock, d.RepeatTool.GlobalStop,
	})
	// ModelErrorConfig
	assert.True(t, d.ModelError.Enabled)
	assert.InDelta(t, 120.0, d.ModelError.WindowSeconds, 1e-9)
	assert.Equal(t, 3, d.ModelError.RateThreshold)
	assert.Equal(t, 2, d.ModelError.ConsecutiveThreshold)
	// OutputLengthConfig
	assert.True(t, d.OutputLength.Enabled)
	assert.Equal(t, 32000, d.OutputLength.TextThreshold)
	assert.Equal(t, 16000, d.OutputLength.ThinkingThreshold)
	// CompactionConfig
	assert.True(t, d.Compaction.Enabled)
	assert.InDelta(t, 300.0, d.Compaction.WindowSeconds, 1e-9)
	assert.Equal(t, 3, d.Compaction.FrequencyThreshold)
	assert.InDelta(t, 0.3, d.Compaction.DropRatio, 1e-9)
	// PingPongConfig
	assert.True(t, d.PingPong.Enabled)
	assert.Equal(t, 6, d.PingPong.MinVolleys)
}

func TestDefaultReliabilityConfig_policy与预算(t *testing.T) {
	cfg := DefaultReliabilityConfig()
	// Python remediation/policy.py DEFAULT_SEVERITY_ACTIONS 分层 leader-first
	assert.Equal(t, []string{"observe_only"}, cfg.Policy.SeverityActions["low"])
	assert.Equal(t, []string{"report_leader"}, cfg.Policy.SeverityActions["medium"])
	assert.Equal(t, []string{"local_steer", "report_leader"}, cfg.Policy.SeverityActions["high"])
	assert.Equal(t, []string{"local_steer", "escalate_user"}, cfg.Policy.SeverityActions["critical"])
	// RestartIntensityConfig
	assert.Equal(t, 5, cfg.RestartIntensity.Intensity)
	assert.InDelta(t, 60.0, cfg.RestartIntensity.PeriodSeconds, 1e-9)
}

func TestDefaultSeverityActionStrings_表拷贝隔离(t *testing.T) {
	a := DefaultSeverityActionStrings()
	b := DefaultSeverityActionStrings()
	assert.Equal(t, a, b)
	a["low"][0] = "mutated"
	assert.NotEqual(t, a["low"], b["low"], "两次调用不得共享底层数组")
}
