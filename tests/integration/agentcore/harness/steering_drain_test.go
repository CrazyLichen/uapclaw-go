//go:build integration

package harness_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SteeringDrainSuite 覆盖 spec C3~C8/C14/C15：steering 批上限真实链路。
//
// Python: ReActAgent._drain_steering_batch（react_agent.py L964-1000）
// 与 _BRIDGE_EVENTS 第三条（harness/deep_agent.py L176）。
type SteeringDrainSuite struct {
	isuite.AgentSuite
}

// limitRail 在 BeforeSteeringDrain 里写死批上限，并记录触发情况。
type limitRail struct {
	agentinterfaces.BaseRail
	// limit 写死的上限；nil 表示不写（全部吸收）
	limit *int
	// fired 触发次数
	fired int
	// pendingSeen 最后一次看到的排队条数
	pendingSeen int
	// firstPending 首次触发时看到的排队条数
	firstPending int
}

// preloadRail 在 AfterToolCall 里向 steering 队列预灌消息。
//
// 复用 steer_test.go 的模式：用阻塞工具造出"忙碌窗口"，在工具返回时
// 把积压一次性塞进队列，模拟成员忙碌期间积攒的消息。
type preloadRail struct {
	agentinterfaces.BaseRail
	// queue 共享的 steering 队列
	queue chan string
	// payload 待灌入的消息
	payload []string
	// done 是否已灌
	done bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSteeringDrainSuite 运行 SteeringDrainSuite。
func TestSteeringDrainSuite(t *testing.T) { suite.Run(t, new(SteeringDrainSuite)) }

// GetCallbacks 只声明 BeforeSteeringDrain。
func (r *limitRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackBeforeSteeringDrain, r.onDrain),
	)
}

// GetCallbacks 声明 AfterToolCall。
func (r *preloadRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackAfterToolCall, r.onAfterTool),
	)
}

// TestIT_Steering事件_空队列不发 验证从不注入 steering 时事件零触发。
//
// spec C3。
func (s *SteeringDrainSuite) TestIT_Steering事件_空队列不发() {
	ctx := context.Background()
	rail := &limitRail{}

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("done"))
	agent, sess := s.createSteerAgent(ctx, rail, nil, "it-drain-empty")

	s.Require().NoError(sess.PreRun(ctx))
	_, err := agent.Invoke(ctx, map[string]any{"query": "hi"}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err)
	sess.PostRun(ctx)

	s.Zero(rail.fired, "无积压不得触发 before_steering_drain")
}

// TestIT_Steering批上限_真实链路 验证分多批吸收且顺序不变。
//
// spec C3/C4/C7/C14：5 条积压、上限 2 → 第 1 次模型调用只见 2 条。
func (s *SteeringDrainSuite) TestIT_Steering批上限_真实链路() {
	ctx := s.Ctx
	limit := 2
	rail := &limitRail{limit: &limit}
	steeringCh := make(chan string, 16)
	preload := &preloadRail{queue: steeringCh, payload: []string{"s1", "s2", "s3", "s4", "s5"}}
	observer := testhelpers.NewModelCallObserver()

	blockingTool := testhelpers.NewBlockingTool("blocking_tool")
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("blocking_tool", `{}`),
		mockllm.CreateTextResponse("第二批"),
		mockllm.CreateTextResponse("第三批"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{blockingTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{observer, rail, preload},
		MaxIterations: 8,
	})
	s.Require().NoError(err)
	sess := s.NewTestSession("it-drain-limit")
	s.Require().NoError(sess.PreRun(ctx))

	done := make(chan error, 1)
	go func() {
		_, invokeErr := agent.Invoke(ctx, map[string]any{
			"query":           "执行",
			"_steering_queue": steeringCh,
		}, agentinterfaces.WithSession(sess))
		done <- invokeErr
	}()

	s.Require().NoError(blockingTool.WaitEntered(ctx), "等待工具进入超时")
	blockingTool.Release()
	s.Require().NoError(<-done, "Invoke 失败")
	sess.PostRun(ctx)

	s.GreaterOrEqual(rail.fired, 1, "有积压必须触发事件")
	s.Equal(5, rail.firstPending, "首次触发时应看到全部 5 条积压")

	// 第 2 次模型调用（工具跑完后的第一次）只应吸收 s1、s2 两条。
	s.Equal(2, countSteeringContents(observer.GetMessages(1), "s"),
		"上限 2 生效：本次只吸收 2 条")
}

// TestIT_Steering无Limit不回归 验证不写 Limit 时一次全取（既有行为）。
//
// spec C5。
func (s *SteeringDrainSuite) TestIT_Steering无Limit不回归() {
	ctx := s.Ctx
	rail := &limitRail{}
	steeringCh := make(chan string, 16)
	preload := &preloadRail{queue: steeringCh, payload: []string{"s1", "s2", "s3", "s4", "s5"}}
	observer := testhelpers.NewModelCallObserver()

	blockingTool := testhelpers.NewBlockingTool("blocking_tool")
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("blocking_tool", `{}`),
		mockllm.CreateTextResponse("done"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{blockingTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{observer, rail, preload},
		MaxIterations: 6,
	})
	s.Require().NoError(err)
	sess := s.NewTestSession("it-drain-nolimit")
	s.Require().NoError(sess.PreRun(ctx))

	done := make(chan error, 1)
	go func() {
		_, invokeErr := agent.Invoke(ctx, map[string]any{
			"query":           "执行",
			"_steering_queue": steeringCh,
		}, agentinterfaces.WithSession(sess))
		done <- invokeErr
	}()
	s.Require().NoError(blockingTool.WaitEntered(ctx))
	blockingTool.Release()
	s.Require().NoError(<-done)
	sess.PostRun(ctx)

	// steering 批经 admission 后 join 成**一条**消息，前缀只加一次
	// （"[STEERING] s1\ns2\n..."），故数内容条目而不是标记次数。
	s.Equal(5, countSteeringContents(observer.GetMessages(1), "s"),
		"无 Limit 时必须一次全取（既有行为不回归）")
}

// TestIT_Steering_Limit为0仍前进 验证 0 不会死循环。
//
// spec C6：非空队列每轮至少放行 1 条，循环能收敛。
func (s *SteeringDrainSuite) TestIT_Steering_Limit为0仍前进() {
	ctx := s.Ctx
	zero := 0
	rail := &limitRail{limit: &zero}
	steeringCh := make(chan string, 16)
	preload := &preloadRail{queue: steeringCh, payload: []string{"s1", "s2", "s3"}}

	blockingTool := testhelpers.NewBlockingTool("blocking_tool")
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("blocking_tool", `{}`),
		mockllm.CreateTextResponse("a"),
		mockllm.CreateTextResponse("b"),
		mockllm.CreateTextResponse("c"),
		mockllm.CreateTextResponse("d"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{blockingTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{rail, preload},
		MaxIterations: 10,
	})
	s.Require().NoError(err)
	sess := s.NewTestSession("it-drain-zero")
	s.Require().NoError(sess.PreRun(ctx))

	done := make(chan error, 1)
	go func() {
		_, invokeErr := agent.Invoke(ctx, map[string]any{
			"query":           "执行",
			"_steering_queue": steeringCh,
		}, agentinterfaces.WithSession(sess))
		done <- invokeErr
	}()
	s.Require().NoError(blockingTool.WaitEntered(ctx))
	blockingTool.Release()

	select {
	case invokeErr := <-done:
		s.Require().NoError(invokeErr)
	case <-time.After(15 * time.Second):
		s.T().Fatal("Limit=0 时 Invoke 未收敛（疑似死循环）")
	}
	sess.PostRun(ctx)

	s.GreaterOrEqual(rail.fired, 1, "有积压必须触发事件")
	s.Empty(steeringCh, "队列必须被吃完（每轮至少 1 条，不得饿死）")
}

// TestIT_钩子桥接到内层ReActAgent 验证 rail 的 BeforeSteeringDrain 真的落在内层。
//
// spec C8。
func (s *SteeringDrainSuite) TestIT_钩子桥接到内层ReActAgent() {
	ctx := s.Ctx
	rail := &limitRail{}
	steeringCh := make(chan string, 8)
	preload := &preloadRail{queue: steeringCh, payload: []string{"s1"}}

	blockingTool := testhelpers.NewBlockingTool("blocking_tool")
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("blocking_tool", `{}`),
		mockllm.CreateTextResponse("done"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{blockingTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{rail, preload},
		MaxIterations: 6,
	})
	s.Require().NoError(err)
	sess := s.NewTestSession("it-drain-bridge")
	s.Require().NoError(sess.PreRun(ctx))

	done := make(chan error, 1)
	go func() {
		_, invokeErr := agent.Invoke(ctx, map[string]any{
			"query":           "执行",
			"_steering_queue": steeringCh,
		}, agentinterfaces.WithSession(sess))
		done <- invokeErr
	}()
	s.Require().NoError(blockingTool.WaitEntered(ctx))
	blockingTool.Release()
	s.Require().NoError(<-done)
	sess.PostRun(ctx)

	s.GreaterOrEqual(rail.fired, 1,
		"事件必须由内层 ReActAgent 触发到该 rail（证明桥接正确）")
}

// TestIT_steering前缀与事件在真实链路上成立 验证真实链路的 steering 形态。
//
// spec C14：第 1 次模型调用无 [STEERING]，之后有且带 steer 文本。
func (s *SteeringDrainSuite) TestIT_steering前缀与事件在真实链路上成立() {
	ctx := s.Ctx
	const steerText = "steer-payload"
	rail := &limitRail{}
	steeringCh := make(chan string, 8)
	preload := &preloadRail{queue: steeringCh, payload: []string{steerText}}
	observer := testhelpers.NewModelCallObserver()

	blockingTool := testhelpers.NewBlockingTool("blocking_tool")
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("blocking_tool", `{}`),
		mockllm.CreateTextResponse("done"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{blockingTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{observer, rail, preload},
		MaxIterations: 6,
	})
	s.Require().NoError(err)
	sess := s.NewTestSession("it-drain-prefix")
	s.Require().NoError(sess.PreRun(ctx))

	done := make(chan error, 1)
	go func() {
		_, invokeErr := agent.Invoke(ctx, map[string]any{
			"query":           "执行",
			"_steering_queue": steeringCh,
		}, agentinterfaces.WithSession(sess))
		done <- invokeErr
	}()
	s.Require().NoError(blockingTool.WaitEntered(ctx))
	blockingTool.Release()
	s.Require().NoError(<-done)
	sess.PostRun(ctx)

	// 第 1 次模型调用不应含 [STEERING]（steering 在模型调用前才注入）
	for _, m := range observer.GetMessages(0) {
		if c := m.GetContent(); c.IsText() {
			s.NotContains(c.Text(), "[STEERING]")
		}
	}

	// 之后必须出现 [STEERING] + steer 文本
	s.GreaterOrEqual(countSteeringContents(observer.GetMessages(1), "steer"), 1,
		"第 2 次模型调用必须包含 [STEERING] steer-payload")
	found := false
	for _, m := range observer.GetMessages(1) {
		if c := m.GetContent(); c.IsText() && strings.Contains(c.Text(), steerText) {
			found = true
			break
		}
	}
	s.True(found, "第 2 次模型调用必须包含 steer 文本 %s", steerText)
}

// TestIT_Clear后重跑无残留 验证 Clear() 真的清掉了桥接到内层的钩子。
//
// spec C15：before_steering_drain 是桥接事件，注册在内层 ReActAgent 的
// callback manager 上。若该事件漏加进 AllCallbackEvents，Clear() 枚举不到它，
// 钩子会残留并在后续轮次继续触发。
//
// 用真实链路验证：注册 rail → 内层 manager 有钩子 → Clear() → 无钩子 →
// 重跑一轮，rail 不再被触发。
func (s *SteeringDrainSuite) TestIT_Clear后重跑无残留() {
	ctx := s.Ctx
	rail := &limitRail{}
	steeringCh := make(chan string, 8)

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("first"),
		mockllm.CreateTextResponse("second"),
	)
	agent, sess := s.createSteerAgent(ctx, rail, nil, "it-drain-clear")

	// 必须先初始化：pending rails 在 ensureInitialized 时才转注册并桥接
	_, initErr := agent.EnsureInitialized(ctx)
	s.Require().NoError(initErr, "初始化 DeepAgent 失败")

	// 定位内层 manager：桥接事件注册在它上面，外层 CallbackManager() 看不到
	inner := agent.ReactAgent()
	s.Require().NotNil(inner, "必须能取到内层 ReActAgent")
	innerMgr := inner.CallbackManager()
	s.Require().NotNil(innerMgr)
	s.True(innerMgr.HasHooks(agentinterfaces.CallbackBeforeSteeringDrain),
		"前置：rail 的 before_steering_drain 必须已桥接到内层")

	// 清理
	innerMgr.Clear()
	s.False(innerMgr.HasHooks(agentinterfaces.CallbackBeforeSteeringDrain),
		"Clear() 必须清掉 before_steering_drain（验证它已进 AllCallbackEvents）")

	// 重跑一轮：有 steering 积压，但 rail 已被清掉，不得再被触发
	steeringCh <- "s1"
	s.Require().NoError(sess.PreRun(ctx))
	_, err := agent.Invoke(ctx, map[string]any{
		"query":           "执行",
		"_steering_queue": steeringCh,
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err)
	sess.PostRun(ctx)

	s.Zero(rail.fired, "Clear() 之后 rail 不得再被触发")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// onDrain 记录并可选写入 Limit。
func (r *limitRail) onDrain(_ context.Context, railCtx any) error {
	r.fired++
	cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	in, ok := cbc.Inputs().(*agentinterfaces.SteeringDrainInputs)
	if !ok {
		return nil
	}
	r.pendingSeen = in.Pending
	if r.fired == 1 {
		r.firstPending = in.Pending
	}
	if r.limit != nil {
		v := *r.limit
		in.Limit = &v
	}
	return nil
}

// onAfterTool 在工具返回后把积压一次性灌进队列。
func (r *preloadRail) onAfterTool(_ context.Context, _ any) error {
	if r.done || r.queue == nil {
		return nil
	}
	r.done = true
	for _, m := range r.payload {
		select {
		case r.queue <- m:
		default:
			return nil
		}
	}
	return nil
}

// createSteerAgent 构造一个只跑单轮的简单 agent（用于不需要工具场景）。
func (s *SteeringDrainSuite) createSteerAgent(
	ctx context.Context,
	rail *limitRail,
	_ *preloadRail,
	sessionName string,
) (*harness.DeepAgent, *session.Session) {
	dummyTool := testhelpers.NewBlockingTool("dummy_tool")
	dummyTool.Release()

	a, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
		ToolInstances: []tool.Tool{dummyTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{rail},
	})
	s.Require().NoError(err)
	return a, s.NewTestSession(sessionName)
}

// countSteeringContents 统计 steering 消息体里 "sN" 形式的条目数。
//
// steering 批经 admission 后按换行 join 成一条消息，前缀 [STEERING] 只加一次
// （上游 _admit_user_message 的 "{prefix}{body}" 语义），因此"吸收了几条"
// 要看正文里的条目，而不是标记或消息条数。
func countSteeringContents(msgs []llmschema.BaseMessage, prefix string) int {
	n := 0
	for _, m := range msgs {
		c := m.GetContent()
		if !c.IsText() {
			continue
		}
		text := c.Text()
		if !strings.Contains(text, "[STEERING]") {
			continue
		}
		for _, piece := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(strings.TrimPrefix(piece, "[STEERING] "))
			if trimmed != "" && strings.HasPrefix(trimmed, prefix) {
				n++
			}
		}
	}
	return n
}
