//go:build integration

package model_anomaly_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
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
		if schema.Type == "error" {
			if message, ok := payload["error"].(string); ok {
				errText.WriteString(message)
			}
			return
		}
		if content, ok := payload["content"].(string); ok {
			output.WriteString(content)
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
