package session

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Test接口满足_ProxySession 验证 ProxySession 满足 BaseSession 接口。
func Test接口满足_ProxySession(t *testing.T) {
	var _ BaseSession = (*ProxySession)(nil)
}

// TestNewProxySession 验证 NewProxySession 创建的实例 stub 为 nil。
func TestNewProxySession(t *testing.T) {
	p := NewProxySession()
	if p.stub != nil {
		t.Errorf("NewProxySession() stub 应为 nil，实际为 %v", p.stub)
	}
}

// ──────────────────────────── 非导出类型 ────────────────────────────

// mockStub 用于测试的 BaseSession 模拟实现
type mockStub struct {
	configVal              any
	stateVal               state.SessionState
	tracerVal              any
	streamWriterManagerVal any
	sessionIDVal           string
	checkpointerVal        checkpointer.Checkpointer
	actorManagerVal        any
	closeErr               error
	closeCalled            bool
}

func (m *mockStub) Config() any                             { return m.configVal }
func (m *mockStub) State() state.SessionState               { return m.stateVal }
func (m *mockStub) Tracer() any                             { return m.tracerVal }
func (m *mockStub) StreamWriterManager() any                { return m.streamWriterManagerVal }
func (m *mockStub) SessionID() string                       { return m.sessionIDVal }
func (m *mockStub) Checkpointer() checkpointer.Checkpointer { return m.checkpointerVal }
func (m *mockStub) ActorManager() any                       { return m.actorManagerVal }
func (m *mockStub) Close() error                            { m.closeCalled = true; return m.closeErr }

// testMockCheckpointer 用于 session_test 的模拟检查点器
type testMockCheckpointer struct{}

func (m *testMockCheckpointer) GetThreadID(session checkpointer.CheckpointerSession) string { return "" }
func (m *testMockCheckpointer) PreWorkflowExecute(ctx context.Context, session checkpointer.CheckpointerSession, inputs any) error {
	return nil
}
func (m *testMockCheckpointer) PostWorkflowExecute(ctx context.Context, session checkpointer.CheckpointerSession, result any, exception error) error {
	return nil
}
func (m *testMockCheckpointer) PreAgentExecute(ctx context.Context, session checkpointer.CheckpointerSession, inputs any) error {
	return nil
}
func (m *testMockCheckpointer) PreAgentTeamExecute(ctx context.Context, session checkpointer.CheckpointerSession, inputs any) error {
	return nil
}
func (m *testMockCheckpointer) InterruptAgentExecute(ctx context.Context, session checkpointer.CheckpointerSession) error {
	return nil
}
func (m *testMockCheckpointer) PostAgentExecute(ctx context.Context, session checkpointer.CheckpointerSession) error {
	return nil
}
func (m *testMockCheckpointer) PostAgentTeamExecute(ctx context.Context, session checkpointer.CheckpointerSession) error {
	return nil
}
func (m *testMockCheckpointer) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	return false, nil
}
func (m *testMockCheckpointer) Release(ctx context.Context, sessionID string) error { return nil }
func (m *testMockCheckpointer) GraphStore() any                                     { return nil }

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestProxySession_SetSession 验证 SetSession 正确注入底层会话。
func TestProxySession_SetSession(t *testing.T) {
	p := NewProxySession()
	stub := &mockStub{sessionIDVal: "test-id"}
	p.SetSession(stub)
	if p.stub != stub {
		t.Error("SetSession 后 stub 应指向注入的实例")
	}
}

// TestProxySession_委托全部方法 验证 ProxySession 的 8 个方法全部委托给 stub。
func TestProxySession_委托全部方法(t *testing.T) {
	// 准备 mock 数据
	expectedState := state.NewInMemoryStateLike()
	mockCP := &testMockCheckpointer{}
	stub := &mockStub{
		configVal:              "config-value",
		stateVal:               expectedState,
		tracerVal:              "tracer-value",
		streamWriterManagerVal: "swm-value",
		sessionIDVal:           "session-123",
		checkpointerVal:        mockCP,
		actorManagerVal:        "actor-value",
		closeErr:               nil,
	}

	p := NewProxySession()
	p.SetSession(stub)

	// 验证 Config 委托
	if got := p.Config(); got != "config-value" {
		t.Errorf("Config() = %v, 期望 %v", got, "config-value")
	}

	// 验证 State 委托
	if got := p.State(); got != expectedState {
		t.Errorf("State() = %v, 期望 %v", got, expectedState)
	}

	// 验证 Tracer 委托
	if got := p.Tracer(); got != "tracer-value" {
		t.Errorf("Tracer() = %v, 期望 %v", got, "tracer-value")
	}

	// 验证 StreamWriterManager 委托
	if got := p.StreamWriterManager(); got != "swm-value" {
		t.Errorf("StreamWriterManager() = %v, 期望 %v", got, "swm-value")
	}

	// 验证 SessionID 委托
	if got := p.SessionID(); got != "session-123" {
		t.Errorf("SessionID() = %v, 期望 %v", got, "session-123")
	}

	// 验证 Checkpointer 委托
	if got := p.Checkpointer(); got != mockCP {
		t.Errorf("Checkpointer() = %v, 期望 %v", got, mockCP)
	}

	// 验证 ActorManager 委托
	if got := p.ActorManager(); got != "actor-value" {
		t.Errorf("ActorManager() = %v, 期望 %v", got, "actor-value")
	}

	// 验证 Close 委托
	err := p.Close()
	if err != nil {
		t.Errorf("Close() 返回意外错误: %v", err)
	}
	if !stub.closeCalled {
		t.Error("Close() 未委托到 stub")
	}
}

// TestProxySession_Close传播错误 验证 ProxySession.Close 传播 stub 的错误。
func TestProxySession_Close传播错误(t *testing.T) {
	expectedErr := errors.New("close failed")
	stub := &mockStub{closeErr: expectedErr}

	p := NewProxySession()
	p.SetSession(stub)

	err := p.Close()
	if err != expectedErr {
		t.Errorf("Close() 错误 = %v, 期望 %v", err, expectedErr)
	}
}

// TestProxySession_NilStub时Panic 验证 stub 为 nil 时调用方法 panic。
func TestProxySession_NilStub时Panic(t *testing.T) {
	p := NewProxySession()

	// 测试每个方法在 nil stub 时 panic
	panicTests := []struct {
		name string
		fn   func()
	}{
		{"Config", func() { p.Config() }},
		{"State", func() { p.State() }},
		{"Tracer", func() { p.Tracer() }},
		{"StreamWriterManager", func() { p.StreamWriterManager() }},
		{"SessionID", func() { p.SessionID() }},
		{"Checkpointer", func() { p.Checkpointer() }},
		{"ActorManager", func() { p.ActorManager() }},
		{"Close", func() { _ = p.Close() }},
	}

	for _, tt := range panicTests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s 在 nil stub 时未 panic", tt.name)
				}
			}()
			tt.fn()
		})
	}
}

// TestAgentSession_接口实现 在 session 包中验证 AgentSession 满足 BaseSession 接口
// （internal 包不能导入 session 包，否则循环依赖）
func TestAgentSession_接口实现(t *testing.T) {
	var _ BaseSession = internal.NewAgentSession("test")
}
