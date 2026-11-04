//go:build integration

package adapter_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// CompressionTriggerSuite 验证各上下文处理器「触发判据」的真实链路契约：
//  1. RoundLevelCompressor：started 事件 duration_ms 为 0（旧实现 EndedAt 零值负溢出）、
//     before 只统计窗口内对话消息、context_max 等于配置窗口、completed 产出记忆块；
//  2. DialogueCompressor / CurrentRoundCompressor：add_messages 判据触发；
//  3. ToolResultBudgetProcessor：session_memory 预设下按「轮内工具结果预算」触发。
//
// 对应 Python：RoundLevelCompressor.on_get_context_window /
// ProcessorStateRecorder.build_state / 各处理器 trigger_add_messages。
type CompressionTriggerSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
	// client 记录模型入参并按脚本返回响应的客户端
	client *compressionScriptClient
}

// compressionState 一轮对话中的一个 context.compression_state 事件。
type compressionState struct {
	// status 事件状态（started/completed/...）
	status string
	// phase 压缩阶段
	phase string
	// processor 处理器类型
	processor string
	// contextMax 上下文窗口上限
	contextMax int64
	// durationMs 处理器耗时（毫秒）
	durationMs int64
	// beforeMessages 处理前对话消息条数
	beforeMessages int64
	// beforeTokens 处理前对话消息 token 数
	beforeTokens int64
	// compactSummary 压缩摘要
	compactSummary string
}

// scriptStep 一次模型调用的预设响应（文本或工具调用二选一）。
type scriptStep struct {
	// text 回复正文
	text string
	// toolName 工具调用名（非空时按工具调用返回）
	toolName string
	// toolArgs 工具调用参数（JSON 字符串）
	toolArgs string
}

// compressionScriptClient 按脚本返回响应并记录每次调用的入参。
type compressionScriptClient struct {
	// MockModelClient 复用其余接口方法（GenerateImage/Release 等）
	*mockllm.MockModelClient
	// mu 保护 script / streams
	mu sync.Mutex
	// script 待消费的响应脚本
	script []scriptStep
	// streams 每次 Stream 调用收到的消息参数
	streams []model_clients.MessagesParam
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCompressionTriggerSuite 启动压缩触发契约套件。
func TestCompressionTriggerSuite(t *testing.T) {
	suite.Run(t, new(CompressionTriggerSuite))
}

// SetupTest 每个测试方法前构建隔离环境并替换 provider 工厂。
func (s *CompressionTriggerSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_compression_trigger")
	s.client = &compressionScriptClient{MockModelClient: mockllm.NewMockModelClient()}
	registry := model_clients.GetClientRegistry()
	registry.Unregister(s.env.Provider, "llm")
	registry.Register(s.env.Provider, "llm", s.client.Factory())
}

// TestWindow262_started事件duration与before口径 window=262 时 RoundLevelCompressor 的契约。
func (s *CompressionTriggerSuite) TestWindow262_started事件duration与before口径() {
	s.writeConfig(262, "")
	s.setScript(scriptStep{text: "用户在打招呼"})

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	states := s.runTurn(d, "sess_compress_262", "req-c-1", "你好")
	s.Require().NotEmpty(states, "小窗口下应至少产生 started 事件")

	started := states[0]
	s.Equal("started", started.status, "首个事件应为 started")
	s.Equal("get_context_window", started.phase, "阶段应为 get_context_window")
	s.Equal("RoundLevelCompressor", started.processor, "处理器应为 RoundLevelCompressor")
	s.Equal(int64(0), started.durationMs, "started 事件 duration_ms 必须为 0（不得负溢出）")
	s.Equal(int64(262), started.contextMax, "context_max 应等于配置窗口 262，而不是兜底值")
	s.Equal(int64(1), started.beforeMessages, "before 只统计窗口内对话消息（1 条用户消息）")
	s.Greater(started.beforeTokens, int64(0), "before.tokens 应为正数")

	var completed *compressionState
	for i := range states {
		if states[i].status == "completed" {
			completed = &states[i]
		}
	}
	s.Require().NotNil(completed, "小窗口下压缩应执行完成（completed 事件）")
	s.Contains(completed.compactSummary, "[ROUND_LEVEL_MEMORY_BLOCK]",
		"压缩产物应为轮级记忆块标记")

	// system prompt 未被压缩改写：模型仍收到一条很长的 system 消息
	calls := s.client.calls()
	s.Require().NotEmpty(calls, "模型应至少被调用一次")
	first := calls[0].Messages()
	s.Require().NotEmpty(first, "模型入参不应为空")
	s.Equal("system", first[0].GetRole().String(), "首条消息应为 system prompt")
	s.Greater(len(first[0].GetContent().Text()), 1000, "system prompt 不应被压缩/清空")
}

// TestAddMessages路径_Dialogue与当轮压缩触发 验证 add_messages 判据下两个压缩器都会触发。
func (s *CompressionTriggerSuite) TestAddMessages路径_Dialogue与当轮压缩触发() {
	// 窗口 10 → 比例收口后 Dialogue/CurrentRound 阈值只有几 token，助手消息回填即触发
	s.writeConfig(10, `    dialogue_compressor_config:
      messages_to_keep: 1
    current_round_compressor_config:
      messages_to_keep: 1
`)
	s.setScript(scriptStep{text: "用户在打招呼"})

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	states := s.runTurn(d, "sess_compress_add", "req-c-2", "你好")
	processors := collectProcessors(states)
	s.Contains(processors, "DialogueCompressor", "add_messages 路径应触发 DialogueCompressor")
	s.Contains(processors, "CurrentRoundCompressor", "add_messages 路径应触发 CurrentRoundCompressor")
}

// TestToolResultBudget触发 session_memory 预设下轮内工具结果超预算应触发卸载。
func (s *CompressionTriggerSuite) TestToolResultBudget触发() {
	s.writeConfig(262, `    session_memory: true
    tool_result_budget_processor_config:
      tokens_threshold: 1
      large_message_threshold: 1
`)
	s.setScript(
		scriptStep{toolName: "todo_list", toolArgs: "{}"},
		scriptStep{text: "已查看任务列表"},
	)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	states := s.runTurn(d, "sess_compress_trb", "req-c-3", "看看任务列表")
	processors := collectProcessors(states)
	s.Contains(processors, "ToolResultBudgetProcessor",
		"轮内工具结果超过预算时应触发 ToolResultBudgetProcessor")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setScript 设置模型响应脚本。
func (s *CompressionTriggerSuite) setScript(steps ...scriptStep) {
	s.client.mu.Lock()
	s.client.script = steps
	s.client.mu.Unlock()
}

// writeConfig 写入配置；extraEngine 为 context_engine_config 下追加的缩进行。
func (s *CompressionTriggerSuite) writeConfig(maxInput int, extraEngine string) {
	s.T().Helper()
	yaml := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        max_input_tokens: %d
      is_default: true

react:
  agent_name: main_agent
  context_engine_config:
    enabled: true
    compression_trigger_ratio: 0.85
    compression_target_ratio: 0.55
%s`, s.env.Provider, maxInput, extraEngine)
	s.Require().NoError(os.WriteFile(
		filepath.Join(s.env.DataDir, "config", "config.yaml"), []byte(yaml), 0o644))
}

// runTurn 跑一轮对话并收集 context.compression_state 事件。
func (s *CompressionTriggerSuite) runTurn(
	d *adapter.DeepAdapter,
	sessionID string,
	requestID string,
	query string,
) []compressionState {
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
	s.Require().NoError(streamErr, "流式入口不应返回启动错误")

	var states []compressionState
	for chunk := range chunkCh {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		if et, _ := chunk.Payload["event_type"].(string); et != "context.compression_state" {
			continue
		}
		states = append(states, parseCompressionState(chunk.Payload))
	}
	return states
}

// Stream 覆写流式调用：按脚本返回文本或工具调用，并记录入参。
func (c *compressionScriptClient) Stream(
	_ context.Context,
	messages model_clients.MessagesParam,
	_ ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	c.mu.Lock()
	step := scriptStep{text: "用户在打招呼"}
	if len(c.script) > 0 {
		step = c.script[0]
		c.script = c.script[1:]
	}
	c.streams = append(c.streams, messages)
	c.mu.Unlock()

	chunk := llmschema.NewAssistantMessageChunk(step.text)
	if step.toolName != "" {
		chunk.ToolCalls = []*llmschema.ToolCall{
			llmschema.NewToolCall("call_probe", step.toolName, step.toolArgs),
		}
	}
	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	ch <- chunk
	close(ch)
	return ch, nil
}

// Factory 返回本客户端的注册工厂。
func (c *compressionScriptClient) Factory() model_clients.ClientFactory {
	return func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return c, nil
	}
}

// calls 返回已记录的模型调用副本。
func (c *compressionScriptClient) calls() []model_clients.MessagesParam {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model_clients.MessagesParam, len(c.streams))
	copy(out, c.streams)
	return out
}

// collectProcessors 提取事件中出现过的处理器类型（去重，保持出现顺序）。
func collectProcessors(states []compressionState) []string {
	seen := make(map[string]struct{}, len(states))
	out := make([]string, 0, len(states))
	for _, state := range states {
		if state.processor == "" {
			continue
		}
		if _, ok := seen[state.processor]; ok {
			continue
		}
		seen[state.processor] = struct{}{}
		out = append(out, state.processor)
	}
	return out
}

// parseCompressionState 把 payload 映射为结构体，兼容 JSON 往返后的数值类型。
func parseCompressionState(payload map[string]any) compressionState {
	state := compressionState{
		status:         stringField(payload, "status"),
		phase:          stringField(payload, "phase"),
		processor:      stringField(payload, "processor"),
		contextMax:     intField(payload, "context_max"),
		durationMs:     intField(payload, "duration_ms"),
		compactSummary: stringField(payload, "compact_summary"),
	}
	if before, ok := payload["before"].(map[string]any); ok {
		state.beforeMessages = intField(before, "messages")
		state.beforeTokens = intField(before, "tokens")
	}
	return state
}

// stringField 读取字符串字段。
func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

// intField 读取整数字段，兼容 int/int64/float64 三种形态。
func intField(payload map[string]any, key string) int64 {
	switch v := payload[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
	}
	return 0
}
