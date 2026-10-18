package memoryrpc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// setupIsolatedHome 隔离 UserHomeDir 缓存（UAPCLAW_HOME 优先于真实 HOME）
func setupIsolatedHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("UAPCLAW_HOME", tmp)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return tmp
}

// ──────────────────────────── isForbiddenEnabled ────────────────────────────

// TestIsForbiddenEnabled_默认关闭 测试无配置时返回 false
func TestIsForbiddenEnabled_默认关闭(t *testing.T) {
	assert.False(t, isForbiddenEnabled(nil))
	assert.False(t, isForbiddenEnabled(map[string]any{}))
	// 非 dict memory 段（对齐 Python isinstance 判断）
	assert.False(t, isForbiddenEnabled(map[string]any{"memory": "not-a-map"}))
}

// TestIsForbiddenEnabled_配置开启 测试 enabled=true 时返回 true
func TestIsForbiddenEnabled_配置开启(t *testing.T) {
	cfg := map[string]any{
		"memory": map[string]any{
			"forbidden_memory_definition": map[string]any{"enabled": true},
		},
	}
	assert.True(t, isForbiddenEnabled(cfg))
}

// TestIsForbiddenEnabled_显式关闭 测试 enabled=false
func TestIsForbiddenEnabled_显式关闭(t *testing.T) {
	cfg := map[string]any{
		"memory": map[string]any{
			"forbidden_memory_definition": map[string]any{"enabled": false},
		},
	}
	assert.False(t, isForbiddenEnabled(cfg))
}

// ──────────────────────────── getCodingMemoryDir ────────────────────────────

// TestGetCodingMemoryDir_有项目目录 测试 basename 命名
func TestGetCodingMemoryDir_有项目目录(t *testing.T) {
	got := getCodingMemoryDir("/ws", "/home/u/proj")
	assert.Equal(t, filepath.Join("/ws", "coding_memory", "proj"), got)
}

// TestGetCodingMemoryDir_无项目目录 测试 default 回退
func TestGetCodingMemoryDir_无项目目录(t *testing.T) {
	got := getCodingMemoryDir("/ws", "")
	assert.Equal(t, filepath.Join("/ws", "coding_memory", "default"), got)
}

// ──────────────────────────── getAllowedDirs ────────────────────────────

// TestGetAllowedDirs_完整白名单 测试六项白名单（隔离 HOME）
func TestGetAllowedDirs_完整白名单(t *testing.T) {
	home := setupIsolatedHome(t)
	dirs := getAllowedDirs("/ws", "/proj")
	assert.Len(t, dirs, 6)
	assert.Contains(t, dirs, filepath.Join("/ws", "memory"))
	assert.Contains(t, dirs, filepath.Join("/ws", "coding_memory", "proj"))
	assert.Contains(t, dirs, filepath.Join("/ws", ".uapclaw"))
	assert.Contains(t, dirs, filepath.Join(home, ".uapclaw"))
	assert.Contains(t, dirs, filepath.Clean("/proj"))
	assert.Contains(t, dirs, filepath.Clean("/proj/.uapclaw"))
}

// TestGetAllowedDirs_无项目目录 测试四项白名单
func TestGetAllowedDirs_无项目目录(t *testing.T) {
	setupIsolatedHome(t)
	dirs := getAllowedDirs("/ws", "")
	assert.Len(t, dirs, 4)
}

// ──────────────────────────── isInAllowedDirs ────────────────────────────

// TestIsInAllowedDirs_前缀判定 测试目录内/外/恰好等于
func TestIsInAllowedDirs_前缀判定(t *testing.T) {
	setupIsolatedHome(t)
	assert.True(t, isInAllowedDirs("/ws/memory/a.md", "/ws", ""))
	assert.True(t, isInAllowedDirs("/ws/memory", "/ws", ""))        // 恰好等于白名单目录
	assert.False(t, isInAllowedDirs("/ws/memoryX/a.md", "/ws", "")) // 前缀字符串伪相似
	assert.False(t, isInAllowedDirs("/etc/passwd", "/ws", ""))      // 完全越权
}

// TestIsInAllowedDirs_项目目录白名单 测试 project_dir 加入后放行
func TestIsInAllowedDirs_项目目录白名单(t *testing.T) {
	setupIsolatedHome(t)
	assert.False(t, isInAllowedDirs("/proj/notes.md", "/ws", ""))
	assert.True(t, isInAllowedDirs("/proj/notes.md", "/ws", "/proj"))
}

// ──────────────────────────── validateEditPath ────────────────────────────

// TestValidateEditPath_白名单内 测试各合法目录
func TestValidateEditPath_白名单内(t *testing.T) {
	setupIsolatedHome(t)
	for _, raw := range []string{
		"/ws/memory/a.md",             // memory 目录
		"/ws/coding_memory/proj/b.md", // coding 目录
		"/ws/.uapclaw/c.md",           // workspace/.uapclaw
		"/ws/UAPCLAWSWARM.md",         // workspace 根特例文件
		"/proj/notes.md",              // project_dir 本身
		"/proj/UAPCLAWSWARM.local.md", // project_dir 根特例文件
	} {
		ok, resolved := validateEditPath(raw, "/ws", "/proj")
		assert.True(t, ok, "应放行: %s", raw)
		assert.Equal(t, filepath.Clean(raw), resolved)
	}
}

// TestValidateEditPath_越权拒绝 测试白名单外路径
func TestValidateEditPath_越权拒绝(t *testing.T) {
	setupIsolatedHome(t)
	for _, raw := range []string{
		"/etc/passwd",
		"/ws/other/x.md",           // workspace 内非白名单子目录
		"/ws/sub/UAPCLAWSWARM.md",  // 特例文件但不在根
		"/outside/UAPCLAWSWARM.md", // 特例文件父目录非 workspace/project_dir
	} {
		ok, msg := validateEditPath(raw, "/ws", "/proj")
		assert.False(t, ok, "应拒绝: %s", raw)
		assert.Contains(t, msg, "Path not in allowed memory directories")
	}
}

// TestValidateEditPath_相对路径解析 测试相对路径基于 workspace 解析
func TestValidateEditPath_相对路径解析(t *testing.T) {
	setupIsolatedHome(t)
	ok, resolved := validateEditPath("memory/a.md", "/ws", "")
	assert.True(t, ok)
	assert.Equal(t, "/ws/memory/a.md", resolved)
}

// TestValidateEditPath_反斜杠归一 测试 Windows 风格路径归一
func TestValidateEditPath_反斜杠归一(t *testing.T) {
	setupIsolatedHome(t)
	ok, resolved := validateEditPath(`\ws\memory\a.md`, "/ws", "")
	assert.True(t, ok)
	assert.Equal(t, "/ws/memory/a.md", resolved)
}

// TestValidateEditPath_项目目录优先 测试相对路径 project_dir 解析在白名单内时优先
func TestValidateEditPath_项目目录优先(t *testing.T) {
	setupIsolatedHome(t)
	// "notes.md" 相对路径：workspace 解析 /ws/notes.md 不在白名单，
	// 但 project_dir 解析 /proj/notes.md 在白名单 → 采 project 解析（对齐 Python L79-82）
	ok, resolved := validateEditPath("notes.md", "/ws", "/proj")
	assert.True(t, ok)
	assert.Equal(t, "/proj/notes.md", resolved)
}

// TestValidateEditPath_用户目录展开 测试 ~/ 展开后白名单判定
func TestValidateEditPath_用户目录展开(t *testing.T) {
	home := setupIsolatedHome(t)
	ok, resolved := validateEditPath("~/.uapclaw/user.md", "/ws", "")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(home, ".uapclaw", "user.md"), resolved)
}

// ──────────────────────────── classifyMemoryFile ────────────────────────────

// TestClassifyMemoryFile_判定链 测试 kind 分类（对齐 Python 判定链顺序）
func TestClassifyMemoryFile_判定链(t *testing.T) {
	home := setupIsolatedHome(t)
	assert.Equal(t, "coding", classifyMemoryFile("/ws/coding_memory/proj/a.md", "/ws"))
	assert.Equal(t, "auto", classifyMemoryFile("/ws/memory/a.md", "/ws"))
	assert.Equal(t, "project", classifyMemoryFile("/ws/.uapclaw/a.md", "/ws"))
	assert.Equal(t, "local", classifyMemoryFile("/proj/x.local.md", "/ws"))
	// ~/.uapclaw 路径同时含 .uapclaw 段 → project 先于 user 命中
	// （对齐 Python：Path.parts 含 ".jiuwen" 段先返回 project，user 分支实际不可达）
	assert.Equal(t, "project", classifyMemoryFile(filepath.Join(home, ".uapclaw", "a.md"), "/ws"))
	assert.Equal(t, "project", classifyMemoryFile("/some/other/path.md", "/ws")) // 兜底
}

// TestClassifyMemoryFile_coding优先 测试 coding_memory 子串最先判定
func TestClassifyMemoryFile_coding优先(t *testing.T) {
	// 路径同时含 coding_memory 与 memory 段，coding 优先（对齐 Python L106-107）
	assert.Equal(t, "coding", classifyMemoryFile("/ws/coding_memory/proj/memory/a.md", "/ws"))
}

// ──────────────────────────── relativePath ────────────────────────────

// TestRelativePath_优先级 测试 project_dir 优先于 workspace
func TestRelativePath_优先级(t *testing.T) {
	setupIsolatedHome(t)
	assert.Equal(t, "src/a.md", relativePath("/proj/src/a.md", "/ws", "/proj"))
	assert.Equal(t, "memory/a.md", relativePath("/ws/memory/a.md", "/ws", "/proj"))
	assert.Equal(t, "/opt/abs/a.md", relativePath("/opt/abs/a.md", "/ws", "")) // 都不在 → 原绝对路径
}

// TestRelativePath_等于根 测试路径恰好等于 project_dir/workspace
func TestRelativePath_等于根(t *testing.T) {
	setupIsolatedHome(t)
	assert.Equal(t, ".", relativePath("/proj", "/ws", "/proj"))
	assert.Equal(t, ".", relativePath("/ws", "/ws", ""))
}

// ──────────────────────────── safeStat / countLines ────────────────────────────

// TestSafeStat_正常与异常 测试 stat 与 OSError 路径
func TestSafeStat_正常与异常(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.md")
	assert.NoError(t, os.WriteFile(p, []byte("hello"), 0o644))
	st := safeStat(p)
	assert.Equal(t, int64(5), st["size"])
	assert.NotZero(t, st["mtime"])

	st2 := safeStat(filepath.Join(tmp, "missing.md"))
	assert.Equal(t, int64(0), st2["size"])
	assert.Equal(t, int64(0), st2["mtime"])
}

// TestCountLines_行数统计 测试正常与 OSError 与空文件
func TestCountLines_行数统计(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.md")
	assert.NoError(t, os.WriteFile(p, []byte("l1\nl2\nl3"), 0o644))
	assert.Equal(t, 3, countLines(p)) // 尾无换行 3 行（对齐 Python 逐行迭代）
	assert.Equal(t, 0, countLines(filepath.Join(tmp, "missing.md")))

	empty := filepath.Join(tmp, "empty.md")
	assert.NoError(t, os.WriteFile(empty, []byte(""), 0o644))
	assert.Equal(t, 0, countLines(empty)) // 空文件 0 行（对齐 Python sum=0）
}

// TestCountLines_带尾换行 测试尾换行语义
func TestCountLines_带尾换行(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.md")
	assert.NoError(t, os.WriteFile(p, []byte("l1\nl2\n"), 0o644))
	// Python 逐行迭代 "l1\nl2\n" → 2 行
	assert.Equal(t, 2, countLines(p))
}

// ──────────────────────────── scanMDFiles ────────────────────────────

// TestScanMDFiles_基本扫描 测试仅 .md + 排序 + 字段
func TestScanMDFiles_基本扫描(t *testing.T) {
	tmp := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "b.md"), []byte("x\ny"), 0o644))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "a.md"), []byte("z"), 0o644))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "c.txt"), []byte("ignore"), 0o644)) // 非 md
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "sub"), 0o755))                     // 子目录忽略

	files := scanMDFiles(tmp, "auto", "/ws", "")
	assert.Len(t, files, 2)
	assert.Equal(t, "a.md", filepath.Base(files[0]["path"].(string))) // 按名排序
	assert.Equal(t, "auto", files[0]["kind"].(string))
	assert.Equal(t, true, files[0]["exists"].(bool))
	assert.Equal(t, 1, files[0]["lines"].(int)) // "z" → 1 行
	// relative_path：tmp 不在 /ws 下 → 原绝对路径（对齐 Python 兜底）
	assert.Equal(t, filepath.Join(tmp, "a.md"), files[0]["relative_path"].(string))

	// b.md 行数："x\ny" → 2 行
	assert.Equal(t, 2, files[1]["lines"].(int))
}

// TestScanMDFiles_目录不存在 测试空结果
func TestScanMDFiles_目录不存在(t *testing.T) {
	files := scanMDFiles("/nonexistent/dir", "auto", "/ws", "")
	assert.Empty(t, files)
}

// ──────────────────────────── isCodeMode ────────────────────────────

// TestIsCodeMode_前缀判定 测试 code 前缀（对齐 Python mode.startswith("code")）
func TestIsCodeMode_前缀判定(t *testing.T) {
	assert.True(t, isCodeMode("code"))
	assert.True(t, isCodeMode("code-x"))
	assert.False(t, isCodeMode("plan"))
	assert.False(t, isCodeMode("agent"))
	assert.False(t, isCodeMode(""))
}

// ──────────────────────────── HandleMemoryEdit ────────────────────────────

// TestHandleMemoryEdit_读已有文件 测试 20 行预览截断
func TestHandleMemoryEdit_读已有文件(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "memory", "a.md")
	assert.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	content := ""
	for i := 0; i < 25; i++ {
		content += "line\n"
	}
	assert.NoError(t, os.WriteFile(target, []byte(content), 0o644))

	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": target})
	assert.Equal(t, true, res["editable"].(bool))
	assert.Equal(t, true, res["exists"].(bool))
	assert.Equal(t, "auto", res["kind"].(string))
	preview := res["content_preview"].(string)
	// 对齐 Python：lines[:20] join + "\n... (truncated)"
	assert.True(t, strings.HasSuffix(preview, "\n... (truncated)"))
	body := strings.TrimSuffix(preview, "\n... (truncated)")
	assert.Equal(t, 20, len(strings.Split(body, "\n")))
}

// TestHandleMemoryEdit_短文件全量预览 测试不超 20 行无截断标记
func TestHandleMemoryEdit_短文件全量预览(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "memory", "b.md")
	assert.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	assert.NoError(t, os.WriteFile(target, []byte("l1\nl2\nl3"), 0o644))

	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": target})
	preview := res["content_preview"].(string)
	assert.Equal(t, "l1\nl2\nl3", preview) // 3 行无截断
}

// TestHandleMemoryEdit_创建新文件 测试 mkdir+touch
func TestHandleMemoryEdit_创建新文件(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "memory", "new.md")
	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": target})
	assert.Equal(t, true, res["editable"].(bool))
	assert.Equal(t, false, res["exists"].(bool))
	assert.Equal(t, "", res["content_preview"].(string))
	assert.FileExists(t, target) // touch 创建
}

// TestHandleMemoryEdit_相对路径 测试相对路径解析后创建
func TestHandleMemoryEdit_相对路径(t *testing.T) {
	tmp := t.TempDir()
	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": "memory/rel.md"})
	assert.Equal(t, true, res["editable"].(bool))
	assert.Equal(t, filepath.Join(tmp, "memory", "rel.md"), res["path"])
}

// TestHandleMemoryEdit_白名单拒绝 测试越权路径 editable=false
func TestHandleMemoryEdit_白名单拒绝(t *testing.T) {
	setupIsolatedHome(t)
	res := HandleMemoryEdit(context.Background(), "/ws", map[string]any{"path": "/etc/passwd"})
	assert.Equal(t, false, res["editable"].(bool))
	assert.Equal(t, false, res["exists"].(bool))
	assert.Equal(t, "unknown", res["kind"].(string))
	assert.Equal(t, "", res["content_preview"].(string))
	// 拒绝时 path 返回原始 raw_path（对齐 Python L255）
	assert.Equal(t, "/etc/passwd", res["path"])
}

// TestHandleMemoryEdit_项目目录参数 测试 project_dir 透传
func TestHandleMemoryEdit_项目目录参数(t *testing.T) {
	tmp := t.TempDir()
	proj := filepath.Join(tmp, "proj")
	assert.NoError(t, os.MkdirAll(proj, 0o755))
	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{
		"path":        "notes.md",
		"project_dir": proj,
	})
	assert.Equal(t, true, res["editable"].(bool))
	assert.Equal(t, filepath.Join(proj, "notes.md"), res["path"])
}

// ──────────────────────────── HandleMemoryOpen ────────────────────────────

// TestHandleMemoryOpen_基本 测试路径返回与 coding 目录条件包含
func TestHandleMemoryOpen_基本(t *testing.T) {
	tmp := t.TempDir()
	res := HandleMemoryOpen(context.Background(), tmp, map[string]any{"project_dir": "/proj"})
	assert.Equal(t, filepath.Join(tmp, "memory"), res["memory_dir"])
	assert.Equal(t, tmp, res["project_memory_dir"])
	assert.Equal(t, "/proj", res["project_dir"])
	_, hasCoding := res["coding_memory_dir"] // 目录不存在时不含
	assert.False(t, hasCoding)
}

// TestHandleMemoryOpen_无项目目录 测试 project_dir 缺省
func TestHandleMemoryOpen_无项目目录(t *testing.T) {
	tmp := t.TempDir()
	res := HandleMemoryOpen(context.Background(), tmp, map[string]any{})
	assert.Equal(t, filepath.Join(tmp, "memory"), res["memory_dir"])
	assert.Equal(t, tmp, res["project_memory_dir"])
	_, hasProj := res["project_dir"]
	assert.False(t, hasProj)
}

// TestHandleMemoryOpen_coding目录存在 测试 isdir 时包含
func TestHandleMemoryOpen_coding目录存在(t *testing.T) {
	tmp := t.TempDir()
	coding := getCodingMemoryDir(tmp, "/proj")
	assert.NoError(t, os.MkdirAll(coding, 0o755))
	res := HandleMemoryOpen(context.Background(), tmp, map[string]any{"project_dir": "/proj"})
	assert.Equal(t, coding, res["coding_memory_dir"])
}
