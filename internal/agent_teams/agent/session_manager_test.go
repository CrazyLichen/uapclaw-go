package agent

import (
	"context"
	"testing"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/stretchr/testify/assert"
)

// mockSession 用于测试的 mock session 对象
type mockSession struct {
	sessionID string
}

// GetSessionID 返回 mock session ID
func (m *mockSession) GetSessionID() string {
	return m.sessionID
}

// TestNewSessionManager_基本构造 测试构造函数
// 对齐 Python: SessionManager.__init__(state, configurator, recovery_manager)
func TestNewSessionManager_基本构造(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)
	assert.NotNil(t, m)
	assert.NotNil(t, m.sessionState)
}

// TestSessionManager_TeamSession 测试 TeamSession getter/setter
// 对齐 Python: SessionManager.team_session (property)
func TestSessionManager_TeamSession(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	assert.Nil(t, m.TeamSession())
	sess := &mockSession{sessionID: "sess-1"}
	m.SetTeamSession(sess)
	assert.Equal(t, sess, m.TeamSession())
}

// TestBindSession_基本绑定 测试 sessionID 设置 + ctx 注入 + teamSession 存储
// 对齐 Python: SessionManager.bind_session(session)
//   - set_session_id(session.get_session_id())  → SessionState.SetSessionID
//   - state.team_session = session              → state.TeamSession = session
func TestBindSession_基本绑定(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "sess-123"}
	ctx, err := m.BindSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.Equal(t, "sess-123", m.sessionState.GetSessionID())
	assert.Equal(t, sess, state.TeamSession)
	// 返回的 ctx 应包含 SessionState
	assert.Equal(t, "sess-123", agentteams.GetSessionID(ctx))
}

// TestBindSession_返回新Ctx 测试返回的 ctx 含 SessionState
// 对齐 Python: set_session_id() 后全局生效
// Go: BindSession 返回新 ctx，调用方必须用于后续传播
func TestBindSession_返回新Ctx(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "new-ctx"}
	newCtx, err := m.BindSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.NotNil(t, agentteams.SessionStateFromCtx(newCtx))
	assert.Equal(t, "new-ctx", agentteams.GetSessionID(newCtx))

	// 原 ctx 不含 SessionState
	assert.Nil(t, agentteams.SessionStateFromCtx(context.Background()))
}

// TestBindSession_重复绑定 测试两次 bind 覆盖前一次
// 对齐 Python: bind_session 中 _reset_session_id_token() → set_session_id()
// Go: 重复 BindSession 直接 SetSessionID 覆盖旧值
func TestBindSession_重复绑定(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess1 := &mockSession{sessionID: "sess-1"}
	ctx1, err := m.BindSession(context.Background(), sess1)
	assert.NoError(t, err)

	sess2 := &mockSession{sessionID: "sess-2"}
	ctx2, err := m.BindSession(ctx1, sess2)
	assert.NoError(t, err)

	assert.Equal(t, "sess-2", m.sessionState.GetSessionID())
	assert.Equal(t, sess2, state.TeamSession)
	assert.Equal(t, "sess-2", agentteams.GetSessionID(ctx2))
}

// TestReleaseSession_释放 测试 sessionID 清空 + teamSession 置 nil
// 对齐 Python: SessionManager.release_session()
//   - _reset_session_id_token()  → SessionState.SetSessionID("")
//   - state.team_session = None   → state.TeamSession = nil
func TestReleaseSession_释放(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "sess-1"}
	_, _ = m.BindSession(context.Background(), sess)

	m.ReleaseSession()

	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)
}

// TestReleaseSession_幂等 测试多次 release 不 panic
// 对齐 Python: release_session 幂等（对已释放状态再次调用为 no-op）
func TestReleaseSession_幂等(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	m.ReleaseSession() // 未 bind 就 release
	m.ReleaseSession() // 再次 release
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)
}

// TestSessionManager_三态转换 测试 Unbound → Fully-bound → Unbound
// 对齐 Python: SessionManager 三态模型
//
//	Unbound     — sessionID="" teamSession=nil  从未绑定或显式解绑
//	Fully-bound — sessionID="X" teamSession=ses 活跃会话
//
// 禁止状态：sessionID="" + teamSession!=nil（悬空状态）
func TestSessionManager_三态转换(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	// Unbound: sessionID="" teamSession=nil
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)

	// → Fully-bound
	sess := &mockSession{sessionID: "sess-1"}
	_, _ = m.BindSession(context.Background(), sess)
	assert.Equal(t, "sess-1", m.sessionState.GetSessionID())
	assert.Equal(t, sess, state.TeamSession)

	// → Unbound (ReleaseSession 清空 sessionID 和 teamSession)
	m.ReleaseSession()
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)

	// → Fully-bound again
	sess2 := &mockSession{sessionID: "sess-2"}
	_, _ = m.BindSession(context.Background(), sess2)
	assert.Equal(t, "sess-2", m.sessionState.GetSessionID())
	assert.Equal(t, sess2, state.TeamSession)
}

// TestResumeForNewSession_基本流程 测试返回新 ctx
// 对齐 Python: SessionManager.resume_for_new_session(session)
// TODO(#9.61): RecoveryManager 回填后补充完整逻辑测试
func TestResumeForNewSession_基本流程(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "resume-sess"}
	newCtx, err := m.ResumeForNewSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.Equal(t, "resume-sess", m.sessionState.GetSessionID())
	assert.Equal(t, "resume-sess", agentteams.GetSessionID(newCtx))
}

// TestRecoverForExistingSession_基本流程 测试返回新 ctx
// 对齐 Python: SessionManager.recover_for_existing_session(session)
// TODO(#9.61): RecoveryManager 回填后补充完整逻辑测试
func TestRecoverForExistingSession_基本流程(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "recover-sess"}
	newCtx, err := m.RecoverForExistingSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.Equal(t, "recover-sess", m.sessionState.GetSessionID())
	assert.Equal(t, "recover-sess", agentteams.GetSessionID(newCtx))
}

// TestExtractSessionID_空session 测试 nil session 返回空串
func TestExtractSessionID_空session(t *testing.T) {
	assert.Equal(t, "", extractSessionID(nil))
}

// TestExtractSessionID_有GetSessionID 测试含 GetSessionID 方法的对象
// 对齐 Python: session.get_session_id()
func TestExtractSessionID_有GetSessionID(t *testing.T) {
	sess := &mockSession{sessionID: "extracted-123"}
	assert.Equal(t, "extracted-123", extractSessionID(sess))
}

// TestExtractSessionID_无GetSessionID 测试不含 GetSessionID 方法的对象
func TestExtractSessionID_无GetSessionID(t *testing.T) {
	assert.Equal(t, "", extractSessionID("not-a-session"))
}
