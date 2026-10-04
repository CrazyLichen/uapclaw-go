package evolution

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	gatewaypush "github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionPushContext evolution 推送上下文。
// Python: EvolutionPushContext
type EvolutionPushContext struct {
	// Transport 推送传输
	Transport gatewaypush.GatewayPushTransport
	// ChannelID 通道标识（可能为空）
	ChannelID string
	// SessionID 会话标识
	SessionID string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildPushMessageFunc 构建 server_push 消息的函数类型。
// Python: build_server_push_message 回调参数（关键字参数，位置无关）。
// 参数顺序与 session.BuildServerPushMessage 一致，可直接赋值。
type BuildPushMessageFunc func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any

// ParseStreamChunkFunc 解析流式 chunk 的函数类型。
// Python: parse_stream_chunk 回调参数
type ParseStreamChunkFunc func(evt map[string]any) map[string]any

// BroadcastEventFunc 广播事件的函数类型。
// Python: broadcast_event 回调参数
type BroadcastEventFunc func(channelID *string, sessionID string, parsed map[string]any)

// ──────────────────────────── 非导出函数 ────────────────────────────

// PushEvolutionStatus 推送状态。
// Python: push_evolution_status()
func PushEvolutionStatus(
	ctx context.Context,
	pushCtx *EvolutionPushContext,
	update evolutionlogic.EvolutionStatusUpdate,
	buildMsgFn BuildPushMessageFunc,
	includePayloadRequestID ...bool,
) error {
	payload := map[string]any{
		"event_type": "chat.evolution_status",
		"status":     update.Status,
		"stage":      update.Stage,
		"message":    update.Message,
	}

	includeReqID := true
	if len(includePayloadRequestID) > 0 {
		includeReqID = includePayloadRequestID[0]
	}
	if includeReqID {
		payload["request_id"] = update.RequestID
	}

	msg := buildMsgFn(pushCtx.SessionID, update.RequestID, payload, pushCtx.ChannelID)
	return pushCtx.Transport.SendPush(ctx, msg)
}

// PushEvolutionEvent 推送事件。
// Python: push_evolution_event()
func PushEvolutionEvent(
	ctx context.Context,
	pushCtx *EvolutionPushContext,
	requestID string,
	evt map[string]any,
	buildMsgFn BuildPushMessageFunc,
) error {
	// Python: payload = event_payload_dict(evt) — 浅拷贝避免修改原始 evt
	payload := evolutionlogic.EventPayloadDict(evt)

	evtType := evolutionlogic.EventType(evt)
	if evtType != "" {
		if _, ok := payload["event_type"]; !ok {
			payload["event_type"] = evtType
		}
	}
	if _, ok := payload["request_id"]; !ok {
		payload["request_id"] = requestID
	}

	msg := buildMsgFn(pushCtx.SessionID, requestID, payload, pushCtx.ChannelID)
	return pushCtx.Transport.SendPush(ctx, msg)
}

// BroadcastEvolutionProgress 广播进度。
// Python: broadcast_evolution_progress()
func BroadcastEvolutionProgress(
	ctx context.Context,
	channelID *string,
	sessionID string,
	events []map[string]any,
	parseChunk ParseStreamChunkFunc,
	broadcastEvent BroadcastEventFunc,
) error {
	for _, evt := range events {
		if evolutionlogic.IsEvolutionApprovalEvent(evt) || evolutionlogic.IsEvolutionOutcomeEvent(evt) || evolutionlogic.TeamEvolutionTerminalProgress(evt) != nil {
			continue
		}
		parsed := parseChunk(evt)
		if parsed != nil {
			broadcastEvent(channelID, sessionID, parsed)
		}
	}
	return nil
}

// PushEvolutionProgress 推送进度。
// Python: push_evolution_progress()
func PushEvolutionProgress(
	ctx context.Context,
	pushCtx *EvolutionPushContext,
	requestID string,
	events []map[string]any,
	parseChunk ParseStreamChunkFunc,
	buildMsgFn BuildPushMessageFunc,
) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("event_type", "EVOLUTION_PUSH_RECOVERED").
				Any("recover", r).
				Msg("PushEvolutionProgress panic recovered")
		}
	}()
	for _, evt := range events {
		if evolutionlogic.IsEvolutionApprovalEvent(evt) || evolutionlogic.IsEvolutionOutcomeEvent(evt) || evolutionlogic.TeamEvolutionTerminalProgress(evt) != nil {
			continue
		}
		parsed := parseChunk(evt)
		if parsed == nil {
			continue
		}
		msg := buildMsgFn(pushCtx.SessionID, requestID, parsed, pushCtx.ChannelID)
		if err := pushCtx.Transport.SendPush(ctx, msg); err != nil {
			logger.Warn(logComponent).
				Str("request_id", requestID).
				Str("session_id", pushCtx.SessionID).
				Err(err).
				Msg("failed to push evolution progress")
		}
	}
	return nil
}
