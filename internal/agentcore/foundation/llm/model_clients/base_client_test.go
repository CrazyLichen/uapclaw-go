package model_clients

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// newTestClientEmbed 创建测试用 BaseClientEmbed。
func newTestClientEmbed() *BaseClientEmbed {
	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-4"))
	cc := llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	e, err := NewBaseClientEmbed(mc, cc, WithClientName("TestClient"))
	if err != nil {
		panic(err)
	}
	return e
}

// ──── ValidateConfig 测试 ────

// TestValidateConfig_成功 测试正常配置校验。
func TestValidateConfig_成功(t *testing.T) {
	e := newTestClientEmbed()
	if err := e.ValidateConfig(); err != nil {
		t.Errorf("正常配置校验不应报错: %v", err)
	}
}

// TestValidateConfig_APIKey缺失 测试缺少 API Key。
func TestValidateConfig_APIKey缺失(t *testing.T) {
	mc := llmschema.NewModelRequestConfig()
	cc := llmschema.NewModelClientConfig("OpenAI", "", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	_, err := NewBaseClientEmbed(mc, cc)
	if err == nil {
		t.Error("缺少 api_key 应报错")
	}
	// 验证错误类型
	if _, ok := err.(*exception.BaseError); !ok {
		t.Error("错误应为 BaseError 类型")
	}
}

// TestValidateConfig_APIBase缺失 测试缺少 API Base。
func TestValidateConfig_APIBase缺失(t *testing.T) {
	mc := llmschema.NewModelRequestConfig()
	cc := llmschema.NewModelClientConfig("OpenAI", "test-key", "",
		llmschema.WithVerifySSL(false),
	)
	_, err := NewBaseClientEmbed(mc, cc)
	if err == nil {
		t.Error("缺少 api_base 应报错")
	}
}

// TestValidateConfig_验证SSL无证书 测试 verify_ssl=true 但无 ssl_cert。
func TestValidateConfig_验证SSL无证书(t *testing.T) {
	mc := llmschema.NewModelRequestConfig()
	cc := llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(true),
	)
	_, err := NewBaseClientEmbed(mc, cc)
	if err == nil {
		t.Error("verify_ssl=true 但无 ssl_cert 应报错")
	}
}

// ──── ConvertMessagesToDict 测试 ────

// TestConvertMessagesToDict_文本消息 测试纯文本输入。
func TestConvertMessagesToDict_文本消息(t *testing.T) {
	e := newTestClientEmbed()
	p := NewTextMessagesParam("你好")

	result, err := e.ConvertMessagesToDict(p)
	if err != nil {
		t.Fatalf("ConvertMessagesToDict 报错: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("结果长度 = %d, 期望 1", len(result))
	}
	if result[0]["role"] != "user" {
		t.Errorf("role = %v, 期望 user", result[0]["role"])
	}
	if result[0]["content"] != "你好" {
		t.Errorf("content = %v, 期望 你好", result[0]["content"])
	}
}

// TestConvertMessagesToDict_字典消息 测试 dict 列表输入（直接透传）。
func TestConvertMessagesToDict_字典消息(t *testing.T) {
	e := newTestClientEmbed()
	dicts := []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi"},
	}
	p := NewDictsMessagesParam(dicts)

	result, err := e.ConvertMessagesToDict(p)
	if err != nil {
		t.Fatalf("ConvertMessagesToDict 报错: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("结果长度 = %d, 期望 2", len(result))
	}
}

// TestConvertMessagesToDict_消息对象 测试消息列表输入。
func TestConvertMessagesToDict_消息对象(t *testing.T) {
	e := newTestClientEmbed()
	p := NewMessagesParam(
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("hi"),
	)

	result, err := e.ConvertMessagesToDict(p)
	if err != nil {
		t.Fatalf("ConvertMessagesToDict 报错: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("结果长度 = %d, 期望 2", len(result))
	}
	if result[0]["role"] != "user" {
		t.Errorf("第一条消息 role = %v, 期望 user", result[0]["role"])
	}
	if result[1]["role"] != "assistant" {
		t.Errorf("第二条消息 role = %v, 期望 assistant", result[1]["role"])
	}
}

// TestConvertMessagesToDict_带ToolCalls的助手消息 测试 AssistantMessage 带 tool_calls。
func TestConvertMessagesToDict_带ToolCalls的助手消息(t *testing.T) {
	e := newTestClientEmbed()
	assistantMsg := llmschema.NewAssistantMessage("", llmschema.WithToolCalls([]*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "get_weather", `{"city":"Beijing"}`),
	}))
	p := NewMessagesParam(assistantMsg)

	result, err := e.ConvertMessagesToDict(p)
	if err != nil {
		t.Fatalf("ConvertMessagesToDict 报错: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("结果长度 = %d, 期望 1", len(result))
	}

	toolCalls, ok := result[0]["tool_calls"].([]map[string]any)
	if !ok {
		t.Fatal("tool_calls 应为 []map[string]any 类型")
	}
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls 数量 = %d, 期望 1", len(toolCalls))
	}
	if toolCalls[0]["id"] != "call_1" {
		t.Errorf("tool_calls[0].id = %v, 期望 call_1", toolCalls[0]["id"])
	}
	// OpenAI 嵌套格式检查
	fn, ok := toolCalls[0]["function"].(map[string]any)
	if !ok {
		t.Fatal("tool_calls[0].function 应为 map[string]any 类型")
	}
	if fn["name"] != "get_weather" {
		t.Errorf("function.name = %v, 期望 get_weather", fn["name"])
	}
}

// TestConvertMessagesToDict_工具消息 测试 ToolMessage 带 tool_call_id。
func TestConvertMessagesToDict_工具消息(t *testing.T) {
	e := newTestClientEmbed()
	toolMsg := llmschema.NewToolMessage("call_1", `{"temp": 25}`)
	p := NewMessagesParam(toolMsg)

	result, err := e.ConvertMessagesToDict(p)
	if err != nil {
		t.Fatalf("ConvertMessagesToDict 报错: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("结果长度 = %d, 期望 1", len(result))
	}
	if result[0]["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v, 期望 call_1", result[0]["tool_call_id"])
	}
}

// TestConvertMessagesToDict_空消息 测试空消息输入。
func TestConvertMessagesToDict_空消息(t *testing.T) {
	e := newTestClientEmbed()
	p := MessagesParam{}

	_, err := e.ConvertMessagesToDict(p)
	if err == nil {
		t.Error("空消息应报错")
	}
}

// TestConvertMessagesToDict_推理内容 测试 AssistantMessage 带 reasoning_content。
func TestConvertMessagesToDict_推理内容(t *testing.T) {
	e := newTestClientEmbed()
	assistantMsg := llmschema.NewAssistantMessage("答案", llmschema.WithReasoningContent("思考过程..."))
	p := NewMessagesParam(assistantMsg)

	result, err := e.ConvertMessagesToDict(p)
	if err != nil {
		t.Fatalf("ConvertMessagesToDict 报错: %v", err)
	}
	if result[0]["reasoning_content"] != "思考过程..." {
		t.Errorf("reasoning_content = %v, 期望 思考过程...", result[0]["reasoning_content"])
	}
}

// ──── ConvertToolsToDict 测试 ────

// TestConvertToolsToDict_Nil 测试 nil 工具列表。
func TestConvertToolsToDict_Nil(t *testing.T) {
	e := newTestClientEmbed()
	result := e.ConvertToolsToDict(nil)
	if result != nil {
		t.Error("nil 工具列表应返回 nil")
	}
}

// TestConvertToolsToDict_空 测试空工具列表。
func TestConvertToolsToDict_空(t *testing.T) {
	e := newTestClientEmbed()
	result := e.ConvertToolsToDict([]commonschema.ToolInfoInterface{})
	if result != nil {
		t.Error("空工具列表应返回 nil")
	}
}

// TestConvertToolsToDict_ToolInfo 测试 ToolInfo 列表转换。
func TestConvertToolsToDict_ToolInfo(t *testing.T) {
	e := newTestClientEmbed()
	tools := []commonschema.ToolInfoInterface{
		commonschema.NewToolInfo("get_weather", "获取天气", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{"type": "string"},
			},
		}),
	}
	result := e.ConvertToolsToDict(tools)
	if len(result) != 1 {
		t.Fatalf("结果长度 = %d, 期望 1", len(result))
	}
	if result[0]["type"] != "function" {
		t.Errorf("type = %v, 期望 function", result[0]["type"])
	}
	fn, ok := result[0]["function"].(map[string]any)
	if !ok {
		t.Fatal("function 应为 map[string]any 类型")
	}
	if fn["name"] != "get_weather" {
		t.Errorf("function.name = %v, 期望 get_weather", fn["name"])
	}
}

// ──── BuildRequestParams 测试 ────

// TestBuildRequestParams_基本 测试基础参数构建。
func TestBuildRequestParams_基本(t *testing.T) {
	e := newTestClientEmbed()
	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams()

	result, err := e.BuildRequestParams(context.Background(), messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if result["model"] != "gpt-4" {
		t.Errorf("model = %v, 期望 gpt-4", result["model"])
	}
	if result["stream"] != false {
		t.Errorf("stream = %v, 期望 false", result["stream"])
	}
	if result["messages"] == nil {
		t.Error("messages 不应为 nil")
	}
}

// TestBuildRequestParams_带工具 测试带工具的参数构建。
func TestBuildRequestParams_带工具(t *testing.T) {
	e := newTestClientEmbed()
	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams(
		WithTools(commonschema.NewToolInfo("get_weather", "获取天气", nil)),
	)

	result, err := e.BuildRequestParams(context.Background(), messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if result["tools"] == nil {
		t.Error("有 tools 时 tools 字段不应为 nil")
	}
	if result["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, 期望 auto", result["tool_choice"])
	}
}

// TestBuildRequestParams_覆盖模型 测试方法参数覆盖 model_config。
func TestBuildRequestParams_覆盖模型(t *testing.T) {
	e := newTestClientEmbed()
	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams(WithInvokeModel("gpt-3.5-turbo"))

	result, err := e.BuildRequestParams(context.Background(), messagesDict, params, true)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if result["model"] != "gpt-3.5-turbo" {
		t.Errorf("model = %v, 期望 gpt-3.5-turbo", result["model"])
	}
}

// TestBuildRequestParams_无模型 测试无模型名称报错。
func TestBuildRequestParams_无模型(t *testing.T) {
	mc := llmschema.NewModelRequestConfig() // ModelName 为空
	cc := llmschema.NewModelClientConfig("OpenAI", "key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	e, err := NewBaseClientEmbed(mc, cc)
	if err != nil {
		t.Fatal(err)
	}

	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams() // Model 也为空

	_, err = e.BuildRequestParams(context.Background(), messagesDict, params, false)
	if err == nil {
		t.Error("无模型名称应报错")
	}
}

// TestBuildRequestParams_额外参数合并 测试 Extra 合并。
func TestBuildRequestParams_额外参数合并(t *testing.T) {
	mc := llmschema.NewModelRequestConfig(
		llmschema.WithModelName("gpt-4"),
		llmschema.WithRequestExtra(map[string]any{"top_k": 50}),
	)
	cc := llmschema.NewModelClientConfig("OpenAI", "key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	e, err := NewBaseClientEmbed(mc, cc)
	if err != nil {
		t.Fatal(err)
	}

	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams(
		WithInvokeExtra(map[string]any{"frequency_penalty": 0.5}),
	)

	result, err := e.BuildRequestParams(context.Background(), messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if result["top_k"] != 50 {
		t.Errorf("top_k = %v, 期望 50", result["top_k"])
	}
	if result["frequency_penalty"] != 0.5 {
		t.Errorf("frequency_penalty = %v, 期望 0.5", result["frequency_penalty"])
	}
}

// ──── ExtractCostInfo 测试 ────

// TestExtractCostInfo_简单费用 测试简单数值 cost。
func TestExtractCostInfo_简单费用(t *testing.T) {
	obj := map[string]any{"cost": 0.05}
	inputCost, outputCost, totalCost := ExtractCostInfo(obj)
	if totalCost != 0.05 {
		t.Errorf("totalCost = %f, 期望 0.05", totalCost)
	}
	if inputCost != 0 || outputCost != 0 {
		t.Errorf("inputCost=%f, outputCost=%f, 期望均为 0", inputCost, outputCost)
	}
}

// TestExtractCostInfo_费用对象 测试 cost 对象。
func TestExtractCostInfo_费用对象(t *testing.T) {
	obj := map[string]any{
		"cost": map[string]any{
			"input_cost":  0.01,
			"output_cost": 0.03,
			"total_cost":  0.04,
		},
	}
	inputCost, outputCost, totalCost := ExtractCostInfo(obj)
	if inputCost != 0.01 {
		t.Errorf("inputCost = %f, 期望 0.01", inputCost)
	}
	if outputCost != 0.03 {
		t.Errorf("outputCost = %f, 期望 0.03", outputCost)
	}
	if totalCost != 0.04 {
		t.Errorf("totalCost = %f, 期望 0.04", totalCost)
	}
}

// TestExtractCostInfo_费用详情 测试 cost_details 兜底。
func TestExtractCostInfo_费用详情(t *testing.T) {
	obj := map[string]any{
		"cost_details": map[string]any{
			"upstream_inference_prompt_cost":      0.02,
			"upstream_inference_completions_cost": 0.06,
		},
	}
	inputCost, outputCost, totalCost := ExtractCostInfo(obj)
	if inputCost != 0.02 {
		t.Errorf("inputCost = %f, 期望 0.02", inputCost)
	}
	if outputCost != 0.06 {
		t.Errorf("outputCost = %f, 期望 0.06", outputCost)
	}
	if totalCost != 0.08 {
		t.Errorf("totalCost = %f, 期望 0.08", totalCost)
	}
}

// TestExtractCostInfo_无费用 测试无费用信息。
func TestExtractCostInfo_无费用(t *testing.T) {
	obj := map[string]any{}
	inputCost, outputCost, totalCost := ExtractCostInfo(obj)
	if inputCost != 0 || outputCost != 0 || totalCost != 0 {
		t.Errorf("无费用信息时所有值应为 0, got input=%f output=%f total=%f", inputCost, outputCost, totalCost)
	}
}

// ──── WithSkipValidate 测试 ────

// TestWithSkipValidate_跳过校验 测试 WithSkipValidate 跳过校验。
func TestWithSkipValidate_跳过校验(t *testing.T) {
	mc := llmschema.NewModelRequestConfig()
	// api_key 和 api_base 均为空，正常情况应报错
	cc := llmschema.NewModelClientConfig("intelli_router", "", "",
		llmschema.WithVerifySSL(false),
	)

	// 不使用 WithSkipValidate 应报错
	_, err := NewBaseClientEmbed(mc, cc)
	if err == nil {
		t.Error("缺少 api_key/api_base 应报错")
	}

	// 使用 WithSkipValidate 应跳过校验
	e, err := NewBaseClientEmbed(mc, cc, WithSkipValidate())
	if err != nil {
		t.Errorf("WithSkipValidate 应跳过校验，但报错: %v", err)
	}
	if !e.skipValidate {
		t.Error("skipValidate 应为 true")
	}
}

// TestWithSkipValidate_直接校验配置 测试 ValidateConfig 在 skipValidate 下的行为。
func TestWithSkipValidate_直接校验配置(t *testing.T) {
	mc := llmschema.NewModelRequestConfig()
	cc := llmschema.NewModelClientConfig("intelli_router", "", "",
		llmschema.WithVerifySSL(false),
	)

	e := &BaseClientEmbed{
		ModelConfig:  mc,
		ClientConfig: cc,
		clientName:   "TestSkipValidate",
		skipValidate: true,
	}

	// skipValidate=true 时 ValidateConfig 应返回 nil
	if err := e.ValidateConfig(); err != nil {
		t.Errorf("skipValidate=true 时 ValidateConfig 不应报错: %v", err)
	}

	// skipValidate=false 时 ValidateConfig 应报错
	e.skipValidate = false
	if err := e.ValidateConfig(); err == nil {
		t.Error("skipValidate=false 且 api_key/api_base 为空时应报错")
	}
}

// TestGetModelName_有ModelConfig 验证 GetModelName 优先返回 ModelConfig.ModelName。
func TestGetModelName_有ModelConfig(t *testing.T) {
	e := newTestClientEmbed()
	got := e.GetModelName()
	if got != "gpt-4" {
		t.Errorf("GetModelName() = %q, want %q", got, "gpt-4")
	}
}

// TestGetModelName_无ModelConfig 验证 GetModelName 降级返回 clientName。
func TestGetModelName_无ModelConfig(t *testing.T) {
	mc := llmschema.NewModelRequestConfig() // 无 ModelName
	cc := llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	e, _ := NewBaseClientEmbed(mc, cc, WithClientName("MyClient"))
	got := e.GetModelName()
	if got != "MyClient" {
		t.Errorf("GetModelName() = %q, want %q", got, "MyClient")
	}
}

// TestGetModelName_全部为空 验证 GetModelName 最终降级返回 BaseModelClient。
func TestGetModelName_全部为空(t *testing.T) {
	e := &BaseClientEmbed{}
	got := e.GetModelName()
	if got != "BaseModelClient" {
		t.Errorf("GetModelName() = %q, want %q", got, "BaseModelClient")
	}
}
