package harness

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestNativeHarness_并发Send全部拿到应答 验证 C1/C16。
func TestNativeHarness_并发Send全部拿到应答(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	defer func() { _ = h.Stop(context.Background()) }()

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errs <- h.Send(ctx, "并发", false)
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			assert.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("并发 Send 有调用方永久阻塞")
		}
	}
}

// TestNativeHarness_Stop后Send返回错误 验证 C16：supervisor 退出后调用方不永久阻塞。
func TestNativeHarness_Stop后Send返回错误(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	require.NoError(t, h.Stop(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := h.Send(ctx, "stop 之后", false)
	assert.Error(t, err)
}
