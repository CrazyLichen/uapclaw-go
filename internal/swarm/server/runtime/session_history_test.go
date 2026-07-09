package runtime

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
func resetWorkspaceCache() {
	path.ResetCache()
}

func TestAppendHistoryRecord_基本写入(t *testing.T) {
	sessionID := "test-session-basic"
	requestID := "req-001"
	channelID := "web"

	// 使用临时目录覆盖 sessions 目录
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	AppendHistoryRecord(sessionID, requestID, channelID, "user", "你好", float64(time.Now().UnixMilli())/1000, "", nil, nil, "")

	// 等待异步写入完成（简短等待）
	time.Sleep(100 * time.Millisecond)

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
	resetWorkspaceCache()

	AppendHistoryRecord("sess-1", "r1", "web", "assistant", "回复", 1.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	AppendHistoryRecord("sess-1", "r2", "web", "system", "系统消息", 2.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-1")
	require.Len(t, records, 2)
	assert.Equal(t, "assistant", records[0]["role"]) // assistant 保持
	assert.Equal(t, "user", records[1]["role"])      // system → user 归一化
}

func TestAppendHistoryRecord_eventType仅assistant写入(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	AppendHistoryRecord("sess-2", "r1", "web", "user", "问题", 1.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	AppendHistoryRecord("sess-2", "r2", "web", "assistant", "回答", 2.0, "chat.final", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-2")
	require.Len(t, records, 2)
	// user 记录不应有 event_type
	_, hasEventType := records[0]["event_type"]
	assert.False(t, hasEventType)
	// assistant 记录应有 event_type
	assert.Equal(t, "chat.final", records[1]["event_type"])
}

func TestAppendHistoryRecord_extra字段展开(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	extra := map[string]any{"tool_result": map[string]any{"name": "search"}}
	AppendHistoryRecord("sess-3", "r1", "web", "assistant", "工具结果", 1.0, "chat.tool_result", extra, nil, "")
	time.Sleep(100 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-3")
	require.Len(t, records, 1)
	assert.Equal(t, "chat.tool_result", records[0]["event_type"])
	// extra 展开到顶层
	_, hasToolResult := records[0]["tool_result"]
	assert.True(t, hasToolResult)
}

func TestAppendHistoryRecord_mode字段(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	AppendHistoryRecord("sess-4", "r1", "web", "assistant", "回答", 1.0, "chat.final", nil, nil, "team")
	time.Sleep(100 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-4")
	require.Len(t, records, 1)
	assert.Equal(t, "team", records[0]["mode"])
}

func TestTruncateHistoryRecords(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	AppendHistoryRecord("sess-5", "r1", "web", "user", "第一条", 1.0, "", nil, nil, "")
	time.Sleep(50 * time.Millisecond)
	AppendHistoryRecord("sess-5", "r2", "web", "user", "第二条", 2.0, "", nil, nil, "")
	time.Sleep(50 * time.Millisecond)
	AppendHistoryRecord("sess-5", "r3", "web", "user", "第三条", 3.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	// 截断到 r2
	err := TruncateHistoryRecords("sess-5", "r2")
	require.NoError(t, err)

	records, _ := ReadHistoryRecords("sess-5")
	assert.Len(t, records, 2)
	assert.Equal(t, "第一条", records[0]["content"])
	assert.Equal(t, "第二条", records[1]["content"])
}

func TestHistoryFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	AppendHistoryRecord("my-session", "r1", "web", "user", "test", 1.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	// 验证文件存在
	expectedPath := filepath.Join(tmpDir, "agent", "sessions", "my-session", "history.json")
	_, err := os.Stat(expectedPath)
	require.NoError(t, err, "history.json 应该存在于正确的路径")

	// 验证是合法 JSON 数组
	data, _ := os.ReadFile(expectedPath)
	var records []map[string]any
	err = json.Unmarshal(data, &records)
	require.NoError(t, err)
}
