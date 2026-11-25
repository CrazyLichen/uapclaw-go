package web

import (
	"os"
	"strconv"
	"strings"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveEnvFloat 读取浮点 env，空/非法/越界回默认值。
// Python: web_http_timeouts.py:10-20 _env_float（一比一）。
func resolveEnvFloat(name string, defaultValue float64, allowZero bool) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultValue
	}
	if allowZero {
		if value >= 0 {
			return value
		}
		return defaultValue
	}
	if value > 0 {
		return value
	}
	return defaultValue
}

// resolveWebHTTPSSETimeout SSE 流总生命周期秒数（默认 0=不封顶）。
// 长 agent 轮次可能跑数小时，硬上限会掐断在跑的 chat.send 流；
// 运维要显式限时时才设 GATEWAY_WEB_HTTP_SSE_TIMEOUT。
// Python: web_http_timeouts.py:23-30 resolve_web_http_sse_timeout。
func resolveWebHTTPSSETimeout() float64 {
	return resolveEnvFloat("GATEWAY_WEB_HTTP_SSE_TIMEOUT", 0.0, true)
}

// resolveWebHTTPSSEIdleTimeout 无帧空闲秒数，超过即结束 SSE（默认 0=禁用）。
// Python: web_http_timeouts.py:33-35 resolve_web_http_sse_idle_timeout。
func resolveWebHTTPSSEIdleTimeout() float64 {
	return resolveEnvFloat("GATEWAY_WEB_HTTP_SSE_IDLE_TIMEOUT", 0.0, true)
}

// resolveWebHTTPSSEKeepalive 等待期间 SSE keepalive 注释帧间隔秒数（默认 30）。
// Python: web_http_timeouts.py:38-40 resolve_web_http_sse_keepalive。
func resolveWebHTTPSSEKeepalive() float64 {
	return resolveEnvFloat("GATEWAY_WEB_HTTP_SSE_KEEPALIVE", 30.0, false)
}

// resolveWebHTTPUnaryTimeout unary wait_response 超时秒数（默认 120）。
// Python: web_http_timeouts.py:43-45 resolve_web_http_unary_timeout。
func resolveWebHTTPUnaryTimeout() float64 {
	return resolveEnvFloat("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", 120.0, false)
}

// resolveWebHTTPHistoryTimeout history JSON 收集器超时秒数（默认 60）。
// Python: web_http_timeouts.py:48-50 resolve_web_http_history_timeout。
func resolveWebHTTPHistoryTimeout() float64 {
	return resolveEnvFloat("GATEWAY_WEB_HTTP_HISTORY_TIMEOUT", 60.0, false)
}
