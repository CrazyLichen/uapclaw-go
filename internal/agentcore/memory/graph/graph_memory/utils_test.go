package graph_memory

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── Msg2Dict 测试 ────────────────────────────

func TestMsg2Dict_BaseMessage列表_不保留元数据(t *testing.T) {
	msgs := []schema.BaseMessage{
		schema.NewUserMessage("hello"),
		schema.NewSystemMessage("system prompt"),
	}
	result, err := Msg2Dict(msgs, false)
	assert.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "user", result[0]["role"])
	assert.Equal(t, "hello", result[0]["content"])
	assert.Equal(t, "system", result[1]["role"])
	assert.Equal(t, "system prompt", result[1]["content"])
	// 不保留元数据时不应有 name/metadata 字段
	_, hasName := result[0]["name"]
	assert.False(t, hasName)
}

func TestMsg2Dict_BaseMessage列表_保留元数据(t *testing.T) {
	msg := schema.NewUserMessage("hello",
		schema.WithMessageName("test_user"),
		schema.WithMetadata(map[string]any{"key": "value"}),
	)
	msgs := []schema.BaseMessage{msg}
	result, err := Msg2Dict(msgs, true)
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "user", result[0]["role"])
	assert.Equal(t, "hello", result[0]["content"])
	assert.Equal(t, "test_user", result[0]["name"])
	meta, ok := result[0]["metadata"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "value", meta["key"])
}

func TestMsg2Dict_BaseMessage列表_保留元数据无额外字段(t *testing.T) {
	msg := schema.NewUserMessage("hello")
	msgs := []schema.BaseMessage{msg}
	result, err := Msg2Dict(msgs, true)
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	// 没有 name 和 metadata 时不应有这些键
	_, hasName := result[0]["name"]
	assert.False(t, hasName)
	_, hasMeta := result[0]["metadata"]
	assert.False(t, hasMeta)
}

func TestMsg2Dict_Dict列表(t *testing.T) {
	dicts := []map[string]any{
		{"role": "user", "content": "hello"},
	}
	result, err := Msg2Dict(dicts, false)
	assert.NoError(t, err)
	assert.Equal(t, dicts, result)
}

func TestMsg2Dict_无效类型(t *testing.T) {
	_, err := Msg2Dict("invalid", false)
	assert.Error(t, err)
}

func TestMsg2Dict_空列表(t *testing.T) {
	result, err := Msg2Dict([]schema.BaseMessage{}, false)
	assert.NoError(t, err)
	assert.Len(t, result, 0)
}

// ──────────────────────────── UpdateEntity 测试 ────────────────────────────

func TestUpdateEntity_正常JSON响应(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "old content"
	response := `{"summary": "new summary", "attributes": {"color": "blue"}}`
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "new summary", entity.Content)
	assert.Equal(t, "blue", entity.Attributes["color"])
}

func TestUpdateEntity_仅更新Summary(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "old content"
	response := `{"summary": "updated summary"}`
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "updated summary", entity.Content)
}

func TestUpdateEntity_Summary为列表(t *testing.T) {
	entity := graph.NewEntity()
	response := `{"summary": ["line1", "line2", "line3"]}`
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "line1\nline2\nline3", entity.Content)
}

func TestUpdateEntity_Summary包含null占位词(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "old content"
	response := `{"summary": "null"}`
	UpdateEntity(entity, response, nil)
	// 包含 "null" 不应更新 content
	assert.Equal(t, "old content", entity.Content)
}

func TestUpdateEntity_Summary包含none占位词(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "old content"
	response := `{"summary": "None"}`
	UpdateEntity(entity, response, nil)
	// "none" (casefold 后) 不应更新 content
	assert.Equal(t, "old content", entity.Content)
}

func TestUpdateEntity_Summary包含empty占位词(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "old content"
	response := `{"summary": "empty"}`
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "old content", entity.Content)
}

func TestUpdateEntity_响应为列表取第一个(t *testing.T) {
	entity := graph.NewEntity()
	response := `[{"summary": "first item", "attributes": {"key": "val"}}]`
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "first item", entity.Content)
	assert.Equal(t, "val", entity.Attributes["key"])
}

func TestUpdateEntity_响应为代码块中的字符串(t *testing.T) {
	entity := graph.NewEntity()
	// 纯 JSON 字符串无法被 parse_json 直接解析（不以 { 或 [ 开头），
	// 需要放在代码块中且包含对象
	response := "```json\n{\"summary\": \"just a string summary\"}\n```"
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "just a string summary", entity.Content)
}

func TestUpdateEntity_Attributes为字符串(t *testing.T) {
	entity := graph.NewEntity()
	response := `{"summary": "test", "attributes": "{\"color\": \"red\"}"}`
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "test", entity.Content)
	assert.Equal(t, "red", entity.Attributes["color"])
}

func TestUpdateEntity_空响应(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "original"
	UpdateEntity(entity, "", nil)
	// 空响应无法解析，不应修改 entity
	assert.Equal(t, "original", entity.Content)
}

// ──────────────────────────── AssembleInvokeParams 测试 ────────────────────────────

func TestAssembleInvokeParams_字符串模板(t *testing.T) {
	tmpl := prompt.NewPromptTemplate("test", "Hello {{name}}")
	kwargs := map[string]any{"name": "world"}
	params, err := AssembleInvokeParams(kwargs, tmpl, nil)
	require.NoError(t, err)
	assert.Contains(t, params, "messages")
	msgs, ok := params["messages"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0]["role"])
	assert.Equal(t, "Hello world", msgs[0]["content"])
	_, hasResponseFormat := params["response_format"]
	assert.False(t, hasResponseFormat)
}

func TestAssembleInvokeParams_带OutputModel(t *testing.T) {
	tmpl := prompt.NewPromptTemplate("test", "Hello {{name}}")
	kwargs := map[string]any{"name": "world"}
	outputModel := map[string]any{"type": "json_object"}
	params, err := AssembleInvokeParams(kwargs, tmpl, outputModel)
	require.NoError(t, err)
	assert.Contains(t, params, "response_format")
	assert.Equal(t, outputModel, params["response_format"])
}

func TestAssembleInvokeParams_消息列表模板(t *testing.T) {
	msgs := []schema.BaseMessage{
		schema.NewSystemMessage("You are helpful"),
		schema.NewUserMessage("{{question}}"),
	}
	tmpl := prompt.NewPromptTemplate("test", msgs)
	kwargs := map[string]any{"question": "What is Go?"}
	params, err := AssembleInvokeParams(kwargs, tmpl, nil)
	require.NoError(t, err)
	resultMsgs, ok := params["messages"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, resultMsgs, 2)
	assert.Equal(t, "system", resultMsgs[0]["role"])
	assert.Equal(t, "You are helpful", resultMsgs[0]["content"])
	assert.Equal(t, "user", resultMsgs[1]["role"])
	assert.Equal(t, "What is Go?", resultMsgs[1]["content"])
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

func TestContainsNullWord(t *testing.T) {
	assert.True(t, containsNullWord("this is null"))
	assert.True(t, containsNullWord("this is none"))
	assert.True(t, containsNullWord("this is empty"))
	assert.False(t, containsNullWord("this is valid"))
	assert.False(t, containsNullWord(""))
}

func TestContentToAny(t *testing.T) {
	// 文本内容
	textContent := schema.NewTextContent("hello")
	assert.Equal(t, "hello", contentToAny(textContent))

	// 多模态内容
	multiContent := schema.NewMultiModalContent(schema.ContentPart{Type: "text", Text: "hi"})
	parts := contentToAny(multiContent)
	partsSlice, ok := parts.([]schema.ContentPart)
	assert.True(t, ok)
	assert.Len(t, partsSlice, 1)
}

func TestParseSummary_各种类型(t *testing.T) {
	tests := []struct {
		name     string
		info     map[string]any
		expected string
	}{
		{"字符串摘要", map[string]any{"summary": "hello"}, "hello"},
		{"列表摘要", map[string]any{"summary": []any{"a", "b", "c"}}, "a\nb\nc"},
		{"空摘要不更新", map[string]any{"summary": ""}, "original"},
		{"nil摘要不更新", map[string]any{}, "original"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity := graph.NewEntity()
			entity.Content = "original"
			parseSummary(entity, tt.info)
			assert.Equal(t, tt.expected, entity.Content)
		})
	}
}

func TestParseAttributes_各种类型(t *testing.T) {
	tests := []struct {
		name      string
		info      map[string]any
		wantAttrs bool
		attrCheck func(map[string]any)
	}{
		{
			"dict属性",
			map[string]any{"attributes": map[string]any{"color": "blue"}},
			true,
			func(m map[string]any) { assert.Equal(t, "blue", m["color"]) },
		},
		{
			"字符串JSON属性",
			map[string]any{"attributes": `{"size": "large"}`},
			true,
			func(m map[string]any) { assert.Equal(t, "large", m["size"]) },
		},
		{
			"无属性",
			map[string]any{},
			false,
			func(m map[string]any) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity := graph.NewEntity()
			parseAttributes(entity, tt.info)
			if tt.wantAttrs {
				require.NotNil(t, entity.Attributes)
				tt.attrCheck(entity.Attributes)
			}
		})
	}
}

func TestParseAttributes_列表类型(t *testing.T) {
	entity := graph.NewEntity()
	// []any 类型的 attributes，尝试转为 dict
	info := map[string]any{
		"attributes": []any{[]any{"color", "blue"}, []any{"size", "large"}},
	}
	parseAttributes(entity, info)
	require.NotNil(t, entity.Attributes)
	assert.Equal(t, "blue", entity.Attributes["color"])
	assert.Equal(t, "large", entity.Attributes["size"])
}

func TestParseAttributes_列表类型无法转换(t *testing.T) {
	entity := graph.NewEntity()
	// []any 中元素不是 [key, value] 对，无法转为 dict
	info := map[string]any{
		"attributes": []any{"just", "strings"},
	}
	parseAttributes(entity, info)
	// 无法转为 dict，不应设置 attributes
	assert.Nil(t, entity.Attributes)
}

func TestParseAttributes_字符串属性无法解析(t *testing.T) {
	entity := graph.NewEntity()
	// 字符串不是有效 JSON，parse_json 返回 nil
	info := map[string]any{
		"attributes": "not valid json",
	}
	parseAttributes(entity, info)
	assert.Nil(t, entity.Attributes)
}

func TestParseAttributes_default类型可JSON转换(t *testing.T) {
	entity := graph.NewEntity()
	// 使用一个可通过 JSON 序列化/反序列化转为 map 的类型
	type attrStruct struct {
		Color string `json:"color"`
		Size  string `json:"size"`
	}
	info := map[string]any{
		"attributes": attrStruct{Color: "red", Size: "big"},
	}
	parseAttributes(entity, info)
	require.NotNil(t, entity.Attributes)
	assert.Equal(t, "red", entity.Attributes["color"])
	assert.Equal(t, "big", entity.Attributes["size"])
}

func TestUpdateEntity_Attributes为列表(t *testing.T) {
	entity := graph.NewEntity()
	response := `{"summary": "test", "attributes": [["color", "blue"], ["size", "big"]]}`
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "test", entity.Content)
	require.NotNil(t, entity.Attributes)
	assert.Equal(t, "blue", entity.Attributes["color"])
}

func TestUpdateEntity_Summary为非字符串非列表(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "original"
	// summary 为数字类型，应转为字符串
	response := `{"summary": 42}`
	UpdateEntity(entity, response, nil)
	assert.Equal(t, "42", entity.Content)
}

func TestUpdateEntity_ParseJSON返回nil(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "original"
	// 无法解析的响应
	UpdateEntity(entity, "no json here at all", nil)
	assert.Equal(t, "original", entity.Content)
}

func TestUpdateEntity_ParseJSON返回非map非list非string(t *testing.T) {
	entity := graph.NewEntity()
	entity.Content = "original"
	// 纯数字 JSON 响应
	UpdateEntity(entity, "42", nil)
	// 42 解析后是 float64，不是 map/list/string，不应修改 entity
	assert.Equal(t, "original", entity.Content)
}

func TestAssembleInvokeParams_格式化失败(t *testing.T) {
	tmpl := prompt.NewPromptTemplate("test", "Hello {{name}} {{missing}}")
	// 不传 missing，模板中的 missing 占位符会保留
	// Format 不会返回错误，只是保留占位符
	kwargs := map[string]any{"name": "world"}
	params, err := AssembleInvokeParams(kwargs, tmpl, nil)
	require.NoError(t, err)
	assert.Contains(t, params, "messages")
}

// 确保 JSON 序列化正常（验证 map 返回值可序列化）
func TestMsg2Dict_可JSON序列化(t *testing.T) {
	msgs := []schema.BaseMessage{
		schema.NewUserMessage("hello"),
	}
	result, err := Msg2Dict(msgs, false)
	require.NoError(t, err)
	data, err := json.Marshal(result)
	require.NoError(t, err)
	assert.Contains(t, string(data), "hello")
}
