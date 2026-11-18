// Package harnessrsi 单 agent 可靠性轨装配测试。
//
// 对应 Python：runtime_reliability.py 行为内联断言。
package harnessrsi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
)

// TestBuildSingleAgentReliabilityRail_真身类型与配置 断言收窄后的签名与硬编码配置。
func TestBuildSingleAgentReliabilityRail_真身类型与配置(t *testing.T) {
	rail, err := BuildSingleAgentReliabilityRail()
	assert.NoError(t, err)
	assert.NotNil(t, rail)
	// 具体类型（签名收窄，D-4 闭环）：*reliability.ReliabilityRail
	assert.Equal(t, reliability.RailPriority, rail.Priority())
	assert.Equal(t, "task_agent", rail.MemberName())
	// is_leader=true → LocalAnomalyReporter：绑定后 sink 可直达（间接断言）
	called := 0
	rail.BindLocalSink(func(context.Context, *core.Anomaly) { called++ })
	rail.Monitor().Reporter().Report(context.Background(), &core.Anomaly{MemberName: "task_agent"})
	assert.Equal(t, 1, called, "leader 本地 sink 直达（经 reporter 缝）")
}

// TestSingleAgentReliabilityConfig_逐字段 对齐 model_validate 的硬编码 dict。
func TestSingleAgentReliabilityConfig_逐字段(t *testing.T) {
	cfg := singleAgentReliabilityConfig()
	assert.True(t, cfg.Enabled)
	// repeat_tool 收紧（Python: history 36, repeat 3, pingpong 6, loop 6, stop 12）
	assert.Equal(t, []int{36, 3, 6, 6, 12}, []int{
		cfg.Detectors.RepeatTool.HistorySize, cfg.Detectors.RepeatTool.RepeatWarn,
		cfg.Detectors.RepeatTool.PingpongWarn, cfg.Detectors.RepeatTool.LoopBlock,
		cfg.Detectors.RepeatTool.GlobalStop,
	})
	assert.False(t, cfg.Detectors.ToolError.Enabled)
	assert.False(t, cfg.Detectors.ModelError.Enabled)
	assert.True(t, cfg.Detectors.OutputLength.Enabled)
	assert.False(t, cfg.Detectors.Compaction.Enabled)
	assert.False(t, cfg.Detectors.PingPong.Enabled)
	// 全档 ["local_steer", "observe_only"]
	for _, level := range []string{"low", "medium", "high", "critical"} {
		assert.Equal(t, []string{"local_steer", "observe_only"}, cfg.Policy.SeverityActions[level])
	}
}

// TestStandaloneRemediator_OUTPUT_TOO_LONG转向文案 断言改写分支。
func TestStandaloneRemediator_OUTPUT_TOO_LONG转向文案(t *testing.T) {
	cfg := singleAgentReliabilityConfig()
	policy := reliability.BuildRemediationPolicy(cfg)
	rem := newStandaloneRemediator(policy, cfg)
	// OUTPUT_TOO_LONG → 专用转向文案（逐字对齐上游）
	got := rem.SteerMessage(&core.Anomaly{
		Kind: core.AnomalyKindOutputTooLong, Severity: core.SeverityLow,
		MemberName: "task_agent", Summary: "output_too_long: 40000 chars exceeds 32000",
	})
	assert.Equal(t, standaloneTruncatedText, got)
	// 其他 kind 走基类默认
	other := rem.SteerMessage(&core.Anomaly{
		Kind: core.AnomalyKindRepeatToolCall, Severity: core.SeverityCritical,
		MemberName: "task_agent", Summary: "loop",
	})
	assert.NotEqual(t, standaloneTruncatedText, other)
	assert.NotEmpty(t, other)
}

// TestStandaloneRemediator_预算与NO_STEER继承基类 断言基类语义未被覆写破坏。
func TestStandaloneRemediator_预算与NO_STEER继承基类(t *testing.T) {
	cfg := singleAgentReliabilityConfig()
	policy := reliability.BuildRemediationPolicy(cfg)
	rem := newStandaloneRemediator(policy, cfg)
	// 模型组裁决类别不 steer（基类 _NO_STEER_KINDS）
	assert.Empty(t, rem.SteerMessage(&core.Anomaly{
		Kind: core.AnomalyKindModelUnavailable, Severity: core.SeverityCritical, MemberName: "task_agent",
	}))
	// 全档策略含 local_steer，预算内可多次
	a := &core.Anomaly{Kind: core.AnomalyKindRepeatToolCall, Severity: core.SeverityCritical, MemberName: "task_agent"}
	assert.NotEmpty(t, rem.SteerMessage(a))
}
