package web

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// headerToParam 透传头 → params 键映射。
// Python: web_http_routes.py:36-42 _HEADER_TO_PARAM。
var headerToParam = map[string]string{
	"x-user-id":    "user_id",
	"x-group-id":   "group_id",
	"x-bot-id":     "bot_id",
	"x-gateway-id": "gateway_id",
	"x-session-id": "session_id",
}

// streamRPCMethods 无 SSE Accept 也按流式转发的 RPC 方法集合。
// Python: web_http_routes.py:54 _STREAM_RPC_METHODS。
var streamRPCMethods = map[string]bool{"chat.send": true, "history.get": true}

// httpErrorCodeStatus res 帧错误码 → HTTP status 映射。
// Python: web_http_routes.py:197-208。
var httpErrorCodeStatus = map[string]int{
	"BAD_REQUEST":              400,
	"UNAUTHORIZED":             401,
	"FORBIDDEN":                403,
	"NOT_FOUND":                404,
	"METHOD_NOT_FOUND":         404,
	"CONFLICT":                 409,
	"TIMEOUT":                  504,
	"SERVICE_UNAVAILABLE":      503,
	"WORKSPACE_QUOTA_EXCEEDED": 403,
}

// ──────────────────────────── 结构体（body 模型，json tag 承载） ────────────────────────────

// sessionCreateBody POST /api/v1/sessions body。
// Python: web_http_routes.py:72-90 SessionCreateBody（extra=allow，全字段可选）。
type sessionCreateBody struct {
	// Mode 可选运行模式
	Mode *string `json:"mode,omitempty"`
	// ProjectID 可选；省略绑定默认项目
	ProjectID *string `json:"project_id,omitempty"`
	// ProjectDir 可选
	ProjectDir *string `json:"project_dir,omitempty"`
	// WorkMode 可选（work / code）
	WorkMode *string `json:"work_mode,omitempty"`
	// CreateToken 可选幂等令牌；省略 Gateway 自动生成
	CreateToken *string `json:"create_token,omitempty"`
}

// sessionPatchBody PATCH /api/v1/sessions/{id} body。
// Python: web_http_routes.py:92-95 SessionPatchBody。
type sessionPatchBody struct {
	// Title 重命名标题
	Title *string `json:"title,omitempty"`
	// Pinned 置顶
	Pinned *bool `json:"pinned,omitempty"`
}

// chatSendBody POST /api/v1/chat/completions body。
// Python: web_http_routes.py:97-110 ChatSendBody（session_id/query 必填）。
type chatSendBody struct {
	// SessionID 已有会话 id（必填，先 POST /sessions）
	SessionID string `json:"session_id"`
	// Query 用户消息（必填，对齐 RPC chat.send）
	Query string `json:"query"`
	// Mode 可选；省略 agent
	Mode *string `json:"mode,omitempty"`
	// EnableStreaming 可选；默认 true
	EnableStreaming *bool `json:"enable_streaming,omitempty"`
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// webHTTPMetadata transport 元数据。
// Python: web_http_routes.py:166-168 _web_http_metadata。
func webHTTPMetadata(method string) map[string]any {
	return map[string]any{"rpc_method": method, "transport": "web-http"}
}

// responseHeaders 响应头：X-Request-Id 恒带，method 非空补 X-Web-RPC-Method。
// Python: web_http_routes.py:170-174 _response_headers。
func responseHeaders(requestID, method string) map[string]string {
	headers := map[string]string{"X-Request-Id": requestID}
	if method != "" {
		headers["X-Web-RPC-Method"] = method
	}
	return headers
}

// envelopeFromRes 把 res 帧转换为 REST envelope 与 HTTP status。
// Python: web_http_routes.py:177-208 _envelope_from_res。
func envelopeFromRes(frame map[string]any, requestID string, rpcMethod string) (map[string]any, int) {
	ok, _ := frame["ok"].(bool)
	body := map[string]any{"request_id": requestID, "ok": ok}
	if ok {
		if payload, has := frame["payload"]; has && payload != nil {
			body["data"] = payload
		} else {
			body["data"] = map[string]any{}
		}
		if rpcMethod != "" {
			body["metadata"] = webHTTPMetadata(rpcMethod)
		} else {
			body["metadata"] = map[string]any{}
		}
		return body, 200
	}
	errCode := ""
	if v, ok := frame["code"].(string); ok {
		errCode = v
	}
	if errCode == "" {
		errCode = "INTERNAL_ERROR"
	}
	errMsg := "request failed"
	if v, ok := frame["error"].(string); ok && v != "" {
		errMsg = v
	}
	details := map[string]any{}
	if payload, isMap := frame["payload"].(map[string]any); isMap {
		details = payload
	}
	body["error"] = map[string]any{"code": errCode, "message": errMsg, "details": details}
	if rpcMethod != "" {
		body["metadata"] = webHTTPMetadata(rpcMethod)
	}
	status, mapped := httpErrorCodeStatus[errCode]
	if !mapped {
		status = 500
	}
	return body, status
}

// ssePack 打包一帧 SSE 文本。
// Python: web_http_routes.py:211-213 _sse_pack。
func ssePack(requestID string, event string, data any) string {
	payload := ""
	if s, isStr := data.(string); isStr {
		payload = s
	} else {
		raw, err := json.Marshal(data)
		if err != nil {
			payload = "{}"
		} else {
			payload = string(raw)
		}
	}
	return fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", requestID, event, payload)
}

// chatIdentityError 校验写历史 chat 方法的 header/body 身份一致性；不一致回 400。
// Python: web_http_routes.py:640-666 _chat_identity_error。
func chatIdentityError(method string, headers http.Header, params map[string]any, requestID string) (map[string]any, int, bool) {
	if method != "chat.send" && method != "chat.resume" && method != "chat.user_answer" {
		return nil, 0, false
	}
	for _, field := range []string{"user_id", "group_id", "bot_id", "gateway_id"} {
		headerKey := "x-" + strings.ReplaceAll(field, "_", "-")
		headerValue := strings.TrimSpace(headers.Get(headerKey))
		bodyValue := ""
		if v, ok := params[field]; ok && v != nil {
			bodyValue = strings.TrimSpace(toStr(v))
		}
		// Match routing normalization: missing/null/blank identity is unset.
		if headerValue != "" && bodyValue != "" && bodyValue != headerValue {
			body := map[string]any{
				"request_id": requestID,
				"ok":         false,
				"error": map[string]any{
					"code":    "IDENTITY_CONFLICT",
					"message": "Request header and body identity must match",
					"details": map[string]any{"field": field},
				},
				"metadata": webHTTPMetadata(method),
			}
			return body, 400, true
		}
	}
	return nil, 0, false
}

// toStr any → string（json 序列化兜底非 string 类型）。
func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

// coerceQueryValue query 值类型归一：布尔只认显式字面量；1/0 更可能是数值
// （如 page=1）交给 isdigit 转 int，避免被截胡成 True/False。
// Python: web_http_routes.py:823-837 _coerce_query_value。
func coerceQueryValue(raw string) any {
	text := strings.TrimSpace(raw)
	lowered := strings.ToLower(text)
	if lowered == "true" || lowered == "yes" {
		return true
	}
	if lowered == "false" || lowered == "no" {
		return false
	}
	digits := text
	if strings.HasPrefix(digits, "-") {
		digits = digits[1:]
	}
	if digits != "" && isAllDigits(digits) {
		return jsonNumber(text)
	}
	return text
}

// isAllDigits 全数字判定（对齐 str.isdigit）。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// jsonNumber 数字字符串 → int（float64 兜底，对齐 Python int()）。
func jsonNumber(text string) any {
	var n int
	if _, err := fmt.Sscanf(text, "%d", &n); err == nil {
		return n
	}
	return text
}

// mergeHeaderParams params 缺省时按头注入（不入业务 body 的身份仅补缺）。
// Python: web_http_routes.py:840-848 _merge_header_params。
func mergeHeaderParams(request *http.Request, params map[string]any) map[string]any {
	out := make(map[string]any, len(params)+len(headerToParam))
	for key, value := range params {
		out[key] = value
	}
	for headerName, paramName := range headerToParam {
		if existing, ok := out[paramName]; ok && existing != nil && existing != "" {
			continue
		}
		value := request.Header.Get(headerName)
		if value != "" {
			out[paramName] = value
		}
	}
	return out
}

// paramsFromMappedRoute 组装 mapped 路由的 params：extra → path → query → body → headers。
// Python: web_http_routes.py:851-877 _params_from_mapped_route。
func paramsFromMappedRoute(request *http.Request, spec *webHTTPMappedRoute, body map[string]any) map[string]any {
	params := map[string]any{}
	for key, value := range spec.extraParams {
		params[key] = value
	}
	for src, dst := range spec.pathToParam {
		if value := chi.URLParam(request, src); value != "" {
			params[dst] = value
		}
	}
	for _, key := range spec.queryKeys {
		if values, ok := request.URL.Query()[key]; ok && len(values) > 0 {
			params[key] = coerceQueryValue(values[0])
		}
	}
	rawBody := body
	if spec.acceptBody && rawBody == nil && isBodyMethod(request.Method) {
		contentType := strings.ToLower(request.Header.Get("Content-Type"))
		if strings.Contains(contentType, "application/json") {
			var parsed map[string]any
			if err := json.NewDecoder(request.Body).Decode(&parsed); err != nil {
				parsed = nil
			}
			if parsed != nil {
				rawBody = parsed
			}
		}
	}
	if rawBody != nil {
		merged := make(map[string]any, len(rawBody)+len(params))
		for key, value := range rawBody {
			merged[key] = value
		}
		for key, value := range params {
			merged[key] = value
		}
		params = merged
	}
	return mergeHeaderParams(request, params)
}

// isBodyMethod 带请求体的方法集（Python :864 request.method in {...}）。
func isBodyMethod(method string) bool {
	return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
}

// writeJSONResponse 统一 JSON 响应写出（含响应头）。
func writeJSONResponse(w http.ResponseWriter, headers map[string]string, status int, body map[string]any) {
	for key, value := range headers {
		w.Header().Set(key, value)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterWebHTTPAPIRoutes 在 chi 路由上注册 CORE + mapped /api/v1 路由。
// Go 恒个人版（enterprise 参数不移植，D-B2-5）。
// Python: web_http_routes.py:216-519 register_web_http_api_routes。
func RegisterWebHTTPAPIRoutes(r chi.Router, wc *WebChannel) {
	mappedRoutesList := mappedRoutesForEdition()

	// GET /api/v1/health 探活（Python :234-240）
	r.Get("/api/v1/health", func(w http.ResponseWriter, req *http.Request) {
		rid := req.Header.Get("x-request-id")
		if rid == "" {
			rid = hexUUID()
		}
		writeJSONResponse(w, map[string]string{"X-Request-Id": rid}, 200, map[string]any{
			"request_id": rid, "ok": true,
			"data": map[string]any{"status": "ready"}, "metadata": map[string]any{},
		})
	})

	// GET /api/v1/connection/status（Python :242-244）
	r.Get("/api/v1/connection/status", func(w http.ResponseWriter, req *http.Request) {
		handleMappedUnary(w, req, wc, "connection.status", map[string]any{}, false, true)
	})

	// GET /api/v1/cron/runs/live 定时任务运行通知 SSE。
	// Python :246-279 依赖 gateway.cron.live_bus；Go 侧总线为回填件（偏差 D-B2-9），
	// 本批落所有权校验与 SSE 形状（连接即订阅，帧到达前仅 keepalive）。
	r.Get("/api/v1/cron/runs/live", func(w http.ResponseWriter, req *http.Request) {
		access := runUnaryInternal(req, wc, "cron.job.list", map[string]any{}, false, false)
		if access.status >= 400 {
			writeJSONResponse(w, access.headers, access.status, access.body)
			return
		}
		serveKeepaliveSSE(w, req, "cron.run.updated")
	})

	// GET /api/v1/sessions 列会话（Python :281-292）
	r.Get("/api/v1/sessions", func(w http.ResponseWriter, req *http.Request) {
		params := map[string]any{}
		if v := req.URL.Query().Get("limit"); v != "" {
			if n, ok := coerceQueryValue(v).(int); ok {
				params["limit"] = n
			}
		}
		if v := req.URL.Query().Get("offset"); v != "" {
			if n, ok := coerceQueryValue(v).(int); ok {
				params["offset"] = n
			}
		}
		handleMappedUnary(w, req, wc, "session.list", params, false, true)
	})

	// POST /api/v1/sessions 建会话，201（Python :294-315）
	r.Post("/api/v1/sessions", func(w http.ResponseWriter, req *http.Request) {
		params, perr := bodyParams(readRawBody(req), &sessionCreateBody{},
			[]string{"mode", "project_id", "project_dir", "work_mode", "create_token"})
		if perr != nil {
			rid := requestIDOrUUID(req)
			writeJSONResponse(w, responseHeaders(rid, "session.create"), 400, map[string]any{
				"request_id": rid, "ok": false,
				"error":    map[string]any{"code": "BAD_REQUEST", "message": "invalid JSON body", "details": map[string]any{}},
				"metadata": webHTTPMetadata("session.create"),
			})
			return
		}
		handleMappedUnary(w, req, wc, "session.create", params, true, true)
	})

	// GET /api/v1/sessions/{session_id} 会话元数据（Python :317-325）
	r.Get("/api/v1/sessions/{session_id}", func(w http.ResponseWriter, req *http.Request) {
		handleMappedUnary(w, req, wc, "session.get_metadata",
			map[string]any{"session_id": chi.URLParam(req, "session_id")}, false, true)
	})

	// PATCH /api/v1/sessions/{session_id} 重命名/置顶双分流（Python :327-335）
	r.Patch("/api/v1/sessions/{session_id}", func(w http.ResponseWriter, req *http.Request) {
		payload, perr := bodyParams(readRawBody(req), &sessionPatchBody{}, []string{"title", "pinned"})
		if perr != nil {
			writeJSONResponse(w, map[string]string{}, 400, map[string]any{
				"ok":    false,
				"error": map[string]any{"code": "BAD_REQUEST", "message": "invalid JSON body", "details": map[string]any{}},
			})
			return
		}
		payload["session_id"] = chi.URLParam(req, "session_id")
		if _, hasPinned := payload["pinned"]; hasPinned {
			handleMappedUnary(w, req, wc, "session.pin", payload, false, true)
			return
		}
		if _, hasPin := payload["pin"]; hasPin {
			handleMappedUnary(w, req, wc, "session.pin", payload, false, true)
			return
		}
		handleMappedUnary(w, req, wc, "session.rename", payload, false, true)
	})

	// DELETE /api/v1/sessions/{session_id}（Python :337-341）
	r.Delete("/api/v1/sessions/{session_id}", func(w http.ResponseWriter, req *http.Request) {
		handleMappedUnary(w, req, wc, "session.delete",
			map[string]any{"session_id": chi.URLParam(req, "session_id")}, false, true)
	})

	// GET /api/v1/sessions/{session_id}/history JSON；Accept SSE 时走流（Python :343-357）
	r.Get("/api/v1/sessions/{session_id}/history", func(w http.ResponseWriter, req *http.Request) {
		params := historyParamsFromRequest(chi.URLParam(req, "session_id"), req)
		accept := strings.ToLower(req.Header.Get("Accept"))
		if strings.Contains(accept, "text/event-stream") {
			handleStream(w, req, wc, "history.get", params)
			return
		}
		handleHistoryJSON(w, req, wc, params)
	})

	// GET /api/v1/sessions/{session_id}/live 订阅定时任务执行会话（Python :359-408）
	r.Get("/api/v1/sessions/{session_id}/live", func(w http.ResponseWriter, req *http.Request) {
		sid := strings.TrimSpace(chi.URLParam(req, "session_id"))
		metadataResult := runUnaryInternal(req, wc, "session.get_metadata",
			map[string]any{"session_id": sid}, false, true)
		if metadataResult.status >= 400 {
			writeJSONResponse(w, metadataResult.headers, metadataResult.status, metadataResult.body)
			return
		}
		data, _ := metadataResult.body["data"].(map[string]any)
		if data == nil || data["cron_id"] == nil || data["cron_id"] == "" {
			writeJSONResponse(w, map[string]string{}, 404, map[string]any{"error": "not_cron_session"})
			return
		}
		// live_bus 订阅为回填件（D-B2-9），此处保持 SSE 形状与 keepalive。
		serveKeepaliveSSE(w, req, "message")
	})

	// POST /api/v1/chat/completions 发消息，默认 SSE（Python :410-429）
	r.Post("/api/v1/chat/completions", func(w http.ResponseWriter, req *http.Request) {
		payload, perr := bodyParams(readRawBody(req), &chatSendBody{},
			[]string{"session_id", "query", "mode", "enable_streaming"})
		if perr != nil || payload["session_id"] == "" || payload["query"] == "" {
			// Python pydantic 必填缺失回 422；Go envelope 统一 400 BAD_REQUEST
			rid := requestIDOrUUID(req)
			writeJSONResponse(w, responseHeaders(rid, "chat.send"), 400, map[string]any{
				"request_id": rid, "ok": false,
				"error":    map[string]any{"code": "BAD_REQUEST", "message": "session_id and query are required", "details": map[string]any{}},
				"metadata": webHTTPMetadata("chat.send"),
			})
			return
		}
		accept := strings.ToLower(req.Header.Get("accept"))
		wantStream := strings.Contains(accept, "text/event-stream")
		if v, ok := payload["enable_streaming"].(bool); ok && v {
			wantStream = true
		}
		if wantStream {
			handleStream(w, req, wc, "chat.send", payload)
			return
		}
		handleUnary(w, req, wc, "chat.send", payload, false, false)
	})
	// POST /api/v1/chat/send 兼容路径（不进 OpenAPI，Python :415-420）
	r.Post("/api/v1/chat/send", func(w http.ResponseWriter, req *http.Request) {
		payload, perr := bodyParams(readRawBody(req), &chatSendBody{},
			[]string{"session_id", "query", "mode", "enable_streaming"})
		if perr != nil || payload["session_id"] == "" || payload["query"] == "" {
			rid := requestIDOrUUID(req)
			writeJSONResponse(w, responseHeaders(rid, "chat.send"), 400, map[string]any{
				"request_id": rid, "ok": false,
				"error":    map[string]any{"code": "BAD_REQUEST", "message": "session_id and query are required", "details": map[string]any{}},
				"metadata": webHTTPMetadata("chat.send"),
			})
			return
		}
		accept := strings.ToLower(req.Header.Get("accept"))
		wantStream := strings.Contains(accept, "text/event-stream")
		if v, ok := payload["enable_streaming"].(bool); ok && v {
			wantStream = true
		}
		if wantStream {
			handleStream(w, req, wc, "chat.send", payload)
			return
		}
		handleUnary(w, req, wc, "chat.send", payload, false, false)
	})

	// POST /api/v1/chat/resume（Python :431-435）
	r.Post("/api/v1/chat/resume", func(w http.ResponseWriter, req *http.Request) {
		handleUnary(w, req, wc, "chat.resume", readJSONBodyOptional(req), false, true)
	})

	// POST /api/v1/chat/{session_id}/actions/interrupt（Python :437-447）
	r.Post("/api/v1/chat/{session_id}/actions/interrupt", func(w http.ResponseWriter, req *http.Request) {
		payload := readJSONBodyOptional(req)
		payload["session_id"] = chi.URLParam(req, "session_id")
		handleUnary(w, req, wc, "chat.interrupt", payload, false, true)
	})

	// POST /api/v1/chat/{session_id}/actions/user_answer（Python :449-465）
	r.Post("/api/v1/chat/{session_id}/actions/user_answer", func(w http.ResponseWriter, req *http.Request) {
		payload := readJSONBodyOptional(req)
		payload["session_id"] = chi.URLParam(req, "session_id")
		handleUnary(w, req, wc, "chat.user_answer", payload, false, true)
	})

	// GET /api/v1/catalog（Python :467-485）
	r.Get("/api/v1/catalog", func(w http.ResponseWriter, req *http.Request) {
		rid := req.Header.Get("x-request-id")
		if rid == "" {
			rid = hexUUID()
		}
		writeJSONResponse(w, responseHeaders(rid, "catalog"), 200, map[string]any{
			"request_id": rid,
			"ok":         true,
			"data": map[string]any{
				"prefix": "/api/v1",
				"routes": catalogEntries(),
			},
			"metadata": webHTTPMetadata("catalog"),
		})
	})

	// POST /api/v1/harness/packages/actions/import-file multipart 50MB（Python :487-517）
	r.Post("/api/v1/harness/packages/actions/import-file", func(w http.ResponseWriter, req *http.Request) {
		rid := req.Header.Get("x-request-id")
		if rid == "" {
			rid = hexUUID()
		}
		file, _, err := req.FormFile("file")
		if err != nil {
			writeJSONResponse(w, responseHeaders(rid, "harness.import"), 400, map[string]any{
				"request_id": rid, "ok": false,
				"error":    map[string]any{"code": "BAD_REQUEST", "message": "multipart field `file` is required", "details": map[string]any{}},
				"metadata": webHTTPMetadata("harness.import"),
			})
			return
		}
		defer file.Close()
		raw, err := io.ReadAll(file)
		if err != nil {
			writeJSONResponse(w, responseHeaders(rid, "harness.import"), 400, map[string]any{
				"request_id": rid, "ok": false,
				"error":    map[string]any{"code": "BAD_REQUEST", "message": err.Error(), "details": map[string]any{}},
				"metadata": webHTTPMetadata("harness.import"),
			})
			return
		}
		const maxUpload = 50 * 1024 * 1024
		if len(raw) > maxUpload {
			writeJSONResponse(w, responseHeaders(rid, "harness.import"), 400, map[string]any{
				"request_id": rid, "ok": false,
				"error":    map[string]any{"code": "BAD_REQUEST", "message": "File exceeds 50MB limit", "details": map[string]any{}},
				"metadata": webHTTPMetadata("harness.import"),
			})
			return
		}
		handleUnary(w, req, wc, "harness.import",
			map[string]any{"file_content": base64.StdEncoding.EncodeToString(raw)}, false, false)
	})

	registerMappedRoutes(r, wc, mappedRoutesList)
}

// bodyParams 解析 body 为 typed struct（json tag 承载已知字段，exclude_none
// 语义经指针 omitempty）+ 未声明字段 extra 透传（对齐 Python extra="allow" +
// model_dump(exclude_none=True)，web_http_routes.py:112-117 _body_dict）。
func bodyParams(raw []byte, typed any, declared []string) (map[string]any, error) {
	extra := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &extra); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(raw, typed); err != nil {
		return nil, err
	}
	// 已知字段经 typed 序列化（nil 指针被 omitempty 排除 = exclude_none）
	typedJSON, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if len(typedJSON) > 0 {
		_ = json.Unmarshal(typedJSON, &out)
	}
	declaredSet := map[string]bool{}
	for _, key := range declared {
		declaredSet[key] = true
	}
	for key, value := range extra {
		if !declaredSet[key] {
			out[key] = value
		}
	}
	return out, nil
}

// readRawBody 读完整 body（空 body 返回 nil）。
func readRawBody(req *http.Request) []byte {
	if req.Body == nil {
		return nil
	}
	raw, _ := io.ReadAll(req.Body)
	return raw
}

// requestIDOrUUID X-Request-Id 缺省时生成 uuid hex。
func requestIDOrUUID(req *http.Request) string {
	rid := req.Header.Get("x-request-id")
	if rid == "" {
		rid = hexUUID()
	}
	return rid
}

// ──────────────────────────── 非导出函数（分发通道） ────────────────────────────

// unaryResult 内部 unary 结果（供 live 路由复用响应）。
type unaryResult struct {
	headers map[string]string
	status  int
	body    map[string]any
}

// runUnaryInternal 运行一次 unary 分发并捕获结果（Python _unary 内部逻辑）。
func runUnaryInternal(req *http.Request, wc *WebChannel, method string, params map[string]any, created bool, bindSessionParam bool) unaryResult {
	rid := req.Header.Get("x-request-id")
	if rid == "" {
		rid = hexUUID()
	}
	if body, status, conflict := chatIdentityError(method, req.Header, params, rid); conflict {
		return unaryResult{headers: responseHeaders(rid, method), status: status, body: body}
	}
	outbound := newHTTPJSONOutbound(nil, "")
	_, reqID, _ := dispatchHTTPRequest(wc, &outbound.httpOutboundBase, method, params, req.Header, rid,
		isStreamRPC(method), false, bindSessionParam, requestClientHost(req))
	frame := outbound.waitResponse(reqID, time.Duration(resolveWebHTTPUnaryTimeout()*float64(time.Second)))
	body, status := envelopeFromRes(frame, reqID, method)
	if created && body["ok"] == true {
		status = 201
	}
	return unaryResult{headers: responseHeaders(reqID, method), status: status, body: body}
}

// isStreamRPC 无 SSE Accept 也按流式转发的方法（Python :54/:691）。
func isStreamRPC(method string) bool {
	return streamRPCMethods[method]
}

// requestClientHost 客户端地址（Python :47-52 _request_client_host）。
func requestClientHost(req *http.Request) string {
	host := req.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	return host
}

// handleUnary 处理 unary 请求（Python :669-740 _unary）。
func handleUnary(w http.ResponseWriter, req *http.Request, wc *WebChannel, method string, params map[string]any, created bool, bindSessionParam bool) {
	result := runUnaryInternal(req, wc, method, params, created, bindSessionParam)
	writeJSONResponse(w, result.headers, result.status, result.body)
}

// handleMappedUnary mapped 表行的 unary 处理。
func handleMappedUnary(w http.ResponseWriter, req *http.Request, wc *WebChannel, method string, params map[string]any, created bool, bindSessionParam bool) {
	handleUnary(w, req, wc, method, params, created, bindSessionParam)
}

// handleStream SSE 流式处理（Python :743-820 _stream）。
func handleStream(w http.ResponseWriter, req *http.Request, wc *WebChannel, method string, params map[string]any) {
	rid := req.Header.Get("x-request-id")
	if rid == "" {
		rid = hexUUID()
	}
	if body, status, conflict := chatIdentityError(method, req.Header, params, rid); conflict {
		writeJSONResponse(w, responseHeaders(rid, method), status, body)
		return
	}
	for key, value := range responseHeaders(rid, method) {
		w.Header().Set(key, value)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)

	outbound := newHTTPSSEOutbound(nil, "")
	flusher, _ := w.(http.Flusher)
	_, reqID, _ := dispatchHTTPRequest(wc, &outbound.httpOutboundBase, method, params, req.Header, rid,
		true, true, true, requestClientHost(req))
	defer wc.UnregisterRequestOutbound(&outbound.httpOutboundBase)

	frames := outbound.iterSSEFrames(reqID,
		time.Duration(resolveWebHTTPSSETimeout()*float64(time.Second)),
		time.Duration(resolveWebHTTPSSEIdleTimeout()*float64(time.Second)),
		time.Duration(resolveWebHTTPSSEKeepalive()*float64(time.Second)))
	for frame := range frames {
		if req.Context().Err() != nil {
			logger.Info(logComponent).
				Str("method", method).
				Str("request_id", reqID).
				Msg("[WebHTTP] SSE client disconnected")
			break
		}
		ftype, _ := frame["type"].(string)
		if ftype == "keepalive" {
			_, _ = w.Write([]byte(": keepalive\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			continue
		}
		if ftype == "res" {
			ok, _ := frame["ok"].(bool)
			if ok {
				_, _ = w.Write([]byte(ssePack(reqID, "web.response", frameOrEmpty(frame))))
			} else {
				data := map[string]any{"error": frame["error"], "code": frame["code"]}
				if payload, isMap := frame["payload"].(map[string]any); isMap {
					for key, value := range payload {
						data[key] = value
					}
				}
				_, _ = w.Write([]byte(ssePack(reqID, "chat.error", data)))
			}
			if !ok {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			continue
		}
		if ftype == "event" {
			event, _ := frame["event"].(string)
			if event == "" {
				event = "message"
			}
			payload, _ := frame["payload"].(map[string]any)
			if payload == nil {
				payload = map[string]any{}
			}
			_, _ = w.Write([]byte(ssePack(reqID, event, payload)))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// frameOrEmpty 取 res 帧 payload（缺省空 map）。
func frameOrEmpty(frame map[string]any) map[string]any {
	if payload, ok := frame["payload"].(map[string]any); ok {
		return payload
	}
	return map[string]any{}
}

// serveKeepaliveSSE live 路由的占位 SSE 流（live bus 回填前仅 keepalive，D-B2-9）。
func serveKeepaliveSSE(w http.ResponseWriter, req *http.Request, event string) {
	_ = event
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(": connected\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// historyParamsFromRequest 按页面实际发来的参数组装 history.get。
// 游标协议：query 丢掉 null，只看到 limit 没有 page_idx 时也按游标转发；
// 缺失/空白 cursor 必须转发为 nil 首页——agentserver 把空串当非法游标。
// Python: web_http_routes.py:131-163 _history_params_from_request。
func historyParamsFromRequest(sessionID string, request *http.Request) map[string]any {
	query := request.URL.Query()
	pageIdxRaw := query.Get("page_idx")
	var pageIdx any
	if pageIdxRaw != "" {
		if n, ok := coerceQueryValue(pageIdxRaw).(int); ok {
			pageIdx = n
		}
	}
	_, hasCursor := query["cursor"]
	useCursor := hasCursor ||
		(query.Has("limit") && !query.Has("page_idx") && pageIdx == nil)
	if !useCursor {
		pageIdxVal := 1
		if n, ok := pageIdx.(int); ok {
			pageIdxVal = n
		}
		return map[string]any{"session_id": sessionID, "page_idx": pageIdxVal}
	}
	rawCursor := strings.TrimSpace(query.Get("cursor"))
	rawLimit := strings.TrimSpace(query.Get("limit"))
	limit := 50
	if rawLimit != "" {
		if n, ok := coerceQueryValue(rawLimit).(int); ok {
			limit = n
		}
	}
	params := map[string]any{"session_id": sessionID, "limit": limit}
	if rawCursor != "" {
		params["cursor"] = rawCursor
	} else {
		params["cursor"] = nil
	}
	subagentID := strings.TrimSpace(query.Get("subagent_id"))
	if subagentID != "" {
		params["subagent_id"] = subagentID
	}
	return params
}

// handleHistoryJSON 把 AgentServer history.message 帧收集为 REST envelope。
// Python: web_http_routes.py:522-637 _history_json。
func handleHistoryJSON(w http.ResponseWriter, req *http.Request, wc *WebChannel, params map[string]any) {
	rid := req.Header.Get("x-request-id")
	if rid == "" {
		rid = hexUUID()
	}
	outbound := newHTTPSSEOutbound(nil, "")
	_, reqID, _ := dispatchHTTPRequest(wc, &outbound.httpOutboundBase, "history.get", params, req.Header, rid,
		true, true, true, requestClientHost(req))
	defer wc.UnregisterRequestOutbound(&outbound.httpOutboundBase)

	messages := []any{}
	var totalPages any
	pageIdx := params["page_idx"]
	if pageIdx == nil {
		pageIdx = 1
	}
	var errorPayload map[string]any

	frames := outbound.iterSSEFrames(reqID,
		time.Duration(resolveWebHTTPHistoryTimeout()*float64(time.Second)),
		time.Duration(resolveWebHTTPSSEIdleTimeout()*float64(time.Second)),
		time.Duration(resolveWebHTTPSSEKeepalive()*float64(time.Second)))
collect:
	for frame := range frames {
		ftype, _ := frame["type"].(string)
		if ftype == "res" {
			if ok, okExists := frame["ok"].(bool); okExists && !ok {
				body, status := envelopeFromRes(frame, reqID, "history.get")
				writeJSONResponse(w, responseHeaders(reqID, "history.get"), status, body)
				return
			}
			continue
		}
		if ftype != "event" {
			continue
		}
		event, _ := frame["event"].(string)
		payload, _ := frame["payload"].(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		if event == "chat.error" {
			errorPayload = payload
			break
		}
		if event == "history.message" {
			if status, _ := payload["status"].(string); status == "done" {
				if v, has := payload["total_pages"]; has {
					totalPages = v
				}
				if v, has := payload["page_idx"]; has {
					pageIdx = v
				}
				break collect
			}
			if item, has := payload["message"]; has && item != nil {
				messages = append(messages, item)
			}
			if v, has := payload["total_pages"]; has {
				totalPages = v
			}
			if v, has := payload["page_idx"]; has {
				pageIdx = v
			}
		}
	}
	if errorPayload != nil {
		errMsg := "history.get failed"
		if v, ok := errorPayload["error"].(string); ok && v != "" {
			errMsg = v
		}
		writeJSONResponse(w, responseHeaders(reqID, "history.get"), 404, map[string]any{
			"request_id": rid,
			"ok":         false,
			"error":      map[string]any{"code": "NOT_FOUND", "message": errMsg, "details": errorPayload},
		})
		return
	}
	writeJSONResponse(w, responseHeaders(reqID, "history.get"), 200, map[string]any{
		"request_id": rid,
		"ok":         true,
		"data": map[string]any{
			"session_id":  params["session_id"],
			"messages":    messages,
			"total_pages": totalPages,
			"page_idx":    pageIdx,
		},
		"metadata": webHTTPMetadata("history.get"),
	})
}

// readJSONBodyOptional 读 JSON body（body 可空、非 dict 时回空 map）。
// Python: web_http_routes.py:119-124 _body_dict。
func readJSONBodyOptional(req *http.Request) map[string]any {
	body := map[string]any{}
	if req.Body == nil {
		return body
	}
	_ = json.NewDecoder(req.Body).Decode(&body)
	return body
}

// registerMappedRoutes 挂载表驱动路由（静态路径先于 {param} 行由表序保证）。
// Python: web_http_routes.py:880-925 _register_mapped_routes。
func registerMappedRoutes(r chi.Router, wc *WebChannel, routes []webHTTPMappedRoute) {
	for i := range routes {
		route := &routes[i]
		httpMethod := route.httpMethod
		path := "/api/v1" + route.path
		switch httpMethod {
		case "GET":
			r.Get(path, mappedHandler(route, wc))
		case "POST":
			r.Post(path, mappedHandler(route, wc))
		case "PUT":
			r.Put(path, mappedHandler(route, wc))
		case "PATCH":
			r.Patch(path, mappedHandler(route, wc))
		case "DELETE":
			r.Delete(path, mappedHandler(route, wc))
		}
	}
}

// mappedHandler 生成表驱动 handler（accept_body 决定是否读 body）。
func mappedHandler(route *webHTTPMappedRoute, wc *WebChannel) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if route.acceptBody {
			body = readJSONBodyOptional(req)
		}
		params := paramsFromMappedRoute(req, route, body)
		handleUnary(w, req, wc, route.rpcMethod, params, route.created, route.bindSessionParam)
	}
}
