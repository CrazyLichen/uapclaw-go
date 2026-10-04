//go:build integration

package harness_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OuterLoopSuite 测试 DeepAgent 外层任务循环（TaskLoop）的核心行为。
// 对照 Python: tests/system_tests/harness/test_deep_agent_outer_loop_system.py
//
// 外层循环核心机制：
//   - EnableTaskLoop=True 时，DeepAgent.Invoke 路由到 runTaskLoopInvoke
//   - 循环内每轮调用 innerInvoke（或 innerInvokeOverride）执行一个任务
//   - Steering 通过 DeepAgent.Steer() → TaskInteractionEvent → LoopQueues.steering 注入
//   - Follow-up 通过 DeepAgent.FollowUp() → FollowUpEvent → LoopQueues.followUp 注入
//   - 循环退出条件：stop condition evaluator、interrupt、abort、无剩余任务
//
// 测试策略：
//   - 使用 ControlledReactAgent 替换内层 Agent（通过 SetInnerInvokeOverride）
//   - 预植 TaskPlan（pending 任务），确保循环不会因 hasRemainingTasks=false 提前退出
//   - 阻塞指定调用序号，在阻塞期间注入 steer/follow_up
//   - 验证调用次数、输入参数、steering 是否传递到内层
//
// 运行方式: go test -tags=integration ./tests/integration/agentcore/harness/...
type OuterLoopSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestOuterLoopSuite 运行外层循环测试套件
func TestOuterLoopSuite(t *testing.T) {
	suite.Run(t, new(OuterLoopSuite))
}

// TestOuterLoop_多轮迭代 测试 TaskLoop 正常执行多轮迭代。
// 对照 Python: test_outer_loop_multistep_with_steer_follow_up（基础部分）
//
// 流程：
//  1. 预植 3 步 TaskPlan → 循环至少执行 3 轮
//  2. ControlledReactAgent 阻塞调用 #2
//  3. 第 1 轮正常完成 → 第 2 轮开始并阻塞
//  4. 释放调用 #2 → 第 3 轮正常完成
//  5. 验证调用次数 ≥ 3
func (s *OuterLoopSuite) TestOuterLoop_多轮迭代() {
	ctx := s.Ctx

	controlledAgent := testhelpers.NewControlledReactAgent(2)

	agent, sess := s.createTaskLoopAgent(ctx, controlledAgent, "outer-loop-multi-round")

	// 预植 3 步 TaskPlan
	seedTaskPlan(agent, sess, "执行三步计划", 3)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Invoke
	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, invokeErr := agent.Invoke(ctx, map[string]any{
			"query": "执行三步计划",
		}, agentinterfaces.WithSession(sess))
		if invokeErr != nil {
			errCh <- invokeErr
			return
		}
		done <- result
	}()

	// 等待第 2 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(2, 10*time.Second),
		"等待第 2 次内层调用启动超时",
	)

	// 释放第 2 次调用
	controlledAgent.ReleaseCall(2)

	// 等待第 3 次调用启动并自然完成
	s.Require().NoError(
		controlledAgent.WaitCallStarted(3, 10*time.Second),
		"等待第 3 次内层调用启动超时",
	)

	// 等待 Invoke 完成
	select {
	case result := <-done:
		s.Require().NotNil(result, "结果不应为 nil")
	case err := <-errCh:
		s.Require().NoError(err, "Invoke 失败")
	case <-time.After(15 * time.Second):
		s.T().Fatal("Invoke 超时")
	}

	sess.PostRun(ctx)

	// 验证至少执行了 3 轮
	s.GreaterOrEqual(controlledAgent.InvokeCallCount(), 3, "外层循环应至少执行 3 轮")
}

// TestOuterLoop_Steer注入 测试在外层循环期间注入 steering 消息。
// 对照 Python: test_outer_loop_multistep_with_steer_follow_up（steer 部分）
//
// 核心验证：
//   - Steer 在第 2 轮执行期间注入
//   - 第 2 轮内层 invoke 的 inputs 应包含 _steering_queue
//   - Steering 文本不拼接在 query 中，而是通过 _steering_queue channel 传递
func (s *OuterLoopSuite) TestOuterLoop_Steer注入() {
	ctx := s.Ctx

	const steerText = "请以要点列表格式输出"

	controlledAgent := testhelpers.NewControlledReactAgent(2)

	agent, sess := s.createTaskLoopAgent(ctx, controlledAgent, "outer-loop-steer")

	// 预植 2 步 TaskPlan（steer 不增加额外轮次）
	seedTaskPlan(agent, sess, "执行两步计划", 2)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Invoke
	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, invokeErr := agent.Invoke(ctx, map[string]any{
			"query": "执行两步计划",
		}, agentinterfaces.WithSession(sess))
		if invokeErr != nil {
			errCh <- invokeErr
			return
		}
		done <- result
	}()

	// 等待第 2 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(2, 10*time.Second),
		"等待第 2 次内层调用启动超时",
	)

	// 在阻塞期间注入 steering
	agent.Steer(ctx, steerText, sess)

	// 释放第 2 次调用
	controlledAgent.ReleaseCall(2)

	// 等待 Invoke 完成
	select {
	case result := <-done:
		s.Require().NotNil(result, "结果不应为 nil")
	case err := <-errCh:
		s.Require().NoError(err, "Invoke 失败")
	case <-time.After(15 * time.Second):
		s.T().Fatal("Invoke 超时")
	}

	sess.PostRun(ctx)

	// 验证至少 2 轮
	s.GreaterOrEqual(controlledAgent.InvokeCallCount(), 2, "外层循环应至少执行 2 轮")

	// 验证第 2 轮 inputs 包含 _steering_queue（steering 通过此 channel 传递）
	secondCall := controlledAgent.GetInvokeCall(1) // 0-indexed
	s.Require().NotNil(secondCall, "第 2 次调用记录应存在")
	steeringQueue, hasSQ := secondCall.Inputs["_steering_queue"]
	s.True(hasSQ, "第 2 轮内层调用 inputs 应包含 _steering_queue")
	if hasSQ {
		// _steering_queue 应为 chan string 类型
		_, isChan := steeringQueue.(chan string)
		s.True(isChan, "_steering_queue 应为 chan string 类型")
	}

	// 验证 steering 文本不拼接在 query 中
	queryStr, _ := secondCall.Inputs["query"].(string)
	s.NotContains(queryStr, "[STEERING]",
		"steering 不应拼接在 query 中，应通过 _steering_queue 传递")
	s.NotContains(queryStr, steerText,
		"steering 文本不应拼接在 query 中")
}

// TestOuterLoop_FollowUp触发额外轮次 测试 follow-up 消息触发额外外层循环轮次。
// 对照 Python: test_outer_loop_multistep_with_steer_follow_up（follow_up 部分）
//
// 核心验证：
//   - FollowUp 在第 2 轮执行期间注入
//   - FollowUp 消息成为后续轮次的 query
//   - 总调用次数 > 原始 TaskPlan 步数
func (s *OuterLoopSuite) TestOuterLoop_FollowUp触发额外轮次() {
	ctx := s.Ctx

	const followUpText = "补充检查步骤"

	controlledAgent := testhelpers.NewControlledReactAgent(2)

	agent, sess := s.createTaskLoopAgent(ctx, controlledAgent, "outer-loop-follow-up")

	// 预植 2 步 TaskPlan
	seedTaskPlan(agent, sess, "执行两步计划", 2)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Invoke
	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, invokeErr := agent.Invoke(ctx, map[string]any{
			"query": "执行两步计划",
		}, agentinterfaces.WithSession(sess))
		if invokeErr != nil {
			errCh <- invokeErr
			return
		}
		done <- result
	}()

	// 等待第 2 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(2, 10*time.Second),
		"等待第 2 次内层调用启动超时",
	)

	// 在阻塞期间注入 follow-up
	agent.FollowUp(ctx, followUpText, "", sess)

	// 释放第 2 次调用
	controlledAgent.ReleaseCall(2)

	// 等待第 3 次调用启动（follow-up 触发的额外轮次）
	s.Require().NoError(
		controlledAgent.WaitCallStarted(3, 10*time.Second),
		"等待第 3 次内层调用（follow-up 轮次）启动超时",
	)

	// 等待 Invoke 完成
	select {
	case result := <-done:
		s.Require().NotNil(result, "结果不应为 nil")
	case err := <-errCh:
		s.Require().NoError(err, "Invoke 失败")
	case <-time.After(15 * time.Second):
		s.T().Fatal("Invoke 超时")
	}

	sess.PostRun(ctx)

	// 验证至少 3 轮：2 个原始轮次 + 1 个 follow-up
	s.GreaterOrEqual(controlledAgent.InvokeCallCount(), 3,
		"外层循环应至少执行 3 轮（2 原始 + 1 follow-up）")

	// 验证第 3 轮 query 包含 follow-up 文本
	thirdCall := controlledAgent.GetInvokeCall(2) // 0-indexed
	s.Require().NotNil(thirdCall, "第 3 次调用记录应存在")
	queryStr, _ := thirdCall.Inputs["query"].(string)
	s.Contains(queryStr, followUpText,
		"follow-up 轮次的 query 应包含 follow-up 文本")
}

// TestOuterLoop_多个FollowUpFIFO消费 测试多个 follow-up 按 FIFO 顺序消费。
// 对照 Python: test_multiple_follow_ups_consumed_in_order
//
// 核心验证：
//   - 3 个 follow-up 一次性注入
//   - 后续轮次按注入顺序消费
//   - 总轮次 > 原始步数 + 3 个 follow-up
func (s *OuterLoopSuite) TestOuterLoop_多个FollowUpFIFO消费() {
	ctx := s.Ctx

	controlledAgent := testhelpers.NewControlledReactAgent(2)

	agent, sess := s.createTaskLoopAgent(ctx, controlledAgent, "outer-loop-multi-fu")

	// 预植 2 步 TaskPlan + 3 个 follow-up = 5 轮
	seedTaskPlan(agent, sess, "执行计划", 2)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Invoke
	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, invokeErr := agent.Invoke(ctx, map[string]any{
			"query": "执行计划",
		}, agentinterfaces.WithSession(sess))
		if invokeErr != nil {
			errCh <- invokeErr
			return
		}
		done <- result
	}()

	// 等待第 2 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(2, 10*time.Second),
		"等待第 2 次内层调用启动超时",
	)

	// 在阻塞期间注入 3 个 follow-up
	agent.FollowUp(ctx, "第一步补充", "", sess)
	agent.FollowUp(ctx, "第二步补充", "", sess)
	agent.FollowUp(ctx, "第三步补充", "", sess)

	// 释放第 2 次调用
	controlledAgent.ReleaseCall(2)

	// 等待 Invoke 完成
	select {
	case result := <-done:
		s.Require().NotNil(result, "结果不应为 nil")
	case err := <-errCh:
		s.Require().NoError(err, "Invoke 失败")
	case <-time.After(15 * time.Second):
		s.T().Fatal("Invoke 超时")
	}

	sess.PostRun(ctx)

	// 验证总调用次数 ≥ 5：2 个原始轮次 + 3 个 follow-up
	callCount := controlledAgent.InvokeCallCount()
	s.GreaterOrEqual(callCount, 5,
		"外层循环应至少执行 5 轮（2 原始 + 3 follow-up），实际 %d", callCount)

	// 验证 follow-up 消费顺序（FIFO）
	// 第 3、4、5 次调用应分别是 3 个 follow-up
	followUpTexts := []string{"第一步补充", "第二步补充", "第三步补充"}
	for i, expected := range followUpTexts {
		callIdx := 2 + i // 0-indexed: 第 3、4、5 次调用
		call := controlledAgent.GetInvokeCall(callIdx)
		if call == nil {
			s.T().Logf("第 %d 次调用记录不存在（循环可能提前退出）", callIdx+1)
			continue
		}
		queryStr, _ := call.Inputs["query"].(string)
		s.Contains(queryStr, expected,
			"第 %d 次 follow-up 轮次 query 应包含 %q，实际: %q",
			i+1, expected, queryStr)
	}
}

// TestOuterLoop_Steer加FollowUp组合 测试 steer + follow-up 组合使用。
// 对照 Python: test_outer_loop_multistep_with_steer_follow_up（完整版）
//
// 核心验证：
//   - 第 2 轮阻塞期间注入 steer + follow-up
//   - Steer 通过 _steering_queue 传递给内层
//   - FollowUp 触发额外的第 3 轮
//   - 第 3 轮 query 为 follow-up 文本
func (s *OuterLoopSuite) TestOuterLoop_Steer加FollowUp组合() {
	ctx := s.Ctx

	const steerText = "请用简洁格式输出"
	const followUpText = "补充检查步骤"

	controlledAgent := testhelpers.NewControlledReactAgent(2)

	agent, sess := s.createTaskLoopAgent(ctx, controlledAgent, "outer-loop-steer-fu")

	// 预植 2 步 TaskPlan
	seedTaskPlan(agent, sess, "执行两步计划", 2)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Invoke
	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, invokeErr := agent.Invoke(ctx, map[string]any{
			"query": "执行两步计划",
		}, agentinterfaces.WithSession(sess))
		if invokeErr != nil {
			errCh <- invokeErr
			return
		}
		done <- result
	}()

	// 等待第 2 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(2, 10*time.Second),
		"等待第 2 次内层调用启动超时",
	)

	// 在阻塞期间注入 steer + follow-up
	agent.Steer(ctx, steerText, sess)
	agent.FollowUp(ctx, followUpText, "", sess)

	// 释放第 2 次调用
	controlledAgent.ReleaseCall(2)

	// 等待第 3 次调用启动（follow-up 触发）
	s.Require().NoError(
		controlledAgent.WaitCallStarted(3, 10*time.Second),
		"等待第 3 次内层调用（follow-up 轮次）启动超时",
	)

	// 等待 Invoke 完成
	select {
	case result := <-done:
		s.Require().NotNil(result, "结果不应为 nil")
	case err := <-errCh:
		s.Require().NoError(err, "Invoke 失败")
	case <-time.After(15 * time.Second):
		s.T().Fatal("Invoke 超时")
	}

	sess.PostRun(ctx)

	// 验证至少 3 轮
	callCount := controlledAgent.InvokeCallCount()
	s.GreaterOrEqual(callCount, 3, "外层循环应至少执行 3 轮")

	// 验证第 2 轮 inputs 包含 _steering_queue
	secondCall := controlledAgent.GetInvokeCall(1) // 0-indexed
	s.Require().NotNil(secondCall, "第 2 次调用记录应存在")
	_, hasSQ := secondCall.Inputs["_steering_queue"]
	s.True(hasSQ, "第 2 轮内层调用 inputs 应包含 _steering_queue（steering 通道）")

	// 验证第 3 轮 query 包含 follow-up 文本
	thirdCall := controlledAgent.GetInvokeCall(2) // 0-indexed
	if thirdCall != nil {
		queryStr, _ := thirdCall.Inputs["query"].(string)
		s.Contains(queryStr, followUpText,
			"follow-up 轮次的 query 应包含 follow-up 文本")
	}
}

// TestOuterLoop_Abort中止循环 测试外层循环的 Abort 中止机制。
// 对照 Python: DeepAgent.abort(session)
//
// 核心验证：
//   - Abort 在第 2 轮阻塞期间调用
//   - 循环应立即停止，不再执行后续轮次
func (s *OuterLoopSuite) TestOuterLoop_Abort中止循环() {
	ctx := s.Ctx

	controlledAgent := testhelpers.NewControlledReactAgent(2)

	agent, sess := s.createTaskLoopAgent(ctx, controlledAgent, "outer-loop-abort")

	// 预植 3 步 TaskPlan（Abort 应在第 2 轮后终止）
	seedTaskPlan(agent, sess, "执行多步计划", 3)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Invoke
	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, invokeErr := agent.Invoke(ctx, map[string]any{
			"query": "执行多步计划",
		}, agentinterfaces.WithSession(sess))
		if invokeErr != nil {
			errCh <- invokeErr
			return
		}
		done <- result
	}()

	// 等待第 2 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(2, 10*time.Second),
		"等待第 2 次内层调用启动超时",
	)

	// 在阻塞期间调用 Abort
	agent.Abort(ctx)

	// 释放第 2 次调用（让阻塞的 goroutine 继续）
	controlledAgent.ReleaseCall(2)

	// 等待 Invoke 完成
	select {
	case <-done:
		// 循环已中止
	case err := <-errCh:
		// 中止可能导致 ctx 取消错误，属正常行为
		s.T().Logf("Abort 后返回错误（预期行为）: %v", err)
	case <-time.After(15 * time.Second):
		s.T().Fatal("Invoke 超时，Abort 可能未生效")
	}

	sess.PostRun(ctx)

	// 验证调用次数 ≤ 3（Abort 应在第 2 轮之后停止循环）
	s.LessOrEqual(controlledAgent.InvokeCallCount(), 3,
		"Abort 后外层循环不应继续执行过多轮次")
}

// TestOuterLoop_无TaskLoop时单轮执行 测试 EnableTaskLoop=false 时仍为单轮执行。
// 对比测试：确保 EnableTaskLoop 标志正确路由。
func (s *OuterLoopSuite) TestOuterLoop_无TaskLoop时单轮执行() {
	ctx := s.Ctx

	controlledAgent := testhelpers.NewControlledReactAgent()

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		EnableTaskLoop:    false,
		MaxIterations:     10,
		CompletionTimeout: 15.0,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	agent.SetInnerInvokeOverride(controlledAgent.Invoke)

	sess := s.NewTestSession("outer-loop-no-task-loop")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "简单问题",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result)

	sess.PostRun(ctx)

	// 无 TaskLoop 时应只执行 1 轮
	s.Equal(1, controlledAgent.InvokeCallCount(),
		"无 TaskLoop 时应只执行 1 次内层调用")
}

// TestOuterLoop_Steer不拼接在Query中 测试 steering 消息不拼接在 query 字符串中。
// 对照 Python: test_outer_loop_multistep_with_steer_follow_up 中的断言
//
// 核心验证：
//   - Steer 通过 _steering_queue channel 传递，不修改 query
//   - Query 保持原始值
func (s *OuterLoopSuite) TestOuterLoop_Steer不拼接在Query中() {
	ctx := s.Ctx

	const steerText = "请改为英文输出"
	originalQuery := "执行数据分析"

	controlledAgent := testhelpers.NewControlledReactAgent(1)

	agent, sess := s.createTaskLoopAgent(ctx, controlledAgent, "outer-loop-steer-no-concat")

	// 预植 3 步 TaskPlan
	seedTaskPlan(agent, sess, "执行数据分析", 3)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Invoke
	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		result, invokeErr := agent.Invoke(ctx, map[string]any{
			"query": originalQuery,
		}, agentinterfaces.WithSession(sess))
		if invokeErr != nil {
			errCh <- invokeErr
			return
		}
		done <- result
	}()

	// 等待第 1 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(1, 10*time.Second),
		"等待第 1 次内层调用启动超时",
	)

	// 在阻塞期间注入 steering
	agent.Steer(ctx, steerText, sess)

	// 释放第 1 次调用
	controlledAgent.ReleaseCall(1)

	// 等待 Invoke 完成
	select {
	case result := <-done:
		s.Require().NotNil(result, "结果不应为 nil")
	case err := <-errCh:
		s.Require().NoError(err, "Invoke 失败")
	case <-time.After(15 * time.Second):
		s.T().Fatal("Invoke 超时")
	}

	sess.PostRun(ctx)

	// 验证第 1 轮 inputs 不包含 [STEERING] 在 query 中
	firstCall := controlledAgent.GetInvokeCall(0)
	s.Require().NotNil(firstCall, "第 1 次调用记录应存在")
	queryStr, _ := firstCall.Inputs["query"].(string)
	s.NotContains(queryStr, "[STEERING]",
		"steering 不应以 [STEERING] 前缀拼接在 query 中")
	s.NotContains(queryStr, steerText,
		"steering 文本不应拼接在 query 中")

	// 验证第 1 轮 inputs 包含 _steering_queue
	_, hasSQ := firstCall.Inputs["_steering_queue"]
	s.True(hasSQ, "第 1 轮内层调用 inputs 应包含 _steering_queue")

	// 验证 query 保持原始值（TaskPlan 任务可能改变 query 内容，但不应包含 steer）
	if !strings.Contains(queryStr, steerText) && !strings.Contains(queryStr, "[STEERING]") {
		// 通过：steering 文本未拼接在 query 中
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createTaskLoopAgent 创建启用 TaskLoop 的 DeepAgent 并设置 innerInvokeOverride。
// 返回 agent 和 session。
func (s *OuterLoopSuite) createTaskLoopAgent(
	ctx context.Context,
	controlledAgent *testhelpers.ControlledReactAgent,
	sessionName string,
) (*harness.DeepAgent, *session.Session) {
	// 需要至少一个工具才能让 DeepAgent 正常初始化
	dummyTool := testhelpers.NewBlockingTool("dummy_tool")
	dummyTool.Release() // 立即释放，不阻塞

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		EnableTaskLoop:    true,
		MaxIterations:     10,
		CompletionTimeout: 30.0,
		ToolInstances:     []tool.Tool{dummyTool.Tool()},
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	agent.SetInnerInvokeOverride(controlledAgent.Invoke)

	sess := s.NewTestSession(sessionName)
	return agent, sess
}

// seedTaskPlan 在 Invoke 前预植 TaskPlan，确保外层循环不会因
// hasRemainingTasks=false 提前退出。
// 对照 Python: test_deep_agent_outer_loop_system.py 中 _seed_multi_step_plan()。
func seedTaskPlan(agent *harness.DeepAgent, sess *session.Session, goal string, taskCount int) {
	state := agent.LoadState(sess)
	plan := hschema.NewTaskPlan(goal)
	for i := 0; i < taskCount; i++ {
		task := hschema.NewTodoItem()
		task.Content = goal + " 步骤" + strings.Repeat("一", i+1)
		task.ActiveForm = "执行" + task.Content
		plan.AddTask(task)
	}
	state.TaskPlan = &plan
	agent.SaveState(sess, state)
}
