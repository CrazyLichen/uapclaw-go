package gateway

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fileEntry 文件列表条目。
type fileEntry struct {
	// Name 文件/目录名
	Name string `json:"name"`
	// Type 类型："file" 或 "dir"
	Type string `json:"type"`
	// Path 完整相对路径
	Path string `json:"path"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponentFileAPI 本文件日志组件
const logComponentFileAPI = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// wsDebugConfig WS 调试配置（内存存储）
var wsDebugConfig = map[string]any{
	"host": "127.0.0.1",
	"port": float64(19000),
}

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleFileContentGet 处理 GET /file-api/file-content 请求。
//
// 读取指定文件内容返回给前端。
// 对齐 Vite dev server 中 devFileContentApi() 的 GET 逻辑。
func HandleFileContentGet(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeJSONError(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}

	// 安全检查：路径穿越防护
	absPath, err := safeFilePath(filePath)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}

	// 读取文件内容
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, http.StatusNotFound, "文件不存在")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "读取文件失败: "+err.Error())
		return
	}

	// 根据编码参数处理
	encoding := r.URL.Query().Get("encoding")
	if encoding == "base64" {
		writeJSON(w, http.StatusOK, map[string]any{
			"path":     filePath,
			"content":  data,
			"encoding": "base64",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    filePath,
		"content": string(data),
	})
}

// HandleFileContentPost 处理 POST /file-api/file-content 请求。
//
// 写入 Markdown 文件。仅允许 .md 文件写入。
// 对齐 Vite dev server 中 devFileContentApi() 的 POST 逻辑。
func HandleFileContentPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}

	if body.Path == "" {
		writeJSONError(w, http.StatusBadRequest, "缺少 path 字段")
		return
	}

	// 仅允许 Markdown 文件写入
	if !strings.HasSuffix(strings.ToLower(body.Path), ".md") {
		writeJSONError(w, http.StatusForbidden, "仅允许写入 Markdown 文件")
		return
	}

	// 安全检查：路径穿越防护
	absPath, err := safeFilePath(body.Path)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建目录失败: "+err.Error())
		return
	}

	// 写入文件
	if err := os.WriteFile(absPath, []byte(body.Content), 0o644); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "写入文件失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": body.Path})
}

// HandleListFiles 处理 GET /file-api/list-files 请求。
//
// 列出指定目录下的文件和子目录，目录排在前面。
// 对齐 Vite dev server 中 devFileContentApi() 的 list-files 逻辑。
func HandleListFiles(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "/"
	}

	absDir, err := safeFilePath(dir)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, http.StatusNotFound, "目录不存在")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "读取目录失败: "+err.Error())
		return
	}

	var files []fileEntry
	for _, entry := range entries {
		// 跳过隐藏文件
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		}
		files = append(files, fileEntry{
			Name: entry.Name(),
			Type: entryType,
			Path: filepath.Join(dir, entry.Name()),
		})
	}

	// 排序：目录在前，名称升序
	sort.Slice(files, func(i, j int) bool {
		if files[i].Type != files[j].Type {
			return files[i].Type == "dir"
		}
		return files[i].Name < files[j].Name
	})

	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// HandleListMarkdown 处理 GET /file-api/list-markdown 请求。
//
// 列出指定目录下的 Markdown 文件。
// 对齐 Vite dev server 中 devFileContentApi() 的 list-markdown 逻辑。
func HandleListMarkdown(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "/"
	}

	absDir, err := safeFilePath(dir)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}

	var mdFiles []fileEntry
	err = filepath.WalkDir(absDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// 跳过隐藏文件和目录
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			relPath, _ := filepath.Rel(absDir, path)
			mdFiles = append(mdFiles, fileEntry{
				Name: d.Name(),
				Type: "file",
				Path: filepath.Join(dir, relPath),
			})
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, http.StatusNotFound, "目录不存在")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "遍历目录失败: "+err.Error())
		return
	}

	// 按名称排序
	sort.Slice(mdFiles, func(i, j int) bool {
		return mdFiles[i].Name < mdFiles[j].Name
	})

	writeJSON(w, http.StatusOK, map[string]any{"files": mdFiles})
}

// HandleWsDebugConfigGet 处理 GET /file-api/ws-debug-config 请求。
//
// 返回 WebSocket 调试配置。
func HandleWsDebugConfigGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, wsDebugConfig)
}

// HandleWsDebugConfigPost 处理 POST /file-api/ws-debug-config 请求。
//
// 更新 WebSocket 调试配置。
func HandleWsDebugConfigPost(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}

	for k, v := range body {
		wsDebugConfig[k] = v
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleRebuildAgentData 处理 POST /file-api/rebuild-agent-data 请求。
//
// 重建 Agent 数据（stub 实现）。
func HandleRebuildAgentData(w http.ResponseWriter, _ *http.Request) {
	logger.Info(logComponentFileAPI).Msg("rebuild-agent-data: stub 实现")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "rebuild_stub"})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeFilePath 将用户提供的相对路径转为绝对路径，并验证不超出工作区目录。
//
// 路径穿越防护：拒绝包含 ".." 的路径，确保结果路径在工作区内。
func safeFilePath(userPath string) (string, error) {
	// 拒绝明显的路径穿越
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("路径不允许包含 '..': %s", userPath)
	}

	// 基于工作区目录解析绝对路径
	workspaceDir := workspace.AgentWorkspaceDir()
	absPath := filepath.Join(workspaceDir, userPath)

	// 清理路径并验证仍在工作区内
	cleanPath := filepath.Clean(absPath)
	cleanWorkspace := filepath.Clean(workspaceDir)

	if !strings.HasPrefix(cleanPath, cleanWorkspace) {
		return "", fmt.Errorf("路径超出工作区范围: %s", userPath)
	}

	return cleanPath, nil
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Error(logComponentFileAPI).
			Err(err).
			Msg("JSON 编码响应失败")
	}
}

// writeJSONError 写入 JSON 错误响应。
func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, map[string]any{"error": message})
}
