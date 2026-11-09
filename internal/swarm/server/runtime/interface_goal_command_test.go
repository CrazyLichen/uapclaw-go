package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestProcessGoalCommand_无目标返回控制响应 会话已启动但尚无目标时的只读查询。
// Python: interface_deep.py:17822-17829（peek 为 nil → 控制类结果 + 逐字文案）
//
// 注：start_interaction 接线后（Task 9），会话级 child 会自动启动交互循环，
// 因此 goal_manager_not_started 只出现在真正未初始化的路径上。
func TestProcessGoalCommand_无目标返回控制响应(t *testing.T) {
	uc := NewUapClaw()
	params, _ := json.Marshal(map[string]any{"action": "get", "mode": "agent"})
	sid := "sess-goal-cmd"
	req := &schema.AgentRequest{
		RequestID: "req-gc-1", ChannelID: "web", SessionID: &sid, Params: params,
		ReqMethod: schema.ReqMethodCommandGoal,
	}
	resp, err := uc.processGoalCommand(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.OK, "已启动的会话查询空目标应成功返回")
	require.Equal(t, "get", resp.Payload["action"])
	require.Equal(t, "No goal in this session.", resp.Payload["message"])
	require.Nil(t, resp.Payload["goal"])
	require.Nil(t, resp.Payload["code"])
	require.Nil(t, resp.Payload["error"])
}

// TestProcessGoalCommandStream_非setresume单帧 流式 get 只回一帧即结束。
// Python: interface.py:4175-4235
func TestProcessGoalCommandStream_非setresume单帧(t *testing.T) {
	uc := NewUapClaw()
	params, _ := json.Marshal(map[string]any{"action": "pause", "mode": "agent"})
	sid := "sess-goal-cmd-stream"
	req := &schema.AgentRequest{
		RequestID: "req-gc-2", ChannelID: "web", SessionID: &sid, Params: params,
		ReqMethod: schema.ReqMethodCommandGoal, IsStream: true,
	}
	chunks, err := uc.processGoalCommandStream(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.True(t, chunks[0].IsComplete)
	// pause 无目标 → goal_error → 单帧 chat.error 带 no_goal
	require.Equal(t, "chat.error", chunks[0].Payload["event_type"])
	require.Equal(t, "no_goal", chunks[0].Payload["code"])
	require.Equal(t, "No goal in this session; cannot pause.", chunks[0].Payload["error"])
}

// TestGoalControlResponse_成功与失败双写 payload message 与 error 保持兼容。
// Python: interface.py:3460-3485
func TestGoalControlResponse_成功与失败双写(t *testing.T) {
	params, _ := json.Marshal(map[string]any{"action": "get"})
	sid := "sess-resp"
	req := &schema.AgentRequest{RequestID: "r", ChannelID: "web", SessionID: &sid, Params: params}

	ok := goalControlResponse(req, "get", map[string]any{
		"result_type": "goal_control", "action": "get",
		"goal": map[string]any{"objective": "x"}, "output": "Goal: x",
	})
	require.True(t, ok.OK)
	require.Equal(t, "Goal: x", ok.Payload["message"])
	require.Equal(t, "x", ok.Payload["record"].(map[string]any)["objective"])
	require.Nil(t, ok.Payload["error"])

	bad := goalControlResponse(req, "pause", map[string]any{
		"result_type": "goal_error", "action": "pause",
		"error_code": "no_goal", "error": "No goal in this session; cannot pause.",
	})
	require.False(t, bad.OK)
	require.Equal(t, "no_goal", bad.Payload["code"])
	require.Equal(t, "No goal in this session; cannot pause.", bad.Payload["error"])
	require.Equal(t, "No goal in this session; cannot pause.", bad.Payload["message"])
}
