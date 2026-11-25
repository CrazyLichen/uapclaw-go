package adapter

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	webfiledownload "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/common/attachmentsafety"
	sessionlifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/attachments"
)

// workspaceFileMethods 文件域 adapter 承接的方法集（个人版八组，一比一 methods frozenset）。
// Python: gateway_adapter/workspace_file_adapter.py:109-127 methods
// （im.file_persist 归 IM 渠道域、workspace.* 四值归企业版挂账——D-27.8-1/2）。
var workspaceFileMethods = map[schema.ReqMethod]struct{}{
	schema.ReqMethodMediaPersist:              {},
	schema.ReqMethodMediaDiscard:              {},
	schema.ReqMethodDocumentPersist:           {},
	schema.ReqMethodDocumentFormats:           {},
	schema.ReqMethodPathSelectDirectory:       {},
	schema.ReqMethodPathSelectFiles:           {},
	schema.ReqMethodFileImportURL:             {},
	schema.ReqMethodFileUploadChunk:           {},
	schema.ReqMethodFileReadUploadChunk:       {},
	schema.ReqMethodFileDownloadVerifiedChunk: {},
	// workspace 面板四方法（27-A B2；Python: workspace_file_adapter.py:118-126
	// methods 注册表 + :199-238 _handle_workspace；个人版 adapter 同样分发）。
	schema.ReqMethodWorkspaceTree:          {},
	schema.ReqMethodWorkspacePreview:       {},
	schema.ReqMethodWorkspaceEntriesDelete: {},
	schema.ReqMethodWorkspaceUsage:         {},
}

// HandlesWorkspaceFileMethod 判断请求是否由文件域 adapter 短路承接。
func HandlesWorkspaceFileMethod(method schema.ReqMethod) bool {
	_, ok := workspaceFileMethods[method]
	return ok
}

// HandleWorkspaceFileRequest 文件域请求分发；返回 (payload, ok, code, err)。
// ok=false 且 code 非空表示业务错误（对齐 build_error_response 的 code 形状）。
// Python: workspace_file_adapter.py:140-197 _handle（分发顺序逐条对应）。
func HandleWorkspaceFileRequest(method schema.ReqMethod, params map[string]any, sessionID string) (
	payload map[string]any, ok bool, code string, err error,
) {
	switch method {
	case schema.ReqMethodMediaPersist:
		return handleMediaPersist(params, sessionID)
	case schema.ReqMethodMediaDiscard:
		return handleMediaDiscard(params, sessionID)
	case schema.ReqMethodDocumentPersist:
		return handleDocumentPersist(params, sessionID)
	case schema.ReqMethodDocumentFormats:
		// Python: :326-333 _handle_document_formats
		return map[string]any{"forbidden_formats": attachments.ForbiddenFormats()}, true, "", nil
	case schema.ReqMethodPathSelectDirectory:
		return handleSelectDirectory(params)
	case schema.ReqMethodPathSelectFiles:
		return handleSelectFiles(params)
	case schema.ReqMethodFileImportURL:
		return handleImportURL(params)
	case schema.ReqMethodFileUploadChunk:
		return handleUploadChunk(params)
	case schema.ReqMethodFileReadUploadChunk:
		return handleReadUploadChunk(params, sessionID)
	case schema.ReqMethodFileDownloadVerifiedChunk:
		return handleVerifiedDownloadChunk(params, sessionID)
	case schema.ReqMethodWorkspaceTree:
		return handleWorkspacePanel(method, params)
	case schema.ReqMethodWorkspacePreview:
		return handleWorkspacePanel(method, params)
	case schema.ReqMethodWorkspaceEntriesDelete:
		return handleWorkspacePanel(method, params)
	case schema.ReqMethodWorkspaceUsage:
		return handleWorkspacePanel(method, params)
	default:
		return nil, false, "BAD_REQUEST", fmt.Errorf("unsupported method: %s", method)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleMediaPersist media.persist：normalize 落盘后只回传关心的 key。
// Python: :240-264 _handle_media_persist。
func handleMediaPersist(params map[string]any, sessionID string) (map[string]any, bool, string, error) {
	normalized := copyParams(params)
	attachments.NormalizeChatMediaAttachments(normalized, sessionID)
	payload := map[string]any{}
	for _, key := range []string{"content", "query", "media_items", "files"} {
		if v, exists := normalized[key]; exists {
			payload[key] = v
		}
	}
	return payload, true, "", nil
}

// handleMediaDiscard media.discard：删除未发送的会话 uploads 副本。
// Python: :266-288 _handle_media_discard。
func handleMediaDiscard(params map[string]any, sessionID string) (map[string]any, bool, string, error) {
	rawPath, _ := params["path"].(string)
	if rawPath == "" {
		return nil, false, "BAD_REQUEST", fmt.Errorf("path is required")
	}
	payload, err := attachments.DiscardSessionUpload(sessionID, rawPath)
	if err != nil {
		return nil, false, "BAD_REQUEST", err
	}
	return payload, true, "", nil
}

// handleDocumentPersist document.persist：黑名单校验落盘并回填七 key + forbidden_formats 兜底。
// Python: :290-324 _handle_document_persist。
func handleDocumentPersist(params map[string]any, sessionID string) (map[string]any, bool, string, error) {
	normalized := copyParams(params)
	result := attachments.PersistAndParseDocuments(normalized, sessionID)
	payload := map[string]any{}
	for _, key := range []string{
		"content", "query", "media_items", "files", "documents", "document_errors", "forbidden_formats",
	} {
		if v, exists := result[key]; exists {
			payload[key] = v
		}
	}
	if _, exists := payload["forbidden_formats"]; !exists {
		payload["forbidden_formats"] = attachments.ForbiddenFormats()
	}
	return payload, true, "", nil
}

// handleSelectDirectory path.select_directory：workspace 内解析目录选择。
// Python: :335-381 _handle_select_directory。
func handleSelectDirectory(params map[string]any) (map[string]any, bool, string, error) {
	initialDir, _ := parseOptionalStr(params, "initial_dir")
	base := workspaceBaseDir()
	if initialDir == "" {
		// 无明确目标不把 workspace 根当"已选中"返回；返回 UNSUPPORTED 让前端回退手动输入表单
		return nil, false, "UNSUPPORTED", fmt.Errorf(
			"system directory picker is unavailable in this environment; enter the project name and absolute path manually")
	}
	selected := resolveWithinWorkspace(initialDir, base)
	if selected == "" {
		return nil, false, "BAD_REQUEST", fmt.Errorf("initial_dir must be inside the agent workspace")
	}
	fi, err := fileStat(selected)
	if err != nil {
		// 注入目录尚未创建：返回 cancelled，前端回落（与对话框取消同形）
		return map[string]any{"path": nil, "cancelled": true}, true, "", nil
	}
	if !fi.IsDir() {
		return nil, false, "BAD_REQUEST", fmt.Errorf("path is not a directory")
	}
	return map[string]any{"path": selected, "cancelled": false}, true, "", nil
}

// handleSelectFiles path.select_files：workspace 内枚举可上传文件元数据。
// Python: :383-435 _handle_select_files。
func handleSelectFiles(params map[string]any) (map[string]any, bool, string, error) {
	initialDir, _ := parseOptionalStr(params, "initial_dir")
	if raw, exists := params["allow_multiple"]; exists {
		if _, ok := raw.(bool); !ok {
			return nil, false, "BAD_REQUEST", fmt.Errorf("allow_multiple must be boolean")
		}
	}
	base := workspaceBaseDir()
	selectedDir := resolveWithinWorkspace(initialDir, base)
	if selectedDir == "" {
		return nil, false, "BAD_REQUEST", fmt.Errorf("initial_dir must be inside the agent workspace")
	}
	files := enumerateSelectableFiles(selectedDir)
	return map[string]any{"files": files, "cancelled": false}, true, "", nil
}

// handleImportURL file.import_url：外部 url 下载落盘注入目录（个人版路径）。
// Python: :673-730 _handle_import_url（企业 OBS 分支不移植，D-27.8-4）。
func handleImportURL(params map[string]any) (map[string]any, bool, string, error) {
	rawFiles, ok := params["files"].([]any)
	if !ok {
		return nil, false, "BAD_REQUEST", fmt.Errorf("files must be a list")
	}
	overwrite := false
	if v, exists := params["overwrite"]; exists {
		if b, ok := v.(bool); ok {
			overwrite = b
		}
	}
	imported := make([]any, 0, len(rawFiles))
	for _, raw := range rawFiles {
		item, isMap := raw.(map[string]any)
		if !isMap {
			imported = append(imported, raw)
			continue
		}
		result, err := importURLFile(item, overwrite)
		if err != nil {
			imported = append(imported, mergeError(item, err.Error()))
			continue
		}
		imported = append(imported, result)
	}
	return map[string]any{"files": imported}, true, "", nil
}

// handleUploadChunk file.upload_chunk：分块上传（首块 O_EXCL 认领 + 后续 append）。
// Python: :495-545 _handle_upload_chunk。
func handleUploadChunk(params map[string]any) (map[string]any, bool, string, error) {
	uploadID, _ := params["upload_id"].(string)
	uploadID = strings.TrimSpace(uploadID)
	relPath := strings.ReplaceAll(strings.TrimSpace(strOf(params["target_rel_path"])), "\\", "/")
	resolvedPath := strings.TrimSpace(strOf(params["resolved_path"]))
	rawChunk, hasChunk := params["data"].(string)
	isFinal := boolOf(params["final"])
	if uploadID == "" || relPath == "" || !hasChunk {
		return nil, false, "BAD_REQUEST", fmt.Errorf("upload_id, target_rel_path and data are required")
	}
	if strings.HasPrefix(relPath, "/") || containsDotDotSegment(relPath) {
		return nil, false, "BAD_REQUEST", fmt.Errorf("invalid_target_path")
	}
	chunk, err := decodeBase64Strict(rawChunk)
	if err != nil {
		return nil, false, "BAD_REQUEST", fmt.Errorf("invalid base64 data: %s", err)
	}
	if len(chunk) == 0 {
		return nil, false, "BAD_REQUEST", fmt.Errorf("data is empty")
	}
	var target string
	if resolvedPath == "" {
		target = filepath.Join(userWorkspaceDir(), relPath)
	} else {
		target = filepath.Clean(resolvedPath)
	}
	if !isPathWithinUserDirs(target) {
		return nil, false, "BAD_REQUEST", fmt.Errorf("path_outside_workspace")
	}
	var size int64
	if resolvedPath == "" {
		// 首块独占创建认领同名去重槽位（并发首块 check-then-write 会互相覆盖）
		claimed, err := attachments.AtomicWriteUnique(target, chunk)
		if err != nil {
			return nil, false, "INTERNAL_ERROR", fmt.Errorf("failed to persist upload: %s", err)
		}
		target = claimed
	} else {
		size, err = appendUploadChunk(target, chunk)
		if err != nil {
			return nil, false, "INTERNAL_ERROR", fmt.Errorf("failed to persist upload: %s", err)
		}
	}
	if size == 0 {
		fi, statErr := fileStat(target)
		if statErr == nil {
			size = fi.Size()
		}
	}
	return map[string]any{"path": target, "size": size, "upload_id": uploadID, "final": isFinal}, true, "", nil
}

// handleReadUploadChunk file.read_upload_chunk：会话 uploads 有界分块读。
// Python: :547-569 _handle_read_upload_chunk（四态错误码）。
func handleReadUploadChunk(params map[string]any, sessionID string) (map[string]any, bool, string, error) {
	path, _ := params["path"].(string)
	offset := intOf(params["offset"], 0)
	limit := intOf(params["limit"], attachments.UploadReadChunkBytes)
	payload, err := attachments.ReadSessionUploadChunk(sessionID, path, offset, limit)
	if err != nil {
		switch {
		case errIs(err, attachments.ErrUploadAccessDenied):
			return nil, false, "FORBIDDEN", err
		case errIs(err, attachments.ErrInvalidUploadRequest):
			// FileNotFoundError 单列：路径找不到 → NOT_FOUND
			return nil, false, "BAD_REQUEST", err
		default:
			return nil, false, "FORBIDDEN", err
		}
	}
	return payload, true, "", nil
}

// handleVerifiedDownloadChunk file.download_verified_chunk：sealed-asset 校验分块下载。
// Python: :571-671 _handle_verified_download_chunk（tenant_key 企业校验不移植，D-27.8-4）。
func handleVerifiedDownloadChunk(params map[string]any, sessionID string) (map[string]any, bool, string, error) {
	token, _ := params["token"].(string)
	token = strings.TrimSpace(token)
	offset := intOf(params["offset"], 0)
	limit := intOf(params["limit"], verifiedDownloadChunkMaxBytes)
	if _, intErr := params["offset"].(float64); params["offset"] != nil && !intErr {
		if _, isInt := params["offset"].(int); !isInt {
			return nil, false, "BAD_REQUEST", fmt.Errorf("offset and limit must be integers")
		}
	}
	if token == "" || offset < 0 || limit <= 0 || limit > verifiedDownloadChunkMaxBytes {
		return nil, false, "BAD_REQUEST", fmt.Errorf("invalid token, offset, or limit")
	}
	payloadMap := validateFileDownloadToken(token, sessionID)
	if payloadMap == nil || payloadMap["kind"] != "verified_asset_v1" {
		return nil, false, "FORBIDDEN", fmt.Errorf("verified download token rejected")
	}
	claimedSize, ok := intFromAny(payloadMap["size"])
	if !ok || claimedSize < 0 || offset > claimedSize {
		return nil, false, "BAD_REQUEST", fmt.Errorf("verified asset range is invalid")
	}
	result, code, err := readVerifiedAssetChunk(strOf(payloadMap["path"]), offset, claimedSize, limit, strOf(payloadMap["name"]))
	if err != nil {
		return nil, false, code, err
	}
	return result, true, "", nil
}

// parseOptionalStr 解析可选字符串参数：缺失→("", nil)；非字符串→错误。
// Python: workspace_file_adapter.py:71-77 _parse_optional_str。
func parseOptionalStr(params map[string]any, key string) (string, error) {
	raw, exists := params[key]
	if !exists || raw == nil {
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be string", key)
	}
	return strings.TrimSpace(s), nil
}

// resolveWithinWorkspace 把 initial_dir 解析为 workspace 根内的绝对路径（决策 D3 边界）。
// 空值→根；绝对路径须位于 base 内（越界→""）；相对路径拼接在 base 下。
// Python: workspace_file_adapter.py:80-103 _resolve_within_workspace。
func resolveWithinWorkspace(raw, base string) string {
	if raw == "" {
		return filepath.Clean(base)
	}
	cleaned := raw
	// 仅展开 ~（Python Path.expanduser）；相对路径在此分支保持相对语义
	if strings.HasPrefix(cleaned, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			cleaned = filepath.Join(home, strings.TrimPrefix(cleaned, "~"))
		}
	}
	if filepath.IsAbs(cleaned) {
		cleaned = resolveClean(cleaned)
		if cleaned != filepath.Clean(base) && !strings.HasPrefix(cleaned+string(filepath.Separator), filepath.Clean(base)+string(filepath.Separator)) {
			return ""
		}
		return cleaned
	}
	return resolveClean(filepath.Join(base, cleaned))
}

// workspaceBaseDir 注入 workspace 根。
// Python: get_agent_workspace_dir()。
func workspaceBaseDir() string {
	return pathutil.AgentWorkspaceDir()
}

// userWorkspaceDir 用户态数据根。
// Python: get_user_workspace_dir()。
func userWorkspaceDir() string {
	return pathutil.WorkspaceDir()
}

// enumerateSelectableFiles 枚举目录下可上传文件元数据（排除隐藏/下划线前缀，上限 200）。
// Python: workspace_file_adapter.py:405-422 _enumerate_files + :56-59 常量。
func enumerateSelectableFiles(dir string) []map[string]any {
	const (
		hiddenPrefixes     = "._"
		selectFilesLimit   = 200
		selectImageMaxByte = 4 * 1024 * 1024
	)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Python: OSError → logger.warning + 空列表
		return []map[string]any{}
	}
	files := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.IndexByte(hiddenPrefixes, name[0]) >= 0 {
			continue
		}
		full := filepath.Join(dir, name)
		info := describeLocalFile(full)
		if info != nil {
			files = append(files, info)
		}
		if len(files) >= selectFilesLimit {
			break
		}
	}
	return files
}

// describeLocalFile 与桌面端 select_local_files 同形的文件元数据；
// 图片 ≤4MB 附 base64 供前端预览（前端硬判 kind=image && base64）。
// Python: workspace_file_adapter.py:61-66 常量 + :784-818 _describe_local_file。
func describeLocalFile(path string) map[string]any {
	filename := filepath.Base(path)
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	} else if i := strings.Index(mimeType, ";"); i >= 0 {
		mimeType = mimeType[:i]
	}
	ext := strings.ToLower(filepath.Ext(filename))
	imageExts := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".jfif": true}
	kind := "document"
	if imageExts[ext] {
		kind = "image"
	}
	var size int64
	if fi, err := os.Stat(path); err == nil {
		size = fi.Size()
	}
	info := map[string]any{
		"path":      path,
		"filename":  filename,
		"size":      size,
		"mime_type": mimeType,
		"kind":      kind,
	}
	if kind == "image" && size > 0 && size <= 4*1024*1024 {
		if data, err := os.ReadFile(path); err == nil {
			info["base64"] = base64.StdEncoding.EncodeToString(data)
		}
		// 读取失败仅省略 base64（Python: logger.warning 后继续）
	}
	return info
}

// importURLFile 下载单个外部 url 文件到注入 workspace 并返回带本地 path 的元数据。
// Python: workspace_file_adapter.py:740-781 _import_url_file（60s 超时）。
func importURLFile(item map[string]any, overwrite bool) (map[string]any, error) {
	fileURL := strings.TrimSpace(firstNonEmptyString(item["url"], item["uri"]))
	if fileURL == "" {
		return nil, fmt.Errorf("missing url for file import")
	}
	fallbackName := "download"
	if base := path.Base(strings.SplitN(fileURL, "?", 2)[0]); base != "" && base != "." && base != "/" {
		fallbackName = base
	}
	filename := strings.TrimSpace(firstNonEmptyString(item["name"], item["filename"], fallbackName))
	if filename == "" {
		filename = "download"
	}
	workspaceDir := pathutil.AgentWorkspaceDir()
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return nil, err
	}
	target := filepath.Join(workspaceDir, filename)
	if !overwrite {
		target = attachments.UniqueUploadPath(target)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jiuwenswarm-agent")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	_, copyErr := io.Copy(f, resp.Body)
	if closeErr := f.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return nil, copyErr
	}
	size := int64(0)
	if fi, err := os.Stat(target); err == nil {
		size = fi.Size()
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(target)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	result := map[string]any{
		"path":      target,
		"filename":  filepath.Base(target),
		"size":      size,
		"mime_type": mimeType,
	}
	for k, v := range item {
		if _, exists := result[k]; !exists {
			result[k] = v
		}
	}
	return result, nil
}

// appendUploadChunk 追加一个解码后的分块（不缓冲整文件）。
// Python: workspace_file_adapter.py:733-737 _append_upload_chunk。
func appendUploadChunk(target string, chunk []byte) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	if _, err := f.Write(chunk); err != nil {
		_ = f.Close()
		return 0, err
	}
	size, statErr := f.Stat()
	closeErr := f.Close()
	if statErr != nil {
		return 0, statErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	return size.Size(), nil
}

// readVerifiedAssetChunk 校验后读取 sealed-asset 的有界分块。
// O_NOFOLLOW 等价：Lstat 拒 symlink + 普通文件 + 尺寸与 token 声明一致。
// Python: workspace_file_adapter.py:631-655（os.open O_NOFOLLOW + fstat 校验）。
func readVerifiedAssetChunk(path string, offset, claimedSize, limit int, name string) (map[string]any, string, error) {
	if name == "" {
		name = "download"
	} else {
		name = filepath.Base(name)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, "NOT_FOUND", fmt.Errorf("verified asset is unavailable")
	}
	if isSymlinkFileInfo(fi) || !fi.Mode().IsRegular() || fi.Size() != int64(claimedSize) {
		return nil, "FORBIDDEN", fmt.Errorf("verified asset changed after authorization")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "NOT_FOUND", fmt.Errorf("verified asset is unavailable")
	}
	defer func() { _ = f.Close() }()
	readLen := limit
	if claimedSize-offset < readLen {
		readLen = claimedSize - offset
	}
	data := make([]byte, readLen)
	n, err := f.ReadAt(data, int64(offset))
	if err != nil && n == 0 {
		return nil, "NOT_FOUND", fmt.Errorf("verified asset is unavailable")
	}
	data = data[:n]
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return map[string]any{
		"data_base64": base64.StdEncoding.EncodeToString(data),
		"offset":      offset,
		"chunk_size":  n,
		"size":        claimedSize,
		"eof":         offset+n >= claimedSize,
		"name":        name,
		"mime_type":   mimeType,
	}, "", nil
}

// validateFileDownloadToken 转发 web_file_download 令牌校验（kind 白名单由调用方判）。
func validateFileDownloadToken(token, sessionID string) map[string]any {
	return webfiledownload.ValidateFileDownloadToken(token, sessionID, true)
}

// isPathWithinUserDirs 判断路径是否位于用户态业务目录内（下载/上传边界校验）。
// Python: web_file_download.py:69-87 is_path_within_user_dirs
// （roots = user_workspace_dir + agent_sessions_dir + agent_workspace_dir +
// project_store 项目目录；此处一比一）。
func isPathWithinUserDirs(pathStr string) bool {
	if strings.TrimSpace(pathStr) == "" {
		return false
	}
	candidate := resolveClean(pathStr)
	roots := []string{
		pathutil.WorkspaceDir(),
		pathutil.AgentSessionsDir(),
		pathutil.AgentWorkspaceDir(),
	}
	for _, project := range sessionlifecycle.ListProjects() {
		if dir := strings.TrimSpace(project.ProjectDir); dir != "" {
			roots = append(roots, resolveClean(dir))
		}
	}
	for _, root := range roots {
		if candidate == root || strings.HasPrefix(candidate+string(filepath.Separator), root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ──────────────────────────── 通用小助手 ────────────────────────────

// copyParams 浅拷贝 params（Python dict(params) 等价）。
func copyParams(params map[string]any) map[string]any {
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = v
	}
	return out
}

// strOf any→string（nil→""）。
func strOf(v any) string {
	s, _ := v.(string)
	return s
}

// boolOf any→bool（缺省 false）。
func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

// intOf any→int（缺省 def）。JSON 解出 float64 或 int 双形态（YAML 整数解出 int 非 float64 坑）。
func intOf(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return def
	}
}

// intFromAny any→(int, ok)。
func intFromAny(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	default:
		return 0, false
	}
}

// containsDotDotSegment 路径是否含 ".." 段。
func containsDotDotSegment(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// decodeBase64Strict 严格 base64 解码（validate=True 等价）。
func decodeBase64Strict(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(attachmentsafety.StripDataURIPrefix(s))
}

// mergeError 返回 {**item, "error": msg} 形状。
func mergeError(item map[string]any, msg string) map[string]any {
	out := copyParams(item)
	out["error"] = msg
	return out
}

// errIs errors.Is 转发。
func errIs(err, target error) bool { return errors.Is(err, target) }

// fileStat os.Stat 转发。
func fileStat(path string) (os.FileInfo, error) { return os.Stat(path) }

// isSymlinkFileInfo 判断 FileInfo 是否符号链接。
func isSymlinkFileInfo(fi os.FileInfo) bool { return fi.Mode()&os.ModeSymlink != 0 }

// verifiedDownloadChunkMaxBytes 校验下载分块上限。
// Python: workspace_file_adapter.py:68 _VERIFIED_DOWNLOAD_CHUNK_MAX_BYTES = 512KiB。
const verifiedDownloadChunkMaxBytes = 512 * 1024

// resolveClean 展开 ~ 后取绝对路径（Python Path.expanduser().resolve(strict=False) 等价）。
func resolveClean(raw string) string {
	if strings.HasPrefix(raw, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			raw = filepath.Join(home, strings.TrimPrefix(raw, "~"))
		}
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return filepath.Clean(raw)
	}
	return filepath.Clean(abs)
}

// firstNonEmptyString 依序取第一个非空字符串。
func firstNonEmptyString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
