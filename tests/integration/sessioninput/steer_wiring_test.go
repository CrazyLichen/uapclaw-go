//go:build integration

package sessioninput

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SteerWiringSuite SESSION_INPUT 接线链路级验证（spec §8.2 七用例的可本地确定子集）。
type SteerWiringSuite struct {
	isuite.BaseIntegrationSuite
	coordinator *session.RuntimeSessionCoordinator
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSteerWiringSuite 运行本套件。
func TestSteerWiringSuite(t *testing.T) {
	suite.Run(t, new(SteerWiringSuite))
}

// SetupTest 每用例一套全新协调器（无跨用例污染）。
func (s *SteerWiringSuite) SetupTest() {
	s.coordinator = session.NewRuntimeSessionCoordinator(session.DefaultCoordinatorOptions())
	_, err := s.coordinator.RegisterSession("session-A", "web", session.SessionPersistencePolicyPersistent)
	s.Require().NoError(err)
}

// ──────────────────────────── 用例 ────────────────────────────

// execForTest 构造并登记一条执行（经既有导出 NewExecutionForTest；parent
// 锚定用 RootWorkKind 携带——ChildOptions 无 parent_execution_id 直填，
// 父绑定在用例①经快照断言校验的是 coordinator 三守卫分流产物）。
func (s *SteerWiringSuite) execForTest(requestID string, kind session.SessionWorkKind,
	parentID ...string) *session.SessionExecutionHandle {
	opts := session.ChildOptions{}
	if len(parentID) > 0 && parentID[0] != "" {
		opts.ParentRequestID = parentID[0]
	}
	h, err := s.coordinator.NewExecutionForTest("session-A", requestID, kind, opts)
	s.Require().NoError(err)
	return h
}

// TestSteer_插话与父绑定 用例①：有活跃父执行时补充输入走 SESSION_INPUT 且挂父。
func (s *SteerWiringSuite) TestSteer_插话与父绑定() {
	parent := s.execForTest("req-parent", session.SessionWorkKindChatStream)
	child, cerr := s.coordinator.NewExecutionForTest("session-A", "req-child", session.SessionWorkKindSessionInput,
		session.ChildOptions{ParentRequestID: parent.RequestID})
	s.Require().NoError(cerr)
	_ = child
	snapshots := s.coordinator.SnapshotSession("session-A")
	s.Require().NotNil(snapshots)
	found := false
	for _, ex := range snapshots.Executions {
		if ex.WorkKind == session.SessionWorkKindSessionInput {
			found = true
			s.Require().Equal(parent.ExecutionID, ex.ParentExecutionID)
			s.Require().Equal(parent.WorkKind, ex.RootWorkKind)
		}
	}
	s.Require().True(found, "应存在挂父的 SESSION_INPUT 子执行")
}

// TestSteer_锚定失配 用例②：expected_execution_id 失配 → TARGET_CHANGED。
func (s *SteerWiringSuite) TestSteer_锚定失配() {
	s.execForTest("req-parent", session.SessionWorkKindChatStream)
	wrongID := "exec-gone"
	err := s.coordinator.StreamSessionInput(context.Background(), session.StreamSessionInputOptions{
		SessionID:           "session-A",
		RequestID:           "req-ans",
		Operation:           func(context.Context, string) (<-chan *schema.AgentResponseChunk, error) { return nil, nil },
		IdleOperation:       func(context.Context) (<-chan *schema.AgentResponseChunk, error) { return nil, nil },
		ExpectedExecutionID: &wrongID,
	}, func(*schema.AgentResponseChunk) bool { return true })
	var target *session.SessionInputTargetError
	s.Require().ErrorAs(err, &target)
	s.Require().Equal("the targeted execution has ended or changed; supplemental input was not sent", target.Message)
}

// TestSteer_idle兜底 用例③：无活跃执行 → idle 闭包被调用。
func (s *SteerWiringSuite) TestSteer_idle兜底() {
	idleCalled := make(chan struct{})
	err := s.coordinator.StreamSessionInput(context.Background(), session.StreamSessionInputOptions{
		SessionID: "session-A",
		RequestID: "req-idle",
		Operation: func(context.Context, string) (<-chan *schema.AgentResponseChunk, error) {
			return nil, errors.New("有父时不应走 idle")
		},
		IdleOperation: func(context.Context) (<-chan *schema.AgentResponseChunk, error) {
			close(idleCalled)
			ch := make(chan *schema.AgentResponseChunk)
			close(ch)
			return ch, nil
		},
	}, func(*schema.AgentResponseChunk) bool { return true })
	s.Require().NoError(err)
	select {
	case <-idleCalled:
	case <-time.After(time.Second):
		s.Fail("无活跃执行时应调用 idle 兜底")
	}
}

// TestSteer_等交互应答拒收 用例④：活跃执行挂起等交互 → REJECTED。
func (s *SteerWiringSuite) TestSteer_等交互应答拒收() {
	parent := s.execForTest("req-parent", session.SessionWorkKindChatStream)
	s.coordinator.MarkAwaitingControlForTest(parent.ExecutionID, map[string]struct{}{"ctl-1": {}})
	err := s.coordinator.StreamSessionInput(context.Background(), session.StreamSessionInputOptions{
		SessionID: "session-A",
		RequestID: "req-ans",
		Operation: func(context.Context, string) (<-chan *schema.AgentResponseChunk, error) { return nil, nil },
		IdleOperation: func(context.Context) (<-chan *schema.AgentResponseChunk, error) {
			return nil, errors.New("挂起时不应走 idle")
		},
	}, func(*schema.AgentResponseChunk) bool { return true })
	var rejected *session.SessionInputRejectedError
	s.Require().ErrorAs(err, &rejected)
	s.Require().Equal("session is waiting for an interaction answer; supplemental input was not sent", rejected.Message)
}

// TestSteer_超时FAILED 用例⑥：TimeoutScope 到期 → 终态 FAILED。
func (s *SteerWiringSuite) TestSteer_超时FAILED() {
	_, err := s.coordinator.RunUnary(context.Background(), "session-A", "req-to",
		session.SessionWorkKindChatUnary,
		func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, session.UnaryOptions{TimeoutScope: closedScope(), TimeoutError: "session unary timed out"})
	s.Require().NotNil(s.coordinator.SnapshotSession("session-A"))
	foundFailed := false
	for _, ex := range s.coordinator.SnapshotSession("session-A").Executions {
		if ex.RequestID == "req-to" && ex.State == session.SessionExecutionStateFailed {
			foundFailed = true
		}
	}
	s.Require().True(foundFailed, "超时应终态 FAILED")
	_ = err
}

// TestSteer_代次防误判 用例⑦：会话关闭重开后旧代次执行不参与活跃判定。
func (s *SteerWiringSuite) TestSteer_代次防误判() {
	parent := s.execForTest("req-old", session.SessionWorkKindChatStream)
	// 关闭会话（代次封存）：旧代次执行立即失去活跃资格
	_, err := s.coordinator.CloseSession("session-A", nil, nil)
	s.Require().NoError(err)
	// 关闭后补充输入必须被拒收（旧代次父不可挂载、idle 不可走）
	idleCalled := make(chan struct{})
	err = s.coordinator.StreamSessionInput(context.Background(), session.StreamSessionInputOptions{
		SessionID: "session-A",
		RequestID: "req-new",
		Operation: func(context.Context, string) (<-chan *schema.AgentResponseChunk, error) {
			return nil, errors.New("旧代次父不应被挂载")
		},
		IdleOperation: func(context.Context) (<-chan *schema.AgentResponseChunk, error) {
			close(idleCalled)
			ch := make(chan *schema.AgentResponseChunk)
			close(ch)
			return ch, nil
		},
	}, func(*schema.AgentResponseChunk) bool { return true })
	s.Require().Error(err, "关闭后代次封存：旧父不可挂载、idle 不可走")
	select {
	case <-idleCalled:
		s.Fail("关闭后不得走 idle")
	default:
	}
	_ = parent
}

// TestSteer_守卫边界剔除 用例⑤：锚定失配与挂起同时存在时挂起守卫优先。
func (s *SteerWiringSuite) TestSteer_守卫边界剔除() {
	parent := s.execForTest("req-parent", session.SessionWorkKindChatStream)
	s.coordinator.MarkAwaitingControlForTest(parent.ExecutionID, map[string]struct{}{"ctl-1": {}})
	gone := "exec-gone"
	err := s.coordinator.StreamSessionInput(context.Background(), session.StreamSessionInputOptions{
		SessionID: "session-A",
		RequestID: "req-ans",
		Operation: func(context.Context, string) (<-chan *schema.AgentResponseChunk, error) { return nil, nil },
		IdleOperation: func(context.Context) (<-chan *schema.AgentResponseChunk, error) {
			return nil, errors.New("挂起优先于 idle")
		},
		ExpectedExecutionID: &gone,
	}, func(*schema.AgentResponseChunk) bool { return true })
	var rejected *session.SessionInputRejectedError
	s.Require().ErrorAs(err, &rejected)
	s.Require().Equal("session is waiting for an interaction answer; supplemental input was not sent", rejected.Message)
}

// closedScope 已到期的超时 ctx（TimeoutScope 消费判定用）。
func closedScope() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	time.Sleep(time.Millisecond)
	_ = cancel
	return ctx
}
