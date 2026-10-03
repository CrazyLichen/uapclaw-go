//go:build integration

package spawn_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnSuite Spawn 集成测试套件
type SpawnSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSpawnSuite 运行 Spawn 集成测试套件
func TestSpawnSuite(t *testing.T) {
	suite.Run(t, new(SpawnSuite))
}

// TestSpawnProcess_真实子进程 测试真实启动子进程
// 运行方式: go test -tags=integration ./tests/integration/external/spawn/...
func (s *SpawnSuite) TestSpawnProcess_真实子进程() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	agentConfig := spawn.SpawnAgentConfig{
		AgentKind: spawn.SpawnAgentKindClassAgent,
		Payload:   map[string]any{},
	}
	inputs := map[string]any{}

	handle, err := spawn.SpawnProcess(ctx, agentConfig, inputs)
	s.Require().NoError(err, "SpawnProcess 失败")
	defer handle.ForceKill()

	s.True(handle.IsAlive(), "子进程应为存活状态")
	s.NotEmpty(handle.ProcessID(), "ProcessID 不应为空")
	s.Greater(handle.PID(), 0, "PID 应 > 0")

	graceful, err := handle.Shutdown(ctx)
	if err != nil {
		s.T().Logf("Shutdown 返回错误: %v", err)
	}
	s.T().Logf("优雅关闭: %v", graceful)
}

// TestSpawnDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *SpawnSuite) TestSpawnDoc_包引用验证() {
	// 验证 spawn 和 runner 包可正常导入
	s.NotNil(spawn.SpawnProcess)
	s.NotNil(runner.GetResourceMgr)
}
