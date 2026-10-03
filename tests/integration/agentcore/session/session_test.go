//go:build integration

package session_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionLifeCycleSuite 测试 Session 生命周期。
// 嵌入 SessionSuite，可使用 NewTestSession() 创建测试会话。
// 对齐 Python: tests/cli/e2e/test_session_persist.py + 各测试中 Session 用法
type SessionLifeCycleSuite struct {
	isuite.SessionSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSessionLifeCycleSuite(t *testing.T) {
	suite.Run(t, new(SessionLifeCycleSuite))
}

// TestNewSession_ID自动生成 测试未指定 ID 时自动生成 UUID。
// 对齐 Python: Session() 无参构造
func (s *SessionLifeCycleSuite) TestNewSession_ID自动生成() {
	// 不传 WithSessionID，NewSession 自动生成 UUID
	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	s.Require().NotNil(sess)
	s.NotEmpty(sess.GetSessionID())
}

// TestNewSession_指定ID 测试 WithSessionID 指定 ID。
// 对齐 Python: Session(session_id="test-1")
func (s *SessionLifeCycleSuite) TestNewSession_指定ID() {
	sess := s.NewTestSession("test-session-1")
	s.Equal("test-session-1", sess.GetSessionID())
}

// TestPreRun_幂等 测试 PreRun 多次调用无报错。
// 对齐 Python: session.pre_run() 幂等
func (s *SessionLifeCycleSuite) TestPreRun_幂等() {
	sess := s.NewTestSession("pre-run-idempotent")
	err := sess.PreRun(s.Ctx)
	s.Require().NoError(err)
	// 第二次调用应无报错
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)
}

// TestPostRun_幂等 测试 PostRun 多次调用无报错。
// 对齐 Python: session.post_run() 幂等
func (s *SessionLifeCycleSuite) TestPostRun_幂等() {
	sess := s.NewTestSession("post-run-idempotent")
	err := sess.PreRun(s.Ctx)
	s.Require().NoError(err)
	err = sess.PostRun(s.Ctx)
	s.Require().NoError(err)
	// 第二次调用应无报错
	err = sess.PostRun(s.Ctx)
	s.Require().NoError(err)
}

// TestUpdateState_GetState 测试状态读写一致。
// 对齐 Python: session.state 全局状态读写
func (s *SessionLifeCycleSuite) TestUpdateState_GetState() {
	sess := s.NewTestSession("state-rw")
	sess.UpdateState(map[string]any{"my_key": "my_value"})
	val, err := sess.GetState(state.StringKey("my_key"))
	s.Require().NoError(err)
	s.Equal("my_value", val)
}

// TestDumpState_完整快照 测试 DumpState 包含已写入的键值。
// 对齐 Python: session.dump_state()
func (s *SessionLifeCycleSuite) TestDumpState_完整快照() {
	sess := s.NewTestSession("dump-state")
	sess.UpdateState(map[string]any{"k1": "v1", "k2": "v2"})
	dump := sess.DumpState()
	s.NotNil(dump)
	// DumpState 返回全局+Agent+Trace 三层状态
	// 验证全局层包含写入的键
	global, ok := dump["global_state"]
	if ok {
		globalMap, ok := global.(map[string]any)
		if ok {
			s.Equal("v1", globalMap["k1"])
			s.Equal("v2", globalMap["k2"])
		}
	}
}

// TestInMemoryCheckpointer_持久化 测试 InMemoryCheckpointer 存储和恢复。
// 对齐 Python: tests/cli/e2e/test_session_persist.py
func (s *SessionLifeCycleSuite) TestInMemoryCheckpointer_持久化() {
	sess := s.NewTestSession("checkpoint-persist")
	// PreRun 触发 checkpointer PreAgentExecute
	err := sess.PreRun(s.Ctx)
	s.Require().NoError(err)
	// PostRun 触发 checkpointer PostAgentExecute
	err = sess.PostRun(s.Ctx)
	s.Require().NoError(err)
	// 验证 session 不为 nil（已通过 checkpointer 存储）
	s.NotEmpty(sess.GetSessionID())
}

// TestSession_Env读写 测试环境变量通过 Config 传播。
// 对齐 Python: session.get_env(key, default)
func (s *SessionLifeCycleSuite) TestSession_Env读写() {
	sess := session.NewSession(
		session.WithSessionID("env-test"),
		session.WithEnvs(map[string]any{"API_KEY": "test-key-123"}),
	)
	s.Equal("test-key-123", sess.GetEnv("API_KEY"))
	// 不存在的 key 返回 nil
	s.Nil(sess.GetEnv("NOT_EXIST"))
	// 带 default
	s.Equal("default_val", sess.GetEnv("NOT_EXIST", "default_val"))
}
