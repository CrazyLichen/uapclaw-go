package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
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
	// GET 请求
	req := httptest.NewRequest(http.MethodGet, "/file-api/ws-debug-config", nil)
	w := httptest.NewRecorder()
	HandleWsDebugConfigGet(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// POST 请求
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

func TestHandleFileContentPost_缺少path(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	wsDir := workspace.AgentWorkspaceDir()
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	body := map[string]any{"content": "no path"}
	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/file-api/file-content", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleFileContentPost(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleFileContentPost_无效JSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/file-api/file-content", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleFileContentPost(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleListFiles_目录不存在(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	wsDir := workspace.AgentWorkspaceDir()
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	req := httptest.NewRequest(http.MethodGet, "/file-api/list-files?dir=/nonexistent", nil)
	w := httptest.NewRecorder()
	HandleListFiles(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleListFiles_路径穿越(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/file-api/list-files?dir=../../etc", nil)
	w := httptest.NewRecorder()
	HandleListFiles(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleListMarkdown_无dir参数(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	wsDir := workspace.AgentWorkspaceDir()
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	req := httptest.NewRequest(http.MethodGet, "/file-api/list-markdown", nil)
	w := httptest.NewRecorder()
	HandleListMarkdown(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleWsDebugConfigPost_无效JSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/file-api/ws-debug-config", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleWsDebugConfigPost(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	assert.Equal(t, http.StatusOK, w.Code)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result["ok"].(bool))
}

// ──────────────────── HandleFileDownload 测试（10.6.24b）────────────────────

// newDownloadServer 构造带 download 路由的测试服务器（独立 mux，不依赖 chi 路由树）。
func newDownloadServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/file-api/download", HandleFileDownload)
	return httptest.NewServer(mux)
}

// TestHandleFileDownload_缺token 400。
// Python: L676-678 missing_token
func TestHandleFileDownload_缺token(t *testing.T) {
	srv := newDownloadServer()
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/file-api/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

// TestHandleFileDownload_无效token 403。
// Python: L685-687 invalid_or_expired_token
func TestHandleFileDownload_无效token(t *testing.T) {
	srv := newDownloadServer()
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/file-api/download?token=abc")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

// TestHandleFileDownload_过期token 403。
// Python: validate_file_download_token 过期 → None → 403
func TestHandleFileDownload_过期token(t *testing.T) {
	srv := newDownloadServer()
	defer srv.Close()
	m := swarmtools.GetWebFileDownloadManager()
	token := m.GenerateToken("/nonexistent", "", -10)
	resp, _ := http.Get(srv.URL + "/file-api/download?token=" + token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

// TestHandleFileDownload_文件不存在 404。
// Python: L689-691 file_not_found
func TestHandleFileDownload_文件不存在(t *testing.T) {
	srv := newDownloadServer()
	defer srv.Close()
	m := swarmtools.GetWebFileDownloadManager()
	token := m.GenerateToken("/nonexistent/file.pdf", "", 600)
	resp, _ := http.Get(srv.URL + "/file-api/download?token=" + token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

// TestHandleFileDownload_成功 全响应头断言 + 64KB 分块流式。
// Python: L693-720（Content-Disposition filename*=UTF-8'' + quote(safe="")）
func TestHandleFileDownload_成功(t *testing.T) {
	srv := newDownloadServer()
	defer srv.Close()
	m := swarmtools.GetWebFileDownloadManager()

	tmp := t.TempDir()
	fp := filepath.Join(tmp, "报告 final.pdf")
	content := strings.Repeat("A", 150000) // >2 个 64KB 块
	require.NoError(t, os.WriteFile(fp, []byte(content), 0o644))

	token := m.GenerateToken(fp, "", 600)
	resp, err := http.Get(srv.URL + "/file-api/download?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/pdf") {
		t.Errorf("Content-Type: %q", ct)
	}
	if resp.Header.Get("Content-Length") != "150000" {
		t.Errorf("Content-Length: %q", resp.Header.Get("Content-Length"))
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment; filename*=UTF-8''") {
		t.Errorf("Content-Disposition: %q", cd)
	}
	// 中文+空格应被百分号编码（Python quote(safe="") 对齐）
	if !strings.Contains(cd, "%E6%8A%A5") || !strings.Contains(cd, "%20") {
		t.Errorf("filename 编码: %q", cd)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control: %q", resp.Header.Get("Cache-Control"))
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 150000 {
		t.Errorf("body 长度: %d", len(body))
	}
}

// TestHandleFileDownload_HEAD 仅头无 body。
// Python: if self.command != "HEAD"
func TestHandleFileDownload_HEAD(t *testing.T) {
	srv := newDownloadServer()
	defer srv.Close()
	m := swarmtools.GetWebFileDownloadManager()
	tmp := t.TempDir()
	fp := filepath.Join(tmp, "h.txt")
	require.NoError(t, os.WriteFile(fp, []byte("head"), 0o644))

	token := m.GenerateToken(fp, "", 600)
	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/file-api/download?token="+token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("HEAD 不应有 body: %d", len(body))
	}
}

// TestHandleFileDownload_目录404 目录路径按 isfile=False 走 404。
// Python: os.path.isfile(目录) = False → file_not_found
func TestHandleFileDownload_目录404(t *testing.T) {
	srv := newDownloadServer()
	defer srv.Close()
	m := swarmtools.GetWebFileDownloadManager()
	tmp := t.TempDir()
	token := m.GenerateToken(tmp, "", 600)
	resp, _ := http.Get(srv.URL + "/file-api/download?token=" + token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("目录应按 isfile=False 走 404, status: %d", resp.StatusCode)
	}
}

// TestQuoteAllForDownload 全量百分号编码对齐 Python quote(safe="")。
// unreserved（RFC 3986）外全编码，十六进制大写，'/' 也编码。
func TestQuoteAllForDownload(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain.txt", "plain.txt"},                                     // 字母数字点保留
		{"a b.pdf", "a%20b.pdf"},                                       // 空格编码
		{"报告.pdf", "%E6%8A%A5%E5%91%8A.pdf"},                         // 中文逐字节 UTF-8 编码
		{"a/b.txt", "a%2Fb.txt"},                                       // 斜杠编码（PathEscape 不编）
		{"x~y-z_w.", "x~y-z_w."},                                       // unreserved 全保留
		{"a+b.txt", "a%2Bb.txt"},                                       // 加号编码
		{"100%.csv", "100%25.csv"},                                     // 百分号本身编码
	}
	for _, c := range cases {
		if got := quoteAllForDownload(c.in); got != c.want {
			t.Errorf("quoteAll(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
