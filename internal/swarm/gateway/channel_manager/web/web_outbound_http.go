package web

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// _METHODS_WITHOUT_PARAM_SESSION_ID Agent 端拒绝 params.session_id 的方法
// （恢复走 session.switch）。
// Python: outbound.py:19 _METHODS_WITHOUT_PARAM_SESSION_ID。
var methodsWithoutParamSessionID = map[string]bool{
	"session.create": true,
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sharedUserRouteSessionID 没有客户端会话号的 HTTP 请求按用户共用一条路由会话。
// 企业版服务的并发名额很小；语言配置、项目列表和专家目录如果每次都新开
// webhttp_ 路由键，会把名额占满，随后的 session.create 直接失败。
// Python: outbound.py:21-29 _shared_user_route_session_id。
func sharedUserRouteSessionID(params map[string]any) string {
	user := ""
	if v, ok := params["user_id"].(string); ok {
		user = v
	}
	user = strings.TrimSpace(user)
	cleaned := make([]rune, 0, len(user))
	for _, ch := range user {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			cleaned = append(cleaned, ch)
		} else {
			cleaned = append(cleaned, '_')
		}
	}
	name := string(cleaned)
	if name == "" {
		name = "default"
	}
	return "shared_user_" + name
}

// bindHTTPSession 返回 HTTP 调用的 (envelope_session_id, params)。
// envelope session_id 恒设置（路由 / request outbound）；对 Agent 会拒绝
// params.session_id 的方法不伪造客户端会话号；bind_param=false 时不向 params
// 注入缺失的 session_id（settings / catalog RPC）。
// Python: outbound.py:32-70 bind_http_session（一比一）。
func bindHTTPSession(method string, params map[string]any, headerSessionID string, bindParam bool) (string, map[string]any) {
	out := make(map[string]any, len(params)+1)
	for key, value := range params {
		out[key] = value
	}
	clientSID := ""
	if v, ok := out["session_id"].(string); ok {
		clientSID = strings.TrimSpace(v)
	}
	if clientSID == "" {
		clientSID = strings.TrimSpace(headerSessionID)
	}
	if methodsWithoutParamSessionID[method] {
		delete(out, "session_id")
		if method == "session.create" {
			if token, _ := out["create_token"].(string); strings.TrimSpace(token) == "" {
				out["create_token"] = hexUUID()[:12]
			}
		}
		return "webhttp_" + hexUUID()[:12], out
	}
	// 不往 params 写会话号的调用只需要路由亲和。按用户复用，避免每次请求占一个名额。
	sharedSID := ""
	if clientSID == "" && (strings.HasPrefix(method, "agent_templates.") || !bindParam) {
		sharedSID = sharedUserRouteSessionID(out)
	}
	envelope := clientSID
	if envelope == "" {
		envelope = sharedSID
	}
	if envelope == "" {
		envelope = "webhttp_" + hexUUID()[:12]
	}
	if !bindParam {
		if clientSID != "" {
			out["session_id"] = clientSID
		} else {
			delete(out, "session_id")
		}
		return envelope, out
	}
	if clientSID == "" {
		if _, exists := out["session_id"]; !exists {
			out["session_id"] = envelope
		}
	} else {
		out["session_id"] = clientSID
	}
	return envelope, out
}

// isSSEEndFrame SSE 帧是否终止当前流。
// 个人版语义：chat.final 终止；chat.processing_status(false) 不终止
// （企业版分支恒不触达，偏差 D-B2-5）。
// Python: outbound.py:73-93 _is_sse_end_frame。
func isSSEEndFrame(frame map[string]any, reqID string) bool {
	event, _ := frame["event"].(string)
	if event == "chat.error" {
		return true
	}
	// 企业版 HTTP/SSE 的 chat.final 只结束当前回复段，必须继续转发到
	// processing_status(false)；个人版维持原有 chat.final 终止语义。
	if event == "chat.final" {
		return true // is_enterprise() 恒 false（个人版）
	}
	payload, _ := frame["payload"].(map[string]any)
	if event == "chat.processing_status" {
		if isProcessing, ok := payload["is_processing"].(bool); ok && !isProcessing {
			return false // 个人版分支
		}
	}
	if event == "history.message" {
		status, _ := payload["status"].(string)
		return status == "done"
	}
	return false
}

// hexUUID 生成去连字符的 uuid hex（对齐 Python uuid4().hex）。
func hexUUID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

// normalizeFrame 把帧数据归一为 dict；JSON 字符串/字节解析失败返回 nil。
// Python: outbound.py:96-110 _normalize_frame。
func normalizeFrame(data any) map[string]any {
	switch v := data.(type) {
	case map[string]any:
		return v
	case []byte:
		var parsed map[string]any
		if err := json.Unmarshal(v, &parsed); err != nil {
			return nil
		}
		return parsed
	case string:
		var parsed map[string]any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil
		}
		return parsed
	default:
		return nil
	}
}

// ──────────────────────────── 结构体 ────────────────────────────

// httpOutboundBase JSON / SSE HTTP outbound 的公共字段。
// Python: outbound.py:128-199 _HttpOutboundBase。
type httpOutboundBase struct {
	// isHTTPOutbound 标记请求级 HTTP outbound（对齐 Python is_http_outbound = True）。
	isHTTPOutbound bool
	requestHeaders map[string]string
	remoteAddress  [2]string
	path           string
	sessionID      string

	mu         sync.Mutex
	closedFlag bool
	outboundID string
	// jiuwenWsID 兼容 WebChannel metadata.ws_id / _lookup_peer。
	jiuwenWsID string
	// connectionUserID 请求级解析出的 user_id（Python: setattr
	// _WEB_CONNECTION_USER_ID_ATTR，dispatch.py:143）。
	connectionUserID string
	// appID 请求级 app 标识（Python: setattr _jiuwen_app_id，dispatch.py:144）。
	appID string
	// frames 帧队列（容量 256，对齐 Python asyncio.Queue(maxsize=256)）。
	frames chan map[string]any
}

func (b *httpOutboundBase) initHTTPBase(headers map[string]string, sessionID string) {
	b.isHTTPOutbound = true
	b.requestHeaders = make(map[string]string, len(headers))
	for key, value := range headers {
		b.requestHeaders[key] = value
	}
	b.remoteAddress = [2]string{"127.0.0.1", "0"}
	b.path = "/api/v1"
	b.sessionID = sessionID
	b.closedFlag = false
	b.outboundID = uuid.NewString()
	b.jiuwenWsID = b.outboundID
	b.frames = make(chan map[string]any, 256)
}

// isClosed 返回 closed 标志。
// Python: outbound.closed 属性。
func (b *httpOutboundBase) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closedFlag
}

// reply 构造 res 帧并投递给 acceptFrame。
// Python: outbound.py:150-169 _HttpOutboundBase.reply。
func (b *httpOutboundBase) reply(reqID string, ok bool, payload map[string]any, errorMsg string, code string) {
	frame := map[string]any{
		"type":    "res",
		"id":      reqID,
		"ok":      ok,
		"payload": payload,
	}
	if frame["payload"] == nil {
		frame["payload"] = map[string]any{}
	}
	if !ok {
		if errorMsg == "" {
			errorMsg = "request failed"
		}
		frame["error"] = errorMsg
		if code != "" {
			frame["code"] = code
		}
	}
	b.acceptFrameBase(frame, "HttpOutbound")
}

// emit 构造 event 帧并投递给 acceptFrame。
// Python: outbound.py:171-184 _HttpOutboundBase.emit。
func (b *httpOutboundBase) emit(event string, payload map[string]any, seq int, hasSeq bool, streamID string, hasStreamID bool) {
	frame := map[string]any{"type": "event", "event": event, "payload": payload}
	if hasSeq {
		frame["seq"] = seq
	}
	if hasStreamID {
		frame["stream_id"] = streamID
	}
	b.acceptFrameBase(frame, "HttpOutbound")
}

// acceptFrameBase 公共入队逻辑；队列满丢弃并告警。
// Python: 各子类 accept_frame 的 QueueFull 分支。
func (b *httpOutboundBase) acceptFrameBase(frame map[string]any, label string) {
	if b.isClosed() {
		return
	}
	select {
	case b.frames <- frame:
	default:
		logger.Warn(logComponent).Str("outbound_id", b.outboundID).Msg("[WebHTTP] " + label + " queue full; dropping frame")
	}
}

// send 兼容 writer 风格 ws.send（JSON 字符串或字节）。
// Python: outbound.py:189-195 _HttpOutboundBase.send。
func (b *httpOutboundBase) send(data any) {
	if b.isClosed() {
		return
	}
	frame := normalizeFrame(data)
	if frame != nil {
		b.acceptFrameBase(frame, "HttpOutbound")
	}
}

// closeBase 置 closed 并投递 nil 哨兵。
// Python: outbound.py:197-199 / :271-278 / :367-374。
func (b *httpOutboundBase) closeBase() {
	b.mu.Lock()
	if b.closedFlag {
		b.mu.Unlock()
		return
	}
	b.closedFlag = true
	b.mu.Unlock()
	select {
	case b.frames <- nil:
	default:
	}
}

// httpJSONOutbound 捕获 unary HTTP handler 匹配的 res 帧。
// Python: outbound.py:201-278 HttpJsonOutbound。
type httpJSONOutbound struct {
	httpOutboundBase
}

func newHTTPJSONOutbound(headers map[string]string, sessionID string) *httpJSONOutbound {
	ob := &httpJSONOutbound{}
	ob.initHTTPBase(headers, sessionID)
	return ob
}

// acceptFrame 入队（closed 后丢弃）。
// Python: outbound.py:208-215。
func (o *httpJSONOutbound) acceptFrame(frame map[string]any) {
	o.acceptFrameBase(frame, "HttpJsonOutbound")
}

// waitResponse 阻塞等待匹配 reqID 的 res 帧；超时回 TIMEOUT 帧；
// chat.error 事件直接转换为失败 res，不打空等。
// Python: outbound.py:216-269。
func (o *httpJSONOutbound) waitResponse(reqID string, timeout time.Duration) map[string]any {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return map[string]any{"type": "res", "id": reqID, "ok": false,
				"error": "request timeout", "code": "TIMEOUT", "payload": map[string]any{}}
		}
		select {
		case frame := <-o.frames:
			if frame == nil {
				return map[string]any{"type": "res", "id": reqID, "ok": false,
					"error": "connection closed", "code": "INTERNAL_ERROR", "payload": map[string]any{}}
			}
			if typeStr, _ := frame["type"].(string); typeStr == "res" {
				if idStr, _ := frame["id"].(string); idStr == reqID {
					return frame
				}
				continue
			}
			// 单次请求若被收成 chat.error 事件，不能继续空等 res。
			if typeStr, _ := frame["type"].(string); typeStr == "event" {
				if event, _ := frame["event"].(string); event == "chat.error" {
					payload, _ := frame["payload"].(map[string]any)
					if payload == nil {
						payload = map[string]any{}
					}
					frameRID := ""
					if v, ok := payload["request_id"].(string); ok {
						frameRID = v
					} else if v, ok := frame["id"].(string); ok {
						frameRID = v
					}
					if frameRID == reqID {
						errMsg := ""
						if v, ok := payload["error"].(string); ok && v != "" {
							errMsg = v
						} else if v, ok := payload["message"].(string); ok && v != "" {
							errMsg = v
						} else {
							errMsg = "request failed"
						}
						code := "INTERNAL_ERROR"
						if v, ok := payload["code"].(string); ok && v != "" {
							code = v
						}
						return map[string]any{"type": "res", "id": reqID, "ok": false,
							"error": errMsg, "code": code, "payload": payload}
					}
				}
			}
		case <-time.After(remaining):
			return map[string]any{"type": "res", "id": reqID, "ok": false,
				"error": "request timeout", "code": "TIMEOUT", "payload": map[string]any{}}
		}
	}
}

// close 关闭 outbound 并投递哨兵。
// Python: outbound.py:271-278。
func (o *httpJSONOutbound) close() {
	o.closeBase()
}

// httpSSEOutbound 为 SSE / history 聚合消费方排队帧。
// Python: outbound.py:281-374 HttpSseOutbound。
type httpSSEOutbound struct {
	httpOutboundBase
}

func newHTTPSSEOutbound(headers map[string]string, sessionID string) *httpSSEOutbound {
	ob := &httpSSEOutbound{}
	ob.initHTTPBase(headers, sessionID)
	return ob
}

// acceptFrame 入队（closed 后丢弃）。
// Python: outbound.py:288-297。
func (o *httpSSEOutbound) acceptFrame(frame map[string]any) {
	o.acceptFrameBase(frame, "HttpSseOutbound")
}

// iterSSEFrames 持续产出帧直到流结束、总超时或空闲超窗。
// timeout<=0 不限总生命周期（长 agent 轮次保持打开）；idle_timeout<=0
// 等待期间产 keepalive 帧且不因静默结束；idle_timeout>0 时空窗产
// chat.error 帧结束。返回的 channel 在流结束后关闭。
// Python: outbound.py:299-366（一比一，asyncio.wait_for → select/time.After）。
func (o *httpSSEOutbound) iterSSEFrames(reqID string, timeout time.Duration, idleTimeout time.Duration, keepalive time.Duration) <-chan map[string]any {
	out := make(chan map[string]any)
	go func() {
		defer close(out)
		totalLimit := timeout
		var deadline time.Time
		hasDeadline := totalLimit > 0
		if hasDeadline {
			deadline = time.Now().Add(totalLimit)
		}
		idleLimit := idleTimeout
		lastFrameAt := time.Now()
		ping := keepalive
		if ping < 500*time.Millisecond {
			ping = 500 * time.Millisecond
		}
		for {
			now := time.Now()
			var remaining time.Duration
			hasRemaining := hasDeadline
			if hasDeadline {
				remaining = deadline.Sub(now)
				if remaining <= 0 {
					out <- map[string]any{"type": "event", "event": "chat.error",
						"payload": map[string]any{"error": "stream timeout", "session_id": ""}}
					return
				}
			}
			if idleLimit > 0 && now.Sub(lastFrameAt) >= idleLimit {
				out <- map[string]any{"type": "event", "event": "chat.error",
					"payload": map[string]any{"error": "stream idle timeout", "session_id": ""}}
				return
			}
			waitFor := ping
			if hasRemaining {
				if remaining < waitFor {
					waitFor = remaining
				}
			}
			if idleLimit > 0 {
				if idleWait := idleLimit - now.Sub(lastFrameAt); idleWait < waitFor {
					waitFor = idleWait
				}
			}
			if waitFor < 50*time.Millisecond {
				waitFor = 50 * time.Millisecond
			}
			var frame map[string]any
			var timedOut bool
			select {
			case frame = <-o.frames:
			case <-time.After(waitFor):
				timedOut = true
			}
			if timedOut {
				if idleLimit > 0 && time.Since(lastFrameAt) >= idleLimit {
					out <- map[string]any{"type": "event", "event": "chat.error",
						"payload": map[string]any{"error": "stream idle timeout", "session_id": ""}}
					return
				}
				out <- map[string]any{"type": "keepalive"}
				continue
			}
			if frame == nil {
				return
			}
			lastFrameAt = time.Now()
			ftype, _ := frame["type"].(string)
			if ftype == "res" {
				if idStr, _ := frame["id"].(string); idStr == reqID {
					out <- frame
					if ok, okExists := frame["ok"].(bool); okExists && !ok {
						return
					}
				}
				continue
			}
			if ftype == "event" {
				out <- frame
				if isSSEEndFrame(frame, reqID) {
					return
				}
			}
		}
	}()
	return out
}

// close 关闭 outbound 并投递哨兵。
// Python: outbound.py:367-374。
func (o *httpSSEOutbound) close() {
	o.closeBase()
}
