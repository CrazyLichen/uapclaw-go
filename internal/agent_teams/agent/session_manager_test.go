package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestAgentTeamSession 构造带指定 sessionID 的 *session.AgentTeamSession。
// M-29: TeamSession 存储收窄到具体类型后，测试统一使用真实 AgentTeamSession。
func newTestAgentTeamSession(sessionID string) *session.AgentTeamSession {
	return session.NewAgentTeamSession(session.WithAgentTeamSessionID(sessionID))
}

// TestNewSessionManager_基本构造 测试构造函数
// Python: SessionManager.__init__(state, configurator, recovery_manager)
func TestNewSessionManager_基本构造(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)
	assert.NotNil(t, m)
	assert.NotNil(t, m.sessionState)
}

// TestSessionManager_TeamSession 测试 TeamSession getter/setter
// Python: SessionManager.team_session (property)
func TestSessionManager_TeamSession(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	assert.Nil(t, m.TeamSession())
	sess := newTestAgentTeamSession("sess-1")
	m.SetTeamSession(sess)
	assert.Equal(t, sess, m.TeamSession())
}

// TestBindSession_基本绑定 测试 sessionID 设置 + ctx 注入 + teamSession 存储
// Python: SessionManager.bind_session(session)
//   - set_session_id(session.get_session_id())  → SessionState.SetSessionID
//   - state.team_session = session              → state.TeamSession = session
func TestBindSession_基本绑定(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := newTestAgentTeamSession("sess-123")
	ctx, err := m.BindSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.Equal(t, "sess-123", m.sessionState.GetSessionID())
	assert.Equal(t, sess, state.TeamSession)
	// 返回的 ctx 应包含 SessionState
	assert.Equal(t, "sess-123", agentteams.GetSessionID(ctx))
}

// TestBindSession_返回新Ctx 测试返回的 ctx 含 SessionState
// Python: set_session_id() 后全局生效
// Go: BindSession 返回新 ctx，调用方必须用于后续传播
func TestBindSession_返回新Ctx(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := newTestAgentTeamSession("new-ctx")
	newCtx, err := m.BindSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.NotNil(t, agentteams.SessionStateFromCtx(newCtx))
	assert.Equal(t, "new-ctx", agentteams.GetSessionID(newCtx))

	// 原 ctx 不含 SessionState
	assert.Nil(t, agentteams.SessionStateFromCtx(context.Background()))
}

// TestBindSession_重复绑定 测试两次 bind 覆盖前一次
// Python: bind_session 中 _reset_session_id_token() → set_session_id()
// Go: 重复 BindSession 直接 SetSessionID 覆盖旧值
func TestBindSession_重复绑定(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess1 := newTestAgentTeamSession("sess-1")
	ctx1, err := m.BindSession(context.Background(), sess1)
	assert.NoError(t, err)

	sess2 := newTestAgentTeamSession("sess-2")
	ctx2, err := m.BindSession(ctx1, sess2)
	assert.NoError(t, err)

	assert.Equal(t, "sess-2", m.sessionState.GetSessionID())
	assert.Equal(t, sess2, state.TeamSession)
	assert.Equal(t, "sess-2", agentteams.GetSessionID(ctx2))
}

// TestBindSession_字符串入参不存session M-29 收窄语义验证：
// 非 *session.AgentTeamSession 输入（如 string sessionID）存 nil（对齐 Python None），
// 但 sessionID 仍正常设置。
// Python: state.team_session = session if isinstance(session, AgentTeamSession) else None
func TestBindSession_字符串入参不存session(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	_, err := m.BindSession(context.Background(), "str-sess-id")

	assert.NoError(t, err)
	assert.Equal(t, "str-sess-id", m.sessionState.GetSessionID())
	// M-29: string 不是 AgentTeamSession → TeamSession 存 nil
	assert.Nil(t, state.TeamSession)
}

// TestReleaseSession_释放 测试 sessionID 清空 + teamSession 置 nil
// Python: SessionManager.release_session()
//   - _reset_session_id_token()  → SessionState.SetSessionID("")
//   - state.team_session = None   → state.TeamSession = nil
func TestReleaseSession_释放(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := newTestAgentTeamSession("sess-1")
	_, _ = m.BindSession(context.Background(), sess)

	m.ReleaseSession(context.Background())

	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)
}

// TestReleaseSession_幂等 测试多次 release 不 panic
// Python: release_session 幂等（对已释放状态再次调用为 no-op）
func TestReleaseSession_幂等(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	m.ReleaseSession(context.Background()) // 未 bind 就 release
	m.ReleaseSession(context.Background()) // 再次 release
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)
}

// TestSessionManager_三态转换 测试 Unbound → Fully-bound → Unbound
// Python: SessionManager 三态模型
//
//	Unbound     — sessionID="" teamSession=nil  从未绑定或显式解绑
//	Fully-bound — sessionID="X" teamSession=ses 活跃会话
//
// 禁止状态：sessionID="" + teamSession!=nil（悬空状态）
func TestSessionManager_三态转换(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	// Unbound 未绑定态：sessionID="" 且 teamSession=nil
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)

	// → 完整绑定（Fully-bound）
	sess := newTestAgentTeamSession("sess-1")
	_, _ = m.BindSession(context.Background(), sess)
	assert.Equal(t, "sess-1", m.sessionState.GetSessionID())
	assert.Equal(t, sess, state.TeamSession)

	// → Unbound (ReleaseSession 清空 sessionID 和 teamSession)
	m.ReleaseSession(context.Background())
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)

	// → 再次完整绑定（Fully-bound again）
	sess2 := newTestAgentTeamSession("sess-2")
	_, _ = m.BindSession(context.Background(), sess2)
	assert.Equal(t, "sess-2", m.sessionState.GetSessionID())
	assert.Equal(t, sess2, state.TeamSession)
}

// TestResumeForNewSession_基本流程 测试返回新 ctx
// Python: SessionManager.resume_for_new_session(session)
// RecoveryManager 为 nil 时跳过 collect/restart，仅执行 BindSession
func TestResumeForNewSession_基本流程(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := newTestAgentTeamSession("resume-sess")
	newCtx, err := m.ResumeForNewSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.Equal(t, "resume-sess", m.sessionState.GetSessionID())
	assert.Equal(t, "resume-sess", agentteams.GetSessionID(newCtx))
}

// TestRecoverForExistingSession_基本流程 测试返回新 ctx
// Python: SessionManager.recover_for_existing_session(session)
// RecoveryManager 为 nil 时跳过 collect/restart，仅执行 BindSession
func TestRecoverForExistingSession_基本流程(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := newTestAgentTeamSession("recover-sess")
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
// Python: session.get_session_id()
func TestExtractSessionID_有GetSessionID(t *testing.T) {
	sess := newTestAgentTeamSession("extracted-123")
	assert.Equal(t, "extracted-123", extractSessionID(sess))
}

// TestExtractSessionID_无GetSessionID 测试不含 GetSessionID 方法的对象
// S-39: string 入参直通返回（kernel.Start 传字符串 sessionID 的场景），
// 非 string 且无 GetSessionID 方法的对象返回空串
func TestExtractSessionID_无GetSessionID(t *testing.T) {
	assert.Equal(t, "not-a-session", extractSessionID("not-a-session"))
	assert.Equal(t, "", extractSessionID(12345))
	assert.Equal(t, "", extractSessionID(nil))
}
