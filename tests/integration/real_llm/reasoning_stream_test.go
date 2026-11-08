//go:build integration && llm

package real_llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	swarmruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReasoningStreamSuite 真实模型的「思考（reasoning_content）」输出端到端判定套件。
//
// 分层判定，任一层断裂都能独立定位：
//  1. 模型客户端层：流式 chunk 是否携带 reasoning_content
//     （parse_stream_chunk.go 解析 delta.reasoning_content）
//  2. Agent 会话流层：ReActAgent 是否写出 OutputSchema(type="llm_reasoning")
//     （react_model_call.go callLLMStream）
//  3. 界面事件层：DeepAdapter 是否产出 chat.reasoning
//     （deep_adapter.go ProcessMessageStreamImpl）
//
// 规划模式（mode=agent.plan + SwitchMode("plan")）单独覆盖：本用例断言
// 规划模式并不会吞掉思考输出，与 Python 行为一致。
//
// 运行方式:
//
//	LLM_API_KEY=... go test -tags="sqlite_fts5 test integration llm" \
//	  -run TestReasoningStreamSuite ./tests/integration/real_llm/ -v
type ReasoningStreamSuite struct {
	isuite.RealLLMSuite
}

// reasoningProbe 推理输出观测结果，供三个层次的断言复用。
type reasoningProbe struct {
	// chunkCount 收到的流式 chunk 总数
	chunkCount int
	// reasoningChunks 携带非空 reasoning_content 的 chunk 数
	reasoningChunks int
	// reasoningText 拼接后的推理全文
	reasoningText strings.Builder
	// contentChunks 携带非空 content 的 chunk 数
	contentChunks int
}

// ──────────────────────────── 常量 ────────────────────────────

// reasoningProbeQuery 诱导推理的提问（需要两步推理，非推理模型会直接给答案）。
const reasoningProbeQuery = "一个农夫有 17 只羊，除了 9 只以外都死了，还剩几只？只回答数字。"

// reasoningEventCollectTimeout 界面事件层的收集超时。
// 规划模式第一轮之后会起子代理，链路很长；拿到 chat.reasoning 即提前结束，
// 因此该超时只在模型迟迟不产出时兜底。
const reasoningEventCollectTimeout = 3 * time.Minute

// maxPlanEvents 单次规划模式采集的事件条数上限，避免流落到子代理阶段长时间等待。
const maxPlanEvents = 60

// ──────────────────────────── 导出函数 ────────────────────────────

// TestReasoningStreamSuite 运行真实模型思考输出判定套件
func TestReasoningStreamSuite(t *testing.T) {
	suite.Run(t, new(ReasoningStreamSuite))
}

// TestReasoning_模型层_流式返回推理内容 判定第 1 层：真实模型是否返回 reasoning_content。
//
// 判定标准：至少一个 chunk 的 ReasoningContent 非空，且拼接结果非空。
// 若此用例失败，说明当前模型/中转不产出推理，后续两层的失败都是它的下游后果。
func (s *ReasoningStreamSuite) TestReasoning_模型层_流式返回推理内容() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 90*time.Second)
	defer cancel()

	ch, err := model.Stream(ctx, model_clients.NewTextMessagesParam(reasoningProbeQuery))
	s.Require().NoError(err, "真实 LLM Stream 调用失败")

	probe := collectReasoningProbe(ctx, ch)
	s.T().Logf("[reasoning-model] chunks=%d reasoningChunks=%d contentChunks=%d reasoningRunes=%d",
		probe.chunkCount, probe.reasoningChunks, probe.contentChunks, len(probe.reasoningText.String()))

	s.Greater(probe.chunkCount, 0, "模型流应至少产出一个 chunk")
	s.Greater(probe.reasoningChunks, 0,
		"模型层未返回 reasoning_content：当前模型 %q 不产出推理输出（或中转把它剥掉了）",
		s.RealModelConfig.ModelName)
	s.NotEmpty(strings.TrimSpace(probe.reasoningText.String()), "reasoning_content 拼接结果不应为空")
}

// TestReasoning_Agent会话流_产出llm_reasoning 判定第 2 层：ReActAgent 是否把推理写进会话流。
//
// 判定标准：DeepAgent.Stream 的 chunk 中存在 OutputSchema(type="llm_reasoning")
// 且 payload.content 非空（该类型是 deep adapter 产出 chat.reasoning 的唯一来源）。
func (s *ReasoningStreamSuite) TestReasoning_Agent会话流_产出llm_reasoning() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 3*time.Minute)
	defer cancel()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("real_llm_reasoning_stream"),
		agentschema.WithAgentName("RealLLMReasoningStreamAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID(fmt.Sprintf("reasoning_stream_%d", time.Now().UnixNano())),
		session.WithEnvs(map[string]any{}),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	ch, err := agent.Stream(ctx, map[string]any{"query": reasoningProbeQuery},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "DeepAgent Stream 调用失败")

	chunks := collectStreamChunks(ctx, ch)

	reasoningChunks := 0
	reasoningText := strings.Builder{}
	contentChunks := 0
	typeCounts := map[string]int{}
	for _, c := range chunks {
		output, ok := c.(stream.OutputSchema)
		if !ok {
			typeCounts[fmt.Sprintf("%T", c)]++
			continue
		}
		typeCounts[output.Type]++
		payload, _ := output.Payload.(map[string]any)
		switch output.Type {
		case "llm_reasoning":
			if text, ok := payload["content"].(string); ok && text != "" {
				reasoningChunks++
				reasoningText.WriteString(text)
			}
		case "llm_output":
			if text, ok := payload["content"].(string); ok && text != "" {
				contentChunks++
			}
		}
	}
	s.T().Logf("[reasoning-agent] chunks=%d typeCounts=%v reasoningChunks=%d contentChunks=%d reasoningRunes=%d",
		len(chunks), typeCounts, reasoningChunks, contentChunks, len(reasoningText.String()))

	s.NotEmpty(chunks, "DeepAgent 流应至少产出一个 chunk")
	s.Greater(reasoningChunks, 0,
		"会话流中没有 llm_reasoning chunk（ReActAgent 未写推理，或 _streaming 未透传退化为 invoke）")
	s.NotEmpty(strings.TrimSpace(reasoningText.String()), "llm_reasoning 拼接内容不应为空")
	s.Greater(contentChunks, 0, "同一轮应同时产出 llm_output（判定推理与正文顺序）")
}

// TestReasoning_规划模式界面事件_产出chat_reasoning 判定第 3 层：规划模式下界面能否收到 chat.reasoning。
//
// 链路与界面完全一致：DeepAdapter.CreateInstance(mode=agent.plan) → SwitchMode("plan")
// → ProcessMessageStreamImpl → Runner.RunAgentStreaming → ReActAgent →
// session 流 llm_reasoning → 适配层 chat.reasoning。
//
// 判定标准：
//   - 事件流中出现 chat.reasoning 且 content 非空（界面拿得到思考）
//   - 出站 wire 编解码不丢 reasoning（最后一跳不吞事件）
//
// 刻意不断言「reasoning 必须早于首个 delta」：模型这一轮是否思考、先出正文还是
// 先思考属于模型自由行为（实测同一提示词两种都出现过），按真 LLM 只硬断言
// 确定性契约的原则，顺序仅打印日志。
func (s *ReasoningStreamSuite) TestReasoning_规划模式界面事件_产出chat_reasoning() {
	s.SkipIfNoRealLLM()

	// 全链路需要完整工作区（config.yaml models.defaults + react 段 + agent 目录）
	setupPromptEnv(s.T(), "auto_list")

	channel := "web"
	sessionID := fmt.Sprintf("%s_reasoning_plan_%d", channel, time.Now().UnixNano())
	requestID := fmt.Sprintf("reasoning-plan-%d", time.Now().UnixNano())

	// 提问刻意要求先探查再规划：规划模式提示词会强制模型先调 enter_plan_mode，
	// 因此这一轮必然「思考 + 工具调用」并存——正是界面上「只看到工具卡片」的场景
	Query := "先看一下当前工作目录里有哪些文件，然后说明你打算如何规划这个仓库的接口文档改造。只做规划，不要修改任何文件。"
	userPrompt := swarmruntime.BuildUserPrompt(Query, map[string]any{}, channel, "zh", nil, nil)
	inputs := map[string]any{
		"conversation_id": sessionID,
		"query":           userPrompt,
		"channel":         channel,
		"language":        "zh",
	}
	params, _ := json.Marshal(map[string]any{
		"query": Query,
		"mode":  "agent.work.plan",
		"files": map[string]any{},
	})
	ctx, cancel := context.WithTimeout(s.Ctx, reasoningEventCollectTimeout)
	defer cancel()

	// 真实模型「这一轮是否思考」不是确定性契约（同一提示词可能直接给正文+工具调用），
	// 因此按 deep_agent_e2e 的先例做有限重试：任一 attempt 拿到 chat.reasoning 即判定
	// 链路通；全部 attempt 都没有才判红，并打印每次的事件序列供定位。
	const maxPlanAttempts = 3
	var (
		lastOrder      []string
		reasoningText  strings.Builder
		firstReasoning int
		firstDelta     int
		firstToolCall  int
		toolResults    int
		lastTotal      int
	)
	for attempt := 1; attempt <= maxPlanAttempts; attempt++ {
		attemptSession := fmt.Sprintf("%s_%d", sessionID, attempt)
		attemptReq := swarmschema.NewAgentRequest(
			requestID+"-"+strconv.Itoa(attempt), channel, swarmschema.ReqMethodChatSend, params,
			swarmschema.WithAgentSessionID(attemptSession),
		)
		attemptInputs := map[string]any{}
		for k, v := range inputs {
			attemptInputs[k] = v
		}
		attemptInputs["conversation_id"] = attemptSession

		d := adapter.NewDeepAdapter()
		s.Require().NoError(d.CreateInstance(ctx, nil, "agent.work.plan", "plan"),
			"CreateInstance 失败（attempt=%d）", attempt)
		// 对齐界面：先切到 plan 子模式（写会话级 PlanModeState），再发消息
		s.Require().NoError(d.SwitchMode(ctx, attemptSession, "plan"),
			"SwitchMode(plan) 失败（attempt=%d）", attempt)

		ch, streamErr := d.ProcessMessageStreamImpl(ctx, attemptReq, attemptInputs)
		s.Require().NoError(streamErr, "ProcessMessageStreamImpl 失败（attempt=%d）", attempt)

		order, reasonText, dIdx, tIdx, trCount, deltaPreview := collectPlanEvents(ch, maxPlanEvents)
		lastOrder = order
		reasoningText.Reset()
		reasoningText.WriteString(reasonText)
		firstReasoning, firstDelta, firstToolCall = indexOf(order, "chat.reasoning"), dIdx, tIdx
		toolResults, lastTotal = trCount, len(order)
		s.T().Logf("[reasoning-plan] attempt=%d events=%d order=%v reasoningRunes=%d "+
			"firstReasoning=%d firstDelta=%d firstToolCall=%d toolResults=%d deltaPreview=%q",
			attempt, len(order), summarizeEventOrder(order), len(reasonText),
			firstReasoning, dIdx, tIdx, trCount, deltaPreview)

		_ = d.Cleanup()
		if firstReasoning >= 0 && strings.TrimSpace(reasonText) != "" {
			break
		}
	}

	s.NotEmpty(lastOrder, "规划模式流应至少产出一个事件")
	s.NotContains(lastOrder, "chat.error", "规划模式不应产出 chat.error")
	s.GreaterOrEqual(firstReasoning, 0,
		"界面事件层没有 chat.reasoning：规划模式下模型推理未透传到 UI"+
			"（诊断：先看用例 2 的 llm_reasoning 是否存在，再看 deltaPreview 判断模型这一轮是否根本没思考）")
	s.NotEmpty(strings.TrimSpace(reasoningText.String()), "chat.reasoning 的 content 不应为空")
	s.Greater(lastTotal, 0, "事件条数应大于 0")
	s.T().Logf("[reasoning-plan] 末次采集 toolResults=%d", toolResults)
	if firstDelta >= 0 || firstToolCall >= 0 {
		s.T().Logf("[reasoning-plan] 首个 reasoning=%d 首个 delta=%d 首个 tool_call=%d（同一轮内 Python 语义为 reasoning 先写，模型可先出正文）",
			firstReasoning, firstDelta, firstToolCall)
	}

	// 第 4 层：出站 wire 编解码不丢推理（adapter → AgentServer → gateway 的最后一跳）
	wireChunk := swarmschema.NewAgentResponseChunk(
		requestID, channel,
		map[string]any{"event_type": "chat.reasoning", "content": "推理片段"},
	)
	wire := e2a.EncodeAgentChunkForWire(wireChunk, "resp-1", 1, true)
	back, wireErr := e2a.ParseAgentServerWireChunk(wire)
	s.Require().NoError(wireErr, "wire 解码失败")
	s.Require().NotNil(back, "wire 解码结果不应为 nil")
	s.Require().NotNil(back.Payload, "wire 解码 payload 不应为 nil")
	s.Equal("chat.reasoning", back.Payload["event_type"], "wire 编解码后事件类型应仍是 chat.reasoning")
	s.Equal("推理片段", back.Payload["content"], "wire 编解码后 content 不应丢失")
}

// TestReasoningStreamDoc_包引用验证 保证包内引用在被跳过时仍参与编译。
func (s *ReasoningStreamSuite) TestReasoningStreamDoc_包引用验证() {
	s.NotNil(llmschema.NewAssistantMessageChunk)
	s.NotNil(harness.CreateDeepAgent)
	s.NotNil(adapter.NewDeepAdapter)
	s.NotNil(stream.OutputSchema{})
	s.NotNil(swarmruntime.BuildUserPrompt)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectReasoningProbe 消费模型流式通道，统计推理/正文输出。
func collectReasoningProbe(ctx context.Context, ch <-chan *llmschema.AssistantMessageChunk) reasoningProbe {
	probe := reasoningProbe{}
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return probe
			}
			if chunk == nil {
				continue
			}
			probe.chunkCount++
			if chunk.ReasoningContent != "" {
				probe.reasoningChunks++
				probe.reasoningText.WriteString(chunk.ReasoningContent)
			}
			if chunk.Content.Text() != "" {
				probe.contentChunks++
			}
		case <-ctx.Done():
			return probe
		}
	}
}

// collectPlanEvents 消费界面事件流，返回事件序列、推理全文与首现下标（-1 表示未出现）。
//
// limit 为事件条数上限：规划模式后续会起 explore/plan 子代理，链路很长，
// 拿到思考与首个正文/工具事件后没有必要继续等。
func collectPlanEvents(
	ch <-chan *swarmschema.AgentResponseChunk, limit int,
) (order []string, reasoningText string, firstDeltaIdx, firstToolCallIdx, toolResultCount int, deltaPreview string) {
	reasoning := strings.Builder{}
	firstDeltaIdx, firstToolCallIdx, toolResultCount = -1, -1, 0
	order = make([]string, 0, 64)

	for chunk := range ch {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		eventType, _ := chunk.Payload["event_type"].(string)
		if eventType == "" {
			continue
		}
		order = append(order, eventType)
		switch eventType {
		case "chat.reasoning":
			if text, ok := chunk.Payload["content"].(string); ok && text != "" {
				reasoning.WriteString(text)
			}
		case "chat.delta", "chat.final":
			if firstDeltaIdx < 0 {
				firstDeltaIdx = len(order) - 1
				if text, ok := chunk.Payload["content"].(string); ok {
					deltaPreview = truncateRunes(text, 80)
				}
			}
		case "chat.tool_call":
			if firstToolCallIdx < 0 {
				firstToolCallIdx = len(order) - 1
			}
		case "chat.tool_result":
			toolResultCount++
		}
		if len(order) >= limit {
			break
		}
		if indexOf(order, "chat.reasoning") >= 0 && (firstDeltaIdx >= 0 || firstToolCallIdx >= 0) {
			break
		}
	}
	return order, reasoning.String(), firstDeltaIdx, firstToolCallIdx, toolResultCount, deltaPreview
}

// indexOf 返回事件类型在序列中首次出现的下标，未出现返回 -1。
func indexOf(order []string, eventType string) int {
	for i, t := range order {
		if t == eventType {
			return i
		}
	}
	return -1
}

// truncateRunes 按 rune 截断字符串（避免 CJK 截断半字符）。
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// collectStreamChunks 消费 session 流通道，收集全部 chunk 直到通道关闭或 ctx 到期。
func collectStreamChunks(ctx context.Context, ch <-chan stream.Schema) []stream.Schema {
	chunks := make([]stream.Schema, 0, 64)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				return chunks
			}
			chunks = append(chunks, c)
		case <-ctx.Done():
			return chunks
		}
	}
}

// summarizeEventOrder 把事件序列压缩成「类型×次数」的可读摘要（保留首次出现顺序）。
func summarizeEventOrder(order []string) []string {
	counts := map[string]int{}
	seq := make([]string, 0, len(order))
	for _, t := range order {
		if counts[t] == 0 {
			seq = append(seq, t)
		}
		counts[t]++
	}
	for i, t := range seq {
		seq[i] = fmt.Sprintf("%s×%d", t, counts[t])
	}
	return seq
}
