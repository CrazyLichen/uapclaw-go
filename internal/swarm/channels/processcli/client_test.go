package processcli

import (
	"testing"

	"github.com/stretchr/testify/require"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestRequireProcessCliChannel_非processCli报错
// Python: client.py:21-25 _require_process_cli_channel
func TestRequireProcessCliChannel_非processCli报错(t *testing.T) {
	err := requireProcessCliChannel("web")
	require.Error(t, err)
	require.Contains(t, err.Error(), "channel_id='process_cli'")

	require.NoError(t, requireProcessCliChannel("process_cli"))
}

// TestProcessCliBoundaryError_错误码
// Python: client.py:17-18 code / retryable
func TestProcessCliBoundaryError_错误码(t *testing.T) {
	err := &ProcessCliBoundaryError{Message: "x"}
	require.Equal(t, "PROCESS_CLI_CHANNEL_INVALID", err.Code())
	require.False(t, err.Retryable())
	require.Equal(t, "x", err.Error())
}

// TestNewInProcessRuntimeClient_复用传入runtime
// Python: client.py:76-80 __init__
func TestNewInProcessRuntimeClient_复用传入runtime(t *testing.T) {
	rt := swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{})
	c := NewInProcessRuntimeClient(rt)
	require.Same(t, rt, c.Runtime())
}

// TestNewInProcessRuntimeClient_nil时新建
func TestNewInProcessRuntimeClient_nil时新建(t *testing.T) {
	c := NewInProcessRuntimeClient(nil)
	require.NotNil(t, c.Runtime())
}

// TestGetSession_渠道守卫
// Python: client.py:115-117 get_session 走 _require_process_cli_channel
func TestGetSession_渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	_, err := c.GetSession(swruntime.SessionGetInput{ChannelID: "web", SessionID: "s1"})
	require.Error(t, err)
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestListSessions_渠道守卫
// Python: client.py:119-121 list_sessions
func TestListSessions_渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	_, err := c.ListSessions(swruntime.SessionListInput{ChannelID: "web"})
	require.Error(t, err)
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestCreateOrResumeSession_渠道守卫
// Python: client.py:139-149 create_or_resume_session
func TestCreateOrResumeSession_渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	_, err := c.CreateOrResumeSession("web", nil)
	require.Error(t, err)
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestStream_渠道守卫
// Python: client.py:226-230 stream
func TestStream_渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	err := c.Stream(t.Context(), testRequest("web"), func(*swruntime.RuntimeEvent) bool { return false }, nil)
	require.Error(t, err)
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestStreamAgent_渠道守卫
// Python: client.py:232-242 stream_agent
func TestStreamAgent_渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	err := c.StreamAgent(t.Context(), testRequest("web"), nil,
		func(*swruntime.RuntimeEvent) bool { return false }, nil)
	require.Error(t, err)
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestInvoke_渠道守卫
// Python: client.py:244-247 invoke
func TestInvoke_渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	_, err := c.Invoke(t.Context(), testRequest("web"))
	require.Error(t, err)
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestCancel_渠道守卫
// Python: client.py:279-281 cancel
func TestCancel_渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	err := c.Cancel(t.Context(), testRequest("web"))
	require.Error(t, err)
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestCleanupSession_渠道守卫
// Python: client.py:283-288 cleanup_session
func TestCleanupSession_渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	_, err := c.CleanupSession("web", "s1")
	require.Error(t, err)
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestGetPermissionSnapshot_全空时不做渠道守卫
// Python: client.py:127-131 if session_id or channel_id: _require_process_cli_channel
func TestGetPermissionSnapshot_全空时不做渠道守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	// channelID 与 sessionID 皆空 → 跳过守卫，直达 Runtime（Runtime 内部另报未启动）
	_, err := c.GetPermissionSnapshot(swruntime.PermissionSnapshotInput{})
	var boundary *ProcessCliBoundaryError
	require.NotErrorAs(t, err, &boundary, "全空时不得触发渠道守卫")
}

// TestGetPermissionSnapshot_有sessionId时守卫
// Python: client.py:129-130
func TestGetPermissionSnapshot_有sessionId时守卫(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	sid := "s1"
	_, err := c.GetPermissionSnapshot(swruntime.PermissionSnapshotInput{
		ChannelID: "web", SessionID: &sid,
	})
	var boundary *ProcessCliBoundaryError
	require.ErrorAs(t, err, &boundary)
}

// TestValidateAgentDefinition_转调PrepareAgentExecution
// Python: client.py:89-95 validate_agent_definition
func TestValidateAgentDefinition_转调PrepareAgentExecution(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	got, err := c.ValidateAgentDefinition(map[string]any{
		"name":         "agent1",
		"instructions": "do it",
	}, "agent.code.normal")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "agent1", got.Definition.Name)

	// 非法定义应报错
	_, err = c.ValidateAgentDefinition(map[string]any{
		"name":         "ab",
		"instructions": "do it",
	}, "agent.code.normal")
	require.Error(t, err)
}

// TestValidateMcpReferences_本批恒有效
// ⤵️ 回填 20.7：Runtime 侧 validate_mcp_references 尚未实现
func TestValidateMcpReferences_本批恒有效(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	got, err := c.ValidateMcpReferences([]string{"server-a"})
	require.NoError(t, err)
	require.True(t, got.Valid, "本批返回全部有效，使 MCP_NOT_READY 分支不可达")
}

// TestDescribeSession_渠道不符返回nil
// Python: client.py:160-165 捕获 ProcessCliBoundaryError 后返回 None
func TestDescribeSession_渠道不符返回nil(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	// 未启动的 Runtime 会先报未启动，不触发渠道判定；此处只验证不 panic
	_, _ = c.DescribeSession("s1")
}

// testRequest 构造一个最小请求。
func testRequest(channelID string) *swschema.AgentRequest {
	return &swschema.AgentRequest{RequestID: "r1", ChannelID: channelID}
}
