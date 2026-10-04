package sessionops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupTestSessionsDir 设置测试用的 sessions 目录，对齐 handle_session_test.go 的模式。
// 返回（sessionsDir, cleanup）。
func setupTestSessionsDir(t *testing.T) (sessionsDir string, cleanup func()) {
	t.Helper()
	tmpDir := t.TempDir()
	sessionsDir = filepath.Join(tmpDir, "agent", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("创建 sessions 目录失败: %v", err)
	}
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	path.ResetCache()
	session.ClearAllSessionMetadataCache()

	cleanup = func() {
		session.FlushMetadataQueue()
		session.FlushHistoryQueue()
		session.ResetHistoryWorker()
		session.ClearAllSessionMetadataCache()
		path.ResetCache()
	}
	return sessionsDir, cleanup
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

// ──────────────────────────── getUniqueForkName 测试 ────────────────────────────

// TestGetUniqueForkName_空baseName 验证 baseName 为空时生成 "(Branch)"。
func TestGetUniqueForkName_空baseName(t *testing.T) {
	result := getUniqueForkName("", map[string]bool{})
	if result != "(Branch)" {
		t.Errorf("期望 %q, 实际 %q", "(Branch)", result)
	}
}

// TestGetUniqueForkName_有baseName 验证有 baseName 时生成 "base (Branch)"。
func TestGetUniqueForkName_有baseName(t *testing.T) {
	result := getUniqueForkName("My Session", map[string]bool{})
	if result != "My Session (Branch)" {
		t.Errorf("期望 %q, 实际 %q", "My Session (Branch)", result)
	}
}

// TestGetUniqueForkName_已存在Branch 验证已存在同名 Branch 时递增编号。
func TestGetUniqueForkName_已存在Branch(t *testing.T) {
	existing := map[string]bool{
		"My Session (Branch)": true,
	}
	result := getUniqueForkName("My Session", existing)
	if result != "My Session (Branch 2)" {
		t.Errorf("期望 %q, 实际 %q", "My Session (Branch 2)", result)
	}
}

// TestGetUniqueForkName_多个已存在 验证多个已存在 Branch 时找到下一个可用编号。
func TestGetUniqueForkName_多个已存在(t *testing.T) {
	existing := map[string]bool{
		"My Session (Branch)":   true,
		"My Session (Branch 2)": true,
		"My Session (Branch 3)": true,
	}
	result := getUniqueForkName("My Session", existing)
	if result != "My Session (Branch 4)" {
		t.Errorf("期望 %q, 实际 %q", "My Session (Branch 4)", result)
	}
}

// TestGetUniqueForkName_空baseName已存在 验证空 baseName 且 "(Branch)" 已存在时递增。
func TestGetUniqueForkName_空baseName已存在(t *testing.T) {
	existing := map[string]bool{
		"(Branch)": true,
	}
	result := getUniqueForkName("", existing)
	if result != "(Branch 2)" {
		t.Errorf("期望 %q, 实际 %q", "(Branch 2)", result)
	}
}

// TestGetUniqueForkName_跳号 验证中间编号缺失时使用缺失的编号（从 2 开始）。
// Python 行为：找最小可用编号 >= 2，所以跳号时填补空缺。
func TestGetUniqueForkName_跳号(t *testing.T) {
	existing := map[string]bool{
		"Test (Branch)":   true,
		"Test (Branch 3)": true,
	}
	result := getUniqueForkName("Test", existing)
	// 编号 1（无编号）和 3 已被占用，2 未被占用
	if result != "Test (Branch 2)" {
		t.Errorf("期望 %q, 实际 %q", "Test (Branch 2)", result)
	}
}

// ──────────────────────────── isSelectableUserMessage 测试 ────────────────────────────

// TestIsSelectableUserMessage_正常消息 验证正常用户消息返回 true。
func TestIsSelectableUserMessage_正常消息(t *testing.T) {
	if !isSelectableUserMessage("Hello, can you help me?") {
		t.Error("正常用户消息应返回 true")
	}
}

// TestIsSelectableUserMessage_空消息 验证空消息返回 true。
func TestIsSelectableUserMessage_空消息(t *testing.T) {
	if !isSelectableUserMessage("") {
		t.Error("空消息应返回 true（无排除标签）")
	}
}

// TestIsSelectableUserMessage_bashStdout 验证包含 bash-stdout 标签时返回 false。
func TestIsSelectableUserMessage_bashStdout(t *testing.T) {
	if isSelectableUserMessage("result: <bash-stdout>hello</bash-stdout>") {
		t.Error("包含 <bash-stdout> 的消息应返回 false")
	}
}

// TestIsSelectableUserMessage_bashStderr 验证包含 bash-stderr 标签时返回 false。
func TestIsSelectableUserMessage_bashStderr(t *testing.T) {
	if isSelectableUserMessage("error: <bash-stderr>fail</bash-stderr>") {
		t.Error("包含 <bash-stderr> 的消息应返回 false")
	}
}

// TestIsSelectableUserMessage_localCommandStdout 验证包含 local-command-stdout 标签时返回 false。
func TestIsSelectableUserMessage_localCommandStdout(t *testing.T) {
	if isSelectableUserMessage("<local-command-stdout>ok</local-command-stdout>") {
		t.Error("包含 <local-command-stdout> 的消息应返回 false")
	}
}

// TestIsSelectableUserMessage_taskNotification 验证包含 task-notification 标签时返回 false。
func TestIsSelectableUserMessage_taskNotification(t *testing.T) {
	if isSelectableUserMessage("<task-notification>done</task-notification>") {
		t.Error("包含 <task-notification> 的消息应返回 false")
	}
}

// TestIsSelectableUserMessage_tick 验证包含 tick 标签时返回 false。
func TestIsSelectableUserMessage_tick(t *testing.T) {
	if isSelectableUserMessage("<tick>heartbeat</tick>") {
		t.Error("包含 <tick> 的消息应返回 false")
	}
}

// TestIsSelectableUserMessage_teammateMessage 验证包含 teammate-message 标签时返回 false。
func TestIsSelectableUserMessage_teammateMessage(t *testing.T) {
	if isSelectableUserMessage("<teammate-message from=agent-1>hello</teammate-message>") {
		t.Error("包含 <teammate-message 的消息应返回 false")
	}
}

// ──────────────────────────── copyFile 测试 ────────────────────────────

// TestCopyFile_正常复制 验证文件内容正确复制。
func TestCopyFile_正常复制(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")

	content := "hello world\nline 2\n"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile 返回错误: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(data) != content {
		t.Errorf("内容不匹配: 期望 %q, 实际 %q", content, string(data))
	}
}

// TestCopyFile_源文件不存在 验证源文件不存在时返回错误。
func TestCopyFile_源文件不存在(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "nonexistent.txt")
	dst := filepath.Join(tmpDir, "dst.txt")

	err := copyFile(src, dst)
	if err == nil {
		t.Error("期望返回错误，实际 nil")
	}
}

// ──────────────────────────── deepCopyMap 测试 ────────────────────────────

// TestDeepCopyMap_深拷贝 验证深拷贝后修改不影响原始 map。
func TestDeepCopyMap_深拷贝(t *testing.T) {
	original := map[string]any{
		"key1": "value1",
		"nested": map[string]any{
			"inner": "inner_value",
		},
		"slice": []any{1, 2, 3},
	}

	copied := deepCopyMap(original)

	// 修改拷贝不影响原始
	copied["key1"] = "modified"
	copied["nested"].(map[string]any)["inner"] = "modified_inner"

	if original["key1"] != "value1" {
		t.Errorf("修改拷贝不应影响原始: 期望 %q, 实际 %q", "value1", original["key1"])
	}
	nested := original["nested"].(map[string]any)
	if nested["inner"] != "inner_value" {
		t.Errorf("修改嵌套拷贝不应影响原始: 期望 %q, 实际 %q", "inner_value", nested["inner"])
	}
}

// TestDeepCopyMap_nil 验证 nil map 不会 panic。
func TestDeepCopyMap_nil(t *testing.T) {
	result := deepCopyMap(nil)
	// json.Marshal(nil) → "null"，json.Unmarshal("null", &result) → nil
	if result != nil {
		t.Errorf("期望 nil, 实际 %v", result)
	}
}

// ──────────────────────────── ForkSession 测试 ────────────────────────────

// TestForkSession_正常分叉 验证分叉会话：复制 history + 写 metadata。
func TestForkSession_正常分叉(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	// 准备源会话
	sourceID := "sess_source"
	writeTestMetadata(t, sessionsDir, sourceID, map[string]any{
		"session_id":      sourceID,
		"title":           "原始会话",
		"mode":            "code",
		"last_message_at": 1000.0,
		"message_count":   5,
	})
	writeTestHistory(t, sessionsDir, sourceID, []map[string]any{
		{"role": "user", "content": "hello", "id": "msg-1"},
		{"role": "assistant", "content": "hi", "id": "msg-2"},
	})

	targetID := "sess_target"
	result, err := ForkSession(ForkSessionParams{
		SourceSessionID: sourceID,
		TargetSessionID: targetID,
		ChannelID:       "test-channel",
	})
	if err != nil {
		t.Fatalf("ForkSession 返回错误: %v", err)
	}

	if result.SessionID != targetID {
		t.Errorf("SessionID = %q, 期望 %q", result.SessionID, targetID)
	}
	if result.SourceSessionID != sourceID {
		t.Errorf("SourceSessionID = %q, 期望 %q", result.SourceSessionID, sourceID)
	}
	if result.Title != "原始会话 (Branch)" {
		t.Errorf("Title = %q, 期望 %q", result.Title, "原始会话 (Branch)")
	}

	// 验证目标目录存在
	targetDir := filepath.Join(sessionsDir, targetID)
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Error("目标会话目录应存在")
	}

	// 验证 history.json 已复制
	targetHistory := filepath.Join(targetDir, "history.json")
	data, err := os.ReadFile(targetHistory)
	if err != nil {
		t.Fatalf("读取目标 history.json 失败: %v", err)
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("解析目标 history.json 失败: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("记录数 = %d, 期望 2", len(records))
	}
	// 验证 forked_from 元数据
	forkedFrom, ok := records[0]["forked_from"].(map[string]any)
	if !ok {
		t.Fatal("第一条记录应包含 forked_from")
	}
	if forkedFrom["session_id"] != sourceID {
		t.Errorf("forked_from.session_id = %q, 期望 %q", forkedFrom["session_id"], sourceID)
	}
}

// TestForkSession_源不存在 验证源会话不存在时返回错误。
func TestForkSession_源不存在(t *testing.T) {
	_, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	_, err := ForkSession(ForkSessionParams{
		SourceSessionID: "nonexistent",
		TargetSessionID: "target",
	})
	if err == nil {
		t.Error("期望返回错误")
	}
	if err.Error() != "source session not found" {
		t.Errorf("错误消息 = %q, 期望 %q", err.Error(), "source session not found")
	}
}

// TestForkSession_目标已存在 验证目标会话已存在时返回错误。
func TestForkSession_目标已存在(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sourceID := "sess_source"
	targetID := "sess_target"
	writeTestMetadata(t, sessionsDir, sourceID, map[string]any{"session_id": sourceID})
	writeTestMetadata(t, sessionsDir, targetID, map[string]any{"session_id": targetID})

	_, err := ForkSession(ForkSessionParams{
		SourceSessionID: sourceID,
		TargetSessionID: targetID,
	})
	if err == nil {
		t.Error("期望返回错误")
	}
	if err.Error() != "target session already exists" {
		t.Errorf("错误消息 = %q, 期望 %q", err.Error(), "target session already exists")
	}
}

// TestForkSession_无源history 验证源会话无 history.json 时不报错（仅复制 metadata）。
func TestForkSession_无源history(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sourceID := "sess_no_history"
	writeTestMetadata(t, sessionsDir, sourceID, map[string]any{
		"session_id":      sourceID,
		"title":           "无历史",
		"last_message_at": 1000.0,
		"message_count":   0,
	})

	targetID := "sess_fork_target"
	result, err := ForkSession(ForkSessionParams{
		SourceSessionID: sourceID,
		TargetSessionID: targetID,
		ChannelID:       "test-channel",
	})
	if err != nil {
		t.Fatalf("ForkSession 返回错误: %v", err)
	}
	if result.SessionID != targetID {
		t.Errorf("SessionID = %q, 期望 %q", result.SessionID, targetID)
	}

	// 目标目录存在但无 history.json
	targetHistory := filepath.Join(sessionsDir, targetID, "history.json")
	if _, err := os.Stat(targetHistory); !os.IsNotExist(err) {
		t.Error("目标 history.json 不应存在")
	}
}

// TestForkSession_自定义标题 验证自定义标题直接使用。
func TestForkSession_自定义标题(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sourceID := "sess_custom"
	writeTestMetadata(t, sessionsDir, sourceID, map[string]any{
		"session_id":      sourceID,
		"title":           "原始",
		"last_message_at": 1000.0,
		"message_count":   1,
	})

	result, err := ForkSession(ForkSessionParams{
		SourceSessionID: sourceID,
		TargetSessionID: "target_custom",
		Title:           "自定义标题",
		ChannelID:       "ch",
	})
	if err != nil {
		t.Fatalf("ForkSession 返回错误: %v", err)
	}
	if result.Title != "自定义标题 (Branch)" {
		t.Errorf("Title = %q, 期望 %q", result.Title, "自定义标题 (Branch)")
	}
}

// ──────────────────────────── RewindSession 测试 ────────────────────────────

// TestRewindSession_正常回退 验证回退到指定 turn。
func TestRewindSession_正常回退(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_rewind"
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "user", "content": "first question"},
		{"role": "assistant", "content": "first answer"},
		{"role": "user", "content": "second question"},
		{"role": "assistant", "content": "second answer"},
		{"role": "user", "content": "third question"},
		{"role": "assistant", "content": "third answer"},
	})

	result, err := RewindSession(RewindSessionParams{
		SessionID: sessionID,
		TurnIndex: 2,
	})
	if err != nil {
		t.Fatalf("RewindSession 返回错误: %v", err)
	}

	if result.SessionID != sessionID {
		t.Errorf("SessionID = %q, 期望 %q", result.SessionID, sessionID)
	}
	if result.TurnIndex != 2 {
		t.Errorf("TurnIndex = %d, 期望 2", result.TurnIndex)
	}
	if result.Content != "second question" {
		t.Errorf("Content = %q, 期望 %q", result.Content, "second question")
	}
	if result.RemovedRecords < 1 {
		t.Errorf("RemovedRecords = %d, 期望 > 0", result.RemovedRecords)
	}
}

// TestRewindSession_turnIndex小于1 验证 turn_index < 1 返回错误。
func TestRewindSession_turnIndex小于1(t *testing.T) {
	_, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	_, err := RewindSession(RewindSessionParams{
		SessionID: "any",
		TurnIndex: 0,
	})
	if err == nil {
		t.Error("期望返回错误")
	}
	if err.Error() != "turn_index must be >= 1" {
		t.Errorf("错误消息 = %q, 期望 %q", err.Error(), "turn_index must be >= 1")
	}
}

// TestRewindSession_会话不存在 验证会话 history 不存在时返回错误。
func TestRewindSession_会话不存在(t *testing.T) {
	_, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	_, err := RewindSession(RewindSessionParams{
		SessionID: "nonexistent",
		TurnIndex: 1,
	})
	if err == nil {
		t.Error("期望返回错误")
	}
	if err.Error() != "session history not found" {
		t.Errorf("错误消息 = %q, 期望 %q", err.Error(), "session history not found")
	}
}

// TestRewindSession_无用户消息 验证 history 中无用户消息时返回错误。
func TestRewindSession_无用户消息(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_no_user"
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "assistant", "content": "no user message"},
	})

	_, err := RewindSession(RewindSessionParams{
		SessionID: sessionID,
		TurnIndex: 1,
	})
	if err == nil {
		t.Error("期望返回错误")
	}
	if err.Error() != "no user messages in session" {
		t.Errorf("错误消息 = %q, 期望 %q", err.Error(), "no user messages in session")
	}
}

// TestRewindSession_turnIndex超出范围 验证 turn_index 超过总 turn 数时返回错误。
func TestRewindSession_turnIndex超出范围(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_exceed"
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "user", "content": "one"},
		{"role": "assistant", "content": "reply"},
	})

	_, err := RewindSession(RewindSessionParams{
		SessionID: sessionID,
		TurnIndex: 5,
	})
	if err == nil {
		t.Error("期望返回错误")
	}
}

// TestRewindSession_剥离FileContent 验证 content 中 <file-content> 块被剥离。
func TestRewindSession_剥离FileContent(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_filecontent"
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "user", "content": "before <file-content path=\"foo.go\">content here</file-content> after"},
		{"role": "assistant", "content": "reply"},
	})

	result, err := RewindSession(RewindSessionParams{
		SessionID: sessionID,
		TurnIndex: 1,
	})
	if err != nil {
		t.Fatalf("RewindSession 返回错误: %v", err)
	}
	// <file-content> 块应被剥离，保留前后文本
	if result.Content != "before  after" {
		t.Errorf("Content = %q, 期望 %q", result.Content, "before  after")
	}
}

// ──────────────────────────── ListSessionTurns 测试 ────────────────────────────

// TestListSessionTurns_正常 验证列出用户 turn。
func TestListSessionTurns_正常(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_list"
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "user", "content": "first question", "id": "msg-1", "request_id": "req-1", "timestamp": 1000.0},
		{"role": "assistant", "content": "first answer"},
		{"role": "user", "content": "second question", "id": "msg-2", "request_id": "req-2", "timestamp": 2000.0},
		{"role": "assistant", "content": "second answer"},
	})

	result := ListSessionTurns(sessionID)

	if result.Total != 2 {
		t.Errorf("Total = %d, 期望 2", result.Total)
	}
	if len(result.Turns) != 2 {
		t.Fatalf("Turns 长度 = %d, 期望 2", len(result.Turns))
	}
	if result.Turns[0].TurnIndex != 1 {
		t.Errorf("Turns[0].TurnIndex = %d, 期望 1", result.Turns[0].TurnIndex)
	}
	if result.Turns[0].ContentPreview != "first question" {
		t.Errorf("Turns[0].ContentPreview = %q, 期望 %q", result.Turns[0].ContentPreview, "first question")
	}
	if result.Turns[1].TurnIndex != 2 {
		t.Errorf("Turns[1].TurnIndex = %d, 期望 2", result.Turns[1].TurnIndex)
	}
}

// TestListSessionTurns_不存在 验证会话不存在时返回空结果。
func TestListSessionTurns_不存在(t *testing.T) {
	_, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	result := ListSessionTurns("nonexistent")
	if result.Total != 0 {
		t.Errorf("Total = %d, 期望 0", result.Total)
	}
	if result.Turns != nil {
		t.Errorf("Turns 应为 nil")
	}
}

// TestListSessionTurns_过滤非用户消息 验证 system/tool 消息被跳过。
func TestListSessionTurns_过滤非用户消息(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_filter"
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "system", "content": "system prompt"},
		{"role": "user", "content": "user message"},
		{"role": "assistant", "content": "reply"},
		{"role": "tool", "content": "tool result"},
	})

	result := ListSessionTurns(sessionID)

	if result.Total != 1 {
		t.Errorf("Total = %d, 期望 1（仅 user 消息）", result.Total)
	}
	if len(result.Turns) != 1 {
		t.Fatalf("Turns 长度 = %d, 期望 1", len(result.Turns))
	}
}

// TestListSessionTurns_过滤不可选择消息 验证包含非用户标签的消息被跳过。
func TestListSessionTurns_过滤不可选择消息(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_unselectable"
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "user", "content": "normal question"},
		{"role": "assistant", "content": "reply"},
		{"role": "user", "content": "<bash-stdout>output</bash-stdout>"},
		{"role": "assistant", "content": "reply2"},
		{"role": "user", "content": "another question"},
		{"role": "assistant", "content": "reply3"},
	})

	result := ListSessionTurns(sessionID)

	if result.Total != 3 {
		t.Errorf("Total = %d, 期望 3（包括不可选的）", result.Total)
	}
	// 只有 2 个可选 turn（跳过了 bash-stdout 的）
	if len(result.Turns) != 2 {
		t.Fatalf("Turns 长度 = %d, 期望 2（跳过 bash-stdout）", len(result.Turns))
	}
}

// TestListSessionTurns_长内容截断 验证超过 80 字符的内容被截断。
func TestListSessionTurns_长内容截断(t *testing.T) {
	sessionsDir, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sessionID := "sess_long"
	longContent := ""
	for i := 0; i < 20; i++ {
		longContent += "abcdefghij" // 10 chars * 20 = 200 chars
	}
	writeTestHistory(t, sessionsDir, sessionID, []map[string]any{
		{"role": "user", "content": longContent},
		{"role": "assistant", "content": "reply"},
	})

	result := ListSessionTurns(sessionID)

	if len(result.Turns) != 1 {
		t.Fatalf("Turns 长度 = %d, 期望 1", len(result.Turns))
	}
	if len(result.Turns[0].ContentPreview) > 80 {
		t.Errorf("ContentPreview 长度 = %d, 期望 <= 80", len(result.Turns[0].ContentPreview))
	}
}

// ──────────────────────────── RestoreSessionFiles 测试 ────────────────────────────

// TestRestoreSessionFiles_无恢复文件 验证无文件需要恢复时返回空结果。
func TestRestoreSessionFiles_无恢复文件(t *testing.T) {
	_, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	result := RestoreSessionFiles("nonexistent", 1)

	if len(result.RestoredFiles) != 0 {
		t.Errorf("RestoredFiles 长度 = %d, 期望 0", len(result.RestoredFiles))
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors 长度 = %d, 期望 0", len(result.Errors))
	}
}

// ──────────────────────────── RewindSessionContext 测试 ────────────────────────────

// TestRewindSessionContext_nilAgent 验证 DeepAgent 为 nil 时返回 false。
func TestRewindSessionContext_nilAgent(t *testing.T) {
	_, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	result := RewindSessionContext(context.Background(), nil, "any-session", 1)
	if result {
		t.Error("nil DeepAgent 应返回 false")
	}
}

// ──────────────────────────── CopySessionState 测试 ────────────────────────────

// TestCopySessionState_nilDeepAgent 验证 DeepAgent 为 nil 时跳过 flush 但继续执行。
func TestCopySessionState_nilDeepAgent(t *testing.T) {
	_, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	// 无真实 session state，PreRun 会失败，但不应 panic
	result := CopySessionState(context.Background(), "source", "target", nil, nil)
	// 在没有真实 checkpointer 的情况下，PreRun 失败，返回 false
	if result {
		t.Error("无真实 checkpointer 应返回 false")
	}
}

// ──────────────────────────── CopySessionContext 测试 ────────────────────────────

// TestCopySessionContext_nilAgent 验证 DeepAgent 为 nil 时返回 false（不 panic）。
func TestCopySessionContext_nilAgent(t *testing.T) {
	_, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	result := CopySessionContext(context.Background(), nil, "source", "target")
	if result {
		t.Error("nil DeepAgent 应返回 false")
	}
}
