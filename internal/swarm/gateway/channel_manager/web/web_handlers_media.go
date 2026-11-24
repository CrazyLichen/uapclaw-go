package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/common/attachmentsafety"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// e2aPayloadMaxBytes E2A 单帧 payload 上限（分流阈值基准）。
	// Python: gateway/routing/agent_http_bridge.py:31 E2A_PAYLOAD_MAX_BYTES = 4MiB。
	e2aPayloadMaxBytes = 4 * 1024 * 1024

	// e2aUploadChunkBytes 大文件 E2A 分块上传的块大小。
	// Python: agent_http_bridge.py:35 _E2A_UPLOAD_CHUNK_BYTES = 2MiB。
	e2aUploadChunkBytes = 2 * 1024 * 1024
)

// ──────────────────────────── 导出函数 ────────────────────────────

// mediaPersist 浏览器上传媒体附件落盘（E2A 转发 AgentServer 注入目录 uploads）。
// 先做 _pre_persist_large_media 大图分流，再经统一薄代理转发
// WorkspaceFileAdapter.MEDIA_PERSIST。
// Python: app_web_handlers.py:4040-4069 _media_persist + :2063-2119 _pre_persist_large_media。
func mediaPersist(wc *WebChannel, ac *routing.AgentClient) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, sessionID string) {
		if params != nil {
			prePersistLargeMedia(params, sessionID, ac)
		}
		sessionProxyForward(wc, ac, "media.persist")(ws, reqID, params, sessionID)
	}
}

// prePersistLargeMedia 转发 media.persist 前，把超预算的 base64 图片改为 E2A 分块上传。
// 预算 = e2aPayloadMaxBytes - params 其余部分 JSON 开销；超预算 item 在 Gateway
// 侧解码后按 2MiB 块经 file.upload_chunk 上传，标 _persisted（AgentServer 侧
// storeImageItem 透传不重复写盘）。上传失败保留原 base64（由下游链路处理）。
// Python: app_web_handlers.py:2063-2119 _pre_persist_large_media（就地修改 media_items）。
func prePersistLargeMedia(params map[string]any, sessionID string, ac *routing.AgentClient) {
	items, ok := params["media_items"].([]any)
	if !ok {
		return
	}
	// 预算扣减：其余 params 的 JSON 开销（Python: json.dumps(base_payload)）
	remaining := e2aPayloadMaxBytes
	basePayload := make(map[string]any, len(params))
	for k, v := range params {
		if k != "media_items" {
			basePayload[k] = v
		}
	}
	if b, err := json.Marshal(basePayload); err == nil {
		remaining -= len(b)
	} else {
		remaining -= 4096
	}
	newItems := make([]any, 0, len(items))
	for index, raw := range items {
		item, isMap := raw.(map[string]any)
		if !isMap || item["type"] != "image" {
			newItems = append(newItems, raw)
			continue
		}
		rawBase64 := ""
		if v, ok := item["base64Data"].(string); ok {
			rawBase64 = v
		} else if v, ok := item["base64_data"].(string); ok {
			rawBase64 = v
		}
		if strings.TrimSpace(rawBase64) == "" {
			newItems = append(newItems, raw)
			continue
		}
		data, err := base64.StdEncoding.DecodeString(attachmentsafety.StripDataURIPrefix(rawBase64))
		if err != nil || len(data) == 0 {
			newItems = append(newItems, raw)
			continue
		}
		if len(data) > remaining {
			persisted := uploadMediaItemViaE2A(item, data, sessionID, index, ac)
			if persisted != nil {
				newItems = append(newItems, persisted)
				continue
			}
			// 上传失败：保留原 base64（若仍超帧限制，由下游链路返回可重试错误）
			logger.Warn(logComponent).
				Str("method", "media.persist").
				Int("index", index).
				Msg("[media.persist] 大图上传失败，保留原 base64")
			newItems = append(newItems, item)
			continue
		}
		remaining -= len(data)
		newItems = append(newItems, item)
	}
	params["media_items"] = newItems
}

// uploadMediaItemViaE2A 把单张超预算图片经 E2A 分块上传到 AgentServer 注入目录。
// 落盘路径与 AgentServer 侧 storeImageItem 一致
// （agent/sessions/<safe_session_id>/uploads/<safe_filename>，同名去重）；
// 失败返回 nil（调用方保留原 base64 项）。
// Python: app_web_handlers.py:1994-2032 _upload_media_item_via_http +
// gateway/routing/agent_http_bridge.py:50-97 upload_file_bytes_via_e2a。
func uploadMediaItemViaE2A(item map[string]any, data []byte, sessionID string, index int, ac *routing.AgentClient) map[string]any {
	mimeType := strings.ToLower(strings.TrimSpace(firstStringOf(item["mimeType"], item["mime_type"])))
	suffix := attachmentsafety.ImageSuffixForMime(mimeType)
	if suffix == "" {
		return nil
	}
	fallback := fmt.Sprintf("image-%d%s", index+1, suffix)
	filename := attachmentsafety.EnsureImageUploadFilename(
		attachmentsafety.SanitizeUploadFilename(
			firstStringOf(item["filename"], fallback), fallback), suffix)
	relPath := fmt.Sprintf("agent/sessions/%s/uploads/%s",
		attachmentsafety.SafeSessionDirname(sessionID), filename)
	if len(data) == 0 {
		return nil
	}
	uploadID := fmt.Sprintf("gw-%d", time.Now().UnixNano())
	resolvedPath := ""
	var lastPayload map[string]any
	for offset := 0; offset < len(data); offset += e2aUploadChunkBytes {
		end := offset + e2aUploadChunkBytes
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]
		payload, ok := sendUploadChunk(ac, sessionID, map[string]any{
			"upload_id":       uploadID,
			"target_rel_path": relPath,
			"resolved_path":   resolvedPath,
			"data":            base64.StdEncoding.EncodeToString(chunk),
			"final":           end >= len(data),
		})
		if !ok {
			return nil
		}
		p, _ := payload["path"].(string)
		if strings.TrimSpace(p) == "" {
			return nil
		}
		resolvedPath = p
		lastPayload = payload
	}
	_ = lastPayload
	return map[string]any{
		"type":       "image",
		"filename":   filename,
		"mime_type":  mimeType,
		"path":       resolvedPath,
		"size_bytes": len(data),
		"_persisted": true,
	}
}

// sendUploadChunk 经 E2A 发送单个 file.upload_chunk 一元请求。
// Python: agent_http_bridge.py:78-97（fetch_agent_unary FILE_UPLOAD_CHUNK）。
func sendUploadChunk(ac *routing.AgentClient, sessionID string, params map[string]any) (map[string]any, bool) {
	if ac == nil || !ac.ServerReady() {
		return nil, false
	}
	reqID := fmt.Sprintf("gw-upload-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&gwUploadSeq, 1))
	env := e2a.E2AFromAgentFields(reqID,
		e2a.WithFieldChannelID("web"),
		e2a.WithFieldSessionID(sessionID),
		e2a.WithFieldReqMethod("file.upload_chunk"),
		e2a.WithFieldParams(params),
	)
	resp, err := ac.SendRequest(context.Background(), env)
	if err != nil || resp == nil || !resp.OK {
		return nil, false
	}
	return resp.Payload, true
}

// gwUploadSeq 上传请求 id 单调计数器。
var gwUploadSeq uint64

// ──────────────────────────── 非导出函数 ────────────────────────────

// firstStringOf 依序取第一个非空字符串。
func firstStringOf(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
