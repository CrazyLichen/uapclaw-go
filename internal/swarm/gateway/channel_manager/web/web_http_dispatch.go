package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/common/requestext"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/common/requestidentity"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// requestextSetForwardHeaders 测试/调用方使用的白名单覆盖别名。
func requestextSetForwardHeaders(headers []string) {
	requestext.SetForwardHeaders(headers)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// trustClientTenantHeaders 是否信任客户端携带的租户身份头。
// env 显式开关（1/true/yes 与 0/false/no）优先；企业版恒信任（上游认证边界，
// Go 恒个人版走回环判定，偏差 D-B2-5）；个人版只信任本机请求。
// Python: web_http_dispatch.py:48-60 _trust_client_tenant_headers。
func trustClientTenantHeaders(clientHost string) bool {
	explicit := strings.TrimSpace(os.Getenv("GATEWAY_WEB_HTTP_TRUST_CLIENT_HEADERS"))
	switch strings.ToLower(explicit) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	}
	// 企业用户面通过 NodePort/Web Pod 访问时，客户端地址不是回环地址。
	// 企业版的身份边界由上游认证和 is_enterprise() 控制，必须把选中的
	// user/group/bot 透传给 Runtime 路由；个人版继续只信任本机请求。
	host := strings.ToLower(strings.TrimSpace(clientHost))
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// buildHTTPExt 从 header/query 组装请求级 ext（白名单过滤）。
// 企业登录集成有一个内建凭据字段 X-User-Token；其余扩展字段走既有白名单。
// 企业段（is_enterprise → X-User-Token 注入，dispatch.py:75-79）不移植，D-B2-6。
// Python: web_http_dispatch.py:63-79 _build_http_ext。
func buildHTTPExt(hdrs map[string]string, query map[string][]string) map[string]any {
	source := map[string]any{}
	for key, value := range hdrs {
		source[key] = value
	}
	for key, values := range query {
		if len(values) == 0 {
			continue
		}
		source[key] = values[0]
	}
	return requestext.BuildExtFromSource(source)
}

// resolveHTTPRoutingIdentity 经与 RPC 入站相同的可信租户头解析通知 scope。
// Python: web_http_dispatch.py:82-94 resolve_http_routing_identity。
func resolveHTTPRoutingIdentity(headers http.Header, clientHost string) map[string]string {
	hdrs := flattenHeaders(headers)
	if !trustClientTenantHeaders(clientHost) {
		return map[string]string{}
	}
	return requestidentity.NormalizeRoutingIdentity(map[string]string{
		"user_id":    headerGet(hdrs, "X-User-Id"),
		"group_id":   headerGet(hdrs, "X-Group-Id"),
		"bot_id":     headerGet(hdrs, "X-Bot-Id"),
		"gateway_id": headerGet(hdrs, "X-Gateway-Id"),
	})
}

// flattenHeaders 把 http.Header 平铺为小写键 map（对齐 Python _header_map）。
// Python: web_http_dispatch.py:28-37。
func flattenHeaders(headers http.Header) map[string]string {
	out := map[string]string{}
	if headers == nil {
		return out
	}
	for key, values := range headers {
		if len(values) > 0 {
			out[strings.ToLower(key)] = values[0]
		}
	}
	return out
}

// headerGet 大小写不敏感取头值（去空白）。
// Python: web_http_dispatch.py:40-45 _get_header。
func headerGet(hdrs map[string]string, name string) string {
	want := strings.ToLower(name)
	for key, value := range hdrs {
		if strings.ToLower(key) == want {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// dispatchHTTPRequest HTTP 分发适配器：绑定请求 outbound，再运行共享入站管线
// dispatchWebRequest。返回 (outbound 路由面, request_id, session_id)；调用方
// 消费完帧后必须 UnregisterRequestOutbound。不调用 register_ws。
// use_sse=true 选 HttpSseOutbound（SSE / history 收集器）；否则 HttpJsonOutbound
// （unary wait_response）。is_stream 只为下游 handler 设置 Message.is_stream。
// Python: web_http_dispatch.py:97-228 dispatch_http_request（一比一）。
func dispatchHTTPRequest(
	wc *WebChannel,
	outbound *httpOutboundBase,
	method string,
	params map[string]any,
	headers http.Header,
	requestID string,
	isStream bool,
	useSSE bool,
	bindSessionParam bool,
	clientHost string,
) (httpOutboundPeer, string, string) {
	hdrs := flattenHeaders(headers)
	copied := make(map[string]any, len(params)+1)
	for key, value := range params {
		copied[key] = value
	}
	params = copied
	reqID := strings.TrimSpace(requestID)
	if reqID == "" {
		reqID = headerGet(hdrs, "X-Request-Id")
	}
	if reqID == "" {
		reqID = hexUUID()
	}
	headerSessionID := headerGet(hdrs, "X-Session-Id")
	sessionID, params := bindHTTPSession(method, params, headerSessionID, bindSessionParam)

	trustTenantHeaders := trustClientTenantHeaders(clientHost)
	userID := ""
	if trustTenantHeaders {
		userID = headerGet(hdrs, "X-User-Id")
	}
	channelIDHdr := headerGet(hdrs, "X-Channel-Id")
	if channelIDHdr == "" {
		channelIDHdr = "web"
	}
	appID := headerGet(hdrs, "X-App-Id")
	if appID == "" {
		appID = "default"
	}

	// 连接级身份挂到 outbound（Python: setattr _WEB_CONNECTION_USER_ID_ATTR /
	// _jiuwen_app_id，dispatch.py:143-144）。
	outbound.connectionUserID = userID
	outbound.appID = appID
	peer := wc.RegisterRequestOutbound(outbound)
	_ = peer

	// 仅新建会话且解析到 user_id 时打 http_resolve_identity 审计日志。
	if method == "session.create" && userID != "" {
		logger.Info(logComponent).
			Str("proc", "http_resolve_identity").
			Str("user_id", userID).
			Str("session_id", sessionID).
			Str("request_id", reqID).
			Msg("[WebHTTP] http_resolve_identity")
	}

	// 路由 query（仅信任来源）。
	query := map[string][]string{}
	if trustTenantHeaders {
		for hk, hv := range map[string]string{
			"user_id":    userID,
			"group_id":   headerGet(hdrs, "X-Group-Id"),
			"bot_id":     headerGet(hdrs, "X-Bot-Id"),
			"gateway_id": headerGet(hdrs, "X-Gateway-Id"),
		} {
			if hv != "" {
				query[hk] = []string{hv}
			}
		}
	}

	ext := buildHTTPExt(hdrs, func() map[string][]string {
		if trustTenantHeaders {
			return query
		}
		return map[string][]string{}
	}())

	mode := "agent"
	if v, ok := params["mode"].(string); ok && v != "" {
		mode = v
	}
	agentID := "default"
	if v, ok := params["agent_id"].(string); ok && v != "" {
		agentID = v
	}

	// 路由身份写入 metadata：user_id 顶层、三元组收进 routing（:185-195）。
	routing := resolveHTTPRoutingIdentity(headers, clientHost)
	meta := requestidentity.ApplyRoutingMetadata(map[string]any{
		"query":     query,
		"method":    method,
		"ws_id":     outbound.jiuwenWsID,
		"transport": "web-http",
		// 偏差 D-B2-11：Python Message.agent_ref={"mode":mode,"id":agent_id}
		// 在 Go schema.Message 无对应字段，落 metadata.agent_ref 保数据不丢。
		"agent_ref": map[string]any{"mode": mode, "id": agentID},
	}, routing)

	reqMethod, _ := schema.ParseReqMethod(method)
	userMessage := &schema.Message{
		ID:        reqID,
		Type:      schema.MessageTypeReq,
		ChannelID: channelIDHdr,
		SessionID: sessionID,
		Params:    marshalParams(params),
		Timestamp: float64(time.Now().UnixMilli()) / 1000.0,
		OK:        true,
		ReqMethod: reqMethod,
		Mode:      parseMode(params["mode"]),
		IsStream:  isStream,
		AppID:     appID,
		UserID:    userID,
		// 偏差：Python Message.agent_ref={mode,id} 在 Go schema.Message 无对应字段，
		// 已落 metadata.agent_ref（见 D-B2-11）。
		Metadata: requestext.AttachToMetadata(meta, ext),
	}

	wc.dispatchWebRequest(httpOutboundAdapter{base: outbound}, method, reqID, params, sessionID, userMessage)
	return httpOutboundAdapter{base: outbound}, reqID, sessionID
}

// marshalParams params → JSON（对齐 BuildUserMessage 的 params 编码习惯）。
func marshalParams(params map[string]any) []byte {
	data, err := json.Marshal(params)
	if err != nil {
		return []byte("{}")
	}
	return data
}
