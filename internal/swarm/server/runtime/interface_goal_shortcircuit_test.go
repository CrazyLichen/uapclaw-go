package runtime

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// isolateDataDir 把数据根指向临时目录，并在收尾时先 flush 异步队列再删目录。
// 与 interface_history_test.go 的既有隔离方式一致（workspace.SetUserHome 不足以
// 覆盖 AgentSessionsDir 的解析结果）。
func isolateDataDir(t *testing.T) {
	t.Helper()
	orig, had := os.LookupEnv(pathutil.EnvDataDir)
	dataDir := t.TempDir()
	t.Cleanup(func() {
		// LIFO：先 flush，后恢复环境与清缓存，避免异步队列写到已删除目录
		sessionpkg.FlushMetadataQueue()
		sessionpkg.FlushHistoryQueue()
		if had {
			_ = os.Setenv(pathutil.EnvDataDir, orig)
		} else {
			_ = os.Unsetenv(pathutil.EnvDataDir)
		}
		pathutil.ResetCache()
	})
	require.NoError(t, os.Setenv(pathutil.EnvDataDir, dataDir))
	pathutil.ResetCache()
}

// historyRecords 读取会话历史（先 flush 异步队列，避免假空）。
func historyRecords(t *testing.T, sessionID string) []map[string]any {
	t.Helper()
	sessionpkg.FlushHistoryQueue()
	records, err := sessionpkg.ReadHistoryRecords(sessionID)
	require.NoError(t, err)
	return records
}

// TestRunMessageDirect_CommandGoal不写user历史 门面入口必须在写历史与调模型之前短路。
// Python: interface.py:3408（COMMAND_GOAL 在 stateless/skills 判定之前单独处理）
func TestRunMessageDirect_CommandGoal不写user历史(t *testing.T) {
	isolateDataDir(t)
	uc := NewUapClaw()
	params, _ := json.Marshal(map[string]any{"action": "get", "mode": "agent"})
	sid := "sess-goal-shortcircuit"
	req := &schema.AgentRequest{
		RequestID: "req-sc-1", ChannelID: "web", SessionID: &sid, Params: params,
		ReqMethod: schema.ReqMethodCommandGoal,
	}
	resp, err := uc.runMessageDirect(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "goal_manager_not_started", resp.Payload["code"])
	require.Empty(t, historyRecords(t, sid), "只读 goal 查询不得写入任何历史")
}

// TestProcessMessageStreamInternal_CommandGoal不写user历史 流式入口同样短路。
// Python: interface.py:4205 `request.req_method != ReqMethod.COMMAND_GOAL` 才写历史
func TestProcessMessageStreamInternal_CommandGoal不写user历史(t *testing.T) {
	isolateDataDir(t)
	uc := NewUapClaw()
	params, _ := json.Marshal(map[string]any{"action": "get", "mode": "agent"})
	sid := "sess-goal-shortcircuit-stream"
	req := &schema.AgentRequest{
		RequestID: "req-sc-2", ChannelID: "web", SessionID: &sid, Params: params,
		ReqMethod: schema.ReqMethodCommandGoal, IsStream: true,
	}
	ch, err := uc.processMessageStreamInternal(context.Background(), req, false)
	require.NoError(t, err)
	var chunks int
	for range ch {
		chunks++
	}
	require.Equal(t, 1, chunks)
	require.Empty(t, historyRecords(t, sid), "流式只读 goal 查询不得写入任何历史")
}

// TestProcessMessageStreamInternal_CommandGoalSet不短路 set/resume 必须继续走
// 完整流式链（适配器的 attach→control 两段映射），不得在门面被单帧截断。
// Python: interface.py:4180 `if action not in {"set", "resume"}`
func TestProcessMessageStreamInternal_CommandGoalSet不短路(t *testing.T) {
	isolateDataDir(t)
	uc := NewUapClaw()
	params, _ := json.Marshal(map[string]any{
		"action": "set", "objective": "修完测试", "mode": "agent",
	})
	sid := "sess-goal-set-passthrough"
	req := &schema.AgentRequest{
		RequestID: "req-sc-3", ChannelID: "web", SessionID: &sid, Params: params,
		ReqMethod: schema.ReqMethodCommandGoal, IsStream: true,
	}
	ch, err := uc.processMessageStreamInternal(context.Background(), req, false)
	require.NoError(t, err)
	// 走到适配器流式链后，没有 goal manager 时应回 chat.error（而不是 goal.snapshot 单帧）
	var sawError bool
	for chunk := range ch {
		if eventType, _ := chunk.Payload["event_type"].(string); eventType == "chat.error" {
			sawError = true
		}
	}
	require.True(t, sawError, "set 必须进入适配器流式链并由其报错，而不是门面单帧短路")
}
