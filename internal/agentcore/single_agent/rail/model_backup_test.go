package rail

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── Mock 实现 ────────────────────────────

// fakeSwitcherAgent 同时实现 BaseAgent + modelSwitcher 的测试桩。
// 对齐 Python 测试中 hasattr(ctx.agent, "set_llm") 命中的场景。
type fakeSwitcherAgent struct {
	// setLLMCalls 记录 SetLLM 收到的模型
	setLLMCalls []*llm.Model
	// cbMgr 回调管理器（BaseAgent 接口要求）
	cbMgr *agentinterfaces.AgentCallbackManager
}

func newFakeSwitcherAgent() *fakeSwitcherAgent {
	return &fakeSwitcherAgent{cbMgr: agentinterfaces.NewAgentCallbackManager("mb-test-agent")}
}

func (f *fakeSwitcherAgent) SetLLM(model *llm.Model) {
	f.setLLMCalls = append(f.setLLMCalls, model)
}
func (f *fakeSwitcherAgent) GetLLM() (*llm.Model, error) { return nil, nil }

func (f *fakeSwitcherAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeSwitcherAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeSwitcherAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeSwitcherAgent) Card() *agentschema.AgentCard {
	return agentschema.NewAgentCard(agentschema.WithAgentID("mb-test-agent"))
}
func (f *fakeSwitcherAgent) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeSwitcherAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeSwitcherAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return f.cbMgr }
func (f *fakeSwitcherAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return saprompt.NewSystemPromptBuilder()
}
func (f *fakeSwitcherAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeSwitcherAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeSwitcherAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// 编译时验证 fakeSwitcherAgent 同时满足 BaseAgent 与 modelSwitcher
var _ agentinterfaces.BaseAgent = (*fakeSwitcherAgent)(nil)
var _ modelSwitcher = (*fakeSwitcherAgent)(nil)

// fakeNoSwitcherAgent 仅实现 BaseAgent（不含 SetLLM/GetLLM），
// 对齐 Python 测试中 hasattr(ctx.agent, "set_llm") 未命中的场景。
type fakeNoSwitcherAgent struct {
	// cbMgr 回调管理器（BaseAgent 接口要求）
	cbMgr *agentinterfaces.AgentCallbackManager
}

func newFakeNoSwitcherAgent() *fakeNoSwitcherAgent {
	return &fakeNoSwitcherAgent{cbMgr: agentinterfaces.NewAgentCallbackManager("mb-noswitch-agent")}
}

func (f *fakeNoSwitcherAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeNoSwitcherAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeNoSwitcherAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeNoSwitcherAgent) Card() *agentschema.AgentCard {
	return agentschema.NewAgentCard(agentschema.WithAgentID("mb-noswitch-agent"))
}
func (f *fakeNoSwitcherAgent) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeNoSwitcherAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeNoSwitcherAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return f.cbMgr }
func (f *fakeNoSwitcherAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return saprompt.NewSystemPromptBuilder()
}
func (f *fakeNoSwitcherAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeNoSwitcherAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeNoSwitcherAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

var _ agentinterfaces.BaseAgent = (*fakeNoSwitcherAgent)(nil)

// newTestModel 构造轻量 Model 指针（仅用于指针断言，不发起真实调用）
func newTestModel(id string) *llm.Model {
	return &llm.Model{
		ModelConfig:  &llmschema.ModelRequestConfig{ModelName: id},
		ClientConfig: &llmschema.ModelClientConfig{ClientID: id},
	}
}

// ──────────────────────────── 单元测试 ────────────────────────────

func TestNewModelBackupRail_构造(t *testing.T) {
	t.Parallel()
	m1, m2 := newTestModel("m1"), newTestModel("m2")
	r := NewModelBackupRail([]*llm.Model{m1, m2})
	assert.Equal(t, []*llm.Model{m1, m2}, r.backupModels)
	assert.Equal(t, 0, r.index)
	assert.Equal(t, 50, r.Priority(), "默认优先级应为 50，对齐 Python AgentRail.priority")
}

func TestNewModelBackupRail_空列表(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail(nil)
	assert.Empty(t, r.backupModels)
}

func TestModelBackupRail_GetCallbacks映射(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail([]*llm.Model{newTestModel("m1")})
	callbacks := r.GetCallbacks()
	assert.Len(t, callbacks, 1, "仅声明 OnModelException 一个钩子")
	fn, ok := callbacks[agentinterfaces.CallbackOnModelException]
	require.True(t, ok, "应包含 CallbackOnModelException 映射")
	require.NotNil(t, fn)

	// wrap 函数类型断言后应可正常调用（不 panic）
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	require.NoError(t, fn(context.Background(), cbc))
	// 触发了一次切换
	require.Len(t, agent.setLLMCalls, 1)
}

func TestModelBackupRail_OnModelException_切换并请求重试(t *testing.T) {
	t.Parallel()
	m1, m2 := newTestModel("m1"), newTestModel("m2")
	r := NewModelBackupRail([]*llm.Model{m1, m2})
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	require.NoError(t, r.OnModelException(context.Background(), cbc))
	// 切到第一个备用模型
	require.Len(t, agent.setLLMCalls, 1)
	assert.Equal(t, m1, agent.setLLMCalls[0])
	assert.Equal(t, 1, r.index)
	// 请求了重试（consume-and-clear 语义：消费一次后清空）
	require.NotNil(t, cbc.ConsumeRetryRequest())
	assert.Nil(t, cbc.ConsumeRetryRequest(), "retry 请求应一次性消费")
}

func TestModelBackupRail_OnModelException_连续异常依次切换(t *testing.T) {
	t.Parallel()
	m1, m2 := newTestModel("m1"), newTestModel("m2")
	r := NewModelBackupRail([]*llm.Model{m1, m2})
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	// 第一次异常 → m1
	require.NoError(t, r.OnModelException(context.Background(), cbc))
	_ = cbc.ConsumeRetryRequest() // 清掉（模拟 executor 循环行为）
	// 第二次异常 → m2
	require.NoError(t, r.OnModelException(context.Background(), cbc))
	require.Len(t, agent.setLLMCalls, 2)
	assert.Equal(t, m2, agent.setLLMCalls[1])
	assert.Equal(t, 2, r.index)
	_ = cbc.ConsumeRetryRequest()
	// 第三次异常 → 备用耗尽，不再切换
	require.NoError(t, r.OnModelException(context.Background(), cbc))
	assert.Len(t, agent.setLLMCalls, 2, "备用耗尽后不应再切换")
	assert.Nil(t, cbc.ConsumeRetryRequest(), "备用耗尽后不应请求重试")
}

func TestModelBackupRail_OnModelException_agent不支持切换(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail([]*llm.Model{newTestModel("m1")})
	agent := newFakeNoSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	require.NoError(t, r.OnModelException(context.Background(), cbc))
	assert.Equal(t, 0, r.index, "不切换时 index 不应推进")
	assert.Nil(t, cbc.ConsumeRetryRequest(), "不切换时不应请求重试")
}

func TestModelBackupRail_OnModelException_空备用列表(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail(nil)
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	require.NoError(t, r.OnModelException(context.Background(), cbc))
	assert.Empty(t, agent.setLLMCalls)
	assert.Nil(t, cbc.ConsumeRetryRequest())
}
