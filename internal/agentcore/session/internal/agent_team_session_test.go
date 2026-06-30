package internal

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
)

// TestNewAgentTeamSession 测试构造函数及默认值
func TestNewAgentTeamSession(t *testing.T) {
	s := NewAgentTeamSession("test-session-id", "test-team-id")
	if s == nil {
		t.Fatal("NewAgentTeamSession 返回 nil")
	}
	if s.SessionID() != "test-session-id" {
		t.Errorf("SessionID 期望 test-session-id，实际 %s", s.SessionID())
	}
	if s.TeamID() != "test-team-id" {
		t.Errorf("TeamID 期望 test-team-id，实际 %s", s.TeamID())
	}
}

// TestAgentTeamSession_默认字段 测试未传选项时默认值符合 Python 行为
func TestAgentTeamSession_默认字段(t *testing.T) {
	s := NewAgentTeamSession("test-session-id", "test-team-id")

	// config 默认为 nil（Python 不自动创建，由外层传入）
	if s.Config() != nil {
		t.Error("默认 Config 应为 nil")
	}

	// state 默认创建 AgentStateCollection（Python: StateCollection()）
	if s.State() == nil {
		t.Error("State 不应为 nil")
	}
	coll, ok := s.State().(*state.AgentStateCollection)
	if !ok {
		t.Errorf("State 期望 *AgentStateCollection，实际 %T", s.State())
	}
	if coll == nil {
		t.Error("AgentStateCollection 不应为 nil")
	}

	// checkpointer 默认从全局工厂获取（Python: CheckpointerFactory.get_checkpointer()）
	if s.Checkpointer() == nil {
		t.Error("默认 Checkpointer 不应为 nil（对齐 Python 自动获取）")
	}

	// streamWriterManager 默认自动创建（Python: StreamWriterManager(StreamEmitter())）
	if s.StreamWriterManager() == nil {
		t.Error("默认 StreamWriterManager 不应为 nil（对齐 Python 自动创建）")
	}

	// tracer 默认自动创建并初始化（Python: Tracer(); tracer.init(swm)）
	if s.Tracer() == nil {
		t.Error("默认 Tracer 不应为 nil（对齐 Python 自动创建）")
	}

	// teamSpan 默认从 tracer 创建（Python: tracer.tracer_agent_span_manager.create_agent_span()）
	if s.TeamSpan() == nil {
		t.Error("默认 TeamSpan 不应为 nil（对齐 Python 自动创建）")
	}

	// ActorManager 始终返回 nil
	if s.ActorManager() != nil {
		t.Error("默认 ActorManager 应为 nil")
	}
}

// TestAgentTeamSession_InnerSession接口 测试 InnerSession 接口全部方法
func TestAgentTeamSession_InnerSession接口(t *testing.T) {
	s := NewAgentTeamSession("test-session-id", "test-team-id")

	// 验证 InnerSession 接口满足
	var _ interfaces.InnerSession = s

	// Config
	if s.Config() != nil {
		t.Error("默认 Config 应为 nil")
	}

	// State
	if s.State() == nil {
		t.Error("State 不应为 nil")
	}

	// Tracer
	if s.Tracer() == nil {
		t.Error("Tracer 不应为 nil")
	}

	// StreamWriterManager
	if s.StreamWriterManager() == nil {
		t.Error("StreamWriterManager 不应为 nil")
	}

	// SessionID
	if s.SessionID() != "test-session-id" {
		t.Errorf("SessionID 期望 test-session-id，实际 %s", s.SessionID())
	}

	// Checkpointer
	if s.Checkpointer() == nil {
		t.Error("Checkpointer 不应为 nil")
	}

	// ActorManager
	if s.ActorManager() != nil {
		t.Error("ActorManager 应为 nil")
	}

	// Close
	if err := s.Close(); err != nil {
		t.Errorf("Close 应返回 nil，实际 %v", err)
	}
}

// TestAgentTeamSession_TeamID 测试 TeamIDProvider 接口
func TestAgentTeamSession_TeamID(t *testing.T) {
	s := NewAgentTeamSession("test-session-id", "my-team")

	// 验证 TeamIDProvider 接口满足
	var _ interfaces.TeamIDProvider = s

	if s.TeamID() != "my-team" {
		t.Errorf("TeamID 期望 my-team，实际 %s", s.TeamID())
	}
}

// TestAgentTeamSession_TeamSpan 测试 TeamSpan 方法
func TestAgentTeamSession_TeamSpan(t *testing.T) {
	s := NewAgentTeamSession("test-session-id", "test-team-id")

	// 默认自动创建，不为 nil
	if s.TeamSpan() == nil {
		t.Error("默认 TeamSpan 不应为 nil（对齐 Python 自动创建）")
	}

	// 通过 WithTeamSpan 注入自定义 span
	customTracer := tracer.NewTracer()
	customSpan := customTracer.AgentSpanManager.CreateAgentSpan()
	s2 := NewAgentTeamSession("test-session-id", "test-team-id", WithTeamSpan(customSpan))
	if s2.TeamSpan() != customSpan {
		t.Errorf("TeamSpan 期望 customSpan，实际 %v", s2.TeamSpan())
	}
}

// TestAgentTeamSession_构造选项 测试选项覆盖默认值
func TestAgentTeamSession_构造选项(t *testing.T) {
	// WithTeamConfig
	cfg := config.NewSessionConfig(context.Background())
	s1 := NewAgentTeamSession("sid", "tid", WithTeamConfig(cfg))
	if s1.Config() != cfg {
		t.Error("WithTeamConfig 未生效")
	}

	// WithTeamState
	customState := state.NewInMemoryStateLike()
	s2 := NewAgentTeamSession("sid", "tid", WithTeamState(customState))
	if s2.State() != customState {
		t.Error("WithTeamState 未生效")
	}

	// WithTeamTracer
	customTracer := tracer.NewTracer()
	s3 := NewAgentTeamSession("sid", "tid", WithTeamTracer(customTracer))
	if s3.Tracer() != customTracer {
		t.Error("WithTeamTracer 未生效")
	}

	// WithTeamStreamWriterManager
	swm := stream.NewStreamWriterManager(stream.NewStreamEmitter())
	s4 := NewAgentTeamSession("sid", "tid", WithTeamStreamWriterManager(swm))
	if s4.StreamWriterManager() != swm {
		t.Error("WithTeamStreamWriterManager 未生效")
	}

	// WithTeamCheckpointer
	mockCP := &testMockCP{}
	s5 := NewAgentTeamSession("sid", "tid", WithTeamCheckpointer(mockCP))
	if s5.Checkpointer() != mockCP {
		t.Error("WithTeamCheckpointer 未生效")
	}

	// WithTeamSpan
	customSpan := customTracer.AgentSpanManager.CreateAgentSpan()
	s6 := NewAgentTeamSession("sid", "tid", WithTeamSpan(customSpan))
	if s6.TeamSpan() != customSpan {
		t.Error("WithTeamSpan 未生效")
	}
}

// TestAgentTeamSession_Close返回Nil 测试 Close 始终返回 nil
func TestAgentTeamSession_Close返回Nil(t *testing.T) {
	s := NewAgentTeamSession("sid", "tid")
	if err := s.Close(); err != nil {
		t.Errorf("Close 应返回 nil，实际 %v", err)
	}
}
