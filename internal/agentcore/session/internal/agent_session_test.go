package internal

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// TestNewAgentSession 测试构造函数
func TestNewAgentSession(t *testing.T) {
	s := NewAgentSession("test-id")
	if s == nil {
		t.Fatal("NewAgentSession 返回 nil")
	}
	if s.SessionID() != "test-id" {
		t.Errorf("SessionID 期望 test-id，实际 %s", s.SessionID())
	}
}

// TestAgentSession_默认字段为Nil 测试未传选项时部分字段返回 nil。
// checkpointer 不再为 nil：对齐 Python，nil 时自动从全局工厂获取默认 InMemoryCheckpointer。
// streamWriterManager 不再为 nil：对齐 Python，nil 时自动创建 StreamWriterManager(StreamEmitter())。
// tracer 不再为 nil：对齐 Python，nil 时自动创建 Tracer() 并 Init(swm)。
// agentSpan 不再为 nil：对齐 Python，nil 时从 tracer.AgentSpanManager.CreateAgentSpan() 创建。
func TestAgentSession_默认字段为Nil(t *testing.T) {
	s := NewAgentSession("test-id")
	if s.Config() != nil {
		t.Error("默认 Config 应为 nil")
	}
	// ✅ 5.11 已回填：Tracer 默认自动创建，不再为 nil
	if s.Tracer() == nil {
		t.Error("默认 Tracer 不应为 nil（对齐 Python 自动创建）")
	}
	if s.StreamWriterManager() == nil {
		t.Error("默认 StreamWriterManager 不应为 nil（对齐 Python 自动创建）")
	}
	// checkpointer 不再为 nil，对齐 Python：CheckpointerFactory.get_checkpointer()
	if s.Checkpointer() == nil {
		t.Error("默认 Checkpointer 不应为 nil（对齐 Python 自动获取）")
	}
	if s.ActorManager() != nil {
		t.Error("默认 ActorManager 应为 nil")
	}
	// ✅ 5.11 已回填：AgentSpan 默认自动创建，不再为 nil
	if s.AgentSpan() == nil {
		t.Error("默认 AgentSpan 不应为 nil（对齐 Python 自动创建）")
	}
}

// TestAgentSession_State不为Nil 测试默认创建 AgentStateCollection
func TestAgentSession_State不为Nil(t *testing.T) {
	s := NewAgentSession("test-id")
	if s.State() == nil {
		t.Error("State 不应为 nil")
	}
	// 验证 State 是 AgentStateCollection
	coll, ok := s.State().(*state.AgentStateCollection)
	if !ok {
		t.Errorf("State 期望 *AgentStateCollection，实际 %T", s.State())
	}
	if coll == nil {
		t.Error("AgentStateCollection 不应为 nil")
	}
}

// TestAgentSession_选项注入 测试通过选项注入组件
func TestAgentSession_选项注入(t *testing.T) {
	config := map[string]any{"key": "value"}
	card := &schema.AgentCard{BaseCard: schema.BaseCard{ID: "test-agent"}}
	s := NewAgentSession("test-id",
		WithConfig(config),
		WithCard(card),
	)

	if s.Config() == nil {
		t.Error("Config 不应为 nil")
	}
	if s.Card() == nil {
		t.Error("Card 不应为 nil")
	}
	if s.AgentID() != "test-agent" {
		t.Errorf("AgentID 期望 test-agent，实际 %s", s.AgentID())
	}
}

// TestAgentSession_ActorManager返回Nil 测试 ActorManager 始终返回 nil
func TestAgentSession_ActorManager返回Nil(t *testing.T) {
	s := NewAgentSession("test-id")
	if s.ActorManager() != nil {
		t.Error("ActorManager 应始终返回 nil")
	}
}

// TestAgentSession_Close返回Nil 测试 Close 始终返回 nil
func TestAgentSession_Close返回Nil(t *testing.T) {
	s := NewAgentSession("test-id")
	if err := s.Close(); err != nil {
		t.Errorf("Close 应返回 nil，实际 %v", err)
	}
}

// TestAgentSession_Card 测试 Card 方法
func TestAgentSession_Card(t *testing.T) {
	s := NewAgentSession("test-id")
	if s.Card() != nil {
		t.Error("默认 Card 应为 nil")
	}
	if s.AgentID() != "" {
		t.Errorf("无 Card 时 AgentID 应返回空字符串，实际 %s", s.AgentID())
	}

	card := &schema.AgentCard{BaseCard: schema.BaseCard{ID: "my-agent"}}
	s2 := NewAgentSession("test-id", WithCard(card))
	if s2.Card() != card {
		t.Errorf("Card 期望 %v，实际 %v", card, s2.Card())
	}
	if s2.AgentID() != "my-agent" {
		t.Errorf("AgentID 期望 my-agent，实际 %s", s2.AgentID())
	}
}

// TestAgentSession_AgentSpan 测试 AgentSpan 方法
// ✅ 5.11 已回填：AgentSpan 类型改为 *tracer.TraceAgentSpan
func TestAgentSession_AgentSpan(t *testing.T) {
	s := NewAgentSession("test-id")
	// 默认自动创建，不再为 nil
	if s.AgentSpan() == nil {
		t.Error("默认 AgentSpan 不应为 nil（对齐 Python 自动创建）")
	}

	// 通过 WithAgentSpan 注入自定义 span
	customTracer := tracer.NewTracer()
	customSpan := customTracer.AgentSpanManager.CreateAgentSpan()
	s2 := NewAgentSession("test-id", WithAgentSpan(customSpan))
	if s2.AgentSpan() != customSpan {
		t.Errorf("AgentSpan 期望 customSpan，实际 %v", s2.AgentSpan())
	}
}

// TestAgentSession_WithTracer 测试 WithTracer 选项
// ✅ 5.11 已回填：WithTracer 参数类型改为 *tracer.Tracer
func TestAgentSession_WithTracer(t *testing.T) {
	customTracer := tracer.NewTracer()
	s := NewAgentSession("test-id", WithTracer(customTracer))
	if s.Tracer() != customTracer {
		t.Errorf("Tracer 期望 customTracer，实际 %v", s.Tracer())
	}
}

// TestAgentSession_WithStreamWriterManager 测试 WithStreamWriterManager 选项
func TestAgentSession_WithStreamWriterManager(t *testing.T) {
	swm := stream.NewStreamWriterManager(stream.NewStreamEmitter())
	s := NewAgentSession("test-id", WithStreamWriterManager(swm))
	if s.StreamWriterManager() != swm {
		t.Errorf("StreamWriterManager 期望 swm，实际 %v", s.StreamWriterManager())
	}
}

// testMockCP 用于 agent_session_test 的模拟检查点器
type testMockCP struct{}

func (m *testMockCP) PreWorkflowExecute(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	return nil
}
func (m *testMockCP) PostWorkflowExecute(ctx context.Context, session interfaces.BaseSession, result any, exception error) error {
	return nil
}
func (m *testMockCP) PreAgentExecute(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	return nil
}
func (m *testMockCP) PreAgentTeamExecute(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	return nil
}
func (m *testMockCP) InterruptAgentExecute(ctx context.Context, session interfaces.BaseSession) error {
	return nil
}
func (m *testMockCP) PostAgentExecute(ctx context.Context, session interfaces.BaseSession) error {
	return nil
}
func (m *testMockCP) PostAgentTeamExecute(ctx context.Context, session interfaces.BaseSession) error {
	return nil
}
func (m *testMockCP) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	return false, nil
}
func (m *testMockCP) Release(ctx context.Context, sessionID string, agentID ...string) error {
	return nil
}
func (m *testMockCP) GraphStore() any { return nil }

// TestAgentSession_WithCheckpointer 测试 WithCheckpointer 选项
func TestAgentSession_WithCheckpointer(t *testing.T) {
	mockCP := &testMockCP{}
	s := NewAgentSession("test-id", WithCheckpointer(mockCP))
	if s.Checkpointer() != mockCP {
		t.Errorf("Checkpointer 期望 mockCP，实际 %v", s.Checkpointer())
	}
}

// TestAgentSession_WithState 测试 WithState 选项
func TestAgentSession_WithState(t *testing.T) {
	customState := state.NewInMemoryStateLike()
	s := NewAgentSession("test-id", WithState(customState))
	if s.State() != customState {
		t.Errorf("State 期望 customState 实例，实际 %v", s.State())
	}
}
