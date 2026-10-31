// ACP Gateway 路由协议对端桥（11.25）。
// 对齐 Python jiuwenswarm/gateway/channel_manager/protocol/acp/acp_connect.py
// L47-52（常量）、L83-97（RequestContext）、L99-118（三辅助）、L120-661（Bridge）。
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/wswrite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
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
// （Python 独立 assistant/thought/tool 字段并入，语义一致）。
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
	// idleFinalizeTimerSlot idle finalize 定时器槽（回调引用比对防 superseded，偏差 §9-4）
	idleFinalizeTimerSlot *acpIdleTimerSlot
	// matchedRequestID maybeFinalizeDeferredPrompts 遍历辅助（Python 以局部变量
	// 元组携带；Go 需挂到结构体以在锁外使用）
	matchedRequestID string
}

// acpIdleTimerSlot idle finalize 定时器槽（§9-4）。
// AfterFunc 回调可能先于闭包变量赋值启动，闭包不能引用 timer 变量本身；
// 经 slot 中转：调度时先建 slot 写 ctx 字段，回调锁内比对 slot 归属。
type acpIdleTimerSlot struct {
	// timer 已 arm 的定时器（slot 创建后锁内赋值，回调只读）
	timer *time.Timer
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
//
// 异常兜底对齐 Python L327-331 的 `except Exception → -32603`：panic 转 JSON-RPC
// 错误帧而非拖垮连接（通道版 handleJsonrpcRequest 有同构 recover）。
func (b *AcpGatewayBridge) HandleJsonrpcRequest(ctx context.Context, ws *websocket.Conn, data map[string]any) (handled bool) {
	if !IsJsonrpcRequest(data) {
		return false
	}
	rpcID := data["id"]
	method := strings.TrimSpace(strVal(data["method"]))
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Any("panic", r).
				Str("method", method).
				Msg("Gateway ACP JSON-RPC handler failed")
			b.sendRawJsonrpcError(ws, rpcID, -32603, fmt.Sprintf("%v", r))
			handled = true
		}
	}()
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

// InboundIntercept 入站拦截器：识别 IDE JSON-RPC 响应（工具结果回流）。
// Python: inbound_intercept (L147-198)。
// 返回 true = 已拦截（响应帧或软忽略帧）；false = 非响应帧，交回路由。
func (b *AcpGatewayBridge) InboundIntercept(ws *websocket.Conn, data map[string]any) bool {
	if data == nil {
		return false
	}
	if v, _ := data["jsonrpc"].(string); strings.TrimSpace(v) != "2.0" {
		return false
	}
	// 数字型 id 必须归一（IDE 可能把我们的 string id 回成 number）——否则 pending
	// 查不到会把 tool response 当 unknown 丢弃，agent 侧工具调用永久挂起。
	idStr := acp.NormalizeJSONRPCID(data["id"])
	_, hasResult := data["result"]
	_, hasError := data["error"]
	if idStr == "" || (!hasResult && !hasError) {
		return false
	}

	jsonrpcID := idStr
	b.mu.Lock()
	pending, ok := b.pendingRPCSessionByID[jsonrpcID]
	if ok {
		delete(b.pendingRPCSessionByID, jsonrpcID)
	}
	b.mu.Unlock()
	sessionID := ""
	if ok {
		sessionID = strings.TrimSpace(pending.sessionID)
	}

	if jsonrpcID == "" || sessionID == "" {
		logger.Info(logComponent).
			Str("jsonrpc_id", jsonrpcID).
			Str("session_id", sessionID).
			Msg("[ACP] ignoring unknown/late client rpc response")
		writeAcpFrame(ws, mustJSON(map[string]any{
			"type": "res",
			"id":   jsonrpcID,
			"ok":   true,
			"payload": map[string]any{
				"accepted": false,
				"ignored":  true,
				"reason":   "unknown_or_late_response",
			},
		}))
		return true
	}

	msg := e2a.BuildACPToolResponseMessage(jsonrpcID, cpMap(data), sessionID, b.channelID)
	b.dispatchMessage(context.Background(), msg)
	b.resolvePendingRPC(sessionID)
	return true
}

// OutboundIntercept 出站拦截器：acp.output_request 原样转发 IDE。
// Python: outbound_intercept (L199-228)。
// Go 侧拦截器收 {"raw": msg} 包裹帧（gateway_server.go Send() 既有约定）。
// 返回 true = 已拦截；false = 非本拦截器关注的消息。
func (b *AcpGatewayBridge) OutboundIntercept(ws *websocket.Conn, data map[string]any) bool {
	msg, _ := data["raw"].(*schema.Message)
	if msg == nil {
		return false
	}
	if msg.Type != schema.MessageTypeEvent || msg.Payload == nil {
		return false
	}
	if strVal(msg.Payload["event_type"]) != "acp.output_request" {
		return false
	}
	rawJsonrpc, ok := msg.Payload["jsonrpc"].(map[string]any)
	if !ok {
		logger.Warn(logComponent).
			Msg("[ACP] outbound_intercept: invalid jsonrpc payload (not dict), dropping frame")
		return true
	}
	jsonrpcID := acp.NormalizeJSONRPCID(rawJsonrpc["id"])
	sessionID := strings.TrimSpace(msg.SessionID)
	if jsonrpcID == "" || sessionID == "" {
		logger.Warn(logComponent).
			Str("jsonrpc_id", jsonrpcID).
			Str("session_id", sessionID).
			Msg("[ACP] outbound_intercept: missing jsonrpc_id or session_id, forwarding without pending registration")
	} else {
		b.mu.Lock()
		b.pendingRPCSessionByID[jsonrpcID] = pendingRPCEntry{
			sessionID: sessionID,
			ws:        ws,
			createdAt: nowFloat(),
		}
		b.mu.Unlock()
		b.registerPendingRPC(sessionID)
	}
	b.sweepStalePending()
	writeAcpFrame(ws, mustJSON(rawJsonrpc))
	return true
}

// Cleanup 断连清理：按 ws 清 pending RPC 表。
// Python: cleanup (L229-240)。
func (b *AcpGatewayBridge) Cleanup(ws *websocket.Conn) {
	b.mu.Lock()
	stale := make([]string, 0)
	for jsonrpcID, entry := range b.pendingRPCSessionByID {
		if entry.ws == ws {
			stale = append(stale, jsonrpcID)
		}
	}
	for _, jsonrpcID := range stale {
		entry := b.pendingRPCSessionByID[jsonrpcID]
		delete(b.pendingRPCSessionByID, jsonrpcID)
		b.decreasePendingRPCCountLocked(entry.sessionID)
	}
	b.mu.Unlock()
}

// SendMessage 出站消息转换（GatewayServer.Send 的 acp 分支调用）。
// Python: send_message (L334-460) 五分支：
// delta → update+usage；reasoning/tool 系/todo/subtask → update；
// chat.final → final text + end_turn 三态；chat.error → -32603；
// processing_status → idle 计数 + finalize 调度。
// 返回 true = 已处理；false = 无 ctx 或未关注事件（交回通用出站）。
// 锁纪律：ctx 可变字段经持 b.mu 访问（timer 回调 goroutine 并发，-race 干净关键）。
func (b *AcpGatewayBridge) SendMessage(ctx context.Context, msg *schema.Message, ws *websocket.Conn) bool {
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	sessionID := strings.TrimSpace(msg.SessionID)

	b.mu.Lock()
	pctx := b.requestCtxByReqID[msg.ID]
	if pctx == nil {
		b.mu.Unlock()
		return false
	}
	isEvent := msg.Type == schema.MessageTypeEvent
	switch {
	case isEvent && msg.EventType == schema.EventTypeChatDelta:
		update := acp.BuildAcpSessionUpdate(msg, payload, &pctx.State)
		if update == nil {
			b.mu.Unlock()
			return true
		}
		markStreamActivityLocked(pctx)
		b.mu.Unlock()
		b.sendRawJsonrpcNotification(ws, "session/update", map[string]any{
			"sessionId": orDefault(sessionID, pctx.SessionID), "update": update,
		})
		if usageUpdate := acp.BuildAcpUsageUpdate(payload); usageUpdate != nil {
			b.sendRawJsonrpcNotification(ws, "session/update", map[string]any{
				"sessionId": orDefault(sessionID, pctx.SessionID), "update": usageUpdate,
			})
		}
		return true
	case isEvent && (msg.EventType == schema.EventTypeChatReasoning ||
		msg.EventType == schema.EventTypeChatToolCall || msg.EventType == schema.EventTypeChatToolUpdate ||
		msg.EventType == schema.EventTypeChatToolResult || msg.EventType == schema.EventTypeTodoUpdated ||
		msg.EventType == schema.EventTypeChatSubtaskUpdate):
		update := acp.BuildAcpSessionUpdate(msg, payload, &pctx.State)
		if update == nil {
			b.mu.Unlock()
			return true
		}
		markStreamActivityLocked(pctx)
		b.mu.Unlock()
		b.sendRawJsonrpcNotification(ws, "session/update", map[string]any{
			"sessionId": orDefault(sessionID, pctx.SessionID), "update": update,
		})
		return true
	case isEvent && msg.EventType == schema.EventTypeChatFinal:
		pctx.SawChatFinal = true
		update := acp.BuildAcpFinalTextUpdate(payload, &pctx.State)
		sawIdle := pctx.SawProcessingIdle
		hasPending := b.pendingRPCCountBySession[orDefault(sessionID, pctx.SessionID)] > 0
		jsonrpcID := pctx.JSONRPCID
		userMessageID := pctx.UserMessageID
		pctxSessionID := pctx.SessionID
		// cancel 条件对齐 Python L398-413：仅「已见 idle 且有 pending」保留 timer
		// （该分支转 pending_stop_reason 延后收尾）；其余两态（已见 idle 无 pending、
		// 未见过 idle）Python 都调 _cancel_idle_finalize_task。
		// 未见过 idle 也必须取消：deferred 解挂路径（maybeFinalizeDeferredPrompts）
		// 会在 SawProcessingIdle==false 下 armed timer，不取消会让 timer 提前 3s
		// 发 end_turn，与 Python「等真正 idle 再收尾」不等价。
		if !(sawIdle && hasPending) {
			cancelIdleFinalizeTimerLocked(pctx)
		}
		b.mu.Unlock()
		if update != nil {
			b.sendRawJsonrpcNotification(ws, "session/update", map[string]any{
				"sessionId": orDefault(sessionID, pctxSessionID), "update": update,
			})
		}
		if usageUpdate := acp.BuildAcpUsageUpdate(payload); usageUpdate != nil {
			b.sendRawJsonrpcNotification(ws, "session/update", map[string]any{
				"sessionId": orDefault(sessionID, pctxSessionID), "update": usageUpdate,
			})
		}
		if sawIdle {
			if hasPending {
				b.mu.Lock()
				pctx.PendingStopReason = "end_turn"
				b.mu.Unlock()
				return true
			}
			b.sendRawJsonrpcResult(ws, jsonrpcID,
				acp.BuildAcpPromptResult("end_turn", userMessageID))
			b.clearRequestContext(msg.ID, pctxSessionID)
			return true
		}
		// 未见过 idle：只记 final，等后续 idle 触发收尾（Python L409-411）
		return true
	case isEvent && msg.EventType == schema.EventTypeChatError:
		cancelIdleFinalizeTimerLocked(pctx)
		jsonrpcID := pctx.JSONRPCID
		pctxSessionID := pctx.SessionID
		b.mu.Unlock()
		errText := strVal(payload["error"])
		if errText == "" {
			errText = strVal(payload["content"])
		}
		if errText == "" {
			errText = "Agent error"
		}
		b.sendRawJsonrpcError(ws, jsonrpcID, -32603, errText)
		b.clearRequestContext(msg.ID, pctxSessionID)
		return true
	case isEvent && msg.EventType == schema.EventTypeChatProcessingStatus:
		// 归档偏差 B7：Python 桥版把 is_processing 处理嵌在 `if update is not None` 内
		// （acp_connect.py L427-457），通道版则写在 update 写帧之外（L1408-1433）——
		// 两版本身不一致。Go 桥跟桥版、通道跟通道版。BuildAcpSessionUpdate 对
		// CHAT_PROCESSING_STATUS 恒返回 session_info_update（session_updates.go），
		// update 永不为 nil，故该差异不可达。
		update := acp.BuildAcpSessionUpdate(msg, payload, &pctx.State)
		if update == nil {
			b.mu.Unlock()
			return true
		}
		isProc, isProcSet := payload["is_processing"].(bool)
		finishNow := false
		scheduleWait := false
		if isProcSet && !isProc {
			pctx.SawProcessingIdle = true
			hasPending := b.pendingRPCCountBySession[orDefault(sessionID, pctx.SessionID)] > 0
			if hasPending {
				pctx.PendingStopReason = "end_turn"
			} else if shouldWaitForFinalTextBeforeEndTurn(pctx) {
				scheduleWait = true
			} else {
				cancelIdleFinalizeTimerLocked(pctx)
				finishNow = true
			}
		} else if isProcSet && isProc {
			pctx.SawProcessingIdle = false
			cancelIdleFinalizeTimerLocked(pctx)
		}
		jsonrpcID := pctx.JSONRPCID
		userMessageID := pctx.UserMessageID
		pctxSessionID := pctx.SessionID
		b.mu.Unlock()
		b.sendRawJsonrpcNotification(ws, "session/update", map[string]any{
			"sessionId": orDefault(sessionID, pctxSessionID), "update": update,
		})
		if scheduleWait {
			b.scheduleIdleFinalize(msg.ID, pctx, ws)
			return true
		}
		if finishNow {
			b.sendRawJsonrpcResult(ws, jsonrpcID,
				acp.BuildAcpPromptResult("end_turn", userMessageID))
			b.clearRequestContext(msg.ID, pctxSessionID)
		}
		return true
	}
	b.mu.Unlock()
	return false
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

// markStreamActivityLocked 流活动重置 idle（调用方持 b.mu）。
// Python: _mark_stream_activity (L106-109)。
func markStreamActivityLocked(pctx *AcpGatewayRequestCtx) {
	pctx.SawProcessingIdle = false
	cancelIdleFinalizeTimerLocked(pctx)
}

// cancelIdleFinalizeTimerLocked 取消 idle finalize timer 后置 nil（调用方持 b.mu）。
// Python: _cancel_idle_finalize_task (L99-104)。
// Go 侧取消点统一在持锁段内执行（SendMessage/清理路径），timer.Stop 后置 nil。
func cancelIdleFinalizeTimerLocked(pctx *AcpGatewayRequestCtx) {
	if pctx.idleFinalizeTimerSlot != nil {
		if pctx.idleFinalizeTimerSlot.timer != nil {
			pctx.idleFinalizeTimerSlot.timer.Stop()
		}
		pctx.idleFinalizeTimerSlot = nil
	}
}

// shouldWaitForFinalTextBeforeEndTurn 判定是否等迟到 final text。
// Python: _should_wait_for_final_text_before_end_turn (L111-118)：
// sawChatFinal=true → false；assistant_text 或 assistant_message_id 非空 → true。
func shouldWaitForFinalTextBeforeEndTurn(pctx *AcpGatewayRequestCtx) bool {
	if pctx.SawChatFinal {
		return false
	}
	return pctx.State.AssistantText != "" || pctx.State.AssistantMessageID != ""
}

// registerPendingRPC 会话 pending 计数 +1 并取消该会话全部 idle finalize timer。
// Python: _register_pending_client_rpc (L503-516)。
func (b *AcpGatewayBridge) registerPendingRPC(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	b.mu.Lock()
	b.pendingRPCCountBySession[sid]++
	entries := make([]*AcpGatewayRequestCtx, 0)
	for _, c := range b.requestCtxByReqID {
		if c.SessionID == sid {
			entries = append(entries, c)
		}
	}
	b.mu.Unlock()
	for _, c := range entries {
		b.mu.Lock()
		cancelIdleFinalizeTimerLocked(c)
		b.mu.Unlock()
	}
}

// resolvePendingRPC 会话 pending 计数 -1 + maybe finalize。
// Python: _resolve_pending_client_rpc (L517-523)。
func (b *AcpGatewayBridge) resolvePendingRPC(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	b.decreasePendingRPCCount(sid)
	b.maybeFinalizeDeferredPrompts(sid)
}

// decreasePendingRPCCount 计数递减（<=1 删键）。
// Python: _decrease_pending_client_rpc_count (L524-533)。
func (b *AcpGatewayBridge) decreasePendingRPCCount(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	b.mu.Lock()
	b.decreasePendingRPCCountLocked(sid)
	b.mu.Unlock()
}

// decreasePendingRPCCountLocked 计数递减（调用方持 b.mu，供 Cleanup 复用）。
func (b *AcpGatewayBridge) decreasePendingRPCCountLocked(sessionID string) {
	current := b.pendingRPCCountBySession[sessionID]
	if current <= 1 {
		delete(b.pendingRPCCountBySession, sessionID)
	} else {
		b.pendingRPCCountBySession[sessionID] = current - 1
	}
}

// sessionHasPendingRPCs 会话 pending 判定。
// Python: _session_has_pending_client_rpcs (L534-538)。
func (b *AcpGatewayBridge) sessionHasPendingRPCs(sessionID string) bool {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pendingRPCCountBySession[sid] > 0
}

// maybeFinalizeDeferredPrompts pending 清零后收尾挂起的 prompt。
// Python: _maybe_finalize_deferred_prompts (L540-575)。
func (b *AcpGatewayBridge) maybeFinalizeDeferredPrompts(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || b.sessionHasPendingRPCs(sid) {
		return
	}
	b.mu.Lock()
	matched := make([]*AcpGatewayRequestCtx, 0)
	for requestID, c := range b.requestCtxByReqID {
		if c.SessionID == sid && strings.TrimSpace(c.PendingStopReason) != "" {
			c.matchedRequestID = requestID
			matched = append(matched, c)
		}
	}
	b.mu.Unlock()

	for _, c := range matched {
		b.mu.Lock()
		stopReason := strings.TrimSpace(c.PendingStopReason)
		if stopReason == "" {
			b.mu.Unlock()
			continue
		}
		c.PendingStopReason = ""
		ws := c.ClientWS
		b.mu.Unlock()
		if ws == nil {
			continue
		}
		if shouldWaitForFinalTextBeforeEndTurn(c) {
			b.scheduleIdleFinalize(c.matchedRequestID, c, ws)
			continue
		}
		b.mu.Lock()
		cancelIdleFinalizeTimerLocked(c)
		b.mu.Unlock()
		b.sendRawJsonrpcResult(ws, c.JSONRPCID,
			acp.BuildAcpPromptResult(stopReason, c.UserMessageID))
		b.clearRequestContext(c.matchedRequestID, sid)
	}
}

// clearRequestContext 清理请求上下文。
// Python: _clear_request_context (L577-583)。
func (b *AcpGatewayBridge) clearRequestContext(requestID, sessionID string) {
	b.mu.Lock()
	if pctx, ok := b.requestCtxByReqID[requestID]; ok {
		cancelIdleFinalizeTimerLocked(pctx)
		delete(b.requestCtxByReqID, requestID)
	}
	if b.activePromptBySession[sessionID] == requestID {
		delete(b.activePromptBySession, sessionID)
	}
	b.mu.Unlock()
}

// scheduleIdleFinalize 调度 idle finalize timer。
// Python: _schedule_idle_finalize (L585-592)。
// 竞态安全（§9-4 关键）：AfterFunc 回调可能先于闭包变量赋值完成启动，
// 故不可在回调里引用闭包 timer 变量。改为经 acpIdleTimerSlot 中转：
// 锁内先建 slot 并写入 ctx 字段，再 arm timer（回调从 slot.idle 读引用）。
func (b *AcpGatewayBridge) scheduleIdleFinalize(requestID string, pctx *AcpGatewayRequestCtx, ws *websocket.Conn) {
	b.mu.Lock()
	cancelIdleFinalizeTimerLocked(pctx)
	secs := b.getIdleFinalizeSeconds()
	slot := &acpIdleTimerSlot{}
	pctx.idleFinalizeTimerSlot = slot
	slot.timer = time.AfterFunc(time.Duration(secs*float64(time.Second)), func() {
		b.idleFinalizeAfterTimeout(requestID, pctx, slot, ws)
	})
	b.mu.Unlock()
}

// idleFinalizeAfterTimeout timer 超时回调：引用比对防 superseded。
// Python: _idle_finalize_after_timeout (L594-615)——asyncio.current_task 比对
// 的 Go 等价：回调经 slot（调度时 ctx 上的占位）比对当前 timer 归属。
func (b *AcpGatewayBridge) idleFinalizeAfterTimeout(requestID string, pctx *AcpGatewayRequestCtx, slot *acpIdleTimerSlot, ws *websocket.Conn) {
	b.mu.Lock()
	cur, ok := b.requestCtxByReqID[requestID]
	if !ok || cur != pctx || pctx.idleFinalizeTimerSlot != slot {
		b.mu.Unlock()
		return
	}
	if b.pendingRPCCountBySession[pctx.SessionID] > 0 {
		pctx.PendingStopReason = "end_turn"
		pctx.idleFinalizeTimerSlot = nil
		b.mu.Unlock()
		return
	}
	pctx.idleFinalizeTimerSlot = nil
	// 比对通过后需停掉 slot 内 timer（正常触发场景 Stop 返回 false，无害）
	if slot.timer != nil {
		slot.timer.Stop()
	}
	b.mu.Unlock()
	b.sendRawJsonrpcResult(ws, pctx.JSONRPCID,
		acp.BuildAcpPromptResult("end_turn", pctx.UserMessageID))
	b.clearRequestContext(requestID, pctx.SessionID)
}

// getIdleFinalizeSeconds 取 idle finalize 秒数（默认 3.0）。
// Python: _get_idle_finalize_seconds (L617-627)。
func (b *AcpGatewayBridge) getIdleFinalizeSeconds() float64 {
	if b.idleFinalizeSecs != nil {
		return b.idleFinalizeSecs()
	}
	return acpPromptIdleFinalizeSeconds
}

// sweepStalePending 清扫过期 pending RPC（60s）。
// Python: _sweep_stale_pending (L629-640)。
func (b *AcpGatewayBridge) sweepStalePending() {
	now := nowFloat()
	b.mu.Lock()
	stale := make([][2]string, 0)
	for jsonrpcID, entry := range b.pendingRPCSessionByID {
		if now-entry.createdAt > acpPendingRPCTimeoutSeconds {
			stale = append(stale, [2]string{jsonrpcID, strings.TrimSpace(entry.sessionID)})
			delete(b.pendingRPCSessionByID, jsonrpcID)
		}
	}
	b.mu.Unlock()
	for _, pair := range stale {
		logger.Info(logComponent).
			Str("jsonrpc_id", pair[0]).
			Msg("[ACP] pending RPC entry expired")
		b.resolvePendingRPC(pair[1])
	}
}

// mustJSON 序列化（失败返回空字节数组，防御）。
func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return data
}

// nowFloat 当前 Unix 秒（float）。
func nowFloat() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// orDefault 空串回退。
func orDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
