package server

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// wsTestEnv 隔离 DATA_DIR 并建会话 uploads。
func wsTestEnv(t *testing.T, sid string) string {
	t.Helper()
	root := t.TempDir()
	uploads := filepath.Join(root, "agent", "sessions", sid, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UAPCLAW_DATA_DIR", root)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return uploads
}

// TestHandleWorkspaceFiles_短路分发 文件域 method 不经 agentRuntime.Invoke 直接承接。
// Python: workspace_file_adapter.py:129 handle（GatewayAdapter 链先于 agent invoke）。
func TestHandleWorkspaceFiles_短路分发(t *testing.T) {
	s := &AgentServer{}
	sid := "ws-dispatch-sess"
	wsTestEnv(t, sid)

	png, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	params := map[string]any{
		"media_items": []any{
			map[string]any{"type": "image", "filename": "d.png", "mime_type": "image/png",
				"base64_data": base64.StdEncoding.EncodeToString(png)},
		},
	}
	payload, ok, code, err := s.handleWorkspaceFiles(schema.ReqMethodMediaPersist, params, sid)
	if err != nil || !ok || code != "" {
		t.Fatalf("media.persist 短路失败: %v %q %v", ok, code, err)
	}
	items := payload["media_items"].([]any)
	path := items[0].(map[string]any)["path"].(string)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("落盘缺失: %v", err)
	}
}

// TestHandleWorkspaceFiles_非文件域方法返回不承接 非文件域 method 返回 ok=false。
func TestHandleWorkspaceFiles_非文件域方法返回不承接(t *testing.T) {
	s := &AgentServer{}
	payload, ok, _, err := s.handleWorkspaceFiles(schema.ReqMethodChatSend, map[string]any{}, "s")
	if ok || payload != nil || err != nil {
		t.Fatalf("chat.send 不应由文件域承接: %v %v %v", ok, payload, err)
	}
}

// TestHandleWorkspaceFiles_未知文件域方法 未知方法（无法达 Handles 判定，防御性）。
func TestHandleWorkspaceFiles_未知文件域方法(t *testing.T) {
	s := &AgentServer{}
	raw, _ := json.Marshal(map[string]any{})
	_ = raw
	// HandlesWorkspaceFileMethod 不含 im.file_persist（D-27.8-2）——外层 switch
	// 只在 Handles=true 时才调进来，此分支是防御性回退（不承接、不报错）。
	payload, ok, code, err := s.handleWorkspaceFiles(schema.ReqMethod("im.file_persist"), map[string]any{}, "s")
	if ok || payload != nil || err != nil {
		t.Fatalf("im.file_persist 应不承接（D-27.8-2）: %v %q %v", payload, code, err)
	}
}
