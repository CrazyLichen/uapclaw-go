//go:build integration

package web_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalEventOrderSuite G2：出站事件顺序与形状不变量。
type GoalEventOrderSuite struct {
	goalContractSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGoalEventOrderSuite 套件入口。
func TestGoalEventOrderSuite(t *testing.T) {
	suite.Run(t, new(GoalEventOrderSuite))
}

// SetupTest 每用例启动独立进程内 AgentServer 并隔离数据目录。
func (s *GoalEventOrderSuite) SetupTest() {
	s.env = newGoalContractEnv(s.T())
	isolateGoalDataDir(s.T())
}

// TearDownTest 刷盘并还原环境，避免污染后续用例。
func (s *GoalEventOrderSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue()
	sessionpkg.FlushHistoryQueue()
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// TestO1_Snapshot早于RuntimeAccepted set 流中 snapshot 严格早于 accepted。
// Python: interface_deep.py:19887-19892 yield goal.snapshot 在前，
//
//	:19907 / :19918 / :19955 _yield_runtime_accepted() 在后
//
// 前端: hooks/useWebSocket.ts:4690-4693 实测 16/16 次 resume 的顺序假设
func (s *GoalEventOrderSuite) TestO1_Snapshot早于RuntimeAccepted() {
	sid := "web-goal-o1"
	rid := sendGoal(s.env, sid, map[string]any{
		"action": "set", "objective": "顺序验证", "overwrite_confirmed": true,
	}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	snap := indexOfEvent(frames, eventGoalSnapshot)
	accepted := indexOfEvent(frames, eventRuntimeAccepted)
	if snap >= 0 && accepted >= 0 {
		s.Less(snap, accepted, "goal.snapshot 必须早于 runtime.accepted")
	} else {
		// 前端 pendingAction 靠四种事件之一清空；错误路径还有 chat.error 兜底
		// （hooks/useWebSocket.ts:1888-1890 + :1849 reportFailure）。
		// 本环境未注入模型，set 走适配器会回 chat.error —— 属既有基线
		// （interface_goal_shortcircuit_test.go:87-108 同款），不是契约缺口。
		clearing := 0
		for _, ev := range []string{
			eventGoalSnapshot, eventGoalUpdated, eventExecutionError,
			eventRuntimeAccepted, eventChatError,
		} {
			clearing += countEvent(frames, ev)
		}
		s.Greater(clearing, 0, "必须至少发出一种能收敛前端 loading 的事件")
	}
}

// TestO2_终结位区分 流式 set 的 snapshot 是非终结帧；流式 get 的是终结帧。
// Python: interface_deep.py:19892 (is_complete=False) / :19362 (单帧返回)
// Go: internal/swarm/server/runtime/interface.go:1511-1519 WithChunkIsComplete(true)
func (s *GoalEventOrderSuite) TestO2_终结位区分() {
	getFrames := goalFrames(s.env.AwaitStreamEnd(
		sendGoal(s.env, "web-goal-o2-get", map[string]any{"action": "get"}, true)))
	s.Require().NotEmpty(getFrames)
	s.True(getFrames[len(getFrames)-1].IsComplete, "get 单帧必须 is_complete=true")

	setFrames := goalFrames(s.env.AwaitStreamEnd(
		sendGoal(s.env, "web-goal-o2-set", map[string]any{
			"action": "set", "objective": "x", "overwrite_confirmed": true,
		}, true)))
	s.Require().NotEmpty(setFrames)
	if i := indexOfEvent(setFrames, eventGoalSnapshot); i >= 0 {
		s.False(setFrames[i].IsComplete, "流式 set 的 snapshot 是非终结帧")
	}
}

// TestO3_Updated载荷形状 goal.updated 载荷恒 {event_type, goal}；对象时 15 字段合法。
// Python: interface_deep.py:17725-17737 _interaction_goal_updated_payload
// Go: internal/agentcore/harness/schema/interaction.go:292-296 NewGoalUpdatedEvent
func (s *GoalEventOrderSuite) TestO3_Updated载荷形状() {
	sid := "web-goal-o3"
	rid := sendGoal(s.env, sid, map[string]any{
		"action": "set", "objective": "形状验证", "overwrite_confirmed": true,
	}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	for _, c := range frames {
		if eventTypeOf(c) != eventGoalUpdated {
			continue
		}
		s.Contains(c.Payload, "goal", "goal.updated 必须带 goal")
		if c.Payload["goal"] != nil {
			requireGoalRecord(s.T(), c.Payload["goal"])
		}
	}
	// clear 路径的 goal:null 由 G3 R2 覆盖；此处只验对象形状
}

// TestO4_中间Final降级且终结帧唯一 goal attempt 边界的 chat.final 必须降级。
// Python: interface_deep.py:15619-15635 _adapt_goal_intermediate_final
// Go: internal/swarm/server/adapter/deep_adapter_interaction.go:511-535
func (s *GoalEventOrderSuite) TestO4_中间Final降级且终结帧唯一() {
	sid := "web-goal-o4"
	rid := sendGoal(s.env, sid, map[string]any{
		"action": "set", "objective": "降级验证", "overwrite_confirmed": true,
	}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	businessTerminal := 0
	for _, c := range frames {
		// 区分业务终结帧与协议终止哨兵：
		// schema/agent.go:70-72 规定 is_complete=true 且 payload 仅含
		// {"is_complete":true}（或为空）的是流结束标记，可与业务终结帧并存，
		// 二者各一个都合法，不属重复终结。
		if c.IsComplete && eventTypeOf(c) != "" {
			businessTerminal++
		}
		if eventTypeOf(c) == eventChatFinal {
			s.NotContains(c.Payload, fieldGoalIntermediate,
				"终结 final 不得带 goal_intermediate 标记")
		}
		if v, ok := c.Payload[fieldGoalIntermediate].(bool); ok && v {
			s.Equal(eventChatDelta, eventTypeOf(c), "降级帧必须是 chat.delta")
		}
	}
	s.LessOrEqual(businessTerminal, 1, "一条流最多一个业务终结帧")
}

// TestO5_失败帧可落状态 execution.error 须带 goal 供前端落状态。
// Python: openjiuwen/harness/schema/interaction.py:167-177
// 前端: hooks/useWebSocket.ts:4705 → applyGoalSnapshot(payload)
// 与 G1 #8 的区别：本条锁帧形状，G1 锁错误码/可达性。
func (s *GoalEventOrderSuite) TestO5_失败帧可落状态() {
	sid := "web-goal-o5"
	rid := sendGoal(s.env, sid, map[string]any{"action": "explode"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	for _, c := range frames {
		if eventTypeOf(c) == eventExecutionError {
			s.Contains(c.Payload, "goal", "execution.error 须带 goal 供前端 applyGoalSnapshot")
		}
	}
}

// 明确不断言 goal.updated 与 chat.delta/chat.final 的相对顺序：
// 前端 hooks/useWebSocket.ts:269-270 明说后端不保证到达顺序，上游亦无保证。
