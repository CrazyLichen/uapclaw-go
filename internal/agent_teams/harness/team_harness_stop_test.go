package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestTeamHarnessStop_提交活跃会话 验证 Stop 会 Commit activeAgentSession 并清引用。
func TestTeamHarnessStop_提交活跃会话(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))

	th := newTeamHarnessWithNative(h, h.DeepAgent(), "leader", "leader_1", false, nil)
	th.setActiveSession(sess)
	th.nativeSessionID = "native-session"

	require.NoError(t, th.Stop(context.Background()))
	assert.Nil(t, th.getActiveSession(), "Stop 后必须清空 activeAgentSession")
	assert.Empty(t, th.nativeSessionID)
}
