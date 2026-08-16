package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── NewStructuredAskUserRail ────────────────────────────

// TestNewStructuredAskUserRail 验证构造函数
func TestNewStructuredAskUserRail(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	assert.NotNil(t, r)
	assert.Equal(t, "cn", r.language)
	// 验证 ResolveInterruptFn 已被覆盖
	assert.NotNil(t, r.AskUserRail.BaseInterruptRail.ResolveInterruptFn)
	// 验证 parentResolve 已保存
	assert.NotNil(t, r.parentResolve)
}

// TestNewStructuredAskUserRail_空语言 验证空语言参数
func TestNewStructuredAskUserRail_空语言(t *testing.T) {
	r := NewStructuredAskUserRail("")
	assert.NotNil(t, r)
	assert.Equal(t, "", r.language)
}

// ──────────────────────────── ExtractQuestions ────────────────────────────

// TestExtractQuestions_有questions 验证提取 questions 列表
func TestExtractQuestions_有questions(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:   "tc1",
		Name: "ask_user",
		Arguments: `{"questions": [{"question": "选择方案", "header": "方案", "options": [{"label": "A"}, {"label": "B"}]}]}`,
	}

	questions := r.ExtractQuestions(toolCall)
	require.Len(t, questions, 1)
	assert.Equal(t, "选择方案", questions[0]["question"])
	assert.Equal(t, "方案", questions[0]["header"])
}

// TestExtractQuestions_无questions 验证无 questions 参数返回 nil
func TestExtractQuestions_无questions(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	questions := r.ExtractQuestions(toolCall)
	assert.Nil(t, questions)
}

// TestExtractQuestions_nilToolCall 验证 nil ToolCall 返回 nil
func TestExtractQuestions_nilToolCall(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	questions := r.ExtractQuestions(nil)
	assert.Nil(t, questions)
}

// TestExtractQuestions_空questions 验证空 questions 列表返回 nil
func TestExtractQuestions_空questions(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": []}`,
	}

	questions := r.ExtractQuestions(toolCall)
	assert.Nil(t, questions)
}

// ──────────────────────────── resolveStructuredInterrupt ────────────────────────────

// TestResolveStructuredInterrupt_无输入中断 验证无用户输入返回中断
func TestResolveStructuredInterrupt_无输入中断(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, nil, nil,
	)

	interruptResult, ok := decision.(*interrupt.InterruptResult)
	require.True(t, ok)
	assert.NotNil(t, interruptResult.Request)
}

// TestResolveStructuredInterrupt_结构化路径 验证结构化输入返回 Reject
func TestResolveStructuredInterrupt_结构化路径(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "选择方案", "header": "方案"}]}`,
	}

	// 用户输入：结构化回答（对齐 Python: {"answers": {"选择方案": "方案A"}}）
	userInput := map[string]any{
		"answers": map[string]any{
			"选择方案": "方案A",
		},
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Equal(t, "选择方案: 方案A", rejectResult.ToolResult)
}

// TestResolveStructuredInterrupt_结构化路径自由文本 验证 __free_text__ 键
func TestResolveStructuredInterrupt_结构化路径自由文本(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	// 字符串输入 → __free_text__（对齐 Python: StructuredAskUserPayload(answers={"__free_text__": user_input})）
	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, "用户自由输入", nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Equal(t, "用户自由输入", rejectResult.ToolResult)
}

// TestResolveStructuredInterrupt_非结构化路径回退 验证无 questions 时回退父类
func TestResolveStructuredInterrupt_非结构化路径回退(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	// 无 questions → 回退父类，无用户输入 → 中断
	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, nil, nil,
	)

	_, ok := decision.(*interrupt.InterruptResult)
	assert.True(t, ok, "非结构化无输入应回退父类返回 InterruptResult")
}

// TestResolveStructuredInterrupt_非结构化路径有输入 验证无 questions 时回退父类（有输入）
func TestResolveStructuredInterrupt_非结构化路径有输入(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	// 无 questions + string 输入 → 对齐 Python: elif isinstance(user_input, str): return self.reject(tool_result=user_input)
	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, "用户回答", nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Equal(t, "用户回答", rejectResult.ToolResult)
}

// TestResolveStructuredInterrupt_dict输入无answers键 验证 dict 输入无 answers 键
func TestResolveStructuredInterrupt_dict输入无answers键(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	// 对齐 Python: Frontend sends answers as {question: selected_option}
	userInput := map[string]any{
		"Q1": "选项A",
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Equal(t, "Q1: 选项A", rejectResult.ToolResult)
}

// TestResolveStructuredInterrupt_AskUserPayload输入 验证 AskUserPayload 输入
func TestResolveStructuredInterrupt_AskUserPayload输入(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	// 对齐 Python: isinstance(user_input, AskUserPayload)
	userInput := &interrupt.AskUserPayload{
		Answers: map[string]string{"Q1": "回答1"},
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Contains(t, rejectResult.ToolResult, "Q1: 回答1")
}

// ──────────────────────────── parseStructuredInput ────────────────────────────

// TestParseStructuredInput_StructuredAskUserPayload 验证 StructuredAskUserPayload 输入
func TestParseStructuredInput_StructuredAskUserPayload(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	payload := &StructuredAskUserPayload{Answers: map[string]string{"Q1": "A1"}}
	result, ok := r.parseStructuredInput(payload)
	assert.True(t, ok)
	assert.Equal(t, "A1", result.Answers["Q1"])
}

// TestParseStructuredInput_DictWithAnswers 验证 dict with answers 输入
func TestParseStructuredInput_DictWithAnswers(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	input := map[string]any{
		"answers": map[string]any{"Q1": "A1"},
	}
	result, ok := r.parseStructuredInput(input)
	assert.True(t, ok)
	assert.Equal(t, "A1", result.Answers["Q1"])
}

// TestParseStructuredInput_DictWithoutAnswers 验证 dict without answers 输入
func TestParseStructuredInput_DictWithoutAnswers(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	input := map[string]any{"Q1": "选项A"}
	result, ok := r.parseStructuredInput(input)
	assert.True(t, ok)
	assert.Equal(t, "选项A", result.Answers["Q1"])
}

// TestParseStructuredInput_String 验证字符串输入
func TestParseStructuredInput_String(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	result, ok := r.parseStructuredInput("自由文本")
	assert.True(t, ok)
	assert.Equal(t, "自由文本", result.Answers["__free_text__"])
}

// TestParseStructuredInput_EmptyString 验证空字符串输入
func TestParseStructuredInput_EmptyString(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	result, ok := r.parseStructuredInput("")
	assert.True(t, ok)
	assert.Empty(t, result.Answers)
}

// TestParseStructuredInput_InvalidType 验证无效类型返回 false
func TestParseStructuredInput_InvalidType(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	result, ok := r.parseStructuredInput(42)
	assert.False(t, ok)
	assert.Nil(t, result)
}

// ──────────────────────────── parseToolArgsJSON ────────────────────────────

// TestParseToolArgsJSON_正常JSON 验证正常 JSON 解析
func TestParseToolArgsJSON_正常JSON(t *testing.T) {
	args := parseToolArgsJSON(`{"key": "value", "num": 1}`)
	assert.Equal(t, "value", args["key"])
}

// TestParseToolArgsJSON_空字符串 验证空字符串
func TestParseToolArgsJSON_空字符串(t *testing.T) {
	args := parseToolArgsJSON("")
	assert.Empty(t, args)
}

// TestParseToolArgsJSON_无效JSON 验证无效 JSON
func TestParseToolArgsJSON_无效JSON(t *testing.T) {
	args := parseToolArgsJSON("not json")
	assert.Empty(t, args)
}

// ──────────────────────────── 编译时接口验证 ────────────────────────────

// TestStructuredAskUserRail_AgentRail接口 验证满足 AgentRail 接口
func TestStructuredAskUserRail_AgentRail接口(t *testing.T) {
	var r agentinterfaces.AgentRail = NewStructuredAskUserRail("cn")
	assert.NotNil(t, r)
}

// TestStructuredAskUserRail_Priority 验证继承的优先级
func TestStructuredAskUserRail_Priority(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	assert.Equal(t, 90, r.Priority())
}

// ──────────────────────────── StructuredAskUserPayload ────────────────────────────

// TestStructuredAskUserPayload_JSON序列化 验证 JSON 序列化
func TestStructuredAskUserPayload_JSON序列化(t *testing.T) {
	payload := &StructuredAskUserPayload{
		Answers: map[string]string{"问题1": "选项A"},
	}
	// 验证字段存在
	assert.Equal(t, "选项A", payload.Answers["问题1"])
}
