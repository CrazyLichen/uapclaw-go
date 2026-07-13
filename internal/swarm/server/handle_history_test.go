package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestIsRestorableHistoryRecord 验证可恢复历史记录判断逻辑，对齐 Python _is_restorable_history_record。
func TestIsRestorableHistoryRecord(t *testing.T) {
	tests := []struct {
		name   string
		record map[string]any
		want   bool
	}{
		{
			name:   "user 有内容",
			record: map[string]any{"role": "user", "content": "hello"},
			want:   true,
		},
		{
			name:   "user 无内容",
			record: map[string]any{"role": "user", "content": ""},
			want:   false,
		},
		{
			name:   "user 内容仅空白",
			record: map[string]any{"role": "user", "content": "  "},
			want:   false,
		},
		{
			name:   "assistant 有可恢复 event_type",
			record: map[string]any{"role": "assistant", "event_type": "chat.final", "content": "done"},
			want:   true,
		},
		{
			name:   "assistant 有不可恢复 event_type",
			record: map[string]any{"role": "assistant", "event_type": "chat.thinking", "content": "..."},
			want:   false,
		},
		{
			name:   "assistant 无 event_type 有内容",
			record: map[string]any{"role": "assistant", "content": "response"},
			want:   true,
		},
		{
			name:   "assistant 无 event_type 无内容",
			record: map[string]any{"role": "assistant", "content": ""},
			want:   false,
		},
		{
			name:   "tool_call 可恢复",
			record: map[string]any{"event_type": "chat.tool_call"},
			want:   true,
		},
		{
			name:   "compact_boundary 可恢复",
			record: map[string]any{"event_type": "context.compact_boundary"},
			want:   true,
		},
		{
			name:   "空记录",
			record: map[string]any{},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRestorableHistoryRecord(tt.record)
			if got != tt.want {
				t.Errorf("isRestorableHistoryRecord() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHandleHistoryGet_无SessionID 验证 history.get 无 session_id 时返回空历史。
func TestHandleHistoryGet_无SessionID(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHistoryGet, json.RawMessage(`{}`))

	resp, err := s.handleHistoryGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHistoryGet 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if messages, ok := resp.Payload["messages"]; !ok {
		t.Error("payload 应包含 messages")
	} else if arr, ok := messages.([]any); !ok || len(arr) != 0 {
		t.Errorf("payload.messages 应为空数组, 实际: %v", messages)
	}
}

// TestHandleHistoryGet_不存在的Session 验证 history.get 不存在的 session 返回空。
func TestHandleHistoryGet_不存在的Session(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHistoryGet,
		json.RawMessage(`{"session_id": "nonexistent-session"}`))

	resp, err := s.handleHistoryGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHistoryGet 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if totalPages, ok := resp.Payload["total_pages"]; !ok || totalPages != 0 {
		t.Errorf("payload.total_pages 应为 0, 实际: %v", totalPages)
	}
}

// setupHistoryTest 设置历史测试环境：创建临时目录、会话目录、写入 history.json，重置路径缓存。
func setupHistoryTest(t *testing.T, sessionID string, history []any) string {
	t.Helper()
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "agent", "sessions")
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建会话目录失败: %v", err)
	}

	historyJSON, _ := json.Marshal(history)
	if err := os.WriteFile(filepath.Join(sessionDir, "history.json"), historyJSON, 0o644); err != nil {
		t.Fatalf("写入 history.json 失败: %v", err)
	}

	// 设置 UAPCLAW_DATA_DIR 并重置路径缓存
	t.Setenv(pathutil.EnvDataDir, tmpDir)
	pathutil.ResetCache()

	return tmpDir
}

// TestHandleHistoryGet_有历史数据 验证 history.get 正确过滤、倒序、分页。
func TestHandleHistoryGet_有历史数据(t *testing.T) {
	// 构造 history.json：包含可恢复和不可恢复记录
	history := []any{
		map[string]any{"role": "user", "content": "hello"},
		map[string]any{"role": "assistant", "event_type": "chat.thinking", "content": "..."}, // 不可恢复
		map[string]any{"role": "assistant", "event_type": "chat.final", "content": "hi there"},
		map[string]any{"role": "user", "content": "how are you"},
		map[string]any{"role": "assistant", "event_type": "chat.final", "content": "fine"},
	}
	setupHistoryTest(t, "test-session", history)

	data := getConversationHistory("test-session", 1)
	if data == nil {
		t.Fatal("getConversationHistory 应返回数据，不应为 nil")
	}

	messages, _ := data["messages"].([]any)
	totalPages, _ := data["total_pages"].(int)
	pageIdx, _ := data["page_idx"].(int)

	// 可恢复记录：user(hello), chat.final(hi there), user(how are you), chat.final(fine) = 4 条
	// 倒序后：chat.final(fine), user(how are you), chat.final(hi there), user(hello)
	if len(messages) != 4 {
		t.Errorf("messages 长度应为 4, 实际: %d", len(messages))
	}
	if totalPages != 1 {
		t.Errorf("total_pages 应为 1, 实际: %d", totalPages)
	}
	if pageIdx != 1 {
		t.Errorf("page_idx 应为 1, 实际: %d", pageIdx)
	}

	// 验证倒序：第一条应为最后一条可恢复记录
	if len(messages) > 0 {
		firstMsg, _ := messages[0].(map[string]any)
		if content, _ := firstMsg["content"].(string); content != "fine" {
			t.Errorf("第一条消息 content 应为 'fine', 实际: %s", content)
		}
	}
}

// TestHandleHistoryGet_分页 验证分页逻辑，对齐 Python math.ceil 计算 total_pages。
func TestHandleHistoryGet_分页(t *testing.T) {
	// 构造 25 条可恢复记录
	history := make([]any, 25)
	for i := 0; i < 25; i++ {
		history[i] = map[string]any{"role": "user", "content": "msg"}
	}
	setupHistoryTest(t, "page-session", history)

	// page_idx=1，应返回 20 条
	data := getConversationHistory("page-session", 1)
	if data == nil {
		t.Fatal("page 1 应返回数据")
	}
	messages, _ := data["messages"].([]any)
	totalPages, _ := data["total_pages"].(int)
	if len(messages) != 20 {
		t.Errorf("page 1 messages 长度应为 20, 实际: %d", len(messages))
	}
	// total_pages = ceil(25/20) = 2
	if totalPages != 2 {
		t.Errorf("total_pages 应为 2, 实际: %d", totalPages)
	}

	// page_idx=2，应返回 5 条
	data2 := getConversationHistory("page-session", 2)
	if data2 == nil {
		t.Fatal("page 2 应返回数据")
	}
	messages2, _ := data2["messages"].([]any)
	if len(messages2) != 5 {
		t.Errorf("page 2 messages 长度应为 5, 实际: %d", len(messages2))
	}

	// page_idx=3，超过 total_pages，应返回 nil
	data3 := getConversationHistory("page-session", 3)
	if data3 != nil {
		t.Error("page 3 超过 total_pages，应返回 nil")
	}
}

// TestHandleHistoryGetStream_基本 验证流式版本返回正确 chunk 序列。
func TestHandleHistoryGetStream_基本(t *testing.T) {
	s, _ := newTestServer()

	// 无 session_id，应返回错误 chunk
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHistoryGet, json.RawMessage(`{}`))

	chunks, err := s.handleHistoryGetStream(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHistoryGetStream 返回错误: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("应返回 1 个错误 chunk, 实际: %d", len(chunks))
	}
	if !chunks[0].IsComplete {
		t.Error("错误 chunk 的 IsComplete 应为 true")
	}
	eventType, _ := chunks[0].Payload["event_type"].(string)
	if eventType != "chat.error" {
		t.Errorf("错误 chunk event_type 应为 chat.error, 实际: %s", eventType)
	}
}

// TestGetConversationHistory_参数校验 验证参数校验逻辑，对齐 Python get_conversation_history。
func TestGetConversationHistory_参数校验(t *testing.T) {
	// 空 session_id
	if data := getConversationHistory("", 1); data != nil {
		t.Error("空 session_id 应返回 nil")
	}
	// 仅空白 session_id
	if data := getConversationHistory("   ", 1); data != nil {
		t.Error("仅空白 session_id 应返回 nil")
	}
	// page_idx <= 0
	if data := getConversationHistory("some-session", 0); data != nil {
		t.Error("page_idx=0 应返回 nil")
	}
	if data := getConversationHistory("some-session", -1); data != nil {
		t.Error("page_idx=-1 应返回 nil")
	}
}
