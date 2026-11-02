package web

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
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestResolveFilePath_基准为数据根 验证四类白名单根均可解析。
//
// 对齐 Python: (project_root / arg).resolve() + _is_path_under_allowed_root。
func TestResolveFilePath_基准为数据根(t *testing.T) {
	dataDir := setupDataDir(t)

	cases := []string{
		"agent/workspace/AGENT.md",
		"agent/sessions/s1/session.json",
		"agent/.logs/ws-dev.log",
		".agent_teams/team-a/team.db",
		"auto-harness/pkg/runtime.py",
	}
	for _, rel := range cases {
		got, err := resolveFilePath(rel)
		require.NoError(t, err, rel)
		assert.Equal(t, filepath.Join(dataDir, filepath.FromSlash(rel)), got, rel)
	}
}

// TestResolveFilePath_越界拒绝 验证非白名单根与穿越路径均被拒绝。
func TestResolveFilePath_越界拒绝(t *testing.T) {
	setupDataDir(t)

	for _, rel := range []string{
		"../../etc/passwd",
		"agent/../../etc/passwd",
		"config/.env",         // 非白名单根
		"channels/web/x.html", // 非白名单根
		"/etc/passwd",         // 绝对路径
	} {
		_, err := resolveFilePath(rel)
		assert.ErrorIs(t, err, errPathForbidden, rel)
	}
}

// TestResolveFilePath_符号链接逃逸 验证链接到白名单外时按真实路径判定为越界。
func TestResolveFilePath_符号链接逃逸(t *testing.T) {
	dataDir := setupDataDir(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644))

	link := filepath.Join(dataDir, "agent", "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	_, err := resolveFilePath("agent/escape/secret.txt")
	assert.ErrorIs(t, err, errPathForbidden)
}

// TestResolveFilePath_链接前缀加不存在叶子 已存在的链接祖先必须被解析，即使叶子不存在。
//
// 对齐 Python Path.resolve(strict=False)：只解析存在的前缀。原实现"EvalSymlinks 失败
// 就整体退化为 Clean"，会把 agent/linkdir/new.md 当成字面路径，链接目标没被解析。
func TestResolveFilePath_链接前缀加不存在叶子(t *testing.T) {
	dataDir := setupDataDir(t)
	target := filepath.Join(dataDir, "agent", "workspace", "real")
	require.NoError(t, os.MkdirAll(target, 0o755))
	link := filepath.Join(dataDir, "agent", "linkdir")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	got, err := resolveFilePath("agent/linkdir/new.md")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(target, "new.md"), got, "链接祖先必须被解析到真实目录")
}

// TestResolveFilePath_链接前缀指向白名单外 不存在叶子也要按解析后的真实路径判越界。
func TestResolveFilePath_链接前缀指向白名单外(t *testing.T) {
	dataDir := setupDataDir(t)
	outside := t.TempDir()
	link := filepath.Join(dataDir, "agent", "escape-new")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	_, err := resolveFilePath("agent/escape-new/fresh.md")
	assert.ErrorIs(t, err, errPathForbidden, "链接到白名单外时，不存在的新文件也必须被拒")
}

// TestHandleFileContentGet_返回原文 验证 text/plain 原文 + X-Original-Encoding。
//
// 对齐 Python: app_web.py L646-659（不是 JSON 包装）。
func TestHandleFileContentGet_返回原文(t *testing.T) {
	dataDir := setupDataDir(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(dataDir, "agent", "workspace", "AGENT.md"), []byte("# agent\n"), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=agent/workspace/AGENT.md", nil)
	w := httptest.NewRecorder()
	HandleFileContentGet(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "# agent\n", w.Body.String(), "应返回裸文本")
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	assert.Equal(t, "utf-8", w.Header().Get("X-Original-Encoding"))
}

// TestHandleFileContentGet_错误分支 验证缺参/越界/不存在三种错误码。
func TestHandleFileContentGet_错误分支(t *testing.T) {
	dataDir := setupDataDir(t)

	req := httptest.NewRequest(http.MethodGet, "/file-api/file-content", nil)
	w := httptest.NewRecorder()
	HandleFileContentGet(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "missing_file_path", errorCode(t, w.Body.Bytes()))

	req = httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=../etc/passwd", nil)
	w = httptest.NewRecorder()
	HandleFileContentGet(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "forbidden_path", errorCode(t, w.Body.Bytes()))

	req = httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=agent/workspace/none.md", nil)
	w = httptest.NewRecorder()
	HandleFileContentGet(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "file_not_found", resp["error"])
	assert.Equal(t, filepath.Join(dataDir, "agent", "workspace", "none.md"), resp["fullPath"])
}

// TestHandleFileContentGet_编码 验证 gbk 显式解码与 auto 探测。
func TestHandleFileContentGet_编码(t *testing.T) {
	dataDir := setupDataDir(t)
	gbkPath := filepath.Join(dataDir, "agent", "workspace", "gbk.md")
	// "中文" 的 GBK 编码
	require.NoError(t, os.WriteFile(gbkPath, []byte{0xD6, 0xD0, 0xCE, 0xC4}, 0o644))

	req := httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=agent/workspace/gbk.md&encoding=gbk", nil)
	w := httptest.NewRecorder()
	HandleFileContentGet(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "中文", w.Body.String())
	assert.Equal(t, "gbk", w.Header().Get("X-Original-Encoding"))

	req = httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=agent/workspace/gbk.md&encoding=auto", nil)
	w = httptest.NewRecorder()
	HandleFileContentGet(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "中文", w.Body.String())
	assert.Equal(t, "gbk", w.Header().Get("X-Original-Encoding"))

	// 未知编码 → 500（对齐 Python LookupError 未捕获）
	req = httptest.NewRequest(http.MethodGet, "/file-api/file-content?path=agent/workspace/gbk.md&encoding=base64", nil)
	w = httptest.NewRecorder()
	HandleFileContentGet(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleFileContentGet_agentData缺失时自动生成 验证索引缺失时先重建再读。
//
// 对齐 Python: app_web.py L615。
func TestHandleFileContentGet_agentData缺失时自动生成(t *testing.T) {
	dataDir := setupDataDir(t)
	wsDir := filepath.Join(dataDir, "agent", "workspace")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "AGENT.md"), []byte("# agent"), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/file-api/file-content?path="+agentDataRelPath, nil)
	w := httptest.NewRecorder()
	HandleFileContentGet(w, req)
	require.Equal(t, http.StatusOK, w.Code, "缺失时应自动生成后返回")

	var data map[string][]map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &data), "内容应为文件树索引对象: %s", w.Body.String())
	assert.Contains(t, data, "workspace")
}

// TestHandleFileContentPost 验证覆盖写入与全部错误码。
func TestHandleFileContentPost(t *testing.T) {
	dataDir := setupDataDir(t)
	mdPath := filepath.Join(dataDir, "agent", "workspace", "AGENT.md")
	require.NoError(t, os.WriteFile(mdPath, []byte("old"), 0o644))

	body, _ := json.Marshal(map[string]any{"path": "agent/workspace/AGENT.md", "content": "# new"})
	req := httptest.NewRequest(http.MethodPost, "/file-api/file-content", bytes.NewReader(body))
	w := httptest.NewRecorder()
	HandleFileContentPost(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	data, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	assert.Equal(t, "# new", string(data))
}

// TestHandleFileContentPost_错误分支 验证各错误码（对齐 Python L745-780）。
func TestHandleFileContentPost_错误分支(t *testing.T) {
	dataDir := setupDataDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "agent", "workspace", "AGENT.md"), []byte("x"), 0o644))

	cases := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"无效JSON", "invalid", http.StatusBadRequest, "invalid_json"},
		{"缺path", `{"content":"x"}`, http.StatusBadRequest, "missing_file_path"},
		{"空path", `{"path":"   ","content":"x"}`, http.StatusBadRequest, "missing_file_path"},
		{"缺content", `{"path":"agent/workspace/AGENT.md"}`, http.StatusBadRequest, "missing_file_content"},
		{"越界", `{"path":"../evil.md","content":"x"}`, http.StatusForbidden, "forbidden_path"},
		{"非markdown", `{"path":"agent/workspace/a.txt","content":"x"}`, http.StatusBadRequest, "only_markdown_supported"},
		{"文件不存在", `{"path":"agent/workspace/none.md","content":"x"}`, http.StatusNotFound, "file_not_found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/file-api/file-content", bytes.NewReader([]byte(c.body)))
			w := httptest.NewRecorder()
			HandleFileContentPost(w, req)
			assert.Equal(t, c.status, w.Code)
			assert.Equal(t, c.code, errorCode(t, w.Body.Bytes()))
		})
	}
}

// TestHandleFileContentPost_允许mdx 验证 .mdx 也属于 markdown。
func TestHandleFileContentPost_允许mdx(t *testing.T) {
	dataDir := setupDataDir(t)
	mdxPath := filepath.Join(dataDir, "agent", "workspace", "doc.mdx")
	require.NoError(t, os.WriteFile(mdxPath, []byte("old"), 0o644))

	body := []byte(`{"path":"agent/workspace/doc.mdx","content":"new"}`)
	req := httptest.NewRequest(http.MethodPost, "/file-api/file-content", bytes.NewReader(body))
	w := httptest.NewRecorder()
	HandleFileContentPost(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	got, err := os.ReadFile(mdxPath)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
}

// TestHandleListFiles 验证字段契约、目录优先排序与相对数据根路径。
//
// 对齐 Python: app_web.py L575-602。
func TestHandleListFiles(t *testing.T) {
	dataDir := setupDataDir(t)
	wsDir := filepath.Join(dataDir, "agent", "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, "skills"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "AGENT.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "notes.txt"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, ".hidden"), []byte("x"), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/file-api/list-files?dir=agent/workspace", nil)
	w := httptest.NewRecorder()
	HandleListFiles(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Files []fileEntry `json:"files"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	require.NotEmpty(t, resp.Files)
	// 目录在前
	assert.Equal(t, "skills", resp.Files[0].Name)
	assert.True(t, resp.Files[0].IsDirectory)
	assert.False(t, resp.Files[0].IsMarkdown)
	assert.Equal(t, "agent/workspace/skills", resp.Files[0].Path)

	// isMarkdown / isDirectory 字段齐全（TeamPanel 严格校验）
	byName := map[string]fileEntry{}
	for _, f := range resp.Files {
		byName[f.Name] = f
	}
	assert.True(t, byName["AGENT.md"].IsMarkdown)
	assert.False(t, byName["AGENT.md"].IsDirectory)
	assert.Equal(t, "agent/workspace/AGENT.md", byName["AGENT.md"].Path)
	assert.False(t, byName["notes.txt"].IsMarkdown)
	// 对齐 Python: 不过滤隐藏文件（由前端过滤）
	assert.Contains(t, byName, ".hidden")
}

// TestHandleListFiles_错误分支 验证缺 dir 与越界，及目录不存在返回空列表。
func TestHandleListFiles_错误分支(t *testing.T) {
	setupDataDir(t)

	req := httptest.NewRequest(http.MethodGet, "/file-api/list-files", nil)
	w := httptest.NewRecorder()
	HandleListFiles(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "missing_dir", errorCode(t, w.Body.Bytes()))

	req = httptest.NewRequest(http.MethodGet, "/file-api/list-files?dir=../../etc", nil)
	w = httptest.NewRecorder()
	HandleListFiles(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "forbidden_dir", errorCode(t, w.Body.Bytes()))

	// 对齐 Python: 目录不存在返回 200 + 空列表（前端不抛错）
	req = httptest.NewRequest(http.MethodGet, "/file-api/list-files?dir=agent/workspace/none", nil)
	w = httptest.NewRecorder()
	HandleListFiles(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"files":[]}`, w.Body.String())
}

// TestHandleListMarkdown 验证非递归、只取 markdown、只返回 name/path。
//
// 对齐 Python: app_web.py L550-573。
func TestHandleListMarkdown(t *testing.T) {
	dataDir := setupDataDir(t)
	wsDir := filepath.Join(dataDir, "agent", "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, "skills"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "AGENT.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "doc.mdx"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "notes.txt"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "skills", "SKILL.md"), []byte("x"), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/file-api/list-markdown?dir=agent/workspace", nil)
	w := httptest.NewRecorder()
	HandleListMarkdown(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Files []markdownEntry `json:"files"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	names := make([]string, 0, len(resp.Files))
	for _, f := range resp.Files {
		names = append(names, f.Name)
	}
	assert.ElementsMatch(t, []string{"AGENT.md", "doc.mdx"}, names, "只取本层 markdown，不递归")
	assert.Equal(t, "agent/workspace/AGENT.md", resp.Files[0].Path, "按名称小写升序")
}

// TestHandleListMarkdown_错误分支 验证缺 dir、越界、目录不存在。
func TestHandleListMarkdown_错误分支(t *testing.T) {
	setupDataDir(t)

	req := httptest.NewRequest(http.MethodGet, "/file-api/list-markdown", nil)
	w := httptest.NewRecorder()
	HandleListMarkdown(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "missing_dir", errorCode(t, w.Body.Bytes()))

	req = httptest.NewRequest(http.MethodGet, "/file-api/list-markdown?dir=../../etc", nil)
	w = httptest.NewRecorder()
	HandleListMarkdown(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/file-api/list-markdown?dir=agent/workspace/none", nil)
	w = httptest.NewRecorder()
	HandleListMarkdown(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"files":[]}`, w.Body.String())
}

// TestHandleWsDebugConfig 验证 GET/POST 契约 wsDisableCompress。
//
// 对齐 Python: app_web.py L665-672 / L782-802。
func TestHandleWsDebugConfig(t *testing.T) {
	origValue := wsDisableCompress.Load()
	t.Cleanup(func() { wsDisableCompress.Store(origValue) })
	wsDisableCompress.Store(false)

	req := httptest.NewRequest(http.MethodGet, "/file-api/ws-debug-config", nil)
	w := httptest.NewRecorder()
	HandleWsDebugConfigGet(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"wsDisableCompress":false}`, w.Body.String())

	req = httptest.NewRequest(http.MethodPost, "/file-api/ws-debug-config", bytes.NewReader([]byte(`{"wsDisableCompress":true}`)))
	w = httptest.NewRecorder()
	HandleWsDebugConfigPost(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"ok":true,"wsDisableCompress":true}`, w.Body.String())
	assert.True(t, wsDisableCompress.Load())

	// 非 bool → 400
	req = httptest.NewRequest(http.MethodPost, "/file-api/ws-debug-config", bytes.NewReader([]byte(`{"wsDisableCompress":"yes"}`)))
	w = httptest.NewRecorder()
	HandleWsDebugConfigPost(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_ws_disable_compress", errorCode(t, w.Body.Bytes()))

	// 无效 JSON → 400
	req = httptest.NewRequest(http.MethodPost, "/file-api/ws-debug-config", bytes.NewReader([]byte("invalid")))
	w = httptest.NewRecorder()
	HandleWsDebugConfigPost(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_json", errorCode(t, w.Body.Bytes()))
}

// TestHandleRebuildAgentData 验证重建成功与失败。
func TestHandleRebuildAgentData(t *testing.T) {
	dataDir := setupDataDir(t)
	wsDir := filepath.Join(dataDir, "agent", "workspace")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "AGENT.md"), []byte("# agent"), 0o644))

	req := httptest.NewRequest(http.MethodPost, "/file-api/rebuild-agent-data", nil)
	w := httptest.NewRecorder()
	HandleRebuildAgentData(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"ok":true}`, w.Body.String())

	raw, err := os.ReadFile(filepath.Join(wsDir, testAgentDataFileName))
	require.NoError(t, err, "应生成 agent-data.json")

	var data map[string][]map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
	assert.Contains(t, data["workspace"], map[string]any{
		"name":       "AGENT.md",
		"path":       "agent/workspace/AGENT.md",
		"isMarkdown": true,
	})
}

// TestHandleRebuildAgentData_工作区不存在 验证失败返回 rebuild_failed。
func TestHandleRebuildAgentData_工作区不存在(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dataDir)
	workspace.SetUserHome(workspace.UserHomeDir())
	t.Cleanup(func() { workspace.SetUserHome("") })
	// 不创建 config 目录 → AgentWorkspaceDir 走 resources 回退，但 workspace 目录本身缺失
	require.NoError(t, os.RemoveAll(filepath.Join(dataDir, "agent")))

	req := httptest.NewRequest(http.MethodPost, "/file-api/rebuild-agent-data", nil)
	w := httptest.NewRecorder()
	HandleRebuildAgentData(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "rebuild_failed", errorCode(t, w.Body.Bytes()))
}

// TestFileAPIRouter 验证子路由装配与 404 契约。
func TestFileAPIRouter(t *testing.T) {
	setupDataDir(t)

	srv := httptest.NewServer(FileAPIRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/list-files")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp2, err := http.Get(srv.URL + "/no-such-endpoint")
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
	body, _ := io.ReadAll(resp2.Body)
	assert.Equal(t, "not_found", errorCode(t, body))
}

// TestSPAHandler 验证静态资源与 SPA fallback。
func TestSPAHandler(t *testing.T) {
	srv := httptest.NewServer(SPAHandler())
	defer srv.Close()

	for _, p := range []string{"/", "/index.html", "/agents/whatever"} {
		resp, err := http.Get(srv.URL + p)
		require.NoError(t, err, p)
		assert.Equal(t, http.StatusOK, resp.StatusCode, p)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html", p)
		_ = resp.Body.Close()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestResolveFilePath_工具函数 验证 isUnderAllowedRoot / relToDataRoot / isAgentDataRelPath / isMarkdownName。
func TestResolveFilePath_工具函数(t *testing.T) {
	dataDir := setupDataDir(t)

	assert.True(t, isUnderAllowedRoot(filepath.Join(dataDir, "agent", "workspace")))
	assert.True(t, isUnderAllowedRoot(filepath.Join(dataDir, ".agent_teams", "t")))
	assert.True(t, isUnderAllowedRoot(filepath.Join(dataDir, "auto-harness")))
	assert.False(t, isUnderAllowedRoot(filepath.Join(dataDir, "agent2")), "前缀相同但不同目录应拒绝")
	assert.False(t, isUnderAllowedRoot(filepath.Join(dataDir, "config")))

	assert.Equal(t, "agent/workspace/AGENT.md",
		relToDataRoot(filepath.Join(dataDir, "agent", "workspace", "AGENT.md")))

	assert.True(t, isAgentDataRelPath("agent/workspace/agent-data.json"))
	assert.True(t, isAgentDataRelPath("./agent/workspace/agent-data.json"))
	assert.True(t, isAgentDataRelPath(`agent\workspace\agent-data.json`))
	assert.False(t, isAgentDataRelPath("agent-data.json"))
	assert.False(t, isAgentDataRelPath("agent/workspace/other.json"))

	assert.True(t, isMarkdownName("a.md"))
	assert.True(t, isMarkdownName("a.MDX"))
	assert.False(t, isMarkdownName("a.txt"))
	assert.True(t, isMarkdownPath("/tmp/a.md"))
}

// TestReadFileWithEncoding_未知编码 验证查表失败与严格解码失败。
func TestReadFileWithEncoding_未知编码(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	require.NoError(t, os.WriteFile(path, []byte{0xFF, 0xFE, 0x00}, 0o644))

	_, _, err := readFileWithEncoding(path, "no-such-encoding")
	assert.Error(t, err)

	// 非法 UTF-8 字节流按 utf-8 解码失败
	_, _, err = readFileWithEncoding(path, "utf-8")
	assert.Error(t, err)

	// 读不存在的文件
	_, _, err = readFileWithEncoding(filepath.Join(dir, "missing"), "utf-8")
	assert.Error(t, err)
}

// TestDecodeStrict 验证非法字节被判为解码失败。
func TestDecodeStrict(t *testing.T) {
	_, ok := decodeStrict([]byte("hello"), charmap.ISO8859_1)
	assert.True(t, ok)

	// GBK 无法解码的非法字节序列
	_, ok = decodeStrict([]byte{0x81, 0x20, 0xFF}, simplifiedchinese.GBK)
	assert.False(t, ok)
}

// TestWriteJSON 验证错误码写入。
func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	assert.Equal(t, http.StatusOK, w.Code)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])

	w = httptest.NewRecorder()
	writeJSONError(w, http.StatusForbidden, "forbidden_path")
	assert.Equal(t, "forbidden_path", errorCode(t, w.Body.Bytes()))

	w = httptest.NewRecorder()
	writeJSONDetail(w, http.StatusInternalServerError, "generate_failed", "boom")
	var detail map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	assert.Equal(t, "generate_failed", detail["error"])
	assert.Equal(t, "boom", detail["detail"])
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

// TestHandleFileDownload_无效token 403。
// Python: L685-687 invalid_or_expired_token
func TestHandleFileDownload_无效token(t *testing.T) {
	srv := newDownloadServer()
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/file-api/download?token=abc")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
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
	resp, err := http.Get(srv.URL + "/file-api/download?token=" + token)
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
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
	resp, err := http.Get(srv.URL + "/file-api/download?token=" + token)
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

// TestHandleFileDownload_成功 全响应头断言 + 64KB 分块流式。
// Python: L693-720（Content-Disposition filename*=UTF-8 加两个连续单引号前缀 + quote(safe="")）
// 注：RFC 5987 语法的连续双单引号字面量不直接写入注释——go1.26 gofmt 对
// doc comment 做 smart-quote 规范化（ASCII 排版惯例中两个连续单引号被视为
// Unicode 右双引号的等价排版），会被静默改写导致与原文不符。
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
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
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
	resp, err := http.Get(srv.URL + "/file-api/download?token=" + token)
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("目录应按 isfile=False 走 404, status: %d", resp.StatusCode)
	}
}

// TestQuoteAll 全量百分号编码对齐 Python quote(safe="")。
// unreserved（RFC 3986）外全编码，十六进制大写，'/' 也编码。
func TestQuoteAll(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain.txt", "plain.txt"},           // 字母数字点保留
		{"a b.pdf", "a%20b.pdf"},             // 空格编码
		{"报告.pdf", "%E6%8A%A5%E5%91%8A.pdf"}, // 中文逐字节 UTF-8 编码
		{"a/b.txt", "a%2Fb.txt"},             // 斜杠编码（PathEscape 不编）
		{"x~y-z_w.", "x~y-z_w."},             // unreserved 全保留
		{"a+b.txt", "a%2Bb.txt"},             // 加号编码
		{"100%.csv", "100%25.csv"},           // 百分号本身编码
	}
	for _, c := range cases {
		if got := quoteAll(c.in); got != c.want {
			t.Errorf("quoteAll(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
