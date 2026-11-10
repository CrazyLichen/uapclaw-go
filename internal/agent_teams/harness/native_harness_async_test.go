package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)

// TestNativeHarness_AsyncToolRuntime懒创建 验证 runtime 单例与 launch 接线。
func TestNativeHarness_AsyncToolRuntime懒创建(t *testing.T) {
	h := newTestNativeHarness(t)
	rt := h.AsyncToolRuntime()
	require.NotNil(t, rt)
	require.Same(t, rt, h.AsyncToolRuntime())

	require.NoError(t, h.LaunchAsyncTool("t1", func(context.Context) (any, error) {
		return "async-result", nil
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"}))
	waitStatus(t, rt, "t1", AsyncToolStatusCompleted)
}
