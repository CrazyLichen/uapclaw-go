package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// keepaliveInterval 流式心跳间隔（对齐 Python _StreamKeepalive 默认 10s）
	keepaliveInterval = 10 * time.Second
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleUnary 处理非流式请求，对齐 Python _handle_unary。
// Python: agent_ws_server.py:3913 `_handle_unary` → `_handle_unary_impl`
func (s *AgentServer) handleUnary(ctx context.Context, request *schema.AgentRequest) {
	s.handleUnaryImpl(ctx, request)
}

// handleUnaryImpl 非流式处理：特殊方法分流 → runtime.Invoke → 逐事件发帧。
// Python: agent_ws_server.py:3941 _handle_unary_impl
func (s *AgentServer) handleUnaryImpl(ctx context.Context, request *schema.AgentRequest) {
	// 1. 特殊方法分流（对齐上游 _handle_initialize / _handle_session_create /
	// _handle_session_fork / _handle_acp_tool_response 的 4 个前置 return）
	switch request.ReqMethod {
	case schema.ReqMethodInitialize:
		resp, err := s.handleInitialize(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "INITIALIZE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodSessionCreate:
		resp, err := s.handleSessionCreate(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "SESSION_CREATE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodSessionFork:
		resp, err := s.handleSessionFork(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "SESSION_FORK_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodACPToolResponse:
		resp, err := s.handleACPToolResponse(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "ACP_TOOL_RESPONSE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	}

	// 2-6. 交给 AgentRuntime（选 agent → ensure_state → 执行 → check_exit → 控制事件）
	// Python: agent_ws_server.py:3941 _handle_unary_impl → runtime.invoke(...)
	events, err := s.agentRuntime.Invoke(ctx, request)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Msg("agent failed to process request")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_PROCESS_ERROR")
		return
	}
	for _, ev := range events {
		if ev.Payload == nil {
			continue
		}
		if !s.sendRuntimeEvent(ev, false, 0) {
			logger.Warn(logComponent).
				Str("request_id", request.RequestID).
				Msg("unary response send failed")
		}
	}
	logger.Info(logComponent).
		Str("request_id", request.RequestID).
		Msg("unary response sent")
}

// handleStream 处理流式请求，对齐 Python _handle_stream。
// Python: agent_ws_server.py:4627 `_handle_stream` → `_handle_stream_impl`
func (s *AgentServer) handleStream(ctx context.Context, request *schema.AgentRequest) {
	s.handleStreamImpl(ctx, request)
}

// handleStreamImpl 流式处理：登记流任务 → 心跳 → runtime.Stream → 逐 chunk 发帧。
// Python: agent_ws_server.py:4654 _handle_stream_impl
func (s *AgentServer) handleStreamImpl(ctx context.Context, request *schema.AgentRequest) {
	// 1. 会话键
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}
	// 2. 归一化参数
	request = swruntime.NormalizeRequest(request)
	// 3. 登记流任务（Go 的取消链路依赖该登记：handleCancel → cancelStreamTask 才能
	//    打断本次流的 ctx。上游 `_session_stream_tasks` 只登记 targeted_expert/非
	//    session-runtime 请求，但其取消走 adapter interrupt；Go 两者都要保留，
	//    故此处始终登记，见 spec §13 偏差）
	streamCtx, finish := s.beginStreamTask(ctx, sessionID, request.RequestID)
	ctx = streamCtx
	defer finish()

	// 4. 心跳
	interval := s.streamKeepaliveInterval
	if interval <= 0 {
		interval = keepaliveInterval
	}
	keepalive := NewStreamKeepalive(func() {
		s.sendKeepalive(request.RequestID, request.ChannelID)
	}, interval)
	keepalive.Start()

	// 5-6. 流式执行：控制事件不占 sequence、不重置心跳
	chunkCount := 0
	streamErr := s.agentRuntime.Stream(ctx, request, func(ev *swruntime.RuntimeEvent) bool {
		// 控制事件正常经 AgentRuntime 的 on_control_event 回调直接推 push 帧；
		// 此处分流对齐上游 _handle_stream_impl 对自定义 Runtime 的兼容分支
		// （agent_ws_server.py:4711-4716）。
		if ev.EventType() == swruntime.PlanModeExitedEvent {
			s.sendRuntimeEvent(ev, true, chunkCount)
			return true
		}
		chunkCount++
		keepalive.NotifyActivity(ev.IsComplete)
		if !s.sendRuntimeEvent(ev, true, chunkCount-1) {
			logger.Warn(logComponent).
				Str("request_id", request.RequestID).
				Int("sequence", chunkCount-1).
				Msg("streaming response stopped: chunk send failed")
			keepalive.SignalStop()
			return false
		}
		return true
	})

	// 7. 收尾：先阻止新心跳，再等 goroutine 退出
	keepalive.SignalStop()
	keepalive.Stop()

	if streamErr != nil {
		logger.Error(logComponent).
			Err(streamErr).
			Str("request_id", request.RequestID).
			Msg("agent stream processing failed")
		s.writeErrorResponse(request.RequestID, request.ChannelID, streamErr.Error(), "AGENT_STREAM_ERROR")
		return
	}

	logger.Debug(logComponent).
		Str("request_id", request.RequestID).
		Int("chunk_count", chunkCount).
		Msg("stream processing completed")
}

// handleCancel 处理取消/中断请求，对齐 Python _handle_cancel。
// 流式任务取消留在 server 层（对齐上游 _handle_message 的 intent 判定），
// agent 选取与 process_message 交给 runtime.CancelRequest。
func (s *AgentServer) handleCancel(ctx context.Context, request *schema.AgentRequest) {
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}

	// 1. 取消流式 goroutine（对齐 Python：仅 cancel/supplement 时取消流式任务，pause/resume 不取消）
	intent := "cancel"
	if len(request.Params) > 0 {
		var params map[string]any
		if json.Unmarshal(request.Params, &params) == nil {
			if i, ok := params["intent"].(string); ok && i != "" {
				intent = i
			}
		}
	}
	if intent == "cancel" || intent == "supplement" {
		s.cancelStreamTask(sessionID)
	}

	// 2. 复用既有 Runtime agent 取消（三级 fallback 已上移到 runtime.CancelRequest）
	resp, err := swruntime.CancelRequest(ctx, s.agentManager, request, false)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Msg("agent interrupt processing failed")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_INTERRUPT_ERROR")
		return
	}
	s.writeResponse(request.RequestID, request.ChannelID, resp)
}

// sendRuntimeEvent 按事件类型选择 push 帧或响应/流帧。
// Python: agent_ws_server.py:5024 _send_runtime_event
func (s *AgentServer) sendRuntimeEvent(ev *swruntime.RuntimeEvent, streaming bool, sequence int) bool {
	if ev.EventType() == swruntime.PlanModeExitedEvent {
		// SendPush 内部按 Python 语义吞掉所有发送异常并记日志，这里不重复处理错误
		_ = s.SendPush(context.Background(), map[string]any{
			"channel_id": ev.ChannelID,
			"session_id": ev.SessionID,
			"payload":    ev.Payload,
		})
		return true
	}
	if streaming {
		return s.writeChunk(ev.RequestID, ev.ChannelID, ev.ToChunk(), sequence, true)
	}
	return s.writeResponse(ev.RequestID, ev.ChannelID, &schema.AgentResponse{
		RequestID: ev.RequestID,
		ChannelID: ev.ChannelID,
		OK:        ev.OK,
		Payload:   ev.Payload,
	})
}
