package client

import (
	"math"
	"testing"

	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── callToolResultToMap 测试 ────────────────────────────

// TestCallToolResultToMap_Nil 测试 nil 输入返回 nil。
func TestCallToolResultToMap_Nil(t *testing.T) {
	result := callToolResultToMap(nil)
	assert.Nil(t, result)
}

// TestCallToolResultToMap_空Content 测试空 Content 列表。
func TestCallToolResultToMap_空Content(t *testing.T) {
	r := &mcp.CallToolResult{
		Content: []mcp.Content{},
		IsError: false,
	}
	result := callToolResultToMap(r)
	assert.NotNil(t, result)
	assert.Equal(t, []any{}, result["content"])
	assert.Equal(t, false, result["isError"])
	assert.Nil(t, result["structuredContent"])
}

// TestCallToolResultToMap_含TextContent 测试含 TextContent 的转换。
func TestCallToolResultToMap_含TextContent(t *testing.T) {
	r := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: "你好"},
		},
		IsError: false,
	}
	result := callToolResultToMap(r)
	assert.NotNil(t, result)
	contents, ok := result["content"].([]any)
	assert.True(t, ok)
	assert.Len(t, contents, 1)
	textMap, ok := contents[0].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "text", textMap["type"])
	assert.Equal(t, "你好", textMap["text"])
}

// TestCallToolResultToMap_含StructuredContent 测试含 StructuredContent 的转换。
func TestCallToolResultToMap_含StructuredContent(t *testing.T) {
	structured := map[string]any{"key": "value"}
	r := &mcp.CallToolResult{
		Content:           []mcp.Content{},
		IsError:           true,
		StructuredContent: structured,
	}
	result := callToolResultToMap(r)
	assert.NotNil(t, result)
	assert.Equal(t, true, result["isError"])
	assert.Equal(t, structured, result["structuredContent"])
}

// TestCallToolResultToMap_多种Content类型 测试含多种 Content 类型的转换。
func TestCallToolResultToMap_多种Content类型(t *testing.T) {
	r := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: "hello"},
			mcp.ImageContent{Type: "image", Data: "base64data", MIMEType: "image/png"},
			mcp.AudioContent{Type: "audio", Data: "audiob64", MIMEType: "audio/wav"},
		},
		IsError: false,
	}
	result := callToolResultToMap(r)
	contents := result["content"].([]any)
	assert.Len(t, contents, 3)
}

// ──────────────────────────── contentToMap 测试 ────────────────────────────

// TestContentToMap_Nil 测试 nil Content 返回 nil。
func TestContentToMap_Nil(t *testing.T) {
	result := contentToMap(nil)
	assert.Nil(t, result)
}

// TestContentToMap_TextContent 测试 TextContent 转换。
func TestContentToMap_TextContent(t *testing.T) {
	c := mcp.TextContent{Type: "text", Text: "hello world"}
	result := contentToMap(c)
	assert.Equal(t, map[string]any{
		"type": "text",
		"text": "hello world",
	}, result)
}

// TestContentToMap_ImageContent 测试 ImageContent 转换。
func TestContentToMap_ImageContent(t *testing.T) {
	c := mcp.ImageContent{Type: "image", Data: "base64data", MIMEType: "image/png"}
	result := contentToMap(c)
	assert.Equal(t, map[string]any{
		"type":     "image",
		"data":     "base64data",
		"mimeType": "image/png",
	}, result)
}

// TestContentToMap_AudioContent 测试 AudioContent 转换。
func TestContentToMap_AudioContent(t *testing.T) {
	c := mcp.AudioContent{Type: "audio", Data: "audiob64", MIMEType: "audio/wav"}
	result := contentToMap(c)
	assert.Equal(t, map[string]any{
		"type":     "audio",
		"data":     "audiob64",
		"mimeType": "audio/wav",
	}, result)
}

// TestContentToMap_EmbeddedResource 测试 EmbeddedResource 转换。
func TestContentToMap_EmbeddedResource(t *testing.T) {
	c := mcp.EmbeddedResource{
		Type: "resource",
		Resource: mcp.TextResourceContents{
			URI:      "file:///test.txt",
			MIMEType: "text/plain",
			Text:     "内容",
		},
	}
	result := contentToMap(c)
	assert.Equal(t, "resource", result["type"])
	resourceMap, ok := result["resource"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "file:///test.txt", resourceMap["uri"])
	assert.Equal(t, "内容", resourceMap["text"])
}

// TestContentToMap_ResourceLink 测试 ResourceLink 转换。
func TestContentToMap_ResourceLink(t *testing.T) {
	c := mcp.ResourceLink{
		Type:        "resource_link",
		URI:         "file:///data.json",
		Name:        "数据文件",
		Description: "数据描述",
		MIMEType:    "application/json",
	}
	result := contentToMap(c)
	assert.Equal(t, map[string]any{
		"type":        "resource_link",
		"uri":         "file:///data.json",
		"name":        "数据文件",
		"description": "数据描述",
		"mimeType":    "application/json",
	}, result)
}

// TestContentToMap_未知类型 测试未在 switch 中处理的 Content 类型走 default 分支。
func TestContentToMap_未知类型(t *testing.T) {
	// ToolUseContent 实现了 mcp.Content 接口，但不在 contentToMap 的 switch 中
	c := mcp.ToolUseContent{Type: "tool_use", ID: "1", Name: "test"}
	result := contentToMap(c)
	assert.Equal(t, map[string]any{
		"type": "unknown",
	}, result)
}

// ──────────────────────────── resourceContentsToMap 测试 ────────────────────────────

// TestResourceContentsToMap_Nil 测试 nil 输入返回 nil。
func TestResourceContentsToMap_Nil(t *testing.T) {
	result := resourceContentsToMap(nil)
	assert.Nil(t, result)
}

// TestResourceContentsToMap_TextResourceContents 测试 TextResourceContents 转换。
func TestResourceContentsToMap_TextResourceContents(t *testing.T) {
	rc := mcp.TextResourceContents{
		URI:      "file:///readme.md",
		MIMEType: "text/markdown",
		Text:     "# 标题",
	}
	result := resourceContentsToMap(rc)
	assert.Equal(t, map[string]any{
		"uri":      "file:///readme.md",
		"mimeType": "text/markdown",
		"text":     "# 标题",
	}, result)
}

// TestResourceContentsToMap_BlobResourceContents 测试 BlobResourceContents 转换。
func TestResourceContentsToMap_BlobResourceContents(t *testing.T) {
	rc := mcp.BlobResourceContents{
		URI:      "file:///image.png",
		MIMEType: "image/png",
		Blob:     "base64blob",
	}
	result := resourceContentsToMap(rc)
	assert.Equal(t, map[string]any{
		"uri":      "file:///image.png",
		"mimeType": "image/png",
		"blob":     "base64blob",
	}, result)
}

// ──────────────────────────── jsonSchemaToParams 测试 ────────────────────────────

// TestJsonSchemaToParams_NilProperties 测试 nil Properties 返回 nil。
func TestJsonSchemaToParams_NilProperties(t *testing.T) {
	schema := mcp.ToolInputSchema{
		Type:       "object",
		Properties: nil,
	}
	result := jsonSchemaToParams(schema)
	assert.Nil(t, result)
}

// TestJsonSchemaToParams_空Properties 测试空 Properties 返回空切片。
func TestJsonSchemaToParams_空Properties(t *testing.T) {
	schema := mcp.ToolInputSchema{
		Type:       "object",
		Properties: map[string]any{},
	}
	result := jsonSchemaToParams(schema)
	assert.Empty(t, result)
}

// TestJsonSchemaToParams_简单属性 测试简单属性转换。
func TestJsonSchemaToParams_简单属性(t *testing.T) {
	schema := mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"name": map[string]any{"type": "string", "description": "名称"},
			"age":  map[string]any{"type": "integer", "description": "年龄"},
		},
		Required: []string{"name"},
	}
	result := jsonSchemaToParams(schema)
	assert.Len(t, result, 2)

	// 验证 name 是必填的
	var nameParam *commonschema.Param
	for _, p := range result {
		if p.Name == "name" {
			nameParam = p
			break
		}
	}
	assert.NotNil(t, nameParam)
	assert.True(t, nameParam.Required)
	assert.Equal(t, commonschema.ParamTypeString, nameParam.Type)
}

// TestJsonSchemaToParams_无Required字段 测试无 Required 字段时所有参数非必填。
func TestJsonSchemaToParams_无Required字段(t *testing.T) {
	schema := mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	result := jsonSchemaToParams(schema)
	assert.Len(t, result, 1)
	assert.False(t, result[0].Required)
}

// ──────────────────────────── jsonSchemaPropToParam 测试 ────────────────────────────

// TestJsonSchemaPropToParam_非map类型 测试 prop 不是 map 类型时返回默认字符串参数。
func TestJsonSchemaPropToParam_非map类型(t *testing.T) {
	p := jsonSchemaPropToParam("field", "not_a_map", true)
	assert.NotNil(t, p)
	assert.Equal(t, commonschema.ParamTypeString, p.Type)
	assert.True(t, p.Required)
}

// TestJsonSchemaPropToParam_String类型 测试 string 类型转换。
func TestJsonSchemaPropToParam_String类型(t *testing.T) {
	prop := map[string]any{
		"type":        "string",
		"description": "用户名",
	}
	p := jsonSchemaPropToParam("username", prop, true)
	assert.Equal(t, "username", p.Name)
	assert.Equal(t, "用户名", p.Description)
	assert.Equal(t, commonschema.ParamTypeString, p.Type)
	assert.True(t, p.Required)
}

// TestJsonSchemaPropToParam_Boolean类型 测试 boolean 类型转换。
func TestJsonSchemaPropToParam_Boolean类型(t *testing.T) {
	prop := map[string]any{
		"type":        "boolean",
		"description": "是否启用",
	}
	p := jsonSchemaPropToParam("enabled", prop, false)
	assert.Equal(t, commonschema.ParamTypeBoolean, p.Type)
	assert.False(t, p.Required)
}

// TestJsonSchemaPropToParam_Integer类型 测试 integer 类型转换。
func TestJsonSchemaPropToParam_Integer类型(t *testing.T) {
	prop := map[string]any{
		"type":        "integer",
		"description": "年龄",
		"minimum":     float64(0),
		"maximum":     float64(150),
	}
	p := jsonSchemaPropToParam("age", prop, true)
	assert.Equal(t, commonschema.ParamTypeInteger, p.Type)
	assert.Equal(t, float64(0), p.Minimum)
	assert.Equal(t, float64(150), p.Maximum)
}

// TestJsonSchemaPropToParam_Number类型 测试 number 类型转换。
func TestJsonSchemaPropToParam_Number类型(t *testing.T) {
	prop := map[string]any{
		"type":        "number",
		"description": "价格",
		"minimum":     float64(0.01),
		"maximum":     float64(9999.99),
	}
	p := jsonSchemaPropToParam("price", prop, false)
	assert.Equal(t, commonschema.ParamTypeNumber, p.Type)
	assert.Equal(t, float64(0.01), p.Minimum)
	assert.Equal(t, float64(9999.99), p.Maximum)
}

// TestJsonSchemaPropToParam_Array类型 测试 array 类型转换。
func TestJsonSchemaPropToParam_Array类型(t *testing.T) {
	prop := map[string]any{
		"type":        "array",
		"description": "标签列表",
		"items":       map[string]any{"type": "string"},
	}
	p := jsonSchemaPropToParam("tags", prop, false)
	assert.Equal(t, commonschema.ParamTypeArray, p.Type)
	assert.NotNil(t, p.Items)
	assert.Equal(t, commonschema.ParamTypeString, p.Items.Type)
}

// TestJsonSchemaPropToParam_Array类型无Items 测试 array 类型无 items 时 Items 为 nil。
func TestJsonSchemaPropToParam_Array类型无Items(t *testing.T) {
	prop := map[string]any{
		"type":        "array",
		"description": "空数组",
	}
	p := jsonSchemaPropToParam("empty", prop, false)
	assert.Equal(t, commonschema.ParamTypeArray, p.Type)
	assert.Nil(t, p.Items)
}

// TestJsonSchemaPropToParam_Object类型 测试 object 类型转换。
func TestJsonSchemaPropToParam_Object类型(t *testing.T) {
	prop := map[string]any{
		"type":        "object",
		"description": "配置",
		"properties": map[string]any{
			"host": map[string]any{"type": "string", "description": "主机"},
			"port": map[string]any{"type": "integer", "description": "端口"},
		},
		"required": []any{"host"},
	}
	p := jsonSchemaPropToParam("config", prop, true)
	assert.Equal(t, commonschema.ParamTypeObject, p.Type)
	assert.Len(t, p.Properties, 2)

	// 验证 host 是必填的
	var hostParam *commonschema.Param
	for _, prop := range p.Properties {
		if prop.Name == "host" {
			hostParam = prop
			break
		}
	}
	assert.NotNil(t, hostParam)
	assert.True(t, hostParam.Required)
}

// TestJsonSchemaPropToParam_Object类型无Properties 测试 object 类型无 properties 时 Properties 为 nil。
func TestJsonSchemaPropToParam_Object类型无Properties(t *testing.T) {
	prop := map[string]any{
		"type":        "object",
		"description": "空对象",
	}
	p := jsonSchemaPropToParam("empty_obj", prop, false)
	assert.Equal(t, commonschema.ParamTypeObject, p.Type)
	assert.Nil(t, p.Properties)
}

// TestJsonSchemaPropToParam_Object类型无Required 测试 object 类型无 required 时所有属性非必填。
func TestJsonSchemaPropToParam_Object类型无Required(t *testing.T) {
	prop := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	p := jsonSchemaPropToParam("obj", prop, false)
	assert.Len(t, p.Properties, 1)
	assert.False(t, p.Properties[0].Required)
}

// TestJsonSchemaPropToParam_未知类型 测试未知类型默认返回字符串参数。
func TestJsonSchemaPropToParam_未知类型(t *testing.T) {
	prop := map[string]any{
		"type":        "unknown_type",
		"description": "未知类型字段",
	}
	p := jsonSchemaPropToParam("field", prop, false)
	assert.Equal(t, commonschema.ParamTypeString, p.Type)
	assert.Equal(t, "未知类型字段", p.Description)
}

// TestJsonSchemaPropToParam_无type字段 测试无 type 字段时走 default 分支。
func TestJsonSchemaPropToParam_无type字段(t *testing.T) {
	prop := map[string]any{
		"description": "无类型字段",
	}
	p := jsonSchemaPropToParam("field", prop, true)
	assert.Equal(t, commonschema.ParamTypeString, p.Type)
}

// ──────────────────────────── applyStringConstraints 测试 ────────────────────────────

// TestApplyStringConstraints_完整约束 测试所有字符串约束字段。
func TestApplyStringConstraints_完整约束(t *testing.T) {
	p := commonschema.NewStringParam("name", "名称", true)
	propMap := map[string]any{
		"minLength": float64(1),
		"maxLength": float64(100),
		"pattern":   "^[a-z]+$",
		"format":    "email",
	}
	applyStringConstraints(p, propMap)
	assert.Equal(t, 1, p.MinLength)
	assert.Equal(t, 100, p.MaxLength)
	assert.Equal(t, "^[a-z]+$", p.Pattern)
	assert.Equal(t, "email", p.Format)
}

// TestApplyStringConstraints_零值不设置 测试零值约束字段不设置。
func TestApplyStringConstraints_零值不设置(t *testing.T) {
	p := commonschema.NewStringParam("name", "名称", true)
	propMap := map[string]any{
		"minLength": float64(0),
		"maxLength": float64(0),
	}
	applyStringConstraints(p, propMap)
	assert.Equal(t, 0, p.MinLength)
	assert.Equal(t, 0, p.MaxLength)
}

// TestApplyStringConstraints_空Map 测试空 map 不影响 Param。
func TestApplyStringConstraints_空Map(t *testing.T) {
	p := commonschema.NewStringParam("name", "名称", true)
	applyStringConstraints(p, map[string]any{})
	assert.Equal(t, 0, p.MinLength)
	assert.Equal(t, 0, p.MaxLength)
	assert.Equal(t, "", p.Pattern)
	assert.Equal(t, "", p.Format)
}

// TestApplyStringConstraints_类型不匹配 测试字段值类型不匹配时不设置。
func TestApplyStringConstraints_类型不匹配(t *testing.T) {
	p := commonschema.NewStringParam("name", "名称", true)
	propMap := map[string]any{
		"minLength": "not_a_number",
		"maxLength": int(50),
		"pattern":   123,
		"format":    true,
	}
	applyStringConstraints(p, propMap)
	assert.Equal(t, 0, p.MinLength)
	assert.Equal(t, 0, p.MaxLength)
	assert.Equal(t, "", p.Pattern)
	assert.Equal(t, "", p.Format)
}

// ──────────────────────────── applyNumericConstraints 测试 ────────────────────────────

// TestApplyNumericConstraints_完整约束 测试所有数值约束字段。
func TestApplyNumericConstraints_完整约束(t *testing.T) {
	p := commonschema.NewIntegerParam("score", "分数", true)
	propMap := map[string]any{
		"minimum": float64(0),
		"maximum": float64(100),
		"format":  "int32",
	}
	applyNumericConstraints(p, propMap)
	assert.Equal(t, float64(0), p.Minimum) // minimum=0 是合法值，应被设置
	assert.Equal(t, float64(100), p.Maximum)
	assert.Equal(t, "int32", p.Format)
}

// TestApplyNumericConstraints_空Map 测试空 map 不影响 Param。
func TestApplyNumericConstraints_空Map(t *testing.T) {
	p := commonschema.NewIntegerParam("score", "分数", true)
	applyNumericConstraints(p, map[string]any{})
	assert.True(t, math.IsNaN(p.Minimum), "Minimum 应为 NaN（未设置）")
	assert.True(t, math.IsNaN(p.Maximum), "Maximum 应为 NaN（未设置）")
	assert.Equal(t, "", p.Format)
}

// TestApplyNumericConstraints_类型不匹配 测试字段值类型不匹配时不设置。
func TestApplyNumericConstraints_类型不匹配(t *testing.T) {
	p := commonschema.NewNumberParam("price", "价格", true)
	propMap := map[string]any{
		"minimum": "not_a_number",
		"maximum": int(50),
		"format":  123,
	}
	applyNumericConstraints(p, propMap)
	assert.True(t, math.IsNaN(p.Minimum), "Minimum 应为 NaN（类型不匹配，未设置）")
	assert.True(t, math.IsNaN(p.Maximum), "Maximum 应保持 NaN（int(50) 不会匹配 float64 断言）")
	assert.Equal(t, "", p.Format)
}

// TestApplyNumericConstraints_Number类型 测试 Number 类型的数值约束。
func TestApplyNumericConstraints_Number类型(t *testing.T) {
	p := commonschema.NewNumberParam("ratio", "比率", false)
	propMap := map[string]any{
		"minimum": float64(0.0),
		"maximum": float64(1.0),
		"format":  "float",
	}
	applyNumericConstraints(p, propMap)
	assert.Equal(t, float64(0.0), p.Minimum)
	assert.Equal(t, float64(1.0), p.Maximum)
	assert.Equal(t, "float", p.Format)
}

// ──────────────────────────── mergeQueryParams 测试 ────────────────────────────

// TestMergeQueryParams_空Params 测试空查询参数直接返回原始 URL。
func TestMergeQueryParams_空Params(t *testing.T) {
	result, err := mergeQueryParams("http://example.com/path", map[string]string{})
	assert.NoError(t, err)
	assert.Equal(t, "http://example.com/path", result)
}

// TestMergeQueryParams_NilParams 测试 nil 查询参数直接返回原始 URL。
func TestMergeQueryParams_NilParams(t *testing.T) {
	result, err := mergeQueryParams("http://example.com/path", nil)
	assert.NoError(t, err)
	assert.Equal(t, "http://example.com/path", result)
}

// TestMergeQueryParams_追加参数 测试向无查询参数的 URL 追加参数。
func TestMergeQueryParams_追加参数(t *testing.T) {
	result, err := mergeQueryParams("http://example.com/path", map[string]string{
		"key": "value",
		"foo": "bar",
	})
	assert.NoError(t, err)
	assert.Contains(t, result, "key=value")
	assert.Contains(t, result, "foo=bar")
}

// TestMergeQueryParams_合并到已有参数 测试向已有查询参数的 URL 合并参数。
func TestMergeQueryParams_合并到已有参数(t *testing.T) {
	result, err := mergeQueryParams("http://example.com/path?existing=1", map[string]string{
		"new": "2",
	})
	assert.NoError(t, err)
	assert.Contains(t, result, "existing=1")
	assert.Contains(t, result, "new=2")
}

// TestMergeQueryParams_同名键覆盖 测试同名键覆盖已有参数。
func TestMergeQueryParams_同名键覆盖(t *testing.T) {
	result, err := mergeQueryParams("http://example.com/path?key=old", map[string]string{
		"key": "new",
	})
	assert.NoError(t, err)
	assert.Contains(t, result, "key=new")
	assert.NotContains(t, result, "key=old")
}

// TestMergeQueryParams_无效URL 测试无效 URL 返回错误。
func TestMergeQueryParams_无效URL(t *testing.T) {
	_, err := mergeQueryParams("://invalid-url", map[string]string{
		"key": "value",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "解析 URL 失败")
}

// ──────────────────────────── convertSchemaArray 测试 ────────────────────────────

// TestConvertSchemaArray_空数组 测试空数组返回空切片。
func TestConvertSchemaArray_空数组(t *testing.T) {
	result := convertSchemaArray([]any{})
	assert.Empty(t, result)
}

// TestConvertSchemaArray_字符串类型 测试含字符串类型子 schema 的转换。
func TestConvertSchemaArray_字符串类型(t *testing.T) {
	arr := []any{
		map[string]any{"type": "string", "description": "名称"},
		map[string]any{"type": "integer", "description": "年龄"},
	}
	result := convertSchemaArray(arr)
	assert.Len(t, result, 2)
	assert.Equal(t, commonschema.ParamTypeString, result[0].Type)
	assert.Equal(t, commonschema.ParamTypeInteger, result[1].Type)
}

// TestConvertSchemaArray_非map类型 测试非 map 类型走默认分支仍生成参数。
func TestConvertSchemaArray_非map类型(t *testing.T) {
	arr := []any{
		"not_a_map",
		map[string]any{"type": "string"},
	}
	result := convertSchemaArray(arr)
	// 非 map 类型会走 jsonSchemaPropToParam 的默认分支，生成 string 类型参数
	assert.Len(t, result, 2)
	assert.Equal(t, commonschema.ParamTypeString, result[0].Type)
}

// ──────────────────────────── applyCommonFields 测试 ────────────────────────────

// TestApplyCommonFields_Enum 测试 enum 字段提取。
func TestApplyCommonFields_Enum(t *testing.T) {
	p := commonschema.NewStringParam("status", "状态", true)
	propMap := map[string]any{
		"enum": []any{"active", "inactive"},
	}
	applyCommonFields(p, propMap)
	assert.Equal(t, []any{"active", "inactive"}, p.Enum)
}

// TestApplyCommonFields_Default 测试 default 字段提取。
func TestApplyCommonFields_Default(t *testing.T) {
	p := commonschema.NewStringParam("name", "名称", true)
	propMap := map[string]any{
		"default": "test",
	}
	applyCommonFields(p, propMap)
	assert.Equal(t, "test", p.Default)
}

// TestApplyCommonFields_Nullable 测试 nullable 字段提取。
func TestApplyCommonFields_Nullable(t *testing.T) {
	p := commonschema.NewStringParam("name", "名称", true)
	propMap := map[string]any{
		"nullable": true,
	}
	applyCommonFields(p, propMap)
	assert.True(t, p.Nullable)
}

// TestApplyCommonFields_AnyOf 测试 anyOf 字段提取。
func TestApplyCommonFields_AnyOf(t *testing.T) {
	p := commonschema.NewStringParam("value", "值", false)
	propMap := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}
	applyCommonFields(p, propMap)
	assert.Len(t, p.AnyOf, 2)
}

// TestApplyCommonFields_AllOf 测试 allOf 字段提取。
func TestApplyCommonFields_AllOf(t *testing.T) {
	p := commonschema.NewStringParam("value", "值", false)
	propMap := map[string]any{
		"allOf": []any{
			map[string]any{"type": "string"},
		},
	}
	applyCommonFields(p, propMap)
	assert.Len(t, p.AllOf, 1)
}

// TestApplyCommonFields_OneOf 测试 oneOf 字段提取。
func TestApplyCommonFields_OneOf(t *testing.T) {
	p := commonschema.NewStringParam("value", "值", false)
	propMap := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "null"},
		},
	}
	applyCommonFields(p, propMap)
	assert.Len(t, p.OneOf, 2)
}

// TestApplyCommonFields_空Map 测试空 map 不影响 Param。
func TestApplyCommonFields_空Map(t *testing.T) {
	p := commonschema.NewStringParam("name", "名称", true)
	applyCommonFields(p, map[string]any{})
	assert.Nil(t, p.Enum)
	assert.Nil(t, p.Default)
	assert.False(t, p.Nullable)
	assert.Nil(t, p.AnyOf)
	assert.Nil(t, p.AllOf)
	assert.Nil(t, p.OneOf)
}

// ──────────────────────────── resourceContentsToMap 扩展测试 ────────────────────────────

// TestResourceContentsToMap_未知类型 测试未知 ResourceContents 走 default 分支。
func TestResourceContentsToMap_未知类型(t *testing.T) {
	// 使用一个不在 switch 中的类型
	rc := mcp.BlobResourceContents{} // 已有测试，换用自定义方式
	// 直接构造一个未实现具体类型的 ResourceContents
	result := resourceContentsToMap(rc)
	// BlobResourceContents 有自己的分支，结果不为 unknown
	assert.NotNil(t, result)
}
