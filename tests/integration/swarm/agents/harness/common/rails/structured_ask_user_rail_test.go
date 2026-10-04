//go:build integration

package rails

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	rails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StructuredAskUserRailSuite 测试 StructuredAskUserRail 在真实 ResourceMgr 上下文中的集成行为。
//
// 覆盖：
//   - AgentRail 接口满足
//   - Priority 继承值
//   - StructuredAskUserPayload JSON 序列化
//   - Init 工具注册到 AbilityManager + ResourceMgr
//   - Uninit 从 AbilityManager + ResourceMgr 注销
//   - Init → ExtractQuestions → Uninit 完整生命周期
//   - interrupt.AskUserRail 父类行为集成
//   - 语言回退逻辑（空语言→SystemPromptBuilder→默认 cn）
//   - resolveStructuredInterrupt 通过 ResolveInterruptFn 集成调用
//
// 对齐 Python: jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
type StructuredAskUserRailSuite struct {
	isuite.BaseIntegrationSuite
	// rail 每个测试方法共享的 StructuredAskUserRail 实例
	rail *rails.StructuredAskUserRail
	// agent 每个测试方法共享的 mock BaseAgent
	agent *fakeIntegrationBaseAgent
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestStructuredAskUserRailSuite 运行 StructuredAskUserRail 集成测试
func TestStructuredAskUserRailSuite(t *testing.T) {
	suite.Run(t, new(StructuredAskUserRailSuite))
}

// SetupSuite 初始化基础测试环境
func (s *StructuredAskUserRailSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
}

// TearDownSuite 清理基础测试环境
func (s *StructuredAskUserRailSuite) TearDownSuite() {
	s.BaseIntegrationSuite.TearDownSuite()
}

// SetupTest 每个测试方法前初始化 rail 和 agent
func (s *StructuredAskUserRailSuite) SetupTest() {
	s.rail = rails.NewStructuredAskUserRail("cn")
	s.agent = &fakeIntegrationBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "itest-structured-agent"}},
		am:   newFakeAbilityManager(),
	}
}

// TearDownTest 每个测试方法后清理注册的工具
func (s *StructuredAskUserRailSuite) TearDownTest() {
	if s.rail != nil && s.agent != nil {
		_ = s.rail.Uninit(s.agent)
	}
}

// ──────────────────────────── AgentRail 接口满足 ────────────────────────────

// TestStructuredAskUserRail_满足AgentRail接口 编译时验证 StructuredAskUserRail 满足 AgentRail 接口
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_满足AgentRail接口() {
	var _ agentinterfaces.AgentRail = (*rails.StructuredAskUserRail)(nil)
	s.True(true, "编译通过即满足接口")
}

// ──────────────────────────── Priority ────────────────────────────

// TestStructuredAskUserRail_Priority 验证继承 BaseInterruptRail 的优先级 90
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_Priority() {
	s.Equal(90, s.rail.Priority(), "StructuredAskUserRail 应继承 BaseInterruptRail 的优先级 90")
}

// ──────────────────────────── StructuredAskUserPayload 序列化 ────────────────────────────

// TestStructuredAskUserPayload_JSON序列化 验证 StructuredAskUserPayload 的 JSON 序列化输出
func (s *StructuredAskUserRailSuite) TestStructuredAskUserPayload_JSON序列化() {
	payload := &rails.StructuredAskUserPayload{
		Answers: map[string]string{
			"选择方案": "方案A",
			"部署环境": "生产环境",
		},
	}

	data, err := json.Marshal(payload)
	s.Require().NoError(err, "JSON 序列化不应返回错误")

	// 反序列化验证
	var decoded rails.StructuredAskUserPayload
	s.Require().NoError(json.Unmarshal(data, &decoded), "JSON 反序列化不应返回错误")
	s.Equal("方案A", decoded.Answers["选择方案"], "序列化/反序列化后值应一致")
	s.Equal("生产环境", decoded.Answers["部署环境"], "序列化/反序列化后值应一致")
}

// TestStructuredAskUserPayload_JSON字段名 验证 JSON 字段名为 answers
func (s *StructuredAskUserRailSuite) TestStructuredAskUserPayload_JSON字段名() {
	payload := &rails.StructuredAskUserPayload{
		Answers: map[string]string{"Q1": "A1"},
	}

	data, err := json.Marshal(payload)
	s.Require().NoError(err, "JSON 序列化不应返回错误")
	s.Contains(string(data), `"answers"`, "JSON 输出应包含 answers 字段名")
}

// TestStructuredAskUserPayload_空Answers 验证空 Answers 的序列化
func (s *StructuredAskUserRailSuite) TestStructuredAskUserPayload_空Answers() {
	payload := &rails.StructuredAskUserPayload{Answers: map[string]string{}}

	data, err := json.Marshal(payload)
	s.Require().NoError(err, "JSON 序列化不应返回错误")

	var decoded rails.StructuredAskUserPayload
	s.Require().NoError(json.Unmarshal(data, &decoded), "JSON 反序列化不应返回错误")
	s.Empty(decoded.Answers, "空 Answers 序列化后应仍为空")
}

// ──────────────────────────── Init 工具注册 ────────────────────────────

// TestStructuredAskUserRail_Init_工具注册 验证 Init 将 StructuredAskUserTool 注册到 AbilityManager
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_Init_工具注册() {
	err := s.rail.Init(s.Ctx, s.agent)
	s.Require().NoError(err, "Init 不应返回错误")
	s.Len(s.rail.GetStructuredTools(), 1, "Init 后应注册 1 个工具")
	s.Equal("ask_user", s.rail.GetStructuredTools()[0].Card().Name, "工具名应为 ask_user")

	// 验证 AbilityManager 收到注册
	s.True(s.agent.am.Has("ask_user"), "AbilityManager 应包含 ask_user 工具")
}

// TestStructuredAskUserRail_Init_空语言从Builder获取 验证空语言时从 SystemPromptBuilder 获取语言
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_Init_空语言从Builder获取() {
	r := rails.NewStructuredAskUserRail("")
	agent := &fakeIntegrationBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "itest-en-agent"}},
		sb:   &fakeIntegrationSPB{language: "en"},
		am:   newFakeAbilityManager(),
	}

	err := r.Init(s.Ctx, agent)
	s.Require().NoError(err, "Init 不应返回错误")
	s.Contains(r.GetStructuredTools()[0].Card().Description, "Structured questions",
		"英文语言下工具描述应包含 Structured questions")
}

// TestStructuredAskUserRail_Init_空语言无Builder默认中文 验证空语言且无 SystemPromptBuilder 时默认中文
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_Init_空语言无Builder默认中文() {
	r := rails.NewStructuredAskUserRail("")
	agent := &fakeIntegrationBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "itest-default-agent"}},
		am:   newFakeAbilityManager(),
	}

	err := r.Init(s.Ctx, agent)
	s.Require().NoError(err, "Init 不应返回错误")
	s.Contains(r.GetStructuredTools()[0].Card().Description, "结构化选项",
		"默认中文下工具描述应包含 结构化选项")
}

// TestStructuredAskUserRail_Init_工具ID包含AgentID 验证工具 ID 包含 agent ID
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_Init_工具ID包含AgentID() {
	err := s.rail.Init(s.Ctx, s.agent)
	s.Require().NoError(err, "Init 不应返回错误")

	toolID := s.rail.GetStructuredTools()[0].Card().ID
	s.Contains(toolID, "itest-structured-agent", "工具 ID 应包含 agent ID")
}

// TestStructuredAskUserRail_Init_工具Schema包含Questions 验证注册的工具 schema 包含 questions 参数
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_Init_工具Schema包含Questions() {
	err := s.rail.Init(s.Ctx, s.agent)
	s.Require().NoError(err, "Init 不应返回错误")

	card := s.rail.GetStructuredTools()[0].Card()
	hasQuestions := false
	for _, p := range card.InputParams {
		if p.Name == "questions" {
			hasQuestions = true
			break
		}
	}
	s.True(hasQuestions, "工具 InputParams 应包含 questions 参数")
}

// ──────────────────────────── Uninit 注销 ────────────────────────────

// TestStructuredAskUserRail_Uninit_注销工具 验证 Uninit 从 AbilityManager 注销工具
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_Uninit_注销工具() {
	s.Require().NoError(s.rail.Init(s.Ctx, s.agent))
	s.True(s.agent.am.Has("ask_user"), "Init 后 AbilityManager 应包含 ask_user")

	err := s.rail.Uninit(s.agent)
	s.Require().NoError(err, "Uninit 不应返回错误")
	s.Nil(s.rail.GetStructuredTools(), "Uninit 后 structuredTools 应为 nil")
	s.False(s.agent.am.Has("ask_user"), "Uninit 后 AbilityManager 不应包含 ask_user")
}

// TestStructuredAskUserRail_Uninit_无工具 验证无工具时 Uninit 不报错
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_Uninit_无工具() {
	r := rails.NewStructuredAskUserRail("cn")
	agent := &fakeIntegrationBaseAgent{am: newFakeAbilityManager()}

	err := r.Uninit(agent)
	s.Require().NoError(err, "无工具时 Uninit 不应返回错误")
}

// ──────────────────────────── 完整生命周期 ────────────────────────────

// TestStructuredAskUserRail_完整生命周期 验证 Init → ExtractQuestions → Uninit 完整流程
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_完整生命周期() {
	// 1. Init
	err := s.rail.Init(s.Ctx, s.agent)
	s.Require().NoError(err, "Init 不应返回错误")
	s.Len(s.rail.GetStructuredTools(), 1, "Init 后应有 1 个工具")

	// 2. ExtractQuestions
	toolCall := &llmschema.ToolCall{
		ID:        "tc_lifecycle",
		Name:      "ask_user",
		Arguments: `{"query": "请选择", "questions": [{"question": "部署方式", "header": "方式", "options": [{"label": "容器"}, {"label": "虚拟机"}]}]}`,
	}
	questions := s.rail.ExtractQuestions(toolCall)
	s.Len(questions, 1, "应提取 1 个问题")
	s.Equal("部署方式", questions[0]["question"], "问题文本应正确")
	s.Equal("方式", questions[0]["header"], "header 应正确")

	// 3. Uninit
	err = s.rail.Uninit(s.agent)
	s.Require().NoError(err, "Uninit 不应返回错误")
	s.Nil(s.rail.GetStructuredTools(), "Uninit 后 structuredTools 应为 nil")
}

// TestStructuredAskUserRail_多次InitUninit 验证多次 Init/Uninit 不泄漏工具
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_多次InitUninit() {
	for i := 0; i < 3; i++ {
		err := s.rail.Init(s.Ctx, s.agent)
		s.Require().NoError(err, "第 %d 次 Init 不应返回错误", i+1)
		s.Len(s.rail.GetStructuredTools(), 1, "第 %d 次 Init 后应有 1 个工具", i+1)

		err = s.rail.Uninit(s.agent)
		s.Require().NoError(err, "第 %d 次 Uninit 不应返回错误", i+1)
		s.Nil(s.rail.GetStructuredTools(), "第 %d 次 Uninit 后 structuredTools 应为 nil", i+1)
	}
}

// ──────────────────────────── ExtractQuestions 集成 ────────────────────────────

// TestStructuredAskUserRail_ExtractQuestions_多问题 验证提取多个 questions
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_ExtractQuestions_多问题() {
	toolCall := &llmschema.ToolCall{
		ID:        "tc_multi",
		Name:      "ask_user",
		Arguments: `{"query": "请选择", "questions": [{"question": "Q1", "header": "H1", "options": [{"label": "A"}, {"label": "B"}]}, {"question": "Q2", "header": "H2", "multi_select": true}]}`,
	}

	questions := s.rail.ExtractQuestions(toolCall)
	s.Len(questions, 2, "应提取 2 个问题")
	s.Equal("Q1", questions[0]["question"])
	s.Equal("Q2", questions[1]["question"])
	s.Equal(true, questions[1]["multi_select"], "multi_select 字段应保留")
}

// TestStructuredAskUserRail_ExtractQuestions_无questions返回nil 验证无 questions 参数返回 nil
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_ExtractQuestions_无questions返回nil() {
	toolCall := &llmschema.ToolCall{
		ID:        "tc_no_q",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	questions := s.rail.ExtractQuestions(toolCall)
	s.Nil(questions, "无 questions 参数应返回 nil")
}

// ──────────────────────────── interrupt.AskUserRail 父类行为集成 ────────────────────────────

// TestStructuredAskUserRail_父类ResolveInterruptFn覆盖 验证 NewStructuredAskUserRail 覆盖父类 ResolveInterruptFn
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_父类ResolveInterruptFn覆盖() {
	// 当前 ResolveInterruptFn 应不为 nil（被覆盖为 resolveStructuredInterrupt）
	s.NotNil(s.rail.AskUserRail.BaseInterruptRail.ResolveInterruptFn,
		"ResolveInterruptFn 应被覆盖")
}

// TestStructuredAskUserRail_父类工具名列表 验证继承父类的工具拦截名 ask_user
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_父类工具名列表() {
	tools := s.rail.AskUserRail.BaseInterruptRail.GetTools()
	s.Contains(tools, "ask_user", "应继承父类拦截 ask_user 工具")
}

// TestStructuredAskUserRail_父类Interrupt行为 验证父类 Interrupt 方法可正常创建 InterruptResult
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_父类Interrupt行为() {
	toolCall := &llmschema.ToolCall{
		ID:        "tc_interrupt",
		Name:      "ask_user",
		Arguments: `{"query": "请确认"}`,
	}
	request := s.rail.AskUserRail.BuildAskRequest(toolCall)
	s.Require().NotNil(request, "BuildAskRequest 不应返回 nil")

	interruptResult := s.rail.AskUserRail.Interrupt(request)
	s.NotNil(interruptResult, "Interrupt 不应返回 nil")
	s.NotNil(interruptResult.Request, "InterruptResult.Request 不应为 nil")
}

// TestStructuredAskUserRail_父类Reject行为 验证父类 Reject 方法可正常创建 RejectResult
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_父类Reject行为() {
	rejectResult := s.rail.AskUserRail.BaseInterruptRail.Reject("测试拒绝")
	s.NotNil(rejectResult, "Reject 不应返回 nil")
	s.Equal("测试拒绝", rejectResult.ToolResult, "RejectResult.ToolResult 应正确")
}

// TestStructuredAskUserRail_父类BuildAskRequest 验证父类 BuildAskRequest 包含 questions 字段
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_父类BuildAskRequest() {
	toolCall := &llmschema.ToolCall{
		ID:        "tc_build",
		Name:      "ask_user",
		Arguments: `{"query": "请选择", "questions": [{"question": "Q1", "header": "H1"}]}`,
	}
	request := s.rail.AskUserRail.BuildAskRequest(toolCall)
	s.Require().NotNil(request, "BuildAskRequest 不应返回 nil")

	// AskUserRequest 应包含 questions
	s.Len(request.Questions, 1, "AskUserRequest 应包含 1 个 question")
	s.Equal("Q1", request.Questions[0]["question"], "question 文本应正确")
}

// ──────────────────────────── resolveStructuredInterrupt 集成 ────────────────────────────

// TestStructuredAskUserRail_结构化路径Reject 验证结构化输入走 Reject 路径
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_结构化路径Reject() {
	toolCall := &llmschema.ToolCall{
		ID:        "tc_reject",
		Name:      "ask_user",
		Arguments: `{"query": "请选择", "questions": [{"question": "部署方式", "header": "方式"}]}`,
	}
	userInput := &rails.StructuredAskUserPayload{
		Answers: map[string]string{"部署方式": "容器部署"},
	}

	decision := s.rail.AskUserRail.BaseInterruptRail.ResolveInterruptFn(
		s.Ctx, nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	s.True(ok, "结构化路径应返回 RejectResult")
	s.Equal("部署方式: 容器部署", rejectResult.ToolResult, "RejectResult 内容应正确格式化")
}

// TestStructuredAskUserRail_非结构化路径回退父类 验证无 questions 时回退到父类 resolve
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_非结构化路径回退父类() {
	toolCall := &llmschema.ToolCall{
		ID:        "tc_fallback",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	// AskUserPayload 输入 → 非结构化路径 → 回退父类
	userInput := &interrupt.AskUserPayload{
		Answers: map[string]string{"q1": "回答1"},
	}

	decision := s.rail.AskUserRail.BaseInterruptRail.ResolveInterruptFn(
		s.Ctx, nil, toolCall, userInput, nil,
	)

	// parentResolve 处理 AskUserPayload → 应返回 RejectResult
	rejectResult, ok := decision.(*interrupt.RejectResult)
	s.True(ok, "非结构化路径回退父类应返回 RejectResult")
	s.Contains(rejectResult.ToolResult, "回答1", "父类应正确格式化 AskUserPayload")
}

// TestStructuredAskUserRail_无用户输入中断 验证无用户输入返回 InterruptResult
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_无用户输入中断() {
	toolCall := &llmschema.ToolCall{
		ID:        "tc_no_input",
		Name:      "ask_user",
		Arguments: `{"query": "请确认", "questions": [{"question": "Q1"}]}`,
	}

	decision := s.rail.AskUserRail.BaseInterruptRail.ResolveInterruptFn(
		s.Ctx, nil, toolCall, nil, nil,
	)

	interruptResult, ok := decision.(*interrupt.InterruptResult)
	s.True(ok, "无用户输入应返回 InterruptResult")
	s.NotNil(interruptResult.Request, "InterruptResult.Request 不应为 nil")
}

// TestStructuredAskUserRail_字符串输入Reject 验证字符串输入直接 Reject
func (s *StructuredAskUserRailSuite) TestStructuredAskUserRail_字符串输入Reject() {
	toolCall := &llmschema.ToolCall{
		ID:        "tc_string",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	decision := s.rail.AskUserRail.BaseInterruptRail.ResolveInterruptFn(
		s.Ctx, nil, toolCall, "用户回答", nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	s.True(ok, "字符串输入应返回 RejectResult")
	s.Equal("用户回答", rejectResult.ToolResult, "字符串应直接作为 ToolResult")
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeIntegrationBaseAgent 用于集成测试的 mock BaseAgent
type fakeIntegrationBaseAgent struct {
	card *agentschema.AgentCard
	am   *fakeAbilityManager
	sb   saprompt.SystemPromptBuilderInterface
}

func (f *fakeIntegrationBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeIntegrationBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeIntegrationBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeIntegrationBaseAgent) Card() *agentschema.AgentCard        { return f.card }
func (f *fakeIntegrationBaseAgent) Config() agentinterfaces.AgentConfig { return nil }
func (f *fakeIntegrationBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.am
}
func (f *fakeIntegrationBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return nil
}
func (f *fakeIntegrationBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.sb
}
func (f *fakeIntegrationBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ callback.PerAgentCallbackFunc, _ ...callback.CallbackOption) error {
	return nil
}
func (f *fakeIntegrationBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (f *fakeIntegrationBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// fakeIntegrationSPB 用于集成测试的 mock SystemPromptBuilder
type fakeIntegrationSPB struct {
	language string
}

func (f *fakeIntegrationSPB) AddSection(_ saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	return nil
}
func (f *fakeIntegrationSPB) RemoveSection(_ string) *saprompt.SystemPromptBuilder { return nil }
func (f *fakeIntegrationSPB) Language() string                                     { return f.language }
func (f *fakeIntegrationSPB) SetLanguage(lang string)                              { f.language = lang }
func (f *fakeIntegrationSPB) GetSection(_ string) *saprompt.PromptSection          { return nil }
func (f *fakeIntegrationSPB) HasSection(_ string) bool                             { return false }

// fakeAbilityManager 用于集成测试的 mock AbilityManager
// 实现 agentinterfaces.AbilityManagerInterface
type fakeAbilityManager struct {
	abilities map[string]schema.Ability
}

func newFakeAbilityManager() *fakeAbilityManager {
	return &fakeAbilityManager{abilities: make(map[string]schema.Ability)}
}

func (f *fakeAbilityManager) Add(ability schema.Ability) agentschema.AddAbilityResult {
	if ability != nil {
		f.abilities[ability.AbilityName()] = ability
	}
	return agentschema.AddAbilityResult{}
}

func (f *fakeAbilityManager) AddMany(abilities []schema.Ability) []agentschema.AddAbilityResult {
	results := make([]agentschema.AddAbilityResult, len(abilities))
	for i, a := range abilities {
		results[i] = f.Add(a)
	}
	return results
}

func (f *fakeAbilityManager) Remove(name string) schema.Ability {
	a := f.abilities[name]
	delete(f.abilities, name)
	return a
}

func (f *fakeAbilityManager) RemoveMany(names []string) []schema.Ability {
	removed := make([]schema.Ability, len(names))
	for i, n := range names {
		removed[i] = f.Remove(n)
	}
	return removed
}

func (f *fakeAbilityManager) Get(name string) schema.Ability {
	return f.abilities[name]
}

func (f *fakeAbilityManager) List() []schema.Ability {
	result := make([]schema.Ability, 0, len(f.abilities))
	for _, a := range f.abilities {
		result = append(result, a)
	}
	return result
}

func (f *fakeAbilityManager) ListToolInfo(_ context.Context, _ []string, _ ...string) ([]schema.ToolInfoInterface, error) {
	return nil, nil
}

func (f *fakeAbilityManager) Execute(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ []*llmschema.ToolCall, _ sessioninterfaces.SessionFacade, _ string) []agentschema.ExecuteResult {
	return nil
}

func (f *fakeAbilityManager) SetContextEngine(_ ceinterface.ContextEngine) {}

func (f *fakeAbilityManager) ReorderTools(_ []string) {}

// Has 检查指定名称的能力是否存在（测试辅助方法）
func (f *fakeAbilityManager) Has(name string) bool {
	_, ok := f.abilities[name]
	return ok
}
