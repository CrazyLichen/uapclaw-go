package message_handler

import (
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleAgentServerPush 处理 AgentServer 主动推送的消息。
//
// Python: _handle_agent_server_push (L1610-L1672)：
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
			Msg("failed to parse push message as E2AResponse")
		return
	}

	// E2A 响应转为 Agent 响应块
	chunk, err := e2a.E2AResponseToAgentChunk(e2aResp)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "push_chunk_parse_error").
			Err(err).
			Msg("failed to parse push chunk")
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

	// ACP 会话别名出站解析（⑧ 回填，Python L1643-1644：chunk 渠道判定 + 三级回落）
	if chunk.ChannelID == acpChannelIDConst {
		sessionID = mh.resolveAcpExternalSessionID(sessionID, busMetadata)
	}

	// cron 判断：chunk.Payload["event_type"] == "cron.response"
	if chunk.Payload != nil {
		if eventType, ok := chunk.Payload["event_type"].(string); ok && eventType == "cron.response" {
			mh.handleCronPushPayload(chunk.Payload, rid, chunk.ChannelID, sessionID, busMetadata)
			return
		}
	}

	// 跳过终止哨兵
	if IsTerminalStreamChunk(chunk) {
		logger.Debug(logComponent).
			Str("event_type", "push_terminal_chunk").
			Str("request_id", rid).
			Msg("ignoring server_push terminal chunk")
		return
	}

	// 演化块处理
	mh.handleEvolutionChunk(chunk, sessionID, busMetadata)

	// Agent 响应块转为消息再推送到机器人消息列表
	outMsg := ChunkToMessage(chunk, sessionID, busMetadata)
	mh.PublishRobotMessages(outMsg)

	logger.Info(logComponent).
		Str("event_type", "server_push_processed").
		Str("request_id", rid).
		Str("channel_id", chunk.ChannelID).
		Msg("server_push written to robot_messages")
}

// handleCronPushPayload 处理 cron 推送消息。
//
// Python: _handle_cron_push_payload (L1677-L1737)：
// action 路由到 CronController，结果以 chat.tool_result event_type 回发。
// create 时从 _stream_modes 取 mode 覆盖 LLM 工具调用的默认值。
func (mh *MessageHandler) handleCronPushPayload(payload map[string]any, requestID, channelID, sessionID string, metadata map[string]any) {
	if payload == nil {
		return
	}
	cc := mh.cronController
	if cc == nil {
		return
	}
	action := strings.TrimSpace(strVal(payload["action"]))
	params, _ := payload["data"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}

	var data any
	var err error
	switch action {
	case "list":
		data, err = cc.ListJobs()
	case "get":
		data, err = cc.GetJob(strings.TrimSpace(strVal(params["job_id"])))
	case "create":
		// 从原始请求中获取 mode，覆盖 LLM 工具调用的默认值
		// Python: request_mode = self._stream_modes.get(request_id)
		if mode := mh.getStreamMode(requestID); mode != "" {
			params["mode"] = mode
		}
		data, err = cc.CreateJob(params)
	case "update":
		patch, _ := params["patch"].(map[string]any)
		if patch == nil {
			// 对齐 Python: dict(params.get("patch") or {})
			patch = map[string]any{}
		}
		data, err = cc.UpdateJob(strings.TrimSpace(strVal(params["job_id"])), patch)
	case "delete":
		var deleted bool
		deleted, err = cc.DeleteJob(strings.TrimSpace(strVal(params["job_id"])))
		data = map[string]any{"deleted": deleted}
	case "toggle":
		data, err = cc.ToggleJob(strings.TrimSpace(strVal(params["job_id"])), toBool(params["enabled"]))
	case "preview":
		count := 5
		if v, ok := params["count"].(float64); ok {
			count = int(v)
		}
		data, err = cc.PreviewJob(strings.TrimSpace(strVal(params["job_id"])), count)
	case "run_now":
		var runID string
		runID, err = cc.RunNow(strings.TrimSpace(strVal(params["job_id"])))
		data = map[string]any{"run_id": runID}
	default:
		data = map[string]any{"error": fmt.Sprintf("unknown cron action: %s", action)}
	}
	if err != nil {
		data = map[string]any{"error": err.Error()}
	}

	out := schema.NewEventMessage(channelID, sessionID, schema.EventTypeChatToolResult,
		map[string]any{
			"event_type": "chat.tool_result",
			"tool_name":  "cron",
			"result":     data,
		},
		schema.WithMetadata(metadata),
	)
	out.ID = requestID
	out.Timestamp = float64(time.Now().UnixNano()) / 1e9
	// enable_streaming=false：工具结果不开启流式，避免被发送到群聊
	// （NewEventMessage 默认 true，显式关闭对齐 Python enable_streaming=False）
	out.EnableStreaming = false
	mh.PublishRobotMessages(out)
}

// strVal 安全取字符串（nil/非字符串返回空串）。
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
