package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNarrowPermissions_收窄基础权限 对齐 Python
// tests/unit_tests/agent_teams/test_permission_narrowing.py::test_narrow_permissions_import_and_tighten
func TestNarrowPermissions_收窄基础权限(t *testing.T) {
	narrowed, err := NarrowPermissions(
		map[string]any{"tools": map[string]any{"bash": "allow"}, "defaults": map[string]any{"*": "allow"}},
		map[string]string{"bash": "ask"},
	)
	require.NoError(t, err)
	tools := narrowed["tools"].(map[string]any)
	assert.Equal(t, "ask", tools["bash"])
}

// TestNarrowPermissions_兜底与自动修正 覆盖 defaults 兜底链与 strictest 语义。
func TestNarrowPermissions_兜底与自动修正(t *testing.T) {
	base := map[string]any{
		"tools":    map[string]any{"bash": "allow"},
		"defaults": map[string]any{"*": "ask"},
	}

	// 未列出的工具：defaults["*"] 兜底，override allow → strictest(ask, allow) = ask
	narrowed, err := NarrowPermissions(base, map[string]string{"web": "allow"})
	require.NoError(t, err)
	tools := narrowed["tools"].(map[string]any)
	assert.Equal(t, "ask", tools["web"])

	// 兜底缺失时回退 ASK，再与 override 收紧
	narrowed2, err := NarrowPermissions(map[string]any{"tools": map[string]any{}}, map[string]string{"x": "deny"})
	require.NoError(t, err)
	assert.Equal(t, "deny", narrowed2["tools"].(map[string]any)["x"])

	// deny 不允许放宽为 allow（自动修正回 deny）
	narrowed3, err := NarrowPermissions(base, map[string]string{"bash": "deny"})
	require.NoError(t, err)
	assert.Equal(t, "deny", narrowed3["tools"].(map[string]any)["bash"])
}

// TestNarrowPermissions_非法级别返回错误 对齐 Python PermissionLevel(str) 的 ValueError 传播。
func TestNarrowPermissions_非法级别返回错误(t *testing.T) {
	_, err := NarrowPermissions(map[string]any{}, map[string]string{"x": "bogus"})
	require.Error(t, err)
}

// TestNarrowPermissions_不污染入参 对齐 Python copy.deepcopy 语义。
func TestNarrowPermissions_不污染入参(t *testing.T) {
	base := map[string]any{"tools": map[string]any{"bash": "allow"}}
	_, err := NarrowPermissions(base, map[string]string{"bash": "deny"})
	require.NoError(t, err)
	assert.Equal(t, "allow", base["tools"].(map[string]any)["bash"])
}

// TestFormatBasePermissionsForDesc_英文 对齐 Python format_base_permissions_for_desc(lang="en")。
func TestFormatBasePermissionsForDesc_英文(t *testing.T) {
	got := FormatBasePermissionsForDesc(map[string]any{
		"tools":    map[string]any{"bash": "allow", "web": "ask"},
		"defaults": map[string]any{"*": "ask"},
	}, "en")
	assert.Contains(t, got, "Current teammate base permissions:")
	assert.Contains(t, got, "- bash: allow")
	assert.Contains(t, got, "- web: ask")
	assert.Contains(t, got, "- Other tools not listed above: ask (defaults fallback)")
	assert.Contains(t, got, "Narrowing rules: only tightening, never loosening.")
}

// TestFormatBasePermissionsForDesc_中文 对齐 lang="cn" 分支。
func TestFormatBasePermissionsForDesc_中文(t *testing.T) {
	got := FormatBasePermissionsForDesc(map[string]any{
		"tools":    map[string]any{"bash": "allow"},
		"defaults": map[string]any{"*": "deny"},
	}, "cn")
	assert.Contains(t, got, "当前 teammate 的基础权限规则：")
	assert.Contains(t, got, "- 其他未列出的工具: deny (defaults 兜底)")
	assert.Contains(t, got, "收窄规则：只能收紧，不能放宽。")
}

// TestFormatBasePermissionsForDesc_空配置返回空串 对齐 Python tools 与 defaults 均空 → ""。
func TestFormatBasePermissionsForDesc_空配置返回空串(t *testing.T) {
	assert.Empty(t, FormatBasePermissionsForDesc(map[string]any{}, "en"))
	assert.Empty(t, FormatBasePermissionsForDesc(map[string]any{
		"tools": map[string]any{}, "defaults": map[string]any{},
	}, "en"))
}
