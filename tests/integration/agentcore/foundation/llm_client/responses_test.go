//go:build integration

// Responses API 两轮历史回放端到端集成测试（同包 llm_client）。
// 跨子系统：llm/model_clients/openai（Responses 分流）→ llm/utils/responses
// （SSE transport + StreamParser + usage 归一）→ llm/schema（ResponseItemID 回放）
// → runner/callback（LLM_INPUT / LLM_OUTPUT 事件）。
//
// Python: openai_model_client.py responses 分支 + responses_utils.py convert_messages
// 的链路级对拍（第一轮 function_call → 第二轮重建 function_call item）。
package llm_client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResponsesSuite Responses API 集成测试套件。
type ResponsesSuite struct {
	isuite.BaseIntegrationSuite
}

// sseBody 逐事件输出 Responses SSE 的处理器。
func sseBody(events ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "data: [DONE]")
		fmt.Fprintln(w)
	}
}

// capturedResponsesServer 记录多轮请求体并按序返回 SSE。
type capturedResponsesServer struct {
	*httptest.Server
	mu       chan struct{}
	bodies   []map[string]any
	replies  [][]string
	auths    []string
	requestN int
}

func newCapturedResponsesServer(replies ...[]string) *capturedResponsesServer {
	fake := &capturedResponsesServer{mu: make(chan struct{}, 1), replies: replies}
	fake.mu <- struct{}{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		<-fake.mu
		fake.bodies = append(fake.bodies, body)
		fake.auths = append(fake.auths, r.Header.Get("Authorization"))
		idx := fake.requestN
		fake.requestN++
		events := fake.replies[idx]
		fake.mu <- struct{}{}

		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "data: [DONE]")
		fmt.Fprintln(w)
	})
	fake.Server = httptest.NewServer(mux)
	return fake
}

func (f *capturedResponsesServer) snapshot() ([]map[string]any, []string) {
	<-f.mu
	defer func() { f.mu <- struct{}{} }()
	return f.bodies, f.auths
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestResponsesSuite 运行套件。
func TestResponsesSuite(t *testing.T) {
	suite.Run(t, new(ResponsesSuite))
}

// newResponsesModel 构造 api_mode=responses 的 Model 门面（走真实注册表分派链）。
func (s *ResponsesSuite) newResponsesModel(baseURL string) *llm.Model {
	clientConfig, err := llmschema.NewModelClientConfig("OpenAI", "test-key", baseURL+"/v1")
	s.Require().NoError(err)
	clientConfig.APIMode = llmschema.APIModeResponses
	requestConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-5"))
	model, err := llm.NewModel(clientConfig, requestConfig)
	s.Require().NoError(err)
	return model
}

// IT-R1 两轮历史回放：第一轮 function_call（含 item id）→ 第二轮重建 function_call item。
func (s *ResponsesSuite) TestResponsesAPI_两轮历史回放() {
	fake := newCapturedResponsesServer(
		// 第一轮：function_call 交付（added 起帧 + args delta + done 补差）+ 终帧
		[]string{
			`data: {"type":"response.output_item.added","item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"read_file","arguments":"{\"a\":"}}`,
			`data: {"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"1}"}`,
			`data: {"type":"response.output_item.done","item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"read_file","arguments":"{\"a\":1}"}}`,
			`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5","usage":{"input_tokens":10,"output_tokens":8,"output_tokens_details":{"reasoning_tokens":5}}}}`,
		},
		// 第二轮：纯文本回复
		[]string{
			`data: {"type":"response.output_text.delta","delta":"done"}`,
			`data: {"type":"response.completed","response":{"id":"resp_2","model":"gpt-5","usage":{"input_tokens":20,"output_tokens":2}}}`,
		},
	)
	defer fake.Close()

	model := s.newResponsesModel(fake.URL)

	// 第一轮：invoke → AssistantMessage（含 ResponseItemID）
	msgs := []llmschema.BaseMessage{
		llmschema.NewSystemMessage("use tools"),
		llmschema.NewUserMessage("read a"),
	}
	first, err := model.Invoke(context.Background(), model_clients.NewMessagesParam(msgs...))
	s.Require().NoError(err)
	s.Require().Len(first.ToolCalls, 1)
	s.Require().Equal("call_1", first.ToolCalls[0].ID)
	s.Require().Equal("item_1", first.ToolCalls[0].ResponseItemID, "ResponseItemID 应保留")
	s.Require().Equal(`{"a":1}`, first.ToolCalls[0].Arguments, "added+delta 拼接即完整参数")
	s.Require().Equal("tool_calls", first.FinishReason)
	s.Require().NotNil(first.UsageMetadata)
	s.Require().Equal(5, *first.UsageMetadata.ReasoningTokens, "reasoning_tokens 归一")
	s.Require().Equal("resp_1", first.ResponseID)

	// 第二轮：第一轮 assistant + 工具结果 → 断言请求体重建 Responses 语义
	secondMsgs := append(msgs, first,
		llmschema.NewToolMessage("call_1", "done reading"))
	second, err := model.Invoke(context.Background(), model_clients.NewMessagesParam(secondMsgs...))
	s.Require().NoError(err)
	s.Require().Equal("done", second.Content.Text())
	s.Require().Equal("resp_2", second.ResponseID)

	bodies, auths := fake.snapshot()
	s.Require().Len(bodies, 2)
	s.Require().Equal("Bearer test-key", auths[0])
	s.Require().Equal("Bearer test-key", auths[1])

	// 第一轮 body：instructions + store=false；
	// include 仅在调用方显式传 include_reasoning_encrypted_content=true 时携带
	//（Python: kwargs.pop("include_reasoning_encrypted_content", False)）
	s.Require().Equal("use tools", bodies[0]["instructions"])
	s.Require().Equal(false, bodies[0]["store"])
	s.Require().Nil(bodies[0]["include"], "默认不携带 include")

	// 第二轮 body：input 含 function_call（id=response_item_id）与 function_call_output
	input, ok := bodies[1]["input"].([]any)
	s.Require().True(ok, "input 应为数组: %v", bodies[1]["input"])
	var functionCall, functionCallOutput map[string]any
	for _, item := range input {
		m, _ := item.(map[string]any)
		switch m["type"] {
		case "function_call":
			functionCall = m
		case "function_call_output":
			functionCallOutput = m
		}
	}
	s.Require().NotNil(functionCall, "应有 function_call item: %v", input)
	s.Require().Equal("item_1", functionCall["id"], "id 应取 response_item_id")
	s.Require().Equal("call_1", functionCall["call_id"])
	s.Require().Equal("read_file", functionCall["name"])
	s.Require().NotNil(functionCallOutput, "应有 function_call_output item")
	s.Require().Equal("call_1", functionCallOutput["call_id"])
	s.Require().Equal("done reading", functionCallOutput["output"])
}

// IT-R2 流式：增量帧序 + usage 终帧 + LLM_OUTPUT 回调。
func (s *ResponsesSuite) TestResponsesAPI_流式() {
	var lastUsage *llmschema.UsageMetadata
	sawOutput := false
	outputFn := func(ctx context.Context, data *callback.LLMCallEventData) any {
		sawOutput = true
		if data.IsStream {
			lastUsage = data.Usage
		}
		return nil
	}
	callback.GetCallbackFramework().OnLLM(callback.LLMOutput, outputFn)
	defer callback.GetCallbackFramework().OffLLM(callback.LLMOutput, outputFn)

	fake := newCapturedResponsesServer([]string{
		`data: {"type":"response.output_text.delta","delta":"he"}`,
		`data: {"type":"response.output_text.delta","delta":"y"}`,
		`data: {"type":"response.completed","response":{"id":"r9","model":"gpt-5","usage":{"input_tokens":3,"output_tokens":4}}}`,
	})
	defer fake.Close()

	model := s.newResponsesModel(fake.URL)
	ch, err := model.Stream(context.Background(), model_clients.NewMessagesParam(
		llmschema.NewUserMessage("hi")))
	s.Require().NoError(err)
	var text strings.Builder
	sawDelta := false
	for chunk := range ch {
		if chunk.StreamError != nil {
			s.FailNow("流内错误: %v", chunk.StreamError)
		}
		if chunk.Content.Text() != "" {
			sawDelta = true
			text.WriteString(chunk.Content.Text())
		}
		if chunk.UsageMetadata != nil {
			lastUsage = chunk.UsageMetadata
		}
	}
	s.Require().True(sawDelta, "应收到内容增量帧")
	s.Require().Equal("hey", text.String())
	s.Require().True(sawOutput, "LLM_OUTPUT 应触发")
	s.Require().NotNil(lastUsage)
	s.Require().Equal(7, lastUsage.TotalTokens)
}
