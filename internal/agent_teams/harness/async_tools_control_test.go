package harness

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)

// TestRuntime_HasRunning与CancelAll 验证在飞统计与停机取消。
func TestRuntime_HasRunning与CancelAll(t *testing.T) {
	var injected []string
	rt := newTestRuntime(t, &injected)
	started := make(chan struct{})
	rt.Launch("t3", func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"})
	<-started
	assert.True(t, rt.HasRunning("demo"))
	assert.False(t, rt.HasRunning("other"))
	rt.CancelAll()
	waitStatus(t, rt, "t3", AsyncToolStatusError)
	assert.Equal(t, "cancelled", rt.Get("t3").Error)
	assert.Empty(t, injected)
}

// TestRuntime_Cancel与Wait 验证按 id 取消与等待。
func TestRuntime_Cancel与Wait(t *testing.T) {
	var injected []string
	rt := newTestRuntime(t, &injected)
	gate := make(chan struct{})
	rt.Launch("w", func(ctx context.Context) (any, error) {
		select {
		case <-gate:
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"})

	rec := rt.Wait(context.Background(), "w", 10*time.Millisecond)
	require.NotNil(t, rec)
	assert.Equal(t, AsyncToolStatusRunning, rec.Status)
	assert.Nil(t, rt.Wait(context.Background(), "missing", time.Millisecond))

	close(gate)
	rec = rt.Wait(context.Background(), "w", time.Second)
	assert.Equal(t, AsyncToolStatusCompleted, rec.Status)
	assert.Equal(t, "done", rec.Result)

	assert.True(t, rt.Cancel("w"), "已完成任务按 docstring 返回 true")
	assert.False(t, rt.Cancel("missing"))
}

// TestRuntime_ListAll保序 验证 launch 顺序与未知 id。
func TestRuntime_ListAll保序(t *testing.T) {
	var injected []string
	rt := newTestRuntime(t, &injected)
	rt.Launch("a", func(context.Context) (any, error) { return "a", nil }, tools.AsyncToolLaunchOptions{ToolName: "demo"})
	rt.Launch("b", func(context.Context) (any, error) { return "b", nil }, tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "a", AsyncToolStatusCompleted)
	waitStatus(t, rt, "b", AsyncToolStatusCompleted)
	all := rt.ListAll()
	require.Len(t, all, 2)
	assert.Equal(t, "a", all[0].TaskID)
	assert.Equal(t, "b", all[1].TaskID)
	assert.Nil(t, rt.Get("missing"))
}
