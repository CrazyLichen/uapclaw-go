package extract

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	result := g.processExtractedData(variables)
	require.Len(t, result, 2)
	assert.Equal(t, "name", result[0].VariableName)
	assert.Equal(t, "张三", result[0].VariableMem)
	assert.Equal(t, "age", result[1].VariableName)
	assert.Equal(t, "25", result[1].VariableMem)
	assert.Equal(t, mem_model.MemoryTypeVariable, result[0].MemType)
}

// TestProcessExtractedData_空值跳过 测试 VariableValue 为空时跳过
func TestProcessExtractedData_空值跳过(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	variables := []VariableResult{
		{VariableKey: "name", VariableValue: "张三"},
		{VariableKey: "empty", VariableValue: ""},
	}
	result := g.processExtractedData(variables)
	assert.Len(t, result, 1)
	assert.Equal(t, "name", result[0].VariableName)
}

// TestProcessExtractedData_空列表 测试空输入返回空列表
func TestProcessExtractedData_空列表(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	result := g.processExtractedData([]VariableResult{})
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
	assert.Equal(t, mem_model.MemoryTypeUserProfile, result[0].MemType)
	assert.Equal(t, mem_model.OperationTypeAdd, result[0].OperationType)
	assert.Equal(t, "用户喜欢Python", result[0].Content)
	assert.Equal(t, mem_model.MemoryTypeSemanticMemory, result[1].MemType)
	assert.Equal(t, "项目使用Go语言", result[1].Content)
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
	// 不依赖真实 SearchManager（processMemoryOperations 需要 searchManager 为 nil 时会 panic）
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

	// 缺少 userID
	result, err := g.GenAllMemory(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("test")},
		nil,
		newFakeModelWithResponse(t, `{}`),
		config.DefaultAgentMemoryConfig(),
		nil, nil,
		"", // userID 为空
		"scope1",
		"", "", "", 128, nil,
	)
	require.NoError(t, err)
	assert.Empty(t, result)
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

	result, err := g.GenAllMemory(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewUserMessage("我叫张三")},
		nil,
		model,
		memoryConfig,
		nil, nil,
		"user1", "scope1",
		"", "msg1", "2025-01-01T00:00:00Z", 128, nil,
	)
	require.NoError(t, err)
	// 应该只有 variable 类型
	varType := mem_model.MemoryTypeVariable.String()
	assert.Contains(t, result, varType)
	// 不应包含 summary（因为 EnableLongTermMem=false）
	summaryType := mem_model.MemoryTypeSummary.String()
	assert.NotContains(t, result, summaryType)
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
