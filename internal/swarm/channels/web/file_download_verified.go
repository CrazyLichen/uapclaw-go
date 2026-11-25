package web

import (
	"net/http"
	"strings"

	gwweb "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/web"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// verified-asset 下载分支委托层（27-A B2 回收 D-27.8-7）。
// 实现落点对齐 Python container_file_http.py（gateway/channel_manager/web/
// container_file_http_download.go，2026-11-25 复核裁决：落点+中转一并对齐）；
// 本文件仅保留 /file-api/download 路由入口（channels/web 既有挂载点）与
// AgentClient 注入缝（对齐 Python attach_container_file_routes 的
// AgentOSRouterClient 装配条件——Go 恒个人版有 AgentClient，恒注入）。

// downloadAgentClient 下载分块 E2A 中转用 AgentClient（app_gateway 启动时注入）。
var downloadAgentClient *routing.AgentClient

// SetFileDownloadAgentClient 注入下载链 AgentClient（app_gateway 装配调用）。
func SetFileDownloadAgentClient(ac *routing.AgentClient) {
	downloadAgentClient = ac
}

// verifiedAssetTokenKind verified-asset 令牌 kind（委托判断用）。
const verifiedAssetTokenKind = gwweb.VerifiedAssetTokenKind

// serveVerifiedDownload 委托 gateway 侧实现（E2A 中转 + HEAD/Range/416）。
func serveVerifiedDownload(w http.ResponseWriter, r *http.Request, uid, token string, tokenPayload map[string]any) {
	gwweb.ServeVerifiedDownload(w, r, downloadAgentClient, uid, token, tokenPayload)
}

// resolveDownloadUserID 解析下载者 user_id：query 优先，头兜底。
// Python: container_file_http.py:845-848 _resolve_user_id。
func resolveDownloadUserID(r *http.Request) string {
	uid := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if uid == "" {
		uid = strings.TrimSpace(r.Header.Get("X-User-Id"))
	}
	return uid
}
