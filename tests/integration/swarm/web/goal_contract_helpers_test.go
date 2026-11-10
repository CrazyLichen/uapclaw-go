//go:build integration

package web_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// goalFrames 收集一次流式请求的全部帧，供顺序与形状断言复用。
type goalFrames []*schema.AgentResponseChunk

// goalContractSuite 所有 G1-G4 suite 的公共基类：进程内 AgentServer + 每用例隔离数据目录。
type goalContractSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境（E2A 信封投递 + 帧收集）
	env *testhelpers.ServerEnv
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// eventGoalSnapshot 目标快照事件（webClient.ts applyGoalSnapshot 消费）
	eventGoalSnapshot = "goal.snapshot"
	// eventGoalUpdated 目标记录变更事件
	eventGoalUpdated = "goal.updated"
	// eventGoalConfirmRequire 目标覆盖确认事件
	eventGoalConfirmRequire = "goal.confirm_required"
	// eventRuntimeAccepted 请求受理确认事件
	eventRuntimeAccepted = "runtime.accepted"
	// eventExecutionError 轮次执行失败事件
	eventExecutionError = "execution.error"
	// eventChatFinal 正式回答终态事件
	eventChatFinal = "chat.final"
	// eventChatDelta 流式增量事件
	eventChatDelta = "chat.delta"
	// eventChatError 面向用户的错误事件
	eventChatError = "chat.error"
	// fieldGoalIntermediate goal attempt 边界降级标记
	fieldGoalIntermediate = "goal_intermediate"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// eventTypeOf 取帧的业务事件名；非字符串返回空串。
func eventTypeOf(c *schema.AgentResponseChunk) string {
	if c == nil || c.Payload == nil {
		return ""
	}
	v, _ := c.Payload["event_type"].(string)
	return v
}

// indexOfEvent 返回首个指定事件帧的下标，不存在返回 -1。
func indexOfEvent(frames goalFrames, event string) int {
	for i, c := range frames {
		if eventTypeOf(c) == event {
			return i
		}
	}
	return -1
}

// countEvent 统计指定事件帧数量。
func countEvent(frames goalFrames, event string) int {
	n := 0
	for _, c := range frames {
		if eventTypeOf(c) == event {
			n++
		}
	}
	return n
}

// newGoalContractEnv 启动进程内 AgentServer。
// 签名只依赖 *testing.T，不绑定具体 suite —— G3 R1 用 AgentSuite 派生
// 的 suite 也能复用，避免后半程改签名波及所有调用点。
// Python: 进程内 AgentServer 等价于 tests/system_tests 的 conftest fixture。
// Go 先例: tests/integration/swarm/server/goal_dispatch_test.go:40-45
func newGoalContractEnv(t *testing.T) *testhelpers.ServerEnv {
	t.Helper()
	cfg, err := config.New("")
	require.NoError(t, err)
	return testhelpers.NewServerEnv(t, cfg, nil)
}

// isolateGoalDataDir 每个用例隔离 UAPCLAW_DATA_DIR 与会话缓存。
// 同样只依赖 *testing.T，供 BaseIntegrationSuite / AgentSuite 派生的套件共用。
// Go 先例: goal_dispatch_test.go:48-60（TearDown 先 FlushMetadataQueue 再清缓存）
func isolateGoalDataDir(t *testing.T) {
	t.Helper()
	t.Setenv(pathutil.EnvDataDir, t.TempDir())
	pathutil.ResetCache()
	sessionpkg.ClearAllSessionMetadataCache()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sendGoal 投递 command.goal，返回 request_id。
// isStream=false 对应 webClient.ts:892 requestGoalAction（一元）；
// isStream=true  对应 webClient.ts:916 sendGoalStreamCommand（流式且不等 res）。
// Python: jiuwenswarm/server/agent_ws_server.py 的 ReqMethod.COMMAND_GOAL 分发。
func sendGoal(s *goalContractSuite, sid string, params map[string]any, isStream bool) string {
	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params["mode"]; !ok {
		params["mode"] = "agent"
	}
	return s.env.Send(string(schema.ReqMethodCommandGoal), params, sid, isStream)
}

// requireNumeric 接受 JSON 反序列化的 float64 与 Go 直构的 int 两种数值。
// Python: GoalRecord.to_dict 的数值字段经 WS JSON 后统一变成 number。
func requireNumeric(t *testing.T, v any, field string) {
	t.Helper()
	switch v.(type) {
	case float64, float32, int, int64, int32:
	default:
		t.Fatalf("字段 %s 必须是数值，实际 %T (%v)", field, v, v)
	}
}

// requireGoalRecord 断言 GoalRecord 的 15 字段形状。
// Python: openjiuwen/harness/goal/schema.py:210-227 (to_dict)
// Go: internal/agentcore/harness/goal/schema.go:337-359 (ToDict)
// 用白名单 + 类型断言，不用 golden JSON 全等（goal_id 随机、时间戳变化）。
func requireGoalRecord(t *testing.T, raw any) map[string]any {
	t.Helper()
	rec, ok := raw.(map[string]any)
	require.True(t, ok, "goal 必须是对象，实际 %T", raw)

	for _, key := range []string{
		"goal_id", "session_id", "objective", "status",
		"created_at", "updated_at",
	} {
		require.IsType(t, "", rec[key], "字段 %s 必须是字符串", key)
	}
	for _, key := range []string{"revision", "attempt_count", "time_used_seconds"} {
		requireNumeric(t, rec[key], key)
	}
	require.Contains(t, []string{"active", "paused", "completed", "blocked"},
		rec["status"], "status 必须属 4 值枚举")

	usage, ok := rec["token_usage"].(map[string]any)
	require.True(t, ok, "token_usage 必须是对象")
	for _, key := range []string{
		"input_tokens", "output_tokens", "cached_input_tokens", "total_tokens",
	} {
		requireNumeric(t, usage[key], "token_usage."+key)
	}

	for _, key := range []string{"max_attempts", "token_budget"} {
		if rec[key] != nil {
			requireNumeric(t, rec[key], key)
		}
	}
	for _, key := range []string{"last_stop_reason", "active_started_at"} {
		if rec[key] != nil {
			require.IsType(t, "", rec[key], "字段 %s 必须是字符串或 null", key)
		}
	}
	if rec["last_assessment"] != nil {
		a, ok := rec["last_assessment"].(map[string]any)
		require.True(t, ok, "last_assessment 必须是对象或 null")
		require.IsType(t, "", a["status"], "last_assessment.status 必须是字符串")
		require.Contains(t, []string{"continue", "complete", "blocked"}, a["status"])
		require.IsType(t, "", a["evidence"], "last_assessment.evidence 必须是字符串")
		for _, key := range []string{"remaining_work", "next_instruction"} {
			if a[key] != nil {
				require.IsType(t, "", a[key], "last_assessment.%s 必须是字符串或 null", key)
			}
		}
	}
	return rec
}

// ──────────────────────────── 测试 ────────────────────────────

// TestGoalContractHelpersSuite 自测助手：索引、计数、15 字段校验的红绿基线。
func TestGoalContractHelpersSuite(t *testing.T) {
	frames := goalFrames{
		{Payload: map[string]any{"event_type": eventGoalSnapshot}},
		{Payload: map[string]any{"event_type": eventRuntimeAccepted}},
		{Payload: map[string]any{"event_type": eventGoalUpdated}},
	}
	require.Equal(t, 0, indexOfEvent(frames, eventGoalSnapshot))
	require.Equal(t, 1, indexOfEvent(frames, eventRuntimeAccepted))
	require.Equal(t, -1, indexOfEvent(frames, eventGoalConfirmRequire))
	require.Equal(t, 1, countEvent(frames, eventGoalUpdated))

	record := map[string]any{
		"goal_id": "abc123def456", "session_id": "s1", "objective": "修完测试",
		"status": "active", "created_at": "2026-11-09T00:00:00+00:00",
		"updated_at": "2026-11-09T00:00:00+00:00",
		"revision":   float64(0), "attempt_count": float64(1), "time_used_seconds": float64(3),
		"token_usage": map[string]any{
			"input_tokens": float64(10), "output_tokens": float64(5),
			"cached_input_tokens": float64(0), "total_tokens": float64(15),
		},
		"max_attempts": nil, "token_budget": nil,
		"last_assessment": nil, "last_stop_reason": nil, "active_started_at": nil,
	}
	requireGoalRecord(t, record)

	// Go 直构 map 用 int 也必须通过（requireNumeric 兼容两种来源）
	record["revision"] = 2
	requireGoalRecord(t, record)
}
