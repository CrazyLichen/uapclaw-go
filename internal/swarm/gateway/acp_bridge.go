// ACP Gateway 路由协议对端桥（11.25）。
// 对齐 Python jiuwenswarm/gateway/channel_manager/protocol/acp/acp_connect.py
// L47-52（常量）、L83-97（RequestContext）、L99-118（三辅助）、L120-661（Bridge）。
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/wswrite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a/acp"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpGatewayBridge /acp WS 路由协议对端桥。
// Python: AcpGatewayBridge (acp_connect.py L120-661)。
type AcpGatewayBridge struct {
	// onMessageCb dispatch 回调（InboundGatewayServer 链，必填）
	onMessageCb func(ctx context.Context, msg *schema.Message) error
	// bindSessionClient 会话定向绑定回调（可 nil）
	bindSessionClient func(sessionID string, ws *websocket.Conn)
	// channelID 渠道标识（默认 "acp"）
	channelID string
	// idleFinalizeSecs idle finalize 时长（Python 值/可调用双态 → Go 统一 func，默认 3.0）
	idleFinalizeSecs func() float64
	// mu 保护五表
	mu sync.Mutex
	// requestCtxByReqID 请求上下文表
	requestCtxByReqID map[string]*AcpGatewayRequestCtx
	// activePromptBySession 会话活跃 prompt
	activePromptBySession map[string]string
	// knownSessions 已知会话集合
	knownSessions map[string]struct{}
	// pendingRPCCountBySession 会话 pending RPC 计数
	pendingRPCCountBySession map[string]int
	// pendingRPCSessionByID pending RPC 表（jsonrpcID → entry）
	pendingRPCSessionByID map[string]pendingRPCEntry
}

// AcpGatewayRequestCtx prompt 请求上下文。
// Python: AcpGatewayRequestContext (L83-97)。嵌入 AcpSessionUpdateState
//（Python 独立 assistant/thought/tool 字段并入，语义一致）。
type AcpGatewayRequestCtx struct {
	// JSONRPCID jsonrpc id（string|int 双形态，原样回写）
	JSONRPCID any
	// SessionID 会话标识
	SessionID string
	// UserMessageID user 消息 ID（可空）
	UserMessageID string
	// State session/update 状态（assistant/thought 文本与 messageID、toolCallCache）
	State acp.AcpSessionUpdateState
	// PendingStopReason 挂起收尾原因（pending RPC>0 时置 end_turn）
	PendingStopReason string
	// SawChatFinal 见过 chat.final
	SawChatFinal bool
	// SawProcessingIdle 见过 processing idle
	SawProcessingIdle bool
	// ClientWS 客户端连接
	ClientWS *websocket.Conn
	// idleFinalizeTimer idle finalize 定时器（回调首行引用比对防 superseded，偏差 §9-4）
	idleFinalizeTimer *time.Timer
}

// pendingRPCEntry pending RPC 条目（Python (session_id, ws, created_at) 元组）。
type pendingRPCEntry struct {
	sessionID string
	ws        *websocket.Conn
	createdAt float64
}

// AcpBridgeOption 桥构造选项。
type AcpBridgeOption func(*AcpGatewayBridge)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// acpPromptIdleFinalizeSeconds idle finalize 默认 3 秒（Python _PROMPT_IDLE_FINALIZE_SECONDS）
	acpPromptIdleFinalizeSeconds = 3.0
	// acpPendingRPCTimeoutSeconds pending RPC 过期 60 秒（Python _ACP_PENDING_RPC_TIMEOUT_SECONDS）
	acpPendingRPCTimeoutSeconds = 60.0
	// acpChannelIDDefault 默认渠道标识
	acpChannelIDDefault = "acp"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAcpGatewayBridge 构造桥。
// Python: AcpGatewayBridge.__init__ (L123-141)。
func NewAcpGatewayBridge(onMessageCb func(ctx context.Context, msg *schema.Message) error, opts ...AcpBridgeOption) *AcpGatewayBridge {
	b := &AcpGatewayBridge{
		onMessageCb:              onMessageCb,
		channelID:                acpChannelIDDefault,
		requestCtxByReqID:        make(map[string]*AcpGatewayRequestCtx),
		activePromptBySession:    make(map[string]string),
		knownSessions:            make(map[string]struct{}),
		pendingRPCCountBySession: make(map[string]int),
		pendingRPCSessionByID:    make(map[string]pendingRPCEntry),
	}
	for _, o := range opts {
		o(b)
	}
	if strings.TrimSpace(b.channelID) == "" {
		b.channelID = acpChannelIDDefault
	}
	return b
}

// WithBindSessionClient 设置会话绑定回调。
func WithBindSessionClient(fn func(sessionID string, ws *websocket.Conn)) AcpBridgeOption {
	return func(b *AcpGatewayBridge) { b.bindSessionClient = fn }
}

// WithAcpChannelID 设置渠道标识。
func WithAcpChannelID(channelID string) AcpBridgeOption {
	return func(b *AcpGatewayBridge) { b.channelID = strings.TrimSpace(channelID) }
}

// WithIdleFinalizeSeconds 设置 idle finalize 时长。
// Python idle_finalize_seconds 支持 float | Callable | None；Go 统一为 func() float64。
func WithIdleFinalizeSeconds(fn func() float64) AcpBridgeOption {
	return func(b *AcpGatewayBridge) { b.idleFinalizeSecs = fn }
}

// HandleJsonrpcRequest 处理 IDE JSON-RPC 请求（六方法分派）。
// Python: handle_jsonrpc_request (L241-333)。
// 返回 true = 已处理；false = 非 JSON-RPC 请求（交回调用方）。
func (b *AcpGatewayBridge) HandleJsonrpcRequest(ctx context.Context, ws *websocket.Conn, data map[string]any) bool {
	if !IsJsonrpcRequest(data) {
		return false
	}
	rpcID := data["id"]
	method := strings.TrimSpace(strVal(data["method"]))
	params, _ := data["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}

	if method == "initialize" {
		// Python L249-251：直接 build_acp_initialize_result()，无 clientCapabilities 合并
		b.sendRawJsonrpcResult(ws, rpcID, acp.BuildAcpInitializeResult())
		return true
	}
	if method == "session/new" {
		sessionID := strings.TrimSpace(strVal(params["sessionId"]))
		if sessionID == "" {
			sessionID = "acp_" + randHexStr(12)
		}
		b.bindSession(ws, sessionID)
		b.sendRawJsonrpcResult(ws, rpcID, acp.BuildAcpSessionNewResult(sessionID))
		return true
	}
	if method == "session/prompt" {
		sessionID := strings.TrimSpace(strVal(params["sessionId"]))
		if sessionID == "" {
			b.sendRawJsonrpcError(ws, rpcID, -32602, "sessionId is required")
			return true
		}
		text := ExtractPromptText(params)
		if text == "" {
			b.sendRawJsonrpcError(ws, rpcID, -32602, "prompt is required")
			return true
		}
		requestID := "acp_" + randHexStr(12)
		b.bindSession(ws, sessionID)
		b.mu.Lock()
		b.requestCtxByReqID[requestID] = &AcpGatewayRequestCtx{
			JSONRPCID:     rpcID,
			SessionID:     sessionID,
			UserMessageID: strings.TrimSpace(strVal(params["messageId"])),
			ClientWS:      ws,
		}
		b.activePromptBySession[sessionID] = requestID
		b.mu.Unlock()
		mParams := cpMap(params)
		mParams["content"] = text
		mParams["query"] = text
		mParams["session_id"] = sessionID
		paramsJSON, _ := json.Marshal(mParams)
		msg := &schema.Message{
			ID:        requestID,
			Type:      schema.MessageTypeReq,
			ChannelID: b.channelID,
			SessionID: sessionID,
			Params:    paramsJSON,
			Timestamp: schema.NowTimestamp(),
			OK:        true,
			ReqMethod: schema.ReqMethodChatSend,
			Mode:      schema.ModeAgentPlan,
			Metadata:  map[string]any{"acp": map[string]any{"jsonrpc_id": rpcID, "method": method}},
		}
		b.dispatchMessage(ctx, msg)
		return true
	}
	if method == "session/cancel" {
		sessionID := strings.TrimSpace(strVal(params["sessionId"]))
		if sessionID != "" {
			b.mu.Lock()
			requestID := b.activePromptBySession[sessionID]
			delete(b.activePromptBySession, sessionID)
			var pctx *AcpGatewayRequestCtx
			if requestID != "" {
				pctx = b.requestCtxByReqID[requestID]
				delete(b.requestCtxByReqID, requestID)
			}
			b.mu.Unlock()
			if pctx != nil {
				b.sendRawJsonrpcResult(ws, pctx.JSONRPCID,
					acp.BuildAcpPromptResult("cancelled", pctx.UserMessageID))
			}
		}
		b.sendRawJsonrpcResult(ws, rpcID, nil)
		return true
	}
	if method == "session/list" {
		b.mu.Lock()
		sids := make([]string, 0, len(b.knownSessions))
		for sid := range b.knownSessions {
			sids = append(sids, sid)
		}
		sort.Strings(sids)
		b.mu.Unlock()
		b.sendRawJsonrpcResult(ws, rpcID, acp.BuildAcpSessionListResult(sids))
		return true
	}
	if method == "session/load" {
		b.sendRawJsonrpcError(ws, rpcID, -32601,
			"Method not supported by agent capabilities: session/load")
		return true
	}
	b.sendRawJsonrpcError(ws, rpcID, -32601, "Method not found: "+method)
	return true
}

// IsJsonrpcRequest 判定 JSON-RPC 请求。
// Python: is_jsonrpc_request (L462-468)：dict + jsonrpc=="2.0" + method 是 str。
func IsJsonrpcRequest(data map[string]any) bool {
	if data == nil {
		return false
	}
	if v, _ := data["jsonrpc"].(string); v != "2.0" {
		return false
	}
	_, ok := data["method"].(string)
	return ok
}

// ExtractPromptText 提取 prompt 文本。
// Python: extract_prompt_text (L470-486)：prompt list[text] 拼接（有 text 项时），
// 否则 text/content/query 依序取非空白串。
func ExtractPromptText(params map[string]any) string {
	if prompt, ok := params["prompt"].([]any); ok {
		texts := make([]string, 0, len(prompt))
		for _, item := range prompt {
			if m, ok := item.(map[string]any); ok && m["type"] == "text" {
				if text, ok := m["text"].(string); ok && text != "" {
					texts = append(texts, text)
				}
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	for _, key := range []string{"text", "content", "query"} {
		if v, ok := params[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// dispatchMessage 派发消息（on_message_cb 链）。
// Python: _dispatch_message (L487-493)：cb 为 None → False；结果取 bool。
func (b *AcpGatewayBridge) dispatchMessage(ctx context.Context, msg *schema.Message) {
	if b.onMessageCb == nil {
		return
	}
	if err := b.onMessageCb(ctx, msg); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("msg_id", msg.ID).
			Msg("acp bridge on_message callback failed")
	}
}

// bindSession 登记会话并回调绑定。
// Python: _bind_session (L495-501)：knownSessions.add + bind_session_client(sid, ws)。
func (b *AcpGatewayBridge) bindSession(ws *websocket.Conn, sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	b.mu.Lock()
	b.knownSessions[sid] = struct{}{}
	b.mu.Unlock()
	if b.bindSessionClient != nil {
		b.bindSessionClient(sid, ws)
	}
}

// sendRawJsonrpcResult 发送裸 JSON-RPC result 帧。
// Python: _send_raw_jsonrpc_result (L642-643)。写失败静默（Debug）。
func (b *AcpGatewayBridge) sendRawJsonrpcResult(ws *websocket.Conn, rpcID any, result any) {
	frame := map[string]any{"jsonrpc": "2.0", "id": rpcID, "result": result}
	data, _ := json.Marshal(frame)
	writeAcpFrame(ws, data)
}

// sendRawJsonrpcError 发送裸 JSON-RPC error 帧。
// Python: _send_raw_jsonrpc_error (L646-657)。
func (b *AcpGatewayBridge) sendRawJsonrpcError(ws *websocket.Conn, rpcID any, code int, message string) {
	frame := map[string]any{
		"jsonrpc": "2.0",
		"id":      rpcID,
		"error":   map[string]any{"code": code, "message": message},
	}
	data, _ := json.Marshal(frame)
	writeAcpFrame(ws, data)
}

// sendRawJsonrpcNotification 发送裸 JSON-RPC notification 帧。
// Python: _send_raw_jsonrpc_notification (L659-661)。
func (b *AcpGatewayBridge) sendRawJsonrpcNotification(ws *websocket.Conn, method string, params map[string]any) {
	frame := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	data, _ := json.Marshal(frame)
	writeAcpFrame(ws, data)
}

// writeAcpFrame 经 wswrite per-conn 写锁写帧（偏差 §9-3）。
// ws 为 nil（测试）或写失败 → Debug 日志，不 panic。
func writeAcpFrame(ws *websocket.Conn, data []byte) {
	if ws == nil {
		return
	}
	wswrite.Lock(ws, func() {
		if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
			logger.Debug(logComponent).
				Err(err).
				Msg("acp bridge frame write failed (client disconnected?)")
		}
	})
}

// randHexStr 生成 n 字符 hex（crypto/rand）。
// Python: uuid.uuid4().hex[:12] 等价语义（12 hex 字符）。
func randHexStr(n int) string {
	buf := make([]byte, (n+1)/2)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)[:n]
}

// cpMap 一层浅拷贝（gateway 包内私有）。
func cpMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
