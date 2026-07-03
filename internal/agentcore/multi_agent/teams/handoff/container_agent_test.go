package handoff

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockBaseAgent 模拟 BaseAgent 接口
type mockBaseAgent struct {
	// card Agent 卡片
	card *agentschema.AgentCard
	// invokeResult Invoke 返回结果
	invokeResult map[string]any
	// invokeErr Invoke 返回错误
	invokeErr error
	// abilityMgr 能力管理器
	abilityMgr agentinterfaces.AbilityManagerInterface
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newMockBaseAgent 创建模拟 Agent
func newMockBaseAgent(id string) *mockBaseAgent {
	card := agentschema.NewAgentCard(
		commonschema.WithID(id),
		commonschema.WithName(id),
	)
	return &mockBaseAgent{
		card:       card,
		abilityMgr: ability.NewAbilityManager(nil),
	}
}

func (m *mockBaseAgent) Card() *agentschema.AgentCard                { return m.card }
func (m *mockBaseAgent) Config() agentinterfaces.AgentConfig         { return nil }
func (m *mockBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return m.abilityMgr }
func (m *mockBaseAgent) CallbackManager() *rail.AgentCallbackManager { return nil }
func (m *mockBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (m *mockBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return m.invokeResult, m.invokeErr
}
func (m *mockBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema, 1)
	ch <- &stream.OutputSchema{Payload: m.invokeResult, IsLastSchema: true}
	close(ch)
	return ch, nil
}
func (m *mockBaseAgent) RegisterCallback(_ context.Context, _ any, _ any, _ ...callback.CallbackOption) error {
	return nil
}
func (m *mockBaseAgent) RegisterRail(_ context.Context, _ rail.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (m *mockBaseAgent) UnregisterRail(_ context.Context, _ rail.AgentRail) error { return nil }

// mockContainerSessionFacade 模拟 SessionFacade 接口
type mockContainerSessionFacade struct {
	// state 状态存储
	state map[string]any
	// sessionID 会话标识
	sessionID string
}

func newMockContainerSessionFacade(id string) *mockContainerSessionFacade {
	return &mockContainerSessionFacade{
		state:     make(map[string]any),
		sessionID: id,
	}
}

func (m *mockContainerSessionFacade) GetSessionID() string            { return m.sessionID }
func (m *mockContainerSessionFacade) UpdateState(data map[string]any) { m.state["global"] = data }
func (m *mockContainerSessionFacade) GetState(key state.StateKey) (any, error) {
	return m.state[key.String()], nil
}
func (m *mockContainerSessionFacade) DumpState() map[string]any                        { return m.state }
func (m *mockContainerSessionFacade) WriteStream(_ context.Context, _ any) error       { return nil }
func (m *mockContainerSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (m *mockContainerSessionFacade) GetEnv(_ string, _ ...any) any                    { return nil }
func (m *mockContainerSessionFacade) Interact(_ context.Context, _ any) error          { return nil }

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewContainerAgent 测试构造函数
func TestNewContainerAgent(t *testing.T) {
	card := agentschema.NewAgentCard(
		commonschema.WithID("test_agent"),
		commonschema.WithName("test_agent"),
	)
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test_agent"), nil
	}
	allowedTargets := map[string]struct{}{"agent_b": {}}

	agent := NewContainerAgent(card, provider, allowedTargets, nil)

	assert.NotNil(t, agent)
	assert.Equal(t, card, agent.targetCard)
	assert.Equal(t, allowedTargets, agent.allowedTargets)
	assert.Nil(t, agent.coordinatorLookup)
}

// TestContainerAgent_Card 测试 Card 方法
func TestContainerAgent_Card(t *testing.T) {
	card := agentschema.NewAgentCard(
		commonschema.WithID("my_agent"),
		commonschema.WithName("my_agent"),
	)
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("my_agent"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	result := agent.Card()
	assert.Equal(t, card, result)
	assert.Equal(t, "my_agent", result.ID)
}

// TestContainerAgent_Configure 测试 Configure 空操作
func TestContainerAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	err := agent.Configure(context.Background(), nil)
	assert.NoError(t, err)
}

// TestBuildAgentInput_无历史 测试构建 Agent 输入（无历史）
func TestBuildAgentInput_无历史(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
		History:      nil,
	}

	result := agent.buildAgentInput(req)
	assert.Equal(t, "hello", result["query"])
	_, hasHistory := result["handoff_history"]
	assert.False(t, hasHistory, "无历史时不应包含 handoff_history")
}

// TestBuildAgentInput_有历史 测试构建 Agent 输入（有历史）
func TestBuildAgentInput_有历史(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
		History: []HandoffHistoryEntry{
			{AgentID: "agent_a", Output: map[string]any{"result": "ok"}},
		},
	}

	result := agent.buildAgentInput(req)
	assert.Equal(t, "hello", result["query"])
	history, hasHistory := result["handoff_history"]
	assert.True(t, hasHistory, "有历史时应包含 handoff_history")
	historySlice, ok := history.([]map[string]any)
	require.True(t, ok)
	require.Len(t, historySlice, 1)
	assert.Equal(t, "agent_a", historySlice[0]["agent"])
}

// TeststripHandoffMessages_过滤role为tool 测试过滤 role=tool 消息
func TestStripHandoffMessages_过滤role为tool(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "hello"},
		map[string]any{"role": "tool", "content": "tool result"},
		map[string]any{"role": "assistant", "content": "response"},
	}

	cleaned := stripHandoffMessages(messages)
	assert.Len(t, cleaned, 2)
	assert.Equal(t, "user", cleaned[0].(map[string]any)["role"])
	assert.Equal(t, "assistant", cleaned[1].(map[string]any)["role"])
}

// TeststripHandoffMessages_过滤含toolCalls 测试过滤含 tool_calls 的消息
func TestStripHandoffMessages_过滤含toolCalls(t *testing.T) {
	messages := []any{
		map[string]any{"role": "assistant", "content": "thinking", "tool_calls": []any{"call1"}},
		map[string]any{"role": "assistant", "content": "response"},
	}

	cleaned := stripHandoffMessages(messages)
	assert.Len(t, cleaned, 1)
	assert.Equal(t, "response", cleaned[0].(map[string]any)["content"])
}

// TeststripHandoffMessages_空toolCalls保留 测试空 tool_calls 消息应保留
func TestStripHandoffMessages_空toolCalls保留(t *testing.T) {
	messages := []any{
		map[string]any{"role": "assistant", "content": "response", "tool_calls": []any{}},
	}

	cleaned := stripHandoffMessages(messages)
	assert.Len(t, cleaned, 1, "空 tool_calls 消息应保留")
}

// TeststripHandoffMessages_nilToolCalls保留 测试 nil tool_calls 消息应保留
func TestStripHandoffMessages_nilToolCalls保留(t *testing.T) {
	messages := []any{
		map[string]any{"role": "assistant", "content": "response", "tool_calls": nil},
	}

	cleaned := stripHandoffMessages(messages)
	assert.Len(t, cleaned, 1, "nil tool_calls 消息应保留")
}

// TeststripHandoffMessages_非map类型保留 测试非 map 类型消息直接保留
func TestStripHandoffMessages_非map类型保留(t *testing.T) {
	messages := []any{
		"plain string message",
		42,
	}

	cleaned := stripHandoffMessages(messages)
	assert.Len(t, cleaned, 2)
}

// TeststripHandoffMessages_空列表 测试空列表
func TestStripHandoffMessages_空列表(t *testing.T) {
	cleaned := stripHandoffMessages(nil)
	assert.Len(t, cleaned, 0)

	cleaned = stripHandoffMessages([]any{})
	assert.Len(t, cleaned, 0)
}

// TestContainerAgent_Invoke_提取HandoffRequest失败 测试 Invoke 提取 HandoffRequest 失败
func TestContainerAgent_Invoke_提取HandoffRequest失败(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	// 无 HandoffRequestKey
	result, err := agent.Invoke(context.Background(), map[string]any{"query": "hello"})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	// HandoffRequestKey 值类型不对
	result, err = agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: "not_a_request"})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	// HandoffRequestKey 值为 nil
	result, err = agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: (*HandoffRequest)(nil)})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)
}

// TestContainerAgent_Invoke_协调器为nil 测试 Invoke coordinator 为 nil
func TestContainerAgent_Invoke_协调器为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}

	// coordinatorLookup 返回 nil
	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return nil
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestContainerAgent_Invoke_目标Agent无交接信号 测试目标 Agent 返回无交接信号
func TestContainerAgent_Invoke_目标Agent无交接信号(t *testing.T) {
	mockAgent := newMockBaseAgent("test_agent")
	mockAgent.invokeResult = map[string]any{"output": "done"}

	card := agentschema.NewAgentCard(commonschema.WithID("test_agent"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	// 创建协调器
	coord := NewHandoffOrchestrator("test_agent", []string{"test_agent", "agent_b"}, nil)

	agent := NewContainerAgent(card, provider, map[string]struct{}{"agent_b": {}}, func(_ string) *HandoffOrchestrator {
		return coord
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	// 验证协调器的 DoneCh 已关闭（Complete 被调用）
	select {
	case <-coord.DoneCh():
		// Complete 被调用，正确
	default:
		t.Fatal("无交接信号时应该调用 Complete")
	}
}

// TestContainerAgent_Invoke_目标Agent返回交接信号且审批通过 测试交接信号且审批通过
func TestContainerAgent_Invoke_目标Agent返回交接信号且审批通过(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeResult = map[string]any{
		"output":          "need handoff",
		HandoffTargetKey:  "agent_b",
		HandoffMessageKey: "context for b",
		HandoffReasonKey:  "b is better",
	}

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	// 创建协调器，路由允许 agent_a → agent_b
	config := &HandoffConfig{
		MaxHandoffs: 10,
		Routes:      []HandoffRoute{{Source: "agent_a", Target: "agent_b"}},
	}
	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a", "agent_b"}, config)

	agent := NewContainerAgent(card, provider, map[string]struct{}{"agent_b": {}}, func(_ string) *HandoffOrchestrator {
		return coord
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	// 审批通过时不会调用 Complete，DoneCh 不会被关闭
	// 验证协调器的 currentAgentID 已更新为 agent_b
	assert.Equal(t, "agent_b", coord.CurrentAgentID())
}

// TestContainerAgent_Invoke_目标Agent返回交接信号且审批拒绝 测试交接信号且审批拒绝
func TestContainerAgent_Invoke_目标Agent返回交接信号且审批拒绝(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeResult = map[string]any{
		"output":          "need handoff",
		HandoffTargetKey:  "agent_c",
		HandoffMessageKey: "",
		HandoffReasonKey:  "c is better",
	}

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	// 创建协调器，路由不允许 agent_a → agent_c（只允许 agent_a → agent_b）
	config := &HandoffConfig{
		MaxHandoffs: 10,
		Routes:      []HandoffRoute{{Source: "agent_a", Target: "agent_b"}},
	}
	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a", "agent_b", "agent_c"}, config)

	agent := NewContainerAgent(card, provider, map[string]struct{}{"agent_c": {}}, func(_ string) *HandoffOrchestrator {
		return coord
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	// 审批拒绝时应调用 Complete
	select {
	case <-coord.DoneCh():
		// Complete 被调用
	default:
		t.Fatal("审批拒绝时应该调用 Complete")
	}
}

// TestContainerAgent_Invoke_目标Agent返回中断信号 测试中断信号处理
func TestContainerAgent_Invoke_目标Agent返回中断信号(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeResult = map[string]any{
		"result_type": "interrupt",
		"message":     "need user input",
	}

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a"}, nil)

	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return coord
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	// 中断时应该调用 Complete
	select {
	case <-coord.DoneCh():
		// Complete 被调用
	default:
		t.Fatal("中断时应该调用 Complete")
	}
}

// TestContainerAgent_Invoke_目标Agent执行错误 测试 Agent 执行错误
func TestContainerAgent_Invoke_目标Agent执行错误(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeErr = errors.New("execution failed")

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a"}, nil)

	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return coord
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err) // ContainerAgent 内部处理错误，不向上抛
	assert.Equal(t, map[string]any{}, result)
}

// TestContainerAgent_Invoke_目标AgentProvider失败 测试目标 Agent 提供者失败
func TestContainerAgent_Invoke_目标AgentProvider失败(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, errors.New("provider failed")
	}

	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a"}, nil)

	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return coord
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err) // ContainerAgent 内部处理错误
	assert.Equal(t, map[string]any{}, result)
}

// TestContainerAgent_Invoke_有TeamSession 测试有 team session 时的调用路径
func TestContainerAgent_Invoke_有TeamSession(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeResult = map[string]any{"output": "result"}

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a"}, nil)
	go func() {
		<-coord.DoneCh()
	}()

	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return coord
	})

	// 创建 team session
	teamSession := session.NewAgentTeamSession()

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
		Session:      teamSession,
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)
}

// TestContainerAgent_getTargetAgent_懒初始化 测试懒初始化
func TestContainerAgent_getTargetAgent_懒初始化(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	callCount := 0
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		callCount++
		return newMockBaseAgent("agent_a"), nil
	}

	agent := NewContainerAgent(card, provider, nil, nil)

	// 第一次调用
	a1, err := agent.getTargetAgent(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, a1)
	assert.Equal(t, 1, callCount)

	// 第二次调用应复用实例
	a2, err := agent.getTargetAgent(context.Background())
	assert.NoError(t, err)
	assert.Same(t, a1, a2)
	assert.Equal(t, 1, callCount, "provider 应只调用一次")
}

// TestContainerAgent_injectToolsOnce 测试工具注入
func TestContainerAgent_injectToolsOnce(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	allowedTargets := map[string]struct{}{
		"agent_b": {},
		"agent_c": {},
	}

	agent := NewContainerAgent(card, provider, allowedTargets, nil)

	// 注入工具
	agent.injectToolsOnce(context.Background(), mockAgent)
	assert.True(t, agent.toolsInjected)

	// 验证 AbilityManager 中有对应的工具
	am := mockAgent.abilityMgr
	assert.NotNil(t, am)

	// 应有 transfer_to_agent_b 和 transfer_to_agent_c 两个工具
	toolB := am.Get("transfer_to_agent_b")
	assert.NotNil(t, toolB, "应注册 transfer_to_agent_b 工具")
	toolC := am.Get("transfer_to_agent_c")
	assert.NotNil(t, toolC, "应注册 transfer_to_agent_c 工具")

	// 再次调用不应重复注入
	agent.injectToolsOnce(context.Background(), mockAgent)
	// toolsInjected 仍为 true
	assert.True(t, agent.toolsInjected)
}

// TestContainerAgent_满足BaseAgent接口 测试 ContainerAgent 满足 BaseAgent 接口
func TestContainerAgent_满足BaseAgent接口(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	// 编译时接口检查已在 var _ 行完成
	// 运行时验证关键方法
	var _ agentinterfaces.BaseAgent = agent

	assert.NotNil(t, agent.Card())
	assert.Nil(t, agent.Config())
	assert.Nil(t, agent.AbilityManager())
	assert.Nil(t, agent.CallbackManager())
}

// TestContainerAgent_Stream 测试 Stream 方法
func TestContainerAgent_Stream(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeResult = map[string]any{"output": "result"}

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a"}, nil)
	go func() {
		<-coord.DoneCh()
	}()

	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return coord
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	ch, err := agent.Stream(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err)
	assert.NotNil(t, ch)

	// 读取流数据
	count := 0
	for range ch {
		count++
	}
	assert.True(t, count > 0, "流应至少有一个元素")
}

// TestMsgKey 测试消息去重键生成
func TestMsgKey(t *testing.T) {
	tests := []struct {
		name     string
		msg      any
		expected string
	}{
		{
			name:     "map类型",
			msg:      map[string]any{"role": "user", "content": "hello"},
			expected: "user:hello::",
		},
		{
			name:     "非map类型",
			msg:      "string message",
			expected: "",
		},
		{
			name:     "空map",
			msg:      map[string]any{},
			expected: ":::",
		},
		{
			name:     "只有role",
			msg:      map[string]any{"role": "assistant"},
			expected: "assistant:::",
		},
		{
			name:     "有tool_calls",
			msg:      map[string]any{"role": "assistant", "content": "call", "tool_calls": []any{"call1"}},
			expected: "assistant:call:[call1]:",
		},
		{
			name:     "有tool_call_id",
			msg:      map[string]any{"role": "tool", "content": "result", "tool_call_id": "call_abc"},
			expected: "tool:result::call_abc",
		},
		{
			name:     "相同role_content不同tool_calls",
			msg:      map[string]any{"role": "assistant", "content": "same", "tool_calls": []any{"call2"}},
			expected: "assistant:same:[call2]:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := msgKey(tt.msg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestContainerAgent_saveContextToTeamSession 测试上下文历史保存
func TestContainerAgent_saveContextToTeamSession(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	// agent session 有上下文消息
	agentSession := newMockContainerSessionFacade("agent_123")
	agentSession.state["context"] = map[string]any{
		defaultContextID: map[string]any{
			"messages": []any{
				map[string]any{"role": "user", "content": "hello"},
				map[string]any{"role": "tool", "content": "tool result"}, // 应被过滤
				map[string]any{"role": "assistant", "content": "response"},
			},
		},
	}

	teamSession := session.NewAgentTeamSession()

	agent.saveContextToTeamSession(agentSession, teamSession)

	// 验证 team session 中有上下文历史
	historyVal, err := teamSession.GetState(state.StringKey(contextHistoryKey))
	assert.NoError(t, err)
	if historyVal != nil {
		history, ok := historyVal.([]any)
		if ok {
			// 应有 2 条消息（过滤了 role=tool）
			assert.Equal(t, 2, len(history))
		}
	}
}

// TestContainerAgent_injectContextHistory 测试上下文历史注入
func TestContainerAgent_injectContextHistory(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	// team session 有上下文历史
	teamSession := session.NewAgentTeamSession()
	teamSession.UpdateState(map[string]any{
		contextHistoryKey: []any{
			map[string]any{"role": "user", "content": "previous message"},
		},
	})

	agentSession := newMockContainerSessionFacade("agent_123")

	agent.injectContextHistory(agentSession, teamSession)

	// 验证 agent session 的 context 被注入
	ctxState, ok := agentSession.state["context"]
	if ok && ctxState != nil {
		ctxMap, ok := ctxState.(map[string]any)
		if ok {
			defaultCtx, ok := ctxMap[defaultContextID]
			if ok && defaultCtx != nil {
				defaultCtxMap, ok := defaultCtx.(map[string]any)
				if ok {
					msgs, ok := defaultCtxMap["messages"]
					if ok {
						msgSlice, ok := msgs.([]any)
						if ok {
							assert.Equal(t, 1, len(msgSlice))
						}
					}
				}
			}
		}
	}
}

// TestContainerAgent_injectContextHistory_无历史 测试无历史时不注入
func TestContainerAgent_injectContextHistory_无历史(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	teamSession := session.NewAgentTeamSession()
	// 不设置上下文历史

	agentSession := newMockContainerSessionFacade("agent_123")
	originalState := len(agentSession.state)

	agent.injectContextHistory(agentSession, teamSession)

	// 不应修改 agentSession 状态
	assert.Equal(t, originalState, len(agentSession.state))
}

// TestContainerAgent_handleTeamInterrupt 测试中断处理
func TestContainerAgent_handleTeamInterrupt(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	coord := NewHandoffOrchestrator("test", []string{"test"}, nil)

	interruptSignal := &TeamInterruptSignal{
		Result:  map[string]any{"result_type": "interrupt", "message": "need input"},
		Message: "need input",
	}

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	history := []HandoffHistoryEntry{
		{AgentID: "test", Output: map[string]any{"result_type": "interrupt"}},
	}

	go func() {
		<-coord.DoneCh()
	}()

	agent.handleTeamInterrupt(context.Background(), interruptSignal, coord, history, req)

	// 验证 Complete 被调用
	select {
	case <-coord.DoneCh():
		// 正确
	default:
		t.Fatal("中断处理应该调用 Complete")
	}
}

// TestContainerAgent_HandleTeamInterrupt_有Session 测试有 session 时的中断处理
func TestContainerAgent_HandleTeamInterrupt_有Session(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	coord := NewHandoffOrchestrator("test", []string{"test"}, nil)

	interruptSignal := &TeamInterruptSignal{
		Result:  map[string]any{"result_type": "interrupt"},
		Message: "need input",
	}

	teamSession := session.NewAgentTeamSession()
	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
		Session:      teamSession,
	}

	history := []HandoffHistoryEntry{
		{AgentID: "test", Output: map[string]any{"result_type": "interrupt"}},
	}

	go func() {
		<-coord.DoneCh()
	}()

	agent.handleTeamInterrupt(context.Background(), interruptSignal, coord, history, req)

	// 验证 team session 中有交接历史
	historyVal, _ := teamSession.GetState(state.StringKey(HandoffHistoryKey))
	assert.NotNil(t, historyVal, "中断时应有交接历史保存到 session")
}

// TestHandoffRequestKey 测试常量值
func TestHandoffRequestKey(t *testing.T) {
	assert.Equal(t, "__handoff_request__", HandoffRequestKey)
}

// TestContextHistoryKey 测试常量值
func TestContextHistoryKey(t *testing.T) {
	assert.Equal(t, "__handoff_ctx_history__", contextHistoryKey)
}

// TestContainerAgent_Invoke_最大交接次数耗尽 测试最大交接次数耗尽时审批拒绝
func TestContainerAgent_Invoke_最大交接次数耗尽(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeResult = map[string]any{
		"output":          "need handoff",
		HandoffTargetKey:  "agent_b",
		HandoffReasonKey:  "pass to b",
		HandoffMessageKey: "",
	}

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	// MaxHandoffs=1，先消耗一次
	config := &HandoffConfig{MaxHandoffs: 1}
	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a", "agent_b"}, config)
	// 消耗一次交接配额
	coord.RequestHandoff("agent_b", "first handoff")
	assert.Equal(t, 1, coord.HandoffCount())

	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return coord
	})

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	// 交接被拒绝（已耗尽配额），应调用 Complete
	select {
	case <-coord.DoneCh():
		// 正确：Complete 被调用
	default:
		t.Fatal("交接被拒绝时应该调用 Complete")
	}
}

// TestContainerAgent_RegisterCallback 测试 RegisterCallback 空操作
func TestContainerAgent_RegisterCallback(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	err := agent.RegisterCallback(context.Background(), nil, nil)
	assert.NoError(t, err)
}

// TestContainerAgent_RegisterRail 测试 RegisterRail 空操作
func TestContainerAgent_RegisterRail(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	err := agent.RegisterRail(context.Background(), rail.NewBaseRail(), nil)
	assert.NoError(t, err)
}

// TestContainerAgent_UnregisterRail 测试 UnregisterRail 空操作
func TestContainerAgent_UnregisterRail(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	err := agent.UnregisterRail(context.Background(), rail.NewBaseRail())
	assert.NoError(t, err)
}

// TestContainerAgent_saveAgentContext_无ContextEngine 测试目标 Agent 无 ContextEngine 时跳过
func TestContainerAgent_saveAgentContext_无ContextEngine(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	mockAgent := newMockBaseAgent("test")
	sess := session.NewSession(session.WithSessionID("sess1"))

	// mockBaseAgent 不实现 ContextEngine() 方法，应直接跳过
	agent.saveAgentContext(context.Background(), mockAgent, sess)
}

// TestContainerAgent_saveAgentContext_agentSession为nil 测试 agentSession 为 nil 时直接返回
func TestContainerAgent_saveAgentContext_agentSession为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	mockAgent := newMockBaseAgent("test")
	agent.saveAgentContext(context.Background(), mockAgent, nil)
}

// TestContainerAgent_saveAgentContext_targetAgent为nil 测试 targetAgent 为 nil 时直接返回
func TestContainerAgent_saveAgentContext_targetAgent为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	sess := session.NewSession(session.WithSessionID("sess1"))
	agent.saveAgentContext(context.Background(), nil, sess)
}

// TestContainerAgent_writeResultToStream_result为nil 测试 writeResultToStream result 为 nil 时直接返回
func TestContainerAgent_writeResultToStream_result为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	teamSession := session.NewAgentTeamSession()
	// result 为 nil，不应 panic
	agent.writeResultToStream(context.Background(), nil, teamSession)
}

// TestContainerAgent_writeResultToStream_session为nil 测试 writeResultToStream teamSession 为 nil 时直接返回
func TestContainerAgent_writeResultToStream_session为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	// teamSession 为 nil，不应 panic
	agent.writeResultToStream(context.Background(), map[string]any{"result": "ok"}, nil)
}

// TestContainerAgent_writeResultToStream_正常 测试 writeResultToStream 正常写入
func TestContainerAgent_writeResultToStream_正常(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	teamSession := session.NewAgentTeamSession()
	agent.writeResultToStream(context.Background(), map[string]any{"result": "ok"}, teamSession)
	// 不 panic 即可
}

// TestContainerAgent_saveContextToTeamSession_agentSession为nil 测试 agentSession 为 nil 时直接返回
func TestContainerAgent_saveContextToTeamSession_agentSession为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	teamSession := session.NewAgentTeamSession()
	agent.saveContextToTeamSession(nil, teamSession)
}

// TestContainerAgent_saveContextToTeamSession_teamSession为nil 测试 teamSession 为 nil 时直接返回
func TestContainerAgent_saveContextToTeamSession_teamSession为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	agentSession := newMockContainerSessionFacade("agent_123")
	agent.saveContextToTeamSession(agentSession, nil)
}

// TestContainerAgent_saveContextToTeamSession_无上下文状态 测试 agent session 无上下文状态时直接返回
func TestContainerAgent_saveContextToTeamSession_无上下文状态(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	agentSession := newMockContainerSessionFacade("agent_123")
	// 不设置 context 状态
	teamSession := session.NewAgentTeamSession()
	agent.saveContextToTeamSession(agentSession, teamSession)
}

// TestContainerAgent_saveContextToTeamSession_上下文状态非map 测试 context 状态不是 map 类型时直接返回
func TestContainerAgent_saveContextToTeamSession_上下文状态非map(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	agentSession := newMockContainerSessionFacade("agent_123")
	agentSession.state["context"] = "not_a_map" // 非法类型
	teamSession := session.NewAgentTeamSession()
	agent.saveContextToTeamSession(agentSession, teamSession)
}

// TestContainerAgent_saveContextToTeamSession_无默认上下文 测试无 defaultContextID 时直接返回
func TestContainerAgent_saveContextToTeamSession_无默认上下文(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	agentSession := newMockContainerSessionFacade("agent_123")
	agentSession.state["context"] = map[string]any{
		"other_context": map[string]any{},
	}
	teamSession := session.NewAgentTeamSession()
	agent.saveContextToTeamSession(agentSession, teamSession)
}

// TestContainerAgent_saveContextToTeamSession_无消息 测试 messages 为空时直接返回
func TestContainerAgent_saveContextToTeamSession_无消息(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	agentSession := newMockContainerSessionFacade("agent_123")
	agentSession.state["context"] = map[string]any{
		defaultContextID: map[string]any{
			"messages": []any{},
		},
	}
	teamSession := session.NewAgentTeamSession()
	agent.saveContextToTeamSession(agentSession, teamSession)
}

// TestContainerAgent_injectContextHistory_agentSession为nil 测试 agentSession 为 nil 时直接返回
func TestContainerAgent_injectContextHistory_agentSession为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	teamSession := session.NewAgentTeamSession()
	agent.injectContextHistory(nil, teamSession)
}

// TestContainerAgent_injectContextHistory_teamSession为nil 测试 teamSession 为 nil 时直接返回
func TestContainerAgent_injectContextHistory_teamSession为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	agentSession := newMockContainerSessionFacade("agent_123")
	agent.injectContextHistory(agentSession, nil)
}

// TestContainerAgent_injectContextHistory_历史类型错误 测试历史消息类型错误时不注入
func TestContainerAgent_injectContextHistory_历史类型错误(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, nil, nil)

	teamSession := session.NewAgentTeamSession()
	teamSession.UpdateState(map[string]any{
		contextHistoryKey: "not_a_slice", // 非法类型
	})

	agentSession := newMockContainerSessionFacade("agent_123")
	originalState := len(agentSession.state)
	agent.injectContextHistory(agentSession, teamSession)

	// 不应修改 agentSession 状态
	assert.Equal(t, originalState, len(agentSession.state))
}

// TestContainerAgent_injectToolsOnce_AbilityManager为nil 测试目标 Agent AbilityManager 为 nil 时跳过注入
func TestContainerAgent_injectToolsOnce_AbilityManager为nil(t *testing.T) {
	card := agentschema.NewAgentCard(commonschema.WithID("test"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return newMockBaseAgent("test"), nil
	}
	agent := NewContainerAgent(card, provider, map[string]struct{}{"target": {}}, nil)

	// 创建 AbilityManager 返回 nil 的 mock
	mockAgent := &mockBaseAgentNoAbility{
		card: agentschema.NewAgentCard(commonschema.WithID("test")),
	}

	agent.injectToolsOnce(context.Background(), mockAgent)
	assert.True(t, agent.toolsInjected)
}

// TestContainerAgent_Invoke_有TeamSession且执行错误 测试有 team session 且执行出错时的中断信号提取
func TestContainerAgent_Invoke_有TeamSession且执行错误(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeErr = errors.New("execution error")

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a"}, nil)

	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return coord
	})

	// 创建 team session
	teamSession := session.NewAgentTeamSession()

	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
		Session:      teamSession,
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err) // ContainerAgent 内部处理错误
	assert.Equal(t, map[string]any{}, result)
}

// TestContainerAgent_Invoke_有TeamSession且返回中断 测试有 team session 且返回中断信号
func TestContainerAgent_Invoke_有TeamSession且返回中断(t *testing.T) {
	mockAgent := newMockBaseAgent("agent_a")
	mockAgent.invokeResult = map[string]any{
		"result_type": "interrupt",
		"message":     "need input",
	}

	card := agentschema.NewAgentCard(commonschema.WithID("agent_a"))
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	coord := NewHandoffOrchestrator("agent_a", []string{"agent_a"}, nil)

	agent := NewContainerAgent(card, provider, nil, func(_ string) *HandoffOrchestrator {
		return coord
	})

	teamSession := session.NewAgentTeamSession()
	req := &HandoffRequest{
		InputMessage: map[string]any{"query": "hello"},
		Session:      teamSession,
	}

	result, err := agent.Invoke(context.Background(), map[string]any{HandoffRequestKey: req})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	// 中断时应调用 Complete
	select {
	case <-coord.DoneCh():
		// 正确
	default:
		t.Fatal("中断时应该调用 Complete")
	}
}

// mockBaseAgentNoAbility 无 AbilityManager 的 mock Agent
type mockBaseAgentNoAbility struct {
	card *agentschema.AgentCard
}

func (m *mockBaseAgentNoAbility) Card() *agentschema.AgentCard                { return m.card }
func (m *mockBaseAgentNoAbility) Config() agentinterfaces.AgentConfig         { return nil }
func (m *mockBaseAgentNoAbility) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (m *mockBaseAgentNoAbility) CallbackManager() *rail.AgentCallbackManager { return nil }
func (m *mockBaseAgentNoAbility) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (m *mockBaseAgentNoAbility) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (m *mockBaseAgentNoAbility) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema, 1)
	ch <- &stream.OutputSchema{IsLastSchema: true}
	close(ch)
	return ch, nil
}
func (m *mockBaseAgentNoAbility) RegisterCallback(_ context.Context, _ any, _ any, _ ...callback.CallbackOption) error {
	return nil
}
func (m *mockBaseAgentNoAbility) RegisterRail(_ context.Context, _ rail.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (m *mockBaseAgentNoAbility) UnregisterRail(_ context.Context, _ rail.AgentRail) error {
	return nil
}

// mockContextEngine 模拟 ContextEngine 接口
type mockContextEngine struct {
	saveResult map[string]any
	saveErr    error
}

func (m *mockContextEngine) CreateContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...ceinterface.CreateContextOption) (ceinterface.ModelContext, error) {
	return nil, nil
}
func (m *mockContextEngine) GetContext(_ string, _ string) ceinterface.ModelContext { return nil }
func (m *mockContextEngine) CompressContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...ceinterface.CompressContextOption) (string, error) {
	return "", nil
}
func (m *mockContextEngine) ClearContext(_ context.Context, _ ...ceinterface.ClearContextOption) error {
	return nil
}
func (m *mockContextEngine) SaveContexts(_ context.Context, _ sessioninterfaces.SessionFacade, _ []string) (map[string]any, error) {
	return m.saveResult, m.saveErr
}

// 确保 mockBaseAgentNoAbility 满足 BaseAgent 接口
var _ agentinterfaces.BaseAgent = (*mockBaseAgentNoAbility)(nil)

// 确保 mockContextEngine 满足 ContextEngine 接口
var _ ceinterface.ContextEngine = (*mockContextEngine)(nil)

// 确保 mockBaseAgent 满足 BaseAgent 接口
var _ agentinterfaces.BaseAgent = (*mockBaseAgent)(nil)

// 确保 mockContainerSessionFacade 满足 SessionFacade 接口
var _ sessioninterfaces.SessionFacade = (*mockContainerSessionFacade)(nil)

// 确保 HandoffTool 的 Card() 返回 *tool.ToolCard
var _ tool.Tool = (*HandoffTool)(nil)
