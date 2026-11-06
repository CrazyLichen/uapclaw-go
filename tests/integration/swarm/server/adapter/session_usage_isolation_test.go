//go:build integration

package adapter_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionUsageIsolationSuite 验证跨会话的用量与上下文隔离。
//
// 三个问题同一批反馈（2026-11-03）：
//  1. 新建会话后 usage 统计是否沿用上个会话（会话级状态是否被串用）；
//  2. usage_summary 的 token 数是否真能取到（llm_usage 的嵌套 payload 是否被读到）；
//  3. 新会话的 prompt 是否夹带上个会话的历史（上下文检索/检查点恢复是否越界）。
//
// 手段：MockLLM 预设三份带用量的响应，驱动 DeepAdapter 走 A→B→A 三轮，
// 断言每轮 usage_summary / context.usage 的数值与模型实际收到的 messages。
//
// 对应 Python 代码：interface_deep.py L4723-4736（llm_usage 累加读嵌套
// usage_metadata）、L4957-4976（usage_summary 每轮独立累加）。
type SessionUsageIsolationSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// turnEvents 一轮对话中与用量相关的事件。
type turnEvents struct {
	// usage chat.usage_summary 的 usage 子对象（消息下方 in/out/total）
	usage map[string]any
	// contextUsage context.usage 的完整 payload（状态卡「上下文窗口」）
	contextUsage map[string]any
}

// usageStep 一次流式模型调用的预设响应（带 token 用量）。
type usageStep struct {
	// text 回复正文
	text string
	// inputTokens 上报的输入 token 数
	inputTokens int
	// outputTokens 上报的输出 token 数
	outputTokens int
}

// usageScriptClient 按脚本返回带用量的流式响应的模型客户端。
//
// 之所以在测试内自建而不是扩展共享夹具 mockllm：mockllm 位于 tests/ 下且非
// _test.go，改动行会被「改动行覆盖率」门禁判 0%（unit/integration 两个 job
// 都不对其做插桩）。本类型嵌入 MockModelClient 复用其余方法，只覆写
// Stream 与 Factory，并把每次请求的 messages 记录下来供 prompt 隔离断言。
type usageScriptClient struct {
	*mockllm.MockModelClient
	// mu 保护 script / streams
	mu sync.Mutex
	// script 待消费的响应脚本
	script []usageStep
	// streams 每次 Stream 调用收到的消息参数
	streams []model_clients.MessagesParam
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// usageIsolationSessionA 会话 A 的 session_id
	usageIsolationSessionA = "sess_usage_iso_a"
	// usageIsolationSessionB 会话 B 的 session_id
	usageIsolationSessionB = "sess_usage_iso_b"
	// usageIsolationMarkerA 会话 A 首轮的用户消息标记，用于判定是否被带进 B
	usageIsolationMarkerA = "unique-marker-A1"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionUsageIsolationSuite 启动会话用量隔离套件。
func TestSessionUsageIsolationSuite(t *testing.T) {
	suite.Run(t, new(SessionUsageIsolationSuite))
}

// SetupTest 每个测试方法前构建隔离环境（数据目录 / MockLLM / env 备份）。
func (s *SessionUsageIsolationSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_usage_isolation")
}

// TestSwitchSession_UsageSummary与会话隔离 覆盖 usage_summary 数值与会话间
// 上下文隔离：
//   - A 首轮必须有 usage_summary，且数值等于 Mock 上报值（当前 llm_usage
//     读平铺键会得到 0，事件根本不发，这条首先失败）；
//   - B 首轮 usage 不得是 A 的累计；
//   - B 首轮发给模型的 messages 不含 A 的用户消息；
//   - 反向对照：A 第二轮必须能看到 A 首轮内容，证明隔离不是"历史整体丢失"。
func (s *SessionUsageIsolationSuite) TestSwitchSession_UsageSummary与会话隔离() {
	s.env.WriteConfigYAML(s.T(), "")

	// 用带用量的脚本客户端替换本环境的 provider 工厂（配置里的 provider 不变）
	client := newUsageScriptClient(
		usageStep{text: "A1 回复", inputTokens: 1000, outputTokens: 100},
		usageStep{text: "B1 回复", inputTokens: 120, outputTokens: 20},
		usageStep{text: "A2 回复", inputTokens: 1500, outputTokens: 100},
	)
	registry := model_clients.GetClientRegistry()
	registry.Unregister(s.env.Provider, "llm")
	registry.Register(s.env.Provider, "llm", client.Factory())

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 1) 会话 A 首轮：usage_summary 必须存在且取到真实 token 数
	a1 := s.runTurn(d, usageIsolationSessionA, "req-usage-a1", "会话A第一问 "+usageIsolationMarkerA)
	s.Require().NotNil(a1.usage, "A 首轮应发出 chat.usage_summary（llm_usage 累加为 0 时该事件不会发）")
	s.Equal(1000, a1.usage["input_tokens"], "input_tokens 应等于模型上报值")
	s.Equal(100, a1.usage["output_tokens"], "output_tokens 应等于模型上报值")
	s.Equal(1100, a1.usage["total_tokens"], "total_tokens 应等于模型上报值")
	s.Require().NotNil(a1.contextUsage, "A 首轮应发出 context.usage（状态卡「上下文窗口」）")
	// context.usage 载荷是 context-usage.v1 快照：分子在 context_window.input_tokens，
	// 取 provider 上报的输入侧（输出侧下一轮才进历史，alignment 见 analyze BuildSnapshot）
	s.Equal("context-usage.v1", a1.contextUsage["schema_version"], "载荷必须是 v1 快照")
	a1Window, ok := a1.contextUsage["context_window"].(map[string]any)
	s.Require().True(ok, "快照必须带 context_window，实际 %v", a1.contextUsage)
	s.Equal(float64(1000), a1Window["input_tokens"], "context_window.input_tokens 应为本轮 provider 上报输入量")

	// 2) 会话 B 首轮：usage 必须是本会话的值，不能是 A 的累计
	b1 := s.runTurn(d, usageIsolationSessionB, "req-usage-b1", "会话B第一问")
	s.Require().NotNil(b1.usage, "B 首轮应发出 chat.usage_summary")
	s.Equal(120, b1.usage["input_tokens"], "新会话 input_tokens 不得沿用上个会话的数值")
	s.Equal(20, b1.usage["output_tokens"], "新会话 output_tokens 不得沿用上个会话的数值")
	s.Equal(140, b1.usage["total_tokens"], "新会话 total_tokens 不得是上个会话的累计")
	s.Require().NotNil(b1.contextUsage, "B 首轮应发出 context.usage")
	b1Window, ok := b1.contextUsage["context_window"].(map[string]any)
	s.Require().True(ok, "B 首轮快照必须带 context_window")
	s.Equal(float64(120), b1Window["input_tokens"],
		"新会话的用量必须是自己的，不能带着上个会话的值")

	// 3) B 首轮的 prompt 不得夹带 A 的历史
	bCall := client.streamCall(1)
	s.NotContains(s.messagesText(bCall),
		usageIsolationMarkerA, "新会话的 prompt 不得包含上个会话的历史消息")

	// 4) 反向对照：回到 A 第二轮，必须能看到 A 首轮内容（同会话历史照常）
	a2 := s.runTurn(d, usageIsolationSessionA, "req-usage-a2", "会话A第二问")
	s.Require().NotNil(a2.usage, "A 第二轮应发出 chat.usage_summary")
	s.Equal(1500, a2.usage["input_tokens"], "A 第二轮的 input_tokens 应等于本轮模型上报值")
	s.Require().NotNil(a2.contextUsage, "A 第二轮应发出 context.usage")
	a2Window, ok := a2.contextUsage["context_window"].(map[string]any)
	s.Require().True(ok, "A 第二轮快照必须带 context_window")
	s.Equal(float64(1500), a2Window["input_tokens"], "A 第二轮用量应为本轮 provider 上报输入量")

	a2Call := client.streamCall(2)
	s.Contains(s.messagesText(a2Call),
		usageIsolationMarkerA, "同一会话的第二轮应携带首轮历史")
	s.NotContains(s.messagesText(a2Call),
		"会话B第一问", "会话 A 的 prompt 不得包含会话 B 的消息")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newUsageScriptClient 创建带用量脚本的模型客户端（复用 mockllm 的其余能力）。
func newUsageScriptClient(steps ...usageStep) *usageScriptClient {
	return &usageScriptClient{
		MockModelClient: mockllm.NewMockModelClient(),
		script:          steps,
	}
}

// Stream 覆写流式调用：按脚本返回单个带 usage_metadata 的 chunk，并记录入参消息。
func (c *usageScriptClient) Stream(
	_ context.Context,
	messages model_clients.MessagesParam,
	_ ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	c.mu.Lock()
	step := usageStep{text: "默认回复"}
	if len(c.script) > 0 {
		step = c.script[0]
		c.script = c.script[1:]
	}
	c.streams = append(c.streams, messages)
	c.mu.Unlock()

	chunk := llmschema.NewAssistantMessageChunk(step.text)
	chunk.UsageMetadata = &llmschema.UsageMetadata{
		ModelName:    "mock-model",
		InputTokens:  step.inputTokens,
		OutputTokens: step.outputTokens,
		TotalTokens:  step.inputTokens + step.outputTokens,
	}

	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	ch <- chunk
	close(ch)
	return ch, nil
}

// Factory 返回本客户端的注册工厂（覆写嵌入类型的工厂，确保取到的是包装体）。
func (c *usageScriptClient) Factory() model_clients.ClientFactory {
	return func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return c, nil
	}
}

// streamCall 返回第 i 次 Stream 调用收到的消息参数（0-indexed，越界返回空）。
func (c *usageScriptClient) streamCall(i int) model_clients.MessagesParam {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i < 0 || i >= len(c.streams) {
		return model_clients.MessagesParam{}
	}
	return c.streams[i]
}

// runTurn 以指定会话跑一轮流式对话，返回本轮的关键事件。
//
// 同时收集两类用量事件：
//   - chat.usage_summary：助手消息下方的 in/out/total 统计（每轮独立累加）
//   - context.usage：右侧状态卡「上下文窗口 X/Y(%)」，分子为本轮 usage_metadata
func (s *SessionUsageIsolationSuite) runTurn(
	d *adapter.DeepAdapter,
	sessionID string,
	requestID string,
	query string,
) turnEvents {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": "agent.work.plan"})
	s.Require().NoError(err)

	sid := sessionID
	req := &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{"query": query})
	s.Require().NoError(streamErr, "ProcessMessageStreamImpl 不应返回启动错误")

	var events turnEvents
	for chunk := range chunkCh {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		eventType, _ := chunk.Payload["event_type"].(string)
		switch eventType {
		case "chat.usage_summary":
			if sub, ok := chunk.Payload["usage"].(map[string]any); ok {
				events.usage = sub
			}
		case "context.usage":
			events.contextUsage = chunk.Payload
		}
	}
	return events
}

// messagesText 拼接一次模型调用收到的全部文本，用于断言 prompt 内容。
func (s *SessionUsageIsolationSuite) messagesText(messages model_clients.MessagesParam) string {
	s.T().Helper()
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
