package message_handler

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleAgentServerPush 处理 AgentServer 主动推送的消息。
//
// 对齐 Python _handle_agent_server_push (L1610-L1672)：
//  1. 解析 wire → AgentResponseChunk
//  2. session_id 回退：优先 wire["session_id"]，否则 streamSessions[requestID]
//  3. metadata 合并：requestMetadata + 响应 metadata（过滤内部键）
//  4. ACP session_id 解析（预留 TODO）
//  5. cron 判断：chunk.Payload["event_type"] == "cron.response"
//  6. 跳过终止 chunk
//  7. evolution chunk 处理
//  8. 消息块转完整消息 → 发布机器人消息
func (mh *MessageHandler) handleAgentServerPush(wire map[string]any) {
	if wire == nil {
		return
	}

	// 解析为 E2AResponse
	e2aResp := e2a.ResponseFromMap(wire)
	if e2aResp == nil {
		logger.Warn(logComponent).
			Str("event_type", "push_parse_error").
			Msg("push 消息解析为 E2AResponse 失败")
		return
	}

	// E2A 响应转为 Agent 响应块
	chunk, err := e2a.E2AResponseToAgentChunk(e2aResp)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "push_chunk_parse_error").
			Err(err).
			Msg("push chunk 解析失败")
		return
	}

	// session_id 回退：优先 wire["session_id"]，否则 streamSessions[requestID]
	rid := chunk.RequestID
	sessionID := ""
	if sidRaw, ok := wire["session_id"]; ok {
		if s, isStr := sidRaw.(string); isStr && s != "" {
			sessionID = s
		}
	}
	if sessionID == "" {
		sessionID = mh.getStreamSessionID(rid)
	}

	// metadata 合并：请求 metadata 在前，响应 metadata 在后（响应优先）
	requestMetadata := mh.getStreamMetadata(rid)
	var respMetadata map[string]any
	if wmd, ok := wire["metadata"]; ok {
		if wmdMap, isMap := wmd.(map[string]any); isMap {
			// 过滤 E2A Wire 内部键
			respMetadata = make(map[string]any)
			for k, v := range wmdMap {
				if _, isInternal := e2a.E2AWireInternalMetadataKeys[k]; !isInternal {
					respMetadata[k] = v
				}
			}
		}
	}
	busMetadata := MergeAgentMetadata(requestMetadata, respMetadata)

	// TODO: ACP session_id 解析（等 ACP 章节回填）
	// 对齐 Python: ACP渠道检测
	//     session_id = self._resolve_acp_external_session_id(session_id, bus_metadata)

	// cron 判断：chunk.Payload["event_type"] == "cron.response"
	if chunk.Payload != nil {
		if eventType, ok := chunk.Payload["event_type"].(string); ok && eventType == "cron.response" {
			mh.handleCronPushPayload(chunk.Payload)
			return
		}
	}

	// 跳过终止哨兵
	if IsTerminalStreamChunk(chunk) {
		logger.Debug(logComponent).
			Str("event_type", "push_terminal_chunk").
			Str("request_id", rid).
			Msg("忽略 server_push 终止 chunk")
		return
	}

	// evolution chunk 处理
	mh.handleEvolutionChunk(chunk, sessionID, busMetadata)

	// Agent 响应块转为消息再推送到机器人消息列表
	outMsg := ChunkToMessage(chunk, sessionID, busMetadata)
	mh.PublishRobotMessages(outMsg)

	logger.Info(logComponent).
		Str("event_type", "server_push_processed").
		Str("request_id", rid).
		Str("channel_id", chunk.ChannelID).
		Msg("server_push 已写入 robot_messages")
}

// handleCronPushPayload 处理 cron 推送消息。
//
// 对齐 Python _handle_cron_push_payload (L1677-L1737)：
// 路由 cron action 到 CronController（当前 CronController 为 stub，直接返回空结果）。
// 依赖：11.10 Cron 调度服务实现后对接。
func (mh *MessageHandler) handleCronPushPayload(payload map[string]any) {
	logger.Debug(logComponent).
		Str("event_type", "cron_push_received").
		Msg("cron push 消息已接收（当前为 stub）")
	// 后续对接 CronController（11.10）
}
