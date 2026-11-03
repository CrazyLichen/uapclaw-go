package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveProjectDir_三级回退 对齐 Python _resolve_project_dir。
func TestResolveProjectDir_三级回退(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"project_dir 优先", map[string]any{"project_dir": "/a", "trusted_dirs": []any{"/b"}, "cwd": "/c"}, "/a"},
		{"trusted_dirs 次之", map[string]any{"trusted_dirs": []any{"/b"}, "cwd": "/c"}, "/b"},
		{"cwd 兜底", map[string]any{"cwd": "/c"}, "/c"},
		{"全缺省返回空", map[string]any{}, ""},
		{"空字符串视为缺省", map[string]any{"project_dir": "", "cwd": ""}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveProjectDir(tc.params))
		})
	}
}

// TestMemoryMode_缺省为plan 对齐 Python params.get("mode", "plan")。
func TestMemoryMode_缺省为plan(t *testing.T) {
	require.Equal(t, "plan", memoryMode(map[string]any{}))
	require.Equal(t, "plan", memoryMode(map[string]any{"mode": ""}))
	require.Equal(t, "code.normal", memoryMode(map[string]any{"mode": "code.normal"}))
}

// TestWithProjectDir_不修改入参且命中时注入副本 对齐 Python {**params, ...}。
func TestWithProjectDir_不修改入参且命中时注入副本(t *testing.T) {
	params := map[string]any{"cwd": "/c"}

	merged := withProjectDir(params, "/p")
	require.Equal(t, "/p", merged["project_dir"])
	require.Equal(t, "/c", merged["cwd"])
	_, hasKey := params["project_dir"]
	require.False(t, hasKey, "不得就地修改调用方 map")

	unchanged := withProjectDir(params, "")
	require.Equal(t, "/c", unchanged["cwd"])
	_, hasKey = unchanged["project_dir"]
	require.False(t, hasKey)
}
