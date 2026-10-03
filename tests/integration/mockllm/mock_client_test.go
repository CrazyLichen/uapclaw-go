//go:build integration

package mockllm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MockClientSuite MockModelClient 集成测试套件
// 注意：mockllm 是 suite 包的上游依赖，不可导入 suite 包（循环导入），
// 因此直接内嵌 suite.Suite 而非 BaseIntegrationSuite
type MockClientSuite struct {
	suite.Suite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMockClientSuite 运行 MockModelClient 集成测试套件
func TestMockClientSuite(t *testing.T) {
	suite.Run(t, new(MockClientSuite))
}

// TestNewMockModelClient_默认响应 测试无预设响应时返回默认文本
// 对齐 Python: MockLLMModel()._get_next_response() → "Default mock response"
func (s *MockClientSuite) TestNewMockModelClient_默认响应() {
	client := NewMockModelClient()
	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), defaultMockResponse, msg.Content.String())
}

// TestMockModelClient_AddTextResponse_文本响应 测试纯文本响应
// 对齐 Python: create_text_response("你好世界")
func (s *MockClientSuite) TestMockModelClient_AddTextResponse_文本响应() {
	client := NewMockModelClient()
	client.AddTextResponse("你好世界")
	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hi"))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "你好世界", msg.Content.String())
}

// TestMockModelClient_响应队列依次消费 测试多轮响应队列
// 对齐 Python: mock_llm.set_responses([text1, text2, text3]) 依次消费
func (s *MockClientSuite) TestMockModelClient_响应队列依次消费() {
	client := NewMockModelClient()
	client.AddTextResponse("第一轮")
	client.AddTextResponse("第二轮")
	client.AddTextResponse("第三轮")

	msg1, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q1"))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "第一轮", msg1.Content.String())

	msg2, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q2"))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "第二轮", msg2.Content.String())

	msg3, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q3"))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "第三轮", msg3.Content.String())

	// 队列耗尽后返回默认响应
	msg4, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q4"))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), defaultMockResponse, msg4.Content.String())
}

// TestMockModelClient_AddToolCallResponse 测试工具调用响应
// 对齐 Python: create_tool_call_response("read_file", '{"path": "/tmp/test.txt"}')
func (s *MockClientSuite) TestMockModelClient_AddToolCallResponse() {
	client := NewMockModelClient()
	client.AddToolCallResponse("read_file", `{"path": "/tmp/test.txt"}`)
	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("读取文件"))
	require.NoError(s.T(), err)
	require.Len(s.T(), msg.ToolCalls, 1)
	assert.Equal(s.T(), "read_file", msg.ToolCalls[0].Name)
	assert.Equal(s.T(), `{"path": "/tmp/test.txt"}`, msg.ToolCalls[0].Arguments)
	assert.Equal(s.T(), "mock_call_read_file", msg.ToolCalls[0].ID)
	assert.Equal(s.T(), "tool_calls", msg.FinishReason)
}

// TestMockModelClient_AddErrorResponse 测试错误响应
func (s *MockClientSuite) TestMockModelClient_AddErrorResponse() {
	client := NewMockModelClient()
	client.AddErrorResponse(context.DeadlineExceeded)
	_, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))
	assert.ErrorIs(s.T(), err, context.DeadlineExceeded)
}

// TestMockModelClient_InvokeCallCount 测试调用次数记录
// 对齐 Python: MockLLMModel.call_count
func (s *MockClientSuite) TestMockModelClient_InvokeCallCount() {
	client := NewMockModelClient()
	client.AddTextResponse("a")
	client.AddTextResponse("b")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("1"))
	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("2"))

	assert.Equal(s.T(), 2, client.InvokeCallCount())
}

// TestMockModelClient_GetInvokeCall 测试调用记录获取
// 对齐 Python: MockLLMModel.call_history[i]
func (s *MockClientSuite) TestMockModelClient_GetInvokeCall() {
	client := NewMockModelClient()
	client.AddTextResponse("a")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))

	call := client.GetInvokeCall(0)
	assert.NotNil(s.T(), call)
	assert.Equal(s.T(), 1, call.InvokeCount)

	// 越界返回 nil
	assert.Nil(s.T(), client.GetInvokeCall(1))
}

// TestMockModelClient_LastInvokeCall 测试最近调用记录
func (s *MockClientSuite) TestMockModelClient_LastInvokeCall() {
	client := NewMockModelClient()
	client.AddTextResponse("a")
	client.AddTextResponse("b")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("first"))
	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("second"))

	last := client.LastInvokeCall()
	assert.Equal(s.T(), 2, last.InvokeCount)
}

// TestMockModelClient_ResetCalls 测试重置调用历史
func (s *MockClientSuite) TestMockModelClient_ResetCalls() {
	client := NewMockModelClient()
	client.AddTextResponse("a")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))
	assert.Equal(s.T(), 1, client.InvokeCallCount())

	client.ResetCalls()
	assert.Equal(s.T(), 0, client.InvokeCallCount())
}

// TestMockModelClient_Stream 测试流式响应
// 对齐 Python: MockLLMModel.stream() — 单次 yield 整个 chunk
func (s *MockClientSuite) TestMockModelClient_Stream() {
	client := NewMockModelClient()
	client.AddTextResponse("流式响应")

	ch, err := client.Stream(context.Background(), model_clients.NewTextMessagesParam("hello"))
	require.NoError(s.T(), err)

	chunk := <-ch
	assert.NotNil(s.T(), chunk)
	assert.Equal(s.T(), "流式响应", chunk.Content.String())
	assert.Equal(s.T(), defaultMockModelName, chunk.UsageMetadata.ModelName)
}

// TestMockModelClient_Stream_ToolCall 测试流式工具调用响应
func (s *MockClientSuite) TestMockModelClient_Stream_ToolCall() {
	client := NewMockModelClient()
	client.AddToolCallResponse("search", `{"query": "test"}`)

	ch, err := client.Stream(context.Background(), model_clients.NewTextMessagesParam("搜索"))
	require.NoError(s.T(), err)

	chunk := <-ch
	assert.NotNil(s.T(), chunk)
	require.Len(s.T(), chunk.ToolCalls, 1)
	assert.Equal(s.T(), "search", chunk.ToolCalls[0].Name)
}

// TestMockModelClient_MixedResponseSequence 测试混合响应序列
// 对齐 Python: system_tests 中常见的 tool_call → text 模式
func (s *MockClientSuite) TestMockModelClient_MixedResponseSequence() {
	client := NewMockModelClient()
	// 第一次：工具调用
	client.AddToolCallResponse("write_file", `{"path": "a.txt", "content": "hello"}`)
	// 第二次：文本响应
	client.AddTextResponse("文件已写入")

	msg1, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("写入文件"))
	require.NoError(s.T(), err)
	require.Len(s.T(), msg1.ToolCalls, 1)
	assert.Equal(s.T(), "write_file", msg1.ToolCalls[0].Name)

	msg2, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("结果"))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "文件已写入", msg2.Content.String())
	assert.Empty(s.T(), msg2.ToolCalls)
}

// TestMockModelClient_CreateTextResponse 测试 CreateTextResponse 辅助函数
// 对齐 Python: create_text_response(content)
func (s *MockClientSuite) TestMockModelClient_CreateTextResponse() {
	resp := CreateTextResponse("测试文本")
	assert.Equal(s.T(), "测试文本", resp.Text)
	assert.Empty(s.T(), resp.ToolCalls)
	assert.Nil(s.T(), resp.Err)
}

// TestMockModelClient_CreateToolCallResponse 测试 CreateToolCallResponse 辅助函数
// 对齐 Python: create_tool_call_response(tool_name, arguments)
func (s *MockClientSuite) TestMockModelClient_CreateToolCallResponse() {
	resp := CreateToolCallResponse("my_tool", `{"key": "value"}`)
	assert.Empty(s.T(), resp.Text)
	require.Len(s.T(), resp.ToolCalls, 1)
	assert.Equal(s.T(), "my_tool", resp.ToolCalls[0].Name)
	assert.Equal(s.T(), `{"key": "value"}`, resp.ToolCalls[0].Arguments)
	assert.Equal(s.T(), "mock_call_my_tool", resp.ToolCalls[0].ID)
}

// TestMockModelClient_CreateJSONResponse 测试 CreateJSONResponse 辅助函数
// 对齐 Python: create_json_response(data)
func (s *MockClientSuite) TestMockModelClient_CreateJSONResponse() {
	data := map[string]any{"name": "test", "count": float64(42)}
	resp := CreateJSONResponse(data)
	assert.Contains(s.T(), resp.Text, `"name"`)
	assert.Contains(s.T(), resp.Text, `"test"`)
	assert.Contains(s.T(), resp.Text, `"count"`)
	assert.Nil(s.T(), resp.Err)
}

// TestMockModelClient_AddJSONResponse 测试 AddJSONResponse 链式追加
func (s *MockClientSuite) TestMockModelClient_AddJSONResponse() {
	client := NewMockModelClient()
	client.AddJSONResponse(map[string]any{"status": "ok"})

	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("query"))
	require.NoError(s.T(), err)
	assert.Contains(s.T(), msg.Content.String(), `"status"`)
	assert.Contains(s.T(), msg.Content.String(), `"ok"`)
}

// TestMockModelClient_RegisterToClientRegistry 测试 ClientRegistry 注册
// 对齐 Python: mock_llm_context() 通过 patch 注入 MockLLM
func (s *MockClientSuite) TestMockModelClient_RegisterToClientRegistry() {
	client := NewMockModelClient()
	client.AddTextResponse("注册测试")

	registry := model_clients.NewClientRegistry()
	registry.Register("mock_itest", "llm", client.Factory())

	got, err := registry.GetClient("mock_itest", "llm",
		&llmschema.ModelRequestConfig{ModelName: "mock-model"},
		&llmschema.ModelClientConfig{ClientID: "mock_itest_id"},
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), got)
}

// TestMockModelClient_ConcurrentInvoke 测试并发安全
func (s *MockClientSuite) TestMockModelClient_ConcurrentInvoke() {
	client := NewMockModelClient()
	for i := 0; i < 10; i++ {
		client.AddTextResponse("ok")
	}

	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("concurrent"))
			errCh <- err
		}()
	}

	for i := 0; i < 10; i++ {
		assert.NoError(s.T(), <-errCh)
	}
	assert.Equal(s.T(), 10, client.InvokeCallCount())
}

// TestMockModelClient_SetResponses_重置语义 测试 SetResponses 的重置语义
// 对齐 Python: MockLLMModel.set_responses() 同时重置 call_count 和 call_history
func (s *MockClientSuite) TestMockModelClient_SetResponses_重置语义() {
	client := NewMockModelClient()
	client.AddTextResponse("旧响应")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q"))
	assert.Equal(s.T(), 1, client.InvokeCallCount())

	// SetResponses 重置队列和调用历史
	client.SetResponses(MockResponse{Text: "新响应"})
	assert.Equal(s.T(), 0, client.InvokeCallCount())

	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q"))
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "新响应", msg.Content.String())
}

// TestMockModelClient_UsageMetadata 测试响应携带 UsageMetadata
// 对齐 Python: UsageMetadata(model_name="mock-model")
func (s *MockClientSuite) TestMockModelClient_UsageMetadata() {
	client := NewMockModelClient()
	client.AddTextResponse("hello")

	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hi"))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), msg.UsageMetadata)
	assert.Equal(s.T(), defaultMockModelName, msg.UsageMetadata.ModelName)
}

// TestMockClientDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *MockClientSuite) TestMockClientDoc_包引用验证() {
	// 验证 llm schema 和 runner 包可正常导入
	s.NotNil(llmschema.ModelRequestConfig{})
	s.NotNil(runner.GetResourceMgr)
}
