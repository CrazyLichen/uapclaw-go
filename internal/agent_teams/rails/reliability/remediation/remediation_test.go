// Package remediation 单元测试。
//
// 对应 Python：policy.py / local.py 行为内联断言。
package remediation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
)

func TestRemediationPolicy_默认分层表(t *testing.T) {
	p := NewRemediationPolicy(nil)
	assert.Equal(t, []RemediationAction{RemediationActionObserveOnly}, p.ActionsFor(core.SeverityLow))
	assert.Equal(t, []RemediationAction{RemediationActionReportLeader}, p.ActionsFor(core.SeverityMedium))
	assert.Equal(t, []RemediationAction{RemediationActionLocalSteer, RemediationActionReportLeader}, p.ActionsFor(core.SeverityHigh))
	assert.Equal(t, []RemediationAction{RemediationActionLocalSteer, RemediationActionEscalateUser}, p.ActionsFor(core.SeverityCritical))
	assert.Empty(t, p.ActionsFor(core.Severity("bogus")))
}

func TestRemediationPolicy_构造拷贝隔离(t *testing.T) {
	src := map[core.Severity][]RemediationAction{
		core.SeverityHigh: {RemediationActionLocalSteer},
	}
	p := NewRemediationPolicy(src)
	src[core.SeverityHigh] = append(src[core.SeverityHigh], RemediationActionReportLeader)
	assert.Len(t, p.ActionsFor(core.SeverityHigh), 1, "构造时拷贝，外部改写不影响策略")
}

func TestBuildRemediationPolicy_字符串形状(t *testing.T) {
	p := BuildRemediationPolicy(map[string][]string{
		"low":      {"local_steer", "observe_only"},
		"bogus":    {"report_leader"}, // 未知 severity 丢弃
	})
	assert.Equal(t, []RemediationAction{RemediationActionLocalSteer, RemediationActionObserveOnly}, p.ActionsFor(core.SeverityLow))
	assert.Empty(t, p.ActionsFor(core.Severity("bogus")))
}

func TestBuildRemediationPolicy_Nil用默认表(t *testing.T) {
	p := BuildRemediationPolicy(nil)
	assert.Equal(t, NewRemediationPolicy(nil).ActionsFor(core.SeverityHigh), p.ActionsFor(core.SeverityHigh))
}

// newTestRemediator 构造注入时钟与固定文本的纠偏器。
func newTestRemediator(intensity int) (*LocalAutoRemediator, *float64) {
	now := 0.0
	p := NewRemediationPolicy(nil)
	r := NewLocalAutoRemediator(p, intensity, 60.0, func() float64 { return now })
	r.SetSteerText(func(kind core.AnomalyKind, summary string) string {
		return "steer:" + string(kind)
	})
	return r, &now
}

func TestLocalAutoRemediator_强度预算限流(t *testing.T) {
	r, now := newTestRemediator(5)
	a := &core.Anomaly{Kind: core.AnomalyKindToolErrorRate, Severity: core.SeverityCritical, Summary: "s"}
	for i := 0; i < 5; i++ {
		assert.Equal(t, "steer:tool_error_rate", r.SteerMessage(a), "第 %d 次在预算内", i+1)
	}
	assert.Empty(t, r.SteerMessage(a), "第 6 次超预算返回空")
	// 时间推进越过周期后预算恢复
	*now = 61.0
	assert.Equal(t, "steer:tool_error_rate", r.SteerMessage(a))
}

func TestLocalAutoRemediator_非STEER严重度不出文案(t *testing.T) {
	r, _ := newTestRemediator(5)
	// MEDIUM 策略只有 report_leader（默认表）→ 不 steer
	a := &core.Anomaly{Kind: core.AnomalyKindToolErrorRate, Severity: core.SeverityMedium, Summary: "s"}
	assert.Empty(t, r.SteerMessage(a))
}

func TestLocalAutoRemediator_模型组裁决类别不steer(t *testing.T) {
	r, _ := newTestRemediator(5)
	// Python local.py _NO_STEER_KINDS：自纠偏对模型不可用/路由切换无益
	for _, kind := range []core.AnomalyKind{core.AnomalyKindModelUnavailable, core.AnomalyKindModelRouteNotice} {
		a := &core.Anomaly{Kind: kind, Severity: core.SeverityCritical, Summary: "s"}
		assert.Empty(t, r.SteerMessage(a), "kind=%s", kind)
	}
}

func TestLocalAutoRemediator_Reset(t *testing.T) {
	r, now := newTestRemediator(1)
	a := &core.Anomaly{Kind: core.AnomalyKindToolErrorRate, Severity: core.SeverityCritical, Summary: "s"}
	assert.NotEmpty(t, r.SteerMessage(a))
	assert.Empty(t, r.SteerMessage(a))
	r.Reset()
	assert.NotEmpty(t, r.SteerMessage(a), "重置后预算恢复")
	_ = now
}
