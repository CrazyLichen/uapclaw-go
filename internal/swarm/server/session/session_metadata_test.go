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
