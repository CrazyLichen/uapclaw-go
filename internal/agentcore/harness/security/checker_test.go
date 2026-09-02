package security

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewExternalDirectoryChecker 测试创建外部目录检查器
func TestNewExternalDirectoryChecker(t *testing.T) {
	config := map[string]any{"external_directory": map[string]any{"*": "deny"}}
	workspace := "/home/user/project"

	checker := NewExternalDirectoryChecker(config, workspace)

	require.NotNil(t, checker)
	assert.Equal(t, config, checker.config)
	assert.Equal(t, workspace, checker.workspaceRoot)
}

// TestCheckExternalPaths_工作空间内路径 测试访问工作空间内路径返回 nil
func TestCheckExternalPaths_工作空间内路径(t *testing.T) {
	workspace := t.TempDir()
	checker := NewExternalDirectoryChecker(nil, workspace)

	// read_file 是 pathApprovalTools 中的工具，访问工作空间内路径
	result := checker.CheckExternalPaths("read_file", map[string]any{
		"path": filepath.Join(workspace, "src/main.go"),
	})

	assert.Nil(t, result)
}

// TestCheckExternalPaths_工作空间外路径_默认ASK 测试访问工作空间外路径且默认配置返回 ASK
func TestCheckExternalPaths_工作空间外路径_默认ASK(t *testing.T) {
	workspace := t.TempDir()
	// 未配置 external_directory，默认走 ask
	checker := NewExternalDirectoryChecker(map[string]any{}, workspace)

	result := checker.CheckExternalPaths("read_file", map[string]any{
		"path": "/etc/hosts",
	})

	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAsk, result.Permission)
	assert.Contains(t, result.Reason, "requires approval")
	assert.Equal(t, "external_directory.*", result.MatchedRule)
	assert.Contains(t, result.ExternalPaths[0], "etc/hosts")
}

// TestCheckExternalPaths_工作空间外路径_DENY 测试配置为 deny 时返回 DENY
func TestCheckExternalPaths_工作空间外路径_DENY(t *testing.T) {
	workspace := t.TempDir()
	checker := NewExternalDirectoryChecker(map[string]any{
		"external_directory": map[string]any{"*": "deny"},
	}, workspace)

	result := checker.CheckExternalPaths("read_file", map[string]any{
		"path": "/etc/hosts",
	})

	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelDeny, result.Permission)
	assert.Contains(t, result.Reason, "is denied")
}

// TestCheckExternalPaths_无工作空间 测试无工作空间时跳过检查返回 nil
func TestCheckExternalPaths_无工作空间(t *testing.T) {
	checker := NewExternalDirectoryChecker(nil, "")

	result := checker.CheckExternalPaths("read_file", map[string]any{
		"path": "/etc/hosts",
	})

	assert.Nil(t, result)
}

// TestCheckExternalPaths_shell工具外部路径 测试 shell 工具访问外部路径
func TestCheckExternalPaths_shell工具外部路径(t *testing.T) {
	workspace := t.TempDir()
	checker := NewExternalDirectoryChecker(map[string]any{}, workspace)

	// bash 是 shellApprovalTools 中的工具
	result := checker.CheckExternalPaths("bash", map[string]any{
		"command": "cat /etc/hosts",
		"workdir": "",
	})

	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAsk, result.Permission)
	// 提取到的外部路径应包含 /etc/hosts
	assert.NotEmpty(t, result.ExternalPaths)
}

// TestCheckExternalPaths_externalDirectoryAllow配置 测试 external_directory allow 配置放行外部路径
func TestCheckExternalPaths_externalDirectoryAllow配置(t *testing.T) {
	workspace := t.TempDir()
	// 配置 /etc 目录为 allow
	checker := NewExternalDirectoryChecker(map[string]any{
		"external_directory": map[string]any{
			"*":          "ask",
			"/etc/hosts": "allow",
		},
	}, workspace)

	result := checker.CheckExternalPaths("read_file", map[string]any{
		"path": "/etc/hosts",
	})

	// /etc/hosts 匹配到 allow 规则，应返回 nil
	assert.Nil(t, result)
}

// TestCheckExternalPaths_非审批工具 测试非审批工具返回 nil
func TestCheckExternalPaths_非审批工具(t *testing.T) {
	workspace := t.TempDir()
	checker := NewExternalDirectoryChecker(nil, workspace)

	result := checker.CheckExternalPaths("unknown_tool", map[string]any{
		"path": "/etc/hosts",
	})

	assert.Nil(t, result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestExtractPathsFromCommand_简单命令 测试从 path-aware 命令提取路径
func TestExtractPathsFromCommand_简单命令(t *testing.T) {
	workdir := "/home/user/project"

	paths := extractPathsFromCommand("cat /etc/hosts", workdir)

	require.Len(t, paths, 1)
	assert.Equal(t, "/etc/hosts", filepath.ToSlash(paths[0]))
}

// TestExtractPathsFromCommand_非路径感知命令 测试非 path-aware 命令不提取路径
func TestExtractPathsFromCommand_非路径感知命令(t *testing.T) {
	workdir := "/home/user/project"

	paths := extractPathsFromCommand("echo hello", workdir)

	assert.Nil(t, paths)
}

// TestExtractPathsFromCommand_相对路径 测试相对路径解析为绝对路径
func TestExtractPathsFromCommand_相对路径(t *testing.T) {
	workdir := "/home/user/project"

	paths := extractPathsFromCommand("cat ./foo.txt", workdir)

	require.Len(t, paths, 1)
	assert.Contains(t, filepath.ToSlash(paths[0]), "foo.txt")
}

// TestExtractPathsFromCommand_空命令 测试空命令返回 nil
func TestExtractPathsFromCommand_空命令(t *testing.T) {
	paths := extractPathsFromCommand("", "/home/user")

	assert.Nil(t, paths)
}

// TestExtractPathsFromCommand_跳过标志参数 测试跳过以 - 开头的标志参数
func TestExtractPathsFromCommand_跳过标志参数(t *testing.T) {
	workdir := "/home/user/project"

	paths := extractPathsFromCommand("ls -la /home/user/project/src", workdir)

	require.Len(t, paths, 1)
	assert.Contains(t, filepath.ToSlash(paths[0]), "src")
}

// TestLooksLikePath 测试路径启发式检测
func TestLooksLikePath(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		// 以 ./ 开头
		{"./foo", true},
		// 以 ../ 开头
		{"../bar", true},
		// 以 \\ 开头（UNC 路径）
		{"\\server\\share", true},
		// 绝对路径
		{"/home/user", true},
		// Windows 盘符
		{"C:\\Windows", true},
		{"D:/data", true},
		// 包含反斜杠
		{"foo\\bar", true},
		// 以 -- 开头的标志 → 不应识别为路径
		{"--flag", false},
		// 纯文本无分隔符 → 不是路径
		{"hello", false},
		// 纯标志
		{"-v", false},
	}

	for _, tt := range tests {
		got := looksLikePath(tt.token)
		assert.Equal(t, tt.want, got, "looksLikePath(%q)", tt.token)
	}
}

// TestToolArgValueLooksLikePath 测试工具参数值路径判断
func TestToolArgValueLooksLikePath(t *testing.T) {
	tests := []struct {
		argKey string
		value  string
		want   bool
	}{
		// path 键在 pathArgKeys 中 → true
		{"path", "/etc/hosts", true},
		// file_path 键在 pathArgKeys 中 → true
		{"file_path", "data.txt", true},
		// directory 键在 pathArgKeys 中 → true
		{"directory", "docs", true},
		// command 键 + 无斜杠值 → 不在 pathArgKeys 且无分隔符 → false
		{"command", "echo hello", false},
		// 含斜杠的值 → true（即使键不在 pathArgKeys 中）
		{"command", "cat /etc/hosts", true},
		// 非路径键 + 无路径特征 → false
		{"mode", "verbose", false},
		// Windows 盘符（第二字符为冒号）→ true
		{"target", "C:temp", true},
	}

	for _, tt := range tests {
		got := toolArgValueLooksLikePath(tt.argKey, tt.value)
		assert.Equal(t, tt.want, got, "toolArgValueLooksLikePath(%q, %q)", tt.argKey, tt.value)
	}
}
