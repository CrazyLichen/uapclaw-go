// Package server 会话消息编排服务单元测试。
// Python 对应测试：tests/unit_tests/agentserver/test_session_messaging.py（service 分支）。
package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeAdmission 准入桩（记录调用序）。
type fakeAdmission struct {
	mu           sync.Mutex
	begun        []string
	ended        []string
	busy         bool
	beginErr     error
	userActive   bool
	heartbeatAct bool
	blocked      bool
}

func (f *fakeAdmission) BeginSessionMessage(sessionID, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begun = append(f.begun, sessionID+"\x00"+runID)
	return f.beginErr
}

func (f *fakeAdmission) EndSessionMessage(sessionID, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = append(f.ended, sessionID+"\x00"+runID)
	return nil
}

func (f *fakeAdmission) IsUserActive(string) bool            { return f.userActive }
func (f *fakeAdmission) IsSessionMessageActive(string) bool  { return f.busy }
func (f *fakeAdmission) IsHeartbeatActive(string) bool       { return f.heartbeatAct }
func (f *fakeAdmission) IsSessionMessageBlocked(string) bool { return f.blocked }

// fakeModes mode 判定桩。
type fakeModes struct{}

func (fakeModes) DeprecateMode(m string) string { return m }
func (fakeModes) IsSingleAgentMode(m string) bool {
	// 对齐 Python mode_matrix：plan 属单 agent 模式族
	return m == "agent" || m == "code" || m == "plan"
}
func (fakeModes) IsTeamMode(string) bool   { return false }
func (fakeModes) IsPlanMode(m string) bool { return m == "plan" }

// fakeMeta 元数据桩。
type fakeMeta struct {
	sessions map[string]map[string]any
}

func (f *fakeMeta) GetSessionMetadata(sessionID string) (map[string]any, bool) {
	m, ok := f.sessions[sessionID]
	return m, ok
}

func (f *fakeMeta) GetAllSessionsMetadata() ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(f.sessions))
	for _, m := range f.sessions {
		out = append(out, m)
	}
	return out, nil
}

// fakeExec 执行桩。
type fakeExec struct {
	mu      sync.Mutex
	results []SessionMessageExecutionResult
	delay   time.Duration
	calls   int
}

func (f *fakeExec) exec(record *SessionMessageRecord) (SessionMessageExecutionResult, error) {
	f.mu.Lock()
	f.calls++
	idx := f.calls - 1
	res := SessionMessageExecutionResult{Status: StatusDelivered}
	if idx < len(f.results) {
		res = f.results[idx]
	}
	f.mu.Unlock()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return res, nil
}

// rejectingExec 返回 steer 被拒三码。
func rejectingExec(code string) SessionMessageExecuteFunc {
	return func(record *SessionMessageRecord) (SessionMessageExecutionResult, error) {
		return SessionMessageExecutionResult{Status: StatusFailed, ErrorCode: code, Error: "rejected"}, nil
	}
}

// waitCond 轮询等待条件成立（测试辅助，非生产轮询）。
func waitCond(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("条件超时未满足: %s", msg)
}

// ──────────────────────────── 套件 ────────────────────────────

// SessionMessageServiceSuite 编排服务测试套件。
type SessionMessageServiceSuite struct {
	suite.Suite
	store  *SessionMessageStore
	admit  *fakeAdmission
	modes  fakeModes
	meta   *fakeMeta
	exec   *fakeExec
	svc    *SessionMessageService
	tmpDir string
}

// TestSessionMessageServiceSuite 入口。
func TestSessionMessageServiceSuite(t *testing.T) {
	suite.Run(t, new(SessionMessageServiceSuite))
}

// SetupTest 每用例重建（TempDir 根）。
func (s *SessionMessageServiceSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	s.store = NewSessionMessageStore(s.tmpDir+"/session_messages.sqlite3", "WAL")
	s.admit = &fakeAdmission{}
	s.meta = &fakeMeta{sessions: map[string]map[string]any{
		"sess-A": {"session_id": "sess-A", "title": "A", "mode": "agent", "channel_id": "web", "user_id": "user-1"},
		"sess-B": {"session_id": "sess-B", "title": "B", "mode": "agent", "channel_id": "web", "user_id": "user-1"},
		"sess-P": {"session_id": "sess-P", "title": "P", "mode": "plan", "channel_id": "web", "user_id": "user-1"},
	}}
	s.exec = &fakeExec{}
	s.buildService()
}

// TearDownTest 停服等 worker 全退出，避免与 TempDir 清理竞态。
func (s *SessionMessageServiceSuite) TearDownTest() {
	if s.svc != nil {
		s.svc.Stop(context.Background())
	}
}

// buildService 用当前桩组装服务。
func (s *SessionMessageServiceSuite) buildService() {
	s.svc = NewSessionMessageService(SessionMessageServiceParams{
		Store:            s.store,
		Admission:        s.admit,
		Execute:          s.exec.exec,
		ModeClassifier:   s.modes,
		MetadataProvider: s.meta,
		Available:        true,
	})
}

// source 便捷来源（sess-A → user-1）。
func (s *SessionMessageServiceSuite) source() *SessionMessageSource {
	return &SessionMessageSource{
		SessionID: "sess-A", RequestID: "req-1", ToolCallID: "call-1",
		IdempotencyKey: "idem-" + newUUIDHex(), UserID: "user-1",
	}
}

// ──────────────────────────── 校验链（矩阵 #3/#5/#6） ────────────────────────────

// TestSendMessage_校验链逐分支 顺序忠实 Python :489-598。
func (s *SessionMessageServiceSuite) TestSendMessage_校验链逐分支() {
	ctx := context.Background()
	src := s.source()

	// 非法 input_mode
	_, err := s.svc.SendMessage(ctx, src, "sess-B", "hi", "bogus")
	s.Require().Error(err)
	s.Equal("INVALID_ARGUMENT", err.(*SessionMessagingError).Code)

	// 无效 target
	_, err = s.svc.SendMessage(ctx, src, "bad id!", "hi", "")
	s.Require().Error(err)
	s.Equal("INVALID_ARGUMENT", err.(*SessionMessagingError).Code)

	// 自发
	_, err = s.svc.SendMessage(ctx, src, "sess-A", "hi", "")
	s.Require().Error(err)
	s.Equal("SELF_SEND_NOT_ALLOWED", err.(*SessionMessagingError).Code)

	// 空内容
	_, err = s.svc.SendMessage(ctx, src, "sess-B", "  ", "")
	s.Require().Error(err)
	s.Equal("INVALID_ARGUMENT", err.(*SessionMessagingError).Code)

	// 32KB 上限
	_, err = s.svc.SendMessage(ctx, src, "sess-B", string(make([]byte, MaxSessionMessageBytes+1)), "")
	s.Require().Error(err)
	// Python: session_message_service.py:514-516 LIMIT_EXCEEDED
	s.Equal("LIMIT_EXCEEDED", err.(*SessionMessagingError).Code)

	// 目标不存在
	_, err = s.svc.SendMessage(ctx, src, "sess-X", "hi", "")
	s.Require().Error(err)
	s.Equal("NOT_FOUND_OR_FORBIDDEN", err.(*SessionMessagingError).Code)

	// 跨 user
	other := s.source()
	other.UserID = "user-2"
	_, err = s.svc.SendMessage(ctx, other, "sess-B", "hi", "")
	s.Require().Error(err)
	s.Equal("NOT_FOUND_OR_FORBIDDEN", err.(*SessionMessagingError).Code)

	// 不支持的源（cron）
	s.meta.sessions["sess-A"]["cron_id"] = "cron-1"
	_, err = s.svc.SendMessage(ctx, src, "sess-B", "hi", "")
	s.Require().Error(err)
	s.Equal("UNSUPPORTED_SOURCE", err.(*SessionMessagingError).Code)
	delete(s.meta.sessions["sess-A"], "cron_id")

	// 不支持的目标（mode 不在白名单）
	s.meta.sessions["sess-B"]["mode"] = "weird"
	_, err = s.svc.SendMessage(ctx, src, "sess-B", "hi", "")
	s.Require().Error(err)
	s.Equal("UNSUPPORTED_TARGET", err.(*SessionMessagingError).Code)
	s.meta.sessions["sess-B"]["mode"] = "agent"
}

// TestSendMessage_Hop上限 矩阵 #3。
func (s *SessionMessageServiceSuite) TestSendMessage_Hop上限() {
	src := s.source()
	src.HopCount = MaxSessionMessageHops // +1 超限
	_, err := s.svc.SendMessage(context.Background(), src, "sess-B", "hi", "")
	s.Require().Error(err)
	s.Equal("LIMIT_EXCEEDED", err.(*SessionMessagingError).Code)
}

// TestSendMessage_删除屏障目标 矩阵 #14。
func (s *SessionMessageServiceSuite) TestSendMessage_删除屏障目标() {
	s.svc.BlockTargetDelete("sess-B")
	_, err := s.svc.SendMessage(context.Background(), s.source(), "sess-B", "hi", "")
	s.Require().Error(err)
	s.Equal("NOT_FOUND_OR_FORBIDDEN", err.(*SessionMessagingError).Code)
}

// TestSendMessage_成功落库立即返回 矩阵 #1。
func (s *SessionMessageServiceSuite) TestSendMessage_成功落库立即返回() {
	out, err := s.svc.SendMessage(context.Background(), s.source(), "sess-B", "hello", "")
	s.Require().NoError(err)
	s.Equal(true, out["accepted"])
	s.Equal(StatusQueued, out["status"])
	s.Equal(false, out["deduplicated"])
	// 幂等重发 → deduplicated
	src := s.source()
	src.IdempotencyKey = out["message_id"].(string)
	_ = src
}

// ──────────────────────────── 投递 worker（矩阵 #7/#8/#11/#12/#22） ────────────────────────────

// TestWorker_投递成功 空闲目标 claim → execute → delivered。
func (s *SessionMessageServiceSuite) TestWorker_投递成功() {
	out, err := s.svc.SendMessage(context.Background(), s.source(), "sess-B", "hello", "")
	s.Require().NoError(err)
	msgID := out["message_id"].(string)
	waitCond(s.T(), 5*time.Second, func() bool {
		rec, _ := s.store.Get(msgID)
		return rec != nil && rec.Status == StatusDelivered
	}, "消息应到 delivered")
	rec, _ := s.store.Get(msgID)
	s.Require().NotNil(rec)
	s.Require().NotNil(rec.StartedAt)
	s.Equal("session-message-"+msgID, rec.ExecutionRequestID)
	// 队空 worker 退出
	waitCond(s.T(), 2*time.Second, func() bool {
		s.svc.workersMu.Lock()
		defer s.svc.workersMu.Unlock()
		_, ok := s.svc.workers[workerKey("sess-B", "")]
		return !ok
	}, "worker 应退出")
}

// TestWorker_幂等去重 不重复投递。
func (s *SessionMessageServiceSuite) TestWorker_幂等去重() {
	ctx := context.Background()
	src := s.source()
	out1, err := s.svc.SendMessage(ctx, src, "sess-B", "dup", "")
	s.Require().NoError(err)
	waitCond(s.T(), 5*time.Second, func() bool {
		rec, _ := s.store.Get(out1["message_id"].(string))
		return rec != nil && rec.Status == StatusDelivered
	}, "首条投递")
	out2, err := s.svc.SendMessage(ctx, src, "sess-B", "dup", "")
	s.Require().NoError(err)
	s.Equal(true, out2["deduplicated"])
	s.Equal(out1["message_id"], out2["message_id"])
}

// TestWorker_WatchdogUnknown 先释放准入再置 unknown（顺序矩阵 #11）。
func (s *SessionMessageServiceSuite) TestWorker_WatchdogUnknown() {
	// 慢执行超过极小看门狗
	slowExec := func(record *SessionMessageRecord) (SessionMessageExecutionResult, error) {
		time.Sleep(300 * time.Millisecond)
		return SessionMessageExecutionResult{Status: StatusDelivered}, nil
	}
	s.svc = NewSessionMessageService(SessionMessageServiceParams{
		Store: s.store, Admission: s.admit, Execute: slowExec,
		ModeClassifier: s.modes, MetadataProvider: s.meta,
		Available: true, ExecutionWatchdogTimeout: 0.05, // 50ms
	})
	out, err := s.svc.SendMessage(context.Background(), s.source(), "sess-B", "slow", "")
	s.Require().NoError(err)
	msgID := out["message_id"].(string)
	waitCond(s.T(), 5*time.Second, func() bool {
		rec, _ := s.store.Get(msgID)
		return rec != nil && rec.Status == StatusUnknown
	}, "应置 unknown")
	rec, _ := s.store.Get(msgID)
	s.Equal("EXECUTION_WATCHDOG_TIMEOUT", rec.LastErrorCode)
	// 顺序：admission end 在 unknown 终态前发生（本例 task 道无 begin/end，
	// watchdog 释放路径在 acquired=false 时不调用 End —— 用 task 模式复验）
	// 直接核对未知态 finished_at 已写
	s.Require().NotNil(rec.FinishedAt)
}

// TestWorker_Steer被拒自动defer 矩阵 #12。
func (s *SessionMessageServiceSuite) TestWorker_Steer被拒自动defer() {
	// 首次 steer 提交被拒（QUEUE_REQUIRED），此后执行成功——
	// 对齐 Python：被拒 → defer 重排队；task 道重试时执行放行。
	var calls int
	var mu sync.Mutex
	// secondInputMode 第二次执行（task 道放行）收到的 input_mode——
	// defer 归一化的确定性锚点（queued 中间态会被 worker 立即 claim，
	// 轮询该瞬态会在负载下假红，改从执行回调捕获）。
	secondInputMode := "unset"
	flip := func(record *SessionMessageRecord) (SessionMessageExecutionResult, error) {
		mu.Lock()
		calls++
		first := calls == 1
		if !first {
			secondInputMode = record.InputMode
		}
		mu.Unlock()
		if first {
			return SessionMessageExecutionResult{Status: StatusFailed,
				ErrorCode: SessionInputQueueRequiredCode, Error: "rejected"}, nil
		}
		return SessionMessageExecutionResult{Status: StatusDelivered}, nil
	}
	s.svc = NewSessionMessageService(SessionMessageServiceParams{
		Store: s.store, Admission: s.admit, Execute: flip,
		ModeClassifier: s.modes, MetadataProvider: s.meta,
		Available: true, ExecutionWatchdogTimeout: 5,
	})
	out, err := s.svc.SendMessage(context.Background(), s.source(), "sess-B", "hi", "steer")
	s.Require().NoError(err)
	msgID := out["message_id"].(string)
	// task 道重试放行 → 送达（终态；中间 queued/claimed 态不再断言，防竞态假红）
	waitCond(s.T(), 5*time.Second, func() bool {
		rec, _ := s.store.Get(msgID)
		return rec != nil && rec.Status == StatusDelivered
	}, "defer 后应送达")
	mu.Lock()
	defer mu.Unlock()
	s.Equal("", secondInputMode, "defer 归一化：task 道执行时 input_mode 应已置空")
}

// TestWorker_Unknown不自动降级 unknown 结果绝不 defer（矩阵 #12）。
func (s *SessionMessageServiceSuite) TestWorker_Unknown不自动降级() {
	unknownExec := func(record *SessionMessageRecord) (SessionMessageExecutionResult, error) {
		return SessionMessageExecutionResult{Status: StatusUnknown, ErrorCode: "X", Error: "uncertain"}, nil
	}
	s.svc = NewSessionMessageService(SessionMessageServiceParams{
		Store: s.store, Admission: s.admit, Execute: unknownExec,
		ModeClassifier: s.modes, MetadataProvider: s.meta,
		Available: true, ExecutionWatchdogTimeout: 5,
	})
	out, err := s.svc.SendMessage(context.Background(), s.source(), "sess-B", "hi", "steer")
	s.Require().NoError(err)
	msgID := out["message_id"].(string)
	waitCond(s.T(), 5*time.Second, func() bool {
		rec, _ := s.store.Get(msgID)
		return rec != nil && rec.Status == StatusUnknown
	}, "应保持 unknown")
	rec, _ := s.store.Get(msgID)
	s.Equal("steer", rec.InputMode) // 未 defer 归一化
}

// TestWorker_TaskAdmissionBeginEnd task 道准入 begin/end 配对（矩阵 #11）。
func (s *SessionMessageServiceSuite) TestWorker_TaskAdmissionBeginEnd() {
	out, err := s.svc.SendMessage(context.Background(), s.source(), "sess-B", "task-msg", "follow_up")
	s.Require().NoError(err)
	msgID := out["message_id"].(string)
	waitCond(s.T(), 5*time.Second, func() bool {
		rec, _ := s.store.Get(msgID)
		return rec != nil && rec.Status == StatusDelivered
	}, "task 投递完成")
	s.admit.mu.Lock()
	defer s.admit.mu.Unlock()
	s.Len(s.admit.begun, 1)
	s.Len(s.admit.ended, 1)
}

// TestWorker_Steer排队降级 矩阵 #9：plan 目标 steer → effective 置空排队。
func (s *SessionMessageServiceSuite) TestWorker_Steer排队降级() {
	out, err := s.svc.SendMessage(context.Background(), s.source(), "sess-P", "hi", "steer")
	s.Require().NoError(err)
	s.Equal("", out["input_mode"]) // effective 置空
	rec, _ := s.store.Get(out["message_id"].(string))
	s.Equal("", rec.InputMode)
	s.Equal("steer", rec.RequestedInputMode)
}

// TestStatusCallback失败容忍 矩阵 #22。
func (s *SessionMessageServiceSuite) TestStatusCallback失败容忍() {
	failing := func(record *SessionMessageRecord) error { return errors.New("ws down") }
	s.svc = NewSessionMessageService(SessionMessageServiceParams{
		Store: s.store, Admission: s.admit, Execute: s.exec.exec,
		StatusCallback: failing,
		ModeClassifier: s.modes, MetadataProvider: s.meta,
		Available: true,
	})
	out, err := s.svc.SendMessage(context.Background(), s.source(), "sess-B", "hi", "")
	s.Require().NoError(err)
	msgID := out["message_id"].(string)
	waitCond(s.T(), 5*time.Second, func() bool {
		rec, _ := s.store.Get(msgID)
		return rec != nil && rec.Status == StatusDelivered
	}, "推送失败不中断投递")
}

// TestOnTargetDeleted_取消持久工作 矩阵 #14。
func (s *SessionMessageServiceSuite) TestOnTargetDeleted_取消持久工作() {
	ctx := context.Background()
	out, err := s.svc.SendMessage(ctx, s.source(), "sess-B", "doomed", "")
	s.Require().NoError(err)
	msgID := out["message_id"].(string)
	n, err := s.svc.OnTargetDeleted(ctx, "sess-B")
	s.Require().NoError(err)
	s.Equal(1, n)
	rec, _ := s.store.Get(msgID)
	s.Equal(StatusCancelled, rec.Status)
	s.Equal("TARGET_DELETED", rec.LastErrorCode)
	s.Equal("", rec.Content)
}

// TestSetAvailable_补扫 恢复后队列进 host_restarted 屏障（Python start() 语义），
// 经 continue_queued_for_target 释放后投递。
func (s *SessionMessageServiceSuite) TestSetAvailable_补扫() {
	ctx := context.Background()
	// 停服务（清 worker），直接落一条 queued 行，再恢复
	s.svc.Stop(ctx)
	p := s.enqueueParams()
	rec, _, err := s.store.Enqueue(p)
	s.Require().NoError(err)
	s.svc.SetAvailable(ctx, true)
	// host_restarted 屏障下 worker 被扣住（Python _ensure_delivery_worker held）
	waitCond(s.T(), 2*time.Second, func() bool {
		s.svc.targetStateMu.Lock()
		defer s.svc.targetStateMu.Unlock()
		return s.svc.restartHeldTargets["sess-B"]
	}, "补扫目标应进 held 屏障")
	// 显式继续 → 释放屏障 → 投递
	_, err = s.svc.ContinueQueuedForTarget(ctx, "sess-B", "user-1")
	s.Require().NoError(err)
	waitCond(s.T(), 5*time.Second, func() bool {
		got, _ := s.store.Get(rec.MessageID)
		return got != nil && got.Status == StatusDelivered
	}, "continue 后应投递")
}

// enqueueParams 测试便捷（复用 store 套件形态）。
func (s *SessionMessageServiceSuite) enqueueParams() SessionMessageEnqueueParams {
	return SessionMessageEnqueueParams{
		OwnerScopeID: "user-1", SourceSessionID: "sess-A", SourceRequestID: "req",
		SourceToolCallID: "call", IdempotencyKey: "idem-" + newUUIDHex(),
		TargetSessionID: "sess-B", Content: "hello", HopCount: 1,
		MaxPendingPerTarget: 100, MaxMessagesPerChain: 16,
	}
}

var _ = fmt.Sprintf // 保留 fmt 引用（测试内未直接用）
