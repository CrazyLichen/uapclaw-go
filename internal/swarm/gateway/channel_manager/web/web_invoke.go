package web

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/common/requestidentity"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// _LOCAL_ROUTING_IDENTITY_PREFIXES / _LOCAL_ROUTING_IDENTITY_METHODS
// Gateway 本地 handler 只有 params：把 metadata.routing 合并进调用副本
// （不进 Message.params）。
// Python: invoke.py:26-32。
var localRoutingIdentityPrefixes = []string{"cron.", "skills.enterprise.", "workspace."}

var localRoutingIdentityMethods = map[string]bool{
	"models.list":               true,
	"project.get_sessions":      true,
	"project.get_cron_sessions": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// httpOutboundAdapter 把 httpOutboundBase 适配成 webDeliveryRegistry 的路由面。
type httpOutboundAdapter struct {
	base *httpOutboundBase
}

func (a httpOutboundAdapter) getOutboundID() string  { return a.base.outboundID }
func (a httpOutboundAdapter) getSessionID() string   { return a.base.sessionID }
func (a httpOutboundAdapter) isClosedOutbound() bool { return a.base.isClosed() }
func (a httpOutboundAdapter) acceptFrame(f map[string]any) {
	a.base.acceptFrameBase(f, "HttpOutbound")
}
func (a httpOutboundAdapter) closeOutbound() { a.base.closeBase() }

// WriteJSON 实现 wsSender（handler 经 sendResponse 下发帧时走此入口，
// Python: web_connect.py:414-428 _enqueue_send 的 http outbound 分支）。
func (b *httpOutboundBase) WriteJSON(v any) error {
	b.send(v)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isEnterpriseWriteForbidden 企业写禁守卫（WS/HTTP 共用）。
// Python: invoke.py:62-75 is_enterprise_write_forbidden —— is_enterprise()
// 恒 false（个人版），整组企业封禁名单不触达，偏差 D-B2-5。
func isEnterpriseWriteForbidden(method string) bool {
	_ = method
	return false
}

// needsLocalRoutingIdentity 判定方法是否需要本地路由身份合并。
// Python: invoke.py:33-38 判定段。
func needsLocalRoutingIdentity(method string) bool {
	if localRoutingIdentityMethods[method] {
		return true
	}
	for _, prefix := range localRoutingIdentityPrefixes {
		if len(method) >= len(prefix) && method[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterRequestOutbound 注册请求级 HTTP outbound，返回 id。
// Python: web_connect.py:429-431 register_request_outbound。
func (wc *WebChannel) RegisterRequestOutbound(outbound *httpOutboundBase) string {
	return wc.delivery.register(httpOutboundAdapter{base: outbound})
}

// UnregisterRequestOutbound 从路由表移除请求 outbound 并关闭。
// Python: web_connect.py:433-435 unregister_request_outbound。
func (wc *WebChannel) UnregisterRequestOutbound(outbound *httpOutboundBase) {
	wc.delivery.unregister(httpOutboundAdapter{base: outbound})
}

// LookupPeer 解析 WS peer 或 HTTP outbound（metadata.ws_id / delivery.ws_id）。
// Python: web_connect.py:437-444 _lookup_peer。
func (wc *WebChannel) LookupPeer(peerID string) httpOutboundPeer {
	return wc.delivery.lookup(peerID)
}

// dispatchWebRequest 运行 HANDLER_BEFORE → on_message（或 bus）→
// 本地 handler / METHOD_NOT_FOUND 共享入站管线（WS 与 HTTP 共用）。
// outbound 是回复目标：真实 WebSocket 对端或请求级 HTTP Outbound。
// Python: invoke.py:114-211 dispatch_web_request（一比一）。
func (wc *WebChannel) dispatchWebRequest(
	outbound httpOutboundPeer,
	method string,
	reqID string,
	params map[string]any,
	sessionID string,
	userMessage *schema.Message,
) {
	// 企业写禁守卫：个人版恒 false（D-B2-5），调用位置与 Python 对齐。
	if isEnterpriseWriteForbidden(method) {
		base := outboundBaseOf(outbound)
		base.reply(reqID, false, nil, "企业版配置由管理面统一下发", "FORBIDDEN")
		return
	}

	wc.methodsMu.RLock()
	handler, hasHandler := wc.methodHandlers[method]
	wc.methodsMu.RUnlock()

	// 路由身份已由 WS/HTTP 入口写入 metadata.routing；此处只读、不再二次归一化。
	metadata := userMessage.Metadata
	routing := requestidentity.WebRoutingIdentity(metadata)
	if _, ok := routing["bot_id"]; !ok {
		logger.Warn(logComponent).
			Str("method", method).
			Str("request_id", reqID).
			Msg("[WebChannel] missing bot_id in metadata.routing")
	}
	handlerParams := params
	if needsLocalRoutingIdentity(method) {
		// Gateway 本地 handler 只有 params：把 routing 合并进调用副本。
		handlerParams = requestidentity.MergeRoutingIntoParams(params, metadata, true)
	}

	handlerAlreadyCalled := false
	if handlerBeforeCallbackMethods[method] && hasHandler {
		handlerAlreadyCalled = wc.invokeMethodHandler(outboundSenderOf(outbound), handler, method, reqID, handlerParams, sessionID)
		if !handlerAlreadyCalled {
			return
		}
	}

	handledByCallback := false
	if wc.onMessageCb != nil {
		handledByCallback = wc.onMessageCb(userMessage)
	} else if wc.channelMgr != nil {
		// fallback: 对齐 Python self.bus.publish_user_messages
		wc.channelMgr.DeliverToMessageHandler(userMessage)
	}

	if handledByCallback {
		return
	}
	if handlerAlreadyCalled {
		return
	}

	if hasHandler {
		wc.invokeMethodHandler(outboundSenderOf(outbound), handler, method, reqID, handlerParams, sessionID)
	} else {
		outboundBaseOf(outbound).reply(reqID, false, nil, "unknown method: "+method, "METHOD_NOT_FOUND")
	}
}

// outboundSenderOf 取 outbound 的 wsSender 面（WriteJSON）。
func outboundSenderOf(outbound httpOutboundPeer) wsSender {
	if base := outboundBaseOf(outbound); base != nil {
		return base
	}
	return nil
}

// outboundBaseOf 从路由面取回底层 base。
func outboundBaseOf(outbound httpOutboundPeer) *httpOutboundBase {
	if adapter, ok := outbound.(httpOutboundAdapter); ok {
		return adapter.base
	}
	return nil
}

// deliverHTTPFrames 把 Agent 事件帧投递给绑定会话的 HTTP outbound。
// Python: send() 的 session 桶路由（web_connect.py:1434 peers_for_session）——
// Go 侧 WS 仍走既有广播（B1 既有决策），本函数仅补 HTTP 请求级投递（偏差登记：
// Go res 帧不按 metadata.ws_id 精确路由，见 spec §6 D-B2-9）。
func (wc *WebChannel) deliverHTTPFrames(event string, payload map[string]any, sessionID string) {
	frame := map[string]any{"type": "event", "event": event, "payload": payload}
	for _, peer := range wc.delivery.peersForSession(sessionID) {
		peer.acceptFrame(frame)
	}
}

// deliverHTTPRes 把 res 帧投递给 metadata.ws_id 指向的 HTTP outbound
// （回退按 session 桶）。Python: web_connect.py:1340-1358。
func (wc *WebChannel) deliverHTTPRes(res map[string]any, sessionID string) {
	if wsID, ok := res["id"].(string); ok && wsID != "" {
		if peer := wc.delivery.lookup(wsID); peer != nil {
			peer.acceptFrame(res)
			return
		}
	}
	for _, peer := range wc.delivery.peersForSession(sessionID) {
		peer.acceptFrame(res)
	}
}
