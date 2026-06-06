package wsorigin

import (
	"net/http"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// HTTPMiddleware 返回 net/http 中间件，对 WebSocket 升级请求进行 Origin 校验。
//
// 使用示例：
//
//	handler := wsorigin.HTTPMiddleware()(wsHandler)
//	http.Handle("/ws", handler)
//
// 逻辑：
//   - 非 WebSocket 升级请求 → 直接放行（传递给下一个 handler）
//   - WebSocket 请求且未启用校验 → 直接放行
//   - WebSocket 请求且启用校验 → 校验 Origin，通过则放行，拒绝则返回 403
func HTTPMiddleware() func(http.Handler) http.Handler {
	return HTTPMiddlewareWithChecker(NewOriginChecker())
}

// HTTPMiddlewareWithChecker 使用自定义校验器返回中间件。
func HTTPMiddlewareWithChecker(checker *OriginChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 判断是否为 WebSocket 升级请求
			if !isWebSocketUpgrade(r) {
				next.ServeHTTP(w, r)
				return
			}

			// 未启用校验，直接放行
			if !checker.IsEnabled() {
				next.ServeHTTP(w, r)
				return
			}

			// 校验 Origin
			origin := r.Header.Get("Origin")
			if checker.IsAllowed(origin) {
				next.ServeHTTP(w, r)
				return
			}

			// 校验失败，返回 403
			code, headers, body := checker.ForbiddenResponse()
			for k, vs := range headers {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isWebSocketUpgrade 判断请求是否为 WebSocket 升级请求。
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
}
