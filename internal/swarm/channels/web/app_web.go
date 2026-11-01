package web

import (
	"net/http"

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

// ──────────────────────────── 非导出函数 ────────────────────────────
