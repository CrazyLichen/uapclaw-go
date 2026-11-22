package runtime

import (
	"testing"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// TestTeamLifecycle_懒构造缓存 渠道维度懒构造并缓存（Python _team_lifecycle :4022-4031）。
func TestTeamLifecycle_懒构造缓存(t *testing.T) {
	r := NewAgentRuntime(AgentRuntimeDeps{})
	first := r.teamLifecycle("web")
	if first == nil {
		t.Fatal("teamLifecycle must be lazily constructed")
	}
	second := r.teamLifecycle("web")
	if first != second {
		t.Fatal("same channel must reuse cached adapter")
	}
	other := r.teamLifecycle("tui")
	if other == nil {
		t.Fatal("other channel must be constructed")
	}
	_ = sessionpkg.SessionLifecycleContext{}
}
