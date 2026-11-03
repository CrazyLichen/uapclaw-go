//go:build integration

package adapter_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
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
// 断言每轮 usage_summary 的数值与模型实际收到的 messages。
//
// 对应 Python 代码：interface_deep.py L4723-4736（llm_usage 累加读嵌套
// usage_metadata）、L4957-4976（usage_summary 每轮独立累加）。
type SessionUsageIsolationSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
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

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponseWithUsage("A1 回复", 1000, 100),
		mockllm.CreateTextResponseWithUsage("B1 回复", 120, 20),
		mockllm.CreateTextResponseWithUsage("A2 回复", 1500, 100),
	)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 1) 会话 A 首轮：usage_summary 必须存在且取到真实 token 数
	usageA1 := s.runTurn(d, usageIsolationSessionA, "req-usage-a1", "会话A第一问 "+usageIsolationMarkerA)
	s.Require().NotNil(usageA1, "A 首轮应发出 chat.usage_summary（llm_usage 累加为 0 时该事件不会发）")
	s.Equal(1000, usageA1["input_tokens"], "input_tokens 应等于模型上报值")
	s.Equal(100, usageA1["output_tokens"], "output_tokens 应等于模型上报值")
	s.Equal(1100, usageA1["total_tokens"], "total_tokens 应等于模型上报值")

	// 2) 会话 B 首轮：usage 必须是本会话的值，不能是 A 的累计
	usageB1 := s.runTurn(d, usageIsolationSessionB, "req-usage-b1", "会话B第一问")
	s.Require().NotNil(usageB1, "B 首轮应发出 chat.usage_summary")
	s.Equal(120, usageB1["input_tokens"], "新会话 input_tokens 不得沿用上个会话的数值")
	s.Equal(20, usageB1["output_tokens"], "新会话 output_tokens 不得沿用上个会话的数值")
	s.Equal(140, usageB1["total_tokens"], "新会话 total_tokens 不得是上个会话的累计")

	// 3) B 首轮的 prompt 不得夹带 A 的历史
	bCall := s.MockLLM.GetStreamCall(1)
	s.Require().NotNil(bCall, "应有第 2 次 Stream 调用记录（B 首轮）")
	s.NotContains(s.messagesText(bCall.Messages),
		usageIsolationMarkerA, "新会话的 prompt 不得包含上个会话的历史消息")

	// 4) 反向对照：回到 A 第二轮，必须能看到 A 首轮内容（同会话历史照常）
	usageA2 := s.runTurn(d, usageIsolationSessionA, "req-usage-a2", "会话A第二问")
	s.Require().NotNil(usageA2, "A 第二轮应发出 chat.usage_summary")
	s.Equal(1500, usageA2["input_tokens"], "A 第二轮的 input_tokens 应等于本轮模型上报值")

	a2Call := s.MockLLM.GetStreamCall(2)
	s.Require().NotNil(a2Call, "应有第 3 次 Stream 调用记录（A 第二轮）")
	s.Contains(s.messagesText(a2Call.Messages),
		usageIsolationMarkerA, "同一会话的第二轮应携带首轮历史")
	s.NotContains(s.messagesText(a2Call.Messages),
		"会话B第一问", "会话 A 的 prompt 不得包含会话 B 的消息")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runTurn 以指定会话跑一轮流式对话，返回本轮 chat.usage_summary 的 usage 子对象。
func (s *SessionUsageIsolationSuite) runTurn(
	d *adapter.DeepAdapter,
	sessionID string,
	requestID string,
	query string,
) map[string]any {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": "agent.plan"})
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

	var usage map[string]any
	for chunk := range chunkCh {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		eventType, _ := chunk.Payload["event_type"].(string)
		if eventType != "chat.usage_summary" {
			continue
		}
		if sub, ok := chunk.Payload["usage"].(map[string]any); ok {
			usage = sub
		}
	}
	return usage
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
