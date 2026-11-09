//go:build test

package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 用例 ────────────────────────────

// TestCheckPermission_五层管道 覆盖 bypass/deny/allow/只读/接受编辑 五种模式。
func TestCheckPermission_五层管道(t *testing.T) {
	// 第一层：BYPASS 直接放行
	ok, _ := CheckPermission("rm -rf /tmp/x", NewPermissionConfig(PermissionModeBypass, []string{`rm`}, nil), false)
	assert.True(t, ok)

	// 第二层：deny 命中即拒
	ok, reason := CheckPermission("git push && rm -rf build", NewPermissionConfig(PermissionModeAuto, []string{`rm\s+-rf`}, nil), false)
	assert.False(t, ok)
	assert.Contains(t, reason, "命令被规则拒绝")

	// 第三层：allow 命中整条命令即放行
	ok, _ = CheckPermission("npm run build", NewPermissionConfig(PermissionModeReadOnly, nil, []string{`^npm`}), false)
	assert.True(t, ok)

	// 第四层：只读模式拒绝写命令
	ok, reason = CheckPermission("rm -rf build", NewPermissionConfig(PermissionModeReadOnly, nil, nil), false)
	assert.False(t, ok)
	assert.Contains(t, reason, "Read-only mode")

	// 第四层：接受编辑模式拒绝未知命令
	ok, reason = CheckPermission("some-unknown-binary --flag", NewPermissionConfig(PermissionModeAcceptEdits, nil, nil), false)
	assert.False(t, ok)
	assert.Contains(t, reason, "Accept-edits mode")

	// 第五层：AUTO 模式建议性放行
	ok, _ = CheckPermission("echo hi", NewPermissionConfig(PermissionModeAuto, nil, nil), false)
	assert.True(t, ok)
	ok, _ = CheckPermission("echo hi", NewPermissionConfig(PermissionModeAuto, nil, nil), true)
	assert.True(t, ok)
}

// TestCheckPermission_只读命令集 常见只读命令在只读模式下放行。
func TestCheckPermission_只读命令集(t *testing.T) {
	cfg := NewPermissionConfig(PermissionModeReadOnly, nil, nil)
	for _, cmd := range []string{"ls -la", "cat README.md", "grep -rn TODO ."} {
		ok, reason := CheckPermission(cmd, cfg, false)
		assert.True(t, ok, "只读命令应放行: %s (%s)", cmd, reason)
	}
}

// TestCheckPermission_接受编辑安全命令 文件操作与已知安全命令放行。
func TestCheckPermission_接受编辑安全命令(t *testing.T) {
	cfg := NewPermissionConfig(PermissionModeAcceptEdits, nil, nil)
	for _, cmd := range []string{"mkdir -p out", "cp a b", "git status"} {
		ok, reason := CheckPermission(cmd, cfg, false)
		assert.True(t, ok, "安全命令应放行: %s (%s)", cmd, reason)
	}
}

// TestIsReadOnlyCommand_只读与写入 只读命令为真，写入命令为假。
func TestIsReadOnlyCommand_只读与写入(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("ls", false))
	assert.True(t, IsReadOnlyCommand("cat file.txt", false))
	assert.False(t, IsReadOnlyCommand("rm file.txt", false))
	assert.False(t, IsReadOnlyCommand("mkdir dir", false))
}

// TestSplitPipeline_拆分 bash 与 powershell bash 按管道符拆分。
func TestSplitPipeline_拆分(t *testing.T) {
	segments := SplitPipeline("echo a | grep a && echo b", false)
	assert.GreaterOrEqual(t, len(segments), 2)
	assert.NotEmpty(t, segments)

	psSegments := SplitPipeline("Get-ChildItem | Select-String foo", true)
	assert.NotEmpty(t, psSegments)
}

// TestCompilePatterns_非法正则忽略 非法正则不进入结果，合法保留。
func TestCompilePatterns_非法正则忽略(t *testing.T) {
	patterns := CompilePatterns([]string{`^ls`, `[`, `rm\s+-rf`})
	assert.Len(t, patterns, 2)
}

// TestNewPermissionConfig_字段装配 模式与正则列表按入参装配。
func TestNewPermissionConfig_字段装配(t *testing.T) {
	cfg := NewPermissionConfig(PermissionModeReadOnly, []string{`rm`}, []string{`^ls`})
	assert.Equal(t, PermissionModeReadOnly, cfg.Mode)
	assert.Len(t, cfg.DenyPatterns, 1)
	assert.Len(t, cfg.AllowPatterns, 1)
}
