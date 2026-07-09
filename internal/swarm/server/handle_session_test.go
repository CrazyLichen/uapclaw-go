package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupTestSessionsDir 设置测试用的 sessions 目录，返回（sessionsDir, 设置好 sessionsDir 的 AgentServer, cleanup）。
func setupTestSessionsDir(t *testing.T) (sessionsDir string, s *AgentServer, cleanup func()) {
	t.Helper()
	tmpDir := t.TempDir()
	sessionsDir = filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("创建 sessions 目录失败: %v", err)
	}
	server, _ := newTestServer()
	server.SetSessionsDir(sessionsDir)
	cleanup = func() {}
	return sessionsDir, server, cleanup
}

// writeTestMetadata 写入测试用 metadata.json。
func writeTestMetadata(t *testing.T, sessionsDir, sessionID string, meta map[string]any) {
	t.Helper()
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建会话目录失败: %v", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("序列化 metadata 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644); err != nil {
		t.Fatalf("写入 metadata.json 失败: %v", err)
	}
}

// toSessionSlice 将 payload["sessions"] 转为 []map[string]any，处理 Go 泛型切片类型断言。
func toSessionSlice(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	raw, ok := payload["sessions"]
	if !ok {
		t.Fatal("payload 中缺少 sessions 键")
	}
	// 优先尝试 []map[string]any
	if slices, ok := raw.([]map[string]any); ok {
		return slices
	}
	// 回退到 []any
	if items, ok := raw.([]any); ok {
		result := make([]map[string]any, len(items))
		for i, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("sessions[%d] 类型不匹配: %T", i, item)
			}
			result[i] = m
		}
		return result
	}
	t.Fatalf("sessions 类型不匹配: %T", raw)
	return nil
}

// TestHandleSessionList_空目录 验证 sessions 目录不存在时返回空列表。
func TestHandleSessionList_空目录(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionList, nil)

	resp, err := s.handleSessionList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionList 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	sessions := toSessionSlice(t, resp.Payload)
	if len(sessions) != 0 {
		t.Errorf("sessions 长度 = %d, 期望 0", len(sessions))
	}
}

// TestHandleSessionList_正常返回 验证扫描 sessions 目录并按 mtime 降序排列。
func TestHandleSessionList_正常返回(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	// 创建两个会话
	writeTestMetadata(t, sessionsDir, "sess_old", map[string]any{
		"session_id":      "sess_old",
		"title":           "旧会话",
		"last_message_at": 1000.0,
	})
	writeTestMetadata(t, sessionsDir, "sess_new", map[string]any{
		"session_id":      "sess_new",
		"title":           "新会话",
		"last_message_at": 2000.0,
	})

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionList, nil)

	resp, err := s.handleSessionList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionList 返回错误: %v", err)
	}
	sessions := toSessionSlice(t, resp.Payload)
	if len(sessions) != 2 {
		t.Fatalf("sessions 长度 = %d, 期望 2", len(sessions))
	}

	// 按 last_message_at 降序：sess_new 应排在前面
	if sessions[0]["session_id"] != "sess_new" {
		t.Errorf("第一个 session_id = %q, 期望 %q", sessions[0]["session_id"], "sess_new")
	}
}

// TestHandleSessionList_跳过心跳会话 验证 heartbeat_ 前缀的会话被跳过。
func TestHandleSessionList_跳过心跳会话(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	writeTestMetadata(t, sessionsDir, "heartbeat_task1", map[string]any{
		"session_id": "heartbeat_task1",
		"title":      "心跳任务",
	})
	writeTestMetadata(t, sessionsDir, "sess_normal", map[string]any{
		"session_id": "sess_normal",
		"title":      "普通会话",
	})

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionList, nil)

	resp, err := s.handleSessionList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionList 返回错误: %v", err)
	}
	sessions := toSessionSlice(t, resp.Payload)
	if len(sessions) != 1 {
		t.Fatalf("sessions 长度 = %d, 期望 1", len(sessions))
	}
	if sessions[0]["session_id"] != "sess_normal" {
		t.Errorf("session_id = %q, 期望 %q", sessions[0]["session_id"], "sess_normal")
	}
}

// TestHandleSessionList_无Metadata 验证无 metadata.json 的会话用默认值。
func TestHandleSessionList_无Metadata(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	// 创建目录但不写 metadata.json
	if err := os.MkdirAll(filepath.Join(sessionsDir, "sess_no_meta"), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionList, nil)

	resp, err := s.handleSessionList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionList 返回错误: %v", err)
	}
	sessions := toSessionSlice(t, resp.Payload)
	if len(sessions) != 1 {
		t.Fatalf("sessions 长度 = %d, 期望 1", len(sessions))
	}
	if sessions[0]["session_id"] != "sess_no_meta" {
		t.Errorf("session_id = %q, 期望 %q", sessions[0]["session_id"], "sess_no_meta")
	}
}

// TestHandleSessionRename_设置标题 验证设置标题。
func TestHandleSessionRename_设置标题(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	writeTestMetadata(t, sessionsDir, "sess_1", map[string]any{
		"session_id":      "sess_1",
		"title":           "旧标题",
		"last_message_at": 1000.0,
	})

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRename,
		json.RawMessage(`{"session_id": "sess_1", "title": "新标题"}`))

	resp, err := s.handleSessionRename(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionRename 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if resp.Payload["title"] != "新标题" {
		t.Errorf("title = %q, 期望 %q", resp.Payload["title"], "新标题")
	}
	if resp.Payload["previous_title"] != "旧标题" {
		t.Errorf("previous_title = %q, 期望 %q", resp.Payload["previous_title"], "旧标题")
	}
}

// TestHandleSessionRename_查询标题 验证 title 为 nil 时返回当前标题。
func TestHandleSessionRename_查询标题(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	writeTestMetadata(t, sessionsDir, "sess_1", map[string]any{
		"session_id": "sess_1",
		"title":      "当前标题",
	})

	// 不传 title 字段 → 查询模式
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRename,
		json.RawMessage(`{"session_id": "sess_1"}`))

	resp, err := s.handleSessionRename(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionRename 返回错误: %v", err)
	}
	if resp.Payload["title"] != "当前标题" {
		t.Errorf("title = %q, 期望 %q", resp.Payload["title"], "当前标题")
	}
}

// TestHandleSessionRename_缺少SessionID 验证缺少 session_id 时返回错误。
func TestHandleSessionRename_缺少SessionID(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRename,
		json.RawMessage(`{}`))

	resp, err := s.handleSessionRename(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionRename 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false")
	}
}

// TestHandleSessionDelete_正常删除 验证删除会话目录。
func TestHandleSessionDelete_正常删除(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	writeTestMetadata(t, sessionsDir, "sess_del", map[string]any{
		"session_id": "sess_del",
	})

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionDelete,
		json.RawMessage(`{"session_id": "sess_del"}`))

	resp, err := s.handleSessionDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionDelete 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}

	// 验证目录已被删除
	if _, statErr := os.Stat(filepath.Join(sessionsDir, "sess_del")); !os.IsNotExist(statErr) {
		t.Error("会话目录应已被删除")
	}
}

// TestHandleSessionDelete_缺少SessionID 验证缺少 session_id 时返回错误。
func TestHandleSessionDelete_缺少SessionID(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionDelete,
		json.RawMessage(`{}`))

	resp, err := s.handleSessionDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionDelete 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false")
	}
}

// TestHandleSessionCreate_自动生成ID 验证不传 session_id 时自动生成。
func TestHandleSessionCreate_自动生成ID(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionCreate,
		json.RawMessage(`{}`))

	resp, err := s.handleSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionCreate 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}

	sessionID, ok := resp.Payload["sessionId"].(string)
	if !ok || sessionID == "" {
		t.Error("sessionId 不应为空")
	}

	// 验证目录和 metadata.json 已创建
	metaPath := filepath.Join(sessionsDir, sessionID, "metadata.json")
	if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
		t.Error("metadata.json 应已创建")
	}
}

// TestHandleSessionCreate_指定ID 验证传入 session_id 时使用指定值。
func TestHandleSessionCreate_指定ID(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionCreate,
		json.RawMessage(`{"session_id": "my_custom_id"}`))

	resp, err := s.handleSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionCreate 返回错误: %v", err)
	}
	if resp.Payload["sessionId"] != "my_custom_id" {
		t.Errorf("sessionId = %q, 期望 %q", resp.Payload["sessionId"], "my_custom_id")
	}

	// 验证目录和 metadata.json 已创建
	metaPath := filepath.Join(sessionsDir, "my_custom_id", "metadata.json")
	if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
		t.Error("metadata.json 应已创建")
	}
}

// TestHandleSessionSwitch 验证 session.switch 返回 ok=true。
func TestHandleSessionSwitch(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionSwitch,
		json.RawMessage(`{"session_id": "sess_1"}`))

	resp, err := s.handleSessionSwitch(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionSwitch 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
}

// TestHandleSessionRewind 验证 session.rewind 返回 NOT_IMPLEMENTED。
func TestHandleSessionRewind(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRewind, nil)

	resp, err := s.handleSessionRewind(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionRewind 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_IMPLEMENTED）")
	}
}

// TestHandleSessionFork 验证 session.fork 返回 NOT_IMPLEMENTED。
func TestHandleSessionFork(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionFork, nil)

	resp, err := s.handleSessionFork(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionFork 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_IMPLEMENTED）")
	}
}

// TestMakeSessionID 验证会话 ID 生成格式。
func TestMakeSessionID(t *testing.T) {
	id := makeSessionID()
	if len(id) < 10 {
		t.Errorf("session ID 太短: %q", id)
	}
	// 验证前缀格式 sess_
	prefix := "sess_"
	if len(id) < len(prefix) || id[:len(prefix)] != prefix {
		t.Errorf("session ID 前缀不匹配: %q, 期望以 %q 开头", id, prefix)
	}
}

// TestReadSessionMetadata 验证读取 metadata.json。
func TestReadSessionMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "sess_1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	meta := map[string]any{
		"session_id": "sess_1",
		"title":      "测试标题",
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	result := readSessionMetadata(tmpDir, "sess_1")
	if result == nil {
		t.Fatal("readSessionMetadata 返回 nil")
	}
	if result["title"] != "测试标题" {
		t.Errorf("title = %q, 期望 %q", result["title"], "测试标题")
	}
}

// TestReadSessionMetadata_不存在 验证文件不存在时返回 nil。
func TestReadSessionMetadata_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	result := readSessionMetadata(tmpDir, "nonexistent")
	if result != nil {
		t.Errorf("期望 nil, 实际 %v", result)
	}
}

// TestWriteSessionMetadata 验证写入 metadata.json。
func TestWriteSessionMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "sess_1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	meta := map[string]any{
		"session_id": "sess_1",
		"title":      "写入测试",
	}
	if err := writeSessionMetadata(tmpDir, "sess_1", meta); err != nil {
		t.Fatalf("writeSessionMetadata 返回错误: %v", err)
	}

	// 读回验证
	result := readSessionMetadata(tmpDir, "sess_1")
	if result["title"] != "写入测试" {
		t.Errorf("title = %q, 期望 %q", result["title"], "写入测试")
	}
}
