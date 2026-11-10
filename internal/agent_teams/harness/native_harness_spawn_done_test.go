package harness

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativeHarness_SpawnDone不阻塞 验证覆写异步返回、复位 auto_invoke 标记。
func TestNativeHarness_SpawnDone不阻塞(t *testing.T) {
	h := newTestNativeHarness(t)
	done := make(chan error, 1)
	go func() {
		done <- h.ScheduleAutoInvokeOnSpawnDone(context.Background(), "spawn-done", 0)
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("ScheduleAutoInvokeOnSpawnDone 阻塞了调用方")
	}
	time.Sleep(50 * time.Millisecond)
	assert.False(t, h.agent.IsAutoInvokeScheduled(), "延迟结束后必须复位标记")
}
