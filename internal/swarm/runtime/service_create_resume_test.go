package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ownedTestManager 提供 CreateSession 能力的 manager，供 CreateOrResumeSession 测试用。
type ownedTestManager struct {
	startTestManager
}

func (m *ownedTestManager) CreateSession(channelID, sessionID string) (string, error) {
	if sessionID != "" {
		return sessionID, nil
	}
	return "default", nil
}

// TestCreateOrResumeSession_空ID分配默认 验证无 session_id 时建会话。
// Python: service.py:901-903 manager.create_session(channel_id, session_id=None)
func TestCreateOrResumeSession_空ID分配默认(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Manager: &ownedTestManager{startTestManager{agent: &startTestChatAgent{}}},
	})
	require.NoError(t, rt.Start())

	sid, err := rt.CreateOrResumeSession("process_cli", "")
	require.NoError(t, err)
	require.NotEmpty(t, sid)
}

// TestCreateOrResumeSession_显式ID原样返回 验证显式 ID 复用。
// Python: service.py:886-909
func TestCreateOrResumeSession_显式ID原样返回(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Manager: &ownedTestManager{startTestManager{agent: &startTestChatAgent{}}},
	})
	require.NoError(t, rt.Start())

	got, err := rt.CreateOrResumeSession("process_cli", "sess-abc-123")
	require.NoError(t, err)
	require.Equal(t, "sess-abc-123", got)
}

// TestCreateOrResumeSession_非法ID报错 验证 is_valid_session_id 守卫。
// Python: service.py:888-889 raise ValueError("invalid session_id")
func TestCreateOrResumeSession_非法ID报错(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Manager: &ownedTestManager{startTestManager{agent: &startTestChatAgent{}}},
	})
	require.NoError(t, rt.Start())

	_, err := rt.CreateOrResumeSession("process_cli", "../../etc/passwd")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid session_id")
}

// TestCreateOrResumeSession_外部渠道NOT_FOUND 验证渠道归属守卫。
// Python: service.py:891-904 persisted_channel != requested_channel -> NOT_FOUND
func TestCreateOrResumeSession_外部渠道NOT_FOUND(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	session.ClearAllSessionMetadataCache()

	rt := NewAgentRuntime(AgentRuntimeDeps{
		Manager: &ownedTestManager{startTestManager{agent: &startTestChatAgent{}}},
	})
	require.NoError(t, rt.Start())

	// 建一个 web 渠道的持久化会话
	sid := session.MakeSessionID()
	session.InitSessionMetadata(sid, "web", "", "t", "agent.code.normal", "")
	session.FlushMetadataQueue(5.0)

	_, err := rt.CreateOrResumeSession("process_cli", sid)
	require.Error(t, err)
	require.Contains(t, err.Error(), "session not found",
		"外部渠道会话对 process_cli 必须表现为不存在")
}

// TestCreateOrResumeSession_同渠道可续 验证渠道一致时放行。
func TestCreateOrResumeSession_同渠道可续(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	session.ClearAllSessionMetadataCache()

	rt := NewAgentRuntime(AgentRuntimeDeps{
		Manager: &ownedTestManager{startTestManager{agent: &startTestChatAgent{}}},
	})
	require.NoError(t, rt.Start())

	sid := session.MakeSessionID()
	session.InitSessionMetadata(sid, "process_cli", "", "t", "agent.code.normal", "")
	session.FlushMetadataQueue(5.0)

	got, err := rt.CreateOrResumeSession("process_cli", sid)
	require.NoError(t, err)
	require.Equal(t, sid, got)
}
