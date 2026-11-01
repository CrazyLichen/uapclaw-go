package web

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// FileAPIRouter 返回 /file-api 子路由处理器。
//
// 子路由不带 "/file-api" 前缀（由调用方 Mount 时剥离），
// 未匹配的路径返回 404 {"error":"not_found"} 以对齐 Python 行为。
func FileAPIRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/file-content", HandleFileContentGet)
	r.Post("/file-content", HandleFileContentPost)
	r.Get("/list-files", HandleListFiles)
	r.Get("/list-markdown", HandleListMarkdown)
	r.Get("/download", HandleFileDownload)
	r.Get("/ws-debug-config", HandleWsDebugConfigGet)
	r.Post("/ws-debug-config", HandleWsDebugConfigPost)
	r.Post("/rebuild-agent-data", HandleRebuildAgentData)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusNotFound, "not_found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusMethodNotAllowed, "not_found")
	})
	return r
}

// SPAHandler 返回前端静态文件处理器，未命中时回退到 index.html。
//
// 静态资源来自编译期嵌入的 frontend/dist。
func SPAHandler() http.Handler {
	sub, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		// 嵌入目录在编译期确定，此处不可达；保守返回 500 而非 Fatal，避免库代码终止进程
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "frontend assets unavailable", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path
		// 非根路径且非 assets 前缀时，文件不存在则回退到 index.html（SPA 路由）
		if reqPath != "/" && !strings.HasPrefix(reqPath, "/assets/") {
			filePath := strings.TrimPrefix(reqPath, "/")
			if _, statErr := fs.Stat(sub, filePath); statErr != nil {
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────
