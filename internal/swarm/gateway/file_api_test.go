package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestHandleFileContentGet(t *testing.T) {
	// 设置临时工作区
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	// 创建测试文件
	wsDir := workspace.AgentWorkspaceDir()
	require.NoError(t, os.MkdirAll(wsDir, 0o755))
	testFile := filepath.Join(wsDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("hello world"), 0o644))

	// 正常读取
	req := httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=test.txt", nil)
	w := httptest.NewRecorder()
	HandleFileContentGet(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "hello world", result["content"])

	// 缺少 path 参数
	req = httptest.NewRequest(http.MethodGet, "/file-api/file-content", nil)
	w = httptest.NewRecorder()
	HandleFileContentGet(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 路径穿越
	req = httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=../../etc/passwd", nil)
	w = httptest.NewRecorder()
	HandleFileContentGet(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 文件不存在
	req = httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=nonexistent.txt", nil)
	w = httptest.NewRecorder()
	HandleFileContentGet(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleFileContentPost(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	wsDir := workspace.AgentWorkspaceDir()
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	// 正常写入 Markdown 文件
	body := map[string]any{"path": "test.md", "content": "# Hello"}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/file-api/file-content", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleFileContentPost(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 验证文件已写入
	data, err := os.ReadFile(filepath.Join(wsDir, "test.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Hello", string(data))

	// 拒绝非 Markdown 文件
	body = map[string]any{"path": "test.txt", "content": "hello"}
	bodyJSON, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/file-api/file-content", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	HandleFileContentPost(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 路径穿越
	body = map[string]any{"path": "../../etc/evil.md", "content": "evil"}
	bodyJSON, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/file-api/file-content", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	HandleFileContentPost(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleListFiles(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	wsDir := workspace.AgentWorkspaceDir()
	require.NoError(t, os.MkdirAll(wsDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "file1.txt"), []byte("1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "file2.txt"), []byte("2"), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/file-api/list-files?dir=/", nil)
	w := httptest.NewRecorder()
	HandleListFiles(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	files, ok := result["files"].([]any)
	require.True(t, ok)
	assert.Len(t, files, 3) // subdir, file1.txt, file2.txt

	// 验证目录排在前面
	first := files[0].(map[string]any)
	assert.Equal(t, "dir", first["type"])
}

func TestHandleListMarkdown(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	wsDir := workspace.AgentWorkspaceDir()
	require.NoError(t, os.MkdirAll(wsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "readme.md"), []byte("# README"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "notes.txt"), []byte("notes"), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/file-api/list-markdown?dir=/", nil)
	w := httptest.NewRecorder()
	HandleListMarkdown(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	files, ok := result["files"].([]any)
	require.True(t, ok)
	assert.Len(t, files, 1) // 仅 readme.md
}

func TestHandleWsDebugConfig(t *testing.T) {
	// GET
	req := httptest.NewRequest(http.MethodGet, "/file-api/ws-debug-config", nil)
	w := httptest.NewRecorder()
	HandleWsDebugConfigGet(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// POST
	body := map[string]any{"port": float64(8080)}
	bodyJSON, _ := json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/file-api/ws-debug-config", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	HandleWsDebugConfigPost(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleRebuildAgentData(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/file-api/rebuild-agent-data", nil)
	w := httptest.NewRecorder()
	HandleRebuildAgentData(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSafeFilePath_路径穿越(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	// 正常路径
	_, err := safeFilePath("foo/bar.txt")
	assert.NoError(t, err)

	// 包含 ..
	_, err = safeFilePath("../etc/passwd")
	assert.Error(t, err)

	// 隐式穿越
	_, err = safeFilePath("foo/../../etc/passwd")
	assert.Error(t, err)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
