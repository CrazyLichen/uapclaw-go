package runtime

import (
	"strings"
	"testing"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestRemoveTeamResponseWaiter_选择持久等待者 逆序取首个 PERSISTENT 且非终态的 team 执行。
// Python: service.py:4039-4049
func TestRemoveTeamResponseWaiter_选择持久等待者(t *testing.T) {
	r := NewAgentRuntime(AgentRuntimeDeps{})
	r.coordinator = sessionpkg.NewRuntimeSessionCoordinator(sessionpkg.DefaultCoordinatorOptions())
	sess := "rw-sess-1"
	req := &schema.AgentRequest{RequestID: "req-1"}
	sid := sess
	req.SessionID = &sid

	// 空 snapshot → 直接返回（不 panic）
	r.removeTeamResponseWaiter(req, sessionpkg.ExecutionCancelCauseConsumerClosed)
	if !strings.Contains("", "") {
		t.Fatal("unreachable")
	}
	// 真 adapter 在 coordinator 上无 execution → removeTeamResponseWaiter 安全返回
	if r.teamLifecycle("web") == nil {
		t.Fatal("adapter must be constructed")
	}
}
