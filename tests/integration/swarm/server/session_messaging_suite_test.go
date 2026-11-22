//go:build integration

package server_test

// Session messaging 邮箱端到端集成（25.6 批一，spec §3.8 集成列）。
// 真实跨子系统链路：tools.SessionMessagingToolkit（swarm/agents/harness/common/tools）
// → server port 适配器 → SessionMessageService 编排（校验链/worker/状态机）
// → SessionMessageStore（sqlite 落盘）+ admission/execute 注入缝。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// sessionMessageAdmissionFake 准入假实现（记录 begin/end 调用序）。
type sessionMessageAdmissionFake struct {
	mu    sync.Mutex
	begun []string
	ended []string
}

func (f *sessionMessageAdmissionFake) BeginSessionMessage(_, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begun = append(f.begun, runID)
	return nil
}

func (f *sessionMessageAdmissionFake) EndSessionMessage(_, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = append(f.ended, runID)
	return nil
}

func (f *sessionMessageAdmissionFake) beginCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.begun)
}

// IsUserActive / IsSessionMessageActive / IsHeartbeatActive / IsSessionMessageBlocked
// busy 探针恒否。
func (f *sessionMessageAdmissionFake) IsUserActive(string) bool            { return false }
func (f *sessionMessageAdmissionFake) IsSessionMessageActive(string) bool  { return false }
func (f *sessionMessageAdmissionFake) IsHeartbeatActive(string) bool       { return false }
func (f *sessionMessageAdmissionFake) IsSessionMessageBlocked(string) bool { return false }

// sessionMessageExecuteFake 执行假实现（可编程脚本 + 调用记录）。
type sessionMessageExecuteFake struct {
	mu     sync.Mutex
	calls  int
	calls2 int
	record *server.SessionMessageRecord
	script []server.SessionMessageExecutionResult
}

func (f *sessionMessageExecuteFake) Execute(record *server.SessionMessageRecord) (server.SessionMessageExecutionResult, error) {
	f.mu.Lock()
	f.calls++
	f.calls2++
	idx := f.calls
	rec := *record
	f.record = &rec
	var out server.SessionMessageExecutionResult
	if len(f.script) > 0 {
		out = f.script[0]
		f.script = f.script[1:]
	} else {
		out = server.SessionMessageExecutionResult{Status: "succeeded"}
	}
	f.mu.Unlock()
	_ = idx
	return out, nil
}

func (f *sessionMessageExecuteFake) snapshot() (int, *server.SessionMessageRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.record
}

// sessionMessageMetaFake 元数据假实现（同 user 域两个会话）。
type sessionMessageMetaFake struct {
	sessions map[string]map[string]any
}

func (f *sessionMessageMetaFake) GetSessionMetadata(sessionID string) (map[string]any, bool) {
	m, ok := f.sessions[sessionID]
	return m, ok
}

func (f *sessionMessageMetaFake) GetAllSessionsMetadata() ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(f.sessions))
	for _, m := range f.sessions {
		out = append(out, m)
	}
	return out, nil
}

// sessionMessageModesFake mode 判定假实现（全单 agent）。
type sessionMessageModesFake struct{}

func (sessionMessageModesFake) DeprecateMode(mode string) string { return mode }
func (sessionMessageModesFake) IsSingleAgentMode(string) bool    { return true }
func (sessionMessageModesFake) IsTeamMode(string) bool           { return false }
func (sessionMessageModesFake) IsPlanMode(string) bool           { return false }

// SessionMessagingSuite 跨会话消息邮箱端到端（25.6 批一）。
type SessionMessagingSuite struct {
	isuite.BaseIntegrationSuite
	admit   *sessionMessageAdmissionFake
	exec    *sessionMessageExecuteFake
	meta    *sessionMessageMetaFake
	svc     *server.SessionMessageService
	toolkit *tools.SessionMessagingToolkit
	// services 本用例创建的全部 service（TearDown 全停，防与 TempDir 清理竞态）
	services []*server.SessionMessageService
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionMessagingSuite 套件入口。
func TestSessionMessagingSuite(t *testing.T) {
	suite.Run(t, new(SessionMessagingSuite))
}

// spawnService 组装并登记 service。
func (s *SessionMessagingSuite) spawnService(store *server.SessionMessageStore, exec server.SessionMessageExecuteFunc, available bool, watchdog float64) *server.SessionMessageService {
	svc := server.NewSessionMessageService(server.SessionMessageServiceParams{
		Store: store, Admission: s.admit, Execute: exec,
		ModeClassifier: sessionMessageModesFake{}, MetadataProvider: s.meta,
		Available: available, ExecutionWatchdogTimeout: watchdog,
	})
	s.services = append(s.services, svc)
	return svc
}

// TearDownTest 停全部 service 等 worker 退出，避免与 TempDir 清理竞态。
func (s *SessionMessagingSuite) TearDownTest() {
	for _, svc := range s.services {
		svc.Stop(context.Background())
	}
}

// SetupTest 隔离 sqlite + 装配全链。
func (s *SessionMessagingSuite) SetupTest() {
	s.admit = &sessionMessageAdmissionFake{}
	s.exec = &sessionMessageExecuteFake{}
	s.meta = &sessionMessageMetaFake{sessions: map[string]map[string]any{
		"sess-A": {"mode": "agent", "user_id": "u1", "channel_id": "web", "title": "A"},
		"sess-B": {"mode": "agent", "user_id": "u1", "channel_id": "web", "title": "B"},
		"sess-C": {"mode": "agent", "user_id": "u2", "channel_id": "web", "title": "C"},
	}}
	s.svc = s.spawnService(
		server.NewSessionMessageStore(s.T().TempDir()+"/session_messages.sqlite3", "WAL"),
		s.exec.Execute, true, 5)
	s.toolkit = tools.NewSessionMessagingToolkit(server.NewSessionMessagingServicePort(s.svc))
}

// routeCtx 绑定发送方路由（对齐 route rail 注入后的调用级 ctx，
// 含 tool_call_id——rail before_tool_call 的宿主等价物）。
func (s *SessionMessagingSuite) routeCtx(sessionID string) context.Context {
	ctx, _ := tools.BindSessionMessagingRoute(context.Background(), sessionID, "req-"+sessionID, "u1", nil)
	return tools.WithSessionMessagingToolCallID(ctx, "tc-"+sessionID)
}

// waitCond 条件轮询（100ms 间隔，默认 5s 上限）。
func waitCond25(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("条件超时未满足: %s", msg)
}

// ──────────────────────────── 测试 ────────────────────────────

// TestSendMessage_ColdSessionWakeup 用例 1（矩阵 #1/#7）：工具 → 落库 →
// 投递 worker → execute 回调 → 状态落库；重复幂等键去重。
func (s *SessionMessagingSuite) TestSendMessage_ColdSessionWakeup() {
	out := s.toolkit.SendMessage(s.routeCtx("sess-A"), "sess-B", "读 X 文件", "")
	s.Equal(true, out["accepted"], "工具层应 accepted: %v", out)
	s.Equal("queued", out["status"])
	msgID, _ := out["message_id"].(string)
	s.NotEmpty(msgID)

	// 落库即达（先于 worker 拾取的持久化保证）
	rec, err := s.svc.Store().Get(msgID)
	s.Require().NoError(err)
	s.Require().NotNil(rec)
	s.Equal("sess-A", rec.SourceSessionID)
	s.Equal("sess-B", rec.TargetSessionID)
	s.Equal(1, rec.HopCount, "hop+1")

	// 冷会话唤醒：worker 投递 → execute 收到 record → succeeded 落库
	waitCond25(s.T(), 5*time.Second, func() bool {
		rec, _ := s.svc.Store().Get(msgID)
		return rec != nil && rec.Status == "succeeded"
	}, "消息应送达并置 succeeded")
	calls, rec2 := s.exec.snapshot()
	s.GreaterOrEqual(calls, 1)
	s.Equal("sess-B", rec2.TargetSessionID)
	s.Equal("session-message-"+msgID, rec2.ExecutionRequestID, "claim 写 execution_request_id")

	// 幂等：同 tool_call_id 重发 → deduplicated=true 不重复执行
	dup := s.toolkit.SendMessage(s.routeCtx("sess-A"), "sess-B", "读 X 文件", "")
	s.Equal(true, dup["deduplicated"], "重复发送应去重: %v", dup)
	calls2, _ := s.exec.snapshot()
	s.Equal(calls, calls2, "去重后不应再次执行")
}

// TestSendMessage_ValidationChainAndOwnerScope 用例 2（矩阵 #3/#5/#6）：
// 错误码经 toolkit → service 全链透传；跨 user 不可见；hop 上限。
func (s *SessionMessagingSuite) TestSendMessage_ValidationChainAndOwnerScope() {
	// 自发拒绝
	_, err := s.svc.SendMessage(context.Background(), s.serverSource("sess-A"), "sess-A", "hi", "")
	s.Require().Error(err)
	s.Equal("SELF_SEND_NOT_ALLOWED", messagingErrCode(err))
	// 空内容
	_, err = s.svc.SendMessage(context.Background(), s.serverSource("sess-A"), "sess-B", "  ", "")
	s.Require().Error(err)
	s.Equal("INVALID_ARGUMENT", messagingErrCode(err))
	// 32KB 上限
	_, err = s.svc.SendMessage(context.Background(), s.serverSource("sess-A"), "sess-B", strings.Repeat("x", 33*1024), "")
	s.Require().Error(err)
	s.Equal("LIMIT_EXCEEDED", messagingErrCode(err))
	// 跨 user 不可见（sess-C 属 u2）
	_, err = s.svc.SendMessage(context.Background(), s.serverSource("sess-A"), "sess-C", "hi", "")
	s.Require().Error(err)
	s.Equal("NOT_FOUND_OR_FORBIDDEN", messagingErrCode(err))
	// 不存在的目标
	_, err = s.svc.SendMessage(context.Background(), s.serverSource("sess-A"), "sess-Z", "hi", "")
	s.Require().Error(err)
	s.Equal("NOT_FOUND_OR_FORBIDDEN", messagingErrCode(err))
	// hop 上限：hop_count 已达上限时直接拒
	src := s.serverSource("sess-A")
	src.HopCount = 64
	_, err = s.svc.SendMessage(context.Background(), src, "sess-B", "hi", "")
	s.Require().Error(err)
	s.Equal("LIMIT_EXCEEDED", messagingErrCode(err))
}

// TestSteerDefer_ThenDeliver 用例 3（矩阵 #8/#9/#12）：steer 被拒 → defer
// 归一化 task 道 → 放行送达；requires_task_queue 降级排队。
func (s *SessionMessagingSuite) TestSteerDefer_ThenDeliver() {
	var mu sync.Mutex
	var secondInputMode = "unset"
	calls := 0
	exec := func(record *server.SessionMessageRecord) (server.SessionMessageExecutionResult, error) {
		mu.Lock()
		calls++
		first := calls == 1
		if !first {
			secondInputMode = record.InputMode
		}
		mu.Unlock()
		if first {
			return server.SessionMessageExecutionResult{Status: "failed",
				ErrorCode: "SESSION_INPUT_QUEUE_REQUIRED", Error: "rejected"}, nil
		}
		return server.SessionMessageExecutionResult{Status: "delivered"}, nil
	}
	svc := s.spawnService(server.NewSessionMessageStore(s.T().TempDir()+"/sm2.sqlite3", "WAL"), exec, true, 5)
	tk := tools.NewSessionMessagingToolkit(server.NewSessionMessagingServicePort(svc))
	out := tk.SendMessage(s.routeCtx("sess-A"), "sess-B", "hi", "steer")
	s.Equal(true, out["accepted"])
	msgID, _ := out["message_id"].(string)

	waitCond25(s.T(), 5*time.Second, func() bool {
		rec, _ := svc.Store().Get(msgID)
		return rec != nil && rec.Status == "delivered"
	}, "defer 后应送达")
	mu.Lock()
	defer mu.Unlock()
	s.Equal("", secondInputMode, "defer 归一化：task 道执行时 input_mode 已置空")
}

// TestWatchdogUnknown_ReleasesAdmissionFirst 用例 4（矩阵 #11/#12）：
// watchdog 超时 → 先释放 admission 再置 unknown；unknown 不自动降级。
func (s *SessionMessagingSuite) TestWatchdogUnknown_ReleasesAdmissionFirst() {
	block := make(chan struct{})
	svc := s.spawnService(server.NewSessionMessageStore(s.T().TempDir()+"/sm3.sqlite3", "WAL"),
		func(record *server.SessionMessageRecord) (server.SessionMessageExecutionResult, error) {
			<-block
			return server.SessionMessageExecutionResult{Status: "succeeded"}, nil
		}, true, 0.05)
	tk := tools.NewSessionMessagingToolkit(server.NewSessionMessagingServicePort(svc))
	out := tk.SendMessage(s.routeCtx("sess-A"), "sess-B", "slow", "follow_up")
	msgID, _ := out["message_id"].(string)
	waitCond25(s.T(), 5*time.Second, func() bool {
		rec, _ := svc.Store().Get(msgID)
		return rec != nil && rec.Status == "unknown"
	}, "watchdog 超时应置 unknown")
	rec, _ := svc.Store().Get(msgID)
	s.Equal("EXECUTION_WATCHDOG_TIMEOUT", rec.LastErrorCode)
	s.admit.mu.Lock()
	begun, ended := len(s.admit.begun), len(s.admit.ended)
	s.admit.mu.Unlock()
	s.Equal(begun, ended, "unknown 终态前 admission 应已释放")
	s.NotZero(begun)
	close(block)
	// unknown 不自动降级：状态保持 unknown 不重跑
	time.Sleep(200 * time.Millisecond)
	rec2, _ := svc.Store().Get(msgID)
	s.Equal("unknown", rec2.Status)
}

// TestBlockedTarget_RecoveryResweep 用例 5（矩阵 #1/#14）：删除中目标
// worker hold/消息终结；重启补扫续投。
func (s *SessionMessagingSuite) TestBlockedTarget_RecoveryResweep() {
	// 删除中目标：BeginTargetDelete 后发送被拒（NOT_FOUND_OR_FORBIDDEN）
	s.Require().NoError(s.svc.BeginTargetDelete(context.Background(), "sess-B"))
	_, err := s.svc.SendMessage(context.Background(), s.serverSource("sess-A"), "sess-B", "hi", "")
	s.Require().Error(err)
	s.Equal("NOT_FOUND_OR_FORBIDDEN", messagingErrCode(err))
	n, err := s.svc.OnTargetDeleted(context.Background(), "sess-B")
	s.Require().NoError(err)
	s.Equal(0, n, "无在途消息")
	_ = n

	// 重启补扫：新 service + 同 store，Start 后 queued 消息续投
	store := server.NewSessionMessageStore(s.T().TempDir()+"/sm4.sqlite3", "WAL")
	svc1 := s.spawnService(store, func(*server.SessionMessageRecord) (server.SessionMessageExecutionResult, error) {
		return server.SessionMessageExecutionResult{Status: "succeeded"}, nil
	}, false, 5) // 先不可用：消息只落库不投
	tk1 := tools.NewSessionMessagingToolkit(server.NewSessionMessagingServicePort(svc1))
	out := tk1.SendMessage(s.routeCtx("sess-A"), "sess-B", "crash-recover", "")
	msgID, _ := out["message_id"].(string)
	waitCond25(s.T(), 5*time.Second, func() bool {
		rec, _ := store.Get(msgID)
		return rec != nil && rec.Status == "queued"
	}, "不可用期消息保持 queued")

	// 「重启」：新 service 挂同一 store，Start() 补扫 → held 屏障 =
	// host_restarted 暂停（Python 语义：重启本身不授权恢复，须显式 continue）
	svc2 := s.spawnService(store, s.exec.Execute, true, 5)
	s.Require().NoError(svc2.Start(context.Background()))
	svc2.SetAvailable(context.Background(), true)
	time.Sleep(200 * time.Millisecond)
	recPaused, _ := store.Get(msgID)
	s.Require().NotNil(recPaused)
	s.Equal("queued", recPaused.Status, "重启后 held 屏障应暂停投递（host_restarted）")

	// 显式 continue → 解除屏障 → 送达（Python session_continue_queued :690 discard）
	resumed, err := svc2.ContinueQueuedForTarget(context.Background(), "sess-B", "u1")
	s.Require().NoError(err)
	s.NotEmpty(resumed)
	waitCond25(s.T(), 5*time.Second, func() bool {
		rec, _ := store.Get(msgID)
		return rec != nil && rec.Status == "succeeded"
	}, "continue 解除屏障后续投送达")
}

// messagingErrCode 提取 SessionMessagingError 的稳定错误码。
func messagingErrCode(err error) string {
	var msgErr *server.SessionMessagingError
	if errors.As(err, &msgErr) {
		return msgErr.Code
	}
	return "NO_SESSION_MESSAGING_ERROR:" + err.Error()
}

// serverSource 构造 server 侧源（测试直调 service 用）。
func (s *SessionMessagingSuite) serverSource(sessionID string) *server.SessionMessageSource {
	return &server.SessionMessageSource{
		SessionID: sessionID,
		RequestID: "req-" + sessionID,
		UserID:    "u1",
	}
}

// 防编译期未使用（fmt 保留给后续调试输出）。
var _ = fmt.Sprintf
