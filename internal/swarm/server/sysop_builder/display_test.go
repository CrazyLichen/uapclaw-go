package sysop_builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── ListAutoManagedSandboxPaths 测试 ────────────────────────────

func TestListAutoManagedSandboxPaths(t *testing.T) {
	t.Run("基本调用返回 allow_write 和 deny_write", func(t *testing.T) {
		result := ListAutoManagedSandboxPaths("", false)
		assert.Contains(t, result, "allow_write")
		assert.Contains(t, result, "deny_write")
	})

	t.Run("固有 rw 文件出现在 allow_write 中", func(t *testing.T) {
		result := ListAutoManagedSandboxPaths("", false)
		// 应至少有 5 个固有 rw 文件
		assert.True(t, len(result["allow_write"]) >= 5, "应至少有 5 个固有 rw 文件")
	})

	t.Run("isCodeAgent=true 且 projectDir 存在时挂载项目目录", func(t *testing.T) {
		dir := t.TempDir()
		result := ListAutoManagedSandboxPaths(dir, true)
		absDir, _ := filepath.Abs(dir)
		found := false
		for _, entry := range result["allow_write"] {
			if entry["path"] == absDir+"/" && entry["kind"] == "directory" {
				found = true
				break
			}
		}
		assert.True(t, found, "isCodeAgent=true 时应挂载 projectDir")
	})

	t.Run("isCodeAgent=false 时项目目录不出现", func(t *testing.T) {
		dir := t.TempDir()
		result := ListAutoManagedSandboxPaths(dir, false)
		absDir, _ := filepath.Abs(dir)
		for _, entry := range result["allow_write"] {
			if entry["path"] == absDir+"/" {
				t.Fatalf("isCodeAgent=false 时 projectDir 不应出现在 allow_write 中")
			}
		}
	})
}

// ──────────────────────────── ListEffectiveSandboxFiles 测试 ────────────────────────────

func TestListEffectiveSandboxFiles(t *testing.T) {
	t.Run("空 filesRuntime 返回 auto 条目", func(t *testing.T) {
		result := ListEffectiveSandboxFiles(nil, "", false)
		assert.Contains(t, result, "allow_write")
		assert.Contains(t, result, "deny_write")
	})

	t.Run("filesRuntime 含 allow 时合并到 allow_write", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "test.txt")
		require.NoError(t, os.WriteFile(file, []byte("test"), 0o666))

		filesRuntime := map[string]any{
			"allow": []any{file},
		}
		result := ListEffectiveSandboxFiles(filesRuntime, "", false)
		// 应包含用户 allow 条目
		found := false
		for _, entry := range result["allow_write"] {
			if entry["path"] == file {
				found = true
				break
			}
		}
		assert.True(t, found, "用户 allow 条目应出现在 allow_write 中")
	})

	t.Run("filesRuntime 含 deny 时合并到 deny_write", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "secret.txt")
		require.NoError(t, os.WriteFile(file, []byte("secret"), 0o666))

		filesRuntime := map[string]any{
			"deny": []any{file},
		}
		result := ListEffectiveSandboxFiles(filesRuntime, "", false)
		found := false
		for _, entry := range result["deny_write"] {
			if entry["path"] == file {
				found = true
				break
			}
		}
		assert.True(t, found, "用户 deny 条目应出现在 deny_write 中")
	})
}

// ──────────────────────────── FindAutoManagedMatch 测试 ────────────────────────────

func TestFindAutoManagedMatch(t *testing.T) {
	t.Run("不存在的路径返回 false", func(t *testing.T) {
		_, _, found := FindAutoManagedMatch("/nonexistent_12345", "", false)
		assert.False(t, found)
	})

	t.Run("匹配固有 rw 文件", func(t *testing.T) {
		// 取第一个固有 rw 文件路径
		if len(intrinsicRWFilePathFuncs) == 0 {
			t.Skip("无固有 rw 文件路径函数")
		}
		path := intrinsicRWFilePathFuncs[0]()
		if path == "" {
			t.Skip("固有 rw 文件路径为空")
		}
		bucket, _, found := FindAutoManagedMatch(path, "", false)
		assert.True(t, found)
		assert.Equal(t, "allow_write", bucket)
	})
}

// ──────────────────────────── classifyHostKind 测试 ────────────────────────────

func TestClassifyHostKind(t *testing.T) {
	t.Run("文件返回 file", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "test.txt")
		require.NoError(t, os.WriteFile(file, []byte("test"), 0o666))
		assert.Equal(t, "file", classifyHostKind(file))
	})

	t.Run("目录返回 directory", func(t *testing.T) {
		dir := t.TempDir()
		assert.Equal(t, "directory", classifyHostKind(dir))
	})

	t.Run("不存在的路径返回 file", func(t *testing.T) {
		assert.Equal(t, "file", classifyHostKind("/nonexistent_12345"))
	})
}

// ──────────────────────────── appendUnique 测试 ────────────────────────────

func TestAppendUnique(t *testing.T) {
	t.Run("空切片追加", func(t *testing.T) {
		target := make([]map[string]string, 0)
		entry := map[string]string{"path": "/tmp", "permissions": "0666"}
		result := appendUnique(target, entry)
		assert.Len(t, result, 1)
	})

	t.Run("重复 path 不追加", func(t *testing.T) {
		entry := map[string]string{"path": "/tmp", "permissions": "0666"}
		target := []map[string]string{entry}
		result := appendUnique(target, entry)
		assert.Len(t, result, 1)
	})

	t.Run("不同 path 追加", func(t *testing.T) {
		entry1 := map[string]string{"path": "/tmp1", "permissions": "0666"}
		entry2 := map[string]string{"path": "/tmp2", "permissions": "0666"}
		target := []map[string]string{entry1}
		result := appendUnique(target, entry2)
		assert.Len(t, result, 2)
	})
}

// ──────────────────────────── resolveDisplayPath 测试 ────────────────────────────

func TestResolveDisplayPath(t *testing.T) {
	t.Run("空字符串返回空", func(t *testing.T) {
		assert.Equal(t, "", resolveDisplayPath(""))
	})

	t.Run("纯空格返回空", func(t *testing.T) {
		assert.Equal(t, "", resolveDisplayPath("   "))
	})

	t.Run("绝对路径返回自身", func(t *testing.T) {
		result := resolveDisplayPath("/tmp")
		assert.Equal(t, "/tmp", result)
	})

	t.Run("相对路径解析为绝对路径", func(t *testing.T) {
		result := resolveDisplayPath(".")
		assert.True(t, filepath.IsAbs(result))
	})
}
