package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// stubChatAgent 固定响应的聊天 agent。
type stubChatAgent struct {
	// chunks 流式返回的片段
	chunks []*schema.AgentResponseChunk
}

// ProcessMessage 返回一条 chat.final 响应
func (s *stubChatAgent) ProcessMessage(_ context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{
		RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final", "content": "ok"},
	}, nil
}

// ProcessMessageStream 返回预置片段后关闭通道
func (s *stubChatAgent) ProcessMessageStream(_ context.Context, _ *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk, len(s.chunks))
	for _, c := range s.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// PlanStateAccessor 无状态存储
func (s *stubChatAgent) PlanStateAccessor() swruntime.PlanStateAccessor { return nil }

// ──────────────────────────── 测试 ────────────────────────────

// TestHandleUnaryImpl_走runtime并发帧 验证非流式入口经 AgentRuntime 输出响应帧。
func TestHandleUnaryImpl_走runtime并发帧(t *testing.T) {
	s, tr := newTestServer()
	agent := &stubChatAgent{}
	s.agentRuntime = swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string) (string, string, swruntime.ChatAgentPort, error) {
			return "agent", "", agent, nil
		},
	})

	req := &schema.AgentRequest{RequestID: "req-unary-1", ChannelID: "web", Params: json.RawMessage(`{"mode":"agent.work.normal"}`)}
	s.handleUnaryImpl(context.Background(), req)

	frame := readFrame(t, tr)
	resp, err := parseUnaryFrame(frame)
	require.NoError(t, err)
	require.True(t, resp.OK)
	require.Equal(t, "chat.final", resp.Payload["event_type"])
}

// TestHandleStreamImpl_逐帧带sequence 验证流式帧的 sequence 递增与结束帧。
func TestHandleStreamImpl_逐帧带sequence(t *testing.T) {
	s, tr := newTestServer()
	agent := &stubChatAgent{chunks: []*schema.AgentResponseChunk{
		{Payload: map[string]any{"event_type": "chat.delta", "content": "a"}},
		{Payload: map[string]any{"event_type": "chat.final"}, IsComplete: true},
	}}
	s.agentRuntime = swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string) (string, string, swruntime.ChatAgentPort, error) {
			return "agent", "", agent, nil
		},
	})

	sid := "sess-stream-1"
	req := &schema.AgentRequest{
		RequestID: "req-stream-1", ChannelID: "web", SessionID: &sid,
		Params: json.RawMessage(`{"mode":"agent.work.normal"}`), IsStream: true,
	}
	s.handleStreamImpl(context.Background(), req)

	first := readFrame(t, tr)
	require.Equal(t, float64(0), first["sequence"], "首个 chunk 从 0 开始编号")
	second := readFrame(t, tr)
	require.Equal(t, float64(1), second["sequence"])
}

// TestSendRuntimeEvent_控制事件走push 验证 plan_mode_exited 走 push 帧而非流帧。
func TestSendRuntimeEvent_控制事件走push(t *testing.T) {
	s, tr := newTestServer()
	ev := &swruntime.RuntimeEvent{
		RequestID: "req-ctrl", ChannelID: "web", SessionID: "sess-ctrl",
		Payload: map[string]any{"event_type": swruntime.PlanModeExitedEvent, "mode": "agent.work.normal"},
	}
	require.True(t, s.sendRuntimeEvent(ev, true, 3))

	frame := readFrame(t, tr)
	metadata, _ := frame["metadata"].(map[string]any)
	require.Equal(t, true, metadata["_jiuwenswarm_server_push"], "控制事件必须是 push 帧")
}

// TestHandleCancel_无agent返回interrupt_result 验证取消链路的兜底响应。
func TestHandleCancel_无agent返回interrupt_result(t *testing.T) {
	s, tr := newTestServer()
	sid := "sess-cancel-unit"
	req := &schema.AgentRequest{
		RequestID: "req-cancel-unit", ChannelID: "web", SessionID: &sid,
		ReqMethod: schema.ReqMethodChatCancel, Params: json.RawMessage(`{"intent":"cancel"}`),
	}
	s.handleCancel(context.Background(), req)

	resp, err := parseUnaryFrame(readFrame(t, tr))
	require.NoError(t, err)
	require.True(t, resp.OK)
	require.Equal(t, "chat.interrupt_result", resp.Payload["event_type"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readFrame 读取一帧出站 JSON（带超时）。
func readFrame(t *testing.T, tr *transport.ChannelTransport) map[string]any {
	t.Helper()
	recvCh, err := tr.Recv()
	require.NoError(t, err)
	select {
	case data := <-recvCh:
		var frame map[string]any
		require.NoError(t, json.Unmarshal(data, &frame))
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("等待出站帧超时")
		return nil
	}
}

// parseUnaryFrame 把出站帧解析为 AgentResponse。
func parseUnaryFrame(frame map[string]any) (*schema.AgentResponse, error) {
	return e2a.ParseAgentServerWireUnary(frame)
}
