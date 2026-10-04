//go:build integration

package mockllm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MockResponse 预设响应，支持纯文本、工具调用、错误三种模式。
// 对齐 Python MockLLMModel 的 create_text_response / create_tool_call_response。
type MockResponse struct {
	// Text 纯文本响应内容
	Text string
	// ToolCalls 工具调用响应列表
	ToolCalls []*llmschema.ToolCall
	// Err 模拟错误
	Err error
}

// InvokeRecord 记录一次 Invoke 调用，用于测试断言。
// 对齐 Python MockLLMModel.call_history 中的条目。
type InvokeRecord struct {
	// InvokeCount 调用序号（从 1 开始）
	InvokeCount int
}

// MockModelClient 集成测试专用的 LLM Mock 客户端。
// 对齐 Python tests/unit_tests/fixtures/mock_llm.py:MockLLMModel
//
// 核心特性：
//   - 预设响应队列，按调用顺序依次消费
//   - 队列耗尽后返回默认文本响应
//   - 记录调用历史（InvokeCallCount / GetInvokeCall / LastInvokeCall）
//   - 并发安全（sync.Mutex 保护）
//   - 支持注册到 ClientRegistry（Factory 方法）
type MockModelClient struct {
	mu          sync.Mutex
	responses   []MockResponse
	callCount   int
	invokeCalls []InvokeRecord
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMockResponse 队列耗尽后的默认响应文本
	// 对齐 Python: _get_next_response 耗尽返回 "Default mock response"
	defaultMockResponse = "MockLLM 默认响应"
	// defaultMockModelName Mock 模型名称
	// 对齐 Python: MockLLMModel.__init__ 中 model_config.model_name = "mock-model"
	defaultMockModelName = "mock-model"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时接口断言
var _ model_clients.BaseModelClient = (*MockModelClient)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockModelClient 创建 MockModelClient 实例。
// 可传入初始响应列表，也可后续通过 AddTextResponse / AddToolCallResponse 追加。
// 对齐 Python: MockLLMModel()
func NewMockModelClient(responses ...MockResponse) *MockModelClient {
	return &MockModelClient{
		responses: responses,
	}
}

// CreateTextResponse 创建纯文本预设响应。
// 对齐 Python: create_text_response(content, model_name, finish_reason)
func CreateTextResponse(content string) MockResponse {
	return MockResponse{Text: content}
}

// CreateToolCallResponse 创建工具调用预设响应。
// 对齐 Python: create_tool_call_response(tool_name, arguments, tool_call_id, model_name)
func CreateToolCallResponse(toolName string, argsJSON string) MockResponse {
	tc := llmschema.NewToolCall(
		fmt.Sprintf("mock_call_%s", toolName),
		toolName,
		argsJSON,
	)
	return MockResponse{
		ToolCalls: []*llmschema.ToolCall{tc},
	}
}

// CreateJSONResponse 创建 JSON 格式的预设响应。
// 对齐 Python: create_json_response(data, model_name)
func CreateJSONResponse(data map[string]any) MockResponse {
	b, err := json.Marshal(data)
	if err != nil {
		return MockResponse{Text: fmt.Sprintf("{\"error\": \"%s\"}", err.Error())}
	}
	return MockResponse{Text: string(b)}
}

// CreateErrorResponse 创建错误预设响应。
func CreateErrorResponse(err error) MockResponse {
	return MockResponse{Err: err}
}

// Invoke 实现 BaseModelClient.Invoke，按序消费预设响应队列。
// 对齐 Python: MockLLMModel.invoke(messages, ...)
func (m *MockModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++
	record := InvokeRecord{InvokeCount: m.callCount}
	m.invokeCalls = append(m.invokeCalls, record)

	resp := m.getNextResponseLocked()
	if resp.Err != nil {
		return nil, resp.Err
	}
	return m.buildAssistantMessage(resp), nil
}

// Stream 实现 BaseModelClient.Stream，将预设响应作为单个 chunk 发出。
// 对齐 Python: MockLLMModel.stream(messages, ...) — 单次 yield 整个 chunk
func (m *MockModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	m.mu.Lock()
	resp := m.getNextResponseLocked()
	m.callCount++
	m.mu.Unlock()

	if resp.Err != nil {
		return nil, resp.Err
	}

	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	chunk := llmschema.NewAssistantMessageChunk(resp.Text)
	if len(resp.ToolCalls) > 0 {
		chunk.ToolCalls = resp.ToolCalls
	}
	chunk.UsageMetadata = &llmschema.UsageMetadata{ModelName: defaultMockModelName}
	ch <- chunk
	close(ch)

	return ch, nil
}

// GenerateImage 实现 BaseModelClient.GenerateImage。
// 对齐 Python: MockLLMModel.generate_image — 返回占位数据
func (m *MockModelClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("MockModelClient 不支持 GenerateImage")
}

// GenerateSpeech 实现 BaseModelClient.GenerateSpeech。
// 对齐 Python: MockLLMModel.generate_speech — 返回占位数据
func (m *MockModelClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("MockModelClient 不支持 GenerateSpeech")
}

// GenerateVideo 实现 BaseModelClient.GenerateVideo。
// 对齐 Python: MockLLMModel.generate_video — 返回占位数据
func (m *MockModelClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("MockModelClient 不支持 GenerateVideo")
}

// TranscribeAudio 实现 BaseModelClient.TranscribeAudio。
func (m *MockModelClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("MockModelClient 不支持 TranscribeAudio")
}

// Release 实现 BaseModelClient.Release。
func (m *MockModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 实现 BaseModelClient.SupportsKVCacheRelease。
func (m *MockModelClient) SupportsKVCacheRelease() bool {
	return false
}

// --- 响应配置（链式调用） ---

// SetResponses 设置完整响应队列，覆盖已有队列并重置调用计数。
// 对齐 Python: MockLLMModel.set_responses(responses)
func (m *MockModelClient) SetResponses(responses ...MockResponse) *MockModelClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = responses
	m.callCount = 0
	m.invokeCalls = nil
	return m
}

// AddTextResponse 追加一条纯文本响应。
// 对齐 Python: mock_llm.set_response("文本") / create_text_response("文本")
func (m *MockModelClient) AddTextResponse(text string) *MockModelClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, MockResponse{Text: text})
	return m
}

// AddToolCallResponse 追加一条工具调用响应。
// 对齐 Python: create_tool_call_response(tool_name, arguments)
func (m *MockModelClient) AddToolCallResponse(toolName string, argsJSON string) *MockModelClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	tc := llmschema.NewToolCall(
		fmt.Sprintf("mock_call_%s", toolName),
		toolName,
		argsJSON,
	)
	m.responses = append(m.responses, MockResponse{
		ToolCalls: []*llmschema.ToolCall{tc},
	})
	return m
}

// AddJSONResponse 追加一条 JSON 格式响应。
// 对齐 Python: create_json_response(data)
func (m *MockModelClient) AddJSONResponse(data map[string]any) *MockModelClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, CreateJSONResponse(data))
	return m
}

// AddErrorResponse 追加一条错误响应。
func (m *MockModelClient) AddErrorResponse(err error) *MockModelClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, MockResponse{Err: err})
	return m
}

// --- 调用断言 ---

// InvokeCallCount 返回 Invoke 被调用的总次数。
// 对齐 Python: MockLLMModel.call_count
func (m *MockModelClient) InvokeCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// GetInvokeCall 返回第 i 次 Invoke 调用的记录（0-indexed）。
// 对齐 Python: MockLLMModel.call_history[i]
func (m *MockModelClient) GetInvokeCall(i int) *InvokeRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.invokeCalls) {
		return nil
	}
	return &m.invokeCalls[i]
}

// LastInvokeCall 返回最近一次 Invoke 调用的记录。
func (m *MockModelClient) LastInvokeCall() *InvokeRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.invokeCalls) == 0 {
		return nil
	}
	return &m.invokeCalls[len(m.invokeCalls)-1]
}

// ResetCalls 清空调用历史和计数，同时清空响应队列。
// 对齐 Python: MockLLMModel.set_responses([]) 的重置语义
func (m *MockModelClient) ResetCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount = 0
	m.invokeCalls = nil
}

// --- ClientRegistry 注册 ---

// Factory 返回可用于 ClientRegistry.Register 的 ClientFactory。
// 用法：registry.Register("mock_itest", "llm", client.Factory())
//
// 对齐 Python: mock_llm_context() 中通过 patch 替换 Model.invoke/stream，
// Go 通过 ClientRegistry 注入 mock client 实现等价效果。
func (m *MockModelClient) Factory() model_clients.ClientFactory {
	return func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return m, nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getNextResponseLocked 从队列头部取出下一条响应（需在 mu.Lock 内调用）。
// 队列耗尽时返回默认文本响应（对齐 Python: _get_next_response 耗尽返回 "Default mock response"）。
func (m *MockModelClient) getNextResponseLocked() MockResponse {
	if len(m.responses) == 0 {
		return MockResponse{Text: defaultMockResponse}
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp
}

// buildAssistantMessage 从 MockResponse 构造 AssistantMessage。
// 对齐 Python: create_text_response / create_tool_call_response 的构造逻辑。
func (m *MockModelClient) buildAssistantMessage(resp MockResponse) *llmschema.AssistantMessage {
	opts := []llmschema.AssistantMessageOption{
		llmschema.WithAssistantUsageMetadata(&llmschema.UsageMetadata{
			ModelName: defaultMockModelName,
		}),
	}
	if len(resp.ToolCalls) > 0 {
		opts = append(opts, llmschema.WithToolCalls(resp.ToolCalls))
		opts = append(opts, llmschema.WithFinishReason("tool_calls"))
	}
	return llmschema.NewAssistantMessage(resp.Text, opts...)
}
