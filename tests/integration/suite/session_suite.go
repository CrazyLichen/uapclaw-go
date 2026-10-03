//go:build integration

package suite

import (
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionSuite 包含 Session 生命周期的集成测试套件。
// 等价于 Python conftest.py 中的 session fixture。
//
// 提供：
//   - RunnerSuite 全部能力
//   - SessionFacade 实例
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
	// Session 在具体测试用例中按需创建，
	// 因为不同测试可能需要不同的 Session 配置。
}

// TearDownSuite 清理 Session 测试环境。
func (s *SessionSuite) TearDownSuite() {
	if s.Session != nil {
		// Session 清理逻辑（如有 Close 方法）
	}
	s.RunnerSuite.TearDownSuite()
}

// SetSession 设置测试用 Session 实例。
// 供具体测试用例在 SetupTest / 测试函数中调用。
func (s *SessionSuite) SetSession(session sessioninterfaces.SessionFacade) {
	s.Session = session
}
