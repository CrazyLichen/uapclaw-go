// Package reliability 单元测试（monitor / reporter / factory / rail）。
//
// 对应 Python：monitor.py / reporter.py / factory.py / rail.py 行为内联断言。
package reliability

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/detectors"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/remediation"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeReporter 记录上报的异常。
type fakeReporter struct{ got []*core.Anomaly }

func (f *fakeReporter) Report(a *core.Anomaly) { f.got = append(f.got, a) }

// panicDetector observe 时 panic 的坏 detector。
type panicDetector struct{}

func (panicDetector) Name() string                         { return "panic" }
func (panicDetector) Observe(*core.Signal) *core.Anomaly   { panic("boom") }
func (panicDetector) Reset()                               {}

// silentDetector 恒返回 nil。
type silentDetector struct{}

func (silentDetector) Name() string                       { return "silent" }
func (silentDetector) Observe(*core.Signal) *core.Anomaly { return nil }
func (silentDetector) Reset()                             {}

// mediumAnomaly 构造 MEDIUM 异常。
func mediumAnomaly() *core.Anomaly {
	return &core.Anomaly{Detector: "t", Kind: core.AnomalyKindToolErrorRate, Severity: core.SeverityMedium, MemberName: "c", Summary: "s"}
}

// ──────────────────────────── Monitor ────────────────────────────

func TestReliabilityMonitor_扇出并按策略路由(t *testing.T) {
	rep := &fakeReporter{}
	// 默认策略 MEDIUM → report_leader；借 ToolErrorRateDetector 两次失败触发
	d := detectors.NewToolErrorRateDetector(60, 5, 2, func() float64 { return 0 })
	m2 := NewReliabilityMonitor([]detectors.Detector{silentDetector{}, d}, rep, remediation.NewRemediationPolicy(nil))
	m2.Feed(&core.Signal{Kind: core.SignalKindToolException, MemberName: "c"})
	produced := m2.Feed(&core.Signal{Kind: core.SignalKindToolException, MemberName: "c"})
	assert.Len(t, produced, 1)
	assert.Len(t, rep.got, 1, "MEDIUM 按默认策略上报 leader")
}

func TestReliabilityMonitor_ObserveOnly仅日志(t *testing.T) {
	rep := &fakeReporter{}
	p := remediation.NewRemediationPolicy(map[core.Severity][]remediation.RemediationAction{
		core.SeverityMedium: {remediation.RemediationActionObserveOnly},
	})
	d := detectors.NewToolErrorRateDetector(60, 5, 2, func() float64 { return 0 })
	m := NewReliabilityMonitor([]detectors.Detector{d}, rep, p)
	m.Feed(&core.Signal{Kind: core.SignalKindToolException, MemberName: "c"})
	m.Feed(&core.Signal{Kind: core.SignalKindToolException, MemberName: "c"})
	assert.Empty(t, rep.got, "observe-only 不上报")
}

func TestReliabilityMonitor_坏detector隔离(t *testing.T) {
	rep := &fakeReporter{}
	d := detectors.NewToolErrorRateDetector(60, 5, 2, func() float64 { return 0 })
	m := NewReliabilityMonitor([]detectors.Detector{panicDetector{}, d}, rep, remediation.NewRemediationPolicy(nil))
	m.Feed(&core.Signal{Kind: core.SignalKindToolException, MemberName: "c"})
	produced := m.Feed(&core.Signal{Kind: core.SignalKindToolException, MemberName: "c"})
	assert.Len(t, produced, 1, "坏 detector panic 被隔离，好 detector 正常产出")
}

func TestReliabilityMonitor_Reset(t *testing.T) {
	d := detectors.NewToolErrorRateDetector(60, 5, 2, func() float64 { return 0 })
	m := NewReliabilityMonitor([]detectors.Detector{d}, &fakeReporter{}, remediation.NewRemediationPolicy(nil))
	m.Reset() // 不 panic 即通过（级联 Reset）
}

// ──────────────────────────── LocalAnomalyReporter ────────────────────────────

func TestLocalAnomalyReporter_未绑定时告警丢弃(t *testing.T) {
	r := NewLocalAnomalyReporter()
	assert.NotPanics(t, func() { r.Report(mediumAnomaly()) })
	assert.Nil(t, r.TakeRouteNotices(), "未 bind source 返回空")
}

func TestLocalAnomalyReporter_绑定后直达sink(t *testing.T) {
	r := NewLocalAnomalyReporter()
	got := 0
	r.Bind(func(*core.Anomaly) { got++ })
	r.BindRouteNoticeSource(func() []string { return []string{"n1", "n2"} })
	r.Report(mediumAnomaly())
	assert.Equal(t, 1, got)
	assert.Equal(t, []string{"n1", "n2"}, r.TakeRouteNotices())
}

// ──────────────────────────── Factory ────────────────────────────

func TestBuildMemberDetectors_按enabled顺序构造(t *testing.T) {
	cfg := core.DefaultReliabilityConfig().Detectors
	dets := BuildMemberDetectors(cfg, func() float64 { return 0 }, nil)
	names := make([]string, 0, len(dets))
	for _, d := range dets {
		names = append(names, d.Name())
	}
	// Python factory.build_member_detectors 顺序：tool_error → repeat_tool →
	// model_error → output_length → compaction（pingpong 不在成员内）
	assert.Equal(t, []string{
		"tool_error_rate", "repeat_tool_call", "model_error", "output_length", "frequent_compaction",
	}, names)
}

func TestBuildMemberDetectors_全禁用(t *testing.T) {
	cfg := core.DefaultReliabilityConfig().Detectors
	cfg.ToolError.Enabled = false
	cfg.RepeatTool.Enabled = false
	cfg.ModelError.Enabled = false
	cfg.OutputLength.Enabled = false
	cfg.Compaction.Enabled = false
	assert.Empty(t, BuildMemberDetectors(cfg, nil, nil))
}

func TestBuildReliabilityComponents_leader走本地sink(t *testing.T) {
	cfg := core.DefaultReliabilityConfig()
	cfg.Enabled = true
	comps := BuildReliabilityComponents(BuildComponentsDeps{
		Config: cfg, MemberName: "leader", TeamName: "t", SenderId: "leader", SessionId: "s", IsLeader: true,
	})
	assert.NotNil(t, comps.LocalReporter, "leader 得 LocalAnomalyReporter")
	assert.NotNil(t, comps.AutoRemediator)
	assert.NotNil(t, comps.Monitor)
	assert.Equal(t, "leader", comps.MemberName)
}

func TestBuildReliabilityComponents_成员走事件上报(t *testing.T) {
	cfg := core.DefaultReliabilityConfig()
	comps := BuildReliabilityComponents(BuildComponentsDeps{
		Config: cfg, MemberName: "coder", TeamName: "t", SenderId: "coder", SessionId: "s",
	})
	assert.Nil(t, comps.LocalReporter)
	assert.NotNil(t, comps.Monitor)
}

func TestBuildReliabilityRail_一步构建(t *testing.T) {
	cfg := core.DefaultReliabilityConfig()
	rail := BuildReliabilityRail(BuildComponentsDeps{
		Config: cfg, MemberName: "m", TeamName: "t", SenderId: "m", SessionId: "s",
	})
	assert.Equal(t, RailPriority, rail.Priority())
	assert.Equal(t, "m", rail.member)
}

func TestReliabilityRailFromComponents_新rail共享有状态核(t *testing.T) {
	cfg := core.DefaultReliabilityConfig()
	comps := BuildReliabilityComponents(BuildComponentsDeps{Config: cfg, MemberName: "m"})
	r1 := ReliabilityRailFromComponents(comps)
	r2 := ReliabilityRailFromComponents(comps)
	assert.Same(t, comps.Monitor, r1.monitor, "跨周期复用有状态核")
	assert.Same(t, comps.Monitor, r2.monitor)
	assert.NotSame(t, r1, r2, "rail 实例每周期新建")
}

func TestBuildPingPongDetector(t *testing.T) {
	cfg := core.DefaultReliabilityConfig()
	assert.Equal(t, "ping_pong", BuildPingPongDetector(cfg).Name())
}

// ──────────────────────────── Rail ────────────────────────────

func TestReliabilityRail_Priority五(t *testing.T) {
	cfg := core.DefaultReliabilityConfig()
	rail := BuildReliabilityRail(BuildComponentsDeps{Config: cfg, MemberName: "m"})
	assert.Equal(t, 5, rail.Priority())
	assert.NotEmpty(t, rail.GetCallbacks(), "GetCallbacks 必须覆盖接口方法（不得命名 Callbacks()）")
}

func TestReliabilityRail_BindLocalSink非leaderNoOp(t *testing.T) {
	cfg := core.DefaultReliabilityConfig()
	rail := BuildReliabilityRail(BuildComponentsDeps{Config: cfg, MemberName: "coder"})
	assert.NotPanics(t, func() { rail.BindLocalSink(func(*core.Anomaly) {}) }, "localReporter 为 nil 时 no-op")
	assert.NotPanics(t, func() { rail.BindRouteNoticeSource(func() []string { return nil }) })
}

func TestRailModelFailure_因果链提取(t *testing.T) {
	// 构造带 Details 的错误链：BaseError → wrapped
	root := exceptionWithRouteCase()
	wrapped := errors.New("wrapped: " + root.Error())
	// wrapped 无 Details、无 Unwrap → 找不到
	assert.Nil(t, modelFailure(wrapped))
	// 直接传 BaseError → 命中白名单
	got := modelFailure(root)
	assert.NotNil(t, got)
	assert.Equal(t, "switchable", got["route_case"])
	for _, key := range modelFailureFields {
		_, ok := got[key]
		assert.True(t, ok, "白名单字段 %s 必须存在", key)
	}
}

func TestRailArgsAsDict(t *testing.T) {
	assert.Nil(t, argsAsDict(nil))
	m := map[string]any{"a": 1}
	assert.Equal(t, m, argsAsDict(m))
	got := argsAsDict(`{"a": 1}`)
	assert.Equal(t, map[string]any{"a": float64(1)}, got)
	assert.Nil(t, argsAsDict("not json"))
	assert.Nil(t, argsAsDict(42))
}

func TestRailErrText(t *testing.T) {
	assert.Equal(t, "error", *errText(nil))
	assert.Equal(t, "boom", *errText(errors.New("boom")))
}

func TestRailMeasureResponse(t *testing.T) {
	// 纯文本响应
	msg := userMsgForMeasure("hello", "thinking...")
	tl, thl := measureResponse(msg)
	assert.NotNil(t, tl)
	assert.Equal(t, 5, *tl)
	assert.NotNil(t, thl)
	assert.Equal(t, len([]rune("thinking...")), *thl)
	// 空响应
	tl2, thl2 := measureResponse(userMsgForMeasure("", ""))
	assert.Nil(t, tl2)
	assert.Nil(t, thl2)
}
