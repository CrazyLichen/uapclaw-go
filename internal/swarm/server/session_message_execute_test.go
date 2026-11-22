// Package server mailbox 执行回调单元测试（目标不支持/归属变更分支）。
// Python 对应测试：agent_ws_server execute_internal_session_message 的前置守卫。
package server

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// SessionMessageExecuteSuite 执行回调守卫测试套件。
type SessionMessageExecuteSuite struct {
	suite.Suite
}

// TestSessionMessageExecuteSuite 入口。
func TestSessionMessageExecuteSuite(t *testing.T) {
	suite.Run(t, new(SessionMessageExecuteSuite))
}

// TestExecute_目标不支持 矩阵 #5：mode/channel/cron 守卫 → UNSUPPORTED_TARGET。
func (s *SessionMessageExecuteSuite) TestExecute_目标不支持() {
	svc := &AgentServer{
		sessionMessageMetadataProvider: &fakeMeta{sessions: map[string]map[string]any{
			"sess-B": {"session_id": "sess-B", "mode": "weird", "channel_id": "web"},
		}},
		sessionMessageModeClassifier: fakeModes{},
	}
	res, err := svc.ExecuteInternalSessionMessage(&SessionMessageRecord{
		MessageID: "sm_1", TargetSessionID: "sess-B", OwnerScopeID: "user-1",
	})
	s.Require().NoError(err)
	s.Equal(StatusFailed, res.Status)
	s.Equal("UNSUPPORTED_TARGET", res.ErrorCode)
}

// TestExecute_归属变更 矩阵 #6：执行前 owner 变更 → NOT_FOUND_OR_FORBIDDEN。
func (s *SessionMessageExecuteSuite) TestExecute_归属变更() {
	svc := &AgentServer{
		sessionMessageMetadataProvider: &fakeMeta{sessions: map[string]map[string]any{
			"sess-B": {"session_id": "sess-B", "mode": "agent", "channel_id": "web", "user_id": "user-9"},
		}},
		sessionMessageModeClassifier: fakeModes{},
	}
	res, err := svc.ExecuteInternalSessionMessage(&SessionMessageRecord{
		MessageID: "sm_1", TargetSessionID: "sess-B", OwnerScopeID: "user-1",
	})
	s.Require().NoError(err)
	s.Equal(StatusFailed, res.Status)
	s.Equal("NOT_FOUND_OR_FORBIDDEN", res.ErrorCode)
}

// TestExecute_元数据缺失 缺失目标 → UNSUPPORTED_TARGET。
func (s *SessionMessageExecuteSuite) TestExecute_元数据缺失() {
	svc := &AgentServer{
		sessionMessageMetadataProvider: &fakeMeta{sessions: map[string]map[string]any{}},
		sessionMessageModeClassifier:   fakeModes{},
	}
	res, err := svc.ExecuteInternalSessionMessage(&SessionMessageRecord{
		MessageID: "sm_1", TargetSessionID: "sess-X", OwnerScopeID: "user-1",
	})
	s.Require().NoError(err)
	s.Equal(StatusFailed, res.Status)
	s.Equal("UNSUPPORTED_TARGET", res.ErrorCode)
}

// TestTurnOutcomeTracker 归类语义（Python :644-693 全分支）。
func (s *SessionMessageExecuteSuite) TestTurnOutcomeTracker() {
	tr := &turnOutcomeTracker{}
	s.Equal(StatusUnknown, tr.outcome())
	tr.observe(newRuntimeEvent("chat.final"))
	s.Equal(StatusSucceeded, tr.outcome())
	tr2 := &turnOutcomeTracker{}
	tr2.observe(newRuntimeEvent("chat.ask_user_question"))
	s.Equal(StatusWaitingUser, tr2.outcome())
	tr3 := &turnOutcomeTracker{}
	tr3.observe(newRuntimeEvent("chat.error"))
	s.Equal(StatusFailed, tr3.outcome())
	tr4 := &turnOutcomeTracker{}
	tr4.sawRuntimeAccepted = true
	s.Equal("Runtime accepted the request without confirming completion", tr4.unconfirmedError())
}

// swruntimeEvent 构造 helper（payload 仅 event_type）。
func newRuntimeEvent(eventType string) *runtime.RuntimeEvent {
	return &runtime.RuntimeEvent{OK: true, Payload: map[string]any{"event_type": eventType}}
}
