package web

import (
	"context"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// registerKBFileProxies 注册 kb 分帧方法族（领域三十 30.5 B1 拍板：对外 kb.*
// 语义面独立，gateway 零 kb 业务——控制面原样转发 kbadmin 分发，数据面薄映射为
// 既有 file.upload_chunk 协议（27.8，一比一对齐 workspace_file_adapter.py），
// server 落盘/消毒/去重/50MB 护栏全部复用 upload_storage。
func registerKBFileProxies(wc *WebChannel, ac *routing.AgentClient) {
	// ②+① 断连清理依赖：记下转发用客户端（cancel 用）
	wc.kbUploadAC = ac
	// 控制面：kb.doc.upload 用带追踪的转发（成功后登记 upload_id，断连自动 cancel）；
	// upload_complete / cancel_upload 原样转发
	wc.registerMethod(string(schema.ReqMethodKBDocUpload), kbDocUploadForward(wc, ac), false)
	wc.registerMethod(string(schema.ReqMethodKBFileUploadComplete), sessionProxyForward(wc, ac, string(schema.ReqMethodKBFileUploadComplete)), false)
	wc.registerMethod(string(schema.ReqMethodKBDocCancelUpload), sessionProxyForward(wc, ac, string(schema.ReqMethodKBDocCancelUpload)), false)
	// 下载：kb.doc.download 反向分帧（kbadmin DownloadChunk 承接，前端聚合 blob URL ⤵️30.8）
	wc.registerMethod(string(schema.ReqMethodKBDocDownload), sessionProxyForward(wc, ac, string(schema.ReqMethodKBDocDownload)), false)
	// 数据面：kb.file.upload_chunk → file.upload_chunk（method 改写，params 原样透传；
	// resolved_path 由前端自 kb.doc.upload 返回值带入，落盘进 kb_uploads）
	wc.registerMethod(string(schema.ReqMethodKBFileUploadChunk), kbUploadChunkForward(wc, ac), false)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// kbUploadChunkForward 数据面薄映射：method 改写为既有 file.upload_chunk 协议转发。
func kbUploadChunkForward(wc *WebChannel, ac *routing.AgentClient) methodHandler {
	return sessionProxyForward(wc, ac, string(schema.ReqMethodFileUploadChunk))
}

// kbDocUploadForward 控制面转发 + 在途登记（②+①）：成功响应中提取 upload_id
// 记入 session 注册表；WS 断连时由 cleanupKBUploadsOnDisconnect 统一 cancel。
// 转发主体与 sessionProxyForward 同形（E2AFromAgentFields + SendRequest + sendResponse）。
func kbDocUploadForward(wc *WebChannel, ac *routing.AgentClient) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, sessionID string) {
		if ac == nil || !ac.ServerReady() {
			logger.Warn(logComponent).
				Str("method", string(schema.ReqMethodKBDocUpload)).
				Str("req_id", reqID).
				Msg("AgentServer is unavailable")
			wc.sendResponse(ws, reqID, false, nil, "AgentServer is unavailable", WsErrServiceUnavailable)
			return
		}
		env := e2a.E2AFromAgentFields(reqID,
			e2a.WithFieldChannelID(wc.ChannelID()),
			e2a.WithFieldSessionID(sessionID),
			e2a.WithFieldReqMethod(string(schema.ReqMethodKBDocUpload)),
			e2a.WithFieldParams(params),
		)
		resp, err := ac.SendRequest(context.Background(), env)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("method", string(schema.ReqMethodKBDocUpload)).
				Msg("session proxy forwarding to AgentServer failed")
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		if resp == nil {
			wc.sendResponse(ws, reqID, false, nil, "AgentServer returned empty response", WsErrInternalError)
			return
		}
		if resp.OK {
			if uploadID := extractUploadID(resp.Payload); uploadID != "" {
				wc.trackKBUpload(sessionID, uploadID)
			}
			wc.sendResponse(ws, reqID, true, resp.Payload, "", "")
			return
		}
		errMsg := "request failed"
		switch ev := resp.Payload["error"].(type) {
		case string:
			if ev != "" {
				errMsg = ev
			}
		}
		wc.sendResponse(ws, reqID, false, nil, errMsg, WsErrInternalError)
	}
}

// extractUploadID 从 kb.doc.upload 成功 payload 提取 upload_id（{"upload":{"upload_id":...}}）。
func extractUploadID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	upload, ok := payload["upload"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := upload["upload_id"].(string)
	return id
}

// trackConnSession 记录连接内出现过的 session（web_connect 消息循环调用）
func (wc *WebChannel) trackConnSession(conn *websocket.Conn, sid string) {
	wc.connSessionsMu.Lock()
	defer wc.connSessionsMu.Unlock()
	set, ok := wc.connSessions[conn]
	if !ok {
		set = make(map[string]struct{})
		wc.connSessions[conn] = set
	}
	set[sid] = struct{}{}
}

// trackKBUpload 登记 sid 的在途 upload_id
func (wc *WebChannel) trackKBUpload(sid, uploadID string) {
	wc.kbUploadMu.Lock()
	defer wc.kbUploadMu.Unlock()
	set, ok := wc.kbUploadSessions[sid]
	if !ok {
		set = make(map[string]struct{})
		wc.kbUploadSessions[sid] = set
	}
	set[uploadID] = struct{}{}
}

// cleanupKBUploadsOnDisconnect 断连清理：对该连接所有 session 的在途 kb 上传
// 逐个 fire-and-forget 发 kb.doc.cancel_upload（②+① 拍板；失败仅记日志，
// TTL/janitor 兜底）。
func (wc *WebChannel) cleanupKBUploadsOnDisconnect(conn *websocket.Conn) {
	wc.connSessionsMu.Lock()
	sids := make([]string, 0, len(wc.connSessions[conn]))
	for sid := range wc.connSessions[conn] {
		sids = append(sids, sid)
	}
	delete(wc.connSessions, conn)
	wc.connSessionsMu.Unlock()
	if len(sids) == 0 {
		return
	}
	ac := wc.kbUploadAC
	if ac == nil {
		return
	}
	for _, sid := range sids {
		wc.kbUploadMu.Lock()
		ids := make([]string, 0, len(wc.kbUploadSessions[sid]))
		for id := range wc.kbUploadSessions[sid] {
			ids = append(ids, id)
		}
		delete(wc.kbUploadSessions, sid)
		wc.kbUploadMu.Unlock()
		for _, uploadID := range ids {
			go cancelKBUpload(ac, sid, uploadID)
		}
	}
}

// cancelKBUpload 单个在途上传的断连 cancel（5s 超时，fire-and-forget）
func cancelKBUpload(ac *routing.AgentClient, sid, uploadID string) {
	if !ac.ServerReady() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := e2a.E2AFromAgentFields(MakeSessionID(),
		e2a.WithFieldChannelID(""),
		e2a.WithFieldSessionID(sid),
		e2a.WithFieldReqMethod(string(schema.ReqMethodKBDocCancelUpload)),
		e2a.WithFieldParams(map[string]any{"upload_id": uploadID}),
	)
	if _, err := ac.SendRequest(ctx, env); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("upload_id", uploadID).
			Str("session_id", sid).
			Msg("cancel kb upload on disconnect failed")
	}
}
