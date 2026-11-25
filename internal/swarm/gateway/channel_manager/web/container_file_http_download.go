package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// container_file_http 下载半区：verified-asset 下载链（验签分块 + HEAD/Range/416）。
// 27-A B2 回收 D-27.8-7。Python: gateway/channel_manager/web/container_file_http.py
// （serve_verified_download :483-524 + serve_file_chunks :527+ + /download 路由 :838）。

// verifiedDownloadChunkMaxBytes 单块上限（对齐 server 侧 verifiedDownloadChunkMaxBytes）。
const verifiedDownloadChunkMaxBytes = 2 * 1024 * 1024

// VerifiedAssetTokenKind verified-asset 令牌 kind（导出供 channels/web 分流判断）。
// Python: container_file_http.py _VERIFIED_ASSET_TOKEN_KIND = "verified_asset_v1"。
const VerifiedAssetTokenKind = "verified_asset_v1"

// ──────────────────────────── 结构体 ────────────────────────────

// fileAPIError 下载链 API 错误（对齐 Python _error_json）。
type fileAPIError struct {
	// error 错误描述
	Error string
	// code HTTP 错误码
	Code string
	// status HTTP status
	Status int
}

// verifiedChunkResult 单块读取结果（对齐 _decode_verified_chunk 返回元组）。
type verifiedChunkResult struct {
	// data 块数据
	data []byte
	// totalSize 资产总大小
	totalSize int64
	// name 文件名
	name string
	// mimeType mime 提示
	mimeType string
	// eof 是否末块
	eof bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ServeVerifiedDownload 流式回传已签名 sealed 资产（经 E2A 请求 AgentServer 分块读取）。
// Python: container_file_http.py:483-524 serve_verified_download（fetch_agent_unary）。
func ServeVerifiedDownload(w http.ResponseWriter, r *http.Request, ac *routing.AgentClient, uid, token string, tokenPayload map[string]any) {
	sessionIDValue, ok := tokenPayload["sid"].(string)
	if !ok || sessionIDValue == "" {
		writeFileAPIError(w, &fileAPIError{Error: "invalid_download_token", Code: "BAD_REQUEST"})
		return
	}
	_ = uid
	readChunk := func(offset, limit int) (*verifiedChunkResult, *fileAPIError) {
		return fetchVerifiedChunk(ac, token, sessionIDValue, offset, limit)
	}
	serveFileChunks(w, r, readChunk)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeFileAPIError JSON 错误响应（Python container_file_http.py _error_json 形状）。
func writeFileAPIError(w http.ResponseWriter, apiErr *fileAPIError) {
	status := apiErr.Status
	if status == 0 {
		status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": apiErr.Error, "code": apiErr.Code}
	_ = json.NewEncoder(w).Encode(body)
}

// verifiedChunkFetcher 分块读取缝：默认走 E2A；单测可覆盖为直取 server/adapter。
var verifiedChunkFetcher = fetchVerifiedChunkViaE2A

// fetchVerifiedChunk 读 sealed-asset 有界分块并校验回读。
// Python: container_file_http.py:497-525 _verified_chunk → fetch_agent_unary
// (ReqMethod.FILE_DOWNLOAD_VERIFIED_CHUNK)。
func fetchVerifiedChunk(ac *routing.AgentClient, token, sessionID string, offset, limit int) (*verifiedChunkResult, *fileAPIError) {
	return verifiedChunkFetcher(ac, token, sessionID, offset, limit)
}

// fetchVerifiedChunkViaE2A 经 E2A 请求 AgentServer 读分块（生产路径）。
func fetchVerifiedChunkViaE2A(ac *routing.AgentClient, token, sessionID string, offset, limit int) (*verifiedChunkResult, *fileAPIError) {
	if ac == nil || !ac.ServerReady() {
		return nil, &fileAPIError{Error: "AgentServer is unavailable", Code: "SERVICE_UNAVAILABLE", Status: http.StatusServiceUnavailable}
	}
	env := e2a.E2AFromAgentFields(hexUUIDDownload(),
		e2a.WithFieldChannelID("web"),
		e2a.WithFieldSessionID(sessionID),
		e2a.WithFieldReqMethod(string(schema.ReqMethodFileDownloadVerifiedChunk)),
		e2a.WithFieldParams(map[string]any{"token": token, "offset": offset, "limit": limit}),
	)
	resp, err := ac.SendRequest(context.Background(), env)
	if err != nil || resp == nil || !resp.OK {
		errMsg := "verified download failed"
		if err != nil {
			errMsg = err.Error()
		} else if resp != nil {
			errMsg = extractE2AErrorMessage(resp.Payload)
		}
		return nil, &fileAPIError{Error: errMsg, Code: "INTERNAL_ERROR", Status: http.StatusInternalServerError}
	}
	result := resp.Payload
	// 校验块回读一致（_decode_verified_chunk：data_base64/size/name/mime_type/eof）
	totalSize, hasSize := intFromAnyNum(result["size"])
	if !hasSize || totalSize < 0 {
		return nil, &fileAPIError{Error: "invalid verified download response", Code: "INTERNAL_ERROR", Status: http.StatusBadGateway}
	}
	data, derr := decodeBase64Field(result["data_base64"])
	if derr != nil {
		return nil, &fileAPIError{Error: "invalid verified download response", Code: "INTERNAL_ERROR", Status: http.StatusBadGateway}
	}
	name := ""
	if v, okv := result["name"].(string); okv {
		name = v
	}
	if name == "" {
		name = "download"
	}
	mimeType := ""
	if v, okv := result["mime_type"].(string); okv {
		mimeType = v
	}
	eof, _ := result["eof"].(bool)
	return &verifiedChunkResult{data: data, totalSize: totalSize, name: name, mimeType: mimeType, eof: eof}, nil
}

// serveFileChunks 有界 reader 的 HEAD/Range 通用语义。
// Python: container_file_http.py:527+ serve_file_chunks：
// 首块探测 total_size/name/mime；Range 单区间解析；416 Content-Range；
// 206 局部流；200 全量流；Content-Disposition 附件名。
func serveFileChunks(w http.ResponseWriter, r *http.Request, readChunk func(offset, limit int) (*verifiedChunkResult, *fileAPIError)) {
	first, ferr := readChunk(0, 1)
	if ferr != nil {
		writeFileAPIError(w, ferr)
		return
	}
	totalSize := first.totalSize
	fileName := first.name
	mimeType := first.mimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	rangeHeader := strings.TrimSpace(r.Header.Get("Range"))
	start, end, hasRange := parseSingleByteRange(rangeHeader, totalSize)
	if rangeHeader != "" && !hasRange {
		// 416：Range 不可满足
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
		w.Header().Set("Content-Length", "0")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	if hasRange {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename*=UTF-8''%s", quoteAllDownload(fileName)))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		if !streamRange(w, readChunk, start, end) {
			return
		}
		return
	}

	w.Header().Set("Content-Type", mimeType)
	if totalSize >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(totalSize, 10))
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename*=UTF-8''%s", quoteAllDownload(fileName)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if !streamRange(w, readChunk, 0, totalSize-1) {
		return
	}
}

// streamRange 按 [start,end] 逐块写出；end<0 表示由 eof 决定结束。
func streamRange(w http.ResponseWriter, readChunk func(offset, limit int) (*verifiedChunkResult, *fileAPIError), start, end int64) bool {
	offset := start
	for {
		remaining := end - offset + 1
		if end >= 0 && remaining <= 0 {
			return true
		}
		limit := int64(verifiedDownloadChunkMaxBytes)
		if end >= 0 && remaining < limit {
			limit = remaining
		}
		chunk, ferr := readChunk(int(offset), int(limit))
		if ferr != nil {
			// 头已发出，无法改写 status；中断即客户端可见截断（对齐 Python 流中断行为）。
			logger.Warn(logComponent).Str("code", ferr.Code).Msg("[file-api] verified download chunk failed")
			return false
		}
		if len(chunk.data) > 0 {
			if _, werr := w.Write(chunk.data); werr != nil {
				logger.Warn(logComponent).Err(werr).Msg("[file-api] verified download write failed")
				return false
			}
		}
		offset += int64(len(chunk.data))
		if chunk.eof || (end >= 0 && offset > end) {
			return true
		}
		if len(chunk.data) == 0 && !chunk.eof {
			// 无数据且非末块：防死循环
			return false
		}
	}
}

// parseSingleByteRange 解析单字节 Range（bytes=start-end / bytes=start- / bytes=-suffix）。
// 越界或非法回 hasRange=false。Python: _parse_single_byte_range。
func parseSingleByteRange(header string, totalSize int64) (int64, int64, bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if strings.Contains(spec, ",") {
		// 多区间不支持 → 不可满足（对齐单区间语义）
		return 0, 0, false
	}
	dash := strings.Index(spec, "-")
	if dash < 0 {
		return 0, 0, false
	}
	startStr := strings.TrimSpace(spec[:dash])
	endStr := strings.TrimSpace(spec[dash+1:])
	var start, end int64
	if startStr == "" {
		// 后缀区间：bytes=-N → 取末 N 字节
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if totalSize < 0 {
			return 0, 0, false
		}
		if n > totalSize {
			n = totalSize
		}
		return totalSize - n, totalSize - 1, true
	}
	var err error
	start, err = strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}
	if endStr == "" {
		end = totalSize - 1
	} else {
		end, err = strconv.ParseInt(endStr, 10, 64)
		if err != nil {
			return 0, 0, false
		}
	}
	if totalSize >= 0 && (start > end || start >= totalSize) {
		return 0, 0, false
	}
	if totalSize >= 0 && end > totalSize-1 {
		end = totalSize - 1
	}
	return start, end, true
}

// quoteAllDownload 全量百分号编码（对齐 Python quote(file_name, safe="")）。
func quoteAllDownload(name string) string {
	// 与 channels/web file_api.go quoteAll 同语义
	out := make([]byte, 0, len(name)*3)
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			out = append(out, c)
		} else {
			out = append(out, '%')
			out = append(out, "0123456789ABCDEF"[c>>4], "0123456789ABCDEF"[c&0xF])
		}
	}
	return string(out)
}

// decodeBase64Field any → []byte（base64 字符串解码）。
func decodeBase64Field(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("not a base64 string")
	}
	return base64.StdEncoding.DecodeString(s)
}

// intFromAnyNum any → int64（int/int64/float64 归一）。
func intFromAnyNum(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

// extractE2AErrorMessage 从失败响应 payload 提取错误消息（嵌套/扁平两形状）。
func extractE2AErrorMessage(payload map[string]any) string {
	if payload == nil {
		return "request failed"
	}
	switch ev := payload["error"].(type) {
	case string:
		if ev != "" {
			return ev
		}
	case map[string]any:
		if m, ok := ev["message"].(string); ok && m != "" {
			return m
		}
	}
	if m, ok := payload["message"].(string); ok && m != "" {
		return m
	}
	return "request failed"
}

// hexUUIDDownload 生成请求 id（uuid4 hex 无连字符）。
func hexUUIDDownload() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}
