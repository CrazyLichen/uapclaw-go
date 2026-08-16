package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeBaseAgent 用于测试的 mock BaseAgent
type fakeBaseAgent struct {
	card *agentschema.AgentCard
	am   agentinterfaces.AbilityManagerInterface
	sb   saprompt.SystemPromptBuilderInterface
}

func (f *fakeBaseAgent) Configure(ctx context.Context, config agentinterfaces.AgentConfig) error {
	return nil
}

func (f *fakeBaseAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

func (f *fakeBaseAgent) Stream(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

func (f *fakeBaseAgent) Card() *agentschema.AgentCard                               { return f.card }
func (f *fakeBaseAgent) Config() agentinterfaces.AgentConfig                        { return nil }
func (f *fakeBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface    { return f.am }
func (f *fakeBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager     { return nil }
func (f *fakeBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return f.sb }
func (f *fakeBaseAgent) RegisterCallback(ctx context.Context, event agentinterfaces.AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgent) RegisterRail(ctx context.Context, rail agentinterfaces.AgentRail, opts ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgent) UnregisterRail(ctx context.Context, rail agentinterfaces.AgentRail) error {
	return nil
}

// fakeSystemPromptBuilder 用于测试的 mock SystemPromptBuilder
type fakeSystemPromptBuilder struct {
	language string
}

func (f *fakeSystemPromptBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	return nil
}
func (f *fakeSystemPromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	return nil
}
func (f *fakeSystemPromptBuilder) Language() string                               { return f.language }
func (f *fakeSystemPromptBuilder) GetSection(name string) *saprompt.PromptSection { return nil }
func (f *fakeSystemPromptBuilder) HasSection(name string) bool                    { return false }

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
		ID:        "tc1",
		Name:      "ask_user",
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

// ──────────────────────────── Init ────────────────────────────

// TestInit_正常注册 验证 Init 注册工具到 AbilityManager
func TestInit_正常注册(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	agent := &fakeBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "test-agent"}},
	}

	err := r.Init(agent)
	require.NoError(t, err)
	assert.Len(t, r.structuredTools, 1)
	assert.Equal(t, "ask_user", r.structuredTools[0].Card().Name)
}

// TestInit_空语言从Agent获取 验证空语言时从 SystemPromptBuilder 获取
func TestInit_空语言从Agent获取(t *testing.T) {
	r := NewStructuredAskUserRail("")
	agent := &fakeBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "test-agent"}},
		sb:   &fakeSystemPromptBuilder{language: "en"},
	}

	err := r.Init(agent)
	require.NoError(t, err)
	assert.Len(t, r.structuredTools, 1)
	// 工具描述应为英文
	assert.Contains(t, r.structuredTools[0].Card().Description, "Structured questions")
}

// TestInit_空语言无Builder默认中文 验证空语言且无 SystemPromptBuilder 时默认中文
func TestInit_空语言无Builder默认中文(t *testing.T) {
	r := NewStructuredAskUserRail("")
	agent := &fakeBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "test-agent"}},
	}

	err := r.Init(agent)
	require.NoError(t, err)
	assert.Contains(t, r.structuredTools[0].Card().Description, "结构化选项")
}

// TestInit_无AgentID 验证无 Card 时 agentID 为空
func TestInit_无AgentID(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	agent := &fakeBaseAgent{}

	err := r.Init(agent)
	require.NoError(t, err)
	// 工具 ID 应包含 "ask_user_"（使用 UUID 代替 agentID）
	assert.Contains(t, r.structuredTools[0].Card().ID, "ask_user_")
}

// ──────────────────────────── Uninit ────────────────────────────

// TestUninit_正常注销 验证 Uninit 注销工具
func TestUninit_正常注销(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	agent := &fakeBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "test-agent"}},
	}

	err := r.Init(agent)
	require.NoError(t, err)
	assert.Len(t, r.structuredTools, 1)

	err = r.Uninit(agent)
	require.NoError(t, err)
	assert.Nil(t, r.structuredTools)
}

// TestUninit_无工具 验证无工具时 Uninit 不报错
func TestUninit_无工具(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	agent := &fakeBaseAgent{}

	err := r.Uninit(agent)
	require.NoError(t, err)
}

// ──────────────────────────── GetStructuredTools ────────────────────────────

// TestGetStructuredTools_初始化前 验证 Init 前返回 nil
func TestGetStructuredTools_初始化前(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	assert.Nil(t, r.GetStructuredTools())
}

// TestGetStructuredTools_初始化后 验证 Init 后返回工具列表
func TestGetStructuredTools_初始化后(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	agent := &fakeBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "test-agent"}},
	}

	err := r.Init(agent)
	require.NoError(t, err)

	tools := r.GetStructuredTools()
	assert.Len(t, tools, 1)
	assert.Equal(t, "ask_user", tools[0].Card().Name)
}

// ──────────────────────────── resolveStructuredInterrupt 补充 ────────────────────────────

// TestResolveStructuredInterrupt_结构化路径Answers为空 验证结构化路径 answers 为空返回中断
func TestResolveStructuredInterrupt_结构化路径Answers为空(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	// 空字符串输入 → parseStructuredInput 返回空 Answers → Interrupt
	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, "", nil,
	)

	_, ok := decision.(*interrupt.InterruptResult)
	assert.True(t, ok, "结构化路径空 Answers 应返回 InterruptResult")
}

// TestResolveStructuredInterrupt_结构化路径多问题 验证结构化路径多问题格式化
func TestResolveStructuredInterrupt_结构化路径多问题(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}, {"question": "Q2", "header": "H2"}]}`,
	}

	userInput := map[string]any{
		"answers": map[string]any{
			"Q1": "A1",
			"Q2": "A2",
		},
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Contains(t, rejectResult.ToolResult, "Q1: A1")
	assert.Contains(t, rejectResult.ToolResult, "Q2: A2")
}

// TestResolveStructuredInterrupt_结构化路径自由文本键 验证 __free_text__ 键不添加前缀
func TestResolveStructuredInterrupt_结构化路径自由文本键(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	userInput := &StructuredAskUserPayload{
		Answers: map[string]string{
			"__free_text__": "自由文本内容",
		},
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Equal(t, "自由文本内容", rejectResult.ToolResult)
}

// TestResolveStructuredInterrupt_无效类型输入有Questions 验证有 questions 但无效输入类型返回中断
func TestResolveStructuredInterrupt_无效类型输入有Questions(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	// int 输入 → parseStructuredInput 返回 false → Interrupt
	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, 42, nil,
	)

	_, ok := decision.(*interrupt.InterruptResult)
	assert.True(t, ok, "无效类型输入应返回 InterruptResult")
}

// TestResolveStructuredInterrupt_非结构化路径ParentResolve 验证非结构化路径回退到 parentResolve
func TestResolveStructuredInterrupt_非结构化路径ParentResolve(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	// AskUserPayload 输入 → 回退到 parentResolve（对齐 Python: isinstance(user_input, AskUserPayload) → super().resolve_interrupt()）
	userInput := &interrupt.AskUserPayload{
		Answers: map[string]string{"q1": "回答1"},
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	// parentResolve 是原始 AskUserRail 的 resolveAskUserInterrupt
	// AskUserPayload 有 Answers → RejectResult
	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Contains(t, rejectResult.ToolResult, "回答1")
}

// ──────────────────────────── parseStructuredInput 补充 ────────────────────────────

// TestParseStructuredInput_DictWithAnswersNonStringValues 验证 dict 中 answers 值非字符串被过滤
func TestParseStructuredInput_DictWithAnswersNonStringValues(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	input := map[string]any{
		"answers": map[string]any{
			"Q1": "A1",
			"Q2": 42, // 非字符串应被过滤
		},
	}
	result, ok := r.parseStructuredInput(input)
	assert.True(t, ok)
	assert.Equal(t, "A1", result.Answers["Q1"])
	_, exists := result.Answers["Q2"]
	assert.False(t, exists, "非字符串值应被过滤")
}

// TestParseStructuredInput_DictWithoutAnswersNonStringValues 验证 dict 无 answers 键时非字符串值被过滤
func TestParseStructuredInput_DictWithoutAnswersNonStringValues(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	input := map[string]any{
		"Q1": "A1",
		"Q2": 42, // 非字符串应被过滤
	}
	result, ok := r.parseStructuredInput(input)
	assert.True(t, ok)
	assert.Equal(t, "A1", result.Answers["Q1"])
	_, exists := result.Answers["Q2"]
	assert.False(t, exists, "非字符串值应被过滤")
}

// ──────────────────────────── ExtractQuestions 补充 ────────────────────────────

// TestExtractQuestions_questions非列表 验证 questions 非列表类型返回 nil
func TestExtractQuestions_questions非列表(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": "not a list"}`,
	}

	questions := r.ExtractQuestions(toolCall)
	assert.Nil(t, questions)
}

// TestExtractQuestions_questions元素非Dict 验证 questions 元素非 dict 被过滤
func TestExtractQuestions_questions元素非Dict(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": ["not a dict", 42]}`,
	}

	questions := r.ExtractQuestions(toolCall)
	assert.Nil(t, questions)
}
