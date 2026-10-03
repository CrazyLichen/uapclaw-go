package extract

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	storeindex "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewGenerator 测试创建 Generator
func TestNewGenerator(t *testing.T) {
	dataIdMgr := mem_model.NewDataIdManager()
	g := NewGenerator(dataIdMgr, nil)
	assert.NotNil(t, g)
	assert.Equal(t, dataIdMgr, g.dataIdGenerator)
	assert.Nil(t, g.searchManager)
}

// TestProcessExtractedData_正常转换 测试 VariableResult → VariableUnit
func TestProcessExtractedData_正常转换(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	variables := []VariableResult{
		{VariableKey: "name", VariableValue: "张三"},
		{VariableKey: "age", VariableValue: "25"},
	}
	result := g.processExtractedData("user1", variables)
	require.Len(t, result, 2)
	assert.Equal(t, "name", result[0].VariableName)
	assert.Equal(t, "张三", result[0].VariableMem)
	assert.Equal(t, "age", result[1].VariableName)
	assert.Equal(t, "25", result[1].VariableMem)
	assert.Equal(t, mem_model.MemoryTypeVariable, result[0].MemType)
	assert.NotEmpty(t, result[0].MemID)
}

// TestProcessExtractedData_空值跳过 测试 VariableValue 为空时跳过
func TestProcessExtractedData_空值跳过(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	variables := []VariableResult{
		{VariableKey: "name", VariableValue: "张三"},
		{VariableKey: "empty", VariableValue: ""},
	}
	result := g.processExtractedData("user1", variables)
	assert.Len(t, result, 1)
	assert.Equal(t, "name", result[0].VariableName)
}

// TestProcessExtractedData_空列表 测试空输入返回空列表
func TestProcessExtractedData_空列表(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	result := g.processExtractedData("user1", []VariableResult{})
	assert.Empty(t, result)
}

// TestProcessSummaryData 测试生成摘要记忆单元
func TestProcessSummaryData(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	unit := g.processSummaryData("user1", "msg1", "这是摘要", "2025-01-01T00:00:00Z")
	assert.Equal(t, "这是摘要", unit.Summary)
	assert.Equal(t, "msg1", unit.MessageMemID)
	assert.Equal(t, "2025-01-01T00:00:00Z", unit.Timestamp)
	assert.NotEmpty(t, unit.MemID)
	assert.Equal(t, mem_model.MemoryTypeSummary, unit.MemType)
}

// TestGetFragmentMemoryUnit_正常 测试正常碎片记忆生成
func TestGetFragmentMemoryUnit_正常(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryDict := map[string]any{
		"user_profile":    []any{"用户喜欢Python"},
		"semantic_memory": []any{"项目使用Go语言"},
		"unknown_type":    []any{"忽略此项"},
	}
	result, err := g.getFragmentMemoryUnit("user1", "msg1", memoryDict, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Len(t, result, 2)
	// map 遍历顺序不确定，按类型收集验证
	resultMap := map[string]string{}
	for _, u := range result {
		resultMap[u.MemType.String()] = u.Content
		assert.Equal(t, mem_model.OperationTypeAdd, u.OperationType)
	}
	assert.Equal(t, "用户喜欢Python", resultMap["user_profile"])
	assert.Equal(t, "项目使用Go语言", resultMap["semantic_memory"])
}

// TestGetFragmentMemoryUnit_非string内容 测试非 string 内容尝试 .content 字段
func TestGetFragmentMemoryUnit_非string内容(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryDict := map[string]any{
		"user_profile": []any{map[string]any{"content": "嵌套内容"}},
	}
	result, err := g.getFragmentMemoryUnit("user1", "msg1", memoryDict, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "嵌套内容", result[0].Content)
}

// TestGetFragmentMemoryUnit_非string无content字段 测试非 string 且无 content 字段时 fallback
func TestGetFragmentMemoryUnit_非string无content字段(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryDict := map[string]any{
		"user_profile": []any{map[string]any{"other": "值"}},
	}
	result, err := g.getFragmentMemoryUnit("user1", "msg1", memoryDict, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Len(t, result, 1)
	// fallback 到 fmt.Sprint
	assert.NotEmpty(t, result[0].Content)
}

// TestGetFragmentMemoryUnit_空dict 测试空 memoryDict
func TestGetFragmentMemoryUnit_空dict(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	result, err := g.getFragmentMemoryUnit("user1", "msg1", map[string]any{}, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Empty(t, result)
}

// TestProcessProactiveMemoryData_仅ADD 测试仅处理 ADD 操作
func TestProcessProactiveMemoryData_仅ADD(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryList := []any{
		map[string]any{"mem_instruct": "add", "mem_type": "user_profile", "mem_content": "新增内容"},
		map[string]any{"mem_instruct": "delete", "mem_type": "user_profile", "mem_content": "忽略"},
		map[string]any{"mem_instruct": "update", "mem_type": "user_profile", "mem_content": "也忽略"},
	}
	result, err := g.processProactiveMemoryData("user1", "msg1", memoryList, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "新增内容", result[0].Content)
	assert.Equal(t, mem_model.OperationTypeAdd, result[0].OperationType)
}

// TestProcessProactiveMemoryData_非dict跳过 测试非 dict 元素跳过
func TestProcessProactiveMemoryData_非dict跳过(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryList := []any{
		"not a dict",
		42,
		map[string]any{"mem_instruct": "add", "mem_type": "user_profile", "mem_content": "有效"},
	}
	result, err := g.processProactiveMemoryData("user1", "msg1", memoryList, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

// TestProcessProactiveMemoryData_未知类型跳过 测试未知 mem_type 跳过
func TestProcessProactiveMemoryData_未知类型跳过(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryList := []any{
		map[string]any{"mem_instruct": "add", "mem_type": "unknown_type", "mem_content": "忽略"},
	}
	result, err := g.processProactiveMemoryData("user1", "msg1", memoryList, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Empty(t, result)
}

// TestProcessProactiveMemoryData_无memContent跳过 测试无 mem_content 时跳过
func TestProcessProactiveMemoryData_无memContent跳过(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryList := []any{
		map[string]any{"mem_instruct": "add", "mem_type": "user_profile"},
	}
	result, err := g.processProactiveMemoryData("user1", "msg1", memoryList, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Empty(t, result)
}

// TestHandleMemoryWithInstruct_分离UPDATE和DELETE 测试分离 UPDATE 和 DELETE 操作
func TestHandleMemoryWithInstruct_分离UPDATE和DELETE(t *testing.T) {
	// 此测试仅验证映射常量的正确性，
	// 不依赖真实 SearchManager（processMemoryCommands 需要 searchManager 为 nil 时会返回空）
	// 所以只测试方法本身的数据分组逻辑

	// 验证 categoryToClass 映射
	assert.Equal(t, mem_model.MemoryTypeUserProfile, categoryToClass["user_profile"])
	assert.Equal(t, mem_model.MemoryTypeSemanticMemory, categoryToClass["semantic_memory"])
	assert.Equal(t, mem_model.MemoryTypeEpisodicMemory, categoryToClass["episodic_memory"])

	// 验证 operationStrToEnum 映射
	assert.Equal(t, mem_model.OperationTypeAdd, operationStrToEnum["add"])
	assert.Equal(t, mem_model.OperationTypeUpdate, operationStrToEnum["update"])
	assert.Equal(t, mem_model.OperationTypeDelete, operationStrToEnum["delete"])
}

// TestCategoryToClass_未知类型 测试 categoryToClass 未知类型返回零值
func TestCategoryToClass_未知类型(t *testing.T) {
	_, ok := categoryToClass["unknown"]
	assert.False(t, ok)
}

// TestGenAllMemory_必填参数缺失 测试必填参数缺失时返回空 map
func TestGenAllMemory_必填参数缺失(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)

	tests := []struct {
		name     string
		userID   string
		scopeID  string
		messages []llmschema.BaseMessage
		model    *llm.Model
	}{
		{"缺少userID", "", "scope1", []llmschema.BaseMessage{llmschema.NewUserMessage("test")}, newFakeModelWithResponse(t, `{}`)},
		{"缺少scopeID", "user1", "", []llmschema.BaseMessage{llmschema.NewUserMessage("test")}, newFakeModelWithResponse(t, `{}`)},
		{"缺少messages", "user1", "scope1", []llmschema.BaseMessage{}, newFakeModelWithResponse(t, `{}`)},
		{"缺少model", "user1", "scope1", []llmschema.BaseMessage{llmschema.NewUserMessage("test")}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := &GenAllMemoryParams{
				Messages:     tt.messages,
				BaseModel:    tt.model,
				MemoryConfig: config.DefaultAgentMemoryConfig(),
				UserID:       tt.userID,
				ScopeID:      tt.scopeID,
			}
			result, err := g.GenAllMemory(context.Background(), params)
			require.NoError(t, err)
			assert.Empty(t, result)
		})
	}
}

// TestGenAllMemory_关闭长期记忆 测试 EnableLongTermMem=false 时只返回变量
func TestGenAllMemory_关闭长期记忆(t *testing.T) {
	analyzerJSON := `{
		"has_key_information": true,
		"variables": [{"variable_key": "name", "variable_value": "张三"}],
		"summary": "测试摘要"
	}`
	model := newFakeModelWithResponse(t, analyzerJSON)

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables: []commonschema.Param{
			*commonschema.NewStringParam("name", "用户姓名", true),
		},
		EnableLongTermMem:    false,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}

	params := &GenAllMemoryParams{
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("我叫张三")},
		BaseModel:       model,
		MemoryConfig:    memoryConfig,
		UserID:          "user1",
		ScopeID:         "scope1",
		MessageMemID:    "msg1",
		Timestamp:       "2025-01-01T00:00:00Z",
		SummaryMaxToken: 128,
	}

	result, err := g.GenAllMemory(context.Background(), params)
	require.NoError(t, err)
	// 应该只有 variable 类型
	varType := mem_model.MemoryTypeVariable.String()
	assert.Contains(t, result, varType)
	// 不应包含 summary（因为 EnableLongTermMem=false）
	summaryType := mem_model.MemoryTypeSummary.String()
	assert.NotContains(t, result, summaryType)
}

// TestGenAllMemory_无关键信息 测试 has_key_information=false 时不生成碎片记忆
func TestGenAllMemory_无关键信息(t *testing.T) {
	analyzerJSON := `{
		"has_key_information": false,
		"variables": [],
		"summary": "测试摘要"
	}`
	model := newFakeModelWithResponse(t, analyzerJSON)

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables:         []commonschema.Param{},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}

	params := &GenAllMemoryParams{
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("今天天气不错")},
		BaseModel:       model,
		MemoryConfig:    memoryConfig,
		UserID:          "user1",
		ScopeID:         "scope1",
		MessageMemID:    "msg1",
		Timestamp:       "2025-01-01T00:00:00Z",
		SummaryMaxToken: 128,
	}

	result, err := g.GenAllMemory(context.Background(), params)
	require.NoError(t, err)
	// 应包含 summary
	summaryType := mem_model.MemoryTypeSummary.String()
	assert.Contains(t, result, summaryType)
	// 不应包含碎片记忆类型
	userProfileType := mem_model.MemoryTypeUserProfile.String()
	assert.NotContains(t, result, userProfileType)
}

// TestGenAllMemory_摘要记忆关闭 测试 EnableSummaryMemory=false 时不生成摘要
func TestGenAllMemory_摘要记忆关闭(t *testing.T) {
	analyzerJSON := `{
		"has_key_information": false,
		"variables": [],
		"summary": "测试摘要"
	}`
	model := newFakeModelWithResponse(t, analyzerJSON)

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables:         []commonschema.Param{},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  false,
	}

	params := &GenAllMemoryParams{
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		BaseModel:       model,
		MemoryConfig:    memoryConfig,
		UserID:          "user1",
		ScopeID:         "scope1",
		MessageMemID:    "msg1",
		Timestamp:       "2025-01-01T00:00:00Z",
		SummaryMaxToken: 128,
	}

	result, err := g.GenAllMemory(context.Background(), params)
	require.NoError(t, err)
	summaryType := mem_model.MemoryTypeSummary.String()
	assert.NotContains(t, result, summaryType)
}

// TestGenAllMemory_部分fragment关闭 测试关闭部分碎片记忆类型时的过滤
func TestGenAllMemory_部分fragment关闭(t *testing.T) {
	callCount := 0
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			callCount++
			if callCount == 1 {
				return llmschema.NewAssistantMessage(`{
					"has_key_information": true,
					"variables": [],
					"summary": "摘要"
				}`), nil
			}
			return llmschema.NewAssistantMessage(`{
				"has_explict_instruct": false,
				"instruct_memories": [],
				"user_profile": ["用户画像"],
				"semantic_memory": ["语义记忆"],
				"episodic_memory": ["情景记忆"]
			}`), nil
		},
	}
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, _ := llm.NewModel(clientCfg, nil, llm.WithClient(client))

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables:         []commonschema.Param{},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: false, // 关闭语义记忆
		EnableEpisodicMemory: false, // 关闭情景记忆
		EnableSummaryMemory:  true,
	}

	params := &GenAllMemoryParams{
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		BaseModel:       model,
		MemoryConfig:    memoryConfig,
		UserID:          "user1",
		ScopeID:         "scope1",
		MessageMemID:    "msg1",
		Timestamp:       "2025-01-01T00:00:00Z",
		SummaryMaxToken: 128,
	}

	result, err := g.GenAllMemory(context.Background(), params)
	require.NoError(t, err)
	// user_profile 开启
	assert.Contains(t, result, mem_model.MemoryTypeUserProfile.String())
	assert.Len(t, result[mem_model.MemoryTypeUserProfile.String()], 1)
	// semantic_memory 和 episodic_memory 关闭
	assert.NotContains(t, result, mem_model.MemoryTypeSemanticMemory.String())
	assert.NotContains(t, result, mem_model.MemoryTypeEpisodicMemory.String())
}

// TestGenAllMemory_完整流程 测试完整的 Generator 流程（含碎片记忆提取）
func TestGenAllMemory_完整流程(t *testing.T) {
	// 第一次调用 MemoryAnalyzer 返回，第二次调用 ExtractLongTermMemory 返回
	callCount := 0
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			callCount++
			if callCount == 1 {
				// MemoryAnalyzer 的响应
				return llmschema.NewAssistantMessage(`{
					"has_key_information": true,
					"variables": [{"variable_key": "name", "variable_value": "张三"}],
					"summary": "用户介绍了自己"
				}`), nil
			}
			// ExtractLongTermMemory 的响应
			return llmschema.NewAssistantMessage(`{
				"has_explict_instruct": false,
				"instruct_memories": [],
				"user_profile": ["用户喜欢编程"],
				"semantic_memory": ["项目使用Go"],
				"episodic_memory": []
			}`), nil
		},
	}

	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	require.NoError(t, err)
	model, err := llm.NewModel(clientCfg, nil, llm.WithClient(client))
	require.NoError(t, err)

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryConfig := &config.AgentMemoryConfig{
		MemVariables: []commonschema.Param{
			*commonschema.NewStringParam("name", "用户姓名", true),
		},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}

	params := &GenAllMemoryParams{
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("我叫张三，喜欢编程")},
		BaseModel:       model,
		MemoryConfig:    memoryConfig,
		UserID:          "user1",
		ScopeID:         "scope1",
		MessageMemID:    "msg1",
		Timestamp:       "2025-01-01T00:00:00Z",
		SummaryMaxToken: 128,
	}

	result, err := g.GenAllMemory(context.Background(), params)
	require.NoError(t, err)
	// 应包含 variable、summary、user_profile、semantic_memory
	assert.Contains(t, result, mem_model.MemoryTypeVariable.String())
	assert.Contains(t, result, mem_model.MemoryTypeSummary.String())
	assert.Contains(t, result, mem_model.MemoryTypeUserProfile.String())
	assert.Contains(t, result, mem_model.MemoryTypeSemanticMemory.String())
	// episodic_memory 为空列表，不应包含
	assert.NotContains(t, result, mem_model.MemoryTypeEpisodicMemory.String())
}

// TestCategoriesToMemoryUnit_无指令 测试 has_explict_instruct=false 时不调用 handleMemoryWithInstruct
func TestCategoriesToMemoryUnit_无指令(t *testing.T) {
	// ExtractLongTermMemory 返回无指令的结果
	extractJSON := `{
		"has_explict_instruct": false,
		"instruct_memories": [],
		"user_profile": ["用户画像信息"],
		"semantic_memory": [],
		"episodic_memory": []
	}`
	model := newFakeModelWithResponse(t, extractJSON)

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		BaseChatModel:   model,
	}

	result, err := g.categoriesToMemoryUnit(
		context.Background(),
		params,
		"msg1",
		"2025-01-01T00:00:00Z",
		nil,
		nil,
	)
	require.NoError(t, err)
	// 应包含 user_profile 碎片
	assert.NotEmpty(t, result)
	foundUserProfile := false
	for _, u := range result {
		if u.GetMemType() == mem_model.MemoryTypeUserProfile {
			foundUserProfile = true
		}
	}
	assert.True(t, foundUserProfile)
}

// TestSemanticValidation_CORRECT 测试语义校验返回 CORRECT
func TestSemanticValidation_CORRECT(t *testing.T) {
	// LLM 返回 CORRECT
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			return llmschema.NewAssistantMessage("The result is CORRECT"), nil
		},
	}
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, _ := llm.NewModel(clientCfg, nil, llm.WithClient(client))

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	obtainedMems := []*storeindex.MemorySearchResult{
		{
			Doc:   &storeindex.MemoryDoc{ID: "mem123", Text: "用户喜欢编程"},
			Score: 0.9,
		},
	}

	result := g.semanticValidation(context.Background(), obtainedMems, "用户喜欢编程", model)
	assert.Len(t, result, 1)
	assert.Equal(t, "mem123", result[0].ID)
	assert.Equal(t, "用户喜欢编程", result[0].Mem)
}

// TestSemanticValidation_WRONG 测试语义校验返回 WRONG
func TestSemanticValidation_WRONG(t *testing.T) {
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			return llmschema.NewAssistantMessage("The result is WRONG"), nil
		},
	}
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, _ := llm.NewModel(clientCfg, nil, llm.WithClient(client))

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	obtainedMems := []*storeindex.MemorySearchResult{
		{
			Doc:   &storeindex.MemoryDoc{ID: "mem123", Text: "用户喜欢编程"},
			Score: 0.9,
		},
	}

	result := g.semanticValidation(context.Background(), obtainedMems, "完全不同的内容", model)
	assert.Empty(t, result)
}

// TestSemanticValidation_LLM错误 测试 LLM 调用失败时跳过
func TestSemanticValidation_LLM错误(t *testing.T) {
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			return nil, fmt.Errorf("API 调用失败")
		},
	}
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, _ := llm.NewModel(clientCfg, nil, llm.WithClient(client))

	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	obtainedMems := []*storeindex.MemorySearchResult{
		{
			Doc:   &storeindex.MemoryDoc{ID: "mem123", Text: "用户喜欢编程"},
			Score: 0.9,
		},
	}

	result := g.semanticValidation(context.Background(), obtainedMems, "用户喜欢编程", model)
	assert.Empty(t, result)
}

// TestSemanticValidation_空列表 测试空候选列表
func TestSemanticValidation_空列表(t *testing.T) {
	model := newFakeModelWithResponse(t, "CORRECT")
	g := NewGenerator(mem_model.NewDataIdManager(), nil)

	result := g.semanticValidation(context.Background(), nil, "用户喜欢编程", model)
	assert.Empty(t, result)
}

// TestHandleMemoryWithInstruct_空列表 测试空 instruct 列表
func TestHandleMemoryWithInstruct_空列表(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryOperationParams := &MemoryOperationParams{
		UserID:    "user1",
		ScopeID:   "scope1",
		BaseModel: newFakeModelWithResponse(t, "CORRECT"),
	}

	result := g.handleMemoryWithInstruct(context.Background(), memoryOperationParams, []any{})
	assert.Empty(t, result)
}

// TestHandleMemoryWithInstruct_有UPDATE和DELETE 测试分离 UPDATE 和 DELETE 操作（searchManager 为 nil）
func TestHandleMemoryWithInstruct_有UPDATE和DELETE(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryOperationParams := &MemoryOperationParams{
		UserID:       "user1",
		ScopeID:      "scope1",
		MessageMemID: "msg1",
		Timestamp:    "2025-01-01T00:00:00Z",
		BaseModel:    newFakeModelWithResponse(t, "CORRECT"),
	}
	memoryList := []any{
		map[string]any{"mem_instruct": "update", "mem_type": "user_profile", "mem_content": "更新内容", "old_mem": "旧内容"},
		map[string]any{"mem_instruct": "delete", "mem_type": "user_profile", "mem_content": "删除内容", "old_mem": "旧内容"},
		map[string]any{"mem_instruct": "add", "mem_type": "user_profile", "mem_content": "新增忽略"}, // 非 update/delete
		"not a dict",
	}
	// searchManager 为 nil，processMemoryOperations 返回空，但 handleMemoryWithInstruct 的分组逻辑会被执行
	result := g.handleMemoryWithInstruct(context.Background(), memoryOperationParams, memoryList)
	assert.Empty(t, result) // searchManager 为 nil 时返回空
}

// TestProcessMemoryOperations_searchManager为nil 测试 searchManager 为 nil 时返回空
func TestProcessMemoryOperations_searchManager为nil(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryOperationParams := &MemoryOperationParams{
		UserID:       "user1",
		ScopeID:      "scope1",
		MessageMemID: "msg1",
		Timestamp:    "2025-01-01T00:00:00Z",
		BaseModel:    newFakeModelWithResponse(t, "CORRECT"),
	}
	memoryDicts := []map[string]any{
		{"old_mem": "旧内容", "mem_type": "user_profile", "mem_content": "新内容"},
	}

	result := g.processMemoryOperations(context.Background(), memoryOperationParams, memoryDicts, mem_model.OperationTypeUpdate)
	assert.Empty(t, result)
}

// TestProcessMemoryOperations_无OldMem 测试 old_mem 为空时跳过
func TestProcessMemoryOperations_无OldMem(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryOperationParams := &MemoryOperationParams{
		UserID:       "user1",
		ScopeID:      "scope1",
		MessageMemID: "msg1",
		Timestamp:    "2025-01-01T00:00:00Z",
		BaseModel:    newFakeModelWithResponse(t, "CORRECT"),
	}
	memoryDicts := []map[string]any{
		{"mem_type": "user_profile", "mem_content": "新内容"},                // 缺少 old_mem
		{"old_mem": "", "mem_type": "user_profile", "mem_content": "新内容"}, // old_mem 为空
	}

	result := g.processMemoryOperations(context.Background(), memoryOperationParams, memoryDicts, mem_model.OperationTypeUpdate)
	assert.Empty(t, result)
}

// TestCategoriesToMemoryUnit_有指令 测试 has_explict_instruct=true 时调用 handleMemoryWithInstruct
func TestCategoriesToMemoryUnit_有指令(t *testing.T) {
	// ExtractLongTermMemory 返回有指令的结果
	extractJSON := `{
		"has_explict_instruct": true,
		"instruct_memories": [{"mem_instruct": "update", "mem_type": "user_profile", "mem_content": "更新内容", "old_mem": "旧内容"}],
		"user_profile": ["用户画像信息"],
		"semantic_memory": [],
		"episodic_memory": []
	}`
	model := newFakeModelWithResponse(t, extractJSON)

	// searchManager 为 nil，handleMemoryWithInstruct 内部 processMemoryOperations 返回空
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		BaseChatModel:   model,
	}

	result, err := g.categoriesToMemoryUnit(
		context.Background(),
		params,
		"msg1",
		"2025-01-01T00:00:00Z",
		nil,
		nil,
	)
	require.NoError(t, err)
	// 即使指令式记忆处理返回空，仍应有 user_profile 碎片
	assert.NotEmpty(t, result)
}

// TestProcessProactiveMemoryData_非stringMemContent 测试 mem_content 非字符串时尝试 .mem_content 字段
func TestProcessProactiveMemoryData_非stringMemContent(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryList := []any{
		map[string]any{"mem_instruct": "add", "mem_type": "user_profile", "mem_content": map[string]any{"mem_content": "嵌套内容"}},
	}
	result, err := g.processProactiveMemoryData("user1", "msg1", memoryList, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "嵌套内容", result[0].Content)
}

// TestSortSearchResultsByScore 测试搜索结果按分数降序排序
func TestSortSearchResultsByScore(t *testing.T) {
	results := []*storeindex.MemorySearchResult{
		{Score: 0.3},
		{Score: 0.9},
		{Score: 0.5},
	}
	sortSearchResultsByScore(results)
	assert.Equal(t, 0.9, results[0].Score)
	assert.Equal(t, 0.5, results[1].Score)
	assert.Equal(t, 0.3, results[2].Score)
}

// TestSortSearchResultsByScore_空列表 测试空列表排序
func TestSortSearchResultsByScore_空列表(t *testing.T) {
	results := []*storeindex.MemorySearchResult{}
	sortSearchResultsByScore(results)
	assert.Empty(t, results)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
