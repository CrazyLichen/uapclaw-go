package acp

// AcpChannel ACP stdio 通道本体（11.25 Task 8）。
// Python: acp_connect.py AcpChannel (L663-1738)——stdio 三路由 +
// jsonrpc 六方法 + E2A 双模式出站 + pending/finalize 家族。
// WS 客户端族（L1526-1738）在 Task 9 补全。

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	e2a "github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	e2aacp "github.com/uapclaw/uapclaw-go/internal/swarm/e2a/acp"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpChannelConfig ACP 通道配置。
// Python: AcpChannelConfig (acp_connect.py L55-63)。
type AcpChannelConfig struct {
	// Enabled 是否启用（默认 true）
	Enabled bool
	// ChannelID 渠道标识（默认 "acp"）
	ChannelID string
	// DefaultSessionID 默认会话（默认 "acp_cli_session"）
	DefaultSessionID string
	// Metadata 附加元数据
	Metadata map[string]any
}

// acpRequestCtx 通道侧请求上下文（双 response_mode）。
// Python: _AcpRequestContext (L65-81)。State 并入 AcpSessionUpdateState。
type acpRequestCtx struct {
	// JSONRPCID jsonrpc id（string|int 双形态）
	JSONRPCID any
	// Method 请求方法
	Method string
	// ResponseMode 出站模式（"e2a"|"jsonrpc"）
	ResponseMode string
	// SessionID 会话
	SessionID string
	// UserMessageID user 消息 ID
	UserMessageID string
	// State session/update 状态
	State e2aacp.AcpSessionUpdateState
	// PendingStopReason 挂起收尾原因
	PendingStopReason string
	// SawChatFinal 见过 chat.final
	SawChatFinal bool
	// SawProcessingIdle 见过 processing idle
	SawProcessingIdle bool
	// Sequence 流式序号
	Sequence int
	// idleFinalizeTimerSlot idle finalize 定时器槽（引用比对防 superseded，§9-4）
	idleFinalizeTimerSlot *acpIdleTimerSlot
	// matchedRequestID maybeFinalize 遍历辅助
	matchedRequestID string
}

// acpPendingEntry pending RPC 条目（Python (session_id, created_at)）。
type acpPendingEntry struct {
	sessionID string
	createdAt float64
}

// AcpChannel ACP stdio 通道：stdin 逐行 JSON（E2A envelope 或 JSON-RPC），
// stdout 逐行 JSON-RPC；WS 客户端转发到 Gateway。
// Python: AcpChannel (L663-1738)。
type AcpChannel struct {
	// cfg 通道配置
	cfg AcpChannelConfig
	// _channelID 归一化渠道标识
	_channelID string
	// _defaultSessionID 归一化默认会话
	_defaultSessionID string
	// gatewayURL 网关 WS 地址（空 = 未启用 WS 客户端，测试注入 onMessage 模式）
	gatewayURL string
	// gwConn 网关连接
	gwConn *websocket.Conn
	// gwConnMu 保护 gwConn/连接关闭
	gwConnMu sync.Mutex
	// onMessageCb dispatch 回调（测试注入；生产路径走 sendToGateway，§9-1）
	onMessageCb func(msg *schema.Message) bool
	// stdin stdin 读入（生产 os.Stdin；测试注入）
	stdin io.Reader
	// stdout stdout 写出（早捕获，§9-8）
	stdout io.Writer
	// stdoutMu stdout 写互斥（§9-2：stdin loop / WS reader / send 三方并发）
	stdoutMu sync.Mutex
	// running 运行标志
	running bool
	// runMu 保护 running
	runMu sync.Mutex
	// mu 保护四表
	mu sync.Mutex
	// requestCtx 请求上下文表
	requestCtx map[string]*acpRequestCtx
	// sessionCtx 会话上下文（session/new params）
	sessionCtx map[string]map[string]any
	// activePromptBySession 会话活跃 prompt
	activePromptBySession map[string]string
	// knownSessions 已知会话集合
	knownSessions map[string]struct{}
	// pendingRPCSessionByID pending RPC 表
	pendingRPCSessionByID map[string]acpPendingEntry
	// stdinEOFGraceSecs EOF 宽限秒数（测试可缩；Python monkeypatch _STDIN_EOF_GRACE_SECONDS）
	stdinEOFGraceSecs float64
	// idleFinalizeSecs idle finalize 秒数（测试可缩）
	idleFinalizeSecs float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// acpStdinEOFGraceSeconds stdin EOF 宽限（Python _STDIN_EOF_GRACE_SECONDS）
	acpStdinEOFGraceSeconds = 5.0
	// acpPromptIdleFinalizeSeconds idle finalize（Python _PROMPT_IDLE_FINALIZE_SECONDS）
	acpPromptIdleFinalizeSeconds = 3.0
	// acpPendingRPCTimeoutSeconds pending RPC 过期（Python _ACP_PENDING_RPC_TIMEOUT_SECONDS）
	acpPendingRPCTimeoutSeconds = 60.0
	// acpGatewayConnectMaxAttempts WS 连接尝试次数（Python _ACP_GATEWAY_CONNECT_MAX_ATTEMPTS）
	acpGatewayConnectMaxAttempts = 12
	// acpGatewayConnectBaseDelaySec 首次重试延迟（Python _ACP_GATEWAY_CONNECT_BASE_DELAY_SEC）
	acpGatewayConnectBaseDelaySec = 0.15
	// acpGatewayConnectMaxDelaySec 重试延迟封顶
	acpGatewayConnectMaxDelaySec = 2.0
	// acpChannelName 默认渠道名
	acpChannelName = "acp"
	// acpDefaultSessionID 默认会话
	acpDefaultSessionID = "acp_cli_session"
	// acpConverter provenance converter 串（§9-9 Go 包路径）
	acpConverter = "gateway/channel_manager/protocol/acp:AcpChannel"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// acpStdout 进程原始 stdout 早捕获（Python _ACP_STDOUT = sys.__stdout__，§9-8）。
// cmd 层重定向 os.Stdout 不影响本句柄。
var acpStdout io.Writer = os.Stdout

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAcpChannel 构造 ACP 通道。
// Python: AcpChannel.__init__ (L673-691)。
func NewAcpChannel(cfg AcpChannelConfig) *AcpChannel {
	channelID := strings.TrimSpace(cfg.ChannelID)
	if channelID == "" {
		channelID = acpChannelName
	}
	sid := strings.TrimSpace(cfg.DefaultSessionID)
	if sid == "" {
		sid = acpDefaultSessionID
	}
	return &AcpChannel{
		cfg:                   cfg,
		stdin:                 os.Stdin,
		stdout:                acpStdout,
		requestCtx:            make(map[string]*acpRequestCtx),
		sessionCtx:            make(map[string]map[string]any),
		activePromptBySession: make(map[string]string),
		knownSessions:         make(map[string]struct{}),
		pendingRPCSessionByID: make(map[string]acpPendingEntry),
		stdinEOFGraceSecs:     acpStdinEOFGraceSeconds,
		idleFinalizeSecs:      acpPromptIdleFinalizeSeconds,
		_channelID:            channelID,
		_defaultSessionID:     sid,
	}
}

// ChannelID 渠道标识（Python channel_id property L693-695）。
func (ch *AcpChannel) ChannelID() string {
	if ch._channelID != "" {
		return ch._channelID
	}
	return acpChannelName
}

// Start 启动：确保网关连接（有 URL 时）+ 阻塞读 stdin。
// Python: start (L700-742)。
func (ch *AcpChannel) Start(ctx context.Context) error {
	ch.runMu.Lock()
	if ch.running {
		ch.runMu.Unlock()
		return nil
	}
	ch.running = true
	ch.runMu.Unlock()
	defer func() {
		ch.runMu.Lock()
		ch.running = false
		ch.runMu.Unlock()
	}()

	if ch.onMessageCb == nil && ch.gatewayURL != "" {
		if err := ch.ensureGatewayConnection(); err != nil {
			return err
		}
	}

	reader := bufio.NewReader(ch.stdin)
	stdinEOF := false
	var stdinEOFSince float64
	for {
		ch.runMu.Lock()
		running := ch.running
		ch.runMu.Unlock()
		if !running {
			break
		}
		if stdinEOF {
			ch.mu.Lock()
			hasPending := len(ch.requestCtx) > 0 || len(ch.pendingRPCSessionByID) > 0
			ch.mu.Unlock()
			if !hasPending {
				if nowFloat()-stdinEOFSince >= ch.stdinEOFGraceSecs {
					break
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		// Python readline 阻塞；ctx 取消时无法中断阻塞读——接受此限制
		//（测试用 fakeStdin 即时 EOF；生产 IDE 关 stdin 即退出）
		line, err := reader.ReadString('\n')
		if err != nil {
			stdinEOF = true
			stdinEOFSince = nowFloat()
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := ch.handleRawLine(ctx, line); err != nil {
			logger.Error(logger.ComponentGateway).
				Err(err).
				Msg("[ACP] stdio inbound failed")
			prov := ch.provenance("stdio_error")
			ch.writeE2AResponse(&e2a.E2AResponse{
				ResponseID:   "acp-err-" + randHexStr(8),
				Status:       e2a.E2AResponseStatusFailed,
				ResponseKind: e2a.E2AResponseKindACPJSONRPCError,
				IsFinal:      true,
				Timestamp:    e2a.UTCNowISO(),
				Provenance:   *prov,
				Channel:      ch.ChannelID(),
				Body:         map[string]any{"code": -32603, "message": err.Error()},
			})
		}
	}
	return nil
}

// Stop 停止：清 ctx + pending + 关网关连接。
// Python: stop (L744-749)。
func (ch *AcpChannel) Stop() {
	ch.runMu.Lock()
	ch.running = false
	ch.runMu.Unlock()

	ch.mu.Lock()
	ids := make([]string, 0, len(ch.requestCtx))
	for id := range ch.requestCtx {
		ids = append(ids, id)
	}
	ch.mu.Unlock()
	for _, id := range ids {
		ch.clearRequestContext(id)
	}

	ch.mu.Lock()
	ch.pendingRPCSessionByID = make(map[string]acpPendingEntry)
	ch.mu.Unlock()

	ch.closeGatewayConnection()
}

// Send 出站消息（双模式分派）。
// Python: send (L768-785)。
func (ch *AcpChannel) Send(ctx context.Context, msg *schema.Message) {
	ch.mu.Lock()
	ctxObj := ch.requestCtx[msg.ID]
	ch.mu.Unlock()
	if ctxObj == nil {
		logger.Debug(logger.ComponentGateway).
			Str("id", msg.ID).
			Msg("[ACP] skip outbound without request context")
		return
	}

	if ctxObj.ResponseMode == "jsonrpc" {
		isFinal := ch.sendJsonrpcMessage(ctx, msg, ctxObj)
		if isFinal {
			ch.clearRequestContext(msg.ID)
		}
		return
	}

	response := ch.messageToE2aResponse(msg, ctxObj)
	if response == nil {
		return
	}
	ch.writeE2AResponse(response)
	if response.IsFinal {
		ch.clearRequestContext(msg.ID)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleRawLine 入站三路由：JSON-RPC request / JSON-RPC response / E2A envelope。
// Python: _handle_raw_line (L787-805)。
func (ch *AcpChannel) handleRawLine(ctx context.Context, line string) error {
	var data map[string]any
	if err := json.Unmarshal([]byte(line), &data); err != nil {
		return fmt.Errorf("invalid json line: %w", err)
	}
	if isAcpJsonrpcRequest(data) {
		ch.handleJsonrpcRequest(ctx, data)
		return nil
	}
	if isAcpJsonrpcResponse(data) {
		ch.handleJsonrpcResponse(ctx, data)
		return nil
	}
	env := ch.parseEnvelope(data)
	msg, err := ch.envelopeToMessage(env)
	if err != nil {
		return err
	}
	ch.mu.Lock()
	ch.requestCtx[msg.ID] = &acpRequestCtx{
		JSONRPCID:    env.JSONRPCID,
		Method:       env.Method,
		ResponseMode: "e2a",
		SessionID:    msg.SessionID,
	}
	ch.mu.Unlock()
	ch.dispatchMessage(ctx, msg)
	return nil
}

// parseEnvelope 解析 E2A envelope（request_id/channel 兜底）。
// Python: _parse_envelope (L832-838)。
func (ch *AcpChannel) parseEnvelope(data map[string]any) *e2a.E2AEnvelope {
	env := e2a.EnvelopeFromMap(cpStrAnyMap(data))
	if env.RequestID == "" {
		env.RequestID = "acp_" + randHexStr(12)
	}
	if env.Channel == "" {
		env.Channel = ch.ChannelID()
	}
	return env
}

// handleJsonrpcRequest 六方法分派（响应写 stdout）。
// Python: _handle_jsonrpc_request (L1050-1086)。
func (ch *AcpChannel) handleJsonrpcRequest(ctx context.Context, data map[string]any) {
	rpcID := data["id"]
	method := strings.TrimSpace(strVal(data["method"]))
	params, _ := data["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logger.ComponentGateway).
				Any("panic", r).
				Str("method", method).
				Msg("[ACP] jsonrpc request panicked")
			ch.writeJsonrpcError(rpcID, -32603, fmt.Sprintf("%v", r))
		}
	}()
	if err := ch.dispatchJsonrpcMethod(ctx, rpcID, method, params); err != nil {
		if isValueError(err) {
			ch.writeJsonrpcError(rpcID, -32602, err.Error())
			return
		}
		logger.Error(logger.ComponentGateway).
			Err(err).
			Str("method", method).
			Msg("[ACP] jsonrpc request failed")
		ch.writeJsonrpcError(rpcID, -32603, err.Error())
	}
}

// dispatchJsonrpcMethod 六方法体（Python L1055-1081 的 if 链）。
func (ch *AcpChannel) dispatchJsonrpcMethod(ctx context.Context, rpcID any, method string, params map[string]any) error {
	switch method {
	case "initialize":
		ch.writeJsonrpcResult(rpcID, e2aacp.BuildAcpInitializeResult())
		ch.notifyAgentInitialize(ctx, params)
		return nil
	case "session/new":
		return ch.handleJsonrpcSessionNew(rpcID, params)
	case "session/prompt":
		return ch.handleJsonrpcSessionPrompt(ctx, rpcID, params)
	case "session/cancel":
		return ch.handleJsonrpcSessionCancel(ctx, rpcID, params)
	case "session/list":
		ch.mu.Lock()
		sids := make([]string, 0, len(ch.knownSessions))
		for sid := range ch.knownSessions {
			sids = append(sids, sid)
		}
		ch.mu.Unlock()
		sort.Strings(sids)
		ch.writeJsonrpcResult(rpcID, e2aacp.BuildAcpSessionListResult(sids))
		return nil
	case "session/load":
		ch.writeJsonrpcError(rpcID, -32601,
			"Method not supported by agent capabilities: session/load")
		return nil
	default:
		ch.writeJsonrpcError(rpcID, -32601, "Method not found: "+method)
		return nil
	}
}

// handleJsonrpcSessionNew Python L1109-1120。
func (ch *AcpChannel) handleJsonrpcSessionNew(rpcID any, params map[string]any) error {
	sessionID := strings.TrimSpace(strVal(params["sessionId"]))
	if sessionID == "" {
		sessionID = "acp_" + randHexStr(12)
	}
	ch.mu.Lock()
	ch.knownSessions[sessionID] = struct{}{}
	ch.sessionCtx[sessionID] = cpStrAnyMap(params)
	ch.mu.Unlock()
	ch.writeJsonrpcResult(rpcID, e2aacp.BuildAcpSessionNewResult(sessionID))
	return nil
}

// handleJsonrpcSessionPrompt Python L1122-1164。
func (ch *AcpChannel) handleJsonrpcSessionPrompt(ctx context.Context, rpcID any, params map[string]any) error {
	sessionID := strings.TrimSpace(strVal(params["sessionId"]))
	if sessionID == "" {
		return acpValueError("sessionId is required")
	}
	ch.mu.Lock()
	ch.knownSessions[sessionID] = struct{}{}
	ch.mu.Unlock()

	rpcParams := cpStrAnyMap(params)
	prompt, ok := rpcParams["prompt"].([]any)
	if !ok || len(prompt) == 0 {
		text := chExtractPromptText(rpcParams)
		if text == "" {
			return acpValueError("prompt is required")
		}
		rpcParams["prompt"] = []any{map[string]any{"type": "text", "text": text}}
	}
	rpcParams["session_id"] = sessionID
	ch.mu.Lock()
	sessionCtxMap := ch.sessionCtx[sessionID]
	ch.mu.Unlock()
	for k, v := range sessionCtxMap { // Python setdefault：已存在不覆盖
		if _, exists := rpcParams[k]; !exists {
			rpcParams[k] = v
		}
	}

	env := e2a.EnvelopeFromACPJsonrpc("session/prompt", rpcParams, &e2a.EnvelopeFromACPOptions{
		JSONRPCID: rpcID,
		SessionID: sessionID,
		Channel:   ch.ChannelID(),
	})
	env.RequestID = "acp_" + randHexStr(12)
	env.IsStream = true

	msg, err := ch.envelopeToMessage(env)
	if err != nil {
		return err
	}
	ch.mu.Lock()
	ch.requestCtx[msg.ID] = &acpRequestCtx{
		JSONRPCID:     rpcID,
		Method:        env.Method,
		ResponseMode:  "jsonrpc",
		SessionID:     sessionID,
		UserMessageID: strings.TrimSpace(strVal(rpcParams["messageId"])),
	}
	ch.activePromptBySession[sessionID] = msg.ID
	ch.mu.Unlock()
	ch.dispatchMessage(ctx, msg)
	return nil
}

// handleJsonrpcSessionCancel Python L1166-1189。
func (ch *AcpChannel) handleJsonrpcSessionCancel(ctx context.Context, rpcID any, params map[string]any) error {
	sessionID := strings.TrimSpace(strVal(params["sessionId"]))
	if sessionID == "" {
		return acpValueError("sessionId is required")
	}
	paramsJSON, _ := json.Marshal(map[string]any{"session_id": sessionID})
	msg := &schema.Message{
		ID: "acp_cancel_" + randHexStr(12), Type: schema.MessageTypeReq,
		ChannelID: ch.ChannelID(), SessionID: sessionID,
		Params:    paramsJSON,
		Timestamp: nowFloat(), OK: true,
		ReqMethod: schema.ReqMethodChatCancel,
		Metadata:  map[string]any{"acp": map[string]any{"jsonrpc_id": rpcID, "method": "session/cancel"}},
	}
	ch.dispatchMessage(ctx, msg)
	ch.finalizeSessionPrompts(sessionID, "cancelled")
	ch.writeJsonrpcResult(rpcID, nil)
	return nil
}

// handleJsonrpcResponse 工具响应回流（Python L807-830）。
func (ch *AcpChannel) handleJsonrpcResponse(ctx context.Context, data map[string]any) {
	jsonrpcID := strings.TrimSpace(strVal(data["id"]))
	if jsonrpcID == "" {
		return
	}
	ch.mu.Lock()
	pending, ok := ch.pendingRPCSessionByID[jsonrpcID]
	if ok {
		delete(ch.pendingRPCSessionByID, jsonrpcID)
	}
	ch.mu.Unlock()
	sessionID := ""
	if ok {
		sessionID = strings.TrimSpace(pending.sessionID)
	}
	if sessionID == "" {
		logger.Info(logger.ComponentGateway).
			Str("jsonrpc_id", jsonrpcID).
			Msg("[ACP] ignoring unknown/late stdio jsonrpc response")
		return
	}
	msg := e2a.BuildACPToolResponseMessage(jsonrpcID, cpStrAnyMap(data), sessionID, ch.ChannelID())
	ch.dispatchMessage(ctx, msg)
	ch.maybeFinalizeDeferredPrompts(sessionID)
}

// envelopeToMessage E2A envelope → 内部 Message。
// Python: _envelope_to_message (L840-878)。
func (ch *AcpChannel) envelopeToMessage(env *e2a.E2AEnvelope) (*schema.Message, error) {
	method := strings.TrimSpace(env.Method)
	params := cpStrAnyMap(env.Params)
	sessionID := env.SessionID
	if sessionID == "" {
		if v, ok := params["session_id"].(string); ok && v != "" {
			sessionID = v
		}
	}
	if sessionID == "" {
		sessionID = ch._defaultSessionID
	}

	var reqMethod schema.ReqMethod
	if parsed, err := schema.ParseReqMethod(method); err == nil {
		reqMethod = parsed
	}
	if method == "session/prompt" {
		text := chExtractPromptText(params)
		if _, exists := params["content"]; !exists {
			params["content"] = text
		}
		if _, exists := params["query"]; !exists {
			params["query"] = text
		}
		reqMethod = schema.ReqMethodChatSend
	}
	if reqMethod == "" {
		return nil, acpValueError(fmt.Sprintf("unsupported ACP/E2A method: %s", orStr(method, "<empty>")))
	}
	if reqMethod == schema.ReqMethodChatSend {
		if _, exists := params["query"]; !exists {
			if c, ok := params["content"].(string); ok {
				params["query"] = c
			} else {
				params["query"] = ""
			}
		}
	}

	acpMeta := map[string]any{"jsonrpc_id": env.JSONRPCID, "method": method}
	for k, v := range ch.cfg.Metadata {
		acpMeta[k] = v
	}
	paramsJSON, _ := json.Marshal(params)
	return &schema.Message{
		ID: env.RequestID, Type: schema.MessageTypeReq,
		ChannelID: ch.ChannelID(), SessionID: sessionID,
		Params: paramsJSON, Timestamp: nowFloat(), OK: true,
		ReqMethod:      reqMethod,
		IsStream:       env.IsStream || reqMethod == schema.ReqMethodChatSend,
		EnableStreaming: true,
		Metadata:       map[string]any{"acp": acpMeta},
	}, nil
}

// messageToE2aResponse E2A 模式出站转换（Python L880-1016 五分支）。
// 五分支：CHAT_DELTA → e2a.chunk；CHAT_ERROR → acp.jsonrpc_error(final)；
// CHAT_FINAL → acp.prompt_result(final)；event 兜底 → acp.session_update；
// res ok 非 accepted → prompt_result(final)；其余 → jsonrpc_error(final)。
func (ch *AcpChannel) messageToE2aResponse(msg *schema.Message, c *acpRequestCtx) *e2a.E2AResponse {
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	ts := e2a.UTCNowISO()
	sequence := c.Sequence

	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatDelta {
		c.Sequence++
		deltaKind := "text"
		if payload["source_chunk_type"] == "llm_reasoning" {
			deltaKind = "reasoning"
		}
		return &e2a.E2AResponse{
			ResponseID:   msg.ID + ":" + fmt.Sprintf("%d", sequence),
			RequestID:    msg.ID,
			JSONRPCID:    c.JSONRPCID,
			Sequence:     sequence,
			Status:       e2a.E2AResponseStatusInProgress,
			ResponseKind: e2a.E2AResponseKindE2AChunk,
			Timestamp:    ts,
			Provenance:   *ch.provenance("chat.delta"),
			Channel:      ch.ChannelID(),
			SessionID:    msg.SessionID,
			IsStream:     true,
			Body: map[string]any{
				"event_type":  "chat.delta",
				"delta_kind":  deltaKind,
				"delta":       strVal(payload["content"]),
				"payload":     payload,
			},
		}
	}

	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatError {
		c.Sequence++
		errText := strVal(payload["error"])
		if errText == "" {
			errText = strVal(payload["content"])
		}
		if errText == "" {
			errText = "Agent error"
		}
		return &e2a.E2AResponse{
			ResponseID:   msg.ID + ":" + fmt.Sprintf("%d", sequence),
			RequestID:    msg.ID,
			JSONRPCID:    c.JSONRPCID,
			Sequence:     sequence,
			IsFinal:      true,
			Status:       e2a.E2AResponseStatusFailed,
			ResponseKind: e2a.E2AResponseKindACPJSONRPCError,
			Timestamp:    ts,
			Provenance:   *ch.provenance("chat.error"),
			Channel:      ch.ChannelID(),
			SessionID:    msg.SessionID,
			Body:         map[string]any{"code": -32603, "message": errText},
		}
	}

	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatFinal {
		c.Sequence++
		resultBody := cpStrAnyMap(payload)
		if _, has := resultBody["session_id"]; !has {
			resultBody["session_id"] = msg.SessionID
		}
		return &e2a.E2AResponse{
			ResponseID:   msg.ID + ":" + fmt.Sprintf("%d", sequence),
			RequestID:    msg.ID,
			JSONRPCID:    c.JSONRPCID,
			Sequence:     sequence,
			IsFinal:      true,
			Status:       e2a.E2AResponseStatusSucceeded,
			ResponseKind: e2a.E2AResponseKindACPPromptResult,
			Timestamp:    ts,
			Provenance:   *ch.provenance("chat.final"),
			Channel:      ch.ChannelID(),
			SessionID:    msg.SessionID,
			Body:         resultBody,
		}
	}

	if msg.Type == schema.MessageTypeEvent {
		update := e2aacp.BuildAcpSessionUpdate(msg, payload, &c.State)
		if update != nil {
			c.Sequence++
			provKind := "session.update"
			if msg.EventType != "" {
				provKind = msg.EventType.String()
			}
			sid := msg.SessionID
			if sid == "" {
				sid = c.SessionID
			}
			return &e2a.E2AResponse{
				ResponseID:   msg.ID + ":" + fmt.Sprintf("%d", sequence),
				RequestID:    msg.ID,
				JSONRPCID:    c.JSONRPCID,
				Sequence:     sequence,
				Status:       e2a.E2AResponseStatusInProgress,
				ResponseKind: e2a.E2AResponseKindACPSessionUpdate,
				Timestamp:    ts,
				Provenance:   *ch.provenance(provKind),
				Channel:      ch.ChannelID(),
				SessionID:    msg.SessionID,
				IsStream:     true,
				Body: map[string]any{
					"sessionId": sid,
					"update":    update,
				},
			}
		}
		// 辅助事件先忽略（Python L997-999：避免把中间态误判为最终失败）
		return nil
	}

	if msg.Type == schema.MessageTypeRes && msg.OK {
		if accepted, _ := payload["accepted"].(bool); accepted {
			return nil
		}
		c.Sequence++
		resultBody := cpStrAnyMap(payload)
		if _, has := resultBody["session_id"]; !has {
			resultBody["session_id"] = msg.SessionID
		}
		return &e2a.E2AResponse{
			ResponseID:   msg.ID + ":" + fmt.Sprintf("%d", sequence),
			RequestID:    msg.ID,
			JSONRPCID:    c.JSONRPCID,
			Sequence:     sequence,
			IsFinal:      true,
			Status:       e2a.E2AResponseStatusSucceeded,
			ResponseKind: e2a.E2AResponseKindACPPromptResult,
			Timestamp:    ts,
			Provenance:   *ch.provenance("response.ok"),
			Channel:      ch.ChannelID(),
			SessionID:    msg.SessionID,
			Body:         resultBody,
		}
	}

	c.Sequence++
	errText := strVal(payload["error"])
	if errText == "" {
		errText = strVal(payload["content"])
	}
	if errText == "" {
		errText = "request failed"
	}
	return &e2a.E2AResponse{
		ResponseID:   msg.ID + ":" + fmt.Sprintf("%d", sequence),
		RequestID:    msg.ID,
		JSONRPCID:    c.JSONRPCID,
		Sequence:     sequence,
		IsFinal:      true,
		Status:       e2a.E2AResponseStatusFailed,
		ResponseKind: e2a.E2AResponseKindACPJSONRPCError,
		Timestamp:    ts,
		Provenance:   *ch.provenance("response.error"),
		Channel:      ch.ChannelID(),
		SessionID:    msg.SessionID,
		Body:         map[string]any{"code": -32603, "message": errText},
	}
}

// writeE2AResponse 写 E2AResponse 帧（L1018-1021）。
func (ch *AcpChannel) writeE2AResponse(resp *e2a.E2AResponse) {
	ch.writeLine(mustJSONBytes(resp.ToMap()))
}

// writeJsonrpcResult 写 JSON-RPC result 帧（L1023-1026）。
func (ch *AcpChannel) writeJsonrpcResult(rpcID any, result any) {
	ch.writeLine(mustJSONBytes(map[string]any{"jsonrpc": "2.0", "id": rpcID, "result": result}))
}

// writeJsonrpcError 写 JSON-RPC error 帧（L1028-1043）。
func (ch *AcpChannel) writeJsonrpcError(rpcID any, code int, message string) {
	ch.writeLine(mustJSONBytes(map[string]any{
		"jsonrpc": "2.0", "id": rpcID,
		"error": map[string]any{"code": code, "message": message},
	}))
}

// writeJsonrpcNotification 写 JSON-RPC notification 帧（L1045-1048）。
func (ch *AcpChannel) writeJsonrpcNotification(method string, params map[string]any) {
	ch.writeLine(mustJSONBytes(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}))
}

// writeAcpSessionUpdate 写 session/update 通知（L1456-1463）。
func (ch *AcpChannel) writeAcpSessionUpdate(sessionID string, update map[string]any) {
	ch.writeJsonrpcNotification("session/update", map[string]any{
		"sessionId": sessionID,
		"update":    update,
	})
}

// writeLine 写一行（stdoutMu 互斥，§9-2）。
func (ch *AcpChannel) writeLine(data []byte) {
	ch.stdoutMu.Lock()
	defer ch.stdoutMu.Unlock()
	_, _ = ch.stdout.Write(append(data, '\n'))
}

// isAcpJsonrpcRequest 判定 JSON-RPC 请求（L1474-1480）。
func isAcpJsonrpcRequest(data map[string]any) bool {
	if v, _ := data["jsonrpc"].(string); v != "2.0" {
		return false
	}
	_, ok := data["method"].(string)
	return ok
}

// isAcpJsonrpcResponse 判定 JSON-RPC 响应（L1482-1490）。
func isAcpJsonrpcResponse(data map[string]any) bool {
	if v, _ := data["jsonrpc"].(string); v != "2.0" {
		return false
	}
	if _, ok := data["id"]; !ok {
		return false
	}
	if _, isStr := data["method"].(string); isStr {
		return false
	}
	_, hasResult := data["result"]
	_, hasError := data["error"]
	return hasResult || hasError
}

// chExtractPromptText 通道版键序：content → query → text（L1492-1508）。
// ⚠️ 与桥版 ExtractPromptText（text→content→query）不同，勿混用。
func chExtractPromptText(params map[string]any) string {
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
	for _, key := range []string{"content", "query", "text"} {
		if v, ok := params[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// notifyAgentInitialize initialize 透传（L1088-1104）。
func (ch *AcpChannel) notifyAgentInitialize(ctx context.Context, params map[string]any) {
	msg := &schema.Message{
		ID: "acp_init_" + randHexStr(12), Type: schema.MessageTypeReq,
		ChannelID: ch.ChannelID(), SessionID: ch._defaultSessionID,
		Params:    mustJSONBytes(params),
		Timestamp: nowFloat(), OK: true,
		ReqMethod: schema.ReqMethodInitialize,
		Metadata:  map[string]any{"acp": map[string]any{"method": "initialize"}},
	}
	if msg.Params == nil {
		msg.Params = []byte(`{}`)
	}
	ch.dispatchMessage(ctx, msg) // 异常仅吞（Python try/except debug）
}

// dispatchMessage 派发（§9-1：onMessageCb 测试钩子 or sendToGateway 单路径）。
// Python: _dispatch_message (L1191-1205)。
func (ch *AcpChannel) dispatchMessage(ctx context.Context, msg *schema.Message) {
	if ch.onMessageCb != nil {
		ch.onMessageCb(msg)
		return
	}
	if ch.gatewayURL != "" {
		_ = ch.sendToGateway(ctx, msg)
	}
	// 无 cb 无 URL：丢弃（Python bus publish 死代码不实现，§9-1）
}

// clearRequestContext Python L1207-1220。
func (ch *AcpChannel) clearRequestContext(requestID string) {
	ch.mu.Lock()
	ctxObj := ch.requestCtx[requestID]
	delete(ch.requestCtx, requestID)
	if ctxObj != nil && ctxObj.SessionID != "" &&
		ch.activePromptBySession[ctxObj.SessionID] == requestID {
		delete(ch.activePromptBySession, ctxObj.SessionID)
	}
	if ctxObj != nil && ctxObj.idleFinalizeTimerSlot != nil {
		if ctxObj.idleFinalizeTimerSlot.timer != nil {
			ctxObj.idleFinalizeTimerSlot.timer.Stop()
		}
		ctxObj.idleFinalizeTimerSlot = nil
	}
	ch.mu.Unlock()
}

// sessionHasPendingClientRpcs Python L1222-1229（锁外调用版）。
func (ch *AcpChannel) sessionHasPendingClientRpcs(sessionID string) bool {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.sessionHasPendingClientRpcsLocked(sid)
}

// sessionHasPendingClientRpcs 锁内版（sendJsonrpcMessage 持 ch.mu 段调用；
// Go sync.Mutex 不可重入——Python 无锁语义可直接调，Go 侧拆两版）。
func (ch *AcpChannel) sessionHasPendingClientRpcsLocked(sid string) bool {
	for _, entry := range ch.pendingRPCSessionByID {
		if strings.TrimSpace(entry.sessionID) == sid {
			return true
		}
	}
	return false
}

// collectFinalizableRequestIDs Python L1231-1241。
func (ch *AcpChannel) collectFinalizableRequestIDs(sessionID string) []string {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	result := []string{}
	for requestID, c := range ch.requestCtx {
		if c.ResponseMode != "jsonrpc" || c.SessionID != sessionID {
			continue
		}
		if strings.TrimSpace(c.PendingStopReason) == "" {
			continue
		}
		c.matchedRequestID = requestID
		result = append(result, requestID)
	}
	return result
}

// maybeFinalizeDeferredPrompts Python L1243-1268。
func (ch *AcpChannel) maybeFinalizeDeferredPrompts(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || ch.sessionHasPendingClientRpcs(sid) {
		return
	}
	for _, requestID := range ch.collectFinalizableRequestIDs(sid) {
		ch.mu.Lock()
		c := ch.requestCtx[requestID]
		if c == nil {
			ch.mu.Unlock()
			continue
		}
		stopReason := strings.TrimSpace(c.PendingStopReason)
		if stopReason == "" {
			ch.mu.Unlock()
			continue
		}
		c.PendingStopReason = ""
		shouldWait := acpShouldWaitForFinalText(c)
		ch.mu.Unlock()
		if shouldWait {
			ch.scheduleIdleFinalize(requestID)
			continue
		}
		ch.cancelIdleFinalize(requestID)
		ch.mu.Lock()
		jsonrpcID := c.JSONRPCID
		userMessageID := c.UserMessageID
		ch.mu.Unlock()
		ch.writeJsonrpcResult(jsonrpcID, e2aacp.BuildAcpPromptResult(stopReason, userMessageID))
		ch.clearRequestContext(requestID)
	}
}

// acpIdleTimerSlot timer 槽（§9-4：AfterFunc 回调先于闭包变量赋值启动的竞态，
// 槽在锁内先行创建并写入 ctx 字段，回调经槽引用比对归属）。
type acpIdleTimerSlot struct {
	timer *time.Timer
}

// scheduleIdleFinalize Python L1270-1278（timer 引用比对，§9-4 槽模式）。
func (ch *AcpChannel) scheduleIdleFinalize(requestID string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	c := ch.requestCtx[requestID]
	if c == nil {
		return
	}
	if c.idleFinalizeTimerSlot != nil && c.idleFinalizeTimerSlot.timer != nil {
		c.idleFinalizeTimerSlot.timer.Stop()
	}
	slot := &acpIdleTimerSlot{}
	c.idleFinalizeTimerSlot = slot
	slot.timer = time.AfterFunc(time.Duration(ch.idleFinalizeSecs*float64(time.Second)), func() {
		ch.idleFinalizeAfterTimeout(requestID, slot)
	})
}

// idleFinalizeAfterTimeout Python L1280-1304（槽引用比对）。
func (ch *AcpChannel) idleFinalizeAfterTimeout(requestID string, slot *acpIdleTimerSlot) {
	ch.mu.Lock()
	c := ch.requestCtx[requestID]
	if c == nil || c.ResponseMode != "jsonrpc" || c.idleFinalizeTimerSlot != slot {
		ch.mu.Unlock()
		return
	}
	if sid := strings.TrimSpace(c.SessionID); sid != "" && ch.sessionHasPendingClientRpcsLocked(sid) {
		c.PendingStopReason = "end_turn"
		c.idleFinalizeTimerSlot = nil
		ch.mu.Unlock()
		return
	}
	jsonrpcID := c.JSONRPCID
	userMessageID := c.UserMessageID
	ch.mu.Unlock()
	ch.writeJsonrpcResult(jsonrpcID, e2aacp.BuildAcpPromptResult("end_turn", userMessageID))
	ch.clearRequestContext(requestID)
}

// finalizeSessionPrompts Python L1306-1323。
func (ch *AcpChannel) finalizeSessionPrompts(sessionID, stopReason string) {
	ch.mu.Lock()
	matched := []string{}
	for requestID, c := range ch.requestCtx {
		if c.ResponseMode == "jsonrpc" && c.SessionID == sessionID {
			matched = append(matched, requestID)
		}
	}
	ch.mu.Unlock()
	for _, requestID := range matched {
		ch.mu.Lock()
		c := ch.requestCtx[requestID]
		if c == nil {
			ch.mu.Unlock()
			continue
		}
		jsonrpcID := c.JSONRPCID
		userMessageID := c.UserMessageID
		ch.mu.Unlock()
		ch.writeJsonrpcResult(jsonrpcID, e2aacp.BuildAcpPromptResult(stopReason, userMessageID))
		ch.clearRequestContext(requestID)
	}
}

// sendJsonrpcMessage jsonrpc 模式出站（Python _send_jsonrpc_message L1325-1454）。
// 返回 true = final（调用方清 ctx）。
// 锁纪律：c 可变字段的读写持 ch.mu（timer 回调并发）；写帧在锁外（stdoutMu 自守）。
func (ch *AcpChannel) sendJsonrpcMessage(_ context.Context, msg *schema.Message, c *acpRequestCtx) bool {
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	ch.mu.Lock()
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = c.SessionID
	}

	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatDelta {
		text := strVal(payload["content"])
		if text == "" {
			ch.mu.Unlock()
			return false
		}
		update := e2aacp.BuildAcpSessionUpdate(msg, payload, &c.State)
		if update == nil {
			ch.mu.Unlock()
			return false
		}
		acpMarkStreamActivity(c)
		ch.mu.Unlock()
		ch.writeAcpSessionUpdate(sessionID, update)
		if usage := e2aacp.BuildAcpUsageUpdate(payload); usage != nil {
			ch.writeAcpSessionUpdate(sessionID, usage)
		}
		return false
	}

	if msg.Type == schema.MessageTypeEvent && (msg.EventType == schema.EventTypeChatReasoning ||
		msg.EventType == schema.EventTypeChatToolCall || msg.EventType == schema.EventTypeChatToolUpdate ||
		msg.EventType == schema.EventTypeChatToolResult || msg.EventType == schema.EventTypeTodoUpdated ||
		msg.EventType == schema.EventTypeChatSubtaskUpdate) {
		update := e2aacp.BuildAcpSessionUpdate(msg, payload, &c.State)
		if update == nil {
			ch.mu.Unlock()
			return false
		}
		acpMarkStreamActivity(c)
		ch.mu.Unlock()
		ch.writeAcpSessionUpdate(sessionID, update)
		return false
	}

	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatFinal {
		c.SawChatFinal = true
		update := e2aacp.BuildAcpFinalTextUpdate(payload, &c.State)
		sawIdle := c.SawProcessingIdle
		hasPending := ch.sessionHasPendingClientRpcsLocked(sessionID)
		jsonrpcID := c.JSONRPCID
		userMessageID := c.UserMessageID
		ch.mu.Unlock()
		if update != nil {
			ch.writeAcpSessionUpdate(sessionID, update)
		}
		if usage := e2aacp.BuildAcpUsageUpdate(payload); usage != nil {
			ch.writeAcpSessionUpdate(sessionID, usage)
		}
		if sawIdle {
			if hasPending {
				ch.mu.Lock()
				c.PendingStopReason = "end_turn"
				ch.mu.Unlock()
				return false
			}
			ch.cancelIdleFinalize(msg.ID)
			ch.writeJsonrpcResult(jsonrpcID, e2aacp.BuildAcpPromptResult("end_turn", userMessageID))
			return true
		}
		ch.cancelIdleFinalize(msg.ID)
		return false
	}

	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatError {
		jsonrpcID := c.JSONRPCID
		ch.mu.Unlock()
		ch.cancelIdleFinalize(msg.ID)
		errText := strVal(payload["error"])
		if errText == "" {
			errText = strVal(payload["content"])
		}
		if errText == "" {
			errText = "Agent error"
		}
		ch.writeJsonrpcError(jsonrpcID, -32603, errText)
		return true
	}

	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatInterruptResult {
		jsonrpcID := c.JSONRPCID
		userMessageID := c.UserMessageID
		ch.mu.Unlock()
		ch.cancelIdleFinalize(msg.ID)
		ch.writeJsonrpcResult(jsonrpcID, e2aacp.BuildAcpPromptResult("cancelled", userMessageID))
		return true
	}

	if msg.Type == schema.MessageTypeEvent {
		if msg.EventType == schema.EventTypeChatProcessingStatus {
			update := e2aacp.BuildAcpSessionUpdate(msg, payload, &c.State)
			isProc, isProcSet := payload["is_processing"].(bool)
			var finishNow, scheduleWait bool
			if isProcSet && !isProc {
				c.SawProcessingIdle = true
				if ch.sessionHasPendingClientRpcsLocked(sessionID) {
					c.PendingStopReason = "end_turn"
				} else if acpShouldWaitForFinalText(c) {
					scheduleWait = true
				} else {
					if c.idleFinalizeTimerSlot != nil {
						c.idleFinalizeTimerSlot.timer.Stop()
						c.idleFinalizeTimerSlot = nil
					}
					finishNow = true
				}
			} else if isProcSet && isProc {
				c.SawProcessingIdle = false
				if c.idleFinalizeTimerSlot != nil {
					c.idleFinalizeTimerSlot.timer.Stop()
					c.idleFinalizeTimerSlot = nil
				}
			}
			jsonrpcID := c.JSONRPCID
			userMessageID := c.UserMessageID
			ch.mu.Unlock()
			if update != nil {
				ch.writeAcpSessionUpdate(sessionID, update)
			}
			if scheduleWait {
				ch.scheduleIdleFinalize(msg.ID)
				return false
			}
			if finishNow {
				ch.writeJsonrpcResult(jsonrpcID, e2aacp.BuildAcpPromptResult("end_turn", userMessageID))
				return true
			}
			return false
		}
		ch.mu.Unlock()
		return false
	}

	if msg.Type == schema.MessageTypeRes && msg.OK {
		accepted, _ := payload["accepted"].(bool)
		jsonrpcID := c.JSONRPCID
		userMessageID := c.UserMessageID
		ch.mu.Unlock()
		if accepted {
			return false
		}
		ch.cancelIdleFinalize(msg.ID)
		ch.writeJsonrpcResult(jsonrpcID, e2aacp.BuildAcpPromptResult("end_turn", userMessageID))
		return true
	}

	jsonrpcID := c.JSONRPCID
	ch.mu.Unlock()
	errText := strVal(payload["error"])
	if errText == "" {
		errText = strVal(payload["content"])
	}
	if errText == "" {
		errText = "request failed"
	}
	ch.writeJsonrpcError(jsonrpcID, -32603, errText)
	return true
}

// sweepStalePending Python L751-766。
func (ch *AcpChannel) sweepStalePending() {
	now := nowFloat()
	ch.mu.Lock()
	stale := []string{}
	affected := map[string]struct{}{}
	for jsonrpcID, entry := range ch.pendingRPCSessionByID {
		if now-entry.createdAt > acpPendingRPCTimeoutSeconds {
			stale = append(stale, jsonrpcID)
			if sid := strings.TrimSpace(entry.sessionID); sid != "" {
				affected[sid] = struct{}{}
			}
		}
	}
	for _, jsonrpcID := range stale {
		delete(ch.pendingRPCSessionByID, jsonrpcID)
	}
	ch.mu.Unlock()
	for _, jsonrpcID := range stale {
		logger.Info(logger.ComponentGateway).
			Str("jsonrpc_id", jsonrpcID).
			Msg("[ACP] pending RPC entry expired")
	}
	for sid := range affected {
		ch.maybeFinalizeDeferredPrompts(sid)
	}
}

// acpShouldWaitForFinalText Python _should_wait_for_final_text_before_end_turn (L111-118)。
func acpShouldWaitForFinalText(c *acpRequestCtx) bool {
	if c.SawChatFinal {
		return false
	}
	return c.State.AssistantText != "" || c.State.AssistantMessageID != ""
}

// acpMarkStreamActivity Python _mark_stream_activity (L106-109)。
func acpMarkStreamActivity(c *acpRequestCtx) {
	c.SawProcessingIdle = false
	if c.idleFinalizeTimerSlot != nil {
		c.idleFinalizeTimerSlot.timer.Stop()
		c.idleFinalizeTimerSlot = nil
	}
}

// cancelIdleFinalize 按 requestID 取消 timer。
func (ch *AcpChannel) cancelIdleFinalize(requestID string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if c := ch.requestCtx[requestID]; c != nil && c.idleFinalizeTimerSlot != nil {
		c.idleFinalizeTimerSlot.timer.Stop()
		c.idleFinalizeTimerSlot = nil
	}
}

// provenance 构造出处（L1517-1524；converter 串 Go 路径，§9-9）。
func (ch *AcpChannel) provenance(kind string) *e2a.E2AProvenance {
	return &e2a.E2AProvenance{
		SourceProtocol: e2a.E2ASourceProtocolE2A,
		Converter:      acpConverter,
		ConvertedAt:    e2a.UTCNowISO(),
		Details:        map[string]any{"kind": kind},
	}
}

// acpValueErr 参数校验错误（Python ValueError → -32602）。
type acpValueErr struct{ msg string }

// Error 错误消息。
func (e *acpValueErr) Error() string { return e.msg }

// acpValueError 构造 ValueError。
func acpValueError(msg string) error { return &acpValueErr{msg: msg} }

// isValueError 判定 ValueError。
func isValueError(err error) bool {
	_, ok := err.(*acpValueErr)
	return ok
}

// nowFloat 当前时间戳（秒）。
func nowFloat() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// randHexStr 生成 n 字符 hex（uuid4().hex[:12] 等价）。
func randHexStr(n int) string {
	buf := make([]byte, (n+1)/2)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)[:n]
}

// mustJSONBytes 序列化（失败返回空字节数组，防御）。
func mustJSONBytes(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return data
}

// cpStrAnyMap 一层浅拷贝。
func cpStrAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// strVal 任意值转 string（nil/非 string → 空串；bool/数字原样 Python str 语义近似——
// 本包值域内仅 string|nil，取窄实现）。包内本地定义（跨包不可见同名实现）。
func strVal(v any) string {
	s, _ := v.(string)
	return s
}

// orStr 空串回退。
func orStr(s, fallback string) string {
	if strings.TrimSpace(s) != "" {
		return s
	}
	return fallback
}

// ──────────────────────────── WS 客户端族（Task 9 补全，先占方法面） ────────────────────────────

// ensureGatewayConnection WS 连接（重试退避）。Task 9 实现。
func (ch *AcpChannel) ensureGatewayConnection() error {
	// ⤵️ 11.25 Task 9：ensureGatewayConnection + readerLoop + sendToGateway + handleGatewayFrame
	return nil
}

// closeGatewayConnection 关闭 WS 连接。Task 9 实现。
func (ch *AcpChannel) closeGatewayConnection() {
	// ⤵️ 11.25 Task 9
}

// sendToGateway 发送到网关。Task 9 实现。
func (ch *AcpChannel) sendToGateway(ctx context.Context, msg *schema.Message) error {
	// ⤵️ 11.25 Task 9
	_ = ctx
	_ = msg
	return nil
}

// handleGatewayFrame 处理网关帧。Task 9 实现。
func (ch *AcpChannel) handleGatewayFrame(data map[string]any) {
	// ⤵️ 11.25 Task 9
}

// ──────────────────────────── 测试钩子（Python AcpChannelHarness 对应） ────────────────────────────

// SetStdoutForTest 注入 stdout 收集器。
func (ch *AcpChannel) SetStdoutForTest(w io.Writer) { ch.stdout = w }

// SetStdinForTest 注入 stdin 序列。
func (ch *AcpChannel) SetStdinForTest(r io.Reader) { ch.stdin = r }

// SetOnMessageForTest 注入 dispatch 回调（Python on_message）。
func (ch *AcpChannel) SetOnMessageForTest(cb func(msg *schema.Message) bool) { ch.onMessageCb = cb }

// SetGatewayURL 设置网关地址（模式二 / 集成测试）。
func (ch *AcpChannel) SetGatewayURL(url string) { ch.gatewayURL = url }

// SetIdleFinalizeSecondsForTest 缩短 idle finalize（Python monkeypatch）。
func (ch *AcpChannel) SetIdleFinalizeSecondsForTest(secs float64) { ch.idleFinalizeSecs = secs }

// SetStdinEOFGraceSecondsForTest 缩短 EOF 宽限（Python monkeypatch）。
func (ch *AcpChannel) SetStdinEOFGraceSecondsForTest(secs float64) { ch.stdinEOFGraceSecs = secs }

// SeedRequestCtxForTest 预灌请求上下文（Python set_request_context_for_test）。
func (ch *AcpChannel) SeedRequestCtxForTest(requestID string, c *acpRequestCtx) {
	ch.mu.Lock()
	ch.requestCtx[requestID] = c
	ch.mu.Unlock()
}

// SetActivePromptRequestForTest Python set_active_prompt_request_for_test。
func (ch *AcpChannel) SetActivePromptRequestForTest(sessionID, requestID string) {
	ch.mu.Lock()
	ch.activePromptBySession[sessionID] = requestID
	ch.mu.Unlock()
}

// SetPendingRPCSessionForTest Python set_pending_client_rpc_session_for_test。
func (ch *AcpChannel) SetPendingRPCSessionForTest(jsonrpcID, sessionID string) {
	ch.mu.Lock()
	ch.pendingRPCSessionByID[jsonrpcID] = acpPendingEntry{sessionID: sessionID, createdAt: nowFloat()}
	ch.mu.Unlock()
}

// GetPendingRPCSessionForTest Python get_pending_client_rpc_session_for_test。
func (ch *AcpChannel) GetPendingRPCSessionForTest(jsonrpcID string) string {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.pendingRPCSessionByID[jsonrpcID].sessionID
}

// AgePendingRPCForTest 回溯 pending 条目时间（过期清扫用例）。
func (ch *AcpChannel) AgePendingRPCForTest(jsonrpcID string, ageSecs float64) {
	ch.mu.Lock()
	if e, ok := ch.pendingRPCSessionByID[jsonrpcID]; ok {
		e.createdAt = nowFloat() - ageSecs
		ch.pendingRPCSessionByID[jsonrpcID] = e
	}
	ch.mu.Unlock()
}

// HandleGatewayFrameForTest Python handle_gateway_frame_for_test。
func (ch *AcpChannel) HandleGatewayFrameForTest(data map[string]any) {
	ch.handleGatewayFrame(data)
}

// HasRequestContextForTest Python has_request_context_for_test。
func (ch *AcpChannel) HasRequestContextForTest(requestID string) bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	_, ok := ch.requestCtx[requestID]
	return ok
}

// SendJsonrpcMessageForTest Python send_jsonrpc_message_for_test。
func (ch *AcpChannel) SendJsonrpcMessageForTest(msg *schema.Message, c *acpRequestCtx) bool {
	return ch.sendJsonrpcMessage(context.Background(), msg, c)
}

// HandleJsonrpcResponseForTest Python handle_jsonrpc_response_for_test。
func (ch *AcpChannel) HandleJsonrpcResponseForTest(data map[string]any) {
	ch.handleJsonrpcResponse(context.Background(), data)
}

// DefaultAcpChannelConfig 默认配置。
func DefaultAcpChannelConfig() AcpChannelConfig {
	return AcpChannelConfig{
		Enabled:          true,
		ChannelID:        acpChannelName,
		DefaultSessionID: acpDefaultSessionID,
		Metadata:         map[string]any{},
	}
}

// LoadAcpChannelConfig 从 config 读取（Python _load_acp_channel_config L1740-1761）。
// Go 侧 config 无全局单例：reader 注入（偏差 §9-13）。
func LoadAcpChannelConfig(readRaw func() map[string]any) AcpChannelConfig {
	conf := DefaultAcpChannelConfig()
	if readRaw == nil {
		return conf
	}
	full := readRaw()
	if full == nil {
		return conf
	}
	channelsCfg, _ := full["channels"].(map[string]any)
	if channelsCfg == nil {
		return conf
	}
	acpConf, _ := channelsCfg["acp"].(map[string]any)
	if acpConf == nil {
		return conf
	}
	if v, ok := acpConf["enabled"].(bool); ok {
		conf.Enabled = v
	}
	if v := strings.TrimSpace(strVal(acpConf["channel_id"])); v != "" {
		conf.ChannelID = v
	}
	if v := strings.TrimSpace(strVal(acpConf["default_session_id"])); v != "" {
		conf.DefaultSessionID = v
	}
	if m, ok := acpConf["metadata"].(map[string]any); ok {
		conf.Metadata = m
	}
	return conf
}
