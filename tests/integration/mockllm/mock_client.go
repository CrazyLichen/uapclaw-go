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
	// FinishReason 指定的 finish_reason（为空时按既有规则推导：有 tool_calls 则 tool_calls）
	FinishReason string
	// UsageCode usage_metadata.code（非 0 表示 provider 返回错误响应）
	UsageCode int
	// UsageErrMsg usage_metadata.err_msg（provider 错误文案）
	UsageErrMsg string
	// OutputTokens usage_metadata.output_tokens（输出 token 计数）
	OutputTokens int
}

// InvokeRecord 记录一次 Invoke 调用，用于测试断言。
// 对齐 Python MockLLMModel.call_history 中的条目。
type InvokeRecord struct {
	// InvokeCount 调用序号（从 1 开始）
	InvokeCount int
	// Messages 本次调用传入的消息参数（用于断言 prompt 内容）
	Messages model_clients.MessagesParam
}

// StreamRecord 记录一次 Stream 调用的入参。
//
// 独立于 InvokeRecord：流式路径（web/adapter 的 ProcessMessageStreamImpl）只走 Stream，
// 若与 Invoke 共用同一份记录，会把「Invoke 调用次数」的语义污染掉。
// 用途：断言模型实际收到的系统提示词与会话上下文（如提示词节是否残留）。
type StreamRecord struct {
	// CallCount 流式调用序号（从 1 开始）
	CallCount int
	// Messages 本次调用传入的消息参数
	Messages model_clients.MessagesParam
}

// MockModelClient 集成测试专用的 LLM Mock 客户端。
// 对齐 Python tests/unit_tests/fixtures/mock_llm.py:MockLLMModel
//
// 核心特性：
//   - 预设响应队列，按调用顺序依次消费
//   - 队列耗尽后返回默认文本响应
//   - 记录调用历史（InvokeCallCount / GetInvokeCall / LastInvokeCall）
//   - 记录流式入参（StreamCallCount / GetStreamCall / LastStreamCall）
//   - 并发安全（sync.Mutex 保护）
//   - 支持注册到 ClientRegistry（Factory 方法）
type MockModelClient struct {
	mu          sync.Mutex
	responses   []MockResponse
	callCount   int
	invokeCalls []InvokeRecord
	streamCalls []StreamRecord
	streamCount int
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

// ToolCallSpec 工具调用规格，用于 CreateMultiToolCallResponse。
type ToolCallSpec struct {
	// Name 工具名称
	Name string
	// ArgsJSON 工具参数 JSON
	ArgsJSON string
	// CallID 工具调用 ID（可选，为空时自动生成）
	CallID string
}

// CreateMultiToolCallResponse 创建并发工具调用预设响应。
// 对齐 Python: create_tool_call_response 多个工具同时调用。
// 每个工具调用自动生成唯一 ID（mock_call_<name>_<index>）。
func CreateMultiToolCallResponse(specs []ToolCallSpec) MockResponse {
	toolCalls := make([]*llmschema.ToolCall, 0, len(specs))
	for i, spec := range specs {
		callID := spec.CallID
		if callID == "" {
			callID = fmt.Sprintf("mock_call_%s_%d", spec.Name, i)
		}
		tc := llmschema.NewToolCall(callID, spec.Name, spec.ArgsJSON)
		toolCalls = append(toolCalls, tc)
	}
	return MockResponse{
		ToolCalls: toolCalls,
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

// CreateProviderErrorResponse 创建「provider 错误响应」预设。
//
// 与 CreateErrorResponse 的区别：后者模拟传输层直接返回 error（Invoke 返回 err），
// 前者模拟 HTTP 200 但响应体带错误语义（finish_reason=error + usage.code != 0），
// 用于覆盖调用方对「错误响应」而非「错误」的判定链路。
func CreateProviderErrorResponse(code int, errMsg string) MockResponse {
	return MockResponse{
		FinishReason: "error",
		UsageCode:    code,
		UsageErrMsg:  errMsg,
	}
}

// CreateOutputLimitResponse 创建「输出上限命中且无正文」预设。
//
// finish_reason=length 且 content 为空：推理模型把输出预算花在思维链上，
// 调用方应把这种空响应判定为模型调用失败。
func CreateOutputLimitResponse(outputTokens int) MockResponse {
	return MockResponse{
		FinishReason: "length",
		OutputTokens: outputTokens,
	}
}

// Invoke 实现 BaseModelClient.Invoke，按序消费预设响应队列。
// 对齐 Python: MockLLMModel.invoke(messages, ...)
// 增强：当 InvokeOption 中包含 OutputParser 时，自动解析 JSON 并填充 ParserContent。
func (m *MockModelClient) Invoke(_ context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++
	record := InvokeRecord{InvokeCount: m.callCount, Messages: messages}
	m.invokeCalls = append(m.invokeCalls, record)

	resp := m.getNextResponseLocked()
	if resp.Err != nil {
		return nil, resp.Err
	}

	result := m.buildAssistantMessage(resp)

	// 如果传入 OutputParser，自动解析并填充 ParserContent
	// 对齐 Python: LTM 的 GenAllMemory 通过 WithInvokeOutputParser(parser) 传入解析器，
	// 真实 LLM 客户端在响应解析时自动调用 parser.Parse()。Mock 客户端需等价处理。
	params := model_clients.NewInvokeParams(opts...)
	if params.OutputParser != nil {
		parsed, parseErr := params.OutputParser.Parse(result.Content.Text())
		if parseErr == nil && parsed != nil {
			result.ParserContent = parsed
		}
	}

	return result, nil
}

// Stream 实现 BaseModelClient.Stream，将预设响应作为单个 chunk 发出。
// 对齐 Python: MockLLMModel.stream(messages, ...) — 单次 yield 整个 chunk
//
// 入参 messages 会记录到 streamCalls，供用例断言「模型实际收到的提示词」。
func (m *MockModelClient) Stream(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	m.mu.Lock()
	resp := m.getNextResponseLocked()
	m.callCount++
	m.streamCount++
	m.streamCalls = append(m.streamCalls, StreamRecord{CallCount: m.streamCount, Messages: messages})
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

// StreamCallCount 返回 Stream 被调用的总次数。
func (m *MockModelClient) StreamCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamCount
}

// GetStreamCall 返回第 i 次 Stream 调用的记录（0-indexed）。
func (m *MockModelClient) GetStreamCall(i int) *StreamRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.streamCalls) {
		return nil
	}
	return &m.streamCalls[i]
}

// LastStreamCall 返回最近一次 Stream 调用的记录。
func (m *MockModelClient) LastStreamCall() *StreamRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.streamCalls) == 0 {
		return nil
	}
	return &m.streamCalls[len(m.streamCalls)-1]
}

// ResetCalls 清空调用历史和计数，同时清空响应队列。
// 对齐 Python: MockLLMModel.set_responses([]) 的重置语义
func (m *MockModelClient) ResetCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount = 0
	m.invokeCalls = nil
	m.streamCalls = nil
	m.streamCount = 0
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
			ModelName:    defaultMockModelName,
			Code:         resp.UsageCode,
			ErrMsg:       resp.UsageErrMsg,
			OutputTokens: resp.OutputTokens,
		}),
	}
	if resp.FinishReason != "" {
		opts = append(opts, llmschema.WithFinishReason(resp.FinishReason))
	}
	if len(resp.ToolCalls) > 0 {
		opts = append(opts, llmschema.WithToolCalls(resp.ToolCalls))
		opts = append(opts, llmschema.WithFinishReason("tool_calls"))
	}
	return llmschema.NewAssistantMessage(resp.Text, opts...)
}
