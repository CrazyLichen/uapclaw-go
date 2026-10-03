//go:build integration

package mockllm_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// MockLLME2ESuite 验证 MockLLM + ClientRegistry + Invoke 完整链路
type MockLLME2ESuite struct {
	isuite.BaseIntegrationSuite
}

func TestMockLLME2ESuite(t *testing.T) {
	suite.Run(t, new(MockLLME2ESuite))
}

// TestInvoke_响应队列依次消费 测试 MockLLM 响应队列按序消费
// 对齐 Python: MockLLMModel.invoke() 按序消费 responses
func (s *MockLLME2ESuite) TestInvoke_响应队列依次消费() {
	// 每个测试方法独立设置响应，避免 Suite 共享状态干扰
	// 对齐 Python: mock_llm.set_responses([create_tool_call_response(...), create_text_response(...)])
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/test.txt"}`),
		mockllm.CreateTextResponse("文件内容是 hello world"),
	)

	msg, err := s.MockLLM.Invoke(s.Ctx, model_clients.NewTextMessagesParam("读取文件"))
	s.Require().NoError(err)
	s.Require().Len(msg.ToolCalls, 1)
	s.Equal("read_file", msg.ToolCalls[0].Name)

	msg2, err := s.MockLLM.Invoke(s.Ctx, model_clients.NewTextMessagesParam("结果如何"))
	s.Require().NoError(err)
	s.Equal("文件内容是 hello world", msg2.Content.String())
}

// TestInvokeCallCount_记录调用次数 测试 MockLLM 调用次数记录
// 对齐 Python: MockLLMModel.call_count
func (s *MockLLME2ESuite) TestInvokeCallCount_记录调用次数() {
	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("a"),
		mockllm.CreateTextResponse("b"),
	)

	_, _ = s.MockLLM.Invoke(s.Ctx, model_clients.NewTextMessagesParam("q1"))
	_, _ = s.MockLLM.Invoke(s.Ctx, model_clients.NewTextMessagesParam("q2"))
	s.Equal(2, s.MockLLM.InvokeCallCount())
}

// TestClientRegistry_注册和获取 测试通过 ClientRegistry 注册和获取 MockLLM
// 对齐 Python: mock_llm_context() 中通过 patch 注入 MockLLM
func (s *MockLLME2ESuite) TestClientRegistry_注册和获取() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("hello"))

	registry := model_clients.NewClientRegistry()
	registry.Register("e2e_mock", "llm", s.MockLLM.Factory())

	got, err := registry.GetClient("e2e_mock", "llm",
		&llmschema.ModelRequestConfig{ModelName: "mock"},
		&llmschema.ModelClientConfig{ClientID: "e2e_mock_id"},
	)
	s.Require().NoError(err)
	s.Require().NotNil(got)

	// 通过 Registry 获取的客户端调用
	msg, err := got.Invoke(s.Ctx, model_clients.NewTextMessagesParam("hello"))
	s.Require().NoError(err)
	s.NotNil(msg)
}

// TestCreateHelpers_辅助函数 测试 CreateTextResponse/CreateToolCallResponse/CreateJSONResponse
// 对齐 Python: create_text_response / create_tool_call_response / create_json_response
func (s *MockLLME2ESuite) TestCreateHelpers_辅助函数() {
	// CreateTextResponse
	resp := mockllm.CreateTextResponse("测试文本")
	s.Equal("测试文本", resp.Text)
	s.Empty(resp.ToolCalls)
	s.Nil(resp.Err)

	// CreateToolCallResponse
	resp2 := mockllm.CreateToolCallResponse("my_tool", `{"key": "value"}`)
	s.Empty(resp2.Text)
	s.Len(resp2.ToolCalls, 1)
	s.Equal("my_tool", resp2.ToolCalls[0].Name)

	// CreateJSONResponse
	resp3 := mockllm.CreateJSONResponse(map[string]any{"status": "ok"})
	s.Contains(resp3.Text, `"status"`)
	s.Contains(resp3.Text, `"ok"`)
}

// TestStream_流式响应 测试 MockLLM 流式响应
// 对齐 Python: MockLLMModel.stream() — 单次 yield 整个 chunk
func (s *MockLLME2ESuite) TestStream_流式响应() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("流式测试"))

	ch, err := s.MockLLM.Stream(s.Ctx, model_clients.NewTextMessagesParam("hello"))
	s.Require().NoError(err)

	chunk := <-ch
	s.NotNil(chunk)
	s.Equal("流式测试", chunk.Content.String())
}
