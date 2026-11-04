package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveNewCanonicalModeText 只按原文查新三段表，legacy 串不折叠。
//
// Python: gateway/common/mode_matrix.py resolve_new_canonical_mode + canonicalize_mode_text
func TestResolveNewCanonicalModeText(t *testing.T) {
	cases := []struct {
		name          string
		text          string
		wantMode      string
		wantSub       string
		wantCanonical string
		wantOK        bool
	}{
		{"team code 族", "team.code.normal", "code", "team", "team.code.normal", true},
		{"agent code 族", "agent.code.plan", "code", "plan", "agent.code.plan", true},
		{"plan 态", "agent.work.plan", "agent", "plan", "agent.work.plan", true},
		{"normal 态", "agent.work.normal", "agent", "", "agent.work.normal", true},
		{"team work 族", "team.work.normal", "team", "", "team.work.normal", true},
		{"legacy 串不折叠", "agent", "", "", "", false},
		{"空串", "", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, sub, canonical, ok := ResolveNewCanonicalModeText(tc.text)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantMode, mode)
			require.Equal(t, tc.wantSub, sub)
			require.Equal(t, tc.wantCanonical, canonical)
		})
	}
}

// TestCanonicalizeModeText 只解析别名、不套 DEPRECATION_MAP。
//
// Python: gateway/common/mode_matrix.py canonicalize_mode_text
func TestCanonicalizeModeText(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"空值回落 agent", "", "agent"},
		{"空白回落 agent", "   ", "agent"},
		{"小写去空白", "  AGENT.WORK  ", "agent.work.normal"},
		{"别名解析", "team.code", "code.team"},
		{"legacy 串保留原文", "agent.plan", "agent.plan"},
		{"未知串原样", "unknown.value", "unknown.value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, CanonicalizeModeText(tc.raw))
		})
	}
}

// TestIsWebComposableMode 只有 Web 组合表覆盖的 mode 才参与 mode+work_mode 组合。
//
// Python: gateway/common/mode_matrix.py WEB_COMPOSABLE_MODES
func TestIsWebComposableMode(t *testing.T) {
	require.True(t, IsWebComposableMode("agent"))
	require.True(t, IsWebComposableMode("agent.plan"))
	require.True(t, IsWebComposableMode("team"))
	require.True(t, IsWebComposableMode("agent.work.plan"))
	require.False(t, IsWebComposableMode("agent.code.plan"), "新三段 code 族自带 work profile，不参与组合")
	require.False(t, IsWebComposableMode(""))
}

// TestComposeWebMode 组合 mode + work_mode 为后端三元组。
//
// Python: gateway/common/mode_matrix.py compose_web_mode（_WEB_MODE_TABLE）
func TestComposeWebMode(t *testing.T) {
	mode, sub, canonical, ok := ComposeWebMode("agent", "code")
	require.True(t, ok)
	require.Equal(t, "code", mode)
	require.Equal(t, "normal", sub)
	require.Equal(t, "code.normal", canonical)

	mode, sub, canonical, ok = ComposeWebMode("team", "code")
	require.True(t, ok)
	require.Equal(t, "code", mode)
	require.Equal(t, "team", sub)
	require.Equal(t, "code.team", canonical)

	mode, sub, canonical, ok = ComposeWebMode("agent.work.plan", "work")
	require.True(t, ok)
	require.Equal(t, "agent", mode)
	require.Equal(t, "plan", sub)
	require.Equal(t, "agent.work.plan", canonical)

	_, _, _, ok = ComposeWebMode("agent.code.plan", "code")
	require.False(t, ok, "未在组合表内的组合应返回 false")
}
