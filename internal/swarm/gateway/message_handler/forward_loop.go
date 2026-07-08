package message_handler

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// forwardLoop 入站转发主循环
//
// 从 userMessages channel 持续读取消息，依次处理：
//  1. _handle_channel_control(msg) → 如果是 slash 命令则处理，跳过后续
//  2. _apply_channel_state(msg) → 注入 session_id / mode
//  3. 根据 req_method 分支转发到 AgentServer
//
// 对齐 Python _forward_loop (L2163-L2556)
func (mh *MessageHandler) forwardLoop(ctx context.Context) {
	logger.Info(logComponent).Msg("入站转发循环已启动")
	defer logger.Info(logComponent).Msg("入站转发循环已退出")

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-mh.userMessages:
			if msg == nil {
				continue
			}
			mh.processInbound(ctx, msg)
		}
	}
}

// processInbound 处理单条入站消息
func (mh *MessageHandler) processInbound(ctx context.Context, msg *schema.Message) {
	// 1. 尝试处理 slash 命令（仅受控渠道）
	if mh.handleChannelControl(msg) {
		return // 已作为控制指令处理
	}

	// 2. 注入渠道状态（session_id / mode）
	mh.ApplyChannelState(msg)

	// 3. 根据请求方法转发
	switch msg.ReqMethod {
	case schema.ReqMethodChatSend:
		mh.handleChatSend(ctx, msg)
	case schema.ReqMethodChatCancel:
		mh.handleChatCancel(ctx, msg)
	case schema.ReqMethodChatResume:
		mh.handleChatResume(ctx, msg)
	case schema.ReqMethodChatAnswer:
		mh.handleChatUserAnswer(ctx, msg)
	default:
		// 其他方法直接转发
		mh.forwardToAgent(ctx, msg)
	}
}

// handleChatSend 处理 chat.send 请求
func (mh *MessageHandler) handleChatSend(ctx context.Context, msg *schema.Message) {
	mh.forwardToAgent(ctx, msg)
}

// handleChatCancel 处理 chat.cancel/interrupt 请求
func (mh *MessageHandler) handleChatCancel(ctx context.Context, msg *schema.Message) {
	mh.forwardToAgent(ctx, msg)
}

// handleChatResume 处理 chat.resume 请求
func (mh *MessageHandler) handleChatResume(ctx context.Context, msg *schema.Message) {
	mh.forwardToAgent(ctx, msg)
}

// handleChatUserAnswer 处理 chat.user_answer 请求
func (mh *MessageHandler) handleChatUserAnswer(ctx context.Context, msg *schema.Message) {
	mh.forwardToAgent(ctx, msg)
}

// forwardToAgent 转发消息到 AgentServer
func (mh *MessageHandler) forwardToAgent(ctx context.Context, msg *schema.Message) {
	if mh.transport == nil {
		logger.Warn(logComponent).
			Str("event_type", "forward_no_transport").
			Str("msg_id", msg.ID).
			Msg("Transport 为空，无法转发")
		return
	}

	// Message → E2AEnvelope
	envelope := e2a.MessageToE2AOrFallback(msg)

	// 根据是否流式选择处理方式
	if msg.IsStream {
		go mh.processStream(ctx, msg, envelope)
	} else {
		go mh.processNonStreamRequest(ctx, msg, envelope)
	}
}

// processStream 流式处理：发送请求并持续读取响应 chunk
//
// 对齐 Python process_stream
func (mh *MessageHandler) processStream(ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope) {
	requestID := envelope.RequestID
	streamCtx, streamCancel := context.WithCancel(ctx)
	mh.registerStreamTask(requestID, msg.SessionID, msg.Metadata, streamCancel)
	defer mh.unregisterStreamTask(requestID)

	// 发送请求
	if err := mh.transport.Send(streamCtx, envelope); err != nil {
		logger.Error(logComponent).
			Str("event_type", "stream_send_error").
			Err(err).
			Str("request_id", requestID).
			Msg("流式请求发送失败")
		return
	}

	// 读取响应通道
	recvCh, err := mh.transport.Recv()
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "stream_recv_error").
			Err(err).
			Str("request_id", requestID).
			Msg("获取响应通道失败")
		return
	}

	for {
		select {
		case <-streamCtx.Done():
			return
		case resp, ok := <-recvCh:
			if !ok {
				return
			}
			if resp == nil {
				continue
			}

			// E2AResponse → AgentResponseChunk
			chunk, err := e2a.E2AResponseToAgentChunk(resp)
			if err != nil {
				logger.Warn(logComponent).
					Str("event_type", "stream_chunk_parse_error").
					Err(err).
					Msg("流式 chunk 解析失败")
				continue
			}

			// 跳过终止哨兵
			if IsTerminalStreamChunk(chunk) {
				return
			}

			// AgentResponseChunk → Message → robotMessages
			reqMetadata := mh.getStreamMetadata(requestID)
			outMsg := ChunkToMessage(chunk, mh.getStreamSessionID(requestID), reqMetadata)
			mh.enqueueOutbound(outMsg)
		}
	}
}

// processNonStreamRequest 非流式处理：发送请求并等待完整响应
//
// 对齐 Python _process_non_stream_request
func (mh *MessageHandler) processNonStreamRequest(ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope) {
	requestID := envelope.RequestID

	// 发送请求
	if err := mh.transport.Send(ctx, envelope); err != nil {
		logger.Error(logComponent).
			Str("event_type", "non_stream_send_error").
			Err(err).
			Str("request_id", requestID).
			Msg("非流式请求发送失败")
		return
	}

	// 读取响应通道
	recvCh, err := mh.transport.Recv()
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "non_stream_recv_error").
			Err(err).
			Str("request_id", requestID).
			Msg("获取响应通道失败")
		return
	}

	select {
	case <-ctx.Done():
		return
	case resp, ok := <-recvCh:
		if !ok || resp == nil {
			return
		}

		// E2AResponse → AgentResponse
		agentResp, err := e2a.E2AResponseToAgentResponse(resp)
		if err != nil {
			logger.Warn(logComponent).
				Str("event_type", "non_stream_parse_error").
				Err(err).
				Msg("非流式响应解析失败")
			return
		}

		// AgentResponse → Message → robotMessages
		outMsg := ResponseToMessage(agentResp, msg.SessionID, msg.Metadata)
		mh.enqueueOutbound(outMsg)
	}
}

// enqueueOutbound 将消息写入出站 channel
func (mh *MessageHandler) enqueueOutbound(msg *schema.Message) {
	select {
	case mh.robotMessages <- msg:
	default:
		logger.Warn(logComponent).
			Str("event_type", "outbound_queue_full").
			Str("msg_id", msg.ID).
			Msg("出站消息队列已满，丢弃消息")
	}
}

// registerStreamTask 注册流式任务
func (mh *MessageHandler) registerStreamTask(requestID, sessionID string, metadata map[string]any, cancel context.CancelFunc) {
	mh.streamMu.Lock()
	defer mh.streamMu.Unlock()
	mh.streamTasks[requestID] = cancel
	mh.streamSessions[requestID] = sessionID
	if metadata != nil {
		mh.streamMetadata[requestID] = metadata
	}
}

// unregisterStreamTask 注销流式任务
func (mh *MessageHandler) unregisterStreamTask(requestID string) {
	mh.streamMu.Lock()
	defer mh.streamMu.Unlock()
	delete(mh.streamTasks, requestID)
	delete(mh.streamSessions, requestID)
	delete(mh.streamMetadata, requestID)
	delete(mh.streamModes, requestID)
}

// getStreamMetadata 获取流式任务的请求 metadata
func (mh *MessageHandler) getStreamMetadata(requestID string) map[string]any {
	mh.streamMu.RLock()
	defer mh.streamMu.RUnlock()
	return mh.streamMetadata[requestID]
}

// getStreamSessionID 获取流式任务的 sessionID
func (mh *MessageHandler) getStreamSessionID(requestID string) string {
	mh.streamMu.RLock()
	defer mh.streamMu.RUnlock()
	return mh.streamSessions[requestID]
}
