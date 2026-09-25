package handlers

import (
	"context"
	"testing"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// testHandler 测试用子类，覆盖 GetCallbacks
type testHandler struct {
	BaseCoordinationHandler
}

func (h *testHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		"test_event": h.OnTestEvent,
	}
}

func (h *testHandler) OnTestEvent(_ context.Context, _ types.CoordinationEvent) {}

// fakeHost 实现 types.DispatcherHost 用于测试
type fakeHost struct {
	shutdownCalled bool
	cancelCalled   bool
	deliverCalled  bool
}

func (f *fakeHost) IsAgentReady() bool                                       { return true }
func (f *fakeHost) IsAgentRunning() bool                                     { return false }
func (f *fakeHost) HasInFlightRound() bool                                   { return false }
func (f *fakeHost) HasPendingInterrupt() bool                                { return false }
func (f *fakeHost) CancelAgent(_ context.Context) error                      { f.cancelCalled = true; return nil }
func (f *fakeHost) DeliverInput(_ context.Context, _ any, _ bool) error      { f.deliverCalled = true; return nil }
func (f *fakeHost) ResumeInterrupt(_ context.Context, _ *interaction.InteractiveInput) error { return nil }
func (f *fakeHost) ShutdownSelf(_ context.Context) error                     { f.shutdownCalled = true; return nil }
func (f *fakeHost) ConcludeCompletedRound(_ context.Context, _, _ int) error { return nil }

// fakeBP 实现 types.DispatcherBlueprint 用于测试
type fakeBP struct {
	role       schema.TeamRole
	memberName string
}

func (f *fakeBP) Role() schema.TeamRole   { return f.role }
func (f *fakeBP) MemberName() string      { return f.memberName }

// fakePollCtrl 实现 types.PollController 用于测试
type fakePollCtrl struct {
	paused  bool
	resumed bool
}

func (f *fakePollCtrl) PausePolls()  { f.paused = true }
func (f *fakePollCtrl) ResumePolls() { f.resumed = true }

// ──────────────────────────── BaseCoordinationHandler 测试 ────────────────────────────

func TestBaseCoordinationHandler_GetCallbacks_基类返回空(t *testing.T) {
	base := NewBaseCoordinationHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	cb := base.GetCallbacks()
	if len(cb) != 0 {
		t.Errorf("基类 GetCallbacks 应返回空 map，实际 %d 项", len(cb))
	}
}

func TestBaseCoordinationHandler_GetCallbacks_子类覆盖(t *testing.T) {
	h := &testHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{}),
	}
	cb := h.GetCallbacks()
	if len(cb) != 1 {
		t.Errorf("子类 GetCallbacks 应返回 1 项，实际 %d 项", len(cb))
	}
	if _, ok := cb["test_event"]; !ok {
		t.Error("子类 GetCallbacks 应包含 test_event")
	}
}
