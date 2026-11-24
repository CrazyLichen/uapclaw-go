//go:build integration

// Package agentteams_test 团队可靠性全链集成（25.1g 补强，spec §4 例 1/2/3 全链版）。
//
// 例 1（F01）：MockLLM 驱动完整成员轮次——reliability rail 真实钩子采集 →
// repeat_tool detector → LocalAutoRemediator → PushSteering → 下一轮模型输入
// 含纠偏文案（agentcore runner 编排 + rails/reliability + 工具边界）。
// 例 2（F02）：EventDispatcher 真实 Dispatch 链——粗筛 + framework 分发 +
// reliability handler 路由 → leader 轮次 deliver_input（coordination +
// reliability + handlers）。
// 例 3（F03）：leader 自检全链——rail 钩子 emit → monitor → LocalAnomalyReporter
// sink → ReliabilityHandler.HandleLocalAnomaly → pending route notices。
package agentteams_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	mockllm "github.com/uapclaw/uapclaw-go/tests/integration/mockllm"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination"
	handlers "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/handlers"
	coordtypes "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
	schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	hconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/harness_config"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 测试工具 ────────────────────────────

// echoTool 确定性回声工具（同参恒同结果，喂 repeat_tool detector）。
type echoTool struct {
	toolInstance tool.Tool
}

// newEchoTool 创建回声工具。
func newEchoTool(name string) *echoTool {
	e := &echoTool{}
	tc := tool.NewToolCardWithID(name, name, "回声测试工具: "+name, nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"output": "same"}, nil
		}, nil,
	)
	e.toolInstance = t
	return e
}

// Tool 返回底层 tool.Tool。
func (e *echoTool) Tool() tool.Tool { return e.toolInstance }

// invokeMessagesText 拼接一次模型调用收到的全部文本（断言 prompt 内容；
// 三分支对齐 MessagesParam：text / messages / dicts）。
func invokeMessagesText(messages model_clients.MessagesParam) string {
	var b strings.Builder
	if messages.IsText() {
		b.WriteString(messages.Text())
		b.WriteString("\n")
	}
	for _, msg := range messages.Messages() {
		if msg == nil {
			continue
		}
		b.WriteString(msg.GetContent().Text())
		b.WriteString("\n")
	}
	for _, dict := range messages.Dicts() {
		if content, ok := dict["content"].(string); ok {
			b.WriteString(content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ──────────────────────────── 套件 ────────────────────────────

// ReliabilityFullChainSuite 全链集成套件。
type ReliabilityFullChainSuite struct {
	isuite.AgentSuite
}

// TestReliabilityFullChainSuite 套件入口。
func TestReliabilityFullChainSuite(t *testing.T) {
	suite.Run(t, new(ReliabilityFullChainSuite))
}

// fullChainReliabilityCfg 收紧阈值的可靠性配置（low→local_steer，repeat_warn=2）。
func fullChainReliabilityCfg() *core.ReliabilityConfig {
	cfg := core.DefaultReliabilityConfig()
	cfg.Enabled = true
	cfg.Detectors.RepeatTool.RepeatWarn = 2
	cfg.Policy.SeverityActions["low"] = []string{"local_steer", "observe_only"}
	return cfg
}

// ──────────────────────────── 例 1：MockLLM 全链 ────────────────────────────

// TestF01_成员轮次_纠偏文案进入模型输入 MockLLM 驱动完整成员轮次：
// 同参同结果工具调用 ×3（repeat_warn=2 收紧）→ repeat_tool LOW → 本地 steer →
// 下一轮模型调用的消息历史含纠偏文案。
func (s *ReliabilityFullChainSuite) TestF01_成员轮次_纠偏文案进入模型输入() {
	ctx := s.Ctx

	echo := newEchoTool("echo_tool")
	rail := reliability.BuildReliabilityRail(reliability.BuildComponentsDeps{
		Config: fullChainReliabilityCfg(), MemberName: "coder", TeamName: "t", SenderId: "coder", IsLeader: false,
	})

	steeringCh := make(chan string, 8)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("echo_tool", `{"id": "x"}`),
		mockllm.CreateToolCallResponse("echo_tool", `{"id": "x"}`),
		mockllm.CreateToolCallResponse("echo_tool", `{"id": "x"}`),
		mockllm.CreateTextResponse("已改用其他方式完成任务。"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{echo.Tool()},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 8,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("reliability-f01")
	s.Require().NoError(sess.PreRun(ctx))

	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, runErr := agent.Invoke(ctx, map[string]any{
			"query":           "连续执行三次 echo_tool",
			"_steering_queue": steeringCh,
		}, agentinterfaces.WithSession(sess))
		if runErr != nil {
			errCh <- runErr
			return
		}
		done <- result
	}()

	select {
	case <-done:
	case runErr := <-errCh:
		s.Require().NoError(runErr, "成员轮次失败")
	case <-s.Ctx.Done():
		s.Require().Fail("轮次超时")
	}

	// 全链断言：某次模型调用的输入里含纠偏文案（steering 注入历史）
	found := false
	for i := 0; i < s.MockLLM.InvokeCallCount(); i++ {
		call := s.MockLLM.GetInvokeCall(i)
		if call != nil && strings.Contains(invokeMessagesText(call.Messages), "请停止重复无效操作") {
			found = true
			break
		}
	}
	s.True(found, "期望某次模型调用的输入含纠偏文案（rail→detector→remediator→steer→模型输入）")
}

// ──────────────────────────── 例 2：dispatcher 全链 ────────────────────────────

// fullChainHost 走 dispatcher 真实粗筛的 host 桩（round + lifecycle）。
type fullChainHost struct {
	mu        sync.Mutex
	delivered []string
}

func (h *fullChainHost) AutoStartMember(_ context.Context, _ string) bool { return true }
func (h *fullChainHost) IsAgentReady() bool                               { return true }
func (h *fullChainHost) IsAgentRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return false
}
func (h *fullChainHost) HasInFlightRound() bool            { return false }
func (h *fullChainHost) HasPendingInterrupt() bool         { return false }
func (h *fullChainHost) CancelAgent(context.Context) error { return nil }
func (h *fullChainHost) ResumeInterrupt(context.Context, *interaction.InteractiveInput) error {
	return nil
}
func (h *fullChainHost) AnnounceTaskRevisions(context.Context) error            { return nil }
func (h *fullChainHost) ShutdownSelf(context.Context) error                     { return nil }
func (h *fullChainHost) ConcludeCompletedRound(context.Context, int, int) error { return nil }
func (h *fullChainHost) DeliverInput(_ context.Context, content string, _ bool, runContext *agentinterfaces.RunContext) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivered = append(h.delivered, content)
	return nil
}

// Delivered 返回已投递内容。
func (h *fullChainHost) Delivered() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.delivered...)
}

// fullChainBlueprint leader 蓝图桩（启用可靠性）。
type fullChainBlueprint struct{}

func (f *fullChainBlueprint) Role() schema.TeamRole        { return schema.TeamRoleLeader }
func (f *fullChainBlueprint) DispatcherMemberName() string { return "leader" }
func (f *fullChainBlueprint) DispatcherSpec() *schema.TeamAgentSpec {
	return nil
}
func (f *fullChainBlueprint) DispatcherReliability() *core.ReliabilityConfig {
	return fullChainReliabilityCfg()
}

// TestF02_跨进程上报_经dispatcher真实分发链 成员异常事件 → NewEventDispatcher
// Dispatch（粗筛 + framework 分发）→ reliability handler 路由 → leader 轮次
// deliver_input；同 (deployment, reason, status) 二次上报被丢弃。
func (s *ReliabilityFullChainSuite) TestF02_跨进程上报_经dispatcher真实分发链() {
	host := &fullChainHost{}
	d := coordination.NewEventDispatcher(host, &fullChainBlueprint{}, nil, nil)
	s.Require().NotNil(d.Reliability, "blueprint 启用可靠性 → reliability handler 装配")

	verdict := map[string]any{
		"model_failure": map[string]any{
			"route_case": "switchable", "reason": "deterministic",
			"failures": []any{map[string]any{"deployment_id": "dep1", "status_code": 401}},
		},
	}
	anomalyEvent := func(member, summary string) coordtypes.CoordinationEvent {
		return coordtypes.CoordinationEvent{Transport: &events.EventMessage{
			EventType: events.TeamEventAnomalyDetected,
			Payload: map[string]any{
				"team_name": "t", "member_name": member, "kind": "model_route_notice",
				"severity": "medium", "summary": summary, "detector": "model_error",
				"evidence": verdict,
			},
		}}
	}

	// 两个成员先后上报同一故障：经真实 Dispatch 链只通告一次
	d.Dispatch(s.Ctx, anomalyEvent("coder", "s1"))
	d.Dispatch(s.Ctx, anomalyEvent("writer", "s2"))

	delivered := host.Delivered()
	s.Require().Len(delivered, 1, "同 (deployment, reason, status) 只通告一次（经粗筛+分发表全链）")
	s.Contains(delivered[0], "[模型路由通知]")
}

// ──────────────────────────── 例 3：leader 自检全链 ────────────────────────────

// TestF03_自检全链_本地sink与pendingNotices leader rail 钩子 emit → monitor →
// LocalAnomalyReporter sink → handler.HandleLocalAnomaly：
// route notice 进 pending（rail OnUserMessage 前插），不可用裁决仅告警不投递。
func (s *ReliabilityFullChainSuite) TestF03_自检全链_本地sink与pendingNotices() {
	// leader 组件：local reporter + policy + pingpong
	comps := reliability.BuildReliabilityComponents(reliability.BuildComponentsDeps{
		Config: fullChainReliabilityCfg(), MemberName: "leader", TeamName: "t", SenderId: "leader", IsLeader: true,
	})
	host := &fullChainHost{}
	handler := handlers.NewReliabilityHandler(host, &fullChainBlueprint{}, nil, nil,
		reliability.BuildRemediationPolicy(fullChainReliabilityCfg()),
		reliability.BuildPingPongDetector(fullChainReliabilityCfg()))

	// 绑定（TeamAgent.registerReliabilityLocalSink 的装配动作）
	rail := reliability.ReliabilityRailFromComponents(comps)
	rail.BindLocalSink(func(_ context.Context, anomaly *core.Anomaly) {
		handler.HandleLocalAnomaly(context.Background(), anomaly)
	})
	rail.BindRouteNoticeSource(handler.TakePendingRouteNotices)

	// leader 自检：模型调用异常（带路由裁决）→ 进 pending 而非开新轮
	verdict := map[string]any{
		"route_case": "switchable", "reason": "deterministic",
		"failures": []any{map[string]any{"deployment_id": "depX", "status_code": 401}},
	}
	// 经真实钩子链触发：OnModelException emit → monitor.Feed → localReporter
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	cbc.SetException(errWithDetails(verdict))
	s.Require().NoError(rail.OnModelException(context.Background(), cbc))

	s.Empty(host.Delivered(), "本地 route notice 不开新轮（等下一次被受理输入）")

	// OnUserMessage 前插：通告经 drain 缝折进 leader 下一次被受理输入的最前面
	// （不显式 take——rail.OnUserMessage 内部经 BindRouteNoticeSource 消费）
	parts := &agentinterfaces.UserMessageInputs{Parts: []string{"用户问题"}}
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, parts, nil)
	s.Require().NoError(rail.OnUserMessage(context.Background(), cbc2))
	s.Require().Len(parts.Parts, 2, "通告前插 + 原输入")
	s.Contains(parts.Parts[0], "[模型路由通知]")
	// take 清空语义：再次 OnUserMessage 不重复前插
	parts2 := &agentinterfaces.UserMessageInputs{Parts: []string{"第二个问题"}}
	s.Require().NoError(rail.OnUserMessage(context.Background(),
		agentinterfaces.NewAgentCallbackContext(nil, parts2, nil)))
	s.Equal([]string{"第二个问题"}, parts2.Parts, "take 清空后无重复通告")
}

// errWithDetails 构造带路由裁决 details 的异常（exception.BaseError 链）。
func errWithDetails(verdict map[string]any) error {
	return exception.NewBaseError(exception.StatusError,
		exception.WithDetails(map[string]any{
			"route_case": verdict["route_case"], "reason": verdict["reason"],
			"failures": verdict["failures"], "usable": []any{},
			"pinned_model": "", "user_action_required": false,
		}))
}
