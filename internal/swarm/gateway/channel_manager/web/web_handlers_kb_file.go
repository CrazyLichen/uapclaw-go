package web

import (
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// registerKBFileProxies 注册 kb 分帧方法族（领域三十 30.5 B1 拍板：对外 kb.*
// 语义面独立，gateway 零 kb 业务——控制面原样转发 kbadmin 分发，数据面薄映射为
// 既有 file.upload_chunk 协议（27.8，一比一对齐 workspace_file_adapter.py），
// server 落盘/消毒/去重/50MB 护栏全部复用 upload_storage。
func registerKBFileProxies(wc *WebChannel, ac *routing.AgentClient) {
	// 控制面：kb.doc.upload / kb.file.upload_complete / kb.doc.cancel_upload → 同名 E2A
	wc.registerMethod(string(schema.ReqMethodKBDocUpload), sessionProxyForward(wc, ac, string(schema.ReqMethodKBDocUpload)), false)
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
