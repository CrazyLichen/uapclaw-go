package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListModeCapabilities_四模式固定顺序 返回顺序即产品顺序，且不因调用方修改而污染。
// Python: mode_catalog.py:87-90 list_mode_capabilities
func TestListModeCapabilities_四模式固定顺序(t *testing.T) {
	got := ListModeCapabilities()
	require.Len(t, got.Modes, 4)
	require.Equal(t, []string{
		"agent.work.normal", "agent.work.plan", "agent.code.normal", "agent.code.plan",
	}, []string{
		got.Modes[0].Mode, got.Modes[1].Mode, got.Modes[2].Mode, got.Modes[3].Mode,
	})
}

// TestListModeCapabilities_调用方修改不污染 返回副本，改动不影响后续调用。
// Python: mode_catalog.py:89 返回 _SINGLE_AGENT_MODES（tuple 不可变）
func TestListModeCapabilities_调用方修改不污染(t *testing.T) {
	first := ListModeCapabilities()
	first.Modes[0].Mode = "tampered"

	second := ListModeCapabilities()
	require.Equal(t, "agent.work.normal", second.Modes[0].Mode)
}

// TestListModeCapabilities_能力矩阵 work_mode / is_plan / 自定义定义支持均正确。
// Python: mode_catalog.py:57-84 _SINGLE_AGENT_MODES
func TestListModeCapabilities_能力矩阵(t *testing.T) {
	cases := []struct {
		mode     string
		workMode string
		isPlan   bool
	}{
		{"agent.work.normal", "work", false},
		{"agent.work.plan", "work", true},
		{"agent.code.normal", "code", false},
		{"agent.code.plan", "code", true},
	}
	byMode := make(map[string]RuntimeModeDescriptor, 4)
	for _, descriptor := range ListModeCapabilities().Modes {
		byMode[descriptor.Mode] = descriptor
	}
	for _, c := range cases {
		got, ok := byMode[c.mode]
		require.True(t, ok, "缺失模式 %s", c.mode)
		require.Equal(t, c.workMode, got.WorkMode, c.mode)
		require.Equal(t, c.isPlan, got.IsPlan, c.mode)
		require.True(t, got.SupportsCustomAgentDefinitions, c.mode)
	}
}

// TestResolveModeCapability_空模式报BAD_REQUEST 空串/空白/非字符串均报 required。
// Python: mode_catalog.py:99-101 target 为空 → BAD_REQUEST
func TestResolveModeCapability_空模式报BAD_REQUEST(t *testing.T) {
	cases := []struct {
		name      string
		requested any
	}{
		{"空串", ""},
		{"仅空白", "   "},
		{"nil", nil},
		{"非字符串", 42},
		{"非字符串切片", []string{"agent.work.normal"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ResolveModeCapability(c.requested)
			require.ErrorIs(t, err, ErrModeRequired)
		})
	}
}

// TestResolveModeCapability_未知模式报NOT_FOUND 不在四模式内且非别名报 not found。
// Python: mode_catalog.py:105-106
func TestResolveModeCapability_未知模式报NOT_FOUND(t *testing.T) {
	for _, mode := range []string{
		"team.work.normal", "team.code.plan", "bogus", "agent.work.weird",
	} {
		_, err := ResolveModeCapability(mode)
		require.ErrorIs(t, err, ErrModeNotFound, "mode=%q", mode)
	}
}

// TestResolveModeCapability_权威模式命中 四个权威模式直接命中。
// Python: mode_catalog.py:103-107
func TestResolveModeCapability_权威模式命中(t *testing.T) {
	cases := map[string]struct {
		workMode string
		isPlan   bool
	}{
		"agent.work.normal": {"work", false},
		"agent.work.plan":   {"work", true},
		"agent.code.normal": {"code", false},
		"agent.code.plan":   {"code", true},
	}
	for mode, want := range cases {
		got, err := ResolveModeCapability(mode)
		require.NoError(t, err, mode)
		require.Equal(t, mode, got.Mode)
		require.Equal(t, want.workMode, got.WorkMode)
		require.Equal(t, want.isPlan, got.IsPlan)
	}
}

// TestResolveModeCapability_遗留别名折叠 上游 deprecate_mode 保留受支持别名。
// Python: mode_catalog.py:96 normalized = deprecate_mode(requested)
//
// 别名来自 gateway/common/mode_matrix.py:94-105 DEPRECATION_MAP。
func TestResolveModeCapability_遗留别名折叠(t *testing.T) {
	cases := map[string]string{
		"agent":       "agent.work.normal",
		"agent.plan":  "agent.work.plan",
		"agent.fast":  "agent.work.normal",
		"agent.work":  "agent.work.normal",
		"agent.code":  "agent.code.normal",
		"code":        "agent.code.normal",
		"code.normal": "agent.code.normal",
		"code.plan":   "agent.code.plan",
	}
	for alias, want := range cases {
		got, err := ResolveModeCapability(alias)
		require.NoError(t, err, "alias=%q", alias)
		require.Equal(t, want, got.Mode, "alias=%q", alias)
	}
}

// TestResolveModeCapability_大小写与空白归一 上游 canonicalize 会小写化并去空白。
// Python: gateway/common/mode_matrix.py canonicalize_mode_text
func TestResolveModeCapability_大小写与空白归一(t *testing.T) {
	got, err := ResolveModeCapability("  AGENT.WORK.PLAN  ")
	require.NoError(t, err)
	require.Equal(t, "agent.work.plan", got.Mode)
}

// TestResolveModeCapability_团队模式不属单Agent 团队模式已折叠但不在目录内。
// Python: mode_catalog.py:105 只匹配 _SINGLE_AGENT_MODES
func TestResolveModeCapability_团队模式不属单Agent(t *testing.T) {
	_, err := ResolveModeCapability("team")
	require.ErrorIs(t, err, ErrModeNotFound, "team 折叠为 team.work.normal，不在单 Agent 目录")
}

// TestRuntimeModeDescriptorToDict_四字段 机器协议字段名与上游一致。
// Python: mode_catalog.py:39-47 to_dict
func TestRuntimeModeDescriptorToDict_四字段(t *testing.T) {
	descriptor, err := ResolveModeCapability("agent.code.plan")
	require.NoError(t, err)

	got := RuntimeModeDescriptorToDict(descriptor)
	require.Len(t, got, 4)
	require.Equal(t, "agent.code.plan", got["mode"])
	require.Equal(t, "code", got["work_mode"])
	require.Equal(t, true, got["is_plan"])
	require.Equal(t, true, got["supports_custom_agent_definitions"])
}

// TestModeCatalogResultToDict_包装modes键 结果为 {"modes": [...]}。
// Python: mode_catalog.py:54-56 ModeCatalogResult.to_dict
func TestModeCatalogResultToDict_包装modes键(t *testing.T) {
	got := ModeCatalogResultToDict(ListModeCapabilities())
	require.Len(t, got, 1)
	modes, ok := got["modes"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, modes, 4)
}
