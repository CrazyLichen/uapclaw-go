package wsorigin

import (
	"net/http"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GorillaCheckOrigin 返回适用于 gorilla/websocket Upgrader.CheckOrigin 的函数。
//
// 使用示例：
//
//	upgrader := websocket.Upgrader{
//	    CheckOrigin: wsorigin.GorillaCheckOrigin(),
//	}
//
// 逻辑：
//   - 未启用校验 → 始终返回 true（放行）
//   - 启用校验 → 从请求 Origin 头提取值，调用 OriginChecker.IsAllowed 校验
func GorillaCheckOrigin() func(r *http.Request) bool {
	return GorillaCheckOriginWithChecker(NewOriginChecker())
}

// GorillaCheckOriginWithChecker 使用自定义校验器返回 CheckOrigin 函数。
func GorillaCheckOriginWithChecker(checker *OriginChecker) func(r *http.Request) bool {
	return func(r *http.Request) bool {
		// 未启用校验，直接放行
		if !checker.IsEnabled() {
			return true
		}

		origin := r.Header.Get("Origin")
		return checker.IsAllowed(origin)
	}
}
