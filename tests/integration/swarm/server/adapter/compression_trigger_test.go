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

// CompressionTriggerSuite 验证小窗口（max_input_tokens=262）下的压缩链路契约：
//  1. started 事件 duration_ms 为 0（旧实现 EndedAt 零值负溢出成 -9223372036854）；
//  2. before 只统计窗口内对话消息（不含 system prompt 与工具 schema）；
//  3. context_max 等于配置的窗口值（262），不是兜底 200000；
//  4. completed 事件的 compact_summary 是轮级记忆块标记（压缩确实发生过）。
//
// 对应 Python：RoundLevelCompressor.on_get_context_window / ProcessorStateRecorder.build_state
type CompressionTriggerSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
	// client 记录模型入参的脚本客户端
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

// compressionScriptClient 记录模型入参并返回固定摘要文本的模型客户端。
type compressionScriptClient struct {
	*mockllm.MockModelClient
	// mu 保护 streams
	mu sync.Mutex
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

// Stream 覆写流式调用：记录入参，返回一段会被当作压缩摘要的文本。
func (c *compressionScriptClient) Stream(
	_ context.Context,
	messages model_clients.MessagesParam,
	_ ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	c.mu.Lock()
	c.streams = append(c.streams, messages)
	c.mu.Unlock()

	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	ch <- llmschema.NewAssistantMessageChunk("用户在打招呼")
	close(ch)
	return ch, nil
}

// Factory 返回本客户端的注册工厂。
func (c *compressionScriptClient) Factory() model_clients.ClientFactory {
	return func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return c, nil
	}
}

// TestWindow262_started事件duration与before口径 window=262 时的 started/完成事件契约。
func (s *CompressionTriggerSuite) TestWindow262_started事件duration与before口径() {
	s.writeWindowConfig(262)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.plan", ""))
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeWindowConfig 写入 max_input_tokens 指定的配置。
func (s *CompressionTriggerSuite) writeWindowConfig(maxInput int) {
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
`, s.env.Provider, maxInput)
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

// calls 返回已记录的模型调用副本。
func (c *compressionScriptClient) calls() []model_clients.MessagesParam {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model_clients.MessagesParam, len(c.streams))
	copy(out, c.streams)
	return out
}
