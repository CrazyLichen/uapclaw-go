package e2a

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentFieldOption E2AFromAgentFields 的可选配置函数。
type AgentFieldOption func(map[string]any)

// ResponseNormOption 响应规范化可选配置函数。
type ResponseNormOption func(*responseNormConfig)

// responseNormConfig 响应规范化内部配置。
type responseNormConfig struct {
	timestamp string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// e2aInternalContextKey 内部上下文键前缀（与 E2A-AgentRequest-log-migration.md §7.4 一致）
	e2aInternalContextKey = "_jiuwenswarm"
	// e2aFallbackFailedKey 兜底失败标记键
	e2aFallbackFailedKey = "normalize_failed"
	// e2aLegacyAgentRequestKey 旧版 Agent 请求键
	e2aLegacyAgentRequestKey = "legacy_agent_request"
	// maxLegacyAgentRequestJSONBytes 旧版请求 JSON 最大字节数
	maxLegacyAgentRequestJSONBytes = 512000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件
var logComponent = logger.ComponentChannel

// ──────────────────────────── 导出函数 ────────────────────────────

// MessageToLegacyAgentDict 从 Message 生成与历史 WebSocket 一致的 dict（用于兜底 legacy_agent_request）。
// 对应 Python: message_to_legacy_agent_dict(msg)
func MessageToLegacyAgentDict(msg *schema.Message) map[string]any {
	rmVal := reqMethodToString(msg.ReqMethod)

	params := map[string]any{}
	if msg.Params != nil {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			// 反序列化失败时保留空 map
			params = map[string]any{}
		}
	}

	out := map[string]any{
		"request_id": msg.ID,
		"channel_id": msg.ChannelID,
		"session_id": msg.SessionID,
		"req_method": rmVal,
		"params":     params,
		"is_stream":  msg.IsStream,
		"timestamp":  msg.Timestamp,
	}
	if msg.Metadata != nil {
		meta := make(map[string]any, len(msg.Metadata))
		for k, v := range msg.Metadata {
			meta[k] = v
		}
		out["metadata"] = meta
	}
	return out
}

// BuildFallbackE2A 规范化失败时仍发 E2A 形状：在 channel_context 内携带 legacy 快照。
// 对应 Python: build_fallback_e2a(legacy)
func BuildFallbackE2A(legacy map[string]any) *E2AEnvelope {
	legacy = legacyPayloadWithinLimit(copyMap(legacy))

	rid, _ := legacy["request_id"].(string)

	internal := map[string]any{
		e2aFallbackFailedKey:     true,
		e2aLegacyAgentRequestKey: legacy,
	}
	cc := map[string]any{
		e2aInternalContextKey: internal,
	}

	chStr, _ := legacy["channel_id"].(string)
	sid := legacy["session_id"]
	isStream, _ := legacy["is_stream"].(bool)

	env := &E2AEnvelope{
		ProtocolVersion: E2AProtocolVersion,
		Channel:         chStr,
		SessionID:       fmt.Sprintf("%v", sid),
		Params:          map[string]any{},
		IsStream:        isStream,
		ChannelContext:  cc,
	}
	if rid != "" {
		env.RequestID = rid
	}
	return env
}

// MessageToE2A Message → E2AEnvelope（不经兜底）。
// 对应 Python: message_to_e2a(msg)
func MessageToE2A(msg *schema.Message) (*E2AEnvelope, error) {
	params := map[string]any{}
	if msg.Params != nil {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			logger.Warn(logComponent).Str("request_id", msg.ID).Err(err).Msg("params 反序列化失败")
			params = map[string]any{}
		}
	}

	d := map[string]any{
		"request_id": msg.ID,
		"channel_id": msg.ChannelID,
		"session_id": msg.SessionID,
		"chat_id":    msg.ChatID,
		"params":     params,
		"is_stream":  msg.IsStream,
		"timestamp":  msg.Timestamp,
	}

	if msg.ReqMethod != "" {
		d["method"] = string(msg.ReqMethod)
	}

	// 合并 metadata 和独立字段（enable_memory, group_digital_avatar 等）
	metadata := make(map[string]any)
	if msg.Metadata != nil {
		for k, v := range msg.Metadata {
			metadata[k] = v
		}
	}

	// enable_memory 逻辑：只有当 enable_memory=False 且 group_digital_avatar=True 且 is_group_chat=True 时才禁用记忆
	// is_group_chat 通过 metadata 中的 avatar_mode 判断
	isGroupChat := false
	if v, ok := metadata["avatar_mode"]; ok {
		isGroupChat = toBool(v)
	}
	shouldDisableMemory := !msg.EnableMemory && msg.GroupDigitalAvatar && isGroupChat
	// 默认启用记忆，只有在上述三个条件同时满足时才禁用
	finalEnableMemory := !shouldDisableMemory
	// Go 中 Message.EnableMemory 是 bool（零值 false），与 Python msg.enable_memory is False 行为一致
	// Python 中 msg.enable_memory is None 表示未设置，Go 中没有 None 概念，
	// 遵循用户要求：Go 中 EnableMemory 零值 false 与 Python 默认值 False 行为一致
	metadata["enable_memory"] = finalEnableMemory

	logger.Info(logComponent).
		Str("request_id", msg.ID).
		Bool("msg_enable_memory", msg.EnableMemory).
		Bool("msg_group_digital_avatar", msg.GroupDigitalAvatar).
		Bool("is_group_chat", isGroupChat).
		Bool("should_disable", shouldDisableMemory).
		Bool("final", finalEnableMemory).
		Msg("[E2A][enable_memory]")

	if msg.GroupDigitalAvatar {
		metadata["group_digital_avatar"] = msg.GroupDigitalAvatar
	}
	if len(metadata) > 0 {
		d["metadata"] = metadata
	}

	return EnvelopeFromMap(d), nil
}

// MessageToE2AOrFallback Message → E2A；失败或校验不通过则 BuildFallbackE2A。
// 对应 Python: message_to_e2a_or_fallback(msg)
func MessageToE2AOrFallback(msg *schema.Message) *E2AEnvelope {
	env, err := MessageToE2A(msg)
	if err != nil {
		legacy := MessageToLegacyAgentDict(msg)
		logger.Warn(logComponent).
			Str("request_id", msg.ID).
			Err(err).
			Msg("[E2A][fallback] normalize failed")
		return BuildFallbackE2A(legacy)
	}
	if env.RequestID == "" || strings.TrimSpace(env.RequestID) == "" {
		legacy := MessageToLegacyAgentDict(msg)
		logger.Warn(logComponent).
			Str("request_id", msg.ID).
			Str("reason", "empty request_id").
			Msg("[E2A][fallback] normalize failed")
		return BuildFallbackE2A(legacy)
	}

	paramsKeys := make([]string, 0, 32)
	for k := range env.Params {
		paramsKeys = append(paramsKeys, k)
		if len(paramsKeys) >= 32 {
			break
		}
	}
	logger.Info(logComponent).
		Str("request_id", env.RequestID).
		Str("channel", env.Channel).
		Str("method", env.Method).
		Bool("is_stream", env.IsStream).
		Strs("params_keys", paramsKeys).
		Msg("[E2A][norm]")

	return env
}

// WithFieldChannelID 设置 channel_id。
func WithFieldChannelID(v string) AgentFieldOption {
	return func(d map[string]any) { d["channel_id"] = v }
}

// WithFieldSessionID 设置 session_id。
func WithFieldSessionID(v string) AgentFieldOption {
	return func(d map[string]any) { d["session_id"] = v }
}

// WithFieldReqMethod 设置 req_method。
func WithFieldReqMethod(v string) AgentFieldOption {
	return func(d map[string]any) { d["method"] = v }
}

// WithFieldParams 设置 params。
func WithFieldParams(v map[string]any) AgentFieldOption {
	return func(d map[string]any) { d["params"] = v }
}

// WithFieldIsStream 设置 is_stream。
func WithFieldIsStream(v bool) AgentFieldOption {
	return func(d map[string]any) { d["is_stream"] = v }
}

// WithFieldTimestamp 设置 timestamp。
func WithFieldTimestamp(v float64) AgentFieldOption {
	return func(d map[string]any) { d["timestamp"] = v }
}

// WithFieldMetadata 设置 metadata。
func WithFieldMetadata(v map[string]any) AgentFieldOption {
	return func(d map[string]any) { d["metadata"] = v }
}

// E2AFromAgentFields 由与 AgentRequest 相同的字段构造 E2A（heartbeat / cron / app 管理请求等）。
// 对应 Python: e2a_from_agent_fields(...)
func E2AFromAgentFields(requestID string, opts ...AgentFieldOption) *E2AEnvelope {
	d := map[string]any{
		"request_id": requestID,
		"channel_id": "",
		"session_id": nil,
		"params":     map[string]any{},
		"is_stream":  false,
		"timestamp":  0.0,
	}
	for _, opt := range opts {
		opt(d)
	}
	// 确保 params 为 map[string]any
	if p, ok := d["params"]; ok {
		if m, ok := p.(map[string]any); ok {
			// 已是 map，无需处理
			_ = m
		}
	}
	return EnvelopeFromMap(d)
}

// ChannelContextForChannelReply 供流式 chunk 回传到 Channel：去掉内部 _jiuwenswarm，保留 trace 与业务 metadata。
// 对应 Python: channel_context_for_channel_reply(env)
func ChannelContextForChannelReply(env *E2AEnvelope) map[string]any {
	ctx := make(map[string]any)
	if env.ChannelContext != nil {
		for k, v := range env.ChannelContext {
			ctx[k] = v
		}
	}
	delete(ctx, e2aInternalContextKey)
	if len(ctx) == 0 {
		return nil
	}
	return ctx
}

// WithNormTimestamp 设置响应规范化时间戳。
func WithNormTimestamp(ts string) ResponseNormOption {
	return func(c *responseNormConfig) { c.timestamp = ts }
}

// E2AResponseFromAgentResponse 将 AgentResponse 规范为 E2AResponse。
//
// 非流式完整响应恒为 is_final=true、sequence 默认 0。
//
// 对应 Python: e2a_response_from_agent_response(resp, ...)
func E2AResponseFromAgentResponse(resp *schema.AgentResponse, responseID string, sequence int, opts ...ResponseNormOption) *E2AResponse {
	cfg := &responseNormConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	ts := cfg.timestamp
	if ts == "" {
		ts = UTCNowISO()
	}

	prov := E2AProvenance{
		SourceProtocol: E2ASourceProtocolE2A,
		Converter:      "e2a.gateway_normalize:E2AResponseFromAgentResponse",
		ConvertedAt:    ts,
		Details:        map[string]any{"kind": "legacy_agent_response", "ok": resp.OK},
	}

	meta := map[string]any{}
	if resp.Metadata != nil {
		for k, v := range resp.Metadata {
			meta[k] = v
		}
	}

	var body map[string]any
	var status, kind string

	if resp.OK {
		payload := map[string]any{}
		if resp.Payload != nil {
			payload = resp.Payload
		}
		body = map[string]any{"result": payload}
		status = E2AResponseStatusSucceeded
		kind = E2AResponseKindE2AComplete
	} else {
		payload := map[string]any{}
		if resp.Payload != nil {
			payload = resp.Payload
		}
		errMsg := "Agent error"
		if v, ok := payload["error"]; ok {
			errMsg = fmt.Sprintf("%v", v)
		} else if v, ok := payload["message"]; ok {
			errMsg = fmt.Sprintf("%v", v)
		}
		body = map[string]any{
			"code":    "E2A.AGENT_ERROR",
			"message": errMsg,
			"details": payload,
		}
		status = E2AResponseStatusFailed
		kind = E2AResponseKindE2AError
	}

	ch := resp.ChannelID

	return &E2AResponse{
		ProtocolVersion: E2AProtocolVersion,
		ResponseID:      responseID,
		RequestID:       resp.RequestID,
		Sequence:        sequence,
		IsFinal:         true,
		Status:          status,
		ResponseKind:    kind,
		Timestamp:       ts,
		Provenance:      prov,
		Body:            body,
		Channel:         ch,
		Metadata:        meta,
		IdentityOrigin:  IdentityOriginAgent,
		IsStream:        false,
	}
}

// E2AResponseFromAgentChunk 将 AgentResponseChunk 规范为 E2AResponse。
//
// 中间帧：e2a.chunk，status=in_progress，is_final=false。
// 终止帧：payload == {"is_complete":true} → e2a.complete。
// chat.delta：按 source_chunk_type 映射 body.delta_kind（llm_reasoning → reasoning）。
//
// 对应 Python: e2a_response_from_agent_chunk(chunk, ...)
func E2AResponseFromAgentChunk(chunk *schema.AgentResponseChunk, responseID string, sequence int, isStream bool, opts ...ResponseNormOption) *E2AResponse {
	cfg := &responseNormConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	ts := cfg.timestamp
	if ts == "" {
		ts = UTCNowISO()
	}

	prov := E2AProvenance{
		SourceProtocol: E2ASourceProtocolE2A,
		Converter:      "e2a.gateway_normalize:E2AResponseFromAgentChunk",
		ConvertedAt:    ts,
		Details:        map[string]any{"kind": "legacy_agent_response_chunk", "is_complete": chunk.IsComplete},
	}

	pl := map[string]any{}
	if chunk.Payload != nil {
		pl = chunk.Payload
	}

	// 终止帧: is_complete=true 且 payload=={"is_complete":true}
	if chunk.IsComplete && isTerminalPayload(pl) {
		return &E2AResponse{
			ProtocolVersion: E2AProtocolVersion,
			ResponseID:      responseID,
			RequestID:       chunk.RequestID,
			Sequence:        sequence,
			IsFinal:         true,
			Status:          E2AResponseStatusSucceeded,
			ResponseKind:    E2AResponseKindE2AComplete,
			Timestamp:       ts,
			Provenance:      prov,
			Body:            map[string]any{"result": map[string]any{}},
			Channel:         chunk.ChannelID,
			IdentityOrigin:  IdentityOriginAgent,
			IsStream:        isStream,
		}
	}

	// 错误帧: is_complete=true 且 event_type=="chat.error"
	if chunk.IsComplete {
		et, _ := pl["event_type"].(string)
		if et == "chat.error" {
			return &E2AResponse{
				ProtocolVersion: E2AProtocolVersion,
				ResponseID:      responseID,
				RequestID:       chunk.RequestID,
				Sequence:        sequence,
				IsFinal:         true,
				Status:          E2AResponseStatusFailed,
				ResponseKind:    E2AResponseKindE2AError,
				Timestamp:       ts,
				Provenance:      prov,
				Body: map[string]any{
					"code":    "chat.error",
					"message": fmt.Sprintf("%v", pl["error"]),
					"details": pl,
				},
				Channel:        chunk.ChannelID,
				IdentityOrigin: IdentityOriginAgent,
				IsStream:       isStream,
			}
		}
	}

	// 业务结束帧: is_complete=true 且非上述
	if chunk.IsComplete {
		return &E2AResponse{
			ProtocolVersion: E2AProtocolVersion,
			ResponseID:      responseID,
			RequestID:       chunk.RequestID,
			Sequence:        sequence,
			IsFinal:         true,
			Status:          E2AResponseStatusSucceeded,
			ResponseKind:    E2AResponseKindE2AComplete,
			Timestamp:       ts,
			Provenance:      prov,
			Body:            map[string]any{"result": pl},
			Channel:         chunk.ChannelID,
			IdentityOrigin:  IdentityOriginAgent,
			IsStream:        isStream,
		}
	}

	// 中间帧: is_complete=false
	eventType, _ := pl["event_type"].(string)
	var bodyChunk map[string]any

	if eventType == "chat.delta" {
		sct := pl["source_chunk_type"]
		deltaKind := "text"
		if fmt.Sprintf("%v", sct) == "llm_reasoning" {
			deltaKind = "reasoning"
		}
		bodyChunk = map[string]any{
			"delta_kind":        deltaKind,
			"delta":             pl["content"],
			"event_type":        eventType,
			"source_chunk_type": sct,
		}
		// 保留 team-member 归属字段供前端显示
		for _, key := range []string{"role", "member_name"} {
			if v, ok := pl[key]; ok {
				bodyChunk[key] = v
			}
		}
	} else {
		bodyChunk = map[string]any{
			"delta_kind": "custom",
			"delta":      pl,
			"event_type": eventType,
		}
	}

	return &E2AResponse{
		ProtocolVersion: E2AProtocolVersion,
		ResponseID:      responseID,
		RequestID:       chunk.RequestID,
		Sequence:        sequence,
		IsFinal:         false,
		Status:          E2AResponseStatusInProgress,
		ResponseKind:    E2AResponseKindE2AChunk,
		Timestamp:       ts,
		Provenance:      prov,
		Body:            bodyChunk,
		Channel:         chunk.ChannelID,
		IdentityOrigin:  IdentityOriginAgent,
		IsStream:        isStream,
	}
}

// E2AResponseToAgentResponse E2AResponse → 非流式 AgentResponse。
//
// 仅处理网关 unary 常见 response_kind：e2a.complete、e2a.error；其它 kind 返回 error。
//
// 对应 Python: e2a_response_to_agent_response(e2a)
func E2AResponseToAgentResponse(e2a *E2AResponse) (*schema.AgentResponse, error) {
	rid := e2a.RequestID
	ch := e2a.Channel

	var meta map[string]any
	if e2a.Metadata != nil {
		meta = make(map[string]any, len(e2a.Metadata))
		for k, v := range e2a.Metadata {
			meta[k] = v
		}
	}

	kind := e2a.ResponseKind
	body := make(map[string]any)
	if e2a.Body != nil {
		for k, v := range e2a.Body {
			body[k] = v
		}
	}

	if kind == E2AResponseKindE2AComplete && e2a.Status == E2AResponseStatusSucceeded {
		res := body["result"]
		pl := map[string]any{}
		if m, ok := res.(map[string]any); ok {
			pl = m
		}
		return &schema.AgentResponse{
			RequestID: rid,
			ChannelID: ch,
			OK:        true,
			Payload:   pl,
			Metadata:  meta,
		}, nil
	}

	if kind == E2AResponseKindE2AError || e2a.Status == E2AResponseStatusFailed {
		details := body["details"]
		var pl map[string]any
		if m, ok := details.(map[string]any); ok {
			pl = m
		} else {
			errMsg := "Agent error"
			if v, ok := body["message"]; ok {
				errMsg = fmt.Sprintf("%v", v)
			}
			pl = map[string]any{
				"error": errMsg,
				"code":  body["code"],
			}
		}
		return &schema.AgentResponse{
			RequestID: rid,
			ChannelID: ch,
			OK:        false,
			Payload:   pl,
			Metadata:  meta,
		}, nil
	}

	return nil, fmt.Errorf(
		"e2a_response_to_agent_response: unsupported response_kind=%q status=%q",
		kind, e2a.Status,
	)
}

// E2AResponseToAgentChunk E2AResponse → AgentResponseChunk。
//
// 覆盖：流式 e2a.chunk、终止 e2a.complete / e2a.error、cron、acp.output_request。
//
// 对应 Python: e2a_response_to_agent_chunk(e2a)
func E2AResponseToAgentChunk(e2a *E2AResponse) (*schema.AgentResponseChunk, error) {
	rid := e2a.RequestID
	ch := e2a.Channel
	kind := e2a.ResponseKind

	body := make(map[string]any)
	if e2a.Body != nil {
		for k, v := range e2a.Body {
			body[k] = v
		}
	}

	// e2a.complete + is_final 条件分支
	if kind == E2AResponseKindE2AComplete && e2a.IsFinal {
		res := body["result"]
		if emptyCompleteMarker(body, res) {
			return &schema.AgentResponseChunk{
				RequestID:  rid,
				ChannelID:  ch,
				Payload:    map[string]any{"is_complete": true},
				IsComplete: true,
			}, nil
		}
		pl := map[string]any{}
		if m, ok := res.(map[string]any); ok {
			pl = m
		}
		return &schema.AgentResponseChunk{
			RequestID:  rid,
			ChannelID:  ch,
			Payload:    pl,
			IsComplete: true,
		}, nil
	}

	// e2a.error + is_final 条件分支
	if kind == E2AResponseKindE2AError && e2a.IsFinal {
		det := body["details"]
		var pl map[string]any
		if m, ok := det.(map[string]any); ok {
			pl = m
		} else {
			pl = make(map[string]any, len(body))
			for k, v := range body {
				pl[k] = v
			}
		}
		return &schema.AgentResponseChunk{
			RequestID:  rid,
			ChannelID:  ch,
			Payload:    pl,
			IsComplete: true,
		}, nil
	}

	// e2a.chunk + !is_final 条件分支
	if kind == E2AResponseKindE2AChunk && !e2a.IsFinal {
		dk, _ := body["delta_kind"].(string)
		et, _ := body["event_type"].(string)
		delta := body["delta"]
		sctIn := body["source_chunk_type"]

		if et == "chat.delta" || dk == "text" || dk == "reasoning" {
			sct := sctIn
			if dk == "reasoning" {
				sct = "llm_reasoning"
			}
			pl := map[string]any{
				"event_type": "chat.delta",
				"content":    delta,
			}
			if sct != nil {
				pl["source_chunk_type"] = sct
			}
			// 保留 team-member 归属字段供前端显示
			for _, key := range []string{"role", "member_name"} {
				if v, ok := body[key]; ok {
					pl[key] = v
				}
			}
			return &schema.AgentResponseChunk{
				RequestID:  rid,
				ChannelID:  ch,
				Payload:    pl,
				IsComplete: false,
			}, nil
		}

		// custom 或其它 delta_kind
		var pl2 map[string]any
		if m, ok := delta.(map[string]any); ok {
			pl2 = m
		} else {
			pl2 = map[string]any{}
			if et != "" {
				pl2["event_type"] = et
			}
			if delta != nil {
				pl2["content"] = delta
			}
		}
		if et != "" {
			if _, has := pl2["event_type"]; !has {
				pl2["event_type"] = et
			}
		}
		return &schema.AgentResponseChunk{
			RequestID:  rid,
			ChannelID:  ch,
			Payload:    pl2,
			IsComplete: false,
		}, nil
	}

	// 定时任务
	if kind == E2AResponseKindCron {
		bodyPayload := map[string]any{
			"event_type": "cron.response",
			"action":     body["action"],
			"status":     body["status"],
			"data":       body["data"],
			"message":    body["message"],
		}
		return &schema.AgentResponseChunk{
			RequestID:  rid,
			ChannelID:  ch,
			Payload:    bodyPayload,
			IsComplete: true,
		}, nil
	}

	// acp.output_request 请求
	if kind == E2AResponseKindACPOutputRequest {
		jsonrpc := map[string]any{}
		if e2a.Body != nil {
			for k, v := range e2a.Body {
				jsonrpc[k] = v
			}
		}
		return &schema.AgentResponseChunk{
			RequestID:  rid,
			ChannelID:  ch,
			Payload:    map[string]any{"event_type": "acp.output_request", "jsonrpc": jsonrpc},
			IsComplete: false,
		}, nil
	}

	return nil, fmt.Errorf(
		"e2a_response_to_agent_chunk: unsupported response_kind=%q is_final=%v",
		kind, e2a.IsFinal,
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reqMethodToString 将 ReqMethod 转为字符串，空值返回 nil。
func reqMethodToString(rm schema.ReqMethod) any {
	if rm == "" {
		return nil
	}
	return string(rm)
}

// legacyPayloadWithinLimit legacy JSON 超过 512KB 时裁剪 params。
// 对应 Python: _legacy_payload_within_limit(legacy)
func legacyPayloadWithinLimit(legacy map[string]any) map[string]any {
	raw, err := json.Marshal(legacy)
	if err != nil {
		return map[string]any{
			"request_id": fmt.Sprintf("%v", legacy["request_id"]),
			"channel_id": fmt.Sprintf("%v", legacy["channel_id"]),
			"session_id": legacy["session_id"],
			"req_method": nil,
			"params":     map[string]any{"_e2a_fallback_error": "legacy not json-serializable"},
			"is_stream":  false,
			"timestamp":  0.0,
		}
	}
	if len(raw) <= maxLegacyAgentRequestJSONBytes {
		return legacy
	}
	logger.Error(logComponent).
		Int("max_bytes", maxLegacyAgentRequestJSONBytes).
		Msg("[E2A][fallback] legacy_agent_request exceeds bytes, stripping params")
	slim := make(map[string]any, len(legacy))
	for k, v := range legacy {
		slim[k] = v
	}
	slim["params"] = map[string]any{"_e2a_fallback_error": "legacy payload too large"}
	return slim
}

// isTerminalPayload 判断 payload 是否为终止哨兵 {"is_complete": true}。
func isTerminalPayload(pl map[string]any) bool {
	if len(pl) != 1 {
		return false
	}
	v, ok := pl["is_complete"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// emptyCompleteMarker 判断 e2a.complete body 是否表示空终止帧。
// 对应 Python: _empty_complete_marker(b, r)
func emptyCompleteMarker(body map[string]any, res any) bool {
	// body 等于 {"result": {}} 判断
	if len(body) == 1 {
		if v, ok := body["result"]; ok {
			if m, ok := v.(map[string]any); ok && len(m) == 0 {
				return true
			}
		}
	}
	// result 不是 dict 或非空 dict → 非终止帧
	if m, ok := res.(map[string]any); ok {
		if len(m) > 0 {
			return false
		}
	} else {
		return false
	}
	// result 是空 dict，检查 body 键列表是否仅有 "result"
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	return len(keys) == 1 && keys[0] == "result"
}

// copyMap 深拷贝 map[string]any（仅一层）。
func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// toBool 将任意值转为 bool（对齐 Python bool() 语义）。
func toBool(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != ""
	case int:
		return val != 0
	case float64:
		return val != 0
	default:
		return true
	}
}
