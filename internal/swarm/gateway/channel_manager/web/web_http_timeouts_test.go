package web

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveWebHTTPTimeouts默认档 五个 env 全空时的默认值
// 对齐 Python web_http_timeouts.py:23-49。
func TestResolveWebHTTPTimeouts默认档(t *testing.T) {
	t.Setenv("GATEWAY_WEB_HTTP_SSE_TIMEOUT", "")
	t.Setenv("GATEWAY_WEB_HTTP_SSE_IDLE_TIMEOUT", "")
	t.Setenv("GATEWAY_WEB_HTTP_SSE_KEEPALIVE", "")
	t.Setenv("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", "")
	t.Setenv("GATEWAY_WEB_HTTP_HISTORY_TIMEOUT", "")

	require.Equal(t, 0.0, resolveWebHTTPSSETimeout())     // 默认 0=不限生命周期
	require.Equal(t, 0.0, resolveWebHTTPSSEIdleTimeout()) // 0=禁用
	require.Equal(t, 30.0, resolveWebHTTPSSEKeepalive())
	require.Equal(t, 120.0, resolveWebHTTPUnaryTimeout())
	require.Equal(t, 60.0, resolveWebHTTPHistoryTimeout())
}

// TestResolveWebHTTPTimeoutsenv覆盖与非法值 合法值覆盖；非法/负值回默认
// 对齐 Python web_http_timeouts.py:10-20 _env_float。
func TestResolveWebHTTPTimeoutsenv覆盖与非法值(t *testing.T) {
	t.Setenv("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", "5")
	require.Equal(t, 5.0, resolveWebHTTPUnaryTimeout())

	t.Setenv("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", "abc")
	require.Equal(t, 120.0, resolveWebHTTPUnaryTimeout()) // 非数字回默认

	t.Setenv("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", "-1")
	require.Equal(t, 120.0, resolveWebHTTPUnaryTimeout()) // 非 allow_zero 档 ≤0 回默认

	t.Setenv("GATEWAY_WEB_HTTP_SSE_TIMEOUT", "0")
	require.Equal(t, 0.0, resolveWebHTTPSSETimeout()) // allow_zero 档 0 合法

	t.Setenv("GATEWAY_WEB_HTTP_SSE_TIMEOUT", "-3")
	require.Equal(t, 0.0, resolveWebHTTPSSETimeout()) // allow_zero 档负数回默认 0
}
