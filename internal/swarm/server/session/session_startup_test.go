package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// resetTestWorkspace 重置测试环境的工作区缓存并设置临时数据目录。
func resetTestWorkspace(t *testing.T) {
	t.Helper()
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	path.ResetCache()
}

// writeTestMetaFile 在指定会话目录下写入 metadata.json。
func writeTestMetaFile(t *testing.T, sessionsDir, sessionID, mode string) {
	t.Helper()
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建会话目录失败: %v", err)
	}
	meta := map[string]any{
		"session_id": sessionID,
		"mode":       mode,
		"title":      "测试会话",
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("序列化 metadata 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644); err != nil {
		t.Fatalf("写入 metadata.json 失败: %v", err)
	}
}

// TestRemoveTeamModeSessionDirsAtStartup_删除team会话 验证 mode=team 的目录被删除
func TestRemoveTeamModeSessionDirsAtStartup_删除team会话(t *testing.T) {
	resetTestWorkspace(t)
	sessionsDir := GetSessionsDir()

	// 创建 team 和 non-team 会话目录
	writeTestMetaFile(t, sessionsDir, "sess_team1", "team")
	writeTestMetaFile(t, sessionsDir, "sess_normal1", "single")

	RemoveTeamModeSessionDirsAtStartup()

	// team 目录应被删除
	if _, err := os.Stat(filepath.Join(sessionsDir, "sess_team1")); !os.IsNotExist(err) {
		t.Error("team 模式会话目录应被删除")
	}
	// normal 目录应保留
	if _, err := os.Stat(filepath.Join(sessionsDir, "sess_normal1")); os.IsNotExist(err) {
		t.Error("非 team 模式会话目录不应被删除")
	}
}

// TestRemoveTeamModeSessionDirsAtStartup_保留非team会话 验证 mode!=team 的目录保留
func TestRemoveTeamModeSessionDirsAtStartup_保留非team会话(t *testing.T) {
	resetTestWorkspace(t)
	sessionsDir := GetSessionsDir()

	// 创建多种非 team 会话
	writeTestMetaFile(t, sessionsDir, "sess_single", "single")
	writeTestMetaFile(t, sessionsDir, "sess_unknown", "unknown")

	RemoveTeamModeSessionDirsAtStartup()

	// 所有目录应保留
	if _, err := os.Stat(filepath.Join(sessionsDir, "sess_single")); os.IsNotExist(err) {
		t.Error("single 模式会话目录不应被删除")
	}
	if _, err := os.Stat(filepath.Join(sessionsDir, "sess_unknown")); os.IsNotExist(err) {
		t.Error("unknown 模式会话目录不应被删除")
	}
}

// TestRemoveTeamModeSessionDirsAtStartup_空目录 验证目录不存在时不报错
func TestRemoveTeamModeSessionDirsAtStartup_空目录(t *testing.T) {
	resetTestWorkspace(t)
	// 不创建任何文件，sessionsDir 本身也不存在

	RemoveTeamModeSessionDirsAtStartup()
	// 应无 panic 或错误
}

// TestRemoveTeamModeSessionDirsAtStartup_无metadataDir 验证无 metadata.json 的目录不被删除
func TestRemoveTeamModeSessionDirsAtStartup_无metadataDir(t *testing.T) {
	resetTestWorkspace(t)
	sessionsDir := GetSessionsDir()

	// 创建一个目录但不写 metadata.json
	if err := os.MkdirAll(filepath.Join(sessionsDir, "sess_no_meta"), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	RemoveTeamModeSessionDirsAtStartup()

	// 无 metadata 的目录应保留
	if _, err := os.Stat(filepath.Join(sessionsDir, "sess_no_meta")); os.IsNotExist(err) {
		t.Error("无 metadata.json 的目录不应被删除")
	}
}

// TestGetAllSessionsMetadata_分页 验证 limit/offset 分页
func TestGetAllSessionsMetadata_分页(t *testing.T) {
	resetTestWorkspace(t)
	sessionsDir := GetSessionsDir()

	// 创建 5 个会话
	for i := 0; i < 5; i++ {
		sessionID := "sess_page_" + string(rune('a'+i))
		writeTestMetaFile(t, sessionsDir, sessionID, "single")
		// 修改 last_message_at 以创建不同排序
		sessionDir := filepath.Join(sessionsDir, sessionID)
		meta := map[string]any{
			"session_id":      sessionID,
			"mode":            "single",
			"title":           "测试" + string(rune('a'+i)),
			"last_message_at": float64(1000 + i),
		}
		data, _ := json.MarshalIndent(meta, "", "  ")
		os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644)
	}

	// 测试分页
	sessions, total := GetAllSessionsMetadata(2, 0)
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(sessions) != 2 {
		t.Errorf("len(sessions) = %d, want 2", len(sessions))
	}

	// 第二页
	sessions2, _ := GetAllSessionsMetadata(2, 2)
	if len(sessions2) != 2 {
		t.Errorf("len(sessions2) = %d, want 2", len(sessions2))
	}

	// 第三页只有 1 条
	sessions3, _ := GetAllSessionsMetadata(2, 4)
	if len(sessions3) != 1 {
		t.Errorf("len(sessions3) = %d, want 1", len(sessions3))
	}
}

// TestGetAllSessionsMetadata_跳过心跳 验证 heartbeat_ 前缀被跳过
func TestGetAllSessionsMetadata_跳过心跳(t *testing.T) {
	resetTestWorkspace(t)
	sessionsDir := GetSessionsDir()

	// 创建普通会话和心跳会话
	writeTestMetaFile(t, sessionsDir, "sess_normal", "single")
	writeTestMetaFile(t, sessionsDir, "heartbeat_task1", "single")

	sessions, total := GetAllSessionsMetadata(20, 0)
	if total != 1 {
		t.Errorf("total = %d, want 1（心跳会话被跳过）", total)
	}
	if len(sessions) != 1 {
		t.Errorf("len(sessions) = %d, want 1", len(sessions))
	}
	if sessions[0]["session_id"] != "sess_normal" {
		t.Errorf("session_id = %v, want sess_normal", sessions[0]["session_id"])
	}
}

// TestGetAllSessionsMetadata_空目录 验证空目录返回空列表
func TestGetAllSessionsMetadata_空目录(t *testing.T) {
	resetTestWorkspace(t)
	// sessionsDir 不存在

	sessions, total := GetAllSessionsMetadata(20, 0)
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(sessions) != 0 {
		t.Errorf("len(sessions) = %d, want 0", len(sessions))
	}
}

// TestGetAllSessionsMetadata_排序 验证按 last_message_at 降序排列
func TestGetAllSessionsMetadata_排序(t *testing.T) {
	resetTestWorkspace(t)
	sessionsDir := GetSessionsDir()

	// 创建 3 个会话，不同的 last_message_at
	for i, ts := range []float64{100, 300, 200} {
		sessionID := "sess_sort_" + string(rune('a'+i))
		sessionDir := filepath.Join(sessionsDir, sessionID)
		os.MkdirAll(sessionDir, 0o755)
		meta := map[string]any{
			"session_id":      sessionID,
			"mode":            "single",
			"last_message_at": ts,
		}
		data, _ := json.MarshalIndent(meta, "", "  ")
		os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644)
	}

	sessions, _ := GetAllSessionsMetadata(20, 0)
	if len(sessions) != 3 {
		t.Fatalf("len(sessions) = %d, want 3", len(sessions))
	}
	// 降序排列: 300 > 200 > 100
	firstTS, _ := sessions[0]["last_message_at"].(float64)
	if firstTS != 300 {
		t.Errorf("sessions[0].last_message_at = %v, want 300", firstTS)
	}
	lastTS, _ := sessions[2]["last_message_at"].(float64)
	if lastTS != 100 {
		t.Errorf("sessions[2].last_message_at = %v, want 100", lastTS)
	}
}

// TestGetAllSessionsMetadata_无metadata旧会话 验证无 metadata.json 时构造最小信息
func TestGetAllSessionsMetadata_无metadata旧会话(t *testing.T) {
	resetTestWorkspace(t)
	sessionsDir := GetSessionsDir()

	// 创建一个目录但不写 metadata.json
	sessionDir := filepath.Join(sessionsDir, "sess_old")
	os.MkdirAll(sessionDir, 0o755)

	sessions, total := GetAllSessionsMetadata(20, 0)
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if sessions[0]["session_id"] != "sess_old" {
		t.Errorf("session_id = %v, want sess_old", sessions[0]["session_id"])
	}
	mode, _ := sessions[0]["mode"].(string)
	if mode != "unknown" {
		t.Errorf("mode = %q, want \"unknown\"", mode)
	}
}
