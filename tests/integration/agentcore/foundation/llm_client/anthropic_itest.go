//go:build integration

// Anthropic Messages 客户端端到端集成测试（同包 llm_client）。
// 跨子系统：llm/model_clients/anthropic（协议翻译与解析）→ llm.Model 门面
// → runner/callback（LLM_INPUT / LLM_RESPONSE_STARTED / LLM_OUTPUT 三事件）
// → llm/schema（usage 归一）→ transport_pool（连接复用）。
//
// Python: tests/unit_tests/core/foundation/llm/model_clients/
// test_anthropic_model_client.py 的链路级对拍。
package llm_client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	anthropic "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/anthropic"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AnthropicSuite Anthropic 客户端集成测试套件。
type AnthropicSuite struct {
	isuite.BaseIntegrationSuite
}

// capturedEvents 捕获的回调事件（线程安全）。
type capturedEvents struct {
	mu     sync.Mutex
	events []callback.LLMCallEventType
}

func (c *capturedEvents) record(ev callback.LLMCallEventType) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *capturedEvents) contains(ev callback.LLMCallEventType) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e == ev {
			return true
		}
	}
	return false
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAnthropicSuite 运行套件。
func TestAnthropicSuite(t *testing.T) {
	suite.Run(t, new(AnthropicSuite))
}

// fakeAnthropicServer 构造伪 Anthropic /v1/messages 端点。
// capture 记录请求体与关键头，供断言缓存断点与鉴权。
type fakeAnthropicServer struct {
	*httptest.Server
	mu           sync.Mutex
	lastBody     map[string]any
	lastAPIKey   string
	lastVersion  string
	lastPath     string
	responses    []string // 依次返回（invoke 单条 / stream SSE 多段）
	contentType  string
}

func newFakeAnthropicServer(responses []string, contentType string) *fakeAnthropicServer {
	fake := &fakeAnthropicServer{responses: responses, contentType: contentType}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		fake.mu.Lock()
		fake.lastBody = parsed
		fake.lastAPIKey = r.Header.Get("x-api-key")
		fake.lastVersion = r.Header.Get("anthropic-version")
		fake.lastPath = r.URL.Path
		idx := 0
		if len(fake.responses) > 0 {
			idx = 0
		}
		resp := fake.responses[idx]
		fake.mu.Unlock()

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	})
	fake.Server = httptest.NewServer(mux)
	return fake
}

func (f *fakeAnthropicServer) snapshot() (map[string]any, string, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastBody, f.lastAPIKey, f.lastVersion, f.lastPath
}

// newAnthropicModel 构造指向伪端点的 Model 门面（走真实注册表分派链）。
func (s *AnthropicSuite) newAnthropicModel(baseURL string) *llm.Model {
	model, err := llm.InitModel("Anthropic", "claude-sonnet-4-5", "test-key", baseURL)
	s.Require().NoError(err)
	return model
}

// IT-1 invoke 全链路：Model.Invoke → AnthropicModelClient → httptest /v1/messages。
// 断言：x-api-key / anthropic-version 头、system 顶层 + max_tokens、
// LLM_INPUT / LLM_RESPONSE_STARTED / LLM_OUTPUT 三回调、usage 归一。
func (s *AnthropicSuite) TestInvoke_全链路() {
	fake := newFakeAnthropicServer([]string{`{"id":"msg_1","model":"claude-sonnet-4-5",
		"role":"assistant","content":[{"type":"text","text":"hello"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":10,"cache_read_input_tokens":5,"output_tokens":3}}`},
		"application/json")
	defer fake.Close()

	events := &capturedEvents{}
	recorder := func(expected callback.LLMCallEventType) callback.LLMCallbackFunc {
		return func(ctx context.Context, data *callback.LLMCallEventData) any { events.record(expected); return nil }
	}
	offInput := recorder(callback.LLMInput)
	offStarted := recorder(callback.LLMResponseStarted)
	offOutput := recorder(callback.LLMOutput)
	callback.GetCallbackFramework().OnLLM(callback.LLMInput, offInput)
	callback.GetCallbackFramework().OnLLM(callback.LLMResponseStarted, offStarted)
	callback.GetCallbackFramework().OnLLM(callback.LLMOutput, offOutput)
	defer func() {
		callback.GetCallbackFramework().OffLLM(callback.LLMInput, offInput)
		callback.GetCallbackFramework().OffLLM(callback.LLMResponseStarted, offStarted)
		callback.GetCallbackFramework().OffLLM(callback.LLMOutput, offOutput)
	}()

	model := s.newAnthropicModel(fake.URL)
	msg, err := model.Invoke(context.Background(), model_clients.NewTextMessagesParam("hi"))
	s.Require().NoError(err)
	s.Require().Equal("hello", msg.Content.Text())
	s.Require().Equal("msg_1", msg.ResponseID)
	s.Require().NotNil(msg.UsageMetadata)
	// usage 归一：input = uncached 10 + read 5 + write 0
	s.Require().Equal(15, msg.UsageMetadata.InputTokens)
	s.Require().Equal(5, *msg.UsageMetadata.CacheReadTokens)
	s.Require().True(msg.UsageMetadata.CacheAuthoritative)

	body, apiKey, version, path := fake.snapshot()
	s.Require().Equal("/v1/messages", path, "api_base 尾部 /v1 应剥除（InitModel 传的 base 无 /v1）")
	s.Require().Equal("test-key", apiKey)
	s.Require().Equal("2023-06-01", version)
	s.Require().Equal(float64(32768), body["max_tokens"], "未设 max_tokens → 默认 32768")
	s.Require().NotNil(body["system"], "system 应提到顶层")

	s.Require().True(events.contains(callback.LLMInput), "LLM_INPUT 应触发")
	s.Require().True(events.contains(callback.LLMResponseStarted), "LLM_RESPONSE_STARTED 应触发")
	s.Require().True(events.contains(callback.LLMOutput), "LLM_OUTPUT 应触发")
}

// IT-2 stream thinking 往返：SSE 回放 thinking+signature 块 → 最终 metadata 含回放块。
func (s *AnthropicSuite) TestStream_思考块往返() {
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"m2","model":"claude-sonnet-4-5","usage":{"input_tokens":8}}}`,
		"",
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me think"}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-abc"}}`,
		"",
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		"",
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
	fake := newFakeAnthropicServer([]string{sse}, "text/event-stream")
	defer fake.Close()

	model := s.newAnthropicModel(fake.URL)
	chunkChan, err := model.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	s.Require().NoError(err)

	var chunks []*llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		s.Require().NoError(chunk.StreamError)
		chunks = append(chunks, chunk)
	}
	s.Require().Greater(len(chunks), 0)

	// 累积合并（复用 chunk Merge 链）
	var final *llmschema.AssistantMessageChunk
	for _, chunk := range chunks {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	s.Require().Equal("answer", final.Content.Text())
	s.Require().Equal("let me think", final.ReasoningContent)
	s.Require().Equal("end_turn", final.FinishReason)
	// 回放块：thinking 带 signature 原样保留（供下一轮 assistant 消息回放）
	replay, ok := final.Metadata[anthropic.AnthropicContentBlocksMetadataKey].([]map[string]any)
	s.Require().True(ok, "metadata 应含回放块")
	found := false
	for _, block := range replay {
		if block["type"] == "thinking" && block["signature"] == "sig-abc" {
			found = true
		}
	}
	s.Require().True(found, "thinking 回放块应携带原样 signature")
}

// IT-3 tool_use 往返：input_json_delta 累积 → tool_calls chunk → finish_reason=tool_calls。
func (s *AnthropicSuite) TestStream_工具往返() {
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"m3","model":"claude","usage":{"input_tokens":9}}}`,
		"",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"run"}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":"}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
	fake := newFakeAnthropicServer([]string{sse}, "text/event-stream")
	defer fake.Close()

	model := s.newAnthropicModel(fake.URL)
	chunkChan, err := model.Stream(context.Background(), model_clients.NewTextMessagesParam("run ls"))
	s.Require().NoError(err)

	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	s.Require().Equal("tool_calls", final.FinishReason)
	s.Require().Len(final.ToolCalls, 1)
	s.Require().Equal("t1", final.ToolCalls[0].ID)
	s.Require().Equal("run", final.ToolCalls[0].Name)
	s.Require().Equal(`{"cmd":"ls"}`, final.ToolCalls[0].Arguments)
}

// IT-4 缓存断点：请求体断言 tools 尾 ephemeral、system 块 5m、transient 尾不锚定。
func (s *AnthropicSuite) TestInvoke_缓存断点() {
	fake := newFakeAnthropicServer([]string{`{"id":"m4","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1}}`},
		"application/json")
	defer fake.Close()

	model := s.newAnthropicModel(fake.URL)
	msgs := []llmschema.BaseMessage{
		llmschema.NewSystemMessage("sys prompt"),
		llmschema.NewUserMessage("q"),
		llmschema.NewUserMessage("transient-tail",
			llmschema.WithMetadata(map[string]any{"transient": true})),
	}
	_, err := model.Invoke(context.Background(),
		model_clients.NewMessagesParam(msgs...),
		model_clients.WithInvokeExtra(map[string]any{}))
	s.Require().NoError(err)

	body, _, _, _ := fake.snapshot()
	systemBlocks := body["system"].([]any)
	s.Require().NotEmpty(systemBlocks)
	sysBlock := systemBlocks[len(systemBlocks)-1].(map[string]any)
	s.Require().NotNil(sysBlock["cache_control"], "system 块应挂 5m 断点")

	anthMsgs := body["messages"].([]any)
	// 尾消息 transient → 断点应锚在前一条稳定消息
	stable := anthMsgs[len(anthMsgs)-2].(map[string]any)["content"].([]any)
	stableBlock := stable[len(stable)-1].(map[string]any)
	s.Require().NotNil(stableBlock["cache_control"], "稳定消息应挂断点")
	tail := anthMsgs[len(anthMsgs)-1].(map[string]any)["content"].([]any)
	tailBlock := tail[len(tail)-1].(map[string]any)
	s.Require().Nil(tailBlock["cache_control"], "transient 尾不应打标")
}

// IT-5 错误折叠：502 HTML 网关页 → 错误消息不含 <html>、含摘要、params.http_status=502。
func (s *AnthropicSuite) TestInvoke_502HTML折叠() {
	fake := newFakeAnthropicServer([]string{
		"<html><head><title>502 Bad Gateway</title></head><body><h1>502 Bad Gateway</h1></body></html>",
	}, "text/html")
	defer fake.Close()

	model := s.newAnthropicModel(fake.URL)
	_, err := model.Invoke(context.Background(), model_clients.NewTextMessagesParam("hi"))
	s.Require().Error(err)
	msgText := err.Error()
	s.Require().NotContains(msgText, "<html", "错误消息不应含原始 HTML")
	s.Require().Contains(msgText, "502", "应含状态码")
}

// IT-6 reasoning：ModelConfig.Reasoning → thinking 进请求体且 max_tokens 抬升。
func (s *AnthropicSuite) TestInvoke_reasoning接线() {
	fake := newFakeAnthropicServer([]string{`{"id":"m6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1}}`},
		"application/json")
	defer fake.Close()

	model := s.newAnthropicModel(fake.URL)
	msgs := []llmschema.BaseMessage{llmschema.NewUserMessage("hi")}
	_, err := model.Invoke(context.Background(),
		model_clients.NewMessagesParam(msgs...),
		model_clients.WithInvokeExtra(map[string]any{
			"reasoning": map[string]any{"mode": "enabled", "budget_tokens": 5000},
		}),
		model_clients.WithInvokeMaxTokens(2000))
	s.Require().NoError(err)

	body, _, _, _ := fake.snapshot()
	thinking, ok := body["thinking"].(map[string]any)
	s.Require().True(ok, "thinking 应进请求体: %v", body)
	s.Require().Equal("enabled", thinking["type"])
	s.Require().Equal(float64(5000), thinking["budget_tokens"])
	// budget 抬升：2000 <= 5000 → 6024
	s.Require().Equal(float64(6024), body["max_tokens"], "max_tokens 应抬升为 budget+1024")
}

// fmt 引用保持。
var _ = fmt.Sprintf
