//go:build integration

package model_anomaly_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AnomalySuite 模型异常检测护栏集成测试套件。
type AnomalySuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAnomalySuite 运行套件。
func TestAnomalySuite(t *testing.T) {
	suite.Run(t, new(AnomalySuite))
}

// TestStreamingRepeatTriggersRetry 首轮退化重复 → rail 重试 → 第二轮正常收尾。
//
// 链路：DeepAgent.Stream → 内层 ReAct 流循环 → 检查器检出 → MODEL_CALL_FAILED
// → rail.OnModelException → RequestRetry → RailExecutor 重跑 → mock 第二响应。
func (s *AnomalySuite) TestStreamingRepeatTriggersRetry() {
	ctx := s.Ctx
	baseline := s.MockLLM.StreamCallCount()
	s.MockLLM.WithStreamScripts(mockllm.CreateRepeatStreamScript("abc", 60))
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("done"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{MaxIterations: 5})
	s.Require().NoError(err)

	output, errText := s.drainStream(agent, ctx, "你好")
	s.Require().Empty(errText, "重试后应正常收尾，不应报错")
	s.Require().Contains(output, "done", "最终输出应为第二轮内容")
	// Mock 的 streamCount 跨用例累积，故用增量断言
	s.Require().Equal(baseline+2, s.MockLLM.StreamCallCount(), "首次命中 + 重试成功 = 本次用例两次流调用")
}

// TestStreamFrameTimeoutTriggersRetry 首帧卡死 → 超时错误 → rail 重试 → 第二轮正常。
//
// 链路：Model.Stream select 超时 → StreamError 哨兵 → callLLMStream 转 error
// → rail.OnModelException（marker 命中）→ 重试。
func (s *AnomalySuite) TestStreamFrameTimeoutTriggersRetry() {
	ctx := s.Ctx

	firstBudget := 0.05
	idleBudget := 0.05
	originalFirst := s.ClientConfig.StreamFirstChunkTimeout
	originalIdle := s.ClientConfig.StreamIdleTimeout
	s.ClientConfig.StreamFirstChunkTimeout = &firstBudget
	s.ClientConfig.StreamIdleTimeout = &idleBudget
	s.T().Cleanup(func() {
		s.ClientConfig.StreamFirstChunkTimeout = originalFirst
		s.ClientConfig.StreamIdleTimeout = originalIdle
	})

	baseline := s.MockLLM.StreamCallCount()
	s.MockLLM.WithStreamScripts(mockllm.StreamChunkScript{
		StallBefore: 300 * time.Millisecond,
		Chunks:      []*llmschema.AssistantMessageChunk{llmschema.NewAssistantMessageChunk("late")},
	})
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("recovered"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{MaxIterations: 5})
	s.Require().NoError(err)

	output, errText := s.drainStream(agent, ctx, "你好")
	s.Require().Empty(errText, "重试后应正常收尾，不应报错")
	s.Require().Contains(output, "recovered", "最终输出应为超时重试后的内容")
	s.Require().Equal(baseline+2, s.MockLLM.StreamCallCount(), "首次超时 + 重试成功 = 本次用例两次流调用")
}

// TestToolLoopCompactConfigured 压缩配置经工厂透传后真实生效（装配可达性）。
//
// 只验证"配置生效且不误伤正常回答"：折叠行为本身由 rail 单测覆盖。
func (s *AnomalySuite) TestToolLoopCompactConfigured() {
	ctx := s.Ctx
	rail, err := rails.NewModelAnomalyDetectionRail(rails.WithToolLoopCompact(rails.ToolLoopCompactConfig{
		Enabled:              true,
		ConsecutiveThreshold: 3,
		BailoutThreshold:     0,
	}))
	s.Require().NoError(err)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ok"))
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
		Rails:         []agentinterfaces.AgentRail{rail},
	})
	s.Require().NoError(err)

	output, errText := s.drainStream(agent, ctx, "你好")
	s.Require().Empty(errText)
	s.Require().Contains(output, "ok")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// drainStream 消费 DeepAgent.Stream，返回拼接后的可见输出与错误文案。
func (s *AnomalySuite) drainStream(agent interface {
	Stream(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error)
}, ctx context.Context, query string) (string, string) {
	ch, err := agent.Stream(ctx, map[string]any{"query": query})
	s.Require().NoError(err)

	var output strings.Builder
	var errText strings.Builder
	appendPayload := func(schema *stream.OutputSchema) {
		payload, ok := schema.Payload.(map[string]any)
		if !ok {
			return
		}
		// 错误帧有两种形状：emitStreamError 的 Type=error + error 字段，
		// 与 WriteInvokeResultToStream 的 result_type=error + output 字段。
		if schema.Type == "error" || payload["result_type"] == "error" {
			if message, ok := payload["error"].(string); ok {
				errText.WriteString(message)
				return
			}
			if message, ok := payload["output"].(string); ok {
				errText.WriteString(message)
			}
			return
		}
		if content, ok := payload["content"].(string); ok {
			output.WriteString(content)
			return
		}
		if message, ok := payload["output"].(string); ok {
			output.WriteString(message)
		}
	}
	for chunk := range ch {
		switch typed := chunk.(type) {
		case *stream.OutputSchema:
			appendPayload(typed)
		case stream.OutputSchema:
			appendPayload(&typed)
		}
	}
	return output.String(), errText.String()
}

// TestToolLoopCompactInjectsWarning 连续相同工具调用被折叠成警告并注入后续模型上下文。
//
// 链路：mock 反复返回同一 tool_call → rail.BeforeModelCall 折叠历史 →
// 下一次模型调用收到的 messages 中出现双语警告 UserMessage。
func (s *AnomalySuite) TestToolLoopCompactInjectsWarning() {
	ctx := s.Ctx
	rail, err := rails.NewModelAnomalyDetectionRail(rails.WithToolLoopCompact(rails.ToolLoopCompactConfig{
		Enabled:              true,
		ConsecutiveThreshold: 2,
		BailoutThreshold:     0,
	}))
	s.Require().NoError(err)

	baseline := s.MockLLM.StreamCallCount()
	responses := make([]mockllm.MockResponse, 0, 7)
	for i := 0; i < 6; i++ {
		responses = append(responses, mockllm.CreateToolCallResponse("echo_tool", `{"x":1}`))
	}
	responses = append(responses, mockllm.CreateTextResponse("done"))
	s.MockLLM.SetResponses(responses...)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 8,
		ToolInstances: []tool.Tool{newEchoTool("echo_tool")},
		Rails:         []agentinterfaces.AgentRail{rail},
		// 本用例只关心折叠，rail 由 Rails 显式提供；关掉默认实例避免双份
		EnableModelAnomalyDetectionRail: anomalyDisabledFlag(),
	})
	s.Require().NoError(err)

	output, errText := s.drainStream(agent, ctx, "反复读同一个文件")
	s.Require().Empty(errText, "折叠后应正常收尾")
	s.Require().Contains(output, "done")

	injected := false
	for i := baseline; i < s.MockLLM.StreamCallCount(); i++ {
		record := s.MockLLM.GetStreamCall(i)
		if record == nil {
			continue
		}
		for _, message := range record.Messages.Messages() {
			userMessage, ok := message.(*llmschema.UserMessage)
			if ok && strings.Contains(userMessage.Content.Text(), "请跳出重复工具调用") {
				injected = true
			}
		}
	}
	s.Require().True(injected, "后续模型调用应收到折叠警告（UserMessage）")
}

// TestToolLoopCompactBailout 第 N 次命中折叠时以 AbortError 中止整轮任务。
//
// 链路：rail.BeforeModelCall 抛 AbortError(cause=CONTEXT_EXECUTION_ERROR)
// → RailExecutor 不重试 → DeepAgent.Stream 输出 error 帧。
func (s *AnomalySuite) TestToolLoopCompactBailout() {
	ctx := s.Ctx
	// 关掉瞬态重试：bailout 抛出的 AbortError 没有状态码，会被瞬时分类判为"可重试"
	// （上游同样如此），重试期间 mock 队列耗尽会把"中止"变成"成功"，掩盖本用例要验的链路。
	rail, err := rails.NewModelAnomalyDetectionRail(
		rails.WithToolLoopCompact(rails.ToolLoopCompactConfig{
			Enabled:              true,
			ConsecutiveThreshold: 2,
			BailoutThreshold:     3,
		}),
		// 无需关瞬态重试：bailout 的 cause 带「控制流中止」标记，
		// rail 与 ModelBackupRail 都会跳过，默认配置下也不会退避重试。
	)
	s.Require().NoError(err)

	baseline := s.MockLLM.StreamCallCount()
	responses := make([]mockllm.MockResponse, 0, 40)
	for i := 0; i < 40; i++ {
		responses = append(responses, mockllm.CreateToolCallResponse("echo_tool", `{"x":1}`))
	}
	s.MockLLM.SetResponses(responses...)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations:                   30,
		ToolInstances:                   []tool.Tool{newEchoTool("echo_tool")},
		Rails:                           []agentinterfaces.AgentRail{rail},
		EnableModelAnomalyDetectionRail: anomalyDisabledFlag(),
	})
	s.Require().NoError(err)

	_, errText := s.drainStream(agent, ctx, "反复读同一个文件")
	s.Require().Contains(errText, "automatically aborted", "第 3 次命中应 bailout 并冒泡到流错误帧")

	// 远小于 MaxIterations，证明是兜底中止而不是跑满迭代预算
	calls := s.MockLLM.StreamCallCount() - baseline
	s.Require().Less(calls, 12, "应在迭代预算耗尽前中止，实际调用 %d 次", calls)
}

// ──────────────────────────── 测试工具 ────────────────────────────

// echoTool 固定返回同一结果的测试工具（用于制造"连续相同工具调用"）。
type echoTool struct {
	// name 工具名（需与 mock 的 tool_call 名称一致）
	name string
	// card 工具卡片
	card *tool.ToolCard
}

// newEchoTool 创建 echo 工具。
func newEchoTool(name string) *echoTool {
	return &echoTool{
		name: name,
		card: tool.NewToolCard(name, "回显固定结果（集成测试用）", nil, nil),
	}
}

// Card 返回工具卡片。
func (t *echoTool) Card() *tool.ToolCard { return t.card }

// Invoke 固定返回同一结果。
func (t *echoTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"output": "fixed"}, nil
}

// Stream 不支持流式。
func (t *echoTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// anomalyDisabledFlag 返回 false 指针，用于关闭默认注册的模型异常检测 Rail。
func anomalyDisabledFlag() *bool {
	disabled := false
	return &disabled
}
