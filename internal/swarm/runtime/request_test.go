package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveRequestRuntimeMode 覆盖三段/裸 subMode/旧串回落的归一。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode
func TestResolveRequestRuntimeMode(t *testing.T) {
	cases := []struct {
		name, mode, subMode string
		wantCanonical       string
		wantPlan, wantTeam  bool
	}{
		{"三段 plan", "agent.work.plan", "", "agent.work.plan", true, false},
		{"三段 normal", "agent.work.normal", "", "agent.work.normal", false, false},
		{"code plan", "code.plan", "", "code.plan", true, false},
		{"code team", "code.team", "", "code.team", false, true},
		{"裸 subMode plan", "", "plan", "agent.work.plan", true, false},
		{"裸 subMode normal", "", "normal", "agent.work.normal", false, false},
		{"旧两段串失效", "agent." + "plan", "", "agent.work.normal", false, false},
		{"team 族", "team", "", "team", false, true},
		{"team 带子模式", "team.work.plan", "", "team.work.plan", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveRequestRuntimeMode(c.mode, c.subMode)
			require.Equal(t, c.wantCanonical, got.CanonicalMode)
			require.Equal(t, c.wantPlan, got.IsPlan)
			require.Equal(t, c.wantTeam, got.IsTeam)
		})
	}
}

// TestShouldSync 验证只认 chat 类方法。
func TestShouldSync(t *testing.T) {
	require.True(t, ShouldSync(""))
	require.True(t, ShouldSync("chat.send"))
	require.True(t, ShouldSync("chat.resume"))
	require.True(t, ShouldSync("chat.answer"))
	require.False(t, ShouldSync("session.list"))
	require.False(t, ShouldSync("agent.status"))
}

// TestIsPlanEntrySource 验证显式进入来源集合。
func TestIsPlanEntrySource(t *testing.T) {
	require.True(t, IsPlanEntrySource(PlanEntrySourceSlashCommand))
	require.True(t, IsPlanEntrySource(PlanEntrySourcePlanToggle))
	require.False(t, IsPlanEntrySource(""))
	require.False(t, IsPlanEntrySource("auto"))
}
