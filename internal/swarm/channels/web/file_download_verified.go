package web

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	serveradapter "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
)

// verifiedDownloadChunkMaxBytes 单块上限（对齐 server 侧 verifiedDownloadChunkMaxBytes）。
const verifiedDownloadChunkMaxBytes = 2 * 1024 * 1024

// verifiedAssetTokenKind verified-asset 令牌 kind。
// Python: container_file_http.py _VERIFIED_ASSET_TOKEN_KIND = "verified_asset_v1"。
const verifiedAssetTokenKind = "verified_asset_v1"

// fileAPIError 下载链 API 错误（对齐 Python _error_json）。
type fileAPIError struct {
	// error 错误描述
	Error string
	// code HTTP 错误码
	Code string
	// status HTTP status
	Status int
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeFileAPIError JSON 错误响应（Python container_file_http.py _error_json 形状）。
func writeFileAPIError(w http.ResponseWriter, apiErr *fileAPIError) {
	status := apiErr.Status
	if status == 0 {
		status = http.StatusBadRequest
	}
	writeJSONDetail(w, status, apiErr.Code, apiErr.Error)
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

// verifiedChunkResult 单块读取结果（对齐 serve_verified_download 的
// _decode_verified_chunk 返回元组 data/total_size/name/mime_type/eof）。
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

// fetchVerifiedChunk 经 server 侧 adapter 读 sealed-asset 有界分块并校验回读。
// Python: container_file_http.py:497-525 _verified_chunk → fetch_agent_unary
// (ReqMethod.FILE_DOWNLOAD_VERIFIED_CHUNK)；Go 单进程同源，直接调
// server/adapter.HandleWorkspaceFileRequest（偏差 D-B2-16：无跨进程 E2A 中转）。
func fetchVerifiedChunk(token, sessionID string, offset, limit int) (*verifiedChunkResult, *fileAPIError) {
	result, ok, code, err := serveradapter.HandleWorkspaceFileRequest(
		schema.ReqMethodFileDownloadVerifiedChunk,
		map[string]any{"token": token, "offset": offset, "limit": limit},
		sessionID,
	)
	if !ok || err != nil {
		errMsg := "verified download failed"
		if err != nil {
			errMsg = err.Error()
		}
		return nil, &fileAPIError{Error: errMsg, Code: code}
	}
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

// serveVerifiedDownload 流式回传已签名 sealed 资产（任意 AgentServer 传输形态）。
// Python: container_file_http.py:483-524 serve_verified_download。
func serveVerifiedDownload(w http.ResponseWriter, r *http.Request, uid, token string, tokenPayload map[string]any) {
	sessionIDValue, ok := tokenPayload["sid"].(string)
	if !ok || sessionIDValue == "" {
		writeFileAPIError(w, &fileAPIError{Error: "invalid_download_token", Code: "BAD_REQUEST"})
		return
	}
	_ = uid
	readChunk := func(offset, limit int) (*verifiedChunkResult, *fileAPIError) {
		return fetchVerifiedChunk(token, sessionIDValue, offset, limit)
	}
	serveFileChunks(w, r, readChunk)
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

// streamRange 按 [start,end] 逐块写出；totalSize<1 时按 eof 逐块直到结束。
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
	if totalSize >= 0 && start > end || start >= totalSize && totalSize >= 0 {
		return 0, 0, false
	}
	if totalSize >= 0 && end > totalSize-1 {
		end = totalSize - 1
	}
	return start, end, true
}

// quoteAllDownload 全量百分号编码（复用 file_api.go 的 quoteAll）。
func quoteAllDownload(name string) string {
	return quoteAll(name)
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
