package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// 编译期断言：真实协调器满足 SessionCoordinatorPort。
// 端口扩展（P4 收归账本调用）后必须仍然成立，否则 Runtime 无法持有真实协调器。
var _ SessionCoordinatorPort = (*sessionpkg.RuntimeSessionCoordinator)(nil)

// TestSessionCoordinatorPort_真实协调器可用 真实协调器可从 Runtime 访问器取用。
func TestSessionCoordinatorPort_真实协调器可用(t *testing.T) {
	coordinator := sessionpkg.NewRuntimeSessionCoordinator(sessionpkg.DefaultCoordinatorOptions())
	runtime := NewAgentRuntime(AgentRuntimeDeps{Coordinator: coordinator})

	require.Same(t, SessionCoordinatorPort(coordinator), runtime.Coordinator())

	// 未经登记的会话：快照为 nil，ownsSession 为假
	require.Nil(t, coordinator.SnapshotSession("never-registered"))
	require.False(t, runtime.ownsSession("never-registered"))
}

// TestActiveExecutions_真实协调器空账本 无执行时为假。
// Python: service.py:845 snapshot_session(...).executions 判定
func TestActiveExecutions_真实协调器空账本(t *testing.T) {
	coordinator := sessionpkg.NewRuntimeSessionCoordinator(sessionpkg.DefaultCoordinatorOptions())
	runtime := NewAgentRuntime(AgentRuntimeDeps{Coordinator: coordinator})
	require.False(t, runtime.ActiveExecutions())
}
