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
