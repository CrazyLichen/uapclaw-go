package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestWithOnAgentReady_设置成功 验证 option 可构造。
func TestWithOnAgentReady_设置成功(t *testing.T) {
	opt := WithOnAgentReady(func(_ context.Context, _ harnessinterfaces.DeepAgentInterface) error {
		return nil
	})
	opts := &streamOptions{}
	opt(opts)
	require.NotNil(t, opts.onAgentReady)
}

// TestWithAgentExecution_设置成功 验证 execution 透传 option 可构造。
func TestWithAgentExecution_设置成功(t *testing.T) {
	exec := &RuntimeAgentExecution{}
	opt := WithAgentExecution(exec)
	opts := &streamOptions{}
	opt(opts)
	require.Same(t, exec, opts.agentExecution)
}

// TestApplyStreamOptions_折叠多个option 验证折叠顺序。
func TestApplyStreamOptions_折叠多个option(t *testing.T) {
	first := &RuntimeAgentExecution{Mode: "a"}
	second := &RuntimeAgentExecution{Mode: "b"}
	got := applyStreamOptions([]StreamOption{
		WithAgentExecution(first),
		nil, // nil option 应被跳过
		WithAgentExecution(second),
	})
	require.Same(t, second, got.agentExecution, "后设置者覆盖")
}

// TestStream_接受StreamOption 验证 Stream 签名可用。
func TestStream_接受StreamOption(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Manager: &startTestManager{agent: &startTestChatAgent{}},
	})
	err := rt.Stream(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		func(*RuntimeEvent) bool { return false },
		WithOnAgentReady(func(context.Context, harnessinterfaces.DeepAgentInterface) error {
			return nil
		}))
	require.NoError(t, err)

	// 无 agent 时 onAgentReady 不该被触发，只验证签名可编译
}

// TestInvoke_接受StreamOption 验证 Invoke 签名可用。
func TestInvoke_接受StreamOption(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Manager: &startTestManager{agent: &startTestChatAgent{}},
	})
	_, err := rt.Invoke(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		WithAgentExecution(&RuntimeAgentExecution{Mode: "x"}))
	require.NoError(t, err)
}
