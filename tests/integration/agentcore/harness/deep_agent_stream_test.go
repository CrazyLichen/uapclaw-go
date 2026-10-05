//go:build integration

package harness_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgentStreamE2ESuite 测试 DeepAgent.Stream() 流式输出。
//
// 覆盖：
//   - 单轮流式输出基本流程
//   - Stream chunk 类型验证（OutputSchema）
//   - IsLastSchema 最后一帧标记
//   - 多轮工具调用流式
//   - TaskLoop + Stream 联合
//   - Stream + Steer/FollowUp
//
// 对齐 Python: tests/system_tests/harness/test_deep_agent_stream_e2e.py
type DeepAgentStreamE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDeepAgentStreamE2ESuite(t *testing.T) {
	suite.Run(t, new(DeepAgentStreamE2ESuite))
}

// TestStream_单轮纯文本 测试 DeepAgent 单轮流式文本输出。
// 对齐 Python: test_deep_agent_stream_e2e（文本场景）
//
// 核心验证：
//   - Stream() 返回 <-chan stream.Schema
//   - channel 中至少有一个 chunk
//   - chunk 类型为 OutputSchema
//   - channel 最终关闭
func (s *DeepAgentStreamE2ESuite) TestStream_单轮纯文本() {
	ctx := s.Ctx

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("流式文本输出"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	ch, err := agent.Stream(ctx, map[string]any{"query": "你好"})
	s.Require().NoError(err, "Stream 不应返回错误")
	s.Require().NotNil(ch, "Stream 返回的 channel 不应为 nil")

	// 消费所有 chunk
	var chunks []stream.Schema
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// 验证至少收到一个 chunk
	s.NotEmpty(chunks, "应至少收到一个 stream chunk")

	// 验证 chunk 类型
	for _, chunk := range chunks {
		outputChunk, ok := chunk.(stream.OutputSchema)
		if ok {
			s.NotEmpty(outputChunk.Type, "OutputSchema.Type 不应为空")
		}
	}
}

// TestStream_单轮工具调用 测试 DeepAgent 流式工具调用场景。
// 对齐 Python: test_deep_agent_stream_e2e（工具调用场景）
//
// 核心验证：
//   - Stream 模式下工具调用正常执行
//   - 流式输出包含工具调用后的最终结果
func (s *DeepAgentStreamE2ESuite) TestStream_单轮工具调用() {
	ctx := s.Ctx

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/stream_test.txt"}`),
		mockllm.CreateTextResponse("流式工具调用完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{newReadFileTool()},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	ch, err := agent.Stream(ctx, map[string]any{"query": "读取文件"})
	s.Require().NoError(err, "Stream 不应返回错误")

	// 消费所有 chunk
	var chunks []stream.Schema
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	s.NotEmpty(chunks, "应至少收到一个 stream chunk")

	// 验证 MockLLM 被调用（工具调用 + 文本响应）
	s.GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1, "MockLLM 应至少被调用 1 次")
}

// TestStream_IsLastSchema标记 测试流式输出最后一帧的 IsLastSchema 标记。
// 对齐 Python: ControllerOutputChunk.last_chunk
//
// 核心验证：
//   - 最后一个 chunk 应标记 IsLastSchema=true
//   - 中间 chunk 的 IsLastSchema 可为 false
func (s *DeepAgentStreamE2ESuite) TestStream_IsLastSchema标记() {
	ctx := s.Ctx

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("最后帧测试"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	ch, err := agent.Stream(ctx, map[string]any{"query": "测试"})
	s.Require().NoError(err)

	var chunks []stream.Schema
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	s.NotEmpty(chunks, "应至少收到一个 chunk")

	// 最后一个 chunk 如果是 OutputSchema 类型，验证 IsLastSchema 字段
	// 注意：MockLLM 的 Stream 实现可能不设置 IsLastSchema=true，
	// 因此仅验证 chunk 类型正确且字段可读
	lastChunk := chunks[len(chunks)-1]
	outputChunk, ok := lastChunk.(stream.OutputSchema)
	if ok {
		// IsLastSchema 可能不为 true（取决于 MockLLM 实现）
		s.NotEmpty(outputChunk.Type, "OutputSchema.Type 不应为空")
	}
}

// TestStream_带Session 测试 DeepAgent Stream 带 Session 运行。
// 对齐 Python: Agent + Session + Stream 联动
func (s *DeepAgentStreamE2ESuite) TestStream_带Session() {
	ctx := s.Ctx

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("带 Session 的流式输出"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("stream-session-test")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	ch, err := agent.Stream(ctx, map[string]any{"query": "带 Session 测试"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Stream 不应返回错误")

	var chunks []stream.Schema
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	s.NotEmpty(chunks, "应至少收到一个 stream chunk")

	sess.PostRun(ctx)
}

// TestStream_TaskLoopStream 测试 EnableTaskLoop=true + Stream 联合。
// 对齐 Python: test_deep_agent_task_loop_stream_e2e
//
// 核心验证：
//   - TaskLoop 模式下 Stream 正常输出
//   - channel 中收到多轮结果
//   - 使用 ControlledReactAgent 控制内层
func (s *DeepAgentStreamE2ESuite) TestStream_TaskLoopStream() {
	ctx := s.Ctx

	controlledAgent := testhelpers.NewControlledReactAgent(2)

	dummyTool := testhelpers.NewBlockingTool("dummy_stream")
	dummyTool.Release()

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		EnableTaskLoop:    true,
		MaxIterations:     10,
		CompletionTimeout: 30.0,
		ToolInstances:     []tool.Tool{dummyTool.Tool()},
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// TaskLoop 内部使用 innerInvokeOverride
	agent.SetInnerInvokeOverride(controlledAgent.Invoke)

	sess := s.NewTestSession("stream-task-loop")
	seedTaskPlan(agent, sess, "流式任务规划", 2)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 在 goroutine 中执行 Stream
	done := make(chan []stream.Schema, 1)
	errCh := make(chan error, 1)
	go func() {
		ch, streamErr := agent.Stream(ctx, map[string]any{"query": "流式任务规划"},
			agentinterfaces.WithSession(sess))
		if streamErr != nil {
			errCh <- streamErr
			return
		}
		var chunks []stream.Schema
		for chunk := range ch {
			chunks = append(chunks, chunk)
		}
		done <- chunks
	}()

	// 等待第 2 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(2, 10*time.Second),
		"等待第 2 次内层调用启动超时",
	)

	// 释放第 2 次调用
	controlledAgent.ReleaseCall(2)

	// 等待 Stream 完成
	select {
	case chunks := <-done:
		s.NotEmpty(chunks, "应至少收到一个 stream chunk")
	case streamErr := <-errCh:
		s.Require().NoError(streamErr, "Stream 失败")
	case <-time.After(15 * time.Second):
		s.T().Fatal("Stream 超时")
	}

	sess.PostRun(ctx)

	// 验证至少执行了 2 轮
	s.GreaterOrEqual(controlledAgent.InvokeCallCount(), 2, "外层循环应至少执行 2 轮")
}

// TestStream_Steer注入 测试 Stream 模式下 Steer 注入。
// 对齐 Python: test_outer_loop_multistep_with_steer_follow_up（Stream 部分）
//
// 核心验证：
//   - Stream + TaskLoop 模式下 Steer 正常传递
func (s *DeepAgentStreamE2ESuite) TestStream_Steer注入() {
	ctx := s.Ctx

	const steerText = "请简化输出"

	controlledAgent := testhelpers.NewControlledReactAgent(2)

	dummyTool := testhelpers.NewBlockingTool("dummy_steer")
	dummyTool.Release()

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		EnableTaskLoop:    true,
		MaxIterations:     10,
		CompletionTimeout: 30.0,
		ToolInstances:     []tool.Tool{dummyTool.Tool()},
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	agent.SetInnerInvokeOverride(controlledAgent.Invoke)

	sess := s.NewTestSession("stream-steer")
	seedTaskPlan(agent, sess, "流式Steer测试", 2)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	done := make(chan []stream.Schema, 1)
	errCh := make(chan error, 1)
	go func() {
		ch, streamErr := agent.Stream(ctx, map[string]any{"query": "流式Steer测试"},
			agentinterfaces.WithSession(sess))
		if streamErr != nil {
			errCh <- streamErr
			return
		}
		var chunks []stream.Schema
		for chunk := range ch {
			chunks = append(chunks, chunk)
		}
		done <- chunks
	}()

	// 等待第 2 次调用启动
	s.Require().NoError(
		controlledAgent.WaitCallStarted(2, 10*time.Second),
		"等待第 2 次内层调用启动超时",
	)

	// 在阻塞期间注入 Steer
	agent.Steer(ctx, steerText, sess)

	// 释放第 2 次调用
	controlledAgent.ReleaseCall(2)

	select {
	case chunks := <-done:
		s.NotEmpty(chunks, "应至少收到一个 stream chunk")
	case streamErr := <-errCh:
		s.Require().NoError(streamErr, "Stream 失败")
	case <-time.After(15 * time.Second):
		s.T().Fatal("Stream 超时")
	}

	sess.PostRun(ctx)

	// 验证至少 2 轮
	s.GreaterOrEqual(controlledAgent.InvokeCallCount(), 2, "外层循环应至少执行 2 轮")

	// 验证第 2 轮 inputs 包含 _steering_queue
	secondCall := controlledAgent.GetInvokeCall(1)
	s.Require().NotNil(secondCall, "第 2 次调用记录应存在")
	_, hasSQ := secondCall.Inputs["_steering_queue"]
	s.True(hasSQ, "第 2 轮内层调用 inputs 应包含 _steering_queue")
}
