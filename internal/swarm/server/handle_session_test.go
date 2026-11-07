package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupTestSessionsDir 设置测试用的 sessions 目录，返回（sessionsDir, AgentServer, cleanup）。
//
// Python: 测试隔离模式：设置 UAPCLAW_DATA_DIR 环境变量 + ResetCache，
// 确保全局函数 GetSessionsDir() 指向测试临时目录（Python 用 set_user_workspace_dir()）。
func setupTestSessionsDir(t *testing.T) (sessionsDir string, s *AgentServer, cleanup func()) {
	t.Helper()
	tmpDir := t.TempDir()
	sessionsDir = filepath.Join(tmpDir, "agent", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("创建 sessions 目录失败: %v", err)
	}
	// Python: set_user_workspace_dir(tmpDir) — 通过环境变量覆盖全局路径
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	path.ResetCache()
	session.ClearAllSessionMetadataCache()

	server, _ := newTestServer()
	cleanup = func() {
		session.FlushMetadataQueue()
		session.FlushHistoryQueue()
		session.ResetHistoryWorker()
		session.ClearAllSessionMetadataCache()
		path.ResetCache()
	}
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

// writeTestHistory 写入测试用 history.json。
func writeTestHistory(t *testing.T, sessionsDir, sessionID string, records []map[string]any) {
	t.Helper()
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建会话目录失败: %v", err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatalf("序列化 history 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "history.json"), data, 0o644); err != nil {
		t.Fatalf("写入 history.json 失败: %v", err)
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

// TestHandleSessionDelete_会话不存在 验证目录不存在时返回 NOT_FOUND。
// Python: session_dir.exists() 守卫（agent_ws_server.py L1537-1544）。
func TestHandleSessionDelete_会话不存在(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionDelete,
		json.RawMessage(`{"session_id": "no_such_session"}`))

	resp, err := s.handleSessionDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionDelete 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false")
	}
	if code, _ := resp.Payload["code"].(string); code != "NOT_FOUND" {
		t.Errorf("code 应为 NOT_FOUND，实际 %v", resp.Payload["code"])
	}
}

// TestHandleSessionDelete_路径为文件 验证 session_dir 非目录时返回 BAD_REQUEST。

// TestHandleSessionDelete_团队模式清理失败 验证 team 模式 DeleteSessionRuntime
// 失败时返回 DELETE_FAILED 且不删目录。

// TestHandleSessionDelete_空SessionID空白字符 验证空白 session_id 视为缺失。
// Python: target = str(params.get("session_id") or "").strip()。
func TestHandleSessionDelete_空SessionID空白字符(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionDelete,
		json.RawMessage(`{"session_id": "   "}`))

	resp, err := s.handleSessionDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionDelete 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false")
	}
	if code, _ := resp.Payload["code"].(string); code != "BAD_REQUEST" {
		t.Errorf("code 应为 BAD_REQUEST，实际 %v", resp.Payload["code"])
	}
}




// TestHandleSessionRewind_缺少参数 验证 session.rewind 缺少参数时返回错误。
func TestHandleSessionRewind_缺少参数(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRewind, nil)

	_, err := s.handleSessionRewind(context.Background(), req)
	if err == nil {
		t.Error("期望返回错误（缺少 session_id）")
	}
}

// TestHandleSessionRewind_正常回退 验证 session.rewind 正常回退。
func TestHandleSessionRewind_正常回退(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_rewind_test"
	writeTestMetadata(t, sessionsDir, sessionID, map[string]any{
		"session_id":      sessionID,
		"title":           "回退测试",
		"last_message_at": 1000.0,
		"message_count":   3,
	})
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "user", "content": "q1"},
		{"role": "assistant", "content": "a1"},
		{"role": "user", "content": "q2"},
		{"role": "assistant", "content": "a2"},
	})

	params, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"turn_index": 1,
	})
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRewind, params)

	resp, err := s.handleSessionRewind(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionRewind 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if resp.Payload["session_id"] != sessionID {
		t.Errorf("payload session_id = %q, 期望 %q", resp.Payload["session_id"], sessionID)
	}
}

// TestHandleSessionRewindAndRestore_缺少参数 验证 session.rewind_and_restore 缺少参数时返回错误。
func TestHandleSessionRewindAndRestore_缺少参数(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRewindAndRestore, nil)

	_, err := s.handleSessionRewindAndRestore(context.Background(), req)
	if err == nil {
		t.Error("期望返回错误（缺少 session_id）")
	}
}

// TestHandleSessionRewindContext_缺少参数 验证 session.rewind_context 缺少参数时返回错误。
func TestHandleSessionRewindContext_缺少参数(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRewindContext, nil)

	_, err := s.handleSessionRewindContext(context.Background(), req)
	if err == nil {
		t.Error("期望返回错误（缺少 session_id）")
	}
}

// TestHandleSessionFork_缺少参数 验证 session.fork 缺少参数时返回错误。
func TestHandleSessionFork_缺少参数(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionFork, nil)

	_, err := s.handleSessionFork(context.Background(), req)
	if err == nil {
		t.Error("期望返回错误（缺少 source_session_id）")
	}
}


// TestHandleHistoryListTurns_缺少参数 验证 history.list_turns 缺少参数时返回错误。
func TestHandleHistoryListTurns_缺少参数(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHistoryListTurns, nil)

	_, err := s.handleHistoryListTurns(context.Background(), req)
	if err == nil {
		t.Error("期望返回错误（缺少 session_id）")
	}
}

// TestHandleHistoryListTurns_正常 验证 history.list_turns 正常返回 turn 列表。
func TestHandleHistoryListTurns_正常(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_list_turns"
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "user", "content": "q1", "id": "m1"},
		{"role": "assistant", "content": "a1"},
	})

	params, _ := json.Marshal(map[string]any{"session_id": sessionID})
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHistoryListTurns, params)

	resp, err := s.handleHistoryListTurns(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHistoryListTurns 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	total, ok := resp.Payload["total"].(int)
	if !ok || total != 1 {
		t.Errorf("payload total = %v, 期望 1", resp.Payload["total"])
	}
}

// TestHandleSessionRestoreFiles_缺少参数 验证 session.restore_files 缺少参数时返回错误。
func TestHandleSessionRestoreFiles_缺少参数(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionRestoreFiles, nil)

	_, err := s.handleSessionRestoreFiles(context.Background(), req)
	if err == nil {
		t.Error("期望返回错误（缺少 session_id）")
	}
}

// TestMakeSessionID 验证会话 ID 生成格式。
func TestMakeSessionID(t *testing.T) {
	id := session.MakeSessionID()
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

	result := session.ReadSessionMetadata(tmpDir, "sess_1")
	if result == nil {
		t.Fatal("ReadSessionMetadata 返回 nil")
	}
	if result["title"] != "测试标题" {
		t.Errorf("title = %q, 期望 %q", result["title"], "测试标题")
	}
}

// TestReadSessionMetadata_不存在 验证文件不存在时返回 nil。
func TestReadSessionMetadata_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	result := session.ReadSessionMetadata(tmpDir, "nonexistent")
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
	if err := session.WriteSessionMetadata(tmpDir, "sess_1", meta); err != nil {
		t.Fatalf("WriteSessionMetadata 返回错误: %v", err)
	}

	// 读回验证
	result := session.ReadSessionMetadata(tmpDir, "sess_1")
	if result["title"] != "写入测试" {
		t.Errorf("title = %q, 期望 %q", result["title"], "写入测试")
	}
}

// ──────────────────────────── 10.3.28 provisioner 链路用例 ────────────────────────────

// TestHandleSessionCreate_返回会话标识 校验 create 走 provisioner 全链路。
//
// 10.3.28b：原「自动生成ID/指定ID」用例改写——session.create 不再接受
// 非 TUI 的 session_id，改由 create_token 认领（Go 无预热池，直接新建）。
func TestHandleSessionCreate_返回会话标识(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionCreate,
		json.RawMessage(`{"create_token": "tok", "mode": "agent", "work_mode": "work"}`))
	resp, err := s.handleSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.OK 应为 true，实际 payload=%v", resp.Payload)
	}
	payload := resp.Payload
	sessionID, _ := payload["session_id"].(string)
	if sessionID == "" {
		t.Fatal("session_id 不应为空")
	}
	if _, ok := payload["created"]; !ok {
		t.Fatal("应返回 created 字段")
	}
	// metadata.json 已落盘
	metaPath := filepath.Join(sessionsDir, sessionID, "metadata.json")
	if _, statErr := os.Stat(metaPath); statErr != nil {
		t.Fatalf("metadata.json 应已创建: %v", statErr)
	}
}

// TestHandleSessionCreate_非TUI显式ID被拒绝 校验上游语义。
//
// Python: session.create no longer accepts session_id
func TestHandleSessionCreate_非TUI显式ID被拒绝(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionCreate,
		json.RawMessage(`{"session_id": "custom", "create_token": "tok"}`))
	resp, err := s.handleSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if resp.OK {
		t.Fatal("非 TUI 显式 session_id 应被拒绝")
	}
	payload := resp.Payload
	want := "session.create no longer accepts session_id; use session.switch to restore"
	if payload["error"] != want {
		t.Fatalf("错误文案 = %v, want %q", payload["error"], want)
	}
}

// TestHandleSessionSwitch_切换已有会话 校验 switch 走 provisioner 两阶段事务。
func TestHandleSessionSwitch_切换已有会话(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	writeTestMetadata(t, sessionsDir, "sess_sw", map[string]any{
		"session_id": "sess_sw",
		"mode":       "agent.plan",
		"channel_id": "web",
	})

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionSwitch,
		json.RawMessage(`{"session_id": "sess_sw", "mode": "agent.plan"}`))
	resp, err := s.handleSessionSwitch(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.OK 应为 true，实际 payload=%v", resp.Payload)
	}
	payload := resp.Payload
	if payload["session_id"] != "sess_sw" {
		t.Fatalf("session_id = %v, want sess_sw", payload["session_id"])
	}
	if payload["switched"] != true {
		t.Fatalf("switched = %v, want true", payload["switched"])
	}
}

// TestHandleSessionSwitch_无会话标识报错 校验参数校验。
func TestHandleSessionSwitch_无会话标识报错(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionSwitch,
		json.RawMessage(`{}`))
	resp, err := s.handleSessionSwitch(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if resp.OK {
		t.Fatal("无 session_id 应失败")
	}
	payload := resp.Payload
	if payload["code"] != "BAD_REQUEST" {
		t.Fatalf("code = %v, want BAD_REQUEST", payload["code"])
	}
	if payload["error"] != "session_id is required" {
		t.Fatalf("error = %v, want session_id is required", payload["error"])
	}
}

// TestHandleSessionDelete_七阶段事务成功 校验删除走 provisioner。
//
// 10.3.28c：原「正常删除」用例改写——删除改为七阶段事务，
// 成功后目录删除 + 元数据缓存清理 + plan 状态重置。
func TestHandleSessionDelete_七阶段事务成功(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	writeTestMetadata(t, sessionsDir, "sess_del", map[string]any{
		"session_id": "sess_del",
		"mode":       "agent.plan",
		"channel_id": "web",
	})

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionDelete,
		json.RawMessage(`{"session_id": "sess_del"}`))
	resp, err := s.handleSessionDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.OK 应为 true，实际 payload=%v", resp.Payload)
	}
	if _, statErr := os.Stat(filepath.Join(sessionsDir, "sess_del")); !os.IsNotExist(statErr) {
		t.Fatal("会话目录应已被删除")
	}
}

// TestHandleSessionDelete_目录不存在报NOT_FOUND 校验删除前置。
func TestHandleSessionDelete_目录不存在报NOT_FOUND(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodSessionDelete,
		json.RawMessage(`{"session_id": "not_exist"}`))
	resp, err := s.handleSessionDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if resp.OK {
		t.Fatal("目录不存在应失败")
	}
	payload := resp.Payload
	if payload["code"] != "NOT_FOUND" {
		t.Fatalf("code = %v, want NOT_FOUND", payload["code"])
	}
}
