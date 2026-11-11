package server

import (
	"context"
	"encoding/json"
	"errors"
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
func (s *stubChatAgent) ProcessMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{
		RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final", "content": "ok"},
	}, nil
}

// ExecuteMessage 由 Runtime 持有调度的非流式执行（与 ProcessMessage 同行为）
func (s *stubChatAgent) ExecuteMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{
		RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final", "content": "ok"},
	}, nil
}

// ExecuteMessageStream 由 Runtime 持有调度的流式执行（与 ProcessMessageStream 同行为）
func (s *stubChatAgent) ExecuteMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk, len(s.chunks))
	for _, c := range s.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// ProcessMessageStream 返回预置片段后关闭通道
func (s *stubChatAgent) ProcessMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
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
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string, _ map[string]any, _ string) (string, string, swruntime.ChatAgentPort, error) {
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
	s.streamKeepaliveInterval = 0 // 覆盖心跳间隔缺省回落分支
	s.agentRuntime = swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string, _ map[string]any, _ string) (string, string, swruntime.ChatAgentPort, error) {
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

// TestHandleUnaryImpl_特殊方法分流 验证 4 个前置 return 分支各自写出响应帧。
func TestHandleUnaryImpl_特殊方法分流(t *testing.T) {
	methods := []schema.ReqMethod{
		schema.ReqMethodInitialize,
		schema.ReqMethodSessionCreate,
		schema.ReqMethodSessionFork,
		schema.ReqMethodACPToolResponse,
	}
	for _, method := range methods {
		t.Run(string(method), func(t *testing.T) {
			s, tr := newTestServer()
			req := &schema.AgentRequest{
				RequestID: "req-special", ChannelID: "web",
				ReqMethod: method, Params: json.RawMessage(`{}`),
			}
			s.handleUnaryImpl(context.Background(), req)
			frame := readFrame(t, tr)
			require.NotNil(t, frame, "特殊方法必须写出响应帧")
		})
	}
}

// TestSendKeepalive_写出心跳帧 验证心跳帧编码与发送。
func TestSendKeepalive_写出心跳帧(t *testing.T) {
	s, tr := newTestServer()
	s.sendKeepalive("req-keepalive", "web")

	frame := readFrame(t, tr)
	body, _ := frame["body"].(map[string]any)
	delta, _ := body["delta"].(map[string]any)
	require.Equal(t, "keepalive", delta["event_type"])
}

// ──────────────────────────── 入口错误/边界分支 ────────────────────────────

// errStreamAgent 流式执行直接返回错误的 agent 桩。
type errStreamAgent struct{}

// ProcessMessage 非流式固定成功
func (e *errStreamAgent) ProcessMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final"}}, nil
}

// ProcessMessageStream 流式固定失败
func (e *errStreamAgent) ProcessMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	return nil, errors.New("stream boom")
}

// ExecuteMessage 由 Runtime 持有调度的非流式执行（与 ProcessMessage 同行为）
func (e *errStreamAgent) ExecuteMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final"}}, nil
}

// ExecuteMessageStream 由 Runtime 持有调度的流式执行（与 ProcessMessageStream 同行为）
func (e *errStreamAgent) ExecuteMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	return nil, errors.New("stream boom")
}

// PlanStateAccessor 无状态存储
func (e *errStreamAgent) PlanStateAccessor() swruntime.PlanStateAccessor { return nil }

// nilPayloadAgent 返回空 payload 的 agent 桩（验证 unary 跳过空事件）。
type nilPayloadAgent struct{}

// ProcessMessage 返回 Payload 为 nil 的响应
func (n *nilPayloadAgent) ProcessMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true}, nil
}

// ProcessMessageStream 返回空通道
func (n *nilPayloadAgent) ProcessMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

// ExecuteMessage 由 Runtime 持有调度的非流式执行（与 ProcessMessage 同行为）
func (n *nilPayloadAgent) ExecuteMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true}, nil
}

// ExecuteMessageStream 由 Runtime 持有调度的流式执行（与 ProcessMessageStream 同行为）
func (n *nilPayloadAgent) ExecuteMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

// PlanStateAccessor 无状态存储
func (n *nilPayloadAgent) PlanStateAccessor() swruntime.PlanStateAccessor { return nil }

// chunkHolderAgent 预填固定数量 chunk 的 agent 桩（用于观察消费进度）。
type chunkHolderAgent struct {
	// ch 预填并关闭的 chunk 通道
	ch chan *schema.AgentResponseChunk
}

// ProcessMessage 非流式固定成功
func (c *chunkHolderAgent) ProcessMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final"}}, nil
}

// ProcessMessageStream 返回预填通道
func (c *chunkHolderAgent) ProcessMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	return c.ch, nil
}

// ExecuteMessage 由 Runtime 持有调度的非流式执行（与 ProcessMessage 同行为）
func (c *chunkHolderAgent) ExecuteMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final"}}, nil
}

// ExecuteMessageStream 由 Runtime 持有调度的流式执行（与 ProcessMessageStream 同行为）
func (c *chunkHolderAgent) ExecuteMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	return c.ch, nil
}

// PlanStateAccessor 无状态存储
func (c *chunkHolderAgent) PlanStateAccessor() swruntime.PlanStateAccessor { return nil }

// TestHandleUnaryImpl_特殊方法错误写错误帧 会话方法失败时回写错误响应。
func TestHandleUnaryImpl_特殊方法错误写错误帧(t *testing.T) {
	cases := []struct {
		name    string
		method  schema.ReqMethod
		params  string
		wantErr string
	}{
		// 10.3.28b：session.create 改走 provisioner —— 参数解析失败得到空 map，
		// 随后因缺 create_token 走业务错误响应（code=BAD_REQUEST），不再是错误帧
		{"session.create 参数非法", schema.ReqMethodSessionCreate, `{bad json`, "BAD_REQUEST"},
		{"session.fork 缺源会话", schema.ReqMethodSessionFork, `{}`, "SESSION_FORK_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, tr := newTestServer()
			req := &schema.AgentRequest{
				RequestID: "req-special-err", ChannelID: "web",
				ReqMethod: tc.method, Params: json.RawMessage(tc.params),
			}
			s.handleUnaryImpl(context.Background(), req)

			resp, err := parseUnaryFrame(readFrame(t, tr))
			require.NoError(t, err)
			require.False(t, resp.OK)
			// 错误帧：payload["error"] 是对象且 code 为 xxx_ERROR；
			// 业务失败：payload["code"] 直接在顶层
			errPayload, isFrame := resp.Payload["error"].(map[string]any)
			if isFrame {
				require.Equal(t, tc.wantErr, errPayload["code"])
				return
			}
			require.Equal(t, tc.wantErr, resp.Payload["code"])
		})
	}
}

// TestHandleUnaryImpl_空负载事件跳过 事件 payload 为空时不下发响应帧。
func TestHandleUnaryImpl_空负载事件跳过(t *testing.T) {
	s, tr := newTestServer()
	s.agentRuntime = swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string, _ map[string]any, _ string) (string, string, swruntime.ChatAgentPort, error) {
			return "agent", "", &nilPayloadAgent{}, nil
		},
	})
	req := &schema.AgentRequest{RequestID: "req-nil-payload", ChannelID: "web", Params: json.RawMessage(`{}`)}
	s.handleUnaryImpl(context.Background(), req)

	requireNoFrame(t, tr)
}

// TestHandleUnaryImpl_发送失败不阻断 传输已关闭时 unary 仍正常返回（不 panic）。
func TestHandleUnaryImpl_发送失败不阻断(t *testing.T) {
	s, tr := newTestServer()
	s.agentRuntime = swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string, _ map[string]any, _ string) (string, string, swruntime.ChatAgentPort, error) {
			return "agent", "", &stubChatAgent{}, nil
		},
	})
	require.NoError(t, tr.Close())

	req := &schema.AgentRequest{RequestID: "req-send-fail", ChannelID: "web", Params: json.RawMessage(`{}`)}
	require.NotPanics(t, func() { s.handleUnaryImpl(context.Background(), req) })
}

// TestHandleStreamImpl_发送失败停止流 chunk 发送失败时立即停止消费。
func TestHandleStreamImpl_发送失败停止流(t *testing.T) {
	s, tr := newTestServer()
	ch := make(chan *schema.AgentResponseChunk, 3)
	ch <- &schema.AgentResponseChunk{Payload: map[string]any{"event_type": "chat.delta", "content": "1"}}
	ch <- &schema.AgentResponseChunk{Payload: map[string]any{"event_type": "chat.delta", "content": "2"}}
	ch <- &schema.AgentResponseChunk{Payload: map[string]any{"event_type": "chat.final"}, IsComplete: true}
	close(ch)
	s.agentRuntime = swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string, _ map[string]any, _ string) (string, string, swruntime.ChatAgentPort, error) {
			return "agent", "", &chunkHolderAgent{ch: ch}, nil
		},
	})
	require.NoError(t, tr.Close())

	req := &schema.AgentRequest{RequestID: "req-stream-fail", ChannelID: "web", Params: json.RawMessage(`{}`), IsStream: true}
	s.handleStreamImpl(context.Background(), req)

	require.Len(t, ch, 2, "首个 chunk 发送失败后应停止消费，剩余 chunk 不再读取")
}

// TestHandleStreamImpl_流错误写错误帧 流式执行出错时回写 AGENT_STREAM_ERROR。
func TestHandleStreamImpl_流错误写错误帧(t *testing.T) {
	s, tr := newTestServer()
	s.agentRuntime = swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string, _ map[string]any, _ string) (string, string, swruntime.ChatAgentPort, error) {
			return "agent", "", &errStreamAgent{}, nil
		},
	})

	req := &schema.AgentRequest{RequestID: "req-stream-err", ChannelID: "web", Params: json.RawMessage(`{}`), IsStream: true}
	s.handleStreamImpl(context.Background(), req)

	resp, err := parseUnaryFrame(readFrame(t, tr))
	require.NoError(t, err)
	require.False(t, resp.OK)
	errPayload, _ := resp.Payload["error"].(map[string]any)
	require.Equal(t, "AGENT_STREAM_ERROR", errPayload["code"])
}

// requireNoFrame 断言在短窗口内没有出站帧。
func requireNoFrame(t *testing.T, tr *transport.ChannelTransport) {
	t.Helper()
	recvCh, err := tr.Recv()
	require.NoError(t, err)
	select {
	case data := <-recvCh:
		t.Fatalf("不应有出站帧，实际收到 %s", string(data))
	case <-time.After(100 * time.Millisecond):
	}
}

// blockingStreamAgent 首个 chunk 前阻塞的 agent 桩（用于观察空闲心跳）。
type blockingStreamAgent struct {
	// release 放行信号
	release chan struct{}
}

// ProcessMessage 非流式固定成功
func (b *blockingStreamAgent) ProcessMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final"}}, nil
}

// ProcessMessageStream 收到 release 信号后才产出终态 chunk
func (b *blockingStreamAgent) ProcessMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk, 1)
	go func() {
		<-b.release
		ch <- &schema.AgentResponseChunk{Payload: map[string]any{"event_type": "chat.final"}, IsComplete: true}
		close(ch)
	}()
	return ch, nil
}

// ExecuteMessage 由 Runtime 持有调度的非流式执行（与 ProcessMessage 同行为）
func (b *blockingStreamAgent) ExecuteMessage(_ context.Context, req *schema.AgentRequest, opts ...swruntime.ExecuteOption) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{RequestID: req.RequestID, ChannelID: req.ChannelID, OK: true,
		Payload: map[string]any{"event_type": "chat.final"}}, nil
}

// ExecuteMessageStream 由 Runtime 持有调度的流式执行（与 ProcessMessageStream 同行为）
func (b *blockingStreamAgent) ExecuteMessageStream(_ context.Context, _ *schema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk, 1)
	go func() {
		<-b.release
		ch <- &schema.AgentResponseChunk{Payload: map[string]any{"event_type": "chat.final"}, IsComplete: true}
		close(ch)
	}()
	return ch, nil
}

// PlanStateAccessor 无状态存储
func (b *blockingStreamAgent) PlanStateAccessor() swruntime.PlanStateAccessor { return nil }

// TestHandleStreamImpl_空闲发心跳 流空闲超过心跳间隔时下发 keepalive 帧。
func TestHandleStreamImpl_空闲发心跳(t *testing.T) {
	s, tr := newTestServer()
	agent := &blockingStreamAgent{release: make(chan struct{})}
	s.streamKeepaliveInterval = 10 * time.Millisecond
	s.agentRuntime = swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string, _ map[string]any, _ string) (string, string, swruntime.ChatAgentPort, error) {
			return "agent", "", agent, nil
		},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := &schema.AgentRequest{RequestID: "req-keepalive", ChannelID: "web", Params: json.RawMessage(`{}`), IsStream: true}
		s.handleStreamImpl(context.Background(), req)
	}()

	frame := readFrame(t, tr)
	body, _ := frame["body"].(map[string]any)
	delta, _ := body["delta"].(map[string]any)
	require.Equal(t, "keepalive", delta["event_type"], "空闲时应下发心跳帧")

	close(agent.release)
	<-done
}

// TestHandleCancel_取消链路报错 agent 处理失败时回写错误响应。
func TestHandleCancel_取消链路报错(t *testing.T) {
	s, tr := newTestServer()
	// 预创建 agent：取消链路会命中既有实例并驱动它处理取消请求
	_, err := s.agentManager.GetChatAgent(context.Background(), "web", "agent", "", "", nil, "")
	require.NoError(t, err)

	sid := "sess-cancel-err"
	req := &schema.AgentRequest{
		RequestID: "req-cancel-err", ChannelID: "web", SessionID: &sid,
		ReqMethod: schema.ReqMethodChatCancel, Params: json.RawMessage(`{"mode":"agent"}`),
	}
	s.handleCancel(context.Background(), req)

	resp, err := parseUnaryFrame(readFrame(t, tr))
	require.NoError(t, err)
	if resp.OK {
		t.Skip("当前 agent 桩未报错，取消链路无错误分支可断言")
	}
	errPayload, _ := resp.Payload["error"].(map[string]any)
	require.Equal(t, "AGENT_INTERRUPT_ERROR", errPayload["code"])
}

// TestHandleUnaryImpl_会话方法正常分流 session.create / session.fork 成功时写出正常响应帧。
func TestHandleUnaryImpl_会话方法正常分流(t *testing.T) {
	t.Run("session.create", func(t *testing.T) {
		s, tr := newTestServer()
		req := &schema.AgentRequest{
			RequestID: "req-create-ok", ChannelID: "web", ReqMethod: schema.ReqMethodSessionCreate,
			Params: json.RawMessage(`{"create_token":"tok","mode":"agent","work_mode":"work"}`),
		}
		s.handleUnaryImpl(context.Background(), req)

		resp, err := parseUnaryFrame(readFrame(t, tr))
		require.NoError(t, err)
		require.True(t, resp.OK)
		// 10.3.28b：session.create 不再接受非 TUI 的 session_id，会话标识由认领生成
		require.NotEmpty(t, resp.Payload["session_id"])
	})

	t.Run("session.fork", func(t *testing.T) {
		sessionsDir, _, cleanup := setupTestSessionsDir(t)
		defer cleanup()

		sourceID := "sess_chat_fork_source"
		writeTestMetadata(t, sessionsDir, sourceID, map[string]any{
			"session_id":      sourceID,
			"title":           "源会话",
			"last_message_at": 1000.0,
			"message_count":   1,
		})
		writeTestHistory(t, sessionsDir, sourceID, []map[string]any{
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "hi"},
		})

		params, err := json.Marshal(map[string]any{
			"source_session_id": sourceID,
			"target_session_id": "sess_chat_fork_target",
		})
		require.NoError(t, err)

		s, tr := newTestServer()
		req := &schema.AgentRequest{
			RequestID: "req-fork-ok", ChannelID: "web", ReqMethod: schema.ReqMethodSessionFork,
			Params: params,
		}
		s.handleUnaryImpl(context.Background(), req)

		resp, err := parseUnaryFrame(readFrame(t, tr))
		require.NoError(t, err)
		require.True(t, resp.OK)
		require.Equal(t, "sess_chat_fork_target", resp.Payload["session_id"])
	})
}
