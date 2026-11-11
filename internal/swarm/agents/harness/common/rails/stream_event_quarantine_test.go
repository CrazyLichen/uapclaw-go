package rails

import (
	"context"
	"testing"

	interruptrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 带状态的 fake session ────────────────────────────

// statefulSession 记录 state 写入的模拟 Session。
//
// 隔离逻辑依赖 state 的读写，而 stream_event_emit_test.go 的 fakeSession
// 把 UpdateState/GetState 都做成空实现，无法验证持久化分支。
type statefulSession struct {
	state     map[string]any
	written   []any
	stateErr  error
	getCalled int
}

func newStatefulSession() *statefulSession {
	return &statefulSession{state: make(map[string]any)}
}

func (s *statefulSession) GetSessionID() string { return "test-session" }

func (s *statefulSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

func (s *statefulSession) GetParentSessionID() string { return "" }

func (s *statefulSession) UpdateState(data map[string]any) {
	for key, value := range data {
		s.state[key] = value
	}
}

func (s *statefulSession) WriteStream(_ context.Context, data any) error {
	s.written = append(s.written, data)
	return nil
}

func (s *statefulSession) WriteCustomStream(_ context.Context, _ any) error { return nil }

func (s *statefulSession) GetState(key state.StateKey) (any, error) {
	s.getCalled++
	if s.stateErr != nil {
		return nil, s.stateErr
	}
	if key.IsAll() {
		return s.state, nil
	}
	return s.state[key.String()], nil
}

func (s *statefulSession) DumpState() map[string]any               { return s.state }
func (s *statefulSession) GetEnv(_ string, _ ...any) any           { return nil }
func (s *statefulSession) Interact(_ context.Context, _ any) error { return nil }
func (s *statefulSession) ClearSession(_ context.Context)          {}

// ──────────────────────────── QuarantineSession ────────────────────────────

// TestQuarantineSession_写入会话状态 测试持久化隔离标记。
//
// Python: quarantine_session 第 2 步 session.update_state({KEY: True})
func TestQuarantineSession_写入会话状态(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := newStatefulSession()

	rail.QuarantineSession("sid-1", session)

	if session.state[interruptrail.PermissionRuntimeQuarantinedKey] != true {
		t.Errorf("会话状态未写入隔离标记，实际 %v", session.state)
	}
}

// TestQuarantineSession_记入内存集合 测试内存标记。
func TestQuarantineSession_记入内存集合(t *testing.T) {
	rail := NewJiuClawStreamEventRail()

	rail.QuarantineSession("sid-1", nil)

	rail.mu.Lock()
	quarantined := rail.quarantinedSessions["sid-1"]
	rail.mu.Unlock()
	if !quarantined {
		t.Error("未记入内存隔离集合")
	}
}

// TestQuarantineSession_终止会话 测试第三步调用 Abort。
//
// Python: self.abort(sid)
func TestQuarantineSession_终止会话(t *testing.T) {
	rail := NewJiuClawStreamEventRail()

	rail.QuarantineSession("sid-1", nil)

	rail.mu.Lock()
	aborted := rail.abortRequested["sid-1"]
	rail.mu.Unlock()
	if !aborted {
		t.Error("隔离后应设置终止标记")
	}
}

// TestQuarantineSession_nilSession 测试无 session 时不 panic。
func TestQuarantineSession_nilSession(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.QuarantineSession("sid-1", nil) // 不应 panic

	rail.mu.Lock()
	defer rail.mu.Unlock()
	if !rail.quarantinedSessions["sid-1"] {
		t.Error("nil session 时仍应记入内存集合")
	}
}

// TestQuarantineSession_空sessionID走默认 测试默认会话键。
func TestQuarantineSession_空sessionID走默认(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.QuarantineSession("", nil)

	rail.mu.Lock()
	defer rail.mu.Unlock()
	if !rail.quarantinedSessions[defaultSessionID] {
		t.Error("空 sessionID 应归一到 defaultSessionID")
	}
}

// ──────────────────────────── RaiseIfQuarantined ────────────────────────────

// TestRaiseIfQuarantined_未隔离返回nil 测试正常会话。
func TestRaiseIfQuarantined_未隔离返回nil(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	if err := rail.RaiseIfQuarantined("sid-1", newStatefulSession()); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// TestRaiseIfQuarantined_内存已隔离 测试内存命中。
func TestRaiseIfQuarantined_内存已隔离(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.QuarantineSession("sid-1", nil)

	err := rail.RaiseIfQuarantined("sid-1", nil)
	if err == nil {
		t.Fatal("err = nil, want session_runtime_quarantined")
	}
	if err.Error() != "session_runtime_quarantined" {
		t.Errorf("err = %v, want session_runtime_quarantined", err)
	}
}

// TestRaiseIfQuarantined_持久化命中 测试从会话状态读到隔离标记。
//
// 场景：进程重启后内存集合为空，但会话状态里仍有隔离标记。
func TestRaiseIfQuarantined_持久化命中(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := newStatefulSession()
	session.state[interruptrail.PermissionRuntimeQuarantinedKey] = true

	err := rail.RaiseIfQuarantined("sid-1", session)
	if err == nil {
		t.Fatal("err = nil, want 从 state 读到隔离标记后拒绝")
	}

	// 命中后应把 sid 补写进内存集合
	rail.mu.Lock()
	backfilled := rail.quarantinedSessions["sid-1"]
	rail.mu.Unlock()
	if !backfilled {
		t.Error("持久化命中后应回填内存集合")
	}
}

// TestRaiseIfQuarantined_持久化为false不隔离 测试 false 不算隔离。
func TestRaiseIfQuarantined_持久化为false不隔离(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := newStatefulSession()
	session.state[interruptrail.PermissionRuntimeQuarantinedKey] = false

	if err := rail.RaiseIfQuarantined("sid-1", session); err != nil {
		t.Errorf("err = %v, want nil（false 不是隔离）", err)
	}
}

// TestRaiseIfQuarantined_state读错误不隔离 测试读错误时按未隔离处理。
func TestRaiseIfQuarantined_state读错误不隔离(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := newStatefulSession()
	session.stateErr = context.DeadlineExceeded

	if err := rail.RaiseIfQuarantined("sid-1", session); err != nil {
		t.Errorf("err = %v, want nil（读 state 失败按未隔离兜底）", err)
	}
}

// TestRaiseIfQuarantined_空sessionID走默认 测试归一化。
func TestRaiseIfQuarantined_空sessionID走默认(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.QuarantineSession(defaultSessionID, nil)

	if err := rail.RaiseIfQuarantined("", nil); err == nil {
		t.Error("空 sessionID 应归一到 defaultSessionID 并命中隔离")
	}
}

// ──────────────────────────── beforeInvoke 接线 ────────────────────────────

// TestBeforeInvoke_隔离会话被拒绝 测试 beforeInvoke 里真的拦住了。
func TestBeforeInvoke_隔离会话被拒绝(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := newStatefulSession()
	rail.QuarantineSession("conv-1", session)

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{ConversationID: "conv-1"}, session)

	err := rail.BeforeInvoke(context.Background(), cbc)
	if err == nil {
		t.Fatal("err = nil, want 隔离会话被拒绝")
	}
	if err.Error() != "session_runtime_quarantined" {
		t.Errorf("err = %v", err)
	}
}

// TestBeforeInvoke_正常会话放行 测试未隔离时正常通过。
func TestBeforeInvoke_正常会话放行(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := newStatefulSession()

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{ConversationID: "conv-2"}, session)

	if err := rail.BeforeInvoke(context.Background(), cbc); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// ──────────────────────────── AfterInvoke 与投影回调 ────────────────────────────

// TestAfterInvoke_未注入回调时无操作 验证装配前的安全空转。
func TestAfterInvoke_未注入回调时无操作(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := newStatefulSession()
	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{ConversationID: "c1"}, session)

	if err := rail.AfterInvoke(context.Background(), cbc); err != nil {
		t.Errorf("err = %v, want nil（未注入回调应空转）", err)
	}
}

// TestAfterInvoke_非InvokeInputs跳过 验证 inputs 类型判据。
func TestAfterInvoke_非InvokeInputs跳过(t *testing.T) {
	called := false
	rail := NewJiuClawStreamEventRail()
	rail.SetInterruptionProjectionDeps(InterruptionProjectionDeps{
		ProjectCompleteInterruption: func(context.Context, *JiuClawStreamEventRail, *sainterfaces.AgentCallbackContext) error {
			called = true
			return nil
		},
	})

	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, newStatefulSession())
	if err := rail.AfterInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Error("inputs 不是 InvokeInputs 时不应调用投影回调")
	}
}

// TestAfterInvoke_无会话跳过 验证 session 为空时跳过。
func TestAfterInvoke_无会话跳过(t *testing.T) {
	called := false
	rail := NewJiuClawStreamEventRail()
	rail.SetInterruptionProjectionDeps(InterruptionProjectionDeps{
		ProjectCompleteInterruption: func(context.Context, *JiuClawStreamEventRail, *sainterfaces.AgentCallbackContext) error {
			called = true
			return nil
		},
	})

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{}, nil)
	if err := rail.AfterInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Error("无会话时不应调用投影回调")
	}
}

// TestAfterInvoke_调用投影回调 验证正常路径。
func TestAfterInvoke_调用投影回调(t *testing.T) {
	called := false
	rail := NewJiuClawStreamEventRail()
	rail.SetInterruptionProjectionDeps(InterruptionProjectionDeps{
		ProjectCompleteInterruption: func(_ context.Context, got *JiuClawStreamEventRail, _ *sainterfaces.AgentCallbackContext) error {
			called = true
			if got != rail {
				t.Error("回调应收到同一个 rail 实例")
			}
			return nil
		},
	})

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{}, newStatefulSession())
	if err := rail.AfterInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("err = %v", err)
	}
	if !called {
		t.Error("应调用投影回调")
	}
}

// TestAfterInvoke_回调错误透传 验证错误不被吞掉。
func TestAfterInvoke_回调错误透传(t *testing.T) {
	sentinel := context.DeadlineExceeded
	rail := NewJiuClawStreamEventRail()
	rail.SetInterruptionProjectionDeps(InterruptionProjectionDeps{
		ProjectCompleteInterruption: func(context.Context, *JiuClawStreamEventRail, *sainterfaces.AgentCallbackContext) error {
			return sentinel
		},
	})

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{}, newStatefulSession())
	if err := rail.AfterInvoke(context.Background(), cbc); err != sentinel {
		t.Errorf("err = %v, want 透传 %v", err, sentinel)
	}
}

// TestProjectionDeps_存取一致 验证 Set/Get 配对。
func TestProjectionDeps_存取一致(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	if deps := rail.ProjectionDeps(); deps.ProjectCompleteInterruption != nil {
		t.Error("初始应为零值")
	}

	rail.SetInterruptionProjectionDeps(InterruptionProjectionDeps{
		ProjectCompleteInterruption: func(context.Context, *JiuClawStreamEventRail, *sainterfaces.AgentCallbackContext) error {
			return nil
		},
	})
	if rail.ProjectionDeps().ProjectCompleteInterruption == nil {
		t.Error("注入后应能取回")
	}
}

// TestResolveSID_导出访问器 验证对外可用且与内部一致。
func TestResolveSID_导出访问器(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := newStatefulSession()
	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{ConversationID: "conv-x"}, session)

	// 未走 BeforeInvoke，回退到 defaultSessionID
	if got := rail.ResolveSID(cbc, session); got != defaultSessionID {
		t.Errorf("got %q, want %q", got, defaultSessionID)
	}
}

// TestPromptLanguage_无Agent返回cn 验证兜底语言。
func TestPromptLanguage_无Agent返回cn(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	if got := rail.PromptLanguage(); got != "cn" {
		t.Errorf("got %q, want cn", got)
	}
}

// TestWrapAfterInvoke_类型不符返回nil 验证回调适配器兜底。
func TestWrapAfterInvoke_类型不符返回nil(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	if err := rail.wrapAfterInvoke(context.Background(), "not-a-context"); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// TestWrapAfterInvoke_正常转调 验证适配器转发。
func TestWrapAfterInvoke_正常转调(t *testing.T) {
	called := false
	rail := NewJiuClawStreamEventRail()
	rail.SetInterruptionProjectionDeps(InterruptionProjectionDeps{
		ProjectCompleteInterruption: func(context.Context, *JiuClawStreamEventRail, *sainterfaces.AgentCallbackContext) error {
			called = true
			return nil
		},
	})

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{}, newStatefulSession())
	if err := rail.wrapAfterInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("err = %v", err)
	}
	if !called {
		t.Error("适配器应转调到 AfterInvoke")
	}
}
