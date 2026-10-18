package memoryrpc

import (
	"path/filepath"
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
