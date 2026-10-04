//go:build integration

package harness_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SteerInnerLoopSuite 测试 steering 消息在 ReAct 内循环中的注入。
// 对照 Python: tests/system_tests/harness/test_steer_inner_loop.py
//
// 核心验证：在工具执行期间注入 steering，[STEERING] 消息应出现在
// 同一 invoke 的下一次模型调用中，而非延迟到下一轮外循环。
//
// 实现方式：
//   - 在 invoke inputs 中注入 "_steering_queue"（模拟 TaskLoop executor 的行为）
//   - ReActAgent.innerInvoke 会从 inputs["_steering_queue"] 绑定 cbc.BindSteeringQueue
//   - SteerInjectRail 在 AfterToolCall 中通过 channel 推入 steering
//   - ReActAgent 下次迭代开头 DrainSteering → [STEERING] UserMessage 注入模型上下文
//
// 运行方式: go test -tags=integration ./tests/integration/agentcore/harness/...
type SteerInnerLoopSuite struct {
	isuite.AgentSuite
}

// SteerInjectRail 在工具执行后注入 steering 消息的辅助 Rail。
// 对照 Python: 在 blocking_tool 执行期间调用 agent.steer()。
// 由于 Go 端 DeepAgent.Steer 需要 TaskLoop（controller event queue），
// 本 Rail 直接通过 steering channel 推入消息模拟注入效果。
type SteerInjectRail struct {
	agentinterfaces.BaseRail
	// steeringCh 共享的 steering channel
	steeringCh chan string
	// steerText 要注入的 steering 文本
	steerText string
	// injected 是否已注入
	injected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSteerInnerLoopSuite 运行 steering 内循环测试套件
func TestSteerInnerLoopSuite(t *testing.T) {
	suite.Run(t, new(SteerInnerLoopSuite))
}

// TestSteer_工具执行后注入 测试工具执行后注入 steering，验证 [STEERING] 出现在下一次模型调用。
// 对照 Python: test_steer_visible_in_same_invoke
//
// 流程：
//  1. MockLLM 第 1 轮返回 blocking_tool 调用 → 工具执行
//  2. SteerInjectRail.AfterToolCall 通过 steering channel 注入 steering
//  3. ReActAgent 下次迭代开头 DrainSteering → [STEERING] 消息注入模型上下文
//  4. MockLLM 第 2 轮模型调用时应看到 [STEERING] 消息
//  5. MockLLM 第 2 轮返回文本响应 → 完成循环
func (s *SteerInnerLoopSuite) TestSteer_工具执行后注入() {
	ctx := s.Ctx

	const steerText = "请用中文输出简洁要点"
	steeringCh := make(chan string, 8)

	blockingTool := testhelpers.NewBlockingTool("blocking_tool")
	observer := testhelpers.NewModelCallObserver()
	steerRail := &SteerInjectRail{steeringCh: steeringCh, steerText: steerText}

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("blocking_tool", `{}`),
		mockllm.CreateTextResponse("第一步已完成。"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{blockingTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{observer, steerRail},
		MaxIterations: 6,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("steer-inner-loop")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Invoke（注入 _steering_queue 模拟 TaskLoop executor 行为）
	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := agent.Invoke(ctx, map[string]any{
			"query":           "执行两步计划",
			"_steering_queue": steeringCh, // 模拟 TaskLoop executor 注入
		}, agentinterfaces.WithSession(sess))
		if err != nil {
			errCh <- err
			return
		}
		done <- result
	}()

	// 等待 blocking_tool 进入
	s.Require().NoError(blockingTool.WaitEntered(ctx), "等待 blocking_tool 进入超时")

	// 释放 blocking_tool（让工具执行完成，触发 AfterToolCall → steering channel 推入）
	blockingTool.Release()

	// 等待 Invoke 完成
	select {
	case result := <-done:
		s.Require().NotNil(result, "结果不应为 nil")
		testhelpers.AssertAnswerResult(s.T(), result)
	case err := <-errCh:
		s.Require().NoError(err, "Invoke 失败")
	case <-time.After(10 * time.Second):
		s.T().Fatal("Invoke 超时")
	}

	sess.PostRun(ctx)

	// 验证模型调用次数 ≥ 2
	s.GreaterOrEqual(observer.CallCount(), 2, "至少应有 2 次模型调用")

	// 验证第 1 次模型调用不包含 [STEERING]
	firstMsgs := observer.GetMessages(0)
	for _, msg := range firstMsgs {
		content := msg.GetContent()
		if content.IsText() {
			s.NotContains(content.Text(), "[STEERING]",
				"第 1 次模型调用不应包含 [STEERING]（steering 尚未注入）")
		}
	}

	// 验证第 2 次模型调用包含 [STEERING]
	secondMsgs := observer.GetMessages(1)
	steerFound := false
	for _, msg := range secondMsgs {
		content := msg.GetContent()
		if content.IsText() {
			text := content.Text()
			if strings.Contains(text, "[STEERING]") && strings.Contains(text, steerText) {
				steerFound = true
				break
			}
		}
	}
	s.True(steerFound, "第 2 次模型调用应包含 [STEERING] %s", steerText)
}

// TestSteer_无Steering时正常完成 测试无 steering 注入时模型调用正常完成。
// 对比测试：确保 ModelCallObserver 不干扰正常流程。
func (s *SteerInnerLoopSuite) TestSteer_无Steering时正常完成() {
	ctx := s.Ctx

	observer := testhelpers.NewModelCallObserver()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("正常完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{observer},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("steer-no-steering")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "简单问题"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result)

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(1, observer.CallCount(), "无工具调用时应只有 1 次模型调用")
	s.False(observer.SteerSeenInMessages(), "无 steering 注入时不应出现 [STEERING]")

	sess.PostRun(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// GetCallbacks 注册 AfterToolCall 回调。
func (r *SteerInjectRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackAfterToolCall, func(ctx context.Context, railCtx any) error {
			return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}),
	)
}

// AfterToolCall 工具执行后通过 steering channel 注入 steering 消息。
func (r *SteerInjectRail) AfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	if r.injected {
		return nil // 只注入一次
	}
	r.injected = true
	select {
	case r.steeringCh <- r.steerText:
	default:
		// channel 满则丢弃（对齐 LoopQueues.PushSteer 行为）
	}
	return nil
}
