package message_handler

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// pushLoop 持续消费 push 通道消息
//
// 从 ChannelTransport.PushCh() 持续读取 → handleAgentServerPush。
// 对齐 Python 中 AgentServer push 消费逻辑。
func (mh *MessageHandler) pushLoop(ctx context.Context) {
	logger.Info(logComponent).Msg("Push 消费循环已启动")
	defer logger.Info(logComponent).Msg("Push 消费循环已退出")

	// 获取 push 通道
	var pushCh <-chan map[string]any
	if ct, ok := mh.pushTransport.(interface{ PushCh() <-chan map[string]any }); ok {
		pushCh = ct.PushCh()
	}
	if pushCh == nil {
		logger.Warn(logComponent).Msg("PushTransport 未提供 PushCh，push 循环退出")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-pushCh:
			if !ok {
				return
			}
			if msg == nil {
				continue
			}
			mh.handleAgentServerPush(ctx, msg)
		}
	}
}

// handleAgentServerPush 处理 AgentServer 主动推送的消息
//
// 对齐 Python _handle_agent_server_push (L1610-L1672)：
//  1. 解析 wire → AgentResponseChunk
//  2. 判断是否 cron push → handleCronPushPayload
//  3. 判断是否终止 chunk → 跳过
//  4. chunkToMessage → robotMessages
func (mh *MessageHandler) handleAgentServerPush(_ context.Context, msg map[string]any) {
	if msg == nil {
		return
	}

	// 检查是否为 cron push
	if isCronPayload(msg) {
		mh.handleCronPushPayload(msg)
		return
	}

	// 解析为 E2AResponse
	e2aResp := e2a.ResponseFromMap(msg)
	if e2aResp == nil {
		logger.Warn(logComponent).
			Str("event_type", "push_parse_error").
			Msg("push 消息解析为 E2AResponse 失败")
		return
	}

	// E2AResponse → AgentResponseChunk
	chunk, err := e2a.E2AResponseToAgentChunk(e2aResp)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "push_chunk_parse_error").
			Err(err).
			Msg("push chunk 解析失败")
		return
	}

	// 跳过终止哨兵
	if IsTerminalStreamChunk(chunk) {
		return
	}

	// 获取 sessionID 和 metadata
	sessionID := e2aResp.SessionID
	var metadata map[string]any
	if e2aResp.Metadata != nil {
		metadata = e2aResp.Metadata
	}

	// AgentResponseChunk → Message → robotMessages
	outMsg := ChunkToMessage(chunk, sessionID, metadata)
	mh.enqueueOutbound(outMsg)
}

// handleCronPushPayload 处理 cron 推送消息
//
// 对齐 Python _handle_cron_push_payload (L1677-L1737)：
// 路由 cron action 到 CronController（当前 CronController 为 stub，直接返回空结果）。
func (mh *MessageHandler) handleCronPushPayload(msg map[string]any) {
	logger.Debug(logComponent).
		Str("event_type", "cron_push_received").
		Msg("cron push 消息已接收（当前为 stub）")
	// 后续对接 CronController
}

// isCronPayload 判断是否为 cron 推送消息
func isCronPayload(msg map[string]any) bool {
	if msg == nil {
		return false
	}
	// 检查 metadata 中的 cron 标识
	if meta, ok := msg["metadata"]; ok {
		if metaMap, isMap := meta.(map[string]any); isMap {
			if _, hasCron := metaMap["cron"]; hasCron {
				return true
			}
		}
	}
	// 检查 body 中的 cron 标识
	if body, ok := msg["body"]; ok {
		if bodyMap, isMap := body.(map[string]any); isMap {
			if _, hasCron := bodyMap["cron"]; hasCron {
				return true
			}
		}
	}
	return false
}

// ensurePushLoopInterfaces 确保接口实现
var _ = (*schema.Message)(nil) // 确保 schema import 可用
