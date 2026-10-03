package extract

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildTimeContext_合法ISO时间 测试合法 ISO 时间戳转换为中文周范围
func TestBuildTimeContext_合法ISO时间(t *testing.T) {
	// 2026-01-06 是周二，周一应为 2026-01-05，周日应为 2026-01-11
	result := buildTimeContext("2026-01-06T10:00:00")
	expected := "2026年1月5日(周一)～2026年1月11日(周日)（即01.05～01.11）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_周中日期 测试周中的日期也能正确计算周范围
func TestBuildTimeContext_周中日期(t *testing.T) {
	// 2026-03-18 是周三，周一应为 2026-03-16，周日应为 2026-03-22
	result := buildTimeContext("2026-03-18T15:30:00")
	expected := "2026年3月16日(周一)～2026年3月22日(周日)（即03.16～03.22）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_周日日期 测试周日日期
func TestBuildTimeContext_周日日期(t *testing.T) {
	// 2026-01-11 是周日，周一应为 2026-01-05（Python: dt.weekday() 对周日返回 6）
	result := buildTimeContext("2026-01-11T10:00:00")
	expected := "2026年1月5日(周一)～2026年1月11日(周日)（即01.05～01.11）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_跨月周 测试跨月的周范围
func TestBuildTimeContext_跨月周(t *testing.T) {
	// 2026-03-31 是周二，周一应为 2026-03-30，周日应为 2026-04-05
	result := buildTimeContext("2026-03-31T10:00:00")
	expected := "2026年3月30日(周一)～2026年4月5日(周日)（即03.30～04.05）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_空串 测试空串原样返回
func TestBuildTimeContext_空串(t *testing.T) {
	result := buildTimeContext("")
	if result != "" {
		t.Errorf("buildTimeContext('') = %q, want empty string", result)
	}
}

// TestBuildTimeContext_非法格式 测试非法格式原样返回
func TestBuildTimeContext_非法格式(t *testing.T) {
	result := buildTimeContext("not-a-date")
	if result != "not-a-date" {
		t.Errorf("buildTimeContext('not-a-date') = %q, want %q", result, "not-a-date")
	}
}

// TestBuildTimeContext_带时区信息 测试带时区信息的 ISO 时间戳
func TestBuildTimeContext_带时区信息(t *testing.T) {
	// 2026-01-06 是周二，周一应为 2026-01-05
	result := buildTimeContext("2026-01-06T10:00:00+08:00")
	expected := "2026年1月5日(周一)～2026年1月11日(周日)（即01.05～01.11）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_空格分隔时间 测试 Python 常见的 "2026-01-06 10:00:00" 格式
func TestBuildTimeContext_空格分隔时间(t *testing.T) {
	// Python: timestamp.strftime('%Y-%m-%d %H:%M:%S') 产出此格式
	result := buildTimeContext("2026-01-06 10:00:00")
	expected := "2026年1月5日(周一)～2026年1月11日(周日)（即01.05～01.11）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestExtractLongTermMemory_上下文取消 测试 ctx 取消时提前返回
func TestExtractLongTermMemory_上下文取消(t *testing.T) {
	// 创建可取消的 ctx，立即取消
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 使用支持 ctx 检查的 mock：检查 ctx 状态，如果已取消则返回 context.Canceled
	client := &mockLLMClient{
		invokeFn: func(ctx context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			// 模拟真实 LLM 客户端行为：检查 ctx 是否已取消
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return llmschema.NewAssistantMessage(`{}`), nil
			}
		},
	}
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, _ := llm.NewModel(clientCfg, nil, llm.WithClient(client))

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseChatModel:   model,
	}

	result, err := ExtractLongTermMemory(ctx, params, "2026-01-06 10:00:00", nil, 3)
	if err == nil {
		t.Fatal("ctx 取消时应返回错误")
	}
	if result != nil {
		t.Errorf("ctx 取消时 result 应为 nil，实际 %v", result)
	}
	if !strings.Contains(err.Error(), "长期记忆提取 LLM 调用失败") {
		t.Errorf("错误信息应包含'长期记忆提取 LLM 调用失败'，实际: %v", err)
	}
}

// TestBuildTimeContext_UTC时间戳 测试 Z 后缀 UTC 时间戳
func TestBuildTimeContext_UTC时间戳(t *testing.T) {
	result := buildTimeContext("2026-01-06T10:00:00Z")
	// Z 后缀是 UTC 时间，2026-01-06 周二，应返回该周范围
	if result == "" || result == "2026-01-06T10:00:00Z" {
		t.Errorf("buildTimeContext('Z') = %q, want week range string", result)
	}
	// 验证具体输出
	expected := "2026年1月5日(周一)～2026年1月11日(周日)（即01.05～01.11）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_负时区时间戳 测试 -05:00 格式时间戳
func TestBuildTimeContext_负时区时间戳(t *testing.T) {
	result := buildTimeContext("2026-01-06T10:00:00-05:00")
	if result == "" || result == "2026-01-06T10:00:00-05:00" {
		t.Errorf("buildTimeContext('-05:00') = %q, want week range string", result)
	}
}

// TestExtractLongTermMemory_合法JSON返回 测试 LLM 返回合法 JSON
func TestExtractLongTermMemory_合法JSON返回(t *testing.T) {
	jsonResponse := `{
		"has_explict_instruct": false,
		"instruct_memories": [],
		"user_profile": ["用户喜欢编程"],
		"semantic_memory": ["Go 语言支持泛型"],
		"episodic_memory": ["用户今天学习了 Go 泛型"]
	}`

	model := newFakeModelWithResponse(t, jsonResponse)

	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("我今天学习了 Go 泛型"),
	}
	historyMessages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("你好！有什么可以帮你的？"),
	}

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        messages,
		HistoryMessages: historyMessages,
		BaseChatModel:   model,
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 3)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
	if _, ok := result["user_profile"]; !ok {
		t.Error("结果缺少 user_profile 键")
	}
	if _, ok := result["semantic_memory"]; !ok {
		t.Error("结果缺少 semantic_memory 键")
	}
	if _, ok := result["episodic_memory"]; !ok {
		t.Error("结果缺少 episodic_memory 键")
	}
}

// TestExtractLongTermMemory_解析失败返回空map 测试 LLM 返回非法 JSON 时重试后返回空 map
func TestExtractLongTermMemory_解析失败返回空map(t *testing.T) {
	model := newFakeModelWithResponse(t, "not valid json {{{")

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseChatModel:   model,
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 2)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 不应返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
	if len(result) != 0 {
		t.Errorf("解析失败时应返回空 map，实际 %d 个键", len(result))
	}
}

// TestExtractLongTermMemory_Invoke错误向上传播 测试 LLM Invoke 错误直接返回
func TestExtractLongTermMemory_Invoke错误向上传播(t *testing.T) {
	model := newFakeModelWithInvokeErr(fmt.Errorf("API 调用失败"))

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseChatModel:   model,
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 3)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if result != nil {
		t.Errorf("Invoke 错误时 result 应为 nil，实际 %v", result)
	}
	if !strings.Contains(err.Error(), "长期记忆提取 LLM 调用失败") {
		t.Errorf("错误信息应包含'长期记忆提取 LLM 调用失败'，实际: %v", err)
	}
}

// TestExtractLongTermMemory_scopeConfig为nil使用默认值 测试 scopeConfig 为 nil 时使用默认值
func TestExtractLongTermMemory_scopeConfig为nil使用默认值(t *testing.T) {
	jsonResponse := `{
		"has_explict_instruct": false,
		"instruct_memories": [],
		"user_profile": [],
		"semantic_memory": [],
		"episodic_memory": []
	}`

	model := newFakeModelWithResponse(t, jsonResponse)

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseChatModel:   model,
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 3)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
}

// TestExtractLongTermMemory_自定义scopeConfig 测试传入自定义 scopeConfig
func TestExtractLongTermMemory_自定义scopeConfig(t *testing.T) {
	jsonResponse := `{
		"has_explict_instruct": false,
		"instruct_memories": [],
		"user_profile": ["自定义画像"],
		"semantic_memory": [],
		"episodic_memory": []
	}`

	model := newFakeModelWithResponse(t, jsonResponse)

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseChatModel:   model,
	}

	scopeConfig := &config.MemoryScopeConfig{
		UserProfileDefinition:    "自定义用户画像定义",
		SemanticMemoryDefinition: "自定义语义记忆定义",
		EpisodicMemoryDefinition: "自定义情景记忆定义",
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", scopeConfig, 3)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
}

// TestExtractLongTermMemory_retries为1时仅尝试一次 测试 retries=1 时 LLM 返回无法解析的 JSON 只尝试一次
func TestExtractLongTermMemory_retries为1时仅尝试一次(t *testing.T) {
	invokeCount := 0
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			invokeCount++
			return llmschema.NewAssistantMessage("not valid json {{{"), nil
		},
	}
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, _ := llm.NewModel(clientCfg, nil, llm.WithClient(client))

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseChatModel:   model,
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 1)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 不应返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
	if len(result) != 0 {
		t.Errorf("解析失败时应返回空 map，实际 %d 个键", len(result))
	}
	if invokeCount != 1 {
		t.Errorf("retries=1 时 LLM 应仅调用一次，实际调用 %d 次", invokeCount)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mockLLMClient 模拟 BaseModelClient，记录 Invoke 调用参数。
// 对齐项目已有模式：internal/agentcore/context_evolver/service/llm_wrapper_test.go (mockLLMClient)
type mockLLMClient struct {
	// invokeFn 自定义 Invoke 行为，nil 时返回默认成功响应
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

// Invoke 实现 BaseModelClient.Invoke，执行 invokeFn 并处理 OutputParser。
//
// 重要：必须手动调用 OutputParser.Parse() 并设置 resp.ParserContent，
// 对齐真实 client 的行为（真实 client 在 Invoke 内部会调用 OutputParser
// 并将解析结果填入 ParserContent）。如果不模拟此步骤，ExtractLongTermMemory
// 永远拿不到 parsedResult，所有测试将失败。
func (m *mockLLMClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		resp, err := m.invokeFn(ctx, messages, opts...)
		if err != nil {
			return nil, err
		}
		// 对齐真实 client 行为：如果传入了 OutputParser，尝试解析并设置 ParserContent
		params := model_clients.NewInvokeParams(opts...)
		if params.OutputParser != nil && resp.Content.Text() != "" {
			parsed, parseErr := params.OutputParser.Parse(resp.Content.Text())
			if parseErr == nil && parsed != nil {
				resp.ParserContent = parsed
			}
		}
		return resp, nil
	}
	return llmschema.NewAssistantMessage("mock response"), nil
}

// Stream 实现 BaseModelClient.Stream，返回不支持。
func (m *mockLLMClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateImage 实现 BaseModelClient.GenerateImage，返回不支持。
func (m *mockLLMClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateSpeech 实现 BaseModelClient.GenerateSpeech，返回不支持。
func (m *mockLLMClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateVideo 实现 BaseModelClient.GenerateVideo，返回不支持。
func (m *mockLLMClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// TranscribeAudio 实现 BaseModelClient.TranscribeAudio，返回不支持。
func (m *mockLLMClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// Release 实现 BaseModelClient.Release，返回不支持。
func (m *mockLLMClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, fmt.Errorf("not supported in mock")
}

// SupportsKVCacheRelease 实现 BaseModelClient.SupportsKVCacheRelease。
func (m *mockLLMClient) SupportsKVCacheRelease() bool {
	return false
}

// newFakeModelWithResponse 构造返回预设 JSON 字符串的 fake *llm.Model。
//
// 通过 NewModel + 合法 provider（"openai" + 虚假凭据 + WithVerifySSL(false)）
// + WithClient 覆盖底层客户端实现 mock。
// 注意：不能用 NewModelClientConfig("mock", ...)，因为 "mock" 不是合法 provider 会被校验拒绝。
// WithInvokeOutputParser 会自动解析 JSON 并填入 ParserContent。
func newFakeModelWithResponse(t *testing.T, jsonStr string) *llm.Model {
	t.Helper()
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			return llmschema.NewAssistantMessage(jsonStr), nil
		},
	}
	// 使用合法 provider "openai" + 虚假凭据，WithClient 覆盖掉真实客户端
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造 ModelClientConfig 失败: %v", err)
	}
	model, err := llm.NewModel(clientCfg, nil, llm.WithClient(client))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// newFakeModelWithInvokeErr 构造 Invoke 返回错误的 fake *llm.Model。
// 同 newFakeModelWithResponse，使用合法 provider + WithClient 模式。
func newFakeModelWithInvokeErr(invokeErr error) *llm.Model {
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			return nil, invokeErr
		},
	}
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, _ := llm.NewModel(clientCfg, nil, llm.WithClient(client))
	return model
}
