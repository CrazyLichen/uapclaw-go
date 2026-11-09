package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestDispatchGoalControl_无管理器返回结构化错误 未 start_interaction 时的兜底。
// Python: interface_deep.py:17812-17818
func TestDispatchGoalControl_无管理器返回结构化错误(t *testing.T) {
	d := &DeepAdapter{}
	out, err := d.dispatchGoalControl(context.Background(), "s1", "get", "", false, 0, 0)
	require.NoError(t, err)
	require.Equal(t, goalResultError, out["result_type"])
	require.Equal(t, goalErrManagerNotStarted, out["error_code"])
	require.Equal(t, "goal manager is not started", out["error"])
	require.Equal(t, "get", out["action"])
}

// TestFormatGoalControlMessage_文案逐字对齐 控制文案是前端展示契约。
// Python: interface_deep.py:17561-17572
func TestFormatGoalControlMessage_文案逐字对齐(t *testing.T) {
	require.Equal(t, "No goal in this session.", formatGoalControlMessage("get", nil))
	require.Equal(t, "Goal: 修完测试",
		formatGoalControlMessage("get", map[string]any{"objective": "修完测试"}))
	require.Equal(t, "No goal in this session.", formatGoalControlMessage("pause", nil))
	require.Equal(t, "Goal paused.", formatGoalControlMessage("pause", map[string]any{}))
	require.Equal(t, "Goal cleared.", formatGoalControlMessage("clear", nil))
	require.Equal(t, "Goal was not cleared.", formatGoalControlMessage("clear", map[string]any{}))
	require.Equal(t, "No goal in this session.", formatGoalControlMessage("resume", nil))
	require.Equal(t, "Goal resumed.", formatGoalControlMessage("resume", map[string]any{}))
	require.Equal(t, "Goal set.", formatGoalControlMessage("set", map[string]any{}))
	require.Equal(t, "Goal was not set.", formatGoalControlMessage("set", nil))
}

// TestGoalControlResult_四类result_type set/resume 且 ACTIVE 才进执行流。
// Python: interface_deep.py:17904-17930
func TestGoalControlResult_四类result_type(t *testing.T) {
	d := &DeepAdapter{}
	active := goal.CreateGoalRecord("s1", "修完测试", nil, nil)
	require.Equal(t, goal.GoalStatusActive, active.Status)

	out := d.goalControlResult("set", active)
	require.Equal(t, goalResultStream, out["result_type"])
	require.Equal(t, "Goal set.", out["output"])

	out = d.goalControlResult("resume", active)
	require.Equal(t, goalResultStream, out["result_type"])

	// 非 ACTIVE：set/resume 也只回控制类
	paused := goal.CreateGoalRecord("s1", "x", nil, nil)
	paused.Status = goal.GoalStatusPaused
	require.Equal(t, goalResultControl, d.goalControlResult("resume", paused)["result_type"])
	require.Equal(t, goalResultControl, d.goalControlResult("get", active)["result_type"])
	require.Equal(t, goalResultControl, d.goalControlResult("pause", paused)["result_type"])
}

// TestGoalRecordPayload_nil安全 nil 记录转 nil payload。
// Python: interface_deep.py:17554-17559
func TestGoalRecordPayload_nil安全(t *testing.T) {
	require.Nil(t, goalRecordPayload(nil))
	record := goal.CreateGoalRecord("s1", "目标", nil, nil)
	payload := goalRecordPayload(record)
	require.Equal(t, "目标", payload["objective"])
	require.Equal(t, "active", payload["status"])
	require.Equal(t, "s1", payload["session_id"])
}

// TestGoalErrorResult_四码映射 already_exists 走确认，其余走错误。
// Python: interface_deep.py:17895-17912
func TestGoalErrorResult_四码映射(t *testing.T) {
	d := &DeepAdapter{}
	existing := goal.CreateGoalRecord("s1", "旧目标", nil, nil)

	confirm := d.goalErrorResult("set", "新目标", &goal.GoalOperationError{
		Operation: "set", Code: goalErrAlreadyExists, Message: "a goal already exists", Goal: existing,
	})
	require.Equal(t, goalResultConfirmRequired, confirm["result_type"])
	require.Equal(t, goalErrAlreadyExists, confirm["error_code"])
	require.Equal(t, "新目标", confirm["requested_objective"])
	require.Equal(t, "旧目标", confirm["existing_goal"].(map[string]any)["objective"])

	invalid := d.goalErrorResult("set", "", &goal.GoalOperationError{
		Operation: "set", Code: "invalid_objective", Message: "goal objective must not be empty",
	})
	require.Equal(t, goalResultError, invalid["result_type"])
	require.Equal(t, "invalid_objective", invalid["error_code"])

	// 非 GoalOperationError：兜底码
	generic := d.goalErrorResult("set", "", errors.New("boom"))
	require.Equal(t, goalResultError, generic["result_type"])
	require.Equal(t, "goal_operation_failed", generic["error_code"])
	require.Equal(t, "boom", generic["error"])
}

// TestOptionalPositive 非正数视为未指定。
func TestOptionalPositive(t *testing.T) {
	require.Nil(t, optionalPositive(0))
	require.Nil(t, optionalPositive(-3))
	value := optionalPositive(200)
	require.NotNil(t, value)
	require.Equal(t, 200, *value)
}

// TestParseGoalSlashIntent_语法映射 只翻译产品语法，不碰 GoalManager。
// Python: interface_deep.py:15739-15757
func TestParseGoalSlashIntent_语法映射(t *testing.T) {
	cases := []struct {
		query  string
		action string
		object string
		isNil  bool
	}{
		{"/goal", "get", "", false},
		{"/goal   ", "get", "", false},
		{"/goal pause", "pause", "", false},
		{"/goal RESUME", "resume", "", false},
		{"/goal clear", "clear", "", false},
		{"/goal set 修完测试", "set", "修完测试", false},
		{"/goal set", "set", "", false},
		{"/goal 修完测试", "set", "修完测试", false},
		{"/goals", "set", "s", false},
		{"hello /goal", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			got := parseGoalSlashIntent(c.query)
			if c.isNil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, c.action, got["action"])
			if c.action == "set" {
				require.Equal(t, c.object, got["objective"])
			}
		})
	}
}

// TestShouldParseTUIGoalSlash_仅TUI且无结构化op 产品入口互斥规则。
// Python: interface_deep.py:15724-15737
func TestShouldParseTUIGoalSlash_仅TUI且无结构化op(t *testing.T) {
	require.True(t, shouldParseTUIGoalSlash(nil, false, "tui", "/goal"))
	require.False(t, shouldParseTUIGoalSlash(map[string]any{}, false, "tui", "/goal"))
	require.False(t, shouldParseTUIGoalSlash(nil, true, "tui", "/goal"))
	require.False(t, shouldParseTUIGoalSlash(nil, false, "web", "/goal"))
	require.False(t, shouldParseTUIGoalSlash(nil, false, "tui", 123))
}

// TestStructuredGoalOpFromRequest_只映射set与resume 结构化入口只接管执行类动作。
// Python: interface_deep.py:16251-16281
func TestStructuredGoalOpFromRequest_只映射set与resume(t *testing.T) {
	setReq := &schema.AgentRequest{
		ReqMethod: schema.ReqMethodCommandGoal,
		Params: mustJSON(t, map[string]any{
			"action": "set", "objective": "x", "overwrite_confirmed": true,
			"token_budget": "200", "max_attempts": 3,
		}),
	}
	op := structuredGoalOpFromRequest(setReq)
	require.NotNil(t, op)
	require.Equal(t, "set", op["action"])
	require.Equal(t, "x", op["objective"])
	require.Equal(t, true, op["overwrite_confirmed"])
	require.Equal(t, 200, op["token_budget"])
	require.Equal(t, 3, op["max_attempts"])

	resumeReq := &schema.AgentRequest{
		ReqMethod: schema.ReqMethodCommandGoal,
		Params:    mustJSON(t, map[string]any{"action": "resume"}),
	}
	resumeOp := structuredGoalOpFromRequest(resumeReq)
	require.NotNil(t, resumeOp)
	require.Equal(t, "resume", resumeOp["action"])
	require.NotContains(t, resumeOp, "objective")

	getReq := &schema.AgentRequest{
		ReqMethod: schema.ReqMethodCommandGoal,
		Params:    mustJSON(t, map[string]any{"action": "get"}),
	}
	require.Nil(t, structuredGoalOpFromRequest(getReq))
	require.Nil(t, structuredGoalOpFromRequest(&schema.AgentRequest{ReqMethod: schema.ReqMethodChatSend}))
	require.Nil(t, structuredGoalOpFromRequest(nil))
}

// mustJSON 序列化测试参数（失败即判失败）。
func mustJSON(t *testing.T, value map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

// TestGoalGuards_未启动时全为假 instance 缺失/未 start 时三守卫都返回 false。
// Python: interface_deep.py:15402/15427/15464
func TestGoalGuards_未启动时全为假(t *testing.T) {
	d := &DeepAdapter{}
	require.False(t, d.hasActiveGoalRound())
	require.False(t, d.goalRecordIsActive())
	require.False(t, d.HasActiveGoalInteraction())
	require.False(t, d.HasActiveGoal("s1"))
}

// TestHasActiveGoal_会话不匹配为假 子适配器只认自己绑定的会话。
// Python: interface_deep.py:15450-15462
func TestHasActiveGoal_会话不匹配为假(t *testing.T) {
	d := &DeepAdapter{isSessionScoped: true, parentSessionID: "sess-a"}
	require.False(t, d.HasActiveGoal("sess-b"))
	// 同会话时回落到 HasActiveGoalInteraction（未启动 → false）
	require.False(t, d.HasActiveGoal("sess-a"))
}

// TestHasActiveGoal_根无缓存子适配器为假 不借也不建 agent。
// Python: agent_manager.py:1423（inspect cached Goal owners without borrowing）
func TestHasActiveGoal_根无缓存子适配器为假(t *testing.T) {
	d := &DeepAdapter{sessionAdapters: newSessionAdapterCache()}
	require.False(t, d.HasActiveGoal("sess-none"))
	require.False(t, d.sessionAdapters.has("sess-none"), "探针不得创建子适配器")
}
