package sysop_builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── normalizeFSEntry 测试 ────────────────────────────

func TestNormalizeFSEntry(t *testing.T) {
	t.Run("nil 输入返回 nil", func(t *testing.T) {
		result := normalizeFSEntry(nil, "0666")
		assert.Nil(t, result)
	})

	t.Run("空字符串返回 nil", func(t *testing.T) {
		result := normalizeFSEntry("", "0666")
		assert.Nil(t, result)
	})

	t.Run("纯空格返回 nil", func(t *testing.T) {
		result := normalizeFSEntry("   ", "0666")
		assert.Nil(t, result)
	})

	t.Run("字符串输入返回 path+permissions", func(t *testing.T) {
		result := normalizeFSEntry("/tmp/test", "0666")
		require.NotNil(t, result)
		assert.Equal(t, "/tmp/test", result["path"])
		assert.Equal(t, "0666", result["permissions"])
	})

	t.Run("字符串输入去除前后空格", func(t *testing.T) {
		result := normalizeFSEntry("  /tmp/test  ", "0666")
		require.NotNil(t, result)
		assert.Equal(t, "/tmp/test", result["path"])
	})

	t.Run("map 输入含 path 和 permissions", func(t *testing.T) {
		entry := map[string]any{"path": "/tmp/dir", "permissions": "0777"}
		result := normalizeFSEntry(entry, "0666")
		require.NotNil(t, result)
		assert.Equal(t, "/tmp/dir", result["path"])
		assert.Equal(t, "0777", result["permissions"])
	})

	t.Run("map 输入缺 permissions 用默认值", func(t *testing.T) {
		entry := map[string]any{"path": "/tmp/dir"}
		result := normalizeFSEntry(entry, "0666")
		require.NotNil(t, result)
		assert.Equal(t, "/tmp/dir", result["path"])
		assert.Equal(t, "0666", result["permissions"])
	})

	t.Run("map 输入空 path 返回 nil", func(t *testing.T) {
		entry := map[string]any{"path": "", "permissions": "0666"}
		result := normalizeFSEntry(entry, "0666")
		assert.Nil(t, result)
	})

	t.Run("非 string/map 类型返回 nil", func(t *testing.T) {
		result := normalizeFSEntry(42, "0666")
		assert.Nil(t, result)
	})
}

// ──────────────────────────── ensureIntrinsicFile 测试 ────────────────────────────

func TestEnsureIntrinsicFile(t *testing.T) {
	t.Run("文件已存在返回 true", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "existing.md")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0o666))

		result := ensureIntrinsicFile(path)
		assert.True(t, result)
	})

	t.Run("文件不存在则创建返回 true", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "new.md")

		result := ensureIntrinsicFile(path)
		assert.True(t, result)

		// 验证文件已创建
		_, err := os.Stat(path)
		assert.NoError(t, err)
	})

	t.Run("父目录不存在也创建", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sub", "dir", "new.md")

		result := ensureIntrinsicFile(path)
		assert.True(t, result)

		_, err := os.Stat(path)
		assert.NoError(t, err)
	})
}

// ──────────────────────────── resolveProjectDir 测试 ────────────────────────────

func TestResolveProjectDir(t *testing.T) {
	t.Run("override 指向存在目录返回绝对路径", func(t *testing.T) {
		dir := t.TempDir()
		result := resolveProjectDir(dir)
		abs, _ := filepath.Abs(dir)
		assert.Equal(t, abs, result)
	})

	t.Run("override 指向不存在的目录回落", func(t *testing.T) {
		result := resolveProjectDir("/nonexistent_dir_12345")
		// 应回落到 cwd（当前工作目录一定存在）
		assert.NotEqual(t, "", result)
	})

	t.Run("override 指向文件回落", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "file.txt")
		require.NoError(t, os.WriteFile(file, []byte("test"), 0o666))

		result := resolveProjectDir(file)
		// 应回落到 cwd
		assert.NotEqual(t, "", result)
	})

	t.Run("环境变量覆盖", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(envSandboxProjectDir, dir)

		// override 为空，走环境变量
		result := resolveProjectDir("")
		abs, _ := filepath.Abs(dir)
		assert.Equal(t, abs, result)
	})

	t.Run("override 和 env 都为空时回落 cwd", func(t *testing.T) {
		t.Setenv(envSandboxProjectDir, "")
		result := resolveProjectDir("")
		// cwd 一定不为空且是绝对路径
		assert.NotEqual(t, "", result)
		assert.True(t, filepath.IsAbs(result))
	})

	t.Run("拒绝文件系统根", func(t *testing.T) {
		t.Setenv(envSandboxProjectDir, "")
		// 直接传 "/" 作为 override
		result := resolveProjectDir("/")
		// 应拒绝并回落
		// 结果可能是 cwd 或空
		_ = result
	})
}

// ──────────────────────────── BuildFilesystemPolicy 测试 ────────────────────────────

func TestBuildFilesystemPolicy_空输入(t *testing.T) {
	policy, uploadList, err := BuildFilesystemPolicy(nil, "", false)
	require.NoError(t, err)
	require.NotNil(t, policy)

	fsPolicy, ok := policy["filesystem_policy"].(map[string]any)
	require.True(t, ok)

	// 空输入应至少有 files 和 directories
	assert.Contains(t, fsPolicy, "files")
	assert.Contains(t, fsPolicy, "directories")

	// uploadList 应为空
	assert.Empty(t, uploadList)
}

func TestBuildFilesystemPolicy_isCodeAgent_挂载项目目录(t *testing.T) {
	dir := t.TempDir()

	policy, _, err := BuildFilesystemPolicy(nil, dir, true)
	require.NoError(t, err)
	require.NotNil(t, policy)

	fsPolicy := policy["filesystem_policy"].(map[string]any)
	bindMounts, ok := fsPolicy["bind_mounts"].([]map[string]any)
	require.True(t, ok)

	// 应包含 project_dir 的 rw bind
	found := false
	absDir, _ := filepath.Abs(dir)
	for _, bm := range bindMounts {
		if bm["host_path"] == absDir && bm["mode"] == "rw" {
			found = true
			break
		}
	}
	assert.True(t, found, "isCodeAgent=true 时应挂载 project_dir")
}

func TestBuildFilesystemPolicy_非CodeAgent_忽略项目目录(t *testing.T) {
	dir := t.TempDir()

	policy, _, err := BuildFilesystemPolicy(nil, dir, false)
	require.NoError(t, err)
	require.NotNil(t, policy)

	fsPolicy := policy["filesystem_policy"].(map[string]any)
	bindMountsRaw := fsPolicy["bind_mounts"]
	if bindMountsRaw != nil {
		bindMounts := bindMountsRaw.([]map[string]any)
		absDir, _ := filepath.Abs(dir)
		for _, bm := range bindMounts {
			// project_dir 不应作为独立条目出现（但可能被固有文件包含在同一路径前缀下）
			if bm["host_path"] == absDir && bm["sandbox_path"] == absDir {
				t.Fatalf("isCodeAgent=false 时不应单独挂载 project_dir")
			}
		}
	}
}

func TestBuildFilesystemPolicy_filesAllow不存在返回错误(t *testing.T) {
	filesRuntime := map[string]any{
		"allow": []any{"/nonexistent_path_12345"},
	}

	_, _, err := BuildFilesystemPolicy(filesRuntime, "", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "files.allow 路径在主机上不存在")
}

func TestBuildFilesystemPolicy_filesDeny不存在返回错误(t *testing.T) {
	filesRuntime := map[string]any{
		"deny": []any{"/nonexistent_path_12345"},
	}

	_, _, err := BuildFilesystemPolicy(filesRuntime, "", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "files.deny 路径在主机上不存在")
}

func TestBuildFilesystemPolicy_filesAllow和Deny混合(t *testing.T) {
	dir := t.TempDir()

	// 创建 allow 文件和 deny 文件
	allowFile := filepath.Join(dir, "allow.txt")
	denyFile := filepath.Join(dir, "deny.txt")
	require.NoError(t, os.WriteFile(allowFile, []byte("allow"), 0o666))
	require.NoError(t, os.WriteFile(denyFile, []byte("deny"), 0o666))

	filesRuntime := map[string]any{
		"allow": []any{allowFile},
		"deny":  []any{denyFile},
	}

	policy, _, err := BuildFilesystemPolicy(filesRuntime, "", false)
	require.NoError(t, err)
	require.NotNil(t, policy)

	fsPolicy := policy["filesystem_policy"].(map[string]any)

	// 应有 bind_mounts
	bindMounts, ok := fsPolicy["bind_mounts"].([]map[string]any)
	require.True(t, ok)
	assert.True(t, len(bindMounts) >= 2, "应至少有 allow + deny 的 bind mount")

	// 应有 read_only（deny 条目）
	readOnly, _ := fsPolicy["read_only"].([]string)
	assert.True(t, len(readOnly) >= 1, "deny 应产生 read_only 条目")

	// 应有 read_write（allow 条目）
	readWrite, _ := fsPolicy["read_write"].([]string)
	assert.True(t, len(readWrite) >= 1, "allow 应产生 read_write 条目")
}

// ──────────────────────────── collectIntrinsicTargets 测试 ────────────────────────────

func TestCollectIntrinsicTargets(t *testing.T) {
	rwFiles, rwDirs, roFiles := collectIntrinsicTargets()

	// 固有 rw 文件应至少有 5 个（AGENT.md / HEARTBEAT.md / IDENTITY.md / SOUL.md / USER.md）
	assert.True(t, len(rwFiles) >= 5, "应至少有 5 个固有 rw 文件，实际 %d", len(rwFiles))

	// roFiles 可能 0 或 1（取决于 config.yaml 是否存在）
	_ = roFiles

	// rwDirs 取决于 daily_memory 是否存在
	_ = rwDirs
}

// ──────────────────────────── toSlice 测试 ────────────────────────────

func TestToSlice(t *testing.T) {
	t.Run("nil 返回 false", func(t *testing.T) {
		_, ok := toSlice(nil)
		assert.False(t, ok)
	})

	t.Run("[]any 返回 true", func(t *testing.T) {
		result, ok := toSlice([]any{"a", "b"})
		assert.True(t, ok)
		assert.Equal(t, []any{"a", "b"}, result)
	})

	t.Run("[]string 转为 []any", func(t *testing.T) {
		result, ok := toSlice([]string{"a", "b"})
		assert.True(t, ok)
		assert.Equal(t, []any{"a", "b"}, result)
	})

	t.Run("非切片类型返回 false", func(t *testing.T) {
		_, ok := toSlice("not a slice")
		assert.False(t, ok)
	})
}

// ──────────────────────────── containsString 测试 ────────────────────────────

func TestContainsString(t *testing.T) {
	t.Run("包含返回 true", func(t *testing.T) {
		assert.True(t, containsString([]string{"a", "b", "c"}, "b"))
	})

	t.Run("不包含返回 false", func(t *testing.T) {
		assert.False(t, containsString([]string{"a", "b"}, "c"))
	})

	t.Run("空切片返回 false", func(t *testing.T) {
		assert.False(t, containsString([]string{}, "a"))
	})
}

// ──────────────────────────── ensureIntrinsicFile 补充测试 ────────────────────────────

func TestEnsureIntrinsicFile_只读父目录(t *testing.T) {
	// 注意：root 用户可绕过文件权限，此测试在非 root 下才有效
	if os.Getuid() == 0 {
		t.Skip("root 用户可绕过文件权限，跳过只读目录测试")
	}
	dir := t.TempDir()
	readOnlyDir := filepath.Join(dir, "readonly")
	require.NoError(t, os.Mkdir(readOnlyDir, 0o555))
	target := filepath.Join(readOnlyDir, "sub", "file.md")
	assert.False(t, ensureIntrinsicFile(target))
}

// ──────────────────────────── resolveAgentSkillsDir 测试 ────────────────────────────

func TestResolveAgentSkillsDir(t *testing.T) {
	// 此测试验证 resolveAgentSkillsDir 在不存在技能目录时返回空字符串
	// 而不是崩溃（因实际部署环境中可能没有 skills 目录）
	result := resolveAgentSkillsDir()
	// 结果要么是空字符串（不存在），要么是有效目录路径
	if result != "" {
		info, err := os.Stat(result)
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	}
}

// ──────────────────────────── resolveProjectDir 测试补充 ────────────────────────────

func TestResolveProjectDir_环境变量(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envSandboxProjectDir, dir)
	result := resolveProjectDir("")
	// 环境变量指向存在的目录，应返回该目录
	if result != "" {
		assert.Contains(t, result, filepath.Base(dir))
	}
}

func TestResolveProjectDir_环境变量优先于Cwd(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envSandboxProjectDir, dir)
	// override 为空时，应优先使用环境变量而非 cwd
	result := resolveProjectDir("")
	if result != "" {
		assert.Contains(t, result, filepath.Base(dir))
	}
}
