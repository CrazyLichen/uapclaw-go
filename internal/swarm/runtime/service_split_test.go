package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// splitTestJSON 把 map 序列化为请求参数。
func splitTestJSON(value map[string]any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

// recordingChatAgent 记录走了哪个入口。
type recordingChatAgent struct {
	processCalls int
	executeCalls int
}

// ProcessMessage 记录并返回固定响应。
func (a *recordingChatAgent) ProcessMessage(
	_ context.Context, _ *schema.AgentRequest,
) (*schema.AgentResponse, error) {
	a.processCalls++
	return schema.NewAgentResponse("r1", "ch-1"), nil
}

// ProcessMessageStream 记录按流式处理。
func (a *recordingChatAgent) ProcessMessageStream(
	_ context.Context, _ *schema.AgentRequest,
) (<-chan *schema.AgentResponseChunk, error) {
	a.processCalls++
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

// ExecuteMessage 记录并返回固定响应。
func (a *recordingChatAgent) ExecuteMessage(
	_ context.Context, _ *schema.AgentRequest,
) (*schema.AgentResponse, error) {
	a.executeCalls++
	return schema.NewAgentResponse("r1", "ch-1"), nil
}

// ExecuteMessageStream 记录按流式处理。
func (a *recordingChatAgent) ExecuteMessageStream(
	_ context.Context, _ *schema.AgentRequest,
) (<-chan *schema.AgentResponseChunk, error) {
	a.executeCalls++
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

// PlanStateAccessor 返回 nil（本用例不涉及 plan）。
func (a *recordingChatAgent) PlanStateAccessor() PlanStateAccessor { return nil }

// newSplitRuntime 构造带固定 agent 的 runtime。
func newSplitRuntime(agent ChatAgentPort) *AgentRuntime {
	return NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(context.Context, *schema.AgentRequest, string, map[string]any, string) (string, string, ChatAgentPort, error) {
			return "agent", "", agent, nil
		},
	})
}

// newChatRequest 构造聊天请求。
func newChatRequest(method schema.ReqMethod, mode string, stream bool) *schema.AgentRequest {
	sessionID := "s1"
	request := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "ch-1",
		SessionID: &sessionID,
		ReqMethod: method,
		IsStream:  stream,
	}
	request.Params = splitTestJSON(map[string]any{"mode": mode, "query": "hi"})
	return request
}

// TestInvoke_聊天请求走ExecuteMessage 单 agent 聊天归 Runtime 调度。
func TestInvoke_聊天请求走ExecuteMessage(t *testing.T) {
	agent := &recordingChatAgent{}
	rt := newSplitRuntime(agent)

	if _, err := rt.Invoke(context.Background(), newChatRequest(schema.ReqMethodChatSend, "agent.work.normal", false)); err != nil {
		t.Fatalf("Invoke 不应报错: %v", err)
	}
	if agent.executeCalls != 1 {
		t.Errorf("聊天请求应走 ExecuteMessage，实际 execute=%d process=%d",
			agent.executeCalls, agent.processCalls)
	}
	if agent.processCalls != 0 {
		t.Errorf("聊天请求不应走 ProcessMessage，实际 process=%d", agent.processCalls)
	}
}

// TestInvoke_团队请求走ExecuteMessage 团队执行同样由 Runtime 持有。
func TestInvoke_团队请求走ExecuteMessage(t *testing.T) {
	agent := &recordingChatAgent{}
	rt := newSplitRuntime(agent)

	if _, err := rt.Invoke(context.Background(), newChatRequest(schema.ReqMethodChatSend, "team.work.normal", false)); err != nil {
		t.Fatalf("Invoke 不应报错: %v", err)
	}
	if agent.executeCalls != 1 {
		t.Errorf("团队请求应走 ExecuteMessage，实际 execute=%d process=%d",
			agent.executeCalls, agent.processCalls)
	}
}

// TestInvoke_零碎RPC走ProcessMessage 非 chat 轮次进门面队列。
func TestInvoke_零碎RPC走ProcessMessage(t *testing.T) {
	agent := &recordingChatAgent{}
	rt := newSplitRuntime(agent)

	if _, err := rt.Invoke(context.Background(), newChatRequest(schema.ReqMethodSessionRename, "agent.work.normal", false)); err != nil {
		t.Fatalf("Invoke 不应报错: %v", err)
	}
	if agent.processCalls != 1 {
		t.Errorf("零碎 RPC 应走 ProcessMessage，实际 process=%d execute=%d",
			agent.processCalls, agent.executeCalls)
	}
}

// TestStream_聊天请求走ExecuteMessageStream 流式同样分流。
func TestStream_聊天请求走ExecuteMessageStream(t *testing.T) {
	agent := &recordingChatAgent{}
	rt := newSplitRuntime(agent)

	if err := rt.Stream(context.Background(), newChatRequest(schema.ReqMethodChatSend, "agent.work.normal", true), nil); err != nil {
		t.Fatalf("Stream 不应报错: %v", err)
	}
	if agent.executeCalls != 1 {
		t.Errorf("流式聊天应走 ExecuteMessageStream，实际 execute=%d process=%d",
			agent.executeCalls, agent.processCalls)
	}
}

// TestStream_零碎RPC走ProcessMessageStream 非 chat 流式进门面队列。
func TestStream_零碎RPC走ProcessMessageStream(t *testing.T) {
	agent := &recordingChatAgent{}
	rt := newSplitRuntime(agent)

	if err := rt.Stream(context.Background(), newChatRequest(schema.ReqMethodSessionRename, "agent.work.normal", true), nil); err != nil {
		t.Fatalf("Stream 不应报错: %v", err)
	}
	if agent.processCalls != 1 {
		t.Errorf("零碎 RPC 流式应走 ProcessMessageStream，实际 process=%d execute=%d",
			agent.processCalls, agent.executeCalls)
	}
}

// TestInvoke_无sessionID走ProcessMessage 无会话标识不归 Runtime。
func TestInvoke_无sessionID走ProcessMessage(t *testing.T) {
	agent := &recordingChatAgent{}
	rt := newSplitRuntime(agent)
	request := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "ch-1",
		ReqMethod: schema.ReqMethodChatSend,
	}
	request.Params = splitTestJSON(map[string]any{"mode": "agent.work.normal", "query": "hi"})

	if _, err := rt.Invoke(context.Background(), request); err != nil {
		t.Fatalf("Invoke 不应报错: %v", err)
	}
	if agent.processCalls != 1 {
		t.Errorf("无 session_id 应走 ProcessMessage，实际 process=%d execute=%d",
			agent.processCalls, agent.executeCalls)
	}
}

// TestInvoke_执行错误仍透传 分流不影响错误传播。
func TestInvoke_执行错误仍透传(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(context.Context, *schema.AgentRequest, string, map[string]any, string) (string, string, ChatAgentPort, error) {
			return "", "", nil, errors.New("prepare failed")
		},
	})
	if _, err := rt.Invoke(context.Background(), newChatRequest(schema.ReqMethodChatSend, "agent.work.normal", false)); err == nil {
		t.Fatal("prepare 失败应透传错误")
	}
}
