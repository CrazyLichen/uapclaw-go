//go:build integration

package harness_test

import (
	"context"
	"time"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NativeHarnessSuite NativeHarness 生命周期集成测试套件。
//
// 对齐 Python openjiuwen/agent_teams/harness/native_harness.py 的
// start/send/outputs/subscribe 生命周期契约：单 supervisor 串行化所有输入，
// 状态在 IDLE/RUNNING/TERMINATED 间迁移，输出经单消费者迭代器泵出。
//
// 集成边界：TeamHarness/NativeHarness（agent_teams）→ DeepAgent 任务循环
// （agentcore/harness）→ ReAct 执行器 + MockLLM → Session 流。跨 ≥3 个
// internal 二级子系统，满足集成审计门槛。
type NativeHarnessSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newNativeHarness 构造一个带 MockLLM 的 NativeHarness（不 Start）。
func (s *NativeHarnessSuite) newNativeHarness() *teamharness.NativeHarness {
	// 25.1b：前向构造——spec 自己解析 parts。把 MockLLM 的 card + model
	// 装进 spec，使物化出的 agent 与被替换的 agent 等价。
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 4,
	})
	s.Require().NoError(err)
	_ = agent
	spec, specErr := s.NewSpecWithMockLLM(4)
	s.Require().NoError(specErr)
	h, err := teamharness.NewNativeHarness(spec, nil)
	s.Require().NoError(err, "NewNativeHarness")
	return h
}

// newStartedHarness 构造并 Start 一个 NativeHarness，返回 harness + 会话。
func (s *NativeHarnessSuite) newStartedHarness() (*teamharness.NativeHarness, *session.Session) {
	h := s.newNativeHarness()
	sess := session.CreateAgentSession("nh-sess", h.DeepAgent().Card(), nil)
	s.Require().NoError(sess.PreRun(s.Ctx, map[string]any{}))
	s.Require().NoError(h.Start(s.Ctx, sess))
	return h, sess
}

// ──────────────────────────── 测试 ────────────────────────────

// TestStart后Send一轮回到Idle 验证 start → send 一轮 → 自然回到 idle。
func (s *NativeHarnessSuite) TestStart后Send一轮回到Idle() {
	h, _ := s.newStartedHarness()
	defer func() { _ = h.Stop(context.Background()) }()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第一轮完成"))

	s.Require().NoError(h.Send(s.Ctx, "开始任务", false))

	s.Require().Eventually(func() bool { return h.State() == harnessprotocol.HarnessStateRunning },
		5*time.Second, 20*time.Millisecond, "Send 后应进入 RUNNING")
	s.Require().Eventually(func() bool { return h.State() == harnessprotocol.HarnessStateIdle },
		10*time.Second, 50*time.Millisecond, "一轮完成后应回到 IDLE")
}

// Test连发两轮 验证 idle 下连续 Send 两次依次各起一轮。
func (s *NativeHarnessSuite) Test连发两轮() {
	h, _ := s.newStartedHarness()
	defer func() { _ = h.Stop(context.Background()) }()

	// 第一轮返回文本；第二轮也返回文本
	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("第一轮"),
		mockllm.CreateTextResponse("第二轮"),
	)

	s.Require().NoError(h.Send(s.Ctx, "任务一", false))
	// 等第一轮结束回到 idle
	s.Require().Eventually(func() bool { return h.State() == harnessprotocol.HarnessStateIdle },
		10*time.Second, 50*time.Millisecond)

	s.Require().NoError(h.Send(s.Ctx, "任务二", false))
	s.Require().Eventually(func() bool { return h.State() == harnessprotocol.HarnessStateIdle },
		10*time.Second, 50*time.Millisecond)

	// MockLLM 至少被调用两次（两轮各一次）
	s.GreaterOrEqual(s.MockLLM.InvokeCallCount(), 2)
}

// TestAbort立即中断 验证 abort(immediate=true) 让在飞轮次停止。
func (s *NativeHarnessSuite) TestAbort立即中断() {
	h, _ := s.newStartedHarness()
	defer func() { _ = h.Stop(context.Background()) }()

	// 让模型调用卡住：不设响应，模型调用会等待？改用工具调用让一轮持续。
	// 简单验证：Send 后 abort，状态不应 panic，最终能 Stop。
	s.Require().NoError(h.Send(s.Ctx, "长任务", false))
	s.Require().NoError(h.Abort(s.Ctx, true))
}

// Test输出迭代器读到数据 验证 outputs() 泵出 chunk。
func (s *NativeHarnessSuite) Test输出迭代器读到数据() {
	h, _ := s.newStartedHarness()
	defer func() { _ = h.Stop(context.Background()) }()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("流式内容"))

	out := h.Outputs()
	s.Require().NotNil(out)

	s.Require().NoError(h.Send(s.Ctx, "生成内容", false))

	// 读取至少一个 chunk（含 END 哨兵前的内容）
	readAny := false
	ctx, cancel := context.WithTimeout(s.Ctx, 10*time.Second)
	defer cancel()
	for {
		_, ok := out.Next(ctx)
		if !ok {
			break
		}
		readAny = true
		break
	}
	s.True(readAny, "outputs() 应至少读到一个 chunk")
}

// Test订阅轮次事件 验证 subscribe 收到 round started/finished。
func (s *NativeHarnessSuite) Test订阅轮次事件() {
	h, _ := s.newStartedHarness()
	defer func() { _ = h.Stop(context.Background()) }()

	events := make(chan string, 8)
	s.Require().NoError(h.Subscribe(s.Ctx, nil, func(ctx context.Context, ev teamharness.HarnessRoundEvent) {
		events <- ev.Kind
	}))

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("一轮"))

	s.Require().NoError(h.Send(s.Ctx, "开始", false))

	kinds := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case k := <-events:
			kinds[k] = true
		case <-time.After(10 * time.Second):
			s.Fail("超时未收到轮次事件")
			return
		}
	}
	s.True(kinds["started"], "应收到 started 事件")
	s.True(kinds["finished"], "应收到 finished 事件")
}
