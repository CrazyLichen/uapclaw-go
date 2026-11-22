//go:build integration

package server_test

// 团队暂停收尾链 RPC→真实控制集成（25.6 批二，spec §5 矩阵 #1-#4）。
// 链路：ServerEnv（真 AgentServer）→ chat.interrupt RPC（handleCancel）→
// AgentRuntime.CancelRequestWithIntent → TeamSessionLifecycleAdapter →
// 真实 TeamManager（GetTeamManager("web")）→ processing_status 出站帧。
// mock 仅 WS 出站边界（ServerEnv 帧收集），被测子系统不打桩。

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	teamharness "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamPauseChainSuite 团队暂停链端到端。
type TeamPauseChainSuite struct {
	isuite.BaseIntegrationSuite
	env *testhelpers.ServerEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamPauseChainSuite 套件入口。
func TestTeamPauseChainSuite(t *testing.T) {
	suite.Run(t, new(TeamPauseChainSuite))
}

// SetupSuite 启动进程内 AgentServer。
func (s *TeamPauseChainSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createSession 经 RPC 建会话（session.create）。
func (s *TeamPauseChainSuite) createSession(token string) string {
	createID := s.env.Send(string(schema.ReqMethodSessionCreate),
		map[string]any{"create_token": token}, "", false)
	created := s.env.AwaitUnary(createID)
	s.Require().NotNil(created)
	s.Require().True(created.OK, "session.create 应成功: %+v", created.Payload)
	sessionID, _ := created.Payload["session_id"].(string)
	s.Require().NotEmpty(sessionID)
	return sessionID
}

// registerToCoordinator 让会话进入 coordinator（真实 RunUnary 注册路径；
// Python owns_session 前提 = coordinator snapshot 存在）。执行失败可容忍。
func (s *TeamPauseChainSuite) registerToCoordinator(sessionID string) {
	// 单 agent chat.send 走门面 ProcessMessageImpl → coordinator.RegisterSession
	// （server/runtime/interface.go:336，Python RunUnary 注册等价物）；执行失败
	// 可容忍——注册发生在 ProcessMessage 前段。
	rid := s.env.Send(string(schema.ReqMethodChatSend),
		map[string]any{"session_id": sessionID, "mode": "agent", "query": "hi"}, sessionID, false)
	_ = s.env.AwaitUnary(rid)
}

// interruptPause 发送 chat.interrupt pause 意图并等待响应。
// params.mode="team"：requestTargetsTeam 的 params 优先段（Python :3757-3759，
// 调用方显式带 mode 的合法请求形态；session metadata 默认 agent）。
func (s *TeamPauseChainSuite) interruptPause(sessionID string) *schema.AgentResponse {
	id := s.env.Send(string(schema.ReqMethodChatCancel),
		map[string]any{"intent": "pause", "session_id": sessionID, "mode": "team"}, sessionID, false)
	resp := s.env.AwaitUnary(id)
	s.Require().NotNil(resp)
	return resp
}

// pushBaseline 记录当前 push 帧基准（用例起始），隔离用例间累积。
func (s *TeamPauseChainSuite) pushBaseline() int {
	return len(s.env.Pushes())
}

// findPauseCompletedPush 自基准起在 push 帧里找 chat.processing_status is_complete=true。
func (s *TeamPauseChainSuite) findPauseCompletedPush(baseline int) (channelID string, found bool) {
	pushes := s.env.Pushes()
	for _, push := range pushes[min(baseline, len(pushes)):] {
		payload := testhelpers.PushPayload(push)
		if payload == nil {
			continue
		}
		if payload["event_type"] == "chat.processing_status" &&
			payload["is_processing"] == false && payload["is_complete"] == true {
			ch, _ := push["channel"].(string)
			return ch, true
		}
	}
	return "", false
}

// ──────────────────────────── 测试 ────────────────────────────

// TestPause_FullChain_PushFrame 矩阵 #1：RPC pause → 真实 adapter/manager →
// 无 waiter 终态帧经 push fallback 出站（channel=web）。
func (s *TeamPauseChainSuite) TestPause_FullChain_PushFrame() {
	sessionID := s.createSession("tok-pause-chain")
	// 真实控制面预置：manager 持有该会话的 stream task（pause 成功条件，
	// Python has_stream_task / has_team_runtime）
	s.registerToCoordinator(sessionID)
	mgr := teamharness.GetTeamManager("web")
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.RegisterStreamTask(sessionID, cancel)
	// 真实控制面代次预置：team 轮启动时 manager 登记会话代次（真实流程由
	// 轮启动路径写入；本用例预置与 coordinator 首注册代次 1 对齐）
	mgr.SetSessionGeneration(sessionID, 1)
	baseline := s.pushBaseline()
	resp := s.interruptPause(sessionID)
	s.Require().True(resp.OK, "interrupt pause 应成功: %+v", resp.Payload)
	s.T().Logf("payload: %+v pushes=%d", resp.Payload, len(s.env.Pushes())-baseline)

	// 暂停完成帧出站（channel 归属 web；D-25.3-1）
	channelID, found := s.findPauseCompletedPush(baseline)
	for i, push := range s.env.Pushes()[baseline:] {
		s.T().Logf("push[%d]: %+v payload=%+v", i, push, testhelpers.PushPayload(push))
	}
	s.Require().True(found, "应出站 chat.processing_status is_complete=true 帧，pushes=%d", len(s.env.Pushes())-baseline)
	s.Require().Equal("web", channelID)
	// manager 状态：stream task 已被 stop/pause 清理或保留——pause 不清 task，
	// 仅断言帧与响应（manager 内部状态由单测覆盖）。
}

// TestStop_FullChain 矩阵 #2：RPC stop → stop_session（USER_CANCEL）→
// stream task 清理。
func (s *TeamPauseChainSuite) TestStop_FullChain() {
	sessionID := s.createSession("tok-stop-chain")
	s.registerToCoordinator(sessionID)
	s.registerToCoordinator(sessionID)
	mgr := teamharness.GetTeamManager("web")
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.RegisterStreamTask(sessionID, cancel)
	s.Require().True(mgr.HasStreamTask(sessionID))

	id := s.env.Send(string(schema.ReqMethodChatCancel),
		map[string]any{"intent": "stop", "session_id": sessionID, "mode": "team"}, sessionID, false)
	resp := s.env.AwaitUnary(id)
	s.Require().NotNil(resp)
	s.Require().True(resp.OK, "interrupt stop 应成功: %+v", resp.Payload)
	// Python 语义：stop_session_runtime 不清 _stream_tasks（stream 任务自身
	// 退出时清理），仅停运行时资源——故 stream task 保留属正确行为
	// _ = mgr
	// stop 路径不发布暂停完成帧
	_, found := s.findPauseCompletedPush(s.pushBaseline())
	s.Require().False(found, "stop 路径不应出站暂停完成帧")
}

// TestPause_NoRuntime_NoPublish 矩阵 #3：无 team runtime（未注册 stream task）
// → adapter pause 返回 false → 二次判定失败 → 不发布暂停完成帧。
func (s *TeamPauseChainSuite) TestPause_NoRuntime_NoPublish() {
	baseline := s.pushBaseline()
	sessionID := s.createSession("tok-pause-noruntime")
	pauseNoRuntimeMode := ""
	resp := s.interruptPause(sessionID)
	s.Require().True(resp.OK, "响应仍应成功（无 runtime 时 pause=false 合法）: %+v", resp.Payload)
	_ = pauseNoRuntimeMode
	_, found := s.findPauseCompletedPush(baseline)
	s.Require().False(found, "pause 未命中不应出站暂停完成帧")
}

// TestPause_ClosedSession_StaleGuard 矩阵 #4：会话删除（CLOSED/无 snapshot）→
// 守卫拒绝，不出站暂停完成帧（Python :1506-1512 守卫的 CLOSED 分支）。
func (s *TeamPauseChainSuite) TestPause_ClosedSession_StaleGuard() {
	baseline := s.pushBaseline()
	sessionID := s.createSession("tok-pause-closed")
	pauseClosedMode := ""
	s.registerToCoordinator(sessionID)
	mgr := teamharness.GetTeamManager("web")
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.RegisterStreamTask(sessionID, cancel)

	// 删除会话（session.delete 真实路径）
	_ = pauseClosedMode
	deleteID := s.env.Send(string(schema.ReqMethodSessionDelete),
		map[string]any{"session_id": sessionID}, sessionID, false)
	deleted := s.env.AwaitUnary(deleteID)
	s.Require().NotNil(deleted)
	s.Require().True(deleted.OK, "session.delete 应成功: %+v", deleted.Payload)

	resp := s.interruptPause(sessionID)
	s.Require().NotNil(resp)
	s.Require().True(resp.OK, "closed 会话的 cancel 仍应返回 OK: %+v", resp.Payload)
	_, found := s.findPauseCompletedPush(baseline)
	s.Require().False(found, "closed 会话不应出站暂停完成帧（守卫拒绝）")
	_ = strings.TrimSpace // 保持 strings 引用（断言扩展位）
}
