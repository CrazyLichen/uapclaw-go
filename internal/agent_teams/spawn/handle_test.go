package spawn_test

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
)

// TestSpawnHandle_SpawnedProcessHandle满足接口 验证 SpawnedProcessHandle 满足 SpawnHandle 接口。
// 对齐 Python: SpawnedProcessHandle 和 InProcessSpawnHandle 共享相同的方法表面。
func TestSpawnHandle_SpawnedProcessHandle满足接口(t *testing.T) {
	// 编译期断言：SpawnedProcessHandle 满足 SpawnHandle 接口
	var _ spawn.SpawnHandle = (*runnerspawn.SpawnedProcessHandle)(nil)
}
