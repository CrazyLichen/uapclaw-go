package handlers

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// testHandler 测试用子类，覆盖 GetCallbacks
type testHandler struct {
	BaseCoordinationHandler
}

func (h *testHandler) GetCallbacks() map[string]coordination.EventCallbackFunc {
	return map[string]coordination.EventCallbackFunc{
		"test_event": h.OnTestEvent,
	}
}

func (h *testHandler) OnTestEvent(_ context.Context, _ coordination.CoordinationEvent) {}

// fakeHost 实现 coordination.DispatcherHost 用于测试
type fakeHost struct{}

func (f *fakeHost) IsAgentReady() bool                                                { return true }
func (f *fakeHost) IsAgentRunning() bool                                              { return false }
func (f *fakeHost) HasInFlightRound() bool                                            { return false }
func (f *fakeHost) HasPendingInterrupt() bool                                         { return false }
func (f *fakeHost) CancelAgent(_ context.Context) error                               { return nil }
func (f *fakeHost) DeliverInput(_ context.Context, _ any, _ bool) error               { return nil }
func (f *fakeHost) ResumeInterrupt(_ context.Context, _ any) error                    { return nil }
func (f *fakeHost) ShutdownSelf(_ context.Context) error                              { return nil }
func (f *fakeHost) ConcludeCompletedRound(_ context.Context, _, _ int) error          { return nil }

// fakeBP 实现 coordination.DispatcherBlueprint 用于测试
type fakeBP struct {
	role       schema.TeamRole
	memberName string
}

func (f *fakeBP) Role() schema.TeamRole   { return f.role }
func (f *fakeBP) MemberName() string      { return f.memberName }

// fakePollCtrl 实现 coordination.PollController 用于测试
type fakePollCtrl struct{}

func (f *fakePollCtrl) PausePolls()  {}
func (f *fakePollCtrl) ResumePolls() {}

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
