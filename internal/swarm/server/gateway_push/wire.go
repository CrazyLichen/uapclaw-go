package gateway_push

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponentWire wire 编码日志组件
const logComponentWire = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildServerPushWire 将 send_push 入参编码为 E2A 响应线 dict。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/wire.py (build_server_push_wire)。
// 编码后的 dict 带有 metadata[E2A_WIRE_SERVER_PUSH_KEY] = True 标记，
// 使 AgentClient 的 receiverLoop 能识别此帧为 server_push 并调回调投递。
func BuildServerPushWire(msg map[string]any) map[string]any {
	responseKind := ""
	if rk, ok := msg["response_kind"].(string); ok {
		responseKind = strings.TrimSpace(rk)
	}

	if responseKind != "" {
		// 有 response_kind → 编码为完整 E2AResponse wire
		return buildServerPushWireWithResponseKind(msg, responseKind)
	}
	// 无 response_kind → 编码为 chunk 形 wire
	return buildServerPushWireChunk(msg)
}

// BuildConnectionAckFrame 构建 connection.ack 事件帧 dict。
//
// 对齐 Python: jiuwenswarm/server/agent_ws_server.py (_connection_handler 首帧)。
// 此帧通过 RecvCh 发送到 AgentClient，在 receiverLoop 中被识别为事件帧。
func BuildConnectionAckFrame() map[string]any {
	return map[string]any{
		"type":    "event",
		"event":   "connection.ack",
		"payload": map[string]any{"status": "ready"},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildServerPushWireWithResponseKind 有 response_kind 的 server_push 编码。
//
// 对齐 Python: wire.py build_server_push_wire 中 response_kind 非空分支。
func buildServerPushWireWithResponseKind(msg map[string]any, responseKind string) map[string]any {
	requestID := WireRequestIDKey(msg["request_id"])

	e2aResp := e2a.NewE2AResponse()
	e2aResp.ResponseID = requestID
	e2aResp.RequestID = requestID
	e2aResp.Sequence = 0
	e2aResp.IsFinal = true
	e2aResp.Status = e2a.E2AResponseStatusSucceeded
	e2aResp.ResponseKind = responseKind
	e2aResp.EnsureTimestamp()

	if channel, ok := msg["channel_id"].(string); ok && channel != "" {
		e2aResp.Channel = channel
	}
	if sessionID, ok := msg["session_id"].(string); ok {
		e2aResp.SessionID = sessionID
	}
	if body, ok := msg["body"].(map[string]any); ok {
		e2aResp.Body = body
	}
	// 设置 server_push 标记（确保 Metadata 非 nil）
	if e2aResp.Metadata == nil {
		e2aResp.Metadata = make(map[string]any)
	}
	if metadata, ok := msg["metadata"].(map[string]any); ok {
		for k, v := range metadata {
			e2aResp.Metadata[k] = v
		}
	}
	e2aResp.Metadata[e2a.E2AWireServerPushKey] = true

	wire := e2aResp.ToMap()
	return wire
}

// buildServerPushWireChunk 无 response_kind 的 server_push 编码（chunk 形）。
//
// 对齐 Python: wire.py build_server_push_wire 中 response_kind 为空分支。
func buildServerPushWireChunk(msg map[string]any) map[string]any {
	requestID := WireRequestIDKey(msg["request_id"])
	channelID := ""
	if cid, ok := msg["channel_id"].(string); ok {
		channelID = cid
	}
	var payload map[string]any
	if p, ok := msg["payload"].(map[string]any); ok {
		payload = p
	}

	// 使用 EncodeAgentChunkForWire 编码
	chunk := schema.NewAgentResponseChunk(requestID, channelID, payload)
	wire := e2a.EncodeAgentChunkForWire(chunk, requestID, 0, false)

	// 合并 metadata
	metadata := make(map[string]any)
	if wireMeta, ok := wire["metadata"].(map[string]any); ok {
		for k, v := range wireMeta {
			metadata[k] = v
		}
	}
	if msgMeta, ok := msg["metadata"].(map[string]any); ok {
		for k, v := range msgMeta {
			if _, isInternal := e2a.E2AWireInternalMetadataKeys[k]; isInternal {
				continue
			}
			metadata[k] = v
		}
	}
	metadata[e2a.E2AWireServerPushKey] = true
	wire["metadata"] = metadata

	if sessionID, ok := msg["session_id"].(string); ok && sessionID != "" {
		wire["session_id"] = sessionID
	}
	return wire
}

// WireRequestIDKey 统一 request_id 为字符串。
//
// 对齐 Python: _wire_request_id_key — 将任意 request_id 值转为字符串，
// 避免 JSON 数字/字符串导致队列键不一致。
func WireRequestIDKey(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	// 非字符串类型转为字符串（对齐 Python str()）
	return fmt.Sprintf("%v", v)
}
