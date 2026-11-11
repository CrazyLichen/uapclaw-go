package adapter

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// isolateGoalHistoryDataDir 把数据根指向临时目录并在收尾先 flush 队列再删目录。
// 与 interface_history_test.go 的既有隔离方式一致。
func isolateGoalHistoryDataDir(t *testing.T) {
	t.Helper()
	orig, had := os.LookupEnv(pathutil.EnvDataDir)
	dataDir := t.TempDir()
	t.Cleanup(func() {
		sessionpkg.FlushMetadataQueue(5.0)
		sessionpkg.FlushHistoryQueue(5.0)
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

// goalHistoryRecords 读取会话历史（先 flush 异步队列，避免假空）。
func goalHistoryRecords(t *testing.T, sessionID string) []map[string]any {
	t.Helper()
	sessionpkg.FlushHistoryQueue(5.0)
	records, err := sessionpkg.ReadHistoryRecords(sessionID)
	require.NoError(t, err)
	return records
}

// goalHistoryRequest 构造带 mode 的请求。
func goalHistoryRequest(t *testing.T, sessionID, requestID string) *schema.AgentRequest {
	t.Helper()
	sid := sessionID
	return &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    mustJSON(t, map[string]any{"mode": "agent"}),
	}
}

// ──────────────────────────── 测试 ────────────────────────────

// TestRecordGoalSetHistory_空闲时立刻落盘 objective 作为 user 轮写入。
// Python: interface_deep.py:17610-17654
func TestRecordGoalSetHistory_空闲时立刻落盘(t *testing.T) {
	isolateGoalHistoryDataDir(t)
	d := &DeepAdapter{activeSessionIDs: make(map[string]int)}
	req := goalHistoryRequest(t, "sess-hist-idle", "req-h1")

	d.recordGoalSetHistoryIfNeeded(req, "set", goalResultStream,
		map[string]any{"goal_id": "g1", "objective": "修完测试"}, nil)

	records := goalHistoryRecords(t, "sess-hist-idle")
	require.Len(t, records, 1)
	require.Equal(t, "user", records[0]["role"])
	require.Equal(t, "修完测试", records[0]["content"])
	// extra 字段被平铺进记录（AppendHistoryRecord 的行为），不是嵌套在 "extra" 下
	require.Equal(t, true, records[0]["is_goal_objective_message"])
	require.Equal(t, "g1", records[0]["goal_id"])
}

// TestRecordGoalSetHistory_忙碌时挂起待轮边界flush 不能打断上一轮时间线。
// Python: interface_deep.py:17584-17608
func TestRecordGoalSetHistory_忙碌时挂起待轮边界flush(t *testing.T) {
	isolateGoalHistoryDataDir(t)
	d := &DeepAdapter{activeSessionIDs: make(map[string]int)}
	d.activeSessionIDs["sess-hist-busy"] = 2 // 同会话还有别的请求在跑
	req := goalHistoryRequest(t, "sess-hist-busy", "req-h2")

	d.recordGoalSetHistoryIfNeeded(req, "set", goalResultStream,
		map[string]any{"goal_id": "g2", "objective": "忙碌目标"}, nil)
	require.Empty(t, goalHistoryRecords(t, "sess-hist-busy"), "忙碌时不得立刻落盘")
	require.NotNil(t, d.pendingGoalObjective("sess-hist-busy"))

	d.flushPendingGoalObjectiveHistory("sess-hist-busy")
	records := goalHistoryRecords(t, "sess-hist-busy")
	require.Len(t, records, 1)
	require.Equal(t, "忙碌目标", records[0]["content"])
	require.Nil(t, d.pendingGoalObjective("sess-hist-busy"), "flush 后必须清空挂起表")
}

// TestRecordGoalSetHistory_非set或失败不写 只有成功的 set 才写。
// Python: interface_deep.py:17619-17625
func TestRecordGoalSetHistory_非set或失败不写(t *testing.T) {
	isolateGoalHistoryDataDir(t)
	d := &DeepAdapter{activeSessionIDs: make(map[string]int)}
	req := goalHistoryRequest(t, "sess-hist-skip", "req-h3")

	d.recordGoalSetHistoryIfNeeded(req, "pause", goalResultControl, nil, nil)
	d.recordGoalSetHistoryIfNeeded(req, "set", goalResultError, nil, nil)
	d.recordGoalSetHistoryIfNeeded(req, "set", goalResultConfirmRequired, nil, nil)
	d.recordGoalSetHistoryIfNeeded(req, "set", goalResultStream, map[string]any{"objective": ""}, nil)
	require.Empty(t, goalHistoryRecords(t, "sess-hist-skip"))
	require.Nil(t, d.pendingGoalObjective("sess-hist-skip"))
}

// TestRecordGoalCompletedHistory_只写一次 同一 goal_id 重复到达不重复落盘。
// Python: interface_deep.py:17677-17723
func TestRecordGoalCompletedHistory_只写一次(t *testing.T) {
	isolateGoalHistoryDataDir(t)
	d := &DeepAdapter{}
	payload := map[string]any{
		"goal_id": "g9", "status": "completed",
		"last_assessment": map[string]any{"evidence": "全部用例通过"},
	}
	d.recordGoalCompletedHistoryIfNeeded("sess-completed", "web", nil, "agent", payload)
	d.recordGoalCompletedHistoryIfNeeded("sess-completed", "web", nil, "agent", payload)

	records := goalHistoryRecords(t, "sess-completed")
	require.Len(t, records, 1, "幂等：同一 goal_id 只写一次")
	require.Equal(t, `goal.completed:{"evidence":"全部用例通过"}`, records[0]["content"])
	require.Equal(t, "goal-completed-g9", records[0]["id"])
}

// TestRecordGoalCompletedHistory_非completed不写 状态门卫。
// Python: interface_deep.py:17677（status 必须是 completed）
func TestRecordGoalCompletedHistory_非completed不写(t *testing.T) {
	isolateGoalHistoryDataDir(t)
	d := &DeepAdapter{}
	d.recordGoalCompletedHistoryIfNeeded("sess-completed-no", "web", nil, "agent",
		map[string]any{"goal_id": "g10", "status": "active"})
	d.recordGoalCompletedHistoryIfNeeded("sess-completed-no", "web", nil, "agent", nil)
	d.recordGoalCompletedHistoryIfNeeded("sess-completed-no", "web", nil, "agent",
		map[string]any{"goal_id": "", "status": "completed"})
	require.Empty(t, goalHistoryRecords(t, "sess-completed-no"))
}

// TestBeginVisibleChatContent_user转goal补空final 不补的话两个答案糊一个气泡。
// Python: interface_deep.py:15574-15617
func TestBeginVisibleChatContent_user转goal补空final(t *testing.T) {
	d := &DeepAdapter{activeSessionIDs: make(map[string]int)}
	req := goalHistoryRequest(t, "sess-split", "req-split")

	// 第一块：user（直接置闩等价于首块采样）
	d.streamRoundKindLatch = "user"
	require.Nil(t, d.beginVisibleChatContent(req, false))
	require.Equal(t, "user", d.streamContentRunKind)

	// 切到 goal → 应补一个空 chat.final
	d.streamRoundKindLatch = "goal"
	boundary := d.beginVisibleChatContent(req, false)
	require.NotNil(t, boundary)
	require.Equal(t, "chat.final", boundary["event_type"])
	require.Equal(t, "", boundary["content"])
	require.Equal(t, "goal", d.streamContentRunKind)
}

// TestBeginVisibleChatContent_纯goal流首块不补final 严格 prev=="user" 规则。
func TestBeginVisibleChatContent_纯goal流首块不补final(t *testing.T) {
	d := &DeepAdapter{activeSessionIDs: make(map[string]int)}
	req := goalHistoryRequest(t, "sess-split-pure", "req-pure")
	d.streamRoundKindLatch = "goal"
	require.Nil(t, d.beginVisibleChatContent(req, false),
		"非用户来源的流保留严格 prev==user 规则")
}

// TestBeginVisibleChatContent_用户流被goal劫持也补final 慢首token + 早到的 goal 插入。
// Python: interface_deep.py:15594-15605（user_originated 分支）
func TestBeginVisibleChatContent_用户流被goal劫持也补final(t *testing.T) {
	d := &DeepAdapter{activeSessionIDs: make(map[string]int)}
	req := goalHistoryRequest(t, "sess-hijack", "req-hijack")
	d.streamRoundKindLatch = "goal"
	boundary := d.beginVisibleChatContent(req, true)
	require.NotNil(t, boundary, "用户来源的流被 goal 劫持时必须补分气泡 final")
	require.Equal(t, "chat.final", boundary["event_type"])
}

// TestConsumedRoundRunKind 优先取闩，其次回落 live active_round。
// Python: interface_deep.py:15510-15521
func TestConsumedRoundRunKind(t *testing.T) {
	d := &DeepAdapter{}
	require.Equal(t, "", d.consumedRoundRunKind())
	d.streamRoundKindLatch = "goal"
	require.Equal(t, "goal", d.consumedRoundRunKind())
}

// 确保 context 导入被使用（历史函数不需要 ctx，但测试文件保留统一风格）。
var _ = context.Background
