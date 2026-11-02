package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fileEntry list-files 返回的目录条目。
//
// 对齐 Python: app_web.py _handle_file_api_get list-files 分支的字段集合
// （name / path / isMarkdown / isDirectory）。
type fileEntry struct {
	// Name 文件或目录名
	Name string `json:"name"`
	// Path 相对数据根的路径
	Path string `json:"path"`
	// IsMarkdown 是否 markdown 文件（.md / .mdx）
	IsMarkdown bool `json:"isMarkdown"`
	// IsDirectory 是否目录
	IsDirectory bool `json:"isDirectory"`
}

// markdownEntry list-markdown 返回的条目。
//
// 对齐 Python: 该分支只返回 name 与 path 两个字段。
type markdownEntry struct {
	// Name 文件名
	Name string `json:"name"`
	// Path 相对数据根的路径
	Path string `json:"path"`
}

// fileContentPostBody POST /file-api/file-content 请求体。
//
// 使用指针以区分「字段缺失」与「字段为空串」，对齐 Python
// `isinstance(request_path, str)` / `isinstance(request_content, str)` 的判断。
type fileContentPostBody struct {
	// Path 目标文件路径（数据根相对）
	Path *string `json:"path"`
	// Content 文件内容
	Content *string `json:"content"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentGateway

	// fileAPIPrefix file-api 路由前缀
	fileAPIPrefix = "/file-api"

	// agentDataRelPath agent-data.json 相对数据根的标准路径
	agentDataRelPath = "agent/workspace/agent-data.json"

	// defaultEncoding file-content 未指定 encoding 时的默认编码（对齐 Python "utf-8"）
	defaultEncoding = "utf-8"

	// autoEncoding file-content 自动探测编码的取值
	autoEncoding = "auto"

	// downloadChunkSize 下载分块大小（对齐 Python 65536）
	downloadChunkSize = 64 * 1024
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// allowedRoots 返回 file-api 允许访问的根目录列表。
	//
	// 对齐 Python app_web.py `_is_path_under_allowed_root`：
	//   - workspace_root    = <data>/agent
	//   - agent_teams_root  = <data>/.agent_teams
	//   - logs_root         = <data>/agent/.logs
	//   - auto_harness_root = <data>/auto-harness
	//
	// 注意：基准目录必须是 WorkspaceDir()（数据根）。AgentWorkspaceDir() 在未初始化时
	// 会回退到 resources 目录，不能作为校验基准。

	// errPathForbidden 路径越界（不在任何白名单根下）
	errPathForbidden = errors.New("path forbidden")

	// errNotDecodable 所有候选编码均无法解码
	errNotDecodable = errors.New("unable to decode file with any known encoding")

	// wsDisableCompress WS 压缩开关（内存存储，对齐 Python 类变量 ws_disable_compress）。
	// 用 atomic.Bool 是因为 HTTP handler 可能被并发调用，普通 bool 会触发数据竞争。
	wsDisableCompress atomic.Bool

	// encodingByName 编码名到 x/text 编码器的映射。
	//
	// 覆盖前端 FileViewer 下拉框的全部取值。gb2312 是 GBK 的子集，
	// x/text 无独立实现，统一用 GBK 解码器（可解 gb2312 编码的字节流）。
	encodingByName = map[string]encoding.Encoding{
		"utf-16":     unicode.UTF16(unicode.BigEndian, unicode.UseBOM),
		"utf-16le":   unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM),
		"utf-16be":   unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM),
		"gbk":        simplifiedchinese.GBK,
		"gb2312":     simplifiedchinese.GBK,
		"gb18030":    simplifiedchinese.GB18030,
		"big5":       traditionalchinese.Big5,
		"shift_jis":  japanese.ShiftJIS,
		"shift-jis":  japanese.ShiftJIS,
		"euc_kr":     korean.EUCKR,
		"euc-kr":     korean.EUCKR,
		"iso-8859-1": charmap.ISO8859_1,
		"latin-1":    charmap.ISO8859_1,
		"latin1":     charmap.ISO8859_1,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleFileContentGet 处理 GET /file-api/file-content 请求。
//
// 读取指定文件内容，以 text/plain 原样返回，并通过 X-Original-Encoding
// 告知前端实际使用的编码。
//
// 对齐 Python: app_web.py L604-659（返回裸文本而非 JSON 包装）。
func HandleFileContentGet(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_file_path")
		return
	}

	absPath, err := resolveFilePath(filePath)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden_path")
		return
	}

	if !pathExists(absPath) {
		// 对齐 Python: app_web.py L614-623 —— agent-data.json 缺失时先重建再重读
		if isAgentDataRelPath(filePath) {
			if genErr := skillruntime.GenerateAgentDataForWorkspace(workspace.AgentWorkspaceDir()); genErr != nil {
				writeJSONDetail(w, http.StatusInternalServerError, "generate_failed", genErr.Error())
				return
			}
		}
		if !pathExists(absPath) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error":    "file_not_found",
				"fullPath": absPath,
			})
			return
		}
	}

	encodingName := r.URL.Query().Get("encoding")
	if encodingName == "" {
		encodingName = defaultEncoding
	}

	content, usedEncoding, decErr := readFileWithEncoding(absPath, encodingName)
	if decErr != nil {
		logger.Warn(logComponent).Err(decErr).Str("path", absPath).
			Str("encoding", encodingName).Msg("read file content failed")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": decErr.Error()})
		return
	}

	body := []byte(content)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Original-Encoding", usedEncoding)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, wErr := w.Write(body); wErr != nil {
		logger.Error(logComponent).Err(wErr).Str("path", absPath).Msg("failed to write file content response")
	}
}

// HandleFileContentPost 处理 POST /file-api/file-content 请求。
//
// 保存 markdown 文件。目标文件必须已存在，且扩展名必须是 .md / .mdx。
//
// 对齐 Python: app_web.py L745-780。
func HandleFileContentPost(w http.ResponseWriter, r *http.Request) {
	var body fileContentPostBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if body.Path == nil || strings.TrimSpace(*body.Path) == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_file_path")
		return
	}
	if body.Content == nil {
		writeJSONError(w, http.StatusBadRequest, "missing_file_content")
		return
	}

	absPath, err := resolveFilePath(*body.Path)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden_path")
		return
	}

	if !isMarkdownPath(absPath) {
		writeJSONError(w, http.StatusBadRequest, "only_markdown_supported")
		return
	}
	// 对齐 Python: 只允许覆盖已存在文件，不新建
	if !pathExists(absPath) {
		writeJSONError(w, http.StatusNotFound, "file_not_found")
		return
	}

	if writeErr := os.WriteFile(absPath, []byte(*body.Content), 0o644); writeErr != nil {
		logger.Error(logComponent).Err(writeErr).Str("path", absPath).Msg("write file content failed")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": writeErr.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleListFiles 处理 GET /file-api/list-files 请求。
//
// 列出指定目录下一层条目，目录在前，返回 isMarkdown / isDirectory 布尔字段。
//
// 对齐 Python: app_web.py L575-602。
func HandleListFiles(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_dir")
		return
	}

	absDir, err := resolveFilePath(dir)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden_dir")
		return
	}

	// 对齐 Python: 目录不存在或不是目录时返回空列表而非 404
	if info, statErr := os.Stat(absDir); statErr != nil || !info.IsDir() {
		writeJSON(w, http.StatusOK, map[string]any{"files": []fileEntry{}})
		return
	}

	entries, readErr := os.ReadDir(absDir)
	if readErr != nil {
		logger.Warn(logComponent).Err(readErr).Str("dir", absDir).Msg("read directory failed")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": readErr.Error()})
		return
	}

	// 对齐 Python: 不过滤隐藏文件（前端自行过滤），按「目录优先 + 名称小写升序」排序
	files := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		files = append(files, fileEntry{
			Name:        entry.Name(),
			Path:        relToDataRoot(filepath.Join(absDir, entry.Name())),
			IsMarkdown:  !entry.IsDir() && isMarkdownName(entry.Name()),
			IsDirectory: entry.IsDir(),
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].IsDirectory != files[j].IsDirectory {
			return files[i].IsDirectory
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// HandleListMarkdown 处理 GET /file-api/list-markdown 请求。
//
// 列出指定目录下一层的 markdown 文件（非递归）。
//
// 对齐 Python: app_web.py L550-573。
func HandleListMarkdown(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_dir")
		return
	}

	absDir, err := resolveFilePath(dir)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden_dir")
		return
	}

	if info, statErr := os.Stat(absDir); statErr != nil || !info.IsDir() {
		writeJSON(w, http.StatusOK, map[string]any{"files": []markdownEntry{}})
		return
	}

	entries, readErr := os.ReadDir(absDir)
	if readErr != nil {
		logger.Warn(logComponent).Err(readErr).Str("dir", absDir).Msg("read directory failed")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": readErr.Error()})
		return
	}

	// 对齐 Python: sorted(full_dir.iterdir(), key=name.lower())，只取本层 markdown 文件
	files := make([]markdownEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isMarkdownName(entry.Name()) {
			continue
		}
		files = append(files, markdownEntry{
			Name: entry.Name(),
			Path: relToDataRoot(filepath.Join(absDir, entry.Name())),
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// HandleWsDebugConfigGet 处理 GET /file-api/ws-debug-config 请求。
//
// 对齐 Python: app_web.py L665-672 —— 只返回 wsDisableCompress。
func HandleWsDebugConfigGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"wsDisableCompress": wsDisableCompress.Load()})
}

// HandleWsDebugConfigPost 处理 POST /file-api/ws-debug-config 请求。
//
// 对齐 Python: app_web.py L782-802 —— 先解析 JSON（失败 invalid_json），
// 再校验 wsDisableCompress 必须是 bool（否则 invalid_ws_disable_compress）。
func HandleWsDebugConfigPost(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	raw, ok := body["wsDisableCompress"].(bool)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid_ws_disable_compress")
		return
	}

	wsDisableCompress.Store(raw)
	logger.Info(logComponent).Bool("ws_disable_compress", raw).Msg("ws disable compress updated")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"wsDisableCompress": raw,
	})
}

// HandleRebuildAgentData 处理 POST /file-api/rebuild-agent-data 请求。
//
// 重建 agent 工作区的 agent-data.json 文件树索引。
//
// 对齐 Python: app_web.py L735-743。
func HandleRebuildAgentData(w http.ResponseWriter, _ *http.Request) {
	workspaceDir := workspace.AgentWorkspaceDir()
	if err := skillruntime.GenerateAgentDataForWorkspace(workspaceDir); err != nil {
		logger.Warn(logComponent).Err(err).Str("workspace_dir", workspaceDir).
			Msg("rebuild agent-data failed")
		writeJSONDetail(w, http.StatusInternalServerError, "rebuild_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleFileDownload 处理 GET/HEAD /file-api/download 请求。
//
// HMAC 令牌验签 + 过期检查 + 流式返回。不做沙箱检查——令牌本身即授权。
//
// 对齐 Python: app_web.py L676-732 _handle_file_download。
func HandleFileDownload(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_token")
		return
	}

	payload := swarmtools.ValidateFileDownloadToken(token)
	if payload == nil {
		writeJSONError(w, http.StatusForbidden, "invalid_or_expired_token")
		return
	}

	filePath, _ := payload["path"].(string)
	if filePath == "" || !isRegularFile(filePath) {
		writeJSONError(w, http.StatusNotFound, "file_not_found")
		return
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("path", filePath).Msg("stat download file failed")
		writeJSONDetail(w, http.StatusInternalServerError, "download_failed", err.Error())
		return
	}
	fileName := filepath.Base(filePath)

	// 对齐 Python: mimetypes.guess_type，未识别回退 octet-stream
	mimeType := mime.TypeByExtension(filepath.Ext(fileName))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	// 对齐 Python: quote(file_name, safe="") 全量百分号编码
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+quoteAll(fileName))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}
	f, err := os.Open(filePath)
	if err != nil {
		// 头已发，只能记录（对齐 Python 写失败 log_error）
		logger.Error(logComponent).Err(err).Str("path", filePath).Msg("file download open failed")
		return
	}
	// errcheck：Close 错误不可行动（头已发、读循环已结束），显式丢弃并注释
	defer func() { _ = f.Close() }()

	buf := make([]byte, downloadChunkSize)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, wErr := w.Write(buf[:n]); wErr != nil {
				logger.Error(logComponent).Err(wErr).Msg("failed to send download response")
				return
			}
		}
		if readErr == io.EOF {
			return
		}
		if readErr != nil {
			logger.Error(logComponent).Err(readErr).Msg("file download read error")
			return
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveFilePath 把用户提供的相对路径解析为绝对路径，并校验落在白名单根下。
//
// 基准目录固定为数据根 WorkspaceDir()；任何越界路径返回 errPathForbidden。
//
// 对齐 Python: app_web.py L610/L763 的 (project_root / arg).resolve()
// 配合 _is_path_under_allowed_root。
func resolveFilePath(userPath string) (string, error) {
	normalized := strings.ReplaceAll(userPath, "\\", "/")
	if filepath.IsAbs(normalized) {
		return "", errPathForbidden
	}

	full := filepath.Join(workspace.WorkspaceDir(), normalized)
	resolved := resolveExistingPrefix(full)

	if !isUnderAllowedRoot(resolved) {
		return "", errPathForbidden
	}
	return resolved, nil
}

// resolveExistingPrefix 逐段解析符号链接：对"已存在的前缀"做 EvalSymlinks，
// 不存在的尾部按字面拼接。对齐 Python Path.resolve(strict=False)——
// 它只解析存在的前缀，缺的尾巴原样保留。
//
// 原实现"EvalSymlinks 失败就整体退化为 Clean"是错的：祖先是指向别处的符号链接、
// 叶子还不存在时（"已存在链接祖先 + 新文件"），路径不会被解析到链接目标，
// 白名单校验因此作用在字面路径上。当前 GET/POST 都要求文件已存在，暂不可利用，
// 但语义已与 Python 不一致；一旦放开"新建文件"就是一条逃逸路径。
func resolveExistingPrefix(full string) string {
	cur := filepath.Clean(full)
	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// 已到根仍解析失败：退回字面路径（Clean 后），与原行为一致
			return filepath.Clean(full)
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// isUnderAllowedRoot 判断绝对路径是否落在 file-api 白名单根内。
//
// 对齐 Python: _is_path_under_allowed_root 的 os.path.commonpath 比较
// （用 filepath.Rel 判定，避免 "/data/agent" 前缀误命中 "/data/agent2"）。
func isUnderAllowedRoot(absPath string) bool {
	target := filepath.Clean(absPath)
	for _, root := range allowedRoots() {
		cleanRoot := filepath.Clean(root)
		rel, err := filepath.Rel(cleanRoot, target)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

// allowedRoots 返回 file-api 允许访问的根目录列表（每次调用重新计算，便于测试替换 HOME）。
func allowedRoots() []string {
	dataDir := workspace.WorkspaceDir()
	return []string{
		workspace.AgentRootDir(),               // <data>/agent（含 workspace/sessions 等）
		workspace.AgentTeamsHomeDir(),          // <data>/.agent_teams
		workspace.LogsDir(),                    // <data>/agent/.logs
		filepath.Join(dataDir, "auto-harness"), // <data>/auto-harness
	}
}

// relToDataRoot 返回绝对路径相对数据根的斜杠形式路径。
//
// 对齐 Python: str(entry.relative_to(self.project_root))。
func relToDataRoot(absPath string) string {
	rel, err := filepath.Rel(workspace.WorkspaceDir(), absPath)
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	return filepath.ToSlash(rel)
}

// isAgentDataRelPath 判断请求路径是否指向标准 agent-data.json。
//
// 对齐 Python: file_arg.replace("\\", "/") == "agent/workspace/agent-data.json"。
func isAgentDataRelPath(userPath string) bool {
	normalized := strings.ReplaceAll(userPath, "\\", "/")
	normalized = strings.TrimPrefix(normalized, "./")
	return normalized == agentDataRelPath
}

// isMarkdownName 判断文件名是否 markdown（.md / .mdx，大小写不敏感）。
//
// 对齐 Python: _is_markdown 的 suffix in {".md", ".mdx"}。
func isMarkdownName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".mdx":
		return true
	default:
		return false
	}
}

// isMarkdownPath 判断路径是否 markdown 文件。
func isMarkdownPath(p string) bool {
	return isMarkdownName(p)
}

// pathExists 判断路径是否存在（对齐 Python Path.exists，不区分文件与目录）。
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// isRegularFile 判定常规文件（对齐 os.path.isfile）。
func isRegularFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// readFileWithEncoding 按指定编码读取文件，返回内容与实际使用的编码。
//
// encoding 取值：
//   - "auto"：先判 UTF-8 合法性，再按 gbk → big5 → shift_jis → euc_kr 顺序探测
//   - 其他：按 encodingByName 查表严格解码
//
// 对齐 Python: app_web.py L625-644 read_file_with_encoding。
// 差异：Python 的 auto 走 charset_normalizer 统计检测，Go 侧不引入该依赖，
// 改用「UTF-8 合法性 + 候选表顺序」探测（候选顺序与 Python 解码失败后的回退表一致）。
func readFileWithEncoding(filePath, encodingName string) (string, string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", err
	}

	switch strings.ToLower(strings.TrimSpace(encodingName)) {
	case "", "utf-8", "utf8":
		if !utf8.Valid(raw) {
			return "", "", fmt.Errorf("'utf-8' codec can't decode file %s", filePath)
		}
		return string(raw), "utf-8", nil
	case autoEncoding:
		return detectAndDecode(raw)
	}

	enc, ok := encodingByName[strings.ToLower(strings.TrimSpace(encodingName))]
	if !ok {
		return "", "", fmt.Errorf("unknown encoding: %s", encodingName)
	}
	content, ok := decodeStrict(raw, enc)
	if !ok {
		return "", "", fmt.Errorf("'%s' codec can't decode file %s", encodingName, filePath)
	}
	return content, encodingName, nil
}

// detectAndDecode 探测编码并解码（auto 模式）。
//
// 对齐 Python: auto 分支解码失败后依次尝试 gb2312/big5/shift_jis/euc_kr。
func detectAndDecode(raw []byte) (string, string, error) {
	if utf8.Valid(raw) {
		return string(raw), "utf-8", nil
	}
	candidates := []struct {
		name string
		enc  encoding.Encoding
	}{
		{"gbk", simplifiedchinese.GBK},
		{"big5", traditionalchinese.Big5},
		{"shift_jis", japanese.ShiftJIS},
		{"euc_kr", korean.EUCKR},
	}
	for _, cand := range candidates {
		if content, ok := decodeStrict(raw, cand.enc); ok {
			return content, cand.name, nil
		}
	}
	return "", "", errNotDecodable
}

// decodeStrict 用指定编码严格解码。
//
// x/text 的字符集解码器遇到非法字节会静默替换为 U+FFFD，这里把 U+FFFD 视为解码失败，
// 以对齐 Python UnicodeDecodeError 的语义。
func decodeStrict(raw []byte, enc encoding.Encoding) (string, bool) {
	out, _, err := transform.Bytes(enc.NewDecoder(), raw)
	if err != nil {
		return "", false
	}
	if bytes.ContainsRune(out, utf8.RuneError) {
		return "", false
	}
	return string(out), true
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Error(logComponent).Err(err).Msg("failed to encode response as JSON")
	}
}

// writeJSONError 写入带错误码的 JSON 响应。
//
// 错误码沿用 Python 契约（missing_file_path / forbidden_path / not_found 等），
// 前端按 HTTP 状态码判断成功与否，不解析文案。
func writeJSONError(w http.ResponseWriter, statusCode int, code string) {
	writeJSON(w, statusCode, map[string]any{"error": code})
}

// writeJSONDetail 写入带错误码与详情的 JSON 响应。
func writeJSONDetail(w http.ResponseWriter, statusCode int, code, detail string) {
	writeJSON(w, statusCode, map[string]any{"error": code, "detail": detail})
}

// quoteAll 百分号编码全部非 unreserved 字符（对齐 Python urllib.parse.quote(safe="")）。
//
// Go url.PathEscape 不转义 "/"，须手写逐字节编码；十六进制大写与 Python %XX 输出一致。
func quoteAll(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		// RFC 3986 unreserved: A-Z a-z 0-9 - _ . ~
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0xF])
		}
	}
	return b.String()
}
