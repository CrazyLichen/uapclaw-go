package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// startTestChatAgent 最小 ChatAgentPort 实现，供 R-1 测试用。
type startTestChatAgent struct{}

func (s *startTestChatAgent) ProcessMessage(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{}, nil
}

func (s *startTestChatAgent) ProcessMessageStream(_ context.Context, _ *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

func (s *startTestChatAgent) ExecuteMessage(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{}, nil
}

func (s *startTestChatAgent) ExecuteMessageStream(_ context.Context, _ *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

func (s *startTestChatAgent) PlanStateAccessor() PlanStateAccessor { return nil }

// startTestManager 最小 AgentManagerPort 实现。
type startTestManager struct{ agent ChatAgentPort }

func (m *startTestManager) GetChatAgent(context.Context, string, string, string, string, map[string]any, string) (ChatAgentPort, error) {
	return m.agent, nil
}

func (m *startTestManager) GetChatAgentNoWait(string, string, string, string) ChatAgentPort {
	return m.agent
}

// TestStream_自启动并置started标志 验证 Stream 自己调用 Start()。
// Python: service.py:2194 await self.start()
//
// 用 fake manager 让 Stream 能走完，然后检查 requireStarted() 通过，
// 说明 Stream 内部确实调用了 Start()（否则 started 仍为 false）。
func TestStream_自启动并置started标志(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: &startTestManager{agent: &startTestChatAgent{}}})

	require.NoError(t, rt.Stream(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		func(*RuntimeEvent) bool { return false }))

	// started 已置位 → requireStarted 不报错
	require.NoError(t, rt.requireStarted(),
		"Stream 必须自己调用 Start()，否则 started 仍为 false")
}

// TestInvoke_自启动并置started标志 验证 Invoke 同理。
// Python: service.py:1707 await self.start()
func TestInvoke_自启动并置started标志(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: &startTestManager{agent: &startTestChatAgent{}}})

	_, err := rt.Invoke(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"})
	require.NoError(t, err)

	require.NoError(t, rt.requireStarted(),
		"Invoke 必须自己调用 Start()")
}
