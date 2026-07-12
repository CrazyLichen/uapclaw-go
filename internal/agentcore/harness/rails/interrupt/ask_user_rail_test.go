package interrupt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 构造函数 ────────────────────────────

// TestNewAskUserRail 验证默认拦截 "ask_user"
func TestNewAskUserRail(t *testing.T) {
	r := NewAskUserRail()
	assert.Contains(t, r.toolNames, "ask_user")
	assert.Equal(t, 90, r.Priority())
}

// TestNewAskUserRail_自定义工具名 验证传入自定义工具名
func TestNewAskUserRail_自定义工具名(t *testing.T) {
	r := NewAskUserRail("custom_tool", "another_tool")
	assert.Contains(t, r.toolNames, "custom_tool")
	assert.Contains(t, r.toolNames, "another_tool")
	assert.NotContains(t, r.toolNames, "ask_user")
}

// ──────────────────────────── resolveInterrupt ────────────────────────────

// TestAskUserRail_resolveInterrupt_无输入中断 验证 userInput=nil → InterruptResult
func TestAskUserRail_resolveInterrupt_无输入中断(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{"questions":[{"question":"你喜欢什么？"}]}`}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, nil, nil)
	assert.IsType(t, &InterruptResult{}, decision)
	interruptResult := decision.(*InterruptResult)
	assert.Equal(t, "", interruptResult.Request.GetMessage()) // AskUserRequest message 为空
}

// TestAskUserRail_resolveInterrupt_有效输入拒绝 验证 AskUserPayload 输入 → RejectResult
func TestAskUserRail_resolveInterrupt_有效输入拒绝(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"你喜欢什么？"}]}`,
	}
	userInput := &AskUserPayload{Answers: map[string]string{"你喜欢什么？": "Go"}}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	assert.IsType(t, &RejectResult{}, decision)
	rejectResult := decision.(*RejectResult)
	assert.Contains(t, rejectResult.ToolResult, "User has answered")
	assert.Contains(t, rejectResult.ToolResult, "Go")
}

// TestAskUserRail_resolveInterrupt_字符串输入 验证 string 输入解析为 AskUserPayload
func TestAskUserRail_resolveInterrupt_字符串输入(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"你的名字？"}]}`,
	}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, "Alice", nil)
	assert.IsType(t, &RejectResult{}, decision)
}

// TestAskUserRail_resolveInterrupt_空字符串中断 验证空字符串 → InterruptResult
func TestAskUserRail_resolveInterrupt_空字符串中断(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, "", nil)
	assert.IsType(t, &InterruptResult{}, decision)
}

// TestAskUserRail_resolveInterrupt_Dict输入 验证 map 输入解析
func TestAskUserRail_resolveInterrupt_Dict输入(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}
	userInput := map[string]any{
		"answers": map[string]any{
			"问题1": "回答1",
		},
	}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	assert.IsType(t, &RejectResult{}, decision)
}

// TestAskUserRail_resolveInterrupt_Dict输入无Answers 验证 map 无 answers 字段
func TestAskUserRail_resolveInterrupt_Dict输入无Answers(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}
	userInput := map[string]any{
		"other": "value",
	}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	// 无 answers 字段 → 空的 AskUserPayload → 无有效答案 → 中断
	assert.IsType(t, &InterruptResult{}, decision)
}

// TestAskUserRail_resolveInterrupt_无效类型 验证不支持的输入类型 → 中断
func TestAskUserRail_resolveInterrupt_无效类型(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, 42, nil)
	assert.IsType(t, &InterruptResult{}, decision)
}

// TestAskUserRail_resolveInterrupt_字符串输入无问题 验证字符串输入但无 questions → 空 AskUserPayload
func TestAskUserRail_resolveInterrupt_字符串输入无问题(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, "回答", nil)
	// 字符串输入但无 questions → 空 AskUserPayload → 中断
	assert.IsType(t, &InterruptResult{}, decision)
}

// ──────────────────────────── formatToolResult ────────────────────────────

// TestAskUserRail_formatToolResult 验证格式化输出
func TestAskUserRail_formatToolResult(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"你喜欢什么？"},{"question":"你的名字？"}]}`,
	}
	payload := &AskUserPayload{Answers: map[string]string{
		"你喜欢什么？": "Go",
		"你的名字？":  "Alice",
	}}

	result := r.formatToolResult(toolCall, payload)
	assert.Contains(t, result, "User has answered")
	assert.Contains(t, result, `"你喜欢什么？"="Go"`)
	assert.Contains(t, result, `"你的名字？"="Alice"`)
}

// TestAskUserRail_formatToolResult_无问题 验证无问题时的格式化
func TestAskUserRail_formatToolResult_无问题(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}
	payload := &AskUserPayload{Answers: map[string]string{"q": "a"}}

	result := r.formatToolResult(toolCall, payload)
	// 无问题时直接输出 answers map
	assert.NotEmpty(t, result)
}

// TestAskUserRail_formatToolResult_问题无Question字段 验证问题 map 缺少 question 字段
func TestAskUserRail_formatToolResult_问题无Question字段(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions":[{"header":"测试"}]}`,
	}
	payload := &AskUserPayload{Answers: map[string]string{}}

	result := r.formatToolResult(toolCall, payload)
	assert.Contains(t, result, "User has answered")
}

// TestAskUserPayload_JSON序列化 验证 AskUserPayload JSON 序列化
func TestAskUserPayload_JSON序列化(t *testing.T) {
	payload := &AskUserPayload{Answers: map[string]string{"问题1": "回答1"}}
	data, err := json.Marshal(payload)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"answers"`)
	assert.Contains(t, string(data), `"问题1"`)
}

// TestAskUserRail_resolveInterrupt_Dict输入Answers非Dict 验证 answers 字段非 dict
func TestAskUserRail_resolveInterrupt_Dict输入Answers非Dict(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}
	userInput := map[string]any{
		"answers": "not_a_dict",
	}

	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	// answers 非 dict → parseUserInputDict 返回空 AskUserPayload → 中断
	assert.IsType(t, &InterruptResult{}, decision)
}

// ──────────────────────────── parseToolArgs ────────────────────────────

// TestAskUserRail_parseToolArgs 验证 JSON 参数解析
func TestAskUserRail_parseToolArgs(t *testing.T) {
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"你喜欢什么？"}],"other":"value"}`,
	}

	args := parseToolArgs(toolCall)
	assert.Contains(t, args, "questions")
	assert.Contains(t, args, "other")
}

// TestAskUserRail_parseToolArgs_空参数 验证空参数
func TestAskUserRail_parseToolArgs_空参数(t *testing.T) {
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}
	args := parseToolArgs(toolCall)
	assert.Empty(t, args)
}

// TestAskUserRail_parseToolArgs_nil 验证 nil ToolCall
func TestAskUserRail_parseToolArgs_nil(t *testing.T) {
	args := parseToolArgs(nil)
	assert.Empty(t, args)
}

// TestAskUserRail_parseToolArgs_错误JSON 验证无效 JSON
func TestAskUserRail_parseToolArgs_错误JSON(t *testing.T) {
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `invalid json`}
	args := parseToolArgs(toolCall)
	assert.Empty(t, args)
}

// TestAskUserRail_parseUserInputDict_单问题answer字段 验证单问题模式下 answer 字段
func TestAskUserRail_parseUserInputDict_单问题answer字段(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"你的名字？"}]}`,
	}
	userInput := map[string]any{"answer": "Alice"}

	payload, ok := r.parseUserInputDict(userInput, toolCall)
	assert.True(t, ok)
	assert.Equal(t, "Alice", payload.Answers["你的名字？"])
}

// ──────────────────────────── buildAskRequest ────────────────────────────

// TestAskUserRail_buildAskRequest 验证请求构建
func TestAskUserRail_buildAskRequest(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"你喜欢什么？"}]}`,
	}

	request := r.buildAskRequest(toolCall)
	assert.Equal(t, "", request.Message)
	assert.NotNil(t, request.PayloadSchema)
}

// ──────────────────────────── AskUserPayload Schema ────────────────────────────

// TestAskUserPayloadSchema 验证 AskUserPayload 的 JSON Schema
func TestAskUserPayloadSchema(t *testing.T) {
	schema := askUserPayloadSchema()
	assert.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, props, "answers")
}

// ──────────────────────────── InterruptDecision 接口 ────────────────────────────

// TestAskUserRail_InterruptRequest接口 验证 InterruptResult.Request 满足 InterruptRequester
func TestAskUserRail_InterruptRequest接口(t *testing.T) {
	r := NewAskUserRail()
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: `{}`}
	decision := r.resolveAskUserInterrupt(context.TODO(), nil, toolCall, nil, nil)
	interruptResult, ok := decision.(*InterruptResult)
	assert.True(t, ok)
	// InterruptRequest 满足 InterruptRequester 接口
	var _ saschema.InterruptRequester = interruptResult.Request
}
