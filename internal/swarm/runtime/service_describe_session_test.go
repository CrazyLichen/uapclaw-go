package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// TestDescribeSession_不存在返回nil 验证空目录不报错。
// Python: service.py:944 session_dir is None or not session_dir.is_dir() -> return None
func TestDescribeSession_不存在返回nil(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, rt.Start())

	d, err := rt.DescribeSession("no-such-session-id")
	require.NoError(t, err)
	require.Nil(t, d)
}

// TestDescribeSession_空ID返回nil 验证空串短路。
// Python: service.py:937 if not target: return None
func TestDescribeSession_空ID返回nil(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, rt.Start())

	d, err := rt.DescribeSession("   ")
	require.NoError(t, err)
	require.Nil(t, d)
}

// TestDescribeSession_读出七字段 验证已存在会话的字段映射。
// Python: service.py:949-956 SessionDescriptor 七字段
func TestDescribeSession_读出七字段(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	session.ClearAllSessionMetadataCache()

	rt := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, rt.Start())

	sid := session.MakeSessionID()
	session.InitSessionMetadata(sid, "process_cli", "u1", "t", "agent.code.normal", "")
	session.FlushMetadataQueue(5.0)

	d, err := rt.DescribeSession(sid)
	require.NoError(t, err)
	require.NotNil(t, d)
	require.Equal(t, sid, d.SessionID)
	require.Equal(t, "process_cli", d.ChannelID)
	require.Equal(t, "agent.code.normal", d.Mode)
	require.Equal(t, "u1", d.UserID)
}
