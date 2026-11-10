package harness

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)

// TestRuntime_HasRunning与CancelAll 验证在飞统计与停机取消。
func TestRuntime_HasRunning与CancelAll(t *testing.T) {
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
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
	assert.Empty(t, rec.snapshot())
}

// TestRuntime_Cancel与Wait 验证按 id 取消与等待。
func TestRuntime_Cancel与Wait(t *testing.T) {
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
	gate := make(chan struct{})
	rt.Launch("w", func(ctx context.Context) (any, error) {
		select {
		case <-gate:
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"})

	record := rt.Wait(context.Background(), "w", 10*time.Millisecond)
	require.NotNil(t, record)
	assert.Equal(t, AsyncToolStatusRunning, record.Status)
	assert.Nil(t, rt.Wait(context.Background(), "missing", time.Millisecond))

	close(gate)
	record = rt.Wait(context.Background(), "w", time.Second)
	assert.Equal(t, AsyncToolStatusCompleted, record.Status)
	assert.Equal(t, "done", record.Result)

	assert.False(t, rt.Cancel("w"), "已完成任务已从 _tasks 移除，cancel 返回 false")
	assert.False(t, rt.Cancel("missing"))
}

// TestRuntime_Cancel运行中返回true 验证在飞任务可按 id 取消。
func TestRuntime_Cancel运行中返回true(t *testing.T) {
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
	gate := make(chan struct{})
	defer close(gate)
	rt.Launch("c", func(ctx context.Context) (any, error) {
		<-gate
		return "x", nil
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"})
	assert.True(t, rt.Cancel("c"))
	waitStatus(t, rt, "c", AsyncToolStatusError)
	assert.Equal(t, "cancelled", rt.Get("c").Error)
}

// TestRuntime_ListAll保序 验证 launch 顺序与未知 id。
func TestRuntime_ListAll保序(t *testing.T) {
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
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

// TestRuntime_Wait取消立即返回 验证 ctx 取消时返回当前（running）记录。
func TestRuntime_Wait取消立即返回(t *testing.T) {
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
	gate := make(chan struct{})
	defer close(gate)
	rt.Launch("wc", func(context.Context) (any, error) {
		<-gate
		return "x", nil
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	record := rt.Wait(ctx, "wc", time.Second)
	require.NotNil(t, record)
	assert.Equal(t, AsyncToolStatusRunning, record.Status)
}

// TestRuntime_取消后factory忽略取消 验证 ctx 已取消但 factory 返回成功时按取消结算。
func TestRuntime_取消后factory忽略取消(t *testing.T) {
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
	started := make(chan struct{})
	rt.Launch("ignore", func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return "late", nil
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"})
	<-started
	rt.CancelAll()
	waitStatus(t, rt, "ignore", AsyncToolStatusError)
	assert.Equal(t, "cancelled", rt.Get("ignore").Error)
	assert.Empty(t, rec.snapshot())
}

// TestRuntime_inject为nil 验证无注入回调时不 panic。
func TestRuntime_inject为nil(t *testing.T) {
	rt := NewAsyncToolRuntime(nil)
	rt.Launch("n", func(context.Context) (any, error) { return "x", nil },
		tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "n", AsyncToolStatusCompleted)
}

// TestRuntime_inject返回错误不panic 验证注入失败只吞掉，不影响任务终态。
func TestRuntime_inject返回错误不panic(t *testing.T) {
	rt := NewAsyncToolRuntime(func(context.Context, string) error { return errors.New("closed") })
	rt.Launch("e", func(context.Context) (any, error) { return "x", nil },
		tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "e", AsyncToolStatusCompleted)
}
