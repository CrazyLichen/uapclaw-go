package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestNativeHarness_RunOnce拒绝已启动 验证 C15：supervisor 起来后不允许 run_once。
func TestNativeHarness_RunOnce拒绝已启动(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	defer func() { _ = h.Stop(context.Background()) }()

	_, err := h.RunOnce(context.Background(), "x", sess)
	assert.Error(t, err)
}

// TestNativeHarness_RunOnce不启动supervisor 验证 C15：结束后相位仍是 idle。
func TestNativeHarness_RunOnce不启动supervisor(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))

	_, _ = h.RunOnce(context.Background(), "单次任务", sess)
	assert.Equal(t, HarnessStateIdle, h.State())
	assert.Nil(t, h.st.Active())
}

// TestNativeHarness_Dispose后不可再用 验证 Dispose 收尾语义。
func TestNativeHarness_Dispose后不可再用(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	require.NoError(t, h.Dispose(context.Background()))
	assert.Equal(t, HarnessStateTerminated, h.State())
}
