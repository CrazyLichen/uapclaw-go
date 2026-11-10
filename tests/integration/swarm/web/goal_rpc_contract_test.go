//go:build integration

package web_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalRPCContractSuite G1：command.goal 出站契约与前端 webClient.ts 读法逐条对拍。
// 跨子系统：swarm/server（E2A 分发）+ swarm/runtime（门面短路）
// + swarm/server/adapter（控制面）+ agentcore/harness（GoalManager）。
type GoalRPCContractSuite struct {
	goalContractSuite
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGoalRPCContractSuite 套件入口。
func TestGoalRPCContractSuite(t *testing.T) {
	suite.Run(t, new(GoalRPCContractSuite))
}

// SetupTest 每用例启动独立进程内 AgentServer 并隔离数据目录。
func (s *GoalRPCContractSuite) SetupTest() {
	s.env = newGoalContractEnv(s.T())
	isolateGoalDataDir(s.T())
}

// TearDownTest 刷盘并还原环境，避免污染后续用例。
func (s *GoalRPCContractSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue()
	sessionpkg.FlushHistoryQueue()
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// TestGet_空会话正常态且不写历史 一元 get 的空会话是正常态。
// Python: agent_ws_server.py:3717（只读、不兜底新建 session metadata）
// 前端: services/webClient.ts:892 requestGoalAction('get') 读 payload.goal ?? payload.record
// 对照: internal/swarm/server/runtime/interface_goal_shortcircuit_test.go:39-62
func (s *GoalRPCContractSuite) TestGet_空会话正常态且不写历史() {
	sid := "web-goal-get-empty"
	rid := sendGoal(s.env, sid, map[string]any{"action": "get"}, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.True(resp.OK, "空会话 get 属正常态")
	s.Equal("No goal in this session.", resp.Payload["message"])

	records, err := sessionpkg.ReadHistoryRecords(sid)
	s.Require().NoError(err)
	s.Empty(records, "只读 goal 查询不得写 user 轮历史")
}

// TestGet_流式单帧Snapshot 流式 get 由门面短路成单帧 goal.snapshot。
// Python: interface_deep.py:19358-19366（goal_control 单帧）
// Go: internal/swarm/server/runtime/interface.go:1511-1519
// 前端: hooks/useWebSocket.ts:4673 goal.snapshot → applyGoalSnapshot 读 payload.goal
func (s *GoalRPCContractSuite) TestGet_流式单帧Snapshot() {
	sid := "web-goal-get-stream"
	rid := sendGoal(s.env, sid, map[string]any{"action": "get"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	s.Require().NotEmpty(frames)
	s.Equal(eventGoalSnapshot, eventTypeOf(frames[0]), "流式 get 首帧应为 goal.snapshot")
	s.True(frames[len(frames)-1].IsComplete, "单帧必须带 is_complete=true")
	s.Contains(frames[0].Payload, "goal", "帧必须带 goal 字段（前端 applyGoalSnapshot 读取）")
}

// TestPause_空会话返回no_goal pause 空会话必须结构化报错，不得静默成功。
// Python: interface_deep.py:17815-17825（no_goal / "No goal in this session; cannot pause."）
// Go: internal/swarm/server/adapter/deep_adapter_goal.go case "pause"
// 前端: hooks/useWebSocket.ts:1849 reportFailure 消费这个失败。
func (s *GoalRPCContractSuite) TestPause_空会话返回no_goal() {
	sid := "web-goal-pause-empty"
	rid := sendGoal(s.env, sid, map[string]any{"action": "pause"}, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.False(resp.OK, "空会话 pause 属失败态（上游 goal_error）")
	s.Equal("no_goal", resp.Payload["code"])
	s.Contains(resp.Payload, "goal", "失败响应须带 goal:null")
	s.Nil(resp.Payload["goal"])
}

// TestClear_空会话返回no_goal clear 空会话结构化报错。
// Python: interface_deep.py 的 clear 分支（"No goal in this session; nothing to clear."）
// Go: internal/swarm/server/adapter/deep_adapter_goal.go case "clear"
// 前端: services/webClient.ts:898-900 仅在 resp 成功时返回 null；失败走 reportFailure
func (s *GoalRPCContractSuite) TestClear_空会话返回no_goal() {
	sid := "web-goal-clear-empty"
	rid := sendGoal(s.env, sid, map[string]any{"action": "clear"}, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.False(resp.OK, "空会话 clear 属失败态")
	s.Equal("no_goal", resp.Payload["code"])
	s.Nil(resp.Payload["goal"], "clear 后 goal 应为 null")
}

// TestUnknownAction_返回invalid_action 未知 action 结构化报错。
// Python: interface_deep.py:17900；前端 hooks/useWebSocket.ts:1849 reportFailure
func (s *GoalRPCContractSuite) TestUnknownAction_返回invalid_action() {
	sid := "web-goal-bad"
	rid := sendGoal(s.env, sid, map[string]any{"action": "explode"}, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.False(resp.OK)
	s.Equal("invalid_action", resp.Payload["code"])
}

// TestSet_进入流式链 set 必须继续走适配器流式链，不被门面单帧短路。
// Python: interface_deep.py:19880-19892（goal_stream 先 yield goal.snapshot）
// Go: internal/swarm/server/runtime/interface_goal_shortcircuit_test.go:87-108
// 前端: services/webClient.ts:916 sendGoalStreamCommand(set) 不等 res
func (s *GoalRPCContractSuite) TestSet_进入流式链且首帧为Snapshot() {
	sid := "web-goal-set"
	rid := sendGoal(s.env, sid, map[string]any{
		"action": "set", "objective": "修完测试", "overwrite_confirmed": true,
	}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	s.Require().NotEmpty(frames)
	// 无 goal manager 的环境会回 chat.error（既有基线）；有则回 goal.snapshot。
	// 两种都合法，但绝不能是「门面单帧短路后什么都没发生」。
	first := eventTypeOf(frames[0])
	s.Contains([]string{eventGoalSnapshot, eventChatError}, first,
		"set 必须进适配器链，首帧为 goal.snapshot 或 chat.error")
	if first == eventGoalSnapshot {
		s.False(frames[0].IsComplete, "流式 set 的 snapshot 是非终结帧")
	}
}

// TestResume_进入流式链 resume 与 set 走同一条 attach→control 路径。
// Python: interface_deep.py:16251-16257
func (s *GoalRPCContractSuite) TestResume_进入流式链() {
	sid := "web-goal-resume"
	rid := sendGoal(s.env, sid, map[string]any{"action": "resume"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	s.Require().NotEmpty(frames)
	s.Contains([]string{eventGoalSnapshot, eventChatError, eventRuntimeAccepted},
		eventTypeOf(frames[0]), "resume 必须进适配器链")
}

// TestSet未确认覆盖_返回确认要求 已存在目标且未确认覆盖时不得静默覆盖。
// 正常前端 set 恒带 overwrite_confirmed:true（webClient.ts:925-932，
// EditGoalModal 复用同一路径），此契约只能由后端直接构造未确认请求覆盖。
// Python: goal/manager.py:134-140 (already_exists) · interface_deep.py:19365-19375
func (s *GoalRPCContractSuite) TestSet未确认覆盖_返回确认要求() {
	sid := "web-goal-overwrite"
	rid1 := sendGoal(s.env, sid, map[string]any{
		"action": "set", "objective": "第一个目标", "overwrite_confirmed": true,
	}, false)
	s.Require().NotNil(s.env.AwaitUnary(rid1))

	rid2 := sendGoal(s.env, sid, map[string]any{
		"action": "set", "objective": "第二个目标", "overwrite_confirmed": false,
	}, false)
	resp2 := s.env.AwaitUnary(rid2)
	s.Require().NotNil(resp2)
	// 契约：要么显式 already_exists 错误，要么结构化 confirm_required；
	// 二者都不允许静默覆盖（会把用户第一个目标丢掉）。
	if !resp2.OK {
		s.Equal("already_exists", resp2.Payload["code"])
	} else {
		s.Contains([]string{eventGoalConfirmRequire, "goal_confirm_required"},
			resp2.Payload["event_type"], "未确认覆盖必须给前端确认信号")
	}
}

// TestExecutionError_帧形状 失败路径的帧必须带 goal 字段供前端落状态。
// Python: openjiuwen/harness/schema/interaction.py:167-177 (execution_error(goal=...))
// 前端: hooks/useWebSocket.ts:4705 execution.error → applyGoalSnapshot(payload)
func (s *GoalRPCContractSuite) TestExecutionError_帧形状() {
	sid := "web-goal-exec-error"
	rid := sendGoal(s.env, sid, map[string]any{"action": "explode"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	s.Require().NotEmpty(frames)
	found := false
	for _, c := range frames {
		if eventTypeOf(c) != eventExecutionError && eventTypeOf(c) != eventChatError {
			continue
		}
		found = true
		s.Contains(c.Payload, "goal", "失败帧须带 goal 字段")
	}
	s.True(found, "失败路径必须产出 execution.error 或 chat.error 帧")
}
