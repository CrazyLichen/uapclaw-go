package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
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
	assert.Equal(t, []string{"meta", "name", "tags"}, schema["required"], "D-18-6：Go map 无序 → required 排序")
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

// ──────────────────────────── Task6 invoke 链 ────────────────────────────

// fakeModelClient 可控 Invoke 返回的模型桩（满足 llModelInvoker 面）
type fakeModelClient struct {
	respContent string
	invokeErr   error
	invokeCalls int
	lastInputs  []llmschema.BaseMessage
}

func (f *fakeModelClient) Invoke(ctx context.Context, messages modelclients.MessagesParam, opts ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	f.invokeCalls++
	f.lastInputs = messages.Messages()
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	return llmschema.NewAssistantMessage(f.respContent), nil
}

func (f *fakeModelClient) Stream(ctx context.Context, messages modelclients.MessagesParam, opts ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	c := llmschema.NewAssistantMessageChunk(f.respContent)
	ch <- c
	close(ch)
	return ch, nil
}

// newTestExecutable 构造带 fake 模型的执行体（WithModelResolver 注入缝）
func newTestExecutable(t *testing.T, cfg *LLMCompConfig, fake llModelInvoker) *LLMExecutable {
	t.Helper()
	e, err := NewLLMExecutable(cfg)
	require.NoError(t, err)
	if fake != nil {
		e.llModel = fake
		e.initialized = true
	}
	return e
}

// TestInvoke_全链 懒初始化→日志→上下文写回→格式化（llm_comp.py:524-572 十步）
func TestInvoke_全链(t *testing.T) {
	cfg := llmTestConfig()
	cfg.EnableHistory = true
	e := newTestExecutable(t, cfg, &fakeModelClient{respContent: "模型回答"})
	sess := newTestNodeSession(t)
	e.setSession(sess)
	e.setContext(newFakeModelContext())
	out, err := e.invokeEntry(context.Background(), map[string]any{"q": "hi"}, sess)
	require.NoError(t, err)
	assert.Equal(t, "模型回答", out["answer"])
	assert.True(t, e.initialized)
	// 上下文写回 user + assistant 两条
	msgs, _ := e.context.GetMessages(0, true)
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].GetRole().String())
	assert.Equal(t, "assistant", msgs[1].GetRole().String())
}

// TestInvoke_懒初始化modelID路径 ModelID 走 getModelFn 注入缝
func TestInvoke_懒初始化modelID路径(t *testing.T) {
	cfg := llmTestConfig()
	cfg.ModelID = ptrString("m1")
	cfg.ModelClientConfig = nil
	cfg.ModelConfig = nil
	e, err := NewLLMExecutable(cfg)
	require.NoError(t, err)
	e.getModelFn = func(ctx context.Context, modelID string) (llModelInvoker, error) {
		assert.Equal(t, "m1", modelID)
		return &fakeModelClient{respContent: "ok"}, nil
	}
	sess := newTestNodeSession(t)
	e.setSession(sess)
	out, err := e.invokeEntry(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	assert.Equal(t, "ok", out["answer"])
}

// TestInvoke_双nil配置拒绝 → 101003（llm_comp.py:626-630）
func TestInvoke_双nil配置拒绝(t *testing.T) {
	cfg := llmTestConfig()
	cfg.ModelClientConfig = nil
	cfg.ModelConfig = nil
	e := newTestExecutable(t, cfg, nil)
	sess := newTestNodeSession(t)
	e.setSession(sess)
	_, err := e.invokeEntry(context.Background(), map[string]any{}, sess)
	// Python: createLLMInstance 的 101003 经 _initialize_if_needed 再包装为 101005
	assertLLMCode(t, err, exception.StatusComponentLLMInitFailed)
}

// TestInvoke_调用失败包装 → 101003 敏感模式文案 "invoke llm failed"（:545-557）
func TestInvoke_调用失败包装(t *testing.T) {
	e := newTestExecutable(t, llmTestConfig(), &fakeModelClient{invokeErr: errors.New("boom")})
	sess := newTestNodeSession(t)
	e.setSession(sess)
	_, err := e.invokeEntry(context.Background(), map[string]any{}, sess)
	assertLLMCode(t, err, exception.StatusComponentLLMInvokeCallFailed)
	assert.Equal(t, "invoke llm failed", errMsg(t, err))
}

// TestInvoke_格式化101007转101004（:687-695）：构造期校验合法（text 单字段），
// json 格式下 LLM 输出非法 → formatResponse 抛 101007 → createOutput 转 101004。
// （text 两字段的 101007 在 Python __init__ 即被 validate_response_format 拦截，
// 运行时不可达，故场景取 json 解析失败。）
func TestInvoke_格式化101007转101004(t *testing.T) {
	cfg := llmTestConfig()
	cfg.ResponseFormat = map[string]any{"type": "json"}
	e := newTestExecutable(t, cfg, &fakeModelClient{respContent: "not-json{"})
	sess := newTestNodeSession(t)
	e.setSession(sess)
	_, err := e.invokeEntry(context.Background(), map[string]any{}, sess)
	assertLLMCode(t, err, exception.StatusComponentLLMExecutionProcessError)
}

// TestGetModelInput_模板组装 template_content 组装 + system 头部连续截取
func TestGetModelInput_模板组装(t *testing.T) {
	cfg := llmTestConfig()
	cfg.ResponseFormat = map[string]any{"type": "text"}
	e := newTestExecutable(t, cfg, &fakeModelClient{})
	msgs := e.getModelInput(map[string]any{"q": "hi"})
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].GetRole().String())
	assert.Equal(t, "user", msgs[1].GetRole().String())
}

// TestGetModelInput_userPromptTemplate缺省 模板分支：user 模板 nil → 空用户消息（:640-643）
func TestGetModelInput_userPromptTemplate缺省(t *testing.T) {
	cfg := llmTestConfig()
	cfg.TemplateContent = nil
	cfg.SystemPromptTemplate = llmschema.NewSystemMessage("sys")
	e := newTestExecutable(t, cfg, &fakeModelClient{})
	msgs := e.getModelInput(map[string]any{})
	// system 模板 1 条 + user 缺省空消息 1 条（Python: system prompt + [UserMessage(content="")]）
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].GetRole().String())
	assert.Equal(t, "", msgs[1].GetContent().String())
}

func ptrString(s string) *string { return &s }

// fakeModelContext 可写上下文桩
type fakeModelContext struct {
	iface.ModelContext
	messages []llmschema.BaseMessage
}

func newFakeModelContext() *fakeModelContext { return &fakeModelContext{} }

func (f *fakeModelContext) GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error) {
	return f.messages, nil
}

func (f *fakeModelContext) AddMessages(ctx context.Context, messages []llmschema.BaseMessage, opts ...iface.Option) ([]llmschema.BaseMessage, error) {
	f.messages = append(f.messages, messages...)
	return f.messages, nil
}

// ──────────────────────────── Task7 stream 链 ────────────────────────────

// fakeStreamClient 逐 chunk 可控的流式桩
type fakeStreamClient struct {
	chunks    []string
	streamErr error
}

func (f *fakeStreamClient) Invoke(ctx context.Context, messages modelclients.MessagesParam, opts ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(strings.Join(f.chunks, "")), nil
}

func (f *fakeStreamClient) Stream(ctx context.Context, messages modelclients.MessagesParam, opts ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	ch := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(ch)
		for _, c := range f.chunks {
			ch <- llmschema.NewAssistantMessageChunk(c)
		}
		// 末尾哨兵：流中断错误（foundation/llm StreamError 契约）
	}()
	return ch, nil
}

// drainFrames 消费全部帧
func drainFrames(t *testing.T, ch <-chan graphexe.StreamChunk[any]) ([]any, error) {
	t.Helper()
	var vals []any
	for f := range ch {
		if f.Err != nil {
			return vals, f.Err
		}
		vals = append(vals, f.Val)
	}
	return vals, nil
}

// TestStream_json格式_一次性帧 json 分支整体 Invoke 后单帧（:582-584,707-731）
func TestStream_json格式_一次性帧(t *testing.T) {
	cfg := llmTestConfig()
	cfg.ResponseFormat = map[string]any{"type": "json"}
	cfg.OutputConfig = map[string]any{"name": map[string]any{"type": "string", "required": true}}
	e := newTestExecutable(t, cfg, &fakeStreamClient{chunks: []string{"{\"name\": \"ok\"}"}})
	sess := newTestNodeSession(t)
	e.setSession(sess)
	ch, err := e.streamEntry(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	frames, ferr := drainFrames(t, ch)
	require.NoError(t, ferr)
	require.Len(t, frames, 1)
	assert.Equal(t, "ok", frames[0].(map[string]any)["name"])
}

// TestStream_逐chunk格式化 text 分支每 chunk 一帧（:733-747）
func TestStream_逐chunk格式化(t *testing.T) {
	e := newTestExecutable(t, llmTestConfig(), &fakeStreamClient{chunks: []string{"你", "好"}})
	sess := newTestNodeSession(t)
	e.setSession(sess)
	ch, err := e.streamEntry(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	frames, ferr := drainFrames(t, ch)
	require.NoError(t, ferr)
	require.Len(t, frames, 2)
	// 每 chunk 单独过格式化器（Python :744-747 同为 dict 帧）
	assert.Equal(t, map[string]any{"answer": "你"}, frames[0])
}

// TestStream_cacheStream_出错清状态（:748-751,588-600）
func TestStream_cacheStream_出错清状态(t *testing.T) {
	cfg := llmTestConfig()
	cfg.CacheStream = true
	e := newTestExecutable(t, cfg, &fakeStreamClient{streamErr: errors.New("mid")})
	sess := newTestNodeSession(t)
	e.setSession(sess)
	ch, err := e.streamEntry(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	_, ferr := drainFrames(t, ch)
	require.Error(t, ferr)
	assertLLMCode(t, ferr, exception.StatusComponentLLMInvokeCallFailed)
	assert.Equal(t, "", e.state.accumulatedContent)
}

// TestStream_getStreamOutput 空结果 nil、累积后取回（:602-610）
func TestStream_getStreamOutput(t *testing.T) {
	cfg := llmTestConfig()
	cfg.CacheStream = true
	e := newTestExecutable(t, cfg, &fakeStreamClient{chunks: []string{"ab"}})
	sess := newTestNodeSession(t)
	e.setSession(sess)
	assert.Nil(t, e.getStreamOutput(), "空累积 → nil（Python output=None）")
	ch, err := e.streamEntry(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	_, ferr := drainFrames(t, ch)
	require.NoError(t, ferr)
	out := e.getStreamOutput()
	require.NotNil(t, out)
	assert.Equal(t, "ab", out["answer"])
}

// TestStream_cacheStream_成功累积 json 分支累积后 getStreamOutput 可取格式化结果
func TestStream_cacheStream_成功累积(t *testing.T) {
	cfg := llmTestConfig()
	cfg.CacheStream = true
	cfg.ResponseFormat = map[string]any{"type": "json"}
	cfg.OutputConfig = map[string]any{"name": map[string]any{"type": "string", "required": true}}
	e := newTestExecutable(t, cfg, &fakeStreamClient{chunks: []string{"{\"name\": \"ok\"}"}})
	sess := newTestNodeSession(t)
	e.setSession(sess)
	ch, err := e.streamEntry(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	_, ferr := drainFrames(t, ch)
	require.NoError(t, ferr)
	out := e.getStreamOutput()
	require.NotNil(t, out)
	assert.Equal(t, "ok", out["name"])
}
