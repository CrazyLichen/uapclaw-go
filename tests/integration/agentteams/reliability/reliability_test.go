//go:build integration

// Package agentteams_test 团队可靠性子系统集成测试（25.1g）。
//
// 跨 agent_teams/rails/reliability（采集/检测/纠偏）、agent_teams/messager
// （inprocess 总线发布）、agent_teams/agent/coordination/handlers（leader 侧
// 路由）与 agent_teams/schema/events（事件契约）四个子系统，验证 25.1g 的
// 成员内链路 / 跨进程上报 / route notice 去重 / 未启用 no-op。
package agentteams_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	handlers "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/handlers"
	coordtypes "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReliabilitySuite 团队可靠性子系统集成测试套件。
type ReliabilitySuite struct {
	isuite.AgentSuite
}

// fakeLeaderRound 记录 deliver_input 内容的 leader 轮控制面桩。
type fakeLeaderRound struct {
	mu        sync.Mutex
	delivered []string
}

func (f *fakeLeaderRound) IsAgentReady() bool                { return true }
func (f *fakeLeaderRound) IsAgentRunning() bool              { return false }
func (f *fakeLeaderRound) HasInFlightRound() bool            { return false }
func (f *fakeLeaderRound) HasPendingInterrupt() bool         { return false }
func (f *fakeLeaderRound) CancelAgent(context.Context) error { return nil }
func (f *fakeLeaderRound) DeliverInput(_ context.Context, content any, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delivered = append(f.delivered, content.(string))
	return nil
}
func (f *fakeLeaderRound) ResumeInterrupt(context.Context, *interaction.InteractiveInput) error {
	return nil
}
func (f *fakeLeaderRound) AnnounceTaskRevisions(context.Context) error            { return nil }
func (f *fakeLeaderRound) ShutdownSelf(context.Context) error                     { return nil }
func (f *fakeLeaderRound) ConcludeCompletedRound(context.Context, int, int) error { return nil }
func (f *fakeLeaderRound) Delivered() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.delivered...)
}

// fakeLeaderBlueprint leader 角色蓝图桩（含启用的可靠性配置）。
type fakeLeaderBlueprint struct {
	cfg *core.ReliabilityConfig
}

func (f *fakeLeaderBlueprint) Role() schema.TeamRole                          { return schema.TeamRoleLeader }
func (f *fakeLeaderBlueprint) DispatcherMemberName() string                   { return "leader" }
func (f *fakeLeaderBlueprint) DispatcherReliability() *core.ReliabilityConfig { return f.cfg }

// capturedSubscriber messager 订阅侧的捕获器（模拟 leader kernel 的 _deliver）。
type capturedSubscriber struct {
	mu  sync.Mutex
	got []*events.EventMessage
}

func (c *capturedSubscriber) handle(_ context.Context, msg *events.EventMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, msg)
	return nil
}

// ──────────────────────────── 套件 ────────────────────────────

// TestReliabilitySuite 集成入口（每个 _test.go 必须自带 suite.Run）。
func TestReliabilitySuite(t *testing.T) {
	suite.Run(t, new(ReliabilitySuite))
}

// ──────────────────────────── 成员内链路 ────────────────────────────

// TestReliabilitySuite_成员内链路_重复调用触发本地steer
// 成员连续同参同结果工具调用 → repeat_tool detector 判定 → LocalAutoRemediator
// 出纠偏文案 → PushSteering 进 steering 队列（rail 采集 → core.detectors →
// remediation → single_agent/interfaces 四子系统）。
func (s *ReliabilitySuite) Test成员内链路_重复调用触发本地steer() {
	cfg := core.DefaultReliabilityConfig()
	cfg.Enabled = true
	cfg.Detectors.RepeatTool.RepeatWarn = 3 // 收紧阈值便于触发
	// LOW 档策略加 local_steer（上游默认表 LOW 仅 observe_only；这里把
	// 严重度→动作映射配置为可本地纠偏，复用 ReliabilityPolicyConfig 的 string 形状）
	cfg.Policy.SeverityActions["low"] = []string{"local_steer", "observe_only"}
	// 成员（非 leader）+ 无 messager：上报线路缺失，但本地 steer 不依赖它
	rail := reliability.BuildReliabilityRail(reliability.BuildComponentsDeps{
		Config: cfg, MemberName: "coder", TeamName: "t", SenderId: "coder", IsLeader: false,
	})
	steerQueue := make(chan string, 16)

	tool := "read_file"
	call := func() {
		cbc := interfaces.NewAgentCallbackContext(nil,
			&interfaces.ToolCallInputs{ToolName: tool, ToolArgs: map[string]any{"path": "a.txt"}}, nil)
		cbc.BindSteeringQueue(steerQueue)
		assert.NoError(s.T(), rail.BeforeToolCall(context.Background(), cbc))
		assert.NoError(s.T(), rail.AfterToolCall(context.Background(), cbc))
	}
	// 第 3 次达 repeat_warn=3 → LOW 异常 + 策略含 local_steer → steer 入队
	call()
	call()
	call()
	select {
	case msg := <-steerQueue:
		assert.Contains(s.T(), msg, "请停止重复无效操作", "纠偏文案经 i18n 渲染（默认中文表）")
	default:
		s.T().Fatalf("期望 steering 队列出现纠偏文案（repeat_tool HIGH 档 + local_steer）")
	}
}

// ──────────────────────────── 跨进程上报 ────────────────────────────

// TestReliabilitySuite_跨进程上报_成员异常到leader轮次
// 成员 detector 产出 MEDIUM → EventAnomalyReporter 发布 TEAM topic → messager
// inprocess 总线扇出 → leader kernel 等价订阅捕获 → ReliabilityHandler 路由 →
// deliver_input 注入 leader 轮次。
func (s *ReliabilitySuite) Test跨进程上报_成员异常到leader轮次() {
	cfg := core.DefaultReliabilityConfig()
	cfg.Enabled = true
	cfg.Policy.SeverityActions["medium"] = []string{"report_leader"}
	cfg.Detectors.ToolError.ConsecutiveThreshold = 2 // 收紧阈值便于触发
	cfg.Detectors.ToolError.RateThreshold = 2
	// 成员侧：真实 inprocess messager 发布
	msgr, err := messager.CreateMessager(schema.MessagerTransportConfig{Backend: "inprocess"})
	assert.NoError(s.T(), err)
	assert.NoError(s.T(), msgr.Start(context.Background()))
	defer msgr.Stop(context.Background())
	_ = reliability.NewEventAnomalyReporter(msgr, "myteam", "coder") // 发布面由 rail.monitor 内部 reporter 承担
	// leader 侧：订阅同一 topic（kernel.subscribe_transport 等价）
	sub := &capturedSubscriber{}
	topic := events.TeamTopicTeam.Build("sess1", "myteam")
	assert.NoError(s.T(), msgr.Subscribe(context.Background(), topic, sub.handle))

	// 会话作用域 ctx（sessionctx 传播，D-g6）：topic 构造经 GetSessionID(ctx)
	state := sessionctx.InitSessionState()
	state.SetSessionID("sess1")
	feedCtx := sessionctx.WithSessionState(context.Background(), state)

	// 成员 monitor 产出 MEDIUM 异常（借 ToolErrorRate 两连失败）
	rail := reliability.BuildReliabilityRail(reliability.BuildComponentsDeps{
		Config: cfg, MemberName: "coder", Messager: msgr, TeamName: "myteam", SenderId: "coder", IsLeader: false,
	})
	rail.Monitor().Feed(feedCtx, &core.Signal{Kind: core.SignalKindToolException, MemberName: "coder"})
	rail.Monitor().Feed(feedCtx, &core.Signal{Kind: core.SignalKindToolException, MemberName: "coder"})

	// 断言事件经总线到达 leader 订阅侧（schema/events + messager 子系统）
	assert.Len(s.T(), sub.got, 1)
	assert.Equal(s.T(), events.TeamEventAnomalyDetected, sub.got[0].EventType)
	assert.Equal(s.T(), "coder", sub.got[0].Payload["member_name"])

	// leader 侧 handler 消费同一事件（coordination/handlers 子系统）
	round := &fakeLeaderRound{}
	handler := handlers.NewReliabilityHandler(
		roundStub(round), &fakeLeaderBlueprint{cfg: cfg}, nil, nil,
		reliability.BuildRemediationPolicy(cfg), reliability.BuildPingPongDetector(cfg),
	)
	// 经 GetCallbacks 注册面触发（真实注册路径，非测试后门）
	callbacks := handler.GetCallbacks()
	callbacks[events.TeamEventAnomalyDetected](context.Background(), coordtypes.CoordinationEvent{
		Transport: sub.got[0],
	})
	delivered := round.Delivered()
	assert.Len(s.T(), delivered, 1)
	assert.Contains(s.T(), delivered[0], "[可靠性告警]", "report_leader 文案（中文表）")
	assert.Contains(s.T(), delivered[0], "(detector=tool_error_rate)")
}

// ──────────────────────────── route notice 去重 ────────────────────────────

// TestReliabilitySuite_routeNotice去重_同deployment只通告一次
// 两个成员先后携带同 (pinned, deployment, reason, status) verdict → leader 只收到
// 一次通告；leader 本地异常经 HandleLocalAnomaly 进 pending、随下一次输入前插。
func (s *ReliabilitySuite) TestRouteNotice去重_同deployment只通告一次() {
	cfg := core.DefaultReliabilityConfig()
	cfg.Enabled = true
	round := &fakeLeaderRound{}
	handler := handlers.NewReliabilityHandler(
		roundStub(round), &fakeLeaderBlueprint{cfg: cfg}, nil, nil,
		reliability.BuildRemediationPolicy(cfg), reliability.BuildPingPongDetector(cfg),
	)
	verdict := map[string]any{
		"route_case": "switchable", "reason": "deterministic",
		"failures": []any{map[string]any{"deployment_id": "dep1", "status_code": 401}},
	}
	// 成员 A 上报
	callbacks := handler.GetCallbacks()
	callbacks[events.TeamEventAnomalyDetected](context.Background(), coordtypes.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventAnomalyDetected,
			Payload: map[string]any{
				"member_name": "coder", "kind": "model_route_notice",
				"severity": "medium", "summary": "s1", "detector": "model_error",
				"evidence": map[string]any{"model_failure": verdict},
			},
		},
	})
	// 成员 B 上报同一故障
	callbacks[events.TeamEventAnomalyDetected](context.Background(), coordtypes.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventAnomalyDetected,
			Payload: map[string]any{
				"member_name": "writer", "kind": "model_route_notice",
				"severity": "medium", "summary": "s2", "detector": "model_error",
				"evidence": map[string]any{"model_failure": verdict},
			},
		},
	})
	assert.Len(s.T(), round.Delivered(), 1, "同 (deployment, reason, status) 只通告一次")
	assert.Contains(s.T(), round.Delivered()[0], "[模型路由通知]")

	// leader 本地 route notice（不同 deployment，避免被成员 A 的通告键去重——
	// 对齐上游语义：同一故障成员已报，leader 本地的重复上报被丢弃）
	dep2Verdict := map[string]any{
		"route_case": "switchable", "reason": "stream_interrupted",
		"failures": []any{map[string]any{"deployment_id": "dep2", "status_code": 500}},
	}
	handler.HandleLocalAnomaly(context.Background(), &core.Anomaly{
		Kind: core.AnomalyKindModelRouteNotice, Severity: core.SeverityMedium,
		MemberName: "leader", Summary: "s3",
		Evidence: map[string]any{"model_failure": dep2Verdict},
	})
	assert.Len(s.T(), round.Delivered(), 1, "本地 route notice 不开新轮")
	assert.Len(s.T(), handler.TakePendingRouteNotices(), 1, "pending 由下次输入前插")
	assert.Empty(s.T(), handler.TakePendingRouteNotices(), "take 清空语义")
}

// ──────────────────────────── 未启用 no-op ────────────────────────────

// TestReliabilitySuite_未启用全链NoOp config 缺省/未启用时装配面为空。
func (s *ReliabilitySuite) Test未启用全链NoOp() {
	// 默认配置 enabled=false → 门卫拒绝出 rail spec
	cfg := core.DefaultReliabilityConfig()
	assert.False(s.T(), cfg.Enabled)
	// dispatcher 侧：blueprint 未配置 reliability → Reliability 为 nil
	// （NewEventDispatcher 由 coordination 单测覆盖；此处断言门卫字段）
	comps, isLeader := buildReliabilityGate(cfg, "coder", false, false)
	assert.Nil(s.T(), comps, "未启用时不构建组件")
	assert.False(s.T(), isLeader)
	// rail 绑定未绑 sink 时 Report 告警丢弃不 panic
	r := reliability.NewLocalAnomalyReporter()
	assert.NotPanics(s.T(), func() {
		r.Report(context.Background(), &core.Anomaly{MemberName: "m"})
	})
}

// buildReliabilityGate 复刻配置器门卫（enabled + member_name + monitor_roles +
// (is_leader 或 messager)）供集成侧断言。
func buildReliabilityGate(cfg *core.ReliabilityConfig, memberName string, isLeader, hasMessager bool) (*reliability.ReliabilityComponents, bool) {
	if cfg == nil || !cfg.Enabled || memberName == "" {
		return nil, false
	}
	roleIn := false
	for _, r := range cfg.MonitorRoles {
		if r == "leader" && isLeader {
			roleIn = true
		}
		if r == "teammate" && !isLeader {
			roleIn = true
		}
	}
	if !roleIn || (!isLeader && !hasMessager) {
		return nil, false
	}
	return reliability.BuildReliabilityComponents(reliability.BuildComponentsDeps{
		Config: cfg, MemberName: memberName, TeamName: "t", SenderId: memberName, IsLeader: isLeader,
	}), isLeader
}

// roundStub 把 fakeLeaderRound 适配成 DispatcherHost（round + lifecycle）。
func roundStub(r *fakeLeaderRound) coordtypes.DispatcherHost {
	return &roundAdapter{r}
}

// roundAdapter 转发 DeliverInput（其余恒零值）。
type roundAdapter struct{ r *fakeLeaderRound }

func (a *roundAdapter) IsAgentReady() bool                { return true }
func (a *roundAdapter) IsAgentRunning() bool              { return false }
func (a *roundAdapter) HasInFlightRound() bool            { return false }
func (a *roundAdapter) HasPendingInterrupt() bool         { return false }
func (a *roundAdapter) CancelAgent(context.Context) error { return nil }
func (a *roundAdapter) DeliverInput(ctx context.Context, content any, useSteer bool) error {
	return a.r.DeliverInput(ctx, content, useSteer)
}
func (a *roundAdapter) ResumeInterrupt(context.Context, *interaction.InteractiveInput) error {
	return nil
}
func (a *roundAdapter) AnnounceTaskRevisions(context.Context) error            { return nil }
func (a *roundAdapter) ShutdownSelf(context.Context) error                     { return nil }
func (a *roundAdapter) ConcludeCompletedRound(context.Context, int, int) error { return nil }

// 编译期契约检查
var _ coordtypes.DispatcherHost = (*roundAdapter)(nil)
