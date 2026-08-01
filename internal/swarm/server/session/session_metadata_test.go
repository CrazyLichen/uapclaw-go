package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// resetWorkspaceCache 重置 workspace 路径缓存，确保 t.Setenv 生效。
func resetWorkspaceCache() {
	path.ResetCache()
}

// ──────────────────────────── 导出函数 ────────────────────────────

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

	result := ReadSessionMetadata(tmpDir, "sess_1")
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
	result := ReadSessionMetadata(tmpDir, "nonexistent")
	if result != nil {
		t.Errorf("期望 nil, 实际 %v", result)
	}
}

// TestWriteSessionMetadata 验证写入 metadata.json。
func TestWriteSessionMetadata(t *testing.T) {
	tmpDir := t.TempDir()

	meta := map[string]any{
		"session_id": "sess_1",
		"title":      "写入测试",
	}
	if err := WriteSessionMetadata(tmpDir, "sess_1", meta); err != nil {
		t.Fatalf("WriteSessionMetadata 返回错误: %v", err)
	}

	// 读回验证
	result := ReadSessionMetadata(tmpDir, "sess_1")
	if result["title"] != "写入测试" {
		t.Errorf("title = %q, 期望 %q", result["title"], "写入测试")
	}
}

// TestInitSessionMetadata 验证同步写入后立即可读。
func TestInitSessionMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	sessionDir := filepath.Join(tmpDir, "agent", "sessions", "sess_init")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	// 手动执行 InitSessionMetadata 逻辑
	meta := map[string]any{
		"session_id":      "sess_init",
		"channel_id":      "web",
		"user_id":         "",
		"created_at":      CurrentTimestamp(),
		"last_message_at": CurrentTimestamp(),
		"title":           "",
		"message_count":   0,
		"mode":            "unknown",
		"team_name":       "",
		"round_id":        0,
	}
	sessionsDir := filepath.Join(tmpDir, "agent", "sessions")
	if err := WriteSessionMetadata(sessionsDir, "sess_init", meta); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	result := ReadSessionMetadata(sessionsDir, "sess_init")
	if result == nil {
		t.Fatal("同步写入后应立即可读")
	}
	if result["session_id"] != "sess_init" {
		t.Errorf("session_id = %q, 期望 %q", result["session_id"], "sess_init")
	}
}

// TestGetSessionMetadata 验证读取（缓存优先）。
func TestGetSessionMetadata(t *testing.T) {
	// 写入缓存
	deliveryContextMu.Lock()
	deliveryContextCache["sess_meta"] = map[string]any{"session_id": "sess_meta", "title": "测试获取"}
	deliveryContextMu.Unlock()

	result := GetSessionMetadata("sess_meta")
	if result == nil {
		t.Fatal("GetSessionMetadata 返回 nil")
	}
	if result["title"] != "测试获取" {
		t.Errorf("title = %q, 期望 %q", result["title"], "测试获取")
	}

	// 清理缓存
	RemoveSessionMetadataCache("sess_meta")
}

// TestRemoveSessionMetadataCache 验证缓存清除。
func TestRemoveSessionMetadataCache(t *testing.T) {
	deliveryContextMu.Lock()
	deliveryContextCache["test_remove"] = map[string]any{"session_id": "test_remove"}
	deliveryContextMu.Unlock()

	RemoveSessionMetadataCache("test_remove")

	deliveryContextMu.RLock()
	_, exists := deliveryContextCache["test_remove"]
	deliveryContextMu.RUnlock()

	if exists {
		t.Error("缓存应已被清除")
	}
}

// TestUpdateSessionMetadata_自动标题创建 验证新 metadata 时自动标题。
func TestUpdateSessionMetadata_自动标题创建(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	userContent := "这是一条用户消息"
	UpdateSessionMetadata(SessionMetadataUpdate{
		SessionID:             "sess_autotitle_new",
		ChannelID:             PtrStr("web"),
		IncrementMessageCount: true,
		UserContent:           PtrStr(userContent),
		Mode:                  PtrStr("agent"),
	})

	// 等待异步写入
	time.Sleep(200 * time.Millisecond)

	sessionsDir := filepath.Join(tmpDir, "agent", "sessions")
	result := ReadSessionMetadata(sessionsDir, "sess_autotitle_new")
	if result == nil {
		t.Fatal("metadata 应已创建")
	}
	title, _ := result["title"].(string)
	if title == "" {
		t.Errorf("自动标题不应为空, 实际: %q", title)
	}
}

// TestUpdateSessionMetadata_自动标题回填 验证更新 metadata 时自动标题回填。
func TestUpdateSessionMetadata_自动标题回填(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()

	sessionsDir := filepath.Join(tmpDir, "agent", "sessions")

	// 先创建一个无标题的会话
	meta := map[string]any{
		"session_id":      "sess_backfill",
		"channel_id":      "web",
		"title":           "",
		"message_count":   0,
		"last_message_at": CurrentTimestamp(),
	}
	if err := WriteSessionMetadata(sessionsDir, "sess_backfill", meta); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// 更新时传入用户内容
	UpdateSessionMetadata(SessionMetadataUpdate{
		SessionID:             "sess_backfill",
		IncrementMessageCount: true,
		UserContent:           PtrStr("用户想问的问题"),
	})

	// 等待异步写入
	time.Sleep(200 * time.Millisecond)

	result := ReadSessionMetadata(sessionsDir, "sess_backfill")
	if result == nil {
		t.Fatal("metadata 应存在")
	}
	title, _ := result["title"].(string)
	if title == "" {
		t.Errorf("自动标题应已回填, 实际: %q", title)
	}
}

// TestSetSessionDeliveryContext_基本设置 验证 delivery context 写入和返回
func TestSetSessionDeliveryContext_基本设置(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()
	ClearAllSessionMetadataCache()

	// 先初始化 metadata
	sessionsDir := filepath.Join(tmpDir, "agent", "sessions")
	meta := map[string]any{
		"session_id":      "sess_dc",
		"channel_id":      "web",
		"created_at":      CurrentTimestamp(),
		"last_message_at": CurrentTimestamp(),
		"title":           "",
		"message_count":   0,
		"mode":            "unknown",
	}
	WriteSessionMetadata(sessionsDir, "sess_dc", meta)

	// 设置 delivery context
	dc := SetSessionDeliveryContext("sess_dc", PtrStr("web"), PtrStr("req-1"), map[string]any{"key": "val"})

	if dc["channel_id"] != "web" {
		t.Errorf("channel_id = %v, want web", dc["channel_id"])
	}
	if dc["source_request_id"] != "req-1" {
		t.Errorf("source_request_id = %v, want req-1", dc["source_request_id"])
	}
	if dc["delivery_kind"] != "server_push" {
		t.Errorf("delivery_kind = %v, want server_push", dc["delivery_kind"])
	}
	rm, ok := dc["route_metadata"].(map[string]any)
	if !ok || rm["key"] != "val" {
		t.Errorf("route_metadata = %v, want {key: val}", dc["route_metadata"])
	}

	FlushMetadataQueue()
}

// TestSetSessionDeliveryContext_空metadata创建 验证空 metadata 时创建新的
func TestSetSessionDeliveryContext_空metadata创建(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()
	ClearAllSessionMetadataCache()

	dc := SetSessionDeliveryContext("sess_new_dc", PtrStr("api"), PtrStr("req-2"), nil)
	if dc["channel_id"] != "api" {
		t.Errorf("channel_id = %v, want api", dc["channel_id"])
	}
	if dc["delivery_kind"] != "server_push" {
		t.Errorf("delivery_kind = %v, want server_push", dc["delivery_kind"])
	}

	FlushMetadataQueue()
}

// TestGetSessionDeliveryContext_从缓存读取 验证缓存优先
func TestGetSessionDeliveryContext_从缓存读取(t *testing.T) {
	// 通过 Set 写入缓存
	SetSessionDeliveryContext("sess_cache_dc", PtrStr("ch1"), PtrStr("rid1"), nil)

	result := GetSessionDeliveryContext("sess_cache_dc")
	if result == nil {
		t.Fatal("GetSessionDeliveryContext 返回 nil")
	}
	if result["channel_id"] != "ch1" {
		t.Errorf("channel_id = %v, want ch1", result["channel_id"])
	}
	FlushMetadataQueue()
}

// TestGetSessionDeliveryContext_不存在 验证无 delivery context 时返回 nil
func TestGetSessionDeliveryContext_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()
	ClearAllSessionMetadataCache()

	result := GetSessionDeliveryContext("nonexistent")
	if result != nil {
		t.Errorf("期望 nil, 实际 %v", result)
	}
}

// TestBuildServerPushMessage_基本构造 验证基本 message 构造
func TestBuildServerPushMessage_基本构造(t *testing.T) {
	// 先设置 delivery context
	SetSessionDeliveryContext("sess_push", PtrStr("ch1"), PtrStr("rid1"), map[string]any{"model": "qwen"})

	msg := BuildServerPushMessage("sess_push", "req-push", map[string]any{"event": "done"})
	if msg["request_id"] != "req-push" {
		t.Errorf("request_id = %v, want req-push", msg["request_id"])
	}
	if msg["channel_id"] != "ch1" {
		t.Errorf("channel_id = %v, want ch1", msg["channel_id"])
	}
	if msg["session_id"] != "sess_push" {
		t.Errorf("session_id = %v, want sess_push", msg["session_id"])
	}
	rm, ok := msg["metadata"].(map[string]any)
	if !ok || rm["model"] != "qwen" {
		t.Errorf("metadata = %v, want {model: qwen}", msg["metadata"])
	}
	FlushMetadataQueue()
}

// TestBuildServerPushMessage_fallbackChannelID 验证 fallback 逻辑
func TestBuildServerPushMessage_fallbackChannelID(t *testing.T) {
	msg := BuildServerPushMessage("no_dc_session", "req-push", map[string]any{"event": "done"}, "fallback_ch")
	if msg["channel_id"] != "fallback_ch" {
		t.Errorf("channel_id = %v, want fallback_ch", msg["channel_id"])
	}
}

// TestInitSessionMetadata_同步写入 验证 InitSessionMetadata 直接调用
func TestInitSessionMetadata_同步写入(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()
	ClearAllSessionMetadataCache()

	InitSessionMetadata("sess_init2", "web2", "user1", "初始标题", "agent", "team1")

	sessionsDir := filepath.Join(tmpDir, "agent", "sessions")
	result := ReadSessionMetadata(sessionsDir, "sess_init2")
	if result == nil {
		t.Fatal("InitSessionMetadata 同步写入后应立即可读")
	}
	if result["session_id"] != "sess_init2" {
		t.Errorf("session_id = %q, want sess_init2", result["session_id"])
	}
	if result["channel_id"] != "web2" {
		t.Errorf("channel_id = %q, want web2", result["channel_id"])
	}
	if result["title"] != "初始标题" {
		t.Errorf("title = %q, want 初始标题", result["title"])
	}
	if result["mode"] != "agent" {
		t.Errorf("mode = %q, want agent", result["mode"])
	}
}

// TestIncrementSessionRoundCount_递增 验证 round_id 递增
func TestIncrementSessionRoundCount_递增(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	resetWorkspaceCache()
	ClearAllSessionMetadataCache()

	InitSessionMetadata("sess_round", "web", "", "", "agent", "")
	// round_id 应为 0

	newRound, err := IncrementSessionRoundCount("sess_round")
	if err != nil {
		t.Fatalf("IncrementSessionRoundCount 返回错误: %v", err)
	}
	if newRound != 1 {
		t.Errorf("newRound = %d, want 1", newRound)
	}

	FlushMetadataQueue()

	// 再次递增
	newRound2, err := IncrementSessionRoundCount("sess_round")
	if err != nil {
		t.Fatalf("第二次递增返回错误: %v", err)
	}
	if newRound2 != 2 {
		t.Errorf("newRound2 = %d, want 2", newRound2)
	}
}

// TestClearAllSessionMetadataCache 验证全量缓存清除
func TestClearAllSessionMetadataCache(t *testing.T) {
	// 设置一些缓存
	deliveryContextMu.Lock()
	deliveryContextCache["sess1"] = map[string]any{"session_id": "sess1"}
	deliveryContextCache["sess2"] = map[string]any{"session_id": "sess2"}
	deliveryContextMu.Unlock()

	ClearAllSessionMetadataCache()

	deliveryContextMu.RLock()
	lenCache := len(deliveryContextCache)
	deliveryContextMu.RUnlock()

	if lenCache != 0 {
		t.Errorf("缓存应完全清除, 实际剩余 %d 条", lenCache)
	}
}

// TestFlushMetadataQueue 验证刷盘哨兵项被消费
func TestFlushMetadataQueue(t *testing.T) {
	// 确保初始化 worker
	FlushMetadataQueue()
	// 无 panic 或 error 即通过
}
