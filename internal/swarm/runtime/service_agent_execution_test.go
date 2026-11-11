package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// defRecordingManager 记录 GetChatAgent 收到的 agentDefinition 与 fingerprint。
type defRecordingManager struct {
	gotDefinition  map[string]any
	gotFingerprint string
	agent          ChatAgentPort
}

func (m *defRecordingManager) GetChatAgent(
	ctx context.Context, channelID, mode, subMode, projectDir string,
	agentDefinition map[string]any, fingerprint string,
) (ChatAgentPort, error) {
	m.gotDefinition = agentDefinition
	m.gotFingerprint = fingerprint
	return m.agent, nil
}

func (m *defRecordingManager) GetChatAgentNoWait(channelID, mode, projectDir, subMode string) ChatAgentPort {
	return m.agent
}

// TestStream_把definition传给manager 验证 _agent_execution 不再被丢弃。
// Python: service.py:1688 -> :2561 prepare_kwargs["agent_execution"]
//
//	-> :1279-1283 agent_definition + agent_definition_fingerprint
func TestStream_把definition传给manager(t *testing.T) {
	mgr := &defRecordingManager{agent: &startTestChatAgent{}}
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: mgr})
	require.NoError(t, rt.Start())

	def, err := RuntimeAgentDefinitionFromMapping(map[string]any{
		"name":         "my-agent",
		"instructions": "be helpful",
		"tools":        []any{"read_file"},
	})
	require.NoError(t, err)
	exec := &RuntimeAgentExecution{Definition: def, Mode: "agent.code.normal"}

	err = rt.Stream(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		func(*RuntimeEvent) bool { return false },
		WithAgentExecution(exec))
	require.NoError(t, err)

	require.NotNil(t, mgr.gotDefinition, "_agent_execution.Definition 必须传到 manager")
	require.Equal(t, "my-agent", mgr.gotDefinition["name"],
		"definition 必须可被 adapter 读到 name（Python interface_deep.py:13096）")
	require.NotEmpty(t, mgr.gotFingerprint, "fingerprint 必须一并传递")
}

// TestInvoke_把definition传给manager 验证非流式路径同样。
func TestInvoke_把definition传给manager(t *testing.T) {
	mgr := &defRecordingManager{agent: &startTestChatAgent{}}
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: mgr})
	require.NoError(t, rt.Start())

	def, err := RuntimeAgentDefinitionFromMapping(map[string]any{
		"name":         "inv-agent",
		"instructions": "do it",
	})
	require.NoError(t, err)
	exec := &RuntimeAgentExecution{Definition: def, Mode: "agent.code.normal"}

	_, err = rt.Invoke(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		WithAgentExecution(exec))
	require.NoError(t, err)

	require.NotNil(t, mgr.gotDefinition)
	require.Equal(t, "inv-agent", mgr.gotDefinition["name"])
}

// TestStream_无execution时传nil 验证普通聊天路径不受影响。
func TestStream_无execution时传nil(t *testing.T) {
	mgr := &defRecordingManager{agent: &startTestChatAgent{}}
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: mgr})
	require.NoError(t, rt.Start())

	require.NoError(t, rt.Stream(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		func(*RuntimeEvent) bool { return false }))

	require.Nil(t, mgr.gotDefinition, "普通聊天不传 definition")
	require.Empty(t, mgr.gotFingerprint)
}
