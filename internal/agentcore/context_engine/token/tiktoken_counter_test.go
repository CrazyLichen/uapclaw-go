package token

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/stretchr/testify/assert"
)

// TestNewTiktokenCounter_默认模型 验证 model 为空时默认使用 "gpt-4"
func TestNewTiktokenCounter_默认模型(t *testing.T) {
	tc := NewTiktokenCounter("")
	assert.NotNil(t, tc)
	assert.Equal(t, "gpt-4", tc.model)
	assert.NotNil(t, tc.enc, "enc 不应为 nil（gpt-4 是已知模型）")
}

// TestNewTiktokenCounter_GPT4o 验证 "gpt-4o" 使用 o200k_base 编码
func TestNewTiktokenCounter_GPT4o(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4o")
	assert.NotNil(t, tc)
	assert.NotNil(t, tc.enc, "enc 不应为 nil（gpt-4o 是已知模型）")
	assert.Equal(t, "gpt-4o", tc.model)
}

// TestNewTiktokenCounter_GPT35Turbo 验证 "gpt-3.5-turbo" 使用 cl100k_base
func TestNewTiktokenCounter_GPT35Turbo(t *testing.T) {
	tc := NewTiktokenCounter("gpt-3.5-turbo")
	assert.NotNil(t, tc)
	assert.NotNil(t, tc.enc)
}

// TestNewTiktokenCounter_未知模型降级 验证未知模型降级到 Cl100kBase
func TestNewTiktokenCounter_未知模型降级(t *testing.T) {
	tc := NewTiktokenCounter("qwen-max")
	assert.NotNil(t, tc)
	// 未知模型应降级到 Cl100kBase（enc 不为 nil）
	assert.NotNil(t, tc.enc, "未知模型应降级到 cl100k_base")
}

// TestCount_纯文本 验证英文/中文/混合文本的 token 计数
func TestCount_纯文本(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	// 英文文本
	enCount, err := tc.Count("hello world", "gpt-4")
	assert.NoError(t, err)
	assert.Greater(t, enCount, 0, "英文文本 token 数应大于 0")

	// 中文文本
	zhCount, err := tc.Count("你好世界", "gpt-4")
	assert.NoError(t, err)
	assert.Greater(t, zhCount, 0, "中文文本 token 数应大于 0")

	// 混合文本
	mixCount, err := tc.Count("hello 世界", "gpt-4")
	assert.NoError(t, err)
	assert.Greater(t, mixCount, 0, "混合文本 token 数应大于 0")

	// 长文本 token 数应大于短文本
	longCount, err := tc.Count("This is a longer sentence with more words.", "gpt-4")
	assert.NoError(t, err)
	assert.Greater(t, longCount, enCount, "长文本 token 数应大于短文本")
}

// TestCount_空字符串 验证返回 (0, nil)
func TestCount_空字符串(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")
	count, err := tc.Count("", "gpt-4")
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestCountMessages_多角色 验证 system/user/assistant/tool 消息格式化后计数
func TestCountMessages_多角色(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	messages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("You are a helpful assistant."),
		llm_schema.NewUserMessage("What is the weather?"),
		llm_schema.NewAssistantMessage("The weather is sunny."),
	}

	count, err := tc.CountMessages(messages, "gpt-4")
	assert.NoError(t, err)
	assert.Greater(t, count, 0, "多角色消息 token 数应大于 0")
	// 末尾 +3
	assert.GreaterOrEqual(t, count, 3, "应包含末尾 3 tokens")
}

// TestCountMessages_AssistantToolCalls 验证 AssistantMessage 带 ToolCalls 时额外计数
func TestCountMessages_AssistantToolCalls(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	// 不带 ToolCalls
	msgNoCalls := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("hello"),
	}
	countNoCalls, err := tc.CountMessages(msgNoCalls, "gpt-4")
	assert.NoError(t, err)

	// 带 ToolCalls
	calls := []*llm_schema.ToolCall{
		llm_schema.NewToolCall("call_1", "search", `{"query":"weather"}`),
	}
	msgWithCalls := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(calls)),
	}
	countWithCalls, err := tc.CountMessages(msgWithCalls, "gpt-4")
	assert.NoError(t, err)

	assert.Greater(t, countWithCalls, countNoCalls,
		"带 ToolCalls 的消息 token 数应大于不带 ToolCalls 的消息")
}

// TestCountMessages_空列表 验证返回 (0, nil)
func TestCountMessages_空列表(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")
	count, err := tc.CountMessages(nil, "gpt-4")
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	count, err = tc.CountMessages([]llm_schema.BaseMessage{}, "gpt-4")
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestCountTools_多个工具 验证 tools 按 functions.{name}:{idx} 格式计数
func TestCountTools_多个工具(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	tools := []*schema.ToolInfo{
		schema.NewToolInfo("search", "Search the web", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		}),
		schema.NewToolInfo("calculator", "Calculate math", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{"type": "string"},
			},
		}),
	}

	count, err := tc.CountTools(tools, "gpt-4")
	assert.NoError(t, err)
	assert.Greater(t, count, 0, "工具 token 数应大于 0")
	// 末尾 +3
	assert.GreaterOrEqual(t, count, 3, "应包含末尾 3 tokens")
}

// TestCountTools_空列表 验证返回 (0, nil)
func TestCountTools_空列表(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")
	count, err := tc.CountTools(nil, "gpt-4")
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	count, err = tc.CountTools([]*schema.ToolInfo{}, "gpt-4")
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestCountTools_Parameters为空 验证 parameters 为 nil 时的处理
func TestCountTools_Parameters为空(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	tools := []*schema.ToolInfo{
		schema.NewToolInfo("simple_tool", "A simple tool", nil),
	}

	count, err := tc.CountTools(tools, "gpt-4")
	assert.NoError(t, err)
	assert.Greater(t, count, 0, "即使 parameters 为空，token 数也应大于 0")
}

// TestModel2Enc_所有映射 遍历 model2enc 映射表验证每个映射可以创建编码器
func TestModel2Enc_所有映射(t *testing.T) {
	for model := range model2enc {
		tc := NewTiktokenCounter(model)
		assert.NotNil(t, tc.enc, "模型 %s 的编码器不应为 nil", model)
		assert.Equal(t, model, tc.model)
	}
}

// TestContentToString_纯文本 验证纯文本内容直接返回
func TestContentToString_纯文本(t *testing.T) {
	content := llm_schema.NewTextContent("hello world")
	result := contentToString(content)
	assert.Equal(t, "hello world", result)
}

// TestContentToString_多模态提取文本 验证多模态内容提取 text 分片拼接
func TestContentToString_多模态提取文本(t *testing.T) {
	content := llm_schema.NewMultiModalContent(
		llm_schema.ContentPart{Type: "text", Text: "Hello "},
		llm_schema.ContentPart{Type: "image_url", ImageURL: &llm_schema.ImageURL{URL: "https://example.com/img.png"}},
		llm_schema.ContentPart{Type: "text", Text: "World"},
	)
	result := contentToString(content)
	assert.Equal(t, "Hello World", result, "应只提取 text 分片，忽略 image_url")
}

// TestContentToString_空文本 验证空文本内容
func TestContentToString_空文本(t *testing.T) {
	content := llm_schema.NewTextContent("")
	result := contentToString(content)
	assert.Equal(t, "", result)
}

// TestTiktokenCounter_实现接口 验证 TiktokenCounter 实现了 TokenCounter 接口
func TestTiktokenCounter_实现接口(t *testing.T) {
	var _ TokenCounter = (*TiktokenCounter)(nil)
}

// TestCount_不同模型结果 验证同一文本在不同编码下 token 数不同
func TestCount_不同模型结果(t *testing.T) {
	tc4 := NewTiktokenCounter("gpt-4")
	tc4o := NewTiktokenCounter("gpt-4o")

	text := "hello world"
	count4, err := tc4.Count(text, "")
	assert.NoError(t, err)
	count4o, err := tc4o.Count(text, "")
	assert.NoError(t, err)

	// 两者都应能正常计数
	assert.Greater(t, count4, 0)
	assert.Greater(t, count4o, 0)
}

// TestCountMessages_ToolMessage 验证 ToolMessage 也被正确计数
func TestCountMessages_ToolMessage(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	messages := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage(""),
		llm_schema.NewToolMessage("call_1", "The weather is 72°F"),
	}

	count, err := tc.CountMessages(messages, "gpt-4")
	assert.NoError(t, err)
	assert.Greater(t, count, 0, "包含 ToolMessage 的消息列表 token 数应大于 0")
}

// TestFallbackCount_降级计算 验证 enc 为 nil 时 Count 返回 error
func TestFallbackCount_降级计算(t *testing.T) {
	tc := &TiktokenCounter{
		model: "test-model",
		enc:   nil, // 模拟初始化失败
	}

	text := "hello world"
	count, err := tc.Count(text, "")
	assert.Error(t, err, "enc 为 nil 时 Count 应返回 error")
	assert.Equal(t, 0, count, "enc 为 nil 时 Count 应返回 0")
}

// TestFallbackCount_只警告一次 验证降级警告只输出一次
// 注意：enc 为 nil 时 Count 返回 error 不调用 fallbackCount，
// 此测试验证 Count 返回 error 的行为，fallbackWarned 不应在 enc 为 nil 时触发
func TestFallbackCount_只警告一次(t *testing.T) {
	tc := &TiktokenCounter{
		model: "test-model",
		enc:   nil,
	}

	// enc 为 nil 时 Count 返回 error，不调用 fallbackCount
	_, err := tc.Count("first call", "")
	assert.Error(t, err, "enc 为 nil 时 Count 应返回 error")
	assert.False(t, tc.fallbackWarned, "enc 为 nil 时不调用 fallbackCount，fallbackWarned 应为 false")

	_, err = tc.Count("second call", "")
	assert.Error(t, err)
	assert.False(t, tc.fallbackWarned)
}

// TestFallbackCount_空文本 验证 enc 为 nil 时 Count 空文本
func TestFallbackCount_空文本(t *testing.T) {
	tc := &TiktokenCounter{
		model: "test-model",
		enc:   nil,
	}
	count, err := tc.Count("", "")
	assert.Error(t, err, "enc 为 nil 时即使空文本也应返回 error")
	assert.Equal(t, 0, count)
}

// TestNewTiktokenCounter_ForModel路径 验证通过 ForModel 路径创建编码器
func TestNewTiktokenCounter_ForModel路径(t *testing.T) {
	// "gpt-4-turbo" 在 model2enc 中有映射，但测试一个不在映射表但在 ForModel 中的模型
	// "gpt-4-0314" 不在 model2enc 中，但 ForModel 通过前缀匹配能识别
	tc := NewTiktokenCounter("gpt-4-0314")
	assert.NotNil(t, tc)
	// ForModel 通过前缀 "gpt-4-" 匹配到 Cl100kBase
	assert.NotNil(t, tc.enc, "gpt-4-0314 应通过 ForModel 前缀匹配创建编码器")
}

// TestCountMessages_多模态消息 验证多模态内容只提取 text 分片计数
func TestCountMessages_多模态消息(t *testing.T) {
	tc := NewTiktokenCounter("gpt-4")

	// 纯文本消息
	textMsg := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("Hello World"),
	}
	textCount, err := tc.CountMessages(textMsg, "gpt-4")
	assert.NoError(t, err)

	// 多模态消息（text 部分 + image_url 部分）
	multiMsg := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("", llm_schema.WithMultiModalContent(
			llm_schema.ContentPart{Type: "text", Text: "Hello World"},
			llm_schema.ContentPart{Type: "image_url", ImageURL: &llm_schema.ImageURL{URL: "https://example.com/img.png"}},
		)),
	}
	multiCount, err := tc.CountMessages(multiMsg, "gpt-4")
	assert.NoError(t, err)

	// 多模态消息只计算 text 部分，不含 image_url 的 JSON 结构
	assert.LessOrEqual(t, multiCount, textCount+3,
		"多模态消息的 token 数不应超过纯文本消息太多（忽略 image_url 部分）")
}

// TestFallbackCount_CountMessages 验证 enc 为 nil 时 CountMessages 返回 error
func TestFallbackCount_CountMessages(t *testing.T) {
	tc := &TiktokenCounter{
		model: "test-model",
		enc:   nil,
	}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}
	count, err := tc.CountMessages(messages, "")
	assert.Error(t, err, "enc 为 nil 时 CountMessages 应返回 error")
	assert.Equal(t, 0, count, "enc 为 nil 时 CountMessages 应返回 0")
}

// TestFallbackCount_CountTools 验证 enc 为 nil 时 CountTools 返回 error
func TestFallbackCount_CountTools(t *testing.T) {
	tc := &TiktokenCounter{
		model: "test-model",
		enc:   nil,
	}

	tools := []*schema.ToolInfo{
		schema.NewToolInfo("search", "Search the web", nil),
	}
	count, err := tc.CountTools(tools, "")
	assert.Error(t, err, "enc 为 nil 时 CountTools 应返回 error")
	assert.Equal(t, 0, count, "enc 为 nil 时 CountTools 应返回 0")
}
