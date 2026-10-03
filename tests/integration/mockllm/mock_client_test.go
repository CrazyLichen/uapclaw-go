//go:build integration

package mockllm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
)

// TestNewMockModelClient_默认响应 测试无预设响应时返回默认文本
// 对齐 Python: MockLLMModel()._get_next_response() → "Default mock response"
func TestNewMockModelClient_默认响应(t *testing.T) {
	client := NewMockModelClient()
	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))
	require.NoError(t, err)
	assert.Equal(t, defaultMockResponse, msg.Content.String())
}

// TestMockModelClient_AddTextResponse_文本响应 测试纯文本响应
// 对齐 Python: create_text_response("你好世界")
func TestMockModelClient_AddTextResponse_文本响应(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("你好世界")
	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hi"))
	require.NoError(t, err)
	assert.Equal(t, "你好世界", msg.Content.String())
}

// TestMockModelClient_响应队列依次消费 测试多轮响应队列
// 对齐 Python: mock_llm.set_responses([text1, text2, text3]) 依次消费
func TestMockModelClient_响应队列依次消费(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("第一轮")
	client.AddTextResponse("第二轮")
	client.AddTextResponse("第三轮")

	msg1, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q1"))
	require.NoError(t, err)
	assert.Equal(t, "第一轮", msg1.Content.String())

	msg2, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q2"))
	require.NoError(t, err)
	assert.Equal(t, "第二轮", msg2.Content.String())

	msg3, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q3"))
	require.NoError(t, err)
	assert.Equal(t, "第三轮", msg3.Content.String())

	// 队列耗尽后返回默认响应
	msg4, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q4"))
	require.NoError(t, err)
	assert.Equal(t, defaultMockResponse, msg4.Content.String())
}

// TestMockModelClient_AddToolCallResponse 测试工具调用响应
// 对齐 Python: create_tool_call_response("read_file", '{"path": "/tmp/test.txt"}')
func TestMockModelClient_AddToolCallResponse(t *testing.T) {
	client := NewMockModelClient()
	client.AddToolCallResponse("read_file", `{"path": "/tmp/test.txt"}`)
	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("读取文件"))
	require.NoError(t, err)
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, "read_file", msg.ToolCalls[0].Name)
	assert.Equal(t, `{"path": "/tmp/test.txt"}`, msg.ToolCalls[0].Arguments)
	assert.Equal(t, "mock_call_read_file", msg.ToolCalls[0].ID)
	assert.Equal(t, "tool_calls", msg.FinishReason)
}

// TestMockModelClient_AddErrorResponse 测试错误响应
func TestMockModelClient_AddErrorResponse(t *testing.T) {
	client := NewMockModelClient()
	client.AddErrorResponse(context.DeadlineExceeded)
	_, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestMockModelClient_InvokeCallCount 测试调用次数记录
// 对齐 Python: MockLLMModel.call_count
func TestMockModelClient_InvokeCallCount(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("a")
	client.AddTextResponse("b")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("1"))
	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("2"))

	assert.Equal(t, 2, client.InvokeCallCount())
}

// TestMockModelClient_GetInvokeCall 测试调用记录获取
// 对齐 Python: MockLLMModel.call_history[i]
func TestMockModelClient_GetInvokeCall(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("a")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))

	call := client.GetInvokeCall(0)
	assert.NotNil(t, call)
	assert.Equal(t, 1, call.InvokeCount)

	// 越界返回 nil
	assert.Nil(t, client.GetInvokeCall(1))
}

// TestMockModelClient_LastInvokeCall 测试最近调用记录
func TestMockModelClient_LastInvokeCall(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("a")
	client.AddTextResponse("b")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("first"))
	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("second"))

	last := client.LastInvokeCall()
	assert.Equal(t, 2, last.InvokeCount)
}

// TestMockModelClient_ResetCalls 测试重置调用历史
func TestMockModelClient_ResetCalls(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("a")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))
	assert.Equal(t, 1, client.InvokeCallCount())

	client.ResetCalls()
	assert.Equal(t, 0, client.InvokeCallCount())
}

// TestMockModelClient_Stream 测试流式响应
// 对齐 Python: MockLLMModel.stream() — 单次 yield 整个 chunk
func TestMockModelClient_Stream(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("流式响应")

	ch, err := client.Stream(context.Background(), model_clients.NewTextMessagesParam("hello"))
	require.NoError(t, err)

	chunk := <-ch
	assert.NotNil(t, chunk)
	assert.Equal(t, "流式响应", chunk.Content.String())
	assert.Equal(t, defaultMockModelName, chunk.UsageMetadata.ModelName)
}

// TestMockModelClient_Stream_ToolCall 测试流式工具调用响应
func TestMockModelClient_Stream_ToolCall(t *testing.T) {
	client := NewMockModelClient()
	client.AddToolCallResponse("search", `{"query": "test"}`)

	ch, err := client.Stream(context.Background(), model_clients.NewTextMessagesParam("搜索"))
	require.NoError(t, err)

	chunk := <-ch
	assert.NotNil(t, chunk)
	require.Len(t, chunk.ToolCalls, 1)
	assert.Equal(t, "search", chunk.ToolCalls[0].Name)
}

// TestMockModelClient_MixedResponseSequence 测试混合响应序列
// 对齐 Python: system_tests 中常见的 tool_call → text 模式
func TestMockModelClient_MixedResponseSequence(t *testing.T) {
	client := NewMockModelClient()
	// 第一次：工具调用
	client.AddToolCallResponse("write_file", `{"path": "a.txt", "content": "hello"}`)
	// 第二次：文本响应
	client.AddTextResponse("文件已写入")

	msg1, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("写入文件"))
	require.NoError(t, err)
	require.Len(t, msg1.ToolCalls, 1)
	assert.Equal(t, "write_file", msg1.ToolCalls[0].Name)

	msg2, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("结果"))
	require.NoError(t, err)
	assert.Equal(t, "文件已写入", msg2.Content.String())
	assert.Empty(t, msg2.ToolCalls)
}

// TestMockModelClient_CreateTextResponse 测试 CreateTextResponse 辅助函数
// 对齐 Python: create_text_response(content)
func TestMockModelClient_CreateTextResponse(t *testing.T) {
	resp := CreateTextResponse("测试文本")
	assert.Equal(t, "测试文本", resp.Text)
	assert.Empty(t, resp.ToolCalls)
	assert.Nil(t, resp.Err)
}

// TestMockModelClient_CreateToolCallResponse 测试 CreateToolCallResponse 辅助函数
// 对齐 Python: create_tool_call_response(tool_name, arguments)
func TestMockModelClient_CreateToolCallResponse(t *testing.T) {
	resp := CreateToolCallResponse("my_tool", `{"key": "value"}`)
	assert.Empty(t, resp.Text)
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "my_tool", resp.ToolCalls[0].Name)
	assert.Equal(t, `{"key": "value"}`, resp.ToolCalls[0].Arguments)
	assert.Equal(t, "mock_call_my_tool", resp.ToolCalls[0].ID)
}

// TestMockModelClient_CreateJSONResponse 测试 CreateJSONResponse 辅助函数
// 对齐 Python: create_json_response(data)
func TestMockModelClient_CreateJSONResponse(t *testing.T) {
	data := map[string]any{"name": "test", "count": float64(42)}
	resp := CreateJSONResponse(data)
	assert.Contains(t, resp.Text, `"name"`)
	assert.Contains(t, resp.Text, `"test"`)
	assert.Contains(t, resp.Text, `"count"`)
	assert.Nil(t, resp.Err)
}

// TestMockModelClient_AddJSONResponse 测试 AddJSONResponse 链式追加
func TestMockModelClient_AddJSONResponse(t *testing.T) {
	client := NewMockModelClient()
	client.AddJSONResponse(map[string]any{"status": "ok"})

	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("query"))
	require.NoError(t, err)
	assert.Contains(t, msg.Content.String(), `"status"`)
	assert.Contains(t, msg.Content.String(), `"ok"`)
}

// TestMockModelClient_RegisterToClientRegistry 测试 ClientRegistry 注册
// 对齐 Python: mock_llm_context() 通过 patch 注入 MockLLM
func TestMockModelClient_RegisterToClientRegistry(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("注册测试")

	registry := model_clients.NewClientRegistry()
	registry.Register("mock_itest", "llm", client.Factory())

	got, err := registry.GetClient("mock_itest", "llm",
		&llmschema.ModelRequestConfig{ModelName: "mock-model"},
		&llmschema.ModelClientConfig{ClientID: "mock_itest_id"},
	)
	require.NoError(t, err)
	require.NotNil(t, got)
}

// TestMockModelClient_ConcurrentInvoke 测试并发安全
func TestMockModelClient_ConcurrentInvoke(t *testing.T) {
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
		assert.NoError(t, <-errCh)
	}
	assert.Equal(t, 10, client.InvokeCallCount())
}

// TestMockModelClient_SetResponses_重置语义 测试 SetResponses 的重置语义
// 对齐 Python: MockLLMModel.set_responses() 同时重置 call_count 和 call_history
func TestMockModelClient_SetResponses_重置语义(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("旧响应")

	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q"))
	assert.Equal(t, 1, client.InvokeCallCount())

	// SetResponses 重置队列和调用历史
	client.SetResponses(MockResponse{Text: "新响应"})
	assert.Equal(t, 0, client.InvokeCallCount())

	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("q"))
	require.NoError(t, err)
	assert.Equal(t, "新响应", msg.Content.String())
}

// TestMockModelClient_UsageMetadata 测试响应携带 UsageMetadata
// 对齐 Python: UsageMetadata(model_name="mock-model")
func TestMockModelClient_UsageMetadata(t *testing.T) {
	client := NewMockModelClient()
	client.AddTextResponse("hello")

	msg, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hi"))
	require.NoError(t, err)
	require.NotNil(t, msg.UsageMetadata)
	assert.Equal(t, defaultMockModelName, msg.UsageMetadata.ModelName)
}
