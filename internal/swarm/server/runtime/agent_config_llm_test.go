package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 测试用例 ────────────────────────────

// TestParseLLMGenerationResponse_ValidJSON 测试解析有效的 JSON 响应
func TestParseLLMGenerationResponse_ValidJSON(t *testing.T) {
	text := `{"whenToUse": "Use this agent when you need to analyze data", "systemPrompt": "You are a data analysis expert."}`
	result := parseLLMGenerationResponse(text)
	assert.NotNil(t, result)
	assert.Equal(t, "Use this agent when you need to analyze data", result.WhenToUse)
	assert.Equal(t, "You are a data analysis expert.", result.SystemPrompt)
}

// TestParseLLMGenerationResponse_JSONWithExtraText 测试 JSON 前后有多余文本
func TestParseLLMGenerationResponse_JSONWithExtraText(t *testing.T) {
	text := `Here is the result:
{"whenToUse": "Use this agent when coding", "systemPrompt": "You are a coding expert."}
End of response.`
	result := parseLLMGenerationResponse(text)
	assert.NotNil(t, result)
	assert.Equal(t, "Use this agent when coding", result.WhenToUse)
	assert.Equal(t, "You are a coding expert.", result.SystemPrompt)
}

// TestParseLLMGenerationResponse_ChineseContent 测试中文内容
func TestParseLLMGenerationResponse_ChineseContent(t *testing.T) {
	text := `{"whenToUse": "当需要数据分析时使用此 agent", "systemPrompt": "你是一个数据分析专家。"}`
	result := parseLLMGenerationResponse(text)
	assert.NotNil(t, result)
	assert.Equal(t, "当需要数据分析时使用此 agent", result.WhenToUse)
	assert.Equal(t, "你是一个数据分析专家。", result.SystemPrompt)
}

// TestParseLLMGenerationResponse_MissingWhenToUse 测试缺少 whenToUse
func TestParseLLMGenerationResponse_MissingWhenToUse(t *testing.T) {
	text := `{"systemPrompt": "You are an expert."}`
	result := parseLLMGenerationResponse(text)
	assert.Nil(t, result)
}

// TestParseLLMGenerationResponse_MissingSystemPrompt 测试缺少 systemPrompt
func TestParseLLMGenerationResponse_MissingSystemPrompt(t *testing.T) {
	text := `{"whenToUse": "Use this agent when..."}`
	result := parseLLMGenerationResponse(text)
	assert.Nil(t, result)
}

// TestParseLLMGenerationResponse_EmptyWhenToUse 测试空 whenToUse
func TestParseLLMGenerationResponse_EmptyWhenToUse(t *testing.T) {
	text := `{"whenToUse": "", "systemPrompt": "You are an expert."}`
	result := parseLLMGenerationResponse(text)
	assert.Nil(t, result)
}

// TestParseLLMGenerationResponse_NoJSON 测试无 JSON 内容
func TestParseLLMGenerationResponse_NoJSON(t *testing.T) {
	text := `This is just plain text without any JSON.`
	result := parseLLMGenerationResponse(text)
	assert.Nil(t, result)
}

// TestParseLLMGenerationResponse_InvalidJSON 测试无效 JSON
func TestParseLLMGenerationResponse_InvalidJSON(t *testing.T) {
	text := `{this is not valid json}`
	result := parseLLMGenerationResponse(text)
	assert.Nil(t, result)
}

// TestParseLLMGenerationResponse_WhitespaceTrimmed 测试前后空格被去除
func TestParseLLMGenerationResponse_WhitespaceTrimmed(t *testing.T) {
	text := `  {"whenToUse": "  需要时使用  ", "systemPrompt": "  你是专家  "}  `
	result := parseLLMGenerationResponse(text)
	assert.NotNil(t, result)
	assert.Equal(t, "需要时使用", result.WhenToUse)
	assert.Equal(t, "你是专家", result.SystemPrompt)
}

// TestGenerateAgentWithLLM_NilModel 测试 nil model 返回 nil
func TestGenerateAgentWithLLM_NilModel(t *testing.T) {
	result := GenerateAgentWithLLM(context.TODO(), nil, "test", "desc")
	assert.Nil(t, result)
}

// TestGenerateAgentWithLLM_EmptyName 测试空名称返回 nil
func TestGenerateAgentWithLLM_EmptyName(t *testing.T) {
	result := GenerateAgentWithLLM(context.TODO(), nil, "", "desc")
	assert.Nil(t, result)
}

// TestGenerateAgentWithLLM_EmptyDescription 测试空描述返回 nil
func TestGenerateAgentWithLLM_EmptyDescription(t *testing.T) {
	result := GenerateAgentWithLLM(context.TODO(), nil, "test", "")
	assert.Nil(t, result)
}

// TestTruncate 测试 truncate 辅助函数
func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hel", truncate("hello", 3))
	assert.Equal(t, "", truncate("", 5))
}
