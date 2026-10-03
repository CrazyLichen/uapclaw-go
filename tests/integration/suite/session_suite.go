//go:build integration

package suite

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionSuite 包含 Session 生命周期的集成测试套件。
// 等价于 Python conftest.py 中的 session fixture。
//
// 提供：
//   - RunnerSuite 全部能力
//   - SessionFacade 实例
//   - NewTestSession() 辅助方法（按需创建测试会话）
//
// Session 在具体测试用例中按需创建，因为不同测试可能需要不同的 Session 配置。
type SessionSuite struct {
	RunnerSuite
	// Session 测试用会话实例
	Session sessioninterfaces.SessionFacade
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化 Session 测试环境。
func (s *SessionSuite) SetupSuite() {
	s.RunnerSuite.SetupSuite()
}

// TearDownSuite 清理 Session 测试环境。
func (s *SessionSuite) TearDownSuite() {
	s.RunnerSuite.TearDownSuite()
}

// SetSession 设置测试用 Session 实例。
// 供具体测试用例在 SetupTest / 测试函数中调用。
func (s *SessionSuite) SetSession(session sessioninterfaces.SessionFacade) {
	s.Session = session
}

// NewTestSession 创建测试用 Session 实例。
// 使用 InMemoryCheckpointer，对齐 Python: Session(session_id=..., envs=...)
// 返回具体类型 *session.Session，以便测试调用 PreRun/PostRun/Commit 等方法。
func (s *SessionSuite) NewTestSession(sessionID string) *session.Session {
	return session.NewSession(
		session.WithSessionID(sessionID),
		session.WithEnvs(map[string]any{}),
	)
}
