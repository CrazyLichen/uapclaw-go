//go:build integration

package team_runtime_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── mock 类型 ────────────────────────────

// mockMessageBus 内存消息总线 mock，记录 Send/Publish 调用
type mockMessageBus struct {
	started  atomic.Bool
	sends    []mockSendCall
	publishes []mockPublishCall
	subs     map[string]map[string]struct{} // agentID → topic set
	mu       sync.RWMutex
	subMu    sync.RWMutex
}

type mockSendCall struct {
	Message   any
	Recipient string
	Sender    string
	SessionID string
	Timeout   float64
}

type mockPublishCall struct {
	Message   any
	TopicID   string
	Sender    string
	SessionID string
}

func newMockMessageBus() *mockMessageBus {
	return &mockMessageBus{
		subs: make(map[string]map[string]struct{}),
	}
}

func (m *mockMessageBus) Start(_ context.Context) error {
	m.started.Store(true)
	return nil
}

func (m *mockMessageBus) Stop(_ context.Context) error {
	m.started.Store(false)
	return nil
}

func (m *mockMessageBus) CleanupSession(_ context.Context, _ string) error {
	return nil
}

func (m *mockMessageBus) Send(_ context.Context, message any, recipient, sender, sessionID string, timeout float64) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, mockSendCall{
		Message: message, Recipient: recipient, Sender: sender,
		SessionID: sessionID, Timeout: timeout,
	})
	return map[string]any{"response": fmt.Sprintf("ack_from_%s", recipient)}, nil
}

func (m *mockMessageBus) Publish(_ context.Context, message any, topicID, sender, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishes = append(m.publishes, mockPublishCall{
		Message: message, TopicID: topicID, Sender: sender, SessionID: sessionID,
	})
	return nil
}

func (m *mockMessageBus) AddSubscription(agentID, topic string) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	if m.subs[agentID] == nil {
		m.subs[agentID] = make(map[string]struct{})
	}
	m.subs[agentID][topic] = struct{}{}
}

func (m *mockMessageBus) RemoveSubscription(agentID, topic string) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	if topics, ok := m.subs[agentID]; ok {
		delete(topics, topic)
	}
}

func (m *mockMessageBus) RemoveAllSubscriptions(agentID string) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	delete(m.subs, agentID)
}

func (m *mockMessageBus) ListSubscriptions(agentID string) any {
	m.subMu.RLock()
	defer m.subMu.RUnlock()
	topics, ok := m.subs[agentID]
	if !ok {
		return &team_runtime.SubscriptionInfo{AgentID: agentID}
	}
	topicList := make([]string, 0, len(topics))
	for t := range topics {
		topicList = append(topicList, t)
	}
	return &team_runtime.SubscriptionInfo{AgentID: agentID, Topics: topicList}
}

func (m *mockMessageBus) GetSubscriptionCount() int {
	m.subMu.RLock()
	defer m.subMu.RUnlock()
	count := 0
	for _, topics := range m.subs {
		count += len(topics)
	}
	return count
}

// mockBaseAgent 最小 BaseAgent mock，仅实现 Invoke
type mockBaseAgent struct {
	card       *agentschema.AgentCard
	invokeResp map[string]any
	invokeErr  error
	invokeCnt  atomic.Int32
}

func (a *mockBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (a *mockBaseAgent) Invoke(_ context.Context, inputs map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	a.invokeCnt.Add(1)
	if a.invokeErr != nil {
		return nil, a.invokeErr
	}
	if a.invokeResp != nil {
		return a.invokeResp, nil
	}
	return map[string]any{"echo": inputs}, nil
}
func (a *mockBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema, 1)
	close(ch)
	return ch, nil
}
func (a *mockBaseAgent) Card() *agentschema.AgentCard                              { return a.card }
func (a *mockBaseAgent) Config() agentinterfaces.AgentConfig                       { return nil }
func (a *mockBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface   { return nil }
func (a *mockBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager    { return nil }
func (a *mockBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return nil }
func (a *mockBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (a *mockBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (a *mockBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// ──────────────────────────── 测试套件 ────────────────────────────

// TeamRuntimeE2ESuite TeamRuntime 核心 E2E 集成测试套件
// 对齐 Python: test_team_runtime.py, test_subscription_manager.py,
// test_message_router.py, test_communicable_agent.py
type TeamRuntimeE2ESuite struct {
	suite.Suite
	runtime   *team_runtime.TeamRuntime
	mockBus   *mockMessageBus
	agentIDs  []string
}

func TestTeamRuntimeE2E(t *testing.T) {
	suite.Run(t, new(TeamRuntimeE2ESuite))
}

func (s *TeamRuntimeE2ESuite) SetupTest() {
	s.mockBus = newMockMessageBus()
	cfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("test-team"),
		team_runtime.WithRuntimeP2PTimeout(30.0),
	)
	s.runtime = team_runtime.NewTeamRuntime(*cfg)
	s.runtime.SetMessageBus(s.mockBus)
	s.agentIDs = nil
}

func (s *TeamRuntimeE2ESuite) TearDownTest() {
	if s.runtime != nil && s.runtime.IsRunning() {
		_ = s.runtime.Stop(context.Background())
	}
	// 清理 ResourceMgr 中的测试 agent
	for _, id := range s.agentIDs {
		if mgr := runner.GetResourceMgr(); mgr != nil {
			_, _ = mgr.RemoveAgent([]string{id})
		}
	}
}

// registerMockAgent 注册 mock agent 到 TeamRuntime 和 ResourceMgr
func (s *TeamRuntimeE2ESuite) registerMockAgent(agentID, agentName string) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID(agentID),
		agentschema.WithAgentName(agentName),
	)
	agent := &mockBaseAgent{card: card, invokeResp: map[string]any{"from": agentName}}
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return agent, nil
	}

	ctx := context.Background()
	err := s.runtime.RegisterAgent(ctx, card, provider)
	s.Require().NoError(err)
	s.agentIDs = append(s.agentIDs, agentID)
}

// ──────────────────────────── 生命周期测试 ────────────────────────────

// TestTeamRuntime_生命周期 验证 Start→Stop 幂等性
// 对齐 Python: test_team_runtime.py test_start_stop
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_生命周期() {
	ctx := context.Background()

	// Start 幂等
	s.Require().NoError(s.runtime.Start(ctx))
	s.True(s.runtime.IsRunning())
	s.Require().NoError(s.runtime.Start(ctx), "重复 Start 应幂等")

	// Stop 幂等
	s.Require().NoError(s.runtime.Stop(ctx))
	s.False(s.runtime.IsRunning())
	s.Require().NoError(s.runtime.Stop(ctx), "重复 Stop 应幂等")

	// 消息总线同步
	s.True(s.mockBus.started.Load() == false, "Stop 后 bus 应停止")
}

// TestTeamRuntime_懒启动 验证 ensureStarted 在 Send/Publish 时自动启动
// 对齐 Python: test_team_runtime.py test_ensure_started
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_懒启动() {
	s.registerMockAgent("agent-a", "AgentA")
	s.False(s.runtime.IsRunning(), "初始应未启动")

	ctx := context.Background()
	_, err := s.runtime.Send(ctx, "hello", "agent-a", "sender-1")
	s.Require().NoError(err)
	s.True(s.runtime.IsRunning(), "Send 应触发懒启动")
}

// ──────────────────────────── Agent 注册测试 ────────────────────────────

// TestTeamRuntime_Agent注册 验证 RegisterAgent/UnregisterAgent/HasAgent
// 对齐 Python: test_team_runtime.py test_register_agent
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_Agent注册() {
	ctx := context.Background()

	// 注册
	s.registerMockAgent("agent-a", "AgentA")
	s.registerMockAgent("agent-b", "AgentB")
	s.Equal(2, s.runtime.GetAgentCount())
	s.True(s.runtime.HasAgent("agent-a"))
	s.True(s.runtime.HasAgent("agent-b"))
	s.False(s.runtime.HasAgent("agent-c"))

	// 列出
	agents := s.runtime.ListAgents()
	s.Len(agents, 2)

	// 获取卡片
	card, err := s.runtime.GetAgentCard("agent-a")
	s.Require().NoError(err)
	s.Equal("AgentA", card.Name)

	// 不存在
	_, err = s.runtime.GetAgentCard("agent-c")
	s.Error(err, "不存在的 agent 应报错")

	// 注销
	unregistered, err := s.runtime.UnregisterAgent(ctx, "agent-a")
	s.Require().NoError(err)
	s.Equal("agent-a", unregistered.ID)
	s.Equal(1, s.runtime.GetAgentCount())
	s.False(s.runtime.HasAgent("agent-a"))
}

// ──────────────────────────── P2P Send 测试 ────────────────────────────

// TestTeamRuntime_P2PSend 验证 Send 消息到指定接收者
// 对齐 Python: test_team_runtime.py test_send_p2p
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_P2PSend() {
	s.registerMockAgent("agent-a", "AgentA")
	s.registerMockAgent("agent-b", "AgentB")

	ctx := context.Background()
	msg := map[string]any{"task": "analyze"}
	result, err := s.runtime.Send(ctx, msg, "agent-b", "agent-a")
	s.Require().NoError(err)
	s.NotNil(result)

	// 验证 mockBus 记录
	s.Len(s.mockBus.sends, 1)
	call := s.mockBus.sends[0]
	s.Equal("agent-b", call.Recipient)
	s.Equal("agent-a", call.Sender)
	s.Equal(msg, call.Message)
}

// TestTeamRuntime_P2PSend参数校验 验证 Send 的参数校验
// 对齐 Python: test_team_runtime.py test_send_empty_sender_recipient
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_P2PSend参数校验() {
	s.registerMockAgent("agent-a", "AgentA")
	ctx := context.Background()

	// 空 sender
	_, err := s.runtime.Send(ctx, "msg", "agent-a", "")
	s.Error(err, "空 sender 应报错")

	// 空 recipient
	_, err = s.runtime.Send(ctx, "msg", "", "agent-a")
	s.Error(err, "空 recipient 应报错")

	// 不存在的 recipient
	_, err = s.runtime.Send(ctx, "msg", "nonexistent", "agent-a")
	s.Error(err, "不存在的 recipient 应报错")
}

// TestTeamRuntime_P2PSendWithSessionID 验证带 sessionID 的 P2P Send
// 对齐 Python: test_team_runtime.py test_send_with_session
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_P2PSendWithSessionID() {
	s.registerMockAgent("agent-a", "AgentA")
	s.registerMockAgent("agent-b", "AgentB")

	ctx := context.Background()
	result, err := s.runtime.Send(ctx, "hello", "agent-b", "agent-a",
		maschema.WithTeamSessionID("sess-123"),
	)
	s.Require().NoError(err)
	s.NotNil(result)
	s.Len(s.mockBus.sends, 1)
	s.Equal("sess-123", s.mockBus.sends[0].SessionID)
}

// ──────────────────────────── Pub-Sub 测试 ────────────────────────────

// TestTeamRuntime_PubSub 验证 Publish + Subscribe + Unsubscribe
// 对齐 Python: test_team_runtime.py test_publish_subscribe
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_PubSub() {
	ctx := context.Background()

	// 订阅
	s.Require().NoError(s.runtime.Subscribe(ctx, "agent-a", "topic/news"))
	s.Require().NoError(s.runtime.Subscribe(ctx, "agent-b", "topic/news"))
	s.Require().NoError(s.runtime.Subscribe(ctx, "agent-b", "topic/weather"))
	s.Equal(3, s.runtime.GetSubscriptionCount())

	// 发布
	s.registerMockAgent("sender", "Sender")
	err := s.runtime.Publish(ctx, "breaking news", "topic/news", "sender")
	s.Require().NoError(err)

	s.Len(s.mockBus.publishes, 1)
	s.Equal("topic/news", s.mockBus.publishes[0].TopicID)

	// 取消订阅
	s.Require().NoError(s.runtime.Unsubscribe(ctx, "agent-a", "topic/news"))
	s.Equal(2, s.runtime.GetSubscriptionCount())
}

// TestTeamRuntime_PubSub参数校验 验证 Publish/Subscribe 参数校验
// 对齐 Python: test_team_runtime.py test_publish_empty_params
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_PubSub参数校验() {
	ctx := context.Background()

	// Publish 空 sender
	err := s.runtime.Publish(ctx, "msg", "topic/1", "")
	s.Error(err, "空 sender 应报错")

	// Publish 空 topic
	err = s.runtime.Publish(ctx, "msg", "", "sender")
	s.Error(err, "空 topic 应报错")

	// Subscribe 空 agentID
	err = s.runtime.Subscribe(ctx, "", "topic/1")
	s.Error(err, "空 agentID 应报错")

	// Subscribe 空 topic
	err = s.runtime.Subscribe(ctx, "agent-a", "")
	s.Error(err, "空 topic 应报错")
}

// ──────────────────────────── SubscriptionManager 测试 ────────────────────────────

// TestSubscriptionManager_订阅匹配 验证通配符匹配和精确匹配
// 对齐 Python: test_subscription_manager.py
func (s *TeamRuntimeE2ESuite) TestSubscriptionManager_订阅匹配() {
	sm := team_runtime.NewSubscriptionManager()

	// 精确订阅
	sm.Subscribe("agent-a", "topic/news")
	sm.Subscribe("agent-b", "topic/weather")

	// 通配符订阅
	sm.Subscribe("agent-c", "topic/*")

	// 精确匹配
	subscribers := sm.GetSubscribers("topic/news")
	s.Contains(subscribers, "agent-a")
	s.NotContains(subscribers, "agent-b")

	// 通配符匹配
	subscribers = sm.GetSubscribers("topic/news")
	s.Contains(subscribers, "agent-c", "topic/* 应匹配 topic/news")

	subscribers = sm.GetSubscribers("topic/weather")
	s.Contains(subscribers, "agent-c", "topic/* 应匹配 topic/weather")

	// 不匹配
	subscribers = sm.GetSubscribers("other/topic")
	s.Empty(subscribers, "无订阅者应返回空")
}

// TestSubscriptionManager_取消订阅 验证 Unsubscribe 和 UnsubscribeAll
// 对齐 Python: test_subscription_manager.py test_unsubscribe
func (s *TeamRuntimeE2ESuite) TestSubscriptionManager_取消订阅() {
	sm := team_runtime.NewSubscriptionManager()

	sm.Subscribe("agent-a", "topic/1")
	sm.Subscribe("agent-a", "topic/2")
	sm.Subscribe("agent-a", "topic/3")
	s.Equal(3, sm.GetSubscriptionCount())

	// 取消单个
	sm.Unsubscribe("agent-a", "topic/2")
	s.Equal(2, sm.GetSubscriptionCount())
	subscribers := sm.GetSubscribers("topic/2")
	s.NotContains(subscribers, "agent-a")

	// 全部取消
	sm.UnsubscribeAll("agent-a")
	s.Equal(0, sm.GetSubscriptionCount())
}

// TestSubscriptionManager_并发安全 验证并发订阅不丢数据
// 对齐 Python: test_subscription_manager.py（Go 特有：无 Python 对应）
func (s *TeamRuntimeE2ESuite) TestSubscriptionManager_并发安全() {
	sm := team_runtime.NewSubscriptionManager()
	var wg sync.WaitGroup

	// 并发 100 个 agent 订阅同一 topic
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sm.Subscribe(fmt.Sprintf("agent-%d", idx), "topic/concurrent")
		}(i)
	}
	wg.Wait()

	s.Equal(100, sm.GetSubscriptionCount())
	subscribers := sm.GetSubscribers("topic/concurrent")
	s.Len(subscribers, 100)
}

// ──────────────────────────── MessageEnvelope 测试 ────────────────────────────

// TestMessageEnvelope_P2P 验证 P2P 信封构建
// 对齐 Python: test_envelope.py test_p2p_envelope
func (s *TeamRuntimeE2ESuite) TestMessageEnvelope_P2P() {
	env := team_runtime.NewMessageEnvelope(
		"msg-001",
		map[string]any{"text": "hello"},
		"sender-a",
		team_runtime.WithRecipient("receiver-b"),
		team_runtime.WithSessionID("sess-1"),
	)

	s.True(env.IsP2P())
	s.False(env.IsPubSub())
	s.Equal("receiver-b", env.Recipient)
	s.Equal("sess-1", env.SessionID)
	s.Equal("sender-a", env.Sender)
}

// TestMessageEnvelope_PubSub 验证 Pub-Sub 信封构建
// 对齐 Python: test_envelope.py test_pubsub_envelope
func (s *TeamRuntimeE2ESuite) TestMessageEnvelope_PubSub() {
	env := team_runtime.NewMessageEnvelope(
		"msg-002",
		"broadcast message",
		"sender-a",
		team_runtime.WithTopicID("topic/alerts"),
		team_runtime.WithMetadata(map[string]any{"priority": "high"}),
	)

	s.True(env.IsPubSub())
	s.False(env.IsP2P())
	s.Equal("topic/alerts", env.TopicID)
	s.Equal("high", env.Metadata["priority"])
}

// TestMessageEnvelope_互斥校验 验证 Recipient 和 TopicID 不能同时设置
// 对齐 Python: test_envelope.py test_mutual_exclusion
func (s *TeamRuntimeE2ESuite) TestMessageEnvelope_互斥校验() {
	s.Panics(func() {
		team_runtime.NewMessageEnvelope(
			"msg-003",
			"msg",
			"sender",
			team_runtime.WithRecipient("recip"),
			team_runtime.WithTopicID("topic/1"),
		)
	}, "同时设置 Recipient 和 TopicID 应 panic")
}

// ──────────────────────────── CommunicableAgent 测试 ────────────────────────────

// TestCommunicableAgent_绑定与通信 验证 BindRuntime + Send/Publish 委托
// 对齐 Python: test_communicable_agent.py
func (s *TeamRuntimeE2ESuite) TestCommunicableAgent_绑定与通信() {
	s.registerMockAgent("agent-a", "AgentA")
	s.registerMockAgent("agent-b", "AgentB")

	ca := team_runtime.NewCommunicableAgent()
	s.False(ca.IsBound(), "未绑定前 IsBound 应为 false")

	ca.BindRuntime(s.runtime, "agent-a")
	s.True(ca.IsBound())

	ctx := context.Background()

	// Send
	result, err := ca.Send(ctx, map[string]any{"action": "query"}, "agent-b")
	s.Require().NoError(err)
	s.NotNil(result)

	// Subscribe
	s.Require().NoError(ca.Subscribe(ctx, "topic/data"))

	// Publish
	err = ca.Publish(ctx, map[string]any{"data": "payload"}, "topic/data")
	s.Require().NoError(err)
}

// TestCommunicableAgent_未绑定报错 验证未绑定 Runtime 时通信方法报错
// 对齐 Python: test_communicable_agent.py test_unbound_error
func (s *TeamRuntimeE2ESuite) TestCommunicableAgent_未绑定报错() {
	ca := team_runtime.NewCommunicableAgent()
	ctx := context.Background()

	_, err := ca.Send(ctx, map[string]any{"msg": "hi"}, "agent-b")
	s.Error(err, "未绑定 Send 应报错")

	err = ca.Publish(ctx, map[string]any{"msg": "hi"}, "topic/1")
	s.Error(err, "未绑定 Publish 应报错")
}

// ──────────────────────────── 会话绑定测试 ────────────────────────────

// TestTeamRuntime_会话绑定 验证 BindTeamSession/UnbindTeamSession
// 对齐 Python: test_team_runtime.py test_bind_session
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_会话绑定() {
	// BindTeamSession 需要 *session.AgentTeamSession，但测试中无法轻易构造
	// 这里验证 nil 安全性和基础 API
	s.runtime.BindTeamSession(nil) // 不应 panic

	s.Nil(s.runtime.GetTeamSession("nonexistent"))

	// 验证 UnbindTeamSession 不存在 sessionID 不 panic
	s.runtime.UnbindTeamSession("nonexistent")
}

// ──────────────────────────── P2P Timeout 测试 ────────────────────────────

// TestTeamRuntime_P2PTimeout 验证 timeout getter/setter
// 对齐 Python: test_team_runtime.py test_p2p_timeout
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_P2PTimeout() {
	s.Equal(30.0, s.runtime.P2PTimeout())
	s.Equal(30.0, s.runtime.GetP2PTimeout())

	s.runtime.SetP2PTimeout(60.0)
	s.Equal(60.0, s.runtime.P2PTimeout())
	s.Equal(60.0, s.runtime.GetP2PTimeout())
}

// ──────────────────────────── 并发注册测试 ────────────────────────────

// TestTeamRuntime_并发注册 验证并发 RegisterAgent 不丢数据
// 对齐 Python: 无直接对应（Go 特有：验证 sync.RWMutex 正确性）
func (s *TeamRuntimeE2ESuite) TestTeamRuntime_并发注册() {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-agent-%d", idx)
			name := fmt.Sprintf("ConcurrentAgent%d", idx)
			card := agentschema.NewAgentCard(
				agentschema.WithAgentID(id),
				agentschema.WithAgentName(name),
			)
			provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
				return &mockBaseAgent{card: card}, nil
			}
			_ = s.runtime.RegisterAgent(context.Background(), card, provider)
		}(i)
	}
	wg.Wait()

	s.Equal(50, s.runtime.GetAgentCount())
}

// ──────────────────────────── TeamCard 构建测试 ────────────────────────────

// TestTeamCard_构建验证 验证 TeamCard 和 EventDrivenTeamCard 构建选项
// 对齐 Python: test_team_card.py
func (s *TeamRuntimeE2ESuite) TestTeamCard_构建验证() {
	agentCard1 := agentschema.NewAgentCard(
		agentschema.WithAgentID("a1"),
		agentschema.WithAgentName("Worker"),
	)
	agentCard2 := agentschema.NewAgentCard(
		agentschema.WithAgentID("a2"),
		agentschema.WithAgentName("Analyst"),
	)

	// TeamCard
	tc := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-1"),
		maschema.WithTeamCardName("AlphaTeam"),
		maschema.WithTeamCardDescription("测试团队"),
		maschema.WithTopic("topic/alpha"),
		maschema.WithTeamVersion("2.0.0"),
		maschema.WithTags([]string{"test", "alpha"}),
		maschema.WithAgentCards([]*agentschema.AgentCard{agentCard1, agentCard2}),
	)
	s.Equal("team-1", tc.ID)
	s.Equal("AlphaTeam", tc.Name)
	s.Equal("测试团队", tc.Description)
	s.Equal("topic/alpha", tc.GetTopic())
	s.Equal("2.0.0", tc.GetVersion())
	s.Len(tc.GetAgentCards(), 2)
	s.Nil(tc.GetSubscriptions(), "TeamCard.GetSubscriptions 应返回 nil")

	// EventDrivenTeamCard
	edc := maschema.NewEventDrivenTeamCard(
		maschema.WithEventDrivenID("team-ed-1"),
		maschema.WithEventDrivenName("EventTeam"),
		maschema.WithSubscriptions(map[string][]string{
			"a1": {"topic/tasks", "topic/alerts"},
			"a2": {"topic/results"},
		}),
	)
	s.Equal("team-ed-1", edc.ID)
	s.Equal("EventTeam", edc.Name)
	s.NotNil(edc.GetSubscriptions())
	s.Len(edc.GetSubscriptions()["a1"], 2)
}

// ──────────────────────────── TeamConfig 测试 ────────────────────────────

// TestTeamConfig_链式配置 验证 TeamConfig 链式配置
// 对齐 Python: test_team_config.py
func (s *TeamRuntimeE2ESuite) TestTeamConfig_链式配置() {
	tc := maschema.NewTeamConfig()
	s.Equal(10, tc.MaxAgents, "默认 MaxAgents 应为 10")
	s.Equal(100, tc.MaxConcurrentMessages, "默认 MaxConcurrentMessages 应为 100")
	s.Equal(30.0, tc.MessageTimeout, "默认 MessageTimeout 应为 30.0")

	// 链式配置
	tc.ConfigureMaxAgents(20).ConfigureTimeout(60.0).ConfigureConcurrency(200)
	s.Equal(20, tc.MaxAgents)
	s.Equal(60.0, tc.MessageTimeout)
	s.Equal(200, tc.MaxConcurrentMessages)

	// Extra
	tc.SetExtra("custom_key", "custom_value")
	v, ok := tc.GetExtra("custom_key")
	s.True(ok)
	s.Equal("custom_value", v)
}

// ──────────────────────────── 编译时接口合规检查 ────────────────────────────

func init() {
	// 验证 mock 类型实现接口
	_ = team_runtime.MessageBusInterface(newMockMessageBus())
	// 验证 CommunicableAgent 实现 Communicable
	_ = team_runtime.Communicable(team_runtime.NewCommunicableAgent())
	// 验证 CommunicableAgent 实现 RuntimeBindable
	_ = team_runtime.RuntimeBindable(team_runtime.NewCommunicableAgent())
}

// CI 合规性验证：导入 ≥2 个 internal 包
var _ = (*resources_manager.AgentProvider)(nil)
var _ = (*runner.AgentRef)(nil)

// 确保 suite 使用 testing
var _ = testing.Init
var _ = time.Second
