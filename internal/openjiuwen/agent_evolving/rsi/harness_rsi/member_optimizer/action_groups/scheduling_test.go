package actiongroups

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildActionWaves_拓扑排序(t *testing.T) {
	actions := []SchedAction{
		{ActionID: "a", TargetPath: "identity.md"},
		{ActionID: "b", DependsOn: []string{"a"}},
		{ActionID: "c", DependsOn: []string{"a", "b"}},
		{ActionID: "d"},
	}
	waves, err := BuildActionWaves(actions)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"a", "d"}, {"b"}, {"c"}}, waves)
}

func TestBuildActionWaves_环检测(t *testing.T) {
	actions := []SchedAction{
		{ActionID: "a", DependsOn: []string{"b"}},
		{ActionID: "b", DependsOn: []string{"a"}},
	}
	_, err := BuildActionWaves(actions)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dependency cycle detected")
}

func TestBuildActionWavesFromData(t *testing.T) {
	actions := []map[string]any{
		{"action_id": "a"},
		{"action_id": "b", "depends_on": []any{"a"}},
	}
	waves, err := BuildActionWavesFromData(actions)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"a"}, {"b"}}, waves)
	// 环检测（raw 消息变体）
	_, err = BuildActionWavesFromData([]map[string]any{
		{"action_id": "a", "depends_on": []any{"a"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dependency cycle detected in raw actions")
}

func TestBuildWavesFromDepsOnly_无视声明波次(t *testing.T) {
	// Python：LLM 声明的 action_waves 不参与——以 depends_on 重算为准
	waves, err := BuildWavesFromDepsOnly([]map[string]any{
		{"action_id": "b", "depends_on": []any{"a"}},
		{"action_id": "a"},
	})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"a"}, {"b"}}, waves)
}

func TestResolveDeclaredPaths_回落(t *testing.T) {
	require.Equal(t, []string{"x.md", "y.md"}, ResolveDeclaredPaths(SchedAction{TargetPath: "t", DeclaredWritePaths: []string{"x.md", "y.md"}}))
	require.Equal(t, []string{"t"}, ResolveDeclaredPaths(SchedAction{TargetPath: "t"}))
	require.Empty(t, ResolveDeclaredPaths(SchedAction{}))
}

func TestBuildRoleSubwaves_写路径互斥分组(t *testing.T) {
	actions := []SchedAction{
		{ActionID: "c", TargetPath: "config.yaml"},
		{ActionID: "a", TargetPath: "identity.md"},
		{ActionID: "b", TargetPath: "identity.md"}, // 与 a 重叠 → 后置子波
	}
	subwaves := BuildRoleSubwaves(actions)
	require.Len(t, subwaves, 2)
	// 子波内按 action_id 排序
	require.Equal(t, []string{"a", "c"}, idsOf(subwaves[0]))
	require.Equal(t, []string{"b"}, idsOf(subwaves[1]))
}

func TestBuildRoleSubwaves_目录前缀重叠(t *testing.T) {
	actions := []SchedAction{
		{ActionID: "a", DeclaredWritePaths: []string{"skills/"}},
		{ActionID: "b", DeclaredWritePaths: []string{"skills/new_skill/SKILL.md"}}, // 目录包含 → 重叠
	}
	subwaves := BuildRoleSubwaves(actions)
	require.Len(t, subwaves, 2)
}

func TestBuildRoleSubwaves_空输入(t *testing.T) {
	require.Empty(t, BuildRoleSubwaves(nil))
}

func idsOf(subwave []SchedAction) []string {
	out := make([]string, 0, len(subwave))
	for _, action := range subwave {
		out = append(out, action.ActionID)
	}
	return out
}
