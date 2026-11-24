//go:build integration

package agentobservability_test

import (
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	obsdemand "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability/demand"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	extdemand "github.com/uapclaw/uapclaw-go/internal/openuapclaw/extensions/observability/demand"
	hconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/harness_config"
	harnessobs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/observability"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ObservabilityChainSuite 采集链端到端集成（15.24a W9 前半）。
// 对照 Python: harness/observability/ 行为——真跑 DeepAgent 验证
// "埋点→provider→trajectory processor 订阅 Drain→TrajectoryToMessages 重放"。
//
// 运行方式: go test -tags=integration ./tests/integration/agentcore/agentobservability/...
type ObservabilityChainSuite struct {
	isuite.AgentSuite
}

// TestObservabilityChainSuite 运行采集链端到端套件。
func TestObservabilityChainSuite(t *testing.T) {
	suite.Run(t, new(ObservabilityChainSuite))
}

// testObservabilityConfig 测试观测配置（全采样、console 导出）。
func testObservabilityConfig() *obs.ObservabilityConfig {
	return &obs.ObservabilityConfig{
		Enabled:     true,
		ServiceName: "agentobservability-itest",
		Exporter:    "console",
		SampleRate:  1,
	}
}

// trajectoryProcessor 取进程级 trajectory processor 具体类型（订阅/Drain 用）。
func trajectoryProcessor() *trajectory.TrajectorySpanProcessor {
	if p, ok := obsdemand.GetTrajectorySpanProcessor().(*trajectory.TrajectorySpanProcessor); ok {
		return p
	}
	return nil
}

// TestObs_真跑DeepAgent采数 真跑 DeepAgent 整轮 invoke，验证 span 落进
// trajectory processor 且能重放出消息序列。断言链（非符号存在）：
// 订阅 Drain 非空 → Trajectory 非 nil → TrajectoryToMessages 重放出消息。
func (s *ObservabilityChainSuite) TestObs_真跑DeepAgent采数() {
	ctx := s.Ctx

	existed, err := harnessobs.AcquireObservability(testObservabilityConfig())
	s.Require().NoError(err, "AcquireObservability 失败")
	s.Require().False(existed, "测试进程内 provider 应为本用例首次创建")
	defer harnessobs.ReleaseObservability()

	proc := trajectoryProcessor()
	s.Require().NotNil(proc, "trajectory processor 单例应为具体类型")

	// 宿主开运行根（单 agent 的根来自 OpenAgentRunSpan）
	root := harnessobs.OpenAgentRunSpan("obs-sess-1", "agent.fast", "", "", "", 0, nil)
	s.Require().NotNil(root, "tracing 已开时运行根必须创建")
	defer root.End()

	handle, err := proc.Subscribe([]string{"llm", "tool", "agent"}, "")
	s.Require().NoError(err, "订阅失败")
	defer proc.Unsubscribe(handle)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("观测链采数完成。"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 4,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("obs-sess-1")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")
	result, err := agent.Invoke(ctx, map[string]any{"query": "只回一句话"}, agentinterfaces.WithSession(sess))
	sess.PostRun(ctx)
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	// 轮询等待 span End 落进 processor（同步路径本应即时，防御批处理延迟）
	var traj *trajectory.Trajectory
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		traj, _ = proc.Drain(handle)
		if traj != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.Require().NotNil(traj, "Drain 应产出 Trajectory（采到了 span）")

	messages, msgErr := trajectory.TrajectoryToMessages(traj, nil, false)
	s.Require().NoError(msgErr, "消息重放失败")
	s.NotEmpty(messages, "TrajectoryToMessages 应重放出消息序列")
}

// TestObs_TracingOff门卫 观测未开启时不产 rail、不造根，Invoke 仍正常。
// Python: maybe_agent_observability_rail / open_agent_run_span 的双道门卫。
func (s *ObservabilityChainSuite) TestObs_TracingOff门卫() {
	ctx := s.Ctx

	s.Assert().Nil(harnessobs.MaybeAgentObservabilityRail(), "观测未初始化时守卫应返回 nil")
	s.Assert().Nil(harnessobs.OpenAgentRunSpan("obs-sess-2", "agent.fast", "", "", "", 0, nil),
		"观测未开启时运行根应返回 nil")

	proc := trajectoryProcessor()
	handle, err := proc.Subscribe([]string{"llm", "tool", "agent"}, "")
	s.Require().NoError(err)
	defer proc.Unsubscribe(handle)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("无观测也不影响运行。"))
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{MaxIterations: 4})
	s.Require().NoError(err)
	sess := s.NewTestSession("obs-sess-2")
	s.Require().NoError(sess.PreRun(ctx))
	_, invokeErr := agent.Invoke(ctx, map[string]any{"query": "只回一句话"}, agentinterfaces.WithSession(sess))
	sess.PostRun(ctx)
	s.Require().NoError(invokeErr, "观测关闭不得影响主链路")

	traj, _ := proc.Drain(handle)
	s.Assert().Nil(traj, "观测关闭时不应采到 span")
}

// TestObs_Demand引用计数 provider demand 的复用与跨 runtime 语义。
// Python: acquire_observability_demand / release_observability_demand
// （demand.py:118-200）——_ACTIVE_RUNTIMES 是 set[str]：同 runtime 二次
// acquire 早退（不叠加计数），仅不同 runtime 才构成多 demand；最后一个
// demand 释放且 provider 由协调者创建时才 shutdown。
func (s *ObservabilityChainSuite) TestObs_Demand引用计数() {
	// 同 runtime 二次 acquire：早退复用（Python demand.py:152-153）。
	existed1, err := harnessobs.AcquireObservability(testObservabilityConfig())
	s.Require().NoError(err)
	s.Require().False(existed1, "首次 acquire 应创建 provider")
	existed2, err := harnessobs.AcquireObservability(testObservabilityConfig())
	s.Require().NoError(err)
	s.Require().True(existed2, "同 runtime 二次 acquire 应复用 provider")

	// 同 runtime 只记一次 demand：一次 release 即拆 provider
	// （Python _ACTIVE_RUNTIMES.discard 后集合为空 → shutdown）。
	harnessobs.ReleaseObservability()
	s.Assert().False(harnessobs.IsInitialized(), "同 runtime 仅记一个 demand，释放即 shutdown")

	// 跨 runtime 双 demand：team 释放后 provider 仍在（agent 仍持有）；
	// agent 释放后才 shutdown。
	teamInitializer := func(config *obs.ObservabilityConfig, processors []sdktrace.SpanProcessor) error {
		return obs.InitObservability(config, obs.WithAdditionalSpanProcessors(processors...))
	}
	teamExisted, err := extdemand.AcquireObservabilityDemand("team", testObservabilityConfig(), teamInitializer)
	s.Require().NoError(err)
	s.Require().False(teamExisted, "team 首次 acquire 创建 provider")
	agentExisted, err := extdemand.AcquireObservabilityDemand("agent", testObservabilityConfig(), teamInitializer)
	s.Require().NoError(err)
	s.Require().True(agentExisted, "agent 在 team 之后 acquire 应复用 provider")
	s.Assert().True(harnessobs.IsInitialized(), "双 demand 期间 provider 在")

	extdemand.ReleaseObservabilityDemand("team")
	s.Assert().True(harnessobs.IsInitialized(), "team 释放后 agent demand 仍持有 provider")
	extdemand.ReleaseObservabilityDemand("agent")
	s.Assert().False(harnessobs.IsInitialized(), "最后一个 demand 释放后 provider 关闭")
}
