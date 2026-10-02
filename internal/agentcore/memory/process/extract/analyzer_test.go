package extract

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAnalyze_消息为空返回nil 测试 messages 为空时返回 nil
func TestAnalyze_消息为空返回nil(t *testing.T) {
	model := newFakeModelWithResponse(t, `{}`)

	memoryConfig := config.DefaultAgentMemoryConfig()

	result, err := Analyze(
		context.Background(),
		[]llmschema.BaseMessage{}, // 空 messages
		nil,
		model,
		memoryConfig,
		128,
		nil,
		"",
		3,
	)

	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestAnalyze_正常分析返回结果 测试正常 LLM 返回有效 JSON
func TestAnalyze_正常分析返回结果(t *testing.T) {
	jsonResponse := `{
		"has_key_information": true,
		"variables": [
			{"variable_key": "name", "variable_value": "张三"},
			{"variable_key": "age", "variable_value": "25"}
		],
		"summary": "用户介绍了自己的姓名和年龄"
	}`

	model := newFakeModelWithResponse(t, jsonResponse)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables: []commonschema.Param{
			*commonschema.NewStringParam("name", "用户姓名", true),
			*commonschema.NewStringParam("age", "用户年龄", true),
		},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}

	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("我叫张三，今年25岁"),
	}
	historyMessages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("你好！有什么可以帮你的？"),
	}

	result, err := Analyze(
		context.Background(),
		messages,
		historyMessages,
		model,
		memoryConfig,
		128,
		nil,
		"",
		3,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.HasKeyInformation)
	assert.Len(t, result.Variables, 2)
	assert.Equal(t, "name", result.Variables[0].VariableKey)
	assert.Equal(t, "张三", result.Variables[0].VariableValue)
	assert.Equal(t, "age", result.Variables[1].VariableKey)
	assert.Equal(t, "25", result.Variables[1].VariableValue)
	assert.Equal(t, "用户介绍了自己的姓名和年龄", result.Summary)
}

// TestAnalyze_LLM解析失败返回空结果 测试 LLM 返回非法 JSON 时重试后返回空 MemoryAnalyzerResult
func TestAnalyze_LLM解析失败返回空结果(t *testing.T) {
	model := newFakeModelWithResponse(t, "not valid json {{{")

	memoryConfig := config.DefaultAgentMemoryConfig()

	result, err := Analyze(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		nil,
		model,
		memoryConfig,
		128,
		nil,
		"",
		2,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	// 返回空 MemoryAnalyzerResult（对齐 Python: return MemoryAnalyzerResult()）
	assert.False(t, result.HasKeyInformation)
	assert.Empty(t, result.Variables)
	assert.Empty(t, result.Summary)
}

// TestAnalyze_Invoke错误向上传播 测试 LLM Invoke 错误直接返回
func TestAnalyze_Invoke错误向上传播(t *testing.T) {
	model := newFakeModelWithInvokeErr(fmt.Errorf("API 调用失败"))

	memoryConfig := config.DefaultAgentMemoryConfig()

	result, err := Analyze(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		nil,
		model,
		memoryConfig,
		128,
		nil,
		"",
		3,
	)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "记忆分析 LLM 调用失败")
}

// TestAnalyze_关闭长期记忆时清空摘要 测试 EnableLongTermMem=false 时 Summary 被清空
func TestAnalyze_关闭长期记忆时清空摘要(t *testing.T) {
	jsonResponse := `{
		"has_key_information": true,
		"variables": [],
		"summary": "这是一段摘要"
	}`

	model := newFakeModelWithResponse(t, jsonResponse)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables:         []commonschema.Param{},
		EnableLongTermMem:    false, // 关闭长期记忆
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}

	result, err := Analyze(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		nil,
		model,
		memoryConfig,
		128,
		nil,
		"",
		3,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	// 对齐 Python: if not memory_config.enable_long_term_mem or not memory_config.enable_summary_memory: analyze_result.summary = ""
	assert.Equal(t, "", result.Summary)
}

// TestAnalyze_关闭摘要记忆时清空摘要 测试 EnableSummaryMemory=false 时 Summary 被清空
func TestAnalyze_关闭摘要记忆时清空摘要(t *testing.T) {
	jsonResponse := `{
		"has_key_information": true,
		"variables": [],
		"summary": "这是一段摘要"
	}`

	model := newFakeModelWithResponse(t, jsonResponse)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables:         []commonschema.Param{},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  false, // 关闭摘要记忆
	}

	result, err := Analyze(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		nil,
		model,
		memoryConfig,
		128,
		nil,
		"",
		3,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "", result.Summary)
}

// TestAnalyze_scopeConfig为nil时使用默认值 测试 scopeConfig=nil 时不 panic，使用空定义
func TestAnalyze_scopeConfig为nil时使用默认值(t *testing.T) {
	jsonResponse := `{
		"has_key_information": false,
		"variables": [],
		"summary": ""
	}`

	model := newFakeModelWithResponse(t, jsonResponse)
	memoryConfig := config.DefaultAgentMemoryConfig()

	result, err := Analyze(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		[]llmschema.BaseMessage{llmschema.NewUserMessage("历史消息")}, // 有历史消息
		model,
		memoryConfig,
		128,
		nil, // scopeConfig 为 nil
		"", // 空禁止变量
		3,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestAnalyze_有变量配置 测试有 mem_variables 时构建 variables JSON
func TestAnalyze_有变量配置(t *testing.T) {
	jsonResponse := `{
		"has_key_information": true,
		"variables": [{"variable_key": "hobby", "variable_value": "编程"}],
		"summary": "用户提到自己的爱好"
	}`

	model := newFakeModelWithResponse(t, jsonResponse)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables: []commonschema.Param{
			*commonschema.NewStringParam("hobby", "用户爱好", true),
		},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}

	result, err := Analyze(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("我喜欢编程")},
		nil,
		model,
		memoryConfig,
		256,
		&config.MemoryScopeConfig{
			UserProfileDefinition:    "用户画像定义",
			SemanticMemoryDefinition: "语义记忆定义",
			EpisodicMemoryDefinition: "情景记忆定义",
		},
		"password", // 禁止变量
		3,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.HasKeyInformation)
	assert.Len(t, result.Variables, 1)
	assert.Equal(t, "hobby", result.Variables[0].VariableKey)
	assert.Equal(t, "编程", result.Variables[0].VariableValue)
}

// TestAnalyze_forbiddenVariables空字符串映射为None 测试空字符串映射
func TestAnalyze_forbiddenVariables空字符串映射为None(t *testing.T) {
	jsonResponse := `{
		"has_key_information": false,
		"variables": [],
		"summary": ""
	}`

	model := newFakeModelWithResponse(t, jsonResponse)
	memoryConfig := config.DefaultAgentMemoryConfig()

	result, err := Analyze(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		nil,
		model,
		memoryConfig,
		128,
		nil,
		"", // 空字符串 → 模板中 "None"
		3,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestMapToMemoryAnalyzerResult_正常解析 测试 map 转 MemoryAnalyzerResult
func TestMapToMemoryAnalyzerResult_正常解析(t *testing.T) {
	m := map[string]any{
		"has_key_information": true,
		"variables": []any{
			map[string]any{"variable_key": "name", "variable_value": "张三"},
		},
		"summary": "测试摘要",
	}

	result, err := mapToMemoryAnalyzerResult(m)
	require.NoError(t, err)
	assert.True(t, result.HasKeyInformation)
	assert.Len(t, result.Variables, 1)
	assert.Equal(t, "张三", result.Variables[0].VariableValue)
	assert.Equal(t, "测试摘要", result.Summary)
}

// TestMapToMemoryAnalyzerResult_缺少字段使用零值 测试缺少字段时使用零值
func TestMapToMemoryAnalyzerResult_缺少字段使用零值(t *testing.T) {
	m := map[string]any{}

	result, err := mapToMemoryAnalyzerResult(m)
	require.NoError(t, err)
	assert.False(t, result.HasKeyInformation)
	assert.Empty(t, result.Variables)
	assert.Empty(t, result.Summary)
}

// TestMapToMemoryAnalyzerResult_variables类型错误 测试 variables 字段类型错误时不崩溃
func TestMapToMemoryAnalyzerResult_variables类型错误(t *testing.T) {
	m := map[string]any{
		"has_key_information": true,
		"variables":           "not an array",
	}

	result, err := mapToMemoryAnalyzerResult(m)
	require.NoError(t, err)
	assert.True(t, result.HasKeyInformation)
	assert.Empty(t, result.Variables)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
