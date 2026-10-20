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

// ──────────────────────────── isForbiddenEnabled 禁止开关判定 ────────────────────────────

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

// ──────────────────────────── getCodingMemoryDir 编码记忆目录获取 ────────────────────────────

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

// ──────────────────────────── getAllowedDirs 允许目录列表 ────────────────────────────

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

// ──────────────────────────── isInAllowedDirs 目录白名单判定 ────────────────────────────

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

// ──────────────────────────── validateEditPath 编辑路径校验 ────────────────────────────

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

// ──────────────────────────── classifyMemoryFile 记忆文件分类 ────────────────────────────

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

// ──────────────────────────── relativePath 相对路径 ────────────────────────────

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

// ──────────────────────────── safeStat 安全取属性 / countLines 统计行数 ────────────────────────────

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

// ──────────────────────────── scanMDFiles 扫描 Markdown 文件 ────────────────────────────

// TestScanMDFiles_基本扫描 测试仅 .md + 排序 + 字段
func TestScanMDFiles_基本扫描(t *testing.T) {
	tmp := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "b.md"), []byte("x\ny"), 0o644))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "a.md"), []byte("z"), 0o644))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "c.txt"), []byte("ignore"), 0o644)) // 非 md
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "sub"), 0o755))                      // 子目录忽略

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

// ──────────────────────────── isCodeMode Code 模式判定 ────────────────────────────

// TestIsCodeMode_前缀判定 测试 code 前缀（对齐 Python mode.startswith("code")）
func TestIsCodeMode_前缀判定(t *testing.T) {
	assert.True(t, isCodeMode("code"))
	assert.True(t, isCodeMode("code-x"))
	assert.False(t, isCodeMode("plan"))
	assert.False(t, isCodeMode("agent"))
	assert.False(t, isCodeMode(""))
}

// ──────────────────────────── HandleMemoryEdit 记忆编辑处理 ────────────────────────────

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

// ──────────────────────────── HandleMemoryOpen 记忆打开处理 ────────────────────────────

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

// ──────────────────────────── HandleMemoryList 记忆列表处理 ────────────────────────────

// TestHandleMemoryList_三路合并 测试 project + auto + coding 三路
func TestHandleMemoryList_三路合并(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	// project 层需要 root marker（对齐 Python find_project_root 语义）
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, ".uapclaw"), 0o755))
	// project 层：UAPCLAWSWARM.md 在 workspace 根
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "UAPCLAWSWARM.md"), []byte("p\n"), 0o644))
	// auto 层：workspace/memory
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "memory", "a.md"), []byte("m1\nm2"), 0o644))
	// coding 层：workspace/coding_memory/default（不传 project_dir）
	coding := getCodingMemoryDir(tmp, "")
	assert.NoError(t, os.MkdirAll(coding, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(coding, "c.md"), []byte("c"), 0o644))

	// project 层发现不传 project_dir（Python L211：project_dir or workspace，
	// 传 project_dir 后发现目标变为 project_dir；本场景针对 workspace 自身）
	res := HandleMemoryList(context.Background(), tmp, "plan", map[string]any{})
	files := res["files"].([]map[string]any)

	byPath := map[string]map[string]any{}
	for _, f := range files {
		byPath[f["path"].(string)] = f
	}
	assert.Len(t, byPath, 3)
	assert.Equal(t, "project", byPath[filepath.Join(tmp, "UAPCLAWSWARM.md")]["kind"])
	// project 文件行数：content 口径（frontmatter 剥离后 "p"）→ "p".count("\n")+1 = 1
	assert.Equal(t, 1, byPath[filepath.Join(tmp, "UAPCLAWSWARM.md")]["lines"])
	auto := byPath[filepath.Join(tmp, "memory", "a.md")]
	assert.Equal(t, "auto", auto["kind"])
	// auto 文件行数：countLines 口径 "m1\nm2" → 2
	assert.Equal(t, 2, auto["lines"])
	// coding 层：不传 project_dir → coding_memory/default
	codingDefault := getCodingMemoryDir(tmp, "")
	codingFile := byPath[filepath.Join(codingDefault, "c.md")]
	assert.Equal(t, "coding", codingFile["kind"])
	assert.Equal(t, 1, codingFile["lines"])
}

// TestHandleMemoryList_空内容文件被丢弃 测试空 UAPCLAWSWARM.md 不进列表
// （对齐 Python project_memory _load_path L553-555：body_stripped 为空即 return；
// memory_rpc L226 的 else 0 分支为防御性写法，实际空文件在发现层已被丢弃）
func TestHandleMemoryList_空内容文件被丢弃(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, ".uapclaw"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "UAPCLAWSWARM.md"), []byte(""), 0o644))

	res := HandleMemoryList(context.Background(), tmp, "plan", map[string]any{})
	files := res["files"].([]map[string]any)
	assert.Empty(t, files) // 空内容在发现层被丢弃
}

// TestHandleMemoryList_无marker无project层 测试无 root marker 时 project 层跳过（对齐 Python find_project_root）
func TestHandleMemoryList_无marker无project层(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "UAPCLAWSWARM.md"), []byte("p"), 0o644))
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "memory", "a.md"), []byte("m"), 0o644))

	res := HandleMemoryList(context.Background(), tmp, "plan", map[string]any{})
	files := res["files"].([]map[string]any)
	// 无 marker：UAPCLAWSWARM.md 不被发现，仅 auto 层 1 个文件
	assert.Len(t, files, 1)
	assert.Equal(t, "auto", files[0]["kind"])
}

// TestHandleMemoryList_去重 测试同 path 只保留首份（project 优先于 auto）
func TestHandleMemoryList_去重(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "memory", "dup.md"), []byte("x"), 0o644))

	// project 发现（workspace 根扫描）与 memory 目录扫描都覆盖 memory/dup.md？
	// Python 语义：project_files 来自 discover_and_load_memory_files（不含 memory/ 子目录扫描），
	// auto 扫描补 memory/ 下文件，seen_paths 保证跨路去重
	res := HandleMemoryList(context.Background(), tmp, "plan", map[string]any{})
	files := res["files"].([]map[string]any)
	dupCount := 0
	for _, f := range files {
		if strings.HasSuffix(f["path"].(string), "dup.md") {
			dupCount++
		}
	}
	assert.Equal(t, 1, dupCount)
}

// TestHandleMemoryList_不含项目 测试 include_project=false
func TestHandleMemoryList_不含项目(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "UAPCLAWSWARM.md"), []byte("p"), 0o644))
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "memory", "a.md"), []byte("m"), 0o644))

	res := HandleMemoryList(context.Background(), tmp, "plan", map[string]any{"include_project": false})
	files := res["files"].([]map[string]any)
	for _, f := range files {
		assert.NotContains(t, f["path"], "UAPCLAWSWARM.md")
	}
	assert.Len(t, files, 1)
}

// TestHandleMemoryList_空结果 测试无任何记忆文件时 files 为空数组
func TestHandleMemoryList_空结果(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	res := HandleMemoryList(context.Background(), tmp, "plan", map[string]any{})
	files := res["files"].([]map[string]any)
	assert.NotNil(t, files)
	assert.Empty(t, files)
}

// TestHandleMemoryList_额外目录参数 测试 additional_directories 透传
func TestHandleMemoryList_额外目录参数(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	extra := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(extra, "UAPCLAWSWARM.md"), []byte("extra"), 0o644))

	res := HandleMemoryList(context.Background(), tmp, "plan", map[string]any{
		"additional_directories": []any{extra},
	})
	files := res["files"].([]map[string]any)
	found := false
	for _, f := range files {
		if f["path"] == filepath.Join(extra, "UAPCLAWSWARM.md") {
			found = true
		}
	}
	assert.True(t, found, "additional_directories 应被发现")
}

// ──────────────────────────── HandleMemoryStatus 记忆状态处理 ────────────────────────────

// TestHandleMemoryStatus_基本块 测试非 detailed 返回
func TestHandleMemoryStatus_基本块(t *testing.T) {
	setupToggleEnv(t, "memory:\n  mode: sqlite\n")
	res := HandleMemoryStatus(context.Background(), "/ws", "plan", map[string]any{})
	assert.Equal(t, "plan", res["current_mode"])
	assert.Equal(t, "sqlite", res["storage_mode"])
	assert.Equal(t, "builtin", res["engine"]) // 默认 engine（空/非法回落 builtin）
	assert.Equal(t, false, res["enabled"])    // 空配置 IsMemoryEnabled → false（对齐 Python 缺省）
	assert.Equal(t, false, res["proactive"])
	assert.Equal(t, false, res["forbidden_enabled"])
	_, hasIndex := res["index"]
	assert.False(t, hasIndex) // 非 detailed 无 index
}

// TestHandleMemoryStatus_配置驱动基本块 测试配置各字段生效
func TestHandleMemoryStatus_配置驱动基本块(t *testing.T) {
	setupToggleEnv(t, "modes:\n  agent:\n    plan:\n      memory:\n        enabled: true\n        is_proactive: true\nmemory:\n  forbidden_memory_definition:\n    enabled: true\n")
	res := HandleMemoryStatus(context.Background(), "/ws", "plan", map[string]any{})
	assert.Equal(t, true, res["enabled"])
	assert.Equal(t, true, res["proactive"])
	assert.Equal(t, true, res["forbidden_enabled"])
	assert.Equal(t, "local", res["storage_mode"]) // memory 段无 mode → local
}

// TestHandleMemoryStatus_code模式特判 测试 enabled=true/proactive=false 硬编码
func TestHandleMemoryStatus_code模式特判(t *testing.T) {
	setupToggleEnv(t, "")
	res := HandleMemoryStatus(context.Background(), "/ws", "code", map[string]any{})
	assert.Equal(t, true, res["enabled"])
	assert.Equal(t, false, res["proactive"])
}

// TestHandleMemoryStatus_detailed六块 测试 detailed 全字段
func TestHandleMemoryStatus_detailed六块(t *testing.T) {
	envDir := setupToggleEnv(t, "")
	ws := filepath.Join(envDir, "ws")
	assert.NoError(t, os.MkdirAll(filepath.Join(ws, "memory"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(ws, "memory", "a.md"), []byte("hello"), 0o644))

	res := HandleMemoryStatus(context.Background(), ws, "plan", map[string]any{"detailed": true, "project_dir": "/proj"})

	// 块 1：index（GetMemoryManager nil settings 创建真实 manager）
	idx, hasIdx := res["index"].(map[string]any)
	assert.True(t, hasIdx)
	assert.Contains(t, idx, "available")
	assert.Contains(t, idx, "provider")
	assert.Contains(t, idx, "files_count")
	assert.Contains(t, idx, "chunks_count")

	// 块 2：project_memory（/proj 不存在 → 0 文件）
	pm, hasPm := res["project_memory"].(map[string]any)
	assert.True(t, hasPm)
	assert.Equal(t, 0, pm["files_count"])
	assert.Equal(t, 60000, pm["max_chars"])
	assert.Equal(t, 40000, pm["threshold"])
	assert.Equal(t, "/proj", pm["project_dir"])

	// 块 3：large_files 键存在（可为空数组）
	assert.Contains(t, res, "large_files")

	// 块 4：coding_memory（不存在 → files_count 0，dir 空串）
	cm, hasCm := res["coding_memory"].(map[string]any)
	assert.True(t, hasCm)
	assert.Equal(t, 0, cm["files_count"])
	assert.Equal(t, "", cm["dir"])

	// 块 5：auto_memory（1 文件 5 字符）
	am, hasAm := res["auto_memory"].(map[string]any)
	assert.True(t, hasAm)
	assert.Equal(t, 1, am["files_count"])
	assert.Equal(t, 5, am["total_chars"])
	assert.NotEmpty(t, am["dir"])

	// 块 6：external_memory：engine=builtin 时不含
	_, hasExt := res["external_memory"]
	assert.False(t, hasExt)
}

// TestHandleMemoryStatus_detailed大文件 测试 large_files 超阈值上报
func TestHandleMemoryStatus_detailed大文件(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, ".uapclaw"), 0o755))
	// 41000 字符 > 40000 阈值
	big := strings.Repeat("x", 41000)
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "UAPCLAWSWARM.md"), []byte(big), 0o644))

	res := HandleMemoryStatus(context.Background(), tmp, "plan", map[string]any{"detailed": true})
	large, ok := res["large_files"].([]map[string]any)
	assert.True(t, ok)
	if assert.Len(t, large, 1) {
		assert.Equal(t, 41000, large[0]["char_count"])
	}
}

// TestHandleMemoryStatus_external块 测试 engine=external 时含 external_memory
// Python L381-387：external_memory 块在 if detailed: 分支内，必须传 detailed=true
func TestHandleMemoryStatus_external块(t *testing.T) {
	envDir := setupToggleEnv(t, "memory:\n  mode: local\n  engine: external\n  external:\n    provider: mem0\n    mem0:\n      api_key: test\n")
	ws := filepath.Join(envDir, "ws")
	assert.NoError(t, os.MkdirAll(ws, 0o755))
	res := HandleMemoryStatus(context.Background(), ws, "plan", map[string]any{"detailed": true})
	ext, ok := res["external_memory"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "mem0", ext["provider"]) // provider 原样返回（对齐 Python .get 不触发默认）
	assert.Equal(t, true, ext["enabled"])
}

// TestHandleMemoryStatus_engineBoth 测试 engine=both 时含 external_memory
func TestHandleMemoryStatus_engineBoth(t *testing.T) {
	envDir := setupToggleEnv(t, "memory:\n  engine: both\n  external:\n    provider: openjiuwen\n")
	ws := filepath.Join(envDir, "ws")
	assert.NoError(t, os.MkdirAll(ws, 0o755))
	res := HandleMemoryStatus(context.Background(), ws, "plan", map[string]any{"detailed": true})
	ext, ok := res["external_memory"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "openjiuwen", ext["provider"])
	assert.Equal(t, true, ext["enabled"])
}

// ──────────────────────────── 覆盖率补漏 ────────────────────────────

// TestHandleMemoryEdit_不存在文件创建 测试 mkdir+touch 创建路径
func TestHandleMemoryEdit_不存在文件创建(t *testing.T) {
	home := setupIsolatedHome(t)
	// ~/ .uapclaw 深层路径：touch 时 parent 不存在 → mkdir -p + touch
	p := filepath.Join(home, ".uapclaw", "deep", "nested", "note.md")
	res := HandleMemoryEdit(context.Background(), "/ws", map[string]any{"path": p})
	assert.Equal(t, p, res["path"])
	assert.Equal(t, false, res["exists"])
	assert.Equal(t, true, res["editable"])
	_, statErr := os.Stat(p)
	assert.NoError(t, statErr, "touch 应创建文件")
}

// TestHandleMemoryEdit_超20行截断 测试 content_preview 截断分支
func TestHandleMemoryEdit_超20行截断(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	// workspace/memory 在白名单内
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	p := filepath.Join(tmp, "memory", "a.md")
	var sb strings.Builder
	for i := 0; i < 25; i++ {
		sb.WriteString("line\n")
	}
	assert.NoError(t, os.WriteFile(p, []byte(sb.String()), 0o644))

	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": p})
	assert.Equal(t, true, res["exists"])
	preview, _ := res["content_preview"].(string)
	assert.True(t, strings.HasSuffix(preview, "\n... (truncated)"),
		"超 20 行应截断，got %d chars: %q", len(preview), preview)
}

// TestHandleMemoryEdit_编辑目录 测试 path 是目录时 exists=false
func TestHandleMemoryEdit_编辑目录(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	// memory 目录在白名单内但它是目录 → exists=false，进入 touch 分支（目录已存在创建失败不 panic）
	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": filepath.Join(tmp, "memory")})
	assert.Equal(t, false, res["exists"])
	assert.Equal(t, true, res["editable"]) // 路径合法仍可编辑
}

// TestHandleMemoryList_包含项目目录 测试 project_dir 改变发现根
func TestHandleMemoryList_包含项目目录(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, ".uapclaw"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "UAPCLAWSWARM.md"), []byte("p"), 0o644))

	res := HandleMemoryList(context.Background(), tmp, "plan", map[string]any{"project_dir": tmp})
	files, _ := res["files"].([]map[string]any)
	assert.NotEmpty(t, files, "project_dir 指向自身时应发现 UAPCLAWSWARM.md")
}

// TestExpandUser_分支覆盖 测试 ~ 与 ~/ 前缀之外的分支
func TestExpandUser_分支覆盖(t *testing.T) {
	setupIsolatedHome(t)
	assert.Equal(t, "/abs/path", expandUser("/abs/path"))
	assert.Equal(t, "plain", expandUser("plain"))
	assert.Equal(t, "a~b", expandUser("a~b")) // 非前缀 ~ 不展开
}

// TestAnyOr_默认值 测试 anyOr nil/非 nil 分支
func TestAnyOr_默认值(t *testing.T) {
	assert.Equal(t, "fallback", anyOr(nil, "fallback"))
	assert.Equal(t, "value", anyOr("value", "fallback"))
}

// TestDirOrEmpty_分支覆盖 测试目录存在/不存在
func TestDirOrEmpty_分支覆盖(t *testing.T) {
	tmp := t.TempDir()
	assert.Equal(t, tmp, dirOrEmpty(tmp))
	assert.Equal(t, "", dirOrEmpty(filepath.Join(tmp, "missing")))
	// 文件（非目录）也应返回空串
	p := filepath.Join(tmp, "file.md")
	assert.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	assert.Equal(t, "", dirOrEmpty(p))
}

// TestLoadConfigMap_读取失败 测试 YAML 损坏时返回空 map
func TestLoadConfigMap_读取失败(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	assert.NoError(t, os.MkdirAll(cfgDir, 0o755))
	// 非法 YAML（未闭合的 flow sequence）
	assert.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("memory: [unclosed"), 0o644))
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	t.Setenv("UAPCLAW_HOME", filepath.Join(tmp, "home"))
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	assert.Equal(t, map[string]any{}, loadConfigMap())
}

// TestUpdateModeMemoryConfig_Load失败 测试 YAML 损坏时返回 error
func TestUpdateModeMemoryConfig_Load失败(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	assert.NoError(t, os.MkdirAll(cfgDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("modes: {unclosed"), 0o644))
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	t.Setenv("UAPCLAW_HOME", filepath.Join(tmp, "home"))
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	assert.Error(t, updateModeMemoryConfig("plan", "enabled", true))
	assert.Error(t, updateMemoryForbiddenEnabledInConfig(true))
}

// TestUpdateModeMemoryConfig_写回失败 测试 Set 失败路径（目录替换文件导致写回失败）
func TestUpdateModeMemoryConfig_写回失败(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	assert.NoError(t, os.MkdirAll(cfgDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("modes:\n  agent:\n    plan:\n      memory:\n        enabled: true\n"), 0o644))
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	t.Setenv("UAPCLAW_HOME", filepath.Join(tmp, "home"))
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	// config.yaml 所在目录被替换为同名目录（写回时 MkdirAll 失败）
	assert.NoError(t, os.Remove(filepath.Join(cfgDir, "config.yaml")))
	assert.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "config.yaml"), 0o755))

	assert.Error(t, updateModeMemoryConfig("plan", "enabled", false))
}

// TestHandleMemoryToggle_写回失败global 测试 D6 决策：写回失败时 new_value 置 old_value
func TestHandleMemoryToggle_写回失败global(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	assert.NoError(t, os.MkdirAll(cfgDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("memory:\n  forbidden_memory_definition:\n    enabled: true\n"), 0o644))
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	t.Setenv("UAPCLAW_HOME", filepath.Join(tmp, "home"))
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	// config.yaml 被同名目录替换 → 写回失败
	assert.NoError(t, os.Remove(filepath.Join(cfgDir, "config.yaml")))
	assert.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "config.yaml"), 0o755))

	res := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{"key": "memory_forbidden_enabled"})
	assert.Equal(t, "global", res["mode_affected"])
	assert.Equal(t, true, res["needs_restart"])
	assert.Equal(t, res["old_value"], res["new_value"]) // D6：写回失败如实上报未变更
}

// TestHandleMemoryToggle_写回失败mode 测试模式级开关写回失败分支
func TestHandleMemoryToggle_写回失败mode(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	assert.NoError(t, os.MkdirAll(cfgDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(""), 0o644))
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	t.Setenv("UAPCLAW_HOME", filepath.Join(tmp, "home"))
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	assert.NoError(t, os.Remove(filepath.Join(cfgDir, "config.yaml")))
	assert.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "config.yaml"), 0o755))

	// memory_enabled / memory_proactive 两分支（写回失败 → new=old）
	res1 := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{"key": "memory_enabled"})
	assert.Equal(t, "plan", res1["mode_affected"])
	assert.Equal(t, res1["old_value"], res1["new_value"])

	res2 := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{"key": "memory_proactive"})
	assert.Equal(t, "plan", res2["mode_affected"])
	assert.Equal(t, res2["old_value"], res2["new_value"])
}

// TestHandleMemoryStatus_detailedCoding统计 测试 coding_memory 逐文件字符统计分支
func TestHandleMemoryStatus_detailedCoding统计(t *testing.T) {
	envDir := setupToggleEnv(t, "")
	ws := filepath.Join(envDir, "ws")
	// coding_memory/default 目录 + 1 个 .md 文件（5 字符）
	assert.NoError(t, os.MkdirAll(filepath.Join(ws, "coding_memory", "default"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(ws, "coding_memory", "default", "c1.md"), []byte("hello"), 0o644))

	res := HandleMemoryStatus(context.Background(), ws, "plan", map[string]any{"detailed": true})
	cm, ok := res["coding_memory"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, 1, cm["files_count"])
	assert.Equal(t, 5, cm["total_chars"])
	assert.NotEmpty(t, cm["dir"]) // 目录存在返回原路径
}

// TestExpandUser_波浪号 测试单独 ~ 展开为 HOME
func TestExpandUser_波浪号(t *testing.T) {
	home := setupIsolatedHome(t)
	assert.Equal(t, home, expandUser("~"))
}

// TestAbsoluteJoin_相对基路径 测试 base 相对路径时补 cwd（对齐 os.path.abspath）
func TestAbsoluteJoin_相对基路径(t *testing.T) {
	joined := absoluteJoin("relbase", "sub/a.md")
	abs, err := filepath.Abs("relbase/sub/a.md")
	assert.NoError(t, err)
	assert.Equal(t, abs, joined)
}

// TestScanMDFiles_目录变文件 测试 dir 状态竞争：Stat 时是目录、ReadDir 时失败
func TestScanMDFiles_目录变文件(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	// 先建目录并通过白名单判定，再替换为文件：ReadDir 返回 ENOTDIR → nil
	dir := filepath.Join(tmp, "memory")
	assert.NoError(t, os.MkdirAll(dir, 0o755))
	assert.NoError(t, os.RemoveAll(dir))
	assert.NoError(t, os.WriteFile(dir, []byte("now a file"), 0o644))
	// Stat 成功但非目录 → 走 isdir=false 分支
	assert.Nil(t, scanMDFiles(dir, "auto", tmp, ""))
}

// TestHandleMemoryEdit_创建失败 测试 mkdir/touch 失败分支（parent 路径上有同名文件）
func TestHandleMemoryEdit_创建失败(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	// memory 是文件（非目录）：tmp/memory/deep/a.md 前缀匹配白名单（字符串判定），
	// 但 MkdirAll(tmp/memory/deep) 与 OpenFile(tmp/memory/deep/a.md) 均失败 → 双 Error 分支
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "memory"), []byte("blocker"), 0o644))
	p := filepath.Join(tmp, "memory", "deep", "a.md")

	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": p})
	assert.Equal(t, p, res["path"])
	assert.Equal(t, false, res["exists"])
	assert.Equal(t, true, res["editable"]) // 路径在白名单内仍报 editable（对齐 Python 不回滚 editable）
}

// TestValidateEditPath_项目根特例 测试 UAPCLAWSWARM.md 在 project_dir 根的判定路径
func TestValidateEditPath_项目根特例(t *testing.T) {
	setupIsolatedHome(t)
	// 有 project_dir 时 /proj 本身在白名单内（_get_allowed_dirs L48-49），
	// /proj/UAPCLAWSWARM.md 经前缀匹配直接放行（走不到 basename 特例分支）
	ok, resolved := validateEditPath("/proj/UAPCLAWSWARM.md", "/ws", "/proj")
	assert.True(t, ok)
	assert.Equal(t, "/proj/UAPCLAWSWARM.md", resolved)

	// UAPCLAWSWARM.local.md 同理（白名单前缀匹配）
	ok2, resolved2 := validateEditPath("/proj/UAPCLAWSWARM.local.md", "/ws", "/proj")
	assert.True(t, ok2)
	assert.Equal(t, "/proj/UAPCLAWSWARM.local.md", resolved2)

	// /proj 深层路径同样经白名单前缀匹配放行（project_dir 全子树可编辑）
	ok3, resolved3 := validateEditPath("/proj/sub/UAPCLAWSWARM.md", "/ws", "/proj")
	assert.True(t, ok3)
	assert.Equal(t, "/proj/sub/UAPCLAWSWARM.md", resolved3)
}

// TestHandleMemoryEdit_恰好20行尾换行 测试 splitlines 语义：恰好 20 行+尾换行无截断标记
// Python: "l\n"*20.splitlines() → 20 行，len==20 不追加 "... (truncated)"
func TestHandleMemoryEdit_恰好20行尾换行(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	p := filepath.Join(tmp, "memory", "a.md")
	assert.NoError(t, os.WriteFile(p, []byte(strings.Repeat("l\n", 20)), 0o644))

	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": p})
	preview, _ := res["content_preview"].(string)
	assert.NotContains(t, preview, "truncated", "恰好 20 行不应有截断标记")
	assert.Equal(t, 20, len(strings.Split(preview, "\n")))
}

// TestHandleMemoryEdit_crlf行尾 测试 \r\n 行界处理（对齐 Python splitlines 不残留 \r）
func TestHandleMemoryEdit_crlf行尾(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	p := filepath.Join(tmp, "memory", "crlf.md")
	assert.NoError(t, os.WriteFile(p, []byte("l1\r\nl2\r\nl3"), 0o644))

	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": p})
	preview, _ := res["content_preview"].(string)
	assert.Equal(t, "l1\nl2\nl3", preview)
}

// TestHandleMemoryEdit_空文件预览 测试空文件 preview 为空串
func TestHandleMemoryEdit_空文件预览(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	p := filepath.Join(tmp, "memory", "empty.md")
	assert.NoError(t, os.WriteFile(p, []byte(""), 0o644))

	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": p})
	preview, _ := res["content_preview"].(string)
	// Python: "".splitlines() → [] → "\n".join([]) → ""
	assert.Equal(t, "", preview)
}

// TestHandleMemoryEdit_只有尾换行 测试仅 "\n" 的文件 preview 为空串
func TestHandleMemoryEdit_只有尾换行(t *testing.T) {
	setupIsolatedHome(t)
	tmp := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(tmp, "memory"), 0o755))
	p := filepath.Join(tmp, "memory", "nl.md")
	assert.NoError(t, os.WriteFile(p, []byte("\n"), 0o644))

	res := HandleMemoryEdit(context.Background(), tmp, map[string]any{"path": p})
	preview, _ := res["content_preview"].(string)
	// Python: "\n".splitlines() → [""] → join → ""
	assert.Equal(t, "", preview)
}
