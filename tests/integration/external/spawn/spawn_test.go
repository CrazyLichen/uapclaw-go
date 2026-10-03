//go:build integration

package spawn_test

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSpawnProcess_真实子进程 测试真实启动子进程
// 运行方式: go test -tags=integration ./tests/integration/external/spawn/...
func TestSpawnProcess_真实子进程(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	agentConfig := spawn.SpawnAgentConfig{
		AgentKind: spawn.SpawnAgentKindClassAgent,
		Payload:   map[string]any{},
	}
	inputs := map[string]any{}

	handle, err := spawn.SpawnProcess(ctx, agentConfig, inputs)
	if err != nil {
		t.Fatalf("SpawnProcess 失败: %v", err)
	}
	defer handle.ForceKill()

	if !handle.IsAlive() {
		t.Error("子进程应为存活状态")
	}
	if handle.ProcessID() == "" {
		t.Error("ProcessID 不应为空")
	}
	if handle.PID() <= 0 {
		t.Errorf("PID = %d, 应 > 0", handle.PID())
	}

	graceful, err := handle.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown 返回错误: %v", err)
	}
	t.Logf("优雅关闭: %v", graceful)
}
