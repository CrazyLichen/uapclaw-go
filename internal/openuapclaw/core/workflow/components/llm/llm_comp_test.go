package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 测试助手 ────────────────────────────

// llmTestConfig 合法基线配置（system→user 模板 + text 格式 + 单字段输出）
func llmTestConfig() *LLMCompConfig {
	return &LLMCompConfig{
		TemplateContent: []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "hi"},
		},
		ResponseFormat: map[string]any{"type": "text"},
		OutputConfig:   map[string]any{"answer": map[string]any{"type": "string", "required": true}},
	}
}

// assertLLMCode 断言错误为 BaseError 且状态码匹配（仓内 atomic_node_test.go 同款惯例）
func assertLLMCode(t *testing.T, err error, status exception.StatusCode) {
	t.Helper()
	require.Error(t, err)
	be, ok := err.(*exception.BaseError)
	require.True(t, ok, "期望 BaseError，实际 %T", err)
	assert.Equal(t, status, be.Status())
}

// ──────────────────────────── 构造期校验链 ────────────────────────────

// TestValidateTemplate_缺少user消息 必须报 101000（llm_comp.py:468-472）
func TestValidateTemplate_缺少user消息(t *testing.T) {
	err := validateTemplate([]any{map[string]any{"role": "system", "content": "sys"}}, nil, nil)
	assertLLMCode(t, err, exception.StatusComponentLLMTemplateConfigError)
	assert.Equal(t, "user message is required", errMsg(t, err))
}

// TestValidateTemplate_system在user之后 system 必须在 user 前（llm_comp.py:462-467）
func TestValidateTemplate_system在user之后(t *testing.T) {
	cfg := []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "system", "content": "sys"},
	}
	err := validateTemplate(cfg, nil, nil)
	assertLLMCode(t, err, exception.StatusComponentLLMTemplateConfigError)
	assert.Equal(t, "system message must be before user message", errMsg(t, err))
}

// TestValidateTemplate_非法system消息 role=system 元素缺 content → 101000（:451-455）
func TestValidateTemplate_非法system消息(t *testing.T) {
	cfg := []any{
		map[string]any{"role": "system"},
		map[string]any{"role": "user", "content": "hi"},
	}
	err := validateTemplate(cfg, nil, nil)
	assertLLMCode(t, err, exception.StatusComponentLLMTemplateConfigError)
	assert.Equal(t, "system message is invalid", errMsg(t, err))
}

// TestValidateTemplate_短路分支 system/user 模板任一非 nil 或模板为空 → 直接返回 nil（:444-445）
func TestValidateTemplate_短路分支(t *testing.T) {
	assert.NoError(t, validateTemplate(nil, nil, nil))
	assert.NoError(t, validateTemplate(nil, llmschema.NewSystemMessage("s"), nil))
	// 模板内容非法但 user 模板非 nil → 短路（Python 同序）
	assert.NoError(t, validateTemplate([]any{"junk"}, nil, llmschema.NewUserMessage("u")))
}

// TestValidateResponseFormat_非法type → 101001（llm_comp.py:507-516）
func TestValidateResponseFormat_非法type(t *testing.T) {
	err := validateResponseFormat(map[string]any{"type": "xml"}, map[string]any{"a": 1})
	assertLLMCode(t, err, exception.StatusComponentLLMResponseConfigInvalid)
}

// TestValidateResponseFormat_text多字段 → 101001（:518-522）
func TestValidateResponseFormat_text多字段(t *testing.T) {
	err := validateResponseFormat(map[string]any{"type": "text"}, map[string]any{"a": 1, "b": 2})
	assertLLMCode(t, err, exception.StatusComponentLLMResponseConfigInvalid)
	assert.Equal(t, "output config must contain exactly one parameter for text or markdown response type", errMsg(t, err))
}

// TestValidateResponseFormat_markdown单字段合法 恰一字段通过
func TestValidateResponseFormat_markdown单字段合法(t *testing.T) {
	assert.NoError(t, validateResponseFormat(map[string]any{"type": "markdown"}, map[string]any{"a": 1}))
}

// TestValidateOutputConfig_空配置 → 101002（llm_comp.py:476-480）
func TestValidateOutputConfig_空配置(t *testing.T) {
	err := validateOutputConfig(nil)
	assertLLMCode(t, err, exception.StatusComponentLLMConfigError)
	assert.Equal(t, "output config is empty", errMsg(t, err))
}

// TestValidateOutputConfig_object直通 type=="object" 字符串直接通过（:481-483）
func TestValidateOutputConfig_object直通(t *testing.T) {
	assert.NoError(t, validateOutputConfig(map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}))
}

// TestValidateOutputConfig_空参数名 → 101002（:484-489）
func TestValidateOutputConfig_空参数名(t *testing.T) {
	err := validateOutputConfig(map[string]any{
		"": map[string]any{"type": "string"},
	})
	assertLLMCode(t, err, exception.StatusComponentLLMConfigError)
}

// TestValidateOutputConfig_字段值非法 → 101002（敏感模式脱敏文案，:490-504）
func TestValidateOutputConfig_字段值非法(t *testing.T) {
	err := validateOutputConfig(map[string]any{
		"answer": map[string]any{"type": 123},
	})
	assertLLMCode(t, err, exception.StatusComponentLLMConfigError)
}

// TestNewLLMExecutable_校验失败透传 构造期校验错误原样返回
func TestNewLLMExecutable_校验失败透传(t *testing.T) {
	cfg := llmTestConfig()
	cfg.ResponseFormat = map[string]any{"type": "bogus"}
	_, err := NewLLMExecutable(cfg)
	assertLLMCode(t, err, exception.StatusComponentLLMResponseConfigInvalid)
}

// TestNewLLMExecutable_校验通过 构造成功且字段一致
func TestNewLLMExecutable_校验通过(t *testing.T) {
	cfg := llmTestConfig()
	e, err := NewLLMExecutable(cfg)
	require.NoError(t, err)
	assert.Same(t, cfg, e.getConfig())
	assert.NotNil(t, e.getState())
	assert.False(t, e.initialized)
}

// ──────────────────────────── 助手 ────────────────────────────

// errMsg 提取 BaseError 最终消息（WithMsg 注入面，Python build_error error_msg 等价）
func errMsg(t *testing.T, err error) string {
	t.Helper()
	be, ok := err.(*exception.BaseError)
	require.True(t, ok)
	return be.Message()
}

// ──────────────────────────── Task3/4 工具类族（合并实施） ────────────────────────────

// recoverErr 工具类族以 panic 承载 Python raise——测试断言助手：捕获并返回
func recoverErr(t *testing.T, fn func()) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			be, ok := r.(*exception.BaseError)
			require.True(t, ok, "期望 BaseError panic，实际 %T", r)
			err = be
			return
		}
		require.FailNow(t, "期望 panic(BaseError)，实际正常返回")
	}()
	fn()
	return nil
}

// TestValidateType_六类型矩阵 integer 特判 bool（llm_comp.py:79-86）
func TestValidateType_六类型矩阵(t *testing.T) {
	cases := []struct {
		val   any
		typ   string
		valid bool
	}{
		{map[string]any{}, "object", true},
		{[]any{}, "array", true},
		{"s", "string", true},
		{1, "integer", true},
		{float64(2), "integer", true}, // json 整值 float → integer
		{float64(2.5), "integer", false},
		{true, "integer", false}, // bool 不是 integer（:83）
		{true, "boolean", true},
		{float64(1.5), "number", true},
		{1, "number", true},
		{true, "number", false},
		{"x", "number", false},
	}
	for _, c := range cases {
		if c.valid {
			assert.True(t, validateType(c.val, c.typ), "validateType(%v,%s) 应通过", c.val, c.typ)
		} else {
			assert.False(t, validateType(c.val, c.typ), "validateType(%v,%s) 应拒绝", c.val, c.typ)
		}
	}
	// 未知类型 → 101007（:89-90）
	err := recoverErr(t, func() { validateType(1, "float") })
	assertLLMCode(t, err, exception.StatusComponentLLMConfigInvalid)
}

// TestValidateJSONSchema_递归 required 缺失 + array items 下钻（:97-129）
func TestValidateJSONSchema_递归(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "integer"},
			},
		},
		"required": []any{"missing_field"},
	}
	err := recoverErr(t, func() {
		validateJSONSchema(map[string]any{"items": []any{1}}, schema)
	})
	assertLLMCode(t, err, exception.StatusComponentLLMConfigInvalid)
	assert.Contains(t, errMsg(t, err), "missing required properties")

	err = recoverErr(t, func() {
		validateJSONSchema(map[string]any{"items": []any{1, "x"}, "missing_field": 1}, schema)
	})
	assertLLMCode(t, err, exception.StatusComponentLLMConfigInvalid)
	assert.Contains(t, errMsg(t, err), "invalid array item 1")

	// 无 type 键（:98-99）
	err = recoverErr(t, func() { validateJSONSchema(1, map[string]any{}) })
	assert.Contains(t, errMsg(t, err), "schema must have 'type' key")
}

// TestGenerateJSONSchema required 默认 true、items/properties 透传（:142-165）
func TestGenerateJSONSchema(t *testing.T) {
	schema := generateJSONSchema(map[string]any{
		"name": map[string]any{"type": "string", "description": "名称"},
		"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"meta": map[string]any{"type": "object", "properties": map[string]any{"k": map[string]any{"type": "string"}}},
		"opt":  map[string]any{"type": "string", "required": false},
	})
	assert.Equal(t, []string{"name", "tags", "meta"}, schema["required"])
	props := schema["properties"].(map[string]any)
	assert.Contains(t, props["tags"], "items")
	assert.Contains(t, props["meta"], "properties")
	assert.Equal(t, "名称", props["name"].(map[string]any)["description"])
}

// TestParseJSONContent_围栏剥离（:182-196）
func TestParseJSONContent_围栏剥离(t *testing.T) {
	out := parseJSONContent("```json\n{\"a\": 1}\n```")
	assert.Equal(t, float64(1), out["a"])
	out = parseJSONContent("  {\"a\": 2}  ")
	assert.Equal(t, float64(2), out["a"])
	out = parseJSONContent("```\n{\"a\": 3}\n```")
	assert.Equal(t, float64(3), out["a"])
}

// TestParseJSONContent_解析失败敏感脱敏 敏感模式（默认）下报错不带原文（:176-179）
func TestParseJSONContent_解析失败敏感脱敏(t *testing.T) {
	raw := "{\"a\": not-json"
	err := recoverErr(t, func() { parseJSONContent(raw) })
	assertLLMCode(t, err, exception.StatusComponentLLMConfigInvalid)
	assert.NotContains(t, errMsg(t, err), raw)
}

// TestFormatResponse_三分派（:205-215）
func TestFormatResponse_三分派(t *testing.T) {
	out := formatResponse("正文", map[string]any{"type": "text"}, map[string]any{"answer": nil})
	assert.Equal(t, "正文", out["answer"])

	err := recoverErr(t, func() {
		formatResponse("[1,2]", map[string]any{"type": "text"}, map[string]any{"a": nil, "b": nil})
	})
	assertLLMCode(t, err, exception.StatusComponentLLMConfigInvalid)

	err = recoverErr(t, func() {
		formatResponse("x", map[string]any{"type": "yaml"}, map[string]any{"a": nil})
	})
	assertLLMCode(t, err, exception.StatusComponentLLMConfigInvalid)
}

// TestFormatJSONResponse_扁平配置 提取声明字段+剔除未声明 key（:233-241,256-277）
func TestFormatJSONResponse_扁平配置(t *testing.T) {
	outputConfig := map[string]any{
		"name": map[string]any{"type": "string", "required": true},
		"meta": map[string]any{"type": "object", "properties": map[string]any{"k": map[string]any{"type": "integer"}}},
	}
	out := formatJSONResponse(`{"name": "x", "meta": {"k": 1, "extra": 2}, "noise": true}`, outputConfig)
	meta := out["meta"].(map[string]any)
	assert.NotContains(t, meta, "extra", "未声明 key 应被剔除（海象怪癖路径）")
	assert.NotContains(t, out, "noise", "未声明字段不应出现在输出")
	assert.Equal(t, "x", out["name"])
}

// TestFormatJSONResponse_缺required 敏感模式文案（:271-275）
func TestFormatJSONResponse_缺required(t *testing.T) {
	outputConfig := map[string]any{"name": map[string]any{"type": "string", "required": true}}
	err := recoverErr(t, func() { formatJSONResponse(`{}`, outputConfig) })
	assertLLMCode(t, err, exception.StatusComponentLLMConfigInvalid)
	assert.Contains(t, errMsg(t, err), "missing required fields")
}

// TestFormatJSONResponse_object型schema 直用 output_config 校验+格式化（:234-236）
func TestFormatJSONResponse_object型schema(t *testing.T) {
	objectSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
		},
		"required": []any{"a"},
	}
	out := formatJSONResponse(`{"a": "v"}`, objectSchema)
	assert.Equal(t, "v", out["a"])
	// 违反 schema → 校验失败
	err := recoverErr(t, func() { formatJSONResponse(`{"a": 1}`, objectSchema) })
	assertLLMCode(t, err, exception.StatusComponentLLMConfigInvalid)
}

// TestFormatJSONResponse_空配置拒绝（:227-229）
func TestFormatJSONResponse_空配置拒绝(t *testing.T) {
	err := recoverErr(t, func() { formatJSONResponse(`{}`, nil) })
	assert.Contains(t, errMsg(t, err), "at least one field")
}

// TestFormatWithSchema_default填充 object schema 带默认值字段补齐（D-18-1 等价面）
func TestFormatWithSchema_default填充(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "string", "default": "dflt"},
		},
	}
	out := formatWithSchema(map[string]any{"a": "v"}, schema)
	assert.Equal(t, "v", out["a"])
	assert.Equal(t, "dflt", out["b"])
}

// ──────────────────────────── Task5 提示词格式化 + 流式状态 ────────────────────────────

// newMsgHistory 构造历史消息
func newMsgHistory(contents ...string) []llmschema.BaseMessage {
	out := make([]llmschema.BaseMessage, 0, len(contents))
	for _, c := range contents {
		out = append(out, llmschema.NewUserMessage(c))
	}
	return out
}

// TestFormatPrompt_json注入 最后一条 user 消息被原地改写（llm_comp.py:356-372）
func TestFormatPrompt_json注入(t *testing.T) {
	history := newMsgHistory("第一条", "真正的问题")
	outputConfig := map[string]any{"name": map[string]any{"type": "string"}}
	out := formatPrompt(history, map[string]any{"type": "json"}, outputConfig)
	content := out[1].GetContent().String()
	assert.Contains(t, content, "真正的问题")
	assert.Contains(t, content, "properties", "占位符应替换为实际 schema JSON")
	// 非最后消息不改写
	first := out[0].GetContent().String()
	assert.Equal(t, "第一条", first)
	// 原地写回：入参切片同一对象
	assert.Same(t, history[1], out[1])
}

// TestFormatPrompt_text原样 text 模式不改写（:340-341）
func TestFormatPrompt_text原样(t *testing.T) {
	history := newMsgHistory("q")
	out := formatPrompt(history, map[string]any{"type": "text"}, nil)
	c := out[0].GetContent().String()
	assert.Equal(t, "q", c)
}

// TestFormatPrompt_无user消息 不改写（:344-345）
func TestFormatPrompt_无user消息(t *testing.T) {
	history := []llmschema.BaseMessage{llmschema.NewSystemMessage("s")}
	out := formatPrompt(history, map[string]any{"type": "json"}, map[string]any{})
	c := out[0].GetContent().String()
	assert.Equal(t, "s", c)
}

// TestFormatPrompt_markdown注入 默认 markdown 指令（:349-354）
func TestFormatPrompt_markdown注入(t *testing.T) {
	history := newMsgHistory("q")
	out := formatPrompt(history, map[string]any{"type": "markdown"}, map[string]any{})
	c := out[0].GetContent().String()
	assert.Contains(t, c, "markdown format")
	assert.Contains(t, c, "q")
}

// TestFormatPrompt_自定义instruction 自带指令优先（:351-352,363-364）
func TestFormatPrompt_自定义instruction(t *testing.T) {
	history := newMsgHistory("q")
	rf := map[string]any{"type": "json", "jsonInstruction": "OUT ${json_schema} ASK ${query}"}
	out := formatPrompt(history, rf, map[string]any{"a": map[string]any{"type": "string"}})
	c := out[0].GetContent().String()
	assert.Contains(t, c, "OUT")
	// 默认模板不再出现
	assert.NotContains(t, c, "Carefully consider")
}

// TestFormatPrompt_json_object型config_type object 型直接用作 schema（:357-359）
func TestFormatPrompt_json_object型config_type(t *testing.T) {
	history := newMsgHistory("q")
	objectSchema := map[string]any{"type": "object", "properties": map[string]any{}}
	out := formatPrompt(history, map[string]any{"type": "json"}, objectSchema)
	c := out[0].GetContent().String()
	// object 型 config 的 json 序列化含 "properties" 键
	assert.Contains(t, c, "properties")
}

// TestLLMExecutableState_累积与清空（:404-424）
func TestLLMExecutableState_累积与清空(t *testing.T) {
	st := newLLMExecutableState()
	res := st.BuildFinalResult(map[string]any{"type": "text"}, map[string]any{"a": nil})
	assert.Empty(t, res)
	st.AccumulateContent("hel")
	st.AccumulateContent("lo")
	res = st.BuildFinalResult(map[string]any{"type": "text"}, map[string]any{"answer": nil})
	assert.Equal(t, "hello", res["answer"])
	st.Clear()
	assert.Equal(t, "", st.accumulatedContent)
}
