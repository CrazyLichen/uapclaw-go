package team_runtime

import (
	"context"
)

// mockMessageBusForRuntime 测试用 mock 消息总线
type mockMessageBusForRuntime struct {
	sendResult    any
	sendErr       error
	publishErr    error
	subscriptions map[string]map[string]struct{}
}

func newMockMessageBus() *mockMessageBusForRuntime {
	return &mockMessageBusForRuntime{
		subscriptions: make(map[string]map[string]struct{}),
	}
}

func (m *mockMessageBusForRuntime) Start(_ context.Context) error { return nil }
func (m *mockMessageBusForRuntime) Stop(_ context.Context) error  { return nil }
func (m *mockMessageBusForRuntime) CleanupSession(_ context.Context, _ string) error {
	return nil
}
func (m *mockMessageBusForRuntime) Send(_ context.Context, _ any, _ string, _ string, _ string, _ float64) (any, error) {
	return m.sendResult, m.sendErr
}
func (m *mockMessageBusForRuntime) Publish(_ context.Context, _ any, _ string, _ string, _ string) error {
	return m.publishErr
}
func (m *mockMessageBusForRuntime) AddSubscription(agentID, topic string) {
	if _, ok := m.subscriptions[agentID]; !ok {
		m.subscriptions[agentID] = make(map[string]struct{})
	}
	m.subscriptions[agentID][topic] = struct{}{}
}
func (m *mockMessageBusForRuntime) RemoveSubscription(agentID, topic string) {
	if topics, ok := m.subscriptions[agentID]; ok {
		delete(topics, topic)
	}
}
func (m *mockMessageBusForRuntime) RemoveAllSubscriptions(agentID string) {
	delete(m.subscriptions, agentID)
}
func (m *mockMessageBusForRuntime) ListSubscriptions(agentID string) any {
	topics, ok := m.subscriptions[agentID]
	if !ok {
		return &SubscriptionInfo{AgentID: agentID, Topics: []string{}}
	}
	topicList := make([]string, 0, len(topics))
	for t := range topics {
		topicList = append(topicList, t)
	}
	return &SubscriptionInfo{AgentID: agentID, Topics: topicList}
}
func (m *mockMessageBusForRuntime) GetSubscriptionCount() int {
	count := 0
	for _, topics := range m.subscriptions {
		count += len(topics)
	}
	return count
}

// mockRuntimeBindable 测试用 mock RuntimeBindable 实现
type mockRuntimeBindable struct {
	runtime *TeamRuntime
	agentID string
	bound   bool
}

func (m *mockRuntimeBindable) BindRuntime(runtime *TeamRuntime, agentID string) {
	m.runtime = runtime
	m.agentID = agentID
	m.bound = true
}
