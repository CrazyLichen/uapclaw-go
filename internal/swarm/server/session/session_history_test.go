package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// resetWorkspaceCache 重置 workspace 路径缓存，确保 t.Setenv 生效。
func resetHistoryWorkspaceCache() {
	path.ResetCache()
}

func TestAppendHistoryRecord_基本写入(t *testing.T) {
	sessionID := "test-session-basic"
	requestID := "req-001"
	channelID := "web"

	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	AppendHistoryRecord(sessionID, requestID, channelID, "user", "你好", float64(time.Now().UnixMilli())/1000, "", nil, nil, "")

	// 等待异步写入完成
	time.Sleep(200 * time.Millisecond)

	records, err := ReadHistoryRecords(sessionID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "req-001:user", records[0]["id"])
	assert.Equal(t, "user", records[0]["role"])
	assert.Equal(t, "你好", records[0]["content"])
	assert.Equal(t, requestID, records[0]["request_id"])
	assert.Equal(t, channelID, records[0]["channel_id"])
}

func TestAppendHistoryRecord_role归一化(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	AppendHistoryRecord("sess-1", "r1", "web", "assistant", "回复", 1.0, "", nil, nil, "")
	time.Sleep(200 * time.Millisecond)

	AppendHistoryRecord("sess-1", "r2", "web", "system", "系统消息", 2.0, "", nil, nil, "")
	time.Sleep(200 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-1")
	require.Len(t, records, 2)
	assert.Equal(t, "assistant", records[0]["role"]) // assistant 保持
	assert.Equal(t, "user", records[1]["role"])      // system → user 归一化
}

func TestAppendHistoryRecord_eventType仅assistant写入(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	AppendHistoryRecord("sess-2", "r1", "web", "user", "问题", 1.0, "", nil, nil, "")
	time.Sleep(200 * time.Millisecond)

	AppendHistoryRecord("sess-2", "r2", "web", "assistant", "回答", 2.0, "chat.final", nil, nil, "")
	time.Sleep(200 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-2")
	require.Len(t, records, 2)
	_, hasEventType := records[0]["event_type"]
	assert.False(t, hasEventType)
	assert.Equal(t, "chat.final", records[1]["event_type"])
}

func TestAppendHistoryRecord_extra字段展开(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	extra := map[string]any{"tool_result": map[string]any{"name": "search"}}
	AppendHistoryRecord("sess-3", "r1", "web", "assistant", "工具结果", 1.0, "chat.tool_result", extra, nil, "")
	time.Sleep(200 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-3")
	require.Len(t, records, 1)
	assert.Equal(t, "chat.tool_result", records[0]["event_type"])
	_, hasToolResult := records[0]["tool_result"]
	assert.True(t, hasToolResult)
}

func TestAppendHistoryRecord_mode字段(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	AppendHistoryRecord("sess-4", "r1", "web", "assistant", "回答", 1.0, "chat.final", nil, nil, "team")
	time.Sleep(200 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-4")
	require.Len(t, records, 1)
	assert.Equal(t, "team", records[0]["mode"])
}

func TestTruncateHistoryRecords(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	AppendHistoryRecord("sess-5", "r1", "web", "user", "第一条", 1.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)
	AppendHistoryRecord("sess-5", "r2", "web", "user", "第二条", 2.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)
	AppendHistoryRecord("sess-5", "r3", "web", "user", "第三条", 3.0, "", nil, nil, "")
	time.Sleep(200 * time.Millisecond)

	// 截断到索引 2（保留前两条）
	result, err := TruncateHistoryRecords("sess-5", 2)
	require.NoError(t, err)
	assert.Equal(t, 2, result.RemainingRecords)
	assert.Equal(t, 1, result.RemovedRecords)

	records, _ := ReadHistoryRecords("sess-5")
	assert.Len(t, records, 2)
	assert.Equal(t, "第一条", records[0]["content"])
	assert.Equal(t, "第二条", records[1]["content"])
}

func TestTruncateHistoryRecords_边界值(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	AppendHistoryRecord("sess-bound", "r1", "web", "user", "内容", 1.0, "", nil, nil, "")
	time.Sleep(200 * time.Millisecond)

	// cutIndex=0 → 清空所有记录
	result, err := TruncateHistoryRecords("sess-bound", 0)
	require.NoError(t, err)
	assert.Equal(t, 0, result.RemainingRecords)
	assert.Equal(t, 1, result.RemovedRecords)
}

func TestHistoryFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	AppendHistoryRecord("my-session", "r1", "web", "user", "test", 1.0, "", nil, nil, "")
	time.Sleep(200 * time.Millisecond)

	expectedPath := filepath.Join(tmpDir, "agent", "sessions", "my-session", "history.json")
	_, err := os.Stat(expectedPath)
	require.NoError(t, err, "history.json 应该存在于正确的路径")

	data, _ := os.ReadFile(expectedPath)
	var records []map[string]any
	err = json.Unmarshal(data, &records)
	require.NoError(t, err)
}

func TestIsTeamRelevant_teamMessage始终保留(t *testing.T) {
	item := map[string]any{"event_type": "team.message"}
	assert.True(t, IsTeamRelevant(item))
}

func TestIsTeamRelevant_toolCall仅TeamMode(t *testing.T) {
	teamItem := map[string]any{"event_type": "chat.tool_call", "mode": "team"}
	assert.True(t, IsTeamRelevant(teamItem))

	nonTeamItem := map[string]any{"event_type": "chat.tool_call", "mode": "agent"}
	assert.False(t, IsTeamRelevant(nonTeamItem))
}

func TestIsTeamRelevant_final仅TeammateRole(t *testing.T) {
	teammateItem := map[string]any{"event_type": "chat.final", "role": "teammate"}
	assert.True(t, IsTeamRelevant(teammateItem))

	assistantItem := map[string]any{"event_type": "chat.final", "role": "assistant"}
	assert.False(t, IsTeamRelevant(assistantItem))
}

func TestIsTeamRelevant_不相关事件(t *testing.T) {
	item := map[string]any{"event_type": "chat.error"}
	assert.False(t, IsTeamRelevant(item))
}

func TestReadTeamHistoryRecords_过滤team记录(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetHistoryWorkspaceCache()

	AppendHistoryRecord("sess-team", "r1", "web", "user", "用户消息", 1.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)
	AppendHistoryRecord("sess-team", "r2", "web", "assistant", "团队消息", 2.0, "team.message", nil, nil, "team")
	time.Sleep(100 * time.Millisecond)
	AppendHistoryRecord("sess-team", "r3", "web", "assistant", "普通回复", 3.0, "chat.final", nil, nil, "agent")
	time.Sleep(200 * time.Millisecond)

	records, err := ReadTeamHistoryRecords("sess-team")
	require.NoError(t, err)
	// 仅 team.message 应保留
	require.Len(t, records, 1)
	assert.Equal(t, "team.message", records[0]["event_type"])
}

// TestAppendCompactHistoryRecords_基本写入 验证 boundary + summary 记录
func TestAppendCompactHistoryRecords_基本写入(t *testing.T) {
	resetHistoryWorkspaceCache()
	ClearAllSessionMetadataCache()
	FlushMetadataQueue()

	sessionsDir := GetSessionsDir()
	sid := MakeSessionID()
	os.MkdirAll(filepath.Join(sessionsDir, sid), 0o755)

	AppendCompactHistoryRecords(sid, "r1", "web", "压缩摘要", float64(time.Now().UnixMilli())/1000, "manual", map[string]any{"original_count": 5, "compressed_count": 2}, "agent")
	time.Sleep(300 * time.Millisecond)

	records, err := ReadHistoryRecords(sid)
	require.NoError(t, err)
	// 应写入 2 条: boundary + summary
	require.Len(t, records, 2)
	assert.Equal(t, "context.compact_boundary", records[0]["event_type"])
	assert.Equal(t, "context.compact_summary", records[1]["event_type"])
}

// TestAppendCompactHistoryFromPayload_从payload写入 验证从 payload 提取写入
func TestAppendCompactHistoryFromPayload_从payload写入(t *testing.T) {
	resetHistoryWorkspaceCache()
	ClearAllSessionMetadataCache()
	FlushMetadataQueue()

	sessionsDir := GetSessionsDir()
	os.MkdirAll(filepath.Join(sessionsDir, "sess-compact-test2"), 0o755)

	// 对齐 Python payload 结构：需要 compact_summary 和 status=success
	payload := map[string]any{
		"event_type":      "context_compression_state",
		"compact_summary": "自动压缩结果",
		"status":          "success",
		"stats": map[string]any{
			"original_count": 10,
			"compressed_count": 3,
		},
	}
	AppendCompactHistoryFromPayload(payload, "sess-compact-test2", "r2", "web", "agent")
	time.Sleep(300 * time.Millisecond)

	records, err := ReadHistoryRecords("sess-compact-test2")
	require.NoError(t, err)
	// 应写入 compact 记录
	assert.GreaterOrEqual(t, len(records), 1)
}
