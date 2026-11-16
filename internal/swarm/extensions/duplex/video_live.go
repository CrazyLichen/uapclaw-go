package duplex

import (
	"context"
	"errors"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex/tasks"
)

// ──────────────────────────── 常量 ────────────────────────────

// allowedImageMimeTypes 画面帧图片 MIME 白名单。
// 对齐 video_live.py:46-48 _ALLOWED_IMAGE_MIME_TYPES。
var allowedImageMimeTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true,
}

// conversationEventKinds 会话事件类别。
// 对齐 video_live.py:50-57 _CONVERSATION_EVENT_KINDS。
var conversationEventKinds = map[string]bool{
	"user": true, "assistant": true, "reasoning": true,
	"tool_call": true, "tool_result": true, "file": true,
}

// realtimeTelemetryEvent 遥测事件名白名单正则。
// 对齐 video_live.py:60-62 _REALTIME_TELEMETRY_EVENT。
var realtimeTelemetryEvent = regexp.MustCompile(`^(?:realtime|qwen|joyai|search_result|barge_in)_[a-z0-9_]{1,80}$`)

// realtimeTelemetryFields 遥测字段白名单。
// 对齐 video_live.py:63-69 _REALTIME_TELEMETRY_FIELDS（逐字段一比一）。
var realtimeTelemetryFields = map[string]bool{
	"event": true, "client_time": true, "level": true, "peak_level": true, "threshold": true,
	"noise_floor": true, "speech_ms": true, "assistant_playing": true, "response_active": true, "reason": true,
	"source": true, "frame_count": true, "model": true, "provider": true, "url": true, "code": true,
	"message": true, "client_build": true, "job_id": true, "search_session_id": true, "question": true,
	"query": true, "result": true, "realtime_answer": true, "turn_id": true, "attempt": true,
	"decision": true, "response_chars": true, "context_job_count": true, "context_chars": true,
	"name": true, "call_id": true, "text": true, "transcript": true, "has_transcript": true,
	"speech_epoch": true, "tts_generation": true, "resumed_text_chars": true, "text_chars": true,
	"transcript_chars": true, "user_speech_active": true, "response_generation": true,
	"current_generation": true, "stream_id": true, "streamed": true, "cooldown_ms": true,
	"rate_limit_strikes": true, "request_kind": true, "audio_append_sequence": true,
	"image_append_sequence": true, "has_deferred_image": true, "response_id": true,
	"audio_level": true, "speech_threshold": true, "cancel_event_sent": true,
	"replacing_pending_prompt": true, "instruction_chars": true, "frame_time_range": true,
	"audio_sequence": true, "image_sequence": true, "error_type": true, "raw_event": true,
	"speech_probability": true,
	"queued_count": true, "result_kind": true, "brief_chars": true,
	"latency_ms": true, "inference_ms": true, "wait_ms": true, "dropped_chunks": true,
}

// stagedTelemetryEvents 落 video-live-events.jsonl 的检索结果事件。
// 对齐 video_live.py:510-520 的 event_name 集合。
var stagedTelemetryEvents = map[string]bool{
	"realtime_answer_final": true,
	"search_result_received":            true,
	"search_result_duplicate_ignored":   true,
	"search_result_queued":              true,
	"search_result_queue_failed":        true,
	"search_result_dispatched":          true,
	"search_result_answered":            true,
	"search_result_response_interrupted": true,
	"search_result_response_empty":      true,
}

// logWriteLock JSONL 追加写锁（每行一个完整 JSON 对象）。
// 对齐 video_live.py:34 _LOG_WRITE_LOCK（偏差 D-4）。
var logWriteLock sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// AppendJSONL 追加一行 JSON（并发安全）。
// 对齐 video_live.py:71-78 _append_jsonl。
func AppendJSONL(path string, event map[string]any) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Error(logComponent).Err(err).Str("path", filepath.Dir(path)).Msg("创建日志目录失败")
		return
	}
	line, err := json.Marshal(event)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("遥测事件序列化失败")
		return
	}
	logWriteLock.Lock()
	defer logWriteLock.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("path", path).Msg("打开遥测日志失败")
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// VideoDuplexEnabled 插件启用判定。
// 对齐 video_live.py:180-186 video_duplex_enabled。
func VideoDuplexEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(envOr("VIDEO_DUPLEX_ENABLED", "")))
	if value == "" {
		value = "true"
	}
	return !(value == "0" || value == "false" || value == "no" || value == "off")
}

// RegisterVideoLiveHandler 注册实时问答 RPC 集并返回搜索任务管理器。
// 对齐 video_live.py:189-698 register_video_live_handler。
func registerVideoLiveHandler(channel DuplexChannel, opts registerOptions) *VideoSearchManager {
	// 对齐 :189-211：VideoSearchManager + voice_handlers 装配
	searchManager := NewVideoSearchManager(channel, opts.AgentClient, opts.NormalizeMediaAttachments,
		func(event map[string]any) { appendVideoEventLog(event) },
		func() bool { return videoLiveMode() == "realtime" },
		opts.TaskDBPath)
	voiceHandlers := CreateVoiceHandlers(channel,
		func() bool { return usesJoyaiVoiceChannel() },
		func(event map[string]any) { appendVideoEventLog(event) },
		func(event map[string]any) { appendASRLog(event) })

	// video.realtime.config（对齐 :213-248）
	channel.RegisterMethod("video.realtime.config", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		if videoLiveMode() == "joyai" {
			apiBase, _, model := JoyAIModelConfig()
			if apiBase == "" || model == "" {
				return &DuplexResponse{OK: false, Error: "请配置 JOYAI_API_BASE 和 JOYAI_MODEL_NAME", Code: "VIDEO_CONFIG_ERROR"}
			}
			return &DuplexResponse{OK: true, Payload: map[string]any{
				"provider": "joyai", "model": model, "reply_language": ReplyLanguage(),
			}}
		}
		config := QwenOmniRealtimeConfigFromEnvironment()
		if err := config.Validate(); err != nil {
			return &DuplexResponse{OK: false, Error: err.Error(), Code: "VIDEO_CONFIG_ERROR"}
		}
		return &DuplexResponse{OK: true, Payload: map[string]any{
			"provider": "qwen_omni", "url": QwenOmniProxyPath, "model": config.Model,
			"voice": config.Voice, "tools": QwenOmniTools(), "reply_language": ReplyLanguage(),
		}}
	}, true, true)

	// video.joyai.frame（对齐 :279-495）
	channel.RegisterMethod("video.joyai.frame", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		return joyaiFrameHandler(ctx, ws, reqID, params, sessionID, searchManager)
	}, true, true)

	// video.realtime.telemetry（对齐 :497-536）
	channel.RegisterMethod("video.realtime.telemetry", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		return realtimeTelemetryHandler(params)
	}, true, true)

	// video.conversation.append（对齐 :538-676）
	channel.RegisterMethod("video.conversation.append", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		return appendConversationHandler(ctx, ws, reqID, params, channel)
	}, true, true)

	// 语音四件套（video_voice handlers）
	for method, handler := range voiceHandlers {
		channel.RegisterMethod(method, handler, true, true)
	}

	// 任务管理五入口（search_manager handlers，对齐 :677-682）
	channel.RegisterMethod("video.qwen.tool", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		return searchManager.HandleQwenTool(ctx, ws, reqID, params, sessionID)
	}, true, true)
	channel.RegisterMethod("video.search.status", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		return searchManager.HandleStatus(ctx, ws, reqID, params, sessionID)
	}, true, true)
	channel.RegisterMethod("video.search.answer", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		return searchManager.HandleAnswer(ctx, ws, reqID, params, sessionID)
	}, true, true)
	channel.RegisterMethod("video.search.list", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		return searchManager.HandleList(ctx, ws, reqID, params, sessionID)
	}, true, true)
	channel.RegisterMethod("video.search.control", func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		return searchManager.HandleControl(ctx, ws, reqID, params, sessionID)
	}, true, true)

	// 注册完成事件（对齐 :684-690）
	appendVideoEventLog(map[string]any{
		"stage": "video_handlers_registered",
		"search_status_enabled": true,
	})
	return searchManager
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerOptions 注册注入项。
// Python 侧无对应实现：Python 由 ApplicationPluginServices 鸭子注入，Go 显式结构。
type registerOptions struct {
	AgentClient              AgentClient
	NormalizeMediaAttachments func(map[string]any, string) error
	TaskDBPath               string
}

// appendRealtimeTelemetry 实时打断遥测。
// 对齐 video_live.py:81-84 _append_realtime_telemetry。
func appendRealtimeTelemetry(event map[string]any) {
	AppendJSONL(homeLogsPath("realtime-interrupt.jsonl"), event)
}

// appendRealtimeSessionLog 实时会话遥测。
// 对齐 video_live.py:86-90。
func appendRealtimeSessionLog(event map[string]any) {
	AppendJSONL(homeLogsPath("realtime-session.jsonl"), withServerTime(event))
}

// appendQwenRealtimeErrorLog Qwen 实时错误遥测。
// 对齐 video_live.py:92-96。
func appendQwenRealtimeErrorLog(event map[string]any) {
	AppendJSONL(homeLogsPath("qwen-realtime-errors.jsonl"), withServerTime(event))
}

// appendVideoEventLog 视频事件遥测。
// 对齐 video_live.py:98-105。
func appendVideoEventLog(event map[string]any) {
	AppendJSONL(homeLogsPath("video-live-events.jsonl"), withServerTime(event))
}

// appendASRLog ASR 结果遥测。
// 对齐 video_live.py:107-114。
func appendASRLog(event map[string]any) {
	AppendJSONL(homeLogsPath("asr-results.jsonl"), withServerTime(event))
}

// appendJoyaiLog JoyAI 遥测（completed+非 silence 另落 raw-content）。
// 对齐 video_live.py:116-150 _append_joyai_log。
func appendJoyaiLog(event map[string]any) {
	record := copyMap(event)
	modelRawContent := record["model_raw_content"]
	delete(record, "model_raw_content")
	AppendJSONL(homeLogsPath("joyai-video.jsonl"), withServerTime(record))
	if toString(record["stage"]) != "completed" || toString(record["decision"]) == "silence" {
		return
	}
	rawContent := ""
	if s, ok := modelRawContent.(string); ok {
		rawContent = s
	} else {
		rawContent = toString(record["raw_content"])
	}
	if strings.TrimSpace(rawContent) == "" {
		return
	}
	rawRecord := map[string]any{
		"request_id":        record["request_id"],
		"joyai_session_id":  record["joyai_session_id"],
		"instruction":       record["instruction"],
		"request_kind":      record["request_kind"],
		"frame_time_range":  record["frame_time_range"],
		"decision":          record["decision"],
		"raw_content":       rawContent,
	}
	adapterContent := toString(record["raw_content"])
	if adapterContent != rawContent {
		rawRecord["adapter_content"] = adapterContent
	}
	AppendJSONL(homeLogsPath("joyai-raw-content.jsonl"), withServerTime(rawRecord))
}

// validateJoyaiResult 拒坏内部动作结果。
// 对齐 video_live.py:152-160 _validate_joyai_result。
func validateJoyaiResult(result map[string]any) error {
	if result == nil {
		return fmt.Errorf("JoyAI returned an invalid action result")
	}
	for _, key := range []string{"decision", "response", "delegation"} {
		if _, ok := result[key].(string); !ok {
			return fmt.Errorf("JoyAI returned an invalid action result")
		}
	}
	decision := toString(result["decision"])
	if decision != "silence" && decision != "response" && decision != "delegation" {
		return fmt.Errorf("JoyAI returned an invalid action result")
	}
	return nil
}

// isAllowedImageDataURL 图片帧白名单。
// 对齐 video_live.py:162-168。
func isAllowedImageDataURL(value string) bool {
	header, rest, _ := strings.Cut(value, ",")
	if rest == "" || !strings.HasPrefix(strings.ToLower(header), "data:") {
		return false
	}
	parts := strings.Split(strings.ToLower(header[5:]), ";")
	if !allowedImageMimeTypes[parts[0]] {
		return false
	}
	for _, param := range parts[1:] {
		if param == "base64" {
			return true
		}
	}
	return false
}

// videoLiveMode 实时模式。
// 对齐 video_live.py:170-173。
func videoLiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("VIDEO_LIVE_MODE")))
	if mode == "realtime" {
		return "realtime"
	}
	return "joyai"
}

// usesJoyaiVoiceChannel JoyAI 是否走原生语音 WS。
// 对齐 video_live.py:175-178。
func usesJoyaiVoiceChannel() bool {
	return JoyAIUsesNativeVoiceChannel(videoLiveMode())
}

// joyaiFrameHandler video.joyai.frame 处理。
// 对齐 video_live.py:279-495 _joyai_frame。
func joyaiFrameHandler(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string, searchManager *VideoSearchManager) *DuplexResponse {
	frameDataURL := strings.TrimSpace(toString(params["frame_data_url"]))
	instruction := strings.TrimSpace(toString(params["instruction"]))
	requestedSessionID := strings.TrimSpace(toString(params["joyai_session_id"]))
	searchSessionID := strings.TrimSpace(toString(params["search_session_id"]))
	question := strings.TrimSpace(toString(params["question"]))
	toolContext := strings.TrimSpace(toString(params["tool_context"]))
	frameTimeRange := strings.TrimSpace(toString(params["frame_time_range"]))
	replyLanguage := JoyAINormalizeResponseLanguage(orFallbackAny(params["reply_language"], ReplyLanguage()))
	requestKind := strings.ToLower(strings.TrimSpace(orChainStr(params["request_kind"], "frame")))
	// 参数校验逐条对照 :292-328
	if !isAllowedImageDataURL(frameDataURL) {
		return &DuplexResponse{OK: false, Error: "frame_data_url must be a base64 JPEG, PNG, or WebP data URL", Code: "BAD_REQUEST"}
	}
	if len(frameDataURL) > MaxFrameChars {
		return &DuplexResponse{OK: false, Error: "frame_data_url is too large", Code: "BAD_REQUEST"}
	}
	if requestKind != "user" && requestKind != "frame" {
		return &DuplexResponse{OK: false, Error: "request_kind is invalid", Code: "BAD_REQUEST"}
	}
	if len([]rune(instruction)) > MaxInstructionChars || (instruction == "" && requestKind != "frame") {
		return &DuplexResponse{OK: false, Error: fmt.Sprintf(
			"instruction may be empty only for frame-only requests; otherwise it must contain 1-%d characters",
			MaxInstructionChars), Code: "BAD_REQUEST"}
	}
	if len([]rune(toolContext)) > MaxToolContextChars {
		return &DuplexResponse{OK: false, Error: "tool_context must not exceed 4000 characters", Code: "BAD_REQUEST"}
	}
	if toolContext != "" && requestKind != "user" {
		return &DuplexResponse{OK: false, Error: "tool_context is allowed only for user requests", Code: "BAD_REQUEST"}
	}
	if len(requestedSessionID) > 200 {
		return &DuplexResponse{OK: false, Error: "joyai_session_id is too long", Code: "BAD_REQUEST"}
	}
	if len(frameTimeRange) > 100 || (frameTimeRange != "" &&
		!frameTimeRangePattern.MatchString(frameTimeRange)) {
		return &DuplexResponse{OK: false, Error: "frame_time_range is invalid", Code: "BAD_REQUEST"}
	}
	upstreamSessionID := requestedSessionID
	if upstreamSessionID == "" {
		upstreamSessionID = sessionID
	}
	if upstreamSessionID == "" {
		upstreamSessionID = "joyai-" + newRequestID()
	}
	if searchSessionID == "" {
		searchSessionID = upstreamSessionID
	}
	requestLog := map[string]any{
		"request_id": reqID, "joyai_session_id": upstreamSessionID,
		"instruction": instruction, "request_kind": requestKind,
		"frame_only": instruction == "", "frame_chars": len(frameDataURL),
		"frame_time_range": frameTimeRange, "tool_context_chars": len(toolContext),
	}
	appendJoyaiLog(withEntries(requestLog, map[string]any{"stage": "requested"}))

	var result map[string]any
	requestErr := func() (err error) {
		defer RecoverDuplexError(&err)
		modelInstruction := instruction
		if requestKind == "user" {
			modelInstruction = JoyAIGroundUserInstruction(instruction, toolContext, replyLanguage)
		}
		frameTime := frameTimeRange
		result = JoyAIRequestFrame(frameDataURL, modelInstruction, upstreamSessionID, frameTime)
		err = validateJoyaiResult(result)
		return err
	}()
	if requestErr != nil {
		errorText := requestErr.Error()
		if errorText == "" {
			errorText = "JoyAI frame request failed"
		}
		errorCode := "JOYAI_ERROR"
		var rateLimit *JoyAIRateLimitError
		if asErr(requestErr, &rateLimit) {
			errorCode = "JOYAI_RATE_LIMIT"
		}
		appendJoyaiLog(withEntries(requestLog, map[string]any{
			"stage": "failed", "error": errorText, "error_code": errorCode,
		}))
		return &DuplexResponse{OK: false, Error: errorText, Code: errorCode}
	}

	// JoyAI 以 delegation 标记而非原生函数调用；仅显式 schema 校验过的 JSON 是控制（对齐 :414-416 注释）
	scheduling := map[string]any(nil)
	operationRejected := false
	for operationIndex := 0; operationIndex < 3; operationIndex++ {
		delegation := strings.TrimSpace(toString(result["delegation"]))
		if toString(result["decision"]) != "delegation" || !strings.HasPrefix(delegation, "{") {
			break
		}
		var value map[string]any
		if json.Unmarshal([]byte(delegation), &value) != nil ||
			!sameKeySet(value, "name", "arguments") {
			break
		}
		name := toString(value["name"])
		if name != QwenOmniDelegateToolName && name != "jiuwen_task_query" &&
			name != "jiuwen_task_modify" && name != "jiuwen_task_cancel" {
			break
		}
		owner, scope, err := searchManager.Scope(ws, map[string]any{"search_session_id": searchSessionID})
		if err != nil {
			operationRejected = true
			return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
		}
		commandID := orChainStr(params["command_id"], reqID)
		call, err := ParseQwenOmniToolCall(map[string]any{
			"name": value["name"], "arguments": value["arguments"],
			"call_id": fmt.Sprintf("%s:%d", commandID, operationIndex),
		})
		if err != nil {
			operationRejected = true
			return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
		}
		if call.Name == QwenOmniDelegateToolName {
			for _, k := range []string{"independent", "depends_on", "resources"} {
				if v, ok := call.Arguments[k]; ok {
					scheduling = map[string]any{}
					scheduling[k] = v
				}
			}
			result = withEntries(result, map[string]any{"delegation": call.Task})
			break
		}
		receipt, err := searchManager.Operate(ctx, owner, scope, call)
		if err != nil {
			operationRejected = true
			return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
		}
		// 事实经同一 provider 会话回灌（含 query→modify 所需 ID）；回执不能制造任务
		followup := tasks.TaskReceiptFollowup(receipt)
		frameErr := func() (runErr error) {
			defer RecoverDuplexError(&runErr)
			result = JoyAIRequestFrame(frameDataURL, followup, upstreamSessionID, "")
			runErr = validateJoyaiResult(result)
			return runErr
		}()
		if frameErr != nil {
			operationRejected = true
			return &DuplexResponse{OK: false, Error: frameErr.Error(), Code: "TASK_REQUEST_REJECTED"}
		}
	}
	if operationRejected {
		return &DuplexResponse{OK: false, Error: "Task operation limit reached; query current state", Code: "TASK_REQUEST_REJECTED"}
	}

	searchJob := map[string]any(nil)
	toolsUsed := []string{}
	delegation := strings.TrimSpace(toString(result["delegation"]))
	if toString(result["decision"]) == "delegation" && delegation != "" {
		job, err := searchManager.Start(ctx, ws,
			orChainStr(question, delegation), delegation, searchSessionID,
			toString(result["response"]), frameDataURL,
			toString(params["command_id"]), "", "", "", scheduling)
		if err != nil {
			return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
		}
		searchJob = job
		toolsUsed = append(toolsUsed, QwenOmniDelegateToolName)
	}
	appendJoyaiLog(withEntries(requestLog, map[string]any{
		"stage": "completed", "decision": result["decision"],
		"response": result["response"], "delegation": result["delegation"],
		"raw_content": result["raw_content"],
		"model_raw_content": result["model_raw_content"],
		"latency_ms": result["latency_ms"], "timing": result["timing"],
		"tools_used": toolsUsed, "search_job": searchJob,
	}))
	return &DuplexResponse{OK: true, Payload: map[string]any{
		"response": result["response"], "search_job": searchJob,
	}}
}

// realtimeTelemetryHandler video.realtime.telemetry 处理。
// 对齐 video_live.py:497-536 _realtime_telemetry。
func realtimeTelemetryHandler(params map[string]any) *DuplexResponse {
	eventName := strings.TrimSpace(toString(params["event"]))
	if !realtimeTelemetryEvent.MatchString(eventName) {
		return &DuplexResponse{OK: false, Error: "unsupported telemetry event", Code: "BAD_REQUEST"}
	}
	allowed := map[string]any{}
	for key, value := range params {
		if !realtimeTelemetryFields[key] {
			continue
		}
		switch v := value.(type) {
		case string, int, int64, float64, bool:
			allowed[key] = v
		}
	}
	for _, key := range []string{"question", "query", "text", "transcript"} {
		if s, ok := allowed[key].(string); ok {
			allowed[key] = runePreview(s, 1000)
		}
	}
	for _, key := range []string{"result", "realtime_answer"} {
		if s, ok := allowed[key].(string); ok {
			allowed[key] = runePreview(s, 50000)
		}
	}
	if stagedTelemetryEvents[eventName] {
		allowed["stage"] = eventName
		appendVideoEventLog(allowed)
	} else if strings.HasPrefix(eventName, "realtime_") {
		appendRealtimeSessionLog(allowed)
	} else {
		allowed["server_time"] = time.Now().UTC().Format(time.RFC3339Nano)
		appendRealtimeTelemetry(allowed)
		if eventName == "qwen_realtime_error" {
			appendQwenRealtimeErrorLog(allowed)
		}
	}
	return &DuplexResponse{OK: true, Payload: map[string]any{"logged": true}}
}

// appendConversationHandler video.conversation.append 处理。
// 对齐 video_live.py:538-676 _append_conversation。
// 会话历史/元数据经历史桥接注入（O-7：Go 由 registerOptions 历史桥提供）。
func appendConversationHandler(ctx context.Context, ws any, reqID string, params map[string]any, channel DuplexChannel) *DuplexResponse {
	visibleSessionID := strings.TrimSpace(toString(params["session_id"]))
	eventID := strings.TrimSpace(toString(params["event_id"]))
	kind := strings.ToLower(strings.TrimSpace(toString(params["kind"])))
	if visibleSessionID == "" || visibleSessionID == "new" {
		return &DuplexResponse{OK: false, Code: "SESSION_NOT_READY",
			Error: "A persisted session is required before appending Full-duplex history."}
	}
	if eventID == "" || len(eventID) > 160 {
		return &DuplexResponse{OK: false, Code: "INVALID_EVENT_ID",
			Error: "event_id is required and must not exceed 160 characters."}
	}
	if !conversationEventKinds[kind] {
		return &DuplexResponse{OK: false, Code: "INVALID_EVENT_KIND",
			Error: "Unsupported Full-duplex conversation event kind."}
	}
	timestamp := parseTimestamp(params["timestamp"])
	if timestamp <= 0 {
		timestamp = float64(time.Now().UnixMicro()) / 1e6
	}
	content := toString(params["content"])
	if len(content) > 100000 {
		return &DuplexResponse{OK: false, Code: "CONTENT_TOO_LARGE",
			Error: "Full-duplex history content exceeds 100000 characters."}
	}
	role := "assistant"
	if kind == "user" {
		role = "user"
	}
	eventType := ""
	var extra map[string]any
	switch kind {
	case "assistant":
		eventType = "chat.final"
		if toString(params["presentation"]) == "tool_result" {
			extra = map[string]any{"presentation": "tool_result"}
		}
	case "reasoning":
		eventType = "chat.reasoning"
		extra = map[string]any{
			"reasoning_content": content,
			"reasoning_started_at": params["started_at"],
			"reasoning_updated_at": params["updated_at"],
		}
		content = ""
	case "tool_call":
		toolCall, ok := params["tool_call"].(map[string]any)
		if !ok {
			return &DuplexResponse{OK: false, Code: "INVALID_TOOL_CALL", Error: "tool_call must be an object."}
		}
		eventType = "chat.tool_call"
		extra = map[string]any{"tool_call": toolCall}
		content = ""
	case "tool_result":
		toolResult, ok := params["tool_result"].(map[string]any)
		if !ok {
			return &DuplexResponse{OK: false, Code: "INVALID_TOOL_RESULT", Error: "tool_result must be an object."}
		}
		eventType = "chat.tool_result"
		extra = map[string]any{"tool_result": toolResult}
		content = ""
	case "file":
		files := NormalizeFileItems(params["files"])
		if len(files) == 0 {
			return &DuplexResponse{OK: false, Code: "INVALID_FILES",
				Error: "files must contain named file resources."}
		}
		eventType = "chat.file"
		extra = map[string]any{"files": files}
		content = ""
	}
	previousTitle := ""
	if role == "user" && strings.TrimSpace(content) != "" {
		// 历史写入前刷新：尊重 AgentServer 的重命名（对齐 :645-647 注释）
		metadata := conversationHistory.GetSessionMetadata(visibleSessionID, true)
		previousTitle = toString(metadata["title"])
		if previousTitle == "Full-duplex conversation" {
			// 旧版本把占位标题持久化为真标题；恢复会话时复用九问自动起名策略
			conversationHistory.UpdateSessionMetadata(visibleSessionID, content)
		}
	}
	conversationHistory.AppendHistoryRecord(visibleSessionID,
		"video-duplex-"+eventID, "video_duplex", role, content, timestamp, eventType, extra, "agent")
	if previousTitle != "" {
		metadata := conversationHistory.GetSessionMetadata(visibleSessionID, false)
		title := toString(metadata["title"])
		if title != "" && title != previousTitle {
			// 原生监听器同时更新头部与工作台侧栏（对齐 :666 注释）
			_ = channel.SendEventChecked(ctx, ws, "session.updated", map[string]any{
				"session_id": visibleSessionID, "title": title, "display_title": title,
			})
		}
	}
	return &DuplexResponse{OK: true, Payload: map[string]any{"persisted": true, "event_id": eventID}}
}

// ConversationHistoryBridge 会话历史桥（Python append_history_record/get·update_session_metadata 对应物）。
// Python 侧无对应实现：Python 直接 import 会话模块，Go 由网关装配层注入桥接实现。
type ConversationHistoryBridge struct {
	AppendHistoryRecord  func(sessionID, requestID, channelID, role, content string, timestamp float64, eventType string, extra map[string]any, mode string)
	GetSessionMetadata   func(sessionID string, cacheBust bool) map[string]any
	UpdateSessionMetadata func(sessionID, userContent string)
}

// conversationHistory 历史桥注入点（装配层赋值；nil 时跳过持久化）。
var conversationHistory *ConversationHistoryBridge

// homeLogsPath 日志目录路径。
// 对齐 Path.home()/.jiuwenswarm/logs（Go 用 HOME 环境变量）。
func homeLogsPath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".jiuwenswarm", "logs", name)
}

// withServerTime 附加服务端时间。
// 对齐 datetime.now(timezone.utc).isoformat()。
func withServerTime(event map[string]any) map[string]any {
	return withEntries(map[string]any{"server_time": time.Now().UTC().Format(time.RFC3339Nano)}, event)
}

// parseTimestamp 宽松时间戳解析。
// 对齐 video_live.py:566-572 float(raw_timestamp) except → now。
func parseTimestamp(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &parsed); err == nil {
			return parsed
		}
	}
	return -1
}

// frameTimeRangePattern 帧时间范围正则。
// 对齐 video_live.py:309-314。
var frameTimeRangePattern = regexp.MustCompile(`^\d+(?:\.\d+)? seconds ~ \d+(?:\.\d+)? seconds$`)

// sameKeySet 键集合完全一致。
func sameKeySet(value map[string]any, keys ...string) bool {
	if len(value) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}

// asErr errors.As 包装。
// Python 侧无对应实现：isinstance 判断的 Go 等价。
func asErr(err error, target any) bool { return errors.As(err, target) }

// orFallbackAny 空值回退。
func orFallbackAny(primary, fallback any) any {
	if primary == nil {
		return fallback
	}
	return primary
}
