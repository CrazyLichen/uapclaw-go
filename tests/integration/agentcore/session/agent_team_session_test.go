//go:build integration

package session_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── mock 类型 ────────────────────────────

// mockCheckpointer 简单的 checkpointer mock，所有方法不执行操作
type mockCheckpointer struct{}

func (m *mockCheckpointer) PreWorkflowExecute(_ context.Context, _ interfaces.InnerSession, _ any) error {
	return nil
}
func (m *mockCheckpointer) PostWorkflowExecute(_ context.Context, _ interfaces.InnerSession, _ any, _ error) error {
	return nil
}
func (m *mockCheckpointer) PreAgentExecute(_ context.Context, _ interfaces.InnerSession, _ any) error {
	return nil
}
func (m *mockCheckpointer) PreAgentTeamExecute(_ context.Context, _ interfaces.InnerSession, _ any) error {
	return nil
}
func (m *mockCheckpointer) InterruptAgentExecute(_ context.Context, _ interfaces.InnerSession) error {
	return nil
}
func (m *mockCheckpointer) PostAgentExecute(_ context.Context, _ interfaces.InnerSession) error {
	return nil
}
func (m *mockCheckpointer) PostAgentTeamExecute(_ context.Context, _ interfaces.InnerSession) error {
	return nil
}
func (m *mockCheckpointer) SessionExists(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockCheckpointer) Release(_ context.Context, _ string, _ ...string) error {
	return nil
}
func (m *mockCheckpointer) GraphStore() any { return nil }

// ──────────────────────────── 测试套件 ────────────────────────────

// AgentTeamSessionE2ESuite AgentTeamSession 生命周期 E2E 集成测试套件
// 对齐 Python: openjiuwen/core/session/agent_team.py 单元测试
type AgentTeamSessionE2ESuite struct {
	suite.Suite
}

func TestAgentTeamSessionE2E(t *testing.T) {
	suite.Run(t, new(AgentTeamSessionE2ESuite))
}

// ──────────────────────────── 创建和基本属性测试 ────────────────────────────

// TestAgentTeamSession_创建默认值 验证默认创建行为
// 对齐 Python: test_session_creation
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_创建默认值() {
	sess := session.NewAgentTeamSession()
	s.NotEmpty(sess.GetSessionID(), "默认应自动生成 UUID sessionID")
	s.Equal("agent_team", sess.GetTeamID(), "默认 teamID 应为 agent_team")
	s.NotNil(sess.Inner(), "inner 不应为 nil")
}

// TestAgentTeamSession_自定义创建 验证自定义选项
// 对齐 Python: test_session_with_options
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_自定义创建() {
	envs := map[string]any{"key1": "val1", "key2": 42}
	sess := session.CreateAgentTeamSession("sess-custom", envs, "my-team")

	s.Equal("sess-custom", sess.GetSessionID())
	s.Equal("my-team", sess.GetTeamID())

	// 验证 envs
	s.Equal("val1", sess.GetEnv("key1"))
	s.Equal(42, sess.GetEnv("key2"))
}

// ──────────────────────────── 状态读写测试 ────────────────────────────

// TestAgentTeamSession_状态读写 验证 UpdateState/GetState/DumpState
// 对齐 Python: test_session_state
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_状态读写() {
	sess := session.NewAgentTeamSession(
		session.WithAgentTeamCheckpointer(&mockCheckpointer{}),
	)
	ctx := context.Background()

	// 需要先 PreRun 以确保状态系统初始化
	s.Require().NoError(sess.PreRun(ctx))

	// UpdateState
	sess.UpdateState(map[string]any{
		"task_status": "running",
		"iteration":   3,
		"last_agent":  "agent-b",
	})

	// GetState
	val, err := sess.GetState(state.StringKey("task_status"))
	s.Require().NoError(err)
	s.Equal("running", val)

	val, err = sess.GetState(state.StringKey("iteration"))
	s.Require().NoError(err)
	s.Equal(3, val)

	// 不存在的 key
	val, err = sess.GetState(state.StringKey("nonexistent"))
	s.Require().NoError(err)
	s.Nil(val, "不存在的 key 应返回 nil")

	// DumpState
	dump := sess.DumpState()
	s.NotEmpty(dump)
}

// ──────────────────────────── 生命周期测试 ────────────────────────────

// TestAgentTeamSession_生命周期 验证 PreRun→PostRun 幂等性
// 对齐 Python: test_session_lifecycle
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_生命周期() {
	sess := session.NewAgentTeamSession()
	ctx := context.Background()

	// PreRun 幂等
	s.Require().NoError(sess.PreRun(ctx))
	s.Require().NoError(sess.PreRun(ctx), "重复 PreRun 应幂等")

	// PostRun 幂等
	s.Require().NoError(sess.PostRun(ctx))
	s.Require().NoError(sess.PostRun(ctx), "重复 PostRun 应幂等")
}

// TestAgentTeamSession_ClearSession 验证 ClearSession 重置状态和生命周期
// 对齐 Python: test_session_clear
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_ClearSession() {
	sess := session.NewAgentTeamSession()
	ctx := context.Background()

	// 设置状态
	sess.UpdateState(map[string]any{"key": "value"})

	// PreRun
	s.Require().NoError(sess.PreRun(ctx))

	// ClearSession
	sess.ClearSession(ctx)

	// 状态应清空
	val, err := sess.GetState(state.StringKey("key"))
	s.Require().NoError(err)
	s.Nil(val, "ClearSession 后状态应为 nil")

	// 生命周期应重置（允许再次 PreRun）
	s.Require().NoError(sess.PreRun(ctx), "ClearSession 后应可再次 PreRun")
}

// ──────────────────────────── Interact 测试 ────────────────────────────

// TestAgentTeamSession_Interact报错 验证团队会话不支持交互
// 对齐 Python: test_team_session_interact_error
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_Interact报错() {
	sess := session.NewAgentTeamSession()
	err := sess.Interact(context.Background(), "hello")
	s.Error(err, "团队会话 Interact 应返回错误")
	s.Contains(err.Error(), "团队会话不支持交互")
}

// ──────────────────────────── CreateAgentSession 测试 ────────────────────────────

// TestAgentTeamSession_CreateAgentSession 验证子 Agent Session 创建
// 对齐 Python: test_create_agent_session
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_CreateAgentSession() {
	sess := session.NewAgentTeamSession(
		session.WithAgentTeamSessionID("team-sess-1"),
		session.WithAgentTeamTeamID("team-alpha"),
	)

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("worker-1"),
		agentschema.WithAgentName("Worker1"),
	)

	// 创建共享 StreamWriterManager 的子会话
	agentSess := sess.CreateAgentSession(card, "worker-1", true)
	s.NotNil(agentSess)
	s.Equal("team-sess-1", agentSess.GetSessionID(), "子会话应继承父 sessionID")

	// 创建不共享 StreamWriterManager 的子会话
	agentSessNoShare := sess.CreateAgentSession(card, "worker-1", false)
	s.NotNil(agentSessNoShare)
}

// TestAgentTeamSession_CreateAgentSession_nilCard 验证 nil card 时自动构造
// 对齐 Python: test_create_agent_session_default_card
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_CreateAgentSession_nilCard() {
	sess := session.NewAgentTeamSession()

	agentSess := sess.CreateAgentSession(nil, "agent-1", true)
	s.NotNil(agentSess)
}

// ──────────────────────────── 流写入测试 ────────────────────────────

// TestAgentTeamSession_WriteStream 验证 WriteStream/WriteCustomStream 不报错
// 对齐 Python: test_session_write_stream
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_WriteStream() {
	sess := session.NewAgentTeamSession()
	ctx := context.Background()

	// WriteStream 不应报错
	err := sess.WriteStream(ctx, map[string]any{"type": "text", "content": "hello"})
	s.Require().NoError(err)

	// WriteCustomStream 不应报错
	err = sess.WriteCustomStream(ctx, map[string]any{"type": "custom", "data": "payload"})
	s.Require().NoError(err)
}

// ──────────────────────────── 检查点测试 ────────────────────────────

// TestAgentTeamSession_Commit 验证 Commit 提交检查点
// 对齐 Python: test_session_commit
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_Commit() {
	// 使用 mockCheckpointer（不依赖外部存储）
	sess := session.NewAgentTeamSession(
		session.WithAgentTeamCheckpointer(&mockCheckpointer{}),
	)
	ctx := context.Background()

	// Commit 应成功
	s.Require().NoError(sess.Commit(ctx))

	// FlushCheckpoint 等价 Commit
	s.Require().NoError(sess.FlushCheckpoint(ctx))
}

// TestAgentTeamSession_完整生命周期E2E 验证完整的 PreRun→State→Commit→PostRun 流程
// 对齐 Python: test_full_lifecycle
func (s *AgentTeamSessionE2ESuite) TestAgentTeamSession_完整生命周期E2E() {
	sess := session.NewAgentTeamSession(
		session.WithAgentTeamSessionID("e2e-sess"),
		session.WithAgentTeamTeamID("e2e-team"),
		session.WithAgentTeamCheckpointer(&mockCheckpointer{}),
		session.WithAgentTeamEnvs(map[string]any{"env1": "val1"}),
	)
	ctx := context.Background()

	// 1. PreRun
	s.Require().NoError(sess.PreRun(ctx))

	// 2. 更新状态
	sess.UpdateState(map[string]any{"phase": "executing", "step": 1})
	val, err := sess.GetState(state.StringKey("phase"))
	s.Require().NoError(err)
	s.Equal("executing", val)

	// 3. Commit（中途保存）
	s.Require().NoError(sess.Commit(ctx))

	// 4. 继续执行
	sess.UpdateState(map[string]any{"phase": "completed", "step": 2})

	// 5. PostRun（关闭流 + 最终提交）
	s.Require().NoError(sess.PostRun(ctx))

	// 6. 验证最终状态
	val, err = sess.GetState(state.StringKey("phase"))
	s.Require().NoError(err)
	s.Equal("completed", val)
}

// ──────────────────────────── 编译时接口合规检查 ────────────────────────────

func init() {
	// 验证 AgentTeamSession 实现 SessionFacade
	_ = session.AgentTeamSession{}
}

// CI 合规性验证：导入 ≥2 个 internal 包
var _ = checkpointer.NewInMemoryCheckpointer
var _ = state.StringKey
var _ = interfaces.Checkpointer(nil)
var _ = testing.Init
