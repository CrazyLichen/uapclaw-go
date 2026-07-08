package e2a

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IsE2AResponseWireDict 判别 JSON 对象是否为 E2A 响应线格式。
// 须含非空 response_kind 且 protocol_version=="1.0" 且 type!="event"。
// 对应 Python: is_e2a_response_wire_dict(data)
func IsE2AResponseWireDict(data map[string]any) bool {
	if data == nil {
		return false
	}
	if v, ok := data["type"]; ok {
		if s, ok := v.(string); ok && s == "event" {
			return false
		}
	}
	if v, ok := data["protocol_version"]; ok {
		if s, ok := v.(string); ok && s != E2AProtocolVersion {
			return false
		}
	} else {
		return false
	}
	rk, ok := data["response_kind"]
	if !ok || rk == nil {
		return false
	}
	s, ok := rk.(string)
	return ok && s != ""
}

// ParseAgentServerWireUnary 将一条非流式 WebSocket JSON 解析为 AgentResponse。
// 对应 Python: parse_agent_server_wire_unary(data)（精简版，无 deprecated 形状判别）
func ParseAgentServerWireUnary(data map[string]any) (*schema.AgentResponse, error) {
	rid := getString(data, "request_id")

	if IsE2AResponseWireDict(data) {
		e2a := ResponseFromMap(data)

		meta := make(map[string]any)
		if e2a.Metadata != nil {
			for k, v := range e2a.Metadata {
				meta[k] = v
			}
		}
		legacy, hasLegacy := meta[E2AWireLegacyAgentResponseKey]
		if hasLegacy {
			if legacyMap, ok := legacy.(map[string]any); ok {
				logger.Warn(logComponent).
					Str("request_id", rid).
					Str("response_id", e2a.ResponseID).
					Str("legacy_key", E2AWireLegacyAgentResponseKey).
					Msg("入站解码走 legacy 兜底")
				return rawDictToAgentResponse(legacyMap), nil
			}
		}

		out, err := E2AResponseToAgentResponse(e2a)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("request_id", rid).
				Str("stage", "inverse").
				Msg("入站解码失败")

			legacyInv, hasInv := meta[E2AWireLegacyAgentResponseKey]
			if hasInv {
				if legacyMap, ok := legacyInv.(map[string]any); ok {
					logger.Warn(logComponent).
						Str("request_id", rid).
						Str("response_id", e2a.ResponseID).
						Str("legacy_key", E2AWireLegacyAgentResponseKey).
						Msg("入站解码走 legacy 兜底")
					return rawDictToAgentResponse(legacyMap), nil
				}
			}
			return nil, fmt.Errorf("parse_agent_server_wire_unary: %w", err)
		}

		logger.Debug(logComponent).
			Str("request_id", rid).
			Str("response_kind", e2a.ResponseKind).
			Msg("入站解码成功")

		return out, nil
	}

	return nil, fmt.Errorf("parse_agent_server_wire_unary: 无法识别的线格式")
}

// ParseAgentServerWireChunk 将一条流式 WebSocket JSON 解析为 AgentResponseChunk。
// 对应 Python: parse_agent_server_wire_chunk(data)（精简版，无 deprecated 形状判别）
func ParseAgentServerWireChunk(data map[string]any) (*schema.AgentResponseChunk, error) {
	rid := getString(data, "request_id")

	if IsE2AResponseWireDict(data) {
		e2a := ResponseFromMap(data)

		meta := make(map[string]any)
		if e2a.Metadata != nil {
			for k, v := range e2a.Metadata {
				meta[k] = v
			}
		}
		legacy, hasLegacy := meta[E2AWireLegacyAgentChunkKey]
		if hasLegacy {
			if legacyMap, ok := legacy.(map[string]any); ok {
				logger.Warn(logComponent).
					Str("request_id", rid).
					Str("response_id", e2a.ResponseID).
					Str("legacy_key", E2AWireLegacyAgentChunkKey).
					Msg("入站解码走 legacy 兜底")
				return rawDictToAgentChunk(legacyMap), nil
			}
		}

		out, err := E2AResponseToAgentChunk(e2a)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("request_id", rid).
				Str("stage", "inverse").
				Msg("入站解码失败")

			legacyInv, hasInv := meta[E2AWireLegacyAgentChunkKey]
			if hasInv {
				if legacyMap, ok := legacyInv.(map[string]any); ok {
					logger.Warn(logComponent).
						Str("request_id", rid).
						Str("response_id", e2a.ResponseID).
						Str("legacy_key", E2AWireLegacyAgentChunkKey).
						Msg("入站解码走 legacy 兜底")
					return rawDictToAgentChunk(legacyMap), nil
				}
			}
			return nil, fmt.Errorf("parse_agent_server_wire_chunk: %w", err)
		}

		logger.Debug(logComponent).
			Str("request_id", rid).
			Str("response_kind", e2a.ResponseKind).
			Msg("入站解码成功")

		return out, nil
	}

	return nil, fmt.Errorf("parse_agent_server_wire_chunk: 无法识别的线格式")
}

// EncodeAgentResponseForWire 将 AgentResponse 编码为 E2A 线 dict。
// 失败时 metadata 塞入整包 legacy 并记日志。
// 对应 Python: encode_agent_response_for_wire(resp, ...)
func EncodeAgentResponseForWire(resp *schema.AgentResponse, responseID string, sequence int) map[string]any {
	rid := resp.RequestID
	e2a := E2AResponseFromAgentResponse(resp, responseID, sequence)

	wire := e2a.ToMap()
	if len(wire) == 0 {
		exc := fmt.Errorf("ToMap 返回空")
		logger.Error(logComponent).
			Err(exc).
			Str("request_id", rid).
			Str("stage", "to_dict").
			Bool("legacy_stashed", true).
			Msg("出站编码失败")
		return fallbackWireUnaryFromLegacy(agentResponseToMap(resp), responseID, sequence, exc)
	}

	logger.Info(logComponent).
		Str("request_id", rid).
		Str("response_id", responseID).
		Str("response_kind", e2a.ResponseKind).
		Msg("出站编码成功")

	return wire
}

// EncodeAgentChunkForWire 将 AgentResponseChunk 编码为 E2A 线 dict。
// 失败时 metadata 塞入整包 legacy。
// 对应 Python: encode_agent_chunk_for_wire(chunk, ...)
func EncodeAgentChunkForWire(chunk *schema.AgentResponseChunk, responseID string, sequence int, isStream bool) map[string]any {
	rid := chunk.RequestID
	e2a := E2AResponseFromAgentChunk(chunk, responseID, sequence, isStream)

	wire := e2a.ToMap()
	if len(wire) == 0 {
		exc := fmt.Errorf("ToMap 返回空")
		logger.Error(logComponent).
			Err(exc).
			Str("request_id", rid).
			Str("stage", "to_dict").
			Bool("legacy_stashed", true).
			Msg("出站编码失败")
		return fallbackWireChunkFromLegacy(agentChunkToMap(chunk), responseID, sequence, exc, isStream)
	}

	return wire
}

// EncodeJSONParseErrorWire 入站 JSON 无法解析时发送的单帧 E2A 形错误（无 legacy blob）。
// 对应 Python: encode_json_parse_error_wire(...)
func EncodeJSONParseErrorWire(requestID, channelID, message string, responseID ...string) map[string]any {
	ts := UTCNowISO()
	ridOut := ""
	if len(responseID) > 0 && responseID[0] != "" {
		ridOut = responseID[0]
	}
	if ridOut == "" {
		if requestID != "" {
			ridOut = requestID
		} else {
			ridOut = "invalid-json"
		}
	}

	var reqID *string
	if requestID != "" {
		reqID = &requestID
	}

	var ch *string
	if channelID != "" {
		ch = &channelID
	}

	e2a := &E2AResponse{
		ProtocolVersion: E2AProtocolVersion,
		ResponseID:      ridOut,
		RequestID:       getStringFromPtr(reqID),
		Sequence:        0,
		IsFinal:         true,
		Status:          E2AResponseStatusFailed,
		ResponseKind:    E2AResponseKindE2AError,
		Timestamp:       ts,
		Provenance: E2AProvenance{
			SourceProtocol: E2ASourceProtocolE2A,
			Converter:      "e2a.wire_codec:EncodeJSONParseErrorWire",
			ConvertedAt:    ts,
			Details:        map[string]any{"kind": "json_parse_error"},
		},
		Body: map[string]any{
			"code":    "E2A.INVALID_JSON",
			"message": message,
			"details": map[string]any{},
		},
		Channel:        getStringFromPtr(ch),
		IdentityOrigin: IdentityOriginAgent,
		IsStream:       false,
	}

	return e2a.ToMap()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rawDictToAgentResponse 从原始 dict 构造 AgentResponse（legacy fallback 辅助）。
// 对应 Python: _raw_dict_to_agent_response(data)
func rawDictToAgentResponse(data map[string]any) *schema.AgentResponse {
	return &schema.AgentResponse{
		RequestID: getString(data, "request_id"),
		ChannelID: getString(data, "channel_id"),
		OK:        getBool(data, "ok", true),
		Payload:   getMapAny(data, "payload"),
		Metadata:  getMapAny(data, "metadata"),
	}
}

// rawDictToAgentChunk 从原始 dict 构造 AgentResponseChunk（legacy fallback 辅助）。
// 对应 Python: _raw_dict_to_agent_chunk(data)
func rawDictToAgentChunk(data map[string]any) *schema.AgentResponseChunk {
	return &schema.AgentResponseChunk{
		RequestID:  getString(data, "request_id"),
		ChannelID:  getString(data, "channel_id"),
		Payload:    getMapAny(data, "payload"),
		IsComplete: getBool(data, "is_complete", false),
	}
}

// fallbackWireUnaryFromLegacy 构造含 legacy 的 E2AResponse error 帧（unary）。
// 对应 Python: _fallback_wire_unary_from_legacy(legacy, ...)
func fallbackWireUnaryFromLegacy(legacy map[string]any, responseID string, sequence int, exc error) map[string]any {
	ts := UTCNowISO()
	prov := E2AProvenance{
		SourceProtocol: E2ASourceProtocolE2A,
		Converter:      "e2a.wire_codec:fallbackWireUnaryFromLegacy",
		ConvertedAt:    ts,
		Details:        map[string]any{"error": exc.Error(), "kind": "wire_encode_fallback"},
	}

	chStr := getString(legacy, "channel_id")
	var chPtr *string
	if chStr != "" {
		chPtr = &chStr
	}

	e2a := &E2AResponse{
		ProtocolVersion: E2AProtocolVersion,
		ResponseID:      responseID,
		RequestID:       getString(legacy, "request_id"),
		Sequence:        sequence,
		IsFinal:         true,
		Status:          E2AResponseStatusFailed,
		ResponseKind:    E2AResponseKindE2AError,
		Timestamp:       ts,
		Provenance:      prov,
		Body: map[string]any{
			"code":    "E2A.WIRE_ENCODE_ERROR",
			"message": "Failed to encode AgentResponse as E2A; see metadata legacy blob",
			"details": map[string]any{"error": exc.Error()},
		},
		Channel:        getStringFromPtr(chPtr),
		Metadata:       map[string]any{E2AWireLegacyAgentResponseKey: legacy},
		IdentityOrigin: IdentityOriginAgent,
		IsStream:       false,
	}

	return e2a.ToMap()
}

// fallbackWireChunkFromLegacy 构造含 legacy 的 E2AResponse error 帧（chunk）。
// 对应 Python: _fallback_wire_chunk_from_legacy(legacy, ...)
func fallbackWireChunkFromLegacy(legacy map[string]any, responseID string, sequence int, exc error, isStream bool) map[string]any {
	ts := UTCNowISO()
	prov := E2AProvenance{
		SourceProtocol: E2ASourceProtocolE2A,
		Converter:      "e2a.wire_codec:fallbackWireChunkFromLegacy",
		ConvertedAt:    ts,
		Details:        map[string]any{"error": exc.Error(), "kind": "wire_encode_chunk_fallback"},
	}

	chStr := getString(legacy, "channel_id")
	var chPtr *string
	if chStr != "" {
		chPtr = &chStr
	}

	isComplete := getBool(legacy, "is_complete", false)

	e2a := &E2AResponse{
		ProtocolVersion: E2AProtocolVersion,
		ResponseID:      responseID,
		RequestID:       getString(legacy, "request_id"),
		Sequence:        sequence,
		IsFinal:         isComplete,
		Status:          E2AResponseStatusFailed,
		ResponseKind:    E2AResponseKindE2AError,
		Timestamp:       ts,
		Provenance:      prov,
		Body: map[string]any{
			"code":    "E2A.WIRE_ENCODE_ERROR",
			"message": "Failed to encode AgentResponseChunk as E2A; see metadata legacy blob",
			"details": map[string]any{"error": exc.Error()},
		},
		Channel:        getStringFromPtr(chPtr),
		Metadata:       map[string]any{E2AWireLegacyAgentChunkKey: legacy},
		IdentityOrigin: IdentityOriginAgent,
		IsStream:       isStream,
	}

	return e2a.ToMap()
}

// agentResponseToMap 将 AgentResponse 转为 map[string]any（用于 legacy fallback）。
// 对应 Python: asdict(resp)
func agentResponseToMap(resp *schema.AgentResponse) map[string]any {
	return map[string]any{
		"request_id": resp.RequestID,
		"channel_id": resp.ChannelID,
		"ok":         resp.OK,
		"payload":    resp.Payload,
		"metadata":   resp.Metadata,
	}
}

// agentChunkToMap 将 AgentResponseChunk 转为 map[string]any（用于 legacy fallback）。
// 对应 Python: asdict(chunk)
func agentChunkToMap(chunk *schema.AgentResponseChunk) map[string]any {
	return map[string]any{
		"request_id":  chunk.RequestID,
		"channel_id":  chunk.ChannelID,
		"payload":     chunk.Payload,
		"is_complete": chunk.IsComplete,
	}
}

// getStringFromPtr 从字符串指针取值，nil 返回 ""。
func getStringFromPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
